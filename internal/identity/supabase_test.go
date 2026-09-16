package identity

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ZacxDev/cairn/internal/control"
)

// goodSupabaseConfig is a configuration every construction rung ACCEPTS.
func goodSupabaseConfig(t *testing.T, keys *KeySet) SupabaseConfig {
	t.Helper()
	return SupabaseConfig{
		Authority: newTestAuthority(t),
		Keys:      keys,
		Issuer:    testIssuer,
		Audience:  testAudience,
		Now:       fixedNow,
	}
}

// TestEverySupabaseConstructionRefusalIsReachable — same discipline as the
// trusted-header ladder: a positive control, then one broken field per arm, each naming
// its own sentinel.
func TestEverySupabaseConstructionRefusalIsReachable(t *testing.T) {
	keys := keySetOver(t, newRSASigner(t, "rsa-1", 2048))
	built, err := NewSupabaseJWT(goodSupabaseConfig(t, keys))
	if err != nil {
		t.Fatalf("precondition: a complete configuration must build, got %v", err)
	}
	// 🔴 THE KEY SET IS HELD TWICE AND THE TWO MUST BE ONE OBJECT. `verify.Keys` is the
	// resolver `Verify` reads; `keys` is the same pointer in its concrete type, so the
	// refresh methods need no assertion that could fail. Two fields set from one
	// expression cannot disagree today — this pins that they still cannot, because the
	// divergence would be silent and its shape is a pod refreshing one key set while
	// verifying against another. The same defect `the-machine-token-backend-captures-the-authority`
	// records one package over, at a different seam.
	if built.keys != keys {
		t.Fatal("the concrete key set is not the one the config named")
	}
	if built.verify.Keys != keyResolver(keys) {
		t.Fatal("verify.Keys and keys are two different objects — a refresh would maintain one while Verify read the other")
	}

	for _, arm := range []struct {
		name   string
		break_ func(*SupabaseConfig)
		want   error
	}{
		{"no authority", func(c *SupabaseConfig) { c.Authority = nil }, ErrNoAuthority},
		{"no issuer", func(c *SupabaseConfig) { c.Issuer = "" }, ErrSupabaseNoIssuer},
		{"no audience", func(c *SupabaseConfig) { c.Audience = "" }, ErrSupabaseNoAudience},
		{"nothing to verify against", func(c *SupabaseConfig) { c.Keys = nil }, ErrSupabaseNoKeys},
		{"a leeway wider than MaxLeeway", func(c *SupabaseConfig) { c.Leeway = MaxLeeway + time.Second }, ErrSupabaseLeeway},
		{"a negative leeway", func(c *SupabaseConfig) { c.Leeway = -time.Second }, ErrSupabaseLeeway},
		{
			// A negative MaxAge would be read as zero by `checkClaims`, which means OFF
			// — a configured bound silently not enforced.
			"a negative maximum token age",
			func(c *SupabaseConfig) { c.MaxAge = -time.Second },
			ErrSupabaseMaxAge,
		},
	} {
		t.Run(arm.name, func(t *testing.T) {
			cfg := goodSupabaseConfig(t, keys)
			arm.break_(&cfg)
			if _, err := NewSupabaseJWT(cfg); !errors.Is(err, arm.want) {
				t.Fatalf("the WRONG guard fired.\n  got:  %v\n  want: %v", err, arm.want)
			}
		})
	}
}

// TestAVerifiedSessionResolvesToTheControlPlanesOwnPrincipal.
//
// 🔴 THE POSITIVE CONTROL FOR EVERY REFUSAL IN THIS FILE, AND IT ASSERTS AUTHORITY AND
// NOT MERELY SUCCESS. A backend that authenticated everybody to an EMPTY authorization
// would satisfy "it returned no error"; this checks the principal is the fixture user
// and that the user actually reaches their scope.
func TestAVerifiedSessionResolvesToTheControlPlanesOwnPrincipal(t *testing.T) {
	s := newRSASigner(t, "rsa-1", 2048)
	backend, err := NewSupabaseJWT(goodSupabaseConfig(t, keySetOver(t, s)))
	if err != nil {
		t.Fatal(err)
	}
	who, err := backend.Authenticate(bearer(t, s.sign(t, defaultClaims(), nil)))
	if err != nil {
		t.Fatalf("a correct session must authenticate: %v", err)
	}
	if who.Principal.Kind != control.KindUser {
		t.Fatalf("a session resolves to a USER principal, got %q", who.Principal.Kind)
	}
	if who.Principal.ID != testUserID {
		t.Fatalf("principal id = %q, want %q", who.Principal.ID, testUserID)
	}
	if who.Principal.Display != testEmail {
		t.Fatalf("display = %q, want the control plane's OWN record %q — a display taken from the token would be caller-supplied text in an audit line", who.Principal.Display, testEmail)
	}
	if who.Principal.CredentialID != "" {
		t.Fatalf("a session presented no credential, so CredentialID must be empty, got %q", who.Principal.CredentialID)
	}
	if who.Fingerprint != "" {
		t.Fatalf("a session carries no token fingerprint, got %q", who.Fingerprint)
	}
	if !who.Auth.Allows(control.DerivedID(control.PrefixScope, "quarry-notes"), control.VerbRead) {
		t.Fatal("the resolved principal has no authority — every refusal in this file would be indistinguishable from this succeeding")
	}
	if !who.Valid() {
		t.Fatal("a resolved session must be a valid Identity")
	}
}

// TestAVerifiedTokenForAnUNKNOWNUserIsRefusedRatherThanProvisioned.
//
// 🔴 THE IdP VOUCHES FOR WHO SOMEBODY IS; IT SAYS NOTHING ABOUT WHETHER THIS INSTANCE HAS
// AN ACCOUNT FOR THEM. Just-in-time provisioning here would make every read route a
// user-creation endpoint for anybody with an account at the provider — which is
// self-serve signup, and it is P6's, with quotas and abuse handling attached.
func TestAVerifiedTokenForAnUnknownUserIsRefusedRatherThanProvisioned(t *testing.T) {
	s := newRSASigner(t, "rsa-1", 2048)
	authority := newTestAuthority(t)
	cfg := goodSupabaseConfig(t, keySetOver(t, s))
	cfg.Authority = authority
	backend, err := NewSupabaseJWT(cfg)
	if err != nil {
		t.Fatal(err)
	}

	before := len(authority.Model().Users)
	claims := defaultClaims()
	claims["sub"] = testStranger
	// PRECONDITION: this token VERIFIES. The refusal must come from the user lookup, not
	// from the signature — otherwise the arm measures the verifier again.
	if _, err := Verify(s.sign(t, claims, nil), backend.verify); err != nil {
		t.Fatalf("precondition: the stranger's token must verify, got %v", err)
	}

	if _, err := backend.Authenticate(bearer(t, s.sign(t, claims, nil))); err == nil {
		t.Fatal("a token for a subject this control plane has never heard of authenticated")
	} else if !strings.Contains(err.Error(), "holds no user for") {
		t.Fatalf("refused for the wrong reason: %v", err)
	}
	if after := len(authority.Model().Users); after != before {
		t.Fatalf("the control plane gained a user (%d -> %d): the read path provisioned an account, which is signup and is P6's", before, after)
	}
}

// TestTheProviderNamespaceIsPartOfTheLookup.
//
// 🔴 A SUBJECT IS ONLY MEANINGFUL INSIDE ITS PROVIDER'S NAMESPACE. Two identity
// providers can issue the same subject string, so a lookup keyed on the subject alone
// would let one provider's user become another's.
func TestTheProviderNamespaceIsPartOfTheLookup(t *testing.T) {
	s := newRSASigner(t, "rsa-1", 2048)
	cfg := goodSupabaseConfig(t, keySetOver(t, s))
	cfg.Provider = "some-other-idp"
	backend, err := NewSupabaseJWT(cfg)
	if err != nil {
		t.Fatal(err)
	}
	// The SAME subject the fixture user holds, under a different provider.
	if _, err := backend.Authenticate(bearer(t, s.sign(t, defaultClaims(), nil))); err == nil {
		t.Fatal("a subject matched across provider namespaces — one IdP's user can become another's")
	}
}

// TestARequiredRoleClaimIsEnforced. Supabase issues `anon` to a session that has not
// signed in, so a deployment that cares can refuse one.
func TestARequiredRoleClaimIsEnforced(t *testing.T) {
	s := newRSASigner(t, "rsa-1", 2048)
	cfg := goodSupabaseConfig(t, keySetOver(t, s))
	cfg.RequireRole = "authenticated"
	backend, err := NewSupabaseJWT(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := backend.Authenticate(bearer(t, s.sign(t, defaultClaims(), nil))); err != nil {
		t.Fatalf("precondition: the default fixture carries role=authenticated and must pass, got %v", err)
	}
	anon := defaultClaims()
	anon["role"] = "anon"
	if _, err := backend.Authenticate(bearer(t, s.sign(t, anon, nil))); err == nil {
		t.Fatal("an `anon` session authenticated against a deployment that requires `authenticated`")
	}
}

// TestNoBearerTokenIsRefusedWithoutTouchingTheVerifier — the cheapest rung, and the one
// every unauthenticated request takes.
func TestNoBearerTokenIsRefusedWithoutTouchingTheVerifier(t *testing.T) {
	backend, err := NewSupabaseJWT(goodSupabaseConfig(t, keySetOver(t, newRSASigner(t, "rsa-1", 2048))))
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodGet, "/api/v1/recall/quarry-notes", nil)
	_, err = backend.Authenticate(r)
	if err == nil {
		t.Fatal("a request with no Authorization header authenticated")
	}
	if !errors.Is(err, control.ErrNoCredential{}) {
		t.Fatalf("the refusal must be classifiable as the uniform no-credential error, got %v", err)
	}
	if !strings.Contains(err.Error(), "no bearer token") {
		t.Fatalf("refused for the wrong reason: %v", err)
	}
}

// TestAnAuthorityOutageDoesNotStopAnAlreadyIssuedSession — the CONTROL-PLANE half of the
// outage claim, which is a DIFFERENT dependency from the identity provider's.
//
// 🔴 TWO DEPENDENCIES, TWO CLAIMS, AND MEASURING ONE SAYS NOTHING ABOUT THE OTHER.
// `jwks_test.go` kills the identity provider, which is what verifies the SIGNATURE;
// this kills the authority behind the materialized cache, which is what resolves the
// SUBJECT to a principal. They fail in different places. Measured with a power switch
// rather than reasoned about, because `control.Cache`'s real `FileStore` cannot produce
// this failure once it has loaded.
func TestAnAuthorityOutageDoesNotStopAnAlreadyIssuedSession(t *testing.T) {
	s := newRSASigner(t, "rsa-1", 2048)
	src := &switchableSource{inner: staticSource{fixtureModel(t)}}
	authority := control.NewCache(src, control.CacheOptions{Now: fixedNow})
	if err := authority.Refresh(context.Background()); err != nil {
		t.Fatalf("materializing: %v", err)
	}

	cfg := goodSupabaseConfig(t, keySetOver(t, s))
	cfg.Authority = authority
	backend, err := NewSupabaseJWT(cfg)
	if err != nil {
		t.Fatal(err)
	}
	// PRECONDITION: it works while the authority is up.
	if _, err := backend.Authenticate(bearer(t, s.sign(t, defaultClaims(), nil))); err != nil {
		t.Fatalf("precondition: %v", err)
	}

	// UNPLUG IT.
	src.dead.Store(true)
	if err := authority.Refresh(context.Background()); err == nil {
		t.Fatal("precondition: a refresh against a dead authority reported success, so the switch is not wired")
	}
	if got := authority.Staleness(); !got.Failing {
		t.Fatal("precondition: the cache reports itself healthy while its authority is dead")
	}

	who, err := backend.Authenticate(bearer(t, s.sign(t, defaultClaims(), nil)))
	if err != nil {
		t.Fatalf("an already-issued session stopped resolving when the AUTHORITY died: %v — reads must survive this", err)
	}
	if !who.Auth.Allows(control.DerivedID(control.PrefixScope, "quarry-notes"), control.VerbRead) {
		t.Fatal("the session resolved to an EMPTY authority during the outage, which is a read outage wearing a 200")
	}
}

// switchableSource is the power switch: a `control.Source` whose `Model` can be made to
// fail at any instant.
type switchableSource struct {
	inner control.Source
	dead  atomic.Bool
}

func (s *switchableSource) Model(ctx context.Context) (control.Model, error) {
	if s.dead.Load() {
		return control.Model{}, constError("the authority is unplugged")
	}
	return s.inner.Model(ctx)
}

func bearer(t *testing.T, token string) *http.Request {
	t.Helper()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/recall/quarry-notes", nil)
	r.Header.Set("Authorization", "Bearer "+token)
	return r
}
