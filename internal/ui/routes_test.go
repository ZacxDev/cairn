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
	want := []string{"GET /"}
	got := DeclaredRoutes()

	if len(got) == 0 {
		t.Fatal("DeclaredRoutes is EMPTY, so the comparison below is vacuous and this server dispatches nothing")
	}
	if !slices.Equal(got, want) {
		t.Errorf("the declared route set is %v, the ledger names %v.\n"+
			"Adding a row to `routes` is adding a public, internet-reachable endpoint on a BROWSER surface, and "+
			"this is where somebody has to think about it. Removing one silently is the other direction and this "+
			"check refuses both.", got, want)
	}
	if !slices.IsSorted(got) {
		t.Errorf("DeclaredRoutes is not sorted (%v); the comparison above is order-sensitive, and an unsorted "+
			"ledger derived from a map iteration would make this test flap rather than fail", got)
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
	// `GET /entries` is in this list rather than in the ledger, and that is the point
	// of naming it: it WAS a row, and the row was removed because the page behind it
	// and the page behind `GET /` are the same page. A path that stops being a route
	// must get the uniform refusal, not a 404 and not a stale handler.
	for _, probe := range [][2]string{
		{"GET", "/entries"},
		{"GET", "/entries/"},
		{"GET", "/admin"},
		{"POST", "/"},
		{"GET", "/entriesx"},
	} {
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, httptest.NewRequest(probe[0], probe[1], nil))
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s %s answered %d; an undeclared path must get the same uniform 401 a bad credential "+
				"gets, so the URL space is not mappable", probe[0], probe[1], rec.Code)
		}
	}
	t.Logf("routing: %d declared route(s) served 200, 5 undeclared probe(s) refused 401", served)
}

// countingSource records whether the authority was consulted, and is the whole
// instrument for the test below.
type countingSource struct {
	calls  int
	scopes []Scope
}

func (c *countingSource) Visible(control.Authorization) ([]Scope, error) {
	c.calls++
	return c.scopes, nil
}

// TestEveryContentRouteConsultsTheAuthority is a REGRESSION test, and the defect it
// pins shipped: `GET /` was dispatched to a handler that called [Page] with a `nil`
// scope slice and never called [Source.Visible].
//
// 🔴 THE PAGE'S EMPTY BRANCH IS AN ASSERTION ABOUT AUTHORITY, WHICH IS WHY A ROUTE
// THAT DOES NOT ASK IS A ROUTE THAT LIES. `Page` renders "No scope is visible to this
// credential. That is an authority answer, not an empty store." for any empty slice.
// Handed `nil` by a handler that asked nobody, that sentence told an operator with
// authority over every scope the exact opposite of the truth, in the one sentence
// written to distinguish those two cases.
//
// It walks the LEDGER rather than naming a path, so a content route added later is
// covered on the day it is added rather than on the day somebody remembers this file.
//
// ⚠ IT PINS "ASKED", NOT "RENDERED WHAT IT WAS TOLD". The differential fixture in
// `render_test.go` is what measures the second.
func TestEveryContentRouteConsultsTheAuthority(t *testing.T) {
	declared := DeclaredRoutes()
	if len(declared) == 0 {
		t.Fatal("DeclaredRoutes is EMPTY, so this test iterates nothing and passes vacuously")
	}

	for _, route := range declared {
		method, path, _ := splitRoute(route)
		source := &countingSource{scopes: benignWorld()}
		srv, err := New(staticAuth{testIdentity()}, source)
		if err != nil {
			t.Fatalf("the server did not build: %v", err)
		}

		// INSTRUMENT CONTROL: the counter starts at zero, so a non-zero below is the
		// request's doing and not the constructor's.
		if source.calls != 0 {
			t.Fatalf("the counting source was already called %d time(s) before any request; its count below "+
				"would measure construction rather than routing", source.calls)
		}

		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, httptest.NewRequest(method, path, nil))

		if rec.Code != http.StatusOK {
			t.Errorf("declared route %s answered %d, not 200, so the authority count below is about a refusal",
				route, rec.Code)
			continue
		}
		if source.calls == 0 {
			t.Errorf("%s rendered a page WITHOUT consulting the authority (Source.Visible called 0 times). "+
				"Page's empty branch renders \"No scope is visible to this credential. That is an authority "+
				"answer, not an empty store.\" — a sentence this route has no standing to say, and which is "+
				"FALSE for any caller who can in fact see a scope. A route that renders the page either asks "+
				"or is not a route.", route)
		}
	}

	// POSITIVE CONTROL: the counter can stay at zero, so the non-zeroes above are a
	// measurement rather than a counter that only goes up. The health path is answered
	// before the chain and before the ledger, and must consult nobody.
	health := &countingSource{scopes: benignWorld()}
	srv, err := New(staticAuth{testIdentity()}, health)
	if err != nil {
		t.Fatalf("the server did not build: %v", err)
	}
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest("GET", HealthPath, nil))
	if health.calls != 0 {
		t.Errorf("the health path consulted the authority %d time(s); it is answered before the chain runs and "+
			"must read nothing", health.calls)
	}
	t.Logf("authority consulted: once per declared route (%d route(s)), 0 times on %s",
		len(declared), HealthPath)
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
	srv.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
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
	srv.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
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
