package api

import (
	"net/url"
	"strings"

	"github.com/ZacxDev/cairn/internal/pytext"
)

// parseQuery is CPython's `parse_qs(qs, keep_blank_values=True)`.
//
// 🔴 IT EXISTS BECAUSE `url.Values` SILENTLY DROPS A PAIR WITH A BAD ESCAPE, WHICH IS A
// SILENT DEFAULT — the one outcome every query parameter on this API is forbidden to
// produce. MEASURED, on Go 1.25 and CPython 3.12, on the same four inputs:
//
//	"limit=%zz"  ParseQuery -> url.Values{}                      err="invalid URL escape"
//	             parse_qs   -> {'limit': ['%zz']}
//	"limit=%2"   ParseQuery -> url.Values{}                      err="invalid URL escape"
//	             parse_qs   -> {'limit': ['%2']}
//	"limit="     ParseQuery -> {"limit": [""]}   parse_qs -> {'limit': ['']}   (agree)
//	"foo"        ParseQuery -> {"foo":   [""]}   parse_qs -> {'foo':   ['']}   (agree)
//
// `r.URL.Query()` discards the parse error, so `?limit=%zz` arrives at the handler as
// NO `limit` AT ALL and the default is used — while the oracle answers
// `400 bad request: limit must be an integer, got '%zz'`. A caller who typo'd an escape
// would have been told their setting took effect. `tests/conformance/` cannot see it:
// the runner builds its targets from the declared list and never sends a malformed
// escape.
//
// The rule, from CPython's `parse_qsl`: split on `&`, skip an empty field, split each
// field on the FIRST `=`, and treat a field with no `=` as an empty value (that is what
// `keep_blank_values` buys). Both halves are then unquoted.
//
// ⚠ ONE RESIDUAL, NAMED RATHER THAN ASSUMED AWAY: a RAW non-ASCII byte in a query
// string. `http.server` decodes the request line as latin-1 before parsing, and
// `unquote` then percent-decodes only the ASCII runs — so such a byte survives as
// mojibake there, while here it stays a byte and decodes as whatever UTF-8 it is. No
// conforming client sends one (a query string is percent-encoded on the wire), and no
// corpus case does; the difference is recorded because "nobody looked" and "measured
// equivalent" are not the same statement.
func parseQuery(raw string) url.Values {
	out := url.Values{}
	if raw == "" {
		return out
	}
	for _, field := range strings.Split(raw, "&") {
		if field == "" {
			continue
		}
		name, value, _ := strings.Cut(field, "=")
		key := pyUnquotePlus(name)
		out[key] = append(out[key], pyUnquotePlus(value))
	}
	return out
}

// pyUnquotePlus is `urllib.parse.unquote_plus(s, errors="replace")`.
func pyUnquotePlus(s string) string {
	return pyUnquote(strings.ReplaceAll(s, "+", " "))
}

// pyUnquote is `urllib.parse.unquote(s, errors="replace")`.
//
// 🔴 AN INVALID ESCAPE IS LEFT LITERAL, NOT AN ERROR AND NOT DROPPED. That is the whole
// point of this function: the caller's own `%zz` reaches the parameter validator, which
// refuses it and QUOTES IT BACK, so the refusal names what the caller actually sent.
//
// The byte-level decode comes first and the UTF-8 decode second, in that order, because
// `%C3%A9` is ONE character and two independent decodes would make it two — the same
// reason CPython's `unquote_to_bytes` exists as its own function.
func pyUnquote(s string) string {
	if !strings.Contains(s, "%") {
		return s
	}
	chunks := strings.Split(s, "%")
	decoded := make([]byte, 0, len(s))
	decoded = append(decoded, chunks[0]...)
	for _, chunk := range chunks[1:] {
		if len(chunk) >= 2 {
			if b, ok := hexByte(chunk[0], chunk[1]); ok {
				decoded = append(decoded, b)
				decoded = append(decoded, chunk[2:]...)
				continue
			}
		}
		decoded = append(decoded, '%')
		decoded = append(decoded, chunk...)
	}
	return pytext.DecodeUTF8Replace(decoded)
}

func hexByte(high, low byte) (byte, bool) {
	h, okHigh := hexNibble(high)
	l, okLow := hexNibble(low)
	if !okHigh || !okLow {
		return 0, false
	}
	return h<<4 | l, true
}

func hexNibble(c byte) (byte, bool) {
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
