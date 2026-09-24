package ui

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/ZacxDev/cairn/internal/control"
	"github.com/ZacxDev/cairn/internal/identity"
)

// stubOAuth is a provider that records what it was asked and answers what it was told to.
//
// 🔴 IT IS NOT A SECOND IMPLEMENTATION OF THE FLOW — it implements nothing but the two
// questions `OAuthAuthority` asks, and the authorize URL's own parameters are measured in
// `internal/identity` by `TestTheAuthorizeURLCarriesAPKCEChallengeAndNoState`. What it does
// is RECORD what reached it, so a test can compare the challenge against an INDEPENDENT
// computation rather than against the function that produced it — see the S256 assertion
// below, and the mutant that survived the other spelling.
type stubOAuth struct {
	// challenges records every challenge `AuthorizeURL` was handed, in order.
	challenges []string
	// codes and verifiers record every exchange, so a test can assert that the verifier
	// which reached the provider is the one the flight held.
	codes     []string
	verifiers []string
	// principal is what a successful exchange resolves to.
	principal control.Principal
	// err, when set, is what every exchange returns.
	err error
	// authorizeBase is where the browser is sent. A `.invalid` host: this repository is
	// public, so no fixture names a real identity provider.
	authorizeBase string
}

func (s *stubOAuth) AuthorizeURL(challenge string) string {
	s.challenges = append(s.challenges, challenge)
	base := s.authorizeBase
	if base == "" {
		base = "https://idp.notes.example.invalid/auth/v1/authorize"
	}
	return base + "?code_challenge=" + url.QueryEscape(challenge)
}

func (s *stubOAuth) Exchange(_ context.Context, code, verifier string) (control.Principal, error) {
	s.codes = append(s.codes, code)
	s.verifiers = append(s.verifiers, verifier)
	if s.err != nil {
		return control.Principal{}, s.err
	}
	p := s.principal
	if p.ID == "" {
		p = testIdentity().Principal
	}
	return p, nil
}

// providerServer builds a server whose provider is the returned stub.
func providerServer(t *testing.T, stub *stubOAuth) *Server {
	t.Helper()
	cfg := testConfig(t, refusingAuth{})
	cfg.OAuth = stub
	srv, err := New(cfg)
	if err != nil {
		t.Fatalf("the server did not build: %v", err)
	}
	return srv
}

// startFlight drives `POST /sign-in/github` with a correct Origin and returns the flight
// cookie the response set.
func startFlight(t *testing.T, srv *Server) *http.Cookie {
	t.Helper()
	r := httptest.NewRequest("POST", OAuthStartPath, nil)
	r.Header.Set("Origin", "https://"+r.Host)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, r)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("PRECONDITION FAILED: starting a flight answered %d, not 303, so every assertion about "+
			"the callback below would be about a flight nobody opened", rec.Code)
	}
	for _, c := range rec.Result().Cookies() {
		if c.Name == oauthFlightCookieName {
			return c
		}
	}
	t.Fatalf("PRECONDITION FAILED: the start route set no %s cookie, so nothing binds the flight to this "+
		"browser and the callback cannot be driven", oauthFlightCookieName)
	return nil
}

// TestTheGitHubButtonMintsAFlightAndRedirectsToTheProvider is the REGRESSION test for the
// start route: before this change `POST /sign-in/github` was not a row and answered the
// uniform 401.
//
// 🔴 IT ASSERTS THE THREE THINGS THAT MAKE THE FLOW A PKCE FLOW AND NOT A REDIRECT: the
// challenge in the URL is `base64url(sha256(verifier))` for the verifier the flight HELD,
// the flight cookie is `HttpOnly`/`Secure` and bounded, and the table grew by exactly one.
// The first is the one that cannot be checked from the outside, so it is checked by
// completing the flow and comparing what reached the provider.
func TestTheGitHubButtonMintsAFlightAndRedirectsToTheProvider(t *testing.T) {
	stub := &stubOAuth{}
	srv := providerServer(t, stub)

	// INSTRUMENT CONTROL: the table starts empty, so the count below is the request's doing.
	if n := srv.flights.openCount(); n != 0 {
		t.Fatalf("the flight table held %d record(s) before any request; the count below would measure "+
			"construction rather than the handler", n)
	}

	cookie := startFlight(t, srv)
	if n := srv.flights.openCount(); n != 1 {
		t.Errorf("the flight table holds %d record(s) after one start, want 1", n)
	}
	if len(stub.challenges) != 1 {
		t.Fatalf("the provider was asked for %d authorize URL(s), want 1", len(stub.challenges))
	}
	if !cookie.HttpOnly || !cookie.Secure {
		t.Errorf("the flight cookie is HttpOnly=%v Secure=%v; both must be true. A flight id readable by "+
			"script is a flight somebody else can complete.", cookie.HttpOnly, cookie.Secure)
	}
	if cookie.SameSite != http.SameSiteLaxMode {
		t.Errorf("the flight cookie is SameSite=%v, want Lax. The callback is a top-level GET navigation "+
			"from the PROVIDER's origin, which is cross-site: Strict would withhold the cookie from it and "+
			"every provider sign-in would be refused.", cookie.SameSite)
	}
	if cookie.MaxAge <= 0 || time.Duration(cookie.MaxAge)*time.Second > FlightTTL {
		t.Errorf("the flight cookie's MaxAge is %d s, want a positive value no greater than the flight TTL (%s)",
			cookie.MaxAge, FlightTTL)
	}
	if cookie.Value == "" {
		t.Error("the flight cookie carries no value, so nothing binds the flight to this browser")
	}

	// Complete the flow so the verifier the provider receives can be compared against the
	// challenge it was given. This is the only way to see the relationship from outside.
	callback := httptest.NewRequest("GET", OAuthCallbackPath+"?code=fixture-authorization-code", nil)
	callback.AddCookie(cookie)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, callback)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("the callback answered %d, want 303: %s", rec.Code, rec.Body.String())
	}
	if len(stub.verifiers) != 1 {
		t.Fatalf("the provider was asked to exchange %d time(s), want 1", len(stub.verifiers))
	}
	// 🔴 THE EXPECTATION IS COMPUTED HERE, FROM RFC 7636, AND *NOT* BY CALLING
	// `pkceChallenge` — AND THAT IS A MEASURED CORRECTION RATHER THAN A STYLE CHOICE. This
	// assertion first read `pkceChallenge(stub.verifiers[0]) == stub.challenges[0]`, which is
	// both sides of a comparison derived from the implementation under test: a mutant making
	// `pkceChallenge` return its argument VERBATIM — no hashing at all, which is PKCE
	// switched off — SURVIVED that spelling, because both sides moved together. An
	// independent `sha256` plus `base64.RawURLEncoding` here is what makes the comparison a
	// measurement.
	sum := sha256.Sum256([]byte(stub.verifiers[0]))
	want := base64.RawURLEncoding.EncodeToString(sum[:])
	if stub.challenges[0] != want {
		t.Errorf("the challenge sent to the provider was %q; RFC 7636 S256 of the verifier presented at the "+
			"exchange is %q. They must be the same value or the provider refuses the code at the END of the "+
			"flow — after the person has already authorised the app.", stub.challenges[0], want)
	}
	// AND THE CHALLENGE IS NOT THE VERIFIER. A flow that sent the verifier as the challenge
	// would complete successfully against a provider that only compares the two, while
	// putting the secret in a URL the browser, its history and the provider's logs all hold —
	// which is PKCE with its one property removed.
	if stub.challenges[0] == stub.verifiers[0] {
		t.Error("the challenge and the verifier are the SAME STRING, so the verifier travelled in the " +
			"authorize URL. The whole point of S256 is that what goes in the URL cannot be replayed as the " +
			"verifier.")
	}
	if stub.codes[0] != "fixture-authorization-code" {
		t.Errorf("the code the provider received was %q, want the one the callback carried", stub.codes[0])
	}
	// The session is the point of all of it.
	if session := sessionCookieFrom(rec.Result()); session == nil {
		t.Error("the callback minted no session cookie, so a verified provider sign-in left the browser " +
			"signed out — which looks exactly like a refused one")
	}
	t.Logf("flight: 1 opened, challenge %d chars, verifier %d chars, table back to %d after the callback",
		len(stub.challenges[0]), len(stub.verifiers[0]), srv.flights.openCount())
}

// TestAFlightIsSingleUseAndBoundToItsBrowser is the security core of the flow, and it is
// three claims over one fixture.
//
// 🔴 EACH ROW IS A DIFFERENT ATTACK AND THE THIRD IS THE ONE A `state` PARAMETER WOULD
// OTHERWISE BUY. (a) A REPLAY of a completed callback must fail, or an authorization code
// plus a cookie in a log is a second session. (b) A callback with NO flight cookie must fail,
// which is the shape an attacker who holds their own code and makes a victim open the URL
// lands in. (c) A callback carrying a flight id this server never issued must fail, which is
// what stops a sibling host planting one.
func TestAFlightIsSingleUseAndBoundToItsBrowser(t *testing.T) {
	stub := &stubOAuth{}
	srv := providerServer(t, stub)
	cookie := startFlight(t, srv)

	// (a) Complete it once...
	first := httptest.NewRequest("GET", OAuthCallbackPath+"?code=code-one", nil)
	first.AddCookie(cookie)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, first)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("PRECONDITION FAILED: the first callback answered %d, not 303, so the replay below is not a "+
			"replay of anything", rec.Code)
	}
	if n := srv.flights.openCount(); n != 0 {
		t.Errorf("the flight table holds %d record(s) after the callback consumed the flight, want 0. A record "+
			"left behind is a replayable sign-in.", n)
	}
	// ...then again with the same cookie and a fresh code.
	replay := httptest.NewRequest("GET", OAuthCallbackPath+"?code=code-two", nil)
	replay.AddCookie(cookie)
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, replay)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("a REPLAYED flight id answered %d, want 400. `flights.take` deletes the record on read, so "+
			"the second presentation must find nothing.", rec.Code)
	}
	if sessionCookieFrom(rec.Result()) != nil {
		t.Error("the replayed callback minted a SESSION, which is a second live session from one sign-in")
	}
	if exchanges := len(stub.codes); exchanges != 1 {
		t.Errorf("the provider was asked to exchange %d time(s); the replay must be refused BEFORE the "+
			"exchange, because a refused exchange still costs a network round trip anybody can drive", exchanges)
	}

	// (b) No cookie at all.
	bare := httptest.NewRequest("GET", OAuthCallbackPath+"?code=code-three", nil)
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, bare)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("a callback with NO flight cookie answered %d, want 400. This is the shape an attacker who "+
			"holds their own code and makes a victim's browser open the callback lands in.", rec.Code)
	}

	// (c) A flight id this server never issued.
	planted := httptest.NewRequest("GET", OAuthCallbackPath+"?code=code-four", nil)
	planted.AddCookie(&http.Cookie{Name: oauthFlightCookieName, Value: "a-flight-id-this-server-never-minted"})
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, planted)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("a PLANTED flight id answered %d, want 400", rec.Code)
	}
	if exchanges := len(stub.codes); exchanges != 1 {
		t.Errorf("the provider was asked to exchange %d time(s) in total, want 1 — only the one legitimate "+
			"callback may reach it", exchanges)
	}
}

// TestAnExpiredFlightIsRefused pins the expiry, over an injected clock.
//
// ⚠ IT IS AN INVARIANT GUARD, NOT A REGRESSION TEST: no defect made a flight immortal. What
// it stands against is the boundary — `Live`-style comparisons are wrong on exactly one
// instant, and `identity.Session.Live`'s own comment records that an injected clock lands on
// that instant whenever a test pins it there. So this drives the instant AT the expiry and
// one nanosecond before it.
func TestAnExpiredFlightIsRefused(t *testing.T) {
	base := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	now := base
	table := newFlights(func() time.Time { return now })

	id, ok := table.start("fixture-verifier-value", time.Minute)
	if !ok {
		t.Fatal("PRECONDITION FAILED: the flight table refused to open a flight at all")
	}

	// One nanosecond before expiry: live.
	now = base.Add(time.Minute - time.Nanosecond)
	if _, held := table.take(id); !held {
		t.Error("a flight one nanosecond BEFORE its expiry was refused; the boundary is closed at the expiry " +
			"instant, not before it")
	}

	// And AT the expiry instant: dead. A second flight, because the first was consumed.
	now = base
	id2, ok := table.start("fixture-verifier-value-two", time.Minute)
	if !ok {
		t.Fatal("PRECONDITION FAILED: the flight table refused a second flight")
	}
	now = base.Add(time.Minute)
	if _, held := table.take(id2); held {
		t.Error("a flight AT its expiry instant was accepted; `!now.Before(expires)` and `now.After(expires)` " +
			"differ on exactly this instant and an injected clock lands on it")
	}
}

// TestTheFlightTableIsBounded pins the memory bound on an UNAUTHENTICATED endpoint.
//
// 🔴 IT IS A REGRESSION TEST FOR A DEFECT THIS CHANGE WOULD HAVE SHIPPED WITHOUT THE CAP:
// `POST /sign-in/github` is reachable by anybody who can open a socket, and every request
// writes a record that lives five minutes. Without `maxOpenFlights` the route is a memory
// exhaustion endpoint. The cap is measured here rather than asserted, and the POSITIVE
// CONTROL is that the table grew to the cap at all — a table that refused from the start
// would satisfy the refusal below while measuring nothing.
func TestTheFlightTableIsBounded(t *testing.T) {
	fixed := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	table := newFlights(func() time.Time { return fixed })
	opened := 0
	for i := 0; i < maxOpenFlights+8; i++ {
		if _, ok := table.start("fixture-verifier", time.Minute); ok {
			opened++
		}
	}
	if opened != maxOpenFlights {
		t.Errorf("%d flight(s) were opened against a cap of %d. A cap that admits more than it declares is "+
			"not a bound, and one that admits fewer refuses sign-ins nobody asked it to.", opened, maxOpenFlights)
	}
	if n := table.openCount(); n != maxOpenFlights {
		t.Errorf("the table holds %d record(s), want %d", n, maxOpenFlights)
	}
	t.Logf("flight cap: %d opened, %d refused, table at %d", opened, maxOpenFlights+8-opened, table.openCount())
}

// TestAFailedExchangeSaysNothingAboutTheCredentialTable pins the uniform refusal across the
// three reasons an exchange can fail.
//
// 🔴 THE THIRD ONE IS THE ORACLE THIS GUARDS. "the provider refused that code", "that token
// does not verify" and "no user here matches that subject" are three different facts, and
// only the third is about the CONTROL PLANE — it answers "does this person have an account
// here", which is exactly what `signInRefused`'s comment refuses to answer. All three must
// render the same bytes with the same status.
func TestAFailedExchangeSaysNothingAboutTheCredentialTable(t *testing.T) {
	type outcome struct {
		code int
		body string
	}
	seen := map[outcome][]string{}
	for _, tc := range []struct {
		name string
		err  error
	}{
		{"the provider refused the code", errors.New("identity: the token endpoint answered 400")},
		{"the token does not verify", errors.New("supabase-jwt: no credential: signature does not verify")},
		{"no user here matches the subject",
			errors.New("supabase-jwt: no credential: the token verifies and names a subject this control plane holds no user for")},
	} {
		stub := &stubOAuth{err: tc.err}
		srv := providerServer(t, stub)
		cookie := startFlight(t, srv)
		r := httptest.NewRequest("GET", OAuthCallbackPath+"?code=fixture-code", nil)
		r.AddCookie(cookie)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, r)
		key := outcome{code: rec.Code, body: rec.Body.String()}
		seen[key] = append(seen[key], tc.name)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s answered %d, want 401", tc.name, rec.Code)
		}
		if !strings.Contains(rec.Body.String(), signInRefused) {
			t.Errorf("%s did not render the uniform refusal %q", tc.name, signInRefused)
		}
		if sessionCookieFrom(rec.Result()) != nil {
			t.Errorf("%s minted a SESSION", tc.name)
		}
	}
	if len(seen) != 1 {
		t.Errorf("the three failure reasons produced %d distinguishable responses, want 1. A refusal that "+
			"discriminates is an enumeration API: %v", len(seen), seen)
	}
	t.Logf("uniform refusal: 3 reasons, %d distinguishable response(s)", len(seen))
}

// TestTheProviderErrorIsNotReflectedIntoThePage pins that a query parameter the PROVIDER
// controls does not reach the rendered page.
//
// 🔴 IT IS A GUARD ON A REFLECTION, NOT ON THE ESCAPING. gomponents would escape the text —
// that is what `TestHostileEntryTextIsEscaped` measures — so the hazard here is not breakout
// but CONTENT: `error_description` is a string chosen by whoever can send the browser to this
// URL, and a page that renders it is a page an attacker writes a sentence on. The fixture
// value is a plausible phishing sentence rather than an XSS payload for exactly that reason.
func TestTheProviderErrorIsNotReflectedIntoThePage(t *testing.T) {
	srv := providerServer(t, &stubOAuth{})
	cookie := startFlight(t, srv)
	const hostile = "Your session expired. Send your credential token to help@notes.example.invalid to restore it."
	r := httptest.NewRequest("GET",
		OAuthCallbackPath+"?error=access_denied&error_description="+url.QueryEscape(hostile), nil)
	r.AddCookie(cookie)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, r)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("a declined provider sign-in answered %d, want 400", rec.Code)
	}
	body := rec.Body.String()
	if strings.Contains(body, "notes.example.invalid") || strings.Contains(body, "Your session expired") {
		t.Error("the provider's `error_description` reached the rendered page. It is a sentence chosen by " +
			"whoever sent the browser here, so rendering it lets somebody else write on this surface.")
	}
	if !strings.Contains(body, oauthIncomplete) {
		t.Errorf("the page did not carry %q; a declined sign-in is an incomplete one, and that is what it says",
			oauthIncomplete)
	}
}

// TestTheGitHubRowsAnswerAnHonestRefusalWhenTheProviderIsNotConfigured is what makes the two
// OAuth rows measured rather than inert on a deployment that has no provider.
//
// 🔴 `Config.Sharing`'s COMMENT REFUSES A NIL-MEANS-DISABLED FIELD BECAUSE IT PUTS AN
// UNMEASURED ROW IN THE LEDGER. `Config.OAuth` is nil-able anyway, and this is the measurement
// that pays for it: both rows answer 501 with a sentence naming the configuration, and the
// sign-in page renders no button. The status is NOT 404 deliberately — the path IS a route,
// and `DeclaredRoutes()` publishes it.
func TestTheGitHubRowsAnswerAnHonestRefusalWhenTheProviderIsNotConfigured(t *testing.T) {
	cfg := testConfig(t, refusingAuth{})
	cfg.OAuth = nil
	srv, err := New(cfg)
	if err != nil {
		t.Fatalf("a server with no provider must still build (the deployment that exists has none): %v", err)
	}

	start := httptest.NewRequest("POST", OAuthStartPath, nil)
	start.Header.Set("Origin", "https://"+start.Host)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, start)
	if rec.Code != http.StatusNotImplemented {
		t.Errorf("the start row answered %d with no provider configured, want 501", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), oauthUnavailable) {
		t.Errorf("the start row's refusal did not name the configuration (%q)", oauthUnavailable)
	}

	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest("GET", OAuthCallbackPath, nil))
	if rec.Code != http.StatusNotImplemented {
		t.Errorf("the callback row answered %d with no provider configured, want 501", rec.Code)
	}

	// AND NO BUTTON. A control that is present and cannot work teaches a user that sign-in
	// is unreliable — the ruling [Page] makes about the sign-out button it withholds.
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest("GET", SignInPath, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("the sign-in page answered %d, so the button assertion below is about a refusal", rec.Code)
	}
	if strings.Contains(rec.Body.String(), OAuthStartPath) {
		t.Errorf("the sign-in page offers a form posting to %s on a deployment with no provider, so the "+
			"button leads to a 501", OAuthStartPath)
	}
	// POSITIVE CONTROL: the same assertion CAN see the button, so its absence above is a
	// measurement rather than a search for a string this page never carries.
	withProvider := newTestServer(t, refusingAuth{})
	rec = httptest.NewRecorder()
	withProvider.ServeHTTP(rec, httptest.NewRequest("GET", SignInPath, nil))
	if !strings.Contains(rec.Body.String(), OAuthStartPath) {
		t.Errorf("POSITIVE CONTROL FAILED: the sign-in page of a server WITH a provider does not offer a form "+
			"posting to %s either, so the absence asserted above means nothing", OAuthStartPath)
	}
	// And the credential form is on BOTH, which is the decision `SignInPage` records: the
	// token door is the one that works when the provider is down.
	if !strings.Contains(rec.Body.String(), `name="`+FieldToken+`"`) {
		t.Error("the sign-in page of a server WITH a provider dropped the credential form. It is the only " +
			"door that works when the identity provider is unreachable, and the only one that verified this " +
			"deployment.")
	}
}

// TestTheStartRowIsRefusedCrossSite pins that the start route inherits gate (2).
//
// ⚠ AN INVARIANT GUARD, AND IT IS HERE BECAUSE THE GATE IS DERIVED FROM THE METHOD AND THE
// METHOD IS A CHOICE THIS CHANGE MADE. `stateChanging` calls GET safe, so a start route
// spelled as a GET would carry no cross-site gate at all — and the failure would be silent:
// every `<img src>` and every link prefetcher would mint a flight. This is the test that
// notices if somebody "simplifies" the button into a link.
func TestTheStartRowIsRefusedCrossSite(t *testing.T) {
	srv := providerServer(t, &stubOAuth{})
	r := httptest.NewRequest("POST", OAuthStartPath, nil)
	r.Header.Set("Origin", "https://evil.invalid")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, r)
	if rec.Code != http.StatusForbidden {
		t.Errorf("a cross-site POST to %s answered %d, want 403", OAuthStartPath, rec.Code)
	}
	if body := rec.Body.String(); body != crossSiteRefusal {
		t.Errorf("the refusal body is %q, want %q — a kill by the WRONG guard would otherwise read as a kill",
			body, crossSiteRefusal)
	}
	if n := srv.flights.openCount(); n != 0 {
		t.Errorf("a cross-site POST opened %d flight(s); gate (2) runs before the handler and must cost "+
			"nothing", n)
	}
}

// TestTheStylesheetIsServedAsItsOwnRoute is the REGRESSION test for the CSP change: with
// `style-src 'self'` an inline `<style>` does not apply, so a page that still inlined it would
// render unstyled with nothing going red.
//
// 🔴 IT PINS THE RELATIONSHIP AND NOT EITHER SIDE. Two assertions on their own would both pass
// for a broken pair: a route serving CSS nobody links, or a page linking a path nobody serves.
// So it asserts the page's `<link href>` and then FETCHES that exact href.
func TestTheStylesheetIsServedAsItsOwnRoute(t *testing.T) {
	srv := newTestServer(t, staticAuth{testIdentity()})

	for _, page := range []string{RootPath, SignInPath, SharePath} {
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, httptest.NewRequest("GET", page, nil))
		body := rec.Body.String()
		if strings.Contains(body, "<style") {
			t.Errorf("%s still carries an inline <style> element. `style-src 'self'` forbids one in a "+
				"conforming browser, so it would not be refused — it would simply not apply, and the page "+
				"would render unstyled with nothing going red.", page)
		}
		if !strings.Contains(body, `href="`+StylesheetPath+`"`) {
			t.Errorf("%s does not link %s, so it has no styles at all", page, StylesheetPath)
		}
	}

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest("GET", StylesheetPath, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("%s answered %d, want 200", StylesheetPath, rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != "text/css; charset=utf-8" {
		t.Errorf("the stylesheet's Content-Type is %q; a browser that is told `text/html` will not apply it", got)
	}
	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("the stylesheet's X-Content-Type-Options is %q, want nosniff", got)
	}
	if body := rec.Body.String(); body != stylesheet {
		t.Errorf("the served stylesheet is %d bytes and the constant is %d; they must be the same bytes",
			len(body), len(stylesheet))
	}
	// 🔴 AND IT IS REACHABLE WITHOUT A CREDENTIAL, because the SIGN-IN page links it. A
	// stylesheet behind the chain renders the way in as unstyled text.
	anon := newTestServer(t, refusingAuth{})
	rec = httptest.NewRecorder()
	anon.ServeHTTP(rec, httptest.NewRequest("GET", StylesheetPath, nil))
	if rec.Code != http.StatusOK {
		t.Errorf("an unauthenticated caller got %d for %s; the sign-in page links it and that page answers "+
			"anybody", rec.Code, StylesheetPath)
	}
}

// TestTheCredentialFormSURVIVESTheProviderButton pins the token door as a CONTRACT rather
// than as a leftover, on both shapes of deployment.
//
// 🔴 IT IS PINNED BY ITS SELECTOR BECAUSE SOMETHING OUTSIDE THIS REPOSITORY DRIVES IT. A
// browser evaluation harness signs in through this exact field, and OAuth cannot serve that
// purpose at any effort: the provider flow is cross-origin to a third party with MFA, so no
// automated harness can complete it. The alternative — a test-only authentication bypass — is
// what `identity.SessionCookie`'s comment argues against in as many words: "an env var to turn
// it off for local development is a variable that ends up set in production".
//
// 🔴 SO THE ASSERTIONS ARE THE SELECTOR, THE COUNT AND THE ACTION, NOT "the page mentions a
// token". A guard on the WORD is walkable by a reword; what the harness depends on is that
// there is exactly ONE field named `token`, that it is a password input, and that the form
// carrying it posts to `SignInPath`. A second field with that name would make
// `PostFormValue(FieldToken)` return whichever the browser serialised first.
//
// ⚠ IT IS AN INVARIANT GUARD, LABELLED AS ONE: no defect removed the form. What it stands
// against is a later "simplification" that replaces two doors with one, and the cost of that
// would land on a harness this package cannot see.
func TestTheCredentialFormSURVIVESTheProviderButton(t *testing.T) {
	withProvider := newTestServer(t, refusingAuth{})
	noProviderCfg := testConfig(t, refusingAuth{})
	noProviderCfg.OAuth = nil
	withoutProvider, err := New(noProviderCfg)
	if err != nil {
		t.Fatalf("a server with no provider must still build: %v", err)
	}

	for _, tc := range []struct {
		name string
		srv  *Server
	}{
		{"with a provider configured", withProvider},
		{"with none", withoutProvider},
	} {
		rec := httptest.NewRecorder()
		tc.srv.ServeHTTP(rec, httptest.NewRequest("GET", SignInPath, nil))
		if rec.Code != http.StatusOK {
			t.Errorf("%s: the sign-in page answered %d", tc.name, rec.Code)
			continue
		}
		body := rec.Body.String()
		selector := `name="` + FieldToken + `"`
		if n := strings.Count(body, selector); n != 1 {
			t.Errorf("%s: the page carries %d field(s) matching %s, want exactly 1. A harness outside this "+
				"repository signs in through this selector, and two fields with one name make "+
				"PostFormValue return whichever the browser serialised first.", tc.name, n, selector)
		}
		if !strings.Contains(body, `type="password"`) {
			t.Errorf("%s: the credential field is not a password input, so the value is shoulder-readable "+
				"and a browser will offer to remember it as ordinary form text", tc.name)
		}
		if !strings.Contains(body, `action="`+SignInPath+`"`) {
			t.Errorf("%s: no form posts to %s, so the credential has nowhere to go", tc.name, SignInPath)
		}
	}

	// AND IT STILL WORKS, END TO END, ON A SERVER THAT HAS A PROVIDER. A rendered field is not
	// a working door — `TestEveryContentRouteConsultsTheAuthority`'s whole lesson is that a
	// page can render an answer nobody asked for — so this drives the exchange.
	r := httptest.NewRequest("POST", SignInPath,
		strings.NewReader(url.Values{FieldToken: {testCredential}}.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Origin", "https://"+r.Host)
	rec := httptest.NewRecorder()
	withProvider.ServeHTTP(rec, r)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("a valid credential posted to a server WITH a provider answered %d, want 303: %s",
			rec.Code, rec.Body.String())
	}
	if sessionCookieFrom(rec.Result()) == nil {
		t.Error("the credential sign-in minted no session on a server with a provider configured, so adding " +
			"the provider degraded the door it was meant to sit beside")
	}
}

// sessionCookieFrom returns the session cookie a response set, or nil.
func sessionCookieFrom(resp *http.Response) *http.Cookie {
	for _, c := range resp.Cookies() {
		if c.Name == identity.SessionCookieName && c.Value != "" {
			return c
		}
	}
	return nil
}
