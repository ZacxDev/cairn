package main

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/ZacxDev/cairn/internal/ui"
	"github.com/ZacxDev/cairn/internal/worker"
)

// The worker listener's startup surface (S3 of the transcripts plan). As for presence, the
// in-process cases drive the predicate and the re-exec cases drive `main`, because whether `main`
// ACTS on a refusal, binds a third listener, and hands the ARMING through is wiring no in-process
// test can see.

var workerOwnerA = worker.Owner{Kind: presenceOwnerA.Kind, ID: presenceOwnerA.ID}

const (
	childCaptureToken = "fixture-child-capture-token-host-a-not-real-xxx"
	childGarbageA     = "fixture-child-garbage-token-one-not-real-xxxxx"
	childGarbageB     = "fixture-child-garbage-token-two-not-real-xxxxx"
)

// workerArgs is the six listener flags over fresh directories, with `extra` appended.
func workerArgs(t *testing.T, tokens string, extra ...string) []string {
	t.Helper()
	return append([]string{"-worker-addr", "127.0.0.1:0", "-worker-tokens", tokens, "-worker-owner", workerOwnerA.String(),
		"-transcript-dir", t.TempDir(), "-transcript-retention", "90d", "-transcript-quota", "1GiB"}, extra...)
}

func workerTokenFile(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "worker-tokens")
	row := worker.NewTokenRow(worker.KindCapture, workerOwnerA, "host-a", childCaptureToken)
	if err := os.WriteFile(path, []byte(row.String()+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestTheWorkerFlagsAreAllOrNoneAndNeverBlank: none ⇒ off; a subset ⇒ a refusal naming what is
// missing; a blank ⇒ refused; arming with no listener ⇒ refused; the transcript directory inside
// the store ⇒ refused. All six, well formed ⇒ on.
func TestTheWorkerFlagsAreAllOrNoneAndNeverBlank(t *testing.T) {
	cache, _ := seededJournal(t, credentialLive)
	m := cache.Model()
	store := t.TempDir()
	inside := filepath.Join(store, ".transcripts")
	if err := os.Mkdir(inside, 0o700); err != nil {
		t.Fatal(err)
	}
	full := workerSettings{addr: "127.0.0.1:9", tokens: "/nonexistent/worker-tokens", owner: workerOwnerA.String(),
		dir: t.TempDir(), retention: "90d", quota: "20GB"}

	if _, on, err := workerListener(workerSettings{}, m, store); on || err != nil {
		t.Fatalf("no flags: on=%v err=%v", on, err)
	}
	got, on, err := workerListener(full, m, store)
	if !on || err != nil || got.owner != workerOwnerA || got.retention != 90*24*time.Hour || got.quota != 20e9 {
		t.Fatalf("POSITIVE CONTROL: all six: on=%v err=%v plan=%+v", on, err, got)
	}
	with := func(f func(*workerSettings)) workerSettings { s := full; f(&s); return s }
	for _, tc := range []struct {
		name string
		s    workerSettings
		want string
	}{
		{"addr alone", workerSettings{addr: "127.0.0.1:9"}, "half-configuration"},
		{"no retention", with(func(s *workerSettings) { s.retention = "" }), "-transcript-retention missing"},
		{"no quota", with(func(s *workerSettings) { s.quota = "" }), "-transcript-quota missing"},
		{"no directory", with(func(s *workerSettings) { s.dir = "" }), "-transcript-dir missing"},
		{"whitespace tokens", with(func(s *workerSettings) { s.tokens = "  " }), "-worker-tokens is set to a value that reduces to nothing"},
		{"arming with no listener", workerSettings{armed: true}, "would arm nothing"},
		{"a directory inside the store", with(func(s *workerSettings) { s.dir = inside }), "INSIDE the store root"},
		{"an owner the authority does not hold", with(func(s *workerSettings) { s.owner = "user:usr_nobody" }), "does not hold"},
		{"a retention of zero", with(func(s *workerSettings) { s.retention = "0s" }), "not a positive duration"},
		{"a quota with no number", with(func(s *workerSettings) { s.quota = "GB" }), "not a positive byte count"},
		{"host without a mint", workerSettings{host: "host-a"}, "means nothing without -issue-worker-token"},
		{"mint and listen at once", workerSettings{issue: "capture", addr: "127.0.0.1:9"}, "Refusing to do both"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, on, err := workerListener(tc.s, m, store)
			if on || err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("on=%v err=%v, want a refusal containing %q", on, err, tc.want)
			}
		})
	}
}

func TestRetentionAndQuotaParse(t *testing.T) {
	for in, want := range map[string]time.Duration{"90d": 90 * 24 * time.Hour, "1d": 24 * time.Hour, "2160h": 2160 * time.Hour, "90s": 90 * time.Second} {
		if got, err := parseRetention(in); err != nil || got != want {
			t.Errorf("parseRetention(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	for _, in := range []string{"", "0d", "-1h", "90", "ninety days", "d"} {
		if _, err := parseRetention(in); err == nil {
			t.Errorf("parseRetention(%q) accepted", in)
		}
	}
	for in, want := range map[string]int64{"20GB": 20e9, "20GiB": 20 << 30, "500MB": 500e6, "4096": 4096, "1TiB": 1 << 40, "7B": 7} {
		if got, err := parseQuota(in); err != nil || got != want {
			t.Errorf("parseQuota(%q) = %d, %v; want %d", in, got, err, want)
		}
	}
	for _, in := range []string{"", "0", "-1GB", "20 GB", "20gb", "9999999999999999TiB", "1.5GB"} {
		if _, err := parseQuota(in); err == nil {
			t.Errorf("parseQuota(%q) accepted", in)
		}
	}
}

func TestTheWorkerBindGetsItsOwnReachabilityVerdict(t *testing.T) {
	noList := errors.New("no allowlist")
	if err := workerBindRefusal("0.0.0.0:9", noList); err == nil || !strings.Contains(err.Error(), "refusing to serve the worker listener") {
		t.Fatalf("a reachable worker bind with no allowlist was admitted: %v", err)
	}
	if err := workerBindRefusal("127.0.0.1:9", noList); err != nil {
		t.Fatalf("POSITIVE CONTROL: a loopback worker bind refused: %v", err)
	}
}

// TestTheBinaryRefusesEachWorkerMisconfiguration: each arm exits 78 with ITS OWN refusal.
func TestTheBinaryRefusesEachWorkerMisconfiguration(t *testing.T) {
	_, journal := seededJournal(t, credentialLive)
	tokens := workerTokenFile(t)
	for _, arm := range []struct {
		name string
		args []string
		want string
	}{
		{"address alone", []string{"-worker-addr", "127.0.0.1:0"}, "half-configuration"},
		{"arming with no listener", []string{"-arm-transcript-capture"}, "would arm nothing"},
		{"a reachable worker bind with no allowlist", append(workerArgs(t, tokens), "-worker-addr", "0.0.0.0:0"),
			"refusing to serve the worker listener"},
		{"a token file that does not exist", append(workerArgs(t, tokens), "-worker-tokens", tokens+".typo"), "cannot be read"},
	} {
		t.Run(arm.name, func(t *testing.T) {
			c := startPresenceChild(t, journal, arm.args...)
			if code := c.exit(t); code != exitConfig {
				t.Fatalf("exit %d, want %d:\n%s", code, exitConfig, c.out.String())
			}
			if !strings.Contains(c.out.String(), arm.want) {
				t.Fatalf("the refusal does not say %q:\n%s", arm.want, c.out.String())
			}
			if strings.Contains(c.out.String(), childCaptureToken) {
				t.Fatal("a refusal printed a token")
			}
		})
	}
}

var workerLine = regexp.MustCompile(`worker listener on (127\.0\.0\.1:\d+) \(sole capture owner (\S+), (\d+) token row\(s\), 2 route\(s\)\), ` +
	`transcripts in (\S+), capture (DISARMED|ARMED)`)

func (c *presenceChild) workerAddr(t *testing.T) []string {
	t.Helper()
	m := workerLine.FindStringSubmatch(c.out.String())
	if m == nil {
		t.Fatalf("the startup line does not announce the worker listener:\n%s", c.out.String())
	}
	return m
}

func postUpload(t *testing.T, addr, token string) (int, string) {
	t.Helper()
	body := `{"schema":1,"runtime":"claude","host":"host-a","from":"","to":"100","declared_scopes":[],` +
		`"records":[{"src":"0","rec":{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"synthetic"}]}}}]}`
	req, _ := http.NewRequest(http.MethodPost, "http://"+addr+"/capture/v1/sessions/s-0001/streams/main/records", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		t.Fatalf("POST to the worker listener: %v", err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

// TestTheWorkerListenerIsDisarmedUnlessArmedByFlag drives O16 through `main`: with the six flags
// and NO arming flag the listener starts, says capture is DISARMED, and refuses a valid upload 503
// while the transcript directory stays empty. With `-arm-transcript-capture` the same upload is 200
// and the startup log carries the arming warning naming the plan's preconditions. RED if `main`
// hands anything but the flag to `worker.Config.Armed`.
func TestTheWorkerListenerIsDisarmedUnlessArmedByFlag(t *testing.T) {
	_, journal := seededJournal(t, credentialLive)
	tokens := workerTokenFile(t)

	off := startPresenceChild(t, journal, workerArgs(t, tokens)...)
	off.serving(t)
	m := off.workerAddr(t)
	if m[2] != workerOwnerA.String() || m[3] != "1" || m[5] != "DISARMED" {
		t.Fatalf("the startup line does not announce a DISARMED listener for the sole owner:\n%s", off.out.String())
	}
	if code, body := postUpload(t, m[1], childCaptureToken); code != http.StatusServiceUnavailable || body != worker.DisarmedBody {
		t.Fatalf("a disarmed child answered %d %q to a valid upload", code, body)
	}
	if entries, err := os.ReadDir(filepath.Join(m[4], "sessions")); err != nil || len(entries) != 0 {
		t.Fatalf("a disarmed child stored %d session(s) (%v)", len(entries), err)
	}
	if strings.Contains(off.out.String(), "transcript capture is ARMED") {
		t.Fatal("a disarmed child printed the arming warning")
	}

	on := startPresenceChild(t, journal, workerArgs(t, tokens, "-arm-transcript-capture")...)
	on.serving(t)
	m = on.workerAddr(t)
	if m[5] != "ARMED" || !strings.Contains(on.out.String(), "O15's arming gate") || !strings.Contains(on.out.String(), "S11's client read ledger") {
		t.Fatalf("an armed child does not announce the arming and its preconditions:\n%s", on.out.String())
	}
	if code, body := postUpload(t, m[1], childCaptureToken); code != http.StatusOK || body != `{"stored_to":"100","seq_to":1}`+"\n" {
		t.Fatalf("POSITIVE CONTROL: an armed child answered %d %q", code, body)
	}
}

// TestTheWorkerMintModeMintsOnceStoresTheDigestAndExits: exit 0, the token on stdout ONCE, its
// digest in the file, nothing on stderr but the digest prefix.
func TestTheWorkerMintModeMintsOnceStoresTheDigestAndExits(t *testing.T) {
	_, journal := seededJournal(t, credentialLive)
	tokens := filepath.Join(t.TempDir(), "worker-tokens")
	c := startPresenceChild(t, journal, "-issue-worker-token", "capture", "-worker-owner", workerOwnerA.String(),
		"-worker-host", "host-a", "-worker-tokens", tokens)
	if code := c.exit(t); code != 0 {
		t.Fatalf("the mint exited %d:\n%s", code, c.out.String())
	}
	token := strings.TrimSuffix(c.stdout.String(), "\n")
	if len(token) != 43 {
		t.Fatalf("stdout is not one 43-character token: %q", c.stdout.String())
	}
	raw, err := os.ReadFile(tokens)
	if err != nil {
		t.Fatal(err)
	}
	if want := worker.NewTokenRow(worker.KindCapture, workerOwnerA, "host-a", token).String() + "\n"; string(raw) != want {
		t.Fatalf("the token file is %q, want %q", raw, want)
	}
	if strings.Contains(string(raw), token) || strings.Contains(c.out.String(), token) {
		t.Fatal("the token reached the file or stderr")
	}
}

// TestAWorkerTokenIsGarbageToEveryBrowserRow: a capture token presented as a bearer to EVERY row
// of the browser ledger is answered exactly as a random token is — same status, same body. The
// control is two different random tokens, which must already agree; a row where they do not is a
// row this comparison cannot read, and the test says so rather than passing it.
func TestAWorkerTokenIsGarbageToEveryBrowserRow(t *testing.T) {
	_, journal := seededJournal(t, credentialLive)
	tokens := workerTokenFile(t)
	c := startPresenceChild(t, journal, workerArgs(t, tokens, "-arm-transcript-capture")...)
	base := c.serving(t)
	addr := c.workerAddr(t)[1]
	if code, _ := postUpload(t, addr, childCaptureToken); code != http.StatusOK {
		t.Fatalf("POSITIVE CONTROL: the capture token does not authenticate on the worker listener (%d)", code)
	}
	client := &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	ask := func(method, path, token string) string {
		t.Helper()
		req, _ := http.NewRequest(method, base+path, strings.NewReader(""))
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("%s %s: %v", method, path, err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return fmt.Sprintf("%d %s %s", resp.StatusCode, resp.Header.Get("Location"), b)
	}
	rows := ui.DeclaredRoutes()
	if len(rows) < 10 {
		t.Fatalf("POSITIVE CONTROL: the browser ledger has %d rows", len(rows))
	}
	for _, row := range rows {
		method, path, _ := strings.Cut(row, " ")
		g1, g2 := ask(method, path, childGarbageA), ask(method, path, childGarbageB)
		if g1 != g2 {
			t.Fatalf("%s: two random tokens are answered differently, so this row cannot be compared", row)
		}
		if got := ask(method, path, childCaptureToken); got != g1 {
			t.Fatalf("%s: a capture token is answered differently from a random one:\ncapture: %.200s\nrandom:  %.200s", row, got, g1)
		}
	}
}
