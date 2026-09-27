package ui

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ZacxDev/cairn/internal/control"
	"github.com/ZacxDev/cairn/internal/invite"
)

// inviteTokenField is the form field an invitation token travels in, spelled ONCE.
//
// 🔴 A FORM FIELD AND NEVER A QUERY PARAMETER, because the value is a bearer capability that
// can create a principal. A query parameter lands in browser history, in the referrer the
// next hop receives and in every access log on the way; a POST body lands in none of those.
// The one place a token legitimately appears in a URL is the invitation LINK itself, which
// is the thing being sent to a person — and the page it opens moves it into this field
// before anything is redeemed.
const inviteTokenField = "invite"

// Inviting is the invitation half of the browser surface, as an interface for the reason
// [Sharing] is one: the handlers are then testable without a database and without a control
// plane, and the authorization lives in one implementation rather than at each handler.
type Inviting interface {
	// Invitable lists the projects this principal may invite somebody into.
	//
	// 🔴 IT TAKES A PRINCIPAL AND NOT AN `Authorization`, WHICH IS THE OPPOSITE OF
	// `Sharing.Administrable` AND THE DIFFERENCE IS NOT AN INCONSISTENCY. Scope authority
	// is frozen into the `Authorization` the request was authenticated against, so
	// `Administrable` must read that value and never re-derive it. Membership is not in an
	// `Authorization` at all — `control.Authorization` has no project dimension — so there
	// is no frozen value to read and the model is the only source. The cost is stated
	// rather than hidden: this reads the model a second time, so a refresh landing
	// mid-request could answer from a newer world than the one that authenticated the
	// caller. That is acceptable HERE and would not be for scopes, because the narrowing
	// this produces is re-checked at the write.
	Invitable(actor control.Principal) []control.NamedProject

	// Outstanding is every invitation naming a project, newest first, in every state.
	Outstanding(project control.ID) ([]invite.Invite, error)

	// Mint creates an invitation and returns the token ONCE.
	//
	// 🔴 THE TOKEN IS RETURNED, NEVER STORED, AND NEVER RE-DERIVABLE. See
	// `invite.NewToken`. A caller that loses it has to mint another.
	Mint(ctx context.Context, actor control.Principal, project control.ID, role control.Role,
		ttl time.Duration) (token string, inv invite.Invite, err error)

	// Revoke withdraws an outstanding invitation by DIGEST, which is the only handle the
	// mint page has — it never sees the tokens.
	Revoke(ctx context.Context, actor control.Principal, digest string) error

	// Redeem accepts an invitation on behalf of a verified provider identity, provisioning
	// a user if the control plane holds none.
	//
	// 🔴 IT TAKES A VERIFIED (provider, subject) PAIR AND NOT AN `identity.Identity`,
	// because the whole point is the case where NO identity could be resolved — the caller
	// has a token the provider signed and nobody this control plane knows. Taking an
	// `Identity` would make the provisioning case unrepresentable.
	Redeem(ctx context.Context, token, provider, subject string) (Redemption, error)
}

// Redemption is what a completed redemption did, so a handler can say so and a log can
// record it.
type Redemption struct {
	// Principal is who the redeemer now is — created by this redemption or already held.
	Principal control.Principal
	// Provisioned is true when this redemption CREATED the user.
	//
	// ⚠ IT EXISTS FOR THE AUDIT LINE, NOT FOR A BRANCH IN THE PAGE. The person sees the
	// same thing either way; an operator reading the log needs to know whether a principal
	// came into existence, because that is the event `-create-user`'s help says only an
	// operator can cause.
	Provisioned bool
	// Project and Role are what the invitation conferred.
	Project control.ID
	Role    control.Role
}

// ErrNotInvitable refuses an invite into a project this actor may not manage.
//
// ⚠ ONE ERROR FOR "no such project" AND "not yours to manage", for `ErrNotPermitted`'s
// reason on the share flow: a caller that could tell them apart could enumerate projects.
var ErrNotInvitable = errors.New("ui: no such project, or it is not yours to invite into")

// ControlInviting is [Inviting] over the real control plane and a real invite store.
type ControlInviting struct {
	// Authority is the SAME `*control.Cache` the authentication chain resolves against,
	// for `ControlSharing`'s reason: an authority computed from one model and a write
	// applied to another is how a page authorises against a world that no longer exists.
	Authority *control.Cache
	// Invites is the durable half.
	Invites invite.Store
	// Now is the clock. Nil means `time.Now().UTC()`.
	Now func() time.Time
}

var _ Inviting = ControlInviting{}

func (c ControlInviting) now() time.Time {
	if c.Now == nil {
		return time.Now().UTC()
	}
	return c.Now().UTC()
}

// Invitable lists the projects the actor may invite into.
//
// ⚠ A PROJECT PRINCIPAL GETS NOTHING, and that follows from the model rather than from a
// check here: `Memberships` is keyed by USER, and this repository's standing decision is
// that a project is not a member of itself. So a project service account invites nobody,
// which is the same direction `internal/control/README.md` records for scope authority.
func (c ControlInviting) Invitable(actor control.Principal) []control.NamedProject {
	// ⚠ THIS GUARD'S REMOVAL IS NOT OBSERVABLE EITHER, AND FOR A STRUCTURAL REASON: ids are
	// NAMESPACED BY PREFIX (`usr_` against `prj_`, see `control.PrefixUser`), so a project's
	// id can never appear as a key in a membership's user map and `ProjectsManagedBy` answers
	// nothing for one whether this line exists or not. A mutation removing it SURVIVED. Kept
	// as an explicit statement of the rule in an authorization path — the alternative is that
	// the rule lives only in the id-prefix convention, which is a long way from here.
	if actor.Kind != control.KindUser {
		return nil
	}
	return c.Authority.Model().ProjectsManagedBy(actor.ID)
}

// Outstanding is the project's invitations. It performs NO authority check.
//
// 🔴 THE CALLER MUST HAVE NARROWED FIRST, AND THAT IS STATED BECAUSE IT IS A TRAP. This
// returns digests, roles and timestamps for a project — a listing that would tell an
// outsider who is being invited where. `handleInvitePage` reaches it only for a project
// [Invitable] returned; a future caller that forgets is the defect this sentence exists to
// prevent. It is not enforced here because the narrowing is a LIST and re-deriving it per
// call would be the second read `Invitable`'s own comment argues against.
func (c ControlInviting) Outstanding(project control.ID) ([]invite.Invite, error) {
	if project == "" {
		return nil, nil
	}
	return c.Invites.ForProject(project)
}

// Mint creates an invitation for a project this actor manages.
func (c ControlInviting) Mint(ctx context.Context, actor control.Principal, project control.ID,
	role control.Role, ttl time.Duration) (string, invite.Invite, error) {

	if !c.mayManage(actor, project) {
		return "", invite.Invite{}, ErrNotInvitable
	}
	if !role.Valid() {
		return "", invite.Invite{}, fmt.Errorf("ui: %q is not a role", role)
	}
	// 🔴 AN INVITATION MAY NOT CONFER MORE THAN THE INVITER HOLDS. Without this an `admin`
	// could mint an `owner` invitation and then redeem it themselves — a privilege
	// escalation with an audit trail that reads as an ordinary join. An owner may confer
	// any role; an admin may not confer `owner`.
	held, _ := c.Authority.Model().RoleIn(project, actor.ID)
	if role == control.RoleOwner && held != control.RoleOwner {
		return "", invite.Invite{}, fmt.Errorf(
			"ui: only an owner may invite another owner (you hold %q)", held)
	}
	if ttl <= 0 {
		ttl = invite.DefaultTTL
	}
	token, digest, err := invite.NewToken()
	if err != nil {
		return "", invite.Invite{}, err
	}
	now := c.now()
	inv := invite.Invite{
		Digest:    digest,
		ProjectID: project,
		Role:      role,
		Inviter:   actor.ID,
		CreatedAt: now,
		ExpiresAt: now.Add(ttl),
	}
	if err := c.Invites.Create(inv); err != nil {
		return "", invite.Invite{}, err
	}
	return token, inv, nil
}

// Revoke withdraws an invitation the actor may manage.
//
// 🔴 THE AUTHORITY IS CHECKED AGAINST THE INVITATION'S OWN PROJECT, READ FROM THE STORED
// ROW — never against a project the caller named. This is `Sharing.Unshare`'s rule: a
// handler that took the project alongside the digest would let somebody who manages project
// A revoke an invitation into project B by naming A in the form.
func (c ControlInviting) Revoke(ctx context.Context, actor control.Principal, digest string) error {
	if digest == "" {
		return invite.ErrNotRedeemable
	}
	rows, err := c.invitesByDigest(digest)
	if err != nil {
		return err
	}
	if rows.ProjectID == "" {
		// Unknown digest. The same error a non-open invitation gets, so this cannot be used
		// to ask whether a digest exists.
		return invite.ErrNotRedeemable
	}
	if !c.mayManage(actor, rows.ProjectID) {
		return ErrNotInvitable
	}
	return c.Invites.RevokeByDigest(digest, c.now())
}

// Redeem is the provisioning path, and the ORDER of its two writes is the whole design.
//
// # 🔴 THE INVITE STORE IS SPENT FIRST, THE JOURNAL SECOND, AND THE REVERSE IS UNSAFE
//
// `InviteStore.Redeem` is one conditional `UPDATE … RETURNING`: exactly one concurrent
// caller can win it. That makes it the only thing in this flow capable of picking a winner,
// so it must come FIRST. The two orders fail differently and the difference is not close:
//
//   - store first (this order): if the journal write then fails, the invitation is spent
//     and no membership exists. The person is told to ask for a new link, and an operator
//     can mint one. Annoying; recoverable; nothing false was recorded.
//   - journal first: two clicks arriving together BOTH read the invitation as open, both
//     write a `member-set`, and only then does one of them lose the update. That is two
//     membership records from one invitation, on an APPEND-ONLY journal with no undo — the
//     one outcome `invite.Store.Redeem`'s own comment says an invite must never produce.
//
// # 🔴 THE USER ID IS MINTED BEFORE THE REDEMPTION, BECAUSE THE REDEMPTION RECORDS IT
//
// `Redeem` stores `redeemed_by`, so the winner's identity has to exist before the write
// that picks the winner. For a returning user that is the id the model already holds; for a
// stranger it is a fresh id that only becomes real if the redemption succeeds. An id minted
// and then discarded costs nothing — ids are not a scarce resource and none was written.
//
// # 🔴 BOTH JOURNAL EVENTS GO IN ONE `Apply`, AND NEITHER IS A NEW KIND
//
// `user-created` and `member-set` both already have writers, which is the constraint the
// handoff records: `Event.validate`'s `default:` arm has no default-accept and
// `replayDroppable` is a closed table, so the FIRST record of a new kind would make an
// image ROLLBACK a total control-plane outage. One `Apply` with both events means a replay
// never sees a membership whose user does not exist.
func (c ControlInviting) Redeem(ctx context.Context, token, provider, subject string) (Redemption, error) {
	if token == "" || provider == "" || subject == "" {
		return Redemption{}, invite.ErrNotRedeemable
	}
	now := c.now()

	inv, known, err := c.Invites.ByToken(token)
	if err != nil {
		return Redemption{}, err
	}
	if !known || !inv.Redeemable(now) {
		// One error for unknown, expired, revoked and already-used. The PAGE decides how
		// much to say; see `invite.ErrNotRedeemable`.
		//
		// ⚠ THIS CHECK IS DEFENCE IN DEPTH AND ITS REMOVAL IS NOT OBSERVABLE — MEASURED, NOT
		// ASSUMED. A mutation making it unsatisfiable SURVIVED the suite, because the store's
		// own conditional `UPDATE` carries the identical three conjuncts and is the gate that
		// must hold (it is the only one that is ATOMIC, which is what picks a winner between
		// two clicks). It is KEPT rather than deleted for two reasons worth more than the line:
		// it avoids minting a `control.NewID` for an invitation that is already dead, and this
		// is an authorization path where a redundant refusal costs nothing and a missing one
		// costs a membership. 🔴 So do not count it as coverage — `TestANonOpenInvitationIs
		// RefusedUniformly` passes with this arm removed, and what it is really measuring is
		// the STORE.
		return Redemption{}, invite.ErrNotRedeemable
	}

	model := c.Authority.Model()
	user, held := model.UserByProviderSubject(provider, subject)
	userID := user.ID
	provisioning := !held
	if provisioning {
		userID, err = control.NewID(control.PrefixUser)
		if err != nil {
			return Redemption{}, err
		}
	}

	// 🔴 THE INVITATION IS SPENT HERE, BEFORE ANY AUTHORITY IS RECORDED. See the doc above.
	redeemed, err := c.Invites.Redeem(token, userID, now)
	if err != nil {
		return Redemption{}, err
	}

	events := make([]control.Event, 0, 2)
	if provisioning {
		events = append(events, control.Event{
			Kind:     control.EventUserCreated,
			At:       now,
			UserID:   userID,
			Provider: provider,
			Subject:  subject,
			// ⚠ NO EMAIL. `identity.Claims` carries none by design and
			// `EventUserCreated` does not require one — see
			// `identity.UnprovisionedSubject`. A user row with no email is a shape this
			// repository's own fixtures already hold.
			//
			// 🔴 AND NO ACTOR. This event is not somebody acting on somebody else; the
			// inviter's decision is recorded on the `member-set` below, which is where a
			// reader asking "who let this person in" will look.
		})
	}
	events = append(events, control.Event{
		Kind:      control.EventMemberSet,
		At:        now,
		ProjectID: redeemed.ProjectID,
		UserID:    userID,
		Role:      redeemed.Role,
		// 🔴 THE ACTOR IS THE INVITER, NOT THE REDEEMER. The journal's question is who
		// conferred this authority, and the answer is the person who minted the invitation —
		// the redeemer only chose to accept. `invite.Invite.Inviter`'s own comment states
		// this is what the field is for.
		Actor: redeemed.Inviter,
	})

	if _, err := c.Authority.ApplyNow(ctx, events...); err != nil {
		// 🔴 THE INVITATION IS ALREADY SPENT AND THAT IS NOT REVERSED HERE. Un-redeeming it
		// would need a second write that could itself fail, and a "redeemed then
		// un-redeemed" row is indistinguishable from a replay to every later reader. The
		// error names the state so an operator reading the log knows to mint a new
		// invitation rather than hunt for a half-applied one.
		return Redemption{}, fmt.Errorf(
			"ui: the invitation was spent but the membership could not be recorded, so a new "+
				"invitation is needed: %w", err)
	}

	principal, ok := c.Authority.Model().PrincipalFor(control.KindUser, userID)
	if !ok {
		// The write reported success, so the model this reads should hold the user.
		// Refusing rather than returning a nameless principal keeps the direction safe:
		// a principal with no display is a session whose writes could not record an actor.
		return Redemption{}, errors.New("ui: the redemption was recorded but resolves to no principal")
	}
	return Redemption{
		Principal:   principal,
		Provisioned: provisioning,
		Project:     redeemed.ProjectID,
		Role:        redeemed.Role,
	}, nil
}

// mayManage is the one authority question this type asks, spelled once.
func (c ControlInviting) mayManage(actor control.Principal, project control.ID) bool {
	if actor.Kind != control.KindUser || project == "" {
		return false
	}
	role, held := c.Authority.Model().RoleIn(project, actor.ID)
	return held && role.CanManageMembers()
}

// invitesByDigest finds one stored invitation by digest.
//
// ⚠ `invite.Store` HAS NO BY-DIGEST READ, DELIBERATELY — every read takes the presented
// TOKEN, and the mint page never holds one. So the project is recovered by listing the
// projects this store can be asked about... which it cannot be, without a project. The
// honest consequence: this scans the actor's OWN invitable projects, which is why [Revoke]
// takes the actor. A `ByDigest` on the interface would be the cleaner shape and is a
// deliberate follow-up rather than an omission: adding a read that takes a digest widens
// `invite.Store`'s stated rule ("every method takes the presented token") and that rule has
// exactly one exception today, argued at `RevokeByDigest`.
func (c ControlInviting) invitesByDigest(digest string) (invite.Invite, error) {
	// The caller is about to be authority-checked against whatever project this returns, so
	// scanning every project would be safe — but it would also be a listing of every
	// project's invitations, so it is scoped to the model's projects and nothing wider.
	for project := range c.Authority.Model().Projects {
		rows, err := c.Invites.ForProject(project)
		if err != nil {
			return invite.Invite{}, err
		}
		for _, inv := range rows {
			if inv.Digest == digest {
				return inv, nil
			}
		}
	}
	return invite.Invite{}, nil
}
