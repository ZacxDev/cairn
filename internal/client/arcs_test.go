package client

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ZacxDev/cairn/internal/api"
	"github.com/ZacxDev/cairn/internal/authz"
	"github.com/ZacxDev/cairn/internal/netid"
)

const arcWriterToken = "arc-writer-token-arc-writer-token-arc-writ"

// arcPod serves a synthetic store through the REAL pod handler; `journal` "" leaves the pod in
// the `registrations-unconfigured` off state. The credential may read and write `alpha-notes`.
func arcPod(t *testing.T, journal string) *httptest.Server {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "alpha-notes")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "gadget-one.md"), []byte("---\nservice: gadget-one\n"+
		"scope: alpha-notes\n---\n\n## What it is\nsynthetic.\n\n## Nuance / work-history\n"+
		"- 2000-01-03: one [cairn: arc-writer/s-0007]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	srv, err := api.New(root, []authz.TokenRecord{
		{Token: arcWriterToken, Identity: "arc-writer", Scopes: []string{"alpha-notes"}},
	}, []netip.Prefix{netip.MustParsePrefix("192.0.2.0/24")},
		netid.NewRateLimiter(1000000, time.Minute, 15*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	srv.Audit, srv.Warn = func(string) {}, func(string) {}
	srv.ArcJournal = journal
	tsrv := httptest.NewServer(srv)
	t.Cleanup(tsrv.Close)
	return tsrv
}

func arcClientHost(t *testing.T, url string) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CAIRN_MIRROR_ROOT", "")
	for _, old := range []string{"SUBSYSTEM_STORE_URL", "SUBSYSTEM_STORE_TOKEN"} {
		t.Setenv(old, "")
		os.Unsetenv(old)
	}
	t.Setenv("CAIRN_URL", url)
	t.Setenv("CAIRN_TOKEN", arcWriterToken)
	configuredHost(t, filepath.Join(home, "config"))
	return home
}

func podBody(t *testing.T, url, path string) (string, http.Header) {
	t.Helper()
	req, err := http.NewRequest("GET", url+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	tok := arcWriterToken
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return string(raw), resp.Header
}

// TestTheArcVerbsRoundTripThroughTheRealPod: register from a payload file, then `arc-show` and
// `arcs` print the pod's body VERBATIM — the property that makes `internal/report` the only
// rendering of these answers. The member wrote in `alpha-notes`, so the round trip also carries
// the pod's inference.
func TestTheArcVerbsRoundTripThroughTheRealPod(t *testing.T) {
	tsrv := arcPod(t, filepath.Join(t.TempDir(), "journal.jsonl"))
	home := arcClientHost(t, tsrv.URL)
	payload := filepath.Join(home, "arc.json")
	if err := os.WriteFile(payload, []byte(`{"schema":1,"status":"open","members":[`+
		`{"session":"s-0007","role":"originated","first_seen":""},{"session":"not joinable","role":"wrote"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out, errOut := runCLI(t, "arc-register", "--scope", "alpha-notes", "--slug", "gadget-rollout", "--from", payload)
	if code != ExitOK || out != "cairn: arc-registered instance=personal home=alpha-notes slug=gadget-rollout\n"+
		"arc-registered: alpha-notes/gadget-rollout · members=1 · unjoinable=1\n" {
		t.Fatalf("arc-register: %d\n%s\n%s", code, out, errOut)
	}
	code, out, errOut = runCLI(t, "arc-show", "--scope", "alpha-notes", "--slug", "gadget-rollout")
	want, _ := podBody(t, tsrv.URL, "/api/v1/arc/alpha-notes/gadget-rollout")
	if code != ExitOK || out != want || !strings.Contains(out, "\n- s-0007 · originated · first seen unknown · wrote in: alpha-notes") {
		t.Fatalf("arc-show must print the pod's body verbatim: %d\n%s\n--- pod\n%s\n%s", code, out, want, errOut)
	}
	code, out, _ = runCLI(t, "arcs", "--scope", "alpha-notes")
	want, _ = podBody(t, tsrv.URL, "/api/v1/arcs/alpha-notes")
	if code != ExitOK || out != want || !strings.Contains(out, "\n- alpha-notes/gadget-rollout · declared · status open") {
		t.Fatalf("arcs must print the pod's body verbatim: %d\n%s\n--- pod\n%s", code, out, want)
	}
}

// TestTheArcVerbsExitOnTheExistingCodes is Q5: no new exit code. An off-state pod REFUSES a
// registration (6, nothing written, a retry cannot help) and still ANSWERS both reads (0); a pod
// that is not there is 7 for the write and 3 for a read; a missing payload is usage (2) before
// the network.
func TestTheArcVerbsExitOnTheExistingCodes(t *testing.T) {
	tsrv := arcPod(t, "")
	home := arcClientHost(t, tsrv.URL)
	payload := filepath.Join(home, "arc.json")
	if err := os.WriteFile(payload, []byte(`{"schema":1}`), 0o644); err != nil {
		t.Fatal(err)
	}
	code, _, errOut := runCLI(t, "arc-register", "--scope", "alpha-notes", "--slug", "gadget-rollout", "--from", payload)
	if code != ExitWriteRefused || !strings.Contains(errOut, "[registrations-unconfigured]") {
		t.Fatalf("an off-state pod refuses a registration at 6: %d\n%s", code, errOut)
	}
	for _, argv := range [][]string{{"arcs", "--scope", "alpha-notes"}, {"arc-show", "--scope", "alpha-notes", "--slug", "x"}} {
		code, out, _ := runCLI(t, argv...)
		if code != ExitOK || !strings.Contains(out, "status=registrations-unconfigured") {
			t.Fatalf("%v on an off-state pod answers at 0: %d\n%s", argv, code, out)
		}
	}
	code, _, _ = runCLI(t, "arc-register", "--scope", "alpha-notes", "--slug", "x", "--from", filepath.Join(home, "missing.json"))
	if code != ExitUsage {
		t.Fatalf("a missing payload file is usage: %d", code)
	}
	tsrv.Close()
	if code, _, _ := runCLI(t, "arc-register", "--scope", "alpha-notes", "--slug", "x", "--from", payload); code != ExitWriteUnreachable {
		t.Fatalf("an unreachable pod: the write did NOT happen, 7 — got %d", code)
	}
	if code, _, _ := runCLI(t, "arcs", "--scope", "alpha-notes"); code != ExitUnreachableNoCache {
		t.Fatalf("an unreachable pod on a read is 3 — got %d", code)
	}
}

func TestTheArcVerbsTakeTheirFlagsAndNothingElse(t *testing.T) {
	for _, argv := range [][]string{
		{"arcs", "--scope", "alpha-notes"},
		{"arcs", "--repo", "."},
		{"arc-show", "--scope", "alpha-notes", "--slug", "s"},
		{"arc-register", "--repo", ".", "--slug", "s", "--from", "f.json"},
	} {
		if _, _, err := Parse(argv); err != nil {
			t.Fatalf("%v must parse: %v", argv, err)
		}
	}
	for _, argv := range [][]string{
		{"arc-show", "--scope", "alpha-notes"},                    // no --slug
		{"arc-register", "--scope", "alpha-notes", "--slug", "s"}, // no --from
		{"arcs", "--slug", "s"},                                   // arcs takes no slug
		{"arcs", "--no-sync"},                                     // nothing is cached to read
		{"arc-show", "--slug", "s", "--ref", "r"},
	} {
		if _, _, err := Parse(argv); err == nil {
			t.Fatalf("%v must be refused", argv)
		}
	}
}
