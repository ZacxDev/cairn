package identity

// The JWKS FILE source: `JWKSOptions.File` / `CAIRN_OIDC_JWKS_FILE`.
//
// The two sources share `parseJWKS` and `KeySet.key` by construction, so the key RULES are
// measured once, through the shared parser, in `jwks_test.go`. What is asserted here is the
// file source's own behaviour, plus one invariant guard that it really does reach that parser
// (`TestAJWKSFileIsParsedByTheSharedJWKSParser`).

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// rewrite replaces the file's contents through a rename, the way a mounted configuration
// object is swapped, so a reader can never observe a half-written document from this helper.
func rewrite(t *testing.T, path string, body []byte) {
	t.Helper()
	tmp := path + ".next"
	if err := os.WriteFile(tmp, body, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(tmp, path); err != nil {
		t.Fatal(err)
	}
}

func fileKeySet(t *testing.T, path string) *KeySet {
	t.Helper()
	set, err := NewKeySet(JWKSOptions{File: path, Now: steppingClock()})
	if err != nil {
		t.Fatalf("building a key set over %s: %v", path, err)
	}
	return set
}

func verifierOverSet(set *KeySet) VerifyOptions {
	return VerifyOptions{
		Algs: []Alg{AlgRS256, AlgES256}, Issuer: testIssuer, Audience: testAudience, Keys: set, Now: fixedNow,
	}
}

// TestAJWKSURLAndAJWKSFileTogetherAreRefused: one key set, one source.
//
// 🔴 ASSERTED AT BOTH LAYERS, BECAUSE THEY ARE TWO CLAIMS. `NewKeySet` is where the rule
// lives; `FromEnvironment` is what an operator reaches, and a reader that resolved one
// variable and quietly dropped the other would satisfy the constructor test with the rule
// never running on a real configuration.
func TestAJWKSURLAndAJWKSFileTogetherAreRefused(t *testing.T) {
	path := writeJWKSFile(t, jwksDocumentOf(t, newECSigner(t, "ec-1").publicJWK(t)))

	_, err := NewKeySet(JWKSOptions{URL: "https://notes-idp.example.test/jwks", File: path})
	if !errors.Is(err, ErrJWKSTwoSources) {
		t.Fatalf("a URL and a file together must refuse with ErrJWKSTwoSources, got %v", err)
	}

	_, _, err = FromEnvironment(map[string]string{
		EnvSupabaseIssuer:   testIssuer,
		EnvSupabaseJWKSURL:  "https://notes-idp.example.test/jwks",
		EnvSupabaseJWKSFile: path,
	}, newTestAuthority(t), newTestSessionAuthority(t))
	if !errors.Is(err, ErrJWKSTwoSources) {
		t.Fatalf("the environment carrying both must refuse with ErrJWKSTwoSources, got %v", err)
	}

	// THE POSITIVE CONTROL: either one alone builds, so the refusal above is about the PAIR.
	for name, opts := range map[string]JWKSOptions{
		"url alone":  {URL: "https://notes-idp.example.test/jwks"},
		"file alone": {File: path},
	} {
		if _, err := NewKeySet(opts); err != nil {
			t.Fatalf("%s must build: %v", name, err)
		}
	}
}

// TestEveryJWKSFileConstructionRefusalIsReachable walks the file source's shape checks with
// inputs no earlier rung rejects, asserting the specific sentinel.
func TestEveryJWKSFileConstructionRefusalIsReachable(t *testing.T) {
	for _, arm := range []struct {
		name, file string
		want       error
		wantText   string
	}{
		{"whitespace only", "   ", ErrJWKSFile, "empty"},
		{"relative", "conf/jwks.json", ErrJWKSFile, "relative"},
	} {
		t.Run(arm.name, func(t *testing.T) {
			_, err := NewKeySet(JWKSOptions{File: arm.file})
			if !errors.Is(err, arm.want) || !strings.Contains(err.Error(), arm.wantText) {
				t.Fatalf("NewKeySet(File: %q) = %v, want %v mentioning %q", arm.file, err, arm.want, arm.wantText)
			}
		})
	}
}

// TestAKeySetReadFromAFileNeverTouchesTheNetwork is the promise the file source exists for,
// reported as the PAIR: a counting transport that DOES move on a URL source, and does not on
// a file source handed the very same client.
func TestAKeySetReadFromAFileNeverTouchesTheNetwork(t *testing.T) {
	s := newRSASigner(t, "rsa-1", 2048)
	doc := jwksDocumentOf(t, s.publicJWK(t))
	counter := &countingTransport{inner: http.DefaultTransport}
	client := &http.Client{Transport: counter, Timeout: 5 * time.Second}

	// Positive control: the counter is wired to the client.
	provider := newJWKSServer(t, doc)
	viaURL, err := NewKeySet(JWKSOptions{URL: provider.url(), Client: client})
	if err != nil {
		t.Fatal(err)
	}
	if err := viaURL.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := counter.calls.Load(); got != 1 {
		t.Fatalf("positive control: the URL source must make exactly one request, made %d", got)
	}

	viaFile, err := NewKeySet(JWKSOptions{File: writeJWKSFile(t, doc), Client: client})
	if err != nil {
		t.Fatal(err)
	}
	for range 3 {
		if err := viaFile.Refresh(context.Background()); err != nil {
			t.Fatalf("the file source must materialize: %v", err)
		}
	}
	if got := counter.calls.Load(); got != 1 {
		t.Fatalf("the file source made %d network request(s); it must make none (1 on the positive control, %d under test)",
			got-1, got-1)
	}
	if _, err := Verify(s.sign(t, defaultClaims(), nil), verifierOverSet(viaFile)); err != nil {
		t.Fatalf("a token signed by the file's key must verify: %v", err)
	}
}

// TestARotatedJWKSFileIsPickedUpOnTheNextRefresh: the new key verifies, the retired kid does
// not — both halves, because "the new key verifies" alone is satisfied by a set that only
// ever grows.
func TestARotatedJWKSFileIsPickedUpOnTheNextRefresh(t *testing.T) {
	oldKey := newRSASigner(t, "rsa-old", 2048)
	newKey := newECSigner(t, "ec-new")
	path := writeJWKSFile(t, jwksDocumentOf(t, oldKey.publicJWK(t)))
	set := fileKeySet(t, path)
	if err := set.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	opts := verifierOverSet(set)
	if _, err := Verify(oldKey.sign(t, defaultClaims(), nil), opts); err != nil {
		t.Fatalf("precondition: the original key must verify: %v", err)
	}
	if _, err := Verify(newKey.sign(t, defaultClaims(), nil), opts); !errors.Is(err, ErrNoKey) {
		t.Fatalf("precondition: the not-yet-published key must be refused with ErrNoKey, got %v", err)
	}

	rewrite(t, path, jwksDocumentOf(t, newKey.publicJWK(t)))
	if err := set.Refresh(context.Background()); err != nil {
		t.Fatalf("re-reading the rotated file: %v", err)
	}
	if _, err := Verify(newKey.sign(t, defaultClaims(), nil), opts); err != nil {
		t.Fatalf("after the rotation the new key must verify: %v", err)
	}
	if _, err := Verify(oldKey.sign(t, defaultClaims(), nil), opts); !errors.Is(err, ErrNoKey) {
		t.Fatalf("after the rotation the retired kid must be refused with ErrNoKey, got %v", err)
	}
}

// TestABadReReadOfAJWKSFileKeepsTheLastGoodSet — every way a re-read can go wrong, each
// leaving the previous keys serving, the failure VISIBLE, and `FetchedAt` unmoved.
func TestABadReReadOfAJWKSFileKeepsTheLastGoodSet(t *testing.T) {
	good := newRSASigner(t, "rsa-1", 2048)
	other := newECSigner(t, "ec-2")
	for _, arm := range []struct {
		name    string
		breakIt func(t *testing.T, path string)
		want    string
	}{
		{"removed", func(t *testing.T, path string) {
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
		}, "no such file"},
		{"not JSON", func(t *testing.T, path string) { rewrite(t, path, []byte("{\"keys\": [")) }, "not JSON"},
		{"no usable key", func(t *testing.T, path string) {
			rewrite(t, path, jwksDocumentOf(t, map[string]any{"kty": "oct", "kid": "k", "k": "c2VjcmV0"}))
		}, ErrJWKSEmpty.Error()},
		{"a duplicated kid", func(t *testing.T, path string) {
			dup := other.publicJWK(t)
			dup["kid"] = "rsa-1"
			rewrite(t, path, jwksDocumentOf(t, good.publicJWK(t), dup))
		}, "declares kid"},
		{"oversized", func(t *testing.T, path string) {
			rewrite(t, path, []byte(strings.Repeat(" ", maxJWKSBytes+1)))
		}, "exceeds"},
	} {
		t.Run(arm.name, func(t *testing.T) {
			path := writeJWKSFile(t, jwksDocumentOf(t, good.publicJWK(t)))
			set := fileKeySet(t, path)
			if err := set.Refresh(context.Background()); err != nil {
				t.Fatal(err)
			}
			before := set.Status()

			arm.breakIt(t, path)
			err := set.Refresh(context.Background())
			if err == nil || !strings.Contains(err.Error(), arm.want) {
				t.Fatalf("a %s re-read must fail mentioning %q, got %v", arm.name, arm.want, err)
			}
			after := set.Status()
			if !after.Failing || after.Keys != before.Keys || !after.FetchedAt.Equal(before.FetchedAt) {
				t.Fatalf("a failed re-read must keep the last-good set and report the failure:\n  before %s\n  after  %s",
					before, after)
			}
			if _, err := Verify(good.sign(t, defaultClaims(), nil), verifierOverSet(set)); err != nil {
				t.Fatalf("the last-good key must keep verifying after a %s re-read: %v", arm.name, err)
			}
		})
	}
}

// TestAJWKSFileThatNeverParsedVerifiesNothing — the cold-start half, which `cmd/cairn-server`
// turns into an exit 78: a file-sourced set whose first read failed is UNMATERIALIZED, not
// empty-and-healthy.
func TestAJWKSFileThatNeverParsedVerifiesNothing(t *testing.T) {
	s := newECSigner(t, "ec-1")
	set := fileKeySet(t, filepath.Join(t.TempDir(), "absent.json"))
	if err := set.Refresh(context.Background()); err == nil {
		t.Fatal("reading a file that does not exist must fail")
	}
	if _, err := Verify(s.sign(t, defaultClaims(), nil), verifierOverSet(set)); !errors.Is(err, ErrJWKSUnmaterialized) {
		t.Fatalf("a never-read file source must refuse with ErrJWKSUnmaterialized, got %v", err)
	}
}

// TestAJWKSFileIsParsedByTheSharedJWKSParser — ONE assertion: a file-sourced refresh refuses a
// document with exactly the error `parseJWKS` gives for the same bytes.
//
// ⚠ AN INVARIANT GUARD, NOT REGRESSION COVERAGE. No bug ever gave the file source its own
// parser, so this cannot have been red on any tree that had the file source; it pins the shape
// that makes every key rule in `parseJWKS` (duplicate `kid`, dropped `oct` and sub-floor RSA,
// the on-curve check) hold for file keys without re-asserting each rule here — those rules are
// already measured through the shared parser by `jwks_test.go`. The fixture is a duplicate
// `kid` because that refusal's text is produced by `parseJWKS` and by nothing else, so a
// second parser that agreed on valid documents would still disagree here.
func TestAJWKSFileIsParsedByTheSharedJWKSParser(t *testing.T) {
	a, b := newRSASigner(t, "rsa-a", 2048), newECSigner(t, "ec-b")
	dup := b.publicJWK(t)
	dup["kid"] = "rsa-a"
	doc := jwksDocumentOf(t, a.publicJWK(t), dup)
	_, want := parseJWKS(doc)
	err := fileKeySet(t, writeJWKSFile(t, doc)).Refresh(context.Background())
	if want == nil || err == nil || !strings.HasSuffix(err.Error(), ": "+want.Error()) {
		t.Fatalf("the file source must refuse with parseJWKS's own error for the same bytes\n  parseJWKS: %v\n  file:      %v", want, err)
	}
}
