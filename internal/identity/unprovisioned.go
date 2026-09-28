package identity

import "fmt"

// UnprovisionedSubject is the refusal a VERIFIED token gets when this control plane holds
// no user for the subject it names.
//
// # 🔴 WHY THIS TYPE EXISTS AT ALL, GIVEN THAT THE REFUSAL ALREADY WORKED
//
// Until the invite flow there was exactly one correct response to "the token verifies and
// names somebody we have never heard of": refuse, uniformly, and put the reason in the
// operator's log. `-create-user`'s help still states the rule — a subject is *"refused,
// never provisioned, so somebody with write access to the journal has to do this
// deliberately"*.
//
// An invite REDEMPTION is a second way for that deliberate act to happen: a 256-bit
// capability token, minted by somebody who already holds authority over the project, IS a
// deliberate decision to let this person in. So a caller holding a valid invite needs to
// distinguish this one refusal from every other, and no other caller may be able to.
//
// # 🔴 WHAT IT DELIBERATELY DOES *NOT* CHANGE — READ THIS BEFORE EXTENDING IT
//
//   - [UnprovisionedSubject.Error] is BYTE-IDENTICAL to the refusal it replaces. Every
//     existing log line, including the OAuth callback's, prints exactly what it printed
//     before, so this type adds no disclosure anywhere. The subject is reachable only
//     through `errors.As`, by a caller that asked for it by type.
//   - `Unwrap` returns the underlying [*Refusal], which itself unwraps to
//     `control.ErrNoCredential{}`. So `errors.Is(err, control.ErrNoCredential{})` — the
//     uniform question every serving path asks — is still true, and every
//     `errors.As(err, &*Refusal)` still matches. A serving path that does not know about
//     this type cannot behave differently because of it.
//   - it carries NO EMAIL. `Claims` has no `Email` field on purpose, and its comment gives
//     the reason: a second copy of a user's address, caller-supplied, competing with the
//     `control.User` row. `EventUserCreated` does not require one (a user with no email is
//     a shape this repository's own fixtures already carry), so provisioning from an invite
//     needs provider and subject and nothing else. Adding an email here would reverse a
//     documented decision to buy a field the write does not need.
//
// # ⚠ ONE BACKEND RETURNS IT, NOT TWO
//
// `TrustedHeader.Authenticate` has the same `!known` arm and is deliberately left alone.
// Its refusal is about a deployment shape — a proxy vouching for a subject — where the
// invite flow does not run, and widening this to a backend with no caller for it would be
// the exported-API-with-no-consumer shape this repository refuses. If a proxy-fronted
// deployment ever needs redemption, widening it is a deliberate edit with a test, not an
// assumption already made here.
type UnprovisionedSubject struct {
	// refusal is the error this type stands in for. Held rather than reconstructed so the
	// two cannot drift: the sentence a log prints is THIS value's sentence.
	refusal *Refusal
	// Provider and Subject are the verified identity of the caller the control plane does
	// not know. Together they are exactly what `control.Model.UserByProviderSubject` keys
	// on, and exactly what an `EventUserCreated` needs.
	Provider string
	Subject  string
}

// Error is the refusal's own sentence, unchanged.
//
// 🔴 IT MUST NOT GAIN THE SUBJECT. This string reaches an operator's log, and the refusal
// it replaces deliberately named no subject — a refusal that echoed the value would put a
// caller-supplied identifier into the log on every unknown-subject probe, which is a
// disclosure and a log-injection surface at once. `TestTheUnprovisionedRefusalPrintsExactly
// WhatItAlwaysDid` pins this against the plain refusal.
func (u *UnprovisionedSubject) Error() string { return u.refusal.Error() }

// Unwrap keeps the whole existing error chain intact. See the type's doc.
func (u *UnprovisionedSubject) Unwrap() error { return u.refusal }

// refuseUnprovisioned builds the refusal for a verified token naming a subject with no user.
//
// 🔴 IT TAKES THE SAME `backend, reason` PAIR `refuse` DOES, AND BUILDS THE SAME `*Refusal`
// FROM THEM. One construction, so the sentence cannot differ between the plain arm and this
// one; `refuse` is still what every other arm calls.
func refuseUnprovisioned(backend, reason, provider, subject string) error {
	if provider == "" || subject == "" {
		// A refusal carrying half an identity is worse than one carrying none: a caller
		// doing `errors.As` would read it as "provisionable" and then write an event with
		// an empty key. `Event.validate` would refuse that write, but the honest place to
		// stop is here, where the reason is known.
		return refuse(backend, reason)
	}
	return &UnprovisionedSubject{
		refusal:  &Refusal{Backend: backend, Reason: reason},
		Provider: provider,
		Subject:  subject,
	}
}

// String is for a test's failure message and for nothing on the wire.
//
// ⚠ IT NAMES THE SUBJECT WHERE `Error` DOES NOT, which is the point of having both: a test
// asserting which subject was carried needs to print it, and a log must not. Nothing in the
// serving path calls this.
func (u *UnprovisionedSubject) String() string {
	return fmt.Sprintf("UnprovisionedSubject{provider:%q subject:%q}", u.Provider, u.Subject)
}
