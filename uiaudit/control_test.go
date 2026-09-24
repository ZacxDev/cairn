package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/ZacxDev/cairn/internal/identity"
	"github.com/ZacxDev/cairn/internal/ui"
	"github.com/chromedp/cdproto/network"
)

// 🔴 THIS FILE IS THE POSITIVE CONTROL, AND WITHOUT IT EVERY ZERO THIS HARNESS REPORTS IS
// INDISTINGUISHABLE FROM AN INSTRUMENT WIRED TO NOTHING.
//
// The walk over `cairn-ui` reports small numbers, and some are zero BY CONSTRUCTION rather than
// by passing: the page ships no script and `internal/ui`'s own XSS guard asserts `"<img"` can
// never render, so the console collector cannot count there whatever the code does. Reporting
// that zero alone would be a claim about nothing.
//
// ⚠ THE NETWORK ZERO IS STRUCTURAL ONLY WHILE THE STYLESHEET IS INLINE. The auth change gives it
// its own route, which makes it a real blocking subresource — so that zero stops being about the
// page's shape and starts meaning "every subresource succeeded". The assertion below reads the
// LEDGER rather than assuming, because the version that assumed failed on the merged tree and it
// was the assertion that was stale, not the walk.
//
// So this test serves a page built to make EVERY collector non-zero and watches each number
// move. What gets reported is the PAIR — "N on the control, M under test" — never the M
// alone.
//
// 🔴 AND IT REFUSES RATHER THAN SKIPS WHEN CHROMIUM IS ABSENT. A skip nobody counts is a
// pass, and this is the one test in the module whose absence would make the whole harness
// unfalsifiable. The CI job installs chromium; a local run without one is supposed to be
// loud.

// controlPage is the deliberately-bad page. Every element in it exists to make one specific
// collector produce a number, and the comment beside each says which.
//
// ⚠ IT IS NOT A TEXTBOOK FIXTURE. A scanner's own canonical example is the case it is most
// likely to special-case, so the markup below is ordinary-looking markup with ordinary
// defects: a real image with no alt and no dimensions, a real button that is 10px tall
// because its padding is zero, real 9px text, a real wide table, a real 404 subresource.
const controlPage = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>control</title>
<style>
 body { margin: 0; font-family: system-ui, sans-serif; }
 /* 9px, which is under the 12px small-text threshold. */
 .fine-print { font-size: 9px; }
 /* A control 10px tall, which is under the 44px tap-target threshold. */
 button.compact { height: 10px; width: 10px; padding: 0; border: 0; font-size: 6px; }
 /* 1800px of content inside a 390px viewport: horizontal overflow on mobile. */
 table.wide { width: 1800px; }
 /* Grey on white: an axe color-contrast violation with real-looking colours. */
 .muted { color: #b9b9b9; background: #ffffff; }
</style>
</head>
<body>
<!-- No <meta name=viewport>: the missing_viewport_meta flag. -->
<!-- An <img> with no alt (axe image-alt) and no width/height (images_no_dims). -->
<img src="/missing.png">
<p class="muted">Quarterly figures are provisional.</p>
<p class="fine-print">Terms apply.</p>
<button class="compact">x</button>
<table class="wide"><tr><td>one</td><td>two</td></tr></table>
<script>
 // A first-party console error and a first-party uncaught exception.
 console.error("control: a first-party console error");
 fetch("/missing.json").catch(() => {});
 setTimeout(() => { throw new Error("control: a first-party uncaught exception"); }, 10);
</script>
</body>
</html>`

func chromiumOrRefuse(t *testing.T) {
	t.Helper()
	for _, name := range []string{"chromium", "chromium-browser", "google-chrome-stable", "google-chrome", "chrome"} {
		if _, err := exec.LookPath(name); err == nil {
			return
		}
	}
	if p := os.Getenv("CHROMEDP_EXEC"); p != "" {
		if _, err := os.Stat(p); err == nil {
			return
		}
	}
	// 🔴 FATAL, NOT SKIP. See the file comment: this is the module's only instrument
	// validation, and a skipped instrument validation is a green that means nothing.
	t.Fatal("no chromium on PATH: this is the harness's only positive control, so it REFUSES rather than skipping. " +
		"Install chromium, or run this module's tests only where one exists (the CI `uiaudit` job installs it).")
}

func TestThePositiveControlMakesEverySignalNonZero(t *testing.T) {
	chromiumOrRefuse(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			// The 404 that gives the network collector a first-party number to count.
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		// 🔴 NO CSP ON THE CONTROL, DELIBERATELY. The control's job is to prove the
		// COLLECTORS can count; `spike/main.go` is what proves the axe injection survives
		// the real surface's `default-src 'none'`. Conflating the two would make a CSP
		// change here look like a collector failure.
		_, _ = w.Write([]byte(controlPage))
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	b, err := NewBrowser(ctx, srv.URL, 90*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()

	c, err := b.CaptureTarget(Target{Path: "/", PushURL: "/control", LedgerRow: "control"}, Mobile)
	if err != nil {
		t.Fatal(err)
	}

	// Each assertion names the collector and the reason a zero here would mean the collector
	// is wired to nothing rather than that the page is clean.
	type check struct {
		name string
		got  int
		why  string
	}
	rules := map[string]bool{}
	for _, v := range c.Violations {
		rules[v.ID] = true
	}
	checks := []check{
		{"axe violations", len(c.Violations), "the page has grey-on-white text and an <img> with no alt"},
		{"layout: tap targets under 44px", c.Layout.SmallTapTargets, "the page has a 10x10 button"},
		{"layout: text under 12px", c.Layout.SmallText, "the page has 9px text"},
		{"layout: images with no dimensions", c.Layout.ImagesNoDims, "the page has an <img> with no width or height"},
		{"console events", len(c.Console), "the page calls console.error and throws"},
		{"network events", len(c.Network), "the page fetches two paths that 404"},
		{"a11y digest entries", digestEntries(c), "the page has a button, an image and a table"},
	}
	for _, ck := range checks {
		if ck.got == 0 {
			t.Errorf("POSITIVE CONTROL FAILED: %s counted 0, and %s. "+
				"A zero here means the collector observes nothing, so every zero it reports on the real surface is unreadable.",
				ck.name, ck.why)
			continue
		}
		t.Logf("positive control: %s = %d", ck.name, ck.got)
	}

	// Two flags rather than counts, asserted separately because a bool cannot be "moved".
	if !c.Layout.HorizontalOverflow {
		t.Errorf("POSITIVE CONTROL FAILED: horizontal_overflow is false on a page with 1800px of table inside a %dpx viewport", Mobile.Width)
	}
	if !c.Layout.MissingViewportMeta {
		t.Error("POSITIVE CONTROL FAILED: missing_viewport_meta is false on a page with no <meta name=viewport>")
	}

	// The two axe rules the markup was built to trip, named so a green is attributable.
	for _, want := range []string{"image-alt", "color-contrast"} {
		if !rules[want] {
			t.Errorf("POSITIVE CONTROL: axe ran but did not report %q; rules seen: %v — "+
				"axe may be executing while its rule set is not what this control assumes", want, keys(rules))
		}
	}

	if len(c.Screenshot) == 0 {
		t.Error("POSITIVE CONTROL FAILED: the screenshot is empty, so the pixel leg captures nothing")
	}
	// A PNG magic check, because "non-empty" is satisfied by an error page's bytes.
	if len(c.Screenshot) >= 8 && string(c.Screenshot[1:4]) != "PNG" {
		t.Errorf("the screenshot is not a PNG: first bytes %q", c.Screenshot[:8])
	}
	t.Logf("positive control: screenshot = %d bytes", len(c.Screenshot))

	// 🔴 AND THE CONTROL'S OWN CAPTURE MUST BUILD A PUSHABLE PAYLOAD. A control that proved
	// the collectors count while producing a payload the server would reject would validate
	// half the instrument.
	p, files, err := BuildPayload("uiaudit-control", []*Capture{c})
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Validate(files); err != nil {
		t.Fatalf("the control's own capture does not build a valid payload: %v", err)
	}
}

// TestTheHermeticSurfaceIsWhereTheZEROSCOMEFROM is the other half of the pair.
//
// It boots the real `cairn-ui` and asserts that the console and network collectors report
// ZERO — not because zero is good, but because the pair "non-zero on the control, zero here"
// is what makes the zero a measurement instead of a shrug. If this ever goes non-zero, the
// page has grown a script or a subresource and the structural claim in `doc.go` is stale.
func TestTheHermeticSurfaceIsWhereTheZEROSCOMEFROM(t *testing.T) {
	chromiumOrRefuse(t)
	bin := os.Getenv("UIAUDIT_CAIRN_UI")
	if bin == "" {
		t.Fatal("UIAUDIT_CAIRN_UI must name a built cairn-ui: this test is the other half of the " +
			"positive-control pair, and skipping it leaves the control unpaired")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	world, err := BootWorld(ctx, os.Getenv("UIAUDIT_REPO_ROOT"), bin, t.TempDir(), 18779)
	if err != nil {
		t.Fatal(err)
	}
	defer world.Stop()

	b, err := NewBrowser(ctx, world.BaseURL, 2*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	if _, err := b.SignIn(fixtureToken); err != nil {
		t.Fatalf("%v\n--- cairn-ui log ---\n%s", err, world.Log())
	}
	c, err := b.CaptureTarget(Target{Path: "/", PushURL: "/", LedgerRow: "GET / content"}, Mobile)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Console) != 0 {
		t.Errorf("the hermetic surface produced %d console event(s): %v — the page has grown a script, "+
			"and `doc.go`'s structural-zero claim is now false", len(c.Console), c.Console)
	}

	// 🔴 THE NETWORK ASSERTION IS DERIVED FROM THE LEDGER, BECAUSE THE STRUCTURAL ZERO IS A
	// PROPERTY OF THE SURFACE AND THE SURFACE IS CHANGING.
	//
	// The claim "zero by construction" rested on the page having no subresources — an inline
	// stylesheet and no script. The auth change moves the stylesheet to its own ROUTE, which makes
	// it a real blocking subresource, and the zero stops being structural. Measured on the merged
	// tree: this assertion failed there, and it was the ASSERTION that was stale, not the walk.
	//
	// So the expectation is read off the ledger rather than written down. On a ledger with no
	// stylesheet row the zero must hold; on one that has it, a page that loaded NOTHING would mean
	// the browser never fetched the stylesheet — which is a finding in the other direction, and
	// the only reason this is not simply relaxed to "don't care".
	hasStylesheet := hasRow(ui.DeclaredRouteLedger(), "GET "+StylesheetPath)
	failures := 0
	for _, e := range c.Network {
		// Only ERRORS are recorded, so any entry here is a failed request. A successful
		// stylesheet fetch produces no event at all.
		failures++
		t.Logf("network event: %+v", e)
	}
	if failures != 0 {
		t.Errorf("the hermetic surface produced %d FAILED network request(s) — every subresource it asks "+
			"for must succeed, whether or not the stylesheet route exists", failures)
	}
	if hasStylesheet {
		t.Logf("this ledger declares %s, so the console zero above remains structural while the network "+
			"one does NOT: the page now has a real blocking subresource. `doc.go` and `README.md` scope "+
			"the structural claim to console for exactly this reason.", StylesheetPath)
	} else {
		t.Logf("this ledger declares no %s row, so BOTH zeros are structural: the page has no scripts and "+
			"no subresources at all", StylesheetPath)
	}
	// A floor, not an assertion about the count: a walk that rendered the sign-in page
	// instead would have a different digest and this is the cheapest way to notice.
	if !strings.Contains(string(c.AxeJSON), `"testEngine"`) {
		t.Error("the axe result carries no testEngine: axe did not run, and the a11y half of this harness is inert")
	}
	t.Logf("hermetic surface: console=0 network=0 (structural), axe violations=%d, digest entries=%d",
		len(c.Violations), digestEntries(c))
}

// TestAPageThatANSWEREDAnErrorIsREFUSEDRatherThanMeasured is the REGRESSION guard for the
// defect this harness's first draft shipped.
//
// 🔴 RED ON THE FIRST DRAFT, GREEN AT HEAD, AND THE SYMPTOM IS EXACTLY WHAT THIS SERVES. That
// draft expanded the share row into `/share?scope=<name>`; the parameter is a `control.ID`,
// so every target 404'd. The walk measured the error page: axe reported five violations on it
// (`document-title`, `html-has-lang`, `landmark-one-main`, `page-has-heading-one`, `region`),
// the layout script returned real numbers including `missing_viewport_meta=true`, the
// screenshot was a valid PNG, and the run exited 0 reporting "62 axe violations across 6
// rules". Every collector worked. NONE of them could tell it was the wrong document.
//
// So this test serves a 404 whose body is a plausible page and requires the capture to
// REFUSE. The body matters: a refusal that only fired on an empty response would pass here
// and fail on a real error page, which is the shape a textbook fixture hides.
func TestAPageThatANSWEREDAnErrorIsREFUSEDRatherThanMeasured(t *testing.T) {
	chromiumOrRefuse(t)

	for _, tc := range []struct {
		name   string
		status int
	}{
		{"404, which is what a guessed scope id answers", http.StatusNotFound},
		{"401, which is what every unledgered path answers", http.StatusUnauthorized},
		{"500, so the gate is not a 4xx-shaped guard", http.StatusInternalServerError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				w.WriteHeader(tc.status)
				// A plausible page, not an empty body: this is the case the real defect had.
				_, _ = w.Write([]byte(`<!doctype html><html><head></head><body><p>no such scope, or it is not yours to share</p></body></html>`))
			}))
			defer srv.Close()

			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			b, err := NewBrowser(ctx, srv.URL, 60*time.Second)
			if err != nil {
				t.Fatal(err)
			}
			defer b.Close()

			_, err = b.CaptureTarget(Target{Path: "/", PushURL: "/", LedgerRow: "control"}, Mobile)
			if err == nil {
				t.Fatalf("a document answering %d was captured and measured: every signal in this "+
					"harness would describe the error page as if it were the surface", tc.status)
			}
			// 🔴 IT MUST FAIL FOR THIS GUARD'S OWN REASON. A capture that errored because
			// axe would not inject, or because the screenshot failed, would be green for the
			// wrong reason and would stay green with the status check deleted.
			if !strings.Contains(err.Error(), "the page captured is not the page requested") {
				t.Fatalf("the refusal came from somewhere other than the document-status gate: %v", err)
			}
			if !strings.Contains(err.Error(), "answered "+itoa(tc.status)) {
				t.Fatalf("the refusal must name the status it saw; got %v", err)
			}
		})
	}
}

// TestAHealthyDocumentIsNOTRefused is the positive control on the gate above: a guard that
// refused everything would pass every case in that test while making the harness useless.
func TestAHealthyDocumentIsNOTRefused(t *testing.T) {
	chromiumOrRefuse(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<!doctype html><html lang="en"><head><title>ok</title></head><body><main><h1>ok</h1></main></body></html>`))
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	b, err := NewBrowser(ctx, srv.URL, 60*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	c, err := b.CaptureTarget(Target{Path: "/", PushURL: "/", LedgerRow: "control"}, Mobile)
	if err != nil {
		t.Fatalf("a 200 document must be captured: %v", err)
	}
	if c.DocStatus != 200 {
		t.Fatalf("DocStatus = %d, want 200 — the gate is reading the wrong response", c.DocStatus)
	}
}

// TestAPageThatREDIRECTEDIsREFUSEDEvenThoughItAnswered200 closes the hazard the status gate
// structurally cannot see.
//
// 🔴 A REDIRECT LANDS ON A 2xx, SO EVERY OTHER CHECK IN THIS HARNESS PASSES ON IT. The bytes
// measured then belong to a different route and get filed under this target's push identity —
// and the hub matches its P2 diff on that string, so one page's violations would be attributed
// to another forever with nothing reporting an error. Not hypothetical: the auth change makes
// `GET /` answer `303 /sign-in` for an `Accept: text/html` request without a session, so a walk
// whose session dropped mid-run would capture the sign-in page and call it `/`.
//
// The 303 case is the real one; 302 and 307 are driven too so the guard is not tied to one
// status, and the SAME-PATH case is the positive control — a guard that refused every navigation
// would satisfy the first three and make the harness useless.
func TestAPageThatREDIRECTEDIsREFUSEDEvenThoughItAnswered200(t *testing.T) {
	chromiumOrRefuse(t)

	const page = `<!doctype html><html lang="en"><head><title>ok</title></head><body><main><h1>ok</h1></main></body></html>`
	for _, tc := range []struct {
		name    string
		status  int
		wantErr bool
	}{
		{"303, which is what the auth change answers on / without a session", http.StatusSeeOther, true},
		{"302, so the guard is not tied to one status", http.StatusFound, true},
		{"307, which preserves the method and still moves the document", http.StatusTemporaryRedirect, true},
		{"no redirect at all — the POSITIVE CONTROL: a guard that refused everything would pass the three above", 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/" && tc.status != 0 {
					http.Redirect(w, r, "/sign-in", tc.status)
					return
				}
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				_, _ = w.Write([]byte(page))
			}))
			defer srv.Close()

			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			b, err := NewBrowser(ctx, srv.URL, 60*time.Second)
			if err != nil {
				t.Fatal(err)
			}
			defer b.Close()

			c, err := b.CaptureTarget(Target{Path: "/", PushURL: "/", LedgerRow: "GET / content"}, Mobile)
			if !tc.wantErr {
				if err != nil {
					t.Fatalf("a page that did NOT redirect must be captured: %v", err)
				}
				if c.DocStatus != 200 {
					t.Fatalf("DocStatus = %d, want 200", c.DocStatus)
				}
				return
			}
			if err == nil {
				t.Fatalf("a %d redirect was captured: the final document answered 200, so the status gate "+
					"passed, and the sign-in page's bytes would be filed under PushURL \"/\" forever", tc.status)
			}
			// 🔴 IT MUST FAIL FOR THE REDIRECT GUARD'S OWN REASON. A failure from the status
			// gate, or from axe declining to inject, would be green for the wrong reason and
			// would stay green with the redirect guard deleted.
			if !strings.Contains(err.Error(), "was REDIRECTED to") {
				t.Fatalf("the refusal came from somewhere other than the redirect guard: %v", err)
			}
			if !strings.Contains(err.Error(), `wanted "/"`) {
				t.Fatalf("the refusal must name the path it wanted; got %v", err)
			}
		})
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

// TestTheSessionCookiesFourFlagsAreHONOUREDByTheBrowser turns justification 1 from an assertion
// into a pinned, re-checkable property.
//
// 🔴 EVERY EXISTING GATE IN THIS REPOSITORY CAN ONLY READ THE HEADER THE SERVER SENT. `internal/
// identity` chooses `__Host-`, `Secure`, `HttpOnly`, `SameSite=Lax`, `Path=/` and NO `Domain`, and
// a unit test can assert the `http.Cookie` struct it built — but a struct field is not a browser's
// behaviour. `session.go`'s own comment says the `Secure`-over-plaintext-loopback half is "a claim
// about browsers and no test here has measured it", and the `Domain` absence is load-bearing
// because a conforming browser REFUSES a `__Host-` cookie that carries one. This is the only test
// in the repository that reads those six facts back out of a real jar, after a real navigation,
// over a real plaintext loopback origin.
//
// ⚠ WHAT IT CANNOT DRIVE, STATED RATHER THAN IMPLIED. Three cookie claims matter; this pins the
// two that need no third party:
//
//  1. ✅ the `__Host-` prefixed `Secure` cookie is STORED on a plaintext loopback origin — here.
//  2. ✅ its four flags survive the round trip as the browser recorded them — here.
//  3. ❌ that it is ATTACHED to a cross-site-initiated callback while `SameSite=Strict` would
//     withhold it. That needs a real provider redirecting from a DIFFERENT origin, which a
//     hermetic walk has no way to stage. It is measured in an audit transcript and is NOT pinned
//     by anything in this repository; `README.md` names it in the blind set. Do not read this
//     test as covering it.
func TestTheSessionCookiesFourFlagsAreHONOUREDByTheBrowser(t *testing.T) {
	chromiumOrRefuse(t)
	bin := os.Getenv("UIAUDIT_CAIRN_UI")
	if bin == "" {
		t.Fatal("UIAUDIT_CAIRN_UI must name a built cairn-ui: this test is the only pinned record of " +
			"the cookie attributes a BROWSER honours, so skipping it silently removes that record")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()
	world, err := BootWorld(ctx, os.Getenv("UIAUDIT_REPO_ROOT"), bin, t.TempDir(), 18781)
	if err != nil {
		t.Fatal(err)
	}
	defer world.Stop()

	// 🔴 TWO ORIGINS, BECAUSE ONE MEASUREMENT IS NOT A GENERAL CLAIM AND THE DIMENSION HERE IS THE
	// ORIGIN HOST. `session.go`'s comment names `localhost`; the pod binds `127.0.0.1`. Chromium's
	// potentially-trustworthy-origin rule is stated per host, so a `Secure` cookie being stored is
	// a claim about the host it was measured on. The pod binds loopback once and the BROWSER is
	// pointed at each name in turn — which is the honest split, since the server cannot tell them
	// apart and the browser can.
	//
	// ⚠ THIS REPLACED A FLAG. An earlier draft of the harness carried a second program that took a
	// `-base` argument so an operator could re-measure at `localhost`. It rotted inside its own PR
	// and was deleted; a flag whose only caller is a human who remembers to pass it is a path with
	// no caller. A subtest is a caller.
	for _, origin := range []struct{ name, base string }{
		{"127.0.0.1 — what the pod binds", world.BaseURL},
		{"localhost — what session.go's comment names", strings.Replace(world.BaseURL, "127.0.0.1", "localhost", 1)},
	} {
		t.Run(origin.name, func(t *testing.T) {
			assertCookieHonoured(t, ctx, world, origin.base)
		})
	}
}

// assertCookieHonoured is the per-origin body. `wantDomain` is derived from the base rather than
// passed, because a host-only cookie's jar domain IS the origin's host and deriving it removes the
// chance of asserting one origin's value against another's.
func assertCookieHonoured(t *testing.T, ctx context.Context, world *World, base string) {
	t.Helper()
	u, err := url.Parse(base)
	if err != nil {
		t.Fatal(err)
	}
	wantDomain := u.Hostname()

	b, err := NewBrowser(ctx, base, 2*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	got, err := b.SignIn(fixtureToken)
	if err != nil {
		t.Fatalf("%v\n--- cairn-ui log ---\n%s", err, world.Log())
	}

	// 🔴 READ BACK FROM THE JAR, FIELD BY FIELD, EACH WITH THE ATTACK IT REFUSES. A single
	// struct-equality assertion would report "the cookie is wrong" and make a reader diff two
	// printouts; naming each one means a failure says which property was lost.
	if got.Name != identity.SessionCookieName {
		t.Fatalf("the browser stored %q, not %q — every assertion below would be about the wrong cookie",
			got.Name, identity.SessionCookieName)
	}
	if !strings.HasPrefix(got.Name, "__Host-") {
		t.Errorf("the stored name %q lacks the __Host- prefix: a conforming browser then permits a "+
			"sibling subdomain to set it, which is the attack the prefix refuses", got.Name)
	}
	if !got.Secure {
		t.Error("the browser did NOT record Secure. This is the half `internal/identity/session.go` " +
			"flags as unmeasured: `Secure` is unconditional there, and a browser that dropped the flag " +
			"over plaintext loopback would send the session id to every network hop")
	}
	if !got.HTTPOnly {
		t.Error("the browser did NOT record HttpOnly: the session id becomes readable from " +
			"document.cookie, which also breaks CSRFTokenFor's soundness — a token derived from a " +
			"value script can read is derivable by the attacker")
	}
	if got.SameSite != network.CookieSameSiteLax {
		t.Errorf("the browser recorded SameSite=%q, want Lax. Strict would break an ordinary link into "+
			"the surface; None would attach it to a cross-site POST, which is the CSRF vector", got.SameSite)
	}
	if got.Path != "/" {
		t.Errorf("the browser recorded Path=%q, want \"/\": a __Host- cookie with any other path is "+
			"refused outright by a conforming browser", got.Path)
	}
	// 🔴 THE `Domain` ABSENCE IS THE SUBTLE ONE, AND CDP REPORTS IT AS THE HOST. A `__Host-` cookie
	// that carries a `Domain` attribute is refused outright — so the thing to assert is that the
	// jar's domain is exactly the origin's host with NO leading dot, which is how a host-only
	// cookie appears. A leading dot would mean the attribute was sent and honoured.
	if strings.HasPrefix(got.Domain, ".") {
		t.Errorf("the browser recorded Domain=%q — a leading dot means a domain-scoped cookie, and a "+
			"__Host- cookie carrying Domain must be refused outright", got.Domain)
	}
	if got.Domain != wantDomain {
		t.Errorf("the browser recorded Domain=%q, want the origin host %q exactly (host-only)", got.Domain, wantDomain)
	}

	t.Logf("HONOURED BY %s: name=%s secure=%v httpOnly=%v sameSite=%v path=%q domain=%q (host-only)",
		"chromium", got.Name, got.Secure, got.HTTPOnly, got.SameSite, got.Path, got.Domain)

	// And the load-bearing half of justification 1: stored is not the same as RE-SENT. A browser
	// that parsed Set-Cookie and then dropped the cookie for failing the `Secure` requirement looks
	// identical at the header and differs only in what it sends back.
	c, err := b.CaptureTarget(Target{Path: ui.RootPath, PushURL: ui.RootPath, LedgerRow: "GET / content"}, Desktop)
	if err != nil {
		t.Fatalf("re-navigating with the session cookie: %v\n--- cairn-ui log ---\n%s", err, world.Log())
	}
	if c.DocStatus != 200 {
		t.Fatalf("%s answered %d on a second navigation: the cookie was stored but not RE-SENT",
			ui.RootPath, c.DocStatus)
	}
	t.Logf("and RE-SENT: a second navigation to %s answered %d", ui.RootPath, c.DocStatus)
}

func digestEntries(c *Capture) int {
	if !c.HasDigest() {
		return 0
	}
	var d struct {
		Interactive  []any `json:"interactive"`
		FormControls []any `json:"form_controls"`
		Landmarks    []any `json:"landmarks"`
	}
	if err := json.Unmarshal(c.DigestJSON, &d); err != nil {
		return 0
	}
	return len(d.Interactive) + len(d.FormControls) + len(d.Landmarks)
}

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
