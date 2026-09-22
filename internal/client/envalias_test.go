package client

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ZacxDev/cairn/internal/envalias"
)

// clientConfigNames is every variable that configures THIS client, in BOTH spellings.
//
// 🔴 DERIVED FROM THE LEDGER, NEVER RESTATED. The hand-written list it replaced named four
// names, and the rename would have left three of them pointing at a spelling the client no
// longer reads FIRST — so a developer with `CAIRN_URL` exported would have had their live
// store reach a test that believed it had cleared the environment. Deriving it means a
// twelfth pair is cleared on the day it is added.
//
// It is the Go-side sibling of `tests/testlib/env_pin.py`, which does the same job for the
// Python suites and already swept both prefixes before this change.
func clientConfigNames() []string {
	out := []string{ConfigEnv, RoutesEnv, EnvURL, EnvToken}
	for _, p := range envalias.Ledger {
		out = append(out, p.New, p.Old)
	}
	return out
}

// hermetic points HOME at a fresh directory and clears every configuration pointer in both
// spellings, so nothing an operator exported can reach the run.
func hermetic(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	for _, key := range clientConfigNames() {
		t.Setenv(key, "")
	}
	return home
}

// TestTheNEWNamesConfigureTheDefaultInstance is REGRESSION coverage for the rename:
// measured RED at `f74657d9` (the pre-change client does not read `CAIRN_URL` at all and
// refuses with "config incomplete") and green at HEAD.
func TestTheNEWNamesConfigureTheDefaultInstance(t *testing.T) {
	hermetic(t)
	t.Setenv(EnvURL, "http://127.0.0.1:9999")
	t.Setenv(EnvToken, "from-the-new-name")

	cfg, err := LoadConfigFor(DefaultAlias)
	if err != nil {
		t.Fatalf("the new names must configure the client: %v", err)
	}
	if cfg.URL != "http://127.0.0.1:9999" || cfg.Token != "from-the-new-name" {
		t.Fatalf("got %#v", cfg)
	}
}

// TestTheDEPRECATEDNamesStillConfigureTheDefaultInstance is the deprecation window's own
// claim.
//
// ⚠ AN INVARIANT GUARD, NOT REGRESSION COVERAGE — it is green at `f74657d9` too, by
// construction: that tree read only these names. It is here because the window is a
// PROMISE, and the thing that will eventually break it is somebody deleting the alias
// before P8 rather than a defect.
func TestTheDEPRECATEDNamesStillConfigureTheDefaultInstance(t *testing.T) {
	hermetic(t)
	t.Setenv("SUBSYSTEM_STORE_URL", "http://127.0.0.1:9998")
	t.Setenv("SUBSYSTEM_STORE_TOKEN", "from-the-old-name")

	cfg, err := LoadConfigFor(DefaultAlias)
	if err != nil {
		t.Fatalf("the deprecated names must keep working until P8: %v", err)
	}
	if cfg.URL != "http://127.0.0.1:9998" || cfg.Token != "from-the-old-name" {
		t.Fatalf("got %#v", cfg)
	}
}

// TestTheNewNameWinsOverTheDeprecatedOne is REGRESSION coverage: measured RED at
// `f74657d9`, where the old name is the ONLY one read and therefore wins by default.
func TestTheNewNameWinsOverTheDeprecatedOne(t *testing.T) {
	hermetic(t)
	t.Setenv(EnvURL, "http://127.0.0.1:9999")
	t.Setenv(EnvToken, "from-the-new-name")
	t.Setenv("SUBSYSTEM_STORE_URL", "http://127.0.0.1:1/shadowed")
	t.Setenv("SUBSYSTEM_STORE_TOKEN", "from-the-old-name")

	cfg, err := LoadConfigFor(DefaultAlias)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.URL != "http://127.0.0.1:9999" || cfg.Token != "from-the-new-name" {
		t.Fatalf("the deprecated name shadowed its replacement: %#v", cfg)
	}
}

// TestTheConfigFileAcceptsBothSpellingsWithTheSamePrecedence.
//
// 🔴 THE FILE IS AN ALIAS SURFACE TOO, AND FORGETTING IT IS THE SILENT HALF-MIGRATION.
// `SUBSYSTEM_STORE_URL=` is a KEY inside `~/.config/subsystem-store/env` as much as it is
// an exported variable. A rename that covered only the environment would leave an operator
// who edited their config file with a client that ignores the edit and falls back to
// nothing — "config incomplete" on a file they just corrected.
func TestTheConfigFileAcceptsBothSpellingsWithTheSamePrecedence(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
		want string
	}{
		{"new keys", "CAIRN_URL=http://127.0.0.1:9001\nCAIRN_TOKEN=t\n", "http://127.0.0.1:9001"},
		{"old keys", "SUBSYSTEM_STORE_URL=http://127.0.0.1:9002\nSUBSYSTEM_STORE_TOKEN=t\n", "http://127.0.0.1:9002"},
		{
			"both, new wins",
			"SUBSYSTEM_STORE_URL=http://127.0.0.1:1/shadowed\nSUBSYSTEM_STORE_TOKEN=t\n" +
				"CAIRN_URL=http://127.0.0.1:9003\nCAIRN_TOKEN=t\n",
			"http://127.0.0.1:9003",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := hermetic(t)
			dir := filepath.Join(home, ".config", "subsystem-store")
			if err := os.MkdirAll(dir, 0o700); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, "env")
			if err := os.WriteFile(path, []byte(tc.body), 0o600); err != nil {
				t.Fatal(err)
			}
			cfg, err := LoadConfigFor(DefaultAlias)
			if err != nil {
				t.Fatalf("%v", err)
			}
			if cfg.URL != tc.want {
				t.Fatalf("got %q, want %q", cfg.URL, tc.want)
			}
		})
	}
}

// TestTheIncompleteRefusalNamesTheCURRENTSpelling.
//
// The refusal is what an operator reads when nothing is configured, so it is the one place
// the client actively TELLS somebody which name to set. Naming the deprecated one would
// steer every new user into the alias.
func TestTheIncompleteRefusalNamesTheCURRENTSpelling(t *testing.T) {
	hermetic(t)
	_, err := LoadConfigFor(DefaultAlias)
	if err == nil {
		t.Fatal("a fully unconfigured client must refuse")
	}
	for _, want := range []string{EnvURL, EnvToken} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not name %s: %v", want, err)
		}
	}
	for _, p := range envalias.Ledger {
		if strings.Contains(err.Error(), p.Old) {
			t.Errorf("the refusal steers the operator at the deprecated %s: %v", p.Old, err)
		}
	}
}
