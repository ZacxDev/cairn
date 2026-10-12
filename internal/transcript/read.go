package transcript

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/ZacxDev/cairn/internal/transcript/archive"
)

// 🔴 THE COLLAPSED READING (`claudedocs/plan-cairn-plugins.md`, decision 18; slice S4): a stored
// session as a reader sees it — turns of items — built from the stored records through the ONE
// classification table (`classify.go`).
//
// Storage keeps every byte (O9); this is a DISPLAY model and it drops none of them either: what it
// does not carry as an item it COUNTS (bookkeeping records, duplicate tool-result copies) and every
// item names the stored record it came from, so the raw-record view shows the bytes. The limits
// here are read-time limits only — [TurnsPerPage], [ChildItemCap] and [CollapsedBudget]; the
// per-item display limits (how much assistant text shows before "read more", how much of a tool
// result shows inline) are the renderer's, because they are about HTML.
//
// 🔴 IT CLASSIFIES UNITS, NOT RECORDS. A `user` record carrying a `tool_result` block is not a user
// message: its block is a tool result (collapsed), and only a `human-text` unit is ever shown in
// full. Reading the record's TYPE instead would render every tool result — up to hundreds of
// kilobytes — in full as something a human typed.

// TurnsPerPage is how many turns one page of a reading holds (decision 18: `?turn=` paging), so a
// very large session never renders in one response.
const TurnsPerPage = 50

// ChildItemCap bounds how many items of ONE child stream are carried inline under the tool call
// that spawned it; the rest are counted, and are in the raw-record view.
const ChildItemCap = 200

// CollapsedBudget bounds the bytes of COLLAPSED content one page carries — tool inputs and results,
// thinking, runtime blocks, everything inside a child stream. Past it a collapsed item keeps its
// summary and its raw-record link and drops its body ([Item.Elided]), so a page of fifty turns with
// a thousand tool calls cannot become a hundred-megabyte response.
//
// 🔴 USER MESSAGES AND ASSISTANT TEXT ARE NEVER CHARGED TO IT OR CUT BY IT: a `human-text` unit is
// shown in full whatever its length (O9), and assistant text is shown whole behind its own reveal.
const CollapsedBudget = 2 << 20

// Item is one thing a reading shows, in order.
type Item struct {
	// Class is the unit's class. A tool call and its result are ONE item of class [ToolCall]; a
	// result whose call is not in the same turn is its own item of class [ToolResult].
	Class Class
	// Stream and Seq name the stored record the item came from, for the raw-record view.
	Stream string
	Seq    int64
	// Text is the unit's text: what the human typed, what the assistant said, a thinking block, a
	// runtime block's content.
	Text string
	// Label says what a [Runtime], [Binary] or [Unknown] item is (`compaction summary`,
	// `image/png`, a record type).
	Label string
	// Bytes is the size of a [Binary] payload or an [Unknown] record.
	Bytes int
	// Elided is true when [CollapsedBudget] ran out: the body was dropped, the summary and the
	// raw-record link stay.
	Elided bool
	// Tool is set for [ToolCall] and [ToolResult] items.
	Tool *Tool
}

// Tool is one tool call with its result.
type Tool struct {
	// ID pairs a Claude Code `tool_use` with its `tool_result`. Never rendered.
	ID   string
	Name string
	// Input is the call's input as indented JSON.
	Input string
	// HasResult is false for a call whose result is not in the same turn (still running, or in a
	// record that never arrived).
	HasResult bool
	// Result is the result's text; ResultBytes its full size.
	Result      string
	ResultBytes int
	// ResultSeq is the stored record holding the result (the call's own record for opencode).
	ResultSeq int64
	// Status is the short outcome a summary line carries: "", `error`, `exit 1`, `running`.
	Status string
	// ResultElided is [Item.Elided] for the result's body.
	ResultElided bool
	// Binary is every binary payload attached to the result — shown as one line each.
	Binary []Blob
	// Child is the subagent or child session this call spawned, nil for every other call. A call
	// that ASKED for a subagent whose stream no result has named yet carries a Child with an empty
	// Stream — nothing to show.
	Child *Child
}

// Blob is one binary payload: what it is and how big.
type Blob struct {
	Type  string
	Bytes int
}

// Child is a child stream — a Claude Code subagent, an opencode child session — as carried under
// the tool call that spawned it.
type Child struct {
	Stream string
	// AgentType and Description are the spawning call's own words (`subagent_type`, `description`).
	AgentType, Description string
	Items                  []Item
	// More is how many further items the stream holds past [ChildItemCap].
	More                    int
	Bookkeeping, Duplicates int
}

// Turn is one human message and everything up to the next one.
type Turn struct {
	// N is the turn's number, from 1.
	N int
	// SeqFrom and SeqTo bound the main-stream records the turn was built from.
	SeqFrom, SeqTo int64
	Items          []Item
	// Bookkeeping and Duplicates count what the turn does NOT show as items: whole bookkeeping
	// records, and duplicate tool-result copies (a FIELD of a record, not a record).
	Bookkeeping, Duplicates int
}

// StreamRef names one stored stream and how many records it holds.
type StreamRef struct {
	Name    string
	Records int64
}

// Reading is one page of a session's collapsed reading.
type Reading struct {
	// Turns are the page's turns; TotalTurns is how many the session has; FirstTurn is the page's
	// first turn number (1, 51, 101, …).
	Turns      []Turn
	TotalTurns int
	FirstTurn  int
	// Streams is every stored stream, `main` first — so a child stream no tool call on this page
	// names is still reachable, through the raw-record view.
	Streams []StreamRef
}

// PageStart folds a requested turn number onto the first turn of its page: 1 for anything that is
// not a positive number.
func PageStart(turn int) int {
	if turn < 1 {
		return 1
	}
	return (turn-1)/TurnsPerPage*TurnsPerPage + 1
}

// Read builds the page of `meta`'s session that holds turn `turn`.
//
// ⚠ IT SCANS THE WHOLE MAIN STREAM ON EVERY CALL, one record in memory at a time: a turn boundary
// is a `human-text` unit, which only classification finds, and nothing indexes it yet (the skeleton
// of decision 17 is slice S8's). The RESPONSE is bounded by the page; the work is not. An opencode
// stream is held whole instead, because a part is stored once per VERSION and only the last one is
// the part.
func Read(a *archive.Archive, meta *archive.Meta, turn int) (Reading, error) {
	r := reader{a: a, meta: meta, budget: CollapsedBudget, entered: map[string]bool{}}
	out := Reading{FirstTurn: PageStart(turn)}
	for name, pos := range meta.Streams {
		out.Streams = append(out.Streams, StreamRef{Name: name, Records: pos.SeqTo})
	}
	sort.Slice(out.Streams, func(i, j int) bool {
		x, y := out.Streams[i].Name, out.Streams[j].Name
		if (x == mainStream) != (y == mainStream) {
			return x == mainStream
		}
		return x < y
	})
	w := &window{first: out.FirstTurn, last: out.FirstTurn + TurnsPerPage - 1}
	if err := r.stream(mainStream, w.add); err != nil {
		return Reading{}, err
	}
	w.close()
	for i := range w.turns {
		r.children(w.turns[i].Items, 0)
	}
	out.Turns, out.TotalTurns = w.turns, w.n
	return out, nil
}

const mainStream = "main"

// maxChildDepth bounds subagent-of-subagent nesting; a stream is also entered at most once per
// reading, so a cycle in what the records claim cannot recurse.
const maxChildDepth = 4

// reader carries what one reading shares: the archive, the session, the collapsed-content budget.
type reader struct {
	a       *archive.Archive
	meta    *archive.Meta
	budget  int
	entered map[string]bool
}

// piece is what ONE stored record contributes: its items, what it only counts, and whether it
// opens a turn.
type piece struct {
	seq         int64
	items       []Item
	bookkeeping int
	duplicates  int
	human       bool
}

// window collects the turns of one page out of a stream of pieces.
type window struct {
	first, last int
	n           int
	cur         *Turn
	curHuman    bool
	turns       []Turn
}

// add folds one record's piece into the turn it belongs to.
//
// A `human-text` unit opens a new turn — except the first one of a turn that has none yet, so the
// records a runtime writes before the first message (a mode, a hook result) share turn 1 with it
// rather than making a turn of their own.
func (w *window) add(p piece) {
	if w.n == 0 || (p.human && w.curHuman) {
		w.close()
		w.n++
		w.curHuman = false
		if w.n >= w.first && w.n <= w.last {
			w.cur = &Turn{N: w.n, SeqFrom: p.seq}
		}
	}
	w.curHuman = w.curHuman || p.human
	if w.cur == nil {
		return
	}
	w.cur.SeqTo = p.seq
	w.cur.Bookkeeping += p.bookkeeping
	w.cur.Duplicates += p.duplicates
	w.cur.Items = mergeResults(w.cur.Items, p.items)
}

func (w *window) close() {
	if w.cur != nil {
		w.turns = append(w.turns, *w.cur)
		w.cur = nil
	}
}

// mergeResults appends `more` to `items`, folding each Claude Code tool RESULT into the call it
// answers when that call is already in `items`. A result with no call there stays its own item —
// shown, never dropped.
func mergeResults(items, more []Item) []Item {
	for _, it := range more {
		if it.Class == ToolResult && it.Tool != nil && it.Tool.ID != "" {
			if call := findCall(items, it.Tool.ID); call != nil {
				call.HasResult = true
				call.Result, call.ResultBytes, call.ResultSeq = it.Tool.Result, it.Tool.ResultBytes, it.Seq
				call.Status, call.Binary, call.ResultElided = it.Tool.Status, it.Tool.Binary, it.Tool.ResultElided
				if it.Tool.Child != nil {
					// The RESULT names the stream; the CALL said what was asked for.
					if call.Child == nil {
						call.Child = &Child{}
					}
					call.Child.Stream = it.Tool.Child.Stream
				}
				continue
			}
		}
		items = append(items, it)
	}
	return items
}

func findCall(items []Item, id string) *Tool {
	for i := len(items) - 1; i >= 0; i-- {
		if items[i].Class == ToolCall && items[i].Tool != nil && items[i].Tool.ID == id {
			return items[i].Tool
		}
	}
	return nil
}

// stream feeds every record of one stored stream, as pieces, to `emit`.
func (r *reader) stream(name string, emit func(piece)) error {
	if r.meta.Runtime == "opencode" {
		return r.opencodeStream(name, emit)
	}
	return r.a.ScanStream(r.meta.Root, name, func(rec archive.StoredRecord) error {
		emit(r.claudePiece(name, rec))
		return nil
	})
}

// children fills in the child stream of every tool call in `items` that names one.
func (r *reader) children(items []Item, depth int) {
	for i := range items {
		t := items[i].Tool
		if t == nil || t.Child == nil {
			continue
		}
		c := t.Child
		if _, stored := r.meta.Streams[c.Stream]; c.Stream == "" || !stored || depth >= maxChildDepth || r.entered[c.Stream] {
			continue
		}
		r.entered[c.Stream] = true
		// A stream that cannot be read leaves the child with no items; its raw-record link still
		// stands, and the page is not refused over one subagent.
		_ = r.stream(c.Stream, func(p piece) {
			c.Bookkeeping += p.bookkeeping
			c.Duplicates += p.duplicates
			merged := mergeResults(c.Items, p.items)
			if len(merged) > ChildItemCap {
				c.More += len(merged) - ChildItemCap
				merged = merged[:ChildItemCap]
			}
			c.Items = merged
		})
		r.children(c.Items, depth+1)
	}
}

// charge takes `n` bytes of collapsed content from the page's budget and reports whether they fit.
func (r *reader) charge(n int) bool {
	if n > r.budget {
		r.budget = 0
		return false
	}
	r.budget -= n
	return true
}

// collapsed is an item whose body is collapsed content: charged to the budget, elided past it.
func (r *reader) collapsed(it Item) Item {
	n := len(it.Text)
	if it.Tool != nil {
		n += len(it.Tool.Input) + len(it.Tool.Result)
	}
	if !r.charge(n) {
		it.Elided, it.Text = true, ""
		if it.Tool != nil {
			it.Tool.Input, it.Tool.Result, it.Tool.ResultElided = "", "", it.Tool.HasResult
		}
	}
	return it
}

func decodeObject(raw []byte) map[string]any {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var m map[string]any
	if err := dec.Decode(&m); err != nil {
		return nil
	}
	return m
}

// claudePiece is one stored Claude Code record as a piece.
func (r *reader) claudePiece(stream string, sr archive.StoredRecord) piece {
	p := piece{seq: sr.Seq}
	rec := decodeObject(sr.Rec)
	if rec == nil {
		p.items = append(p.items, Item{Class: Unknown, Stream: stream, Seq: sr.Seq, Label: "not a JSON object", Bytes: len(sr.Rec)})
		return p
	}
	kind, _ := rec["type"].(string)
	// A subagent's stream is written by its parent agent, whatever one record says about itself.
	inChild := stream != mainStream
	var child *Child
	if dup, ok := rec["toolUseResult"].(map[string]any); ok {
		if id, _ := dup["agentId"].(string); id != "" {
			child = &Child{Stream: "subagent:" + id}
		}
	}
	for _, u := range ClassifyClaude(rec) {
		class := u.Class
		if class == HumanText && inChild {
			class = AgentPrompt
		}
		node := at(rec, u.Path)
		it := Item{Class: class, Stream: stream, Seq: sr.Seq}
		switch class {
		case HumanText:
			it.Text = unitText(node)
			p.human = true
			p.items = append(p.items, it)
		case AssistantText, AgentPrompt:
			it.Text = unitText(node)
			p.items = append(p.items, it)
		case Thinking:
			it.Text = unitText(node)
			p.items = append(p.items, r.collapsed(it))
		case Runtime:
			it.Label, it.Text = claudeRuntimeLabel(rec, kind), unitText(node)
			p.items = append(p.items, r.collapsed(it))
		case ToolCall:
			block, _ := node.(map[string]any)
			t := &Tool{Input: indent(block["input"])}
			t.ID, _ = block["id"].(string)
			t.Name, _ = block["name"].(string)
			if in, ok := block["input"].(map[string]any); ok {
				// What the call ASKS for; the stream is named by its result's record (below).
				if agentType, _ := in["subagent_type"].(string); agentType != "" {
					t.Child = &Child{AgentType: agentType}
					t.Child.Description, _ = in["description"].(string)
				}
			}
			it.Tool = t
			p.items = append(p.items, r.collapsed(it))
		case ToolResult:
			block, _ := node.(map[string]any)
			t := &Tool{HasResult: true, ResultSeq: sr.Seq, Child: child}
			t.ID, _ = block["tool_use_id"].(string)
			t.Result, t.Binary = resultContent(block["content"])
			t.ResultBytes = len(t.Result)
			if truthy(block["is_error"]) {
				t.Status = errorStatus(t.Result)
			}
			it.Tool = t
			p.items = append(p.items, r.collapsed(it))
		case Duplicate:
			p.duplicates++
		case Bookkeeping:
			p.bookkeeping++
		case Binary:
			// An image item inside a tool result is carried by that result ([resultContent]); the
			// base64 inside the duplicate field is part of the duplicate, which is counted.
		default:
			it.Class, it.Label, it.Bytes = Unknown, unknownLabel(rec, kind), len(sr.Rec)
			p.items = append(p.items, it)
		}
	}
	return p
}

func claudeRuntimeLabel(rec map[string]any, kind string) string {
	switch {
	case kind == "system":
		if sub, _ := rec["subtype"].(string); sub != "" {
			return strings.ReplaceAll(sub, "_", " ")
		}
		return "system"
	case truthy(rec["isCompactSummary"]):
		return "compaction summary"
	case truthy(rec["isMeta"]):
		return "meta"
	default:
		return "note beside a tool result"
	}
}

func unknownLabel(rec map[string]any, kind string) string {
	if marker, _ := rec["cairn_capture"].(string); marker != "" {
		// The capture agent's own placeholder (`omitted`, `not-json`): say so, by its own word.
		return "capture placeholder: " + marker
	}
	if kind == "" {
		return "untyped record"
	}
	return kind
}

// errorStatus reads a failed result's status line: `exit N` when the runtime wrote "Exit code N",
// `error` otherwise.
func errorStatus(result string) string {
	const lead = "Exit code "
	if rest, ok := strings.CutPrefix(result, lead); ok {
		digits := rest
		if i := strings.IndexFunc(rest, func(c rune) bool { return c < '0' || c > '9' }); i >= 0 {
			digits = rest[:i]
		}
		if digits != "" && len(digits) <= 3 {
			return "exit " + digits
		}
	}
	return "error"
}

// resultContent reads a `tool_result` block's content — a string, or a list of items — into its
// text and its binary payloads.
func resultContent(content any) (string, []Blob) {
	switch c := content.(type) {
	case string:
		return c, nil
	case []any:
		var text []string
		var blobs []Blob
		for _, it := range c {
			im, _ := it.(map[string]any)
			switch im["type"] {
			case "text":
				s, _ := im["text"].(string)
				text = append(text, s)
			case "image":
				src, _ := im["source"].(map[string]any)
				media, _ := src["media_type"].(string)
				data, _ := src["data"].(string)
				blobs = append(blobs, Blob{Type: orDefault(media, "image"), Bytes: base64.StdEncoding.DecodedLen(len(data))})
			case "tool_reference":
				name, _ := im["tool_name"].(string)
				text = append(text, "[tool reference: "+name+"]")
			default:
				text = append(text, indent(it))
			}
		}
		return strings.Join(text, "\n"), blobs
	case nil:
		return "", nil
	default:
		return indent(content), nil
	}
}

func orDefault(s, d string) string {
	if s == "" {
		return d
	}
	return s
}

// at follows a [Unit.Path] (`/message/content/2`) into a decoded record. "" is the record itself.
func at(rec map[string]any, path string) any {
	var node any = rec
	if path == "" {
		return node
	}
	for _, step := range strings.Split(strings.TrimPrefix(path, "/"), "/") {
		switch n := node.(type) {
		case map[string]any:
			node = n[step]
		case []any:
			i, err := strconv.Atoi(step)
			if err != nil || i < 0 || i >= len(n) {
				return nil
			}
			node = n[i]
		default:
			return nil
		}
	}
	return node
}

// unitText is the text of a unit, whatever shape carries it: a string, a block with `text` or
// `thinking`, a block whose `content` is a string or a list of such blocks.
func unitText(node any) string {
	switch n := node.(type) {
	case string:
		return n
	case []any:
		var parts []string
		for _, e := range n {
			if s := unitText(e); s != "" {
				parts = append(parts, s)
			}
		}
		return strings.Join(parts, "\n")
	case map[string]any:
		for _, key := range []string{"text", "thinking"} {
			if s, ok := n[key].(string); ok {
				return s
			}
		}
		if c, ok := n["content"]; ok {
			return unitText(c)
		}
	}
	return ""
}

// indent renders a JSON value for reading. A string is itself.
func indent(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return fmt.Sprint(v)
	}
	return strings.TrimRight(b.String(), "\n")
}

// opencodeStream feeds one stored opencode stream. Its records are export UNITS — the session's
// `info`, each message's `info`, each part — stored once per VERSION under `<key>@<digest>`; the
// reading is every key's LAST version, in the order the keys first appeared.
func (r *reader) opencodeStream(name string, emit func(piece)) error {
	type unit struct {
		seq int64
		rec []byte
	}
	var order []string
	last := map[string]unit{}
	err := r.a.ScanStream(r.meta.Root, name, func(rec archive.StoredRecord) error {
		key := rec.Src
		if i := strings.LastIndexByte(key, '@'); i >= 0 {
			key = key[:i]
		}
		if _, seen := last[key]; !seen {
			order = append(order, key)
		}
		last[key] = unit{seq: rec.Seq, rec: rec.Rec}
		return nil
	})
	if err != nil {
		return err
	}
	roles := map[string]string{}
	for _, key := range order {
		if id, ok := strings.CutPrefix(key, "msg:"); ok {
			if info := decodeObject(last[key].rec); info != nil {
				roles[id], _ = info["role"].(string)
			}
		}
	}
	inChild := name != mainStream
	for _, key := range order {
		u := last[key]
		p := piece{seq: u.seq}
		if !strings.HasPrefix(key, "prt:") {
			// The session's and each message's `info`: metadata about the conversation, not part of it.
			p.bookkeeping++
			emit(p)
			continue
		}
		part := decodeObject(u.rec)
		if part == nil {
			p.items = append(p.items, Item{Class: Unknown, Stream: name, Seq: u.seq, Label: "not a JSON object", Bytes: len(u.rec)})
			emit(p)
			continue
		}
		msgID, _ := part["messageID"].(string)
		kind, _ := part["type"].(string)
		it := Item{Stream: name, Seq: u.seq}
		units := ClassifyOpencodePart(roles[msgID], inChild, part)
		switch units[0].Class {
		case HumanText:
			it.Class, it.Text = HumanText, unitText(part)
			p.human = true
			p.items = append(p.items, it)
		case AssistantText, AgentPrompt:
			it.Class, it.Text = units[0].Class, unitText(part)
			p.items = append(p.items, it)
		case Thinking:
			it.Class, it.Text = Thinking, unitText(part)
			p.items = append(p.items, r.collapsed(it))
		case Runtime:
			it.Class, it.Label, it.Text = Runtime, kind, unitText(part)
			if kind == "text" {
				it.Label = "synthetic text"
			}
			p.items = append(p.items, r.collapsed(it))
		case ToolCall:
			it.Class, it.Tool = ToolCall, opencodeTool(part, u.seq)
			p.items = append(p.items, r.collapsed(it))
		case Bookkeeping:
			p.bookkeeping++
		case Binary:
			mime, _ := part["mime"].(string)
			url, _ := part["url"].(string)
			it.Class, it.Label, it.Bytes = Binary, orDefault(mime, "file"), dataURLBytes(url)
			p.items = append(p.items, it)
		default:
			it.Class, it.Label, it.Bytes = Unknown, orDefault(kind, "untyped part"), len(u.rec)
			p.items = append(p.items, it)
		}
		emit(p)
	}
	return nil
}

// opencodeTool reads a `tool` part: its call, its result and its attachments are ONE item.
func opencodeTool(part map[string]any, seq int64) *Tool {
	t := &Tool{ResultSeq: seq}
	t.Name, _ = part["tool"].(string)
	state, _ := part["state"].(map[string]any)
	t.Input = indent(state["input"])
	status, _ := state["status"].(string)
	md, _ := state["metadata"].(map[string]any)
	switch status {
	case "completed":
		t.HasResult = true
		t.Result, _ = state["output"].(string)
		if exit, ok := md["exit"].(json.Number); ok && exit.String() != "0" {
			t.Status = "exit " + exit.String()
		}
	case "error":
		t.HasResult = true
		t.Result, _ = state["error"].(string)
		t.Status = "error"
	default:
		t.Status = orDefault(status, "no result")
	}
	t.ResultBytes = len(t.Result)
	if atts, ok := state["attachments"].([]any); ok {
		for _, a := range atts {
			am, _ := a.(map[string]any)
			mime, _ := am["mime"].(string)
			url, _ := am["url"].(string)
			t.Binary = append(t.Binary, Blob{Type: orDefault(mime, "file"), Bytes: dataURLBytes(url)})
		}
	}
	if t.Name == "task" {
		if child, _ := md["sessionId"].(string); child != "" {
			in, _ := state["input"].(map[string]any)
			c := &Child{Stream: "child:" + child}
			c.AgentType, _ = in["subagent_type"].(string)
			c.Description, _ = in["description"].(string)
			t.Child = c
		}
	}
	return t
}

// dataURLBytes is the decoded size of a `data:…;base64,` URL's payload, or the URL's own length.
func dataURLBytes(url string) int {
	if i := strings.Index(url, ";base64,"); strings.HasPrefix(url, "data:") && i >= 0 {
		return base64.StdEncoding.DecodedLen(len(url) - i - len(";base64,"))
	}
	return len(url)
}
