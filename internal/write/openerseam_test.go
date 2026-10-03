package write

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ZacxDev/cairn/internal/pytext"
	"github.com/ZacxDev/cairn/internal/store"
)

// The OPENER SEAM between `BulletRequestProblem` and `BulletContent` — the GO half.
//
// 🔴 WHAT THE DEFECT WAS. `write.go` asserted that `BulletContent`'s opener strip is
// "inert for a request" because the validator "refuses a `text` that opens a markdown
// bullet". The two asked DIFFERENT questions of DIFFERENT strings: the validator
// tested `strings.HasPrefix(lstripped, "- ")`, an ASCII space, on the RAW text, while
// `BulletContent` strips `bulletOpenerRe` from the COLLAPSED text — and
// `pytext.CollapseWhitespace`'s class carries U+00A0, U+1680, U+2000..U+200A, U+202F,
// U+205F and U+3000. So a `text` whose opener used any of those was accepted, reduced
// to its bare prose, and hashed equal to an unrelated stored bullet.
//
// 🔴 AND IT IS A REGRESSION, NOT AN INVARIANT: measured over one synthetic entry,
// `POST "-<U+00A0><prose>"` straight after `POST "<prose>"` answered `appended` at
// 3fb8dc2 (the request side was hashed raw, so no opener strip ran), `duplicate` at
// 0aa5fc48 with the caller's text never written, and 400 here.
//
// ⚠ EVERY NON-ASCII WHITESPACE BELOW IS SPELLED AS AN ESCAPE, NEVER PASTED. A literal
// U+00A0 in a source file is indistinguishable on screen from a space, which is the
// very confusion the defect is made of.
//
// `tests/test_bullet_trailer_strip.py` carries the Python twin of these tests.

// collapseWhitespaceOpeners are the non-ASCII members of the collapse class that
// REACH the opener clause. U+0085, U+2028 and U+2029 are deliberately absent: they are
// line breaks, so the one-line clause refuses them several clauses earlier, and a
// fixture built on one would pass this test for the wrong reason.
//
// ⚠ IT IS A SAMPLE OF A RANGE, NOT THE RANGE: U+2000 and U+200A are the ENDPOINTS of
// U+2000..U+200A and the nine between them are not listed. Said because the list reads
// like an enumeration. The whole class was swept at this head instead — all 29 code
// points `str.isspace()`/`CollapseWhitespace` accept, each as `"-" + ws + prose`
// through the Python validator, which is the oracle for this clause — and it leaves no
// gap behind the sample: 0 ACCEPTED; 17 refused by the OPENER clause (U+0020, U+00A0,
// U+1680, all of U+2000..U+200A, U+202F, U+205F, U+3000); 10 refused EARLIER by the
// one-line clause (U+000A..U+000D, U+001C, U+001D, U+001E, U+0085, U+2028, U+2029);
// and 2 refused earlier by the category clause (U+0009, U+001F). So the nine
// unsampled members are covered by the opener clause exactly as the endpoints are, and
// U+001C..U+001F are absent for TWO different reasons rather than one.
var collapseWhitespaceOpeners = []rune{
	0x00a0, 0x1680, 0x2000, 0x200a, 0x202f, 0x205f, 0x3000,
}

func TestAnOpenerSpelledWithNonASCIIWhitespaceIsRefusedRatherThanDeduped(t *testing.T) {
	for _, r := range collapseWhitespaceOpeners {
		for _, opener := range []string{"-", "*"} {
			text := opener + string(r) + trailerProse
			problem := BulletRequestProblem([]byte(`{}`), map[string]any{
				"text": text, "session": "s1",
			})
			if problem == "" {
				t.Errorf("U+%04X after %q: ACCEPTED, and `BulletContent` reduces it to %q "+
					"— the same content as a bare prose bullet, so the append answers "+
					"`duplicate` and the caller's text is never stored",
					r, opener, BulletContent([]string{text}))
				continue
			}
			if !strings.Contains(problem, "must not open a markdown bullet") {
				t.Errorf("U+%04X after %q: refused for the wrong reason: %q", r, opener, problem)
			}
		}
	}
}

func TestTheValidatorRefusesEveryTextWhoseCOLLAPSEDFormOpensABullet(t *testing.T) {
	// 🔴 THE SEAM AS A RELATIONSHIP RATHER THAN A CHARACTER LIST. The property
	// `write.go` now asserts is "no accepted `text` reaches `bulletOpenerRe`", so this
	// asks exactly that of every row — a spelled list of whitespace characters would
	// pass while the next one nobody thought of walked straight through.
	//
	// ⚠ IT IS AN INVARIANT GUARD OVER THE ROWS THE TEST ABOVE ALREADY COVERS, so it is
	// not counted twice as regression coverage; what it adds is the SHRINK direction —
	// a future narrowing of the clause fails here even if it keeps refusing the seven
	// characters named above.
	ws := []string{
		"", " ", "  ",
		"\u00a0", "\u1680", "\u2000", "\u200a", "\u202f", "\u205f", "\u3000",
		" \u00a0", "\u00a0 ",
	}
	heads := []string{"", "-", "*", "--", "-x", "x", "a-", " ", "\u00a0"}
	tails := []string{"", trailerProse, "2000-01-02: " + trailerProse, "-", "*", "]"}

	accepted, refused := 0, 0
	for _, h := range heads {
		for _, w := range ws {
			for _, tl := range tails {
				text := h + w + tl
				problem := BulletRequestProblem([]byte(`{}`), map[string]any{
					"text": text, "session": "s1",
				})
				if problem != "" {
					refused++
					continue
				}
				accepted++
				collapsed := pytext.CollapseWhitespace(text)
				if bulletOpenerRe.MatchString(collapsed) {
					t.Errorf("%q is ACCEPTED yet `bulletOpenerRe` matches its collapsed "+
						"form %q — `BulletContent` will strip an opener the CALLER sent, "+
						"so the content hash is taken over somebody else's bullet",
						text, collapsed)
				}
			}
		}
	}
	// POSITIVE CONTROL: a zero here would be indistinguishable from a loop that never
	// entered its body, and the accepted branch is the only one that asserts anything.
	if accepted == 0 {
		t.Fatal("not one row was ACCEPTED, so the assertion above never ran")
	}
	if refused == 0 {
		t.Fatal("not one row was REFUSED, so this table cannot see the clause at all")
	}
	t.Logf("%d accepted, %d refused, %d rows", accepted, refused, accepted+refused)
}

func TestAnOpenerWithNothingAfterItIsStillRefused(t *testing.T) {
	// ⚠ THE DUAL HAZARD THE SENTINEL SPACE CLOSES, AND THE REASON THE CLAUSE IS NOT
	// SIMPLY `bulletOpenerRe` OVER THE COLLAPSED TEXT. The collapse drops a trailing
	// whitespace run, so `"- "` arrives as `"-"`, which `[ \t]+` does not match — and
	// the raw-prefix check this replaced DID refuse it. Without the sentinel the fix
	// would have been wider on one axis and NARROWER on another.
	for _, text := range []string{"-", "*", "- ", "* ", "-  ", "-\u00a0", " -\u00a0"} {
		problem := BulletRequestProblem([]byte(`{}`), map[string]any{
			"text": text, "session": "s1",
		})
		if !strings.Contains(problem, "must not open a markdown bullet") {
			t.Errorf("%q: got %q, want the markdown-opener refusal", text, problem)
		}
	}
	// The NEGATIVE control for the same clause: no whitespace after the dash is prose,
	// here AND in `BulletContent`, and must stay accepted.
	for _, text := range []string{"-foo", "*foo", "--foo", "x - y"} {
		if problem := BulletRequestProblem([]byte(`{}`), map[string]any{
			"text": text, "session": "s1",
		}); problem != "" {
			t.Errorf("%q is prose and must be accepted, got %q", text, problem)
		}
	}
}

func TestANonASCIIOpenerDoesNotSilentlyDedupeEndToEnd(t *testing.T) {
	// 🔴 THE DEFECT AS BEHAVIOUR, because the assertions above are about the validator
	// and the damage lived in the SEAM: a unit test of either side alone stays green.
	dir := t.TempDir()
	path := filepath.Join(dir, "entry.md")
	body := "---\nservice: synth\n---\n\n## What it is\n\nx\n\n## Pointers\n\n- y\n\n" +
		store.NuanceHeading + "\n\n- 2000-01-01: an older note.\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	status, _, _, err := AppendBullet(path, trailerProse, "zach", "s1", "2000-01-02", nil)
	if err != nil {
		t.Fatal(err)
	}
	if status != "appended" {
		t.Fatalf("the first append is %q, so the premise of this test is gone", status)
	}

	disguised := "-\u00a0" + trailerProse
	if problem := BulletRequestProblem([]byte(`{}`), map[string]any{
		"text": disguised, "session": "s1",
	}); problem == "" {
		// The validator let it through, so measure what the write path then does — the
		// failure must name the OUTCOME, not merely the missing refusal.
		status2, line2, _, err := AppendBullet(path, disguised, "zach", "s1", "2000-01-03", nil)
		if err != nil {
			t.Fatal(err)
		}
		after, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		t.Fatalf("a U+00A0 opener was accepted and the append answered %q naming %q; "+
			"is the caller's own text in the file? %v",
			status2, line2, strings.Contains(string(after), disguised))
	}
}

// trailerStripBudget is the wall-clock ceiling for ONE `BulletContent` call over a
// stored line of many trailers. A hundred-to-thousandfold-margin number rather than a
// tight one — the measurements that set it are on the test below.
const trailerStripBudget = time.Second

// trailerStripShapeRatioMax bounds NOT-END cost ÷ AT-END cost at the same n, measured
// back to back in ONE process. It pins the SHAPE axis rather than the machine: a
// wall-clock budget can be met by a fast box, a ratio cannot. Measured 1.10 and 1.13 at
// the two ASSERTED points (n=4,000 and n=8,000), so 50 leaves a factor of 44.
//
// ⚠ ON THIS SIDE IT IS AN INVARIANT GUARD, NOT REGRESSION COVERAGE: RE2 does not
// backtrack, so neither this bound nor the NOT-END budget row below has ever been red
// on any Go implementation this repository has carried — `0aa5fc48`'s loop was
// quadratic on AT-END and returned immediately on NOT-END. It exists because the Python
// counterpart's identical row IS regression coverage, and because the two fixtures are
// meant to be readable against each other.
//
// 🔴 AND IT IS MUTATION-PROVEN REACHABLE RATHER THAN MERELY PRESENT, which is the thing
// an invariant guard most often is not. Replacing `stripBulletTrailers` with a CORRECT
// but NOT-END-quadratic strip — scan `i` from 0 and take the first full-suffix match,
// the Go analogue of what CPython's engine does internally — turns both this bound and
// the NOT-END budget row RED with their own messages (n=8,000: 2m34s, 1863×) while
// every other test in this package stays green.
const trailerStripShapeRatioMax = 50.0

func TestTheTrailerStripIsLinearRatherThanQuadratic(t *testing.T) {
	// 🔴 A REGRESSION GUARD ON CPU, AND THE REGRESSION WAS REACHABLE ON THE DEPLOYED
	// WRITE PATH. `0aa5fc48` made `stripBulletTrailers` a LOOP over two end-anchored
	// expressions, so every iteration re-scanned from position 0: O(trailers × length).
	// `BulletContent` runs once per stored bullet on every `POST /bullets`, inside the
	// per-entry write lock, and there is no per-line length cap on the bytes a write can
	// put on disk — `BulletTextMax` governs only the REQUEST text, and the rate limiter
	// counts failed auths only, so an authenticated writer is not request-rate-limited.
	//
	// 🔴 WHICH SHAPES THIS FIXTURE COVERS, NAMED, BECAUSE IT COVERS EXACTLY TWO. Both
	// are `"- 2000-01-02: " + trailerProse + trailerToken*n`: AT-END as-is, so the
	// trailer run reaches end-of-line; NOT-END with one non-trailer word appended, so
	// it does not. The AT-END shape ALONE is what this fixture built before `cee28805`,
	// and that gap is why `server.py`'s whole-run `(?:[ \t]*(?:ATTR|TOKEN))+\Z` shipped
	// green there while being quadratic on NOT-END under CPython's backtracking engine.
	// What is still NOT covered: a run interleaving attributions and tokens, a run of
	// malformed near-misses, a trailer-like suffix in the MIDDLE of a long line, and any
	// multi-line bullet.
	//
	// MEASURED on this host over both shapes in ONE process in ONE run, Go 1.25,
	// `BulletContent` end to end:
	//
	//	n        line bytes   AT-END     NOT-END    NOT-END/AT-END
	//	1,000        14,059   368µs      381µs      1.03
	//	4,000        56,059   1.421ms    1.559ms    1.10
	//	8,000       112,059   3.337ms    3.762ms    1.13
	//	16,000      224,059   5.716ms    8.843ms    1.55
	//	64,000      896,059   27.44ms    33.46ms    1.22
	//
	// 🔴 TWO POINTS ASSERTED, NAMED, because one measurement is not a claim about a
	// curve; the five rows above are ×1.7-2.4 per doubling on both shapes, which is what
	// "linear" means here. The budget sits 266-641× ABOVE the measured cost at the two
	// asserted points.
	for _, n := range []int{4000, 8000} {
		atEnd := "- 2000-01-02: " + trailerProse + strings.Repeat(trailerToken, n)
		notEnd := atEnd + " tail"

		start := time.Now()
		gotAtEnd := BulletContent([]string{atEnd})
		atEndElapsed := time.Since(start)

		start = time.Now()
		gotNotEnd := BulletContent([]string{notEnd})
		notEndElapsed := time.Since(start)

		if gotAtEnd != trailerProse {
			t.Fatalf("n=%d AT-END: reduced to %q (%d bytes), want %q — this measures the "+
				"wrong thing if the strip is not also CORRECT at scale",
				n, truncateForFailure(gotAtEnd), len(gotAtEnd), trailerProse)
		}
		// NOT-END: the run does not reach the anchor, so NOTHING is a trailer and only
		// the opener comes off. Pinned so a strip that got fast by stripping too much
		// cannot pass this test.
		wantNotEnd := trailerProse + strings.Repeat(trailerToken, n) + " tail"
		if gotNotEnd != wantNotEnd {
			t.Fatalf("n=%d NOT-END: the strip removed a suffix that does not reach "+
				"end-of-line — %d bytes, want %d",
				n, len(gotNotEnd), len(wantNotEnd))
		}

		for _, shape := range []struct {
			label   string
			elapsed time.Duration
			line    string
		}{{"AT-END", atEndElapsed, atEnd}, {"NOT-END", notEndElapsed, notEnd}} {
			if shape.elapsed > trailerStripBudget {
				t.Errorf("n=%d %s (%d line bytes): BulletContent took %s, budget %s — the "+
					"strip is super-linear in the number of trailers on the %s suffix "+
					"shape, which is a write-path CPU exhaustion reachable by an "+
					"authenticated writer",
					n, shape.label, len(shape.line), shape.elapsed, trailerStripBudget,
					shape.label)
			}
		}

		ratio := float64(notEndElapsed) / float64(atEndElapsed)
		if ratio >= trailerStripShapeRatioMax {
			t.Errorf("n=%d: NOT-END cost %s is %.0fx AT-END's %s, bound %.0fx — the "+
				"strip's cost depends on WHERE the trailer run ends, which is the "+
				"signature of a backtracking re-scan and a write-path CPU exhaustion "+
				"reachable by an authenticated writer",
				n, notEndElapsed, ratio, atEndElapsed, trailerStripShapeRatioMax)
		} else {
			t.Logf("n=%d (%d line bytes): AT-END %s, NOT-END %s, ratio %.2f",
				n, len(atEnd), atEndElapsed, notEndElapsed, ratio)
		}
	}
}

func truncateForFailure(s string) string {
	if len(s) <= 80 {
		return s
	}
	return s[:80] + fmt.Sprintf("…(+%d bytes)", len(s)-80)
}
