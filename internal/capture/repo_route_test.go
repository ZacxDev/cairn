package capture

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/ZacxDev/cairn/internal/client"
)

// The repo arm of decision 16: a session that recorded NO scope is placed by the repository it ran
// in — the routing table's answer for the repo's scope when the table names it, else
// `-route-origin-owner`'s for its origin's owner, else the default instance. Synthetic
// throughout: owners `acme` and `globex`, scopes `*-notes`, hosts under `.invalid`.
//
// Every test here reaches the arm only through `decide` and `setOwnerRoutes`
// (`repo_seam_test.go`), so this file compiles unchanged against the code before the arm existed
// and its tests can be watched fail there on their own assertions.

var repoRouting = client.Routing{
	Instances: []client.Instance{{Alias: "personal"}, {Alias: "client"}},
	Routes: map[string]string{"alpha-notes": "personal", "beta-notes": "client", "pinned-notes": "personal",
		"stale-notes": "elsewhere"},
	RoutesSource: "the test's table",
}

// hermeticGit keeps the host's own git configuration (an `insteadOf` rewrite, a hook path) out of
// every git the test or the agent runs.
func hermeticGit(t *testing.T) {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_SYSTEM", os.DevNull)
}

func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=fixture", "GIT_AUTHOR_EMAIL=fixture@example.invalid",
		"GIT_COMMITTER_NAME=fixture", "GIT_COMMITTER_EMAIL=fixture@example.invalid")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// gitRepo makes `<parent>/<name>` a repository with one commit and `origin` set to `origin` (none
// when it is "").
func gitRepo(t *testing.T, parent, name, origin string) string {
	t.Helper()
	hermeticGit(t)
	dir := filepath.Join(parent, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "init", "-q")
	gitIn(t, dir, "commit", "-q", "--allow-empty", "-m", "fixture")
	if origin != "" {
		gitIn(t, dir, "remote", "add", "origin", origin)
	}
	return dir
}

// gitWorktree adds a worktree of `base` at `<parent>/<name>`.
func gitWorktree(t *testing.T, base, parent, name string) string {
	t.Helper()
	dir := filepath.Join(parent, name)
	gitIn(t, base, "worktree", "add", "-q", "-b", name, dir)
	return dir
}

// repoEnv is an empty capture world: a Claude Code projects root, a ledger dir, an in-memory sink.
func repoEnv(t *testing.T) env {
	t.Helper()
	root := t.TempDir()
	e := env{root: root, claude: filepath.Join(root, "projects"), ledger: filepath.Join(root, "ledger"), sink: newMemSink()}
	if err := os.MkdirAll(e.ledger, 0o700); err != nil {
		t.Fatal(err)
	}
	return e
}

// claudeIn writes a Claude Code session `id` of `n` plain records, each carrying `cwd` — none of
// which reads or writes a scope, so its V is empty. It returns the session file.
func claudeIn(t *testing.T, e env, id, cwd string, n int) string {
	t.Helper()
	proj := filepath.Join(e.claude, "-fixture-project")
	if err := os.MkdirAll(proj, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(proj, id+".jsonl")
	var b strings.Builder
	for i := 0; i < n; i++ {
		b.WriteString(claudeLine(id, cwd, i) + "\n")
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func claudeLine(id, cwd string, i int) string {
	c, _ := json.Marshal(cwd)
	return fmt.Sprintf(`{"type":"user","sessionId":%q,"cwd":%s,"message":{"role":"user","content":"line %d"}}`, id, c, i)
}

// mustBeEmptyV is the control every end-to-end case here stands on: the session recorded no scope.
func mustBeEmptyV(t *testing.T, a *Agent, id string) {
	t.Helper()
	if ss := a.State.Sessions[id]; ss == nil || len(ss.V) != 0 {
		t.Fatalf("CONTROL: the fixture session must record no scope, so the repo arm is what places it; state %+v", ss)
	}
}

// TestAnEmptyVSessionInAnOwnerRoutedRepoShipsToThatInstance is THE regression: a session that read
// no scope, run inside a repository whose `origin` owner `-route-origin-owner` sends to the second
// instance, went to the DEFAULT instance unconditionally — the wrong tenant.
func TestAnEmptyVSessionInAnOwnerRoutedRepoShipsToThatInstance(t *testing.T) {
	e := repoEnv(t)
	repo := gitRepo(t, t.TempDir(), "gamma-notes", "git@git.example.invalid:acme/gamma-notes.git")
	claudeIn(t, e, "s-repo", repo, 3)
	a := newAgent(t, e, repoRouting, &fakeRunner{}, &bytes.Buffer{})
	setOwnerRoutes(a, OwnerRoutes{"acme": "client"})
	mustRun(t, a)
	mustBeEmptyV(t, a, "s-repo")
	if got := a.State.Sessions["s-repo"].Instance; got != "client" {
		t.Fatalf("a session with no recorded scope, run in a repo whose origin owner is routed to `client`, was bound to %q", got)
	}
	if len(spooled(t, e, "client", "s-repo", "main")) != 3 || e.sink.has("personal", "s-repo") {
		t.Fatalf("the session's records: %d on client, any on personal=%v — want all 3 on client, none on personal",
			len(spooled(t, e, "client", "s-repo", "main")), e.sink.has("personal", "s-repo"))
	}
}

// TestAWorktreeSessionIsPlacedByItsBaseClonesScope: rule 1 derives the scope as the client does —
// a worktree answers its BASE clone's directory name, not its own — and the table naming that
// scope places the session, with no owner rule at all.
func TestAWorktreeSessionIsPlacedByItsBaseClonesScope(t *testing.T) {
	e := repoEnv(t)
	parent := t.TempDir()
	base := gitRepo(t, parent, "beta-notes", "")
	wt := gitWorktree(t, base, parent, "beta-notes-topic")
	claudeIn(t, e, "s-wt", wt, 2)
	a := newAgent(t, e, repoRouting, &fakeRunner{}, &bytes.Buffer{})
	mustRun(t, a)
	mustBeEmptyV(t, a, "s-wt")
	if got := a.State.Sessions["s-wt"].Instance; got != "client" {
		t.Fatalf("a session in a worktree of beta-notes (the table sends beta-notes to client) was bound to %q", got)
	}
}

// TestTheTablesAnswerForTheRepoScopeBeatsTheOwnerRule: rule 1 before rule 2. The repo's scope is
// `beta-notes` (table: client) and its owner `globex` is routed to personal; the table wins.
func TestTheTablesAnswerForTheRepoScopeBeatsTheOwnerRule(t *testing.T) {
	e := repoEnv(t)
	repo := gitRepo(t, t.TempDir(), "beta-notes", "https://git.example.invalid/globex/beta-notes.git")
	claudeIn(t, e, "s-both", repo, 2)
	a := newAgent(t, e, repoRouting, &fakeRunner{}, &bytes.Buffer{})
	setOwnerRoutes(a, OwnerRoutes{"globex": "personal"})
	mustRun(t, a)
	mustBeEmptyV(t, a, "s-both")
	if got := a.State.Sessions["s-both"].Instance; got != "client" {
		t.Fatalf("the table names the repo's scope (beta-notes → client) but the session was bound to %q — "+
			"the owner rule must not override an explicitly-named scope", got)
	}
}

// TestAShippedEmptyVSessionMovesThroughTheWithdrawal is what the FIRST run with the new rule does
// to a session an earlier run already shipped to the default instance: the changed answer is a
// MOVE — the default instance is asked to withdraw it, and the WHOLE session is re-shipped from
// offset 0 to the routed instance. Neither stranded on the default nor duplicated on both.
func TestAShippedEmptyVSessionMovesThroughTheWithdrawal(t *testing.T) {
	e := repoEnv(t)
	repo := gitRepo(t, t.TempDir(), "gamma-notes", "ssh://git@git.example.invalid:2222/acme/gamma-notes.git")
	path := claudeIn(t, e, "s-move", repo, 3)
	a := newAgent(t, e, repoRouting, &fakeRunner{}, &bytes.Buffer{})
	mustRun(t, a)
	if a.State.Sessions["s-move"].Instance != "personal" || len(spooled(t, e, "personal", "s-move", "main")) != 3 {
		t.Fatal("CONTROL: with no owner rule, run 1 must ship the empty-V session to the default instance")
	}
	setOwnerRoutes(a, OwnerRoutes{"acme": "client"})
	appendTo(t, path, claudeLine("s-move", repo, 3))
	sum := mustRun(t, a)
	if got := a.State.Sessions["s-move"].Instance; got != "client" {
		t.Fatalf("after the owner rule is set, the shipped session is still bound to %q", got)
	}
	if !slices.Contains(e.sink.withdrawn["personal"], "s-move") || sum.Withdrawn != 1 {
		t.Fatalf("the move did not withdraw the prefix from the default instance: withdrawn=%v, %+v", e.sink.withdrawn, sum)
	}
	if got := spooled(t, e, "client", "s-move", "main"); len(got) != 4 || got[0].Src != "0" {
		t.Fatalf("the routed instance holds %d records — want the WHOLE session (4) re-shipped from offset 0", len(got))
	}
	for k := range e.sink.pos {
		if strings.HasPrefix(k, "personal/s-move/") {
			t.Fatalf("the default instance still holds %s after the move", k)
		}
	}
}

// TestARemovedWorktreeDoesNotMoveTheSessionBack: a session placed by its worktree stays placed when
// the worktree is deleted — as worktrees routinely are once their work merges. Re-asking git every
// run would read "gone" as "no repo" and withdraw the session back to the default instance.
func TestARemovedWorktreeDoesNotMoveTheSessionBack(t *testing.T) {
	e := repoEnv(t)
	parent := t.TempDir()
	base := gitRepo(t, parent, "beta-notes", "")
	wt := gitWorktree(t, base, parent, "beta-notes-gone")
	path := claudeIn(t, e, "s-gone", wt, 2)
	a := newAgent(t, e, repoRouting, &fakeRunner{}, &bytes.Buffer{})
	mustRun(t, a)
	if a.State.Sessions["s-gone"].Instance != "client" {
		t.Fatalf("CONTROL: run 1 must place the worktree session on client; bound to %q", a.State.Sessions["s-gone"].Instance)
	}
	if err := os.RemoveAll(wt); err != nil {
		t.Fatal(err)
	}
	appendTo(t, path, claudeLine("s-gone", wt, 2))
	sum := mustRun(t, a)
	if got := a.State.Sessions["s-gone"].Instance; got != "client" || sum.Withdrawn != 0 || len(e.sink.withdrawn["client"]) != 0 {
		t.Fatalf("after its worktree was removed the session is bound to %q (withdrawals %v) — want it to stay on client",
			got, e.sink.withdrawn)
	}
	if n := len(spooled(t, e, "client", "s-gone", "main")); n != 3 {
		t.Fatalf("client holds %d records — want the appended line shipped after the first two (3)", n)
	}
}

// TestAnOpencodeSessionIsPlacedByItsListedDirectory: an opencode root is placed by the `directory`
// its `session list` row carries.
func TestAnOpencodeSessionIsPlacedByItsListedDirectory(t *testing.T) {
	e := repoEnv(t)
	repo := gitRepo(t, t.TempDir(), "gamma-notes", "git@git.example.invalid:acme/gamma-notes")
	dir, _ := json.Marshal(repo)
	runner := &fakeRunner{
		list:    []byte(fmt.Sprintf(`[{"id":"ses_repo","updated":946685651000,"directory":%s}]`, dir)),
		exports: map[string][]byte{"ses_repo": []byte(`{"info":{"id":"ses_repo"},"messages":[]}`)},
	}
	a := newAgent(t, e, repoRouting, runner, &bytes.Buffer{})
	a.ClaudeRoot, a.OpencodeDirs = "", []string{repo}
	setOwnerRoutes(a, OwnerRoutes{"acme": "client"})
	mustRun(t, a)
	mustBeEmptyV(t, a, "ses_repo")
	if got := a.State.Sessions["ses_repo"].Instance; got != "client" || !e.sink.has("client", "ses_repo") {
		t.Fatalf("an opencode root listed in an acme repo was bound to %q (shipped to client: %v)", got, e.sink.has("client", "ses_repo"))
	}
}

// TestADryRunPrintsTheRepoDecision: `--dry-run` places an empty-V session exactly as `--run` does.
func TestADryRunPrintsTheRepoDecision(t *testing.T) {
	e := repoEnv(t)
	repo := gitRepo(t, t.TempDir(), "gamma-notes", "git@git.example.invalid:acme/gamma-notes.git")
	claudeIn(t, e, "s-dry", repo, 1)
	a := newAgent(t, e, repoRouting, &fakeRunner{}, &bytes.Buffer{})
	setOwnerRoutes(a, OwnerRoutes{"acme": "client"})
	var out bytes.Buffer
	if err := a.DryRun(&out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "v=[] decision=ship:client") {
		t.Fatalf("the dry run's decision for an empty-V session in an acme repo: %q", out.String())
	}
}

// TestDecidePlacesAnEmptyVSessionByItsRepo pins the precedence on Decide itself: first match wins
// — a table-named repo scope, then the owner rule, then the default.
func TestDecidePlacesAnEmptyVSessionByItsRepo(t *testing.T) {
	owners := OwnerRoutes{"acme": "client", "globex": "personal"}
	for _, c := range []struct {
		name   string
		repo   Repo
		owners OwnerRoutes
		want   string
	}{
		{"the table names the repo's scope", Repo{Scope: "beta-notes"}, nil, "client"},
		{"the table's scope beats the owner rule", Repo{Scope: "beta-notes", Owner: "globex"}, owners, "client"},
		{"the owner rule, for a scope the table does not name", Repo{Scope: "gamma-notes", Owner: "acme"}, owners, "client"},
		{"the owner rule folds case", Repo{Scope: "gamma-notes", Owner: "ACME"}, owners, "client"},
		{"a table entry for an UNCONFIGURED alias falls through to the owner rule", Repo{Scope: "stale-notes", Owner: "acme"}, owners, "client"},
	} {
		if d := decide(nil, c.repo, repoRouting, c.owners); d.Held || d.Instance != c.want {
			t.Errorf("%s: %+v, want %s", c.name, d, c.want)
		}
	}
}

// TestDecideKeepsTheDefaultWhenNothingMatches — ⚠ INVARIANT GUARDS, not regression coverage: the
// code before the repo arm sent every one of these to the default instance too. They pin that the
// arm does not WIDEN: an operator's pin to the default holds against the owner rule, and every way
// of having no usable repo answer falls to the default.
func TestDecideKeepsTheDefaultWhenNothingMatches(t *testing.T) {
	owners := OwnerRoutes{"acme": "client"}
	for _, c := range []struct {
		name string
		repo Repo
	}{
		{"a repo the table pins to the default, whatever its owner", Repo{Scope: "pinned-notes", Owner: "acme"}},
		{"an owner no rule names", Repo{Scope: "gamma-notes", Owner: "initech"}},
		{"a repo with no origin", Repo{Scope: "gamma-notes"}},
		{"no repo at all (not git, gone, or no directory recorded)", Repo{}},
		{"an unconfigured table entry and no owner rule", Repo{Scope: "stale-notes"}},
	} {
		if d := decide(nil, c.repo, repoRouting, owners); d.Held || d.Instance != client.DefaultAlias {
			t.Errorf("%s: %+v, want the default instance", c.name, d)
		}
	}
}

// TestARecordedScopeIsNeverOverruledByTheRepo — ⚠ AN INVARIANT GUARD: the repo arm reads nothing
// when V is non-empty, so every V answer is the one it was without a repo, byte for byte. The case
// that matters most is the first: a session in an `acme` repo whose scope the table sends to
// client, that READ only a default-instance scope, still goes to the default instance.
func TestARecordedScopeIsNeverOverruledByTheRepo(t *testing.T) {
	repo := Repo{Scope: "beta-notes", Owner: "acme"}
	owners := OwnerRoutes{"acme": "client"}
	if d := decide([]string{"alpha-notes"}, repo, repoRouting, owners); d.Held || d.Instance != "personal" {
		t.Fatalf("a session that read only alpha-notes (personal), run in an acme repo routed to client: %+v — want personal", d)
	}
	for _, v := range [][]string{{"alpha-notes"}, {"beta-notes"}, {"alpha-notes", "beta-notes"}, {"*"}, {"gamma-notes"}, {"stale-notes"}} {
		with, without := decide(v, repo, repoRouting, owners), decide(v, Repo{}, repoRouting, nil)
		if with != without {
			t.Errorf("V=%v: with a repo %+v, without %+v — the repo must not move a recorded scope", v, with, without)
		}
	}
}
