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

// TestTheProviderFlowDecisionTable is `providerSignIn`'s decision table, written out because
// it is a decision and not a derivation.
//
// ⚠ IT WAS CALLED `…HasTHREEStatesAndTheMIDDLEOneIsNotAnError` AND THE COUNT WENT STALE IN THE
// NEXT COMMIT — the startup path check added two more refusals. The number is out of the name
// for the same reason `README.md` lost "Seven routes": a count in a label beside a table that
// grows is a second spelling that only ever goes stale. The MIDDLE state is still the one worth
// naming, and it is named below.
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
func TestTheProviderFlowDecisionTable(t *testing.T) {
	const redirect = "https://notes.example.invalid/sign-in/github/callback"

	// 🔴 THE LAST FOUR ROWS ARE THE STARTUP PATH CHECK, WHICH SHIPPED WITH NO ROW AT ALL AND
	// THEREFORE NO GUARD. Measured: mutating its condition to `false` survived all 27 tests in
	// this package, because the only URL any of them fed it happened to have a matching path.
	// A check whose guard is "the fixture happens to satisfy it" is a check nobody is holding.
	for _, arm := range []struct {
		name     string
		redirect string
		armed    bool
		wantFlow bool
		wantErr  bool
		// wantErrNames, when set, must appear in the refusal — so a row cannot be satisfied
		// by the WRONG refusal, which is how a decision table goes green about nothing.
		wantErrNames string
	}{
		{"no Supabase at all", "", false, false, false, ""},
		{"the ledger armed and no redirect URL", "", true, false, false, ""},
		{"both", redirect, true, true, false, ""},
		{"a redirect URL and no verifier", redirect, false, false, true, identity.EnvSupabaseJWKSURL},

		// The path check. Each of these is a real way to get the variable wrong.
		{"a callback path that is not the route", "https://notes.example.invalid/callback",
			true, false, true, ui.OAuthCallbackPath},
		{"a path that is a PREFIX of the route but not the route",
			"https://notes.example.invalid/sign-in/github", true, false, true, ui.OAuthCallbackPath},
		// ⚠ THE UNPARSEABLE FIXTURE IS A BAD PERCENT ESCAPE, NOT A CONTROL CHARACTER. The first
		// version used `\x7f\x00`, which `url.Parse` does reject — and which `t.Setenv` cannot
		// set at all ("setenv: invalid argument"), so the row failed on the HARNESS rather than
		// on the check. A fixture that cannot reach the code under test measures nothing. This
		// one is also the realistic shape: a broken template substitution.
		{"a URL that does not parse", "https://notes.example.invalid/%zz/sign-in/github/callback",
			true, false, true, EnvSupabaseRedirectURL},

		// 🔴 AND THE ROW THE EQUALITY VERSION OF THIS CHECK REFUSED: a surface behind a
		// path-prefixing proxy. It served `/sign-in/github/callback` after the prefix was
		// stripped, started and worked before the check existed, and exited 78 with it. A
		// suffix keeps the typo-catching value and admits this.
		{"a callback behind a path-prefixing proxy",
			"https://notes.example.invalid/cairn/sign-in/github/callback", true, true, false, ""},
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
					t.Fatalf("want a refusal for %q, got none. Every row here is a way to get this "+
						"configuration wrong such that the surface would come up announcing a live "+
						"button and then answer 404 or 401 at the first click.", arm.redirect)
				}
				if arm.wantErrNames != "" && !strings.Contains(err.Error(), arm.wantErrNames) {
					t.Errorf("the refusal does not name %q, so this row could be satisfied by a DIFFERENT "+
						"refusal than the one it is about: %v", arm.wantErrNames, err)
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
