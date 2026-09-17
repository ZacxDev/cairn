package identity

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestNothingConfiguredIsMachineTokenONLY is the compatibility claim, and it is the one
// that makes "a deployment with no Supabase and no proxy config behaves byte-identically
// to today" a property rather than a promise.
func TestNothingConfiguredIsMachineTokenOnly(t *testing.T) {
	chain, supabase, err := FromEnvironment(map[string]string{}, newTestAuthority(t))
	if err != nil {
		t.Fatalf("an empty environment must yield the default chain: %v", err)
	}
	if len(chain) != 1 {
		t.Fatalf("expected exactly one backend, got %d — a deployment that configured nothing gained an authentication path it did not ask for", len(chain))
	}
	if _, ok := chain[0].(*MachineToken); !ok {
		t.Fatalf("the only default backend is the machine token, got %T", chain[0])
	}
	if supabase != nil {
		t.Fatal("an empty environment built a Supabase backend")
	}

	// An environment carrying UNRELATED variables is still the default chain: the
	// trigger is this package's own ledgers, not "some environment exists".
	chain, _, err = FromEnvironment(map[string]string{
		"SUBSYSTEM_STORE_ROOT": "/data", "PATH": "/bin", "HOME": "/root",
	}, newTestAuthority(t))
	if err != nil || len(chain) != 1 {
		t.Fatalf("unrelated variables changed the chain: %v / %d", err, len(chain))
	}
}

// TestAPartiallyConfiguredBackendREFUSESToStart.
//
// 🔴 THE DANGEROUS ALTERNATIVE IS THE ONE THAT READS SENSIBLY. "Build the backend if the
// required fields are present" gives an operator who set the subject header and forgot
// the shared secret a pod that comes up, passes its health check, and quietly
// authenticates nobody through a backend they believe is live. So the trigger is "did
// anybody touch ANY of these variables", and a broken configuration is a refusal.
func TestAPartiallyConfiguredBackendRefusesToStart(t *testing.T) {
	for _, arm := range []struct {
		name string
		env  map[string]string
		// want is the sentinel this arm must name. wantText is for the one refusal
		// that is a wrapped `time.ParseDuration` error rather than a sentinel of this
		// package's own; exactly one of the two is set.
		want     error
		wantText string
	}{
		{
			name: "the proxy subject header alone — never DECLARED proxy-fronted",
			env:  map[string]string{EnvProxySubjectHeader: "X-Forwarded-User"},
			want: ErrTrustedHeaderNotDeclared,
		},
		{
			name: "declared proxy-fronted with no source check",
			env: map[string]string{
				EnvProxyFronted:       "yes",
				EnvProxySubjectHeader: "X-Forwarded-User",
				EnvProxyProvider:      testProvider,
			},
			want: ErrTrustedHeaderNoSourceCheck,
		},
		{
			name: "declared proxy-fronted with a secret and no subject header",
			env: map[string]string{
				EnvProxyFronted: "yes",
				EnvProxySecret:  string(testProxySecret),
			},
			want: ErrTrustedHeaderNoSubjectHeader,
		},
		{
			name: "a Supabase JWKS with no issuer",
			env:  map[string]string{EnvSupabaseJWKSURL: "https://notes-idp.example.test/jwks"},
			want: ErrSupabaseNoIssuer,
		},
		{
			// ⚠ THE ONE RUNG THAT USED TO HAVE A SIBLING. It asked "is there ANY key",
			// and `ErrSupabaseWeakSecret` asked "is the one you gave strong enough" —
			// two questions, which is what made the second reachable. With no symmetric
			// secret there is no second question, and the rung that asked it was
			// deleted rather than left standing unreachable.
			name: "a Supabase issuer with nothing to verify against",
			env:  map[string]string{EnvSupabaseIssuer: testIssuer},
			want: ErrSupabaseNoKeys,
		},
		{
			name: "a negative maximum token age, which would be read as OFF",
			env: map[string]string{
				EnvSupabaseIssuer:  testIssuer,
				EnvSupabaseJWKSURL: "https://notes-idp.example.test/jwks",
				EnvSupabaseMaxAge:  "-1h",
			},
			want: ErrSupabaseMaxAge,
		},
		{
			name: "a maximum token age that is not a duration",
			env: map[string]string{
				EnvSupabaseIssuer:  testIssuer,
				EnvSupabaseJWKSURL: "https://notes-idp.example.test/jwks",
				EnvSupabaseMaxAge:  "10min",
			},
			// Not a sentinel: `time.ParseDuration`'s own error, wrapped with the name.
			// Asserted by substring below.
			wantText: EnvSupabaseMaxAge,
		},
		{
			// 🔴 THE RETIRED SETTING, AND IT IS THE *ONLY* ARM HERE THAT NEEDS NOTHING
			// ELSE SET. Every other row must touch a live variable to arm `anySet`; a
			// name dropped from a ledger is invisible to `anySet` by construction, so
			// if this refusal did not exist the pod would come up healthy having
			// silently discarded the line.
			name: "the retired legacy symmetric secret, set ALONE",
			env:  map[string]string{"CAIRN_SUPABASE_JWT_SECRET": "whatever-the-operator-still-has-in-their-manifest"},
			want: ErrRetiredSetting,
		},
		{
			name: "the retired legacy symmetric secret FILE, beside a complete configuration",
			env: map[string]string{
				EnvSupabaseIssuer:                testIssuer,
				EnvSupabaseJWKSURL:               "https://notes-idp.example.test/jwks",
				"CAIRN_SUPABASE_JWT_SECRET_FILE": "/run/secrets/legacy",
			},
			want: ErrRetiredSetting,
		},
		{
			name: "a plaintext JWKS endpoint",
			env: map[string]string{
				EnvSupabaseIssuer:  testIssuer,
				EnvSupabaseJWKSURL: "http://notes-idp.example.test/jwks",
			},
			want: ErrJWKSURL,
		},
	} {
		t.Run(arm.name, func(t *testing.T) {
			_, _, err := FromEnvironment(arm.env, newTestAuthority(t))
			if err == nil {
				t.Fatal("a partial configuration came up quietly rather than refusing")
			}
			if arm.want != nil && !errors.Is(err, arm.want) {
				t.Fatalf("the WRONG guard fired.\n  got:  %v\n  want: %v", err, arm.want)
			}
			if arm.wantText != "" && !strings.Contains(err.Error(), arm.wantText) {
				t.Fatalf("the refusal does not name the setting it is about.\n  got:  %v\n  want it to contain: %s", err, arm.wantText)
			}
			if arm.want == nil && arm.wantText == "" {
				t.Fatal("this arm asserts nothing about WHICH guard fired, so any refusal satisfies it")
			}
		})
	}
}

// TestAFullyConfiguredEnvironmentBuildsTheWholeChain — the positive control for the
// table above, without which every arm is satisfied by a `FromEnvironment` that refuses
// everything.
// ⚠ IT NOW DRIVES A LIVE LOOPBACK JWKS, WHICH IS A WIDER CLAIM THAN THE VERSION IT
// REPLACES. The old body configured the LEGACY symmetric secret, so `RefreshKeys` and
// `KeyStatus` took their no-key arms: the test asserted that two calls did NOTHING. The
// symmetric path is gone and those arms with it, so the only configuration this test can
// express is the one that fetches — and that turns two vacuous assertions into an
// end-to-end one, from an environment variable through `NewKeySet` to a materialized key
// set. The URL is loopback because `checkJWKSURL` refuses plaintext to a remote host;
// `newJWKSServer` closes the listener on cleanup.
func TestAFullyConfiguredEnvironmentBuildsTheWholeChain(t *testing.T) {
	idp := newJWKSServer(t, jwksDocumentOf(t, newRSASigner(t, "rsa-1", 2048).publicJWK(t)))

	chain, supabase, err := FromEnvironment(map[string]string{
		EnvSupabaseIssuer:  testIssuer,
		EnvSupabaseJWKSURL: idp.url(),

		EnvProxyFronted:       "yes",
		EnvProxySubjectHeader: "X-Forwarded-User",
		EnvProxySecret:        string(testProxySecret),
		EnvProxyProvider:      testProvider,
		EnvProxyPeers:         "192.0.2.10/32",
	}, newTestAuthority(t))
	if err != nil {
		t.Fatalf("a complete configuration must build: %v", err)
	}
	if len(chain) != 3 {
		t.Fatalf("expected three backends, got %d", len(chain))
	}
	if supabase == nil {
		t.Fatal("the Supabase backend was not returned, so nothing could refresh its keys")
	}
	// The negative half first: nothing has fetched, so the backend verifies nothing.
	// Without it, a `Fetched` that was true from the start would make the assertion
	// below a fact about the zero value.
	if got := supabase.KeyStatus(); got.Fetched {
		t.Fatalf("the key set reported itself fetched before anything fetched it: %s", got)
	}
	if err := supabase.RefreshKeys(t.Context()); err != nil {
		t.Fatalf("the configured JWKS endpoint must materialize: %v", err)
	}
	if got := supabase.KeyStatus(); !got.Fetched {
		t.Fatalf("RefreshKeys returned nil and the key set is still unmaterialized: %s", got)
	}
}

// TestAnUnrecognisedBooleanIsAnERRORRatherThanFalse.
//
// 🔴 A TYPO THAT SILENTLY DISABLES A SECURITY SETTING LEAVES THE OPERATOR BELIEVING IT IS
// ON. `CAIRN_TRUSTED_HEADER_PROXY_FRONTED=treu` must not mean "not proxy-fronted".
func TestAnUnrecognisedBooleanIsAnErrorRatherThanFalse(t *testing.T) {
	_, _, err := FromEnvironment(map[string]string{
		EnvProxyFronted:       "treu",
		EnvProxySubjectHeader: "X-Forwarded-User",
		EnvProxySecret:        string(testProxySecret),
		EnvProxyProvider:      testProvider,
	}, newTestAuthority(t))
	if err == nil {
		t.Fatal("a misspelled boolean was read as `no`, silently disabling the backend the operator configured")
	}
	if !strings.Contains(err.Error(), EnvProxyFronted) {
		t.Fatalf("the refusal must name the variable: %v", err)
	}

	// Every accepted spelling, both ways, so the table is not "anything but `yes` fails".
	for _, yes := range []string{"1", "t", "true", "y", "yes", "on", "YES", "True"} {
		if got, err := envBool(map[string]string{"X": yes}, "X"); err != nil || !got {
			t.Fatalf("%q must read as true, got %v / %v", yes, got, err)
		}
	}
	for _, no := range []string{"", "0", "f", "false", "n", "no", "off", "NO"} {
		if got, err := envBool(map[string]string{"X": no}, "X"); err != nil || got {
			t.Fatalf("%q must read as false, got %v / %v", no, got, err)
		}
	}
}

// TestASecretHasEXACTLYOneSource.
//
// 🔴 TWO SOURCES FOR ONE SECRET MEANS "WHICH ONE IS LIVE" DEPENDS ON A PRECEDENCE NOBODY
// READS, and a rotation that updated the wrong one appears to work.
func TestASecretHasExactlyOneSource(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "proxy-secret")
	// A trailing newline, because every editor and every `echo` adds one and a secret
	// differing by an invisible byte fails with a refusal that says nothing about why.
	if err := os.WriteFile(path, append(append([]byte{}, testProxySecret...), '\n'), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := secretFrom(map[string]string{EnvProxySecretFile: path}, EnvProxySecret, EnvProxySecretFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(testProxySecret) {
		t.Fatalf("the file form read %q, want %q with the trailing newline stripped", got, testProxySecret)
	}

	_, err = secretFrom(map[string]string{
		EnvProxySecret:     string(testProxySecret),
		EnvProxySecretFile: path,
	}, EnvProxySecret, EnvProxySecretFile)
	if err == nil {
		t.Fatal("both an inline secret and a secret file were accepted")
	}

	// A file that does not exist is a refusal, not an empty secret — which would fall
	// through to "no source check configured" and blame the wrong setting.
	if _, err := secretFrom(map[string]string{EnvProxySecretFile: filepath.Join(dir, "absent")},
		EnvProxySecret, EnvProxySecretFile); err == nil {
		t.Fatal("an unreadable secret file was read as no secret at all")
	}
}

// TestAnEmptySecretFileBlamesITSELFRatherThanTheSourceCheck.
//
// 🔴 THE MISSING-FILE CASE WAS CLOSED AND THE EMPTY-FILE CASE WAS NOT, WHICH IS THE SAME
// MIS-BLAME ONE STEP FURTHER IN. A file holding only the newline an editor added strips to
// nothing; the zero-length slice reaches `NewTrustedHeader`'s "is there any source check at
// all" rung, and the operator is told to configure the secret they did configure, in a
// crash loop with nothing naming the file.
func TestAnEmptySecretFileBlamesItselfRatherThanTheSourceCheck(t *testing.T) {
	dir := t.TempDir()
	for _, arm := range []struct{ name, body string }{
		{"a file holding only the newline an editor added", "\n"},
		{"a file holding a CRLF", "\r\n"},
		{"a zero-byte file", ""},
	} {
		t.Run(arm.name, func(t *testing.T) {
			path := filepath.Join(dir, strings.ReplaceAll(arm.name, " ", "-"))
			if err := os.WriteFile(path, []byte(arm.body), 0o600); err != nil {
				t.Fatal(err)
			}
			_, err := secretFrom(map[string]string{EnvProxySecretFile: path}, EnvProxySecret, EnvProxySecretFile)
			if err == nil {
				t.Fatal("an empty secret file was read as no secret at all, which blames the source check")
			}
			// 🔴 THIS GUARD'S OWN MESSAGE, NOT ANY REFUSAL. The variable AND the path: a
			// refusal that names neither is the crash loop this test exists to end, and
			// `ErrTrustedHeaderNoSourceCheck` would satisfy a bare `err != nil`.
			for _, want := range []string{EnvProxySecretFile, path, "is empty"} {
				if !strings.Contains(err.Error(), want) {
					t.Fatalf("the refusal does not contain %q, so it does not point at the file:\n  %v", want, err)
				}
			}
		})
	}

	// 🔴 THE POSITIVE CONTROL. Without it every arm above is satisfied by a `secretFrom`
	// that refuses every file, and the whole file form would be dead while reading green.
	good := filepath.Join(dir, "real")
	if err := os.WriteFile(good, append(append([]byte{}, testProxySecret...), '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := secretFrom(map[string]string{EnvProxySecretFile: good}, EnvProxySecret, EnvProxySecretFile)
	if err != nil || string(got) != string(testProxySecret) {
		t.Fatalf("a real secret file stopped working: %q / %v", got, err)
	}

	// …and the whole way through `FromEnvironment`, because `secretFrom` is a package
	// function and the operator meets this through an environment.
	//
	// 🔴 THE ENVIRONMENT HERE IS OTHERWISE COMPLETE, AND THAT IS WHAT MAKES
	// `ErrTrustedHeaderNoSourceCheck` THE MIS-BLAME TO ASSERT AGAINST. Measured while
	// building this test: with the secret FILE as the only proxy variable set, the
	// unfixed code refused with `ErrTrustedHeaderNotDeclared` instead — an earlier rung,
	// a different wrong answer, and an arm that would have satisfied a
	// `!errors.Is(…, ErrTrustedHeaderNoSourceCheck)` assertion for the wrong reason. Every
	// rung above "is there any source check" is therefore satisfied below, so the ONLY
	// thing left for the constructor to complain about is the secret.
	empty := filepath.Join(dir, "empty-through-fromenvironment")
	if err := os.WriteFile(empty, []byte("\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, _, err = FromEnvironment(map[string]string{
		EnvProxyFronted:       "yes",
		EnvProxySubjectHeader: "X-Forwarded-User",
		EnvProxyProvider:      testProvider,
		EnvProxySecretFile:    empty,
	}, newTestAuthority(t))
	if err == nil {
		t.Fatal("an empty secret file came up quietly through FromEnvironment")
	}
	if errors.Is(err, ErrTrustedHeaderNoSourceCheck) {
		t.Fatalf("the WRONG guard fired — the operator is told to configure the secret they configured: %v", err)
	}
	if !strings.Contains(err.Error(), EnvProxySecretFile) {
		t.Fatalf("the refusal does not name the variable that is wrong: %v", err)
	}
}

// TestAWhitespaceOnlyValueIsREFUSEDRatherThanReadAsUnset.
//
// 🔴 `anySet` TRIMS, SO A WHITESPACE-ONLY VALUE ARMS NOTHING AND THE BACKEND THE OPERATOR
// CONFIGURED IS SILENTLY OFF. Measured before the guard: `FromEnvironment` with
// `CAIRN_TRUSTED_HEADER_SECRET` set to three spaces returned a nil error and a ONE-backend
// chain — a pod that starts, passes its health check, and logs nothing about the backend it
// is not running. That is the defect the ledgers exist to close, in the one spelling they
// cannot see.
func TestAWhitespaceOnlyValueIsRefusedRatherThanReadAsUnset(t *testing.T) {
	// The measured case first, by itself, so the arm that reproduced the defect is named.
	_, _, err := FromEnvironment(map[string]string{EnvProxySecret: "   "}, newTestAuthority(t))
	if !errors.Is(err, ErrBlankSetting) {
		t.Fatalf("a whitespace-only shared secret was read as unset.\n  got:  %v\n  want: %v", err, ErrBlankSetting)
	}
	if !strings.Contains(err.Error(), EnvProxySecret) {
		t.Fatalf("the refusal must name the offending variable, so the operator knows which line to fix: %v", err)
	}

	// EVERY ledger name and several spellings of blank, because the guard's sentence is
	// about the LEDGERS rather than about one variable.
	ledgers := append(append([]string{}, supabaseEnv...), proxyEnv...)
	for _, name := range ledgers {
		t.Run("blank/"+name, func(t *testing.T) {
			for _, blank := range []string{" ", "\t", "\n", "\r\n", " \t \n "} {
				_, _, err := FromEnvironment(map[string]string{name: blank}, newTestAuthority(t))
				if !errors.Is(err, ErrBlankSetting) {
					t.Fatalf("%s=%q was read as unset: %v", name, blank, err)
				}
				if !strings.Contains(err.Error(), name) {
					t.Fatalf("the refusal for %s=%q does not name it: %v", name, blank, err)
				}
			}
		})
	}

	// 🔴 THE HALF THAT IS THE REGRESSION RISK, AND IT IS THE ONE TO CHECK HARDEST: AN
	// ABSENT VARIABLE MUST BEHAVE EXACTLY AS IT DID. "Nothing set is machine-token only" is
	// this package's whole compatibility claim, and a blank check that fired on an absent
	// name would refuse every deployment that exists today.
	chain, supabase, err := FromEnvironment(map[string]string{}, newTestAuthority(t))
	if err != nil || len(chain) != 1 || supabase != nil {
		t.Fatalf("an empty environment stopped being machine-token only: %v / %d / %v", err, len(chain), supabase)
	}

	// …and present-with-the-EMPTY-string is unchanged too, which is the scope line in
	// `refuseBlankSettings` rather than an accident. A manifest that emits every variable
	// with an empty default is a common shape; refusing it is a wider change than the
	// defect above, and it is not made here.
	for _, name := range ledgers {
		t.Run("empty/"+name, func(t *testing.T) {
			chain, _, err := FromEnvironment(map[string]string{name: ""}, newTestAuthority(t))
			if err != nil {
				t.Fatalf("%s set to the empty string became a refusal — that is a behaviour change "+
					"this guard deliberately does not make: %v", name, err)
			}
			if len(chain) != 1 {
				t.Fatalf("%s set to the empty string armed a backend, got %d", name, len(chain))
			}
		})
	}

	// 🔴 AND THE GUARD IS SCOPED TO THE LEDGERS. An unrelated variable that happens to hold
	// whitespace is not this package's business, and a guard that refused it would be
	// refusing on "some environment exists" — the trigger `TestNothingConfiguredIsMachineTokenOnly`
	// already names as the wrong one.
	chain, _, err = FromEnvironment(map[string]string{"SUBSYSTEM_STORE_ROOT": "   ", "PATH": " "}, newTestAuthority(t))
	if err != nil || len(chain) != 1 {
		t.Fatalf("an unrelated whitespace-only variable was refused: %v / %d", err, len(chain))
	}
}

// TestABlankSettingBesideAnARMEDBackendIsAcceptedAsUnset is the REGRESSION guard on the
// scope of `refuseBlankSettings`, and it is not an invariant guard: the refusal whose
// absence it pins landed on this branch and refused environments that had been accepted
// one commit earlier.
//
// 🔴 THE REFUSAL'S OWN SENTENCE IS WHAT BOUNDS IT — "the backend it belongs to would be
// silently OFF" — AND THAT IS FALSE WHENEVER SOMETHING ELSE IN THE SAME LEDGER ARMED IT.
// Measured on the first version of the guard, which walked `supabaseEnv ∪ proxyEnv`
// unconditionally: a COMPLETE trusted-header configuration carrying
// `CAIRN_TRUSTED_HEADER_PEERS="  "` beside it was refused with `ErrBlankSetting`, so
// `main.go` exited 78 in a crash loop — for a value that setting's OWN reader
// (`strings.TrimSpace(env[EnvProxyPeers])`) turns into the empty default, and for a backend
// that was demonstrably ON. All three accept-arms below were accepted at `7ac810e`, the
// commit before the guard existed: measured, nil error and a TWO-backend chain for each.
//
// ⚠ THE LAST ARM IS THE ONE THAT KEEPS THE NARROWING PER-LEDGER RATHER THAN GLOBAL. A fix
// that asked "is ANY backend armed" would pass every arm above it and re-open the measured
// defect for a deployment that runs Supabase and typed a blank proxy secret.
//
// 🔴 AND ITS ARMS ARE NOW THE SETTINGS WHOSE ZERO IS A DEFAULT, BECAUSE THE ROUND AFTER
// FOUND THE OTHER HALF OF THE SAME HAZARD AND THE OLD ARMS WERE PINNING IT OPEN. The three
// it carried — the peer list, the client-certificate flag and the required role — all have
// a PERMISSIVE zero, so "accepted as unset" meant a security check silently off: measured,
// `CAIRN_TRUSTED_HEADER_REQUIRE_CLIENT_CERT="  "` beside a complete armed configuration
// gave a nil error and `requireCert` FALSE. `envSetting` refuses those in the reader now,
// and `TestABlankSecuritySettingCannotSilentlyDisableTheCheckItArms` is where they moved.
// What remains here is the half round 2 was right about: a setting whose blank value
// becomes a documented DEFAULT — the secret header, the audience, the provider — where the
// ledger-level refusal's own sentence ("the backend it belongs to would be silently OFF")
// is false and refusing crash-looped a deployment that worked. Both halves are live at
// once, and neither is the other's revert.
func TestABlankSettingBesideAnArmedBackendIsAcceptedAsUnset(t *testing.T) {
	// A complete configuration for each backend, with nothing blank in it. `NewKeySet`
	// constructs from the URL and does not fetch, so no server is needed here.
	armedSupabase := func() map[string]string {
		return map[string]string{
			EnvSupabaseIssuer:  testIssuer,
			EnvSupabaseJWKSURL: "https://notes-idp.example.test/jwks",
		}
	}
	armedProxy := func() map[string]string {
		return map[string]string{
			EnvProxyFronted:       "yes",
			EnvProxySubjectHeader: "X-Forwarded-User",
			EnvProxySecret:        string(testProxySecret),
			EnvProxyProvider:      testProvider,
		}
	}
	with := func(base map[string]string, name, value string) map[string]string {
		base[name] = value
		return base
	}

	for _, arm := range []struct {
		name  string
		env   map[string]string
		blank string
		// atDefault asserts the blank value reached the backend as the UNSET default —
		// not merely that the chain built. Without it this test passes against a
		// `FromEnvironment` that accepted the line and read the whitespace as a value.
		atDefault func(t *testing.T, chain Chain, supabase *SupabaseJWT)
	}{
		{
			name:  "a blank secret header beside an armed trusted-header backend",
			env:   with(armedProxy(), EnvProxySecretHeader, "  "),
			blank: EnvProxySecretHeader,
			atDefault: func(t *testing.T, chain Chain, _ *SupabaseJWT) {
				if got := trustedHeaderIn(t, chain).secretHeader; got != DefaultProxySecretHeader {
					t.Fatalf("a blank secret header became %q, want the default %q", got, DefaultProxySecretHeader)
				}
			},
		},
		{
			name:  "a blank audience beside an armed Supabase backend",
			env:   with(armedSupabase(), EnvSupabaseAudience, " "),
			blank: EnvSupabaseAudience,
			atDefault: func(t *testing.T, _ Chain, supabase *SupabaseJWT) {
				if supabase == nil {
					t.Fatal("the Supabase backend was not built")
				}
				// 🔴 THE DEFAULT, NOT THE EMPTY STRING, AND THAT IS WHY THIS ARM BELONGS
				// HERE RATHER THAN IN THE REFUSING TEST. An empty audience is refused by
				// `ErrSupabaseNoAudience`; a blank one reaches the SAME check `authenticated`
				// arms, so nothing is switched off.
				if got := supabase.verify.Audience; got != DefaultSupabaseAudience {
					t.Fatalf("a blank audience became %q, want the default %q", got, DefaultSupabaseAudience)
				}
			},
		},
		{
			name:  "a blank provider beside an armed Supabase backend",
			env:   with(armedSupabase(), EnvSupabaseProvider, " \t "),
			blank: EnvSupabaseProvider,
			atDefault: func(t *testing.T, _ Chain, supabase *SupabaseJWT) {
				if supabase == nil {
					t.Fatal("the Supabase backend was not built")
				}
				if got := supabase.provider; got != DefaultSupabaseProvider {
					t.Fatalf("a blank provider became %q, want the default %q", got, DefaultSupabaseProvider)
				}
			},
		},
	} {
		t.Run(arm.name, func(t *testing.T) {
			chain, supabase, err := FromEnvironment(arm.env, newTestAuthority(t))
			if err != nil {
				t.Fatalf("a blank OPTIONAL setting beside an armed backend was refused, which is a "+
					"deployment that worked before the guard existed:\n  %v", err)
			}
			if len(chain) != 2 {
				t.Fatalf("expected the machine token plus the armed backend, got %d", len(chain))
			}
			arm.atDefault(t, chain, supabase)

			// …and the result is the one the ABSENT spelling produces, which is the
			// "exactly as before" half stated as a comparison rather than asserted
			// arm by arm.
			absent := map[string]string{}
			for k, v := range arm.env {
				if k != arm.blank {
					absent[k] = v
				}
			}
			chainAbsent, supabaseAbsent, errAbsent := FromEnvironment(absent, newTestAuthority(t))
			if errAbsent != nil || len(chainAbsent) != len(chain) {
				t.Fatalf("the same environment WITHOUT %s behaved differently: %v / %d", arm.blank, errAbsent, len(chainAbsent))
			}
			arm.atDefault(t, chainAbsent, supabaseAbsent)
		})
	}

	// 🔴 THE PER-LEDGER HALF. Supabase is armed; the proxy ledger holds nothing but a
	// blank secret, so the trusted-header backend IS silently off and the refusal's own
	// sentence is true for it. A narrowing that asked "is any backend armed" would accept
	// this, which is the measured defect back again one ledger over.
	_, _, err := FromEnvironment(with(armedSupabase(), EnvProxySecret, "   "), newTestAuthority(t))
	if !errors.Is(err, ErrBlankSetting) {
		t.Fatalf("a blank proxy secret beside an armed SUPABASE backend was accepted — the trusted-header "+
			"backend is silently off.\n  got:  %v\n  want: %v", err, ErrBlankSetting)
	}
	if !strings.Contains(err.Error(), EnvProxySecret) {
		t.Fatalf("the refusal must name the offending variable: %v", err)
	}
}

// TestABlankSecuritySettingCannotSilentlyDisableTheCheckItArms.
//
// 🔴 `envBool` REFUSED `treu` AND ACCEPTED `"  "` — THE SAME HAZARD IN TWO SPELLINGS, AND
// ONLY ONE OF THEM REFUSED. Measured at `d5880f3` with a complete, armed trusted-header
// configuration carrying BOTH a shared secret and a client-certificate requirement:
// `CAIRN_TRUSTED_HEADER_REQUIRE_CLIENT_CERT="yes"` gave a backend that enforced mTLS,
// `…="treu"` was refused naming the variable, and `…="  "` returned a NIL error and a
// backend with `requireCert` FALSE — mTLS not enforced, pod healthy, nothing logged. Five
// settings share that shape, and all five have a PERMISSIVE zero: the client-certificate
// requirement, the required role, the peer allowlist, the maximum token age and the shared
// secret's FILE path. The fifth was not in the audit that named the other four; it turned up
// by reading the fix's own comment against the code, which is the arm to distrust first.
//
// 🔴 EACH ARM ASSERTS THE BEHAVIOUR, NOT THAT "AN ERROR HAPPENED", BECAUSE AN ERROR TEST
// PASSES FOR THE WRONG REASON. The hazard is not "a blank value is accepted" — it is a
// backend that ADMITS a request the configuration says it must refuse. So every arm carries
// a two-sided probe over a real `Authenticate` call: a request the armed check must REFUSE
// and one it must ACCEPT. A probe with only the refusing half would report "enforced" for a
// backend that refuses everything.
//
// 🔴 AND THE PROBE ITSELF IS CONTROLLED IN BOTH DIRECTIONS BEFORE IT IS BELIEVED. Step 1
// builds with the real value and requires ENFORCED — without it the arm passes against a
// probe wired to nothing. Step 2 builds with the setting ABSENT and requires NOT ENFORCED —
// without it the arm passes against a probe hardwired to "enforced", which is the reassuring
// zero this repository refuses to read. Only then does step 3 ask what a blank value does.
//
// ⚠ STEP 3 ACCEPTS EITHER OUTCOME THAT IS SAFE, WHICH IS WHY IT PINS THE HAZARD RATHER THAN
// THIS PARTICULAR FIX: a refusal that NAMES the variable, or a backend whose check is on.
// What it refuses is the measured one — built, quiet, and the check off.
func TestABlankSecuritySettingCannotSilentlyDisableTheCheckItArms(t *testing.T) {
	signer := newRSASigner(t, "rsa-1", 2048)
	idp := newJWKSServer(t, jwksDocumentOf(t, signer.publicJWK(t)))
	secretPath := filepath.Join(t.TempDir(), "proxy-secret")
	if err := os.WriteFile(secretPath, append(append([]byte{}, testProxySecret...), '\n'), 0o600); err != nil {
		t.Fatal(err)
	}

	// A complete configuration for each backend, with the setting under test ABSENT.
	armedProxy := func() map[string]string {
		return map[string]string{
			EnvProxyFronted:       "yes",
			EnvProxySubjectHeader: testSubjectHeader,
			EnvProxySecret:        string(testProxySecret),
			EnvProxyProvider:      testProvider,
		}
	}
	// 🔴 A SECOND PROXY BASE, ARMED BY THE *CERTIFICATE* RUNG, BECAUSE THE SECRET ARM
	// CANNOT USE THE ONE ABOVE. `armedProxy` supplies the secret inline, so a blank
	// `…_SECRET_FILE` beside it is refused as two sources for one secret — an answer about
	// a different guard. Here the backend is armed by `RequireClientCert`, which is the
	// defence-in-depth deployment the measurement came from: both rungs configured, one of
	// them written as whitespace.
	armedProxyByCert := func() map[string]string {
		return map[string]string{
			EnvProxyFronted:           "yes",
			EnvProxySubjectHeader:     testSubjectHeader,
			EnvProxyProvider:          testProvider,
			EnvProxyRequireClientCert: "yes",
		}
	}
	armedSupabase := func() map[string]string {
		return map[string]string{
			EnvSupabaseIssuer:   testIssuer,
			EnvSupabaseJWKSURL:  idp.url(),
			EnvSupabaseProvider: testProvider,
		}
	}

	// proxyAdmits runs one real request at the trusted-header backend the environment
	// built. `nil` means the backend accepted it.
	proxyAdmits := func(t *testing.T, chain Chain, remoteAddr string, certVerified, secretPresented bool) error {
		t.Helper()
		headers := map[string]string{testSubjectHeader: testSubject}
		if secretPresented {
			headers[DefaultProxySecretHeader] = string(testProxySecret)
		}
		r := proxyRequest(t, headers, remoteAddr)
		if certVerified {
			r.TLS = &tls.ConnectionState{VerifiedChains: [][]*x509.Certificate{{{}}}}
		}
		_, err := trustedHeaderIn(t, chain).Authenticate(r)
		return err
	}

	// supabaseAdmits mints a token this world would otherwise accept, varying only the
	// claim the arm is about. The keys are fetched here because `FromEnvironment` hands
	// back an unmaterialized set on purpose.
	supabaseAdmits := func(t *testing.T, supabase *SupabaseJWT, role string, age time.Duration) error {
		t.Helper()
		if supabase == nil {
			t.Fatal("the Supabase backend was not built, so this probe measures nothing")
		}
		if err := supabase.RefreshKeys(t.Context()); err != nil {
			t.Fatalf("materializing the fixture key set: %v", err)
		}
		// Real wall-clock claims: `supabaseFromEnv` sets no `Now`, so the verifier runs
		// against `time.Now` rather than the year-2000 fixture clock.
		now := time.Now()
		_, err := supabase.Authenticate(bearer(t, signer.sign(t, claimSet{
			"iss":  testIssuer,
			"aud":  testAudience,
			"sub":  testSubject,
			"exp":  now.Add(time.Hour).Unix(),
			"iat":  now.Add(-age).Unix(),
			"role": role,
		}, nil)))
		return err
	}

	for _, arm := range []struct {
		setting string
		// on is the value an operator writes to turn the check ON.
		on string
		// armed is the rest of the deployment, with `setting` absent.
		armed func() map[string]string
		// disabled says, in the operator's words, what the permissive zero means.
		disabled string
		// enforced runs the two-sided probe and reports whether the check is ON. It
		// fails the test outright when NEITHER side behaves, which is a broken probe
		// rather than an answer about the setting.
		enforced func(t *testing.T, chain Chain, supabase *SupabaseJWT) bool
	}{
		{
			setting:  EnvProxyRequireClientCert,
			on:       "yes",
			armed:    armedProxy,
			disabled: "mTLS is not enforced: a plaintext request carrying the shared secret authenticates",
			enforced: func(t *testing.T, chain Chain, _ *SupabaseJWT) bool {
				if err := proxyAdmits(t, chain, "192.0.2.10:1", true, true); err != nil {
					t.Fatalf("a request with a VERIFIED client certificate was refused, so this probe "+
						"cannot tell the requirement from a backend that refuses everything: %v", err)
				}
				return proxyAdmits(t, chain, "192.0.2.10:1", false, true) != nil
			},
		},
		{
			setting:  EnvProxyPeers,
			on:       "192.0.2.10/32",
			armed:    armedProxy,
			disabled: "there is no peer allowlist: any address may present the identity header",
			enforced: func(t *testing.T, chain Chain, _ *SupabaseJWT) bool {
				if err := proxyAdmits(t, chain, "192.0.2.10:1", false, true); err != nil {
					t.Fatalf("the ALLOWED peer was refused, so this probe cannot tell an allowlist "+
						"from a backend that refuses everything: %v", err)
				}
				return proxyAdmits(t, chain, "198.51.100.7:1", false, true) != nil
			},
		},
		{
			// 🔴 THE FIFTH, AND IT WAS NOT IN THE AUDIT — IT CAME OUT OF READING THE FIX'S
			// OWN COMMENT AGAINST THE CODE. The secret file's path was the last bare
			// `strings.TrimSpace`, and beside an armed client-certificate requirement its
			// blank spelling produced a ZERO-length secret: measured at `d5880f3`, a request
			// carrying a verified certificate and NO shared-secret header authenticated,
			// where the same request against the real path is refused by name. Defence in
			// depth reduced to one rung, by whitespace, in a deployment that configured both.
			setting: EnvProxySecretFile,
			on:      secretPath,
			armed:   armedProxyByCert,
			disabled: "the shared-secret rung is gone: a verified certificate ALONE authenticates, " +
				"though the deployment configured both",
			enforced: func(t *testing.T, chain Chain, _ *SupabaseJWT) bool {
				if err := proxyAdmits(t, chain, "192.0.2.10:1", true, true); err != nil {
					t.Fatalf("a request carrying BOTH rungs was refused, so this probe cannot tell a "+
						"live shared secret from a backend that refuses everything: %v", err)
				}
				return proxyAdmits(t, chain, "192.0.2.10:1", true, false) != nil
			},
		},
		{
			setting:  EnvSupabaseRequireRole,
			on:       "authenticated",
			armed:    armedSupabase,
			disabled: "no role is required: an `anon` session authenticates",
			enforced: func(t *testing.T, _ Chain, supabase *SupabaseJWT) bool {
				if err := supabaseAdmits(t, supabase, "authenticated", time.Minute); err != nil {
					t.Fatalf("the REQUIRED role was refused, so this probe cannot tell a role check "+
						"from a backend that refuses everything: %v", err)
				}
				return supabaseAdmits(t, supabase, "anon", time.Minute) != nil
			},
		},
		{
			setting:  EnvSupabaseMaxAge,
			on:       "5m",
			armed:    armedSupabase,
			disabled: "`iat` is unbounded: a session minted hours ago still authenticates",
			enforced: func(t *testing.T, _ Chain, supabase *SupabaseJWT) bool {
				if err := supabaseAdmits(t, supabase, "authenticated", 10*time.Second); err != nil {
					t.Fatalf("a FRESH token was refused, so this probe cannot tell a maximum age "+
						"from a backend that refuses everything: %v", err)
				}
				return supabaseAdmits(t, supabase, "authenticated", time.Hour) != nil
			},
		},
	} {
		t.Run(arm.setting, func(t *testing.T) {
			build := func(t *testing.T, value string, present bool) (Chain, *SupabaseJWT, error) {
				t.Helper()
				env := arm.armed()
				if present {
					env[arm.setting] = value
				}
				return FromEnvironment(env, newTestAuthority(t))
			}

			// 1. THE POSITIVE CONTROL ON THE PROBE — the value an operator writes.
			chain, supabase, err := build(t, arm.on, true)
			if err != nil {
				t.Fatalf("precondition: %s=%q must build, or every assertion below is about "+
					"nothing: %v", arm.setting, arm.on, err)
			}
			if !arm.enforced(t, chain, supabase) {
				t.Fatalf("precondition: %s=%q did not arm the check, so the probe is not measuring "+
					"the setting this arm is about", arm.setting, arm.on)
			}

			// 2. THE NEGATIVE CONTROL ON THE PROBE — the setting genuinely absent. Its
			// zero is PERMISSIVE, which is the whole reason this arm exists, so a probe
			// that still reports ENFORCED here is wired to something other than the check.
			chain, supabase, err = build(t, "", false)
			if err != nil {
				t.Fatalf("precondition: the deployment without %s must build: %v", arm.setting, err)
			}
			if arm.enforced(t, chain, supabase) {
				t.Fatalf("the probe reports %s ENFORCED while it is UNSET — it cannot observe the "+
					"check going off, so step 3 below would pass for the wrong reason", arm.setting)
			}

			// 3. THE HAZARD. Present, whitespace only, beside a backend that IS armed.
			for _, blank := range []string{" ", "  ", "\t", "\n", " \t \n "} {
				chain, supabase, err = build(t, blank, true)
				if err != nil {
					if !errors.Is(err, ErrBlankSetting) {
						t.Fatalf("%s=%q was refused by some OTHER guard, which is a refusal this "+
							"arm cannot attribute: %v", arm.setting, blank, err)
					}
					if !strings.Contains(err.Error(), arm.setting) {
						t.Fatalf("%s=%q was refused without naming the variable, so the operator "+
							"cannot find the line: %v", arm.setting, blank, err)
					}
					continue
				}
				if !arm.enforced(t, chain, supabase) {
					t.Fatalf("%s=%q built quietly with the check OFF — %s. A typo that silently "+
						"disables a security setting leaves the operator believing it is on, which is "+
						"the sentence `envBool` refuses `treu` for three lines away",
						arm.setting, blank, arm.disabled)
				}
			}
		})
	}
}

// TestARetiredSettingHoldingOnlyWhitespaceIsStillREFUSED.
//
// 🔴 `retiredEnv`'S OWN SENTENCE IS "A DELETED SETTING MUST BE LOUD, NOT IGNORED", AND A
// TRIM BEFORE THE TEST IS THE IGNORING. Measured on the first version of the pair:
// `CAIRN_SUPABASE_JWT_SECRET="x"` refused with `ErrRetiredSetting`, and the same name set
// to three spaces returned a NIL error and fell straight through to a machine-token-only
// chain — the operator's line silently discarded, which is the exact defect the retired
// ledger exists to end. `FromEnvironment` calls the blank guard "the mirror image" of this
// check; until this arm existed only one of the two mirrors was built.
//
// ⚠ A RETIRED NAME IS NEVER "ARMED", SO THE PER-LEDGER NARROWING IN `refuseBlankSettings`
// HAS NO ANALOGUE HERE. There is no backend for it to be silently off; the whole content
// of the name is the refusal.
func TestARetiredSettingHoldingOnlyWhitespaceIsStillRefused(t *testing.T) {
	if len(retiredEnv) == 0 {
		t.Fatal("no retired name is declared, so every assertion below is vacuous")
	}
	for name := range retiredEnv {
		for _, blank := range []string{" ", "   ", "\t", "\n", "\r\n", " \t \n "} {
			_, _, err := FromEnvironment(map[string]string{name: blank}, newTestAuthority(t))
			if !errors.Is(err, ErrRetiredSetting) {
				t.Fatalf("%s=%q was silently discarded rather than refused.\n  got:  %v\n  want: %v",
					name, blank, err, ErrRetiredSetting)
			}
			if !strings.Contains(err.Error(), name) {
				t.Fatalf("the refusal for %s=%q does not name it: %v", name, blank, err)
			}
		}

		// The scope line, and it is the SAME one `refuseBlankSettings` draws: a name
		// present with the EMPTY string carries no value to discard, and a manifest that
		// emits every variable with an empty default is the shape that would be refused
		// by widening this any further.
		chain, _, err := FromEnvironment(map[string]string{name: ""}, newTestAuthority(t))
		if err != nil {
			t.Fatalf("%s set to the EMPTY string became a refusal — a wider change than the "+
				"discard measured above: %v", name, err)
		}
		if len(chain) != 1 {
			t.Fatalf("%s set to the empty string armed a backend, got %d", name, len(chain))
		}
	}
}

// trustedHeaderIn returns the one `*TrustedHeader` in a chain, and fails when there is
// not exactly one — a helper that returned the zero value would make every field
// assertion above a fact about a nil backend.
func trustedHeaderIn(t *testing.T, chain Chain) *TrustedHeader {
	t.Helper()
	var found *TrustedHeader
	for _, backend := range chain {
		if th, ok := backend.(*TrustedHeader); ok {
			if found != nil {
				t.Fatal("more than one trusted-header backend in the chain")
			}
			found = th
		}
	}
	if found == nil {
		t.Fatalf("no trusted-header backend in a chain of %d", len(chain))
	}
	return found
}

// TestTheEnvironmentLedgersNameEveryVariableEachBackendReads is a LEDGER over the
// package's own constants.
//
// 🔴 A VARIABLE LEFT OUT OF A LEDGER IS A VARIABLE THAT CAN BE SET ALONE WITHOUT ARMING
// THE PARTIAL-CONFIGURATION CHECK — which is the same defect the check exists to close,
// one level up. It fails when the set GROWS as well as when it shrinks, so adding a
// setting without deciding which backend owns it is a red test rather than a silent gap.
//
// 🔴 THE "GROWS" HALF IS A CLAIM ABOUT `envDeclarationsFromSource`, AND IT IS ONLY TRUE
// BECAUSE THAT SIDE OF THE COMPARISON IS DERIVED FROM THE SOURCE. Both sides of a
// comparison written by the same hand move together, which is a test that reads as
// coverage and provides none — see the note on `known` below for the measurement.
//
// 🔴 AND IT COMPARES EACH LIST AGAINST ITS OWN BACKEND, NOT THE UNION AGAINST THE WHOLE
// SET. The union half sees a name missing from BOTH ledgers and is structurally blind to a
// name in the WRONG one: measured on this package, moving `EnvProxyPeers` out of `proxyEnv`
// and into `supabaseEnv` left `TestTheEnvironmentLedgersNameEveryVariableEachBackendReads`
// GREEN, while the docstring on `supabaseEnv` said "EACH LIST MUST NAME EVERY VARIABLE ITS
// BACKEND READS" — a sentence about a relationship over a test that inspected one side of
// it. `envNamesReadBy` closes that by deriving the other side too: which `Env*` names each
// backend's own constructor actually references, read out of the source by AST.
func TestTheEnvironmentLedgersNameEveryVariableEachBackendReads(t *testing.T) {
	declared := map[string]bool{}
	for _, name := range append(append([]string{}, supabaseEnv...), proxyEnv...) {
		if declared[name] {
			t.Fatalf("%s appears in a ledger twice", name)
		}
		declared[name] = true
	}

	// Every `Env*` constant this package exports, read out of the package's own SOURCE.
	//
	// ⚠ THIS LINE USED TO BE A HAND-WRITTEN LIST UNDER A COMMENT CLAIMING IT WAS
	// "discovered rather than restated", AND THAT IS THE DEFECT, NOT A STYLE POINT. The
	// list named the same fifteen constants the ledgers do, so a SIXTEENTH constant
	// absent from the list and from both ledgers was in neither side of the comparison:
	// measured on this package, an exported `Env*` that `supabaseFromEnv` actually read,
	// in neither ledger, left `go test ./...` fully green. A restated list can only ever
	// catch the set SHRINKING, which is the half the docstring above does not claim.
	decls := envDeclarationsFromSource(t)
	known := make([]string, 0, len(decls))
	for _, value := range decls {
		known = append(known, value)
	}
	sort.Strings(known)
	inLedgers := make([]string, 0, len(declared))
	for name := range declared {
		inLedgers = append(inLedgers, name)
	}
	sort.Strings(inLedgers)
	if !reflect.DeepEqual(known, inLedgers) {
		t.Fatalf("the ledgers and the constants disagree.\n  ledgers:   %v\n  constants: %v\n"+
			"A variable missing from a ledger can be set alone without arming the partial-configuration refusal.",
			inLedgers, known)
	}

	// 🔴 AND EACH LEDGER AGAINST ITS OWN BACKEND, WHICH IS THE RELATIONSHIP THE SENTENCE
	// ON `supabaseEnv` CLAIMS AND THE UNION ABOVE CANNOT SEE. A name in the wrong ledger
	// arms the wrong backend: `CAIRN_TRUSTED_HEADER_PEERS` listed under `supabaseEnv`
	// makes a peer list alone build a SUPABASE backend and leaves the trusted-header
	// backend unarmed by the one variable that should arm it — and the union comparison
	// above is satisfied either way, measured.
	//
	// The other side is derived, not restated: `envNamesReadBy` asks which `Env*` names
	// each constructor's own body references. The table here is two rows and does not move
	// when a variable is added, which is what stops it being the "both sides written by the
	// same hand" shape the note above records.
	for _, backend := range []struct {
		constructor string
		ledger      []string
		ledgerName  string
	}{
		{"supabaseFromEnv", supabaseEnv, "supabaseEnv"},
		{"trustedHeaderFromEnv", proxyEnv, "proxyEnv"},
	} {
		t.Run(backend.ledgerName, func(t *testing.T) {
			reads := make([]string, 0, len(decls))
			for _, name := range envNamesReadBy(t, backend.constructor) {
				value, ok := decls[name]
				if !ok {
					t.Fatalf("%s references %s, which is not a declared Env* name in this package — "+
						"this walk cannot say which variable it reads", backend.constructor, name)
				}
				reads = append(reads, value)
			}
			sort.Strings(reads)
			want := append([]string{}, backend.ledger...)
			sort.Strings(want)
			if !reflect.DeepEqual(reads, want) {
				t.Fatalf("%s and what %s actually reads disagree.\n  %s: %v\n  read by %s: %v\n"+
					"A name in the WRONG ledger arms the wrong backend, and leaves the backend that "+
					"reads it unarmed by the one variable that should arm it.",
					backend.ledgerName, backend.constructor, backend.ledgerName, want, backend.constructor, reads)
			}
		})
	}

	// 🔴 AND THE RETIRED NAMES ARE NOT IN EITHER LEDGER, WHICH IS A RELATIONSHIP BETWEEN
	// TWO SETS RATHER THAN A PROPERTY OF ONE. A retired name may only REFUSE; a name in
	// both places would arm `anySet` too, making it a live setting that also errors —
	// a state with no coherent reading. Failing here when the sets overlap is what keeps
	// `retiredEnv`'s own comment true.
	for name := range retiredEnv {
		if declared[name] {
			t.Fatalf("%s is in retiredEnv AND in a live ledger. A retired name must only refuse — in a ledger it also ARMS the backend it was dropped from", name)
		}
		// Each retired name must actually reach the refusal: a map entry nothing reads
		// is the decorative shape this file's ledgers exist to refuse.
		_, _, err := FromEnvironment(map[string]string{name: "x"}, newTestAuthority(t))
		if !errors.Is(err, ErrRetiredSetting) {
			t.Fatalf("%s is listed as retired and setting it did not refuse: %v", name, err)
		}
		// …and it must name ITSELF, so an operator reading the crash loop knows which
		// line of their manifest to delete.
		if !strings.Contains(err.Error(), name) {
			t.Fatalf("the refusal for %s does not name it: %v", name, err)
		}
	}

	// And the mechanical half: EVERY one of them, set alone, must reach a refusal rather
	// than being ignored. This is what proves the ledger is load-bearing rather than
	// decorative.
	for _, name := range known {
		t.Run(name, func(t *testing.T) {
			if _, _, err := FromEnvironment(map[string]string{name: "x"}, newTestAuthority(t)); err == nil {
				t.Fatalf("%s set alone was IGNORED — the backend it belongs to is silently off", name)
			}
		})
	}
}

// packageSourceFiles parses every non-test `.go` file in THIS PACKAGE'S OWN directory.
//
// ⚠ EVERY NON-TEST FILE, NOT JUST `config.go` — because the two derivations below describe
// themselves as being about "this package", and a reader scoped to one file would make
// those sentences wider than the code under them. All fifteen `Env*` names live in
// `config.go` today; a sixteenth added in `supabase.go` is exactly the edit this must not
// miss. The working directory of a Go test binary is its own package directory, so `"."`
// is the package.
//
// 🔴 A WALK THAT PARSED NOTHING IS A REASSURING ZERO, SO IT IS A `t.Fatal` HERE RATHER
// THAN AN EMPTY MAP. Two empty sets compare EQUAL, and every comparison built on this
// would pass against one.
func packageSourceFiles(t *testing.T) map[string]*ast.File {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("cannot read the package directory, so nothing here is derived from anything: %v", err)
	}
	fset := token.NewFileSet()
	files := map[string]*ast.File{}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		files[name] = file
	}
	if len(files) == 0 {
		t.Fatal("no non-test .go file was parsed at all, so everything derived from this is derived from nothing")
	}
	return files
}

// envDeclarationsFromSource reads every exported `Env*` string declaration out of THIS
// PACKAGE'S OWN SOURCE, by AST, and returns Go name → environment variable name.
//
// 🔴 IT IS A DERIVATION AND NOT A SECOND SPELLING, WHICH IS THE ONLY REASON THE LEDGER
// TEST ABOVE CAN FAIL WHEN THE SET *GROWS*. A hand-written list is written by whoever
// adds the constant, in the same edit, so it moves with the thing it is supposed to
// pin — and the comparison is then between two copies of one decision. This repository
// already answers that shape the same way twice: `tests/test_cairn_doctor.py` walks the
// `cairn` script's AST for its exit codes, and `capability_ledger.cli_verbs_from_parser`
// asks argparse rather than restating the verbs.
//
// 🔴 `const` AND `var` BOTH, AND THE `var` HALF WAS THE SKIP THIS FUNCTION'S OWN SENTENCE
// SAYS IT REFUSES. The walk filtered on `token.CONST`, so `var EnvSupabaseNonceZL = "…"`
// appended to `supabase.go` left the ledger test GREEN — measured — where the `const`
// spelling of the identical line goes red. That is precisely "the derivation silently
// narrower than its own description", asserted two paragraphs down about a different
// spelling and true of this one. Nothing about a ledger entry depends on immutability, so
// the fix is to READ the `var` rather than to refuse it.
//
// ⚠ AN `Env*` DECLARATION WHOSE VALUE IS NOT A PLAIN STRING LITERAL IS A `t.Fatal`, NEVER
// A SKIP, for the same reason.
func envDeclarationsFromSource(t *testing.T) map[string]string {
	t.Helper()
	declared := map[string]string{}
	for name, file := range packageSourceFiles(t) {
		for _, decl := range file.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || (gen.Tok != token.CONST && gen.Tok != token.VAR) {
				continue
			}
			for _, spec := range gen.Specs {
				value, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for i, ident := range value.Names {
					if !strings.HasPrefix(ident.Name, "Env") {
						continue
					}
					if i >= len(value.Values) {
						t.Fatalf("%s: %s is an Env* declaration with no value of its own (an iota, a repeated "+
							"const line, or a `var` with only a type), so this ledger cannot read what variable "+
							"it names", name, ident.Name)
					}
					lit, ok := value.Values[i].(*ast.BasicLit)
					if !ok || lit.Kind != token.STRING {
						t.Fatalf("%s: %s is an Env* declaration whose value is not a plain string literal, so this "+
							"ledger cannot read it — and a ledger that silently skips one is the defect "+
							"it exists to close", name, ident.Name)
					}
					unquoted, err := strconv.Unquote(lit.Value)
					if err != nil {
						t.Fatalf("%s: %s: %v", name, ident.Name, err)
					}
					if previous, clash := declared[ident.Name]; clash {
						t.Fatalf("%s is declared twice, as %q and %q", ident.Name, previous, unquoted)
					}
					declared[ident.Name] = unquoted
				}
			}
		}
	}

	// 🔴 THE POSITIVE CONTROL, BECAUSE A DERIVATION THAT MATCHES NOTHING MAKES THE
	// COMPARISON ABOVE VACUOUS RATHER THAN RED. An AST walk that finds no declaration — a
	// renamed prefix, a changed `Tok` filter — returns an empty map, and two empty sets
	// compare EQUAL. A reassuring zero is indistinguishable from a reader wired to nothing,
	// so it is asserted non-zero here and keyed on a string only a successful literal
	// resolution can produce.
	if len(declared) == 0 {
		t.Fatal("no Env* declaration was found — the walk matched nothing, which would make the ledger " +
			"comparison pass against an empty set")
	}
	// ⚠ NO TOTAL IS ASSERTED, DELIBERATELY. A count written down beside the thing it
	// counts is the hand-maintained number this whole function exists to delete, and the
	// `reflect.DeepEqual` against the ledgers is already the exact-membership check. What
	// is pinned instead is a VALUE: reading names but not values, or resolving a literal
	// wrongly, both produce a set that does not contain this one.
	const canary = "CAIRN_SUPABASE_JWKS_URL"
	for _, v := range declared {
		if v == canary {
			return declared
		}
	}
	t.Fatalf("the derived set does not contain %q, so the walk is reading something other than the "+
		"`Env*` declarations' values. Got: %v", canary, declared)
	return nil
}

// envNamesReadBy returns the `Env*` Go identifiers referenced anywhere inside the named
// function's body.
//
// 🔴 IT IS THE *OTHER* SIDE OF "EACH LIST MUST NAME EVERY VARIABLE ITS BACKEND READS", AND
// WITHOUT IT THAT SENTENCE HAS NO GATE. A ledger compared only against the whole declared
// set sees a name missing from BOTH ledgers and nothing at all about a name in the WRONG
// one — measured: `EnvProxyPeers` moved from `proxyEnv` into `supabaseEnv` left the ledger
// test green. Deriving what each constructor actually references is what makes the
// comparison a relationship rather than a property of one side.
//
// ⚠ A NAME REFERENCED ANYWHERE IN THE BODY COUNTS, INCLUDING AS AN ARGUMENT TO A HELPER.
// `envDuration(env, EnvSupabaseLeeway)` and `secretFrom(env, EnvProxySecret, …)` read the
// variable exactly as `env[EnvSupabaseIssuer]` does, and a walk that only looked at index
// expressions would call them unread — which reads as a ledger entry nothing needs and
// invites deleting a live one.
//
// 🔴 A FUNCTION THIS CANNOT FIND IS A `t.Fatal`, NOT AN EMPTY SET. A renamed constructor
// would otherwise make the caller's comparison "the ledger is empty", which is a
// comfortable red pointing at the wrong file — and if the ledger ever were empty too, a
// silent green.
func envNamesReadBy(t *testing.T, function string) []string {
	t.Helper()
	var body *ast.BlockStmt
	var in string
	for name, file := range packageSourceFiles(t) {
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Name.Name != function || fn.Body == nil {
				continue
			}
			if body != nil {
				t.Fatalf("%s is declared in both %s and %s", function, in, name)
			}
			body, in = fn.Body, name
		}
	}
	if body == nil {
		t.Fatalf("no function named %s in this package's source, so the set below would be derived "+
			"from nothing", function)
	}

	seen := map[string]bool{}
	var names []string
	ast.Inspect(body, func(node ast.Node) bool {
		ident, ok := node.(*ast.Ident)
		if !ok || !strings.HasPrefix(ident.Name, "Env") || seen[ident.Name] {
			return true
		}
		seen[ident.Name] = true
		names = append(names, ident.Name)
		return true
	})
	if len(names) == 0 {
		t.Fatalf("%s references no Env* name at all — the walk matched nothing, and an empty set "+
			"compares equal to an empty ledger", function)
	}
	return names
}
