package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ZacxDev/cairn/internal/control"
)

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
