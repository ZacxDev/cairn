package client

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// 🔴 THIS PACKAGE'S TESTS RUN THE CLIENT IN-PROCESS, SO THEY MUST NOT INHERIT A SESSION ID. Run
// from inside an agent session (`CLAUDE_CODE_SESSION_ID` set, the developer's real
// `XDG_STATE_HOME`), every served call a test made appended FIXTURE scopes to that session's REAL
// read ledger — measured: 24 records from one `go test ./internal/... ./cmd/...` — which the capture
// agent then uploads, making the developer's session owner-only (#266's audit; the Python twin is
// `tests/testlib/env_pin.py`'s `READ_LEDGER_ENV`). So TestMain clears both session ids and pins
// `XDG_STATE_HOME` to a scratch directory before any test runs; a test that wants a ledger sets its
// own. The originals are kept ONLY so [TestTheTestBinaryWritesNoInheritedLedger]'s probe can prove
// its instrument sees a file.
var inheritedSession, inheritedStateHome string

func TestMain(m *testing.M) {
	inheritedSession = os.Getenv(ClaudeSessionEnv)
	inheritedStateHome = os.Getenv(StateHomeEnv)
	os.Unsetenv(ClaudeSessionEnv)
	os.Unsetenv(OpencodeSessionEnv)
	scratch, err := os.MkdirTemp("", "cairn-client-test-state-")
	if err != nil {
		panic(err)
	}
	os.Setenv(StateHomeEnv, scratch)
	code := m.Run()
	os.RemoveAll(scratch)
	os.Exit(code)
}

// probeArg is the flag value that turns [TestLedgerLeakProbe] on: it runs only as the child of
// [TestTheTestBinaryWritesNoInheritedLedger].
const probeArg = "ledger-leak-probe"

// TestLedgerLeakProbe is the CHILD half: a served call made the way an ordinary test makes one —
// no session variable touched — and then, as the POSITIVE CONTROL, the same call with the parent's
// session id and state home put back, which must write exactly one record there.
func TestLedgerLeakProbe(t *testing.T) {
	if len(os.Args) == 0 || os.Args[len(os.Args)-1] != probeArg {
		t.Skip("the child half of TestTheTestBinaryWritesNoInheritedLedger")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CAIRN_MIRROR_ROOT", "")
	// The NEW names, and the old ones UNSET: an old name present warns once per PROCESS
	// (`internal/envalias`), and spending that warning here made a later test that asserts on it
	// order-dependent.
	for _, old := range []string{"SUBSYSTEM_STORE_URL", "SUBSYSTEM_STORE_TOKEN"} {
		t.Setenv(old, "")
		os.Unsetenv(old)
	}
	t.Setenv("CAIRN_URL", "http://127.0.0.1:1")
	t.Setenv("CAIRN_TOKEN", "x")
	dir := configuredHost(t, filepath.Join(home, "config"))
	writeTable(t, dir, `{"alpha-notes": "`+DefaultAlias+`"}`)
	seedCache(t, filepath.Join(home, ".cache", "subsystem-store"), "alpha-notes", "one")
	if code, _, stderr := runCLI(t, "recall", "--no-sync", "--scope", "alpha-notes"); code != ExitOK {
		t.Fatalf("the probe's call exited %d\n%s", code, stderr)
	}
	t.Setenv(ClaudeSessionEnv, inheritedSession)
	t.Setenv(StateHomeEnv, inheritedStateHome)
	if code, _, stderr := runCLI(t, "recall", "--no-sync", "--scope", "alpha-notes"); code != ExitOK {
		t.Fatalf("the control's call exited %d\n%s", code, stderr)
	}
}

// TestTheTestBinaryWritesNoInheritedLedger re-runs this test binary with a sentinel session id and
// state home in its environment — what an agent-run `go test` hands it — and counts the records
// under the sentinel: exactly ONE, the probe's deliberate control. A second is a test's served call
// that inherited the session id.
//
// RED without TestMain's clear: the probe's first call records too, and the file holds 2.
func TestTheTestBinaryWritesNoInheritedLedger(t *testing.T) {
	state := t.TempDir()
	const session = "ses-leak-sentinel"
	cmd := exec.Command(os.Args[0], "-test.run=^TestLedgerLeakProbe$", "-test.count=1", probeArg)
	cmd.Env = append(os.Environ(), ClaudeSessionEnv+"="+session, StateHomeEnv+"="+state)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("the probe failed: %v\n%s", err, out)
	}
	recs := readLedgerFile(t, ledgerPath(state, session))
	if len(recs) != 1 {
		t.Fatalf("the sentinel ledger holds %d record(s), want exactly the control's 1 — a test call "+
			"inherited the session id: %v", len(recs), keys(recs))
	}
}
