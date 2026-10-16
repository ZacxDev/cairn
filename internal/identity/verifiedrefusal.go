package identity

import "fmt"

// VerifiedClaimsRefusal is the refusal a bearer token gets when its SIGNATURE VERIFIED
// over a key in the configured set and one of the CLAIM-TIME checks inside `verifyToken`
// then refused it: `exp` (past or absent), `nbf`, `iat` (in the future, or older than the
// configured max age), `iss`, `aud`, an empty `sub`, or a payload that is not a JSON claim
// set. Nothing else.
//
// # 🔴 WHAT IT COVERS — AND WHAT IT DOES NOT
//
// `internal/api` counts every failed auth against the caller's client address, and five
// in a window lock that address out. That is right for a guess. It is wrong for a fault
// that makes EVERY genuine token fail at once: a clock skew between this pod and the
// issuer, or an issuer/audience misconfiguration on either side (an `iss` or `aud` that
// does not match). When many callers arrive through one egress address, five such
// failures lock everyone behind that address out for the whole lockout window, which
// stretches the fault past its fix. Those are the cases this type exists for, and the
// only ones.
//
// ⚠ IT DOES NOT COVER A KEY ROTATION. A token signed by a key the verifier does not hold
// (an unknown `kid`, or a rotated key under a reused one) fails the SIGNATURE, never
// reaches the claim checks, and is counted like any other bad signature.
//
// ⚠ AND IT DELIBERATELY DOES NOT COVER A ROLE MISMATCH, AN UNKNOWN SUBJECT, OR A
// MALFORMED GROUPS CLAIM, all of which are decided AFTER `verifyToken` and are COUNTED.
// The argument that makes the claim-time checks safe — producing a refusal of this kind
// needs a genuine token — buys nothing against an issuer that signs for SELF-REGISTERED
// users: anybody can obtain a genuine token naming a subject, role or group set this
// control plane does not accept, so those refusals are the guessing surface the limiter
// exists for.
//
// # 🔴 WHY THE CLAIM-TIME ARM CANNOT BE USED TO GUESS FOR FREE
//
// Reaching it requires a valid signature under a key the verifier trusts, over a token
// that would otherwise authenticate — it searches for no secret, because the only thing
// it varies is time or the issuer's own `iss`/`aud`. Every refusal that does not prove
// possession of a genuine token — a bad signature, an unknown `kid`, a refused algorithm,
// a malformed token, a wrong machine token — is a plain [Refusal] and is counted exactly
// as before. And [Chain] propagates this type only when no OTHER credential carrier on
// the request (a proxy's identity or secret header) was presented, so an attacker cannot
// replay one expired-but-genuine token beside an uncounted stream of guesses at a second
// credential.
//
// # 🔴 IT IS CONSTRUCTIBLE IN EXACTLY ONE PLACE
//
// [newVerifiedClaimsRefusal] is unexported and called only from
// [SupabaseJWT.AuthenticateToken], when [verifyToken] reported the signature verified AND
// returned an error. The decision is made from that boolean, never from an error's text
// and never from `ErrTokenClaims` — a sentinel can be wrapped by an earlier step in a
// later edit, a flag set beside `verifySignature`'s success cannot.
//
// # ⚠ WHAT IT DOES NOT CHANGE
//
// [VerifiedClaimsRefusal.Error] is byte-identical to the refusal it wraps, and `Unwrap`
// returns that refusal, so `errors.Is(err, control.ErrNoCredential{})` and `errors.As(err,
// &*Refusal)` answer exactly as they did. The wire is unchanged: the caller still gets the
// one uniform 401.
type VerifiedClaimsRefusal struct {
	wrapped error
	// Subject and TokenID are the `sub` and `jti` the issuer signed into the refused
	// token — possibly empty (an empty `sub` is itself one of the refusals, and a
	// payload that is not a claim set carries neither). They exist
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
