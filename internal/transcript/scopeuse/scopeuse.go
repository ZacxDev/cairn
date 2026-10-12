// Package scopeuse derives the READ half of a session's visibility set — every scope it read
// through cairn, as far as its transcript and its client read ledger show — and the two
// fail-closed fallbacks (`claudedocs/plan-cairn-plugins.md`, decision 3).
//
// 🔴 ONE FUNCTION, TWO CALLERS, THE SAME ANSWER. `cmd/cairn-capture` computes it for ROUTING
// (decision 16) and `cairn-ui` will re-derive it over the STORED records (S3), so an old or lying
// agent cannot shrink `V`. Neither caller resolves a working directory or parses a shell line: a
// command line is read for exactly two substrings (F1, F2), never decomposed.
//
// 🔴 EVERYTHING HERE ONLY ADDS. A header quoted in prose, a spurious ledger record, an
// over-matching F1 or F2 — each adds a scope or `*`, which hides the session from MORE people. The
// sentinel `*` ("unknown scope") is readable by nobody but the owner, so every way this package
// can be wrong about a READ fails closed rather than open.
//
// The parts:
//
//   - R_header: every scope named by a rendered read header (`subsystem-recall:`) found in ANY
//     string of ANY record — tool results, duplicates, hook attachments, opencode outputs. A
//     header whose `scope=` is absent or not a valid scope name (the store-wide `(all scopes)`
//     search, the "all N entry files … MALFORMED" form) contributes `*`.
//   - R_memo (folded into R_header): every scope a MEMO BLOCK names (`<<<cairn-memo` …), found the
//     same way. The memo hook injects one with no header and no command line, and its previews are
//     that scope's text, so it is a read; a block that names no parseable scope contributes `*`.
//     It was missing until #261 round 2 ([MemoScopes]).
//   - L: the scopes the client read ledger names (decision 3a). A ledger line that does not parse,
//     or names an invalid scope, contributes `*`.
//   - F1: a tool INPUT names `cairn` or `subsystem-recall` (case-insensitive) AND the ledger holds
//     ZERO records → `*`. It asks "is the ledger empty", never "which record is this call's".
//   - F2: a tool input names `subsystem-store` (the client cache root's directory name) → `*`,
//     because a file read from the cache never passes through the client and is never ledgered.
//
// ⚠ WHAT IT CANNOT SEE (threat T3): a session mixing a ledger-writing client with one that writes
// nothing (non-empty ledger, so F1 is silent); a file read of a store copy whose path lacks
// `subsystem-store`; a read that bypasses the client entirely; a memo shown by any path that does
// not print the renderer's block (a memo's text quoted without its fence, or the browser tab).
package scopeuse

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"regexp"
	"sort"
	"strings"

	"github.com/ZacxDev/cairn/internal/authz"
)

// Star is the "unknown scope" sentinel.
const Star = "*"

// HeaderMarker is the prefix every rendered read header carries (`internal/report`).
const HeaderMarker = "subsystem-recall:"

var scopeField = regexp.MustCompile(`(?:^|\s)scope=(\S*)`)

// ValidScope is the scope-name grammar: the path-component rule the pod applies to a scope
// before it reaches the filesystem (`authz.SafePathComponent`) — one rule, imported, not copied.
func ValidScope(name string) bool { return authz.SafePathComponent.MatchString(name) }

// HeaderScopes returns every scope named by a rendered read header in s, `*` for a header that
// names none or names something that is not a scope.
//
// A header is a marker and the rest of ITS line: every `scope=` field from the marker to the end
// of the line counts toward it, and a marker followed by none adds `*`.
//
// 🔴 LINEAR, AND THAT IS A REQUIREMENT, NOT A TUNING: the pod runs this over every record it
// stores, while that session's uploads wait on its lock. It used to re-read the rest of the line
// after EVERY marker, so a line of back-to-back markers with no newline was quadratic — measured
// at 2.3 s for a 32 KB record and 2 min 39 s for 256 KB, under a 4 MiB cap (#261's audit). Now
// each line is scanned once: the fields after its FIRST marker are found in one pass (they are the
// union of every later marker's, since a field begins at whitespace and no field can swallow one),
// plus the one case a later marker adds by itself — `scope=` IMMEDIATELY after it, where the old
// per-marker scan matched at the start of its substring. That value's END is found by one forward
// cursor every marker shares; looking for it from each marker was still quadratic on a
// whitespace-free run of `subsystem-recall:scope=` (round 2: 691 ms at 128 KB).
// `TestHeaderScopesMatchesThePerMarkerScan` holds the scan to the per-marker answer over generated
// lines; `TestHeaderScopesIsLinearOnARunOfMarkers` holds both shapes to linear growth.
//
// ⚠ A SHORTER RULE WAS PROPOSED AND IS NOT EQUIVALENT: "the fields after the first marker, `*` if no
// field starts at or after the LAST marker, and any later marker's immediate `scope=` value". On
// `subsystem-recall:x subsystem-recall:scope=a` the first marker has no field of its own and the
// per-marker answer is {*, a}; the rule gives {a} if an immediate `scope=` counts as a field (a
// dropped `*` — fail OPEN), and on `subsystem-recall:scope=b subsystem-recall:scope=a` it gives
// {*, a, b} against {a, b} if it does not. Per-marker `found` is kept for that reason.
func HeaderScopes(s string) []string {
	var out []string
	for {
		i := strings.Index(s, HeaderMarker)
		if i < 0 {
			return out
		}
		line := s[i:]
		rest := ""
		if nl := strings.IndexByte(line, '\n'); nl >= 0 {
			line, rest = line[:nl], line[nl:]
		}
		out = append(out, lineScopes(line)...)
		s = rest
	}
}

// fieldEnd is RE2's `\s` — the characters a `scope=` value (`\S*`) runs up to.
const fieldEnd = "\t\n\f\r "

// lineScopes is [HeaderScopes] for ONE line that begins with a marker.
func lineScopes(line string) []string {
	var marks []int // the offset just past each marker
	for off := 0; ; {
		j := strings.Index(line[off:], HeaderMarker)
		if j < 0 {
			break
		}
		off += j + len(HeaderMarker)
		marks = append(marks, off)
	}
	var out []string
	add := func(v string) {
		if ValidScope(v) {
			out = append(out, v)
		} else {
			out = append(out, Star)
		}
	}
	body := line[marks[0]:]
	locs := scopeField.FindAllStringSubmatchIndex(body, -1)
	for _, l := range locs {
		add(body[l[2]:l[3]])
	}
	// end is where the field-end scan last stopped: the first field end at or after the position it
	// was asked about. Markers arrive in order, so a later marker whose value starts at or before
	// `end` ends at that same `end` and never rescans; only one starting past it scans again, from
	// its own start. Every byte is scanned at most once per line. (Scanning from each marker instead
	// was the round-2 quadratic: with no whitespace, each `scope=` value ran to the end of the line.)
	end := -1
	k := 0
	for _, m := range marks {
		rel := m - marks[0]
		found := false
		if rel > 0 && strings.HasPrefix(body[rel:], "scope=") {
			start := rel + len("scope=")
			if end < start {
				end = len(body)
				if e := strings.IndexAny(body[start:], fieldEnd); e >= 0 {
					end = start + e
				}
			}
			add(body[start:end])
			found = true
		}
		for k < len(locs) && locs[k][0] < rel {
			k++
		}
		if k < len(locs) {
			found = true
		}
		if !found {
			out = append(out, Star)
		}
	}
	return out
}

// Deriver accumulates one ROOT session's evidence across every stream. Feed it every string of
// every record ([Deriver.Content]), every tool input ([Deriver.ToolInput]) and every ledger file
// ([Deriver.Ledger]); then read [Deriver.Result].
type Deriver struct {
	headers       map[string]bool
	ledger        map[string]bool
	ledgerRecords int
	namesProgram  bool
	namesCache    bool
}

// NewDeriver returns an empty Deriver.
func NewDeriver() *Deriver {
	return &Deriver{headers: map[string]bool{}, ledger: map[string]bool{}}
}

// Content scans one string of any record for rendered read headers.
//
// A string that is ITSELF a JSON document (a tool printing a JSON response that embeds a rendered
// recall) is also decoded and its strings scanned: in the raw text the header's newline is the two
// characters `\n`, so the `scope=` value runs into the next field and parses as no scope at all —
// fail-closed `*`, but `*` where the real scope is readable (review round 1). So a string that
// decodes is scanned through EVERY string and EVERY key it holds instead of as raw text — nothing
// it carries goes unscanned — and one that does not decode is scanned raw.
//
// 🔴 THE DECODE IS NOT GATED ON THE MARKER BEING PRESENT. It was, and JSON can ESCAPE any character
// of the marker (`subsystem-recall:`), so such a string was neither decoded nor matched raw
// and contributed NOTHING — not even `*` (#261 round 2). Every string that opens like a document is
// now decoded. The cost stays linear: json.Valid and the token walk are each one pass, and the
// depth cap bounds how many times one byte is re-decoded through nesting.
func (d *Deriver) Content(s string) { d.content(s, 0) }

func (d *Deriver) content(s string, depth int) {
	if depth < 3 {
		t := strings.TrimSpace(s)
		if len(t) >= 2 && (t[0] == '{' || t[0] == '[') {
			if strs, ok := jsonStrings(t); ok {
				for _, inner := range strs {
					d.content(inner, depth+1)
				}
				return
			}
		}
	}
	for _, sc := range HeaderScopes(s) {
		d.headers[sc] = true
	}
	for _, sc := range MemoScopes(s) {
		d.headers[sc] = true
	}
}

// MemoBlockOpen and MemoBlockClose begin the first and last lines of a memo block as
// `internal/memo` renders it (`<<<cairn-memo untrusted nonce=… count=…>>>` … `<<<end cairn-memo
// nonce=…>>>`). They are spelled here rather than imported because `internal/memo` pulls the
// store and the redactor into every capture binary; `TestAMemoBlockIsARead` renders a REAL block
// through `memo.RenderPreview` and `memo.RenderFull`, so a renderer change that this parse no
// longer reads turns that test red instead of shrinking `V`.
const (
	MemoBlockOpen  = "<<<cairn-memo"
	MemoBlockClose = "<<<end cairn-memo"
)

// MemoScopes returns every scope a memo block in s names — `*` for a block that names none this
// can parse. A memo block is a READ: the memo hook injects one into a session (a hook attachment,
// with no `subsystem-recall:` header and no command line anywhere), and its previews are text OF
// that scope. Before this, a session that wrote only alpha-notes and was shown a gamma-notes memo
// was visible to an alpha-only viewer, memo text included (#261 round 2).
//
// A block runs from its open marker to its close marker or the next open marker, whichever comes
// first, so a string of unterminated opens is still scanned once. Within it, a scope is read from
// two line shapes, each after the block's `| ` content prefix:
//
//	[m-<id>] from <sender> · <scope> · <created> · expires <date>
//	and <n> more on <scope>: cairn memo-read --scope <word>
//
// The memo heading is split from the RIGHT, so a sender carrying ` · ` cannot move the scope. A
// scope that is not a valid name is `*`, like a header's.
func MemoScopes(s string) []string {
	var out []string
	for {
		i := strings.Index(s, MemoBlockOpen)
		if i < 0 {
			return out
		}
		s = s[i+len(MemoBlockOpen):]
		// The next OPEN bounds the block before the close is looked for: searching for the close
		// first would scan to the end of s from every unterminated open — quadratic.
		block := s
		if j := strings.Index(block, MemoBlockOpen); j >= 0 {
			block = block[:j]
		}
		if j := strings.Index(block, MemoBlockClose); j >= 0 {
			block = block[:j]
		}
		named := false
		for _, line := range strings.Split(block, "\n") {
			line = strings.TrimPrefix(strings.TrimRight(line, "\r"), "| ")
			sc, ok := memoLineScope(line)
			if !ok {
				continue
			}
			named = true
			if ValidScope(sc) {
				out = append(out, sc)
			} else {
				out = append(out, Star)
			}
		}
		if !named {
			out = append(out, Star)
		}
		s = s[len(block):]
	}
}

const memoMoreCommand = ": cairn memo-read --scope "

func memoLineScope(line string) (string, bool) {
	if strings.HasPrefix(line, "[m-") && strings.Contains(line, "] from ") {
		parts := strings.Split(line, " · ")
		if n := len(parts); n >= 4 && strings.HasPrefix(parts[n-1], "expires ") {
			return parts[n-3], true
		}
		return "", true // a heading this cannot read: still a memo, so `*`
	}
	if strings.HasPrefix(line, "and ") {
		if k := strings.Index(line, " more on "); k >= 0 {
			if e := strings.LastIndex(line, memoMoreCommand); e > k {
				return line[k+len(" more on ") : e], true
			}
		}
	}
	return "", false
}

// jsonStrings returns every string TOKEN of one JSON document — every key and every value,
// DUPLICATE members included (a map decode keeps only the last of two equal keys, and a header in
// the first would be lost) — or false when t is not exactly one valid document.
func jsonStrings(t string) ([]string, bool) {
	if !json.Valid([]byte(t)) {
		return nil, false
	}
	dec := json.NewDecoder(strings.NewReader(t))
	var out []string
	for {
		tok, err := dec.Token()
		if err != nil {
			return out, err == io.EOF
		}
		if s, ok := tok.(string); ok {
			out = append(out, s)
		}
	}
}

// ToolInput records one string of a tool's INPUT (a command line, a file path, an argument).
// Inputs are content too, so the header scan runs on them as well.
func (d *Deriver) ToolInput(s string) {
	d.Content(s)
	lower := strings.ToLower(s)
	if strings.Contains(lower, "cairn") || strings.Contains(lower, "subsystem-recall") {
		d.namesProgram = true
	}
	if strings.Contains(lower, "subsystem-store") {
		d.namesCache = true
	}
}

// Ledger folds one session's read-ledger file (JSON Lines, decision 3a). An absent file and an
// empty one are both zero records, and that is what F1 asks.
func (d *Deriver) Ledger(data []byte) {
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 64*1024), 1<<20)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		d.ledgerRecords++
		var rec struct {
			Scope *string `json:"scope"`
		}
		if err := json.Unmarshal(line, &rec); err != nil || rec.Scope == nil {
			d.ledger[Star] = true
			continue
		}
		if *rec.Scope == Star || !ValidScope(*rec.Scope) {
			d.ledger[Star] = true
			continue
		}
		d.ledger[*rec.Scope] = true
	}
	if sc.Err() != nil {
		d.ledger[Star] = true
	}
}

// LedgerUnreadable records that a ledger file EXISTS but could not be read. It fails closed: it
// counts as a record (so F1 cannot read it as "empty") naming `*`.
func (d *Deriver) LedgerUnreadable() {
	d.ledgerRecords++
	d.ledger[Star] = true
}

// Evidence is the part of a Deriver that must survive between capture runs, so a later run can
// feed only NEW records and still answer over the whole session. The ledger is not here: it is a
// file on the host and is re-read whole on every run.
type Evidence struct {
	Headers      []string `json:"headers,omitempty"`
	NamesProgram bool     `json:"names_program,omitempty"`
	NamesCache   bool     `json:"names_cache,omitempty"`
}

// Evidence returns what to persist.
func (d *Deriver) Evidence() Evidence {
	return Evidence{Headers: keys(d.headers), NamesProgram: d.namesProgram, NamesCache: d.namesCache}
}

// Restore folds persisted evidence back in. It only ADDS, like everything else here.
func (d *Deriver) Restore(e Evidence) {
	for _, h := range e.Headers {
		d.headers[h] = true
	}
	d.namesProgram = d.namesProgram || e.NamesProgram
	d.namesCache = d.namesCache || e.NamesCache
}

// Result is what one session's evidence adds to its visibility set.
type Result struct {
	Headers []string // R_header
	Ledger  []string // L
	F1, F2  bool
	// V is R_header ∪ L ∪ F, sorted, with `*` present when any part contributed it.
	V []string
}

// Result reads the accumulated evidence.
func (d *Deriver) Result() Result {
	r := Result{Headers: keys(d.headers), Ledger: keys(d.ledger)}
	r.F1 = d.namesProgram && d.ledgerRecords == 0
	r.F2 = d.namesCache
	all := map[string]bool{}
	for k := range d.headers {
		all[k] = true
	}
	for k := range d.ledger {
		all[k] = true
	}
	if r.F1 || r.F2 {
		all[Star] = true
	}
	r.V = keys(all)
	return r
}

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// ClaudeRecord feeds one decoded Claude Code record: a `tool_use` block's `input` is a tool
// input; every other string, anywhere in the record, is content.
func (d *Deriver) ClaudeRecord(rec map[string]any) {
	msg, _ := rec["message"].(map[string]any)
	blocks, _ := msg["content"].([]any)
	inputs := map[int]bool{}
	for i, b := range blocks {
		if bm, ok := b.(map[string]any); ok && bm["type"] == "tool_use" {
			inputs[i] = true
			eachString(bm["input"], d.ToolInput)
		}
	}
	for k, v := range rec {
		if k == "message" {
			continue
		}
		eachString(v, d.Content)
	}
	for k, v := range msg {
		if k == "content" {
			continue
		}
		eachString(v, d.Content)
	}
	if s, ok := msg["content"].(string); ok {
		d.Content(s)
	}
	for i, b := range blocks {
		if !inputs[i] {
			eachString(b, d.Content)
		} else if bm, ok := b.(map[string]any); ok {
			for k, v := range bm {
				if k != "input" {
					eachString(v, d.Content)
				}
			}
		}
	}
}

// OpencodePart feeds one decoded opencode part: a `tool` part's `state.input` is a tool input;
// everything else is content.
func (d *Deriver) OpencodePart(part map[string]any) {
	state, _ := part["state"].(map[string]any)
	if part["type"] == "tool" && state != nil {
		eachString(state["input"], d.ToolInput)
		for k, v := range state {
			if k != "input" {
				eachString(v, d.Content)
			}
		}
		for k, v := range part {
			if k != "state" {
				eachString(v, d.Content)
			}
		}
		return
	}
	eachString(part, d.Content)
}

func eachString(v any, f func(string)) {
	switch t := v.(type) {
	case string:
		f(t)
	case map[string]any:
		for _, e := range t {
			eachString(e, f)
		}
	case []any:
		for _, e := range t {
			eachString(e, f)
		}
	}
}
