package main

import (
	"bytes"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ZacxDev/cairn/internal/control"
	"github.com/ZacxDev/cairn/internal/netid"
	"github.com/ZacxDev/cairn/internal/redact"
	"github.com/ZacxDev/cairn/internal/transcript/archive"
	"github.com/ZacxDev/cairn/internal/worker"
)

func envOf(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

// TestTheSelfTestIsClosingConditionPart3: exit 0 and the SUMMARY pair as its last line, at the
// default seed and one other. The three `control …` lines `redact.SelfTest` prints first are not
// pinned here; only the last line is the closing condition. 79 is the corpus's declared plant
// count, written as a literal rather than read from `redact.DeclaredPlants`, so a corpus that
// shrinks has to move this line too.
func TestTheSelfTestIsClosingConditionPart3(t *testing.T) {
	for _, args := range [][]string{{"--self-test"}, {"--self-test", "-seed", "42"}} {
		var out bytes.Buffer
		code := run(args, &out, &bytes.Buffer{}, envOf(nil))
		lines := strings.Split(strings.TrimSpace(out.String()), "\n")
		if code != exitOK || lines[len(lines)-1] != "SUMMARY redaction: planted=79 caught=79 clean-damaged=0" {
			t.Fatalf("%v: exit %d, output:\n%s", args, code, out.String())
		}
	}
}

func TestUsageRefusals(t *testing.T) {
	for _, args := range [][]string{{}, {"-no-such-flag"}, {"--dry-run", "extra"}, {"-verbs"}} {
		var errb bytes.Buffer
		if code := run(args, &bytes.Buffer{}, &errb, envOf(map[string]string{"HOME": t.TempDir()})); code != exitUsage {
			t.Errorf("%v: exit %d, want %d (%s)", args, code, exitUsage, errb.String())
		}
	}
}

func listFiles(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			out = append(out, p)
		}
		return nil
	})
	return out
}

// TestADryRunWritesNothing: one synthetic session, the binary's real wiring — and not one file
// created anywhere under HOME, the host key included (review round 1).
func TestADryRunWritesNothing(t *testing.T) {
	home := t.TempDir()
	proj := filepath.Join(home, "projects", "-work-alpha")
	if err := os.MkdirAll(proj, 0o700); err != nil {
		t.Fatal(err)
	}
	line := `{"type":"user","sessionId":"s-0001","message":{"role":"user","content":"hello"}}` + "\n"
	if err := os.WriteFile(filepath.Join(proj, "s-0001.jsonl"), []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
	before := listFiles(t, home)
	var out, errb bytes.Buffer
	code := run([]string{"--dry-run", "-claude-root", filepath.Join(home, "projects")}, &out, &errb,
		envOf(map[string]string{"HOME": home, "XDG_STATE_HOME": filepath.Join(home, "xdg")}))
	if code != exitOK || !strings.HasPrefix(out.String(), "claude s-0001 ") {
		t.Fatalf("exit %d, out %q, err %q", code, out.String(), errb.String())
	}
	if after := listFiles(t, home); len(after) != len(before) {
		t.Fatalf("--dry-run created files: %v", after)
	}
}

// A synthetic capture token; it has never authorised anything.
const runToken = "fixture-capture-token-run-mode-test-not-real"

type runPod struct {
	url      string
	arc      *archive.Archive
	requests *atomic.Int64
}

func newRunPod(t *testing.T, armed bool) runPod {
	t.Helper()
	red, err := redact.New(redact.SelfTestKey(5), nil)
	if err != nil {
		t.Fatal(err)
	}
	arc, err := archive.Open(archive.Config{Dir: t.TempDir(), Retention: time.Hour, Quota: 1 << 30, Recheck: red})
	if err != nil {
		t.Fatal(err)
	}
	owner := worker.Owner{Kind: control.KindUser, ID: "u_alpha"}
	tokens := filepath.Join(t.TempDir(), "worker-tokens")
	if err := os.WriteFile(tokens, []byte(worker.NewTokenRow(worker.KindCapture, owner, "host-a", runToken).String()+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	w, _, err := worker.New(worker.Config{TokenFile: tokens, Owner: owner, OwnerHeld: func() bool { return true },
		Archive: arc, Armed: armed, Limiter: netid.NewRateLimiter(1000, time.Minute, time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	var n atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		n.Add(1)
		w.ServeHTTP(rw, r)
	}))
	t.Cleanup(srv.Close)
	return runPod{url: srv.URL, arc: arc, requests: &n}
}

// runWorld is one synthetic Claude Code session under a fresh HOME, and the token file.
func runWorld(t *testing.T, token string) (home, tokenFile string, env func(string) string) {
	t.Helper()
	home = t.TempDir()
	proj := filepath.Join(home, ".claude", "projects", "-work-alpha")
	if err := os.MkdirAll(proj, 0o700); err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	for i := 0; i < 5; i++ {
		fmt.Fprintf(&b, `{"type":"user","sessionId":"s-0001","message":{"role":"user","content":"line %d"}}`+"\n", i)
	}
	if err := os.WriteFile(filepath.Join(proj, "s-0001.jsonl"), []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	tokenFile = filepath.Join(home, "capture-token")
	if err := os.WriteFile(tokenFile, []byte(token+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return home, tokenFile, envOf(map[string]string{"HOME": home, "XDG_STATE_HOME": filepath.Join(home, "xdg")})
}

// TestARunUploadsOnceAndAnIdleRunSendsNothing: `--run` against the real worker stores the
// session, saves a 0600 state, and the next run sends no request at all.
func TestARunUploadsOnceAndAnIdleRunSendsNothing(t *testing.T) {
	p := newRunPod(t, true)
	home, tokenFile, env := runWorld(t, runToken)
	args := []string{"--run", "-host", "host-a", "-worker", p.url, "-capture-token-file", tokenFile}
	var errb bytes.Buffer
	if code := run(args, &bytes.Buffer{}, &errb, env); code != exitOK {
		t.Fatalf("exit %d: %s", code, errb.String())
	}
	recs, err := p.arc.ReadStream("s-0001", "main")
	if err != nil || len(recs) != 5 {
		t.Fatalf("stored %d records (%v)\n%s", len(recs), err, errb.String())
	}
	info, err := os.Stat(filepath.Join(home, "xdg", "cairn", "capture", "state.json"))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("the state file: %v %v", info, err)
	}
	before := p.requests.Load()
	if code := run(args, &bytes.Buffer{}, &errb, env); code != exitOK || p.requests.Load() != before {
		t.Fatalf("an idle run: exit %d, %d requests", code, p.requests.Load()-before)
	}
	// An idle run does not rewrite the state: a save renames a NEW file over it.
	if again, _ := os.Stat(filepath.Join(home, "xdg", "cairn", "capture", "state.json")); !os.SameFile(info, again) {
		t.Fatal("an idle run saved the state")
	}
	if strings.Contains(errb.String(), runToken) {
		t.Fatal("the token reached the log")
	}
}

// TestADisarmedInstanceIsAHoldNotAFailure: 503 → exit 0, logged, nothing advanced — and once the
// instance is armed, the next run ships everything.
func TestADisarmedInstanceIsAHoldNotAFailure(t *testing.T) {
	p := newRunPod(t, false)
	_, tokenFile, env := runWorld(t, runToken)
	args := []string{"--run", "-host", "host-a", "-worker", p.url, "-capture-token-file", tokenFile}
	var errb bytes.Buffer
	if code := run(args, &bytes.Buffer{}, &errb, env); code != exitOK || !strings.Contains(errb.String(), "capture-disarmed") {
		t.Fatalf("exit %d: %s", code, errb.String())
	}
	armed := newRunPod(t, true)
	args[4] = armed.url
	if code := run(args, &bytes.Buffer{}, &errb, env); code != exitOK {
		t.Fatalf("exit %d: %s", code, errb.String())
	}
	if recs, _ := armed.arc.ReadStream("s-0001", "main"); len(recs) != 5 {
		t.Fatalf("after arming, %d records stored — the disarmed run advanced a watermark", len(recs))
	}
}

// TestARefusedTokenExitsOne: 401 is loud — exit 1 — and the token is never printed.
func TestARefusedTokenExitsOne(t *testing.T) {
	p := newRunPod(t, true)
	const wrong = "fixture-capture-token-no-row-holds-this-not-real"
	_, tokenFile, env := runWorld(t, wrong)
	var errb bytes.Buffer
	code := run([]string{"--run", "-host", "host-a", "-worker", p.url, "-capture-token-file", tokenFile}, &bytes.Buffer{}, &errb, env)
	if code != exitError || !strings.Contains(errb.String(), "REFUSED the capture token") || strings.Contains(errb.String(), wrong) {
		t.Fatalf("exit %d: %s", code, errb.String())
	}
}

func TestRunUsageRefusals(t *testing.T) {
	_, tokenFile, env := runWorld(t, runToken)
	open := tokenFile + ".open"
	_ = os.WriteFile(open, []byte(runToken+"\n"), 0o644)
	for name, args := range map[string][]string{
		"two modes":                       {"--run", "--dry-run", "-host", "host-a", "-worker", "http://127.0.0.1:9", "-capture-token-file", tokenFile},
		"no host":                         {"--run", "-worker", "http://127.0.0.1:9", "-capture-token-file", tokenFile},
		"no worker":                       {"--run", "-host", "host-a", "-capture-token-file", tokenFile},
		"no token file":                   {"--run", "-host", "host-a", "-worker", "http://127.0.0.1:9"},
		"request over the worker's bound": {"--run", "-host", "host-a", "-worker", "http://127.0.0.1:9", "-capture-token-file", tokenFile, "-max-request-bytes", "8388609"},
		"clear http":                      {"--run", "-host", "host-a", "-worker", "http://worker.example.invalid", "-capture-token-file", tokenFile},
		"open token file":                 {"--run", "-host", "host-a", "-worker", "http://127.0.0.1:9", "-capture-token-file", open},
		"unconfigured alias":              {"--run", "-host", "host-a", "-worker", "client=http://127.0.0.1:9", "-capture-token-file", "client=" + tokenFile},
		"token for no worker":             {"--run", "-host", "host-a", "-worker", "http://127.0.0.1:9", "-capture-token-file", tokenFile, "-capture-token-file", "client=" + tokenFile},
	} {
		var errb bytes.Buffer
		if code := run(args, &bytes.Buffer{}, &errb, env); code != exitUsage {
			t.Errorf("%s: exit %d, want %d (%s)", name, code, exitUsage, errb.String())
		}
	}
}
