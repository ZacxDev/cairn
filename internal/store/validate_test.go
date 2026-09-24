package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The WRITE-PROTOCOL half of `validate`, ported. Every fixture is synthetic and
// dated year 2000, per `AGENTS.md`.
//
// 🔴 THESE MIRROR `tests/test_entry_shape_validate.py` CASE FOR CASE. The gate that
// makes the two agree BYTE-FOR-BYTE is `tests/parity/`, which runs both clients over
// one world; these are the unit half, and they exist because the parity harness
// compares two clients rather than either client against an expectation.

func entryWithNuance(t *testing.T, dir, name, nuance string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	body := "---\nservice: talus-svc\nscope: crag-notes\n---\n" +
		"\n## What it is\n\na synthetic entry.\n" +
		"\n## Pointers\n\n- `apps/talus-svc/values.yaml`\n" +
		"\n" + NuanceHeading + "\n\n" + nuance + "\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

func TestDroppedLinesBeforeTheFirstBulletAreReported(t *testing.T) {
	dir := t.TempDir()
	path := entryWithNuance(t, dir, "talus-svc.md",
		"  the bullet opening that carried this line is gone.\n"+
			"- 2000-01-04: RESOLVED abc1234: the bullet that did survive.")
	got := ScanDroppedLines([]string{path})
	if len(got) != 1 {
		t.Fatalf("want 1 dropped line, got %d: %#v", len(got), got)
	}
	if got[0].Filename != "talus-svc.md" || got[0].Offset != 1 || got[0].CarriesMarker {
		t.Fatalf("unexpected finding: %#v", got[0])
	}
	if got[0].Line != "  the bullet opening that carried this line is gone." {
		t.Fatalf("line not verbatim: %q", got[0].Line)
	}
}

func TestADroppedDeclarationIsFlaggedAndPlainProseIsNot(t *testing.T) {
	dir := t.TempDir()
	path := entryWithNuance(t, dir, "talus-svc.md",
		"  ordinary lost prose.\n"+
			"  OPEN: a declaration nothing will ever surface.\n"+
			"- 2000-01-04: a bullet.")
	got := ScanDroppedLines([]string{path})
	if len(got) != 2 || got[0].CarriesMarker || !got[1].CarriesMarker {
		t.Fatalf("want [false true], got %#v", got)
	}
}

func TestLinesInsideABulletAreNotDropped(t *testing.T) {
	dir := t.TempDir()
	path := entryWithNuance(t, dir, "talus-svc.md",
		"- 2000-01-04: a bullet.\n  its wrapped continuation, which readers surface.")
	if got := ScanDroppedLines([]string{path}); len(got) != 0 {
		t.Fatalf("want none, got %#v", got)
	}
}

// 🔴 REACHABILITY IS KEYED ON (OFFSET, LINE), NOT ON THE LINE ALONE. A set of
// strings masks an orphan whose text is byte-identical to a line inside a bullet of
// the same file — and a read-modify-write race, the very shape that decapitates a
// bullet, is exactly what duplicates a block. Drop the offset from the key and the
// duplicate below is silently blessed.
func TestAnOrphanWhoseTextRepeatsInsideABulletIsStillReported(t *testing.T) {
	dir := t.TempDir()
	repeated := "  the same wrapped sentence, twice."
	path := entryWithNuance(t, dir, "talus-svc.md",
		repeated+"\n- 2000-01-04: a bullet.\n"+repeated)
	got := ScanDroppedLines([]string{path})
	if len(got) != 1 || got[0].Offset != 1 || got[0].Line != repeated {
		t.Fatalf("want one finding at offset 1, got %#v", got)
	}
}

func TestFencedTextBeforeTheFirstBulletIsNotDropped(t *testing.T) {
	for _, fence := range []string{"```", "~~~"} {
		dir := t.TempDir()
		path := entryWithNuance(t, dir, "talus-svc.md",
			fence+"\n- OPEN: sample text inside a fence.\n"+fence+
				"\n- 2000-01-04: a bullet.")
		if got := ScanDroppedLines([]string{path}); len(got) != 0 {
			t.Fatalf("%s fence: want none, got %#v", fence, got)
		}
	}
}

func TestAFileWithNoNuanceHeadingContributesNothing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "shapeless.md")
	if err := os.WriteFile(path, []byte("## What it is\n\nprose.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := ScanDroppedLines([]string{path}); len(got) != 0 {
		t.Fatalf("dropped: want none, got %#v", got)
	}
	if got := ScanUnreachableMarkers([]string{path}); len(got) != 0 {
		t.Fatalf("unreachable: want none, got %#v", got)
	}
}

func TestAnUnreadableFileContributesNothingRatherThanFailing(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "never-written.md")
	if got := ScanDroppedLines([]string{missing}); len(got) != 0 {
		t.Fatalf("dropped: want none, got %#v", got)
	}
	if got := ScanUnreachableMarkers([]string{missing}); len(got) != 0 {
		t.Fatalf("unreachable: want none, got %#v", got)
	}
}

// 🔴 AN INVARIANT GUARD ON A DECLARED BLIND SPOT — NOT regression coverage. A
// bullet that loses its opening line while ANOTHER bullet sits above it is absorbed
// into that one and the file is byte-identical to a legitimate wrap. This pins that
// the scanner does not pretend otherwise, so the zero it prints keeps carrying its
// PARTIAL caveat.
func TestTheAbsorbedTailCaseIsKnownInvisible(t *testing.T) {
	dir := t.TempDir()
	path := entryWithNuance(t, dir, "talus-svc.md",
		"- 2000-01-04: a bullet that is genuinely here.\n"+
			"  2000-01-05: the NEXT bullet, whose `- ` is gone.")
	if got := ScanDroppedLines([]string{path}); len(got) != 0 {
		t.Fatalf("want none (undecidable), got %#v", got)
	}
}

func TestAMarkerOnAContinuationLineIsReported(t *testing.T) {
	dir := t.TempDir()
	path := entryWithNuance(t, dir, "talus-svc.md",
		"- 2000-01-04: RESOLVED abc1234: the bullet that did survive.\n"+
			"  OPEN: a marker several lines in, where no parser looks.")
	got := ScanUnreachableMarkers([]string{path})
	if len(got) != 1 {
		t.Fatalf("want 1, got %#v", got)
	}
	if got[0].Offset != 2 || got[0].Openness != OpennessOpen ||
		got[0].Filename != "talus-svc.md" {
		t.Fatalf("unexpected finding: %#v", got[0])
	}
	if !strings.HasPrefix(got[0].BulletFirstLine, "- 2000-01-04: RESOLVED abc1234:") {
		t.Fatalf("bullet first line not carried: %q", got[0].BulletFirstLine)
	}
}

func TestAMarkerOnTheOpeningLineIsNotOutOfReach(t *testing.T) {
	dir := t.TempDir()
	path := entryWithNuance(t, dir, "talus-svc.md",
		"- 2000-01-04: OPEN: a reachable declaration.")
	if got := ScanUnreachableMarkers([]string{path}); len(got) != 0 {
		t.Fatalf("want none, got %#v", got)
	}
}

// The field case was exactly a bullet carrying two markers with the head one broken.
func TestItIsNotSuppressedWhenTheBulletAlreadyDeclaresOne(t *testing.T) {
	dir := t.TempDir()
	path := entryWithNuance(t, dir, "talus-svc.md",
		"- 2000-01-04: OPEN: the head marker, which parses.\n"+
			"  RESOLVED abc1234: a second claim, in the body.")
	got := ScanUnreachableMarkers([]string{path})
	if len(got) != 1 || got[0].Openness != OpennessResolved {
		t.Fatalf("want one resolved finding, got %#v", got)
	}
}

func TestAMarkerInsideAFenceWithinABulletIsNotReported(t *testing.T) {
	dir := t.TempDir()
	path := entryWithNuance(t, dir, "talus-svc.md",
		"- 2000-01-04: a bullet quoting a snippet.\n  ```\n  OPEN: sample text.\n  ```")
	if got := ScanUnreachableMarkers([]string{path}); len(got) != 0 {
		t.Fatalf("want none, got %#v", got)
	}
}

// 🔴 THE FIELD SHAPE. A nested list item is a CONTINUATION line, and `AsOpeningLine`
// strips its indentation rather than manufacturing a second `- ` prefix. A scanner
// that only handled unprefixed prose would be inert on exactly the observed shape.
func TestAnIndentedBulletMarkerIsReachedThroughTheRealParser(t *testing.T) {
	dir := t.TempDir()
	path := entryWithNuance(t, dir, "talus-svc.md",
		"- 2000-01-04: a bullet.\n  - OPEN: a nested item that declares nothing.")
	got := ScanUnreachableMarkers([]string{path})
	if len(got) != 1 || got[0].Openness != OpennessOpen {
		t.Fatalf("want one open finding, got %#v", got)
	}
}

func TestLineCarriesMarker(t *testing.T) {
	// 🔴 EACH FALSE CASE IS A SHAPE THE HAND-SPELLED ORACLE ORIGINAL GOT WRONG,
	// measured against a live store: it required no colon and no word boundary, so
	// prose fired and the field shapes did not.
	cases := []struct {
		line string
		want bool
	}{
		{"  OPEN: a declaration.", true},
		{"  - 2000-01-04: OPEN: a dated, nested declaration.", true},
		{"  RESOLVED abc1234: a closing claim.", true},
		{"  the remedy landed; RESOLVED abc1234: mid-line, still a declaration.", true},
		{"  resolved upstream in 1.2.3.", false},
		{"  RESOLVED_ADDR appears in the trace…", false},
		// 🔴 THE WORD-BOUNDARY GUARD'S OWN DISCRIMINATING INPUT, and it is narrow on
		// purpose. The line above is rejected by the COLON rule and would pass with
		// no boundary guard at all, so it cannot witness the guard. These two put a
		// colon within reach of the token, so the only thing that can reject them is
		// the boundary.
		{"  RESOLVED_ADDR: the symbol named in the trace.", false},
		{"  OPEN_: an identifier, not a declaration.", false},
		{"  OPEN SOURCE licences are listed below.", false},
		{"  OPENED the lease and moved on.", false},
		{"  ordinary prose with no marker at all.", false},
	}
	for _, c := range cases {
		if got := LineCarriesMarker(c.line); got != c.want {
			t.Errorf("LineCarriesMarker(%q) = %v, want %v", c.line, got, c.want)
		}
	}
}

// 🔴 "0 across 0 entry file(s)" IS THE REASSURING ZERO FROM AN INSTRUMENT THAT
// WALKED NOTHING, and it must not render anywhere near a clean-looking count.
func TestNothingCheckedPrintsNotCheckedAndNeitherZero(t *testing.T) {
	lines := ValidationAdvisoryLines(0, nil, nil)
	if len(lines) != 1 {
		t.Fatalf("want one line, got %#v", lines)
	}
	if !strings.Contains(lines[0], "NOT CHECKED") ||
		strings.Contains(lines[0], "0 across 0") {
		t.Fatalf("unexpected line: %q", lines[0])
	}
	for _, token := range []string{ReasonDroppedLine, ReasonUnreachableMarker} {
		if !strings.Contains(lines[0], token) {
			t.Fatalf("line does not carry %q: %q", token, lines[0])
		}
	}
}

func TestBothZerosCarryTheirDenominator(t *testing.T) {
	blob := strings.Join(ValidationAdvisoryLines(7, nil, nil), "\n")
	for _, want := range []string{
		"dropped lines: 0 across 7 entry file(s)",
		"marker reachability: 0 out-of-reach marker(s) across 7 entry file(s)",
		// 🔴 THIS CHECK IS KNOWINGLY PARTIAL. A bare "0 dropped" would read as
		// "no bullet has lost its head", which is a claim it cannot make.
		"PARTIAL BY CONSTRUCTION",
	} {
		if !strings.Contains(blob, want) {
			t.Errorf("missing %q in:\n%s", want, blob)
		}
	}
}

// 🔴 A dropped line is content NO reader reaches, so the marker scan never sees it.
// A `0 out-of-reach` printed ABOVE a `N DROPPED LINE(S)` is a fact about text the
// parser never got to, and reads as a reassurance it cannot support.
func TestTheDroppedBlockComesBeforeTheReachabilityBlock(t *testing.T) {
	blob := strings.Join(ValidationAdvisoryLines(1,
		[]DroppedLineFinding{{Filename: "a.md", Offset: 1, Line: "  lost."}},
		nil), "\n")
	dropped := strings.Index(blob, "DROPPED LINE(S)")
	reach := strings.Index(blob, "marker reachability:")
	if dropped < 0 || reach < 0 || dropped > reach {
		t.Fatalf("order wrong (dropped=%d reach=%d):\n%s", dropped, reach, blob)
	}
}

func TestFindingsAreQuotedWithTheirFileAndOffset(t *testing.T) {
	blob := strings.Join(ValidationAdvisoryLines(2,
		[]DroppedLineFinding{
			{Filename: "talus-svc.md", Offset: 1, Line: "  lost prose."},
			{Filename: "talus-svc.md", Offset: 2, Line: "  OPEN: lost claim.",
				CarriesMarker: true},
		},
		[]UnreachableMarkerFinding{
			{Filename: "talus-svc.md", BulletFirstLine: "- 2000-01-04: a bullet.",
				Offset: 2, Line: "  OPEN: x.", Openness: OpennessOpen},
		}), "\n")
	for _, want := range []string{
		"🔴 2 DROPPED LINE(S) across 2 entry file(s)",
		"1 of them looks like a `OPEN:`/`RESOLVED:` DECLARATION",
		"    talus-svc.md: nuance line 1",
		"    talus-svc.md: nuance line 2  ← looks like a DECLARATION",
		"🔴 1 MARKER(S) OUT OF REACH across 2 entry file(s)",
		"    talus-svc.md: line 2 of the bullet opening",
		"(would declare `open`)",
	} {
		if !strings.Contains(blob, want) {
			t.Errorf("missing %q in:\n%s", want, blob)
		}
	}
}

// The quote is there to recognise a finding by; an entry may hold a
// 4,000-character bullet, and a validator that reprints one has buried its verdict.
// 🔴 RUNES, NOT BYTES — the oracle slices a `str`.
func TestALongLineIsCutToTheDeclaredQuoteLength(t *testing.T) {
	long := strings.Repeat("é", 500)
	lines := ValidationAdvisoryLines(1,
		[]DroppedLineFinding{{Filename: "a.md", Offset: 1, Line: long}}, nil)
	var quoted string
	for _, ln := range lines {
		if strings.HasPrefix(strings.TrimSpace(ln), "é") {
			quoted = strings.TrimSpace(ln)
		}
	}
	if n := len([]rune(quoted)); n != AdvisoryQuoteMax {
		t.Fatalf("quote is %d runes, want %d", n, AdvisoryQuoteMax)
	}
}
