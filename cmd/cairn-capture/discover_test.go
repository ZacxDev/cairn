package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestMain doubles as a FAKE opencode: when CAIRN_CAPTURE_FAKE_OPENCODE names a directory, the
// test binary answers `debug scrap` from `scrap.json` there (exit 65 when it is absent), `session
// list` with ONE root named after the directory it runs IN (`ses_<base name>`), and fails every
// `export` — so a dry run's `opencode ses_<name> refused: the export failed` lines name exactly
// the project directories the pass enumerated.
func TestMain(m *testing.M) {
	if dir := os.Getenv("CAIRN_CAPTURE_FAKE_OPENCODE"); dir != "" {
		args := os.Args[1:]
		switch {
		case len(args) == 2 && args[0] == "debug" && args[1] == "scrap":
			data, err := os.ReadFile(filepath.Join(dir, "scrap.json"))
			if err != nil {
				os.Exit(65)
			}
			os.Stdout.Write(data)
		case len(args) >= 2 && args[0] == "session" && args[1] == "list":
			wd, _ := os.Getwd()
			row, _ := json.Marshal([]map[string]any{{"id": "ses_" + filepath.Base(wd), "updated": 946684800000}})
			os.Stdout.Write(row)
		default:
			os.Exit(64)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// discoverWorld is two project directories that exist (alpha, beta), one scrap names that does
// not (gone), and the fake opencode answering scrap with `scrap` (nil: no scrap.json, exit 65).
func discoverWorld(t *testing.T, scrap []byte) (base string) {
	t.Helper()
	base = t.TempDir()
	for _, d := range []string{"alpha", "beta"} {
		if err := os.Mkdir(filepath.Join(base, d), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	fake := t.TempDir()
	if scrap != nil {
		if err := os.WriteFile(filepath.Join(fake, "scrap.json"), scrap, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("CAIRN_CAPTURE_FAKE_OPENCODE", fake)
	return base
}

func scrapOf(worktrees ...string) []byte {
	rows := make([]map[string]any, len(worktrees))
	for i, w := range worktrees {
		rows[i] = map[string]any{"id": "p" + string(rune('0'+i)), "worktree": w, "vcs": "git",
			"time": map[string]any{"created": 946684800000, "updated": 946684800000}, "sandboxes": []any{}}
	}
	raw, _ := json.Marshal(rows)
	return raw
}

func opencodeLines(out string) []string {
	var got []string
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(l, "opencode ") {
			got = append(got, strings.Fields(l)[1])
		}
	}
	return got
}

// TestADryRunDiscoversOpencodesProjects: `-opencode-discover` unions scrap's projects with the
// explicit `-opencode-project` (one per cleaned path), skips the worktree that is gone, and says
// the counts on ONE stderr line.
func TestADryRunDiscoversOpencodesProjects(t *testing.T) {
	base := discoverWorld(t, nil)
	alpha, beta := filepath.Join(base, "alpha"), filepath.Join(base, "beta")
	if err := os.WriteFile(filepath.Join(os.Getenv("CAIRN_CAPTURE_FAKE_OPENCODE"), "scrap.json"),
		scrapOf(alpha, filepath.Join(base, "gone")), 0o600); err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	var out, errb bytes.Buffer
	code := run([]string{"--dry-run", "-claude-root", "", "-opencode-bin", os.Args[0], "-opencode-discover",
		"-opencode-project", alpha + "/", "-opencode-project", beta}, &out, &errb, envOf(map[string]string{"HOME": home}))
	got := strings.Join(opencodeLines(out.String()), ",")
	if code != exitOK || got != "ses_alpha,ses_beta" {
		t.Fatalf("exit %d, roots %q; want ses_alpha,ses_beta once each\nstdout:\n%s\nstderr:\n%s", code, got, out.String(), errb.String())
	}
	const want = "cairn-capture: opencode discovery: projects=2 skipped-missing-worktree=1 enumerated=2\n"
	if strings.Count(errb.String(), "opencode discovery") != 1 || !strings.Contains(errb.String(), want) {
		t.Fatalf("stderr does not carry the one discovery line %q:\n%s", want, errb.String())
	}
	// CONTROL: without the flag, scrap is never asked and only the explicit directories are read.
	out.Reset()
	errb.Reset()
	code = run([]string{"--dry-run", "-claude-root", "", "-opencode-bin", os.Args[0], "-opencode-project", beta},
		&out, &errb, envOf(map[string]string{"HOME": home}))
	if got := strings.Join(opencodeLines(out.String()), ","); code != exitOK || got != "ses_beta" || strings.Contains(errb.String(), "discovery") {
		t.Fatalf("without -opencode-discover: exit %d, roots %q\n%s", code, got, errb.String())
	}
}

// TestAFailedDiscoveryIsAnErrorNeverNoProjects: an unparseable scrap answer, a non-zero exit and a
// missing binary each end the pass with exit 1 — in --dry-run AND --run — and nothing of opencode
// is read; never a pass that quietly enumerated nothing.
func TestAFailedDiscoveryIsAnErrorNeverNoProjects(t *testing.T) {
	cases := []struct {
		name  string
		scrap []byte
		bin   string
		says  string
	}{
		{"unparseable", []byte(`{"worktree":"/"}`), "", "did not answer a JSON list of projects"},
		{"non-zero exit", nil, "", "exit status 65"},
		{"missing binary", []byte(`[]`), "/nonexistent/opencode-for-a-test", "opencode project discovery failed"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			base := discoverWorld(t, tc.scrap)
			bin := tc.bin
			if bin == "" {
				bin = os.Args[0]
			}
			var out, errb bytes.Buffer
			code := run([]string{"--dry-run", "-claude-root", "", "-opencode-bin", bin, "-opencode-discover",
				"-opencode-project", filepath.Join(base, "alpha")}, &out, &errb, envOf(map[string]string{"HOME": t.TempDir()}))
			if code != exitError || !strings.Contains(errb.String(), tc.says) || len(opencodeLines(out.String())) != 0 {
				t.Fatalf("--dry-run: exit %d, stdout %q, stderr %q; want exit %d naming %q", code, out.String(), errb.String(), exitError, tc.says)
			}
			p := newRunPod(t, true)
			_, tokenFile, env := runWorld(t, runToken)
			errb.Reset()
			code = run([]string{"--run", "-host", "host-a", "-worker", p.url, "-capture-token-file", tokenFile,
				"-opencode-bin", bin, "-opencode-discover"}, &bytes.Buffer{}, &errb, env)
			if code != exitError || !strings.Contains(errb.String(), tc.says) || p.requests.Load() != 0 {
				t.Fatalf("--run: exit %d, %d request(s), stderr %q; want exit %d naming %q and nothing sent",
					code, p.requests.Load(), errb.String(), exitError, tc.says)
			}
		})
	}
}

// TestAnEmptyProjectListIsZeroProjectsNotAnError: `[]` is opencode knowing no project — exit 0,
// the counts line saying so.
func TestAnEmptyProjectListIsZeroProjectsNotAnError(t *testing.T) {
	discoverWorld(t, []byte("[]\n"))
	var out, errb bytes.Buffer
	code := run([]string{"--dry-run", "-claude-root", "", "-opencode-bin", os.Args[0], "-opencode-discover"},
		&out, &errb, envOf(map[string]string{"HOME": t.TempDir()}))
	if code != exitOK || !strings.Contains(errb.String(), "projects=0 skipped-missing-worktree=0 enumerated=0") {
		t.Fatalf("exit %d, stderr %q", code, errb.String())
	}
}
