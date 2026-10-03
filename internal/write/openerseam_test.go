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
// stored line of many trailers. A thousandfold-margin number rather than a tight one —
// the measurements that set it are on the test below.
const trailerStripBudget = time.Second

func TestTheTrailerStripIsLinearRatherThanQuadratic(t *testing.T) {
	// 🔴 A REGRESSION GUARD ON CPU, AND THE REGRESSION WAS REACHABLE ON THE DEPLOYED
	// WRITE PATH. `0aa5fc48` made `stripBulletTrailers` a LOOP over two end-anchored
	// expressions, so every iteration re-scanned from position 0: O(trailers × length).
	// `BulletContent` runs once per stored bullet on every `POST /bullets`, inside the
	// per-entry write lock, and there is no per-line length cap on the bytes a write can
	// put on disk — `BulletTextMax` governs only the REQUEST text, and the rate limiter
	// counts failed auths only, so an authenticated writer is not request-rate-limited.
	//
	// MEASURED on this host over one stored line of n trailing ` [cb:deadbeef]` tokens,
	// Go 1.25, the strip alone:
	//
	//	n        line bytes   loop     one alternation
	//	1                48   2.9µs    0.6µs
	//	1,000        14,034   234ms    0.30ms
	//	4,000        56,034   4.33s    1.15ms
	//	16,000      224,034   72.8s    4.71ms
	//
	// 🔴 TWO POINTS, NAMED, because one measurement is not a claim about a curve. The
	// budget sits three orders of magnitude ABOVE the linear cost at both points and a
	// factor of 4 and 17 BELOW the quadratic cost at them — so no plausible machine or
	// `-race` slowdown turns a red into a green or the reverse.
	for _, n := range []int{4000, 8000} {
		line := "- 2000-01-02: " + trailerProse + strings.Repeat(trailerToken, n)
		start := time.Now()
		got := BulletContent([]string{line})
		elapsed := time.Since(start)
		if got != trailerProse {
			t.Fatalf("n=%d: reduced to %q (%d bytes), want %q — this measures the wrong "+
				"thing if the strip is not also CORRECT at scale",
				n, truncateForFailure(got), len(got), trailerProse)
		}
		if elapsed > trailerStripBudget {
			t.Errorf("n=%d (%d line bytes): BulletContent took %s, budget %s — the strip "+
				"is super-linear in the number of trailers, which is a write-path CPU "+
				"exhaustion reachable by an authenticated writer",
				n, len(line), elapsed, trailerStripBudget)
		} else {
			t.Logf("n=%d (%d line bytes): %s", n, len(line), elapsed)
		}
	}
}

func truncateForFailure(s string) string {
	if len(s) <= 80 {
		return s
	}
	return s[:80] + fmt.Sprintf("…(+%d bytes)", len(s)-80)
}
