package ui

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ZacxDev/cairn/internal/control"
	"github.com/ZacxDev/cairn/internal/invite"
)

// memInvites is an in-memory `invite.Store`.
//
// 🔴 A FAKE HERE IS CORRECT AND NOT A SHORTCUT, AND THE BOUNDARY IS WORTH STATING. What
// these tests measure is `ControlInviting`'s AUTHORIZATION and the ORDER of its two writes.
// The SQL — the conditional `UPDATE` that picks a winner, the three `WHERE` conjuncts, the
// expiry boundary against `StateAt` — is measured against a real PostgreSQL 18.6 by the
// build-tagged tier in `internal/pgstore`, which is where a fake would be worthless.
//
// ⚠ SO THE ONE THING THIS FAKE MUST GET RIGHT IS SINGLE-WINNER REDEMPTION, because that is
// the property the ordering test leans on. It holds a mutex and re-checks state inside it,
// which is the in-memory shape of the same conditional update.
type memInvites struct {
	mu   sync.Mutex
	rows map[string]invite.Invite
}

// ⚠ NO CLOCK AND NO INJECTED FAILURE FIELD. Both were written and both were dead: this fake
// takes `at` on every method that needs a time, and the one test that must fail a write does
// it with a READ-ONLY authority rather than a flag. A field a test can set but nothing reads
// is a capability the fake appears to have.
func newMemInvites() *memInvites {
	return &memInvites{rows: map[string]invite.Invite{}}
}

func (m *memInvites) Create(inv invite.Invite) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, dup := m.rows[inv.Digest]; dup {
		return errors.New("memInvites: duplicate digest")
	}
	m.rows[inv.Digest] = inv
	return nil
}

func (m *memInvites) ByToken(token string) (invite.Invite, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	inv, ok := m.rows[invite.Digest(token)]
	return inv, ok, nil
}

func (m *memInvites) Redeem(token string, by control.ID, at time.Time) (invite.Invite, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	d := invite.Digest(token)
	inv, ok := m.rows[d]
	// Re-checked INSIDE the lock: this is the single-winner property the real store gets
	// from one conditional UPDATE.
	if !ok || !inv.Redeemable(at) {
		return invite.Invite{}, invite.ErrNotRedeemable
	}
	inv.RedeemedAt, inv.RedeemedBy = at, by
	m.rows[d] = inv
	return inv, nil
}

func (m *memInvites) Revoke(token string, at time.Time) error {
	return m.RevokeByDigest(invite.Digest(token), at)
}

func (m *memInvites) RevokeByDigest(digest string, at time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	inv, ok := m.rows[digest]
	if !ok || !inv.Redeemable(at) {
		return invite.ErrNotRedeemable
	}
	inv.RevokedAt = at
	m.rows[digest] = inv
	return nil
}

func (m *memInvites) ForProject(project control.ID) ([]invite.Invite, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []invite.Invite
	for _, inv := range m.rows {
		if inv.ProjectID == project {
			out = append(out, inv)
		}
	}
	slices.SortFunc(out, func(a, b invite.Invite) int { return strings.Compare(a.Digest, b.Digest) })
	return out, nil
}

var _ invite.Store = (*memInvites)(nil)

// The invitation world: two projects, and a user who OWNS one and merely BELONGS to the
// other — which is the shape every authorization assertion below needs.
var (
	invOwner   = control.DerivedID(control.PrefixUser, "inv-owner")
	invAdmin   = control.DerivedID(control.PrefixUser, "inv-admin")
	invPlain   = control.DerivedID(control.PrefixUser, "inv-plain")
	invProject = control.DerivedID(control.PrefixProject, "inv-quarry")
	invOther   = control.DerivedID(control.PrefixProject, "inv-foreign")
	invClock   = time.Date(2000, 6, 1, 12, 0, 0, 0, time.UTC)
)

func invWorld() []control.Event {
	at := invClock
	return []control.Event{
		{Kind: control.EventUserCreated, At: at, UserID: invOwner, Provider: "fixture-provider", Subject: "sub-owner"},
		{Kind: control.EventUserCreated, At: at, UserID: invAdmin, Provider: "fixture-provider", Subject: "sub-admin"},
		{Kind: control.EventUserCreated, At: at, UserID: invPlain, Provider: "fixture-provider", Subject: "sub-plain"},
		{Kind: control.EventProjectCreated, At: at, ProjectID: invProject, Name: "quarry", UserID: invOwner},
		{Kind: control.EventProjectCreated, At: at, ProjectID: invOther, Name: "foreign", UserID: invPlain},
		{Kind: control.EventMemberSet, At: at, ProjectID: invProject, UserID: invOwner, Role: control.RoleOwner},
		{Kind: control.EventMemberSet, At: at, ProjectID: invProject, UserID: invAdmin, Role: control.RoleAdmin},
		{Kind: control.EventMemberSet, At: at, ProjectID: invProject, UserID: invPlain, Role: control.RoleMember},
		{Kind: control.EventMemberSet, At: at, ProjectID: invOther, UserID: invPlain, Role: control.RoleOwner},
	}
}

type invRig struct {
	t         *testing.T
	inviting  ControlInviting
	authority *control.Cache
	invites   *memInvites
	// journal is the path the control journal was written to.
	//
	// 🔴 IT IS HELD SO A TEST CAN ASSERT THE RECORDED EVENT AND NOT ONLY THE RESULTING
	// MODEL. `control.Membership` carries no actor — the model keeps WHO BELONGS, not who
	// put them there — so the only place "the inviter is the actor" is observable is the
	// journal itself. A mutation sweep found the actor assertion MISSING for exactly this
	// reason: the test was named for the property and measured the membership instead.
	journal string
}

// newInvRig builds a WRITABLE authority — a real `control.FileStore` on a temp path, which
// is `openShareJournal`'s pattern. A read-only source would make every `ApplyNow` fail and
// the provisioning assertions would pass for the wrong reason.
func newInvRig(t *testing.T) *invRig {
	t.Helper()
	journal := filepath.Join(t.TempDir(), "control.journal")
	store, err := control.OpenFileStore(journal)
	if err != nil {
		t.Fatalf("opening the control journal: %v", err)
	}
	if _, err := store.Append(context.Background(), invWorld()...); err != nil {
		t.Fatalf("seeding: %v", err)
	}
	cache := control.NewCache(store, control.CacheOptions{Now: func() time.Time { return invClock }})
	if err := cache.Refresh(context.Background()); err != nil {
		t.Fatalf("materializing: %v", err)
	}
	if !cache.Writable() {
		t.Fatal("precondition: the rig's authority is not writable, so every provisioning assertion would pass vacuously")
	}
	inv := newMemInvites()
	return &invRig{
		t:         t,
		authority: cache,
		invites:   inv,
		journal:   journal,
		inviting:  ControlInviting{Authority: cache, Invites: inv, Now: func() time.Time { return invClock }},
	}
}

// memberSetActors reads the journal and returns the actor recorded on every `member-set`
// naming (project, user).
//
// ⚠ IT READS THE FILE RATHER THAN THE MODEL, because the model does not keep an actor. That
// is the whole reason this helper exists — see `invRig.journal`.
func (r *invRig) memberSetActors(project, user control.ID) []control.ID {
	r.t.Helper()
	raw, err := os.ReadFile(r.journal)
	if err != nil {
		r.t.Fatalf("reading the journal: %v", err)
	}
	var out []control.ID
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var e control.Event
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			r.t.Fatalf("a journal line is not an Event: %v", err)
		}
		if e.Kind == control.EventMemberSet && e.ProjectID == project && e.UserID == user {
			out = append(out, e.Actor)
		}
	}
	return out
}

func (r *invRig) principal(id control.ID) control.Principal {
	r.t.Helper()
	p, ok := r.authority.Model().PrincipalFor(control.KindUser, id)
	if !ok {
		r.t.Fatalf("no principal for %s", id)
	}
	return p
}

func TestOnlyAProjectsOwnerOrAdminCanMintAnInvitation(t *testing.T) {
	r := newInvRig(t)

	// An owner may.
	if _, _, err := r.inviting.Mint(context.Background(), r.principal(invOwner), invProject, control.RoleMember, 0); err != nil {
		t.Fatalf("an owner could not mint: %v", err)
	}
	// An admin may.
	if _, _, err := r.inviting.Mint(context.Background(), r.principal(invAdmin), invProject, control.RoleMember, 0); err != nil {
		t.Fatalf("an admin could not mint: %v", err)
	}
	// 🔴 A PLAIN MEMBER MAY NOT. This is the clause that decides whether belonging to a
	// project lets you widen who else belongs to it.
	if _, _, err := r.inviting.Mint(context.Background(), r.principal(invPlain), invProject, control.RoleMember, 0); !errors.Is(err, ErrNotInvitable) {
		t.Fatalf("a plain member minted an invitation (err=%v)", err)
	}
	// 🔴 AND THE CROSS-PROJECT CASE: owning ANOTHER project confers nothing here. `invPlain`
	// owns `invOther`, so a check that asked "do you own anything" rather than "do you manage
	// THIS one" would pass.
	if _, _, err := r.inviting.Mint(context.Background(), r.principal(invPlain), invProject, control.RoleMember, 0); !errors.Is(err, ErrNotInvitable) {
		t.Fatal("owning a different project let a caller invite into this one")
	}
	// An unknown project is the same refusal, so it cannot be used to ask which exist.
	if _, _, err := r.inviting.Mint(context.Background(), r.principal(invOwner), "prj_nosuch", control.RoleMember, 0); !errors.Is(err, ErrNotInvitable) {
		t.Fatalf("an unknown project gave a distinguishable error: %v", err)
	}
}

// TestAnAdminCannotMintAnOwnerInvitation is the privilege-escalation case.
//
// 🔴 WITHOUT THIS, AN ADMIN MINTS AN `owner` INVITATION AND REDEEMS IT THEMSELVES. The audit
// trail then reads as an ordinary join: a `member-set` to `owner`, actor the inviter, which
// is also the redeemer. Nothing in the journal would look wrong.
func TestAnAdminCannotMintAnOwnerInvitation(t *testing.T) {
	r := newInvRig(t)
	_, _, err := r.inviting.Mint(context.Background(), r.principal(invAdmin), invProject, control.RoleOwner, 0)
	if err == nil {
		t.Fatal("an admin minted an OWNER invitation — that is an escalation with a clean-looking audit trail")
	}
	// 🔴 THE SENTINEL, NOT THE SENTENCE — AND THE SWAP IS WHAT MAKES THIS A GUARD ON THE
	// REFUSAL RATHER THAN ON ITS WORDING. It asserted `strings.Contains(err.Error(), "only
	// an owner may invite another owner")`, which is this repository's own spelled-guard
	// trap: a reword walks past it, and it says nothing about the one property the HANDLER
	// depends on, which is that `refuseInviteWrite` can RECOGNISE this error with
	// `errors.Is`. An unwrapped error of any wording falls to that function's `default` arm
	// and becomes a 500.
	if !errors.Is(err, ErrRoleNotConferrable) {
		t.Fatalf("refused for a different reason, or with an error no handler can classify "+
			"(`refuseInviteWrite` maps on ErrRoleNotConferrable and answers 500 for anything "+
			"it cannot match): %v", err)
	}
	// The positive half: an OWNER may, or the assertion above would also pass against a
	// blanket refusal of owner invitations.
	if _, _, err := r.inviting.Mint(context.Background(), r.principal(invOwner), invProject, control.RoleOwner, 0); err != nil {
		t.Fatalf("an owner could not mint an owner invitation: %v", err)
	}
}

// TestTheMintedTokenIsReturnedOnceAndOnlyItsDigestIsStored.
func TestTheMintedTokenIsReturnedOnceAndOnlyItsDigestIsStored(t *testing.T) {
	r := newInvRig(t)
	token, inv, err := r.inviting.Mint(context.Background(), r.principal(invOwner), invProject, control.RoleMember, 0)
	if err != nil {
		t.Fatal(err)
	}
	if token == "" {
		t.Fatal("no token was returned, so the invitation could never be redeemed")
	}
	if inv.Digest != invite.Digest(token) {
		t.Fatal("the stored digest is not the token's digest")
	}
	// The stored row must not BE the token, under any field.
	stored, ok, err := r.invites.ByToken(token)
	if err != nil || !ok {
		t.Fatalf("the invitation did not store: ok=%v err=%v", ok, err)
	}
	if stored.Digest == token {
		t.Fatal("the token itself was stored as the digest")
	}
	if stored.Inviter != invOwner {
		t.Errorf("inviter is %q, want %q — the journal's 'who let this person in' comes from here", stored.Inviter, invOwner)
	}
	if stored.ExpiresAt != invClock.Add(invite.DefaultTTL) {
		t.Errorf("a zero ttl did not fall back to invite.DefaultTTL: %v", stored.ExpiresAt)
	}
}

// TestRevokingIsCheckedAgainstTheInvitationsOwnProject is `Sharing.Unshare`'s rule.
func TestRevokingIsCheckedAgainstTheInvitationsOwnProject(t *testing.T) {
	r := newInvRig(t)
	token, inv, err := r.inviting.Mint(context.Background(), r.principal(invOwner), invProject, control.RoleMember, 0)
	if err != nil {
		t.Fatal(err)
	}
	_ = token

	// 🔴 `invPlain` OWNS A DIFFERENT PROJECT AND MUST NOT BE ABLE TO REVOKE THIS. A handler
	// that took the project from the caller rather than from the stored row would let them.
	if err := r.inviting.Revoke(context.Background(), r.principal(invPlain), inv.Digest); !errors.Is(err, ErrNotInvitable) {
		t.Fatalf("a foreign project's owner revoked this invitation (err=%v)", err)
	}
	// The invitation must still be open after the refused attempt — otherwise the refusal
	// happened after the write.
	if after, ok, _ := r.invites.ByToken(token); !ok || after.StateAt(invClock) != invite.StateOpen {
		t.Fatal("the refused revoke still changed the invitation's state")
	}
	// And the owner can.
	if err := r.inviting.Revoke(context.Background(), r.principal(invOwner), inv.Digest); err != nil {
		t.Fatalf("the project's owner could not revoke: %v", err)
	}
	if after, ok, _ := r.invites.ByToken(token); !ok || after.StateAt(invClock) != invite.StateRevoked {
		t.Fatal("the revoke reported success and changed nothing")
	}
}

// TestRedeemingProvisionsAnUnknownSubjectAndRecordsTheInviterAsActor is the operator
// decision, measured.
func TestRedeemingProvisionsAnUnknownSubjectAndRecordsTheInviterAsActor(t *testing.T) {
	r := newInvRig(t)
	token, _, err := r.inviting.Mint(context.Background(), r.principal(invOwner), invProject, control.RoleMember, 0)
	if err != nil {
		t.Fatal(err)
	}

	before := len(r.authority.Model().Users)
	red, err := r.inviting.Redeem(context.Background(), token, "fixture-provider", "sub-a-total-stranger")
	if err != nil {
		t.Fatalf("redeeming: %v", err)
	}
	if !red.Provisioned {
		t.Error("Provisioned is false for a subject the control plane had never seen")
	}
	if red.Project != invProject || red.Role != control.RoleMember {
		t.Errorf("conferred %s/%s, want %s/member", red.Project, red.Role, invProject)
	}

	m := r.authority.Model()
	if after := len(m.Users); after != before+1 {
		t.Fatalf("users went %d -> %d, want exactly one more", before, after)
	}
	// The new user is findable by the provider identity that redeemed — which is what makes
	// their NEXT sign-in resolve rather than refuse again.
	user, held := m.UserByProviderSubject("fixture-provider", "sub-a-total-stranger")
	if !held {
		t.Fatal("the provisioned user is not findable by (provider, subject), so the next sign-in would refuse again")
	}
	if red.Principal.ID != user.ID {
		t.Errorf("the returned principal %q is not the created user %q", red.Principal.ID, user.ID)
	}

	// 🔴 THE MEMBERSHIP'S ACTOR IS THE INVITER, NOT THE REDEEMER. The journal's question is
	// who CONFERRED this authority.
	role, ok := m.RoleIn(invProject, user.ID)
	if !ok || role != control.RoleMember {
		t.Fatalf("the redeemer is not a member: role=%q ok=%v", role, ok)
	}
	// 🔴 THE ASSERTION THIS TEST IS NAMED FOR, AND IT WAS MISSING UNTIL A MUTATION SURVIVED.
	// `Actor: userID` instead of `Actor: redeemed.Inviter` passed every check above, because
	// the MODEL records who belongs and not who put them there. The journal is the only
	// place the answer exists, so this reads the journal.
	actors := r.memberSetActors(invProject, user.ID)
	if len(actors) != 1 {
		t.Fatalf("the journal holds %d member-set records for the redeemer, want exactly 1: %v", len(actors), actors)
	}
	if actors[0] != invOwner {
		t.Fatalf(`the membership's actor is %q, want the INVITER %q.

The journal's question is who CONFERRED this authority. Recording the redeemer makes every
invited join read as self-granted, which is the one thing a grant log exists to answer.`,
			actors[0], invOwner)
	}

	// And the invitation is spent, so the link cannot be reused.
	if again, _, _ := r.invites.ByToken(token); again.StateAt(invClock) != invite.StateRedeemed {
		t.Fatalf("the invitation is %q after redemption, want redeemed", again.StateAt(invClock))
	}
	if _, err := r.inviting.Redeem(context.Background(), token, "fixture-provider", "sub-someone-else"); !errors.Is(err, invite.ErrNotRedeemable) {
		t.Fatalf("a spent invitation was redeemed a second time (err=%v)", err)
	}
}

// TestRedeemingAKnownUserJoinsThemWithoutCreatingASecondAccount.
func TestRedeemingAKnownUserJoinsThemWithoutCreatingASecondAccount(t *testing.T) {
	r := newInvRig(t)
	// `invPlain` exists and is NOT a member of `invProject`... they are, as `member`. Use a
	// project they are not in: invite the OWNER of `invOther` into `invProject` is already
	// true, so instead invite `invAdmin` into `invOther`, whose owner is `invPlain`.
	token, _, err := r.inviting.Mint(context.Background(), r.principal(invPlain), invOther, control.RoleMember, 0)
	if err != nil {
		t.Fatal(err)
	}
	before := len(r.authority.Model().Users)
	if _, held := r.authority.Model().RoleIn(invOther, invAdmin); held {
		t.Fatal("precondition: invAdmin already belongs to invOther, so this measures nothing")
	}

	red, err := r.inviting.Redeem(context.Background(), token, "fixture-provider", "sub-admin")
	if err != nil {
		t.Fatalf("redeeming as a known user: %v", err)
	}
	if red.Provisioned {
		t.Error("Provisioned is true for a user the control plane already held")
	}
	m := r.authority.Model()
	if after := len(m.Users); after != before {
		t.Fatalf("users went %d -> %d: a second account was created for an existing subject", before, after)
	}
	if red.Principal.ID != invAdmin {
		t.Errorf("resolved to %q, want the existing user %q", red.Principal.ID, invAdmin)
	}
	if role, ok := m.RoleIn(invOther, invAdmin); !ok || role != control.RoleMember {
		t.Fatalf("the existing user did not join: role=%q ok=%v", role, ok)
	}
}

// TestANonOpenInvitationIsRefusedUniformly.
//
// 🔴 THIS MEASURES THE STORE, NOT THE SERVICE'S PRE-CHECK — LABELLED BECAUSE A SWEEP PROVED
// IT. Making `ControlInviting.Redeem`'s `Redeemable` arm unsatisfiable leaves every case here
// GREEN, because `invite.Store.Redeem` carries the same three conjuncts atomically and is the
// gate that actually holds. So this is coverage of the STORE contract reached through the
// service, and the service's own arm is defence in depth with no independent test. Both facts
// are stated at the code.
func TestANonOpenInvitationIsRefusedUniformly(t *testing.T) {
	r := newInvRig(t)

	t.Run("unknown token", func(t *testing.T) {
		if _, err := r.inviting.Redeem(context.Background(), "not-a-token", "fixture-provider", "sub-x"); !errors.Is(err, invite.ErrNotRedeemable) {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("revoked", func(t *testing.T) {
		token, inv, err := r.inviting.Mint(context.Background(), r.principal(invOwner), invProject, control.RoleMember, 0)
		if err != nil {
			t.Fatal(err)
		}
		if err := r.inviting.Revoke(context.Background(), r.principal(invOwner), inv.Digest); err != nil {
			t.Fatal(err)
		}
		if _, err := r.inviting.Redeem(context.Background(), token, "fixture-provider", "sub-y"); !errors.Is(err, invite.ErrNotRedeemable) {
			t.Fatalf("a revoked invitation was redeemed: %v", err)
		}
	})
	t.Run("expired", func(t *testing.T) {
		token, _, err := r.inviting.Mint(context.Background(), r.principal(invOwner), invProject, control.RoleMember, time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		// Move the service's clock past the expiry. The STORE's clock moves with it, because
		// both read the same function — a test that moved only one would measure the
		// disagreement rather than the expiry.
		late := invClock.Add(2 * time.Hour)
		svc := ControlInviting{Authority: r.authority, Invites: r.invites, Now: func() time.Time { return late }}
		if _, err := svc.Redeem(context.Background(), token, "fixture-provider", "sub-z"); !errors.Is(err, invite.ErrNotRedeemable) {
			t.Fatalf("an expired invitation was redeemed: %v", err)
		}
	})
	t.Run("an empty provider or subject is not redeemable", func(t *testing.T) {
		token, _, err := r.inviting.Mint(context.Background(), r.principal(invOwner), invProject, control.RoleMember, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, bad := range [][2]string{{"", "sub"}, {"prov", ""}, {"", ""}} {
			if _, err := r.inviting.Redeem(context.Background(), token, bad[0], bad[1]); !errors.Is(err, invite.ErrNotRedeemable) {
				t.Errorf("provider=%q subject=%q was accepted", bad[0], bad[1])
			}
		}
		// The invitation must still be open — a refused redemption must not spend it.
		if inv, _, _ := r.invites.ByToken(token); inv.StateAt(invClock) != invite.StateOpen {
			t.Error("a refused redemption spent the invitation")
		}
	})
}

// TestTheInvitationIsSpentBEFORETheMembershipIsRecorded pins the ORDER, which is the one
// design decision inside `Redeem`.
//
// 🔴 THE TWO ORDERS FAIL DIFFERENTLY AND ONLY ONE FAILURE IS RECOVERABLE. Store-first, a
// failed journal write leaves a spent invitation and no membership: recoverable by minting
// another. Journal-first, two simultaneous clicks both read the invitation as open and both
// write a `member-set` — two memberships from one invitation, on an append-only journal with
// no undo.
//
// The instrument: a READ-ONLY authority, so `ApplyNow` refuses. If the invitation is spent
// afterwards, the store write happened first.
func TestTheInvitationIsSpentBEFORETheMembershipIsRecorded(t *testing.T) {
	r := newInvRig(t)
	token, _, err := r.inviting.Mint(context.Background(), r.principal(invOwner), invProject, control.RoleMember, 0)
	if err != nil {
		t.Fatal(err)
	}

	// A cache over a read-only source cannot write.
	readOnly := control.NewCache(fixtureSource{r.authority.Model()}, control.CacheOptions{Now: func() time.Time { return invClock }})
	if err := readOnly.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if readOnly.Writable() {
		t.Fatal("precondition: the read-only authority reports writable, so this test cannot fail the journal write")
	}
	svc := ControlInviting{Authority: readOnly, Invites: r.invites, Now: func() time.Time { return invClock }}

	_, err = svc.Redeem(context.Background(), token, "fixture-provider", "sub-stranger-2")
	if err == nil {
		t.Fatal("the redemption reported success with an unwritable journal")
	}
	// The error must SAY the invitation was spent, because an operator reading it needs to
	// know to mint a new one rather than hunt for a half-applied write.
	if !strings.Contains(err.Error(), "spent but the membership could not be recorded") {
		t.Errorf("the error does not name the state it left behind: %v", err)
	}
	// 🔴 THE ORDER, OBSERVED: the invitation is REDEEMED even though the journal write failed.
	inv, ok, _ := r.invites.ByToken(token)
	if !ok {
		t.Fatal("the invitation vanished")
	}
	if got := inv.StateAt(invClock); got != invite.StateRedeemed {
		t.Fatalf(`the invitation is %q after a failed journal write, want %q.

If it is still OPEN, the journal write is being attempted FIRST — which is the order that
produces two memberships from one invitation under two simultaneous clicks. See this
function's doc comment.`, got, invite.StateRedeemed)
	}
}

// TestTwoSimultaneousRedemptionsProduceOneMembership is the consequence the ordering exists
// for, driven concurrently.
func TestTwoSimultaneousRedemptionsProduceOneMembership(t *testing.T) {
	r := newInvRig(t)
	token, _, err := r.inviting.Mint(context.Background(), r.principal(invOwner), invProject, control.RoleMember, 0)
	if err != nil {
		t.Fatal(err)
	}

	const racers = 6
	start := make(chan struct{})
	var wg sync.WaitGroup
	results := make([]error, racers)
	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			// The SAME subject: two clicks of one link by one person, which is the ordinary
			// way this race happens.
			_, results[i] = r.inviting.Redeem(context.Background(), token, "fixture-provider", "sub-racer")
		}(i)
	}
	close(start)
	wg.Wait()

	winners, refusals, other := 0, 0, 0
	for _, err := range results {
		switch {
		case err == nil:
			winners++
		case errors.Is(err, invite.ErrNotRedeemable):
			refusals++
		default:
			other++
			t.Errorf("a racer failed with something else: %v", err)
		}
	}
	if winners+refusals+other != racers {
		t.Fatalf("only %d of %d racers reported", winners+refusals+other, racers)
	}
	if winners != 1 {
		t.Fatalf("%d of %d redemptions succeeded, want exactly 1", winners, racers)
	}
	// And exactly one user was created for the one subject.
	m := r.authority.Model()
	if _, held := m.UserByProviderSubject("fixture-provider", "sub-racer"); !held {
		t.Fatal("the winner's user was not created")
	}
	created := 0
	for _, u := range m.Users {
		if u.Provider == "fixture-provider" && u.Subject == "sub-racer" {
			created++
		}
	}
	if created != 1 {
		t.Fatalf("%d users exist for one subject — the race created duplicates", created)
	}
}

func TestInvitableListsOnlyManagedProjectsAndNothingForAProjectPrincipal(t *testing.T) {
	r := newInvRig(t)

	owner := r.inviting.Invitable(r.principal(invOwner))
	if len(owner) != 1 || owner[0].ID != invProject {
		t.Errorf("the owner sees %v, want exactly %s", owner, invProject)
	}
	admin := r.inviting.Invitable(r.principal(invAdmin))
	if len(admin) != 1 || admin[0].ID != invProject {
		t.Errorf("the admin sees %v, want exactly %s", admin, invProject)
	}
	// `invPlain` is a MEMBER of invProject and the OWNER of invOther, so they must see
	// exactly one project — which also proves the list is not simply "every project you
	// touch".
	plain := r.inviting.Invitable(r.principal(invPlain))
	if len(plain) != 1 || plain[0].ID != invOther {
		t.Errorf("the plain member sees %v, want exactly %s", plain, invOther)
	}

	// 🔴 A PROJECT PRINCIPAL INVITES NOBODY. A project is not a member of itself, so a
	// service account for a project cannot widen that project's membership.
	//
	// ⚠ THE PRINCIPAL IS CONSTRUCTED, NOT LOOKED UP, AND THE EARLIER VERSION `t.Skip`ped WHEN
	// THE LOOKUP MISSED — so the assertion never ran and a mutation removing the `Kind` check
	// SURVIVED a green suite. A skip here read exactly like coverage. `Invitable` takes a
	// `control.Principal`, so a hand-built one is precisely the input under test.
	// 🔴 AN INVARIANT GUARD, LABELLED AS ONE. A mutation removing the `Kind` check SURVIVES
	// this: ids are namespaced by prefix, so a project id cannot appear as a membership's user
	// key and the lookup answers nothing either way. It pins the RULE rather than catching a
	// reachable bug, and counting it as regression coverage would be the mislabelling this
	// repository's own evidence rules forbid.
	proj := control.Principal{Kind: control.KindProject, ID: invProject}
	if got := r.inviting.Invitable(proj); len(got) != 0 {
		t.Errorf("a project principal was offered %v — a project is not a member of itself", got)
	}
	// The positive control: the SAME id as a USER principal is also offered nothing (it is
	// not a user), so the assertion above cannot pass merely because the id is unknown.
	if got := r.inviting.Invitable(r.principal(invOwner)); len(got) == 0 {
		t.Error("the control is broken: a real owner was offered nothing, so the zero above proves nothing")
	}
}

// TestTheRoleChooserIsDrivenByTheREALModelsHeldRole is the SEAM, and it exists because a
// mutation sweep found the gap it closes.
//
// 🔴 THE MEASURED HOLE: dropping `HeldRole` from `Model.ProjectsManagedBy` SURVIVED a fully
// green suite. `internal/control` tests `ProjectsManagedBy` against a hand-built Model;
// `TestTheRoleChooserOffersOnlyWhatTheCallerMayConfer` tests the chooser against a hand-built
// `control.NamedProject`. Both were hermetic, both passed, and NEITHER built the combined
// state — so the field that carries the caller's role from the model to the form could be
// deleted and the only visible consequence was a chooser that silently offered nothing at
// all. That is this repository's own "verified in isolation is the new vacuous green".
//
// 🔴 SO WHAT IS ASSERTED IS THE RELATIONSHIP, IN BOTH DIRECTIONS AND BEHAVIOURALLY. The real
// authority's owner must see `owner` offered and the real authority's ADMIN must not — which
// is a claim about `ProjectsManagedBy`, about `Invitable`, about `CanConfer` and about
// `inviteForm` together, and no one of them can satisfy it alone.
func TestTheRoleChooserIsDrivenByTheREALModelsHeldRole(t *testing.T) {
	r := newInvRig(t)

	for _, tc := range []struct {
		who        control.ID
		wantHeld   control.Role
		wantOwner  bool
		wantMember bool
	}{
		{invOwner, control.RoleOwner, true, true},
		{invAdmin, control.RoleAdmin, false, true},
	} {
		projects := r.inviting.Invitable(r.principal(tc.who))
		if len(projects) != 1 {
			t.Fatalf("%s: Invitable returned %d project(s), want 1 — the rig seeds exactly one",
				tc.who, len(projects))
		}
		// The STRUCTURAL half: the field crossing the seam carries the caller's own role.
		if got := projects[0].HeldRole; got != tc.wantHeld {
			t.Errorf("%s: Invitable reported HeldRole %q, want %q. A zero value here is what the mutation "+
				"that survived produced, and its only symptom was a chooser offering nothing",
				tc.who, got, tc.wantHeld)
		}

		// The BEHAVIOURAL half, because a structural check type-checks past a wrong value: the
		// rendered form over that REAL value must offer what the role admits.
		view := InviteView{
			Viewer:   "fixture-viewer",
			CSRF:     renderCSRF,
			Projects: projects,
			Project:  projects[0],
		}
		html := renderNode(t, inviteOnTeam(view))
		if got := strings.Contains(html, `value="`+string(control.RoleOwner)+`"`); got != tc.wantOwner {
			t.Errorf("%s (holds %s): the chooser offers `owner` = %v, want %v",
				tc.who, tc.wantHeld, got, tc.wantOwner)
		}
		if got := strings.Contains(html, `value="`+string(control.RoleMember)+`"`); got != tc.wantMember {
			t.Errorf("%s (holds %s): the chooser offers `member` = %v, want %v",
				tc.who, tc.wantHeld, got, tc.wantMember)
		}
		// 🔴 AND THE FORM MUST BE THERE AT ALL. `inviteForm` renders a sentence instead of a
		// form when nothing is conferrable, which is exactly what a zero `HeldRole` produces
		// — so an assertion that only looked for the ABSENCE of `owner` would be satisfied by
		// a page with no chooser on it.
		if !strings.Contains(html, `action="`+InvitePath+`"`) {
			t.Errorf("%s: no mint form is rendered at all. That is what a zero HeldRole looks like, and "+
				"it satisfies every absence assertion above", tc.who)
		}
	}
}

// TestRedeemForJoinsAPrincipalTheControlPlaneAlreadyHolds is `RedeemFor`'s base case — the
// path a user who already has an account takes.
func TestRedeemForJoinsAPrincipalTheControlPlaneAlreadyHolds(t *testing.T) {
	r := newInvRig(t)
	// `invAdmin` is not in `invOther`, whose owner is `invPlain`.
	token, _, err := r.inviting.Mint(context.Background(), r.principal(invPlain), invOther, control.RoleMember, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, held := r.authority.Model().RoleIn(invOther, invAdmin); held {
		t.Fatal("precondition: invAdmin already belongs to invOther, so this measures nothing")
	}
	before := len(r.authority.Model().Users)

	red, err := r.inviting.RedeemFor(context.Background(), token, r.principal(invAdmin))
	if err != nil {
		t.Fatalf("RedeemFor: %v", err)
	}
	if red.Provisioned {
		t.Error("Provisioned is true on a path that cannot provision — no `user-created` is written here")
	}
	m := r.authority.Model()
	if after := len(m.Users); after != before {
		t.Fatalf("users went %d -> %d: RedeemFor created an account, and it must not", before, after)
	}
	if role, ok := m.RoleIn(invOther, invAdmin); !ok || role != control.RoleMember {
		t.Errorf("membership after redemption is (%q, %v), want (member, true)", role, ok)
	}
}

// TestRedeemForRefusesSomebodyTheProjectAlreadyHolds is the guard that keeps this path from
// being a role-rewrite primitive.
//
// 🔴 THE REFUSAL IS NOT POLITENESS. `Redeem`/`RedeemFor` write a bare `member-set`, and
// `Model.apply` calls `setMembership` UNCONDITIONALLY — it never consults `refuseOrphaning`,
// which exists only on the operator's `PlanMemberSet` path and whose whole job is to refuse
// demoting a project's last owner. So without this check an `admin` could mint a `member`
// invitation into their own project, have the sole OWNER redeem it, and leave the project
// with nobody who may delete it or change its membership.
//
// 🔴 AND IT MUST REFUSE **BEFORE** THE INVITATION IS SPENT, which is the second assertion:
// a refusal that consumed the token would burn a link that was legitimately for somebody
// else.
func TestRedeemForRefusesSomebodyTheProjectAlreadyHolds(t *testing.T) {
	r := newInvRig(t)
	// `invPlain` owns `invOther`. Mint an invitation into it and have its OWNER redeem.
	token, _, err := r.inviting.Mint(context.Background(), r.principal(invPlain), invOther, control.RoleMember, 0)
	if err != nil {
		t.Fatal(err)
	}
	roleBefore, held := r.authority.Model().RoleIn(invOther, invPlain)
	if !held || roleBefore != control.RoleOwner {
		t.Fatalf("precondition: invPlain is (%q, %v) in invOther, want owner — this case is about a "+
			"redemption that would DEMOTE the only owner", roleBefore, held)
	}

	_, err = r.inviting.RedeemFor(context.Background(), token, r.principal(invPlain))
	if !errors.Is(err, ErrAlreadyAMember) {
		t.Fatalf("RedeemFor returned %v, want ErrAlreadyAMember. Without this refusal the redemption "+
			"writes a `member-set` that OVERWRITES the redeemer's role, and `Model.apply` does not "+
			"consult `refuseOrphaning` — so the project's only owner just became a member.", err)
	}
	// The role is untouched…
	if role, ok := r.authority.Model().RoleIn(invOther, invPlain); !ok || role != roleBefore {
		t.Errorf("the owner's role moved to (%q, %v) despite the refusal, want (%q, true)",
			role, ok, roleBefore)
	}
	// 🔴 …AND THE INVITATION IS STILL OPEN. A refusal that spent the token would destroy a
	// link somebody else was meant to use, which is a worse outcome than the refusal itself.
	inv, known, err := r.invites.ByToken(token)
	if err != nil {
		t.Fatalf("reading the invitation back: %v", err)
	}
	if !known {
		t.Fatal("the invitation vanished")
	}
	if !inv.Redeemable(invClock) {
		t.Errorf("the invitation is %q after a refused redemption, want still open — the refusal "+
			"consumed a link that was not the redeemer's to spend", inv.StateAt(invClock))
	}
}

// TestRedeemAlsoRefusesSomebodyTheProjectAlreadyHolds is the TWIN of the case above, and it
// exists because the guard shipped on ONE of the two redemption writers.
//
// 🔴 THE DEFECT IT PINS IS A DISAGREEMENT BETWEEN TWO ENTRY POINTS, NOT A MISSING CHECK.
// `RedeemFor` refused an existing member before the spend; `Redeem` computed the very same
// `held` boolean, used it only to decide whether to MINT a user id, and then wrote
// `member-set` unconditionally. Both are reachable from the callback — `oauth.go` sends the
// provisioning arm to `Redeem` and the success arm to `RedeemFor` — so the rule held on one
// path and not the other, while `RedeemFor`'s own doc cited `Redeem` as the reason the rule
// matters.
//
// 🔴 THE REACHABLE PRODUCTION CASE IS A CONCURRENT DOUBLE-CALLBACK: one account, two open
// invitations into one project, two tabs. Both exchanges fail `UnprovisionedSubject`, so
// both enter the provisioning arm; the first mints and writes `member-set`, and the second
// now reads `held == true` and writes `member-set` AGAIN at its own invitation's role. This
// test drives that second state directly — a subject the model ALREADY holds — because the
// race is what makes it reachable, not what makes it wrong.
//
// ⚠ IT ASSERTS THROUGH `Redeem`, NOT `RedeemFor`. Asserting the fix by calling `RedeemFor`
// would pass on the pre-change code and measure nothing: the whole defect is which function
// the callback reaches.
func TestRedeemAlsoRefusesSomebodyTheProjectAlreadyHolds(t *testing.T) {
	r := newInvRig(t)
	// `invPlain` OWNS `invOther`, and is a subject the model already knows.
	token, _, err := r.inviting.Mint(context.Background(), r.principal(invPlain), invOther, control.RoleMember, 0)
	if err != nil {
		t.Fatal(err)
	}
	roleBefore, held := r.authority.Model().RoleIn(invOther, invPlain)
	if !held || roleBefore != control.RoleOwner {
		t.Fatalf("precondition: invPlain is (%q, %v) in invOther, want owner — this case is about a "+
			"redemption that would DEMOTE the only owner", roleBefore, held)
	}

	_, err = r.inviting.Redeem(context.Background(), token, "fixture-provider", "sub-plain")
	if !errors.Is(err, ErrAlreadyAMember) {
		t.Fatalf("Redeem returned %v, want ErrAlreadyAMember. `Redeem` is the callback's PROVISIONING "+
			"arm, and it reaches an already-held user whenever two redemptions race; without this "+
			"refusal it writes a `member-set` that overwrites the redeemer's role, and `Model.apply` "+
			"never consults `refuseOrphaning` — so the project's only owner just became a member.", err)
	}
	if role, ok := r.authority.Model().RoleIn(invOther, invPlain); !ok || role != roleBefore {
		t.Errorf("the owner's role moved to (%q, %v) despite the refusal, want (%q, true)",
			role, ok, roleBefore)
	}
	// 🔴 AND THE INVITATION IS STILL OPEN — the refusal must not burn a link that was
	// legitimately somebody else's, which is the second half of the twin's contract.
	inv, known, err := r.invites.ByToken(token)
	if err != nil {
		t.Fatalf("reading the invitation back: %v", err)
	}
	if !known {
		t.Fatal("the invitation vanished")
	}
	if !inv.Redeemable(invClock) {
		t.Errorf("the invitation is %q after a refused redemption, want still open", inv.StateAt(invClock))
	}
}
