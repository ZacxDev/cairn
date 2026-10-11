package ui

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"time"

	g "maragu.dev/gomponents"

	"github.com/ZacxDev/cairn/internal/control"
	"github.com/ZacxDev/cairn/internal/invite"
)

// staticTeamLinks is a team-link world with no database behind it, so the dispatch tests
// measure ROUTING — `staticInviting`'s ruling, including that its `Mintable` is NOT derived
// from the principal it is handed. `teamlinks_test.go` drives the real `ControlTeamLinks`.
type staticTeamLinks struct {
	mintable []MintableTarget
	links    []LinkWithLog
	// reads counts every authority QUESTION — the counter
	// `TestEveryContentRouteConsultsTheAuthority` reads for `GET /team`.
	reads int

	minted, revoked int
	token           string
	mintErr         error
	revokeErr       error
	readErr         error

	lastActor    control.Principal
	lastTargets  []invite.Target
	lastRole     invite.LinkRole
	lastTTL      time.Duration
	lastReusable bool
	lastDigest   string
}

func (s *staticTeamLinks) Mintable(control.Principal) []MintableTarget {
	s.reads++
	return s.mintable
}

func (s *staticTeamLinks) Links(control.Principal) ([]LinkWithLog, error) {
	s.reads++
	if s.readErr != nil {
		return nil, s.readErr
	}
	return s.links, nil
}

func (s *staticTeamLinks) Mint(_ context.Context, actor control.Principal, targets []invite.Target,
	role invite.LinkRole, ttl time.Duration, reusable bool) (string, invite.TeamLink, error) {
	s.lastActor, s.lastTargets, s.lastRole, s.lastTTL, s.lastReusable = actor, targets, role, ttl, reusable
	if s.mintErr != nil {
		return "", invite.TeamLink{}, s.mintErr
	}
	s.minted++
	return s.token, invite.TeamLink{
		Digest: fixtureLinkDigest, Targets: targets, Role: role, Inviter: actor.ID, Reusable: reusable,
		CreatedAt: fixtureInviteCreated, ExpiresAt: fixtureInviteCreated.Add(ttl),
	}, nil
}

func (s *staticTeamLinks) Revoke(_ context.Context, actor control.Principal, digest string) error {
	s.lastActor, s.lastDigest = actor, digest
	if s.revokeErr != nil {
		return s.revokeErr
	}
	s.revoked++
	return nil
}

var _ TeamLinking = (*staticTeamLinks)(nil)

const (
	// fixtureLinkDigest is 64 hex characters for `fixtureInviteDigest`'s reason, and differs
	// from it so a page carrying both can be told apart.
	fixtureLinkDigest = "fedcba9876543210fedcba9876543210fedcba9876543210fedcba9876543210"
	// fixtureLinkToken is NOT a credential: not the preimage of anything, verified by nothing.
	fixtureLinkToken = "fixture-team-link-token-which-is-not-a-real-capability"
)

func fixtureProjectTarget() invite.Target {
	return invite.Target{Kind: invite.TargetProject, ID: fixtureNamedProject.ID}
}

func fixtureScopeTarget() invite.Target {
	return invite.Target{Kind: invite.TargetScope, ID: fixtureNamedScope.ID}
}

// benignTeamLinks is the wired fixture `testConfig` uses.
func benignTeamLinks() *staticTeamLinks {
	return &staticTeamLinks{
		token: fixtureLinkToken,
		mintable: []MintableTarget{
			{Target: fixtureProjectTarget(), Name: fixtureNamedProject.Name, Roles: invite.AllLinkRoles},
			{Target: fixtureScopeTarget(), Name: fixtureNamedScope.Name, Roles: []invite.LinkRole{invite.LinkReader}},
		},
		links: []LinkWithLog{{
			Link: invite.TeamLink{
				Digest: fixtureLinkDigest, Targets: []invite.Target{fixtureProjectTarget()},
				Role: invite.LinkReader, Inviter: testIdentity().Principal.ID, Reusable: true,
				CreatedAt: fixtureInviteCreated, ExpiresAt: fixtureInviteCreated.Add(invite.MaxLinkTTL * 400),
				Redemptions: 2,
			},
			TargetLabels: []string{"project " + fixtureNamedProject.Name},
			Log: []RedemptionEntry{
				{Seq: 1, Who: "wren@notes.example.invalid", At: fixtureInviteCreated, Provisioned: true, Confirmed: true},
				// An UNCONFIRMED spend — the double-callback's losing tab — so every page walk
				// renders both shapes.
				{Seq: 2, Who: "usr_fixture_never_created", At: fixtureInviteCreated, Provisioned: true},
			},
		}},
	}
}

// memLinks is an in-memory `invite.LinkStore`, for `memInvites`' reason: what the UI tests
// measure is `ControlTeamLinks`' AUTHORIZATION and write order. The SQL — the conditional
// `UPDATE`, the single-use CHECK, the expiry boundary — is measured against a real Postgres
// by `internal/pgstore`'s tagged tier.
//
// 🔴 ITS REDEEM AND REVOKE ASK `TeamLink.Redeemable` INSIDE THE LOCK, which is the
// in-memory shape of the conditional `UPDATE` — so a mutant in `StateAt` reaches every
// redemption these tests drive, as it would reach production's SQL through the pinned
// agreement test.
type memLinks struct {
	mu    sync.Mutex
	rows  map[string]invite.TeamLink
	log   map[string][]invite.LinkRedemption
	order []string
}

func newMemLinks() *memLinks {
	return &memLinks{rows: map[string]invite.TeamLink{}, log: map[string][]invite.LinkRedemption{}}
}

func (m *memLinks) CreateLink(l invite.TeamLink) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, dup := m.rows[l.Digest]; dup {
		return errors.New("memLinks: duplicate digest")
	}
	if err := invite.ValidateTargets(l.Targets); err != nil {
		return err
	}
	l.Targets = slices.Clone(l.Targets)
	m.rows[l.Digest] = l
	m.order = append(m.order, l.Digest)
	return nil
}

func (m *memLinks) LinkByToken(token string) (invite.TeamLink, bool, error) {
	return m.LinkByDigest(invite.Digest(token))
}

func (m *memLinks) LinkByDigest(digest string) (invite.TeamLink, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	l, ok := m.rows[digest]
	return l, ok, nil
}

func (m *memLinks) RedeemLink(token string, by control.ID, provisioned bool, at time.Time) (invite.TeamLink, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	d := invite.Digest(token)
	l, ok := m.rows[d]
	if !ok || !l.Redeemable(at) {
		return invite.TeamLink{}, invite.ErrNotRedeemable
	}
	l.Redemptions++
	m.rows[d] = l
	m.log[d] = append(m.log[d], invite.LinkRedemption{Digest: d, Seq: l.Redemptions, By: by, At: at, Provisioned: provisioned})
	return l, nil
}

func (m *memLinks) ConfirmRedemption(digest string, seq int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, r := range m.log[digest] {
		if r.Seq == seq {
			m.log[digest][i].Confirmed = true
			return nil
		}
	}
	return errors.New("memLinks: no such redemption")
}

func (m *memLinks) RevokeLink(digest string, at time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	l, ok := m.rows[digest]
	if !ok || !l.Redeemable(at) {
		return invite.ErrNotRedeemable
	}
	l.RevokedAt = at
	m.rows[digest] = l
	return nil
}

func (m *memLinks) LinksBy(inviter control.ID) ([]invite.TeamLink, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []invite.TeamLink
	for i := len(m.order) - 1; i >= 0; i-- {
		if l := m.rows[m.order[i]]; l.Inviter == inviter {
			out = append(out, l)
		}
	}
	return out, nil
}

func (m *memLinks) LinkRedemptions(digest string) ([]invite.LinkRedemption, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return slices.Clone(m.log[digest]), nil
}

var _ invite.LinkStore = (*memLinks)(nil)

// joinedLabels is a test helper: every label, one string, for a substring-free comparison.
func joinedLabels(labels []string) string { return strings.Join(labels, "|") }

// inviteOnTeam and shareOnTeam render the TEAM PAGE carrying one flow's section — what the
// tests that rendered the old `InvitePage` / `SharePage` were re-aimed at when those pages
// moved onto `/team` (operator decision O-a). Each test still reads the same section
// renderer; it now reads it inside the page that is actually served.
func inviteOnTeam(v InviteView) g.Node {
	return TeamPage(TeamView{Viewer: v.Viewer, CSRF: v.CSRF, App: v.App, Invite: v, NoInviteStore: v.NoStore})
}

func shareOnTeam(v ShareView) g.Node {
	return TeamPage(TeamView{Viewer: v.Viewer, CSRF: v.CSRF, App: v.App, Share: v})
}
