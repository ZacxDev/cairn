package redact

import (
	"encoding/binary"
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
// It is INPUT NORMALISATION, NOT A REWRITE. A line may carry up to four layers, in this order:
//
//  1. Read's numbered copy: `     12\t`, `12→`;
//  2. a tool's line prefix: grep (`path:12:`, `path:12:3:` with `--column`/`rg --vimgrep`,
//     `path-12-` for a context line, `12:` / `12:3:` / `12-` for one file, `path:` without `-n`),
//     docker compose (`svc-1  | `), `kubectl logs --prefix` (`[pod/web/app] `), `git blame`
//     (`1a2b3c4d (Author 2000-01-01 00:00:00 +0000 12) `);
//  3. a log timestamp: ISO 8601 (`2000-01-01T00:00:00.123Z `, `kubectl logs --timestamps`) or
//     syslog with its host and tag (`Jan 01 00:00:00 box unit[12]: `);
//  4. a diff: `< `, `> `, `+`, `-`.
//
// 🔴 A PREFIX IS RECOGNISED BY ITS STRUCTURE, NEVER BY A WORD LIST (review round 4). A bare `path:`
// must LOOK like a path — a `/` or a `.` in it — because `fix:`, `TODO:`, `Q:` and `Subject:` are
// the same bytes in prose, and stripping them exposed `password reset` to a line-start rule. A
// `path-N-` context line must look like a path for the same reason (`top-10-` is prose). The
// numeric forms need no such test: a number glued to `:` or `-` at a line's start is grep's.
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
	// The order of the alternatives matters: leftmost-first, so `path:12:3:` is preferred over
	// `path:12:`, and that over the shorter `path:` reading of the same bytes.
	prefixTool = regexp.MustCompile(`^(?:` +
		`[^\s:]+:[0-9]+:[0-9]+:|[^\s:]+:[0-9]+[:-]|[^\s:]*[/.][^\s:]*-[0-9]+-|[0-9]+:[0-9]+:|[0-9]+[:-]|` +
		`[^\s:]*[/.][^\s:]*:|` +
		// docker compose: a service name, padding, `| `.
		`[A-Za-z0-9][A-Za-z0-9_.-]* +\| |` +
		// kubectl logs --prefix: `[pod/<name>/<container>] `.
		`\[[a-z]+/[^\]\s]+\] |` +
		// git blame: a hash, an optional file name, then `(<author> <date> <time> <tz> <line>) `.
		`\^?[0-9a-f]{7,40}(?: [^\s(]+)? +\([^()\n]*? [0-9]+\) )`)
	prefixTime = regexp.MustCompile(`^(?:` +
		`[0-9]{4}-[0-9]{2}-[0-9]{2}[T ][0-9]{2}:[0-9]{2}:[0-9]{2}(?:[.,][0-9]+)?(?:Z|[+-][0-9]{2}:?[0-9]{2})?[ \t]+|` +
		`[A-Z][a-z]{2} [ 0-9][0-9] [0-9]{2}:[0-9]{2}:[0-9]{2} [^\s]+ [^\s:]+: )`)
	prefixDiff = regexp.MustCompile(`^(?:[<>] ?|[+-])`)
	layers     = []*regexp.Regexp{prefixRead, prefixTool, prefixTime, prefixDiff}
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
	if !strings.ContainsAny(s, "\t:-+<>→|") {
		return out
	}
	lines := strings.SplitAfter(s, "\n")
	// offs[mask][i] is line i's content offset with the layers in mask removed.
	const combos = 1 << 4
	var offs [combos][]int
	any := false
	for mask := 1; mask < combos; mask++ {
		offs[mask] = make([]int, len(lines))
	}
	// A layer's match depends only on (layer, offset), and a line has few distinct offsets, so
	// each is computed once per line rather than once per mask.
	type memoKey struct{ layer, off int }
	memo := map[memoKey]int{}
	for i, l := range lines {
		body := strings.TrimRight(l, "\r\n")
		clear(memo)
		for mask := 1; mask < combos; mask++ {
			off := 0
			for k, re := range layers {
				if mask&(1<<k) == 0 {
					continue
				}
				n, ok := memo[memoKey{k, off}]
				if !ok {
					n = 0
					if re == prefixDiff && yamlDocSep.MatchString(body[off:]) {
						// `---` is a YAML document separator, not a diff's `-` before `--`.
					} else if m := re.FindStringIndex(body[off:]); m != nil {
						n = m[1]
					}
					memo[memoKey{k, off}] = n
				}
				off += n
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
		// The key is every line's offset as a VARINT — self-delimiting, so it is injective at any
		// offset (round 4 kept two bytes per offset, and two views whose prefixes differed by a
		// multiple of 64 KiB collided and one was dropped).
		key := make([]byte, 0, len(lines)*2)
		nonzero := false
		for _, o := range offs[mask] {
			key = binary.AppendUvarint(key, uint64(o))
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
