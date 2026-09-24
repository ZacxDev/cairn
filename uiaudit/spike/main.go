// Command spike measures the two claims the harness design rests on, each of which is
// asserted somewhere in this repository and measured nowhere.
//
// It is throwaway in intent and kept in the tree because a spike nobody can re-run is a
// claim again. Run it against a live `cairn-ui`:
//
//	go run ./spike -base http://127.0.0.1:18771 -token <token>
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
)

func main() {
	base := flag.String("base", "http://127.0.0.1:18771", "origin to drive")
	token := flag.String("token", "", "a credential the pod accepts")
	axePath := flag.String("axe", "", "path to axe.min.js")
	flag.Parse()

	allocOpts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", "new"),
		chromedp.Flag("no-sandbox", true),
		chromedp.Flag("disable-dev-shm-usage", true),
	)
	allocCtx, cancelAlloc := chromedp.NewExecAllocator(context.Background(), allocOpts...)
	defer cancelAlloc()
	ctx, cancel := chromedp.NewContext(allocCtx)
	defer cancel()
	ctx, cancelT := context.WithTimeout(ctx, 90*time.Second)
	defer cancelT()

	var version string
	if err := chromedp.Run(ctx, chromedp.Evaluate(`navigator.userAgent`, &version)); err != nil {
		fmt.Fprintf(os.Stderr, "could not start chromium: %v\n", err)
		os.Exit(2)
	}
	fmt.Printf("chromium user-agent: %s\n", version)

	rc := 0
	if err := spike1(ctx, *base, *token); err != nil {
		fmt.Printf("SPIKE 1 FAIL: %v\n", err)
		rc = 1
	}
	if err := spike2(ctx, *base, *axePath); err != nil {
		fmt.Printf("SPIKE 2 FAIL: %v\n", err)
		rc = 1
	}
	os.Exit(rc)
}

// spike1 asks whether a real Chromium STORES and RE-SENDS a `__Host-`-prefixed `Secure`
// cookie set over a plaintext loopback origin. `internal/identity/session.go` asserts it
// and flags the assertion unmeasured.
//
// The discriminator is deliberately the SECOND request rather than the Set-Cookie header:
// a browser that parsed the header and then dropped the cookie for failing the `Secure`
// requirement looks identical at the header, and differs only in what it sends back.
func spike1(ctx context.Context, base, token string) error {
	fmt.Printf("\n== SPIKE 1: __Host- Secure cookie over %s ==\n", base)

	var status int
	var location string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/sign-in"),
		chromedp.WaitVisible(`#token`, chromedp.ByID),
		chromedp.SendKeys(`#token`, token, chromedp.ByID),
		chromedp.Click(`form button[type=submit], form input[type=submit]`, chromedp.ByQuery),
		chromedp.Sleep(750*time.Millisecond),
		chromedp.Location(&location),
	); err != nil {
		return fmt.Errorf("driving the sign-in form: %w", err)
	}
	_ = status
	fmt.Printf("  after submit, location = %s\n", location)

	var cookies []*network.Cookie
	if err := chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		got, err := network.GetCookies().Do(ctx)
		cookies = got
		return err
	})); err != nil {
		return fmt.Errorf("reading the cookie jar: %w", err)
	}
	found := false
	for _, c := range cookies {
		fmt.Printf("  jar: name=%q secure=%v httpOnly=%v sameSite=%v path=%q domain=%q\n",
			c.Name, c.Secure, c.HTTPOnly, c.SameSite, c.Path, c.Domain)
		if strings.HasPrefix(c.Name, "__Host-") {
			found = true
		}
	}
	if !found {
		return fmt.Errorf("no __Host- cookie in the jar after sign-in (jar holds %d cookie(s))", len(cookies))
	}

	// The load-bearing half: navigate again and see whether the surface treats us as
	// signed in. A cookie in the jar that the browser refuses to ATTACH is the failure
	// mode this exists to rule out.
	var body string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/"),
		chromedp.Sleep(400*time.Millisecond),
		chromedp.Location(&location),
		chromedp.OuterHTML(`html`, &body, chromedp.ByQuery),
	); err != nil {
		return fmt.Errorf("re-navigating: %w", err)
	}
	fmt.Printf("  second navigation: location=%s, signed-out form present=%v\n",
		location, strings.Contains(body, `id="token"`))
	if strings.Contains(body, `id="token"`) {
		return fmt.Errorf("the second navigation rendered the sign-in form: the cookie was not re-sent")
	}
	fmt.Printf("  SPIKE 1 PASS: the cookie was stored AND re-sent; %s/ renders authenticated content\n", base)
	return nil
}

// spike2 asks whether axe-core injected over CDP executes on a page whose CSP names no
// `script-src` at all and therefore falls back to `default-src 'none'`.
//
// `Runtime.evaluate` is debugger-privileged and is documented to bypass CSP; that is a
// claim about a specification, and the page under test is the strictest case a
// specification argument can be wrong about.
func spike2(ctx context.Context, base, axePath string) error {
	fmt.Printf("\n== SPIKE 2: axe-core over CDP against default-src 'none' ==\n")
	if axePath == "" {
		return fmt.Errorf("no -axe path given")
	}
	src, err := os.ReadFile(axePath)
	if err != nil {
		return err
	}

	var csp string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/"),
		chromedp.Sleep(300*time.Millisecond),
		// The CSP as the page actually carries it, read back rather than assumed.
		chromedp.Evaluate(`(async () => {
			const r = await fetch(location.href, {method: "GET"});
			return r.headers.get("content-security-policy") || "(none)";
		})()`, &csp, withAwait),
	); err != nil {
		// `fetch` is itself subject to `connect-src`, so a failure here is expected and
		// not the spike's answer. Fall through with the CSP unread.
		csp = "(unread: " + err.Error() + ")"
	}
	fmt.Printf("  page CSP (as the browser saw it): %s\n", csp)

	// Inject through Runtime.evaluate — the same channel `chromedp.Evaluate` uses.
	if err := chromedp.Run(ctx, chromedp.Evaluate(string(src), nil)); err != nil {
		return fmt.Errorf("injecting axe: %w", err)
	}
	var present bool
	if err := chromedp.Run(ctx, chromedp.Evaluate(`typeof window.axe === "object"`, &present)); err != nil {
		return err
	}
	fmt.Printf("  window.axe present after injection: %v\n", present)
	if !present {
		return fmt.Errorf("axe did not define itself: the injection was blocked")
	}

	var raw []byte
	if err := chromedp.Run(ctx, chromedp.Evaluate(
		`axe.run(document, {resultTypes: ["violations"]}).then(r => JSON.stringify({
			violations: r.violations.length,
			ids: r.violations.map(v => v.id),
			testEngine: r.testEngine.version,
		}))`, &raw, withAwait)); err != nil {
		return fmt.Errorf("axe.run: %w", err)
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		raw = []byte(s)
	}
	fmt.Printf("  axe.run returned: %s\n", raw)
	fmt.Printf("  SPIKE 2 PASS: axe executed and returned a result object under this CSP\n")
	return nil
}

func withAwait(p *runtime.EvaluateParams) *runtime.EvaluateParams { return p.WithAwaitPromise(true) }
