package control

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// provisionClock is the instant every event below carries. Year 2000, per the repo's
// fixture rule.
var provisionClock = time.Date(2000, 6, 1, 12, 0, 0, 0, time.UTC)

func journalAt(t *testing.T) (*FileStore, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "control.journal")
	store, err := OpenFileStore(path)
	if err != nil {
		t.Fatalf("opening a journal: %v", err)
	}
	store.Now = func() time.Time { return provisionClock }
	return store, path
}

func aUser(scopes ...string) NewUser {
	return NewUser{
		Provider:    "notes-idp",
		Subject:     "subject-0001",
		Email:       "rowan@notes.example.test",
		ProjectName: "quarry",
		ScopeNames:  scopes,
		At:          provisionClock,
	}
}

// TestProvisioningAUserYieldsAnAuthorityThatAuthorisesThem.
//
// 🔴 IT ASSERTS THE AUTHORIZATION'S CONTENT, NOT THAT THE WRITE RETURNED NIL. A batch
// that wrote the user and the project and SKIPPED the membership would satisfy every
// structural check and leave `Resolve` empty — which is the exact state P4 shipped, and
// which is indistinguishable from a broken sign-in from anywhere outside this function.
func TestProvisioningAUserYieldsAnAuthorityThatAuthorisesThem(t *testing.T) {
	store, _ := journalAt(t)
	made, err := ProvisionUser(context.Background(), store, aUser("quarry-notes", "quarry-plans"))
	if err != nil {
		t.Fatalf("provisioning: %v", err)
	}

	// Re-read from DISK rather than trusting the returned model: the point of a journal
	// is that the world survives the process, and a batch that projected correctly in
	// memory while writing something `Replay` refuses would pass an in-memory check.
	reread, err := OpenFileStore(store.Path())
	if err != nil {
		t.Fatalf("re-opening the journal: %v", err)
	}
	m, err := reread.Model(context.Background())
	if err != nil {
		t.Fatalf("replaying the journal a second process wrote: %v", err)
	}

	user, held := m.UserByProviderSubject("notes-idp", "subject-0001")
	if !held {
		t.Fatal("the journal holds no user for the pair it was told to create")
	}
	if user.ID != made.User {
		t.Fatalf("the replayed user id %q is not the one provisioning reported, %q", user.ID, made.User)
	}
	principal, known := m.PrincipalFor(KindUser, user.ID)
	if !known {
		t.Fatal("the created user resolves to no principal, so no write could record an actor")
	}
	if principal.Display != "rowan@notes.example.test" {
		t.Fatalf("display = %q, want the email the operator supplied", principal.Display)
	}

	auth := Resolve(m, principal)
	if len(made.Scopes) != 2 {
		t.Fatalf("expected two provisioned scopes, got %+v", made.Scopes)
	}
	for _, sc := range made.Scopes {
		for _, verb := range AllVerbs {
			if !auth.Allows(sc.ID, verb) {
				t.Fatalf("the owner cannot %s %s (%q): membership at RoleOwner is what confers "+
					"read+write+admin over the project's own scopes, and an empty authorization is "+
					"what a missing `member-set` produces", verb, sc.ID, sc.Name)
			}
		}
	}
	// The name projection, which is what the reader narrows directories by.
	visible := auth.VisibleScopes(VerbRead)
	for _, sc := range made.Scopes {
		if !visible.Allows(sc.Name) {
			t.Fatalf("scope %q is authorised by id and absent from the NAME projection, so the reader would serve nothing", sc.Name)
		}
	}
	// 🔴 THE NEGATIVE CONTROL. Without it a `VisibleScopes` that returned
	// `store.Unrestricted()` — the sentinel `Authorization.VisibleScopes`'s comment
	// forbids — would satisfy every assertion above.
	if visible.Allows("a-scope-nobody-created") {
		t.Fatal("the visible set answers yes to a name no scope carries: it is a wildcard rather than an enumeration")
	}
	if made.Epoch != 5 {
		// user + project + member + two scopes. Pinned as a LITERAL rather than derived
		// from the batch, so a path that silently dropped an event is visible.
		t.Fatalf("epoch = %d after a five-event batch", made.Epoch)
	}
}

// TestTwoUsersCannotShareOneProviderSubjectPair is the journal invariant this slice adds.
//
// 🔴 IT IS A REGRESSION TEST WITH A MEASURED RED, AND THE HAZARD IT CLOSES IS ONE THIS
// SLICE ITSELF CREATES. Before a creation path existed nothing minted user rows, so a
// duplicated pair was unreachable in practice; `Replay` accepted the pair happily, which
// is what the red at the base commit shows. With a creation path an operator who runs
// `-create-user` twice for one person would get two rows, and `UserByProviderSubject`
// returns the FIRST match over a Go map — so "who is this session" would answer a
// different user id, with a different authorization, on different requests in one
// process.
func TestTwoUsersCannotShareOneProviderSubjectPair(t *testing.T) {
	first := Event{
		Kind: EventUserCreated, At: provisionClock,
		UserID: "usr_aaaa", Provider: "notes-idp", Subject: "subject-0001",
	}
	second := first
	second.UserID = "usr_bbbb"
	// Deliberately a DIFFERENT email, so the refusal cannot be mistaken for a check on
	// the whole row being identical.
	second.Email = "someone-else@notes.example.test"

	if _, err := Replay([]Event{first}); err != nil {
		t.Fatalf("precondition: one user must replay: %v", err)
	}
	_, err := Replay([]Event{first, second})
	if err == nil {
		t.Fatal("two user rows carrying one (provider, subject) pair replayed cleanly — " +
			"`UserByProviderSubject` would then answer by map iteration order")
	}
	// The refusal must name BOTH ids, because an operator reading it has to know which
	// row to remove.
	for _, want := range []string{"usr_aaaa", "usr_bbbb"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("the refusal does not name %s: %v", want, err)
		}
	}

	// 🔴 THE GUARD MUST NOT BE WIDER THAN ITS SENTENCE. A second user at the SAME
	// provider with a different subject, and at a different provider with the same
	// subject, are both legitimate — the key is the pair.
	for _, arm := range []struct {
		name  string
		build func(Event) Event
	}{
		{"same provider, different subject", func(e Event) Event { e.Subject = "subject-0002"; return e }},
		{"different provider, same subject", func(e Event) Event { e.Provider = "other-idp"; return e }},
	} {
		t.Run(arm.name, func(t *testing.T) {
			other := arm.build(second)
			if _, err := Replay([]Event{first, other}); err != nil {
				t.Fatalf("a legitimate second user was refused: %v", err)
			}
		})
	}

	// And through the provisioning path, which is how an operator meets it.
	store, _ := journalAt(t)
	if _, err := ProvisionUser(context.Background(), store, aUser("quarry-notes")); err != nil {
		t.Fatalf("the first provisioning must succeed: %v", err)
	}
	again := aUser("quarry-other")
	if _, err := ProvisionUser(context.Background(), store, again); err == nil {
		t.Fatal("provisioning the same person twice succeeded, leaving two rows for one pair")
	}
}

// TestAScopeNameAlreadyInTheJournalIsRefused.
//
// 🔴 THE HAZARD IS THE NAME-BASED PROJECTION, NOT THE MODEL'S OWN UNIQUENESS RULE.
// `apply` allows two projects to each hold a scope called `notes`, correctly, because
// authorization is keyed on ids. The reader is not: `VisibleScopes` hands out NAMES and
// the store root's DIRECTORIES are narrowed by them, so two scope records sharing a
// display name resolve to one directory and each project's members read the other's
// entries. This asserts the guard, and asserts it is not wider than that sentence.
func TestAScopeNameAlreadyInTheJournalIsRefused(t *testing.T) {
	store, path := journalAt(t)
	if _, err := ProvisionUser(context.Background(), store, aUser("quarry-notes")); err != nil {
		t.Fatalf("the first provisioning must succeed: %v", err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the journal: %v", err)
	}

	clash := aUser("quarry-notes")
	clash.Subject = "subject-0002"
	_, err = ProvisionUser(context.Background(), store, clash)
	if !errors.Is(err, ErrScopeNameTaken) {
		t.Fatalf("the WRONG guard fired.\n  got:  %v\n  want: %v", err, ErrScopeNameTaken)
	}
	// The refusal must name the scope and the project that holds it, so an operator can
	// see whose directory they were about to alias.
	if !strings.Contains(err.Error(), "quarry-notes") || !strings.Contains(err.Error(), "prj_") {
		t.Fatalf("the refusal names neither the scope nor the project holding it: %v", err)
	}

	// 🔴 REFUSED BEFORE ANY BYTE IS WRITTEN. The check runs before `Append`, so a
	// rejected provisioning must leave the file exactly as it was — the same property
	// `TestARejectedBatchLeavesNeitherBytesNorState` pins one level down. Compared as
	// BYTES rather than by event count: a rewritten file with the same number of lines
	// would pass a count.
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the journal: %v", err)
	}
	if string(after) != string(before) {
		t.Fatalf("a refused provisioning changed the journal.\n  before: %q\n  after:  %q", before, after)
	}

	// The positive control: a FREE name, for the same new person, succeeds. Without it
	// the refusal above is satisfied by a path that refuses every second provisioning.
	fine := aUser("quarry-plans")
	fine.Subject = "subject-0002"
	if _, err := ProvisionUser(context.Background(), store, fine); err != nil {
		t.Fatalf("a second user with a FREE scope name must succeed: %v", err)
	}
}

// TestARefusedProvisioningLeavesNeitherBytesNorState covers the refusals that happen
// INSIDE `Append` rather than before it — where the events are built, validated against a
// clone, and the file is only written if every one of them replays.
func TestARefusedProvisioningLeavesNeitherBytesNorState(t *testing.T) {
	store, path := journalAt(t)
	for _, arm := range []struct {
		name string
		req  NewUser
	}{
		// Each of these is refused by `Event.validate` or `Model.apply`, which is where
		// the rule lives — `ProvisionUser` deliberately re-validates nothing.
		{"no provider", NewUser{Subject: "s", ProjectName: "p", At: provisionClock}},
		{"no subject", NewUser{Provider: "notes-idp", ProjectName: "p", At: provisionClock}},
		{"no project name", NewUser{Provider: "notes-idp", Subject: "s", At: provisionClock}},
		{
			"two scopes with one name in the SAME new project",
			NewUser{Provider: "notes-idp", Subject: "s", ProjectName: "p",
				ScopeNames: []string{"notes", "notes"}, At: provisionClock},
		},
	} {
		t.Run(arm.name, func(t *testing.T) {
			if _, err := ProvisionUser(context.Background(), store, arm.req); err == nil {
				t.Fatal("an invalid provisioning request was accepted")
			}
			body, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("reading the journal: %v", err)
			}
			if len(body) != 0 {
				t.Fatalf("a refused provisioning wrote %d bytes: %q", len(body), body)
			}
			m, err := store.Model(context.Background())
			if err != nil {
				t.Fatalf("reading the model: %v", err)
			}
			if len(m.Users) != 0 || len(m.Projects) != 0 || len(m.Scopes) != 0 {
				t.Fatalf("a refused provisioning left state: %d users, %d projects, %d scopes",
					len(m.Users), len(m.Projects), len(m.Scopes))
			}
		})
	}

	// The positive control for the whole table: the same store accepts a VALID request
	// afterwards, so none of the refusals above left the journal unusable.
	if _, err := ProvisionUser(context.Background(), store, aUser("quarry-notes")); err != nil {
		t.Fatalf("a valid request after four refusals must succeed: %v", err)
	}
}

// TestAFileStoreSeesAWriteFromANOTHERProcessThroughItsOrdinaryReadPath.
//
// 🔴 THE POINT IS THE ORDINARY METHOD, NOT A SECOND ONE. The hazard this pins is the
// process-local projection `FileStore.Model` used to serve: it was invalidated only by
// THIS VALUE'S own appends, so a pod reading through it would materialize the journal once
// at startup and never see a user `cairn-server -create-user` wrote from another process —
// the journal correct, the command successful, the sign-in still refused. The fix was to
// delete the short-circuit rather than to route around it through a wrapper type, so the
// assertion is on `Model` itself: the spelling every caller reaches for is the safe one.
//
// ⚠ IT IS TWO VALUES IN ONE PROCESS, NOT TWO PROCESSES, AND THAT IS THE LIMIT OF WHAT IT
// MEASURES. What makes the substitution faithful is that `FileStore`'s state is per-VALUE:
// `cached`/`loaded` are its own fields and nothing shares them, so a second value stands in
// for a second process exactly. What it does NOT exercise is the `flock` between two real
// processes, which `filestore_test.go` covers separately.
func TestAFileStoreSeesAWriteFromAnotherProcessThroughItsOrdinaryReadPath(t *testing.T) {
	writer, path := journalAt(t)
	reader, err := OpenFileStore(path)
	if err != nil {
		t.Fatalf("opening the journal a second time: %v", err)
	}

	// The reader materializes the EMPTY world first, which is what a pod starting before
	// anybody provisions does — and it is what arms the cache the short-circuit used to
	// serve. Without this read the test would pass against the OLD code too, because a
	// `FileStore` that had never loaded fell through to `Reload` anyway.
	if m, err := reader.Model(context.Background()); err != nil || len(m.Users) != 0 {
		t.Fatalf("precondition: the reader must start over an empty journal: %v / %d users", err, len(m.Users))
	}

	if _, err := ProvisionUser(context.Background(), writer, aUser("quarry-notes")); err != nil {
		t.Fatalf("provisioning through the other handle: %v", err)
	}

	current, err := reader.Model(context.Background())
	if err != nil {
		t.Fatalf("reading the model: %v", err)
	}
	if len(current.Users) != 1 {
		t.Fatalf("FileStore.Model answered %d users after ANOTHER handle provisioned one. It is "+
			"serving a process-local projection, so a pod reading through it would never see a "+
			"user created by `cairn-server -create-user`", len(current.Users))
	}
	if _, held := current.UserByProviderSubject("notes-idp", "subject-0001"); !held {
		t.Fatal("the reloaded world holds a user that is not the one provisioned")
	}
}

// TestAnUnreadableJournalLeavesTheFileStoreServingLastKnownGood.
//
// The narrowing `Reload`'s comment claims: it returns last-known-good ALONGSIDE the error,
// and `control.Cache` then discards the model and keeps serving. Asserted here at the store
// rather than only at the cache, because the store is what the pod's authority is built
// over and its error/model pair is the contract the cache depends on.
//
// ⚠ IT IS WHAT KEEPS THE `cached`/`loaded` FIELDS ALIVE AFTER `Model`'s SHORT-CIRCUIT WAS
// DELETED. Nothing reads them on the happy path any more; this is the branch that does, so
// a round that removed them as "unused" is this test going red rather than a review catch.
func TestAnUnreadableJournalLeavesTheFileStoreServingLastKnownGood(t *testing.T) {
	store, path := journalAt(t)
	if _, err := ProvisionUser(context.Background(), store, aUser("quarry-notes")); err != nil {
		t.Fatalf("provisioning: %v", err)
	}
	src := store
	if m, err := src.Model(context.Background()); err != nil || len(m.Users) != 1 {
		t.Fatalf("precondition: one user must be readable: %v / %d", err, len(m.Users))
	}

	if err := os.WriteFile(path, []byte("{not json\n"), 0o600); err != nil {
		t.Fatalf("corrupting the journal: %v", err)
	}
	m, err := src.Model(context.Background())
	if err == nil {
		t.Fatal("a journal that does not parse was read without complaint")
	}
	if len(m.Users) != 1 {
		t.Fatalf("an unreadable journal emptied the authority (%d users). An empty Model authorises "+
			"nobody, so that turns a parse error into a total sign-in outage", len(m.Users))
	}
}
