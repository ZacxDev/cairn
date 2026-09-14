// Command cairn-server is the Go port of `server/server.py`: the pod that serves
// scoped, per-token-authorised entries.
//
// 🔴 IT IS NOT DEPLOYED BY THIS COMMIT, AND SAYING SO IS PART OF THE COMMIT. The
// Python server remains the oracle; this binary exists so the conformance corpus can
// be replayed against both on the same store and the difference measured. The
// sequence is: this passes the corpus, then both run over one store and byte-identity
// is compared, then the client is ported, then Python is retired — in that order,
// never by declaring one of the steps done early.
package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/ZacxDev/cairn/internal/api"
	"github.com/ZacxDev/cairn/internal/authz"
	"github.com/ZacxDev/cairn/internal/netid"
)

const (
	defaultStore     = "/data"
	defaultTokenFile = "/run/secrets/subsystem-store/token"
	defaultPort      = 8102

	// exitConfig is sysexits.h EX_CONFIG — a misconfiguration, not a crash. A store
	// that came up with a weak token or an unset proxy allowlist is worse than one
	// that did not come up at all, because it looks healthy.
	exitConfig = 78
)

// The two reload verdicts, AS CONSTANTS, BECAUSE THE OPERATOR'S ONLY SIGNAL THAT A
// `kill -HUP` DID ANYTHING IS THIS LINE.
//
// A reload is fire-and-forget: the signal returns immediately whether the file parsed
// or not, so "did my edit take effect" is not answerable from the sending side at all.
// If the two outcomes are not distinguishable in the log, a REFUSED reload is
// indistinguishable from a successful one, and the operator walks away believing a
// credential was revoked when it is still live — strictly worse than the pre-reload
// status quo, where a bad file at least announced itself as a crash loop.
//
// The shared stem is a constant too, so "is this line a reload verdict at all" is one
// question with one answer. A reader that filtered on the two outcomes separately
// would silently stop seeing a THIRD outcome the day one is added.
const (
	reloadPrefix  = "token reload: "
	reloadLoaded  = reloadPrefix + "LOADED"
	reloadRefused = reloadPrefix + "REFUSED"
)

func main() {
	store := flag.String("store", envOr("SUBSYSTEM_STORE_ROOT", defaultStore), "store root")
	host := flag.String("host", envOr("SUBSYSTEM_STORE_HOST", "0.0.0.0"), "listen address")
	port := flag.Int("port", envInt("SUBSYSTEM_STORE_PORT", defaultPort), "listen port")
	tokenFile := flag.String("token-file", envOr("SUBSYSTEM_STORE_TOKEN_FILE", defaultTokenFile),
		"file holding the bearer token SET, ONE ROW PER LINE, current first (mode 0600). "+
			"A row is `<token>` (legacy: unrestricted scope) or "+
			"`<token> <identity> <scope>,<scope>`. FILE FIRST: an agent exec sandbox "+
			"strips environment variables, so $SUBSYSTEM_STORE_TOKEN is the fallback")
	routes := flag.Bool("routes", false,
		"print the declared `<METHOD> <head>` route ledger and exit. 🔴 THIS IS THE "+
			"LEDGER THE CONFORMANCE SUITE READS FOR A NON-PYTHON IMPLEMENTATION: the "+
			"suite discovers the oracle's routes from its source by AST and has no "+
			"equivalent here, so it reads this instead. It is an output of the DISPATCH "+
			"TABLES, never a restatement of them")
	flag.Parse()

	if *routes {
		for _, route := range api.DeclaredRoutes() {
			fmt.Println(route)
		}
		return
	}

	env := environ()
	resolvedTokenFile := *tokenFile
	if resolvedTokenFile != "" && !isFile(resolvedTokenFile) && env["SUBSYSTEM_STORE_TOKEN"] != "" {
		// The default path does not exist and an environment token does: use it, and SAY
		// SO. Falling back silently is how a deployment that lost its secret mount keeps
		// serving on a token nobody meant to be authoritative.
		fmt.Fprintln(os.Stderr, reloadSafe(fmt.Sprintf(
			"subsystem-store-api: token file %s absent; falling back to $SUBSYSTEM_STORE_TOKEN",
			resolvedTokenFile)))
		resolvedTokenFile = ""
	}

	warn := func(line string) { fmt.Fprintln(os.Stderr, reloadSafe(line)) }
	tokens, err := authz.LoadTokens(resolvedTokenFile, env, warn)
	if err != nil {
		// Sanitised for the same reason the fallback notice above is: the token-file
		// guard interpolates a PATH, and a path may contain a separator.
		fmt.Fprintln(os.Stderr, reloadSafe("subsystem-store-api: "+err.Error()))
		os.Exit(exitConfig)
	}
	trusted, err := netid.LoadTrustedProxies(env)
	if err != nil {
		fmt.Fprintln(os.Stderr, reloadSafe("subsystem-store-api: "+err.Error()))
		os.Exit(exitConfig)
	}
	maxFailures, window, lockout, err := netid.LimiterSettings(env)
	if err != nil {
		fmt.Fprintln(os.Stderr, reloadSafe("subsystem-store-api: "+err.Error()))
		os.Exit(exitConfig)
	}

	srv, err := api.New(*store, tokens, trusted, netid.NewRateLimiter(maxFailures, window, lockout))
	if err != nil {
		fmt.Fprintln(os.Stderr, reloadSafe("subsystem-store-api: "+err.Error()))
		os.Exit(exitConfig)
	}

	// 🔴 INSTALLED BEFORE THE LISTENER ACCEPTS, because the window between the first
	// accepted connection and the handler being installed is a window in which a
	// `kill -HUP` is LOST. It is short and it is real.
	//
	// 🔴 AND IN THE DEPLOYED SHAPE THE LOSS IS SILENT RATHER THAN FATAL, which is
	// worse to be in. An ordinary process dies on an unhandled SIGHUP (default
	// disposition: terminate), so the loss announces itself. As PID 1 of a container's
	// namespace the kernel DISCARDS a default-action signal sent from inside that
	// namespace — measured under `unshare --fork --pid`: PID 1 with no handler survived
	// a SIGHUP sent by a child, while the same program at any other pid died 128+1. So
	// `kubectl exec … kill -HUP 1` returns exit 0 having done nothing, and the
	// operator's next move after an exit-0 is to delete the old credential's line
	// believing it was replaced.
	installReload(srv, resolvedTokenFile, env)

	listener, err := net.Listen("tcp", net.JoinHostPort(*host, strconv.Itoa(*port)))
	if err != nil {
		fmt.Fprintln(os.Stderr, reloadSafe("subsystem-store-api: "+err.Error()))
		os.Exit(exitConfig)
	}

	// 🔴 THE STARTUP LINE PRINTS EVERY FINGERPRINT, IN THE ORDER THE FILE LISTS THEM,
	// AND NEVER A TOKEN. It is what makes an overlap rotation checkable: the operator
	// reads these ids, then greps the audit log for the one that should have stopped
	// appearing before deleting its line from the secret.
	//
	// 🔴 `<fingerprint>:<identity>` — the fingerprint FIRST and unchanged in form,
	// because the rotation procedure greps for it. The identity is APPENDED, not
	// substituted: two rows can hold one holder's current and previous credential, so
	// the identity alone cannot tell an operator which line to delete.
	fmt.Println(reloadSafe(fmt.Sprintf(
		"subsystem-store-api: listening on %s:%d store=%s token-ids=%s lockout=%d/%gs->%gs trusted-proxies=%s reload=SIGHUP",
		*host, *port, *store, tokenIDs(tokens), maxFailures,
		window.Seconds(), lockout.Seconds(), joinPrefixes(trusted))))

	server := &http.Server{
		Handler: srv,
		// 🔴 WITHOUT A READ TIMEOUT A HALF-OPEN CONNECTION PINS A CONNECTION SLOT
		// INDEFINITELY. Measured on the oracle: 50 slowloris connections held 50 live
		// threads. A CDN shields the public side; nothing shields a caller that reaches
		// the pod directly, which is the same exposure the client-IP trust rests on.
		ReadHeaderTimeout: 15 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
		log.Fatalf("subsystem-store-api: %v", err)
	}
}

// installReload makes `kill -HUP <pid>` re-read the token file.
//
// 🔴 A PARSE FAILURE HERE MUST NOT TAKE THE SERVER DOWN, AND THAT IS THE WHOLE
// FEATURE. At STARTUP a malformed file exits, because there is nothing to fall back to
// and a store that came up serving nothing looks healthy while being useless. On
// RELOAD there IS something to fall back to — the table that has been authorising every
// request until this instant — so exiting would turn a typo in a secret into a read
// outage, total and lasting until a human notices.
//
// 🔴 THE VALIDATION IS THE SAME LOADER, NOT A SECOND COPY OF IT. Every guard in that
// ladder has to hold on a reloaded file exactly as it does on a loaded one, and the
// only way to keep two implementations of that predicate in step is to have one. A
// reload path with its own parser is the shape where the migration guards silently stop
// applying to the only file anybody edits after day one.
func installReload(srv *api.Server, tokenFile string, env map[string]string) {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGHUP)
	go func() {
		for range signals {
			previous := srv.Tokens()
			records, err := authz.LoadTokens(tokenFile, env, func(line string) {
				fmt.Fprintln(os.Stderr, reloadSafe(line))
			})
			if err != nil {
				// Read BEFORE the attempt is reported: on refusal the previous table is
				// the one that keeps serving, and the operator needs to be told what
				// that is — "refused" without "and these are still live" leaves them
				// guessing whether a revocation landed.
				fmt.Println(reloadSafe(fmt.Sprintf(
					"subsystem-store-api: %s — the token source did not parse (%s). NOTHING CHANGED: still serving the %d previously loaded identities [%s]. Fix the file and send SIGHUP again",
					reloadRefused, err.Error(), len(previous), tokenIDs(previous))))
				continue
			}
			srv.SetTokens(records)
			fmt.Println(reloadSafe(fmt.Sprintf(
				"subsystem-store-api: %s %d identities [%s] (was %d [%s])",
				reloadLoaded, len(records), tokenIDs(records), len(previous), tokenIDs(previous))))
		}
	}()
}

func tokenIDs(records []authz.TokenRecord) string {
	parts := make([]string, 0, len(records))
	for _, record := range records {
		parts = append(parts, record.Fingerprint()+":"+record.Identity)
	}
	return strings.Join(parts, ",")
}

func joinPrefixes[T fmt.Stringer](items []T) string {
	parts := make([]string, 0, len(items))
	for _, item := range items {
		parts = append(parts, item.String())
	}
	return strings.Join(parts, ",")
}

// reloadSafe strips anything that could forge a LINE BOUNDARY on the operator stream.
//
// ⚠ IT IS THE RESIDUAL, NOT THE FIX, and saying which is the point. The leak it used
// to be asked to cover — a token echoed by a guard — is fixed at the GUARDS, because
// those messages are also what the startup path prints and a sanitiser here would have
// left the exit path publishing the credential exactly as before.
//
// What is left is text this program does not author: an OS error naming a path, or a
// genuine loader bug, reaching a stream that is read by machine. A newline in there is
// a second, syntactically perfect log line of somebody else's choosing.
//
// 🔴 "A LINE BOUNDARY" IS WHATEVER THE READER SPLITS ON, NOT `\n`. A C0/C1-only class
// is narrower than that sentence: U+2028 and U+2029 pass through it and CPython's
// `str.splitlines()` — the idiom the tests read this stream with — then reports TWO
// lines, the second a fabricated audit record. Measured on the oracle.
//
// Control characters become `?`; SPACES ARE LEFT ALONE, because a refusal is prose
// rather than a whitespace-delimited field record and folding its spaces would make
// every reload line unreadable.
func reloadSafe(line string) string {
	var b strings.Builder
	for _, r := range line {
		switch {
		case r <= 0x1f, r >= 0x7f && r <= 0x9f, r == 0x2028, r == 0x2029:
			b.WriteByte('?')
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func envOr(name, fallback string) string {
	if value, present := os.LookupEnv(name); present && value != "" {
		return value
	}
	return fallback
}

func envInt(name string, fallback int) int {
	raw, present := os.LookupEnv(name)
	if !present || raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		fmt.Fprintf(os.Stderr, "subsystem-store-api: %s must be a number, got %q\n", name, raw)
		os.Exit(exitConfig)
	}
	return value
}

func environ() map[string]string {
	out := map[string]string{}
	for _, entry := range os.Environ() {
		if key, value, found := strings.Cut(entry, "="); found {
			out[key] = value
		}
	}
	return out
}

func isFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
