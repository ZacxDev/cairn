package redact

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

// signature is one known binary file format's leading bytes. `at` is the offset they sit at (WebP
// puts `WEBP` at 8, behind a `RIFF` header that is not WebP-specific on its own).
type signature struct {
	name  string
	at    int
	magic []byte
}

// Signatures is the CLOSED list of formats this package calls BINARY. It is the coordinator's
// reading of operator decision O12 (`claudedocs/plan-cairn-plugins.md`, revision 18): O12 ships
// images, PDFs and binary PAYLOADS unredacted, and a payload is recognised by what it STARTS with,
// never by a name and never by "contains a NUL".
var Signatures = []signature{
	{"png", 0, []byte("\x89PNG\r\n\x1a\n")},
	{"jpeg", 0, []byte{0xff, 0xd8, 0xff}},
	{"gif", 0, []byte("GIF87a")},
	{"gif", 0, []byte("GIF89a")},
	{"webp", 8, []byte("WEBP")},
	{"pdf", 0, []byte("%PDF-")},
	{"zip", 0, []byte("PK\x03\x04")},
	{"gzip", 0, []byte{0x1f, 0x8b}},
	{"bzip2", 0, []byte("BZh")},
	{"xz", 0, []byte{0xfd, '7', 'z', 'X', 'Z', 0x00}},
	{"zstd", 0, []byte{0x28, 0xb5, 0x2f, 0xfd}},
	{"7z", 0, []byte{'7', 'z', 0xbc, 0xaf, 0x27, 0x1c}},
	{"elf", 0, []byte("\x7fELF")},
}

// IsBinary is the binary rule: the bytes BEGIN WITH a known binary file signature.
//
// 🔴 EVERYTHING ELSE IS TEXT AND IS SCANNED, and the cases that rule used to get wrong are named
// here because each was a way to ship a secret unread. Revision 17's rule ("no NUL in the first
// 8,000 bytes AND valid UTF-8") called a `/proc/<pid>/environ` dump, an `env -0` listing and a
// `find -print0` result binary — NUL-separated text that is ALL secrets in the environ case — and
// a mostly-UTF-8 log with one stray byte binary too. Now NUL-separated text is split on NUL and
// each segment scanned, and invalid bytes are carried through untouched while the rest is scanned.
//
// ⚠ RESIDUAL, AND THE ONE O12 ACCEPTED: a payload that does start with a signature ships as it
// is, so a PNG carrying a token in a text chunk ships the token (T1).
func IsBinary(b []byte) bool {
	for _, s := range Signatures {
		if len(b) >= s.at+len(s.magic) && bytes.Equal(b[s.at:s.at+len(s.magic)], s.magic) {
			if s.name == "webp" && !bytes.HasPrefix(b, []byte("RIFF")) {
				continue
			}
			return true
		}
	}
	return false
}

// MinBase64 is the floor for scanning a whole base64 string decoded: 16 characters, 12 bytes.
//
// 🔴 THE FLOOR BUYS NO PRECISION, SO IT IS LOW. The decoded scan redacts only on a rule match, so
// a lower floor costs a decode attempt and nothing else; a higher one lets an encoded secret under
// it ship. Measured in the plan: a 40-character token encodes to 56 characters, which a floor of
// 64 (revision 4's) missed.
const MinBase64 = 16

// decodeWholeBase64 decodes s as ONE base64 value, or reports false.
//
// The whole string, after removing ASCII whitespace (so a 76-column line-wrapped encoding is one
// value), must be base64-alphabet characters; the four encodings are tried in a fixed order —
// padded standard, unpadded standard, padded URL-safe, unpadded URL-safe.
//
// ⚠ AN ENCODED SECRET EMBEDDED IN A LONGER STRING IS NOT DECODED, and that is a declared residual
// (decision 6), pinned as one by the tests so that adding an in-string decoder is a deliberate
// change rather than an accident.
func decodeWholeBase64(s string) ([]byte, bool) { return decodeBase64Floor(s, MinBase64) }

func decodeBase64Floor(s string, floor int) ([]byte, bool) {
	compact := strings.Map(func(r rune) rune {
		switch r {
		case ' ', '\t', '\n', '\r', '\f', '\v':
			return -1
		}
		return r
	}, s)
	if len(compact) < floor {
		return nil, false
	}
	for i := 0; i < len(compact); i++ {
		c := compact[i]
		isAlpha := c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9'
		if !isAlpha && c != '+' && c != '/' && c != '-' && c != '_' && c != '=' {
			return nil, false
		}
	}
	for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		if out, err := enc.DecodeString(compact); err == nil {
			return out, true
		}
	}
	return nil, false
}

// dataURLPayload returns the decoded payload of a WHOLE-string base64 `data:` URL. The `data:`
// prefix is parsed FIRST because it disqualifies the string from the base64 alphabet: without
// this, `data:text/plain;base64,<payload>` would never be decoded (decision 6).
func dataURLPayload(s string) ([]byte, bool) {
	if !strings.HasPrefix(s, "data:") {
		return nil, false
	}
	head, payload, ok := strings.Cut(s, ",")
	if !ok || !strings.HasSuffix(head, ";base64") {
		return nil, false
	}
	return decodeBase64Floor(payload, 1)
}

// utf16BOM decodes a byte-order-marked UTF-16 text, reporting the byte order so the result can be
// re-encoded the same way.
func utf16BOM(b []byte) (string, binary.ByteOrder, bool) {
	if len(b) < 2 || len(b)%2 != 0 {
		return "", nil, false
	}
	var order binary.ByteOrder
	switch {
	case b[0] == 0xff && b[1] == 0xfe:
		order = binary.LittleEndian
	case b[0] == 0xfe && b[1] == 0xff:
		order = binary.BigEndian
	default:
		return "", nil, false
	}
	u := make([]uint16, 0, len(b)/2)
	for i := 0; i < len(b); i += 2 {
		u = append(u, order.Uint16(b[i:]))
	}
	s := string(utf16.Decode(u))
	if !utf8.ValidString(s) {
		return "", nil, false
	}
	return s, order, true
}

func encodeUTF16(s string, order binary.ByteOrder) []byte {
	u := utf16.Encode([]rune(s))
	out := make([]byte, 2*len(u))
	for i, c := range u {
		order.PutUint16(out[2*i:], c)
	}
	return out
}
