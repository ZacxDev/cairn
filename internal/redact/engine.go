package redact

import (
	"sort"
	"strings"
)

// span is one match on the ORIGINAL string: [lo, hi) is replaced by a marker naming `rule`.
// `prio` orders rules when two spans overlap: the lower wins the name (table order — a specific
// token shape before key context, key context before structure, entropy last).
type span struct {
	lo, hi int
	rule   string
	prio   int
}

// scanOpts carries what a caller knows about the string beyond its bytes.
type scanOpts struct {
	depth int
	// noEntropy turns the entropy rule off for a value whose STRUCTURE says it is an opaque
	// non-secret — a thinking block's signature — and for decoded base64 bytes, where "entropy"
	// would be a property of the encoding's payload, not of a token.
	noEntropy bool
}

const (
	prioDenylist = -1
	prioYAML     = 500
	// prioLate puts the entropy rule BELOW every structural rule, so a value a named rule also
	// matched is tagged by that rule's name.
	prioLate = 1000
)

// ruleSpans runs one table rule over one view and returns its spans in ORIGINAL offsets.
func (r *Redactor) ruleSpans(rule Rule, prio int, v *view) []span {
	var out []span
	add := func(lo, hi int) {
		out = append(out, span{lo: v.toOriginal(lo, false), hi: v.toOriginal(hi, true), rule: rule.Name, prio: prio})
	}
	if rule.FindV != nil {
		for _, m := range rule.FindV(v) {
			add(m[0], m[1])
		}
		return out
	}
	if rule.Find != nil {
		for _, m := range rule.Find(v.text) {
			add(m[0], m[1])
		}
		return out
	}
	for _, loc := range rule.Re.FindAllStringSubmatchIndex(v.text, -1) {
		lo, hi := loc[2*rule.Group], loc[2*rule.Group+1]
		if lo < 0 || lo == hi {
			continue
		}
		if rule.KeyGroup > 0 {
			klo, khi := loc[2*rule.KeyGroup], loc[2*rule.KeyGroup+1]
			keyOK := rule.KeyOK
			if keyOK == nil {
				keyOK = SecretKey
			}
			if klo < 0 || !keyOK(v.text[klo:khi]) {
				continue
			}
		}
		if rule.Accept != nil && !rule.Accept(v.text[lo:hi]) {
			continue
		}
		add(lo, hi)
	}
	return out
}

// detect returns every non-entropy span over every view of s.
func (r *Redactor) detect(s string) []span {
	var out []span
	if r.deny != nil {
		for _, lit := range r.deny.Literals {
			for i := 0; lit != ""; {
				j := strings.Index(s[i:], lit)
				if j < 0 {
					break
				}
				out = append(out, span{lo: i + j, hi: i + j + len(lit), rule: "denylist", prio: prioDenylist})
				i += j + len(lit)
			}
		}
	}
	vs := views(s)
	for prio, rule := range r.rules {
		if rule.Late {
			continue
		}
		for _, v := range vs {
			if !v.original && !rule.Anchored {
				// An unanchored rule finds the same spans in every view; only rules that read a
				// line's START are run over the prefix-stripped ones.
				continue
			}
			out = append(out, r.ruleSpans(rule, prio, v)...)
		}
	}
	for _, v := range vs {
		for _, m := range yamlSpans(v.text) {
			out = append(out, span{lo: v.toOriginal(m.lo, false), hi: v.toOriginal(m.hi, true), rule: m.rule, prio: prioYAML})
		}
	}
	return out
}

// lateSpans runs the LATE rules (entropy) over the original text only: they are unanchored.
func (r *Redactor) lateSpans(s string) []span {
	var out []span
	v := &view{text: s, original: true}
	for i, rule := range r.rules {
		if rule.Late {
			out = append(out, r.ruleSpans(rule, prioLate+i, v)...)
		}
	}
	return out
}

// apply merges overlapping spans (the union, named by the winning rule) and replaces each merged
// span of s with its marker. Nothing outside a span changes.
func (r *Redactor) apply(s string, spans []span) (string, []Hit) {
	if len(spans) == 0 {
		return s, nil
	}
	sort.Slice(spans, func(i, j int) bool {
		if spans[i].lo != spans[j].lo {
			return spans[i].lo < spans[j].lo
		}
		return spans[i].prio < spans[j].prio
	})
	// An existing marker is never part of a span: each span loses the marker regions it overlaps
	// and what is left of it is redacted ([withoutMarkers]). Pieces stay in (lo, prio) order
	// because a span's pieces are disjoint and ascending, and they replace it in place.
	regions := markerRegions(s)
	if len(regions) > 0 {
		var cut []span
		for _, sp := range spans {
			cut = append(cut, withoutMarkers(sp, regions)...)
		}
		spans = cut
		sort.SliceStable(spans, func(i, j int) bool {
			if spans[i].lo != spans[j].lo {
				return spans[i].lo < spans[j].lo
			}
			return spans[i].prio < spans[j].prio
		})
	}
	var merged []span
	for _, sp := range spans {
		if sp.lo < 0 || sp.hi > len(s) || sp.lo >= sp.hi {
			continue
		}
		if n := len(merged); n > 0 && sp.lo < merged[n-1].hi {
			last := &merged[n-1]
			if sp.hi > last.hi {
				last.hi = sp.hi
			}
			if sp.prio < last.prio {
				last.prio, last.rule = sp.prio, sp.rule
			}
			continue
		}
		merged = append(merged, sp)
	}
	if len(merged) == 0 {
		return s, nil
	}
	var b strings.Builder
	var hits []Hit
	last := 0
	for _, sp := range merged {
		m, h := r.marker(sp.rule, s[sp.lo:sp.hi])
		b.WriteString(s[last:sp.lo])
		b.WriteString(m)
		hits = append(hits, h)
		last = sp.hi
	}
	b.WriteString(s[last:])
	return b.String(), hits
}
