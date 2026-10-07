package ui

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/ZacxDev/cairn/internal/presence"
)

// TestAPresenceTokenAuthenticatesNothingOnTheBrowserSurface is decision 3's isolation claim on
// THIS surface: a presence token — push or claim, freshly minted — presented as a bearer to
// every GET row, and pasted into `POST /sign-in`, is answered BYTE FOR BYTE as a random token is.
// A real control-plane credential on the same request IS accepted (the positive control), so the
// equality is not two refusals of a request that could never succeed.
//
// ⚠ AN INVARIANT GUARD, NOT REGRESSION COVERAGE: presence tokens are stored only in the agent
// listener's token file and are unknown to `identity.Backends`, so no code path ever accepted
// one here. It pins that a later change wiring presence into the browser chain cannot do so
// silently. The pod's half is `internal/api`'s `TestAPresenceTokenAuthenticatesNothingOnThePod`.
func TestAPresenceTokenAuthenticatesNothingOnTheBrowserSurface(t *testing.T) {
	push, err := presence.MintToken()
	if err != nil {
		t.Fatal(err)
	}
	claim, err := presence.MintToken()
	if err != nil {
		t.Fatal(err)
	}
	random, err := presence.MintToken()
	if err != nil {
		t.Fatal(err)
	}
	l := newLive(t)
	get := func(path, bearer string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodGet, path, nil)
		r.Host = testHost
		r.Header.Set("Authorization", "Bearer "+bearer)
		rec := httptest.NewRecorder()
		l.srv.ServeHTTP(rec, r)
		return rec
	}

	if real := get(RootPath, testCredential); real.Code != http.StatusOK {
		t.Fatalf("POSITIVE CONTROL FAILED: a real credential on GET / answered %d", real.Code)
	} else if garbage := get(RootPath, random); garbage.Code == http.StatusOK || garbage.Body.String() == real.Body.String() {
		t.Fatalf("POSITIVE CONTROL FAILED: a random token is answered like the real credential (%d)", garbage.Code)
	}

	gets := 0
	for _, line := range DeclaredRoutes() {
		method, path, _ := strings.Cut(line, " ")
		if method != http.MethodGet {
			continue
		}
		gets++
		want := get(path, random)
		for name, token := range map[string]string{"push": push, "claim": claim} {
			got := get(path, token)
			if got.Code != want.Code || got.Body.String() != want.Body.String() ||
				got.Header().Get("Location") != want.Header().Get("Location") {
				t.Errorf("GET %s: a %s presence token answered %d, a random token %d (or the bodies differ)",
					path, name, got.Code, want.Code)
			}
		}
	}
	if gets < 10 {
		t.Fatalf("only %d GET rows were probed — the ledger walk narrowed", gets)
	}

	signIn := func(token string) *httptest.ResponseRecorder {
		return l.do(http.MethodPost, SignInPath, url.Values{FieldToken: {token}})
	}
	want := signIn(random)
	if want.Code != http.StatusUnauthorized {
		t.Fatalf("CONTROL: a random token's sign-in answered %d", want.Code)
	}
	for _, token := range []string{push, claim} {
		got := signIn(token)
		if got.Code != want.Code || got.Body.String() != want.Body.String() || sessionCookieOf(t, got) != nil {
			t.Fatalf("POST /sign-in with a presence token answered %d (random: %d) or set a cookie", got.Code, want.Code)
		}
	}
	if rec := signIn(testCredential); rec.Code != http.StatusSeeOther {
		t.Fatalf("POSITIVE CONTROL FAILED: the real credential's sign-in answered %d", rec.Code)
	}
}
