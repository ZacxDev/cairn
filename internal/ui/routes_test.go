package ui

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/ZacxDev/cairn/internal/control"
	"github.com/ZacxDev/cairn/internal/identity"
)

// TestTheRouteLedgerMatchesTheDispatchTable is the ledger's own guard.
//
// ⚠ IT IS WEAKER THAN `api.DeclaredRoutes`'s EQUIVALENT ON PURPOSE, AND SAYING SO IS
// BETTER THAN LETTING THE SIMILARITY IMPLY OTHERWISE. The pod's ledger is checked
// against `tests/conformance/requests.json` — an external corpus that says what the
// world expects. This surface has no corpus, so the strongest available claim is
// that the ledger and the dispatcher read the same map and that the expected set is
// spelled out once here by hand. That is a GROW-or-SHRINK guard on a hand-written
// list, not a contract comparison, and the difference matters if anybody later reads
// a green here as "the browser contract is pinned".
func TestTheRouteLedgerMatchesTheDispatchTable(t *testing.T) {
	want := []string{"GET /", "GET /entries"}
	got := DeclaredRoutes()

	if len(got) == 0 {
		t.Fatal("DeclaredRoutes is EMPTY, so the comparison below is vacuous and `cairn-ui -routes` prints nothing")
	}
	if !slices.Equal(got, want) {
		t.Errorf("the declared route set is %v, the ledger names %v.\n"+
			"Adding a row to `routes` is adding a public, internet-reachable endpoint on a BROWSER surface, and "+
			"this is where somebody has to think about it. Removing one silently is the other direction and this "+
			"check refuses both.", got, want)
	}
	if !slices.IsSorted(got) {
		t.Errorf("DeclaredRoutes is not sorted (%v); `cairn-ui -routes` is read by a flake check that diffs its "+
			"output against a fixed list, so an unstable order makes that check flap", got)
	}
	// The health path is deliberately NOT in the ledger — it is answered before
	// authentication and serves no content. Asserting its absence is what stops a
	// later edit "tidying" it in.
	for _, r := range got {
		if r == "GET "+HealthPath {
			t.Errorf("%s is in the route ledger. It is answered before the authentication chain runs, so a row "+
				"here would describe it as an authenticated content route, which it is not.", HealthPath)
		}
	}
}

// staticAuth authenticates every request as one fixed principal, so the dispatch
// tests measure ROUTING rather than credentials.
type staticAuth struct{ id identity.Identity }

func (s staticAuth) Authenticate(*http.Request) (identity.Identity, error) { return s.id, nil }

// refusingAuth refuses everything, with the uniform sentinel the serving path asks
// about.
type refusingAuth struct{}

func (refusingAuth) Authenticate(*http.Request) (identity.Identity, error) {
	return identity.Identity{}, control.ErrNoCredential{}
}

type staticSource struct{ scopes []Scope }

func (s staticSource) Visible(control.Authorization) ([]Scope, error) { return s.scopes, nil }

func testIdentity() identity.Identity {
	return identity.Identity{Principal: control.Principal{
		Kind:    control.KindUser,
		ID:      "user-fixture",
		Display: "operator@example.invalid",
	}}
}

func newTestServer(t *testing.T, auth identity.Authenticator) *Server {
	t.Helper()
	srv, err := New(auth, staticSource{scopes: benignWorld()})
	if err != nil {
		t.Fatalf("the server did not build: %v", err)
	}
	return srv
}

// TestEveryServedPathComesFromTheLedger closes the blind spot `DeclaredRoutes`'s own
// comment names: a path served from anywhere other than the table.
func TestEveryServedPathComesFromTheLedger(t *testing.T) {
	srv := newTestServer(t, staticAuth{testIdentity()})

	// POSITIVE CONTROL: a declared route really is served, so the refusals below are
	// not "this handler refuses everything".
	served := 0
	for _, route := range DeclaredRoutes() {
		method, path, _ := splitRoute(route)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
		if rec.Code != http.StatusOK {
			t.Errorf("declared route %s answered %d, not 200", route, rec.Code)
			continue
		}
		served++
	}
	if served == 0 {
		t.Fatal("NO declared route answered 200, so the refusals below prove nothing about routing")
	}

	// An undeclared path gets the SAME uniform refusal a bad credential gets — never
	// a 404, which would let an unauthenticated caller map the URL space.
	for _, probe := range [][2]string{
		{"GET", "/entries/"},
		{"GET", "/admin"},
		{"POST", "/entries"},
		{"GET", "/entriesx"},
	} {
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, httptest.NewRequest(probe[0], probe[1], nil))
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s %s answered %d; an undeclared path must get the same uniform 401 a bad credential "+
				"gets, so the URL space is not mappable", probe[0], probe[1], rec.Code)
		}
	}
	t.Logf("routing: %d declared route(s) served 200, 4 undeclared probe(s) refused 401", served)
}

// TestAnUnauthenticatedRequestReachesNoRenderer pins that the chain runs BEFORE the
// ledger is consulted — a refusal must not depend on which path was asked for.
func TestAnUnauthenticatedRequestReachesNoRenderer(t *testing.T) {
	srv := newTestServer(t, refusingAuth{})
	for _, route := range DeclaredRoutes() {
		method, path, _ := splitRoute(route)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s answered %d for an unauthenticated caller, not 401", route, rec.Code)
		}
		if body := rec.Body.String(); body != "unauthorized" {
			t.Errorf("%s answered %q; the refusal body must be uniform and carry no reason — a reason that "+
				"reaches the wire is an enumeration API", route, body)
		}
	}

	// The health path is the ONE exception, and it is answered without a credential.
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest("GET", HealthPath, nil))
	if rec.Code != http.StatusOK || rec.Body.String() != healthBody {
		t.Errorf("the health path answered %d %q for an unauthenticated caller; a readiness probe broken by a "+
			"security guard is how the guard gets deleted", rec.Code, rec.Body.String())
	}
}

// TestAZeroIdentityWithNoErrorIsRefused pins the one fail-open shape the
// `Authenticator` interface makes possible. `identity.Chain` refuses it too; this is
// the server's own refusal, and a chain of length one bypasses the chain's.
func TestAZeroIdentityWithNoErrorIsRefused(t *testing.T) {
	srv := newTestServer(t, staticAuth{identity.Identity{}})
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest("GET", "/entries", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("a backend that answered `yes` without naming anybody was SERVED (%d). "+
			"`Identity{}` has a zero Authorization that permits nothing, so no scope would leak — but the "+
			"request would be audited as an authenticated one and would pass every guard that asks only "+
			"whether authentication succeeded.", rec.Code)
	}
}

// TestTheHTMLResponseCarriesItsHardeningHeaders pins the two headers that are part of
// the escaping story rather than decoration.
func TestTheHTMLResponseCarriesItsHardeningHeaders(t *testing.T) {
	srv := newTestServer(t, staticAuth{testIdentity()})
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest("GET", "/entries", nil))
	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options is %q; every byte of this body came out of a store entry, and a "+
			"browser that content-sniffs can be talked into a different type by the leading bytes", got)
	}
	if got := rec.Header().Get("Content-Security-Policy"); got == "" {
		t.Error("no Content-Security-Policy was sent")
	} else if !slices.Contains([]string{"default-src 'none'; style-src 'unsafe-inline'; base-uri 'none'; form-action 'none'"}, got) {
		t.Errorf("the Content-Security-Policy is %q, which is not the policy this surface declares. It permits "+
			"no script at all, which is a SECOND barrier behind the escaping — widening it is a decision, not a "+
			"tidy-up.", got)
	}
	if got := rec.Header().Get("Content-Type"); got != "text/html; charset=utf-8" {
		t.Errorf("Content-Type is %q", got)
	}
}

func splitRoute(route string) (method, path string, ok bool) {
	for i := 0; i < len(route); i++ {
		if route[i] == ' ' {
			return route[:i], route[i+1:], true
		}
	}
	return "", "", false
}
