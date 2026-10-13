// Command clickup is example plugin B of `claudedocs/plan-cairn-plugins.md` (slice S10; operator decision
// O6): it matches ClickUp tickets to sessions. Like plugin A it is a ONE-SHOT program for a host-side
// user timer, inert until the operator registers its manifest (`manifest.json`, via
// `cairn-ui -plugin-registry`), issues it a `plugin` token, and a scope admin turns it ON.
//
//	clickup -worker-url https://… -token-file ~/.config/cairn-clickup/token \
//	        -clickup-token-file ~/.config/cairn-clickup/clickup-token -clickup-list <list id> \
//	        [-repo ~/src/alpha]… [-gh-bin gh] [-store-root ~/.cache/…]… [-key-file ~/.config/cairn-clickup/key]
//
// It writes three output types, and they are three different CLAIMS (decision 14):
//
//   - `ticket-edge` — a DETERMINISTIC match with its provenance: which signal (a branch name, a commit
//     message or trailer, a PR body or the linked PR's title, a URL in the transcript, an entry ref)
//     and where exactly (a record, a commit, a PR, an entry);
//   - `ticket-suggestion` — a MODEL's guess, with a confidence; never promoted to an edge here;
//   - `ticket-scan` — that this pass examined the session, whatever it found.
//
// 🔴 CLICKUP IS READ, NEVER WRITTEN (O6). 🔴 EVERY CREDENTIAL IS READ FROM A FILE (mode 0600), NEVER FROM
// A FLAG VALUE OR THE ENVIRONMENT, and none is ever printed. 🔴 WITH `-key-file` IT SENDS SESSION
// CONTENT AND TICKET NAMES TO A THIRD PARTY — the provider — which is why its manifest says
// `calls_model: true`; without one it asks no model and is deterministic only.
//
// Exit codes: 0 the run finished (including one the spend cap limited), 1 a session, a source or the
// ticket fetch failed, 2 a usage or configuration error.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ZacxDev/cairn/plugins/internal/endpoint"
	"github.com/ZacxDev/cairn/plugins/internal/llm"
	"github.com/ZacxDev/cairn/plugins/internal/pluginapi"
	"github.com/ZacxDev/cairn/plugins/internal/secretfile"
	"github.com/ZacxDev/cairn/plugins/internal/spend"
)

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr, time.Now)) }

// multi is a repeatable string flag.
type multi []string

func (m *multi) String() string     { return strings.Join(*m, ",") }
func (m *multi) Set(v string) error { *m = append(*m, v); return nil }

func run(args []string, stdout, stderr io.Writer, now func() time.Time) int {
	fs := flag.NewFlagSet("clickup", flag.ContinueOnError)
	fs.SetOutput(stderr)
	workerURL := fs.String("worker-url", "", "the instance's worker listener base URL (required)")
	tokenFile := fs.String("token-file", "", "file holding this plugin's `plugin` token, mode 0600 (required)")
	cuTokenFile := fs.String("clickup-token-file", "", "file holding a READ-ONLY ClickUp API token, mode 0600 (required)")
	cuURL := fs.String("clickup-url", DefaultClickUpURL, "the ClickUp API's base URL")
	cuTeam := fs.String("clickup-team", "", "the ClickUp workspace id a /t/<workspace>/<custom-id> URL must name (\"\" accepts any)")
	cuWait := fs.Duration("clickup-max-wait", 2*time.Minute, "the longest one wait for a ClickUp rate-limit window (429)")
	var lists, repos, roots multi
	fs.Var(&lists, "clickup-list", "a ClickUp list id whose tasks are the closed ticket set (repeatable; at least one)")
	fs.Var(&repos, "repo", "a host-local repository whose commits are read for session trailers (repeatable; none reads no commit)")
	fs.Var(&roots, "store-root", "a cairn cache root whose entries are read for `clickup:` refs (repeatable; none reads no entry)")
	gitBin := fs.String("git-bin", "git", "the git to run in each -repo")
	ghBin := fs.String("gh-bin", "", "the GitHub CLI to read PR titles and bodies with (\"\" reads no PR)")
	provider := fs.String("provider", "anthropic", "model provider: anthropic or openai (an OpenAI-compatible chat-completions API)")
	providerURL := fs.String("provider-url", "", "override the provider's base URL (a proxy)")
	model := fs.String("model", "claude-opus-5-5", "the model to ask")
	effort := fs.String("effort", "", "anthropic only: output_config.effort (\"\" sends none)")
	keyFile := fs.String("key-file", "", "file holding the provider API key, mode 0600 (\"\" asks no model: deterministic matches only)")
	limit := fs.Int("limit", 10, "pending sessions one run takes (1–100)")
	perDay := fs.Int("max-requests-per-day", 50, "the spend cap: provider requests over a rolling 24 hours; 0 sends none")
	ledgerPath := fs.String("spend-ledger", spend.DefaultPath("cairn-clickup"), "the spend cap's ledger file")
	statePath := fs.String("state-file", "", "per-session backoff (default: sessions.json beside -spend-ledger)")
	excerpt := fs.Int("excerpt-bytes", 32<<10, "rendered bytes of a session's opening sent when no summary is stored")
	maxOut := fs.Int("max-output-tokens", 4000, "the bound on one answer, thinking included")
	maxCands := fs.Int("max-candidates", 200, "the most candidate tickets one request names (most recently updated first)")
	maxSugg := fs.Int("max-suggestions", 3, "the most suggestions one pass writes for a session")
	minConf := fs.Float64("min-confidence", 0.5, "a suggestion below this confidence is not written")
	storeTitles := fs.Bool("store-ticket-titles", false, "send each ticket's NAME to the pod as the output's title; the default "+
		"stores ticket ids only, and an instance not started with -plugin-store-ticket-titles refuses a titled output")
	var plaintext pluginapi.HostList
	fs.Var(&plaintext, "allow-plaintext-host", "a host -worker-url may be plain http to (exact match, repeatable; no default): "+
		"for a listener on a private ENCRYPTED mesh address. Never applies to -clickup-url or -provider-url")
	timeout := fs.Duration("timeout", 30*time.Minute, "the bound on the whole run")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	usage := func(msg string) int {
		fmt.Fprintln(stderr, "clickup: "+msg)
		return 2
	}
	switch {
	case fs.NArg() > 0:
		return usage("takes no arguments")
	case *workerURL == "":
		return usage("-worker-url is required")
	case len(lists) == 0:
		return usage("-clickup-list is required (the closed ticket set a custom id must be in)")
	case *cuTeam != "" && !allDigits.MatchString(*cuTeam):
		return usage("-clickup-team must be a numeric workspace id")
	case *limit < 1 || *limit > 100:
		return usage("-limit must be 1–100")
	case *perDay < 0:
		return usage("-max-requests-per-day must be ≥ 0")
	case *excerpt < 4<<10 || *excerpt > 1<<20:
		return usage("-excerpt-bytes must be 4 KiB–1 MiB")
	case *maxOut < 256 || *maxOut > 64000:
		return usage("-max-output-tokens must be 256–64000")
	case *maxCands < 1 || *maxCands > 2000:
		return usage("-max-candidates must be 1–2000")
	case *maxSugg < 1 || *maxSugg > 20:
		return usage("-max-suggestions must be 1–20")
	case *minConf < 0 || *minConf > 1:
		return usage("-min-confidence must be 0–1")
	case *cuWait < 0:
		return usage("-clickup-max-wait must be ≥ 0")
	}
	for _, l := range lists {
		if !listIDShape.MatchString(l) {
			return usage(fmt.Sprintf("-clickup-list %q is not a ClickUp list id", l))
		}
	}
	for _, dir := range repos {
		if st, err := os.Stat(dir); err != nil || !st.IsDir() {
			return usage(fmt.Sprintf("%s is not a directory", dir))
		}
	}
	for i, dir := range roots {
		// Resolved first: a walk does not follow a symlinked ROOT, and would index nothing in silence.
		real, err := filepath.EvalSymlinks(dir)
		if st, serr := os.Stat(real); err != nil || serr != nil || !st.IsDir() {
			return usage(fmt.Sprintf("%s is not a directory", dir))
		}
		roots[i] = real
	}
	token, err := secretfile.Read(*tokenFile)
	if err != nil {
		return usage(err.Error())
	}
	cuToken, err := secretfile.Read(*cuTokenFile)
	if err != nil {
		return usage(err.Error())
	}
	api := &pluginapi.Client{Base: *workerURL, Token: token, AllowPlaintextHosts: plaintext}
	plainHost, err := api.ValidateBase()
	if err != nil {
		return usage(err.Error())
	}
	if plainHost != "" {
		fmt.Fprintf(stderr, "clickup: -worker-url is PLAIN HTTP to %s, allowed by -allow-plaintext-host: the plugin token and transcript "+
			"content are encrypted only by whatever carries that address\n", plainHost)
	}
	cuBase, _, err := endpoint.Validate("-clickup-url", *cuURL, nil)
	if err != nil {
		return usage(err.Error())
	}
	if *statePath == "" {
		if *ledgerPath == "" {
			return usage("-state-file is required (no home directory to default it under)")
		}
		*statePath = filepath.Join(filepath.Dir(*ledgerPath), "sessions.json")
	}
	state, err := loadState(*statePath)
	if err != nil {
		return usage(err.Error())
	}
	logf := func(s string) { fmt.Fprintln(stderr, s) }
	r := &Runner{
		API:             api,
		State:           state,
		Now:             now,
		Limit:           *limit,
		Repos:           repos,
		GitBin:          *gitBin,
		GHBin:           *ghBin,
		PageBytes:       1 << 20,
		ExcerptBytes:    *excerpt,
		MaxOutputTokens: *maxOut,
		MaxCandidates:   *maxCands,
		MaxSuggestions:  *maxSugg,
		MinConfidence:   *minConf,
		StoreTitles:     *storeTitles,
		Log:             logf,
	}
	if *keyFile != "" {
		if *ledgerPath == "" {
			return usage("-spend-ledger is required (no home directory to default it under)")
		}
		key, err := secretfile.Read(*keyFile)
		if err != nil {
			return usage(err.Error())
		}
		p, err := llm.New(llm.Config{Provider: *provider, BaseURL: *providerURL, Model: *model, Key: key, Effort: *effort})
		if err != nil {
			return usage(err.Error())
		}
		r.Provider = p
		r.Spend = &spend.Ledger{Path: *ledgerPath, Cap: *perDay, Now: now}
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	var st Stats
	summary := func() {
		fmt.Fprintf(stdout, "SUMMARY clickup-run: sessions=%d edges=%d suggestions=%d requests=%d failed=%d capped=%t skipped=%d\n",
			st.Sessions, st.Edges, st.Suggestions, st.Requests, st.Failed, st.Capped, st.Skipped)
	}
	fail := func(err error) int {
		summary()
		fmt.Fprintln(stderr, "clickup: "+err.Error())
		return 1
	}
	// The pending list FIRST: a run with nothing to do asks ClickUp nothing.
	items, err := r.API.Pending(ctx, pendingAsk)
	if err != nil {
		return fail(fmt.Errorf("the pending query: %w", err))
	}
	todo, err := r.due(ctx, items, &st)
	if err != nil {
		return fail(err)
	}
	if len(todo) == 0 {
		summary()
		return 0
	}
	// 🔴 THE TICKET LIST BEFORE ANY SESSION, AND A FAILED FETCH WRITES NOTHING: a pass over a session
	// without the closed list would resolve no custom id and still write a scan saying it examined it.
	cu := &ClickUp{Base: cuBase, Token: cuToken, MaxWait: *cuWait, Log: logf, Now: now}
	var tickets []Ticket
	for _, l := range lists {
		ts, err := cu.List(ctx, l)
		if err != nil {
			return fail(err)
		}
		tickets = append(tickets, ts...)
	}
	ix := NewIndex(tickets)
	if ix.Dropped > 0 {
		logf(fmt.Sprintf("clickup: %d task(s) carry an id that is not a canonical id and are left out", ix.Dropped))
	}
	r.Matcher = &Matcher{Index: ix, Team: *cuTeam}
	if len(roots) > 0 {
		entries, skipped, err := BuildEntryIndex(roots)
		if err != nil {
			return fail(err)
		}
		if skipped > 0 {
			logf(fmt.Sprintf("clickup: %d cached entry file(s) could not be read and are left out", skipped))
		}
		r.Entries = entries
	}
	if err := r.Run(ctx, todo, &st); err != nil {
		return fail(err)
	}
	summary()
	if st.Failed > 0 {
		return 1
	}
	return 0
}
