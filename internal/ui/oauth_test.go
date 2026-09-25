package ui

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/ZacxDev/cairn/internal/control"
	"github.com/ZacxDev/cairn/internal/identity"
	"github.com/ZacxDev/cairn/internal/netid"
	"github.com/ZacxDev/cairn/internal/testcookie"
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
	// 🔴 THE LIFETIME IS PINNED AGAINST A LITERAL BOUND AND NOT AGAINST `FlightTTL`, BECAUSE
	// COMPARING IT TO THE CONSTANT IS A TEST THAT `a == a`. Measured: with the comparison
	// written as `MaxAge <= FlightTTL`, setting `FlightTTL = 720 * time.Hour` — a thirty-day
	// window in which a planted flight stays completable — passed this whole suite. The
	// relationship is still asserted below; what the literals add is a claim about the
	// MAGNITUDE, which is the thing a reader of `FlightTTL`'s own comment ("short because a
	// flight is a live credential-in-waiting") is being promised.
	const (
		floor   = 30 * time.Second
		ceiling = 15 * time.Minute
	)
	if FlightTTL < floor || FlightTTL > ceiling {
		t.Errorf("FlightTTL is %s, outside [%s, %s]. It is a window in which a planted flight stays "+
			"completable, and its own comment promises it is 'longer than any provider round trip a person "+
			"will sit through and far shorter than the session lifetime'. Widening it is a decision taken "+
			"HERE, not a constant edit.", FlightTTL, floor, ceiling)
	}
	if got, want := time.Duration(cookie.MaxAge)*time.Second, FlightTTL; got != want {
		t.Errorf("the flight cookie's MaxAge is %s and the flight TTL is %s. A cookie that outlived its "+
			"record would be a browser holding a key to a lock that no longer exists; one that died first "+
			"would refuse a sign-in the server was still willing to complete.", got, want)
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

// TestTheFlightCookieCarriesItsPrefixAndFlagsOnTheWire guards the `__Host-` prefix, which
// nothing guarded.
//
// 🔴 RENAMING `oauthFlightCookieName` TO `"cairn-oauth"` PASSED THE ENTIRE SUITE, AND THAT
// PREFIX IS THE LOAD-BEARING HALF OF THE NO-`state` DESIGN. Without it, any sibling host under
// the registrable domain can set a cookie this origin will send — so a sibling could PLANT a
// flight id of its own choosing, and a flight id it chose is one it can complete. That is
// precisely the subdomain attack a `state` parameter would otherwise close, and this flow
// deliberately does not carry one. The prefix is therefore not a naming convention here; it is
// the guard, and it was asserted nowhere.
//
// 🔴 IT ASSERTS THE RENDERED `Set-Cookie`, NOT THE STRUCT, WHICH IS THE SAME SHAPE
// `identity.TestTheSessionCookieCarriesItsFlagsOnTheWire` TAKES AND FOR THE SAME REASON:
// `http.SetCookie` SILENTLY DROPS a cookie it considers invalid, so a struct-level assertion
// can pass for a cookie no browser will ever receive. The empty-header check below is what
// makes every assertion after it non-vacuous.
//
// ⚠ THE `Domain=` ABSENCE IS ASSERTED, NOT ASSUMED. A `__Host-` cookie carrying one is refused
// outright by a conforming browser — so a well-meaning edit adding `Domain` would not weaken
// the guard, it would delete the cookie, and every provider sign-in would fail with nothing
// naming why.
func TestTheFlightCookieCarriesItsPrefixAndFlagsOnTheWire(t *testing.T) {
	for _, tc := range []struct {
		name   string
		cookie *http.Cookie
	}{
		{"the flight cookie", oauthFlightCookie("a-flight-id", FlightTTL)},
		{"the cleared flight cookie", clearedOAuthFlightCookie()},
	} {
		rec := httptest.NewRecorder()
		http.SetCookie(rec, tc.cookie)
		header := rec.Header().Get("Set-Cookie")
		if header == "" {
			t.Errorf("%s: NO Set-Cookie HEADER WAS RENDERED. `http.SetCookie` silently drops a cookie it "+
				"considers invalid — an unacceptable name is the usual cause — so every assertion below "+
				"would be over an empty string.", tc.name)
			continue
		}
		if !strings.HasPrefix(header, "__Host-") {
			t.Errorf("%s is not a `__Host-` cookie: %q. That prefix is what stops a sibling host under the "+
				"registrable domain planting a flight id — the subdomain attack a `state` parameter would "+
				"otherwise close, and this flow deliberately carries no `state`.", tc.name, header)
		}
		if !strings.HasPrefix(header, oauthFlightCookieName+"=") {
			t.Errorf("%s is not named %s: %q", tc.name, oauthFlightCookieName, header)
		}
		for _, want := range []string{"HttpOnly", "Secure", "SameSite=Lax"} {
			if !strings.Contains(header, want) {
				t.Errorf("%s does not carry %s: %q", tc.name, want, header)
			}
		}
		// 🔴 `Path` IS MATCHED EXACTLY, BECAUSE `strings.Contains(header, "Path=/")` IS
		// SATISFIED BY `Path=/sign-in` — AND THAT WALKED THE GUARD THIS TEST WAS WRITTEN TO
		// BE. Measured: mutating `oauthFlightCookie`'s `Path: "/"` to `"/sign-in"` survived
		// the whole suite. A conforming browser refuses a `__Host-` cookie whose path is not
		// exactly `/`, so the cookie is DROPPED and every provider sign-in fails with nothing
		// naming why — the identical consequence this test already asserts correctly for
		// `Domain`. `testcookie.HasExactAttr` is the ONE predicate; three other sites had the
		// same defect and now share it.
		if !testcookie.HasExactAttr(header, "Path=/") {
			t.Errorf("%s does not carry an exact `Path=/`: %q. A `__Host-` cookie with any other path is "+
				"refused outright by a conforming browser, so this would not weaken the guard — it would "+
				"delete the cookie.", tc.name, header)
		}
		if strings.Contains(header, "Domain=") {
			t.Errorf("%s carries a Domain attribute: %q. A `__Host-` cookie with one is refused outright by "+
				"a conforming browser, so this would not weaken the guard — it would delete the cookie, and "+
				"every provider sign-in would fail with nothing naming why.", tc.name, header)
		}
	}

	// 🔴 AND IT IS NOT THE SESSION COOKIE'S NAME. Reusing that name would mean a signed-in
	// person who clicks the GitHub button and abandons the flow is signed out by the click —
	// and the cleared flight cookie at the callback would clear their session.
	if oauthFlightCookieName == identity.SessionCookieName {
		t.Errorf("the flight cookie and the session cookie share the name %q, so starting a provider "+
			"sign-in overwrites an existing session and abandoning one destroys it", oauthFlightCookieName)
	}
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
	// 🔴 THE TABLE DOES *NOT* EMPTY, AND THAT IS THE MECHANISM RATHER THAN A LEAK. This
	// assertion used to require `openCount() == 0`, which encoded the OLD implementation:
	// `take` deleted the record, which freed the caller's per-client slot at the start of the
	// token exchange and made the cap bound concurrency instead of rate — measured, one client
	// drove 200 exchanges. A consumed record now keeps its slot until it EXPIRES. So the
	// property to assert is that the record is SPENT, not that it is gone; a second `take`
	// below is what measures it, and `TestOneClientCannotAmplifyRequestsAtTheProvider` is what
	// measures why it stays.
	if n := srv.flights.openCount(); n != 1 {
		t.Errorf("the flight table holds %d record(s) after the callback, want 1 — a SPENT record that keeps "+
			"its slot until expiry, which is what bounds the rate of outbound exchanges", n)
	}
	if _, held := srv.flights.take(cookie.Value); held {
		t.Error("a consumed flight was takeable a SECOND time straight from the table, so single use rests on " +
			"nothing. This is the assertion that replaced an `openCount() == 0` check, which measured the old " +
			"delete-on-read implementation rather than the property.")
	}
	// 🔴 AND THE VERIFIER IS GONE FROM THE SPENT RECORD. `take`'s comment claims it is cleared,
	// and deleting `rec.verifier = ""` survived the whole suite — so the claim had no guard. It
	// matters more than it looks: a spent record is now KEPT until expiry rather than deleted,
	// so an unguarded claim here means a live PKCE secret sitting in memory for the rest of the
	// TTL. This reaches into the table's internals because nothing observable from outside can
	// see a field that is no longer read.
	srv.flights.mu.Lock()
	spent, present := srv.flights.open[cookie.Value]
	srv.flights.mu.Unlock()
	if !present {
		t.Error("the spent record is GONE from the table, so it did not keep its slot — which is the " +
			"mechanism that bounds the rate of outbound exchanges")
	} else if spent.verifier != "" {
		t.Errorf("the spent record still holds its PKCE verifier (%d bytes). It has served its only purpose, "+
			"and a record that keeps it leaves a live secret in memory for the rest of the TTL.",
			len(spent.verifier))
	} else if !spent.consumed {
		t.Error("the record is not marked consumed, so the refusal above happened for some other reason")
	}
	// ...then again with the same cookie and a fresh code.
	replay := httptest.NewRequest("GET", OAuthCallbackPath+"?code=code-two", nil)
	replay.AddCookie(cookie)
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, replay)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("a REPLAYED flight id answered %d, want 400. `flights.take` MARKS the record consumed, so "+
			"the second presentation must find a spent one and refuse it.", rec.Code)
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

	id, outcome := table.start("198.51.100.7", "fixture-verifier-value", time.Minute)
	if outcome != flightOpened {
		t.Fatalf("PRECONDITION FAILED: the flight table refused to open a flight at all (%s)", outcome)
	}

	// One nanosecond before expiry: live.
	now = base.Add(time.Minute - time.Nanosecond)
	if _, held := table.take(id); !held {
		t.Error("a flight one nanosecond BEFORE its expiry was refused; the boundary is closed at the expiry " +
			"instant, not before it")
	}

	// And AT the expiry instant: dead. A second flight, because the first was consumed.
	now = base
	id2, outcome := table.start("198.51.100.7", "fixture-verifier-value-two", time.Minute)
	if outcome != flightOpened {
		t.Fatalf("PRECONDITION FAILED: the flight table refused a second flight (%s)", outcome)
	}
	now = base.Add(time.Minute)
	if _, held := table.take(id2); held {
		t.Error("a flight AT its expiry instant was accepted; `!now.Before(expires)` and `now.After(expires)` " +
			"differ on exactly this instant and an injected clock lands on it")
	}
}

// TestTheFlightTableIsBoundedGloballyAndPerClient pins BOTH bounds on an UNAUTHENTICATED
// endpoint, and pins that each one refuses for its OWN reason.
//
// 🔴 IT IS A REGRESSION TEST FOR A DEFECT THIS CHANGE WOULD HAVE SHIPPED WITHOUT THE CAPS:
// `POST /sign-in/github` is reachable by anybody who can open a socket, and every request
// writes a record that lives five minutes. Without `maxOpenFlights` the route is a memory
// exhaustion endpoint; without `maxFlightsPerClient` ONE caller reaches the global bound
// alone and every other person's button refuses until the oldest flights expire.
//
// 🔴 THE TWO ARMS USE DIFFERENT CLIENT KEYS AND THAT IS THE WHOLE DESIGN OF THE FIXTURE, NOT
// A DETAIL. An earlier version of this test opened 1032 flights under ONE fixture client and
// asserted the global cap. Under a per-client bound that loop never reaches the global one —
// it stops at 8 — so the global assertion would have been satisfied by the WRONG bound, with
// the number that matters never exercised. Distinct keys are what make the global arm reach
// its own boundary; `flightRefusal` is what makes each arm prove WHICH bound fired, so
// neither can be credited with the other's kill.
func TestTheFlightTableIsBoundedGloballyAndPerClient(t *testing.T) {
	fixed := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)

	// 🔴 THE MAGNITUDES ARE PINNED AGAINST LITERALS FIRST, BECAUSE EVERY ASSERTION BELOW IS
	// DERIVED FROM THE CONSTANTS AND THEREFORE CANNOT SEE THEM MOVE. Measured: WIDENING
	// `maxOpenFlights` passed this whole suite, because the loop bound is written
	// `maxOpenFlights+8` — only a NARROWED cap failed. A bound that can be raised without a
	// test going red is not a bound, it is a variable. The same self-referential shape the
	// S256 assertion had.
	if maxFlightsPerClient < 2 || maxFlightsPerClient > 64 {
		t.Errorf("maxFlightsPerClient is %d, outside [2, 64]. Below 2 an ordinary retry is refused; above 64 "+
			"one caller can drive the identity provider harder than any person would. Changing it is a "+
			"decision taken HERE.", maxFlightsPerClient)
	}
	if maxOpenFlights < 64 || maxOpenFlights > 8192 {
		t.Errorf("maxOpenFlights is %d, outside [64, 8192]. Because a spent record holds its slot until "+
			"expiry, this bounds BOTH memory and the total rate of sign-in starts across all clients per "+
			"FlightTTL; raising it is a decision taken HERE.", maxOpenFlights)
	}
	// And the relationship: the global bound must admit many distinct clients, or the
	// per-client bound is the only one that ever fires and the global one is decoration.
	if maxOpenFlights < 8*maxFlightsPerClient {
		t.Errorf("maxOpenFlights (%d) admits fewer than 8 clients at their own cap (%d each), so a handful of "+
			"callers reach the GLOBAL bound and the per-client bound stops protecting anybody",
			maxOpenFlights, maxFlightsPerClient)
	}

	// PER-CLIENT: one key, many attempts.
	perClient := newFlights(func() time.Time { return fixed })
	opened, refusals := 0, map[flightRefusal]int{}
	for i := 0; i < maxFlightsPerClient+5; i++ {
		_, outcome := perClient.start("198.51.100.7", "fixture-verifier", time.Minute)
		if outcome == flightOpened {
			opened++
			continue
		}
		refusals[outcome]++
	}
	if opened != maxFlightsPerClient {
		t.Errorf("one client opened %d flight(s) against a per-client cap of %d", opened, maxFlightsPerClient)
	}
	if refusals[flightRefusedPerClient] != 5 {
		t.Errorf("the per-client refusals were %v; all 5 over-cap attempts must be refused BY THE PER-CLIENT "+
			"bound. A refusal by the GLOBAL bound here would mean this arm measured the wrong number.", refusals)
	}

	// GLOBAL: distinct keys, so the per-client bound is never the thing that refuses. Each
	// key opens one flight, so reaching `maxOpenFlights` takes exactly that many keys.
	global := newFlights(func() time.Time { return fixed })
	openedGlobal, globalRefusals := 0, map[flightRefusal]int{}
	for i := 0; i < maxOpenFlights+8; i++ {
		_, outcome := global.start(fmt.Sprintf("198.51.100.%d", i), "fixture-verifier", time.Minute)
		if outcome == flightOpened {
			openedGlobal++
			continue
		}
		globalRefusals[outcome]++
	}
	if openedGlobal != maxOpenFlights {
		t.Errorf("%d flight(s) were opened against a global cap of %d. A cap that admits more than it declares "+
			"is not a bound, and one that admits fewer refuses sign-ins nobody asked it to.",
			openedGlobal, maxOpenFlights)
	}
	if globalRefusals[flightRefusedGlobal] != 8 {
		t.Errorf("the global refusals were %v; all 8 over-cap attempts must be refused BY THE GLOBAL bound",
			globalRefusals)
	}
	if n := global.openCount(); n != maxOpenFlights {
		t.Errorf("the table holds %d record(s), want %d", n, maxOpenFlights)
	}

	// 🔴 AND THE PROPERTY THE PER-CLIENT BOUND EXISTS FOR: one caller AT ITS OWN CAP must not
	// stop anybody else signing in. This is the assertion the global bound alone cannot make,
	// and it is the whole reason the second number exists.
	shared := newFlights(func() time.Time { return fixed })
	for i := 0; i < maxFlightsPerClient+3; i++ {
		shared.start("198.51.100.7", "fixture-verifier", time.Minute)
	}
	if _, outcome := shared.start("203.0.113.9", "fixture-verifier", time.Minute); outcome != flightOpened {
		t.Errorf("a SECOND client was refused (%s) while the first sat at its own cap. One caller spending "+
			"everybody else's share is the denial of service the per-client bound exists to prevent.", outcome)
	}

	t.Logf("flight caps: per-client %d opened / %d refused; global %d opened / %d refused; a second client "+
		"still opened one while the first was at its cap",
		opened, refusals[flightRefusedPerClient], openedGlobal, globalRefusals[flightRefusedGlobal])
}

// TestOneClientCannotAmplifyRequestsAtTheProvider is the REGRESSION test for a defect this
// branch shipped into review: the per-client cap bounded CONCURRENCY and not RATE.
//
// 🔴 THE DEFECT, AS MEASURED BY PROBE BEFORE THE FIX: `flights.take` DELETED the record, so
// the caller's slot freed at the START of the token exchange. One client key drove **200
// completed exchanges, reached the provider 200 times, and left 0 open flights** — a public,
// unauthenticated endpoint amplifying one inbound request into one outbound request against
// the identity provider, each holding a socket for up to `supabaseOAuthTimeout`. The cap's own
// comment claimed it bounded the rate. It did not.
//
// 🔴 WHAT THE FIX IS, AND WHY IT IS ONE WORD: `take` now MARKS the record consumed instead of
// deleting it, so a spent flight keeps occupying its client's share until it EXPIRES. Single
// use is preserved by refusing an already-consumed record. The bound becomes
// `maxFlightsPerClient` starts — and therefore outbound exchanges — per client per
// `FlightTTL`.
//
// ⚠ THE LOOP DRIVES FAR MORE ATTEMPTS THAN THE CAP, AND COUNTS THE PROVIDER'S CALLS RATHER
// THAN THE RESPONSES. A test asserting "some requests were refused" would pass for a surface
// that refused the wrong ones; what matters is the number of times THIS PROCESS TALKED TO THE
// PROVIDER, which is the thing being amplified.
func TestOneClientCannotAmplifyRequestsAtTheProvider(t *testing.T) {
	stub := &stubOAuth{}
	srv := providerServer(t, stub)

	const attempts = 200
	completed := 0
	for i := 0; i < attempts; i++ {
		start := httptest.NewRequest("POST", OAuthStartPath, nil)
		start.Header.Set("Origin", "https://"+start.Host)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, start)
		if rec.Code != http.StatusSeeOther {
			continue
		}
		var cookie *http.Cookie
		for _, c := range rec.Result().Cookies() {
			if c.Name == oauthFlightCookieName && c.Value != "" {
				cookie = c
			}
		}
		if cookie == nil {
			continue
		}
		cb := httptest.NewRequest("GET", OAuthCallbackPath+"?code=fixture-code", nil)
		cb.AddCookie(cookie)
		rec2 := httptest.NewRecorder()
		srv.ServeHTTP(rec2, cb)
		if rec2.Code == http.StatusSeeOther {
			completed++
		}
	}

	// INSTRUMENT CONTROL: the loop really did complete sign-ins, so a low provider count is a
	// bound and not a harness that never reached the handler.
	if completed == 0 {
		t.Fatal("NO sign-in completed across 200 attempts, so the provider count below measures a broken " +
			"fixture rather than a bound")
	}
	if len(stub.codes) > maxFlightsPerClient {
		t.Errorf("ONE client reached the provider %d time(s) from %d attempts, against a per-client cap of "+
			"%d. A spent flight must keep its slot until it EXPIRES, or this endpoint amplifies one inbound "+
			"request into one outbound request at the identity provider — each holding a socket for up to %s.",
			len(stub.codes), attempts, maxFlightsPerClient, supabaseOAuthTimeoutForTest)
	}
	if completed > maxFlightsPerClient {
		t.Errorf("%d sign-ins completed for one client against a cap of %d", completed, maxFlightsPerClient)
	}
	t.Logf("amplification: %d attempts, %d completed, provider reached %d time(s), cap %d",
		attempts, completed, len(stub.codes), maxFlightsPerClient)
}

// supabaseOAuthTimeoutForTest is only the number this package can quote in a message — the
// real bound is `identity.supabaseOAuthTimeout`, which is unexported. Named rather than
// inlined so a reader does not mistake it for a second source of truth.
const supabaseOAuthTimeoutForTest = 10 * time.Second

// TestTheStartRowSaysNOTHINGAboutWhyItRefusedToBegin pins `oauthNotStarted`, which nothing
// asserted: three mutants survived both suites — rendering the lockout sentence at the lockout
// site, reverting the constant to `signInRefused`, and emptying it.
//
// 🔴 THE FIRST MUTANT IS THE ONE THAT MATTERS: a distinct sentence at the lockout site turns
// the PUBLIC start row into a lockout oracle in words, which is what the constant's own comment
// forbids. So the two observable sites must render the SAME body, and it must be this one.
//
// ⚠ THE OTHER TWO SITES ARE THE UNREACHABLE ENTROPY BRANCHES (see `flightRefusedNoID`), so no
// test can drive them and none pretends to.
func TestTheStartRowSaysNOTHINGAboutWhyItRefusedToBegin(t *testing.T) {
	cfg := testConfig(t, refusingAuth{})
	cfg.OAuth = &stubOAuth{}
	limiter := netid.NewRateLimiter(netid.DefaultMaxFailures, time.Minute, time.Hour)
	cfg.Limiter = limiter
	srv, err := New(cfg)
	if err != nil {
		t.Fatalf("the server did not build: %v", err)
	}

	// POSITIVE CONTROL: this request SUCCEEDS, so a refusal below is the arm's doing.
	warm := httptest.NewRequest("POST", OAuthStartPath, nil)
	warm.Header.Set("Origin", "https://"+warm.Host)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, warm)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POSITIVE CONTROL FAILED: an ordinary start answered %d, not 303", rec.Code)
	}

	// (a) NO RESOLVABLE CLIENT — a peer address `netid.PeerAddress` cannot parse. Nothing
	// drove this path before.
	unresolvable := httptest.NewRequest("POST", OAuthStartPath, nil)
	unresolvable.Header.Set("Origin", "https://"+unresolvable.Host)
	unresolvable.RemoteAddr = "not-an-address"
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, unresolvable)
	unresolvableBody := rec.Body.String()
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("an unresolvable client answered %d, want 401", rec.Code)
	}

	// (b) LOCKED OUT.
	probe := httptest.NewRequest("POST", OAuthStartPath, nil)
	client, _, ok := netid.ResolveClient(probe.Header, probe.RemoteAddr, nil)
	if !ok {
		t.Fatal("PRECONDITION FAILED: the fixture request has no resolvable client identity")
	}
	for i := 0; i < netid.DefaultMaxFailures; i++ {
		limiter.RecordFailure(client)
	}
	locked := httptest.NewRequest("POST", OAuthStartPath, nil)
	locked.Header.Set("Origin", "https://"+locked.Host)
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, locked)
	lockedBody := rec.Body.String()
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("a locked-out client answered %d, want 401", rec.Code)
	}

	// Both must carry `oauthNotStarted` — non-empty, and NOT the credential sentence.
	if oauthNotStarted == "" || oauthNotStarted == signInRefused {
		t.Fatalf("oauthNotStarted is %q; it must be a THIRD non-empty sentence. Empty renders a refusal that "+
			"says nothing at all, and reusing %q tells somebody who typed no credential that theirs was "+
			"rejected.", oauthNotStarted, signInRefused)
	}
	for _, arm := range []struct {
		name string
		body string
	}{
		{"no resolvable client", unresolvableBody},
		{"locked out", lockedBody},
	} {
		if !strings.Contains(arm.body, oauthNotStarted) {
			t.Errorf("%s did not render %q", arm.name, oauthNotStarted)
		}
		if strings.Contains(arm.body, signInRefused) {
			t.Errorf("%s rendered the CREDENTIAL refusal %q to somebody who presented none",
				arm.name, signInRefused)
		}
		if strings.Contains(arm.body, oauthIncomplete) {
			t.Errorf("%s rendered %q, which is for an abandoned FLIGHT rather than a refusal to begin",
				arm.name, oauthIncomplete)
		}
	}

	// 🔴 AND THE TWO ARMS ARE INDISTINGUISHABLE, which is what stops the row being a lockout
	// oracle. A mutant rendering a distinct sentence at the lockout site fails HERE.
	if unresolvableBody != lockedBody {
		t.Error("the two refusals to BEGIN rendered DIFFERENT bodies, so the public start row discloses " +
			"which check fired — a caller can tell a lockout from an unidentifiable peer, which is the " +
			"lockout oracle `signInRefused`'s own ruling refuses")
	}
}

// TestALockedOutClientOpensNoFlight pins that the existing sign-in lockout covers the
// provider door too.
//
// 🔴 IT IS A REGRESSION TEST FOR A REAL HOLE IN THE FIRST DRAFT OF THIS CHANGE: `handleSignIn`
// consulted the limiter and `handleOAuthStart` did not, so a client locked out for five failed
// credential attempts could still drive the start row without limit.
//
// ⚠ WHAT IT DOES *NOT* ASSERT, DELIBERATELY: that starting a flight RECORDS a failure. It must
// not. `netid.RateLimiter` counts failed auth into one bucket per client, shared with
// `POST /sign-in` at 5 failures per window — so recording here would mean five clicks of this
// button lock the caller out of the CREDENTIAL FORM. The second assertion below pins that
// absence, because a later edit "tidying" the limiter calls to match would otherwise look
// like a consistency fix.
func TestALockedOutClientOpensNoFlight(t *testing.T) {
	cfg := testConfig(t, refusingAuth{})
	stub := &stubOAuth{}
	cfg.OAuth = stub
	limiter := netid.NewRateLimiter(netid.DefaultMaxFailures, time.Minute, time.Hour)
	cfg.Limiter = limiter
	srv, err := New(cfg)
	if err != nil {
		t.Fatalf("the server did not build: %v", err)
	}

	// The client `httptest.NewRequest` presents, locked out through the limiter's own API so
	// this test does not depend on how the lockout was reached.
	probe := httptest.NewRequest("POST", OAuthStartPath, nil)
	client, _, ok := netid.ResolveClient(probe.Header, probe.RemoteAddr, nil)
	if !ok {
		t.Fatal("PRECONDITION FAILED: the fixture request has no resolvable client identity, so the lockout " +
			"below would be keyed on nothing")
	}
	// POSITIVE CONTROL: before the lockout, this exact request opens a flight. Without it a
	// refusal below could be any other guard.
	warm := httptest.NewRequest("POST", OAuthStartPath, nil)
	warm.Header.Set("Origin", "https://"+warm.Host)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, warm)
	if rec.Code != http.StatusSeeOther || srv.flights.openCount() != 1 {
		t.Fatalf("POSITIVE CONTROL FAILED: an unlocked client answered %d with %d flight(s) open; the refusal "+
			"below would not be the lockout's doing", rec.Code, srv.flights.openCount())
	}

	for i := 0; i < netid.DefaultMaxFailures; i++ {
		limiter.RecordFailure(client)
	}
	if !limiter.LockedOut(client) {
		t.Fatal("PRECONDITION FAILED: the client is not locked out after the configured number of failures")
	}

	before := srv.flights.openCount()
	r := httptest.NewRequest("POST", OAuthStartPath, nil)
	r.Header.Set("Origin", "https://"+r.Host)
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, r)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("a LOCKED-OUT client got %d from the start row, want 401. The lockout must cover this door "+
			"too, or five failed credential attempts cost an attacker nothing here.", rec.Code)
	}
	if n := srv.flights.openCount(); n != before {
		t.Errorf("a locked-out client opened %d flight(s); the refusal must cost nothing", n-before)
	}

	// 🔴 AND THE ABSENCE: a SUCCESSFUL flight start must not count against the sign-in bucket.
	fresh := testConfig(t, refusingAuth{})
	fresh.OAuth = &stubOAuth{}
	freshLimiter := netid.NewRateLimiter(netid.DefaultMaxFailures, time.Minute, time.Hour)
	fresh.Limiter = freshLimiter
	srv2, err := New(fresh)
	if err != nil {
		t.Fatalf("the server did not build: %v", err)
	}
	for i := 0; i < netid.DefaultMaxFailures+2; i++ {
		req := httptest.NewRequest("POST", OAuthStartPath, nil)
		req.Header.Set("Origin", "https://"+req.Host)
		srv2.ServeHTTP(httptest.NewRecorder(), req)
	}
	if freshLimiter.LockedOut(client) {
		t.Errorf("%d successful flight starts LOCKED THE CLIENT OUT. Starting a sign-in is not a failed one, "+
			"and that bucket is shared with POST /sign-in — so this would lock a person out of the credential "+
			"form, which is the door that works when the provider is down.", netid.DefaultMaxFailures+2)
	}
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

	// 🔴 AND THE FLIGHT WAS CONSUMED BEFORE THE QUERY WAS READ, WHICH THIS TEST IS THE ONLY
	// PLACE THAT CAN MEASURE. `handleOAuthCallback`'s comment asserts the flight is taken
	// "before anything else is read, and unconditionally" — and moving the `error`/`code`
	// checks ahead of `flights.take` passed the whole suite, so the property had no guard.
	// It matters because a handler that checked the query first would leave a REPLAYABLE
	// flight behind for every malformed callback, and a malformed callback is the cheapest
	// request an attacker can send.
	//
	// The flight above was consumed, so it is spent — and a spent record keeps its slot until
	// it expires (see `flights.take`), so "consumed" is read as `take` refusing a second
	// presentation rather than as the table emptying.
	if _, held := srv.flights.take(cookie.Value); held {
		t.Error("the flight was still takeable after a callback that carried a provider ERROR, so the query " +
			"was read before the flight was consumed. Every malformed callback would leave a replayable " +
			"flight behind.")
	}
}

// TestTheProviderDoorIsWITHHELDWhileItsKeySetHasNeverBeenFetched is the REGRESSION test for
// the 🔴 finding of round 1: arming Supabase coupled the whole surface's ability to START to
// the identity provider's uptime.
//
// 🔴 WHAT THE OLD SHAPE COST. `cmd/cairn-ui` exited 78 when the first JWKS fetch failed,
// mirroring `cmd/cairn-server`. The pod can afford that — one door, so refusing to start and
// refusing every request are the same outcome. THIS SURFACE HAS TWO, and the entire stated
// reason the credential form is kept is that it works when the provider does not. A GoTrue
// restarting while this pod was rescheduled meant CrashLoopBackOff: the entries page, the
// share flow, the credential form and every already-issued cookie session unservable, because
// a door nobody was using could not reach its key set.
//
// 🔴 AND IT IS STILL FAIL-CLOSED FOR AUTHENTICATION, which is the half a reader should check
// rather than take on faith: the withheld thing is the BUTTON. No Supabase token can verify
// without keys, so the backend refuses every one — that is `internal/identity`'s to enforce and
// it is unchanged. This test pins the AVAILABILITY half: the other door still answers.
func TestTheProviderDoorIsWITHHELDWhileItsKeySetHasNeverBeenFetched(t *testing.T) {
	fetched := false
	cfg := testConfig(t, refusingAuth{})
	cfg.OAuth = &stubOAuth{}
	cfg.OAuthReady = func() bool { return fetched }
	srv, err := New(cfg)
	if err != nil {
		t.Fatalf("a server whose provider is not ready must still BUILD — refusing to build is the failure "+
			"this whole finding is about: %v", err)
	}

	// (1) NOT READY: the two provider rows answer 503, the button is absent, and the
	// credential form is untouched.
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest("GET", SignInPath, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("the sign-in page answered %d while the provider was unready; the page that offers the "+
			"OTHER door must still render", rec.Code)
	}
	body := rec.Body.String()
	if strings.Contains(body, OAuthStartPath) {
		t.Error("the sign-in page offered the provider button while its key set had never been fetched, so " +
			"the button leads to a 503")
	}
	if !strings.Contains(body, `name="`+FieldToken+`"`) {
		t.Error("the CREDENTIAL FORM is missing while the provider is unready. That is the door whose whole " +
			"purpose is to work when the provider does not.")
	}

	start := httptest.NewRequest("POST", OAuthStartPath, nil)
	start.Header.Set("Origin", "https://"+start.Host)
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, start)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("the start row answered %d while unready, want 503 (not 501: the route IS implemented here, "+
			"it just cannot be served yet)", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), oauthNotReady) {
		t.Errorf("the refusal did not carry %q", oauthNotReady)
	}
	if n := srv.flights.openCount(); n != 0 {
		t.Errorf("an unready start opened %d flight(s); it must cost nothing", n)
	}
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest("GET", OAuthCallbackPath+"?code=x", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("the callback answered %d while unready, want 503", rec.Code)
	}

	// (2) AND IT RE-ARMS WITH NO RESTART, which is why readiness is a predicate rather than a
	// value read once at construction. A boolean sampled at startup would leave the door shut
	// until somebody noticed and restarted the pod.
	fetched = true
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest("GET", SignInPath, nil))
	if !strings.Contains(rec.Body.String(), OAuthStartPath) {
		t.Error("the button did not come back after the key set became available, so the door needs a " +
			"restart to re-arm — which is the failure mode a predicate exists to avoid")
	}
	start2 := httptest.NewRequest("POST", OAuthStartPath, nil)
	start2.Header.Set("Origin", "https://"+start2.Host)
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, start2)
	if rec.Code != http.StatusSeeOther {
		t.Errorf("the start row answered %d once the key set was available, want 303", rec.Code)
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

// TestTheStylesheetIsServedAsItsOwnRoute pins that every page reaches the stylesheet through
// the route that answers it.
//
// ⚠ IT WAS WRITTEN AS THE REGRESSION TEST FOR THE CSP CHANGE — under `style-src 'self'` an
// inline `<style>` did not apply, so a page that still inlined one rendered unstyled with
// nothing going red. THAT PREMISE IS GONE: the policy was deleted by operator decision, so an
// inline `<style>` would work again. The test is kept and the reason is restated rather than
// the test deleted, because the relationship it pins is what the surface still depends on and
// the bytes are now BUILD OUTPUT (`tailwind.css` → `app.css`, ~29 KB) that nobody wants
// inlined into every response. Do not read the `<style>` assertion below as a claim about a
// policy: it is a claim about where the stylesheet lives.
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
			t.Errorf("%s carries an inline <style> element. The stylesheet is build output served from "+
				"%s and cached for five minutes; inlining it sends ~29 KB on every response and leaves "+
				"this test's link/route relationship unstated.", page, StylesheetPath)
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
	served := rec.Body.String()
	if served != stylesheet {
		t.Errorf("the served stylesheet is %d bytes and the constant is %d; they must be the same bytes",
			len(served), len(stylesheet))
	}
	// 🔴 AND THE BYTES ARE JUDGED INDEPENDENTLY OF THE CONSTANT, BECAUSE THE COMPARISON ABOVE
	// IS `a == a`. Measured: `stylesheet = ""` shipped green — the route served nothing, every
	// page linked it, and the equality held. So the served body is also checked for CONTENT it
	// can only have if it is a real stylesheet, over selectors the pages actually use.
	if len(served) < 200 {
		t.Errorf("the served stylesheet is %d bytes, which is not a stylesheet. The equality above cannot see "+
			"this: an EMPTY constant satisfies it while every page renders unstyled.", len(served))
	}
	for _, selector := range []string{".viewer", ".signin", ".replica-honesty", "body"} {
		if !strings.Contains(served, selector) {
			t.Errorf("the served stylesheet has no rule for %q, which the pages render; an equality against "+
				"the constant would pass for a stylesheet that styled nothing", selector)
		}
	}
	// 🔴 AND THE THREE PROPERTIES THE THEME IS SUPPOSED TO HAVE, BECAUSE A STYLESHEET THAT IS
	// LONG AND HAS THE RIGHT SELECTORS CAN STILL BE THE WRONG STYLESHEET. Each substring below
	// can only be produced by the thing it names, and a hand-edit of `app.css` that dropped one
	// would pass every assertion above.
	//
	// ⚠ THIS IS NOT THE MEASUREMENT OF CRITERION 3 AND MUST NOT BE READ AS ONE. It asserts the
	// media query is IN the bytes; whether a browser applies it is a browser question, answered
	// by the Chromium harness with `Emulation.setEmulatedMedia`. A test here structurally cannot
	// see that — which is exactly why the browser run is a separate claim.
	for _, want := range []struct{ substring, why string }{
		{"@media (prefers-reduced-motion: reduce)", "reduced motion is honoured at all"},
		{"animation: none !important", "reduced motion REMOVES motion rather than shortening it — " +
			"the common `0.01ms` snippet still runs the animation, one frame of it"},
		{"--color-surface", "the theme's colour tokens are present, so the pages resolve one palette"},
		{"transition-property", "there are transitions to reduce in the first place"},
	} {
		if !strings.Contains(served, want.substring) {
			t.Errorf("the served stylesheet does not contain %q, so %s is not true of it. `app.css` is "+
				"GENERATED from `tailwind.css` — regenerate with `nix run .#build-ui-stylesheet` rather "+
				"than editing the output.", want.substring, want.why)
		}
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
