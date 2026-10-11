package ui

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ZacxDev/cairn/internal/control"
	"github.com/ZacxDev/cairn/internal/invite"
)

// The TEAM LINK world: `invWorld` plus scopes, a third project, and two users whose scope
// authority is a GRANT of an unusual verb set — which is the shape that separates "holds
// admin" from "holds every verb the link confers".
//
//	invProject (quarry):  owner invOwner, admin invAdmin, member invPlain; scopes tlA, tlB
//	invOther   (foreign): owner invPlain;                                    scope  tlC
//	tlThird    (third):   owner invOwner;                                    scope  tlD
//	tlAdminOnly holds {admin} on tlC; tlReadAdmin holds {read,admin} on tlC.
var (
	tlA         = control.DerivedID(control.PrefixScope, "tl-quarry-a")
	tlB         = control.DerivedID(control.PrefixScope, "tl-quarry-b")
	tlC         = control.DerivedID(control.PrefixScope, "tl-foreign-c")
	tlD         = control.DerivedID(control.PrefixScope, "tl-third-d")
	tlThird     = control.DerivedID(control.PrefixProject, "tl-third")
	tlAdminOnly = control.DerivedID(control.PrefixUser, "tl-admin-only")
	tlReadAdmin = control.DerivedID(control.PrefixUser, "tl-read-admin")
)

func teamWorld() []control.Event {
	at := invClock
	return append(invWorld(),
		control.Event{Kind: control.EventUserCreated, At: at, UserID: tlAdminOnly, Provider: "fixture-provider", Subject: "sub-admin-only"},
		control.Event{Kind: control.EventUserCreated, At: at, UserID: tlReadAdmin, Provider: "fixture-provider", Subject: "sub-read-admin"},
		control.Event{Kind: control.EventProjectCreated, At: at, ProjectID: tlThird, Name: "third", UserID: invOwner},
		control.Event{Kind: control.EventMemberSet, At: at, ProjectID: tlThird, UserID: invOwner, Role: control.RoleOwner},
		control.Event{Kind: control.EventScopeCreated, At: at, ScopeID: tlA, DisplayName: "tl-quarry-a", ProjectID: invProject},
		control.Event{Kind: control.EventScopeCreated, At: at, ScopeID: tlB, DisplayName: "tl-quarry-b", ProjectID: invProject},
		control.Event{Kind: control.EventScopeCreated, At: at, ScopeID: tlC, DisplayName: "tl-foreign-c", ProjectID: invOther},
		control.Event{Kind: control.EventScopeCreated, At: at, ScopeID: tlD, DisplayName: "tl-third-d", ProjectID: tlThird},
		control.Event{Kind: control.EventGranted, At: at, GrantID: control.DerivedID(control.PrefixGrant, "tl-g1"),
			SubjectKind: control.KindUser, SubjectID: tlAdminOnly, ObjectKind: control.ObjectScope, ObjectID: tlC,
			Verbs: control.NewVerbSet(control.VerbAdmin), Actor: invPlain},
		control.Event{Kind: control.EventGranted, At: at, GrantID: control.DerivedID(control.PrefixGrant, "tl-g2"),
			SubjectKind: control.KindUser, SubjectID: tlReadAdmin, ObjectKind: control.ObjectScope, ObjectID: tlC,
			Verbs: control.NewVerbSet(control.VerbRead, control.VerbAdmin), Actor: invPlain},
	)
}

type teamRig struct {
	*invRig
	links *memLinks
	team  *ControlTeamLinks
	// clock is the link layer's clock, movable so expiry can be driven to the boundary.
	clock time.Time
}

func newTeamRig(t *testing.T) *teamRig {
	t.Helper()
	journal := filepath.Join(t.TempDir(), "control.journal")
	store, err := control.OpenFileStore(journal)
	if err != nil {
		t.Fatalf("opening the control journal: %v", err)
	}
	if _, err := store.Append(context.Background(), teamWorld()...); err != nil {
		t.Fatalf("seeding: %v", err)
	}
	cache := control.NewCache(store, control.CacheOptions{Now: func() time.Time { return invClock }})
	if err := cache.Refresh(context.Background()); err != nil {
		t.Fatalf("materializing: %v", err)
	}
	if !cache.Writable() {
		t.Fatal("precondition: the rig's authority is not writable, so every redemption assertion would pass vacuously")
	}
	r := &teamRig{links: newMemLinks(), clock: invClock.Add(time.Minute)}
	r.team = &ControlTeamLinks{Authority: cache, Store: r.links, Now: func() time.Time { return r.clock }}
	inv := newMemInvites()
	r.invRig = &invRig{
		t: t, authority: cache, invites: inv, journal: journal,
		inviting: ControlInviting{Authority: cache, Invites: inv, Now: func() time.Time { return r.clock }, Links: r.team},
	}
	return r
}

// mint is a helper that FAILS the test on a refusal — for setting up a link a case redeems.
func (r *teamRig) mint(minter control.ID, role invite.LinkRole, reusable bool, targets ...invite.Target) (string, invite.TeamLink) {
	r.t.Helper()
	token, link, err := r.team.Mint(context.Background(), r.principal(minter), targets, role, 0, reusable)
	if err != nil {
		r.t.Fatalf("precondition: %s could not mint a %s link over %v: %v", minter, role, targets, err)
	}
	return token, link
}

func scopeT(id control.ID) invite.Target   { return invite.Target{Kind: invite.TargetScope, ID: id} }
func projectT(id control.ID) invite.Target { return invite.Target{Kind: invite.TargetProject, ID: id} }

// strangerSubject is a provider subject the model does not hold, distinct per call.
func strangerSubject(n int) string { return fmt.Sprintf("tl-stranger-%d", n) }

// authorityOf renders a principal's whole scope authority as one sorted string — the
// EXACT-SET comparison every "joins exactly what was selected" assertion uses.
func (r *teamRig) authorityOf(user control.ID) string {
	r.t.Helper()
	m := r.authority.Model()
	p, ok := m.PrincipalFor(control.KindUser, user)
	if !ok {
		r.t.Fatalf("no principal for %s", user)
	}
	auth := control.Resolve(m, p)
	var parts []string
	for _, id := range auth.ScopeIDs(control.VerbRead) {
		parts = append(parts, string(id)+"="+auth.VerbsOn(id).String())
	}
	for _, id := range auth.ScopeIDs(control.VerbWrite) {
		if !auth.Allows(id, control.VerbRead) {
			parts = append(parts, string(id)+"="+auth.VerbsOn(id).String())
		}
	}
	slices.Sort(parts)
	return strings.Join(parts, " ")
}

// TestAProjectLinkNeedsAMemberManager: a project target is membership authority, and only
// a role that `CanManageMembers` may put one on a link — at ANY link role, `reader` too.
// RED under `ui-teamlink-project-arm-stops-asking-who-may-manage`.
func TestAProjectLinkNeedsAMemberManager(t *testing.T) {
	r := newTeamRig(t)
	ctx := context.Background()
	for _, role := range invite.AllLinkRoles {
		if _, _, err := r.team.Mint(ctx, r.principal(invPlain), []invite.Target{projectT(invProject)}, role, 0, false); !errors.Is(err, ErrNotLinkable) {
			t.Errorf("a plain MEMBER minted a %s link over their project (err=%v) — belonging to a project "+
				"must not let you widen who else belongs to it", role, err)
		}
	}
	// POSITIVE CONTROL: the admin may, at every link role (admin may confer admin).
	for _, role := range invite.AllLinkRoles {
		if _, _, err := r.team.Mint(ctx, r.principal(invAdmin), []invite.Target{projectT(invProject)}, role, 0, false); err != nil {
			t.Errorf("POSITIVE CONTROL: the project's admin could not mint a %s link: %v", role, err)
		}
	}
	// An unknown project is the SAME refusal.
	if _, _, err := r.team.Mint(ctx, r.principal(invOwner), []invite.Target{projectT("prj_nosuch")}, invite.LinkReader, 0, false); !errors.Is(err, ErrNotLinkable) {
		t.Errorf("an unknown project gave a distinguishable error: %v", err)
	}
}

// TestAScopeLinkNeedsAdminOnTheScope: a scope target needs `admin` on it, decided by
// `control.Resolve` of the minter. `invPlain` is a MEMBER of the scope's project, so holds
// read+write and not admin. RED under `ui-teamlink-scope-arm-drops-the-admin-requirement`.
func TestAScopeLinkNeedsAdminOnTheScope(t *testing.T) {
	r := newTeamRig(t)
	ctx := context.Background()
	if got := r.authorityOf(invPlain); !strings.Contains(got, string(tlA)+"=read,write") {
		t.Fatalf("INSTRUMENT: the fixture's member does not hold read,write on %s (%s), so the refusal below "+
			"would be about something else", tlA, got)
	}
	if _, _, err := r.team.Mint(ctx, r.principal(invPlain), []invite.Target{scopeT(tlA)}, invite.LinkReader, 0, false); !errors.Is(err, ErrNotLinkable) {
		t.Errorf("a read,write MEMBER put a scope on a link (err=%v): sharing a scope needs admin on it", err)
	}
	// POSITIVE CONTROL.
	if _, _, err := r.team.Mint(ctx, r.principal(invAdmin), []invite.Target{scopeT(tlA)}, invite.LinkReader, 0, false); err != nil {
		t.Errorf("POSITIVE CONTROL: the project admin could not link its own scope: %v", err)
	}
}

// TestALinkCannotConferVerbsItsMinterLacks is "never widen beyond the minter's": verbs are
// independent bits, so holding `admin` does not mean holding what a role confers. RED under
// `ui-teamlink-scope-arm-stops-asking-for-every-verb`.
func TestALinkCannotConferVerbsItsMinterLacks(t *testing.T) {
	r := newTeamRig(t)
	ctx := context.Background()
	for _, tc := range []struct {
		minter control.ID
		role   invite.LinkRole
		ok     bool
	}{
		{tlAdminOnly, invite.LinkReader, false}, // holds admin, not read
		{tlAdminOnly, invite.LinkAdmin, false},
		{tlReadAdmin, invite.LinkReader, true},  // holds read and admin
		{tlReadAdmin, invite.LinkMember, false}, // does not hold write
		{tlReadAdmin, invite.LinkAdmin, false},
	} {
		_, _, err := r.team.Mint(ctx, r.principal(tc.minter), []invite.Target{scopeT(tlC)}, tc.role, 0, false)
		if tc.ok && err != nil {
			t.Errorf("POSITIVE CONTROL: %s could not mint %s over %s, which it holds every verb of: %v", tc.minter, tc.role, tlC, err)
		}
		if !tc.ok && !errors.Is(err, ErrNotLinkable) {
			t.Errorf("%s minted a %s link over %s (err=%v) — it confers a verb the minter does not hold", tc.minter, tc.role, tlC, err)
		}
	}
	// One target out of reach refuses the WHOLE link, and nothing is stored.
	before, _ := r.links.LinksBy(tlReadAdmin)
	if _, _, err := r.team.Mint(ctx, r.principal(tlReadAdmin), []invite.Target{scopeT(tlC), scopeT(tlA)}, invite.LinkReader, 0, false); !errors.Is(err, ErrNotLinkable) {
		t.Errorf("a link with one target out of the minter's reach was minted (err=%v)", err)
	}
	after, _ := r.links.LinksBy(tlReadAdmin)
	if len(after) != len(before) {
		t.Errorf("a refused mint STORED a link (%d -> %d)", len(before), len(after))
	}
}

// TestAMinterWhoLostAuthorityMintsNothingUsable is the redemption-time re-check. The admin
// mints a link, is then REMOVED from the project, and the link must confer nothing — and
// must not be spent or log a redemption. RED under `ui-teamlink-redeem-skips-the-minter-recheck`.
func TestAMinterWhoLostAuthorityMintsNothingUsable(t *testing.T) {
	for _, arm := range []struct {
		name   string
		target invite.Target
	}{{"project target", projectT(invProject)}, {"scope target", scopeT(tlA)}} {
		t.Run(arm.name, func(t *testing.T) {
			r := newTeamRig(t)
			ctx := context.Background()
			token, link := r.mint(invAdmin, invite.LinkReader, true, arm.target)

			// POSITIVE CONTROL first: the link works while the minter holds authority.
			if _, err := r.inviting.Redeem(ctx, token, "fixture-provider", strangerSubject(1)); err != nil {
				t.Fatalf("POSITIVE CONTROL: a fresh link from a current admin was refused: %v", err)
			}
			if _, err := r.authority.ApplyNow(ctx, control.Event{
				Kind: control.EventMemberRemoved, At: invClock, ProjectID: invProject, UserID: invAdmin, Actor: invOwner,
			}); err != nil {
				t.Fatalf("removing the minter: %v", err)
			}
			_, err := r.inviting.Redeem(ctx, token, "fixture-provider", strangerSubject(2))
			if !errors.Is(err, invite.ErrNotRedeemable) {
				t.Fatalf("a link whose minter LOST the authority it confers was redeemed (err=%v)", err)
			}
			if _, held := r.authority.Model().UserByProviderSubject("fixture-provider", strangerSubject(2)); held {
				t.Error("the refused redemption still PROVISIONED a user")
			}
			got, _, _ := r.links.LinkByDigest(link.Digest)
			if got.Redemptions != 1 {
				t.Errorf("the refused redemption was SPENT: the count is %d, want 1 (the control's)", got.Redemptions)
			}
		})
	}
}

// TestASingleUseLinkRedeemsExactlyOnce: reuse unticked is single use, as an invitation is.
// RED under `invite-teamlink-single-use-stops-closing`.
func TestASingleUseLinkRedeemsExactlyOnce(t *testing.T) {
	r := newTeamRig(t)
	ctx := context.Background()
	token, link := r.mint(invOwner, invite.LinkReader, false, scopeT(tlA))
	if _, err := r.inviting.Redeem(ctx, token, "fixture-provider", strangerSubject(1)); err != nil {
		t.Fatalf("the first redemption of a single-use link was refused: %v", err)
	}
	if _, err := r.inviting.Redeem(ctx, token, "fixture-provider", strangerSubject(2)); !errors.Is(err, invite.ErrNotRedeemable) {
		t.Fatalf("a SINGLE-USE link was redeemed a second time, by a second stranger (err=%v)", err)
	}
	// …and by an EXISTING user, through the other entry point.
	if _, err := r.inviting.RedeemFor(ctx, token, r.principal(tlAdminOnly)); !errors.Is(err, invite.ErrNotRedeemable) {
		t.Fatalf("a SINGLE-USE link was redeemed a second time by a known user (err=%v)", err)
	}
	log, _ := r.links.LinkRedemptions(link.Digest)
	if len(log) != 1 {
		t.Errorf("the redemption log holds %d rows, want 1", len(log))
	}
}

// TestAReusableLinkEnrolsEveryoneAndLogsEachRedemption is the operator's reuse decision:
// UNLIMITED redemptions, each one a row in the audit log naming who and whether it created
// the account.
func TestAReusableLinkEnrolsEveryoneAndLogsEachRedemption(t *testing.T) {
	r := newTeamRig(t)
	ctx := context.Background()
	token, link := r.mint(invOwner, invite.LinkReader, true, scopeT(tlA))
	var created []control.ID
	for i := 1; i <= 3; i++ {
		red, err := r.inviting.Redeem(ctx, token, "fixture-provider", strangerSubject(i))
		if err != nil {
			t.Fatalf("redemption %d of a REUSABLE link was refused: %v", i, err)
		}
		if !red.Provisioned || !red.Link {
			t.Errorf("redemption %d: provisioned=%v link=%v, want both true", i, red.Provisioned, red.Link)
		}
		created = append(created, red.Principal.ID)
	}
	// A known user too, through `RedeemFor`.
	if _, err := r.inviting.RedeemFor(ctx, token, r.principal(tlAdminOnly)); err != nil {
		t.Fatalf("a known user could not redeem a reusable link: %v", err)
	}
	log, _ := r.links.LinkRedemptions(link.Digest)
	if len(log) != 4 {
		t.Fatalf("the redemption log holds %d rows, want 4 (three strangers and one known user)", len(log))
	}
	for i, row := range log {
		wantProvisioned := i < 3
		if row.Seq != i+1 || row.Provisioned != wantProvisioned {
			t.Errorf("log row %d is %+v, want seq %d provisioned=%v", i, row, i+1, wantProvisioned)
		}
		if i < 3 && row.By != created[i] {
			t.Errorf("log row %d names %s, want the user that redemption created (%s)", i, row.By, created[i])
		}
	}
	if log[3].By != tlAdminOnly {
		t.Errorf("the known user's row names %s, want %s", log[3].By, tlAdminOnly)
	}
}

// TestARevokedLinkIsNotRedeemable. RED under `invite-teamlink-revoke-stops-closing`.
func TestARevokedLinkIsNotRedeemable(t *testing.T) {
	r := newTeamRig(t)
	ctx := context.Background()
	token, link := r.mint(invOwner, invite.LinkReader, true, scopeT(tlA))
	if _, err := r.inviting.Redeem(ctx, token, "fixture-provider", strangerSubject(1)); err != nil {
		t.Fatalf("POSITIVE CONTROL: the open link was refused: %v", err)
	}
	if err := r.team.Revoke(ctx, r.principal(invOwner), link.Digest); err != nil {
		t.Fatalf("the minter could not revoke their own link: %v", err)
	}
	if _, err := r.inviting.Redeem(ctx, token, "fixture-provider", strangerSubject(2)); !errors.Is(err, invite.ErrNotRedeemable) {
		t.Fatalf("a REVOKED reusable link was redeemed (err=%v)", err)
	}
	if _, err := r.inviting.RedeemFor(ctx, token, r.principal(tlAdminOnly)); !errors.Is(err, invite.ErrNotRedeemable) {
		t.Fatalf("a REVOKED reusable link was redeemed by a known user (err=%v)", err)
	}
	// 🔴 AND THE REVOKE TOOK NOTHING BACK — `TeamHonesty`'s last clause, measured.
	log, _ := r.links.LinkRedemptions(link.Digest)
	if len(log) != 1 || !strings.Contains(r.authorityOf(log[0].By), string(tlA)+"=read") {
		t.Errorf("revoking the link changed what its earlier redeemer holds: log=%v", log)
	}
}

// TestAnExpiredLinkIsNotRedeemable drives the CLOSED boundary: one tick before `ExpiresAt`
// is open, `ExpiresAt` itself is expired. RED under `invite-teamlink-expiry-stops-closing`.
func TestAnExpiredLinkIsNotRedeemable(t *testing.T) {
	r := newTeamRig(t)
	ctx := context.Background()
	token, link := r.mint(invOwner, invite.LinkReader, true, scopeT(tlA))
	r.clock = link.ExpiresAt.Add(-time.Nanosecond)
	if _, err := r.inviting.Redeem(ctx, token, "fixture-provider", strangerSubject(1)); err != nil {
		t.Fatalf("POSITIVE CONTROL: the link one tick before its expiry was refused: %v", err)
	}
	r.clock = link.ExpiresAt
	if _, err := r.inviting.Redeem(ctx, token, "fixture-provider", strangerSubject(2)); !errors.Is(err, invite.ErrNotRedeemable) {
		t.Fatalf("a link AT its expiry instant was redeemed (err=%v)", err)
	}
	r.clock = link.ExpiresAt.Add(time.Hour)
	if _, err := r.inviting.RedeemFor(ctx, token, r.principal(tlAdminOnly)); !errors.Is(err, invite.ErrNotRedeemable) {
		t.Fatalf("an EXPIRED link was redeemed by a known user (err=%v)", err)
	}
}

// TestARedemptionJoinsExactlyTheSelectedTargets: a link over ONE scope of a two-scope project
// and over a whole OTHER project joins exactly those — not the scope's sibling, not the
// scope's project. Compared as the redeemer's WHOLE authority, so an extra grant anywhere is
// visible. RED under `ui-teamlink-scope-target-joins-its-whole-project`.
func TestARedemptionJoinsExactlyTheSelectedTargets(t *testing.T) {
	r := newTeamRig(t)
	ctx := context.Background()
	token, _ := r.mint(invOwner, invite.LinkMember, false, scopeT(tlA), projectT(tlThird))
	red, err := r.inviting.Redeem(ctx, token, "fixture-provider", strangerSubject(1))
	if err != nil {
		t.Fatalf("the redemption was refused: %v", err)
	}
	parts := []string{string(tlA) + "=read,write", string(tlD) + "=read,write"}
	slices.Sort(parts)
	want := strings.Join(parts, " ")
	if got := r.authorityOf(red.Principal.ID); got != want {
		t.Fatalf("the redeemer holds %q, want EXACTLY %q — a link must join the targets it names and nothing else", got, want)
	}
	m := r.authority.Model()
	if _, member := m.RoleIn(invProject, red.Principal.ID); member {
		t.Error("a link naming one SCOPE made the redeemer a member of that scope's whole project")
	}
	if role, member := m.RoleIn(tlThird, red.Principal.ID); !member || role != control.RoleMember {
		t.Errorf("the project target was not joined at member: role=%q member=%v", role, member)
	}
	if red.Targets != 2 {
		t.Errorf("the redemption reports %d record(s), want 2", red.Targets)
	}
}

// TestAProjectReaderLinkGrantsReadOnly: `reader` on a project is a READ GRANT over the whole
// project — no write, no membership — because `control.Role` has no read-only member.
func TestAProjectReaderLinkGrantsReadOnly(t *testing.T) {
	r := newTeamRig(t)
	ctx := context.Background()
	token, _ := r.mint(invOwner, invite.LinkReader, false, projectT(invProject))
	red, err := r.inviting.Redeem(ctx, token, "fixture-provider", strangerSubject(1))
	if err != nil {
		t.Fatalf("the redemption was refused: %v", err)
	}
	ids := []string{string(tlA), string(tlB)}
	slices.Sort(ids)
	want := ids[0] + "=read " + ids[1] + "=read"
	if got := r.authorityOf(red.Principal.ID); got != want {
		t.Fatalf("a project READER holds %q, want exactly %q", got, want)
	}
	if _, member := r.authority.Model().RoleIn(invProject, red.Principal.ID); member {
		t.Error("a project READER link made the redeemer a MEMBER, which writes")
	}
}

// TestOnlyTheMinterCanRevokeALink — even the project's OWNER, who holds more authority than
// the admin who minted it, cannot revoke the admin's link: ownership is the minter alone.
// RED under `ui-teamlink-revoke-skips-the-ownership-check`.
func TestOnlyTheMinterCanRevokeALink(t *testing.T) {
	r := newTeamRig(t)
	ctx := context.Background()
	token, link := r.mint(invAdmin, invite.LinkReader, true, projectT(invProject))
	for _, who := range []control.ID{invOwner, invPlain, tlReadAdmin} {
		if err := r.team.Revoke(ctx, r.principal(who), link.Digest); !errors.Is(err, invite.ErrNotRedeemable) {
			t.Errorf("%s, who did not mint it, revoked the link (err=%v)", who, err)
		}
	}
	if _, err := r.inviting.Redeem(ctx, token, "fixture-provider", strangerSubject(1)); err != nil {
		t.Fatalf("a non-owner's refused revoke still closed the link: %v", err)
	}
	// An unknown digest is the SAME refusal, so revoke cannot ask which digests exist.
	if err := r.team.Revoke(ctx, r.principal(invAdmin), strings.Repeat("0", 64)); !errors.Is(err, invite.ErrNotRedeemable) {
		t.Errorf("an unknown digest gave a distinguishable error: %v", err)
	}
	// POSITIVE CONTROL: the minter may.
	if err := r.team.Revoke(ctx, r.principal(invAdmin), link.Digest); err != nil {
		t.Fatalf("POSITIVE CONTROL: the minter could not revoke their own link: %v", err)
	}
	// Listed to the minter alone.
	if mine, _ := r.team.Links(r.principal(invOwner)); len(mine) != 0 {
		t.Errorf("the project owner was LISTED a link somebody else minted: %+v", mine)
	}
}

// TestALinkNeverOverwritesAnExistingMembership: the project's sole OWNER redeeming a
// `member` link into their own project must not be demoted — `ErrAlreadyAMember`'s
// argument — and the link is not spent.
func TestALinkNeverOverwritesAnExistingMembership(t *testing.T) {
	r := newTeamRig(t)
	ctx := context.Background()
	token, link := r.mint(invAdmin, invite.LinkMember, false, projectT(invProject))
	if _, err := r.inviting.RedeemFor(ctx, token, r.principal(invOwner)); !errors.Is(err, ErrAlreadyAMember) {
		t.Fatalf("the owner redeeming a member link answered %v, want ErrAlreadyAMember", err)
	}
	if role, _ := r.authority.Model().RoleIn(invProject, invOwner); role != control.RoleOwner {
		t.Fatalf("the project's owner is now %q — a team link DEMOTED them", role)
	}
	if got, _, _ := r.links.LinkByDigest(link.Digest); got.Redemptions != 0 {
		t.Errorf("the refused redemption spent the single-use link (count %d)", got.Redemptions)
	}
}

// TestTheJournalNamesTheMinterAsActor: every record a redemption writes carries the MINTER
// as actor — `ControlInviting.Redeem`'s rule — read off the journal, not the model.
func TestTheJournalNamesTheMinterAsActor(t *testing.T) {
	r := newTeamRig(t)
	ctx := context.Background()
	token, _ := r.mint(invOwner, invite.LinkMember, false, projectT(tlThird))
	red, err := r.inviting.Redeem(ctx, token, "fixture-provider", strangerSubject(1))
	if err != nil {
		t.Fatalf("the redemption was refused: %v", err)
	}
	if got := r.memberSetActors(tlThird, red.Principal.ID); len(got) != 1 || got[0] != invOwner {
		t.Errorf("the member-set records actor(s) %v, want exactly [%s] — the minter", got, invOwner)
	}
}

// TestANarrowedOrProjectActorHasNoLinkAuthority: the zero principal (`membershipActor` of a
// narrowed credential) and a PROJECT principal mint nothing and are offered nothing.
func TestANarrowedOrProjectActorHasNoLinkAuthority(t *testing.T) {
	r := newTeamRig(t)
	project, _ := r.authority.Model().PrincipalFor(control.KindProject, invProject)
	for name, actor := range map[string]control.Principal{"narrowed (zero)": {}, "project": project} {
		if got := r.team.Mintable(actor); len(got) != 0 {
			t.Errorf("%s actor is offered %d mintable target(s)", name, len(got))
		}
		if _, _, err := r.team.Mint(context.Background(), actor, []invite.Target{scopeT(tlA)}, invite.LinkReader, 0, false); !errors.Is(err, ErrNotLinkable) {
			t.Errorf("%s actor minted a link (err=%v)", name, err)
		}
	}
	// POSITIVE CONTROL: the owner is offered their two projects and four scopes.
	if got := r.team.Mintable(r.principal(invOwner)); len(got) != 2+3 {
		t.Errorf("POSITIVE CONTROL: the owner is offered %d target(s), want 5 (two projects, three scopes): %+v", len(got), got)
	}
}

// TestMintRefusesABadLifetimeOrTargetSet — the TTL ceiling and the target-set shape.
func TestMintRefusesABadLifetimeOrTargetSet(t *testing.T) {
	r := newTeamRig(t)
	ctx := context.Background()
	owner := r.principal(invOwner)
	one := []invite.Target{scopeT(tlA)}
	for _, ttl := range []time.Duration{-time.Second, invite.MaxLinkTTL + time.Nanosecond} {
		if _, _, err := r.team.Mint(ctx, owner, one, invite.LinkReader, ttl, true); !errors.Is(err, ErrBadLinkTTL) {
			t.Errorf("a lifetime of %v was accepted (err=%v)", ttl, err)
		}
	}
	_, link, err := r.team.Mint(ctx, owner, one, invite.LinkReader, invite.MaxLinkTTL, true)
	if err != nil || !link.ExpiresAt.Equal(link.CreatedAt.Add(invite.MaxLinkTTL)) {
		t.Errorf("the ceiling itself was refused or mis-applied: %v %v", link.ExpiresAt, err)
	}
	_, link, err = r.team.Mint(ctx, owner, one, invite.LinkReader, 0, true)
	if err != nil || !link.ExpiresAt.Equal(link.CreatedAt.Add(invite.DefaultTTL)) {
		t.Errorf("a zero lifetime did not default to invite.DefaultTTL: %v %v", link.ExpiresAt, err)
	}
	many := make([]invite.Target, invite.MaxLinkTargets+1)
	for i := range many {
		many[i] = scopeT(control.ID(fmt.Sprintf("scp_many_%d", i)))
	}
	for name, ts := range map[string][]invite.Target{
		"empty":     nil,
		"duplicate": {scopeT(tlA), scopeT(tlA)},
		"too many":  many,
		"bad kind":  {{Kind: "group", ID: tlA}},
	} {
		if _, _, err := r.team.Mint(ctx, owner, ts, invite.LinkReader, 0, true); !errors.Is(err, invite.ErrBadTarget) {
			t.Errorf("%s target set was accepted (err=%v)", name, err)
		}
	}
	if _, _, err := r.team.Mint(ctx, owner, one, invite.LinkRole("owner"), 0, true); !errors.Is(err, ErrNotLinkable) {
		t.Errorf("an `owner` link was accepted (err=%v) — there is no owner link role", err)
	}
}

// TestLinkVerbsMatchTheControlRoleTable pins `linkVerbs` for the two link roles that share a
// name with a `control.Role` against what membership at that role actually confers.
func TestLinkVerbsMatchTheControlRoleTable(t *testing.T) {
	r := newTeamRig(t)
	m := r.authority.Model()
	for role, user := range map[invite.LinkRole]control.ID{invite.LinkMember: invPlain, invite.LinkAdmin: invAdmin} {
		p, _ := m.PrincipalFor(control.KindUser, user)
		if got := control.Resolve(m, p).VerbsOn(tlA); got != linkVerbs[role] {
			t.Errorf("a %s link confers %s on a scope, while %s membership confers %s", role, linkVerbs[role], role, got)
		}
	}
	if got := linkVerbs[invite.LinkReader]; got != control.NewVerbSet(control.VerbRead) {
		t.Errorf("a reader link confers %s, want read alone", got)
	}
}

// TestAnInvitationStillRedeemsAsAnInvitationWithLinksWired: the dispatch by store must not
// route an INVITATION token to the link store.
func TestAnInvitationStillRedeemsAsAnInvitationWithLinksWired(t *testing.T) {
	r := newTeamRig(t)
	token, _, err := r.inviting.Mint(context.Background(), r.principal(invOwner), tlThird, control.RoleMember, 0)
	if err != nil {
		t.Fatal(err)
	}
	red, err := r.inviting.Redeem(context.Background(), token, "fixture-provider", strangerSubject(1))
	if err != nil {
		t.Fatalf("an invitation was refused once links were wired: %v", err)
	}
	if red.Link || red.Project != tlThird {
		t.Errorf("an invitation redeemed as link=%v project=%s, want an invitation into %s", red.Link, red.Project, tlThird)
	}
	// And an unknown token is refused uniformly — neither store knows it.
	if _, err := r.inviting.Redeem(context.Background(), "no-such-token", "fixture-provider", strangerSubject(2)); !errors.Is(err, invite.ErrNotRedeemable) {
		t.Errorf("an unknown token gave %v", err)
	}
}

// TestTheTeamListShowsTargetsAndNamesEachRedeemer — the minter's view of their own link.
func TestTheTeamListShowsTargetsAndNamesEachRedeemer(t *testing.T) {
	r := newTeamRig(t)
	ctx := context.Background()
	token, _ := r.mint(invOwner, invite.LinkReader, true, scopeT(tlA), projectT(tlThird))
	if _, err := r.inviting.RedeemFor(ctx, token, r.principal(tlAdminOnly)); err != nil {
		t.Fatal(err)
	}
	links, err := r.team.Links(r.principal(invOwner))
	if err != nil || len(links) != 1 {
		t.Fatalf("Links answered %d link(s), %v", len(links), err)
	}
	labels := joinedLabels(links[0].TargetLabels)
	if !strings.Contains(labels, "scope tl-quarry-a") || !strings.Contains(labels, "project third") {
		t.Errorf("the targets are labelled %q", labels)
	}
	if len(links[0].Log) != 1 || links[0].Log[0].Who != r.principal(tlAdminOnly).Display {
		t.Errorf("the log is %+v, want one row naming %q", links[0].Log, r.principal(tlAdminOnly).Display)
	}
	// A target the minter can no longer confer is labelled as such — the row says why the
	// link stopped working.
	if _, err := r.authority.ApplyNow(ctx, control.Event{
		Kind: control.EventMemberSet, At: invClock, ProjectID: tlThird, UserID: invAdmin, Role: control.RoleOwner, Actor: invOwner,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.authority.ApplyNow(ctx, control.Event{
		Kind: control.EventMemberRemoved, At: invClock, ProjectID: tlThird, UserID: invOwner, Actor: invAdmin,
	}); err != nil {
		t.Fatal(err)
	}
	links, _ = r.team.Links(r.principal(invOwner))
	if got := joinedLabels(links[0].TargetLabels); !strings.Contains(got, "project "+string(tlThird)+" (no longer yours to confer)") {
		t.Errorf("a target the minter lost is labelled %q", got)
	}
}
