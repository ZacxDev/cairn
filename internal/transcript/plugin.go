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
//
// 🔴 AND THE VIEWER'S WAY OUT IS WHAT RENDERS A PLUGIN'S OUTPUT (S6, decision 11, clause k). An
// output is a CLAIM derived from the transcript, so it is shown to exactly the principals the
// transcript is: [View.Outputs] hangs off a [View], and the two constructors a viewer has —
// [Store.View] and [Store.ViewsWithOutputs] — each end in [Visible].

// Meta is a stored session's `meta.json`, for callers that decide over it.
type Meta = archive.Meta

// Roots lists every stored root session, sorted. nil with no archive.
func (s *Store) Roots() ([]string, error) {
	if s == nil || s.archive == nil {
		return nil, nil
	}
	return s.archive.Roots()
}

// Gate is a plugin's authority over stored sessions, as the caller folds it (decision 10).
type Gate struct {
	// On is the fold over a session's WHOLE visibility set: may the plugin read it?
	On func(meta *Meta, v []string) bool
	// Settled reports that the answer is OFF for `stored` AND FOR EVERY SUPERSET of it — the one
	// condition under which the store walk, which can only ADD scopes, need not run. Nil never
	// settles: every stored session is walked.
	Settled func(meta *Meta, stored []string) bool
}

// settled reports whether the walk may be skipped for `stored`, the STORED half of `V`.
//
// 🔴 "ON SAID NO OVER THE STORED HALF" IS NOT THAT CONDITION, AND IT STOOD HERE AS IF IT WERE (#270's
// audit, closed in S6). The comment read "decision 10's fold is monotone for a non-empty set — a
// superset cannot turn OFF into ON", and the fold is NOT monotone: an arc counts only when its HOME
// is in `V`, so a scope the walk ADDS can be the home of an ON arc, and adding it turns OFF into ON.
// Measured: a session whose arc home was found only by the walk was answered OFF, and flipped ON
// after an unrelated viewer's page view recorded the scope in `meta.Written` — an answer that
// depended on who had looked. So the skip is the CALLER's to justify ([Gate.Settled], which
// `internal/plugins` answers from the fold's own rules), and an empty stored set is never settled:
// empty is OFF and the walk may find a scope.
func (g Gate) settled(meta *Meta, stored []string) bool {
	return len(stored) > 0 && g.Settled != nil && g.Settled(meta, stored)
}

// ForPlugin returns the stored session as a [View] when `gate.On(meta, V)` says the plugin may read
// it, and nil otherwise — the same one answer for "not stored", "stored and OFF", "deleted" and "not
// a session id", so a plugin's refusal can name none of them (T10).
//
// 🔴 `V` IS THE WHOLE SET: the store's write trailers are walked over EVERY scope (decision 3's trap
// applies to a plugin exactly as to a viewer) and the walk's finds are folded into the grow-only
// `meta.Written`, as [Store.View] does. The walk is skipped only where [Gate.settled] says it cannot
// change the answer.
func (s *Store) ForPlugin(root string, gate Gate) (*View, error) {
	if s == nil || s.archive == nil {
		return nil, nil
	}
	meta, err := s.archive.ReadMeta(root)
	if err != nil || meta == nil {
		return nil, err
	}
	if gate.settled(meta, VisibilitySet(meta, Written{})) {
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
	if !gate.On(meta, VisibilitySet(meta, written)) {
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
func (s *Store) ForPlugins(roots []string, gate Gate, skip func(root string, err error)) ([]*View, error) {
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
		if gate.settled(meta, VisibilitySet(meta, Written{})) {
			continue // OFF for the stored half and for every superset of it ([Gate.settled])
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
		found := foundIn(bySession, meta)
		m, err := s.archive.RecordWritten(meta.Root, found)
		if err != nil {
			return nil, err
		}
		if m == nil {
			continue
		}
		if gate.On(m, VisibilitySet(m, Written{Scopes: found, Unscanned: unscanned})) {
			out = append(out, &View{Meta: m, a: s.archive})
		}
	}
	return out, nil
}

// ViewsWithOutputs is [Store.View] over many roots, for a LIST of sessions that shows each one's
// derived one-liner: the [View] of every root that holds at least one plugin output AND that the
// viewer may see, keyed by root. A root with no output is left out before anything is decided — a
// row with nothing to show costs one `stat`.
//
// 🔴 ONE STORE WALK AT MOST, AND THE SAME PREDICATE. Asked per row, [Store.View] walks the whole
// store once per session a non-owner's stored half admits — one whole-store parse per row of a
// page. Here the walk is [writtenIndex], once, and only when some candidate needs it (the owner's
// never do; a viewer the stored half refuses never does). What decides is [Visible], over the same
// set [Store.View] builds: the stored half, this walk's finds (folded into the grow-only
// `meta.Written`) and this walk's unscanned scopes.
//
// A root whose `meta.json` cannot be read or parsed is left out — shown to nobody, the listing's
// own rule ([Store.Mine]).
func (s *Store) ViewsWithOutputs(viewer Viewer, roots []string) (map[string]*View, error) {
	out := map[string]*View{}
	if s == nil || s.archive == nil {
		return out, nil
	}
	var walk []*Meta
	for _, root := range roots {
		if !s.archive.HasOutputs(root) {
			continue
		}
		meta, err := s.archive.ReadMeta(root)
		if err != nil || meta == nil {
			continue
		}
		switch {
		case viewer.owns(meta):
			out[root] = &View{Meta: meta, a: s.archive}
		case viewer.readsEvery(VisibilitySet(meta, Written{})):
			walk = append(walk, meta)
		}
	}
	if len(walk) == 0 {
		return out, nil
	}
	bySession, unscanned, err := writtenIndex(s.storeRoot)
	if err != nil {
		return nil, err
	}
	for _, meta := range walk {
		found := foundIn(bySession, meta)
		m, err := s.archive.RecordWritten(meta.Root, found)
		if err != nil {
			return nil, err
		}
		if m == nil {
			continue
		}
		if !Visible(viewer, m, VisibilitySet(m, Written{Scopes: found, Unscanned: unscanned})) {
			continue
		}
		out[m.Root] = &View{Meta: m, a: s.archive}
	}
	return out, nil
}

// foundIn is every scope ONE walk found a trailer of the session — or of a child session — in,
// sorted.
func foundIn(bySession map[string][]string, meta *Meta) []string {
	var found []string
	for _, id := range append([]string{meta.Root}, meta.Children...) {
		for _, scope := range bySession[id] {
			if !slices.Contains(found, scope) {
				found = append(found, scope)
			}
		}
	}
	slices.Sort(found)
	return found
}

// Delete is the OWNER's "delete transcript" (S6, clause l): the session's directory — its streams,
// blobs and every plugin output and ticket edge derived from it — is removed and the deletion is
// journaled (who uploaded it, which root, `owner-delete`, when; no content). It reports whether
// anything was deleted.
//
// 🔴 THE OWNER ARM AND NOTHING ELSE ([Viewer.owns]: `(Kind, ID)`, an UN-NARROWED credential). A
// reader of every scope the session touched may SEE the transcript and may not delete it; a
// narrowed credential of the owner may not either. False — and nothing deleted — is ONE answer for
// "not stored", "not yours" and "not a session id", so the row that calls this cannot be driven to
// ask what exists.
//
// The owner arm is asked AGAIN under the root's lock (`still`), over the meta as it stands then.
func (s *Store) Delete(viewer Viewer, root string) (bool, error) {
	if s == nil || s.archive == nil {
		return false, nil
	}
	return s.archive.Delete(root, archive.ReasonOwnerDelete, viewer.owns)
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

// QuotaError is the archive's quota refusal, for a caller mapping it to 507; its `Share` says
// which limit was hit.
type QuotaError = archive.QuotaError

// Which limit a [QuotaError] names.
const (
	ShareInstance = archive.ShareInstance
	SharePlugin   = archive.SharePlugin
	ShareOutputs  = archive.ShareOutputs
)

// ErrMetaUnparsable marks one session whose `meta.json` does not decode.
var ErrMetaUnparsable = archive.ErrMetaUnparsable

// ErrNoSession is the archive's answer for a root it no longer holds.
var ErrNoSession = archive.ErrNoSession
