package redact

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"path"
	"strings"
)

// Hit is one redaction: which rule matched and the keyed tag that replaced the value. It carries
// NO part of the secret, so a report of hits can be printed and logged.
type Hit struct {
	Rule string
	Tag  string
}

// Redactor applies one rule table under one host key.
type Redactor struct {
	rules []Rule
	key   []byte
	deny  *Denylist
	// decode is the whole-string base64 decoder. It is a field only so the tests can run the
	// floor and decoder-coverage CONTROLS through the real pipeline; production uses
	// decodeWholeBase64.
	decode func(string) ([]byte, bool)
}

// MinKeyBytes is the shortest host key accepted.
const MinKeyBytes = 16

// New builds a redactor over [DefaultRules] under `key` (see [LoadOrCreateKey]). `deny` may be
// nil.
func New(key []byte, deny *Denylist) (*Redactor, error) {
	return newWithRules(DefaultRules(), key, deny)
}

func newWithRules(rules []Rule, key []byte, deny *Denylist) (*Redactor, error) {
	if len(key) < MinKeyBytes {
		return nil, errors.New("redact: the host key is shorter than 16 bytes; a short key makes the tag guessable")
	}
	return &Redactor{rules: rules, key: append([]byte(nil), key...), deny: deny, decode: decodeWholeBase64}, nil
}

// Tag is the keyed tag for one secret: the first 8 hex of HMAC-SHA256(key, secret).
func (r *Redactor) Tag(secret string) string {
	mac := hmac.New(sha256.New, r.key)
	mac.Write([]byte(secret))
	return hex.EncodeToString(mac.Sum(nil))[:8]
}

func (r *Redactor) marker(rule, secret string) (string, Hit) {
	tag := r.Tag(secret)
	return "[redacted:" + rule + ":" + tag + "]", Hit{Rule: rule, Tag: tag}
}

// maxNesting bounds how many times a string may be re-entered as decoded JSON or decoded base64,
// so a hostile value cannot recurse without end.
const maxNesting = 3

// String redacts one decoded string.
func (r *Redactor) String(s string) (string, []Hit) { return r.scanText(s, 0) }

func (r *Redactor) scanText(s string, depth int) (string, []Hit) {
	var hits []Hit
	out := s
	if r.deny != nil {
		for _, lit := range r.deny.Literals {
			if strings.Contains(out, lit) {
				m, h := r.marker("denylist", lit)
				out = strings.ReplaceAll(out, lit, m)
				hits = append(hits, h)
			}
		}
	}
	for _, rule := range r.rules {
		var hs []Hit
		out, hs = r.applyRule(rule, out)
		hits = append(hits, hs...)
	}
	var hs []Hit
	out, hs = r.yamlSecret(out)
	hits = append(hits, hs...)
	if depth < maxNesting {
		if replaced, hs, ok := r.jsonInString(out, depth); ok {
			out = replaced
			hits = append(hits, hs...)
		}
	}
	if len(hits) == 0 && depth < maxNesting {
		// A WHOLE value that is base64 (or a base64 `data:` URL) whose payload is TEXT is scanned
		// decoded; a match replaces the whole encoded value, because a partially-redacted
		// encoding would still decode to the rest of the secret's context. A payload that is NOT
		// text (an image, a PDF) is left exactly as it is — O12.
		payload, ok := dataURLPayload(s)
		if !ok {
			payload, ok = r.decode(s)
		}
		if ok && IsText(payload) {
			if _, inner := r.scanText(string(payload), depth+1); len(inner) > 0 {
				m, h := r.marker("base64/"+inner[0].Rule, s)
				return m, append([]Hit{h}, inner...)
			}
		}
	}
	return out, hits
}

func (r *Redactor) applyRule(rule Rule, s string) (string, []Hit) {
	locs := rule.Re.FindAllStringSubmatchIndex(s, -1)
	if len(locs) == 0 {
		return s, nil
	}
	var b strings.Builder
	var hits []Hit
	last := 0
	for _, loc := range locs {
		lo, hi := loc[2*rule.Group], loc[2*rule.Group+1]
		if lo < 0 || lo < last {
			continue
		}
		secret := s[lo:hi]
		if strings.HasPrefix(secret, "[redacted:") || (rule.Accept != nil && !rule.Accept(secret)) {
			continue
		}
		m, h := r.marker(rule.Name, secret)
		b.WriteString(s[last:lo])
		b.WriteString(m)
		hits = append(hits, h)
		last = hi
	}
	b.WriteString(s[last:])
	return b.String(), hits
}

// jsonInString re-enters a string that is ITSELF one JSON document (a tool printing a JSON file,
// an API response), so a value escaped inside it is scanned decoded too. It reports ok only when
// something inside matched; an unmatched document keeps its original text.
func (r *Redactor) jsonInString(s string, depth int) (string, []Hit, bool) {
	trimmed := strings.TrimSpace(s)
	if len(trimmed) < 2 || (trimmed[0] != '{' && trimmed[0] != '[') {
		return "", nil, false
	}
	v, err := decodeJSON([]byte(trimmed))
	if err != nil {
		return "", nil, false
	}
	nv, hits := r.walk(v, depth+1)
	if len(hits) == 0 {
		return "", nil, false
	}
	indent := ""
	if strings.Contains(trimmed, "\n") {
		indent = "  "
	}
	enc, err := encodeJSON(nv, indent)
	if err != nil {
		return "", nil, false
	}
	lead := s[:strings.Index(s, trimmed[:1])]
	trail := s[len(lead)+len(trimmed):]
	return lead + string(enc) + trail, hits, true
}

// Value redacts every string in a decoded JSON value (as produced by this package's decoder).
func (r *Redactor) walk(v any, depth int) (any, []Hit) {
	switch t := v.(type) {
	case *object:
		var hits []Hit
		isSecret := t.vals["kind"] == "Secret"
		denyPath := r.deny != nil && (r.deny.matchesPath(t.vals["filePath"]) || r.deny.matchesPath(t.vals["file_path"]))
		for _, k := range t.keys {
			val := t.vals[k]
			switch s, isString := val.(string); {
			case isSecret && (k == "data" || k == "stringData"):
				nv, hs := r.redactAllStrings(val, "k8s-secret")
				t.vals[k] = nv
				hits = append(hits, hs...)
			case denyPath && isString && (k == "content" || k == "originalFile" || k == "base64"):
				m, h := r.marker("denylist-path", s)
				t.vals[k] = m
				hits = append(hits, h)
			case isString && secretFieldRe.MatchString(k) && len(s) >= 8 && notTrivial(s) &&
				!strings.Contains(s, " "):
				m, h := r.marker("secret-field", s)
				t.vals[k] = m
				hits = append(hits, h)
			default:
				nv, hs := r.walk(val, depth)
				t.vals[k] = nv
				hits = append(hits, hs...)
			}
		}
		return t, hits
	case []any:
		var hits []Hit
		for i, e := range t {
			nv, hs := r.walk(e, depth)
			t[i] = nv
			hits = append(hits, hs...)
		}
		return t, hits
	case string:
		return r.scanText(t, depth)
	default:
		return v, nil
	}
}

func (r *Redactor) redactAllStrings(v any, rule string) (any, []Hit) {
	switch t := v.(type) {
	case *object:
		var hits []Hit
		for _, k := range t.keys {
			nv, hs := r.redactAllStrings(t.vals[k], rule)
			t.vals[k] = nv
			hits = append(hits, hs...)
		}
		return t, hits
	case []any:
		var hits []Hit
		for i, e := range t {
			nv, hs := r.redactAllStrings(e, rule)
			t[i] = nv
			hits = append(hits, hs...)
		}
		return t, hits
	case string:
		if t == "" {
			return t, nil
		}
		m, h := r.marker(rule, t)
		return m, []Hit{h}
	default:
		return v, nil
	}
}

// Record redacts one JSON document — one JSONL line without its newline, or one opencode part.
//
// 🔴 AN UNTOUCHED RECORD IS RETURNED BYTE-IDENTICAL. Only a record with at least one hit is
// re-encoded (compact, key order and number text preserved). A line that is not JSON is redacted
// as text, and returned unchanged when it is not text either.
func (r *Redactor) Record(raw []byte) ([]byte, []Hit) {
	v, err := decodeJSON(raw)
	if err != nil {
		if !IsText(raw) {
			return raw, nil
		}
		out, hits := r.scanText(string(raw), 0)
		if len(hits) == 0 {
			return raw, nil
		}
		return []byte(out), hits
	}
	nv, hits := r.walk(v, 0)
	if len(hits) == 0 {
		return raw, nil
	}
	enc, err := encodeJSON(nv, "")
	if err != nil {
		// Cannot happen for a value this package decoded; refuse to ship the original.
		m, h := r.marker("unencodable", string(raw))
		return []byte(m), []Hit{h}
	}
	return enc, hits
}

// Blob redacts one persisted tool-result file.
//
//   - NOT TEXT (decision 6a's rule): returned byte-identical. O12: binary ships.
//   - a JSON document, or JSON Lines: redacted by decoded traversal, so an escaped secret inside
//     is caught; an unmatched blob keeps its original bytes.
//   - any other text: redacted as ONE string with the same table and the line-based Secret rule.
func (r *Redactor) Blob(name string, data []byte) ([]byte, []Hit) {
	if r.deny != nil && r.deny.matchesPath(name) {
		m, h := r.marker("denylist-path", string(data))
		return []byte(m + "\n"), []Hit{h}
	}
	if !IsText(data) {
		return data, nil
	}
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) > 0 && (trimmed[0] == '{' || trimmed[0] == '[') {
		if v, err := decodeJSON(trimmed); err == nil {
			nv, hits := r.walk(v, 0)
			if len(hits) == 0 {
				return data, nil
			}
			indent := ""
			if bytes.Contains(trimmed, []byte("\n")) {
				indent = "  "
			}
			enc, err := encodeJSON(nv, indent)
			if err == nil {
				if bytes.HasSuffix(data, []byte("\n")) {
					enc = append(enc, '\n')
				}
				return enc, hits
			}
		} else if lines, ok := jsonLines(data); ok {
			var out bytes.Buffer
			var hits []Hit
			for _, line := range lines {
				body := bytes.TrimSuffix(line, []byte("\n"))
				red, hs := r.Record(body)
				out.Write(red)
				if len(line) > len(body) {
					out.WriteByte('\n')
				}
				hits = append(hits, hs...)
			}
			if len(hits) == 0 {
				return data, nil
			}
			return out.Bytes(), hits
		}
	}
	out, hits := r.scanText(string(data), 0)
	if len(hits) == 0 {
		return data, nil
	}
	return []byte(out), hits
}

// jsonLines reports whether every non-empty line of data is one JSON document.
func jsonLines(data []byte) ([][]byte, bool) {
	lines := bytes.SplitAfter(data, []byte("\n"))
	n := 0
	for _, l := range lines {
		body := bytes.TrimSpace(l)
		if len(body) == 0 {
			continue
		}
		if _, err := decodeJSON(body); err != nil {
			return nil, false
		}
		n++
	}
	return lines, n > 0
}

func (d *Denylist) matchesPath(v any) bool {
	p, ok := v.(string)
	if !ok || p == "" {
		return false
	}
	for _, g := range d.Globs {
		if ok, _ := path.Match(g, p); ok {
			return true
		}
		if ok, _ := path.Match(g, path.Base(p)); ok {
			return true
		}
	}
	return false
}
