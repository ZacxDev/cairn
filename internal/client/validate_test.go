package client

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 🔴 THE VERB-LEVEL HALF OF THE README MISCOUNT, AND IT IS A DIFFERENT CLAIM FROM
// `store.TestValidateScopeSkipsTheScopesREADME`. That one says the function answers
// correctly; this one says the VERB asks it — the seam where the old `Glob("*.md")`
// beside a `LoadIndex` that skips `README.md` actually lived. A unit test of the
// function alone stays green over a caller that never calls it.
//
// The oracle's own regression coverage for the same seam is
// `tests/test_cairn_cli.py::TestValidateActuallyRuns::test_the_count_EXCLUDES_the_scopes_README`,
// which was measured RED against the previous commit at `3 of 3 entry file(s) parse`.
// The two clients are compared byte-for-byte by the parity gate, so this line has to be
// identical on both sides or that gate goes red — which is the shape the fix wanted.

// seedREADME drops a scope-policy sheet into a cached scope. It is NOT an entry: the
// loader skips one in every scope, and `/snapshot` ships it, so a real cache has them.
func seedREADME(t *testing.T, root, scope string) {
	t.Helper()
	path := filepath.Join(root, scope, "README.md")
	if err := os.WriteFile(path, []byte("# the scope's own policy sheet\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestValidateDoesNotCountAScopesREADMEAsAnEntryFile(t *testing.T) {
	home := oneInstanceHost(t)
	cache := filepath.Join(home, ".cache", "subsystem-store")
	// 🔴 THE REACHABILITY CONTROL. `oneInstanceHost` seeds exactly one entry into
	// `alpha-notes`; if the README below did not land beside it the assertion would pass
	// over a cache the defect cannot reach and read as coverage while providing none.
	seedREADME(t, cache, "alpha-notes")
	if _, err := os.Stat(filepath.Join(cache, "alpha-notes", "README.md")); err != nil {
		t.Fatalf("the fixture never wrote the README: %v", err)
	}

	opts := readOpts()
	opts.Scope = "alpha-notes"
	code, stdout, stderr := capture(t, Validate, opts)

	if code != ExitOK {
		t.Fatalf("a clean scope exits 0, got %d\n%s", code, stderr)
	}
	want := "cairn: alpha-notes: 1 of 1 entry file(s) parse, 0 malformed"
	if !strings.Contains(stdout, want) {
		t.Fatalf("got %q, want a line containing %q — the README is in the count",
			stdout, want)
	}
}

func TestValidateStillReportsAMalformedEntryBesideAREADME(t *testing.T) {
	// 🔴 THE DIRECTION THAT MISLEADS, AND THE NEGATIVE CONTROL IN ONE. With one entry and
	// one README, a BROKEN entry printed `1 of 2 … 1 malformed` — asserting a file parsed
	// when none had. The honest line is `0 of 1`, and the verb must still exit non-zero.
	home := oneInstanceHost(t)
	cache := filepath.Join(home, ".cache", "subsystem-store")
	seedREADME(t, cache, "alpha-notes")
	if err := os.WriteFile(filepath.Join(cache, "alpha-notes", "one.md"),
		[]byte("no front matter at all\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	opts := readOpts()
	opts.Scope = "alpha-notes"
	code, stdout, stderr := capture(t, Validate, opts)

	if code != ExitCorrupt {
		t.Fatalf("a malformed cache exits %d, got %d\n%s", ExitCorrupt, code, stderr)
	}
	want := "cairn: alpha-notes: 0 of 1 entry file(s) parse, 1 malformed"
	if !strings.Contains(stdout, want) {
		t.Fatalf("got %q, want a line containing %q", stdout, want)
	}
	if !strings.Contains(stderr, "malformed: MalformedEntry(scope='alpha-notes'") {
		t.Fatalf("the per-entry row is missing from stderr:\n%s", stderr)
	}
}
