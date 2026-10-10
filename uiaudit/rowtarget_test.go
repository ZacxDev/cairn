package main

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// rowHitJS hit-tests the scope page's entry rows the way a finger does: it scrolls each target to
// the middle of the viewport and asks `document.elementFromPoint` what is under its centre.
//
//   - a point on the row's TITLE must resolve to the row's own ref link (B1: the row is the target);
//   - a point on a CHIP link must resolve to THAT chip link (the overlay must not swallow it);
//   - `titleNoLink` counts title points that resolve to no link at all (the desktop answer).
const rowHitJS = `(() => {
  const hit = (el) => {
    el.scrollIntoView({block: 'center'});
    const r = el.getBoundingClientRect();
    const h = document.elementFromPoint(r.left + r.width / 2, r.top + r.height / 2);
    return h ? h.closest('a') : null;
  };
  const out = {rows: 0, titleHitsRef: 0, titleNoLink: 0, chips: 0, chipHitsSelf: 0};
  for (const row of document.querySelectorAll('#entry-list > .entry-row')) {
    const ref = row.querySelector('.ref a');
    const title = row.querySelector('.title');
    if (!ref || !title || title.getBoundingClientRect().width === 0) continue;
    out.rows++;
    const a = hit(title);
    if (a === ref) out.titleHitsRef++;
    if (!a) out.titleNoLink++;
    for (const chip of row.querySelectorAll('.chips a')) {
      out.chips++;
      if (hit(chip) === chip) out.chipHitsSelf++;
    }
  }
  return JSON.stringify(out);
})()`

// excerptHitJS asks the same question of the SESSION page's bullet excerpts, which are `.entry-row`s
// too but not the scope page's: a point on an excerpt must resolve to NO link.
const excerptHitJS = `(() => {
  const out = {excerpts: 0, onALink: 0};
  for (const el of document.querySelectorAll('.entry-row .excerpt')) {
    if (el.getBoundingClientRect().width === 0) continue;
    el.scrollIntoView({block: 'center'});
    const r = el.getBoundingClientRect();
    const h = document.elementFromPoint(r.left + r.width / 2, r.top + r.height / 2);
    out.excerpts++;
    if (h && h.closest('a')) out.onALink++;
  }
  return JSON.stringify(out);
})()`

type rowHits struct {
	Rows         int `json:"rows"`
	TitleHitsRef int `json:"titleHitsRef"`
	TitleNoLink  int `json:"titleNoLink"`
	Chips        int `json:"chips"`
	ChipHitsSelf int `json:"chipHitsSelf"`
}

// TestTheWholeRowTargetIsTheScopePagesAlone is B1's behavioural guard, by hit-testing in a real
// chromium rather than by reading the stylesheet.
//
// 🔴 IT EXISTS BECAUSE THE WALK CANNOT SEE EITHER FAILURE. Deleting the rule that lifts the chips
// above the row's overlay leaves every box the walk measures unchanged (walk rc=0), while a real tap
// on a chip lands on the entry instead; and S1's first draft put the overlay on EVERY `.entry-row`
// — session-page bullets, arc rows, an arc's scope rows — where it swallowed taps on excerpts and
// badges, again with every walk number green. Measured: this test is RED on that draft (the session
// excerpts resolved to a link) and RED with the chip rule deleted.
//
// Three readings, each with a non-zero denominator so none of them can pass over nothing:
//   - at `mobile` (touch), every scope-page row's title resolves to its ref link, and every chip link
//     resolves to itself;
//   - at `laptop` (no touch), every title resolves to NO link — the overlay is a coarse-pointer rule;
//   - at `mobile`, a session page's bullet excerpts resolve to no link.
func TestTheWholeRowTargetIsTheScopePagesAlone(t *testing.T) {
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

	root, err := b.CaptureTarget(Target{Path: "/scopes", PushURL: "/scopes", LedgerRow: "control", ExpandLinks: true}, Mobile)
	if err != nil {
		t.Fatal(err)
	}
	var scopePages []string
	for _, href := range root.Hrefs {
		if strings.HasPrefix(href, "/scope?id=") && !strings.Contains(href, "&") {
			scopePages = append(scopePages, href)
		}
	}
	if len(scopePages) == 0 {
		t.Fatalf("the scope list (/scopes) linked no scope page, so there is no row to hit-test (hrefs: %v)", root.Hrefs)
	}

	sum := map[string]*rowHits{}
	for _, vp := range []Viewport{Mobile, Laptop} {
		s := &rowHits{}
		sum[vp.Name] = s
		for _, href := range scopePages {
			if _, err := b.CaptureTarget(Target{Path: href, PushURL: href, LedgerRow: "control"}, vp); err != nil {
				t.Fatal(err)
			}
			var raw string
			if err := chromedp.Run(b.ctx, chromedp.Evaluate(rowHitJS, &raw)); err != nil {
				t.Fatal(err)
			}
			var h rowHits
			if err := json.Unmarshal([]byte(raw), &h); err != nil {
				t.Fatalf("%s: %q: %v", href, raw, err)
			}
			s.Rows += h.Rows
			s.TitleHitsRef += h.TitleHitsRef
			s.TitleNoLink += h.TitleNoLink
			s.Chips += h.Chips
			s.ChipHitsSelf += h.ChipHitsSelf
		}
		t.Logf("%-7s over %d scope page(s): %+v", vp.Name, len(scopePages), *s)
	}
	m, l := sum[Mobile.Name], sum[Laptop.Name]
	if m.Rows == 0 || m.Chips == 0 {
		t.Fatalf("the hit-test reached %d row(s) and %d chip link(s) at mobile; both must be non-zero or the "+
			"readings below are about nothing", m.Rows, m.Chips)
	}
	if m.TitleHitsRef != m.Rows {
		t.Errorf("at mobile %d of %d row title(s) resolved to the row's ref link: the WHOLE ROW is not the target", m.TitleHitsRef, m.Rows)
	}
	if m.ChipHitsSelf != m.Chips {
		t.Errorf("at mobile %d of %d chip link(s) resolved to themselves: the row overlay swallows a tap meant "+
			"for a chip", m.ChipHitsSelf, m.Chips)
	}
	if l.TitleNoLink != l.Rows || l.Rows == 0 {
		t.Errorf("at laptop %d of %d row title(s) resolved to NO link: the row overlay leaked onto a fine pointer",
			l.TitleNoLink, l.Rows)
	}

	members, err := fixtureMembers(world.Store)
	if err != nil {
		t.Fatal(err)
	}
	sessionPage := "/session?session=" + members[0].Session
	if _, err := b.CaptureTarget(Target{Path: sessionPage, PushURL: "/session", LedgerRow: "control"}, Mobile); err != nil {
		t.Fatal(err)
	}
	var raw string
	if err := chromedp.Run(b.ctx, chromedp.Evaluate(excerptHitJS, &raw)); err != nil {
		t.Fatal(err)
	}
	var ex struct{ Excerpts, OnALink int }
	if err := json.Unmarshal([]byte(raw), &ex); err != nil {
		t.Fatalf("%q: %v", raw, err)
	}
	t.Logf("mobile  %s: %d excerpt(s), %d resolving to a link", sessionPage, ex.Excerpts, ex.OnALink)
	if ex.Excerpts == 0 {
		t.Fatalf("the session page rendered no bullet excerpt, so the not-a-whole-row reading is about nothing")
	}
	if ex.OnALink != 0 {
		t.Errorf("at mobile %d of %d session-page excerpt(s) resolved to a LINK: the row overlay reached a row that is "+
			"not a scope-page entry row, where it swallows taps and blocks text selection", ex.OnALink, ex.Excerpts)
	}
}
