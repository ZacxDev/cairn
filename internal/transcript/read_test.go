package transcript

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/ZacxDev/cairn/internal/redact"
	"github.com/ZacxDev/cairn/internal/transcript/archive"
	"github.com/ZacxDev/cairn/internal/transcript/transcripttest"
)

// The collapsed reading (decision 18), over a real archive. Every record is SYNTHETIC: the rich
// session is the generator's (`testdata/synthetic_world.json`), the rest are built here.

const readOwner = "user:u_reader"

// hostRedacted is a fixture line as the capture agent would upload it: through the host-side
// redactor, so the pod's refusing re-check stores it (a thinking `signature` is long and random).
func hostRedacted(t *testing.T, lines []string) []string {
	t.Helper()
	red, err := redact.New(redact.SelfTestKey(7), nil)
	if err != nil {
		t.Fatal(err)
	}
	out := make([]string, len(lines))
	for i, l := range lines {
		rec, _ := red.Record([]byte(l))
		out[i] = string(rec)
	}
	return out
}

func readAll(t *testing.T, a *archive.Archive, root string, turn int) Reading {
	t.Helper()
	meta, err := a.ReadMeta(root)
	if err != nil || meta == nil {
		t.Fatalf("%s is not stored (%v)", root, err)
	}
	rd, err := Read(a, meta, turn)
	if err != nil {
		t.Fatal(err)
	}
	return rd
}

func itemsOf(items []Item, class Class) []Item {
	var out []Item
	for _, it := range items {
		if it.Class == class {
			out = append(out, it)
		}
	}
	return out
}

// TestTheRichClaudeSessionReadsAsTurnsOfClassifiedUnits stores the generator's rich Claude Code
// session — main stream and both subagents — and reads it back: turns open at human text and only
// there; every tool call carries its result; a failed call says how; the duplicate tool-result
// copies and the bookkeeping records are COUNTED, not shown; the compaction summary is a labelled
// runtime block; and each subagent is nested under the call that spawned it, with its prompt
// classed as the parent agent's.
func TestTheRichClaudeSessionReadsAsTurnsOfClassifiedUnits(t *testing.T) {
	w := loadWorld(t)
	rich := w.Sessions["cc-rich"].ID
	a := transcripttest.Open(t)
	sess := transcripttest.Session{Root: rich, Owner: readOwner}
	var paths []string
	for path := range w.Claude.Files {
		if strings.Contains(path, rich) && strings.HasSuffix(path, ".jsonl") {
			paths = append(paths, path)
		}
	}
	sort.Strings(paths)
	if len(paths) != 3 {
		t.Fatalf("the rich session has %d stream files, want main and two subagents: %v", len(paths), paths)
	}
	for _, path := range paths {
		stream := "main"
		if i := strings.Index(path, "/subagents/agent-"); i >= 0 {
			stream = "subagent:" + strings.TrimSuffix(path[i+len("/subagents/agent-"):], ".jsonl")
		}
		lines := strings.Split(strings.TrimRight(w.Claude.Files[path]["utf8"], "\n"), "\n")
		transcripttest.Append(t, a, sess, stream, hostRedacted(t, lines))
	}

	rd := readAll(t, a, rich, 1)
	if rd.TotalTurns != 2 || len(rd.Turns) != 2 || rd.FirstTurn != 1 {
		t.Fatalf("the rich session reads as %d turns (%d on the page, first %d), want 2 — it holds two human messages; "+
			"a compaction summary, an `isMeta` record and fifteen tool results are `user` records too and open none",
			rd.TotalTurns, len(rd.Turns), rd.FirstTurn)
	}
	if len(rd.Streams) != 3 || rd.Streams[0].Name != "main" {
		t.Errorf("the reading lists streams %v, want main first and both subagents", rd.Streams)
	}
	one, two := rd.Turns[0], rd.Turns[1]
	if n := len(itemsOf(one.Items, HumanText)); n != 1 {
		t.Errorf("turn 1 carries %d human-text items, want 1", n)
	}
	calls := itemsOf(one.Items, ToolCall)
	if len(calls) != 16 {
		t.Fatalf("turn 1 carries %d tool calls, want 16", len(calls))
	}
	statuses := map[string]int{}
	var children []*Child
	binaries := 0
	for _, c := range calls {
		if !c.Tool.HasResult {
			t.Errorf("the %s call (seq %d) has no result, and its result is in the same turn", c.Tool.Name, c.Seq)
		}
		statuses[c.Tool.Status]++
		binaries += len(c.Tool.Binary)
		if c.Tool.Child != nil && c.Tool.Child.Stream != "" {
			children = append(children, c.Tool.Child)
		}
	}
	if statuses["exit 1"] != 1 || statuses[""] != 15 {
		t.Errorf("tool statuses %v, want one `exit 1` (the `false` call) and fifteen with none", statuses)
	}
	if binaries != 1 {
		t.Errorf("turn 1's tool results carry %d binary payloads, want 1 (the image the Read call returned)", binaries)
	}
	if n := len(itemsOf(one.Items, ToolResult)); n != 0 {
		t.Errorf("%d tool results were left unpaired in turn 1, want 0", n)
	}
	// 15 of the 16 results carry the `toolUseResult` copy (the ToolSearch result does not).
	if one.Duplicates != 15 || one.Bookkeeping != 4 || two.Bookkeeping != 14 || two.Duplicates != 0 {
		t.Errorf("counted %d duplicates and %d bookkeeping records in turn 1, %d and %d in turn 2; want 15, 4, 0, 14",
			one.Duplicates, one.Bookkeeping, two.Duplicates, two.Bookkeeping)
	}
	var labels []string
	for _, it := range itemsOf(one.Items, Runtime) {
		labels = append(labels, it.Label)
	}
	if got := strings.Join(labels, ","); got != "compact boundary,compaction summary,meta" {
		t.Errorf("turn 1's runtime blocks are labelled %q, want the boundary, the summary and the meta record", got)
	}
	if len(children) != 2 {
		t.Fatalf("%d calls carry a subagent, want 2", len(children))
	}
	for _, c := range children {
		if c.AgentType != "general-purpose" || c.Description == "" {
			t.Errorf("the subagent %s is summarised %q/%q, want its spawning call's agent type and description", c.Stream, c.AgentType, c.Description)
		}
		if len(itemsOf(c.Items, AgentPrompt)) != 1 || len(itemsOf(c.Items, HumanText)) != 0 {
			t.Errorf("the subagent %s: its prompt is not classed `agent-prompt` (and only that): %+v", c.Stream, c.Items)
		}
		if len(itemsOf(c.Items, ToolCall)) != 1 || len(itemsOf(c.Items, AssistantText)) != 1 || c.Bookkeeping != 1 || c.Duplicates != 1 {
			t.Errorf("the subagent %s reads as %d call(s), %d assistant text(s), %d bookkeeping, %d duplicate(s); want 1 each",
				c.Stream, len(itemsOf(c.Items, ToolCall)), len(itemsOf(c.Items, AssistantText)), c.Bookkeeping, c.Duplicates)
		}
	}
	if children[0].Stream == children[1].Stream {
		t.Error("both calls name the same subagent stream")
	}
}

func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func claudeUser(text string) string {
	return `{"type":"user","message":{"role":"user","content":` + jsonString(text) + `}}`
}

func claudeAssistant(text string) string {
	return `{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":` + jsonString(text) + `}]}}`
}

// TestAToolResultInsideAUserRecordIsNotAUserMessage is decision 17's block-level rule, at the size
// that makes it matter: ONE `user` record carrying a 600 KB `tool_result` block (and its duplicate
// copy) is a tool result, 600 KB of it; a SEPARATE `user` record of 20,000 characters is a user
// message, whole. RED under `transcript-render-classifies-by-record-type`, which reads the record's
// TYPE and hands the 600 KB block back as something a human typed.
func TestAToolResultInsideAUserRecordIsNotAUserMessage(t *testing.T) {
	a := transcripttest.Open(t)
	sess := transcripttest.Session{Root: "s-0001", Owner: readOwner}
	big := strings.Repeat("lorem ipsum dolor sit amet ", 600_000/27+1)[:600_000]
	typed := strings.Repeat("a human typed this. ", 1000)
	result := `{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_r0001",` +
		`"content":` + jsonString(big) + `}]},"toolUseResult":{"stdout":` + jsonString(big) + `}}`
	transcripttest.Append(t, a, sess, "main", []string{claudeUser(typed), result})

	rd := readAll(t, a, "s-0001", 1)
	if len(rd.Turns) != 1 {
		t.Fatalf("%d turns, want 1: the tool-result record must not open a second", len(rd.Turns))
	}
	turn := rd.Turns[0]
	human := itemsOf(turn.Items, HumanText)
	if len(human) != 1 || len(human[0].Text) != 20_000 || human[0].Text != typed {
		t.Fatalf("the reading carries %d human-text item(s); the first is %d bytes, want ONE of exactly 20000 — a user "+
			"message is shown whole, and a tool result is never one", len(human), len(human[0].Text))
	}
	results := itemsOf(turn.Items, ToolResult)
	if len(results) != 1 || results[0].Tool.ResultBytes != 600_000 || results[0].Tool.Result != big {
		t.Fatalf("the 600 KB block reads as %d tool-result item(s)", len(results))
	}
	if turn.Duplicates != 1 {
		t.Errorf("the `toolUseResult` copy was counted %d times, want 1 — it is a duplicate, not content", turn.Duplicates)
	}
}

// TestTurnsPageByFifty: 120 human messages are 120 turns; a page holds fifty of them, numbered from
// the page's first; a turn past the end is an empty page that still knows the total.
func TestTurnsPageByFifty(t *testing.T) {
	a := transcripttest.Open(t)
	sess := transcripttest.Session{Root: "s-0001", Owner: readOwner}
	var lines []string
	for i := 1; i <= 120; i++ {
		lines = append(lines, claudeUser(fmt.Sprintf("message %d", i)), claudeAssistant(fmt.Sprintf("answer %d", i)))
	}
	transcripttest.Append(t, a, sess, "main", lines)
	for _, tc := range []struct{ ask, first, turns int }{
		{1, 1, 50}, {0, 1, 50}, {50, 1, 50}, {51, 51, 50}, {77, 51, 50}, {101, 101, 20}, {120, 101, 20}, {999, 951, 0},
	} {
		rd := readAll(t, a, "s-0001", tc.ask)
		if rd.TotalTurns != 120 || rd.FirstTurn != tc.first || len(rd.Turns) != tc.turns {
			t.Errorf("turn %d: page starts at %d with %d turns of %d, want start %d with %d of 120",
				tc.ask, rd.FirstTurn, len(rd.Turns), rd.TotalTurns, tc.first, tc.turns)
			continue
		}
		for i, turn := range rd.Turns {
			want := fmt.Sprintf("message %d", tc.first+i)
			if turn.N != tc.first+i || len(turn.Items) != 2 || turn.Items[0].Text != want || turn.SeqFrom != int64(2*(tc.first+i)-1) {
				t.Errorf("turn %d: page item %d is turn %d starting at seq %d with %q, want turn %d with %q",
					tc.ask, i, turn.N, turn.SeqFrom, turn.Items[0].Text, tc.first+i, want)
				break
			}
		}
	}
}

// TestRecordsBeforeTheFirstMessageShareItsTurn: what a runtime writes before the first human
// message (a mode record, a runtime note) belongs to turn 1 with it, not to a turn of its own.
func TestRecordsBeforeTheFirstMessageShareItsTurn(t *testing.T) {
	a := transcripttest.Open(t)
	transcripttest.Append(t, a, transcripttest.Session{Root: "s-0001", Owner: readOwner}, "main", []string{
		`{"type":"permission-mode","permissionMode":"default"}`,
		`{"type":"system","subtype":"informational","content":"started"}`,
		claudeUser("first"), claudeAssistant("one"), claudeUser("second"),
	})
	rd := readAll(t, a, "s-0001", 1)
	if rd.TotalTurns != 2 || rd.Turns[0].Bookkeeping != 1 || len(rd.Turns[0].Items) != 3 || rd.Turns[1].Items[0].Text != "second" {
		t.Fatalf("read as %d turns, turn 1 with %d bookkeeping and %d items; want 2 turns, 1 and 3", rd.TotalTurns,
			rd.Turns[0].Bookkeeping, len(rd.Turns[0].Items))
	}
}

// TestCollapsedContentIsBudgetedAndUserTextNeverIs: past [CollapsedBudget] a tool result keeps its
// summary — its size included — and drops its body; a user message of any size is carried whole.
func TestCollapsedContentIsBudgetedAndUserTextNeverIs(t *testing.T) {
	a := transcripttest.Open(t)
	sess := transcripttest.Session{Root: "s-0001", Owner: readOwner}
	chunk := strings.Repeat("0123456789abcdef", 600_000/16)
	huge := strings.Repeat("typed. ", 3<<20/7)
	lines := []string{claudeUser(huge)}
	for i := 0; i < 6; i++ {
		lines = append(lines, fmt.Sprintf(`{"type":"user","message":{"role":"user","content":[{"type":"tool_result",`+
			`"tool_use_id":"toolu_b%04d","content":%s}]}}`, i, jsonString(chunk)))
	}
	transcripttest.Append(t, a, sess, "main", lines)
	rd := readAll(t, a, "s-0001", 1)
	turn := rd.Turns[0]
	if human := itemsOf(turn.Items, HumanText); len(human) != 1 || human[0].Text != huge || human[0].Elided {
		t.Fatalf("the %d-byte user message was not carried whole", len(huge))
	}
	results := itemsOf(turn.Items, ToolResult)
	shown, elided := 0, 0
	for _, r := range results {
		if r.Tool.ResultBytes != len(chunk) {
			t.Errorf("a result reports %d bytes, want its full %d whether or not its body is shown", r.Tool.ResultBytes, len(chunk))
		}
		if r.Elided && r.Tool.ResultElided && r.Tool.Result == "" {
			elided++
		} else if !r.Elided && r.Tool.Result == chunk {
			shown++
		}
	}
	// 2 MiB of budget holds three 600 KB results; the other three keep only their summary.
	if len(results) != 6 || shown != 3 || elided != 3 {
		t.Fatalf("of %d results %d are shown and %d elided, want 3 and 3 under a %d-byte budget", len(results), shown, elided, CollapsedBudget)
	}
}

func opencodeUnit(key, body string) (rec, src string) {
	return body, key + "@" + fmt.Sprintf("%012x", len(body))
}

// TestAnOpencodeStreamReadsEachUnitAtItsLastVersion: opencode mutates units in place, so the store
// holds one record per VERSION. The reading is each unit's LAST version, once, in first-seen order;
// a `task` call's child session is nested under it, its text classed as the parent agent's prompt.
func TestAnOpencodeStreamReadsEachUnitAtItsLastVersion(t *testing.T) {
	a := transcripttest.Open(t)
	sess := transcripttest.Session{Root: "ses_root0001", Owner: readOwner, Runtime: "opencode"}
	var recs, srcs []string
	add := func(key, body string) {
		r, s := opencodeUnit(key, body)
		recs, srcs = append(recs, r), append(srcs, s)
	}
	add("info", `{"id":"ses_root0001","title":"v1"}`)
	add("msg:msg_u1", `{"id":"msg_u1","role":"user"}`)
	add("prt:prt_1", `{"id":"prt_1","messageID":"msg_u1","type":"text","text":"please do the thing"}`)
	add("msg:msg_a1", `{"id":"msg_a1","role":"assistant"}`)
	add("prt:prt_2", `{"id":"prt_2","messageID":"msg_a1","type":"tool","tool":"bash","state":{"status":"running","input":{"command":"seq 1 3"}}}`)
	add("prt:prt_3", `{"id":"prt_3","messageID":"msg_a1","type":"text","text":"first draft"}`)
	add("prt:prt_4", `{"id":"prt_4","messageID":"msg_a1","type":"tool","tool":"task","state":{"status":"completed",`+
		`"input":{"description":"look it up","subagent_type":"general"},"output":"done","metadata":{"sessionId":"ses_child0001"}}}`)
	add("prt:prt_5", `{"id":"prt_5","messageID":"msg_a1","type":"step-finish","reason":"stop"}`)
	// Later versions of three units already stored.
	add("info", `{"id":"ses_root0001","title":"v2"}`)
	add("prt:prt_2", `{"id":"prt_2","messageID":"msg_a1","type":"tool","tool":"bash","state":{"status":"completed",`+
		`"input":{"command":"seq 1 3"},"output":"1\n2\n3\n","metadata":{"exit":3}}}`)
	add("prt:prt_3", `{"id":"prt_3","messageID":"msg_a1","type":"text","text":"final answer"}`)
	transcripttest.Append(t, a, sess, "main", recs, srcs...)
	transcripttest.Append(t, a, sess, "child:ses_child0001", []string{
		`{"id":"ses_child0001","parentID":"ses_root0001"}`, `{"id":"msg_c1","role":"user"}`,
		`{"id":"prt_c1","messageID":"msg_c1","type":"text","text":"the parent asked for this"}`,
	}, "info@000000000001", "msg:msg_c1@000000000002", "prt:prt_c1@000000000003")

	rd := readAll(t, a, "ses_root0001", 1)
	if rd.TotalTurns != 1 || len(rd.Turns) != 1 {
		t.Fatalf("%d turns, want 1", rd.TotalTurns)
	}
	turn := rd.Turns[0]
	var shape []string
	for _, it := range turn.Items {
		shape = append(shape, string(it.Class))
	}
	if got := strings.Join(shape, ","); got != "human-text,tool-call,assistant-text,tool-call" {
		t.Fatalf("the turn reads as %s, want one item per unit at its last version", got)
	}
	// info ×1 (its last version), two message infos, the step-finish.
	if turn.Bookkeeping != 4 {
		t.Errorf("counted %d bookkeeping records, want 4", turn.Bookkeeping)
	}
	bash, text, task := turn.Items[1], turn.Items[2], turn.Items[3]
	if bash.Tool.Status != "exit 3" || bash.Tool.Result != "1\n2\n3\n" || !bash.Tool.HasResult || bash.Seq != 10 {
		t.Errorf("the bash call reads %+v at seq %d, want its COMPLETED version (stored 10th)", *bash.Tool, bash.Seq)
	}
	if text.Text != "final answer" {
		t.Errorf("the assistant text reads %q, want its last version", text.Text)
	}
	c := task.Tool.Child
	if c == nil || c.Stream != "child:ses_child0001" || c.AgentType != "general" || c.Description != "look it up" {
		t.Fatalf("the task call's child is %+v", c)
	}
	if len(c.Items) != 1 || c.Items[0].Class != AgentPrompt || c.Items[0].Text != "the parent asked for this" || c.Bookkeeping != 2 {
		t.Errorf("the child reads as %+v with %d bookkeeping; want ONE agent-prompt and 2", c.Items, c.Bookkeeping)
	}
}

// TestAChildStreamIsCappedAndCounted: a subagent's stream is carried up to [ChildItemCap] items;
// the rest are counted, never silently dropped.
func TestAChildStreamIsCappedAndCounted(t *testing.T) {
	a := transcripttest.Open(t)
	sess := transcripttest.Session{Root: "s-0001", Owner: readOwner}
	transcripttest.Append(t, a, sess, "main", []string{
		claudeUser("go"),
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","id":"toolu_c0001","name":"Agent",` +
			`"input":{"description":"count things","subagent_type":"general-purpose"}}]}}`,
		`{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_c0001","content":"ok"}]},` +
			`"toolUseResult":{"agentId":"a1b2c3"}}`,
	})
	var lines []string
	for i := 0; i < ChildItemCap+50; i++ {
		lines = append(lines, strings.Replace(claudeAssistant(fmt.Sprintf("step %d", i)), `{"type"`, `{"isSidechain":true,"type"`, 1))
	}
	transcripttest.Append(t, a, sess, "subagent:a1b2c3", lines)
	rd := readAll(t, a, "s-0001", 1)
	call := itemsOf(rd.Turns[0].Items, ToolCall)
	if len(call) != 1 || call[0].Tool.Child == nil {
		t.Fatalf("the spawning call carries no child: %+v", rd.Turns[0].Items)
	}
	c := call[0].Tool.Child
	if c.Stream != "subagent:a1b2c3" || len(c.Items) != ChildItemCap || c.More != 50 || c.Items[ChildItemCap-1].Text != fmt.Sprintf("step %d", ChildItemCap-1) {
		t.Fatalf("the child carries %d items and counts %d more, want %d and 50", len(c.Items), c.More, ChildItemCap)
	}
}
