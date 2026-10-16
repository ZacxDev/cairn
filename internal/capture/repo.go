package capture

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"

	"github.com/ZacxDev/cairn/internal/client"
)

// Repo is what a session's working directory says about the repository it ran in: the store
// scope the client would derive there, and the OWNER segment of its `origin` remote. The zero
// value is "no repository known", which [Decide] reads as the default instance.
//
// 🔴 THE OWNER, NEVER THE URL. A remote URL can carry a credential (`https://<token>@host/…`), and
// this struct is persisted in the state file; only the one path segment routing needs is kept.
type Repo struct {
	Scope string `json:"scope,omitempty"`
	Owner string `json:"owner,omitempty"`
}

// OwnerRoutes is `-route-origin-owner OWNER=ALIAS`, keyed by the owner FOLDED TO LOWER CASE.
//
// Case-insensitive because the hosts that have owners treat them so: `git@host:Acme/x` and
// `git@host:acme/x` are one organisation, and an exact match would route the two differently.
// Nothing else is loosened — no host is compared, no prefix, suffix or pattern.
type OwnerRoutes map[string]string

// AliasFor is the alias `owner` is routed to, if any. An empty owner matches nothing.
func (o OwnerRoutes) AliasFor(owner string) (string, bool) {
	if owner == "" {
		return "", false
	}
	alias, ok := o[strings.ToLower(owner)]
	return alias, ok
}

// ParseOwnerRoutes reads every `-route-origin-owner` value against this host's routing.
//
// 🔴 AN ALIAS THE ROUTING DOES NOT CONFIGURE IS A USAGE ERROR, as it is for `-worker`: the rule
// would bind sessions to an instance nothing on this host can reach, and every one of them would
// then wait in `withdrawal-pending` or be refused on every run. An owner named twice is refused
// too, whatever the two aliases: which line wins is not a question a unit file should leave open.
func ParseOwnerRoutes(values []string, routing client.Routing) (OwnerRoutes, error) {
	known := map[string]bool{}
	for _, a := range routing.Aliases() {
		known[a] = true
	}
	out := OwnerRoutes{}
	for _, v := range values {
		owner, alias, ok := strings.Cut(v, "=")
		if !ok || owner == "" || strings.ContainsAny(owner, "/:@ \t\r\n") || !client.ValidAlias(alias) {
			return nil, fmt.Errorf("-route-origin-owner %q is not OWNER=ALIAS — the owner segment of an origin URL "+
				"(no host, slash or colon) and a configured instance alias", v)
		}
		if !known[alias] {
			return nil, fmt.Errorf("-route-origin-owner %s names instance %s, which this host's routing does not configure (%s)",
				v, alias, strings.Join(routing.Aliases(), ", "))
		}
		key := strings.ToLower(owner)
		if _, dup := out[key]; dup {
			return nil, fmt.Errorf("-route-origin-owner names owner %s twice", owner)
		}
		out[key] = alias
	}
	return out, nil
}

// OriginOwner is the OWNER segment of a git remote URL — the first path segment, when the path
// has at least `owner/repo` — or "" when the URL has no such shape. Three spellings:
//
//	git@host:owner/repo(.git)            scp-like: a colon before any slash
//	https://host/owner/repo(.git)        and http, git
//	ssh://[user@]host[:port]/owner/repo  and git+ssh, ssh+git
//
// A local path, a `file://` URL, a `~user` path, or a path with a single segment has no owner. A
// nested group path (`group/sub/repo`) answers its first segment, `group`.
func OriginOwner(raw string) string {
	s := strings.TrimSpace(raw)
	var path string
	if scheme, _, ok := strings.Cut(s, "://"); ok {
		switch strings.ToLower(scheme) {
		case "https", "http", "ssh", "git", "git+ssh", "ssh+git":
		default:
			return ""
		}
		u, err := url.Parse(s)
		if err != nil || u.Host == "" {
			return ""
		}
		path = u.Path
	} else {
		// git's own rule for the scp-like form: a colon BEFORE the first slash. Anything else
		// without a scheme is a local path.
		colon, slash := strings.IndexByte(s, ':'), strings.IndexByte(s, '/')
		if colon <= 0 || (slash >= 0 && slash < colon) {
			return ""
		}
		path = s[colon+1:]
	}
	segs := strings.Split(strings.Trim(path, "/"), "/")
	if len(segs) < 2 || segs[0] == "" || segs[len(segs)-1] == "" {
		return ""
	}
	owner := segs[0]
	if owner == "." || owner == ".." || strings.HasPrefix(owner, "~") {
		return ""
	}
	return owner
}

// deriveRepo asks git about `dir`, the way the client does ([client.ScopeForRepo],
// [client.OriginURL] — bare `git` by name). `settled` reports whether the answer may be KEPT:
//
//   - a repository: its scope, and its origin's owner ("" when it has no `origin`) — settled;
//   - a directory that is gone, or that git answered is not a repository — the zero [Repo], settled;
//   - a git that could not be RUN at all (not on PATH): the zero Repo, NOT settled — asked again
//     next run, so a timer started with a thin PATH does not pin every session to the default.
//
// 🔴 WHY A SETTLED ANSWER IS KEPT RATHER THAN RE-ASKED EVERY RUN. Worktrees are removed routinely
// once their work merges. Re-deriving each run would turn "the worktree is gone" into "no repo",
// and a session placed on the second instance while its worktree existed would be WITHDRAWN and
// moved back to the default the run after the worktree was deleted — the misroute this exists to
// end, reintroduced on a delay. ⚠ The cost: a repository whose `origin` changes AFTER a session's
// first routed pass keeps the owner it had then.
func deriveRepo(dir string) (repo Repo, settled bool) {
	if dir == "" {
		return Repo{}, false
	}
	scope, err := client.ScopeForRepo(dir)
	if err != nil {
		return Repo{}, !gitDidNotRun(err)
	}
	u, err := client.OriginURL(dir)
	if err != nil {
		if gitDidNotRun(err) {
			return Repo{}, false
		}
		return Repo{Scope: scope}, true
	}
	return Repo{Scope: scope, Owner: OriginOwner(u)}, true
}

// gitDidNotRun is a [client.GitError] whose process never ran (Code -1): no answer about the
// directory at all.
func gitDidNotRun(err error) bool {
	var ge *client.GitError
	return errors.As(err, &ge) && ge.Code < 0
}

// sessionRepo is the [Repo] [Decide] places `ss` by — the zero Repo whenever V is non-empty, since
// Decide does not read it then and asking git would cost a process per session for nothing.
//
// The answer is derived ONCE per session and kept in the state (`repo`) when [deriveRepo] says it
// is settled; one run asks git at most once per directory (`repoCache`).
func (a *Agent) sessionRepo(ss *SessionState) Repo {
	if len(ss.V) > 0 {
		return Repo{}
	}
	if ss.Repo != nil {
		return *ss.Repo
	}
	if ss.Dir == "" {
		return Repo{}
	}
	if a.repoCache == nil {
		a.repoCache = map[string]repoAnswer{}
	}
	ans, ok := a.repoCache[ss.Dir]
	if !ok {
		ans.repo, ans.settled = deriveRepo(ss.Dir)
		a.repoCache[ss.Dir] = ans
	}
	if ans.settled {
		r := ans.repo
		ss.Repo = &r
	}
	return ans.repo
}

type repoAnswer struct {
	repo    Repo
	settled bool
}

// cwdScanBytes bounds how much of a Claude Code main stream is read looking for its `cwd`.
// Measured on one host: 300 of 300 sampled transcripts carried one within their first 64 KiB. A
// stream with none in the bound records no directory, and is looked at again on the next run.
const cwdScanBytes = 1 << 20

// claudeCwd is the `cwd` of the FIRST record of a Claude Code JSONL stream that carries one — the
// directory the session was started in — or "" when none appears in the first [cwdScanBytes]. A
// session that later `cd`s elsewhere is still placed by where it started.
//
// Opened through [openStream], so a test counting which session files a run opens sees this read.
func claudeCwd(path string) string {
	f, err := openStream(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	sc := bufio.NewScanner(io.LimitReader(f, cwdScanBytes))
	sc.Buffer(make([]byte, 0, 64*1024), cwdScanBytes)
	for sc.Scan() {
		var rec struct {
			Cwd string `json:"cwd"`
		}
		if json.Unmarshal(sc.Bytes(), &rec) == nil && rec.Cwd != "" {
			return rec.Cwd
		}
	}
	return ""
}
