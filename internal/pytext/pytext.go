// Package pytext holds the CPython string predicates the ported reader and
// writer depend on for BYTE-IDENTICAL behaviour, spelled once.
//
// 🔴 THESE ARE NOT CONVENIENCE HELPERS — EACH IS A CONTRACT THE GOLDENS PIN.
// `server/server.py` and the reader beside it make load-bearing decisions from
// `str.splitlines()` and `str.split()`:
//
//   - `_bullet_request_problem` refuses a bullet whose text becomes more than
//     one line, and it asks `str.splitlines()` itself precisely BECAUSE that
//     function splits on TEN characters rather than two. A Go port that split on
//     `\n` alone would accept a U+2028 and store one line that renders as two —
//     the measured defect that comment exists for. See LineBreaks.
//   - `content_hash` collapses whitespace with `" ".join(text.split())`, and the
//     idempotency of every append is a hash over that collapse. `str.split()`
//     treats U+001C..U+001F as whitespace and Go's `unicode.IsSpace` does not,
//     so `strings.Fields` would hash a different string for a bullet carrying
//     one. See isPySpace.
//
// 🔴 AND THERE IS NO `surrogateescape` HERE, DELIBERATELY. Python needs that
// codec because it must decode an entry file to `str` to index into it, and only
// `surrogateescape` round-trips a byte that is not valid UTF-8 — a mismatched
// decode/encode pair once made one legacy byte answer `500` forever on every
// append to that entry. Go strings ARE byte sequences, so the round trip is the
// identity and that whole hazard cannot arise. The functions below therefore
// index bytes and only DECODE far enough to recognise a multi-byte separator; an
// invalid byte decodes to `utf8.RuneError` with width 1, which is not a
// separator — exactly as Python's lone surrogate is not one either.
//
// 🔴 EVERY NON-ASCII CODE POINT BELOW IS A NUMERIC RUNE CONSTANT, NOT A LITERAL
// CHARACTER AND NOT A `\x` ESCAPE. Two reasons, and the first is a real trap: a
// Go `"\x85"` is the raw BYTE 0x85 (invalid UTF-8) while a Python `"\x85"` in a
// `str` is the CODE POINT U+0085, so transcribing `LINE_BREAK_CHARS` by eye
// produces a separator this package cannot recognise — silently, on the one
// input the guard exists for. The second is legibility: an invisible separator
// pasted into source is indistinguishable from its neighbour on screen, and this
// repository has already had a raw private-use character render identically to
// the character beside it.
package pytext

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// The separators, by code point. `fileSep`..`unitSep` are the C0 information
// separators, `nel` is the C1 NEXT LINE, and the last two are Unicode's own.
const (
	fileSep   = rune(0x1c)
	groupSep  = rune(0x1d)
	recordSep = rune(0x1e)
	unitSep   = rune(0x1f)
	nel       = rune(0x85)
	lineSep   = rune(0x2028)
	paraSep   = rune(0x2029)
)

// LineBreaks is every character `str.splitlines()` treats as a line boundary:
// seven C0 controls, the C1 NEL, and the two Unicode separators. It is the Go
// spelling of `server.py`'s `LINE_BREAK_CHARS`, and `TestLineBreaksIsTen` pins
// the count so a dropped entry cannot pass for the whole set.
//
// 🔴 TEN, NOT TWO. The Python validator this mirrors used to read
// `if "\n" in text or "\r" in text`, which is a membership test on two
// characters standing in for a predicate about ten.
var LineBreaks = string([]rune{
	'\n', '\r', '\v', '\f', fileSep, groupSep, recordSep, nel, lineSep, paraSep,
})

func isLineBreak(r rune) bool {
	switch r {
	case '\n', '\r', '\v', '\f', fileSep, groupSep, recordSep, nel, lineSep, paraSep:
		return true
	}
	return false
}

// SplitLinesKeepEnds is `str.splitlines(keepends=True)`.
//
// A `\r\n` pair is ONE break, and a trailing break does not produce a final
// empty element — both are CPython's rules, and both are load-bearing: the
// append path derives its splice offset from the joined length of the lines
// before the insertion point and its terminator from the difference between a
// kept-ends line and the same line without its break. A disagreement here is an
// off-by-one inside a file the caller cannot see.
func SplitLinesKeepEnds(s string) []string {
	var out []string
	start, i := 0, 0
	for i < len(s) {
		r, w := utf8.DecodeRuneInString(s[i:])
		if !isLineBreak(r) {
			i += w
			continue
		}
		end := i + w
		if r == '\r' && end < len(s) && s[end] == '\n' {
			end++
		}
		out = append(out, s[start:end])
		i = end
		start = end
	}
	if start < len(s) {
		out = append(out, s[start:])
	}
	return out
}

// SplitLines is `str.splitlines()` — the same split with the breaks removed.
func SplitLines(s string) []string {
	kept := SplitLinesKeepEnds(s)
	out := make([]string, len(kept))
	for i, line := range kept {
		out[i] = StripOneLineBreak(line)
	}
	return out
}

// StripOneLineBreak removes the single trailing line break SplitLinesKeepEnds
// would have kept, and nothing else. It is `line.splitlines()[0]` — how
// `append_bullet` derives a heading's terminator VERBATIM rather than guessing a
// newline or stripping a hand-written character class, which would also eat
// trailing spaces the heading line is entitled to keep.
func StripOneLineBreak(line string) string {
	if line == "" {
		return ""
	}
	if strings.HasSuffix(line, "\r\n") {
		return line[:len(line)-2]
	}
	r, w := utf8.DecodeLastRuneInString(line)
	if isLineBreak(r) {
		return line[:len(line)-w]
	}
	return line
}

// TrimLineBreaks is `str.strip(LINE_BREAK_CHARS)` — used to catch a LEADING or
// TRAILING break, which `splitlines` folds away (`"a\n".splitlines()` is one
// element) and which would otherwise open or close a stored bullet with an empty
// line.
func TrimLineBreaks(s string) string {
	return strings.Trim(s, LineBreaks)
}

// isPySpace is CPython's `Py_UNICODE_ISSPACE`, which `str.split()` and
// `str.strip()` with no separator use.
//
// 🔴 IT IS A SUPERSET OF Go's `unicode.IsSpace`: CPython counts the four C0
// information separators U+001C..U+001F as whitespace and Unicode's White_Space
// property does not. `strings.Fields` therefore collapses a DIFFERENT string,
// and `content_hash` is a hash over the collapse — so a stored bullet carrying
// one of those bytes would hash differently here and land as a NEW bullet
// instead of being recognised as the duplicate it is. Reachable only from bytes
// already on disk (a request body carrying any of the four is refused twice
// over, by the line-break clause and by Unicode category `Cc`), which is
// precisely why it would have been invisible.
func isPySpace(r rune) bool {
	switch r {
	case '\t', '\n', '\v', '\f', '\r', ' ',
		fileSep, groupSep, recordSep, unitSep,
		nel, lineSep, paraSep,
		0xa0,   // NO-BREAK SPACE
		0x1680, // OGHAM SPACE MARK
		0x202f, // NARROW NO-BREAK SPACE
		0x205f, // MEDIUM MATHEMATICAL SPACE
		0x3000: // IDEOGRAPHIC SPACE
		return true
	}
	// U+2000..U+200A — EN QUAD through HAIR SPACE.
	return r >= 0x2000 && r <= 0x200a
}

// SplitWhitespace is `str.split()` with no argument: split on runs of
// whitespace, leading and trailing runs discarded, never an empty element.
func SplitWhitespace(s string) []string {
	var out []string
	start := -1
	i := 0
	for i < len(s) {
		r, w := utf8.DecodeRuneInString(s[i:])
		if isPySpace(r) {
			if start >= 0 {
				out = append(out, s[start:i])
				start = -1
			}
		} else if start < 0 {
			start = i
		}
		i += w
	}
	if start >= 0 {
		out = append(out, s[start:])
	}
	return out
}

// CollapseWhitespace is `" ".join(text.split())`.
func CollapseWhitespace(s string) string {
	return strings.Join(SplitWhitespace(s), " ")
}

// StripWhitespace is `str.strip()` with no argument — the same predicate as
// SplitWhitespace, so "is this text blank" has one answer here too.
func StripWhitespace(s string) string {
	return strings.TrimFunc(s, isPySpace)
}

// ContainsSpace is `any(c.isspace() for c in s)`, on the same predicate as the
// two functions above so a ref rejected for "contains whitespace" and a text
// collapsed for hashing cannot disagree about what whitespace is.
func ContainsSpace(s string) bool {
	return strings.IndexFunc(s, isPySpace) >= 0
}

// DecodeUTF8Replace is Python's `bytes.decode("utf-8", errors="replace")`.
//
// 🔴 ONE U+FFFD PER INVALID **BYTE**, NOT PER INVALID RUN, and that is why
// `strings.ToValidUTF8` is not used. CPython's `replace` handler emits one replacement
// character for every byte it could not decode, so a two-byte invalid sequence becomes
// TWO U+FFFD there and ONE with Go's helper — a length difference, in text that offsets
// are computed against and that a refusal quotes back.
//
// Two callers need it and they need the same answer: the index loader reads an entry
// file this way, and the query parser decodes a percent-escaped byte run this way. A
// second copy is the duplicated predicate that diverges the day one of them is fixed.
func DecodeUTF8Replace(data []byte) string {
	if utf8.Valid(data) {
		return string(data)
	}
	out := make([]rune, 0, len(data))
	for i := 0; i < len(data); {
		r, w := utf8.DecodeRune(data[i:])
		if r == utf8.RuneError && w <= 1 {
			out = append(out, utf8.RuneError)
			i++
			continue
		}
		out = append(out, r)
		i += w
	}
	return string(out)
}

// Lower is CPython's `str.lower()`.
//
// 🔴 `strings.ToLower` IS NOT IT, AND THE DIFFERENCE IS EXACTLY ONE CODE POINT — WHICH IS
// WHY IT SURVIVED A REVIEW THAT SAID SO. `str.lower()` applies Unicode's FULL lowercase
// mapping, which may expand one code point into SEVERAL; `strings.ToLower` applies the
// SIMPLE mapping, one rune to one rune. Measured differentially over every code point in
// the range (0 … U+10FFFF, 1,433 of which lower at all) on the pinned interpreter:
//
//	U+0130 LATIN CAPITAL LETTER I WITH DOT ABOVE  ->  Python "i̇"   Go "i"
//	every other code point                        ->  identical
//
// One divergence, in both directions (no code point lowers in Go and not in Python).
// `TestLowerMatchesCPython` pins the pair, and the sweep's own positive control is that
// the U+0130 row FAILS when this function delegates straight to `strings.ToLower`.
//
// ⚠ THE COMBINING MARK IS THE WHOLE HAZARD, AND A PREVIOUS COMMENT DISMISSED IT ON A
// MEASUREMENT THAT ONLY LOOKED AT THE EASY CASE. `store.NormalizeRef` folds everything
// outside `[a-z0-9.-]` to `-`, and U+0307 is outside it — so the expansion becomes a
// SEPARATOR. At the end of a string that separator is trimmed and the two agree, which is
// what the old note measured; ANYWHERE ELSE it does not:
//
//	İ    -> oracle "i"     Go "i"     (agree — the trailing dash is trimmed)
//	xİ   -> oracle "xi"    Go "xi"    (agree, same reason)
//	İa   -> oracle "i-a"   Go "ia"    (DIVERGE)
//	aİb  -> oracle "ai-b"  Go "aib"   (DIVERGE)
//	İİ   -> oracle "i-i"   Go "ii"    (DIVERGE)
//
// A ref that folds to a different string resolves to a different entry, or to none — which
// is a create/alias collision, and it is reachable from a request BODY (an entry's
// `service:`, `scope:` and `aliases:` front matter all go through the fold) even though the
// URL path cannot carry it.
func Lower(s string) string {
	// The fast path is the common one and is not an optimisation for its own sake: it also
	// documents that the special case is a single code point rather than a general
	// algorithm, so a reader can see the whole divergence in one branch.
	if !strings.ContainsRune(s, dottedCapitalI) {
		return strings.ToLower(s)
	}
	var b strings.Builder
	b.Grow(len(s) + 2)
	for _, r := range s {
		if r == dottedCapitalI {
			b.WriteRune('i')
			b.WriteRune(combiningDotAbove)
			continue
		}
		b.WriteString(strings.ToLower(string(r)))
	}
	return b.String()
}

// The one full-lowercase expansion in the Unicode tables the pinned interpreter ships, as
// NUMERIC RUNE CONSTANTS for the reason the package comment gives: U+0307 is invisible
// beside its neighbour in source, so a literal here would be unreviewable.
const (
	dottedCapitalI    = rune(0x0130)
	combiningDotAbove = rune(0x0307)
)

// DecodeStrictProblem reports why `data` is not a STRICT UTF-8 decode, in CPython's
// `UnicodeDecodeError` shape, or "" when it decodes cleanly. It is
// `bytes.decode("utf-8")` with no error handler — the sibling of
// DecodeUTF8Replace above, and the reason the two live together: Go's
// `string(data)` is neither of them. It is a reinterpret-cast that cannot fail, so
// every place the oracle relies on a strict decode to REFUSE something has to ask
// this function instead of trusting a conversion that always succeeds.
//
// 🔴 TWO CALLERS, AND THE SECOND ONE IS WHY THIS MOVED HERE. Both are places where
// the oracle's strictness is the whole guard and Go's leniency is the whole defect:
//
//   - the append path (`write.DecodeBulletBody`). `json.Unmarshal` REPLACES an
//     invalid byte rather than refusing it, so a body carrying one landed a
//     permanent U+FFFD in a non-re-derivable entry at `200 appended`, while
//     `server.py`'s `json.loads(body.decode("utf-8"))` is a strict decode whose
//     failure is a 400. MEASURED against both servers on the same world: oracle
//     `400 bad request: body must be JSON ('utf-8' codec can't decode byte 0xff in
//     position 37: invalid start byte)`, Go `200 appended` plus a U+FFFD in the file.
//   - the token loader (`authz.LoadTokens`). `read_text(encoding="utf-8")` raises
//     there, so the oracle will not START on a token file that is not text; Go's
//     `string(data)` turned 43 bytes of `0xFF` into a 43-rune BARE LEGACY ROW —
//     an UNRESTRICTED-scope credential — and served `200` plus the whole store to a
//     caller presenting those bytes. MEASURED on both: oracle exit 78 with the codec
//     message, Go serving.
//
// A copy per caller is the duplicated predicate that diverges the day one is fixed,
// and these two are exactly the pair where a divergence is a credential.
//
// ⚠ THE SENTENCE IS CPython-SHAPED BUT NOT CPython-IDENTICAL, and that limit is
// deliberate rather than unmeasured: the reason clause (`invalid start byte` /
// `invalid continuation byte` / `unexpected end of data`), the offending byte and its
// position are reproduced; any further divergence in CPython's wording is unmeasured
// and no golden pins it. What IS pinned for both implementations is the status, the
// `X-Store-Status` and the `bad request: body must be JSON (` prefix on the append
// path, and a non-zero exit with a named refusal on the token path.
func DecodeStrictProblem(data []byte) string {
	for i := 0; i < len(data); {
		c := data[i]
		if c < 0x80 {
			i++
			continue
		}
		size, reason := utf8SequenceLength(c)
		if size == 0 {
			return codecMessage(c, i, reason)
		}
		if i+size > len(data) {
			return codecMessage(c, i, "unexpected end of data")
		}
		for offset := 1; offset < size; offset++ {
			if data[i+offset]&0xc0 != 0x80 {
				return codecMessage(data[i+offset], i+offset, "invalid continuation byte")
			}
		}
		// Overlong forms, surrogates and anything past U+10FFFF are refused the way a
		// strict decoder refuses them: the first byte is the one named.
		value := decodeSequence(data[i:i+size], size)
		if value < minForLength(size) || (value >= 0xd800 && value <= 0xdfff) || value > 0x10ffff {
			return codecMessage(c, i, "invalid continuation byte")
		}
		i += size
	}
	return ""
}

func codecMessage(b byte, position int, reason string) string {
	return fmt.Sprintf("'utf-8' codec can't decode byte 0x%02x in position %d: %s",
		b, position, reason)
}

func utf8SequenceLength(c byte) (int, string) {
	switch {
	case c&0xe0 == 0xc0:
		return 2, ""
	case c&0xf0 == 0xe0:
		return 3, ""
	case c&0xf8 == 0xf0:
		return 4, ""
	}
	return 0, "invalid start byte"
}

func decodeSequence(data []byte, size int) rune {
	masks := map[int]byte{2: 0x1f, 3: 0x0f, 4: 0x07}
	value := rune(data[0] & masks[size])
	for offset := 1; offset < size; offset++ {
		value = value<<6 | rune(data[offset]&0x3f)
	}
	return value
}

func minForLength(size int) rune {
	switch size {
	case 2:
		return 0x80
	case 3:
		return 0x800
	}
	return 0x10000
}
