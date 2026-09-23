package client

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// THE README MISCOUNT, AT THE VERB — the seam where the old `Glob("*.md")` sat beside a
// `LoadIndex` that skips `README.md`, so the numerator and the denominator came from two
// different walks. `/snapshot` ships a policy sheet into every scope, so this fired against
// the real store.
//
// 🔴 EVERY TEST IN THIS FILE IS RED AT THE COMMIT BEFORE THE FIX, and red with the WRONG
// COUNT rather than with an error: `1 of 1` where `0 of 0` is honest, `2 of 2` where the
// README inflates it to `3 of 3`, and `1 of 2 … 1 malformed` where nothing parsed at all.
//
// The oracle's own regression coverage for the same three rows is
// `tests/test_cairn_cli.py::TestValidateActuallyRuns` (`test_the_count_EXCLUDES_the_scopes_README`,
// `test_a_scope_holding_ONLY_a_README_reports_ZERO_walked`,
// `test_the_MALFORMED_count_is_not_softened_by_a_README`). The two clients are compared
// byte-for-byte by the parity gate, so a one-sided fix is a divergence, not a fix.

// seedREADME drops a scope-policy sheet into a cached scope. It is NOT an entry: the loader
// skips one in every scope, and `/snapshot` ships it, so a real cache has them.
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

func TestValidateReportsZeroForAScopeHoldingONLYAREADME(t *testing.T) {
	// 🔴 THE SHARPEST FORM OF THE SAME DEFECT: a directory with no entries in it printed
	// `1 of 1 entry file(s) parse, 0 malformed` — a clean bill of health over a scope the
	// reader renders as empty. The honest line is `0 of 0`.
	home := oneInstanceHost(t)
	cache := filepath.Join(home, ".cache", "subsystem-store")
	if err := os.Remove(filepath.Join(cache, "alpha-notes", "one.md")); err != nil {
		t.Fatal(err)
	}
	seedREADME(t, cache, "alpha-notes")
	// The reachability control, in the direction this case needs it: the scope dir must
	// still exist (or `validate --scope` refuses before counting anything) and must hold
	// the README and nothing else.
	names, err := os.ReadDir(filepath.Join(cache, "alpha-notes"))
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 1 || names[0].Name() != "README.md" {
		t.Fatalf("the fixture is not a README-only scope: %v", names)
	}

	opts := readOpts()
	opts.Scope = "alpha-notes"
	code, stdout, stderr := capture(t, Validate, opts)

	if code != ExitOK {
		t.Fatalf("an empty scope exits 0, got %d\n%s", code, stderr)
	}
	want := "cairn: alpha-notes: 0 of 0 entry file(s) parse, 0 malformed"
	if !strings.Contains(stdout, want) {
		t.Fatalf("got %q, want a line containing %q", stdout, want)
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
