// Command cairn-capture is the host-side session-transcript capture agent (S2 of
// `claudedocs/plan-cairn-plugins.md`, decision 5).
//
// It reads Claude Code JSONL by byte offset (complete lines only, refusing a rewritten file),
// every subagent file as a child stream and every persisted tool result as a blob; it reads
// opencode through `opencode export` (stdout to a FILE — a pipe truncates it at exit 0, measured)
// and never opens opencode's database; it derives each session's read scopes
// (`internal/transcript/scopeuse`), routes the session (decision 16), redacts it
// (`internal/redact`), and writes the redacted bytes to a LOCAL SPOOL.
//
// 🔴 IT UPLOADS NOTHING. The spool is this slice's stand-in for S3's upload, so the binary is
// inert: it sends no byte off the host.
//
// 🔴 A SEPARATE BINARY, NOT A `cairn` VERB (decision 5): a verb that reads every transcript on the
// machine would widen what the installed reader is for. And it is STDLIB-ONLY, enforced: it is one
// of `internal/depspolicy`'s `LinkedBinaryRoots`.
//
//	cairn-capture -state DIR -spool DIR [-claude-root DIR] [-opencode-project DIR]... [-dry-run]
//	cairn-capture --self-test [-seed N]
//	cairn-capture -verbs
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/ZacxDev/cairn/internal/capture"
	"github.com/ZacxDev/cairn/internal/client"
	"github.com/ZacxDev/cairn/internal/redact"
)

// Modes is this binary's ledger, printed by -verbs and pinned by the nix check
// `cairn-capture-declares-its-modes`: a mode added or lost here without that check moving is red.
var Modes = []string{
	"dry-run reads",
	"run writes-spool",
	"self-test reads",
	"verbs reads",
}

// Exit codes.
const (
	exitOK    = 0
	exitError = 1
	exitUsage = 2
)

type multi []string

func (m *multi) String() string     { return strings.Join(*m, ",") }
func (m *multi) Set(v string) error { *m = append(*m, v); return nil }

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr, os.Getenv)) }

func run(args []string, stdout, stderr io.Writer, getenv func(string) string) int {
	fs := flag.NewFlagSet("cairn-capture", flag.ContinueOnError)
	fs.SetOutput(stderr)
	home := getenv("HOME")
	var (
		verbs      = fs.Bool("verbs", false, "print this binary's mode ledger and exit")
		selfTest   = fs.Bool("self-test", false, "run the redaction corpus and print its SUMMARY line")
		seed       = fs.Uint64("seed", 20000101, "the self-test corpus seed")
		dryRun     = fs.Bool("dry-run", false, "print per-session counts, V and the routing decision; write nothing")
		stateDir   = fs.String("state", "", "the agent's state directory (watermarks, host key); required")
		spoolDir   = fs.String("spool", "", "the local spool the redacted sessions are written to; required to run")
		claudeRoot = fs.String("claude-root", filepath.Join(home, ".claude", "projects"), "the Claude Code projects root")
		ledgerDir  = fs.String("ledger-dir", "", "the client read-ledger directory (default $XDG_STATE_HOME/cairn/read-ledger)")
		denylist   = fs.String("denylist", "", "a 0600 per-host denylist file (optional)")
		ocBin      = fs.String("opencode-bin", "opencode", "the opencode binary, by name or path")
		ocDirs     multi
	)
	fs.Var(&ocDirs, "opencode-project", "an opencode project directory to enumerate (repeatable; `session list` lists ONE project's roots)")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(stderr, "cairn-capture: unexpected argument %q\n", fs.Arg(0))
		return exitUsage
	}
	switch {
	case *verbs:
		for _, m := range Modes {
			fmt.Fprintln(stdout, m)
		}
		return exitOK
	case *selfTest:
		return redact.SelfTest(stdout, *seed)
	}
	if *stateDir == "" {
		fmt.Fprintln(stderr, "cairn-capture: -state is required (it holds the watermarks and the host key)")
		return exitUsage
	}
	if !*dryRun && *spoolDir == "" {
		fmt.Fprintln(stderr, "cairn-capture: -spool is required: this build uploads nothing and writes a local spool")
		return exitUsage
	}
	if *ledgerDir == "" {
		base := getenv("XDG_STATE_HOME")
		if base == "" {
			base = filepath.Join(home, ".local", "state")
		}
		*ledgerDir = filepath.Join(base, "cairn", "read-ledger")
	}
	if err := os.MkdirAll(*stateDir, 0o700); err != nil {
		fmt.Fprintf(stderr, "cairn-capture: %v\n", err)
		return exitError
	}
	key, err := redact.LoadOrCreateKey(filepath.Join(*stateDir, "host.key"))
	if err != nil {
		fmt.Fprintf(stderr, "cairn-capture: %v\n", err)
		return exitError
	}
	var deny *redact.Denylist
	if *denylist != "" {
		if deny, err = redact.LoadDenylist(*denylist); err != nil {
			fmt.Fprintf(stderr, "cairn-capture: %v\n", err)
			return exitError
		}
	}
	red, err := redact.New(key, deny)
	if err != nil {
		fmt.Fprintf(stderr, "cairn-capture: %v\n", err)
		return exitError
	}
	routing, err := client.Discover(getenv)
	if err != nil {
		fmt.Fprintf(stderr, "cairn-capture: %v\n", err)
		return exitError
	}
	agent := &capture.Agent{
		ClaudeRoot:   *claudeRoot,
		OpencodeDirs: ocDirs,
		Runner:       capture.ExecRunner{Bin: *ocBin, TempDir: *stateDir},
		LedgerDir:    *ledgerDir,
		Routing:      routing,
		Redactor:     red,
		Log:          stderr,
	}
	if *dryRun {
		if err := agent.DryRun(stdout); err != nil {
			fmt.Fprintf(stderr, "cairn-capture: %v\n", err)
			return exitError
		}
		return exitOK
	}
	statePath := filepath.Join(*stateDir, "state.json")
	st, err := capture.LoadState(statePath)
	if err != nil {
		fmt.Fprintf(stderr, "cairn-capture: %v\n", err)
		return exitError
	}
	agent.State = st
	agent.Sink = capture.FileSpool{Dir: *spoolDir}
	sum, runErr := agent.Run()
	// The state is saved even after an error: every watermark in it advanced only after its
	// spool write succeeded, so what it holds is true.
	if err := st.Save(statePath); err != nil {
		runErr = errors.Join(runErr, err)
	}
	fmt.Fprintf(stderr, "cairn-capture: sessions=%d shipped=%d held=%d withdrawn=%d records=%d blobs=%d refused=%d\n",
		sum.Sessions, sum.Shipped, sum.Held, sum.Withdrawn, sum.Records, sum.Blobs, sum.Refused)
	if runErr != nil {
		fmt.Fprintf(stderr, "cairn-capture: %v\n", runErr)
		return exitError
	}
	return exitOK
}
