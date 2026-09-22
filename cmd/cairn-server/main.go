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
	"github.com/ZacxDev/cairn/internal/envalias"
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

	// 🔴 THERE IS NO SECOND FAILURE CODE, AND THE ONE THAT WAS HERE WAS DELETED RATHER
	// THAN NARROWED. `exitDataErr = 65` (sysexits.h EX_DATAERR) was defined as "the pod is
	// configured correctly and the REQUEST was refused" — a typo in a `-subject`, a scope
	// name already taken — so that an operator reading exit codes could tell that from a
	// missing journal path. It did not do that: `runCreateUser` returned 65 for EVERY
	// `control.ProvisionUser` error, including `reading the control journal`, which is an
	// unreadable or corrupt journal and is this file's own definition of 78.
	//
	// Making it honest needs a classifier `internal/control` does not have: `Append`
	// returns a plain `fmt.Errorf` both for a batch the model refused (a request fault)
	// and for a failed `write`/`flock`/`Sync` (a deployment fault), so narrowing means
	// adding sentinels to the journal for the benefit of a code nothing reads.
	//
	// 🔴 AND NOTHING READS IT, MEASURED RATHER THAN ASSUMED — `AGENTS.md`'s `{0, 9}`
	// OVERLAP RULE IS ABOUT A DIFFERENT PROGRAM AND DOES NOT APPLY. The printed
	// exit-code contract belongs to the CLIENT: `cmd/cairn` registers an `exit-codes`
	// flag and `internal/client/exit.go` is the table it prints. THIS program registers
	// five flags — `store`, `host`, `port`, `token-file`, `routes` — plus
	// `-create-user`'s six, and no exit-code flag among them; measured at `e11c3a7` by
	// reading every file under `cmd/cairn-server/` in that tree (positive control: the
	// same sweep hits `-routes` in three of them). So this program declares its exit
	// codes to nothing, and no runbook, test or script branches on 65. A distinction
	// with no consumer, no gate and a known-wrong classification is worth less than the
	// two true codes left: 0, and 78 for every refusal to act.
	//
	// ⚠ Do not re-run that sweep as a LITERAL search of this tree and expect zero: the
	// sentence above names the flag, so the string is now in this file. The claim is
	// about what the program REGISTERS, which is the list six lines up.
	//
	// What the operator needs is on stderr and always was — the journal's own message
	// names the offending event and the field. **If something ever does branch on the
	// difference**, bring the code back WITH the sentinels that make it true, not before.
)

// EnvControlJournal is the path to the append-only control journal this pod resolves
// browser/proxy SESSIONS against.
//
// 🔴 UNSET MEANS "NO JOURNAL AUTHORITY", WHICH IS EXACTLY TODAY'S BEHAVIOUR AND IS THE
// WHOLE COMPATIBILITY CLAIM. A deployment that does not set this wires
// `tokenfile.Source` and nothing else, and `identity.FromEnvironment` resolves its
// session backends against the same projection it always did. Nothing about the serving
// path changes; there is no new route, and the machine-token backend is untouched.
//
// 🔴 A VALUE THAT REDUCES TO NOTHING IS REFUSED RATHER THAN READ AS UNSET, AND THAT IS
// THE `refuseBlank` POLICY `internal/identity/config.go` DECLARES FOR THE SETTINGS WHOSE
// BLANK SILENTLY DISABLES WHAT THE OPERATOR WROTE DOWN. This is one of them: an operator
// who wrote the line meant the pod to read a journal, and a blank read as "not set"
// discards it. The blank test is `identity.ValueReducesToNothing`, the one predicate,
// rather than a second `strings.TrimSpace` here: 32 zero-width runes are not whitespace
// and it has already cost this repository a live bypass at a different setting.
//
// ⚠ IT IS NO LONGER THE ONLY THING STANDING BETWEEN A TYPO AND A SILENT POD, AND THIS
// COMMENT CLAIMED IT WAS. It said a blank "gives them a pod that starts, fetches its
// JWKS, passes its health check and refuses every sign-in" — true when it was written and
// false now: an armed session backend with no session authority is
// `identity.ErrSessionBackendWithoutAuthority` and the pod refuses to start either way.
// What the blank policy buys is the BETTER of the two messages — "your journal line
// reduces to nothing" rather than "set the variable you can see you already set" — and
// it is the only thing that refuses a blank on the `-create-user` path, which arms no
// backend at all.
//
// ⚠ IT IS NOT IN AN `internal/identity` LEDGER, AND THE REASON IS WHAT THAT MACHINERY
// ASKS. A ledger's `armed` flag answers "is this BACKEND half-configured, so refuse
// rather than come up silently off"; a journal path configures no backend — it supplies
// the authority the backends resolve against — and the gate over that machinery requires
// a ledger to carry at least two settings, so putting it there would mean inventing a
// second setting to satisfy a test. What the ledger would have bought instead is bought
// directly: the blank policy is declared above, the predicate is shared, and
// `TestABlankControlJournalIsRefusedRatherThanReadAsUnset` is the gate.
const EnvControlJournal = "CAIRN_CONTROL_JOURNAL"

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
	// 🔴 FIRST, BECAUSE A `flag.String` DEFAULT IS EVALUATED AT THE CALL AND THE NOTICE
	// HAS TO PRECEDE THE VALUE IT IS ABOUT. It goes to the same stream and through the same
	// sanitiser as every other startup line this pod emits — an operator greps
	// `subsystem-store-api:` and a deprecation notice that did not carry the prefix would
	// not be in what they read.
	envalias.WarnOnce(envalias.OSDeprecations(), func(line string) {
		fmt.Fprintln(os.Stderr, reloadSafe("subsystem-store-api: "+line))
	})

	store := flag.String("store", envOr("CAIRN_STORE_ROOT", defaultStore), "store root")
	host := flag.String("host", envOr("CAIRN_LISTEN_HOST", "0.0.0.0"), "listen address")
	port := flag.Int("port", envInt("CAIRN_PORT", defaultPort), "listen port")
	tokenFile := flag.String("token-file", envOr("CAIRN_TOKEN_FILE", defaultTokenFile),
		"file holding the bearer token SET, ONE ROW PER LINE, current first (mode 0600). "+
			"A row is `<token>` (legacy: unrestricted scope) or "+
			"`<token> <identity> <scope>,<scope>`. FILE FIRST: an agent exec sandbox "+
			"strips environment variables, so $"+authz.EnvToken+" is the fallback")
	routes := flag.Bool("routes", false,
		"print the declared `<METHOD> <head>` route ledger and exit. 🔴 THIS IS THE "+
			"LEDGER THE CONFORMANCE SUITE READS FOR A NON-PYTHON IMPLEMENTATION: the "+
			"suite discovers the oracle's routes from its source by AST and has no "+
			"equivalent here, so it reads this instead. It is an output of the DISPATCH "+
			"TABLES, never a restatement of them")
	create := registerCreateUserFlags()
	flag.Parse()

	// 🔴 TWO MODES AT ONCE IS A REFUSAL, NOT A PRECEDENCE. Checking `-routes` first and
	// returning would make `-routes -create-user` print the ledger and silently NOT create
	// the user — exit 0, plausible output, and an operator who believes a person now has
	// access. There is no reading of that command line worth guessing at.
	if *routes && *create.enabled {
		fmt.Fprintln(os.Stderr, reloadSafe(
			"subsystem-store-api: -routes and -create-user are both set. One prints a ledger and "+
				"exits, the other writes to the control journal and exits; running either silently "+
				"while ignoring the other is how an operator concludes a user was created"))
		os.Exit(exitConfig)
	}
	if *routes {
		for _, route := range api.DeclaredRoutes() {
			fmt.Println(route)
		}
		return
	}
	if *create.enabled {
		// `*store` is passed because this mode is the ONE place a scope display name is
		// chosen, and the store root is the only thing that can say whether that name
		// already reaches somebody else's directory. `internal/control` holds no path and
		// must not grow one; see `warnScopesThatAlreadyExistOnDisk`.
		os.Exit(runCreateUser(environ(), *store, create, os.Stdout, os.Stderr))
	}

	env := environ()
	resolvedTokenFile := *tokenFile
	// 🔴 `authz.IsTokenFile`, NOT A LOCAL COPY. This test and the loader's guard 2 decide
	// the SAME question, and the local `!IsDir()` version they used to share accepted a
	// character device — so `/dev/null` at the default path took this fallback away and
	// exited 78 where the oracle falls back and serves. See authz.IsTokenFile.
	if resolvedTokenFile != "" && !authz.IsTokenFile(resolvedTokenFile) && envalias.Value(env, authz.EnvToken) != "" {
		// The default path does not exist and an environment token does: use it, and SAY
		// SO. Falling back silently is how a deployment that lost its secret mount keeps
		// serving on a token nobody meant to be authoritative.
		fmt.Fprintln(os.Stderr, reloadSafe(fmt.Sprintf(
			"subsystem-store-api: token file %s absent; falling back to $%s",
			resolvedTokenFile, authz.EnvToken)))
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
	//
	// 🔴 AND THE SESSION BACKENDS RESOLVE AGAINST A DIFFERENT AUTHORITY WHEN ONE IS
	// CONFIGURED, WHICH IS WHAT MAKES THEM ABLE TO AUTHENTICATE ANYBODY AT ALL. Until
	// `CAIRN_CONTROL_JOURNAL` existed the only authority any binary wired was the
	// token-file projection, which holds one synthetic user at provider
	// `cairn-token-file` and grants only project subjects — so `SupabaseJWT` (default
	// provider `supabase`) could never match `UserByProviderSubject`, and a
	// `TrustedHeader` aimed at that one pair resolved to an EMPTY `Authorization`. A
	// deployment could follow `internal/identity/README.md` exactly, come up clean, pass
	// its health check and refuse every sign-in. `openSessionAuthority` returns a nil
	// `identity.ModelSource` when the setting is absent, which is byte-for-byte today's
	// wiring.
	sessions, err := openSessionAuthority(context.Background(), env, warn)
	if err != nil {
		fmt.Fprintln(os.Stderr, reloadSafe("subsystem-store-api: control journal: "+err.Error()))
		os.Exit(exitConfig)
	}
	identities, supabase, err := identity.FromEnvironment(env, srv.AuthorityView(), sessions)
	if err != nil {
		if errors.Is(err, identity.ErrSessionAuthorityUnread) {
			// The sentinel carries no variable name — it cannot, the package is handed a
			// `ModelSource` — so the line an operator has to edit is named here. Same
			// shape as the `tokenfile.ErrStoreRootUnreadable` arm above.
			fmt.Fprintln(os.Stderr, reloadSafe(fmt.Sprintf(
				"subsystem-store-api: identity: %s. Either configure a session backend (a "+
					"$CAIRN_SUPABASE_* or $CAIRN_TRUSTED_HEADER_* set) or unset $%s",
				err.Error(), EnvControlJournal)))
			os.Exit(exitConfig)
		}
		// 🔴 THE MIRROR SENTINEL GETS NO SECOND WORDING HERE, AND THAT IS THE DIFFERENCE
		// BETWEEN THE TWO ARMS RATHER THAN AN OMISSION. `ErrSessionAuthorityUnread` has two
		// remedies and `internal/identity` cannot name either, so this program supplies the
		// sentence. `ErrSessionBackendWithoutAuthority` has exactly one — set the journal —
		// and the sentinel already says so, including the variable's name; repeating it
		// here would be a second description of one rule, and the one that drifts is always
		// the copy. `TestTheSentinelNamesTheVariableThisProgramReads` is what keeps the
		// spelling in that sentinel and `EnvControlJournal` from coming apart.
		//
		// It falls through to the generic arm below, which prints the sentinel and exits 78.
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
//
// ⚠ IT IS THE ONLY SIGHUP CONSUMER IN THIS PROGRAM, AND THAT IS A RULE RATHER THAN A
// COINCIDENCE — see the authority timer above, which states it. A second
// `signal.Notify` channel was added here for the control-journal cache and then removed:
// it bought at most one refresh interval on a command a human runs by hand, and it put a
// second answer beside the one the timer already gives.
func installReload(srv *api.Server, tokenFile string, env map[string]string) {
	// BUFFERED, BECAUSE `signal.Notify` DROPS ON A FULL CHANNEL rather than blocking the
	// signal delivery. One slot is the right size for a reload: a second HUP arriving
	// while the first is being serviced asks for the same thing.
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

// envOr and envInt resolve through `internal/envalias`, so a name passed here is the
// CURRENT spelling and its deprecated alias is found for free. Neither spells an old name
// — one ledger, one place.
func envOr(name, fallback string) string {
	return envalias.OSValueOr(name, fallback)
}

func envInt(name string, fallback int) int {
	raw := envalias.OSValue(name)
	if raw == "" {
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
