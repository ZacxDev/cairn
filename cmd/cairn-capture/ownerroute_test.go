package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestRouteOriginOwnerUsageRefusals: `-route-origin-owner OWNER=ALIAS` names a CONFIGURED instance
// — and under --run one with a `-worker` — or the binary refuses at exit 2, in both modes that read
// it. A synthetic second instance, `client`, is configured beside the default one.
func TestRouteOriginOwnerUsageRefusals(t *testing.T) {
	home, tokenFile, env := runWorld(t, runToken)
	instances := filepath.Join(home, ".config", "cairn", "instances")
	if err := os.MkdirAll(instances, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(instances, "client.env"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	run1 := []string{"--run", "-host", "host-a", "-worker", "http://127.0.0.1:9", "-capture-token-file", tokenFile}
	dry := []string{"--dry-run", "-claude-root", filepath.Join(home, ".claude", "projects")}
	for name, c := range map[string]struct {
		args []string
		says string
	}{
		"run: an unconfigured alias":          {append(run1, "-route-origin-owner", "acme=elsewhere"), "does not configure"},
		"run: a configured alias, no -worker": {append(run1, "-route-origin-owner", "acme=client"), "has no -worker client=<url>"},
		"run: not OWNER=ALIAS":                {append(run1, "-route-origin-owner", "acme"), "is not OWNER=ALIAS"},
		"run: an owner named twice":           {append(run1, "-route-origin-owner", "acme=personal", "-route-origin-owner", "Acme=personal"), "twice"},
		"dry-run: an unconfigured alias":      {append(dry, "-route-origin-owner", "acme=elsewhere"), "does not configure"},
		"dry-run: not OWNER=ALIAS":            {append(dry, "-route-origin-owner", "git@git.example.invalid:acme=client"), "is not OWNER=ALIAS"},
	} {
		var errb bytes.Buffer
		if code := run(c.args, &bytes.Buffer{}, &errb, env); code != exitUsage || !strings.Contains(errb.String(), c.says) {
			t.Errorf("%s: exit %d, want %d naming %q (%s)", name, code, exitUsage, c.says, errb.String())
		}
	}
	// CONTROL: a well-formed rule naming a configured instance is accepted by the dry run.
	var out, errb bytes.Buffer
	if code := run(append(dry, "-route-origin-owner", "acme=client"), &out, &errb, env); code != exitOK {
		t.Fatalf("CONTROL: a well-formed rule: exit %d (%s)", code, errb.String())
	}
}

// TestATableRoutedRepoWithNoWorkerShipsToTheDefaultThroughTheBinary is #292's audit F1 as the
// auditor reproduced it, through the real wiring and the real HTTPSink: a second instance
// `client` is configured and the routing table sends the repo's scope `beta-notes` to it, but
// `--run` has a `-worker` for the default instance only. The session must ship to the default —
// before the fix it was refused on every run ("no worker listener is configured for instance
// client") at exit 0.
func TestATableRoutedRepoWithNoWorkerShipsToTheDefaultThroughTheBinary(t *testing.T) {
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_SYSTEM", os.DevNull)
	p := newRunPod(t, true)
	home, tokenFile, env := runWorld(t, runToken)
	cfg := filepath.Join(home, ".config", "cairn")
	if err := os.MkdirAll(filepath.Join(cfg, "instances"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfg, "instances", "client.env"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfg, "routes.json"), []byte(`{"beta-notes":"client"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(t.TempDir(), "beta-notes")
	if out, err := exec.Command("git", "init", "-q", repo).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	var b strings.Builder
	for i := 0; i < 5; i++ {
		fmt.Fprintf(&b, `{"type":"user","sessionId":"s-0001","cwd":%q,"message":{"role":"user","content":"line %d"}}`+"\n", repo, i)
	}
	if err := os.WriteFile(filepath.Join(home, ".claude", "projects", "-work-alpha", "s-0001.jsonl"), []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	var errb bytes.Buffer
	code := run([]string{"--run", "-host", "host-a", "-worker", p.url, "-capture-token-file", tokenFile}, &bytes.Buffer{}, &errb, env)
	recs, _ := p.arc.ReadStream("s-0001", "main")
	if code != exitOK || len(recs) != 5 || strings.Contains(errb.String(), "refused s-0001") {
		t.Fatalf("exit %d, %d records stored on the default instance — want 0 and all 5:\n%s", code, len(recs), errb.String())
	}
}
