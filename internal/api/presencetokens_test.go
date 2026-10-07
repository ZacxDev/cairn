package api

import (
	"net/http"
	"testing"

	"github.com/ZacxDev/cairn/internal/presence"
)

// TestAPresenceTokenAuthenticatesNothingOnThePod is decision 3's isolation claim on the POD: a
// presence token presented to every read route is answered exactly as a random token is —
// status and body, byte for byte — while a real credential on the same request is served.
//
// ⚠ AN INVARIANT GUARD, NOT REGRESSION COVERAGE: the pod never sees the presence token file
// (that lives with `cairn-ui`'s agent listener), so nothing here ever accepted one. It pins that
// a later change cannot silently teach the pod one. The browser half is `internal/ui`'s
// `TestAPresenceTokenAuthenticatesNothingOnTheBrowserSurface`.
func TestAPresenceTokenAuthenticatesNothingOnThePod(t *testing.T) {
	h := newHarness(t)
	var tokens [3]string
	for i := range tokens {
		tok, err := presence.MintToken()
		if err != nil {
			t.Fatal(err)
		}
		tokens[i] = tok
	}
	push, claim, random := tokens[0], tokens[1], tokens[2]

	targets := []string{"/api/v1/recall/alpha-notes", "/api/v1/snapshot", "/api/v1/sessions/alpha-notes"}
	for _, target := range targets {
		real := h.do(t, http.MethodGet, target, wideToken, nil, "")
		if real.status != http.StatusOK {
			t.Fatalf("POSITIVE CONTROL FAILED: a real credential on GET %s answered %d", target, real.status)
		}
		want := h.do(t, http.MethodGet, target, random, nil, "")
		if want.status != http.StatusUnauthorized {
			t.Fatalf("CONTROL: a random token on GET %s answered %d", target, want.status)
		}
		for name, token := range map[string]string{"push": push, "claim": claim} {
			got := h.do(t, http.MethodGet, target, token, nil, "")
			if got.status != want.status || got.body != want.body {
				t.Errorf("GET %s: a %s presence token answered %d %q; a random token %d %q",
					target, name, got.status, got.body, want.status, want.body)
			}
		}
	}
}
