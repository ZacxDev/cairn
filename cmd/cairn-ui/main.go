// Command cairn-ui is the browser surface: the SECOND binary the pod's control
// plane serves, and the one that renders HTML.
//
// 🔴 IT IS PHASE A AND IT IS DEPLOYED BY NOTHING. One page, one authentication
// chain, one rendering path — enough to prove the wiring and to stand the gate that
// replaces the guarantee the first third-party dependency in this repository
// removed. Cookie sessions, the sign-in flow and the screens are later phases with
// their own decisions; none of them are here.
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
	// ⚠ THERE IS NO `-routes` FLAG HERE, UNLIKE `cairn-server`, AND THE ASYMMETRY IS
	// DELIBERATE. The pod prints its ledger because a Python corpus owns its served
	// contract and cannot read a compiled binary — the printed table is the only way
	// a second instrument can see it. This surface has no corpus and no Python-side
	// gate: `ui.DeclaredRoutes()` derives from the map its dispatcher reads, and
	// `TestTheRouteLedgerMatchesTheDispatchTable` reads it in-process. A flag whose
	// only reader was a flake check printing the same list is a PATH entry with no
	// caller, which this repository refuses elsewhere. It returns with a corpus.
	flag.Parse()

	env := environ()
	tokens, err := authz.LoadTokens(*tokenFile, env, func(line string) {
		fmt.Fprintln(os.Stderr, "cairn-ui: "+line)
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "cairn-ui: "+err.Error())
		os.Exit(exitConfig)
	}
	if len(tokens) == 0 {
		fmt.Fprintln(os.Stderr, "cairn-ui: the token table is empty: the UI is not served without a credential")
		os.Exit(exitConfig)
	}

	// 🔴 THE AUTHORITY IS THE SAME PROJECTION THE POD USES, BUILT THE SAME WAY. A
	// second way to read the token file would be a second answer to "who may see
	// what", and the first thing two answers lose is agreement.
	//
	// ⚠ `Records` RETURNS A FIXED TABLE HERE, WHERE THE POD'S IS SWAPPABLE UNDER
	// SIGHUP. This binary has no reload path yet, so a revoked credential takes a
	// restart to stop working — stated because it is a real operational difference
	// from the pod and not a property anybody should assume from the shared type.
	authority := control.NewCache(tokenfile.Source{
		StoreRoot: *store,
		Records:   func() []authz.TokenRecord { return tokens },
	}, control.CacheOptions{MaxAge: authorityMaxAge})

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
		Sessions:    sessions,
		TTL:         *sessionTTL,
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

	fmt.Fprintf(os.Stderr, "cairn-ui: serving %d route(s) on %s, store %s\n",
		len(ui.DeclaredRoutes()), addr, *store)
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
