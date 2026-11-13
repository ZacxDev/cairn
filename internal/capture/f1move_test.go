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

	"github.com/ZacxDev/cairn/internal/client"
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
// The same flip follows a `cairn` mention that writes no ledger line at all (a non-served
// invocation) for as long as it takes the session's first served call to land. Synthetic
// throughout: scopes `*-notes`, aliases `personal` (default) and `client`.

// claudeToolUse is a Claude Code assistant record whose tool input names the program — F1's
// evidence — and nothing else: no store header, so no scope is derived from it.
func claudeToolUse(id, cwd string, i int) string {
	c, _ := json.Marshal(cwd)
	return fmt.Sprintf(`{"type":"assistant","sessionId":%q,"cwd":%s,"message":{"role":"assistant","content":`+
		`[{"type":"tool_use","id":"toolu_%d","name":"Bash","input":{"command":"cairn recall"}}]}}`, id, c, i)
}

// TestF1DoesNotMoveABoundSessionAndSoCannotFlipItIntoAHold is THE regression, end to end through
// `Agent.Run`, with the sink keeping a withdrawal as a final deletion exactly as the pod does.
//
// RED on 1c529c5: run 2 withdraws the session from `client` and re-ships it to `personal`; run 3
// then routes it back to `client`, whose probe answers 410, and the session is HELD on `personal`.
func TestF1DoesNotMoveABoundSessionAndSoCannotFlipItIntoAHold(t *testing.T) {
	e := repoEnv(t)
	e.sink.tombstone = true
	repo := gitRepo(t, t.TempDir(), "beta-notes", "")
	path := claudeIn(t, e, "s-f1", repo, 2)
	var log bytes.Buffer
	a := newAgent(t, e, repoRouting, &fakeRunner{}, &log)

	// Run 1: no scope recorded, the repo is beta-notes (table: client) — bound to client.
	mustRun(t, a)
	mustBeEmptyV(t, a, "s-f1")
	if ss := a.State.Sessions["s-f1"]; ss.Instance != "client" || len(spooled(t, e, "client", "s-f1", "main")) != 2 {
		t.Fatalf("CONTROL: run 1 must bind the empty-V session to client by its repo and ship both records: %+v\n%s", ss, log.String())
	}

	// Runs 2 and 2b: the transcript names the program and the ledger file does not exist yet — F1 is
	// live on BOTH passes. The session must stay bound to client, and keep shipping there.
	for i, run := range []string{"2", "2b"} {
		appendTo(t, path, claudeToolUse("s-f1", repo, 2+i))
		if _, err := os.Stat(filepath.Join(e.ledger, "s-f1.jsonl")); !os.IsNotExist(err) {
			t.Fatalf("CONTROL: run %s needs NO ledger file, so that F1 is live: %v", run, err)
		}
		mustRun(t, a)
		ss := a.State.Sessions["s-f1"]
		if !ss.Evidence.NamesProgram || len(ss.V) != 0 {
			t.Fatalf("CONTROL: run %s must see F1's evidence and still no recorded scope: %+v", run, ss)
		}
		if d := DecideRun(ss.V, true, Repo{Scope: "beta-notes"}, repoRouting, RepoRules{}); d.Instance != "personal" {
			t.Fatalf("CONTROL: for an UNBOUND session this F1 answers personal, got %+v — without that the case proves nothing", d)
		}
		if ss.Held || ss.Instance != "client" {
			t.Fatalf("run %s (F1 live): bound to %q, held %v — F1's one-run `*` MOVED a session bound to client\n%s",
				run, ss.Instance, ss.Held, log.String())
		}
		if slices.Contains(e.sink.withdrawn["client"], "s-f1") || e.sink.has("personal", "s-f1") {
			t.Fatalf("run %s (F1 live): withdrawn from client %v, anything on personal %v — want neither\n%s",
				run, e.sink.withdrawn["client"], e.sink.has("personal", "s-f1"), log.String())
		}
		if got := len(spooled(t, e, "client", "s-f1", "main")); got != 3+i {
			t.Fatalf("run %s (F1 live): client holds %d main records, want %d — the session must keep shipping, not stall",
				run, got, 3+i)
		}
	}

	// Run 3: the client's ledger line lands, naming the repo's own scope. Nothing moves and nothing is held.
	if err := os.WriteFile(filepath.Join(e.ledger, "s-f1.jsonl"), []byte(ledgerLine("s-f1", "recall", "beta-notes")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	appendTo(t, path, claudeLine("s-f1", repo, 4))
	mustRun(t, a)
	ss := a.State.Sessions["s-f1"]
	if ss.Held || ss.Instance != "client" || !slices.Equal(ss.V, []string{"beta-notes"}) {
		t.Fatalf("run 3 (ledger landed): bound to %q, held %v (%s), V %v — want client, not held, [beta-notes]\n%s",
			ss.Instance, ss.Held, ss.HeldReason, ss.V, log.String())
	}
	if strings.Contains(log.String(), "HOLD") || len(e.sink.withdrawn["client"]) != 0 || len(e.sink.withdrawn["personal"]) != 0 {
		t.Fatalf("run 3: a HOLD or a withdrawal was logged (withdrawn %v)\n%s", e.sink.withdrawn, log.String())
	}
	if got := len(spooled(t, e, "client", "s-f1", "main")); got != 5 || e.sink.has("personal", "s-f1") {
		t.Fatalf("run 3: client holds %d main records (want 5), personal holds any: %v", got, e.sink.has("personal", "s-f1"))
	}
}

// TestADryRunPrintsRunsDecisionForABoundSessionUnderF1: `--dry-run` claims to print the decision
// `--run` makes (#293). It used to route the derived `V` — F1's `*` included, as if the session had
// RECORDED it — through [Decide], which forces the default instance whatever the session is bound
// to, so with the rule above it would print `ship:personal` for a session `--run` keeps on client.
//
// RED before the dry run routed F1 through [DecideRun] like `--run` does.
func TestADryRunPrintsRunsDecisionForABoundSessionUnderF1(t *testing.T) {
	e := repoEnv(t)
	repo := gitRepo(t, t.TempDir(), "beta-notes", "")
	path := claudeIn(t, e, "s-dry", repo, 1)
	a := newAgent(t, e, repoRouting, &fakeRunner{}, &bytes.Buffer{})
	mustRun(t, a)
	if a.State.Sessions["s-dry"].Instance != "client" {
		t.Fatal("CONTROL: run 1 must bind the session to client by its repo")
	}
	appendTo(t, path, claudeToolUse("s-dry", repo, 1))
	var out bytes.Buffer
	if err := a.DryRun(&out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "v=[*] decision=ship:client") {
		t.Fatalf("the dry run under F1 for a session bound to client: %q — want v=[*] (F1 shown) and --run's ship:client", out.String())
	}
	mustRun(t, a)
	if ss := a.State.Sessions["s-dry"]; ss.Instance != "client" || ss.Held {
		t.Fatalf("CONTROL: --run must agree with the dry run: %+v", ss)
	}
}

// TestF1CanOnlyKeepABoundSessionWhereItIs pins [DecideRun]'s rule for a BOUND session under F1:
// F1 never changes the answer away from the instance the session is bound to — in EITHER direction —
// and a move happens only when the answer WITHOUT F1 leaves that instance too. An unbound session is
// placed exactly as before.
func TestF1CanOnlyKeepABoundSessionWhereItIs(t *testing.T) {
	beta := Repo{Scope: "beta-notes"}
	threeInstances := client.Routing{
		Instances:    []client.Instance{{Alias: "personal"}, {Alias: "client"}, {Alias: "gamma"}},
		Routes:       map[string]string{"alpha-notes": "personal", "beta-notes": "client", "gamma-notes": "gamma"},
		RoutesSource: "the test's table",
	}
	for _, tc := range []struct {
		name     string
		v        []string
		repo     Repo
		bound    string
		instance string
		routing  client.Routing
	}{
		// REGRESSION rows (red on 1c529c5). The observed flip, step 1: placed by its repo, F1 must not
		// move it to the default.
		{"empty V, bound to the repo's instance", nil, beta, "client", "client", repoRouting},
		// The same with a recorded scope that routes to the bound instance (a header, no ledger yet).
		{"a scope of the bound instance", []string{"beta-notes"}, Repo{}, "client", "client", repoRouting},
		// INVARIANT GUARDS below (green on 1c529c5 too): they pin that the fix is not WIDER than the
		// rule. The other direction: F1 placed it on the default while unbound; ignoring F1 now would
		// move it OFF the default while F1 still holds, and the ledger landing could route it back.
		{"F1 placed it on the default", []string{"beta-notes"}, Repo{}, "personal", "personal", repoRouting},
		// Unbound: unchanged — F1 still places a NEW session on the default instance.
		{"unbound, empty V", nil, beta, "", "personal", repoRouting},
		{"unbound, a scope of the other instance", []string{"beta-notes"}, Repo{}, "", "personal", repoRouting},
		// A move NOT caused by F1 still happens: without F1 the answer leaves `client` too.
		{"the recorded scope routes elsewhere", []string{"alpha-notes"}, Repo{}, "client", "personal", repoRouting},
		// …and when both answers leave the bound instance AND disagree with each other, it goes to the
		// answer WITH F1 (the default) — not to the one without it. Only three instances can say so.
		{"both answers leave, and disagree", []string{"beta-notes"}, Repo{}, "gamma", "personal", threeInstances},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := DecideRun(tc.v, true, tc.repo, tc.routing, RepoRules{Bound: tc.bound})
			if d.Held || d.Deferred || d.Instance != tc.instance {
				t.Fatalf("%+v, want instance %s", d, tc.instance)
			}
			if d.DeclaresStar() {
				t.Fatalf("F1 alone must never declare `*`: %+v", d)
			}
		})
	}
}
