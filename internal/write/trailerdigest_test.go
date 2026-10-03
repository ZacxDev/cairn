package write

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

// THE CROSS-LANGUAGE HALF OF THE TRAILER STRIP — the GO side.
//
// 🔴 THE TWO STRIPS ARE NO LONGER ONE EXPRESSION IN TWO SPELLINGS. This side keeps
// the whole-run `(?:[ \t]*(?:ATTR|TOKEN))+\z` alternation; `server.py` peels the run
// one piece at a time, because the same shape is QUADRATIC under CPython's
// backtracking engine on any suffix the run does not end, and RE2 cannot backtrack —
// see `bulletTrailersRe` for both measurements. So "they agree" stopped being readable
// off a shared pattern and became a property that has to be MEASURED.
//
// This file is where it is measured: both languages enumerate the SAME declared axes,
// hash their own strip's output over them, and assert the SAME hex constant. One
// constant, two independent implementations — if either drifts on any enumerated
// input, both sides go red.
//
// ⚠ A DIGEST NAMES NO INPUT. It answers "do the two agree", never "where do they
// differ" — when it goes red, `citationtoken_test.go`'s per-row tests and
// `tests/test_bullet_trailer_strip.py`'s oracle comparison are what name rows. A
// digest is what makes this affordable in two languages without shipping a 10 MB
// fixture.
//
// ⚠ EVERY CHARACTER IN THE AXES IS ASCII, AND THAT IS DELIBERATE RATHER THAN
// INCIDENTAL: a trailer is ASCII, and an invisible character in a shared corpus would
// move one side's digest for a reason nobody could see on screen. Anything non-ASCII
// added here must be spelled as an escape.

// trailerStripAxisPieces is one axis of the declared enumeration: 20 trailer-ish
// pieces — 5 attribution spellings, 5 token spellings, 10 malformed near-misses.
// Spelled IDENTICALLY in `tests/test_bullet_trailer_strip.py`; the digest is only a
// cross-language claim while both lists are the same, so change them together or not
// at all.
var trailerStripAxisPieces = []string{
	" [cairn: zach/s1]", "[cairn: a/b]", "  [cairn: a-b/c.d_e]",
	"\t[cairn: z9/Z9]", " [cairn: abcdefghijklmnopqrstuvwxyz0123/S]",
	" [cb:deadbeef]", "[cb:00000000]", "\t[cb:ffffffff]",
	"   [cb:0123abcd]", " [cb:abcdef01]",
	" [cairn: /b]", " [cairn: a/]", " [cb:deadbee]", " [cb:deadbeeff]",
	" [cb:DEADBEEF]", " [cairn:a/b]", " [cairn: A/b]", " [cb:deadbeef",
	"cb:deadbeef]", " []",
}

// trailerStripAxisProses is the other axis: 10 prose prefixes, chosen so the
// enumeration reaches the shapes a suffix peel can get wrong — a prose that ENDS in
// `]`, one that CONTAINS a bracket, one that is itself a trailer, an empty one, a
// whitespace-only one, and one that already carries a bullet opener.
var trailerStripAxisProses = []string{
	"the drill head overheats", "x", "", "   ", "- 2000-01-02: a note",
	"ends in a bracket]", "starts with [", "[cb:deadbeef] mid prose",
	"a\tb", "trailing space ",
}

// trailerStripAxisInputs is 10 proses × (1 + 20 + 400 + 8000) suffix sequences of
// length 0..3. The axes are collision-free, so the count is exact rather than a
// ceiling — and it is asserted SEPARATELY, because a digest over an EMPTY enumeration
// is a perfectly stable hex string. Reachable rather than decorative: removing ONE of
// the 20 pieces reds the count assertion at 72,400, before the digest is read.
const trailerStripAxisInputs = 84210

// trailerStripAxisDigest is sha256 over `stripBulletTrailers(input) + "\x00"` for
// every enumerated input, in enumeration order.
//
// 🔴 `tests/test_bullet_trailer_strip.py` PINS THE SAME STRING for
// `_strip_bullet_trailers`, and that is the whole point: this constant is the only
// place the two implementations meet. Re-derive it by running either side, never by
// copying the other's failure message — a message quotes what the code DID.
const trailerStripAxisDigest = "a5f4c2e4116e202169a3d00cc1c39f9e4fc3e6380d4c5f128d7a8ab9212e970e"

// trailerStripAxisEach walks the declared enumeration in the order the digest is
// taken over: for each prose, the suffix sequences of length 0, then 1, then 2, then
// 3. The nesting is spelled out rather than built from a generic product helper so
// that the ORDER is readable against the Python half's `itertools.product`.
func trailerStripAxisEach(visit func(string)) {
	for _, prose := range trailerStripAxisProses {
		visit(prose)
		for _, a := range trailerStripAxisPieces {
			visit(prose + a)
		}
		for _, a := range trailerStripAxisPieces {
			for _, b := range trailerStripAxisPieces {
				visit(prose + a + b)
			}
		}
		for _, a := range trailerStripAxisPieces {
			for _, b := range trailerStripAxisPieces {
				for _, c := range trailerStripAxisPieces {
					visit(prose + a + b + c)
				}
			}
		}
	}
}

func TestTheTrailerStripDigestsToTheSameStringThePythonHalfPins(t *testing.T) {
	// ⚠ AN INVARIANT GUARD, LABELLED AS ONE: it was authored green, because the two
	// implementations were already equivalent when the peel landed. What it is FOR is
	// the next edit to EITHER side — and it is mutation-proven rather than assumed, on
	// the PYTHON side, where the mechanism actually changed: replacing that peel's body
	// with an UNANCHORED substitution moves the digest to `f347293a…` and reds the
	// twin assertion with its own message. ⚠ Swapping THIS side for the Python peel, or
	// for a correct-but-quadratic scan, does NOT move it — equivalent implementations
	// digest equal, which is the whole claim; those mutants are caught by the COST
	// guards in `openerseam_test.go` instead.
	//
	// 🔴 IT IS A SEAM GUARD, NOT A COMPONENT ONE. Both languages were verified in
	// isolation before the mechanisms diverged, and both were green; nothing measured
	// the RELATIONSHIP, because it used to be readable off a shared regex.
	sum := sha256.New()
	count := 0
	trailerStripAxisEach(func(text string) {
		sum.Write([]byte(stripBulletTrailers(text)))
		sum.Write([]byte{0})
		count++
	})
	if count != trailerStripAxisInputs {
		t.Fatalf("the enumeration produced %d inputs, not %d — the axes moved, so the "+
			"digest is a claim about a different corpus and the Python half is now "+
			"comparing something else", count, trailerStripAxisInputs)
	}
	got := hex.EncodeToString(sum.Sum(nil))
	if got != trailerStripAxisDigest {
		t.Errorf("the strip digests to %s over %d enumerated inputs, pinned %s — either "+
			"this implementation changed answer on one of them, or the axes above drifted "+
			"from `tests/test_bullet_trailer_strip.py`'s. `citationtoken_test.go`'s per-row "+
			"tests and that file's oracle comparison name rows; this one cannot",
			got, count, trailerStripAxisDigest)
	}
}
