package identity

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
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

// TestTheEnvironmentLedgersNameEveryVariableEachBackendReads is a LEDGER over the
// package's own constants.
//
// 🔴 A VARIABLE LEFT OUT OF A LEDGER IS A VARIABLE THAT CAN BE SET ALONE WITHOUT ARMING
// THE PARTIAL-CONFIGURATION CHECK — which is the same defect the check exists to close,
// one level up. It fails when the set GROWS as well as when it shrinks, so adding a
// setting without deciding which backend owns it is a red test rather than a silent gap.
func TestTheEnvironmentLedgersNameEveryVariableEachBackendReads(t *testing.T) {
	declared := map[string]bool{}
	for _, name := range append(append([]string{}, supabaseEnv...), proxyEnv...) {
		if declared[name] {
			t.Fatalf("%s appears in a ledger twice", name)
		}
		declared[name] = true
	}

	// Every `Env*` constant this package exports, discovered rather than restated.
	// Restating them would be the second spelling this test exists to refuse.
	known := []string{
		EnvSupabaseJWKSURL, EnvSupabaseIssuer, EnvSupabaseAudience, EnvSupabaseProvider,
		EnvSupabaseRequireRole, EnvSupabaseLeeway, EnvSupabaseMaxAge,
		EnvProxyFronted, EnvProxySubjectHeader, EnvProxySecret, EnvProxySecretFile,
		EnvProxySecretHeader, EnvProxyRequireClientCert, EnvProxyPeers, EnvProxyProvider,
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
