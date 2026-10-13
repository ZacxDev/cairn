package main

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// transcriptProbeJS reads the transcript section as a browser built it: how many reveals it holds,
// how many are OPEN before anything is touched, whether the user message's unbreakable token is
// wholly in the page, and what one click on a tool call's summary does — with no script on the
// page, that click is the browser's own `<details>` behaviour.
const transcriptProbeJS = `(() => {
	const section = document.getElementById('transcript');
	if (!section) return JSON.stringify({section: false});
	const details = section.querySelectorAll('details');
	const open = section.querySelectorAll('details[open]').length;
	const tool = section.querySelector('details[data-class="tool-call"]');
	let opened = false;
	if (tool) { tool.querySelector('summary').click(); opened = tool.open; tool.open = false; }
	const user = section.querySelector('[data-class="human-text"] .t-text');
	return JSON.stringify({
		section: true, details: details.length, open: open, toolOpened: opened,
		turns: section.querySelectorAll('.turn').length,
		nested: section.querySelectorAll('details details[data-stream]').length,
		more: section.querySelectorAll('details.t-more').length,
		userWhole: !!user && user.textContent.includes('` + "synthetic" + `'.repeat(12)),
		scripts: document.scripts.length,
		scrollWidth: document.documentElement.scrollWidth, innerWidth: window.innerWidth,
	});
})()`

// TestTheSessionPageCarriesItsTranscriptAtEveryWidth is the walk's transcript reading as a GUARD
// rather than a row in an advisory log (the S4 test plan: the session page with a transcript at all
// five widths, overflow 0).
//
// 🔴 IT EXISTS BECAUSE THE WALK CANNOT TELL A TRANSCRIPT FROM ITS ABSENCE. A world whose upload
// silently stopped storing — or a page that stopped rendering the section — captures the
// no-transcript session page at every width with every number green. This reads the section itself:
// it is there, its reveals are closed until asked, a click opens one with no script on the page,
// the subagent is nested inside its call, the user message's unbreakable token is whole — and the
// page does not scroll sideways at any of the five widths (the walk's own overflow reading, which
// `control_test.go` proves can go red).
func TestTheSessionPageCarriesItsTranscriptAtEveryWidth(t *testing.T) {
	chromiumOrRefuse(t)
	bin := os.Getenv("UIAUDIT_CAIRN_UI")
	if bin == "" {
		t.Fatal("UIAUDIT_CAIRN_UI must name a built cairn-ui: this test boots the token-file world")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	port, err := aFreeLoopbackPort()
	if err != nil {
		t.Fatal(err)
	}
	world, err := BootWorld(ctx, os.Getenv("UIAUDIT_REPO_ROOT"), bin, t.TempDir(), port)
	if err != nil {
		t.Fatal(err)
	}
	defer world.Stop()
	b, err := NewBrowser(ctx, world.BaseURL, 3*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	if _, err := b.SignIn(world.Token); err != nil {
		t.Fatal(err)
	}
	members, err := fixtureMembers(world.Store)
	if err != nil {
		t.Fatal(err)
	}
	page := "/session?session=" + members[0].Session
	for _, vp := range Viewports {
		capture, err := b.CaptureTarget(Target{Path: page, PushURL: "/session", LedgerRow: "control"}, vp)
		if err != nil {
			t.Fatal(err)
		}
		var raw string
		if err := chromedp.Run(b.ctx, chromedp.Evaluate(transcriptProbeJS, &raw)); err != nil {
			t.Fatal(err)
		}
		var got struct {
			Section, ToolOpened, UserWhole     bool
			Details, Open, Turns, Nested, More int
			Scripts, ScrollWidth, InnerWidth   int
		}
		if err := json.Unmarshal([]byte(raw), &got); err != nil {
			t.Fatalf("%s: %q: %v", vp.Name, raw, err)
		}
		t.Logf("%-9s %s: %+v overflow=%v", vp.Name, page, got, capture.Layout.HorizontalOverflow)
		if !got.Section {
			t.Fatalf("%s: the session page carries NO transcript section — the world's upload stored nothing, or the "+
				"page stopped rendering it; every other reading here would be about the no-transcript page", vp.Name)
		}
		// The fixture: two turns; a thinking block, three tool calls, two runtime blocks, the
		// bookkeeping line, the raw-records list and the nested subagent are reveals; one long
		// assistant message has a "read more".
		if got.Turns != 2 || got.Details < 9 || got.Nested != 1 || got.More != 1 {
			t.Errorf("%s: the section holds %d turn(s), %d reveal(s), %d nested subagent(s), %d read-more; want 2, at least 9, 1, 1",
				vp.Name, got.Turns, got.Details, got.Nested, got.More)
		}
		if got.Open != 0 {
			t.Errorf("%s: %d reveal(s) are OPEN before anything was clicked; the reading must arrive collapsed", vp.Name, got.Open)
		}
		if !got.ToolOpened {
			t.Errorf("%s: clicking a tool call's summary did not open it", vp.Name)
		}
		if got.Scripts != 0 {
			t.Errorf("%s: the page carries %d script element(s); the reveal must be the browser's own <details>", vp.Name, got.Scripts)
		}
		if !got.UserWhole {
			t.Errorf("%s: the user message's 108-character token is not whole in the page", vp.Name)
		}
		if capture.Layout.HorizontalOverflow || got.ScrollWidth > got.InnerWidth {
			t.Errorf("%s: the session page with its transcript scrolls sideways (document %dpx, viewport %dpx)",
				vp.Name, got.ScrollWidth, got.InnerWidth)
		}
	}
}
