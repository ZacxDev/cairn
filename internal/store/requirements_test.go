package store

import (
	"strings"
	"testing"
)

// requirementsFixture is one synthetic entry carrying BOTH a requirements section and a
// nuance section.
//
// 🔴 THE TWO SECTIONS SHARE ONE BULLET'S TEXT, VERBATIM, AND THAT IS THE WHOLE POINT OF
// THE FIXTURE. If the section boundary ever stops being what separates a requirement from
// a note, this text is what makes the failure visible — any parser that reads the entry
// body instead of the section body returns it twice.
//
// ⚠ THE VALUES ARE PAIRWISE DISTINCT AND DISTINCT FROM EVERY CONSTANT THE ASSERTIONS
// NAME. The shas differ from each other, the dates differ, and no bullet's text contains
// the words `operator` or `inferred` outside its own parenthetical — so a mutant that
// hardcodes a provenance literal, or one that returns the first bullet for every index,
// cannot land on the expected value by coincidence.
const requirementsFixture = "" +
	"---\n" +
	"service: beta-widget\n" +
	"---\n" +
	"\n" +
	"## What it is\n" +
	"\n" +
	"A synthetic subsystem.\n" +
	"\n" +
	"## Requirements\n" +
	"\n" +
	"- OPEN: (operator) the listing page should show a per-row freshness stamp\n" +
	"- 2000-01-02: OPEN: (inferred) the export should stream rather than buffer\n" +
	"- RESOLVED a1b2c3d: (operator) the sign-in card should name the workspace\n" +
	"- RESOLVED: (inferred) the digest should collapse duplicate rows\n" +
	"- OPEN: the archive should keep its original timestamps\n" +
	"- 2000-03-04: this row was never marked at all\n" +
	"\n" +
	"## Nuance / work-history\n" +
	"\n" +
	"- OPEN: (operator) the listing page should show a per-row freshness stamp\n" +
	"- 2000-05-06: the cache was rebuilt by hand once\n"

func requirementsOf(t *testing.T, text string) []Requirement {
	t.Helper()
	sections := ExtractSections(text, []string{RequirementsHeading})
	return ParseRequirements(sections[RequirementsHeading])
}

// TestARequirementsSectionYieldsRequirements is criterion 1's guard.
//
// 🔴 WATCHED RED ON PRE-CHANGE CODE: with `ParseRequirements` returning nil — which is
// exactly what the store did before this change, since no caller could name the heading —
// this reports 0 and fails naming the count. It is a REGRESSION guard, not an invariant
// guard: the behaviour it pins did not exist before.
func TestARequirementsSectionYieldsRequirements(t *testing.T) {
	got := requirementsOf(t, requirementsFixture)
	if len(got) != 6 {
		t.Fatalf("requirements parsed = %d, want 6 (the six bullets under the heading)", len(got))
	}
	// The bullet reader is `ParseJournalBullets`, so the openness vocabulary must come
	// through unchanged rather than being re-derived here.
	if got[0].Openness != OpennessOpen {
		t.Errorf("bullet 0 openness = %q, want %q", got[0].Openness, OpennessOpen)
	}
	if got[2].ResolvedBy != "a1b2c3d" {
		t.Errorf("bullet 2 resolvedBy = %q, want %q", got[2].ResolvedBy, "a1b2c3d")
	}
	if got[1].Date != "2000-01-02" {
		t.Errorf("bullet 1 date = %q, want %q", got[1].Date, "2000-01-02")
	}
}

// TestProvenanceHasExactlyThreeAnswers is criterion 2's guard — one case per state, with
// absent asserted as a DECIDED answer rather than left untested.
func TestProvenanceHasExactlyThreeAnswers(t *testing.T) {
	got := requirementsOf(t, requirementsFixture)
	if len(got) != 6 {
		t.Fatalf("fixture drifted: %d bullets, want 6", len(got))
	}
	for _, c := range []struct {
		index int
		want  string
		why   string
	}{
		{0, ProvenanceOperator, "`OPEN: (operator)`"},
		{1, ProvenanceInferred, "a dated `OPEN: (inferred)`"},
		{2, ProvenanceOperator, "`RESOLVED <sha>: (operator)`"},
		{3, ProvenanceInferred, "a sha-less `RESOLVED: (inferred)`"},
		{4, ProvenanceAbsent, "a marker with no parenthetical"},
		{5, ProvenanceAbsent, "no marker at all"},
	} {
		if got[c.index].Provenance != c.want {
			t.Errorf("bullet %d (%s) provenance = %q, want %q",
				c.index, c.why, got[c.index].Provenance, c.want)
		}
	}
}

// TestProvenanceIsTheWholeParenthesizedWord pins the narrowing the doc comment claims:
// a prefix is not a match, a different case is not a match, and a bullet with no parsed
// marker has no provenance however it is spelled.
//
// 🔴 THIS IS WHAT STOPS PROVENANCE BEING MANUFACTURABLE. Every row here is a line a
// writer could plausibly type, and every one must come back ABSENT — an accepted
// near-spelling would attribute a statement to the operator that the operator did not
// make.
func TestProvenanceIsTheWholeParenthesizedWord(t *testing.T) {
	for _, c := range []struct {
		line string
		want string
	}{
		{"- OPEN: (operator) a real one", ProvenanceOperator},
		{"- OPEN: (inferred) a real one", ProvenanceInferred},
		{"- OPEN:(operator) no space is still fine", ProvenanceOperator},
		{"- OPEN: (operators) a plural", ProvenanceAbsent},
		{"- OPEN: (operator-ish) a hyphenated extension", ProvenanceAbsent},
		{"- OPEN: (Operator) capitalised", ProvenanceAbsent},
		{"- OPEN: (operator a missing paren", ProvenanceAbsent},
		{"- OPEN: operator) no opening paren", ProvenanceAbsent},
		{"- (operator) OPEN: the parenthetical came first", ProvenanceAbsent},
		{"- (operator) no marker at all", ProvenanceAbsent},
		{"- 2000-07-08 OPEN: (operator) a near-miss marker", ProvenanceAbsent},
	} {
		if got := BulletProvenance(c.line); got != c.want {
			t.Errorf("BulletProvenance(%q) = %q, want %q", c.line, got, c.want)
		}
	}
}

// TestTheSameTextUnderNuanceIsNotARequirement is criterion 3's guard — THE claim of this
// change, since the section boundary is the only thing telling the two apart.
//
// 🔴 WATCHED RED WITH THE BOUNDARY REMOVED: parsing `ParseRequirements(text)` over the
// whole entry body instead of the section body returns 8 bullets rather than 6 and the
// nuance count collapses, because the identical line appears under both headings.
func TestTheSameTextUnderNuanceIsNotARequirement(t *testing.T) {
	sections := ExtractSections(requirementsFixture,
		[]string{RequirementsHeading, NuanceHeading})

	reqs := ParseRequirements(sections[RequirementsHeading])
	nuance := ParseJournalBullets(sections[NuanceHeading])

	if len(reqs) != 6 {
		t.Errorf("requirements = %d, want 6", len(reqs))
	}
	if len(nuance) != 2 {
		t.Errorf("nuance bullets = %d, want 2", len(nuance))
	}

	// The shared line must appear on BOTH sides exactly once — that is what makes this a
	// boundary test rather than a counting test. A parser that read the whole body would
	// put it in the requirements list twice.
	const shared = "the listing page should show a per-row freshness stamp"
	if n := countCarrying(bulletTexts(reqs), shared); n != 1 {
		t.Errorf("requirements carrying the shared line = %d, want 1", n)
	}
	if n := countCarrying(journalTexts(nuance), shared); n != 1 {
		t.Errorf("nuance bullets carrying the shared line = %d, want 1", n)
	}
}

// TestARequirementsHeadingInsideAFenceIsNotAHeading is criterion 4's guard, over the
// EXISTING `IsFence` rule rather than a second fence notion.
func TestARequirementsHeadingInsideAFenceIsNotAHeading(t *testing.T) {
	fenced := "" +
		"---\n" +
		"service: gamma-widget\n" +
		"---\n" +
		"\n" +
		"## What it is\n" +
		"\n" +
		"An entry that only TALKS about the section.\n" +
		"\n" +
		"```markdown\n" +
		"## Requirements\n" +
		"\n" +
		"- OPEN: (operator) this is sample text, not a stated requirement\n" +
		"```\n"

	sections := ExtractSections(fenced, []string{RequirementsHeading})
	if _, present := sections[RequirementsHeading]; present {
		t.Fatalf("a fenced heading was treated as a real section")
	}
	if got := ParseRequirements(sections[RequirementsHeading]); len(got) != 0 {
		t.Errorf("requirements = %d, want 0 — the heading is inside a fence", len(got))
	}
}

// TestMetAndOpenAreNotComplements pins the gap the two predicates deliberately leave: an
// unmarked bullet is neither, and `IsOpen` is not `!IsMet`.
func TestMetAndOpenAreNotComplements(t *testing.T) {
	got := requirementsOf(t, requirementsFixture)
	if len(got) != 6 {
		t.Fatalf("fixture drifted: %d bullets, want 6", len(got))
	}
	for _, c := range []struct {
		index     int
		open, met bool
		why       string
	}{
		{0, true, false, "declared OPEN"},
		{1, true, false, "dated, declared OPEN"},
		{2, false, true, "RESOLVED with a sha"},
		{3, false, true, "RESOLVED with no sha — closed but unprovable, still MET"},
		{4, true, false, "OPEN with no provenance is still OPEN"},
		{5, false, false, "unmarked: neither open nor met"},
	} {
		if got[c.index].IsOpen() != c.open {
			t.Errorf("bullet %d (%s) IsOpen = %v, want %v", c.index, c.why, got[c.index].IsOpen(), c.open)
		}
		if got[c.index].IsMet() != c.met {
			t.Errorf("bullet %d (%s) IsMet = %v, want %v", c.index, c.why, got[c.index].IsMet(), c.met)
		}
	}
}

func bulletTexts(rs []Requirement) []string {
	out := make([]string, 0, len(rs))
	for _, r := range rs {
		out = append(out, r.Text())
	}
	return out
}

func journalTexts(bs []JournalBullet) []string {
	out := make([]string, 0, len(bs))
	for _, b := range bs {
		out = append(out, b.Text())
	}
	return out
}

func countCarrying(texts []string, want string) int {
	n := 0
	for _, t := range texts {
		if strings.Contains(t, want) {
			n++
		}
	}
	return n
}
