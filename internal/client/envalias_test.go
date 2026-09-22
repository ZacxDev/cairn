package client

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ZacxDev/cairn/internal/envalias"
	"github.com/ZacxDev/cairn/internal/hostid"
)

// clientConfigNames is every variable that configures THIS client, in BOTH spellings.
//
// 🔴 SWEPT BY PREFIX, NOT LISTED — AND THE VERSION BEFORE THIS ONE CLAIMED THE SWEEP
// WITHOUT DOING IT. Its comment called this "the Go-side sibling of
// `tests/testlib/env_pin.py`, which does the same job". `env_pin` clears by PREFIX plus the
// derived host-label names; this function hand-listed four extras beside the ledger and so
// missed `CAIRN_MIRROR_ROOT`, which `cli.go` reads with a direct `os.Getenv`. A developer
// with that exported reached a test that believed it had cleared the environment. The
// docstring named a relationship while the body inspected one side of it.
//
// Three sources, none of them a restatement:
//   - every variable in the PROCESS environment under either prefix, which is what makes
//     the claim above true and picks up a name nothing here knows about;
//   - `hostid.HostLabelEnv`, because two of those (`ASIB_HOST`, `ACTIVITY_HOST`) carry
//     NEITHER prefix and a prefix sweep structurally cannot see them;
//   - the ledger, plus the four constants this package owns, so the clear does not depend
//     on what the operator happens to have exported.
func clientConfigNames() []string {
	seen := map[string]bool{}
	var out []string
	add := func(name string) {
		if name == "" || seen[name] {
			return
		}
		seen[name] = true
		out = append(out, name)
	}
	for _, entry := range os.Environ() {
		if key, _, ok := strings.Cut(entry, "="); ok {
			if strings.HasPrefix(key, "CAIRN_") || strings.HasPrefix(key, "SUBSYSTEM_STORE_") {
				add(key)
			}
		}
	}
	for _, name := range hostid.HostLabelEnv {
		add(name)
	}
	add(ConfigEnv)
	add(RoutesEnv)
	add(EnvURL)
	add(EnvToken)
	for _, p := range envalias.Ledger {
		add(p.New)
		add(p.Old)
	}
	return out
}

// TestTheHermeticSweepClearsWhatTheClientActuallyREADS is the positive control on
// `clientConfigNames`, and it exists because the list it replaced was wrong in exactly the
// way a list is wrong: quietly, and only about the name nobody re-checked.
//
// ⚠ AN INVARIANT GUARD. No shipped defect ever turned on it; it pins that the sweep covers
// the names the client reads directly, which a hand-list stopped doing once.
func TestTheHermeticSweepClearsWhatTheClientActuallyREADS(t *testing.T) {
	// Exported so the prefix arm has something to find, and so this fails if that arm is
	// deleted: `CAIRN_MIRROR_ROOT` is not in the ledger and not one of the four constants.
	t.Setenv("CAIRN_MIRROR_ROOT", "/somewhere/an/operator/exported")
	covered := map[string]bool{}
	for _, name := range clientConfigNames() {
		covered[name] = true
	}
	for _, name := range []string{"CAIRN_MIRROR_ROOT", "CAIRN_HOST", "ASIB_HOST", "ACTIVITY_HOST"} {
		if !covered[name] {
			t.Fatalf("clientConfigNames() does not clear %q, so a developer who exported it "+
				"reaches every test in this file; got %v", name, clientConfigNames())
		}
	}
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

// TestTheNEWNamesConfigureTheDefaultInstance is REGRESSION coverage for the rename.
//
// 🔴 HOW THE BASELINE WAS ACTUALLY OBTAINED, BECAUSE THE EARLIER SENTENCE CLAIMED A RUN
// THAT CANNOT HAVE HAPPENED. It said "measured RED at `f74657d9`". This FILE does not
// exist at `f74657d`, and neither do `EnvURL`/`EnvToken`, so the package does not compile
// there and no run of this test at that commit is possible. What was measured instead —
// and what the claim actually needs — is the pre-change BINARY: `git archive f74657d`,
// `go build ./cmd/cairn`, then the same world this test builds:
//
//	CAIRN_URL / CAIRN_TOKEN set, nothing else
//	base  => "config incomplete: SUBSYSTEM_STORE_URL, SUBSYSTEM_STORE_TOKEN not set"
//	HEAD  => reaches the URL (connection refused at 127.0.0.1:19201)
//
// So the BEHAVIOUR is red at base and green at HEAD; the test file is not what was run
// there. Those are different claims and only the second one was ever true.
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
// ⚠ AN INVARIANT GUARD, NOT REGRESSION COVERAGE — the pre-change binary honours these
// names too, by construction: that tree read only these names. It is here because the window is a
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

// TestTheNewNameWinsOverTheDeprecatedOne is REGRESSION coverage, with the baseline taken
// the way the comment above describes — against the pre-change BINARY, not by running this
// file at a commit where it does not exist. With both spellings exported, the base client
// reaches the OLD name's URL (127.0.0.1:19202) because that is the only name it reads;
// HEAD reaches the new one.
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

// TestEVERYConfigPointerReaderHonoursTheDeprecatedSpelling is the guard for the defect this
// change shipped and the parity harness caught: `LoadConfigFor` resolved the alias while the
// ROUTING layer's own copy of "where is the config file" did not.
//
// 🔴 THE SYMPTOM DID NOT LOOK LIKE AN ENVIRONMENT BUG. With `$SUBSYSTEM_STORE_CONFIG` naming
// a two-instance world, the credential loader found the file and the routing layer did not —
// so the client reported the routed scope as living on an instance "not configured on this
// host", and the recall banner lost its `[personal]` label because the instance COUNT came
// back as one. Every Go test stayed green; only a byte diff against the other client saw it.
//
// It asserts the RELATIONSHIP — all three readers derive from ONE pointer — rather than
// testing `ConfigPath` alone, because testing the one reader that was already correct is
// exactly what the previous round did.
func TestEVERYConfigPointerReaderHonoursTheDeprecatedSpelling(t *testing.T) {
	home := hermetic(t)
	world := filepath.Join(home, "elsewhere")
	if err := os.MkdirAll(filepath.Join(world, InstanceDirName), 0o700); err != nil {
		t.Fatal(err)
	}
	cfg := filepath.Join(world, "env")
	t.Setenv("SUBSYSTEM_STORE_CONFIG", cfg)

	if got := ConfigPath(nil); got != cfg {
		t.Errorf("ConfigPath ignored the deprecated spelling: %q", got)
	}
	if got := DefaultConfigPath(); got != cfg {
		t.Errorf("DefaultConfigPath disagrees with ConfigPath: %q", got)
	}
	dir, err := InstanceDir(nil)
	if err != nil || dir != filepath.Join(world, InstanceDirName) {
		t.Errorf("InstanceDir ignored the deprecated spelling: %q %v", dir, err)
	}
	routes, explicit, err := RoutesFile(nil)
	if err != nil || explicit || routes != filepath.Join(world, RoutesFileName) {
		t.Errorf("RoutesFile ignored the deprecated spelling: %q explicit=%v %v",
			routes, explicit, err)
	}
}

// TestAnInjectedLookupAlsoResolvesAliases.
//
// 🔴 THE INJECTED GETTER IS THE SHAPE THE BUG LIVED IN. `lookup` takes a caller-supplied
// `func(string) string`, and the first fix wrapped only the `nil` (process-environment) arm —
// which every unit test in this package exercises and no real run does. Both arms, or the
// guard is a claim about the case that was never broken.
func TestAnInjectedLookupAlsoResolvesAliases(t *testing.T) {
	get := func(name string) string {
		if name == "SUBSYSTEM_STORE_CONFIG" {
			return "/tmp/synthetic/injected/env"
		}
		return ""
	}
	if got := ConfigPath(get); got != "/tmp/synthetic/injected/env" {
		t.Fatalf("an injected lookup did not resolve the alias: %q", got)
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
