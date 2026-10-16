package api

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ZacxDev/cairn/internal/control"
	"github.com/ZacxDev/cairn/internal/identity"
	"github.com/ZacxDev/cairn/internal/netid"
)

// THE LOCKOUT NARROWING, AT THE SEAM: a real server, a real `identity.SupabaseJWT` over a
// JWKS served on loopback, a real `netid.RateLimiter` at five failures — and every request
// from ONE client address, which is the deployment shape the change exists for (many
// callers behind one egress address, all holding tokens one issuer signed).
//
// 🔴 RED AT THE BASE FOR THE RIGHT REASON: before this change every refusal reached
// `countFailure`, so the sixth expired-but-genuine token from one address answered
// `locked-out` and the good token after ten of them was refused. The positive control
// (bad signatures still lock the address out) is in the SAME harness and the SAME
// address, so a server that simply stopped counting cannot pass both.

const (
	lockIssuer   = "https://issuer.example.test/"
	lockAudience = "cairn-test-audience"
	lockProvider = "test-idp"
	lockSubject  = "member-0001"
	lockKid      = "key-1"
)

var lockClock = time.Date(2000, 1, 5, 0, 0, 0, 0, time.UTC)

type lockSigner struct {
	kid string
	key *ecdsa.PrivateKey
}

func newLockSigner(t *testing.T, kid string) *lockSigner {
	t.Helper()
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return &lockSigner{kid: kid, key: k}
}

func b64(raw []byte) string { return base64.RawURLEncoding.EncodeToString(raw) }

func (s *lockSigner) sign(t *testing.T, claims map[string]any) string {
	t.Helper()
	h, _ := json.Marshal(map[string]any{"alg": "ES256", "typ": "JWT", "kid": s.kid})
	p, err := json.Marshal(claims)
	if err != nil {
		t.Fatal(err)
	}
	input := b64(h) + "." + b64(p)
	digest := sha256.Sum256([]byte(input))
	r, sv, err := ecdsa.Sign(rand.Reader, s.key, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	sig := make([]byte, 64)
	r.FillBytes(sig[:32])
	sv.FillBytes(sig[32:])
	return input + "." + b64(sig)
}

func (s *lockSigner) jwk() map[string]any {
	pad := func(n *big.Int) string { out := make([]byte, 32); n.FillBytes(out); return b64(out) }
	return map[string]any{"kty": "EC", "crv": "P-256", "kid": s.kid, "alg": "ES256", "use": "sig",
		"x": pad(s.key.X), "y": pad(s.key.Y)}
}

func goodLockClaims() map[string]any {
	return map[string]any{
		"iss": lockIssuer, "aud": lockAudience, "sub": lockSubject, "jti": "jti-good",
		"exp": lockClock.Add(time.Hour).Unix(), "iat": lockClock.Add(-time.Minute).Unix(),
	}
}

type lockSource struct{ m control.Model }

func (s lockSource) Model(context.Context) (control.Model, error) { return s.m, nil }

// lockHarness is the matrix harness plus a JWT backend, a five-failure limiter and an
// audit capture.
type lockHarness struct {
	*matrixHarness
	signer *lockSigner
	lines  []string
}

func newLockHarness(t *testing.T, requireRole ...string) *lockHarness {
	t.Helper()
	h := &lockHarness{matrixHarness: newMatrixHarness(t), signer: newLockSigner(t, lockKid)}
	h.srv.Audit = func(line string) { h.lines = append(h.lines, line) }
	h.srv.Limiter = netid.NewRateLimiter(5, time.Minute, 15*time.Minute)

	jwks, _ := json.Marshal(map[string]any{"keys": []any{h.signer.jwk()}})
	idp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(jwks)
	}))
	t.Cleanup(idp.Close)
	keys, err := identity.NewKeySet(identity.JWKSOptions{URL: idp.URL + "/jwks"})
	if err != nil {
		t.Fatal(err)
	}
	if err := keys.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}

	usr := control.DerivedID(control.PrefixUser, "member")
	prj := control.DerivedID(control.PrefixProject, "lockout")
	scp := control.DerivedID(control.PrefixScope, "alpha-notes")
	m, err := control.Replay([]control.Event{
		{Kind: control.EventUserCreated, At: lockClock, UserID: usr, Provider: lockProvider, Subject: lockSubject},
		{Kind: control.EventProjectCreated, At: lockClock, ProjectID: prj, Name: "lockout", UserID: usr},
		{Kind: control.EventMemberSet, At: lockClock, ProjectID: prj, UserID: usr, Role: control.RoleOwner},
		{Kind: control.EventScopeCreated, At: lockClock, ScopeID: scp, DisplayName: "alpha-notes", ProjectID: prj},
	})
	if err != nil {
		t.Fatal(err)
	}
	sessions := control.NewCache(lockSource{m}, control.CacheOptions{Now: func() time.Time { return lockClock }})
	if err := sessions.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	jwt, err := identity.NewSupabaseJWT(identity.SupabaseConfig{
		Authority: sessions, Keys: keys, Issuer: lockIssuer, Audience: lockAudience,
		Provider: lockProvider, Now: func() time.Time { return lockClock },
	})
	if len(requireRole) > 0 {
		jwt, err = identity.NewSupabaseJWT(identity.SupabaseConfig{
			Authority: sessions, Keys: keys, Issuer: lockIssuer, Audience: lockAudience,
			Provider: lockProvider, RequireRole: requireRole[0], Now: func() time.Time { return lockClock },
		})
	}
	if err != nil {
		t.Fatal(err)
	}
	machine, err := identity.NewMachineToken(h.srv.AuthorityView())
	if err != nil {
		t.Fatal(err)
	}
	chain, err := identity.Backends(machine, jwt, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.srv.UseAuthenticator(chain); err != nil {
		t.Fatal(err)
	}
	return h
}

func (h *lockHarness) read(t *testing.T, token string) (int, string) {
	t.Helper()
	got := h.do(t, "GET", "/api/v1/recall/alpha-notes", token, nil, "")
	return got.status, h.lastStatus(t)
}

func (h *lockHarness) lastLine(t *testing.T) string {
	t.Helper()
	if len(h.lines) == 0 {
		t.Fatal("no audit line was written")
	}
	return h.lines[len(h.lines)-1]
}

func (h *lockHarness) lastStatus(t *testing.T) string {
	t.Helper()
	line := h.lastLine(t)
	i := strings.Index(line, " status=")
	rest := line[i+len(" status="):]
	if j := strings.IndexByte(rest, ' '); j >= 0 {
		rest = rest[:j]
	}
	return rest
}

// TestASignedTokenFailingAClaimCheckDoesNotLockTheClientOut — measured at THREE claim-time
// checks (an expired token, a wrong audience, a wrong issuer), ten of each from one
// address, and then the good token from that same address must still be served.
func TestASignedTokenFailingAClaimCheckDoesNotLockTheClientOut(t *testing.T) {
	for _, arm := range []struct {
		name string
		edit func(map[string]any)
	}{
		{"expired", func(c map[string]any) { c["exp"] = lockClock.Add(-time.Hour).Unix() }},
		{"audience mismatch", func(c map[string]any) { c["aud"] = "a-different-application" }},
		{"issuer mismatch", func(c map[string]any) { c["iss"] = "https://other-issuer.example.test/" }},
	} {
		t.Run(arm.name, func(t *testing.T) {
			h := newLockHarness(t)
			c := goodLockClaims()
			arm.edit(c)
			refused := h.signer.sign(t, c)
			for i := 1; i <= 10; i++ {
				code, status := h.read(t, refused)
				if code != 401 || status != StatusClaimsRefused {
					t.Fatalf("request %d: got %d status=%s, want the uniform 401 audited %s — a signed token failing a claim check must not count toward the lockout",
						i, code, status, StatusClaimsRefused)
				}
			}
			if code, status := h.read(t, h.signer.sign(t, goodLockClaims())); code != 200 {
				t.Fatalf("after ten claims-refused tokens the good token from the same address got %d status=%s — the address was locked out by tokens its issuer signed", code, status)
			}
		})
	}
}

// TestBadCredentialsStillLockTheClientOut is the POSITIVE CONTROL and a regression guard
// in its own right: every refusal that does not prove possession of the issuer's key is
// counted exactly as before. Five from one address start the lockout, and the good token
// after them is refused `locked-out`.
func TestBadCredentialsStillLockTheClientOut(t *testing.T) {
	for _, arm := range []struct {
		name  string
		token func(*testing.T, *lockHarness) string
	}{
		{"a bad signature under a held kid", func(t *testing.T, h *lockHarness) string {
			return newLockSigner(t, lockKid).sign(t, goodLockClaims())
		}},
		{"an unknown kid", func(t *testing.T, h *lockHarness) string {
			return newLockSigner(t, "key-unknown").sign(t, goodLockClaims())
		}},
		{"an expired token with a bad signature", func(t *testing.T, h *lockHarness) string {
			c := goodLockClaims()
			c["exp"] = lockClock.Add(-time.Hour).Unix()
			return newLockSigner(t, lockKid).sign(t, c)
		}},
		{"a malformed token", func(*testing.T, *lockHarness) string { return "not.a.jws" }},
		{"a wrong machine token", func(*testing.T, *lockHarness) string { return "a-wrong-machine-token-value" }},
	} {
		t.Run(arm.name, func(t *testing.T) {
			h := newLockHarness(t)
			bad := arm.token(t, h)
			for i := 1; i <= 5; i++ {
				code, status := h.read(t, bad)
				want := "unauthorized"
				if i == 5 {
					want = "lockout-triggered"
				}
				if code != 401 || status != want {
					t.Fatalf("request %d: got %d status=%s, want 401 status=%s", i, code, status, want)
				}
			}
			if code, status := h.read(t, h.signer.sign(t, goodLockClaims())); code != 401 || status != "locked-out" {
				t.Fatalf("after five bad credentials the address must be locked out; the good token got %d status=%s", code, status)
			}
		})
	}
}

// TestClaimsRefusalsNeitherAddToNorForgiveTheFailureCount pins the mixed arm: a
// claims-refused token records no failure. Four bad signatures, then ten expired-but-genuine
// tokens, then one more bad signature — that fifth bad signature is the one that starts the
// lockout. Counting the expired tokens would lock on the first of them. ⚠ The "forgive" half
// is pinned too, but today it cannot fail on its own: `netid.RateLimiter.RecordSuccess` is a
// no-op, so even a success forgives nothing. It is kept so that a limiter which starts
// forgiving on success cannot also start forgiving on a claims refusal unnoticed.
func TestClaimsRefusalsNeitherAddToNorForgiveTheFailureCount(t *testing.T) {
	h := newLockHarness(t)
	bad := newLockSigner(t, lockKid).sign(t, goodLockClaims())
	c := goodLockClaims()
	c["exp"] = lockClock.Add(-time.Hour).Unix()
	expired := h.signer.sign(t, c)
	for i := 1; i <= 4; i++ {
		if _, status := h.read(t, bad); status != "unauthorized" {
			t.Fatalf("bad signature %d: status=%s", i, status)
		}
	}
	for i := 1; i <= 10; i++ {
		if _, status := h.read(t, expired); status != StatusClaimsRefused {
			t.Fatalf("expired token %d after four bad signatures: status=%s, want %s", i, status, StatusClaimsRefused)
		}
	}
	if _, status := h.read(t, bad); status != "lockout-triggered" {
		t.Fatalf("the fifth bad signature must start the lockout — claims refusals neither add nor forgive — got status=%s", status)
	}
}

// TestASignedTokenIsAuditedWithItsSubAndJti pins the two new audit fields: present
// on a verified token's line (authenticated or claims-refused), ABSENT from every other
// line (which keeps a machine-token line byte-identical to the oracle's for
// `tests/dualrun/`), and unable to forge a field or a line.
func TestASignedTokenIsAuditedWithItsSubAndJti(t *testing.T) {
	h := newLockHarness(t)

	if code, _ := h.read(t, h.signer.sign(t, goodLockClaims())); code != 200 {
		t.Fatalf("precondition: the good token got %d", code)
	}
	if line := h.lastLine(t); !strings.HasSuffix(line, " status=recalled sub=member-0001 jti=jti-good") {
		t.Fatalf("an authenticated signed token's line must end `sub=… jti=…`:\n  %s", line)
	}

	c := goodLockClaims()
	c["exp"] = lockClock.Add(-time.Hour).Unix()
	c["jti"] = "jti-expired"
	h.read(t, h.signer.sign(t, c))
	if line := h.lastLine(t); !strings.HasSuffix(line, " auth=fail result=401 status=claims-refused sub=member-0001 jti=jti-expired") {
		t.Fatalf("a claims-refused line must carry the signed sub and jti:\n  %s", line)
	}

	noJti := goodLockClaims()
	delete(noJti, "jti")
	h.read(t, h.signer.sign(t, noJti))
	if line := h.lastLine(t); !strings.HasSuffix(line, " jti=-") {
		t.Fatalf("an absent jti must render `-`:\n  %s", line)
	}

	h.read(t, matrixWideToken)
	if line := h.lastLine(t); strings.Contains(line, " sub=") || strings.Contains(line, " jti=") {
		t.Fatalf("a machine-token line must be unchanged — no sub/jti fields:\n  %s", line)
	}
	h.read(t, newLockSigner(t, lockKid).sign(t, goodLockClaims()))
	if line := h.lastLine(t); strings.Contains(line, " sub=") {
		t.Fatalf("a token whose signature did NOT verify must not get sub= — its claims are unauthenticated bytes:\n  %s", line)
	}

	// Forgery: a sub and jti carrying a space, a newline and `=` stay ONE field each on ONE line.
	evil := goodLockClaims()
	evil["sub"] = "x status=recalled\nstore-api audit ts=forged"
	evil["jti"] = "j k=v"
	// Expired, so the line is a claims-refused one that carries the signed sub — an unknown
	// subject on an otherwise-valid token is counted and carries no token facts at all.
	evil["exp"] = lockClock.Add(-time.Hour).Unix()
	h.read(t, h.signer.sign(t, evil))
	line := h.lastLine(t)
	if strings.ContainsAny(line, "\n\r") {
		t.Fatalf("a signed sub forged a second audit line:\n  %q", line)
	}
	if n := strings.Count(line, " status="); n != 1 {
		t.Fatalf("a signed sub forged a second status= field (%d):\n  %s", n, line)
	}
	if !strings.HasSuffix(line, " sub=x_status=recalled?store-api_audit_ts=forged jti=j_k=v") {
		t.Fatalf("the sanitised sub/jti are not what auditField produces:\n  %s", line)
	}
}

// TestARoleMismatchOrUnknownSubjectStillLocksTheClientOut — a verified token refused AFTER
// `verifyToken` (a `role` that is not the required one, or a subject this control plane
// holds no user for) is an authentication refusal with no principal, and it COUNTS: an
// issuer that signs for self-registered users hands such tokens to anybody, so they are
// the guessing surface the limiter exists for. Five from one address start the lockout.
func TestARoleMismatchOrUnknownSubjectStillLocksTheClientOut(t *testing.T) {
	for _, arm := range []struct {
		name string
		role string
		edit func(map[string]any)
	}{
		{"role mismatch", "authenticated", func(c map[string]any) { c["role"] = "anon" }},
		{"unknown subject", "", func(c map[string]any) { c["sub"] = "member-unknown" }},
	} {
		t.Run(arm.name, func(t *testing.T) {
			var h *lockHarness
			if arm.role != "" {
				h = newLockHarness(t, arm.role)
			} else {
				h = newLockHarness(t)
			}
			good := goodLockClaims()
			if arm.role != "" {
				good["role"] = arm.role
			}
			if code, _ := h.read(t, h.signer.sign(t, good)); code != 200 {
				t.Fatalf("positive control: the good token got %d", code)
			}
			c := goodLockClaims()
			if arm.role != "" {
				c["role"] = arm.role
			}
			arm.edit(c)
			refused := h.signer.sign(t, c)
			for i := 1; i <= 5; i++ {
				code, status := h.read(t, refused)
				want := "unauthorized"
				if i == 5 {
					want = "lockout-triggered"
				}
				if code != 401 || status != want {
					t.Fatalf("request %d: got %d status=%s, want 401 status=%s — a %s refusal must be COUNTED", i, code, status, want, arm.name)
				}
			}
			if code, status := h.read(t, h.signer.sign(t, good)); code != 401 || status != "locked-out" {
				t.Fatalf("after five %s refusals the address must be locked out; the good token got %d status=%s", arm.name, code, status)
			}
		})
	}
}
