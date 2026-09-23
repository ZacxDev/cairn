package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ZacxDev/cairn/internal/control"
)

// syncBuffer collects a child's stderr from the goroutine `exec` writes it on while this
// test polls it. A plain `bytes.Buffer` here is a data race `go test -race` reports.
type syncBuffer struct {
	mu  sync.Mutex
	buf strings.Builder
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// runUIEnv makes THIS TEST BINARY run `main()` instead of the test suite.
//
// 🔴 THE CHILD IS THE REAL `main`, NOT A SECOND COPY OF ITS WIRING, for the reason
// `cmd/cairn-server/main_test.go` states at length: a test that re-assembled the startup
// sequence would pass with the call it is about deleted. It is read HERE and nowhere in the
// program, so it is not a configuration surface an operator can reach.
const (
	runUIEnv       = "CAIRN_UI_TEST_RUN_MAIN"
	testRefreshEnv = "CAIRN_UI_TEST_REFRESH_INTERVAL"
)

func TestMain(m *testing.M) {
	if os.Getenv(runUIEnv) != "1" {
		os.Exit(m.Run())
	}
	// The child REFUSES an unusable interval rather than falling back to the production
	// one: a child quietly running at 30 s would turn the refresh assertions below into a
	// timeout whose message named the loop instead of the harness.
	if raw, set := os.LookupEnv(testRefreshEnv); set {
		interval, err := time.ParseDuration(raw)
		if err != nil || interval <= 0 {
			fmt.Fprintf(os.Stderr, "cairn-ui test child: %s must be a positive duration, got %q\n",
				testRefreshEnv, raw)
			os.Exit(2)
		}
		refreshInterval = interval
	}
	main()
}

// The startup refusals are the only thing standing between a misconfigured deployment and
// a surface that passes its health check and can serve nobody. Until this file existed
// they were verified by hand and reported in a commit message — the same "a number in
// prose with no harness" defect an audit round had just filed against this PR's own cost
// comment, repeated one commit later on the guard that round added. So: a test.
//
// 🔴 THIS DRIVES THE PREDICATE AND `openAuthority`, NOT THE PROCESS. The exit code is one
// `os.Exit(exitConfig)` in `main`; what can be wrong is WHICH STATES the guard admits, and
// that is exactly what these cases vary. A test that spawned the binary would measure the
// same predicate through a slower door and would still not cover a state nobody thought of.

func TestAnAbsentOrUnusableControlJournalIsRefusedBeforeTheListener(t *testing.T) {
	dir := t.TempDir()

	empty := filepath.Join(dir, "empty.journal")
	if err := os.WriteFile(empty, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	// 🔴 ONE BYTE, BECAUSE ONE BYTE IS WHAT DEFEATED THE FIRST SPELLING OF THIS GUARD.
	// A draft refused `info.Size() == 0`; a journal holding a single newline replays clean
	// and the surface came up serving nobody.
	oneByte := filepath.Join(dir, "newline.journal")
	if err := os.WriteFile(oneByte, []byte("\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	aDir := filepath.Join(dir, "a-directory")
	if err := os.MkdirAll(aDir, 0o755); err != nil {
		t.Fatal(err)
	}

	for _, arm := range []struct {
		name    string
		journal string
		want    string
	}{
		{"a path that does not exist", filepath.Join(dir, "no", "such.journal"), "cannot be read"},
		{"a directory", aDir, "is a DIRECTORY"},
	} {
		if _, err := openAuthority(arm.journal, dir, ""); err == nil {
			t.Errorf("%s: openAuthority returned no error; a surface that starts here passes its "+
				"health check and refuses every sign-in", arm.name)
		} else if !strings.Contains(err.Error(), arm.want) {
			t.Errorf("%s: refusal is %q, want it to contain %q", arm.name, err.Error(), arm.want)
		}
	}

	// 🔴 AND THE FILE THE REFUSAL NAMES MUST NOT BE CREATED BY THE PROGRAM COMPLAINING
	// ABOUT IT. `control.OpenFileStore` does `MkdirAll` then `O_CREATE`, so the `Stat`
	// ordering is the whole reason a typo'd path stays a typo rather than becoming an
	// empty journal this program then has to refuse for a second reason.
	missing := filepath.Join(dir, "no", "such.journal")
	if _, err := os.Stat(filepath.Dir(missing)); !os.IsNotExist(err) {
		t.Errorf("openAuthority created %s while refusing it", filepath.Dir(missing))
	}

	// An empty and a one-byte journal both OPEN fine — they are refused by the state
	// check, not by `openAuthority`, and that split is the point of having two.
	for _, arm := range []struct {
		name    string
		journal string
	}{
		{"an empty journal", empty},
		{"a one-byte journal", oneByte},
	} {
		cache, err := openAuthority(arm.journal, dir, "")
		if err != nil {
			t.Fatalf("%s: openAuthority refused it (%v); this arm exists to reach the STATE check", arm.name, err)
		}
		if err := cache.Refresh(context.Background()); err != nil {
			t.Fatalf("%s: refresh: %v", arm.name, err)
		}
		if err := refuseAnAuthorityNobodyCanSignInTo(cache, arm.journal); err == nil {
			t.Errorf("%s: the state check admitted an authority with no usable credential", arm.name)
		}
	}
}

// TestAJournalWithUsersAndNoCredentialIsRefused is the case the SECOND spelling of this
// guard admitted, and it is the one the guard's own remedy used to produce.
//
// 🔴 `cairn-server -create-user` MINTS A USER AND NO CREDENTIAL — `createuser.go` says so
// in as many words — so a journal seeded exactly as the old refusal instructed had users,
// zero credentials, and could authenticate nobody. A `len(Users) == 0` check passes it.
// Measured end to end on both built binaries before this test was written.
func TestAJournalWithUsersAndNoCredentialIsRefused(t *testing.T) {
	cache, journal := seededJournal(t, noCredential)
	err := refuseAnAuthorityNobodyCanSignInTo(cache, journal)
	if err == nil {
		t.Fatal("a journal with a user and NO credential was admitted. `control.Authenticate` matches " +
			"a presented token against LIVE credentials, so this deployment answers 401 to every " +
			"sign-in while announcing itself writable — the exact state the refusal names.")
	}
	// The message has to name the real remedy, because the obvious one does not work.
	for _, want := range []string{"NO USABLE CREDENTIAL", "create-user", "does not fix", "1 user(s)"} {
		if !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(want)) {
			t.Errorf("the refusal does not mention %q, so an operator follows the wrong remedy:\n%s", want, err)
		}
	}
}

// TestAJournalWhoseCredentialsAreALLREVOKEDIsRefused covers the one clause of the
// predicate a mutation sweep found UNCOVERED.
//
// 🔴 DELETING `if !c.Live() { continue }` LEFT THIS PACKAGE GREEN — measured, in a sweep
// whose two controls both moved (an always-refuse mutant and an always-admit mutant were
// each killed). A journal whose every credential carries `credential-revoked` is a real
// operational state — a rotation with the new credential not yet issued — and without
// that clause the surface starts and can authenticate nobody.
//
// ⚠ THE OTHER CLAUSE, `PrincipalFor`, IS EQUIVALENT AND DELIBERATELY HAS NO TEST — AND
// THE REASON RESTS ON THREE CONSTRAINTS, NOT ONE, BECAUSE A CREDENTIAL'S PRINCIPAL MAY BE
// A PROJECT AS WELL AS A USER (`control.Kind.Valid()` accepts both). (a) `control.FileStore`
// refuses a `credential-issued` naming a subject the model does not hold
// (`event N (credential-issued): subject … does not exist`); (b) `AllEventKinds` has no
// user-deletion, project-removal or project-RENAME kind — only `scope-renamed` — so a held
// principal cannot stop being held or lose its name; and (c) `Event.validate` requires a
// non-empty `name` on `project-created`, which is the only thing stopping `displayOf`
// returning "" for a project the model DOES hold, which is how `PrincipalFor` reports
// false. No journal can reach the state this clause guards, so a test for it would mean
// writing a journal the store refuses to replay.
//
// 🔴 ALL THREE ARE LOAD-BEARING, AND (b) AND (c) ARE THE ONES A LATER CHANGE BREAKS. Add a
// `project-renamed` kind without a non-empty-name check, or any principal-removal kind, and
// this clause becomes REACHABLE while this comment still reads as a valid reason not to
// test it. An audit round found the earlier version of this paragraph naming only (a) and
// the user half of it.
func TestAJournalWhoseCredentialsAreALLREVOKEDIsRefused(t *testing.T) {
	cache, journal := seededJournal(t, credentialRevoked)
	err := refuseAnAuthorityNobodyCanSignInTo(cache, journal)
	if err == nil {
		t.Fatal("a journal whose only credential is REVOKED was admitted. `control.Authenticate` skips a " +
			"revoked credential, so this deployment answers 401 to every sign-in while announcing " +
			"itself writable.")
	}
	// 🔴 THE MESSAGE IS PINNED HERE FOR THE REASON ITS SIBLING PINS ONE, AND THIS IS THE
	// STATE WHERE IT MATTERS MOST. An operator hitting the revoked case needs to read that a
	// credential EXISTS and is not live — "1 credential record(s), 0 of them live" — not
	// that they have none. A refusal reworked to report `0 credential record(s)` here would
	// leave the no-credential test green and send them looking for a record that is sitting
	// in the journal, revoked.
	for _, want := range []string{"NO USABLE CREDENTIAL", "1 credential record(s)", "0 of them live"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not mention %q, so an operator cannot tell a REVOKED credential "+
				"from an absent one:\n%s", want, err)
		}
	}
}

// TestAJournalWithALiveCredentialIsAdmitted is the POSITIVE CONTROL. Without it every
// assertion above is satisfied by a guard that refuses everything.
func TestAJournalWithALiveCredentialIsAdmitted(t *testing.T) {
	cache, journal := seededJournal(t, credentialLive)
	if err := refuseAnAuthorityNobodyCanSignInTo(cache, journal); err != nil {
		t.Fatalf("POSITIVE CONTROL FAILED: a journal with a live, attributable credential was refused "+
			"(%v). Every refusal in this file would then be about a guard that admits nothing.", err)
	}
	// And the token-file branch must be exempt, or the guard fires where it cannot apply.
	if err := refuseAnAuthorityNobodyCanSignInTo(cache, ""); err != nil {
		t.Errorf("the state check fired with no -control-journal, where the token-file projection "+
			"synthesizes its own credentials and this check cannot apply: %v", err)
	}
}

// TestAJournalAnIssuedCredentialMakesSignInCapableIsAdmitted is the regression test for
// the DELIVERABLE rather than for a predicate: the refusal above IS the defect, and this
// is the case that says an operator can now get out of it with a command.
//
// 🔴 IT BUILDS THE JOURNAL THE WAY AN OPERATOR DOES — `control.ProvisionUser` then
// `control.IssueCredential`, the two library halves `cairn-server -create-user` and
// `cairn-server -issue-credential` are the only callers of — rather than hand-appending a
// `credential-issued` event the way `seededJournal` does. A hand-built fixture would pass
// whether or not any tool in this repository could produce that state, which is exactly
// the gap this change closes: the old refusal text said no tool could, and it was right.
//
// ⚠ ITS MATRIX IS NOT "RED AT THE BASE COMMIT", AND SAYING SO IS THE HONEST VERSION.
// `control.IssueCredential` does not exist at `origin/main`, so this file does not COMPILE
// there — which proves nothing about a guard. What is measured red at the base commit is
// the defect itself, by hand and on the built binaries: `-create-user` writes a journal,
// `cairn-ui -control-journal` on that journal exits 78 naming "0 credential record(s)", and
// nothing in the tree moves it. The sibling `TestAJournalWithUsersAndNoCredentialIsRefused`
// is the guard that pins that state stays refused; this one pins that it is now ESCAPABLE.
func TestAJournalAnIssuedCredentialMakesSignInCapableIsAdmitted(t *testing.T) {
	at := time.Date(2000, 6, 1, 12, 0, 0, 0, time.UTC)
	path := filepath.Join(t.TempDir(), "provisioned.journal")
	store, err := control.OpenFileStore(path)
	if err != nil {
		t.Fatalf("opening the journal: %v", err)
	}

	made, err := control.ProvisionUser(context.Background(), store, control.NewUser{
		Provider: "fixture-provider", Subject: "00000000-0000-4000-8000-000000000041",
		Email: "rowan@notes.example.invalid", ProjectName: "quarry",
		ScopeNames: []string{"quarry-notes"}, At: at,
	})
	if err != nil {
		t.Fatalf("provisioning: %v", err)
	}

	// THE PRE-STATE, MEASURED RATHER THAN ASSUMED. Without this the test cannot tell "the
	// credential fixed it" from "this journal was never refused in the first place", which
	// is the same shape as a harness wired to nothing.
	cache, err := openAuthority(path, t.TempDir(), "")
	if err != nil {
		t.Fatalf("openAuthority: %v", err)
	}
	if err := cache.Refresh(context.Background()); err != nil {
		t.Fatalf("materializing: %v", err)
	}
	if err := refuseAnAuthorityNobodyCanSignInTo(cache, path); err == nil {
		t.Fatal("PRE-STATE FAILED: a journal holding only a provisioned user was ADMITTED, so the " +
			"case below measures nothing. `-create-user` mints a user and no credential.")
	}

	issued, err := control.IssueCredential(context.Background(), store, control.NewCredential{
		SubjectKind: control.KindUser, SubjectID: made.User, Label: "rowan laptop", At: at,
	})
	if err != nil {
		t.Fatalf("issuing a credential: %v", err)
	}

	// A fresh cache, because the refusal runs against what the binary materializes at
	// startup rather than against a value a test kept warm.
	after, err := openAuthority(path, t.TempDir(), "")
	if err != nil {
		t.Fatalf("openAuthority after issuing: %v", err)
	}
	if err := after.Refresh(context.Background()); err != nil {
		t.Fatalf("re-materializing: %v", err)
	}
	if err := refuseAnAuthorityNobodyCanSignInTo(after, path); err != nil {
		t.Fatalf("a journal with a credential issued by `control.IssueCredential` — the library half "+
			"of `cairn-server -issue-credential` — was still refused: %v", err)
	}

	// And the credential is the one that makes it sign-in-capable, asserted through the
	// authentication path rather than by counting records: a record that cannot
	// authenticate would satisfy the count and serve nobody.
	p, _, err := control.Authenticate(after.Model(), issued.Token())
	if err != nil {
		t.Fatalf("the issued credential does not authenticate against the materialized authority: %v", err)
	}
	if p.ID != made.User {
		t.Fatalf("the credential authenticates as %s, want the provisioned user %s", p.ID, made.User)
	}
}

// credentialState is which of the three shapes a seeded journal carries. Named rather
// than a bool, because the third state — a credential that exists and is REVOKED — is the
// one a bool could not express and the one a mutation sweep found uncovered.
type credentialState int

const (
	noCredential credentialState = iota
	credentialLive
	credentialRevoked
)

// seededJournal builds a real `control.FileStore` journal in the requested state.
func seededJournal(t *testing.T, state credentialState) (*control.Cache, string) {
	t.Helper()
	at := time.Date(2000, 6, 1, 12, 0, 0, 0, time.UTC)
	path := filepath.Join(t.TempDir(), "seeded.journal")
	store, err := control.OpenFileStore(path)
	if err != nil {
		t.Fatalf("opening the journal: %v", err)
	}
	user := control.DerivedID(control.PrefixUser, "startup-user")
	project := control.DerivedID(control.PrefixProject, "startup-project")
	events := []control.Event{
		{Kind: control.EventUserCreated, At: at, UserID: user,
			Provider: "fixture-provider", Subject: "00000000-0000-4000-8000-000000000031",
			Email: "startup@notes.example.invalid"},
		{Kind: control.EventProjectCreated, At: at, ProjectID: project, Name: "startup", UserID: user},
		{Kind: control.EventMemberSet, At: at, ProjectID: project, UserID: user, Role: control.RoleOwner},
	}
	if state != noCredential {
		events = append(events, control.Event{
			Kind: control.EventCredentialIssued, At: at, CredentialID: "crd_startup",
			SubjectKind: control.KindUser, SubjectID: user,
			TokenHash: control.HashToken("fixture-startup-credential-not-a-real-token"),
			Label:     "fixture",
		})
	}
	if state == credentialRevoked {
		events = append(events, control.Event{
			Kind: control.EventCredentialRevoked, At: at, CredentialID: "crd_startup",
		})
	}
	if _, err := store.Append(context.Background(), events...); err != nil {
		t.Fatalf("seeding the journal: %v", err)
	}
	cache := control.NewCache(store, control.CacheOptions{})
	if err := cache.Refresh(context.Background()); err != nil {
		t.Fatalf("materializing: %v", err)
	}
	return cache, path
}

// TestTheBINARYSaysSoWhenItsJournalLostARecordAtReplay is the gate on THIS PROGRAM'S drop
// render, and nothing else in this package can be it.
//
// 🔴 THE RENDER LIVES IN `main`, SO EVERY TEST ABOVE IS STRUCTURALLY BLIND TO IT. They
// drive `openAuthority` and `refuseAnAuthorityNobodyCanSignInTo` directly; deleting the
// announcement from `main` leaves all of them green and leaves this surface serving an
// authority quietly shorter than its journal. That is the "a capability nobody routes to"
// shape `main-never-dispatches-create-user` exists for. ⚠ **THAT WAS MEASURED AT `ddb74dc`,
// AND THIS TEST IS WHAT FALSIFIED IT.** Stubbing out both startup renders left
// `go test ./cmd/... ./internal/control/...` fully green THEN. Re-measured here, stubbing
// exactly those two call sites and nothing else, it now fails THREE: this one,
// `TestTheRUNNINGBinarySaysSoWhenARecordIsDroppedAfterStartup` (this binary's startup and
// refresh renders are one loop, so one stub takes both), and `cmd/cairn-server`'s
// `TestADropAlreadyInTheJournalIsAnnouncedWhenTheAuTHORITYOPENS` — while
// `internal/control/...` still passes, which is the half that shows the coverage is in the
// right package. The green was the defect; do not reproduce it and conclude the tree broke.
//
// 🔴 AND IT IS THE STATE WHERE THE MISSING LINE IS WORST. The refusal below fires BECAUSE
// the only credential in the journal was the dropped one; without the warning it reports a
// journal that simply has no credential, and the operator issues a second one rather than
// deleting the line they pasted a raw token into.
//
// ⚠ THE CHILD EXITS 78 AND THAT IS THE POINT, NOT A LIMITATION: the drop is announced on
// the way up, before the refusal and before any listener, so this test needs no port and no
// timing. It carries its own NEGATIVE CONTROL — the same binary over a journal with no
// dropped record must print no WARNING — because "stderr contains a warning" is otherwise
// satisfied by a program that warns unconditionally.
func TestTheBINARYSaysSoWhenItsJournalLostARecordAtReplay(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}

	run := func(journal string) (int, string) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		child := exec.CommandContext(ctx, self,
			"-control-journal", journal,
			"-store", t.TempDir(),
			"-session-file", filepath.Join(t.TempDir(), "sessions"),
			"-token-file", filepath.Join(t.TempDir(), "absent-token"),
			"-port", "0")
		child.Env = []string{runUIEnv + "=1"}
		body, _ := child.CombinedOutput()
		if ctx.Err() != nil {
			t.Fatalf("the child did not exit within 20s — it reached the listener over a journal it "+
				"should have refused:\n%s", body)
		}
		if child.ProcessState == nil {
			t.Fatalf("no process state:\n%s", body)
		}
		return child.ProcessState.ExitCode(), string(body)
	}

	// The NEGATIVE CONTROL first: a journal with a user, no credential and NOTHING dropped.
	// It is refused for the ordinary reason and must say nothing about a dropped record.
	_, clean := seededJournal(t, noCredential)
	if code, body := run(clean); code != exitConfig {
		t.Fatalf("the control journal with no credential exited %d, want %d:\n%s", code, exitConfig, body)
	} else if strings.Contains(body, "WARNING the control journal") {
		t.Fatalf("NEGATIVE CONTROL FAILED: a journal with no dropped record produced a drop "+
			"warning, so the assertion below is about a program that warns unconditionally:\n%s", body)
	}

	// …and the case: the same journal with a hand-appended `credential-issued` whose
	// `token_hash` is a pasted RAW TOKEN. Synthetic, fixed, 64 characters and not hex — the
	// width is what matters, because any other length was refused by the check this one
	// replaced.
	_, journal := seededJournal(t, noCredential)
	fh, err := os.OpenFile(journal, os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	notADigest := strings.Repeat("cairn-test-not-a-digest-", 3)[:64]
	line := `{"kind":"credential-issued","at":"2000-06-01T12:00:00Z","credential_id":"crd_pastedraw",` +
		`"subject_kind":"user","subject_id":"` + string(control.DerivedID(control.PrefixUser, "startup-user")) +
		`","token_hash":"` + notADigest + `","label":"hand-appended"}` + "\n"
	if _, err := fh.WriteString(line); err != nil {
		t.Fatal(err)
	}
	if err := fh.Close(); err != nil {
		t.Fatal(err)
	}

	code, body := run(journal)
	if !strings.Contains(body, "crd_pastedraw") {
		t.Fatalf("the binary started over a journal whose credential record was DROPPED at replay "+
			"and said nothing about it. The authority it serves is short of what the file describes, "+
			"silently, and that silence is the whole cost of the replay leniency:\n%s", body)
	}
	if !strings.Contains(body, "hex digest") {
		t.Fatalf("the drop is announced without its reason, so an operator cannot tell a pasted "+
			"secret from a duplicate record:\n%s", body)
	}
	if code != exitConfig {
		t.Fatalf("exit %d, want %d — the refusal still fires, because the dropped record was the "+
			"only credential:\n%s", code, exitConfig, body)
	}
	// And the refusal's own count now says what it counts, rather than reporting the
	// dropped record as an absent one.
	if !strings.Contains(body, "1 record(s) DROPPED") {
		t.Fatalf("the refusal does not report the dropped record in its count, so 'credential "+
			"record(s)' reads as 'nobody ever issued one':\n%s", body)
	}
}

// handAppendedRawTokenRecord is a `credential-issued` line an operator wrote by hand with
// the RAW TOKEN pasted into `token_hash`. Synthetic and fixed: 64 characters, not hex, and
// it has never authorised anything. The width is what matters — a value of any other length
// was already refused by the length-only check this guard replaced.
func handAppendedRawTokenRecord(credential string, subject control.ID) string {
	notADigest := strings.Repeat("cairn-test-not-a-digest-", 3)[:64]
	return `{"kind":"credential-issued","at":"2000-06-01T12:00:00Z","credential_id":"` + credential +
		`","subject_kind":"user","subject_id":"` + string(subject) +
		`","token_hash":"` + notADigest + `","label":"hand-appended"}` + "\n"
}

// TestTheRUNNINGBinarySaysSoWhenARecordIsDroppedAfterStartup is the REFRESH half, and it is
// a different claim from the startup half above.
//
// 🔴 THE TWO RENDER CALLS FAIL INDEPENDENTLY, WHICH IS WHY THERE ARE TWO TESTS. Deleting
// the announcement from the refresh loop leaves the startup test green and leaves this
// surface silent about exactly the drop that matters most: the one that appears while a
// person is trying to sign in. It is the same measured shape as the pod's — there,
// `warnAboutDroppedRecords` ran once before `Cache.Run` and a record appended afterwards
// produced an EMPTY operator stream.
//
// 🔴 AND THE SECOND ASSERTION IS "ONCE". The loop ticks on a timer, so an announcement per
// refresh is thousands of identical lines a day for one bad journal line — a stream an
// operator filters, which is the same outcome as saying nothing.
//
// ⚠ IT RUNS A LONG-LIVED CHILD AND BINDS AN EPHEMERAL PORT (`-port 0`), which is a
// dimension: the same two points `cmd/cairn-server`'s binary tests are measured at, an
// ordinary `go test` and the nix sandbox's `checkPhase`, and the child is killed by
// cancelling its context.
func TestTheRUNNINGBinarySaysSoWhenARecordIsDroppedAfterStartup(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	// A LIVE credential, because this arm needs the binary to get PAST the startup refusal
	// and reach its refresh loop.
	_, journal := seededJournal(t, credentialLive)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	child := exec.CommandContext(ctx, self,
		"-control-journal", journal,
		"-store", t.TempDir(),
		"-session-file", filepath.Join(t.TempDir(), "sessions"),
		"-token-file", filepath.Join(t.TempDir(), "absent-token"),
		"-host", "127.0.0.1", "-port", "0")
	child.Env = []string{runUIEnv + "=1", testRefreshEnv + "=5ms"}
	var body syncBuffer
	child.Stdout, child.Stderr = &body, &body
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cancel(); _ = child.Wait() })

	waitFor := func(what string, cond func() bool) {
		t.Helper()
		deadline := time.Now().Add(20 * time.Second)
		for time.Now().Before(deadline) {
			if cond() {
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
		t.Fatalf("timed out waiting for %s. Child output so far:\n%s", what, body.String())
	}
	count := func(sub string) int { return strings.Count(body.String(), sub) }

	waitFor("the surface to come up", func() bool { return strings.Contains(body.String(), "serving") })

	// 🔴 THE NEGATIVE CONTROL. Many refreshes over a clean journal must produce no drop
	// line, or the assertion below is about a program that warns unconditionally.
	time.Sleep(200 * time.Millisecond)
	if n := count("WARNING the control journal"); n != 0 {
		t.Fatalf("a clean journal produced %d drop line(s) while running:\n%s", n, body.String())
	}

	fh, err := os.OpenFile(journal, os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fh.WriteString(handAppendedRawTokenRecord("crd_appearedlater",
		control.DerivedID(control.PrefixUser, "startup-user"))); err != nil {
		t.Fatal(err)
	}
	if err := fh.Close(); err != nil {
		t.Fatal(err)
	}

	waitFor("the drop to reach the operator", func() bool { return count("crd_appearedlater") > 0 })
	if !strings.Contains(body.String(), "hex digest") {
		t.Fatalf("the announced line does not carry the refusal's own reason:\n%s", body.String())
	}

	// …and ONCE, across many further refreshes over the same unchanged file.
	time.Sleep(200 * time.Millisecond)
	if n := count("crd_appearedlater"); n != 1 {
		t.Fatalf("one standing drop was announced %d times. At the production interval that is "+
			"~2,880 identical lines a day:\n%s", n, body.String())
	}
}
