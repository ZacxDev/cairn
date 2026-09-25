package store

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
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

// ⚠ INVARIANT GUARDS, NOT REGRESSION COVERAGE, AND THAT IS THE WHOLE POINT OF
// SPLITTING THEM OUT. Every kind below fails the read by RETURNING AN ERROR, which
// `nuanceBody` handled before the classifier gate existed and handles now — so all
// five pass on pre-change code. The single-case version of this test claimed to
// prevent a crash and was the only evidence for it; the kinds that actually produced
// one do not return an error at all, and they are in
// `TestNonRegularPathsAreRefusedBeforeOpen`.
func TestAnUnreadableFileContributesNothingRatherThanFailing(t *testing.T) {
	dir := t.TempDir()
	aDir := filepath.Join(dir, "a-directory.md")
	if err := os.Mkdir(aDir, 0o755); err != nil {
		t.Fatal(err)
	}
	dangling := filepath.Join(dir, "dangling.md")
	if err := os.Symlink(filepath.Join(dir, "nothing-here"), dangling); err != nil {
		t.Fatal(err)
	}
	toDir := filepath.Join(dir, "to-a-directory.md")
	if err := os.Symlink(aDir, toDir); err != nil {
		t.Fatal(err)
	}
	sockPath := filepath.Join(dir, "a-socket.md")
	listener, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	for kind, path := range map[string]string{
		"absent":         filepath.Join(dir, "never-written.md"),
		"directory":      aDir,
		"broken-link":    dangling,
		"link-to-dir":    toDir,
		"other (socket)": sockPath,
	} {
		if got := ScanDroppedLines([]string{path}); len(got) != 0 {
			t.Errorf("%s dropped: want none, got %#v", kind, got)
		}
		if got := ScanUnreachableMarkers([]string{path}); len(got) != 0 {
			t.Errorf("%s unreachable: want none, got %#v", kind, got)
		}
	}
}

// 🔴 THE NEGATIVE CONTROL ON THE CLASSIFIER GATE, AND IT IS THE CELL THE WHOLE
// NARROW-VS-BROAD RULING WAS ABOUT. `link-to-file` is `Take` in
// `loaderEntryActions`: the loader reads a symlink to a regular `*.md` and always
// has. A gate spelled "regular files only" would make these scanners NARROWER than
// the loader — printing `dropped lines: 0 across N entry file(s)` over a file the
// denominator counted and the scanner never opened, which is the reassuring zero the
// advisory's own prose exists to refuse. A mutant that flips the gate to
// `ClassifyPath(path) == KindRegularFile` is killed here and nowhere else.
func TestASymlinkToARegularEntryIsStillScanned(t *testing.T) {
	dir := t.TempDir()
	real := entryWithNuance(t, dir, "talus-svc.md",
		"  the bullet opening that carried this line is gone.\n"+
			"- 2000-01-04: a bullet.")
	link := filepath.Join(dir, "linked-svc.md")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	got := ScanDroppedLines([]string{link})
	if len(got) != 1 || got[0].Filename != "linked-svc.md" || got[0].Offset != 1 {
		t.Fatalf("a symlinked entry must still be scanned, got %#v", got)
	}
}

// 🔴 THE TWO KINDS THAT DO NOT RETURN AN ERROR, WHICH IS WHY THEY GOT PAST AN
// `err != nil` CHECK AND PAST THE TEST THAT CLAIMED TO COVER THEM.
//
// Reading a FIFO BLOCKS until somebody writes to it — no error, no return, and
// `validate` never finishes. `LoadIndex` has refused `other` and `link-to-other`
// before `open()` since a fifo was measured wedging a request thread for 25s; the
// advisories added beside it opened every path unconditionally, so `cairn validate`
// over a cache holding one hung on BOTH clients where the commit before it exited 5.
//
// ⚠ IT RUNS IN A SUBPROCESS BECAUSE THE PRE-CHANGE FAILURE IS A HANG. An in-process
// call cannot be watched to fail — it never comes back, and a test that wedges the
// whole package is not a red, it is a dead runner. `-timeout` would kill the binary
// and report the panic against whatever test was running, which is a different and
// much worse signal. The context timeout is what makes the symptom observable, bounded
// and attributed.
func TestNonRegularPathsAreRefusedBeforeOpen(t *testing.T) {
	if os.Getenv(scanHelperEnv) != "" {
		// The helper half. `go test` re-execs this binary; this branch does the
		// scan and exits, so the parent measures a process rather than a call.
		for _, scan := range []func([]string) int{
			func(p []string) int { return len(ScanDroppedLines(p)) },
			func(p []string) int { return len(ScanUnreachableMarkers(p)) },
		} {
			if n := scan([]string{os.Getenv(scanHelperEnv)}); n != 0 {
				os.Exit(3)
			}
		}
		os.Exit(0)
	}

	dir := t.TempDir()
	fifo := filepath.Join(dir, "wedge.md")
	mkfifo(t, fifo)
	realFifo := filepath.Join(dir, "real-fifo")
	mkfifo(t, realFifo)
	linked := filepath.Join(dir, "wedge-through-a-link.md")
	if err := os.Symlink(realFifo, linked); err != nil {
		t.Fatal(err)
	}

	for kind, path := range map[string]string{
		// `open()` does not care which path shape reached the fifo, which is
		// exactly why the loader's table refuses both kinds and why one case
		// cannot stand for the other: they are two cells.
		"other":         fifo,
		"link-to-other": linked,
	} {
		// RED at `d6a4b91`: the child never returns and this is a DeadlineExceeded.
		ctx, cancel := context.WithTimeout(context.Background(), scanHelperTimeout)
		cmd := exec.CommandContext(ctx, os.Args[0],
			"-test.run=^TestNonRegularPathsAreRefusedBeforeOpen$")
		cmd.Env = append(os.Environ(), scanHelperEnv+"="+path)
		out, err := cmd.CombinedOutput()
		// 🔴 READ `ctx.Err()` BEFORE `cancel()`, NOT AFTER. A cancelled context
		// reports `context canceled` whether or not the deadline was reached, so
		// the tidy-up-first ordering turns every PASS into a wedge report — which
		// is how the first draft of this test failed on a tree that was correct.
		wedged := errors.Is(ctx.Err(), context.DeadlineExceeded)
		cancel()
		if wedged {
			t.Errorf("%s: the scanners WEDGED — the child did not return within %s "+
				"(output: %s)", kind, scanHelperTimeout, out)
			continue
		}
		if err != nil {
			t.Errorf("%s: helper failed: %v (output: %s)", kind, err, out)
		}
	}
}

// scanHelperEnv carries the path under test into the re-executed helper. Its
// PRESENCE is what selects the helper half, so it must never be a path that could
// legitimately be empty.
const scanHelperEnv = "CAIRN_TEST_SCAN_NONREGULAR_PATH"

// scanHelperTimeout is long enough that a loaded box does not flake it, short enough
// that a real wedge is not mistaken for slowness. The pre-change failure is INFINITE,
// so no value here can be too small in the direction that matters.
const scanHelperTimeout = 20 * time.Second

func mkfifo(t *testing.T, path string) {
	t.Helper()
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Fatalf("mkfifo %s: %v", path, err)
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

// 🔴 AN INVARIANT GUARD ON BLIND SPOT (2) — NOT regression coverage, and the one
// where the zero is actively misleading rather than partial. `IsFence` toggles, so an
// odd count leaves every following line fenced, and fenced lines are skipped as sample
// text by design. A bullet swallowed that way produces no bullet AND no dropped-line
// finding, so the `OPEN:` below is surfaced by nothing in any scanner. Pinned so
// the printed caveat keeps naming it.
func TestAnUnclosedFenceIsKnownInvisible(t *testing.T) {
	dir := t.TempDir()
	path := entryWithNuance(t, dir, "talus-svc.md",
		"```\n- 2000-01-04: OPEN: a whole bullet swallowed by one stray fence.")
	if got := ScanDroppedLines([]string{path}); len(got) != 0 {
		t.Fatalf("dropped: want none (fenced), got %#v", got)
	}
	if got := ScanUnreachableMarkers([]string{path}); len(got) != 0 {
		t.Fatalf("unreachable: want none (fenced), got %#v", got)
	}
}

// 🔴 AN INVARIANT GUARD ON BLIND SPOT (3) — NOT regression coverage. `ExtractSections`
// concatenates same-named sections, so the orphan under the SECOND heading arrives
// immediately after the FIRST section's bullet and is absorbed into it. The offsets
// this scanner reports index the concatenated body, which stops being the file's
// coordinate system once a heading repeats.
func TestADuplicatedNuanceHeadingIsKnownInvisible(t *testing.T) {
	dir := t.TempDir()
	path := entryWithNuance(t, dir, "talus-svc.md",
		"- 2000-01-04: a bullet under the FIRST heading.")
	extra := "\n" + NuanceHeading + "\n\n  an orphan under the SECOND heading.\n"
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(extra); err != nil {
		t.Fatal(err)
	}
	f.Close()
	if got := ScanDroppedLines([]string{path}); len(got) != 0 {
		t.Fatalf("want none (absorbed across the duplicated heading), got %#v", got)
	}
	// The positive control on the fixture: the orphan really is in the file, so the
	// zero above is about absorption and not about a fixture that never wrote it.
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "SECOND heading") {
		t.Fatal("the fixture never wrote the orphan, so the zero above proves nothing")
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
		// 🔴 THE TWO NARROWINGS `LineMentionsMarker`'S COMMENT DECLARED CLOSED, PINNED
		// WHERE A MUTANT CAN SEE THEM. The oracle compiles `_MARKER_ANYWHERE` with
		// `re.IGNORECASE` over the WHOLE pattern, so its `PR#\d+` folds; and neither
		// oracle pattern is `re.ASCII`, so `\d` is the Unicode `Nd` category. Both
		// were false here until `refAtomEnds` grew `foldPR` and `unicode.IsDigit`.
		// ⚠ They are TRUE cases, so a mutant that made this function always-false is
		// killed by half the table; what these two alone kill is a REVERT of either
		// convergence.
		{"  OPEN pr#12: a lowercase PR reference.", true},
		// 🔴 FIVE DIGITS, AND THE COUNT IS THE WHOLE FIXTURE. `[^A-Za-z0-9\n]{0,4}`
		// can span at most four non-alphanumerics, so a run of five non-ASCII digits
		// cannot be absorbed by the terminator and only the digit class can match it.
		// Two digits sit exactly ON that boundary, and with two the mutant SURVIVED a
		// fully green suite.
		// ⚠ That paragraph used to live INSIDE the string literal below — a four-line
		// blob masquerading as one line. The assertion discriminated anyway; it just
		// printed a comment as test data.
		{"  OPEN #١٢٣٤٥: a non-ASCII decimal digit reference.", true},
		{"  resolved upstream in 1.2.3.", false},
		{"  RESOLVED_ADDR appears in the trace…", false},
		// ⚠ AND SO IS THIS ONE, WHICH THE COMMENT BELOW USED TO CLAIM AS A BOUNDARY
		// WITNESS. Re-derived by mutation: delete the word-boundary guard and this
		// line is STILL rejected, by `terminatorColon` — `[^A-Za-z0-9\n]{0,4}:`
		// cannot cross `ADDR`, and `_ADDR` is no ref atom either. It is a second
		// sample of the line above it, one colon richer. Kept because it is a real
		// prose shape, relabelled because a comment asserting coverage it does not
		// provide is worse than none: it stops the next reader looking for a case
		// that does.
		{"  RESOLVED_ADDR: the symbol named in the trace.", false},
		// 🔴 THE WORD-BOUNDARY GUARD'S OWN DISCRIMINATING INPUT — ONE PER TOKEN,
		// because the guard runs once per word in the loop and a mutant can weaken
		// either. The colon sits within `terminatorColon`'s reach of the token itself
		// (`_` is not alphanumeric, so the run spans it), which means nothing but the
		// boundary can reject them: delete it and both flip to true.
		{"  OPEN_: an identifier, not a declaration.", false},
		{"  RESOLVED_: an identifier prefix, not a declaration.", false},
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

// 🔴 THE `foldPR` FLAG IS AN ASYMMETRY BETWEEN TWO ORACLE PATTERNS, NOT A SETTING —
// AND WITHOUT THIS TEST THE CHEAPEST "SIMPLIFICATION" IS TO DELETE IT.
//
// `_MARKER_ANYWHERE` is `re.IGNORECASE` over the whole pattern, so its ref run folds.
// `_NEAR_MISS_MARKER` scopes the fold to `(?i:OPEN|RESOLVED)` and leaves `PR#\d+`
// OUTSIDE it, so its ref run does not. Passing `true` from both call sites would make
// `nearMissMarker` WIDER than the oracle — inventing a near-miss badge nobody typed,
// which is the dangerous direction; passing `false` from both is the narrowing this
// change closed. The pair below is the only place the difference is observable.
//
// ⚠ `- Open …` rather than `- OPEN …` DELIBERATELY: the all-caps spelling is matched
// by the SHOUTED branch, which needs no terminator and no ref run at all, so a shouted
// fixture would be true whatever `foldPR` does. The sentence-cased spelling is the
// only one that reaches the walk.
func TestTheRefRunFoldsPROnlyForTheMarkerAnywherePattern(t *testing.T) {
	for _, c := range []struct {
		line              string
		anywhere, nearMis bool
	}{
		{"- Open PR#12: an upper-case PR reference.", true, true},
		{"- Open pr#12: a lower-case PR reference.", true, false},
		{"- Open #١٢٣٤٥: a non-ASCII decimal digit, Unicode `\\d` in BOTH patterns.",
			true, true},
	} {
		if got := LineMentionsMarker(c.line); got != c.anywhere {
			t.Errorf("LineMentionsMarker(%q) = %v, want %v", c.line, got, c.anywhere)
		}
		if got := nearMissMarker(c.line); got != c.nearMis {
			t.Errorf("nearMissMarker(%q) = %v, want %v", c.line, got, c.nearMis)
		}
	}
}

// 🔴 "0 across 0 entry file(s)" IS THE REASSURING ZERO FROM AN INSTRUMENT THAT
// WALKED NOTHING, and it must not render anywhere near a clean-looking count.
func TestNothingCheckedPrintsNotCheckedAndNeitherZero(t *testing.T) {
	lines := ValidationAdvisoryLines(0, nil, nil, nil, nil)
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
	blob := strings.Join(ValidationAdvisoryLines(7, nil, nil, nil, nil), "\n")
	for _, want := range []string{
		"dropped lines: 0 across 7 entry file(s)",
		"marker reachability: 0 out-of-reach marker(s) across 7 entry file(s)",
		// 🔴 THIS CHECK IS KNOWINGLY PARTIAL — AND IN THREE WAYS, WHERE THE
		// PRINTED SENTENCE USED TO NAME ONE. A bare "0 dropped" would read as "no
		// bullet has lost its head", which is a claim it cannot make; naming only
		// the absorbed tail was the same defect one size smaller, because the
		// sentence read as a complete enumeration and was not. Each name is
		// pinned to an invariant guard above, so the claim and the behaviour move
		// together.
		"PARTIAL BY CONSTRUCTION, IN THREE WAYS",
		"ABSORBED TAIL",
		"UNCLOSED FENCE",
		"DUPLICATED",
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
	blob := strings.Join(ValidationAdvisoryLines(1, nil,
		[]DroppedLineFinding{{Filename: "a.md", Offset: 1, Line: "  lost."}},
		nil, nil), "\n")
	dropped := strings.Index(blob, "DROPPED LINE(S)")
	reach := strings.Index(blob, "marker reachability:")
	if dropped < 0 || reach < 0 || dropped > reach {
		t.Fatalf("order wrong (dropped=%d reach=%d):\n%s", dropped, reach, blob)
	}
}

func TestFindingsAreQuotedWithTheirFileAndOffset(t *testing.T) {
	blob := strings.Join(ValidationAdvisoryLines(2, nil,
		[]DroppedLineFinding{
			{Filename: "talus-svc.md", Offset: 1, Line: "  lost prose."},
			{Filename: "talus-svc.md", Offset: 2, Line: "  OPEN: lost claim.",
				CarriesMarker: true},
		},
		nil,
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
	lines := ValidationAdvisoryLines(1, nil,
		[]DroppedLineFinding{{Filename: "a.md", Offset: 1, Line: long}}, nil, nil)
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

// --------------------------------------------------------------------------
// ScanEntryShape — the entry's SPINE, as opposed to whether it parses
// --------------------------------------------------------------------------

// shapedEntry writes an entry whose SECTIONS are given verbatim, front matter
// prepended. Deliberately not `entryWithNuance`, which hard-codes a correct spine —
// the whole point of these fixtures is that the spine is wrong.
func shapedEntry(t *testing.T, dir, name, sections string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	slug := strings.TrimSuffix(name, ".md")
	body := "---\nservice: " + slug + "\nscope: crag-notes\n---\n" + sections
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func shapeKinds(findings []ShapeFinding, kind string) []ShapeFinding {
	var out []ShapeFinding
	for _, f := range findings {
		if f.Kind == kind {
			out = append(out, f)
		}
	}
	return out
}

// The positive control for every test below: if a clean entry produced findings,
// every other assertion here would be about noise.
func TestACleanEntryYieldsNoShapeFindings(t *testing.T) {
	dir := t.TempDir()
	path := entryWithNuance(t, dir, "clean-svc.md", "- 2000-01-04: a bullet.")
	if got := ScanEntryShape([]string{path}); len(got) != 0 {
		t.Fatalf("want no findings, got %#v", got)
	}
}

// 🔴 RENAMED AND ABSENT ARE THE SAME MISSING SECTION AT TWO RESOLUTIONS, and the
// renamed one is the only half a writer can act on in a single edit. Collapsing them
// into "absent" sends someone looking for prose that is already on disk.
func TestACaseNearMissIsRenamedAndQuotesWhatTheWriterTyped(t *testing.T) {
	dir := t.TempDir()
	path := shapedEntry(t, dir, "moraine-cfg.md",
		"\n## pointers\n\n- `apps/moraine-cfg/values.yaml`\n"+
			"\n"+NuanceHeading+"\n\n- 2000-01-04: a bullet.\n")
	findings := ScanEntryShape([]string{path})
	renamed := shapeKinds(findings, ShapeRenamed)
	if len(renamed) != 1 {
		t.Fatalf("want one RENAMED, got %#v", findings)
	}
	if renamed[0].Heading != PointersHeading {
		t.Fatalf("finding is about %q", renamed[0].Heading)
	}
	if len(renamed[0].Found) != 1 || renamed[0].Found[0] != "## pointers" {
		t.Fatalf("want the written spelling, got %#v", renamed[0].Found)
	}
	// 🔴 AND IT IS NOT ALSO REPORTED ABSENT — two findings for one heading would
	// double the count a reader acts on.
	if got := shapeKinds(findings, ShapeAbsent); len(got) != 0 {
		t.Fatalf("also reported ABSENT: %#v", got)
	}
}

// The three near-misses the fold is declared to cover, each in its own file so a
// single assertion cannot pass on the wrong one.
//
// 🔴 THE WHITESPACE CASE IS AN *INTERNAL* RUN, NOT A LEADING ONE, AND THE FIRST DRAFT OF
// THIS TEST GOT IT WRONG. `##   Pointers` is folded by the `StripWhitespace` already in
// the chain, so the collapse (`pytext.CollapseWhitespace`) changes nothing — a mutant
// that DELETED it survived a green run of this very test. Only a run BETWEEN two words
// reaches it. That is the difference between a mutant that is breakable and a guard that
// is REACHABLE.
func TestALevelAColonAndAWhitespaceRunAllPairAsRenamed(t *testing.T) {
	dir := t.TempDir()
	for _, tc := range []struct{ name, written, schema string }{
		{"level-svc.md", "### Pointers", PointersHeading},
		{"colon-svc.md", "## Pointers:", PointersHeading},
		{"leading-svc.md", "##   Pointers", PointersHeading},
		{"internal-svc.md", "## Nuance /  work-history", NuanceHeading},
	} {
		// The heading NOT under test is spelled correctly, so exactly one finding
		// can exist and a passing assertion cannot be about the other.
		other := NuanceHeading
		if tc.schema == NuanceHeading {
			other = PointersHeading
		}
		path := shapedEntry(t, dir, tc.name,
			"\n"+tc.written+"\n\nbent section body.\n"+
				"\n"+other+"\n\n- 2000-01-04: a bullet.\n")
		findings := ScanEntryShape([]string{path})
		renamed := shapeKinds(findings, ShapeRenamed)
		if len(renamed) != 1 {
			t.Errorf("%s: want one RENAMED, got %#v", tc.name, findings)
			continue
		}
		if renamed[0].Heading != tc.schema || renamed[0].Found[0] != tc.written {
			t.Errorf("%s: want RENAMED of %q quoting %q, got %#v",
				tc.name, tc.schema, tc.written, renamed[0])
		}
	}
}

// 🔴 THE ONE PROPERTY THAT MAKES THE FOLD SAFE, AND IT IS A CLAIM ABOUT THE READER,
// NOT ABOUT THIS SCANNER. `headingKey` folds `## pointers` onto `## Pointers` so the
// report can name the typo. If that fold ever reached `ExtractSections`, the store
// would quietly start accepting a wider set of headings than every reader parses — the
// exact opposite of what a write-time check is for.
func TestTheFoldNeverWidensWhatExtractSectionsAccepts(t *testing.T) {
	text := "\n## pointers\n\n- `apps/x/values.yaml`\n" +
		"\n" + NuanceHeading + "\n\n- 2000-01-04: a bullet.\n"
	if headingKey("## pointers") != headingKey(PointersHeading) {
		t.Fatal("the fold must pair them, or the RENAMED report is impossible")
	}
	if _, ok := ExtractSections(text, []string{PointersHeading})[PointersHeading]; ok {
		t.Fatal("ExtractSections accepted a folded heading — the store just widened")
	}
}

// 🔴 THE CASE FOLD IS CPython's `str.lower()`, NOT `strings.ToLower`, AND EXACTLY ONE
// CODE POINT SEPARATES THEM. `pytext.Lower` tabulates the divergence; this pins that
// `headingKey` — the one place in this file that lowercases anything — actually goes
// through it. The expectation is written as the two runes the FULL mapping produces
// (`i` + U+0307 COMBINING DOT ABOVE), which is what the oracle's `.lower()` yields and
// what `strings.ToLower` does NOT: the simple mapping emits a bare `i`, so the key
// collides with the schema heading's and the file is reported RENAMED where the oracle
// reports ABSENT.
//
// ⚠ REACHABILITY: the `U+0130` is placed BETWEEN two letters, never at an end. Nothing
// earlier in the chain touches it — `TrimLeft("#")`, `StripWhitespace` and
// `TrimRight(":")` all leave an interior letter alone — so this assertion is about the
// lowercase step and no other.
func TestTheFoldIsCPythonsFullLowercaseMappingNotTheSimpleOne(t *testing.T) {
	// 🔴 ESCAPES, NOT LITERALS, FOR THE REASON `pytext` GIVES: U+0307 renders on top of
	// its neighbour and is invisible in source, so a literal here would be unreviewable.
	const dottedCapitalI = "\u0130"    // LATIN CAPITAL LETTER I WITH DOT ABOVE
	const combiningDotAbove = "\u0307" // COMBINING DOT ABOVE
	written := "## PO" + dottedCapitalI + "NTERS"
	want := "poi" + combiningDotAbove + "nters"
	if got := headingKey(written); got != want {
		t.Fatalf("headingKey(%q) = %q, want %q — the simple mapping would give %q and "+
			"pair this heading with the schema's, reporting RENAMED where the oracle "+
			"reports ABSENT", written, got, want, "pointers")
	}
	// The consequence, stated as the thing the report gets wrong rather than as a string:
	// the key must NOT collide with the schema heading's.
	if headingKey(written) == headingKey(PointersHeading) {
		t.Fatalf("a full-case-expansion heading paired with %q — the two clients now "+
			"disagree about ABSENT vs RENAMED", PointersHeading)
	}
}

// 🔴 THE INVENTORY IS WHAT MAKES AN ABSENT FINDING ACTIONABLE. "no `## Pointers`"
// alone leaves the writer re-reading a file they just wrote; the list of what IS there
// is how they see that they called it something else entirely.
func TestAHeadingNothingPairsWithIsAbsentAndCarriesTheInventory(t *testing.T) {
	dir := t.TempDir()
	path := shapedEntry(t, dir, "cirque-api.md",
		"\n## Links and references\n\n- `apps/cirque-api/values.yaml`\n"+
			"\n"+NuanceHeading+"\n\n- 2000-01-04: a bullet.\n")
	absent := shapeKinds(ScanEntryShape([]string{path}), ShapeAbsent)
	if len(absent) != 1 || absent[0].Heading != PointersHeading {
		t.Fatalf("want one ABSENT about Pointers, got %#v", absent)
	}
	want := []string{"## Links and references", NuanceHeading}
	if strings.Join(absent[0].Found, "|") != strings.Join(want, "|") {
		t.Fatalf("inventory is %#v, want %#v", absent[0].Found, want)
	}
}

// A bound, not a filter. NINE headings against a cap of six, deliberately: nine
// OVERSHOOTS the cap rather than being a multiple of it, so a mutant that dropped the
// slice, or sliced by a different constant, cannot land on the same rendered string.
func TestTheInventoryIsCappedAndTheRemainderIsCounted(t *testing.T) {
	dir := t.TempDir()
	var sections strings.Builder
	for i := 1; i <= 9; i++ {
		fmt.Fprintf(&sections, "\n## Section %d\n\nprose %d.\n", i, i)
	}
	path := shapedEntry(t, dir, "many-svc.md", sections.String())
	findings := ScanEntryShape([]string{path})
	absent := shapeKinds(findings, ShapeAbsent)
	if len(absent) != 2 {
		t.Fatalf("want both spine headings ABSENT, got %#v", findings)
	}
	if len(absent[0].Found) != 9 {
		t.Fatalf("inventory holds %d headings, want 9", len(absent[0].Found))
	}
	blob := strings.Join(ValidationAdvisoryLines(11, findings, nil, nil, nil), "\n")
	for _, want := range []string{"… 3 more", "`## Section 6`"} {
		if !strings.Contains(blob, want) {
			t.Errorf("missing %q in:\n%s", want, blob)
		}
	}
	if strings.Contains(blob, "`## Section 7`") {
		t.Errorf("the cap did not apply:\n%s", blob)
	}
}

// 🔴 THE DISJOINTNESS CLAIM, MADE OBSERVABLE. `duplicated` and `empty` are facts about
// the same heading with different remedies — "fold them into one section" and "write
// something under it" — and a reader handed one of the two has half a remedy. Neither
// branch in ScanEntryShape is an `else`, and this is the fixture that can tell.
//
// THREE occurrences, not two: a mutant that hard-coded the count, or that reported
// presence as a boolean, cannot produce the rendered `appears 3 times`.
func TestADuplicatedHeadingWhoseMergedBodyIsEmptyYieldsBothFindings(t *testing.T) {
	dir := t.TempDir()
	path := shapedEntry(t, dir, "dupempty-svc.md",
		"\n## Pointers\n\n- `apps/dupempty-svc/values.yaml`\n"+
			"\n"+NuanceHeading+"\n"+
			"\n"+NuanceHeading+"\n"+
			"\n"+NuanceHeading+"\n")
	findings := ScanEntryShape([]string{path})
	dup := shapeKinds(findings, ShapeDuplicated)
	empty := shapeKinds(findings, ShapeEmpty)
	if len(dup) != 1 || dup[0].Count != 3 {
		t.Fatalf("want one DUPLICATED with Count 3, got %#v", findings)
	}
	if len(empty) != 1 {
		t.Fatalf("want one EMPTY beside it, got %#v", findings)
	}
	if dup[0].Heading != NuanceHeading || empty[0].Heading != NuanceHeading {
		t.Fatalf("findings are about %q / %q", dup[0].Heading, empty[0].Heading)
	}
	// 🔴 AND THE TWO ARE NEVER SUMMED INTO ONE NUMBER.
	blob := strings.Join(ValidationAdvisoryLines(5, findings, nil, nil, nil), "\n")
	for _, want := range []string{
		"1 heading(s) DUPLICATED", "1 section(s) PRESENT AND EMPTY",
	} {
		if !strings.Contains(blob, want) {
			t.Errorf("missing %q in:\n%s", want, blob)
		}
	}
	if strings.Contains(blob, "2 heading(s) DUPLICATED") {
		t.Errorf("the two kinds were summed:\n%s", blob)
	}
}

// The other side of the same disjointness: the `empty` branch must be a branch on the
// BODY, not a side effect of the duplicate count.
func TestADuplicatedHeadingWithANonEmptyBodyIsNotAlsoEmpty(t *testing.T) {
	dir := t.TempDir()
	path := shapedEntry(t, dir, "dupfull-svc.md",
		"\n## Pointers\n\n- `apps/dupfull-svc/values.yaml`\n"+
			"\n"+NuanceHeading+"\n\n- 2000-01-04: one.\n"+
			"\n"+NuanceHeading+"\n\n- 2000-01-05: two.\n")
	findings := ScanEntryShape([]string{path})
	if len(shapeKinds(findings, ShapeDuplicated)) != 1 {
		t.Fatalf("want one DUPLICATED, got %#v", findings)
	}
	if got := shapeKinds(findings, ShapeEmpty); len(got) != 0 {
		t.Fatalf("also reported EMPTY over a non-empty body: %#v", got)
	}
}

// `ExtractSections` tracks presence separately from content precisely so this
// distinction survives, and the remedies differ: "the section was never started" sends
// you to another file, "it is there and unfilled" does not.
func TestAPresentButEmptyHeadingIsNotReportedAbsent(t *testing.T) {
	dir := t.TempDir()
	path := shapedEntry(t, dir, "empty-svc.md",
		"\n## Pointers\n"+"\n"+NuanceHeading+"\n\n- 2000-01-04: a bullet.\n")
	findings := ScanEntryShape([]string{path})
	if len(findings) != 1 || findings[0].Kind != ShapeEmpty ||
		findings[0].Heading != PointersHeading {
		t.Fatalf("want one EMPTY about Pointers, got %#v", findings)
	}
}

// 🔴 THE SAME GATE AS THE OTHER SCANNERS, AND IT IS NOT AN `err != nil`. Reading a FIFO
// returns no error — it BLOCKS until somebody writes, and `validate` never returns.
// This scanner reads the WHOLE file rather than one section, which is exactly the route
// that could have bypassed the gate.
func TestScanEntryShapeRefusesAPathNoScannerMayOpen(t *testing.T) {
	dir := t.TempDir()
	fifo := filepath.Join(dir, "wedge.md")
	if err := syscall.Mkfifo(fifo, 0o644); err != nil {
		t.Skipf("mkfifo unavailable: %v", err)
	}
	real := entryWithNuance(t, dir, "real-svc.md", "- 2000-01-04: a bullet.")
	if got := ScanEntryShape([]string{real, fifo}); len(got) != 0 {
		t.Fatalf("want no findings, got %#v", got)
	}
}

// --------------------------------------------------------------------------
// ScanOpenActions — the four openness populations, never summed
// --------------------------------------------------------------------------

// One bullet per population, plus the two the scan must be SILENT about.
var populationBullets = map[string]string{
	"open":         "- 2000-01-05: OPEN: the writer declared this one, exactly.",
	"near-miss":    "- 2000-01-06: **OPEN**: emphasis, so the marker never parses.",
	"unmarked":     "- 2000-01-07: Open items: the retry budget is not yet addressed.",
	"unverifiable": "- 2000-01-08: RESOLVED: closed, and naming no sha at all.",
	"resolved":     "- 2000-01-09: RESOLVED abc1234: closed, and reported by nothing.",
	"none":         "- 2000-01-10: an ordinary bullet about an ordinary thing.",
}

// allPopulations joins the six in a fixed order — a map iteration would reorder the
// bullets between runs, and the openness precedence is per-bullet so the order does not
// change the answer, but a fixture whose bytes move is not a fixture.
func allPopulations() string {
	order := []string{"open", "near-miss", "unmarked", "unverifiable", "resolved", "none"}
	rows := make([]string, 0, len(order))
	for _, k := range order {
		rows = append(rows, populationBullets[k])
	}
	return strings.Join(rows, "\n")
}

// 🔴 SIX POPULATIONS IN, FOUR OUT. `resolved` and `none` are the control: a scanner
// that reported every bullet it saw would produce six records and satisfy any
// "at least" assertion.
func TestAllFourPopulationsAreReportedAndTheOtherTwoAreSilent(t *testing.T) {
	dir := t.TempDir()
	path := entryWithNuance(t, dir, "populations-svc.md", allPopulations())
	actions := ScanOpenActions([]string{path})
	if len(actions) != 4 {
		t.Fatalf("want four records, got %d: %#v", len(actions), actions)
	}
	var declared, near, unverifiable, guessed []OpenAction
	for _, a := range actions {
		// 🔴 EXACTLY ONE FLAG EACH. A record with two is how one bullet came to be
		// counted twice on one surface while being one thing on another.
		n := 0
		for _, f := range []bool{a.Declared, a.NearMiss, a.UnverifiableClosure} {
			if f {
				n++
			}
		}
		if n > 1 {
			t.Fatalf("record carries %d flags: %#v", n, a)
		}
		switch {
		case a.Declared:
			declared = append(declared, a)
		case a.NearMiss:
			near = append(near, a)
		case a.UnverifiableClosure:
			unverifiable = append(unverifiable, a)
		default:
			guessed = append(guessed, a)
		}
	}
	for pop, rows := range map[string][]OpenAction{
		"open": declared, "near-miss": near, "unverifiable": unverifiable,
		"unmarked": guessed,
	} {
		if len(rows) != 1 {
			t.Fatalf("%s: want one record, got %#v", pop, rows)
		}
		if rows[0].FirstLine != populationBullets[pop] {
			t.Errorf("%s: quoted %q", pop, rows[0].FirstLine)
		}
	}
	// The two controls appear in NO record at all.
	for _, silent := range []string{"resolved", "none"} {
		for _, a := range actions {
			if a.FirstLine == populationBullets[silent] {
				t.Errorf("%s was reported: %#v", silent, a)
			}
		}
	}
}

// The dates in the fixture are pairwise distinct and none of them is a constant any
// assertion or any renderer names, so a scanner that invented a date — or reused one
// bullet's — cannot land on the right answer.
func TestTheDateIsCarriedFromTheBulletNotReconstructed(t *testing.T) {
	dir := t.TempDir()
	path := entryWithNuance(t, dir, "dates-svc.md", allPopulations())
	byLine := map[string]string{}
	for _, a := range ScanOpenActions([]string{path}) {
		byLine[a.FirstLine] = a.Date
	}
	for pop, want := range map[string]string{
		"open": "2000-01-05", "unverifiable": "2000-01-08",
	} {
		if got := byLine[populationBullets[pop]]; got != want {
			t.Errorf("%s: date is %q, want %q", pop, got, want)
		}
	}
}

// 🔴 THE ORDERING ARGUMENT, AS BEHAVIOUR RATHER THAN AS A COMMENT. This scan reads only
// the nuance heading, so an entry whose heading is renamed yields zero open actions NO
// MATTER HOW MANY IT HOLDS — and the `entry shape:` block is the only one that can say
// why. That is why it prints ABOVE this one: read in the other order, `0 declared` is
// false.
func TestARenamedNuanceHeadingMakesTheOpenActionScanFindNothing(t *testing.T) {
	dir := t.TempDir()
	path := shapedEntry(t, dir, "renamed-svc.md",
		"\n## Pointers\n\n- `apps/renamed-svc/values.yaml`\n"+
			"\n## Nuance / Work-History\n\n"+allPopulations()+"\n")
	if got := ScanOpenActions([]string{path}); len(got) != 0 {
		t.Fatalf("want nothing, got %#v", got)
	}
	renamed := shapeKinds(ScanEntryShape([]string{path}), ShapeRenamed)
	if len(renamed) != 1 || renamed[0].Heading != NuanceHeading {
		t.Fatalf("the shape scan did not report it: %#v", renamed)
	}
}

func TestScanOpenActionsRefusesAPathNoScannerMayOpen(t *testing.T) {
	dir := t.TempDir()
	fifo := filepath.Join(dir, "wedge.md")
	if err := syscall.Mkfifo(fifo, 0o644); err != nil {
		t.Skipf("mkfifo unavailable: %v", err)
	}
	if got := ScanOpenActions([]string{fifo}); len(got) != 0 {
		t.Fatalf("want nothing, got %#v", got)
	}
}

// --------------------------------------------------------------------------
// The two NEW blocks, rendered
// --------------------------------------------------------------------------

// 🔴 A ZERO THAT DOES NOT NAME ITS SET IS A CLAIM ABOUT A HEADING NOTHING EXAMINED. The
// denominator says how many files; the SET says which headings — and without the second
// half a reader who assumes `## What it is` was checked takes this line as a guarantee
// about it.
//
// The denominator here is 13, which is not any length in this file's fixtures and not
// any constant the block names, so a renderer that printed a finding count, a cap, or a
// hard-coded number cannot produce it.
func TestTheShapeZeroCarriesItsDenominatorAndTheSetItChecked(t *testing.T) {
	var block string
	for _, ln := range ValidationAdvisoryLines(13, nil, nil, nil, nil) {
		if strings.HasPrefix(ln, "entry shape:") {
			block = ln
		}
	}
	if block == "" {
		t.Fatal("no `entry shape:` line at all")
	}
	wants := []string{"13 entry file(s) checked for", "`" + WhatHeading + "` is NOT checked here"}
	for _, h := range ShapeHeadings {
		wants = append(wants, "`"+h+"`")
	}
	for _, want := range wants {
		if !strings.Contains(block, want) {
			t.Errorf("missing %q in: %s", want, block)
		}
	}
	// The ORDER is part of the printed contract the parity gate compares.
	if strings.Index(block, "`"+ShapeHeadings[0]+"`") >
		strings.Index(block, "`"+ShapeHeadings[1]+"`") {
		t.Errorf("the spine printed out of order: %s", block)
	}
}

// 🔴 THE PRINTED SET AND THE CHECKED SET ARE ONE OBJECT, AND ITERATING `ShapeHeadings`
// IS NOT ENOUGH TO SAY SO. The test above builds its expectation by iterating, which
// makes it survive the set GROWING — but a renderer that spelled the current two
// headings as a LITERAL satisfies it exactly, because the literal and the derived string
// are the same bytes while the set never moves. MEASURED at this commit: replacing
// `entryShapeBlock`'s `strings.Join(quoted, ", ")` with that literal left the whole
// `internal/store` package GREEN. The oracle's twin had the same hole, in the same
// shape, and both are closed the same way — by growing the set and deriving the
// expectation from the GROWN value.
//
// ⚠ `ShapeHeadings` IS A PACKAGE VAR AND THIS TEST WRITES IT. Restored by `t.Cleanup`,
// and no test in this package calls `t.Parallel`, so nothing observes the widened set.
func TestTheSpineIsDerivedFromShapeHeadingsAndNotReTyped(t *testing.T) {
	original := ShapeHeadings
	t.Cleanup(func() { ShapeHeadings = original })
	// A heading no source file in this repo spells, so a re-typed list cannot carry it
	// by accident.
	grown := append(append([]string{}, original...), "## Provenance / where it came from")
	ShapeHeadings = grown

	var block string
	for _, ln := range ValidationAdvisoryLines(13, nil, nil, nil, nil) {
		if strings.HasPrefix(ln, "entry shape:") {
			block = ln
		}
	}
	if block == "" {
		t.Fatal("no `entry shape:` line at all")
	}
	at := make([]int, 0, len(grown))
	for _, h := range grown {
		i := strings.Index(block, "`"+h+"`")
		if i < 0 {
			t.Fatalf("the spine dropped %q after `ShapeHeadings` grew — it is re-typed, "+
				"not derived: %s", h, block)
		}
		at = append(at, i)
	}
	for i := 1; i < len(at); i++ {
		if at[i-1] > at[i] {
			t.Fatalf("the spine printed out of order at %q: %s", grown[i], block)
		}
	}
}

// 🔴 FOUR DISJOINT KINDS, FOUR SUB-BLOCKS, FOUR COUNTS. A single "4 problems" total
// would be the number a writer acts on, and every one of the four needs a different
// edit.
func TestTheShapeFindingsBranchRendersEachKindSeparatelyAndSumsNothing(t *testing.T) {
	shape := []ShapeFinding{
		{Filename: "a.md", Heading: PointersHeading, Kind: ShapeRenamed,
			Found: []string{"## pointers"}},
		{Filename: "b.md", Heading: PointersHeading, Kind: ShapeAbsent,
			Found: []string{"## Elsewhere"}},
		{Filename: "c.md", Heading: NuanceHeading, Kind: ShapeDuplicated, Count: 3},
		{Filename: "d.md", Heading: NuanceHeading, Kind: ShapeEmpty},
	}
	blob := strings.Join(ValidationAdvisoryLines(13, shape, nil, nil, nil), "\n")
	for _, want := range []string{
		"entry shape across 13 entry file(s), checked for",
		"🔴 1 section(s) RENAMED",
		"    a.md: `## Pointers` is written as `## pointers`",
		"🔴 1 section(s) ABSENT",
		"    b.md: no `## Pointers`; the file's headings are `## Elsewhere`",
		"🔴 1 heading(s) DUPLICATED",
		"    c.md: `" + NuanceHeading + "` appears 3 times",
		"⚠ 1 section(s) PRESENT AND EMPTY",
		"    d.md: `" + NuanceHeading + "`",
	} {
		if !strings.Contains(blob, want) {
			t.Errorf("missing %q in:\n%s", want, blob)
		}
	}
	// 🔴 NO SUMMED TOTAL.
	for _, forbidden := range []string{"4 section(s)", "4 heading(s)"} {
		if strings.Contains(blob, forbidden) {
			t.Errorf("found a summed total %q in:\n%s", forbidden, blob)
		}
	}
}

// An empty inventory must read as a sentence, not as a dangling `are `.
func TestAnAbsentFindingWithNoHeadingsAtAllSaysSo(t *testing.T) {
	blob := strings.Join(ValidationAdvisoryLines(13,
		[]ShapeFinding{{Filename: "e.md", Heading: NuanceHeading, Kind: ShapeAbsent}},
		nil, nil, nil), "\n")
	if !strings.Contains(blob, "the file's headings are (none at all)") {
		t.Fatalf("missing the empty-inventory sentence in:\n%s", blob)
	}
}

// 🔴 THE SECOND HALF OF THIS ZERO IS A FLOOR WITH UNKNOWN RECALL, NOT A CLEAN BILL OF
// HEALTH. `OPEN:` is exact; the unmarked guess is two measured phrasings, so an
// unfinished action phrased any other way is invisible — and a line that did not say so
// would be read as "there are none".
func TestTheOpenActionsZeroCarriesItsDenominatorAndNamesTheFloor(t *testing.T) {
	var block string
	for _, ln := range ValidationAdvisoryLines(13, nil, nil, nil, nil) {
		if strings.HasPrefix(ln, "open actions:") {
			block = ln
		}
	}
	if block == "" {
		t.Fatal("no `open actions:` line at all")
	}
	for _, want := range []string{
		"0 declared across 13 entry file(s)", "FLOOR with unknown recall",
	} {
		if !strings.Contains(block, want) {
			t.Errorf("missing %q in: %s", want, block)
		}
	}
}

// 🔴 THE FLOOR MUST NOT MASQUERADE AS A COUNT. `Declared` is exact and `unmarked` is a
// guess; one total over both is a number nobody can act on.
//
// The four sub-counts are 2, 3, 5 and 7 — pairwise distinct, none equal to their total
// (17) or to any pairwise sum, so a renderer that added any two of them cannot land on a
// string these assertions accept.
func TestTheFourPopulationsRenderSeparatelyAndAreNeverSummed(t *testing.T) {
	rows := func(tag string, n int, set func(*OpenAction)) []OpenAction {
		out := make([]OpenAction, 0, n)
		for i := 0; i < n; i++ {
			a := OpenAction{
				Filename:  fmt.Sprintf("%s-%d.md", tag, i),
				Date:      "2000-01-04",
				FirstLine: fmt.Sprintf("- 2000-01-04: %s row %d.", tag, i),
			}
			set(&a)
			out = append(out, a)
		}
		return out
	}
	var actions []OpenAction
	actions = append(actions, rows("declared", 2, func(a *OpenAction) { a.Declared = true })...)
	actions = append(actions, rows("near", 3, func(a *OpenAction) { a.NearMiss = true })...)
	actions = append(actions, rows("guess", 5, func(a *OpenAction) {})...)
	actions = append(actions, rows("unverifiable", 7,
		func(a *OpenAction) { a.UnverifiableClosure = true })...)
	if len(actions) != 17 {
		t.Fatalf("fixture holds %d rows, want 17", len(actions))
	}
	blob := strings.Join(ValidationAdvisoryLines(13, nil, nil, actions, nil), "\n")
	for _, want := range []string{
		"open actions across 13 entry file(s):",
		"🔴 2 declared `OPEN:`",
		"🔴 3 bullet(s) look like an ATTEMPTED marker",
		"⚠ 5 unmarked bullet(s)",
		"⚠ 7 `RESOLVED:` bullet(s) name no sha",
	} {
		if !strings.Contains(blob, want) {
			t.Errorf("missing %q in:\n%s", want, blob)
		}
	}
	if strings.Contains(blob, "17") {
		t.Errorf("a summed total leaked into:\n%s", blob)
	}
}

// The same bound the sibling blocks use, and the same reason: a validator that reprints
// a 4,000-character bullet has buried its own verdict.
// 🔴 RUNES, NOT BYTES — the oracle slices a `str`.
func TestALongOpenActionFirstLineIsCutToTheDeclaredQuoteLength(t *testing.T) {
	long := strings.Repeat("é", 500)
	lines := ValidationAdvisoryLines(13, nil, nil,
		[]OpenAction{{Filename: "a.md", Declared: true, Date: "2000-01-04",
			FirstLine: long}}, nil)
	var quoted string
	for _, ln := range lines {
		if strings.HasPrefix(strings.TrimSpace(ln), "a.md: é") {
			quoted = strings.TrimSpace(ln)
		}
	}
	if quoted == "" {
		t.Fatalf("no quoted line in:\n%s", strings.Join(lines, "\n"))
	}
	if n := len([]rune(quoted)); n != len("a.md: ")+AdvisoryQuoteMax {
		t.Fatalf("quote is %d runes, want %d", n, len("a.md: ")+AdvisoryQuoteMax)
	}
}

// Both new blocks say they are advisory, in their own words, because the remedies
// differ and a shared sentence would name the wrong one for one of them.
func TestBothNewBlocksSayTheyAreAdvisory(t *testing.T) {
	blob := strings.Join(ValidationAdvisoryLines(13,
		[]ShapeFinding{{Filename: "f.md", Heading: NuanceHeading, Kind: ShapeEmpty}},
		nil,
		[]OpenAction{{Filename: "a.md", Declared: true, Date: "2000-01-04",
			FirstLine: "- 2000-01-04: OPEN: x."}},
		nil), "\n")
	for _, want := range []string{
		"changes no verdict", "Fix the heading, not the exit code.",
		"None of this changes the verdict", "red gate nobody could turn green",
	} {
		if !strings.Contains(blob, want) {
			t.Errorf("missing %q in:\n%s", want, blob)
		}
	}
}

// 🔴 THE ORDER IS THE CLAIM, NOT A LAYOUT PREFERENCE.
//
// Three of the four blocks read only the nuance heading. A renamed heading makes every
// one of them read an empty section, so each prints a zero about a section no parser
// reached — and `entry shape:` is the only block that can say why. Printed below them it
// is an afterthought; printed above them it is the reason. `dropped lines:` then
// precedes the two bullet-level blocks for the sibling reason: a dropped line is content
// NO reader reaches, so neither of those scans ever sees it.
func TestTheFourBlocksPrintInOrder(t *testing.T) {
	order := []string{
		"entry shape:", "dropped lines:", "open actions:", "marker reachability:",
	}
	lines := ValidationAdvisoryLines(13, nil, nil, nil, nil)
	at := make([]int, 0, len(order))
	for _, want := range order {
		idx := -1
		for i, ln := range lines {
			if strings.HasPrefix(ln, want) {
				idx = i
				break
			}
		}
		if idx < 0 {
			t.Fatalf("no %q block in:\n%s", want, strings.Join(lines, "\n"))
		}
		at = append(at, idx)
	}
	for i := 1; i < len(at); i++ {
		if at[i-1] >= at[i] {
			t.Fatalf("blocks out of order: %v for %v", at, order)
		}
	}
}

func TestTheFourBlocksPrintInOrderOnTheFindingsBranch(t *testing.T) {
	blob := strings.Join(ValidationAdvisoryLines(13,
		[]ShapeFinding{{Filename: "a.md", Heading: NuanceHeading, Kind: ShapeEmpty}},
		[]DroppedLineFinding{{Filename: "a.md", Offset: 1, Line: "  lost."}},
		[]OpenAction{{Filename: "a.md", Declared: true, Date: "2000-01-04",
			FirstLine: "- 2000-01-04: OPEN: x."}},
		[]UnreachableMarkerFinding{{Filename: "a.md",
			BulletFirstLine: "- 2000-01-04: a bullet.", Offset: 2,
			Line: "  OPEN: x.", Openness: OpennessOpen}}), "\n")
	prev := -1
	for _, want := range []string{
		"entry shape across", "DROPPED LINE(S)", "open actions across",
		"MARKER(S) OUT OF REACH",
	} {
		at := strings.Index(blob, want)
		if at <= prev {
			t.Fatalf("%q at %d, out of order (previous %d):\n%s", want, at, prev, blob)
		}
		prev = at
	}
}

// 🔴 ALL-OR-NOTHING. A run that withheld the two older blocks and still printed
// `entry shape: 0 entry file(s) checked` would emit exactly the reassuring zero the
// `NOT CHECKED` branch exists to prevent — and the `0 across 0` assertion cannot see it,
// because that is not the spelling either new block uses.
func TestNotCheckedWithholdsAllFourAndNamesThem(t *testing.T) {
	lines := ValidationAdvisoryLines(0, nil, nil, nil, nil)
	if len(lines) != 1 {
		t.Fatalf("want one line, got %#v", lines)
	}
	for _, leader := range []string{
		"entry shape:", "dropped lines:", "open actions:", "marker reachability:",
	} {
		if strings.HasPrefix(lines[0], leader) {
			t.Fatalf("a block printed anyway: %q", lines[0])
		}
	}
	for _, named := range []string{
		"entry shape", "dropped lines", "open actions", "marker reachability",
	} {
		if !strings.Contains(lines[0], named) {
			t.Errorf("the line does not name %q: %q", named, lines[0])
		}
	}
}
