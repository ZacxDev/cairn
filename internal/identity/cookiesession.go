package identity

import (
	"net/http"

	"github.com/ZacxDev/cairn/internal/control"
)

// CookieSessionBackend is this backend's name in a `Refusal`. Never rendered to a caller.
const CookieSessionBackend = "cookie-session"

// CookieSession authenticates a browser from a session cookie this surface minted.
//
// 🔴 IT IS THE ONLY BACKEND WHOSE CREDENTIAL THE BROWSER SENDS WITHOUT BEING ASKED, AND
// THAT IS WHY IT SITS WHERE IT DOES IN THE CHAIN. See `Backends`. Every other backend
// reads a header a caller had to decide to set; this one reads a cookie the user agent
// attaches to every request to this origin, including requests a different site caused.
// The consequences are the CSRF guard in `internal/ui` and the chain position here, and
// neither is optional.
//
// 🔴 THE HOT PATH READS THE SESSION FILE AND CONTACTS NOTHING ELSE. `Authority.Model()`
// is a materialized read with no I/O — the property `internal/control/cache.go` exists
// for — so the only cost this backend adds per request is the session table read, which
// `FileSessionStore.Lookup` argues for.
type CookieSession struct {
	sessions  SessionStore
	authority ModelSource
}

var _ Authenticator = (*CookieSession)(nil)

// NewCookieSession builds the backend, refusing either half of it missing.
//
// Two sentinels rather than one, because they are two different misconfigurations and an
// operator reading the startup line needs to know which: a surface with no session store
// can never resolve a cookie, a surface with no authority can never say what the
// principal behind one may see.
func NewCookieSession(sessions SessionStore, authority ModelSource) (*CookieSession, error) {
	if sessions == nil {
		return nil, ErrNoSessionStore
	}
	if authority == nil {
		return nil, ErrNoAuthority
	}
	return &CookieSession{sessions: sessions, authority: authority}, nil
}

// Authenticate resolves the session cookie to exactly one principal, once.
//
// 🔴 THE AUTHORIZATION IS RECOMPUTED FROM THE CURRENT MODEL, NEVER READ OUT OF THE
// SESSION. A `control.Authorization` stored at sign-in would be a second, frozen answer
// to "what may this caller see" — so a grant revoked an hour ago would still be served
// for as long as the session lived, and the revocation would be invisible to everything
// except a sign-out the attacker has no reason to perform. `control.Resolve` against the
// model read on THIS request is what makes revocation take effect on the next page load.
//
// 🔴 AND THE MODEL IS READ ONCE. Every fact below — the principal and its authorization —
// comes out of that one value, so a refresh landing mid-request cannot authenticate
// against one world and authorise against another. The same rule `SupabaseJWT` follows,
// for the same reason `control.Principal`'s own comment states.
func (c *CookieSession) Authenticate(r *http.Request) (Identity, error) {
	cookie, err := r.Cookie(SessionCookieName)
	if err != nil || cookie.Value == "" {
		// `http.ErrNoCookie` is the overwhelmingly common case — every unauthenticated
		// request and every request from a non-browser client takes this branch — so it
		// must be cheap and it must not be distinguishable from a wrong cookie by
		// anything that reaches the wire. `internal/ui`'s uniform 401 is what ensures
		// the second half.
		return Identity{}, refuse(CookieSessionBackend, "no session cookie")
	}

	rec, live, err := c.sessions.Lookup(cookie.Value)
	if err != nil {
		// A store that cannot be read refuses every session. That is the fail-closed
		// direction, and it is the opposite of `control.FileStore`'s last-known-good
		// degradation on purpose: there, an empty authority is a total outage that looks
		// like a permissions problem; here, a session table this process cannot read is
		// a table it cannot honour a REVOCATION from either, and serving from a
		// remembered copy would be serving credentials somebody may have just withdrawn.
		return Identity{}, refuse(CookieSessionBackend, "the session store could not be read")
	}
	if !live {
		// 🔴 ONE REFUSAL FOR "NO SUCH SESSION" AND FOR "EXPIRED", BECAUSE THE DIFFERENCE
		// IS AN ENUMERATION API. `SessionStore.Lookup` collapses them deliberately; a
		// caller who is told their cookie is merely expired has been told their cookie
		// was once real, which is a fact about the store.
		return Identity{}, refuse(CookieSessionBackend, "no live session for this cookie")
	}

	model := c.authority.Model()
	principal, held := model.PrincipalFor(rec.Kind, rec.Principal)
	if !held {
		// The principal this session names is gone from the control plane — a user
		// deleted, a project removed. `PrincipalFor`'s own comment states the rule every
		// caller must follow: a principal with no display is a credential whose use
		// cannot be attributed, so this is a refusal rather than a degraded identity.
		return Identity{}, refuse(CookieSessionBackend, "the session names a principal this control plane no longer holds")
	}

	return Identity{
		Principal: principal,
		Auth:      control.Resolve(model, principal),
		// No fingerprint. `Identity.Fingerprint` is the 12-hex id of a bearer token this
		// pod minted and an operator can look up in the startup line's table; a session
		// has no row there to stop appearing from. `identity=` is what names this
		// request in an audit line. ⚠ And putting the session digest here would be
		// exactly the "synthesize a digest that correlates with nothing" mistake that
		// field's comment already refuses — with the added defect that it would write a
		// session identifier into a log.
	}, nil
}
