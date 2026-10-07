package report

import (
	"slices"

	"github.com/ZacxDev/cairn/internal/arcs"
	"github.com/ZacxDev/cairn/internal/store"
	"github.com/ZacxDev/cairn/internal/touch"
)

// 🔴 "EVERY ARC YOU CAN SEE, AND WHEN ANY MEMBER LAST WROTE WHERE YOU CAN READ" — THE BROWSER'S
// ARCS-FIRST PAGE (`GET /arcs`), AND A STRUCTURED ANSWER ONLY. There is no `RenderText` here and no
// pod route: nothing in the CLI or the conformance corpus prints this, so no printed byte moved to
// add it. It lives in this package for `SessionAcross`'s one reason — the arc rule (an arc exists
// for a caller iff its HOME scope is readable, operator decision Q1) is applied in this package and
// nowhere else, and this answer lists arcs.
//
// 🔴 ONE WHOLE-STORE WALK, NOT ONE PER ARC (decision 10 of `claudedocs/plan-cairn-arcs-presence.md`).
// The index is narrowed to `visible`, `touch.Writes` runs once per readable scope, and each session's
// newest `LastDate` across those scopes is kept; an arc's newest member bullet is then a map lookup
// per member. So a member's write in a scope the caller cannot read never reaches the answer — it is
// not in the narrowed index — and `TestAMemberBulletInAnUnreadableScopeDoesNotMoveTheArc` pins that
// as a relationship between two viewers over one store.
//
// ⚠ WHAT THIS DOES NOT DECIDE: "live", the 14-day window, the future-date clamp and the order. Each
// of those is a function of the reader's CLOCK, and this package's rule is that a reader has the
// clock and the record has the instant (the "NO RELATIVE TIME" block in `arcs.go`). So the newest
// bullet date is returned AS WRITTEN — possibly in the future, because a bullet's date is the
// writer's word on a `put` — and the browser clamps it.

// ArcsAcrossReport is every registration homed in a readable scope, plus each one's newest
// attributed member bullet over the readable scopes.
type ArcsAcrossReport struct {
	// Configured is false when no journal was handed in — the designed off state — so an empty Arcs
	// list can be told apart from "nothing was looked at".
	Configured bool
	// Damaged is the journal's own `Damaged()`: an arc registered only by an unreadable record is
	// not listed.
	Damaged bool
	// Arcs are in `arcs.Snapshot.Sorted` order (home, then slug).
	Arcs []ArcAcross
	// ScopesScanned is how many readable scopes the member walk read.
	ScopesScanned int
	// Coverage SUMS `touch.Writes`'s counts over every readable scope — the walk's own denominator:
	// a bullet with no trailer names nobody, so it can never move an arc.
	Coverage touch.Coverage
}

// ArcAcross is one listed arc. Flat fields rather than the whole `arcs.Registration`, so a caller
// cannot render the UNNARROWED declared-scope list by reaching for the record.
type ArcAcross struct {
	Home, Slug, Status, ClosingKind string
	// Members is the registration's member count.
	Members int
	// DeclaredVisible is the declared scopes NARROWED to `visible` — a hidden one is omitted, not
	// counted (the rule `ArcLine.DeclaredVisible` states).
	DeclaredVisible []string
	// RegisteredAt is the pod-clock RFC 3339 stamp of the LATEST registration — always present on
	// an applied record. ReportedAt is the tooling's own word, optional, on the tooling's clock.
	RegisteredAt, ReportedAt string
	// NewestMemberBullet is the newest `touch.Session.LastDate` of any member session over the
	// readable scopes, AS WRITTEN (`YYYY-MM-DD`, possibly in the future), or "" when no member wrote
	// a dated, attributed bullet anywhere the caller can read.
	//
	// 🔴 ATTRIBUTION IS SELF-DECLARED, AND THAT IS AN ACCEPTED COST (operator decision O1): anybody
	// who can write a readable scope can move this by appending a bullet whose trailer NAMES a member
	// session. It reorders what the reader could already see; it never makes an arc visible.
	NewestMemberBullet string
}

// ArcsAcross derives the answer. `snap` is nil when no journal is configured; the store is then
// not read at all.
//
// Errors are the store's own (`*store.StoreMissingError` and the like), as for `SessionAcross`.
func ArcsAcross(storeRoot string, visible store.ScopeSet, snap *arcs.Snapshot) (ArcsAcrossReport, error) {
	if snap == nil {
		return ArcsAcrossReport{}, nil
	}
	rep := ArcsAcrossReport{Configured: true, Damaged: snap.Damaged()}
	index, err := store.LoadStore(storeRoot, "scanned", visible)
	if err != nil {
		return ArcsAcrossReport{}, err
	}
	newest := map[string]string{}
	for _, scope := range index.Scopes() {
		res, err := touch.Writes(storeRoot, index, scope)
		if err != nil {
			return ArcsAcrossReport{}, err
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
			// ISO dates compare correctly as strings, and "" is below every one of them.
			if s.LastDate > newest[s.ID] {
				newest[s.ID] = s.LastDate
			}
		}
	}
	for _, reg := range snap.Sorted() {
		if !visible.Allows(reg.Home) {
			continue
		}
		line := ArcAcross{Home: reg.Home, Slug: reg.Slug, Status: reg.Status, ClosingKind: reg.ClosingKind,
			Members: len(reg.Members), RegisteredAt: reg.RegisteredAt, ReportedAt: reg.ReportedAt}
		for _, d := range reg.DeclaredScopes {
			if visible.Allows(d) {
				line.DeclaredVisible = append(line.DeclaredVisible, d)
			}
		}
		slices.Sort(line.DeclaredVisible)
		for _, m := range reg.Members {
			if d := newest[m.Session]; d > line.NewestMemberBullet {
				line.NewestMemberBullet = d
			}
		}
		rep.Arcs = append(rep.Arcs, line)
	}
	return rep, nil
}

// Partial is true when some readable entry was never scanned, so a member's newest bullet may be
// newer than the one shown — the predicate `SessionAcrossReport.Partial` states.
func (r ArcsAcrossReport) Partial() bool {
	return r.Coverage.EntriesMalformed > 0 || r.Coverage.EntriesUnreadable > 0
}
