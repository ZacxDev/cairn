package redact

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// object is a decoded JSON object that REMEMBERS its key order.
//
// 🔴 A RE-ENCODED RECORD KEEPS ITS KEY ORDER AND ITS NUMBERS. `encoding/json` decodes an object
// into a map and re-encodes it sorted, and decodes a number into a float64 — so a 20-digit id
// would come back rounded and every record that needed one redaction would come back reshuffled.
// Numbers are kept as `json.Number` (the literal text) and keys in arrival order, so a redacted
// record differs from its source only in the strings that were redacted (and in JSON escaping,
// which is re-chosen by the encoder).
type object struct {
	keys []string
	vals map[string]any
}

// decodeJSON parses exactly one JSON value from raw, preserving order and number text. Trailing
// non-space input is an error: a "JSON" that is a prefix of something else is not one document.
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
			o := &object{vals: map[string]any{}}
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
				if _, dup := o.vals[key]; !dup {
					o.keys = append(o.keys, key)
				}
				o.vals[key] = v
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
		if len(t.keys) == 0 {
			buf.WriteString("{}")
			return nil
		}
		buf.WriteByte('{')
		for i, k := range t.keys {
			if i > 0 {
				buf.WriteByte(',')
			}
			newline(buf, indent, depth+1)
			if err := writeString(buf, k); err != nil {
				return err
			}
			buf.WriteByte(':')
			if indent != "" {
				buf.WriteByte(' ')
			}
			if err := writeValue(buf, t.vals[k], indent, depth+1); err != nil {
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
