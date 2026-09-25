package client

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
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

// THE WRITE-PROTOCOL HALF OF `validate`, AT THE VERB.
//
// 🔴 THE PARSE COUNT ABOVE AND THESE BLOCKS ANSWER DIFFERENT QUESTIONS. "Would the
// loader accept this file?" is the count line; an entry can pass it while holding text NO
// reader will ever surface. Until these blocks the only tool that reported
// `dropped lines:` and `marker reachability:` was an operator-local launcher, so an agent
// that had the package and not the launcher — the deployed case — ran a check that was
// silently weaker than the mandated one, and nothing in its output said so.
//
// The oracle's own coverage for the same rows is
// `tests/test_cairn_cli.py::TestValidateReportsTheWriteProtocolContract`, and the two
// clients are compared byte-for-byte by `tests/parity/` over a scope
// (`crag-notes`) seeded with exactly these defects — so a one-sided change is a
// divergence, not a fix.

// seedLossyEntry writes an entry that PARSES and still holds three things no reader can
// reach: two lines before the first bullet (the second a declaration) and a
// correctly-spelled marker on a bullet's continuation line.
func seedLossyEntry(t *testing.T, root, scope, ref string) {
	t.Helper()
	body := "---\nservice: " + ref + "\nscope: " + scope + "\n---\n\n" +
		"## What it is\n\nsynthetic.\n\n## Pointers\n\n- none\n\n" +
		"## Nuance / work-history\n\n" +
		"  this line reaches no bullet at all: the `- ` that opened it is gone.\n" +
		"  OPEN: and this one is a declaration nothing will ever surface.\n" +
		"- 2000-01-04: RESOLVED abc1234: the bullet that did survive.\n" +
		"  OPEN: a marker several lines in, where no parser looks.\n"
	if err := os.WriteFile(filepath.Join(root, scope, ref+".md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestValidateReportsDroppedLinesAndOutOfReachMarkers(t *testing.T) {
	home := oneInstanceHost(t)
	cache := filepath.Join(home, ".cache", "subsystem-store")
	seedLossyEntry(t, cache, "alpha-notes", "lossy-thing")

	opts := readOpts()
	opts.Scope = "alpha-notes"
	code, stdout, stderr := capture(t, Validate, opts)

	// 🔴 THE FILE PARSES. The parse half must still say so, or this test would be
	// measuring a malformed entry and proving nothing about the new half.
	if code != ExitOK {
		t.Fatalf("a parsing scope exits 0, got %d\n%s", code, stderr)
	}
	for _, want := range []string{
		"cairn: alpha-notes: 2 of 2 entry file(s) parse, 0 malformed",
		"🔴 2 DROPPED LINE(S) across 2 entry file(s) scanned [dropped-line]",
		"1 of them looks like a `OPEN:`/`RESOLVED:` DECLARATION",
		"lossy-thing.md: nuance line 2  ← looks like a DECLARATION",
		"🔴 1 MARKER(S) OUT OF REACH across 2 entry file(s) scanned [unreachable-marker]",
		"lossy-thing.md: line 2 of the bullet opening",
		"(would declare `open`)",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("missing %q in:\n%s", want, stdout)
		}
	}
}

// 🔴 THE WRITE PROTOCOL BRANCHES ON THIS COMMAND'S EXIT CODE TO MEAN "write NOTHING".
// Failing here would stop a session recording anything into an entry whose only defect is
// that an OLDER write lost a line — a gate the author cannot turn green by fixing the file
// they are writing, which is worse than no gate.
func TestNeitherAdvisoryMovesTheExitCode(t *testing.T) {
	home := oneInstanceHost(t)
	seedLossyEntry(t, filepath.Join(home, ".cache", "subsystem-store"),
		"alpha-notes", "lossy-thing")
	opts := readOpts()
	opts.Scope = "alpha-notes"
	code, stdout, stderr := capture(t, Validate, opts)
	if !strings.Contains(stdout, "DROPPED LINE(S)") {
		t.Fatalf("the fixture never reached the advisory:\n%s", stdout)
	}
	if code != ExitOK {
		t.Fatalf("an advisory moved the exit code to %d\n%s", code, stderr)
	}
}

// 🔴 A BARE ABSENCE IS INDISTINGUISHABLE FROM A SCANNER WIRED TO NOTHING.
func TestACleanScopeStillPrintsBothDenominators(t *testing.T) {
	home := oneInstanceHost(t)
	_ = home
	opts := readOpts()
	opts.Scope = "alpha-notes"
	code, stdout, stderr := capture(t, Validate, opts)
	if code != ExitOK {
		t.Fatalf("a clean scope exits 0, got %d\n%s", code, stderr)
	}
	for _, want := range []string{
		"dropped lines: 0 across 1 entry file(s)",
		"PARTIAL BY CONSTRUCTION",
		"marker reachability: 0 out-of-reach marker(s) across 1 entry file(s)",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("missing %q in:\n%s", want, stdout)
		}
	}
}

// 🔴 "0 across 0 entry file(s)" IS THE REASSURING ZERO FROM AN INSTRUMENT THAT WALKED
// NOTHING. The fixture is a scope holding ONLY its policy sheet, which is the reachable
// shape rather than a contrived one: `/snapshot` ships READMEs and both loaders skip them.
func TestAScopeWithNoEntriesPrintsNotCheckedRatherThanAZero(t *testing.T) {
	home := oneInstanceHost(t)
	cache := filepath.Join(home, ".cache", "subsystem-store")
	if err := os.MkdirAll(filepath.Join(cache, "sheet-only"), 0o755); err != nil {
		t.Fatal(err)
	}
	seedREADME(t, cache, "sheet-only")

	code, stdout, stderr := capture(t, Validate, readOpts())
	if code != ExitOK {
		t.Fatalf("exit %d\n%s", code, stderr)
	}
	var line string
	for _, ln := range strings.Split(stdout, "\n") {
		if strings.HasPrefix(ln, "cairn: sheet-only: write-protocol advisories") {
			line = ln
		}
	}
	if line == "" || !strings.Contains(line, "NOT CHECKED") {
		t.Fatalf("want a NOT CHECKED line for sheet-only, got:\n%s", stdout)
	}
	// 🔴 THE WITHHOLDING IS ALL-OR-NOTHING ACROSS ALL FOUR BLOCKS, and that is the half
	// the zero-substring check below cannot assert: neither new block spells its zero as
	// `0 across N`, so a run that withheld the two older blocks and still printed
	// `entry shape: 0 entry file(s) checked` would satisfy every line above.
	for _, withheld := range []string{
		"entry shape:", "open actions:", "dropped lines:", "marker reachability:",
	} {
		if strings.Contains(stdout, "cairn: sheet-only: "+withheld) {
			t.Errorf("%q printed anyway:\n%s", withheld, stdout)
		}
	}
	if strings.Contains(stdout, "0 across 0") {
		t.Fatalf("a zero over nothing was printed:\n%s", stdout)
	}
}

// 🔴 THE ADVISORY DENOMINATOR COUNTS WHAT THE SCANNERS OPENED, NOT WHAT THE DIRECTORY
// LISTED — AND THOSE ARE A THIRD SET, distinct from both the parse line's numerator and
// its denominator.
//
// RED at `cc1242a`: the advisories were handed `checked`, the unfiltered `*.md` listing,
// so this fixture printed `dropped lines: 0 across 2 entry file(s)` — "every non-blank
// line reaches a bullet some reader will surface" asserted over a FIFO nothing had
// opened. The scanners refuse a FIFO before `open()` (that is the round-1 fix, and it
// stands); it was only the printed count that still included it.
//
// ⚠ AND THE FIFO IS NOT SOFTENED OUT OF THE OUTPUT, which is what keeps this a fix
// rather than a hiding place: the parse line above still counts it, still reports it
// malformed, and the verb still exits 5. Both halves are asserted below, because a
// change that quietened the refusal would pass an assertion about the advisory alone.
func TestTheAdvisoryDenominatorExcludesAPathTheScannersNeverOpen(t *testing.T) {
	home := oneInstanceHost(t)
	cache := filepath.Join(home, ".cache", "subsystem-store")
	entries, err := os.ReadDir(filepath.Join(cache, "alpha-notes"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("the fixture seeds %d file(s); this case needs exactly one so the "+
			"counts below are unambiguous", len(entries))
	}
	fifo := filepath.Join(cache, "alpha-notes", "wedge.md")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatalf("mkfifo: %v", err)
	}

	opts := readOpts()
	opts.Scope = "alpha-notes"
	code, stdout, stderr := capture(t, Validate, opts)

	if code != ExitCorrupt {
		t.Fatalf("a refused entry must still drive the exit to %d, got %d\n%s",
			ExitCorrupt, code, stderr)
	}
	if want := "1 of 2 entry file(s) parse, 1 malformed"; !strings.Contains(stdout, want) {
		t.Errorf("the PARSE line must still count the fifo — want %q in:\n%s",
			want, stdout)
	}
	for _, want := range []string{
		"dropped lines: 0 across 1 entry file(s) scanned",
		"marker reachability: 0 out-of-reach marker(s) across 1 entry file(s) scanned",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("missing %q in:\n%s", want, stdout)
		}
	}
	if strings.Contains(stdout, "across 2 entry file(s) scanned") {
		t.Errorf("the advisory counted a path it never opened:\n%s", stdout)
	}
}

// The other end of the same change: when the scanners open NOTHING, `NOT CHECKED` is the
// honest line. Without the denominator fix this printed `0 across 1`, a zero over a file
// that was refused before `open()` — the exact shape
// `TestAScopeWithNoEntriesPrintsNotCheckedRatherThanAZero` exists to forbid, reached
// through a different door.
func TestAScopeHoldingONLYAnUnopenablePathAlsoSaysNotChecked(t *testing.T) {
	home := oneInstanceHost(t)
	cache := filepath.Join(home, ".cache", "subsystem-store")
	dir := filepath.Join(cache, "pipe-only")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(filepath.Join(dir, "wedge.md"), 0o600); err != nil {
		t.Fatalf("mkfifo: %v", err)
	}

	opts := readOpts()
	opts.Scope = "pipe-only"
	code, stdout, stderr := capture(t, Validate, opts)
	if code != ExitCorrupt {
		t.Fatalf("exit %d\n%s", code, stderr)
	}
	var line string
	for _, ln := range strings.Split(stdout, "\n") {
		if strings.HasPrefix(ln, "cairn: pipe-only: write-protocol advisories") {
			line = ln
		}
	}
	if line == "" || !strings.Contains(line, "NOT CHECKED") {
		t.Fatalf("want a NOT CHECKED line for pipe-only, got:\n%s", stdout)
	}
	// 🔴 THE WITHHOLDING IS ALL-OR-NOTHING ACROSS ALL FOUR BLOCKS, and that is the half
	// the zero-substring check below cannot assert: neither new block spells its zero as
	// `0 across N`, so a run that withheld the two older blocks and still printed
	// `entry shape: 0 entry file(s) checked` would satisfy every line above.
	for _, withheld := range []string{
		"entry shape:", "open actions:", "dropped lines:", "marker reachability:",
	} {
		if strings.Contains(stdout, "cairn: pipe-only: "+withheld) {
			t.Errorf("%q printed anyway:\n%s", withheld, stdout)
		}
	}
	if strings.Contains(stdout, "0 across 1") {
		t.Fatalf("a zero over a file nothing opened was printed:\n%s", stdout)
	}
}

// --------------------------------------------------------------------------
// The two NEW blocks, at the verb
// --------------------------------------------------------------------------
//
// The oracle's own coverage for the same rows is
// `tests/test_cairn_cli.py::TestValidateReportsTheWriteProtocolContract`
// (`test_a_BENT_SPINE_that_PARSES_is_reported_by_the_ENTRY_SHAPE_block`,
// `test_ALL_FOUR_OPEN_ACTION_POPULATIONS_are_reported_SEPARATELY`,
// `test_neither_NEW_advisory_moves_the_EXIT_CODE`,
// `test_a_CLEAN_scope_prints_all_FOUR_denominators_IN_ORDER`), and the two clients are
// compared byte-for-byte by `tests/parity/` over `crag-notes`, which now carries every
// shape kind and every openness population — so a one-sided change is a divergence.

// seedBentSpine writes an entry that PARSES and whose SPINE is wrong three ways at once:
// `## Pointers` renamed by case, and the nuance heading written twice with nothing under
// either.
//
// 🔴 IT IS ALSO THE ORDERING FIXTURE. An unreachable nuance section means the three lower
// blocks are all silent about a badly broken file, which is exactly why `entry shape:` has
// to print above them.
func seedBentSpine(t *testing.T, root, scope, ref string) {
	t.Helper()
	body := "---\nservice: " + ref + "\nscope: " + scope + "\n---\n\n" +
		"## What it is\n\nan entry whose spine departs from the schema.\n\n" +
		"## pointers\n\n- none\n\n" +
		"## Nuance / work-history\n\n" +
		"## Nuance / work-history\n"
	if err := os.WriteFile(filepath.Join(root, scope, ref+".md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// seedEveryPopulation writes an entry carrying all four reportable openness populations
// plus the TWO the scan must be silent about — a `RESOLVED <sha>:` and an ordinary bullet.
// Without those two controls a client that reported every bullet it saw would satisfy the
// per-population counts.
func seedEveryPopulation(t *testing.T, root, scope, ref string) {
	t.Helper()
	body := "---\nservice: " + ref + "\nscope: " + scope + "\n---\n\n" +
		"## What it is\n\nsynthetic.\n\n## Pointers\n\n- none\n\n" +
		"## Nuance / work-history\n\n" +
		"- 2000-01-05: OPEN: the writer declared this one, exactly.\n" +
		"- 2000-01-06: **OPEN**: emphasis, so the marker never parses.\n" +
		"- 2000-01-07: Open items: the retry budget is not yet addressed.\n" +
		"- 2000-01-08: RESOLVED: closed, and naming no sha at all.\n" +
		"- 2000-01-09: RESOLVED abc1234: closed, and reported by nothing.\n" +
		"- 2000-01-10: an ordinary bullet about an ordinary thing.\n"
	if err := os.WriteFile(filepath.Join(root, scope, ref+".md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// 🔴 THE SPINE EVERY CONSUMER DEPENDS ON WAS ENFORCED BY NOTHING. This entry parses — only
// a missing `service:` ever went red — while its `## Pointers` reaches no reader and its
// nuance section silently merges into one empty body. A reader computes an entry's bullet
// count and its `OPEN` badge from that section, so this renders as a well-formed EMPTY
// entry rather than as the broken one it is.
func TestValidateReportsABentSpineThatParses(t *testing.T) {
	home := oneInstanceHost(t)
	cache := filepath.Join(home, ".cache", "subsystem-store")
	seedBentSpine(t, cache, "alpha-notes", "bent-thing")

	opts := readOpts()
	opts.Scope = "alpha-notes"
	code, stdout, stderr := capture(t, Validate, opts)
	if code != ExitOK {
		t.Fatalf("a parsing scope exits 0, got %d\n%s", code, stderr)
	}
	for _, want := range []string{
		// 🔴 THE FILE PARSES, or this test is measuring a malformed entry.
		"cairn: alpha-notes: 2 of 2 entry file(s) parse, 0 malformed",
		"entry shape across 2 entry file(s), checked for",
		"1 section(s) RENAMED",
		"bent-thing.md: `## Pointers` is written as `## pointers`",
		"1 heading(s) DUPLICATED",
		"bent-thing.md: `## Nuance / work-history` appears 2 times",
		"1 section(s) PRESENT AND EMPTY",
		// 🔴 AND THE THREE LOWER BLOCKS ARE SILENT ABOUT IT, which is the ordering
		// argument as behaviour: their zeros are facts about a section no parser
		// reached, and only the block above can say so.
		"open actions: 0 declared across 2 entry file(s)",
		"dropped lines: 0 across 2 entry file(s) scanned",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("missing %q in:\n%s", want, stdout)
		}
	}
}

// 🔴 FOUR POPULATIONS, FOUR SUB-BLOCKS, AND THEY ARE NEVER SUMMED. A `OPEN:` bullet is
// exact — the writer said so — while the unmarked guess is a FLOOR from two measured
// phrasings with unknown recall. One total over both would let the floor masquerade as a
// count. Six bullets in, four reported.
func TestValidateReportsAllFourOpenActionPopulationsSeparately(t *testing.T) {
	home := oneInstanceHost(t)
	cache := filepath.Join(home, ".cache", "subsystem-store")
	seedEveryPopulation(t, cache, "alpha-notes", "busy-thing")

	opts := readOpts()
	opts.Scope = "alpha-notes"
	code, stdout, stderr := capture(t, Validate, opts)
	if code != ExitOK {
		t.Fatalf("a parsing scope exits 0, got %d\n%s", code, stderr)
	}
	for _, want := range []string{
		"cairn: alpha-notes: 2 of 2 entry file(s) parse, 0 malformed",
		"open actions across 2 entry file(s):",
		"🔴 1 declared `OPEN:`",
		"🔴 1 bullet(s) look like an ATTEMPTED marker",
		"⚠ 1 unmarked bullet(s) that READ like an open action",
		"⚠ 1 `RESOLVED:` bullet(s) name no sha",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("missing %q in:\n%s", want, stdout)
		}
	}
	// The two controls are reported by NOTHING, and no total sums the four.
	for _, forbidden := range []string{
		"RESOLVED abc1234: closed, and reported by nothing",
		"an ordinary bullet about an ordinary thing",
		"4 declared",
	} {
		if strings.Contains(stdout, forbidden) {
			t.Errorf("found %q in:\n%s", forbidden, stdout)
		}
	}
}

// 🔴 THE WRITE PROTOCOL BRANCHES ON THIS COMMAND'S EXIT CODE TO MEAN "write NOTHING". An
// entry whose heading is renamed is genuinely, silently broken — and still must not fail
// the verdict, because the verdict answers one question ("would the loader accept this
// file?") and the answer is yes.
func TestNeitherNewAdvisoryMovesTheExitCode(t *testing.T) {
	home := oneInstanceHost(t)
	cache := filepath.Join(home, ".cache", "subsystem-store")
	seedBentSpine(t, cache, "alpha-notes", "bent-thing")
	seedEveryPopulation(t, cache, "alpha-notes", "busy-thing")
	opts := readOpts()
	opts.Scope = "alpha-notes"
	code, stdout, stderr := capture(t, Validate, opts)
	// Both new blocks must be in their FINDINGS branch, or this proves nothing.
	for _, reach := range []string{"section(s) RENAMED", "declared `OPEN:`"} {
		if !strings.Contains(stdout, reach) {
			t.Fatalf("the fixture never reached %q:\n%s", reach, stdout)
		}
	}
	if code != ExitOK {
		t.Fatalf("an advisory moved the exit code to %d\n%s", code, stderr)
	}
}

// 🔴 A BARE ABSENCE IS INDISTINGUISHABLE FROM A SCANNER WIRED TO NOTHING, and the shape
// zero has a second way to be vacuous the others do not: it must name the SET it checked,
// because a reader who assumes `## What it is` was checked would take it as a claim about
// a heading nothing examined.
//
// The ORDER is asserted here rather than in the unit tests alone because it is what an
// operator actually reads, and it is the whole reason the shape block exists above the
// other three.
func TestACleanScopePrintsAllFourDenominatorsInOrder(t *testing.T) {
	home := oneInstanceHost(t)
	_ = home
	opts := readOpts()
	opts.Scope = "alpha-notes"
	code, stdout, stderr := capture(t, Validate, opts)
	if code != ExitOK {
		t.Fatalf("a clean scope exits 0, got %d\n%s", code, stderr)
	}
	for _, want := range []string{
		"entry shape: 1 entry file(s) checked for `## Pointers`, " +
			"`## Nuance / work-history`",
		"`## What it is` is NOT checked here",
		"open actions: 0 declared across 1 entry file(s)",
		"FLOOR with unknown recall",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("missing %q in:\n%s", want, stdout)
		}
	}
	prev := -1
	for _, leader := range []string{
		"entry shape:", "dropped lines:", "open actions:", "marker reachability:",
	} {
		at := strings.Index(stdout, leader)
		if at <= prev {
			t.Fatalf("%q at %d, out of order (previous %d):\n%s",
				leader, at, prev, stdout)
		}
		prev = at
	}
}
