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

// TestAnEmptyListAnswerIsZeroRootsNotAnError is the REGRESSION test for opencode's empty answer.
//
// 🔴 MEASURED on opencode 1.18.29: `session list` for a project with no sessions writes NOTHING —
// zero bytes, exit 0, `--format json` or not — rather than `[]`. Parsing that as JSON refused the
// project on every `--run` (a recurring refusal) and failed `--dry-run` outright, stopping it before
// every project after it. Through the REAL subprocess path; a truncated list is still refused.
func TestAnEmptyListAnswerIsZeroRootsNotAnError(t *testing.T) {
	for _, tc := range []struct {
		answer string
		ok     bool
	}{{"", true}, {" \n", true}, {"[]", true}, {"[", false}, {" [", false}} {
		stub := t.TempDir()
		if err := os.WriteFile(filepath.Join(stub, "list.json"), []byte(tc.answer), 0o600); err != nil {
			t.Fatal(err)
		}
		t.Setenv("CAPTURE_OPENCODE_STUB", stub)
		a := &Agent{OpencodeDirs: []string{t.TempDir()}, Runner: ExecRunner{Bin: os.Args[0], TempDir: t.TempDir()},
			State: &State{Schema: 1, Sessions: map[string]*SessionState{}}, Log: &bytes.Buffer{}, Sink: newMemSink()}
		sum, err := a.Run()
		if wantRefused := map[bool]int{true: 0, false: 1}[tc.ok]; err != nil || sum.Sessions != 0 || sum.Refused != wantRefused {
			t.Errorf("`session list` answering %q: %+v (%v); want ok=%v and no session", tc.answer, sum, err, tc.ok)
		}
		if n, err := dryRoots(t, &Agent{OpencodeDirs: a.OpencodeDirs, Runner: a.Runner}); (err == nil) != tc.ok || n != 0 {
			t.Errorf("--dry-run over %q: %d roots (%v); want ok=%v", tc.answer, n, err, tc.ok)
		}
	}
}

// TestDiscoveryUnionsDedupesAndCountsTheMissing:scrap's projects UNION the explicit ones, one per
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
	d, err := DiscoverOpencodeDirs(&fakeRunner{projects: scrap}, []string{alpha, beta + "/./", explicitGone})
	want := []string{"/", alpha, beta, explicitGone}
	if err != nil || d.Projects != 5 || d.Skipped != 2 || d.ViaSandbox != 0 || strings.Join(d.Dirs, "|") != strings.Join(want, "|") {
		t.Fatalf("%+v (%v); want dirs %v, 5 projects, 2 skipped, 0 via a sandbox", d, err, want)
	}
}

// TestAGoneWorktreeIsReachedThroughItsSandbox: `session list` answers a directory's PROJECT
// (measured: all of a project's sessions from either sandbox while its worktree was gone), so a
// project whose worktree is gone is enumerated from its FIRST EXISTING sandbox and counted
// `ViaSandbox` — and only a project with no existing sandbox either is `Skipped`.
func TestAGoneWorktreeIsReachedThroughItsSandbox(t *testing.T) {
	base := t.TempDir()
	sb2, sb3, live := filepath.Join(base, "sb2"), filepath.Join(base, "sb3"), filepath.Join(base, "live")
	for _, d := range []string{sb2, sb3, live} {
		if err := os.Mkdir(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	scrap, _ := json.Marshal([]map[string]any{
		// worktree gone; first sandbox gone, second and third exist → the second, only.
		{"id": "p1", "worktree": filepath.Join(base, "gone1"), "sandboxes": []string{filepath.Join(base, "sb1-gone"), sb2, sb3}},
		// worktree gone, every sandbox gone → skipped.
		{"id": "p2", "worktree": filepath.Join(base, "gone2"), "sandboxes": []string{filepath.Join(base, "sb4-gone")}},
		// worktree present → the worktree, never its sandbox.
		{"id": "p3", "worktree": live, "sandboxes": []string{sb3}},
	})
	d, err := DiscoverOpencodeDirs(&fakeRunner{projects: scrap}, nil)
	want := []string{live, sb2}
	if err != nil || d.Projects != 3 || d.ViaSandbox != 1 || d.Skipped != 1 || strings.Join(d.Dirs, "|") != strings.Join(want, "|") {
		t.Fatalf("%+v (%v); want dirs %v, 3 projects, 1 via a sandbox, 1 skipped", d, err, want)
	}
}

// TestDiscoveryNeverDegradesToNoProjects: a runner that fails, and every answer that is not a list
// of projects with absolute canonical paths or legacy worktrees (and absolute string sandboxes),
// is an ERROR — while `[]` is zero projects and no error.
func TestDiscoveryNeverDegradesToNoProjects(t *testing.T) {
	if _, err := DiscoverOpencodeDirs(&fakeRunner{projErr: errors.New("exit status 1")}, nil); err == nil {
		t.Fatal("a failed project query was not an error")
	}
	for _, raw := range []string{"", "null", "{}", `"x"`, "[", `[{"id":"x"}]`, `[{"worktree":"relative/dir"}]`,
		`[{"worktree":7}]`, `[{"worktree":"/ok"}, {"worktree":null}]`, `[{"worktree":"/ok","sandboxes":["rel"]}]`,
		`[{"canonical":null}]`, `[{"canonical":"relative/dir"}]`, `[{"canonical":"/ok"}, {"canonical":""}]`,
		`[{"worktree":"/ok","sandboxes":[{"dir":"/x"}]}]`, `[{"worktree":"/ok","sandboxes":"/x"}]`} {
		if _, err := DiscoverOpencodeDirs(&fakeRunner{projects: []byte(raw)}, nil); !errors.Is(err, ErrProjectsUnreadable) {
			t.Errorf("project query answering %q: %v; want ErrProjectsUnreadable", raw, err)
		}
	}
	d, err := DiscoverOpencodeDirs(&fakeRunner{projects: []byte(" [] \n")}, []string{"/work/alpha"})
	if err != nil || d.Projects != 0 || d.Skipped != 0 || len(d.Dirs) != 1 {
		t.Fatalf("an empty project list: %+v %v; want the explicit directory alone and no error", d, err)
	}
}

// TestDiscoveryUsesV2ProjectAPIAndFallsBackToLegacyScrap exercises both discovery protocols
// through ExecRunner's real subprocess path. The v2 API's canonical path is authoritative; a
// failing v2 API command falls back to the v1 debug command.
func TestDiscoveryUsesV2ProjectAPIAndFallsBackToLegacyScrap(t *testing.T) {
	t.Run("v2 canonical", func(t *testing.T) {
		stub := t.TempDir()
		project := filepath.Join(stub, "project")
		if err := os.Mkdir(project, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(stub, "v2-projects.json"),
			[]byte(fmt.Sprintf(`[{"id":"synthetic","canonical":%q,"sandboxes":[]}]`, project)), 0o600); err != nil {
			t.Fatal(err)
		}
		t.Setenv("CAPTURE_OPENCODE_STUB", stub)
		d, err := DiscoverOpencodeDirs(ExecRunner{Bin: os.Args[0], TempDir: t.TempDir()}, nil)
		if err != nil || d.Projects != 1 || len(d.Dirs) != 1 || d.Dirs[0] != project {
			t.Fatalf("v2 discovery: %+v (%v); want canonical project %q", d, err, project)
		}
		called, err := os.ReadFile(filepath.Join(stub, "v2-called"))
		if err != nil || string(called) != "api --standalone GET /api/project" {
			t.Fatalf("v2 API command was not called as specified: %q (%v)", called, err)
		}
		if _, err := os.Stat(filepath.Join(stub, "legacy-called")); !os.IsNotExist(err) {
			t.Fatalf("legacy fallback ran despite a successful v2 answer: %v", err)
		}
	})

	t.Run("legacy fallback when v2 API fails", func(t *testing.T) {
		stub := t.TempDir()
		project := filepath.Join(stub, "legacy-project")
		if err := os.Mkdir(project, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(stub, "scrap.json"),
			[]byte(fmt.Sprintf(`[{"id":"synthetic","worktree":%q,"sandboxes":[]}]`, project)), 0o600); err != nil {
			t.Fatal(err)
		}
		t.Setenv("CAPTURE_OPENCODE_STUB", stub)
		d, err := DiscoverOpencodeDirs(ExecRunner{Bin: os.Args[0], TempDir: t.TempDir()}, nil)
		if err != nil || d.Projects != 1 || len(d.Dirs) != 1 || d.Dirs[0] != project {
			t.Fatalf("legacy discovery: %+v (%v); want worktree project %q", d, err, project)
		}
		if _, err := os.Stat(filepath.Join(stub, "v2-called")); err != nil {
			t.Fatalf("v2 API was not attempted before fallback: %v", err)
		}
		if _, err := os.Stat(filepath.Join(stub, "legacy-called")); err != nil {
			t.Fatalf("legacy fallback was not called: %v", err)
		}
	})

	t.Run("malformed successful v2 answer does not fall back", func(t *testing.T) {
		stub := t.TempDir()
		project := filepath.Join(stub, "legacy-project")
		if err := os.Mkdir(project, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(stub, "v2-projects.json"),
			[]byte(`[{"id":"synthetic","canonical":"relative/project","sandboxes":[]}]`), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(stub, "scrap.json"),
			[]byte(fmt.Sprintf(`[{"id":"synthetic","worktree":%q,"sandboxes":[]}]`, project)), 0o600); err != nil {
			t.Fatal(err)
		}
		t.Setenv("CAPTURE_OPENCODE_STUB", stub)
		d, err := DiscoverOpencodeDirs(ExecRunner{Bin: os.Args[0], TempDir: t.TempDir()}, nil)
		if _, statErr := os.Stat(filepath.Join(stub, "legacy-called")); statErr == nil {
			t.Fatal("legacy fallback ran after the v2 API succeeded with an invalid project shape")
		} else if !os.IsNotExist(statErr) {
			t.Fatalf("checking the legacy-call marker: %v", statErr)
		}
		if !errors.Is(err, ErrProjectsUnreadable) || d.Projects != 0 || len(d.Dirs) != 0 {
			t.Fatalf("malformed v2 answer: %+v (%v); want a fail-closed project-shape error", d, err)
		}
	})
}

// TestDiscoveryRunsTheRealSubprocess: the legacy subprocess path remains available — stdout to a
// file, the stub answering it — and a binary that does not exist is an error.
func TestDiscoveryRunsTheRealSubprocess(t *testing.T) {
	stub := t.TempDir()
	if err := os.WriteFile(filepath.Join(stub, "scrap.json"), []byte(`[{"id":"global","worktree":"/"}]`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CAPTURE_OPENCODE_STUB", stub)
	d, err := DiscoverOpencodeDirs(ExecRunner{Bin: os.Args[0], TempDir: t.TempDir()}, nil)
	if err != nil || len(d.Dirs) != 1 || d.Dirs[0] != "/" {
		t.Fatalf("through the subprocess: %+v (%v)", d, err)
	}
	if _, err := DiscoverOpencodeDirs(ExecRunner{Bin: filepath.Join(stub, "no-such-opencode"), TempDir: t.TempDir()}, nil); err == nil {
		t.Fatal("a missing opencode binary was not an error")
	}
	// A non-zero exit: the stub exits 65 when it has no answer to give.
	t.Setenv("CAPTURE_OPENCODE_STUB", t.TempDir())
	if _, err := DiscoverOpencodeDirs(ExecRunner{Bin: os.Args[0], TempDir: t.TempDir()}, nil); err == nil ||
		!strings.Contains(err.Error(), "exit status 65") {
		t.Fatalf("a `debug scrap` that exits non-zero: %v; want its exit status as an error", err)
	}
}
