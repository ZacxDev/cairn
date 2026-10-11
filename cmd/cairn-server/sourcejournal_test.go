package main

import (
	"context"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ZacxDev/cairn/internal/codesrc"
)

// TestTheBinaryREFUSESASourceJournalInsideTheStoreRoot is the deployed half of decision 1 of the
// scope-refs plan: the code-sources journal lives OUTSIDE the store tree (a re-seed overwrites the
// store, and every directory at its root is a scope to the token-file authority), and the pod
// refuses to start otherwise — with a refusal naming the SOURCES journal and its variable, never
// the arc journal.
//
// 🔴 RED FIRST, MEASURED: before `main` read $CAIRN_SOURCE_JOURNAL at all, every inside shape below
// CAME UP (the "did not exit within 20s" arm), because an unknown variable is ignored — which is
// exactly the property that makes the variable the rollback story, and exactly why the refusal has
// to be written rather than assumed.
//
// 🔴 TWO CONTROLS, AND THEY ARE THE SEAM TEST. A path OUTSIDE the root must come up AND serve the
// declaration it holds over `GET /api/v1/sources/<scope>` — so `main` provably hands the variable
// to the server, which no `internal/api` test can see — and with the variable UNSET the same
// binary must come up answering `sources-unconfigured`. Without the first, a binary that refused
// every value would pass the refusals; without the second, one that required the variable would.
func TestTheBinaryREFUSESASourceJournalInsideTheStoreRoot(t *testing.T) {
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
	if err := os.MkdirAll(filepath.Join(root, ".sources"), 0o755); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	viaLink := filepath.Join(outside, "looks-outside")
	if err := os.Symlink(root, viaLink); err != nil {
		t.Fatal(err)
	}

	command := func(ctx context.Context, port int, journal *string) *exec.Cmd {
		child := exec.CommandContext(ctx, self,
			"-store", root, "-host", "127.0.0.1", "-port", strconv.Itoa(port),
			"-token-file", tokenFile)
		child.Env = []string{
			runServerEnv + "=1",
			testRefreshEnv + "=" + testRefreshPeriod.String(),
			"SUBSYSTEM_STORE_TRUSTED_PROXIES=192.0.2.0/24",
		}
		if journal != nil {
			child.Env = append(child.Env, codesrc.EnvJournal+"="+*journal)
		}
		return child
	}

	for _, tc := range []struct{ name, journal string }{
		{"a dot-directory under the root", filepath.Join(root, ".sources", "sources.jsonl")},
		{"a symlinked directory that resolves into the root", filepath.Join(viaLink, "sources.jsonl")},
		{"the store root itself", root},
		{"a value that reduces to nothing", "   "},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			journal := tc.journal
			child := command(ctx, freePort(t), &journal)
			out, _ := child.CombinedOutput()
			if ctx.Err() != nil {
				t.Fatalf("the child did not exit within 20s — it CAME UP with the sources journal at %q, "+
					"which is the defect:\n%s", tc.journal, out)
			}
			if code := child.ProcessState.ExitCode(); code != 78 {
				t.Fatalf("must exit 78 (EX_CONFIG), got %d\n%s", code, out)
			}
			if !strings.Contains(string(out), "Refusing to start") || !strings.Contains(string(out), "CAIRN_SOURCE_JOURNAL") ||
				strings.Contains(string(out), "arc journal") {
				t.Fatalf("the refusal must name $CAIRN_SOURCE_JOURNAL and say it refuses, never the arc journal:\n%s", out)
			}
		})
	}

	// The sources journal resolving to the ARC journal: each reader would read the other's records
	// as damaged lines. Refused at startup, through the variable's one reader (`codesrc.FromEnv`).
	t.Run("the same file as the arc journal", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		shared := filepath.Join(outside, "journal.jsonl")
		child := command(ctx, freePort(t), &shared)
		child.Args = append(child.Args, "-arc-journal", shared)
		out, _ := child.CombinedOutput()
		if ctx.Err() != nil {
			t.Fatalf("the child CAME UP with one file as both journals:\n%s", out)
		}
		if code := child.ProcessState.ExitCode(); code != 78 || !strings.Contains(string(out), "is the ARC journal") {
			t.Fatalf("must exit 78 naming the arc journal, got %d:\n%s", code, out)
		}
	})

	serve := func(t *testing.T, journal *string) string {
		t.Helper()
		port := freePort(t)
		child := command(context.Background(), port, journal)
		log := &lockedBuffer{}
		child.Stdout, child.Stderr = log, log
		if err := child.Start(); err != nil {
			t.Fatal(err)
		}
		exited := make(chan struct{})
		go func() { _, _ = child.Process.Wait(); close(exited) }()
		defer func() { _ = child.Process.Kill(); <-exited }()
		url := "http://127.0.0.1:" + strconv.Itoa(port)
		deadline := time.Now().Add(20 * time.Second)
		for time.Now().Before(deadline) {
			select {
			case <-exited:
				t.Fatalf("control: the child EXITED instead of serving:\n%s", log.String())
			default:
			}
			req, _ := http.NewRequest("GET", url+"/api/v1/sources/alpha-notes", nil)
			req.Header.Set("Authorization", "Bearer "+testToken)
			resp, err := http.DefaultClient.Do(req)
			if err == nil {
				body, _ := io.ReadAll(resp.Body)
				resp.Body.Close()
				return resp.Header.Get("X-Store-Status") + "\n" + string(body)
			}
			time.Sleep(20 * time.Millisecond)
		}
		t.Fatalf("control: the child never served:\n%s", log.String())
		return ""
	}

	t.Run("control: outside the root, served", func(t *testing.T) {
		journal := filepath.Join(outside, "sources.jsonl")
		src := "git:github.com/example-org/example-repo@main"
		if _, err := (codesrc.Journal{Path: journal}).Set("alpha-notes", []string{src}, codesrc.RevisionNone,
			"scope-admin", time.Date(2000, 1, 3, 0, 0, 0, 0, time.UTC), nil); err != nil {
			t.Fatal(err)
		}
		got := serve(t, &journal)
		if !strings.HasPrefix(got, "sources-declared\n") || !strings.Contains(got, "\nsource="+src+"\n") {
			t.Fatalf("the binary must hand $CAIRN_SOURCE_JOURNAL to the server and serve its declaration:\n%s", got)
		}
	})
	t.Run("control: unset, the off state", func(t *testing.T) {
		if got := serve(t, nil); !strings.HasPrefix(got, "sources-unconfigured\n") {
			t.Fatalf("with the variable unset the binary must answer sources-unconfigured:\n%s", got)
		}
	})
}
