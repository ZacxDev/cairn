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

func (s *staticInviting) Redeem(context.Context, string, string, string) (Redemption, error) {
	// Not reached by any HTTP row: redemption happens on the OAuth callback's
	// `UnprovisionedSubject` arm, and `oauth_test.go` drives that with its own stub.
	return Redemption{}, invite.ErrNotRedeemable
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
