package identity

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

// verifierOver builds a verifier for one signer's published key.
func verifierOver(t *testing.T, s *signer, algs ...Alg) VerifyOptions {
	t.Helper()
	keys, err := parseJWKS(jwksDocumentOf(t, s.publicJWK(t)))
	if err != nil {
		t.Fatalf("parsing the fixture JWKS: %v", err)
	}
	set, err := NewKeySet(JWKSOptions{URL: "https://notes-idp.example.test/jwks", Now: fixedNow})
	if err != nil {
		t.Fatalf("building a key set: %v", err)
	}
	set.keys, set.fetched, set.fetchedAt = keys, true, testClock
	if len(algs) == 0 {
		algs = []Alg{AlgRS256, AlgES256}
	}
	return VerifyOptions{
		Algs: algs, Issuer: testIssuer, Audience: testAudience, Keys: set, Now: fixedNow,
	}
}

// signerAndVerifier mints a key and returns BOTH halves, which is what the asymmetric
// arms need and what `verifierOver` alone cannot give.
func signerAndVerifier(t *testing.T, s *signer, algs ...Alg) (*signer, VerifyOptions) {
	t.Helper()
	return s, verifierOver(t, s, algs...)
}

func TestACorrectAsymmetricTokenVerifies(t *testing.T) {
	for _, arm := range []struct {
		name string
		make func(*testing.T) *signer
	}{
		{"RS256", func(t *testing.T) *signer { return newRSASigner(t, "rsa-1", 2048) }},
		{"ES256", func(t *testing.T) *signer { return newECSigner(t, "ec-1") }},
	} {
		t.Run(arm.name, func(t *testing.T) {
			s, opts := signerAndVerifier(t, arm.make(t))
			got, err := Verify(s.sign(t, defaultClaims(), nil), opts)
			if err != nil {
				t.Fatalf("a correct %s token must verify: %v", arm.name, err)
			}
			if got.Subject != testSubject || got.Issuer != testIssuer {
				t.Fatalf("claims came back wrong: %+v", got)
			}
		})
	}
}

// TestTheAlgorithmConfusionForgeryIsREFUSED is the attack this verifier's whole shape
// exists for.
//
// 🔴 THE ATTACK, STATED SO THE ASSERTION IS READABLE: the RSA verification key is
// PUBLIC — it is in the JWKS document anybody can fetch. An attacker takes those bytes,
// uses them as an HMAC-SHA256 secret, mints a token whose header says `alg: HS256` with
// the real key's `kid`, and presents it. A verifier that let the token's header choose
// the primitive computes HMAC over the signing input with a "secret" the attacker also
// has, and the signature matches. Every claim in the token is then whatever they wrote.
//
// 🔴 AND THE ARM IS BUILT FROM THE REAL PUBLIC KEY, NOT A PLACEHOLDER. A test that
// forged with random bytes would pass against a verifier that has the hole, because the
// HMAC would not match anyway — measuring the wrong thing while looking identical.
//
// 🔴 TWO DEPLOYMENTS, BECAUSE ONE OF THEM DOES NOT REACH THE GUARD THIS TEST IS ABOUT —
// MEASURED, NOT ASSUMED. The first draft had only the JWKS-only arm. It passed, and it
// passed for the WRONG REASON: `accepts` refuses HS256 before any key is resolved, so
// the key/algorithm rule never executed. Proven by opening the hole on purpose — the
// type check in `KeySet.key` replaced by `if false`, and `verifySignature`'s HS256 arm
// taught to take an RSA modulus as its secret — after which that arm STILL PASSED. The
// mixed arm is the one that reaches the resolver, and it is also the realistic
// deployment: a Supabase project mid-migration from the legacy symmetric secret to
// asymmetric keys accepts both, which is exactly when this attack is available.
func TestTheAlgorithmConfusionForgeryIsREFUSED(t *testing.T) {
	rsaSigner := newRSASigner(t, "rsa-1", 2048)

	// The attacker's material: the published modulus, verbatim.
	stolen := rsaSigner.rsaModulusBytes()
	if len(stolen) == 0 {
		t.Fatal("precondition: the fixture published no modulus, so this arm forges nothing")
	}
	forged := forgeHS256(t, map[string]any{"alg": "HS256", "kid": "rsa-1", "typ": "JWT"},
		defaultClaims(), stolen)
	// PRECONDITION, so this cannot pass vacuously: the forgery is well-formed and its
	// HMAC is correct for the key the attacker used. If a verifier ever DID treat the
	// public key as an HMAC secret, this exact string would verify.
	if !hmacMatches(t, forged, stolen) {
		t.Fatal("precondition: the forged token's HMAC does not match its own key, so refusing it proves nothing")
	}

	keys := keySetOver(t, rsaSigner)
	legacy := []byte("a-synthetic-symmetric-secret-of-sufficient-length")

	for _, arm := range []struct {
		name string
		opts VerifyOptions
		// reachesResolver says whether `accepts` lets HS256 through, so the refusal has
		// to come from the key resolver rather than from the algorithm allowlist.
		reachesResolver bool
	}{
		{
			name: "a JWKS-only deployment — refused by the ALLOWLIST, before any key is resolved",
			opts: VerifyOptions{Algs: []Alg{AlgRS256, AlgES256}, Issuer: testIssuer,
				Audience: testAudience, Keys: resolverPair{keys: keys}, Now: fixedNow},
		},
		{
			name: "a MIXED deployment — HS256 is accepted, so the RESOLVER is what must refuse",
			opts: VerifyOptions{Algs: []Alg{AlgRS256, AlgES256, AlgHS256}, Issuer: testIssuer,
				Audience: testAudience, Keys: resolverPair{keys: keys, secret: staticSecret(legacy)},
				Now: fixedNow},
			reachesResolver: true,
		},
	} {
		t.Run(arm.name, func(t *testing.T) {
			if arm.reachesResolver {
				// The rung's own precondition: in THIS configuration a genuine HS256
				// token verifies, so the allowlist is not what refuses the forgery.
				if _, err := Verify(newHMACSigner(legacy).sign(t, defaultClaims(), nil), arm.opts); err != nil {
					t.Fatalf("precondition: this deployment accepts HS256, so a genuine one must verify: %v", err)
				}
			}
			_, err := Verify(forged, arm.opts)
			if err == nil {
				t.Fatal("ALGORITHM CONFUSION: a token whose header says HS256, signed with the PUBLIC RSA key as the HMAC secret, verified. Anybody who can fetch the JWKS can now be any user in this control plane")
			}
			if !errors.Is(err, ErrTokenAlg) && !errors.Is(err, ErrTokenSignature) {
				t.Fatalf("refused, but for the wrong reason: %v", err)
			}
		})
	}
}

// keySetOver builds a materialized KeySet holding one signer's published key.
func keySetOver(t *testing.T, signers ...*signer) *KeySet {
	t.Helper()
	entries := make([]map[string]any, 0, len(signers))
	for _, s := range signers {
		entries = append(entries, s.publicJWK(t))
	}
	keys, err := parseJWKS(jwksDocumentOf(t, entries...))
	if err != nil {
		t.Fatalf("parsing the fixture JWKS: %v", err)
	}
	set, err := NewKeySet(JWKSOptions{URL: "https://notes-idp.example.test/jwks", Now: fixedNow})
	if err != nil {
		t.Fatalf("building a key set: %v", err)
	}
	set.keys, set.fetched, set.fetchedAt = keys, true, testClock
	return set
}

// TestAnHS256DEPLOYMENTStillRefusesAnAsymmetricToken is the mirror image, and it is a
// separate claim.
//
// A deployment configured with only a legacy symmetric secret must not verify an RS256
// or ES256 token: there is no public key to check it against, and the dangerous
// implementation is one that reaches for the symmetric secret anyway.
func TestAnHS256DEPLOYMENTStillRefusesAnAsymmetricToken(t *testing.T) {
	secret := []byte("a-synthetic-symmetric-secret-of-sufficient-length")
	opts := VerifyOptions{
		Algs: []Alg{AlgHS256}, Issuer: testIssuer, Audience: testAudience,
		Keys: staticSecret(secret), Now: fixedNow,
	}
	// Positive control: the symmetric token this deployment IS configured for verifies.
	if _, err := Verify(newHMACSigner(secret).sign(t, defaultClaims(), nil), opts); err != nil {
		t.Fatalf("precondition: the configured HS256 token must verify, got %v", err)
	}

	rsaSigner := newRSASigner(t, "rsa-1", 2048)
	if _, err := Verify(rsaSigner.sign(t, defaultClaims(), nil), opts); err == nil {
		t.Fatal("an RS256 token verified against a deployment that has no public key at all")
	} else if !errors.Is(err, ErrTokenAlg) {
		t.Fatalf("refused for the wrong reason: %v", err)
	}
}

// TestEveryTokenSHAPERefusalIsReachable walks the parse/header ladder.
//
// 🔴 EACH CASE IS REACHED BY AN INPUT NO EARLIER RUNG REJECTS, AND THE ASSERTION NAMES
// THE SENTINEL RATHER THAN "an error happened". A guard that dies because the size cap
// rejected the input first is a guard that has never run.
func TestEveryTokenSHAPERefusalIsReachable(t *testing.T) {
	s, opts := signerAndVerifier(t, newRSASigner(t, "rsa-1", 2048))
	good := s.sign(t, defaultClaims(), nil)
	// The positive control for this whole table.
	if _, err := Verify(good, opts); err != nil {
		t.Fatalf("precondition: the unmodified token must verify, got %v", err)
	}
	parts := strings.Split(good, ".")

	for _, arm := range []struct {
		name  string
		token string
		want  error
	}{
		{"empty", "", ErrTokenMalformed},
		{"over the size cap", strings.Repeat("a", maxTokenBytes+1), ErrTokenMalformed},
		{"two parts", parts[0] + "." + parts[1], ErrTokenMalformed},
		{"five parts (a JWE)", good + ".x.y", ErrTokenMalformed},
		{"header is not base64url", "!!!." + parts[1] + "." + parts[2], ErrTokenMalformed},
		{"header is padded base64", strings.TrimRight(parts[0], "=") + "==." + parts[1] + "." + parts[2], ErrTokenMalformed},
		{"header is not JSON", base64.RawURLEncoding.EncodeToString([]byte("not json")) + "." + parts[1] + "." + parts[2], ErrTokenMalformed},
		{"crit names an extension", s.sign(t, defaultClaims(), map[string]any{"crit": []string{"exp"}}), ErrTokenMalformed},
		{"typ is not JWT", s.sign(t, defaultClaims(), map[string]any{"typ": "at+jwt"}), ErrTokenMalformed},
		{"alg none", s.sign(t, defaultClaims(), map[string]any{"alg": "none"}), ErrTokenAlg},
		{"alg absent", s.sign(t, defaultClaims(), map[string]any{"alg": nil}), ErrTokenAlg},
		{"alg this build cannot verify", s.sign(t, defaultClaims(), map[string]any{"alg": "PS512"}), ErrTokenAlg},
		{"kid this verifier does not hold", s.sign(t, defaultClaims(), map[string]any{"kid": "rsa-2"}), ErrNoKey},
		{"signature does not verify", parts[0] + "." + parts[1] + "." + flipLastByte(t, parts[2]), ErrTokenSignature},
		{"payload tampered after signing", parts[0] + "." + tamperedPayload(t, parts[1]) + "." + parts[2], ErrTokenSignature},
	} {
		t.Run(arm.name, func(t *testing.T) {
			_, err := Verify(arm.token, opts)
			if err == nil {
				t.Fatal("accepted a token this verifier must refuse")
			}
			if !errors.Is(err, arm.want) {
				t.Fatalf("refused for the wrong reason: got %v, want a %v", err, arm.want)
			}
		})
	}
}

// TestEveryCLAIMRefusalIsReachable walks the claim ladder, on a token whose SIGNATURE is
// correct every time.
//
// 🔴 THE SIGNATURE IS VALID IN EVERY ARM, WHICH IS THE POINT. A claim guard tested with
// a broken signature is a claim guard that never ran — the signature check is above it
// in the ladder and would reject first. Each token here is minted by the real key.
func TestEveryCLAIMRefusalIsReachable(t *testing.T) {
	s, opts := signerAndVerifier(t, newRSASigner(t, "rsa-1", 2048))

	with := func(edit func(claimSet)) string {
		c := defaultClaims()
		edit(c)
		return s.sign(t, c, nil)
	}

	for _, arm := range []struct {
		name  string
		token string
	}{
		{"a different issuer", with(func(c claimSet) { c["iss"] = "https://other-idp.example.test/auth/v1" })},
		{"no issuer at all", with(func(c claimSet) { delete(c, "iss") })},
		{"a different audience", with(func(c claimSet) { c["aud"] = "some-other-app" })},
		{"an audience ARRAY without ours", with(func(c claimSet) { c["aud"] = []string{"a", "b"} })},
		{"no audience at all", with(func(c claimSet) { delete(c, "aud") })},
		{"an empty subject", with(func(c claimSet) { c["sub"] = "" })},
		// ⚠ THIS ARM ASSERTS THE MESSAGE BELOW, NOT ONLY THE REFUSAL, AND A MUTATION
		// SWEEP IS WHAT SAID IT HAD TO. With `!c.Expiry.Present` deleted, a missing `exp`
		// leaves the zero `numericDate`, which renders as 1970 and is refused by the exp
		// COMPARISON — so "it was refused" cannot tell the two apart and the mutant
		// SURVIVED a fully green table. What the guard buys is that the refusal names the
		// absent claim rather than reporting a token that expired before the epoch.
		{"no exp — a token that never expires", with(func(c claimSet) { delete(c, "exp") })},
		{"exp in the past", with(func(c claimSet) { c["exp"] = testClock.Add(-time.Second).Unix() })},
		{"nbf in the future", with(func(c claimSet) { c["nbf"] = testClock.Add(time.Hour).Unix() })},
		{"iat in the future", with(func(c claimSet) { c["iat"] = testClock.Add(time.Hour).Unix() })},
		{"exp is not an integral NumericDate", with(func(c claimSet) { c["exp"] = 1.5 })},
		// 🔴 THE ARM A `float64` DECODER WOULD ACCEPT. `json.Unmarshal` into a float
		// turns an overflowing literal into `+Inf`, which compares as "expires after
		// every instant" — a token that never expires, spelled as one that does. Written
		// as a raw JSON number because Go cannot hold the constant.
		{"exp overflows float64", with(func(c claimSet) { c["exp"] = json.RawMessage("1e400") })},
		{"aud is a number", with(func(c claimSet) { c["aud"] = 7 })},
	} {
		t.Run(arm.name, func(t *testing.T) {
			if _, err := Verify(arm.token, opts); err == nil {
				t.Fatal("accepted a correctly-signed token whose claims this deployment must refuse")
			}
		})
	}

	t.Run("a missing exp is refused AS MISSING", func(t *testing.T) {
		token := with(func(c claimSet) { delete(c, "exp") })
		_, err := Verify(token, opts)
		if err == nil {
			t.Fatal("a token with no exp verified")
		}
		if !strings.Contains(err.Error(), "exp is absent") {
			t.Fatalf("the refusal must name the ABSENT claim rather than report an expiry "+
				"before the epoch, which is what the zero value renders as: %v", err)
		}
	})

	// And the arm that must be ACCEPTED, so the table above is not measuring "any
	// deviation from the default claim set is refused".
	t.Run("an audience ARRAY containing ours", func(t *testing.T) {
		token := with(func(c claimSet) { c["aud"] = []string{"other-app", testAudience} })
		if _, err := Verify(token, opts); err != nil {
			t.Fatalf("an `aud` array carrying this audience must verify: %v", err)
		}
	})
}

// TestTheLeewayWindowIsBoundedAndIsMEASUREDATBOTHENDS.
//
// 🔴 ONE MEASUREMENT IS NOT A GENERAL CLAIM. Leeway is a duration, so it is measured at
// two points on either side of the boundary it creates: a token expired by less than the
// leeway is accepted, one expired by more is not, and a leeway wider than `MaxLeeway` is
// refused outright rather than clamped.
func TestTheLeewayWindowIsBoundedAndIsMeasuredAtBothEnds(t *testing.T) {
	s, base := signerAndVerifier(t, newRSASigner(t, "rsa-1", 2048))

	expired := func(by time.Duration) string {
		c := defaultClaims()
		c["exp"] = testClock.Add(-by).Unix()
		return s.sign(t, c, nil)
	}

	withLeeway := base
	withLeeway.Leeway = 30 * time.Second

	if _, err := Verify(expired(10*time.Second), withLeeway); err != nil {
		t.Fatalf("expired by 10s inside a 30s leeway must verify: %v", err)
	}
	if _, err := Verify(expired(90*time.Second), withLeeway); err == nil {
		t.Fatal("expired by 90s outside a 30s leeway verified — the window is not bounded")
	}

	tooWide := base
	tooWide.Leeway = MaxLeeway + time.Second
	if _, err := Verify(expired(time.Second), tooWide); err == nil {
		t.Fatalf("a leeway of %s exceeds MaxLeeway (%s) and must be refused rather than clamped", tooWide.Leeway, MaxLeeway)
	}
}

// TestAConfiguredMaxAgeIsENFORCEDRatherThanSilentlySkipped.
//
// 🔴 THE SECOND ARM IS THE ONE WORTH HAVING. A token with NO `iat` cannot have its age
// checked at all, and the tempting implementation skips the check — which means a
// configured bound is not enforced for exactly the tokens that decline to say when they
// were issued.
func TestAConfiguredMaxAgeIsEnforcedRatherThanSilentlySkipped(t *testing.T) {
	s, base := signerAndVerifier(t, newRSASigner(t, "rsa-1", 2048))
	bounded := base
	bounded.MaxAge = 10 * time.Minute

	fresh := defaultClaims()
	fresh["iat"] = testClock.Add(-time.Minute).Unix()
	if _, err := Verify(s.sign(t, fresh, nil), bounded); err != nil {
		t.Fatalf("precondition: a one-minute-old token inside a ten-minute bound must verify: %v", err)
	}

	old := defaultClaims()
	old["iat"] = testClock.Add(-time.Hour).Unix()
	if _, err := Verify(s.sign(t, old, nil), bounded); err == nil {
		t.Fatal("an hour-old token verified against a ten-minute max age")
	}

	none := defaultClaims()
	delete(none, "iat")
	if _, err := Verify(s.sign(t, none, nil), bounded); err == nil {
		t.Fatal("a token with no `iat` verified against a configured max age — the bound is not enforced for tokens that decline to say when they were issued, which is every token an attacker mints")
	}
}

// TestAnES256SignatureMustBeFixedWidthRAndS.
//
// 🔴 TWO WRONG IMPLEMENTATIONS, BOTH REFUSED. A verifier that expected ASN.1 would
// reject every correct token (loud, and caught by the positive control above); one that
// left-padded a SHORT signature would turn a truncated signature into a valid one for a
// different message. The length is checked exactly.
func TestAnES256SignatureMustBeFixedWidthRAndS(t *testing.T) {
	s, opts := signerAndVerifier(t, newECSigner(t, "ec-1"))
	good := s.sign(t, defaultClaims(), nil)
	parts := strings.Split(good, ".")
	raw, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) != 64 {
		t.Fatalf("precondition: a P-256 JWS signature is 64 bytes, this fixture is %d", len(raw))
	}
	short := parts[0] + "." + parts[1] + "." + base64.RawURLEncoding.EncodeToString(raw[1:])
	if _, err := Verify(short, opts); err == nil {
		t.Fatal("a 63-byte ECDSA signature verified")
	} else if !errors.Is(err, ErrTokenSignature) {
		// ⚠ THE REASON IS ASSERTED BECAUSE THE CHECK IS DEFENCE IN DEPTH. Without the
		// length check the signature would fail verification anyway (measured — see
		// `verifySignature`), so "it was refused" does not distinguish the two. What
		// this pins is that the refusal is EXPLICIT about the length.
		t.Fatalf("refused for the wrong reason: %v", err)
	}
	if !strings.Contains(veryLastError(t, short, opts), "signature is 63 bytes") {
		t.Fatalf("the refusal does not name the length, so it is the incidental verification failure rather than the explicit check: %v", veryLastError(t, short, opts))
	}
	long := parts[0] + "." + parts[1] + "." + base64.RawURLEncoding.EncodeToString(append(raw, 0))
	if _, err := Verify(long, opts); err == nil {
		t.Fatal("a 65-byte ECDSA signature verified")
	}
}

// --- helpers for the forgery arms --------------------------------------------------

// forgeHS256 mints a token whose header says whatever the caller wants and whose
// signature is an HMAC under `secret`. This is the attacker's tool, not the fixture's.
func forgeHS256(t *testing.T, header map[string]any, claims claimSet, secret []byte) string {
	t.Helper()
	input := encodeJSON(t, header) + "." + encodeJSON(t, claims)
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(input))
	return input + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// hmacMatches is the forgery's own precondition: the token really is a correct HMAC
// under the key the attacker used.
func hmacMatches(t *testing.T, token string, secret []byte) bool {
	t.Helper()
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return false
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(parts[0] + "." + parts[1]))
	return hmac.Equal(mac.Sum(nil), sig)
}

func flipLastByte(t *testing.T, segment string) string {
	t.Helper()
	raw, err := base64.RawURLEncoding.DecodeString(segment)
	if err != nil || len(raw) == 0 {
		t.Fatalf("decoding a fixture segment: %v", err)
	}
	raw[len(raw)-1] ^= 0xff
	return base64.RawURLEncoding.EncodeToString(raw)
}

// tamperedPayload rewrites the SUBJECT in a signed payload — the edit an attacker
// actually wants — leaving the signature untouched.
func tamperedPayload(t *testing.T, segment string) string {
	t.Helper()
	raw, err := base64.RawURLEncoding.DecodeString(segment)
	if err != nil {
		t.Fatalf("decoding a fixture payload: %v", err)
	}
	var claims map[string]any
	if err := json.Unmarshal(raw, &claims); err != nil {
		t.Fatalf("decoding a fixture payload: %v", err)
	}
	claims["sub"] = testStranger
	return encodeJSON(t, claims)
}

// veryLastError re-runs a verification and returns its error text, so an assertion can
// read the MESSAGE rather than only the sentinel.
func veryLastError(t *testing.T, token string, opts VerifyOptions) string {
	t.Helper()
	_, err := Verify(token, opts)
	if err == nil {
		t.Fatal("expected a refusal")
	}
	return err.Error()
}
