package refaudit

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ZacxDev/cairn/internal/codesrc"
)

// EnvHosts is the auditing host's map from a source's host to the base URL it is fetched from:
// `host=base[,host=base…]`. Unset is [DefaultHosts]. A host it does not name is NEVER contacted
// (T1): the URL is built from the map's base, never from the declaration.
const EnvHosts = "CAIRN_AUDIT_HOSTS"

// DefaultHosts is the map when $CAIRN_AUDIT_HOSTS is unset (Q9).
const DefaultHosts = "github.com=https://github.com"

// ParseHosts reads a host map. Every base must be an absolute `https://` or `file://` URL — SSH is
// not supported in v1, and nothing else is a transport this auditor allows (T1).
func ParseHosts(raw string) (map[string]string, error) {
	out := map[string]string{}
	for _, pair := range strings.Split(raw, ",") {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		host, base, ok := strings.Cut(pair, "=")
		if !ok || host == "" || base == "" {
			return nil, fmt.Errorf("$%s entry %q is not host=base", EnvHosts, pair)
		}
		u, err := url.Parse(base)
		if err != nil || (u.Scheme != "https" && u.Scheme != "file") {
			return nil, fmt.Errorf("$%s entry %q: the base must be an https:// or file:// URL", EnvHosts, pair)
		}
		out[strings.ToLower(host)] = strings.TrimRight(base, "/")
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("$%s names no host", EnvHosts)
	}
	return out, nil
}

// GitCLI is the real [GitReader]: a full mirror per source under `MirrorRoot`, fetched with
// `git fetch --prune --no-tags` over `+refs/heads/*` and `+refs/pull/*/head` (decision 8).
//
// 🔴 EVERY CALL RUNS WITH `GIT_TERMINAL_PROMPT=0`, `GIT_CONFIG_NOSYSTEM=1`, `GIT_LITERAL_PATHSPECS=1`,
// `GIT_NO_LAZY_FETCH=1` and `GIT_ALLOW_PROTOCOL` set to the map's schemes, and passes `--end-of-options`
// or `--` before any operand (T2, T3). The mirror's directory is a DIGEST of the canonical source,
// never a path built from it (T4).
type GitCLI struct {
	MirrorRoot string
	Hosts      map[string]string
	Timeout    time.Duration
}

// MirrorDir is where a source's mirror lives: `<root>/<sha256(canonical)>.git`.
func (g *GitCLI) MirrorDir(canonical string) string {
	sum := sha256.Sum256([]byte(canonical))
	return filepath.Join(g.MirrorRoot, hex.EncodeToString(sum[:])+".git")
}

func (g *GitCLI) allowProtocol() string {
	seen := map[string]bool{}
	for _, base := range g.Hosts {
		if u, err := url.Parse(base); err == nil {
			seen[u.Scheme] = true
		}
	}
	var out []string
	for s := range seen {
		out = append(out, s)
	}
	sort.Strings(out)
	return strings.Join(out, ":")
}

// run is the package's ONE `git` call site.
func (g *GitCLI) run(dir string, args ...string) (string, int, error) {
	timeout := g.Timeout
	if timeout <= 0 {
		timeout = 120 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	argv := args
	if dir != "" {
		argv = append([]string{"-C", dir}, args...)
	}
	cmd := exec.CommandContext(ctx, "git", argv...)
	cmd.Env = append(os.Environ(),
		"GIT_TERMINAL_PROMPT=0", "GIT_CONFIG_NOSYSTEM=1", "GIT_LITERAL_PATHSPECS=1",
		"GIT_NO_LAZY_FETCH=1", "GIT_ALLOW_PROTOCOL="+g.allowProtocol())
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	if err != nil {
		code := -1
		if exitErr, ok := err.(*exec.ExitError); ok {
			code = exitErr.ExitCode()
		}
		return stdout.String(), code, fmt.Errorf("git %s: exit %d: %s", strings.Join(args, " "), code, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), 0, nil
}

// Fetch implements [GitReader].
func (g *GitCLI) Fetch(src codesrc.Source) (string, error) {
	base, ok := g.Hosts[src.Host]
	if !ok {
		return "", &HostNotMappedError{Host: src.Host}
	}
	canon := src.Canonical()
	dir := g.MirrorDir(canon)
	if _, err := os.Stat(dir); err != nil {
		if err := os.MkdirAll(g.MirrorRoot, 0o700); err != nil {
			return "", err
		}
		if _, _, err := g.run("", "init", "--bare", "--quiet", "--end-of-options", dir); err != nil {
			return "", err
		}
	}
	remote := base + "/" + src.RepoPath
	if _, _, err := g.run(dir, "fetch", "--prune", "--no-tags", "--quiet", "--end-of-options", remote,
		"+refs/heads/*:refs/heads/*", "+refs/pull/*/head:refs/pull/*/head"); err != nil {
		return "", err
	}
	out, _, err := g.run(dir, "rev-parse", "--verify", "--quiet", "--end-of-options", "refs/heads/"+src.Branch+"^{commit}")
	if err != nil {
		return "", fmt.Errorf("the declared branch %q is not on the remote", src.Branch)
	}
	return strings.TrimSpace(out), nil
}

// HasPath implements [GitReader]: `ls-tree -z`, and the path is present when a NUL-terminated
// record's path field EQUALS it (a directory's `tree` record counts).
func (g *GitCLI) HasPath(src string, commit, path string) (bool, error) {
	out, _, err := g.run(g.MirrorDir(src), "ls-tree", "-z", commit, "--", path)
	if err != nil {
		return false, err
	}
	for _, rec := range strings.Split(out, "\x00") {
		if _, name, ok := strings.Cut(rec, "\t"); ok && name == path {
			return true, nil
		}
	}
	return false, nil
}

// Commits implements [GitReader].
func (g *GitCLI) Commits(src string, prefix string) ([]string, error) {
	dir := g.MirrorDir(src)
	out, code, err := g.run(dir, "rev-parse", "--disambiguate="+prefix)
	if err != nil && code != 128 {
		return nil, err
	}
	var commits []string
	for _, obj := range strings.Fields(out) {
		kind, _, err := g.run(dir, "cat-file", "-t", obj)
		if err == nil && strings.TrimSpace(kind) == "commit" {
			commits = append(commits, obj)
		}
	}
	return commits, nil
}

// ContainedBy implements [GitReader]. 🔴 THE PATTERNS RESTRICT THE ANSWER TO THE TWO NAMESPACES THE
// FETCH WROTE, so a tag — kept by an older mirror, or auto-followed by a build without `--no-tags` —
// can never make a SHA reachable (round 3 🟡1).
func (g *GitCLI) ContainedBy(src string, sha string) ([]string, error) {
	out, _, err := g.run(g.MirrorDir(src), "for-each-ref", "--format=%(refname)", "--contains", sha,
		"refs/heads/", "refs/pull/")
	if err != nil {
		return nil, err
	}
	return strings.Fields(out), nil
}

// Grep implements [GitReader]: exit 0 found, exit 1 not found, anything else an error.
func (g *GitCLI) Grep(src string, commit, path, symbol string) (bool, error) {
	_, code, err := g.run(g.MirrorDir(src), "grep", "-q", "-w", "-F", "-e", symbol, commit, "--", path)
	switch {
	case err == nil:
		return true, nil
	case code == 1:
		return false, nil
	default:
		return false, err
	}
}
