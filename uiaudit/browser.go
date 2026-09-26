package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/ZacxDev/cairn/internal/identity"
	"github.com/ZacxDev/cairn/internal/ui"
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
	// LandedURL is where the browser ACTUALLY ended up, which a 2xx does not tell you — see
	// [Browser.CaptureTarget]'s redirect guard.
	LandedURL string
	// Hrefs is what this page published, populated only when the target says it publishes
	// links. See [ExpandLinks].
	Hrefs []string

	// ScriptCount is `document.scripts.length` as the BROWSER counted it after the page
	// settled.
	//
	// 🔴 IT IS COUNTED IN THE DOM RATHER THAN GREPPED OUT OF THE HTML, AND THAT IS THE
	// WHOLE VALUE OF MEASURING IT HERE. `internal/ui`'s XSS story partly rests on this
	// surface shipping no script at all, and a string search for `<script` over the served
	// bytes cannot see a script an INJECTION created, a `<script>` a parser recovered from
	// malformed markup, or one a subresource inserted. `document.scripts` is what the
	// browser actually has. It is a measurement and not a refusal at this level: this
	// module's own positive-control page carries a script on purpose, so the assertion
	// belongs to the walk over the real surface — see `refuseWalkRegressions`.
	ScriptCount int
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
	// requestURLs pairs a request id with the URL it was issued for, because
	// `EventLoadingFailed` carries no URL of its own. Bounded by `maxTrackedRequests`.
	requestURLs map[network.RequestID]string
}

// FaviconPath is the one path a browser requests that no ledger row carries.
const FaviconPath = "/favicon.ico"

// maxTrackedRequests bounds the request-id→URL map. The hermetic page issues a handful; a cap
// exists because the map is fed by page-initiated requests, whose COUNT is chosen by the page.
const maxTrackedRequests = 4096

// isFaviconRefusal is the ONE favicon predicate, shared by both network branches.
//
// 🔴 IT IS ONE FUNCTION BECAUSE IT WAS TWO PLACES AND ONLY ONE OF THEM HAD IT. The carve-out lived
// inline in the response branch, so a favicon failure arriving as a LOADING FAILURE was counted as a
// first-party network event on whatever page happened to be loading — the exact per-page attribution
// the carve-out exists to prevent. One predicate, both callers.
func (b *Browser) isFaviconRefusal(raw string) bool {
	if raw == "" {
		return false
	}
	u, err := url.Parse(raw)
	return err == nil && u.Path == FaviconPath && b.isFirstParty(raw)
}

// urlForRequest recovers the URL a request id was issued for, or "" when it was never seen.
func (b *Browser) urlForRequest(id network.RequestID) string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.requestURLs[id]
}

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
	b := &Browser{
		base:        strings.TrimRight(base, "/"),
		baseHost:    u.Hostname(),
		requestURLs: map[network.RequestID]string{},
	}
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
		// 🔴 THIS EVENT CARRIES NO URL, WHICH BREAKS BOTH THINGS THE SIBLING BRANCH DOES. It has a
		// `RequestID` and an `ErrorText` and nothing to classify by — so the `FirstParty: true` here
		// is an ASSUMPTION, not a classification, and it contradicts `onEvent`'s own doc claiming
		// origin is classified by host. It also means the `/favicon.ico` carve-out below cannot apply
		// to this branch: there is no path to compare.
		//
		// The fix is to remember the URL each request was issued for, so this branch can classify and
		// carve out exactly like the other one. `EventRequestWillBeSent` carries both the id and the
		// URL, which is where `requestURLs` is populated.
		//
		// ⚠ FREQUENCY UNMEASURED ON THIS SURFACE: the hermetic page asks for nothing, so no walk has
		// ever recorded a loading failure. That is why this was invisible — the branch has never
		// fired in anger, and its correctness was never tested by a run.
		url := b.urlForRequest(e.RequestID)
		if b.isFaviconRefusal(url) {
			b.mu.Lock()
			b.faviconRefusals++
			b.mu.Unlock()
			return
		}
		text := "loading failed: " + e.ErrorText
		if url == "" {
			text += " (no URL on this event; origin not classifiable)"
		}
		b.record(&b.netw, Event{FirstParty: url == "" || b.isFirstParty(url), Text: text})
	case *network.EventRequestWillBeSent:
		// The only place a request id can be paired with its URL. Bounded, because a page that
		// issued unboundedly many requests would otherwise grow this map without limit.
		if e.Request == nil {
			return
		}
		b.mu.Lock()
		if len(b.requestURLs) < maxTrackedRequests {
			b.requestURLs[e.RequestID] = e.Request.URL
		}
		b.mu.Unlock()
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
		if b.isFaviconRefusal(e.Response.URL) {
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
// 🔴 THE SELECTOR NAMES THE FORM BY ITS ACTION, AND A BARE `form button[type=submit]` WAS A
// MEASURED DEFECT RATHER THAN A STYLE POINT.
//
// The sign-in page grew a SECOND form — an OAuth provider button — and it comes FIRST in document
// order. `chromedp.ByQuery` takes the first match, so a bare selector clicked "Sign in with
// GitHub" and started a provider flight instead of submitting the token. Measured on the merged
// tree: `<form class="signin-provider" method="post" action="/sign-in/github">` precedes
// `<form class="signin" method="post" action="/sign-in">`.
//
// The action is `ui.SignInPath`, so this selector is derived from the same constant the renderer's
// form action is, and a page that adds a third form cannot steal the click.
var signInSubmit = `form[action="` + ui.SignInPath + `"] button[type=submit]`

func (b *Browser) SignIn(token string) (*network.Cookie, error) {
	var loc, html string
	if err := chromedp.Run(b.ctx,
		chromedp.EmulateViewport(int64(Desktop.Width), int64(Desktop.Height)),
		chromedp.Navigate(b.base+ui.SignInPath),
		// `#token` is the single-field token path's input. Waiting on it rather than on a
		// timeout is what makes a sign-in page that failed to render a loud failure.
		chromedp.WaitVisible(`#token`, chromedp.ByID),
		chromedp.SendKeys(`#token`, token, chromedp.ByID),
		chromedp.Click(signInSubmit, chromedp.ByQuery),
		chromedp.Sleep(600*time.Millisecond),
		chromedp.Location(&loc),
		chromedp.OuterHTML(`html`, &html, chromedp.ByQuery),
	); err != nil {
		return nil, fmt.Errorf("driving the sign-in form (selector %q): %w", signInSubmit, err)
	}

	// 🔴 THE VERDICT IS THE COOKIE, NOT THE ABSENCE OF A WORD, AND THE ORDER OF THESE TWO CHECKS
	// IS THE FIX FOR A SECOND DEFECT THE SAME MEASUREMENT EXPOSED.
	//
	// The old code decided "sign-in took" from `!strings.Contains(html, 'id="token"')` — a guard
	// on a SPELLING, satisfied by ANY page that happens not to render that field. When the click
	// went to the OAuth button, the page it landed on satisfied it, and the walk reported
	// "signed in, but no __Host- cookie is in the jar" — a contradiction it should never have
	// been able to express. Asserting the STATE (a session cookie exists) rather than the absence
	// of a word makes the two checks agree by construction.
	var cookies []*network.Cookie
	if err := chromedp.Run(b.ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		got, err := network.GetCookies().Do(ctx)
		cookies = got
		return err
	})); err != nil {
		return nil, err
	}
	// 🔴 THE COOKIE IS NAMED, NOT PREFIX-MATCHED, AND THE PREFIX VERSION WAS A LATENT BUG THAT
	// THE MERGED TREE ARMS. `__Host-` is a SECURITY prefix, not an identity: the auth change adds
	// `__Host-cairn-oauth` beside `__Host-cairn-session`, so "the first cookie whose name starts
	// with `__Host-`" is decided by jar iteration order. Taking the wrong one would have this
	// function report success while returning an OAuth flight cookie, and every attribute the
	// caller then prints — and `TestTheSessionCookiesFourFlagsAreHONOUREDByTheBrowser` asserts —
	// would be about the wrong cookie. `identity.SessionCookieName` is the one spelling.
	var session *network.Cookie
	for _, c := range cookies {
		if c.Name == identity.SessionCookieName {
			session = c
			break
		}
	}
	if session == nil {
		names := make([]string, 0, len(cookies))
		for _, c := range cookies {
			names = append(names, c.Name)
		}
		return nil, fmt.Errorf("sign-in did not take: no %s cookie in the jar after submitting %q "+
			"(landed on %s; jar holds %d cookie(s): %s; token field still rendered: %v). Either the click "+
			"missed the token form, or the pod refused the credential — a fresh pod makes the five-attempt "+
			"lockout unlikely, so check the token file and that the session file's directory is writable",
			identity.SessionCookieName, signInSubmit, loc, len(cookies), strings.Join(names, ", "),
			strings.Contains(html, `id="token"`))
	}
	// The weaker corroboration, kept because it distinguishes two worlds the cookie cannot: a
	// cookie set while the page STILL shows the token form means the redirect did not happen.
	if strings.Contains(html, `id="token"`) {
		return nil, fmt.Errorf("a session cookie was set but %s still renders the token field: "+
			"the credential was accepted and the post-sign-in redirect did not happen", loc)
	}
	return session, nil
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
	if !strings.Contains(html, `action="`+ui.SignOutPath+`"`) {
		return fmt.Errorf("no sign-out form on %s — cannot reach the signed-out state by clicking", ui.RootPath)
	}

	// 🔴 THE CLICK IS VERIFIED, AND IT USED TO BE CLICK-SLEEP-RETURN-NIL WHILE THE CALLER PRINTED
	// "the server revoked the session". That was unfalsifiable: if the POST had been refused, the
	// only page captured afterwards is the sign-in page, which renders IDENTICALLY whether the
	// session was revoked or the click never landed — so the log's claim about a STORE WRITE rested
	// on nothing.
	//
	// Two discriminators, and the jar is the one this code can read directly: a successful sign-out
	// clears the session cookie. The handler also redirects to the sign-in path, so the landed
	// location is the second. The pod's own log carries `sign-out: a session was revoked`, which is
	// the third and is left to the caller — `World.Log()` has it, and a test asserting on a
	// subprocess's log text is a coupling this function should not create.
	if err := chromedp.Run(b.ctx,
		chromedp.Click(`form[action="`+ui.SignOutPath+`"] button[type=submit]`, chromedp.ByQuery),
		chromedp.Sleep(400*time.Millisecond),
	); err != nil {
		return fmt.Errorf("clicking sign-out: %w", err)
	}

	var landed string
	var cookies []*network.Cookie
	if err := chromedp.Run(b.ctx,
		chromedp.Location(&landed),
		chromedp.ActionFunc(func(ctx context.Context) error {
			got, err := network.GetCookies().Do(ctx)
			cookies = got
			return err
		}),
	); err != nil {
		return err
	}

	for _, c := range cookies {
		if c.Name == identity.SessionCookieName {
			return fmt.Errorf("sign-out did not take: %s is still in the jar after clicking (landed on "+
				"%s). The walk would then capture the public rows WITH a live session, and the only page "+
				"it looks at afterwards renders identically either way — which is why this is checked "+
				"rather than assumed", identity.SessionCookieName, landed)
		}
	}
	// The redirect target, as a second independent witness. A cleared cookie with no redirect would
	// mean the browser dropped it rather than the server clearing it.
	if u, err := url.Parse(landed); err != nil {
		return fmt.Errorf("the landed location %q after sign-out is not a URL: %w", landed, err)
	} else if u.Path != ui.SignInPath {
		return fmt.Errorf("sign-out cleared the cookie but landed on %q rather than %s; the handler "+
			"redirects there, so this is a different code path from the one the walk assumes",
			u.Path, ui.SignInPath)
	}
	return nil
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
		// The touch flag is `vp.Touch` and NOT `vp.Name == Mobile.Name`, which is what it
		// was: a behavioural property keyed on a STRING is a property a fourth width named
		// anything else silently loses. See [Viewport.Touch].
		emulation.SetDeviceMetricsOverride(int64(vp.Width), int64(vp.Height), 1, vp.Touch),
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

	// 🔴 AND A 2xx IS NOT ENOUGH, BECAUSE A REDIRECT LANDS ON ONE. This is the same class as the
	// status gate and the status gate structurally cannot see it: a `303` to another page leaves
	// the FINAL document at 200, so every check above passes while the bytes measured belong to a
	// different route. The capture would then be filed under this target's `PushURL`, and the hub
	// matches its P2 diff on that string — so one page's axe violations would be attributed to
	// another page forever, with nothing anywhere reporting an error.
	//
	// 🔴 NOT HYPOTHETICAL: the auth change makes `GET /` answer `303 /sign-in` for an
	// `Accept: text/html` request without a session. A walk whose session dropped mid-run would
	// capture the sign-in page under `PushURL: "/"` and call it clean.
	//
	// ⚠ THE COMPARISON IS ON THE PATH ONLY, AND THE NARROWING IS DELIBERATE. A server may
	// legitimately normalise or reorder a query string, so comparing the whole URL would refuse
	// correct responses — a guard that fires on the honest tree gets deleted. A redirect that
	// keeps the path and drops the query is therefore NOT caught here; it is a lesser fault
	// (same route, different arguments) and `README.md` names it as the remaining edge.
	var landed string
	if err := chromedp.Run(b.ctx, chromedp.Location(&landed)); err != nil {
		return nil, fmt.Errorf("reading the landed location for %s: %w", t.Path, err)
	}
	c.LandedURL = landed
	wantPath, _, _ := strings.Cut(t.Path, "?")
	if lu, err := url.Parse(landed); err != nil {
		return nil, fmt.Errorf("the landed location %q for %s is not a URL: %w", landed, t.Path, err)
	} else if lu.Path != wantPath {
		return nil, fmt.Errorf("%s at %s was REDIRECTED to %s (path %q, wanted %q): the document answered %d, "+
			"so the status gate passed — but the bytes captured belong to a different route and would be "+
			"filed under this target's push identity forever",
			t.Path, vp.Name, landed, lu.Path, wantPath, status)
	}

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

	// `document.scripts.length`, read off the live DOM. See [Capture.ScriptCount] for why
	// this is a browser question and not a grep, and `refuseWalkRegressions` for where it
	// is turned into a refusal.
	if err := chromedp.Run(b.ctx, chromedp.Evaluate(`document.scripts.length`, &c.ScriptCount)); err != nil {
		return nil, fmt.Errorf("counting scripts on %s at %s: %w", t.Path, vp.Name, err)
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

	// 🔴 TWO ASSERTIONS, AND THE FIRST DRAFT OF THEM WAS WRONG IN A WAY THIS HARNESS'S OWN POSITIVE
	// CONTROL CAUGHT. What they close:
	//
	//  1. `layout-smells.js` wraps its whole body in `try/catch` and its catch-all returns `'{}'` — by
	//     design, so a hostile page cannot drop the shared capture. But `{}` unmarshals to a ZERO
	//     `PushLayout`, and `MissingViewportMeta: false` is not an absence: it is the positive claim
	//     "this page HAS a viewport meta tag". So a thrown script was reported as a clean layout line
	//     and the walk printed `no-viewport-meta=false` having measured nothing. `InnerWidth == 0` is
	//     the tell, because no rendered page is zero pixels wide.
	//  2. The emulated WIDTH was asserted by nothing. [Viewport]'s doc explains why 390 and 1440 are
	//     the consumer's numbers rather than a preference here, and `SetDeviceMetricsOverride` was
	//     trusted to have applied them.
	//
	// 🔴 AND THE WIDTH ASSERTION IS CONDITIONAL, WHICH IS THE CORRECTION. A first draft asserted
	// `InnerWidth == vp.Width` unconditionally and FAILED on this module's own control page — measured
	// `innerWidth=1560` against a 390 viewport. That is not a broken emulation: the control page has
	// no `<meta name=viewport>` and 1800px of content, and a mobile browser given a page that never
	// opted into device-width sizing expands the LAYOUT viewport to fit it. Reporting a wider
	// `innerWidth` there is correct behaviour — and it is precisely the condition
	// `missing_viewport_meta` exists to report.
	//
	// So the two claims are separated. `InnerWidth > 0` holds on every page and is what catches the
	// catch-all. `InnerWidth == vp.Width` holds only where the page OPTED IN, so it is asserted only
	// when the same capture says a viewport meta is present — which makes the two signals corroborate
	// each other instead of one silently excusing the other.
	//
	// ⚠ `chromedp.Flag("hide-scrollbars", false)` in [NewBrowser] is the one setting that could move
	// this number, which is a second reason to assert rather than reason: if hiding scrollbars ever
	// changes `innerWidth`, this fails loudly instead of shifting every layout measurement by a
	// scrollbar's width.
	if layout.InnerWidth <= 0 {
		return nil, fmt.Errorf("%s at %s reports innerWidth=%d, which no rendered page can be. "+
			"`layout-smells.js` almost certainly hit its catch-all and returned `{}` — a ZERO block whose "+
			"`missing_viewport_meta:false` is an AFFIRMATIVE claim this harness would otherwise have "+
			"printed as a clean layout line (raw: %q)",
			t.Path, vp.Name, layout.InnerWidth, truncateForLog(layoutRaw, 200))
	}
	if !layout.MissingViewportMeta && layout.InnerWidth != vp.Width {
		return nil, fmt.Errorf("%s at %s declares a <meta viewport> yet reports innerWidth=%d while the "+
			"viewport was set to %d. A page that opted into device-width sizing must see the emulated "+
			"width, so either the emulation did not apply or the page overrides it (raw: %q)",
			t.Path, vp.Name, layout.InnerWidth, vp.Width, truncateForLog(layoutRaw, 200))
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
	// actually holds on THIS page under THIS CSP is MEASURED ON EVERY RUN rather than assumed
	// from the specification: this injection is followed immediately by `axe.run`, and the
	// result must decode with a `testEngine` — which `TestTheHermeticSurfaceIsWhereTheZEROSCOMEFROM`
	// asserts. A blocked injection cannot produce that.
	//
	// ⚠ THE EVIDENCE USED TO BE A THROWAWAY SPIKE PROGRAM, WHICH WAS DELETED AND IS NOT COMING
	// BACK. A separate binary nothing ran had already rotted inside its own change; the walk
	// re-measures the same claim on every push, which is strictly stronger. `README.md` records
	// the spike's original numbers as evidence-at-the-time.
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

// truncateForLog bounds a page-supplied string before it reaches an error message.
//
// 🔴 THE SAME CLASS AS `push.go`'s `maxDiagnosticBody`: a value whose SIZE is chosen by whoever is
// answering. `layout-smells.js` builds its return value in the page, so a hostile page could make it
// arbitrarily long — and this harness's stdout is `tee`d to a file published as an artifact.
func truncateForLog(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + fmt.Sprintf("… (%d more byte(s) elided)", len(s)-n)
}
