package client

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/ZacxDev/cairn/internal/report"
	"github.com/ZacxDev/cairn/internal/store"
)

// THE `audit-refs` VERB — S5 of `claudedocs/plan-cairn-scope-refs.md` (decisions 6–9): check every
// entry's checkable claims against the scope's declared code sources, on THIS host, with THIS host's
// credentials. Go-only (the `audit-refs` row of `capability_ledger.LEDGER`), read-only: it writes only
// its own mirror cache.
//
// 🔴 THIS FILE GATHERS; `internal/refaudit` AUDITS; `cmd/cairn` JOINS THEM — AND `internal/client`
// NEVER IMPORTS `internal/refaudit`. `internal/ui` imports this package (the agent tab's recall
// preamble), so anything imported here is linked into the internet-facing browser surface, and the
// auditor spawns `git` and fetches remotes. So this verb builds an [AuditRequest] of plain values and
// hands it to the runner `cmd/cairn` installs ([SetAuditRunner]); `internal/depspolicy`'s ban holds
// `internal/refaudit` out of `cmd/cairn-ui` and `cmd/cairn-server`.
//
// 🔴 DOCTOR'S 0/9/10, NO NEW CODE (decision 8): 9 a claim MEASURED stale; 10 a claim, a source or the
// scope COULD NOT be looked at — including an undeclared scope, an unconfigured or unreadable journal,
// and a run that checked nothing; 0 only when `checked > 0` and nothing is stale or unlooked. 2 is
// usage (a host map that does not parse); an unrouted scope is the routing contract's 11. "The pod did
// not answer the sources read" is 10, never the plain read's 3 — `arcsCheck`'s ruling.

// AuditEntry is one cached entry as the auditor reads it.
type AuditEntry struct {
	Ref  string
	Text string
	Refs []store.TaskRef
}

// AuditRequest is everything the verb read, as plain values — `refaudit.Run`'s fields, gathered here.
type AuditRequest struct {
	SourcesStatus string
	SourcesDetail string
	JournalAbsent bool
	Sources       []string
	Entries       []AuditEntry
	EntriesDetail string
	Hosts         string
	GitHubAPI     string
	Token         string
	MirrorRoot    string
	Timeout       time.Duration
	JSON          bool
	Label         string
}

// AuditRunner renders a request: stdout, stderr and the exit code.
type AuditRunner func(AuditRequest) (stdout, stderr string, code int)

var auditRunner AuditRunner

// SetAuditRunner installs the auditor. `cmd/cairn` calls it once, before `Run`; a binary that never
// does answers `audit-refs` with a refusal naming the omission rather than a silent zero.
func SetAuditRunner(r AuditRunner) { auditRunner = r }

// errAuditorNotLinked is the binary forgetting `SetAuditRunner`.
var errAuditorNotLinked = errors.New("audit-refs: this binary was built without the auditor (cmd/cairn installs it with client.SetAuditRunner)")

// auditMirrorRoot is `$XDG_CACHE_HOME/cairn/mirrors`, `~/.cache` when unset.
func auditMirrorRoot() (string, error) {
	base := getenv(CacheHomeEnv)
	if base == "" {
		home, err := homeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, ".cache")
	}
	return filepath.Join(base, "cairn", "mirrors"), nil
}

// AuditRefs is `cairn audit-refs --scope X` (or `--repo P`) [--json] [--no-sync].
func AuditRefs(env Env, opts Options) (int, error) {
	if auditRunner == nil {
		return 0, errAuditorNotLinked
	}
	scope := ResolveScope(opts.Scope, opts.Repo, env.Stderr)
	if scope == "" {
		return ExitUsage, nil
	}
	mirrors, err := auditMirrorRoot()
	if err != nil {
		return 0, err
	}
	req := AuditRequest{
		Hosts: getenv(AuditHostsEnv), GitHubAPI: getenv(AuditGitHubAPIEnv), MirrorRoot: mirrors,
		Timeout: time.Duration(max(opts.Timeout, 30)) * time.Second, JSON: opts.JSON,
	}
	if req.Token = getenv(GHTokenEnv); req.Token == "" {
		req.Token = getenv(GitHubTokenEnv)
	}
	alias, label, cfg, err := podFor(scope)
	if err != nil {
		var unrouted *UnroutedScope
		var badConfig *RoutingConfigError
		if errors.As(err, &unrouted) || errors.As(err, &badConfig) {
			return 0, err
		}
		req.SourcesDetail = err.Error()
		return finishAudit(env, req)
	}
	req.Label = label
	_, body, err := FetchReport(cfg, "/api/v1/sources/"+quoteAll(scope), opts.Timeout)
	if err != nil {
		req.SourcesDetail = err.Error()
		return finishAudit(env, req)
	}
	parsed, err := report.ParseSourcesBody(string(body))
	if err != nil {
		req.SourcesDetail = err.Error()
		return finishAudit(env, req)
	}
	req.SourcesStatus, req.JournalAbsent, req.Sources = parsed.Status, parsed.JournalAbsent, parsed.Sources
	if parsed.Status == report.StatusSourcesDeclared {
		req.Entries, req.EntriesDetail, err = auditEntries(opts, alias, scope, env)
		if err != nil {
			return 0, err
		}
	}
	return finishAudit(env, req)
}

func finishAudit(env Env, req AuditRequest) (int, error) {
	stdout, stderr, code := auditRunner(req)
	fmt.Fprint(env.Stdout, stdout)
	fmt.Fprint(env.Stderr, stderr)
	return code, nil
}

// auditEntries reads the scope's entries out of the synced cache — `recall`'s read: a sync first
// (unless `--no-sync`), then the cache, never the network for entry text. A cache that cannot be read
// at all is a DETAIL (nothing could be checked), not an error.
func auditEntries(opts Options, alias, scope string, env Env) ([]AuditEntry, string, error) {
	cache, err := instanceCache(opts, alias)
	if err != nil {
		return nil, "", err
	}
	state, err := ResolveState(cache, opts.NoSync, scope, opts.Timeout, alias)
	if err != nil {
		return nil, "", err
	}
	fmt.Fprintln(env.Stderr, BannerNamed(state.Name, state.Detail, ""))
	if state.ExitHint != 0 {
		return nil, state.Name + ": " + state.Detail, nil
	}
	index, err := store.LoadStore(cache, "audited", store.Unrestricted())
	if err != nil {
		return nil, "", err
	}
	key := store.NormalizeRef(scope)
	if !index.HasScope(key) {
		return nil, "", nil
	}
	list, err := index.Entries(key)
	if err != nil {
		return nil, "", err
	}
	var out []AuditEntry
	for _, e := range list {
		path := filepath.Join(cache, e.Scope, e.Filename)
		text, err := os.ReadFile(path)
		if err != nil {
			return nil, "", store.EntryUnreadable(path, err)
		}
		out = append(out, AuditEntry{Ref: e.Ref(), Text: string(text), Refs: e.Tasks})
	}
	return out, "", nil
}
