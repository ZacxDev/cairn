package depspolicy

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

const refauditPkg = ModulePath + "/internal/refaudit"

// execImporters is every IN-REPO package in root's closure that imports `os/exec` directly, sorted.
func execImporters(graph map[string][]string, root string) []string {
	var out []string
	for pkg := range Closure(graph, root) {
		for _, imp := range graph[pkg] {
			if imp == "os/exec" {
				out = append(out, strings.TrimPrefix(pkg, ModulePath+"/"))
			}
		}
	}
	sort.Strings(out)
	return out
}

// TestThePodAndTheBrowserSurfaceNeverReachTheAuditor is clause (e) of the scope-refs plan's closing
// condition, at unit level (`tests/refaudit/e2e.sh` asserts the same through `go list -deps`): the
// auditor — which spawns `git` and fetches remotes — is linked into `cmd/cairn` and into NEITHER
// server.
//
// 🔴 THE `os/exec` HALF IS STATED AS IT IS, NOT AS THE PLAN WORDED IT. The plan said "no `os/exec` in
// `cmd/cairn-ui`"; that was already false on `main` before S5: `internal/ui`'s agent tab imports
// `internal/client` (for the recall preamble), and `internal/client`'s `runGit` imports `os/exec`. So
// the UI's claim is pinned as the EXACT importer set — `internal/client` alone — failing on GROW or
// SHRINK, while the pod's is zero. A new exec reaching the browser surface through any other package
// goes red here. ⚠ It is a statement about LINKED code, not about what runs: `internal/client`'s exec is
// reachable only through CLI verbs the UI never dispatches, which no import graph can show.
//
// POSITIVE CONTROL: `cmd/cairn`'s closure DOES hold `internal/refaudit` and its exec, so the walk can
// see both.
func TestThePodAndTheBrowserSurfaceNeverReachTheAuditor(t *testing.T) {
	repo, err := RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	graph, err := ImportGraph(repo, true)
	if err != nil {
		t.Fatal(err)
	}
	cli := ModulePath + "/cmd/cairn"
	if !Closure(graph, cli)[refauditPkg] {
		t.Fatalf("POSITIVE CONTROL: cmd/cairn does not reach %s, so a zero below would be a walk that sees nothing", refauditPkg)
	}
	if got := execImporters(graph, cli); !contains(got, "internal/refaudit") || !contains(got, "internal/client") {
		t.Fatalf("POSITIVE CONTROL: cmd/cairn's exec importers are %v", got)
	}
	for _, root := range []string{ModulePath + "/cmd/cairn-server", UIBinaryRoot} {
		if Closure(graph, root)[refauditPkg] {
			t.Errorf("%s reaches %s: a server would link the auditor that fetches remotes", root, refauditPkg)
		}
	}
	if got := execImporters(graph, ModulePath+"/cmd/cairn-server"); len(got) != 0 {
		t.Errorf("the pod's closure imports os/exec through %v; it must spawn nothing", got)
	}
	if got := execImporters(graph, UIBinaryRoot); strings.Join(got, ",") != "internal/client" {
		t.Errorf("the browser surface's os/exec importers are %v, want exactly [internal/client]", got)
	}
}

// TestTheAuditorBanSeesANonTestImportInThePod is the ban's NEGATIVE CONTROL, on a scratch copy: a
// non-test import of `internal/refaudit` in `cmd/cairn-server` is seen.
func TestTheAuditorBanSeesANonTestImportInThePod(t *testing.T) {
	repo, err := RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	copyRoot := t.TempDir()
	copyGoTree(t, repo, copyRoot)
	if err := os.WriteFile(filepath.Join(copyRoot, "cmd", "cairn-server", "zz_control.go"),
		[]byte("package main\n\nimport _ \""+refauditPkg+"\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	graph, err := ImportGraph(copyRoot, true)
	if err != nil {
		t.Fatal(err)
	}
	if !Closure(graph, ModulePath+"/cmd/cairn-server")[refauditPkg] {
		t.Fatal("a non-test import of the auditor in cmd/cairn-server was not seen by the walk")
	}
	if got := execImporters(graph, ModulePath+"/cmd/cairn-server"); !contains(got, "internal/refaudit") {
		t.Fatalf("the pod's exec importers after the planted import: %v", got)
	}
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
