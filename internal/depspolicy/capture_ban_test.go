package depspolicy

import (
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// copyGoTree copies go.mod and every .go file under cmd/ and internal/ into dst — the inputs
// ImportGraph reads, and nothing else.
func copyGoTree(t *testing.T, src, dst string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(src, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dst, "go.mod"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, top := range []string{"cmd", "internal"} {
		err := filepath.WalkDir(filepath.Join(src, top), func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			rel, _ := filepath.Rel(src, path)
			if d.IsDir() {
				return os.MkdirAll(filepath.Join(dst, rel), 0o700)
			}
			if !strings.HasSuffix(path, ".go") {
				return nil
			}
			b, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			return os.WriteFile(filepath.Join(dst, rel), b, 0o600)
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

func captureEdges(t *testing.T, root string) []string {
	t.Helper()
	graph, err := ImportGraph(root, true)
	if err != nil {
		t.Fatal(err)
	}
	capture := ModulePath + "/cmd/cairn-capture"
	if _, ok := graph[capture]; !ok {
		t.Fatalf("the graph has no %s — the control would measure nothing", capture)
	}
	return ThirdPartyEdges(graph, Closure(graph, capture))
}

// TestTheBanSeesANonTestImportInTheCaptureAgent is S2's NEGATIVE CONTROL for the ban's new root.
//
// 🔴 THE CONTROL IS A NON-TEST FILE, AND A `_test.go` ONE IS SHOWN NOT TO BE ONE. The ban's graph
// holds non-test imports only, by design (`ImportGraph`), so a `_test.go` importer of a third-party
// module leaves it green — a control written that way would be green whatever the ban did. On a
// scratch copy: a NON-test `.go` file in `cmd/cairn-capture` importing the HTML renderer must make
// the capture agent's closure reach a third-party module, and the same import in a `_test.go`
// file must not.
func TestTheBanSeesANonTestImportInTheCaptureAgent(t *testing.T) {
	if !slices.Contains(LinkedBinaryRoots, ModulePath+"/cmd/cairn-capture") {
		t.Fatal("cmd/cairn-capture is not one of LinkedBinaryRoots: the ban does not cover the capture agent")
	}
	repo, err := RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	clean := t.TempDir()
	copyGoTree(t, repo, clean)
	if edges := captureEdges(t, clean); len(edges) != 0 {
		t.Fatalf("the unmodified copy already reaches a third-party module: %v", edges)
	}
	const imp = "package main\n\nimport _ \"maragu.dev/gomponents\"\n"

	testOnly := t.TempDir()
	copyGoTree(t, repo, testOnly)
	if err := os.WriteFile(filepath.Join(testOnly, "cmd", "cairn-capture", "zz_control_test.go"), []byte(imp), 0o600); err != nil {
		t.Fatal(err)
	}
	if edges := captureEdges(t, testOnly); len(edges) != 0 {
		t.Fatalf("a _test.go importer reached the ban's graph (%v) — then the ban is not non-test-only and the "+
			"paragraph above is wrong", edges)
	}

	nonTest := t.TempDir()
	copyGoTree(t, repo, nonTest)
	if err := os.WriteFile(filepath.Join(nonTest, "cmd", "cairn-capture", "zz_control.go"), []byte(imp), 0o600); err != nil {
		t.Fatal(err)
	}
	edges := captureEdges(t, nonTest)
	if len(edges) != 1 || edges[0] != ModulePath+"/cmd/cairn-capture -> maragu.dev/gomponents" {
		t.Fatalf("a NON-test third-party import in cmd/cairn-capture was not seen by the ban's walk: %v", edges)
	}
}
