package ui

import (
	"context"
	"fmt"
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
	uiChain, err := AuthBackends(machine, mustSupabaseBackend(t, authority), cookieBackend)
	if err != nil {
		t.Fatalf("ui.AuthBackends refused a full chain: %v", err)
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

// TestTheUIChainTriesEveryHeaderCREDENTIALBeforeTheAmBIENTCookie pins the ORDER of the
// chain this package builds, which is a different claim from its membership.
//
// 🔴 IT IS AN INVARIANT GUARD AND NOT A REGRESSION TEST, LABELLED AS ONE RATHER THAN
// COUNTED AS COVERAGE. No defect put the cookie ahead of the Supabase backend; what the
// change did was add a third member, and a three-member chain has an order that two did
// not. The hazard it stands against is stated in `identity.Chain`'s own comment: a request
// carrying BOTH a bearer token and a stale cookie must resolve as the token's principal,
// because a header is a credential the caller CHOSE to send and a cookie is one the browser
// attaches by itself — and the person debugging the other ordering would be reading a page
// rendered for a principal they did not ask to be.
//
// 🔴 IT ASSERTS THE POSITIONS BY TYPE, WHICH IS THE STATE, RATHER THAN THE PARAMETER ORDER,
// WHICH IS THE SPELLING. `AuthBackends` forwards to `identity.Backends` and could stop doing
// so; a test over the parameter list would go green for a local re-assembly that reversed
// the order.
//
// ⚠ THE MUTATION CONTROL THIS COMMENT FIRST RECORDED DOES NOT COMPILE, AND THE CORRECTION IS
// KEPT RATHER THAN SWAPPED BECAUSE THE ERROR WAS IN THE EVIDENCE, NOT THE GUARD. It read
// "swapping the second and third arguments of the `identity.Backends` call in `AuthBackends`
// makes this test report the cookie at index 1 and fail" — but those parameters are
// `*identity.SupabaseJWT` and `*identity.CookieSession`, so a swap is a TYPE ERROR and the
// build fails. A mutant that does not compile kills every test in the package and proves
// nothing about any of them, so this guard's red had never actually been watched.
//
// The control that DOES compile, and that has now been run: replace the `identity.Backends`
// call in `AuthBackends` with a hand-built `identity.Chain{cookie, machine, supabase}` — all
// three satisfy `Authenticator`, so it builds, and it is exactly the "second assembly of the
// chain" this constructor's comment warns about. It fails here at index 0. That is the row in
// the battery; the guard itself was sound all along.
func TestTheUIChainTriesEveryHeaderCREDENTIALBeforeTheAmBIENTCookie(t *testing.T) {
	authority := materializedAuthority(t)
	machine, err := identity.NewMachineToken(authority)
	if err != nil {
		t.Fatalf("the machine-token backend did not build: %v", err)
	}
	chain, err := AuthBackends(machine, mustSupabaseBackend(t, authority), mustCookieBackend(t))
	if err != nil {
		t.Fatalf("ui.AuthBackends refused a full chain: %v", err)
	}
	if len(chain) != 3 {
		t.Fatalf("the full UI chain has %d member(s), want 3 (machine token, Supabase JWT, cookie). "+
			"With a different length the index assertions below are about a chain nobody built.", len(chain))
	}
	// The expectation is the TYPE at each index, written out, because "every header-borne
	// credential before the ambient one" is a claim about positions and nothing else.
	for i, want := range []string{"*identity.MachineToken", "*identity.SupabaseJWT", "*identity.CookieSession"} {
		got := fmt.Sprintf("%T", chain[i])
		if got != want {
			t.Errorf("chain[%d] is %s, want %s. `identity.Chain`'s comment: every explicitly-presented "+
				"credential is tried before the one the browser sends by itself, so a request carrying a bearer "+
				"token AND a stale cookie must resolve as the token's principal.", i, got, want)
		}
	}
	t.Logf("chain order: %T, %T, %T", chain[0], chain[1], chain[2])
}

// mustSupabaseBackend builds a Supabase verifier over a SYNTHETIC issuer and JWKS URL.
//
// 🔴 EVERY HOST HERE IS `.invalid`, WHICH IS RESERVED BY RFC 2606 AND RESOLVES NOWHERE.
// This repository is public: a real project reference in a fixture is a hostname leak, and a
// fixture that LOOKS real is worse than one that is because nobody can tell. Nothing in this
// test fetches the JWKS, so the URL is never dialled — `NewKeySet` only parses it.
func mustSupabaseBackend(t *testing.T, authority *control.Cache) *identity.SupabaseJWT {
	t.Helper()
	keys, err := identity.NewKeySet(identity.JWKSOptions{
		URL: "https://idp.notes.example.invalid/auth/v1/.well-known/jwks.json",
	})
	if err != nil {
		t.Fatalf("the fixture key set did not build: %v", err)
	}
	backend, err := identity.NewSupabaseJWT(identity.SupabaseConfig{
		Authority: authority,
		Keys:      keys,
		Issuer:    "https://idp.notes.example.invalid/auth/v1",
		Audience:  identity.DefaultSupabaseAudience,
	})
	if err != nil {
		t.Fatalf("PRECONDITION FAILED: the fixture Supabase config did not build (%v), so the ordering "+
			"assertions have no second header-borne backend to place.", err)
	}
	return backend
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
	if _, err := AuthBackends(nil, nil, nil); err != identity.ErrNoBackends {
		t.Errorf("a chain with no backend must be %v, got %v", identity.ErrNoBackends, err)
	}
	// And a chain of the COOKIE backend alone is legal: it is a whole way to
	// authenticate, and `identity.Backends` skipping nil members is what makes that
	// true without a second assembly here.
	cookieOnly, err := AuthBackends(nil, nil, mustCookieBackend(t))
	if err != nil || len(cookieOnly) != 1 {
		t.Errorf("a cookie-only chain must be legal, got %v / %d members", err, len(cookieOnly))
	}
	// 🔴 AND A NIL SUPABASE BACKEND MUST NOT BE A MEMBER, WHICH IS THE CASE EVERY
	// DEPLOYMENT WITHOUT A PROVIDER IS IN. `identity.Backends` skips nil members, so this
	// asserts the skip is reached through THIS constructor: a chain that appended a typed
	// nil would put a member in it whose `Authenticate` panics on the first request.
	machineOnly, err := AuthBackends(mustMachineBackend(t), nil, nil)
	if err != nil || len(machineOnly) != 1 {
		t.Errorf("a machine-token-only chain must be legal and have exactly one member, got %v / %d members",
			err, len(machineOnly))
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

func mustMachineBackend(t *testing.T) *identity.MachineToken {
	t.Helper()
	backend, err := identity.NewMachineToken(materializedAuthority(t))
	if err != nil {
		t.Fatalf("the machine-token backend did not build: %v", err)
	}
	return backend
}

func mustCookieBackend(t *testing.T) *identity.CookieSession {
	t.Helper()
	backend, err := identity.NewCookieSession(uiTestSessions(t), materializedAuthority(t))
	if err != nil {
		t.Fatalf("the cookie backend did not build: %v", err)
	}
	return backend
}
