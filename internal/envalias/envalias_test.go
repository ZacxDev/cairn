package envalias

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// 🔴 WHAT THESE TESTS ARE AND ARE NOT, SAID RATHER THAN LEFT TO BE INFERRED.
//
// Almost everything here is an INVARIANT GUARD on new code, not regression coverage: this
// package did not exist before the rename, so "red on pre-change code" is satisfied
// trivially by the package being absent and proves nothing about a defect. The one thing
// they buy that nothing else does is the SHAPE — the sort order, the shadow rule, the
// once-per-process rule — each of which is a property the parity harness can only observe
// as "the bytes differed", after the fact and without saying which rule broke.
//
// The behavioural claim — that the two languages emit the SAME BYTES in the SAME ORDER —
// is NOT made here and cannot be. `tests/test_env_aliases.py` pins the ledger and the
// warning TEXT across the two spellings; `tests/parity/harness.py` runs both real clients
// with a deprecated name exported and diffs their stderr byte-for-byte, which is the only
// instrument that can see a Go-side argument-ORDER mistake that still renders well-formed
// English.

func TestTheLedgerIsSortedByNewName(t *testing.T) {
	// 🔴 NOT COSMETIC. `Deprecations` sorts by new name so that two clients emitting the
	// same set emit it in the same order; if the LEDGER were also relied on for order
	// anywhere, an out-of-place row would be a stderr diff found by the parity harness
	// rather than here. Pinning it at the source keeps the two statements of the order —
	// the literal and the sort — from disagreeing.
	for i := 1; i < len(Ledger); i++ {
		if Ledger[i-1].New >= Ledger[i].New {
			t.Fatalf("ledger is not sorted by new name: %q then %q",
				Ledger[i-1].New, Ledger[i].New)
		}
	}
	if len(Ledger) == 0 {
		t.Fatal("empty ledger: every assertion in this file would pass vacuously")
	}
}

func TestEveryPairIsDistinctInBothColumns(t *testing.T) {
	seenNew, seenOld := map[string]bool{}, map[string]bool{}
	for _, p := range Ledger {
		if seenNew[p.New] {
			t.Errorf("duplicate new name %q", p.New)
		}
		if seenOld[p.Old] {
			// Two new names sharing one old name would make `Deprecations` emit the old
			// name twice with different replacements — a warning pair that contradicts
			// itself.
			t.Errorf("duplicate old name %q", p.Old)
		}
		seenNew[p.New], seenOld[p.Old] = true, true
	}
}

func TestTheNewNameWins(t *testing.T) {
	env := map[string]string{"CAIRN_URL": "new", "SUBSYSTEM_STORE_URL": "old"}
	if got := Value(env, "CAIRN_URL"); got != "new" {
		t.Fatalf("shadowed old name won: %q", got)
	}
}

func TestTheOldNameIsReadWhenTheNewOneIsAbsentOrBlank(t *testing.T) {
	// Two points on the "is it set" dimension, because "absent" and "present but blank"
	// are different states and the rule names both. A reader treating them differently is
	// exactly the half-migration this ledger exists to prevent.
	for _, tc := range []struct {
		name string
		env  map[string]string
	}{
		{"absent", map[string]string{"SUBSYSTEM_STORE_URL": "old"}},
		{"blank", map[string]string{"CAIRN_URL": "", "SUBSYSTEM_STORE_URL": "old"}},
		{"whitespace", map[string]string{"CAIRN_URL": "   ", "SUBSYSTEM_STORE_URL": "old"}},
	} {
		if got := Value(tc.env, "CAIRN_URL"); got != "old" {
			t.Errorf("%s: got %q, want the old name's value", tc.name, got)
		}
	}
}

func TestANameThatWasNeverRenamedResolvesAsItself(t *testing.T) {
	// `cmd/cairn-ui` passes `CAIRN_UI_PORT` through the same readers as `CAIRN_PORT`.
	// If an un-aliased name did anything other than plain single-name lookup, every one
	// of those call sites would silently stop reading its variable.
	if got := Value(map[string]string{"CAIRN_UI_PORT": "8103"}, "CAIRN_UI_PORT"); got != "8103" {
		t.Fatalf("un-aliased name did not resolve as itself: %q", got)
	}
	if oldName("CAIRN_UI_PORT") != "" {
		t.Fatal("oldName() invented an alias for a name that was never renamed")
	}
}

func TestDeprecationsAreSortedByNewNameRegardlessOfInsertionOrder(t *testing.T) {
	// A Go map has no order, so `Deprecations` iterating `olds` without sorting would
	// produce a DIFFERENT order on different runs of the same binary — which reads as a
	// flaky parity diff rather than as a missing sort.
	env := map[string]string{}
	for _, p := range Ledger {
		env[p.Old] = "set"
	}
	lines := Deprecations(env)
	if len(lines) != len(Ledger) {
		t.Fatalf("got %d lines for %d deprecated names", len(lines), len(Ledger))
	}
	for i, p := range Ledger {
		if !strings.Contains(lines[i], "$"+p.New+" ") {
			t.Fatalf("line %d is not the %s row: %q", i, p.New, lines[i])
		}
	}
}

func TestAShadowedOldNameStillWarns(t *testing.T) {
	// 🔴 THE RULE THAT IS EASIEST TO GET WRONG, BECAUSE THE OBVIOUS IMPLEMENTATION —
	// warn from inside `Value`, on the branch that actually falls through — gets it
	// backwards: it goes SILENT exactly when the operator has set both names and most
	// needs to know the old one is still exported somewhere.
	env := map[string]string{"CAIRN_URL": "new", "SUBSYSTEM_STORE_URL": "old"}
	if lines := Deprecations(env); len(lines) != 1 {
		t.Fatalf("a shadowed old name produced %d warnings, want 1: %v", len(lines), lines)
	}
}

func TestABlankOldNameIsNotADeprecation(t *testing.T) {
	// `FOO=` is how a caller UNSETS an alias for a child process. It changes no
	// resolution, so warning about it would be noise on a correct configuration.
	//
	// 🔴 "CHANGES NO RESOLUTION" IS THE HALF THAT USED TO BE FALSE, AND IT IS WHY THE
	// WHITESPACE ROW IS HERE. `Deprecations` tested blankness with `TrimSpace`; the
	// resolver returned the old name's value RAW. So `SUBSYSTEM_STORE_ROOT="  "` resolved
	// to `"  "` — a value the pod would have used as a store root — while emitting no
	// warning at all, which is the one combination the sentence above rules out. The two
	// halves now read blankness through the same `blank()` predicate; see
	// `TestABlankOldNameResolvesAsABSENT`, which is the resolution half of this pair.
	for _, tc := range []struct {
		name  string
		value string
	}{
		{"empty", ""},
		{"whitespace", "  "},
		{"tab", "\t"},
	} {
		if lines := Deprecations(map[string]string{"SUBSYSTEM_STORE_URL": tc.value}); len(lines) != 0 {
			t.Errorf("%s: a blank old name warned: %v", tc.name, lines)
		}
	}
}

// TestABlankOldNameResolvesAsABSENT is the resolution half of the pair above, and it is
// REGRESSION coverage rather than an invariant guard.
//
// 🔴 HOW THE BASELINE WAS OBTAINED, BECAUSE THE SHA THIS COMMENT USED TO NAME DOES NOT
// EXIST. It said "RED at `78679b9` (this branch's own pre-fix state)". That was a local WIP
// commit, discarded by a later soft reset and unreachable in every clone —
// `git for-each-ref --contains 78679b9` returns nothing — so the matrix could not be
// checked by anybody.
//
// The pre-change resolver is `envalias.go` at `e878f4c`, the commit before blankness became
// one predicate. What was run: `git archive e878f4c` into a scratch tree, THIS file copied
// over it, `go test ./internal/envalias/ -run TestABlankOldName`.
//
//	e878f4c  FAIL — `whitespace` and `tab` both ways: `Value` returned "  " / "\t",
//	         and `OSValueOr` returned them instead of the fallback
//	HEAD     ok
//
// The `empty` row is green at both ends, and is kept as the control that says the fix did
// not simply invert the predicate. ⚠ It is TODAY'S test against the OLD resolver: the file
// at `e878f4c` has no whitespace rows, so "the package was red there" would be a different
// and weaker claim.
//
// 🔴 THE FIXTURE VALUES ARE PAIRWISE DISTINCT AND NONE OF THEM IS THE FALLBACK. A mutant
// that hardcoded `""` for every old-name read would satisfy this and die in
// `TestTheOldNameIsReadWhenTheNewOneIsAbsentOrBlank`, whose expected value is `"old"`.
func TestABlankOldNameResolvesAsABSENT(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value string
	}{
		{"empty", ""},
		{"whitespace", "  "},
		{"tab", "\t"},
	} {
		if got := Value(map[string]string{"SUBSYSTEM_STORE_URL": tc.value}, "CAIRN_URL"); got != "" {
			t.Errorf("%s: a blank OLD name resolved to %q; it must read as ABSENT, the "+
				"same way a blank NEW name does, or `OSValueOr`'s fallback never runs and "+
				"the pod takes a whitespace store root", tc.name, got)
		}
		// And through `OSValueOr`, which is the shape `cmd/cairn-server` actually reads
		// its store root with: the FALLBACK has to be what comes back, not the blank. A
		// fix applied to `Value` alone but not reached by the `os.Getenv` path would pass
		// the assertion above and still ship the defect.
		t.Setenv("SUBSYSTEM_STORE_URL", tc.value)
		t.Setenv("CAIRN_URL", "")
		if got := OSValueOr("CAIRN_URL", "fallback"); got != "fallback" {
			t.Errorf("%s: OSValueOr returned %q, want the fallback", tc.name, got)
		}
	}
}

func TestWarnOnceEmitsEachDistinctLineOnce(t *testing.T) {
	ResetWarnedForTest()
	var got []string
	emit := func(line string) { got = append(got, line) }
	lines := Deprecations(map[string]string{"SUBSYSTEM_STORE_URL": "x"})
	WarnOnce(lines, emit)
	WarnOnce(lines, emit)
	if len(got) != 1 {
		t.Fatalf("emitted %d times, want 1: %v", len(got), got)
	}
}

func TestTheFileWarningNamesTheFileAndTheEnvWarningNamesTheVariable(t *testing.T) {
	// The two texts are deliberately different, and the difference is the remedy they
	// point at: one says "unset a variable", the other says "edit this file".
	pair := Pair{New: "CAIRN_URL", Old: "SUBSYSTEM_STORE_URL"}
	env := EnvWarning(pair)
	file := FileWarning(pair, "/tmp/synthetic/env")
	if !strings.HasPrefix(env, "$SUBSYSTEM_STORE_URL ") {
		t.Errorf("env warning does not lead with the exported name: %q", env)
	}
	if !strings.Contains(file, "/tmp/synthetic/env") {
		t.Errorf("file warning does not name the file: %q", file)
	}
	if strings.Contains(file, "$") {
		t.Errorf("file warning spells a variable, which sends the reader to the wrong "+
			"place — the string lives in a file they have to edit: %q", file)
	}
	for _, line := range []string{env, file} {
		if !strings.Contains(line, RemovalAnchor) {
			t.Errorf("warning does not say when the alias stops being read: %q", line)
		}
	}
}

func TestFileDeprecationsAreSortedAndSkipBlankKeys(t *testing.T) {
	fromFile := map[string]string{
		"SUBSYSTEM_STORE_URL":   "http://127.0.0.1:1",
		"SUBSYSTEM_STORE_TOKEN": "",
	}
	lines := FileDeprecations(fromFile, "/tmp/synthetic/env")
	if len(lines) != 1 || !strings.HasPrefix(lines[0], "SUBSYSTEM_STORE_URL ") {
		t.Fatalf("want exactly the URL row, got %v", lines)
	}
}

// TestNoServingCodeSpellsADeprecatedName is the SEAM guard: it asserts a RELATIONSHIP —
// "this package is the only place an old spelling appears" — rather than a property of
// any one file.
//
// 🔴 IT IS WHAT MAKES "ONE RULE, ONE PLACE" CHECKABLE RATHER THAN ASPIRATIONAL. A call
// site that open-codes `env["SUBSYSTEM_STORE_TOKEN"]` alongside the resolver passes every
// other test in this file and every behavioural test in the tree — both names work, after
// all — while quietly not warning, not honouring the new name's precedence, and not
// moving when the ledger does. The failure is invisible in exactly the way this repo's
// rules say a duplicated predicate is.
//
// ⚠ IT READS SOURCE, SO IT IS BLIND TO A NAME ASSEMBLED AT RUNTIME
// (`"SUBSYSTEM_STORE_" + suffix`). That shape does not exist in this tree and would be a
// bizarre thing to write; the guard is not widened to chase it, and the limit is stated
// here rather than discovered later.
func TestNoServingCodeSpellsADeprecatedName(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	offenders, scanned, walkErr := scanForDeprecatedSpellings(root)
	if walkErr != nil {
		t.Fatal(walkErr)
	}

	// 🔴 THE POSITIVE CONTROL. A walk that matched nothing — a wrong suffix, a SkipDir
	// that ate the tree, an empty ledger — reports a reassuring zero indistinguishable
	// from a clean tree. `cmd/cairn-server/main.go` alone is dozens of string literals,
	// so a scan that saw fewer than a handful of FILES did not run.
	if scanned < 10 {
		t.Fatalf("the scan visited %d files; it is not reading the tree, so its zero "+
			"says nothing about the tree", scanned)
	}
	if len(offenders) > 0 {
		t.Fatalf("serving code spells a deprecated name instead of going through "+
			"envalias: %v", offenders)
	}
}

// scanForDeprecatedSpellings is the walk behind [TestNoServingCodeSpellsADeprecatedName],
// taking its root as a parameter so a synthetic tree can be scanned. It returns the
// offenders it found and how many files it actually read.
//
// 🔴 IT EXISTS AS A FUNCTION BECAUSE THE GUARD COULD NOT OTHERWISE BE TESTED, AND IT
// COULD NOT OTHERWISE BE TESTED BECAUSE IT WALKED THE REAL REPOSITORY ROOT. A guard whose
// only input is "the tree this test happens to be running in" cannot be shown to behave
// correctly on a tree somebody constructs — which is exactly the case that broke it.
//
// 🔴 A NESTED CHECKOUT OF THIS REPOSITORY IS NOT THIS REPOSITORY, AND THE WALK COULD NOT
// TELL THE DIFFERENCE. Agent worktrees are created under `.claude/worktrees/<id>/`, i.e.
// INSIDE the tree this walks. Every such worktree carries its own
// `internal/envalias/envalias.go` — the ledger, whose whole job is to spell the
// deprecated names — so the guard reported each of them as an offender and went RED with
// 24 findings naming real source files, on a tree with no defect in it at all. It is a
// FALSE POSITIVE of the worst kind: it looks exactly like a true one, and the honest
// reading of its message is "serving code spells a deprecated name", which was false.
//
// ⚠ CI NEVER SAW IT, WHICH IS WHY IT SURVIVED: `.claude/` is untracked, so a fresh
// checkout has no worktrees in it and the guard is green. It reddens only on a developer's
// own clone — the one place a red is most likely to be read as "the tree is broken" and
// worked around.
//
// TWO INDEPENDENT HOLES ARE CLOSED HERE, DELIBERATELY BOTH, BECAUSE EITHER ALONE LEAVES A
// REAL CASE OPEN:
//
//   - `.claude` is skipped. That is where this harness puts agent worktrees, so it is the
//     directory the defect actually came from. It does NOT generalise: a worktree made
//     anywhere else under the root is still walked.
//   - the ledger exemption is a path SUFFIX rather than an equality against
//     `<root>/internal/envalias`. That is what makes the exemption mean "this file is the
//     ledger" instead of "this file is THE ledger of the root I was handed", so a ledger
//     reached through any nesting is exempt. It is the half that covers a checkout the
//     first bullet does not know about.
//
// ⚠ AND NEITHER IS A NARROWING OF WHAT THE GUARD CHECKS IN THIS TREE. `.claude/` holds no
// serving code — it is harness state — and the suffix exemption exempts exactly the files
// the equality already exempted, plus copies of them. `uiaudit/` is deliberately still
// walked: it is a nested Go module, so skipping nested modules wholesale would have been
// the tidier-looking fix and would have silently dropped it from the guard's reach.
func scanForDeprecatedSpellings(root string) (offenders []string, scanned int, err error) {
	olds := map[string]bool{}
	for _, p := range Ledger {
		olds[p.Old] = true
	}
	// The ledger's own directory, as a path suffix. `filepath.Join` rather than a literal
	// so the separator is the platform's.
	ledgerDir := filepath.Join("internal", "envalias")

	walkErr := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			// `tests/` holds harnesses that deliberately export the old names to
			// exercise the alias; they are not serving code. `.claude/` is agent
			// harness state and holds nested CHECKOUTS of this repository — see this
			// function's comment for what that cost.
			if name := info.Name(); name == ".git" || name == "tests" || name == ".claude" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		if dir := filepath.Dir(path); dir == ledgerDir || strings.HasSuffix(dir, string(filepath.Separator)+ledgerDir) {
			return nil // the ledger itself, which is the one place a name may be spelled
		}
		scanned++
		src, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		fset := token.NewFileSet()
		file, parseErr := parser.ParseFile(fset, path, src, 0)
		if parseErr != nil {
			return parseErr
		}
		ast.Inspect(file, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			text, unqErr := strconv.Unquote(lit.Value)
			if unqErr != nil {
				return true
			}
			for old := range olds {
				if strings.Contains(text, old) {
					rel, _ := filepath.Rel(root, path)
					offenders = append(offenders, rel+": "+old)
				}
			}
			return true
		})
		return nil
	})
	return offenders, scanned, walkErr
}

// TestTheScanIgnoresANestedCheckoutOfThisRepository is the regression guard for the false
// positive described on [scanForDeprecatedSpellings].
//
// 🔴 IT BUILDS THE TREE THAT BROKE IT RATHER THAN ASSERTING THE SKIP LIST. A guard on the
// skip list would be a guard on a SPELLING — satisfied by any code that happens to name
// `.claude`, and blind to the second hole entirely, because the suffix exemption is not a
// skip. What this asserts is the STATE: a root containing a nested checkout scans clean,
// and it would not have before this change.
//
// ⚠ THE TWO HOLES ARE EXERCISED SEPARATELY, because either fix alone would make a
// single-case test green and the other hole would ship. `.claude/worktrees/<id>/` is the
// first; `vendor-copy/<id>/` is a nesting the skip list does NOT know about, and only the
// suffix exemption saves it.
func TestTheScanIgnoresANestedCheckoutOfThisRepository(t *testing.T) {
	if len(Ledger) == 0 {
		t.Fatal("the ledger is empty, so this test's fixtures spell nothing and it proves nothing")
	}
	old := Ledger[0].Old

	root := t.TempDir()
	write := func(rel, body string) {
		t.Helper()
		full := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// Enough ordinary serving files that the scan's own "did I read anything" floor is
	// satisfied by the REAL tree rather than by the nested copies.
	for i := range 12 {
		write(filepath.Join("internal", "svc", "f"+strconv.Itoa(i)+".go"),
			"package svc\n\nvar Name = \"CAIRN_TOKEN\"\n")
	}
	// The root's own ledger. Exempt, and it is what the exemption is FOR.
	write(filepath.Join("internal", "envalias", "envalias.go"),
		"package envalias\n\nvar Old = \""+old+"\"\n")
	// Hole 1: an agent worktree, where this harness actually puts them. 🔴 THE
	// DISCRIMINATING FILE HERE IS `svc.go`, NOT THE LEDGER COPY — and that is a
	// MEASURED correction, not a precaution. With only the ledger copy present this
	// case passed even with the `.claude` skip REMOVED, because the suffix exemption
	// already exempts a ledger reached through any path: the skip never executed and
	// the assertion was unreachable. A worktree sitting on a DIFFERENT branch can hold
	// ordinary serving code that still spells an old name, and nothing but the skip
	// keeps that out.
	write(filepath.Join(".claude", "worktrees", "agent-0", "internal", "envalias", "envalias.go"),
		"package envalias\n\nvar Old = \""+old+"\"\n")
	write(filepath.Join(".claude", "worktrees", "agent-0", "internal", "svc", "svc.go"),
		"package svc\n\nvar Stale = \""+old+"\"\n")
	// Hole 2: a nested checkout somewhere the skip list does not name.
	write(filepath.Join("vendor-copy", "agent-1", "internal", "envalias", "envalias.go"),
		"package envalias\n\nvar Old = \""+old+"\"\n")

	offenders, scanned, err := scanForDeprecatedSpellings(root)
	if err != nil {
		t.Fatalf("the scan failed: %v", err)
	}
	if scanned < 10 {
		t.Fatalf("the scan visited %d file(s); it is not reading the fixture tree, so a clean "+
			"result here says nothing", scanned)
	}
	if len(offenders) != 0 {
		t.Errorf("a tree whose ONLY deprecated spellings are in ledger files — the root's own and "+
			"two inside nested checkouts — reported %d offender(s): %v. A nested checkout of this "+
			"repository is not this repository, and its ledger is not serving code.",
			len(offenders), offenders)
	}

	// 🔴 THE POSITIVE CONTROL, AND IT IS WHAT STOPS THE ASSERTION ABOVE BEING VACUOUS. A
	// scan that had been broken into finding nothing at all — a SkipDir that ate the tree,
	// an exemption widened until it exempts everything — would satisfy every line above.
	// Real serving code spelling a deprecated name must still be caught.
	write(filepath.Join("internal", "svc", "leak.go"),
		"package svc\n\nvar Bad = \""+old+"\"\n")
	offenders, _, err = scanForDeprecatedSpellings(root)
	if err != nil {
		t.Fatalf("the control scan failed: %v", err)
	}
	if len(offenders) != 1 {
		t.Fatalf("POSITIVE CONTROL FAILED: serving code spelling %q was reported %d time(s), want "+
			"exactly 1 (%v). The scan is not finding what it exists to find, so its zero above is "+
			"a fact about the scan rather than about the tree.", old, len(offenders), offenders)
	}
}
