package control

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// ErrNoSuchCredential refuses a revocation naming a credential this journal does not hold.
//
// ⚠ AN EARLY REFUSAL WITH A BETTER FIRST LINE, NOT A SECOND AUTHORITY — the same ruling as
// [ErrNoSuchPrincipal]. `Model.apply` refuses a `credential-revoked` for an unknown id under
// `Append`'s lock regardless; what the sentinel buys is a branch `cairn-server
// -revoke-credential` takes to print a line naming the id, instead of relaying a sentence
// about a batch that would not replay.
var ErrNoSuchCredential = errors.New("control: no such credential in this control journal")

// ErrAlreadyRevoked refuses a revocation of a credential that is already revoked.
//
// 🔴 A REFUSAL, NOT A SILENT SUCCESS, EVEN THOUGH `apply` WOULD ACCEPT THE LINE. `apply`'s
// `credential-revoked` arm is idempotent (a second revocation is a no-op that still advances
// the epoch), so appending one would succeed — and an operator who typed the id of the
// credential that is ALREADY dead, believing it to be the live one they meant to cut off, would
// be told it worked while the host they meant to cut off keeps its access. Refusing makes that
// mistake visible at the one moment it can be corrected.
var ErrAlreadyRevoked = errors.New("control: this credential is already revoked")

// Revoked is what [RevokeCredential] did: the credential it retired and the journal epoch after.
type Revoked struct {
	Credential Credential
	Epoch      uint64
}

// RevokeCredential appends ONE `credential-revoked` event for a live credential.
//
// 🔴 IT CLOSES THE GAP `IssueCredential` USED TO DOCUMENT: `EventCredentialRevoked` was in the
// closed event set, validated and applied, and nothing in this repository WROTE one, so a
// rotation was "issue the new one, then hand-append the revocation" (scope-mail plan, decision
// 16). It goes through the same [Store.Append] every other writer uses, so the batch is
// validated against a clone under the journal's lock before a byte reaches disk.
//
// ⚠ IT REVOKES AND NOTHING ELSE. Every surface reading this journal refuses the token on its
// NEXT authority refresh — not now: `cairn-ui` re-reads every refresh interval, and while a
// refresh keeps failing the last-known-good model (which still holds this credential live)
// keeps serving. The caller says so; this function cannot see any running process.
func RevokeCredential(ctx context.Context, s Store, id ID, actor ID, at time.Time) (Revoked, error) {
	if at.IsZero() {
		at = time.Now().UTC()
	}
	current, err := s.Model(ctx)
	if err != nil {
		return Revoked{}, fmt.Errorf("reading the control journal: %w", err)
	}
	c, known := current.Credentials[id]
	if !known {
		return Revoked{}, fmt.Errorf("%w: %s", ErrNoSuchCredential, id)
	}
	if !c.Live() {
		return Revoked{}, fmt.Errorf("%w: %s (revoked at %s)", ErrAlreadyRevoked, id, c.RevokedAt.UTC().Format(time.RFC3339))
	}
	after, err := s.Append(ctx, Event{Kind: EventCredentialRevoked, At: at, Actor: actor, CredentialID: id})
	if err != nil {
		return Revoked{}, err
	}
	return Revoked{Credential: after.Credentials[id], Epoch: after.Epoch}, nil
}
