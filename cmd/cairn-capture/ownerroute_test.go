package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
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

// addInstance configures one more synthetic instance beside the default.
func (w repoWorld) addInstance(t *testing.T, alias string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(w.cfg, "instances", alias+".env"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
}

func (w repoWorld) appendLine(t *testing.T, i int) {
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

func (w repoWorld) statePath() string {
	return filepath.Join(w.home, "xdg", "cairn", "capture", "state.json")
}

// TestABoundSessionRoutedToAThirdInstanceWithNoWorkerShipsNothing is #292's round-3 audit F1, the
// auditor's three-instance sequence through run(): bound to `client`, the table then routes the repo
// to `gamma` — configured, no `-worker` this run. Its NEW line must go NOWHERE this run (counted
// `refused=`), never to `client`, which the table no longer routes it to, and never to the default
// (whose probe can answer 410 and HOLD it). Once gamma has a worker, it moves there whole.
func TestABoundSessionRoutedToAThirdInstanceWithNoWorkerShipsNothing(t *testing.T) {
	personal, cli, gamma := newRunPod(t, true), newRunPod(t, true), newRunPod(t, true)
	w := repoRoutedWorld(t, `{"beta-notes":"client"}`)
	w.addInstance(t, "gamma")
	two := []string{"--run", "-host", "host-a", "-worker", personal.url, "-capture-token-file", w.tokenFile,
		"-worker", "client=" + cli.url, "-capture-token-file", "client=" + w.tokenFile}
	pass := func(n int, args []string) string {
		t.Helper()
		var errb bytes.Buffer
		if code := run(args, &bytes.Buffer{}, &errb, w.env); code != exitOK {
			t.Fatalf("run %d: exit %d\n%s", n, code, errb.String())
		}
		return errb.String()
	}
	pass(1, two)
	if recs, _ := cli.arc.ReadStream("s-0001", "main"); len(recs) != 5 {
		t.Fatalf("CONTROL run 1: %d records on client, want 5", len(recs))
	}
	w.route(t, `{"beta-notes":"gamma"}`)
	w.appendLine(t, 5)
	log2 := pass(2, two)
	if recs, _ := cli.arc.ReadStream("s-0001", "main"); len(recs) != 5 {
		t.Fatalf("run 2: client now holds %d records — the new line shipped to an instance the table no longer "+
			"routes the repo to:\n%s", len(recs), log2)
	}
	if recs, _ := personal.arc.ReadStream("s-0001", "main"); len(recs) != 0 || strings.Contains(log2, "HOLD s-0001") {
		t.Fatalf("run 2: the default holds %d records, or the session was held:\n%s", len(recs), log2)
	}
	if !strings.Contains(log2, "refused s-0001") || !strings.Contains(log2, " refused=1 ") {
		t.Fatalf("run 2: the deferral is not counted as this run's refusal:\n%s", log2)
	}
	three := append(append([]string{}, two...), "-worker", "gamma="+gamma.url, "-capture-token-file", "gamma="+w.tokenFile)
	pass(3, three)
	if recs, _ := gamma.arc.ReadStream("s-0001", "main"); len(recs) != 6 {
		t.Fatalf("run 3, gamma's worker added: %d records on gamma, want the whole session (6)", len(recs))
	}
}

// TestADryRunReadsWhereRunBoundTheSession is #292's round-3 audit F2: `--dry-run`, given `--run`'s
// flags, prints `--run`'s decision for a session `--run` already BOUND — it reads the binding from the
// state, READ-ONLY. Table → client, the default's worker only: `--run` keeps a client-bound session
// on client (never re-placed), so the dry run must print `ship:client`, not the unbound answer
// `ship:personal`. And it writes nothing, the state included.
func TestADryRunReadsWhereRunBoundTheSession(t *testing.T) {
	personal, cli := newRunPod(t, true), newRunPod(t, true)
	w := repoRoutedWorld(t, `{"beta-notes":"client"}`)
	dry := []string{"--dry-run", "-claude-root", filepath.Join(w.home, ".claude", "projects"), "-worker", personal.url}
	var out, errb bytes.Buffer
	if code := run(dry, &out, &errb, w.env); code != exitOK || !strings.Contains(out.String(), "decision=ship:personal") {
		t.Fatalf("CONTROL, before any --run: exit %d, %q, %q — want the unbound answer", code, out.String(), errb.String())
	}
	noStateSaid := strings.Contains(errb.String(), "no state file at")
	both := []string{"--run", "-host", "host-a", "-worker", personal.url, "-capture-token-file", w.tokenFile,
		"-worker", "client=" + cli.url, "-capture-token-file", "client=" + w.tokenFile}
	if code := run(both, &bytes.Buffer{}, &errb, w.env); code != exitOK {
		t.Fatalf("run: exit %d\n%s", code, errb.String())
	}
	before := listFiles(t, w.home)
	stateBefore, err := os.ReadFile(w.statePath())
	if err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(w.statePath())
	out.Reset()
	errb.Reset()
	if code := run(dry, &out, &errb, w.env); code != exitOK || !strings.Contains(out.String(), "v=[] decision=ship:client\n") {
		t.Fatalf("a dry run with the default's worker only, for a session --run bound to client: exit %d, %q (%s) — "+
			"want ship:client, what --run does", code, out.String(), errb.String())
	}
	stateAfter, _ := os.ReadFile(w.statePath())
	again, _ := os.Stat(w.statePath())
	if !bytes.Equal(stateBefore, stateAfter) || !os.SameFile(info, again) || !again.ModTime().Equal(info.ModTime()) ||
		!slices.Equal(before, listFiles(t, w.home)) {
		t.Fatal("the dry run wrote: the state changed, or a file appeared under HOME")
	}
	// And --run, given the same worker flags, does what the dry run said: the session stays on client.
	runOnlyDefault := []string{"--run", "-host", "host-a", "-worker", personal.url, "-capture-token-file", w.tokenFile}
	w.appendLine(t, 5)
	errb.Reset()
	if code := run(runOnlyDefault, &bytes.Buffer{}, &errb, w.env); code != exitOK {
		t.Fatalf("run: exit %d\n%s", code, errb.String())
	}
	if recs, _ := personal.arc.ReadStream("s-0001", "main"); len(recs) != 0 {
		t.Fatalf("--run with the default's worker only moved the session to the default (%d records)", len(recs))
	}
	if !noStateSaid {
		t.Fatal("the dry run before any --run did not say on stderr that there was no state file")
	}
}

// TestADryRunRefusesAStateItCannotRead: a state file that is not one JSON state exits 1 — the dry
// run does not print decisions about bindings nobody can see — and is left exactly as it was.
func TestADryRunRefusesAStateItCannotRead(t *testing.T) {
	w := repoRoutedWorld(t, `{"beta-notes":"client"}`)
	if err := os.MkdirAll(filepath.Dir(w.statePath()), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(w.statePath(), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	var out, errb bytes.Buffer
	code := run([]string{"--dry-run", "-claude-root", filepath.Join(w.home, ".claude", "projects")}, &out, &errb, w.env)
	got, _ := os.ReadFile(w.statePath())
	if code != exitError || out.Len() != 0 || string(got) != "{not json" {
		t.Fatalf("a malformed state: exit %d, stdout %q, state now %q — want 1, nothing printed, untouched (%s)",
			code, out.String(), got, errb.String())
	}
}
