package write

import (
	"fmt"
	"strings"
)

// unpairedSurrogateEscape reports the first `\uXXXX` escape in `raw` that names a
// surrogate WITHOUT its partner, or "" when there is none.
//
// 🔴 A WELL-FORMED SURROGATE **PAIR** IS ORDINARY TEXT, AND THE FIRST VERSION OF THIS
// GUARD REFUSED EVERY ONE OF THEM. It was a single regexp whose two alternatives were
// `D800-DBFF` and `DC00-DFFF` — whose union is the ENTIRE surrogate block — so it could
// not tell a lone surrogate from a pair, and it refused on any match.
//
// 🔴 THAT BROKE THE SHIPPED CLIENT FOR EVERY NON-BMP CHARACTER, which is what makes
// this the expensive kind of mistake rather than a cosmetic one. `cairn append` builds
// its body with `json.dumps`, whose default `ensure_ascii=True` encodes an astral
// character as a surrogate PAIR — so `--text "… 😀 …"` is `😀` on the wire.
// MEASURED against both servers on the same world:
//
//	oracle -> 200, `- <date>: an emoji 😀 bullet [cairn: wide-reader/probe1]`
//	Go     -> 400, "the body carries the unpaired surrogate escape `\ud83d`"
//
// The corpus could not see it (no case sends an escaped astral character), and the
// guard's own comment asserted the divergence was "slightly WIDER than Python" — an
// unchecked claim that is what stopped anyone measuring it.
//
// ⚠ THE GUARD ITSELF IS STILL NEEDED, AND THIS IS WHY IT IS NARROWED RATHER THAN
// DELETED. CPython's decoder yields a LONE SURROGATE for `\udc80`, which the `Cs`
// category clause then refuses; Go's replaces it with U+FFFD, whose category is `So`,
// so without this scan the bullet would be STORED carrying a replacement character the
// oracle refuses. The remaining divergence is the narrow one the comment originally
// claimed: an unpaired escape in a key this server ignores is a 400 here and a 200
// there.
//
// The rule is ADJACENCY, checked over the escape tokens in order: a high surrogate is
// paired iff the very next token starts where it ends and is a low surrogate; a low
// surrogate is paired iff the previous token ended where it starts and was a high one.
// Anything else is unpaired. A lookahead would express it in one pattern, and RE2 has
// none — so it is a scan, which is also the form that can name WHICH escape it found.
func unpairedSurrogateEscape(raw string) string {
	type token struct {
		start, end int
		value      rune
	}
	// 🔴 THE WALK CONSUMES WHAT EACH BACKSLASH ESCAPES, WHICH IS WHAT GETS BACKSLASH
	// PARITY RIGHT. A scan that merely looked for `\u` and skipped six bytes on a hit
	// reads `\\ud83d` — an ESCAPED BACKSLASH followed by the literal text `ud83d` — as a
	// surrogate escape, because it restarts inside the pair. Caught by the table in
	// `surrogate_test.go`, which is the row that exists for it. Consuming both bytes of
	// every escape makes the parity implicit instead of a second thing to track.
	var tokens []token
	for i := 0; i < len(raw); {
		if raw[i] != '\\' {
			i++
			continue
		}
		if i+1 >= len(raw) {
			break
		}
		if raw[i+1] == 'u' || raw[i+1] == 'U' {
			if i+6 <= len(raw) {
				if value, ok := hex4(raw[i+2 : i+6]); ok {
					tokens = append(tokens, token{start: i, end: i + 6, value: value})
					i += 6
					continue
				}
			}
		}
		// Any other escape — `\\`, `\"`, `\n` — is two bytes, and consuming both is
		// what stops the second byte being read as an introducer of its own.
		i += 2
	}
	for index, tok := range tokens {
		switch {
		case tok.value >= 0xd800 && tok.value <= 0xdbff:
			if index+1 < len(tokens) {
				next := tokens[index+1]
				if next.start == tok.end && next.value >= 0xdc00 && next.value <= 0xdfff {
					continue
				}
			}
		case tok.value >= 0xdc00 && tok.value <= 0xdfff:
			if index > 0 {
				prev := tokens[index-1]
				if prev.end == tok.start && prev.value >= 0xd800 && prev.value <= 0xdbff {
					continue
				}
			}
		default:
			continue
		}
		return raw[tok.start:tok.end]
	}
	return ""
}

func hex4(s string) (rune, bool) {
	if len(s) != 4 {
		return 0, false
	}
	var value rune
	for i := 0; i < 4; i++ {
		nibble, ok := hexNibbleValue(s[i])
		if !ok {
			return 0, false
		}
		value = value<<4 | rune(nibble)
	}
	return value, true
}

func hexNibbleValue(c byte) (byte, bool) {
	switch {
	case c >= '0' && c <= '9':
		return c - '0', true
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10, true
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10, true
	}
	return 0, false
}

// 🔴 THE STRICT UTF-8 DECODE MOVED TO `pytext.DecodeStrictProblem`, AND THE MOVE IS
// THE FIX FOR A SECOND DEFECT RATHER THAN TIDYING. It lived here because the append
// path was its only caller; the TOKEN LOADER needs exactly the same predicate, and had
// open-coded `string(data)` instead — which turned 43 bytes of `0xFF` into an
// unrestricted-scope legacy credential the oracle refuses to load at all. Two copies of
// "is this byte run text" is the shape where one gets fixed and the other serves the
// store. See `pytext.DecodeStrictProblem` for both measurements.

// describeSurrogate is how the refusal names what it found, so a caller can see the
// escape rather than being told about a category.
func describeSurrogate(escape string) string {
	return fmt.Sprintf(
		"the body carries the unpaired surrogate escape `%s`, which is not text that can be written into a curated entry",
		strings.ToLower(escape))
}
