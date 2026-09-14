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

// utf8DecodeProblem reports why `data` is not a strict UTF-8 decode, in CPython's
// shape, or "" when it decodes cleanly.
//
// 🔴 THE APPEND PATH DECODED ITS BODY LENIENTLY AND WROTE U+FFFD INTO THE STORE AT
// `200 appended`. `json.Unmarshal` REPLACES an invalid byte rather than refusing it, so
// a body carrying one landed a permanent replacement character in a non-re-derivable
// entry — while `server.py:4391` is `json.loads(body.decode("utf-8"))`, a STRICT decode
// whose failure is a 400. MEASURED against both servers on the same world:
//
//	oracle -> 400 bad request: body must be JSON ('utf-8' codec can't decode byte
//	          0xff in position 37: invalid start byte)
//	Go     -> 200 appended, and `beta-notes/spare-six.md` grew a U+FFFD
//
// That is verbatim the defect class `append_bullet`'s own docstring records as already
// fixed once — "a byte that is not valid UTF-8 became U+FFFD, permanently, at
// 200 appended with no error" — reintroduced through the DECODER instead of through the
// rewrite. The strict decode has to happen before the JSON parse, exactly as the oracle
// orders it, because the JSON parser is the thing that hides the byte.
//
// ⚠ THE SENTENCE IS CPython-SHAPED BUT NOT CPython-IDENTICAL, and no golden pins it:
// the reason clause ("invalid start byte" / "invalid continuation byte" /
// "unexpected end of data") is reproduced, the byte and the position are reproduced,
// and any further divergence in CPython's wording is unmeasured. What IS pinned for
// both implementations is the status, the `X-Store-Status`, and the
// `bad request: body must be JSON (` prefix — see the Go test.
func utf8DecodeProblem(data []byte) string {
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

// describeSurrogate is how the refusal names what it found, so a caller can see the
// escape rather than being told about a category.
func describeSurrogate(escape string) string {
	return fmt.Sprintf(
		"the body carries the unpaired surrogate escape `%s`, which is not text that can be written into a curated entry",
		strings.ToLower(escape))
}
