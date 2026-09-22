package ui

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
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
//
// 🔴 THE HAND-WRITTEN LIST CARRIES EACH ROW'S CLASS, AND THAT IS WHAT MAKES `public` A
// DECISION RATHER THAN A DEFAULT. A class can only make a route LESS protected — public
// rows are dispatched before the authentication chain — so adding one has to be written
// out here, in a list somebody reads, rather than being a bit set in a map nobody
// re-reads. The plain `DeclaredRoutes()` is checked against this one by stripping the
// classes, so the two derived views cannot drift.
func TestTheRouteLedgerMatchesTheDispatchTable(t *testing.T) {
	want := []string{
		"GET / content",
		"GET /share content",
		"GET /sign-in public",
		"POST /share",
		"POST /sign-in public",
		"POST /sign-out",
		"POST /unshare",
	}
	got := DeclaredRouteLedger()

	if len(got) == 0 {
		t.Fatal("DeclaredRouteLedger is EMPTY, so the comparison below is vacuous and this server dispatches nothing")
	}
	if !slices.Equal(got, want) {
		t.Errorf("the declared route set is %v, the ledger names %v.\n"+
			"Adding a row to `routes` is adding a public, internet-reachable endpoint on a BROWSER surface, and "+
			"this is where somebody has to think about it — INCLUDING its class: `public` means the row is "+
			"dispatched BEFORE the authentication chain. Removing a row silently is the other direction and this "+
			"check refuses both.", got, want)
	}
	if !slices.IsSorted(got) {
		t.Errorf("DeclaredRouteLedger is not sorted (%v); the comparison above is order-sensitive, and an "+
			"unsorted ledger derived from a map iteration would make this test flap rather than fail", got)
	}

	// The two views are one map. Stripping each line's class must reproduce the plain
	// ledger exactly, or `cmd/cairn-ui` is counting a different set from the one
	// declared above.
	stripped := make([]string, 0, len(got))
	for _, line := range got {
		method, path, _ := splitRoute(line)
		stripped = append(stripped, method+" "+path)
	}
	if plain := DeclaredRoutes(); !slices.Equal(plain, stripped) {
		t.Errorf("DeclaredRoutes() is %v but the classed ledger strips to %v; the two are meant to be two views "+
			"of ONE map and a disagreement means there is a second copy somewhere", plain, stripped)
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

// staticSharing is a share world with no journal behind it, so the dispatch tests
// measure ROUTING rather than the control plane. `sharing_test.go` is what drives the
// real `ControlSharing` against a real `control.FileStore`.
//
// 🔴 ITS `Administrable` IS NOT DERIVED FROM THE AUTHORIZATION IT IS HANDED, AND THAT
// IS DELIBERATE FOR A FIXTURE. The dispatch tests authenticate as a principal with a
// ZERO `control.Authorization`, which permits nothing — so a faithful implementation
// would return an empty list and `GET /share` would render the "no scope is
// administrable" branch. That page is still a 200 with an HTML body, which is what
// those tests measure, but a fixture that can never populate the list would make every
// rendering assertion here vacuous.
type staticSharing struct {
	administrable []control.NamedScope
	audience      []Viewer
	revocable     []GrantRow
	candidates    []Subject
	// reads counts every authority QUESTION this fixture is asked, and it is what
	// `TestEveryContentRouteConsultsTheAuthority` reads for the share routes — the
	// same claim `countingSource` makes for the entries page, over the other half of
	// the authority seam.
	reads int
	// shared and revoked record the writes, for a test that needs to know a handler
	// reached the authority rather than refusing before it.
	shared  int
	revoked int
	// err, when set, is what every write returns.
	err error
	// readOnly makes `Writable` false, which is what the page branches on.
	readOnly bool
}

func (s *staticSharing) Administrable(control.Authorization) []control.NamedScope {
	s.reads++
	return s.administrable
}

func (s *staticSharing) Audience(control.ID) ([]Viewer, error) {
	s.reads++
	return s.audience, nil
}

func (s *staticSharing) Revocable(control.ID) ([]GrantRow, error) {
	s.reads++
	return s.revocable, nil
}

func (s *staticSharing) Candidates(control.Principal) ([]Subject, error) {
	s.reads++
	return s.candidates, nil
}

// Writable is TRUE for the dispatch fixtures, so the read-only banner is not rendered
// on every page those tests inspect. `sharing_test.go` is what varies it.
func (s *staticSharing) Writable() bool { return !s.readOnly }

func (s *staticSharing) ScopeOfGrant(control.ID) (control.ID, bool) {
	return fixtureScope, true
}

func (s *staticSharing) Share(context.Context, control.Principal, control.Authorization,
	control.ID, Subject, control.VerbSet) (Effect, error) {
	if s.err != nil {
		return Effect{}, s.err
	}
	s.shared++
	return Effect{Immediate: true}, nil
}

func (s *staticSharing) Unshare(context.Context, control.Principal, control.Authorization,
	control.ID) (Effect, error) {
	if s.err != nil {
		return Effect{}, s.err
	}
	s.revoked++
	return Effect{Immediate: true}, nil
}

// fixtureNamedScope is `fixtureScope` with the display name the fixture world gives
// it, so the dispatch fixtures and `fixtures_test.go` name ONE scope rather than two.
var fixtureNamedScope = control.NamedScope{ID: fixtureScope, Name: "quarry-notes"}

func benignSharing() *staticSharing {
	return &staticSharing{
		administrable: []control.NamedScope{fixtureNamedScope},
		audience: []Viewer{
			{Display: "rowan@notes.example.invalid", Kind: control.KindUser, Verbs: "read,write,admin"},
		},
		candidates: []Subject{
			{Kind: control.KindUser, ID: control.DerivedID(control.PrefixUser, "wren"),
				Display: "wren@notes.example.invalid"},
		},
	}
}

func testIdentity() identity.Identity {
	return identity.Identity{Principal: control.Principal{
		Kind:    control.KindUser,
		ID:      "user-fixture",
		Display: "operator@example.invalid",
	}}
}

// staticCredentials resolves exactly one token string, so the sign-in exchange can be
// driven without a control journal. Everything else it refuses.
type staticCredentials struct {
	token     string
	principal control.Principal
	auth      control.Authorization
}

func (s staticCredentials) Authenticate(token string) (control.Principal, control.Authorization, error) {
	if token == "" || token != s.token {
		return control.Principal{}, control.Authorization{}, control.ErrNoCredential{}
	}
	return s.principal, s.auth, nil
}

// testConfig is the fully wired server every dispatch test starts from. Each field is
// spelled here once so a test that needs to vary ONE of them varies exactly one.
func testConfig(t *testing.T, auth identity.Authenticator) Config {
	t.Helper()
	return Config{
		Auth:        auth,
		Credentials: staticCredentials{token: testCredential, principal: testIdentity().Principal},
		Source:      staticSource{scopes: benignWorld()},
		Sharing:     benignSharing(),
		Sessions:    mustSessions(t),
		Log:         io.Discard,
	}
}

// testCredential is SYNTHETIC. See `fixtures_test.go` for the rule.
const testCredential = "fixture-credential-value-which-is-not-a-real-token"

func newTestServer(t *testing.T, auth identity.Authenticator) *Server {
	t.Helper()
	srv, err := New(testConfig(t, auth))
	if err != nil {
		t.Fatalf("the server did not build: %v", err)
	}
	return srv
}

// ledgerClasses returns the class field of a classed ledger line, or "" for a row with
// no class.
func ledgerClasses(line string) string {
	fields := strings.Fields(line)
	if len(fields) < 3 {
		return ""
	}
	return fields[2]
}

// contentRoutes and publicRoutes walk the LEDGER rather than naming paths, so a row
// added later is covered on the day it is added.
func contentRoutes() []string {
	var out []string
	for _, line := range DeclaredRouteLedger() {
		if strings.Contains(ledgerClasses(line), "content") {
			out = append(out, line)
		}
	}
	return out
}

func publicRoutes() []string {
	var out []string
	for _, line := range DeclaredRouteLedger() {
		if strings.Contains(ledgerClasses(line), "public") {
			out = append(out, line)
		}
	}
	return out
}

// TestEveryServedPathComesFromTheLedger closes the blind spot `DeclaredRoutes`'s own
// comment names: a path served from anywhere other than the table.
func TestEveryServedPathComesFromTheLedger(t *testing.T) {
	srv := newTestServer(t, staticAuth{testIdentity()})

	// POSITIVE CONTROL: every GET route really is served, so the refusals below are
	// not "this handler refuses everything". The POST rows are excluded because they
	// answer a redirect rather than a 200 — `TestTheWholeSessionLifecycle` is what
	// drives those.
	served := 0
	for _, route := range DeclaredRouteLedger() {
		method, path, _ := splitRoute(route)
		if method != http.MethodGet {
			continue
		}
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
		if rec.Code != http.StatusOK {
			t.Errorf("declared route %s %s answered %d, not 200", method, path, rec.Code)
			continue
		}
		served++
	}
	if served == 0 {
		t.Fatal("NO declared GET route answered 200, so the refusals below prove nothing about routing")
	}

	// An undeclared path gets the SAME uniform refusal a bad credential gets — never
	// a 404, which would let an unauthenticated caller map the URL space.
	// `GET /entries` is in this list rather than in the ledger, and that is the point
	// of naming it: it WAS a row, and the row was removed because the page behind it
	// and the page behind `GET /` are the same page. A path that stops being a route
	// must get the uniform refusal, not a 404 and not a stale handler.
	//
	// ⚠ EVERY PROBE IS A GET AND THE NEAR-MISSES OF THE PUBLIC ROWS ARE IN IT. A POST
	// probe would be refused by the same-origin gate before the ledger is ever
	// consulted, so it would measure gate (2) rather than routing and would pass with
	// the ledger deleted. The public rows themselves are NOT probed here: they answer
	// 200 to anybody by design, which is the narrowing `routes` states.
	for _, probe := range [][2]string{
		{"GET", "/entries"},
		{"GET", "/entries/"},
		{"GET", "/admin"},
		{"GET", "/entriesx"},
		{"GET", "/sign-out"},
		{"GET", "/sign-in/"},
		{"GET", "/sign-inx"},
	} {
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, httptest.NewRequest(probe[0], probe[1], nil))
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s %s answered %d; an undeclared path must get the same uniform 401 a bad credential "+
				"gets, so the URL space is not mappable", probe[0], probe[1], rec.Code)
		}
	}
	// 🔴 AND THE STATE-CHANGING PROBES, WITH A CORRECT ORIGIN SO THEY MEASURE ROUTING.
	// `POST /` was in the list above before Phase B; it moved here because the
	// same-origin gate now runs BEFORE the ledger, so a POST without an `Origin` would
	// be refused at 403 by gate (2) and the probe would pass with the ledger deleted.
	// Giving it a correct origin puts the ledger back in the path.
	crossed := 0
	for _, probe := range [][2]string{
		{"POST", "/"},
		{"POST", "/admin"},
		{"PUT", "/sign-out"},
		{"DELETE", "/sign-in"},
	} {
		rec := httptest.NewRecorder()
		r := httptest.NewRequest(probe[0], probe[1], nil)
		r.Header.Set("Origin", "https://"+r.Host)
		srv.ServeHTTP(rec, r)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s %s with a correct Origin answered %d; an undeclared method/path pair must get the same "+
				"uniform 401 a bad credential gets", probe[0], probe[1], rec.Code)
			continue
		}
		crossed++
	}
	if crossed == 0 {
		t.Error("NO same-origin state-changing probe reached the uniform 401, so the ledger is not in their path")
	}
	t.Logf("routing: %d declared GET route(s) served 200, 7 undeclared GET probe(s) refused 401, "+
		"%d same-origin state-changing probe(s) refused 401", served, crossed)
}

// contentAuthority names, per content route, WHICH half of the authority seam its
// rendered answer comes from.
//
// 🔴 IT IS HAND-WRITTEN AND THAT IS THE POINT, BECAUSE THE LEDGER CANNOT SAY IT. A row's
// `content` class declares that the route renders an answer about authority; it does not
// and cannot say which authority was asked. Deriving the expectation from the route would
// mean accepting whichever one it happened to call — which is exactly the sum this table
// replaced, measured green on the defect the test exists for. A content route missing from
// this map FAILS, so a route added later still forces the decision on the day it is added
// rather than being silently absorbed.
var contentAuthority = map[string]string{
	"GET / content":      "source",
	"GET /share content": "sharing",
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
	declared := contentRoutes()
	if len(declared) == 0 {
		t.Fatal("NO route in the ledger carries the `content` class, so this test iterates nothing and passes " +
			"vacuously. If a content route stopped being classed as one, the class is the thing to fix.")
	}

	for _, route := range declared {
		method, path, _ := splitRoute(route)
		source := &countingSource{scopes: benignWorld()}
		sharing := benignSharing()
		cfg := testConfig(t, staticAuth{testIdentity()})
		cfg.Source = source
		cfg.Sharing = sharing
		srv, err := New(cfg)
		if err != nil {
			t.Fatalf("the server did not build: %v", err)
		}

		// INSTRUMENT CONTROL: both counters start at zero, so a non-zero below is the
		// request's doing and not the constructor's.
		if source.calls != 0 || sharing.reads != 0 {
			t.Fatalf("the counting fixtures were already called (source %d, sharing %d) before any request; "+
				"their counts below would measure construction rather than routing", source.calls, sharing.reads)
		}

		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, httptest.NewRequest(method, path, nil))

		if rec.Code != http.StatusOK {
			t.Errorf("declared route %s answered %d, not 200, so the authority count below is about a refusal",
				route, rec.Code)
			continue
		}
		// 🔴 THE EXPECTATION IS PER ROUTE, AND A SUM WAS MEASURED TO DESTROY THIS GUARD.
		// A previous version of this test asked only whether `source.calls+sharing.reads`
		// was non-zero. That is satisfiable by the WRONG authority: the defect this test
		// was written for — `handlePage` rendering `Page` with a `nil` slice and never
		// calling `Source.Visible` — was re-applied with a `s.sharing.Administrable` call
		// in its place, and this test PASSED. A guard that any authority can satisfy is a
		// guard about none of them.
		want, declared := contentAuthority[route]
		if !declared {
			t.Errorf("%s is classed `content` and this test does not know which authority its answer comes "+
				"from. Add it to `contentAuthority` — that is the decision, and it is deliberately not "+
				"derivable from the ledger: the ledger says a route renders an answer about authority, not "+
				"WHICH authority it asked.", route)
			continue
		}
		got := map[string]int{"source": source.calls, "sharing": sharing.reads}[want]
		if got == 0 {
			t.Errorf("%s rendered a page WITHOUT consulting the %s authority it answers from (source %d, "+
				"sharing %d). Both pages carry a sentence that only an authority answer licenses — `Page`'s "+
				"\"No scope is visible to this credential. That is an authority answer, not an empty store.\" "+
				"and the share index's \"No scope is administrable by this credential\" — and either is FALSE "+
				"for a caller who can in fact see one. Consulting the OTHER authority does not license "+
				"either sentence.", route, want, source.calls, sharing.reads)
		}
	}

	// POSITIVE CONTROL: the counter can stay at zero, so the non-zeroes above are a
	// measurement rather than a counter that only goes up. The health path is answered
	// before the chain and before the ledger, and must consult nobody.
	health := &countingSource{scopes: benignWorld()}
	healthSharing := benignSharing()
	cfg := testConfig(t, staticAuth{testIdentity()})
	cfg.Source = health
	cfg.Sharing = healthSharing
	srv, err := New(cfg)
	if err != nil {
		t.Fatalf("the server did not build: %v", err)
	}
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest("GET", HealthPath, nil))
	if health.calls != 0 || healthSharing.reads != 0 {
		t.Errorf("the health path consulted the authority (source %d, sharing %d time(s)); it is answered "+
			"before the chain runs and must read nothing", health.calls, healthSharing.reads)
	}

	// 🔴 AND THE HOLE THE `content` CLASS ITSELF OPENS, CLOSED BY DERIVING THE CLASS
	// RATHER THAN TRUSTING IT. The loop above walks the rows that DECLARE themselves
	// content, so a new page route added without the class would be skipped by it —
	// and the ledger test above passes for a row written out with no class at all. So:
	// any non-public route that answers 200 with an HTML body IS a content route,
	// whatever its class says, and must carry the class.
	classified := 0
	for _, route := range DeclaredRouteLedger() {
		if strings.Contains(ledgerClasses(route), "public") {
			continue
		}
		method, path, _ := splitRoute(route)
		if method != http.MethodGet {
			continue
		}
		rec := httptest.NewRecorder()
		srv := newTestServer(t, staticAuth{testIdentity()})
		srv.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
		isHTML := rec.Code == http.StatusOK &&
			strings.HasPrefix(rec.Header().Get("Content-Type"), "text/html")
		if !isHTML {
			continue
		}
		classified++
		if !strings.Contains(ledgerClasses(route), "content") {
			t.Errorf("%s answers 200 with an HTML body and is NOT classed `content`, so the walk above skips it "+
				"and nothing requires it to consult the authority. `Page`'s empty branch asserts an answer about "+
				"AUTHORITY; a route that renders it either asks or is not a route. Add the class.", route)
		}
	}
	if classified == 0 {
		t.Fatal("NO non-public route answered 200 with an HTML body, so the derivation above inspected nothing " +
			"and could not have contradicted any row's class")
	}

	t.Logf("authority consulted: once per declared content route (%d route(s)), 0 times on %s; %d HTML-answering "+
		"route(s) cross-checked against the class they declare", len(declared), HealthPath, classified)
}

// TestAnUnauthenticatedRequestReachesNoRenderer pins that the chain runs BEFORE the
// ledger is consulted — a refusal must not depend on which path was asked for.
func TestAnUnauthenticatedRequestReachesNoRenderer(t *testing.T) {
	srv := newTestServer(t, refusingAuth{})
	checked := 0
	for _, route := range DeclaredRouteLedger() {
		if strings.Contains(ledgerClasses(route), "public") {
			continue
		}
		method, path, _ := splitRoute(route)
		rec := httptest.NewRecorder()
		r := httptest.NewRequest(method, path, nil)
		// The same-origin gate runs BEFORE the chain, so a POST row without an Origin
		// would be refused at 403 and this test would report a refusal it did not
		// measure. Setting a correct Origin makes the chain the only thing left that
		// can refuse.
		r.Header.Set("Origin", "https://"+r.Host)
		srv.ServeHTTP(rec, r)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s answered %d for an unauthenticated caller, not 401", route, rec.Code)
		}
		if body := rec.Body.String(); body != "unauthorized" {
			t.Errorf("%s answered %q; the refusal body must be uniform and carry no reason — a reason that "+
				"reaches the wire is an enumeration API", route, body)
		}
		checked++
	}
	if checked == 0 {
		t.Fatal("every declared route is PUBLIC, so this test skipped all of them and measured nothing. A " +
			"surface on which no row is behind the chain is a surface with no authentication.")
	}

	// The PUBLIC rows are the stated exception and they must answer WITHOUT a
	// credential, or the sign-in flow is unreachable and the surface has no way in.
	for _, route := range publicRoutes() {
		method, path, _ := splitRoute(route)
		if method != http.MethodGet {
			continue
		}
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
		if rec.Code != http.StatusOK {
			t.Errorf("the public row %s answered %d for an unauthenticated caller; a sign-in page behind the "+
				"authentication chain is a door locked from the inside", route, rec.Code)
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
	// 🔴 THE POLICY IS PINNED AS A LITERAL HERE RATHER THAN READ FROM
	// `ContentSecurityPolicy`, AND THAT IS THE POINT. A test comparing the header
	// against the constant the handler sets is a test that `a == a`: it goes green for
	// every edit of the constant, including one that deletes `default-src 'none'`.
	// The literal below is the contract; changing it is a decision somebody takes here.
	const want = "default-src 'none'; style-src 'unsafe-inline'; base-uri 'none'; form-action 'self'"
	if got := rec.Header().Get("Content-Security-Policy"); got == "" {
		t.Error("no Content-Security-Policy was sent")
	} else if got != want {
		t.Errorf("the Content-Security-Policy is %q, want %q. It permits no script at all, which is a SECOND "+
			"barrier behind the escaping — widening it is a decision, not a tidy-up. `form-action 'self'` is as "+
			"wide as it goes: it admits this surface's own sign-in and sign-out forms and refuses a form that "+
			"posts anywhere else.", got, want)
	}
	if got := rec.Header().Get("Content-Type"); got != "text/html; charset=utf-8" {
		t.Errorf("Content-Type is %q", got)
	}
}

// splitRoute parses a ledger line. It accepts BOTH spellings — `"<METHOD> <path>"` and
// `"<METHOD> <path> <classes>"` — because both ledgers are read here and a parser that
// silently folded a class into the path would make a route probe drive `"/ content"`,
// get the uniform 401 it expects for an unknown path, and pass.
func splitRoute(route string) (method, path string, ok bool) {
	fields := strings.Fields(route)
	if len(fields) < 2 {
		return "", "", false
	}
	return fields[0], fields[1], true
}
