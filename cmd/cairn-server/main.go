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
	"context"
	"errors"
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
	"github.com/ZacxDev/cairn/internal/control"
	"github.com/ZacxDev/cairn/internal/control/tokenfile"
	"github.com/ZacxDev/cairn/internal/identity"
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

// refreshInterval is `api.AuthorityRefreshInterval`, and it is a VAR rather than a
// const for exactly one reason — stated here because a test hook inside a deployed
// program is a cost that has to be earned.
//
// 🔴 THE LOOP BELOW IS THE ONLY MECHANISM BOUNDING THE DIVERGENCE `tokenfile` DECLARES
// IN ITS PACKAGE DOC, AND ITS ONLY OBSERVABLE IS A SCOPE THAT APPEARS. There is no
// counter, no log line per refresh and no route reporting the epoch (deliberately —
// see `internal/control/README.md`), so the one way to watch the loop work is to
// create a directory out of band and wait for a read to answer. At 30 seconds that is 30
// seconds in every `go test ./...` and 30 minutes across the mutation battery, which
// is how a gate ends up not existing at all: `main_test.go` re-executes THIS binary as
// the server with a short interval instead, so deleting the goroutine is a RED test
// rather than a comment nobody checks.
//
// Nothing in the serving path writes it: no flag, no environment variable, and
// `main_test.go` is the only assignment in the package. An operator cannot reach it,
// which is the difference between this and widening the pod's configuration surface
// for a test's convenience.
var refreshInterval = api.AuthorityRefreshInterval

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
	// 🔴 `authz.IsTokenFile`, NOT A LOCAL COPY. This test and the loader's guard 2 decide
	// the SAME question, and the local `!IsDir()` version they used to share accepted a
	// character device — so `/dev/null` at the default path took this fallback away and
	// exited 78 where the oracle falls back and serves. See authz.IsTokenFile.
	if resolvedTokenFile != "" && !authz.IsTokenFile(resolvedTokenFile) && env["SUBSYSTEM_STORE_TOKEN"] != "" {
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

	// 🔴 A STORE ROOT THAT WILL NOT ENUMERATE IS A REFUSAL TO START, AND THE MESSAGE SAYS
	// SO IN THE OPERATOR'S TERMS RATHER THAN THE PROJECTION'S. `api.New` materializes the
	// authority, and the token-file adapter reads the store root to learn which scopes
	// exist — so an unmounted volume surfaces here as a projection failure whose text is
	// about a control plane the operator has never heard of. `tokenfile.ErrStoreRootUnreadable`
	// is exported so this line can name the volume instead. The decision to refuse rather
	// than come up serving an enumeration known to be empty is argued at
	// `tokenfile.Source.Model`; 78 is EX_CONFIG, which this program already uses for
	// "came up misconfigured is worse than did not come up".
	srv, err := api.New(*store, tokens, trusted, netid.NewRateLimiter(maxFailures, window, lockout))
	if err != nil {
		if errors.Is(err, tokenfile.ErrStoreRootUnreadable) {
			fmt.Fprintln(os.Stderr, reloadSafe(fmt.Sprintf(
				"subsystem-store-api: the store root %s cannot be enumerated (%s), so the authority cannot say which "+
					"scopes exist and every read would answer 503 anyway. Refusing to start; mount the volume and restart",
				*store, err.Error())))
			os.Exit(exitConfig)
		}
		fmt.Fprintln(os.Stderr, reloadSafe("subsystem-store-api: "+err.Error()))
		os.Exit(exitConfig)
	}

	// 🔴 IDENTITY IS CONFIGURED BEFORE THE LISTENER ACCEPTS, AND A BROKEN CONFIGURATION
	// EXITS 78 RATHER THAN DISABLING A BACKEND. `identity.FromEnvironment` builds the
	// machine-token backend always and each of the other two only if its environment
	// was touched at all — so a deployment that sets nothing gets exactly the chain
	// `api.New` already installed, and a deployment that sets HALF of one gets a crash
	// loop naming what is missing. The dangerous alternative is the one that reads
	// sensibly: "build it if the required fields are present" gives an operator who
	// forgot the shared secret a pod that comes up healthy with a backend they believe
	// is live and that authenticates nobody.
	//
	// 🔴 AND THE TRUSTED-HEADER BACKEND IS NEVER REACHED BY DEFAULT. It requires an
	// explicit `CAIRN_TRUSTED_HEADER_PROXY_FRONTED`, and `identity.NewTrustedHeader`
	// refuses without a source check on top of that. A pod that is reachable directly
	// and has this backend armed is an authentication bypass for every user in the
	// control plane, which is why arming it takes two deliberate settings and not one.
	identities, supabase, err := identity.FromEnvironment(env, srv.AuthorityView())
	if err != nil {
		fmt.Fprintln(os.Stderr, reloadSafe("subsystem-store-api: identity: "+err.Error()))
		os.Exit(exitConfig)
	}
	if err := srv.UseAuthenticator(identities); err != nil {
		fmt.Fprintln(os.Stderr, reloadSafe("subsystem-store-api: identity: "+err.Error()))
		os.Exit(exitConfig)
	}
	if supabase != nil {
		// 🔴 THE FIRST FETCH IS AT STARTUP AND ITS FAILURE IS FATAL, WHERE EVERY LATER
		// ONE IS NOT. The asymmetry is the same one `control.Cache` draws between a cold
		// start and an outage: a key set that has never fetched can verify nothing, so a
		// pod that came up that way would refuse every sign-in while looking healthy —
		// and the operator's only signal would be users reporting 401s. After one
		// success there is last-known-good to serve, and a provider outage must not stop
		// reads for sessions already issued.
		//
		// ⚠ THE DEADLINE IS THE FETCH'S OWN. `NewKeySet` gives its client a timeout, so
		// an unreachable provider fails here in seconds rather than hanging a pod that
		// would otherwise have started.
		if err := supabase.RefreshKeys(context.Background()); err != nil {
			fmt.Fprintln(os.Stderr, reloadSafe(fmt.Sprintf(
				"subsystem-store-api: identity: the Supabase key set could not be fetched at startup (%s), so no session could be verified and every sign-in would be refused. Refusing to start",
				err.Error())))
			os.Exit(exitConfig)
		}
		go func() {
			if err := supabase.RunKeyRefresh(context.Background()); err != nil {
				fmt.Fprintln(os.Stderr, reloadSafe("subsystem-store-api: identity: the JWKS refresh loop stopped: "+err.Error()))
			}
		}()
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

	// 🔴 THE AUTHORITY'S TIMER, AND WHAT IT IS ACTUALLY FOR. The token table refreshes
	// on every reload, so the credentials and their allowlists are never older than the
	// last SIGHUP. What ages without one is the SCOPE ENUMERATION the token-file
	// adapter reads off the filesystem: a scope directory created OUT OF BAND —
	// `server/seed.sh` seeds through `kubectl exec … tar -xf -` — is not visible to a
	// bare (unrestricted) row until the next materialization, because the control plane
	// has no unrestricted principal to answer with. See the divergence declared in
	// `tokenfile`'s package doc.
	//
	// ⚠ THE TIMER BOUNDS THAT WINDOW. NOTHING REPORTS IT, AND THIS SENTENCE CLAIMED
	// `Staleness` DID. `control.Cache.Staleness()` is a renderable value with no caller
	// outside the tests — this program does not print it, `internal/doctor` does not read
	// it, and no route carries it — so the window, and a `degraded`/`stale` authority
	// with it, is bounded and SILENT. The surface is deferred because the one place to
	// print it is the startup banner below, which `tests/dualrun/harness.py` compares
	// between the two servers; the closing condition is in `tokenfile`'s package doc.
	//
	// ⚠ SIGHUP IS DELIBERATELY NOT ONE OF THESE TRIGGERS. The reload path above already
	// re-materializes as part of publishing the new table, and a second registration
	// would refresh twice for one signal — `control.RefreshTriggers` documents that two
	// `signal.Notify` channels BOTH receive it, so this would be an extra refresh, not
	// a missed one. One trigger, one place.
	//
	// 🔴 AND IT IS GATED FROM OUTSIDE THIS PROCESS, BECAUSE NOTHING INSIDE IT CAN BE.
	// `TestTheBinarysOwnTimerIsWhatClosesTheDivergence` runs this binary, creates a
	// scope directory behind its back, sends no signal and calls no refresh, and
	// requires the read to start answering. Deleting this goroutine — or emptying its
	// trigger set — is that test going red, which is the only claim that could not be
	// made while the loop lived in a package with no test files at all.
	go func() {
		if err := srv.Authority().Run(context.Background(),
			control.RefreshTriggers{Interval: refreshInterval}); err != nil {
			fmt.Fprintln(os.Stderr, reloadSafe("subsystem-store-api: authority refresh loop stopped: "+err.Error()))
		}
	}()

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
			// 🔴 THE RE-MATERIALIZATION IS PART OF THE RELOAD, AND ITS FAILURE IS
			// REPORTED AS A REFUSAL RATHER THAN AS A SUCCESS WITH A FOOTNOTE. The file
			// parsed; the authority did not rebuild from it, so the table the operator
			// just edited is NOT what is authorising requests. The previous model keeps
			// serving (see `control.Cache.Refresh`), which is the same fallback a
			// refused parse gets.
			//
			// 🔴 BUT THE LINE MUST NOT SAY "NOTHING CHANGED", AND IT DID. `SetTokens`
			// PUBLISHES THE TABLE BEFORE REFRESHING — deliberately, so the next refresh
			// reads the new one — so on this branch the table HAS been swapped and only
			// the projection is stale. Two things follow that "nothing changed" gets
			// exactly backwards: the authority is unchanged (true) while the input is
			// not (false), and the next TIMER tick re-projects the new table with no
			// operator action at all, so "send SIGHUP again" described a step that is
			// not required. Both halves are stated instead, because an operator who
			// believes nothing changed will re-edit the file against a copy that is no
			// longer what the pod holds.
			if err := srv.SetTokens(records); err != nil {
				fmt.Println(reloadSafe(fmt.Sprintf(
					"subsystem-store-api: %s — the token source parsed but the authority did not rebuild from it (%s). "+
						"THE TABLE IS ALREADY SWAPPED: the %d new identities [%s] are published, and the next refresh (within %s) "+
						"re-projects from them with no further signal — and fails the same way until the cause is fixed. "+
						"The AUTHORITY is unchanged: it keeps serving the %d previously loaded identities [%s]. "+
						"Fix the cause; SIGHUP only to retry without waiting",
					reloadRefused, err.Error(),
					len(records), tokenIDs(records), refreshInterval,
					len(previous), tokenIDs(previous))))
				continue
			}
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
