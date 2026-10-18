package api

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/ZacxDev/cairn/internal/report"
)

// 🔴 ARC SCHEMA 2 OVER THE SERVED ANSWER: the declared-scope opt-in as a visibility matrix, the
// listing and the show agreeing about it, and the summary rendered. All names synthetic; the wide
// reader reads and writes alpha+beta, the narrow one only beta.

const summaryOf = `"summary":{"closing_condition":"the gadget answers on the new port",` +
	`"state_now":["the port moved","the old listener is still up"],` +
	`"next_steps":["drain the old listener","close the arc"],` +
	`"source":{"repo_path":"docs/handoff-gadget.md","commit":"` + "0123456789abcdef0123456789abcdef01234567" + `","committed_at":"2000-01-04T00:00:00Z"},` +
	`"task_refs":["tracker:T-1"],"aliases":["Gadget Move","gadget-move"]}`

// arcV2 is an arc homed in ALPHA that declares BETA, with or without the opt-in.
func arcV2(optIn bool) string {
	flag := "false"
	if optIn {
		flag = "true"
	}
	return `{"schema":2,"status":"open","closing_kind":"check","declared_scopes":["beta-notes"],` +
		`"visible_to_declared":` + flag + `,` + summaryOf + `}`
}

// TestTheDeclaredScopeOptInIsAVisibilityMatrix: an arc homed in alpha that declares beta.
//   - not opted in: the beta-only reader gets arc-unregistered (byte-identical to absent) and no
//     listing row — exactly schema 1's rule;
//   - opted in: the same reader gets arc-found and a `declared-visible` listing row;
//   - opted in but declaring only its home: a reader of neither sees nothing.
//
// The POSITIVE CONTROL is the wide reader (it reads the home), who sees the arc in every arm.
func TestTheDeclaredScopeOptInIsAVisibilityMatrix(t *testing.T) {
	for _, tc := range []struct {
		name       string
		body       string
		narrowSees bool
	}{
		{"not opted in", arcV2(false), false},
		{"opted in", arcV2(true), true},
		{"opted in, declares only its home", `{"schema":2,"visible_to_declared":true,` + summaryOf + `}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			withJournal(t, h)
			if put := h.do(t, "PUT", "/api/v1/arc/alpha-notes/gadget-move", wideToken, nil, tc.body); put.status != 200 {
				t.Fatalf("register: %d %q", put.status, put.body)
			}
			wide := h.do(t, "GET", "/api/v1/arc/alpha-notes/gadget-move", wideToken, nil, "")
			if wide.headers.Get("X-Store-Status") != report.StatusArcFound {
				t.Fatalf("POSITIVE CONTROL: the home reader must see the arc: %s\n%s", wide.headers.Get("X-Store-Status"), wide.body)
			}
			shown := h.do(t, "GET", "/api/v1/arc/alpha-notes/gadget-move", narrowToken, nil, "")
			absent := h.do(t, "GET", "/api/v1/arc/ghost-void/gadget-move", narrowToken, nil, "")
			listed := h.do(t, "GET", "/api/v1/arcs/beta-notes", narrowToken, nil, "")
			row := "\n- alpha-notes/gadget-move · declared · status open · closing check · 0 members · declared-visible · aliases gadget-move"
			if tc.narrowSees {
				if shown.headers.Get("X-Store-Status") != report.StatusArcFound ||
					!strings.Contains(shown.body, "\n  "+report.ArcOptInLine+"\n") {
					t.Fatalf("an opted-in arc must be shown to a declared-scope reader:\n%s", shown.body)
				}
				if !strings.Contains(listed.body, row) || !strings.Contains(listed.body, "\n  "+report.ArcsOptInLine+"\n") {
					t.Fatalf("an opted-in arc must be listed under its declared scope:\n%s", listed.body)
				}
				return
			}
			if shown.headers.Get("X-Store-Status") != report.StatusArcUnregistered || shown.body != absent.body {
				t.Fatalf("an arc this reader may not see must answer exactly like absence:\n%s\n--- absent\n%s", shown.body, absent.body)
			}
			if strings.Contains(listed.body, "gadget-move") || strings.Contains(listed.body, "alpha-notes") {
				t.Fatalf("an arc this reader may not see leaked into its listing:\n%s", listed.body)
			}
		})
	}
}

// TestTheListingAndTheShowAgreeAboutEveryArc is the RELATIONSHIP, over one world holding all four
// shapes (homed where the caller reads or not × opted in or not): for every caller, an arc is LISTED
// under the scope it declares iff `arc-show` FINDS it. A listing that kept the home-only rule while
// the show learned the opt-in — or the reverse — fails one row. The positive control is that both
// sides are non-empty for each caller, so "nobody sees anything" cannot pass.
func TestTheListingAndTheShowAgreeAboutEveryArc(t *testing.T) {
	h := newHarness(t)
	withJournal(t, h)
	arcsByKey := map[string]string{
		"alpha-notes/opted-in":   arcV2(true),
		"alpha-notes/not-opted":  arcV2(false),
		"beta-notes/opted-home":  `{"schema":2,"declared_scopes":["alpha-notes"],"visible_to_declared":true}`,
		"beta-notes/plain-home":  `{"schema":1,"declared_scopes":["alpha-notes"]}`,
		"alpha-notes/only-alpha": `{"schema":2,"visible_to_declared":true}`,
	}
	for key, body := range arcsByKey {
		if put := h.do(t, "PUT", "/api/v1/arc/"+key, wideToken, nil, body); put.status != 200 {
			t.Fatalf("register %s: %d %q", key, put.status, put.body)
		}
	}
	for _, token := range []string{wideToken, narrowToken} {
		shownCount, listedCount := 0, 0
		listings := map[string]string{}
		for _, scope := range []string{"alpha-notes", "beta-notes"} {
			listings[scope] = h.do(t, "GET", "/api/v1/arcs/"+scope, token, nil, "").body
		}
		for key := range arcsByKey {
			found := h.do(t, "GET", "/api/v1/arc/"+key, token, nil, "").headers.Get("X-Store-Status") == report.StatusArcFound
			listed := false
			for _, body := range listings {
				if strings.Contains(body, "\n- "+key+" · ") {
					listed = true
				}
			}
			if found != listed {
				t.Fatalf("token %.6s…: arc %s found=%v but listed=%v", token, key, found, listed)
			}
			if found {
				shownCount++
			}
			if listed {
				listedCount++
			}
		}
		if shownCount == 0 || listedCount == 0 {
			t.Fatalf("POSITIVE CONTROL: token %.6s… saw nothing (shown %d, listed %d)", token, shownCount, listedCount)
		}
	}
	// And the narrow reader's exact set: everything homed in beta, plus alpha's OPTED-IN arc that
	// declares beta — never the alpha arc that did not opt in, nor the one declaring only alpha.
	for key, want := range map[string]bool{"alpha-notes/opted-in": true, "alpha-notes/not-opted": false,
		"beta-notes/opted-home": true, "beta-notes/plain-home": true, "alpha-notes/only-alpha": false} {
		got := h.do(t, "GET", "/api/v1/arc/"+key, narrowToken, nil, "").headers.Get("X-Store-Status") == report.StatusArcFound
		if got != want {
			t.Fatalf("narrow reader, %s: found=%v, want %v", key, got, want)
		}
	}
}

// TestAnOptInCannotReachAScopeTheRegistrantCannotWrite is an INVARIANT GUARD, labelled as one: the
// write-on-every-declared-scope rule predates schema 2, and the opt-in is what makes it load-bearing
// for READS too. The narrow writer (beta only) tries to put an opted-in arc in front of alpha's
// readers: the uniform write 404, nothing written. Control: declaring only beta registers.
func TestAnOptInCannotReachAScopeTheRegistrantCannotWrite(t *testing.T) {
	h := newHarness(t)
	withJournal(t, h)
	refused := h.do(t, "PUT", "/api/v1/arc/beta-notes/sneak", narrowToken, nil,
		`{"schema":2,"declared_scopes":["alpha-notes"],"visible_to_declared":true}`)
	if refused.status != 404 || refused.body != "not found\n" {
		t.Fatalf("an opt-in into a scope the registrant cannot write must be the write 404: %d %q", refused.status, refused.body)
	}
	if got := h.do(t, "GET", "/api/v1/arcs/alpha-notes", wideToken, nil, ""); strings.Contains(got.body, "sneak") {
		t.Fatalf("the refused arc reached alpha's listing:\n%s", got.body)
	}
	if ok := h.do(t, "PUT", "/api/v1/arc/beta-notes/sneak", narrowToken, nil,
		`{"schema":2,"visible_to_declared":true}`); ok.status != 200 {
		t.Fatalf("control: an opt-in within the registrant's own scopes registers: %d %q", ok.status, ok.body)
	}
}

// TestTheSummaryIsServedAsTheToolsDatedWord pins the rendered summary block as WHOLE lines, after
// the member list: provenance first, then the claims, the ranked steps numbered in the order sent,
// aliases folded/deduplicated/sorted.
func TestTheSummaryIsServedAsTheToolsDatedWord(t *testing.T) {
	h := newHarness(t)
	withJournal(t, h)
	if put := h.do(t, "PUT", "/api/v1/arc/alpha-notes/gadget-move", wideToken, nil, arcV2(false)); put.status != 200 {
		t.Fatalf("register: %d %q", put.status, put.body)
	}
	got := h.do(t, "GET", "/api/v1/arc/alpha-notes/gadget-move", wideToken, nil, "").body
	want := "\nmembers: 0, ordered by session id (byte-wise)\n\n" +
		"summary: the registering tool's own word, read from docs/handoff-gadget.md at commit 0123456789abcdef0123456789abcdef01234567 (committed 2000-01-04T00:00:00Z)\n" +
		"closing condition (stated): the gadget answers on the new port\n" +
		"state now: 2\n  the port moved\n  the old listener is still up\n" +
		"next steps (ranked): 2\n  drain the old listener\n  close the arc\n" +
		"task refs: tracker:T-1\naliases: gadget-move"
	// The served body ends in one newline the renderer does not write (`serveReport`).
	if !strings.HasSuffix(got, want+"\n") {
		t.Fatalf("summary block:\n%s", got)
	}
}

// TestAPushWithoutTheOptInMakesTheArcHomeOnlyAgain: the opt-in is CLEARABLE. An opted-in arc is
// shown to the beta-only reader; one schema-1 re-push (a host whose tooling cannot send schema 2)
// clears the opt-in and the summary, and that reader then gets the arc-unregistered answer, byte
// for byte what a never-registered arc answers, and no listing row. The home reader still sees the
// arc, with its new status and no summary.
func TestAPushWithoutTheOptInMakesTheArcHomeOnlyAgain(t *testing.T) {
	h := newHarness(t)
	withJournal(t, h)
	if put := h.do(t, "PUT", "/api/v1/arc/alpha-notes/gadget-move", wideToken, nil, arcV2(true)); put.status != 200 {
		t.Fatalf("register: %d %q", put.status, put.body)
	}
	if got := h.do(t, "GET", "/api/v1/arc/alpha-notes/gadget-move", narrowToken, nil, ""); got.headers.Get("X-Store-Status") != report.StatusArcFound {
		t.Fatalf("POSITIVE CONTROL: the opted-in arc is shown to the declared-scope reader:\n%s", got.body)
	}
	if put := h.do(t, "PUT", "/api/v1/arc/alpha-notes/gadget-move", wideToken, nil,
		`{"schema":1,"status":"closed","declared_scopes":["beta-notes"]}`); put.status != 200 {
		t.Fatalf("schema-1 re-push: %d %q", put.status, put.body)
	}
	shown := h.do(t, "GET", "/api/v1/arc/alpha-notes/gadget-move", narrowToken, nil, "")
	absent := h.do(t, "GET", "/api/v1/arc/ghost-void/gadget-move", narrowToken, nil, "")
	if shown.headers.Get("X-Store-Status") != report.StatusArcUnregistered || shown.body != absent.body {
		t.Fatalf("after a push without the opt-in the declared-scope reader must get the absent answer:\n%s", shown.body)
	}
	if listed := h.do(t, "GET", "/api/v1/arcs/beta-notes", narrowToken, nil, ""); strings.Contains(listed.body, "gadget-move") {
		t.Fatalf("the arc is still listed to the declared-scope reader:\n%s", listed.body)
	}
	home := h.do(t, "GET", "/api/v1/arc/alpha-notes/gadget-move", wideToken, nil, "").body
	if !strings.Contains(home, "\nstatus: closed\n") || strings.Contains(home, "\nsummary:") || strings.Contains(home, "OPTED IN") {
		t.Fatalf("the home reader sees the new state with no summary and no opt-in:\n%s", home)
	}
}

// TestAProblemSummaryNeverRefusesTheArcAndTheResponseNamesEveryAdjustment is the WIRE contract a
// registering tool parses: one `summary-adjusted: <action> <field> <detail>` line per change, after
// the outcome line, in the order made — pinned as the WHOLE body. Every secret shape below is one
// the registering side keeps verbatim and this pod used to refuse the whole arc over; each is built
// at run time so no literal token sits in this file. The arc lands with status and members, and
// arc-show serves the stored items. The NEGATIVE half: a payload that is not the schema still
// refuses, and writes nothing.
func TestAProblemSummaryNeverRefusesTheArcAndTheResponseNamesEveryAdjustment(t *testing.T) {
	h := newHarness(t)
	withJournal(t, h)
	long := strings.Repeat("x", 1497) + "日本"
	sk := "sk" + "-" + strings.Repeat("Ab3d", 6)
	stripe := "sk" + "_test_" + strings.Repeat("Ab3d", 5)
	jwt := "ey" + "J" + strings.Repeat("hbGciOiJ", 2) + ".ey" + "J" + strings.Repeat("zdWIiOiI", 2) + "." + strings.Repeat("c2lnbmF0", 2)
	items, _ := json.Marshal([]string{long, "Authorization: Bearer " + "abc12345", "x-api-key: " + "test1234",
		"use " + sk, "use " + stripe, jwt, "postgresql://app:${PGPASSWORD}@db:5432/app", "two\nlines", "   "})
	body := `{"schema":2,"status":"open","members":[{"session":"s-0042","role":"originated"}],"summary":{"state_now":[],` +
		`"next_steps":` + string(items) + `,"task_refs":[],"aliases":[],` +
		`"source":{"repo_path":"a.md","commit":"0123456789abcdef0123456789abcdef01234567","committed_at":"2000-01-04T00:00:00Z"}}}`
	got := h.do(t, "PUT", "/api/v1/arc/alpha-notes/gadget-move", wideToken, nil, body)
	want := "arc-registered: alpha-notes/gadget-move · members=1 · unjoinable=0\n" +
		"summary-adjusted: clipped summary.next_steps[0] bytes=1503 max=1500\n" +
		"summary-adjusted: redacted summary.next_steps[1] rule=authorization\n" +
		"summary-adjusted: redacted summary.next_steps[2] rule=authorization\n" +
		"summary-adjusted: redacted summary.next_steps[3] rule=sk-key\n" +
		"summary-adjusted: redacted summary.next_steps[4] rule=stripe-key\n" +
		"summary-adjusted: redacted summary.next_steps[5] rule=jwt\n" +
		"summary-adjusted: redacted summary.next_steps[6] rule=url-userinfo-password\n" +
		"summary-adjusted: sanitized summary.next_steps[7] replaced=1\n" +
		"summary-adjusted: dropped summary.next_steps[8] reason=blank\n"
	if got.status != 200 || got.headers.Get("X-Store-Status") != "arc-registered" || got.body != want {
		t.Fatalf("the response:\n%d %s\n%s\nwant:\n%s", got.status, got.headers.Get("X-Store-Status"), got.body, want)
	}
	shown := h.do(t, "GET", "/api/v1/arc/alpha-notes/gadget-move", wideToken, nil, "").body
	for _, line := range []string{"\nstatus: open\n", "\n- s-0042 · originated", "\nnext steps (ranked): 8\n",
		"\n  " + strings.Repeat("x", 1497) + "…\n", "\n  [redacted: authorization]\n", "\n  [redacted: jwt]\n",
		"\n  [redacted: url-userinfo-password]\n", "\n  two lines\n"} {
		if !strings.Contains(shown, line) {
			t.Fatalf("arc-show lacks %q:\n%s", line, shown)
		}
	}
	for _, secret := range []string{"abc12345", "test1234", sk, stripe, jwt} {
		if strings.Contains(shown, secret) {
			t.Fatalf("a redacted value was stored: %q\n%s", secret, shown)
		}
	}
	bad := h.do(t, "PUT", "/api/v1/arc/alpha-notes/other-move", wideToken, nil, `{"schema":2,"summary":{"notes":"x"}}`)
	if bad.status != 400 || !strings.Contains(bad.body, "unknown field") {
		t.Fatalf("a payload that is not the schema still refuses: %d %q", bad.status, bad.body)
	}
	if again := h.do(t, "GET", "/api/v1/arcs/alpha-notes", wideToken, nil, ""); strings.Contains(again.body, "other-move") {
		t.Fatalf("a refused registration was recorded:\n%s", again.body)
	}
}

// TestEachFormerSummaryRefusalNowRegistersTheArc: one push per shape that used to refuse the whole
// registration, each on its own so a regression names the shape. Every arm must answer 200 and
// `arc-registered`, carry exactly the named adjustment line, and serve the stored item from
// arc-show. Built from strings only, so the file compiles against a build that predates the change.
func TestEachFormerSummaryRefusalNowRegistersTheArc(t *testing.T) {
	src := `"source":{"repo_path":"a.md","commit":"0123456789abcdef0123456789abcdef01234567","committed_at":"2000-01-04T00:00:00Z"}`
	list := func(items ...string) string { b, _ := json.Marshal(items); return string(b) }
	big := strings.Repeat("y", 1500)
	many := make([]string, 50)
	for i := range many {
		many[i] = big
	}
	for _, tc := range []struct {
		name, summary, line, served string
	}{
		{"over-long item", `"state_now":` + list(strings.Repeat("z", 1612)) + `,"next_steps":[]`,
			"summary-adjusted: clipped summary.state_now[0] bytes=1612 max=1500", "\n  " + strings.Repeat("z", 1497) + "…\n"},
		// A placeholder in a credential position is REDACTED by design: the raw text is scanned and
		// nothing explains a hit away — and it registers, which is the point of this test.
		{"placeholder in a password position", `"state_now":` + list("postgresql://app:${PGPASSWORD}@db:5432/app") + `,"next_steps":[]`,
			"summary-adjusted: redacted summary.state_now[0] rule=url-userinfo-password", "\n  [redacted: url-userinfo-password]\n"},
		{"placeholder token", `"state_now":` + list("Authorization: Bearer <token>") + `,"next_steps":[]`,
			"", "\n  Authorization: Bearer <token>\n"},
		{"secret-shaped item", `"state_now":` + list("Authorization: Bearer "+"abc12345") + `,"next_steps":[]`,
			"summary-adjusted: redacted summary.state_now[0] rule=authorization", "\n  [redacted: authorization]\n"},
		{"control character", `"state_now":` + list("a\tb") + `,"next_steps":[]`,
			"summary-adjusted: sanitized summary.state_now[0] replaced=1", "\n  a b\n"},
		{"blank item", `"state_now":` + list(" ", "kept") + `,"next_steps":[]`,
			"summary-adjusted: dropped summary.state_now[0] reason=blank", "\n  kept\n"},
		{"over the total", `"state_now":` + list(many...) + `,"next_steps":` + list(many...),
			"summary-adjusted: dropped summary.state_now[49] reason=over-total max=24576", "\nnext steps (ranked): "},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			withJournal(t, h)
			body := `{"schema":2,"status":"open","members":[{"session":"s-0042","role":"originated"}],"summary":{` +
				tc.summary + `,"task_refs":[],"aliases":[],` + src + `}}`
			got := h.do(t, "PUT", "/api/v1/arc/alpha-notes/gadget-move", wideToken, nil, body)
			outcome := "arc-registered: alpha-notes/gadget-move · members=1 · unjoinable=0\n"
			if got.status != 200 || !strings.HasPrefix(got.body, outcome) {
				t.Fatalf("the arc must register: %d %q", got.status, got.body)
			}
			lines := strings.TrimPrefix(got.body, outcome)
			if tc.line == "" && lines != "" || tc.line != "" && !strings.HasPrefix(lines, tc.line+"\n") {
				t.Fatalf("adjustment lines %q, want first %q", lines, tc.line)
			}
			shown := h.do(t, "GET", "/api/v1/arc/alpha-notes/gadget-move", wideToken, nil, "").body
			if !strings.Contains(shown, "\nstatus: open\n") || !strings.Contains(shown, tc.served) {
				t.Fatalf("arc-show must serve the stored item %q:\n%s", tc.served, shown)
			}
		})
	}
}

// TestASummaryStoredAsSentAddsNoLineToTheResponse: no adjustment, no line — so a clean schema-2
// push answers exactly the one outcome line a schema-1 push does.
func TestASummaryStoredAsSentAddsNoLineToTheResponse(t *testing.T) {
	h := newHarness(t)
	withJournal(t, h)
	got := h.do(t, "PUT", "/api/v1/arc/alpha-notes/gadget-move", wideToken, nil, arcV2(false))
	if got.status != 200 || got.body != "arc-registered: alpha-notes/gadget-move · members=0 · unjoinable=0\n" {
		t.Fatalf("a clean summary: %d %q", got.status, got.body)
	}
}

// TestWhatAnOptInExposesToADeclaredScopeReader is the MEASURED list behind the README's opt-in
// warning: everything the beta-only reader is served about an alpha-homed arc once it opts in. The
// arc carries a reader member kept by merge rule 7 (`carried`), so that line is exercised too. The
// NEGATIVE half is what stays hidden: alpha's revision header, and alpha as a "wrote in" scope.
func TestWhatAnOptInExposesToADeclaredScopeReader(t *testing.T) {
	h := newHarness(t)
	withJournal(t, h)
	first := `{"schema":1,"status":"open","declared_scopes":["beta-notes"],"writers_measured":true,"readers_measured":true,` +
		`"commits_total":4,"commits_unstamped":1,"reported_at":"2000-01-02T03:04:05Z",` +
		`"members":[{"session":"s-0042","role":"originated","first_seen":"2000-01-01T00:00:00Z"},{"session":"s-0077","role":"resumed","first_seen":""}]}`
	second := `{"schema":2,"status":"open","declared_scopes":["beta-notes"],"writers_measured":true,"readers_measured":false,` +
		`"commits_total":4,"commits_unstamped":1,"reported_at":"2000-01-02T03:04:05Z","visible_to_declared":true,` +
		`"members":[{"session":"s-0042","role":"originated","first_seen":"2000-01-01T00:00:00Z"}],` + summaryOf + `}`
	for _, body := range []string{first, second} {
		if put := h.do(t, "PUT", "/api/v1/arc/alpha-notes/gadget-move", wideToken, nil, body); put.status != 200 {
			t.Fatalf("register: %d %q", put.status, put.body)
		}
	}
	got := h.do(t, "GET", "/api/v1/arc/alpha-notes/gadget-move", narrowToken, nil, "")
	if got.headers.Get("X-Store-Status") != "arc-found" {
		t.Fatalf("the declared-scope reader must see the opted-in arc:\n%s", got.body)
	}
	for name, want := range map[string]string{
		"home and slug":          "cairn-arc: status=arc-found home=alpha-notes slug=gadget-move",
		"status":                 "\nstatus: open\n",
		"registered_by and time": "\nregistered: 2000-01-05T00:00:00Z by wide-reader · reported by the tool at 2000-01-02T03:04:05Z\n",
		"tooling coverage":       "\ntooling coverage: 1 of 4 commits carry no session id · writers: measured · readers: NOT measured\n",
		"carried count":          "1 member(s) carried from an earlier registration",
		"member id, role, first": "\n- s-0042 · originated · first seen 2000-01-01T00:00:00Z · wrote in: none readable to you",
		"carried member":         "\n- s-0077 · resumed · first seen unknown · wrote in: none readable to you · carried",
		"declared, narrowed":     "\ndeclared scopes readable to you: beta-notes (",
		"summary":                "\nsummary: the registering tool's own word, read from docs/handoff-gadget.md",
	} {
		if !strings.Contains(got.body, want) {
			t.Fatalf("%s is not served to the declared-scope reader (the README lists it):\n%s", name, got.body)
		}
	}
	absent := h.do(t, "GET", "/api/v1/arc/ghost-void/gadget-move", narrowToken, nil, "")
	home := h.do(t, "GET", "/api/v1/arc/alpha-notes/gadget-move", wideToken, nil, "")
	if got.headers.Get("X-Store-Revision") != absent.headers.Get("X-Store-Revision") ||
		home.headers.Get("X-Store-Revision") == got.headers.Get("X-Store-Revision") {
		t.Fatalf("the home's revision must stay hidden: declared %q, absent %q, home reader %q",
			got.headers.Get("X-Store-Revision"), absent.headers.Get("X-Store-Revision"), home.headers.Get("X-Store-Revision"))
	}
	if strings.Contains(got.body, "declared scopes readable to you: alpha-notes") {
		t.Fatalf("an unreadable scope was named:\n%s", got.body)
	}
}
