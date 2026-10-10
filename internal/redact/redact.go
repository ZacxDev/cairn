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
	// binary is the binary rule, a field for the same reason (the O12 guard's control).
	binary func([]byte) bool
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
	return &Redactor{rules: rules, key: append([]byte(nil), key...), deny: deny, decode: decodeWholeBase64,
		binary: IsBinary}, nil
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
func (r *Redactor) String(s string) (string, []Hit) { return r.scanText(s, scanOpts{}) }

// scanText is the pipeline for one decoded string:
//
//  1. NUL-separated text is split, and each segment scanned on its own;
//  2. a string that is itself ONE JSON document is walked decoded, so a value escaped inside it is
//     scanned as the value it encodes;
//  3. every non-entropy rule runs over every VIEW of the text (normalise.go: tool line prefixes
//     set aside), and its spans are mapped back to the original offsets;
//  4. if nothing matched, a WHOLE-string base64 value (or a base64 `data:` URL) whose payload is
//     not binary is scanned decoded, and a match replaces the whole encoded value;
//  5. the entropy rule runs last — unless the caller's structure says the value is opaque, or the
//     whole string is the encoding of a binary payload (O12: binary ships);
//  6. the merged spans are replaced on the ORIGINAL string; nothing else changes.
func (r *Redactor) scanText(s string, o scanOpts) (string, []Hit) {
	if strings.IndexByte(s, 0) >= 0 {
		// NUL-SEPARATED text (an environ dump, `env -0`, `find -print0`): each segment is a line
		// to the rules, which a `^`-anchored rule would otherwise never see past the first.
		segs := strings.Split(s, "\x00")
		var hits []Hit
		for i, seg := range segs {
			var hs []Hit
			segs[i], hs = r.scanText(seg, o)
			hits = append(hits, hs...)
		}
		return strings.Join(segs, "\x00"), hits
	}
	if o.depth < maxNesting {
		if replaced, hs, ok := r.jsonInString(s, o); ok {
			return replaced, hs
		}
	}
	spans := r.detect(s)
	payload, isB64 := dataURLPayload(s)
	if !isB64 {
		payload, isB64 = r.decode(s)
	}
	binaryPayload := isB64 && r.binary(payload)
	if len(spans) == 0 && isB64 && !binaryPayload && o.depth < maxNesting {
		text := string(payload)
		if u, _, isU16 := utf16Text(payload); isU16 {
			text = u
		}
		// The decoded bytes are scanned WITHOUT the entropy rule: random bytes decoded are not a
		// token, and the encoded string itself meets the entropy rule in step 5.
		if _, inner := r.scanText(text, scanOpts{depth: o.depth + 1, noEntropy: true}); len(inner) > 0 {
			m, h := r.marker("base64/"+inner[0].Rule, s)
			return m, append([]Hit{h}, inner...)
		}
	}
	if !o.noEntropy && !binaryPayload {
		spans = append(spans, r.lateSpans(s)...)
	}
	return r.apply(s, spans)
}

// jsonInString re-enters a string that is ITSELF one JSON document (a tool printing a JSON file,
// an API response), so a value escaped inside it is scanned decoded too. It reports ok only when
// something inside matched; an unmatched document keeps its original text.
func (r *Redactor) jsonInString(s string, o scanOpts) (string, []Hit, bool) {
	trimmed := strings.TrimSpace(s)
	if len(trimmed) < 2 || (trimmed[0] != '{' && trimmed[0] != '[') {
		return "", nil, false
	}
	v, err := decodeJSON([]byte(trimmed))
	if err != nil {
		return "", nil, false
	}
	nv, hits := r.walk(v, scanOpts{depth: o.depth + 1, noEntropy: o.noEntropy})
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

// denyPathKeys are the members a denylisted PATH covers, in the object that names the path
// (`filePath`, `file_path` or `path`): what Read's, Edit's and Write's structured copies carry.
//
// ⚠ WHAT THE GLOB DOES NOT COVER, stated rather than implied: the numbered `tool_result` block
// of a Read (a different block, linked only by `tool_use_id`), and the output of a shell command
// that prints the file (`cat`). Correlating a result block to its call is a cross-record join the
// redactor, which sees one record at a time, does not do.
var denyPathKeys = map[string]bool{
	"content": true, "originalFile": true, "base64": true, "oldString": true, "newString": true,
	"old_string": true, "new_string": true, "structuredPatch": true, "edits": true,
}

// jwkPrivate are a JSON Web Key's private members (RFC 7518 §6.2.2, §6.3.2, §6.4.1).
var jwkPrivate = map[string]bool{"d": true, "p": true, "q": true, "dp": true, "dq": true, "qi": true, "k": true}

// walk redacts every string — every member VALUE, every member KEY, every duplicate — in a
// decoded JSON value.
func (r *Redactor) walk(v any, o scanOpts) (any, []Hit) {
	switch t := v.(type) {
	case *object:
		var hits []Hit
		isSecret := t.has("kind", "Secret")
		denyPath := false
		if r.deny != nil {
			for _, k := range []string{"filePath", "file_path", "path"} {
				for _, p := range t.strings(k) {
					denyPath = denyPath || r.deny.matchesPath(p)
				}
			}
		}
		// A k8s env entry as JSON: {"name": "DB_PASSWORD", "value": "…"}.
		isJWK := len(t.strings("kty")) > 0
		envSecret := false
		for _, n := range t.strings("name") {
			envSecret = envSecret || SecretKey(n)
		}
		// A thinking block's `signature` (and a redacted block's `data`) is an opaque blob the
		// model API issued, not a credential: it is random base64 by construction, so the entropy
		// rule would destroy every one. Every OTHER rule still reads it.
		opaque := t.has("type", "thinking") || t.has("type", "redacted_thinking")
		for i := range t.pairs {
			k, val := t.pairs[i].k, t.pairs[i].v
			mo := o
			if opaque && (k == "signature" || k == "data") {
				mo.noEntropy = true
			}
			if nk, hs := r.scanText(k, o); len(hs) > 0 {
				t.pairs[i].k = nk
				hits = append(hits, hs...)
			}
			s, isString := val.(string)
			switch {
			case isSecret && (k == "data" || k == "stringData"):
				nv, hs := r.redactAllStrings(val, "k8s-secret")
				t.pairs[i].v = nv
				hits = append(hits, hs...)
			case denyPath && denyPathKeys[k]:
				nv, hs := r.redactAllStrings(val, "denylist-path")
				t.pairs[i].v = nv
				hits = append(hits, hs...)
			case isString && isJWK && jwkPrivate[k] && s != "":
				// A JSON Web Key's PRIVATE members carry no secret-shaped name: `d` (RSA/EC/OKP),
				// the RSA CRT parts, and `k` of a symmetric (`oct`) key.
				m, h := r.marker("jwk-private", s)
				t.pairs[i].v = m
				hits = append(hits, h)
			case isString && envSecret && k == "value" && notTrivial(s):
				m, h := r.marker("k8s-env", s)
				t.pairs[i].v = m
				hits = append(hits, h)
			case isString && secretFieldValue(k, s):
				m, h := r.marker("secret-field", s)
				t.pairs[i].v = m
				hits = append(hits, h)
			default:
				nv, hs := r.walk(val, mo)
				t.pairs[i].v = nv
				hits = append(hits, hs...)
			}
		}
		return t, hits
	case []any:
		var hits []Hit
		for i, e := range t {
			nv, hs := r.walk(e, o)
			t[i] = nv
			hits = append(hits, hs...)
		}
		return t, hits
	case string:
		return r.scanText(t, o)
	default:
		return v, nil
	}
}

// secretFieldValue: member `k` names a secret and its string value `s` is one. A JSON string is a
// quoted value, never code notation; a WEAK name ([secretKeyGrade]) holds it to [weakNameValueOK]
// too, exactly as the key-context rule does.
func secretFieldValue(k, s string) bool {
	strong, weak := secretKeyGrade(k)
	if !strong && !(weak && weakNameValueOK(s)) {
		return false
	}
	return keyedValueOK(s, false)
}

func (r *Redactor) redactAllStrings(v any, rule string) (any, []Hit) {
	switch t := v.(type) {
	case *object:
		var hits []Hit
		for i := range t.pairs {
			nv, hs := r.redactAllStrings(t.pairs[i].v, rule)
			t.pairs[i].v = nv
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
// re-encoded (compact; key order, duplicate members and number text preserved). A line that is not
// JSON is redacted as TEXT bytes ([Redactor.Text]) — invalid bytes carried through — and a binary
// one is returned as it is.
func (r *Redactor) Record(raw []byte) ([]byte, []Hit) {
	v, err := decodeJSON(raw)
	if err != nil {
		return r.Text(raw)
	}
	nv, hits := r.walk(v, scanOpts{})
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

// Text redacts a byte string that is not one JSON document.
//
//   - BINARY by the signature rule: returned as it is (O12).
//   - BOM-marked UTF-16: decoded, scanned, and re-encoded in the same byte order on a hit.
//   - anything else: scanned AS IS — NUL-separated segments each scanned, invalid UTF-8 bytes
//     carried through — and only matched spans replaced, so every other byte is identical.
func (r *Redactor) Text(data []byte) ([]byte, []Hit) {
	if r.binary(data) {
		return data, nil
	}
	if s, order, ok := utf16Text(data); ok {
		out, hits := r.scanText(s, scanOpts{})
		if len(hits) == 0 {
			return data, nil
		}
		return encodeUTF16(out, order), hits
	}
	out, hits := r.scanText(string(data), scanOpts{})
	if len(hits) == 0 {
		return data, nil
	}
	return []byte(out), hits
}

// Blob redacts one persisted tool-result file.
//
//   - BINARY (decision 6a's signature rule): returned byte-identical. O12: binary ships.
//   - a JSON document, or JSON Lines: redacted by decoded traversal, so an escaped secret inside
//     is caught; an unmatched blob keeps its original bytes.
//   - any other text: [Redactor.Text].
func (r *Redactor) Blob(name string, data []byte) ([]byte, []Hit) {
	if r.deny != nil && r.deny.matchesPath(name) {
		m, h := r.marker("denylist-path", string(data))
		return []byte(m + "\n"), []Hit{h}
	}
	if r.binary(data) {
		return data, nil
	}
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) > 0 && (trimmed[0] == '{' || trimmed[0] == '[') {
		if v, err := decodeJSON(trimmed); err == nil {
			nv, hits := r.walk(v, scanOpts{})
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
	return r.Text(data)
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

func (d *Denylist) matchesPath(p string) bool {
	if p == "" {
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
