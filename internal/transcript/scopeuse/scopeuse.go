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
//   - L: the scopes the client read ledger names (decision 3a). A ledger line that does not parse,
//     or names an invalid scope, contributes `*`.
//   - F1: a tool INPUT names `cairn` or `subsystem-recall` (case-insensitive) AND the ledger holds
//     ZERO records → `*`. It asks "is the ledger empty", never "which record is this call's".
//   - F2: a tool input names `subsystem-store` (the client cache root's directory name) → `*`,
//     because a file read from the cache never passes through the client and is never ledgered.
//
// ⚠ WHAT IT CANNOT SEE (threat T3): a session mixing a ledger-writing client with one that writes
// nothing (non-empty ledger, so F1 is silent); a file read of a store copy whose path lacks
// `subsystem-store`; a read that bypasses the client entirely.
package scopeuse

import (
	"bufio"
	"bytes"
	"encoding/json"
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
func HeaderScopes(s string) []string {
	var out []string
	for rest := s; ; {
		i := strings.Index(rest, HeaderMarker)
		if i < 0 {
			return out
		}
		line := rest[i+len(HeaderMarker):]
		if nl := strings.IndexByte(line, '\n'); nl >= 0 {
			line = line[:nl]
		}
		rest = rest[i+len(HeaderMarker):]
		fields := scopeField.FindAllStringSubmatch(line, -1)
		if len(fields) == 0 {
			out = append(out, Star)
			continue
		}
		for _, f := range fields {
			if ValidScope(f[1]) {
				out = append(out, f[1])
			} else {
				out = append(out, Star)
			}
		}
	}
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
func (d *Deriver) Content(s string) {
	for _, sc := range HeaderScopes(s) {
		d.headers[sc] = true
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
