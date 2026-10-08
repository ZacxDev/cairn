package ui

import (
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strings"
	"testing"
	"time"
)

// browserAccept is what a real browser sends on a top-level navigation. A literal rather than
// "text/html", so the test drives the header shape a person's browser does.
const browserAccept = "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8"

// refusedBearer is a SYNTHETIC value no fixture authority resolves; the refusing chain
// answers it with the uniform refusal.
const refusedBearer = "fixture-not-a-credential"

// navigate builds a browser-shaped request: `Accept` as a browser sends it, the test host, and
// an `Origin` on an unsafe method so gate (2) is never what answers.
func navigate(method, target string) *http.Request {
	r := httptest.NewRequest(method, target, nil)
	r.Host = testHost
	r.Header.Set("Accept", browserAccept)
	if method != http.MethodGet && method != http.MethodHead {
		r.Header.Set("Origin", "https://"+testHost)
	}
	return r
}

func serve(srv *Server, r *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, r)
	return rec
}

// hiddenNext reads the return-to value out of a rendered page's hidden field, which is the
// ONLY way a browser gets it — the same reason `csrfOf` reads the CSRF token from the page.
// It returns every occurrence, so a test can see the field is in BOTH forms.
func hiddenNext(body string) []string {
	const marker = `name="` + FieldNext + `" value="`
	var out []string
	for rest := body; ; {
		i := strings.Index(rest, marker)
		if i < 0 {
			return out
		}
		rest = rest[i+len(marker):]
		j := strings.Index(rest, `"`)
		if j < 0 {
			return out
		}
		out = append(out, html.UnescapeString(rest[:j]))
		rest = rest[j:]
	}
}

// TestSafeNextRefusesTheOpenRedirectCorpus is the validator's table, and every row is a
// literal: the expectation is never derived from the function under test.
//
// 🔴 EVERY REFUSED ROW EXPECTS "", AND THE POSITIVE CONTROLS EXPECT THEIR OWN INPUT BACK. A
// validator that refused everything would pass the first half; the second half is what makes
// a refusal evidence. The rows are pairwise distinct and none equals `RootPath` except the one
// positive control that names it, so a mutant hardcoding `/` cannot pass the others.
func TestSafeNextRefusesTheOpenRedirectCorpus(t *testing.T) {
	overlong := "/" + strings.Repeat("a", maxNextLen)  // maxNextLen+1 bytes
	atBound := "/" + strings.Repeat("b", maxNextLen-1) // exactly maxNextLen bytes

	for _, tc := range []struct {
		name, in, want string
	}{
		// the open-redirect corpus — each must be DROPPED
		{"a network-path reference", "//evil.invalid", ""},
		{"a network-path reference with a path", "//evil.invalid/scope?id=scp_a", ""},
		{"a backslash where the authority starts", `/\evil.invalid`, ""},
		{"a backslash later in the path", `/scope\..\x`, ""},
		{"an absolute https URL", "https://evil.invalid/", ""},
		{"a javascript: URL", "javascript:alert(1)", ""},
		{"a relative path with no leading slash", "scope?id=scp_b", ""},
		{"a tab between the slashes (browsers strip it)", "/\t/evil.invalid", ""},
		{"a newline between the slashes", "/\n/evil.invalid", ""},
		{"a carriage return", "/scope\r?id=scp_c", ""},
		{"a space", "/scope ?id=scp_d", ""},
		{"a NUL", "/scope\x00", ""},
		{"DEL", "/scope\x7f", ""},
		{"non-ASCII", "/scöpe", ""},
		{"overlong by one byte", overlong, ""},
		{"empty", "", ""},
		{"the sign-in page itself", "/sign-in", ""},
		{"a sign-in loop carrying its own next", "/sign-in?next=%2Fscope", ""},
		{"sign-out", "/sign-out", ""},
		{"the GitHub start row", "/sign-in/github", ""},
		{"the GitHub callback", "/sign-in/github/callback?code=fixture-code", ""},
		{"the sign-in page, percent-encoded", "/sign%2Din", ""},
		{"the sign-in page via a dot segment", "/./sign-in", ""},
		{"an escape that does not decode", "/scope%zz", ""},
		// 🔴 THE DECODE DECISION: an escaped slash pair is a same-origin PATH, not an authority
		// — a browser does not decode `%2F` into a delimiter when resolving a reference — so it
		// is ACCEPTED, verbatim. `TestAnEscapedSlashPairLandsOnThisOrigin` pins what it does.
		{"escaped slashes are data, not an authority", "/%2F%2Fevil.invalid", "/%2F%2Fevil.invalid"},
		// positive controls — each must come back UNCHANGED
		{"a scope page with two parameters", "/scope?id=scp_x&tab=sessions", "/scope?id=scp_x&tab=sessions"},
		{"the root", "/", "/"},
		{"the arcs index with a flag", "/arcs?all=1", "/arcs?all=1"},
		{"a path that merely STARTS with the sign-in spelling", "/sign-inx", "/sign-inx"},
		{"exactly at the length bound", atBound, atBound},
	} {
		if got := safeNext(tc.in); got != tc.want {
			t.Errorf("safeNext(%q) [%s] = %q, want %q", tc.in, tc.name, got, tc.want)
		}
	}
}

// TestAnUnauthenticatedBrowserIsSentToSignInWithItsReturnPath is the regression test for the
// operator's report: a bookmarked page opened with no session answered "unauthorized" with no
// way in. RED at the base (`a20ebab`): every row below answered 401.
func TestAnUnauthenticatedBrowserIsSentToSignInWithItsReturnPath(t *testing.T) {
	srv := newTestServer(t, refusingAuth{})

	for _, tc := range []struct {
		method, target, want string
	}{
		{"GET", "/scope?id=scp_x&tab=sessions", "/sign-in?next=%2Fscope%3Fid%3Dscp_x%26tab%3Dsessions"},
		{"HEAD", "/arcs?all=1", "/sign-in?next=%2Farcs%3Fall%3D1"},
		{"GET", "/entry?scope=scp_y&ref=runbook", "/sign-in?next=%2Fentry%3Fscope%3Dscp_y%26ref%3Drunbook"},
		// The root is the default landing, so it carries no `next` — exactly the redirect it
		// answered before this change.
		{"GET", "/", "/sign-in"},
		// A request-URI `safeNext` refuses still gets the way in, just with nowhere to return to.
		{"GET", "//evil.invalid", "/sign-in"},
	} {
		rec := serve(srv, navigate(tc.method, tc.target))
		if rec.Code != http.StatusSeeOther {
			t.Errorf("an unauthenticated browser %s %s answered %d, want 303", tc.method, tc.target, rec.Code)
			continue
		}
		if got := rec.Header().Get("Location"); got != tc.want {
			t.Errorf("%s %s redirected to %q, want %q", tc.method, tc.target, got, tc.want)
		}
	}
}

// TestAFailedBearerAndANonBrowserKeepTheUniform401 is the machine half: every request that is
// NOT a browser navigation without an `Authorization` header keeps the 401 byte for byte.
// Each row holds every other dimension at the redirecting value, so a predicate that dropped
// ONE conjunct is visible here.
func TestAFailedBearerAndANonBrowserKeepTheUniform401(t *testing.T) {
	srv := newTestServer(t, refusingAuth{})

	for _, tc := range []struct {
		name  string
		build func() *http.Request
	}{
		{"a GET presenting a bearer that failed", func() *http.Request {
			r := navigate("GET", "/scope?id=scp_e")
			r.Header.Set("Authorization", "Bearer "+refusedBearer)
			return r
		}},
		{"a GET presenting an EMPTY Authorization header", func() *http.Request {
			r := navigate("GET", "/scope?id=scp_f")
			r.Header["Authorization"] = []string{""}
			return r
		}},
		{"a HEAD presenting a bearer that failed", func() *http.Request {
			r := navigate("HEAD", "/arcs")
			r.Header.Set("Authorization", "Bearer "+refusedBearer+"-other")
			return r
		}},
		{"a same-origin POST from a browser, no credential", func() *http.Request {
			return navigate("POST", RingPath)
		}},
		{"a same-origin PUT from a browser, no credential", func() *http.Request {
			return navigate("PUT", "/scope?id=scp_g")
		}},
		{"a GET that asked for anything (*/*)", func() *http.Request {
			r := navigate("GET", "/scope?id=scp_h")
			r.Header.Set("Accept", "*/*")
			return r
		}},
		{"a GET with no Accept at all", func() *http.Request {
			r := navigate("GET", "/scope?id=scp_i")
			r.Header.Del("Accept")
			return r
		}},
		{"an OPTIONS, which is safe but not a navigation", func() *http.Request {
			return navigate("OPTIONS", "/scope?id=scp_j")
		}},
	} {
		rec := serve(srv, tc.build())
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s answered %d (Location %q), want 401 — a program must get the refusal it was written "+
				"against, never an HTML redirect", tc.name, rec.Code, rec.Header().Get("Location"))
			continue
		}
		if body := rec.Body.String(); body != "unauthorized" {
			t.Errorf("%s answered 401 with body %q, want the uniform %q", tc.name, body, "unauthorized")
		}
	}
}

// TestTheSignInRedirectIsUniformAcrossWhatTheTargetNames is the not-an-oracle property.
//
// 🔴 TWO HALVES, BECAUSE "THE BYTES MATCHED" IS SATISFIABLE BY A FIXTURE WHERE NOTHING
// DIFFERED. (1) The responses for an existing scope, a non-existent one, an existing entry, a
// non-existent entry and an arc are byte-identical once the echoed target is substituted —
// status, every header, body. (2) The SOURCE was consulted zero times, which is the structural
// reason (1) holds for every world and not just this one: nothing that knows whether a target
// exists or is readable ran. The positive control is the same server authenticated, where the
// source IS consulted, so the zero is not a counter wired to nothing.
func TestTheSignInRedirectIsUniformAcrossWhatTheTargetNames(t *testing.T) {
	src := &countingSource{scopes: benignWorld()}
	cfg := testConfig(t, refusingAuth{})
	cfg.Source = src
	srv, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}

	dump := func(rec *httptest.ResponseRecorder, target string) string {
		loc := "/sign-in?" + url.Values{FieldNext: {target}}.Encode()
		var b strings.Builder
		b.WriteString(http.StatusText(rec.Code))
		keys := make([]string, 0, len(rec.Header()))
		for k := range rec.Header() {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			b.WriteString("\n" + k + ": " + strings.Join(rec.Header()[k], ","))
		}
		b.WriteString("\n\n" + rec.Body.String())
		s := strings.ReplaceAll(b.String(), html.EscapeString(loc), "<LOC>")
		return strings.ReplaceAll(s, loc, "<LOC>")
	}

	targets := []string{
		"/scope?id=" + string(fixtureScope),       // exists in the source
		"/scope?id=scp_nonexistentfixture0000000", // does not
		"/entry?scope=" + string(fixtureScope) + "&ref=runbook",
		"/entry?scope=" + string(fixtureScope) + "&ref=no-such-entry",
		"/arc?id=arc_nonexistentfixture",
	}
	var first string
	for i, target := range targets {
		rec := serve(srv, navigate("GET", target))
		if rec.Code != http.StatusSeeOther {
			t.Fatalf("%s answered %d, want 303", target, rec.Code)
		}
		d := dump(rec, target)
		if !strings.Contains(d, "Location: <LOC>") {
			t.Fatalf("%s: the Location was not exactly the target-derived value, so the substitution below "+
				"would compare nothing:\n%s", target, d)
		}
		if i == 0 {
			first = d
			continue
		}
		if d != first {
			t.Errorf("the redirect for %s differs from the one for %s beyond the echoed target — an "+
				"existence oracle:\n--- %s\n%s\n--- %s\n%s", target, targets[0], targets[0], first, target, d)
		}
	}
	if src.calls != 0 {
		t.Errorf("the source was consulted %d time(s) on the way to an unauthenticated redirect; anything it "+
			"answered could make the redirect depend on what exists", src.calls)
	}

	// POSITIVE CONTROL: authenticated, the same counter moves.
	cfg2 := testConfig(t, staticAuth{testIdentity()})
	src2 := &countingSource{scopes: benignWorld()}
	cfg2.Source = src2
	authed, err := New(cfg2)
	if err != nil {
		t.Fatal(err)
	}
	if rec := serve(authed, navigate("GET", targets[0])); rec.Code != http.StatusOK {
		t.Fatalf("POSITIVE CONTROL: an authenticated GET of %s answered %d", targets[0], rec.Code)
	}
	if src2.calls == 0 {
		t.Fatal("POSITIVE CONTROL FAILED: an authenticated page did not consult the source either, so the zero " +
			"above is not evidence")
	}
	t.Logf("uniformity: %d targets, byte-identical modulo the echoed target; source calls %d unauthenticated, "+
		"%d on the authenticated control", len(targets), src.calls, src2.calls)
}

// TestAnExpiredCookieIsSentToSignInAndTheCredentialFormLandsBack is the full round trip, over
// the REAL cookie backend: sign in, let the session expire, open a deep page, follow the
// redirect, read the hidden field the page rendered, fail once, then sign in from the form.
func TestAnExpiredCookieIsSentToSignInAndTheCredentialFormLandsBack(t *testing.T) {
	l := newLive(t)
	const target = "/scope?id=scp_x&tab=sessions"

	old := l.signIn(testCredential)
	l.now = l.now.Add(2 * time.Hour) // the TTL is one hour

	// (1) an expired cookie, browser-shaped → 303 with the return path
	r := navigate("GET", target)
	r.AddCookie(old)
	rec := serve(l.srv, r)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("an EXPIRED session's browser GET answered %d, want 303 — the case the operator hit", rec.Code)
	}
	loc := rec.Header().Get("Location")
	if want := "/sign-in?next=%2Fscope%3Fid%3Dscp_x%26tab%3Dsessions"; loc != want {
		t.Fatalf("Location = %q, want %q", loc, want)
	}

	// (2) the sign-in page carries it, in the credential form (the provider is unconfigured
	// in this world, so there is exactly one form)
	page := serve(l.srv, navigate("GET", loc))
	if page.Code != http.StatusOK {
		t.Fatalf("the sign-in page answered %d", page.Code)
	}
	if got := hiddenNext(page.Body.String()); len(got) != 1 || got[0] != target {
		t.Fatalf("the sign-in page carries return-to field(s) %q, want exactly [%q]", got, target)
	}

	// (3) a refused attempt re-renders WITH it
	bad := l.do(http.MethodPost, SignInPath, url.Values{FieldToken: {"fixture-wrong-credential"}, FieldNext: {target}})
	if bad.Code != http.StatusUnauthorized {
		t.Fatalf("a wrong credential answered %d, want 401", bad.Code)
	}
	if got := hiddenNext(bad.Body.String()); len(got) != 1 || got[0] != target {
		t.Errorf("the refused re-render carries return-to field(s) %q, want exactly [%q] — a person who mistypes "+
			"once must not lose where they were going", got, target)
	}

	// (4) the form POST lands on the original page
	ok := l.do(http.MethodPost, SignInPath, url.Values{FieldToken: {testCredential}, FieldNext: {target}}, old)
	if ok.Code != http.StatusSeeOther {
		t.Fatalf("sign-in answered %d: %s", ok.Code, ok.Body.String())
	}
	if got := ok.Header().Get("Location"); got != target {
		t.Errorf("a completed sign-in landed on %q, want %q", got, target)
	}
	if sessionCookieOf(t, ok) == nil {
		t.Error("the sign-in that landed back set no session cookie")
	}

	// (5) a hostile next in the POST lands on `/`, and the refusal does not echo it
	evil := l.do(http.MethodPost, SignInPath, url.Values{FieldToken: {testCredential}, FieldNext: {"//evil.invalid"}})
	if got := evil.Header().Get("Location"); got != RootPath {
		t.Errorf("a sign-in posting next=//evil.invalid landed on %q, want %q", got, RootPath)
	}
	badEvil := l.do(http.MethodPost, SignInPath, url.Values{FieldToken: {"fixture-wrong-credential-2"}, FieldNext: {"//evil.invalid"}})
	if strings.Contains(badEvil.Body.String(), "evil.invalid") {
		t.Error("a refused sign-in reflected a REJECTED next value into the page")
	}
}

// TestTheReturnPathIsReadFromTheBODYOfAPost: `POST /sign-in?next=…` with nothing in the body
// lands on `/`. `requestedNext` reads `PostFormValue`, never the query union `FormValue` is.
func TestTheReturnPathIsReadFromTheBODYOfAPost(t *testing.T) {
	l := newLive(t)
	rec := l.do(http.MethodPost, SignInPath+"?next=%2Farcs", url.Values{FieldToken: {testCredential}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("sign-in answered %d", rec.Code)
	}
	if got := rec.Header().Get("Location"); got != RootPath {
		t.Errorf("a next carried only in a POST's QUERY string was honoured: landed on %q, want %q", got, RootPath)
	}
}

// TestTheSignInPageEscapesTheReturnPathAndCarriesItInBothForms renders the page directly with
// a value `safeNext` accepts but which carries attribute-breaking characters.
func TestTheSignInPageEscapesTheReturnPathAndCarriesItInBothForms(t *testing.T) {
	const hostile = `/scope?id="><b>x</b>&q='y'`
	if safeNext(hostile) != hostile {
		t.Fatalf("PRECONDITION: the fixture is meant to be a value the validator accepts")
	}
	page := renderNode(t, SignInPage("", true, hostile))
	if strings.Contains(page, `"><b>`) {
		t.Fatal("the return-to value broke out of its attribute")
	}
	got := hiddenNext(page)
	if len(got) != 2 || got[0] != hostile || got[1] != hostile {
		t.Errorf("return-to fields %q, want the value in BOTH forms (provider, credential)", got)
	}
	if n := hiddenNext(renderNode(t, SignInPage("", true, ""))); len(n) != 0 {
		t.Errorf("an empty return-to rendered %d field(s), want none", len(n))
	}
}

// TestAnAlreadySignedInBrowserIsSentStraightToItsReturnPath is item 7: `/sign-in?next=X` for
// a browser that is signed in goes straight to X; without a valid `next` the form renders as
// it always did.
func TestAnAlreadySignedInBrowserIsSentStraightToItsReturnPath(t *testing.T) {
	srv := newTestServer(t, staticAuth{testIdentity()})
	rec := serve(srv, navigate("GET", SignInPath+"?next=%2Farcs%3Fall%3D1"))
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/arcs?all=1" {
		t.Errorf("a signed-in browser at /sign-in?next=/arcs?all=1 answered %d → %q, want 303 → %q",
			rec.Code, rec.Header().Get("Location"), "/arcs?all=1")
	}
	for _, q := range []string{"", "?next=%2F%2Fevil.invalid"} {
		rec := serve(srv, navigate("GET", SignInPath+q))
		if rec.Code != http.StatusOK {
			t.Errorf("a signed-in browser at /sign-in%s answered %d (Location %q), want the 200 form", q, rec.Code,
				rec.Header().Get("Location"))
		}
	}
	// And signed OUT, the same URL renders the form — the redirect depends on the identity.
	out := serve(newTestServer(t, refusingAuth{}), navigate("GET", SignInPath+"?next=%2Farcs%3Fall%3D1"))
	if out.Code != http.StatusOK {
		t.Errorf("a signed-out browser at /sign-in?next=… answered %d, want 200", out.Code)
	}
}

// startFlightWith drives the GitHub start row with `form` as its body and returns the flight
// cookie and the response.
func startFlightWith(t *testing.T, srv *Server, target string, form url.Values) (*http.Cookie, *httptest.ResponseRecorder) {
	t.Helper()
	r := httptest.NewRequest("POST", target, strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Origin", "https://"+r.Host)
	rec := serve(srv, r)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("PRECONDITION: the start row answered %d", rec.Code)
	}
	for _, c := range rec.Result().Cookies() {
		if c.Name == oauthFlightCookieName {
			return c, rec
		}
	}
	t.Fatal("PRECONDITION: no flight cookie")
	return nil, nil
}

func callback(srv *Server, flight *http.Cookie) *httptest.ResponseRecorder {
	r := httptest.NewRequest("GET", OAuthCallbackPath+"?code=fixture-authorization-code", nil)
	r.AddCookie(flight)
	return serve(srv, r)
}

// TestTheGitHubFlightCarriesTheReturnPathServerSide is item 5.
//
// 🔴 IT READS THE TABLE'S OWN INTERNALS FOR THE "SERVER-SIDE" AND "CLEARED" HALVES, because
// nothing observable from outside can see a field that is not read — the same instrument
// `TestAFlightIsSingleUseAndBoundToItsBrowser` uses for the verifier.
func TestTheGitHubFlightCarriesTheReturnPathServerSide(t *testing.T) {
	const target = "/scope?id=scp_k&tab=sessions"
	srv := providerServer(t, &stubOAuth{})

	flight, start := startFlightWith(t, srv, OAuthStartPath, url.Values{FieldNext: {target}})
	if rec := srv.flights.open[flight.Value]; rec.next != target {
		t.Fatalf("the flight record holds next=%q, want %q", rec.next, target)
	}
	// Never in the provider redirect, never in a cookie.
	if loc := start.Header().Get("Location"); strings.Contains(loc, "scp_k") || strings.Contains(loc, "next") {
		t.Errorf("the provider redirect carries the return path: %q", loc)
	}
	for _, c := range start.Result().Cookies() {
		if strings.Contains(c.Value, "scp_k") || strings.Contains(c.Name, "next") {
			t.Errorf("a cookie carries the return path: %s=%s", c.Name, c.Value)
		}
	}

	rec := callback(srv, flight)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != target {
		t.Errorf("the callback answered %d → %q, want 303 → %q", rec.Code, rec.Header().Get("Location"), target)
	}
	if got := srv.flights.open[flight.Value]; !got.consumed || got.next != "" {
		t.Errorf("after the callback the record is consumed=%v next=%q; a consumed record must hold no return path",
			got.consumed, got.next)
	}

	// Validated at START: a hostile value posted to the start row is never stored.
	evilFlight, _ := startFlightWith(t, srv, OAuthStartPath, url.Values{FieldNext: {"//evil.invalid"}})
	if got := srv.flights.open[evilFlight.Value].next; got != "" {
		t.Errorf("the start row stored a hostile next %q", got)
	}
	// Read from the BODY only: a next in the start row's query is not stored.
	queryFlight, _ := startFlightWith(t, srv, OAuthStartPath+"?next=%2Farcs", url.Values{})
	if got := srv.flights.open[queryFlight.Value].next; got != "" {
		t.Errorf("the start row stored a next carried only in its QUERY string: %q", got)
	}
}

// TestTheCallbackRevalidatesTheReturnPathAtUse plants an invalid value on the record directly —
// the only way to reach the use-time check, since the start row never stores one — and requires
// the landing to be `/`.
func TestTheCallbackRevalidatesTheReturnPathAtUse(t *testing.T) {
	srv := providerServer(t, &stubOAuth{})
	id, outcome := srv.flights.start("198.51.100.20", "fixture-verifier-planted", "", "//evil.invalid", time.Minute)
	if outcome != flightOpened {
		t.Fatalf("PRECONDITION: %v", outcome)
	}
	rec := callback(srv, oauthFlightCookie(id, FlightTTL))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("the callback answered %d: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Location"); got != RootPath {
		t.Errorf("a planted next=//evil.invalid landed the callback on %q, want %q", got, RootPath)
	}
}

// TestAnEscapedSlashPairLandsOnThisOrigin pins the decode decision end to end: the landing is
// the escaped path itself, which a browser resolves against THIS origin.
func TestAnEscapedSlashPairLandsOnThisOrigin(t *testing.T) {
	l := newLive(t)
	rec := l.do(http.MethodPost, SignInPath, url.Values{FieldToken: {testCredential}, FieldNext: {"/%2F%2Fevil.invalid"}})
	got := rec.Header().Get("Location")
	if got != "/%2F%2Fevil.invalid" {
		t.Fatalf("landed on %q, want %q", got, "/%2F%2Fevil.invalid")
	}
	// Resolved the way a browser resolves it: against this origin, host unchanged.
	base, _ := url.Parse("https://" + testHost + SignInPath)
	ref, err := url.Parse(got)
	if err != nil {
		t.Fatal(err)
	}
	if host := base.ResolveReference(ref).Host; host != testHost {
		t.Errorf("the landing resolves to host %q, want %q", host, testHost)
	}
}
