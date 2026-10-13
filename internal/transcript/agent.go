package transcript

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/ZacxDev/cairn/internal/transcript/archive"
	"github.com/ZacxDev/cairn/internal/transcriptwire"
	"github.com/ZacxDev/cairn/internal/write"
)

// 🔴 THE AGENT READ API (`claudedocs/plan-cairn-plugins.md`, decision 17; slice S8): ONE read model
// over a stored session, for agents — a SKELETON (the stream tree, sizes by class, a turn index), a
// bounded PAGE of records (filters, ranges, two views) and ONE TOOL CALL (with a byte range over its
// result). Storage keeps every byte (O9); these are read-time selections over it, and nothing here
// drops a byte without saying so: a record the conversation view leaves out is a placeholder naming
// its class and size, and `view=raw` serves the stored bytes exactly.
//
// 🔴 EVERY ANSWER IS ASKED OF [Store.View] FIRST, AND EVERY MISS IS ONE BODY ([UnseenBody]). The
// model is methods on [View], so it cannot be reached without the predicate (decision 4): no
// transcript, a refused viewer, an id that is no session, a stream the session does not hold and a
// record or tool call that is not there all answer the same 404, and nothing says which scope hid a
// session (T10). [Store.Answer] is the ONE function both listeners serve it through — the browser
// surface (`internal/ui`) and the client listener agents hold a credential for (`internal/clientapi`,
// what `CAIRN_UI_URL` names) — so neither listener can disagree with the other about what a route
// answers.
//
// ⚠ IT IS SHARED BY THE PLUGIN API TOO, BY DESIGN AND NOT YET BY WIRING: [ParseView] and [View.Page]
// are what the plugin transcript route (S5, which answered `view=` with a 400 "until S8 adds views")
// and the summary plugin (S9, `view=conversation` by default) are meant to read through, so a plugin
// and an agent see one conversation view rather than two.

// The two views of [View.Page] (decision 17).
const (
	// ViewConversation is the DEFAULT: bookkeeping records are left out, the duplicate
	// `toolUseResult` field is stripped from the records that carry it, and each removal is a
	// placeholder ([Omission]) naming its class and size.
	ViewConversation = transcriptwire.ViewConversation
	// ViewRaw is the stored records, byte-exact.
	ViewRaw = transcriptwire.ViewRaw
)

// The read-time bounds (decision 17). Storage is untouched by every one of them.
const (
	// DefaultPageBytes and MaxPageBytes bound one page of records: `max_bytes` defaults to the
	// first and may not exceed the second. A page always carries its first record whole, however
	// large ([View.Page]).
	DefaultPageBytes = 64 << 10
	MaxPageBytes     = 1 << 20
	// DefaultRangeBytes and MaxRangeBytes bound the slice of a tool result one answer carries.
	DefaultRangeBytes = 64 << 10
	MaxRangeBytes     = 1 << 20
)

// The three read routes, by the last segment of their path. Each listener spells its own prefix
// (`/transcript/…` on the browser surface, `/client/v1/transcript/…` on the client listener).
const (
	RouteSkeleton = transcriptwire.RouteSkeleton
	RouteRecords  = transcriptwire.RouteRecords
	RouteTool     = transcriptwire.RouteTool
)

// ReadRoutes is the three routes, in a fixed order.
func ReadRoutes() []string { return []string{RouteSkeleton, RouteRecords, RouteTool} }

// The query parameters of the read routes, spelled once (`internal/transcriptwire`) for both
// listeners and the client.
const (
	ParamSession  = transcriptwire.ParamSession
	ParamStream   = transcriptwire.ParamStream
	ParamFromSeq  = transcriptwire.ParamFromSeq
	ParamToSeq    = transcriptwire.ParamToSeq
	ParamClass    = transcriptwire.ParamClass
	ParamRole     = transcriptwire.ParamRole
	ParamTool     = transcriptwire.ParamTool
	ParamView     = transcriptwire.ParamView
	ParamMaxBytes = transcriptwire.ParamMaxBytes
	ParamSeq      = transcriptwire.ParamSeq
	ParamID       = transcriptwire.ParamID
	ParamRange    = transcriptwire.ParamRange
)

// UnseenBody is the read API's ONE miss (T10): every reason a session, stream, record or tool call
// is not served. It is the client listener's own `not-found` body, byte for byte, so an agent
// reads one miss whichever listener it asked.
const UnseenBody = transcriptwire.UnseenBody

// ErrNotHeld is "the session the View holds has no such stream, record or tool call" — answered as
// [UnseenBody], never told apart from a refusal.
var ErrNotHeld = errors.New("transcript: not held")

// QueryError is a malformed read parameter: a 400 naming the PARAMETER, never echoing its value.
// It is decided from the query alone, before the predicate is asked, so it says nothing about any
// session.
type QueryError struct {
	Param string
	Why   string
}

func (e *QueryError) Error() string { return e.Param + ": " + e.Why }

// ParseView reads `view=`: [ViewConversation] when absent, [ViewRaw], or a [QueryError].
//
// 🔴 AN UNRECOGNISED VIEW IS REFUSED, NOT DEFAULTED — the opposite of the browser surface's
// `?view=` ruling, on purpose. There a typo renders the page a human can see is wrong; here a typo
// of `raw` would silently serve the conversation view to an agent that asked for the stored bytes
// and cannot tell the difference.
func ParseView(v string) (string, error) {
	switch v {
	case "", ViewConversation:
		return ViewConversation, nil
	case ViewRaw:
		return ViewRaw, nil
	}
	return "", &QueryError{ParamView, "must be conversation or raw"}
}

// RecordsQuery is one page request ([View.Page]).
type RecordsQuery struct {
	// Stream is a stored stream (`main` when the query names none).
	Stream string
	// FromSeq and ToSeq bound the records considered, inclusive; 0 is unbounded.
	FromSeq, ToSeq int64
	// Class, Role and Tool keep only the records that carry a unit of that class, whose role is
	// that role, or that call (or answer a call of) that tool. "" is no filter.
	Class Class
	Role  string
	Tool  string
	View  string
	// MaxBytes bounds the page's encoded records ([DefaultPageBytes] when the query names none).
	MaxBytes int
}

// ToolQuery is one tool call request ([View.Tool]).
type ToolQuery struct {
	Stream string
	// Seq is the stored record holding the call; ID picks one call when that record holds several
	// (the first when empty).
	Seq int64
	ID  string
	// Start and End are the byte range of the result (and of the duplicate copy) to carry, end
	// exclusive.
	Start, End int
}

// maxFilterBytes bounds a free-text filter value; a longer one cannot match anything a runtime
// writes, and refusing it keeps a request's cost bounded by its parameters.
const maxFilterBytes = 256

func streamParam(q url.Values) (string, error) {
	s := q.Get(ParamStream)
	if s == "" {
		return mainStream, nil
	}
	if len(s) > maxFilterBytes {
		return "", &QueryError{ParamStream, "is too long"}
	}
	return s, nil
}

// seqOf reads a record number: absent is 0, anything else must be a positive integer.
func seqOf(q url.Values, name string) (int64, error) {
	v := q.Get(name)
	if v == "" {
		return 0, nil
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil || n < 1 {
		return 0, &QueryError{name, "must be a positive integer"}
	}
	return n, nil
}

// ParseRecordsQuery reads decision 17's records parameters.
func ParseRecordsQuery(q url.Values) (RecordsQuery, error) {
	var rq RecordsQuery
	var err error
	if rq.Stream, err = streamParam(q); err != nil {
		return rq, err
	}
	if rq.FromSeq, err = seqOf(q, ParamFromSeq); err != nil {
		return rq, err
	}
	if rq.ToSeq, err = seqOf(q, ParamToSeq); err != nil {
		return rq, err
	}
	if rq.ToSeq > 0 && rq.ToSeq < rq.FromSeq {
		return rq, &QueryError{ParamToSeq, "is before from_seq"}
	}
	if c := q.Get(ParamClass); c != "" {
		if !slices.Contains(Classes(), Class(c)) {
			return rq, &QueryError{ParamClass, "is not a class of the classification table"}
		}
		rq.Class = Class(c)
	}
	rq.Role, rq.Tool = q.Get(ParamRole), q.Get(ParamTool)
	if len(rq.Role) > maxFilterBytes {
		return rq, &QueryError{ParamRole, "is too long"}
	}
	if len(rq.Tool) > maxFilterBytes {
		return rq, &QueryError{ParamTool, "is too long"}
	}
	if rq.View, err = ParseView(q.Get(ParamView)); err != nil {
		return rq, err
	}
	rq.MaxBytes = DefaultPageBytes
	if v := q.Get(ParamMaxBytes); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > MaxPageBytes {
			return rq, &QueryError{ParamMaxBytes, fmt.Sprintf("must be an integer from 1 to %d", MaxPageBytes)}
		}
		rq.MaxBytes = n
	}
	return rq, nil
}

// ParseToolQuery reads decision 17's tool parameters. `seq` is required.
func ParseToolQuery(q url.Values) (ToolQuery, error) {
	var tq ToolQuery
	var err error
	if tq.Stream, err = streamParam(q); err != nil {
		return tq, err
	}
	if tq.Seq, err = seqOf(q, ParamSeq); err != nil {
		return tq, err
	}
	if tq.Seq == 0 {
		return tq, &QueryError{ParamSeq, "is required"}
	}
	tq.ID = q.Get(ParamID)
	if len(tq.ID) > maxFilterBytes {
		return tq, &QueryError{ParamID, "is too long"}
	}
	tq.Start, tq.End = 0, DefaultRangeBytes
	if r := q.Get(ParamRange); r != "" {
		a, b, ok := strings.Cut(r, "-")
		start, err1 := strconv.Atoi(a)
		end, err2 := strconv.Atoi(b)
		if !ok || err1 != nil || err2 != nil || start < 0 || end <= start || end-start > MaxRangeBytes {
			return tq, &QueryError{ParamRange, fmt.Sprintf("must be <start>-<end>, 0 ≤ start < end, at most %d bytes", MaxRangeBytes)}
		}
		tq.Start, tq.End = start, end
	}
	return tq, nil
}

// Classes is the closed class set, in declaration order — what `class=` may name.
func Classes() []Class {
	return []Class{HumanText, AgentPrompt, AssistantText, Thinking, ToolCall, ToolResult, Duplicate, Runtime,
		Bookkeeping, Binary, ChildLink, Unknown}
}

// ---------------------------------------------------------------- the skeleton

// Skeleton is a stored session's shape with no content beyond tool NAMES (and what the spawning
// call said a subagent was for): the stream tree, what each stream holds by class, and a turn index.
type Skeleton struct {
	Session string           `json:"session"`
	Runtime string           `json:"runtime"`
	Host    string           `json:"host"`
	Streams []StreamSkeleton `json:"streams"`
}

// StreamSkeleton is one stored stream.
type StreamSkeleton struct {
	Stream string `json:"stream"`
	// Parent, ParentSeq, AgentType, Description and SpawnDepth place a child stream under the tool
	// call that spawned it: the stream holding the call, the call's record, the call's own words,
	// and how many spawns deep it is (`main` is 0). A stored stream no call names has no parent.
	Parent      string `json:"parent,omitempty"`
	ParentSeq   int64  `json:"parent_seq,omitempty"`
	AgentType   string `json:"agent_type,omitempty"`
	Description string `json:"description,omitempty"`
	SpawnDepth  int    `json:"spawn_depth"`
	// Records and Bytes are what is STORED: every record, every stored version of an opencode part.
	Records int64 `json:"records"`
	Bytes   int64 `json:"bytes"`
	// Classes counts units and their bytes by class. ⚠ A `binary` unit sits INSIDE a tool result
	// or a duplicate field, so its bytes are counted there too.
	Classes map[Class]ClassCount `json:"classes,omitempty"`
	// Compactions are the records that are compaction boundaries or summaries.
	Compactions []int64     `json:"compactions,omitempty"`
	Turns       []TurnIndex `json:"turns,omitempty"`
}

// ClassCount is how many units of one class a stream holds, and their bytes.
type ClassCount struct {
	Units int `json:"units"`
	Bytes int `json:"bytes"`
}

// TurnIndex is one turn: a human message and everything up to the next one (the collapsed
// reading's rule, [window.add]).
type TurnIndex struct {
	Turn    int   `json:"turn"`
	SeqFrom int64 `json:"seq_from"`
	SeqTo   int64 `json:"seq_to"`
	// UserBytes is the text of the turn's `human-text` units — and, in a child stream, of the
	// `agent-prompt` units its parent wrote; AssistantBytes the `assistant-text` units'.
	UserBytes      int         `json:"user_bytes"`
	AssistantBytes int         `json:"assistant_bytes"`
	ToolCalls      []ToolIndex `json:"tool_calls"`
}

// ToolIndex is one tool call in the turn index.
type ToolIndex struct {
	Seq         int64  `json:"seq"`
	ID          string `json:"id,omitempty"`
	Tool        string `json:"tool"`
	InputBytes  int    `json:"input_bytes"`
	ResultBytes int    `json:"result_bytes"`
	ResultSeq   int64  `json:"result_seq,omitempty"`
	IsError     bool   `json:"is_error"`
	ChildStream string `json:"child_stream,omitempty"`
}

// Skeleton builds the session's skeleton. It reads every stored stream once.
func (v *View) Skeleton() (Skeleton, error) {
	sk := Skeleton{Session: v.Meta.Root, Runtime: v.Meta.Runtime, Host: v.Meta.Host}
	type spawn struct {
		parent      string
		seq         int64
		agentType   string
		description string
	}
	spawns := map[string]spawn{}
	done := map[string]bool{}
	var queue []string
	if _, ok := v.Meta.Streams[mainStream]; ok {
		queue = append(queue, mainStream)
	}
	var rest []string
	for name := range v.Meta.Streams {
		if name != mainStream {
			rest = append(rest, name)
		}
	}
	slices.Sort(rest)
	for len(queue) > 0 || len(rest) > 0 {
		var name string
		if len(queue) > 0 {
			name, queue = queue[0], queue[1:]
		} else {
			name, rest = rest[0], rest[1:]
		}
		if done[name] {
			continue
		}
		done[name] = true
		ss, children, err := v.streamSkeleton(name)
		if err != nil {
			return Skeleton{}, err
		}
		for _, c := range children {
			if _, stored := v.Meta.Streams[c.Stream]; !stored || c.Stream == mainStream || c.Stream == name {
				continue
			}
			// 🔴 RECORDED EVEN FOR A STREAM ALREADY READ: a child that is not reachable from `main` yet
			// (its spawning result not uploaded) is read in sorted order, possibly BEFORE its parent,
			// and its placement must not depend on that order. The first spawn found wins.
			if _, placed := spawns[c.Stream]; !placed {
				spawns[c.Stream] = spawn{name, c.seq, c.AgentType, c.Description}
			}
			queue = append(queue, c.Stream)
		}
		sk.Streams = append(sk.Streams, ss)
	}
	// Placement after every stream is read; the depth follows parent links, bounded so a cycle the
	// records claim cannot loop.
	for i := range sk.Streams {
		ss := &sk.Streams[i]
		sp, ok := spawns[ss.Stream]
		if !ok {
			continue
		}
		ss.Parent, ss.ParentSeq, ss.AgentType, ss.Description = sp.parent, sp.seq, sp.agentType, sp.description
		for at, n := ss.Stream, 0; n <= len(sk.Streams); n++ {
			up, ok := spawns[at]
			if !ok {
				break
			}
			ss.SpawnDepth++
			at = up.parent
		}
	}
	return sk, nil
}

// spawned is a child stream a stream's tool calls named, and the call's record.
type spawned struct {
	Child
	seq int64
}

// streamSkeleton is one stream's skeleton and the child streams its tool calls spawned, in order.
func (v *View) streamSkeleton(name string) (StreamSkeleton, []spawned, error) {
	ss := StreamSkeleton{Stream: name, Records: v.Meta.Streams[name].SeqTo}
	if name == archive.LedgerStream {
		// The client read ledger is not a conversation: counted, never classified.
		err := scanTo(v.a, v.Meta, name, func(rec Record) error {
			ss.Bytes += int64(len(rec.Rec))
			return nil
		})
		return ss, nil, err
	}
	r := &reader{a: v.a, meta: v.Meta, budget: math.MaxInt, room: math.MaxInt, entered: map[string]bool{},
		dropped: map[string]bool{}, measure: true}
	ss.Classes = map[Class]ClassCount{}
	type place struct{ turn, call int }
	calls := map[string]place{}
	asked := map[string]Child{}
	var children []spawned
	curHuman := false
	err := r.stream(name, func(p piece) {
		for _, u := range p.units {
			c := ss.Classes[u.class]
			c.Units++
			c.Bytes += u.bytes
			ss.Classes[u.class] = c
		}
		if p.compaction {
			ss.Compactions = append(ss.Compactions, p.seq)
		}
		if len(ss.Turns) == 0 || (p.human && curHuman) {
			ss.Turns = append(ss.Turns, TurnIndex{Turn: len(ss.Turns) + 1, SeqFrom: p.seq, ToolCalls: []ToolIndex{}})
			curHuman = false
		}
		curHuman = curHuman || p.human
		t := &ss.Turns[len(ss.Turns)-1]
		t.SeqTo = p.seq
		for _, it := range p.items {
			switch it.Class {
			case HumanText, AgentPrompt:
				t.UserBytes += len(it.Text)
			case AssistantText:
				t.AssistantBytes += len(it.Text)
			case ToolCall:
				tool := it.Tool
				ti := ToolIndex{Seq: it.Seq, ID: tool.ID, Tool: tool.Name, InputBytes: tool.InputBytes}
				if tool.HasResult {
					// opencode: a `tool` part is its call and its result.
					ti.ResultBytes, ti.ResultSeq, ti.IsError = tool.ResultBytes, tool.ResultSeq, failed(tool.Status)
				}
				if tool.Child != nil {
					if tool.Child.Stream != "" {
						ti.ChildStream = tool.Child.Stream
						children = append(children, spawned{*tool.Child, it.Seq})
					} else if tool.ID != "" {
						asked[tool.ID] = *tool.Child
					}
				}
				t.ToolCalls = append(t.ToolCalls, ti)
				if tool.ID != "" {
					calls[tool.ID] = place{len(ss.Turns) - 1, len(t.ToolCalls) - 1}
				}
			case ToolResult:
				tool := it.Tool
				where, ok := calls[tool.ID]
				if tool.ID == "" || !ok {
					continue // a result whose call is not in this stream: not a call to index
				}
				ti := &ss.Turns[where.turn].ToolCalls[where.call]
				ti.ResultBytes, ti.ResultSeq, ti.IsError = tool.ResultBytes, it.Seq, failed(tool.Status)
				if tool.Child != nil && tool.Child.Stream != "" {
					ti.ChildStream = tool.Child.Stream
					c := asked[tool.ID]
					c.Stream = tool.Child.Stream
					children = append(children, spawned{c, ti.Seq})
				}
			}
		}
	})
	ss.Bytes = r.scanned
	if len(ss.Classes) == 0 {
		ss.Classes = nil
	}
	return ss, children, err
}

// failed reads a tool's short status as "did it fail": an error result, or a non-zero exit.
func failed(status string) bool {
	return status == "error" || strings.HasPrefix(status, "exit ")
}

// ---------------------------------------------------------------- records

// Omission is one thing the conversation view left out of a record — or the whole record — named by
// its class, where it was (`""` for the whole record) and its size. `view=raw` has it.
type Omission struct {
	Class Class  `json:"class"`
	Path  string `json:"path"`
	Bytes int    `json:"bytes"`
	// Label says what a whole omitted record was (its record or part type).
	Label string `json:"label,omitempty"`
}

// PageRecord is one record of a page: its stored bytes (or the conversation view's reading of
// them), or — when the whole record was left out — nothing but its [Omission].
type PageRecord struct {
	Seq     int64
	Src     string
	Rec     []byte
	Omitted []Omission
}

// Page is one bounded page of a stream's records.
type Page struct {
	Session, Stream, View string
	Records               []PageRecord
	// NextSeq is where the next page starts: the first record this page did not carry, or — when
	// Done — the record after the requested range, so a reader can keep it as a cursor and ask
	// again once more has been stored.
	NextSeq int64
	// Done is true when the page reached the requested `to_seq` or the stream's end.
	Done bool
	// SeqTo is the stream's last record, as the View was decided over.
	SeqTo int64
}

// Page builds one page of a stream's records.
//
// 🔴 THE PAGE IS BOUNDED BY `MaxBytes` OF ENCODED RECORDS, AND ITS FIRST RECORD IS ALWAYS CARRIED
// WHOLE. A record larger than the bound is a page alone, with NextSeq naming the record after it —
// never split, never skipped (a record can be several megabytes; `archive.MaxAssembledBytes`).
func (v *View) Page(q RecordsQuery) (Page, error) {
	pos, ok := v.Meta.Streams[q.Stream]
	if !ok {
		return Page{}, ErrNotHeld
	}
	if q.View == "" {
		q.View = ViewConversation
	}
	if q.MaxBytes <= 0 {
		q.MaxBytes = DefaultPageBytes
	}
	p := Page{Session: v.Meta.Root, Stream: q.Stream, View: q.View, SeqTo: pos.SeqTo}
	from, to := max(q.FromSeq, 1), pos.SeqTo
	if q.ToSeq > 0 && q.ToSeq < to {
		to = q.ToSeq
	}
	opencode := v.Meta.Runtime == "opencode"
	inChild := q.Stream != mainStream
	var roles map[string]string
	if opencode && (q.Class != "" || q.Role != "") {
		var err error
		if roles, err = v.opencodeRoles(q.Stream); err != nil {
			return Page{}, err
		}
	}
	decode := q.View == ViewConversation || q.Class != "" || q.Role != "" || q.Tool != ""
	callNames := map[string]string{}
	used := 0
	stopped := false
	err := v.Records(q.Stream, func(rec Record) error {
		if rec.Seq > to {
			return StopRecords
		}
		var obj map[string]any
		if decode {
			obj = decodeObject(rec.Rec)
		}
		if !opencode && q.Tool != "" && obj != nil {
			// Remember every call's tool by id — those BEFORE the page too — so a result can be
			// matched to the tool it answers.
			for _, b := range contentBlocks(obj) {
				if b["type"] == "tool_use" {
					id, _ := b["id"].(string)
					name, _ := b["name"].(string)
					callNames[id] = name
				}
			}
		}
		if rec.Seq < from {
			return nil
		}
		if !matches(q, rec, obj, opencode, inChild, roles, callNames) {
			return nil
		}
		pr := PageRecord{Seq: rec.Seq, Src: rec.Src, Rec: rec.Rec}
		if q.View == ViewConversation {
			pr = conversation(pr, obj, opencode)
		}
		size := len(encodeRecord(pr))
		if len(p.Records) > 0 && used+size > q.MaxBytes {
			p.NextSeq, stopped = rec.Seq, true
			return StopRecords
		}
		used += size
		p.Records = append(p.Records, pr)
		return nil
	})
	if err != nil {
		return Page{}, err
	}
	if !stopped {
		p.Done, p.NextSeq = true, max(from, to+1)
	}
	return p, nil
}

// contentBlocks is a Claude Code record's `message.content` blocks, or nil.
func contentBlocks(obj map[string]any) []map[string]any {
	msg, _ := obj["message"].(map[string]any)
	list, _ := msg["content"].([]any)
	var out []map[string]any
	for _, b := range list {
		if bm, ok := b.(map[string]any); ok {
			out = append(out, bm)
		}
	}
	return out
}

// opencodeKey is an opencode record's unit key: its source without the `@<digest>` version.
func opencodeKey(src string) string {
	if i := strings.LastIndexByte(src, '@'); i >= 0 {
		return src[:i]
	}
	return src
}

// opencodeRoles maps each message id of an opencode stream to its role, from the messages' `info`
// records (their LAST stored version).
func (v *View) opencodeRoles(stream string) (map[string]string, error) {
	roles := map[string]string{}
	err := v.Records(stream, func(rec Record) error {
		if id, ok := strings.CutPrefix(opencodeKey(rec.Src), "msg:"); ok {
			if info := decodeObject(rec.Rec); info != nil {
				roles[id], _ = info["role"].(string)
			}
		}
		return nil
	})
	return roles, err
}

// unitClasses is the class of every unit of one stored record — the ONE classification table, with
// the collapsed reading's child-stream rule (human text in a child stream is an agent's prompt).
func unitClasses(rec Record, obj map[string]any, opencode, inChild bool, roles map[string]string) []Class {
	if obj == nil {
		return []Class{Unknown}
	}
	var units []Unit
	if opencode {
		key := opencodeKey(rec.Src)
		if !strings.HasPrefix(key, "prt:") {
			return []Class{Bookkeeping}
		}
		msgID, _ := obj["messageID"].(string)
		units = ClassifyOpencodePart(roles[msgID], inChild, obj)
	} else {
		units = ClassifyClaude(obj)
	}
	out := make([]Class, 0, len(units))
	for _, u := range units {
		c := u.Class
		if c == HumanText && inChild {
			c = AgentPrompt
		}
		out = append(out, c)
	}
	return out
}

// roleOf is a record's role: a Claude Code record's `message.role` (its `type` when it has no
// message), an opencode part's message's role, an opencode message `info`'s own role.
func roleOf(rec Record, obj map[string]any, opencode bool, roles map[string]string) string {
	if obj == nil {
		return ""
	}
	if opencode {
		key := opencodeKey(rec.Src)
		if strings.HasPrefix(key, "msg:") {
			role, _ := obj["role"].(string)
			return role
		}
		msgID, _ := obj["messageID"].(string)
		return roles[msgID]
	}
	if msg, ok := obj["message"].(map[string]any); ok {
		if role, ok := msg["role"].(string); ok {
			return role
		}
	}
	kind, _ := obj["type"].(string)
	return kind
}

// callsTool is whether a record calls `tool` — or, for Claude Code, answers a call of it.
func callsTool(obj map[string]any, opencode bool, tool string, callNames map[string]string) bool {
	if obj == nil {
		return false
	}
	if opencode {
		return obj["type"] == "tool" && obj["tool"] == tool
	}
	for _, b := range contentBlocks(obj) {
		switch b["type"] {
		case "tool_use":
			if b["name"] == tool {
				return true
			}
		case "tool_result":
			if id, _ := b["tool_use_id"].(string); id != "" && callNames[id] == tool {
				return true
			}
		}
	}
	return false
}

func matches(q RecordsQuery, rec Record, obj map[string]any, opencode, inChild bool, roles, callNames map[string]string) bool {
	if q.Class != "" && !slices.Contains(unitClasses(rec, obj, opencode, inChild, roles), q.Class) {
		return false
	}
	if q.Role != "" && roleOf(rec, obj, opencode, roles) != q.Role {
		return false
	}
	if q.Tool != "" && !callsTool(obj, opencode, q.Tool, callNames) {
		return false
	}
	return true
}

// conversation is decision 17's conversation view of one record: a bookkeeping record becomes a
// placeholder, a record carrying the duplicate `toolUseResult` field loses that field to one, and
// anything else is the stored bytes.
func conversation(pr PageRecord, obj map[string]any, opencode bool) PageRecord {
	if obj == nil {
		return pr
	}
	if opencode {
		key := opencodeKey(pr.Src)
		kind, _ := obj["type"].(string)
		label := ""
		switch {
		case strings.HasPrefix(key, "msg:"):
			label = "message info"
		case !strings.HasPrefix(key, "prt:"):
			label = "session info"
		case OpencodeParts[kind].Class == Bookkeeping:
			label = kind
		default:
			return pr
		}
		return PageRecord{Seq: pr.Seq, Src: pr.Src, Omitted: []Omission{{Bookkeeping, "", len(pr.Rec), label}}}
	}
	kind, _ := obj["type"].(string)
	if ClaudeRecords[kind].Class == Bookkeeping {
		return PageRecord{Seq: pr.Seq, Src: pr.Src, Omitted: []Omission{{Bookkeeping, "", len(pr.Rec), kind}}}
	}
	if _, ok := obj["toolUseResult"]; ok {
		if out, _, total, ok := dropKey(pr.Rec, "toolUseResult"); ok {
			pr.Rec = out
			pr.Omitted = []Omission{{Class: Duplicate, Path: "/toolUseResult", Bytes: total}}
		}
	}
	return pr
}

// dropKey removes a top-level key — every occurrence of it — from a JSON object and returns the
// object and the removed value's bytes. Every other member keeps its VALUE's stored bytes and its
// order; its KEY is re-encoded (`"\u0074ype"` comes out as `"type"`), and the separators between
// members are rewritten.
func dropKey(raw []byte, key string) (out, removed []byte, total int, ok bool) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		return nil, nil, 0, false
	}
	var b bytes.Buffer
	b.WriteByte('{')
	first := true
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return nil, nil, 0, false
		}
		k, isKey := tok.(string)
		var v json.RawMessage
		if !isKey || dec.Decode(&v) != nil {
			return nil, nil, 0, false
		}
		if k == key {
			// EVERY occurrence goes: a record carrying the key twice must not keep the second copy
			// while the omission says it was removed. `removed` is the LAST one — the copy Go's
			// decoder (and so every reader here) sees.
			removed, total = v, total+len(v)
			continue
		}
		if !first {
			b.WriteByte(',')
		}
		first = false
		b.Write(jsonStr(k))
		b.WriteByte(':')
		b.Write(v)
	}
	b.WriteByte('}')
	return b.Bytes(), removed, total, removed != nil
}

// jsonString encodes a string as JSON, HTML left unescaped.
func jsonStr(s string) []byte {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(s)
	return bytes.TrimSuffix(b.Bytes(), []byte("\n"))
}

// encodeRecord is one page record's JSON, built by hand: `{"seq":N,"src":"…","rec":<stored bytes>}`.
//
// 🔴 BY HAND, BECAUSE `encoding/json` WOULD CHANGE THE STORED BYTES: it compacts a
// `json.RawMessage` and escapes `<`, `>` and `&` inside it, so `view=raw` would not be byte-exact.
func encodeRecord(pr PageRecord) []byte {
	var b bytes.Buffer
	b.WriteString(`{"seq":`)
	b.WriteString(strconv.FormatInt(pr.Seq, 10))
	b.WriteString(`,"src":`)
	b.Write(jsonStr(pr.Src))
	if pr.Rec != nil {
		b.WriteString(`,"rec":`)
		b.Write(pr.Rec)
	}
	if len(pr.Omitted) > 0 {
		b.WriteString(`,"omitted":`)
		b.Write(marshal(pr.Omitted))
	}
	b.WriteByte('}')
	return b.Bytes()
}

// JSON is the page as the read route answers it.
func (p Page) JSON() []byte {
	var b bytes.Buffer
	b.WriteString(`{"session":`)
	b.Write(jsonStr(p.Session))
	b.WriteString(`,"stream":`)
	b.Write(jsonStr(p.Stream))
	b.WriteString(`,"view":`)
	b.Write(jsonStr(p.View))
	b.WriteString(`,"records":[`)
	for i, pr := range p.Records {
		if i > 0 {
			b.WriteByte(',')
		}
		b.Write(encodeRecord(pr))
	}
	b.WriteString(`],"next_seq":`)
	b.WriteString(strconv.FormatInt(p.NextSeq, 10))
	b.WriteString(`,"done":`)
	b.WriteString(strconv.FormatBool(p.Done))
	b.WriteString(`,"seq_to":`)
	b.WriteString(strconv.FormatInt(p.SeqTo, 10))
	b.WriteString("}\n")
	return b.Bytes()
}

// marshal is `json.Marshal` with HTML left unescaped and no trailing newline.
func marshal(v any) []byte {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return []byte("null")
	}
	return bytes.TrimSuffix(b.Bytes(), []byte("\n"))
}

// ---------------------------------------------------------------- one tool call

// ToolAnswer is one tool call: the call, its result and its duplicate copy, each long text cut to
// the requested byte range.
type ToolAnswer struct {
	Session     string          `json:"session"`
	Stream      string          `json:"stream"`
	Seq         int64           `json:"seq"`
	ID          string          `json:"id,omitempty"`
	Tool        string          `json:"tool"`
	Input       json.RawMessage `json:"input"`
	Status      string          `json:"status,omitempty"`
	Result      *ToolText       `json:"result"`
	Duplicate   *ToolText       `json:"duplicate"`
	Binary      []ToolBlob      `json:"binary,omitempty"`
	ChildStream string          `json:"child_stream,omitempty"`
}

// ToolText is one long text of a tool call: where it is stored, its whole size, and the slice of it
// this answer carries. Range is `[start, end)` in bytes, moved inward to character boundaries.
type ToolText struct {
	Seq     int64  `json:"seq"`
	IsError bool   `json:"is_error,omitempty"`
	Bytes   int    `json:"bytes"`
	Range   [2]int `json:"range"`
	Text    string `json:"text"`
}

// ToolBlob is one binary payload attached to a result: its type and size. The bytes are in the
// raw records (O12).
type ToolBlob struct {
	Type  string `json:"type"`
	Bytes int    `json:"bytes"`
}

// cut is `s[start:end]` clamped to `s` and moved inward to character boundaries.
func cut(s string, start, end int) (string, [2]int) {
	start, end = min(start, len(s)), min(end, len(s))
	for start < len(s) && !utf8.RuneStart(s[start]) {
		start++
	}
	for end > start && end < len(s) && !utf8.RuneStart(s[end]) {
		end--
	}
	end = max(end, start)
	return s[start:end], [2]int{start, end}
}

func toolText(seq int64, isError bool, s string, q ToolQuery) *ToolText {
	text, rng := cut(s, q.Start, q.End)
	return &ToolText{Seq: seq, IsError: isError, Bytes: len(s), Range: rng, Text: text}
}

// Tool answers one tool call.
func (v *View) Tool(q ToolQuery) (ToolAnswer, error) {
	if _, ok := v.Meta.Streams[q.Stream]; !ok {
		return ToolAnswer{}, ErrNotHeld
	}
	ans := ToolAnswer{Session: v.Meta.Root, Stream: q.Stream, Seq: q.Seq}
	found, answered := false, false
	opencode := v.Meta.Runtime == "opencode"
	err := v.Records(q.Stream, func(rec Record) error {
		if rec.Seq < q.Seq {
			return nil
		}
		obj := decodeObject(rec.Rec)
		if !found {
			if rec.Seq != q.Seq || obj == nil {
				return StopRecords
			}
			if opencode {
				if obj["type"] != "tool" {
					return StopRecords
				}
				t := opencodeTool(obj, rec.Seq)
				ans.ID, _ = obj["callID"].(string)
				if q.ID != "" && q.ID != ans.ID {
					return StopRecords
				}
				state, _ := obj["state"].(map[string]any)
				found, answered = true, true
				ans.Tool, ans.Input, ans.Status = t.Name, marshal(state["input"]), t.Status
				if t.HasResult {
					ans.Result = toolText(rec.Seq, failed(t.Status), t.Result, q)
				}
				for _, b := range t.Binary {
					ans.Binary = append(ans.Binary, ToolBlob(b))
				}
				if t.Child != nil {
					ans.ChildStream = t.Child.Stream
				}
				return StopRecords
			}
			for _, b := range contentBlocks(obj) {
				id, _ := b["id"].(string)
				if b["type"] != "tool_use" || (q.ID != "" && id != q.ID) {
					continue
				}
				found = true
				ans.ID, _ = b["id"].(string)
				ans.Tool, _ = b["name"].(string)
				ans.Input = marshal(b["input"])
				break
			}
			if !found || ans.ID == "" {
				return StopRecords // no call here, or a call nothing can answer
			}
			return nil
		}
		for _, b := range contentBlocks(obj) {
			if b["type"] != "tool_result" || b["tool_use_id"] != ans.ID {
				continue
			}
			answered = true
			text, blobs := resultContent(b["content"])
			isErr := truthy(b["is_error"])
			if isErr {
				ans.Status = errorStatus(text)
			}
			ans.Result = toolText(rec.Seq, isErr, text, q)
			for _, bl := range blobs {
				ans.Binary = append(ans.Binary, ToolBlob(bl))
			}
			if dup, ok := obj["toolUseResult"]; ok {
				if dm, ok := dup.(map[string]any); ok {
					if id, _ := dm["agentId"].(string); id != "" {
						ans.ChildStream = "subagent:" + id
					}
				}
				if _, raw, _, ok := dropKey(rec.Rec, "toolUseResult"); ok {
					ans.Duplicate = toolText(rec.Seq, false, string(raw), q)
				}
			}
			return StopRecords
		}
		return nil
	})
	if err != nil {
		return ToolAnswer{}, err
	}
	if !found {
		return ToolAnswer{}, ErrNotHeld
	}
	if !answered {
		ans.Status = "no result"
	}
	return ans, nil
}

// ---------------------------------------------------------------- the one entry point

// Answer serves one read route — [RouteSkeleton], [RouteRecords] or [RouteTool] — for `viewer`:
// the HTTP status and the body, and, for a 500, the error a listener logs (the body says nothing).
//
// The order is the point: the session id is REQUIRED (400 without one — a request naming no session
// asked about nothing); the route's parameters are parsed (400 naming the parameter, decided from
// the query alone); then the predicate ([Store.View]) is asked, and every miss after that is
// [UnseenBody].
func (s *Store) Answer(viewer Viewer, route string, q url.Values) (int, []byte, error) {
	session := q.Get(ParamSession)
	if session == "" {
		return badRequest(&QueryError{ParamSession, "is required"})
	}
	var rq RecordsQuery
	var tq ToolQuery
	var err error
	switch route {
	case RouteSkeleton:
	case RouteRecords:
		rq, err = ParseRecordsQuery(q)
	case RouteTool:
		tq, err = ParseToolQuery(q)
	default:
		return http.StatusNotFound, []byte(UnseenBody), nil
	}
	if err != nil {
		return badRequest(err)
	}
	if !write.SessionComponent.MatchString(session) {
		return http.StatusNotFound, []byte(UnseenBody), nil
	}
	view, err := s.View(viewer, session)
	if err != nil {
		return http.StatusInternalServerError, []byte(`{"error":"internal"}` + "\n"), err
	}
	if view == nil {
		return http.StatusNotFound, []byte(UnseenBody), nil
	}
	var body []byte
	switch route {
	case RouteSkeleton:
		var sk Skeleton
		if sk, err = view.Skeleton(); err == nil {
			body = append(marshal(sk), '\n')
		}
	case RouteRecords:
		var p Page
		if p, err = view.Page(rq); err == nil {
			body = p.JSON()
		}
	case RouteTool:
		var t ToolAnswer
		if t, err = view.Tool(tq); err == nil {
			body = append(marshal(t), '\n')
		}
	}
	switch {
	case errors.Is(err, ErrNotHeld):
		return http.StatusNotFound, []byte(UnseenBody), nil
	case err != nil:
		return http.StatusInternalServerError, []byte(`{"error":"internal"}` + "\n"), err
	}
	return http.StatusOK, body, nil
}

func badRequest(err error) (int, []byte, error) {
	var qe *QueryError
	detail := "malformed request"
	if errors.As(err, &qe) {
		detail = qe.Error()
	}
	return http.StatusBadRequest, append(marshal(map[string]string{"error": "bad-request", "detail": detail}), '\n'), nil
}
