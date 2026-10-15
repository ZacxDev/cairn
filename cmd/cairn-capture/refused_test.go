package main

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ZacxDev/cairn/internal/capture"
)

// interceptedPod is the real worker (newRunPod) behind a front that answers every upload for ONE
// root with a fixed status and body, so a run can be driven into one outcome for that session
// while every other session is stored for real.
func interceptedPod(t *testing.T, root string, status int, body string) runPod {
	t.Helper()
	prefix := "/capture/v1/sessions/" + root + "/"
	return newRunPodFronted(t, true, func(real http.Handler) http.Handler {
		return http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
			if !strings.HasPrefix(r.URL.Path, prefix) {
				real.ServeHTTP(rw, r)
				return
			}
			_, _ = io.Copy(io.Discard, r.Body)
			rw.Header().Set("Content-Type", "application/json")
			rw.WriteHeader(status)
			_, _ = io.WriteString(rw, body+"\n")
		})
	})
}

// twoSessionWorld is runWorld plus a second session, s-0002, in the same project.
func twoSessionWorld(t *testing.T) (home, tokenFile string, env func(string) string) {
	t.Helper()
	home, tokenFile, env = runWorld(t, runToken)
	var b strings.Builder
	for i := 0; i < 3; i++ {
		fmt.Fprintf(&b, `{"type":"user","sessionId":"s-0002","message":{"role":"user","content":"other %d"}}`+"\n", i)
	}
	if err := os.WriteFile(filepath.Join(home, ".claude", "projects", "-work-alpha", "s-0002.jsonl"), []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	return home, tokenFile, env
}

// TestARunThatRefusedASessionCompletesAndExitsThree is the regression test for a wedged stream
// being invisible to the timer that runs `--run`: the instance answers s-0001's upload with a 409
// naming a stored position that is not the start or the end of any record the write carries
// (`UnresumableError`) — which a later run refuses again, so nothing ever clears it by itself.
//
// The pass must still COMPLETE — s-0002 stored, its watermark saved — and the `refused` line must
// stay; but the run exits 3, not 0, and its last stderr line names the count and the remedy. Run
// twice, because the point is that the refusal RECURS and the exit must recur with it.
//
// RED on the pre-change code (exit 0 on the first pass); the exit code is pinned as the literal 3, the
// printed contract, never derived from the constant under test.
func TestARunThatRefusedASessionCompletesAndExitsThree(t *testing.T) {
	p := interceptedPod(t, "s-0001", http.StatusConflict, `{"stored_to":"7"}`)
	home, tokenFile, env := twoSessionWorld(t)
	args := []string{"--run", "-host", "host-a", "-worker", p.url, "-capture-token-file", tokenFile}
	statePath := filepath.Join(home, "xdg", "cairn", "capture", "state.json")
	for pass := 1; pass <= 2; pass++ {
		var errb bytes.Buffer
		code := run(args, &bytes.Buffer{}, &errb, env)
		log := errb.String()
		if code != 3 {
			t.Fatalf("pass %d: exit %d, want 3 — a run that refused a session must not report success\n%s", pass, code, log)
		}
		if !strings.Contains(log, "refused s-0001: ") || !strings.Contains(log, "which is not the start or the end of any record") {
			t.Fatalf("pass %d: the per-session refusal line is gone:\n%s", pass, log)
		}
		lines := strings.Split(strings.TrimRight(log, "\n"), "\n")
		last := lines[len(lines)-1]
		if !strings.HasPrefix(last, "cairn-capture: 1 session refusal in this run (exit 3)") ||
			!strings.Contains(last, "remove that session's entry from the state file "+statePath) {
			t.Fatalf("pass %d: the last stderr line does not name the count and the remedy: %q", pass, last)
		}
		if !strings.Contains(log, " refused=1 ") {
			t.Fatalf("pass %d: the summary line does not count the refusal:\n%s", pass, log)
		}
	}
	// The pass COMPLETED: the other session is stored and its watermark saved.
	if recs, err := p.arc.ReadStream("s-0002", "main"); err != nil || len(recs) != 3 {
		t.Fatalf("s-0002: %d records stored (%v) — the refusal stopped the run early", len(recs), err)
	}
	st, err := capture.LoadState(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if ss := st.Sessions["s-0002"]; ss == nil || ss.Streams["main"] == nil || ss.Streams["main"].Offset == 0 {
		t.Fatalf("s-0002's watermark was not saved: %+v", st.Sessions["s-0002"])
	}
	if ss := st.Sessions["s-0001"]; ss != nil && ss.Streams["main"] != nil && ss.Streams["main"].Offset != 0 {
		t.Fatalf("s-0001's watermark advanced past a refused write: %d", ss.Streams["main"].Offset)
	}
}

// TestARunWhoseOnlyNonShipIsADeletionExitsZero: a 410 is the instance saying the session's
// transcript was DELETED there — final, counted `deleted=`, and not a failure of the run. It must
// not turn the unit failed. (The 503 half — a disarmed instance holds, exit 0 — is
// TestADisarmedInstanceIsAHoldNotAFailure, which counts `refused=1` for the session that found the
// instance stopped and is therefore what pins the exclusion of a stopped instance.)
func TestARunWhoseOnlyNonShipIsADeletionExitsZero(t *testing.T) {
	p := interceptedPod(t, "s-0001", http.StatusGone, `{"refused":"deleted"}`)
	_, tokenFile, env := twoSessionWorld(t)
	args := []string{"--run", "-host", "host-a", "-worker", p.url, "-capture-token-file", tokenFile}
	for pass := 1; pass <= 2; pass++ {
		var errb bytes.Buffer
		code := run(args, &bytes.Buffer{}, &errb, env)
		log := errb.String()
		if code != exitOK || !strings.Contains(log, " refused=0 ") {
			t.Fatalf("pass %d: exit %d, want 0 with refused=0\n%s", pass, code, log)
		}
		if pass == 1 && !strings.Contains(log, " deleted=1") {
			t.Fatalf("CONTROL: the 410 was not reached — no deleted=1 on the first pass:\n%s", log)
		}
		if strings.Contains(log, "session refusal") {
			t.Fatalf("pass %d: a refusal line on a run that refused nothing:\n%s", pass, log)
		}
	}
	if recs, _ := p.arc.ReadStream("s-0002", "main"); len(recs) != 3 {
		t.Fatalf("s-0002: %d records stored", len(recs))
	}
}

// TestADisarmedRunCountsARefusalButExitsZero is the positive control the 503 exclusion needs: the
// disarmed run DOES count `refused=1` (the session whose upload found the instance stopped), so
// exit 0 there is the exclusion working, not a run that refused nothing.
func TestADisarmedRunCountsARefusalButExitsZero(t *testing.T) {
	p := newRunPod(t, false)
	_, tokenFile, env := twoSessionWorld(t)
	var errb bytes.Buffer
	code := run([]string{"--run", "-host", "host-a", "-worker", p.url, "-capture-token-file", tokenFile}, &bytes.Buffer{}, &errb, env)
	if code != exitOK || !strings.Contains(errb.String(), " refused=1 ") || !strings.Contains(errb.String(), "capture-disarmed") {
		t.Fatalf("exit %d, want 0 with refused=1 for the stopped instance\n%s", code, errb.String())
	}
}

// TestADryRunExitsZeroWhateverItCouldNotRead is an INVARIANT guard, not regression coverage:
// `--dry-run` talks to no instance and reads no state, so none of the refusals `--run` now exits 3
// on can arise in it; its per-session `refused=` counts files it could not read, and it still
// exits 0 having printed them.
func TestADryRunExitsZeroWhateverItCouldNotRead(t *testing.T) {
	home, _, env := runWorld(t, runToken)
	sub := filepath.Join(home, ".claude", "projects", "-work-alpha", "s-0001", "subagents")
	if err := os.MkdirAll(sub, 0o700); err != nil {
		t.Fatal(err)
	}
	// A subagent stream nobody can read: listed, and unreadable.
	if err := os.WriteFile(filepath.Join(sub, "agent-x.jsonl"), []byte("{}\n"), 0o000); err != nil {
		t.Fatal(err)
	}
	var out, errb bytes.Buffer
	code := run([]string{"--dry-run"}, &out, &errb, env)
	if code != exitOK || !strings.Contains(out.String(), " refused=1 ") {
		t.Fatalf("exit %d, out %q, err %q — want exit 0 and a refused=1 count", code, out.String(), errb.String())
	}
}
