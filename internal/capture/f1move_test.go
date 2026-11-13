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
// nothing withdrawn — and routes normally on the first pass where F1 is false. A NEW session keeps
// F1's placement. Synthetic throughout: scopes `*-notes`, aliases `personal` (default) and `client`.

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

// boundToClient is run 1 of the end-to-end cases: a session with no recorded scope in repo
// `beta-notes` (table: client), bound to client by the repo arm, both records shipped there. The
// sink keeps a withdrawal as a final deletion, as the pod does.
func boundToClient(t *testing.T, id string) (env, *Agent, string, string, *bytes.Buffer) {
	t.Helper()
	e := repoEnv(t)
	e.sink.tombstone = true
	repo := gitRepo(t, t.TempDir(), "beta-notes", "")
	path := claudeIn(t, e, id, repo, 2)
	var log bytes.Buffer
	a := newAgent(t, e, repoRouting, &fakeRunner{}, &log)
	mustRun(t, a)
	mustBeEmptyV(t, a, id)
	if ss := a.State.Sessions[id]; ss.Instance != "client" || len(spooled(t, e, "client", id, "main")) != 2 {
		t.Fatalf("CONTROL: run 1 must bind the empty-V session to client by its repo and ship both records: %+v\n%s", ss, log.String())
	}
	return e, a, repo, path, &log
}

// TestF1DefersABoundSessionAndSoCannotFlipItIntoAHold is THE regression, end to end through
// `Agent.Run`: bound by the repo arm → passes under F1 → the ledger line lands.
//
// RED on 1c529c5: run 2 withdraws the session from `client` and re-ships it to `personal`; run 3
// then routes it back to `client`, whose probe answers 410, and the session is HELD on `personal`.
func TestF1DefersABoundSessionAndSoCannotFlipItIntoAHold(t *testing.T) {
	e, a, repo, path, log := boundToClient(t, "s-f1")

	// Runs 2 and 2b: the transcript names the program and NO ledger file exists — F1 is live on both.
	for i, run := range []string{"2", "2b"} {
		appendTo(t, path, claudeToolUse("s-f1", repo, 2+i))
		if _, err := os.Stat(filepath.Join(e.ledger, "s-f1.jsonl")); !os.IsNotExist(err) {
			t.Fatalf("CONTROL: run %s needs NO ledger file, so that F1 is live: %v", run, err)
		}
		sum := mustRun(t, a)
		ss := a.State.Sessions["s-f1"]
		if !ss.Evidence.NamesProgram || len(ss.V) != 0 {
			t.Fatalf("CONTROL: run %s must see F1's evidence and still no recorded scope: %+v", run, ss)
		}
		if ss.Held || ss.Instance != "client" {
			t.Fatalf("run %s (F1 live): bound to %q, held %v — want still bound to client, not held\n%s",
				run, ss.Instance, ss.Held, log.String())
		}
		if len(e.sink.withdrawn["client"]) != 0 || len(e.sink.withdrawn["personal"]) != 0 || len(e.sink.probes) != 0 {
			t.Fatalf("run %s (F1 live): withdrawn %v, probes %v — a deferred pass withdraws and asks NOTHING\n%s",
				run, e.sink.withdrawn, e.sink.probes, log.String())
		}
		if got := len(spooled(t, e, "client", "s-f1", "main")); got != 2 || e.sink.has("personal", "s-f1") {
			t.Fatalf("run %s (F1 live): client holds %d main records (want the 2 of run 1), personal holds any: %v — "+
				"a deferred pass ships NOTHING\n%s", run, got, e.sink.has("personal", "s-f1"), log.String())
		}
		if sum.Refused != 1 || !strings.Contains(log.String(), "refused s-f1: its inputs name the program and its read ledger is empty this run (F1)") {
			t.Fatalf("run %s (F1 live): refused=%d — want the deferral counted once and named\n%s", run, sum.Refused, log.String())
		}
	}

	// Run 3: the ledger line lands, naming the repo's own scope. Nothing moves and nothing is held.
	if err := os.WriteFile(filepath.Join(e.ledger, "s-f1.jsonl"), []byte(ledgerLine("s-f1", "recall", "beta-notes")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	appendTo(t, path, claudeLine("s-f1", repo, 4))
	sum := mustRun(t, a)
	ss := a.State.Sessions["s-f1"]
	if ss.Held || ss.Instance != "client" || !slices.Equal(ss.V, []string{"beta-notes"}) {
		t.Fatalf("run 3 (ledger landed): bound to %q, held %v (%s), V %v — want client, not held, [beta-notes]\n%s",
			ss.Instance, ss.Held, ss.HeldReason, ss.V, log.String())
	}
	if strings.Contains(log.String(), "HOLD") || len(e.sink.withdrawn["client"]) != 0 || len(e.sink.withdrawn["personal"]) != 0 {
		t.Fatalf("run 3: a HOLD or a withdrawal was logged (withdrawn %v)\n%s", e.sink.withdrawn, log.String())
	}
	if got := len(spooled(t, e, "client", "s-f1", "main")); got != 5 || e.sink.has("personal", "s-f1") || sum.Refused != 0 {
		t.Fatalf("run 3: client holds %d main records (want 5: the deferred ones resume), personal holds any: %v, refused=%d",
			got, e.sink.has("personal", "s-f1"), sum.Refused)
	}
}

// TestADryRunPrintsRunsDecisionForABoundSessionUnderF1: `--dry-run` claims to print the decision
// `--run` makes (#293). It used to route the derived `V` — F1's `*` included, as if the session had
// RECORDED it — through [Decide], which ignores F1 and forces the default instance.
//
// RED on 1c529c5 (it printed `ship:personal`).
func TestADryRunPrintsRunsDecisionForABoundSessionUnderF1(t *testing.T) {
	e, a, repo, path, log := boundToClient(t, "s-dry")
	appendTo(t, path, claudeToolUse("s-dry", repo, 2))
	var out bytes.Buffer
	if err := a.DryRun(&out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "v=[*] decision=deferred") {
		t.Fatalf("the dry run under F1 for a session bound to client: %q — want v=[*] (F1 shown) and --run's deferred", out.String())
	}
	if sum := mustRun(t, a); sum.Refused != 1 || len(spooled(t, e, "client", "s-dry", "main")) != 2 {
		t.Fatalf("CONTROL: --run must defer too: refused=%d\n%s", sum.Refused, log.String())
	}
}

// TestF1DefersOnlyABoundSession pins [DecideRun]'s rule on literal expectations: bound + F1 is
// DEFERRED for every bound instance, whatever either answer would be; an unbound session under F1
// and a bound one without F1 route exactly as before.
func TestF1DefersOnlyABoundSession(t *testing.T) {
	beta := Repo{Scope: "beta-notes"}
	type want struct {
		deferred bool
		instance string
	}
	for _, tc := range []struct {
		name  string
		v     []string
		repo  Repo
		f1    bool
		bound string
		want  want
	}{
		// REGRESSION rows (not deferred on 1c529c5): bound + F1 ⇒ deferred, for each bound instance.
		{"bound to the non-default, empty V", nil, beta, true, "client", want{deferred: true}},
		{"bound to the non-default, its own scope", []string{"beta-notes"}, Repo{}, true, "client", want{deferred: true}},
		{"bound to the default, a scope of the other", []string{"beta-notes"}, Repo{}, true, "personal", want{deferred: true}},
		{"bound to the default, empty V", nil, beta, true, "personal", want{deferred: true}},
		// …whatever the answer without F1 would be — a hold included (it would withdraw the prefix).
		{"bound, a scope routed to no configured instance", []string{"stale-notes"}, Repo{}, true, "client", want{deferred: true}},
		// INVARIANT GUARDS (same answer on 1c529c5): unbound + F1 keeps F1's placement on the default.
		{"unbound, empty V", nil, beta, true, "", want{instance: "personal"}},
		{"unbound, a scope of the other instance", []string{"beta-notes"}, Repo{}, true, "", want{instance: "personal"}},
		// …and bound without F1 routes as before, a move included.
		{"bound, no F1, its repo's instance", nil, beta, false, "client", want{instance: "client"}},
		{"bound, no F1, a scope routes elsewhere", []string{"alpha-notes"}, Repo{}, false, "client", want{instance: "personal"}},
		{"bound to the default, no F1, a scope of the other", []string{"beta-notes"}, Repo{}, false, "personal", want{instance: "client"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := DecideRun(tc.v, tc.f1, tc.repo, repoRouting, RepoRules{Bound: tc.bound})
			if d.Held || d.Deferred != tc.want.deferred || d.Instance != tc.want.instance {
				t.Fatalf("%+v, want deferred=%v instance=%q", d, tc.want.deferred, tc.want.instance)
			}
			if tc.want.deferred && !strings.Contains(d.Reason, "F1") {
				t.Fatalf("a deferral under F1 must say so: %q", d.Reason)
			}
		})
	}
	// One instance: F1 changes no routing answer and there is nowhere to be withdrawn to, so a bound
	// session is not deferred (a deferral would only stall its capture).
	if d := DecideRun(nil, true, Repo{}, oneInstance, RepoRules{Bound: "personal"}); d.Deferred || d.Instance != "personal" {
		t.Fatalf("one instance, bound, under F1: %+v, want personal, not deferred", d)
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
	e, a, repo, path, log := boundToClient(t, "s-t3")
	appendTo(t, path, claudeToolUse("s-t3", repo, 2), claudeToolResult("s-t3", repo, 2, marker))
	mustRun(t, a) // F1 live: deferred, nothing shipped
	if len(spooled(t, e, "client", "s-t3", "main")) != 2 {
		t.Fatalf("CONTROL: the F1 pass must ship nothing\n%s", log.String())
	}
	// A LATER call's ledger line lands; the output above never had one.
	if err := os.WriteFile(filepath.Join(e.ledger, "s-t3.jsonl"), []byte(ledgerLine("s-t3", "recall", "beta-notes")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	mustRun(t, a)
	shipped := false
	for _, r := range spooled(t, e, "client", "s-t3", "main") {
		shipped = shipped || bytes.Contains(r.Rec, []byte(marker))
	}
	if !shipped || !slices.Equal(e.sink.declared["client/s-t3"], []string{"beta-notes"}) {
		t.Fatalf("KNOWN RESIDUAL MOVED: the unledgered output shipped=%v, declared %v (pinned: shipped, declared "+
			"[beta-notes] with no `*`). If a fix closed T3 here, invert this test deliberately\n%s",
			shipped, e.sink.declared["client/s-t3"], log.String())
	}
}
