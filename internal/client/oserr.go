package client

import (
	"strings"
	"unicode/utf8"

	"github.com/ZacxDev/cairn/internal/store"
)

// pyOSError is `store.PyOSError`, reached through one name so this package's call sites read
// like the oracle's. 🔴 IT IS NOT A SECOND IMPLEMENTATION — see `store.PyOSError` for the
// sixteen-errno measurement behind it, and for why it lives there rather than here (two
// packages interpolate it and the parity gate compares those sentences byte for byte).
func pyOSError(err error) string { return store.PyOSError(err) }

// oneLine collapses a server's body into the single-line reason a refusal carries. The
// oracle's spelling exactly: strip, then replace every newline with `; `, then truncate.
func oneLine(body []byte, limit int) string {
	return truncateRunes(strings.ReplaceAll(
		strings.TrimSpace(store.DecodeReplace(body)), "\n", "; "), limit)
}

// truncateRunes cuts at `limit` CODE POINTS, which is what CPython's `s[:limit]` does — a byte
// cut would land mid-rune on a non-ASCII body and the two clients would disagree on exactly the
// input a hostile server is most likely to send.
func truncateRunes(s string, limit int) string {
	if utf8.RuneCountInString(s) <= limit {
		return s
	}
	count := 0
	for i := range s {
		if count == limit {
			return s[:i]
		}
		count++
	}
	return s
}
