package main

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ZacxDev/cairn/internal/authz"
)

// ---------------------------------------------------------------------------
// THE ONE THING THIS PACKAGE HAS TO PROVE, AND WHY IT COULD NOT BE PROVED FROM A
// LIBRARY TEST.
//
// 🔴 THE DIVERGENCE `internal/control/tokenfile` DECLARES — a scope directory created
// OUT OF BAND is invisible to a bare (legacy) row until the authority
// re-materializes — is DECLARED rather than closed, and the whole declaration rests on
// the window being BOUNDED. The only thing bounding it in the deployed program is the
// refresh goroutine in `main`. Measured:
// deleting that goroutine and the two imports it alone needed (`context` and
// `internal/control`) leaves `go build ./...` clean, `go vet ./...` clean and all
// thirteen `internal/...` test packages green — and before this file existed
// `tests/control_mutants.py` did not run this package at all. The mitigation had no
// gate of any kind.
//
// 🔴 AND A GATE THAT GREPS `main.go` FOR THE CALL WOULD BE SPELLED RATHER THAN
// STRUCTURAL — walkable by any rewording, and green for a loop that starts and does
// nothing. So this runs the program and watches the DIVERGENCE HEAL: a directory
// appears behind the server's back, nobody sends a signal, nobody calls `Refresh`, and
// the read has to start answering.
//
// ⚠ IT SPAWNS A PROCESS AND BINDS A LOOPBACK PORT, WHICH IS A DIMENSION — so it is
// measured at TWO points rather than one: an ordinary `go test ./...` on a developer
// host, and `packages.cairn-server`'s `checkPhase` inside the nix sandbox, where the
// build would fail if loopback or `os.Executable` were unavailable. Both green.
// ---------------------------------------------------------------------------

// runServerEnv makes THIS TEST BINARY run `main()` instead of the test suite.
//
// 🔴 THE CHILD IS THE REAL `main`, NOT A SECOND COPY OF ITS WIRING. A test that
// re-assembled the startup sequence — build a server, start a loop, serve — would pass
// with `main` deleted, which is the exact failure this file exists to refuse. Both
// variables are read HERE, in `_test.go`, and nowhere in the program: they are not a
// configuration surface an operator can reach.
const (
	runServerEnv      = "CAIRN_TEST_RUN_SERVER"
	testRefreshEnv    = "CAIRN_TEST_AUTHORITY_REFRESH_INTERVAL"
	testRefreshPeriod = 100 * time.Millisecond
)

func TestMain(m *testing.M) {
	if os.Getenv(runServerEnv) != "1" {
		os.Exit(m.Run())
	}
	// The child. It REFUSES rather than falling back to the production interval: a
	// child that quietly ran at 30s would turn every assertion below into a timeout
	// whose message named the loop instead of the harness.
	raw := os.Getenv(testRefreshEnv)
	interval, err := time.ParseDuration(raw)
	if err != nil || interval <= 0 {
		fmt.Fprintf(os.Stderr, "cairn-server test child: %s must be a positive duration, got %q\n",
			testRefreshEnv, raw)
		os.Exit(2)
	}
	refreshInterval = interval
	main()
}

// TestTheBinarysOwnTimerIsWhatClosesTheDivergence is the gate on `main`'s refresh loop.
//
// 🔴 IT PINS A RELATIONSHIP, NOT A CALL: "a scope that appears out of band becomes
// readable by a bare row with no operator action". Every arm of it is observed through
// HTTP against a process started from this package's own `main`.
//
// 🔴 AND IT CARRIES A POSITIVE CONTROL ON ITS OWN PROBE, because "the read answered" is
// indistinguishable from "this server answers everything" — a second scope that is
// never created on disk must still be absent at the end. Without it a server whose
// narrowing had collapsed to "allow" would pass this test perfectly.
func TestTheBinarysOwnTimerIsWhatClosesTheDivergence(t *testing.T) {
	srv := startServer(t)

	// PRECONDITION: the seeded scope reads, so the assertions below are about the
	// scope enumeration rather than about the server being up.
	if got := srv.status(t, "alpha-notes"); got != "recalled" {
		t.Fatalf("precondition: the seeded scope must read, got %q\n%s", got, srv.log(t))
	}
	// PRECONDITION: neither of the two scopes that are not on disk reads.
	for _, scope := range []string{"epsilon-notes", "zeta-notes"} {
		if got := srv.status(t, scope); got != "scope-absent" {
			t.Fatalf("precondition: %s does not exist yet, got %q", scope, got)
		}
	}

	// THE OUT-OF-BAND CREATE. `server/seed.sh` pushes a store through
	// `kubectl exec … tar -xf -`, which is a write to the pod's filesystem that the
	// pod itself never sees — this is that, at the smallest size that reproduces it.
	writeScope(t, srv.root, "epsilon-notes", "cog-seven")

	// THE MEASUREMENT. No SIGHUP, no `Refresh`, no request that could plausibly be a
	// trigger: the only thing that can change the answer is the timer.
	// The budget is 80 refresh periods, not a round number of seconds: it is generous
	// against a loaded machine and still fails in seconds rather than in minutes, which
	// matters because `tests/control_mutants.py` runs this package once per mutant.
	budget := 80 * testRefreshPeriod
	deadline := time.Now().Add(budget)
	var last string
	for time.Now().Before(deadline) {
		if last = srv.status(t, "epsilon-notes"); last == "recalled" {
			break
		}
		time.Sleep(testRefreshPeriod / 2)
	}
	if last != "recalled" {
		t.Fatalf("a scope created out of band never became readable: the last answer was "+
			"%q after %s at a %s refresh interval. `tokenfile`'s divergence is DECLARED on "+
			"the window being bounded, and the refresh loop in main() is the only thing "+
			"bounding it\n%s", last, budget, testRefreshPeriod, srv.log(t))
	}

	// THE POSITIVE CONTROL ON THE PROBE. A scope nobody created must still be absent,
	// or the loop above measured a server that answers everything.
	if got := srv.status(t, "zeta-notes"); got != "scope-absent" {
		t.Fatalf("the control scope was never created and must stay absent, got %q — "+
			"the assertion above cannot distinguish a working refresh from a collapsed "+
			"narrowing without it", got)
	}
}

// ---------------------------------------------------------------------------
// the harness
// ---------------------------------------------------------------------------

// testToken is a synthetic credential at the real floor, built rather than written out:
// a 43-character literal in this repository is a leak-scanner finding regardless of
// what it actually is.
var testToken = strings.Repeat("c", authz.MinTokenChars)

type serverProcess struct {
	root    string
	url     string
	logPath string
}

// startServer runs THIS BINARY as `cairn-server` over a fresh store and token file.
func startServer(t *testing.T) *serverProcess {
	t.Helper()
	root := t.TempDir()
	writeScope(t, root, "alpha-notes", "gadget-one")

	tokenFile := filepath.Join(t.TempDir(), "token")
	// A BARE row — the legacy, unrestricted shape. It is the only principal whose
	// visible set is an ENUMERATION rather than an allowlist, so it is the only one the
	// divergence can be observed through at all.
	if err := os.WriteFile(tokenFile, []byte(testToken+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	logPath := filepath.Join(t.TempDir(), "server.log")
	logFile, err := os.Create(logPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { logFile.Close() })

	// `os.Executable()` rather than `os.Args[0]`: the latter is whatever the runner
	// happened to type, and this child is started with an environment that has no PATH
	// for a relative name to be resolved against.
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	port := freePort(t)
	child := exec.Command(self,
		"-store", root,
		"-host", "127.0.0.1",
		"-port", strconv.Itoa(port),
		"-token-file", tokenFile)
	// 🔴 A HERMETIC ENVIRONMENT, NOT `os.Environ()`. `SUBSYSTEM_STORE_TOKEN` and the
	// limiter settings are read from the environment by the program under test, so
	// inheriting a developer's shell would make this pass or fail for reasons that are
	// not in this file.
	child.Env = []string{
		runServerEnv + "=1",
		testRefreshEnv + "=" + testRefreshPeriod.String(),
		// The peer is loopback and is therefore NOT trusted by this allowlist, which is
		// the documented port-forward case: the header is ignored and the request is
		// bucketed under the peer. RFC5737 TEST-NET-1, so it names nobody's network.
		"SUBSYSTEM_STORE_TRUSTED_PROXIES=192.0.2.0/24",
	}
	child.Stdout = logFile
	child.Stderr = logFile
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = child.Process.Kill()
		_, _ = child.Process.Wait()
	})

	s := &serverProcess{root: root, url: "http://127.0.0.1:" + strconv.Itoa(port), logPath: logPath}
	s.waitUntilServing(t)
	return s
}

// waitUntilServing blocks until the health route answers, and FAILS with the child's
// own output rather than with a bare timeout — a server that exited 78 on a
// misconfigured fixture would otherwise look exactly like one that is slow to start.
func (s *serverProcess) waitUntilServing(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get(s.url + "/healthz")
		if err == nil {
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("the server never answered %s/healthz\n%s", s.url, s.log(t))
}

// status is the ONE observable: `X-Store-Status` on a recall, which is the same field
// the served authorization matrix in `internal/api` is written in.
func (s *serverProcess) status(t *testing.T, scope string) string {
	t.Helper()
	req, err := http.NewRequest("GET", s.url+"/api/v1/recall/"+scope, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v\n%s", scope, err, s.log(t))
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		t.Fatalf("a read route answers 200 for an authenticated caller, got %d %q\n%s",
			resp.StatusCode, string(body), s.log(t))
	}
	return resp.Header.Get("X-Store-Status")
}

// log is the child's own stdout and stderr, TAILED rather than printed whole: the
// audit stream carries one line per request and the polling loop above issues
// hundreds, so the interesting part — the startup banner, or a refusal — is at whichever
// end is not the flood. The tail is what a failure needs; the head is one line.
func (s *serverProcess) log(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(s.logPath)
	if err != nil {
		return "(the child wrote no log: " + err.Error() + ")"
	}
	const tail = 1500
	text := string(data)
	if len(text) > tail {
		text = "…\n" + text[len(text)-tail:]
	}
	return "--- the server's own output (tail) ---\n" + text
}

func writeScope(t *testing.T, root, scope, service string) {
	t.Helper()
	dir := filepath.Join(root, scope)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, service+".md"),
		[]byte("---\nservice: "+service+"\nscope: "+scope+"\n---\n\n"+
			"## What it is\nsynthetic.\n\n## Nuance / work-history\n- 2000-01-02: a lease note.\n"),
		0o644); err != nil {
		t.Fatal(err)
	}
}

// freePort asks the kernel for a port and gives it straight back.
//
// ⚠ IT IS A RACE AND SAYING SO IS THE POINT: between the close here and the child's
// own `net.Listen` another process can take it, and the child then exits 78. That is
// LOUD — `waitUntilServing` prints the child's refusal — rather than a test that hangs,
// which is the trade a listener-passing scheme would buy at the cost of the child no
// longer being the real `main`.
func freePort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	return port
}
