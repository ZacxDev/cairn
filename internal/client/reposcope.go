package client

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/ZacxDev/cairn/internal/store"
)

// 🔴 ONE SCOPE-DERIVATION RULE, ONE PLACE. The READ half and any WRITE half need exactly the
// same answer; a reader and a writer that disagree here send two operators to two different
// remedies for one mistake, and put a bullet in a scope nobody recalls.

// GitError is a failed `git` invocation, carrying the command line and the stderr.
type GitError struct {
	Argv   []string
	Code   int
	Stderr string
}

func (e *GitError) Error() string {
	detail := e.Stderr
	if detail == "" {
		detail = "(no stderr)"
	}
	return fmt.Sprintf("git command failed (%s): exit %d: %s",
		strings.Join(e.Argv, " "), e.Code, detail)
}

// RepoPathMissingError is "that `--repo` value is not a directory", checked BEFORE git runs.
type RepoPathMissingError struct{ message string }

func (e *RepoPathMissingError) Error() string { return e.message }

// runGit is the ONE `git` call site.
//
// 🔴 `GIT_OPTIONAL_LOCKS=0`: a read-only invocation must not take the index lock. Another
// process in the same checkout is a normal case, and a helper that can block someone else's
// commit is not read-only in the way that matters.
func runGit(repo string, args ...string) (string, error) {
	argv := append([]string{"git", "-C", repo}, args...)
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0")
	var stdout, stderr strings.Builder
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	if err != nil {
		code := -1
		if exitErr, ok := err.(*exec.ExitError); ok {
			code = exitErr.ExitCode()
		}
		return "", &GitError{Argv: argv, Code: code, Stderr: strings.TrimSpace(stderr.String())}
	}
	return stdout.String(), nil
}

// DeriveScope is the store scope for a repo, normalized. WORKTREE-STABLE.
//
// `gitCommonDir` is `git rev-parse --path-format=absolute --git-common-dir`: for BOTH a base
// clone and any worktree of it, that is the base clone's `.git`, so its parent is the repo
// everyone means. `--show-toplevel` is deliberately not used for this, because in a worktree
// it is the worktree's own directory — which would give one repo two scopes.
//
// Fallback: when the common dir is not literally named `.git` — a bare repo, or a submodule
// whose common dir is `<super>/.git/modules/<name>` — the parent basename would be
// meaningless (`modules`), so the repo root's basename is used instead. Stated because the
// fallback is otherwise silent.
func DeriveScope(repoRoot, gitCommonDir string) string {
	if filepath.Base(gitCommonDir) == ".git" {
		return store.NormalizeRef(filepath.Base(filepath.Dir(gitCommonDir)))
	}
	return store.NormalizeRef(filepath.Base(repoRoot))
}

// ScopeForRepo asks git where `repo` really lives, then derives the scope. RUNS GIT.
//
// 🔴 THE NON-DIRECTORY CASE IS CHECKED BEFORE GIT RUNS, and it belongs here for the same
// reason the scope rule does: this is the one seam both halves cross, so a guard placed in the
// reader alone would leave the writer answering the identical mistake with an identical git
// dump.
//
// ⚠ THE TWO GIT CALLS RUN IN THE ORACLE'S ORDER — common dir first, toplevel second — and the
// order is observable: in a directory that is not a git repository BOTH fail, and which
// command the refusal names is decided here. The oracle evaluates `common` on its own line
// and `_toplevel(repo)` as an argument, so the common-dir failure is the one a caller sees.
func ScopeForRepo(repo string) (string, error) {
	info, err := os.Stat(repo)
	if err != nil || !info.IsDir() {
		return "", &RepoPathMissingError{message: RepoPathMissingMessage(repo)}
	}
	common, err := runGit(repo, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return "", err
	}
	top, err := runGit(repo, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", err
	}
	return DeriveScope(strings.TrimSpace(top), strings.TrimSpace(common)), nil
}

// RepoPathMissingMessage is the ONE spelling of "that `--repo` value is not a directory".
//
// 🔴 TWO LEADS, BECAUSE THEY ARE TWO DIFFERENT MISTAKES. A path that is ABSENT and a path
// that exists as a FILE need different next moves, and telling someone their `notes.md` "does
// not exist" while they are looking at it is the kind of confidently-wrong line that makes a
// reader distrust the rest of the message.
//
// ⚠ THE `given` → `resolved` ARM OF THE ORACLE'S MESSAGE IS NOT REACHABLE FROM THIS CLIENT
// AND IS THEREFORE NOT PORTED. There, `given` carries the caller's RAW string so a cwd-join
// is visible; `cairn` itself always calls `scope_for_repo(args.repo)` with no `given`, so
// `given_s == resolved_s` on every invocation and the other two arms are dead. Porting them
// would be two untested branches asserting a behaviour no verb can produce.
func RepoPathMissingMessage(path string) string {
	lead := "repo path is not a directory"
	if _, err := os.Stat(path); err != nil {
		lead = "repo path does not exist"
	}
	return fmt.Sprintf("%s: %s. --repo takes a PATH, not a repo NAME. Pass an absolute "+
		"path, or --scope <name>, which names the store directory directly and runs no "+
		"git at all.", lead, store.PyRepr(path))
}

// ResolveScope is `args.scope or scope_for_repo(args.repo)`, plus the one refusal sentence
// both the read and the write halves print. It returns the scope, or "" having already put
// the reason on stderr.
//
// 🔴 A GIT FAILURE IS A USAGE ERROR HERE, NOT A CRASH. `cairn recall` in a plain directory
// used to exit 1 with a traceback, which made the usage path unreachable for the commonest way
// to hit it.
func ResolveScope(scope, repo string, stderr *os.File) string {
	if scope != "" {
		return scope
	}
	derived, err := ScopeForRepo(repo)
	if err != nil {
		fmt.Fprintf(stderr, "cairn: could not derive a scope from %s: %s\n"+
			"       pass --scope explicitly.\n", store.PyRepr(repo), err)
		return ""
	}
	if derived == "" {
		fmt.Fprintln(stderr, "cairn: --scope is required (no scope could be derived)")
		return ""
	}
	return derived
}
