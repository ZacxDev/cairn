package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// `--dry-run`, given `--run`'s flags and state, prints the decision `--run` makes (#293's audit).
// Each test binds a session with a real `--run` against in-process worker pods, then asks the dry
// run. Synthetic throughout: instances `client` and `gamma`, the repo `beta-notes`.

// dryDecision runs `--dry-run` over the world's one session and returns its `decision=` value.
func dryDecision(t *testing.T, w repoWorld, workers ...string) string {
	t.Helper()
	args := append([]string{"--dry-run", "-claude-root", filepath.Join(w.home, ".claude", "projects")}, workers...)
	var out, errb bytes.Buffer
	if code := run(args, &out, &errb, w.env); code != exitOK {
		t.Fatalf("--dry-run: exit %d\n%s", code, errb.String())
	}
	_, after, ok := strings.Cut(out.String(), " decision=")
	if !ok {
		t.Fatalf("--dry-run printed no decision: %q", out.String())
	}
	return strings.TrimSpace(after)
}

// mustRunPass runs `--run` and requires exit 0.
func mustRunPass(t *testing.T, w repoWorld, args []string) string {
	t.Helper()
	var errb bytes.Buffer
	if code := run(args, &bytes.Buffer{}, &errb, w.env); code != exitOK {
		t.Fatalf("--run: exit %d\n%s", code, errb.String())
	}
	return errb.String()
}

// editSession rewrites the state's entry for `s-0001` — the synthetic stand-in for what a pod's
// answer or an earlier read would have recorded there.
func editSession(t *testing.T, w repoWorld, edit func(map[string]any)) {
	t.Helper()
	raw, err := os.ReadFile(w.statePath())
	if err != nil {
		t.Fatal(err)
	}
	var st map[string]any
	if err := json.Unmarshal(raw, &st); err != nil {
		t.Fatal(err)
	}
	ss, ok := st["sessions"].(map[string]any)["s-0001"].(map[string]any)
	if !ok {
		t.Fatalf("CONTROL: the state holds no s-0001: %s", raw)
	}
	edit(ss)
	out, err := json.Marshal(st)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(w.statePath(), out, 0o600); err != nil {
		t.Fatal(err)
	}
}

// boundToClient is a two-instance world whose session a real `--run` has bound to `client` (the
// table routes `beta-notes` there), and the worker flags that run used.
func boundToClient(t *testing.T) (repoWorld, []string) {
	t.Helper()
	personal, cli := newRunPod(t, true), newRunPod(t, true)
	w := repoRoutedWorld(t, `{"beta-notes":"client"}`)
	workers := []string{"-worker", personal.url, "-capture-token-file", w.tokenFile,
		"-worker", "client=" + cli.url, "-capture-token-file", "client=" + w.tokenFile}
	mustRunPass(t, w, append([]string{"--run", "-host", "host-a"}, workers...))
	if recs, _ := cli.arc.ReadStream("s-0001", "main"); len(recs) != 5 {
		t.Fatalf("CONTROL: --run did not bind the session to client (%d records there)", len(recs))
	}
	return w, workers
}

// onlyWorkerFlags drops the token-file flags: `--dry-run` reads no token.
func onlyWorkerFlags(flags []string) []string {
	var out []string
	for i := 0; i+1 < len(flags); i += 2 {
		if flags[i] == "-worker" {
			out = append(out, flags[i], flags[i+1])
		}
	}
	return out
}

// TestADryRunPrintsHeldAndDeletedForWhatRunSkips: `--run` returns before routing a session the
// state marks HELD or DELETED, so it ships nothing of it — the dry run must say so rather than print
// a routing answer `--run` never makes.
func TestADryRunPrintsHeldAndDeletedForWhatRunSkips(t *testing.T) {
	for _, c := range []struct {
		mark string
		edit func(map[string]any)
	}{
		{"held", func(ss map[string]any) { ss["held"], ss["held_reason"] = true, "synthetic" }},
		{"deleted", func(ss map[string]any) { ss["deleted"] = "client" }},
	} {
		w, workers := boundToClient(t)
		editSession(t, w, c.edit)
		w.appendLine(t, 5)
		if got := dryDecision(t, w, onlyWorkerFlags(workers)...); got != c.mark {
			t.Errorf("a session the state marks %s: the dry run printed decision=%s — --run ships nothing of it", c.mark, got)
		}
		log := mustRunPass(t, w, append([]string{"--run", "-host", "host-a"}, workers...))
		if !strings.Contains(log, " shipped=0 ") {
			t.Errorf("CONTROL: --run shipped something of a %s session:\n%s", c.mark, log)
		}
	}
}

// TestADryRunDecidesOverTheStoredV: `--run` routes over the stored `V` UNION what it derives, so a
// scope recorded on an earlier run (its ledger since gone) still places the session. Here the state
// records a read of `alpha-notes` (table: personal): `--run` moves the session to personal, and the
// dry run — which derives nothing from this transcript — must print that, not the repo arm's client.
func TestADryRunDecidesOverTheStoredV(t *testing.T) {
	w, workers := boundToClient(t)
	w.route(t, `{"beta-notes":"client","alpha-notes":"personal"}`)
	editSession(t, w, func(ss map[string]any) { ss["v"] = []string{"alpha-notes"} })
	if got := dryDecision(t, w, onlyWorkerFlags(workers)...); got != "ship:personal" {
		t.Fatalf("a session whose stored V routes to personal: the dry run printed decision=%s", got)
	}
}

// TestADryRunPrintsDeferredForABoundSessionRoutedToAThirdInstanceWithNoWorker: bound to `client`,
// the table now routes the repo to `gamma` — configured, no `-worker` — so `--run` sends nothing
// this run; the dry run must print `deferred`.
func TestADryRunPrintsDeferredForABoundSessionRoutedToAThirdInstanceWithNoWorker(t *testing.T) {
	w, workers := boundToClient(t)
	w.addInstance(t, "gamma")
	w.route(t, `{"beta-notes":"gamma"}`)
	if got := dryDecision(t, w, onlyWorkerFlags(workers)...); got != "deferred" {
		t.Fatalf("bound to client, routed to gamma with no worker: the dry run printed decision=%s, want deferred", got)
	}
	w.appendLine(t, 5)
	if log := mustRunPass(t, w, append([]string{"--run", "-host", "host-a"}, workers...)); !strings.Contains(log, " shipped=0 ") ||
		!strings.Contains(log, " refused=1 ") {
		t.Fatalf("CONTROL: --run did not defer the session:\n%s", log)
	}
}

// TestADryRunUsesTheRepoRunKeptWhenTheWorktreeIsGone: `--run` placed a session run in a worktree
// of `beta-notes` on client and KEPT that repo; the worktree is then removed. `--run` keeps placing
// it by the kept repo — so must the dry run, which re-derived from the gone directory and printed the
// default.
func TestADryRunUsesTheRepoRunKeptWhenTheWorktreeIsGone(t *testing.T) {
	personal, cli := newRunPod(t, true), newRunPod(t, true)
	w := repoRoutedWorld(t, `{"beta-notes":"client"}`)
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=fixture", "GIT_AUTHOR_EMAIL=fixture@example.invalid",
			"GIT_COMMITTER_NAME=fixture", "GIT_COMMITTER_EMAIL=fixture@example.invalid")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("-C", w.repo, "commit", "-q", "--allow-empty", "-m", "fixture")
	wt := filepath.Join(filepath.Dir(w.repo), "beta-notes-topic")
	git("-C", w.repo, "worktree", "add", "-q", "-b", "topic", wt)
	w.repo = wt // the session ran in the worktree
	var b strings.Builder
	for i := 0; i < 5; i++ {
		b.WriteString(w.line(i))
	}
	if err := os.WriteFile(w.session(), []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	workers := []string{"-worker", personal.url, "-capture-token-file", w.tokenFile,
		"-worker", "client=" + cli.url, "-capture-token-file", "client=" + w.tokenFile}
	mustRunPass(t, w, append([]string{"--run", "-host", "host-a"}, workers...))
	if recs, _ := cli.arc.ReadStream("s-0001", "main"); len(recs) != 5 {
		t.Fatalf("CONTROL: --run did not place the worktree session on client (%d records)", len(recs))
	}
	if err := os.RemoveAll(wt); err != nil {
		t.Fatal(err)
	}
	if got := dryDecision(t, w, onlyWorkerFlags(workers)...); got != "ship:client" {
		t.Fatalf("its worktree removed, the dry run printed decision=%s — --run places it by the repo it kept: client", got)
	}
}
