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
// GROWS this ledger. Fails on SHRINK too, so a stale ledger cannot read as coverage.
//
// 🔴 `internal/ui` WAS ADDED BY S4, DELIBERATELY, AND FOR READING ONLY. The browser renders presence
// badges through `presence.Store.For` alone (`internal/ui/presence.go`); it never parses a token,
// never reaches `Agent`, and adds no route. A presence token still authenticates nothing on the
// browser surface because `identity.Backends` does not know the kind — the importer set grew, the
// set of things a token can unlock did not. S5's `POST /ring` will reach `Service.Ring` from the
// same package and so needs no row here; anything ELSE importing presence does.
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
	want := []string{depspolicy.ModulePath + "/cmd/cairn-ui", depspolicy.ModulePath + "/internal/ui"}
	if !slices.Equal(importers, want) {
		t.Fatalf("internal/presence is imported by %v, want exactly %v. A new importer is a new place a "+
			"presence token could come to mean something; add it here only as a deliberate, reviewed decision", importers, want)
	}
}
