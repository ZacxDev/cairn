package ui

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/ZacxDev/cairn/internal/control"
	"github.com/ZacxDev/cairn/internal/identity"
	"github.com/ZacxDev/cairn/internal/invite"
)

// The OAuth callback's REDEMPTION arms — the path that can bring a principal into
// existence, and the one nothing drove.
//
// # 🔴 WHY THIS FILE EXISTS
//
// Two places in the tree claimed this was covered: `invitefixture_test.go`'s `Redeem` stub
// ("`oauth_test.go` drives that with its own stub") and `internal/ui/README.md`. Measured:
// `oauth_test.go` contained ZERO references to an invite or to `UnprovisionedSubject`. The
// whole of `handleOAuthCallback`'s redemption behaviour — reading the flight's token,
// calling into `Inviting`, deciding whether a failure refuses the sign-in — was driven by
// nothing, in the handler that can CREATE A PRINCIPAL.
//
// `internal/identity`'s `TestOnlyTheVerifiedUnknownSubjectArmIsProvisionable` pins the
// error TYPE; it says nothing about what this handler does with it. That is the
// "verified in isolation — the defect lives in the SEAM nobody owns" shape exactly, and
// the seam is what hid the defect this file's second case now pins.
//
// # ⚠ WHAT THESE CASES ARE
//
// They drive the REAL handler through the REAL dispatcher with a stubbed provider and a
// stubbed `Inviting`. They are not a test of `ControlInviting`'s SQL — that is the
// Postgres tier's — and not of a real GoTrue, which nothing here has ever driven.

// providerConfigWithInviting builds the standard fixture config with a stubbed provider
// and a fresh invite stub, returning all three so a case can steer both.
//
// ⚠ IT DOES NOT REUSE `providerServer`, which builds the server itself — these cases have
// to mutate the stub (which arm the exchange takes) BEFORE `New`, and one of them has to
// read the invite stub afterwards.
func providerConfigWithInviting(t *testing.T) (Config, *stubOAuth, *staticInviting) {
	t.Helper()
	cfg := testConfig(t, refusingAuth{})
	stub := &stubOAuth{}
	inviting := benignInviting()
	cfg.OAuth = stub
	cfg.Inviting = inviting
	return cfg, stub, inviting
}

// provisionedPrincipal is who the `Redeem` stub says a stranger became. Declared here
// because the fixture in `invitefixture_test.go` returns it and both files read it.
func provisionedPrincipal() control.Principal {
	return control.Principal{
		Kind:    control.KindUser,
		ID:      control.DerivedID(control.PrefixUser, "provisioned-by-invitation"),
		Display: "provisioned-by-invitation",
	}
}

// startAFlightCarryingAnInvite drives `POST /sign-in/github` with a token in the form and
// returns the flight cookie, so a callback can complete the same flow a person would.
//
// 🔴 IT GOES THROUGH THE REAL START ROW RATHER THAN PLANTING A FLIGHT. The token reaching
// the callback AT ALL is half of what these cases measure: it rides the server-side flight
// record, and a change that stopped carrying it there would be invisible to a test that
// injected the flight directly.
func startAFlightCarryingAnInvite(t *testing.T, srv *Server, token string) *http.Cookie {
	t.Helper()
	form := url.Values{inviteTokenField: {token}}
	req := httptest.NewRequest("POST", OAuthStartPath, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", "http://"+req.Host)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("the start row answered %d, want 303: %s", rec.Code, rec.Body.String())
	}
	for _, c := range rec.Result().Cookies() {
		if c.Name == oauthFlightCookieName {
			return c
		}
	}
	t.Fatal("the start row set no flight cookie, so no callback can complete")
	return nil
}

// TestAStrangerCarryingAnInvitationIsProvisionedOnTheCallback is the arm that creates a
// principal, driven end to end for the first time.
func TestAStrangerCarryingAnInvitationIsProvisionedOnTheCallback(t *testing.T) {
	cfg, stub, inviting := providerConfigWithInviting(t)
	// The exchange FAILS with the one error type that licenses provisioning.
	stub.err = &identity.UnprovisionedSubject{Provider: "supabase", Subject: "stranger-subject"}
	srv, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}

	cookie := startAFlightCarryingAnInvite(t, srv, fixtureInviteToken)
	cb := httptest.NewRequest("GET", OAuthCallbackPath+"?code=fixture-code", nil)
	cb.AddCookie(cookie)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, cb)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("the callback answered %d, want 303 (a completed sign-in): %s", rec.Code, rec.Body.String())
	}
	if inviting.redeemCalls != 1 {
		t.Fatalf("Redeem was called %d time(s), want exactly 1 — the provisioning arm did not run, so a "+
			"stranger's invitation was read by nothing", inviting.redeemCalls)
	}
	// 🔴 THE TOKEN MUST BE THE ONE OFF THE FLIGHT. A handler that read it from the callback's
	// own query or form would let the caller choose which invitation to redeem AFTER the
	// provider had authenticated them, which is the whole reason it rides the flight.
	if inviting.redeemedToken != fixtureInviteToken {
		t.Errorf("Redeem was given token %q, want the one the flight carried (%q)",
			inviting.redeemedToken, fixtureInviteToken)
	}
	// …and the (provider, subject) must be the VERIFIED pair off the error, not anything
	// the request supplied.
	if inviting.redeemedProvider != "supabase" || inviting.redeemedSubject != "stranger-subject" {
		t.Errorf("Redeem was given (%q, %q), want the verified pair from UnprovisionedSubject",
			inviting.redeemedProvider, inviting.redeemedSubject)
	}
	// 🔴 AND THE SESSION IS THE PROVISIONED PRINCIPAL'S. Without this the case passes for a
	// handler that redeems and then signs in as nobody.
	if !sessionWasOpenedFor(rec, provisionedPrincipal().ID) {
		t.Errorf("no session cookie was set for the provisioned principal; the redemption happened and "+
			"the sign-in did not. Response cookies: %v", rec.Result().Cookies())
	}
	if inviting.redeemForCalls != 0 {
		t.Errorf("RedeemFor ran %d time(s) on the PROVISIONING arm; the two arms must not both fire",
			inviting.redeemForCalls)
	}
}

// TestAKnownUserCarryingAnInvitationRedeemsItOnTheCallback is the defect this file was
// written for.
//
// 🔴 BEFORE `RedeemFor`, THIS WAS SILENT AND TOTAL. A user the control plane already holds
// exchanges SUCCESSFULLY, so the provisioning arm's `errors.As` is false; the flight's
// token was never read, the invitation stayed `open`, and the person was signed in with no
// membership and nothing in the log. The handler's own comment sent the reader to "the
// authenticated redeem route", which is not in `DeclaredRoutes()` and was never built.
func TestAKnownUserCarryingAnInvitationRedeemsItOnTheCallback(t *testing.T) {
	cfg, stub, inviting := providerConfigWithInviting(t)
	// The exchange SUCCEEDS: this is a user the control plane holds.
	stub.err = nil
	stub.principal = testIdentity().Principal
	srv, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}

	cookie := startAFlightCarryingAnInvite(t, srv, fixtureInviteToken)
	cb := httptest.NewRequest("GET", OAuthCallbackPath+"?code=fixture-code", nil)
	cb.AddCookie(cookie)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, cb)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("the callback answered %d, want 303: %s", rec.Code, rec.Body.String())
	}
	if inviting.redeemForCalls != 1 {
		t.Fatalf("RedeemFor was called %d time(s), want exactly 1. A known user's invitation is being "+
			"DISCARDED: they are signed in, the invitation stays open, and nothing says so.",
			inviting.redeemForCalls)
	}
	if inviting.redeemedForToken != fixtureInviteToken {
		t.Errorf("RedeemFor was given token %q, want the one the flight carried", inviting.redeemedForToken)
	}
	if inviting.redeemedForPrincipal.ID != testIdentity().Principal.ID {
		t.Errorf("RedeemFor was given principal %q, want the one the exchange resolved (%q)",
			inviting.redeemedForPrincipal.ID, testIdentity().Principal.ID)
	}
	if inviting.redeemCalls != 0 {
		t.Errorf("the PROVISIONING Redeem ran %d time(s) for a user that already exists — that arm may "+
			"only fire for an UnprovisionedSubject", inviting.redeemCalls)
	}
}

// TestAFailedRedemptionRefusesTheSTRANGERAndAdmitsTheKNOWNUser pins the one place the two
// arms deliberately differ, in BOTH directions.
//
// 🔴 A SINGLE-DIRECTION TEST WOULD BE SATISFIED BY A HANDLER THAT TREATED BOTH THE SAME.
// For a stranger the account exists only BECAUSE of the invitation, so a dead invitation
// means there is nobody to sign in — 401. For a known user the account stands on its own,
// so refusing would turn "your invite link expired" into "you cannot log in".
func TestAFailedRedemptionRefusesTheSTRANGERAndAdmitsTheKNOWNUser(t *testing.T) {
	t.Run("the stranger is refused", func(t *testing.T) {
		cfg, stub, inviting := providerConfigWithInviting(t)
		stub.err = &identity.UnprovisionedSubject{Provider: "supabase", Subject: "stranger-subject"}
		inviting.redeemErr = invite.ErrNotRedeemable
		srv, err := New(cfg)
		if err != nil {
			t.Fatal(err)
		}
		cookie := startAFlightCarryingAnInvite(t, srv, fixtureInviteToken)
		cb := httptest.NewRequest("GET", OAuthCallbackPath+"?code=fixture-code", nil)
		cb.AddCookie(cookie)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, cb)

		if rec.Code != http.StatusUnauthorized {
			t.Errorf("a stranger whose invitation could not be redeemed got %d, want 401 — there is no "+
				"account to sign in to", rec.Code)
		}
		if sessionWasOpenedFor(rec, provisionedPrincipal().ID) {
			t.Error("a session was opened for a stranger whose redemption FAILED")
		}
	})

	t.Run("the known user is still signed in", func(t *testing.T) {
		cfg, stub, inviting := providerConfigWithInviting(t)
		stub.err = nil
		stub.principal = testIdentity().Principal
		inviting.redeemForErr = invite.ErrNotRedeemable
		srv, err := New(cfg)
		if err != nil {
			t.Fatal(err)
		}
		cookie := startAFlightCarryingAnInvite(t, srv, fixtureInviteToken)
		cb := httptest.NewRequest("GET", OAuthCallbackPath+"?code=fixture-code", nil)
		cb.AddCookie(cookie)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, cb)

		if rec.Code != http.StatusSeeOther {
			t.Errorf("a KNOWN user whose invitation could not be redeemed got %d, want 303. Their account "+
				"does not depend on the invitation; refusing turns an expired link into a lockout.", rec.Code)
		}
		if !sessionWasOpenedFor(rec, testIdentity().Principal.ID) {
			t.Error("no session was opened for a known user whose sign-in succeeded")
		}
	})

	// ⚠ AND THE ALREADY-A-MEMBER CASE IS NOT A FAILURE. `ErrAlreadyAMember` is the one
	// redemption refusal this flow discriminates, and it must not disturb the sign-in.
	t.Run("already a member is benign", func(t *testing.T) {
		cfg, stub, inviting := providerConfigWithInviting(t)
		stub.err = nil
		stub.principal = testIdentity().Principal
		inviting.redeemForErr = ErrAlreadyAMember
		srv, err := New(cfg)
		if err != nil {
			t.Fatal(err)
		}
		cookie := startAFlightCarryingAnInvite(t, srv, fixtureInviteToken)
		cb := httptest.NewRequest("GET", OAuthCallbackPath+"?code=fixture-code", nil)
		cb.AddCookie(cookie)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, cb)
		if rec.Code != http.StatusSeeOther {
			t.Errorf("clicking an invitation for a project you are already in answered %d, want 303", rec.Code)
		}
	})
}

// TestASignInWithNoInvitationTouchesNeitherRedemptionPath is the NEGATIVE CONTROL for
// every case above.
//
// 🔴 WITHOUT IT, EVERY "Redeem RAN" ASSERTION IS SATISFIED BY A HANDLER THAT REDEEMS
// UNCONDITIONALLY — which would spend an arbitrary invitation on every sign-in.
func TestASignInWithNoInvitationTouchesNeitherRedemptionPath(t *testing.T) {
	cfg, stub, inviting := providerConfigWithInviting(t)
	stub.err = nil
	stub.principal = testIdentity().Principal
	srv, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	// The start row with NO token in the form.
	cookie := startAFlightCarryingAnInvite(t, srv, "")
	cb := httptest.NewRequest("GET", OAuthCallbackPath+"?code=fixture-code", nil)
	cb.AddCookie(cookie)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, cb)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("an ordinary sign-in answered %d, want 303: %s", rec.Code, rec.Body.String())
	}
	if inviting.redeemCalls != 0 || inviting.redeemForCalls != 0 {
		t.Errorf("a sign-in carrying NO invitation called Redeem %d time(s) and RedeemFor %d time(s); "+
			"both must be 0, or an ordinary log-in spends somebody's invitation",
			inviting.redeemCalls, inviting.redeemForCalls)
	}
}

// startAFlightWithTheInviteInTheQUERY drives `POST /sign-in/github?invite=<token>` — the
// token in the URL and NOT in the body — and returns the flight cookie.
//
// 🔴 THE BODY IS A WELL-FORMED FORM CARRYING A DIFFERENT FIELD, DELIBERATELY. A request with
// no body at all would leave `ParseForm` with nothing to parse, so an empty
// `PostFormValue` would be explained by "the body was unreadable" as well as by "the handler
// read the body". Posting a parseable body with a decoy field isolates the one variable the
// case is about: WHICH side of `r.Form` the value came from.
func startAFlightWithTheInviteInTheQUERY(t *testing.T, srv *Server, token string) *http.Cookie {
	t.Helper()
	body := url.Values{"decoy": {"a parseable body that does not carry the token"}}
	req := httptest.NewRequest("POST",
		OAuthStartPath+"?"+url.Values{inviteTokenField: {token}}.Encode(),
		strings.NewReader(body.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", "http://"+req.Host)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("the start row answered %d, want 303: %s", rec.Code, rec.Body.String())
	}
	for _, c := range rec.Result().Cookies() {
		if c.Name == oauthFlightCookieName {
			return c
		}
	}
	t.Fatal("the start row set no flight cookie, so no callback can complete")
	return nil
}

// TestTheGitHubStartRowIgnoresAnInvitationTokenInTheQUERYString is the regression guard for
// `handleOAuthStart` reading `r.FormValue`.
//
// # 🔴 WHAT WAS WRONG
//
// `r.FormValue` reads `r.Form`, which for a POST is the parsed BODY **union the URL query**.
// So `POST /sign-in/github?invite=<token>` was accepted and the flight carried the token —
// while the comment directly above the read promised the value "never appears in a URL, a
// referrer or an access log". An invite token is a bearer capability that can CREATE a
// principal (`internal/invite`'s package doc), so the leak is the one `inviteTokenField`
// exists to prevent, into the referrer the next hop receives and into every access log en
// route. `handleJoinPage` records the mirror image of the same `FormValue` distinction.
//
// # 🔴 WHY THE NEGATIVE SUBTEST ALONE WOULD NOT BE COVERAGE
//
// "the token was not carried" is a reassuring ZERO, and a zero is indistinguishable from a
// harness wired to nothing — a broken fixture, a flight that never opened, an `Inviting`
// stub nothing reaches. The second subtest is the POSITIVE CONTROL: the SAME server, the
// SAME callback, the SAME assertion, with the token in the BODY, where the count MUST move
// to 1. Report the pair, never the zero alone.
//
// ⚠ IT IS A REGRESSION GUARD AND NOT AN INVARIANT ONE: the query subtest is RED on
// `r.FormValue` (measured) and GREEN on `r.PostFormValue`.
func TestTheGitHubStartRowIgnoresAnInvitationTokenInTheQUERYString(t *testing.T) {
	// completeAndCountRedemptions runs one whole flow and reports how many times either
	// redemption entry point was reached. `RedeemFor` is the arm a KNOWN user takes, which is
	// the cheapest shape to drive; the assertion is about whether the token reached the
	// callback at all, and both arms read it from the same flight field.
	completeAndCountRedemptions := func(t *testing.T, start func(*testing.T, *Server, string) *http.Cookie) *staticInviting {
		t.Helper()
		cfg, stub, inviting := providerConfigWithInviting(t)
		stub.err = nil
		stub.principal = testIdentity().Principal
		srv, err := New(cfg)
		if err != nil {
			t.Fatal(err)
		}
		cookie := start(t, srv, fixtureInviteToken)
		cb := httptest.NewRequest("GET", OAuthCallbackPath+"?code=fixture-code", nil)
		cb.AddCookie(cookie)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, cb)
		if rec.Code != http.StatusSeeOther {
			t.Fatalf("the callback answered %d, want 303 (a completed sign-in): %s", rec.Code, rec.Body.String())
		}
		return inviting
	}

	t.Run("a token in the query string is not picked up", func(t *testing.T) {
		inviting := completeAndCountRedemptions(t, startAFlightWithTheInviteInTheQUERY)
		if inviting.redeemForCalls != 0 || inviting.redeemCalls != 0 {
			t.Errorf("a token supplied ONLY in `POST %s?%s=…` was redeemed "+
				"(RedeemFor %d, Redeem %d, token %q). The start row is reading `r.FormValue`, which "+
				"unions the URL query into the posted body — so a capability that can create a "+
				"principal is accepted in a URL, and reaches the referrer and every access log en route.",
				OAuthStartPath, inviteTokenField,
				inviting.redeemForCalls, inviting.redeemCalls, inviting.redeemedForToken)
		}
	})

	t.Run("POSITIVE CONTROL: the same token in the body IS picked up", func(t *testing.T) {
		inviting := completeAndCountRedemptions(t, startAFlightCarryingAnInvite)
		if inviting.redeemForCalls != 1 {
			t.Fatalf("RedeemFor ran %d time(s) for a token posted in the BODY, want 1. The zero above is "+
				"then a fact about this harness and not about the handler.", inviting.redeemForCalls)
		}
		if inviting.redeemedForToken != fixtureInviteToken {
			t.Errorf("RedeemFor was given token %q, want %q", inviting.redeemedForToken, fixtureInviteToken)
		}
	})
}

// sessionWasOpenedFor answers whether the response set this surface's session cookie.
//
// ⚠ IT CANNOT READ THE PRINCIPAL OUT OF THE COOKIE — the value is an opaque id and the
// table holds only its digest, which is the design. So it asserts that a session cookie
// was SET, and the principal argument is there to make the call sites read as the claim
// they are making. Stated rather than left to look stronger than it is.
func sessionWasOpenedFor(rec *httptest.ResponseRecorder, _ control.ID) bool {
	for _, c := range rec.Result().Cookies() {
		if c.Name == identity.SessionCookieName && c.Value != "" {
			return true
		}
	}
	return false
}
