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
//
// ⚠ THE RULE IS ONLY AS TRUE AS THE READ AT EACH CONSUMER, WHICH IS WHY THIS SENTENCE NOW
// NAMES THE READ. `r.FormValue` merges the URL query into the posted body, so a handler
// spelling it that way accepts the token in a query string however firmly the field is
// declared body-only — `handleOAuthStart` did, for one release. Every consumer that must
// take this value from a BODY uses `r.PostFormValue`; `handleJoinPage` is the one that must
// take it from a QUERY and uses `r.URL.Query().Get`. Neither may be `FormValue`.
//
// 🔴 ONLY THE START ROW'S HALF IS MEASURED, AND SAYING SO IS THE POINT.
// `TestTheGitHubStartRowIgnoresAnInvitationTokenInTheQUERYString` measures
// `handleOAuthStart` and nothing else. `handleJoinPage`'s read is measured by NOTHING:
// respelling it `r.FormValue` leaves `go test ./...` at rc 0 with the whole tree green,
// while the SAME suite reddens for that mutation in `handleOAuthStart` — so the suite can
// go red on this exact change and simply never looks at the join page. An earlier draft of
// this sentence said the test measured the two-consumer rule, which read as coverage it
// does not have. The deterministic remedy is an AST ban whose allowlist would be EMPTY,
// filed separately; a ban is SPELLED rather than structural, so it would WIDEN this rather
// than replace the behavioural test.
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

	// RedeemFor accepts an invitation on behalf of a principal this control plane ALREADY
	// holds, and its existence is a DEFECT FIX rather than a symmetry.
	//
	// 🔴 WITHOUT IT AN EXISTING USER COULD NEVER REDEEM ANYTHING, SILENTLY. The callback's
	// provisioning arm is guarded by `errors.As(err, &identity.UnprovisionedSubject{})`,
	// which requires the exchange to have FAILED for that specific reason. A user the
	// control plane holds exchanges SUCCESSFULLY, so `err == nil`, so that arm is
	// unreachable for them — they were signed in, the invitation was never read, it stayed
	// `open`, and nothing on screen or in the log said so. The comment there pointed at
	// "the authenticated redeem route", which is not in `DeclaredRoutes()` and was never
	// built.
	//
	// 🔴 IT REFUSES A PRINCIPAL WHO IS ALREADY A MEMBER, AND THAT REFUSAL IS LOAD-BEARING
	// RATHER THAN TIDINESS. `Redeem` writes a bare `member-set`, and `Model.apply` calls
	// `setMembership` unconditionally — it does NOT consult `refuseOrphaning`, which lives
	// only on the operator's `PlanMemberSet` path. So a redemption by an existing member
	// would OVERWRITE their role, and an `admin` could mint a `member` invitation into
	// their own project, have the sole OWNER redeem it, and leave the project ownerless —
	// a state `refuseOrphaning`'s own message calls recoverable only by a two-step nobody
	// would know to run. Refusing before anything is spent closes that without reaching
	// into `internal/control` for an unexported guard.
	RedeemFor(ctx context.Context, token string, principal control.Principal) (Redemption, error)

	// TeamLinks is the Team page's multi-target link half — the SAME object this
	// implementation redeems link tokens through. Never nil on a server: `New` refuses an
	// `Inviting` that answers nil ([ErrInvitingWithoutTeamLinks]).
	//
	// 🔴 IT IS READ FROM HERE, NEVER WIRED BESIDE IT, AND THAT IS THE POINT. A separate
	// `Config` field let a server be built half-wired either way — links nothing could
	// redeem, or (measured: deleting `Links: links` in `cmd/cairn-ui` left both tiers green)
	// invitations whose store never handed an unknown token to the link store, so every
	// team link minted refused at the callback. One source makes the first shape
	// unrepresentable and the second a startup refusal.
	TeamLinks() TeamLinking
}

// ErrInvitingWithoutTeamLinks refuses a server whose invitation half carries no team-link
// half. See [Inviting.TeamLinks].
var ErrInvitingWithoutTeamLinks = errors.New("ui: the invitation half carries no team-link half, so team links " +
	"could not be minted, or — worse — could be minted and never redeemed")

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
	// Project and Role are what the invitation conferred. EMPTY for a team link, which
	// names a set rather than one project — see `Link`.
	Project control.ID
	Role    control.Role
	// Link is true when the token was a TEAM LINK (`ControlTeamLinks`), and Targets is how
	// many records the redemption wrote — one per selected target the redeemer did not
	// already hold.
	Link    bool
	Targets int
	// LinkDigest and LinkRole name WHICH link and at what role, for the operator's log line —
	// a link has no single Project/Role, and logging those blank left a provisioning event
	// nobody could attribute. The log prints a digest PREFIX (`shortDigest`), never a token.
	LinkDigest string
	LinkRole   invite.LinkRole
	// Unconfirmed is non-nil when the join was recorded but its audit row could not be
	// confirmed (`ControlTeamLinks.confirmed`): the caller logs it, the person is signed in.
	Unconfirmed error
}

// logFields is what an operator's log line says a redemption conferred: the project and role
// for an invitation; the link's digest PREFIX, its role and how many records it wrote for a
// team link — never a token — plus a note when the join's audit row could not be confirmed.
func (r Redemption) logFields() string {
	if !r.Link {
		return fmt.Sprintf("project=%s role=%s", r.Project, r.Role)
	}
	out := fmt.Sprintf("link=%s role=%s targets=%d", shortDigest(r.LinkDigest), r.LinkRole, r.Targets)
	if r.Unconfirmed != nil {
		out += fmt.Sprintf(" AUDIT-ROW-UNCONFIRMED(%v)", r.Unconfirmed)
	}
	return out
}

// ErrNotInvitable refuses an invite into a project this actor may not manage.
//
// ⚠ ONE ERROR FOR "no such project" AND "not yours to manage", for `ErrNotPermitted`'s
// reason on the share flow: a caller that could tell them apart could enumerate projects.
var ErrNotInvitable = errors.New("ui: no such project, or it is not yours to invite into")

// ErrRoleNotConferrable refuses an invitation at a role this actor may not hand out.
//
// 🔴 IT IS A SENTINEL RATHER THAN A BARE `fmt.Errorf`, AND THAT IS WHAT MAKES THE HANDLER
// ABLE TO ANSWER IT AT ALL. `refuseInviteWrite` maps errors onto statuses with `errors.Is`,
// so an unwrapped error falls to its `default` arm and becomes a **500** — which for a
// caller who picked a role their own standing does not permit is both the wrong status and a
// sentence that sends an operator looking for a broken server. The two cases it covers are
// deliberately ONE sentinel: "that is not a role at all" and "that role is above yours" have
// to be answered identically on the wire, because a form posting a bogus role and a form
// posting `owner` are the same user error from the same control.
//
// ⚠ THE WRAPPED MESSAGES DIFFER, AND THAT IS ON PURPOSE. The operator's log gets which of
// the two it was, and which roles were involved; the page gets one fixed sentence
// (`roleRefusal`). Discriminating in the log and not on the wire is this surface's standing
// pattern — see `signInRefused` for the case where even the log line is the point.
var ErrRoleNotConferrable = errors.New("ui: that role cannot be conferred by the role you hold")

// ErrAlreadyAMember refuses a redemption by somebody the project already holds.
//
// ⚠ IT IS NOT UNIFORM WITH `invite.ErrNotRedeemable`, AND THE ASYMMETRY IS DELIBERATE.
// Every other refusal on this flow is collapsed so that a caller cannot learn facts about
// somebody ELSE's invitation. This one is a fact about the CALLER's own membership in a
// project they are authenticated for, which is the same admissibility argument
// `roleRefusal` rests on — and telling them "you are already in" is the only answer that
// does not read as a broken link.
//
// 🔴 IT IS RETURNED BEFORE THE INVITATION IS SPENT, so the link keeps working for whoever
// it was actually for.
var ErrAlreadyAMember = errors.New("ui: that invitation is for a project you are already in")

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
	// Links, when set, redeems every token `Invites` does not know.
	//
	// 🔴 THE TEAM LINK RIDES THE INVITATION'S ONE JOIN PATH RATHER THAN OPENING A SECOND. A
	// link is `/join?invite=<token>` exactly as an invitation is, so `GET /join`, the flight
	// that carries the token and the callback's two redemption arms are unchanged and serve
	// both. The dispatch is by STORE, never by a token prefix or a form field: the two
	// tables are disjoint by digest, and a field naming which kind to try would be a value
	// the presenter chooses.
	Links *ControlTeamLinks
}

var _ Inviting = ControlInviting{}

// TeamLinks answers the link half this value redeems through, or nil.
//
// ⚠ THE NIL IS RETURNED EXPLICITLY RATHER THAN AS `*c.Links`'s interface, which would be a
// non-nil `TeamLinking` over a zero struct whenever `Links` is nil — the typed-nil trap
// `cmd/cairn-ui` already records for `Inviting` itself.
func (c ControlInviting) TeamLinks() TeamLinking {
	if c.Links == nil {
		return nil
	}
	return *c.Links
}

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
// outsider who is being invited where. `inviteSection` reaches it only for a project
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
		return "", invite.Invite{}, fmt.Errorf("%w: %q is not a role", ErrRoleNotConferrable, role)
	}
	// 🔴 AN INVITATION MAY NOT CONFER MORE THAN THE INVITER HOLDS. Without this an `admin`
	// could mint an `owner` invitation and then redeem it themselves — a privilege
	// escalation with an audit trail that reads as an ordinary join.
	//
	// 🔴 THE RULE ITSELF IS `control.Role.CanConfer` AND IS NOT SPELLED HERE, WHICH IS A
	// CONSOLIDATION RATHER THAN AN INDIRECTION. The role CHOOSER on the mint page has to
	// ask the same question — a select offering `owner` to an admin offers a value this
	// function then refuses — so the condition had two readers the moment the page existed.
	// It used to read `role == control.RoleOwner && held != control.RoleOwner` right here,
	// which is correct and is the spelling the second reader would have copied.
	held, _ := c.Authority.Model().RoleIn(project, actor.ID)
	if !held.CanConfer(role) {
		return "", invite.Invite{}, fmt.Errorf(
			"%w: a %q may not confer %q (see control.Role.CanConfer)",
			ErrRoleNotConferrable, held, role)
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
	if !known && c.Links != nil {
		// Not an invitation: a team link, or nothing. See [ControlInviting.Links].
		return c.Links.Redeem(ctx, token, provider, subject)
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

	// 🔴 AN ALREADY-KNOWN USER IS DELEGATED TO `RedeemFor` RATHER THAN HANDLED HERE, BECAUSE
	// THIS FUNCTION WAS A SECOND, UNGUARDED WRITER OF THE SAME MEMBERSHIP.
	//
	// `RedeemFor` refuses a principal the project already holds BEFORE the spend
	// (`ErrAlreadyAMember`); this path did not, so the two redemption entry points disagreed
	// about one rule while `RedeemFor`'s own doc cited THIS function as the reason the rule
	// matters. The reachable case is a concurrent double-callback: one account, two open
	// invitations into the same project, two tabs. Both exchanges fail with
	// `identity.UnprovisionedSubject`, so both take the provisioning arm at `oauth.go`'s
	// `errors.As`; the first mints a user and writes `member-set`, and the second — now
	// reading a model where `held` is true — spends its invitation and writes `member-set`
	// AGAIN. `Model.apply` calls `setMembership` unconditionally and never consults
	// `refuseOrphaning` (that lives only on `PlanMemberSet`), so the second write silently
	// overwrites the role the first conferred, and a project whose sole owner arrived this
	// way is left ownerless.
	//
	// ⚠ DELEGATING RATHER THAN COPYING THE GUARD IS THE POINT. A second `RoleIn` check here
	// would be the same predicate open-coded at two sites, which is the shape that lets the
	// two drift apart again — and it is how this defect existed at all.
	if held {
		principal, ok := model.PrincipalFor(control.KindUser, user.ID)
		if !ok {
			// The model just answered `UserByProviderSubject` for this user, so it holds
			// them. Refusing rather than proceeding keeps the direction safe, for the reason
			// the identical check below the write gives.
			return Redemption{}, errors.New("ui: the redeemer is in the model but resolves to no principal")
		}
		return c.RedeemFor(ctx, token, principal)
	}

	// Past the delegation, this function provisions — there is no other path to here.
	userID, err := control.NewID(control.PrefixUser)
	if err != nil {
		return Redemption{}, err
	}

	// 🔴 THE INVITATION IS SPENT HERE, BEFORE ANY AUTHORITY IS RECORDED. See the doc above.
	redeemed, err := c.Invites.Redeem(token, userID, now)
	if err != nil {
		return Redemption{}, err
	}

	// UNCONDITIONAL, because the `held` case returned above: every redemption that reaches
	// here created the user.
	events := []control.Event{
		{
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
		},
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
		Principal: principal,
		// ALWAYS true here: the `held` case delegated to `RedeemFor`, which reports
		// `Provisioned: false` itself. This path is the only one that creates a user.
		Provisioned: true,
		Project:     redeemed.ProjectID,
		Role:        redeemed.Role,
	}, nil
}

// RedeemFor spends an invitation for a principal that already exists.
//
// 🔴 IT SHARES `Redeem`'s WRITE ORDER AND ITS REASONING — the invitation is spent FIRST,
// because `invite.Store.Redeem` is the one conditional `UPDATE … RETURNING` in this flow
// and therefore the only step that can pick a winner between two concurrent clicks. The
// journal write follows. See `Redeem`'s doc for why the reverse order produces two
// memberships from one invitation on an append-only journal.
//
// ⚠ IT WRITES ONE EVENT, NOT TWO. No `user-created`: the principal is already in the
// model, which is the whole difference between this and `Redeem`.
func (c ControlInviting) RedeemFor(ctx context.Context, token string, principal control.Principal) (Redemption, error) {
	if token == "" || principal.ID == "" {
		return Redemption{}, invite.ErrNotRedeemable
	}
	// 🔴 A PROJECT PRINCIPAL MAY NOT REDEEM. `Memberships` is keyed by USER and this
	// repository's standing decision is that a project is not a member of itself, so a
	// service account redeeming would write a membership row that no authority path reads —
	// an invitation that appears to work and confers nothing.
	if principal.Kind != control.KindUser {
		return Redemption{}, invite.ErrNotRedeemable
	}
	now := c.now()

	inv, known, err := c.Invites.ByToken(token)
	if err != nil {
		return Redemption{}, err
	}
	if !known && c.Links != nil {
		return c.Links.RedeemFor(ctx, token, principal)
	}
	if !known || !inv.Redeemable(now) {
		return Redemption{}, invite.ErrNotRedeemable
	}

	// 🔴 BEFORE THE SPEND. See `ErrAlreadyAMember`: this is what keeps a redemption from
	// silently rewriting the redeemer's own role, and it must not consume the invitation on
	// the way to saying so.
	if _, held := c.Authority.Model().RoleIn(inv.ProjectID, principal.ID); held {
		return Redemption{}, ErrAlreadyAMember
	}

	redeemed, err := c.Invites.Redeem(token, principal.ID, now)
	if err != nil {
		return Redemption{}, err
	}
	if _, err := c.Authority.ApplyNow(ctx, control.Event{
		Kind:      control.EventMemberSet,
		At:        now,
		ProjectID: redeemed.ProjectID,
		UserID:    principal.ID,
		Role:      redeemed.Role,
		// The INVITER, not the redeemer — `Redeem`'s rule, for its reason: the journal's
		// question is who conferred this authority.
		Actor: redeemed.Inviter,
	}); err != nil {
		return Redemption{}, fmt.Errorf(
			"ui: the invitation was spent but the membership could not be recorded, so a new "+
				"invitation is needed: %w", err)
	}
	return Redemption{
		Principal:   principal,
		Provisioned: false,
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
// honest consequence: this scans EVERY project in the model, one query each. See the body. A `ByDigest` on the interface would be the cleaner shape and is a
// deliberate follow-up rather than an omission: adding a read that takes a digest widens
// `invite.Store`'s stated rule ("every method takes the presented token") and that rule has
// exactly one exception today, argued at `RevokeByDigest`.
func (c ControlInviting) invitesByDigest(digest string) (invite.Invite, error) {
	// ⚠ THIS SCANS EVERY PROJECT IN THE MODEL, AND THE TWO SENTENCES THAT USED TO SIT HERE
	// BOTH SAID OTHERWISE. The doc above claimed "this scans the actor's OWN invitable
	// projects, which is why [Revoke] takes the actor" — this function takes no actor, and
	// `Revoke` takes one for `mayManage`, which is the AUTHORITY check and not a scoping
	// input. This comment claimed a narrowing too ("scoped to the model's projects and
	// nothing wider"), which describes no narrowing at all: `Model.Projects` IS every
	// project.
	//
	// 🔴 IT IS NOT AN AUTHORITY HOLE, and that is why it is a comment rather than a fix.
	// The caller is authority-checked by `mayManage` against whatever project the resolved
	// ROW names, and an unknown digest and a not-yours digest collapse to the same 403 with
	// identical bytes — so scanning wider reveals nothing. What it costs is one query per
	// project in the deployment, per revoke. `invite.Store` has no by-digest read by design
	// (every method takes the presented TOKEN), so closing that is a widening of that
	// interface and a separate change, which `invitesByDigest`'s doc already argues.
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
