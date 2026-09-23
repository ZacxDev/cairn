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

// seedNamedEntry writes ONE ordinary entry under an arbitrary FILENAME. `seedCache` derives
// the name from the ref, which cannot express a file whose name is README-shaped — and that
// name is the whole subject of `TestValidateCountsAREADMELookalikeAsAnOrdinaryEntry`.
//
// ⚠ `service:` must normalize to the filename's own slug or the loader rejects the entry as
// malformed ("a ref reaches the wrong file"), so a genuine entry at `readme.md` is
// `service: readme`. That is the shape a real store carries, and it is what makes these
// files entries rather than a second spelling of the policy sheet.
func seedNamedEntry(t *testing.T, root, scope, filename, service string) {
	t.Helper()
	body := "---\nservice: " + service + "\nscope: " + scope + "\n---\n\n" +
		"## What it is\n\nsynthetic.\n\n## Pointers\n\n- none\n\n" +
		"## Nuance / work-history\n\n- 2000-01-01: synthetic.\n"
	if err := os.WriteFile(filepath.Join(root, scope, filename), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// requireDistinctFiles is the fold-detecting reachability control: every name must be on
// disk AND hold its own bytes.
//
// 🔴 `os.Stat` IS NOT THAT CONTROL, AND BOTH LOOKALIKE ROWS USED IT. `README.md` and
// `readme.md` are ONE FILE on a case-folding filesystem: the second `os.WriteFile`
// (`O_CREATE|O_TRUNC`) silently replaces the first's contents, and `os.Stat` of BOTH names
// then succeeds — same inode. So the control passed while the fixture had stopped
// discriminating the exact-spelling rule from a case fold, and the row went on to fail on
// its main assertion with a message pointing at the FILTER rather than at the filesystem.
// `tests/parity/world.py`'s `build_store` already compares CONTENT for exactly this reason;
// this is that check on this side of the gate, so a fold fails here and says so.
//
// ⚠ IT ASSERTS DISTINCTNESS, WHICH IS A CLAIM ABOUT THE FIXTURES TOO. Every caller seeds
// bodies that differ (`seedNamedEntry` writes the ref into `service:`, `seedREADME` writes a
// sheet), so equal bytes can only mean two names reached one file.
func requireDistinctFiles(t *testing.T, dir string, names ...string) {
	t.Helper()
	seen := map[string]string{}
	for _, name := range names {
		body, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("the fixture never wrote %s: %v", name, err)
		}
		if other, dup := seen[string(body)]; dup {
			t.Fatalf("%s and %s hold the same bytes, so one overwrote the other — the "+
				"filesystem folded case and this fixture no longer tells the "+
				"exact-spelling rule apart from a fold", other, name)
		}
		seen[string(body)] = name
	}
}

func TestValidateCountsEveryEntryInAScopeWithNoREADME(t *testing.T) {
	// 🔴 THE CONTROL THAT SEPARATES "EXCLUDE README.md" FROM "SUBTRACT ONE". Each of the
	// three rows above holds EXACTLY ONE `README.md`, so `checked--` written as a blanket
	// `len(globbed) - 1` prints the same line as the rule it is meant to implement and
	// survives all three. The arithmetic can only be told apart by a scope with NO README
	// in it, where the correct count subtracts nothing and `- 1` loses a real entry.
	//
	// `oneInstanceHost` seeds one entry; two more are added so this row's numbers (3 of 3)
	// are distinct from every other row's and from what `- 1` would print (2 of 2).
	home := oneInstanceHost(t)
	cache := filepath.Join(home, ".cache", "subsystem-store")
	seedNamedEntry(t, cache, "alpha-notes", "two.md", "two")
	seedNamedEntry(t, cache, "alpha-notes", "three.md", "three")
	// 🔴 THE REACHABILITY CONTROL, IN THE DIRECTION THIS ROW NEEDS IT: three entries and NO
	// README at all, or the row is a second sample of the README-bearing case above and
	// discriminates nothing.
	names, err := os.ReadDir(filepath.Join(cache, "alpha-notes"))
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, n := range names {
		got = append(got, n.Name())
	}
	if len(got) != 3 {
		t.Fatalf("want a README-free scope holding exactly three entries, got %v", got)
	}
	for _, n := range got {
		if strings.EqualFold(n, "README.md") {
			t.Fatalf("this row must hold no README at all, got %v", got)
		}
	}

	opts := readOpts()
	opts.Scope = "alpha-notes"
	code, stdout, stderr := capture(t, Validate, opts)

	if code != ExitOK {
		t.Fatalf("a clean scope exits 0, got %d\n%s", code, stderr)
	}
	want := "cairn: alpha-notes: 3 of 3 entry file(s) parse, 0 malformed"
	if !strings.Contains(stdout, want) {
		t.Fatalf("got %q, want a line containing %q — the count is not the file count",
			stdout, want)
	}
}

func TestValidateCountsAREADMELookalikeAsAnOrdinaryEntry(t *testing.T) {
	// 🔴 THE PREDICATE IS `== "README.md"` EXACTLY, AND THAT CLAIM IS ONLY A COMMENT UNTIL
	// A ROW HOLDS A LOOKALIKE. Both clients say in prose that the spelling is the loader's
	// — "not a prefix, not a fold". Nothing pinned it, and a case-folded prefix match
	// passes every other row in this class: `readme.md` and `README-old.md` are ORDINARY
	// ENTRIES to the loader, so excluding them from the count while the loader still walks
	// them drives the printed numerator BELOW ZERO the moment one of them is malformed.
	//
	// This scope holds one real policy sheet (excluded), two lookalikes and two ordinary
	// entries (four counted) — five files, four counted, three README-shaped names, two
	// plain ones: no two of those numbers are equal, and none of them equals the `4 of 4`
	// the assertion names.
	home := oneInstanceHost(t)
	cache := filepath.Join(home, ".cache", "subsystem-store")
	seedREADME(t, cache, "alpha-notes")
	seedNamedEntry(t, cache, "alpha-notes", "readme.md", "readme")
	seedNamedEntry(t, cache, "alpha-notes", "README-old.md", "readme-old")
	seedNamedEntry(t, cache, "alpha-notes", "two.md", "two")
	// The reachability control: all five names must be on disk AS FIVE FILES, or the
	// assertion runs over the ordinary case and reads as coverage while providing none.
	// Content-distinct rather than `os.Stat` — see `requireDistinctFiles`.
	requireDistinctFiles(t, filepath.Join(cache, "alpha-notes"),
		"README.md", "README-old.md", "one.md", "readme.md", "two.md")

	opts := readOpts()
	opts.Scope = "alpha-notes"
	code, stdout, stderr := capture(t, Validate, opts)

	if code != ExitOK {
		t.Fatalf("a clean scope exits 0, got %d\n%s", code, stderr)
	}
	want := "cairn: alpha-notes: 4 of 4 entry file(s) parse, 0 malformed"
	if !strings.Contains(stdout, want) {
		t.Fatalf("got %q, want a line containing %q — only `README.md` EXACTLY is excluded",
			stdout, want)
	}
}
