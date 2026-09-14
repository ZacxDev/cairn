package store

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// PyRepr is CPython's `repr()` for a `str`, and it exists because the refusal
// SENTENCES this package produces are part of the HTTP contract.
//
// 🔴 A 422 BODY QUOTES THE LOADER'S OWN MESSAGE, VERBATIM, AND THE GOLDENS PIN
// IT. `PUT` answers `unprocessable: the index loader would reject these bytes:
// malformed index entry 'spare-six.md': …` — that `'spare-six.md'` is a Python
// `{source!r}`, single-quoted. Using Go's `%q` would emit `"spare-six.md"` and
// fail the golden on a character nobody would think to look at.
//
// The rules, from CPython's `unicode_repr`: single quotes UNLESS the string
// contains a `'` and no `"`; escape the quote in use and every backslash;
// `\t`/`\n`/`\r` by name; everything else non-printable as `\xNN`, `\uXXXX` or
// `\UXXXXXXXX` by width. "Printable" is Python's `str.isprintable()` — every
// character whose Unicode category is not one of Cc/Cf/Cs/Co/Cn/Zl/Zp/Zs, plus
// the ASCII space itself.
//
// ⚠ WHAT THIS DOES NOT REPRODUCE: Python's repr is `str`-oriented and this is
// byte-oriented, so a byte sequence that is not valid UTF-8 renders here as the
// replacement-decoded run rather than as Python's lone-surrogate `\udcXX`. That
// divergence is reachable only from an entry FILENAME that is not valid UTF-8,
// which `SAFE_PATH_COMPONENT` cannot name in a URL and which the read path
// answers 503 for on the Python side as well. Stated rather than left to be
// found.
func PyRepr(s string) string {
	quote := byte('\'')
	if strings.Contains(s, "'") && !strings.Contains(s, "\"") {
		quote = '"'
	}
	var b strings.Builder
	b.WriteByte(quote)
	for _, r := range s {
		switch {
		case r == rune(quote) || r == '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		case r == '\t':
			b.WriteString(`\t`)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\r':
			b.WriteString(`\r`)
		case r == ' ':
			b.WriteByte(' ')
		case isPyPrintable(r):
			b.WriteRune(r)
		case r < 0x100:
			fmt.Fprintf(&b, `\x%02x`, r)
		case r < 0x10000:
			fmt.Fprintf(&b, `\u%04x`, r)
		default:
			fmt.Fprintf(&b, `\U%08x`, r)
		}
	}
	b.WriteByte(quote)
	return b.String()
}

// isPyPrintable is `str.isprintable()` for one character: not a separator and
// not "other" (control, format, surrogate, private use, unassigned). The ASCII
// space is handled by PyRepr's own arm above, because Python treats it as
// printable while its category `Zs` is on this list.
func isPyPrintable(r rune) bool {
	if r == utf8.RuneError {
		// A byte that is not valid UTF-8 decoded to the replacement character.
		// U+FFFD is itself printable, and emitting it is the documented
		// divergence in PyRepr's own note.
		return true
	}
	switch {
	case unicode.Is(unicode.Cc, r), unicode.Is(unicode.Cf, r),
		unicode.Is(unicode.Cs, r), unicode.Is(unicode.Co, r),
		unicode.Is(unicode.Zl, r), unicode.Is(unicode.Zp, r),
		unicode.Is(unicode.Zs, r):
		return false
	case !unicode.IsGraphic(r):
		// Covers Cn (unassigned), which has no RangeTable of its own.
		return false
	}
	return true
}
