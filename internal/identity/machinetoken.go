package identity

import (
	"errors"
	"net/http"

	"github.com/ZacxDev/cairn/internal/authz"
	"github.com/ZacxDev/cairn/internal/control"
)

// MachineTokenBackend is this backend's name in a `Refusal`. Never rendered to a
// caller.
const MachineTokenBackend = "machine-token"

// TokenAuthority is the read half `MachineToken` needs. `*control.Cache` satisfies it.
//
// 🔴 AN INTERFACE RATHER THAN A `*control.Cache`, BECAUSE A CAPTURED POINTER IS A
// SECOND HOLDER OF ONE FACT AND THIS ONE WAS MEASURED WRONG. The first draft of this
// backend stored the cache it was built with. `api.Server` also holds one — that is what
// `Server.Authority()` hands the refresh loop — so a path that replaced the server's
// cache left the two looking at DIFFERENT worlds: the loop refreshing one, every
// authentication reading the other, with nothing observable except credentials that
// authenticate against an authority nobody is refreshing. It was found by an existing
// guard going red (`TestTheWritePathNarrowsWithTheWriteVERB`, which swaps the server's
// authority), which is the only reason it is a comment here and not a defect.
// `api.Server` now passes an indirection that resolves its own field at call time, so
// there is exactly one cache and no way to hold a stale reference to it.
type TokenAuthority interface {
	Authenticate(token string) (control.Principal, control.Authorization, error)
}

// MachineToken is the agent/CI path: a bearer token this pod minted, resolved by
// hashed lookup in the materialized control-plane cache.
//
// 🔴 IT IS A MOVE, NOT A REWRITE, AND THAT IS DELIBERATE. Every line of behaviour here
// was `internal/api`'s `request.authenticate` before P4: the same
// `authz.PresentedToken` transport parse, the same single `control.Cache.Authenticate`
// call, the same `authz.TokenID` fingerprint derived from the same string that was
// authenticated. P4's whole claim about the existing deployment is that it behaves
// byte-identically, and rewriting the one path every deployed credential takes would
// have made that claim about new code.
//
// 🔴 THE HOT PATH CONTACTS NOTHING. `control.Cache` holds a materialized model and
// `Source.Model` is called only from a refresh — the property `internal/control/cache.go`
// exists for. This backend adds no I/O of its own.
type MachineToken struct {
	// Authority is the materialized control plane. Required.
	Authority TokenAuthority
}

var _ Authenticator = (*MachineToken)(nil)

// ErrNoAuthority refuses a backend with nothing to authenticate against.
//
// A nil authority would panic on the first request — a server that came up, passed its
// health check and then crashed on the first authenticated call. Refusing at
// construction is the same ruling `api.New` makes about an empty token table.
var ErrNoAuthority = errors.New("identity: no control-plane authority was configured for this backend")

// NewMachineToken builds the machine-token backend.
func NewMachineToken(authority TokenAuthority) (*MachineToken, error) {
	if authority == nil {
		return nil, ErrNoAuthority
	}
	return &MachineToken{Authority: authority}, nil
}

// Authenticate resolves the bearer credential to exactly one principal, once.
//
// ⚠ AN EMPTY PRESENTED TOKEN IS REFUSED BY `control.Authenticate` ITSELF rather than by
// a guard here, which matters: an empty string hashes to a perfectly valid digest, and a
// model that ever held it would authenticate every request with no header at all.
func (m *MachineToken) Authenticate(r *http.Request) (Identity, error) {
	// Extracted ONCE. The fingerprint below is derived from the same string that was
	// authenticated, so the audit line cannot name a credential other than the one the
	// authority matched.
	presented := authz.PresentedToken(r.Header.Get("Authorization"))
	principal, auth, err := m.Authority.Authenticate(presented)
	if err != nil {
		// Returned as-is rather than wrapped. `control.ErrNoCredential` carries nothing
		// on purpose, and the one thing this backend could add — "the token did not
		// match" — is the only outcome it has, so a reason here would be a constant
		// pretending to be information.
		return Identity{}, err
	}
	return Identity{
		Principal: principal,
		Auth:      auth,
		// 🔴 THE FINGERPRINT STAYS `authz.TokenID`, NOT THE CREDENTIAL ID, AND THAT IS A
		// CONTRACT RATHER THAN A CONVENIENCE. The documented rotation procedure is "read
		// the fingerprints the startup line prints, then grep the audit stream for the
		// one that should have stopped appearing" — both halves are the same 12-hex
		// digest of the token, so re-spelling either would silently break every saved
		// query an operator has.
		Fingerprint: authz.TokenID(presented),
	}, nil
}
