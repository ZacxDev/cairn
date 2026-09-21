package ui

import (
	"context"
	"testing"

	"github.com/ZacxDev/cairn/internal/control"
	"github.com/ZacxDev/cairn/internal/identity"
)

// emptySource is a `control.Source` over an empty world. Nothing here authenticates
// anybody; what these tests measure is which BACKENDS are in the chain, which is a
// property of the wiring rather than of any credential.
type emptySource struct{}

func (emptySource) Model(context.Context) (control.Model, error) { return control.Model{}, nil }

func materializedAuthority(t *testing.T) *control.Cache {
	t.Helper()
	c := control.NewCache(emptySource{}, control.CacheOptions{})
	if err := c.Refresh(context.Background()); err != nil {
		t.Fatalf("materializing the empty fixture world: %v", err)
	}
	return c
}

// TestTheUIChainHasNoTrustedHeaderMember pins the membership of the chain this
// package builds.
//
// 🔴 IT INSPECTS THE RETURNED CHAIN, NOT THE SIGNATURE. `AuthBackends` takes no
// trusted-header parameter, and that alone is not the guard: a signature constrains
// one constructor, and `identity.Chain` is a slice anybody can append to. What is
// asserted here is that no member of the value this package hands to `ui.New` is a
// `*identity.TrustedHeader` — the STATE, not the spelling.
//
// 🔴 AND IT CARRIES ITS POSITIVE CONTROL IN THE SAME FUNCTION, BECAUSE "NO
// TRUSTED-HEADER MEMBER" IS A ZERO AND A ZERO IS INDISTINGUISHABLE FROM A TEST THAT
// CANNOT SEE ONE. The same live `*identity.TrustedHeader` value is passed to
// `identity.Backends`, where it MUST appear. If it does not, the type assertion
// below is broken and its absence from the UI chain means nothing.
//
// The hazard it stands against is stated in `internal/identity/README.md`: on a pod
// that is reachable directly, this backend lets anyone who can open a socket BE any
// user in the control plane, at that user's full authority, on every route. A
// browser surface exists to be reachable.
func TestTheUIChainHasNoTrustedHeaderMember(t *testing.T) {
	authority := materializedAuthority(t)

	machine, err := identity.NewMachineToken(authority)
	if err != nil {
		t.Fatalf("the machine-token backend did not build: %v", err)
	}

	trusted, err := identity.NewTrustedHeader(identity.TrustedHeaderConfig{
		ProxyFronted:  true,
		SubjectHeader: "X-Forwarded-User",
		// Generated at test time rather than pasted: this repository is public, and a
		// secret that LOOKS real is worse than one that is, because nobody can tell.
		Secret:    []byte("fixture-shared-secret-not-a-credential"),
		Provider:  "fixture-provider",
		Authority: authority,
	})
	if err != nil {
		t.Fatalf("PRECONDITION FAILED: the fixture trusted-header config did not build (%v), so the positive "+
			"control below has no value to look for and this test measures nothing.", err)
	}

	// POSITIVE CONTROL — the assertion can see a trusted-header member when there is
	// one. Reported as a pair with the zero below, never on its own.
	full, err := identity.Backends(machine, nil, nil, trusted)
	if err != nil {
		t.Fatalf("identity.Backends refused the control chain: %v", err)
	}
	controlCount := countTrustedHeaders(full)
	if controlCount == 0 {
		t.Fatalf("POSITIVE CONTROL FAILED: identity.Backends was handed a live *identity.TrustedHeader and the "+
			"membership check found none in the %d-member chain it returned. The check is broken, so its zero on "+
			"the UI chain below would mean nothing.", len(full))
	}

	// THE PIN — and it is taken over the WIDEST chain this constructor can build, with
	// the cookie backend present. A pin taken over the narrowest one would go green for
	// a `AuthBackends` that forwarded a trusted header only when a cookie backend was
	// also supplied.
	cookieBackend, err := identity.NewCookieSession(uiTestSessions(t), authority)
	if err != nil {
		t.Fatalf("the cookie backend did not build: %v", err)
	}
	uiChain, err := AuthBackends(machine, cookieBackend)
	if err != nil {
		t.Fatalf("ui.AuthBackends refused a machine-token-and-cookie chain: %v", err)
	}
	if len(uiChain) == 0 {
		t.Fatal("ui.AuthBackends returned an EMPTY chain, which authenticates nobody — and an empty chain also " +
			"trivially contains no trusted-header member, so the assertion below would pass for the wrong reason.")
	}
	if n := countTrustedHeaders(uiChain); n != 0 {
		t.Errorf("THE UI CHAIN CONTAINS %d *identity.TrustedHeader MEMBER(S). "+
			"internal/identity/README.md: on a directly-reachable surface that backend lets anyone who can open a "+
			"socket BE any user in this control plane, at that user's full authority, on every route, with the "+
			"writes attributed to them. A browser endpoint is directly reachable BY DESIGN — that is what a "+
			"browser endpoint is. The backend must not be reachable from this binary at any setting.", n)
	}

	t.Logf("trusted-header members: %d in the identity.Backends control chain (%d members), %d in the UI chain (%d members)",
		controlCount, len(full), countTrustedHeaders(uiChain), len(uiChain))
}

func countTrustedHeaders(chain identity.Chain) int {
	n := 0
	for _, backend := range chain {
		if _, is := backend.(*identity.TrustedHeader); is {
			n++
		}
	}
	return n
}

// TestAnEmptyUIChainIsRefusedAtConstruction pins the fail-closed direction one level
// up from `identity.ErrNoBackends`: a UI server with no authenticator must not build.
func TestAnEmptyUIChainIsRefusedAtConstruction(t *testing.T) {
	if _, err := AuthBackends(nil, nil); err != identity.ErrNoBackends {
		t.Errorf("a chain with no backend must be %v, got %v", identity.ErrNoBackends, err)
	}
	// And a chain of the COOKIE backend alone is legal: it is a whole way to
	// authenticate, and `identity.Backends` skipping nil members is what makes that
	// true without a second assembly here.
	cookieOnly, err := AuthBackends(nil, mustCookieBackend(t))
	if err != nil || len(cookieOnly) != 1 {
		t.Errorf("a cookie-only chain must be legal, got %v / %d members", err, len(cookieOnly))
	}
	// `ui.New`'s own refusals are pinned per sentinel by
	// `TestANilPartIsRefusedAtConstruction`; this file's subject is the CHAIN.
}

// uiTestSessions and mustCookieBackend build the one dependency the chain tests need. A
// real `FileSessionStore` rather than a stub, because the type assertions below are
// about MEMBERSHIP and a stub would be a second implementation of the thing under test.
func uiTestSessions(t *testing.T) *identity.FileSessionStore {
	t.Helper()
	return mustSessions(t)
}

func mustCookieBackend(t *testing.T) *identity.CookieSession {
	t.Helper()
	backend, err := identity.NewCookieSession(uiTestSessions(t), materializedAuthority(t))
	if err != nil {
		t.Fatalf("the cookie backend did not build: %v", err)
	}
	return backend
}
