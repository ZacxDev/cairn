package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ZacxDev/cairn/internal/ui"
)

// line is an `appLine` for one of the three settings, written or not.
func line(flagName, env, value string, written bool) appLine {
	return appLine{flag: flagName, env: env, value: value, written: written}
}

func nameLine(v string) appLine    { return line(flagAppName, EnvUIAppName, v, true) }
func shortLine(v string) appLine   { return line(flagAppShortName, EnvUIAppShortName, v, true) }
func variantLine(v string) appLine { return line(flagAppIconVariant, EnvUIAppIconVariant, v, true) }

var (
	noName    = line(flagAppName, EnvUIAppName, "", false)
	noShort   = line(flagAppShortName, EnvUIAppShortName, "", false)
	noVariant = line(flagAppIconVariant, EnvUIAppIconVariant, "", false)
)

// TestEachAppLineIsJudgedWithItsOwnRefusal drives `resolveApp` over every refusal and the arms that
// must be ADMITTED. Each refusal is pinned by a phrase only it produces, so a removed check shows up
// as a different refusal (or none) rather than as "something refused".
//
// 🔴 THE ADMITTED ARMS ARE HALF THE POINT: a resolver that refused every written line would satisfy
// every refusal below and break every armed deployment.
func TestEachAppLineIsJudgedWithItsOwnRefusal(t *testing.T) {
	for _, tc := range []struct {
		name                 string
		appName, sh, variant appLine
		want                 string // "" = admitted
		wantApp              ui.App
		// wantWarning is a phrase the IGNORED-settings warning must carry; "" = no warning at all.
		wantWarning string
	}{
		{"nothing written is the unarmed surface", noName, noShort, noVariant, "", ui.App{}, ""},
		{"armed", nameLine("cairn (alpha)"), noShort, variantLine("amber"), "",
			ui.App{Name: "cairn (alpha)", IconVariant: "amber"}, ""},
		{"a 12-character short name is admitted", nameLine("n"), shortLine("abcdefghijkl"), variantLine("teal"), "",
			ui.App{Name: "n", ShortName: "abcdefghijkl", IconVariant: "teal"}, ""},
		{"a whitespace name", nameLine("   "), noShort, variantLine("amber"), "reduces to nothing", ui.App{}, ""},
		{"an empty name written as a flag", nameLine(""), noShort, variantLine("amber"), "reduces to nothing", ui.App{}, ""},
		{"a zero-width name", nameLine("\u200b\u200b"), noShort, variantLine("amber"), "reduces to nothing", ui.App{}, ""},
		{"a whitespace short name", nameLine("n"), shortLine(" \t"), variantLine("amber"), "reduces to nothing", ui.App{}, ""},
		{"a whitespace variant", nameLine("n"), noShort, variantLine("  "), "reduces to nothing", ui.App{}, ""},
		{"a 13-character short name", nameLine("n"), shortLine("abcdefghijklm"), variantLine("teal"),
			"at most 12 are accepted", ui.App{}, ""},
		{"a name and no variant", nameLine("n"), noShort, noVariant, "-app-icon-variant / $CAIRN_UI_APP_ICON_VARIANT is not",
			ui.App{}, ""},
		{"a variant outside the set", nameLine("n"), noShort, variantLine("chartreuse"),
			"not a committed icon variant. Refusing to start; choose one of: amber, teal, violet, slate", ui.App{}, ""},
		// 🔴 UNARMED AND WARNED, NEVER REFUSED (audit D1): deleting the name line is how a deployment
		// disarms, so a refusal here would crash-loop the pod on that edit. Each ignored line is named.
		{"a variant and no name starts unarmed", noName, noShort, variantLine("amber"), "", ui.App{},
			`-app-icon-variant / $CAIRN_UI_APP_ICON_VARIANT="amber" IGNORED`},
		{"a short name and no name starts unarmed", noName, shortLine("s"), noVariant, "", ui.App{},
			`-app-short-name / $CAIRN_UI_APP_SHORT_NAME="s" IGNORED`},
		{"both, and no name, names both", noName, shortLine("s"), variantLine("chartreuse"), "", ui.App{},
			`-app-short-name / $CAIRN_UI_APP_SHORT_NAME="s" and -app-icon-variant / $CAIRN_UI_APP_ICON_VARIANT="chartreuse" IGNORED`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app, warning, err := resolveApp(tc.appName, tc.sh, tc.variant)
			if tc.want == "" {
				if err != nil {
					t.Fatalf("refused an admissible configuration: %v", err)
				}
				if app != tc.wantApp {
					t.Fatalf("resolved %+v, want %+v", app, tc.wantApp)
				}
				if tc.wantWarning == "" && warning != "" {
					t.Fatalf("warned %q on a configuration that ignores nothing", warning)
				}
				if tc.wantWarning != "" && !strings.Contains(warning, tc.wantWarning) {
					t.Fatalf("warning %q does not contain %q", warning, tc.wantWarning)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v, want a refusal containing %q", err, tc.want)
			}
		})
	}
}

// startAppChild re-execs this test binary as `cairn-ui` over a seeded journal with `env` added.
func startAppChild(t *testing.T, env []string, extra ...string) *presenceChild {
	t.Helper()
	_, journal := seededJournal(t, credentialLive)
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	// 15 s, not 60: a REFUSAL arrives in milliseconds, and the mutant this file must catch is a child
	// that SERVES — so the deadline is what fails it. Two such arms at 60 s each overran the battery's
	// `-timeout=2m`, which turned a kill into a package timeout no test name was attributed to.
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	args := append([]string{
		"-control-journal", journal,
		"-store", t.TempDir(),
		"-session-file", filepath.Join(t.TempDir(), "sessions"),
		"-token-file", filepath.Join(t.TempDir(), "absent-token"),
		"-host", "127.0.0.1",
	}, extra...)
	cmd := exec.CommandContext(ctx, self, args...)
	cmd.Env = append([]string{reexecEnv + "=1"}, env...)
	c := &presenceChild{cmd: cmd, out: &syncBuffer{}, stdout: &syncBuffer{}, cancel: cancel}
	cmd.Stdout, cmd.Stderr = c.stdout, c.out
	if err := cmd.Start(); err != nil {
		cancel()
		t.Fatal(err)
	}
	c.reap(t)
	return c
}

// TestTheBinaryRefusesEachAppMisconfiguration is the END of the chain: environment or flag →
// `resolveApp` → `os.Exit(exitConfig)`. Each arm exits 78 with ITS OWN refusal, so another of
// `main`'s `os.Exit(exitConfig)` sites cannot satisfy it — and the blank arms arrive through the
// ENVIRONMENT, the arrival path a resolver reading `envalias` would turn into "unset".
func TestTheBinaryRefusesEachAppMisconfiguration(t *testing.T) {
	for _, arm := range []struct {
		name string
		env  []string
		args []string
		want string
	}{
		{"a whitespace $CAIRN_UI_APP_NAME", []string{EnvUIAppName + "=   ", EnvUIAppIconVariant + "=amber"}, nil,
			"-app-name / $CAIRN_UI_APP_NAME is set to \"   \", which reduces to nothing"},
		{"an explicit empty -app-name", nil, []string{"-app-name=", "-app-icon-variant", "amber"},
			"-app-name / $CAIRN_UI_APP_NAME is set to \"\", which reduces to nothing"},
		{"a 13-character short name", nil,
			[]string{"-app-name", "n", "-app-short-name", "abcdefghijklm", "-app-icon-variant", "teal"},
			"at most 12 are accepted"},
		{"a name and no variant", nil, []string{"-app-name", "n"},
			"-app-name / $CAIRN_UI_APP_NAME is set and -app-icon-variant / $CAIRN_UI_APP_ICON_VARIANT is not"},
		{"a variant outside the set", nil, []string{"-app-name", "n", "-app-icon-variant", "chartreuse"},
			"choose one of: amber, teal, violet, slate"},
	} {
		t.Run(arm.name, func(t *testing.T) {
			c := startAppChild(t, arm.env, append([]string{"-port", "0"}, arm.args...)...)
			if code := c.exit(t); code != exitConfig {
				t.Fatalf("exit %d, want %d (EX_CONFIG)\n%s", code, exitConfig, c.out.String())
			}
			if !strings.Contains(c.out.String(), arm.want) {
				t.Fatalf("stderr does not contain %q, so this arm cannot tell which refusal fired:\n%s",
					arm.want, c.out.String())
			}
		})
	}
}

// TestTheBinaryServesTheManifestItWasArmedWith drives `main` end to end in BOTH states: armed, the
// running binary serves a manifest carrying the flags' values (a 12-character short name ADMITTED)
// and the startup line names the app; unarmed, the same row answers 404 and the line says so. It is
// what notices a resolved app that never reached `ui.Config` — every `internal/ui` test hands the
// server an `App` itself.
func TestTheBinaryServesTheManifestItWasArmedWith(t *testing.T) {
	for _, arm := range []struct {
		name     string
		args     []string
		wantCode int
		wantLine string
	}{
		{"armed", []string{"-app-name", "cairn (alpha)", "-app-short-name", "abcdefghijkl", "-app-icon-variant", "violet"},
			http.StatusOK, `app "cairn (alpha)" (icon variant violet)`},
		{"unarmed", nil, http.StatusNotFound, "app unarmed (no -app-name"},
		// 🔴 AUDIT D1 / N2, AT THE PROCESS: a variant with no name STARTS (an exit 78 here crash-loops
		// a pod whose operator just deleted the name line), serves NO manifest, and says why on stderr.
		{"a variant and no name starts unarmed and warns", []string{"-app-icon-variant", "teal"}, http.StatusNotFound,
			`cairn-ui: WARNING -app-icon-variant / $CAIRN_UI_APP_ICON_VARIANT="teal" IGNORED`},
	} {
		t.Run(arm.name, func(t *testing.T) {
			port := aPortNothingIsListeningOn(t)
			c := startAppChild(t, nil, append([]string{"-port", fmt.Sprint(port)}, arm.args...)...)
			c.waitFor(t, "the serving line", func() bool { return strings.Contains(c.out.String(), "serving") })
			if !strings.Contains(c.out.String(), arm.wantLine) {
				t.Errorf("the startup line does not say %q:\n%s", arm.wantLine, c.out.String())
			}
			var resp *http.Response
			c.waitFor(t, "the manifest row to answer", func() bool {
				r, err := (&http.Client{Timeout: time.Second}).Get(fmt.Sprintf("http://127.0.0.1:%d%s", port, ui.ManifestPath))
				if err != nil {
					return false
				}
				resp = r
				return true
			})
			defer resp.Body.Close()
			body, _ := io.ReadAll(resp.Body)
			if resp.StatusCode != arm.wantCode {
				t.Fatalf("GET %s answered %d, want %d: %s", ui.ManifestPath, resp.StatusCode, arm.wantCode, body)
			}
			if arm.wantCode != http.StatusOK {
				return
			}
			var m struct {
				Name      string `json:"name"`
				ShortName string `json:"short_name"`
			}
			if err := json.Unmarshal(body, &m); err != nil {
				t.Fatalf("the manifest is not JSON: %v\n%s", err, body)
			}
			if m.Name != "cairn (alpha)" || m.ShortName != "abcdefghijkl" {
				t.Errorf("the running binary's manifest says name=%q short_name=%q, want the flags' values", m.Name, m.ShortName)
			}
		})
	}
}
