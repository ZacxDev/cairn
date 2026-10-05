// Package touch derives WHO TOUCHED A SCOPE from the bytes the store already holds.
//
// In this phase "touched" means WROTE AN ATTRIBUTED BULLET THAT STILL SURVIVES IN THE
// ENTRY: a ` [cairn: <actor>/<session>]` trailer, read by `write.ParseAttributions`,
// at the end of a `## Nuance / work-history` bullet. Nothing is indexed or cached —
// the edges ARE the current bytes, so they cannot be stale, and they cost one parse of
// the scope, the order of a `recall`.
//
// 🔴 A LIBRARY, NOT A HANDLER — the `internal/report` shape: plain values in, plain
// values out, no `net/http` type, no server configuration. Every error is
// classifiable with `errors.As`: an unknown scope is `*store.UnknownScopeError`, and an
// entry that could not be read is a `*store.EntryUnreadableError` on
// `Result.Unreadable` — COUNTED, not fatal, so the edges from the entries that were
// read survive beside an honest statement of what was not.
//
// 🔴 IT NEVER OPENS THE STORE ITSELF. It is handed a `*store.Index` the caller has
// already narrowed to the scopes its principal may read, and reads only files that
// index names — so it cannot see a scope the caller could not.
//
// 🔴 WHAT AN EDGE PROVES, AND DOES NOT. A trailer is a claim the ENTRY makes. Only
// `write.AppendBullet` sets `<actor>` from a credential (and it is the principal's
// DISPLAY name, not an id); `put`/`create` write a caller's bytes verbatim, so a
// trailer can be forged; `<session>` is self-declared on every path; and a re-POST
// that dedupes writes no trailer, so a session that re-asserts an existing bullet
// leaves no edge. The coverage counts exist so that a caller cannot render a list of
// sessions as "everyone who wrote here" — most bullets carry no trailer at all.
package touch

import (
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/ZacxDev/cairn/internal/store"
	"github.com/ZacxDev/cairn/internal/write"
)

// WriteEdge is one attribution on one surviving bullet.
type WriteEdge struct {
	Scope    string
	EntryRef string // `store.Entry.Ref()` of the entry the bullet is in
	// CitationID is the bullet's `store.JournalBullet.CitationID`. ⚠ NOT SCOPED and
	// not unique: two byte-identical bullets share one, so pair it with EntryRef.
	CitationID string
	// Date is the bullet's own `- YYYY-MM-DD:` opener, or "". It is the ONLY time
	// the bytes carry: the pod's clock on an append, the writer's word on a
	// put/create. There is no time of day and no write timestamp anywhere else.
	Date    string
	Actor   string // as written in the trailer
	Session string // opaque, byte-exact
}

// BulletRef names one bullet a session wrote.
type BulletRef struct {
	EntryRef   string
	CitationID string
	Date       string
}

// Session is every surviving bullet one session id is attributed on, in this scope.
type Session struct {
	ID string
	// Actors are the distinct `<actor>` values recorded with this session, sorted
	// byte-wise. Usually one; more than one means the same session id was written
	// under several names — forged, reused, or a renamed principal — and is shown
	// rather than collapsed.
	Actors []string
	// Bullets are the distinct bullets carrying this session, in edge order. A bullet
	// whose trailer run names the session twice appears once. `len(Bullets)` is the
	// session's bullet count.
	Bullets []BulletRef
	// FirstDate and LastDate are the min and max non-empty `Date` over Bullets
	// (ISO dates compare correctly as strings), or "" when none is dated. Position in
	// the file is NOT used: newest-first is a convention a writer can break.
	FirstDate, LastDate string
	// Undated is how many of Bullets carry no date opener.
	Undated int
}

// Coverage is what was looked at, so an empty list can be told apart from a list
// that could not be made.
//
//   - Entries == 0 and EntriesMalformed == 0: the scope holds no entries.
//   - Bullets == 0 with Entries > 0: entries exist, no nuance bullet was written.
//   - Bullets > 0 and Attributed == 0: bullets exist, none carries a trailer — their
//     writers are unknown, NOT absent.
//   - EntriesUnreadable > 0 or EntriesMalformed > 0: the counts are a LOWER bound;
//     some entries' bullets were never scanned.
type Coverage struct {
	Entries           int // entries the index holds for the scope (readable or not)
	EntriesMalformed  int // entries the index REJECTED in this scope; never scanned
	EntriesUnreadable int // entries whose file could not be read; never scanned
	Bullets           int // nuance bullets scanned in the entries that were read
	Attributed        int // of Bullets, those with ≥1 grammatical attribution
	MalformedTrailers int // of Bullets, those with a refused `[cairn:…]` in trailer position
}

// Result is the derivation for one scope.
//
// ORDER, all deterministic for a given store:
//   - Edges: entries in the index's order (`Index.Entries`, which the loader sorts by
//     filename), bullets in file order within the nuance section, attributions left
//     to right within a bullet's trailer run.
//   - Sessions: by ID, byte-wise ascending — a total order, since IDs are unique here.
//   - Unreadable: index order.
type Result struct {
	Scope      string
	Edges      []WriteEdge
	Sessions   []Session
	Coverage   Coverage
	Unreadable []*store.EntryUnreadableError
}

// nuanceOnly is the one section scanned: `AppendBullet` writes only there, and a
// measured scan of real stores found every trailer under it.
var nuanceOnly = []string{store.NuanceHeading}

// Writes derives the session write edges for `scope` from the entries `ix` holds.
//
// `storeRoot` locates the files the index names (the same `<root>/<scope>/<filename>`
// rule `report.ReadEntry` uses). An unknown scope is `*store.UnknownScopeError`, the
// error `ix.Entries` returns; no other error is returned.
func Writes(storeRoot string, ix *store.Index, scope string) (Result, error) {
	entries, err := ix.Entries(scope)
	if err != nil {
		return Result{}, err
	}
	res := Result{Scope: store.NormalizeRef(scope)}
	res.Coverage.Entries = len(entries)
	res.Coverage.EntriesMalformed = len(ix.MalformedIn(scope))
	sessions := sessionSet{}

	for _, entry := range entries {
		path := filepath.Join(storeRoot, entry.Scope, entry.Filename)
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			res.Coverage.EntriesUnreadable++
			res.Unreadable = append(res.Unreadable, store.EntryUnreadable(path, readErr))
			continue
		}
		body := store.ExtractSections(store.DecodeReplace(data), nuanceOnly)[store.NuanceHeading]
		ref := entry.Ref()
		for _, b := range store.ParseJournalBullets(body) {
			res.Coverage.Bullets++
			attrs := write.ParseAttributions(b.Lines)
			if attrs.Malformed {
				res.Coverage.MalformedTrailers++
			}
			if len(attrs.Pieces) == 0 {
				continue
			}
			res.Coverage.Attributed++
			bullet := BulletRef{EntryRef: ref, CitationID: b.CitationID(), Date: b.Date}
			seen := map[string]bool{}
			for _, p := range attrs.Pieces {
				res.Edges = append(res.Edges, WriteEdge{
					Scope: entry.Scope, EntryRef: ref, CitationID: bullet.CitationID,
					Date: b.Date, Actor: p.Actor, Session: p.Session,
				})
				sessions.add(p, bullet, !seen[p.Session])
				seen[p.Session] = true
			}
		}
	}
	res.Sessions = sessions.sorted()
	return res, nil
}

type sessionAcc struct {
	s      Session
	actors map[string]struct{}
}

type sessionSet map[string]*sessionAcc

// add records one attribution. `newBullet` is false for a second piece naming the
// same session in ONE bullet's run, which adds its actor but not a second bullet.
func (set sessionSet) add(p write.Attribution, bullet BulletRef, newBullet bool) {
	a, ok := set[p.Session]
	if !ok {
		a = &sessionAcc{s: Session{ID: p.Session}, actors: map[string]struct{}{}}
		set[p.Session] = a
	}
	a.actors[p.Actor] = struct{}{}
	if !newBullet {
		return
	}
	a.s.Bullets = append(a.s.Bullets, bullet)
	if bullet.Date == "" {
		a.s.Undated++
		return
	}
	if a.s.FirstDate == "" || bullet.Date < a.s.FirstDate {
		a.s.FirstDate = bullet.Date
	}
	if bullet.Date > a.s.LastDate {
		a.s.LastDate = bullet.Date
	}
}

func (set sessionSet) sorted() []Session {
	out := make([]Session, 0, len(set))
	for _, a := range set {
		for actor := range a.actors {
			a.s.Actors = append(a.s.Actors, actor)
		}
		slices.Sort(a.s.Actors)
		out = append(out, a.s)
	}
	slices.SortFunc(out, func(x, y Session) int { return strings.Compare(x.ID, y.ID) })
	return out
}
