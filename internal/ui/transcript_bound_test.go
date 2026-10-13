package ui

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/ZacxDev/cairn/internal/transcript/transcripttest"
)

// TestOneTurnOfTwentyThousandCallsIsABoundedPage (#261 round 2): the page bounded TURNS (fifty) and
// collapsed BYTES (2 MiB), and nothing bounded the ITEMS of one turn — a turn of 20,000 small tool
// calls rendered every one, a 7.4 MB page. The bound stated here is 1 MiB for that turn, and the
// items it does not carry are counted on the page with a link to the raw records they are in.
//
// The calls are tiny on purpose: each fits the collapsed-bytes budget many times over, so only an
// ITEM bound can keep the page small, and a byte budget alone (the pre-fix state) cannot pass.
func TestOneTurnOfTwentyThousandCallsIsABoundedPage(t *testing.T) {
	const calls, bound = 20_000, 1 << 20
	w := transcriptWorld(t)
	a := transcripttest.OpenAt(t, w.clock.Now)
	lines := make([]string, 0, 1+2*calls)
	lines = append(lines, trUser("run them all"))
	for i := range calls {
		lines = append(lines,
			fmt.Sprintf(`{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","id":"toolu_n%05d",`+
				`"name":"Bash","input":{"command":"echo %d"}}]}}`, i, i),
			fmt.Sprintf(`{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_n%05d",`+
				`"content":"ok %d"}]}}`, i, i))
	}
	for len(lines) > 0 { // one upload carries at most 10,000 records
		n := min(len(lines), 10_000)
		transcripttest.Append(t, a, w.session("s-render-03"), "main", lines[:n])
		lines = lines[n:]
	}
	rec := getAs(t, w.server(t, "owner", a), sessionURL("s-render-03"))
	if rec.Code != http.StatusOK {
		t.Fatalf("the page answered %d", rec.Code)
	}
	body := rec.Body.String()
	t.Logf("one turn of %d calls renders a %d-byte page", calls, len(body))
	// POSITIVE CONTROL: the turn is on the page and its first call is rendered, so the size below is
	// a page that carries the turn, not an empty one.
	if !strings.Contains(body, "run them all") || !strings.Contains(body, "echo 0") {
		t.Fatal("CONTROL: the turn or its first call is not on the page")
	}
	if len(body) > bound {
		t.Fatalf("one turn of %d tool calls rendered a %d-byte page, over the %d-byte bound", calls, len(body), bound)
	}
	if !strings.Contains(visibleText(body), "19000 more items in this turn") {
		t.Error("the page does not say how many of the turn's items it did not carry")
	}
	if !strings.Contains(body, `href="/session/transcript?seq=2002&amp;session=s-render-03&amp;stream=main&amp;to=40001"`) {
		t.Error("the count of items not carried does not link to the raw records that hold them (from call 1,000's record, seq 2002, to the turn's last)")
	}
}

// TestEveryCallIsReachableByFollowingThePager (#261 round 3): fifty turns of thirty calls are
// 1,500 items, over the page's 1,000-item allowance. Following the transcript pager's NEXT link from
// the session page must render every call's command in the collapsed view, and no page may say a
// turn holds items it does not carry. Round 2 rendered all fifty turns on one page with turns 34–50
// reduced to "N more items in this turn", and no link reached them: `?turn=34` folded back onto the
// same page.
//
// RED on #261 round 2's head (6dd486e): one page, 1,000 of the 1,500 commands.
func TestEveryCallIsReachableByFollowingThePager(t *testing.T) {
	const turns, perTurn = 50, 30
	w := transcriptWorld(t)
	a := transcripttest.OpenAt(t, w.clock.Now)
	var lines []string
	for n := 1; n <= turns; n++ {
		lines = append(lines, trUser(fmt.Sprintf("message number %d.", n)))
		for i := range perTurn {
			lines = append(lines,
				fmt.Sprintf(`{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","id":"toolu_r%02d%02d",`+
					`"name":"Bash","input":{"command":"echo c%d-%d."}}]}}`, n, i, n, i),
				fmt.Sprintf(`{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_r%02d%02d",`+
					`"content":"ok"}]}}`, n, i))
		}
	}
	transcripttest.Append(t, a, w.session("s-render-03"), "main", lines)
	srv := w.server(t, "owner", a)
	// The fragment is the browser's, never sent: the request carries the path and query only.
	next := regexp.MustCompile(`href="([^"#]*turn=\d+)#transcript">from turn \d+ →`)
	seen := map[string]bool{}
	url, pages := sessionURL("s-render-03"), 0
	for url != "" && pages < turns {
		body := getAs(t, srv, url).Body.String()
		pages++
		if strings.Contains(visibleText(body), "more items in this turn") || strings.Contains(visibleText(body), "more item in this turn") {
			t.Errorf("page %d (%s) says a turn holds items it does not carry", pages, url)
		}
		for _, m := range regexp.MustCompile(`echo c(\d+-\d+)\.`).FindAllStringSubmatch(body, -1) {
			seen[m[1]] = true
		}
		url = ""
		if m := next.FindStringSubmatch(body); m != nil {
			url = strings.ReplaceAll(m[1], "&amp;", "&")
		}
	}
	if len(seen) != turns*perTurn {
		t.Fatalf("following the pager over %d page(s) renders %d of the %d calls", pages, len(seen), turns*perTurn)
	}
	if pages < 2 {
		t.Fatalf("CONTROL: %d calls rendered on one page, so no page boundary was crossed", turns*perTurn)
	}
	t.Logf("%d calls over %d pages", len(seen), pages)
}
