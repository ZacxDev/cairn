package main

import (
	"bytes"
	"os"
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
