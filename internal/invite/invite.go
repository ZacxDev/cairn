// Package invite is the value type and the storage seam for a project invitation.
//
// # 🔴 WHY THIS IS ITS OWN PACKAGE AND NOT PART OF `internal/ui`
//
// `internal/ui` owns the `Sharing` interface it consumes, which is the convention a
// reader would expect this to follow. It does not, for one mechanical reason: the
// implementation lives in `internal/pgstore`, and if the TYPES lived in `internal/ui`
// then `pgstore` would have to import the HTML renderer — dragging `gomponents` into
// the dependency graph of a package whose whole job is SQL, and making "which packages
// link a third-party module" a harder question than it is. A leaf package with no
// dependencies but `internal/control` keeps that edge from existing.
//
// # 🔴 AN INVITE IS A BEARER CAPABILITY, AND EVERY RULE HERE FOLLOWS FROM THAT
//
// The token IS the authority. Anyone holding it can join the named project at the
// named role, and — because redemption may provision a user the control plane has
// never seen — anyone holding it can become a principal. So:
//
//   - the token is 256 bits from `crypto/rand`, the same width as a session id,
//     because the two are the same kind of secret and a narrower one here would be
//     the weakest link in the chain that creates principals;
//   - only its SHA-256 DIGEST is ever stored, so a database read, a backup or a
//     replica is not a set of usable invitations;
//   - it is returned exactly once, by [NewToken], and this package has no function
//     that can produce it again from anything persisted.
//
// # 🔴 WHAT AN INVITE IS NOT: IT IS NOT AUTHORITY UNTIL IT IS REDEEMED
//
// An outstanding invite grants nothing. The authority it eventually confers is the
// `member-set` record redemption writes to the CONTROL JOURNAL, and that record — not
// anything in this package — is what `control.Resolve` reads. This is why an invite
// table outside the journal is not a second authority store: it holds intentions, and
// the journal keeps its property of being the one place authority is recorded.
package invite

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"time"

	"github.com/ZacxDev/cairn/internal/control"
)

// TokenBytes is the entropy behind one invite token, before encoding.
//
// 🔴 THE SAME WIDTH AS `identity.SessionIDBytes`, AND THE EQUALITY IS THE POINT. The
// two secrets sit on the same chain — a token that can create a principal is at least
// as valuable as a cookie that authenticates one — so picking a smaller number here
// would move the weakest link without moving anything visible.
const TokenBytes = 32

// DefaultTTL is how long an invite lives if the minting caller does not say.
//
// ⚠ SHORT ON PURPOSE, AND SHORTER THAN A SESSION'S IS LONG. An invite link travels
// out of band — a chat message, an email, a pasted line — and every one of those is a
// place it keeps working after the sender has forgotten it. Seven days is long enough
// for a person to get round to clicking and short enough that a leaked backlog of chat
// history is not a backlog of live invitations.
const DefaultTTL = 7 * 24 * time.Hour

// Invite is one invitation as the store holds it.
//
// 🔴 IT HOLDS A DIGEST, NEVER THE TOKEN. Same rule, same reason, as
// `identity.Session`: a table of tokens is a set of bearer credentials, and this one
// would be a set of credentials that can CREATE principals.
type Invite struct {
	// Digest is the hex SHA-256 of the token. The token itself is never stored.
	Digest string
	// ProjectID is the project a redeemer joins.
	ProjectID control.ID
	// Role is the authority they join at.
	Role control.Role
	// Inviter is the principal who minted it, and becomes the ACTOR on the
	// `member-set` record redemption writes — so the journal says who let this person
	// in, which is the question a grant log exists to answer.
	Inviter control.ID

	CreatedAt time.Time
	ExpiresAt time.Time

	// RevokedAt is zero unless somebody took it back before it was used.
	RevokedAt time.Time
	// RedeemedAt is zero until it is accepted. A redeemed invite is KEPT, not deleted:
	// see the schema comment in `internal/pgstore`.
	RedeemedAt time.Time
	// RedeemedBy is the user the redemption resolved to — created then, or already
	// existing.
	RedeemedBy control.ID
}

// State is what an invite can be, as one value.
//
// 🔴 AN ENUM RATHER THAN THREE BOOLEAN READS AT EVERY CALL SITE. "Is this usable" is a
// predicate over four fields and a clock; open-coding it is the shape this repository
// has already had to consolidate more than once, and the failure mode is that one site
// forgets `RevokedAt` and honours a revoked invitation.
type State string

const (
	// StateOpen is redeemable right now.
	StateOpen State = "open"
	// StateRedeemed has already been accepted.
	StateRedeemed State = "redeemed"
	// StateRevoked was taken back by a human.
	StateRevoked State = "revoked"
	// StateExpired ran out of time.
	StateExpired State = "expired"
)

// StateAt is the ONE predicate deciding what an invite is at a given instant.
//
// 🔴 THE ORDER OF THE ARMS IS LOAD-BEARING AND IS NOT ALPHABETICAL. Redeemed is read
// FIRST because a redeemed invite stays redeemed after it expires, and a person who
// clicks their own used link a week later must be told "already used", not "expired" —
// the second sends them to ask for a new one they do not need. Revoked outranks expired
// for the same reason in the other direction: a revoked invitation was a DECISION, and
// reporting it as an expiry hides that somebody took it back.
//
// ⚠ THE EXPIRY BOUNDARY IS CLOSED AT `ExpiresAt`, matching `identity.Session.Live`
// exactly — `!now.Before(ExpiresAt)` is expired. The two differ on one instant, an
// injected clock lands on exactly that instant whenever a test pins it there, and
// having the two spellings disagree is the defect this sentence exists to prevent.
func (i Invite) StateAt(now time.Time) State {
	switch {
	case !i.RedeemedAt.IsZero():
		return StateRedeemed
	case !i.RevokedAt.IsZero():
		return StateRevoked
	case !now.Before(i.ExpiresAt):
		return StateExpired
	default:
		return StateOpen
	}
}

// Redeemable is the only question the redemption path may ask.
func (i Invite) Redeemable(now time.Time) bool { return i.StateAt(now) == StateOpen }

// ErrNotRedeemable is what the store returns when a redemption names an invite that is
// not open.
//
// ⚠ IT IS ONE ERROR FOR ALL THREE NON-OPEN STATES AT THE STORE BOUNDARY, and the page
// is what decides how much to tell the person. The store must not be the place that
// decision is taken, because a store that answered differently per state would make any
// caller that forwards its error into an oracle over invites it was not given.
var ErrNotRedeemable = errors.New("invite: this invitation is not open")

// Digest is the ONE hash of an invite token, so there is one place the function is
// chosen and no second spelling to disagree with it. Mirrors `identity.SessionDigest`.
func Digest(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// NewToken mints one invite token and returns it with its digest.
//
// 🔴 IT RETURNS BOTH, AND THE CALLER STORES ONLY THE SECOND. Returning the pair from
// one function is what makes it impossible to store the token by accident: there is no
// path where a caller holds a token and has to remember to hash it, because the hashed
// form is already in its hand.
//
// 🔴 THE ERROR IS RETURNED RATHER THAN SWALLOWED, for `identity.NewSessionID`'s reason:
// the alternative to a random token is a guessable one, so a caller that ignored a
// `crypto/rand` failure would mint a capability whose whole security property had
// silently gone.
func NewToken() (token, digest string, err error) {
	buf := make([]byte, TokenBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", "", err
	}
	// Raw URL encoding: no padding, no `+` or `/`, so the value needs no escaping in
	// the query string it travels in and cannot be corrupted by a proxy that
	// re-encodes one. Same encoding as a session id, for the same reason.
	token = base64.RawURLEncoding.EncodeToString(buf)
	return token, Digest(token), nil
}

// Store is the durable half.
//
// 🔴 EVERY METHOD TAKES THE PRESENTED TOKEN, NOT A DIGEST, which is
// `identity.SessionStore`'s rule applied to the second secret this surface holds: a
// caller that hashed would be the second place the hash function is chosen, and no
// caller ever needs to hold a digest, so no caller can log one.
type Store interface {
	// Create stores one new invitation.
	Create(inv Invite) error
	// ByToken resolves a presented token to an invitation in ANY state. The state is
	// the caller's to read with [Invite.StateAt] — this returns a used or revoked row
	// rather than hiding it, because "already used" and "never existed" are different
	// answers to a person who clicked a link.
	ByToken(presentedToken string) (Invite, bool, error)
	// Redeem marks an invitation accepted, atomically.
	//
	// 🔴 IT IS THE STORE'S JOB RATHER THAN A READ-THEN-WRITE IN THE HANDLER, BECAUSE
	// THE RACE IS REAL AND ITS OUTCOME IS A DOUBLE JOIN. Two clicks on one link
	// arriving together both read `open`, and both then write a `member-set`. A
	// conditional UPDATE is what makes exactly one of them win; the loser gets
	// [ErrNotRedeemable] and is shown the "already used" page it should have got.
	Redeem(presentedToken string, by control.ID, at time.Time) (Invite, error)
	// Revoke takes an OPEN invitation back. Revoking one that is not open is
	// [ErrNotRedeemable]: a revoke that silently succeeded against an already-redeemed
	// invitation would tell an administrator they had closed a door that is open.
	Revoke(presentedToken string, at time.Time) error
	// RevokeByDigest is the administrator's path, which is the only one that can act
	// on an invitation whose token nobody holds any more.
	//
	// ⚠ IT TAKES A DIGEST, WHICH IS THE ONE DELIBERATE EXCEPTION TO THIS INTERFACE'S
	// OWN RULE, and it is safe for the reason the rule exists: a digest is not a
	// credential. The mint page lists outstanding invitations BY DIGEST — it cannot
	// list them by token, because it does not have the tokens — so without this method
	// an invitation could be created and never withdrawn.
	RevokeByDigest(digest string, at time.Time) error
	// ForProject is every invitation naming a project, newest first. For the page that
	// lists them; it returns rows in every state and the page renders the state.
	ForProject(project control.ID) ([]Invite, error)
}
