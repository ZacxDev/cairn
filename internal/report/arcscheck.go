package report

import (
	"fmt"
	"slices"
	"strings"

	"github.com/ZacxDev/cairn/internal/arcs"
	"github.com/ZacxDev/cairn/internal/doctor"
	"github.com/ZacxDev/cairn/internal/store"
	"github.com/ZacxDev/cairn/internal/touch"
)

// 🔴 THE ARC ORPHAN CHECK — `cairn arcs --check`, served as `GET arcs/<scope>?check=1` — AND ITS ONE
// RENDERER. Slice S5 of `claudedocs/plan-cairn-arcs-sessions.md`, moved OUT of `doctor` by the plan
// (doctor is host- and credential-scoped, and three parity rows byte-compare its stdout against the
// oracle). The pod renders it; the Go client prints the body verbatim and exits by
// `ArcsCheckExit`, the ONE status→exit mapping both sides consult.
//
// 🔴 NO NEW EXIT CODE (operator decision Q5): it exits on DOCTOR'S legend, read from
// `internal/doctor`'s own constants rather than restated — 0 nothing wrong measured, 9 a finding
// measured, 10 could not look. The `{0, 9}` overlap with `create` gains a third call site and stays
// unambiguous because each call site knows its verb.
//
// 🔴 WHAT IS A FINDING, AND WHAT IS ONLY COVERAGE — THE LINE THIS FILE EXISTS TO HOLD. A check that
// is permanently non-zero trains everyone to ignore it, so a state that is NORMAL for a healthy
// registry is never a finding:
//
//   - FINDINGS (exit 9), each a state the registry should not be in:
//     `journal-damaged` — the journal holds a complete record that did not parse, or a torn tail,
//     so an arc registered only by that record is invisible to every answer;
//     `home-scope-absent` — an arc's HOME scope is readable to the caller by grant and no such
//     scope exists in the store;
//     `declared-scope-absent` — the same, for one of its other declared scopes.
//   - COVERAGE (printed, never a finding): a member session with no attributed write in any scope
//     readable to the caller, and a writing session in no arc.
//
// ⚠ THE FIRST OF THOSE COVERAGE NUMBERS IS A DELIBERATE DEPARTURE FROM THE PLAN, which made it a
// `10` ("unmeasured"). Measured against the registry's own model: an arc's members are whoever the
// tooling saw stamp a COMMIT (`Claude-Session-Id:`, the writer roles) or resume the doc (a
// transcript, the `resumed` role) — neither is a cairn write, so a member with no entry trailer is
// the ORDINARY state, not an anomaly. As a `10` it would hold the check at "could not look" for
// nearly every arc forever, which is the plan's own argument against making unarced sessions a
// finding, and it would mask a real 0. It is printed as a count instead.
//
// 🔴 REFUSED == ABSENT IS KEPT, AND THAT IS WHY THE ABSENT-SCOPE FINDINGS ARE NARROWER THAN "A
// DECLARED SCOPE THAT NO LONGER EXISTS". A scope the caller may NOT read and a scope that does not
// exist must answer identically, so only a scope `visible.Allows` — a name the caller holds a read
// grant for — can be reported absent; a declared scope outside that set is neither checked nor
// counted (the `arc/<home>/<slug>` rule). Reporting an allowed name's absence leaks nothing:
// `recall` on that name already answers `scope-absent`. ⚠ The cost, named: a deleted scope directory
// that no token row names is not in a BARE row's enumerated set at all, so for that principal the
// arc homed there is simply invisible, and this check cannot see it.

// The two statuses of a check that looked. `registrations-unconfigured` (no journal) is the third.
// 🔴 NEITHER IS SHARED WITH THE `arcs/<scope>` LISTING, ON PURPOSE: a pod that predates the check
// ignores `?check=1` and answers a listing, and a client that accepted a listing status in check
// mode would print that listing and exit 0 — a green from a check that never ran.
const (
	StatusArcsCheckClean    = "arcs-check-clean"
	StatusArcsCheckFindings = "arcs-check-findings"
)

// The finding kinds, in the order they sort.
const (
	FindingJournalDamaged      = "journal-damaged"
	FindingHomeScopeAbsent     = "home-scope-absent"
	FindingDeclaredScopeAbsent = "declared-scope-absent"
)

// The fixed lines. Pinned as WHOLE strings by `arcscheck_test.go`.
const (
	ArcsCheckVisibilityLine = "visibility: only arcs whose HOME scope is readable to you are checked · a declared scope you cannot read is neither checked nor counted"
	ArcsCheckFindingsLine   = "findings (exit 9): a home or declared scope readable to you that does not exist in the store · journal record(s) that could not be read"
	ArcsCheckNotFindingLine = "not findings: a member session with no attributed write (members come from commits and transcripts, not entry trailers) and a writing session in no arc (arcs are registered only from now on) — both are coverage, below"
)

// ArcsCheckExit is the exit code for a check status, and whether the status IS a check answer.
//
// 🔴 THE ONE MAPPING. The pod stamps it into `X-Store-Exit` and the client recomputes it from
// `X-Store-Status`, so a pod and a client can never disagree about what a status means — and a
// status this function does not know (an `arcs/<scope>` listing from a pod that ignored `?check=1`)
// is `ok == false`, which the client turns into "could not look" rather than a 0.
func ArcsCheckExit(status string) (code int, ok bool) {
	switch status {
	case StatusArcsCheckClean:
		return doctor.ExitOK, true
	case StatusArcsCheckFindings:
		return doctor.ExitProblem, true
	case StatusRegistrationsUnconfigured:
		return doctor.ExitUnmeasured, true
	}
	return doctor.ExitUnmeasured, false
}

// ArcFinding is one finding. Scope is the absent scope (empty for `journal-damaged`, which names
// no arc).
type ArcFinding struct{ Kind, Home, Slug, Scope string }

// ArcsCheckReport is the answer to `arcs/<scope>?check=1`.
type ArcsCheckReport struct {
	Status string
	// Scope is the path scope, normalized. Without AllScopes only arcs HOMED there are checked.
	Scope     string
	AllScopes bool
	// Checked counts the arcs examined, split by status (unknown never counted as open, Q4).
	Checked, Open, Closed, Unknown int
	// Members counts member sessions over the checked arcs (one per arc membership); Wrote is how
	// many of them wrote an attributed bullet in a scope readable to the caller.
	Members, MembersWrote int
	// Sessions is the distinct writing sessions in the measured scope(s) — the path scope, or every
	// readable scope with AllScopes; InArcs is how many are a member of ANY arc visible to the
	// caller (not only the checked ones).
	Sessions, InArcs int
	// Coverage sums `touch.Writes` over every readable scope; Scopes is how many.
	Coverage touch.Coverage
	Scopes   int
	Findings []ArcFinding
}

// ArcsCheck derives the check. `snap` is nil when the pod has no journal configured.
//
// 🔴 `visible` IS PASSED, NEVER RE-DERIVED — the rule every arc answer follows. An arc is checked
// only if `visible.Allows(home)`; a scope's existence is the narrowed index's answer; and a
// declared scope is examined only if `visible.Allows` it.
func ArcsCheck(storeRoot, scope string, allScopes bool, visible store.ScopeSet, snap *arcs.Snapshot) (ArcsCheckReport, error) {
	rep := ArcsCheckReport{Scope: store.NormalizeRef(scope), AllScopes: allScopes}
	if snap == nil {
		rep.Status = StatusRegistrationsUnconfigured
		return rep, nil
	}
	index, err := store.LoadStore(storeRoot, "scanned", visible)
	if err != nil {
		return ArcsCheckReport{}, err
	}
	exists := map[string]bool{}
	wroteReadable := map[string]bool{}
	measured := map[string]bool{}
	for _, s := range index.Scopes() {
		res, err := touch.Writes(storeRoot, index, s)
		if err != nil {
			return ArcsCheckReport{}, err
		}
		exists[res.Scope] = true
		rep.Scopes++
		rep.Coverage.Bullets += res.Coverage.Bullets
		rep.Coverage.Attributed += res.Coverage.Attributed
		for _, ses := range res.Sessions {
			wroteReadable[ses.ID] = true
			if allScopes || res.Scope == rep.Scope {
				measured[ses.ID] = true
			}
		}
	}
	if snap.Damaged() {
		rep.Findings = append(rep.Findings, ArcFinding{Kind: FindingJournalDamaged})
	}
	inAnyArc := map[string]bool{}
	for _, reg := range snap.Sorted() {
		if !visible.Allows(reg.Home) {
			continue
		}
		for _, m := range reg.Members {
			inAnyArc[m.Session] = true
		}
		if !allScopes && reg.Home != rep.Scope {
			continue
		}
		rep.Checked++
		switch reg.Status {
		case arcs.StatusOpen:
			rep.Open++
		case arcs.StatusClosed:
			rep.Closed++
		default:
			rep.Unknown++
		}
		for _, m := range reg.Members {
			rep.Members++
			if wroteReadable[m.Session] {
				rep.MembersWrote++
			}
		}
		if !exists[reg.Home] {
			rep.Findings = append(rep.Findings, ArcFinding{Kind: FindingHomeScopeAbsent, Home: reg.Home, Slug: reg.Slug, Scope: reg.Home})
		}
		for _, d := range reg.DeclaredScopes {
			if d == reg.Home || !visible.Allows(d) || exists[d] {
				continue
			}
			rep.Findings = append(rep.Findings, ArcFinding{Kind: FindingDeclaredScopeAbsent, Home: reg.Home, Slug: reg.Slug, Scope: d})
		}
	}
	rep.Sessions = len(measured)
	for id := range measured {
		if inAnyArc[id] {
			rep.InArcs++
		}
	}
	// Already in a deterministic order (journal finding first, then by home, slug, and the declared
	// scopes' own sorted order); sorted again so the order is a property of this function rather
	// than of `Sorted` and `declaredScopes` staying as they are.
	order := map[string]int{FindingJournalDamaged: 0, FindingHomeScopeAbsent: 1, FindingDeclaredScopeAbsent: 2}
	slices.SortStableFunc(rep.Findings, func(a, b ArcFinding) int {
		if a.Kind == FindingJournalDamaged || b.Kind == FindingJournalDamaged {
			return order[a.Kind] - order[b.Kind]
		}
		for _, c := range []int{strings.Compare(a.Home, b.Home), strings.Compare(a.Slug, b.Slug), order[a.Kind] - order[b.Kind], strings.Compare(a.Scope, b.Scope)} {
			if c != 0 {
				return c
			}
		}
		return 0
	})
	if len(rep.Findings) > 0 {
		rep.Status = StatusArcsCheckFindings
	} else {
		rep.Status = StatusArcsCheckClean
	}
	return rep, nil
}

// Exit — see `ArcsCheckExit`.
func (r ArcsCheckReport) Exit() int {
	code, _ := ArcsCheckExit(r.Status)
	return code
}

// RenderText is the check as the pod serves it and the CLI prints it.
func (r ArcsCheckReport) RenderText() string {
	var b strings.Builder
	what := "arcs homed in `" + r.Scope + "/`"
	if r.AllScopes {
		what = "every arc visible to you"
	}
	code, _ := ArcsCheckExit(r.Status)
	fmt.Fprintf(&b, "cairn-arcs-check: status=%s scope=%s exit=%d\n", r.Status, r.Scope, code)
	fmt.Fprintf(&b, "  checked: %s\n", what)
	b.WriteString("  " + SessionsReadsLine + "\n")
	b.WriteString("  " + SessionsAttributionLine + "\n")
	b.WriteString("  " + ArcsCheckVisibilityLine + "\n")
	b.WriteString("  " + ArcsCheckFindingsLine + "\n")
	b.WriteString("  " + ArcsCheckNotFindingLine + "\n")
	if r.Status == StatusRegistrationsUnconfigured {
		b.WriteString("\n")
		b.WriteString(unconfiguredBody)
		b.WriteString(" Nothing was checked (exit 10: could not look).")
		return b.String()
	}
	fmt.Fprintf(&b, "  arcs checked: %d (open %d · closed %d · unknown %d)\n", r.Checked, r.Open, r.Closed, r.Unknown)
	fmt.Fprintf(&b, "  members: %d of %d member sessions wrote an attributed bullet in a scope readable to you (%d did not — coverage, not a finding)\n",
		r.MembersWrote, r.Members, r.Members-r.MembersWrote)
	where := "`" + r.Scope + "/`"
	if r.AllScopes {
		where = "the scopes readable to you"
	}
	fmt.Fprintf(&b, "  sessions: %d of %d writing sessions in %s belong to an arc visible to you (%d in no arc — coverage, not a finding)\n",
		r.InArcs, r.Sessions, where, r.Sessions-r.InArcs)
	fmt.Fprintf(&b, "  attributed: %d of %d bullets across the %d scope(s) readable to you carry a write trailer\n",
		r.Coverage.Attributed, r.Coverage.Bullets, r.Scopes)
	b.WriteString("\n")
	if len(r.Findings) == 0 {
		fmt.Fprintf(&b, "NO FINDING: %d arc(s) checked, and every home and declared scope readable to you exists. This says nothing about arcs homed in scopes you cannot read.", r.Checked)
		return b.String()
	}
	fmt.Fprintf(&b, "findings: %d", len(r.Findings))
	for _, f := range r.Findings {
		switch f.Kind {
		case FindingJournalDamaged:
			b.WriteString("\n- journal-damaged · the registration journal holds record(s) that could not be read; an arc registered only by such a record was NOT checked")
		case FindingHomeScopeAbsent:
			fmt.Fprintf(&b, "\n- home-scope-absent · %s/%s · its home scope `%s/` is readable to you and does not exist in the store", f.Home, f.Slug, f.Scope)
		case FindingDeclaredScopeAbsent:
			fmt.Fprintf(&b, "\n- declared-scope-absent · %s/%s · it declares `%s/`, which is readable to you and does not exist in the store", f.Home, f.Slug, f.Scope)
		}
	}
	return b.String()
}
