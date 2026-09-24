package main

import (
	"strings"
	"testing"

	"github.com/ZacxDev/cairn/internal/identity"
	"github.com/ZacxDev/cairn/internal/ui"
)

// fixtureVerifier is a Supabase backend over a SYNTHETIC issuer. Every host is `.invalid`,
// reserved by RFC 2606 and resolving nowhere, because this repository is public and a fixture
// that names a real project reference is a leak.
func fixtureVerifier(t *testing.T) *identity.SupabaseJWT {
	t.Helper()
	keys, err := identity.NewKeySet(identity.JWKSOptions{
		URL: "https://idp.notes.example.invalid/auth/v1/.well-known/jwks.json",
	})
	if err != nil {
		t.Fatalf("the fixture key set did not build: %v", err)
	}
	// The authority is this package's OWN journal fixture, so the verifier resolves against
	// the same shape the binary actually runs on rather than against a stub built here.
	authority, _ := seededJournal(t, credentialLive)
	backend, err := identity.NewSupabaseJWT(identity.SupabaseConfig{
		Authority: authority,
		Keys:      keys,
		Issuer:    "https://idp.notes.example.invalid/auth/v1",
		Audience:  identity.DefaultSupabaseAudience,
	})
	if err != nil {
		t.Fatalf("the fixture verifier did not build: %v", err)
	}
	return backend
}

// TestTheProviderFlowHasTHREEStatesAndTheMIDDLEOneIsNotAnError is the decision table for
// `providerSignIn`, written out because it is a decision and not a derivation.
//
// 🔴 THE MIDDLE STATE IS THE ONE A READER WOULD GET WRONG. A deployment with the Supabase
// ledger armed and NO redirect URL is not half-broken: it authenticates a BEARER JWT (an API
// caller) and offers no browser button, which is a configuration somebody may want. Making it
// a refusal would break that deployment; making it render a button would render one whose
// route answers 501.
//
// 🔴 AND THE FOURTH ROW IS THE HALF-CONFIGURATION AND IT *IS* A REFUSAL. A redirect URL with
// no verifier means the operator wrote down a callback and would get a button that refuses
// every sign-in — the shape `internal/identity`'s ledgers exist against, arriving through a
// variable outside them.
func TestTheProviderFlowHasTHREEStatesAndTheMIDDLEOneIsNotAnError(t *testing.T) {
	const redirect = "https://notes.example.invalid/sign-in/github/callback"

	for _, arm := range []struct {
		name     string
		redirect string
		armed    bool
		wantFlow bool
		wantErr  bool
	}{
		{"no Supabase at all", "", false, false, false},
		{"the ledger armed and no redirect URL", "", true, false, false},
		{"both", redirect, true, true, false},
		{"a redirect URL and no verifier", redirect, false, false, true},
	} {
		t.Run(arm.name, func(t *testing.T) {
			t.Setenv(EnvSupabaseRedirectURL, arm.redirect)
			var backend *identity.SupabaseJWT
			if arm.armed {
				backend = fixtureVerifier(t)
			}
			flow, err := providerSignIn(backend, arm.armed)
			if arm.wantErr {
				if err == nil {
					t.Fatal("want a refusal, got none — a callback URL with nothing to verify an exchanged " +
						"token against is a button that refuses every sign-in")
				}
				if !strings.Contains(err.Error(), identity.EnvSupabaseJWKSURL) {
					t.Errorf("the refusal does not name the variable that unblocks it: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected refusal: %v", err)
			}
			if (flow != nil) != arm.wantFlow {
				t.Errorf("a flow was built = %v, want %v", flow != nil, arm.wantFlow)
			}
			if flow != nil && flow.RedirectURL() != arm.redirect {
				t.Errorf("the flow's callback URL is %q, want %q", flow.RedirectURL(), arm.redirect)
			}
		})
	}
}

// TestAWhitespaceRedirectURLIsRefusedRatherThanReadAsUnset is the REGRESSION shape
// `EnvUIControlJournal` already carries, applied to the one new variable outside the ledger.
//
// 🔴 THE ZERO-WIDTH ARMS ARE THE POINT AND NOT THE WHITESPACE ONES. `strings.TrimSpace` calls
// a run of U+200B content, which is exactly how this repository measured a live 96-byte shared
// secret made entirely of invisible runes. `identity.ValueReducesToNothing` is the exported
// predicate that covers both spellings, and the test is what proves this call site reaches it
// rather than re-spelling it.
func TestAWhitespaceRedirectURLIsRefusedRatherThanReadAsUnset(t *testing.T) {
	for _, value := range []string{
		"   ",
		"\t\n",
		// Spelled as ESCAPES rather than as literal runes, and both halves of that matter: a
		// literal U+FEFF is a byte-order mark the Go compiler refuses outright, and a literal
		// U+200B is invisible in a diff - which is exactly why this class of value is the one
		// that got through.
		strings.Repeat("\u200b", 8), // ZERO WIDTH SPACE
		strings.Repeat("\ufeff", 4), // ZERO WIDTH NO-BREAK SPACE
		strings.Repeat("\u00a0", 4), // NO-BREAK SPACE: whitespace to Unicode, not to ASCII
	} {
		t.Setenv(EnvSupabaseRedirectURL, value)
		_, err := providerSignIn(fixtureVerifier(t), true)
		if err == nil {
			t.Errorf("%q was read as UNSET, so this surface would serve a sign-in page with no button while "+
				"the line the operator wrote said otherwise", value)
			continue
		}
		if !strings.Contains(err.Error(), EnvSupabaseRedirectURL) {
			t.Errorf("the refusal for %q does not name the variable: %v", value, err)
		}
	}

	// POSITIVE CONTROL: the same call site ACCEPTS a real value, so the refusals above are a
	// measurement rather than a function that refuses everything.
	t.Setenv(EnvSupabaseRedirectURL, "https://notes.example.invalid/sign-in/github/callback")
	if flow, err := providerSignIn(fixtureVerifier(t), true); err != nil || flow == nil {
		t.Errorf("POSITIVE CONTROL FAILED: a real callback URL was refused (%v / flow=%v), so the refusals "+
			"above prove nothing", err, flow != nil)
	}
}

// TestTheOAuthSettingIsInTheSupabaseNamespaceAndItsPathsComeFromTheLedger is what is left of
// a test that had grown two vacuous halves, and saying so is better than quietly shipping
// them.
//
// ⚠ TWO ASSERTIONS WERE DELETED AS MEASURING NOTHING. One walked THREE constants asserting a
// `CAIRN_SUPABASE_` prefix; two of them were the anon-key pair this round deleted, so it now
// walks one. The other read
// `strings.HasSuffix("https://host.invalid"+ui.OAuthCallbackPath, ui.OAuthCallbackPath)` —
// which is true for EVERY value of that constant, including the empty string. A tautology in
// a guard is worse than no guard: it reads as coverage and stops anyone looking.
//
// ⚠ AND THE CLAIM THE OLD NAME MADE IS COVERED ELSEWHERE, WHICH IS WHY IT IS NOT REBUILT
// HERE. "A refusal names the variable that unblocks it" is asserted against the real error
// text by `TestTheProviderFlowHasTHREEStatesAndTheMIDDLEOneIsNotAnError` (which requires
// `identity.EnvSupabaseJWKSURL` in the half-configured refusal) and by
// `TestAWhitespaceRedirectURLIsRefusedRatherThanReadAsUnset` (which requires
// `EnvSupabaseRedirectURL` in every blank refusal). Restating it over the constants would be
// the same claim with less information.
func TestTheOAuthSettingIsInTheSupabaseNamespaceAndItsPathsComeFromTheLedger(t *testing.T) {
	if !strings.HasPrefix(EnvSupabaseRedirectURL, "CAIRN_SUPABASE_") {
		t.Errorf("%q is not in the CAIRN_SUPABASE_* namespace, which is where every setting of this "+
			"integration lives", EnvSupabaseRedirectURL)
	}
	// The callback PATH half comes from `internal/ui` rather than being written twice: the
	// operator's `GOTRUE_URI_ALLOW_LIST` entry has to match the route this binary serves, and
	// an empty constant would make the refusal text name nothing.
	if ui.OAuthCallbackPath == "" || ui.OAuthStartPath == "" {
		t.Error("an OAuth path constant is EMPTY, so the refusal that quotes it tells an operator nothing " +
			"and the route it names is not the one the ledger declares")
	}
}
