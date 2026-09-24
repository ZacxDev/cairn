package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
)

// Capture is everything one page at one width produced.
type Capture struct {
	Target   Target
	Viewport Viewport

	Screenshot []byte
	AxeJSON    []byte
	DigestJSON []byte // nil when the digest was empty — see [Capture.HasDigest]
	Layout     *PushLayout

	Violations []AxeViolation
	Console    []Event
	Network    []Event

	// DocStatus is the status of this page's OWN document response, checked before anything
	// is measured. See [Browser.CaptureTarget].
	DocStatus int
	// Hrefs is what this page published, populated only when the target says it publishes
	// links. See [ExpandLinks].
	Hrefs []string
}

// AxeViolation is the subset of an axe result the push needs. The `ID` is the whole point:
// The hub's P2 `new_a11y_rules` delta reads a TOP-LEVEL `id` off each stored a11y
// finding, and a producer that flattened to the legacy `"<id> — <help>"` string makes the
// delta go through a derivation path instead of the structured one.
type AxeViolation struct {
	ID          string `json:"id"`
	Impact      string `json:"impact"`
	Help        string `json:"help"`
	Description string `json:"description"`
	HelpURL     string `json:"helpUrl"`
	Nodes       int    `json:"nodes"`
}

// Event is one console message or one network failure, with its origin classified.
type Event struct {
	FirstParty bool
	Text       string
}

// HasDigest answers whether this capture's digest may be attached to a push.
//
// 🔴 A DIGEST THAT DECODES TO THREE EMPTY LISTS IS A 400 THAT REJECTS THE WHOLE PUSH, NOT
// A WEAKER ONE. `a11y-digest.js` returns exactly that on a genuinely bare page AND from
// its own catch-all on a JS exception, so the check is not theoretical — it is the normal
// output of the normal script on a page with nothing interactive. The producer contract
// says to omit the ref for that page instead, which is what this gates.
func (c *Capture) HasDigest() bool {
	if len(c.DigestJSON) == 0 {
		return false
	}
	var d struct {
		Interactive  []json.RawMessage `json:"interactive"`
		FormControls []json.RawMessage `json:"form_controls"`
		Landmarks    []json.RawMessage `json:"landmarks"`
	}
	if err := json.Unmarshal(c.DigestJSON, &d); err != nil {
		return false
	}
	return len(d.Interactive)+len(d.FormControls)+len(d.Landmarks) > 0
}

// Browser is one Chromium, held open across the whole walk so the session cookie survives
// between pages.
type Browser struct {
	ctx    context.Context
	cancel []context.CancelFunc

	base     string
	baseHost string

	mu      sync.Mutex
	console []Event
	netw    []Event
	// docStatus is the status of the last DOCUMENT response, which is the only thing that
	// says whether the page captured is the page requested. See [Browser.CaptureTarget].
	docStatus int
	docURL    string
	// faviconRefusals counts the browser's own `/favicon.ico` requests that this surface
	// answered with an error. See [Browser.FaviconRefusals].
	faviconRefusals int
}

// FaviconPath is the one path a browser requests that no ledger row carries.
const FaviconPath = "/favicon.ico"

// NewBrowser starts headless Chromium.
//
// `--no-sandbox` is set because this runs in CI containers where the namespace sandbox is
// unavailable; the page it loads is one this same process just booted on loopback, so the
// sandbox is not what is keeping anything out. `headless=new` rather than the old mode:
// the old one differs from a real browser in exactly the areas this harness measures
// (layout, computed style).
func NewBrowser(parent context.Context, base string, budget time.Duration) (*Browser, error) {
	u, err := url.Parse(base)
	if err != nil {
		return nil, err
	}
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", "new"),
		chromedp.Flag("no-sandbox", true),
		chromedp.Flag("disable-dev-shm-usage", true),
		chromedp.Flag("hide-scrollbars", false),
	)
	b := &Browser{base: strings.TrimRight(base, "/"), baseHost: u.Hostname()}
	allocCtx, cancelAlloc := chromedp.NewExecAllocator(parent, opts...)
	ctx, cancelCtx := chromedp.NewContext(allocCtx)
	ctx, cancelT := context.WithTimeout(ctx, budget)
	b.ctx = ctx
	b.cancel = []context.CancelFunc{cancelT, cancelCtx, cancelAlloc}

	chromedp.ListenTarget(ctx, b.onEvent)
	if err := chromedp.Run(ctx, network.Enable(),
		// Every page is a COLD load. Without this the second viewport reads a warm cache
		// and under-counts requests, which is the defect the hub's own crawl hit.
		network.SetCacheDisabled(true),
	); err != nil {
		b.Close()
		return nil, fmt.Errorf("starting chromium: %w", err)
	}
	return b, nil
}

// Close tears the browser down, innermost cancel first.
func (b *Browser) Close() {
	for i := len(b.cancel) - 1; i >= 0; i-- {
		b.cancel[i]()
	}
}

// Version is the browser build, reported so a measurement carries the version it was made
// under — a claim about "a browser" is a claim about the one that ran.
func (b *Browser) Version() (string, error) {
	var ua string
	err := chromedp.Run(b.ctx, chromedp.Evaluate(`navigator.userAgent`, &ua))
	return ua, err
}

// onEvent classifies console messages and network failures by ORIGIN. First party is the
// host this walk booted; anything else is third party. The distinction is the hub's and
// is carried on the push, because a third-party 404 is somebody else's problem and a
// first-party one is a broken page.
func (b *Browser) onEvent(ev any) {
	switch e := ev.(type) {
	case *runtime.EventConsoleAPICalled:
		if e.Type != "error" && e.Type != "warning" {
			return
		}
		var parts []string
		for _, a := range e.Args {
			parts = append(parts, string(a.Value))
		}
		b.record(&b.console, Event{FirstParty: true, Text: string(e.Type) + ": " + strings.Join(parts, " ")})
	case *runtime.EventExceptionThrown:
		if e.ExceptionDetails == nil {
			return
		}
		b.record(&b.console, Event{FirstParty: true, Text: "exception: " + e.ExceptionDetails.Text})
	case *network.EventLoadingFailed:
		b.record(&b.netw, Event{FirstParty: true, Text: "loading failed: " + e.ErrorText})
	case *network.EventResponseReceived:
		if e.Response == nil {
			return
		}
		// 🔴 THE DOCUMENT RESPONSE IS RECORDED WHATEVER ITS STATUS, BECAUSE IT IS THE ONLY
		// THING THAT SAYS WHETHER THE PAGE CAPTURED IS THE PAGE REQUESTED. Every other
		// signal here is happy to describe an error page in detail.
		if e.Type == "Document" {
			b.mu.Lock()
			b.docStatus, b.docURL = int(e.Response.Status), e.Response.URL
			b.mu.Unlock()
		}
		if e.Response.Status < 400 {
			return
		}
		// ⚠ `/favicon.ico` IS COUNTED SEPARATELY AND KEPT OUT OF THE PER-PAGE TOTALS, FOR A
		// REASON THAT IS ABOUT THE DIFF RATHER THAN ABOUT TIDINESS. The browser requests it
		// on its own initiative, roughly once per origin, so WHICH page it lands on is an
		// artefact of navigation order — and the hub matches its P2 diff on `url`+
		// `viewport`, so an event that wandered between pages run to run would manufacture
		// a network delta forever. It is a real first-party refusal and it is reported once,
		// at walk level, rather than attributed to an arbitrary page.
		if u, err := url.Parse(e.Response.URL); err == nil && u.Path == FaviconPath && b.isFirstParty(e.Response.URL) {
			b.mu.Lock()
			b.faviconRefusals++
			b.mu.Unlock()
			return
		}
		first := b.isFirstParty(e.Response.URL)
		b.record(&b.netw, Event{FirstParty: first,
			Text: fmt.Sprintf("%d %s", int(e.Response.Status), e.Response.URL)})
	}
}

// FaviconRefusals is how many times this surface refused the browser's own favicon request.
//
// It is a genuine finding rather than noise: no ledger row carries `/favicon.ico`, so the
// dispatcher's uniform refusal answers it, and a developer's network panel shows a
// first-party error they did not cause.
//
// ⚠ WHETHER CHROMIUM ASKS AT ALL IS RUN-DEPENDENT, MEASURED AT TWO POINTS ON THIS TREE: one
// walk recorded a `401 /favicon.ico`, a later walk over the same tree recorded none. So the
// claim this count supports is "the surface refuses it when asked", never "every visit
// produces one". That variability is the second reason the event is reported at walk level
// instead of on a page — the first being that its page attribution is arbitrary.
func (b *Browser) FaviconRefusals() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.faviconRefusals
}

func (b *Browser) isFirstParty(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	return u.Hostname() == b.baseHost || u.Hostname() == ""
}

func (b *Browser) record(into *[]Event, e Event) {
	b.mu.Lock()
	*into = append(*into, e)
	b.mu.Unlock()
}

// drain takes the events recorded since the last drain, so each capture carries its own
// page's events rather than the whole walk's running total.
func (b *Browser) drain() (console, netw []Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	console, netw = b.console, b.netw
	b.console, b.netw = nil, nil
	return console, netw
}

// SignIn drives the real sign-in form and returns the session cookie it obtained.
//
// 🔴 IT MUST BE A BROWSER NAVIGATING AND CLICKING, AND THAT IS MEASURED RATHER THAN
// PREFERRED. `sameOrigin` runs BEFORE authentication and refuses a state-changing request
// with a missing `Origin` header, so a raw HTTP POST to `/sign-in` cannot sign in at all.
// The gate is derived from the METHOD, not from a route class, which is what makes it
// cover the public sign-in row — so there is no way around it and no reason to want one.
//
// ⚠ AND FIVE FAILED ATTEMPTS LOCK OUT THE RESOLVED CLIENT IP, AFTER WHICH A VALID
// CREDENTIAL ANSWERS 401. If a walk starts failing on a token that is known good, the
// lockout is the first thing to suspect and the code is the last: the pod is fresh on
// every run precisely so that the counter is too.
func (b *Browser) SignIn(token string) (*network.Cookie, error) {
	var loc, html string
	if err := chromedp.Run(b.ctx,
		chromedp.EmulateViewport(int64(Desktop.Width), int64(Desktop.Height)),
		chromedp.Navigate(b.base+"/sign-in"),
		// `#token` is the single-field token path's input. Waiting on it rather than on a
		// timeout is what makes a sign-in page that failed to render a loud failure.
		chromedp.WaitVisible(`#token`, chromedp.ByID),
		chromedp.SendKeys(`#token`, token, chromedp.ByID),
		chromedp.Click(`form button[type=submit]`, chromedp.ByQuery),
		chromedp.Sleep(600*time.Millisecond),
		chromedp.Location(&loc),
		chromedp.OuterHTML(`html`, &html, chromedp.ByQuery),
	); err != nil {
		return nil, fmt.Errorf("driving the sign-in form: %w", err)
	}
	if strings.Contains(html, `id="token"`) {
		return nil, fmt.Errorf("sign-in did not take: %s still renders the token field "+
			"(a fresh pod, so a lockout is unlikely; check the token file and the session file's directory)", loc)
	}

	var cookies []*network.Cookie
	if err := chromedp.Run(b.ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		got, err := network.GetCookies().Do(ctx)
		cookies = got
		return err
	})); err != nil {
		return nil, err
	}
	for _, c := range cookies {
		if strings.HasPrefix(c.Name, "__Host-") {
			return c, nil
		}
	}
	// A cookie that is absent from the jar and a cookie the browser refuses to ATTACH are
	// different failures, and only the second one is interesting. Reaching here means the
	// first: the page rendered as signed in, yet the jar has no `__Host-` cookie.
	return nil, fmt.Errorf("signed in, but no __Host- cookie is in the jar (%d cookie(s) present): "+
		"the browser parsed Set-Cookie and then dropped it", len(cookies))
}

// SignOut clears the session by clicking the sign-out control, so the public rows are
// captured in the state a signed-out visitor sees.
//
// It deliberately clicks rather than deleting the cookie out of the jar: a jar edit would
// measure a browser with no cookie, where the real signed-out state is a browser whose
// session the SERVER revoked. `ClearedSessionCookie` is the second half of a logout and
// not the logout; the revocation is the store write, and only the click causes it.
func (b *Browser) SignOut() error {
	var html string
	if err := chromedp.Run(b.ctx,
		chromedp.Navigate(b.base+"/"),
		chromedp.OuterHTML(`html`, &html, chromedp.ByQuery),
	); err != nil {
		return err
	}
	if !strings.Contains(html, `action="/sign-out"`) {
		return fmt.Errorf("no sign-out form on / — cannot reach the signed-out state by clicking")
	}
	return chromedp.Run(b.ctx,
		chromedp.Click(`form[action="/sign-out"] button[type=submit]`, chromedp.ByQuery),
		chromedp.Sleep(400*time.Millisecond),
	)
}

// CaptureTarget navigates one target at one width and collects everything.
func (b *Browser) CaptureTarget(t Target, vp Viewport) (*Capture, error) {
	b.drain() // discard whatever the previous navigation left behind

	c := &Capture{Target: t, Viewport: vp}
	b.mu.Lock()
	b.docStatus, b.docURL = 0, ""
	b.mu.Unlock()

	if err := chromedp.Run(b.ctx,
		// 🔴 THE WIDTH IS SET EXPLICITLY, AND THAT IS THE WHOLE POINT OF THE JOB. A
		// harness whose config PINS a dimension is structurally blind to that
		// dimension's defects; this surface has never been rendered at any width, so
		// "the default viewport" would be a dimension nobody chose.
		emulation.SetDeviceMetricsOverride(int64(vp.Width), int64(vp.Height), 1, vp.Name == Mobile.Name),
		chromedp.Navigate(b.base+t.Path),
		chromedp.Sleep(350*time.Millisecond),
	); err != nil {
		return nil, fmt.Errorf("navigating %s at %s: %w", t.Path, vp.Name, err)
	}

	// 🔴 THE DOCUMENT'S OWN STATUS IS CHECKED BEFORE ANYTHING IS MEASURED, AND THIS GUARD
	// IS A REGRESSION GUARD RATHER THAN AN INVARIANT ONE — IT CAUGHT A DEFECT IN THIS
	// HARNESS'S FIRST DRAFT.
	//
	// That draft expanded the share row into `/share?scope=<name>` and every one of those
	// targets 404'd, because the parameter is a `control.ID` and not a name. Nothing
	// downstream noticed: axe ran happily on the error page, the screenshot was a valid
	// PNG, the layout script returned real numbers, and the run printed "62 axe violations
	// across 6 rules" — over a document whose `<html>` had no `lang`, no `<title>`, no
	// `<main>` and no `<h1>`, none of which is a fact about cairn. Every other signal in
	// this program is happy to describe an error page in detail, which is exactly why the
	// document status has to be a REFUSAL and not a field somebody might read.
	b.mu.Lock()
	status, docURL := b.docStatus, b.docURL
	b.mu.Unlock()
	if status == 0 {
		return nil, fmt.Errorf("no document response observed for %s at %s: the walk cannot vouch for what it captured", t.Path, vp.Name)
	}
	if status < 200 || status >= 300 {
		return nil, fmt.Errorf("%s at %s answered %d (document %s): the page captured is not the page requested, "+
			"and every other signal here would describe the error page as if it were the surface",
			t.Path, vp.Name, status, docURL)
	}
	c.DocStatus = status

	// The hrefs this page publishes, collected only where the target says it publishes
	// some. `[href]` rather than `a[href]` would pick up `<link>`; the walk wants links a
	// person can click.
	if t.ExpandLinks {
		// An array of strings, returned BY VALUE rather than stringified. A `JSON.stringify`
		// here would come back as a JSON *string* containing JSON, which `Runtime.evaluate`
		// hands over as a quoted scalar — the axe path below has to unwrap exactly that, and
		// it only has to because an axe result holds DOM references that cannot cross. A
		// plain string array has no such problem, so stringifying it would be a decode step
		// bought for nothing.
		if err := chromedp.Run(b.ctx, chromedp.Evaluate(
			`Array.from(document.querySelectorAll("a[href]")).map(a => a.getAttribute("href"))`,
			&c.Hrefs)); err != nil {
			return nil, fmt.Errorf("reading %s's links at %s: %w", t.Path, vp.Name, err)
		}
	}

	// Layout smells, from the hub's own script, returning its own raw keys.
	var layoutRaw string
	if err := chromedp.Run(b.ctx, chromedp.Evaluate(layoutSmellsJS, &layoutRaw)); err != nil {
		return nil, fmt.Errorf("layout smells on %s at %s: %w", t.Path, vp.Name, err)
	}
	var layout PushLayout
	if err := json.Unmarshal([]byte(layoutRaw), &layout); err != nil {
		return nil, fmt.Errorf("layout smells on %s at %s returned %q: %w", t.Path, vp.Name, layoutRaw, err)
	}
	c.Layout = &layout

	// The a11y digest, likewise verbatim.
	var digestRaw string
	if err := chromedp.Run(b.ctx, chromedp.Evaluate(a11yDigestJS, &digestRaw)); err != nil {
		return nil, fmt.Errorf("a11y digest on %s at %s: %w", t.Path, vp.Name, err)
	}
	c.DigestJSON = []byte(digestRaw)

	// axe. 🔴 INJECTED THROUGH `Runtime.evaluate`, WHICH IS DEBUGGER-PRIVILEGED AND
	// BYPASSES CSP. A `<script>` tag would be blocked outright: this surface's CSP names
	// no `script-src` at all, so `default-src 'none'` governs scripts. That the bypass
	// actually holds on THIS page under THIS CSP is measured by `spike/main.go`, not
	// assumed from the specification.
	if err := chromedp.Run(b.ctx, chromedp.Evaluate(axeJS, nil)); err != nil {
		return nil, fmt.Errorf("injecting axe on %s at %s: %w", t.Path, vp.Name, err)
	}
	var axeRaw []byte
	if err := chromedp.Run(b.ctx, chromedp.Evaluate(axeRunJS, &axeRaw, awaitPromise)); err != nil {
		return nil, fmt.Errorf("axe.run on %s at %s: %w", t.Path, vp.Name, err)
	}
	// `Runtime.evaluate` returns a JSON string value; unwrap it once to get the object.
	var unwrapped string
	if err := json.Unmarshal(axeRaw, &unwrapped); err == nil {
		axeRaw = []byte(unwrapped)
	}
	c.AxeJSON = axeRaw
	var axeResult struct {
		Violations []struct {
			AxeViolation
			Nodes []json.RawMessage `json:"nodes"`
		} `json:"violations"`
	}
	if err := json.Unmarshal(axeRaw, &axeResult); err != nil {
		return nil, fmt.Errorf("axe result on %s at %s was not understood: %w", t.Path, vp.Name, err)
	}
	for _, v := range axeResult.Violations {
		got := v.AxeViolation
		got.Nodes = len(v.Nodes)
		c.Violations = append(c.Violations, got)
	}

	// The screenshot, LAST, so axe's own DOM mutations are gone by the time the pixels
	// are taken. axe cleans up after itself, but "it cleans up" is a claim about a
	// library and the order costs nothing.
	if err := chromedp.Run(b.ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		shot, err := page.CaptureScreenshot().
			WithCaptureBeyondViewport(true).
			WithFormat(page.CaptureScreenshotFormatPng).
			Do(ctx)
		c.Screenshot = shot
		return err
	})); err != nil {
		return nil, fmt.Errorf("screenshot of %s at %s: %w", t.Path, vp.Name, err)
	}

	c.Console, c.Network = b.drain()
	return c, nil
}

// awaitPromise makes `Runtime.evaluate` resolve the promise `axe.run` returns.
func awaitPromise(p *runtime.EvaluateParams) *runtime.EvaluateParams {
	return p.WithAwaitPromise(true)
}

// axeRunJS asks axe for the whole result object and serialises it in the page, because
// `Runtime.evaluate` returns values by value and an axe result holds DOM references that
// do not survive the crossing.
//
// `resultTypes: ["violations"]` keeps the payload bounded: a full axe result carries every
// PASS too, which on this surface is two orders of magnitude more bytes for a set nothing
// reads. The violations are what the hub's rule delta is computed from.
const axeRunJS = `axe.run(document, {resultTypes: ["violations"]}).then(r => JSON.stringify({
	violations: r.violations,
	testEngine: r.testEngine,
	url: r.url,
	timestamp: "pinned",
}))`
