package main

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ZacxDev/cairn/internal/worker"
)

// The plugin registry's startup surface (S5 of the transcripts plan): the predicate in-process, and
// the mint mode and the served plugin API through `main` — whether `main` hands the registry to the
// worker listener is wiring no in-process test can see.

const fixtureRegistry = `{"schema":1,"plugins":[{"schema":1,"name":"summary","version":"0.1.0","description":"synthetic",` +
	`"toggle_key":"summary","capabilities":["pending:list","transcript:read","output:write:summary"],` +
	`"output_types":["summary"],"calls_model":true}]}`

func writeRegistry(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "registry.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestThePluginFlagsAreBothOrNeitherAndNeedTheListener: neither ⇒ off; one ⇒ a half-configuration;
// both with no worker listener ⇒ refused; a journal inside the store root, or AT a file this program
// already writes ⇒ refused; a registry the closed vocabulary refuses ⇒ refused; both, well formed,
// with the listener ⇒ on.
func TestThePluginFlagsAreBothOrNeitherAndNeedTheListener(t *testing.T) {
	storeRoot := t.TempDir()
	reg := writeRegistry(t, fixtureRegistry)
	journal := filepath.Join(t.TempDir(), "toggles.jsonl")
	control := filepath.Join(t.TempDir(), "control.journal")
	if err := os.WriteFile(control, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	others := map[string]string{"-control-journal": control}
	for _, c := range []struct {
		name     string
		s        pluginSettings
		workerOn bool
		want     string
	}{
		{"a registry alone", pluginSettings{registry: reg}, true, "half-configuration"},
		{"a journal alone", pluginSettings{journal: journal}, true, "half-configuration"},
		{"both with no worker listener", pluginSettings{registry: reg, journal: journal}, false, "nowhere to run"},
		{"a journal inside the store root", pluginSettings{registry: reg, journal: filepath.Join(storeRoot, "toggles.jsonl")}, true, "INSIDE"},
		{"the journal at the control journal", pluginSettings{registry: reg, journal: control}, true, "-control-journal"},
		{"a blank registry", pluginSettings{registry: " ", journal: journal}, true, "reduces to nothing"},
		{"an unknown capability", pluginSettings{registry: writeRegistry(t, strings.Replace(fixtureRegistry,
			`"pending:list"`, `"store:read"`, 1)), journal: journal}, true, "closed vocabulary"},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, _, on, err := pluginsConfigured(c.s, storeRoot, c.workerOn, others)
			if err == nil || on || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("pluginsConfigured = on=%v err=%v, want a refusal saying %q", on, err, c.want)
			}
		})
	}
	if r, _, on, err := pluginsConfigured(pluginSettings{}, storeRoot, true, others); err != nil || on || r != nil {
		t.Fatalf("neither flag: on=%v err=%v, want the designed off state", on, err)
	}
	r, j, on, err := pluginsConfigured(pluginSettings{registry: reg, journal: journal}, storeRoot, true, others)
	if err != nil || !on || !r.Holds("summary") || j.Path == "" {
		t.Fatalf("POSITIVE CONTROL: both flags with the listener: on=%v err=%v", on, err)
	}
}

// TestTheBinaryMintsAPluginTokenAndServesThePluginAPI: `-issue-worker-token plugin` mints a row bound
// to a registered plugin (and refuses an unregistered one), and a child started with the registry
// answers that token on the pending route — an empty list, every toggle OFF — and announces the
// registry on its startup line.
func TestTheBinaryMintsAPluginTokenAndServesThePluginAPI(t *testing.T) {
	_, journal := seededJournal(t, credentialLive)
	tokens := workerTokenFile(t)
	reg := writeRegistry(t, fixtureRegistry)

	stranger := startPresenceChild(t, journal, "-issue-worker-token", "plugin", "-worker-plugin", "stranger",
		"-plugin-registry", reg, "-worker-owner", workerOwnerA.String(), "-worker-tokens", tokens)
	if code := stranger.exit(t); code != exitConfig || !strings.Contains(stranger.out.String(), "naming a plugin") {
		t.Fatalf("minting for an unregistered plugin exited %d:\n%s", code, stranger.out.String())
	}
	mint := startPresenceChild(t, journal, "-issue-worker-token", "plugin", "-worker-plugin", "summary",
		"-plugin-registry", reg, "-worker-owner", workerOwnerA.String(), "-worker-tokens", tokens)
	if code := mint.exit(t); code != 0 {
		t.Fatalf("the plugin mint exited %d:\n%s", code, mint.out.String())
	}
	token := strings.TrimSuffix(mint.stdout.String(), "\n")
	raw, _ := os.ReadFile(tokens)
	if len(token) != 43 || !strings.HasSuffix(string(raw), worker.NewPluginTokenRow("summary", token).String()+"\n") {
		t.Fatalf("the mint printed %q and the file is %q", token, raw)
	}

	child := startPresenceChild(t, journal, workerArgs(t, tokens, "-plugin-registry", reg,
		"-plugin-journal", filepath.Join(t.TempDir(), "toggles.jsonl"))...)
	child.serving(t)
	m := child.workerAddr(t)
	if !strings.Contains(child.out.String(), "1 plugin(s) registered (summary)") {
		t.Fatalf("the startup line does not announce the registry:\n%s", child.out.String())
	}
	req, _ := http.NewRequest(http.MethodGet, "http://"+m[1]+"/plugin/v1/pending", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body := make([]byte, 256)
	n, _ := resp.Body.Read(body)
	if resp.StatusCode != http.StatusOK || string(body[:n]) != `{"items":[]}`+"\n" {
		t.Fatalf("the pending route answered %d %q, want 200 and no item (every toggle OFF)", resp.StatusCode, body[:n])
	}
}
