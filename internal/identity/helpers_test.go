package identity

// Test fixtures for the identity backends.
//
// 🔴 EVERY KEY AND EVERY SECRET HERE IS GENERATED AT TEST TIME, NEVER PASTED. This
// repository is public and was extracted from a private one; a real key in a fixture is
// a published key, and a key that LOOKS real is worse, because nobody can tell. The
// generation is also what makes the algorithm-confusion cases honest: the "attacker"
// arms below sign with material this process minted, so the test measures the verifier
// rather than a recorded string.
//
// 🔴 AND EVERY NAME IS SYNTHETIC. Users, projects, scopes, issuers and hostnames are
// invented; `example.test` and `example.invalid` are reserved by RFC 2606 and resolve
// nowhere, so a fixture URL cannot become a request to somebody's infrastructure. Dates
// are year 2000, which is what `tests/leakscan.py` allows and what makes a fixture date
// unambiguous.

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"testing"
	"time"

	"github.com/ZacxDev/cairn/internal/control"
)

// The synthetic world every backend test resolves against.
const (
	testIssuer   = "https://notes-idp.example.test/auth/v1"
	testAudience = "authenticated"
	testProvider = "supabase"
	// testSubject is the provider's own id for the one user this world holds.
	testSubject = "00000000-0000-4000-8000-000000000001"
	// testStranger is a subject the IdP would happily vouch for and this control
	// plane has never heard of.
	testStranger = "00000000-0000-4000-8000-0000000000ff"
	testEmail    = "rowan@notes.example.test"
)

// testClock is the instant every claim is measured against. Year 2000, per the repo's
// fixture rule.
var testClock = time.Date(2000, 6, 1, 12, 0, 0, 0, time.UTC)

func fixedNow() time.Time { return testClock }

// staticSource is a `control.Source` over one Model. The read half only: nothing in
// these tests writes to an authority.
type staticSource struct{ m control.Model }

func (s staticSource) Model(context.Context) (control.Model, error) { return s.m, nil }

type constError string

func (e constError) Error() string { return string(e) }

// newTestAuthority builds a materialized cache holding ONE user, one project they own
// and one scope in it, so a resolved principal has real authority rather than an empty
// one. A test that asserted only "it authenticated" would pass against a resolver that
// grants nothing — the shape `internal/control/README.md` records as a matrix in which
// every cell refuses.
func newTestAuthority(t *testing.T) *control.Cache {
	t.Helper()
	c := control.NewCache(staticSource{fixtureModel(t)}, control.CacheOptions{Now: fixedNow})
	if err := c.Refresh(context.Background()); err != nil {
		t.Fatalf("materializing the fixture world: %v", err)
	}
	return c
}

// fixtureModel is the world itself, separate from the cache over it, so a test that
// needs its own cache — one with a power switch under it — builds from the same world
// rather than a second description of it.
func fixtureModel(t *testing.T) control.Model {
	t.Helper()
	at := testClock
	usr := control.DerivedID(control.PrefixUser, "rowan")
	prj := control.DerivedID(control.PrefixProject, "quarry")
	scp := control.DerivedID(control.PrefixScope, "quarry-notes")
	m, err := control.Replay([]control.Event{
		{Kind: control.EventUserCreated, At: at, UserID: usr, Provider: testProvider, Subject: testSubject, Email: testEmail},
		{Kind: control.EventProjectCreated, At: at, ProjectID: prj, Name: "quarry", UserID: usr},
		{Kind: control.EventMemberSet, At: at, ProjectID: prj, UserID: usr, Role: control.RoleOwner},
		{Kind: control.EventScopeCreated, At: at, ScopeID: scp, DisplayName: "quarry-notes", ProjectID: prj},
	})
	if err != nil {
		t.Fatalf("building the fixture world: %v", err)
	}
	return m
}

// testUserID is the id `newTestAuthority`'s user carries, spelled once.
var testUserID = control.DerivedID(control.PrefixUser, "rowan")

// --- signing ------------------------------------------------------------------------

// signer mints tokens for one key. The tests hold one per algorithm.
type signer struct {
	alg     Alg
	kid     string
	rsa     *rsa.PrivateKey
	ec      *ecdsa.PrivateKey
	hmacKey []byte
}

func newRSASigner(t *testing.T, kid string, bits int) *signer {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, bits)
	if err != nil {
		t.Fatalf("generating an RSA key: %v", err)
	}
	return &signer{alg: AlgRS256, kid: kid, rsa: key}
}

func newECSigner(t *testing.T, kid string) *signer {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generating an EC key: %v", err)
	}
	return &signer{alg: AlgES256, kid: kid, ec: key}
}

func newHMACSigner(secret []byte) *signer {
	return &signer{alg: AlgHS256, hmacKey: secret}
}

// claimSet is the payload a test mints. A map rather than `Claims` so a test can omit a
// field entirely — which is the only way to build the no-`exp` case, and `Claims` with
// its typed zero values cannot express it.
type claimSet map[string]any

// defaultClaims is a token this world accepts.
func defaultClaims() claimSet {
	return claimSet{
		"iss":  testIssuer,
		"aud":  testAudience,
		"sub":  testSubject,
		"exp":  testClock.Add(time.Hour).Unix(),
		"iat":  testClock.Add(-time.Minute).Unix(),
		"role": "authenticated",
	}
}

// sign produces a compact JWS. `headerOverride` replaces fields in the protected
// header, which is what the forgery arms need.
func (s *signer) sign(t *testing.T, claims claimSet, headerOverride map[string]any) string {
	t.Helper()
	header := map[string]any{"alg": string(s.alg), "typ": "JWT"}
	if s.kid != "" {
		header["kid"] = s.kid
	}
	for k, v := range headerOverride {
		if v == nil {
			delete(header, k)
			continue
		}
		header[k] = v
	}
	input := encodeJSON(t, header) + "." + encodeJSON(t, claims)
	return input + "." + base64.RawURLEncoding.EncodeToString(s.rawSign(t, []byte(input)))
}

// rawSign is the primitive, split out so `signWithKeyOf` can sign one algorithm's bytes
// with another algorithm's key material — the confusion arm.
func (s *signer) rawSign(t *testing.T, input []byte) []byte {
	t.Helper()
	digest := sha256.Sum256(input)
	switch s.alg {
	case AlgRS256:
		out, err := rsa.SignPKCS1v15(rand.Reader, s.rsa, crypto.SHA256, digest[:])
		if err != nil {
			t.Fatalf("RSA sign: %v", err)
		}
		return out
	case AlgES256:
		r, sv, err := ecdsa.Sign(rand.Reader, s.ec, digest[:])
		if err != nil {
			t.Fatalf("ECDSA sign: %v", err)
		}
		// RFC 7518 §3.4: fixed-width `r || s`, left-padded to the field size.
		out := make([]byte, 64)
		r.FillBytes(out[:32])
		sv.FillBytes(out[32:])
		return out
	case AlgHS256:
		mac := hmac.New(sha256.New, s.hmacKey)
		mac.Write(input)
		return mac.Sum(nil)
	default:
		t.Fatalf("no signer for %s", s.alg)
		return nil
	}
}

// publicJWK renders this signer's public half as a JWK entry.
func (s *signer) publicJWK(t *testing.T) map[string]any {
	t.Helper()
	switch s.alg {
	case AlgRS256:
		return map[string]any{
			"kty": "RSA", "kid": s.kid, "use": "sig", "alg": "RS256",
			"n": base64.RawURLEncoding.EncodeToString(s.rsa.N.Bytes()),
			"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(s.rsa.E)).Bytes()),
		}
	case AlgES256:
		x := make([]byte, 32)
		y := make([]byte, 32)
		s.ec.X.FillBytes(x)
		s.ec.Y.FillBytes(y)
		return map[string]any{
			"kty": "EC", "kid": s.kid, "use": "sig", "alg": "ES256", "crv": "P-256",
			"x": base64.RawURLEncoding.EncodeToString(x),
			"y": base64.RawURLEncoding.EncodeToString(y),
		}
	default:
		t.Fatalf("no JWK for %s", s.alg)
		return nil
	}
}

// rsaPublicDER is the signer's public key in the form an attacker would use as an HMAC
// secret in the algorithm-confusion attack: the exact bytes the JWKS publishes.
func (s *signer) rsaModulusBytes() []byte { return s.rsa.N.Bytes() }

func encodeJSON(t *testing.T, value any) string {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("encoding a fixture: %v", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw)
}

func jwksDocumentOf(t *testing.T, entries ...map[string]any) []byte {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"keys": entries})
	if err != nil {
		t.Fatalf("encoding a JWKS fixture: %v", err)
	}
	return raw
}
