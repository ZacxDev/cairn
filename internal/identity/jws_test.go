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
// 🔴 ONE RULE WITH ONE ARM, AND THAT IS WHAT THE SYMMETRIC DELETION BOUGHT. This test
// used to need TWO deployments because the JWKS-only one did not reach the guard: with
// HS256 absent from `Algs`, `accepts` refused before any key resolved, so the arm passed
// for the wrong reason — proven at the time by opening the hole on purpose and watching
// that arm STAY GREEN. The second arm had to be a "mixed" deployment carrying a legacy
// symmetric secret, which is a configuration this build can no longer express.
// `symmetricAlg` runs AHEAD of `accepts`, so every arm below reaches it.
//
// 🔴 EACH ARM ASSERTS `ErrTokenAlgSymmetric`, NOT MERELY "AN ERROR HAPPENED", BECAUSE
// THE TOKEN IS REFUSED FOUR TIMES OVER AND ONLY ONE OF THOSE IS THE GUARD. Remove
// `symmetricAlg`'s call site and `accepts` refuses the same token with a bare
// `ErrTokenAlg` — a test that accepted any refusal would stay green and measure nothing,
// which is the vacuous shape the two-arm version above was. Watched to fail: with the
// `if symmetricAlg(alg)` block deleted, every arm here goes red naming this sentinel.
func TestTheAlgorithmConfusionForgeryIsREFUSED(t *testing.T) {
	rsaSigner := newRSASigner(t, "rsa-1", 2048)
	ecSigner := newECSigner(t, "ec-1")
	keys := keySetOver(t, rsaSigner, ecSigner)

	// 🔴 THE POSITIVE CONTROL FOR THE WHOLE TABLE. Without it, a `Verify` that refused
	// everything — a broken fixture, an unmaterialized key set, a wrong issuer — would
	// satisfy every assertion below while measuring nothing about the guard.
	live := VerifyOptions{Algs: []Alg{AlgRS256, AlgES256}, Issuer: testIssuer,
		Audience: testAudience, Keys: keys, Now: fixedNow}
	for _, s := range []*signer{rsaSigner, ecSigner} {
		if _, err := Verify(s.sign(t, defaultClaims(), nil), live); err != nil {
			t.Fatalf("precondition: a genuine %s token must verify against this deployment: %v", s.alg, err)
		}
	}

	// 🔴 THE ATTACKER'S MATERIAL IS THE DEPLOYMENT'S OWN PUBLISHED KEY, VERBATIM — NOT A
	// PLACEHOLDER. A forgery signed with random bytes would be refused by a verifier
	// that HAS the hole, because the HMAC would not match either: it measures the wrong
	// thing while looking identical. These are the exact bytes `publicJWK` puts in the
	// document anybody can fetch.
	stolen := map[string][]byte{
		"rsa-1": rsaSigner.rsaModulusBytes(),
		"ec-1":  ecSigner.ecPublicBytes(),
	}
	for kid, material := range stolen {
		if len(material) == 0 {
			t.Fatalf("precondition: the %s fixture published no key material, so nothing is forged with it", kid)
		}
	}

	for _, arm := range []struct {
		name string
		kid  string
		alg  string
		// algs is the deployment's configured set. Empty means the asymmetric pair.
		algs []Alg
	}{
		{
			name: "the RSA public modulus as the HMAC secret — the classic forgery",
			kid:  "rsa-1", alg: "HS256",
		},
		{
			// A second point on the "which key did the provider publish" dimension: an
			// EC project's public point is equally public, and equally usable as a
			// secret.
			name: "the EC public point as the HMAC secret",
			kid:  "ec-1", alg: "HS256",
		},
		{
			// 🔴 THE ARM THAT PROVES THE GUARD IS NOT `accepts` WEARING A NEW NAME. This
			// deployment EXPLICITLY accepts HS256 — the state a future one-line edit
			// re-adding a symmetric algorithm to `algKeyType` would produce — so
			// `accepts` says yes and the refusal must come from somewhere else.
			name: "a deployment that EXPLICITLY accepts HS256, which `accepts` would let through",
			kid:  "rsa-1", alg: "HS256",
			algs: []Alg{AlgRS256, AlgES256, Alg("HS256")},
		},
		{
			// The prefix rule, at two more registered MAC algorithms and at a
			// re-spelling. A guard written as `alg == "HS256"` passes the three arms
			// above and fails these.
			name: "HS384", kid: "rsa-1", alg: "HS384",
			algs: []Alg{AlgRS256, AlgES256, Alg("HS384")},
		},
		{
			name: "HS512", kid: "rsa-1", alg: "HS512",
			algs: []Alg{AlgRS256, AlgES256, Alg("HS512")},
		},
		{
			name: "a lower-case re-spelling", kid: "rsa-1", alg: "hs256",
			algs: []Alg{AlgRS256, AlgES256, Alg("hs256")},
		},
	} {
		t.Run(arm.name, func(t *testing.T) {
			secret := stolen[arm.kid]
			forged := forgeHS256(t, map[string]any{"alg": arm.alg, "kid": arm.kid, "typ": "JWT"},
				defaultClaims(), secret)
			// THE ARM'S OWN PRECONDITION: the forgery is well-formed and its HMAC is
			// correct for the key the attacker used. If a verifier ever DID treat the
			// published key as an HMAC secret, this exact string would verify.
			if !hmacMatches(t, forged, secret) {
				t.Fatal("precondition: the forged token's HMAC does not match its own key, so refusing it proves nothing")
			}

			opts := live
			if len(arm.algs) != 0 {
				opts.Algs = arm.algs
			}
			_, err := Verify(forged, opts)
			if err == nil {
				t.Fatalf("ALGORITHM CONFUSION: a token whose header says %s, signed with this deployment's own PUBLIC key as the HMAC secret, verified. Anybody who can fetch the JWKS can now be any user in this control plane", arm.alg)
			}
			// 🔴 THE SPECIFIC SENTINEL. `accepts`, `KeySet.key` and `hashFor` each
			// refuse this token too, all with a bare ErrTokenAlg; accepting any of
			// those here would make the guard's removal invisible.
			if !errors.Is(err, ErrTokenAlgSymmetric) {
				t.Fatalf("refused, but NOT by the unconditional symmetric-algorithm guard — so that guard is either gone or unreachable.\n  got:  %v\n  want: %v", err, ErrTokenAlgSymmetric)
			}
			// And the narrowing relationship, so a caller classifying on the wider
			// sentinel did not silently stop matching.
			if !errors.Is(err, ErrTokenAlg) {
				t.Fatalf("ErrTokenAlgSymmetric must remain a narrowing of ErrTokenAlg, got %v", err)
			}
		})
	}
}

// TestNoConfiguredAlgorithmIsSymmetric is the STRUCTURAL half, and it is a different
// claim from the table above.
//
// The table asserts what happens to one token. This asserts a property of the build: the
// tables every verification path reads name no shared-secret algorithm, so there is no
// key type, no digest and no primitive for one. It fails when somebody adds one back —
// which is the edit `symmetricAlg` exists to survive, and this is what makes that edit
// LOUD rather than merely ineffective.
func TestNoConfiguredAlgorithmIsSymmetric(t *testing.T) {
	for alg := range algKeyType {
		if symmetricAlg(alg) {
			t.Fatalf("algKeyType names %q, a shared-secret algorithm. `symmetricAlg` still refuses it in Verify, so no token verifies — but the table now claims a key type this build must never resolve", alg)
		}
	}
	for alg := range hashFor {
		if symmetricAlg(alg) {
			t.Fatalf("hashFor names %q, a shared-secret algorithm", alg)
		}
	}
	// The set a deployment actually gets, discovered from the constructor rather than
	// restated — a restatement is the second spelling this test exists to refuse.
	backend, err := NewSupabaseJWT(goodSupabaseConfig(t, keySetOver(t, newRSASigner(t, "rsa-1", 2048))))
	if err != nil {
		t.Fatal(err)
	}
	if len(backend.verify.Algs) == 0 {
		t.Fatal("the constructor configured an EMPTY algorithm set, which accepts nothing — every assertion below would pass vacuously")
	}
	for _, alg := range backend.verify.Algs {
		if symmetricAlg(alg) {
			t.Fatalf("NewSupabaseJWT configured %q for a deployment", alg)
		}
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

// ⚠ `TestAnHS256DEPLOYMENTStillRefusesAnAsymmetricToken` WAS DELETED HERE, NOT LOST.
// It asserted that a deployment configured with ONLY a legacy symmetric secret refuses
// an RS256 token. No such deployment can be built any more — `SupabaseConfig` has no
// secret field and `NewSupabaseJWT` refuses without a JWKS — so the test described a
// world rather than this one. The claim it actually protected, that a key is never
// handed to a primitive it does not belong to, is `KeySet.key`'s `algKeyType` lookup and
// `verifySignature`'s type assertions, both still measured.

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
