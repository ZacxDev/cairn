package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ZacxDev/cairn/internal/ui"
	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/chromedp"
)

// 🔴 THIS FILE IS S0 OF THE MOBILE PLAN'S INSTRUMENT VALIDATION: every touch measurement the walk
// now takes is shown able to go NON-ZERO (positive control) and, where it is a refusal, able to go
// RED (negative control) — in a real chromium, because each of them is a claim about what a
// browser does and not about what this module's Go does.

// touchPage serves `body` as a page that opted into device-width sizing, so `CaptureTarget`'s
// width assertion binds and every capture here is a page in the shape the real surface has.
func touchPage(t *testing.T, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<!doctype html><html lang="en"><head><meta charset="utf-8"><title>touch</title>` +
			`<meta name="viewport" content="width=device-width, initial-scale=1">` +
			`<style>body{margin:0;font-family:system-ui,sans-serif}</style></head><body><main><h1>touch</h1>` +
			body + `</main></body></html>`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func browserFor(t *testing.T, base string) *Browser {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	t.Cleanup(cancel)
	b, err := NewBrowser(ctx, base, 3*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(b.Close)
	return b
}

// TestTouchEmulationReachesThePageAtEveryRung is the POSITIVE half of the reachability control:
// `(pointer: coarse)` is TRUE at every touch rung and FALSE at every other one, read back from the
// page, over a sequence that crosses the touch boundary in BOTH directions on one tab.
//
// 🔴 THE SEQUENCE IS THE TEST. Touch emulation persists across navigations, so the order mobile →
// tablet → laptop is what exposes a missing DISABLE (laptop would read coarse), and the trailing
// laptop → mobile → laptop leg is what exposes an enable that only took on a fresh tab. A sequence
// starting at a non-touch rung on a fresh tab would read `false` there however broken the code is.
func TestTouchEmulationReachesThePageAtEveryRung(t *testing.T) {
	chromiumOrRefuse(t)
	b := browserFor(t, touchPage(t, `<p>pointer</p>`).URL)

	seq := append(append([]Viewport{}, Viewports...), Mobile, Laptop)
	var captures []*Capture
	for _, vp := range seq {
		c, err := b.CaptureTarget(Target{Path: "/", PushURL: "/touch", LedgerRow: "control"}, vp)
		if err != nil {
			t.Fatalf("capturing at %s: %v", vp.Name, err)
		}
		captures = append(captures, c)
		wantPoints := 0
		if vp.Touch {
			wantPoints = touchPoints
		}
		if c.Pointer.Coarse != vp.Touch || c.Pointer.MaxTouchPoints != wantPoints {
			t.Errorf("%s (touch=%v): (pointer: coarse)=%v maxTouchPoints=%d, want coarse=%v maxTouchPoints=%d",
				vp.Name, vp.Touch, c.Pointer.Coarse, c.Pointer.MaxTouchPoints, vp.Touch, wantPoints)
		}
		t.Logf("%-9s touch=%-5v coarse=%-5v hover:none=%-5v maxTouchPoints=%d",
			vp.Name, vp.Touch, c.Pointer.Coarse, c.Pointer.HoverNone, c.Pointer.MaxTouchPoints)
	}
	if err := refuseUnreachableTouch(captures); err != nil {
		t.Fatalf("the reachability refusal fired on a walk whose emulation is correct: %v", err)
	}
}

// TestTheReachabilityRefusalGoesREDWhenEmulationIsOff is the NEGATIVE half, in a real browser.
//
// 🔴 IT BYPASSES `CaptureTarget`'s EMULATION STEP RATHER THAN REUSING IT, because a control built
// from the step under suspicion is a second sample of the same unknown. Each half drives the
// protocol by hand into the broken state — R10's two failure rows — reads the page through the SAME
// probe the walk uses, and requires `refuseUnreachableTouch` to refuse and NAME the capture:
//
//   - a touch rung with the device-metrics `mobile` flag and NO touch emulation (what every walk did
//     before S0): coarse must read false, so the probe is not a constant;
//   - a laptop rung after touch was enabled and NEVER DISABLED: coarse must read true (stale).
func TestTheReachabilityRefusalGoesREDWhenEmulationIsOff(t *testing.T) {
	chromiumOrRefuse(t)
	srv := touchPage(t, `<p>pointer</p>`)
	b := browserFor(t, srv.URL)

	probe := func(vp Viewport, touch chromedp.Action) *Capture {
		t.Helper()
		var raw string
		if err := chromedp.Run(b.ctx,
			emulation.SetDeviceMetricsOverride(int64(vp.Width), int64(vp.Height), 1, vp.Touch),
			touch,
			chromedp.Navigate(srv.URL+"/"),
			chromedp.Evaluate(pointerProbeJS, &raw),
		); err != nil {
			t.Fatal(err)
		}
		var p PointerProbe
		if err := json.Unmarshal([]byte(raw), &p); err != nil {
			t.Fatal(err)
		}
		return &Capture{Target: Target{Path: "/"}, Viewport: vp, Pointer: &p}
	}
	noop := chromedp.ActionFunc(func(context.Context) error { return nil })

	// Half 1: the mobile flag alone. A correct laptop capture is added so the only defect in the
	// set is the one under test.
	offMobile := probe(Mobile, emulation.SetTouchEmulationEnabled(false))
	if offMobile.Pointer.Coarse {
		t.Fatalf("with touch emulation OFF a mobile-metrics page read (pointer: coarse)=true — the probe cannot " +
			"tell emulation from its absence, so the refusal below would be vacuous")
	}
	okLaptop := probe(Laptop, emulation.SetTouchEmulationEnabled(false))
	err := refuseUnreachableTouch([]*Capture{offMobile, okLaptop})
	if err == nil || !strings.Contains(err.Error(), "TOUCH REACHABILITY FAILED") ||
		!strings.Contains(err.Error(), "at mobile (390px): (pointer: coarse) is false on a TOUCH capture") {
		t.Fatalf("a mobile capture taken with touch emulation OFF was not refused by name: %v", err)
	}
	t.Logf("RED, emulation never enabled: %v", firstLine(err))

	// Half 2: enable at a touch rung, then move to laptop WITHOUT disabling.
	onMobile := probe(Mobile, emulation.SetTouchEmulationEnabled(true).WithMaxTouchPoints(touchPoints))
	staleLaptop := probe(Laptop, noop)
	if !staleLaptop.Pointer.Coarse {
		t.Fatalf("touch emulation did NOT persist into the next navigation (laptop read coarse=false with no " +
			"disable). R10 measured that it does; if chromium changed, `touchEmulation`'s explicit disable is no " +
			"longer what the laptop half of the refusal guards, and its comment wants correcting")
	}
	err = refuseUnreachableTouch([]*Capture{onMobile, staleLaptop})
	if err == nil || !strings.Contains(err.Error(), "at laptop (1280px): (pointer: coarse) is true on a non-touch capture") {
		t.Fatalf("a laptop capture with STALE touch emulation was not refused by name: %v", err)
	}
	t.Logf("RED, emulation never disabled: %v", firstLine(err))
}

func firstLine(err error) string {
	s := err.Error()
	if i := strings.LastIndex(s, "\n"); i >= 0 {
		return strings.TrimSpace(s[i:])
	}
	return s
}

// TestTheTargetSizeRuleRunsOnlyBecauseItIsEnabled is the axe `target-size` (WCAG 2.5.8) pair.
//
// 🔴 THE FIXTURE IS TWO ADJACENT 12×12 TARGETS, NOT ONE, because a LONE small target passes 2.5.8's
// spacing exception — a 24px circle around its centre touches nothing — so a single 12px button is
// a textbook fixture the rule correctly ignores, and a control built on it would read 0 whether or
// not the rule ran.
//
// Three readings, all on real pages: the adjacent 12px pair gives ≥ 1 under the walk's own axe call
// (positive control); a 48px pair gives 0 (the rule is not counting every button); and the SAME 12px
// page under axe's DEFAULT options gives no `target-size` result at all — which is what proves the
// walk's explicit enable is what reaches the rule.
func TestTheTargetSizeRuleRunsOnlyBecauseItIsEnabled(t *testing.T) {
	chromiumOrRefuse(t)
	const btn = `<button aria-label="%s" style="width:%dpx;height:%dpx;padding:0;margin:0;border:0;display:inline-block;vertical-align:top"></button>`
	pair := func(px int) string {
		return fmt.Sprintf(btn, "a", px, px) + fmt.Sprintf(btn, "b", px, px)
	}

	small := browserFor(t, touchPage(t, `<div>`+pair(12)+`</div>`).URL)
	c, err := small.CaptureTarget(Target{Path: "/", PushURL: "/ts-small", LedgerRow: "control"}, Mobile)
	if err != nil {
		t.Fatal(err)
	}
	if c.TargetSizeNodes() < 1 {
		t.Fatalf("POSITIVE CONTROL FAILED: two adjacent 12x12 buttons gave target-size=%d; the rule is not "+
			"running under the walk's axe call, so every target-size zero the walk reports is unreadable (violations: %+v)",
			c.TargetSizeNodes(), c.Violations)
	}
	t.Logf("positive control: adjacent 12px pair -> target-size=%d node(s)", c.TargetSizeNodes())

	// The SAME page, axe's DEFAULT options: the rule ships disabled, so it must not appear.
	var raw string
	if err := chromedp.Run(small.ctx, chromedp.Evaluate(
		`axe.run(document, {resultTypes: ["violations"]}).then(r => JSON.stringify(r.violations.map(v => v.id)))`,
		&raw, awaitPromise)); err != nil {
		t.Fatal(err)
	}
	var ids []string
	if err := json.Unmarshal([]byte(raw), &ids); err != nil {
		t.Fatalf("default axe run returned %q: %v", raw, err)
	}
	for _, id := range ids {
		if id == axeTargetSizeRule {
			t.Fatalf("axe's DEFAULT run reported %q on its own — the rule is no longer disabled by default, so the "+
				"walk's explicit enable is no longer what reaches it and `axeRunJS`'s comment wants correcting", id)
		}
	}
	t.Logf("same page, default axe options: target-size absent (rules reported: %v)", ids)

	large := browserFor(t, touchPage(t, `<div>`+pair(48)+`</div>`).URL)
	c, err = large.CaptureTarget(Target{Path: "/", PushURL: "/ts-large", LedgerRow: "control"}, Mobile)
	if err != nil {
		t.Fatal(err)
	}
	if n := c.TargetSizeNodes(); n != 0 {
		t.Fatalf("two adjacent 48x48 buttons gave target-size=%d; the rule is counting buttons, not small ones", n)
	}
	t.Logf("negative control: adjacent 48px pair -> target-size=0")
}

// TestTheInputFontMeasurementSeesA14pxInput is the font measurement's control pair.
//
// 🔴 A 14px INPUT IS WHAT THE SURFACE SHIPS (`text-sm`), so this is the defect's own shape rather than
// a textbook one. Beside it: a 16px input that must NOT be reported, a 10px CHECKBOX and a 10px
// hidden input that must not be COUNTED (neither takes keyboard focus, so neither zooms iOS), and a
// 10px input inside `display:none` that is not visible at all.
func TestTheInputFontMeasurementSeesA14pxInput(t *testing.T) {
	chromiumOrRefuse(t)
	b := browserFor(t, touchPage(t, `<form>`+
		`<input id="small" type="search" style="font-size:14px">`+
		`<input id="ok" type="text" style="font-size:16px">`+
		`<input id="tick" type="checkbox" style="font-size:10px">`+
		`<input type="hidden" name="h" style="font-size:10px">`+
		`<div style="display:none"><input id="gone" style="font-size:10px"></div>`+
		`</form>`).URL)
	for _, vp := range []Viewport{Mobile, Laptop} {
		c, err := b.CaptureTarget(Target{Path: "/", PushURL: "/font", LedgerRow: "control"}, vp)
		if err != nil {
			t.Fatal(err)
		}
		if c.Touch.InputsMeasured != 2 {
			t.Errorf("%s: measured %d input(s), want 2 (#small and #ok; the checkbox, the hidden input and the "+
				"display:none one are not keyboard-focusable visible fields)", vp.Name, c.Touch.InputsMeasured)
		}
		if len(c.Touch.SmallInputs) != 1 || c.Touch.SmallInputs[0].Selector != "#small" || c.Touch.SmallInputs[0].FontPx != 14 {
			t.Fatalf("POSITIVE CONTROL FAILED at %s: small inputs = %+v, want exactly [#small at 14px]. A measurement "+
				"that cannot see a 14px input makes every zero it reports unreadable", vp.Name, c.Touch.SmallInputs)
		}
		t.Logf("%s: inputs<16px = %+v of %d measured", vp.Name, c.Touch.SmallInputs, c.Touch.InputsMeasured)
	}
}

// TestTheJournalWorldReachesTheGrantForm boots the journal-backed world and walks its rows, the way
// `run` does, and requires the fall-back refusal to PASS on it — the positive half of
// `refuseJournalWorldFellBack`; `TestTheJournalFallBackRefusalNamesEachDefect` is the negative.
func TestTheJournalWorldReachesTheGrantForm(t *testing.T) {
	chromiumOrRefuse(t)
	bin := os.Getenv("UIAUDIT_CAIRN_UI")
	if bin == "" {
		t.Fatal("UIAUDIT_CAIRN_UI must name a built cairn-ui: this is the only test that boots the journal world")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	targets, _, err := Targets(ui.DeclaredRouteLedger())
	if err != nil {
		t.Fatal(err)
	}
	port, err := aFreeLoopbackPort()
	if err != nil {
		t.Fatal(err)
	}
	captures, err := walkJournalWorld(ctx, os.Getenv("UIAUDIT_REPO_ROOT"), bin, t.TempDir(), port, 3*time.Minute,
		targets, ui.DeclaredRouteLedger())
	if err != nil {
		t.Fatal(err)
	}
	if err := refuseJournalWorldFellBack(captures); err != nil {
		t.Fatal(err)
	}
	if err := refuseUnreachableTouch(captures); err != nil {
		t.Fatal(err)
	}
	for _, c := range captures {
		if c.World != JournalWorld {
			t.Fatalf("a journal-world capture is labelled %q", c.World)
		}
	}
	t.Logf("journal world: %d capture(s)", len(captures))
}

// TestTheJournalFallBackRefusalNamesEachDefect is the negative control on `refuseJournalWorldFellBack`.
func TestTheJournalFallBackRefusalNamesEachDefect(t *testing.T) {
	good := func() []*Capture {
		return []*Capture{
			{Target: Target{Path: ui.SharePath}, World: JournalWorld},
			{Target: Target{Path: ui.SharePath + "?scope=scp_x"}, World: JournalWorld,
				FormActions: []string{ui.SignOutPath, ui.UnsharePath, ui.SharePath}},
			{Target: Target{Path: ui.InvitePath}, World: JournalWorld, ReadOnlyNotices: []string{ui.NoInviteStore}},
		}
	}
	if err := refuseJournalWorldFellBack(good()); err != nil {
		t.Fatalf("POSITIVE CONTROL FAILED: a world that reached the grant form was refused: %v", err)
	}
	for _, tc := range []struct {
		name    string
		break_  func([]*Capture) []*Capture
		wantSub string
	}{
		{"the per-scope page carries no grant form", func(cs []*Capture) []*Capture {
			cs[1].FormActions = []string{ui.SignOutPath}
			return cs
		}, "NO per-scope share page with its grant form"},
		{"the grant form is on the INDEX, not a per-scope page", func(cs []*Capture) []*Capture {
			cs[0].FormActions, cs[1].FormActions = []string{ui.SharePath}, nil
			return cs
		}, "NO per-scope share page with its grant form"},
		{"the share page says the authority is read-only (token-file)", func(cs []*Capture) []*Capture {
			cs[0].ReadOnlyNotices = []string{ui.ReadOnlyAuthority}
			return cs
		}, "TOKEN-FILE authority's read-only notice"},
		{"the invite index lacks the no-database notice", func(cs []*Capture) []*Capture {
			cs[2].ReadOnlyNotices = nil
			return cs
		}, "NONE carries the no-database notice"},
		{"no capture at all", func([]*Capture) []*Capture { return nil }, "NO per-scope share page"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := refuseJournalWorldFellBack(tc.break_(good()))
			if err == nil || !strings.Contains(err.Error(), tc.wantSub) {
				t.Fatalf("want a refusal naming %q, got %v", tc.wantSub, err)
			}
		})
	}
}

// TestTheJournalWorldIsNeverPushed pins `BuildPayload`'s filter: a journal-world capture at a PUSHED
// viewport carries a `PushURL` the token-file world also pushes, so it must not reach the payload.
func TestTheJournalWorldIsNeverPushed(t *testing.T) {
	mk := func(world string) *Capture {
		return &Capture{
			Target: Target{Path: ui.SharePath, PushURL: ui.SharePath}, Viewport: Mobile, World: world,
			Screenshot: []byte("\x89PNG\r\n\x1a\n"), AxeJSON: []byte(`{"violations":[]}`),
			Layout: &PushLayout{InnerWidth: Mobile.Width, ScrollWidth: Mobile.Width},
		}
	}
	p, _, err := BuildPayload("t", []*Capture{mk(""), mk(JournalWorld)})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Pages) != 1 {
		t.Fatalf("the payload holds %d page(s) from one token-file and one journal capture of %s; want 1",
			len(p.Pages), ui.SharePath)
	}
}

// TestTheWholeWalkSignalsAreTheTokenFileWorlds pins the SCOPE of `printSignalSummary`'s numbers:
// the whole-walk lines count the token-file world only, and the digests line counts PUSHED
// captures only. Fixture tap counts are pairwise distinct (3, 5, 700) so any wrong inclusion
// moves the sum to a value the right one cannot produce.
func TestTheWholeWalkSignalsAreTheTokenFileWorlds(t *testing.T) {
	digest := []byte(`{"interactive":[{"role":"button"}],"form_controls":[],"landmarks":[]}`)
	mk := func(vp Viewport, world string, tap int) *Capture {
		return &Capture{
			Target: Target{Path: "/", PushURL: "/"}, Viewport: vp, World: world, DigestJSON: digest,
			Screenshot: []byte("\x89PNG\r\n\x1a\n"), AxeJSON: []byte(`{"violations":[]}`),
			Layout: &PushLayout{InnerWidth: vp.Width, ScrollWidth: vp.Width, SmallTapTargets: tap},
		}
	}
	all := []*Capture{mk(Mobile, "", 3), mk(Laptop, "", 5), mk(Mobile, JournalWorld, 700)}
	s := summarizeSignals(all)
	if s.tap != 8 || len(s.tokenFile) != 2 || s.journal != 1 {
		t.Errorf("tap=%d token-file=%d journal=%d, want 8/2/1 — the whole-walk lines must count the token-file "+
			"world only (3+5), never the journal world's 700", s.tap, len(s.tokenFile), s.journal)
	}
	if s.digests != 1 || s.pushed != 1 {
		t.Errorf("digests=%d of pushed=%d, want 1 of 1 — only the token-file MOBILE capture is pushed; the laptop "+
			"one is a local width and the journal one is never pushed", s.digests, s.pushed)
	}
	p, _, err := BuildPayload("t", all)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Pages) != s.pushed {
		t.Errorf("the payload holds %d page(s) but the summary counts %d pushed — two answers to one question",
			len(p.Pages), s.pushed)
	}
}
