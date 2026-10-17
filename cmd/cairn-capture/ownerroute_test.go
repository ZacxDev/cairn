package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestRouteOriginOwnerUsageRefusals: `-route-origin-owner OWNER=ALIAS` names a CONFIGURED instance
// — and under --run one with a `-worker` — or the binary refuses at exit 2, in both modes that read
// it. A synthetic second instance, `client`, is configured beside the default one.
func TestRouteOriginOwnerUsageRefusals(t *testing.T) {
	home, tokenFile, env := runWorld(t, runToken)
	instances := filepath.Join(home, ".config", "cairn", "instances")
	if err := os.MkdirAll(instances, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(instances, "client.env"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	run1 := []string{"--run", "-host", "host-a", "-worker", "http://127.0.0.1:9", "-capture-token-file", tokenFile}
	dry := []string{"--dry-run", "-claude-root", filepath.Join(home, ".claude", "projects")}
	for name, c := range map[string]struct {
		args []string
		says string
	}{
		"run: an unconfigured alias":          {append(run1, "-route-origin-owner", "acme=elsewhere"), "does not configure"},
		"run: a configured alias, no -worker": {append(run1, "-route-origin-owner", "acme=client"), "has no -worker client=<url>"},
		"run: not OWNER=ALIAS":                {append(run1, "-route-origin-owner", "acme"), "is not OWNER=ALIAS"},
		"run: an owner named twice":           {append(run1, "-route-origin-owner", "acme=personal", "-route-origin-owner", "Acme=personal"), "twice"},
		"dry-run: an unconfigured alias":      {append(dry, "-route-origin-owner", "acme=elsewhere"), "does not configure"},
		"dry-run: not OWNER=ALIAS":            {append(dry, "-route-origin-owner", "git@git.example.invalid:acme=client"), "is not OWNER=ALIAS"},
	} {
		var errb bytes.Buffer
		if code := run(c.args, &bytes.Buffer{}, &errb, env); code != exitUsage || !strings.Contains(errb.String(), c.says) {
			t.Errorf("%s: exit %d, want %d naming %q (%s)", name, code, exitUsage, c.says, errb.String())
		}
	}
	// CONTROL: a well-formed rule naming a configured instance is accepted by the dry run.
	var out, errb bytes.Buffer
	if code := run(append(dry, "-route-origin-owner", "acme=client"), &out, &errb, env); code != exitOK {
		t.Fatalf("CONTROL: a well-formed rule: exit %d (%s)", code, errb.String())
	}
}

// TestATableRoutedRepoWithNoWorkerShipsToTheDefaultThroughTheBinary is #292's audit F1 as the
// auditor reproduced it, through the real wiring and the real HTTPSink: a second instance
// `client` is configured and the routing table sends the repo's scope `beta-notes` to it, but
// `--run` has a `-worker` for the default instance only. The session must ship to the default —
// before the fix it was refused on every run ("no worker listener is configured for instance
// client") at exit 0.
func TestATableRoutedRepoWithNoWorkerShipsToTheDefaultThroughTheBinary(t *testing.T) {
	p := newRunPod(t, true)
	w := repoRoutedWorld(t, `{"beta-notes":"client"}`)
	var errb bytes.Buffer
	code := run([]string{"--run", "-host", "host-a", "-worker", p.url, "-capture-token-file", w.tokenFile}, &bytes.Buffer{}, &errb, w.env)
	recs, _ := p.arc.ReadStream("s-0001", "main")
	if code != exitOK || len(recs) != 5 || strings.Contains(errb.String(), "refused s-0001") {
		t.Fatalf("exit %d, %d records stored on the default instance — want 0 and all 5:\n%s", code, len(recs), errb.String())
	}
}

// repoWorld is a two-instance host (the default and a synthetic `client`) with one Claude Code
// session, `s-0001`, of 5 records and no recorded scope, run inside a git repo named `beta-notes`.
type repoWorld struct {
	home, tokenFile, cfg, repo string
	env                        func(string) string
}

// repoRoutedWorld builds it, with `routes` as the routing table ("" for no table).
func repoRoutedWorld(t *testing.T, routes string) repoWorld {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_SYSTEM", os.DevNull)
	home, tokenFile, env := runWorld(t, runToken)
	w := repoWorld{home: home, tokenFile: tokenFile, cfg: filepath.Join(home, ".config", "cairn"), env: env,
		repo: filepath.Join(t.TempDir(), "beta-notes")}
	if err := os.MkdirAll(filepath.Join(w.cfg, "instances"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(w.cfg, "instances", "client.env"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if routes != "" {
		w.route(t, routes)
	}
	if out, err := exec.Command("git", "init", "-q", w.repo).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	var b strings.Builder
	for i := 0; i < 5; i++ {
		b.WriteString(w.line(i))
	}
	if err := os.WriteFile(w.session(), []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	return w
}

func (w repoWorld) route(t *testing.T, routes string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(w.cfg, "routes.json"), []byte(routes), 0o600); err != nil {
		t.Fatal(err)
	}
}

func (w repoWorld) session() string {
	return filepath.Join(w.home, ".claude", "projects", "-work-alpha", "s-0001.jsonl")
}

func (w repoWorld) line(i int) string {
	return fmt.Sprintf(`{"type":"user","sessionId":"s-0001","cwd":%q,"message":{"role":"user","content":"line %d"}}`+"\n", w.repo, i)
}

// TestAMovedSessionIsNotReplacedWhenALaterRunLacksItsWorker is #292's round-2 audit finding, the
// four runs the auditor drove through run() with two in-process worker pods:
//
//  1. both workers, no table entry → the default instance;
//  2. both workers, the table routes the repo to `client` → MOVED there (withdrawn from the default,
//     which the archive keeps as a deletion);
//  3. the default's worker only, with a new line to ship → the session must STAY bound to `client`,
//     refused and retried —
//     not "moved back" to a default whose probe answers 410 and HOLDS it for good;
//  4. both workers again → it resumes on `client`.
func TestAMovedSessionIsNotReplacedWhenALaterRunLacksItsWorker(t *testing.T) {
	personal, other := newRunPod(t, true), newRunPod(t, true)
	w := repoRoutedWorld(t, "")
	both := []string{"--run", "-host", "host-a", "-worker", personal.url, "-capture-token-file", w.tokenFile,
		"-worker", "client=" + other.url, "-capture-token-file", "client=" + w.tokenFile}
	defaultOnly := both[:7]
	pass := func(n int, args []string) string {
		t.Helper()
		var errb bytes.Buffer
		if code := run(args, &bytes.Buffer{}, &errb, w.env); code != exitOK {
			t.Fatalf("run %d: exit %d\n%s", n, code, errb.String())
		}
		return errb.String()
	}
	pass(1, both)
	if recs, _ := personal.arc.ReadStream("s-0001", "main"); len(recs) != 5 {
		t.Fatalf("CONTROL run 1: %d records on the default instance, want 5", len(recs))
	}
	w.route(t, `{"beta-notes":"client"}`)
	pass(2, both)
	if recs, _ := other.arc.ReadStream("s-0001", "main"); len(recs) != 5 {
		t.Fatalf("CONTROL run 2: %d records on client — the table's route did not move the session", len(recs))
	}
	appendLine := func(i int) {
		t.Helper()
		f, err := os.OpenFile(w.session(), os.O_WRONLY|os.O_APPEND, 0)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if _, err := f.WriteString(w.line(i)); err != nil {
			t.Fatal(err)
		}
	}
	appendLine(5)
	log3 := pass(3, defaultOnly)
	state, err := os.ReadFile(filepath.Join(w.home, "xdg", "cairn", "capture", "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(log3, "HOLD s-0001") || strings.Contains(string(state), `"held": true`) {
		t.Fatalf("run 3, without client's worker, HELD the moved session for good:\n%s", log3)
	}
	if !strings.Contains(log3, "no worker listener is configured for instance client") {
		t.Fatalf("CONTROL run 3: the session was not refused for client's missing worker:\n%s", log3)
	}
	appendLine(6)
	before := other.requests.Load()
	pass(4, both)
	if recs, _ := other.arc.ReadStream("s-0001", "main"); len(recs) != 7 || other.requests.Load() == before {
		t.Fatalf("run 4, with client's worker back: %d records on client, want 7 — the session did not resume", len(recs))
	}
}

// TestADryRunAnswersAsTheRunWouldForEachWorkerShape: `--dry-run` places a table-routed empty-V
// session by the aliases its own `-worker` flags name — none (every configured instance counts),
// the default's only, and the default's plus `client`'s.
func TestADryRunAnswersAsTheRunWouldForEachWorkerShape(t *testing.T) {
	w := repoRoutedWorld(t, `{"beta-notes":"client"}`)
	dry := []string{"--dry-run", "-claude-root", filepath.Join(w.home, ".claude", "projects")}
	for name, c := range map[string]struct {
		workers []string
		want    string
	}{
		"no -worker":         {nil, "decision=ship:client"},
		"the default's only": {[]string{"-worker", "http://127.0.0.1:9"}, "decision=ship:personal"},
		"with client's":      {[]string{"-worker", "http://127.0.0.1:9", "-worker", "client=http://127.0.0.1:9"}, "decision=ship:client"},
	} {
		var out, errb bytes.Buffer
		code := run(append(append([]string{}, dry...), c.workers...), &out, &errb, w.env)
		if code != exitOK || !strings.Contains(out.String(), "v=[] "+c.want+"\n") {
			t.Errorf("%s: exit %d, %q — want %s (%s)", name, code, out.String(), c.want, errb.String())
		}
	}
}
