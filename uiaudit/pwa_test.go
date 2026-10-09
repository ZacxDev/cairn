package main

import (
	"bytes"
	"context"
	"fmt"
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
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
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
