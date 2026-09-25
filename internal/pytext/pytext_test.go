package pytext

import (
	"bytes"
	"strings"
	"testing"
	"unicode/utf8"
)

// 🔴 NO SEPARATOR IS PASTED INTO THIS FILE AS A LITERAL CHARACTER, for the reason the
// package comment gives: an invisible separator in source is indistinguishable from
// its neighbour on screen, so a test built out of them is a test nobody can review.
// Every case below constructs the character from the package's own rune constant,
// which also means a mistranscribed constant fails the test rather than hiding in it.
var (
	ls = string(lineSep)  // U+2028 LINE SEPARATOR
	ps = string(paraSep)  // U+2029 PARAGRAPH SEPARATOR
	nl = string(nel)      // U+0085 NEXT LINE
	fs = string(fileSep)  // U+001C
	gs = string(groupSep) // U+001D
	rs = string(recordSep)
	us = string(unitSep)
	// U+00A0 NO-BREAK SPACE: whitespace to `str.split()`, and NOT a line
	// break. It is the control that separates the two predicates.
	nbs = string(rune(0xa0))
)

func TestLineBreaksIsTen(t *testing.T) {
	// 🔴 TEN, NOT TWO. The validator this mirrors used to read
	// `if "\n" in text or "\r" in text`, which is a membership test on two characters
	// standing in for a predicate about ten — and the eight it missed included U+2028,
	// measured being accepted `200 appended` and rendering as TWO lines, the first with
	// no attribution trailer at all.
	//
	// The COUNT is asserted as well as the membership, because a transcription that
	// dropped one would otherwise pass every membership check written for the nine that
	// survived.
	if count := utf8.RuneCountInString(LineBreaks); count != 10 {
		t.Fatalf("LineBreaks holds %d characters, want 10", count)
	}
	for _, r := range []rune{'\n', '\r', '\v', '\f', fileSep, groupSep, recordSep, nel, lineSep, paraSep} {
		if !strings.ContainsRune(LineBreaks, r) {
			t.Fatalf("LineBreaks is missing U+%04X", r)
		}
		if !isLineBreak(r) {
			t.Fatalf("isLineBreak does not recognise U+%04X, which the set names", r)
		}
	}
	// 🔴 THE TABLE AND THE PREDICATE ARE PINNED AGAINST EACH OTHER IN BOTH DIRECTIONS.
	// A predicate that recognised MORE than the set names would split on a character
	// the oracle keeps, which is the same defect wearing the other sign.
	for r := rune(0); r < 0x3000; r++ {
		if isLineBreak(r) != strings.ContainsRune(LineBreaks, r) {
			t.Fatalf("isLineBreak and LineBreaks disagree about U+%04X", r)
		}
	}
	// U+001F is whitespace to `str.split()` and is NOT a line break — the one C0
	// separator that belongs to only one of the two sets. A transcription that put it in
	// both would split a bullet the oracle keeps whole.
	if strings.ContainsRune(LineBreaks, unitSep) {
		t.Fatal("U+001F is not a line break; `str.splitlines()` does not split on it")
	}
	if !isPySpace(unitSep) {
		t.Fatal("…but it IS whitespace to `str.split()`")
	}
}

func TestSplitLines(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []string
	}{
		{"a trailing break does NOT produce a final empty element", "a\nb\n", []string{"a", "b"}},
		{"no trailing break", "a\nb", []string{"a", "b"}},
		{"CRLF is ONE break", "a\r\nb", []string{"a", "b"}},
		{"a lone CR is a break", "a\rb", []string{"a", "b"}},
		{"U+2028 is a break", "a" + ls + "b", []string{"a", "b"}},
		{"U+2029 is a break", "a" + ps + "b", []string{"a", "b"}},
		{"U+0085 is a break", "a" + nl + "b", []string{"a", "b"}},
		{"U+001C is a break", "a" + fs + "b", []string{"a", "b"}},
		{"U+001D is a break", "a" + gs + "b", []string{"a", "b"}},
		{"U+001E is a break", "a" + rs + "b", []string{"a", "b"}},
		{"U+001F is NOT a break", "a" + us + "b", []string{"a" + us + "b"}},
		{"a no-break space is not a break", "a" + nbs + "b", []string{"a" + nbs + "b"}},
		{"the empty string is no lines at all", "", nil},
		{"a break alone is one empty line", "\n", []string{""}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := SplitLines(tc.in)
			if len(got) != len(tc.want) || strings.Join(got, "|") != strings.Join(tc.want, "|") {
				t.Fatalf("SplitLines(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestSplitLinesKeepEndsRoundTrips(t *testing.T) {
	// 🔴 THE SPLICE OFFSET IS DERIVED FROM THE JOINED LENGTH OF THE KEPT-ENDS LINES,
	// so a split that loses or invents a byte is an off-by-one inside a file the caller
	// cannot see. The round trip is the property, asserted over every shape including
	// invalid UTF-8.
	for _, in := range []string{
		"", "a", "a\n", "a\r\nb\n", "a\r\rb", "a" + ls + "b" + ps + "c",
		"## Heading\n- bullet\n", "no newline at all",
		"invalid \xff byte\nsecond line\n",
	} {
		if joined := strings.Join(SplitLinesKeepEnds(in), ""); joined != in {
			t.Fatalf("round trip lost bytes: SplitLinesKeepEnds(%q) joins to %q", in, joined)
		}
	}
}

func TestAnInvalidByteIsNotASeparator(t *testing.T) {
	// 🔴 THE PARALLEL TO PYTHON'S LONE SURROGATE, AND IT AGREES FOR A DIFFERENT
	// MECHANICAL REASON — which is why it is measured rather than assumed. A raw 0x85
	// byte is NOT U+0085: the oracle's `surrogateescape` decode maps it to a surrogate,
	// which its splitter does not split on, and here it decodes to the replacement rune
	// with width 1, which this splitter does not split on either.
	if lines := SplitLines("a\x85b"); len(lines) != 1 {
		t.Fatalf("a raw 0x85 byte must not split: %q", lines)
	}
	// …while its correctly-encoded form does.
	if lines := SplitLines("a" + nl + "b"); len(lines) != 2 {
		t.Fatalf("U+0085 encoded as UTF-8 must split: %q", lines)
	}
}

func TestSplitWhitespaceCountsTheC0Separators(t *testing.T) {
	// 🔴 A SUPERSET OF Go's OWN NOTION OF WHITESPACE. CPython counts U+001C..U+001F as
	// whitespace and Unicode's White_Space property does not, so `strings.Fields` would
	// collapse a DIFFERENT string — and the append idempotency key is a hash over the
	// collapse, so a stored bullet carrying one of those bytes would hash differently
	// and land as a NEW bullet instead of the duplicate it is.
	for _, r := range []rune{fileSep, groupSep, recordSep, unitSep} {
		in := "a" + string(r) + "b"
		if got := CollapseWhitespace(in); got != "a b" {
			t.Fatalf("U+%04X must collapse as whitespace: CollapseWhitespace(%q) = %q", r, in, got)
		}
		// 🔴 THE CONTROL ON THE CLAIM ITSELF. If Go's own helper already treated this
		// character as whitespace, this test could not see the difference it was written
		// for — a green that proves nothing. Asserting the control FAILS is what makes
		// the case above a measurement.
		if got := strings.Join(strings.Fields(in), " "); got == "a b" {
			t.Fatalf("the control failed: strings.Fields already treats U+%04X as "+
				"whitespace, so this test is vacuous", r)
		}
	}
	// The ordinary cases, so the wide predicate has not broken the common one.
	for _, tc := range []struct{ in, want string }{
		{"  a   b  ", "a b"},
		{"a\tb", "a b"},
		{"a" + nbs + "b", "a b"},
		{"", ""},
	} {
		if got := CollapseWhitespace(tc.in); got != tc.want {
			t.Fatalf("CollapseWhitespace(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestTrimLineBreaksCatchesAnEdgeBreak(t *testing.T) {
	// The clause that covers a LEADING or TRAILING break, which the splitter folds away
	// (`"a\n"` is one line) and which would otherwise open or close a stored bullet with
	// an empty line.
	for _, in := range []string{"\na", "a\n", ls + "a", "a" + ps} {
		if TrimLineBreaks(in) == in {
			t.Fatalf("an edge break must be visible in %q", in)
		}
	}
	// 🔴 SPACES ARE NOT LINE BREAKS. A trim that also ate whitespace would make the
	// one-line clause fire on every bullet that happens to end in a space.
	if TrimLineBreaks(" a ") != " a " {
		t.Fatal("TrimLineBreaks must not strip spaces")
	}
	if TrimLineBreaks("a"+nbs+"b") != "a"+nbs+"b" {
		t.Fatal("a no-break space is whitespace but not a line break")
	}
}

func TestStripOneLineBreak(t *testing.T) {
	// It removes the single break the kept-ends split retained, and NOTHING else —
	// including a trailing space the heading line is entitled to keep.
	for _, tc := range []struct{ in, want string }{
		{"## H  \n", "## H  "},
		{"## H\r\n", "## H"},
		{"## H", "## H"},
		{"## H ", "## H "}, // a trailing SPACE is not a line break
		{"## H" + ls, "## H"},
		{"", ""},
	} {
		if got := StripOneLineBreak(tc.in); got != tc.want {
			t.Fatalf("StripOneLineBreak(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestContainsSpace(t *testing.T) {
	if !ContainsSpace("a b") || !ContainsSpace("a"+string(unitSep)+"b") {
		t.Fatal("ContainsSpace must use the same predicate the collapse uses")
	}
	if ContainsSpace("abc") {
		t.Fatal("…and must not fire on ordinary text")
	}
}

// 🔴 `str.lower()` APPLIES THE FULL LOWERCASE MAPPING AND `strings.ToLower` APPLIES THE
// SIMPLE ONE. The expectations below are transcribed from the pinned interpreter, never
// from this implementation.
//
// 🔴 THE NAME OVERSTATES THE SCOPE, AND THIS PARAGRAPH IS THE CORRECTION RATHER THAN A
// RENAME. What this test pins is the UNCONDITIONAL rule (U+0130's expansion). It is
// STRUCTURALLY UNABLE to see the CONTEXTUAL one — Final_Sigma — in either half: the table
// holds no multi-letter Greek word, and the structural sweep below compares
// `Lower(string(r))` against `strings.ToLower(string(r))` ONE CODE POINT AT A TIME, where
// an isolated `Σ` lowercases to `σ` on both sides because Final_Sigma needs a cased letter
// BEFORE the sigma. So no widening of that sweep can reach rule 2, however many code
// points it walks. `TestLowerIsCPythonExceptForFinalSigma` below is the guard that does,
// and `Lower`'s docstring carries the decision not to implement it.
func TestLowerMatchesCPython(t *testing.T) {
	// U+0130 LATIN CAPITAL LETTER I WITH DOT ABOVE, built from its code point rather than
	// pasted, per this file's header — and U+0307 is invisible beside an `i` on screen,
	// which is the whole reason that rule exists.
	dotted := string(rune(0x0130))
	dot := string(rune(0x0307))

	for _, tc := range []struct{ in, want string }{
		{dotted, "i" + dot},
		{dotted + "a", "i" + dot + "a"},
		{"a" + dotted + "b", "ai" + dot + "b"},
		{dotted + dotted, "i" + dot + "i" + dot},
		// The ordinary path must be untouched, in both scripts — a special case that
		// leaked would show up here first.
		{"ABC", "abc"},
		{"I", "i"},
		{"ǅ", "ǆ"}, // a titlecase letter, which has a simple mapping and no expansion
		{"", ""},
	} {
		if got := Lower(tc.in); got != tc.want {
			t.Fatalf("Lower(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}

	// 🔴 THE STRUCTURAL HALF: U+0130 IS THE **ONLY** CODE POINT THIS FUNCTION TREATS
	// SPECIALLY, asserted over the whole range rather than over a table somebody chose.
	// It fails when the set GROWS (a second special case added without a decision) and
	// when it SHRINKS (the special case removed), which is the property a row-by-row table
	// cannot express. It needs no Python at run time: the claim is about the RELATIONSHIP
	// between this function and `strings.ToLower`, and the one place they may differ.
	diverged := []rune{}
	for r := rune(0); r <= 0x10FFFF; r++ {
		if r >= 0xD800 && r <= 0xDFFF {
			continue // a lone surrogate is not a code point a Go string can carry
		}
		s := string(r)
		if Lower(s) != strings.ToLower(s) {
			diverged = append(diverged, r)
		}
	}
	if len(diverged) != 1 || diverged[0] != 0x0130 {
		t.Fatalf("Lower must differ from strings.ToLower at U+0130 and nowhere else, "+
			"differs at %d code point(s): %U", len(diverged), diverged)
	}

	// 🔴 AND THE SWEEP ABOVE NEVER LEAVES THE FAST PATH, WHICH A MUTANT PROVED RATHER THAN
	// A REVIEW SUSPECTING IT. Every single-code-point string except U+0130 itself misses
	// `strings.ContainsRune(s, dottedCapitalI)` and returns from the first branch, so the
	// per-rune loop is UNREACHABLE from the loop above — a second special case added inside
	// that loop (`r == dottedCapitalI || r == 'Q'`) SURVIVED this whole package's tests.
	// That is the "breakable but unreachable" shape: the guard existed, the earlier check
	// always won, and the mutation died nowhere.
	//
	// So the loop gets its OWN sweep, over strings that DO contain U+0130. The property is
	// that the expansion happens exactly at the U+0130 and every other code point passes
	// through the SIMPLE mapping untouched — asserted at both ends and the middle of the
	// string, because a special case keyed on position would otherwise show up at one only.
	for r := rune(0); r <= 0x10FFFF; r++ {
		if (r >= 0xD800 && r <= 0xDFFF) || r == 0x0130 {
			continue
		}
		side := strings.ToLower(string(r))
		in := string(r) + dotted + string(r)
		want := side + "i" + dot + side
		if got := Lower(in); got != want {
			t.Fatalf("the per-rune loop must apply the SIMPLE mapping to every code point "+
				"but U+0130: Lower(%+q) = %+q, want %+q", in, got, want)
		}
	}
}

// 🔴 THE SECOND DIVERGENCE FROM `str.lower()`, WHICH NO SINGLE-CODE-POINT SWEEP CAN SEE.
// `TestLowerMatchesCPython` pins the UNCONDITIONAL rule; Unicode's full lowercase mapping
// has a CONTEXTUAL one too — Final_Sigma — and `Lower` deliberately does not implement it.
// This is the LEDGER of both, so the claim is machine-readable instead of prose: every row
// carries CPython's answer BESIDE what `Lower` returns, and the test fails when a row moves
// IN EITHER DIRECTION. That includes the day somebody teaches `Lower` Final_Sigma — a
// deliberate change then has to update this ledger and the DECISION in `Lower`'s docstring
// together, which is the only way the two can stay in agreement.
//
// ⚠ THE `cpython` COLUMN IS TRANSCRIBED FROM THE PINNED INTERPRETER (3.12.14), NEVER FROM
// THIS IMPLEMENTATION — measured as `s.lower()` on each `in` below. The escapes are numeric
// for the reason this file's header gives: U+0307 renders on top of its neighbour, and
// U+03C2 and U+03C3 are one stroke apart at a glance.
func TestLowerIsCPythonExceptForFinalSigma(t *testing.T) {
	const (
		dotted   = "\u0130" // LATIN CAPITAL LETTER I WITH DOT ABOVE
		dot      = "\u0307" // COMBINING DOT ABOVE — Case_Ignorable, and INVISIBLE in source
		capSigma = "\u03a3" // GREEK CAPITAL LETTER SIGMA
		sigma    = "\u03c3" // GREEK SMALL LETTER SIGMA
		finalSig = "\u03c2" // GREEK SMALL LETTER FINAL SIGMA
		capAlpha = "\u0391" // GREEK CAPITAL LETTER ALPHA — a cased letter BEFORE the sigma
		alpha    = "\u03b1" // GREEK SMALL LETTER ALPHA
		capBeta  = "\u0392" // GREEK CAPITAL LETTER BETA — a cased letter after the space
		beta     = "\u03b2" // GREEK SMALL LETTER BETA
	)
	rows := []struct {
		name    string
		in      string
		cpython string // the pinned interpreter's `.lower()`
		want    string // what `Lower` returns
		agrees  bool   // whether the two above are the same string
	}{
		// RULE 1 — UNCONDITIONAL, IMPLEMENTED. The expansion, in the positions
		// `NormalizeRef`'s own notes show behave differently from each other.
		{"U+0130 alone", dotted, "i" + dot, "i" + dot, true},
		{"U+0130 before a letter", dotted + "a", "i" + dot + "a", "i" + dot + "a", true},
		{"U+0130 between letters", "a" + dotted + "b", "ai" + dot + "b", "ai" + dot + "b", true},
		{"U+0130 doubled", dotted + dotted, "i" + dot + "i" + dot, "i" + dot + "i" + dot, true},

		// RULE 2 — CONTEXTUAL, NOT IMPLEMENTED. These three rows ARE the divergence,
		// spelled out rather than described in a sentence.
		{"sigma ending a word", "A" + capSigma, "a" + finalSig, "a" + sigma, false},
		{"sigma before a space", capAlpha + capSigma + " " + capBeta,
			alpha + finalSig + " " + beta, alpha + sigma + " " + beta, false},
		{"sigma after sigma", capSigma + capSigma, sigma + finalSig, sigma + sigma, false},
		// 🔴 AND THE ROW THAT MAKES THE COST CLAIM CONCRETE: a Case_Ignorable code point
		// after the sigma does NOT stop Final_Sigma firing. Getting this row right is what
		// needs the `Case_Ignorable` property, which Go's `unicode` package does not ship —
		// the second ground in `Lower`'s decision.
		{"sigma then a case-ignorable mark", "A" + capSigma + dot,
			"a" + finalSig + dot, "a" + sigma + dot, false},
		// Both rules in one string: the U+0130 expansion must still happen in a word that
		// also ends in a sigma, so the fast path's `ContainsRune` branch is exercised
		// TOGETHER with the sigma rather than on its own.
		{"both rules at once", dotted + capSigma, "i" + dot + finalSig, "i" + dot + sigma, false},

		// …and the contexts where Final_Sigma does NOT fire, which is what makes the rows
		// above a claim about CONTEXT rather than about the code point. Without these a
		// blanket `Σ -> σ` and a blanket `Σ -> ς` would be indistinguishable here.
		{"sigma alone — nothing cased precedes it", capSigma, sigma, sigma, true},
		{"sigma with a cased letter after it", "A" + capSigma + "B", "a" + sigma + "b",
			"a" + sigma + "b", true},
	}

	for _, tc := range rows {
		t.Run(tc.name, func(t *testing.T) {
			// The ledger's own consistency: `agrees` is derived information and a row that
			// mislabels itself would read as coverage while asserting the opposite.
			if (tc.want == tc.cpython) != tc.agrees {
				t.Fatalf("row %q is self-contradictory: want %+q, cpython %+q, agrees %v",
					tc.name, tc.want, tc.cpython, tc.agrees)
			}
			got := Lower(tc.in)
			if got != tc.want {
				side := "a row where Lower is DOCUMENTED to diverge from CPython"
				if tc.agrees {
					side = "a row where Lower MUST equal CPython"
				}
				t.Fatalf("Lower(%+q) = %+q, want %+q — CPython gives %+q, and this is %s. "+
					"If this moved on purpose, the DECISION in Lower's docstring (Final_Sigma "+
					"deliberately NOT implemented) is now stale and must be rewritten in the "+
					"same change", tc.in, got, tc.want, tc.cpython, side)
			}
		})
	}

	// 🔴 THE DIVERGENT SET IS CLOSED, NOT MERELY ENUMERATED. The only rule `Lower` is
	// documented to get wrong is Final_Sigma, so a row appended with `agrees: false` for any
	// other reason is a divergence nobody decided on. Asserted here rather than left to the
	// docstring, because widening the ledger is exactly how the "one code point" claim this
	// test exists to correct came to be written in the first place.
	for _, tc := range rows {
		if !tc.agrees && !strings.ContainsRune(tc.in, 0x03a3) {
			t.Fatalf("row %q diverges from CPython without carrying U+03A3, so it is not "+
				"Final_Sigma: %+q -> Lower %+q, cpython %+q. Either it is a bug in Lower or "+
				"the divergence set in Lower's docstring has grown and says so nowhere",
				tc.name, tc.in, tc.want, tc.cpython)
		}
	}
}

func TestDecodeStrictProblem(t *testing.T) {
	// The message is CPython-shaped: the offending byte, its position, and the reason.
	// No golden pins it (no corpus case sends an invalid body), so what is asserted is
	// the three facts a caller can act on — and, above all, that a bad byte is an ERROR
	// rather than a silent U+FFFD. Moved here WITH the function, because its second
	// caller is the token loader, where a lenient decode is a live credential.
	cases := []struct {
		name string
		data []byte
		want string
	}{
		{"clean ASCII", []byte("hello"), ""},
		{"clean multi-byte", []byte("café \U0001F600"), ""},
		{"an invalid start byte", []byte("a\xffb"),
			"'utf-8' codec can't decode byte 0xff in position 1: invalid start byte"},
		{"a truncated sequence at the end", []byte("a\xf0\x9f"),
			"'utf-8' codec can't decode byte 0xf0 in position 1: unexpected end of data"},
		{"a bad continuation byte", []byte("a\xc3zb"),
			"'utf-8' codec can't decode byte 0x7a in position 2: invalid continuation byte"},
		{"a bare continuation byte", []byte("a\x80b"),
			"'utf-8' codec can't decode byte 0x80 in position 1: invalid start byte"},
		{"an overlong encoding", []byte("a\xc0\x80b"),
			"'utf-8' codec can't decode byte 0xc0 in position 1: invalid continuation byte"},
		{"a surrogate encoded as UTF-8", []byte("a\xed\xa0\x80b"),
			"'utf-8' codec can't decode byte 0xed in position 1: invalid continuation byte"},
		{"the empty body", []byte(""), ""},
		// 🔴 THE TOKEN-FILE SHAPE, VERBATIM. 43 bytes of 0xFF is exactly
		// `authz.MinTokenChars` runes when the bytes are REINTERPRETED rather than
		// decoded, which is how such a file loaded as an unrestricted legacy row. This
		// row is what makes the second caller's guard reachable at all.
		{"a 43-byte run of 0xFF — the token-file shape", bytes.Repeat([]byte{0xff}, 43),
			"'utf-8' codec can't decode byte 0xff in position 0: invalid start byte"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := DecodeStrictProblem(tc.data); got != tc.want {
				t.Fatalf("DecodeStrictProblem(%q) = %q, want %q", tc.data, got, tc.want)
			}
		})
	}
}
