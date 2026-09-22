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

// issueFlagsFor builds the mode's flag values WITHOUT touching the process's global flag
// set, for the reason `flagsFor` records one file over: `registerIssueCredentialFlags`
// calls `flag.Bool`/`flag.String` on the default set, which `TestMain` has already had
// `flag.Parse` run over, so registering again panics with "flag redefined".
//
// ⚠ SO THIS FILE DOES NOT PROVE THE FLAGS ARE REGISTERED.
// `TestTheBinaryActuallyDispatchesIssueCredential` runs the real `main` for that half.
func issueFlagsFor(kind, principal, label, narrow string) *issueCredentialFlags {
	enabled := true
	return &issueCredentialFlags{
		enabled:       &enabled,
		principalKind: &kind,
		principal:     &principal,
		label:         &label,
		narrowScopes:  &narrow,
	}
}

// aJournalWithAnOwner writes the world every case below issues against, using the other
// operator command rather than a hand-built journal — so these tests exercise the sequence
// an operator actually follows (`-create-user`, then `-issue-credential`) rather than a
// fixture only this file knows how to build.
func aJournalWithAnOwner(t *testing.T) (journal string, env map[string]string, made control.Provisioned) {
	t.Helper()
	journal = filepath.Join(t.TempDir(), "control.journal")
	env = map[string]string{EnvControlJournal: journal}

	store, err := control.OpenFileStore(journal)
	if err != nil {
		t.Fatalf("opening the journal: %v", err)
	}
	made, err = control.ProvisionUser(context.Background(), store, control.NewUser{
		Provider: "notes-idp", Subject: "subject-0001", Email: "rowan@notes.example.test",
		ProjectName: "quarry", ScopeNames: []string{"quarry-notes"},
		At: time.Date(2000, 6, 1, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("provisioning the owner: %v", err)
	}
	return journal, env, made
}

// TestTheTokenIsALONEOnStdoutAndTheProseIsOnStderr is the stream-split claim, and it is
// the one an operator's `> token` redirection depends on.
//
// 🔴 BOTH HALVES ARE LOAD-BEARING AND THEY FAIL DIFFERENTLY. Prose leaking onto stdout
// produces a "token file" the pod then refuses to parse, and the operator's remedy is to
// hand-edit a file holding a live secret. The token leaking onto stderr re-stages it in
// exactly the stream an operator leaves scrolling and a CI job archives — the stream the
// warnings themselves say not to put it in.
func TestTheTokenIsALONEOnStdoutAndTheProseIsOnStderr(t *testing.T) {
	journal, env, made := aJournalWithAnOwner(t)
	var out, errOut bytes.Buffer

	code := runIssueCredential(env, issueFlagsFor("user", string(made.User), "rowan laptop", ""), &out, &errOut)
	if code != 0 {
		t.Fatalf("exit %d, want 0:\nstderr: %s", code, errOut.String())
	}

	// Exactly one line on stdout, and that line is the whole token.
	lines := strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
	if len(lines) != 1 {
		t.Fatalf("stdout carries %d lines, want exactly one (the token):\n%q", len(lines), out.String())
	}
	token := lines[0]
	// 43 as a LITERAL rather than `control.TokenChars`: deriving the expectation from the
	// constant under test would make this true for any value of it.
	if len(token) != 43 {
		t.Fatalf("stdout's line is %d characters, want a 43-character token — anything else means "+
			"prose or a prefix reached the stream a redirection captures", len(token))
	}

	stderr := errOut.String()
	if strings.Contains(stderr, token) {
		t.Fatal("THE TOKEN IS ON STDERR. The stream split is the whole point: stderr is what an " +
			"operator leaves scrolling and what a CI job archives.")
	}
	// And the prose that has to be there, pinned by claim rather than by wording where
	// possible — but the "only time" sentence is pinned because it is the one an operator
	// acts on, and losing it silently turns a lost token into a support question.
	for _, want := range []string{
		"cairn-control: issued credential=crd_",
		"ONLY TIME IT WILL EVER BE SHOWN",
		"RE-STAGES IT",
		"digest=",
	} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr does not carry %q:\n%s", want, stderr)
		}
	}

	// The credential is real: it authenticates against the journal on disk, and it reaches
	// the scope its principal owns. Without this the test above is satisfied by a command
	// that prints 43 random characters and writes nothing.
	reread, err := control.OpenFileStore(journal)
	if err != nil {
		t.Fatalf("re-opening the journal: %v", err)
	}
	m, err := reread.Model(context.Background())
	if err != nil {
		t.Fatalf("replaying: %v", err)
	}
	p, auth, err := control.Authenticate(m, token)
	if err != nil {
		t.Fatalf("the printed token does not authenticate against the journal: %v", err)
	}
	if p.ID != made.User {
		t.Fatalf("the token authenticates as %s, want the provisioned owner %s", p.ID, made.User)
	}
	if !auth.Allows(made.Scopes[0].ID, control.VerbRead) {
		t.Fatalf("the credential cannot read %s, the scope its principal owns", made.Scopes[0].ID)
	}
	// The reach line is what tells an operator the credential is not inert.
	if !strings.Contains(stderr, "reads 1 scope(s)") {
		t.Errorf("stderr does not report what the credential reaches:\n%s", stderr)
	}

	// 🔴 AND THE JOURNAL FILE ITSELF HOLDS NO TOKEN. Asserted here as well as in
	// `internal/control` because this is the layer that has the token in hand and could
	// put it anywhere — a label, an actor, a second line.
	body, err := os.ReadFile(journal)
	if err != nil {
		t.Fatalf("reading the journal: %v", err)
	}
	if strings.Contains(string(body), token) {
		t.Fatal("the raw token is in the journal file")
	}
}

// TestAnIssueThatCannotSucceedIsRefusedAndWritesNothing.
//
// Each arm is a distinct refusal with a distinct message, because an operator reading
// "refused" and nothing else cannot tell a typo from a broken mount — the ruling
// `exitConfig`'s comment records for this program having exactly one failure code.
func TestAnIssueThatCannotSucceedIsRefusedAndWritesNothing(t *testing.T) {
	journal, env, made := aJournalWithAnOwner(t)
	before, err := os.ReadFile(journal)
	if err != nil {
		t.Fatalf("reading the journal: %v", err)
	}

	for _, arm := range []struct {
		name  string
		env   map[string]string
		flags *issueCredentialFlags
		want  string
	}{
		{
			name:  "no control journal configured",
			env:   map[string]string{},
			flags: issueFlagsFor("user", string(made.User), "", ""),
			want:  "needs a control journal",
		},
		{
			name:  "no principal",
			env:   env,
			flags: issueFlagsFor("user", "", "", ""),
			want:  "-principal is required",
		},
		{
			name:  "a principal kind the model does not define",
			env:   env,
			flags: issueFlagsFor("robot", "usr_whatever", "", ""),
			want:  "is not a principal kind",
		},
		{
			// The journal would refuse this too, as "subject user prj_… does not exist" —
			// which reads as a missing record rather than as a mismatched flag pair.
			name:  "a project id under -principal-kind user",
			env:   env,
			flags: issueFlagsFor("user", string(made.Project), "", ""),
			want:  "expects an id starting usr_",
		},
		{
			name:  "a principal the journal does not hold",
			env:   env,
			flags: issueFlagsFor("user", "usr_nobody0000000000000000", "", ""),
			want:  "no such principal",
		},
		{
			name:  "a narrowing named by display name rather than id",
			env:   env,
			flags: issueFlagsFor("user", string(made.User), "", "quarry-notes"),
			want:  "is not a scope id",
		},
		{
			// 🔴 NOT A WAY TO SAY "SEES NOTHING". `nil` and an empty non-nil narrowing are
			// opposites in the model, and on a command line they would be one typed
			// character apart.
			name:  "a narrowing that is only separators",
			env:   env,
			flags: issueFlagsFor("user", string(made.User), "", " , , "),
			want:  "has no ids in it",
		},
	} {
		t.Run(arm.name, func(t *testing.T) {
			var out, errOut bytes.Buffer
			code := runIssueCredential(arm.env, arm.flags, &out, &errOut)
			if code != exitConfig {
				t.Fatalf("exit %d, want %d:\nstdout: %q\nstderr: %s", code, exitConfig, out.String(), errOut.String())
			}
			if out.Len() != 0 {
				t.Fatalf("a REFUSED issue put %q on stdout — stdout is the token stream, so anything "+
					"on it is a value an operator's redirection captures as a credential", out.String())
			}
			if !strings.Contains(strings.ToLower(errOut.String()), strings.ToLower(arm.want)) {
				t.Fatalf("refusal does not name %q:\n%s", arm.want, errOut.String())
			}
		})
	}

	after, err := os.ReadFile(journal)
	if err != nil {
		t.Fatalf("re-reading the journal: %v", err)
	}
	if string(before) != string(after) {
		t.Fatal("a refused issue changed the journal, which is append-only: a record written by a " +
			"command that reported failure is a credential nobody knows exists")
	}
}

// TestACredentialThatReachesNothingIsAnnouncedRatherThanRefused.
//
// 🔴 FROM OUTSIDE THE POD THIS STATE IS INDISTINGUISHABLE FROM A BROKEN CREDENTIAL, which
// is the same argument `-create-user` records for a user with no scopes. It is a
// legitimate intermediate state — and for a PROJECT principal it is the DEFAULT, because a
// project is not a member of itself — so it is a loud line rather than a refusal.
func TestACredentialThatReachesNothingIsAnnouncedRatherThanRefused(t *testing.T) {
	_, env, made := aJournalWithAnOwner(t)
	var out, errOut bytes.Buffer

	code := runIssueCredential(env, issueFlagsFor("project", string(made.Project), "ci", ""), &out, &errOut)
	if code != 0 {
		t.Fatalf("exit %d, want 0 — a credential that reaches nothing is announced, not refused:\n%s", code, errOut.String())
	}
	if out.Len() == 0 {
		t.Fatal("no token was printed, so the announcement is about a credential that was not issued")
	}
	if !strings.Contains(errOut.String(), "can reach NOTHING") {
		t.Fatalf("stderr does not warn that the credential is inert:\n%s", errOut.String())
	}
	if !strings.Contains(errOut.String(), "not a member of itself") {
		t.Fatalf("the warning does not name the reason a PROJECT principal reaches nothing by "+
			"default, which is the case an operator hits first:\n%s", errOut.String())
	}
}

// TestAnUnknownNarrowScopeIdWarnsRatherThanRefusing.
//
// 🔴 A WARNING, BECAUSE `Narrow` INTERSECTS. An id that names no scope confers nothing and
// can never confer anything — scope ids are random rather than derived, so it will not
// start matching later — so it is harmless to authorization and badly misleading to a
// human. Refusing would also put a validity claim at ISSUE time, which is exactly what
// `control.Narrow`'s own comment rules out.
func TestAnUnknownNarrowScopeIdWarnsRatherThanRefusing(t *testing.T) {
	_, env, made := aJournalWithAnOwner(t)
	var out, errOut bytes.Buffer

	code := runIssueCredential(env,
		issueFlagsFor("user", string(made.User), "", "scp_nothingnamesthisid0000"), &out, &errOut)
	if code != 0 {
		t.Fatalf("exit %d, want 0:\n%s", code, errOut.String())
	}
	if !strings.Contains(errOut.String(), "which this journal holds no scope for") {
		t.Fatalf("stderr does not warn about the unknown scope id:\n%s", errOut.String())
	}
	if !strings.Contains(errOut.String(), "can reach NOTHING") {
		t.Fatalf("a credential narrowed to an id nothing matches reaches nothing, and stderr does "+
			"not say so:\n%s", errOut.String())
	}

	// The positive control on this arm: a KNOWN id must not produce the warning, or the
	// check is simply warning about every narrowing.
	var out2, errOut2 bytes.Buffer
	if code := runIssueCredential(env,
		issueFlagsFor("user", string(made.User), "", string(made.Scopes[0].ID)), &out2, &errOut2); code != 0 {
		t.Fatalf("exit %d on a known scope id:\n%s", code, errOut2.String())
	}
	if strings.Contains(errOut2.String(), "holds no scope for") {
		t.Fatalf("a KNOWN scope id produced the unknown-id warning:\n%s", errOut2.String())
	}
	if !strings.Contains(errOut2.String(), "reads 1 scope(s)") {
		t.Fatalf("a credential narrowed to a scope its principal owns must still reach it:\n%s", errOut2.String())
	}
}

// TestTheBinaryActuallyDispatchesIssueCredential is the half the in-process tests cannot
// reach: that `main` REGISTERS the flags and ROUTES to the mode.
//
// 🔴 WITHOUT IT EVERY TEST ABOVE WOULD PASS WITH THE DISPATCH DELETED — the same claim
// `TestTheBinaryActuallyDispatchesCreateUser` makes, and the reason both exist rather than
// one covering the other: a mode reached by no dispatch is a capability that does not exist,
// and `runIssueCredential` called directly cannot tell.
func TestTheBinaryActuallyDispatchesIssueCredential(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	journal := filepath.Join(t.TempDir(), "control.journal")

	run := func(args ...string) (int, string, string) {
		t.Helper()
		// 🔴 UNDER A DEADLINE, BECAUSE THE FAILURE SHAPE IS "IT FELL THROUGH AND SERVED".
		// An `-issue-credential` that reached the server path would not return, and a hanging
		// test is read as infrastructure rather than as a finding.
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		child := exec.CommandContext(ctx, self, args...)
		child.Env = []string{
			runServerEnv + "=1",
			testRefreshEnv + "=" + testRefreshPeriod.String(),
			EnvControlJournal + "=" + journal,
		}
		// 🔴 THE STREAMS ARE CAPTURED SEPARATELY, NOT COMBINED, because the claim under test
		// IS the split. `CombinedOutput` would make a token on stderr indistinguishable from
		// a token on stdout — which is the one thing this mode must never get wrong.
		var out, errOut bytes.Buffer
		child.Stdout = &out
		child.Stderr = &errOut
		_ = child.Run()
		if ctx.Err() != nil {
			t.Fatalf("the child did not exit within 20s — `%v` did not take the issue-credential path:\n%s", args, errOut.String())
		}
		if child.ProcessState == nil {
			t.Fatalf("no process state for `%v`:\n%s", args, errOut.String())
		}
		return child.ProcessState.ExitCode(), out.String(), errOut.String()
	}

	code, out, errOut := run("-create-user", "-provider", "notes-idp", "-subject", "subject-0001",
		"-project", "quarry", "-scopes", "quarry-notes")
	if code != 0 {
		t.Fatalf("`-create-user` exited %d:\n%s", code, errOut)
	}
	// `-create-user` puts its `cairn-control: created user=…` line on STDOUT — the ids are
	// what an operator pipes there. This mode's stdout belongs to the token instead; the
	// divergence is argued at `runIssueCredential`.
	user := principalIDFrom(t, out)
	if user == "" {
		t.Fatalf("could not find the created user id on `-create-user`'s stdout:\n%s", out)
	}

	code, out, errOut = run("-issue-credential", "-principal", user, "-label", "rowan laptop")
	if code != 0 {
		t.Fatalf("`-issue-credential` exited %d:\n%s", code, errOut)
	}
	token := strings.TrimRight(out, "\n")
	if len(token) != 43 {
		t.Fatalf("the child put %d characters on stdout, want a 43-character token:\n%q", len(token), out)
	}
	if strings.Contains(errOut, token) {
		t.Fatal("the child echoed the token on stderr as well as stdout")
	}

	// 🔴 THREE MODES, PAIRWISE, BECAUSE THE LEDGER IN `main` IS WHAT REPLACED THREE `&&`
	// CHECKS AND A LEDGER CAN BE WRONG IN A WAY A PAIR CANNOT: it could refuse the pair it
	// was written for and admit the one it was not.
	for _, combo := range [][]string{
		{"-routes", "-issue-credential", "-principal", user},
		{"-create-user", "-issue-credential", "-principal", user, "-provider", "x", "-subject", "y", "-project", "z"},
		{"-routes", "-create-user", "-provider", "x", "-subject", "y", "-project", "z"},
	} {
		code, out, errOut := run(combo...)
		if code != exitConfig {
			t.Errorf("`%v` exited %d, want %d:\n%s", combo, code, exitConfig, errOut)
		}
		if out != "" {
			t.Errorf("`%v` put %q on stdout while refusing — on this binary stdout is where a token "+
				"goes, so anything there is captured as a credential", combo, out)
		}
		if !strings.Contains(errOut, "are both/all set") {
			t.Errorf("`%v` did not refuse with the mode-conflict message:\n%s", combo, errOut)
		}
	}

	// THE POSITIVE CONTROL on this harness: `-routes` ALONE still prints the ledger and
	// exits 0. Without it every refusal above is indistinguishable from a binary that
	// refuses every flag combination.
	code, out, errOut = run("-routes")
	if code != 0 || !strings.Contains(out, "GET recall") {
		t.Fatalf("`-routes` alone must still print the ledger: exit %d\nstdout: %s\nstderr: %s", code, out, errOut)
	}
}

// principalIDFrom pulls a `usr_…` id out of a captured stream.
//
// A helper rather than a regexp inline, so the two places that need it cannot drift into
// two different ideas of what an id looks like.
func principalIDFrom(t *testing.T, body string) string {
	t.Helper()
	for _, field := range strings.Fields(body) {
		field = strings.Trim(field, `",`)
		if after, found := strings.CutPrefix(field, "user="); found {
			return after
		}
		if strings.HasPrefix(field, "usr_") {
			return field
		}
	}
	return ""
}
