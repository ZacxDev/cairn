package report

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/ZacxDev/cairn/internal/arcs"
	"github.com/ZacxDev/cairn/internal/store"
	"github.com/ZacxDev/cairn/internal/touch"
)

// 🔴 THE ARC ANSWERS — `arcs/<scope>` ("which arcs touched this scope") AND `arc/<home>/<slug>`
// ("this one arc") — AND THE ONE RENDERER FOR BOTH. The pod renders them through `Reader`; the Go
// client's `arcs` and `arc-show` print the pod's body VERBATIM (registrations are not in the
// cache), so there is no second rendering anywhere to drift.
//
// 🔴 GO-ONLY BY DECISION (decision 3 of `claudedocs/plan-cairn-arcs-sessions.md`), so the contract
// witnesses are the literal bodies in `arcs_test.go`; the conformance goldens for these routes are
// recorded from `cmd/cairn-server` and are CHANGE DETECTORS.
//
// 🔴 VISIBILITY IS ONE RULE, APPLIED HERE AND NOWHERE ELSE (operator decision Q1): AN ARC EXISTS
// FOR A CALLER IFF ITS HOME SCOPE IS IN `visible` — the SAME `store.ScopeSet` `recall` and
// `sessions` narrow with, passed in and never re-derived. Every OTHER scope an arc names
// (declared, inferred, a member's) is listed only when it is also in `visible`, and a hidden one
// is OMITTED, NOT COUNTED. So an arc homed in a scope the caller cannot read answers exactly what
// an arc nobody registered answers, and names neither its home nor its slug.
//
// 🔴 NO RELATIVE TIME. The plan asked for "the registration's age"; this prints the registration's
// ABSOLUTE pod-clock time instead, because an age is a function of when the request ran, and a body
// that changes every second could be neither byte-compared across surfaces nor pinned by a golden.
// The reader has the clock; the record has the instant.

// The status tokens. Each is a DISTINCT MECHANISM and no two share a phrase (the plan's coverage
// contract):
//
//   - `registrations-unconfigured`: the pod was started without `-arc-journal`; nothing was looked
//     at. The designed OFF state (Q2), never "no arc".
//   - `scope-absent` (arcs only): the scope is not readable to this caller — absent, refused, or
//     never created — indistinguishably.
//   - `no-arc-registered` (arcs only): the journal was read and no arc visible to this caller
//     declares the scope or has a member that wrote there.
//   - `arcs-listed` (arcs only): at least one, listed.
//   - `arc-unregistered` (arc only): no arc under that key is visible — never registered, or homed
//     in a scope this caller cannot read, indistinguishably.
//   - `arc-found` (arc only): the arc, shown.
const (
	StatusRegistrationsUnconfigured = "registrations-unconfigured"
	StatusNoArcRegistered           = "no-arc-registered"
	StatusArcsListed                = "arcs-listed"
	StatusArcUnregistered           = "arc-unregistered"
	StatusArcFound                  = "arc-found"
)

// The fixed lines. Pinned as WHOLE strings by `arcs_test.go`.
const (
	ArcsVisibilityLine   = "visibility: an arc is listed only when its HOME scope is readable to you — one homed in a scope you cannot read is NOT listed, even if it touched this one"
	ArcVisibilityLine    = "visibility: an arc is shown only when its HOME scope is readable to you — one homed in a scope you cannot read answers exactly like one never registered"
	ArcsProvenanceLine   = "provenance: declared = the registration names this scope · inferred: a member session wrote here; the arc did not declare this scope"
	ArcsRegistrationLine = "registration: arcs are pushed by the operator tooling at each handoff and are its own word — an arc nobody registered is not listed, and each is only as fresh as its last push"
	ArcsDamagedLine      = "⚠ the registration journal holds record(s) that could not be read; an arc registered only by such a record is NOT shown"
)

// unconfiguredBody is shared by both answers: the same off state, one sentence.
const unconfiguredBody = "REGISTRATIONS ARE NOT CONFIGURED ON THIS POD — it was started without -arc-journal / $" +
	arcs.EnvJournal + ", so no arc can be registered or shown. This is the designed off state, NOT 'no arc touched this scope'."

// ArcsReport is the answer to `arcs/<scope>`.
type ArcsReport struct {
	Status string
	Scope  string
	// Coverage is `touch.Writes`'s counts for the scope — the inference's own denominator.
	Coverage touch.Coverage
	// Sessions is how many distinct sessions wrote attributed bullets here; InArcs is how many of
	// them are members of an arc listed below.
	Sessions, InArcs int
	Damaged          bool
	Arcs             []ArcLine
}

// ArcLine is one listed arc.
type ArcLine struct {
	Home, Slug, Status, ClosingKind string
	Members                         int
	// Declared is true when the registration names the scope; otherwise the arc is INFERRED and
	// Wrote names the member sessions that wrote attributed bullets in it.
	Declared bool
	Wrote    []string
}

// Arcs derives the `arcs/<scope>` answer. `snap` is nil when the pod has no journal configured.
//
// 🔴 `visible` IS PASSED, NEVER RE-DERIVED — `Sessions`' rule. The scope's existence is the index's
// answer (refused == absent), the inference reads only the files that index names
// (`touch.Writes`), and an arc is a candidate only if `visible.Allows(home)`.
func Arcs(storeRoot, scope string, visible store.ScopeSet, snap *arcs.Snapshot) (ArcsReport, error) {
	if snap == nil {
		return ArcsReport{Status: StatusRegistrationsUnconfigured, Scope: store.NormalizeRef(scope)}, nil
	}
	index, err := store.LoadStore(storeRoot, "scanned", visible)
	if err != nil {
		return ArcsReport{}, err
	}
	res, err := touch.Writes(storeRoot, index, scope)
	if err != nil {
		var unknown *store.UnknownScopeError
		if !errors.As(err, &unknown) {
			return ArcsReport{}, err
		}
		return ArcsReport{Status: StatusScopeAbsent, Scope: store.NormalizeRef(scope)}, nil
	}
	rep := ArcsReport{Scope: res.Scope, Coverage: res.Coverage, Sessions: len(res.Sessions), Damaged: snap.Damaged()}
	wroteHere := map[string]bool{}
	for _, s := range res.Sessions {
		wroteHere[s.ID] = true
	}
	inListed := map[string]bool{}
	for _, reg := range snap.Sorted() {
		if !visible.Allows(reg.Home) {
			continue
		}
		line := ArcLine{Home: reg.Home, Slug: reg.Slug, Status: reg.Status, ClosingKind: reg.ClosingKind,
			Members: len(reg.Members), Declared: reg.Declares(res.Scope)}
		for _, m := range reg.Members {
			if wroteHere[m.Session] {
				line.Wrote = append(line.Wrote, m.Session)
			}
		}
		if !line.Declared && len(line.Wrote) == 0 {
			continue
		}
		slices.Sort(line.Wrote)
		for _, s := range line.Wrote {
			inListed[s] = true
		}
		rep.Arcs = append(rep.Arcs, line)
	}
	rep.InArcs = len(inListed)
	if len(rep.Arcs) == 0 {
		rep.Status = StatusNoArcRegistered
	} else {
		rep.Status = StatusArcsListed
	}
	return rep, nil
}

// statusWord is how an arc's status is printed. 🔴 `unknown` IS PRINTED AS ITSELF AND NOTHING
// ELSE — no gloss containing the other status words — so an `unknown` line cannot be read, or
// grepped, as `open` (Q4).
func statusWord(s string) string { return s }

// RenderText is the `arcs/<scope>` answer as the pod serves it (and the CLI prints verbatim).
func (r ArcsReport) RenderText() string {
	var b strings.Builder
	fmt.Fprintf(&b, "cairn-arcs: status=%s scope=%s\n", r.Status, r.Scope)
	b.WriteString("  " + SessionsReadsLine + "\n")
	b.WriteString("  " + SessionsAttributionLine + "\n")
	b.WriteString("  " + ArcsVisibilityLine + "\n")
	b.WriteString("  " + ArcsProvenanceLine + "\n")
	b.WriteString("  " + ArcsRegistrationLine + "\n")
	if r.Status == StatusNoArcRegistered || r.Status == StatusArcsListed {
		c := r.Coverage
		fmt.Fprintf(&b, "  attributed: %d of %d bullets in `%s/` carry a write trailer (%d have none — an arc whose sessions wrote only those is NOT inferred)\n",
			c.Attributed, c.Bullets, r.Scope, c.Bullets-c.Attributed)
		fmt.Fprintf(&b, "  sessions: %d of %d writing sessions here belong to an arc listed below\n", r.InArcs, r.Sessions)
		open, closed, unknown := 0, 0, 0
		for _, a := range r.Arcs {
			switch a.Status {
			case arcs.StatusOpen:
				open++
			case arcs.StatusClosed:
				closed++
			default:
				unknown++
			}
		}
		fmt.Fprintf(&b, "  statuses: open %d · closed %d · unknown %d (unknown is the registering tool's lack of a verdict and is never counted as open)\n",
			open, closed, unknown)
		if r.Damaged {
			b.WriteString("  " + ArcsDamagedLine + "\n")
		}
	}
	b.WriteString("\n")
	switch r.Status {
	case StatusRegistrationsUnconfigured:
		b.WriteString(unconfiguredBody)
	case StatusScopeAbsent:
		fmt.Fprintf(&b, "NO SCOPE `%s/` IS READABLE HERE. A scope that does not exist and one this credential may not read answer identically, by design; no arc is listed for it.", r.Scope)
	case StatusNoArcRegistered:
		fmt.Fprintf(&b, "NO REGISTERED ARC TOUCHED `%s/` THAT YOU CAN SEE — none declares it, and no member session of a visible arc wrote an attributed bullet here. Unregistered work, unattributed bullets and arcs homed in scopes you cannot read are all invisible to this answer.", r.Scope)
	default:
		fmt.Fprintf(&b, "arcs: %d, ordered by home scope then slug", len(r.Arcs))
		for _, a := range r.Arcs {
			prov := "declared"
			if !a.Declared {
				prov = "inferred (" + strings.Join(a.Wrote, ", ") + " wrote here)"
			}
			fmt.Fprintf(&b, "\n- %s/%s · %s · status %s · closing %s · %d member%s",
				a.Home, a.Slug, prov, statusWord(a.Status), a.ClosingKind, a.Members, plural(a.Members))
		}
	}
	return b.String()
}

// ArcReport is the answer to `arc/<home>/<slug>`.
type ArcReport struct {
	Status string
	// Reg is the arc as registered, ONLY when Status is `arc-found`.
	Reg arcs.Registration
	// DeclaredVisible is Reg.DeclaredScopes narrowed to `visible`.
	DeclaredVisible []string
	// WroteIn maps a member session to the readable scopes it wrote attributed bullets in.
	WroteIn map[string][]string
	// Coverage sums `touch.Writes`'s counts over every readable scope; Scopes is how many.
	Coverage touch.Coverage
	Scopes   int
	Damaged  bool
}

// Arc derives the `arc/<home>/<slug>` answer. `snap` is nil when no journal is configured.
//
// 🔴 THE HOME CHECK IS `visible.Allows(home)` AND IT RUNS BEFORE THE LOOKUP'S RESULT IS USED, so a
// registered arc homed in an unreadable scope reaches the SAME branch, with the same zero-value
// report, as a key nobody registered. The member inference walks only the scopes the narrowed index
// holds, so a member's write in a hidden scope is never named.
func Arc(storeRoot, home, slug string, visible store.ScopeSet, snap *arcs.Snapshot) (ArcReport, error) {
	if snap == nil {
		return ArcReport{Status: StatusRegistrationsUnconfigured}, nil
	}
	key, err := arcs.NormalizeKey(home, slug)
	reg, found := snap.Latest[key]
	if err != nil || !found || !visible.Allows(key.Home) {
		return ArcReport{Status: StatusArcUnregistered}, nil
	}
	index, err := store.LoadStore(storeRoot, "scanned", visible)
	if err != nil {
		return ArcReport{}, err
	}
	rep := ArcReport{Status: StatusArcFound, Reg: reg, WroteIn: map[string][]string{}, Damaged: snap.Damaged()}
	for _, s := range reg.DeclaredScopes {
		if visible.Allows(s) {
			rep.DeclaredVisible = append(rep.DeclaredVisible, s)
		}
	}
	members := map[string]bool{}
	for _, m := range reg.Members {
		members[m.Session] = true
	}
	for _, scope := range index.Scopes() {
		res, err := touch.Writes(storeRoot, index, scope)
		if err != nil {
			return ArcReport{}, err
		}
		rep.Scopes++
		rep.Coverage.Bullets += res.Coverage.Bullets
		rep.Coverage.Attributed += res.Coverage.Attributed
		for _, s := range res.Sessions {
			if members[s.ID] {
				rep.WroteIn[s.ID] = append(rep.WroteIn[s.ID], res.Scope)
			}
		}
	}
	for id := range rep.WroteIn {
		slices.Sort(rep.WroteIn[id])
	}
	return rep, nil
}

// RenderText is the `arc/<home>/<slug>` answer.
//
// 🔴 `arc-unregistered` AND `registrations-unconfigured` NAME NEITHER THE HOME NOR THE SLUG. The
// caller already knows what it asked; echoing it would make the refused body and the absent body
// differ by the caller's own operands, which is harmless, but naming nothing makes "this body says
// nothing about that arc" a property a test can assert by ABSENCE.
func (r ArcReport) RenderText() string {
	var b strings.Builder
	if r.Status == StatusArcFound {
		fmt.Fprintf(&b, "cairn-arc: status=%s home=%s slug=%s\n", r.Status, r.Reg.Home, r.Reg.Slug)
	} else {
		fmt.Fprintf(&b, "cairn-arc: status=%s\n", r.Status)
	}
	b.WriteString("  " + SessionsReadsLine + "\n")
	b.WriteString("  " + SessionsAttributionLine + "\n")
	b.WriteString("  " + ArcVisibilityLine + "\n")
	b.WriteString("  " + ArcsRegistrationLine + "\n")
	if r.Status == StatusArcFound && r.Damaged {
		b.WriteString("  " + ArcsDamagedLine + "\n")
	}
	b.WriteString("\n")
	switch r.Status {
	case StatusRegistrationsUnconfigured:
		b.WriteString(unconfiguredBody)
		return b.String()
	case StatusArcUnregistered:
		b.WriteString("NO SUCH ARC IS VISIBLE HERE. An arc that was never registered and one homed in a scope this credential may not read answer identically, by design — so this answer names neither the home nor the slug it was asked about.")
		return b.String()
	}
	reg := r.Reg
	fmt.Fprintf(&b, "status: %s\n", statusWord(reg.Status))
	if reg.Status == arcs.StatusUnknown {
		b.WriteString("  (the registering tool reported no verdict; this arc is not counted with any other status)\n")
	}
	fmt.Fprintf(&b, "closing condition: %s\n", reg.ClosingKind)
	reported := reg.ReportedAt
	if reported == "" {
		reported = "an unstated time"
	}
	fmt.Fprintf(&b, "registered: %s by %s · reported by the tool at %s\n", reg.RegisteredAt, reg.RegisteredBy, reported)
	fmt.Fprintf(&b, "tooling coverage: %d of %d commits carry no session id · writers: %s · readers: %s\n",
		reg.CommitsUnstamped, reg.CommitsTotal, measured(reg.WritersMeasured), measured(reg.ReadersMeasured))
	if carried := carriedCount(reg); carried > 0 {
		fmt.Fprintf(&b, "  %d member(s) carried from an earlier registration: the latest push did not measure their leg, and an unmeasured leg never erases a measured one\n", carried)
	}
	declared := "none"
	if len(r.DeclaredVisible) > 0 {
		declared = strings.Join(r.DeclaredVisible, ", ")
	}
	fmt.Fprintf(&b, "declared scopes readable to you: %s (a declared scope you cannot read is omitted, not counted)\n", declared)
	fmt.Fprintf(&b, "member writes: inferred from %d of %d bullets carrying a write trailer, across the %d scope(s) readable to you\n",
		r.Coverage.Attributed, r.Coverage.Bullets, r.Scopes)
	members := append([]arcs.Member{}, reg.Members...)
	slices.SortFunc(members, func(a, b arcs.Member) int { return strings.Compare(a.Session, b.Session) })
	fmt.Fprintf(&b, "members: %d, ordered by session id (byte-wise)", len(members))
	for _, m := range members {
		seen := m.FirstSeen
		if seen == "" {
			seen = "unknown"
		}
		wrote := "none readable to you"
		if scopes := r.WroteIn[m.Session]; len(scopes) > 0 {
			wrote = strings.Join(scopes, ", ")
		}
		line := m.Session + " · " + m.Role + " · first seen " + seen + " · wrote in: " + wrote
		if m.Carried {
			line += " · carried"
		}
		b.WriteString("\n- " + line)
	}
	return b.String()
}

func measured(ok bool) string {
	if ok {
		return "measured"
	}
	return "NOT measured"
}

func carriedCount(reg arcs.Registration) int {
	n := 0
	for _, m := range reg.Members {
		if m.Carried {
			n++
		}
	}
	return n
}

// Exit is 0 for every arc answer: each is a statement the pod made after looking (or, for
// `registrations-unconfigured`, the designed off state). "Could not look" — a journal or store the
// pod could not read — never reaches a report; it is the route's 503. No new exit code (Q5).
func (ArcsReport) Exit() int { return 0 }

// Exit — see `ArcsReport.Exit`.
func (ArcReport) Exit() int { return 0 }
