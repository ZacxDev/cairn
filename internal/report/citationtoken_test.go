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
// whole design exists for, and it is the one test that tells the shipped implementation from
// the ruled-out one.
//
// 🔴 WHAT IS RULED OUT: re-emitting each section body from `ParseJournalBullets`' groups
// instead of from the body's own lines. That parser DROPS text before the first bullet and
// ABSORBS a bullet whose opening line was lost or indented. Today both are an advisory
// validator finding at exit 0; under a reconstruction they are SILENT DELETION on the read
// surface, and the measured history is 7 versions across two entries carrying dropped lines
// for 2–8 days with that validator green the whole time.
//
// 🔴 THE MATRIX, AND IT IS NOT THE USUAL ONE. This guard is GREEN at `d7e1fec8` — at base no
// token is appended, so every body line renders as exactly `"      "+line` and the assertion
// holds vacuously. So against BASE it is an INVARIANT guard and must not be counted as
// regression coverage. It was watched RED against the RULED-OUT IMPLEMENTATION: with
// `text.go`'s loop replaced by `for _, b := range store.ParseJournalBullets(body) { for _, l
// := range b.Lines {…} }`, this test fails on the pre-bullet prose line and on the absorbed
// indented dash while `TestEVERYSurfacedSectionsBulletOpeningsCarryACitationID` above stays
// GREEN — which is exactly why a guard that only checks ids were appended is not enough.
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

// TestTheTokenLandsOnTheLineTheSPLITTERSaysItDoes is a REGRESSION guard against the one
// hazard `store.JournalBullet.StartLine` names and that already has a wrong-splitter
// consumer elsewhere in this tree (`internal/ui/render.go`'s `inlineCode` splits on "\n").
//
// 🔴 A LONE `\r` IS WHAT SEPARATES THE TWO SPLITTERS, AND NOTHING ELSE IN THE SUITE SENDS
// ONE THROUGH A RENDERED BODY. On the body `"- a\rb\n- c"`, `pytext.SplitLines` gives
// `["- a", "b", "- c"]` with StartLines `[0, 2]`; `strings.Split(body, "\n")` gives
// `["- a\rb", "- c"]`, so index 0 would annotate MID-LINE and index 2 would not exist. The
// assertion below is the whole normalised body, so a mutant swapping the splitter cannot
// pass by spelling a substring.
//
// Measured RED at `d7e1fec8`: at base no token is emitted, so the expected bytes carry two
// tokens that are simply absent. GREEN at HEAD.
func TestTheTokenLandsOnTheLineTheSPLITTERSaysItDoes(t *testing.T) {
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
	got := sectionBodies(rep.RenderText("fixture-host", nil, ""))[store.NuanceHeading]
	want := []string{
		"      - a" + store.CitationToken(ids[0]),
		"      b",
		"      - c" + store.CitationToken(ids[2]),
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("the body annotated on the wrong line boundary\n  got\n%s\n  want\n%s",
			strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}
