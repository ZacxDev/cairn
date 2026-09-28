package ui

import (
	"context"
	"time"

	"github.com/ZacxDev/cairn/internal/control"
	"github.com/ZacxDev/cairn/internal/invite"
)

// staticInviting is an invite world with no database behind it, so the dispatch tests
// measure ROUTING rather than Postgres. `inviting_test.go` is what drives the real
// `ControlInviting` against a real `control.FileStore` and a real store.
//
// 🔴 ITS `Invitable` IS NOT DERIVED FROM THE PRINCIPAL IT IS HANDED, AND THAT IS DELIBERATE
// FOR A FIXTURE — the same ruling `staticSharing` records. The dispatch tests authenticate
// as a principal with no memberships, so a faithful implementation would return an empty
// list and every rendering assertion over the project page would be vacuous.
type staticInviting struct {
	invitable   []control.NamedProject
	outstanding []invite.Invite
	// reads counts every authority QUESTION this fixture is asked, and it is what
	// `TestEveryContentRouteConsultsTheAuthority` reads for `GET /invite` — the third
	// counter beside `countingSource` and `staticSharing`, because that test requires a
	// PER-ROUTE expectation and a sum was measured to destroy it.
	reads int
	// minted and revoked record the writes, for a test that needs to know a handler
	// reached the authority rather than refusing before it.
	minted  int
	revoked int
	// token is what `Mint` hands back. SYNTHETIC — see `fixtures_test.go` for the rule.
	token string
	// mintErr and revokeErr, when set, are what the respective write returns.
	mintErr   error
	revokeErr error
	// readErr, when set, is what `Outstanding` returns.
	readErr error
	// lastRole and lastProject record what the write was ASKED for, which is what pins
	// that a handler passed the form's values through rather than a default.
	lastRole    control.Role
	lastProject control.ID
	// lastDigest records what `Revoke` was asked to withdraw.
	lastDigest string

	// The REDEMPTION recorders. Both arms of the OAuth callback are counted separately,
	// because the whole defect they exist for is that one of the two was never reached:
	// asserting a SUM would be satisfied by the provisioning arm firing twice.
	redeemCalls      int
	redeemedToken    string
	redeemedProvider string
	redeemedSubject  string
	redeemErr        error

	redeemForCalls       int
	redeemedForToken     string
	redeemedForPrincipal control.Principal
	redeemForErr         error
}

func (s *staticInviting) Invitable(control.Principal) []control.NamedProject {
	s.reads++
	return s.invitable
}

func (s *staticInviting) Outstanding(control.ID) ([]invite.Invite, error) {
	s.reads++
	if s.readErr != nil {
		return nil, s.readErr
	}
	return s.outstanding, nil
}

func (s *staticInviting) Mint(_ context.Context, _ control.Principal, project control.ID,
	role control.Role, _ time.Duration) (string, invite.Invite, error) {

	s.lastProject, s.lastRole = project, role
	if s.mintErr != nil {
		return "", invite.Invite{}, s.mintErr
	}
	s.minted++
	return s.token, invite.Invite{
		Digest:    fixtureInviteDigest,
		ProjectID: project,
		Role:      role,
		Inviter:   testIdentity().Principal.ID,
		CreatedAt: fixtureInviteCreated,
		ExpiresAt: fixtureInviteCreated.Add(invite.DefaultTTL),
	}, nil
}

func (s *staticInviting) Revoke(_ context.Context, _ control.Principal, digest string) error {
	s.lastDigest = digest
	if s.revokeErr != nil {
		return s.revokeErr
	}
	s.revoked++
	return nil
}

// Redeem is the PROVISIONING arm's dependency — a subject the control plane does not hold.
//
// ⚠ THE COMMENT THAT STOOD HERE WAS FALSE AND IS CORRECTED RATHER THAN DELETED, because it
// is the reason nobody noticed the gap. It read: "Not reached by any HTTP row: redemption
// happens on the OAuth callback's `UnprovisionedSubject` arm, and `oauth_test.go` drives
// that with its own stub." The first clause is right; the second was not — measured,
// `oauth_test.go` contained ZERO references to an invite or to `UnprovisionedSubject`, so
// the callback's redemption arm was driven by nothing at all. `internal/ui/README.md`
// carried the same false claim. The path that CREATES A PRINCIPAL was the uncovered one.
//
// It is reached now: `oauth_invite_test.go` drives both arms of the callback through this
// fixture.
func (s *staticInviting) Redeem(_ context.Context, token, provider, subject string) (Redemption, error) {
	s.redeemedToken, s.redeemedProvider, s.redeemedSubject = token, provider, subject
	s.redeemCalls++
	if s.redeemErr != nil {
		return Redemption{}, s.redeemErr
	}
	return Redemption{
		Principal:   provisionedPrincipal(),
		Provisioned: true,
		Project:     fixtureNamedProject.ID,
		Role:        control.RoleMember,
	}, nil
}

// RedeemFor is the SUCCESS path's dependency — a principal the control plane already holds.
//
// 🔴 IT EXISTS BECAUSE THAT PATH WAS UNREACHABLE. See `Inviting.RedeemFor`: a known user's
// exchange succeeds, so the provisioning arm's `errors.As` is false and their invitation
// was silently discarded.
func (s *staticInviting) RedeemFor(_ context.Context, token string, principal control.Principal) (Redemption, error) {
	s.redeemedForToken, s.redeemedForPrincipal = token, principal
	s.redeemForCalls++
	if s.redeemForErr != nil {
		return Redemption{}, s.redeemForErr
	}
	return Redemption{
		Principal:   principal,
		Provisioned: false,
		Project:     fixtureNamedProject.ID,
		Role:        control.RoleMember,
	}, nil
}

var _ Inviting = (*staticInviting)(nil)

// The invite fixtures. EVERY VALUE HERE IS SYNTHETIC — `fixtures_test.go` states the rule
// and `tests/leakscan.py` is what enforces it.
//
// ⚠ THE DATE IS A YEAR-2000 ONE ON PURPOSE. `AGENTS.md`'s leak rule admits exactly that
// year for a fixture that genuinely needs a date, because an obviously-synthetic one makes
// the scanner's remedy unambiguous.
var fixtureInviteCreated = time.Date(2000, 1, 2, 3, 4, 5, 0, time.UTC)

const (
	// fixtureInviteDigest is 64 hex characters, because a real digest is
	// `hex(sha256(token))` and a shorter one would let `shortDigest`'s truncation branch
	// go unexercised — the fixture would take the `len <= shortDigestChars` path instead.
	fixtureInviteDigest = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	// fixtureInviteToken is NOT a credential and could not be one: it is not the preimage
	// of the digest above, nothing verifies it, and no store here holds it.
	fixtureInviteToken = "fixture-invite-token-which-is-not-a-real-capability"
)

// fixtureNamedProject is the project the dispatch fixtures invite into, at `owner` — so
// `conferrableRoles` returns every role and the chooser's filtering is visible. A fixture
// at `admin` is what `TestTheRoleChooserOffersOnlyWhatTheCallerMayConfer` varies.
var fixtureNamedProject = control.NamedProject{
	ID:       control.DerivedID(control.PrefixProject, "quarry"),
	Name:     "quarry",
	HeldRole: control.RoleOwner,
}

// benignInviting is the wired fixture `testConfig` uses, mirroring `benignSharing`.
func benignInviting() *staticInviting {
	return &staticInviting{
		invitable: []control.NamedProject{fixtureNamedProject},
		token:     fixtureInviteToken,
		outstanding: []invite.Invite{{
			Digest:    fixtureInviteDigest,
			ProjectID: fixtureNamedProject.ID,
			Role:      control.RoleMember,
			Inviter:   testIdentity().Principal.ID,
			CreatedAt: fixtureInviteCreated,
			ExpiresAt: fixtureInviteCreated.Add(invite.DefaultTTL),
		}},
	}
}
