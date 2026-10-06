package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestTheBinaryREFUSESAnArcJournalInsideTheStoreRoot is the deployed half of operator decision
// Q2: the arc journal lives OUTSIDE the store tree, and the pod refuses to start otherwise.
//
// 🔴 RED BEFORE THE CHECK EXISTED, MEASURED: with `-arc-journal` wired straight into the server
// and no `arcs.ResolveJournalPath` call, all three inside-the-root shapes below CAME UP (this
// test's own "did not exit within 20s" arm). The premise — a directory at the store root is a
// scope — is measured separately in `tokenfile`'s
// `TestADotDirectoryAtTheStoreRootIsEnumeratedAsAScope`.
//
// THREE inside shapes, each a different way to be inside: a dot-directory under the root (the
// "beside the store" spelling), a path OUTSIDE the root that reaches it through a symlinked
// directory (so the check must be on resolved paths), and the root itself. The CONTROL is a path
// outside the root, which must come up — without it, a binary that refused every `-arc-journal`
// would pass the three refusals perfectly.
//
// 78 is asserted as a literal for the reason `TestTheBinaryREFUSESToStartOverAStoreRootItCannotEnumerate`
// gives: renumbering `exitConfig` must be a red test, not a silently renumbered contract.
func TestTheBinaryREFUSESAnArcJournalInsideTheStoreRoot(t *testing.T) {
	tokenFile := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(tokenFile, []byte(testToken+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	writeScope(t, root, "alpha-notes", "gadget-one")
	if err := os.MkdirAll(filepath.Join(root, ".arcs"), 0o755); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	viaLink := filepath.Join(outside, "looks-outside")
	if err := os.Symlink(root, viaLink); err != nil {
		t.Fatal(err)
	}

	start := func(ctx context.Context, journal string) *exec.Cmd {
		child := exec.CommandContext(ctx, self,
			"-store", root, "-host", "127.0.0.1", "-port", strconv.Itoa(freePort(t)),
			"-token-file", tokenFile, "-arc-journal", journal)
		child.Env = []string{
			runServerEnv + "=1",
			testRefreshEnv + "=" + testRefreshPeriod.String(),
			"SUBSYSTEM_STORE_TRUSTED_PROXIES=192.0.2.0/24",
		}
		return child
	}

	for _, tc := range []struct{ name, journal string }{
		{"a dot-directory under the root", filepath.Join(root, ".arcs", "journal.jsonl")},
		{"a symlinked directory that resolves into the root", filepath.Join(viaLink, "journal.jsonl")},
		{"the store root itself", root},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			child := start(ctx, tc.journal)
			out, _ := child.CombinedOutput()
			if ctx.Err() != nil {
				t.Fatalf("the child did not exit within 20s — it CAME UP with the arc journal at %s, "+
					"inside the store root, which is the defect:\n%s", tc.journal, out)
			}
			if code := child.ProcessState.ExitCode(); code != 78 {
				t.Fatalf("an arc journal inside the store root must exit 78 (EX_CONFIG), got %d\n%s", code, out)
			}
			for _, want := range []string{"INSIDE the store root", "Refusing to start"} {
				if !strings.Contains(string(out), want) {
					t.Fatalf("the refusal must say %q, got:\n%s", want, out)
				}
			}
		})
	}

	// THE CONTROL: the same binary, the same store, a journal OUTSIDE it — must come up.
	child := start(context.Background(), filepath.Join(outside, "journal.jsonl"))
	log := &lockedBuffer{}
	child.Stdout, child.Stderr = log, log
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = child.Process.Kill()
		_, _ = child.Process.Wait()
	}()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(log.String(), "listening on") {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("control: a journal OUTSIDE the store root must come up, and it did not:\n%s", log.String())
}
