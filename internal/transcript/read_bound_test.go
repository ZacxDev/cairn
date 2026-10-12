package transcript

import (
	"fmt"
	"strings"
	"testing"

	"github.com/ZacxDev/cairn/internal/transcript/transcripttest"
)

func claudeCall(id, input string) string {
	return `{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","id":"` + id +
		`","name":"Bash","input":{"command":` + jsonString(input) + `}}]}}`
}

func claudeResult(id, content string) string {
	return `{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"` + id +
		`","content":` + jsonString(content) + `}]}}`
}

func sidechain(line string) string {
	return strings.Replace(line, `{"type"`, `{"isSidechain":true,"type"`, 1)
}

// TestATurnPastThePageItemCapIsCountedNotCarried (#261 round 2): one turn of PageItemCap+500 tool
// calls carries the first PageItemCap calls, each WITH its result, and counts the other 500 — a call
// and its result are one, so no result of a left-out call appears on its own, and none is counted
// twice. The user message of the NEXT turn is carried although the allowance is spent (O9).
func TestATurnPastThePageItemCapIsCountedNotCarried(t *testing.T) {
	if PageItemCap != 1000 {
		t.Fatalf("PageItemCap is %d; this test and the page's stated bound are written for 1000", PageItemCap)
	}
	a := transcripttest.Open(t)
	sess := transcripttest.Session{Root: "s-0001", Owner: readOwner}
	lines := []string{claudeUser("run them all")}
	for i := range 1500 {
		id := fmt.Sprintf("toolu_p%05d", i)
		lines = append(lines, claudeCall(id, fmt.Sprintf("echo %d", i)), claudeResult(id, fmt.Sprintf("ok %d", i)))
	}
	lines = append(lines, claudeUser("and then this"), claudeAssistant("done"))
	transcripttest.Append(t, a, sess, "main", lines)
	rd := readAll(t, a, "s-0001", 1)
	if len(rd.Turns) != 2 {
		t.Fatalf("%d turns, want 2", len(rd.Turns))
	}
	first, second := rd.Turns[0], rd.Turns[1]
	calls := itemsOf(first.Items, ToolCall)
	if len(calls) != 1000 || len(itemsOf(first.Items, ToolResult)) != 0 || first.More != 500 {
		t.Fatalf("turn 1 carries %d calls and %d lone results and counts %d more; want 1000, 0 and 500",
			len(calls), len(itemsOf(first.Items, ToolResult)), first.More)
	}
	if last := calls[999].Tool; !last.HasResult || last.Result != "ok 999" {
		t.Errorf("the last carried call has no result folded in: %+v", last)
	}
	// Call 1000 is the first left out: user at seq 1, call i at 2+2i.
	if first.MoreFrom != 2002 {
		t.Errorf("MoreFrom = %d, want 2002 (call 1000's record)", first.MoreFrom)
	}
	if human := itemsOf(second.Items, HumanText); len(human) != 1 || human[0].Text != "and then this" {
		t.Errorf("turn 2's user message was not carried after the allowance ran out: %+v", second.Items)
	}
	if len(itemsOf(second.Items, AssistantText)) != 0 || second.More != 1 {
		t.Errorf("turn 2 carries %d assistant items and counts %d more, want 0 and 1 (the allowance is spent)",
			len(itemsOf(second.Items, AssistantText)), second.More)
	}
}

// TestAnEarlierPageDoesNotSpendThisPagesBudget (#261 round 2): every record is classified — and its
// collapsed body charged — before the reading knows which page it is on, and nothing refunded the
// records of other pages, so page two of a session whose page one held 2.4 MB of results showed its
// own 600 KB result elided with the whole budget to spare. CONTROL: page one, read itself, is
// budgeted as before (two of its three results shown).
func TestAnEarlierPageDoesNotSpendThisPagesBudget(t *testing.T) {
	a := transcripttest.Open(t)
	sess := transcripttest.Session{Root: "s-0001", Owner: readOwner}
	big := strings.Repeat("lorem ipsum dolor sit amet ", 800_000/27+1)[:800_000]
	mid := big[:600_000]
	lines := []string{claudeUser("turn 1")}
	for i := range 3 {
		lines = append(lines, claudeResult(fmt.Sprintf("toolu_e%04d", i), big))
	}
	for n := 2; n <= TurnsPerPage; n++ {
		lines = append(lines, claudeUser(fmt.Sprintf("turn %d", n)))
	}
	lines = append(lines, claudeUser("turn 51"), claudeResult("toolu_e9999", mid))
	transcripttest.Append(t, a, sess, "main", lines)

	shown := func(rd Reading) (n int) {
		for _, turn := range rd.Turns {
			for _, r := range itemsOf(turn.Items, ToolResult) {
				if !r.Elided {
					n++
				}
			}
		}
		return n
	}
	if got := shown(readAll(t, a, "s-0001", 1)); got != 2 {
		t.Fatalf("CONTROL: page one shows %d of its three 800 KB results, want 2 under a %d-byte budget", got, CollapsedBudget)
	}
	page2 := readAll(t, a, "s-0001", TurnsPerPage+1)
	if len(page2.Turns) != 1 || page2.Turns[0].N != 51 {
		t.Fatalf("page two holds %d turns, want turn 51 alone", len(page2.Turns))
	}
	if got := shown(page2); got != 1 {
		t.Errorf("page two's one 600 KB result is elided: page one's results spent its budget")
	}
}

// TestAChildsItemsPastItsCapAreNotCharged (#261 round 2): a child stream's items past ChildItemCap
// were charged to the page's budget and then dropped, so a later child's result was elided though
// the page carried far less than the budget. Here the first child holds 300 calls of ~8 KB (200
// carried, ~1.6 MB) and a second child one call of 300 KB, which fits what the page carries.
func TestAChildsItemsPastItsCapAreNotCharged(t *testing.T) {
	a := transcripttest.Open(t)
	sess := transcripttest.Session{Root: "s-0001", Owner: readOwner}
	spawn := func(n int) []string {
		id := fmt.Sprintf("toolu_s%04d", n)
		return []string{
			`{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","id":"` + id + `","name":"Agent",` +
				`"input":{"description":"child","subagent_type":"general-purpose"}}]}}`,
			`{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"` + id + `","content":"ok"}]},` +
				fmt.Sprintf(`"toolUseResult":{"agentId":"c%04d"}}`, n),
		}
	}
	main := append([]string{claudeUser("go")}, spawn(1)...)
	transcripttest.Append(t, a, sess, "main", append(main, spawn(2)...))
	eight := strings.Repeat("x", 8000)
	var first []string
	for i := range ChildItemCap + 100 {
		first = append(first, sidechain(claudeCall(fmt.Sprintf("toolu_k%04d", i), eight)))
	}
	transcripttest.Append(t, a, sess, "subagent:c0001", first)
	transcripttest.Append(t, a, sess, "subagent:c0002", []string{sidechain(claudeCall("toolu_k9999", strings.Repeat("y", 300_000)))})

	calls := itemsOf(readAll(t, a, "s-0001", 1).Turns[0].Items, ToolCall)
	if len(calls) != 2 || calls[0].Tool.Child == nil || calls[1].Tool.Child == nil {
		t.Fatalf("the two spawning calls do not both carry a child: %d calls", len(calls))
	}
	c1, c2 := calls[0].Tool.Child, calls[1].Tool.Child
	if len(c1.Items) != ChildItemCap || c1.More != 100 {
		t.Fatalf("the first child carries %d and counts %d, want %d and 100", len(c1.Items), c1.More, ChildItemCap)
	}
	if len(c2.Items) != 1 || c2.Items[0].Elided {
		t.Errorf("the second child's 300 KB call is elided: the first child's %d uncarried items spent the budget", c1.More)
	}
}
