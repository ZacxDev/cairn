package transcript

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/ZacxDev/cairn/internal/transcript/archive"
	"github.com/ZacxDev/cairn/internal/transcript/transcripttest"
)

// The agent read API's model (decision 17; S8), over a real archive. Every record is SYNTHETIC.

const agentSecret = "the owner asked for the gadget to be deployed"

// agentRecords is a Claude Code main stream exercising every conversation-view rule: a human
// message (1), a tool call (2), its result WITH the duplicate `toolUseResult` field (3), a
// bookkeeping record (4), a compaction boundary (5), a subagent spawn (6) and its result naming the
// stream (7), a failed call (8) and its result (9), and the next human message (10).
var agentRecords = []string{
	`{"type":"user","message":{"role":"user","content":"` + agentSecret + `"}}`,
	`{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","id":"toolu_a1","name":"Bash","input":{"command":"ls <dir>"}}]}}`,
	`{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_a1","content":"a.txt\nb.txt"}]},"toolUseResult":{"stdout":"a.txt\nb.txt","stderr":""},"uuid":"u3"}`,
	`{"type":"attachment","attachment":{"type":"hook_success","content":"hook ran"}}`,
	`{"type":"system","subtype":"compact_boundary","content":"Conversation compacted"}`,
	`{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","id":"toolu_a2","name":"Task","input":{"description":"look it up","subagent_type":"general","prompt":"find it"}}]}}`,
	`{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_a2","content":"found"}]},"toolUseResult":{"agentId":"ag1","content":"found"}}`,
	`{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","id":"toolu_a3","name":"Bash","input":{"command":"false"}}]}}`,
	`{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_a3","content":"Exit code 1","is_error":true}]}}`,
	`{"type":"user","message":{"role":"user","content":"thanks"}}`,
}

var agentSubagent = []string{
	`{"type":"user","isSidechain":true,"agentId":"ag1","message":{"role":"user","content":"find it"}}`,
	`{"type":"assistant","isSidechain":true,"agentId":"ag1","message":{"role":"assistant","content":[{"type":"text","text":"found"}]}}`,
}

func agentView(t *testing.T) (*View, *archive.Archive) {
	t.Helper()
	a := transcripttest.Open(t)
	s := transcripttest.Session{Root: "s-agent-01", Owner: readOwner}
	transcripttest.Append(t, a, s, "main", agentRecords)
	transcripttest.Append(t, a, s, "subagent:ag1", agentSubagent)
	meta, err := a.ReadMeta("s-agent-01")
	if err != nil || meta == nil {
		t.Fatalf("not stored: %v", err)
	}
	return &View{Meta: meta, a: a}, a
}

// pageRecords decodes a page's JSON back into its records, to read what a client reads.
func pageRecords(t *testing.T, p Page) []map[string]json.RawMessage {
	t.Helper()
	var body struct {
		Records []map[string]json.RawMessage `json:"records"`
	}
	if err := json.Unmarshal(p.JSON(), &body); err != nil {
		t.Fatalf("the page is not JSON: %v\n%s", err, p.JSON())
	}
	return body.Records
}

// TestTheRawViewIsTheStoredBytes — `view=raw` returns the stored records BYTE-EXACT. The control is
// the stored segment itself: each `rec` is byte-compared against what the archive holds, and the
// fixture carries `<`, which `encoding/json` would have escaped.
func TestTheRawViewIsTheStoredBytes(t *testing.T) {
	v, a := agentView(t)
	stored, err := a.ReadStream("s-agent-01", "main")
	if err != nil || len(stored) != len(agentRecords) {
		t.Fatalf("the control: %d stored records (%v)", len(stored), err)
	}
	p, err := v.Page(RecordsQuery{Stream: "main", View: ViewRaw, MaxBytes: MaxPageBytes})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Records) != len(stored) || !p.Done || p.NextSeq != int64(len(stored))+1 {
		t.Fatalf("the raw page carries %d records (done=%v next=%d), want all %d", len(p.Records), p.Done, p.NextSeq, len(stored))
	}
	body := p.JSON()
	for i, rec := range stored {
		if !bytes.Equal(p.Records[i].Rec, rec.Rec) || p.Records[i].Omitted != nil {
			t.Errorf("record %d: the raw view carries %q, the store holds %q", rec.Seq, p.Records[i].Rec, rec.Rec)
		}
		if !bytes.Contains(body, append([]byte(`"rec":`), rec.Rec...)) {
			t.Errorf("record %d: the ENCODED page does not carry the stored bytes verbatim", rec.Seq)
		}
	}
	if !bytes.Contains(body, []byte(`ls <dir>`)) {
		t.Error("the encoded raw page escaped `<` — it is not the stored bytes")
	}
}

// TestTheConversationViewLeavesOutBookkeepingAndTheDuplicate — the DEFAULT view: the bookkeeping
// record is a placeholder naming its class, type and size; the duplicate `toolUseResult` field is
// stripped from the records carrying it and named with its size; every other byte is the stored
// one. RED before S8: there was no view at all.
func TestTheConversationViewLeavesOutBookkeepingAndTheDuplicate(t *testing.T) {
	v, a := agentView(t)
	stored, _ := a.ReadStream("s-agent-01", "main")
	p, err := v.Page(RecordsQuery{Stream: "main", MaxBytes: MaxPageBytes})
	if err != nil {
		t.Fatal(err)
	}
	if p.View != ViewConversation || len(p.Records) != len(stored) {
		t.Fatalf("view %q with %d records, want the conversation view with one entry per stored record", p.View, len(p.Records))
	}
	for i, pr := range p.Records {
		raw := stored[i].Rec
		switch pr.Seq {
		case 4:
			want := []Omission{{Bookkeeping, "", len(raw), "attachment"}}
			if pr.Rec != nil || !slices.Equal(pr.Omitted, want) {
				t.Errorf("the bookkeeping record reads rec=%q omitted=%+v, want no record and %+v", pr.Rec, pr.Omitted, want)
			}
		case 3, 7:
			var full map[string]json.RawMessage
			_ = json.Unmarshal(raw, &full)
			dup := full["toolUseResult"]
			want := []Omission{{Class: Duplicate, Path: "/toolUseResult", Bytes: len(dup)}}
			if !slices.Equal(pr.Omitted, want) || bytes.Contains(pr.Rec, []byte("toolUseResult")) {
				t.Errorf("record %d: omitted=%+v rec=%s, want the duplicate stripped and named %+v", pr.Seq, pr.Omitted, pr.Rec, want)
			}
			var got map[string]json.RawMessage
			if err := json.Unmarshal(pr.Rec, &got); err != nil {
				t.Fatalf("record %d is not JSON after the strip: %v", pr.Seq, err)
			}
			delete(full, "toolUseResult")
			for k, val := range full {
				if !bytes.Equal(got[k], val) {
					t.Errorf("record %d: member %q changed: %s → %s", pr.Seq, k, val, got[k])
				}
			}
			if len(got) != len(full) {
				t.Errorf("record %d has %d members after the strip, want %d", pr.Seq, len(got), len(full))
			}
		default:
			if !bytes.Equal(pr.Rec, raw) || pr.Omitted != nil {
				t.Errorf("record %d changed in the conversation view: %s", pr.Seq, pr.Rec)
			}
		}
	}
	if bytes.Contains(p.JSON(), []byte("hook ran")) {
		t.Error("the conversation view carries the bookkeeping record's content")
	}
}

// TestAPageIsBoundedAndNeverSplitsARecord — `max_bytes` bounds every page, INCLUDING one whose first
// record alone exceeds it: that record is returned alone and whole, with `next_seq` naming the next.
func TestAPageIsBoundedAndNeverSplitsARecord(t *testing.T) {
	a := transcripttest.Open(t)
	s := transcripttest.Session{Root: "s-agent-02", Owner: readOwner}
	var big []string
	for range 5 {
		big = append(big, `{"type":"user","message":{"role":"user","content":"`+strings.Repeat("x", 100_000)+`"}}`)
	}
	transcripttest.Append(t, a, s, "main", big)
	meta, _ := a.ReadMeta("s-agent-02")
	v := &View{Meta: meta, a: a}

	p, err := v.Page(RecordsQuery{Stream: "main", MaxBytes: DefaultPageBytes})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Records) != 1 || p.Done || p.NextSeq != 2 || len(p.Records[0].Rec) != len(big[0]) {
		t.Fatalf("a 100 KB record under a 64 KiB bound: %d record(s), done=%v, next=%d — want it ALONE and WHOLE, next 2",
			len(p.Records), p.Done, p.NextSeq)
	}
	p, _ = v.Page(RecordsQuery{Stream: "main", FromSeq: 2, MaxBytes: 250_000})
	if len(p.Records) != 2 || p.NextSeq != 4 || p.Done {
		t.Fatalf("a 250 KB bound from record 2: %d records, next %d, done %v — want 2 records, next 4", len(p.Records), p.NextSeq, p.Done)
	}
	if n := len(p.JSON()); n > 250_000+200 {
		t.Errorf("the page is %d bytes over a 250,000-byte bound", n)
	}
	p, _ = v.Page(RecordsQuery{Stream: "main", FromSeq: 4, MaxBytes: MaxPageBytes})
	if len(p.Records) != 2 || !p.Done || p.NextSeq != 6 {
		t.Errorf("the last page: %d records, done %v, next %d — want 2, done, cursor 6", len(p.Records), p.Done, p.NextSeq)
	}
	p, _ = v.Page(RecordsQuery{Stream: "main", FromSeq: 2, ToSeq: 2, MaxBytes: MaxPageBytes})
	if len(p.Records) != 1 || p.Records[0].Seq != 2 || !p.Done || p.NextSeq != 3 {
		t.Errorf("from 2 to 2: %d records, done %v, next %d", len(p.Records), p.Done, p.NextSeq)
	}
}

func seqs(p Page) []int64 {
	var out []int64
	for _, r := range p.Records {
		out = append(out, r.Seq)
	}
	return out
}

// TestThePageFilters — `class`, `role` and `tool` keep exactly the records they name; a tool's
// RESULT is matched to the tool its call named.
func TestThePageFilters(t *testing.T) {
	v, _ := agentView(t)
	for _, tc := range []struct {
		q    RecordsQuery
		want []int64
	}{
		{RecordsQuery{Class: HumanText}, []int64{1, 10}},
		{RecordsQuery{Class: ToolResult}, []int64{3, 7, 9}},
		{RecordsQuery{Class: Bookkeeping}, []int64{4}},
		{RecordsQuery{Class: Runtime}, []int64{5}},
		{RecordsQuery{Role: "assistant"}, []int64{2, 6, 8}},
		{RecordsQuery{Tool: "Bash"}, []int64{2, 3, 8, 9}},
		{RecordsQuery{Tool: "Task"}, []int64{6, 7}},
		{RecordsQuery{Tool: "Bash", FromSeq: 3, ToSeq: 8}, []int64{3, 8}},
		{RecordsQuery{Class: AgentPrompt, Stream: "subagent:ag1"}, []int64{1}},
	} {
		if tc.q.Stream == "" {
			tc.q.Stream = "main"
		}
		tc.q.View, tc.q.MaxBytes = ViewRaw, MaxPageBytes
		p, err := v.Page(tc.q)
		if err != nil {
			t.Fatal(err)
		}
		if got := seqs(p); !slices.Equal(got, tc.want) {
			t.Errorf("%+v kept %v, want %v", tc.q, got, tc.want)
		}
	}
	if _, err := v.Page(RecordsQuery{Stream: "subagent:nope"}); err != ErrNotHeld {
		t.Errorf("a stream the session does not hold answered %v, want ErrNotHeld", err)
	}
}

// TestTheSkeletonIsTheShapeWithoutTheContent — the stream tree (the subagent under the call that
// spawned it), sizes by class, compaction boundaries and a turn index with every call, its sizes and
// its outcome. No content: the human message's text is absent from the JSON.
func TestTheSkeletonIsTheShapeWithoutTheContent(t *testing.T) {
	v, _ := agentView(t)
	sk, err := v.Skeleton()
	if err != nil {
		t.Fatal(err)
	}
	if body := marshal(sk); bytes.Contains(body, []byte(agentSecret)) || bytes.Contains(body, []byte("a.txt")) {
		t.Fatalf("the skeleton carries content:\n%s", body)
	}
	if len(sk.Streams) != 2 || sk.Streams[0].Stream != "main" || sk.Streams[1].Stream != "subagent:ag1" {
		t.Fatalf("the stream tree is %+v", sk.Streams)
	}
	main, sub := sk.Streams[0], sk.Streams[1]
	if sub.Parent != "main" || sub.ParentSeq != 6 || sub.AgentType != "general" || sub.Description != "look it up" || sub.SpawnDepth != 1 {
		t.Errorf("the subagent is placed as %+v, want under main's call at 6, general, depth 1", sub)
	}
	if main.Parent != "" || main.SpawnDepth != 0 || main.Records != 10 {
		t.Errorf("main reads %+v", main)
	}
	var total int64
	for _, r := range agentRecords {
		total += int64(len(r))
	}
	if main.Bytes != total {
		t.Errorf("main's bytes are %d, want the stored %d", main.Bytes, total)
	}
	if !slices.Equal(main.Compactions, []int64{5}) {
		t.Errorf("compactions %v, want [5]", main.Compactions)
	}
	for class, want := range map[Class]ClassCount{
		HumanText:   {2, len(agentSecret) + 2 + len("thanks") + 2},
		Bookkeeping: {1, len(agentRecords[3])},
		Duplicate:   {2, len(`{"stdout":"a.txt\nb.txt","stderr":""}`) + len(`{"agentId":"ag1","content":"found"}`)},
		ToolCall:    {3, 0},
	} {
		got := main.Classes[class]
		if got.Units != want.Units || (want.Bytes != 0 && got.Bytes != want.Bytes) {
			t.Errorf("class %s: %+v, want %+v", class, got, want)
		}
	}
	if sub.Classes[AgentPrompt].Units != 1 || sub.Classes[HumanText].Units != 0 {
		t.Errorf("the subagent's prompt is classed %+v, want agent-prompt", sub.Classes)
	}
	if len(main.Turns) != 2 {
		t.Fatalf("%d turns, want 2", len(main.Turns))
	}
	t1 := main.Turns[0]
	if t1.SeqFrom != 1 || t1.SeqTo != 9 || t1.UserBytes != len(agentSecret) || len(t1.ToolCalls) != 3 {
		t.Fatalf("turn 1 reads %+v", t1)
	}
	want := []ToolIndex{
		{Seq: 2, ID: "toolu_a1", Tool: "Bash", InputBytes: len(`{"command":"ls <dir>"}`), ResultBytes: len("a.txt\nb.txt"), ResultSeq: 3},
		{Seq: 6, ID: "toolu_a2", Tool: "Task", InputBytes: len(`{"description":"look it up","prompt":"find it","subagent_type":"general"}`),
			ResultBytes: len("found"), ResultSeq: 7, ChildStream: "subagent:ag1"},
		{Seq: 8, ID: "toolu_a3", Tool: "Bash", InputBytes: len(`{"command":"false"}`), ResultBytes: len("Exit code 1"), ResultSeq: 9, IsError: true},
	}
	if !slices.Equal(t1.ToolCalls, want) {
		t.Errorf("turn 1's calls:\n got %+v\nwant %+v", t1.ToolCalls, want)
	}
	if t2 := main.Turns[1]; t2.SeqFrom != 10 || t2.UserBytes != len("thanks") || len(t2.ToolCalls) != 0 {
		t.Errorf("turn 2 reads %+v", t2)
	}
}

// TestOneToolCallCarriesItsResultAndDuplicateInARange — the call, its result and its duplicate copy;
// a byte range moves inward to character boundaries and reports where it landed.
func TestOneToolCallCarriesItsResultAndDuplicateInARange(t *testing.T) {
	a := transcripttest.Open(t)
	s := transcripttest.Session{Root: "s-agent-03", Owner: readOwner}
	long := strings.Repeat("é", 50_000) // 100,000 bytes, every character two
	res, _ := json.Marshal(long)
	transcripttest.Append(t, a, s, "main", []string{
		`{"type":"user","message":{"role":"user","content":"go"}}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"ok"},{"type":"tool_use","id":"toolu_b1","name":"Read","input":{"path":"x"}},{"type":"tool_use","id":"toolu_b2","name":"Read","input":{"path":"y"}}]}}`,
		`{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_b1","content":` + string(res) + `}]},"toolUseResult":{"file":{"content":` + string(res) + `}}}`,
	})
	meta, _ := a.ReadMeta("s-agent-03")
	v := &View{Meta: meta, a: a}

	got, err := v.Tool(ToolQuery{Stream: "main", Seq: 2, Start: 0, End: DefaultRangeBytes})
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "toolu_b1" || got.Tool != "Read" || string(got.Input) != `{"path":"x"}` || got.Result == nil ||
		got.Result.Seq != 3 || got.Result.Bytes != len(long) || got.Result.Range != [2]int{0, DefaultRangeBytes} ||
		len(got.Result.Text) != DefaultRangeBytes {
		t.Fatalf("the first call of record 2 reads %+v", got)
	}
	if got.Duplicate == nil || got.Duplicate.Bytes != len(`{"file":{"content":`)+len(res)+2 {
		t.Errorf("the duplicate reads %+v", got.Duplicate)
	}
	got, _ = v.Tool(ToolQuery{Stream: "main", Seq: 2, Start: 1, End: 11})
	if got.Result.Range != [2]int{2, 10} || got.Result.Text != strings.Repeat("é", 4) {
		t.Errorf("range 1-11 over two-byte characters landed at %v %q, want [2 10] and four characters", got.Result.Range, got.Result.Text)
	}
	got, err = v.Tool(ToolQuery{Stream: "main", Seq: 2, ID: "toolu_b2", End: 10})
	if err != nil || got.ID != "toolu_b2" || got.Result != nil || got.Status != "no result" {
		t.Errorf("the second call (no result stored): %+v %v", got, err)
	}
	for name, q := range map[string]ToolQuery{
		"a record holding no call": {Stream: "main", Seq: 1, End: 10},
		"a record past the end":    {Stream: "main", Seq: 9, End: 10},
		"an id the record lacks":   {Stream: "main", Seq: 2, ID: "toolu_zz", End: 10},
		"a stream not held":        {Stream: "subagent:x", Seq: 1, End: 10},
	} {
		if _, err := v.Tool(q); err != ErrNotHeld {
			t.Errorf("%s answered %v, want ErrNotHeld", name, err)
		}
	}
}

// TestTheOpencodeConversationViewLeavesOutBookkeepingParts — info records and step parts become
// placeholders; text and tool parts are the stored bytes.
func TestTheOpencodeConversationViewLeavesOutBookkeepingParts(t *testing.T) {
	a := transcripttest.Open(t)
	sess := transcripttest.Session{Root: "ses_agent0004", Owner: readOwner, Runtime: "opencode"}
	var recs, srcs []string
	add := func(key, body string) {
		r, s := opencodeUnit(key, body)
		recs, srcs = append(recs, r), append(srcs, s)
	}
	add("info", `{"id":"ses_agent0004"}`)
	add("msg:msg_u1", `{"id":"msg_u1","role":"user"}`)
	add("prt:prt_1", `{"id":"prt_1","messageID":"msg_u1","type":"text","text":"do it"}`)
	add("prt:prt_2", `{"id":"prt_2","messageID":"msg_u1","type":"step-start"}`)
	add("prt:prt_3", `{"id":"prt_3","messageID":"msg_u1","type":"tool","tool":"bash","callID":"c1","state":{"status":"completed","input":{"command":"ls"},"output":"x"}}`)
	transcripttest.Append(t, a, sess, "main", recs, srcs...)
	meta, _ := a.ReadMeta("ses_agent0004")
	v := &View{Meta: meta, a: a}
	p, err := v.Page(RecordsQuery{Stream: "main", MaxBytes: MaxPageBytes})
	if err != nil {
		t.Fatal(err)
	}
	var labels []string
	for i, pr := range p.Records {
		if pr.Rec == nil {
			labels = append(labels, pr.Omitted[0].Label)
		} else if !bytes.Equal(pr.Rec, []byte(recs[i])) {
			t.Errorf("record %d changed: %s", pr.Seq, pr.Rec)
		}
	}
	if !slices.Equal(labels, []string{"session info", "message info", "step-start"}) {
		t.Errorf("the placeholders are %v", labels)
	}
	if p, _ := v.Page(RecordsQuery{Stream: "main", Class: HumanText, View: ViewRaw}); !slices.Equal(seqs(p), []int64{3}) {
		t.Errorf("class=human-text kept %v, want [3]", seqs(p))
	}
	if p, _ := v.Page(RecordsQuery{Stream: "main", Tool: "bash", View: ViewRaw}); !slices.Equal(seqs(p), []int64{5}) {
		t.Errorf("tool=bash kept %v, want [5]", seqs(p))
	}
	got, err := v.Tool(ToolQuery{Stream: "main", Seq: 5, End: 10})
	if err != nil || got.ID != "c1" || got.Result == nil || got.Result.Text != "x" || string(got.Input) != `{"command":"ls"}` {
		t.Errorf("the opencode call reads %+v (%v)", got, err)
	}
}

// TestTheReadParametersAreRefusedBeforeThePredicate — a malformed parameter is a 400 naming it, and
// never echoes its value; an unrecognised view is REFUSED, not defaulted.
func TestTheReadParametersAreRefusedBeforeThePredicate(t *testing.T) {
	for name, q := range map[string]url.Values{
		"from_seq":  {ParamFromSeq: {"0"}},
		"to_seq":    {ParamFromSeq: {"5"}, ParamToSeq: {"4"}},
		"class":     {ParamClass: {"secret-sauce"}},
		"view":      {ParamView: {"rawr"}},
		"max_bytes": {ParamMaxBytes: {"1048577"}},
		"wide":      {ParamFromSeq: {"123456789012345678901"}},
	} {
		_, err := ParseRecordsQuery(q)
		var qe *QueryError
		if !errors.As(err, &qe) {
			t.Errorf("%s: %v, want a QueryError", name, err)
		}
	}
	for name, q := range map[string]url.Values{
		"no seq":       {},
		"range order":  {ParamSeq: {"1"}, ParamRange: {"9-3"}},
		"range width":  {ParamSeq: {"1"}, ParamRange: {"0-1048577"}},
		"range syntax": {ParamSeq: {"1"}, ParamRange: {"3"}},
	} {
		if _, err := ParseToolQuery(q); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	rq, err := ParseRecordsQuery(url.Values{})
	if err != nil || rq.View != ViewConversation || rq.MaxBytes != DefaultPageBytes || rq.Stream != "main" {
		t.Errorf("the defaults read %+v (%v)", rq, err)
	}
	st := NewStore(transcripttest.Open(t), t.TempDir())
	code, body, _ := st.Answer(Viewer{}, RouteRecords, url.Values{ParamSession: {"s-1"}, ParamView: {"<script>"}})
	if code != http.StatusBadRequest || bytes.Contains(body, []byte("script")) {
		t.Errorf("a bad view answered %d %s — want 400 naming the parameter, never echoing the value", code, body)
	}
	if code, _, _ := st.Answer(Viewer{}, RouteSkeleton, url.Values{}); code != http.StatusBadRequest {
		t.Errorf("no session answered %d, want 400", code)
	}
}

// TestARepeatedDuplicateFieldIsStrippedWhole (S8 review): a record carrying `toolUseResult` TWICE
// loses both in the conversation view, and the omission counts both. RED when only the first
// occurrence was dropped: the second stayed on the page while the omission said it was removed.
func TestARepeatedDuplicateFieldIsStrippedWhole(t *testing.T) {
	a := transcripttest.Open(t)
	transcripttest.Append(t, a, transcripttest.Session{Root: "s-agent-05", Owner: readOwner}, "main", []string{
		`{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t","content":"x"}]},"toolUseResult":{"a":1},"toolUseResult":{"b":2}}`,
	})
	meta, _ := a.ReadMeta("s-agent-05")
	p, err := (&View{Meta: meta, a: a}).Page(RecordsQuery{Stream: "main"})
	if err != nil {
		t.Fatal(err)
	}
	pr := p.Records[0]
	if bytes.Contains(pr.Rec, []byte("toolUseResult")) || len(pr.Omitted) != 1 || pr.Omitted[0].Bytes != len(`{"a":1}`)+len(`{"b":2}`) {
		t.Errorf("rec %s omitted %+v — want both copies gone and counted", pr.Rec, pr.Omitted)
	}
}

// TestAChildReadBeforeItsParentIsStillPlaced (S8 review): a subagent spawned by ANOTHER subagent
// that `main` does not link yet is read in sorted order — before its parent here — and must still
// be placed under the call that spawned it. RED when placement happened while reading: the child
// came out with no parent and depth 0.
func TestAChildReadBeforeItsParentIsStillPlaced(t *testing.T) {
	a := transcripttest.Open(t)
	s := transcripttest.Session{Root: "s-agent-06", Owner: readOwner}
	transcripttest.Append(t, a, s, "main", []string{`{"type":"user","message":{"role":"user","content":"go"}}`})
	transcripttest.Append(t, a, s, "subagent:zz", []string{
		`{"type":"assistant","isSidechain":true,"message":{"role":"assistant","content":[{"type":"tool_use","id":"toolu_z1","name":"Task","input":{"subagent_type":"general","description":"deeper"}}]}}`,
		`{"type":"user","isSidechain":true,"message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_z1","content":"ok"}]},"toolUseResult":{"agentId":"aa"}}`,
	})
	transcripttest.Append(t, a, s, "subagent:aa", []string{`{"type":"user","isSidechain":true,"message":{"role":"user","content":"deeper"}}`})
	meta, _ := a.ReadMeta("s-agent-06")
	sk, err := (&View{Meta: meta, a: a}).Skeleton()
	if err != nil {
		t.Fatal(err)
	}
	for _, ss := range sk.Streams {
		if ss.Stream == "subagent:aa" && (ss.Parent != "subagent:zz" || ss.ParentSeq != 1 || ss.SpawnDepth != 1 || ss.Description != "deeper") {
			t.Errorf("subagent:aa is placed as %+v, want under subagent:zz's call at 1, depth 1", ss)
		}
	}
}
