package invite

import (
	"errors"
	"time"

	"github.com/ZacxDev/cairn/internal/control"
)

// # 🔴 A TEAM LINK IS A SIBLING OF [Invite], NOT A WIDER [Invite]
//
// An [Invite] names ONE project at ONE role and is spent by ONE redemption. A team link
// names a SET of targets — projects and scopes — at one link role, and may be redeemable
// MANY times. Widening [Invite] to carry both would change what every reader of the old
// type, the old table and the old tests means by "an invitation", and the one-project,
// single-use shape is the one those guards were written and mutation-swept against. So the
// new shape is new rows in new tables (`team_links`, `team_link_targets`,
// `team_link_redemptions` — `internal/pgstore`'s migration 2), and the two share exactly
// the parts whose sharing is the point: [NewToken], [Digest], [TokenBytes] and the rule
// that only the digest is ever stored.
//
// # 🔴 A REUSABLE LINK IS OPEN ENROLMENT UNTIL IT EXPIRES OR IS REVOKED
//
// That is the operator's decision (reuse is UNLIMITED, not a capped count) and the
// residual it buys is stated here rather than discovered: anybody who holds a reusable
// link — including whoever it leaked to — can create a principal and join every target on
// it, as many times as they like, until [TeamLink.ExpiresAt] or a revoke. The bounds are
// the TTL ceiling ([MaxLinkTTL]), the revoke, the per-redemption log
// ([LinkRedemption]) that makes every one of those joins visible, and the re-check at
// redemption that kills every link the moment its minter loses authority.

// LinkRole is the authority a team link confers on each of its targets.
//
// 🔴 IT IS NOT A [control.Role], AND THE DIFFERENCE IS THE `reader` VALUE. `control.Role`
// has no read-only member — owner, admin and member all write — and adding one would put a
// NEW ROLE STRING into `member-set` records on the append-only journal, which an image
// ROLLBACK cannot replay (`Event.validate` refuses an unknown role for exactly that reason).
// So `reader` is expressed through record kinds every deployed build already accepts: a
// `granted` record of the `read` verb. `member` and `admin` on a PROJECT are the existing
// `member-set` roles; on a SCOPE every link role is a `granted` record of the matching verbs.
// The mapping lives in ONE place on the consuming side (`internal/ui`'s `linkVerbs`), next
// to the authority predicate that reads it.
type LinkRole string

const (
	// LinkReader may read, and nothing else. The DEFAULT, because a form whose unread
	// default confers write is a form that hands out write to everybody who does not read
	// it — `inviteForm`'s ruling about `owner`, one step down.
	LinkReader LinkRole = "reader"
	// LinkMember may read and write.
	LinkMember LinkRole = "member"
	// LinkAdmin may read, write and administer — on a project, manage its membership.
	LinkAdmin LinkRole = "admin"
)

// AllLinkRoles is the closed set, LEAST privileged first — the order a chooser renders and
// the reason its first option is the safe default.
//
// ⚠ THERE IS NO `owner`. A link may be redeemable by anybody, any number of times; a
// reusable owner link would be a transfer of a project to whoever reads a chat log. The
// single-project invite flow keeps its owner option, bounded by single use.
var AllLinkRoles = []LinkRole{LinkReader, LinkMember, LinkAdmin}

// Valid answers whether r is a link role this package defines.
func (r LinkRole) Valid() bool {
	for _, known := range AllLinkRoles {
		if r == known {
			return true
		}
	}
	return false
}

// TargetKind discriminates what a link target names.
type TargetKind string

const (
	// TargetProject names a project: redemption confers MEMBERSHIP (or, for `reader`, a
	// read grant over the whole project).
	TargetProject TargetKind = "project"
	// TargetScope names one scope: redemption confers a grant over it.
	TargetScope TargetKind = "scope"
)

// Valid answers whether k is a target kind this package defines.
func (k TargetKind) Valid() bool { return k == TargetProject || k == TargetScope }

// Target is one thing a link confers authority over.
type Target struct {
	Kind TargetKind
	ID   control.ID
}

// MaxLinkTTL is the longest a team link may live.
//
// ⚠ IT IS A CEILING THE MINTER CANNOT RAISE, AND IT IS WHAT BOUNDS A LEAKED REUSABLE LINK.
// [DefaultTTL] is still the default; this exists because the TTL became operator-chosen per
// link, and a chosen value with no ceiling is a link that is open enrolment for ever.
// Thirty days is long enough for a team onboarding over a month and short enough that a
// forgotten link in a chat log dies on its own.
const MaxLinkTTL = 30 * 24 * time.Hour

// MaxLinkTargets bounds how many targets one link may name.
//
// ⚠ A BOUND ON THE WORK ONE REDEMPTION DOES, NOT A POLICY. Every target is one authority
// re-check and one journal event per redemption, and an unbounded set posted by a hand-made
// form would make one click an unbounded write.
const MaxLinkTargets = 64

// TeamLink is one multi-target link as the store holds it.
//
// 🔴 IT HOLDS A DIGEST, NEVER THE TOKEN — [Invite]'s rule, for [Invite]'s reason.
type TeamLink struct {
	// Digest is the hex SHA-256 of the token.
	Digest string
	// Targets is every project and scope a redemption joins. Never empty.
	Targets []Target
	// Role is what each target is joined at.
	Role LinkRole
	// Inviter is the principal who minted it. It is the ACTOR on every record a
	// redemption writes, it is whose authority a redemption RE-CHECKS, and it is the only
	// principal who may list or revoke the link.
	Inviter control.ID
	// Reusable is the operator's "allow reuse" tick: true is UNLIMITED redemptions until
	// expiry or revoke; false is single-use, as an [Invite] is.
	Reusable bool

	CreatedAt time.Time
	ExpiresAt time.Time
	// RevokedAt is zero unless somebody took it back.
	RevokedAt time.Time
	// Redemptions is how many times it has been redeemed. The per-redemption record is
	// [LinkRedemption]; this is the count the atomic single-use check reads.
	Redemptions int
}

// LinkRedemption is one redemption of a team link — the per-redemption audit record.
type LinkRedemption struct {
	Digest string
	// Seq is 1 for the first redemption, and so on. It is the count AFTER this one.
	Seq int
	// By is the user the redemption resolved to.
	By control.ID
	At time.Time
	// Provisioned is true when this redemption CREATED the user — the event an operator
	// most needs to be able to find, because a reusable link makes it repeatable.
	Provisioned bool
	// Confirmed is true once the AUTHORITY write this redemption exists for succeeded.
	//
	// 🔴 THE ROW IS WRITTEN WHEN THE LINK IS SPENT AND CONFIRMED ONLY AFTER THE JOURNAL
	// ACCEPTED THE JOIN, AND AN UNCONFIRMED ROW IS NOT A JOIN. Round 1 measured the shape this
	// field closes: two tabs on one GitHub identity, one reusable link — both spend, the second
	// journal write is refused (a duplicate provider/subject), and the log named a principal
	// that was never created as "joined (account created by this link)". The row is still
	// WRITTEN at the spend, rather than only after, because the other order loses the audit of a
	// REAL join whenever the process dies between the journal write and the log write — and
	// for a reusable link the log is the only place an operator can see who it let in. So every
	// spend is visible, and only a confirmed one is rendered as a join.
	Confirmed bool
}

// StateAt is the ONE predicate deciding what a team link is at a given instant.
//
// 🔴 THE ARM ORDER IS [Invite.StateAt]'s, FOR ITS REASONS. A SPENT single-use link reads
// `redeemed` first, because somebody clicking their own used link must be told it was used,
// not that it expired. Revoked outranks expired because a revoke was a decision.
//
// ⚠ A REUSABLE LINK IS NEVER `redeemed`: its count does not close it. That is the whole of
// the reuse decision, and this arm's `!l.Reusable` is the expression that carries it.
//
// ⚠ THE EXPIRY BOUNDARY IS CLOSED AT `ExpiresAt`, matching [Invite.StateAt] and
// `identity.Session.Live`, and the SQL guard in `internal/pgstore` is pinned against it.
func (l TeamLink) StateAt(now time.Time) State {
	switch {
	case !l.Reusable && l.Redemptions > 0:
		return StateRedeemed
	case !l.RevokedAt.IsZero():
		return StateRevoked
	case !now.Before(l.ExpiresAt):
		return StateExpired
	default:
		return StateOpen
	}
}

// Redeemable is the only question a redemption path may ask of a link's own state. It is
// NOT the authority question — that is the minter's, re-checked at redemption by the
// consumer.
func (l TeamLink) Redeemable(now time.Time) bool { return l.StateAt(now) == StateOpen }

// ErrBadTarget refuses a link whose target set is empty, too large, duplicated or malformed.
var ErrBadTarget = errors.New("invite: a team link needs between one and MaxLinkTargets distinct, well-formed targets")

// ValidateTargets is the one shape check over a target set, used by every writer.
func ValidateTargets(targets []Target) error {
	if len(targets) == 0 || len(targets) > MaxLinkTargets {
		return ErrBadTarget
	}
	seen := map[Target]bool{}
	for _, t := range targets {
		if !t.Kind.Valid() || t.ID == "" || seen[t] {
			return ErrBadTarget
		}
		seen[t] = true
	}
	return nil
}

// LinkStore is the durable half of team links.
//
// 🔴 [Store]'s TOKEN RULE HOLDS FOR EVERY METHOD A *REDEEMER* REACHES: [LinkStore.LinkByToken]
// and [LinkStore.RedeemLink] take the presented token and hash it here. The methods that take
// a DIGEST are the MINTER's — the Team page lists and revokes links by digest because it never
// holds a token — which is [Store.RevokeByDigest]'s exception, argued there, applied to the
// listing that exception already implied.
type LinkStore interface {
	// CreateLink stores one new link and its targets, atomically.
	CreateLink(l TeamLink) error
	// LinkByToken resolves a presented token to a link in ANY state.
	LinkByToken(presentedToken string) (TeamLink, bool, error)
	// LinkByDigest resolves a digest to a link in any state. For the minter's revoke.
	LinkByDigest(digest string) (TeamLink, bool, error)
	// RedeemLink records one redemption, ATOMICALLY: it increments the count and appends
	// an UNCONFIRMED [LinkRedemption] in one transaction, and only if the link is open — for
	// a single-use link, only if its count is still zero. Exactly one of two concurrent
	// redemptions of a single-use link wins; the loser gets [ErrNotRedeemable]. The returned
	// link's `Redemptions` is this redemption's `Seq`.
	RedeemLink(presentedToken string, by control.ID, provisioned bool, at time.Time) (TeamLink, error)
	// ConfirmRedemption marks one redemption's row CONFIRMED — called only after the
	// authority write it stands for succeeded. See [LinkRedemption.Confirmed].
	ConfirmRedemption(digest string, seq int) error
	// RevokeLink takes an OPEN link back. A link that is not open is [ErrNotRedeemable].
	RevokeLink(digest string, at time.Time) error
	// LinksBy is every link this principal minted, newest first, in every state.
	LinksBy(inviter control.ID) ([]TeamLink, error)
	// LinkRedemptions is a link's redemption log, oldest first.
	LinkRedemptions(digest string) ([]LinkRedemption, error)
}
