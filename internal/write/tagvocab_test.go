package write

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"
)

// taggedEntry is one entry body carrying exactly the `tags:` line given.
//
// ⚠ IT TAKES THE RAW FLOW-SEQUENCE TEXT, NOT A SLICE, so a case can write the UNFOLDED
// spelling an operator would type (`Infra`, `Client_Work`) and measure that the fold
// happens before the vocabulary comparison. A `[]string` here would have folded in the
// helper and made every fold case vacuous.
func taggedEntry(tagsFlow string) string {
	return "---\n" +
		"service: gadget-one\n" +
		"scope: alpha-notes\n" +
		"tags: [" + tagsFlow + "]\n" +
		"---\n" +
		"\n" +
		"## What it is\n" +
		"A synthetic entry.\n" +
		"\n" +
		"## Nuance / work-history\n" +
		"- 2000-01-02: the readiness probe reports ready 40s before it is.\n"
}

// wantRefusal is the refusal the write path must answer, spelled out here rather than
// built from `tagVocabulary` and the format string in `validateEntryBytes`.
//
// 🔴 A LITERAL, BECAUSE AN EXPECTATION DERIVED FROM THE IMPLEMENTATION ASSERTS NOTHING.
// Interpolating `strings.Join(tagVocabulary, "|")` here would make this test pass under
// a mutant that reorders the vocabulary, drops a term, or changes the separator — the
// exact three edits the conformance corpus would then catch on the oracle instead, one
// gate and several minutes later.
const wantRefusal = "tag 'marketing' is not one of client-work|infra|product|tooling — " +
	"the tag vocabulary is CLOSED on the WRITE path, so widening it is a code change. " +
	"The index loader still READS this tag: an entry already carrying it is unaffected"

// TestTheWritePathRefusesATagOutsideTheDeclaredVocabulary is the gate, measured at BOTH
// write primitives.
//
// 🔴 THE RED/GREEN MATRIX. Red at `245b568` (the base this branch forked from): this file
// does not compile there, because `tagOutsideVocabulary` does not exist — the weak red a
// new symbol always produces. The INFORMATIVE red is the one that keeps the shape and
// removes the behaviour: deleting the four-line `tagOutsideVocabulary` block from
// `validateEntryBytes` while leaving everything else in place. Measured with that
// mutation, `CreateEntry` returned a nil error and wrote the file, `ReplaceEntry`
// returned a nil error and replaced it, and the two `EveryDeclaredTag` subtests kept
// PASSING — which is what attributes the failure to the vocabulary check and not to the
// loader validation it sits behind. Green at HEAD. Both reds were run; the second is the
// one that attributes.
//
// 🔴 AND IT ASSERTS THE STORE DID NOT MOVE, NOT ONLY THAT AN ERROR CAME BACK. A refusal
// that has already written the bytes is the failure this whole design exists to prevent,
// and an error return is not evidence about the filesystem.
func TestTheWritePathRefusesATagOutsideTheDeclaredVocabulary(t *testing.T) {
	t.Run("create refuses and leaves the name FREE", func(t *testing.T) {
		dir := t.TempDir()
		target := filepath.Join(dir, "alpha-notes", "gadget-one.md")
		_, err := CreateEntry(target, []byte(taggedEntry("marketing")), "alpha-notes",
			"gadget-one.md", nil)
		var shape *EntryShapeError
		if !errors.As(err, &shape) {
			t.Fatalf("a create carrying an off-vocabulary tag was not refused as an entry "+
				"shape error: %v", err)
		}
		if shape.Error() != wantRefusal {
			t.Fatalf("the refusal is not the declared sentence.\nwant: %s\ngot:  %s",
				wantRefusal, shape.Error())
		}
		// 🔴 THE NAME MUST STILL BE FREE. `CreateEntry` validates BEFORE it claims the
		// name precisely so a caller that fixes its body can retry into the same ref; a
		// refusal that left a zero-byte file there would answer 412 `already-exists` on
		// the retry, which reads as "somebody else took it".
		if _, statErr := os.Stat(target); !os.IsNotExist(statErr) {
			t.Fatalf("a refused create left something at %s (stat err %v)", target, statErr)
		}
	})

	t.Run("replace refuses and leaves the BYTES intact", func(t *testing.T) {
		original := taggedEntry("infra")
		path := writeEntryFile(t, original)
		rev := EntryRevision([]byte(original))
		_, err := ReplaceEntry(path, []byte(taggedEntry("marketing")), []string{rev},
			"alpha-notes", "gadget-one.md", nil)
		var shape *EntryShapeError
		if !errors.As(err, &shape) {
			t.Fatalf("a replace carrying an off-vocabulary tag was not refused as an entry "+
				"shape error: %v", err)
		}
		if shape.Error() != wantRefusal {
			t.Fatalf("the refusal is not the declared sentence.\nwant: %s\ngot:  %s",
				wantRefusal, shape.Error())
		}
		after, readErr := os.ReadFile(path)
		if readErr != nil {
			t.Fatal(readErr)
		}
		if string(after) != original {
			t.Fatalf("a refused replace changed the file on disk:\n%q", string(after))
		}
	})

	t.Run("the FIRST offender in the folded order is the one named", func(t *testing.T) {
		// 🔴 THE ENTRY'S TAG SET IS SORTED BY THE LOADER, so `zeta-tag` and `marketing`
		// together must always name `marketing` — the alphabetically first — whatever
		// order the file wrote them in. Both orderings are sent, because "it reported the
		// one I put first in the file" and "it reported the first in sorted order" are
		// different rules that agree on every single-offender case.
		for _, flow := range []string{"zeta-tag, marketing", "marketing, zeta-tag"} {
			dir := t.TempDir()
			_, err := CreateEntry(filepath.Join(dir, "alpha-notes", "gadget-one.md"),
				[]byte(taggedEntry(flow)), "alpha-notes", "gadget-one.md", nil)
			if err == nil {
				t.Fatalf("tags: [%s] was accepted", flow)
			}
			if err.Error() != wantRefusal {
				t.Fatalf("tags: [%s] named a different offender.\nwant: %s\ngot:  %s",
					flow, wantRefusal, err.Error())
			}
		}
	})
}

// TestEveryDeclaredTagIsAcceptedByBothWritePrimitives is the POSITIVE CONTROL, and it is
// the half that makes the refusal above a measurement.
//
// 🔴 A GATE THAT REFUSES EVERYTHING PASSES THE TEST ABOVE. `validateEntryBytes` returning
// a canned `EntryShapeError` for every body — or a `tagOutsideVocabulary` that inverted
// its own condition — is green there and catastrophic here. So each declared term is
// written through BOTH primitives and the write is observed to LAND: a new file on disk
// for the create, changed bytes for the replace.
//
// ⚠ IT WALKS THE VOCABULARY RATHER THAN NAMING THE FOUR TERMS, WHICH IS THE ONE PLACE
// DERIVING FROM THE IMPLEMENTATION IS CORRECT. The claim here is "every declared term is
// writable", not "these four terms are the declared ones" — that second claim is
// `TestTheVocabularyIsExactlyTheDeclaredFourTerms`'s, with literals, and
// `tests/test_tag_vocabulary.py`'s across the two languages.
func TestEveryDeclaredTagIsAcceptedByBothWritePrimitives(t *testing.T) {
	if len(tagVocabulary) == 0 {
		t.Fatal("the vocabulary is EMPTY, so every subtest below would be vacuous")
	}
	for _, tag := range tagVocabulary {
		t.Run("create/"+tag, func(t *testing.T) {
			dir := t.TempDir()
			target := filepath.Join(dir, "alpha-notes", "gadget-one.md")
			rev, err := CreateEntry(target, []byte(taggedEntry(tag)), "alpha-notes",
				"gadget-one.md", nil)
			if err != nil {
				t.Fatalf("a declared tag was refused on create: %v", err)
			}
			if rev == "" {
				t.Fatal("the create returned no revision")
			}
			landed, readErr := os.ReadFile(target)
			if readErr != nil {
				t.Fatalf("the create reported success and wrote nothing: %v", readErr)
			}
			if string(landed) != taggedEntry(tag) {
				t.Fatalf("the create wrote different bytes:\n%q", string(landed))
			}
		})
		t.Run("replace/"+tag, func(t *testing.T) {
			path := writeEntryFile(t, entryBody)
			rev := EntryRevision([]byte(entryBody))
			body := taggedEntry(tag)
			if _, err := ReplaceEntry(path, []byte(body), []string{rev}, "alpha-notes",
				"gadget-one.md", nil); err != nil {
				t.Fatalf("a declared tag was refused on replace: %v", err)
			}
			landed, readErr := os.ReadFile(path)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if string(landed) != body {
				t.Fatalf("the replace reported success and left other bytes:\n%q", string(landed))
			}
		})
	}
	// ⚠ AND AN ENTRY WITH NO `tags:` AT ALL, which is every entry in the store today. A
	// vocabulary check that treated "no tags" as "not in the vocabulary" would refuse
	// every write in the repository and pass every subtest above.
	dir := t.TempDir()
	if _, err := CreateEntry(filepath.Join(dir, "alpha-notes", "gadget-one.md"),
		[]byte(entryBody), "alpha-notes", "gadget-one.md", nil); err != nil {
		t.Fatalf("an entry carrying NO `tags:` was refused: %v", err)
	}
}

// TestTheVocabularyComparisonSeesTheFOLDEDTag pins that the gate reads the loader's
// folded set and never the raw front matter.
//
// 🔴 THE FOLD IS WHY THIS IS NOT A CASE-SENSITIVE ALLOWLIST. An operator writes
// `tags: [Infra]` or `tags: [Client_Work]`; `NormalizeRef` lowercases and folds `_` to
// `-`, so both are declared terms by the time the comparison happens. A gate that
// compared the raw strings would refuse the spelling most people type, and the refusal
// would name a tag the store would never have held.
func TestTheVocabularyComparisonSeesTheFOLDEDTag(t *testing.T) {
	accepted := []string{"Infra", " infra ", "INFRA", "Client_Work", "client_work"}
	for _, raw := range accepted {
		dir := t.TempDir()
		if _, err := CreateEntry(filepath.Join(dir, "alpha-notes", "gadget-one.md"),
			[]byte(taggedEntry(raw)), "alpha-notes", "gadget-one.md", nil); err != nil {
			t.Errorf("tags: [%s] folds to a declared term and was refused: %v", raw, err)
		}
	}
	// …and the refusal names the FOLDED spelling, not what the file wrote, because the
	// folded form is the only one the store would ever have carried.
	dir := t.TempDir()
	_, err := CreateEntry(filepath.Join(dir, "alpha-notes", "gadget-one.md"),
		[]byte(taggedEntry("Not_Infra")), "alpha-notes", "gadget-one.md", nil)
	if err == nil {
		t.Fatal("tags: [Not_Infra] was accepted")
	}
	if !strings.Contains(err.Error(), "tag 'not-infra' is not one of") {
		t.Fatalf("the refusal does not name the FOLDED tag: %v", err)
	}
}

// TestAppendingToAnEntryCarryingAnOffVocabularyTagStillWorks is the anti-outage guard,
// and it is the reason the check is not in the reader.
//
// 🔴 THIS IS THE CASE THE WHOLE DESIGN IS FOR. Every entry that already carries an
// off-vocabulary tag must stay APPENDABLE: `POST .../bullets` is how the store is
// actually written, and a vocabulary refusal reachable from it would take the operator's
// existing notes out of service. `AppendBullet` deliberately does not call
// `validateEntryBytes` — see that function's caller ledger — and this is the behavioural
// half of that claim, next to the structural one.
func TestAppendingToAnEntryCarryingAnOffVocabularyTagStillWorks(t *testing.T) {
	path := writeEntryFile(t, taggedEntry("marketing, project-xyz"))
	status, line, rev, err := AppendBullet(path, "a synthetic observation.", "wide-reader",
		"sess-1", "2000-01-05", nil)
	if err != nil {
		t.Fatalf("appending to an entry carrying off-vocabulary tags failed: %v", err)
	}
	if status != "appended" {
		t.Fatalf("status=%q, want appended", status)
	}
	if !strings.Contains(line, "a synthetic observation.") {
		t.Fatalf("the appended line is wrong: %q", line)
	}
	if rev == "" {
		t.Fatal("the append returned no revision")
	}
	after, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if !strings.Contains(string(after), "a synthetic observation.") {
		t.Fatal("the append reported success and wrote nothing")
	}
	// ⚠ AND THE OFF-VOCABULARY TAGS ARE STILL THERE AFTERWARDS. An append that "fixed"
	// the front matter on the way past would be a silent rewrite of somebody's file.
	if !strings.Contains(string(after), "tags: [marketing, project-xyz]") {
		t.Fatalf("the append rewrote the front matter:\n%s", string(after))
	}
}

// TestTheVocabularyIsExactlyTheDeclaredFourTerms pins the set, its ORDER and its
// uniqueness.
//
// 🔴 THE ORDER IS PART OF THE WIRE CONTRACT, NOT TIDINESS. The refusal joins this slice
// with `|`, the oracle joins its own tuple the same way, and `tests/conformance/`
// compares the two servers' bytes. Sorted-and-unique is also what makes the refusal
// stable across runs.
//
// ⚠ AN INVARIANT GUARD, NOT REGRESSION COVERAGE: no defect ever reordered this slice.
// It is here so a fifth term added on one side is red in this package rather than three
// gates away.
func TestTheVocabularyIsExactlyTheDeclaredFourTerms(t *testing.T) {
	want := []string{"client-work", "infra", "product", "tooling"}
	if !slices.Equal(tagVocabulary, want) {
		t.Fatalf("tagVocabulary=%q, want %q — if a term was added or removed on purpose, "+
			"move `lib/entry_shape.py`'s TAG_VOCABULARY in the same commit and regenerate "+
			"`tests/conformance/` goldens", tagVocabulary, want)
	}
	if !sort.StringsAreSorted(tagVocabulary) {
		t.Fatal("tagVocabulary is not sorted, so the refusal message's order is accidental")
	}
	seen := map[string]bool{}
	for _, tag := range tagVocabulary {
		if seen[tag] {
			t.Fatalf("tagVocabulary carries %q twice", tag)
		}
		seen[tag] = true
	}
}

// TestTheWriteTimeValidatorHasExactlyTheDeclaredCallers is the SEAM guard, and it fails
// in BOTH directions on purpose.
//
// 🔴 THE HAZARD IS NOT "SOMEBODY DELETES THE CHECK" — IT IS THE CALLER SET MOVING.
// Shrinking it (a write primitive that stops validating, or a third one added that never
// starts) lands an off-vocabulary tag. GROWING it is the worse half and the less obvious
// one: `AppendBullet` looks like it belongs in this list — it writes entry bytes, under
// the same lock, through the same atomic helper — and adding it would make every append
// to an entry that already carries an off-vocabulary tag fail, which is precisely the
// outage the vocabulary was kept out of the reader to avoid. A ledger that only refused
// removals would wave that through.
//
// 🔴 IT COUNTS CALL SITES IN THE AST, NOT OCCURRENCES OF A STRING, so a mention in a
// comment or a doc block cannot satisfy it and cannot break it either.
//
// ⚠ MUTATION-TESTED IN THREE DIRECTIONS, each with its own observed failure: deleting the
// `validateEntryBytes` call from `CreateEntry` → "declares no call"; adding one to
// `AppendBullet` → "is not a declared caller"; renaming a declared caller → "declares no
// call" for the old name. Reported in the pull request with the exact messages.
func TestTheWriteTimeValidatorHasExactlyTheDeclaredCallers(t *testing.T) {
	// The LEDGER. Every function in this package that may hand caller-supplied
	// whole-file bytes to the write-time validator, and no other.
	declared := map[string]bool{"CreateEntry": true, "ReplaceEntry": true}

	fset := token.NewFileSet()
	pkg, err := parser.ParseDir(fset, ".", func(fi os.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatalf("could not parse internal/write: %v", err)
	}
	files := pkg["write"]
	if files == nil {
		t.Fatal("no `write` package parsed, so this ledger would be vacuous")
	}

	callers := map[string]int{}
	for _, file := range files.Files {
		for _, decl := range file.Decls {
			fn, isFunc := decl.(*ast.FuncDecl)
			if !isFunc || fn.Body == nil {
				continue
			}
			name := fn.Name.Name
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				call, isCall := n.(*ast.CallExpr)
				if !isCall {
					return true
				}
				if ident, isIdent := call.Fun.(*ast.Ident); isIdent &&
					ident.Name == "validateEntryBytes" {
					callers[name]++
				}
				return true
			})
		}
	}

	// POSITIVE CONTROL for the walker itself: a zero here is indistinguishable from an
	// AST walk wired to nothing, which is the failure mode this repository's rules name.
	if len(callers) == 0 {
		t.Fatal("the AST walk found NO call to `validateEntryBytes` anywhere in this " +
			"package. Either every write primitive stopped validating, or this walker is " +
			"broken — check the second before believing the first")
	}

	var undeclared []string
	for name := range callers {
		if !declared[name] {
			undeclared = append(undeclared, name)
		}
	}
	sort.Strings(undeclared)
	if len(undeclared) > 0 {
		t.Fatalf("%v call(s) `validateEntryBytes` and %s not a declared caller.\n"+
			"Adding a caller is a DECISION, not a tidy-up: the validator refuses a `tags:` "+
			"outside the closed vocabulary, so any function that starts calling it starts "+
			"failing on entries that already carry one. `AppendBullet` is the specific "+
			"mistake this guard exists for — an append must keep working on every entry "+
			"in the store, whatever its tags. If the addition is right, add it to "+
			"`declared` here and say why in the commit.",
			undeclared, map[bool]string{true: "is", false: "are"}[len(undeclared) == 1])
	}
	for name := range declared {
		if callers[name] == 0 {
			t.Fatalf("%s declares no call to `validateEntryBytes`, so caller-supplied bytes "+
				"reach the store without the loader check OR the closed tag vocabulary. "+
				"If the function was renamed, rename it in `declared` here too.", name)
		}
	}
	if t.Failed() {
		return
	}
	// The ledger's own arithmetic, printed so a reader of the log sees the SET and not
	// only a pass.
	t.Log(fmt.Sprintf("validateEntryBytes callers: %s", strings.Join(sortedNames(callers), ", ")))
}

func sortedNames(m map[string]int) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
