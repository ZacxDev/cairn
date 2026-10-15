package capture

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// stubList writes a `session list` answer of n synthetic root sessions into a stub directory and
// points the test binary's opencode stub at it. No export exists for any of them, so each root a
// dry run sees prints exactly one `opencode <id> refused: the export failed` line: the line count
// is the number of roots the LIST yielded.
func stubList(t *testing.T, n int) (stub string) {
	t.Helper()
	stub = t.TempDir()
	rows := make([]map[string]any, n)
	for i := range rows {
		rows[i] = map[string]any{"id": fmt.Sprintf("ses_synthetic%04d", i), "title": "t", "directory": "/work/alpha",
			"projectId": "p", "created": 946684800000 + int64(i), "updated": 946684800000 + int64(i)}
	}
	raw, err := json.Marshal(rows)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stub, "list.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CAPTURE_OPENCODE_STUB", stub)
	return stub
}

func dryRoots(t *testing.T, a *Agent) (int, error) {
	t.Helper()
	var out bytes.Buffer
	err := a.DryRun(&out)
	n := 0
	for _, l := range strings.Split(out.String(), "\n") {
		if strings.HasPrefix(l, "opencode ") {
			n++
		}
	}
	return n, err
}

// TestEveryRootSessionIsListedNotTheFirstHundred is the REGRESSION test for the session-list cap.
//
// 🔴 MEASURED on opencode 1.18.29: `session list --format json` answers at most 100 rows unless
// `-n` is given, with exit 0 — so a project with more root sessions than that lost every one past
// the 100 most recent, silently, on every run. The stub models that default (TestMain); 150 roots
// sit between the default and the runner's cap, so only a runner that ASKS for every row sees them
// all.
func TestEveryRootSessionIsListedNotTheFirstHundred(t *testing.T) {
	stubList(t, 150)
	a := &Agent{OpencodeDirs: []string{t.TempDir()}, Runner: ExecRunner{Bin: os.Args[0], TempDir: t.TempDir()}}
	n, err := dryRoots(t, a)
	if err != nil || n != 150 {
		t.Fatalf("the dry run saw %d root sessions (%v); the project lists 150", n, err)
	}
}

// TestAListAtItsCapIsRefusedNotRead: a `session list` answer with as many rows as the `-n` it was
// asked with may be cut short, so the project is refused — logged, counted, no root captured — and
// one row fewer is read whole. The cap is lowered so the boundary is reachable; the rows come
// through the REAL subprocess path and the stub's `-n` handling.
func TestAListAtItsCapIsRefusedNotRead(t *testing.T) {
	stubList(t, 5)
	for _, tc := range []struct {
		cap, sessions, refused int
	}{{5, 0, 1}, {4, 0, 1}, {6, 5, 0}} {
		var log bytes.Buffer
		a := &Agent{OpencodeDirs: []string{t.TempDir()}, Runner: ExecRunner{Bin: os.Args[0], TempDir: t.TempDir()},
			ListCap: tc.cap, State: &State{Schema: 1, Sessions: map[string]*SessionState{}}, Log: &log, Sink: newMemSink()}
		sum, err := a.Run()
		if err != nil || sum.Sessions != tc.sessions || sum.Refused < tc.refused ||
			(tc.refused > 0) != strings.Contains(log.String(), ErrSessionListAtCap.Error()) {
			t.Fatalf("cap %d over 5 rows: %+v (%v); want %d sessions, the at-cap refusal logged=%v\n%s",
				tc.cap, sum, err, tc.sessions, tc.refused > 0, log.String())
		}
		if _, err := dryRoots(t, &Agent{OpencodeDirs: a.OpencodeDirs, Runner: a.Runner, ListCap: tc.cap}); (tc.refused > 0) != errors.Is(err, ErrSessionListAtCap) {
			t.Fatalf("cap %d: --dry-run answered %v", tc.cap, err)
		}
	}
}

// TestDiscoveryUnionsDedupesAndCountsTheMissing: scrap's projects UNION the explicit ones, one per
// cleaned absolute path; the global project's `/` is a project; a worktree that is gone is
// skipped and COUNTED, never silently dropped; an explicit directory is kept whether or not it
// exists.
func TestDiscoveryUnionsDedupesAndCountsTheMissing(t *testing.T) {
	base := t.TempDir()
	alpha, beta := filepath.Join(base, "alpha"), filepath.Join(base, "beta")
	for _, d := range []string{alpha, beta} {
		if err := os.Mkdir(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	file := filepath.Join(base, "afile")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	scrap, _ := json.Marshal([]map[string]any{
		{"id": "global", "worktree": "/", "vcs": "git", "time": map[string]any{"created": 1, "updated": 2}, "sandboxes": []any{}},
		{"id": "a", "worktree": alpha + "/"},
		{"id": "b", "worktree": beta},
		{"id": "gone", "worktree": filepath.Join(base, "gone")},
		{"id": "under-a-file", "worktree": filepath.Join(file, "x")},
	})
	explicitGone := filepath.Join(base, "explicit-gone")
	dirs, discovered, skipped, err := DiscoverOpencodeDirs(&fakeRunner{projects: scrap},
		[]string{alpha, beta + "/./", explicitGone})
	want := []string{"/", alpha, beta, explicitGone}
	if err != nil || discovered != 5 || skipped != 2 || strings.Join(dirs, "|") != strings.Join(want, "|") {
		t.Fatalf("dirs %v discovered %d skipped %d (%v); want %v, 5, 2", dirs, discovered, skipped, err, want)
	}
}

// TestDiscoveryNeverDegradesToNoProjects: a runner that fails, and every answer that is not a list
// of projects with absolute worktrees, is an ERROR — while `[]` is zero projects and no error.
func TestDiscoveryNeverDegradesToNoProjects(t *testing.T) {
	if _, _, _, err := DiscoverOpencodeDirs(&fakeRunner{projErr: errors.New("exit status 1")}, nil); err == nil {
		t.Fatal("a failed `debug scrap` was not an error")
	}
	for _, raw := range []string{"", "null", "{}", `"x"`, "[", `[{"id":"x"}]`, `[{"worktree":"relative/dir"}]`,
		`[{"worktree":7}]`, `[{"worktree":"/ok"}, {"worktree":null}]`} {
		if _, _, _, err := DiscoverOpencodeDirs(&fakeRunner{projects: []byte(raw)}, nil); !errors.Is(err, ErrProjectsUnreadable) {
			t.Errorf("scrap answering %q: %v; want ErrProjectsUnreadable", raw, err)
		}
	}
	dirs, n, skipped, err := DiscoverOpencodeDirs(&fakeRunner{projects: []byte(" [] \n")}, []string{"/work/alpha"})
	if err != nil || n != 0 || skipped != 0 || len(dirs) != 1 {
		t.Fatalf("an empty project list: %v %d %d %v; want the explicit directory alone and no error", dirs, n, skipped, err)
	}
}

// TestDiscoveryRunsTheRealSubprocess: `debug scrap` through ExecRunner — stdout to a file, the
// stub answering it — and a binary that does not exist is an error.
func TestDiscoveryRunsTheRealSubprocess(t *testing.T) {
	stub := t.TempDir()
	if err := os.WriteFile(filepath.Join(stub, "scrap.json"), []byte(`[{"id":"global","worktree":"/"}]`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CAPTURE_OPENCODE_STUB", stub)
	dirs, _, _, err := DiscoverOpencodeDirs(ExecRunner{Bin: os.Args[0], TempDir: t.TempDir()}, nil)
	if err != nil || len(dirs) != 1 || dirs[0] != "/" {
		t.Fatalf("through the subprocess: %v (%v)", dirs, err)
	}
	if _, _, _, err := DiscoverOpencodeDirs(ExecRunner{Bin: filepath.Join(stub, "no-such-opencode"), TempDir: t.TempDir()}, nil); err == nil {
		t.Fatal("a missing opencode binary was not an error")
	}
	// A non-zero exit: the stub exits 65 when it has no answer to give.
	t.Setenv("CAPTURE_OPENCODE_STUB", t.TempDir())
	if _, _, _, err := DiscoverOpencodeDirs(ExecRunner{Bin: os.Args[0], TempDir: t.TempDir()}, nil); err == nil ||
		!strings.Contains(err.Error(), "exit status 65") {
		t.Fatalf("a `debug scrap` that exits non-zero: %v; want its exit status as an error", err)
	}
}
