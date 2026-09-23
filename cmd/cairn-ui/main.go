// Command cairn-ui is the browser surface: the SECOND binary the pod's control
// plane serves, and the one that renders HTML.
//
// 🔴 IT IS DEPLOYED BY NOTHING, AND IT NOW CARRIES THREE PHASES. Seven routes over one
// authentication chain and one rendering path: the entries page, the sign-in pair with
// server-side revocable cookie sessions, and the share flow. `apps` has no entry for it,
// and no manifest in this repository deploys it. ⚠ This also said "No image wraps this
// binary"; `packages.ui-image` wraps it now and publishes it. Published is not deployed.
//
// ⚠ THIS COMMENT SAID "IT IS PHASE A … Cookie sessions, the sign-in flow and the screens
// are later phases; none of them are here" THROUGH THE TWO PHASES THAT ADDED THEM. It is
// the canonical site for a Go reader — `go doc ./cmd/cairn-ui`.
//
// ⚠ AND THIS COMMENT CLAIMED TO BE "the last copy of that claim standing", WHICH WAS
// ITSELF FALSE — a round auditing the sweep found `flake.nix`'s `meta.description` still
// shipping "phase A: one page", which is what `nix flake show` and `nix search` render and
// is more visible than any Go doc comment. Two sweeps in a row asserted completeness and
// missed a site; the lesson is to name where you LOOKED rather than to claim you finished.
//
// 🔴 AND IT IS A SEPARATE BINARY RATHER THAN ROUTES ON `cmd/cairn-server`, WHICH IS
// WHAT KEEPS THE POD'S SERVED CONTRACT AND ITS DEPENDENCY SET BOTH UNMOVED.
// `api.DeclaredRoutes()` and `tests/conformance/requests.json` are byte-unchanged by
// this commit, and `internal/depspolicy`'s import ban is what makes "the HTML
// library is not in the pod's binary" a measured fact rather than a hope.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"github.com/ZacxDev/cairn/internal/netid"
	"net"
	"net/http"
	"net/netip"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/ZacxDev/cairn/internal/authz"
	"github.com/ZacxDev/cairn/internal/control"
	"github.com/ZacxDev/cairn/internal/control/tokenfile"
	"github.com/ZacxDev/cairn/internal/envalias"
	"github.com/ZacxDev/cairn/internal/identity"
	"github.com/ZacxDev/cairn/internal/ui"
)

const (
	defaultStore     = "/data"
	defaultTokenFile = "/run/secrets/subsystem-store/token"
	defaultPort      = 8103

	// defaultSessionFile is where the browser session table lives.
	//
	// 🔴 IT IS NOT UNDER THE STORE ROOT, AND THAT IS DELIBERATE. `/data` is the notes
	// volume: `store.LoadStore` enumerates it, `server/seed.sh` replaces its contents
	// through `tar -xf -`, and `/snapshot` serves it. A session table under it would
	// be a set of live credentials inside a directory whose documented operations are
	// "enumerate" and "overwrite wholesale". A separate path means the deployment has
	// to mount a second writable volume — which `OpenFileSessionStore` refuses loudly
	// at startup if it did not, rather than discovering it at the first sign-in.
	defaultSessionFile = "/var/lib/cairn-ui/sessions"

	// EnvUIControlJournal is the `-control-journal` flag's environment spelling.
	//
	// 🔴 IT IS A CONSTANT AND ITS SIBLINGS ARE STRING LITERALS, BECAUSE THIS ONE IS READ
	// TWICE — once for the value and once by the blank policy that refuses a value which
	// reduces to nothing. Two spellings of a name a refusal quotes back at the operator is
	// how a refusal ends up naming a variable nobody set.
	EnvUIControlJournal = "CAIRN_UI_CONTROL_JOURNAL"

	// exitConfig is sysexits.h EX_CONFIG, the same code `cmd/cairn-server` uses and
	// for the same reason: a surface that came up misconfigured is worse than one
	// that did not come up, because it looks healthy.
	exitConfig = 78

	// authorityMaxAge is the staleness BOUND the cache declares. It bounds the
	// report, never the reads — see `control.CacheOptions.MaxAge`.
	authorityMaxAge = 5 * time.Minute
)

// refreshInterval is how often the authority is re-read, so a scope directory created out
// of band becomes visible — and a journal record dropped since the last read gets
// announced — without a restart.
//
// ⚠ A `var` RATHER THAN A `const`, AND THE ONLY ASSIGNMENT IS IN `main_test.go`. The same
// shape `cmd/cairn-server` uses and with the same discipline: the production value is 30 s,
// a test child shortens it through an environment variable read in `_test.go` and NOWHERE
// in this program, and it is deliberately not a flag — a knob whose only caller is a test
// is a configuration surface an operator would find and nobody supports. What it buys is
// that the refresh loop has a GATE at all: the loop is inside `main`, so a guard on it has
// to run the binary, and a binary that took 30 s per observation would not have one.
var refreshInterval = 30 * time.Second

func main() {
	// Same position and the same reason as `cmd/cairn-server`: a `flag.String` default is
	// evaluated at the call, so the notice has to precede the value it is about. This
	// surface has no reload stream and so no sanitiser to route it through — the line is
	// the ledger's own text and interpolates nothing caller-supplied.
	envalias.WarnOnce(envalias.OSDeprecations(), func(line string) {
		fmt.Fprintln(os.Stderr, "cairn-ui: "+line)
	})

	store := flag.String("store", envOr("CAIRN_STORE_ROOT", defaultStore), "store root")
	host := flag.String("host", envOr("CAIRN_UI_HOST", "0.0.0.0"), "listen address")
	port := flag.Int("port", envInt("CAIRN_UI_PORT", defaultPort), "listen port")
	tokenFile := flag.String("token-file", envOr("CAIRN_TOKEN_FILE", defaultTokenFile),
		"path to the token file this surface authenticates against")
	sessionFile := flag.String("session-file", envOr("CAIRN_UI_SESSION_FILE", defaultSessionFile),
		"path to the browser session table")
	sessionTTL := flag.Duration("session-ttl", envDuration("CAIRN_UI_SESSION_TTL", identity.DefaultSessionTTL),
		"absolute lifetime of a browser session")
	// 🔴 THE CONTROL JOURNAL IS WHAT LETS THE SHARE FLOW WRITE, AND IT REPLACES THE
	// TOKEN-FILE PROJECTION RATHER THAN SITTING BESIDE IT. Two authorities would be two
	// answers to "who may see what" — the thing `internal/control` exists to have
	// exactly one of — so this flag SWITCHES the authority instead of adding one. With
	// it unset the surface behaves as it did before the share flow, and the share pages
	// announce on every load that no share can be recorded here.
	//
	// 🔴 A SENTENCE STOOD HERE THAT WAS FALSE, AND IT IS RETRACTED RATHER THAN REPLACED.
	// It read: "the reads work, and a share attempt is refused with
	// `control.ErrAuthorityReadOnly`, which the page renders as a sentence naming the real
	// cause rather than as a permission problem the operator would go hunting for."
	// `internal/control/tokenfile` confers `admin` on NOBODY, so on a token-file
	// deployment no scope is administrable: the scope page answers 404, and `POST /share`
	// is refused **403** on authority before the sentinel is ever reached. The index read
	// does still work and does carry the banner — that half is real. ⚠ This is the THIRD
	// site of one retraction: the same claim was corrected in `README.md`, then in
	// `internal/ui/server.go`, and left standing here both times.
	//
	// 🔴 AND ITS DEFAULT IS NOT `envOr`, BECAUSE `envOr` CANNOT SEE THE VALUE THIS GUARD
	// IS ABOUT. `envalias.blank` is `TrimSpace(v) == ""`, so a whitespace-only
	// `CAIRN_UI_CONTROL_JOURNAL` resolves to `""` — "not set" — and this surface would
	// come up on the token-file projection, which confers `admin` on NOBODY. See
	// `controlJournalDefault`; the error is reported after `flag.Parse` so `-h` still works.
	journalDefault, journalErr := controlJournalDefault(os.Getenv)
	controlJournal := flag.String("control-journal", journalDefault,
		"path to the control journal; without one the authority is the token file and no share can be recorded")
	// ⚠ THERE IS NO `-routes` FLAG HERE, UNLIKE `cairn-server`, AND THE ASYMMETRY IS
	// DELIBERATE. The pod prints its ledger because a Python corpus owns its served
	// contract and cannot read a compiled binary — the printed table is the only way
	// a second instrument can see it. This surface has no corpus and no Python-side
	// gate: `ui.DeclaredRoutes()` derives from the map its dispatcher reads, and
	// `TestTheRouteLedgerMatchesTheDispatchTable` reads it in-process. A flag whose
	// only reader was a flake check printing the same list is a PATH entry with no
	// caller, which this repository refuses elsewhere. It returns with a corpus.
	flag.Parse()

	// 🔴 AFTER `flag.Parse` SO `-h` STILL PRINTS, AND NOT CONDITIONAL ON WHETHER THE FLAG
	// WAS ALSO GIVEN. The policy is about the LINE the operator wrote, not about which
	// value won: a manifest emitting a whitespace journal path is broken whether or not
	// something else supplies a good one, and `flag.Visit` machinery to let a flag rescue
	// it would be a second rule about the same variable.
	if journalErr != nil {
		fmt.Fprintln(os.Stderr, "cairn-ui: "+journalErr.Error())
		os.Exit(exitConfig)
	}

	authority, err := openAuthority(*controlJournal, *store, *tokenFile)
	if err != nil {
		fmt.Fprintln(os.Stderr, "cairn-ui: "+err.Error())
		os.Exit(exitConfig)
	}

	// Materializing is the caller's first `Refresh` — `NewCache` contacts nothing —
	// so a startup failure is this program's to shout about.
	if err := authority.Refresh(context.Background()); err != nil {
		if errors.Is(err, tokenfile.ErrStoreRootUnreadable) {
			fmt.Fprintf(os.Stderr,
				"cairn-ui: the store root %s cannot be enumerated (%s), so the authority cannot say which "+
					"scopes exist and every page would render empty. Refusing to start; mount the volume and restart\n",
				*store, err.Error())
			os.Exit(exitConfig)
		}
		fmt.Fprintln(os.Stderr, "cairn-ui: authority: "+err.Error())
		os.Exit(exitConfig)
	}

	// 🔴 A DROPPED RECORD IS SAID OUT LOUD OR IT IS A SILENT NARROWING, AND THIS SURFACE
	// FEELS IT FIRST. `control.Replay` skips a `credential-issued` record it cannot use — an
	// unusable digest, a digest another record already carries — and loads the rest of the
	// file rather than refusing the operator's whole control plane. A credential dropped
	// here is a person who cannot sign in, and the refusal below may even fire because the
	// only credential in the journal was the dropped one; without this line that reads as
	// an empty journal. `internal/control` holds no logger by design, so it carries the
	// drops as data and the programs that load a journal render them.
	//
	// 🔴 AT LOAD AND AT EVERY REFRESH, FOR THE REASON `cmd/cairn-server`'s OWN ANNOUNCER
	// STATES: the drop that matters most is the one that appears while the process is
	// already up, and a startup-only render is structurally unable to see it. The announcer
	// says each record ONCE — a standing drop repeated on every tick is noise an operator
	// learns to filter, which is the same outcome as saying nothing.
	announceNewDrops := newDropAnnouncer(*controlJournal, authority.Model,
		func(line string) { fmt.Fprintln(os.Stderr, line) })
	announceNewDrops()

	if err := refuseAnAuthorityNobodyCanSignInTo(authority, *controlJournal); err != nil {
		fmt.Fprintln(os.Stderr, "cairn-ui: "+err.Error())
		os.Exit(exitConfig)
	}

	machine, err := identity.NewMachineToken(authority)
	if err != nil {
		fmt.Fprintln(os.Stderr, "cairn-ui: identity: "+err.Error())
		os.Exit(exitConfig)
	}

	// 🔴 THE SESSION TABLE IS OPENED BEFORE THE LISTENER, SO A SURFACE THAT CANNOT
	// PERSIST A SESSION DOES NOT COME UP. Discovering it at the first sign-in would
	// mean a pod that passes every health check and refuses every login, which is the
	// shape every startup refusal in this repository exists against.
	sessions, err := identity.OpenFileSessionStore(*sessionFile)
	if err != nil {
		fmt.Fprintf(os.Stderr,
			"cairn-ui: the session table %s cannot be opened (%s), so no browser could sign in. "+
				"Refusing to start; mount a writable volume for it and restart\n", *sessionFile, err.Error())
		os.Exit(exitConfig)
	}

	cookie, err := identity.NewCookieSession(sessions, authority)
	if err != nil {
		fmt.Fprintln(os.Stderr, "cairn-ui: identity: "+err.Error())
		os.Exit(exitConfig)
	}

	// 🔴 `ui.AuthBackends`, NOT `identity.FromEnvironment`. The environment builder
	// arms the trusted-header backend when an operator declares the deployment
	// proxy-fronted, and this surface must not have that backend at any setting —
	// see `internal/ui/auth.go` for why a browser endpoint cannot carry that trade.
	// The chain's membership is pinned by `TestTheUIChainHasNoTrustedHeaderMember`
	// rather than by this call site. It takes the machine token and the cookie
	// backend: the Supabase backend is still not wired, and a parameter every caller
	// passed `nil` to was removed rather than kept as a promise.
	chain, err := ui.AuthBackends(machine, cookie)
	if err != nil {
		fmt.Fprintln(os.Stderr, "cairn-ui: identity: "+err.Error())
		os.Exit(exitConfig)
	}

	// 🔴 THE CLIENT-IDENTITY PAIR, AND THE REFUSAL IS DERIVED FROM THE BIND ADDRESS RATHER
	// THAN FROM A FLAG. `POST /sign-in` had no throttle and no attribution; this wires the
	// same `internal/netid` the pod uses. The allowlist is REQUIRED whenever this surface
	// is reachable by anybody but the local host, because `netid.ClientIPHeader` is
	// forgeable by every peer outside it — so a lockout without an allowlist behind a proxy
	// buckets the whole internet under the proxy's own address, and one abuser locks out
	// everybody.
	//
	// 🔴 WHY THE BIND ADDRESS AND NOT A FLAG: an operator cannot forget to set it, and it
	// cannot disagree with reality. A loopback listener has no peers but this machine, and a
	// local process that could forge the header already has the token file. Anything else —
	// including the `0.0.0.0` DEFAULT — is reachable, so it refuses. An `--insecure` style
	// flag was not added: `claude/RULES.md`'s objection to an unclearable gate does not
	// apply, because setting the variable IS the clearing and it is one line of a manifest.
	trustedProxies, proxyErr := netid.LoadTrustedProxies(envalias.Environ())
	if bindIsReachable(*host) {
		if proxyErr != nil {
			fmt.Fprintf(os.Stderr,
				"cairn-ui: refusing to serve on %s: %v\n", *host, proxyErr)
			fmt.Fprintf(os.Stderr,
				"cairn-ui: this surface is reachable from outside this machine, so the %s "+
					"header cannot be trusted from an unlisted peer and a sign-in lockout "+
					"would have exactly one bucket for every caller behind your proxy. Set "+
					"$%s, or bind to a loopback address for a local run.\n",
				netid.ClientIPHeader, netid.EnvTrustedProxies)
			os.Exit(exitConfig)
		}
	} else if proxyErr != nil {
		// 🔴 A PASS BY ABSENCE, AND IT SAYS SO — the same reasoning the leak gate's
		// `PASS BY ABSENCE` note is built on. Silence here would be indistinguishable from
		// a configured allowlist.
		fmt.Fprintf(os.Stderr,
			"cairn-ui: no $%s, and this is a loopback bind (%s) — the sign-in limiter will "+
				"key on the local peer, which is ONE bucket. That is fine for a hand-run "+
				"bring-up and wrong anywhere else; it is not a configured allowlist.\n",
			netid.EnvTrustedProxies, *host)
	}

	maxFailures, window, lockout, limErr := netid.LimiterSettings(envalias.Environ())
	if limErr != nil {
		fmt.Fprintln(os.Stderr, "cairn-ui: "+limErr.Error())
		os.Exit(exitConfig)
	}
	limiter := netid.NewRateLimiter(maxFailures, window, lockout)

	srv, err := ui.New(ui.Config{
		Auth: chain,
		// ⚠ THE SAME `authority` THE MACHINE-TOKEN BACKEND HOLDS, WHICH IS THE POINT.
		// The sign-in form resolves its credential through the same projection every
		// request is authenticated against; a second authority here would be a second
		// answer to "is this token real", and the first thing two answers lose is
		// agreement about a revocation.
		Credentials: authority,
		Source:      ui.StoreSource{Root: *store},
		// 🔴 THE SAME `authority` AGAIN, FOR THE SAME REASON THE LINE ABOVE GIVES. The
		// share flow renders "who has access to this" and the chain decides "may this caller
		// see it"; two caches would let the page make a claim about a world the
		// request was never authorised against.
		Sharing:  ui.ControlSharing{Authority: authority},
		Sessions: sessions,
		TTL:      *sessionTTL,
		// Both from the block above. A nil limiter would be an unbounded sign-in, which is
		// the defect; there is no path here that produces one.
		TrustedProxies: trustedProxies,
		Limiter:        limiter,
		// One clock for the server and the store. `FileSessionStore.Now` is left nil,
		// which means `time.Now().UTC()`, and `ui.Config.Now` defaults to the same
		// thing — so they agree by both taking the default rather than by one being
		// handed the other's.
		Log: os.Stderr,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "cairn-ui: "+err.Error())
		os.Exit(exitConfig)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		ticker := time.NewTicker(refreshInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				// A refresh failure keeps the last-known-good model — that is
				// `control.Cache`'s documented behaviour and the reason an authority
				// outage is not a total read outage. It is logged so the operator has
				// a signal other than silence.
				if err := authority.Refresh(ctx); err != nil {
					fmt.Fprintln(os.Stderr, "cairn-ui: authority refresh failed, serving last-known-good: "+err.Error())
					continue
				}
				// A refresh that SUCCEEDED can still have dropped a record — a
				// `credential-issued` line somebody appended by hand since the last
				// read — and that is the case the startup render above cannot see.
				announceNewDrops()
			}
		}
	}()

	addr := net.JoinHostPort(*host, strconv.Itoa(*port))
	listener := &http.Server{
		Addr:    addr,
		Handler: srv,
		// A browser client that opens a connection and sends nothing must not hold a
		// slot open. The pod's own listener takes the same position.
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = listener.Shutdown(shutdown)
	}()

	// 🔴 THE LINE SAYS WHETHER A SHARE CAN BE RECORDED, BECAUSE THE ANSWER IS DECIDED
	// AT STARTUP AND DISCOVERED AT THE FIRST CLICK OTHERWISE. A surface that serves
	// every page and refuses every write is exactly the shape every other startup
	// refusal in this program exists against; this one is a legitimate configuration
	// rather than an error, so it is ANNOUNCED instead of refused.
	sharingMode := "read-only (no -control-journal: no share can be recorded)"
	if *controlJournal != "" {
		sharingMode = "writable (control journal " + *controlJournal + ")"
	}
	fmt.Fprintf(os.Stderr, "cairn-ui: serving %d route(s) on %s, store %s, sharing %s\n",
		len(ui.DeclaredRoutes()), addr, *store, sharingMode)
	if err := listener.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		fmt.Fprintln(os.Stderr, "cairn-ui: "+err.Error())
		os.Exit(1)
	}
}

// `envOr`, `envDuration` and `envInt` resolve through `internal/envalias`, so a name
// passed to one is the CURRENT spelling and its deprecated alias is found for free.
// `CAIRN_UI_*` has no alias and resolves as itself — `envalias.Value` gives plain
// single-name behaviour for a name that was never renamed, which is why no call site has
// to know which kind it holds.
//
// 🔴 `controlJournalDefault`, BELOW, IS THE ONE READER THAT DOES NOT, AND THE EXCEPTION IS
// NAMED HERE RATHER THAN LEFT TO BE FOUND AT ITS DEFINITION. All three readers above
// inherit `envalias.blank` — `TrimSpace(v) == ""` — so for every name they read, a
// whitespace-only value is indistinguishable from an unset one and silently takes the code
// default. For a listen address or a TTL that is the existing, accepted ruling. For the
// control journal it is the difference between a surface that refuses to start and one
// that starts on an authority conferring `admin` on nobody, so that name is read raw.
func envOr(name, fallback string) string {
	return envalias.OSValueOr(name, fallback)
}

// controlJournalDefault is the `-control-journal` flag's default, and the ONE place this
// surface's journal line meets the blank policy.
//
// 🔴 IT IS THE SAME RULING `cmd/cairn-server`'s `controlJournalPath` MAKES FOR THE POD'S
// OWN `CAIRN_CONTROL_JOURNAL`, AND IT IS THE SAME PREDICATE RATHER THAN A SECOND
// SPELLING. Absent, and present-with-the-EMPTY-string, are both "not set" — a manifest
// that emits every variable with an empty default is a common shape. A value that
// REDUCES TO NOTHING is a line the operator wrote and this program would discard, so it
// is refused. `identity.ValueReducesToNothing` is the test rather than a fresh
// `strings.TrimSpace`: 32 zero-width runes are not whitespace and `TrimSpace` calls them
// content, which has already cost this repository a live bypass at a different setting.
//
// 🔴 WHAT A WHITESPACE LINE BOUGHT BEFORE THIS FUNCTION EXISTED, MEASURED ON TWO BUILT
// BINARIES OVER ONE WORLD: `CAIRN_UI_CONTROL_JOURNAL='   '` with no flag exited **78**
// naming the journal at `1659663` and **served** at `68cf955`, announcing
// `sharing read-only (no -control-journal: no share can be recorded)`. That is the
// token-file projection, which grants `admin` to NOBODY — so every scope page answers
// 404, no share can be recorded, and `/healthz` stays 200. The regression arrived in a
// MERGE: the branch that converted `envOr` to `envalias.OSValueOr` had no
// `-control-journal`, and the branch that added `-control-journal` had no `envalias`, so
// neither side's tests could see it.
//
// 🔴 IT READS `os.Getenv` RATHER THAN `envalias`, AND THAT IS THE POINT RATHER THAN AN
// OVERSIGHT. `envalias.ValueFrom` treats a blank value as absent — that IS the defect
// here — so resolving through it would hand this function a `""` it cannot distinguish
// from an unset variable. `TestTheControlJournalVariableIsNotInTheAliasLedger` is what
// keeps the raw read from silently losing a deprecated spelling: this name has none, and
// the day it gains one that test goes red rather than this function going quiet.
//
// ⚠ A NON-BLANK VALUE IS RETURNED RAW, NOT TRIMMED, WHICH IS NARROWER THAN THE POD'S
// READER AND DELIBERATELY SO. `controlJournalPath` returns `TrimSpace(raw)`; trimming
// here would make `CAIRN_UI_CONTROL_JOURNAL="  /data/j  "` open a journal where
// `-control-journal "  /data/j  "` is refused by `openAuthority`'s `Stat`. The whole
// finding this function closes is a value that behaves differently by ARRIVAL PATH, so
// the fix does not open a second one. Measured at `68cf955`: both spellings of that value
// exit 78, and they still do.
func controlJournalDefault(get func(string) string) (string, error) {
	raw := get(EnvUIControlJournal)
	if raw == "" {
		return "", nil
	}
	if identity.ValueReducesToNothing(raw) {
		return "", fmt.Errorf(
			"%s=%q reduces to nothing, so this surface would read it as UNSET and fall back to the "+
				"token-file projection, which confers `admin` on nobody: every scope page would "+
				"answer 404 and no share could be recorded, while /healthz answered 200 and the "+
				"index still rendered. That is a surface that looks healthy and serves nobody. "+
				"Refusing to start; give it a path or delete the line",
			EnvUIControlJournal, raw)
	}
	return raw, nil
}

// envDuration falls back on an unparseable value rather than refusing, which matches
// `envInt` beside it.
//
// ⚠ THAT IS THE WEAKER OF THE TWO AVAILABLE RULINGS AND IT IS THE EXISTING ONE. A
// mistyped `CAIRN_UI_SESSION_TTL` silently gets the default rather than refusing to
// start — the opposite of how `internal/identity`'s ledger treats a mistyped duration.
// It is left consistent with the neighbouring readers rather than made a one-off, and
// the divergence between this program's ad-hoc environment reads and that package's
// ledger is the thing to close, in one change, rather than here.
func envDuration(name string, fallback time.Duration) time.Duration {
	v := envalias.OSValue(name)
	if v == "" {
		return fallback
	}
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		return fallback
	}
	return d
}

func envInt(name string, fallback int) int {
	v := envalias.OSValue(name)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}

// newDropAnnouncer renders the records `control.Replay` skipped that it has NOT already
// rendered — once at load, then after every successful refresh.
//
// 🔴 IT IS A SECOND COPY OF `cmd/cairn-server`'s FUNCTION OF THE SAME NAME, AND THAT IS A
// COST PAID DELIBERATELY. The shared helper would have to live in a package both programs
// import, and the only one is `internal/control`, which holds no logger by design — the
// same boundary that makes the drops DATA rather than a log line in the first place. The
// two differ in their prefix and their wording, so neither is a wrapper of the other.
//
// 🔴 ONLY WHAT IS NEW, BECAUSE A STANDING DROP RE-ANNOUNCED ON EVERY TICK IS NOISE. This
// surface refreshes on the same timer the pod does; a line per refresh for one bad journal
// line is thousands a day, and an operator who filters that stream has lost the warning
// that matters. The identity is the whole rendered claim — position, kind, credential and
// reason — so a record whose position MOVES is said again: a repeat, never a silence.
//
// ⚠ IN TOKEN-FILE MODE THIS NEVER SAYS ANYTHING, BECAUSE `tokenfile.Source` REPLAYS NO
// JOURNAL AND CARRIES NO DROPS. It is called unconditionally anyway rather than guarded on
// `journal != ""`, so there is one code path and no second place to get the condition
// wrong.
func newDropAnnouncer(journal string, model func() control.Model, warn func(string)) func() {
	announced := map[string]struct{}{}
	return func() {
		for _, d := range model().Dropped {
			line := fmt.Sprintf(
				"cairn-ui: WARNING the control journal %s: %s. The rest of the file loaded and this "+
					"surface is serving an authority WITHOUT that credential. Delete or correct that "+
					"line; nothing here rewrites an append-only journal",
				journal, d)
			if _, said := announced[line]; said {
				continue
			}
			announced[line] = struct{}{}
			warn(line)
		}
	}
}

// openAuthority builds the ONE control-plane authority this surface reads.
//
// 🔴 IT RETURNS EXACTLY ONE CACHE AND THE TWO BRANCHES ARE EXCLUSIVE. A journal makes
// the authority WRITABLE — `control.FileStore` implements `control.Writer`, so
// `Cache.ApplyNow` records a grant; the token file does not, so the same call refuses
// with `control.ErrAuthorityReadOnly`. Everything downstream is identical either way,
// which is what keeps the share flow one code path rather than two.
//
// ⚠ THE TOKEN FILE IS STILL LOADED AND STILL REQUIRED TO BE NON-EMPTY IN THE
// PROJECTION BRANCH, AND NOT LOADED AT ALL IN THE JOURNAL BRANCH. A journal carries its
// own credentials (`EventCredentialIssued`), so reading the token file there would be
// the second authority this function exists to avoid.
func openAuthority(journal, storeRoot, tokenFile string) (*control.Cache, error) {
	if journal != "" {
		// 🔴 THE PATH IS REQUIRED TO EXIST AND TO BE A FILE, BECAUSE `OpenFileStore`
		// CREATES IT AND AN EMPTY JOURNAL REPLAYS CLEAN. `control.OpenFileStore` does
		// `MkdirAll` then `O_CREATE`, so a TYPO or an unmounted volume is not an error to
		// it: the file is created, `Refresh` replays zero events successfully, and this
		// program starts — announcing `sharing writable` — with an authority holding no
		// users, no scopes and no credentials. Every sign-in then answers 401 and every
		// page is empty. That is the shape every startup refusal in this file exists
		// against: a surface that passes its health check and can serve nobody. Measured
		// on the built binary with a deliberately misspelled path — `/healthz` 200,
		// `GET /sign-in` 200, `POST /sign-in` 401 for any credential, and the misspelled
		// file created at 0 bytes.
		//
		// ⚠ THE CHECK IS `Stat` BEFORE THE OPEN, so this program never creates the file it
		// is complaining about. An operator bootstrapping a genuinely new deployment seeds
		// the journal with `cairn-server -create-user`; this binary is a READER of the
		// control plane and has no business minting one.
		//
		// 🔴 AND IT IS ONLY HALF THE GUARD — THE OTHER HALF IS
		// `refuseAnAuthorityNobodyCanSignInTo`, AFTER THE REFRESH, BECAUSE THE HAZARD IS A
		// STATE AND NOT A FILE SIZE. A first draft refused `info.Size() == 0` and called it
		// done. Measured: a journal holding a single NEWLINE replays clean, and the surface
		// came up announcing `sharing writable`, answered `/healthz` 200 and could
		// authenticate nobody — the exact shape the refusal names, one byte outside its
		// reach. That is this repository's "a guard can be SPELLED rather than STRUCTURAL"
		// rule, and the spelling here was a byte count. See that function for the SECOND
		// spelling that was also walked around, and for what the guard asks now.
		if info, statErr := os.Stat(journal); statErr != nil {
			return nil, fmt.Errorf("the control journal %s cannot be read (%w), so the authority would hold "+
				"no users, no scopes and no credentials — this program would start, answer its health "+
				"check and refuse every sign-in. Refusing to start; check the path and the mount, and "+
				"seed a new control plane with `cairn-server -create-user` rather than here",
				journal, statErr)
		} else if info.IsDir() {
			return nil, fmt.Errorf("the control journal %s is a DIRECTORY, not a journal file", journal)
		}
		src, err := control.OpenFileStore(journal)
		if err != nil {
			return nil, fmt.Errorf("the control journal %s cannot be opened (%w), so the authority has "+
				"nothing to answer from. Refusing to start; mount a writable volume for it and restart",
				journal, err)
		}
		return control.NewCache(src, control.CacheOptions{MaxAge: authorityMaxAge}), nil
	}

	tokens, err := authz.LoadTokens(tokenFile, envalias.Environ(), func(line string) {
		fmt.Fprintln(os.Stderr, "cairn-ui: "+line)
	})
	if err != nil {
		return nil, err
	}
	if len(tokens) == 0 {
		return nil, errors.New("the token table is empty: the UI is not served without a credential")
	}

	// 🔴 THE AUTHORITY IS THE SAME PROJECTION THE POD USES, BUILT THE SAME WAY. A
	// second way to read the token file would be a second answer to "who may see
	// what", and the first thing two answers lose is agreement.
	//
	// ⚠ `Records` RETURNS A FIXED TABLE HERE, WHERE THE POD'S IS SWAPPABLE UNDER
	// SIGHUP. This binary has no reload path yet, so a revoked credential takes a
	// restart to stop working — stated because it is a real operational difference
	// from the pod and not a property anybody should assume from the shared type.
	return control.NewCache(tokenfile.Source{
		StoreRoot: storeRoot,
		Records:   func() []authz.TokenRecord { return tokens },
	}, control.CacheOptions{MaxAge: authorityMaxAge}), nil
}

// refuseAnAuthorityNobodyCanSignInTo is the STATE half of the control-journal guard.
//
// 🔴 IT TESTS THE SIGN-IN PRECONDITION ITSELF — A LIVE CREDENTIAL WHOSE PRINCIPAL THE
// MODEL HOLDS — AND THAT IS THE THIRD SPELLING OF THIS GUARD, THE FIRST TWO HAVING BEEN
// PROXIES THAT THE HAZARD WALKED AROUND. Draft 1 refused `info.Size() == 0`; a journal
// holding a single NEWLINE replays clean, so the surface came up announcing
// `sharing writable` and served nobody, one byte outside the check. Draft 2 refused
// `len(Model().Users) == 0` — and `cairn-server -create-user`, which this guard's own
// error text prescribes as the remedy, writes a user and **no credential**
// (`createuser.go`: "Never a token: this path mints no credential at all"). So following
// the remedy produced exactly the state the refusal promises to prevent, and the guard
// passed it. Measured end to end on both built binaries.
//
// `control.Authenticate` matches a presented token against LIVE credentials and then
// requires `PrincipalFor` to know the principal, so "can anybody sign in at all" is
// precisely the count below. A proxy for it can always be walked around; this is the
// question itself.
//
// ✅ AND THE CONSEQUENCE THAT USED TO BE A GAP IS CLOSED — THE PARAGRAPH IS KEPT RATHER
// THAN DELETED BECAUSE A COMMENT IS A CLAIM TOO, AND THIS ONE WAS THE REASON THE REFUSAL'S
// TEXT SAID WHAT IT SAID. It read: "**no tool in this repository writes
// `EventCredentialIssued` into a journal.** `-create-user` does not, and there is no
// `-issue-credential`. So a journal-backed `cairn-ui` cannot be brought up sign-in-capable
// BY ANY TOOL HERE." All three sentences are now false. `cairn-server -issue-credential`
// mints a token, writes only its digest, and emits the token once; a journal it has been
// run against passes this guard, which is pinned by
// `TestAJournalAnIssuedCredentialMakesSignInCapableIsAdmitted` below.
//
// ⚠ WHAT DOES NOT CHANGE IS THE GUARD, AND THAT IS THE POINT RATHER THAN AN OVERSIGHT.
// `-create-user` still mints a user and NO credential, so the state this refuses is still
// reachable by following the obvious first command — the remedy moved, the hazard did not.
//
// ⚠ "NO TOOL HERE CAN" WAS NEVER "IT CANNOT", AND AN EARLIER DRAFT SAID THE WIDER THING —
// that the mode was "not a sign-in-capable deployment today". MEASURED FALSE even then:
// appending one `{"kind":"credential-issued",…}` line to a `-create-user` journal made this
// binary start and a real browser sign-in succeed (303, then 200 on `/`). The gap was
// TOOLING and it is the tooling that landed; the record is kept because an operator who
// believed the wider sentence would have abandoned a mode that worked.
//
// ⚠ IT IS SCOPED TO THE JOURNAL BRANCH, DELIBERATELY. The token-file projection
// synthesizes a credential per row, so this could never fire there — and `openAuthority`
// already refuses an empty token table, which is that branch's equivalent.
//
// ⚠ AND IT EQUATES "NOBODY CAN SIGN IN" WITH "SERVES NOBODY", WHICH IS ONE PATH SHORT.
// `identity.CookieSession` resolves a live browser session from the session table and
// `PrincipalFor`, consulting NO credential — so a deployment mid-rotation (every credential
// revoked, the replacement not yet issued) is still serving every signed-in browser, and
// this guard turns the next restart into a refusal that ends those sessions. That is the
// right trade for a surface nothing deploys — coming up unable to authenticate anybody is
// the louder failure — but it is a state the refusal's wording does not weigh, and it is
// named here rather than discovered during a rotation.
func refuseAnAuthorityNobodyCanSignInTo(authority *control.Cache, journal string) error {
	if journal == "" {
		return nil
	}
	m := authority.Model()
	usable := 0
	for _, c := range m.Credentials {
		if !c.Live() {
			continue
		}
		if _, known := m.PrincipalFor(c.PrincipalKind, c.PrincipalID); known {
			usable++
		}
	}
	if usable > 0 {
		return nil
	}
	// 🔴 THE REMEDY IS NAMED AS A COMMAND, BECAUSE THIS TEXT IS THE WHOLE OF WHAT AN
	// OPERATOR GETS. The previous version said no tool here could fix it and offered a
	// hand-appended journal line; that was true when written and is false now, and a
	// refusal that sends somebody to hand-edit an append-only authority — with a live
	// secret in their clipboard — when a command exists is the worst of the two.
	// 🔴 THE COUNT SAYS WHAT IT COUNTS, BECAUSE THE BARE ONE UNDERCOUNTED IN EXACTLY THE
	// CASE THIS REFUSAL FIRES FOR. `len(m.Credentials)` is records LOADED; a journal whose
	// three credential lines were all dropped at replay reported "0 credential record(s)",
	// which reads as a journal nobody ever issued into and sends the operator to mint
	// another rather than to the line above naming the three that were skipped.
	return fmt.Errorf("the control journal %s materialized an authority with NO USABLE CREDENTIAL "+
		"(%d user(s), %d credential record(s) LOADED, 0 of them live and attributable, and %d "+
		"record(s) DROPPED at replay and named on the WARNING line(s) above), so "+
		"`control.Authenticate` can match nothing and every sign-in would answer 401 — this program "+
		"would come up, announce itself writable and serve nobody. Refusing to start. ⚠ NOTE THAT "+
		"`cairn-server -create-user` DOES NOT FIX THIS: it mints a user and no credential. What "+
		"does fix it is `cairn-server -issue-credential -principal <usr_… or prj_…>` against THIS "+
		"journal, which mints a token, writes only its SHA-256 digest, and emits the token once — "+
		"to stdout, or with -token-out <path> to a file it creates at mode 0600. ⚠ If you instead "+
		"hand-append a `credential-issued` record, note that `token_hash` is the SHA-256 HEX DIGEST "+
		"of the token and NEVER the token — a raw secret pasted there is written to the file like "+
		"any other line (nothing validates a journal you edit by hand) and then DROPPED at every "+
		"replay as 'not a 64-character hex digest', so the credential simply does not exist while "+
		"the secret sits in an append-only file that cannot be rewritten. Either case of hex is "+
		"accepted and the model lowercases it, so a digest as `Get-FileHash` or `certutil "+
		"-hashfile` spells it works as written",
		journal, len(m.Users), len(m.Credentials), len(m.Dropped))
}

// bindIsReachable answers whether this listen address admits anybody but the local host.
//
// 🔴 IT FAILS TOWARDS REACHABLE, WHICH IS THE SAFE DIRECTION. An address this cannot parse
// — a hostname, an empty string, something malformed — is treated as reachable, so the
// trusted-proxy refusal applies. The alternative fails towards "loopback", which would let
// a typo'd bind silently serve the internet with a one-bucket limiter.
//
// ⚠ `0.0.0.0` AND `::` ARE REACHABLE, and they are the DEFAULT for `-host`. That is
// deliberate: an unconfigured run of this binary is a run that refuses until somebody has
// thought about the allowlist, which is the same posture `cmd/cairn-server` takes on its
// token file.
func bindIsReachable(host string) bool {
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return true
	}
	return !addr.IsLoopback()
}
