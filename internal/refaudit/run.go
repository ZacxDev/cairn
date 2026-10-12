package refaudit

import (
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/ZacxDev/cairn/internal/report"
)

// Run is one `cairn audit-refs` invocation as plain values: everything the CLI read (the pod's
// sources answer, the cached entries, the auditing host's configuration), and nothing it did.
//
// 🔴 THE CLI DOES NOT IMPORT THIS PACKAGE, AND THAT IS THE WHOLE REASON THIS TYPE EXISTS. `internal/ui`
// imports `internal/client`, so anything `internal/client` imports is linked into the internet-facing
// browser surface — and the auditor, which spawns `git` and fetches remotes, must never be (clause e of
// the plan's closing condition; `internal/depspolicy`'s ban). So `internal/client` gathers these values
// into its own request type, and `cmd/cairn` — the one binary that may hold both — hands them here.
type Run struct {
	// SourcesStatus is the pod's `X-Store-Status`-equivalent read off its body (`report.Status*`), or
	// "" when the read itself failed — SourcesDetail then says why.
	SourcesStatus string
	SourcesDetail string
	JournalAbsent bool
	Sources       []string
	Entries       []EntryInput
	// EntriesDetail is non-empty when the cache could not be read at all.
	EntriesDetail string
	// Hosts is the raw host map ("" is [DefaultHosts]); GitHubAPI "" is [DefaultGitHubAPI].
	Hosts, GitHubAPI, Token, MirrorRoot string
	Timeout                             time.Duration
	JSON                                bool
	// Label is the instance label on a multi-instance host, "" otherwise.
	Label string
}

// ExitUsage is a host map that does not parse: the command line (here, its environment) must change.
const ExitUsage = 2

// Execute runs the audit and renders it: stdout, stderr, and doctor's 0/9/10 (or 2 for a host map
// that does not parse, found before any network).
func Execute(r Run) (stdout, stderr string, code int) {
	hostsRaw := r.Hosts
	if hostsRaw == "" {
		hostsRaw = DefaultHosts
	}
	hosts, err := ParseHosts(hostsRaw)
	if err != nil {
		return "", "cairn: audit-refs: " + err.Error() + "\n", ExitUsage
	}
	var res Result
	detail := ""
	switch r.SourcesStatus {
	case report.StatusSourcesDeclared:
		if r.EntriesDetail != "" {
			// Nothing could be read, so nothing was checked: `checked=0`, never a clean 0 over an empty
			// cache and never the plain read's 3.
			res, detail = Result{Sources: r.Sources}, r.EntriesDetail
			break
		}
		api := r.GitHubAPI
		if api == "" {
			api = DefaultGitHubAPI
		}
		if err := checkAPIBase(api); err != nil {
			return "", "cairn: audit-refs: " + err.Error() + "\n", ExitUsage
		}
		git := &GitCLI{MirrorRoot: r.MirrorRoot, Hosts: hosts, Timeout: r.Timeout}
		res = Audit(r.Sources, r.Entries, git, GitHubPRs{Base: api, Token: r.Token})
	case report.StatusSourcesUnconfigured:
		res = Result{ScopeReason: ReasonUnconfigured}
	case "":
		// The pod did not answer 200 — including its 503 for an unreadable journal: could not look.
		res, detail = Result{ScopeReason: ReasonUnreadable}, r.SourcesDetail
	default: // sources-undeclared, and scope-absent (a scope this credential cannot read reads the same)
		res, detail = Result{ScopeReason: ReasonUndeclared}, "status="+r.SourcesStatus
		if r.JournalAbsent {
			detail += " journal=absent"
		}
	}

	var out strings.Builder
	if r.Label != "" {
		fmt.Fprintf(&out, "cairn: instance=%s\n", r.Label)
	}
	if r.JSON {
		checked, stale, unchecked := res.Counts()
		raw, err := json.Marshal(map[string]any{
			"sources": res.Sources, "checked": checked, "stale": stale, "unchecked": unchecked,
			"reason": res.Reason(), "exit": res.Exit(), "fetched": res.Fetched, "findings": res.Findings,
		})
		if err != nil {
			return "", "cairn: audit-refs: " + err.Error() + "\n", ExitCouldNotLook
		}
		out.Write(raw)
		out.WriteString("\n")
	} else {
		out.WriteString(res.Render())
	}
	if detail != "" {
		stderr = "🔴 cairn: audit-refs could NOT look — " + detail + "\n"
	}
	return out.String(), stderr, res.Exit()
}

// checkAPIBase refuses an API base the token could cross a network over in clear: https anywhere,
// http only to a loopback host (the e2e stub's shape).
func checkAPIBase(base string) error {
	u, err := url.Parse(base)
	if err != nil {
		return fmt.Errorf("$%s %q is not a URL", EnvGitHubAPI, base)
	}
	switch {
	case u.Scheme == "https" && u.Host != "":
		return nil
	case u.Scheme == "http":
		if ip := net.ParseIP(u.Hostname()); (ip != nil && ip.IsLoopback()) || u.Hostname() == "localhost" {
			return nil
		}
	}
	return fmt.Errorf("$%s %q must be https:// (http:// only to a loopback host)", EnvGitHubAPI, base)
}
