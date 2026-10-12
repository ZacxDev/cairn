package refaudit

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/ZacxDev/cairn/internal/codesrc"
	"github.com/ZacxDev/cairn/internal/store"
)

// requireGit is the repository's two-tier rule (`internal/depspolicy`'s test): the `go` CI job sets
// CAIRN_GIT_TESTS_REQUIRED, where a missing git is a FAILURE; the nix check phase has none, where it is
// a skip that names itself.
func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		if os.Getenv("CAIRN_GIT_TESTS_REQUIRED") != "" {
			t.Fatalf("CAIRN_GIT_TESTS_REQUIRED is set but git is absent: this tier must measure the auditor (%v)", err)
		}
		t.Skipf("no git here (expected inside a nix derivation); the `go` CI job sets CAIRN_GIT_TESTS_REQUIRED "+
			"and cannot skip this (%v)", err)
	}
}

func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.invalid",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.invalid", "GIT_CONFIG_NOSYSTEM=1",
		"GIT_AUTHOR_DATE=2000-01-01T00:00:00Z", "GIT_COMMITTER_DATE=2000-01-01T00:00:00Z")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// world is a synthetic remote reached over `file://`: `<base>/example-org/example-repo` is a bare
// repository with `main`, plus a commit reachable ONLY from `refs/pull/1/head`.
type world struct {
	base, remote, work string
	onMain, offBranch  string
}

func newWorld(t *testing.T) world {
	t.Helper()
	requireGit(t)
	root := t.TempDir()
	w := world{base: filepath.Join(root, "remotes"), work: filepath.Join(root, "work")}
	w.remote = filepath.Join(w.base, "example-org", "example-repo")
	if err := os.MkdirAll(w.remote, 0o755); err != nil {
		t.Fatal(err)
	}
	gitIn(t, w.remote, "init", "--bare", "--quiet", "-b", "main")
	gitIn(t, root, "init", "--quiet", "-b", "main", w.work)
	writeFile(t, filepath.Join(w.work, "cmd", "x.go"), "package main\n\nfunc Widget() {}\n")
	writeFile(t, filepath.Join(w.work, "internal", "keep.go"), "package internal\n")
	writeFile(t, filepath.Join(w.work, "services", "widget", "main.go"), "package main\n\nfunc Serve() {}\n")
	// A file literally named `a*.go` beside `ab.go`: a GLOB pathspec matches both, a literal one only
	// the first — the shape `GIT_LITERAL_PATHSPECS` exists for.
	writeFile(t, filepath.Join(w.work, "ab.go"), "package main\n\nfunc Gadget() {}\n")
	writeFile(t, filepath.Join(w.work, "a*.go"), "package main\n")
	gitIn(t, w.work, "add", "-A")
	gitIn(t, w.work, "commit", "--quiet", "-m", "one")
	w.onMain = gitIn(t, w.work, "rev-parse", "HEAD")
	gitIn(t, w.work, "push", "--quiet", w.remote, "main:refs/heads/main")
	writeFile(t, filepath.Join(w.work, "feature.go"), "package main\n")
	gitIn(t, w.work, "add", "-A")
	gitIn(t, w.work, "commit", "--quiet", "-m", "two")
	w.offBranch = gitIn(t, w.work, "rev-parse", "HEAD")
	gitIn(t, w.work, "push", "--quiet", w.remote, "HEAD:refs/pull/1/head")
	return w
}

func (w world) git(t *testing.T) *GitCLI {
	return &GitCLI{MirrorRoot: filepath.Join(t.TempDir(), "mirrors"),
		Hosts: map[string]string{"github.com": "file://" + w.base}, Timeout: 60 * time.Second}
}

const src = "git:github.com/example-org/example-repo@main"

func entry(ref, pointers, nuance string, refs ...string) EntryInput {
	var tasks []store.TaskRef
	for _, r := range refs {
		sys, id, _ := strings.Cut(r, ":")
		tasks = append(tasks, store.TaskRef{System: sys, Ident: id, Raw: r})
	}
	return EntryInput{Ref: ref, Text: "---\nservice: " + ref + "\n---\n\n## Pointers\n" + pointers +
		"\n## Nuance / work-history\n" + nuance, Refs: tasks}
}

func statesOf(res Result) map[string]string {
	out := map[string]string{}
	for _, f := range res.Findings {
		out[f.Kind+" "+f.Value] = f.State
	}
	return out
}

// TestEachPlantedClaimMapsToOneLiteralState is decision 6's table over a real fetched world: every
// planted claim to ONE literal state, every spelling of a path normalised, and `ls-tree`'s exit code
// never trusted.
func TestEachPlantedClaimMapsToOneLiteralState(t *testing.T) {
	w := newWorld(t)
	pointers := strings.Join([]string{
		"- `./cmd/x.go` — the main",
		"- `internal/` — a directory",
		"- `cmd/x.go#Widget` — a symbol present",
		"- `cmd/x.go#Gone` — a symbol absent",
		"- `nope/y.go#Widget` — a symbol in a missing file",
		"- `cmd/missing.go` — gone",
		"- `:(top)cmd/x.go` — magic, looked up literally",
		"- `a*.go#Gadget` — a glob, looked up literally",
		"- the runbook — not a path",
	}, "\n")
	nuance := strings.Join([]string{
		"- 2000-01-02: RESOLVED " + w.onMain[:12] + ": on main.",
		"- 2000-01-03: RESOLVED " + w.offBranch + ": only a pull ref.",
		"- 2000-01-04: RESOLVED 0123456789abcdef: nowhere.",
	}, "\n")
	git := w.git(t)
	res := Audit([]string{src, "git:github.com/example-org/example-repo//services/widget@main"},
		[]EntryInput{entry("gadget-one", pointers, nuance), entry("gadget-two", "- `main.go` — subpath-relative\n", "")}, git, nil)
	got := statesOf(res)
	want := map[string]string{
		"path cmd/x.go":           HoldsRoot,
		"path internal":           HoldsRoot,
		"symbol cmd/x.go#Widget":  SymbolPresent,
		"symbol cmd/x.go#Gone":    SymbolAbsent,
		"symbol nope/y.go#Widget": SymbolFileMissing,
		"path cmd/missing.go":     Missing,
		"path :(top)cmd/x.go":     Missing,
		"symbol a*.go#Gadget":     SymbolAbsent,
		"path the runbook":        NotPathShaped,
		"path main.go":            HoldsSubpath,
		"commit " + w.onMain[:12]: OnBranch,
		"commit " + w.offBranch:   ExistsOffBranch,
		"commit 0123456789abcdef": NotReachable,
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s: %q, want %q", k, got[k], v)
		}
	}
	if len(got) != len(want) {
		t.Errorf("findings %v, want exactly %d", got, len(want))
	}
	// Off-branch is INFORMATIONAL and not-reachable could-not-look, so this world exits 9 (the
	// missing path) — and the source row quotes the fetched commit.
	if res.Exit() != ExitStale || res.Fetched[0].At != w.onMain {
		t.Fatalf("exit %d, fetched %+v", res.Exit(), res.Fetched)
	}
}

// TestReachabilityIsRecomputedFromTheRemotesCurrentRefs (round 2's 🟡3, clause f): run 1 fetches a SHA
// reachable only from `refs/pull/1/head`; that ref is then DELETED on the remote while a TAG pointing at
// the same SHA is KEPT. Run 2 — over the SAME mirror, which still holds the object — reports
// `not-reachable`. RED under `refaudit-presence-counted-as-reachability` (the object is still there) and
// under a fetch that keeps tags with an unrestricted ref walk.
func TestReachabilityIsRecomputedFromTheRemotesCurrentRefs(t *testing.T) {
	w := newWorld(t)
	git := w.git(t)
	e := []EntryInput{entry("gadget-one", "", "- RESOLVED "+w.offBranch+": fix.")}
	if s := statesOf(Audit([]string{src}, e, git, nil))["commit "+w.offBranch]; s != ExistsOffBranch {
		t.Fatalf("run 1: %q, want %q", s, ExistsOffBranch)
	}
	gitIn(t, w.remote, "tag", "v1", w.offBranch)
	gitIn(t, w.remote, "update-ref", "-d", "refs/pull/1/head")
	if s := statesOf(Audit([]string{src}, e, git, nil))["commit "+w.offBranch]; s != NotReachable {
		t.Fatalf("run 2 after the pull ref was deleted (tag kept): %q, want %q", s, NotReachable)
	}
}

// TestATagInAnOlderMirrorNeverCountsAsReachable: the namespace restriction ALONE holds — a tag planted
// straight into the mirror (what a build without `--no-tags` would have left) does not make its SHA
// reachable. RED under `refaudit-tags-count-as-reachable`.
func TestATagInAnOlderMirrorNeverCountsAsReachable(t *testing.T) {
	w := newWorld(t)
	git := w.git(t)
	e := []EntryInput{entry("gadget-one", "", "- RESOLVED "+w.offBranch+": fix.")}
	// Run 1 fetches the object into the mirror; then the remote drops its only ref.
	if s := statesOf(Audit([]string{src}, e, git, nil))["commit "+w.offBranch]; s != ExistsOffBranch {
		t.Fatalf("run 1: %q", s)
	}
	gitIn(t, w.remote, "update-ref", "-d", "refs/pull/1/head")
	if s := statesOf(Audit([]string{src}, e, git, nil))["commit "+w.offBranch]; s != NotReachable {
		t.Fatalf("before the planted tag: %q", s)
	}
	gitIn(t, git.MirrorDir(src), "update-ref", "refs/tags/v1", w.offBranch)
	if s := statesOf(Audit([]string{src}, e, git, nil))["commit "+w.offBranch]; s != NotReachable {
		t.Fatalf("a tag already in the mirror made the SHA %q; tags never count", s)
	}
}

// TestTheMirrorDirectoryIsADigest (T4): never a path built from the declaration.
func TestTheMirrorDirectoryIsADigest(t *testing.T) {
	g := &GitCLI{MirrorRoot: "/m"}
	sum := sha256.Sum256([]byte(src))
	if got := g.MirrorDir(src); got != "/m/"+hex.EncodeToString(sum[:])+".git" {
		t.Fatalf("mirror dir %q", got)
	}
}

// TestAnUnmappedHostIsNeverContacted (T1): the PAIR — a mapped host IS fetched (its mirror exists), an
// unmapped one is not (no mirror, no process), and the source row says why.
func TestAnUnmappedHostIsNeverContacted(t *testing.T) {
	w := newWorld(t)
	git := w.git(t)
	other := "git:git.example.com/team/example-repo@main"
	res := Audit([]string{src, other}, nil, git, nil)
	if _, err := os.Stat(git.MirrorDir(src)); err != nil {
		t.Fatalf("POSITIVE CONTROL: the mapped source was not fetched: %v", err)
	}
	if _, err := os.Stat(git.MirrorDir(other)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the unmapped host's mirror exists, so it was contacted: %v", err)
	}
	if res.Fetched[1].State != HostNotMapped {
		t.Fatalf("source rows %+v", res.Fetched)
	}
}

// fakeGit answers every claim from a table, for the exit-code rules (no git needed).
type fakeGit struct {
	unmapped map[string]bool
	paths    map[string]bool
	commits  map[string][]string
	refs     map[string][]string
}

func (f fakeGit) Fetch(s codesrc.Source) (string, error) {
	if f.unmapped[s.Host] {
		return "", &HostNotMappedError{Host: s.Host}
	}
	return "c0ffee", nil
}
func (f fakeGit) HasPath(_ string, _, p string) (bool, error)        { return f.paths[p], nil }
func (f fakeGit) Commits(_ string, prefix string) ([]string, error)  { return f.commits[prefix], nil }
func (f fakeGit) ContainedBy(_ string, sha string) ([]string, error) { return f.refs[sha], nil }
func (f fakeGit) Grep(string, string, string, string) (bool, error)  { return true, nil }

// TestTheExitIsTheMixedCase is decision 6's rule, each case a literal: one stale claim beside an
// unmapped source is 9; a clean mapped source beside an unmapped one is 10; nothing checked is 10; an
// off-branch commit is informational (0); an unreachable or ambiguous one is could-not-look (10).
func TestTheExitIsTheMixedCase(t *testing.T) {
	clean := fakeGit{paths: map[string]bool{"cmd/x.go": true}, unmapped: map[string]bool{"git.example.com": true},
		commits: map[string][]string{"aaaaaaa": {"aaaaaaa1"}, "bbbbbbb": {"bbbbbbb1"}, "ccccccc": {"ccccccc1", "ccccccc2"}, "ddddddd": nil},
		refs:    map[string][]string{"aaaaaaa1": {"refs/heads/main"}, "bbbbbbb1": {"refs/pull/1/head"}}}
	both := []string{src, "git:git.example.com/team/example-repo@main"}
	for _, tc := range []struct {
		name    string
		sources []string
		e       []EntryInput
		want    int
		reason  string
	}{
		{"stale beside unmapped", both, []EntryInput{entry("g", "- `cmd/gone.go` — x\n", "")}, ExitStale, ""},
		{"clean beside unmapped", both, []EntryInput{entry("g", "- `cmd/x.go` — x\n", "")}, ExitCouldNotLook, ""},
		{"clean, mapped only", []string{src}, []EntryInput{entry("g", "- `cmd/x.go` — x\n", "")}, ExitClean, ""},
		{"checked=0", []string{src}, []EntryInput{entry("g", "- prose only\n", "")}, ExitCouldNotLook, ReasonCheckedZero},
		{"off-branch is informational", []string{src}, []EntryInput{entry("g", "", "- RESOLVED bbbbbbb: x.")}, ExitClean, ""},
		{"on-branch", []string{src}, []EntryInput{entry("g", "", "- RESOLVED aaaaaaa: x.")}, ExitClean, ""},
		{"ambiguous", []string{src}, []EntryInput{entry("g", "- `cmd/x.go` — x\n", "- RESOLVED ccccccc: x.")}, ExitCouldNotLook, ""},
		{"not-reachable is never stale", []string{src}, []EntryInput{entry("g", "- `cmd/x.go` — x\n", "- RESOLVED ddddddd: x.")}, ExitCouldNotLook, ""},
	} {
		res := Audit(tc.sources, tc.e, clean, nil)
		if res.Exit() != tc.want || res.Reason() != tc.reason {
			t.Errorf("%s: exit %d reason %q, want %d %q\n%s", tc.name, res.Exit(), res.Reason(), tc.want, tc.reason, res.Render())
		}
	}
	if r := (Result{ScopeReason: ReasonUndeclared}); r.Exit() != ExitCouldNotLook || r.Reason() != ReasonUndeclared {
		t.Errorf("an undeclared scope: exit %d reason %q", r.Exit(), r.Reason())
	}
}

// TestTheSeverityTableIsExactlyTheEmittedStates (round 2's 🟡5): the table's key set EQUALS the states
// the checks emit, so a state added without a class goes red.
func TestTheSeverityTableIsExactlyTheEmittedStates(t *testing.T) {
	var keys []string
	for k := range Severity {
		keys = append(keys, k)
	}
	emitted := append([]string(nil), EmittedStates...)
	sort.Strings(keys)
	sort.Strings(emitted)
	if strings.Join(keys, "|") != strings.Join(emitted, "|") {
		t.Fatalf("severity keys %v\nemitted      %v", keys, emitted)
	}
	if len(keys) != 19 {
		t.Fatalf("%d states, want the plan's 19", len(keys))
	}
}

// TestThePRCheckReadsTheAPIAndNeverCallsOutsideTheSources: the stub answers open, merged, closed and
// 404 (twice: the issues fallback), each to a literal state; a ref to a repository the scope does not
// declare is `outside-sources` with NO call; a 403 is could-not-look.
func TestThePRCheckReadsTheAPIAndNeverCallsOutsideTheSources(t *testing.T) {
	var calls []string
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.URL.Path)
		switch r.URL.Path {
		case "/repos/example-org/example-repo/pulls/1":
			w.Write([]byte(`{"state":"open","merged_at":null}`))
		case "/repos/example-org/example-repo/pulls/2":
			w.Write([]byte(`{"state":"closed","merged_at":"2000-01-01T00:00:00Z"}`))
		case "/repos/example-org/example-repo/pulls/3":
			w.Write([]byte(`{"state":"closed","merged_at":null}`))
		case "/repos/example-org/example-repo/pulls/5":
			w.WriteHeader(http.StatusForbidden)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer stub.Close()
	e := entry("g", "", "", "github:example-org/example-repo#1", "github:example-org/example-repo#2",
		"github:example-org/example-repo#3", "github:example-org/example-repo#4", "github:example-org/example-repo#5",
		"github:other-org/other-repo#9")
	got := statesOf(Audit([]string{src}, []EntryInput{e}, fakeGit{}, GitHubPRs{Base: stub.URL}))
	for k, v := range map[string]string{
		"pr example-org/example-repo#1": PROpen, "pr example-org/example-repo#2": PRMerged,
		"pr example-org/example-repo#3": PRClosedUnmerged, "pr example-org/example-repo#4": PRNotFound,
		"pr example-org/example-repo#5": PRUnchecked, "pr other-org/other-repo#9": OutsideSources,
	} {
		if got[k] != v {
			t.Errorf("%s: %q, want %q", k, got[k], v)
		}
	}
	for _, c := range calls {
		if strings.Contains(c, "other-org") {
			t.Fatalf("an outside-sources ref was sent to the API: %v", calls)
		}
	}
	if len(calls) != 6 { // 1,2,3,5 once; 4 twice (pulls, then issues)
		t.Fatalf("calls %v", calls)
	}
}
