package redact

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// pair is one member of a decoded JSON object.
type pair struct {
	k string
	v any
}

// object is a decoded JSON object that keeps EVERY member, in arrival order.
//
// 🔴 A RE-ENCODED RECORD KEEPS ITS KEY ORDER AND ITS NUMBERS. `encoding/json` decodes an object
// into a map and re-encodes it sorted, and decodes a number into a float64 — so a 20-digit id
// would come back rounded and every record that needed one redaction would come back reshuffled.
// Numbers are kept as `json.Number` and members in arrival order.
//
// 🔴 AND A DUPLICATE KEY IS KEPT, NOT COLLAPSED. A decoder that keeps only the last of two
// members with one name never SCANS the first — and when nothing else in the record matches, the
// ORIGINAL bytes ship, first member included. So a secret in `{"note":"<secret>","note":"ok"}`
// would leave the host unread. Every member is a pair here, every pair's value is walked, and a
// re-encode writes every pair back.
type object struct {
	pairs []pair
}

// get answers whether ANY member named k has value want — every duplicate counts, so a
// `"kind":"Secret"` hidden behind a second `"kind"` still makes the object a Secret.
func (o *object) has(k string, want any) bool {
	for _, p := range o.pairs {
		if p.k == k && p.v == want {
			return true
		}
	}
	return false
}

// strings returns every string value of the members named k.
func (o *object) strings(k string) []string {
	var out []string
	for _, p := range o.pairs {
		if s, ok := p.v.(string); ok && p.k == k {
			out = append(out, s)
		}
	}
	return out
}

// decodeJSON parses exactly one JSON value from raw, preserving order, duplicates and number
// text. Trailing non-space input is an error.
func decodeJSON(raw []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	v, err := decodeValue(dec)
	if err != nil {
		return nil, err
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, fmt.Errorf("trailing data after a JSON value")
	}
	return v, nil
}

func decodeValue(dec *json.Decoder) (any, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	switch t := tok.(type) {
	case json.Delim:
		switch t {
		case '{':
			o := &object{}
			for dec.More() {
				kt, err := dec.Token()
				if err != nil {
					return nil, err
				}
				key, ok := kt.(string)
				if !ok {
					return nil, fmt.Errorf("object key is not a string")
				}
				v, err := decodeValue(dec)
				if err != nil {
					return nil, err
				}
				o.pairs = append(o.pairs, pair{key, v})
			}
			if _, err := dec.Token(); err != nil {
				return nil, err
			}
			return o, nil
		case '[':
			arr := []any{}
			for dec.More() {
				v, err := decodeValue(dec)
				if err != nil {
					return nil, err
				}
				arr = append(arr, v)
			}
			if _, err := dec.Token(); err != nil {
				return nil, err
			}
			return arr, nil
		}
		return nil, fmt.Errorf("unexpected delimiter %v", t)
	default:
		return tok, nil
	}
}

// spliceJSON applies a walk's redactions to the document's ORIGINAL bytes: `raw` is one JSON
// document, `redacted` its decoded tree after the walk. Every string token of `raw` — member keys
// and string values, in document order — is compared with the string at the same position in the
// tree, and only a token whose string CHANGED is replaced (re-encoded); every other byte of the
// document — indentation, key order, number text, the escapes of untouched strings — is the
// original's.
//
// 🔴 WHY (review round 5): re-serialising the whole document for one hit rewrote 695 of a 698-line
// file's lines — every line whose indentation, spacing or escapes differed from this encoder's —
// which is damage to clean text far beyond the secret, and the opposite of what the text path
// promises ("only matched spans replaced").
//
// It reports false when the token stream and the tree do not line up (which a tree this package
// decoded from the same bytes always does); the caller then re-encodes rather than ship a
// half-applied result.
func spliceJSON(raw []byte, redacted any) ([]byte, bool) {
	var want []string
	flattenStrings(redacted, &want)
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var out bytes.Buffer
	last, n := 0, 0
	for {
		before := int(dec.InputOffset())
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, false
		}
		s, ok := tok.(string)
		if !ok {
			continue
		}
		after := int(dec.InputOffset())
		if n >= len(want) {
			return nil, false
		}
		if want[n] != s {
			// Only separators and white space stand between two tokens, so the first quote after
			// the previous token opens this one.
			q := bytes.IndexByte(raw[before:after], '"')
			if q < 0 {
				return nil, false
			}
			out.Write(raw[last : before+q])
			if err := writeString(&out, want[n]); err != nil {
				return nil, false
			}
			last = after
		}
		n++
	}
	if n != len(want) {
		return nil, false
	}
	out.Write(raw[last:])
	return out.Bytes(), true
}

// flattenStrings lists every string of a decoded tree in document order: a member's key, then its
// value.
func flattenStrings(v any, out *[]string) {
	switch t := v.(type) {
	case *object:
		for _, p := range t.pairs {
			*out = append(*out, p.k)
			flattenStrings(p.v, out)
		}
	case []any:
		for _, e := range t {
			flattenStrings(e, out)
		}
	case string:
		*out = append(*out, t)
	}
}

// encodeJSON writes v compactly. `indent` non-empty pretty-prints, for a text blob that arrived
// pretty-printed.
func encodeJSON(v any, indent string) ([]byte, error) {
	var buf bytes.Buffer
	if err := writeValue(&buf, v, indent, 0); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func writeString(buf *bytes.Buffer, s string) error {
	var tmp bytes.Buffer
	enc := json.NewEncoder(&tmp)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(s); err != nil {
		return err
	}
	buf.Write(bytes.TrimSuffix(tmp.Bytes(), []byte("\n")))
	return nil
}

func newline(buf *bytes.Buffer, indent string, depth int) {
	if indent == "" {
		return
	}
	buf.WriteByte('\n')
	buf.WriteString(strings.Repeat(indent, depth))
}

func writeValue(buf *bytes.Buffer, v any, indent string, depth int) error {
	switch t := v.(type) {
	case *object:
		if len(t.pairs) == 0 {
			buf.WriteString("{}")
			return nil
		}
		buf.WriteByte('{')
		for i, p := range t.pairs {
			if i > 0 {
				buf.WriteByte(',')
			}
			newline(buf, indent, depth+1)
			if err := writeString(buf, p.k); err != nil {
				return err
			}
			buf.WriteByte(':')
			if indent != "" {
				buf.WriteByte(' ')
			}
			if err := writeValue(buf, p.v, indent, depth+1); err != nil {
				return err
			}
		}
		newline(buf, indent, depth)
		buf.WriteByte('}')
	case []any:
		if len(t) == 0 {
			buf.WriteString("[]")
			return nil
		}
		buf.WriteByte('[')
		for i, e := range t {
			if i > 0 {
				buf.WriteByte(',')
			}
			newline(buf, indent, depth+1)
			if err := writeValue(buf, e, indent, depth+1); err != nil {
				return err
			}
		}
		newline(buf, indent, depth)
		buf.WriteByte(']')
	case string:
		return writeString(buf, t)
	case json.Number:
		buf.WriteString(t.String())
	case bool:
		if t {
			buf.WriteString("true")
		} else {
			buf.WriteString("false")
		}
	case nil:
		buf.WriteString("null")
	default:
		return fmt.Errorf("redact: cannot encode %T", v)
	}
	return nil
}
