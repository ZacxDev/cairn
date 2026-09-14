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
