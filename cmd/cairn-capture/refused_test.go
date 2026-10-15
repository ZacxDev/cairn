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
		if !strings.HasPrefix(last, "cairn-capture: 1 session(s) refused because the instance holds a stream at a position") ||
			!strings.Contains(last, "remove that session's entry from the state file "+statePath) {
			t.Fatalf("pass %d: the last stderr line does not name the count and the single-instance remedy: %q", pass, last)
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
// not turn the unit failed. (A 503 — a disarmed instance holds, exit 0 — is
// TestADisarmedInstanceIsAHoldNotAFailure and TestADisarmedRunCountsARefusalButExitsZero.)
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
		if strings.Contains(log, "session(s) refused") || strings.Contains(log, noClearingAdvice) {
			t.Fatalf("pass %d: a refusal line on a run that refused nothing:\n%s", pass, log)
		}
	}
	if recs, _ := p.arc.ReadStream("s-0002", "main"); len(recs) != 3 {
		t.Fatalf("s-0002: %d records stored", len(recs))
	}
}

// TestADisarmedRunCountsARefusalButExitsZero: the disarmed run DOES count `refused=1` (the session
// whose upload found the instance stopped), so exit 0 there is `capture.Recurs` classifying a 503 as
// not recurring, not a run that refused nothing.
func TestADisarmedRunCountsARefusalButExitsZero(t *testing.T) {
	p := newRunPod(t, false)
	_, tokenFile, env := twoSessionWorld(t)
	var errb bytes.Buffer
	code := run([]string{"--run", "-host", "host-a", "-worker", p.url, "-capture-token-file", tokenFile}, &bytes.Buffer{}, &errb, env)
	if code != exitOK || !strings.Contains(errb.String(), " refused=1 ") || !strings.Contains(errb.String(), "capture-disarmed") {
		t.Fatalf("exit %d, want 0 with refused=1 for the stopped instance\n%s", code, errb.String())
	}
}

// noClearingAdvice is the remedy text that must never be printed for a refusal clearing cannot fix.
const noClearingAdvice = "remove that session's entry"

// TestATransient502ExitsZero: a 5xx is retried next run and may not recur, so it still counts in
// `refused=` but exits 0 (an operator decision after review measured a single 502 failing the unit).
// RED at dea411d (exit 3), GREEN after.
func TestATransient502ExitsZero(t *testing.T) {
	p := interceptedPod(t, "s-0001", http.StatusBadGateway, `{"error":"bad gateway"}`)
	_, tokenFile, env := twoSessionWorld(t)
	var errb bytes.Buffer
	code := run([]string{"--run", "-host", "host-a", "-worker", p.url, "-capture-token-file", tokenFile}, &bytes.Buffer{}, &errb, env)
	log := errb.String()
	if !strings.Contains(log, " refused=1 ") || !strings.Contains(log, "refused s-0001: instance personal answered 502") {
		t.Fatalf("CONTROL: the 502 was not reached and counted:\n%s", log)
	}
	if code != exitOK || strings.Contains(log, noClearingAdvice) {
		t.Fatalf("exit %d, want 0 and no remedy line for a transient 502\n%s", code, log)
	}
}

// TestARecheckFieldHoldExitsZeroAndNeverAdvisesClearing: a 422 naming a request FIELD holds the
// stream for good — a `HOLD` line, no `refused <session>:` line — and it is marked once. Clearing
// the entry would forget the mark and re-send bytes the pod's redaction re-check refused, so the
// run must never advise it. RED at dea411d (exit 3, and the final line advised clearing).
func TestARecheckFieldHoldExitsZeroAndNeverAdvisesClearing(t *testing.T) {
	p := interceptedPod(t, "s-0001", http.StatusUnprocessableEntity, `{"refused":"recheck","field":"stream"}`)
	_, tokenFile, env := twoSessionWorld(t)
	var errb bytes.Buffer
	code := run([]string{"--run", "-host", "host-a", "-worker", p.url, "-capture-token-file", tokenFile}, &bytes.Buffer{}, &errb, env)
	log := errb.String()
	if !strings.Contains(log, "HOLD s-0001/main: ") || !strings.Contains(log, " refused=1 ") {
		t.Fatalf("CONTROL: the 422 field hold was not reached:\n%s", log)
	}
	if code != exitOK || strings.Contains(log, noClearingAdvice) || strings.Contains(log, "session(s) refused") {
		t.Fatalf("exit %d — a 422 hold must exit 0 and never advise clearing the entry\n%s", code, log)
	}
}

// TestANotOwnerRefusalExitsThreeAndSaysClearingDoesNotResolveIt: the uniform 409 recurs on every run;
// clearing this host's state does not resolve it, so the remedy must not say to.
func TestANotOwnerRefusalExitsThreeAndSaysClearingDoesNotResolveIt(t *testing.T) {
	p := interceptedPod(t, "s-0001", http.StatusConflict, `{"refused":"held-by-another-uploader"}`)
	_, tokenFile, env := twoSessionWorld(t)
	args := []string{"--run", "-host", "host-a", "-worker", p.url, "-capture-token-file", tokenFile}
	for pass := 1; pass <= 2; pass++ {
		var errb bytes.Buffer
		code := run(args, &bytes.Buffer{}, &errb, env)
		log := errb.String()
		lines := strings.Split(strings.TrimRight(log, "\n"), "\n")
		last := lines[len(lines)-1]
		if code != 3 || !strings.Contains(log, "refused s-0001: the instance answered that this session is held by another uploader (409)") ||
			!strings.HasPrefix(last, "cairn-capture: 1 session(s) refused because the instance holds the session for another uploader") ||
			!strings.Contains(last, "clearing this host's state does not resolve it") || strings.Contains(log, noClearingAdvice) {
			t.Fatalf("pass %d: exit %d\n%s", pass, code, log)
		}
	}
}

// TestTheUnresumableRemedyOnAMultiInstanceHostNeverSaysToClearTheEntry: there the entry also
// records where the earlier upload lives and any final hold; removing it can strand or duplicate it.
// The single-instance wording is the control — the same counts, one flag apart.
func TestTheUnresumableRemedyOnAMultiInstanceHostNeverSaysToClearTheEntry(t *testing.T) {
	sum := capture.Summary{Refused: 1, Unresumable: 1}
	single := strings.Join(recurringLines(sum, false, "/state.json"), "\n")
	multi := strings.Join(recurringLines(sum, true, "/state.json"), "\n")
	if !strings.Contains(single, noClearingAdvice+" from the state file /state.json") {
		t.Fatalf("CONTROL: the single-instance remedy does not say to clear the entry: %q", single)
	}
	if strings.Contains(multi, noClearingAdvice) || !strings.Contains(multi, "do NOT remove the session's whole entry") {
		t.Fatalf("the multi-instance remedy: %q", multi)
	}
	// Not-owner on a multi-instance host carries the same warning: from a move's destination probe,
	// clearing empties `instance` and strands the copy on the instance the session left.
	notOwner := strings.Join(recurringLines(capture.Summary{Refused: 1, NotOwner: 1}, true, "/state.json"), "\n")
	if !strings.Contains(notOwner, "do NOT remove the session's whole entry") || strings.Contains(notOwner, noClearingAdvice) ||
		!strings.Contains(notOwner, "does not resolve it") {
		t.Fatalf("the multi-instance not-owner remedy: %q", notOwner)
	}
	if got := recurringLines(capture.Summary{Refused: 4}, false, "/state.json"); len(got) != 0 {
		t.Fatalf("refusals that do not recur produced a remedy: %q", got)
	}
}

// TestARecheckHoldBesideATransientFailureStillExitsZero: a session whose main stream is HELD by a
// 422 naming a field, and whose subagent stream got a 502, is refused (the 502) — and neither cause
// recurs, so the run exits 0. This is the case where a classification that wrongly called the 422
// hold recurring could reach the exit: alone, the hold leaves the session with no error at all.
func TestARecheckHoldBesideATransientFailureStillExitsZero(t *testing.T) {
	p := newRunPodFronted(t, true, func(real http.Handler) http.Handler {
		return http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/capture/v1/sessions/s-0001/streams/main/records":
				rw.WriteHeader(http.StatusUnprocessableEntity)
				_, _ = io.WriteString(rw, `{"refused":"recheck","field":"stream"}`+"\n")
			case "/capture/v1/sessions/s-0001/streams/subagent:x/records":
				rw.WriteHeader(http.StatusBadGateway)
				_, _ = io.WriteString(rw, `{"error":"bad gateway"}`+"\n")
			default:
				real.ServeHTTP(rw, r)
			}
		})
	})
	_, tokenFile, env := subagentWorld(t)
	var errb bytes.Buffer
	code := run([]string{"--run", "-host", "host-a", "-worker", p.url, "-capture-token-file", tokenFile}, &bytes.Buffer{}, &errb, env)
	log := errb.String()
	if !strings.Contains(log, "HOLD s-0001/main: ") || !strings.Contains(log, "refused s-0001: instance personal answered 502") {
		t.Fatalf("CONTROL: the hold and the 502 were not both reached:\n%s", log)
	}
	if code != exitOK || strings.Contains(log, noClearingAdvice) {
		t.Fatalf("exit %d — neither a 422 hold nor a 502 recurs\n%s", code, log)
	}
}

// subagentWorld is runWorld plus a subagent stream `subagent:x` under s-0001.
func subagentWorld(t *testing.T) (home, tokenFile string, env func(string) string) {
	t.Helper()
	home, tokenFile, env = runWorld(t, runToken)
	sub := filepath.Join(home, ".claude", "projects", "-work-alpha", "s-0001", "subagents")
	if err := os.MkdirAll(sub, 0o700); err != nil {
		t.Fatal(err)
	}
	line := `{"type":"user","sessionId":"s-0001","message":{"role":"user","content":"sub"}}` + "\n"
	if err := os.WriteFile(filepath.Join(sub, "agent-x.jsonl"), []byte(line+line), 0o600); err != nil {
		t.Fatal(err)
	}
	return home, tokenFile, env
}

// TestARecurringRefusalBehindATransientFirstFailureStillExitsThree: a session reports only its
// FIRST failure, so the `refused` line names main's 502 — while its subagent stream is wedged at
// a position it cannot resume from. The classification must see every failed write, not only the
// one the session reports (the agent's `held` hook), or the wedge is masked for as long as the
// transient error lasts.
func TestARecurringRefusalBehindATransientFirstFailureStillExitsThree(t *testing.T) {
	p := newRunPodFronted(t, true, func(real http.Handler) http.Handler {
		return http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/capture/v1/sessions/s-0001/streams/main/records":
				rw.WriteHeader(http.StatusBadGateway)
				_, _ = io.WriteString(rw, `{"error":"bad gateway"}`+"\n")
			case "/capture/v1/sessions/s-0001/streams/subagent:x/records":
				rw.WriteHeader(http.StatusConflict)
				_, _ = io.WriteString(rw, `{"stored_to":"7"}`+"\n")
			default:
				real.ServeHTTP(rw, r)
			}
		})
	})
	_, tokenFile, env := subagentWorld(t)
	var errb bytes.Buffer
	code := run([]string{"--run", "-host", "host-a", "-worker", p.url, "-capture-token-file", tokenFile}, &bytes.Buffer{}, &errb, env)
	log := errb.String()
	if !strings.Contains(log, "refused s-0001: instance personal answered 502") {
		t.Fatalf("CONTROL: the session did not report main's 502 as its first failure:\n%s", log)
	}
	if code != 3 || !strings.Contains(log, "1 session(s) refused because the instance holds a stream at a position") {
		t.Fatalf("exit %d — the wedged subagent stream was masked by the 502 reported first\n%s", code, log)
	}
}

// TestOnATwoInstanceHostTheRemedyNeverSaysToClearTheEntry drives the wiring, not the wording: the
// run must hand `recurringLines` the HOST's instance count, read from its configuration. With a
// second instance configured beside the default one (an empty `instances/<alias>.env` is a
// configured instance), an unresumable refusal must print the multi-instance warning and NOT the
// single-instance "remove that session's entry". A run that passed a constant `false` there would
// tell an operator on exactly the host where clearing strands an upload to clear it.
func TestOnATwoInstanceHostTheRemedyNeverSaysToClearTheEntry(t *testing.T) {
	p := interceptedPod(t, "s-0001", http.StatusConflict, `{"stored_to":"7"}`)
	home, tokenFile, env := runWorld(t, runToken)
	instances := filepath.Join(home, ".config", "cairn", "instances")
	if err := os.MkdirAll(instances, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(instances, "secondary.env"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	var errb bytes.Buffer
	code := run([]string{"--run", "-host", "host-a", "-worker", p.url, "-capture-token-file", tokenFile}, &bytes.Buffer{}, &errb, env)
	log := errb.String()
	if !strings.Contains(log, "refused s-0001: the instance holds this stream at") {
		t.Fatalf("CONTROL: the unresumable refusal was not reached on the two-instance host:\n%s", log)
	}
	lines := strings.Split(strings.TrimRight(log, "\n"), "\n")
	last := lines[len(lines)-1]
	if code != 3 || !strings.Contains(last, "This host routes to several instances: do NOT remove the session's whole entry") ||
		strings.Contains(log, noClearingAdvice) {
		t.Fatalf("exit %d — on a two-instance host the remedy must warn against clearing the entry, never advise it: %q",
			code, last)
	}
}
