package transcript

import (
	"slices"

	"github.com/ZacxDev/cairn/internal/control"
	"github.com/ZacxDev/cairn/internal/store"
	"github.com/ZacxDev/cairn/internal/touch"
	"github.com/ZacxDev/cairn/internal/transcript/archive"
)

// 🔴 VISIBILITY: ONE PREDICATE, ASKED BY EVERY SURFACE THAT SHOWS TRANSCRIPT CONTENT
// (`claudedocs/plan-cairn-plugins.md`, decisions 3 and 4; slice S4). The session page's transcript
// section and the raw-record view ask it today; the agent read API, plugin output rendering, edges
// and "My sessions" are later slices and ask the SAME function — through [Store.View], which is the
// only way this package hands out a stored session.
//
// The rule (operator decisions O2, O8, O10): a session's content is visible to its OWNER, and to a
// principal that can read EVERY scope the session wrote to or read from. Stricter than the session
// PAGE, which is found by a UNION (a write in ANY readable scope): a viewer can see the page and not
// the transcript, and then gets exactly what a session with no transcript gets.
//
// 🔴 AUTHORITY COMES FROM `control.Resolve`, NEVER FROM `Model.Grants`. A [Viewer] carries the
// `control.Authorization` its request was authenticated with — the resolved value, narrowed if the
// credential is — and nothing here reads a model. A grant-row listing under-reports every project
// member (`internal/ui`'s share flow carries the same ruling).

// Star is decision 3's sentinel, "a scope nobody can name": no viewer reads it.
const Star = archive.Star

// Viewer is who is asking: the principal and the authority ONE authentication produced.
type Viewer struct {
	Principal control.Principal
	Auth      control.Authorization
}

// owns is the OWNER ARM: the viewer's `(Kind, ID)` is the uploader's, and the credential is not
// narrowed.
//
// 🔴 `(Kind, ID)` AND NEVER `Display`: a display name is mutable and is not unique, so a principal
// renamed to another's id — or to another's name — must not become that owner.
//
// 🔴 AN UN-NARROWED CREDENTIAL ONLY. A narrowed credential of the owner is an agent credential cut
// down to some scopes; handing it the owner arm would give it every owner-only session, which is
// exactly what the narrowing withheld. It still gets the scope arm, and `control.Narrow` already
// intersected its scopes, so that arm is correct for it as written.
func (v Viewer) owns(meta *archive.Meta) bool {
	return !v.Auth.Narrowed() && meta.Owner == string(v.Principal.Kind)+":"+string(v.Principal.ID)
}

// reads is the scope arm's question about ONE name of `V`.
//
// The names in `V` are STORE names — a directory a trailer was found in, a name a rendered header
// printed — so they are asked of the same name-based set every reader narrows with
// (`Authorization.VisibleScopes`), folded the way that set folds.
func (v Viewer) reads(scope string) bool {
	// 🔴 `*` IS "UNKNOWN SCOPE", NOT "EVERY SCOPE". It is what a header naming no scope, an
	// unparseable ledger line and both fallbacks contribute, and it means the session touched
	// something nobody could identify — so nobody but the owner may read it.
	if scope == Star {
		return false
	}
	// 🔴 A NAME THIS AUTHORITY DOES NOT HOLD UNDER ANY VERB — renamed since the write, deleted, or
	// living on another instance — IS UNREADABLE, never skipped. "It no longer exists, so it cannot
	// be hiding anything" is the wrong direction: the content that was read from it is still in the
	// transcript.
	if !v.holds(scope) {
		return false
	}
	return v.Auth.VisibleScopes(control.VerbRead).Allows(scope)
}

// holds reports whether the authority reaches a scope of that name with ANY verb.
func (v Viewer) holds(scope string) bool {
	for _, verb := range control.AllVerbs {
		if v.Auth.VisibleScopes(verb).Allows(scope) {
			return true
		}
	}
	return false
}

// readsEvery is "every scope of the set", TRUE for the empty set — which is why [Visible] refuses
// an empty `V` itself rather than leaving it to this.
func (v Viewer) readsEvery(scopes []string) bool {
	return !slices.ContainsFunc(scopes, func(scope string) bool { return !v.reads(scope) })
}

// Visible is decision 4's predicate: may this viewer see this stored session's content, given the
// session's whole visibility set `V` ([VisibilitySet])?
//
// True iff EITHER the owner arm ([Viewer.owns]) OR the scope arm: `V` is non-empty and the viewer
// reads every name in it.
//
// 🔴 AN EMPTY `V` IS OWNER-ONLY (O10, clause g). "Every scope of the empty set" is vacuously true,
// and a session with no recorded write or read is precisely one nothing vouches for: it reaches the
// store with no scope whose readers could have been asked.
func Visible(viewer Viewer, meta *archive.Meta, v []string) bool {
	if meta == nil {
		return false
	}
	if viewer.owns(meta) {
		return true
	}
	if len(v) == 0 {
		return false
	}
	return viewer.readsEvery(v)
}

// Written is the `W_trailer` half of `V`: where the store's surviving write trailers name a
// session.
type Written struct {
	// Scopes is every scope in which a surviving trailer names the session (or one of its child
	// sessions), sorted.
	Scopes []string
	// Unscanned is every scope holding an entry the walk could NOT scan — malformed, or unreadable.
	// A trailer there is invisible to the walk, so the scope is in `V` as if it were written
	// (see [VisibilitySet]).
	Unscanned []string
}

// WrittenScopes walks EVERY scope in the store for trailers naming one of `ids`.
//
// 🔴 EVERY SCOPE IN THE STORE, NEVER THE VIEWER'S READABLE SET — decision 3's trap. Over a viewer's
// readable set every scope found is readable by construction, so "reads every written scope" would
// be vacuously true and a viewer of ONE written scope would see the transcript. The session PAGE
// walks the viewer's set, rightly, for "which of its writes may I see"; this is the other question.
// It therefore takes no scope set at all.
//
// The cost is one whole-store parse per call — measured beside the session page's own walk by
// `internal/ui`'s `BenchmarkSessionPageAndScopeTabs` (`written-scopes-walk`) — which is why
// [Store.View] asks it only when the answer could matter.
func WrittenScopes(storeRoot string, ids []string) (Written, error) {
	return writtenWithin(storeRoot, store.Unrestricted(), ids)
}

func writtenWithin(storeRoot string, within store.ScopeSet, ids []string) (Written, error) {
	index, err := store.LoadStore(storeRoot, "scanned", within)
	if err != nil {
		return Written{}, err
	}
	var w Written
	for _, scope := range index.Scopes() {
		res, err := touch.Writes(storeRoot, index, scope)
		if err != nil {
			return Written{}, err
		}
		if slices.ContainsFunc(res.Sessions, func(s touch.Session) bool { return slices.Contains(ids, s.ID) }) {
			w.Scopes = append(w.Scopes, res.Scope)
		}
		if res.Coverage.EntriesMalformed+res.Coverage.EntriesUnreadable > 0 {
			w.Unscanned = append(w.Unscanned, res.Scope)
		}
	}
	return w, nil
}

// VisibilitySet is decision 3's `V(s) = W_trailer ∪ R_header ∪ L ∪ F ∪ D`: the written half from a
// store walk, and the read half as `meta.json` holds it — what the pod DERIVED over the stored
// records and blobs (`R_header`, and `*` for the fallbacks) and what the capture agent DECLARED
// (its own `L ∪ R_header ∪ F`). Sorted, de-duplicated.
//
// 🔴 A SCOPE THE WALK COULD NOT FULLY SCAN IS IN `V` TOO. `touch` reports such a scope's edges as a
// LOWER bound: a trailer in a malformed or unreadable entry is not seen. Leaving the scope out
// would let a damaged entry SHRINK `V`, the one direction every part of this set refuses, so the
// scope counts as written — which costs a viewer who cannot read it the transcript, and costs a
// viewer who can nothing.
func VisibilitySet(meta *archive.Meta, written Written) []string {
	out := slices.Concat(written.Scopes, written.Unscanned, meta.Derived, meta.Declared)
	slices.Sort(out)
	return slices.Compact(out)
}

// Store is the read side of the transcript archive: where stored sessions live, and the store
// root their write trailers are in.
type Store struct {
	Archive   *archive.Archive
	StoreRoot string
}

// View is a stored session a viewer MAY see — and the only handle this package gives out on a
// stored session's content.
//
// 🔴 THE ARCHIVE IS UNEXPORTED HERE ON PURPOSE. [View.Read] and [View.Records] are the two ways to
// read a stored record from outside this package, and a View exists only where [Visible] said yes,
// so "render without asking the predicate" is not something a caller can write by accident.
type View struct {
	Meta *archive.Meta
	a    *archive.Archive
}

// Record is one stored record: its sequence number in its stream, its source position on the
// host, and the record as uploaded.
type Record = archive.StoredRecord

// StopRecords is what a [View.Records] callback returns to end the scan early.
var StopRecords = archive.StopScan

// Records calls fn for every stored record of one of the session's streams, in order. A stream the
// session does not hold calls fn zero times.
func (v *View) Records(stream string, fn func(Record) error) error {
	return v.a.ScanStream(v.Meta.Root, stream, fn)
}

// Read builds the page of the collapsed reading that holds turn `turn` ([Read]).
func (v *View) Read(turn int) (Reading, error) { return Read(v.a, v.Meta, turn) }

// View answers "what may this viewer see of this session's transcript": the stored session when
// [Visible] says so, and NIL otherwise.
//
// 🔴 NIL IS ONE ANSWER FOR THREE STATES — no transcript was ever stored, one is stored and this
// viewer is refused, and the id is not a session id at all — so no caller CAN render them
// differently, and nothing says which scope hid a session (T10). A caller that needs to tell them
// apart is a caller about to build an existence oracle.
//
// The child rule (decision 7) is in the ids the walk is asked about: the root AND every child
// session uploaded under it, so a trailer a child wrote counts toward the root's `V` and the whole
// tree is visible or none of it.
//
// ⚠ THE STORE WALK IS SKIPPED WHEN IT CANNOT CHANGE THE ANSWER: for the owner (the owner arm never
// reads `V`), and for a viewer the STORED half of `V` already refuses (a superset cannot admit
// them). [Visible] is still what decides, over the set as far as it was computed.
func (s Store) View(viewer Viewer, root string) (*View, error) {
	if s.Archive == nil {
		return nil, nil
	}
	meta, err := s.Archive.ReadMeta(root)
	if err != nil || meta == nil {
		return nil, err
	}
	v := VisibilitySet(meta, Written{})
	if !viewer.owns(meta) && viewer.readsEvery(v) {
		ids := append([]string{meta.Root}, meta.Children...)
		written, err := WrittenScopes(s.StoreRoot, ids)
		if err != nil {
			return nil, err
		}
		v = VisibilitySet(meta, written)
	}
	if !Visible(viewer, meta, v) {
		return nil, nil
	}
	return &View{Meta: meta, a: s.Archive}, nil
}
