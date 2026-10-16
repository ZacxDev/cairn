package identity

import "fmt"

// VerifiedClaimsRefusal is the refusal a bearer token gets when its SIGNATURE VERIFIED
// over a key in the configured set and something AFTER that step refused it: an expired,
// not-yet-valid, over-age or future-dated token, a wrong issuer or audience, an empty
// subject, a payload that is not a claim set, a `role` that is not the required one, or a
// subject this control plane holds no user for.
//
// # 🔴 WHY IT EXISTS: SUCH A REFUSAL IS NOT A GUESS, SO IT MUST NOT COUNT AS ONE
//
// `internal/api` counts every failed auth against the caller's client address, and five
// in a window lock that address out. That is right for a guess: a wrong machine token, a
// forged signature, a key this verifier does not hold. It is wrong for a token the
// ISSUER signed. A clock skew or a key-rotation mistake at the issuer makes every one of
// its tokens fail a claim check at once, and when many callers arrive through one egress
// address (a service minting tokens on its users' behalf, a NAT, a gateway) five such
// failures lock EVERYONE behind that address out for the lockout's whole duration —
// stretching an issuer-side outage well past the issuer's fix. So `internal/api` audits
// this refusal as its own status and does not count it.
//
// # 🔴 WHY THAT CANNOT BE ABUSED TO GUESS FOR FREE
//
// Producing a token that reaches this refusal REQUIRES A VALID SIGNATURE under a key the
// verifier trusts. Anyone who can produce one already holds the issuer's signing key, and
// with it can mint a token that PASSES — so exempting them from a guessing limit buys them
// nothing. Every refusal that does not prove possession of that key — a bad signature, an
// unknown `kid`, a refused algorithm, a malformed token, a wrong machine token — is a
// plain [Refusal] and is counted exactly as before. And [Chain] propagates this type only
// when no OTHER credential carrier on the request (a session cookie, a proxy's identity
// header) was presented, so an attacker cannot replay one expired-but-genuine token
// beside an uncounted stream of guesses at a second credential.
//
// # 🔴 IT IS CONSTRUCTIBLE IN EXACTLY ONE PLACE
//
// [newVerifiedClaimsRefusal] is unexported and called only from
// [SupabaseJWT.AuthenticateToken], after [verifyToken] reported the signature verified.
// The decision is made from that boolean, never from an error's text and never from
// `ErrTokenClaims` — a sentinel can be wrapped by an earlier step in a later edit, a flag
// set beside `verifySignature`'s success cannot.
//
// # ⚠ WHAT IT DOES NOT CHANGE
//
// [VerifiedClaimsRefusal.Error] is byte-identical to the refusal it wraps, and `Unwrap`
// returns that refusal, so `errors.Is(err, control.ErrNoCredential{})`, `errors.As(err,
// &*Refusal)` and `errors.As(err, &*UnprovisionedSubject)` all answer exactly as they did.
// The wire is unchanged: the caller still gets the one uniform 401.
type VerifiedClaimsRefusal struct {
	wrapped error
	// Subject and TokenID are the `sub` and `jti` the issuer signed into the refused
	// token — possibly empty (an empty `sub` is itself one of the refusals). They exist
	// for the operator's audit line and nothing else: no route may branch on them.
	Subject string
	TokenID string
}

func (v *VerifiedClaimsRefusal) Error() string { return v.wrapped.Error() }

// Unwrap keeps the existing chain intact. See the type's doc.
func (v *VerifiedClaimsRefusal) Unwrap() error { return v.wrapped }

func (v *VerifiedClaimsRefusal) String() string {
	return fmt.Sprintf("VerifiedClaimsRefusal{subject:%q jti:%q: %v}", v.Subject, v.TokenID, v.wrapped)
}

// newVerifiedClaimsRefusal wraps a refusal produced AFTER a successful signature check.
// See the type's doc for why it has one caller.
func newVerifiedClaimsRefusal(wrapped error, claims Claims) error {
	return &VerifiedClaimsRefusal{wrapped: wrapped, Subject: claims.Subject, TokenID: string(claims.ID)}
}
