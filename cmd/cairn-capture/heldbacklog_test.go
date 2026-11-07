package main

// The held backlog: `held-backlog=` on the run line, `--list-held`, and `--clear-held`.
//
// 🔴 THE POINT OF THESE. `held` is FINAL in the capture state, and a held session is skipped before
// routing on every later pass — so the summary's `held=` counts only the holds ONE pass made and a
// permanently held session reads as `held=0` forever. Measured on a two-instance host: 30 sessions
// were held in one pass, every later pass printed `held=0` for them, and 883 were held in the state.
// Nothing reported that number; these do.

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/ZacxDev/cairn/internal/capture"
)

// heldWorld is a run world whose session has been shipped once, so the state exists and is real.
func heldWorld(t *testing.T) (repoWorld, []string) {
	t.Helper()
	p := newRunPod(t, true)
	w := repoRoutedWorld(t, "")
	workers := []string{"-worker", p.url, "-capture-token-file", w.tokenFile}
	mustRunPass(t, w, append([]string{"--run", "-host", "host-a"}, workers...))
	return w, workers
}

// holdSession marks the world's one session HELD in the state file, as a routing hold leaves it.
func holdSession(t *testing.T, w repoWorld, instance string) {
	t.Helper()
	editSession(t, w, func(ss map[string]any) {
		ss["held"], ss["held_reason"] = true, "synthetic: an unknown scope (*)"
		if instance == "" {
			delete(ss, "instance")
			return
		}
		ss["instance"] = instance
	})
}

// readState returns the state file as a generic map.
func readState(t *testing.T, w repoWorld) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(w.statePath())
	if err != nil {
		t.Fatal(err)
	}
	var st map[string]any
	if err := json.Unmarshal(raw, &st); err != nil {
		t.Fatal(err)
	}
	return st
}

func sessionOf(t *testing.T, st map[string]any) map[string]any {
	t.Helper()
	ss, ok := st["sessions"].(map[string]any)["s-0001"].(map[string]any)
	if !ok {
		t.Fatalf("the state holds no s-0001")
	}
	return ss
}

// TestTheRunLineCarriesTheHeldBacklog: the level is on the run line, and it moves when a hold is
// added — which `held=` cannot do, because the pass that would report it is the one that skips it.
func TestTheRunLineCarriesTheHeldBacklog(t *testing.T) {
	w, workers := heldWorld(t)
	log := mustRunPass(t, w, append([]string{"--run", "-host", "host-a"}, workers...))
	if !strings.HasSuffix(strings.TrimSpace(log), "held-backlog=0") {
		t.Fatalf("the run line does not carry the held backlog:\n%s", log)
	}
	holdSession(t, w, "")
	log = mustRunPass(t, w, append([]string{"--run", "-host", "host-a"}, workers...))
	if !strings.HasSuffix(strings.TrimSpace(log), "held-backlog=1") {
		t.Fatalf("a session the state marks held is not in the backlog:\n%s", log)
	}
	if !strings.Contains(log, " held=0 ") {
		t.Fatalf("CONTROL: this run should have HELD nothing — that is the whole point:\n%s", log)
	}
}

// TestListHeldPrintsTheBacklogAndWhatCanBeCleared.
func TestListHeldPrintsTheBacklogAndWhatCanBeCleared(t *testing.T) {
	w, _ := heldWorld(t)
	holdSession(t, w, "")
	var out, errb bytes.Buffer
	if code := run([]string{"--list-held"}, &out, &errb, w.env); code != exitOK {
		t.Fatalf("--list-held: exit %d\n%s", code, errb.String())
	}
	if !strings.Contains(out.String(), "s-0001\tclaude\tclearable\t-") {
		t.Fatalf("--list-held did not print the clearable hold:\n%s", out.String())
	}
	if !strings.Contains(errb.String(), "held=1 clearable=1") {
		t.Fatalf("--list-held's summary is wrong:\n%s", errb.String())
	}
	// A hold that names an instance is the record of a prefix that landed there: PERMANENT here.
	holdSession(t, w, "client")
	out.Reset()
	errb.Reset()
	if code := run([]string{"--list-held"}, &out, &errb, w.env); code != exitOK {
		t.Fatalf("--list-held: exit %d\n%s", code, errb.String())
	}
	if !strings.Contains(out.String(), "s-0001\tclaude\tpermanent\tclient") {
		t.Fatalf("a hold with an instance was offered for clearing:\n%s", out.String())
	}
	if !strings.Contains(errb.String(), "held=1 clearable=0") {
		t.Fatalf("--list-held's summary is wrong:\n%s", errb.String())
	}
}

// TestClearHeldRecoversTheClearableAndKeepsTheRest: clearing is what makes a fixed routing rule
// recover a session, since Held is FINAL and no rule change can.
func TestClearHeldRecoversTheClearableAndKeepsTheRest(t *testing.T) {
	w, _ := heldWorld(t)
	holdSession(t, w, "client")
	var out, errb bytes.Buffer
	if code := run([]string{"--clear-held"}, &out, &errb, w.env); code != exitOK {
		t.Fatalf("--clear-held: exit %d\n%s", code, errb.String())
	}
	if !strings.Contains(errb.String(), "held=1, none clearable") {
		t.Fatalf("a hold that names an instance was cleared, or not reported:\n%s", errb.String())
	}
	if ss := sessionOf(t, readState(t, w)); ss["held"] != true {
		t.Fatalf("the permanent hold was cleared: %v", ss["held"])
	}
	// Now a routing-only hold: it IS cleared, and the state says so.
	holdSession(t, w, "")
	out.Reset()
	errb.Reset()
	if code := run([]string{"--clear-held"}, &out, &errb, w.env); code != exitOK {
		t.Fatalf("--clear-held: exit %d\n%s", code, errb.String())
	}
	if !strings.Contains(out.String(), "cleared\ts-0001\tclaude") {
		t.Fatalf("the clearable hold was not cleared:\n%s", out.String())
	}
	ss := sessionOf(t, readState(t, w))
	if _, still := ss["held"]; still {
		t.Fatalf("`held` is still in the state: %v", ss)
	}
	if _, still := ss["held_reason"]; still {
		t.Fatalf("`held_reason` is still in the state: %v", ss)
	}
	// CONTROL: nothing else moved — the watermarks the withdrawal obligation depends on are intact.
	if ss["instance"] != nil && ss["instance"] != "" {
		t.Fatalf("clearing a routing-only hold moved the binding: %v", ss["instance"])
	}
	var errb2 bytes.Buffer
	if code := run([]string{"--list-held"}, &bytes.Buffer{}, &errb2, w.env); code != exitOK || !strings.Contains(errb2.String(), "held=0") {
		t.Fatalf("the backlog did not drop to zero: %s", errb2.String())
	}
}

// TestClearHeldRefusesWhileARunHoldsTheLock: a pass and a clear must not interleave — the pass saves
// the state it loaded, so a clear landing inside one is simply lost.
func TestClearHeldRefusesWhileARunHoldsTheLock(t *testing.T) {
	w, _ := heldWorld(t)
	holdSession(t, w, "")
	release, err := capture.LockState(w.statePath())
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	var errb bytes.Buffer
	if code := run([]string{"--clear-held"}, &bytes.Buffer{}, &errb, w.env); code != exitError {
		t.Fatalf("a clear took the lock from a running pass: exit %d\n%s", code, errb.String())
	}
	if !strings.Contains(errb.String(), "another run holds the lock") {
		t.Fatalf("the refusal does not say why:\n%s", errb.String())
	}
	if ss := sessionOf(t, readState(t, w)); ss["held"] != true {
		t.Fatal("the state was written while another run held the lock")
	}
}

// TestTheModesAreMutuallyExclusiveAndNamed.
func TestTheModesAreMutuallyExclusiveAndNamed(t *testing.T) {
	w, _ := heldWorld(t)
	var errb bytes.Buffer
	if code := run([]string{"--list-held", "--run"}, &bytes.Buffer{}, &errb, w.env); code != exitUsage {
		t.Fatalf("two modes were accepted: exit %d\n%s", code, errb.String())
	}
	if !strings.Contains(errb.String(), "--list-held") {
		t.Fatalf("the usage line does not name the new modes:\n%s", errb.String())
	}
}
