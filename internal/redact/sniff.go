package redact

import (
	"bytes"
	"encoding/base64"
	"strings"
	"unicode/utf8"
)

// SniffWindow is how far the NUL half of the text rule looks: leakscan's (git's) 8,000 bytes.
const SniffWindow = 8000

// IsText is decision 6a's text rule: no NUL byte in the first [SniffWindow] bytes AND the WHOLE
// value is valid UTF-8.
//
// 🔴 THE UTF-8 HALF IS THIS PACKAGE'S OWN — leakscan has no UTF-8 test — and its consequence is
// stated rather than hidden: UTF-16 and Latin-1 TEXT are "binary" here, and under O12 they ship
// UNREDACTED. Transcoding them first would make them redactable; that is not built.
//
// 🔴 IT READS THE BYTES, NEVER A NAME. A persisted tool result called `.txt` can hold a NUL, and a
// sniff keyed on the extension would scan — and re-encode — bytes that are not text.
func IsText(b []byte) bool {
	window := b
	if len(window) > SniffWindow {
		window = window[:SniffWindow]
	}
	if bytes.IndexByte(window, 0) >= 0 {
		return false
	}
	return utf8.Valid(b)
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

// dataURLPayload returns the decoded payload of a WHOLE-string base64 `data:` URL.
func dataURLPayload(s string) ([]byte, bool) {
	if !strings.HasPrefix(s, "data:") {
		return nil, false
	}
	head, payload, ok := strings.Cut(s, ",")
	if !ok || !strings.HasSuffix(head, ";base64") {
		return nil, false
	}
	out, err := base64.StdEncoding.DecodeString(strings.TrimSpace(payload))
	if err != nil {
		if out2, ok := decodeWholeBase64(payload); ok {
			return out2, true
		}
		return nil, false
	}
	return out, true
}
