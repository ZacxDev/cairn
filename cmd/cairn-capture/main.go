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
// Three modes. `--self-test` measures the redactor on a synthetic corpus (closing-condition part
// 3); `--dry-run` prints per-session COUNTS, the derived `V` and the routing decision, and writes
// nothing; `--run` (S3b) is ONE capture pass — read, redact, route, upload over the capture/v1
// contract (`internal/capture.HTTPSink`), save the watermarks, exit — for a user systemd timer
// (decision 5: `OnStartupSec` + `OnUnitActiveSec=60s` + `AccuracySec=1s`).
//
// 🔴 `--run` UPLOADS NOTHING TO AN UNARMED INSTANCE, AND LOSES NOTHING EITHER. Every instance is
// unarmed today (O16): the worker answers 503 `capture-disarmed`, the sink holds, no watermark
// advances, and the next run retries. The capture token is read from a FILE (`-capture-token-file`,
// mode 0600) — never an argument, never the environment, never printed. A refused token (401/403)
// ends the run's uploads to that instance and exits 1.
//
// 🔴 A SEPARATE BINARY, NOT A `cairn` VERB (decision 5): a verb that reads every transcript on the
// machine would widen what the installed reader is for. And it is STDLIB-ONLY, enforced: it is one
// of `internal/depspolicy`'s `LinkedBinaryRoots`.
//
//	cairn-capture --self-test [-seed N]
//	cairn-capture --dry-run [-claude-root DIR] [-opencode-project DIR]... [-ledger-dir DIR] [-denylist FILE]
//	cairn-capture --run -host LABEL -worker [ALIAS=]URL -capture-token-file [ALIAS=]FILE [-state FILE]
//	              [-host-key FILE] [the --dry-run reading flags]
package main

import (
	"crypto/rand"
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
		runOnce    = fs.Bool("run", false, "one capture pass: read, redact, route, upload, save the watermarks, exit")
		host       = fs.String("host", "", "--run: this host's label, the one its capture token was minted for (-worker-host)")
		statePath  = fs.String("state", "", "--run: the 0600 state file (default $XDG_STATE_HOME/cairn/capture/state.json)")
		hostKey    = fs.String("host-key", "", "--run: the 0600 per-host redaction tag key (default $XDG_STATE_HOME/cairn/capture/host.key)")
		maxRequest = fs.Int("max-request-bytes", capture.DefaultMaxRequest, "--run: the largest request body; a larger record is sent as frames")
		workers    multi
		tokenFiles multi
		claudeRoot = fs.String("claude-root", filepath.Join(home, ".claude", "projects"), "the Claude Code projects root")
		ledgerDir  = fs.String("ledger-dir", "", "the client read-ledger directory (default $XDG_STATE_HOME/cairn/read-ledger)")
		denylist   = fs.String("denylist", "", "a 0600 per-host denylist file (optional)")
		ocBin      = fs.String("opencode-bin", "opencode", "the opencode binary, by name or path")
		ocDirs     multi
	)
	fs.Var(&ocDirs, "opencode-project", "an opencode project directory to enumerate (repeatable; `session list` lists ONE project's roots)")
	fs.Var(&workers, "worker", "--run: [ALIAS=]URL of an instance's worker listener (repeatable; no alias is the default instance)")
	fs.Var(&tokenFiles, "capture-token-file", "--run: [ALIAS=]FILE holding that instance's capture token, mode 0600 (repeatable)")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(stderr, "cairn-capture: unexpected argument %q\n", fs.Arg(0))
		return exitUsage
	}
	modes := 0
	for _, on := range []bool{*selfTest, *dryRun, *runOnce} {
		if on {
			modes++
		}
	}
	switch {
	case modes != 1:
		fmt.Fprintln(stderr, "cairn-capture: name exactly one of --run, --dry-run or --self-test")
		return exitUsage
	case *selfTest:
		return redact.SelfTest(stdout, *seed)
	}
	stateHome := getenv("XDG_STATE_HOME")
	if stateHome == "" {
		stateHome = filepath.Join(home, ".local", "state")
	}
	if *ledgerDir == "" {
		*ledgerDir = filepath.Join(stateHome, "cairn", "read-ledger")
	}
	if *runOnce {
		return runPass(runConfig{host: *host, state: *statePath, hostKey: *hostKey, maxRequest: *maxRequest,
			workers: workers, tokenFiles: tokenFiles, stateHome: stateHome, claudeRoot: *claudeRoot,
			ocDirs: ocDirs, ocBin: *ocBin, ledgerDir: *ledgerDir, denylist: *denylist}, stderr, getenv)
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

type runConfig struct {
	host, state, hostKey, stateHome string
	maxRequest                      int
	workers, tokenFiles             []string
	claudeRoot, ocBin, ledgerDir    string
	denylist                        string
	ocDirs                          []string
}

// aliased splits `[ALIAS=]VALUE`; no alias is the default instance.
func aliased(v string) (string, string) {
	if alias, rest, ok := strings.Cut(v, "="); ok && client.ValidAlias(alias) {
		return alias, rest
	}
	return client.DefaultAlias, v
}

// endpoints pairs every -worker with its -capture-token-file, by alias. A URL with no token file,
// a token file with no URL, an alias named twice, or an alias the routing does not configure is a
// usage error: each is an instance the run would silently never reach.
func endpoints(c runConfig, routing client.Routing) (map[string]capture.Endpoint, error) {
	urls, files := map[string]string{}, map[string]string{}
	for _, w := range c.workers {
		alias, raw := aliased(w)
		if _, dup := urls[alias]; dup {
			return nil, fmt.Errorf("-worker names instance %s twice", alias)
		}
		u, err := capture.ValidateEndpoint(raw)
		if err != nil {
			return nil, err
		}
		urls[alias] = u
	}
	for _, f := range c.tokenFiles {
		alias, path := aliased(f)
		if _, dup := files[alias]; dup {
			return nil, fmt.Errorf("-capture-token-file names instance %s twice", alias)
		}
		files[alias] = path
	}
	if len(urls) == 0 {
		return nil, errors.New("--run needs at least one -worker [ALIAS=]URL")
	}
	known := map[string]bool{}
	for _, a := range routing.Aliases() {
		known[a] = true
	}
	out := map[string]capture.Endpoint{}
	for alias, u := range urls {
		if !known[alias] {
			return nil, fmt.Errorf("-worker names instance %s, which this host's routing does not configure (%s)",
				alias, strings.Join(routing.Aliases(), ", "))
		}
		path, ok := files[alias]
		if !ok {
			return nil, fmt.Errorf("-worker %s has no -capture-token-file %s=<file>", alias, alias)
		}
		tok, err := capture.ReadTokenFile(path)
		if err != nil {
			return nil, err
		}
		out[alias] = capture.Endpoint{URL: u, Token: tok}
	}
	for alias := range files {
		if _, ok := urls[alias]; !ok {
			return nil, fmt.Errorf("-capture-token-file %s has no -worker %s=<url>", alias, alias)
		}
	}
	return out, nil
}

// runPass is `--run`: ONE capture pass. Exit 0 when the pass completed — a session that was held,
// refused or retried later is a logged line, not a failure of the pass; exit 1 when the state
// cannot be read or saved, or an instance refused the capture token (401/403); exit 2 on usage.
func runPass(c runConfig, stderr io.Writer, getenv func(string) string) int {
	fail := func(err error) int {
		fmt.Fprintf(stderr, "cairn-capture: %v\n", err)
		return exitError
	}
	if c.host == "" || strings.ContainsAny(c.host, " \t\r\n/") {
		fmt.Fprintln(stderr, "cairn-capture: --run needs -host, the label this host's capture token was minted for")
		return exitUsage
	}
	if c.maxRequest < 4096 {
		fmt.Fprintln(stderr, "cairn-capture: -max-request-bytes under 4096 leaves no room for a request")
		return exitUsage
	}
	routing, err := client.Discover(getenv)
	if err != nil {
		return fail(err)
	}
	eps, err := endpoints(c, routing)
	if err != nil {
		fmt.Fprintf(stderr, "cairn-capture: %v\n", err)
		return exitUsage
	}
	dir := filepath.Join(c.stateHome, "cairn", "capture")
	if c.state == "" || c.hostKey == "" {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fail(err)
		}
	}
	if c.state == "" {
		c.state = filepath.Join(dir, "state.json")
	}
	if c.hostKey == "" {
		c.hostKey = filepath.Join(dir, "host.key")
	}
	release, err := capture.LockState(c.state)
	if errors.Is(err, capture.ErrRunning) {
		fmt.Fprintln(stderr, "cairn-capture: another run holds the lock; this one does nothing")
		return exitOK
	}
	if err != nil {
		return fail(err)
	}
	defer release()
	state, err := capture.LoadState(c.state)
	if err != nil {
		return fail(err)
	}
	key, err := redact.LoadOrCreateKey(c.hostKey)
	if err != nil {
		return fail(err)
	}
	var deny *redact.Denylist
	if c.denylist != "" {
		if deny, err = redact.LoadDenylist(c.denylist); err != nil {
			return fail(err)
		}
	}
	red, err := redact.New(key, deny)
	if err != nil {
		return fail(err)
	}
	if capture.RedactionOff() {
		fmt.Fprintln(stderr, "cairn-capture: 🔴 THIS IS THE REDACTION-OFF TEST BUILD — every byte ships unredacted, to loopback only")
	}
	sink := &capture.HTTPSink{Host: c.host, Endpoints: eps, MaxRequest: c.maxRequest, Log: stderr}
	agent := &capture.Agent{
		ClaudeRoot:   c.claudeRoot,
		OpencodeDirs: c.ocDirs,
		Runner:       capture.ExecRunner{Bin: c.ocBin},
		LedgerDir:    c.ledgerDir,
		Routing:      routing,
		Redactor:     red,
		Sink:         sink,
		State:        state,
		Log:          stderr,
	}
	sum, runErr := agent.Run()
	// 🔴 THE STATE IS SAVED EVEN WHEN THE RUN ENDS LOUDLY: every watermark in it advanced on a 200,
	// so what it records is stored, and dropping it would re-send those bytes as 409-resumes.
	if err := state.Save(c.state); err != nil {
		return fail(fmt.Errorf("the state file %s could not be saved: %w", c.state, err))
	}
	fmt.Fprintf(stderr, "cairn-capture: run %s omitted=%d requests=%d\n", sum, sink.Omitted, sink.Requests)
	if runErr != nil {
		return fail(runErr)
	}
	return exitOK
}
