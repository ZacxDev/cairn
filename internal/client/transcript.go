package client

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/ZacxDev/cairn/internal/store"
	"github.com/ZacxDev/cairn/internal/transcriptwire"
	"github.com/ZacxDev/cairn/internal/write"
)

// THE `transcript` VERB — S8 of `claudedocs/plan-cairn-plugins.md` (decision 17; Q15 adopted under
// O13), Go-only and declared (the `transcript` row of `tests/testlib/capability_ledger.LEDGER`,
// `flake.nix`'s `want-go-only-verbs.txt`, residual 11 of `tests/parity/README.md`).
//
//	cairn transcript skeleton --session ID
//	cairn transcript records  --session ID [--stream S] [--from-seq N] [--to-seq N] [--class C]
//	                          [--role R] [--tool T] [--view conversation|raw] [--max-bytes N]
//	cairn transcript tool     --session ID --seq N [--stream S] [--id CALL] [--range A-B]
//
// 🔴 ONE VERB WITH THE ACTION AS ITS POSITIONAL, NOT THREE FLAT VERBS — the plan's spelling, and the
// flat-verb ruling (`Verbs`'s arc comment) does not bite: that ruling exists because `Verb.Writes`
// is one bit per verb, and all three actions READ. `search`'s query is the parser's precedent for a
// positional.
//
// 🔴 IT TALKS TO `cairn-ui`'S CLIENT LISTENER, NEVER TO THE POD — the memo verbs' `CAIRN_UI_URL` and
// `CAIRN_UI_TOKEN`, read through `LoadUIConfigFor`: the agent's OWN credential, so what it reads is
// exactly what decision 4's predicate admits for that (narrowed) credential. Scope mail's Q3 put the
// agent read routes on that listener so an agent holds one URL; the browser surface serves the same
// three answers under `/transcript/…`.
//
// 🔴 `--scope`/`--repo` ONLY PICK THE INSTANCE — a session id is global and decides nothing about
// scopes. With one instance configured neither is needed; with several, the scope routes exactly as
// every verb routes it (exit 11 when unrouted), and a working directory with no scope is a usage
// error naming `--scope`, never a guess.
//
// 🔴 IT PRINTS THE LISTENER'S JSON VERBATIM ON STDOUT AND NOTHING ELSE THERE: an agent parses it, so
// even the multi-instance label goes to stderr. There is one renderer of these answers —
// `internal/transcript`'s — and this verb is not a second one.
//
// 🔴 A SERVED CALL RECORDS `*` IN THE READ LEDGER (decision 3a): it prints ANOTHER session's
// transcript, whose visibility set may hold any scope, so the reading session becomes owner-only. The
// plan states that cost; folding the read session's `V` in instead was not chosen.
//
// Exits: 0 served; 2 usage — including the listener's 400 (a malformed parameter: the command line
// must change); 3 nothing was read — unreachable, unconfigured, the credential refused, or the one
// `not-found` (no transcript, one this credential cannot see, a stream, record or call not there: ONE
// answer, T10); 5 any other answer; 11 unrouted. No new exit code.

// transcriptActions is the closed action set, in the plan's order.
var transcriptActions = []string{transcriptwire.RouteSkeleton, transcriptwire.RouteRecords, transcriptwire.RouteTool}

// transcriptFlags maps each `transcript` flag to the query parameter it becomes. `--session` and
// `--id` are shared flags (the parser's own arms); every other one is collected here.
var transcriptFlags = map[string]string{
	"--stream":    transcriptwire.ParamStream,
	"--from-seq":  transcriptwire.ParamFromSeq,
	"--to-seq":    transcriptwire.ParamToSeq,
	"--class":     transcriptwire.ParamClass,
	"--role":      transcriptwire.ParamRole,
	"--tool":      transcriptwire.ParamTool,
	"--view":      transcriptwire.ParamView,
	"--max-bytes": transcriptwire.ParamMaxBytes,
	"--seq":       transcriptwire.ParamSeq,
	"--range":     transcriptwire.ParamRange,
}

// transcriptActionFlags is which of [transcriptFlags] (and `--id`) each action takes; a flag another
// action takes is refused at exit 2 rather than silently ignored.
var transcriptActionFlags = map[string][]string{
	transcriptwire.RouteSkeleton: nil,
	transcriptwire.RouteRecords:  {"--stream", "--from-seq", "--to-seq", "--class", "--role", "--tool", "--view", "--max-bytes"},
	transcriptwire.RouteTool:     {"--stream", "--seq", "--id", "--range"},
}

// transcriptVerbFlags is the verb's whole flag set, for the verb table.
func transcriptVerbFlags() []string {
	out := []string{"--session", "--scope", "--repo"}
	for _, a := range transcriptActions {
		for _, f := range transcriptActionFlags[a] {
			if !slices.Contains(out, f) {
				out = append(out, f)
			}
		}
	}
	return out
}

// transcriptNotes is `transcript --help`'s statement of the actions and of what a read costs.
const transcriptNotes = "Actions (the one positional):\n" +
	"  skeleton  the stream tree, sizes by class and a turn index — no content beyond tool names\n" +
	"  records   a bounded page of one stream's records (--view conversation by default: bookkeeping and\n" +
	"            duplicate tool-result copies are left out and named; --view raw is the stored bytes)\n" +
	"  tool      one tool call with its result and duplicate, cut to --range A-B (bytes)\n" +
	"\n" +
	"It asks cairn-ui's client listener ($CAIRN_UI_URL) with your own credential ($CAIRN_UI_TOKEN), so\n" +
	"you read exactly the sessions you may see. --scope/--repo only pick the instance. A served read\n" +
	"records `*` in this session's read ledger: the session reading a transcript becomes owner-only."

// maxTranscriptAnswer bounds one answer's body: a page is at most 1 MiB of records and a tool range at
// most 1 MiB of each text, and JSON escaping can grow text several-fold.
const maxTranscriptAnswer = 16 << 20

// Transcript is the verb.
func Transcript(env Env, opts Options) (int, error) {
	action := opts.Action
	if !slices.Contains(transcriptActions, action) {
		fmt.Fprintf(env.Stderr, "cairn: transcript takes one action, %s — got %s\n", strings.Join(transcriptActions, ", "),
			store.PyRepr(action))
		return ExitUsage, nil
	}
	if !write.SessionComponent.MatchString(opts.Session) {
		fmt.Fprintf(env.Stderr, "cairn: --session %s is not a session id (%s)\n", store.PyRepr(opts.Session),
			write.SessionComponent.String())
		return ExitUsage, nil
	}
	q := url.Values{transcriptwire.ParamSession: {opts.Session}}
	allowed := transcriptActionFlags[action]
	for _, flag := range opts.TranscriptFlagsGiven {
		if !slices.Contains(allowed, flag) {
			fmt.Fprintf(env.Stderr, "cairn: transcript %s does not take %s (takes: %s)\n", action, flag,
				strings.Join(append([]string{"--session", "--scope", "--repo"}, allowed...), ", "))
			return ExitUsage, nil
		}
	}
	for param, value := range opts.TranscriptParams {
		q[param] = value
	}
	if opts.HasID {
		if !slices.Contains(allowed, "--id") {
			fmt.Fprintf(env.Stderr, "cairn: transcript %s does not take --id\n", action)
			return ExitUsage, nil
		}
		q.Set(transcriptwire.ParamID, opts.ID)
	}
	if action == transcriptwire.RouteTool && q.Get(transcriptwire.ParamSeq) == "" {
		fmt.Fprintln(env.Stderr, "cairn: transcript tool requires --seq (the record holding the call; the skeleton lists each call's)")
		return ExitUsage, nil
	}

	alias, label, cfg, err := transcriptRoute(env, opts)
	if err != nil {
		var usage *usageError
		if errors.As(err, &usage) {
			fmt.Fprintln(env.Stderr, usage.message)
			return ExitUsage, nil
		}
		if isRoutingError(err) {
			return 0, err
		}
		var missing *UIConfigMissing
		if errors.As(err, &missing) {
			fmt.Fprintln(env.Stderr, "🔴 cairn: transcript: nothing was read — "+missing.Error())
			return ExitUnreachableNoCache, nil
		}
		return 0, err
	}
	if label != "" {
		fmt.Fprintf(env.Stderr, "cairn: instance=%s\n", label)
	}
	a := transcriptRequest(cfg, transcriptwire.ClientPathPrefix+action+"?"+q.Encode(), opts.Timeout)
	switch {
	case a.err != nil && a.status == 0:
		fmt.Fprintf(env.Stderr, "🔴 cairn: transcript: nothing was read — %s unreachable: %v\n", cfg.URL, a.err)
		return ExitUnreachableNoCache, nil
	case a.err != nil:
		fmt.Fprintf(env.Stderr, "🔴 cairn: transcript: %s answered HTTP %d and the body could not be read: %v\n", cfg.URL, a.status, a.err)
		return ExitCorrupt, nil
	case a.status == http.StatusOK:
		_, _ = env.Stdout.Write(a.body)
		env.recordRead(alias, LedgerAllScopes)
		return ExitOK, nil
	case a.notFound():
		fmt.Fprintf(env.Stderr, "cairn: transcript: not-found — no transcript %s that this credential can read, or no such "+
			"stream, record or call in it (one that does not exist and one you cannot read are the same answer)\n",
			store.PyRepr(opts.Session))
		return ExitUnreachableNoCache, nil
	case a.status == http.StatusBadRequest:
		fmt.Fprintf(env.Stderr, "cairn: transcript: the listener refused the request%s\n", oneLineSuffix(a.body))
		return ExitUsage, nil
	case a.status == http.StatusUnauthorized:
		fmt.Fprintf(env.Stderr, "🔴 cairn: transcript: nothing was read — %s refused the credential in %s (HTTP 401; or this "+
			"host is locked out by earlier failures, up to 15 min)\n", cfg.URL, EnvUIToken)
		return ExitUnreachableNoCache, nil
	}
	fmt.Fprintf(env.Stderr, "🔴 cairn: transcript: %s answered HTTP %d%s\n", cfg.URL, a.status, oneLineSuffix(a.body))
	return ExitCorrupt, nil
}

// transcriptRoute is `(alias, label, config)` for a transcript read: the instance `--scope`, or the
// repo's scope, routes to — or the one instance when only one is configured.
func transcriptRoute(env Env, opts Options) (string, string, UIConfig, error) {
	routing, err := Discover(nil)
	if err != nil {
		return "", "", UIConfig{}, err
	}
	scope := opts.Scope
	if scope == "" {
		scope = RepoScope(opts.Repo)
	}
	alias := DefaultAlias
	switch {
	case scope != "":
		if alias, err = routing.AliasFor(scope); err != nil {
			return "", "", UIConfig{}, err
		}
	case routing.MultiInstance():
		return "", "", UIConfig{}, usagef("cairn: transcript: %d instances are configured and neither --scope nor the "+
			"working directory's repo names one; pass --scope <a scope on the instance that holds the session>",
			len(routing.Instances))
	}
	cfg, err := LoadUIConfigFor(alias)
	return alias, instanceLabel(routing, alias), cfg, err
}

// transcriptRequest is ONE bounded GET to the client listener with the agent credential —
// [memoRequest]'s shape with a larger body bound, since a page or a tool range is up to a mebibyte.
func transcriptRequest(cfg UIConfig, path string, timeout int) memoAnswer {
	req, err := http.NewRequest(http.MethodGet, cfg.URL+path, nil)
	if err != nil {
		return memoAnswer{err: err}
	}
	applyStandardHeaders(req, cfg.Token)
	resp, err := httpClient(timeout).Do(req)
	if err != nil {
		return memoAnswer{err: errors.New(urlErrorReason(err))}
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxTranscriptAnswer+1))
	if err == nil && len(data) > maxTranscriptAnswer {
		err = fmt.Errorf("the answer is larger than %d bytes", maxTranscriptAnswer)
	}
	if err != nil {
		return memoAnswer{status: resp.StatusCode, err: err}
	}
	return memoAnswer{status: resp.StatusCode, body: data}
}
