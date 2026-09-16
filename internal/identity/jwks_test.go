package identity

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// countingTransport counts every HTTP request this process makes for a key set.
//
// 🔴 A COUNTER THAT NEVER MOVES IS INDISTINGUISHABLE FROM A COUNTER WIRED TO NOTHING,
// which is why the no-network claim below is measured from BOTH directions: zero while
// verifying, and non-zero on a refresh. The same shape `control`'s
// `TestTheHotPathNeverCallsTheAuthority` uses, and for the same reason.
type countingTransport struct {
	inner http.RoundTripper
	calls atomic.Int64
}

func (c *countingTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	c.calls.Add(1)
	return c.inner.RoundTrip(r)
}

// jwksServer serves a key-set document that a test can change or kill.
type jwksServer struct {
	srv  *httptest.Server
	body atomic.Pointer[[]byte]
	code atomic.Int64
}

func newJWKSServer(t *testing.T, body []byte) *jwksServer {
	t.Helper()
	j := &jwksServer{}
	j.body.Store(&body)
	j.code.Store(200)
	j.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		code := int(j.code.Load())
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		if code == 200 {
			_, _ = w.Write(*j.body.Load())
		}
	}))
	// 🔴 OWN WHAT YOU SPAWN. Every test server here is closed on cleanup, including on a
	// failing run: a leaked listener holds a loopback port for as long as the process
	// lives, and this repository has already paid for one that held a port for hours.
	t.Cleanup(j.srv.Close)
	return j
}

func (j *jwksServer) url() string { return j.srv.URL + "/.well-known/jwks.json" }

// newLiveKeySet builds a key set over a live server, with a counting transport.
// 🔴 THE CLOCK STEPS, AND A MUTATION SWEEP IS WHAT SAID IT HAD TO. The first version
// used the package's FIXED clock, which made `now` equal to the instant of the last
// successful fetch — so a mutant that moves `fetchedAt` on a FAILED refresh changed
// nothing observable and SURVIVED a green test that appeared to assert the timestamp.
// The fixture was sitting exactly on its own guard's boundary, which is the shape this
// repository's rules name explicitly. Each reading advances by a minute, so "did the
// timestamp move" has an answer.
func steppingClock() func() time.Time {
	var calls int64
	return func() time.Time {
		calls++
		return testClock.Add(time.Duration(calls) * time.Minute)
	}
}

func newLiveKeySet(t *testing.T, j *jwksServer) (*KeySet, *countingTransport) {
	t.Helper()
	counter := &countingTransport{inner: http.DefaultTransport}
	set, err := NewKeySet(JWKSOptions{
		URL:    j.url(),
		Client: &http.Client{Transport: counter, Timeout: 5 * time.Second},
		Now:    steppingClock(),
	})
	if err != nil {
		t.Fatalf("building a key set over %s: %v", j.url(), err)
	}
	return set, counter
}

// TestAnIdentityProviderOUTAGEDoesNotStopAnAlreadyIssuedSession is the plan's §E promise,
// MEASURED BY KILLING THE DEPENDENCY rather than by reasoning about the code.
//
// 🔴 THE MEASUREMENT IS IN FOUR PARTS AND EACH IS A DIFFERENT CLAIM:
//
//  1. a fetch happened at all (the positive control — without it, "zero fetches during
//     verification" is indistinguishable from a key set wired to nothing);
//  2. verification of an already-issued token makes ZERO further requests;
//  3. with the provider DEAD, verification still succeeds — this is the promise;
//  4. the failure is still VISIBLE in `Status()`, because an outage that reports itself
//     healthy is the instrument that cannot see the thing it is trusted for.
func TestAnIdentityProviderOutageDoesNotStopAnAlreadyIssuedSession(t *testing.T) {
	s := newRSASigner(t, "rsa-1", 2048)
	provider := newJWKSServer(t, jwksDocumentOf(t, s.publicJWK(t)))
	set, counter := newLiveKeySet(t, provider)

	if err := set.Refresh(context.Background()); err != nil {
		t.Fatalf("the first fetch must succeed: %v", err)
	}
	afterFetch := counter.calls.Load()
	// (1) THE POSITIVE CONTROL. The number MOVED, so the counter is wired to the client
	// the key set actually uses.
	if afterFetch == 0 {
		t.Fatal("the counter did not move on a fetch, so every zero below would be a fact about the counter rather than about the hot path")
	}

	opts := VerifyOptions{
		Algs: []Alg{AlgRS256, AlgES256}, Issuer: testIssuer, Audience: testAudience,
		Keys: resolverPair{keys: set}, Now: fixedNow,
	}
	token := s.sign(t, defaultClaims(), nil)

	// (2) ZERO NETWORK ON THE HOT PATH.
	const verifications = 25
	for i := 0; i < verifications; i++ {
		if _, err := Verify(token, opts); err != nil {
			t.Fatalf("verification %d failed: %v", i, err)
		}
	}
	if got := counter.calls.Load(); got != afterFetch {
		t.Fatalf("%d verifications made %d network requests — the hot path contacts the identity provider, so an outage would stop reads",
			verifications, got-afterFetch)
	}
	t.Logf("no-network claim: %d requests on the refresh, %d across %d verifications",
		afterFetch, counter.calls.Load()-afterFetch, verifications)

	// (3) KILL THE DEPENDENCY. Not "make it slow" — close the listener, so a request
	// cannot even connect.
	provider.srv.Close()
	if _, err := Verify(token, opts); err != nil {
		t.Fatalf("an already-issued session stopped verifying when the identity provider died: %v — this is the promise the whole cached key set exists for", err)
	}

	// (4) AND THE OUTAGE IS VISIBLE. A refresh now fails, the serving key set is
	// unchanged, and the status says so.
	before := set.Status()
	if err := set.Refresh(context.Background()); err == nil {
		t.Fatal("a refresh against a closed listener reported success")
	}
	after := set.Status()
	if !after.Failing {
		t.Fatal("the key set reports itself healthy while its provider is dead")
	}
	if !after.Fetched || after.Keys != before.Keys {
		t.Fatalf("a failed refresh must keep last-known-good: keys %d -> %d", before.Keys, after.Keys)
	}
	if !after.FetchedAt.Equal(before.FetchedAt) {
		t.Fatalf("a failed refresh moved FetchedAt (%s -> %s) — the reported age would reset to zero on every failure, which is most wrong exactly when it matters most",
			before.FetchedAt, after.FetchedAt)
	}
	// And the token STILL verifies after the failed refresh, which is the state a pod
	// actually sits in during an outage.
	if _, err := Verify(token, opts); err != nil {
		t.Fatalf("after a failed refresh the session stopped verifying: %v", err)
	}
}

// TestAJWKSDocumentThatYieldsNoUsableKeyDoesNotREPLACEAGoodOne.
//
// 🔴 THE DEFECT THIS REFUSES IS ONE THIS REPOSITORY HAS ALREADY PAID FOR ONE LAYER DOWN.
// `tokenfile.Source` used to swallow an unreadable store root, and the EMPTY world it
// produced was committed, timestamped, and then served through a root that was readable
// again. Here the equivalent is a provider that answers 200 with a document carrying no
// key this build can use: committing it would refuse every session while reporting
// `fetched=true` and a fresh age.
func TestAJWKSDocumentThatYieldsNoUsableKeyDoesNotReplaceAGoodOne(t *testing.T) {
	s := newRSASigner(t, "rsa-1", 2048)
	provider := newJWKSServer(t, jwksDocumentOf(t, s.publicJWK(t)))
	set, _ := newLiveKeySet(t, provider)
	if err := set.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	good := set.Status()
	if good.Keys != 1 {
		t.Fatalf("precondition: expected one key, got %d", good.Keys)
	}

	for _, arm := range []struct {
		name string
		body []byte
	}{
		{"an empty key array", jwksDocumentOf(t)},
		{"a document that is not a JWKS at all", []byte(`{"error":"not found"}`)},
		{"only an `oct` key", []byte(`{"keys":[{"kty":"oct","kid":"k","k":"c2VjcmV0"}]}`)},
		{"only an encryption key", jwksDocumentOf(t, withUse(s.publicJWK(t), "enc"))},
		{"not JSON", []byte("<html>502</html>")},
	} {
		t.Run(arm.name, func(t *testing.T) {
			body := arm.body
			provider.body.Store(&body)
			if err := set.Refresh(context.Background()); err == nil {
				t.Fatal("a refresh that yields no usable key reported success")
			}
			got := set.Status()
			if got.Keys != good.Keys || !got.FetchedAt.Equal(good.FetchedAt) {
				t.Fatalf("the good key set was replaced: keys %d -> %d, at %s -> %s",
					good.Keys, got.Keys, good.FetchedAt, got.FetchedAt)
			}
		})
	}

	// The positive control for the table: a DIFFERENT but usable document IS adopted, so
	// the assertions above are not passing because `Refresh` never adopts anything.
	other := newECSigner(t, "ec-9")
	body := jwksDocumentOf(t, s.publicJWK(t), other.publicJWK(t))
	provider.body.Store(&body)
	if err := set.Refresh(context.Background()); err != nil {
		t.Fatalf("a usable document must be adopted: %v", err)
	}
	if got := set.Status(); got.Keys != 2 {
		t.Fatalf("the key set did not adopt the new document: %d keys", got.Keys)
	}
}

func withUse(jwk map[string]any, use string) map[string]any {
	out := map[string]any{}
	for k, v := range jwk {
		out[k] = v
	}
	out["use"] = use
	return out
}

// TestANon200OrOversizedJWKSResponseIsAFailure.
func TestANon200OrOversizedJWKSResponseIsAFailure(t *testing.T) {
	s := newRSASigner(t, "rsa-1", 2048)
	provider := newJWKSServer(t, jwksDocumentOf(t, s.publicJWK(t)))
	set, _ := newLiveKeySet(t, provider)

	provider.code.Store(503)
	if err := set.Refresh(context.Background()); err == nil {
		t.Fatal("a 503 from the provider reported success")
	}
	provider.code.Store(200)
	if err := set.Refresh(context.Background()); err != nil {
		t.Fatalf("precondition: a 200 must succeed, got %v", err)
	}

	// The body cap. Lowered on this instance so the fixture does not have to be 256 KiB.
	set.maxBodyLen = 32
	if err := set.Refresh(context.Background()); err == nil {
		t.Fatal("a body over the cap reported success — the remote end chooses this pod's memory")
	}
}

// TestAKeySetThatHasNeverFetchedVerifiesNOTHING.
//
// 🔴 `unmaterialized` IS NOT `stale`, AND THE DISTINCTION IS THE SAME ONE `control.Cache`
// DRAWS. A set that has never fetched has nothing to serve; conflating that with an
// outage would produce a verifier that accepts nobody while reporting itself healthy.
func TestAKeySetThatHasNeverFetchedVerifiesNothing(t *testing.T) {
	set, err := NewKeySet(JWKSOptions{URL: "https://notes-idp.example.test/jwks", Now: fixedNow})
	if err != nil {
		t.Fatal(err)
	}
	if got := set.Status(); got.Fetched {
		t.Fatal("a key set reported itself fetched before any fetch")
	}
	if _, err := set.key("rsa-1", AlgRS256); !errors.Is(err, ErrJWKSUnmaterialized) {
		t.Fatalf("an unmaterialized key set must refuse distinguishably, got %v", err)
	}
}

// TestEveryJWKSURLRefusalIsReachable.
//
// 🔴 THE PLAINTEXT ARM IS THE ONE THAT MATTERS. The keys fetched from this endpoint
// decide who every session is, so over `http` to a remote host anybody on the path
// substitutes their own key set and mints a session for any user in this control plane.
func TestEveryJWKSURLRefusalIsReachable(t *testing.T) {
	for _, arm := range []struct{ name, url string }{
		{"empty", ""},
		{"no host", "https:///jwks"},
		{"a scheme that is not http(s)", "file:///etc/keys.json"},
		{"plaintext to a remote host", "http://notes-idp.example.test/jwks"},
	} {
		t.Run(arm.name, func(t *testing.T) {
			if _, err := NewKeySet(JWKSOptions{URL: arm.url}); !errors.Is(err, ErrJWKSURL) {
				t.Fatalf("expected an %v, got %v", ErrJWKSURL, err)
			}
		})
	}
	// The two that must be ACCEPTED, so the table is not measuring "every URL is
	// refused": https anywhere, and plaintext to loopback (which is what a local TLS
	// terminator and this file's own test server are).
	for _, ok := range []string{"https://notes-idp.example.test/jwks", "http://127.0.0.1:9/jwks", "http://localhost:9/jwks"} {
		if _, err := NewKeySet(JWKSOptions{URL: ok}); err != nil {
			t.Fatalf("%s must be accepted: %v", ok, err)
		}
	}
}

// TestAJWKSKeyThisBuildMustNotUseIsDropped walks the per-key refusals.
//
// 🔴 EACH ARM IS PAIRED WITH A GOOD KEY IN THE SAME DOCUMENT, so "the document was
// refused" and "this key was dropped" are distinguishable: a provider publishing one key
// this build does not implement must not take the whole set down.
func TestAJWKSKeyThisBuildMustNotUseIsDropped(t *testing.T) {
	good := newRSASigner(t, "rsa-good", 2048)
	weak := newRSASigner(t, "rsa-weak", 1024)
	ec := newECSigner(t, "ec-good")

	offCurve := ec.publicJWK(t)
	// Move the point off the curve by flipping a byte of Y. An invalid-curve point is an
	// attack primitive, and `ecdsa.Verify` does not reject one for you.
	offCurve["y"] = flipLastByte(t, offCurve["y"].(string))
	offCurve["kid"] = "ec-off-curve"

	shortCoord := ec.publicJWK(t)
	raw, err := base64.RawURLEncoding.DecodeString(shortCoord["x"].(string))
	if err != nil {
		t.Fatal(err)
	}
	shortCoord["x"] = base64.RawURLEncoding.EncodeToString(raw[1:])
	shortCoord["kid"] = "ec-short"

	wrongCurve := ec.publicJWK(t)
	wrongCurve["crv"] = "P-384"
	wrongCurve["kid"] = "ec-p384"

	contradicts := ec.publicJWK(t)
	contradicts["alg"] = "RS256" // an EC key claiming an RSA algorithm
	contradicts["kid"] = "ec-contradicts"

	for _, arm := range []struct {
		name string
		jwk  map[string]any
	}{
		{"an RSA modulus under the floor", weak.publicJWK(t)},
		{"an EC point that is not on the curve", offCurve},
		{"an EC coordinate of the wrong length", shortCoord},
		{"a curve this build does not implement", wrongCurve},
		{"a key whose declared alg its own type cannot perform", contradicts},
		{"a symmetric key in a PUBLISHED document", map[string]any{"kty": "oct", "kid": "oct-1", "k": "c2VjcmV0"}},
	} {
		t.Run(arm.name, func(t *testing.T) {
			keys, err := parseJWKS(jwksDocumentOf(t, good.publicJWK(t), arm.jwk))
			if err != nil {
				t.Fatalf("one unusable key must not take the document down: %v", err)
			}
			if len(keys) != 1 {
				t.Fatalf("expected the good key alone, got %d keys", len(keys))
			}
			if _, held := keys["rsa-good"]; !held {
				t.Fatal("the good key was dropped instead of the bad one")
			}
		})
	}
}

// TestADuplicateKidRefusesTheWholeDocument.
//
// 🔴 A LAST-WINS WOULD MAKE "WHICH KEY VERIFIES THIS TOKEN" DEPEND ON MAP ITERATION
// ORDER, which Go deliberately randomises. A rotation that published both would then
// verify against whichever the range happened to reach.
func TestADuplicateKidRefusesTheWholeDocument(t *testing.T) {
	a := newRSASigner(t, "same", 2048)
	b := newECSigner(t, "same")
	if _, err := parseJWKS(jwksDocumentOf(t, a.publicJWK(t), b.publicJWK(t))); err == nil {
		t.Fatal("a document declaring one kid twice was accepted")
	}
	// Positive control: the same two keys under DIFFERENT ids are fine.
	b.kid = "different"
	if _, err := parseJWKS(jwksDocumentOf(t, a.publicJWK(t), b.publicJWK(t))); err != nil {
		t.Fatalf("two keys under two ids must parse: %v", err)
	}
}

// TestATokenWithNoKidIsRefusedWhenTheSetIsAMBIGUOUS.
//
// 🔴 "TRY EVERY KEY UNTIL ONE VERIFIES" IS THE IMPLEMENTATION THIS REFUSES. It turns a
// rotation into a permanent acceptance of the retired key and makes the refusal reason
// depend on iteration order. With exactly one candidate there is nothing to choose, so
// that case is allowed.
func TestATokenWithNoKidIsRefusedWhenTheSetIsAmbiguous(t *testing.T) {
	one := newRSASigner(t, "rsa-1", 2048)
	two := newRSASigner(t, "rsa-2", 2048)

	// Unambiguous: one RSA key, no kid in the token.
	set := keySetOver(t, one)
	one.kid = ""
	opts := VerifyOptions{Algs: []Alg{AlgRS256, AlgES256}, Issuer: testIssuer,
		Audience: testAudience, Keys: resolverPair{keys: set}, Now: fixedNow}
	if _, err := Verify(one.sign(t, defaultClaims(), nil), opts); err != nil {
		t.Fatalf("with exactly one candidate key, a token without a kid must verify: %v", err)
	}

	// Ambiguous: two RSA keys, no kid.
	one.kid = "rsa-1"
	ambiguous := keySetOver(t, one, two)
	one.kid = ""
	opts.Keys = resolverPair{keys: ambiguous}
	if _, err := Verify(one.sign(t, defaultClaims(), nil), opts); !errors.Is(err, ErrNoKey) {
		t.Fatalf("with two candidate keys, a token without a kid must be refused, got %v", err)
	}
}

// TestTheKeySetStatusRendersExactly pins the WHOLE normalised line for both states.
//
// 🔴 A GUARD ON A FEW WORDS IS WALKABLE BY REWORDING. `control.Staleness`'s own test
// makes the same ruling, and the cost is the same: a cosmetic reword fails this test,
// which is the price of a machine-readable claim.
func TestTheKeySetStatusRendersExactly(t *testing.T) {
	set, err := NewKeySet(JWKSOptions{URL: "https://notes-idp.example.test/jwks", Now: fixedNow})
	if err != nil {
		t.Fatal(err)
	}
	const wantEmpty = "jwks: fetched=false keys=0 age=n/a at=never fetches=0 failures=0 failing=false"
	if got := set.Status().String(); got != wantEmpty {
		t.Fatalf("unmaterialized renders\n  %q\nwant\n  %q", got, wantEmpty)
	}

	s := newRSASigner(t, "rsa-1", 2048)
	keys, err := parseJWKS(jwksDocumentOf(t, s.publicJWK(t)))
	if err != nil {
		t.Fatal(err)
	}
	set.keys, set.fetched, set.fetchedAt, set.fetches = keys, true, testClock.Add(-90*time.Second), 3
	const wantFetched = "jwks: fetched=true keys=1 age=1m30s at=2000-06-01T11:58:30Z fetches=3 failures=0 failing=false"
	if got := set.Status().String(); got != wantFetched {
		t.Fatalf("materialized renders\n  %q\nwant\n  %q", got, wantFetched)
	}
	if !strings.Contains(wantFetched, "age=1m30s") {
		t.Fatal("this test's own expectation lost the age field")
	}
}
