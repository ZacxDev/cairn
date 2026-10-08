package presence

import (
	"slices"
	"testing"

	"github.com/ZacxDev/cairn/internal/depspolicy"
)

// TestOnlyTheBrowserProgramImportsPresence is the ledger of every package that imports
// `internal/presence` (non-test imports, every package under `cmd/` and `internal/`, through
// `depspolicy.ImportGraph` — the walk the dependency ban already trusts).
//
// 🔴 PRESENCE TOKENS AUTHENTICATE NOTHING BUT THE TWO AGENT ROUTES BECAUSE NOTHING ELSE CAN
// REACH THIS PACKAGE. Wiring it into `internal/identity`, `internal/api`, `internal/ui` or the
// pod's program is the change that would let a presence token mean something there, and it
// GROWS this ledger. S4/S5 will add `internal/ui` here deliberately; that is the edit that has to
// say so. Fails on SHRINK too, so a stale ledger cannot read as coverage.
func TestOnlyTheBrowserProgramImportsPresence(t *testing.T) {
	root, err := depspolicy.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	graph, err := depspolicy.ImportGraph(root, true)
	if err != nil {
		t.Fatal(err)
	}
	const self = depspolicy.ModulePath + "/internal/presence"
	if _, ok := graph[self]; !ok || len(graph) < 10 {
		t.Fatalf("POSITIVE CONTROL: the walk saw %d package(s) and presence=%v, so the ledger below is about nothing", len(graph), ok)
	}
	var importers []string
	for pkg, imports := range graph {
		if slices.Contains(imports, self) {
			importers = append(importers, pkg)
		}
	}
	slices.Sort(importers)
	want := []string{depspolicy.ModulePath + "/cmd/cairn-ui"}
	if !slices.Equal(importers, want) {
		t.Fatalf("internal/presence is imported by %v, want exactly %v. A new importer is a new place a "+
			"presence token could come to mean something; add it here only as a deliberate, reviewed decision", importers, want)
	}
}
