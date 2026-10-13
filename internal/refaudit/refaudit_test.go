package refaudit

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
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
	unmapped    map[string]bool
	unreachable map[string]bool // a host whose fetch fails (a remote that could not be fetched)
	paths       map[string]bool
	commits     map[string][]string
	refs        map[string][]string
	noSymbol    map[string]bool // a symbol Grep does not find; every other symbol is found
}

func (f fakeGit) Fetch(s codesrc.Source) (string, error) {
	if f.unmapped[s.Host] {
		return "", &HostNotMappedError{Host: s.Host}
	}
	if f.unreachable[s.Host] {
		return "", errors.New("git fetch: exit 128: synthetic")
	}
	return "c0ffee", nil
}
func (f fakeGit) HasPath(_ string, _, p string) (bool, error)        { return f.paths[p], nil }
func (f fakeGit) Commits(_ string, prefix string) ([]string, error)  { return f.commits[prefix], nil }
func (f fakeGit) ContainedBy(_ string, sha string) ([]string, error) { return f.refs[sha], nil }
func (f fakeGit) Grep(_, _, _, symbol string) (bool, error)          { return !f.noSymbol[symbol], nil }

// TestTheExitIsTheMixedCase is decision 6's rule, each case a literal: one missing path beside an
// unmapped source is 10 (a stale verdict needs every source read — the operator's decision; the rows
// are `TestAStaleVerdictMeansEverySourceWasRead`); a clean mapped source beside an unmapped one is 10; nothing checked is 10; an
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
		{"missing beside unmapped is not stale", both, []EntryInput{entry("g", "- `cmd/gone.go` — x\n", "")}, ExitCouldNotLook, ReasonCheckedZero},
		{"stale, every source read", []string{src}, []EntryInput{entry("g", "- `cmd/gone.go` — x\n", "")}, ExitStale, ""},
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

// TestAStaleVerdictMeansEverySourceWasRead is the operator's decision on #259 r1 🟡2: when ANY declared
// source could not be looked at, a claim missing from the sources that WERE fetched is could-not-look
// (exit 10), never stale (exit 9) — the unfetched source may hold it, and a claim holds if ANY source
// does. Each source-dependent stale state is a row (path, symbol file, symbol), and both ways a source
// goes unlooked (unmapped, unreachable) appear; the CONTROLS are the same claims with every source read,
// which stay 9. A PR's `not-found` is the GitHub API's answer, not a source's, so it stays stale beside
// an unlooked source — pinned so the demotion cannot quietly widen to it.
//
// The new state is spelled as a literal rather than named, so the test pins the output a reader sees.
func TestAStaleVerdictMeansEverySourceWasRead(t *testing.T) {
	const notInFetched = "unchecked: not in the sources fetched"
	git := fakeGit{paths: map[string]bool{"cmd/x.go": true}, noSymbol: map[string]bool{"Vanished": true},
		unmapped: map[string]bool{"git.example.com": true}, unreachable: map[string]bool{"git.example.org": true}}
	unmapped := []string{src, "git:git.example.com/team/example-repo@main"}
	unreachable := []string{src, "git:git.example.org/team/example-repo@main"}
	gone := entry("g", "- `cmd/gone.go` — x\n", "")
	fileGone := entry("g", "- `cmd/gone.go#Widget` — x\n", "")
	absent := entry("g", "- `cmd/x.go#Vanished` — x\n", "")
	for _, tc := range []struct {
		name    string
		sources []string
		e       EntryInput
		claim   string // the finding's `kind value`
		state   string
		exit    int
		stale   int
	}{
		{"missing path beside unmapped", unmapped, gone, "path cmd/gone.go", notInFetched, ExitCouldNotLook, 0},
		{"missing path beside unreachable", unreachable, gone, "path cmd/gone.go", notInFetched, ExitCouldNotLook, 0},
		{"symbol file missing beside unmapped", unmapped, fileGone, "symbol cmd/gone.go#Widget", notInFetched, ExitCouldNotLook, 0},
		{"symbol absent beside unreachable", unreachable, absent, "symbol cmd/x.go#Vanished", notInFetched, ExitCouldNotLook, 0},
		{"control: missing path, every source read", []string{src}, gone, "path cmd/gone.go", Missing, ExitStale, 1},
		{"control: symbol file missing, every source read", []string{src}, fileGone, "symbol cmd/gone.go#Widget", SymbolFileMissing, ExitStale, 1},
		{"control: symbol absent, every source read", []string{src}, absent, "symbol cmd/x.go#Vanished", SymbolAbsent, ExitStale, 1},
	} {
		res := Audit(tc.sources, []EntryInput{tc.e}, git, nil)
		_, stale, _ := res.Counts()
		if got := statesOf(res)[tc.claim]; got != tc.state || res.Exit() != tc.exit || stale != tc.stale {
			t.Errorf("%s: %s state %q exit %d stale=%d, want %q %d stale=%d\n%s",
				tc.name, tc.claim, got, res.Exit(), stale, tc.state, tc.exit, tc.stale, res.Render())
		}
	}
	// The PR row: the API answered 404, which no unfetched git source could change.
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repos/example-org/example-repo" {
			w.Write([]byte(`{"full_name":"example-org/example-repo"}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer stub.Close()
	res := Audit(unmapped, []EntryInput{entry("g", "", "", "github:example-org/example-repo#4")}, git, GitHubPRs{Base: stub.URL})
	if got := statesOf(res)["pr example-org/example-repo#4"]; got != PRNotFound || res.Exit() != ExitStale {
		t.Errorf("a 404 PR beside an unmapped source: state %q exit %d, want %q %d\n%s", got, res.Exit(), PRNotFound, ExitStale, res.Render())
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
	if len(keys) != 20 {
		t.Fatalf("%d states, want the plan's 20", len(keys))
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
		case "/repos/example-org/example-repo":
			w.Write([]byte(`{"full_name":"example-org/example-repo"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer stub.Close()
	e := entry("g", "", "", "github:example-org/example-repo#1", "github:example-org/example-repo#2",
		"github:example-org/example-repo#3", "github:example-org/example-repo#4", "github:example-org/example-repo#5",
		"github:other-org/other-repo#9")
	// A trailing slash on the base (GitHub Enterprise's `…/api/v3/` shape) must not build `//repos`.
	got := statesOf(Audit([]string{src}, []EntryInput{e}, fakeGit{}, GitHubPRs{Base: stub.URL + "/"}))
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
	if len(calls) != 7 { // 1,2,3,5 once; 4 three times (pulls, issues, then the repository itself)
		t.Fatalf("calls %v", calls)
	}
	for _, c := range calls {
		if strings.HasPrefix(c, "//") {
			t.Fatalf("a trailing-slash base built %q", c)
		}
	}
}

// TestA404FromAnInvisibleRepositoryIsCouldNotLookNotStale: GitHub answers 404 for a PRIVATE repository
// to a caller without access, so a 404 on the PR and the issue is `not-found` ONLY when the repository
// itself answers 200 — otherwise `unchecked` (exit 10), never a false stale (exit 9). RED before the
// repository check: every PR ref in a private scope audited without a token read stale.
func TestA404FromAnInvisibleRepositoryIsCouldNotLookNotStale(t *testing.T) {
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer stub.Close()
	e := entry("g", "", "", "github:example-org/example-repo#4")
	res := Audit([]string{src}, []EntryInput{e}, fakeGit{}, GitHubPRs{Base: stub.URL})
	if s := statesOf(res)["pr example-org/example-repo#4"]; s != PRUnchecked {
		t.Fatalf("a 404 from an invisible repository: %q, want %q", s, PRUnchecked)
	}
}

// TestAnInheritedGitDirNeverRedirectsTheMirror: run from a git hook, `GIT_DIR` is exported, and an
// inherited one overrode `-C <mirror>` — the S5 audit measured the mirror fetch's `--prune` over
// `+refs/heads/*` DELETING the operator's branches while the audit exited 0. The victim's refs must be
// byte-identical before and after, and the audit must still read the real remote.
func TestAnInheritedGitDirNeverRedirectsTheMirror(t *testing.T) {
	w := newWorld(t)
	victim := filepath.Join(t.TempDir(), "victim")
	gitIn(t, filepath.Dir(victim), "init", "--quiet", "-b", "trunk", victim)
	writeFile(t, filepath.Join(victim, "precious.txt"), "wip\n")
	gitIn(t, victim, "add", "-A")
	gitIn(t, victim, "commit", "--quiet", "-m", "precious")
	gitIn(t, victim, "branch", "precious-wip")
	before := gitIn(t, victim, "for-each-ref", "--format=%(refname) %(objectname)")
	t.Setenv("GIT_DIR", filepath.Join(victim, ".git"))
	t.Setenv("GIT_WORK_TREE", victim)
	res := Audit([]string{src}, []EntryInput{entry("g", "- `cmd/x.go` — x\n", "")}, w.git(t), nil)
	os.Unsetenv("GIT_DIR")
	os.Unsetenv("GIT_WORK_TREE")
	if after := gitIn(t, victim, "for-each-ref", "--format=%(refname) %(objectname)"); after != before {
		t.Fatalf("the operator's repository was rewritten:\nbefore %s\nafter  %s", before, after)
	}
	if res.Exit() != ExitClean || statesOf(res)["path cmd/x.go"] != HoldsRoot {
		t.Fatalf("the audit did not read the real remote: exit %d\n%s", res.Exit(), res.Render())
	}
}

// TestAMirrorThatIsNotARepositoryIsAnErrorNotTheEnclosingRepo: a mirror directory that exists but is
// not a repository (an interrupted init) must never fall through to the work tree it sits in.
func TestAMirrorThatIsNotARepositoryIsAnErrorNotTheEnclosingRepo(t *testing.T) {
	w := newWorld(t)
	outer := filepath.Join(t.TempDir(), "outer")
	gitIn(t, filepath.Dir(outer), "init", "--quiet", "-b", "trunk", outer)
	writeFile(t, filepath.Join(outer, "f.txt"), "x\n")
	gitIn(t, outer, "add", "-A")
	gitIn(t, outer, "commit", "--quiet", "-m", "outer")
	before := gitIn(t, outer, "for-each-ref", "--format=%(refname) %(objectname)")
	git := &GitCLI{MirrorRoot: filepath.Join(outer, "mirrors"), Hosts: map[string]string{"github.com": "file://" + w.base}, Timeout: 60 * time.Second}
	if err := os.MkdirAll(git.MirrorDir(src), 0o700); err != nil {
		t.Fatal(err)
	}
	res := Audit([]string{src}, []EntryInput{entry("g", "- `cmd/x.go` — x\n", "")}, git, nil)
	if res.Fetched[0].State != RemoteUnreachable {
		t.Fatalf("an empty mirror directory was used: %+v", res.Fetched)
	}
	if after := gitIn(t, outer, "for-each-ref", "--format=%(refname) %(objectname)"); after != before {
		t.Fatalf("the enclosing repository was rewritten:\nbefore %s\nafter  %s", before, after)
	}
}

// TestTheStrippedEnvironmentCoversGitsOwnLocalList: every variable the INSTALLED git calls
// repository-local is stripped, so a git upgrade that adds one goes red here.
func TestTheStrippedEnvironmentCoversGitsOwnLocalList(t *testing.T) {
	requireGit(t)
	out, err := exec.Command("git", "rev-parse", "--local-env-vars").Output()
	if err != nil {
		t.Fatal(err)
	}
	have := map[string]bool{}
	for _, n := range repoLocalEnv {
		have[n] = true
	}
	names := strings.Fields(string(out))
	if len(names) < 5 {
		t.Fatalf("git listed %d local variables; the probe is not reading git", len(names))
	}
	for _, n := range names {
		if !have[n] {
			t.Errorf("git calls %s repository-local and the auditor does not strip it", n)
		}
	}
}

// TestAStalledRemoteIsBoundedByTheTimeout (T12): a listener that accepts and never answers must cost
// the per-call timeout, not forever — the kill reaches `git-remote-https`, which holds the pipes.
func TestAStalledRemoteIsBoundedByTheTimeout(t *testing.T) {
	requireGit(t)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	held := make(chan net.Conn, 64)
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			held <- c // accept, never speak
		}
	}()
	defer func() {
		for {
			select {
			case c := <-held:
				c.Close()
			default:
				return
			}
		}
	}()
	git := &GitCLI{MirrorRoot: t.TempDir(), Hosts: map[string]string{"github.com": "https://" + ln.Addr().String()}, Timeout: 2 * time.Second}
	start := time.Now()
	done := make(chan error, 1)
	go func() {
		s, _ := codesrc.Parse(src)
		_, err := git.Fetch(s)
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("a stalled remote was fetched")
		}
		if took := time.Since(start); took > 15*time.Second {
			t.Fatalf("the fetch took %s against a 2s timeout", took)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("the fetch did not return within 30s against a 2s timeout: the kill does not reach the transport")
	}
}

// TestCredentialsInAHostBaseAreRefused: a base carrying userinfo would be echoed by every failed
// fetch's error into stdout; credentials come from the credential helper.
func TestCredentialsInAHostBaseAreRefused(t *testing.T) {
	if _, err := ParseHosts("github.com=https://x-access-token:SECRETVALUE@github.com"); err == nil ||
		strings.Contains(err.Error(), "SECRETVALUE") {
		t.Fatalf("a credentialed base: %v", err)
	}
	if _, err := ParseHosts("github.com=https://github.com"); err != nil {
		t.Fatalf("POSITIVE CONTROL: %v", err)
	}
}

// TestTheAPIBaseIsHTTPSOrLoopback: the token must not cross a network in clear.
func TestTheAPIBaseIsHTTPSOrLoopback(t *testing.T) {
	for base, ok := range map[string]bool{
		"https://api.github.com": true, "http://127.0.0.1:8080": true, "http://localhost:9": true,
		"http://api.example.com": false, "ftp://x.example": false,
	} {
		if err := checkAPIBase(base); (err == nil) != ok {
			t.Errorf("%s: %v", base, err)
		}
	}
	out, errOut, code := Execute(Run{SourcesStatus: "sources-declared", Sources: []string{src}, GitHubAPI: "http://api.example.com"})
	if code != ExitUsage || out != "" || !strings.Contains(errOut, EnvGitHubAPI) {
		t.Fatalf("Execute with a cleartext non-loopback API base: %d %q %q", code, out, errOut)
	}
}

// TestAnInterruptKillsTheStalledFetch (#259 r2): `git` runs in its OWN process group so the timeout can
// kill `git-remote-https`, which also means a terminal's Ctrl-C never reaches it. A child process (this
// test binary, re-executed) fetches from a listener that accepts and never answers, with a timeout far
// longer than the test waits; the parent sends the child SIGINT and then watches the HELD CONNECTION.
// The transport closing it is the observable: with no handler the child dies and `git-remote-https`
// keeps the connection open until the parent closes it; with the handler the interrupt reaches the
// group kill. The child must also still die BY the signal (re-raised), not exit as if it refused.
func TestAnInterruptKillsTheStalledFetch(t *testing.T) {
	if addr := os.Getenv("CAIRN_REFAUDIT_INTERRUPT_CHILD"); addr != "" {
		git := &GitCLI{MirrorRoot: os.Getenv("CAIRN_REFAUDIT_INTERRUPT_MIRRORS"),
			Hosts: map[string]string{"github.com": "https://" + addr}, Timeout: 5 * time.Minute}
		s, _ := codesrc.Parse(src)
		_, err := git.Fetch(s)
		t.Fatalf("the child's fetch returned (%v); it should have been interrupted", err)
	}
	requireGit(t)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	held := make(chan net.Conn, 64)
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			held <- c // accept, never speak
		}
	}()
	child := exec.Command(os.Args[0], "-test.run=^TestAnInterruptKillsTheStalledFetch$", "-test.count=1")
	child.Env = append(os.Environ(), "CAIRN_REFAUDIT_INTERRUPT_CHILD="+ln.Addr().String(),
		"CAIRN_REFAUDIT_INTERRUPT_MIRRORS="+t.TempDir())
	var childOut strings.Builder
	child.Stdout, child.Stderr = &childOut, &childOut
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	var conn net.Conn
	select {
	case conn = <-held:
		defer conn.Close() // the RED case's cleanup: closing it is what ends the orphaned transport
	case <-time.After(30 * time.Second):
		child.Process.Kill()
		child.Wait()
		t.Fatalf("the child's fetch never connected\n%s", childOut.String())
	}
	if err := child.Process.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}
	exited := make(chan error, 1)
	go func() { exited <- child.Wait() }()
	select {
	case err := <-exited:
		var exitErr *exec.ExitError
		ws, ok := child.ProcessState.Sys().(syscall.WaitStatus)
		if !errors.As(err, &exitErr) || !ok || !ws.Signaled() || ws.Signal() != syscall.SIGINT {
			t.Errorf("the child did not die by SIGINT (err %v, status %v)\n%s", err, child.ProcessState, childOut.String())
		}
	case <-time.After(15 * time.Second):
		child.Process.Kill()
		t.Fatalf("the child outlived SIGINT by 15s\n%s", childOut.String())
	}
	conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	_, err = conn.Read(make([]byte, 1<<16))
	for err == nil { // a TLS ClientHello may already be buffered; the close is what is awaited
		_, err = conn.Read(make([]byte, 1<<16))
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		t.Fatal("the transport still held the connection 10s after the child was interrupted: Ctrl-C does not reach git's process group")
	}
}
