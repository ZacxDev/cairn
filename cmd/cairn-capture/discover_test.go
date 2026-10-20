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
// test binary answers the v2 project API from `projects.json` there (exit 65 when it is absent),
// the legacy `debug scrap` from `scrap.json`, `session list` with ONE root named after the directory
// it runs IN (`ses_<base name>`), and fails every
// `export` — so a dry run's `opencode ses_<name> refused: the export failed` lines name exactly
// the project directories the pass enumerated.
func TestMain(m *testing.M) {
	if dir := os.Getenv("CAIRN_CAPTURE_FAKE_OPENCODE"); dir != "" {
		args := os.Args[1:]
		switch {
		case len(args) == 4 && args[0] == "api" && args[1] == "--standalone" && args[2] == "GET" && args[3] == "/api/project":
			data, err := os.ReadFile(filepath.Join(dir, "projects.json"))
			if err != nil {
				os.Exit(65)
			}
			os.Stdout.Write(data)
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

// discoverWorld is two project directories that exist (alpha, beta), one project names that does
// not (gone), and a fake opencode with no project list when `projects` is nil.
func discoverWorld(t *testing.T, projects []byte) (base string) {
	t.Helper()
	base = t.TempDir()
	for _, d := range []string{"alpha", "beta"} {
		if err := os.Mkdir(filepath.Join(base, d), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	fake := t.TempDir()
	if projects != nil {
		if err := os.WriteFile(filepath.Join(fake, "projects.json"), projects, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("CAIRN_CAPTURE_FAKE_OPENCODE", fake)
	return base
}

func projectsOf(paths ...string) []byte {
	rows := make([]map[string]any, len(paths))
	for i, path := range paths {
		rows[i] = map[string]any{"id": "p" + string(rune('0'+i)), "canonical": path, "vcs": "git",
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

// TestADryRunDiscoversOpencodesProjects: `-opencode-discover` unions the v2 project API's results with the
// explicit `-opencode-project` (one per cleaned path), skips the worktree that is gone, and says
// the counts on ONE stderr line.
func TestADryRunDiscoversOpencodesProjects(t *testing.T) {
	base := discoverWorld(t, nil)
	alpha, beta := filepath.Join(base, "alpha"), filepath.Join(base, "beta")
	if err := os.WriteFile(filepath.Join(os.Getenv("CAIRN_CAPTURE_FAKE_OPENCODE"), "projects.json"),
		projectsOf(alpha, filepath.Join(base, "gone")), 0o600); err != nil {
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
	const want = "cairn-capture: opencode discovery: projects=2 via-sandbox=0 skipped-missing-worktree=1 enumerated=2\n"
	if strings.Count(errb.String(), "opencode discovery") != 1 || !strings.Contains(errb.String(), want) {
		t.Fatalf("stderr does not carry the one discovery line %q:\n%s", want, errb.String())
	}
	// CONTROL: without the flag, project discovery is never asked and only explicit directories are read.
	out.Reset()
	errb.Reset()
	code = run([]string{"--dry-run", "-claude-root", "", "-opencode-bin", os.Args[0], "-opencode-project", beta},
		&out, &errb, envOf(map[string]string{"HOME": home}))
	if got := strings.Join(opencodeLines(out.String()), ","); code != exitOK || got != "ses_beta" || strings.Contains(errb.String(), "discovery") {
		t.Fatalf("without -opencode-discover: exit %d, roots %q\n%s", code, got, errb.String())
	}
}

// TestAGoneWorktreeIsEnumeratedFromItsSandbox: a project whose worktree is gone but whose sandbox
// exists is listed from the sandbox, and the counts line says so (`via-sandbox=1`).
func TestAGoneWorktreeIsEnumeratedFromItsSandbox(t *testing.T) {
	base := discoverWorld(t, nil)
	projects, _ := json.Marshal([]map[string]any{{"id": "p0", "canonical": filepath.Join(base, "gone"),
		"sandboxes": []string{filepath.Join(base, "alpha")}}})
	if err := os.WriteFile(filepath.Join(os.Getenv("CAIRN_CAPTURE_FAKE_OPENCODE"), "projects.json"), projects, 0o600); err != nil {
		t.Fatal(err)
	}
	var out, errb bytes.Buffer
	code := run([]string{"--dry-run", "-claude-root", "", "-opencode-bin", os.Args[0], "-opencode-discover"},
		&out, &errb, envOf(map[string]string{"HOME": t.TempDir()}))
	const want = "opencode discovery: projects=1 via-sandbox=1 skipped-missing-worktree=0 enumerated=1\n"
	if got := strings.Join(opencodeLines(out.String()), ","); code != exitOK || got != "ses_alpha" || !strings.Contains(errb.String(), want) {
		t.Fatalf("exit %d, roots %q, stderr %q; want ses_alpha and %q", code, got, errb.String(), want)
	}
}

// TestAFailedDiscoveryRefusesOnlyTheOpencodeHalf (operator decision "opencode half only"): an
// unparseable scrap answer, a non-zero exit and a missing binary each REFUSE the discovered
// projects — logged — while Claude Code and the explicit `-opencode-project` directory are still
// captured, and the pass then exits 3 (a refusal that recurs) — in --dry-run AND --run. Never a
// pass that quietly enumerated nothing, and never one that dropped Claude Code over opencode.
func TestAFailedDiscoveryRefusesOnlyTheOpencodeHalf(t *testing.T) {
	cases := []struct {
		name     string
		projects []byte
		bin      string
		says     string
	}{
		{"unparseable", []byte(`{"canonical":"/"}`), "", "did not answer a JSON list of projects"},
		{"non-zero exit", nil, "", "exit status 65"},
		{"missing binary", []byte(`[]`), "/nonexistent/opencode-for-a-test", "opencode project discovery failed"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			base := discoverWorld(t, tc.projects)
			bin := tc.bin
			if bin == "" {
				bin = os.Args[0]
			}
			p := newRunPod(t, true)
			home, tokenFile, env := runWorld(t, runToken)
			var out, errb bytes.Buffer
			args := []string{"--dry-run", "-claude-root", filepath.Join(home, ".claude", "projects"), "-opencode-bin", bin,
				"-opencode-discover"}
			if tc.bin == "" {
				// With a binary that runs, the explicit directory is still listed. (With none, its own
				// `session list` fails too, which a dry run reports as exit 1 — unchanged by this.)
				args = append(args, "-opencode-project", filepath.Join(base, "alpha"))
			}
			code := run(args, &out, &errb, env)
			refused := "cairn-capture: refused opencode discovery: "
			if code != exitRefused || !strings.Contains(errb.String(), refused) || !strings.Contains(errb.String(), tc.says) ||
				!strings.Contains(out.String(), "claude s-0001 ") {
				t.Fatalf("--dry-run: exit %d, stdout %q, stderr %q; want exit %d, Claude Code still read, the refusal naming %q",
					code, out.String(), errb.String(), exitRefused, tc.says)
			}
			if tc.bin == "" && strings.Join(opencodeLines(out.String()), ",") != "ses_alpha" {
				t.Fatalf("--dry-run: the explicit -opencode-project was not read:\n%s", out.String())
			}
			errb.Reset()
			code = run([]string{"--run", "-host", "host-a", "-worker", p.url, "-capture-token-file", tokenFile,
				"-opencode-bin", bin, "-opencode-discover"}, &bytes.Buffer{}, &errb, env)
			recs, _ := p.arc.ReadStream("s-0001", "main")
			last := strings.TrimSpace(errb.String())
			last = last[strings.LastIndex(last, "\n")+1:]
			if code != exitRefused || !strings.Contains(errb.String(), tc.says) || len(recs) != 5 ||
				!strings.Contains(last, "opencode discovery was refused") {
				t.Fatalf("--run: exit %d, %d Claude Code record(s) stored, stderr %q; want exit %d, all 5 stored, the "+
					"refusal naming %q and the discovery line last", code, len(recs), errb.String(), exitRefused, tc.says)
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
	if code != exitOK || !strings.Contains(errb.String(), "projects=0 via-sandbox=0 skipped-missing-worktree=0 enumerated=0") {
		t.Fatalf("exit %d, stderr %q", code, errb.String())
	}
}
