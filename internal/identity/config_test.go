package identity

import (
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

// TestTheEnvironmentLedgersNameEveryVariableEachBackendReads is a LEDGER over the
// package's own constants.
//
// 🔴 A VARIABLE LEFT OUT OF A LEDGER IS A VARIABLE THAT CAN BE SET ALONE WITHOUT ARMING
// THE PARTIAL-CONFIGURATION CHECK — which is the same defect the check exists to close,
// one level up. It fails when the set GROWS as well as when it shrinks, so adding a
// setting without deciding which backend owns it is a red test rather than a silent gap.
//
// 🔴 THE "GROWS" HALF IS A CLAIM ABOUT `envConstantsFromSource`, AND IT IS ONLY TRUE
// BECAUSE THAT SIDE OF THE COMPARISON IS DERIVED FROM THE SOURCE. Both sides of a
// comparison written by the same hand move together, which is a test that reads as
// coverage and provides none — see the note on `known` below for the measurement.
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
	known := envConstantsFromSource(t)
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

// envConstantsFromSource reads every exported `Env*` constant out of THIS PACKAGE'S OWN
// SOURCE, by AST, and returns their values.
//
// 🔴 IT IS A DERIVATION AND NOT A SECOND SPELLING, WHICH IS THE ONLY REASON THE LEDGER
// TEST ABOVE CAN FAIL WHEN THE SET *GROWS*. A hand-written list is written by whoever
// adds the constant, in the same edit, so it moves with the thing it is supposed to
// pin — and the comparison is then between two copies of one decision. This repository
// already answers that shape the same way twice: `tests/test_cairn_doctor.py` walks the
// `cairn` script's AST for its exit codes, and `capability_ledger.cli_verbs_from_parser`
// asks argparse rather than restating the verbs.
//
// ⚠ EVERY NON-TEST FILE IN THE PACKAGE DIRECTORY, NOT JUST `config.go` — because the
// sentence above says "every `Env*` constant this package exports" and a reader scoped to
// one file would make that sentence wider than the code under it. All fifteen live in
// `config.go` today; a sixteenth added in `supabase.go` is exactly the edit this must not
// miss. The working directory of a Go test binary is its own package directory, so `"."`
// is the package.
//
// ⚠ AN `Env*` CONSTANT WHOSE VALUE IS NOT A PLAIN STRING LITERAL IS A `t.Fatal`, NEVER A
// SKIP. Skipping it would make the derivation silently narrower than its own description,
// which is the defect this function was written to remove.
func envConstantsFromSource(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("cannot read the package directory, so nothing here is derived from anything: %v", err)
	}
	fset := token.NewFileSet()
	var values []string
	var files int
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		files++
		file, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		for _, decl := range file.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.CONST {
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
						t.Fatalf("%s: %s is an Env* constant with no value of its own (an iota or a repeated "+
							"const line), so this ledger cannot read what variable it names", name, ident.Name)
					}
					lit, ok := value.Values[i].(*ast.BasicLit)
					if !ok || lit.Kind != token.STRING {
						t.Fatalf("%s: %s is an Env* constant whose value is not a plain string literal, so this "+
							"ledger cannot read it — and a ledger that silently skips a constant is the defect "+
							"it exists to close", name, ident.Name)
					}
					unquoted, err := strconv.Unquote(lit.Value)
					if err != nil {
						t.Fatalf("%s: %s: %v", name, ident.Name, err)
					}
					values = append(values, unquoted)
				}
			}
		}
	}

	// 🔴 THE POSITIVE CONTROL, BECAUSE A DERIVATION THAT MATCHES NOTHING MAKES THE
	// COMPARISON ABOVE VACUOUS RATHER THAN RED. An AST walk that finds no constant — a
	// renamed prefix, a moved file, a working directory that is not the package — returns
	// an empty slice, and two empty sets compare EQUAL. A reassuring zero is
	// indistinguishable from a reader wired to nothing, so it is asserted non-zero here
	// and keyed on a string only a successful literal resolution can produce.
	if files == 0 {
		t.Fatal("no non-test .go file was parsed at all, so the `Env*` set below is derived from nothing")
	}
	if len(values) == 0 {
		t.Fatalf("parsed %d non-test file(s) and found no Env* constant — the walk matched nothing, which "+
			"would make the ledger comparison pass against an empty set", files)
	}
	// ⚠ NO TOTAL IS ASSERTED, DELIBERATELY. A count written down beside the thing it
	// counts is the hand-maintained number this whole function exists to delete, and the
	// `reflect.DeepEqual` against the ledgers is already the exact-membership check. What
	// is pinned instead is a VALUE: reading names but not values, or resolving a literal
	// wrongly, both produce a set that does not contain this one.
	const canary = "CAIRN_SUPABASE_JWKS_URL"
	found := false
	for _, v := range values {
		if v == canary {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("the derived set does not contain %q, so the walk is reading something other than the "+
			"`Env*` constants' values. Got: %v", canary, values)
	}
	return values
}
