package ui

import (
	"github.com/ZacxDev/cairn/internal/identity"
)

// AuthBackends assembles the UI's authentication chain.
//
// 🔴 IT TAKES THREE BACKENDS WHERE `identity.Backends` TAKES FOUR, AND THE MISSING
// ONE IS THE POINT RATHER THAN AN OMISSION. `identity.TrustedHeader` cannot be
// reached through this constructor: there is no parameter to pass one in.
//
// 🔴 THE ORDER IS `identity.Backends`'s AND IS NOT RE-DECIDED HERE, WHICH IS WHY THE
// PARAMETERS ARE POSITIONAL AND IN ITS ORDER. Machine token, then the Supabase JWT, then
// the cookie — so the cookie lands third of four, after BOTH header-borne credentials, and
// the reason is written where the ordering is: an `Authorization` header is a credential
// the caller chose to send, a cookie is one the browser attaches by itself, and the chosen
// one must win. A request carrying a live bearer token and a stale cookie therefore
// resolves as the token's principal. Re-deciding that here would be a second assembly of
// the chain, and the first thing a second assembly loses is exactly that rule.
//
// ✅ THE SUPABASE PARAMETER IS BACK, AND THE SENTENCE THAT STOOD HERE IS KEPT RATHER THAN
// DELETED BECAUSE IT RECORDED THE RULING. It read: "A `*identity.SupabaseJWT` parameter was
// here and every call site passed `nil` — an exported parameter with no caller, which is the
// shape this repository refuses elsewhere … it comes back in the phase that builds the
// sign-in flow, together with a caller that passes something." That phase is this one:
// `cmd/cairn-ui` builds the backend from `CAIRN_SUPABASE_*` and passes it, or passes nil
// when no such variable is set. ⚠ NOTE THAT NIL IS STILL A REAL CASE HERE — a deployment
// with no provider configured — so the parameter is not "always supplied"; what changed is
// that it is now supplied by SOMETHING, which is what the removal was waiting for.
//
// ⚠ AND A NIL SUPABASE BACKEND IS NOT THE SAME KIND OF ABSENCE AS THE TRUSTED HEADER'S.
// This one is a deployment that has not configured a provider; that one is a backend this
// binary refuses at any setting. Conflating them is the misreading to avoid.
//
// `internal/identity/README.md` states the hazard plainly — on a pod that is
// reachable directly, anyone who can open a socket to it can set the identity
// header and BE any user in this control plane, at that user's full authority, on
// every route, with the writes attributed to them. The pod's own defence is an
// operator's explicit proxy-fronted declaration plus a source check, and that
// defence is a property of the DEPLOYMENT rather than of the request.
//
// 🔴 A BROWSER SURFACE CANNOT CARRY THAT TRADE, BECAUSE ITS WHOLE PURPOSE IS TO BE
// PUBLICLY REACHABLE. The pod is an API a deployment may choose to hide behind a
// proxy; this binary is the thing people are meant to open in a browser. An
// operator declaration that the UI is proxy-fronted would be a sentence about an
// endpoint the internet is invited to visit — so the backend is unreachable here
// by construction rather than refused by configuration, and that is a stronger
// claim: a configuration refusal can be reconfigured.
//
// ⚠ THE MECHANISM IS THE SIGNATURE, AND THE SIGNATURE ALONE IS NOT THE GUARD.
// Nothing stops a future caller from building an `identity.Chain` literal by hand
// and appending a `*identity.TrustedHeader` to it — a signature constrains this
// constructor, not the type.
// `TestTheUIChainHasNoTrustedHeaderMember` inspects the chain this function
// RETURNS, with a live `*identity.TrustedHeader` available to it and a positive
// control proving that same value lands in `identity.Backends`'s chain. That pins
// the MEMBERSHIP, which is the property, rather than the parameter list, which is
// the current spelling of it.
func AuthBackends(machine *identity.MachineToken, supabase *identity.SupabaseJWT,
	cookie *identity.CookieSession) (identity.Chain, error) {
	// The ONE absent backend is spelled `nil` HERE, at the one site that may call
	// `identity.Backends` for this binary, so `identity.Backends` stays the single
	// place the ORDER of the chain is decided. Re-implementing the ordering here
	// would be a second assembly of the chain, and the first thing a second assembly
	// loses is the rule that the machine token — the only credential this pod minted
	// and the only one whose revocation is one edit away — wins where two credentials
	// are present.
	return identity.Backends(machine, supabase, cookie, nil)
}
