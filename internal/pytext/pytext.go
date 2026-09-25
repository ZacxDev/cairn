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
	"unicode"
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

// IsSpace is isPySpace under an exported name, so a caller outside this package
// asks the same question rather than spelling a second answer to it.
//
// 🔴 IT IS ALSO `re`'s `\s` FOR A `str` PATTERN, MEASURED AND NOT ASSUMED. On the
// pinned interpreter (3.12) the set of code points matching `re.fullmatch(r"\s", c)`
// and the set for which `c.isspace()` is true are THE SAME 29 code points — checked
// exhaustively over 0 … U+10FFFF, zero in either direction. So a port of a Python
// pattern containing `\s` uses this predicate, NOT Go's `\s`, which is only
// `[\t\n\f\r ]` and would silently stop matching on a NO-BREAK SPACE.
func IsSpace(r rune) bool { return isPySpace(r) }

// IsWordChar is `re`'s `\w` for a `str` pattern, which is what Python's `\b` is
// defined against.
//
// 🔴 GO's `\b` IS ASCII-ONLY AND PYTHON's IS NOT, so a pattern anchored on `\b`
// cannot be transcribed. Measured exhaustively over 0 … U+10FFFF on the pinned
// interpreter: Python's `\w` is exactly `L* | N* | '_'` — 137,936 code points, and
// notably it does NOT include the 2,450 COMBINING MARKS (`Mc`/`Me`/`Mn`) that a
// `IsLetter|IsNumber|IsMark` guess would add. Checked in both directions: nothing
// Python matches is missing here and nothing here is unmatched there.
func IsWordChar(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsNumber(r)
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

// Lower is CPython's `str.lower()` FOR ONE OF THE TWO RULES THAT SEPARATE IT FROM
// `strings.ToLower`, AND NOT THE OTHER. Which one, and why, is the whole of this comment.
//
// 🔴 UNICODE'S FULL LOWERCASE MAPPING DIFFERS FROM THE SIMPLE ONE IN **TWO**
// LANGUAGE-INDEPENDENT RULES — NOT ONE, AND THIS COMMENT SAID ONE FOR A WHOLE ROUND.
// `str.lower()` applies the FULL mapping; `strings.ToLower` applies the SIMPLE one, one
// rune to one rune. The two rules are different KINDS of rule, which is why no
// code-point-at-a-time sweep can see the second:
//
//  1. UNCONDITIONAL, AND IMPLEMENTED HERE — one code point expands into two. Measured
//     differentially over every code point in the range (0 … U+10FFFF, 1,433 of which
//     lower at all) on the pinned interpreter:
//
//     U+0130 LATIN CAPITAL LETTER I WITH DOT ABOVE  ->  Python "i̇"   Go "i"
//     every other code point                        ->  identical
//
//     In both directions: no code point lowers in Go and not in Python.
//
//  2. CONTEXTUAL, AND **NOT** IMPLEMENTED HERE — Final_Sigma. U+03A3 GREEK CAPITAL LETTER
//     SIGMA lowercases to U+03C2 FINAL SIGMA when it ENDS A WORD and to U+03C3 otherwise,
//     so the answer is a function of the NEIGHBOURS rather than of the code point.
//     Measured on both sides (go1.26.7 / CPython 3.12.14):
//
//     the glyphs are one stroke apart, so each row names the code point it ends in:
//     U+03C2 is FINAL SIGMA and U+03C3 is the ordinary one.
//
//     "AΣ"   ->  Python U+03C2  here U+03C3  (DIVERGE)
//     "ΑΣ Β" ->  Python U+03C2  here U+03C3  (DIVERGE)
//     "ΣΣ"   ->  Python U+03C3 U+03C2  here U+03C3 U+03C3  (DIVERGE)
//     "Σ"    ->  Python U+03C3  here U+03C3  (agree — no cased letter precedes it)
//     "AΣB"  ->  Python U+03C3  here U+03C3  (agree — a cased letter follows it)
//
// 🔴 THE DECISION, WRITTEN DOWN SO IT IS A DECISION AND NOT AN OVERSIGHT: FINAL_SIGMA IS
// DELIBERATELY NOT IMPLEMENTED. Two grounds, and the FIRST is the one that expires:
//
//   - NO CALLER CAN OBSERVE IT AT THIS COMMIT — enumerated, not argued from one example.
//     Both `σ` and `ς` are non-ASCII, and every caller either compares this function's
//     output against an ASCII-only set or folds non-ASCII away first, so the two answers
//     take the SAME branch at each of them:
//     · `store.headingKey` pairs a file heading's key only with a SCHEMA heading's key, and
//     every `ShapeHeadings` entry is ASCII — so a Σ-bearing heading pairs with nothing on
//     either side. MEASURED at this commit: an entry whose pointers heading is
//     `## POINTERΣ` renders 11 advisory lines that are BYTE-IDENTICAL on both clients
//     (both ABSENT, and the inventory quotes the heading verbatim).
//     · `store.NormalizeRef` folds everything outside `[a-z0-9.-]` to `-`; σ and ς are
//     both outside it, so both become the same separator.
//     · `report.Tokenize` keeps only `[a-z0-9]` runs; both are separators.
//     · `report.FoldSensitivity` and `report.DiscardedSensitivity` compare against
//     `knownSensitivities`, all ASCII; both answers miss and both fold to the fail-safe.
//     · `store.BulletOpenness` lowercases a `[0-9a-fA-F]{7,40}` submatch, which Σ cannot
//     reach.
//     ⚠ THAT IS A CLAIM ABOUT THE CALLERS, NOT ABOUT THIS FUNCTION, and it is the half
//     that goes stale: a caller added that puts this output on a screen, into a filename,
//     or into a comparison against non-ASCII text makes the divergence live, and nothing
//     here would fail. Re-run the enumeration before adding one.
//   - AND THE FOLD WOULD NEED A UNICODE DATABASE GO DOES NOT SHIP. CPython's condition is
//     `\p{cased}\p{case-ignorable}* Σ !(\p{case-ignorable}*\p{cased})`. `Cased` is
//     derivable from what `unicode` exports (`Lu|Ll|Lt|Other_Lowercase|Other_Uppercase`),
//     but `Case_Ignorable` also needs `Word_Break ∈ {MidLetter, MidNumLet, Single_Quote}`,
//     and Go's `unicode` package exports NO Word_Break table at all — checked against
//     `unicode.Properties` and `unicode.Categories` on go1.26.7. Implementing it therefore
//     means hand-transcribing that set: a second Unicode database whose drift from the
//     pinned interpreter would be silent, which is the hazard this package's header exists
//     for. It is not that it cannot be done; it is that the cost is a table, and today it
//     buys no output.
//
// 🔴 SO THE DIVERGENCE SET IS A LEDGER, NOT A SENTENCE.
// `TestLowerIsCPythonExceptForFinalSigma` holds every row above with CPython's answer
// BESIDE this function's, and fails when one MOVES IN EITHER DIRECTION — a lost U+0130
// expansion, and equally the day somebody teaches this function Final_Sigma without
// revisiting the decision above. `TestLowerMatchesCPython` pins rule 1 and asserts NOTHING
// about rule 2 in either direction — which is a property of how that test is BUILT, not a
// structural impossibility, and an earlier version of this sentence claimed the latter
// ("STRUCTURALLY UNABLE to see rule 2: it compares one code point at a time"). Its
// single-code-point sweep genuinely cannot: an isolated `Σ` lowercases to U+03C3 on both
// sides. Its second sweep walks `<r>İ<r>`, which DOES reach rule 2 — at `r = U+03A3` and
// nowhere else — so that one code point is skipped there on purpose, leaving the ledger
// above the sole router for a Final_Sigma change. That test's positive control is that the
// U+0130 row FAILS when this function delegates straight to `strings.ToLower`.
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
	// documents that the special case handled HERE is a single code point rather than a
	// general algorithm. ⚠ It is NOT the whole divergence from `str.lower()` — rule 2 in
	// the docstring above is contextual and has no branch anywhere in this function, which
	// is exactly why a reader could take this one branch for the complete story.
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

// The one full-lowercase EXPANSION in the Unicode tables the pinned interpreter ships, as
// NUMERIC RUNE CONSTANTS for the reason the package comment gives: U+0307 is invisible
// beside its neighbour in source, so a literal here would be unreviewable.
//
// ⚠ "THE ONE EXPANSION" IS NOT "THE ONE DIVERGENCE". Final_Sigma is a SUBSTITUTION of one
// code point for another, not an expansion, so it is absent from this pair by definition
// rather than by omission. See rule 2 in `Lower`'s docstring.
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
