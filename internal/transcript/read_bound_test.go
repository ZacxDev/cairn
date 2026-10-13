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
// calls — larger than any page can carry — carries the first PageItemCap calls, each WITH its
// result, and counts the other 500: a call and its result are one, so no result of a left-out call
// appears on its own, and none is counted twice. Since round 3 the page then ENDS: the next turn is
// not squeezed onto it as a bare user message and a count, it starts the next page, whole.
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
	if len(rd.Turns) != 1 || rd.TotalTurns != 2 || rd.NextTurn != 2 {
		t.Fatalf("%d turns of %d, next page at turn %d; want turn 1 alone, then turn 2", len(rd.Turns), rd.TotalTurns, rd.NextTurn)
	}
	first := rd.Turns[0]
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
	page2 := readAll(t, a, "s-0001", rd.NextTurn)
	if len(page2.Turns) != 1 || page2.NextTurn != 0 {
		t.Fatalf("the next page holds %d turns and points on to %d, want turn 2 alone and nothing after", len(page2.Turns), page2.NextTurn)
	}
	second := page2.Turns[0]
	if human := itemsOf(second.Items, HumanText); len(human) != 1 || human[0].Text != "and then this" {
		t.Errorf("turn 2's user message is not on its page: %+v", second.Items)
	}
	if said := itemsOf(second.Items, AssistantText); len(said) != 1 || said[0].Text != "done" || second.More != 0 {
		t.Errorf("turn 2 carries %d assistant items and counts %d more, want its one item and 0", len(said), second.More)
	}
}

// TestEveryCallIsReachableAcrossPages (#261 round 3): fifty turns of thirty calls are 1,500 items,
// over [PageItemCap], and every one of them must be reachable in the COLLAPSED reading by following
// pages from turn 1. Round 2 put all fifty turns on one page and counted every item past the
// thousandth — turns 34 onward showed their user message and "N more items" — and `?turn=` of any
// of them folded back onto that same page, so those calls were reachable only as raw records.
//
// RED on #261 round 2's head (6dd486e): one page, turn 34 carried 10 of its 30 calls and turns
// 35–50 none.
func TestEveryCallIsReachableAcrossPages(t *testing.T) {
	const turns, perTurn = 50, 30
	a := transcripttest.Open(t)
	sess := transcripttest.Session{Root: "s-0001", Owner: readOwner}
	var lines []string
	for n := 1; n <= turns; n++ {
		lines = append(lines, claudeUser(fmt.Sprintf("turn %d", n)))
		for i := range perTurn {
			id := fmt.Sprintf("toolu_t%02dc%02d", n, i)
			lines = append(lines, claudeCall(id, fmt.Sprintf("echo %d.%d", n, i)), claudeResult(id, fmt.Sprintf("ok %d.%d", n, i)))
		}
	}
	transcripttest.Append(t, a, sess, "main", lines)
	seen := map[string]bool{}
	pages, start := 0, 1
	for start != 0 && pages < turns {
		rd := readAll(t, a, "s-0001", start)
		pages++
		if rd.FirstTurn != start || len(rd.Turns) == 0 {
			t.Fatalf("?turn=%d read a page starting at %d with %d turns", start, rd.FirstTurn, len(rd.Turns))
		}
		for i, turn := range rd.Turns {
			if turn.N != start+i || turn.More != 0 {
				t.Errorf("page %d: turn %d (want %d) counts %d items it does not carry", pages, turn.N, start+i, turn.More)
			}
			for _, c := range itemsOf(turn.Items, ToolCall) {
				if !c.Tool.HasResult {
					t.Errorf("turn %d: a call is carried without its result", turn.N)
				}
				seen[c.Tool.ID] = true
			}
		}
		next := rd.FirstTurn + len(rd.Turns)
		if next > rd.TotalTurns {
			next = 0
		}
		if rd.NextTurn != next {
			t.Fatalf("page %d says the next page starts at %d, but it carries turns %d–%d of %d",
				pages, rd.NextTurn, rd.FirstTurn, next-1, rd.TotalTurns)
		}
		start = next
	}
	if len(seen) != turns*perTurn {
		t.Fatalf("%d of %d calls are reachable in the collapsed reading over %d page(s)", len(seen), turns*perTurn, pages)
	}
	// CONTROL: the session does not fit one page, so the walk above crossed a page boundary.
	if pages < 2 {
		t.Fatalf("CONTROL: %d calls fit one page, so no page boundary was crossed", turns*perTurn)
	}
	t.Logf("%d calls over %d pages", len(seen), pages)
}

// TestATurnWhoseChildDoesNotFitStartsTheNextPage (#261 round 3): a child stream spends the page's
// allowance like the turn's own items, so a turn whose OWN items fit and whose child does not is not
// carried either — the page ends before it and the child is read whole on the next page. Turn 1
// holds 900 calls; turn 2 one call spawning a 150-item subagent, which the 100 left cannot hold.
// The control: read from turn 2, the child carries all 150.
func TestATurnWhoseChildDoesNotFitStartsTheNextPage(t *testing.T) {
	a := transcripttest.Open(t)
	sess := transcripttest.Session{Root: "s-0001", Owner: readOwner}
	lines := []string{claudeUser("turn 1")}
	for i := range 900 {
		id := fmt.Sprintf("toolu_q%04d", i)
		lines = append(lines, claudeCall(id, "echo"), claudeResult(id, "ok"))
	}
	lines = append(lines, claudeUser("turn 2"),
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","id":"toolu_q9999","name":"Agent",`+
			`"input":{"description":"child","subagent_type":"general-purpose"}}]}}`,
		`{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_q9999","content":"ok"}]},`+
			`"toolUseResult":{"agentId":"c0001"}}`)
	transcripttest.Append(t, a, sess, "main", lines)
	var child []string
	for i := range 150 {
		child = append(child, sidechain(claudeAssistant(fmt.Sprintf("step %d", i))))
	}
	transcripttest.Append(t, a, sess, "subagent:c0001", child)

	rd := readAll(t, a, "s-0001", 1)
	if len(rd.Turns) != 1 || rd.NextTurn != 2 || rd.Turns[0].More != 0 {
		t.Fatalf("page one holds %d turn(s), counts %d, next page at %d; want turn 1 whole and turn 2 next",
			len(rd.Turns), rd.Turns[0].More, rd.NextTurn)
	}
	page2 := readAll(t, a, "s-0001", 2)
	calls := itemsOf(page2.Turns[0].Items, ToolCall)
	if len(calls) != 1 || calls[0].Tool.Child == nil {
		t.Fatalf("CONTROL: turn 2 does not carry its spawning call with a child")
	}
	if c := calls[0].Tool.Child; len(c.Items) != 150 || c.More != 0 {
		t.Fatalf("on its own page the child carries %d and counts %d, want 150 and 0", len(c.Items), c.More)
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
