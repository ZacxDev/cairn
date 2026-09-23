package client

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ZacxDev/cairn/internal/store"
)

// THE README MISCOUNT AT THE OTHER VERB. `Validate`'s count was fixed by filtering its own
// `*.md` glob; `LsEntries` globbed `<cache>/*/*.md` and printed every match, so a scope's
// policy sheet was listed as `<scope>/README.md`. The top-level `README.md` describes this
// verb as "what the cache actually holds", so the listing was ASSERTING that a README is an
// entry — measured twelve of them on a populated cache.
//
// 🔴 EVERY TEST IN THIS FILE IS RED AT THE COMMIT BEFORE THE FIX, WITH THE README IN THE
// LISTING rather than with an error. The oracle's own coverage for the same rows is
// `tests/test_cairn_cli.py::TestLsEntriesListsENTRIES`; the two clients are compared
// byte-for-byte by `tests/parity/harness.py`, whose world now seeds sheets and lookalikes,
// so a one-sided fix is a divergence rather than a fix.

// lsLines is the listing as a slice, with the trailing newline removed and an empty listing
// reported as an empty slice rather than as one empty string.
func lsLines(stdout string) []string {
	trimmed := strings.TrimRight(stdout, "\n")
	if trimmed == "" {
		return nil
	}
	return strings.Split(trimmed, "\n")
}

func TestLsEntriesDoesNotListAScopesREADME(t *testing.T) {
	home := oneInstanceHost(t)
	cache := filepath.Join(home, ".cache", "subsystem-store")
	// 🔴 THE REACHABILITY CONTROL. `oneInstanceHost` seeds exactly one entry into
	// `alpha-notes`; without the README landing beside it this row asserts over a cache the
	// defect cannot reach and reads as coverage while providing none.
	seedREADME(t, cache, "alpha-notes")
	if _, err := os.Stat(filepath.Join(cache, "alpha-notes", "README.md")); err != nil {
		t.Fatalf("the fixture never wrote the README: %v", err)
	}

	code, stdout, stderr := capture(t, LsEntries, readOpts())

	if code != ExitOK {
		t.Fatalf("a readable host exits 0, got %d\n%s", code, stderr)
	}
	got := lsLines(stdout)
	want := []string{"alpha-notes/one.md"}
	if len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("got %v, want %v — the scope's policy sheet is in the listing", got, want)
	}
}

func TestLsEntriesListsNOTHINGForAScopeHoldingONLYAREADME(t *testing.T) {
	// 🔴 THE SHARPEST FORM: a scope with no entries in it listed one line, so a consumer
	// that opens what this verb prints was handed a path to a file the loader will not
	// index. An honest answer is no line at all, still at exit 0.
	home := oneInstanceHost(t)
	cache := filepath.Join(home, ".cache", "subsystem-store")
	if err := os.Remove(filepath.Join(cache, "alpha-notes", "one.md")); err != nil {
		t.Fatal(err)
	}
	seedREADME(t, cache, "alpha-notes")
	names, err := os.ReadDir(filepath.Join(cache, "alpha-notes"))
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 1 || names[0].Name() != "README.md" {
		t.Fatalf("the fixture is not a README-only scope: %v", names)
	}

	code, stdout, stderr := capture(t, LsEntries, readOpts())

	if code != ExitOK {
		t.Fatalf("an entry-less cache exits 0, got %d\n%s", code, stderr)
	}
	if got := lsLines(stdout); len(got) != 0 {
		t.Fatalf("got %v, want an EMPTY listing", got)
	}
	// The negative control for the assertion above: an empty stdout must not be an empty
	// RUN. The banner is what proves the verb reached the cache at all.
	if !strings.Contains(stderr, "cairn") {
		t.Fatalf("no banner on stderr, so the empty listing is not evidence:\n%s", stderr)
	}
}

func TestLsEntriesListsEveryEntryInAScopeWithNoREADME(t *testing.T) {
	// 🔴 THE CONTROL THAT SEPARATES "EXCLUDE README.md" FROM "DROP ONE FILE PER SCOPE".
	// Every other row here holds EXACTLY ONE `README.md`, so a `matches[1:]` — or any
	// blanket subtract-one — prints the same listing as the rule it is meant to implement
	// and survives all of them. Only a scope with NO README tells them apart, and this one
	// holds THREE entries so its line count is distinct from every other row's.
	home := oneInstanceHost(t)
	cache := filepath.Join(home, ".cache", "subsystem-store")
	seedNamedEntry(t, cache, "alpha-notes", "two.md", "two")
	seedNamedEntry(t, cache, "alpha-notes", "three.md", "three")
	landed, err := os.ReadDir(filepath.Join(cache, "alpha-notes"))
	if err != nil {
		t.Fatal(err)
	}
	if len(landed) != 3 {
		t.Fatalf("want three entries and no README, got %d", len(landed))
	}
	for _, n := range landed {
		if strings.EqualFold(n.Name(), "README.md") {
			t.Fatalf("this row must hold no README at all, got %v", landed)
		}
	}

	code, stdout, stderr := capture(t, LsEntries, readOpts())

	if code != ExitOK {
		t.Fatalf("a readable host exits 0, got %d\n%s", code, stderr)
	}
	got := lsLines(stdout)
	want := []string{"alpha-notes/one.md", "alpha-notes/three.md", "alpha-notes/two.md"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("got %v, want %v — the listing is not the entry set", got, want)
	}
}

func TestLsEntriesListsAREADMELookalikeAsAnOrdinaryEntry(t *testing.T) {
	// 🔴 THE PREDICATE IS `== "README.md"` EXACTLY, AND A LISTING IS WHERE A FOLD OR A
	// PREFIX MATCH SHOWS UP AS A MISSING FILE RATHER THAN AS A WRONG NUMBER. `readme.md`
	// and `README-old.md` are ORDINARY ENTRIES — the loader walks and indexes them — so a
	// client that hid them from `ls-entries` would tell a reader the cache does not hold a
	// file `recall --ref readme` will happily serve.
	//
	// This scope holds one sheet (excluded), two lookalikes and two plain entries: five
	// files, four listed, three README-shaped names, two plain ones. No two of those
	// numbers are equal, and none equals the four-line listing asserted below.
	//
	// ⚠ `README.md` AND `readme.md` IN ONE DIRECTORY IS TWO FILES ON LINUX AND ONE ON A
	// CASE-FOLDING FILESYSTEM. CI is `ubuntu-latest`; the reachability control below fails
	// loudly rather than letting a folded fixture score a pass.
	home := oneInstanceHost(t)
	cache := filepath.Join(home, ".cache", "subsystem-store")
	seedREADME(t, cache, "alpha-notes")
	seedNamedEntry(t, cache, "alpha-notes", "readme.md", "readme")
	seedNamedEntry(t, cache, "alpha-notes", "README-old.md", "readme-old")
	seedNamedEntry(t, cache, "alpha-notes", "two.md", "two")
	for _, name := range []string{
		"README.md", "README-old.md", "one.md", "readme.md", "two.md",
	} {
		if _, err := os.Stat(filepath.Join(cache, "alpha-notes", name)); err != nil {
			t.Fatalf("the fixture never wrote %s: %v", name, err)
		}
	}

	code, stdout, stderr := capture(t, LsEntries, readOpts())

	if code != ExitOK {
		t.Fatalf("a readable host exits 0, got %d\n%s", code, stderr)
	}
	got := lsLines(stdout)
	want := []string{
		"alpha-notes/README-old.md",
		"alpha-notes/one.md",
		"alpha-notes/readme.md",
		"alpha-notes/two.md",
	}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("got %v, want %v — only `README.md` EXACTLY is excluded", got, want)
	}
}

// TestTheEntryFilePredicateIsOneRuleAtEveryCallSite is the CONSOLIDATION's own guard, and it
// is a different claim from every row above.
//
// 🔴 THE ROWS ABOVE PIN BEHAVIOUR; THIS PINS THAT THERE IS ONE RULE. Four production sites
// answered "is this an entry file" and two answered it differently, which is the defect this
// change closes. A future site that re-spells the test — `strings.HasPrefix(name, "README")`,
// an `EqualFold`, a `matches[1:]` — passes every behavioural row that holds exactly one
// sheet. So this table feeds the predicate itself the inputs that tell those spellings
// apart, and its cases are pairwise distinct AND distinct from the one constant the rule
// names.
func TestTheEntryFilePredicateIsOneRuleAtEveryCallSite(t *testing.T) {
	cases := []struct {
		name string
		want bool
		why  string
	}{
		{"README.md", false, "the policy sheet itself, the one excluded name"},
		{"readme.md", true, "a CASE fold would swallow this; it is an ordinary entry"},
		{"README-old.md", true, "a PREFIX match would swallow this; it is an ordinary entry"},
		{"README.markdown", false, "`.markdown` is not `.md`, so the suffix half rejects it"},
		{"aREADME.md", true, "a SUFFIX match on the name would swallow this; it is an entry"},
		{"widget-cfg.md", true, "the ordinary shape"},
		{"notes/README.md", true, "a NAME predicate, not a path one — callers pass a base name"},
		{"README", false, "no `.md` suffix, so not an entry file at all"},
		{"README.md.bak", false, "no `.md` suffix either, and it is not the sheet"},
		{".#widget-cfg.md", true, "a dot-file IS in the set; `ClassifyPath` is what refuses it"},
		{"", false, "the empty name is not a file"},
	}
	seen := map[string]bool{}
	for _, tc := range cases {
		if seen[tc.name] {
			t.Fatalf("%q appears twice, so the table measures it once and reports twice", tc.name)
		}
		seen[tc.name] = true
		if got := store.IsEntryFileName(tc.name); got != tc.want {
			t.Fatalf("store.IsEntryFileName(%q) = %v, want %v — %s", tc.name, got, tc.want, tc.why)
		}
	}
	// 🔴 AND THE CALL SITES ACTUALLY ROUTE THROUGH IT. A predicate nothing calls is a
	// declaration, not a rule: `EntryFileNames` is what `store.LoadIndex` and `Validate`
	// enumerate with, so its answer over a real directory must be the predicate's answer,
	// name for name.
	dir := t.TempDir()
	for _, name := range []string{"README.md", "readme.md", "README-old.md", "plain.md", "not-md.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	names, err := store.EntryFileNames(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"README-old.md", "plain.md", "readme.md"}
	if strings.Join(names, "|") != strings.Join(want, "|") {
		t.Fatalf("store.EntryFileNames = %v, want %v — the set and the predicate disagree",
			names, want)
	}
}
