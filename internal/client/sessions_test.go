package client

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ZacxDev/cairn/internal/api"
	"github.com/ZacxDev/cairn/internal/authz"
	"github.com/ZacxDev/cairn/internal/netid"
)

const sessionsReaderToken = "sessions-reader-token-sessions-reader-tok"

// sessionsPod serves a synthetic store through the REAL pod handler. The credential may read
// `alpha-notes` and `gamma-notes` and NOT `beta-notes`, so the client's cache — synced from the
// pod's snapshot — holds exactly what the pod would answer for. All names, ids and dates are
// synthetic.
func sessionsPod(t *testing.T) *httptest.Server {
	t.Helper()
	root := t.TempDir()
	put := func(rel, body string) {
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	entry := func(service, scope, nuance string) string {
		return "---\nservice: " + service + "\nscope: " + scope + "\n---\n\n## What it is\nsynthetic.\n\n" +
			"## Nuance / work-history\n" + nuance
	}
	put("alpha-notes/gadget-one.md", entry("gadget-one", "alpha-notes",
		"- 2000-01-06: one [cairn: lambda-bot/s-0003]\n"+
			"- 2000-01-04: two [cairn: mu-bot/s-0001]\n"+
			"- untrailered\n"))
	put("alpha-notes/gizmo-two.md", entry("gizmo-two", "alpha-notes",
		"- 2000-01-02: three [cairn: mu-bot/s-0001]\n- 2000-01-05: four [cairn: nu-bot/s-0002]\n"))
	put("beta-notes/widget-three.md", entry("widget-three", "beta-notes",
		"- 2000-01-07: hidden [cairn: xi-bot/s-0077]\n"))
	put("gamma-notes/doodad-four.md", entry("doodad-four", "gamma-notes", "- 2000-01-03: unsigned\n"))
	put(".seed-stamp", "2000-01-08T00:00:00Z\n")

	// 🔴 A TRUSTED-PROXY PREFIX THE LOOPBACK PEER IS NOT IN, so the pod meters the peer itself
	// and the client need not send a client-IP header it never sends.
	srv, err := api.New(root, []authz.TokenRecord{
		{Token: sessionsReaderToken, Identity: "sessions-reader", Scopes: []string{"alpha-notes", "gamma-notes"}},
	}, []netip.Prefix{netip.MustParsePrefix("192.0.2.0/24")},
		netid.NewRateLimiter(1000000, time.Minute, 15*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	srv.Audit = func(string) {}
	srv.Warn = func(string) {}
	tsrv := httptest.NewServer(srv)
	t.Cleanup(tsrv.Close)
	return tsrv
}

// TestTheSessionsReportIsByteIdenticalOnPodAndCLI is the "one renderer" property, measured: the
// pod's `GET sessions/<scope>` body and `cairn sessions --scope <scope>`'s stdout, over one
// store and one credential, carry the SAME report bytes.
//
// ⚠ "THE SAME REPORT BYTES", NOT "THE SAME BYTES". Each surface puts ONE paragraph above the
// report, by design and exactly as for `recall`: the pod its snapshot-freshness prose
// (`serveReport`), the client its cache-state banner. So each output is split at its FIRST blank
// line, each head is checked to be the thing it should be, and the remainders must be equal.
//
// Measured at three points on the status dimension, because the property could hold for one
// branch of the renderer and not another: a populated list (`alpha-notes`), a scope the
// credential may NOT read (`beta-notes` — `scope-absent` on both, and its session never
// appears), and a readable scope with no trailer (`gamma-notes`).
func TestTheSessionsReportIsByteIdenticalOnPodAndCLI(t *testing.T) {
	tsrv := sessionsPod(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CAIRN_MIRROR_ROOT", "")
	for _, old := range []string{"SUBSYSTEM_STORE_URL", "SUBSYSTEM_STORE_TOKEN"} {
		t.Setenv(old, "")
		os.Unsetenv(old)
	}
	t.Setenv("CAIRN_URL", tsrv.URL)
	t.Setenv("CAIRN_TOKEN", sessionsReaderToken)
	configuredHost(t, filepath.Join(home, "config"))

	for _, tc := range []struct{ scope, status, mustContain, mustNotContain string }{
		{"alpha-notes", "sessions-listed", "\n- s-0001 · actor mu-bot · 2 bullets · 2000-01-02 → 2000-01-04\n", "s-0077"},
		{"beta-notes", "scope-absent", "NO SCOPE `beta-notes/` IS READABLE HERE.", "s-0077"},
		{"gamma-notes", "no-attributed-writes", "NO ATTRIBUTED WRITES — 1 bullet(s)", "s-0001"},
	} {
		t.Run(tc.scope, func(t *testing.T) {
			req, err := http.NewRequest("GET", tsrv.URL+"/api/v1/sessions/"+tc.scope, nil)
			if err != nil {
				t.Fatal(err)
			}
			tok := sessionsReaderToken // a short name: `"Bearer "+<long identifier>` reads as a credential to leakscan
			req.Header.Set("Authorization", "Bearer "+tok)
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			raw, err := io.ReadAll(resp.Body)
			resp.Body.Close()
			if err != nil {
				t.Fatal(err)
			}
			pod := string(raw)
			if resp.StatusCode != 200 || resp.Header.Get("X-Store-Status") != tc.status {
				t.Fatalf("pod answered %d %s:\n%s", resp.StatusCode, resp.Header.Get("X-Store-Status"), pod)
			}

			opts := readOpts()
			opts.NoSync = false
			opts.Scope = tc.scope
			code, cli, stderr := capture(t, Sessions, opts)
			if strconv.Itoa(code) != resp.Header.Get("X-Store-Exit") {
				t.Fatalf("exit %d, the pod's X-Store-Exit %s\n%s", code, resp.Header.Get("X-Store-Exit"), stderr)
			}

			podHead, podReport, ok := strings.Cut(pod, "\n\n")
			if !ok || !strings.HasPrefix(podHead, "🔴 SNAPSHOT, NOT THE SOURCE") {
				t.Fatalf("the pod's head is not its freshness prose:\n%s", pod)
			}
			cliHead, cliReport, ok := strings.Cut(cli, "\n\n")
			// One banner line: `live`, or — for a scope the synced store does not hold — the
			// `scope-empty` promotion `recall` makes too (see `emptyState`).
			if !ok || strings.Contains(cliHead, "\n") ||
				!(strings.HasPrefix(cliHead, "cairn: live") || strings.HasPrefix(cliHead, "cairn: scope-empty")) {
				t.Fatalf("the CLI's head is not one sync banner line:\n%s\n%s", cli, stderr)
			}
			if podReport != cliReport {
				t.Fatalf("pod and CLI rendered DIFFERENT reports:\n--- pod\n%s\n--- cli\n%s", podReport, cliReport)
			}
			// The positive control on the comparison: the shared bytes are a real report, with
			// this scope's own content in them — two empty strings are also equal.
			if !strings.HasPrefix(podReport, "cairn-sessions: status="+tc.status+" scope="+tc.scope+"\n") ||
				!strings.Contains(podReport, tc.mustContain) {
				t.Fatalf("the shared report is not the expected one:\n%s", podReport)
			}
			if strings.Contains(podReport, tc.mustNotContain) {
				t.Fatalf("the report names %q, which this scope must not:\n%s", tc.mustNotContain, podReport)
			}
		})
	}
}

func TestTheSessionsVerbTakesTheReadFlagsAndNothingElse(t *testing.T) {
	verb, opts, err := Parse([]string{"sessions", "--scope", "alpha-notes", "--no-sync"})
	if err != nil || verb.Name != "sessions" || opts.Scope != "alpha-notes" || !opts.NoSync {
		t.Fatalf("verb=%q opts=%#v err=%v", verb.Name, opts, err)
	}
	if _, _, err := Parse([]string{"sessions", "--repo", "."}); err != nil {
		t.Fatalf("--repo is how the scope is derived, like recall: %v", err)
	}
	for _, flag := range []string{"--ref", "--mode", "--session"} {
		if _, _, err := Parse([]string{"sessions", flag, "x"}); err == nil {
			t.Fatalf("sessions must refuse %s", flag)
		}
	}
}

func TestTheSessionsVerbExitsOnTheExistingReadCodes(t *testing.T) {
	// 0 for an answer, 3 for "nothing in the scope could be scanned" (with the warning on
	// stderr), 3 for `--no-sync` with no cache — the existing read contract, no new code.
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CAIRN_MIRROR_ROOT", "")
	configuredHost(t, filepath.Join(home, "config"))
	cache := filepath.Join(home, ".cache", "subsystem-store")
	seedCache(t, cache, "alpha-notes", "one")
	if err := os.MkdirAll(filepath.Join(cache, "rubble-heap"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cache, "rubble-heap", "broken.md"),
		[]byte("---\nscope: rubble-heap\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	opts := readOpts()
	opts.Scope = "alpha-notes"
	if code, stdout, stderr := capture(t, Sessions, opts); code != ExitOK ||
		!strings.Contains(stdout, "status=no-attributed-writes") || stderr != "" {
		t.Fatalf("a readable scope exits 0: %d\n%s\n%s", code, stdout, stderr)
	}
	opts.Scope = "rubble-heap"
	code, stdout, stderr := capture(t, Sessions, opts)
	if code != ExitUnreachableNoCache || !strings.Contains(stdout, "status=scope-unreadable") ||
		!strings.HasPrefix(stderr, "cairn-sessions: scope-unreadable: none of the 1 entry file(s)") {
		t.Fatalf("an unscannable scope exits 3 with its warning: %d\n%s\n%s", code, stdout, stderr)
	}
	opts.Cache, opts.CacheExplicit = filepath.Join(home, "no-cache-here"), true
	opts.Scope = "alpha-notes"
	if code, _, _ := capture(t, Sessions, opts); code != ExitUnreachableNoCache {
		t.Fatalf("--no-sync with no cache is 3, got %d", code)
	}
}
