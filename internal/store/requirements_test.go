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

// TestProvenanceIsTheWholeParenthesizedWord pins the two narrowings that are BEHAVIOUR:
// the match is the whole parenthesised word, and it is case-sensitive.
//
// 🔴 THIS IS WHAT STOPS PROVENANCE BEING MANUFACTURABLE — an accepted near-spelling would
// attribute a statement to the operator that the operator did not make.
//
// ⚠ ONE ROW PER MECHANISM, AND THE ROW COUNT CAME DOWN ON PURPOSE. An earlier version had
// eleven rows over three mechanisms; the duplicates cost nothing to RUN and cost a reader
// the question "which of these is load-bearing?". Each row below is the narrowest input
// that can distinguish the mechanism it names, and the mutation battery attributes which
// mutant each one kills — `(operators)` kills the prefix-match mutant, `(Operator)` kills
// the case-fold mutant. Deleting either stops a mutant dying, which is the test that the
// trim did not cut muscle.
func TestProvenanceIsTheWholeParenthesizedWord(t *testing.T) {
	for _, c := range []struct {
		line string
		want string
		why  string
	}{
		// The two positive controls: without these the whole table could pass by
		// returning ProvenanceAbsent unconditionally.
		{"- OPEN: (operator) a real one", ProvenanceOperator, "positive control, operator"},
		{"- OPEN: (inferred) a real one", ProvenanceInferred, "positive control, inferred"},
		// The separator is optional, so a missing space must NOT change the answer.
		{"- OPEN:(operator) no space", ProvenanceOperator, "the whitespace run is optional"},
		// Mechanism 1: the closing paren is required, so a longer word is not a match.
		// KILLS the prefix-match mutant.
		{"- OPEN: (operators) a plural", ProvenanceAbsent, "whole word, not a prefix"},
		// Mechanism 2: case-sensitive. KILLS the case-fold mutant.
		{"- OPEN: (Operator) capitalised", ProvenanceAbsent, "case-sensitive"},
	} {
		if got := BulletProvenance(c.line); got != c.want {
			t.Errorf("BulletProvenance(%q) = %q, want %q (%s)", c.line, got, c.want, c.why)
		}
	}
}

// TestALineWithNoParsedMarkerHasNoProvenance is an INVARIANT GUARD, labelled as one, and
// deliberately NOT counted as regression coverage.
//
// 🔴 THE BUG NEVER VIOLATED IT, BECAUSE IT CANNOT BE VIOLATED WITHOUT REWRITING
// `BulletProvenance`'s FIRST TWO LINES. `MarkerSpan` returns 0 for every line
// `BulletOpenness` refuses, and the function returns ProvenanceAbsent on a 0 span before
// looking at anything else — so these rows are structurally unreachable rather than
// behaviourally checked. No mutant in the battery dies here.
//
// It is kept anyway, cheaply, for one reason: it is the executable form of the doc
// comment's claim that provenance cannot attach to a near-miss bullet. If a later change
// makes provenance readable independently of the marker, this is what notices — and that
// change is exactly the one that would dress a failed write up as a recorded one.
func TestALineWithNoParsedMarkerHasNoProvenance(t *testing.T) {
	for _, line := range []string{
		"- (operator) no marker at all",
		"- (operator) OPEN: the parenthetical came first",
		"- 2000-07-08 OPEN: (operator) a near-miss marker, the date has no colon",
	} {
		if got := BulletProvenance(line); got != ProvenanceAbsent {
			t.Errorf("BulletProvenance(%q) = %q, want ProvenanceAbsent", line, got)
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

// TestARequirementsHeadingInsideAFenceIsNotAHeading is criterion 4's guard — and it is an
// INVARIANT GUARD, labelled as one rather than counted as regression coverage.
//
// 🔴 THE FENCE RULE PREDATES THIS CHANGE AND IS TESTED WHERE IT LIVES. `ParseRequirements`
// contains no fence logic at all: it delegates grouping to `ParseJournalBullets` and gets
// its body from `ExtractSections`, both of which already skip fences and are already
// covered. So nothing here could have regressed, and no mutant in this feature's battery
// dies on this test.
//
// It earns its place as a SEAM test instead: it asserts the NEW heading goes through the
// SAME fence rule as the other three, which is the relationship a reader would otherwise
// have to infer from two files. The distinction matters because the house rule is explicit
// — a guard pinning an invariant the bug never violated must say so, or it reads as
// coverage while providing none, which stops anyone looking.
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
