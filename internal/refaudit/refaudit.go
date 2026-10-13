// Package refaudit is the deterministic auditor of decisions 6–9 of
// `claudedocs/plan-cairn-scope-refs.md`: for every entry in a scope it reports which CHECKABLE claims
// hold at a fetched commit of the scope's declared code sources, which do not, and which it could not
// check — never folding those three together, and never editing an entry.
//
// 🔴 A LIBRARY FIRST (decision 9). It takes plain values and two interfaces — [GitReader] and
// [PRReader] — and returns plain findings; `cairn audit-refs` is its first caller. Anything that needs
// judgement (does a file still SAY what the entry claims, does a commit IMPLEMENT it) stays out of the
// exit code (decision 7).
//
// 🔴 THE POD AND THE BROWSER SURFACE NEVER IMPORT THIS PACKAGE (`internal/depspolicy`'s ban): the
// auditor runs on an operator host with the operator's credentials, and fetching is never a server's
// job (clause e of the plan's closing condition).
package refaudit

import (
	"fmt"
	"sort"
	"strings"
)

// The claim kinds.
const (
	KindPath   = "path"
	KindCommit = "commit"
	KindPR     = "pr"
	KindSymbol = "symbol"
)

// The states a check can emit. Every one appears exactly once in [Severity], and
// `TestTheSeverityTableIsExactlyTheEmittedStates` holds that key set equal to [EmittedStates].
const (
	HoldsRoot     = "holds(root)"
	HoldsSubpath  = "holds(subpath)"
	Missing       = "missing"
	NotPathShaped = "unchecked: not path-shaped"

	OnBranch        = "on-branch"
	ExistsOffBranch = "exists-off-branch"
	NotReachable    = "not-reachable"
	Ambiguous       = "ambiguous"

	PROpen           = "open"
	PRMerged         = "merged"
	PRClosedUnmerged = "closed-unmerged"
	PRNotFound       = "not-found"
	PRUnchecked      = "unchecked: rate-limited / no token"
	OutsideSources   = "outside-sources"

	SymbolPresent     = "present"
	SymbolAbsent      = "absent-from-file"
	SymbolFileMissing = "file-missing"

	HostNotMapped     = "unchecked: host not mapped"
	RemoteUnreachable = "unchecked: remote unreachable"

	// NotInFetched is a claim no FETCHED source holds while another declared source was never looked
	// at — what `missing`, `file-missing` or `absent-from-file` would have been with every source read.
	// 🔴 A STALE VERDICT MEANS EVERY DECLARED SOURCE WAS READ (operator decision on #259 r1 🟡2): a
	// claim holds if ANY source holds it, so the unread one may, and calling it stale would be a verdict
	// the auditor never measured.
	NotInFetched = "unchecked: not in the sources fetched"
)

// Class is a state's effect on the counts and the exit code.
type Class string

// The CLOSED set of classes (decision 6's table).
const (
	ClassHolds         Class = "holds"
	ClassInformational Class = "informational"
	ClassStale         Class = "stale"
	ClassCouldNotLook  Class = "could-not-look"
	ClassByDesign      Class = "unchecked-by-design"
)

// Severity is THE ONE classification of every state. A state missing here is a state nothing can
// count, which is why the table's key set is tested equal to the emitted set rather than read.
var Severity = map[string]Class{
	HoldsRoot: ClassHolds, HoldsSubpath: ClassHolds, OnBranch: ClassHolds, SymbolPresent: ClassHolds,

	ExistsOffBranch: ClassInformational, PROpen: ClassInformational, PRMerged: ClassInformational,
	PRClosedUnmerged: ClassInformational, OutsideSources: ClassInformational,

	Missing: ClassStale, SymbolAbsent: ClassStale, SymbolFileMissing: ClassStale, PRNotFound: ClassStale,

	NotReachable: ClassCouldNotLook, Ambiguous: ClassCouldNotLook, PRUnchecked: ClassCouldNotLook,
	HostNotMapped: ClassCouldNotLook, RemoteUnreachable: ClassCouldNotLook, NotInFetched: ClassCouldNotLook,

	NotPathShaped: ClassByDesign,
}

// EmittedStates is every state a check in this package can produce, spelled where the checks are.
// It exists so the severity table's key set can be compared with something other than itself.
var EmittedStates = []string{
	HoldsRoot, HoldsSubpath, Missing, NotPathShaped,
	OnBranch, ExistsOffBranch, NotReachable, Ambiguous,
	PROpen, PRMerged, PRClosedUnmerged, PRNotFound, PRUnchecked, OutsideSources,
	SymbolPresent, SymbolAbsent, SymbolFileMissing,
	HostNotMapped, RemoteUnreachable, NotInFetched,
}

// The four SCOPE-level could-not-look reasons, each exit 10.
const (
	ReasonUndeclared   = "sources=undeclared"
	ReasonUnconfigured = "sources-unconfigured"
	ReasonUnreadable   = "sources-unreadable"
	ReasonCheckedZero  = "checked=0"
)

// The exit codes: doctor's 0/9/10, no new one (decision 8).
const (
	ExitClean        = 0
	ExitStale        = 9
	ExitCouldNotLook = 10
)

// Claim is one checkable statement an entry makes.
type Claim struct {
	Entry string // the entry's ref
	Kind  string
	// Value is what is checked: a normalised path, a lowercased sha, `owner/repo#N`, or
	// `path#Symbol`.
	Value string
}

// Finding is one claim's result, or one source's.
type Finding struct {
	Claim
	State string
	// Source is the canonical source the state was measured against ("" for a PR, a by-design
	// row, or a claim no source could be looked at for).
	Source string
	// At is the fetched commit the state is a fact about ("" when nothing was fetched).
	At string
}

// SourceResult is what fetching one declared source produced.
type SourceResult struct {
	Source string
	// State is "" when the source was fetched, else HostNotMapped or RemoteUnreachable.
	State  string
	Detail string
	At     string // the declared branch's commit, when fetched
}

// Result is one scope's audit.
type Result struct {
	Sources  []string
	Fetched  []SourceResult
	Findings []Finding
	// ScopeReason is a scope-level could-not-look reason, or "".
	ScopeReason string
}

// Counts is the denominator the output prints.
func (r Result) Counts() (checked, stale, unchecked int) {
	for _, f := range r.Findings {
		switch Severity[f.State] {
		case ClassHolds, ClassInformational:
			checked++
		case ClassStale:
			checked++
			stale++
		case ClassCouldNotLook, ClassByDesign:
			unchecked++
		}
	}
	for _, s := range r.Fetched {
		if s.State != "" {
			unchecked++
		}
	}
	return checked, stale, unchecked
}

// Exit is decision 6's mixed case: 9 if anything is stale; else 10 if anything — a claim, a source,
// or the scope — could not be looked at, or nothing was checked; else 0. A source-dependent claim can
// only BE stale when every declared source was read ([NotInFetched]), so "stale beside an unlooked
// source" is reachable here only through a PR's `not-found`, which no git source answers.
func (r Result) Exit() int {
	if r.ScopeReason != "" {
		return ExitCouldNotLook
	}
	checked, stale, _ := r.Counts()
	if stale > 0 {
		return ExitStale
	}
	if r.unlookedSource() {
		return ExitCouldNotLook
	}
	for _, f := range r.Findings {
		if Severity[f.State] == ClassCouldNotLook {
			return ExitCouldNotLook
		}
	}
	if checked == 0 {
		return ExitCouldNotLook
	}
	return ExitClean
}

// unlookedSource is whether any declared source was never looked at (unmapped, or not fetched): part
// of the declaration was never checked, so a clean claim set beside it is still could-not-look.
func (r Result) unlookedSource() bool {
	for _, s := range r.Fetched {
		if s.State != "" {
			return true
		}
	}
	return false
}

// Reason is the scope-level reason the exit names: the declared one, or `checked=0`.
func (r Result) Reason() string {
	if r.ScopeReason != "" {
		return r.ScopeReason
	}
	if checked, _, _ := r.Counts(); checked == 0 {
		return ReasonCheckedZero
	}
	return ""
}

// Render is the text output: `sources=`, the counts, then one line per source problem and finding,
// in a stable order.
func (r Result) Render() string {
	var b strings.Builder
	switch {
	case len(r.Sources) > 0:
		b.WriteString("sources=" + strings.Join(r.Sources, ",") + "\n")
	case r.ScopeReason == ReasonUnconfigured || r.ScopeReason == ReasonUnreadable:
		b.WriteString(r.ScopeReason + "\n")
	default:
		b.WriteString(ReasonUndeclared + "\n")
	}
	checked, stale, unchecked := r.Counts()
	fmt.Fprintf(&b, "checked=%d stale=%d unchecked=%d\n", checked, stale, unchecked)
	if reason := r.Reason(); reason != "" {
		b.WriteString("could-not-look reason=" + reason + "\n")
	}
	for _, s := range r.Fetched {
		if s.State == "" {
			fmt.Fprintf(&b, "source %s at=%s\n", s.Source, s.At)
			continue
		}
		fmt.Fprintf(&b, "source %s state=%q detail=%q\n", s.Source, s.State, s.Detail)
	}
	findings := append([]Finding(nil), r.Findings...)
	sort.SliceStable(findings, func(i, j int) bool {
		a, c := findings[i], findings[j]
		if a.Entry != c.Entry {
			return a.Entry < c.Entry
		}
		if a.Kind != c.Kind {
			return a.Kind < c.Kind
		}
		return a.Value < c.Value
	})
	for _, f := range findings {
		fmt.Fprintf(&b, "%s %s %s %q state=%q", Severity[f.State], f.Entry, f.Kind, f.Value, f.State)
		if f.Source != "" {
			fmt.Fprintf(&b, " source=%s at=%s", f.Source, f.At)
		}
		b.WriteString("\n")
	}
	return b.String()
}
