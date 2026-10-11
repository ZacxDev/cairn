package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

// 🔴 S5 OF THE MOBILE PLAN: THE STANDALONE WINDOW'S OWN BACK AND RELOAD (`pwa.js`, `pwa.go`,
// `tailwind.css`). An installed app has no browser chrome, so the app carries the two controls a
// tab gets from the browser — revealed by `pwa.js` ONLY when `(display-mode: standalone)` matches.
//
// ⚠ CDP CANNOT EMULATE STANDALONE DISPLAY MODE (the plan's measurement: `setEmulatedMedia` with
// `display-mode: standalone` had no effect), so this test STUBS `matchMedia` for that one query
// before any page script runs — the plan's S5 test plan, verbatim. The stub is the REACHABILITY
// control: without it the controls must stay hidden, with it they must appear. ⚠ What that cannot
// see: whether a real iOS or Android standalone window reports the query at all. That is the
// iPhone checklist's step 8, a device check and not part of the closing condition.
//
// 🔴 THE STANDALONE LAYOUT IS KEYED ON THE REVEALED CONTROLS (`:has(> .standalone-nav:not([hidden]))`),
// NOT ON `@media (display-mode: standalone)`, AND THAT IS WHAT MAKES IT MEASURABLE HERE. A media
// query this harness cannot drive would leave the header's position, the grid that keeps the header
// at two rows, and the overscroll rule all unmeasured; keyed on the one state `pwa.js` sets, they render
// under the same stub that reveals the controls, so every reading below is of the real stylesheet.

// standaloneStub makes `matchMedia("(display-mode: standalone)")` match and leaves every other query
// to the browser. It is installed with `Page.addScriptToEvaluateOnNewDocument`, so it is in place
// before the deferred `pwa.js` runs — on every document, Back's included.
const standaloneStub = `(function () {
  var real = window.matchMedia.bind(window);
  window.matchMedia = function (q) {
    if (q !== "(display-mode: standalone)") { return real(q); }
    return {matches: true, media: q, onchange: null,
      addListener: function () {}, removeListener: function () {},
      addEventListener: function () {}, removeEventListener: function () {},
      dispatchEvent: function () { return false; }};
  };
})();`

// interceptOwnInstallPrompt swallows the browser's OWN `beforeinstallprompt` — headless chromium was
// measured firing one on an armed page (`pwaClauseE`) — so the Install button cannot be revealed in
// the tab browser and add a header row the comparison below would then read as the standalone
// controls' doing.
const interceptOwnInstallPrompt = `window.addEventListener("beforeinstallprompt", function (e) {
  if (e.isTrusted) { e.stopImmediatePropagation(); e.preventDefault(); }
}, true);`

// standaloneReadJS reads, in one evaluation, everything the readings below compare.
const standaloneReadJS = `JSON.stringify((() => {
  const back = document.getElementById("pwa-back"), reload = document.getElementById("pwa-reload");
  const header = document.querySelector("header.page-header");
  const box = (el) => { const r = el.getBoundingClientRect(); return {w: r.width, h: r.height}; };
  return {
    present: !!back && !!reload,
    backHidden: !back || back.hidden, reloadHidden: !reload || reload.hidden,
    backBox: back ? box(back) : null, reloadBox: reload ? box(reload) : null,
    installHidden: (document.getElementById("pwa-install") || {hidden: true}).hidden,
    position: header ? getComputedStyle(header).position : "",
    overscrollY: getComputedStyle(document.documentElement).overscrollBehaviorY,
    overflowX: document.documentElement.scrollWidth - window.innerWidth,
  };
})())`

type standaloneRead struct {
	Present      bool `json:"present"`
	BackHidden   bool `json:"backHidden"`
	ReloadHidden bool `json:"reloadHidden"`
	BackBox      *struct {
		W float64 `json:"w"`
		H float64 `json:"h"`
	} `json:"backBox"`
	ReloadBox *struct {
		W float64 `json:"w"`
		H float64 `json:"h"`
	} `json:"reloadBox"`
	InstallHidden bool    `json:"installHidden"`
	Position      string  `json:"position"`
	OverscrollY   string  `json:"overscrollY"`
	OverflowX     float64 `json:"overflowX"`
}

// headerRows counts the rows the header's focusables occupy, by the boxes — the same row rule as
// [headerOrderBreaks]: the next element is on a LATER row when its top is at or below the previous
// one's bottom.
func headerRows(boxes []FocusBox) int {
	if len(boxes) == 0 {
		return 0
	}
	rows := 1
	for i := 1; i < len(boxes); i++ {
		if boxes[i].Top >= boxes[i-1].Bottom-1 {
			rows++
		}
	}
	return rows
}

// headerTopAfterScroll scrolls the document down by `by` px and returns the header's top edge in the
// viewport and how far the document actually scrolled.
func headerTopAfterScroll(ctx context.Context, by int) (top, scrolled float64, err error) {
	var out []float64
	err = chromedp.Run(ctx, chromedp.Evaluate(fmt.Sprintf(`(() => {
  window.scrollTo(0, %d);
  return [document.querySelector("header.page-header").getBoundingClientRect().top, window.scrollY];
})()`, by), &out))
	if err == nil && len(out) == 2 {
		top, scrolled = out[0], out[1]
	}
	return
}

// pollUntil polls `expr` (a boolean expression) until it is true or the budget runs out.
func pollUntil(ctx context.Context, expr string, budget time.Duration) bool {
	deadline := time.Now().Add(budget)
	for time.Now().Before(deadline) {
		var ok bool
		if err := chromedp.Run(ctx, chromedp.Evaluate(expr, &ok)); err == nil && ok {
			return true
		}
		time.Sleep(100 * time.Millisecond)
	}
	return false
}

// TestStandaloneBackAndReload is S5's browser test, over ONE armed world and two browsers — a TAB (no
// stub) and a STANDALONE window (the stub):
//
//  1. THE CONTROL: in the tab, both controls exist and stay HIDDEN, at mobile and laptop.
//  2. THE REVEAL: in the standalone window, both appear, at mobile and laptop; the Install button stays
//     hidden (an installed app has nothing to install); each is ≥ 44px tall at the touch rung.
//  3. THE LAYOUT, at mobile and tablet (the touch rungs): revealing the controls adds NO header row (the
//     tab header's row count is the baseline, measured on the same page); the header's reading order
//     still matches its visual order (`headerOrderBreaks`, the walk's own refusal); no horizontal
//     overflow; the header is NOT `sticky` (nor `fixed`) — an operator decision, see `tailwind.css` —
//     and after a 400px scroll it has scrolled away exactly like the TAB header (the tab's is the
//     control that the page moved at all); and the root's `overscroll-behavior-y` is `contain` in
//     standalone and `auto` in the tab.
//  4. THE BEHAVIOUR (checklist step 8, in chromium): scope page → entry page → Back lands on the scope
//     page; Reload loads a FRESH document (a marker set on the old one is gone) at the same URL.
func TestStandaloneBackAndReload(t *testing.T) {
	chromiumOrRefuse(t)
	bin, root := os.Getenv("UIAUDIT_CAIRN_UI"), os.Getenv("UIAUDIT_REPO_ROOT")
	if bin == "" || root == "" {
		t.Fatal("UIAUDIT_CAIRN_UI and UIAUDIT_REPO_ROOT must name a built cairn-ui and its checkout: this test " +
			"boots an ARMED world, and a skip would be a green about nothing")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	port, err := aFreeLoopbackPort()
	if err != nil {
		t.Fatal(err)
	}
	world, err := BootWorld(ctx, root, bin, t.TempDir(), port, "-app-name", "cairn (alpha)", "-app-icon-variant", "amber")
	if err != nil {
		t.Fatal(err)
	}
	defer world.Stop()

	newBrowser := func(scripts ...string) *Browser {
		br, err := NewBrowser(ctx, world.BaseURL, 4*time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(br.Close)
		for _, s := range scripts {
			if err := chromedp.Run(br.ctx, chromedp.ActionFunc(func(ctx context.Context) error {
				_, err := page.AddScriptToEvaluateOnNewDocument(s).Do(ctx)
				return err
			})); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := br.SignIn(world.Token); err != nil {
			t.Fatal(err)
		}
		return br
	}
	tab := newBrowser(interceptOwnInstallPrompt)
	app := newBrowser(interceptOwnInstallPrompt, standaloneStub)

	// A scope page and an entry page of the synthetic world, found by following the surface's own links.
	scopes, err := tab.CaptureTarget(Target{Path: "/scopes", PushURL: "/scopes", LedgerRow: "control", ExpandLinks: true}, Laptop)
	if err != nil {
		t.Fatal(err)
	}
	scopePage := firstHref(scopes.Hrefs, "/scope?id=")
	if scopePage == "" {
		t.Fatalf("/scopes linked no scope page (hrefs: %v)", scopes.Hrefs)
	}
	scope, err := tab.CaptureTarget(Target{Path: scopePage, PushURL: "/scope", LedgerRow: "control", ExpandLinks: true}, Laptop)
	if err != nil {
		t.Fatal(err)
	}
	entryPage := firstHref(scope.Hrefs, "/entry?")
	if entryPage == "" {
		t.Fatalf("%s linked no entry page (hrefs: %v)", scopePage, scope.Hrefs)
	}

	read := func(b *Browser, vp Viewport, path string) (standaloneRead, *Capture) {
		t.Helper()
		c, err := b.CaptureTarget(Target{Path: path, PushURL: path, LedgerRow: "control"}, vp)
		if err != nil {
			t.Fatal(err)
		}
		var raw string
		if err := chromedp.Run(b.ctx, chromedp.Evaluate(standaloneReadJS, &raw)); err != nil {
			t.Fatal(err)
		}
		var r standaloneRead
		if err := json.Unmarshal([]byte(raw), &r); err != nil {
			t.Fatalf("%s at %s: %q: %v", path, vp.Name, raw, err)
		}
		return r, c
	}

	// 1 + 2: hidden in the tab, revealed in the standalone window, at a touch rung and a fine-pointer one.
	for _, vp := range []Viewport{Mobile, Laptop} {
		tr, _ := read(tab, vp, scopePage)
		if !tr.Present {
			t.Fatalf("CONTROL: %s at %s carries no #pwa-back/#pwa-reload at all on an ARMED world, so 'hidden' "+
				"below would be a reading of nothing", scopePage, vp.Name)
		}
		if !tr.BackHidden || !tr.ReloadHidden {
			t.Errorf("in a browser TAB at %s the standalone controls are hidden=%v/%v, want both hidden — a tab has "+
				"the browser's own Back and Reload", vp.Name, tr.BackHidden, tr.ReloadHidden)
		}
		ar, _ := read(app, vp, scopePage)
		if ar.BackHidden || ar.ReloadHidden {
			t.Errorf("in a STANDALONE window at %s the controls are hidden=%v/%v, want both shown (the reachability "+
				"control: the stub makes (display-mode: standalone) match)", vp.Name, ar.BackHidden, ar.ReloadHidden)
		}
		if !ar.InstallHidden {
			t.Errorf("in a STANDALONE window at %s the Install button is shown — an installed app has nothing to install", vp.Name)
		}
		if vp.Touch && (ar.BackBox == nil || ar.ReloadBox == nil || ar.BackBox.H < 44 || ar.ReloadBox.H < 44) {
			t.Errorf("at the touch rung %s the controls' boxes are %+v / %+v, want each ≥ 44px tall", vp.Name, ar.BackBox, ar.ReloadBox)
		}
	}

	// 3: the layout, at both touch rungs.
	for _, vp := range []Viewport{Mobile, Tablet} {
		tr, tc := read(tab, vp, scopePage)
		ar, ac := read(app, vp, scopePage)
		tabRows, appRows := headerRows(tc.HeaderOrder), headerRows(ac.HeaderOrder)
		t.Logf("%-7s tab header: %d focusable(s) on %d row(s); standalone header: %d on %d row(s)",
			vp.Name, len(tc.HeaderOrder), tabRows, len(ac.HeaderOrder), appRows)
		if len(ac.HeaderOrder) != len(tc.HeaderOrder)+2 {
			t.Errorf("at %s the standalone header has %d focusable(s) and the tab header %d — want exactly two more "+
				"(Back, Reload), or the row comparison below is between different headers", vp.Name, len(ac.HeaderOrder),
				len(tc.HeaderOrder))
		}
		if tabRows < 2 || appRows != tabRows {
			t.Errorf("at %s revealing Back and Reload moved the header from %d row(s) to %d — every row it gains "+
				"pushes every page's content down another row", vp.Name, tabRows, appRows)
		}
		for _, b := range headerOrderBreaks(ac) {
			t.Errorf("at %s in standalone the header's reading order breaks: %s", vp.Name, b)
		}
		if ar.OverflowX > 0 {
			t.Errorf("at %s in standalone the page overflows horizontally by %.0fpx", vp.Name, ar.OverflowX)
		}
		// 🔴 NOT STICKY, BY OPERATOR DECISION: a pinned two-row header held ~12% of a phone screen and
		// covered in-page anchor targets. Read both ways — the computed position here, and the header
		// actually leaving the viewport after the scroll below — so re-adding `sticky` (or `fixed`) to
		// the standalone rule turns this test red at both readings.
		if ar.Position == "sticky" || ar.Position == "fixed" || ar.Position != tr.Position {
			t.Errorf("at %s the header's position is %q in standalone and %q in a tab, want the same and neither "+
				"sticky nor fixed — the standalone header scrolls away like a tab's", vp.Name, ar.Position, tr.Position)
		}
		if ar.OverscrollY != "contain" || tr.OverscrollY != "auto" {
			t.Errorf("at %s the root's overscroll-behavior-y is %q in standalone and %q in a tab, want contain and auto",
				vp.Name, ar.OverscrollY, tr.OverscrollY)
		}
		appTop, appScrolled, err := headerTopAfterScroll(app.ctx, 400)
		if err != nil {
			t.Fatal(err)
		}
		tabTop, tabScrolled, err := headerTopAfterScroll(tab.ctx, 400)
		if err != nil {
			t.Fatal(err)
		}
		if appScrolled < 200 || tabScrolled < 200 {
			t.Fatalf("CONTROL: at %s the scope page scrolled only %.0f/%.0fpx, so the scroll-away reading is about a "+
				"page that never moved", vp.Name, appScrolled, tabScrolled)
		}
		if tabTop > -20 {
			t.Errorf("CONTROL: at %s, scrolled %.0fpx, the TAB header's top is at %.0fpx — it did not scroll away, so "+
				"the standalone reading below cannot tell scrolling away from staying put", vp.Name, tabScrolled, tabTop)
		}
		if appTop > -20 {
			t.Errorf("at %s, scrolled %.0fpx, the standalone header's top is at %.0fpx and the tab's at %.0fpx, want it "+
				"scrolled away like the tab's — a header still on screen is pinned, and the standalone header is not "+
				"sticky (operator decision)", vp.Name, appScrolled, appTop, tabTop)
		}
	}

	// 4: the behaviour, in the standalone window — scope → entry → Back → Reload.
	base := strings.TrimRight(world.BaseURL, "/")
	if err := chromedp.Run(app.ctx, chromedp.Navigate(base+scopePage), chromedp.WaitReady("body"),
		chromedp.Navigate(base+entryPage), chromedp.WaitReady("body")); err != nil {
		t.Fatal(err)
	}
	if !pollUntil(app.ctx, `!document.getElementById("pwa-back").hidden`, 5*time.Second) {
		t.Fatalf("on %s the standalone Back control never appeared", entryPage)
	}
	// The clicks are DISPATCHED (`HTMLElement.click()`), not chromedp's selector-based mouse click: that
	// one, on Reload after a Back, hung until the browser's budget ran out (measured once, chromium 154;
	// the cause was NOT isolated — stale document tracking across the history navigation is a guess).
	// The listener is what is under test here; whether a finger can hit the control is part (2)'s box
	// reading and part (3)'s layout reading.
	if err := chromedp.Run(app.ctx, chromedp.Evaluate(`document.getElementById("pwa-back").click(); true`, nil)); err != nil {
		t.Fatal(err)
	}
	want := base + scopePage
	if !pollUntil(app.ctx, fmt.Sprintf(`location.href === %q`, want), 10*time.Second) {
		var at string
		_ = chromedp.Run(app.ctx, chromedp.Location(&at))
		t.Errorf("Back on %s landed on %s, want %s", entryPage, at, want)
	}
	if err := chromedp.Run(app.ctx, chromedp.Evaluate(`window.__s5marker = 1; true`, nil)); err != nil {
		t.Fatal(err)
	}
	if !pollUntil(app.ctx, `!document.getElementById("pwa-reload").hidden`, 5*time.Second) {
		t.Fatalf("on %s (after Back) the standalone Reload control never appeared", scopePage)
	}
	if err := chromedp.Run(app.ctx, chromedp.Evaluate(`document.getElementById("pwa-reload").click(); true`, nil)); err != nil {
		t.Fatal(err)
	}
	if !pollUntil(app.ctx, `window.__s5marker === undefined && document.readyState !== "loading"`, 10*time.Second) {
		t.Errorf("Reload on %s left the old document in place (its marker survived)", scopePage)
	}
	var at string
	if err := chromedp.Run(app.ctx, chromedp.Location(&at)); err != nil || at != want {
		t.Errorf("after Reload the window is at %q (%v), want %q", at, err, want)
	}
}

// firstHref returns the first href with the prefix, or "".
func firstHref(hrefs []string, prefix string) string {
	for _, h := range hrefs {
		if strings.HasPrefix(h, prefix) {
			return h
		}
	}
	return ""
}
