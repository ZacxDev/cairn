package client

import (
	"errors"
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
	// files, four listed, three README-shaped names, two plain ones — 5, 4, 3, 2, no two of
	// them equal.
	//
	// 🔴 DISTINCTNESS AMONG THOSE FOUR REFUTES A FOLD RULE AND A PREFIX RULE; IT DOES NOT
	// REFUTE A BLANKET SUBTRACT-ONE, AND THIS SENTENCE SAID IT DID. THIRD CORRECTION TO IT.
	// Worked through: a FOLD rule (drop every name that case-folds to `readme.md`) drops
	// `README.md` and `readme.md` and lists 3; a PREFIX rule (drop every name starting
	// `README`) drops `README.md` and `README-old.md` and also lists 3 — both refuted,
	// because 3 != 4. A blanket SUBTRACT-ONE lists 5-1 = 4, which IS the listed count, so no
	// comparison among these four numbers can separate it from the real rule. What refutes it
	// is the assertion below, which pins the four NAMES: sorted, this scope's first file is
	// `README-old.md` and the rule under test LISTS it, so any subtract-one dropping anything
	// other than `README.md` yields a different set. The conclusion stands; its stated reason
	// did not.
	//
	// ⚠ AN EARLIER TAIL READ "and none equals the four-line listing asserted below", WHICH
	// WAS FALSE ABOUT ITS OWN NUMBERS: four listed IS the four-line listing. The round after
	// that moved the claim to "distinctness among the four counts" — correct as far as it
	// went, and still not enough to carry the subtract-one half, which is the paragraph
	// above.
	//
	// ⚠ `README.md` AND `readme.md` IN ONE DIRECTORY IS TWO FILES ON LINUX AND ONE ON A
	// CASE-FOLDING FILESYSTEM. CI is `ubuntu-latest`; the reachability control below fails
	// loudly rather than letting a folded fixture score a pass — and it compares CONTENT,
	// because `os.Stat` of both names succeeds against ONE folded inode and could not make
	// that claim. See `requireDistinctFiles`.
	home := oneInstanceHost(t)
	cache := filepath.Join(home, ".cache", "subsystem-store")
	seedREADME(t, cache, "alpha-notes")
	seedNamedEntry(t, cache, "alpha-notes", "readme.md", "readme")
	seedNamedEntry(t, cache, "alpha-notes", "README-old.md", "readme-old")
	seedNamedEntry(t, cache, "alpha-notes", "two.md", "two")
	requireDistinctFiles(t, filepath.Join(cache, "alpha-notes"),
		"README.md", "README-old.md", "one.md", "readme.md", "two.md")

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

// TestLsEntriesReadsACacheROOTCarryingAGlobMetacharacter is the OTHER half of "what the
// cache actually holds", and it is about the ANCHOR rather than about the filter.
//
// 🔴 A FALSE CLAIM OF ABSENCE, NOT A WRONG LINE. `LsEntries` built its pattern as
// `filepath.Glob(filepath.Join(cache, "*", "*.md"))`, which puts the CACHE ROOT inside the
// pattern — so a `[`, `?`, `*` or `\` anywhere in the operator's own directory name is
// INTERPRETED. RED at `a41dd02` with this exact fixture: `filepath.Match` returned
// `ErrBadPattern`, `matches` was nil, the error was discarded, and the verb printed NOTHING
// at exit 0 over a cache holding one entry. The oracle never had it — `cache.glob("*/*.md")`
// treats its anchor literally — so this was also a silent divergence the parity gate cannot
// see, because no world builds a cache root with a metacharacter in it.
//
// ⚠ THE PYTHON TWIN OF THIS ROW IS AN INVARIANT GUARD, AND IS LABELLED AS ONE:
// `tests/test_cairn_cli.py::TestLsEntriesListsENTRIES::
// test_a_cache_ROOT_carrying_a_glob_METACHARACTER_still_lists_its_entries` PASSES at
// `a41dd02`. Only this client was wrong; the oracle's row pins that it stays right.
func TestLsEntriesReadsACacheROOTCarryingAGlobMetacharacter(t *testing.T) {
	// `t.TempDir()` cannot be made to carry a metacharacter, so `$HOME` is a subdirectory of
	// it that does — which makes the DEFAULT cache root (`$HOME/.cache/subsystem-store`) the
	// thing under test, with no flag involved.
	home := filepath.Join(t.TempDir(), "wid[get")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("CAIRN_MIRROR_ROOT", "")
	t.Setenv("SUBSYSTEM_STORE_URL", "http://127.0.0.1:1")
	t.Setenv("SUBSYSTEM_STORE_TOKEN", "x")
	dir := configuredHost(t, filepath.Join(home, "config"))
	writeTable(t, dir, `{"alpha-notes": "`+DefaultAlias+`"}`)
	cache := filepath.Join(home, ".cache", "subsystem-store")
	seedCache(t, cache, "alpha-notes", "one")

	// 🔴 THE REACHABILITY CONTROL, AND IT NAMES THE MECHANISM RATHER THAN THE SYMPTOM. A
	// fixture whose root merely contains a `[` proves nothing unless the pattern the deleted
	// code built is one `filepath.Match` actually refuses — otherwise this row is a second
	// sample of the ordinary case and would pass at the broken commit.
	if _, err := os.Stat(filepath.Join(cache, "alpha-notes", "one.md")); err != nil {
		t.Fatalf("the fixture never wrote the entry: %v", err)
	}
	if _, err := filepath.Glob(filepath.Join(cache, "*", "*.md")); !errors.Is(err, filepath.ErrBadPattern) {
		t.Fatalf("the fixture does not reach the defect — globbing %q gave err=%v, so the "+
			"deleted code would have found the file anyway", cache, err)
	}

	code, stdout, stderr := capture(t, LsEntries, readOpts())

	if code != ExitOK {
		t.Fatalf("a readable host exits 0, got %d\n%s", code, stderr)
	}
	got := lsLines(stdout)
	want := []string{"alpha-notes/one.md"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("got %v, want %v — a glob metacharacter in the CACHE ROOT emptied the "+
			"listing, so the verb that advertises what the cache holds claimed it holds "+
			"nothing", got, want)
	}
}

// TestLsEntriesVisitsDotNamedAndSymlinkedScopeDirectories pins the SET the replaced glob
// produced, which is the half "same listing, different walk" cannot be asserted without.
//
// 🔴 `d.IsDir()` OFF THE DIRENT IS THE OBVIOUS SPELLING AND IT IS WRONG. A `DirEntry` for a
// symlinked scope directory reports `ModeSymlink`, not a directory, so every symlinked scope
// would drop out of the listing silently; `os.Stat` FOLLOWS the link, which is what
// `filepath.Glob` did when it read the middle `*`'s matches as directories. MEASURED as a
// mutant (`_ = info` + `!d.IsDir()`): it compiles, and every other row in this file stays
// GREEN — this row is the only thing that fails.
//
// ⚠ THE DOT-NAMED SCOPE IS THE SAME CLAIM ON THE OTHER AXIS. Go's `*` matches a leading dot,
// so the glob visited `.hidden-notes`; `os.ReadDir` lists it too, and nothing here filters
// dot-names.
//
// ⚠ THIS WHOLE ROW IS AN INVARIANT GUARD, NOT REGRESSION COVERAGE, AND IS LABELLED AS ONE:
// it PASSES at `a41dd02`, because the glob visited both directories too. That is the point —
// it pins that the replacement did not narrow the set. What it is for is the mutant above,
// which no other row in this file catches.
//
// ⚠ NOT A CLAIM THAT SUCH A CACHE IS REACHABLE. `/snapshot` builds neither, and whether a
// symlinked scope directory can arrive in a real cache is not established here. It is pinned
// because the comment on `LsEntries` asserts the set is unchanged, and an unpinned assertion
// about behaviour is a sentence, not a guard.
func TestLsEntriesVisitsDotNamedAndSymlinkedScopeDirectories(t *testing.T) {
	home := oneInstanceHost(t)
	cache := filepath.Join(home, ".cache", "subsystem-store")

	if err := os.MkdirAll(filepath.Join(cache, ".hidden-notes"), 0o755); err != nil {
		t.Fatal(err)
	}
	seedNamedEntry(t, cache, ".hidden-notes", "dotty.md", "dotty")

	external := t.TempDir()
	if err := os.MkdirAll(filepath.Join(external, "linked-notes"), 0o755); err != nil {
		t.Fatal(err)
	}
	seedNamedEntry(t, external, "linked-notes", "viafile.md", "viafile")
	if err := os.Symlink(filepath.Join(external, "linked-notes"),
		filepath.Join(cache, "linked-notes")); err != nil {
		t.Fatal(err)
	}

	// 🔴 THE REACHABILITY CONTROL, AND IT IS THE DISCRIMINATOR ITSELF: the dirent for
	// `linked-notes` must report NOT-a-directory, or `d.IsDir()` and `os.Stat` agree here
	// and this row measures nothing.
	dirents, err := os.ReadDir(cache)
	if err != nil {
		t.Fatal(err)
	}
	var linked os.DirEntry
	for _, d := range dirents {
		if d.Name() == "linked-notes" {
			linked = d
		}
	}
	if linked == nil {
		t.Fatalf("the fixture never created the symlinked scope: %v", dirents)
	}
	if linked.IsDir() {
		t.Fatalf("the dirent for a symlinked scope reports a directory on this filesystem, "+
			"so this row cannot tell `d.IsDir()` from `os.Stat`: type=%v", linked.Type())
	}

	code, stdout, stderr := capture(t, LsEntries, readOpts())

	if code != ExitOK {
		t.Fatalf("a readable host exits 0, got %d\n%s", code, stderr)
	}
	got := lsLines(stdout)
	want := []string{".hidden-notes/dotty.md", "alpha-notes/one.md", "linked-notes/viafile.md"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("got %v, want %v — the walk visits fewer scope directories than the glob "+
			"it replaced", got, want)
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
//
// 🔴 TWO OF THE ROWS ARE PATH-SHAPED, IN BOTH DIRECTIONS. The predicate's doc comment
// promises that a caller which has NOT globbed — a directory walk, an archive member list —
// asks the same question and gets the same answer; `internal/snapshot` builds its member
// names as `scope + "/" + name`, the shape `ls-entries` prints, so that caller is real.
// `notes/README.md` must be REFUSED and `notes/widget-cfg.md` TAKEN, or the promise holds in
// one direction only and the sheet is classified as an entry the moment somebody keeps it.
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
		{"notes/README.md", false,
			"the policy sheet is NOT an entry however it is addressed — an un-globbed " +
				"caller holds `scope/name` (an archive member list is exactly that shape), " +
				"so the rule compares the BASE name"},
		{"notes/widget-cfg.md", true,
			"the same path shape in the OTHER direction: an ordinary entry addressed by " +
				"path is still an entry, so base-name handling is pinned both ways"},
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
