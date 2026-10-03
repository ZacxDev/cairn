package report

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/ZacxDev/cairn/internal/pytext"
	"github.com/ZacxDev/cairn/internal/store"
)

// citationTokenRe is the rendered token as a READER sees it, spelled out here rather than
// built from `store.CitationToken` on purpose: a pattern derived from the implementation
// agrees with it by construction and could not fail when the format moves.
var citationTokenRe = regexp.MustCompile(` \[cb:[0-9a-f]{8}\]$`)

// citationWorld writes ONE entry carrying top-level bullets under ALL FOUR surfaced
// headings, and returns the store root.
//
// 🔴 ALL FOUR, BECAUSE THE SCOPE DECISION IS "EVERY SURFACED SECTION" AND A FIXTURE
// CARRYING ONLY NUANCE CANNOT SEE A RENDERER THAT NARROWED TO IT. `## Pointers` is the one
// that matters most: its rows are `- path — description`, NOT `- YYYY-MM-DD: prose`, so an
// annotator that keyed off a DATE rather than off the bullet grammar annotates nothing
// here while every nuance row still gets a token.
//
// ⚠ IT ALSO CARRIES THE THREE NO-ID SHAPES ON PURPOSE — prose before the first bullet, an
// INDENTED dash (a continuation), and a dash INSIDE A FENCE — because "which lines get no
// token" is half the contract and a world of nothing but clean bullets cannot state it.
func citationWorld(t *testing.T) string {
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
		"---",
		"",
		"## What it is",
		"",
		"Prose that opens no bullet at all.",
		"",
		"## Pointers",
		"",
		"Some orienting prose BEFORE the first row.",
		"- apps/gadget-one/values.yaml — the chart values",
		"  a wrapped continuation of the row above",
		"  - an INDENTED dash, which is a continuation and not a bullet",
		"- `internal/widget/widget.go` — the loader",
		"",
		"## Nuance / work-history",
		"",
		"- 2000-01-02: the retry budget is still unbounded.",
		"  wrapped prose under it",
		"- 2000-01-03: OPEN: the export should stream.",
		"```",
		"- a dash inside a fence, which is sample text",
		"```",
		"",
		"## Requirements",
		"",
		"- OPEN: the archive should keep its original timestamps",
		"- RESOLVED def5678: (operator) the listing carries a freshness stamp",
		"",
	}, "\n")
	if err := os.WriteFile(filepath.Join(dir, "gadget-one.md"), []byte(body), 0o644); err != nil {
		t.Fatalf("writing the entry: %v", err)
	}
	return root
}

// renderCitationWorld renders the world above in FULL mode, so every body prints.
func renderCitationWorld(t *testing.T) string {
	t.Helper()
	root := citationWorld(t)
	rep, err := Recall(root, RecallOptions{
		Scope: "alpha-notes",
		Limit: DefaultEntryLimit,
		Mode:  "full",
		Page:  1,
	}, store.Unrestricted())
	if err != nil {
		t.Fatalf("recall: %v", err)
	}
	return rep.RenderText("fixture-host", nil, "")
}

// sectionBodies pulls each surfaced section's RENDERED body lines out of a report, with the
// six-space indent still on them. A heading line ends the previous section, and so does the
// `  ### ` entry line, so a notice printed after the last body cannot be mistaken for part
// of it.
func sectionBodies(rendered string) map[string][]string {
	out := map[string][]string{}
	cur := ""
	for _, line := range pytext.SplitLines(rendered) {
		if strings.HasPrefix(line, "  ### ") {
			cur = ""
			continue
		}
		if h := strings.TrimPrefix(line, "    "); strings.HasPrefix(h, "## ") && h == strings.TrimSpace(h) {
			cur = h
			continue
		}
		if cur == "" {
			continue
		}
		if !strings.HasPrefix(line, "      ") {
			cur = ""
			continue
		}
		out[cur] = append(out[cur], line)
	}
	return out
}

// TestEVERYSurfacedSectionsBulletOpeningsCarryACitationID is a REGRESSION guard, not an
// invariant guard: no surface printed a citation id at all before this change. Measured RED
// at `d7e1fec8` — where every one of the four sections renders zero tokens and the first
// subtest's first assertion fails — and GREEN at HEAD.
//
// 🔴 IT ASSERTS THE VALUE, NOT MERELY THE SHAPE. A renderer that appended a CONSTANT token,
// or one that annotated the wrong bullet, satisfies "the line ends in ` [cb:`" perfectly.
// The expected id is taken from `store.ParseJournalBullets` over the same section body —
// which is the derivation under test, so this is a POSITION claim rather than a hash claim;
// what pins the hash itself is `internal/store/citationid_test.go` against the PYTHON
// answers, and what pins the two renderers against each other is `reader_fixtures.json` and
// `tests/parity/`.
//
// 🔴 AND IT ASSERTS THE NEGATIVE HALF IN THE SAME PASS: a line that opens no bullet carries
// no token. Without that, a renderer appending a token to EVERY line passes the positive
// half on every row.
func TestEVERYSurfacedSectionsBulletOpeningsCarryACitationID(t *testing.T) {
	rendered := renderCitationWorld(t)
	bodies := sectionBodies(rendered)

	// The world's own section bodies, re-derived from the entry rather than from the
	// rendered text, so the expectation does not come from the output it checks.
	root := citationWorld(t)
	data, err := os.ReadFile(filepath.Join(root, "alpha-notes", "gadget-one.md"))
	if err != nil {
		t.Fatalf("re-reading the entry: %v", err)
	}
	sections := store.ExtractSections(store.DecodeReplace(data), SurfacedHeadings)

	// Per-section EXPECTED token counts, as literals. A derived count would agree with a
	// renderer that annotated nothing in a section the parser finds no bullets in.
	wantTokens := map[string]int{
		store.WhatHeading:         0, // prose only — the honest zero, and the control below
		store.PointersHeading:     2, // two `- path — description` rows
		store.NuanceHeading:       2, // the fenced dash is sample text, so NOT three
		store.RequirementsHeading: 2,
	}

	for _, heading := range SurfacedHeadings {
		t.Run(heading, func(t *testing.T) {
			body := sections[heading]
			lines := pytext.SplitLines(body)
			got := bodies[heading]
			if len(got) != len(lines) {
				t.Fatalf("%s rendered %d body lines for a %d-line body — the body must print "+
					"line for line:\n%s", heading, len(got), len(lines), strings.Join(got, "\n"))
			}
			ids := store.CitationIDsByStartLine(body)
			seen := 0
			for i, line := range lines {
				want := "      " + line
				if id, annotated := ids[i]; annotated {
					want += " [cb:" + id + "]"
					seen++
				}
				if got[i] != want {
					t.Errorf("%s line %d:\n  got  %q\n  want %q", heading, i, got[i], want)
				}
			}
			if seen != wantTokens[heading] {
				t.Errorf("%s carried %d citation tokens, want %d — the world was written to "+
					"produce exactly that many, so a different number means either the parser "+
					"or the fixture moved", heading, seen, wantTokens[heading])
			}
		})
	}

	// 🔴 THE CONTROL THAT STOPS THE `## What it is` ROW BEING A HARNESS WIRED TO NOTHING. A
	// zero is indistinguishable from "this test never looked at that section", so the
	// non-zero rows have to be observable in the SAME read: four sections, three annotated,
	// one deliberately not.
	total := strings.Count(rendered, " [cb:")
	if total != 6 {
		t.Fatalf("the whole render carried %d tokens, want 6 (0 + 2 + 2 + 2) — a zero in the "+
			"prose section only means something if the other three moved", total)
	}
	if strings.Contains(strings.Join(bodies[store.WhatHeading], "\n"), "[cb:") {
		t.Error("the prose-only section carried a token, so the annotator is not keyed on a " +
			"bullet at all")
	}
}

// TestTheBODYKeepsEveryORIGINALByteEvenWhereTheParserMISHANDLESIt is THE safety property the
// whole design exists for: the body survives annotation BYTE FOR BYTE AND IN ORDER.
//
// ⚠ IT IS NOT "THE ONE TEST THAT TELLS THE SHIPPED IMPLEMENTATION FROM THE RULED-OUT ONE",
// WHICH IS WHAT THIS LINE USED TO SAY. Measured: the id test above fails under the
// reconstruction too — see the matrix below. What is unique here is the CLAIM, not the kill:
// this is the only test that asserts the rendered body reconstructs to the original bytes in
// order, which a line COUNT (what the id test checks first) cannot do.
//
// 🔴 WHAT IS RULED OUT: re-emitting each section body from `ParseJournalBullets`' groups
// instead of from the body's own lines. That parser DROPS text before the first bullet and
// ABSORBS a bullet whose opening line was lost or indented. Today both are an advisory
// validator finding at exit 0; under a reconstruction they are SILENT DELETION on the read
// surface, and the measured history is 7 versions across two entries carrying dropped lines
// for 2–8 days with that validator green the whole time.
//
// 🔴 THE MATRIX, RE-DERIVED — AND BOTH HALVES OF THE EARLIER ONE WERE WRONG. It said this
// guard is "GREEN at `d7e1fec8` … an INVARIANT guard [that] must not be counted as
// regression coverage", and that the ruled-out implementation reds it "while
// `TestEVERYSurfacedSectionsBulletOpeningsCarryACitationID` above stays GREEN". Measured at
// `902be517`:
//
//   - IT IS REGRESSION COVERAGE, NOT AN INVARIANT GUARD. With the emission loop reverted to
//     base's shape — the narrowest expression, the helpers kept referenced so the package
//     still builds — this test FAILS at the token-count floor at the bottom of the
//     function: "this world rendered 0 tokens, want 3". Its own anti-vacuity assertion is
//     what makes it red at base, so the verbatim-body claim never gets to hold vacuously.
//     ⚠ AND AT A LITERAL `d7e1fec8` CHECKOUT THE CLAIM IS NOT EVEN REACHABLE: neither
//     `store.CitationIDsByStartLine` nor `store.CitationToken` exists there, so this file
//     does not compile. The measurement above is the only reading of "at base" that has an
//     answer, and it is the one reported.
//
//   - THE ID TEST DOES NOT STAY GREEN UNDER THE RECONSTRUCTION. With `text.go`'s loop
//     replaced by `for _, b := range store.ParseJournalBullets(body) { for _, l := range
//     b.Lines {…} }`, BOTH tests fail. This one fails at its line-count assertion
//     (`## Pointers`: 1 rendered line for a 2-line body); the id test fails FIRST, at its
//     own per-section `len(got) != len(lines)` check — `## What it is` renders 0 body lines
//     for a 1-line prose body, and `## Pointers` 4 for 5. The structural reason is that the
//     id test's first per-section assertion is a LINE COUNT, which the reconstruction breaks
//     in any section carrying pre-bullet prose, and its world contains a prose-only
//     `## What it is`.
//
// ⚠ SO WHAT IS LEFT OF "A GUARD THAT ONLY CHECKS IDS WERE APPENDED IS NOT ENOUGH"? It
// holds, and the measurement above is its own example — but the weaker guard is a SUBTEST,
// not this file's id test. Under the reconstruction the id test's `## Nuance /
// work-history` and `## Requirements` subtests both PASSED: those two section bodies are
// entirely absorbed into bullet groups, so the reconstruction reproduces them line for line
// and the ids land correctly. A test scoped to a section like that, asserting only that
// bullet openings carry ids, passes the implementation this design rules out. Which is why
// the world above carries a prose-only section and a pre-bullet-prose section at all.
//
// ⚠ IT IS A CLAIM ABOUT CONTENT, NOT ABOUT IDS. A body line that carries no token still has
// to be there in full. A MISSING ID IS A DEGRADATION IN COVERAGE AND NEVER IN CONTENT; a
// missing LINE is the thing this refuses.
func TestTheBODYKeepsEveryORIGINALByteEvenWhereTheParserMISHANDLESIt(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "alpha-notes")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("making the scope dir: %v", err)
	}
	// Every line here is a line the reconstruction loses or relocates:
	//   * the two prose lines BEFORE the first bullet are dropped from the bullet list
	//     entirely (`ParseJournalBullets`' second documented rule);
	//   * the INDENTED dash is absorbed into the bullet above it, so a reconstruction that
	//     printed group by group would move it rather than lose it — and the body's own
	//     blank separators go with it;
	//   * the fenced dash is sample text that still has to PRINT, fence markers and all.
	nuance := []string{
		"A paragraph somebody wrote under the heading before any bullet.",
		"It wraps onto a second line.",
		"",
		"- 2000-01-02: the first real bullet.",
		"  - an indented dash the parser absorbs into the bullet above",
		"",
		"- 2000-01-03: the second real bullet.",
		"```",
		"- a dash inside a fence",
		"```",
		"trailing prose after the fence",
	}
	lines := append([]string{
		"---",
		"service: gadget-one",
		"scope: alpha-notes",
		"sensitivity: public",
		"---",
		"",
		"## Pointers",
		"",
		"orienting prose before the first pointer row",
		"- apps/gadget-one/values.yaml — the chart values",
		"",
		"## Nuance / work-history",
		"",
	}, nuance...)
	lines = append(lines, "")
	if err := os.WriteFile(filepath.Join(dir, "gadget-one.md"),
		[]byte(strings.Join(lines, "\n")), 0o644); err != nil {
		t.Fatalf("writing the entry: %v", err)
	}

	rep, err := Recall(root, RecallOptions{
		Scope: "alpha-notes", Limit: DefaultEntryLimit, Mode: "full", Page: 1,
	}, store.Unrestricted())
	if err != nil {
		t.Fatalf("recall: %v", err)
	}
	rendered := rep.RenderText("fixture-host", nil, "")

	data, err := os.ReadFile(filepath.Join(dir, "gadget-one.md"))
	if err != nil {
		t.Fatalf("re-reading the entry: %v", err)
	}
	sections := store.ExtractSections(store.DecodeReplace(data), SurfacedHeadings)
	bodies := sectionBodies(rendered)

	// The world must actually CONTAIN the mishandled shapes, or this test is a harness wired
	// to nothing — a positive control on the fixture rather than on the renderer.
	for heading, wantDropped := range map[string]int{
		store.PointersHeading: 1, // the one orienting prose line
		store.NuanceHeading:   3, // two prose lines plus the blank after them
	} {
		body := sections[heading]
		ids := store.CitationIDsByStartLine(body)
		first := len(pytext.SplitLines(body))
		for i := range ids {
			if i < first {
				first = i
			}
		}
		if first != wantDropped {
			t.Fatalf("%s: the fixture's first bullet is at body line %d, want %d — this test "+
				"cannot see the dropped-lines defect unless lines PRECEDE the first bullet",
				heading, first, wantDropped)
		}
	}

	for _, heading := range SurfacedHeadings {
		body := sections[heading]
		if body == "" {
			continue
		}
		want := pytext.SplitLines(body)
		got := bodies[heading]
		if len(got) != len(want) {
			t.Fatalf("%s: %d rendered body lines for a %d-line body. Lines went missing, which "+
				"is the one failure this design exists to prevent.\n  rendered:\n%s\n  body:\n%s",
				heading, len(got), len(want), strings.Join(got, "\n"), strings.Join(want, "\n"))
		}
		// 🔴 THE RECONSTRUCTION, IN REVERSE: strip the SIX-SPACE INDENT and any trailing
		// token, and the result must be the body BYTE FOR BYTE AND IN ORDER. That is a
		// stronger claim than "every original line appears somewhere" — a reconstruction
		// that merely REORDERED lines, or printed one twice, passes a containment check.
		rebuilt := make([]string, 0, len(got))
		for i, line := range got {
			stripped, ok := strings.CutPrefix(line, "      ")
			if !ok {
				t.Fatalf("%s line %d is not indented as a body line: %q", heading, i, line)
			}
			rebuilt = append(rebuilt, citationTokenRe.ReplaceAllString(stripped, ""))
		}
		if strings.Join(rebuilt, "\n") != strings.Join(want, "\n") {
			t.Errorf("%s: the body did not survive annotation verbatim.\n  rebuilt:\n%s\n"+
				"  original:\n%s", heading, strings.Join(rebuilt, "\n"), strings.Join(want, "\n"))
		}
	}

	// And the annotation really happened on this world too, so the loop above is not passing
	// because nothing was added.
	if n := strings.Count(rendered, " [cb:"); n != 3 {
		t.Fatalf("this world rendered %d tokens, want 3 (one pointer row, two nuance bullets) "+
			"— a verbatim-body assertion over an UNANNOTATED render is vacuous", n)
	}
}

// TestTheTokenLandsOnTheLineTheSPLITTERSaysItDoes drives a lone `\r` through the WHOLE
// read path — an entry file on disk, `Recall`, `RenderText` — and pins the rendered body
// as whole normalised lines.
//
// 🔴 AND IT CANNOT SEPARATE THE TWO SPLITTERS. AN EARLIER DRAFT OF THIS COMMENT CLAIMED IT
// COULD, AND THAT CLAIM WAS MEASURED FALSE RATHER THAN ARGUED AWAY. It read "a lone `\r` is
// what separates the two splitters … a mutant swapping the splitter cannot pass by spelling
// a substring". The `\r` never reaches a renderer: `ExtractSections` builds a section body
// by splitting the FILE and re-joining with "\n", so every other break character is gone
// before `RenderText` sees a body. Measured on the entry this test writes — the body
// arrives as `"- a\nb\n- c"`, for which `pytext.SplitLines` and `strings.Split(body, "\n")`
// return the SAME list. Mutation-proven at `902be517`: swapping `text.go`'s emission
// splitter to `strings.Split(body, "\n")` (the import kept live so the only change is the
// splitter) left `go test ./... -count=1` at **21 ok, 0 FAIL** — this test among them.
// `TestTheRENDERERSOwnSplitterIsWhatPlacesTheToken` below is what actually separates them,
// by handing `RenderText` a body DIRECTLY.
//
// ⚠ SO WHAT IS THIS TEST FOR? The end-to-end path, which the direct-body test deliberately
// skips: an entry on disk, through `Recall`, renders its bullet openings annotated and its
// continuation line not. That is a real claim and no other test in this file makes it over
// a file. It is kept, re-labelled, and NOT counted as splitter coverage.
//
// ⚠ THE EXPECTED TOKEN IS SPELLED HERE RATHER THAN BUILT FROM `store.CitationToken`, for
// the reason `citationTokenRe` is: a format derived from the implementation agrees with it
// by construction and could not fail when the format moves. Only the 8 hex characters come
// from the derivation, because they are a `sha256` nobody can spell by hand.
//
// Measured RED at `d7e1fec8`: at base no token is emitted, so the expected bytes carry two
// tokens that are simply absent. GREEN at HEAD.
func TestTheTokenLandsOnTheLineTheSPLITTERSaysItDoes(t *testing.T) {
	const body = "- a\rb\n- c"
	ids := store.CitationIDsByStartLine(body)
	if len(ids) != 2 {
		t.Fatalf("the body parsed to %d bullets, want 2: %v", len(ids), ids)
	}
	if _, ok := ids[2]; !ok {
		t.Fatalf("no bullet starts at body line 2, so `pytext.SplitLines` is not what the "+
			"parser used: %v", ids)
	}

	root := t.TempDir()
	dir := filepath.Join(root, "alpha-notes")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("making the scope dir: %v", err)
	}
	entry := "---\nservice: gadget-one\nscope: alpha-notes\nsensitivity: public\n---\n\n" +
		"## Nuance / work-history\n\n" + body + "\n"
	if err := os.WriteFile(filepath.Join(dir, "gadget-one.md"), []byte(entry), 0o644); err != nil {
		t.Fatalf("writing the entry: %v", err)
	}
	rep, err := Recall(root, RecallOptions{
		Scope: "alpha-notes", Limit: DefaultEntryLimit, Mode: "full", Page: 1,
	}, store.Unrestricted())
	if err != nil {
		t.Fatalf("recall: %v", err)
	}

	// 🔴 THE PREMISE THIS TEST RESTS ON, ASSERTED RATHER THAN ASSUMED — and it is the
	// retraction above, made mechanical. If `ExtractSections` ever STOPS collapsing the
	// `\r`, the sentence "this test cannot separate the splitters" becomes false and
	// somebody has to re-read both comments. Pinning it here is what makes that loud.
	sections := store.ExtractSections(store.DecodeReplace([]byte(entry)), SurfacedHeadings)
	extracted := sections[store.NuanceHeading]
	if strings.Contains(extracted, "\r") {
		t.Fatalf("`ExtractSections` preserved the lone \\r (%q) — this test's own comment "+
			"says it does not, and the direct-body test exists because of that. Re-read "+
			"both.", extracted)
	}

	got := sectionBodies(rep.RenderText("fixture-host", nil, ""))[store.NuanceHeading]
	want := []string{
		"      - a [cb:" + ids[0] + "]",
		"      b",
		"      - c [cb:" + ids[2] + "]",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("the body annotated on the wrong line boundary\n  got\n%s\n  want\n%s",
			strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// TestTheRENDERERSOwnSplitterIsWhatPlacesTheToken is the guard the comment at `text.go`'s
// emission loop claims exists: swap that loop's `pytext.SplitLines` for
// `strings.Split(body, "\n")` and THIS test goes red.
//
// 🔴 IT HANDS `RenderText` A BODY DIRECTLY, AND THAT IS THE WHOLE DESIGN RATHER THAN A
// SHORTCUT. Every other test in this file reaches the renderer through `Recall`, which
// reaches `ExtractSections`, which splits the FILE and re-joins with "\n" — so a lone `\r`
// is already gone by the time any splitter runs and the two splitters cannot disagree. The
// hazard is only reachable where a body arrives WITHOUT that normalisation, which a
// `RecalledEntry` constructed in-process is. `RecalledEntry.Sections` is an exported
// `map[string]string` and `RenderText` is a plain method over plain values (see
// `AGENTS.md`: the renderer is a library, not a handler), so this needs no store, no file
// and no fixture.
//
// 🔴 WHY A LONE `\r`, AND WHAT EACH SPLITTER DOES WITH IT. On `"- a\rb\n- c"`:
// `pytext.SplitLines` gives `["- a", "b", "- c"]` and `ParseJournalBullets` reports
// StartLines `[0, 2]`; `strings.Split(body, "\n")` gives `["- a\rb", "- c"]`, so the index
// 0 entry lands MID-LINE (after `b`) and index 2 does not exist at all. `pytext.SplitLines`
// breaks on TEN characters and `"\n"` is one of them, which is exactly why every body that
// has been through `ExtractSections` hides the difference.
//
// 🔴 THE MATRIX. RED at `902be517` with the splitter swapped — measured, with the mutation
// isolated to that one expression (the `pytext` import kept live by a package-level
// reference, because the loop is its only user and an unused import is a BUILD failure,
// which is a red for the wrong reason). Under it this test rendered one body line
// (`"      - a"`) against three expected. GREEN at `902be517` unmodified. ⚠ Against
// `d7e1fec8` the question does not arise: neither `store.CitationIDsByStartLine` nor
// `store.CitationToken` exists there, so this file does not compile.
//
// ⚠ IT IS A POSITION CLAIM, NOT A HASH CLAIM — the 8 hex come from the derivation under
// test. What pins the hash is `internal/store/citationid_test.go` against the PYTHON
// answers; what pins the FORMAT is the literal ` [cb:` spelled below and
// `TestTheRenderedCitationTokenIsStrippedByTheWritePath`.
func TestTheRENDERERSOwnSplitterIsWhatPlacesTheToken(t *testing.T) {
	const body = "- a\rb\n- c"
	ids := store.CitationIDsByStartLine(body)
	if len(ids) != 2 {
		t.Fatalf("the body parsed to %d bullets, want 2 — this case cannot see a splitter "+
			"difference otherwise: %v", len(ids), ids)
	}
	if _, ok := ids[2]; !ok {
		t.Fatalf("no bullet starts at body line 2, so `pytext.SplitLines` is not what the "+
			"parser used: %v", ids)
	}
	// 🔴 THE POSITIVE CONTROL ON THE FIXTURE, because this whole test is vacuous over a
	// body the two splitters agree on: they must DISAGREE on these bytes, here, before any
	// claim about which one the renderer picked means anything.
	if a, b := pytext.SplitLines(body), strings.Split(body, "\n"); len(a) == len(b) {
		t.Fatalf("the two splitters agree on %q (%d vs %d lines), so this test cannot see "+
			"which one the renderer used", body, len(a), len(b))
	}

	rep := RecallReport{
		Status: "recalled",
		Scope:  "alpha-notes",
		Entries: []RecalledEntry{{
			Ref:         "gadget-one",
			Filename:    "gadget-one.md",
			Sensitivity: "public",
			// The body as the renderer receives it — NOT through `ExtractSections`.
			Sections: map[string]string{store.NuanceHeading: body},
		}},
		TotalInScope: 1,
		Limit:        DefaultEntryLimit,
	}
	got := sectionBodies(rep.RenderText("fixture-host", nil, ""))[store.NuanceHeading]
	want := []string{
		"      - a [cb:" + ids[0] + "]",
		"      b",
		"      - c [cb:" + ids[2] + "]",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("the renderer did not split the body with `pytext.SplitLines`, so the "+
			"index from `CitationIDsByStartLine` named the wrong line\n  got\n%s\n  want\n%s",
			strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}
