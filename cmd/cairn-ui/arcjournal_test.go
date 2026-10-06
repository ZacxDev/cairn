package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestTheBinaryREFUSESAnArcJournalInsideTheStoreRoot is the UI's half of operator decision Q2, on
// the BUILT binary — the pod's `TestTheBinaryREFUSESAnArcJournalInsideTheStoreRoot`, restated for
// the surface that mounts the journal read-only.
//
// 🔴 THE OBSERVABLE IS A PROCESS EXIT, SO IN-PROCESS TESTS CANNOT SEE THE WIRING. `main` deciding
// whether to ACT on `resolveArcJournal`'s error is the thing a mutant removes; only a re-exec sees
// the difference between "refused" and "came up serving". Every refusal arm pins WHICH refusal spoke
// (`main` has many `os.Exit(exitConfig)` sites), and the control arms prove the same harness CAN
// see a child come up — with the journal it was given named in the startup line.
func TestTheBinaryREFUSESAnArcJournalInsideTheStoreRoot(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	storeRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(storeRoot, ".arcs"), 0o755); err != nil {
		t.Fatal(err)
	}
	tokenFile := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(tokenFile, []byte(strings.Repeat("s", 43)), 0o600); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "journal.jsonl")
	resolvedOutside, err := filepath.EvalSymlinks(filepath.Dir(outside))
	if err != nil {
		t.Fatal(err)
	}

	for _, arm := range []struct {
		name    string
		journal string // "" = flag not passed
		refuse  bool
		want    string
	}{
		{"a dot-directory at the store root", filepath.Join(storeRoot, ".arcs", "journal.jsonl"), true, "INSIDE the store root"},
		{"the store root itself", storeRoot, true, "INSIDE the store root"},
		{"a whitespace flag value", "   ", true, "reduces to nothing"},
		// The CONTROLS: the same harness sees a child come up, and the startup line names what
		// the source was actually handed.
		{"a journal OUTSIDE the store root", outside, false, "arcs read-only from " + filepath.Join(resolvedOutside, "journal.jsonl")},
		{"no journal at all", "", false, "arcs unconfigured"},
	} {
		t.Run(arm.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			args := []string{"-store", storeRoot, "-token-file", tokenFile,
				"-session-file", filepath.Join(t.TempDir(), "sessions"), "-host", "127.0.0.1", "-port", "0"}
			if arm.journal != "" {
				args = append(args, "-arc-journal", arm.journal)
			}
			child := exec.CommandContext(ctx, self, args...)
			child.Env = []string{reexecEnv + "=1", "PATH=" + os.Getenv("PATH")}
			var out syncBuffer
			child.Stdout, child.Stderr = &out, &out
			if err := child.Start(); err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() { done <- child.Wait() }()

			var exitErr error
			exited := false
			deadline := time.After(20 * time.Second)
		wait:
			for {
				select {
				case exitErr = <-done:
					exited = true
					break wait
				case <-deadline:
					break wait
				case <-time.After(10 * time.Millisecond):
					if strings.Contains(out.String(), "serving") {
						break wait
					}
				}
			}
			if !exited {
				cancel()
				<-done
			}

			if arm.refuse {
				if !exited {
					t.Fatalf("THE CHILD CAME UP with -arc-journal %q — it must refuse to start:\n%s", arm.journal, out.String())
				}
				var exit *exec.ExitError
				if !errors.As(exitErr, &exit) || exit.ExitCode() != exitConfig {
					t.Fatalf("exit %v, want %d (EX_CONFIG):\n%s", exitErr, exitConfig, out.String())
				}
			} else if exited {
				t.Fatalf("CONTROL FAILED: the child exited (%v) instead of coming up, so the refusals above may be "+
					"a program that refuses everything:\n%s", exitErr, out.String())
			}
			if !strings.Contains(out.String(), arm.want) {
				t.Errorf("the output does not contain %q, so this arm cannot tell which branch ran:\n%s", arm.want, out.String())
			}
		})
	}
}
