// Command cairn-capture is the host-side session-transcript capture agent (S2 of
// `claudedocs/plan-cairn-plugins.md`, decision 5).
//
// The agent (`internal/capture`) reads Claude Code JSONL by byte offset (complete lines only,
// refusing a rewritten file), every subagent file as a child stream and every persisted tool result
// as a blob; it reads opencode through `opencode export` (stdout to a FILE — a pipe truncates it at
// exit 0, measured) and never opens opencode's database; it derives each session's read scopes
// (`internal/transcript/scopeuse`), routes the session (decision 16) and redacts it
// (`internal/redact`).
//
// 🔴 THIS BUILD SENDS AND STORES NOTHING. Its two modes read: `--self-test` measures the redactor on
// a synthetic corpus (closing-condition part 3), and `--dry-run` prints per-session COUNTS, the
// derived `V` and the routing decision. The capture run itself arrives with its upload in S3; a
// local spool stood in for it here and was removed on review (D1) rather than shipped as a mode
// with no consumer.
//
// 🔴 A SEPARATE BINARY, NOT A `cairn` VERB (decision 5): a verb that reads every transcript on the
// machine would widen what the installed reader is for. And it is STDLIB-ONLY, enforced: it is one
// of `internal/depspolicy`'s `LinkedBinaryRoots`.
//
//	cairn-capture --self-test [-seed N]
//	cairn-capture --dry-run [-claude-root DIR] [-opencode-project DIR]... [-ledger-dir DIR] [-denylist FILE]
package main

import (
	"crypto/rand"
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
		selfTest   = fs.Bool("self-test", false, "run the redaction corpus and print its SUMMARY line")
		seed       = fs.Uint64("seed", 20000101, "the self-test corpus seed")
		dryRun     = fs.Bool("dry-run", false, "print per-session counts, V and the routing decision; write nothing")
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
	case *selfTest:
		return redact.SelfTest(stdout, *seed)
	case !*dryRun:
		fmt.Fprintln(stderr, "cairn-capture: this build sends nothing; run it with --dry-run or --self-test")
		return exitUsage
	}
	if *ledgerDir == "" {
		base := getenv("XDG_STATE_HOME")
		if base == "" {
			base = filepath.Join(home, ".local", "state")
		}
		*ledgerDir = filepath.Join(base, "cairn", "read-ledger")
	}
	// 🔴 --dry-run WRITES NOTHING, THE HOST KEY INCLUDED. It prints counts by rule, never a tag, so
	// an ephemeral in-memory key is all it needs; creating the persistent key file as a side effect
	// of a read-only mode was a review finding (round 1).
	key := make([]byte, redact.HostKeyBytes)
	if _, err := rand.Read(key); err != nil {
		fmt.Fprintf(stderr, "cairn-capture: %v\n", err)
		return exitError
	}
	var deny *redact.Denylist
	var err error
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
		Runner:       capture.ExecRunner{Bin: *ocBin},
		LedgerDir:    *ledgerDir,
		Routing:      routing,
		Redactor:     red,
		Log:          stderr,
	}
	if err := agent.DryRun(stdout); err != nil {
		fmt.Fprintf(stderr, "cairn-capture: %v\n", err)
		return exitError
	}
	return exitOK
}
