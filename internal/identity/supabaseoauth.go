package identity

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/ZacxDev/cairn/internal/control"
)

// SupabaseOAuth is the SIGN-IN half of the Supabase integration: it starts a provider
// redirect and exchanges the authorization code the provider hands back for an access
// token, which it then resolves through [SupabaseJWT].
//
// 🔴 IT IS NOT AN `Authenticator` AND MUST NOT BECOME ONE. An `Authenticator` answers "who
// is this REQUEST", is in the chain, and runs on every request; this type runs exactly
// twice per sign-in and makes a NETWORK CALL. Putting a network call in the chain would
// undo the property `SupabaseJWT`'s own comment is built on — "verification is local and
// the hot path makes no network call" — so the two are separate types with separate
// lifetimes, and the only thing they share is the verifier this one borrows to resolve what
// it fetched.
//
// 🔴 AND THE VERIFICATION IS NOT SKIPPED JUST BECAUSE THE TOKEN CAME FROM THE PROVIDER
// OVER TLS. A token fetched from the token endpoint is still checked against the JWKS, the
// issuer, the audience and the role, by the same `AuthenticateToken` a bearer header lands
// on. Trusting a token because of the channel it arrived on is how a second, weaker
// authentication path gets built beside the first.
type SupabaseOAuth struct {
	// verifier resolves the exchanged access token to a principal this control plane
	// already holds. Required: an exchange with nothing to verify the result against
	// would hand a session to anybody the provider vouched for, which is self-serve
	// signup — see `SupabaseJWT`'s own refusal of that.
	verifier *SupabaseJWT
	// authBase is the GoTrue base URL, with no trailing slash — `/authorize` and `/token`
	// hang off it.
	authBase string
	// redirect is the absolute URL the provider sends the browser back to. It is
	// configured rather than derived from the request: see `ErrSupabaseOAuthNoRedirect`.
	redirect string
	// provider is the UPSTREAM social provider GoTrue is asked for (`github`), which is a
	// different thing from `SupabaseJWT.provider` — see [SupabaseOAuthProviderGitHub].
	provider string
	// apiKey, when non-empty, is sent as `apikey` and as a bearer token. Empty is the
	// self-hosted-GoTrue shape. See `SupabaseOAuthConfig.APIKey`.
	apiKey string
	// client is this type's own HTTP client, with its own timeout, for the same reason
	// `KeySet` has one: a package that shares `http.DefaultClient` inherits whatever
	// timeout somebody else set on it, which is usually none.
	client *http.Client
	// now is the clock, injected so a test can pin expiry without sleeping.
	now func() time.Time
}

// SupabaseOAuthProviderGitHub is the one upstream provider this flow asks GoTrue for.
//
// 🔴 A CONSTANT RATHER THAN A SETTING, AND THE REASON IS THAT THE BUTTON SAYS "GITHUB".
// The provider name appears in a rendered label, in the authorize URL and in the operator's
// GitHub OAuth app; making it configurable would let those three disagree, and the failure
// would be a button that sends people to a provider the deployment has no app for. A second
// provider is a second button, which is a change to the page as well as to this line.
//
// ⚠ IT IS NOT `SupabaseJWT.provider`, AND CONFLATING THE TWO IS THE MISREADING TO AVOID.
// That one is `control.User.Provider` — the namespace a `sub` resolves in, which is
// `supabase` for every upstream login — and `DefaultSupabaseProvider`'s comment says why.
// This one is which social login GoTrue should start. A user who signs in with GitHub is
// still a `supabase` user here.
const SupabaseOAuthProviderGitHub = "github"

// The construction refusals. One sentinel each, for the reason `NewSupabaseJWT`'s rungs
// give: a test watches THAT ONE fire rather than "a constructor returned an error".
var (
	// ErrSupabaseOAuthNoVerifier refuses an exchange whose result nothing would check.
	ErrSupabaseOAuthNoVerifier = errors.New("identity: no Supabase verifier was supplied, so an exchanged access token would be trusted because of the channel it arrived on rather than because it verified")
	// ErrSupabaseOAuthNoAuthURL refuses an exchange with nowhere to send the browser.
	ErrSupabaseOAuthNoAuthURL = errors.New("identity: no Supabase auth base URL is configured, so there is no /authorize to start and no /token to exchange at")
	// ErrSupabaseOAuthNoRedirect refuses a flow with no callback URL.
	//
	// 🔴 IT IS CONFIGURED RATHER THAN DERIVED FROM THE REQUEST, AND THAT IS THE SAME
	// RULING `sameOrigin` MAKES ONE PACKAGE UP. A callback URL built from `r.Host` is
	// built from a value the CLIENT chose, so a caller sending `Host: elsewhere.invalid`
	// would make this process ask the provider to send its browser there. The provider's
	// own allow-list is what would refuse that — a defence in somebody else's
	// configuration — and this surface cannot read it. A configured value also cannot
	// disagree with the entry the operator put in `GOTRUE_URI_ALLOW_LIST`, because it is
	// the one they copied it from.
	ErrSupabaseOAuthNoRedirect = errors.New("identity: no Supabase redirect URL is configured, so the provider would have nowhere to send the browser back to")
	// ErrSupabaseOAuthRelativeURL refuses a URL that is not absolute `https`.
	//
	// ⚠ `http` IS PERMITTED FOR A LOOPBACK HOST AND NOWHERE ELSE, which is the same
	// exception `checkJWKSURL` makes and for the same reason: a local bring-up has no
	// certificate, and a non-loopback plaintext OAuth callback puts an authorization code
	// on the wire in clear.
	ErrSupabaseOAuthRelativeURL = errors.New("identity: a Supabase OAuth URL is not an absolute https URL")
)

// SupabaseOAuthConfig is what a deployment declares.
type SupabaseOAuthConfig struct {
	// Verifier resolves the exchanged token. Required.
	Verifier *SupabaseJWT
	// AuthBaseURL is the GoTrue base — for a hosted project,
	// `https://<project-ref>.supabase.co/auth/v1`, which is also its `iss`. Required.
	AuthBaseURL string
	// RedirectURL is the absolute URL of this surface's callback route. Required, and it
	// must also appear in the provider's `GOTRUE_URI_ALLOW_LIST`.
	RedirectURL string
	// APIKey is the project's anonymous key, sent as `apikey` and as a bearer token.
	//
	// ⚠ OPTIONAL, AND THE TWO DEPLOYMENTS IT TELLS APART ARE BOTH REAL. A HOSTED Supabase
	// project is reached through an API gateway that refuses a request with no `apikey` —
	// so without this the token exchange answers 401 and the whole button is inert, which
	// is the "shipped completely inert" failure this repository names. A SELF-HOSTED GoTrue
	// has no such gateway and needs no key. Empty means "send neither header", which is the
	// second shape; it is not a disabled check, because the key is not a credential this
	// flow authenticates ANYBODY with — the access token is.
	APIKey string
	// Client is the HTTP client. nil means one with a bounded timeout.
	Client *http.Client
	// Now is the clock. nil means `time.Now().UTC()`.
	Now func() time.Time
}

// supabaseOAuthTimeout bounds the token exchange.
//
// 🔴 IT IS SHORTER THAN A BROWSER'S PATIENCE ON PURPOSE. This call happens while a person
// is watching a blank tab after a provider redirect; a request that hangs for a minute is a
// sign-in that looks broken and gets retried, and each retry is another code exchange. The
// same bound `jwksFetchTimeout` takes, for the same class of reason.
const supabaseOAuthTimeout = 10 * time.Second

// maxOAuthResponseBytes caps what the token endpoint may return.
//
// 🔴 AN UNBOUNDED `io.ReadAll` OVER A NETWORK BODY IS A MEMORY EXHAUSTION ANYBODY WHO CAN
// ANSWER AS THE PROVIDER CAN DRIVE. `maxJWKSBytes` is the same guard on the same package's
// other network read; a token response is a handful of JWTs, so this is generous.
const maxOAuthResponseBytes = 64 << 10

// NewSupabaseOAuth builds the sign-in flow, or refuses.
//
// 🔴 THE RUNGS ARE ORDERED SO EACH IS REACHABLE BY A CONFIGURATION EVERY RUNG ABOVE IT
// ACCEPTS, the same discipline `NewSupabaseJWT` records:
//
//	0  ErrSupabaseOAuthNoVerifier   nothing would check what the exchange returned
//	1  ErrSupabaseOAuthNoAuthURL    a verifier, and nowhere to send the browser
//	2  ErrSupabaseOAuthRelativeURL  an auth base that is not absolute https (or loopback http)
//	3  ErrSupabaseOAuthNoRedirect   a usable auth base, and no callback URL
//	4  ErrSupabaseOAuthRelativeURL  a callback URL that is not absolute https (or loopback http)
//
// ⚠ RUNGS 2 AND 4 SHARE A SENTINEL AND THE MESSAGE NAMES WHICH URL, because they are one
// rule about two settings rather than two rules. A separate sentinel per setting would be
// two spellings of one decision, which is what this repository refuses in the env ledger
// one file over.
func NewSupabaseOAuth(cfg SupabaseOAuthConfig) (*SupabaseOAuth, error) {
	if cfg.Verifier == nil {
		return nil, ErrSupabaseOAuthNoVerifier
	}
	base := strings.TrimRight(strings.TrimSpace(cfg.AuthBaseURL), "/")
	if base == "" {
		return nil, ErrSupabaseOAuthNoAuthURL
	}
	if err := checkOAuthURL("auth base URL", base); err != nil {
		return nil, err
	}
	redirect := strings.TrimSpace(cfg.RedirectURL)
	if redirect == "" {
		return nil, ErrSupabaseOAuthNoRedirect
	}
	if err := checkOAuthURL("redirect URL", redirect); err != nil {
		return nil, err
	}

	client := cfg.Client
	if client == nil {
		client = &http.Client{Timeout: supabaseOAuthTimeout}
	}
	now := cfg.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &SupabaseOAuth{
		verifier: cfg.Verifier,
		authBase: base,
		redirect: redirect,
		provider: SupabaseOAuthProviderGitHub,
		apiKey:   strings.TrimSpace(cfg.APIKey),
		client:   client,
		now:      now,
	}, nil
}

// checkOAuthURL is the ONE place a configured OAuth URL is judged, so the auth base and the
// callback cannot end up under different rules.
//
// ⚠ IT REUSES `isLoopbackHost` RATHER THAN RE-DERIVING "IS THIS LOCAL", which is the same
// predicate `checkJWKSURL` asks. A second spelling of it is a second answer, and the two
// would disagree about `[::1]` first.
func checkOAuthURL(what, raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("%w: the %s %q does not parse", ErrSupabaseOAuthRelativeURL, what, raw)
	}
	if u.Host == "" {
		return fmt.Errorf("%w: the %s %q has no host, so it is relative", ErrSupabaseOAuthRelativeURL, what, raw)
	}
	switch u.Scheme {
	case "https":
		return nil
	case "http":
		if isLoopbackHost(u.Hostname()) {
			return nil
		}
		return fmt.Errorf("%w: the %s %q is plaintext to a non-loopback host, which would put an authorization code on the wire in clear",
			ErrSupabaseOAuthRelativeURL, what, raw)
	default:
		return fmt.Errorf("%w: the %s %q has scheme %q", ErrSupabaseOAuthRelativeURL, what, raw, u.Scheme)
	}
}

// RedirectURL is the callback URL this flow was configured with. Read by the caller so the
// route it serves and the URL it asks the provider to use cannot be two different strings.
func (o *SupabaseOAuth) RedirectURL() string { return o.redirect }

// AuthorizeURL is where the browser is sent to start the flow.
//
// 🔴 IT CARRIES A PKCE CHALLENGE AND NO `state` PARAMETER, AND THE ABSENCE IS A DECISION
// WITH A REASON RATHER THAN AN OMISSION. GoTrue does not pass an arbitrary `state` through
// to the callback — it manages its own and appends only `code` to the redirect — so a
// `state` here would have to be smuggled in the redirect URL's query, which changes the
// string the operator's `GOTRUE_URI_ALLOW_LIST` has to match. What `state` buys is a
// binding between the callback and the browser that started the flow, and that binding is
// supplied instead by the two things the caller holds: the flight cookie (unguessable,
// `HttpOnly`, single-use, and unreadable to any other origin) and the PKCE verifier behind
// it. An attacker who obtains a code of their own and makes a victim's browser open the
// callback loses twice: with no flight cookie there is nothing to exchange with, and with
// the victim's OWN flight cookie the exchange presents the VICTIM's verifier against the
// ATTACKER's code, which the token endpoint refuses. `internal/ui`'s handlers are where
// that is measured.
//
// ⚠ THE CHALLENGE IS THE CALLER'S, NOT THIS TYPE'S. The verifier must outlive this call
// and reach the exchange, and the only thing that can hold it for that long is whatever
// binds it to the browser — which is a decision about session storage and therefore not
// this package's. See `ui.flights`.
func (o *SupabaseOAuth) AuthorizeURL(challenge string) string {
	q := url.Values{}
	q.Set("provider", o.provider)
	q.Set("redirect_to", o.redirect)
	q.Set("code_challenge", challenge)
	// `s256` lowercase: that is the spelling GoTrue's own PKCE parameter takes, and a
	// mismatched method is refused at the token endpoint rather than at the authorize one,
	// which makes it a failure at the END of the flow.
	q.Set("code_challenge_method", "s256")
	// `flow_type` is what makes GoTrue return a `code` to exchange rather than a token in
	// the URL FRAGMENT. Without it the access token arrives after a `#`, which no server
	// ever receives — the flow would need script to read it, and the CSP forbids inline
	// script. So this parameter is what keeps this flow scriptless.
	q.Set("flow_type", "pkce")
	return o.authBase + "/authorize?" + q.Encode()
}

// ErrSupabaseOAuthExchange is every failure of the code exchange, for a caller that must
// not branch on the reason.
//
// 🔴 IT IS NOT A `control.ErrNoCredential`, AND THAT DISTINCTION IS LOAD-BEARING. A failed
// exchange is an OUTAGE or a bad code — the provider was unreachable, the code was already
// used — while a verified token naming no user is a refusal. The serving path renders the
// same sentence for both (a refusal that discriminates is an enumeration API), but an
// operator reading a log needs "the identity provider did not answer" to look different
// from "that person has no account here". `Exchange` wraps this one for the first and
// returns the verifier's own `Refusal` for the second.
var ErrSupabaseOAuthExchange = errors.New("identity: the Supabase authorization-code exchange failed")

// Exchange turns an authorization code into a principal this control plane holds.
//
// 🔴 TWO STEPS, AND THE SECOND IS THE AUTHENTICATION. The exchange is a network call whose
// answer is a token; `AuthenticateToken` is what decides whether that token names anybody
// here. A caller that stopped after step one would have authenticated a browser against the
// PROVIDER's user table rather than against this control plane's — which is the self-serve
// signup `SupabaseJWT` refuses.
//
// ⚠ THE RETURN IS A `control.Principal` AND NOT AN `Identity`, WHICH IS NARROWER THAN THE
// BACKEND'S AND IS WHAT THE CALLER NEEDS. A sign-in mints a session recording WHO, and the
// authorization is re-resolved from the model on every subsequent request — see
// `Session`'s own comment on why it holds a principal reference rather than a snapshot of
// authority. Returning the `Authorization` too would invite a caller to freeze it.
func (o *SupabaseOAuth) Exchange(ctx context.Context, code, verifier string) (control.Principal, error) {
	if code == "" || verifier == "" {
		return control.Principal{}, fmt.Errorf("%w: the callback carried no code, or the flight no verifier", ErrSupabaseOAuthExchange)
	}
	body, err := json.Marshal(map[string]string{"auth_code": code, "code_verifier": verifier})
	if err != nil {
		return control.Principal{}, fmt.Errorf("%w: %v", ErrSupabaseOAuthExchange, err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		o.authBase+"/token?grant_type=pkce", bytes.NewReader(body))
	if err != nil {
		return control.Principal{}, fmt.Errorf("%w: %v", ErrSupabaseOAuthExchange, err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if o.apiKey != "" {
		// Both headers, which is what a hosted project's gateway expects: `apikey`
		// identifies the project and the bearer form is what the gateway forwards.
		req.Header.Set("apikey", o.apiKey)
		req.Header.Set("Authorization", "Bearer "+o.apiKey)
	}

	resp, err := o.client.Do(req)
	if err != nil {
		// The URL is NOT interpolated: it carries the project reference, and this error
		// reaches an operator's log. The sentinel says which step failed.
		return control.Principal{}, fmt.Errorf("%w: the token endpoint could not be reached", ErrSupabaseOAuthExchange)
	}
	defer resp.Body.Close()
	raw, readErr := io.ReadAll(io.LimitReader(resp.Body, maxOAuthResponseBytes))
	if readErr != nil {
		return control.Principal{}, fmt.Errorf("%w: the token response could not be read", ErrSupabaseOAuthExchange)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		// 🔴 THE BODY IS NOT IN THE ERROR. A provider's refusal body echoes parts of the
		// request back — the code, sometimes the redirect — and this string reaches a log
		// an operator pastes into a ticket. The STATUS is the part that is diagnostic and
		// carries nothing secret.
		return control.Principal{}, fmt.Errorf("%w: the token endpoint answered %d", ErrSupabaseOAuthExchange, resp.StatusCode)
	}

	var decoded struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return control.Principal{}, fmt.Errorf("%w: the token response is not JSON this flow understands", ErrSupabaseOAuthExchange)
	}
	if decoded.AccessToken == "" {
		return control.Principal{}, fmt.Errorf("%w: the token response carried no access token", ErrSupabaseOAuthExchange)
	}

	// 🔴 THE FULL VERIFICATION, NOT A SHORTCUT. The reason is in this type's own comment:
	// arriving over TLS from the provider is a fact about the channel, and the signature,
	// issuer, audience, role and user lookup are facts about the token.
	id, err := o.verifier.AuthenticateToken(decoded.AccessToken)
	if err != nil {
		return control.Principal{}, err
	}
	if !id.Valid() {
		// The same fail-closed shape `Chain.Authenticate` takes on a backend that answered
		// yes without naming anybody: a zero identity with a nil error would mint a session
		// for nobody, which passes every guard that asks only whether sign-in succeeded.
		return control.Principal{}, refuse(SupabaseBackend, "the exchange resolved an identity naming no principal")
	}
	return id.Principal, nil
}
