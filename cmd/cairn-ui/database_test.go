package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The database wiring is 28(c), and what it is FOR is that a deployment which cannot hold
// an invitation says so at startup rather than at the first click.
//
// 🔴 WHAT THESE CASES CAN AND CANNOT SEE, SAID FIRST BECAUSE THE GREEN IS OTHERWISE READ
// TOO WIDE. None of them reaches a real PostgreSQL: the server-backed half of this wiring
// is `internal/pgstore`'s and is measured by the build-tagged tier (`tests/pgtest/run.sh`),
// which refuses rather than skips. What is measured HERE is the program's own decisions —
// the blank policy, the refusal firing before any listener, and which branch each
// configuration lands in — because those are the ones that live in `main` and that no
// package test can reach. A reader wanting "does a redemption work against a real
// database" is in the wrong file; that is the pgtest tier plus rank 9's human.

// TestAWhitespaceDatabaseLineIsRefusedRatherThanReadAsUnset is `databaseDSNDefault`'s blank
// policy, and it is `TestAWhitespaceControlJournalLineIsRefusedRatherThanReadAsUnset`'s
// claim about the second variable this program reads raw.
//
// 🔴 THE HAZARD IS THE SAME AND THE COST IS DIFFERENT, WHICH IS WHY IT IS A SECOND CASE
// RATHER THAN A ROW IN THE FIRST. A whitespace journal line drops the surface onto the
// token-file projection; a whitespace DSN drops it onto a LOCAL FILE session table and no
// invite store at all — every `/invite` page telling its reader the deployment holds no
// invitation store, `/healthz` at 200, and every browser that had a database-backed
// session signed out. The refusal has to name THAT, so its text is what this asserts.
//
// ⚠ THE ARMS THAT ARE **NOT** REFUSED ARE HALF THE POINT, for the reason the journal case
// gives: a guard that refused every non-empty value would satisfy every whitespace arm and
// break every deployment. The absent, empty and real-DSN arms are this case's positive
// controls.
func TestAWhitespaceDatabaseLineIsRefusedRatherThanReadAsUnset(t *testing.T) {
	// A syntactically ordinary DSN that names a database nothing here ever opens. It is
	// the POSITIVE CONTROL's value: what this function judges is the SPELLING, and it must
	// pass a string a deployment would really carry.
	const realDSN = "host=127.0.0.1 port=5432 user=cairn dbname=cairn sslmode=disable"

	for _, arm := range []struct {
		name string
		// set=false is "the variable is not in the environment at all", which is a
		// different input from the empty string and must resolve the same way.
		set       bool
		value     string
		wantErr   bool
		wantValue string
	}{
		{name: "absent", set: false, wantValue: ""},
		{name: "the empty string", set: true, value: "", wantValue: ""},
		{name: "one space", set: true, value: " ", wantErr: true},
		{name: "three spaces", set: true, value: "   ", wantErr: true},
		{name: "tabs and newlines", set: true, value: "\t\n ", wantErr: true},
		// ⚠ AN INVARIANT GUARD, NOT REGRESSION COVERAGE, AND LABELLING IT IS THE POINT —
		// the same row the journal case carries, for the same reason. `strings.TrimSpace`
		// does not strip U+200B, so this value is what makes `identity.ValueReducesToNothing`
		// rather than a fresh `TrimSpace` the predicate: a `TrimSpace` reader calls eight
		// zero-width runes CONTENT and hands them to `lib/pq` as a connection string.
		{name: "zero-width runes", set: true, value: strings.Repeat("\u200b", 8), wantErr: true},
		{name: "a real connection string", set: true, value: realDSN, wantValue: realDSN},
		// 🔴 THE ONE PINNING "RETURNED RAW, NOT TRIMMED". A `strings.TrimSpace` added here
		// for symmetry with the pod's journal reader survives every other row in this file
		// and opens exactly the arrival-path divergence `controlJournalDefault` was written
		// to close: the environment path would connect while `-db-dsn "  <dsn>  "` fails,
		// because `lib/pq` parses a URL DSN with `url.Parse` and a leading space is not a
		// scheme. Nothing else pins this: see the note below on the case that does not exist.
		{name: "a real connection string wearing spaces", set: true,
			value: "  " + realDSN + "  ", wantValue: "  " + realDSN + "  "},
	} {
		t.Run(arm.name, func(t *testing.T) {
			t.Setenv(EnvUIDatabase, arm.value)
			if !arm.set {
				if err := os.Unsetenv(EnvUIDatabase); err != nil {
					t.Fatal(err)
				}
			}

			// This is `main`'s own resolution, reached the way `main` reaches it.
			resolved, err := databaseDSNDefault(os.Getenv)
			if arm.wantErr {
				if err == nil {
					// Say what the green would have MEANT, because the failure is silent.
					t.Fatalf("%q resolved to %q with no refusal, so the flag default stays EMPTY and this "+
						"surface comes up with the session table on a local file and NO invitation store, "+
						"while the operator's manifest says there is a database. A whitespace line is a "+
						"line somebody wrote and this program would discard", arm.value, resolved)
				}
				// The refusal has to name the variable and both halves of what it costs, or
				// an operator reading it cannot tell which line to fix or why it matters.
				for _, want := range []string{EnvUIDatabase, "reduces to nothing", "signed out", "invitation store"} {
					if !strings.Contains(err.Error(), want) {
						t.Errorf("the refusal does not mention %q:\n%s", want, err)
					}
				}
				return
			}

			if err != nil {
				t.Fatalf("POSITIVE CONTROL FAILED: %q was refused (%v). A guard that refuses this admits "+
					"nothing, and every refusal above would be about nothing", arm.value, err)
			}
			if resolved != arm.wantValue {
				t.Fatalf("resolved to %q, want %q", resolved, arm.wantValue)
			}
		})
	}
}

// ⚠ THERE IS NO `TestTheTwoArrivalPathsOfADatabaseLineAgree`, AND ITS ABSENCE IS A
// DELETION RATHER THAN A GAP — recorded because the journal variable HAS one and a reader
// will ask why this one does not. The journal's two arrival paths reach two DIFFERENT
// judges: `controlJournalDefault`'s blank policy on one side and `openAuthority`'s `Stat`
// on the other, which is why they can disagree and why that case carries a measured
// residual. The DSN has ONE judge — both paths hand their string to `openDatabase` and the
// SERVER decides — so a case here could only compare the resolver against itself, or try to
// connect, and "try to connect" in a unit test is a dial that finds a local PostgreSQL on
// one machine and a refusal on the next. The single property such a case would really have
// pinned is "returned RAW, not trimmed", and that is the last row of the case above, where
// it is a direct assertion rather than a relation.

// TestTheProcessRefusesAnUnreachableDatabaseBeforeTheListener is the END of the chain and
// the whole deliverable of 28(c): environment or flag -> `openDatabase` -> refusal ->
// `os.Exit(exitConfig)`, with no listener bound.
//
// 🔴 IT RE-EXECS THE BINARY BECAUSE THE OBSERVABLE IS A PROCESS EXIT, which is the same
// argument `TestTheProcessExitsOnAWhitespaceControlJournalLine` makes: a mutation that
// COMPUTES the refusal and then does not act on it leaves every in-process test green while
// the built binary serves. `sql.Open` validates nothing — a wrong host, a wrong password
// and a database that does not exist are all indistinguishable from success until the first
// query — so without this wiring the failure surfaces at the first sign-in, which is
// precisely the "looks healthy, serves nobody" shape this program's refusals exist against.
//
// 🔴 THE UNREACHABLE ADDRESS IS A PORT THIS TEST JUST CLOSED, NOT A MADE-UP ONE. Binding
// `127.0.0.1:0`, reading the port and closing the listener leaves an address that is
// certain to REFUSE rather than to hang or to resolve somewhere real — so the case is
// deterministic, needs no network, and cannot wander onto somebody else's service. A
// literal port would be a bet on what is not listening on the machine running this.
//
// ⚠ THE ARMS PIN WHICH REFUSAL SPOKE, because `main` carries eleven `os.Exit(exitConfig)`
// sites and a bare exit-code assertion is satisfied by any of them. The whitespace arm is
// refused by the blank policy and the unreachable arm by `openDatabase`, with different
// messages, and both are asserted.
func TestTheProcessRefusesAnUnreachableDatabaseBeforeTheListener(t *testing.T) {
	_, journal := seededJournal(t, credentialLive)
	closedPort := aPortNothingIsListeningOn(t)

	// 🔴 A DISTINCT, IMPROBABLE SECRET — never a realistic-looking credential. This repo is
	// PUBLIC and `AGENTS.md` forbids credential-shaped fixtures; the value only has to be
	// unmistakable in a grep.
	const fixtureSecret = "NOT-A-REAL-PASSWORD-e3b0c44298fc"

	for _, arm := range []struct {
		name string
		// dsn arrives in the ENVIRONMENT, which is the path a deployment uses and the only
		// one the blank policy can refuse.
		dsn string
		// secret is the substring that must NOT appear in the refusal. Empty where the arm's
		// DSN carries none — an arm with no secret cannot measure a leak, and pretending it
		// can is how the previous spelling of this guard passed while the property was broken.
		secret string
		want   string
	}{
		{
			name: "a connection refused",
			dsn: fmt.Sprintf("host=127.0.0.1 port=%d user=cairn dbname=cairn sslmode=disable connect_timeout=5",
				closedPort),
			// No secret: a refused TCP connection carries no DSN in its error, so this arm
			// measures that the REFUSAL fires and nothing about redaction.
			want: "could not be opened",
		},
		{
			// 🔴 A MALFORMED URL CARRYING A PASSWORD — and the MALFORMED half is the whole
			// point, which cost a second vacuous guard to learn. `lib/pq` leaks the DSN only
			// through `*url.Error`, which `net/url.Parse` produces for an UNPARSEABLE URL. A
			// well-formed URL aimed at a closed port parses cleanly, fails at connect, and
			// carries no DSN — so a fixture built that way CANNOT observe the leak. Measured:
			// removing the redactor left this case GREEN with that fixture, exactly as the
			// `user=cairn` spelling it replaced had been. An invalid port is what makes the
			// unsafe shape reachable.
			name:   "a malformed URL DSN carrying a password",
			dsn:    "postgres://cairn:" + fixtureSecret + "@127.0.0.1:not-a-port/cairn?sslmode=disable",
			secret: fixtureSecret,
			want:   "could not be opened",
		},
		{
			// 🔴 THE SECOND ARM IS ALSO THE NEGATIVE CONTROL FOR THE FIRST: a different
			// refusal, the same exit code, a different message. Without it the assertion
			// above is satisfied by a program that refuses every DSN for any reason.
			name: "a line that reduces to nothing",
			dsn:  "   ",
			// No secret in this DSN, so the leak check below is skipped rather than run
			// vacuously against a fragment this arm never contained.
			want: "reduces to nothing",
		},
	} {
		t.Run(arm.name, func(t *testing.T) {
			// 🔴 A DEADLINE, BECAUSE THE FAILURE MODE OF THIS CASE IS A PROCESS THAT DOES
			// NOT EXIT. The mutant it exists to kill makes the child SERVE, and a bare
			// `cmd.Run()` then blocks until the whole suite is killed — a hang reads as
			// infrastructure rather than as a finding.
			//
			// 🔴 20s, AND THE NUMBER IS BOUNDED FROM ABOVE BY THE MUTATION BATTERY RATHER
			// THAN BY THIS TEST. It was 60s, and under the `ui-startup-serves-an-
			// unreachable-database` mutant BOTH arms wait their full deadline — 120s, which
			// is exactly `tests/control_mutants.py`'s per-package timeout. The battery then
			// reported `HARNESS ERROR` instead of a kill: the row scored nothing and the run
			// exited 1 on a mutant its killer would have caught. A deadline generous enough
			// to be safe and long enough to break the harness that reads it is worse than a
			// tighter one. The floor is tiny — a refused connection and a blank-policy
			// refusal are both immediate — so 20s is still ~1000x margin.
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0],
				"-control-journal", journal,
				"-store", t.TempDir(),
				"-token-file", filepath.Join(t.TempDir(), "absent-token"),
				"-session-file", filepath.Join(t.TempDir(), "sessions"),
				"-host", "127.0.0.1", "-port", "0")
			cmd.Env = []string{reexecEnv + "=1", EnvUIDatabase + "=" + arm.dsn}
			body, _ := cmd.CombinedOutput()

			if ctx.Err() != nil {
				t.Fatalf("the process was STILL RUNNING at the deadline, so it did not refuse: it bound a "+
					"listener over a database it cannot reach, and every sign-in and every invite page "+
					"will discover that one request at a time.\n%s", body)
			}
			if cmd.ProcessState == nil {
				t.Fatalf("no process state:\n%s", body)
			}
			if code := cmd.ProcessState.ExitCode(); code != exitConfig {
				t.Fatalf("exit %d, want %d (EX_CONFIG)\n%s", code, exitConfig, body)
			}
			if !strings.Contains(string(body), arm.want) {
				t.Errorf("stderr does not contain %q, so this arm cannot tell which of the eleven "+
					"refusals in `main` spoke:\n%s", arm.want, body)
			}
			// 🔴 AND THE SECRET IS NOT IN THE OUTPUT. A refusal is the single most likely line
			// to be pasted into an issue, and this program tells the operator twice that it
			// does not echo the connection string.
			//
			// 🔴 THIS CHECK USED TO READ `strings.Contains(body, "user=cairn")` AND IT WAS
			// THREE WAYS WRONG — it is kept as a worked example rather than quietly replaced,
			// because every one of the three is a shape that recurs. Its comment claimed the
			// fragment was "in EVERY arm's DSN": arm 2's DSN is `"   "`, so the guard was
			// vacuous there. Arm 1's DSN carried NO PASSWORD at all, so the one arm it did
			// run against could not leak a secret even in principle. And neither arm used the
			// `postgres://` URL form — the ONLY form that leaked, the form a Kubernetes
			// secret carries, and the form `README.md` documents. The guard passed while the
			// property it named was broken: a scanner that recognises only its own textbook
			// example, which is exactly what `AGENTS.md` says to expect.
			//
			// What it asserts now is the SECRET, on an arm whose DSN actually carries one.
			// The narrow, always-safe keyword/value case lives in
			// `internal/pgstore/redact_test.go`, which needs no child process.
			if arm.secret != "" && strings.Contains(string(body), arm.secret) {
				t.Errorf("the refusal ECHOED the password out of the connection string:\n%s", body)
			}
		})
	}
}

// TestWithNoDatabaseTheSurfaceComesUpSayingItHoldsNoInvitation is the POSITIVE CONTROL for
// the case above and a guard in its own right — and it is the only thing that can see the
// typed-nil mistake.
//
// 🔴 WITHOUT THIS, EVERY DATABASE ASSERTION IN THIS FILE IS SATISFIED BY A BINARY THAT
// REFUSES TO START FULL STOP. The refusals above all exit 78; a `main` that exited 78
// unconditionally would pass every one of them. So this arm requires the same binary, with
// the same journal and no DSN, to bind a listener and announce itself.
//
// 🔴 AND IT READS THE STATE SENTENCE, WHICH IS DERIVED FROM THE WIRED OBJECTS RATHER THAN
// FROM THE FLAG. That is what makes it able to catch a defect no other test here can: an
// `inviting` declared as `ui.ControlInviting` instead of the `ui.Inviting` interface is a
// NON-NIL interface holding a zero struct, so `ui.Config.Inviting != nil` is true, the
// `NoInviteStore` branch is never taken, and `GET /invite` nil-panics at the first click —
// on a deployment that has no database and never asked for one. With the sentence derived
// from the object, that mutant makes this child announce `invitations in postgres` with no
// DSN anywhere, and this case goes red before anybody clicks.
//
// ⚠ IT BINDS AN EPHEMERAL PORT (`-port 0`) AND IS KILLED BY CANCELLING ITS CONTEXT, the
// same shape `TestTheRUNNINGBinarySaysSoWhenARecordIsDroppedAfterStartup` uses.
func TestWithNoDatabaseTheSurfaceComesUpSayingItHoldsNoInvitation(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	_, journal := seededJournal(t, credentialLive)
	sessionFile := filepath.Join(t.TempDir(), "sessions")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	child := exec.CommandContext(ctx, self,
		"-control-journal", journal,
		"-store", t.TempDir(),
		"-token-file", filepath.Join(t.TempDir(), "absent-token"),
		"-session-file", sessionFile,
		"-host", "127.0.0.1", "-port", "0")
	// 🔴 A CLEARED ENVIRONMENT, NOT `os.Environ()`. A `CAIRN_UI_DB_DSN` in the runner's own
	// environment would take this child down the database branch and turn the whole case
	// into a measurement of somebody's shell.
	child.Env = []string{reexecEnv + "=1"}
	var body syncBuffer
	// One merged stream, through `startChild`, so the wait below fails the moment the child
	// EXITS rather than polling out its 60 s deadline (which, under a mutant that makes every
	// child refuse, pushed this package towards the battery's `-timeout=2m`).
	c := startChild(t, child, cancel, &body, &body)

	c.waitWithin(t, 60*time.Second, "the surface to come up. If it exited, the refusals this file "+
		"asserts are firing for a configuration that has no database and does not want one — which "+
		"would make every one of them a claim about a binary that refuses everything",
		func() bool { return strings.Contains(body.String(), "serving") })

	line := body.String()
	// The sessions half: the FILE, named, because that path is what an operator has to have
	// mounted.
	if !strings.Contains(line, "sessions in "+sessionFile) {
		t.Errorf("the startup line does not say the session table is the file at %s, so a deployment "+
			"cannot tell from its own logs whether the volume it mounted is the one in use:\n%s",
			sessionFile, line)
	}
	// The invitations half: the honest absence.
	if !strings.Contains(line, "NO invitation store") {
		t.Errorf("the startup line does not say this deployment holds no invitation store. That is "+
			"decided at startup and discovered at the first click otherwise — and if the sentence says "+
			"`invitations in postgres` with no DSN configured, `inviting` is a non-nil interface over a "+
			"zero struct and every /invite request will nil-panic:\n%s", line)
	}
	if strings.Contains(line, "invitations in postgres") {
		t.Errorf("the startup line claims invitations are in postgres, with no connection string "+
			"configured anywhere:\n%s", line)
	}
	// The Team page's link half follows the same rule: no database, no team links.
	if strings.Contains(line, "team links in postgres") {
		t.Errorf("the startup line claims team links are in postgres, with no connection string "+
			"configured anywhere:\n%s", line)
	}
	// And the NOTE about an ignored session file must NOT appear: nothing is ignoring it.
	if strings.Contains(line, "is IGNORED") {
		t.Errorf("the surface announced that -session-file is ignored while it is the only session "+
			"table there is:\n%s", line)
	}

	// 🔴 AND THE FILE IS REALLY THERE, WHICH IS THE POSITIVE CONTROL FOR THE POSTGRES
	// TIER'S MIRROR ASSERTION. `database_pgtest_test.go` proves the DSN branch by showing
	// this path was NEVER CREATED, and that reading is only worth anything because
	// `identity.OpenFileSessionStore` creates it EAGERLY (`MkdirAll` + `O_CREATE`) on this
	// branch. An absence is evidence of nothing until the presence has been watched.
	if _, statErr := os.Stat(sessionFile); statErr != nil {
		t.Errorf("%s was not created (%v), so the file session store was never opened on the branch "+
			"that has no database — and the Postgres tier's 'this path does not exist' assertion is "+
			"then satisfied by a path nothing ever creates, on either branch", sessionFile, statErr)
	}
}

// aPortNothingIsListeningOn binds an ephemeral port and gives it back closed.
//
// 🔴 IT IS A MEASURED ADDRESS RATHER THAN A CHOSEN NUMBER, and the difference is whether
// the case is deterministic. A hard-coded port is a bet that nothing on the machine running
// the suite is listening there — and the failure when that bet is lost is not a red test,
// it is a connection that SUCCEEDS against somebody else's service and then fails as a
// protocol error with a message nobody can act on. ⚠ There is a window between the close
// and the child's dial in which something could take the port; it is the standard one, and
// the direction is safe — a taken port produces a noisy failure here, never a false green,
// because the assertion is that the process REFUSED.
func aPortNothingIsListeningOn(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("no ephemeral port could be bound, so this case has no unreachable address to point at: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		t.Fatalf("the probe listener did not close, so %d may still be ACCEPTING and the refusal this "+
			"case asserts would be about a successful connection: %v", port, err)
	}
	// A dial must now fail, or the address is not the unreachable one this case needs.
	conn, dialErr := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), time.Second)
	if dialErr == nil {
		_ = conn.Close()
		t.Fatalf("POSITIVE CONTROL FAILED: something is listening on 127.0.0.1:%d after the probe "+
			"listener closed, so a 'connection refused' case pointed there would be measuring a "+
			"successful connection to somebody else's service", port)
	}
	var opErr *net.OpError
	if !errors.As(dialErr, &opErr) {
		t.Fatalf("the probe dial failed with %v, which is not a network error — the address cannot be "+
			"vouched for as refusing", dialErr)
	}
	return port
}
