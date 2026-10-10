package ui

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ZacxDev/cairn/internal/control"
	"github.com/ZacxDev/cairn/internal/invite"
)

// TeamLinking is the multi-target link half of the Team page, as an interface for
// [Inviting]'s reason: the handlers are testable without a database, and the
// authorization lives in one implementation rather than at each handler.
//
// 🔴 EVERY ACTOR-TAKING METHOD IS REACHED THROUGH `membershipActor`, LIKE `Inviting`'s. A
// project target is MEMBERSHIP authority, which a narrowed credential must not exercise
// (`membershipActor`'s own comment), and `TestEveryMembershipDecisionActsAsMembershipActor`
// watches this interface too.
type TeamLinking interface {
	// Mintable is every project and scope this actor may put on a link, each with the
	// link roles the actor may confer on it. It is decided by [mayLink], the same
	// predicate [TeamLinking.Mint] refuses with and a redemption re-checks.
	Mintable(actor control.Principal) []MintableTarget

	// Links is every link this actor minted, in every state, with its redemption log.
	// Only the MINTER's own links: see [ControlTeamLinks.Revoke] for why ownership is the
	// minter alone.
	Links(actor control.Principal) ([]LinkWithLog, error)

	// Mint creates a link and returns the token ONCE — `Inviting.Mint`'s rule.
	Mint(ctx context.Context, actor control.Principal, targets []invite.Target, role invite.LinkRole,
		ttl time.Duration, reusable bool) (token string, link invite.TeamLink, err error)

	// Revoke withdraws one of the actor's OWN open links, by digest.
	Revoke(ctx context.Context, actor control.Principal, digest string) error
}

// MintableTarget is one row of the link form's target chooser.
type MintableTarget struct {
	Target invite.Target
	// Name is USER TEXT: the project's name, or the scope's display name.
	Name string
	// Roles is every link role this actor may confer on this target, least privileged
	// first. Never empty — a target with no conferrable role is not mintable.
	Roles []invite.LinkRole
}

// LinkWithLog is a link, its targets as the minter may see them, and its per-redemption
// audit record with each redeemer named.
type LinkWithLog struct {
	Link invite.TeamLink
	// TargetLabels is one display string per target: its NAME while the minter may still
	// confer it, and its bare id marked "no longer yours" once they may not — which is also
	// the row's explanation of why the link stopped working.
	TargetLabels []string
	Log          []RedemptionEntry
}

// RedemptionEntry is one redemption with its redeemer's display name.
type RedemptionEntry struct {
	Seq int
	// Who is the redeemer's display (USER TEXT), or their id if the model no longer holds
	// them.
	Who         string
	At          time.Time
	Provisioned bool
}

// ErrNotLinkable refuses a mint naming a target this actor may not confer at that role —
// including one that does not exist. ONE error, for `ErrNotInvitable`'s reason: a caller
// who could tell "no such scope" from "not yours" could enumerate scopes.
var ErrNotLinkable = errors.New("ui: a target on that link does not exist, or is not yours to confer at that role")

// ErrBadLinkTTL refuses a lifetime outside (0, invite.MaxLinkTTL].
var ErrBadLinkTTL = errors.New("ui: a team link's lifetime must be positive and no longer than invite.MaxLinkTTL")

// linkVerbs is the ONE table mapping a link role to the verbs a SCOPE grant confers.
//
// 🔴 `reader` IS `read` ALONE, AND THAT IS THE WHOLE REASON THIS TABLE IS NOT `roleVerbs`.
// `control.Role` has no read-only member (see `invite.LinkRole`'s comment), so a reader is a
// grant of one verb. `member` and `admin` match `control`'s `roleVerbs` for the same names,
// which `TestLinkVerbsMatchTheControlRoleTable` pins rather than assumes.
var linkVerbs = map[invite.LinkRole]control.VerbSet{
	invite.LinkReader: control.NewVerbSet(control.VerbRead),
	invite.LinkMember: control.NewVerbSet(control.VerbRead, control.VerbWrite),
	invite.LinkAdmin:  control.NewVerbSet(control.VerbRead, control.VerbWrite, control.VerbAdmin),
}

// mayLink is THE authority predicate for team links: may `minter` confer `role` on `t` in
// model `m`. Mint asks it; Mintable asks it; every redemption asks it AGAIN, of the
// minter, against the model as it is at redemption.
//
// # 🔴 ONE PREDICATE, THREE READERS, AND THE THIRD IS THE ONE THAT MATTERS
//
// A link outlives the moment it was minted, and a reusable one outlives it by up to
// `invite.MaxLinkTTL`. If only the mint checked, a minter demoted, removed or deleted after
// minting would leave behind a capability still conferring what they no longer hold. So the
// redemption re-asks this exact function of the MINTER — never of the redeemer, who holds
// nothing yet — and a minter who lost authority over ANY target mints nothing usable.
//
// # 🔴 THE TWO ARMS READ THE TWO AUTHORITY AXES, AND NEITHER READS `Model.Grants`
//
//   - a PROJECT target is membership authority: `control.Role.CanManageMembers` over the
//     minter's own membership (`Model.RoleIn`), and `CanConfer` for the role — the invite
//     flow's `mayManage` and `Mint` rules, unchanged. `reader` on a project is a project
//     grant of `read`, which any role that may manage members may hand out.
//   - a SCOPE target is scope authority: `control.Resolve` of the minter, which is the one
//     function permitted to decide what a principal can do to a scope. The minter must hold
//     `admin` on it — the share flow's rule — AND every verb the role confers: the verbs are
//     independent bits, so an `admin`-only minter cannot hand out `read`. That is "never
//     widen beyond the minter's", stated as a subset.
//
// ⚠ A MINTER THE MODEL NO LONGER HOLDS CONFERS NOTHING. `PrincipalFor` is asked first, so a
// deleted user's links die with them rather than being judged on stale memberships.
func mayLink(m control.Model, minter control.Principal, t invite.Target, role invite.LinkRole) bool {
	if minter.Kind != control.KindUser || minter.ID == "" || !role.Valid() {
		return false
	}
	if _, known := m.PrincipalFor(control.KindUser, minter.ID); !known {
		return false
	}
	switch t.Kind {
	case invite.TargetProject:
		held, member := m.RoleIn(t.ID, minter.ID)
		if !member || !held.CanManageMembers() {
			return false
		}
		if role == invite.LinkReader {
			return true
		}
		return held.CanConfer(control.Role(role))
	case invite.TargetScope:
		want := linkVerbs[role]
		have := control.Resolve(m, minter).VerbsOn(t.ID)
		return have.Has(control.VerbAdmin) && have.Intersect(want) == want
	default:
		return false
	}
}

// ControlTeamLinks is [TeamLinking] over the real control plane and a real link store, and
// it is also the REDEMPTION half: `ControlInviting` hands it every token its own store does
// not know (see `ControlInviting.Redeem`), so the one join path — `/join` → GitHub → the
// callback — redeems both kinds of link.
type ControlTeamLinks struct {
	// Authority is the SAME `*control.Cache` the chain resolves against —
	// `ControlInviting.Authority`'s reason.
	Authority *control.Cache
	// Store is the durable half.
	Store invite.LinkStore
	// Now is the clock. Nil means `time.Now().UTC()`.
	Now func() time.Time
}

var _ TeamLinking = ControlTeamLinks{}

func (c ControlTeamLinks) now() time.Time {
	if c.Now == nil {
		return time.Now().UTC()
	}
	return c.Now().UTC()
}

// Mintable lists what the actor may put on a link.
func (c ControlTeamLinks) Mintable(actor control.Principal) []MintableTarget {
	if actor.Kind != control.KindUser {
		return nil
	}
	m := c.Authority.Model()
	var out []MintableTarget
	add := func(t invite.Target, name string) {
		var roles []invite.LinkRole
		for _, r := range invite.AllLinkRoles {
			if mayLink(m, actor, t, r) {
				roles = append(roles, r)
			}
		}
		if len(roles) > 0 {
			out = append(out, MintableTarget{Target: t, Name: name, Roles: roles})
		}
	}
	// Projects through the membership axis, scopes through `Resolve` — the two arms of
	// `mayLink`, enumerated from the same two sources it reads.
	for _, p := range m.ProjectsManagedBy(actor.ID) {
		add(invite.Target{Kind: invite.TargetProject, ID: p.ID}, p.Name)
	}
	for _, s := range control.Resolve(m, actor).NamedScopes(control.VerbAdmin) {
		add(invite.Target{Kind: invite.TargetScope, ID: s.ID}, s.Name)
	}
	return out
}

// Links is the actor's own links with their logs.
func (c ControlTeamLinks) Links(actor control.Principal) ([]LinkWithLog, error) {
	if actor.Kind != control.KindUser || actor.ID == "" {
		return nil, nil
	}
	links, err := c.Store.LinksBy(actor.ID)
	if err != nil {
		return nil, err
	}
	m := c.Authority.Model()
	out := make([]LinkWithLog, 0, len(links))
	for _, l := range links {
		log, err := c.Store.LinkRedemptions(l.Digest)
		if err != nil {
			return nil, err
		}
		row := LinkWithLog{Link: l}
		for _, t := range l.Targets {
			// 🔴 A NAME ONLY WHILE THE MINTER MAY STILL CONFER THE TARGET AT THE LINK'S
			// ROLE — `mayLink`, the redemption's own re-check — so the page never names a
			// scope through a path the caller no longer has authority over.
			name := ""
			if mayLink(m, actor, t, l.Role) {
				name = targetName(m, t)
			}
			row.TargetLabels = append(row.TargetLabels, targetLabel(t, name))
		}
		for _, red := range log {
			who := string(red.By)
			if p, ok := m.PrincipalFor(control.KindUser, red.By); ok {
				who = p.Display
			}
			row.Log = append(row.Log, RedemptionEntry{Seq: red.Seq, Who: who, At: red.At, Provisioned: red.Provisioned})
		}
		out = append(out, row)
	}
	return out, nil
}

// targetName is a target's display name out of the model, or "" if it holds none.
func targetName(m control.Model, t invite.Target) string {
	switch t.Kind {
	case invite.TargetProject:
		return m.Projects[t.ID].Name
	case invite.TargetScope:
		return m.Scopes[t.ID].DisplayName
	}
	return ""
}

// Mint creates a link over targets the actor may confer at `role`.
func (c ControlTeamLinks) Mint(ctx context.Context, actor control.Principal, targets []invite.Target,
	role invite.LinkRole, ttl time.Duration, reusable bool) (string, invite.TeamLink, error) {

	if !role.Valid() {
		return "", invite.TeamLink{}, fmt.Errorf("%w: %q is not a link role", ErrNotLinkable, role)
	}
	if err := invite.ValidateTargets(targets); err != nil {
		return "", invite.TeamLink{}, err
	}
	if ttl == 0 {
		ttl = invite.DefaultTTL
	}
	if ttl < 0 || ttl > invite.MaxLinkTTL {
		return "", invite.TeamLink{}, ErrBadLinkTTL
	}
	m := c.Authority.Model()
	for _, t := range targets {
		// 🔴 EVERY TARGET, AND ONE REFUSAL REFUSES THE LINK. A link is minted whole or not
		// at all: a partial mint would hand somebody a link conferring less than the page
		// said it would, with nothing on the page to say which part was dropped.
		if !mayLink(m, actor, t, role) {
			return "", invite.TeamLink{}, ErrNotLinkable
		}
	}
	token, digest, err := invite.NewToken()
	if err != nil {
		return "", invite.TeamLink{}, err
	}
	now := c.now()
	link := invite.TeamLink{
		Digest:    digest,
		Targets:   append([]invite.Target(nil), targets...),
		Role:      role,
		Inviter:   actor.ID,
		Reusable:  reusable,
		CreatedAt: now,
		ExpiresAt: now.Add(ttl),
	}
	if err := c.Store.CreateLink(link); err != nil {
		return "", invite.TeamLink{}, err
	}
	return token, link, nil
}

// Revoke withdraws one of the actor's own links.
//
// 🔴 OWNERSHIP IS THE MINTER ALONE, READ FROM THE STORED ROW. A link spans projects and
// scopes, so "an admin of one of its targets" would let somebody who manages ONE target
// revoke a link conferring several others they have no say over — and a link is listed
// only to its minter, so nobody else could see what they were revoking. The cost, stated
// rather than discovered: a co-admin cannot withdraw a colleague's leaked link from this
// page. What bounds that is the re-check — demoting the minter kills every link they made.
//
// ⚠ AN UNKNOWN DIGEST AND SOMEBODY ELSE'S ARE ONE ERROR, so revoke is not a way to ask
// whether a digest exists.
func (c ControlTeamLinks) Revoke(ctx context.Context, actor control.Principal, digest string) error {
	if actor.Kind != control.KindUser || actor.ID == "" || digest == "" {
		return invite.ErrNotRedeemable
	}
	link, known, err := c.Store.LinkByDigest(digest)
	if err != nil {
		return err
	}
	if !known || link.Inviter != actor.ID {
		return invite.ErrNotRedeemable
	}
	return c.Store.RevokeLink(digest, c.now())
}

// openLink reads a presented link and asks every question a redemption must ask BEFORE it
// spends anything: is the link open, does its minter still exist, and may the minter still
// confer every target at the link's role.
func (c ControlTeamLinks) openLink(token string, m control.Model, now time.Time) (invite.TeamLink, error) {
	link, known, err := c.Store.LinkByToken(token)
	if err != nil {
		return invite.TeamLink{}, err
	}
	if !known || !link.Redeemable(now) {
		return invite.TeamLink{}, invite.ErrNotRedeemable
	}
	minter, ok := m.PrincipalFor(control.KindUser, link.Inviter)
	if !ok {
		return invite.TeamLink{}, invite.ErrNotRedeemable
	}
	if err := reCheckMinter(m, minter, link); err != nil {
		return invite.TeamLink{}, err
	}
	return link, nil
}

// reCheckMinter is the redemption-time authority check, spelled once so both redemption
// entry points run the same one.
//
// 🔴 ANY TARGET THE MINTER CAN NO LONGER CONFER REFUSES THE WHOLE LINK, rather than joining
// the rest. A partial join would confer a set nobody chose; the minter's own page shows the
// link and its targets, and "it stopped working" is a state they can see and act on.
//
// ⚠ IT ANSWERS `invite.ErrNotRedeemable`, the same refusal a dead link gets, so the person
// clicking cannot learn anything about the minter's standing from the answer.
func reCheckMinter(m control.Model, minter control.Principal, link invite.TeamLink) error {
	for _, t := range link.Targets {
		if !mayLink(m, minter, t, link.Role) {
			return fmt.Errorf("%w: the minter can no longer confer %s %s at %s",
				invite.ErrNotRedeemable, t.Kind, t.ID, link.Role)
		}
	}
	return nil
}

// linkEvents plans what a redemption writes for one user: exactly one record per selected
// target the user does not already hold at least that much of, and NOTHING for any target
// the link does not name.
//
// 🔴 AN EXISTING MEMBERSHIP IS NEVER OVERWRITTEN — `ErrAlreadyAMember`'s argument. `apply`
// calls `setMembership` unconditionally, so a `member` link redeemed by a project's sole
// owner would DEMOTE them and leave the project ownerless. A held target is skipped instead.
func linkEvents(m control.Model, user control.ID, existing bool, link invite.TeamLink, now time.Time) ([]control.Event, error) {
	var have control.Authorization
	if existing {
		p, ok := m.PrincipalFor(control.KindUser, user)
		if !ok {
			return nil, errors.New("ui: the redeemer is in the model but resolves to no principal")
		}
		have = control.Resolve(m, p)
	}
	var events []control.Event
	grant := func(kind control.ObjectKind, object control.ID, verbs control.VerbSet) error {
		id, err := control.NewID(control.PrefixGrant)
		if err != nil {
			return err
		}
		events = append(events, control.Event{
			Kind: control.EventGranted, At: now, Actor: link.Inviter, GrantID: id,
			SubjectKind: control.KindUser, SubjectID: user,
			ObjectKind: kind, ObjectID: object, Verbs: verbs,
		})
		return nil
	}
	for _, t := range link.Targets {
		switch t.Kind {
		case invite.TargetProject:
			if _, member := m.RoleIn(t.ID, user); member {
				continue
			}
			if link.Role == invite.LinkReader {
				if existing && holdsProjectGrant(m, user, t.ID, linkVerbs[invite.LinkReader]) {
					continue
				}
				if err := grant(control.ObjectProject, t.ID, linkVerbs[invite.LinkReader]); err != nil {
					return nil, err
				}
				continue
			}
			events = append(events, control.Event{
				Kind: control.EventMemberSet, At: now, Actor: link.Inviter,
				ProjectID: t.ID, UserID: user, Role: control.Role(link.Role),
			})
		case invite.TargetScope:
			want := linkVerbs[link.Role]
			if existing && have.VerbsOn(t.ID).Intersect(want) == want {
				continue
			}
			if err := grant(control.ObjectScope, t.ID, want); err != nil {
				return nil, err
			}
		}
	}
	return events, nil
}

// holdsProjectGrant answers whether `user` already holds a live direct grant over the whole
// project carrying at least `verbs` — so a second redemption of a reusable `reader` link by
// the same person does not stack identical grant rows.
//
// ⚠ IT READS `Model.Grants`, AND THAT IS NOT THE AUTHORITY QUESTION. Who may MINT is never
// answered from grant rows (`mayLink`); this only decides whether a write would be a
// duplicate, and getting it wrong in either direction writes one redundant row or skips a
// row the user's existing grant already covers.
func holdsProjectGrant(m control.Model, user, project control.ID, verbs control.VerbSet) bool {
	for _, g := range m.Grants {
		if g.Live() && g.SubjectKind == control.KindUser && g.SubjectID == user &&
			g.ObjectKind == control.ObjectProject && g.ObjectID == project &&
			g.Verbs.Intersect(verbs) == verbs {
			return true
		}
	}
	return false
}

// Redeem is the provisioning path for a team link: a verified provider identity the control
// plane may not hold.
//
// 🔴 `ControlInviting.Redeem`'s WRITE ORDER, FOR ITS REASON: the store is spent FIRST, the
// journal second. `RedeemLink` is the one conditional `UPDATE` in this flow, so it is the
// only step that can refuse the second click on a single-use link.
func (c ControlTeamLinks) Redeem(ctx context.Context, token, provider, subject string) (Redemption, error) {
	if token == "" || provider == "" || subject == "" {
		return Redemption{}, invite.ErrNotRedeemable
	}
	now := c.now()
	m := c.Authority.Model()
	if user, held := m.UserByProviderSubject(provider, subject); held {
		principal, ok := m.PrincipalFor(control.KindUser, user.ID)
		if !ok {
			return Redemption{}, errors.New("ui: the redeemer is in the model but resolves to no principal")
		}
		return c.RedeemFor(ctx, token, principal)
	}
	link, err := c.openLink(token, m, now)
	if err != nil {
		return Redemption{}, err
	}
	userID, err := control.NewID(control.PrefixUser)
	if err != nil {
		return Redemption{}, err
	}
	events, err := linkEvents(m, userID, false, link, now)
	if err != nil {
		return Redemption{}, err
	}
	// 🔴 SPENT HERE — the count incremented and the audit row written — BEFORE any
	// authority is recorded.
	if _, err := c.Store.RedeemLink(token, userID, true, now); err != nil {
		return Redemption{}, err
	}
	all := append([]control.Event{{
		Kind: control.EventUserCreated, At: now, UserID: userID, Provider: provider, Subject: subject,
	}}, events...)
	if _, err := c.Authority.ApplyNow(ctx, all...); err != nil {
		return Redemption{}, fmt.Errorf("ui: the team link was redeemed but the authority could not be "+
			"recorded, so the redemption is logged and confers nothing: %w", err)
	}
	principal, ok := c.Authority.Model().PrincipalFor(control.KindUser, userID)
	if !ok {
		return Redemption{}, errors.New("ui: the redemption was recorded but resolves to no principal")
	}
	return Redemption{Principal: principal, Provisioned: true, Link: true, Targets: len(events)}, nil
}

// RedeemFor redeems a team link for a principal the control plane already holds.
//
// ⚠ A PRINCIPAL WHO ALREADY HOLDS EVERY TARGET IS `ErrAlreadyAMember` AND SPENDS NOTHING —
// the invite flow's rule: the link keeps working for whoever it was actually for.
func (c ControlTeamLinks) RedeemFor(ctx context.Context, token string, principal control.Principal) (Redemption, error) {
	if token == "" || principal.ID == "" || principal.Kind != control.KindUser {
		return Redemption{}, invite.ErrNotRedeemable
	}
	now := c.now()
	m := c.Authority.Model()
	link, err := c.openLink(token, m, now)
	if err != nil {
		return Redemption{}, err
	}
	events, err := linkEvents(m, principal.ID, true, link, now)
	if err != nil {
		return Redemption{}, err
	}
	if len(events) == 0 {
		return Redemption{}, ErrAlreadyAMember
	}
	if _, err := c.Store.RedeemLink(token, principal.ID, false, now); err != nil {
		return Redemption{}, err
	}
	if _, err := c.Authority.ApplyNow(ctx, events...); err != nil {
		return Redemption{}, fmt.Errorf("ui: the team link was redeemed but the authority could not be "+
			"recorded, so the redemption is logged and confers nothing: %w", err)
	}
	return Redemption{Principal: principal, Provisioned: false, Link: true, Targets: len(events)}, nil
}
