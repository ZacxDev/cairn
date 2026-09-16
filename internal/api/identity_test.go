package api

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/ZacxDev/cairn/internal/control"
	"github.com/ZacxDev/cairn/internal/identity"
)

// P4's SEAM: the server resolves a request through an `identity.Authenticator`, and
// nothing downstream branches on which backend produced the principal.
//
// 🔴 THESE GUARDS LIVE IN `internal/api` RATHER THAN IN `internal/identity` BECAUSE THE
// DEFECT THEY ARE ABOUT LIVES IN THE SEAM. `internal/identity`'s own tests are
// hermetic — they build a backend and call it. This file builds a REAL server, installs
// a backend, and drives HTTP, because "verified in isolation" is the shape this
// repository's rules name explicitly: two components each tested alone can be broken
// together, and the audit line below is exactly that break.

// sessionBackend is an authenticator that resolves a request WITHOUT a bearer token,
// which is what every non-machine backend does.
//
// It is a double rather than a real `identity.SupabaseJWT` on purpose: the property
// under test is what the SERVER does with an identity carrying no fingerprint, and
// reaching that through a JWT would make the guard depend on the verifier as well.
type sessionBackend struct {
	who identity.Identity
	err error
}

func (s sessionBackend) Authenticate(*http.Request) (identity.Identity, error) {
	return s.who, s.err
}

// TestASessionWithNoTokenFingerprintIsAuditedAsAUTHENTICATED.
//
// 🔴 RED AT BASELINE, AND THE ONE-WORD CHANGE THAT MAKES IT RED IS THE DEFECT ITSELF.
// Before P4 the audit line's `auth=` field was derived from `rq.tokenFP != ""`. That was
// correct while every credential was a bearer token — `authz.TokenID` of a non-empty
// token is a non-empty digest, and an empty token is refused before the fingerprint is
// taken — and it stops being correct the moment a backend authenticates without a token
// this pod minted. A browser session would then be logged `auth=fail` on a fully
// authenticated, fully authorised 200: an operator grepping the audit stream for failed
// authentications would find every successful sign-in, and one grepping for successful
// ones would find none.
//
// Reverting `request.authenticated` to `rq.tokenFP != ""` turns this test red and leaves
// every other test in the repository green, which is what makes it a regression guard
// rather than an invariant guard.
func TestASessionWithNoTokenFingerprintIsAuditedAsAuthenticated(t *testing.T) {
	h := newMatrixHarness(t)
	var lines []string
	h.srv.Audit = func(line string) { lines = append(lines, line) }

	model := h.srv.Authority().Model()
	// Resolve a REAL principal out of the server's own authority, so the identity this
	// backend returns is one the rest of the request path can act on.
	principal, held := principalFromBareRow(t, model)
	if !held {
		t.Fatal("precondition: the fixture authority holds no principal to impersonate")
	}
	if err := h.srv.UseAuthenticator(sessionBackend{who: identity.Identity{
		Principal: principal,
		Auth:      control.Resolve(model, principal),
		// No Fingerprint — this is the whole point.
	}}); err != nil {
		t.Fatal(err)
	}

	got := h.do(t, "GET", "/api/v1/recall/alpha-notes", "", nil, "")
	if got.status != 200 {
		t.Fatalf("precondition: the session must reach the route, got %d %q", got.status, got.body)
	}
	if len(lines) == 0 {
		t.Fatal("precondition: no audit line was emitted, so the assertion below would read a field that does not exist")
	}
	line := lines[len(lines)-1]

	if !strings.Contains(line, " auth=ok ") {
		t.Fatalf("a fully authenticated, fully authorised request was audited as a FAILED authentication:\n  %s\n"+
			"`auth=` must be derived from whether a principal was resolved, not from whether a token fingerprint was taken.", line)
	}
	if !strings.Contains(line, " token=- ") {
		t.Fatalf("a session carries no token this pod minted, so `token=` must render `-`:\n  %s", line)
	}
	if !strings.Contains(line, " identity="+principal.Display+" ") {
		t.Fatalf("the audit line must name the principal:\n  %s", line)
	}
}

// TestATokenAuthenticatedRequestStillCarriesItsFingerprint is the other half, and it is
// what stops the fix above being "always print auth=ok".
//
// 🔴 THE ROTATION PROCEDURE DEPENDS ON THIS FIELD. "Read the fingerprints the startup
// line prints, then grep the audit stream for the one that should have stopped
// appearing" is the documented way to retire a credential, and both halves are the same
// 12-hex digest.
func TestATokenAuthenticatedRequestStillCarriesItsFingerprint(t *testing.T) {
	h := newMatrixHarness(t)
	var lines []string
	h.srv.Audit = func(line string) { lines = append(lines, line) }

	if got := h.do(t, "GET", "/api/v1/recall/alpha-notes", matrixWideToken, nil, ""); got.status != 200 {
		t.Fatalf("precondition: %d %q", got.status, got.body)
	}
	line := lines[len(lines)-1]
	if !strings.Contains(line, " auth=ok ") {
		t.Fatalf("a token-authenticated request must audit `auth=ok`:\n  %s", line)
	}
	if strings.Contains(line, " token=- ") {
		t.Fatalf("a token-authenticated request lost its fingerprint, which is what the rotation procedure greps for:\n  %s", line)
	}
}

// TestTheServerREFUSESAnIdentityNamingNobody.
//
// 🔴 THIS IS THE SECOND OF THE TWO CHECKS `identity.Identity.Valid` DESCRIBES, AND IT IS
// NOT A DUPLICATE OF THE FIRST. `identity.Chain` refuses to PROPAGATE such a value; a
// server wired with a SINGLE backend never passes through a chain at all, so without
// this the zero identity reaches every route — seeing nothing, because a zero
// `control.Authorization` permits nothing, but audited as an authenticated request and
// satisfying every guard that asks whether authentication succeeded.
//
// The fixture below is a single backend precisely to bypass the chain's check.
func TestTheServerRefusesAnIdentityNamingNobody(t *testing.T) {
	h := newMatrixHarness(t)
	var lines []string
	h.srv.Audit = func(line string) { lines = append(lines, line) }

	if err := h.srv.UseAuthenticator(sessionBackend{who: identity.Identity{}}); err != nil {
		t.Fatal(err)
	}
	got := h.do(t, "GET", "/api/v1/recall/alpha-notes", "", nil, "")
	if got.status != 401 {
		t.Fatalf("a backend that answered yes without naming anybody produced %d, want the uniform 401", got.status)
	}
	if len(lines) == 0 {
		t.Fatal("precondition: no audit line")
	}
	if line := lines[len(lines)-1]; !strings.Contains(line, " auth=fail ") {
		t.Fatalf("an identity naming nobody must audit as a failed authentication:\n  %s", line)
	}
}

// TestUseAuthenticatorRefusesNil.
func TestUseAuthenticatorRefusesNil(t *testing.T) {
	h := newMatrixHarness(t)
	if err := h.srv.UseAuthenticator(nil); err == nil {
		t.Fatal("a nil authenticator was installed; the next request would panic or authenticate nobody while the server looked healthy")
	}
	// And the server still works, so the refusal did not half-apply.
	if got := h.do(t, "GET", "/api/v1/recall/alpha-notes", matrixWideToken, nil, ""); got.status != 200 {
		t.Fatalf("the refused install disturbed the installed authenticator: %d", got.status)
	}
}

// TestTheDefaultServerIsMachineTokenONLY.
//
// 🔴 THE COMPATIBILITY CLAIM AT THE SEAM. `api.New` must install the machine-token
// backend and NOTHING else — in particular the trusted-header backend must be
// unreachable, so a header an attacker sets on a default deployment is inert.
func TestTheDefaultServerIsMachineTokenOnly(t *testing.T) {
	h := newMatrixHarness(t)
	held := h.srv.authenticator.Load()
	if held == nil {
		t.Fatal("`New` installed no authenticator")
	}
	if _, ok := (*held).(*identity.MachineToken); !ok {
		t.Fatalf("the default authenticator is %T, want exactly the machine-token backend", *held)
	}

	// And behaviourally: a request carrying every proxy header anybody might set, and no
	// token, is refused.
	got := h.do(t, "GET", "/api/v1/recall/alpha-notes", "", map[string]string{
		"X-Forwarded-User":       "somebody",
		"X-Forwarded-Email":      "somebody@example.test",
		"X-Auth-Request-User":    "somebody",
		DefaultProxySecretHeader: "guess",
	}, "")
	if got.status != 401 {
		t.Fatalf("a default deployment honoured a proxy identity header: %d %q", got.status, got.body)
	}
}

// DefaultProxySecretHeader is re-spelled here rather than imported so this test states
// the header it is probing, and a rename in `internal/identity` does not silently make
// this probe check a header nobody uses.
const DefaultProxySecretHeader = "X-Cairn-Proxy-Secret"

// TestTheAUTHORITYIsNotHELDTwice.
//
// 🔴 THE DEFECT THIS PINS SHIPPED IN P4's FIRST DRAFT AND WAS CAUGHT BY AN EXISTING
// GUARD GOING RED. `identity.MachineToken` built with the cache POINTER holds a second
// reference to it, so replacing `srv.authority` leaves the refresh loop maintaining one
// cache while every authentication reads another — with nothing observable except
// credentials resolved against a world nobody is keeping current. `Server.AuthorityView`
// resolves the field at call time instead.
//
// Measured as a RELATIONSHIP rather than by reading the code: swap the authority, and
// the authentication path must follow.
func TestTheAuthorityIsNotHeldTwice(t *testing.T) {
	h := newMatrixHarness(t)
	// PRECONDITION: the wide token works against the original authority.
	if got := h.do(t, "GET", "/api/v1/recall/alpha-notes", matrixWideToken, nil, ""); got.status != 200 {
		t.Fatalf("precondition: %d", got.status)
	}

	// Replace the authority with one that holds NO credentials at all.
	empty := control.NewCache(emptySource{}, control.CacheOptions{})
	if err := empty.Refresh(t.Context()); err != nil {
		t.Fatal(err)
	}
	h.srv.authority = empty

	if got := h.do(t, "GET", "/api/v1/recall/alpha-notes", matrixWideToken, nil, ""); got.status != 401 {
		t.Fatalf("the authentication path is still reading the PREVIOUS authority (%d): the backend captured the cache pointer instead of resolving the server's field, so a refresh loop and the auth path can look at different worlds",
			got.status)
	}
}

type emptySource struct{}

func (emptySource) Model(_ context.Context) (control.Model, error) { return control.NewModel(), nil }

// principalFromBareRow finds any principal the fixture authority holds, so the session
// double resolves to a real one rather than an invented id.
func principalFromBareRow(t *testing.T, m control.Model) (control.Principal, bool) {
	t.Helper()
	for _, c := range m.Credentials {
		if p, held := m.PrincipalFor(c.PrincipalKind, c.PrincipalID); held {
			return p, true
		}
	}
	return control.Principal{}, false
}
