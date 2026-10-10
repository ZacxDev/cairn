package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func envOf(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func TestTheModeLedgerIsWhatVerbsPrints(t *testing.T) {
	var out bytes.Buffer
	if code := run([]string{"-verbs"}, &out, &bytes.Buffer{}, envOf(nil)); code != exitOK {
		t.Fatalf("-verbs exited %d", code)
	}
	want := "dry-run reads\nrun writes-spool\nself-test reads\nverbs reads\n"
	if out.String() != want {
		t.Fatalf("-verbs printed\n%s\nwant\n%s", out.String(), want)
	}
}

// TestTheSelfTestIsClosingConditionPart3: exit 0 and the SUMMARY pair as its last line, at the
// default seed and one other.
func TestTheSelfTestIsClosingConditionPart3(t *testing.T) {
	for _, args := range [][]string{{"--self-test"}, {"--self-test", "-seed", "42"}} {
		var out bytes.Buffer
		code := run(args, &out, &bytes.Buffer{}, envOf(nil))
		lines := strings.Split(strings.TrimSpace(out.String()), "\n")
		if code != exitOK || lines[len(lines)-1] != "SUMMARY redaction: planted=26 caught=26 clean-damaged=0" {
			t.Fatalf("%v: exit %d, output:\n%s", args, code, out.String())
		}
	}
}

func TestUsageRefusals(t *testing.T) {
	for _, args := range [][]string{{}, {"-state", t.TempDir()}, {"-no-such-flag"}, {"-state", t.TempDir(), "-spool", t.TempDir(), "extra"}} {
		var errb bytes.Buffer
		if code := run(args, &bytes.Buffer{}, &errb, envOf(map[string]string{"HOME": t.TempDir()})); code != exitUsage {
			t.Errorf("%v: exit %d, want %d (%s)", args, code, exitUsage, errb.String())
		}
	}
}

// TestARunWritesTheSpoolAndNothingElse drives the binary's real wiring end to end over one
// synthetic session: host key created 0600, state saved, the spool written — and no network.
func TestARunWritesTheSpoolAndNothingElse(t *testing.T) {
	home := t.TempDir()
	proj := filepath.Join(home, "projects", "-work-alpha")
	if err := os.MkdirAll(proj, 0o700); err != nil {
		t.Fatal(err)
	}
	line := `{"type":"user","sessionId":"s-0001","message":{"role":"user","content":"hello"}}` + "\n"
	if err := os.WriteFile(filepath.Join(proj, "s-0001.jsonl"), []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
	state, spool := filepath.Join(home, "state"), filepath.Join(home, "spool")
	var errb bytes.Buffer
	code := run([]string{"-state", state, "-spool", spool, "-claude-root", filepath.Join(home, "projects")},
		&bytes.Buffer{}, &errb, envOf(map[string]string{"HOME": home, "XDG_STATE_HOME": filepath.Join(home, "xdg")}))
	if code != exitOK {
		t.Fatalf("exit %d: %s", code, errb.String())
	}
	got, err := os.ReadFile(filepath.Join(spool, "personal", "s-0001", "stream-main.jsonl"))
	if err != nil || !strings.Contains(string(got), `"hello"`) {
		t.Fatalf("the spool does not hold the session: %v %q", err, got)
	}
	if st, err := os.Stat(filepath.Join(state, "host.key")); err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("host key: %v", err)
	}
	if _, err := os.Stat(filepath.Join(state, "state.json")); err != nil {
		t.Fatalf("state not saved: %v", err)
	}
	if !strings.Contains(errb.String(), "sessions=1 shipped=1") {
		t.Fatalf("summary line: %q", errb.String())
	}
}
