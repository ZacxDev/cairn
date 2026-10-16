package api

import (
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
		`"visible_to_declared_scopes":` + flag + `,` + summaryOf + `}`
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
		{"opted in, declares only its home", `{"schema":2,"visible_to_declared_scopes":true,` + summaryOf + `}`, false},
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
		"beta-notes/opted-home":  `{"schema":2,"declared_scopes":["alpha-notes"],"visible_to_declared_scopes":true}`,
		"beta-notes/plain-home":  `{"schema":1,"declared_scopes":["alpha-notes"]}`,
		"alpha-notes/only-alpha": `{"schema":2,"visible_to_declared_scopes":true}`,
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
		`{"schema":2,"declared_scopes":["alpha-notes"],"visible_to_declared_scopes":true}`)
	if refused.status != 404 || refused.body != "not found\n" {
		t.Fatalf("an opt-in into a scope the registrant cannot write must be the write 404: %d %q", refused.status, refused.body)
	}
	if got := h.do(t, "GET", "/api/v1/arcs/alpha-notes", wideToken, nil, ""); strings.Contains(got.body, "sneak") {
		t.Fatalf("the refused arc reached alpha's listing:\n%s", got.body)
	}
	if ok := h.do(t, "PUT", "/api/v1/arc/beta-notes/sneak", narrowToken, nil,
		`{"schema":2,"visible_to_declared_scopes":true}`); ok.status != 200 {
		t.Fatalf("control: an opt-in within the registrant's own scopes registers: %d %q", ok.status, ok.body)
	}
}

// TestTheSummaryIsServedAsTheToolsDatedWord pins the rendered summary block as WHOLE lines, after
// the member list: provenance first, then the claims, the ranked steps numbered in the order sent,
// aliases folded/deduplicated/sorted. A schema-1 re-push then CARRIES it and says so.
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
		"state now: 2\n- the port moved\n- the old listener is still up\n" +
		"next steps (ranked): 2\n1. drain the old listener\n2. close the arc\n" +
		"task refs: tracker:T-1\naliases: gadget-move"
	// The served body ends in one newline the renderer does not write (`serveReport`).
	if !strings.HasSuffix(got, want+"\n") {
		t.Fatalf("summary block:\n%s", got)
	}
	if put := h.do(t, "PUT", "/api/v1/arc/alpha-notes/gadget-move", wideToken, nil,
		`{"schema":1,"status":"closed","declared_scopes":["beta-notes"]}`); put.status != 200 {
		t.Fatalf("schema-1 re-push: %d %q", put.status, put.body)
	}
	again := h.do(t, "GET", "/api/v1/arc/alpha-notes/gadget-move", wideToken, nil, "").body
	if !strings.Contains(again, "\nstatus: closed\n") || !strings.Contains(again, "\n  "+report.ArcSummaryCarriedLine+"\n") ||
		!strings.Contains(again, "\nnext steps (ranked): 2\n") {
		t.Fatalf("a schema-1 push must update the status and CARRY the summary, saying so:\n%s", again)
	}
}

// TestASchemaTwoPayloadIsRefusedOnTheWireLikeAnyBadBody: the summary's refusals reach the caller
// as the 400 every malformed registration gets, naming the field, and write nothing.
func TestASchemaTwoPayloadIsRefusedOnTheWireLikeAnyBadBody(t *testing.T) {
	h := newHarness(t)
	journal := withJournal(t, h)
	got := h.do(t, "PUT", "/api/v1/arc/alpha-notes/gadget-move", wideToken, nil,
		`{"schema":2,"summary":{"state_now":["two\nlines"],"next_steps":[],"task_refs":[],"aliases":[],`+
			`"source":{"repo_path":"a.md","commit":"0123456789abcdef0123456789abcdef01234567","committed_at":"2000-01-04T00:00:00Z"}}}`)
	if got.status != 400 || got.headers.Get("X-Store-Status") != "bad-request" ||
		got.body != "bad request: summary.state_now[0] must be one line with no control characters\n" {
		t.Fatalf("a control character in the summary: %d %q", got.status, got.body)
	}
	if again := h.do(t, "GET", "/api/v1/arcs/alpha-notes", wideToken, nil, ""); strings.Contains(again.body, "gadget-move") {
		t.Fatalf("a refused registration was recorded (%s):\n%s", journal, again.body)
	}
}
