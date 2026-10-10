package report

import (
	"slices"
	"strings"

	"github.com/ZacxDev/cairn/internal/arcs"
	"github.com/ZacxDev/cairn/internal/store"
	"github.com/ZacxDev/cairn/internal/touch"
)

// 🔴 "WHAT DID THIS ONE SESSION WRITE, ACROSS EVERY SCOPE YOU CAN READ" — THE BROWSER'S SESSION
// PAGE, AND A STRUCTURED ANSWER ONLY. There is no `RenderText` here and no pod route: nothing in the
// CLI or the conformance corpus prints this, so no printed byte moved to add it. It lives in this
// package rather than in `internal/ui` for ONE reason — the arc rule (an arc exists for a caller iff
// its HOME scope is readable, operator decision Q1) is applied in this package and nowhere else, and
// this answer lists arcs. A browser that re-derived "may this caller see this arc" would be a second
// place deciding it.
//
// 🔴 A SESSION ID IS GLOBAL, SO THIS IS THE ONE ANSWER THAT AGGREGATES ACROSS SCOPES BY A KEY THE
// CALLER CHOSE — and the whole safety argument is that it walks ONLY the index `visible` admits.
// `store.LoadStore` narrows the index to `visible`, `touch.Writes` reads only files that index names,
// and an arc is a candidate only if `visible.Allows(home)`. So a session that wrote ONLY in scopes the
// caller cannot read produces the SAME zero-value answer as an id nobody ever wrote — no scope, no
// arc, and coverage counted over the caller's scopes alone, which do not depend on the id at all.
// `TestASessionSeenOnlyInAnUnreadableScopeIsTheSameAnswerAsANeverWrittenOne` pins that relation.

// SessionAcrossReport is one session's attributed writes over every readable scope, plus the arcs
// (homed in a readable scope) that list it as a member.
type SessionAcrossReport struct {
	// ID is the session id exactly as asked — opaque, byte-exact, never folded.
	ID string
	// Scopes are the readable scopes this session wrote an attributed bullet in, NEWEST ACTIVITY
	// FIRST: by the session's latest bullet date in that scope, descending; a scope where every one
	// of its bullets is undated sorts after every dated one; ties by scope name, byte-wise.
	Scopes []SessionInScope
	// Arcs are the registrations homed in a readable scope whose member list names this session, in
	// `arcs.Snapshot.Sorted` order (home, then slug).
	Arcs []SessionArc
	// Coverage SUMS `touch.Writes`'s counts over EVERY readable scope — the denominator the page
	// states, so "this session wrote 3 bullets" is never read as "this session's whole footprint".
	Coverage touch.Coverage
	// ScopesScanned is how many readable scopes were walked.
	ScopesScanned int
	// ArcsConfigured is false when no journal was handed in (the designed off state), so an empty
	// Arcs list can be told apart from "nothing was looked at".
	ArcsConfigured bool
	// Damaged is the journal's own `Damaged()`: some record could not be read, so an arc registered
	// only by such a record is not listed.
	Damaged bool
}

// SessionInScope is the session's footprint in ONE scope: `touch.Session` unchanged, so no count is
// re-derived here.
type SessionInScope struct {
	Scope   string
	Session touch.Session
}

// SessionArc is one arc the session is a member of, with the role the registering tool recorded.
type SessionArc struct {
	Home, Slug, Status, ClosingKind, Role string
}

// SessionAcross derives the answer. `snap` is nil when no journal is configured.
//
// ⚠ THE ID IS MATCHED BYTE-EXACT AND NEVER VALIDATED HERE. Whether an id is even a POSSIBLE session
// (`write.SessionComponent`) is the caller's bound — the browser refuses a non-conforming id before
// it calls this, so a hostile query string never costs a store walk. Called with one anyway, it
// simply matches nothing.
//
// 🔴 IT IS [SessionsAcross] ASKED ABOUT ONE ID, NOT A SECOND WALK. Both read [walkReadable], so "the
// sessions list names this id" and "this id's page is found" are one predicate over one narrowed
// walk: a list naming a session whose page then 404s, or omitting one whose page answers, is not
// constructible from here.
//
// Errors are the store's own (`*store.StoreMissingError` and the like), as for `Sessions`.
func SessionAcross(storeRoot, session string, visible store.ScopeSet, snap *arcs.Snapshot) (SessionAcrossReport, error) {
	w, err := walkReadable(storeRoot, visible, snap, func(id string) bool { return id == session })
	if err != nil {
		return SessionAcrossReport{}, err
	}
	if rep, ok := w.bySession[session]; ok {
		return *rep, nil
	}
	rep := w.base
	rep.ID = session
	return rep, nil
}

// SessionsAcrossReport is EVERY session visible to the caller — the browser's `/sessions` list. Like
// [SessionAcrossReport] it is a STRUCTURED answer with no `RenderText`: no CLI verb and no pod route
// prints it, so no printed byte moved to add it.
type SessionsAcrossReport struct {
	// Sessions holds one [SessionAcrossReport] per session that is [SessionAcrossReport.Found] — the
	// SAME value `SessionAcross` answers for that id — NEWEST ACTIVITY FIRST: by the latest bullet date
	// over every readable scope, descending; a session with no dated bullet (every bullet undated, or
	// known only as an arc member) after every dated one; ties by id, byte-wise.
	Sessions []SessionAcrossReport
	// Coverage, ScopesScanned, ArcsConfigured and Damaged are the walk's own, as on each row.
	Coverage       touch.Coverage
	ScopesScanned  int
	ArcsConfigured bool
	Damaged        bool
}

// Partial is [SessionAcrossReport.Partial] over the whole walk: some readable entry was never
// scanned, so the list is a LOWER bound.
func (r SessionsAcrossReport) Partial() bool {
	return r.Coverage.EntriesMalformed > 0 || r.Coverage.EntriesUnreadable > 0
}

// SessionsAcross lists every session the caller can see anything of. `snap` is nil when no journal is
// configured. Errors are the store's own, as for `SessionAcross`.
func SessionsAcross(storeRoot string, visible store.ScopeSet, snap *arcs.Snapshot) (SessionsAcrossReport, error) {
	w, err := walkReadable(storeRoot, visible, snap, func(string) bool { return true })
	if err != nil {
		return SessionsAcrossReport{}, err
	}
	out := SessionsAcrossReport{Coverage: w.base.Coverage, ScopesScanned: w.base.ScopesScanned,
		ArcsConfigured: w.base.ArcsConfigured, Damaged: w.base.Damaged}
	for _, rep := range w.bySession {
		out.Sessions = append(out.Sessions, *rep)
	}
	slices.SortFunc(out.Sessions, func(a, b SessionAcrossReport) int {
		_, al := a.DateSpan()
		_, bl := b.DateSpan()
		if al != bl {
			// Newest first, "" last — `SessionAcross`'s scope order, one level up.
			return strings.Compare(bl, al)
		}
		return strings.Compare(a.ID, b.ID)
	})
	return out, nil
}

// readableWalk is [walkReadable]'s answer: the walk's own counts, and one report per wanted session
// that something was found for.
type readableWalk struct {
	// base carries Coverage, ScopesScanned, ArcsConfigured and Damaged — the parts of a
	// [SessionAcrossReport] that do not depend on the id.
	base      SessionAcrossReport
	bySession map[string]*SessionAcrossReport
}

// walkReadable is the ONE walk `SessionAcross` and `SessionsAcross` share, and so the ONE place both
// apply the narrowing: `store.LoadStore` narrows the index to `visible`, `touch.Writes` reads only
// files that index names, and an arc is a candidate only if `visible.Allows(home)` (Q1). `want` picks
// which session ids are collected; it never widens what is read.
func walkReadable(storeRoot string, visible store.ScopeSet, snap *arcs.Snapshot, want func(string) bool) (readableWalk, error) {
	w := readableWalk{base: SessionAcrossReport{ArcsConfigured: snap != nil}, bySession: map[string]*SessionAcrossReport{}}
	of := func(id string) *SessionAcrossReport {
		if rep, ok := w.bySession[id]; ok {
			return rep
		}
		rep := &SessionAcrossReport{ID: id}
		w.bySession[id] = rep
		return rep
	}
	index, err := store.LoadStore(storeRoot, "scanned", visible)
	if err != nil {
		return readableWalk{}, err
	}
	for _, scope := range index.Scopes() {
		res, err := touch.Writes(storeRoot, index, scope)
		if err != nil {
			return readableWalk{}, err
		}
		w.base.ScopesScanned++
		c := res.Coverage
		w.base.Coverage.Entries += c.Entries
		w.base.Coverage.EntriesMalformed += c.EntriesMalformed
		w.base.Coverage.EntriesUnreadable += c.EntriesUnreadable
		w.base.Coverage.Bullets += c.Bullets
		w.base.Coverage.Attributed += c.Attributed
		w.base.Coverage.MalformedTrailers += c.MalformedTrailers
		for _, s := range res.Sessions {
			if want(s.ID) {
				rep := of(s.ID)
				rep.Scopes = append(rep.Scopes, SessionInScope{Scope: res.Scope, Session: s})
			}
		}
	}
	if snap != nil {
		w.base.Damaged = snap.Damaged()
		for _, reg := range snap.Sorted() {
			// A member list naming one session twice lists the arc ONCE, with the first role — what
			// the single-id walk's `break` did before the two walks became one.
			seen := map[string]bool{}
			if !visible.Allows(reg.Home) {
				continue
			}
			for _, m := range reg.Members {
				if !want(m.Session) || seen[m.Session] {
					continue
				}
				seen[m.Session] = true
				rep := of(m.Session)
				rep.Arcs = append(rep.Arcs, SessionArc{Home: reg.Home, Slug: reg.Slug, Status: reg.Status,
					ClosingKind: reg.ClosingKind, Role: m.Role})
			}
		}
	}
	for _, rep := range w.bySession {
		rep.Coverage, rep.ScopesScanned = w.base.Coverage, w.base.ScopesScanned
		rep.ArcsConfigured, rep.Damaged = w.base.ArcsConfigured, w.base.Damaged
		slices.SortStableFunc(rep.Scopes, func(a, b SessionInScope) int {
			if a.Session.LastDate != b.Session.LastDate {
				// Newest first, and "" (all undated) after every date: a descending string compare
				// already puts "" last, because "" is less than every ISO date.
				return strings.Compare(b.Session.LastDate, a.Session.LastDate)
			}
			return strings.Compare(a.Scope, b.Scope)
		})
	}
	return w, nil
}

// Found is true when ANYTHING about this session is visible: a write in a readable scope, or
// membership of an arc homed in one. False is the one answer for "never written", "written only
// where you cannot read" and "not a session id at all".
func (r SessionAcrossReport) Found() bool { return len(r.Scopes) > 0 || len(r.Arcs) > 0 }

// Partial is true when some readable entry was never scanned, so the session's footprint is a LOWER
// bound — the same predicate `SessionsReport.Partial` states for one scope.
func (r SessionAcrossReport) Partial() bool {
	return r.Coverage.EntriesMalformed > 0 || r.Coverage.EntriesUnreadable > 0
}

// Bullets is how many distinct bullets carry this session, over every listed scope.
func (r SessionAcrossReport) Bullets() int {
	n := 0
	for _, s := range r.Scopes {
		n += len(s.Session.Bullets)
	}
	return n
}

// Actors is the union of the actors recorded with this session, sorted byte-wise — more than one
// means the id was written under several names, which is shown rather than collapsed.
func (r SessionAcrossReport) Actors() []string {
	var out []string
	for _, s := range r.Scopes {
		for _, a := range s.Session.Actors {
			if !slices.Contains(out, a) {
				out = append(out, a)
			}
		}
	}
	slices.Sort(out)
	return out
}

// DateSpan is the earliest and latest bullet date over every listed scope, "" when none is dated.
func (r SessionAcrossReport) DateSpan() (first, last string) {
	for _, s := range r.Scopes {
		if f := s.Session.FirstDate; f != "" && (first == "" || f < first) {
			first = f
		}
		if l := s.Session.LastDate; l > last {
			last = l
		}
	}
	return first, last
}
