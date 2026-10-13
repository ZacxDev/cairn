// Command summary is example plugin A of `claudedocs/plan-cairn-plugins.md` (slice S9; operator decision
// O5): continuous session SUMMARIES. It is a ONE-SHOT program for a host-side user timer — each run asks
// the pod which sessions have grown, summarises only what is new, writes one `summary` output per
// session, and exits. Nothing runs until the operator registers its manifest (`manifest.json`, via
// `cairn-ui -plugin-registry`), issues it a `plugin` token, and a scope admin turns it ON.
//
//	summary -worker-url https://… -token-file ~/.config/cairn-summary/token \
//	        -provider anthropic -model claude-opus-5-5 -key-file ~/.config/cairn-summary/key
//
// 🔴 THE PROVIDER KEY AND THE PLUGIN TOKEN ARE READ FROM FILES (mode 0600), NEVER FROM A FLAG VALUE OR
// THE ENVIRONMENT, and neither is ever printed (`internal/secretfile`).
//
// 🔴 IT SENDS TRANSCRIPT CONTENT TO A THIRD PARTY — the provider — which is why its manifest says
// `calls_model: true` and why it reaches only sessions whose every scope's admins consented (T7).
//
// Exit codes: 0 the run finished (including one the spend cap stopped), 1 a session failed or the pod
// could not be asked, 2 a usage or configuration error.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/ZacxDev/cairn/plugins/internal/llm"
	"github.com/ZacxDev/cairn/plugins/internal/pluginapi"
	"github.com/ZacxDev/cairn/plugins/internal/secretfile"
	"github.com/ZacxDev/cairn/plugins/internal/spend"
)

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr, time.Now)) }

func run(args []string, stdout, stderr io.Writer, now func() time.Time) int {
	fs := flag.NewFlagSet("summary", flag.ContinueOnError)
	fs.SetOutput(stderr)
	workerURL := fs.String("worker-url", "", "the instance's worker listener base URL (required)")
	tokenFile := fs.String("token-file", "", "file holding this plugin's `plugin` token, mode 0600 (required)")
	provider := fs.String("provider", "anthropic", "model provider: anthropic or openai (an OpenAI-compatible chat-completions API)")
	providerURL := fs.String("provider-url", "", "override the provider's base URL (a proxy)")
	model := fs.String("model", "claude-opus-5-5", "the model to ask")
	effort := fs.String("effort", "low", "anthropic only: output_config.effort (\"\" sends none)")
	keyFile := fs.String("key-file", "", "file holding the provider API key, mode 0600 (required)")
	limit := fs.Int("limit", 10, "pending sessions one run takes (1–100)")
	perDay := fs.Int("max-requests-per-day", 50, "the spend cap: provider requests over a rolling 24 hours; 0 sends none")
	ledgerPath := fs.String("spend-ledger", spend.DefaultPath("cairn-summary"), "the spend cap's ledger file")
	chunkBytes := fs.Int("chunk-bytes", 96<<10, "rendered transcript bytes one request carries")
	maxOut := fs.Int("max-output-tokens", 16000, "the bound on one answer, thinking included")
	timeout := fs.Duration("timeout", 30*time.Minute, "the bound on the whole run")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	usage := func(msg string) int {
		fmt.Fprintln(stderr, "summary: "+msg)
		return 2
	}
	switch {
	case fs.NArg() > 0:
		return usage("takes no arguments")
	case *workerURL == "":
		return usage("-worker-url is required")
	case *limit < 1 || *limit > 100:
		return usage("-limit must be 1–100")
	case *perDay < 0:
		return usage("-max-requests-per-day must be ≥ 0")
	case *ledgerPath == "":
		return usage("-spend-ledger is required (no home directory to default it under)")
	case *chunkBytes < 4<<10 || *chunkBytes > 1<<20:
		return usage("-chunk-bytes must be 4 KiB–1 MiB")
	case *maxOut < 256 || *maxOut > 64000:
		return usage("-max-output-tokens must be 256–64000")
	}
	if *provider != "anthropic" && *effort == "low" {
		*effort = "" // the default is Anthropic's; another provider takes none
	}
	token, err := secretfile.Read(*tokenFile)
	if err != nil {
		return usage(err.Error())
	}
	key, err := secretfile.Read(*keyFile)
	if err != nil {
		return usage(err.Error())
	}
	p, err := llm.New(llm.Config{Provider: *provider, BaseURL: *providerURL, Model: *model, Key: key, Effort: *effort})
	if err != nil {
		return usage(err.Error())
	}
	r := &Runner{
		API:             &pluginapi.Client{Base: *workerURL, Token: token},
		Provider:        p,
		Spend:           &spend.Ledger{Path: *ledgerPath, Cap: *perDay, Now: now},
		Limit:           *limit,
		ChunkBytes:      *chunkBytes,
		MaxOutputTokens: *maxOut,
		PageBytes:       1 << 20,
		Log:             func(s string) { fmt.Fprintln(stderr, s) },
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	st, err := r.Run(ctx)
	fmt.Fprintf(stdout, "SUMMARY summary-run: sessions=%d written=%d requests=%d failed=%d capped=%t\n",
		st.Sessions, st.Written, st.Requests, st.Failed, st.Capped)
	if err != nil {
		fmt.Fprintln(stderr, "summary: "+err.Error())
		return 1
	}
	if st.Failed > 0 {
		return 1
	}
	return 0
}
