package identity

// The JWKS file source, reached the way an OPERATOR reaches it: through the environment.
//
// ⚠ THIS FILE SPELLS THE VARIABLE AS A LITERAL AND USES ONLY API THAT PREDATES IT, AND THAT
// IS DELIBERATE. It is the half of the file-source coverage that compiles against the tree
// BEFORE the source existed, so it is the half that can be watched RED there: on that tree
// `CAIRN_OIDC_JWKS_FILE` is a name no ledger reads, an issuer alone is `ErrSupabaseNoKeys`,
// and a file beside a URL is silently ignored. `jwksfile_test.go` holds the rest, written
// against the new symbols, and cannot compile there at all.

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ZacxDev/cairn/internal/control"
)

const jwksFileVariable = "CAIRN_OIDC_JWKS_FILE"

// writeJWKSFile writes a document to a fresh absolute path and returns it.
func writeJWKSFile(t *testing.T, body []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "jwks.json")
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestAJWKSFileAloneArmsAWorkingVerifier is the end-to-end claim: environment → key set →
// a token minted by the file's key authenticates as the user this control plane holds — and
// nothing configured a URL.
func TestAJWKSFileAloneArmsAWorkingVerifier(t *testing.T) {
	s := newECSigner(t, "ec-1")
	path := writeJWKSFile(t, jwksDocumentOf(t, s.publicJWK(t)))

	_, backend, err := FromEnvironment(map[string]string{
		EnvSupabaseIssuer: testIssuer,
		jwksFileVariable:  path,
	}, newTestAuthority(t), newTestSessionAuthority(t))
	if err != nil {
		t.Fatalf("an issuer and a JWKS file must build a verifier: %v", err)
	}
	if backend == nil {
		t.Fatal("the environment armed no OIDC backend")
	}
	if err := backend.RefreshKeys(context.Background()); err != nil {
		t.Fatalf("the file must materialize: %v", err)
	}

	// The backend's clock is the real one, so the token is minted against it rather than
	// against the year-2000 fixture clock the library tests pin.
	now := time.Now()
	claims := defaultClaims()
	claims["iat"], claims["exp"] = now.Add(-time.Minute).Unix(), now.Add(time.Hour).Unix()

	id, err := backend.AuthenticateToken(s.sign(t, claims, nil))
	if err != nil {
		t.Fatalf("a token signed by the file's key must authenticate: %v", err)
	}
	if id.Principal.Kind != control.KindUser || id.Principal.ID != testUserID {
		t.Fatalf("authenticated as %+v, want the fixture user %s", id.Principal, testUserID)
	}

	// The negative control on the same backend: a key the file does NOT carry is refused,
	// so the success above is about the file's contents and not a verifier that accepts
	// everything.
	stranger := newECSigner(t, "ec-1") // same kid, different key
	if _, err := backend.AuthenticateToken(stranger.sign(t, claims, nil)); err == nil {
		t.Fatal("a token signed by a key the file does not hold must be refused")
	}
}

// TestAJWKSFileBesideAJWKSURLIsRefusedRatherThanRanked — through the environment, on text
// that predates nothing (the sentinel's own sentence).
func TestAJWKSFileBesideAJWKSURLIsRefusedRatherThanRanked(t *testing.T) {
	path := writeJWKSFile(t, jwksDocumentOf(t, newECSigner(t, "ec-1").publicJWK(t)))
	_, _, err := FromEnvironment(map[string]string{
		EnvSupabaseIssuer:  testIssuer,
		EnvSupabaseJWKSURL: "https://notes-idp.example.test/jwks",
		jwksFileVariable:   path,
	}, newTestAuthority(t), newTestSessionAuthority(t))
	if err == nil {
		t.Fatal("a JWKS URL and a JWKS file together built a verifier; one of them is being silently ignored")
	}
	if !strings.Contains(err.Error(), "one key set, one source") {
		t.Fatalf("the refusal must say why the pair is refused, got %v", err)
	}
	if errors.Is(err, ErrBlankSetting) {
		t.Fatalf("two real values were refused as blank: %v", err)
	}
}
