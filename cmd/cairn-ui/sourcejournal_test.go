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

	"github.com/ZacxDev/cairn/internal/codesrc"
)

// TestTheBinaryREFUSESASourceJournalInsideTheStoreRoot is the UI's half of decision 1 of the scope-refs
// plan, on the BUILT binary: `$CAIRN_SOURCE_JOURNAL` read through `codesrc.FromEnv` — the variable's
// ONE reader, which the pod calls too — and every refusal naming the SOURCES journal. The arc-journal
// test's harness, for its reason: the observable is a process exit, so only a re-exec sees whether
// `main` ACTS on the refusal. The CONTROL arms prove the same harness sees a child come up, and that the
// startup line names the journal the surface was actually handed.
//
// RED, MEASURED: with `main` ignoring `FromEnv`'s error, all four refusal arms fail and both controls
// still pass — so the arms see `main` ACTING on the refusal, not only the function returning one.
func TestTheBinaryREFUSESASourceJournalInsideTheStoreRoot(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	storeRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(storeRoot, ".sources"), 0o755); err != nil {
		t.Fatal(err)
	}
	tokenFile := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(tokenFile, []byte(strings.Repeat("s", 43)), 0o600); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "sources.jsonl")
	resolvedOutside, err := filepath.EvalSymlinks(filepath.Dir(outside))
	if err != nil {
		t.Fatal(err)
	}
	arcJournal := filepath.Join(t.TempDir(), "arcs.jsonl")

	for _, arm := range []struct {
		name    string
		journal string // "" = variable not set
		arc     string // "" = no -arc-journal
		refuse  bool
		want    string
	}{
		{"a dot-directory at the store root", filepath.Join(storeRoot, ".sources", "sources.jsonl"), "", true, codesrc.EnvJournal},
		{"the store root itself", storeRoot, "", true, codesrc.EnvJournal},
		{"a value that reduces to nothing", "   ", "", true, "reduces to nothing"},
		{"the arc journal's own file", arcJournal, arcJournal, true, "is the ARC journal"},
		// The CONTROLS.
		{"a journal OUTSIDE the store root", outside, "", false, "code sources written to " + filepath.Join(resolvedOutside, "sources.jsonl")},
		{"no journal at all", "", "", false, "code sources unconfigured"},
	} {
		t.Run(arm.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			args := []string{"-store", storeRoot, "-token-file", tokenFile,
				"-session-file", filepath.Join(t.TempDir(), "sessions"), "-host", "127.0.0.1", "-port", "0"}
			if arm.arc != "" {
				args = append(args, "-arc-journal", arm.arc)
			}
			child := exec.CommandContext(ctx, self, args...)
			child.Env = []string{reexecEnv + "=1", "PATH=" + os.Getenv("PATH")}
			if arm.journal != "" {
				child.Env = append(child.Env, codesrc.EnvJournal+"="+arm.journal)
			}
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
					t.Fatalf("THE CHILD CAME UP with $CAIRN_SOURCE_JOURNAL %q — it must refuse to start:\n%s", arm.journal, out.String())
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
