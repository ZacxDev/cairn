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

// prePodOf answers a schema-2 payload EXACTLY as a pod built before schema 2 does — its strict
// decoder's 400 naming the first unknown key, the line measured against such a build — and hands
// every other request to `next`, the real pod. It is the old pod for the one request shape the two
// builds disagree on.
func prePodOf(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			body, _ := io.ReadAll(r.Body)
			r.Body = io.NopCloser(strings.NewReader(string(body)))
			// The old decoder names the FIRST unknown key in document order.
			first, at := "", len(body)
			for _, key := range []string{"summary", "visible_to_declared"} {
				if i := strings.Index(string(body), `"`+key+`"`); i >= 0 && i < at {
					first, at = key, i
				}
			}
			if first != "" {
				w.Header().Set("X-Store-Status", "bad-request")
				w.WriteHeader(400)
				io.WriteString(w, oldPodRefusalPrefix+`"`+first+`")`+"\n")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// oldPodRefusalPrefix + `"<key>")` is the old pod's line, measured against such a build for both keys.
const oldPodRefusalPrefix = `bad request: body must be one JSON object of the registration schema (json: unknown field `

const oldPodRefusal = oldPodRefusalPrefix + `"summary")`

const schemaTwoPayload = `{"schema":2,"status":"open","members":[{"session":"s-0007","role":"originated"}],` +
	`"summary":{"state_now":["moved"],"next_steps":[],"task_refs":[],"aliases":[],` +
	`"source":{"repo_path":"docs/h.md","commit":"0123456789abcdef0123456789abcdef01234567","committed_at":"2000-01-04T00:00:00Z"}},` +
	`"visible_to_declared":true}`

// TestAnOldPodsRefusalOfSchemaTwoIsReportedAsARefusal: a pod built before schema 2 refuses the
// payload, and the client SAYS so — the pod's own line on stderr, the write-refused exit (6), no
// "registered" line, and nothing stored. It does not downgrade and re-send: that printed success
// while the summary the caller sent was dropped. The POSITIVE CONTROL is the same payload to a pod
// that knows schema 2, which registers it and serves the summary.
func TestAnOldPodsRefusalOfSchemaTwoIsReportedAsARefusal(t *testing.T) {
	for _, old := range []bool{true, false} {
		journal := filepath.Join(t.TempDir(), "journal.jsonl")
		tsrv := arcPod(t, journal)
		url := tsrv.URL
		if old {
			wrapped := httptest.NewServer(prePodOf(tsrv.Config.Handler))
			t.Cleanup(wrapped.Close)
			url = wrapped.URL
		}
		home := arcClientHost(t, url)
		payload := filepath.Join(home, "arc.json")
		if err := os.WriteFile(payload, []byte(schemaTwoPayload), 0o644); err != nil {
			t.Fatal(err)
		}
		code, out, errOut := runCLI(t, "arc-register", "--scope", "alpha-notes", "--slug", "gadget-rollout", "--from", payload)
		shown, _ := podBody(t, tsrv.URL, "/api/v1/arc/alpha-notes/gadget-rollout")
		if old {
			if code != ExitWriteRefused || !strings.Contains(errOut, oldPodRefusal) || strings.Contains(out, "arc-registered") ||
				!strings.Contains(shown, "status=arc-unregistered") {
				t.Fatalf("an old pod's refusal must be reported, exit 6, nothing stored: %d\nstdout:\n%s\nstderr:\n%s\nshown:\n%s", code, out, errOut, shown)
			}
			continue
		}
		if code != ExitOK || !strings.Contains(out, "arc-registered: alpha-notes/gadget-rollout · members=1") ||
			!strings.Contains(shown, "\nstate now: 1\n  moved\n") {
			t.Fatalf("POSITIVE CONTROL: a schema-2 pod registers the payload: %d\n%s\n%s\nshown:\n%s", code, out, errOut, shown)
		}
	}
}

// TestAnOldPodsRefusalOfAnOptInOnlyPayloadIsReportedToo: the opt-in without a summary is refused by
// an old pod on `visible_to_declared` itself, and the client reports that line (exit 6, nothing
// stored). The stub answers the key that comes first, as the old decoder does.
func TestAnOldPodsRefusalOfAnOptInOnlyPayloadIsReportedToo(t *testing.T) {
	journal := filepath.Join(t.TempDir(), "journal.jsonl")
	tsrv := arcPod(t, journal)
	wrapped := httptest.NewServer(prePodOf(tsrv.Config.Handler))
	t.Cleanup(wrapped.Close)
	home := arcClientHost(t, wrapped.URL)
	payload := filepath.Join(home, "arc.json")
	if err := os.WriteFile(payload, []byte(`{"schema":2,"status":"open","visible_to_declared":true}`), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out, errOut := runCLI(t, "arc-register", "--scope", "alpha-notes", "--slug", "gadget-rollout", "--from", payload)
	shown, _ := podBody(t, tsrv.URL, "/api/v1/arc/alpha-notes/gadget-rollout")
	if code != ExitWriteRefused || !strings.Contains(errOut, oldPodRefusalPrefix+`"visible_to_declared")`) ||
		strings.Contains(out, "arc-registered") || !strings.Contains(shown, "status=arc-unregistered") {
		t.Fatalf("an opt-in-only payload to an old pod must be reported as refused: %d\nstdout:\n%s\nstderr:\n%s\nshown:\n%s", code, out, errOut, shown)
	}
}

// viewPayloadForCLI is a schema-2 arc with three ranked steps; all names synthetic.
const viewPayloadForCLI = `{"schema":2,"status":"open","members":[{"session":"s-0007","role":"originated"}],` +
	`"summary":{"state_now":["moved"],"next_steps":["drain the old listener","flip the gadget","close the arc"],"task_refs":[],"aliases":[],` +
	`"source":{"repo_path":"docs/h.md","commit":"0123456789abcdef0123456789abcdef01234567","committed_at":"2000-01-04T00:00:00Z"}}}`

func registerViewArc(t *testing.T, url string) {
	t.Helper()
	home := arcClientHost(t, url)
	payload := filepath.Join(home, "arc.json")
	if err := os.WriteFile(payload, []byte(viewPayloadForCLI), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, out, errOut := runCLI(t, "arc-register", "--scope", "alpha-notes", "--slug", "gadget-rollout", "--from", payload); code != ExitOK {
		t.Fatalf("register: %d\n%s\n%s", code, out, errOut)
	}
}

// TestArcShowsViewFlagsReachThePod: `--outline`, `--section/--from/--limit` and `--item` become the
// route's view query, and the client prints the pod's body for THAT query verbatim (the one renderer).
func TestArcShowsViewFlagsReachThePod(t *testing.T) {
	tsrv := arcPod(t, filepath.Join(t.TempDir(), "journal.jsonl"))
	registerViewArc(t, tsrv.URL)
	for _, tc := range []struct {
		argv  []string
		query string
		has   string
	}{
		{[]string{"--outline"}, "?view=outline", "next_steps: 3 items, "},
		{[]string{"--section", "next_steps", "--from", "1", "--limit", "1"}, "?view=page&section=next_steps&from=1&limit=1",
			"\n[next_steps#1] flip the gadget\nmore: from=2"},
		{[]string{"--item", "next_steps#2"}, "?item=next_steps%232", "\n[next_steps#2] close the arc\nend"},
	} {
		argv := append([]string{"arc-show", "--scope", "alpha-notes", "--slug", "gadget-rollout"}, tc.argv...)
		code, out, errOut := runCLI(t, argv...)
		want, headers := podBody(t, tsrv.URL, "/api/v1/arc/alpha-notes/gadget-rollout"+tc.query)
		if code != ExitOK || out != want || !strings.Contains(out, tc.has) || errOut != "" || headers.Get("X-Arc-View") == "" {
			t.Fatalf("%v: %d\n%s\n--- pod\n%s\n--- stderr\n%s", tc.argv, code, out, want, errOut)
		}
	}
}

// TestArcShowRefusesAMeaninglessViewCombination: a page flag with no section, or `--outline` beside a
// page flag, is usage (2) before the network; a value the POD refuses (its 400) is usage too, with the
// pod's sentence — never exit 3, which would say the store could not be read.
func TestArcShowRefusesAMeaninglessViewCombination(t *testing.T) {
	tsrv := arcPod(t, filepath.Join(t.TempDir(), "journal.jsonl"))
	registerViewArc(t, tsrv.URL)
	base := []string{"arc-show", "--scope", "alpha-notes", "--slug", "gadget-rollout"}
	for _, extra := range [][]string{{"--from", "1"}, {"--limit", "3"}, {"--outline", "--section", "next_steps"},
		{"--outline", "--from", "1"}, {"--item", "next_steps#0", "--from", "1"}} {
		if code, _, _ := runCLI(t, append(append([]string{}, base...), extra...)...); code != ExitUsage {
			t.Fatalf("%v must be usage (2), got %d", extra, code)
		}
	}
	code, _, errOut := runCLI(t, append(append([]string{}, base...), "--section", "next_steps", "--limit", "0")...)
	if code != ExitUsage || !strings.Contains(errOut, "limit must be an integer from 1 to 200, got 0") {
		t.Fatalf("the pod's 400 is usage with its sentence: %d\n%s", code, errOut)
	}
}

// TestArcShowSaysWhenAnOldPodIgnoredTheView: a pod that predates the views ignores the query and
// answers the full arc with no `X-Arc-View`. The client prints that body (it is a correct answer) and
// says on stderr that the view was ignored. CONTROL: the same request to a current pod says nothing.
func TestArcShowSaysWhenAnOldPodIgnoredTheView(t *testing.T) {
	tsrv := arcPod(t, filepath.Join(t.TempDir(), "journal.jsonl"))
	old := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.URL.RawQuery = "" // the old pod reads no view parameter
		rec := httptest.NewRecorder()
		tsrv.Config.Handler.ServeHTTP(rec, r)
		for k, v := range rec.Header() {
			if k != "X-Arc-View" { // …and never sent this header
				w.Header()[k] = v
			}
		}
		w.WriteHeader(rec.Code)
		w.Write(rec.Body.Bytes())
	}))
	t.Cleanup(old.Close)
	registerViewArc(t, tsrv.URL)
	argv := []string{"arc-show", "--scope", "alpha-notes", "--slug", "gadget-rollout", "--section", "next_steps", "--from", "2"}
	if code, _, errOut := runCLI(t, argv...); code != ExitOK || strings.Contains(errOut, "ignored the view") {
		t.Fatalf("CONTROL: a current pod answers the view and nothing is said: %d\n%s", code, errOut)
	}
	arcClientHost(t, old.URL)
	code, out, errOut := runCLI(t, argv...)
	if code != ExitOK || !strings.Contains(errOut, "the pod ignored the view and answered the FULL arc") ||
		!strings.Contains(out, "next steps (ranked): 3\n") {
		t.Fatalf("an old pod's full answer must be printed AND flagged: %d\n%s\n--- stderr\n%s", code, out, errOut)
	}
}
