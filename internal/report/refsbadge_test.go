package report

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/ZacxDev/cairn/internal/store"
)

// TestTheLinkBadgeSaysREFSAndNotTASKS is a REGRESSION guard, not an invariant guard: the
// badge really did read `🔗 N task(s)` on this line until this change, while the key it
// counts has been `refs:` since `tasks:` folded into it. Measured red at `ba78dbb` with
// `🔗 1 task` / `🔗 2 tasks`, green at HEAD.
//
// 🔴 IT PINS BOTH CARDINALITIES, BECAUSE THE FIXTURE AND THE CORPUS PIN ONLY ONE EACH AND
// NEITHER PINS THE PAIR. `internal/report/testdata/reader_fixtures.json` carries a 2-ref
// entry, so a mutant that dropped `plural(...)` dies there; nothing in that fixture carries
// ONE ref, so a mutant that hardcoded `"refs"` — always plural — survives it. The corpus
// and `tests/parity/` each reach the singular through a live store, which makes them the
// reachability proof; this test is what makes the PAIR readable in one place.
//
// 🔴 IT PINS THE WHOLE NORMALISED LINE, NOT A SUBSTRING, AND THE DIFFERENCE IS MEASURED
// RATHER THAN STYLISTIC. The first version asserted `strings.Contains(line, "🔗 1 ref")` and
// a mutant that hardcoded the plural — `" refs"`, dropping `plural(...)` — SURVIVED it,
// because `🔗 1 refs` contains `🔗 1 ref`. That is the house rule's own failure mode: a guard
// spelled as a prefix is walkable by writing a longer word. The literals below are written
// out here rather than built from `ljustRunes`/`SensitivityLabel`, so a format lifted from
// the implementation cannot agree with it by construction.
func TestTheLinkBadgeSaysREFSAndNotTASKS(t *testing.T) {
	// `  <ref padded to 12>  <count right-aligned to 3> nuance   <sensitivity>   <badges>`
	const base = "  gadget-one      2 nuance   public"
	for _, tc := range []struct {
		name  string
		tasks []string
		badge string
	}{
		{"one ref is singular", []string{"github:example-org/example-repo#1"}, "🔗 1 ref"},
		{"two refs are plural", []string{
			"github:example-org/example-repo#1",
			"github:example-org/example-repo#2",
		}, "🔗 2 refs"},
		{"three refs are plural", []string{
			"github:example-org/example-repo#1",
			"github:example-org/example-repo#2",
			"github:example-org/example-repo#3",
		}, "🔗 3 refs"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			line := listingLine(RecalledEntry{
				Ref:         "gadget-one",
				Sensitivity: "public",
				BulletCount: 2,
				Tasks:       tc.tasks,
			}, 12)
			if want := base + "   " + tc.badge; line != want {
				t.Errorf("index line\n  got  %q\n  want %q", line, want)
			}
			// 🔴 AND THE OLD WORD IS GONE, asserted as the whole badge segment for the same
			// reason the positive assertion is: the bare word `task` appears nowhere on this
			// row today, but `🔗 N task` is exactly what a revert re-introduces, and a
			// positive-only assertion passes against a renderer that emits BOTH.
			if old := "🔗 " + strconv.Itoa(len(tc.tasks)) + " task"; strings.Contains(line, old) {
				t.Errorf("index line still carries the retired badge %q:\n%s", old, line)
			}
		})
	}
}

// TestTheLinkBadgeIsCONDITIONAL is an INVARIANT guard — labelled as one, and not counted as
// regression coverage for the rename. It pins the property every badge on this row shares
// and that the whole feature's additivity rests on: an entry with no refs renders a row
// byte-identical to one from before the badge existed.
func TestTheLinkBadgeIsCONDITIONAL(t *testing.T) {
	entry := RecalledEntry{Ref: "gadget-one", Sensitivity: "public", BulletCount: 2}
	line := listingLine(entry, 12)
	if strings.Contains(line, "🔗") {
		t.Errorf("an entry with no refs rendered a link badge:\n%s", line)
	}
	entry.Tasks = []string{"github:example-org/example-repo#1"}
	withRef := listingLine(entry, 12)
	if withRef == line {
		t.Fatalf("the badge did not change the row at all, so the assertion above is "+
			"vacuous — this test cannot tell a conditional badge from no badge:\n%s", line)
	}
}

// refsLabelWorld writes ONE entry whose front matter carries `frontMatterLine` — the refs key
// under test, in its own shape — plus a `tags:` the body also renders.
//
// ⚠ THE KEY IS A PARAMETER RATHER THAN FIXED, AND THE TWO SHAPES ARE NOT INTERCHANGEABLE.
// `refs:`/`tasks:` are SEQUENCES and `task:` is a SCALAR, so a fixture that spelled all three
// the same way would be writing a file the loader refuses for two of them — and the case would
// then be measuring a rejection rather than a label.
func refsLabelWorld(t *testing.T, frontMatterLine string) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "alpha-notes")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("making the scope dir: %v", err)
	}
	body := strings.Join([]string{
		"---",
		"service: gadget-one",
		"scope: alpha-notes",
		"sensitivity: public",
		frontMatterLine,
		"tags: [infra]",
		"---",
		"",
		"## What it is",
		"",
		"The gadget.",
		"",
		"## Pointers",
		"- somewhere",
		"",
		"## Nuance / work-history",
		"- 2000-01-02: an ordinary lesson.",
		"",
	}, "\n")
	if err := os.WriteFile(filepath.Join(dir, "gadget-one.md"), []byte(body), 0o644); err != nil {
		t.Fatalf("writing the fixture: %v", err)
	}
	return root
}

// renderedLines runs the REAL path — `Recall` then `RenderText` — and returns the output split
// into LINES, so an assertion can compare a whole line instead of searching for a substring
// somewhere in a 3 KB report.
func renderedLines(t *testing.T, root string) []string {
	t.Helper()
	rep, err := Recall(root, RecallOptions{
		Scope: "alpha-notes",
		// `--ref` so exactly ONE body renders. A digest prints the same line, and then the
		// counts below would be measuring the MODE rather than the label.
		Ref:    "gadget-one",
		HasRef: true,
		Mode:   DefaultMode,
		Limit:  DefaultEntryLimit,
		Page:   1,
	}, store.ScopeSet{Unrestricted: true})
	if err != nil {
		t.Fatalf("Recall: %v", err)
	}
	return strings.Split(rep.RenderText("pinned-host", nil, ""), "\n")
}

func countLines(lines []string, want string) int {
	n := 0
	for _, l := range lines {
		if l == want {
			n++
		}
	}
	return n
}

// TestTheBodyLabelSaysREFSAndNotTASKS is a REGRESSION guard. The body label read
// `    tasks: ` from before `tasks:` folded into `refs:` right through the badge rename — so
// for exactly one change the index row said `ref` and the body under it said `task`: two
// words for one field on one screen, which is the confusion the badge rename existed to
// remove. Measured red at `b338e7d` (the badge-only commit) and at `ba78dbb`; green at HEAD.
//
// 🔴 IT COMPARES WHOLE LINES, NOT SUBSTRINGS, AND THE MARGIN THAT BUYS IS MEASURED RATHER
// THAN ASSERTED. The badge guard above first asserted `strings.Contains(line, "🔗 1 ref")`
// and an always-plural mutant SURVIVED it, because `🔗 1 refs` contains `🔗 1 ref`. So the
// substring forms of THIS guard were run against three mutants, by name (so a neighbouring
// guard's red could not be read as this one's):
//
//	guard form                        L1 revert   L2 indent 4→3   L3 emits BOTH labels
//	Contains("refs: ")                RED         PASSED          not measured
//	Contains("    refs: ")            RED         RED             PASSED
//	whole line + count (this test)    RED         RED             RED
//
// ⚠ AN EARLIER VERSION OF THIS COMMENT CLAIMED THE INDENTED SUBSTRING COULD NOT SEE THE
// INDENT MUTANT, AND THAT IS FALSE — it catches it, because the four spaces are IN the
// substring. The two real blind spots are the ones measured above: the un-indented form
// misses the indent, and the indented form misses a HALF-APPLIED RENAME, which is the shape
// that matters most here because it is what a partly-reverted change looks like. The
// retired-label count is what closes that cell.
func TestTheBodyLabelSaysREFSAndNotTASKS(t *testing.T) {
	const refs = "github:example-org/example-repo#1, github:example-org/example-repo#2"
	const want = "    refs: " + refs
	const retired = "    tasks: " + refs

	lines := renderedLines(t, refsLabelWorld(t, "refs: ["+refs+"]"))

	if got := countLines(lines, want); got != 1 {
		t.Errorf("the rendered report carries %d line(s) equal to\n  %q\nwant exactly 1 "+
			"(of %d lines rendered)", got, want, len(lines))
	}
	// 🔴 AND THE RETIRED LABEL IS GONE, as its own whole line: a positive-only check passes
	// against a renderer that emits both.
	if got := countLines(lines, retired); got != 0 {
		t.Errorf("the rendered report still carries %d line(s) of the retired label\n  %q",
			got, retired)
	}
	// The `tags:` line is the CONTROL. It proves this fixture really does render identity
	// lines, so the zero above cannot be a report that rendered none of them — which is the
	// reassuring zero the house rule says is indistinguishable from a harness wired to
	// nothing.
	if got := countLines(lines, "    tags: infra"); got != 1 {
		t.Fatalf("the `tags:` control line rendered %d time(s), want 1 — without it a passing "+
			"assertion above is indistinguishable from a report with no identity lines at all",
			got)
	}
}

// TestTheBodyLabelIsRenderedFromTheOLDERKeysToo is the other half of the pair, and it is what
// keeps this a change to the RENDERED WORD rather than to what is PARSED.
//
// 🔴 `tasks:` AND `task:` STAY ACCEPTED ON THE WAY IN, PERMANENTLY, BY OPERATOR DECISION. An
// entry written with an older key must therefore render the NEW label: the parser keeps no
// record of which key it read, which is also why the browser's Refs tooltip names both spellings.
// A guard on the `refs:` fixture alone would pass just as well if the rename had been
// implemented by dropping the older keys — the one outcome this change must not have — so this
// case is not redundant with the one above.
func TestTheBodyLabelIsRenderedFromTheOLDERKeysToo(t *testing.T) {
	for _, tc := range []struct{ name, frontMatter, want string }{
		{"the older SEQUENCE key", "tasks: [github:example-org/example-repo#1]",
			"    refs: github:example-org/example-repo#1"},
		{"the older SCALAR key", "task: github:example-org/example-repo#2",
			"    refs: github:example-org/example-repo#2"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			lines := renderedLines(t, refsLabelWorld(t, tc.frontMatter))
			if got := countLines(lines, tc.want); got != 1 {
				t.Errorf("an entry written with `%s` rendered %d line(s) equal to\n  %q\n"+
					"want exactly 1. A 0 here means either the label did not move or the "+
					"older key stopped being accepted — and those are different defects, so "+
					"check which before fixing.", tc.frontMatter, got, tc.want)
			}
		})
	}
}
