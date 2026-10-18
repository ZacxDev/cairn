package api

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/ZacxDev/cairn/internal/report"
)

// 🔴 THE ARC READ VIEWS OVER THE SERVED ANSWER: `?view=outline`, `?view=page&section=…`.
// Every test here talks HTTP and compares strings, so on a pod that predates the views each one is
// RED ON ITS OWN ASSERTION (the pod ignores the query and answers the full arc), never on a compile
// error. All names synthetic.

// viewArc is the registered fixture: 12 state lines, 46 ranked next steps (the measured per-list
// maximum), 7 members registered out of session order, homed in alpha, declaring beta, NOT opted in
// — so the beta-only reader may not see it.
var (
	viewSteps   []string
	viewState   []string
	viewMembers []string
)

func init() {
	for i := 0; i < 46; i++ {
		viewSteps = append(viewSteps, fmt.Sprintf("%d. widget step %02d: move the synthetic gadget lever and confirm the dial reads green", i+1, i))
	}
	for i := 0; i < 12; i++ {
		viewState = append(viewState, fmt.Sprintf("state %02d: the synthetic gadget is in a known condition", i))
	}
	for i := 7; i >= 1; i-- {
		role := "wrote"
		if i == 1 {
			role = "originated"
		}
		viewMembers = append(viewMembers, fmt.Sprintf(`{"session":"s-%04d","role":"%s","first_seen":"2000-01-0%dT00:00:00Z"}`, i, role, (i%3)+1))
	}
}

const viewCommit = "0123456789abcdef0123456789abcdef01234567"

func jsonList(items []string) string {
	quoted := make([]string, len(items))
	for i, s := range items {
		quoted[i] = `"` + s + `"`
	}
	return "[" + strings.Join(quoted, ",") + "]"
}

func viewPayload() string {
	return `{"schema":2,"status":"open","closing_kind":"check","declared_scopes":["beta-notes"],"visible_to_declared":false,` +
		`"members":[` + strings.Join(viewMembers, ",") + `],` +
		`"summary":{"closing_condition":"the gadget answers on the new port",` +
		`"state_now":` + jsonList(viewState) + `,` +
		`"next_steps":` + jsonList(viewSteps) + `,` +
		`"source":{"repo_path":"docs/handoff-gadget.md","commit":"` + viewCommit + `","committed_at":"2000-01-04T00:00:00Z"},` +
		`"task_refs":["tracker:T-1"],"aliases":["gadget-move"]}}`
}

const viewTarget = "/api/v1/arc/alpha-notes/gadget-move"

func viewHarness(t *testing.T) *harness {
	t.Helper()
	h := newHarness(t)
	withJournal(t, h)
	if put := h.do(t, "PUT", viewTarget, wideToken, nil, viewPayload()); put.status != 200 {
		t.Fatalf("register: %d %q", put.status, put.body)
	}
	return h
}

// report body = everything after the freshness paragraph, which carries the fixture files' mtimes.
func reportPart(t *testing.T, body string) string {
	t.Helper()
	_, rest, ok := strings.Cut(body, "\n\n")
	if !ok {
		t.Fatalf("no freshness paragraph in:\n%s", body)
	}
	return rest
}

// arcFullGoldenAtBase is the served report for `viewArc` with NO parameter, RECORDED FROM THE POD AS
// IT WAS BEFORE THE VIEWS EXISTED (the freshness paragraph above it is dropped: it carries mtimes).
// Not regenerated from the current renderer — that would compare the code with itself.
const arcFullGoldenAtBase = `cairn-arc: status=arc-found home=alpha-notes slug=gadget-move
  coverage: writes measured from entry trailers · reads NOT recorded (not collected in this phase)
  attribution: trailers are self-reported — actor as written in the entry (only appended bullets had it set by the pod); session ids are declared by the writer
  visibility: an arc is shown only when its HOME scope is readable to you — one homed in a scope you cannot read answers exactly like one never registered
  registration: arcs are pushed by the operator tooling at each handoff and are its own word — an arc nobody registered is not listed, and each is only as fresh as its last push

status: open
closing condition: check
registered: 2000-01-05T00:00:00Z by wide-reader · reported by the tool at an unstated time
tooling coverage: 0 of 0 commits carry no session id · writers: NOT measured · readers: NOT measured
declared scopes readable to you: alpha-notes, beta-notes (a declared scope you cannot read is omitted, not counted)
member writes: inferred from 0 of 2 bullets carrying a write trailer, across the 2 scope(s) readable to you

summary: the registering tool's own word, read from docs/handoff-gadget.md at commit 0123456789abcdef0123456789abcdef01234567 (committed 2000-01-04T00:00:00Z)
closing condition (stated): the gadget answers on the new port
state now: 12
  state 00: the synthetic gadget is in a known condition
  state 01: the synthetic gadget is in a known condition
  state 02: the synthetic gadget is in a known condition
  state 03: the synthetic gadget is in a known condition
  state 04: the synthetic gadget is in a known condition
  state 05: the synthetic gadget is in a known condition
  state 06: the synthetic gadget is in a known condition
  state 07: the synthetic gadget is in a known condition
  state 08: the synthetic gadget is in a known condition
  state 09: the synthetic gadget is in a known condition
  state 10: the synthetic gadget is in a known condition
  state 11: the synthetic gadget is in a known condition
next steps (ranked): 46
  1. widget step 00: move the synthetic gadget lever and confirm the dial reads green
  2. widget step 01: move the synthetic gadget lever and confirm the dial reads green
  3. widget step 02: move the synthetic gadget lever and confirm the dial reads green
  4. widget step 03: move the synthetic gadget lever and confirm the dial reads green
  5. widget step 04: move the synthetic gadget lever and confirm the dial reads green
  6. widget step 05: move the synthetic gadget lever and confirm the dial reads green
  7. widget step 06: move the synthetic gadget lever and confirm the dial reads green
  8. widget step 07: move the synthetic gadget lever and confirm the dial reads green
  9. widget step 08: move the synthetic gadget lever and confirm the dial reads green
  10. widget step 09: move the synthetic gadget lever and confirm the dial reads green
  11. widget step 10: move the synthetic gadget lever and confirm the dial reads green
  12. widget step 11: move the synthetic gadget lever and confirm the dial reads green
  13. widget step 12: move the synthetic gadget lever and confirm the dial reads green
  14. widget step 13: move the synthetic gadget lever and confirm the dial reads green
  15. widget step 14: move the synthetic gadget lever and confirm the dial reads green
  16. widget step 15: move the synthetic gadget lever and confirm the dial reads green
  17. widget step 16: move the synthetic gadget lever and confirm the dial reads green
  18. widget step 17: move the synthetic gadget lever and confirm the dial reads green
  19. widget step 18: move the synthetic gadget lever and confirm the dial reads green
  20. widget step 19: move the synthetic gadget lever and confirm the dial reads green
  21. widget step 20: move the synthetic gadget lever and confirm the dial reads green
  22. widget step 21: move the synthetic gadget lever and confirm the dial reads green
  23. widget step 22: move the synthetic gadget lever and confirm the dial reads green
  24. widget step 23: move the synthetic gadget lever and confirm the dial reads green
  25. widget step 24: move the synthetic gadget lever and confirm the dial reads green
  26. widget step 25: move the synthetic gadget lever and confirm the dial reads green
  27. widget step 26: move the synthetic gadget lever and confirm the dial reads green
  28. widget step 27: move the synthetic gadget lever and confirm the dial reads green
  29. widget step 28: move the synthetic gadget lever and confirm the dial reads green
  30. widget step 29: move the synthetic gadget lever and confirm the dial reads green
  31. widget step 30: move the synthetic gadget lever and confirm the dial reads green
  32. widget step 31: move the synthetic gadget lever and confirm the dial reads green
  33. widget step 32: move the synthetic gadget lever and confirm the dial reads green
  34. widget step 33: move the synthetic gadget lever and confirm the dial reads green
  35. widget step 34: move the synthetic gadget lever and confirm the dial reads green
  36. widget step 35: move the synthetic gadget lever and confirm the dial reads green
  37. widget step 36: move the synthetic gadget lever and confirm the dial reads green
  38. widget step 37: move the synthetic gadget lever and confirm the dial reads green
  39. widget step 38: move the synthetic gadget lever and confirm the dial reads green
  40. widget step 39: move the synthetic gadget lever and confirm the dial reads green
  41. widget step 40: move the synthetic gadget lever and confirm the dial reads green
  42. widget step 41: move the synthetic gadget lever and confirm the dial reads green
  43. widget step 42: move the synthetic gadget lever and confirm the dial reads green
  44. widget step 43: move the synthetic gadget lever and confirm the dial reads green
  45. widget step 44: move the synthetic gadget lever and confirm the dial reads green
  46. widget step 45: move the synthetic gadget lever and confirm the dial reads green
task refs: tracker:T-1
aliases: gadget-move

members: 7, ordered by session id (byte-wise)
- s-0001 · originated · first seen 2000-01-02T00:00:00Z · wrote in: none readable to you
- s-0002 · wrote · first seen 2000-01-03T00:00:00Z · wrote in: none readable to you
- s-0003 · wrote · first seen 2000-01-01T00:00:00Z · wrote in: none readable to you
- s-0004 · wrote · first seen 2000-01-02T00:00:00Z · wrote in: none readable to you
- s-0005 · wrote · first seen 2000-01-03T00:00:00Z · wrote in: none readable to you
- s-0006 · wrote · first seen 2000-01-01T00:00:00Z · wrote in: none readable to you
- s-0007 · wrote · first seen 2000-01-02T00:00:00Z · wrote in: none readable to you
`

// TestTheFullViewIsByteIdenticalToThePreViewsAnswer: no parameter, and `view=full`, serve exactly
// the bytes the pod served before the views existed. ⚠ AN INVARIANT GUARD, LABELLED AS ONE: it is
// green on the pre-views pod too (which ignores `view=`); it exists so the views cannot move the
// default. The fixture is large enough to carry every block the full answer prints.
func TestTheFullViewIsByteIdenticalToThePreViewsAnswer(t *testing.T) {
	h := viewHarness(t)
	for _, q := range []string{"", "?view=full"} {
		got := h.do(t, "GET", viewTarget+q, wideToken, nil, "")
		if got.status != 200 || got.headers.Get("X-Store-Status") != report.StatusArcFound {
			t.Fatalf("%q: %d %s", q, got.status, got.headers.Get("X-Store-Status"))
		}
		if body := reportPart(t, got.body); body != arcFullGoldenAtBase {
			t.Fatalf("%q: the full view moved:\n%s\n--- golden\n%s", q, body, arcFullGoldenAtBase)
		}
	}
}

// TestAPlainRequestCarriesNoViewHeader: a request that names no view gets the pre-views HEADERS
// too, not only the pre-views body — the exact set of `X-` headers the pod sent before the views
// existed, and no `X-Arc-View`. (Red on a build that sent the header on every `arc-found` answer.)
func TestAPlainRequestCarriesNoViewHeader(t *testing.T) {
	h := viewHarness(t)
	got := h.do(t, "GET", viewTarget, wideToken, nil, "")
	var names []string
	for name := range got.headers {
		if strings.HasPrefix(name, "X-") {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	if got.headers.Get("X-Store-Status") != report.StatusArcFound ||
		strings.Join(names, ",") != "X-Store-Exit,X-Store-Revision,X-Store-Snapshot,X-Store-Status" {
		t.Fatalf("a plain request must carry exactly the pre-views X- headers, got %v (status %s)",
			names, got.headers.Get("X-Store-Status"))
	}
}

// TestAViewRequestNamesItsView: a request that names a view — `full` included — gets `X-Arc-View`
// naming the view rendered; a client that asked for a view and finds no header knows the pod predates
// the views.
func TestAViewRequestNamesItsView(t *testing.T) {
	h := viewHarness(t)
	for q, want := range map[string]string{
		"?view=full":                    "full",
		"?view=outline":                 "outline",
		"?view=page&section=next_steps": "page",
	} {
		got := h.do(t, "GET", viewTarget+q, wideToken, nil, "")
		if got.status != 200 || got.headers.Get("X-Arc-View") != want {
			t.Fatalf("%q: X-Arc-View %q, want %q (status %d)", q, got.headers.Get("X-Arc-View"), want, got.status)
		}
	}
}

// TestAPageStartsAtItsFromAndTagsTheStoredIndex is the regression the views exist for: page 40 of
// 46 ranked steps carries steps 40..45, each tagged with its STORED index, and none of the earlier
// ones, and says it reached the end.
func TestAPageStartsAtItsFromAndTagsTheStoredIndex(t *testing.T) {
	h := viewHarness(t)
	got := h.do(t, "GET", viewTarget+"?view=page&section=next_steps&from=40", wideToken, nil, "")
	if got.headers.Get("X-Arc-View") != "page" {
		t.Fatalf("X-Arc-View %q, want page", got.headers.Get("X-Arc-View"))
	}
	body := reportPart(t, got.body)
	if !strings.Contains(body, "\n[next_steps#40] "+viewSteps[40]+"\n") {
		t.Fatalf("page 40 must carry item 40 tagged with its stored index:\n%s", body)
	}
	if strings.Contains(body, viewSteps[0]) || strings.Contains(body, viewSteps[39]) || strings.Contains(body, viewState[0]) {
		t.Fatalf("page 40 carried an item before 40, or another section:\n%s", body)
	}
	wantTail := "next_steps: items 40-45 of 46\n"
	for i := 40; i < 46; i++ {
		wantTail += fmt.Sprintf("[next_steps#%d] %s\n", i, viewSteps[i])
	}
	wantTail += "end\n"
	if !strings.HasSuffix(body, wantTail) {
		t.Fatalf("page tail:\n%s\n--- want suffix\n%s", body, wantTail)
	}
}

// TestAPageIsBoundedByLimitAndBytesAndAlwaysAdvances: the default limit stops a page at 20 items and
// names where the next one starts; a byte budget smaller than ONE item still returns that item (a page
// that returned nothing would never advance); `from` at the count is `end` with no item.
func TestAPageIsBoundedByLimitAndBytesAndAlwaysAdvances(t *testing.T) {
	h := viewHarness(t)
	first := reportPart(t, h.do(t, "GET", viewTarget+"?view=page&section=next_steps", wideToken, nil, "").body)
	if !strings.Contains(first, "[next_steps#19] "+viewSteps[19]+"\n") || strings.Contains(first, "[next_steps#20]") ||
		!strings.HasSuffix(first, "\nmore: from=20\n") {
		t.Fatalf("the default page is items 0..19, then `more: from=20`:\n%s", first)
	}
	tiny := reportPart(t, h.do(t, "GET", viewTarget+"?view=page&section=state_now&from=3&max_bytes=1", wideToken, nil, "").body)
	if !strings.HasSuffix(tiny, "state_now: items 3-3 of 12\n[state_now#3] "+viewState[3]+"\nmore: from=4\n") {
		t.Fatalf("max_bytes below one item must still return exactly that item:\n%s", tiny)
	}
	// Control on the byte bound itself: a budget that holds exactly two rendered lines returns two.
	two := len(fmt.Sprintf("[state_now#0] %s\n[state_now#1] %s\n", viewState[0], viewState[1]))
	pair := reportPart(t, h.do(t, "GET", viewTarget+fmt.Sprintf("?view=page&section=state_now&max_bytes=%d", two), wideToken, nil, "").body)
	if !strings.HasSuffix(pair, "[state_now#1] "+viewState[1]+"\nmore: from=2\n") {
		t.Fatalf("a budget of exactly two lines returns two:\n%s", pair)
	}
	end := reportPart(t, h.do(t, "GET", viewTarget+"?view=page&section=next_steps&from=46", wideToken, nil, "").body)
	if !strings.HasSuffix(end, "\nnext_steps: no items from 46 (46 in all)\nend\n") || strings.Contains(end, "[next_steps#") {
		t.Fatalf("from=count is `end` with no item:\n%s", end)
	}
	members := reportPart(t, h.do(t, "GET", viewTarget+"?view=page&section=members&from=5", wideToken, nil, "").body)
	if !strings.HasSuffix(members, "members: items 5-6 of 7\n"+
		"[members#5] s-0006 · wrote · first seen 2000-01-01T00:00:00Z · wrote in: none readable to you\n"+
		"[members#6] s-0007 · wrote · first seen 2000-01-02T00:00:00Z · wrote in: none readable to you\nend\n") {
		t.Fatalf("members page, in session-id order:\n%s", members)
	}
}

// TestAPageReadAtAnotherCommitSaysTheSummaryChanged: `at=` that is not the stored summary's commit
// adds one line telling the reader to re-read the outline; the stored commit adds nothing (control).
func TestAPageReadAtAnotherCommitSaysTheSummaryChanged(t *testing.T) {
	h := viewHarness(t)
	other := strings.Repeat("ab", 20)
	stale := reportPart(t, h.do(t, "GET", viewTarget+"?view=page&section=next_steps&from=2&at="+other, wideToken, nil, "").body)
	line := "\nsummary changed since " + other + "; re-read the outline\n"
	if !strings.Contains(stale, line) {
		t.Fatalf("a page read at another commit must say so:\n%s", stale)
	}
	fresh := reportPart(t, h.do(t, "GET", viewTarget+"?view=page&section=next_steps&from=2&at="+viewCommit, wideToken, nil, "").body)
	if strings.Contains(fresh, "summary changed since") || !strings.Contains(fresh, "[next_steps#2] ") {
		t.Fatalf("CONTROL: at the stored commit there is no change line:\n%s", fresh)
	}
}

// TestTheOutlineSizesEachSectionAndCarriesNoItem pins the outline's own lines: the summary's short
// fields inline, one size line per section (bytes = the items' own bytes, computed here from the
// fixture), the paging hint naming the stored commit — and no item text.
func TestTheOutlineSizesEachSectionAndCarriesNoItem(t *testing.T) {
	h := viewHarness(t)
	got := h.do(t, "GET", viewTarget+"?view=outline", wideToken, nil, "")
	body := reportPart(t, got.body)
	size := func(items []string) int {
		n := 0
		for _, s := range items {
			n += len(s)
		}
		return n
	}
	want := "summary: the registering tool's own word, read from docs/handoff-gadget.md at commit " + viewCommit + " (committed 2000-01-04T00:00:00Z)\n" +
		"closing condition (stated): the gadget answers on the new port\n" +
		"task refs: tracker:T-1\naliases: gadget-move\n\n" +
		fmt.Sprintf("state_now: 12 items, %d B\nnext_steps: 46 items, %d B\nmembers: 7\n", size(viewState), size(viewSteps)) +
		"page a section: ?view=page&section=<state_now|next_steps|members>&from=0&at=" + viewCommit +
		" (limit default 20, at most 200; max_bytes default 8192) — or cairn arc-show --section <name> --from 0\n"
	if !strings.HasSuffix(body, want) || !strings.HasPrefix(body, "cairn-arc: status=arc-found home=alpha-notes slug=gadget-move view=outline\n") {
		t.Fatalf("outline:\n%s\n--- want suffix\n%s", body, want)
	}
	for _, item := range []string{viewSteps[0], viewSteps[45], viewState[0], "s-0001"} {
		if strings.Contains(body, item) {
			t.Fatalf("the outline must carry no item, found %q:\n%s", item, body)
		}
	}
}

// TestEveryViewOfAHiddenArcIsTheAbsentAnswer: for the beta-only reader the alpha-homed arc (not
// opted in) and a never-registered key answer the SAME status, the SAME body and NO `X-Arc-View`, in
// every view — so no view can tell hidden from absent. The POSITIVE CONTROL is the home reader, who
// gets `arc-found` in every arm and the view header whenever a view was named, so "nobody sees
// anything" cannot pass.
func TestEveryViewOfAHiddenArcIsTheAbsentAnswer(t *testing.T) {
	h := viewHarness(t)
	for q, view := range map[string]string{
		"":                                      "", // a plain request carries no view header at all
		"?view=outline":                         "outline",
		"?view=page&section=next_steps&from=40": "page",
		"?view=page&section=members&limit=1":    "page",
		"?view=full":                            "full",
	} {
		seen := h.do(t, "GET", viewTarget+q, wideToken, nil, "")
		if seen.headers.Get("X-Store-Status") != report.StatusArcFound || seen.headers.Get("X-Arc-View") != view {
			t.Fatalf("POSITIVE CONTROL %q: the home reader must see the arc in view %s: %s %q", q, view,
				seen.headers.Get("X-Store-Status"), seen.headers.Get("X-Arc-View"))
		}
		hidden := h.do(t, "GET", viewTarget+q, narrowToken, nil, "")
		absent := h.do(t, "GET", "/api/v1/arc/alpha-notes/never-filed"+q, narrowToken, nil, "")
		for _, got := range []reply{hidden, absent} {
			if got.status != 200 || got.headers.Get("X-Store-Status") != report.StatusArcUnregistered || got.headers.Get("X-Arc-View") != "" {
				t.Fatalf("%q: a hidden or absent arc is arc-unregistered with no view header: %d %s %q", q,
					got.status, got.headers.Get("X-Store-Status"), got.headers.Get("X-Arc-View"))
			}
		}
		if reportPart(t, hidden.body) != reportPart(t, absent.body) {
			t.Fatalf("%q: hidden differs from absent:\n%s\n--- absent\n%s", q, hidden.body, absent.body)
		}
	}
}

// TestABadViewParameterIs400ForEveryArc: every refusal that depends on the parameters alone is a 400
// with its own sentence — and the same 400 for a visible arc, a hidden one and an absent one, so a
// refusal reveals nothing either.
func TestABadViewParameterIs400ForEveryArc(t *testing.T) {
	h := viewHarness(t)
	for q, want := range map[string]string{
		"?view=page&section=next_steps&limit=0":     "limit must be an integer from 1 to 200, got 0",
		"?view=page&section=next_steps&limit=-1":    "limit must be an integer from 1 to 200, got -1",
		"?view=page&section=next_steps&limit=201":   "limit must be an integer from 1 to 200, got 201",
		"?view=page&section=next_steps&limit=x":     "limit must be an integer, got 'x'",
		"?view=page&section=next_steps&max_bytes=0": "max_bytes must be an integer from 1 to 65536, got 0",
		"?view=page&section=next_steps&from=-1":     "from must be an integer >= 0, got -1",
		"?view=page&section=nowhere":                `view=page needs section, one of state_now, next_steps, members, got "nowhere"`,
		"?view=page":                                `view=page needs section, one of state_now, next_steps, members, got ""`,
		"?view=sideways":                            `view must be one of full, outline, page, got "sideways"`,
		"?view=outline&from=3":                      "from applies only to view=page, got view=outline",
		"?section=next_steps":                       "section applies only to view=page, got view=full",
		"?view=page&section=next_steps&at=HEAD":     `at must be a 40- or 64-character lowercase hex commit, got "HEAD"`,
	} {
		for _, target := range []string{viewTarget, "/api/v1/arc/alpha-notes/never-filed"} {
			for _, token := range []string{wideToken, narrowToken} {
				got := h.do(t, "GET", target+q, token, nil, "")
				if got.status != 400 || got.body != "bad request: "+want+"\n" || got.headers.Get("X-Store-Status") != "bad-request" {
					t.Fatalf("%s%q as %.6s…: %d %q, want 400 %q", target, q, token, got.status, got.body, want)
				}
			}
		}
	}
}
