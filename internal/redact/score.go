package redact

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"strings"
)

// redactor is what the scorer drives: the real one, or a control.
type redactor interface {
	Record(raw []byte) ([]byte, []Hit)
	Blob(name string, data []byte) ([]byte, []Hit)
}

// Score is the corpus measurement: the pair the closing condition reports, and which plants and
// clean values went wrong (by LABEL and rule — never by value).
type Score struct {
	Planted, Caught, CleanDamaged int
	// Missed holds every plant NOT caught: leaked, or gone but not by the rule it was planted
	// for (WrongRule).
	Missed  []Plant
	Damaged []Clean
	// WrongRule is the subset of Missed whose value is gone from the output, but by a rule other
	// than the plant's own — per-rule coverage that a leak-only check cannot see.
	WrongRule []Plant
}

// Score redacts every item and counts.
//
// A plant is CAUGHT when (1) none of its forms appears in the output — searched in the raw output
// bytes, in every decoded JSON string AND KEY of it, and in a UTF-16 output decoded — and (2) its
// OWN rule fired on the item that carried it. (2) is what makes `Plant.Rule` a claim: without it a
// rule could stop matching while a broader one quietly took over, and every per-rule test the
// corpus is supposed to underwrite would still read green.
//
// 🔴 AND A FORM IS SEARCHED IN ITS JSON-ESCAPED SPELLINGS TOO (review round 5). The decoded-string
// search finds a plant the encoder escaped only when the output DECODES. An output that does not —
// trailing bytes after the document, a document encoded twice — kept a plant holding `&`, `<`, `>`
// or a quote in escaped form, where no search looked: it read as "gone", and [ruleFiredOn] then
// credited it. A redactor that changes nothing but corrupts the encoding scored 8 plants over 60
// seeds — and, the same hole through a different door, a UTF-16 blob with one byte appended no
// longer decodes as UTF-16, so its plant was "gone" too. [encodedForms] closes both without
// depending on a decode; `SelfTest` runs such redactors as a third control.
//
// A clean value is DAMAGED when it no longer appears anywhere.
func (c Corpus) Score(r redactor) Score {
	var hay strings.Builder
	itemRules := make([]map[string]bool, len(c.Items))
	for i, it := range c.Items {
		var out []byte
		var hits []Hit
		if it.Blob {
			out, hits = r.Blob(it.Name, it.Data)
		} else {
			out, hits = r.Record(it.Data)
		}
		itemRules[i] = map[string]bool{}
		for _, h := range hits {
			itemRules[i][h.Rule] = true
		}
		hay.Write(out)
		hay.WriteByte(0)
		if s, _, ok := utf16Text(out); ok {
			hay.WriteString(s)
			hay.WriteByte(0)
		}
		for _, line := range bytes.Split(out, []byte("\n")) {
			if v, err := decodeJSON(bytes.TrimSpace(line)); err == nil {
				collectStrings(v, &hay)
			}
		}
		if v, err := decodeJSON(bytes.TrimSpace(out)); err == nil {
			collectStrings(v, &hay)
		}
	}
	h := hay.String()
	carried := c.carriedTexts()
	s := Score{Planted: len(c.Plants)}
	for _, p := range c.Plants {
		leaked := false
		for _, f := range p.Forms {
			if strings.Contains(h, f) {
				leaked = true
			}
			for _, e := range encodedForms(f) {
				if strings.Contains(h, e) {
					leaked = true
				}
			}
		}
		if leaked {
			s.Missed = append(s.Missed, p)
			continue
		}
		if !ruleFiredOn(p, carried, itemRules) {
			s.Missed = append(s.Missed, p)
			s.WrongRule = append(s.WrongRule, p)
			continue
		}
		s.Caught++
	}
	for _, cl := range c.Clean {
		if !strings.Contains(h, cl.Value) {
			s.CleanDamaged++
			s.Damaged = append(s.Damaged, cl)
		}
	}
	return s
}

// ruleFiredOn answers whether the plant's own rule produced a hit on an item that carried it.
//
// 🔴 "CARRIED" IS READ IN THE DECODED STRINGS TOO, not only in the raw bytes (review round 5):
// Go's JSON encoder writes `&`, `<` and `>` as six-character escapes (a backslash, `u`, four hex
// digits), so a plant holding one of them was never found in its own record's bytes, its rule's
// hit was never credited, and the self-test scored 78/79 on about one seed in six — the
// `symbol-password` plant, whose alphabet carries `&`. The leak oracle above already searched
// decoded strings; this check did not.
func ruleFiredOn(p Plant, carried []string, itemRules []map[string]bool) bool {
	for i, text := range carried {
		if !itemRules[i][p.Rule] {
			continue
		}
		for _, f := range p.Forms {
			if strings.Contains(text, f) {
				return true
			}
		}
	}
	return false
}

// carriedTexts is, per item, the text a plant is looked for in to decide the item CARRIED it: the
// bytes (UTF-16 decoded), and for a record every decoded string of it. Built once per score — it
// was rebuilt, JSON decode included, for every plant.
func (c Corpus) carriedTexts() []string {
	out := make([]string, len(c.Items))
	for i, it := range c.Items {
		text := string(it.Data)
		if u, _, ok := utf16Text(it.Data); ok {
			text = u
		}
		if !it.Blob {
			var b strings.Builder
			b.WriteString(text)
			if v, err := decodeJSON(bytes.TrimSpace(it.Data)); err == nil {
				collectStrings(v, &b)
			}
			text = b.String()
		}
		out[i] = text
	}
	return out
}

// encodedForms returns the spellings of f a leak can survive in without decoding: inside a JSON
// string, with and without HTML escaping (`&` as a six-character escape, or literal), each also
// escaped a SECOND time — what a document encoded twice holds — and as UTF-16 bytes in either
// byte order.
func encodedForms(f string) []string {
	var out []string
	seen := map[string]bool{f: true}
	add := func(s string) {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	once := []string{jsonEscape(f, true), jsonEscape(f, false)}
	for _, e := range once {
		add(e)
	}
	for _, e := range once {
		add(jsonEscape(e, true))
		add(jsonEscape(e, false))
	}
	for _, order := range []binary.ByteOrder{binary.LittleEndian, binary.BigEndian} {
		add(string(encodeUTF16(f, order)))
	}
	return out
}

// jsonEscape is s as the body of a JSON string literal (no surrounding quotes).
func jsonEscape(s string, html bool) string {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(html)
	if err := enc.Encode(s); err != nil {
		return s
	}
	t := bytes.TrimSuffix(b.Bytes(), []byte("\n"))
	return string(t[1 : len(t)-1])
}

func collectStrings(v any, b *strings.Builder) {
	switch t := v.(type) {
	case *object:
		for _, p := range t.pairs {
			b.WriteString(p.k)
			b.WriteByte(0)
			collectStrings(p.v, b)
		}
	case []any:
		for _, e := range t {
			collectStrings(e, b)
		}
	case string:
		b.WriteString(t)
		b.WriteByte(0)
	}
}
