package control

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
)

// aCoMemberWorld is the state `-remove-member` is for: an owner's project holding a SECOND
// person, put there by `SetMember` — the only path that can, so the fixture is a world the
// operator commands actually reach. The second person keeps their own solo project.
func aCoMemberWorld(t *testing.T) (store *FileStore, path string, owner, second Provisioned) {
	t.Helper()
	store, path, owner = aProvisionedOwner(t) // project `quarry`, scope `quarry-notes`
	second = aSecondUser(t, store)            // project `harbour`, scope `harbour-notes`
	if _, err := SetMember(context.Background(), store, NewMembership{
		ProjectID: owner.Project, UserID: second.User, Role: RoleMember, At: provisionClock,
	}); err != nil {
		t.Fatalf("making the second user a member: %v", err)
	}
	return store, path, owner, second
}

func reread(t *testing.T, path string) Model {
	t.Helper()
	fresh, err := OpenFileStore(path)
	if err != nil {
		t.Fatalf("re-opening the journal: %v", err)
	}
	m, err := fresh.Model(context.Background())
	if err != nil {
		t.Fatalf("replaying the journal: %v", err)
	}
	return m
}

func grant(t *testing.T, store *FileStore, id ID, subjectKind Kind, subject ID, objectKind ObjectKind, object ID, verbs ...Verb) {
	t.Helper()
	if _, err := store.Append(context.Background(), Event{
		Kind: EventGranted, At: provisionClock, GrantID: id,
		SubjectKind: subjectKind, SubjectID: subject,
		ObjectKind: objectKind, ObjectID: object, Verbs: NewVerbSet(verbs...),
	}); err != nil {
		t.Fatalf("granting %s: %v", id, err)
	}
}

// TestRemovingAMemberTakesAwayTheirMembershipAuthority is the end-to-end claim, read back
// through a SECOND `FileStore`, and asserted as `Resolve`'s answer rather than as a map entry.
//
// RED at the base commit by construction — `RemoveMember` does not exist there. The
// behavioural half is the authority check: a record that `apply` folded and `Resolve` still
// honoured would fail it, and so would one that over-reached into the user's OWN project.
func TestRemovingAMemberTakesAwayTheirMembershipAuthority(t *testing.T) {
	store, path, owner, second := aCoMemberWorld(t)
	notes := owner.Scopes[0].ID
	own := second.Scopes[0].ID

	before := reread(t, path)
	if !Resolve(before, Principal{Kind: KindUser, ID: second.User}).Allows(notes, VerbWrite) {
		t.Fatal("the fixture's premise is gone: the member cannot write the project's scope BEFORE removal")
	}

	got, err := RemoveMember(context.Background(), store, MemberRemoval{
		ProjectID: owner.Project, UserID: second.User, At: provisionClock,
	})
	if err != nil {
		t.Fatalf("removing: %v", err)
	}
	after := reread(t, path)

	if _, in := after.Memberships[owner.Project][second.User]; in {
		t.Fatal("the journal still holds the membership")
	}
	auth := Resolve(after, Principal{Kind: KindUser, ID: second.User})
	if vs := auth.VerbsOn(notes); !vs.Empty() {
		t.Errorf("after removal the user still reaches %s at %s through membership", notes, vs)
	}
	// Not over-reaching: their OWN project is untouched, and the owner is still the owner.
	if !auth.Allows(own, VerbAdmin) {
		t.Error("the removal took away the user's authority over their OWN project")
	}
	if !after.MayDeleteProject(owner.User, owner.Project) {
		t.Error("the removal disturbed the owner")
	}

	if got.Role != RoleMember || got.ProjectID != owner.Project || got.UserID != second.User {
		t.Errorf("the result does not describe the removal: %+v", got)
	}
	if got.Epoch != before.Epoch+1 || after.Epoch != got.Epoch {
		t.Errorf("epoch %d -> reported %d, journal %d: want exactly one appended event", before.Epoch, got.Epoch, after.Epoch)
	}
	if len(got.StillReaches) != 0 || len(got.Grants) != 0 {
		t.Errorf("a removal with no grants in play reports residue: %+v / %+v", got.StillReaches, got.Grants)
	}
}

// TestASeparateGrantSurvivesTheRemovalAndIsReported — the grants answer: REPORTED, never
// revoked, and the report covers BOTH routes `Resolve` has to a grant.
//
// The fixture's grants are pairwise distinct in verbs so a report naming the wrong one is
// visible, and it carries four that must NOT be reported: a revoked grant, a grant to the
// user on an object OUTSIDE the project, a grant to somebody else on the project, and a
// grant to the REMOVED project itself (which reached the user only through the membership
// this removal ends).
func TestASeparateGrantSurvivesTheRemovalAndIsReported(t *testing.T) {
	store, path, owner, second := aCoMemberWorld(t)
	ctx := context.Background()
	notes := owner.Scopes[0].ID
	plans, err := CreateScope(ctx, store, NewScope{ProjectID: owner.Project, Name: "quarry-plans", At: provisionClock})
	if err != nil {
		t.Fatalf("creating a second scope: %v", err)
	}
	third := aThirdUser(t, store)

	grant(t, store, "grt_direct", KindUser, second.User, ObjectScope, notes, VerbRead)
	grant(t, store, "grt_viaown", KindProject, second.Project, ObjectScope, plans.Scope.ID, VerbWrite)
	grant(t, store, "grt_revoked", KindUser, second.User, ObjectProject, owner.Project, VerbAdmin)
	if _, err := store.Append(ctx, Event{Kind: EventGrantRevoked, At: provisionClock, GrantID: "grt_revoked"}); err != nil {
		t.Fatalf("revoking: %v", err)
	}
	grant(t, store, "grt_elsewhere", KindUser, second.User, ObjectScope, third.Scopes[0].ID, VerbRead)
	grant(t, store, "grt_someoneelse", KindUser, third.User, ObjectProject, owner.Project, VerbRead)
	grant(t, store, "grt_tothisproject", KindProject, owner.Project, ObjectScope, third.Scopes[0].ID, VerbAdmin)

	got, err := RemoveMember(ctx, store, MemberRemoval{ProjectID: owner.Project, UserID: second.User, At: provisionClock})
	if err != nil {
		t.Fatalf("removing: %v", err)
	}

	var ids []string
	for _, g := range got.Grants {
		ids = append(ids, string(g.Grant.ID)+"<"+string(g.Via)+">")
	}
	if want := "grt_direct<> grt_viaown<" + string(second.Project) + ">"; strings.Join(ids, " ") != want {
		t.Errorf("reported grants are %q, want %q", strings.Join(ids, " "), want)
	}

	// The survival half, measured on the journal: the grants are still live and still work.
	after := reread(t, path)
	for _, id := range []ID{"grt_direct", "grt_viaown"} {
		if !after.Grants[id].Live() {
			t.Errorf("%s was revoked by the removal — the removal must report it, not take it", id)
		}
	}
	auth := Resolve(after, Principal{Kind: KindUser, ID: second.User})
	if !auth.Allows(notes, VerbRead) || auth.Allows(notes, VerbWrite) {
		t.Errorf("after removal the direct READ grant gives read=%v write=%v on %s; want true/false",
			auth.Allows(notes, VerbRead), auth.Allows(notes, VerbWrite), notes)
	}
	if !auth.Allows(plans.Scope.ID, VerbWrite) || auth.Allows(plans.Scope.ID, VerbRead) {
		t.Errorf("after removal the project-subject WRITE grant gives write=%v read=%v; want true/false",
			auth.Allows(plans.Scope.ID, VerbWrite), auth.Allows(plans.Scope.ID, VerbRead))
	}
	if auth.Allows(third.Scopes[0].ID, VerbAdmin) {
		t.Error("the removed user still reaches a scope shared with the project they LEFT")
	}
}

// TestTheReportedResidueIsExactlyWhatResolveStillGrants is the SEAM guard between the two
// halves of the report: `StillReaches` is `Resolve`'s answer and `Grants` is a restatement of
// `Resolve`'s subject set, so the union of the grants' expansions must EQUAL it — failing if
// the report names a grant that reaches nothing, or omits one that reaches something.
func TestTheReportedResidueIsExactlyWhatResolveStillGrants(t *testing.T) {
	store, path, owner, second := aCoMemberWorld(t)
	ctx := context.Background()
	notes := owner.Scopes[0].ID
	plans, err := CreateScope(ctx, store, NewScope{ProjectID: owner.Project, Name: "quarry-plans", At: provisionClock})
	if err != nil {
		t.Fatalf("creating a second scope: %v", err)
	}
	grant(t, store, "grt_a", KindUser, second.User, ObjectScope, notes, VerbRead)
	grant(t, store, "grt_b", KindProject, second.Project, ObjectProject, owner.Project, VerbWrite)

	got, err := RemoveMember(ctx, store, MemberRemoval{ProjectID: owner.Project, UserID: second.User, At: provisionClock})
	if err != nil {
		t.Fatalf("removing: %v", err)
	}
	after := reread(t, path)

	expanded := map[ID]VerbSet{}
	for _, g := range got.Grants {
		switch g.Grant.ObjectKind {
		case ObjectScope:
			expanded[g.Grant.ObjectID] = expanded[g.Grant.ObjectID].Union(g.Grant.Verbs)
		case ObjectProject:
			for _, s := range after.ScopesIn(g.Grant.ObjectID) {
				expanded[s] = expanded[s].Union(g.Grant.Verbs)
			}
		}
	}
	reached := map[ID]VerbSet{}
	for _, r := range got.StillReaches {
		reached[r.ID] = r.Verbs
		if r.Name != after.Scopes[r.ID].DisplayName {
			t.Errorf("StillReaches names %s %q; the journal calls it %q", r.ID, r.Name, after.Scopes[r.ID].DisplayName)
		}
	}
	auth := Resolve(after, Principal{Kind: KindUser, ID: second.User})
	for _, s := range after.ScopesIn(owner.Project) {
		if expanded[s] != reached[s] || reached[s] != auth.VerbsOn(s) {
			t.Errorf("scope %s: the reported grants expand to %s, StillReaches says %s, Resolve says %s",
				s, expanded[s], reached[s], auth.VerbsOn(s))
		}
	}
	// Positive control: the fixture really does leave residue on BOTH scopes, so the
	// equalities above are not 0 == 0 == 0.
	if want := NewVerbSet(VerbRead, VerbWrite); reached[notes] != want || reached[plans.Scope.ID] != NewVerbSet(VerbWrite) {
		t.Fatalf("the fixture's residue is %v; want notes=%s plans=write", reached, want)
	}
}

// TestRemovalReportsEveryLiveCredentialOfTheUser — and only theirs, and only live ones.
func TestRemovalReportsEveryLiveCredentialOfTheUser(t *testing.T) {
	store, _, owner, second := aCoMemberWorld(t)
	ctx := context.Background()
	kept := issueTo(t, store, KindUser, second.User, nil)
	narrowed := issueTo(t, store, KindUser, second.User, []ID{owner.Scopes[0].ID})
	gone := issueTo(t, store, KindUser, second.User, nil)
	if _, err := RevokeCredential(ctx, store, gone.Credential, "", provisionClock); err != nil {
		t.Fatalf("revoking: %v", err)
	}
	issueTo(t, store, KindUser, owner.User, nil)
	issueTo(t, store, KindProject, second.Project, nil)

	got, err := RemoveMember(ctx, store, MemberRemoval{ProjectID: owner.Project, UserID: second.User, At: provisionClock})
	if err != nil {
		t.Fatalf("removing: %v", err)
	}
	want := []ID{kept.Credential, narrowed.Credential}
	if want[1] < want[0] {
		want[0], want[1] = want[1], want[0]
	}
	var ids []ID
	for _, c := range got.Credentials {
		ids = append(ids, c.ID)
	}
	if len(ids) != 2 || ids[0] != want[0] || ids[1] != want[1] {
		t.Fatalf("reported credentials %v, want exactly %v (sorted, live, this user's)", ids, want)
	}
}

// refusedRemoval asserts a refusal by sentinel AND by a fragment of that guard's own message,
// and that the journal is byte-identical afterwards.
func refusedRemoval(t *testing.T, store *FileStore, path string, req MemberRemoval, sentinel error, fragment string) {
	t.Helper()
	before := journalBytes(t, path)
	_, err := RemoveMember(context.Background(), store, req)
	if !errors.Is(err, sentinel) {
		t.Fatalf("RemoveMember(%s from %s): want %v, got %v", req.UserID, req.ProjectID, sentinel, err)
	}
	if !strings.Contains(err.Error(), fragment) {
		t.Fatalf("refused by a DIFFERENT guard — the message lacks %q: %v", fragment, err)
	}
	if after := journalBytes(t, path); !bytes.Equal(before, after) {
		t.Fatalf("a refused removal changed the journal:\n%s", after[len(before):])
	}
}

// TestRemoveMemberRefusesAProjectTheJournalDoesNotHold — `apply` would ACCEPT this one, so
// the early check is the only guard.
func TestRemoveMemberRefusesAProjectTheJournalDoesNotHold(t *testing.T) {
	store, path, _, second := aCoMemberWorld(t)
	for _, project := range []ID{"prj_notinthisjournal", ""} {
		refusedRemoval(t, store, path, MemberRemoval{ProjectID: project, UserID: second.User, At: provisionClock},
			ErrNoSuchProject, "a removal names its project by id")
	}
}

// TestRemoveMemberRefusesAUserTheJournalDoesNotHold.
func TestRemoveMemberRefusesAUserTheJournalDoesNotHold(t *testing.T) {
	store, path, owner, _ := aCoMemberWorld(t)
	for _, user := range []ID{"usr_nobody", ""} {
		refusedRemoval(t, store, path, MemberRemoval{ProjectID: owner.Project, UserID: user, At: provisionClock},
			ErrNoSuchMember, "a removal names its user by id")
	}
}

// TestRemoveMemberRefusesSomebodyWhoIsNotAMember — an existing user, an existing project, no
// membership between them. `apply` would fold this to a no-op and count it as an event.
func TestRemoveMemberRefusesSomebodyWhoIsNotAMember(t *testing.T) {
	store, path, owner, second := aCoMemberWorld(t)
	// The owner is not in the second user's solo project.
	refusedRemoval(t, store, path, MemberRemoval{ProjectID: second.Project, UserID: owner.User, At: provisionClock},
		ErrNotAMember, "there is nothing to remove")
	// And a removal that already landed is refused on the retry, saying what that means.
	if _, err := RemoveMember(context.Background(), store, MemberRemoval{ProjectID: owner.Project, UserID: second.User, At: provisionClock}); err != nil {
		t.Fatalf("the first removal: %v", err)
	}
	refusedRemoval(t, store, path, MemberRemoval{ProjectID: owner.Project, UserID: second.User, At: provisionClock},
		ErrNotAMember, "the first attempt LANDED")
}

// TestTheLastOwnerCannotBeRemoved — `SetMember`'s rule, reached through `RemoveMember`.
func TestTheLastOwnerCannotBeRemoved(t *testing.T) {
	store, path, owner, _ := aCoMemberWorld(t)
	refusedRemoval(t, store, path, MemberRemoval{ProjectID: owner.Project, UserID: owner.User, At: provisionClock},
		ErrWouldOrphanProject, "so removing them from it leaves a project whose membership nobody may change")
}

// TestRemovingOneOfTwoOwnersIsAccepted — the other half of the rule: a handover's last step.
// Without it a guard refusing EVERY owner removal satisfies the test above.
func TestRemovingOneOfTwoOwnersIsAccepted(t *testing.T) {
	store, path, owner, second := aCoMemberWorld(t)
	if _, err := SetMember(context.Background(), store, NewMembership{
		ProjectID: owner.Project, UserID: second.User, Role: RoleOwner, At: provisionClock,
	}); err != nil {
		t.Fatalf("promoting: %v", err)
	}
	got, err := RemoveMember(context.Background(), store, MemberRemoval{ProjectID: owner.Project, UserID: owner.User, At: provisionClock})
	if err != nil {
		t.Fatalf("removing one of two owners was refused: %v", err)
	}
	if got.Role != RoleOwner {
		t.Errorf("the result reports the removed role as %q, want owner", got.Role)
	}
	after := reread(t, path)
	if !after.MayDeleteProject(second.User, owner.Project) || after.MayAdministerProject(owner.User, owner.Project) {
		t.Error("after the handover the remaining owner is not the owner, or the removed one still administers")
	}
}

// TestEveryWriterOfMemberRemovedChecksTheLastOwner is the LEDGER for `member-removed`: every
// function in non-test code that uses `EventMemberRemoved` (outside journal.go's declaration
// and dispatch), failing when the set GROWS or SHRINKS, and each one calling
// `refuseOrphaning` — the rule `apply` does not carry.
func TestEveryWriterOfMemberRemovedChecksTheLastOwner(t *testing.T) {
	want := map[string]bool{
		"internal/control/member_remove.go:RemoveMember": true,
	}
	assertWriterLedger(t, "EventMemberRemoved", "refuseOrphaning", want)
}

func aThirdUser(t *testing.T, store *FileStore) Provisioned {
	t.Helper()
	req := aUser("ledger-notes")
	req.Subject, req.Email, req.ProjectName = "subject-0003", "ash@notes.example.test", "ledger"
	made, err := ProvisionUser(context.Background(), store, req)
	if err != nil {
		t.Fatalf("provisioning the third user: %v", err)
	}
	return made
}
