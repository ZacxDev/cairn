package main

import (
	"bytes"
	"context"
	"fmt"
	"image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ZacxDev/cairn/internal/ui"
	"github.com/chromedp/cdproto/page"
	cdpruntime "github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
)

// 🔴 CLAUSES (a) AND (b: name, icon) OF THE MOBILE PLAN'S CLOSING CONDITION, AND THIS IS THE ONE
// PLACE THEY ARE IMPLEMENTED. `uiaudit/pwa_check.sh` is an ORCHESTRATOR: it runs this test (and the
// walk, for clause (c)) and greps each subtest's own verdict — it never re-implements a clause. The
// message prefixes `pwa clause (a)`, `pwa clause (b) name`, `pwa clause (b) icon` are what its
// `--self-test` attributes a caught sabotage by, so they are a contract with that script.
//
// What these read is what CHROMIUM decided — `Page.getInstallabilityErrors` and `Page.getAppManifest`
// over a real `cairn-ui` booted on the synthetic world — never the server's JSON parsed by this Go.
// `internal/ui`'s tests pin the bytes; this pins that a browser accepts them.

// pwaBoot is one booted world and what chromium said about its sign-in page.
type pwaBoot struct {
	label, name, variant string // name == "" is the UNARMED control boot
	base                 string
	installErrors        []string
	manifestURL          string
	manifestErrors       []string
	manifest             *page.WebAppManifest
}

// The two armed boots the plan names, and the unarmed one that is clause (a)'s negative control:
// installability is opt-in per deployment, so a boot with no `-app-name` must say exactly
// `no-manifest` — which is also what proves the probe can say anything but `[]`.
var pwaBoots = []pwaBoot{
	{label: "alpha", name: "cairn (alpha)", variant: "amber"},
	{label: "beta", name: "cairn (beta)", variant: "teal"},
	{label: "unarmed"},
}

func bootAndProbe(ctx context.Context, t *testing.T, root, bin string, b pwaBoot) pwaBoot {
	t.Helper()
	port, err := aFreeLoopbackPort()
	if err != nil {
		t.Fatal(err)
	}
	var extra []string
	if b.name != "" {
		extra = []string{"-app-name", b.name, "-app-icon-variant", b.variant}
	}
	world, err := BootWorld(ctx, root, bin, t.TempDir(), port, extra...)
	if err != nil {
		t.Fatalf("booting the %s world: %v", b.label, err)
	}
	t.Cleanup(world.Stop)
	br, err := NewBrowser(ctx, world.BaseURL, 3*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(br.Close)
	b.base = world.BaseURL

	var inst []*page.InstallabilityError
	var merrs []*page.AppManifestError
	if err := chromedp.Run(br.ctx,
		chromedp.Navigate(world.BaseURL+ui.SignInPath),
		chromedp.WaitReady("body"),
		chromedp.ActionFunc(func(ctx context.Context) error {
			var err error
			inst, err = page.GetInstallabilityErrors().Do(ctx)
			return err
		}),
		chromedp.ActionFunc(func(ctx context.Context) error {
			var err error
			b.manifestURL, merrs, _, b.manifest, err = page.GetAppManifest().Do(ctx)
			return err
		}),
	); err != nil {
		t.Fatalf("probing the %s world: %v\n--- cairn-ui log ---\n%s", b.label, err, world.Log())
	}
	b.installErrors = []string{}
	for _, e := range inst {
		b.installErrors = append(b.installErrors, e.ErrorID)
	}
	for _, e := range merrs {
		b.manifestErrors = append(b.manifestErrors, fmt.Sprintf("%s (line %d, critical=%d)", e.Message, e.Line, e.Critical))
	}
	t.Logf("%-7s installability errors %v; manifest %q, %d parse error(s)", b.label, b.installErrors,
		b.manifestURL, len(b.manifestErrors))
	return b
}

// TestPWAClauses boots three worlds — two ARMED with different names and variants, one unarmed — and
// asks chromium about each one's SIGN-IN page, the page an install starts from.
func TestPWAClauses(t *testing.T) {
	chromiumOrRefuse(t)
	bin, root := os.Getenv("UIAUDIT_CAIRN_UI"), os.Getenv("UIAUDIT_REPO_ROOT")
	if bin == "" || root == "" {
		t.Fatal("UIAUDIT_CAIRN_UI and UIAUDIT_REPO_ROOT must name a built cairn-ui and its checkout: these " +
			"clauses boot three worlds, and a skip would be a green about nothing")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	var boots []pwaBoot
	for _, b := range pwaBoots {
		boots = append(boots, bootAndProbe(ctx, t, root, bin, b))
	}
	alpha, beta, unarmed := boots[0], boots[1], boots[2]

	t.Run("a_installability", func(t *testing.T) {
		// The CONTROL first: if the unarmed boot does not read exactly `no-manifest`, the probe is not
		// distinguishing anything and the `[]` below would be a fact about the instrument.
		if !slices.Equal(unarmed.installErrors, []string{"no-manifest"}) {
			t.Fatalf("pwa clause (a) CONTROL: the UNARMED boot's installability errors are %v, want exactly "+
				"[no-manifest] — the probe cannot be read as able to refuse, so the armed verdicts below vouch "+
				"for nothing", unarmed.installErrors)
		}
		for _, b := range []pwaBoot{alpha, beta} {
			if len(b.installErrors) != 0 {
				t.Errorf("pwa clause (a) installability: the %s boot (-app-name %q) is NOT installable on "+
					"chromium: Page.getInstallabilityErrors = %v", b.label, b.name, b.installErrors)
			}
		}
	})

	t.Run("b_name", func(t *testing.T) {
		for _, b := range []pwaBoot{alpha, beta} {
			if b.manifest == nil || len(b.manifestErrors) != 0 {
				t.Errorf("pwa clause (b) name: the %s boot's manifest (%q) did not parse cleanly: parsed=%v errors=%v",
					b.label, b.manifestURL, b.manifest != nil, b.manifestErrors)
				continue
			}
			if b.manifest.Name != b.name {
				t.Errorf("pwa clause (b) name: the %s boot was armed with -app-name %q and chromium read the "+
					"manifest name %q", b.label, b.name, b.manifest.Name)
			}
			// `id`, `start_url` and `scope` are `/` in the manifest; chromium reports them RESOLVED.
			for member, got := range map[string]string{"id": b.manifest.ID, "start_url": b.manifest.StartURL,
				"scope": b.manifest.Scope} {
				if got != b.base+"/" {
					t.Errorf("pwa clause (b) name: the %s boot's manifest %s resolves to %q, want %q",
						b.label, member, got, b.base+"/")
				}
			}
		}
	})

	t.Run("b_icon", func(t *testing.T) {
		urls := map[string][]string{}
		for _, b := range []pwaBoot{alpha, beta} {
			if b.manifest == nil {
				t.Errorf("pwa clause (b) icon: the %s boot has no parsed manifest to read icons from", b.label)
				continue
			}
			matched := 0
			for _, icon := range b.manifest.Icons {
				urls[b.label] = append(urls[b.label], icon.URL)
				got, err := fetchBytes(icon.URL)
				if err != nil {
					t.Errorf("pwa clause (b) icon: fetching %s: %v", icon.URL, err)
					continue
				}
				if name := committedIconFor(t, root, b.variant, got); name == "" {
					t.Errorf("pwa clause (b) icon: the %s boot (-app-icon-variant %s) served %s whose %d byte(s) "+
						"equal NO committed %s-*.png — another variant's picture, or bytes nobody committed",
						b.label, b.variant, icon.URL, len(got), b.variant)
				} else {
					matched++
				}
			}
			if matched != 3 {
				t.Errorf("pwa clause (b) icon: the %s boot's manifest matched %d committed icon(s), want 3 "+
					"(192 any, 512 any, 512 maskable)", b.label, matched)
			}
		}
		for _, u := range urls["alpha"] {
			if slices.Contains(urls["beta"], u) {
				t.Errorf("pwa clause (b) icon: both boots link %s — two instances installed side by side would "+
					"carry one picture", u)
			}
		}
	})

	// 🔴 (b: screenshots), S4: what CHROMIUM parsed as the manifest's screenshots, each fetched and
	// compared byte for byte with a committed, derivation-pinned file (`checks.ui-screenshots-are-current`
	// is what pins the committed file to the synthetic world); its IHDR must match the `sizes` chromium
	// read; and there must be at least one `narrow` and one `wide`.
	t.Run("b_screenshots", func(t *testing.T) {
		for _, b := range []pwaBoot{alpha, beta} {
			if b.manifest == nil {
				t.Errorf("pwa clause (b) screenshots: the %s boot has no parsed manifest to read screenshots from", b.label)
				continue
			}
			forms := map[string]int{}
			matched := map[string]bool{}
			for _, s := range b.manifest.Screenshots {
				if s.Image == nil {
					t.Errorf("pwa clause (b) screenshots: the %s boot's manifest has a screenshot with no image", b.label)
					continue
				}
				// CDP spells the manifest's `narrow`/`wide` as its enum, `kNarrow`/`kWide` (measured, chromium 152).
				forms[strings.ToLower(strings.TrimPrefix(s.FormFactor, "k"))]++
				got, err := fetchBytes(s.Image.URL)
				if err != nil {
					t.Errorf("pwa clause (b) screenshots: fetching %s: %v", s.Image.URL, err)
					continue
				}
				name := committedScreenshotFor(t, root, got)
				if name == "" {
					t.Errorf("pwa clause (b) screenshots: the %s boot served %s whose %d byte(s) equal NO committed "+
						"internal/ui/screenshots/*.png — bytes the derivation did not render", b.label, s.Image.URL, len(got))
					continue
				}
				cfg, err := png.DecodeConfig(bytes.NewReader(got))
				if err != nil || fmt.Sprintf("%dx%d", cfg.Width, cfg.Height) != s.Image.Sizes {
					t.Errorf("pwa clause (b) screenshots: %s (%s) is %dx%d (%v), chromium read sizes %q", s.Image.URL,
						name, cfg.Width, cfg.Height, err, s.Image.Sizes)
					continue
				}
				matched[name] = true
			}
			if forms["narrow"] < 1 || forms["wide"] < 1 {
				t.Errorf("pwa clause (b) screenshots: the %s boot's form factors are %v — the richer install dialog "+
					"needs at least one narrow and one wide", b.label, forms)
			}
			if want := committedScreenshotCount(t, root); len(matched) != want {
				t.Errorf("pwa clause (b) screenshots: the %s boot's manifest matched %d committed screenshot(s), want "+
					"all %d", b.label, len(matched), want)
			}
		}
	})

	// The shortcuts as chromium RESOLVED them — not a closing-condition clause (`internal/ui`'s
	// TestEveryShortcutIsADeclaredRowThatReturnsThroughSignIn pins their answers); this pins that a
	// browser accepts all three, in scope.
	t.Run("shortcuts", func(t *testing.T) {
		want := []string{alpha.base + "/arcs", alpha.base + "/scopes?q=", alpha.base + "/team"}
		var got []string
		if alpha.manifest != nil {
			for _, s := range alpha.manifest.Shortcuts {
				got = append(got, s.URL)
			}
		}
		if !slices.Equal(got, want) {
			t.Errorf("chromium resolved the alpha boot's shortcuts to %v, want %v", got, want)
		}
	})

	// 🔴 (e) CLIENT-SIDE STORAGE, S4 — the STATE guard over `pwa.js` (its Go test is the SPELLING one).
	t.Run("e_storage", func(t *testing.T) {
		pwaClauseE(ctx, t, alpha.base)
	})
}

// committedScreenshotFor returns the committed `internal/ui/screenshots/*.png` whose bytes equal `got`.
func committedScreenshotFor(t *testing.T, root string, got []byte) string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(root, "internal", "ui", "screenshots", "*.png"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no committed screenshot under %s (%v), so nothing could be compared", root, err)
	}
	for _, f := range files {
		want, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Equal(got, want) {
			return filepath.Base(f)
		}
	}
	return ""
}

func committedScreenshotCount(t *testing.T, root string) int {
	t.Helper()
	files, _ := filepath.Glob(filepath.Join(root, "internal", "ui", "screenshots", "*.png"))
	return len(files)
}

// The scripts every (e) browser runs before any page script, so they reach `pwa.js` first.
const (
	// iosSafariTab: chromium has no `navigator.standalone`; iOS Safari defines it, false in a tab.
	iosSafariTab = `Object.defineProperty(Navigator.prototype, "standalone", {value: false, configurable: true});`
	// blockedStorage: storage that refuses writes, as a private window or a blocked-site policy does.
	blockedStorage = `Storage.prototype.setItem = function () { throw new DOMException("blocked", "SecurityError"); };`
)

// storageOf is this origin's whole `localStorage`, as a map.
func storageOf(ctx context.Context) (map[string]string, error) {
	var out map[string]string
	err := chromedp.Run(ctx, chromedp.Evaluate(`Object.fromEntries(Object.keys(localStorage).map(k => [k, localStorage.getItem(k)]))`, &out))
	return out, err
}

func hintHidden(ctx context.Context) (bool, error) {
	var hidden bool
	err := chromedp.Run(ctx, chromedp.Evaluate(`document.getElementById("pwa-install-hint").hidden`, &hidden))
	return hidden, err
}

// pwaClauseE is clause (e). Four browsers, each a fresh profile:
//
//  1. THE WALK: signed in, every signed-in target the ledger derives is navigated — after it,
//     `localStorage` is EMPTY (chromium defines no `navigator.standalone`, so the hint never renders and
//     nothing is ever written). The page count is the positive control.
//  2. THE HINT: with `navigator.standalone = false`, the root's hint SHOWS; one dismiss tap leaves EXACTLY
//     `{cairn.installHintDismissed: "1"}`; after a reload the hint stays hidden; and after signing out
//     through the real control the key is STILL there (decision 11: sign-out says nothing about it).
//  3. BLOCKED STORAGE: with `setItem` throwing, the hint still shows, dismissing it raises no uncaught
//     exception and stores nothing, and the next load shows it again.
//  4. THE INSTALL BUTTON: hidden until a `beforeinstallprompt` arrives — the browser's OWN events are
//     intercepted first, because headless chromium was MEASURED firing one on its own — and then revealed
//     by a SYNTHETIC one (the reachability control); clicking it calls that event's `prompt()` once.
func pwaClauseE(ctx context.Context, t *testing.T, base string) {
	t.Helper()
	newBrowser := func(t *testing.T, scripts ...string) *Browser {
		br, err := NewBrowser(ctx, base, 3*time.Minute)
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
		if _, err := br.SignIn(fixtureToken); err != nil {
			t.Fatal(err)
		}
		return br
	}

	// 1. THE WALK.
	walker := newBrowser(t)
	targets, _, err := Targets(ui.DeclaredRouteLedger())
	if err != nil {
		t.Fatal(err)
	}
	visited := 0
	for _, tg := range targets {
		if !tg.SignedIn {
			continue
		}
		if err := chromedp.Run(walker.ctx, chromedp.Navigate(base+tg.Path), chromedp.WaitReady("body")); err != nil {
			t.Fatalf("navigating %s: %v", tg.Path, err)
		}
		visited++
	}
	if visited < 8 {
		t.Fatalf("pwa clause (e) CONTROL: the signed-in walk visited %d page(s), so an empty storage would be "+
			"measured over almost nothing", visited)
	}
	if got, err := storageOf(walker.ctx); err != nil || len(got) != 0 {
		t.Errorf("pwa clause (e) storage: after the signed-in walk (%d page(s)) localStorage is %v (%v), want EMPTY "+
			"— chromium defines no navigator.standalone, so nothing may have been written", visited, got, err)
	}

	// 2. THE HINT, AND THE ONE KEY.
	ios := newBrowser(t, iosSafariTab)
	var dismissed map[string]string
	if err := chromedp.Run(ios.ctx, chromedp.Navigate(base+ui.RootPath), chromedp.WaitReady("body")); err != nil {
		t.Fatal(err)
	}
	if hidden, err := hintHidden(ios.ctx); err != nil || hidden {
		t.Fatalf("pwa clause (e) CONTROL: with navigator.standalone = false the root's iOS hint is hidden=%v (%v) — "+
			"the dismissal below would be measured on a hint that never showed", hidden, err)
	}
	if err := chromedp.Run(ios.ctx, chromedp.Click(`#pwa-install-hint-dismiss`, chromedp.ByQuery)); err != nil {
		t.Fatal(err)
	}
	dismissed, err = storageOf(ios.ctx)
	if err != nil || len(dismissed) != 1 || dismissed["cairn.installHintDismissed"] != "1" {
		t.Errorf("pwa clause (e) storage: after ONE dismiss tap localStorage is %v (%v), want EXACTLY "+
			"{cairn.installHintDismissed: \"1\"} — O8 allows one key and one value, nothing beside it", dismissed, err)
	}
	if err := chromedp.Run(ios.ctx, chromedp.Reload(), chromedp.WaitReady("body")); err != nil {
		t.Fatal(err)
	}
	if hidden, err := hintHidden(ios.ctx); err != nil || !hidden {
		t.Errorf("pwa clause (e) hint: after a dismiss and a reload the hint is hidden=%v (%v), want hidden — the "+
			"dismissal was not remembered", hidden, err)
	}
	if err := ios.SignOut(); err != nil {
		t.Fatal(err)
	}
	if after, err := storageOf(ios.ctx); err != nil || after["cairn.installHintDismissed"] != "1" || len(after) != 1 {
		t.Errorf("pwa clause (e) hint: after signing out localStorage is %v (%v), want the one key still there "+
			"(decision 11 keeps it deliberately)", after, err)
	}

	// 3. BLOCKED STORAGE.
	blocked := newBrowser(t, iosSafariTab, blockedStorage)
	var thrown []string
	chromedp.ListenTarget(blocked.ctx, func(ev any) {
		if e, ok := ev.(*cdpruntime.EventExceptionThrown); ok {
			thrown = append(thrown, e.ExceptionDetails.Error())
		}
	})
	if err := chromedp.Run(blocked.ctx, cdpruntime.Enable(), chromedp.Navigate(base+ui.RootPath),
		chromedp.WaitReady("body")); err != nil {
		t.Fatal(err)
	}
	if hidden, err := hintHidden(blocked.ctx); err != nil || hidden {
		t.Errorf("pwa clause (e) hint: with storage refusing writes the hint is hidden=%v (%v), want SHOWN", hidden, err)
	}
	if err := chromedp.Run(blocked.ctx, chromedp.Click(`#pwa-install-hint-dismiss`, chromedp.ByQuery),
		chromedp.Sleep(200*time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	if len(thrown) != 0 {
		t.Errorf("pwa clause (e) hint: dismissing with storage refusing writes raised %d uncaught exception(s): %v",
			len(thrown), thrown)
	}
	if got, err := storageOf(blocked.ctx); err != nil || len(got) != 0 {
		t.Errorf("pwa clause (e) storage: with writes refused localStorage is %v (%v), want EMPTY", got, err)
	}
	if err := chromedp.Run(blocked.ctx, chromedp.Reload(), chromedp.WaitReady("body")); err != nil {
		t.Fatal(err)
	}
	if hidden, err := hintHidden(blocked.ctx); err != nil || hidden {
		t.Errorf("pwa clause (e) hint: after a dismissal storage refused, the next load hides the hint (%v, %v); "+
			"it must show again", hidden, err)
	}

	// 4. THE INSTALL BUTTON.
	const interceptTrusted = `window.__trustedPrompts = 0;
window.addEventListener("beforeinstallprompt", function (e) {
  if (e.isTrusted) { window.__trustedPrompts++; e.stopImmediatePropagation(); e.preventDefault(); }
}, true);`
	installer := newBrowser(t, interceptTrusted)
	var before, after, afterClick bool
	var prompted, trusted int
	if err := chromedp.Run(installer.ctx,
		chromedp.Navigate(base+ui.RootPath), chromedp.WaitReady("body"), chromedp.Sleep(500*time.Millisecond),
		chromedp.Evaluate(`document.getElementById("pwa-install").hidden`, &before),
		chromedp.Evaluate(`window.__prompted = 0;
var e = new Event("beforeinstallprompt", {cancelable: true});
e.prompt = function () { window.__prompted++; return Promise.resolve(); };
window.dispatchEvent(e); true`, nil),
		chromedp.Evaluate(`document.getElementById("pwa-install").hidden`, &after),
		chromedp.Click(`#pwa-install`, chromedp.ByQuery),
		chromedp.Evaluate(`document.getElementById("pwa-install").hidden`, &afterClick),
		chromedp.Evaluate(`window.__prompted`, &prompted),
		chromedp.Evaluate(`window.__trustedPrompts`, &trusted),
	); err != nil {
		t.Fatal(err)
	}
	t.Logf("the browser fired %d trusted beforeinstallprompt event(s) of its own (intercepted)", trusted)
	if !before {
		t.Errorf("pwa install button: VISIBLE before any beforeinstallprompt — it must be hidden by default")
	}
	if after {
		t.Errorf("pwa install button: a synthetic beforeinstallprompt did NOT reveal it (the reachability control)")
	}
	if !afterClick || prompted != 1 {
		t.Errorf("pwa install button: clicking it left hidden=%v and called prompt() %d time(s), want hidden and once",
			afterClick, prompted)
	}
}

func fetchBytes(url string) ([]byte, error) {
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

// committedIconFor returns the committed `<variant>-*.png` whose bytes equal `got`, or "".
func committedIconFor(t *testing.T, root, variant string, got []byte) string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(root, "internal", "ui", "icons", variant+"-*.png"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no committed icon matches %s-*.png under %s (%v), so nothing could be compared", variant, root, err)
	}
	for _, f := range files {
		want, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Equal(got, want) {
			return filepath.Base(f)
		}
	}
	return ""
}

// TestThePWAClauseNamesAreTheScriptsContract is a guard on the strings `pwa_check.sh` greps: each
// clause's subtest name and refusal headline is its attribution key, so a rename on EITHER side —
// this file, `main.go`'s refusals, or the script — must fail HERE rather than make the script's
// self-test attribute nothing.
//
// ⚠ AN INVARIANT GUARD, labelled: it pins spellings, not behaviour.
func TestThePWAClauseNamesAreTheScriptsContract(t *testing.T) {
	script, err := os.ReadFile("pwa_check.sh")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"TestPWAClauses/a_installability", "TestPWAClauses/b_name", "TestPWAClauses/b_icon",
		"pwa clause (a) installability", "pwa clause (b) name", "pwa clause (b) icon", "pwa clause (a) CONTROL",
		"TestPWAClauses/b_screenshots", "pwa clause (b) screenshots",
		"TestPWAClauses/e_storage", "pwa clause (e) storage", "pwa clause (e) CONTROL",
		"TOUCH REACHABILITY FAILED", "TOUCH TARGET SIZE (WCAG 2.5.8", "INPUT FONT UNDER 16px",
		"TestEveryNonPublicHTMLRowIsNoStore", "pwa clause (d) no-store",
	} {
		if !strings.Contains(string(script), want) {
			t.Errorf("pwa_check.sh does not mention %q, which it must grep to attribute a clause", want)
		}
	}
	// And clause (d)'s name and tag still exist where they are produced — the root module's Go test,
	// read as a FILE because it lives in another module.
	d, err := os.ReadFile("../internal/ui/cachecontrol_test.go")
	if err != nil {
		t.Fatalf("clause (d)'s test file cannot be read, so pwa_check.sh's (d) attribution is unpinned: %v", err)
	}
	for _, want := range []string{"func TestEveryNonPublicHTMLRowIsNoStore(", "pwa clause (d) no-store"} {
		if !strings.Contains(string(d), want) {
			t.Errorf("internal/ui/cachecontrol_test.go no longer produces %q, so pwa_check.sh's clause (d) "+
				"attribution greps for nothing", want)
		}
	}
	// And the (c) headlines still exist where they are produced — the walk's refusals.
	main, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"TOUCH REACHABILITY FAILED", "TOUCH TARGET SIZE (WCAG 2.5.8", "INPUT FONT UNDER %dpx"} {
		if !strings.Contains(string(main), want) {
			t.Errorf("main.go no longer produces %q, so pwa_check.sh's clause (c) attribution greps for nothing", want)
		}
	}
	if minInputFontPx != 16 {
		t.Errorf("minInputFontPx is %d; pwa_check.sh greps for the 16px headline", minInputFontPx)
	}
}
