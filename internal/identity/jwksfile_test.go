package identity

// The JWKS FILE source: `JWKSOptions.File` / `CAIRN_OIDC_JWKS_FILE`.
//
// 🔴 EVERY KEY RULE IS ASSERTED THROUGH THE FILE SOURCE, NOT ONLY THROUGH THE URL ONE. The two
// sources share `parseJWKS` and `KeySet.key` by construction, and that is a claim about the
// code a test cannot read; what it CAN do is build a file-sourced set and watch each rule fire
// on it. A future refactor that gave the file its own parser would then be red here rather
// than a second, unaudited implementation of "which keys verify a session".

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

// TestAJWKSFileObeysTheKidRules — the two `kid` rules, through the file source.
//
// 🔴 THE MUTATION THESE ARE FOR IS "THE FILE SOURCE SKIPS A KID CHECK". Today it cannot,
// because there is no file-specific parser or resolver for a check to be skipped in; these
// arms are what makes adding one a red test rather than a quiet second implementation.
func TestAJWKSFileObeysTheKidRules(t *testing.T) {
	a := newRSASigner(t, "rsa-a", 2048)
	b := newECSigner(t, "ec-b")

	t.Run("a duplicate kid refuses the whole document", func(t *testing.T) {
		dup := b.publicJWK(t)
		dup["kid"] = "rsa-a"
		set := fileKeySet(t, writeJWKSFile(t, jwksDocumentOf(t, a.publicJWK(t), dup)))
		err := set.Refresh(context.Background())
		if err == nil || !strings.Contains(err.Error(), `declares kid "rsa-a" twice`) {
			t.Fatalf("a file declaring one kid twice must refuse whole, got %v", err)
		}
	})

	t.Run("a token with no kid is refused when two keys could verify it", func(t *testing.T) {
		a2 := newRSASigner(t, "rsa-a2", 2048)
		set := fileKeySet(t, writeJWKSFile(t, jwksDocumentOf(t, a.publicJWK(t), a2.publicJWK(t))))
		if err := set.Refresh(context.Background()); err != nil {
			t.Fatal(err)
		}
		_, err := Verify(a.sign(t, defaultClaims(), map[string]any{"kid": nil}), verifierOverSet(set))
		if !errors.Is(err, ErrNoKey) || !strings.Contains(err.Error(), "names no kid and 2 keys") {
			t.Fatalf("an unkeyed token against two candidate keys must be refused, got %v", err)
		}
		// Control: the same token WITH its kid verifies, so the refusal is about the kid.
		if _, err := Verify(a.sign(t, defaultClaims(), nil), verifierOverSet(set)); err != nil {
			t.Fatalf("control: the keyed token must verify: %v", err)
		}
	})

	t.Run("a key below the RSA floor is dropped", func(t *testing.T) {
		weak := newRSASigner(t, "rsa-weak", 1024)
		set := fileKeySet(t, writeJWKSFile(t, jwksDocumentOf(t, weak.publicJWK(t))))
		if err := set.Refresh(context.Background()); !errors.Is(err, ErrJWKSEmpty) {
			t.Fatalf("a file whose only key is below %d bits must yield no usable key, got %v", MinRSABits, err)
		}
	})
}
