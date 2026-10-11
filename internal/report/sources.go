package report

import (
	"fmt"
	"strings"

	"github.com/ZacxDev/cairn/internal/codesrc"
	"github.com/ZacxDev/cairn/internal/store"
)

// 🔴 THE SOURCES ANSWER — `sources/<scope>`, "which code does this scope describe" — AND ITS ONE
// RENDERER (`claudedocs/plan-cairn-scope-refs.md`, decision 10). The pod renders it through
// `Reader`; nothing else renders a declaration, so there is no second rendering to drift.
//
// 🔴 GO-ONLY BY DECISION (the arcs precedent): `server/server.py` does not serve it, the corpus rows
// addressing it are `go_only` CHANGE DETECTORS recorded from `cmd/cairn-server`, and the contract
// witnesses are the literal bodies in `internal/api`'s sources tests — which also pin the two
// states the one-boot corpus cannot send (unconfigured, and the 503 for an unreadable journal).
//
// 🔴 VISIBILITY IS `visible` AND NOTHING ELSE. The scope's existence is the narrowed index's
// answer, so a scope this caller may not read and one that does not exist answer BYTE-IDENTICALLY
// (`scope-absent`), and the journal's record for a hidden scope is never consulted.
//
// 🔴 THE KEY IS `codesrc.Key` OF THE PATH SCOPE — the folded directory name (the operator's answer
// to the plan's Q17) — never a control-plane scope ID, which the browser surface and the pod mint
// from different authorities.
//
// 🔴 JOURNAL DAMAGE IS NOT ON THE WIRE AT ALL — neither a count nor a flag. A damaged line cannot be
// attributed to a scope (it did not parse), so anything the body said about it would be a statement
// about EVERY scope: a count leaks activity in scopes the caller cannot read, and a flag (round 0
// shipped `damaged=yes`) is journal-wide, permanent after one sealed torn write, and tells a reader
// of a scope the damage never touched that an older declaration "may be shown". The counts go to the
// pod's stderr (`sourcesShow`); `TestAHiddenScopesDamagedLineChangesNothingOnTheWire` pins the
// absence.

// The status tokens. Each is a distinct mechanism:
//
//   - `sources-unconfigured`: the pod was started without $CAIRN_SOURCE_JOURNAL; nothing was
//     looked at. The designed OFF state, never "undeclared".
//   - `scope-absent`: the scope is not readable to this caller — absent or refused,
//     indistinguishably. (The token `recall` and `arcs` already use for the same fact.)
//   - `sources-undeclared`: the journal was read (or is absent) and holds no declaration for the
//     scope, or its latest declaration is the empty list.
//   - `sources-declared`: at least one source, listed in declaration order (the first is the
//     primary).
const (
	StatusSourcesUnconfigured = "sources-unconfigured"
	StatusSourcesUndeclared   = "sources-undeclared"
	StatusSourcesDeclared     = "sources-declared"
)

// The fixed lines. Pinned as WHOLE strings by the literal-body tests.
const (
	SourcesProvenanceLine = "provenance: a declaration is set in the browser by an admin of the scope and is that admin's word — the pod checks its grammar, never that the code exists"
)

// SourcesUnconfiguredBody is the off state's sentence.
const SourcesUnconfiguredBody = "CODE SOURCES ARE NOT CONFIGURED ON THIS POD — it was started without $" +
	codesrc.EnvJournal + ", so no declaration can be shown. This is the designed off state, NOT 'this scope declares no sources'."

// SourcesReport is the answer to `sources/<scope>`.
type SourcesReport struct {
	Status string
	// Scope is `codesrc.Key` of the path scope — the journal's key, and the index's.
	Scope string
	// JournalAbsent is the journal's EMPTY state: configured, never written.
	JournalAbsent bool
	// Record is the scope's latest declaration, when it has one (an empty list included).
	Record *codesrc.Record
}

// Sources derives the `sources/<scope>` answer. `snap` is nil when the pod has no journal
// configured.
func Sources(storeRoot, scope string, visible store.ScopeSet, snap *codesrc.Snapshot) (SourcesReport, error) {
	key := codesrc.Key(scope)
	if snap == nil {
		return SourcesReport{Status: StatusSourcesUnconfigured, Scope: key}, nil
	}
	index, err := store.LoadStore(storeRoot, "scanned", visible)
	if err != nil {
		return SourcesReport{}, err
	}
	if !index.HasScope(key) {
		return SourcesReport{Status: StatusScopeAbsent, Scope: key}, nil
	}
	rep := SourcesReport{Status: StatusSourcesUndeclared, Scope: key, JournalAbsent: snap.Missing}
	if rec, ok := snap.Latest[key]; ok {
		rep.Record = &rec
		if len(rec.Sources) > 0 {
			rep.Status = StatusSourcesDeclared
		}
	}
	return rep, nil
}

// Exit is 0 for every sources answer: a declaration (or its absence) is a fact about the scope,
// and the caller decides what an undeclared scope means — the auditor maps it to 10, a plain read
// does not fail on it. The arc answers' rule.
func (SourcesReport) Exit() int { return 0 }

// RenderText is the body.
func (r SourcesReport) RenderText() string {
	var b strings.Builder
	fmt.Fprintf(&b, "cairn-sources: status=%s scope=%s\n", r.Status, r.Scope)
	switch r.Status {
	case StatusSourcesUnconfigured:
		b.WriteString("\n" + SourcesUnconfiguredBody)
		return b.String()
	case StatusScopeAbsent:
		fmt.Fprintf(&b, "\nNO SCOPE `%s/` IS READABLE HERE. A scope that does not exist and one this credential may not read answer identically, by design; no sources are shown for it.", r.Scope)
		return b.String()
	}
	b.WriteString("  " + SourcesProvenanceLine + "\n")
	if r.JournalAbsent {
		b.WriteString("  journal=absent\n")
	} else {
		b.WriteString("  journal=present\n")
	}
	if r.Record != nil {
		fmt.Fprintf(&b, "  set_by=%s set_at=%s revision=%s\n", r.Record.SetBy, r.Record.SetAt, r.Record.Revision)
	}
	b.WriteString("\n")
	if r.Status == StatusSourcesUndeclared {
		b.WriteString("sources=undeclared")
		return b.String()
	}
	fmt.Fprintf(&b, "sources=declared count=%d", len(r.Record.Sources))
	for _, s := range r.Record.Sources {
		b.WriteString("\nsource=" + s)
	}
	return b.String()
}
