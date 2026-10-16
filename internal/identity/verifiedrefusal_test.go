package identity

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ZacxDev/cairn/internal/control"
)

// The lockout-narrowing contract, at the identity package's boundary: WHICH refusals are
// marked as coming from a verified signature, and when the chain lets that mark stand.
// `internal/api`'s lockout_claims_test.go is the seam half — what the server does with it.

// claimsArm is one way a correctly-signed token can still be refused.
type claimsArm struct {
	name string
	edit func(claimSet)
	role string // a CAIRN_OIDC_REQUIRE_ROLE for this arm, "" for none
	sub  string // the `sub` the refusal must carry
}

func verifiedClaimArms() []claimsArm {
	return []claimsArm{
		{name: "exp in the past", edit: func(c claimSet) { c["exp"] = testClock.Add(-time.Hour).Unix() }, sub: testSubject},
		{name: "exp absent", edit: func(c claimSet) { delete(c, "exp") }, sub: testSubject},
		{name: "aud mismatch", edit: func(c claimSet) { c["aud"] = "a-different-application" }, sub: testSubject},
		{name: "iss mismatch", edit: func(c claimSet) { c["iss"] = "https://other-idp.example.test/" }, sub: testSubject},
		{name: "nbf in the future", edit: func(c claimSet) { c["nbf"] = testClock.Add(time.Hour).Unix() }, sub: testSubject},
		{name: "iat in the future", edit: func(c claimSet) { c["iat"] = testClock.Add(time.Hour).Unix() }, sub: testSubject},
		{name: "sub empty", edit: func(c claimSet) { c["sub"] = "" }, sub: ""},
		{name: "role is not the required one", edit: func(c claimSet) { c["role"] = "anon" }, role: "authenticated", sub: testSubject},
		{name: "unknown subject", edit: func(c claimSet) { c["sub"] = testStranger }, sub: testStranger},
	}
}

// TestOnlyARefusalAfterAVerifiedSignatureIsMarkedVerified pins BOTH sides of the line the
// lockout exemption is drawn on: every post-signature refusal carries the mark (and the
// `sub`/`jti` the issuer signed), and every refusal that does not prove possession of the
// issuer's key carries none.
func TestOnlyARefusalAfterAVerifiedSignatureIsMarkedVerified(t *testing.T) {
	good := newECSigner(t, "ec-1")
	keys := keySetOver(t, good)
	build := func(role string) *SupabaseJWT {
		cfg := goodSupabaseConfig(t, keys)
		cfg.RequireRole = role
		b, err := NewSupabaseJWT(cfg)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}

	// Positive control: an unedited token authenticates and carries its token facts.
	ok := defaultClaims()
	ok["jti"] = "jti-positive"
	who, err := build("").AuthenticateToken(good.sign(t, ok, nil))
	if err != nil {
		t.Fatalf("positive control: a good token was refused: %v", err)
	}
	if who.Token == nil || who.Token.Subject != testSubject || who.Token.ID != "jti-positive" {
		t.Fatalf("a verified token must carry its sub and jti for the audit line, got %+v", who.Token)
	}

	t.Run("verified, so marked", func(t *testing.T) {
		for _, arm := range verifiedClaimArms() {
			t.Run(arm.name, func(t *testing.T) {
				c := defaultClaims()
				c["jti"] = "jti-" + arm.name
				arm.edit(c)
				_, err := build(arm.role).AuthenticateToken(good.sign(t, c, nil))
				var v *VerifiedClaimsRefusal
				if !errors.As(err, &v) {
					t.Fatalf("a correctly-signed token refused on %q must be a VerifiedClaimsRefusal, got %T %v", arm.name, err, err)
				}
				if !errors.Is(err, control.ErrNoCredential{}) {
					t.Fatalf("the uniform question must still answer true: %v", err)
				}
				if v.Subject != arm.sub || v.TokenID != "jti-"+arm.name {
					t.Fatalf("the refusal must carry the signed sub/jti, got sub=%q jti=%q", v.Subject, v.TokenID)
				}
			})
		}
	})

	t.Run("the unprovisioned refusal still answers errors.As through the wrapper", func(t *testing.T) {
		c := defaultClaims()
		c["sub"] = testStranger
		_, err := build("").AuthenticateToken(good.sign(t, c, nil))
		var u *UnprovisionedSubject
		if !errors.As(err, &u) || u.Subject != testStranger {
			t.Fatalf("the invite flow reads UnprovisionedSubject through errors.As; the wrapper broke it: %v", err)
		}
		var plain *Refusal
		if !errors.As(err, &plain) {
			t.Fatalf("errors.As(&*Refusal) must still match: %v", err)
		}
	})

	t.Run("not verified, so not marked", func(t *testing.T) {
		impostor := newECSigner(t, "ec-1") // SAME kid, different key: a bad signature
		stranger := newECSigner(t, "ec-unknown")
		valid := good.sign(t, defaultClaims(), nil)
		forgedSig := valid[:len(valid)-4] + "AAAA"
		for _, arm := range []struct{ name, token string }{
			{"a signature by a key the verifier does not hold, under a held kid", impostor.sign(t, defaultClaims(), nil)},
			{"an unknown kid", stranger.sign(t, defaultClaims(), nil)},
			{"a corrupted signature", forgedSig},
			{"alg none", encodeJSON(t, map[string]any{"alg": "none", "typ": "JWT"}) + "." + encodeJSON(t, defaultClaims()) + "."},
			{"an HS256 forgery", encodeJSON(t, map[string]any{"alg": "HS256", "kid": "ec-1"}) + "." + encodeJSON(t, defaultClaims()) + ".c2ln"},
			{"a crit header", good.sign(t, defaultClaims(), map[string]any{"crit": []string{"x"}})},
			{"two parts", "a.b"},
			{"not base64", "!!!.!!!.!!!"},
			{"an EXPIRED token whose signature is ALSO bad", impostor.sign(t, claimSet{"iss": testIssuer, "aud": testAudience, "sub": testSubject, "exp": testClock.Add(-time.Hour).Unix()}, nil)},
		} {
			t.Run(arm.name, func(t *testing.T) {
				_, err := build("").AuthenticateToken(arm.token)
				if err == nil {
					t.Fatal("precondition: the token was accepted")
				}
				var v *VerifiedClaimsRefusal
				if errors.As(err, &v) {
					t.Fatalf("a refusal that proves nothing about the issuer's key was marked verified — it would escape the lockout: %v", err)
				}
			})
		}
	})
}

// TestANonStringJtiReadsAsAbsentRatherThanRefusingTheToken: `jti` was ignored before it was
// read for the audit line, so decoding it must not turn a working token into a refusal.
func TestANonStringJtiReadsAsAbsentRatherThanRefusingTheToken(t *testing.T) {
	good := newECSigner(t, "ec-1")
	b, err := NewSupabaseJWT(goodSupabaseConfig(t, keySetOver(t, good)))
	if err != nil {
		t.Fatal(err)
	}
	for _, jti := range []any{12345, true, []string{"a"}, map[string]any{"k": 1}} {
		c := defaultClaims()
		c["jti"] = jti
		who, err := b.AuthenticateToken(good.sign(t, c, nil))
		if err != nil {
			t.Fatalf("a token with jti=%v was refused: %v", jti, err)
		}
		if who.Token == nil || who.Token.ID != "" {
			t.Fatalf("a non-string jti must read as absent, got %+v", who.Token)
		}
	}
}

// TestTheChainLetsAVerifiedRefusalStandOnlyWhenNothingElseWasPresented.
//
// 🔴 THE HOLE THIS CLOSES: without it, ONE genuine expired token sent as the bearer beside
// a guessed proxy secret, identity header or session cookie would make the chain's whole
// answer a VerifiedClaimsRefusal — and the guess at the second credential would go
// uncounted, forever.
func TestTheChainLetsAVerifiedRefusalStandOnlyWhenNothingElseWasPresented(t *testing.T) {
	authority := newTestAuthority(t)
	good := newECSigner(t, "ec-1")
	machine, err := NewMachineToken(authority)
	if err != nil {
		t.Fatal(err)
	}
	supabase, err := NewSupabaseJWT(goodSupabaseConfig(t, keySetOver(t, good)))
	if err != nil {
		t.Fatal(err)
	}
	cookie, err := NewCookieSession(newMemorySessions(), authority)
	if err != nil {
		t.Fatal(err)
	}
	trusted, err := NewTrustedHeader(goodProxyConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	chain, err := Backends(machine, supabase, cookie, trusted)
	if err != nil {
		t.Fatal(err)
	}

	expired := defaultClaims()
	expired["exp"] = testClock.Add(-time.Hour).Unix()
	bearer := good.sign(t, expired, nil)
	request := func(extra func(*http.Request)) *http.Request {
		r := httptest.NewRequest(http.MethodGet, "/api/v1/recall/quarry-notes", nil)
		r.Header.Set("Authorization", "Bearer "+bearer)
		if extra != nil {
			extra(r)
		}
		return r
	}

	if _, err := chain.Authenticate(request(nil)); !isVerified(err) {
		t.Fatalf("positive control: an expired-but-genuine bearer alone must stand as verified, got %v", err)
	}
	for _, arm := range []struct {
		name  string
		extra func(*http.Request)
	}{
		{"a guessed session cookie", func(r *http.Request) {
			r.AddCookie(&http.Cookie{Name: SessionCookieName, Value: "guessed-session-value"})
		}},
		{"a proxy identity header", func(r *http.Request) { r.Header.Set(testSubjectHeader, testSubject) }},
		{"a guessed proxy secret", func(r *http.Request) { r.Header.Set(DefaultProxySecretHeader, "a-guess") }},
	} {
		t.Run(arm.name, func(t *testing.T) {
			_, err := chain.Authenticate(request(arm.extra))
			if err == nil {
				t.Fatal("precondition: the request authenticated")
			}
			if isVerified(err) {
				t.Fatalf("a second credential was presented beside the verified bearer, and the chain still marked the request verified — the second credential's guess would go uncounted: %v", err)
			}
			if !errors.Is(err, control.ErrNoCredential{}) {
				t.Fatalf("the uniform question must still answer true: %v", err)
			}
		})
	}
}

func isVerified(err error) bool {
	var v *VerifiedClaimsRefusal
	return errors.As(err, &v)
}
