package ui

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ZacxDev/cairn/internal/identity"
)

// --- fixtures -------------------------------------------------------------------------

// testHost is the origin these tests act as. Synthetic: `.invalid` is IANA-reserved and
// resolves nowhere, so no fixture URL can become a request to anybody's infrastructure.
const testHost = "cairn.invalid"

// live is a server plus the world around it: one credential, one session store, one
// clock. Every session test drives this rather than assembling its own, so a test that
// varies ONE thing varies exactly one.
type live struct {
	srv      *Server
	sessions *identity.FileSessionStore
	log      *bytes.Buffer
	now      time.Time
	t        *testing.T
}

func newLive(t *testing.T) *live {
	t.Helper()
	l := &live{
		sessions: mustSessions(t),
		log:      &bytes.Buffer{},
		now:      time.Date(2000, 6, 1, 12, 0, 0, 0, time.UTC),
		t:        t,
	}
	l.sessions.Now = func() time.Time { return l.now }

	authority := fixtureCache(t)
	cookie, err := identity.NewCookieSession(l.sessions, authority)
	if err != nil {
		t.Fatalf("the cookie backend did not build: %v", err)
	}
	machine, err := identity.NewMachineToken(authority)
	if err != nil {
		t.Fatalf("the machine-token backend did not build: %v", err)
	}
	chain, err := AuthBackends(machine, cookie)
	if err != nil {
		t.Fatalf("the chain did not build: %v", err)
	}

	srv, err := New(Config{
		Auth:        chain,
		Credentials: authority,
		Source:      staticSource{scopes: benignWorld()},
		Sessions:    l.sessions,
		TTL:         time.Hour,
		Now:         func() time.Time { return l.now },
		Log:         l.log,
	})
	if err != nil {
		t.Fatalf("the server did not build: %v", err)
	}
	l.srv = srv
	return l
}

func mustSessions(t *testing.T) *identity.FileSessionStore {
	t.Helper()
	store, err := identity.OpenFileSessionStore(filepath.Join(t.TempDir(), "sessions"))
	if err != nil {
		t.Fatalf("opening the session store: %v", err)
	}
	return store
}

// do drives one request and returns the recorder.
func (l *live) do(method, path string, form url.Values, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	l.t.Helper()
	var r *http.Request
	if form != nil {
		r = httptest.NewRequest(method, path, strings.NewReader(form.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	} else {
		r = httptest.NewRequest(method, path, nil)
	}
	r.Host = testHost
	if method != http.MethodGet {
		r.Header.Set("Origin", "https://"+testHost)
	}
	for _, c := range cookies {
		r.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	l.srv.ServeHTTP(rec, r)
	return rec
}

// signIn performs the credential exchange and returns the cookie the browser is told to
// hold. It FAILS the test on anything but a redirect, so a caller never proceeds with an
// empty cookie and reads the resulting refusals as evidence about something else.
// The variadic cookies are what a BROWSER would send — it attaches whatever it already
// holds to this request too. A helper that silently dropped them would make the fixation
// test drive a request no browser ever makes, and the revocation it exists to measure
// would never be reached.
func (l *live) signIn(token string, cookies ...*http.Cookie) *http.Cookie {
	l.t.Helper()
	rec := l.do(http.MethodPost, SignInPath, url.Values{FieldToken: {token}}, cookies...)
	if rec.Code != http.StatusSeeOther {
		l.t.Fatalf("sign-in answered %d, want 303: %s", rec.Code, rec.Body.String())
	}
	cookie := sessionCookieOf(l.t, rec)
	if cookie == nil {
		l.t.Fatal("sign-in succeeded and set NO session cookie, so the browser holds nothing")
	}
	return cookie
}

func sessionCookieOf(t *testing.T, rec *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	for _, c := range rec.Result().Cookies() {
		if c.Name == identity.SessionCookieName {
			return c
		}
	}
	return nil
}

// csrfOf pulls the token out of the rendered page's hidden field, which is the ONLY way
// a browser gets it. Reading it from `identity.CSRFTokenFor` instead would test the
// derivation against itself and would pass against a page that renders no field at all.
func csrfOf(t *testing.T, body string) string {
	t.Helper()
	const marker = `name="` + FieldCSRF + `" value="`
	i := strings.Index(body, marker)
	if i < 0 {
		return ""
	}
	rest := body[i+len(marker):]
	j := strings.Index(rest, `"`)
	if j < 0 {
		return ""
	}
	return rest[:j]
}

// --- property 1: the cookie's flags ------------------------------------------------------

// TestTheSignInResponseSetsACookieWithItsFlags reads the `Set-Cookie` header a REAL
// sign-in rendered.
//
// 🔴 THIS IS THE END-TO-END HALF OF PROPERTY 1 AND IT IS NOT THE SAME CLAIM AS
// `identity.TestTheSessionCookieCarriesItsFlagsOnTheWire`. That one measures the
// constructor; this one measures what the handler actually sent, which is what a browser
// sees. A handler that built its own `http.Cookie` literal instead of calling the
// constructor would pass the first and fail this.
func TestTheSignInResponseSetsACookieWithItsFlags(t *testing.T) {
	l := newLive(t)
	rec := l.do(http.MethodPost, SignInPath, url.Values{FieldToken: {testCredential}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("sign-in answered %d: %s", rec.Code, rec.Body.String())
	}

	header := rec.Header().Get("Set-Cookie")
	if header == "" {
		t.Fatal("the sign-in response carried NO Set-Cookie header, so every assertion below would be over an " +
			"empty string")
	}
	for _, want := range []string{"HttpOnly", "Secure", "SameSite=Lax", "Path=/"} {
		if !strings.Contains(header, want) {
			t.Errorf("the sign-in response's Set-Cookie does not carry %s: %q.\n"+
				"HttpOnly keeps the id out of `document.cookie`, which is also what makes the CSRF token's "+
				"derivation sound; Secure keeps it off plaintext; SameSite=Lax keeps the browser from attaching "+
				"it to a cross-site POST.", want, header)
		}
	}
	if !strings.HasPrefix(header, identity.SessionCookieName+"=") {
		t.Errorf("the cookie is not named %s: %q", identity.SessionCookieName, header)
	}
	t.Logf("set-cookie on a real sign-in: %q", header)
}

// --- property 6: fixation -----------------------------------------------------------------

// TestTheSessionIDChangesOnSignInAndTheOLDONEStopsWorking is the fixation guard.
//
// 🔴 "THE ID CHANGES" IS THE WEAKER HALF AND IT IS NOT ENOUGH ON ITS OWN. Session
// fixation is an attacker planting an id in a victim's browser and waiting for them to
// authenticate it. A fresh id defeats that for the browser that signed in — but if the
// planted id was a session the attacker also holds, they still hold it. So this asserts
// BOTH: the new cookie differs, AND the presented one is dead afterwards.
func TestTheSessionIDChangesOnSignInAndTheOldOneStopsWorking(t *testing.T) {
	l := newLive(t)

	first := l.signIn(testCredential)
	// PRECONDITION: the first session works, or "it stopped working" below is a
	// statement about a session that never started.
	if rec := l.do(http.MethodGet, RootPath, nil, first); rec.Code != http.StatusOK {
		t.Fatalf("PRECONDITION FAILED: the first session did not authenticate (%d)", rec.Code)
	}

	second := l.signIn(testCredential, first)
	if second.Value == first.Value {
		t.Fatal("THE SESSION ID DID NOT CHANGE ON SIGN-IN. An id a browser held before authenticating and still " +
			"holds afterwards is a session an attacker can plant and then ride.")
	}
	if rec := l.do(http.MethodGet, RootPath, nil, first); rec.Code != http.StatusUnauthorized {
		t.Errorf("THE PRE-SIGN-IN SESSION IS STILL LIVE (answered %d). A fresh id protects the browser that "+
			"signed in; it does nothing about the attacker who PLANTED the old one and still holds it. Signing "+
			"in over a session must revoke it.", rec.Code)
	}
	// POSITIVE CONTROL: the NEW session works, so the refusal above is about the old
	// one rather than about a sign-in that broke everything.
	if rec := l.do(http.MethodGet, RootPath, nil, second); rec.Code != http.StatusOK {
		t.Fatalf("POSITIVE CONTROL FAILED: the new session does not authenticate either (%d), so the refusal "+
			"above is not evidence about fixation", rec.Code)
	}
}

// --- property 5: logout revokes -----------------------------------------------------------

// TestTheWholeSessionLifecycle walks sign-in, use, sign-out and re-use, and the FOURTH
// step is the one the whole storage decision was made for.
func TestTheWholeSessionLifecycle(t *testing.T) {
	l := newLive(t)

	cookie := l.signIn(testCredential)
	page := l.do(http.MethodGet, RootPath, nil, cookie)
	if page.Code != http.StatusOK {
		t.Fatalf("the page answered %d for a signed-in browser", page.Code)
	}
	token := csrfOf(t, page.Body.String())
	if token == "" {
		t.Fatal("the page rendered NO csrf field, so the sign-out below could never be authorised and every " +
			"refusal after it would be about a missing token rather than about the logout")
	}

	out := l.do(http.MethodPost, SignOutPath, url.Values{FieldCSRF: {token}}, cookie)
	if out.Code != http.StatusSeeOther {
		t.Fatalf("sign-out answered %d, want 303: %s", out.Code, out.Body.String())
	}
	if cleared := sessionCookieOf(t, out); cleared == nil || cleared.MaxAge >= 0 {
		t.Errorf("sign-out did not send a cleared cookie: %+v", cleared)
	}

	// 🔴 THE SAME COOKIE, REPLAYED. This is the property a client-held JWT cannot have:
	// the credential itself is refused, not merely discarded by the browser that was
	// willing to discard it.
	after := l.do(http.MethodGet, RootPath, nil, cookie)
	if after.Code != http.StatusUnauthorized {
		t.Errorf("THE REVOKED COOKIE STILL AUTHENTICATES (answered %d). Logout must REVOKE, not just ask the "+
			"browser to forget: a cookie a user has signed out of is exactly the cookie an attacker who captured "+
			"it will replay.", after.Code)
	}
	// And it is refused because the SESSION is gone, not because the server broke: a
	// fresh sign-in still works.
	fresh := l.signIn(testCredential)
	if rec := l.do(http.MethodGet, RootPath, nil, fresh); rec.Code != http.StatusOK {
		t.Fatalf("POSITIVE CONTROL FAILED: a fresh sign-in after a sign-out does not authenticate (%d), so the "+
			"refusal above is evidence about a broken server rather than about revocation", rec.Code)
	}
}

// --- property 4: expiry, end to end ---------------------------------------------------------

// TestAnExpiredSessionIsRefusedByTheSURFACE moves the injected clock rather than sleeping.
func TestAnExpiredSessionIsRefusedBySurface(t *testing.T) {
	l := newLive(t)
	cookie := l.signIn(testCredential)

	if rec := l.do(http.MethodGet, RootPath, nil, cookie); rec.Code != http.StatusOK {
		t.Fatalf("PRECONDITION FAILED: a fresh session did not authenticate (%d)", rec.Code)
	}
	// The TTL is one hour; step to one second before the boundary, then past it.
	l.now = l.now.Add(time.Hour - time.Second)
	if rec := l.do(http.MethodGet, RootPath, nil, cookie); rec.Code != http.StatusOK {
		t.Errorf("a session one second inside its lifetime was refused (%d); an expiry that fires early is a "+
			"surface that signs people out at random", rec.Code)
	}
	l.now = l.now.Add(2 * time.Second)
	if rec := l.do(http.MethodGet, RootPath, nil, cookie); rec.Code != http.StatusUnauthorized {
		t.Errorf("AN EXPIRED SESSION WAS SERVED (answered %d). The absolute lifetime is the bound on how long a "+
			"stolen cookie is worth stealing.", rec.Code)
	}
}

// --- property 2: CSRF ------------------------------------------------------------------------

// TestTheCSRFGuardIsReachedByAnAuthenticatedRequest is the REACHABILITY proof, and it is
// the assertion this whole phase turns on.
//
// 🔴 A CSRF TEST THAT SENDS NO CREDENTIAL PROVES NOTHING. The obvious shape — "POST
// without a token is refused" — is satisfied by a server whose token check is deleted,
// because the authentication chain refuses an anonymous request first and the response is
// a refusal either way. So every arm below is AUTHENTICATED and carries a correct
// `Origin`; the only thing left that can refuse them is gate (6), and each arm asserts
// gate (6)'s OWN message rather than merely "a refusal happened".
func TestTheCSRFGuardIsReachedByAnAuthenticatedRequest(t *testing.T) {
	l := newLive(t)
	cookie := l.signIn(testCredential)
	page := l.do(http.MethodGet, RootPath, nil, cookie)
	good := csrfOf(t, page.Body.String())
	if good == "" {
		t.Fatal("no csrf token was rendered, so the positive control below cannot succeed and every refusal " +
			"would be unattributable")
	}

	// POSITIVE CONTROL FIRST: this exact request, with the token, must SUCCEED. Without
	// it, "the request was refused" is indistinguishable from "this route never works".
	if rec := l.do(http.MethodPost, SignOutPath, url.Values{FieldCSRF: {good}}, cookie); rec.Code != http.StatusSeeOther {
		t.Fatalf("POSITIVE CONTROL FAILED: an authenticated POST WITH a valid token answered %d, not 303. The "+
			"refusals below would then be about this route being broken.", rec.Code)
	}

	for _, arm := range []struct {
		name string
		form url.Values
	}{
		{"no token at all", url.Values{}},
		{"an empty token", url.Values{FieldCSRF: {""}}},
		{"a well-formed token from another session", url.Values{FieldCSRF: {identity.CSRFTokenFor("some-other-session-id")}}},
		{"the token with one character changed", url.Values{FieldCSRF: {flipLast(good)}}},
		{"the SESSION ID presented as the token", url.Values{FieldCSRF: {cookie.Value}}},
	} {
		t.Run(arm.name, func(t *testing.T) {
			// A fresh session per arm: the positive control above consumed one, and an
			// arm that ran against a revoked session would be refused by the CHAIN.
			l := newLive(t)
			c := l.signIn(testCredential)
			rec := l.do(http.MethodPost, SignOutPath, arm.form, c)

			if rec.Code == http.StatusSeeOther {
				t.Fatalf("THE STATE CHANGE WAS PERFORMED. An authenticated, same-origin POST with %s must be "+
					"refused: the cookie is attached by the browser whatever caused the request, so the token in "+
					"the body is the only thing that says the request came from a page this surface rendered.",
					arm.name)
			}
			if rec.Code != http.StatusForbidden {
				t.Fatalf("answered %d, want 403. A 401 here would mean the AUTHENTICATION chain refused it, "+
					"which would make this test evidence about the chain rather than about the csrf gate.", rec.Code)
			}
			if body := rec.Body.String(); body != csrfRefusal {
				t.Errorf("the refusal body is %q, want %q. A kill by a different gate — the same-origin one "+
					"answers %q — is a misattribution, and it would report a deleted csrf check as covered.",
					body, csrfRefusal, crossSiteRefusal)
			}
			// AND THE STATE DID NOT CHANGE: the session is still live afterwards.
			if page := l.do(http.MethodGet, RootPath, nil, c); page.Code != http.StatusOK {
				t.Errorf("the session was revoked (%d) by a request the server REFUSED; a refusal that performs "+
					"the action is worse than no gate", page.Code)
			}
		})
	}
}

// TestTheCSRFTokenIsAcceptedInAHeaderToo pins the second accepted position, so a
// non-browser client holding a session is not forced to build a form body.
func TestTheCSRFTokenIsAcceptedInAHeaderToo(t *testing.T) {
	l := newLive(t)
	cookie := l.signIn(testCredential)
	page := l.do(http.MethodGet, RootPath, nil, cookie)
	token := csrfOf(t, page.Body.String())

	r := httptest.NewRequest(http.MethodPost, SignOutPath, nil)
	r.Host = testHost
	r.Header.Set("Origin", "https://"+testHost)
	r.Header.Set(HeaderCSRF, token)
	r.AddCookie(cookie)
	rec := httptest.NewRecorder()
	l.srv.ServeHTTP(rec, r)
	if rec.Code != http.StatusSeeOther {
		t.Errorf("a valid token in %s answered %d, want 303: %s", HeaderCSRF, rec.Code, rec.Body.String())
	}
}

// --- gate (2): the same-origin check ------------------------------------------------------------

// TestACrossSiteStateChangeIsRefusedBeforeAuthentication pins the gate that covers the
// PUBLIC sign-in row, which by definition has no session to carry a token.
//
// 🔴 IT IS ASSERTED BY ITS OWN MESSAGE FOR THE SAME REASON THE CSRF ARMS ARE. Both gates
// answer 403; a test that read the status alone would score a kill by the wrong gate as a
// kill.
func TestACrossSiteStateChangeIsRefusedBeforeAuthentication(t *testing.T) {
	l := newLive(t)
	cookie := l.signIn(testCredential)

	for _, arm := range []struct {
		name   string
		origin string
		set    bool
	}{
		{"an attacker's origin", "https://evil.invalid", true},
		{"a subdomain of this host", "https://other." + testHost, true},
		{"this host on another port", "https://" + testHost + ":8443", true},
		{"no Origin header at all", "", false},
		{"an unparseable Origin", "://", true},
	} {
		t.Run(arm.name, func(t *testing.T) {
			for _, path := range []string{SignInPath, SignOutPath} {
				r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(""))
				r.Host = testHost
				r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				if arm.set {
					r.Header.Set("Origin", arm.origin)
				}
				r.AddCookie(cookie)
				rec := httptest.NewRecorder()
				l.srv.ServeHTTP(rec, r)

				if rec.Code != http.StatusForbidden {
					t.Errorf("POST %s with %s answered %d, want 403", path, arm.name, rec.Code)
					continue
				}
				if body := rec.Body.String(); body != crossSiteRefusal {
					t.Errorf("POST %s with %s answered %q, want %q — the csrf gate's own message is %q and "+
						"scoring one as the other would report this gate as covered when it is not",
						path, arm.name, body, crossSiteRefusal, csrfRefusal)
				}
			}
		})
	}

	// POSITIVE CONTROL: a same-origin POST to the public row is NOT refused by this
	// gate, so the five refusals above are about the origin rather than about the row.
	rec := l.do(http.MethodPost, SignInPath, url.Values{FieldToken: {testCredential}})
	if rec.Code == http.StatusForbidden && rec.Body.String() == crossSiteRefusal {
		t.Fatal("POSITIVE CONTROL FAILED: a SAME-origin sign-in is refused by the same-origin gate too, so the " +
			"gate refuses everything and its refusals above mean nothing")
	}
}

// TestASafeMethodIsNotGuarded is the mirror, and it is what stops the two gates being
// "refuse every request" with extra steps.
func TestASafeMethodIsNotGuarded(t *testing.T) {
	l := newLive(t)
	cookie := l.signIn(testCredential)
	r := httptest.NewRequest(http.MethodGet, RootPath, nil)
	r.Host = testHost
	r.Header.Set("Origin", "https://evil.invalid")
	r.AddCookie(cookie)
	rec := httptest.NewRecorder()
	l.srv.ServeHTTP(rec, r)
	if rec.Code != http.StatusOK {
		t.Errorf("a GET with a foreign Origin answered %d; `GET` is defined as having no side effects, and a "+
			"surface that refuses a cross-site READ breaks every ordinary link into it while protecting nothing",
			rec.Code)
	}
}

// --- property 7: no secret reaches a log or a page --------------------------------------------

// TestNoSecretReachesTheLogOrThePage drives a whole lifecycle and then greps the captured
// output.
//
// 🔴 IT REPORTS A PAIR. A zero occurrence count is indistinguishable from a log wired to
// `io.Discard` and a page that failed to render, so the log must be NON-EMPTY and the page
// must contain a marker only a real render produces before the zeroes are read.
//
// ⚠ THE CSRF TOKEN IS IN THE PAGE ON PURPOSE AND IS NOT COUNTED AS A LEAK. It is
// `HMAC(session id)`, a hidden form field is the only place it can live, and disclosing a
// MAC does not disclose its key. What must not appear is the SESSION ID, the stored
// DIGEST, and the credential the form carried.
func TestNoSecretReachesTheLogOrThePage(t *testing.T) {
	l := newLive(t)
	cookie := l.signIn(testCredential)
	page := l.do(http.MethodGet, RootPath, nil, cookie)
	body := page.Body.String()
	token := csrfOf(t, body)
	l.do(http.MethodPost, SignOutPath, url.Values{FieldCSRF: {token}}, cookie)
	// A refused sign-in too: its error path is where an echoed credential would land.
	refused := l.do(http.MethodPost, SignInPath, url.Values{FieldToken: {"a-wrong-credential-value"}})

	logged := l.log.String()
	// INSTRUMENT CONTROLS, both halves.
	if logged == "" {
		t.Fatal("NOTHING was logged across a sign-in, a page render, a sign-out and a refused sign-in, so the " +
			"absences below are about a sink wired to nothing rather than about what this surface writes")
	}
	if !strings.Contains(body, "signed in as") {
		t.Fatalf("the page did not render (%d bytes), so the absences below are about an empty string", len(body))
	}

	secrets := map[string]string{
		"the session id":            cookie.Value,
		"the stored session digest": identity.SessionDigest(cookie.Value),
		"the presented credential":  testCredential,
		"the wrong credential":      "a-wrong-credential-value",
	}
	for name, secret := range secrets {
		if strings.Contains(logged, secret) {
			t.Errorf("THE LOG CONTAINS %s. A credential printed to stdout or stderr is re-staged in every "+
				"transcript, log shipper and crash dump that captures the run, and a session id in a log is a "+
				"session anybody with log access can assume.", name)
		}
		if strings.Contains(body, secret) {
			t.Errorf("THE RENDERED PAGE CONTAINS %s", name)
		}
		if strings.Contains(refused.Body.String(), secret) {
			t.Errorf("THE REFUSED SIGN-IN PAGE CONTAINS %s. Echoing a submitted credential back into the form "+
				"is what an ordinary form does to be helpful; here it writes a bearer credential into a page, "+
				"into the browser's back/forward cache, and into any transcript that captures the response.", name)
		}
	}
	// POSITIVE CONTROL on the grep itself: a string that IS in the log must be found,
	// or `strings.Contains` is being called against the wrong buffer.
	if !strings.Contains(logged, "sign-in") {
		t.Fatalf("POSITIVE CONTROL FAILED: the log does not contain the word `sign-in` either, so the four "+
			"absences above are not a measurement. Captured: %q", logged)
	}
	t.Logf("secret sweep: %d secret(s) sought, 0 found across %d bytes of log and %d bytes of page",
		len(secrets), len(logged), len(body))
}

// TestARefusedSignInSaysNothingSpecific pins the uniform refusal a human reads.
func TestARefusedSignInSaysNothingSpecific(t *testing.T) {
	l := newLive(t)
	for _, token := range []string{"", "a-wrong-credential-value", testCredential + "x"} {
		rec := l.do(http.MethodPost, SignInPath, url.Values{FieldToken: {token}})
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("a refused sign-in answered %d, want 401", rec.Code)
		}
		if !strings.Contains(rec.Body.String(), signInRefused) {
			t.Errorf("the refusal does not say %q", signInRefused)
		}
		if sessionCookieOf(t, rec) != nil {
			t.Error("A REFUSED SIGN-IN SET A SESSION COOKIE")
		}
	}
	// POSITIVE CONTROL: the right credential is accepted, so the refusals above are
	// about the credential rather than about a sign-in that never works.
	if rec := l.do(http.MethodPost, SignInPath, url.Values{FieldToken: {testCredential}}); rec.Code != http.StatusSeeOther {
		t.Fatalf("POSITIVE CONTROL FAILED: the correct credential answered %d", rec.Code)
	}
}

// TestAFailedSignInDoesNotRevokeTheExistingSession pins the ordering inside the handler.
//
// 🔴 THE OTHER ORDERING IS A DENIAL OF SERVICE. Revoking on a failed attempt means
// anybody who can make a victim's browser submit this form with a junk token signs them
// out at will. Gate (2) already refuses that request, so this is defence in depth — but
// the ordering costs nothing and the other one is live the moment gate (2) is relaxed.
func TestAFailedSignInDoesNotRevokeTheExistingSession(t *testing.T) {
	l := newLive(t)
	cookie := l.signIn(testCredential)
	if rec := l.do(http.MethodPost, SignInPath, url.Values{FieldToken: {"a-wrong-credential-value"}}, cookie); rec.Code != http.StatusUnauthorized {
		t.Fatalf("PRECONDITION FAILED: the wrong credential answered %d, not 401", rec.Code)
	}
	if rec := l.do(http.MethodGet, RootPath, nil, cookie); rec.Code != http.StatusOK {
		t.Errorf("A FAILED SIGN-IN REVOKED THE LIVE SESSION (the page now answers %d). Anybody who can cause "+
			"that form submission could then sign a user out at will.", rec.Code)
	}
}

// TestTheSignInPageRendersNoTokenAndNoCSRFField pins both halves of what the public page
// must not carry.
func TestTheSignInPageRendersNoTokenAndNoCSRFField(t *testing.T) {
	l := newLive(t)
	rec := l.do(http.MethodGet, SignInPath, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("the sign-in page answered %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `type="password"`) {
		t.Error("the credential field is not `type=password`; the value must not be shoulder-readable and a " +
			"browser must not offer to remember it as ordinary form text")
	}
	if got := csrfOf(t, body); got != "" {
		t.Errorf("the PUBLIC sign-in form carries a csrf token (%q). There is no session yet, so any token here "+
			"is derived from nothing and validates against nothing; what stands in front of login-CSRF is the "+
			"same-origin gate, which needs no credential.", got)
	}
	if !strings.Contains(body, `action="`+SignInPath+`"`) {
		t.Errorf("the form does not post to %s", SignInPath)
	}
}

// TestTheSignOutControlIsAbsentWithoutASession pins that a button which cannot work is
// not rendered.
func TestTheSignOutControlIsAbsentWithoutASession(t *testing.T) {
	// A header-authenticated caller with no cookie.
	l := newLive(t)
	r := httptest.NewRequest(http.MethodGet, RootPath, nil)
	r.Host = testHost
	r.Header.Set("Authorization", "Bearer "+testCredential)
	rec := httptest.NewRecorder()
	l.srv.ServeHTTP(rec, r)
	if rec.Code != http.StatusOK {
		t.Fatalf("PRECONDITION FAILED: the bearer credential did not authenticate (%d), so the absence below is "+
			"about a refusal rather than about the render", rec.Code)
	}
	if strings.Contains(rec.Body.String(), `action="`+SignOutPath+`"`) {
		t.Error("the page rendered a sign-out form for a caller with no session cookie. The token is derived " +
			"from the cookie, so the form could not validate, and a control that is present and cannot work " +
			"teaches a user that sign-out is unreliable.")
	}
	// POSITIVE CONTROL: with a session, the form IS there.
	cookie := l.signIn(testCredential)
	page := l.do(http.MethodGet, RootPath, nil, cookie)
	if !strings.Contains(page.Body.String(), `action="`+SignOutPath+`"`) {
		t.Fatal("POSITIVE CONTROL FAILED: the sign-out form is absent for a SESSION caller too, so the absence " +
			"above is not a measurement")
	}
}

// TestANilPartIsRefusedAtConstruction pins each sentinel separately, because an operator
// reading a startup refusal needs to know which wiring is missing.
func TestANilPartIsRefusedAtConstruction(t *testing.T) {
	base := func() Config { return testConfig(t, staticAuth{testIdentity()}) }
	for _, arm := range []struct {
		name string
		bend func(*Config)
		want error
	}{
		{"no authenticator", func(c *Config) { c.Auth = nil }, ErrNoAuthenticator},
		{"no credential authority", func(c *Config) { c.Credentials = nil }, ErrNoCredentials},
		{"no source", func(c *Config) { c.Source = nil }, ErrNoSource},
		{"no session store", func(c *Config) { c.Sessions = nil }, ErrNoSessions},
		{"a negative TTL", func(c *Config) { c.TTL = -time.Second }, ErrNegativeTTL},
	} {
		cfg := base()
		arm.bend(&cfg)
		if _, err := New(cfg); err != arm.want {
			t.Errorf("%s: New returned %v, want %v", arm.name, err, arm.want)
		}
	}
	// POSITIVE CONTROL: the unbent config builds, so the five refusals are about the
	// one field each bent.
	if _, err := New(base()); err != nil {
		t.Fatalf("POSITIVE CONTROL FAILED: a fully wired config was refused (%v)", err)
	}
	// A zero TTL is LEGAL and means the default; asserting it separately stops somebody
	// "tidying" it into the refusal above.
	cfg := base()
	cfg.TTL = 0
	srv, err := New(cfg)
	if err != nil {
		t.Fatalf("a zero TTL must mean the default, got %v", err)
	}
	if srv.ttl != identity.DefaultSessionTTL {
		t.Errorf("a zero TTL resolved to %v, want %v", srv.ttl, identity.DefaultSessionTTL)
	}
}

// flipLast changes the last character of a token, producing a near-miss rather than a
// wholly different string — the shape a comparison bug is most likely to accept.
func flipLast(s string) string {
	if s == "" {
		return "x"
	}
	last := s[len(s)-1]
	if last == 'A' {
		return s[:len(s)-1] + "B"
	}
	return s[:len(s)-1] + "A"
}
