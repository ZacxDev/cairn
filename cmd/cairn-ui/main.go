// Command cairn-ui is the browser surface: the SECOND binary the pod's control
// plane serves, and the one that renders HTML.
//
// 🔴 IT IS DEPLOYED BY NOTHING, AND IT NOW CARRIES THREE PHASES. Seven routes over one
// authentication chain and one rendering path: the entries page, the sign-in pair with
// server-side revocable cookie sessions, and the share flow. No image wraps this binary,
// `apps` has no entry for it, and no manifest in this repository deploys it.
//
// ⚠ THIS COMMENT SAID "IT IS PHASE A … Cookie sessions, the sign-in flow and the screens
// are later phases; none of them are here" THROUGH THE TWO PHASES THAT ADDED THEM. It is
// the canonical site — `go doc ./cmd/cairn-ui` — and it was the last copy of that claim
// standing after `README.md` was corrected.
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
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/ZacxDev/cairn/internal/authz"
	"github.com/ZacxDev/cairn/internal/control"
	"github.com/ZacxDev/cairn/internal/control/tokenfile"
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

	// exitConfig is sysexits.h EX_CONFIG, the same code `cmd/cairn-server` uses and
	// for the same reason: a surface that came up misconfigured is worse than one
	// that did not come up, because it looks healthy.
	exitConfig = 78

	// authorityMaxAge is the staleness BOUND the cache declares. It bounds the
	// report, never the reads — see `control.CacheOptions.MaxAge`.
	authorityMaxAge = 5 * time.Minute

	// refreshInterval is how often the token-file projection is re-read, so a scope
	// directory created out of band becomes visible without a restart.
	refreshInterval = 30 * time.Second
)

func main() {
	store := flag.String("store", envOr("SUBSYSTEM_STORE_ROOT", defaultStore), "store root")
	host := flag.String("host", envOr("CAIRN_UI_HOST", "0.0.0.0"), "listen address")
	port := flag.Int("port", envInt("CAIRN_UI_PORT", defaultPort), "listen port")
	tokenFile := flag.String("token-file", envOr("SUBSYSTEM_STORE_TOKEN_FILE", defaultTokenFile),
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
	controlJournal := flag.String("control-journal", envOr("CAIRN_UI_CONTROL_JOURNAL", ""),
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

	if err := refuseAnEmptyAuthority(authority, *controlJournal); err != nil {
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
				}
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

func environ() map[string]string {
	out := make(map[string]string)
	for _, kv := range os.Environ() {
		for i := 0; i < len(kv); i++ {
			if kv[i] == '=' {
				out[kv[:i]] = kv[i+1:]
				break
			}
		}
	}
	return out
}

func envOr(name, fallback string) string {
	if v, set := os.LookupEnv(name); set && v != "" {
		return v
	}
	return fallback
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
	v, set := os.LookupEnv(name)
	if !set || v == "" {
		return fallback
	}
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		return fallback
	}
	return d
}

func envInt(name string, fallback int) int {
	v, set := os.LookupEnv(name)
	if !set || v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
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
		// 🔴 THE PATH IS REQUIRED TO EXIST AND TO BE NON-EMPTY, BECAUSE `OpenFileStore`
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
		// 🔴 AND IT IS ONLY HALF THE GUARD — THE OTHER HALF IS `refuseAnEmptyAuthority`,
		// AFTER THE REFRESH, BECAUSE THE HAZARD IS A STATE AND NOT A FILE SIZE. A first
		// draft refused `info.Size() == 0` and called it done. Measured: a journal holding
		// a single NEWLINE replays clean, and the surface came up announcing
		// `sharing writable`, answered `/healthz` 200 and held ZERO users — the exact shape
		// the refusal names, one byte outside its reach. That is this repository's
		// "a guard can be SPELLED rather than STRUCTURAL" rule, and the spelling here was
		// a byte count standing in for "this authority knows nobody".
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

	tokens, err := authz.LoadTokens(tokenFile, environ(), func(line string) {
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

// refuseAnEmptyAuthority is the STATE half of the control-journal guard: a materialized
// authority that knows nobody.
//
// 🔴 IT ASKS THE MODEL, NOT THE FILE, AND THAT IS THE WHOLE POINT. The hazard is "this
// surface can serve nobody", and a file size is a proxy for it that a single newline
// defeats — measured, with the process coming up and answering its health check while
// holding zero users. `len(Users) == 0` is the hazard itself, available for free once
// `Refresh` has run.
//
// ⚠ IT IS SCOPED TO THE JOURNAL BRANCH, DELIBERATELY. The token-file projection always
// synthesizes one operator user, so this could never fire there — and a guard that cannot
// fire on a path is a guard that path does not have. `openAuthority` refuses an empty
// token table on its own, which is that branch's equivalent.
func refuseAnEmptyAuthority(authority *control.Cache, journal string) error {
	if journal == "" {
		return nil
	}
	if len(authority.Model().Users) == 0 {
		return fmt.Errorf("the control journal %s materialized an authority with NO USERS, so no "+
			"credential could ever be resolved and every sign-in would answer 401 — this program "+
			"would come up, announce itself writable and serve nobody. Refusing to start; seed it "+
			"with `cairn-server -create-user`", journal)
	}
	return nil
}
