package redact

import (
	"bytes"
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
	s := Score{Planted: len(c.Plants)}
	for _, p := range c.Plants {
		leaked := false
		for _, f := range p.Forms {
			if strings.Contains(h, f) {
				leaked = true
			}
		}
		if leaked {
			s.Missed = append(s.Missed, p)
			continue
		}
		if !c.ruleFiredOn(p, itemRules) {
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
func (c Corpus) ruleFiredOn(p Plant, itemRules []map[string]bool) bool {
	for i, it := range c.Items {
		carried := false
		text := string(it.Data)
		if u, _, ok := utf16Text(it.Data); ok {
			text = u
		}
		for _, f := range p.Forms {
			if strings.Contains(text, f) {
				carried = true
			}
		}
		if carried && itemRules[i][p.Rule] {
			return true
		}
	}
	return false
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
