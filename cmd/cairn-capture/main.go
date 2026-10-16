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
// mode 0600) — never an argument, never the environment, never printed. The exit codes are stated
// ONCE, in `internal/capture/README.md` § *`--run`'s exit status*.
//
// 🔴 A SEPARATE BINARY, NOT A `cairn` VERB (decision 5): a verb that reads every transcript on the
// machine would widen what the installed reader is for. And it is STDLIB-ONLY, enforced: it is one
// of `internal/depspolicy`'s `LinkedBinaryRoots`.
//
//	cairn-capture --self-test [-seed N]
//	cairn-capture --dry-run [-claude-root DIR] [-opencode-project DIR]... [-opencode-discover] [-opencode-bin BIN]
//	              [-ledger-dir DIR] [-denylist FILE] [-route-origin-owner OWNER=ALIAS]...
//	cairn-capture --run -host LABEL -worker [ALIAS=]URL -capture-token-file [ALIAS=]FILE [-state FILE]
//	              [-host-key FILE] [-allow-plaintext-host [ALIAS=]HOST]... [the --dry-run reading flags]
//
// 🔴 `-allow-plaintext-host` IS AN EXPLICIT OPERATOR OPT-IN, FLAG ONLY — no default, no environment
// spelling. A worker URL must be `https`, or `http` to a loopback address; this names ONE instance's
// worker HOST, exactly, for which plain `http` is accepted because the network it is reached over (a
// private encrypted mesh) is itself the transport protection. The run says so on stderr every time;
// an opt-in that admits nothing (its worker is `https` by now) is a warning line, never a refusal.
//
// 🔴 `-route-origin-owner OWNER=ALIAS` PLACES ONLY SESSIONS THAT RECORDED NO SCOPE (repeatable). Such
// a session goes to the instance the routing table gives the scope of the repo it ran in, when the
// table names that scope; else to ALIAS when that repo's `origin` owner is OWNER; else to the
// default instance (`capture.Decide`). ALIAS must be a configured instance — and, under --run, one
// with a `-worker` — or it is a usage error (exit 2). Neither the table nor the flag places a
// session on an instance this run has no `-worker` for; it goes to the default instead. A session
// that recorded a scope is placed by its scopes alone, as before.
package main

import (
	"crypto/rand"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/ZacxDev/cairn/internal/capture"
	"github.com/ZacxDev/cairn/internal/client"
	"github.com/ZacxDev/cairn/internal/redact"
)

// Exit codes: cairn-capture's OWN table, not the one `cairn -exit-codes` prints (where 3 means
// something else). What each means is stated ONCE, in `internal/capture/README.md` § *`--run`'s
// exit status*; this block only names them.
const (
	exitOK      = 0
	exitError   = 1
	exitUsage   = 2
	exitRefused = 3
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
		plaintext  multi
		claudeRoot = fs.String("claude-root", filepath.Join(home, ".claude", "projects"), "the Claude Code projects root")
		ledgerDir  = fs.String("ledger-dir", "", "the client read-ledger directory (default $XDG_STATE_HOME/cairn/read-ledger)")
		denylist   = fs.String("denylist", "", "a 0600 per-host denylist file (optional)")
		ocBin      = fs.String("opencode-bin", "opencode", "the opencode binary, by name or path")
		ocDisc     = fs.Bool("opencode-discover", false, "also enumerate every project `opencode debug scrap` names "+
			"(unioned with -opencode-project; a project whose worktree is gone is listed through its first existing sandbox, "+
			"else skipped; both counted on stderr)")
		ocDirs      multi
		ownerRoutes multi
	)
	fs.Var(&ocDirs, "opencode-project", "an opencode project directory to enumerate (repeatable; `session list` lists ONE project's roots)")
	fs.Var(&workers, "worker", "--run: [ALIAS=]URL of an instance's worker listener (repeatable; no alias is the default instance)")
	fs.Var(&tokenFiles, "capture-token-file", "--run: [ALIAS=]FILE holding that instance's capture token, mode 0600 (repeatable)")
	fs.Var(&plaintext, "allow-plaintext-host", "--run: [ALIAS=]HOST — accept plain http for that instance's -worker when its URL's "+
		"host is EXACTLY this one, because the network it is reached over is the transport protection (repeatable; no default)")
	fs.Var(&ownerRoutes, "route-origin-owner", "OWNER=ALIAS — a session that recorded no scope, run in a repo whose "+
		"origin URL's owner segment is OWNER (case-insensitive), goes to instance ALIAS, unless the routing table names "+
		"that repo's scope; never to an instance with no -worker (repeatable; no default)")
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
			workers: workers, tokenFiles: tokenFiles, plaintext: plaintext, stateHome: stateHome, claudeRoot: *claudeRoot,
			ocDirs: ocDirs, ocDiscover: *ocDisc, ocBin: *ocBin, ledgerDir: *ledgerDir, denylist: *denylist,
			ownerRoutes: ownerRoutes}, stderr, getenv)
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
	owners, err := capture.ParseOwnerRoutes(ownerRoutes, routing)
	if err != nil {
		fmt.Fprintf(stderr, "cairn-capture: %v\n", err)
		return exitUsage
	}
	runner := capture.ExecRunner{Bin: *ocBin}
	dirs, discRefused := opencodeDirs(*ocDisc, ocDirs, runner, stderr)
	agent := &capture.Agent{
		ClaudeRoot:   *claudeRoot,
		OpencodeDirs: dirs,
		Runner:       runner,
		LedgerDir:    *ledgerDir,
		Routing:      routing,
		OwnerRoutes:  owners,
		Uploadable:   dryUploadable(workers),
		Redactor:     red,
		Log:          stderr,
	}
	if err := agent.DryRun(stdout); err != nil {
		fmt.Fprintf(stderr, "cairn-capture: %v\n", err)
		return exitError
	}
	if discRefused != nil {
		fmt.Fprintf(stderr, "cairn-capture: %s\n", discoveryLine(discRefused))
		return exitRefused
	}
	return exitOK
}

type runConfig struct {
	host, state, hostKey, stateHome string
	maxRequest                      int
	workers, tokenFiles, plaintext  []string
	claudeRoot, ocBin, ledgerDir    string
	denylist                        string
	ocDirs                          []string
	ocDiscover                      bool
	ownerRoutes                     []string
}

// opencodeDirs is the opencode project directories a pass enumerates: `-opencode-project` alone,
// or — under `-opencode-discover` — unioned with every project `opencode debug scrap` names
// ([capture.DiscoverOpencodeDirs]).
//
// 🔴 A DISCOVERY THAT FAILS REFUSES THE DISCOVERED HALF, NEVER THE PASS, AND NEVER READS AS "NO
// PROJECTS" (operator decision). A missing binary, a non-zero exit or an answer that does not parse
// is logged here as a `refused opencode discovery` line and returned as `refused`; the pass still
// captures Claude Code and every explicit `-opencode-project` directory (`dirs` is those alone),
// and the CALLER exits [exitRefused] once the pass is done. 3 rather than 1: it is a refusal that
// RECURS on every run with nothing changed (a broken binary or an answer this build cannot read
// stays broken) and the pass otherwise completed — exactly 3's meaning — while 1 means the pass
// itself could not proceed. And the projects it SKIPS — no existing worktree or sandbox, so
// `session list` cannot reach them — are said on stderr every pass, in one line of counts.
func opencodeDirs(discover bool, explicit []string, runner capture.OpencodeRunner, stderr io.Writer) (dirs []string, refused error) {
	if !discover {
		return explicit, nil
	}
	d, err := capture.DiscoverOpencodeDirs(runner, explicit)
	if err != nil {
		fmt.Fprintf(stderr, "cairn-capture: refused opencode discovery: %v — the projects it would name are not "+
			"captured this pass; Claude Code and every -opencode-project directory still are\n", err)
		return explicit, err
	}
	fmt.Fprintf(stderr, "cairn-capture: opencode discovery: projects=%d via-sandbox=%d skipped-missing-worktree=%d enumerated=%d\n",
		d.Projects, d.ViaSandbox, d.Skipped, len(d.Dirs))
	return d.Dirs, nil
}

// discoveryLine is the run's LAST line when discovery was refused: the cause, its exit, its remedy.
func discoveryLine(refused error) string {
	return fmt.Sprintf("opencode discovery was refused (the `refused opencode discovery` line above), so the projects "+
		"`opencode debug scrap` names were not captured; refused again on every run until it answers (exit %d). "+
		"Check that -opencode-bin runs and that `opencode debug scrap` prints a JSON list of projects", exitRefused)
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
//
// 🔴 `-allow-plaintext-host` IS PER INSTANCE. Each names one alias's worker host; only that alias's
// URL is checked against it. `plain` is every instance whose URL the opt-in admitted, for the run's
// warning line.
//
// ⚠ AN OPT-IN THAT ADMITTED NOTHING IS A WARNING, NOT A REFUSAL — an alias with no `-worker`, or a
// worker that is `https` or loopback anyway. Refusing it (built first) would stop CAPTURE on the day
// a worker moves to `https` and the unit file still carries the flag; the plugins' own client
// (`plugins/internal/endpoint`) takes the same flag and ignores an unused entry. `unused` is those
// warnings. A value that is not a bare host is still a usage error: that is a typo, not a leftover.
func endpoints(c runConfig, routing client.Routing) (eps map[string]capture.Endpoint, plain map[string]string, unused []string, err error) {
	urls, files, allowed := map[string]string{}, map[string]string{}, map[string][]string{}
	for _, a := range c.plaintext {
		alias, host := aliased(a)
		if host == "" || strings.ContainsAny(host, "/@ \t") {
			return nil, nil, nil, fmt.Errorf("-allow-plaintext-host %q is not [ALIAS=]HOST — a bare host name or address, no scheme, port or path", a)
		}
		allowed[alias] = append(allowed[alias], host)
	}
	plain = map[string]string{}
	for _, w := range c.workers {
		alias, raw := aliased(w)
		if _, dup := urls[alias]; dup {
			return nil, nil, nil, fmt.Errorf("-worker names instance %s twice", alias)
		}
		u, usedPlaintext, err := capture.ValidateEndpointAllowing(raw, allowed[alias]...)
		if err != nil {
			return nil, nil, nil, err
		}
		if len(allowed[alias]) > 0 && !usedPlaintext {
			unused = append(unused, fmt.Sprintf("-allow-plaintext-host is set for instance %s, whose -worker does not need it "+
				"(it is https, or a loopback address); the opt-in admits nothing and can be removed", alias))
		}
		if usedPlaintext {
			plain[alias] = u
		}
		urls[alias] = u
	}
	for _, alias := range sortedKeys(mapOf(allowed)) {
		if _, ok := urls[alias]; !ok {
			unused = append(unused, fmt.Sprintf("-allow-plaintext-host names instance %s, which has no -worker %s=<url>; "+
				"the opt-in admits nothing", alias, alias))
		}
	}
	for _, f := range c.tokenFiles {
		alias, path := aliased(f)
		if _, dup := files[alias]; dup {
			return nil, nil, nil, fmt.Errorf("-capture-token-file names instance %s twice", alias)
		}
		files[alias] = path
	}
	if len(urls) == 0 {
		return nil, nil, nil, errors.New("--run needs at least one -worker [ALIAS=]URL")
	}
	known := map[string]bool{}
	for _, a := range routing.Aliases() {
		known[a] = true
	}
	out := map[string]capture.Endpoint{}
	for alias, u := range urls {
		if !known[alias] {
			return nil, nil, nil, fmt.Errorf("-worker names instance %s, which this host's routing does not configure (%s)",
				alias, strings.Join(routing.Aliases(), ", "))
		}
		path, ok := files[alias]
		if !ok {
			return nil, nil, nil, fmt.Errorf("-worker %s has no -capture-token-file %s=<file>", alias, alias)
		}
		tok, err := capture.ReadTokenFile(path)
		if err != nil {
			return nil, nil, nil, err
		}
		out[alias] = capture.Endpoint{URL: u, Token: tok}
	}
	for alias := range files {
		if _, ok := urls[alias]; !ok {
			return nil, nil, nil, fmt.Errorf("-capture-token-file %s has no -worker %s=<url>", alias, alias)
		}
	}
	return out, plain, unused, nil
}

// ownerRoutesFor is `-route-origin-owner` under --run: [capture.ParseOwnerRoutes], and every alias
// it names must also have a `-worker`.
//
// 🔴 A RULE WHOSE INSTANCE HAS NO WORKER IS A USAGE ERROR UNDER --run, not a warning. Unlike an
// unused `-allow-plaintext-host` it is not inert: it MOVES sessions — each one already on the
// default instance would be probed against a destination nothing can reach and sit in
// `withdrawal-pending` on every run, and each new one would be refused on every run.
func ownerRoutesFor(values []string, routing client.Routing, eps map[string]capture.Endpoint) (capture.OwnerRoutes, error) {
	owners, err := capture.ParseOwnerRoutes(values, routing)
	if err != nil {
		return nil, err
	}
	for _, v := range values {
		_, alias, _ := strings.Cut(v, "=")
		if _, ok := eps[alias]; !ok {
			return nil, fmt.Errorf("-route-origin-owner %s names instance %s, which has no -worker %s=<url>", v, alias, alias)
		}
	}
	return owners, nil
}

// uploadable is the aliases `--run` has an upload endpoint for: the repo arm of `capture.Decide`
// places a session on no other (#292's audit, F1 — a table-routed repo whose instance has no
// `-worker` was refused on every run, or left in `withdrawal-pending` for good, at exit 0).
func uploadable(eps map[string]capture.Endpoint) map[string]bool {
	out := map[string]bool{}
	for alias := range eps {
		out[alias] = true
	}
	return out
}

// dryUploadable is the same set for `--dry-run`, which validates no endpoint: the aliases its
// `-worker` flags name, so a dry run given `--run`'s flags prints `--run`'s decisions. With no
// `-worker` at all it restricts nothing — `--run` refuses that case outright, so there is no `--run`
// answer to match, and a dry run then prints what the routing would do with every instance reachable.
func dryUploadable(workers []string) map[string]bool {
	if len(workers) == 0 {
		return nil
	}
	out := map[string]bool{}
	for _, w := range workers {
		alias, _ := aliased(w)
		out[alias] = true
	}
	return out
}

func mapOf(m map[string][]string) map[string]string {
	out := map[string]string{}
	for k := range m {
		out[k] = ""
	}
	return out
}

func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

// runPass is `--run`: ONE capture pass. Its exit codes are `internal/capture/README.md` § *`--run`'s
// exit status*. A refusal never stops the pass early: exit 3 is decided after it has completed and
// the state is saved, from [capture.Recurs]'s counts.
func runPass(c runConfig, stderr io.Writer, getenv func(string) string) int {
	fail := func(err error) int {
		fmt.Fprintf(stderr, "cairn-capture: %v\n", err)
		return exitError
	}
	if c.host == "" || strings.ContainsAny(c.host, " \t\r\n/") {
		fmt.Fprintln(stderr, "cairn-capture: --run needs -host, the label this host's capture token was minted for")
		return exitUsage
	}
	if c.maxRequest < 4096 || c.maxRequest > capture.MaxRequestCeiling {
		fmt.Fprintf(stderr, "cairn-capture: -max-request-bytes must be 4096–%d (the worker listener's own bound on a body)\n",
			capture.MaxRequestCeiling)
		return exitUsage
	}
	routing, err := client.Discover(getenv)
	if err != nil {
		return fail(err)
	}
	eps, plain, unused, err := endpoints(c, routing)
	if err != nil {
		fmt.Fprintf(stderr, "cairn-capture: %v\n", err)
		return exitUsage
	}
	owners, err := ownerRoutesFor(c.ownerRoutes, routing, eps)
	if err != nil {
		fmt.Fprintf(stderr, "cairn-capture: %v\n", err)
		return exitUsage
	}
	for _, line := range unused {
		fmt.Fprintf(stderr, "cairn-capture: WARNING %s\n", line)
	}
	// 🔴 SAID ON EVERY RUN, per instance: the opt-in is a standing statement about a network, and a
	// line in the journal each minute is what keeps it from being forgotten.
	for _, alias := range sortedKeys(plain) {
		fmt.Fprintf(stderr, "cairn-capture: ⚠ instance %s: the capture token and every upload go to %s over PLAIN HTTP "+
			"(-allow-plaintext-host); the network between is the only transport protection\n", alias, plain[alias])
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
	runner := capture.ExecRunner{Bin: c.ocBin}
	dirs, discRefused := opencodeDirs(c.ocDiscover, c.ocDirs, runner, stderr)
	capture.CleanStateTemps(c.state)
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
		OpencodeDirs: dirs,
		Runner:       runner,
		LedgerDir:    c.ledgerDir,
		Routing:      routing,
		OwnerRoutes:  owners,
		Uploadable:   uploadable(eps),
		Redactor:     red,
		Sink:         sink,
		State:        state,
		Log:          stderr,
		// 🔴 THE AGENT SAVES THE STATE, AND ONLY WHEN IT CHANGED: at once where a crash must not lose
		// a decision (an opencode write's plan, a re-check refusal), after a changed session at most
		// every two seconds, and once more when the run ends with unsaved changes — including a run
		// that ends loudly. An idle run writes nothing.
		Checkpoint:      func() error { return state.Save(c.state) },
		CheckpointEvery: 2 * time.Second,
	}
	sum, runErr := agent.Run()
	fmt.Fprintf(stderr, "cairn-capture: run %s omitted=%d recheck-omitted=%d requests=%d\n", sum, sink.Omitted, sink.Rechecked, sink.Requests)
	if runErr != nil {
		return fail(runErr)
	}
	lines := recurringLines(sum, routing.MultiInstance(), c.state)
	if discRefused != nil {
		lines = append(lines, discoveryLine(discRefused))
	}
	if len(lines) > 0 {
		for _, l := range lines {
			fmt.Fprintf(stderr, "cairn-capture: %s\n", l)
		}
		return exitRefused
	}
	return exitOK
}

// recurringLines is what a run that refused a session for a reason that RECURS ([capture.Recurs])
// says last, one line per cause, each with the remedy for THAT cause — or nothing, when the run
// refused nothing that recurs.
//
// 🔴 THE REMEDY IS PER CAUSE BECAUSE NO ONE REMEDY IS RIGHT FOR ALL. Clearing a session's state
// entry is right for an unresumable position on a host routed to ONE instance (measured: it
// un-wedges the session); it does not resolve a root another uploader holds; and on a host routed
// to several instances the entry also records WHERE the earlier upload lives (`instance`, the
// withdrawal obligation of decision 16) and a final hold, so removing it can strand or duplicate
// that upload — for EITHER cause. ⚠ A not-owner refusal can come from a MOVE's destination probe:
// clearing then empties `instance`, the next run ships straight to the destination, is refused
// again, and the copy on the instance the session left is never withdrawn (review round 2). A 422 hold never reaches here ([capture.Recurs]): clearing would re-send bytes the
// pod's re-check refused.
func recurringLines(sum capture.Summary, multiInstance bool, statePath string) []string {
	var out []string
	const doNotClear = "This host routes to several instances: do NOT remove the session's whole entry from the state " +
		"file — it also records which instance holds the session's earlier upload, and any final hold, and removing it " +
		"can strand or duplicate that upload. "
	if n := sum.Unresumable; n > 0 {
		l := fmt.Sprintf("%d session(s) refused because the instance holds a stream at a position this host's state "+
			"cannot resume from (the `refused <session>: the instance holds this stream at …` lines above); refused again "+
			"on every run (exit %d). ", n, exitRefused)
		if multiInstance {
			l += doNotClear + "Resolve it with the instance's operator"
		} else {
			l += "Once the cause is understood (say, the instance's store was restored behind this host), with no run in " +
				"progress, remove that session's entry from the state file " + statePath + "; the next run reads it again " +
				"from the start. The entry also remembers which records the pod's re-check refused, so their bytes may " +
				"be sent once more (the pod refuses them again)"
		}
		out = append(out, l)
	}
	if n := sum.NotOwner; n > 0 {
		l := fmt.Sprintf("%d session(s) refused because the instance holds the session for another uploader "+
			"(the `refused <session>: …held by another uploader (409)…` lines above); refused again on every run "+
			"(exit %d). ", n, exitRefused)
		if multiInstance {
			l += doNotClear
		}
		out = append(out, l+"It is resolved on the instance — the other uploader's copy of that root; clearing this "+
			"host's state does not resolve it")
	}
	return out
}
