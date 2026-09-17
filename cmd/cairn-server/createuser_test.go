package main

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ZacxDev/cairn/internal/control"
)

// flagsFor builds the mode's flag values WITHOUT touching the process's global flag set.
//
// ⚠ `registerCreateUserFlags` CALLS `flag.Bool`/`flag.String` ON THE DEFAULT SET, which
// `TestMain` has already had `flag.Parse` run over — registering again panics with "flag
// redefined". So the values are constructed directly here. That means this file does NOT
// prove the flags are registered; `TestTheBinaryActuallyDispatchesCreateUser` below runs
// the real `main` for exactly that half.
func flagsFor(provider, subject, email, project, scopes string) *createUserFlags {
	enabled := true
	return &createUserFlags{
		enabled:  &enabled,
		provider: &provider,
		subject:  &subject,
		email:    &email,
		project:  &project,
		scopes:   &scopes,
	}
}

// TestABlankControlJournalIsRefusedRatherThanReadAsUnset is the gate on the declared blank
// policy of the one setting this slice adds.
//
// 🔴 THE HAZARD IS THAT UNSET IS *PERMISSIVE IN THE SENSE THAT MATTERS*: it means "resolve
// sessions against the token-file projection", which holds no user any identity provider
// can name — so a blank read as unset gives the operator a pod that starts, fetches its
// JWKS, passes its health check and refuses every sign-in. That is the defect this whole
// change closes, re-entered through a whitespace typo.
//
// 🔴 AND IT IS MEASURED AT TWO SPELLINGS OF "NOTHING", BECAUSE THIS REPOSITORY HAS ALREADY
// PAID FOR ONE OF THEM PASSING. Whitespace is what `strings.TrimSpace` sees; zero-width
// runes are `Cf` and it does not. A second `TrimSpace` here would accept 32 × U+200B as a
// path, which is why the predicate is `identity.ValueReducesToNothing` and not a local
// copy.
func TestABlankControlJournalIsRefusedRatherThanReadAsUnset(t *testing.T) {
	for _, arm := range []struct {
		name  string
		env   map[string]string
		want  string
		fault bool
	}{
		{name: "absent", env: map[string]string{}, want: ""},
		{
			// The EMPTY string is deliberately outside the policy, exactly as
			// `identity.touched` draws the line: a manifest that emits every variable
			// with an empty default is a common shape.
			name: "present and empty",
			env:  map[string]string{EnvControlJournal: ""},
			want: "",
		},
		{name: "one space", env: map[string]string{EnvControlJournal: " "}, fault: true},
		{name: "tabs and newlines", env: map[string]string{EnvControlJournal: "\t\n "}, fault: true},
		{
			// `TrimSpace` calls this content. `unicode.IsGraphic && !IsSpace` does not.
			name:  "zero-width runes only",
			env:   map[string]string{EnvControlJournal: strings.Repeat("\u200b", 8)},
			fault: true,
		},
		{
			name: "a real path, with surrounding whitespace trimmed",
			env:  map[string]string{EnvControlJournal: "  /data/control.journal\t"},
			want: "/data/control.journal",
		},
	} {
		t.Run(arm.name, func(t *testing.T) {
			got, err := controlJournalPath(arm.env)
			if arm.fault {
				if err == nil {
					t.Fatalf("%q was read as UNSET, which silently resolves sessions against an "+
						"authority that holds no provider-named user", arm.env[EnvControlJournal])
				}
				// The refusal must name the variable and quote what was written —
				// whitespace is invisible otherwise.
				if !strings.Contains(err.Error(), EnvControlJournal) {
					t.Fatalf("the refusal does not name the variable: %v", err)
				}
				if got != "" {
					t.Fatalf("a refused value still produced a path %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected refusal: %v", err)
			}
			if got != arm.want {
				t.Fatalf("path = %q, want %q", got, arm.want)
			}
		})
	}
}

// TestCreateUserRefusesWithoutAJournalAndWritesOneWithIt walks the mode's exit codes.
//
// 🔴 THE THREE OUTCOMES ARE THREE DIFFERENT OPERATOR ACTIONS, WHICH IS WHY THEY ARE THREE
// CODES AND NOT ONE. 78 = this deployment has no journal configured, go fix the manifest;
// 65 = the journal refused this request, go fix the arguments; 0 = written.
func TestCreateUserRefusesWithoutAJournalAndWritesOneWithIt(t *testing.T) {
	var out, errOut bytes.Buffer
	good := flagsFor("supabase", "subject-0001", "rowan@notes.example.test", "quarry", "quarry-notes")

	// 78: no journal configured. Refused rather than defaulted to a path, because a
	// default would write somewhere the running pod is not reading.
	if code := runCreateUser(map[string]string{}, good, &out, &errOut); code != exitConfig {
		t.Fatalf("exit = %d with no journal configured, want %d", code, exitConfig)
	}
	if !strings.Contains(errOut.String(), EnvControlJournal) {
		t.Fatalf("the refusal does not name the variable to set: %q", errOut.String())
	}
	if out.Len() != 0 {
		t.Fatalf("a refused creation wrote to stdout: %q", out.String())
	}

	journal := filepath.Join(t.TempDir(), "control.journal")
	env := map[string]string{EnvControlJournal: journal}

	// 0: written.
	out.Reset()
	errOut.Reset()
	if code := runCreateUser(env, good, &out, &errOut); code != 0 {
		t.Fatalf("exit = %d, want 0. stderr: %s", code, errOut.String())
	}
	line := out.String()
	for _, want := range []string{"usr_", "prj_", "scp_", "provider=supabase", "subject=subject-0001", "epoch=4"} {
		if !strings.Contains(line, want) {
			t.Fatalf("the created line does not carry %q: %q", want, line)
		}
	}
	// 🔴 AND IT MUST NOT CARRY A CREDENTIAL, BECAUSE THIS PATH MINTS NONE. A session
	// backend authenticates against the IdP; there is no token here to print, and a
	// printed secret re-stages itself in every transcript that captures the run.
	if strings.Contains(line, "token") || strings.Contains(line, "crd_") {
		t.Fatalf("the created line mentions a credential, and this path issues none: %q", line)
	}

	// 65: the journal refuses a second row for one (provider, subject) pair.
	out.Reset()
	errOut.Reset()
	again := flagsFor("supabase", "subject-0001", "", "quarry-two", "quarry-other")
	if code := runCreateUser(env, again, &out, &errOut); code != exitDataErr {
		t.Fatalf("exit = %d re-creating one person, want %d", code, exitDataErr)
	}
	if !strings.Contains(errOut.String(), "refused") {
		t.Fatalf("the refusal does not say it was refused: %q", errOut.String())
	}

	// And the journal really holds exactly one user afterwards — the exit code alone is a
	// claim about the process, not about the file.
	store, err := control.OpenFileStore(journal)
	if err != nil {
		t.Fatal(err)
	}
	m, err := store.Model(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Users) != 1 || len(m.Scopes) != 1 {
		t.Fatalf("the journal holds %d users and %d scopes, want 1 and 1", len(m.Users), len(m.Scopes))
	}
}

// TestCreateUserSaysSoWhenTheUserItCreatedCanReachNothing.
//
// 🔴 THE STATE IS LEGITIMATE AND INDISTINGUISHABLE FROM THE BUG, WHICH IS WHY IT IS A LINE
// RATHER THAN EITHER SILENCE OR A REFUSAL. A user with no scope authenticates and resolves
// to an EMPTY authorization, so every read answers as if nothing exists — exactly what a
// session got before any journal could be wired. Creating a person before deciding their
// access is a real sequence, so it is allowed and announced.
func TestCreateUserSaysSoWhenTheUserItCreatedCanReachNothing(t *testing.T) {
	journal := filepath.Join(t.TempDir(), "control.journal")
	env := map[string]string{EnvControlJournal: journal}
	var out, errOut bytes.Buffer

	if code := runCreateUser(env, flagsFor("supabase", "s1", "", "quarry", ""), &out, &errOut); code != 0 {
		t.Fatalf("a user with no scopes must be created: %d / %s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "scopes=none") {
		t.Fatalf("the created line does not say the scope set is empty: %q", out.String())
	}
	if !strings.Contains(errOut.String(), "can reach NOTHING") {
		t.Fatalf("no warning for a user who can reach nothing: %q", errOut.String())
	}

	// The negative control: WITH a scope, that warning must be absent. Without this the
	// assertion above would be satisfied by a line printed unconditionally.
	out.Reset()
	errOut.Reset()
	if code := runCreateUser(env, flagsFor("supabase", "s2", "", "quarry-two", "quarry-two-notes"), &out, &errOut); code != 0 {
		t.Fatalf("a user WITH a scope must be created: %d / %s", code, errOut.String())
	}
	if strings.Contains(errOut.String(), "can reach NOTHING") {
		t.Fatalf("the warning fired for a user who has a scope: %q", errOut.String())
	}
}

// TestNoControlJournalMeansNoSessionAuthorityAtAll is the compatibility claim.
//
// 🔴 THE nil MUST BE THE INTERFACE'S OWN nil, NOT A TYPED ONE. `identity.FromEnvironment`
// distinguishes "no journal" from "a journal nobody reads" by `sessions != nil`, and a
// typed nil `*control.Cache` in an interface is NOT nil — so returning one would make every
// existing deployment, which configures no session backend, refuse to start with
// `ErrSessionAuthorityUnread`. That is the worst available outcome: a live pod's auth path
// broken by a feature it does not use.
func TestNoControlJournalMeansNoSessionAuthorityAtAll(t *testing.T) {
	var warnings []string
	warn := func(line string) { warnings = append(warnings, line) }

	sessions, err := openSessionAuthority(context.Background(), map[string]string{}, warn)
	if err != nil {
		t.Fatalf("an unconfigured deployment must not fail: %v", err)
	}
	if sessions != nil {
		t.Fatalf("an unconfigured deployment produced a session authority of type %T — every "+
			"deployment that sets nothing new would then refuse to start", sessions)
	}
	if len(warnings) != 0 {
		t.Fatalf("an unconfigured deployment emitted %d line(s) on the operator stream: %v. "+
			"`tests/dualrun/harness.py` compares the two servers' streams line for line",
			len(warnings), warnings)
	}

	// The positive control on the same call: a CONFIGURED journal does produce one, and
	// warns because it is empty. Without it the nil above is indistinguishable from a
	// function that always returns nil.
	journal := filepath.Join(t.TempDir(), "control.journal")
	sessions, err = openSessionAuthority(context.Background(), map[string]string{EnvControlJournal: journal}, warn)
	if err != nil {
		t.Fatalf("a configured journal must open: %v", err)
	}
	if sessions == nil {
		t.Fatal("a configured journal produced no session authority")
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "NO users") {
		t.Fatalf("an EMPTY journal did not announce itself: %v. `OpenFileStore` creates the file, "+
			"so a typo'd path yields an authority that refuses every sign-in", warnings)
	}
	if got := len(sessions.Model().Users); got != 0 {
		t.Fatalf("the fresh journal projected %d users", got)
	}
}

// TestASessionAuthorityOverAProvisionedJournalSeesTheUser closes the loop between the two
// halves of this slice, in the program that wires them.
//
// ⚠ IT IS THE SEAM, AND IT IS THE ONE THING NEITHER PACKAGE'S OWN TESTS CAN SEE.
// `internal/control` proves the provisioning path writes a world that replays;
// `internal/identity` proves a session resolves against an authority that holds it.
// Whether the authority THIS PROGRAM builds from `$CAIRN_CONTROL_JOURNAL` is the one the
// operator command wrote to is a property of neither.
func TestASessionAuthorityOverAProvisionedJournalSeesTheUser(t *testing.T) {
	journal := filepath.Join(t.TempDir(), "control.journal")
	env := map[string]string{EnvControlJournal: journal}
	var out, errOut bytes.Buffer
	if code := runCreateUser(env, flagsFor("supabase", "subject-0001", "", "quarry", "quarry-notes"), &out, &errOut); code != 0 {
		t.Fatalf("provisioning: %d / %s", code, errOut.String())
	}

	sessions, err := openSessionAuthority(context.Background(), env, func(string) {})
	if err != nil {
		t.Fatalf("opening the session authority over the journal just written: %v", err)
	}
	model := sessions.Model()
	user, held := model.UserByProviderSubject("supabase", "subject-0001")
	if !held {
		t.Fatal("the authority this program builds does not hold the user this program created")
	}
	principal, known := model.PrincipalFor(control.KindUser, user.ID)
	if !known {
		t.Fatal("the created user resolves to no principal")
	}
	auth := control.Resolve(model, principal)
	if !auth.VisibleScopes(control.VerbRead).Allows("quarry-notes") {
		t.Fatal("the provisioned user reaches no scope through the authority this program serves — " +
			"an empty authorization is what every deployment produced before this slice")
	}
}

// TestTheBinaryActuallyDispatchesCreateUser is the half the in-process tests cannot reach:
// that `main` REGISTERS the flag and ROUTES to the mode.
//
// 🔴 WITHOUT IT EVERY TEST ABOVE WOULD PASS WITH THE DISPATCH DELETED. `runCreateUser` is
// called directly there, so a `main` that never looked at `-create-user` — or that fell
// through into the server path — would leave them all green and the capability
// unreachable. This runs the real program.
func TestTheBinaryActuallyDispatchesCreateUser(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	journal := filepath.Join(t.TempDir(), "control.journal")

	run := func(args ...string) (int, string) {
		t.Helper()
		// 🔴 UNDER A DEADLINE, BECAUSE THE FAILURE SHAPE IS "IT FELL THROUGH AND SERVED".
		// A `-create-user` that reached the server path would not return, and a hanging
		// test is read as infrastructure rather than as a finding.
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		child := exec.CommandContext(ctx, self, args...)
		child.Env = []string{
			runServerEnv + "=1",
			testRefreshEnv + "=" + testRefreshPeriod.String(),
			EnvControlJournal + "=" + journal,
		}
		body, _ := child.CombinedOutput()
		if ctx.Err() != nil {
			t.Fatalf("the child did not exit within 20s — `%v` did not take the create-user path:\n%s", args, body)
		}
		if child.ProcessState == nil {
			t.Fatalf("no process state for `%v`:\n%s", args, body)
		}
		return child.ProcessState.ExitCode(), string(body)
	}

	code, body := run("-create-user", "-provider", "supabase", "-subject", "subject-0001",
		"-project", "quarry", "-scopes", "quarry-notes")
	if code != 0 {
		t.Fatalf("`-create-user` exited %d:\n%s", code, body)
	}
	if !strings.Contains(body, "cairn-control: created user=usr_") {
		t.Fatalf("the child did not print a created line:\n%s", body)
	}
	if _, err := os.Stat(journal); err != nil {
		t.Fatalf("the child reported success and wrote no journal: %v", err)
	}

	// 🔴 TWO MODES AT ONCE IS A REFUSAL. Checking `-routes` first and returning would
	// print the ledger and silently NOT create the user — exit 0, plausible output, and an
	// operator who believes a person now has access.
	code, body = run("-routes", "-create-user", "-provider", "supabase", "-subject", "subject-0002",
		"-project", "quarry-two")
	if code != exitConfig {
		t.Fatalf("`-routes -create-user` exited %d, want %d:\n%s", code, exitConfig, body)
	}
	if strings.Contains(body, "created user=") {
		t.Fatalf("`-routes -create-user` created a user anyway:\n%s", body)
	}

	// The positive control on this harness: `-routes` ALONE still prints the ledger and
	// exits 0. Without it the refusal above is indistinguishable from a binary that
	// refuses every flag combination.
	code, body = run("-routes")
	if code != 0 || !strings.Contains(body, "GET recall") {
		t.Fatalf("`-routes` alone must still print the ledger: exit %d\n%s", code, body)
	}
}
