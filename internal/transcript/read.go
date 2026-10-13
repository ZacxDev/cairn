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
// here are read-time limits only — [TurnsPerPage], [PageItemCap], [ChildItemCap] and
// [CollapsedBudget]; the
// per-item display limits (how much assistant text shows before "read more", how much of a tool
// result shows inline) are the renderer's, because they are about HTML.
//
// 🔴 IT CLASSIFIES UNITS, NOT RECORDS. A `user` record carrying a `tool_result` block is not a user
// message: its block is a tool result (collapsed), and only a `human-text` unit is ever shown in
// full. Reading the record's TYPE instead would render every tool result — up to hundreds of
// kilobytes — in full as something a human typed.

// TurnsPerPage is the most turns one page of a reading holds (decision 18: `?turn=` paging), so a
// very large session never renders in one response. A page holds FEWER when [PageItemCap] runs out
// first: it then ends before the first turn it cannot carry whole, and the next page starts there.
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
//
// 🔴 ONLY WHAT THE PAGE CARRIES IS CHARGED. A record is classified — and its collapsed body charged
// — before the reading knows whether it lands on this page, inside [PageItemCap], or inside
// [ChildItemCap]; whatever it then does not carry is refunded ([reader.refund]). Before #261 round
// 2 nothing was: every turn on an EARLIER page, and every child item past its cap, spent this
// page's budget, so page two of a large session showed elided bodies with room to spare.
const CollapsedBudget = 2 << 20

// PageItemCap bounds how many items one page carries — every item but a user message, counted
// across the page's turns AND the child streams nested in them, in reading order (a turn's own
// items, then its children, then the next turn).
//
// 🔴 THE PAGE ENDS WHERE THE ALLOWANCE RUNS OUT, AT A TURN BOUNDARY. A turn after the page's first
// that does not fit whole, children included, is not carried at all: the page stops before it and
// [Reading.NextTurn] names it, so `?turn=` reaches it whole on a page of its own. Round 2 carried
// such a turn's user message and COUNTED the rest — and since `?turn=` folded onto fixed
// fifty-turn pages, a turn counted that way could only ever be reached as raw records (#261 round
// 3: fifty turns of thirty calls showed turns 34–50 as "N more items").
//
// ⚠ ONE TURN LARGER THAN THE WHOLE ALLOWANCE IS STILL CUT, because no page could carry it: as the
// FIRST turn of its page it carries what fits, counts the rest ([Turn.More]) and names the first
// record holding one ([Turn.MoreFrom]) — those are reachable only as raw records. A child stream's
// items past its own [ChildItemCap] are counted in [Child.More] whichever turn it is in.
//
// 🔴 IT EXISTS BECAUSE THE TWO BOUNDS ABOVE DO NOT BOUND A PAGE: fifty turns bound the TURNS and the
// budget bounds collapsed BYTES, and one turn of 20,000 small tool calls fits both and rendered a
// 7.5 MB page (#261 round 2). Each item costs markup however small its body.
//
// A user message is never dropped by it and never counted toward it (O9): it is what a turn IS.
const PageItemCap = 1000

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
	// Input is the call's input as indented JSON; InputBytes is the input's size as compact JSON
	// (what the agent read API's skeleton reports — never the indented rendering's size), set only
	// when the reader measures ([reader.measure]).
	Input      string
	InputBytes int
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
	// More is how many of the turn's items [PageItemCap] left out — a tool call and its result are
	// one — and MoreFrom the first record holding one, so `MoreFrom`–`SeqTo` in the raw records
	// holds every one of them. Zero when the turn is carried whole, which every turn but a page's
	// FIRST is (a later one that does not fit starts the next page instead).
	More     int
	MoreFrom int64
}

// StreamRef names one stored stream and how many records it holds.
type StreamRef struct {
	Name    string
	Records int64
}

// Reading is one page of a session's collapsed reading.
type Reading struct {
	// Turns are the page's turns; TotalTurns is how many the session has; FirstTurn is the page's
	// first turn number — the turn `?turn=` asked for, clamped ([PageStart]).
	Turns      []Turn
	TotalTurns int
	FirstTurn  int
	// NextTurn is the first turn this page does not carry — where the next page starts — or 0 when
	// the page reaches the session's last turn (or is past it). It is FirstTurn + len(Turns): the
	// page ended there for [TurnsPerPage] or for [PageItemCap].
	NextTurn int
	// Streams is every stored stream, `main` first — so a child stream no tool call on this page
	// names is still reachable, through the raw-record view.
	Streams []StreamRef
}

// MaxTurn is the largest turn a page may be asked for. A larger request reads as this one — a
// page past the end — so no page arithmetic can overflow (#261's audit: `?turn=<MaxInt64>`
// rendered negative page links).
const MaxTurn = 1 << 30

// PageStart is the first turn of the page `?turn=` asks for: that turn itself — 1 for anything
// that is not a positive number, and a request past [MaxTurn] as [MaxTurn].
//
// ⚠ IT NO LONGER FOLDS ONTO A FIFTY-TURN GRID. It did until #261 round 3, and a page that ended
// early for [PageItemCap] then left the turns after it unreachable: every `?turn=` among them
// folded back onto the page that could not carry them. A page now starts at exactly the turn
// asked for, so [Reading.NextTurn] is always the start of a page of its own.
func PageStart(turn int) int {
	if turn < 1 {
		return 1
	}
	turn = min(turn, MaxTurn)
	return turn
}

// readSession builds the page of `meta`'s session that holds turn `turn`. UNEXPORTED: outside this
// package a reading is had only through [View.Read], which exists only where [Visible] said yes.
//
// ⚠ IT SCANS THE WHOLE MAIN STREAM ON EVERY CALL, one record in memory at a time: a turn boundary
// is a `human-text` unit, which only classification finds, and nothing indexes it yet (the skeleton
// of decision 17 is slice S8's). What the page CARRIES is bounded — [TurnsPerPage] turns,
// [PageItemCap] items, [CollapsedBudget] collapsed bytes — except user messages (O9) and assistant
// text, which are carried whole; the work of finding the page is not bounded. An opencode
// stream is held whole instead, because a part is stored once per VERSION and only the last one is
// the part.
func readSession(a *archive.Archive, meta *archive.Meta, turn int) (Reading, error) {
	r := &reader{a: a, meta: meta, budget: CollapsedBudget, room: PageItemCap, entered: map[string]bool{}, dropped: map[string]bool{}}
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
	w := &window{r: r, first: out.FirstTurn, last: out.FirstTurn + TurnsPerPage - 1}
	if err := r.stream(mainStream, w.add); err != nil {
		return Reading{}, err
	}
	w.close()
	out.Turns, out.TotalTurns = w.turns, w.n
	if next := out.FirstTurn + len(out.Turns); len(out.Turns) > 0 && next <= out.TotalTurns {
		out.NextTurn = next
	}
	return out, nil
}

const mainStream = "main"

// maxChildDepth bounds subagent-of-subagent nesting; a stream is also entered at most once per
// reading, so a cycle in what the records claim cannot recurse.
const maxChildDepth = 4

// reader carries what one reading shares: the archive, the session, the collapsed-content budget,
// the page's item allowance, and the tool calls that allowance left out (so their results, arriving
// later, are absorbed with them rather than counted or shown on their own).
type reader struct {
	a       *archive.Archive
	meta    *archive.Meta
	budget  int
	room    int
	entered map[string]bool
	dropped map[string]bool
	// roomOut is set when an item is left out because the PAGE allowance is spent (not a child's
	// own cap): the turn being built does not fit whole.
	roomOut bool
	// measure is set by the agent read API's skeleton ([View.Skeleton]): every piece then also
	// carries the size of each of its units by class, and whether it is a compaction boundary.
	// The collapsed reading leaves it false and pays nothing for it.
	measure bool
	// scanned is the stored bytes every stream scan of this reader has read, record by record.
	scanned int64
}

// sizedUnit is one classified unit and its size in bytes: the stored record's own length for a
// whole-record unit, the unit's value re-encoded as compact JSON for a unit inside one.
type sizedUnit struct {
	class Class
	bytes int
}

// piece is what ONE stored record contributes: its items, what it only counts, and whether it
// opens a turn.
type piece struct {
	seq         int64
	items       []Item
	bookkeeping int
	duplicates  int
	human       bool
	// units and compaction are filled only when [reader.measure] is set.
	units      []sizedUnit
	compaction bool
}

// window collects the turns of one page out of a stream of pieces.
type window struct {
	r           *reader
	first, last int
	n           int
	cur         *Turn
	curHuman    bool
	turns       []Turn
	// full is set once a turn did not fit the page's item allowance: the page ends before it.
	full bool
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
		if w.n >= w.first && w.n <= w.last && !w.full {
			w.cur = &Turn{N: w.n, SeqFrom: p.seq}
			w.r.roomOut = false
		}
	}
	w.curHuman = w.curHuman || p.human
	if w.cur == nil {
		// A turn on another page: nothing of it is carried, so none of it is charged.
		for _, it := range p.items {
			w.r.refund(it)
		}
		return
	}
	w.cur.SeqTo = p.seq
	w.cur.Bookkeeping += p.bookkeeping
	w.cur.Duplicates += p.duplicates
	for _, it := range p.items {
		var counted bool
		if w.cur.Items, counted = w.r.place(w.cur.Items, it, -1); counted {
			if w.cur.More == 0 {
				w.cur.MoreFrom = it.Seq
			}
			w.cur.More++
		}
	}
	if w.r.roomOut && len(w.turns) > 0 {
		// Not the page's first turn, and it no longer fits whole: the page ends before it. Waiting
		// for the turn to end would only spend reading on a turn this page will not carry.
		w.drop()
	}
}

// close finishes the turn being built: its child streams are read in (they spend the same
// allowance), and it is kept — unless it is not the page's first turn and it, children included,
// did not fit, in which case the page ends before it ([PageItemCap]).
func (w *window) close() {
	if w.cur == nil {
		return
	}
	w.r.children(w.cur.Items, 0)
	if w.r.roomOut && len(w.turns) > 0 {
		w.drop()
		return
	}
	w.turns = append(w.turns, *w.cur)
	w.cur = nil
}

// drop abandons the turn being built and ends the page before it: no later turn is carried on this
// page. What the dropped turn was charged is NOT refunded, and need not be: a turn's children are
// read when it closes, so nothing the page carries is charged after this point (a mutant that
// refunded nothing was measured to change no answer, so the refund walk is gone).
func (w *window) drop() {
	w.cur, w.full = nil, true
}

// place puts one item into `items` — the ONE rule for a turn and a child stream alike — and reports
// whether it was left out and must be COUNTED:
//
//   - a Claude Code tool RESULT whose call is in `items` folds into that call;
//   - a result whose call was left out goes with it — refunded, not counted again, never shown on
//     its own (a call and its result are one item);
//   - a user message is always carried;
//   - anything else is carried while the page's allowance ([PageItemCap]) and `limit` (a child's
//     [ChildItemCap]; -1 for none) both have room, and otherwise refunded and counted.
//
// A result with no call anywhere stays its own item.
func (r *reader) place(items []Item, it Item, limit int) ([]Item, bool) {
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
			return items, false
		}
		if r.dropped[it.Tool.ID] {
			r.refund(it)
			return items, false
		}
	}
	if it.Class != HumanText {
		if r.room <= 0 || (limit >= 0 && len(items) >= limit) {
			if r.room <= 0 {
				r.roomOut = true
			}
			r.refund(it)
			if it.Class == ToolCall && it.Tool != nil && it.Tool.ID != "" {
				r.dropped[it.Tool.ID] = true
			}
			return items, true
		}
		r.room--
	}
	return append(items, it), false
}

// refund returns to the budget what [reader.collapsed] charged for an item the page then did not
// carry. An elided item was charged nothing.
func (r *reader) refund(it Item) {
	if it.Elided {
		return
	}
	switch it.Class {
	case Thinking, Runtime, ToolCall, ToolResult:
		r.budget += collapsedSize(it)
	}
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
	return scanTo(r.a, r.meta, name, func(rec archive.StoredRecord) error {
		r.scanned += int64(len(rec.Rec))
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
		if _, stored := r.meta.Streams[c.Stream]; !stored {
			// A stream the records name and the store does not hold: nothing to nest, and no link to
			// render to a raw view that would answer its miss.
			c.Stream = ""
			continue
		}
		if depth >= maxChildDepth || r.entered[c.Stream] {
			continue
		}
		r.entered[c.Stream] = true
		// A stream that cannot be read leaves the child with no items; its raw-record link still
		// stands, and the page is not refused over one subagent.
		_ = r.stream(c.Stream, func(p piece) {
			c.Bookkeeping += p.bookkeeping
			c.Duplicates += p.duplicates
			for _, it := range p.items {
				var counted bool
				if c.Items, counted = r.place(c.Items, it, ChildItemCap); counted {
					c.More++
				}
			}
		})
		r.children(c.Items, depth+1)
	}
}

// charge takes `n` bytes of collapsed content from the page's budget and reports whether they fit.
// One item too large for what is left is elided and takes nothing, so a smaller one after it can
// still be shown.
func (r *reader) charge(n int) bool {
	if n > r.budget {
		return false
	}
	r.budget -= n
	return true
}

// collapsed is an item whose body is collapsed content: charged to the budget, elided past it.
func (r *reader) collapsed(it Item) Item {
	if !r.charge(collapsedSize(it)) {
		it.Elided, it.Text = true, ""
		if it.Tool != nil {
			it.Tool.Input, it.Tool.Result, it.Tool.ResultElided = "", "", it.Tool.HasResult
		}
	}
	return it
}

// collapsedSize is what [reader.collapsed] charges for an item, and so what [reader.refund] returns.
func collapsedSize(it Item) int {
	n := len(it.Text)
	if it.Tool != nil {
		n += len(it.Tool.Input) + len(it.Tool.Result)
	}
	return n
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
		if r.measure {
			p.units = append(p.units, sizedUnit{Unknown, len(sr.Rec)})
		}
		return p
	}
	kind, _ := rec["type"].(string)
	if r.measure {
		sub, _ := rec["subtype"].(string)
		p.compaction = (kind == "system" && sub == "compact_boundary") || truthy(rec["isCompactSummary"])
	}
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
		if r.measure {
			p.units = append(p.units, sizedUnit{class, unitBytes(sr.Rec, node, u.Path)})
		}
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
			if r.measure {
				t.InputBytes = compactBytes(block["input"])
			}
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

// compactBytes is the size of a decoded JSON value re-encoded compactly, HTML left unescaped — 0
// for an absent value.
func compactBytes(v any) int {
	if v == nil {
		return 0
	}
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return 0
	}
	return b.Len() - 1 // Encode appends a newline
}

// unitBytes is a unit's size: the whole stored record for the record itself (path ""), and the
// value at its path re-encoded compactly for a unit inside one.
func unitBytes(raw []byte, node any, path string) int {
	if path == "" {
		return len(raw)
	}
	return compactBytes(node)
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
	err := scanTo(r.a, r.meta, name, func(rec archive.StoredRecord) error {
		r.scanned += int64(len(rec.Rec))
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
			if r.measure {
				p.units = append(p.units, sizedUnit{Bookkeeping, len(u.rec)})
			}
			emit(p)
			continue
		}
		part := decodeObject(u.rec)
		if part == nil {
			p.items = append(p.items, Item{Class: Unknown, Stream: name, Seq: u.seq, Label: "not a JSON object", Bytes: len(u.rec)})
			if r.measure {
				p.units = append(p.units, sizedUnit{Unknown, len(u.rec)})
			}
			emit(p)
			continue
		}
		msgID, _ := part["messageID"].(string)
		kind, _ := part["type"].(string)
		it := Item{Stream: name, Seq: u.seq}
		units := ClassifyOpencodePart(roles[msgID], inChild, part)
		if r.measure {
			for _, un := range units {
				p.units = append(p.units, sizedUnit{un.Class, unitBytes(u.rec, at(part, un.Path), un.Path)})
			}
			p.compaction = kind == "compaction"
		}
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
			if r.measure {
				state, _ := part["state"].(map[string]any)
				it.Tool.InputBytes = compactBytes(state["input"])
			}
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
