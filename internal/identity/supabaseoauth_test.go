package identity

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/ZacxDev/cairn/internal/control"
)

// The synthetic OAuth world. Every host is `.invalid` or `.test` — reserved by RFC 2606 and
// resolving nowhere — because this repository is public and a fixture URL must not be able to
// become a request to somebody's infrastructure.
const (
	testAuthBase    = testIssuer
	testRedirectURL = "https://notes.example.invalid/sign-in/github/callback"
)

// goodOAuthConfig is a configuration every construction rung ACCEPTS.
func goodOAuthConfig(t *testing.T) SupabaseOAuthConfig {
	t.Helper()
	verifier, err := NewSupabaseJWT(goodSupabaseConfig(t, keySetOver(t, newECSigner(t, "ec-1"))))
	if err != nil {
		t.Fatalf("the fixture verifier did not build: %v", err)
	}
	return SupabaseOAuthConfig{
		Verifier:    verifier,
		AuthBaseURL: testAuthBase,
		RedirectURL: testRedirectURL,
		Now:         fixedNow,
	}
}

// TestEverySupabaseOAuthConstructionRefusalIsReachable — the same discipline the two
// ladders above it use: a positive control, then one broken field per arm, each arm reaching
// a rung every rung above it ACCEPTS, and each naming its own sentinel.
//
// 🔴 A RUNG NO CONFIGURATION CAN REACH IS AN UNREACHABLE GUARD WITH A LIVE-LOOKING MESSAGE,
// which this package names as a defect rather than as spare safety — see
// `NewSupabaseJWT`'s note on the sixth rung it DELETED. So every arm here is built from the
// complete configuration with exactly one field changed.
func TestEverySupabaseOAuthConstructionRefusalIsReachable(t *testing.T) {
	if _, err := NewSupabaseOAuth(goodOAuthConfig(t)); err != nil {
		t.Fatalf("PRECONDITION FAILED: a complete configuration must build, got %v", err)
	}

	for _, arm := range []struct {
		name   string
		break_ func(*SupabaseOAuthConfig)
		want   error
	}{
		{"nothing to verify the exchanged token with",
			func(c *SupabaseOAuthConfig) { c.Verifier = nil }, ErrSupabaseOAuthNoVerifier},
		{"no auth base URL",
			func(c *SupabaseOAuthConfig) { c.AuthBaseURL = "" }, ErrSupabaseOAuthNoAuthURL},
		{"an auth base URL that is only whitespace",
			func(c *SupabaseOAuthConfig) { c.AuthBaseURL = "   " }, ErrSupabaseOAuthNoAuthURL},
		{"a relative auth base URL",
			func(c *SupabaseOAuthConfig) { c.AuthBaseURL = "/auth/v1" }, ErrSupabaseOAuthRelativeURL},
		{"a plaintext auth base URL to a non-loopback host",
			func(c *SupabaseOAuthConfig) { c.AuthBaseURL = "http://idp.example.invalid/auth/v1" },
			ErrSupabaseOAuthRelativeURL},
		{"an auth base URL with a scheme that is neither",
			func(c *SupabaseOAuthConfig) { c.AuthBaseURL = "ftp://idp.example.invalid/auth/v1" },
			ErrSupabaseOAuthRelativeURL},
		{"no redirect URL",
			func(c *SupabaseOAuthConfig) { c.RedirectURL = "" }, ErrSupabaseOAuthNoRedirect},
		{"a redirect URL that is only whitespace",
			func(c *SupabaseOAuthConfig) { c.RedirectURL = "\t " }, ErrSupabaseOAuthNoRedirect},
		{"a relative redirect URL",
			func(c *SupabaseOAuthConfig) { c.RedirectURL = "/sign-in/github/callback" },
			ErrSupabaseOAuthRelativeURL},
		{"a plaintext redirect URL to a non-loopback host",
			func(c *SupabaseOAuthConfig) { c.RedirectURL = "http://notes.example.invalid/sign-in/github/callback" },
			ErrSupabaseOAuthRelativeURL},
	} {
		t.Run(arm.name, func(t *testing.T) {
			cfg := goodOAuthConfig(t)
			arm.break_(&cfg)
			_, err := NewSupabaseOAuth(cfg)
			if !errors.Is(err, arm.want) {
				t.Errorf("want %v, got %v. Each rung must be reachable by a configuration every rung ABOVE "+
					"it accepts, or it is an unreachable guard with a live-looking message.", arm.want, err)
			}
		})
	}

	// 🔴 AND THE LOOPBACK EXCEPTION IS MEASURED, because a rule with an exception nobody
	// exercises is a rule whose exception is a guess. `http` to a loopback host is the local
	// bring-up shape, and it is the ONLY plaintext that builds.
	for _, local := range []string{
		"http://127.0.0.1:9999/auth/v1",
		"http://localhost:9999/auth/v1",
		"http://[::1]:9999/auth/v1",
	} {
		cfg := goodOAuthConfig(t)
		cfg.AuthBaseURL = local
		if _, err := NewSupabaseOAuth(cfg); err != nil {
			t.Errorf("a plaintext auth base at the loopback host %q was refused (%v); a local bring-up has no "+
				"certificate, which is the exception `checkJWKSURL` already makes", local, err)
		}
	}
}

// TestTheAuthorizeURLCarriesAPKCEChallengeAndNoState pins the authorize URL's parameters as
// a SET, and the absence of `state` is part of the pin.
//
// 🔴 A TEST THAT ASSERTED "THE URL CONTAINS code_challenge" WOULD PASS FOR A URL MISSING
// `flow_type`, AND THAT PARAMETER IS WHAT KEEPS THE FLOW SCRIPTLESS. Without it GoTrue
// returns the access token in the URL FRAGMENT, which no server ever receives — the sign-in
// would need script to read `location.hash` and post it back. So the parameters are compared
// as a whole set: an addition fails as loudly as a removal.
//
// 🔴 AND `state` IS ASSERTED ABSENT RATHER THAN LEFT UNMENTIONED. Its absence is a decision
// with a reason — GoTrue does not pass an arbitrary `state` through, so carrying one would
// mean smuggling it in the redirect URL's query and changing the string the operator's
// `GOTRUE_URI_ALLOW_LIST` has to match — and an undeclared absence is indistinguishable from
// somebody having forgotten it. The binding it would buy is the flight cookie plus the PKCE
// verifier, and `internal/ui`'s `TestAFlightIsSingleUseAndBoundToItsBrowser` is what measures
// that.
func TestTheAuthorizeURLCarriesAPKCEChallengeAndNoState(t *testing.T) {
	flow, err := NewSupabaseOAuth(goodOAuthConfig(t))
	if err != nil {
		t.Fatalf("the flow did not build: %v", err)
	}
	const challenge = "a-fixture-code-challenge-value"
	raw := flow.AuthorizeURL(challenge)
	if !strings.HasPrefix(raw, testAuthBase+"/authorize?") {
		t.Fatalf("the authorize URL is %q; it must hang off the configured auth base", raw)
	}
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("the authorize URL does not parse: %v", err)
	}
	want := url.Values{
		"provider":              {SupabaseOAuthProviderGitHub},
		"redirect_to":           {testRedirectURL},
		"code_challenge":        {challenge},
		"code_challenge_method": {"s256"},
		"flow_type":             {"pkce"},
	}
	got := u.Query()
	if len(got) != len(want) {
		t.Errorf("the authorize URL carries %d parameter(s) (%v), want exactly %d (%v). The set is compared "+
			"whole: `flow_type` missing would move the token into the URL fragment, which no server receives.",
			len(got), got, len(want), want)
	}
	for name, values := range want {
		if got.Get(name) != values[0] {
			t.Errorf("%s is %q, want %q", name, got.Get(name), values[0])
		}
	}
	if _, present := got["state"]; present {
		t.Error("the authorize URL carries a `state` parameter. GoTrue does not pass one through to the " +
			"callback, so a state here would have to be smuggled into the redirect URL's query — which changes " +
			"the string the operator's GOTRUE_URI_ALLOW_LIST must match. The browser binding is the flight " +
			"cookie plus the PKCE verifier; see this test's own comment.")
	}
}

// oauthTestServer stands in for GoTrue's token endpoint, recording what reached it.
type oauthTestServer struct {
	// requests counts every call, so a test can assert an exchange happened at all.
	requests int
	// body is the last request body, parsed.
	body map[string]string
	// status and response are what it answers.
	status   int
	response string
}

func (o *oauthTestServer) start(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		o.requests++
		o.body = map[string]string{}
		_ = json.NewDecoder(r.Body).Decode(&o.body)
		// The grant type is part of the contract and is asserted HERE, at the endpoint,
		// because it is in the query string rather than the body.
		if got := r.URL.Query().Get("grant_type"); got != "pkce" {
			t.Errorf("the exchange used grant_type=%q, want pkce", got)
		}
		if r.Method != http.MethodPost {
			t.Errorf("the exchange used %s, want POST", r.Method)
		}
		status := o.status
		if status == 0 {
			status = http.StatusOK
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(o.response))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestTheExchangeVERIFIESWhatTheProviderReturned is the core claim of this type, and it is
// the one a shortcut would silently break.
//
// 🔴 THE HAZARD IS TRUSTING A TOKEN BECAUSE OF THE CHANNEL IT ARRIVED ON. The token endpoint
// answers over TLS from the configured provider, which makes it tempting to read the `sub`
// out of the response and call it a sign-in. This test drives THREE tokens through one
// exchange path: one this world accepts, one signed by a key this world does not hold, and
// one naming a subject this control plane has no user for. Only the first resolves.
//
// ⚠ THE SECOND ARM IS THE ONE THAT WOULD PASS UNDER THE SHORTCUT, and the third is the one
// that would turn every read route into a user-creation endpoint — which is the self-serve
// signup `SupabaseJWT`'s own comment refuses.
func TestTheExchangeVERIFIESWhatTheProviderReturned(t *testing.T) {
	ours := newECSigner(t, "ec-1")
	theirs := newECSigner(t, "ec-1") // same kid, different key: the forgery arm
	keys := keySetOver(t, ours)

	strangerClaims := defaultClaims()
	strangerClaims["sub"] = testStranger

	for _, arm := range []struct {
		name     string
		token    string
		wantUser bool
	}{
		{"a token this world accepts", ours.sign(t, defaultClaims(), nil), true},
		{"a token signed by a key this world does not hold", theirs.sign(t, defaultClaims(), nil), false},
		{"a token naming a subject this control plane has no user for",
			ours.sign(t, strangerClaims, nil), false},
	} {
		t.Run(arm.name, func(t *testing.T) {
			endpoint := &oauthTestServer{response: `{"access_token":"` + arm.token + `","token_type":"bearer"}`}
			httpSrv := endpoint.start(t)

			verifier, err := NewSupabaseJWT(goodSupabaseConfig(t, keys))
			if err != nil {
				t.Fatalf("the verifier did not build: %v", err)
			}
			flow, err := NewSupabaseOAuth(SupabaseOAuthConfig{
				Verifier:    verifier,
				AuthBaseURL: httpSrv.URL,
				RedirectURL: testRedirectURL,
				Now:         fixedNow,
			})
			if err != nil {
				t.Fatalf("the flow did not build: %v", err)
			}

			principal, err := flow.Exchange(context.Background(), "fixture-code", "fixture-verifier")
			if endpoint.requests != 1 {
				t.Fatalf("the token endpoint saw %d request(s), want 1 — with none, this arm measures the "+
					"HTTP client rather than the verification", endpoint.requests)
			}
			// INSTRUMENT CONTROL: the code and the verifier really did reach the provider,
			// so an arm that refuses is refusing at the VERIFICATION and not before it.
			if endpoint.body["auth_code"] != "fixture-code" || endpoint.body["code_verifier"] != "fixture-verifier" {
				t.Errorf("the exchange body was %v; the code and the verifier must both reach the provider",
					endpoint.body)
			}
			if arm.wantUser {
				if err != nil {
					t.Fatalf("a token this world accepts was refused: %v", err)
				}
				if principal.ID != testUserID {
					t.Errorf("the exchange resolved principal %q, want %q", principal.ID, testUserID)
				}
				if principal.Display == "" {
					t.Error("the resolved principal has no display name, so a write could not record an actor")
				}
				return
			}
			if err == nil {
				t.Fatalf("%s was ACCEPTED and resolved %q. A token is trusted because it VERIFIES, never "+
					"because of the channel it arrived on.", arm.name, principal.ID)
			}
			if !errors.Is(err, control.ErrNoCredential{}) {
				t.Errorf("the refusal is %v; every credential outcome here must satisfy "+
					"errors.Is(err, control.ErrNoCredential{}) so a serving path can ask the uniform question", err)
			}
		})
	}
}

// TestTheExchangeRefusesEveryFailureOfTheTOKENENDPOINT covers the failures that are not
// about a credential at all.
//
// 🔴 THEY CARRY A DIFFERENT SENTINEL FROM A REFUSED CREDENTIAL AND THAT DISTINCTION IS
// LOAD-BEARING FOR AN OPERATOR, NOT FOR THE WIRE. The serving path renders one sentence for
// both — a refusal that discriminates is an enumeration API — but "the identity provider did
// not answer" and "that person has no account here" must look different in a log, or the
// first gets debugged as the second.
func TestTheExchangeRefusesEveryFailureOfTheTOKENENDPOINT(t *testing.T) {
	for _, arm := range []struct {
		name     string
		status   int
		response string
		// noBody drives the case where the endpoint is not reachable at all.
		unreachable bool
	}{
		{name: "a refused code", status: http.StatusBadRequest, response: `{"error":"invalid_grant"}`},
		{name: "a server error", status: http.StatusInternalServerError, response: `{}`},
		{name: "a 200 with no access token", status: http.StatusOK, response: `{"token_type":"bearer"}`},
		{name: "a 200 that is not JSON", status: http.StatusOK, response: `<html>a proxy error page</html>`},
		{name: "an unreachable endpoint", unreachable: true},
	} {
		t.Run(arm.name, func(t *testing.T) {
			verifier, err := NewSupabaseJWT(goodSupabaseConfig(t, keySetOver(t, newECSigner(t, "ec-1"))))
			if err != nil {
				t.Fatalf("the verifier did not build: %v", err)
			}
			base := "http://127.0.0.1:1/auth/v1" // port 1: nothing listens, and it is loopback
			if !arm.unreachable {
				endpoint := &oauthTestServer{status: arm.status, response: arm.response}
				base = endpoint.start(t).URL
			}
			flow, err := NewSupabaseOAuth(SupabaseOAuthConfig{
				Verifier: verifier, AuthBaseURL: base, RedirectURL: testRedirectURL, Now: fixedNow,
			})
			if err != nil {
				t.Fatalf("the flow did not build: %v", err)
			}
			_, err = flow.Exchange(context.Background(), "fixture-code", "fixture-verifier")
			if !errors.Is(err, ErrSupabaseOAuthExchange) {
				t.Errorf("want ErrSupabaseOAuthExchange, got %v", err)
			}
		})
	}

	// 🔴 AND AN EMPTY CODE OR AN EMPTY VERIFIER IS REFUSED WITHOUT A NETWORK CALL. A caller
	// that lost the flight must not spend a round trip to learn it, because that round trip
	// is one anybody who can reach the callback can make this process perform.
	endpoint := &oauthTestServer{response: `{"access_token":"x"}`}
	httpSrv := endpoint.start(t)
	verifier, err := NewSupabaseJWT(goodSupabaseConfig(t, keySetOver(t, newECSigner(t, "ec-1"))))
	if err != nil {
		t.Fatalf("the verifier did not build: %v", err)
	}
	flow, err := NewSupabaseOAuth(SupabaseOAuthConfig{
		Verifier: verifier, AuthBaseURL: httpSrv.URL, RedirectURL: testRedirectURL, Now: fixedNow,
	})
	if err != nil {
		t.Fatalf("the flow did not build: %v", err)
	}
	for _, arm := range []struct{ code, pkce string }{{"", "v"}, {"c", ""}, {"", ""}} {
		if _, err := flow.Exchange(context.Background(), arm.code, arm.pkce); !errors.Is(err, ErrSupabaseOAuthExchange) {
			t.Errorf("Exchange(%q, %q) returned %v, want ErrSupabaseOAuthExchange", arm.code, arm.pkce, err)
		}
	}
	if endpoint.requests != 0 {
		t.Errorf("an empty code or verifier reached the network %d time(s); it must be refused locally",
			endpoint.requests)
	}
}

// TestAuthenticateTokenAndAuthenticateAreOneVERIFICATIONPATH pins that the exported
// string-taking resolver is not a second implementation of the backend.
//
// 🔴 A SECOND PATH IS THE HAZARD, NOT A SECOND FUNCTION. `Authenticate` reads a header and
// delegates; if the two ever diverged, a check added to one would be missing from the other
// and the missing one would be on whichever door is newer. This drives the same tokens
// through both and requires the same outcome.
//
// ⚠ IT IS AN INVARIANT GUARD. No defect produced two paths; the refactor that exported the
// core is what made two callable entry points exist at all.
func TestAuthenticateTokenAndAuthenticateAreOneVERIFICATIONPATH(t *testing.T) {
	ours := newECSigner(t, "ec-1")
	theirs := newECSigner(t, "ec-1")
	backend, err := NewSupabaseJWT(goodSupabaseConfig(t, keySetOver(t, ours)))
	if err != nil {
		t.Fatalf("the backend did not build: %v", err)
	}

	strangerClaims := defaultClaims()
	strangerClaims["sub"] = testStranger

	agreed := 0
	for _, arm := range []struct {
		name  string
		token string
	}{
		{"a token this world accepts", ours.sign(t, defaultClaims(), nil)},
		{"a forged token", theirs.sign(t, defaultClaims(), nil)},
		{"a token naming a stranger", ours.sign(t, strangerClaims, nil)},
		{"not a token at all", "neither-a-jwt-nor-a-credential"},
	} {
		viaHeader := httptest.NewRequest("GET", "/", nil)
		viaHeader.Header.Set("Authorization", "Bearer "+arm.token)
		headerID, headerErr := backend.Authenticate(viaHeader)
		stringID, stringErr := backend.AuthenticateToken(arm.token)
		if (headerErr == nil) != (stringErr == nil) {
			t.Errorf("%s: the header path returned %v and the string path %v. They must be ONE verification "+
				"path, or a check added to one is missing from the other.", arm.name, headerErr, stringErr)
			continue
		}
		if headerID.Principal.ID != stringID.Principal.ID {
			t.Errorf("%s: the header path resolved %q and the string path %q",
				arm.name, headerID.Principal.ID, stringID.Principal.ID)
			continue
		}
		agreed++
	}
	if agreed != 4 {
		t.Errorf("%d of 4 arms agreed", agreed)
	}

	// POSITIVE CONTROL: the loop above can see a disagreement, and the ONE place the two
	// legitimately differ is the header read itself — an empty string and a request with no
	// header both refuse, which is the same answer for the same reason.
	if _, err := backend.AuthenticateToken(""); err == nil {
		t.Error("AuthenticateToken(\"\") was accepted; an empty presented token is no token")
	}
	bare := httptest.NewRequest("GET", "/", nil)
	if _, err := backend.Authenticate(bare); err == nil {
		t.Error("a request with no Authorization header was accepted")
	}
}

// TestTheIssuerIsReadableSoTheFlowNeedsNoSecondVariable pins the accessor the one caller
// derives its endpoints from.
//
// ⚠ AN INVARIANT GUARD, AND IT IS HERE BECAUSE THE ALTERNATIVE IT REPLACED IS THE HAZARD:
// a second environment variable naming the same project. Two places to name it is a
// deployment that verifies tokens from one project and starts sign-ins at another, and every
// sign-in would complete at the provider and be refused here with nothing naming the
// disagreement.
func TestTheIssuerIsReadableSoTheFlowNeedsNoSecondVariable(t *testing.T) {
	backend, err := NewSupabaseJWT(goodSupabaseConfig(t, keySetOver(t, newECSigner(t, "ec-1"))))
	if err != nil {
		t.Fatalf("the backend did not build: %v", err)
	}
	if got := backend.Issuer(); got != testIssuer {
		t.Errorf("Issuer() is %q, want the configured %q — the flow's /authorize and /token hang off it",
			got, testIssuer)
	}
}

// TestTheNarrowSupabaseBuilderIsTheSameLEDGERTheChainBuilderUSES pins that `cairn-ui`'s
// builder is not a second reader of `CAIRN_SUPABASE_*`.
//
// 🔴 A SECOND READER IS THE DEFECT THIS FILE'S WHOLE HISTORY IS ABOUT: "is this setting set?"
// had six answers in one file, and which one a setting got was decided by which reader it
// happened to be plumbed through. So this drives the SAME environments through
// `SupabaseBackendFromEnvironment` and `FromEnvironment` and requires them to agree about
// armed-ness and about every refusal.
func TestTheNarrowSupabaseBuilderIsTheSameLEDGERTheChainBuilderUSES(t *testing.T) {
	jwks := "https://notes-idp.example.test/auth/v1/.well-known/jwks.json"
	for _, arm := range []struct {
		name      string
		env       map[string]string
		wantArmed bool
		wantErr   error
	}{
		{"nothing set", map[string]string{}, false, nil},
		{"a complete configuration",
			map[string]string{EnvSupabaseJWKSURL: jwks, EnvSupabaseIssuer: testIssuer}, true, nil},
		{"an absent companion",
			map[string]string{EnvSupabaseJWKSURL: jwks}, false, ErrSupabaseNoIssuer},
		{"a blank that would turn a check off",
			map[string]string{EnvSupabaseJWKSURL: jwks, EnvSupabaseIssuer: testIssuer,
				EnvSupabaseRequireRole: "   "}, false, ErrBlankSetting},
		{"a retired name",
			map[string]string{"CAIRN_SUPABASE_JWT_SECRET": "whatever-a-manifest-still-carries"}, false,
			ErrRetiredSetting},
		{"a negative maximum token age",
			map[string]string{EnvSupabaseJWKSURL: jwks, EnvSupabaseIssuer: testIssuer,
				EnvSupabaseMaxAge: "-1h"}, false, ErrSupabaseMaxAge},
	} {
		t.Run(arm.name, func(t *testing.T) {
			authority := newTestAuthority(t)
			backend, armed, err := SupabaseBackendFromEnvironment(arm.env, authority)
			if arm.wantErr != nil {
				if !errors.Is(err, arm.wantErr) {
					t.Errorf("want %v, got %v", arm.wantErr, err)
				}
				if backend != nil || armed {
					t.Error("a refusal must not also report a built backend or an armed ledger — a " +
						"half-configured backend must not read as an absent one")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected refusal: %v", err)
			}
			if armed != arm.wantArmed {
				t.Errorf("armed is %v, want %v", armed, arm.wantArmed)
			}
			if armed != (backend != nil) {
				t.Errorf("armed is %v and the backend is %v; `armed` must mean exactly \"a backend was built\" "+
					"for this caller", armed, backend != nil)
			}
		})

		// THE AGREEMENT: the chain builder sees the same environment the same way. It is
		// handed a session authority because `ErrSessionBackendWithoutAuthority` refuses an
		// armed ledger without one — that refusal is about the POD's wiring and is not the
		// claim here, so supplying it is what keeps the comparison about the LEDGER.
		t.Run(arm.name+" agrees with FromEnvironment", func(t *testing.T) {
			authority := newTestAuthority(t)
			_, chainSupabase, chainErr := FromEnvironment(arm.env, authority, newTestSessionAuthority(t))
			_, armed, narrowErr := SupabaseBackendFromEnvironment(arm.env, authority)
			if (chainErr == nil) != (narrowErr == nil) {
				// `ErrSessionAuthorityUnread` is the one refusal only the chain builder can
				// make — it is about a session authority nothing reads, which is a question
				// about the whole chain rather than about this ledger.
				if errors.Is(chainErr, ErrSessionAuthorityUnread) && narrowErr == nil {
					return
				}
				t.Errorf("FromEnvironment returned %v and SupabaseBackendFromEnvironment %v for the same "+
					"environment; two readers of one ledger is the defect this file exists against",
					chainErr, narrowErr)
				return
			}
			if chainErr == nil && (chainSupabase != nil) != armed {
				t.Errorf("FromEnvironment built a backend=%v and the narrow builder reported armed=%v",
					chainSupabase != nil, armed)
			}
		})
	}
}
