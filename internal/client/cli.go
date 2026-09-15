package client

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/ZacxDev/cairn/internal/doctor"
	"github.com/ZacxDev/cairn/internal/report"
)

// Verb is one subcommand: what it is called, whether it WRITES the store, and the flags it takes.
//
// 🔴 `Writes` IS NOT DECORATION. A `*StoreUnreachable` escaping a READ maps to exit 3
// ("store-unreachable, no cache"), which reads as "nothing was shown". For a WRITE the honest
// code is 7 — the record was NOT made — and the two must not share a number, because the whole
// point of write-through is that a caller can tell a failed write from a stale read.
type Verb struct {
	Name   string
	Writes bool
	Help   string
	// Flags is the flag set this verb accepts, used by the parser AND by `DeclaredVerbs`.
	Flags []string
	Run   func(Env, Options) (int, error)
}

// commonReadFlags is `common(sp)` on the Python side: the three every read verb takes.
var commonReadFlags = []string{"--scope", "--repo", "--no-sync"}

// Verbs is the dispatch table, and 🔴 IT IS THE TABLE THE PARSER DISPATCHES FROM — not a
// description of one. A verb with no row here is unreachable, and a row with no verb is a verb the
// parser rejects, so the two cannot disagree.
//
// 🔴 IT IS ALSO THE GO SIDE'S ANSWER TO THE CAPABILITY LEDGER'S BLIND SPOT. `tests/testlib/
// capability_ledger.py` discovers the CLI's verbs by asking the PYTHON parser what subcommands it
// has; there is no equivalent for a compiled binary, so a Go-only verb (or a Go client that
// silently LOST one) would leave that gate green. `cairn -verbs` prints this table and
// `tests/test_capability_ledger.py::TestTheGoClientDeclaresTheSameVerbs` reads it out of the
// RUNNING binary — the same shape, and for the same reason, as `cairn-server -routes`.
func Verbs() []Verb {
	return []Verb{
		{Name: "sync", Help: "refresh the local cache from the pod",
			// ⚠ `sync` TAKES `--scope` AND IGNORES IT, WHICH IS THE ORACLE'S OWN SHAPE. The
			// flag is declared on the subparser and `cmd_sync` passes `scope=None`
			// regardless — see `Sync` for the measured reason the cache may never be
			// narrowed. Removing the flag here would make `cairn sync --scope x` a usage
			// error on one client and a no-op on the other, which is a parity difference
			// introduced by tidying.
			Flags: []string{"--scope"}, Run: Sync},
		{Name: "recall", Help: "the scope's digest",
			Flags: append(append([]string{}, commonReadFlags...),
				"--mode", "--ref", "--list", "--limit", "--page"),
			Run: func(e Env, o Options) (int, error) { return Report(e, o, false) }},
		{Name: "search", Help: "search the scope for hunks",
			Flags: append(append([]string{}, commonReadFlags...), "--all-scopes"),
			Run:   func(e Env, o Options) (int, error) { return Report(e, o, true) }},
		{Name: "validate", Help: "parse-check the cached entries",
			Flags: commonReadFlags, Run: Validate},
		{Name: "ls-entries", Help: "one `<scope>/<entry>.md` per line",
			Flags: commonReadFlags, Run: LsEntries},
		// 🔴 `doctor` TAKES NO `--scope`/`--repo`. It asks about THIS HOST and THIS
		// CREDENTIAL, not about a scope, and a `--scope` here would invite the
		// filtered-cache mistake `Sync` documents. `--no-sync` it does take: "diagnose
		// without touching the network" is a real mode, and it turns every pod-dependent
		// check UNMEASURED with that as the stated reason rather than reporting a zero.
		{Name: "doctor", Help: "one call: pod reachability, credential, cache stamp, counts, " +
			"scope visibility, and which store this host's reader resolves",
			Flags: []string{"--json", "--no-sync"}, Run: Doctor},
		// 🔴 NEITHER WRITE VERB TAKES `--no-sync`. It would be meaningless on `append` and
		// actively harmful on `put` (see `Put` on deriving a precondition from bytes we did
		// not confirm), so it is absent rather than refused.
		{Name: "append", Writes: true,
			Help:  "append ONE dated, attributed bullet to an entry (writes to the pod)",
			Flags: []string{"--scope", "--repo", "--ref", "--text", "--session"}, Run: Append},
		{Name: "put", Writes: true,
			Help:  "replace a whole entry behind an If-Match precondition",
			Flags: []string{"--scope", "--repo", "--ref", "--file", "--if-match"}, Run: Put},
		{Name: "create", Writes: true,
			Help:  "create a NEW entry that does not exist yet (refuses to overwrite)",
			Flags: []string{"--scope", "--repo", "--ref", "--file"}, Run: Create},
	}
}

// requiredFlags is which flags a verb refuses to run without. Kept beside the table rather than
// inside it because it is a property of THREE verbs and spelling it per row would put an empty
// slice on six of them.
var requiredFlags = map[string][]string{
	// 🔴 `--session` IS REQUIRED WITH NO DEFAULT AND NO ENV FALLBACK. The server refuses a
	// request without it, and every appended bullet must record the actor AND the session. A
	// default would be a value nobody chose attached to a durable record; an env fallback would
	// silently attribute one agent's bullet to whatever the shell last exported.
	"append": {"--ref", "--text", "--session"},
	"put":    {"--ref", "--file"},
	"create": {"--ref", "--file"},
}

// DeclaredVerbs is `<name> <writes|reads>` per line, sorted, for a ledger to read out of the
// running binary.
func DeclaredVerbs() []string {
	var out []string
	for _, v := range Verbs() {
		effect := "reads"
		if v.Writes {
			effect = "writes"
		}
		out = append(out, v.Name+" "+effect)
	}
	sort.Strings(out)
	return out
}

// usageError is a command line this client refuses. 🔴 IT IS EXIT 2, WHICH IS THE ONE PART OF THE
// USAGE CONTRACT BOTH CLIENTS SHARE. The MESSAGE is not: the Python client's usage text is
// argparse's, and reproducing argparse's wording, its `usage:` line and its `--help` layout in Go
// would be a large second implementation of a library nobody reads twice. That difference is
// declared in `tests/parity/README.md` and the parity harness compares the CODE on these rows.
type usageError struct{ message string }

func (e *usageError) Error() string { return e.message }

func usagef(format string, args ...any) error {
	return &usageError{message: fmt.Sprintf(format, args...)}
}

// Parse turns argv into a verb and its options.
//
// ⚠ IT IS NOT ARGPARSE AND DOES NOT PRETEND TO BE. Three behaviours are deliberately absent, each
// one a place the two clients differ on a malformed command line and agree on a well-formed one:
// argparse's PREFIX ABBREVIATION (`--sc` for `--scope`), its `--help`/`usage:` rendering, and its
// exact refusal wording. What IS reproduced is the accepted SURFACE — which flags exist on which
// verb, `--flag value` and `--flag=value`, global flags before the verb — and the exit code.
func Parse(argv []string) (Verb, Options, error) {
	opts := Options{
		Cache:   DefaultCacheRoot(),
		Timeout: DefaultTimeout,
		Repo:    ".",
		Mode:    report.DefaultMode,
	}
	i := 0
	// Global flags, BEFORE the verb — which is also argparse's rule for a parser with
	// subparsers, so this is a shared constraint rather than a simplification.
	for i < len(argv) && strings.HasPrefix(argv[i], "--") {
		name, value, hasValue := strings.Cut(argv[i], "=")
		take := func() (string, error) {
			if hasValue {
				return value, nil
			}
			if i+1 >= len(argv) {
				return "", usagef("cairn: %s expects a value", name)
			}
			i++
			return argv[i], nil
		}
		switch name {
		case "--cache":
			v, err := take()
			if err != nil {
				return Verb{}, opts, err
			}
			opts.Cache = expandUser(v)
		case "--timeout":
			v, err := take()
			if err != nil {
				return Verb{}, opts, err
			}
			n, convErr := strconv.Atoi(v)
			if convErr != nil {
				return Verb{}, opts, usagef("cairn: --timeout expects an integer, got %q", v)
			}
			// 🔴 THE SENTINEL IS RESOLVED HERE AND NOWHERE ELSE. On the Python side
			// `--timeout` defaults to `None` so `who` can tell "not given" from an explicit
			// value, which gave every store call site a chance to forget the resolution —
			// and mutating all four to pass the raw `None` through kept the whole suite
			// green, because `None` reaches the socket as NO timeout. Here the default is
			// resolved at parse time, so there is no sentinel to forget; a non-positive
			// value still reaches `UnboundedTimeoutReason`, which refuses it outright.
			opts.Timeout = n
		default:
			return Verb{}, opts, usagef("cairn: unrecognised global option %q", name)
		}
		i++
	}
	if i >= len(argv) {
		return Verb{}, opts, usagef("cairn: a subcommand is required (one of: %s)",
			strings.Join(verbNames(), ", "))
	}
	verbName := argv[i]
	i++
	var verb Verb
	for _, v := range Verbs() {
		if v.Name == verbName {
			verb = v
			break
		}
	}
	if verb.Name == "" {
		return Verb{}, opts, usagef("cairn: unknown subcommand %q (one of: %s)",
			verbName, strings.Join(verbNames(), ", "))
	}

	accepted := map[string]struct{}{}
	for _, f := range verb.Flags {
		accepted[f] = struct{}{}
	}
	seen := map[string]struct{}{}
	positionalsWanted := 0
	if verb.Name == "search" {
		positionalsWanted = 1
	}
	var positionals []string
	for i < len(argv) {
		arg := argv[i]
		if !strings.HasPrefix(arg, "--") {
			positionals = append(positionals, arg)
			i++
			continue
		}
		name, value, hasValue := strings.Cut(arg, "=")
		if _, ok := accepted[name]; !ok {
			return verb, opts, usagef("cairn: %s does not accept %q (accepts: %s)",
				verb.Name, name, strings.Join(verb.Flags, ", "))
		}
		seen[name] = struct{}{}
		needsValue := name != "--no-sync" && name != "--list" && name != "--all-scopes" &&
			name != "--json"
		if !needsValue {
			if hasValue {
				return verb, opts, usagef("cairn: %s takes no value", name)
			}
			switch name {
			case "--no-sync":
				opts.NoSync = true
			case "--list":
				opts.List = true
			case "--all-scopes":
				opts.AllScopes = true
			case "--json":
				opts.JSON = true
			}
			i++
			continue
		}
		if !hasValue {
			if i+1 >= len(argv) {
				return verb, opts, usagef("cairn: %s expects a value", name)
			}
			i++
			value = argv[i]
		}
		switch name {
		case "--scope":
			opts.Scope = value
		case "--repo":
			opts.Repo = value
		case "--mode":
			opts.Mode = value
		case "--ref":
			opts.Ref, opts.HasRef = value, true
		case "--limit":
			n, err := strconv.Atoi(value)
			if err != nil {
				return verb, opts, usagef("cairn: --limit expects an integer, got %q", value)
			}
			opts.Limit = &n
		case "--page":
			n, err := strconv.Atoi(value)
			if err != nil {
				return verb, opts, usagef("cairn: --page expects an integer, got %q", value)
			}
			opts.Page = &n
		case "--text":
			opts.Text = value
		case "--session":
			opts.Session = value
		case "--file":
			opts.File = value
		case "--if-match":
			opts.IfMatch = value
		}
		i++
	}
	for _, need := range requiredFlags[verb.Name] {
		if _, ok := seen[need]; !ok {
			return verb, opts, usagef("cairn: %s requires %s", verb.Name, need)
		}
	}
	if len(positionals) != positionalsWanted {
		return verb, opts, usagef("cairn: %s takes %d positional argument(s), got %d",
			verb.Name, positionalsWanted, len(positionals))
	}
	if positionalsWanted == 1 {
		opts.Query = positionals[0]
	}
	return verb, opts, nil
}

func verbNames() []string {
	var out []string
	for _, v := range Verbs() {
		out = append(out, v.Name)
	}
	return out
}

// Run is the whole client: parse, dispatch, and map every escaping error to its own exit code.
func Run(env Env, argv []string) int {
	verb, opts, err := Parse(argv)
	if err != nil {
		fmt.Fprintln(env.Stderr, err)
		return ExitUsage
	}
	if opts.Cache == "" {
		// 🔴 A HOME THAT CANNOT BE RESOLVED IS NAMED, NOT GUESSED. The oracle's
		// `Path.home()` raises; a client that fell back to a relative path would write a
		// cache into whatever directory it happened to start in and then report it as the
		// host's store.
		fmt.Fprintln(env.Stderr, "cairn: cannot resolve a cache root — neither $HOME nor "+
			"CAIRN_CACHE_ROOT is set, so there is nowhere to read or write the store cache.")
		return ExitUsage
	}
	code, runErr := verb.Run(env, opts)
	if runErr == nil {
		return code
	}

	var refused *WriteRefused
	if errors.As(runErr, &refused) {
		// 🔴 THE STORE ANSWERED, AND THE ANSWER IS NO. Printed with the server's own
		// `X-Store-Status`, because that token is what distinguishes the four 404s from each
		// other (`scope-unknown` vs `ref-unknown`) and a 405 (`read-only`) — which means
		// "the write path is not deployed on the running image", i.e. an operator problem
		// and not a caller problem, and which reads exactly like a wrong URL.
		fmt.Fprintf(env.Stderr, "🔴 cairn: the store REFUSED the write [%s] — %s\n",
			refused.Status, refused.Detail)
		return refused.ExitCode
	}
	var corruptErr *StoreCorrupt
	if errors.As(runErr, &corruptErr) {
		// 🔴 LOUD, and never absorbed into "served from cache".
		fmt.Fprintf(env.Stderr, "🔴 cairn: REFUSED the archive — %s\n", corruptErr.Reason)
		return ExitCorrupt
	}
	var unreachableErr *StoreUnreachable
	if errors.As(runErr, &unreachableErr) {
		// 🔴 A WRITE AND A READ DO NOT SHARE A CODE HERE. Exit 3 is "store-unreachable, no
		// cache" — a statement about what was DISPLAYED. On a write nothing was displayed
		// and, more importantly, nothing was RECORDED; a caller that read 3 as "the read
		// came back empty" would carry on believing the bullet landed.
		if verb.Writes {
			fmt.Fprintf(env.Stderr, "🔴 cairn: the write did NOT happen — %s\n"+
				"          Nothing was queued and nothing was written locally. "+
				"Re-run when the store is reachable.\n", unreachableErr.Reason)
			return ExitWriteUnreachable
		}
		// Reached only by a config failure outside `ResolveState`; still named.
		fmt.Fprintf(env.Stderr, "🔴 cairn: %s — %s\n", StateNoCache, unreachableErr.Reason)
		return ExitUnreachableNoCache
	}
	// ⚠ EVERYTHING ELSE IS A READER ERROR — a missing store root, an unreadable entry — and it
	// exits 3, which is what the reader's own contract says those mean. The oracle reaches the
	// same number by a different route: those raise out of `cmd_recall` and the module's
	// `_exit_for` is never consulted, so its CLI wrapper prints a traceback at exit 1. That is
	// a genuine divergence and it is in the good direction; it is declared in
	// `tests/parity/README.md` rather than reproduced.
	fmt.Fprintf(env.Stderr, "🔴 cairn: %s\n", runErr)
	return ExitUnreachableNoCache
}

// Doctor is the one call for every fact a reader otherwise assembles by hand.
func Doctor(env Env, opts Options) (int, error) {
	resolved := ResolveReadStore("")

	// 🔴 THE CONFIG LOAD HAPPENS EVEN UNDER `--no-sync`, AND THAT IS A FIX. It is a local file
	// read, so "is a credential configured on this host" is answerable with the network
	// switched off — and the first version skipped it, which made `doctor --no-sync` report
	// `token PROBLEM: no token is configured` on a host whose token was sitting in its config
	// file unread. A check that says PROBLEM about something it never looked at is the same
	// defect as one that says OK about it.
	cfg, cfgErr := LoadConfig()
	tokenReason := ""
	if cfgErr != nil {
		tokenReason = cfgErr.Error()
	}

	var pod doctor.PodFacts
	switch {
	case opts.NoSync:
		pod = doctor.PodFacts{Reason: "--no-sync was given, so the store was never contacted"}
	case cfgErr != nil:
		pod = doctor.PodFacts{Reason: cfgErr.Error()}
	default:
		pod = ProbeStore(cfg, opts.Timeout)
	}
	if err := pod.Validate(); err != nil {
		return 0, err
	}

	checks := doctor.Collect(doctor.Inputs{
		ResolvedRoot:   resolved.Root,
		StampLines:     resolved.Stamp,
		StampReason:    resolved.Reason,
		CacheRoot:      opts.Cache,
		MirrorRoot:     MirrorRoot(),
		Pod:            pod,
		Token:          cfg.Token,
		HasToken:       cfgErr == nil,
		TokenReason:    tokenReason,
		IdentityRemedy: IdentityRemedy,
	})
	if opts.JSON {
		fmt.Fprintln(env.Stdout, doctor.JSON(checks))
	} else {
		fmt.Fprintln(env.Stdout, doctor.Render(checks))
	}
	return doctor.ExitCode(checks), nil
}

// MirrorRoot is the frozen pre-cutover store, if this deployment has one.
//
// 🔴 UNSET IS THE DEFAULT, and `doctor` reports NOT-OBSERVABLE rather than OK for it. A
// deployment that migrated from a local store points this at the old root so `doctor` can confirm
// its entry files are read-only; one that never had a local store has nothing to name, and
// claiming "it does not exist" about a path nobody configured would be a statement about the
// operator's disk that this code cannot support.
func MirrorRoot() string {
	raw := strings.TrimSpace(os.Getenv("CAIRN_MIRROR_ROOT"))
	if raw == "" {
		return ""
	}
	return expandUser(raw)
}

// IdentityRemedy is how an operator reads the identity and the DECLARED allowlist behind a token.
//
// 🔴 IT IS A REMEDY, NOT A STEP THIS COMMAND TAKES: `doctor` never reads a secret and never
// shells `kubectl`.
const IdentityRemedy = "`kubectl -n subsystem-store exec deploy/subsystem-store-api -- " +
	"cut -d' ' -f2,3 /run/secrets/subsystem-store/token` " +
	"(fields 2 and 3 are the identity and the allowlist; field 1 is the secret)"
