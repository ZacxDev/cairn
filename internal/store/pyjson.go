package store

import (
	"fmt"
	"strings"
)

// PyJSONString is CPython's `json.encoder.py_encode_basestring_ascii`: `"` and `\` escaped, the
// five named control escapes, `\uXXXX` for every other control character AND for every
// non-ASCII rune, with a surrogate PAIR for anything above the BMP.
//
// 🔴 IT IS SHARED BECAUSE TWO SURFACES SEND OR PRINT JSON AND A SECOND COPY WOULD DRIFT.
// `doctor --json` renders a document whose every `detail` carries an em dash, and `append`
// sends a body whose text may carry an emoji. Both must match what the Python client produces,
// and they must match EACH OTHER's escaping rules, or one of them would be the only surface
// exercising a server-side guard.
//
// 🔴 `ensure_ascii=True` IS CPYTHON'S DEFAULT AND IT IS LOAD-BEARING ON THE WRITE ROUTE. A
// bullet containing an astral character travels as a surrogate PAIR; the server's own guard
// once could not tell a pair from a LONE surrogate and 400'd every astral character. Sending
// raw UTF-8 instead would mean the two clients exercise different halves of that guard, which
// is the one thing a parity gate cannot see (both would be "a 200").
//
// ⚠ IT IS A STRING ENCODER, NOT A DOCUMENT ENCODER. `encoding/json` cannot be used for the
// documents either — it sorts map keys where CPython preserves insertion order, and it escapes
// `<`, `>` and `&` where CPython does not — so each caller writes its own fixed shape around
// this. A general Python-compatible encoder would be a second serialiser to keep true.
func PyJSONString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		case '\b':
			b.WriteString(`\b`)
		case '\f':
			b.WriteString(`\f`)
		default:
			switch {
			case r < 0x20:
				fmt.Fprintf(&b, `\u%04x`, r)
			case r < 0x7f:
				b.WriteRune(r)
			case r <= 0xffff:
				// 🔴 `0x7f` (DEL) IS ESCAPED BY CPython AND IS NOT CAUGHT BY THE `< 0x20`
				// TEST, which is why the raw-ASCII arm above stops BELOW it rather than at
				// 0x80. A port that wrote `r < 0x80` here would differ on exactly one code
				// point, and on nothing a test would think to send.
				fmt.Fprintf(&b, `\u%04x`, r)
			default:
				v := r - 0x10000
				fmt.Fprintf(&b, `\u%04x\u%04x`, 0xd800+(v>>10), 0xdc00+(v&0x3ff))
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}
