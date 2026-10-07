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
// Errors are the store's own (`*store.StoreMissingError` and the like), as for `Sessions`.
func SessionAcross(storeRoot, session string, visible store.ScopeSet, snap *arcs.Snapshot) (SessionAcrossReport, error) {
	rep := SessionAcrossReport{ID: session, ArcsConfigured: snap != nil}
	index, err := store.LoadStore(storeRoot, "scanned", visible)
	if err != nil {
		return SessionAcrossReport{}, err
	}
	for _, scope := range index.Scopes() {
		res, err := touch.Writes(storeRoot, index, scope)
		if err != nil {
			return SessionAcrossReport{}, err
		}
		rep.ScopesScanned++
		c := res.Coverage
		rep.Coverage.Entries += c.Entries
		rep.Coverage.EntriesMalformed += c.EntriesMalformed
		rep.Coverage.EntriesUnreadable += c.EntriesUnreadable
		rep.Coverage.Bullets += c.Bullets
		rep.Coverage.Attributed += c.Attributed
		rep.Coverage.MalformedTrailers += c.MalformedTrailers
		for _, s := range res.Sessions {
			if s.ID == session {
				rep.Scopes = append(rep.Scopes, SessionInScope{Scope: res.Scope, Session: s})
			}
		}
	}
	slices.SortStableFunc(rep.Scopes, func(a, b SessionInScope) int {
		if a.Session.LastDate != b.Session.LastDate {
			// Newest first, and "" (all undated) after every date: a descending string compare
			// already puts "" last, because "" is less than every ISO date.
			return strings.Compare(b.Session.LastDate, a.Session.LastDate)
		}
		return strings.Compare(a.Scope, b.Scope)
	})
	if snap != nil {
		rep.Damaged = snap.Damaged()
		for _, reg := range snap.Sorted() {
			if !visible.Allows(reg.Home) {
				continue
			}
			for _, m := range reg.Members {
				if m.Session == session {
					rep.Arcs = append(rep.Arcs, SessionArc{Home: reg.Home, Slug: reg.Slug, Status: reg.Status,
						ClosingKind: reg.ClosingKind, Role: m.Role})
					break
				}
			}
		}
	}
	return rep, nil
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
