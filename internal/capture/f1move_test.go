package capture

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// F1's `*` — "a tool input names the program and the read ledger is EMPTY on this run" — is a fact
// about ONE run that reverts when the ledger line lands. Routing a BOUND session on it made a
// two-step flip that ended in a permanent hold:
//
//  1. a session the repo arm had bound to the non-default instance (it recorded no scope) runs a
//     `cairn` call; Claude Code writes the call's tool input to the transcript BEFORE the client
//     appends its ledger line, so a pass landing between the two sees F1, answers the DEFAULT
//     instance, and MOVES the session: withdrawn from where it was, re-shipped to the default;
//  2. the next pass reads the ledger line, whose scope routes back to the instance it was just
//     withdrawn from — and a withdrawal is kept there as a FINAL deletion (410), so the move back
//     is refused and the session is HELD on the default for good.
//
// The operator's ruling (#317): while F1 is live a BOUND session is DEFERRED — nothing shipped,
// nothing withdrawn — for at most [f1Window] from the first pass that saw it, then routed by the
// base rule; the window is cleared whenever F1 is false. A NEW session keeps F1's placement.
// Synthetic throughout: scopes `*-notes`, aliases `personal` (default) and `client`, a year-2000 clock.

// claudeToolUse is a Claude Code assistant record whose tool input names the program — F1's
// evidence — and nothing else: no store header, so no scope is derived from it.
func claudeToolUse(id, cwd string, i int) string {
	c, _ := json.Marshal(cwd)
	return fmt.Sprintf(`{"type":"assistant","sessionId":%q,"cwd":%s,"message":{"role":"assistant","content":`+
		`[{"type":"tool_use","id":"toolu_%d","name":"Bash","input":{"command":"cairn recall"}}]}}`, id, c, i)
}

// claudeToolResult is the tool's OUTPUT for [claudeToolUse] `i`: synthetic text carrying `marker`
// and no store header, so it adds no scope either.
func claudeToolResult(id, cwd string, i int, marker string) string {
	c, _ := json.Marshal(cwd)
	return fmt.Sprintf(`{"type":"user","sessionId":%q,"cwd":%s,"message":{"role":"user","content":`+
		`[{"type":"tool_result","tool_use_id":"toolu_%d","content":%q}]}}`, id, c, i, marker)
}

// boundWorld is a session with no recorded scope in repo `beta-notes` (table: client), bound to client
// by the repo arm on run 1 with both records shipped there, under a clock the test moves. The sink
// keeps a withdrawal as a final deletion, as the pod does.
type boundWorld struct {
	e        env
	a        *Agent
	id, repo string
	path     string
	log      *bytes.Buffer
	clock    time.Time
}

// t0 is run 1's clock: a year-2000 instant.
var t0 = time.Date(2000, 1, 2, 3, 0, 0, 0, time.UTC)

func boundToClient(t *testing.T, id string) *boundWorld {
	t.Helper()
	w := &boundWorld{e: repoEnv(t), id: id, log: &bytes.Buffer{}, clock: t0}
	w.e.sink.tombstone = true
	w.repo = gitRepo(t, t.TempDir(), "beta-notes", "")
	w.path = claudeIn(t, w.e, id, w.repo, 2)
	w.a = newAgent(t, w.e, repoRouting, &fakeRunner{}, w.log)
	w.a.Now = func() time.Time { return w.clock }
	mustRun(t, w.a)
	mustBeEmptyV(t, w.a, id)
	if ss := w.a.State.Sessions[id]; ss.Instance != "client" || len(spooled(t, w.e, "client", id, "main")) != 2 || f1Since(ss) != 0 {
		t.Fatalf("CONTROL: run 1 must bind the empty-V session to client by its repo, ship both records, and start no "+
			"window: %+v\n%s", ss, w.log.String())
	}
	return w
}

func (w *boundWorld) ss() *SessionState { return w.a.State.Sessions[w.id] }

// at moves the clock to t0+d and runs one pass.
func (w *boundWorld) at(t *testing.T, d time.Duration) Summary {
	t.Helper()
	w.clock = t0.Add(d)
	return mustRun(t, w.a)
}

func (w *boundWorld) ledger(t *testing.T, scope string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(w.e.ledger, w.id+".jsonl"), []byte(ledgerLine(w.id, "recall", scope)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func (w *boundWorld) noLedger(t *testing.T) {
	t.Helper()
	if err := os.Remove(filepath.Join(w.e.ledger, w.id+".jsonl")); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
}

func (w *boundWorld) mustBeDeferred(t *testing.T, what string, sum Summary, records int) {
	t.Helper()
	ss := w.ss()
	if !ss.Evidence.NamesProgram {
		t.Fatalf("CONTROL: %s must see F1's evidence: %+v", what, ss)
	}
	if ss.Held || ss.Instance != "client" {
		t.Fatalf("%s (F1 live, in the window): bound to %q, held %v — want still bound to client, not held\n%s",
			what, ss.Instance, ss.Held, w.log.String())
	}
	if len(w.e.sink.withdrawn["client"]) != 0 || len(w.e.sink.withdrawn["personal"]) != 0 || len(w.e.sink.probes) != 0 {
		t.Fatalf("%s (F1 live, in the window): withdrawn %v, probes %v — a deferred pass withdraws and asks NOTHING\n%s",
			what, w.e.sink.withdrawn, w.e.sink.probes, w.log.String())
	}
	if got := len(spooled(t, w.e, "client", w.id, "main")); got != records || w.e.sink.has("personal", w.id) {
		t.Fatalf("%s (F1 live, in the window): client holds %d main records (want %d), personal holds any: %v — a "+
			"deferred pass ships NOTHING\n%s", what, got, records, w.e.sink.has("personal", w.id), w.log.String())
	}
	if deferredCount(sum) != 1 || sum.Refused != 0 {
		t.Fatalf("%s (F1 live, in the window): deferred=%d refused=%d — want deferred=1 and NOT a refusal", what, deferredCount(sum), sum.Refused)
	}
}

func (w *boundWorld) count(s string) int { return strings.Count(w.log.String(), s) }

// TestF1DefersABoundSessionAndSoCannotFlipItIntoAHold is THE regression, end to end through
// `Agent.Run`: bound by the repo arm → passes under F1 → the ledger line lands inside the window.
// It also pins (e): `deferred=` counts, `refused=` does not, and the deferral is logged ONCE.
//
// RED on 1c529c5: run 2 withdraws the session from `client` and re-ships it to `personal`; run 3
// then routes it back to `client`, whose probe answers 410, and the session is HELD on `personal`.
func TestF1DefersABoundSessionAndSoCannotFlipItIntoAHold(t *testing.T) {
	w := boundToClient(t, "s-f1")
	for i, d := range []time.Duration{5 * time.Minute, 10 * time.Minute} {
		appendTo(t, w.path, claudeToolUse(w.id, w.repo, 2+i))
		w.mustBeDeferred(t, fmt.Sprintf("pass at +%v", d), w.at(t, d), 2)
		if got := f1Since(w.ss()); got != t0.Add(5*time.Minute).Unix() {
			t.Fatalf("pass at +%v: f1_since %d, want the FIRST F1 pass's clock %d", d, got, t0.Add(5*time.Minute).Unix())
		}
	}
	if n := w.count("defer s-f1:"); n != 1 {
		t.Fatalf("the deferral was logged %d times over two deferred passes, want ONCE\n%s", n, w.log.String())
	}
	if strings.Contains(w.log.String(), "refused s-f1") {
		t.Fatalf("a deferral was logged as a refusal\n%s", w.log.String())
	}

	// The ledger line lands, naming the repo's own scope. Nothing moves, nothing is held, the window clears.
	w.ledger(t, "beta-notes")
	appendTo(t, w.path, claudeLine(w.id, w.repo, 4))
	sum := w.at(t, 15*time.Minute)
	ss := w.ss()
	if ss.Held || ss.Instance != "client" || !slices.Equal(ss.V, []string{"beta-notes"}) {
		t.Fatalf("ledger landed: bound to %q, held %v (%s), V %v — want client, not held, [beta-notes]\n%s",
			ss.Instance, ss.Held, ss.HeldReason, ss.V, w.log.String())
	}
	if strings.Contains(w.log.String(), "HOLD") || len(w.e.sink.withdrawn["client"]) != 0 || len(w.e.sink.withdrawn["personal"]) != 0 {
		t.Fatalf("ledger landed: a HOLD or a withdrawal was logged (withdrawn %v)\n%s", w.e.sink.withdrawn, w.log.String())
	}
	if got := len(spooled(t, w.e, "client", w.id, "main")); got != 5 || deferredCount(sum) != 0 || f1Since(ss) != 0 || f1Lapsed(ss) {
		t.Fatalf("ledger landed: client holds %d main records (want 5), deferred=%d, f1_since %d, lapsed %v — want the "+
			"deferred records resumed and the window CLEARED", got, deferredCount(sum), f1Since(ss), f1Lapsed(ss))
	}
}

// TestF1PastTheWindowFallsBackToTheBaseRule (b): F1 still live when the window expires ⇒ the base
// rule's move happens on the FIRST pass at or after expiry — withdrawn from client, re-shipped to
// personal — logged once. The last step pins the NAMED RESIDUAL that remains: a ledger line landing
// after that move routes the session back to client, whose 410 holds it, exactly as before #317.
func TestF1PastTheWindowFallsBackToTheBaseRule(t *testing.T) {
	w := boundToClient(t, "s-lapse")
	appendTo(t, w.path, claudeToolUse(w.id, w.repo, 2))
	start := 5 * time.Minute
	w.mustBeDeferred(t, "the first F1 pass", w.at(t, start), 2)
	w.mustBeDeferred(t, "one second before expiry", w.at(t, start+f1Window-time.Second), 2)

	sum := w.at(t, start+f1Window)
	ss := w.ss()
	if ss.Instance != "personal" || ss.Held || !slices.Contains(w.e.sink.withdrawn["client"], w.id) || deferredCount(sum) != 0 || sum.Withdrawn != 1 {
		t.Fatalf("at expiry: bound to %q, held %v, withdrawn %v, deferred=%d — want the base rule's move to personal\n%s",
			ss.Instance, ss.Held, w.e.sink.withdrawn, deferredCount(sum), w.log.String())
	}
	if got := len(spooled(t, w.e, "personal", w.id, "main")); got != 3 || !f1Lapsed(ss) {
		t.Fatalf("at expiry: personal holds %d main records (want the whole session, 3), lapsed %v", got, f1Lapsed(ss))
	}
	appendTo(t, w.path, claudeLine(w.id, w.repo, 3))
	if sum := w.at(t, start+f1Window+5*time.Minute); deferredCount(sum) != 0 || len(spooled(t, w.e, "personal", w.id, "main")) != 4 {
		t.Fatalf("after expiry, F1 still live: deferred=%d, personal holds %d — want shipping to personal",
			deferredCount(sum), len(spooled(t, w.e, "personal", w.id, "main")))
	}
	if n := w.count("window expired"); n != 1 {
		t.Fatalf("the expiry was logged %d times, want ONCE\n%s", n, w.log.String())
	}

	// NAMED RESIDUAL: the ledger lands late and routes the session back where it was withdrawn from.
	w.ledger(t, "beta-notes")
	w.at(t, start+f1Window+10*time.Minute)
	if ss := w.ss(); !ss.Held || ss.Instance != "personal" || !strings.Contains(w.log.String(), "HOLD") {
		t.Fatalf("KNOWN RESIDUAL MOVED: a ledger line after an expired window must still flip into the 410 hold "+
			"(pinned); got bound %q held %v\n%s", ss.Instance, ss.Held, w.log.String())
	}
}

// TestAClearedF1ResetsTheWindow (c): F1 clears (a ledger line), then re-appears (the ledger file is
// gone — anything on the host can delete it). The new episode gets a FRESH window: well past the old
// window's end it is still deferred.
func TestAClearedF1ResetsTheWindow(t *testing.T) {
	w := boundToClient(t, "s-reset")
	appendTo(t, w.path, claudeToolUse(w.id, w.repo, 2))
	w.mustBeDeferred(t, "episode 1", w.at(t, 5*time.Minute), 2)

	w.ledger(t, "beta-notes")
	w.at(t, 30*time.Minute)
	if ss := w.ss(); f1Since(ss) != 0 || ss.Instance != "client" {
		t.Fatalf("F1 false: f1_since %d, bound %q — want the window CLEARED, still on client", f1Since(ss), ss.Instance)
	}

	w.noLedger(t)
	second := 50 * time.Minute
	w.mustBeDeferred(t, "episode 2 starts", w.at(t, second), 3)
	if got := f1Since(w.ss()); got != t0.Add(second).Unix() {
		t.Fatalf("episode 2: f1_since %d, want a FRESH start %d", got, t0.Add(second).Unix())
	}
	// Episode 1's window would have ended at +65m; episode 2's runs to +110m.
	w.mustBeDeferred(t, "past episode 1's window, inside episode 2's", w.at(t, second+f1Window-time.Minute), 3)
	if n := w.count("defer s-reset:"); n != 2 {
		t.Fatalf("the deferral was logged %d times over two episodes, want once per episode (2)\n%s", n, w.log.String())
	}
}

// TestTheF1WindowStartSurvivesAStateRoundTrip (d): `f1_since` is in the state file, and a later
// agent reading it measures the window from the persisted start, not from its own first pass.
func TestTheF1WindowStartSurvivesAStateRoundTrip(t *testing.T) {
	w := boundToClient(t, "s-persist")
	appendTo(t, w.path, claudeToolUse(w.id, w.repo, 2))
	w.mustBeDeferred(t, "the first F1 pass", w.at(t, 5*time.Minute), 2)
	path := filepath.Join(t.TempDir(), "state.json")
	if err := w.a.State.Save(path); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	if want := fmt.Sprintf(`"f1_since": %d`, t0.Add(5*time.Minute).Unix()); !bytes.Contains(raw, []byte(want)) {
		t.Fatalf("the saved state carries no %s: %s", want, raw)
	}
	st, err := LoadState(path)
	if err != nil {
		t.Fatal(err)
	}
	w.a = newAgent(t, w.e, repoRouting, &fakeRunner{}, w.log)
	w.a.State = st
	w.a.Now = func() time.Time { return w.clock }
	if sum := w.at(t, 5*time.Minute+f1Window); deferredCount(sum) != 0 || w.ss().Instance != "personal" {
		t.Fatalf("a fresh agent at the persisted window's end: deferred=%d, bound %q — want the base move to personal "+
			"(a window restarted by the new agent would defer)", deferredCount(sum), w.ss().Instance)
	}
}

// TestADryRunPrintsRunsDecisionForABoundSessionUnderF1: `--dry-run` prints `--run`'s decision
// (#293), reading `f1_since` READ-ONLY: `deferred` inside the window, the base move after it.
//
// RED on 1c529c5 (it printed `ship:personal` inside the window).
func TestADryRunPrintsRunsDecisionForABoundSessionUnderF1(t *testing.T) {
	w := boundToClient(t, "s-dry")
	appendTo(t, w.path, claudeToolUse(w.id, w.repo, 2))
	dry := func(what string, want string) {
		t.Helper()
		var out bytes.Buffer
		before := f1Since(w.ss())
		if err := w.a.DryRun(&out); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out.String(), "v=[*] decision="+want+"\n") {
			t.Fatalf("%s: the dry run printed %q, want v=[*] decision=%s", what, out.String(), want)
		}
		if f1Since(w.ss()) != before {
			t.Fatalf("%s: the dry run WROTE f1_since (%d → %d)", what, before, f1Since(w.ss()))
		}
	}
	w.clock = t0.Add(5 * time.Minute)
	dry("before --run started the window", "deferred")
	w.mustBeDeferred(t, "--run", w.at(t, 5*time.Minute), 2)
	w.clock = t0.Add(5*time.Minute + f1Window)
	dry("at the window's end", "ship:personal")
	if w.at(t, 5*time.Minute+f1Window); w.ss().Instance != "personal" {
		t.Fatalf("CONTROL: --run must agree with the dry run at the window's end: %+v", w.ss())
	}
}

// TestF1DefersOnlyABoundSession pins [DecideRun]'s rule on literal expectations.
func TestF1DefersOnlyABoundSession(t *testing.T) {
	beta := Repo{Scope: "beta-notes"}
	now := t0.Add(3 * time.Hour)
	inside, expired := now.Add(-f1Window+time.Minute), now.Add(-f1Window)
	type want struct {
		deferred bool
		instance string
		lapsed   bool
	}
	for _, tc := range []struct {
		name  string
		v     []string
		repo  Repo
		f1    bool
		bound string
		since time.Time
		want  want
	}{
		// REGRESSION rows (not deferred on 1c529c5): bound + F1 inside the window ⇒ deferred, for each
		// bound instance, whether this pass starts the window (zero) or it is already running.
		{"bound to the non-default, window starts", nil, beta, true, "client", time.Time{}, want{deferred: true}},
		{"bound to the non-default, its own scope, window running", []string{"beta-notes"}, Repo{}, true, "client", inside, want{deferred: true}},
		{"bound to the default, a scope of the other", []string{"beta-notes"}, Repo{}, true, "personal", inside, want{deferred: true}},
		{"bound to the default, empty V", nil, beta, true, "personal", time.Time{}, want{deferred: true}},
		// …whatever the answer without F1 would be — a hold included (it would withdraw the prefix).
		{"bound, a scope routed to no configured instance", []string{"stale-notes"}, Repo{}, true, "client", inside, want{deferred: true}},
		// Expired: the BASE rule, exactly as 1c529c5 answered (F1 forces the default).
		{"bound to the non-default, window expired", nil, beta, true, "client", expired, want{instance: "personal", lapsed: true}},
		{"bound to the default, window expired", []string{"beta-notes"}, Repo{}, true, "personal", expired, want{instance: "personal", lapsed: true}},
		// INVARIANT GUARDS (same answer on 1c529c5): unbound + F1 keeps F1's placement on the default…
		{"unbound, empty V", nil, beta, true, "", time.Time{}, want{instance: "personal"}},
		{"unbound, a scope of the other instance", []string{"beta-notes"}, Repo{}, true, "", time.Time{}, want{instance: "personal"}},
		// …and bound without F1 routes as before, a move included — a stale window start read by nothing.
		{"bound, no F1, its repo's instance", nil, beta, false, "client", expired, want{instance: "client"}},
		{"bound, no F1, a scope routes elsewhere", []string{"alpha-notes"}, Repo{}, false, "client", inside, want{instance: "personal"}},
		{"bound to the default, no F1, a scope of the other", []string{"beta-notes"}, Repo{}, false, "personal", time.Time{}, want{instance: "client"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := decideF1(tc.v, tc.f1, tc.repo, repoRouting, tc.bound, tc.since, now)
			if d.Held || d.Deferred != tc.want.deferred || d.F1Deferred != tc.want.deferred || d.Instance != tc.want.instance ||
				d.F1Lapsed != tc.want.lapsed {
				t.Fatalf("%+v, want deferred=%v instance=%q lapsed=%v", d, tc.want.deferred, tc.want.instance, tc.want.lapsed)
			}
			if tc.want.deferred && !strings.Contains(d.Reason, "F1") {
				t.Fatalf("a deferral under F1 must say so: %q", d.Reason)
			}
		})
	}
	// One instance: F1 changes no routing answer and there is nowhere to be withdrawn to, so a bound
	// session is not deferred (a deferral would only stall its capture).
	if d := decideF1(nil, true, Repo{}, oneInstance, "personal", time.Time{}, now); d.Deferred || d.Instance != "personal" {
		t.Fatalf("one instance, bound, under F1: %+v, want personal, not deferred", d)
	}
}

// TestThef1WindowIsTheOperatorsDecision pins the window as a LITERAL: every other test reads the
// constant, so a changed value would move them along with it.
func TestThef1WindowIsTheOperatorsDecision(t *testing.T) {
	if f1Window != time.Hour {
		t.Fatalf("f1Window is %v; the operator decided 1h0m0s", f1Window)
	}
}

// TestTheRunLinePrintsDeferred: `deferred=` is the summary's LAST field, after `export-skipped=`, so
// no field before it moved; it carries the count, not a constant.
func TestTheRunLinePrintsDeferred(t *testing.T) {
	for _, n := range []int{0, 4} {
		line := summaryDeferred(n, 9).String()
		if !strings.HasSuffix(line, fmt.Sprintf(" export-skipped=0 deferred=%d", n)) || !strings.Contains(line, " refused=9 ") {
			t.Fatalf("the run line %q does not print refused=9 and deferred=%d last", line, n)
		}
	}
}

// TestTheUnledgeredOutputResidualShipsUnderTheLaterScope PINS A KNOWN RESIDUAL (threat T3), not a
// guarantee: deferring does not close it. Output that reached the transcript with NO ledger record of
// its own — here a call's tool result, as a swallowed ledger write or a call run with no session id
// leaves it — ships once a LATER ledger record lands, declared under that later record's scope alone,
// with no `*`; the pod's view-time F1 hid it only while the ledger was empty. The new-session path has
// the same residual on 1c529c5 (`TestAnF1PassDoesNotFreezeStarIntoTheSession`'s alpha arm).
//
// 🔴 WHEN THE RESIDUAL IS FIXED THIS TEST GOES RED — invert it deliberately, do not delete it.
func TestTheUnledgeredOutputResidualShipsUnderTheLaterScope(t *testing.T) {
	const marker = "SYNTHETIC-UNLEDGERED-OUTPUT"
	w := boundToClient(t, "s-t3")
	appendTo(t, w.path, claudeToolUse(w.id, w.repo, 2), claudeToolResult(w.id, w.repo, 2, marker))
	w.mustBeDeferred(t, "the F1 pass", w.at(t, 5*time.Minute), 2)
	// A LATER call's ledger line lands; the output above never had one.
	w.ledger(t, "beta-notes")
	w.at(t, 10*time.Minute)
	shipped := false
	for _, r := range spooled(t, w.e, "client", w.id, "main") {
		shipped = shipped || bytes.Contains(r.Rec, []byte(marker))
	}
	if !shipped || !slices.Equal(w.e.sink.declared["client/"+w.id], []string{"beta-notes"}) {
		t.Fatalf("KNOWN RESIDUAL MOVED: the unledgered output shipped=%v, declared %v (pinned: shipped, declared "+
			"[beta-notes] with no `*`). If a fix closed T3 here, invert this test deliberately\n%s",
			shipped, w.e.sink.declared["client/"+w.id], w.log.String())
	}
}
