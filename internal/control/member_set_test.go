package control

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// aSecondUser is a distinct person in the SAME journal, with their own solo project.
//
// 🔴 IT PROVISIONS A WHOLE SECOND USER RATHER THAN APPENDING A BARE `user-created`,
// BECAUSE THE DEFECT UNDER TEST IS ABOUT TWO PEOPLE WITH SEPARATE PROJECTS. That is the
// state `cairn-server -create-user` actually produces when it is run twice with the same
// `-project` name — measured — and a fixture that skipped the second project would be
// testing a world the CLI cannot create.
func aSecondUser(t *testing.T, store *FileStore) Provisioned {
	t.Helper()
	req := aUser("harbour-notes")
	req.Subject = "subject-0002"
	req.Email = "wren@notes.example.test"
	req.ProjectName = "harbour"
	made, err := ProvisionUser(context.Background(), store, req)
	if err != nil {
		t.Fatalf("provisioning the second user: %v", err)
	}
	return made
}

// TestSettingAMemberMakesTwoUsersCO_MEMBERS is the regression test for the defect, and it
// asserts the CONSEQUENCE rather than the write.
//
// 🔴 THE DEFECT WAS NEVER "THE MODEL LACKS A RULE" — it was that nothing called it, so a
// test that only checked `Memberships` would have passed against a journal hand-edited by
// an operator and told us nothing about whether this repository can reach the state. What
// makes it a regression test is that it runs the only path a program here has, and then
// asks the question the share flow asks: is B reachable from A's project.
//
// RED at `origin/main` by construction: `SetMember` does not exist there, so this file
// does not compile. That is a weaker red than a failing assertion and is stated as such —
// the behavioural half is the co-membership check below, which would fail against any
// implementation that appended the event without it taking effect.
func TestSettingAMemberMakesTwoUsersCO_MEMBERS(t *testing.T) {
	store, path, owner := aProvisionedOwner(t)
	second := aSecondUser(t, store)

	before, err := store.Model(context.Background())
	if err != nil {
		t.Fatalf("reading the journal: %v", err)
	}
	// The PRE-STATE is asserted, because "they are co-members now" is only interesting
	// against "they were not". This is also the state the CLI produces unaided.
	if _, in := before.Memberships[owner.Project][second.User]; in {
		t.Fatalf("the fixture already has the second user in the first project, so the "+
			"assertion below cannot distinguish this call from the fixture: %+v",
			before.Memberships[owner.Project])
	}

	got, err := SetMember(context.Background(), store, NewMembership{
		ProjectID: owner.Project,
		UserID:    second.User,
		Role:      RoleMember,
		Actor:     owner.User,
		At:        provisionClock,
	})
	if err != nil {
		t.Fatalf("setting the member: %v", err)
	}
	if got.WasMember {
		t.Errorf("WasMember is true for a user who was not in the project: %+v", got)
	}
	if !got.Changed() {
		t.Errorf("Changed() is false for a join: %+v", got)
	}

	// 🔴 RE-READ THE FILE rather than trust the returned value — the same discipline the
	// rest of this package's tests use. A `SetMember` that computed a correct result and
	// appended nothing would satisfy every assertion above.
	reread, err := OpenFileStore(path)
	if err != nil {
		t.Fatalf("re-opening the journal: %v", err)
	}
	after, err := reread.Model(context.Background())
	if err != nil {
		t.Fatalf("replaying the journal: %v", err)
	}

	ms, in := after.Memberships[owner.Project][second.User]
	if !in {
		t.Fatalf("after SetMember the second user is not in the first project; "+
			"memberships are %+v", after.Memberships[owner.Project])
	}
	if ms.Role != RoleMember {
		t.Errorf("role is %q, want %q", ms.Role, RoleMember)
	}

	// The question the share flow asks: do these two now SHARE a project? That is what
	// `ui.Candidates` narrows on, and it is the thing that was unreachable.
	shared := false
	for projectID := range after.MembershipsOf(owner.User) {
		if _, both := after.MembershipsOf(second.User)[projectID]; both {
			shared = true
			break
		}
	}
	if !shared {
		t.Error("the two users share no project, so a share-flow candidate list " +
			"narrowed on co-membership would still find nobody — which is the defect")
	}
}

// TestAPromotionKeepsTheJoIN_DATE pins the field `apply` already preserves, from the only
// path that can now exercise it twice.
func TestAPromotionKeepsTheJoIN_DATE(t *testing.T) {
	store, _, owner := aProvisionedOwner(t)
	second := aSecondUser(t, store)
	ctx := context.Background()

	if _, err := SetMember(ctx, store, NewMembership{
		ProjectID: owner.Project, UserID: second.User,
		Role: RoleMember, At: provisionClock,
	}); err != nil {
		t.Fatalf("the join: %v", err)
	}
	joined, err := store.Model(ctx)
	if err != nil {
		t.Fatalf("reading: %v", err)
	}
	joinedAt := joined.Memberships[owner.Project][second.User].AddedAt

	got, err := SetMember(ctx, store, NewMembership{
		ProjectID: owner.Project, UserID: second.User,
		Role: RoleAdmin, At: provisionClock.Add(1),
	})
	if err != nil {
		t.Fatalf("the promotion: %v", err)
	}
	if !got.WasMember || got.Previous != RoleMember || got.Role != RoleAdmin {
		t.Errorf("the promotion did not report member->admin: %+v", got)
	}

	promoted, err := store.Model(ctx)
	if err != nil {
		t.Fatalf("reading: %v", err)
	}
	if at := promoted.Memberships[owner.Project][second.User].AddedAt; !at.Equal(joinedAt) {
		t.Errorf("AddedAt moved on a promotion: %v -> %v. `apply` preserves it because "+
			"the field answers 'since when has this person been in the project', and a "+
			"promotion is not a join", joinedAt, at)
	}
}

// TestRestatingTheSameRoleSucceedsAndReportsUNCHANGED.
//
// ⚠ AN INVARIANT GUARD ON A DELIBERATE CHOICE, labelled as one. It pins that a repeated
// call is not refused — so a retry after a timeout cannot fail on the second attempt —
// and that the caller can still tell nothing moved.
func TestRestatingTheSameRoleSucceedsAndReportsUNCHANGED(t *testing.T) {
	store, _, owner := aProvisionedOwner(t)
	second := aSecondUser(t, store)
	ctx := context.Background()

	for range 2 {
		if _, err := SetMember(ctx, store, NewMembership{
			ProjectID: owner.Project, UserID: second.User,
			Role: RoleMember, At: provisionClock,
		}); err != nil {
			t.Fatalf("setting the member: %v", err)
		}
	}
	got, err := SetMember(ctx, store, NewMembership{
		ProjectID: owner.Project, UserID: second.User,
		Role: RoleMember, At: provisionClock,
	})
	if err != nil {
		t.Fatalf("the third identical call: %v", err)
	}
	if got.Changed() {
		t.Errorf("Changed() is true for a restated role: %+v", got)
	}
	if !got.WasMember || got.Previous != RoleMember {
		t.Errorf("want WasMember with Previous=member, got %+v", got)
	}
}

// TestTheLastOwnerCannotBeDemoted is the rule `apply` does not carry.
func TestTheLastOwnerCannotBeDemoted(t *testing.T) {
	store, _, owner := aProvisionedOwner(t)
	ctx := context.Background()

	for _, role := range []Role{RoleAdmin, RoleMember} {
		_, err := SetMember(ctx, store, NewMembership{
			ProjectID: owner.Project, UserID: owner.User,
			Role: role, At: provisionClock,
		})
		if !errors.Is(err, ErrWouldOrphanProject) {
			t.Fatalf("demoting the sole owner to %q: want ErrWouldOrphanProject, got %v",
				role, err)
		}
		// The refusal has to be actionable: it names the remedy, because an operator who
		// cannot see the way out edits the journal by hand instead.
		if !strings.Contains(err.Error(), "Promote somebody else to owner first") {
			t.Errorf("the refusal does not name the remedy: %v", err)
		}
	}

	// 🔴 AND NOTHING WAS WRITTEN. A refusal that appended first would leave the project
	// orphaned anyway and report an error about it.
	after, err := store.Model(ctx)
	if err != nil {
		t.Fatalf("reading: %v", err)
	}
	if got := after.Memberships[owner.Project][owner.User].Role; got != RoleOwner {
		t.Errorf("the sole owner's role is now %q — the refusal wrote anyway", got)
	}
}

// TestASECONDOwnerMakesTheDemotionLEGITIMATE is the control that stops the rule above
// from being "owners can never be demoted".
//
// 🔴 WITHOUT THIS THE GUARD IS INDISTINGUISHABLE FROM A BLANKET REFUSAL, which would
// break the ordinary end of a handover and would pass every assertion in the test above.
func TestASECONDOwnerMakesTheDemotionLEGITIMATE(t *testing.T) {
	store, _, owner := aProvisionedOwner(t)
	second := aSecondUser(t, store)
	ctx := context.Background()

	if _, err := SetMember(ctx, store, NewMembership{
		ProjectID: owner.Project, UserID: second.User,
		Role: RoleOwner, At: provisionClock,
	}); err != nil {
		t.Fatalf("promoting a second owner: %v", err)
	}

	got, err := SetMember(ctx, store, NewMembership{
		ProjectID: owner.Project, UserID: owner.User,
		Role: RoleMember, At: provisionClock.Add(1),
	})
	if err != nil {
		t.Fatalf("demoting one of TWO owners must be allowed, got: %v", err)
	}
	if got.Previous != RoleOwner || got.Role != RoleMember {
		t.Errorf("want owner->member, got %+v", got)
	}
}

// TestAnAlreadyOrphanedProjectStillAcceptsAMembershipChange is the control for
// `prior.Role != RoleOwner`, and it exists because an audit measured that clause SURVIVING
// deletion with both packages' suites green.
//
// 🔴 THE STATE IT NEEDS CANNOT BE BUILT THROUGH `SetMember`, which is the point. A
// zero-owner project is exactly what the guard refuses to create, so the fixture reaches it
// by appending the demotion DIRECTLY — the same route a hand-edited journal or the
// check-then-act window `refuseOrphaning` declares would take. Without this case the clause
// is unfalsifiable: dropping it refuses a legitimate change in a world no other test builds.
//
// ⚠ IT ALSO PINS THE RECOVERY PATH the refusal message now promises. If this went red, the
// message's "that is also how this state is recovered" would be a claim with nothing behind it.
func TestAnAlreadyOrphanedProjectStillAcceptsAMembershipChange(t *testing.T) {
	store, _, owner := aProvisionedOwner(t)
	second := aSecondUser(t, store)
	ctx := context.Background()

	if _, err := SetMember(ctx, store, NewMembership{
		ProjectID: owner.Project, UserID: second.User,
		Role: RoleMember, At: provisionClock,
	}); err != nil {
		t.Fatalf("joining the second user: %v", err)
	}
	// Straight to `Append`, bypassing the guard, to reach the state it prevents.
	if _, err := store.Append(ctx, Event{
		Kind: EventMemberSet, At: provisionClock.Add(1),
		ProjectID: owner.Project, UserID: owner.User, Role: RoleMember,
	}); err != nil {
		t.Fatalf("hand-appending the demotion the guard refuses: %v", err)
	}

	m, err := store.Model(ctx)
	if err != nil {
		t.Fatalf("reading: %v", err)
	}
	for userID, ms := range m.Memberships[owner.Project] {
		if ms.Role == RoleOwner {
			t.Fatalf("the fixture failed to orphan the project — %s is still owner, so "+
				"the assertion below cannot distinguish this clause from its neighbours",
				userID)
		}
	}

	// A membership change in an ALREADY orphaned project must not be refused: nobody is
	// being demoted from owner, so this rule has nothing to say about it.
	got, err := SetMember(ctx, store, NewMembership{
		ProjectID: owner.Project, UserID: second.User,
		Role: RoleAdmin, At: provisionClock.Add(2),
	})
	if err != nil {
		t.Fatalf("changing a MEMBER's role in an already-orphaned project must be "+
			"allowed — this rule is about demoting an owner, and %s is not one. Got: %v",
			second.User, err)
	}
	if got.Previous != RoleMember || got.Role != RoleAdmin {
		t.Errorf("want member->admin, got %+v", got)
	}

	// …and the recovery the refusal message names actually works.
	if _, err := SetMember(ctx, store, NewMembership{
		ProjectID: owner.Project, UserID: second.User,
		Role: RoleOwner, At: provisionClock.Add(3),
	}); err != nil {
		t.Fatalf("restoring an owner must be allowed — the refusal message promises it "+
			"as the recovery path: %v", err)
	}
}

// setMemberErr runs a call expected to fail with the orphan sentinel and returns its text.
func setMemberErr(
	t *testing.T, s Store, project, user ID, role Role,
) string {
	t.Helper()
	_, err := SetMember(context.Background(), s, NewMembership{
		ProjectID: project, UserID: user, Role: role, At: provisionClock,
	})
	if !errors.Is(err, ErrWouldOrphanProject) {
		t.Fatalf("want ErrWouldOrphanProject for role %q, got %v", role, err)
	}
	return err.Error()
}

// TestTheRefusalSaysWHICHCostApplies — the message was measured FALSE for one target role.
//
// 🔴 `MayAdministerProject` RETURNS TRUE FOR AN ADMIN, so "nobody may change its
// membership" is wrong when the sole owner is being made an admin — and the `-member-role`
// help text one file over already says as much, which is the contradiction that gets a
// guard deleted. This pins the two messages APART, because one sentence for both is
// exactly what was wrong.
func TestTheRefusalSaysWHICHCostApplies(t *testing.T) {
	store, _, owner := aProvisionedOwner(t)

	toMember := setMemberErr(t, store, owner.Project, owner.User, RoleMember)
	if !strings.Contains(toMember, "whose membership nobody may change") {
		t.Errorf("the member-demotion refusal lost its administration cost: %s", toMember)
	}

	toAdmin := setMemberErr(t, store, owner.Project, owner.User, RoleAdmin)
	if strings.Contains(toAdmin, "membership nobody may change") {
		t.Errorf("the ADMIN refusal still claims nobody may change membership, which is "+
			"false — an admin may: %s", toAdmin)
	}
	if !strings.Contains(toAdmin, "an admin may still change its membership") {
		t.Errorf("the ADMIN refusal does not say what is actually lost: %s", toAdmin)
	}
	for _, msg := range []string{toMember, toAdmin} {
		if !strings.Contains(msg, "Promote somebody else to owner first") {
			t.Errorf("a refusal lost the remedy: %s", msg)
		}
	}
}

// TestReassertingOwnerOnTheSoleOwnerIsNotRefused — the third clause of the condition.
//
// A blanket "the sole owner may not be touched" would refuse this, and it changes nothing.
func TestReassertingOwnerOnTheSoleOwnerIsNotRefused(t *testing.T) {
	store, _, owner := aProvisionedOwner(t)
	got, err := SetMember(context.Background(), store, NewMembership{
		ProjectID: owner.Project, UserID: owner.User,
		Role: RoleOwner, At: provisionClock,
	})
	if err != nil {
		t.Fatalf("re-asserting owner on the sole owner: %v", err)
	}
	if got.Changed() {
		t.Errorf("Changed() is true for a restated owner role: %+v", got)
	}
}

// TestAMembershipNamingSomethingAbsentIsRefusedBeforeTheWrite covers both id fields, and
// asserts they are DISTINGUISHABLE — with two id-shaped flags on one command line,
// swapping them is the mistake to expect.
func TestAMembershipNamingSomethingAbsentIsRefusedBeforeTheWrite(t *testing.T) {
	store, _, owner := aProvisionedOwner(t)
	ctx := context.Background()

	cases := []struct {
		name string
		req  NewMembership
		want error
	}{
		{
			name: "a project that is not there",
			req: NewMembership{
				ProjectID: "prj_nowhere", UserID: owner.User,
				Role: RoleMember, At: provisionClock,
			},
			want: ErrNoSuchProject,
		},
		{
			name: "a user who is not there",
			req: NewMembership{
				ProjectID: owner.Project, UserID: "usr_nobody",
				Role: RoleMember, At: provisionClock,
			},
			want: ErrNoSuchMember,
		},
		{
			name: "the two ids swapped, which is the realistic typo",
			req: NewMembership{
				ProjectID: ID(owner.User), UserID: ID(owner.Project),
				Role: RoleMember, At: provisionClock,
			},
			want: ErrNoSuchProject,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			before, err := store.Model(ctx)
			if err != nil {
				t.Fatalf("reading: %v", err)
			}
			if _, err := SetMember(ctx, store, tc.req); !errors.Is(err, tc.want) {
				t.Fatalf("want %v, got %v", tc.want, err)
			}
			after, err := store.Model(ctx)
			if err != nil {
				t.Fatalf("reading: %v", err)
			}
			if after.Epoch != before.Epoch {
				t.Errorf("the journal grew from %d to %d on a refused request",
					before.Epoch, after.Epoch)
			}
		})
	}
}

// TestAnUnknownRoleIsRefused — restating `Event.validate`'s rule so the caller can say
// which flag was wrong rather than relaying a sentence about replay.
func TestAnUnknownRoleIsRefused(t *testing.T) {
	store, _, owner := aProvisionedOwner(t)
	second := aSecondUser(t, store)
	_, err := SetMember(context.Background(), store, NewMembership{
		ProjectID: owner.Project, UserID: second.User,
		Role: Role("auditor"), At: provisionClock,
	})
	if err == nil {
		t.Fatal("an unknown role was accepted")
	}
	if !strings.Contains(err.Error(), "auditor") {
		t.Errorf("the refusal does not quote the role it rejected: %v", err)
	}
}

// TestTheEpochReportedIsTheOneTheAppendProduced — pinned because reading it back in a
// second call would race any other writer and report a number this call did not cause.
func TestTheEpochReportedIsTheOneTheAppendProduced(t *testing.T) {
	store, _, owner := aProvisionedOwner(t)
	second := aSecondUser(t, store)
	ctx := context.Background()

	before, err := store.Model(ctx)
	if err != nil {
		t.Fatalf("reading: %v", err)
	}
	got, err := SetMember(ctx, store, NewMembership{
		ProjectID: owner.Project, UserID: second.User,
		Role: RoleMember, At: provisionClock,
	})
	if err != nil {
		t.Fatalf("setting: %v", err)
	}
	if got.Epoch != before.Epoch+1 {
		t.Errorf("epoch is %d, want %d (one event appended)", got.Epoch, before.Epoch+1)
	}
}
