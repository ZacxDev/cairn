package redact

import (
	"regexp"
	"sort"
	"strings"
)

// 🔴 STRUCTURAL FIRST (operator decision O15): before any rule matches, a tool's LINE PREFIXES are
// set aside, so every rule sees the content of the line rather than the bytes a tool put before it.
//
// Three review rounds added the copy shapes one at a time to individual rules — Read's numbered
// copy to the dotenv rule, `grep -n` to the PEM body, a diff's `+`/`-` to the YAML rules — and a
// fresh set still found `N:`, `path:`, `N-` context lines and a diff's `<`/`>` hiding a line from
// every rule that had not been taught that shape. So the prefix grammar lives HERE, ONCE, and no
// rule spells a prefix.
//
// It is INPUT NORMALISATION, NOT A REWRITE. A line may carry up to three layers, in this order:
//
//  1. Read's numbered copy: `     12\t`, `12→`;
//  2. grep: `path:12:`, `path-12-` (a context line), `12:` / `12-` (one file), `path:` (no `-n`);
//  3. a diff: `< `, `> `, `+`, `-`.
//
// Every subset of the layers that a line matches yields a VIEW — the text with those prefixes
// removed from every line that carries them — and the rules run over each distinct view. The
// original text is always a view, because a prefix reading can be wrong (`password: x` also parses
// as a grep `path:` prefix, and stripping it would hide the key); running both is what makes a
// wrong reading cost nothing. A match in a view is mapped back to the ORIGINAL byte offsets, and
// redaction is applied to the original string once, so a line's prefix survives unchanged.
//
// ⚠ A MATCH THAT SPANS LINES in a view (a private-key block) maps to an original span that also
// covers the prefixes of the lines inside it: those line numbers are redacted with the block.

var (
	prefixRead = regexp.MustCompile(`^[ \t]*[0-9]+(?:\t|→)`)
	// The order of the alternatives matters: leftmost-first, so `path:12:` is preferred over the
	// shorter `path:` reading of the same bytes.
	prefixGrep = regexp.MustCompile(`^(?:[^\s:]+:[0-9]+[:-]|[^\s:]+-[0-9]+-|[0-9]+[:-]|[^\s:]+:)`)
	prefixDiff = regexp.MustCompile(`^(?:[<>] ?|[+-])`)
	layers     = []*regexp.Regexp{prefixRead, prefixGrep, prefixDiff}
)

// view is the text with a chosen set of prefixes removed, and the map back to the original.
type view struct {
	text string
	// vStart[i] is where line i starts in text; oStart[i] is the original offset that byte
	// came from. Within a line the mapping is a constant shift.
	vStart, oStart []int
	original       bool
}

// toOriginal maps a view offset to the original string's offset. An END offset is mapped through
// the byte before it, so a span ending at a line break does not land on the next line's prefix.
func (v *view) toOriginal(p int, end bool) int {
	if v.original {
		return p
	}
	q := p
	if end && q > 0 {
		q--
	}
	i := sort.Search(len(v.vStart), func(i int) bool { return v.vStart[i] > q }) - 1
	if i < 0 {
		i = 0
	}
	o := v.oStart[i] + (q - v.vStart[i])
	if end && p > 0 {
		o++
	}
	return o
}

// views returns the original text and every distinct prefix-stripped view of it.
func views(s string) []*view {
	out := []*view{{text: s, original: true}}
	if !strings.ContainsAny(s, "\t:-+<>→") {
		return out
	}
	lines := strings.SplitAfter(s, "\n")
	// offs[mask][i] is line i's content offset with the layers in mask removed.
	const combos = 1 << 3
	var offs [combos][]int
	any := false
	for mask := 1; mask < combos; mask++ {
		offs[mask] = make([]int, len(lines))
	}
	for i, l := range lines {
		body := strings.TrimRight(l, "\r\n")
		for mask := 1; mask < combos; mask++ {
			off := 0
			for k, re := range layers {
				if mask&(1<<k) == 0 {
					continue
				}
				if re == prefixDiff && yamlDocSep.MatchString(body[off:]) {
					// `---` is a YAML document separator, not a diff's `-` before `--`.
					continue
				}
				if m := re.FindStringIndex(body[off:]); m != nil {
					off += m[1]
				}
			}
			offs[mask][i] = off
			if off > 0 {
				any = true
			}
		}
	}
	if !any {
		return out
	}
	seen := map[string]bool{}
	for mask := 1; mask < combos; mask++ {
		key := make([]byte, 0, len(lines)*2)
		nonzero := false
		for _, o := range offs[mask] {
			key = append(key, byte(o), byte(o>>8))
			nonzero = nonzero || o > 0
		}
		if !nonzero || seen[string(key)] {
			continue
		}
		seen[string(key)] = true
		v := &view{vStart: make([]int, len(lines)), oStart: make([]int, len(lines))}
		var b strings.Builder
		pos := 0
		for i, l := range lines {
			v.vStart[i] = b.Len()
			v.oStart[i] = pos + offs[mask][i]
			b.WriteString(l[offs[mask][i]:])
			pos += len(l)
		}
		v.text = b.String()
		out = append(out, v)
	}
	return out
}
