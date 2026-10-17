package capture

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ZacxDev/cairn/internal/client"
)

// Unit tests of the pieces the repo arm is built from. Unlike `repo_route_test.go` these name
// functions that did not exist before the arm, so they have no pre-change behaviour to fail on;
// what each pins is stated per test.

// TestOriginOwnerReadsTheThreeURLShapes: the owner is the first path segment of an `owner/repo`
// path, in the scp-like, https and ssh:// spellings — and nothing for a shape that has no owner.
func TestOriginOwnerReadsTheThreeURLShapes(t *testing.T) {
	for raw, want := range map[string]string{
		"git@git.example.invalid:acme/alpha-notes.git":                 "acme",
		"git@git.example.invalid:acme/alpha-notes":                     "acme",
		"git.example.invalid:acme/alpha-notes.git":                     "acme",
		"https://git.example.invalid/acme/alpha-notes.git":             "acme",
		"https://git.example.invalid/acme/alpha-notes/":                "acme",
		"http://git.example.invalid/acme/alpha-notes":                  "acme",
		"https://fixture-user:fixture-pass@git.example.invalid/acme/x": "acme",
		"ssh://git@git.example.invalid/acme/alpha-notes.git":           "acme",
		"ssh://git@git.example.invalid:2222/acme/alpha-notes.git":      "acme",
		"git+ssh://git.example.invalid/acme/alpha-notes":               "acme",
		"git://git.example.invalid/acme/alpha-notes.git":               "acme",
		"https://git.example.invalid/acme/team/alpha-notes.git":        "acme",
		"  git@git.example.invalid:Acme/alpha-notes.git\n":             "Acme",
		// No owner: a segment carrying `@` or `:` — the scp-like split takes the FIRST colon, so a
		// URL with a password in it would otherwise answer the password (#292's audit, F2).
		"user:fixture-pass@git.example.invalid:acme/alpha-notes.git":  "",
		"user:fixture-pass@git.example.invalid:2222/acme/alpha-notes": "",
		// No owner: a single segment, a local path, a file URL, a home-relative path, junk.
		"https://git.example.invalid/alpha-notes.git": "",
		"git@git.example.invalid:alpha-notes.git":     "",
		"/srv/git/acme/alpha-notes.git":               "",
		"../acme/alpha-notes":                         "",
		"file:///srv/git/acme/alpha-notes.git":        "",
		"ssh://git.example.invalid/~acme/alpha-notes": "",
		"https://git.example.invalid":                 "",
		"":                                            "",
	} {
		if got := OriginOwner(raw); got != want {
			t.Errorf("OriginOwner(%q) = %q, want %q", raw, got, want)
		}
	}
}

// TestParseOwnerRoutesRefusesWhatItCannotRoute: OWNER=ALIAS, a configured alias, each owner once
// (case folded); anything else is an error the binary turns into exit 2.
func TestParseOwnerRoutesRefusesWhatItCannotRoute(t *testing.T) {
	got, err := ParseOwnerRoutes([]string{"Acme=client", "globex=personal"}, repoRouting)
	if err != nil || len(got) != 2 || got["acme"] != "client" || got["globex"] != "personal" {
		t.Fatalf("CONTROL: two well-formed rules: %v, %v", got, err)
	}
	for name, values := range map[string][]string{
		"an unconfigured alias":     {"acme=elsewhere"},
		"no alias":                  {"acme"},
		"an empty owner":            {"=client"},
		"an empty alias":            {"acme="},
		"an owner with a host":      {"git.example.invalid/acme=client"},
		"an scp-like prefix":        {"git@git.example.invalid:acme=client"},
		"an owner named twice":      {"acme=client", "ACME=personal"},
		"an alias that is no alias": {"acme=../client"},
	} {
		if _, err := ParseOwnerRoutes(values, repoRouting); err == nil {
			t.Errorf("%s: %v was accepted", name, values)
		}
	}
}

// TestDeriveRepoAsksGitAsTheClientDoes: the scope the client would derive (a worktree answers its
// base clone's), the origin's owner, and which answers are SETTLED — kept for good — versus asked
// again next run.
func TestDeriveRepoAsksGitAsTheClientDoes(t *testing.T) {
	parent := t.TempDir()
	base := gitRepo(t, parent, "alpha-notes", "git@git.example.invalid:acme/alpha-notes.git")
	wt := gitWorktree(t, base, parent, "alpha-notes-topic")
	bare := gitRepo(t, parent, "beta-notes", "")
	plain := filepath.Join(parent, "not-a-repo")
	if err := os.MkdirAll(plain, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name, dir string
		want      Repo
		settled   bool
	}{
		{"a base clone", base, Repo{Scope: "alpha-notes", Owner: "acme"}, true},
		{"a worktree answers its base clone", wt, Repo{Scope: "alpha-notes", Owner: "acme"}, true},
		{"a repo with no origin", bare, Repo{Scope: "beta-notes"}, true},
		{"a directory that is not a repo", plain, Repo{}, true},
		{"a directory that is gone", filepath.Join(parent, "gone"), Repo{}, true},
		{"no directory recorded", "", Repo{}, false},
	} {
		got, settled := deriveRepo(c.dir)
		if got != c.want || settled != c.settled {
			t.Errorf("%s: %+v settled=%v, want %+v settled=%v", c.name, got, settled, c.want, c.settled)
		}
	}
	// A git that cannot be RUN answers nothing about the directory: not settled, so a timer started
	// with a thin PATH does not pin the session to the default for good.
	t.Setenv("PATH", t.TempDir())
	if got, settled := deriveRepo(base); got != (Repo{}) || settled {
		t.Fatalf("with no git on PATH: %+v settled=%v, want the zero Repo, not settled", got, settled)
	}
}

// TestTheRepoAnswerIsKeptInTheStateWithoutTheURL: what the state remembers is the scope and the
// owner — never the remote URL, which can carry a credential — and only once git has answered.
func TestTheRepoAnswerIsKeptInTheStateWithoutTheURL(t *testing.T) {
	e := repoEnv(t)
	const secret = "fixture-credential-not-real"
	repo := gitRepo(t, t.TempDir(), "gamma-notes", "https://"+secret+"@git.example.invalid/acme/gamma-notes.git")
	claudeIn(t, e, "s-state", repo, 1)
	a := newAgent(t, e, repoRouting, &fakeRunner{}, &bytes.Buffer{})
	mustRun(t, a)
	ss := a.State.Sessions["s-state"]
	if ss.Dir != repo || ss.Repo == nil || *ss.Repo != (Repo{Scope: "gamma-notes", Owner: "acme"}) {
		t.Fatalf("the state keeps dir=%q repo=%v — want the directory, and scope gamma-notes / owner acme", ss.Dir, ss.Repo)
	}
	raw, err := json.Marshal(a.State)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte(secret)) || bytes.Contains(raw, []byte("git.example.invalid")) {
		t.Fatalf("the state carries the remote URL:\n%s", raw)
	}

	// With no git to ask, the directory is recorded and NO repo is kept, so the next run asks again.
	claudeIn(t, e, "s-nogit", repo, 1)
	t.Setenv("PATH", t.TempDir())
	mustRun(t, a)
	if ss := a.State.Sessions["s-nogit"]; ss.Dir != repo || ss.Repo != nil || ss.Instance != "personal" {
		t.Fatalf("with no git to ask: dir=%q repo=%v instance=%q — want the directory recorded, no repo kept, the default",
			ss.Dir, ss.Repo, ss.Instance)
	}
}

// TestClaudeCwdIsTheFirstRecordsDirectory: the FIRST record carrying a `cwd`, past records that
// carry none or do not parse; "" when none does.
func TestClaudeCwdIsTheFirstRecordsDirectory(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s.jsonl")
	body := strings.Join([]string{
		`{"type":"mode","sessionId":"s"}`,
		`not json`,
		`["a","list"]`,
		`{"type":"user","cwd":"/fixture/first"}`,
		`{"type":"user","cwd":"/fixture/second"}`,
	}, "\n") + "\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := claudeCwd(path); got != "/fixture/first" {
		t.Fatalf("claudeCwd = %q, want /fixture/first", got)
	}
	if err := os.WriteFile(path, []byte(`{"type":"mode"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := claudeCwd(path); got != "" {
		t.Fatalf("a stream with no cwd answered %q", got)
	}
}

// TestASessionListDirectoryThatIsNotAStringIsNoDirectory: a malformed `directory` costs that row
// its directory, never the whole list.
func TestASessionListDirectoryThatIsNotAStringIsNoDirectory(t *testing.T) {
	roots, err := listRoots([]byte(`[{"id":"a","updated":1,"directory":"/fixture/a"},{"id":"b","updated":2,"directory":5},{"id":"c"}]`), 100)
	if err != nil || len(roots) != 3 || roots[0].Directory != "/fixture/a" || roots[1].Directory != "" || roots[2].Directory != "" {
		t.Fatalf("listRoots: %+v, %v", roots, err)
	}
}

// TestTheRepoArmPlacesNoSessionOnAnInstanceThisRunCannotUploadTo (#292's audit, F1), on Decide:
// neither rule binds a session to an alias outside `Uploadable`, and a table entry whose alias is
// unusable goes to the default — never to the owner rule, which must not override the table.
func TestTheRepoArmPlacesNoSessionOnAnInstanceThisRunCannotUploadTo(t *testing.T) {
	onlyDefault := RepoRules{Owners: OwnerRoutes{"acme": "client", "globex": "personal"}, Uploadable: map[string]bool{"personal": true}}
	onlyClient := RepoRules{Owners: OwnerRoutes{"acme": "client"}, Uploadable: map[string]bool{"client": true}}
	for _, c := range []struct {
		name  string
		repo  Repo
		rules RepoRules
		want  string
	}{
		{"rule 1 to an alias with no worker", Repo{Scope: "beta-notes"}, onlyDefault, "personal"},
		{"rule 1 with no worker does not fall to the owner rule", Repo{Scope: "beta-notes", Owner: "globex"}, onlyDefault, "personal"},
		{"rule 2 to an alias with no worker", Repo{Scope: "gamma-notes", Owner: "acme"}, onlyDefault, "personal"},
		{"a pin to the default is not overridden when the default has no worker", Repo{Scope: "pinned-notes", Owner: "acme"}, onlyClient, "personal"},
		{"CONTROL: rule 1 to an alias with a worker", Repo{Scope: "beta-notes"}, onlyClient, "client"},
		{"a session BOUND to the routed alias stays bound when it has no worker", Repo{Scope: "beta-notes"},
			RepoRules{Uploadable: map[string]bool{"personal": true}, Bound: "client"}, "client"},
		{"rule 2: a bound session stays bound too", Repo{Scope: "gamma-notes", Owner: "acme"},
			RepoRules{Owners: OwnerRoutes{"acme": "client"}, Uploadable: map[string]bool{"personal": true}, Bound: "client"}, "client"},
		{"a session bound to the default stays on the default", Repo{Scope: "beta-notes"},
			RepoRules{Uploadable: map[string]bool{"personal": true}, Bound: "personal"}, "personal"},
		{"an UNCONFIGURED alias still places a bound session on the default", Repo{Scope: "stale-notes"},
			RepoRules{Uploadable: map[string]bool{"personal": true}, Bound: "client"}, "personal"},
		{"CONTROL: rule 2 to an alias with a worker", Repo{Scope: "gamma-notes", Owner: "acme"}, onlyClient, "client"},
	} {
		if d := Decide(nil, c.repo, repoRouting, c.rules); d.Held || d.Instance != c.want {
			t.Errorf("%s: %+v, want %s", c.name, d, c.want)
		}
	}
}

// TestABoundSessionIsNeverShippedToAnInstanceItsRepoNoLongerRoutesTo (#292's audit, round 3, F1), on
// Decide: with three instances, a session bound to `client` whose repo the table now sends to
// `gamma` — configured, no worker this run — is DEFERRED: neither kept on client nor moved to the
// default. Bound to gamma itself, or to the default, or new, it is not deferred.
func TestABoundSessionIsNeverShippedToAnInstanceItsRepoNoLongerRoutesTo(t *testing.T) {
	three := client.Routing{
		Instances:    []client.Instance{{Alias: "personal"}, {Alias: "client"}, {Alias: "gamma"}},
		Routes:       map[string]string{"beta-notes": "gamma"},
		RoutesSource: "the test's table",
	}
	noGamma := map[string]bool{"personal": true, "client": true}
	for _, c := range []struct {
		name, bound, want string
		deferred          bool
	}{
		{"bound to another non-default instance", "client", "", true},
		{"bound to the routed instance itself", "gamma", "gamma", false},
		{"bound to the default", "personal", "personal", false},
		{"new", "", "personal", false},
	} {
		d := Decide(nil, Repo{Scope: "beta-notes"}, three, RepoRules{Uploadable: noGamma, Bound: c.bound})
		if d.Held || d.Deferred != c.deferred || d.Instance != c.want {
			t.Errorf("%s: %+v, want instance %q deferred=%v", c.name, d, c.want, c.deferred)
		}
	}
}
