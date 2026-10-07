package ui

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ZacxDev/cairn/internal/control"
	"github.com/ZacxDev/cairn/internal/identity"
)

// TestAProviderSignInMintsAnUnnarrowedSession is the POSITIVE half of presence's prerequisite
// control (plan S2, decision 11). The negative half — a narrowed credential pasted into
// `POST /sign-in` is refused and the session store holds no new row — is #195's
// `TestANarrowedCredentialCannotSignIn`, which S2 relies on rather than duplicates.
//
// This half says the refusal is specific to NARROWING and not to sign-in: the provider door,
// over the SAME world that holds narrowed credentials for the same user, mints a session whose
// identity — resolved through the real cookie backend — is un-narrowed and reads both of the
// user's scopes. `presence.Store.For` reads `Auth.Narrowed()` off exactly that identity.
func TestAProviderSignInMintsAnUnnarrowedSession(t *testing.T) {
	authority := narrowedWorld(t)
	principal, ok := authority.Model().PrincipalFor(control.KindUser, fixtureUser)
	if !ok {
		t.Fatal("precondition: the fixture user is not in the world")
	}
	sessions := mustSessions(t)
	cfg := testConfig(t, refusingAuth{})
	cfg.Sessions = sessions
	stub := &stubOAuth{principal: principal}
	cfg.OAuth = stub
	srv, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}

	flight := startFlight(t, srv)
	callback := httptest.NewRequest(http.MethodGet, OAuthCallbackPath+"?code=fixture-provider-code", nil)
	callback.AddCookie(flight)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, callback)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("the provider callback answered %d, want 303: %s", rec.Code, rec.Body.String())
	}
	session := sessionCookieFrom(rec.Result())
	if session == nil {
		t.Fatal("the provider callback minted no session cookie")
	}

	cookie, err := identity.NewCookieSession(sessions, authority)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodGet, RootPath, nil)
	r.AddCookie(session)
	id, err := cookie.Authenticate(r)
	if err != nil {
		t.Fatalf("the provider session does not authenticate: %v", err)
	}
	if id.Principal.ID != fixtureUser {
		t.Fatalf("the provider session is %s, want the fixture user", id.Principal)
	}
	if id.Auth.Narrowed() {
		t.Fatal("A PROVIDER SIGN-IN MINTED A NARROWED SESSION: presence would be withheld from its own owner")
	}
	if !id.Auth.Allows(fixtureScope, control.VerbRead) || !id.Auth.Allows(fixtureScopeTwo, control.VerbRead) {
		t.Fatal("the provider session does not read both of the user's scopes")
	}
}
