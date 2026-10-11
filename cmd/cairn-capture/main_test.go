package main

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func envOf(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

// TestTheSelfTestIsClosingConditionPart3: exit 0 and the SUMMARY pair as its last line, at the
// default seed and one other. The three `control …` lines `redact.SelfTest` prints first are not
// pinned here; only the last line is the closing condition. 79 is the corpus's declared plant
// count, written as a literal rather than read from `redact.DeclaredPlants`, so a corpus that
// shrinks has to move this line too.
func TestTheSelfTestIsClosingConditionPart3(t *testing.T) {
	for _, args := range [][]string{{"--self-test"}, {"--self-test", "-seed", "42"}} {
		var out bytes.Buffer
		code := run(args, &out, &bytes.Buffer{}, envOf(nil))
		lines := strings.Split(strings.TrimSpace(out.String()), "\n")
		if code != exitOK || lines[len(lines)-1] != "SUMMARY redaction: planted=79 caught=79 clean-damaged=0" {
			t.Fatalf("%v: exit %d, output:\n%s", args, code, out.String())
		}
	}
}

func TestUsageRefusals(t *testing.T) {
	for _, args := range [][]string{{}, {"-no-such-flag"}, {"--dry-run", "extra"}, {"-verbs"}} {
		var errb bytes.Buffer
		if code := run(args, &bytes.Buffer{}, &errb, envOf(map[string]string{"HOME": t.TempDir()})); code != exitUsage {
			t.Errorf("%v: exit %d, want %d (%s)", args, code, exitUsage, errb.String())
		}
	}
}

func listFiles(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			out = append(out, p)
		}
		return nil
	})
	return out
}

// TestADryRunWritesNothing: one synthetic session, the binary's real wiring — and not one file
// created anywhere under HOME, the host key included (review round 1).
func TestADryRunWritesNothing(t *testing.T) {
	home := t.TempDir()
	proj := filepath.Join(home, "projects", "-work-alpha")
	if err := os.MkdirAll(proj, 0o700); err != nil {
		t.Fatal(err)
	}
	line := `{"type":"user","sessionId":"s-0001","message":{"role":"user","content":"hello"}}` + "\n"
	if err := os.WriteFile(filepath.Join(proj, "s-0001.jsonl"), []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
	before := listFiles(t, home)
	var out, errb bytes.Buffer
	code := run([]string{"--dry-run", "-claude-root", filepath.Join(home, "projects")}, &out, &errb,
		envOf(map[string]string{"HOME": home, "XDG_STATE_HOME": filepath.Join(home, "xdg")}))
	if code != exitOK || !strings.HasPrefix(out.String(), "claude s-0001 ") {
		t.Fatalf("exit %d, out %q, err %q", code, out.String(), errb.String())
	}
	if after := listFiles(t, home); len(after) != len(before) {
		t.Fatalf("--dry-run created files: %v", after)
	}
}
