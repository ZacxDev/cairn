package main

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/ZacxDev/cairn/internal/control"
	"github.com/ZacxDev/cairn/internal/invite"
	"github.com/ZacxDev/cairn/internal/ui"
)

// noInvites is an `invite.Store` holding nothing: every token is unknown to it, which is the
// state in which `ControlInviting` must hand the token on to its link half.
type noInvites struct{}

func (noInvites) Create(invite.Invite) error                  { return errors.New("noInvites: read-only") }
func (noInvites) ByToken(string) (invite.Invite, bool, error) { return invite.Invite{}, false, nil }
func (noInvites) Redeem(string, control.ID, time.Time) (invite.Invite, error) {
	return invite.Invite{}, invite.ErrNotRedeemable
}
func (noInvites) Revoke(string, time.Time) error                 { return invite.ErrNotRedeemable }
func (noInvites) RevokeByDigest(string, time.Time) error         { return invite.ErrNotRedeemable }
func (noInvites) ForProject(control.ID) ([]invite.Invite, error) { return nil, nil }

// memLinkStore is the smallest in-memory `invite.LinkStore` the wiring test needs.
type memLinkStore struct {
	mu   sync.Mutex
	rows map[string]invite.TeamLink
	log  []invite.LinkRedemption
}

func (m *memLinkStore) CreateLink(l invite.TeamLink) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rows[l.Digest] = l
	return nil
}
func (m *memLinkStore) LinkByToken(t string) (invite.TeamLink, bool, error) {
	return m.LinkByDigest(invite.Digest(t))
}
func (m *memLinkStore) LinkByDigest(d string) (invite.TeamLink, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	l, ok := m.rows[d]
	return l, ok, nil
}
func (m *memLinkStore) RedeemLink(t string, by control.ID, prov bool, at time.Time) (invite.TeamLink, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	d := invite.Digest(t)
	l, ok := m.rows[d]
	if !ok || !l.Redeemable(at) {
		return invite.TeamLink{}, invite.ErrNotRedeemable
	}
	l.Redemptions++
	m.rows[d] = l
	m.log = append(m.log, invite.LinkRedemption{Digest: d, Seq: l.Redemptions, By: by, At: at, Provisioned: prov})
	return l, nil
}
func (m *memLinkStore) ConfirmRedemption(d string, seq int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.log {
		if m.log[i].Digest == d && m.log[i].Seq == seq {
			m.log[i].Confirmed = true
			return nil
		}
	}
	return errors.New("memLinkStore: no such redemption")
}
func (m *memLinkStore) RevokeLink(string, time.Time) error                      { return invite.ErrNotRedeemable }
func (m *memLinkStore) LinksBy(control.ID) ([]invite.TeamLink, error)           { return nil, nil }
func (m *memLinkStore) LinkRedemptions(string) ([]invite.LinkRedemption, error) { return m.log, nil }

// TestTheWiredInvitationHalfRedeemsATeamLink — round 1 🟡4. It drives the function `main`
// builds both halves with, and requires a link minted through the Team page's half to be
// REDEEMED through the invitation half: the path the callback takes. Measured before
// `wireInvitations` existed: deleting `Links: links` from `main` left this package and the
// Postgres tier green, because nothing redeemed a link through what `main` built.
// RED under `main-drops-the-link-store-from-the-invitation-half`.
func TestTheWiredInvitationHalfRedeemsATeamLink(t *testing.T) {
	authority, _ := seededJournal(t, credentialLive)
	store := &memLinkStore{rows: map[string]invite.TeamLink{}}
	inviting := wireInvitations(authority, noInvites{}, store)

	team := inviting.TeamLinks()
	if team == nil {
		t.Fatal("the wired invitation half carries no team-link half — every link minted would be unredeemable")
	}
	owner, ok := authority.Model().PrincipalFor(control.KindUser, control.DerivedID(control.PrefixUser, "startup-user"))
	if !ok {
		t.Fatal("precondition: the seeded owner is not in the model")
	}
	project := control.DerivedID(control.PrefixProject, "startup-project")
	token, _, err := team.Mint(context.Background(), owner, []invite.Target{{Kind: invite.TargetProject, ID: project}},
		invite.LinkMember, 0, false)
	if err != nil {
		t.Fatalf("the owner could not mint through the wired half: %v", err)
	}
	red, err := inviting.Redeem(context.Background(), token, "fixture-provider", "wired-stranger")
	if err != nil {
		t.Fatalf("a link minted on the Team page was NOT redeemable through the invitation half: %v", err)
	}
	if !red.Link || !red.Provisioned {
		t.Errorf("the redemption reads link=%v provisioned=%v, want both", red.Link, red.Provisioned)
	}
	if role, member := authority.Model().RoleIn(project, red.Principal.ID); !member || role != control.RoleMember {
		t.Errorf("the redeemer is %q (member=%v) in the project, want member", role, member)
	}
	// And `ui.New` accepts what was wired — the same refusal `main` meets.
	if _, ok := inviting.(ui.ControlInviting); !ok {
		t.Errorf("wireInvitations returned %T, not a ui.ControlInviting", inviting)
	}
}
