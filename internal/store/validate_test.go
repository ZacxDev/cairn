package store

import (
	"context"
	"errors"
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
// finding, so the `OPEN:` below is surfaced by nothing in either scanner. Pinned so
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
