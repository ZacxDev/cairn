package ui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"

	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"

	"github.com/ZacxDev/cairn/internal/identity"
	"github.com/ZacxDev/cairn/internal/transcript"
	"github.com/ZacxDev/cairn/internal/write"
)

// 🔴 THE TRANSCRIPT ON THE SESSION PAGE (S4 of `claudedocs/plan-cairn-plugins.md`, operator decision
// O3: the session page SHOWS the content). Two surfaces, one seam:
//
//   - the collapsed transcript SECTION on `GET /session` (decision 18), and
//   - the raw-record view, `GET /session/transcript` — the stored records as stored.
//
// 🔴 BOTH ASK `transcript.Store.View` AND NOTHING ELSE — and nothing else is reachable: the Store's
// archive is unexported, so a stored record leaves `internal/transcript` only inside a `View`, and
// a `View` serves records only up to where the stream stood when it was decided. That function is the ONE visibility
// predicate (decision 4: the owner, or a reader of EVERY scope the session wrote or read), computed
// from the request's own `control.Resolve` authority — never `Model.Grants`. It answers NIL for a
// session with no transcript, for one this viewer is refused, and for an id that is no session at
// all; the handlers below cannot tell those apart, so a refused viewer gets THE SAME BYTES as a
// session with no transcript — on the page (no section, no link, no count) and on the raw row (one
// 404 body) — and nothing says which scope hid it.
//
// 🔴 `Config.Transcripts` NIL IS THE OFF STATE AND THE DEPLOYED ONE. A `cairn-ui` started without
// `-transcript-dir` has no archive: the section never renders and the raw row answers its miss for
// every id, so such a deployment serves exactly the bytes it served before this file existed.
//
// 🔴 EVERY BYTE OF A TRANSCRIPT IS SOMEBODY'S TEXT — a prompt, a tool output, a model's words — AND
// IT IS RENDERED AS TEXT. `g.Text` and attribute values only; no Markdown, no link is ever built
// from content (the only hrefs here are this surface's own ledger paths with encoded operands), and
// the raw-node ban covers this file like every other. NO SCRIPT: every reveal is a `<details>`.

// SessionTranscriptPath is the raw-record view: one stored stream's records, as stored, keyed by
// `?session=<id>` with `?stream=`, `?seq=` and `?to=` selecting which. A FIXED path with its
// operands in query parameters, for the browse pair's reason.
const SessionTranscriptPath = "/session/transcript"

// The transcript rows' query parameters.
const (
	// QueryTurn selects the PAGE of the session page's transcript section: the page holding that
	// turn (`transcript.TurnsPerPage` turns each). A view selector, never an authority input — an
	// unrecognised value is page one.
	QueryTurn = "turn"
	// QueryStream names a stored stream on the raw-record view (`main`, `subagent:<id>`,
	// `child:<id>`); absent means `main`. MATCHED against the session's stored streams, never
	// resolved to a path.
	QueryStream = "stream"
	// QuerySeq and QueryTo bound the raw-record view to the stored records `seq`..`to` (absent:
	// from the first, to the page's byte bound).
	QuerySeq = "seq"
	QueryTo  = "to"
)

// The collapsed reading's DISPLAY limits (decision 18). Storage is untouched by every one of them.
const (
	// assistantHeadChars and assistantHeadLines are how much assistant text shows before its
	// "read more" reveal: the first 1,200 characters or 15 lines, whichever ends first.
	assistantHeadChars = 1200
	assistantHeadLines = 15
	// toolResultInline is how much of a tool result shows inside its `<details>`; the rest is in
	// the raw-record view.
	toolResultInline = 64 << 10
	// rawPageBytes bounds one raw-record page. A page always carries at least one record, so one
	// record larger than this (the store admits records of several megabytes) is a page alone.
	rawPageBytes = 512 << 10
)

// transcriptUnseenBody is the raw-record row's ONE miss: no transcript, a refused viewer, an id
// that is no session, a stream the session does not hold, a record number past its end. It names
// nothing.
const transcriptUnseenBody = "no transcript by that session is visible to you — none was stored, or it " +
	"touched a scope you cannot read"

// TranscriptView is the session page's transcript section: one page of the collapsed reading.
type TranscriptView struct {
	Session string
	// Runtime and Host are the uploader's own words about the session (`claude`/`opencode`, a host
	// label).
	Runtime, Host string
	Reading       transcript.Reading
}

// transcriptFor is the session page's ONE question about a transcript: the section to render, or
// nil.
//
// 🔴 NIL IS "RENDER NOTHING", FOR EVERY REASON THERE IS — no archive, no stored session, a refused
// viewer, and a stored session that could not be read. The last is logged and NOT shown: a page that
// said "a transcript exists but could not be read" to a viewer the predicate was never asked about
// would be the existence oracle the predicate exists to close.
func (s *Server) transcriptFor(id identity.Identity, session string, turn int) *TranscriptView {
	view := s.transcriptView(id, session)
	if view == nil {
		return nil
	}
	reading, err := view.Read(turn)
	if err != nil {
		fmt.Fprintf(s.log, "ui: the transcript of a stored session could not be read: %v\n", err)
		return nil
	}
	return &TranscriptView{Session: session, Runtime: view.Meta.Runtime, Host: view.Meta.Host, Reading: reading}
}

// transcriptView asks the ONE predicate.
func (s *Server) transcriptView(id identity.Identity, session string) *transcript.View {
	if s.transcripts == nil {
		return nil
	}
	view, err := s.transcripts.View(transcript.Viewer{Principal: id.Principal, Auth: id.Auth}, session)
	if err != nil {
		fmt.Fprintf(s.log, "ui: a transcript's visibility could not be decided, so it is not shown: %v\n", err)
		return nil
	}
	return view
}

// turnParam reads `?turn=` as the first turn of its page. A value that does not parse — a wider one
// than `int` included — is page one, like any other unrecognised value.
//
// The clamp is `transcript.PageStart`'s and only its: this package kept a copy, so removing the
// one in `transcript` left every ui test green (#261 round 2), and `transcript-turn-unclamped`
// now lists this package's pager test as a killer too.
func turnParam(r *http.Request) int {
	n, err := strconv.Atoi(r.URL.Query().Get(QueryTurn))
	if err != nil {
		n = 1
	}
	return transcript.PageStart(n)
}

// transcriptRawHref is the ONE place a raw-record URL is built: the ledger path plus
// `url.Values.Encode`, `sessionHref`'s ruling. `seq` and `to` are omitted at zero.
func transcriptRawHref(session, stream string, seq, to int64) string {
	q := url.Values{QuerySession: []string{session}, QueryStream: []string{stream}}
	if seq > 0 {
		q.Set(QuerySeq, strconv.FormatInt(seq, 10))
	}
	if to > 0 {
		q.Set(QueryTo, strconv.FormatInt(to, 10))
	}
	return SessionTranscriptPath + "?" + q.Encode()
}

// transcriptPageHref is the session page at the transcript page holding `turn`.
func transcriptPageHref(session string, turn int) string {
	q := url.Values{QuerySession: []string{session}}
	if turn > 1 {
		q.Set(QueryTurn, strconv.Itoa(turn))
	}
	return SessionPath + "?" + q.Encode() + "#transcript"
}

// humanBytes renders a size for a summary line: `118 B`, `3.2 KB`, `1.4 MB`.
func humanBytes(n int) string {
	switch {
	case n < 1000:
		return strconv.Itoa(n) + " B"
	case n < 1000*1000:
		return strconv.FormatFloat(float64(n)/1000, 'f', 1, 64) + " KB"
	default:
		return strconv.FormatFloat(float64(n)/(1000*1000), 'f', 1, 64) + " MB"
	}
}

// splitAssistantText cuts assistant text into the head that is shown and the rest behind the
// reveal: the head ends at [assistantHeadChars] characters or at the end of line
// [assistantHeadLines], whichever comes first. `head + rest` is the text, byte for byte; rest is ""
// when the text is within both limits.
func splitAssistantText(text string) (head, rest string) {
	chars, lines := 0, 0
	for i, c := range text {
		if chars == assistantHeadChars {
			return text[:i], text[i:]
		}
		chars++
		if c == '\n' {
			lines++
			if lines == assistantHeadLines {
				return text[:i], text[i:]
			}
		}
	}
	return text, ""
}

// clipUTF8 cuts s to at most n bytes without splitting a character.
func clipUTF8(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

// transcriptSection renders one page of the collapsed reading.
func transcriptSection(v TranscriptView) g.Node {
	rd := v.Reading
	last := rd.FirstTurn + len(rd.Turns) - 1
	span := "no turn " + strconv.Itoa(rd.FirstTurn)
	if len(rd.Turns) > 0 {
		span = "turns " + strconv.Itoa(rd.FirstTurn) + "–" + strconv.Itoa(last)
	}
	return h.Section(
		h.Class("card transcript"),
		h.ID("transcript"),
		h.Div(h.Class("card-head"),
			h.H2(g.Text("Transcript")),
			h.Span(h.Class("entry-count"), g.Text(plural(rd.TotalTurns, "turn", "turns"))),
		),
		h.P(h.Class("card-stats"),
			stat(v.Runtime, "the runtime the capture agent uploaded this session as"),
			stat("from "+v.Host, "the host label of the capture token that uploaded it"),
			stat(span+" of "+strconv.Itoa(rd.TotalTurns), "user messages are shown in full; assistant text past "+
				strconv.Itoa(assistantHeadChars)+" characters, tool calls, subagents and bookkeeping are collapsed — "+
				"every stored byte is in the raw records"),
		),
		transcriptPager(v),
		g.Map(rd.Turns, func(t transcript.Turn) g.Node { return turnNode(v.Session, t) }),
		transcriptPager(v),
		h.Details(h.Class("t-block"),
			h.Summary(g.Text("raw records · "+plural(len(rd.Streams), "stream", "streams"))),
			h.Ul(h.Class("entry-list"), g.Map(rd.Streams, func(st transcript.StreamRef) g.Node {
				return h.Li(h.Class("t-line"),
					h.A(h.Href(transcriptRawHref(v.Session, st.Name, 0, 0)), g.Text(st.Name)),
					g.Text(" · "+plural(int(st.Records), "record", "records")))
			})),
		),
	)
}

// transcriptPager links the previous and next page of turns, and renders nothing for a transcript
// that fits one page.
func transcriptPager(v TranscriptView) g.Node {
	rd := v.Reading
	if rd.TotalTurns <= transcript.TurnsPerPage && rd.FirstTurn == 1 {
		return nil
	}
	var links []g.Node
	if rd.FirstTurn > 1 {
		prev := rd.FirstTurn - transcript.TurnsPerPage
		// A page past the end links back to the LAST real page, not to another empty one.
		if prev > rd.TotalTurns {
			prev = transcript.PageStart(max(rd.TotalTurns, 1))
		}
		links = append(links, h.A(h.Href(transcriptPageHref(v.Session, prev)),
			g.Text("← turns "+strconv.Itoa(prev)+"–"+strconv.Itoa(min(prev+transcript.TurnsPerPage-1, rd.TotalTurns)))))
	}
	if next := rd.FirstTurn + transcript.TurnsPerPage; next <= rd.TotalTurns {
		links = append(links, h.A(h.Href(transcriptPageHref(v.Session, next)),
			g.Text("turns "+strconv.Itoa(next)+"–"+strconv.Itoa(min(next+transcript.TurnsPerPage-1, rd.TotalTurns))+" →")))
	}
	return h.Nav(h.Class("pager"), g.Attr("aria-label", "transcript pages"), g.Group(links))
}

// turnNode is one turn: its items in order, then ONE collapsed line for everything the turn does
// not show as an item.
func turnNode(session string, t transcript.Turn) g.Node {
	return h.Article(
		h.Class("turn"),
		h.ID("turn-"+strconv.Itoa(t.N)),
		h.H3(h.Class("turn-head"), g.Text("turn "+strconv.Itoa(t.N))),
		g.Map(t.Items, func(it transcript.Item) g.Node { return itemNode(session, it) }),
		g.If(t.More > 0, h.P(h.Class("t-line"),
			g.Text(plural(t.More, "more item in this turn is", "more items in this turn are")+
				" past this page's "+strconv.Itoa(transcript.PageItemCap)+"-item limit · "),
			h.A(h.Href(transcriptRawHref(session, "main", t.MoreFrom, t.SeqTo)), g.Text("raw records")))),
		uncountedNode(session, "main", t.Bookkeeping, t.Duplicates, t.SeqFrom, t.SeqTo),
	)
}

// uncountedNode is decision 18's one `<details>` per turn (or per child stream) for what is stored
// and not shown as an item: bookkeeping records and duplicate tool-result copies, with the raw
// records one link away. Nothing when there is nothing to count.
func uncountedNode(session, stream string, bookkeeping, duplicates int, from, to int64) g.Node {
	if bookkeeping == 0 && duplicates == 0 {
		return nil
	}
	return h.Details(h.Class("t-block"),
		h.Summary(g.Text(plural(bookkeeping, "bookkeeping record", "bookkeeping records")+", "+
			plural(duplicates, "duplicate tool-result copy", "duplicate tool-result copies"))),
		h.P(h.Class("t-line"),
			g.Text("Stored and not shown here: the runtime's own bookkeeping, and the second copy of a tool result "+
				"it keeps beside the first. "),
			h.A(h.Href(transcriptRawHref(session, stream, from, to)), g.Text("raw records"))),
	)
}

// itemNode renders one item of a reading.
//
// 🔴 A USER MESSAGE — A `human-text` ITEM AND NOTHING ELSE — IS RENDERED WHOLE, WHATEVER ITS LENGTH
// (operator decision O9). It is the one arm with no limit and no reveal, deliberately.
func itemNode(session string, it transcript.Item) g.Node {
	raw := func() g.Node {
		return h.A(h.Href(transcriptRawHref(session, it.Stream, it.Seq, it.Seq)), g.Text("raw record"))
	}
	switch it.Class {
	case transcript.HumanText:
		return h.Div(h.Class("t-user"), h.Data("class", string(it.Class)),
			h.P(h.Class("t-label"), g.Text("user")),
			h.Div(h.Class("t-text"), g.Text(it.Text)))
	case transcript.AssistantText:
		return clippedText(h.Class("t-assistant"), "assistant", it)
	case transcript.AgentPrompt:
		return clippedText(h.Class("t-prompt"), "prompt (written by the parent agent)", it)
	case transcript.Thinking:
		return collapsedText(it, "thinking · "+plural(utf8.RuneCountInString(it.Text), "character", "characters"), raw())
	case transcript.Runtime:
		return collapsedText(it, "runtime-generated · "+it.Label, raw())
	case transcript.ToolCall, transcript.ToolResult:
		return toolNode(session, it)
	case transcript.Binary:
		return h.P(h.Class("t-line"), h.Data("class", string(it.Class)),
			g.Text("binary · "+it.Label+" · "+humanBytes(it.Bytes)+" · "), raw())
	default:
		return h.P(h.Class("t-line"), h.Data("class", string(transcript.Unknown)),
			g.Text("unclassified · "+it.Label+" · "+humanBytes(it.Bytes)+" · "), raw())
	}
}

// clippedText is assistant-style text: its head, then the rest behind `read more (N more
// characters)`.
//
// The class arrives as a NODE, spelled as a literal at each call site: a class name passed as a
// string is one `TestEveryRenderedClassHasARuleInTheStylesheet` cannot read.
func clippedText(class g.Node, label string, it transcript.Item) g.Node {
	head, rest := splitAssistantText(it.Text)
	return h.Div(class, h.Data("class", string(it.Class)),
		h.P(h.Class("t-label"), g.Text(label)),
		h.Div(h.Class("t-text"), g.Text(head)),
		g.If(rest != "", h.Details(h.Class("t-more"),
			h.Summary(g.Text("read more ("+strconv.Itoa(utf8.RuneCountInString(rest))+" more characters)")),
			h.Div(h.Class("t-text"), g.Text(rest)))),
	)
}

// collapsedText is a block whose whole body is behind its summary.
func collapsedText(it transcript.Item, summary string, raw g.Node) g.Node {
	return h.Details(h.Class("t-block"), h.Data("class", string(it.Class)),
		h.Summary(g.Text(summary)),
		g.If(!it.Elided, h.Pre(h.Class("t-pre"), g.Text(it.Text))),
		h.P(h.Class("t-line"), g.If(it.Elided, g.Text("not shown on this page (it is already large) · ")), raw),
	)
}

// toolNode is one tool call, collapsed: `<summary>Bash · exit 1 · 3.2 KB</summary>` over its input,
// its result (the first [toolResultInline] bytes), its binary payloads as one line each, and the
// subagent it spawned as a nested collapsed block.
func toolNode(session string, it transcript.Item) g.Node {
	t := it.Tool
	name := t.Name
	if it.Class == transcript.ToolResult {
		name = "tool result"
	}
	parts := []string{name}
	if t.Status != "" {
		parts = append(parts, t.Status)
	}
	switch {
	case t.HasResult:
		parts = append(parts, humanBytes(t.ResultBytes))
	case it.Class == transcript.ToolCall:
		parts = append(parts, "no result in this turn")
	}
	resultSeq := t.ResultSeq
	if resultSeq == 0 {
		resultSeq = it.Seq
	}
	shown := clipUTF8(t.Result, toolResultInline)
	return h.Details(h.Class("t-block"), h.Data("class", string(it.Class)),
		h.Summary(g.Text(strings.Join(parts, " · "))),
		g.If(it.Class == transcript.ToolCall && !it.Elided, g.Group([]g.Node{
			h.P(h.Class("t-label"), g.Text("input")),
			h.Pre(h.Class("t-pre"), g.Text(t.Input)),
		})),
		g.If(t.HasResult && !t.ResultElided, g.Group([]g.Node{
			h.P(h.Class("t-label"), g.Text("result")),
			h.Pre(h.Class("t-pre"), g.Text(shown)),
		})),
		g.If(len(shown) < len(t.Result), h.P(h.Class("t-line"),
			g.Text("the first "+humanBytes(len(shown))+" of "+humanBytes(t.ResultBytes)+" · "),
			h.A(h.Href(transcriptRawHref(session, it.Stream, resultSeq, resultSeq)), g.Text("the whole result, in the raw record")))),
		g.Map(t.Binary, func(b transcript.Blob) g.Node {
			return h.P(h.Class("t-line"), h.Data("class", string(transcript.Binary)),
				g.Text("binary · "+b.Type+" · "+humanBytes(b.Bytes)))
		}),
		h.P(h.Class("t-line"),
			g.If(it.Elided || t.ResultElided, g.Text("not shown on this page (it is already large) · ")),
			h.A(h.Href(transcriptRawHref(session, it.Stream, it.Seq, resultSeq)), g.Text("raw records"))),
		g.Iff(t.Child != nil && t.Child.Stream != "", func() g.Node { return childNode(session, *t.Child) }),
	)
}

// childNode is a subagent (or an opencode child session) nested under the call that spawned it,
// collapsed, summarised by what the call asked for.
func childNode(session string, c transcript.Child) g.Node {
	parts := []string{"subagent"}
	if c.AgentType != "" {
		parts = append(parts, c.AgentType)
	}
	if c.Description != "" {
		parts = append(parts, c.Description)
	}
	parts = append(parts, plural(len(c.Items)+c.More, "item", "items"))
	return h.Details(h.Class("t-block"), h.Data("stream", c.Stream),
		h.Summary(g.Text(strings.Join(parts, " · "))),
		h.Div(h.Class("t-child"),
			g.Map(c.Items, func(it transcript.Item) g.Node { return itemNode(session, it) }),
			g.If(c.More > 0, h.P(h.Class("t-line"),
				g.Text(plural(c.More, "more item is", "more items are")+" in the raw records · "))),
			uncountedNode(session, c.Stream, c.Bookkeeping, c.Duplicates, 0, 0),
			h.P(h.Class("t-line"), h.A(h.Href(transcriptRawHref(session, c.Stream, 0, 0)), g.Text("this stream's raw records"))),
		),
	)
}

// RawTranscript is the raw-record view's page: stored records of one stream, as stored.
type RawTranscript struct {
	Session, Stream string
	// Lines are the stored lines, `{"seq":…,"src":…,"rec":…}`, one per record, in order.
	Lines []RawRecord
	// Next is the first record NOT on this page, 0 when the page reaches the requested end.
	Next, To int64
	// Records is how many records the stream holds.
	Records int64
}

// RawRecord is one stored record on the raw-record page.
type RawRecord struct {
	Seq  int64
	Line string
}

// handleSessionTranscript is the raw-record view.
//
// A request naming no session gets the navigation page — the browse pair's ruling, and what lets
// `TestEveryContentRouteConsultsTheAuthority` see this row ask. Every OTHER miss is ONE answer,
// [transcriptUnseenBody]: an id the grammar cannot produce, no archive, no transcript, a refused
// viewer, a stream the session does not hold, a `seq` past the stream's end.
func (s *Server) handleSessionTranscript(w http.ResponseWriter, r *http.Request, id identity.Identity) {
	scopes, err := s.source.Visible(id.Auth)
	if err != nil {
		writePlain(w, http.StatusInternalServerError, "the store could not be read")
		return
	}
	view := PageView{Viewer: id.Principal.Display, CSRF: csrfTokenFor(r), Scopes: scopes, Now: s.now(), App: s.app}
	q := r.URL.Query()
	session := q.Get(QuerySession)
	if session == "" {
		s.renderNavigate(w, view)
		return
	}
	miss := func() { writePlain(w, http.StatusNotFound, transcriptUnseenBody) }
	if !write.SessionComponent.MatchString(session) {
		miss()
		return
	}
	tv := s.transcriptView(id, session)
	if tv == nil {
		miss()
		return
	}
	stream := q.Get(QueryStream)
	if stream == "" {
		stream = "main"
	}
	pos, stored := tv.Meta.Streams[stream]
	if !stored {
		miss()
		return
	}
	from, to := seqParam(q.Get(QuerySeq), 1), seqParam(q.Get(QueryTo), pos.SeqTo)
	raw := RawTranscript{Session: session, Stream: stream, To: to, Records: pos.SeqTo}
	size := 0
	err = tv.Records(stream, func(rec transcript.Record) error {
		if rec.Seq < from {
			return nil
		}
		if rec.Seq > to {
			return transcript.StopRecords
		}
		line := storedLine(rec)
		if len(raw.Lines) > 0 && size+len(line) > rawPageBytes {
			raw.Next = rec.Seq
			return transcript.StopRecords
		}
		size += len(line)
		raw.Lines = append(raw.Lines, RawRecord{Seq: rec.Seq, Line: line})
		return nil
	})
	if err != nil {
		fmt.Fprintf(s.log, "ui: a stored transcript stream could not be read: %v\n", err)
		miss()
		return
	}
	if len(raw.Lines) == 0 {
		miss()
		return
	}
	s.render(w, RawTranscriptPage(view, raw))
}

// seqParam reads a record number: a positive integer, or `fallback`.
func seqParam(v string, fallback int64) int64 {
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil || n < 1 {
		return fallback
	}
	return n
}

// storedLine is a stored record's line as the store wrote it: `{"seq":N,"src":"…","rec":…}`, the
// record's own bytes untouched (`json.Marshal` on the struct would HTML-escape inside them).
func storedLine(rec transcript.Record) string {
	var b strings.Builder
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(rec.Src)
	src := strings.TrimSuffix(b.String(), "\n")
	return `{"seq":` + strconv.FormatInt(rec.Seq, 10) + `,"src":` + src + `,"rec":` + string(rec.Rec) + `}`
}

// RawTranscriptPage renders stored records as text.
//
// 🔴 `g.Text` INSIDE `<pre><code>`, `rawBlock`'s RULING: this view exists to answer "what was
// stored", so the characters are the stored characters and nothing is interpreted.
func RawTranscriptPage(v PageView, raw RawTranscript) g.Node {
	first, last := raw.Lines[0].Seq, raw.Lines[len(raw.Lines)-1].Seq
	label := "session " + shortID(raw.Session)
	return shell(label+" · raw records", v,
		[]crumb{{Label: label, Href: sessionHref(raw.Session)}, {Label: "raw records"}},
		h.Section(
			h.Class("card"),
			h.ID("transcript-raw"),
			h.Div(h.Class("card-head"),
				h.H2(g.Text("Raw records")),
				h.Span(h.Class("entry-count"), g.Text("stream "+raw.Stream)),
			),
			h.P(h.Class("card-stats"),
				stat("records "+strconv.FormatInt(first, 10)+"–"+strconv.FormatInt(last, 10)+" of "+strconv.FormatInt(raw.Records, 10),
					"each line is one stored record: its sequence number, its source position on the host, and the "+
						"record as the capture agent uploaded it (redacted on that host)"),
				g.If(raw.Next > 0, h.Span(h.Class("stat"),
					h.A(h.Href(transcriptRawHref(raw.Session, raw.Stream, raw.Next, raw.To)), g.Text("next records →")))),
			),
			g.Map(raw.Lines, func(rec RawRecord) g.Node {
				return h.Pre(h.Class("entry-raw"), h.Data("seq", strconv.FormatInt(rec.Seq, 10)), h.Code(g.Text(rec.Line)))
			}),
		),
	)
}
