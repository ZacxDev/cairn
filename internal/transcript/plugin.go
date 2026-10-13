package transcript

import (
	"errors"
	"slices"
	"sync/atomic"

	"github.com/ZacxDev/cairn/internal/store"
	"github.com/ZacxDev/cairn/internal/touch"
	"github.com/ZacxDev/cairn/internal/transcript/archive"
)

// 🔴 THE SECOND WAY OUT OF [Store], AND WHY IT IS NOT A HOLE IN THE FIRST (slice S5). A PLUGIN IS NOT
// A VIEWER: it has no principal, no `control.Authorization` and no owner arm (decision 2 — "a plugin
// is not a principal"). Its authority over ONE session is decision 10's toggle fold, which the caller
// (`internal/worker`'s plugin routes, through `internal/plugins.Effective`) supplies as `on`. So
// [Store.ForPlugin] is [Store.View] with the predicate REPLACED, never skipped: it hands out a [View]
// only where `on` said yes, over the session's WHOLE visibility set — the store walk included — so a
// toggle fold can never be asked over a set narrower than the one [Visible] is asked over.

// Meta is a stored session's `meta.json`, for callers that decide over it.
type Meta = archive.Meta

// Roots lists every stored root session, sorted. nil with no archive.
func (s *Store) Roots() ([]string, error) {
	if s == nil || s.archive == nil {
		return nil, nil
	}
	return s.archive.Roots()
}

// ForPlugin returns the stored session as a [View] when `on(meta, V)` says the plugin may read it,
// and nil otherwise — the same one answer for "not stored", "stored and OFF" and "not a session id",
// so a plugin's refusal can name none of them (T10).
//
// 🔴 `V` IS THE WHOLE SET: the store's write trailers are walked over EVERY scope (decision 3's trap
// applies to a plugin exactly as to a viewer) and the walk's finds are folded into the grow-only
// `meta.Written`, as [Store.View] does.
//
// ⚠ THE WALK IS SKIPPED ONLY WHERE IT CANNOT CHANGE THE ANSWER: when the STORED half of `V` is
// non-empty and `on` already refuses it. Decision 10's fold is monotone for a non-empty set — every
// added scope must itself be consented, so a superset cannot turn OFF into ON — but NOT for an empty
// one (empty is OFF, and the walk may find a scope), so an empty stored set is always walked.
func (s *Store) ForPlugin(root string, on func(meta *Meta, v []string) bool) (*View, error) {
	if s == nil || s.archive == nil {
		return nil, nil
	}
	meta, err := s.archive.ReadMeta(root)
	if err != nil || meta == nil {
		return nil, err
	}
	stored := VisibilitySet(meta, Written{})
	if len(stored) > 0 && !on(meta, stored) {
		return nil, nil
	}
	ids := append([]string{meta.Root}, meta.Children...)
	written, err := WrittenScopes(s.storeRoot, ids)
	if err != nil {
		return nil, err
	}
	if meta, err = s.archive.RecordWritten(meta.Root, written.Scopes); err != nil || meta == nil {
		return nil, err
	}
	if !on(meta, VisibilitySet(meta, written)) {
		return nil, nil
	}
	return &View{Meta: meta, a: s.archive}, nil
}

// storeWalks counts whole-store trailer walks made for plugins — a test's instrument for "ONE walk
// per pending query, and none when nothing could be ON".
var storeWalks atomic.Int64

// writtenIndex is ONE walk of the whole store's write trailers, indexed by session id: every scope a
// trailer names each session in, and every scope the walk could not fully scan. [WrittenScopes]
// answers the same question for one id set; the pending query asks it for every stored root, and
// walking once per root was one whole-store parse per session per poll (#270 round 1: 0.36 s for 50
// sessions, 1.04 s for 200, with every toggle OFF).
func writtenIndex(storeRoot string) (bySession map[string][]string, unscanned []string, err error) {
	storeWalks.Add(1)
	index, err := store.LoadStore(storeRoot, "scanned", store.Unrestricted())
	if err != nil {
		return nil, nil, err
	}
	bySession = map[string][]string{}
	for _, scope := range index.Scopes() {
		res, err := touch.Writes(storeRoot, index, scope)
		if err != nil {
			return nil, nil, err
		}
		for _, sess := range res.Sessions {
			if !slices.Contains(bySession[sess.ID], res.Scope) {
				bySession[sess.ID] = append(bySession[sess.ID], res.Scope)
			}
		}
		if res.Coverage.EntriesMalformed+res.Coverage.EntriesUnreadable > 0 {
			unscanned = append(unscanned, res.Scope)
		}
	}
	return bySession, unscanned, nil
}

// ForPlugins is [Store.ForPlugin] over many roots with ONE store walk — and NO walk when no root
// needs one. A root whose `meta.json` does not decode is handed to `skip` and left out (OFF); every
// other error fails the whole call, so a broken store is never a confident empty answer.
func (s *Store) ForPlugins(roots []string, on func(meta *Meta, v []string) bool, skip func(root string, err error)) ([]*View, error) {
	if s == nil || s.archive == nil {
		return nil, nil
	}
	var need []*Meta
	for _, root := range roots {
		meta, err := s.archive.ReadMeta(root)
		if errors.Is(err, ErrMetaUnparsable) {
			skip(root, err)
			continue
		}
		if err != nil {
			return nil, err
		}
		if meta == nil {
			continue
		}
		if stored := VisibilitySet(meta, Written{}); len(stored) > 0 && !on(meta, stored) {
			continue // monotone: a superset cannot turn OFF into ON (see ForPlugin)
		}
		need = append(need, meta)
	}
	if len(need) == 0 {
		return nil, nil
	}
	bySession, unscanned, err := writtenIndex(s.storeRoot)
	if err != nil {
		return nil, err
	}
	var out []*View
	for _, meta := range need {
		var found []string
		for _, id := range append([]string{meta.Root}, meta.Children...) {
			for _, scope := range bySession[id] {
				if !slices.Contains(found, scope) {
					found = append(found, scope)
				}
			}
		}
		slices.Sort(found)
		m, err := s.archive.RecordWritten(meta.Root, found)
		if err != nil {
			return nil, err
		}
		if m == nil {
			continue
		}
		if on(m, VisibilitySet(m, Written{Scopes: found, Unscanned: unscanned})) {
			out = append(out, &View{Meta: m, a: s.archive})
		}
	}
	return out, nil
}

// OwnedBy reports whether `viewer` holds the OWNER ARM of a stored session (decision 4: un-narrowed,
// `(Kind, ID)` equal to the uploader's). It reads `meta.json` only — no content — and is false for a
// root that is not stored. The session-level toggle (the owner's opt-out) is authorised by it.
func (s *Store) OwnedBy(viewer Viewer, root string) (bool, error) {
	if s == nil || s.archive == nil {
		return false, nil
	}
	meta, err := s.archive.ReadMeta(root)
	if err != nil || meta == nil {
		return false, err
	}
	return viewer.owns(meta), nil
}

// Outputs calls fn for every plugin output line stored for the session, in the order written.
func (v *View) Outputs(fn func(line []byte) error) error {
	return v.a.ScanOutputs(v.Meta.Root, fn)
}

// AppendOutput stores ONE output line `plugin` wrote for the session ([archive.ErrNoSession] when it
// has been deleted since the View was decided; [archive.QuotaError] past the plugin's or all outputs'
// share of the quota).
func (v *View) AppendOutput(plugin string, line []byte) error {
	return v.a.AppendOutput(v.Meta.Root, plugin, line)
}

// QuotaError is the archive's quota refusal, for a caller mapping it to 507.
type QuotaError = archive.QuotaError

// ErrMetaUnparsable marks one session whose `meta.json` does not decode.
var ErrMetaUnparsable = archive.ErrMetaUnparsable

// ErrNoSession is the archive's answer for a root it no longer holds.
var ErrNoSession = archive.ErrNoSession
