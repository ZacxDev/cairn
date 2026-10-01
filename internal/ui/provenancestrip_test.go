package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ZacxDev/cairn/internal/store"
)

// provenanceStripWorld writes ONE entry whose `## Requirements` section carries every
// provenance shape the parser distinguishes, plus a `## Nuance / work-history` bullet whose
// first line is BYTE-IDENTICAL to the first requirement.
//
// 🔴 THE IDENTICAL NUANCE LINE IS THE FIXTURE'S WHOLE POINT, not a decoration. The cut this
// file tests is driven by `Bullet.Provenance`, which is a property of the SECTION, while
// `store.ProvenanceSpan` reads the LINE — so the only case that can tell a gated cut from an
// ungated one is two bullets with the same bytes in different sections. Without it, deleting
// the `b.Provenance != store.ProvenanceAbsent` gate in `Bullet.Body` passes.
//
// ⚠ THE TWO NEAR-SPELLINGS AND THE NEAR MISS ARE HERE FOR THE OTHER DIRECTION — the page must
// not quietly remove text no badge replaces. They are the three lines where
// `BulletProvenance` answers absent for a line that LOOKS attributed.
func provenanceStripWorld(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "alpha-notes")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("making the scope dir: %v", err)
	}
	body := strings.Join([]string{
		"---",
		"service: runbook",
		"scope: alpha-notes",
		"---",
		"",
		"## What it is",
		"",
		"The rollout runbook.",
		"",
		"## Requirements",
		"",
		"- OPEN: (operator) the share page should name the project",
		"- OPEN: (inferred) the export should stream",
		"- RESOLVED def5678: (operator) the retry budget is bounded",
		"- 2000-01-02: OPEN: (inferred) a DATED declared marker carries provenance too",
		"- OPEN: (Operator) a near-spelling nobody attributed",
		"- OPEN: (operators) a second near-spelling, plural",
		"- OPEN: the archive should keep its original timestamps",
		"- OPENISH: (operator) a NEAR MISS, so nothing is removed at all",
		"",
		"## Nuance / work-history",
		"",
		"- OPEN: (operator) the share page should name the project",
		"",
	}, "\n")
	if err := os.WriteFile(filepath.Join(dir, "runbook.md"), []byte(body), 0o644); err != nil {
		t.Fatalf("writing the fixture: %v", err)
	}
	return root
}

func theSection(t *testing.T, item Entry, heading string) Section {
	t.Helper()
	for _, s := range item.Sections {
		if s.Heading == heading {
			return s
		}
	}
	t.Fatalf("the entry carries no %q section; it has %d sections", heading, len(item.Sections))
	return Section{}
}

// TestTheProvenanceParENTHETICALIsRemovedFromTheBODY is a REGRESSION guard. The page rendered
// `(operator)` TWICE — once as a badge, once at the head of the line under it — from the day
// the badge shipped until this change. Measured red at `ba78dbb`: every `want` below fails
// there with the parenthetical still in the body. Green at HEAD.
//
// 🔴 EACH ASSERTION IS THE WHOLE FIRST LINE, NORMALISED, AND NEVER `!Contains(body, "operator")`.
// A substring guard is walkable in both directions: the word `operator` occurs in the page's own
// legend, and a body that lost the parenthetical AND the sentence after it would satisfy a
// negative-only check while having deleted the requirement. Pinning the whole line is what makes
// "the parenthetical went and the prose stayed" one claim instead of two hopes.
func TestTheProvenanceParENTHETICALIsRemovedFromTheBODY(t *testing.T) {
	src := StoreSource{Root: provenanceStripWorld(t)}
	item := readTheOneEntry(t, src)
	section := theSection(t, item, store.RequirementsHeading)

	// Keyed by a fragment of the bullet's own prose, which is the one part of every line
	// that survives every cut — so the table cannot be matched against the wrong bullet.
	want := map[string]string{
		"share page":      "the share page should name the project",
		"export":          "the export should stream",
		"retry budget":    "the retry budget is bounded",
		"DATED":           "a DATED declared marker carries provenance too",
		"near-spelling n": "(Operator) a near-spelling nobody attributed",
		"plural":          "(operators) a second near-spelling, plural",
		"archive":         "the archive should keep its original timestamps",
		"NEAR MISS":       "OPENISH: (operator) a NEAR MISS, so nothing is removed at all",
	}
	if len(section.Bullets) != len(want) {
		t.Fatalf("the section rendered %d bullets, want %d — the fixture and the table have "+
			"drifted apart and every row below would be matched against the wrong line",
			len(section.Bullets), len(want))
	}
	seen := map[string]bool{}
	for _, b := range section.Bullets {
		first := b.Body()[0]
		for key, wantLine := range want {
			if !strings.Contains(strings.Join(b.Lines, "\n"), key) {
				continue
			}
			seen[key] = true
			if first != wantLine {
				t.Errorf("bullet %q rendered body first line\n  got  %q\n  want %q", key, first, wantLine)
			}
		}
	}
	for key := range want {
		if !seen[key] {
			t.Errorf("no bullet matched %q, so its row asserted nothing", key)
		}
	}
}

// TestANuanceBulletKeepsItsParENTHETICAL is the GATE, and it is the assertion that separates
// "remove what a badge replaces" from "remove anything that looks like provenance".
//
// 🔴 MEASURED AS A KILLING CASE. Deleting the `b.Provenance != store.ProvenanceAbsent` gate
// in `Bullet.Body` leaves every assertion in [TestTheProvenanceParENTHETICALIsRemovedFromTheBODY]
// GREEN — the requirements bullets are stripped either way — and reddens this test. A nuance
// bullet gets no provenance badge whatever its line says, so a cut there deletes text with
// nothing on the page to replace it: no badge, no note, no trace.
//
// ⚠ NOT THE ONLY KILLER, AND THE WORD MATTERS: an earlier version of this line said "reddens
// exactly this one", which an independent re-run of the same mutation disproved —
// [TestTheProvenanceParENTHETICALIsGoneFromTheRENDEREDPage] reddens too, on its count of the
// shared line. Two killers is better than one; a comment DESIGNATING a unique killer is how
// the second gets trimmed as redundant. Neither is redundant: this one pins the MODEL, that
// one pins the HTML.
//
// ⚠ IT IS A PAIR WITH THE REQUIREMENTS SIDE OF THE SAME LINE, because the two bullets are
// byte-identical on disk. Asserting only that the nuance copy is intact would pass against a
// renderer that strips nothing anywhere, which is the pre-change state.
func TestANuanceBulletKeepsItsParENTHETICAL(t *testing.T) {
	src := StoreSource{Root: provenanceStripWorld(t)}
	item := readTheOneEntry(t, src)

	nuance := theSection(t, item, store.NuanceHeading)
	if len(nuance.Bullets) != 1 {
		t.Fatalf("the nuance section rendered %d bullets, want 1", len(nuance.Bullets))
	}
	if got, want := nuance.Bullets[0].Body()[0],
		"(operator) the share page should name the project"; got != want {
		t.Errorf("a NUANCE bullet's body\n  got  %q\n  want %q\n"+
			"Provenance is a `## Requirements` concept, so this bullet carries no badge and "+
			"nothing may be cut from its line.", got, want)
	}
	if p := nuance.Bullets[0].Provenance; p != store.ProvenanceAbsent {
		t.Fatalf("the nuance bullet carries provenance %q — if this is ever non-empty the "+
			"assertion above stops being the gate's killing case", p)
	}

	// The requirements copy of the SAME LINE, right here, so the pair is one test.
	reqs := theSection(t, item, store.RequirementsHeading)
	var found bool
	for _, b := range reqs.Bullets {
		if !strings.Contains(strings.Join(b.Lines, "\n"), "share page") {
			continue
		}
		found = true
		if got, want := b.Body()[0], "the share page should name the project"; got != want {
			t.Errorf("the REQUIREMENTS copy of the same line\n  got  %q\n  want %q", got, want)
		}
	}
	if !found {
		t.Fatal("the requirements section holds no copy of the shared line, so this test is " +
			"asserting about one bullet rather than a pair")
	}
}

// TestTheProvenanceParENTHETICALIsGoneFromTheRENDEREDPage asserts the HTML, because
// `Bullet.Body` being right and the page printing `Bullet.Lines` anyway is exactly the shape
// the model-level assertions above cannot see — the same reason
// `TestTheProvenanceBadgeIS_RENDERED` exists beside the model tests for the badge.
//
// 🔴 IT ASSERTS THE BADGE AND THE BODY TOGETHER. A page that dropped both would satisfy "the
// parenthetical is not printed twice" by printing it zero times, which deletes the feature.
func TestTheProvenanceParENTHETICALIsGoneFromTheRENDEREDPage(t *testing.T) {
	src := StoreSource{Root: provenanceStripWorld(t)}
	item := readTheOneEntry(t, src)

	world := benignWorld()
	world[0].Entries[0].Sections = item.Sections
	view := viewOf("operator@example.invalid", world)
	view.Scope = &world[0]
	view.Entry = &world[0].Entries[0]
	out := renderNode(t, EntryPage(view))

	bodies := preBodies(out, "bullet-body")
	if len(bodies) == 0 {
		t.Fatal("the page rendered no bullet bodies at all, so every assertion below is vacuous")
	}

	// 🔴 COUNTED, NOT ABSENT, BECAUSE ONE OCCURRENCE IS CORRECT. The page renders the nuance
	// section too, and its bullet's line is byte-identical to the first requirement's — so a
	// whole-page `absent` assertion is RED on a correct tree, which is the permanently-red
	// guard this repository refuses. The number is the claim: the shared line's parenthetical
	// survives ONCE (under nuance, where no badge replaces it) and not twice.
	for _, tc := range []struct {
		text string
		want int
		why  string
	}{
		{"(operator) the share page should name the project", 1,
			"the nuance copy keeps it; the requirements copy must not"},
		{"(inferred) the export should stream", 0, "requirements only, so the badge replaces it"},
		{"(operator) the retry budget is bounded", 0, "requirements only, on a RESOLVED marker"},
	} {
		got := 0
		for _, b := range bodies {
			got += strings.Count(b, tc.text)
		}
		if got != tc.want {
			t.Errorf("%q appears in %d bullet body/bodies, want %d — %s",
				tc.text, got, tc.want, tc.why)
		}
	}
	// …and the prose they introduced is still on the page, so the check above is not
	// satisfied by a renderer that dropped the whole line.
	for _, kept := range []string{
		"the share page should name the project",
		"the export should stream",
		"the retry budget is bounded",
	} {
		var any bool
		for _, b := range bodies {
			if strings.Contains(b, kept) {
				any = true
			}
		}
		if !any {
			t.Errorf("the page no longer carries %q in any bullet body — the cut removed the "+
				"requirement, not just its attribution", kept)
		}
	}
	// The badges are still there. `(operator)` is written three times in the requirements
	// section and once under nuance; three of the four are attributions the badge renders,
	// and the nuance one is not.
	if n := strings.Count(out, `<span class="badge badge-open">operator</span>`); n != 2 {
		t.Errorf("rendered %d `operator` badge(s), want 2 — if this is 0 the assertions above "+
			"pass by the feature having been deleted rather than de-duplicated", n)
	}
	if n := strings.Count(out, `<span class="badge badge-quiet">inferred</span>`); n != 2 {
		t.Errorf("rendered %d `inferred` badge(s), want 2", n)
	}
}

// TestProvenanceSpanAndTheBadgeAgreeOnEveryFixtureLine is an INVARIANT GUARD, labelled as one,
// and deliberately NOT counted as regression coverage for this change — the same labelling
// [TestTheLinkBadgeIsCONDITIONAL] carries in `internal/report`.
//
// 🔴 MEASURED: IT STAYS GREEN UNDER BOTH PAYLOAD MUTATIONS. Dropping the provenance cut from
// `Bullet.Body` and dropping its `b.Provenance` gate each leave this test passing, because
// neither touches `bulletProvenance` — so it provides NO regression coverage for the defect
// this change fixed, and reporting it as if it did would overstate the suite.
//
// It is kept, cheaply, because it guards a DIFFERENT class that no other test here covers: a
// future edit that splits `bulletProvenance` back into two scans, letting the badge's WORD and
// the cut's OFFSET disagree about which bytes the provenance is. That is the failure
// `ProvenanceSpan`'s doc promises cannot happen; this is its executable form. A mutation
// inside `bulletProvenance` is what would redden it, and none was run — so "no regression
// coverage" is measured and "dead" is not claimed.
//
// ⚠ A STRUCTURAL CHECK ONLY. It pins that a non-zero span implies a non-empty word and vice
// versa, over every line the fixture carries; the behavioural claims are the three tests above.
func TestProvenanceSpanAndTheBadgeAgreeOnEveryFixtureLine(t *testing.T) {
	src := StoreSource{Root: provenanceStripWorld(t)}
	item := readTheOneEntry(t, src)

	var lines int
	for _, s := range item.Sections {
		for _, b := range s.Bullets {
			first := b.Lines[0]
			lines++
			word := store.BulletProvenance(first)
			span := store.ProvenanceSpan(first)
			if (word == store.ProvenanceAbsent) != (span == 0) {
				t.Errorf("BulletProvenance(%q) = %q but ProvenanceSpan = %d — the word and the "+
					"offset disagree, so a badge renders over bytes nobody cut, or the reverse",
					first, word, span)
			}
			if span != 0 && span < store.MarkerSpan(first) {
				t.Errorf("ProvenanceSpan(%q) = %d is BEFORE MarkerSpan = %d; a caller that "+
					"cuts at the provenance span would then re-print the marker",
					first, span, store.MarkerSpan(first))
			}
		}
	}
	if lines < 9 {
		t.Fatalf("walked only %d bullet lines; the fixture declares 9, so this walk is not "+
			"reaching the sections it claims to", lines)
	}
}
