package identity

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ZacxDev/cairn/internal/envalias"
)

// The verifier's settings are `CAIRN_OIDC_*` and their `CAIRN_SUPABASE_*` spellings still
// resolve. This file is the gate on that, and on the ONE place the two resolvers part.
//
// 🔴 WHICH CASES HERE ARE REGRESSION COVERAGE AND WHICH ARE INVARIANT GUARDS, STATED
// RATHER THAN LEFT TO BE INFERRED, BECAUSE THE DIRECTION DECIDES IT. Setting the
// DEPRECATED spelling was the only spelling at the base commit, so a case that asserts it
// still works is green there by construction — the deprecation window's own claim, which
// no defect ever violated. The cases that attribute are the ones naming `CAIRN_OIDC_*`:
// nothing at the base reads those names at all, so the ledger is unarmed, the backend is
// nil and they are RED. Each case below says which it is.
//
// 🔴 HOW THE BASELINE WAS OBTAINED, AND WHY IT IS NOT "THIS FILE COPIED OVER THE BASE
// TREE" — WHICH IS THE IDIOM EVERY OTHER MATRIX IN THIS REPOSITORY USES. This file CANNOT
// COMPILE at `d7e1fec`: the two spellings are the SAME NAME there, so every case that sets
// both is a `duplicate key in map literal`, and `envalias.OldName` and `written` do not
// exist. Reporting "red at base" off a build failure would be true and would attribute
// nothing. So the base was measured with a PROBE — the same three claims, with the current
// names spelled as LITERALS and the structurally-inexpressible cases dropped — run via
// `git archive d7e1fec` into a scratch tree, and then run again unchanged against HEAD:
//
//	                                        d7e1fec   HEAD
//	the CAIRN_OIDC_* names configure it     FAIL      ok    (base: armed=false, no backend)
//	a blank deprecated name takes the
//	  setting's own refuseBlank branch       FAIL      ok    (base: the `nothing else armed
//	                                                          this ledger` branch instead —
//	                                                          it cannot read the companions)
//	a deprecated name WARNS naming its
//	  replacement                            FAIL      ok    (base: 0 lines; the pair is in
//	                                                          no ledger there)
//
// ⚠ THE PROBE IS NOT COMMITTED, AND SAYING SO IS THE POINT: it is a BASE-ERA spelling of
// claims this file states against the current constants. The committed cases are wider —
// seven resolved values rather than three, both shadowing directions, and the seam — and
// three of them are invariant guards, labelled as such below.
//
// Base ref for every matrix in this file: **`d7e1fec`** (`origin/main` at the time).

// The fixture values. 🔴 NONE OF THEM CAN COINCIDE WITH A CONSTANT UNDER TEST, which is
// what stops a mutant that hardcodes a default from surviving: the audience is not
// `DefaultSupabaseAudience`, the provider is not `DefaultSupabaseProvider`, and neither
// duration is zero (the value `checkClaims` reads as "the check is off").
const (
	oidcFixtureAudience = "cairn-oidc-fixture-aud"
	oidcFixtureProvider = "fixture-idp"
	oidcFixtureRole     = "fixture-member"
	oidcFixtureLeeway   = 17 * time.Second
	oidcFixtureMaxAge   = 41 * time.Minute
)

// oidcFixtureJWKS is a URL nothing serves. `NewKeySet` does not fetch at construction, so
// every case here is about CONFIGURATION and never about reaching a provider.
const oidcFixtureJWKS = "https://notes-idp.example.test/auth/v1/.well-known/jwks.json"

// currentSpellingEnv is a complete verifier configuration under the CURRENT names, built
// from the exported constants so it cannot drift from them.
func currentSpellingEnv() map[string]string {
	return map[string]string{
		EnvSupabaseJWKSURL:     oidcFixtureJWKS,
		EnvSupabaseIssuer:      testIssuer,
		EnvSupabaseAudience:    oidcFixtureAudience,
		EnvSupabaseProvider:    oidcFixtureProvider,
		EnvSupabaseRequireRole: oidcFixtureRole,
		EnvSupabaseLeeway:      oidcFixtureLeeway.String(),
		EnvSupabaseMaxAge:      oidcFixtureMaxAge.String(),
	}
}

// deprecatedSpellingEnv is the same configuration under the DEPRECATED names, spelled as
// LITERALS on purpose: taking them from `envalias.OldName` would make the ledger its own
// oracle, so an emptied ledger would leave every case below asserting nothing.
func deprecatedSpellingEnv() map[string]string {
	return map[string]string{
		"CAIRN_SUPABASE_JWKS_URL":     oidcFixtureJWKS,
		"CAIRN_SUPABASE_ISSUER":       testIssuer,
		"CAIRN_SUPABASE_AUDIENCE":     oidcFixtureAudience,
		"CAIRN_SUPABASE_PROVIDER":     oidcFixtureProvider,
		"CAIRN_SUPABASE_REQUIRE_ROLE": oidcFixtureRole,
		"CAIRN_SUPABASE_LEEWAY":       oidcFixtureLeeway.String(),
		"CAIRN_SUPABASE_MAX_AGE":      oidcFixtureMaxAge.String(),
	}
}

// assertFixtureVerifier reads every configured value back off the built backend.
//
// 🔴 IT ASSERTS SEVEN VALUES AND NOT "A BACKEND WAS BUILT", BECAUSE SIX OF THE SEVEN
// SETTINGS HAVE A WORKING DEFAULT OR AN OFF STATE. A resolver that found only the JWKS URL
// and the issuer builds successfully — audience defaults, provider defaults, the role check
// is off, both durations are zero — so "armed, no error" is satisfied by a reader that lost
// five of the seven aliases.
func assertFixtureVerifier(t *testing.T, backend *SupabaseJWT) {
	t.Helper()
	if backend == nil {
		t.Fatal("no backend was built, so there is nothing to read the resolved settings off")
	}
	for _, check := range []struct {
		name string
		got  any
		want any
	}{
		{"issuer", backend.verify.Issuer, testIssuer},
		{"audience", backend.verify.Audience, oidcFixtureAudience},
		{"provider", backend.provider, oidcFixtureProvider},
		{"requireRole", backend.requireRole, oidcFixtureRole},
		{"leeway", backend.verify.Leeway, oidcFixtureLeeway},
		{"maxAge", backend.verify.MaxAge, oidcFixtureMaxAge},
	} {
		if check.got != check.want {
			t.Errorf("%s resolved to %v, want %v — that setting's spelling did not reach the "+
				"constructor", check.name, check.got, check.want)
		}
	}
	if backend.keys == nil {
		t.Error("no key set was built, so the JWKS URL did not reach the constructor")
	}
}

// TestTheCurrentOIDCSpellingConfiguresTheVerifier is REGRESSION coverage, and it is the
// case that attributes.
//
//	d7e1fec  FAIL — armed=false, backend=nil: nothing at that commit reads a
//	         `CAIRN_OIDC_*` name, so the ledger is not armed by any of the seven and
//	         `assertFixtureVerifier` fails on its first line
//	HEAD     ok
func TestTheCurrentOIDCSpellingConfiguresTheVerifier(t *testing.T) {
	backend, armed, err := SupabaseBackendFromEnvironment(currentSpellingEnv(), newTestSessionAuthority(t))
	if err != nil {
		t.Fatalf("a complete configuration under the current names was refused: %v", err)
	}
	if !armed {
		t.Fatal("a complete configuration under the current names did not ARM the ledger, so a " +
			"deployment that migrated its manifest would come up with no verifier at all and a " +
			"health check that passes")
	}
	assertFixtureVerifier(t, backend)
}

// TestTheDeprecatedSupabaseSpellingStillConfiguresTheVerifier is an INVARIANT GUARD and is
// NOT counted as regression coverage: at `d7e1fec` these names WERE the current names, so
// it is green at both ends. What it pins is the deprecation window itself — a deployment
// that has not migrated keeps working — which is the half a rename most easily breaks and
// which no test would otherwise hold.
func TestTheDeprecatedSupabaseSpellingStillConfiguresTheVerifier(t *testing.T) {
	backend, armed, err := SupabaseBackendFromEnvironment(deprecatedSpellingEnv(), newTestSessionAuthority(t))
	if err != nil {
		t.Fatalf("a complete configuration under the deprecated names was refused: %v", err)
	}
	if !armed {
		t.Fatal("the deprecated spelling did not ARM the ledger. Both spellings must work until " +
			"the removal anchor, and a deployment that never migrated would otherwise come up " +
			"with no verifier and a passing health check")
	}
	assertFixtureVerifier(t, backend)
}

// TestTheCurrentNameWinsOverTheDeprecatedOne is REGRESSION coverage.
//
//	d7e1fec  FAIL — the `CAIRN_OIDC_*` half is unread, so every value resolves to the
//	         DEPRECATED side's and the four assertions below report the shadowed values
//	HEAD     ok
//
// 🔴 THE TWO SIDES CARRY DIFFERENT VALUES IN EVERY FIELD, which is what makes this a
// measurement rather than a tautology: a resolver that read either side exclusively
// satisfies one of the two tests above, and only disagreeing values can tell which side
// won.
func TestTheCurrentNameWinsOverTheDeprecatedOne(t *testing.T) {
	env := currentSpellingEnv()
	for name, value := range deprecatedSpellingEnv() {
		env[name] = "shadowed-" + value
	}
	env["CAIRN_SUPABASE_LEEWAY"] = "1s"
	env["CAIRN_SUPABASE_MAX_AGE"] = "2m"

	backend, armed, err := SupabaseBackendFromEnvironment(env, newTestSessionAuthority(t))
	if err != nil {
		t.Fatalf("both spellings set was refused: %v", err)
	}
	if !armed {
		t.Fatal("both spellings set did not arm the ledger")
	}
	assertFixtureVerifier(t, backend)
}

// TestABlankSettingIsNamedInTheSPELLINGTheOperatorWROTE is REGRESSION coverage, and it is
// the case the alias lookup's placement exists for.
//
// 🔴 A REFUSAL THAT RENAMES THE LINE SENDS THE OPERATOR TO A LINE THAT DOES NOT EXIST. The
// blank policy's whole product is an `os.Exit(78)` message quoting the offending variable;
// resolving the alias without carrying the spelling back would print `CAIRN_OIDC_LEEWAY` to
// somebody whose manifest says `CAIRN_SUPABASE_LEEWAY` — the same mis-blame
// `envalias.FileWarning` exists against one level down.
//
//	d7e1fec  FAIL — both arms. The ledger cannot be armed by `CAIRN_OIDC_*` names there,
//	         so the deprecated-spelling arm produces a fault about a ledger NOTHING
//	         armed (a different `because`, and the companion settings unseen), and the
//	         current-spelling arm produces no fault about this setting at all
//	HEAD     ok
//
// ⚠ BOTH ARMS, BECAUSE ONE ALONE IS WALKABLE. A reader that always printed the deprecated
// spelling satisfies the first arm and fails the second; one that always printed the
// current spelling does the reverse.
func TestABlankSettingIsNamedInTheSPELLINGTheOperatorWROTE(t *testing.T) {
	for _, arm := range []struct {
		name string
		// blank is the name the operator wrote the whitespace under.
		blank string
		// absent is the OTHER spelling of the same setting, which must not appear.
		absent string
	}{
		{"the deprecated spelling", "CAIRN_SUPABASE_REQUIRE_ROLE", "CAIRN_OIDC_REQUIRE_ROLE"},
		{"the current spelling", "CAIRN_OIDC_REQUIRE_ROLE", "CAIRN_SUPABASE_REQUIRE_ROLE"},
	} {
		t.Run(arm.name, func(t *testing.T) {
			// Armed by the CURRENT spelling of two other settings, so the fault is the
			// setting's own `refuseBlank` policy and not "nothing else armed this ledger".
			env := map[string]string{
				EnvSupabaseJWKSURL: oidcFixtureJWKS,
				EnvSupabaseIssuer:  testIssuer,
				arm.blank:          "   ",
			}
			_, _, err := SupabaseBackendFromEnvironment(env, newTestSessionAuthority(t))
			if !errors.Is(err, ErrBlankSetting) {
				t.Fatalf("a whitespace %s was not refused as a blank setting: %v", arm.blank, err)
			}
			if !strings.Contains(err.Error(), arm.blank) {
				t.Errorf("the refusal does not name %s, which is the line the operator has to "+
					"edit:\n%s", arm.blank, err)
			}
			// 🔴 AND IT MUST BE THE SETTING'S OWN `refuseBlank` BRANCH, NOT THE
			// NOTHING-ELSE-ARMED ONE. Both produce `ErrBlankSetting` and both name this
			// variable, so the sentinel and the name together do not separate them — and the
			// unarmed branch is exactly what a tree that cannot read the `CAIRN_OIDC_*`
			// companions produces, which is what the base measurement found.
			if !strings.Contains(err.Error(), "read as UNSET") {
				t.Errorf("the refusal is the `nothing else armed this ledger` branch rather than "+
					"this setting's own policy, so the two companions set under their CURRENT "+
					"spelling did not arm the ledger:\n%s", err)
			}
			if strings.Contains(err.Error(), arm.absent) {
				t.Errorf("the refusal names %s, which is NOT in this deployment's manifest — an "+
					"operator told to fix it goes looking for a line that does not exist:\n%s",
					arm.absent, err)
			}
		})
	}
}

// TestAWhitespaceCurrentNameIsREFUSEDRatherThanFallingThroughToTheDeprecatedOne pins the
// ONE place this package's resolver and `envalias.Value` deliberately part, and it is an
// INVARIANT GUARD on new behaviour rather than regression coverage — the configuration it
// describes could not exist at the base commit, where there was only one spelling.
//
// 🔴 `envalias.blank` IS `TrimSpace(v) == ""`, SO `envalias.Value` WOULD READ THE
// WHITESPACE AS ABSENT AND RESOLVE THE DEPRECATED NAME. That is correct in that package and
// is exactly what this one refuses: a setting that is present and reduces to nothing is a
// misconfiguration named in a startup refusal. The divergence is in the LOUD direction, and
// this is where it is written down as a test rather than only as a comment.
//
// 🔴 WHICH REFUSAL IT IS, IS THE SETTING'S OWN DECLARED POLICY AND NOT ONE ANSWER — AND
// THIS TEST'S FIRST DRAFT ASSERTED `ErrBlankSetting` HERE AND WAS MEASURED WRONG.
// `EnvSupabaseIssuer` declares `refusedByConstructor`, so a blank is accepted as UNSET by
// the reader and lands on `ErrSupabaseNoIssuer` — deliberately, so `""` and `"  "` cannot
// reach two different refusals for one configuration. The sibling case below drives a
// `refuseBlank` setting through the same shadowing and gets `ErrBlankSetting`. Two
// policies, two rungs, one claim: neither reads the whitespace as absent and takes the
// deprecated spelling's value.
//
// ⚠ THE EMPTY-STRING ARM IS THE POSITIVE CONTROL AND IS ALSO WHERE THE TWO RESOLVERS
// AGREE. `X=""` is "not using this" everywhere in this file, so it falls through to the
// deprecated spelling and the backend builds — which is what stops the arms above from
// being satisfied by a guard that refuses every value, and what makes the manifest shape
// that emits every variable with an empty default keep working.
func TestAWhitespaceCurrentNameIsREFUSEDRatherThanFallingThroughToTheDeprecatedOne(t *testing.T) {
	base := func(shadow string) map[string]string {
		return map[string]string{
			EnvSupabaseJWKSURL:      oidcFixtureJWKS,
			"CAIRN_SUPABASE_ISSUER": testIssuer,
			EnvSupabaseIssuer:       shadow,
		}
	}

	// The last value is ZERO-WIDTH rather than whitespace, spelled as an escape because an
	// invisible rune in source is unreadable: `TrimSpace` calls it CONTENT, so it is the
	// spelling that separates this package's `reducesToNothing` from `envalias.blank`
	// rather than merely being stricter than it.
	for _, shadow := range []string{" ", "   ", "\t\n ", strings.Repeat("\u200b", 8)} {
		_, _, err := SupabaseBackendFromEnvironment(base(shadow), newTestSessionAuthority(t))
		if !errors.Is(err, ErrSupabaseNoIssuer) {
			t.Errorf("%q under the current name fell through to the deprecated spelling instead of "+
				"being refused: got %v, want %v. A line that reduces to nothing is a line the "+
				"operator wrote, and reading it as absent is what this package's blank policy "+
				"exists against", shadow, err, ErrSupabaseNoIssuer)
		}
	}

	backend, armed, err := SupabaseBackendFromEnvironment(base(""), newTestSessionAuthority(t))
	if err != nil || !armed {
		t.Fatalf("POSITIVE CONTROL FAILED: an EMPTY current name beside a real deprecated one was "+
			"refused (armed=%v, %v), so the refusals above are a function that rejects everything "+
			"rather than a measurement", armed, err)
	}
	if backend.verify.Issuer != testIssuer {
		t.Errorf("an empty current name resolved the issuer to %q, want the deprecated spelling's "+
			"%q — `X=\"\"` is how a manifest says `not using this`", backend.verify.Issuer, testIssuer)
	}
}

// TestAWhitespaceCurrentNameUnderARefuseBlankPolicyIsAlsoRefused is the second policy's
// half of the case above, and it is why that one is not the whole claim.
//
// 🔴 ONE ARM CANNOT COVER TWO POLICIES. `refusedByConstructor` sends a blank to a named
// construction rung; `refuseBlank` sends it to `ErrBlankSetting` with the line quoted. A
// `written` that fell through to the deprecated spelling would make the first arm's
// configuration BUILD (a real issuer resolves) and the second arm's SUCCEED with a live role
// check the operator's blank line disagrees with — different observable failures, so each
// needs its own arm. INVARIANT GUARD, like its sibling: this configuration could not exist
// at the base commit.
func TestAWhitespaceCurrentNameUnderARefuseBlankPolicyIsAlsoRefused(t *testing.T) {
	env := map[string]string{
		EnvSupabaseJWKSURL:            oidcFixtureJWKS,
		EnvSupabaseIssuer:             testIssuer,
		"CAIRN_SUPABASE_REQUIRE_ROLE": oidcFixtureRole,
		EnvSupabaseRequireRole:        "   ",
	}
	_, _, err := SupabaseBackendFromEnvironment(env, newTestSessionAuthority(t))
	if !errors.Is(err, ErrBlankSetting) {
		t.Fatalf("a whitespace %s beside a real %s was not refused: %v", EnvSupabaseRequireRole,
			"CAIRN_SUPABASE_REQUIRE_ROLE", err)
	}
	if !strings.Contains(err.Error(), EnvSupabaseRequireRole) {
		t.Errorf("the refusal does not name %s, the line that is blank:\n%s",
			EnvSupabaseRequireRole, err)
	}

	// The POSITIVE CONTROL for this arm: the deprecated spelling ALONE still arms the role
	// check, so the refusal above is about the blank and not about the pair being set.
	env[EnvSupabaseRequireRole] = ""
	backend, armed, err := SupabaseBackendFromEnvironment(env, newTestSessionAuthority(t))
	if err != nil || !armed {
		t.Fatalf("POSITIVE CONTROL FAILED: an empty current name beside a real deprecated one was "+
			"refused (armed=%v, %v)", armed, err)
	}
	if backend.requireRole != oidcFixtureRole {
		t.Errorf("the role check resolved to %q, want the deprecated spelling's %q",
			backend.requireRole, oidcFixtureRole)
	}
}

// TestEveryRenamedVerifierSettingResolvesFromBothSpellings is the SEAM guard, and it pins a
// RELATIONSHIP between two things neither of which can see the other.
//
// 🔴 TWO SURFACES, EACH CORRECT ALONE AND BREAKABLE TOGETHER. `internal/envalias` can hold
// a pair that nothing resolves; this package can hold a setting whose alias nothing looks
// up. Every test above would stay green if a future edit dropped `envalias.OldName` from
// `written` for SOME settings, because they drive the ledger through its two complete
// configurations and not setting by setting.
//
// 🔴 AND IT ASSERTS THE SET, NOT JUST ITS MEMBERS. A ledger somebody shortened, or a
// `supabaseEnv` somebody renamed out of the `CAIRN_OIDC_` namespace, would leave a
// membership loop green over whatever remained — the "a count of DECLARATIONS is not a count
// of INSTANCES" shape. The expectation is spelled as literals so neither side is its own
// oracle.
func TestEveryRenamedVerifierSettingResolvesFromBothSpellings(t *testing.T) {
	want := map[string]string{
		"CAIRN_OIDC_AUDIENCE":     "CAIRN_SUPABASE_AUDIENCE",
		"CAIRN_OIDC_ISSUER":       "CAIRN_SUPABASE_ISSUER",
		"CAIRN_OIDC_JWKS_URL":     "CAIRN_SUPABASE_JWKS_URL",
		"CAIRN_OIDC_LEEWAY":       "CAIRN_SUPABASE_LEEWAY",
		"CAIRN_OIDC_MAX_AGE":      "CAIRN_SUPABASE_MAX_AGE",
		"CAIRN_OIDC_PROVIDER":     "CAIRN_SUPABASE_PROVIDER",
		"CAIRN_OIDC_REQUIRE_ROLE": "CAIRN_SUPABASE_REQUIRE_ROLE",
	}

	got := map[string]string{}
	for _, s := range supabaseEnv.settings {
		if old := envalias.OldName(s.name); old != "" {
			got[s.name] = old
		}
	}
	if len(got) != len(want) {
		t.Fatalf("the verifier ledger and the alias ledger agree on %d settings, want %d: got %v. "+
			"A setting here with no pair there is a deployment whose deprecated spelling silently "+
			"stopped resolving; a pair there with no setting here is an alias nothing reads",
			len(got), len(want), got)
	}
	for newName, old := range want {
		if got[newName] != old {
			t.Fatalf("%s pairs with %q, want %q", newName, got[newName], old)
		}
	}

	// And the BEHAVIOURAL half, per setting: the structural agreement above type-checks
	// past a `written` that consults the ledger and then ignores the answer.
	for newName, old := range want {
		if raw, spelling, ok := written(map[string]string{old: "a-value"}, newName); !ok ||
			raw != "a-value" || spelling != old {
			t.Errorf("%s written as %s resolved to (%q, %q, %v); the deprecated spelling is not "+
				"reaching the reader", newName, old, raw, spelling, ok)
		}
	}
}

// TestADeprecatedVerifierNameWarnsNamingItsReplacement is REGRESSION coverage on the
// WARNING half, which is a different claim from resolution: a deployment can resolve
// correctly and never tell its operator the line is deprecated.
//
//	d7e1fec  FAIL — `CAIRN_SUPABASE_ISSUER` is in no ledger there, so `Deprecations`
//	         returns an empty slice and the operator is never told
//	HEAD     ok
//
// ⚠ IT ASSERTS THE WHOLE LINE RATHER THAN A KEYWORD. The artifact is prose, and a guard on
// words is walkable by rewording — the same ruling `tests/test_env_aliases.py` applies to
// the cross-language comparison, which is also what keeps the two languages' bytes equal.
func TestADeprecatedVerifierNameWarnsNamingItsReplacement(t *testing.T) {
	lines := envalias.Deprecations(map[string]string{
		"CAIRN_SUPABASE_ISSUER":   testIssuer,
		"CAIRN_SUPABASE_JWKS_URL": oidcFixtureJWKS,
	})
	want := []string{
		envalias.EnvWarning(envalias.Pair{New: "CAIRN_OIDC_ISSUER", Old: "CAIRN_SUPABASE_ISSUER"}),
		envalias.EnvWarning(envalias.Pair{New: "CAIRN_OIDC_JWKS_URL", Old: "CAIRN_SUPABASE_JWKS_URL"}),
	}
	if len(lines) != len(want) {
		t.Fatalf("got %d warning lines, want %d: %v", len(lines), len(want), lines)
	}
	// Sorted by NEW name — ISSUER before JWKS_URL — which is the order `tests/parity/`
	// diffs the two clients' stderr in.
	for i := range want {
		if lines[i] != want[i] {
			t.Errorf("line %d is %q, want %q", i, lines[i], want[i])
		}
	}
}
