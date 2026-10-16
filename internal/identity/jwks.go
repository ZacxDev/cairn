package identity

import (
	"context"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// KeySet is a locally cached JWKS: the identity provider's public keys, fetched from a URL
// (or read from a local file — `JWKSOptions.File`) on a schedule, and read with no network
// call at all.
//
// 🔴 THE HOT PATH CONTACTS NOTHING, AND THAT IS THE PLAN'S §E PROMISE RATHER THAN AN
// OPTIMISATION: *"an IdP outage must not stop reads for already-issued sessions."*
// cairn's whole claim is that an offline "orient me" still answers, so an identity
// provider that is down, slow or rate-limiting must not be able to turn a read into a
// 401. `key` reads a map under an RLock; `Refresh` is the only thing that fetches, and
// it is called from a timer or by an explicit caller.
//
// 🔴 THE SHAPE IS `control.Cache`'s ON PURPOSE, AND IT INHERITS THAT TYPE'S ONE HONEST
// COST: A ROTATED-OUT KEY KEEPS VERIFYING UNTIL THE NEXT REFRESH. No arrangement makes
// that false — a cache that asked the provider whether it was stale would be making
// exactly the call the outage is supposed to survive. So the lag is BOUNDED by the
// refresh schedule and REPORTABLE by `Status`.
//
// ⚠ AND "REPORTABLE" IS THE WORD IT EARNS TODAY, EXACTLY AS `control.Cache.Staleness`
// DOES. `Status()` is a value with a `String()`; no deployed program prints it, because
// the one place to print it is `cmd/cairn-server`'s startup banner, which
// `tests/dualrun/harness.py` compares between the two servers — so a Go-only field
// there would move a gate. **CLOSING CONDITION:** the same one `internal/control`'s
// README states for `Staleness` — a render (a `doctor` section, a status route, or a
// banner field declared in `wire.NORMALIZATIONS`) that a `tests/dualrun/` run exits 0
// with.
//
// ⚠ WHAT A ROTATION COSTS IN THE OTHER DIRECTION IS SMALLER AND IS NOT THE SAME
// PROBLEM. A key ADDED at the provider is unknown here until the next refresh, so
// tokens signed with it are refused for up to one interval. That is a refusal, not a
// leak, and it is why `Refresh` is also exported: a deployment that rotates on a
// schedule can call it.
type KeySet struct {
	// url and file are the key set's SOURCE, and exactly one is non-empty — `NewKeySet`
	// refuses both and neither. See `JWKSOptions.File` for what the file source changes and
	// what it does not.
	url        string
	file       string
	client     *http.Client
	interval   time.Duration
	now        func() time.Time
	maxBodyLen int64
	// retryMin is `Run`'s first wait after a failed attempt; see `jwksRetryMin`.
	retryMin time.Duration
	// after is `Run`'s timer. nil means `time.NewTimer`. It is a seam for the tests, which
	// record the waits `Run` asks for and fire them at once, so the retry schedule is
	// asserted as a sequence of durations rather than slept through.
	after func(time.Duration) (<-chan time.Time, func())

	// mu guards everything below. 🔴 IT IS NEVER HELD ACROSS THE HTTP CALL — the same
	// rule `control.Cache.mu` states, for the same reason: a hung provider holding the
	// lock would block every authentication, which is the outage this type exists to
	// survive, reintroduced by the thing built to survive it.
	mu        sync.RWMutex
	keys      map[string]jwkKey
	fetched   bool
	fetchedAt time.Time
	attempted time.Time
	lastErr   error
	fetches   uint64
	failures  uint64
	// started numbers every `Refresh` as it BEGINS; recorded is the number of the newest
	// attempt whose outcome has been committed. See `Refresh`'s ordering guard.
	started  uint64
	recorded uint64
}

// jwkKey is one parsed key, with the algorithm it may be used for.
type jwkKey struct {
	kid string
	typ keyType
	// alg is the algorithm the JWK itself declares, or empty. When declared it is
	// BINDING: a key published as RS256 may not be used to verify an ES256 token.
	alg    Alg
	public any
}

// JWKSOptions configures a KeySet.
type JWKSOptions struct {
	// URL is the JWKS endpoint, and must be `https` unless it is loopback. Exactly one of
	// URL and File is required.
	URL string
	// File is a local JWKS document, read in place of a fetch. Exactly one of URL and File
	// is required; both is `ErrJWKSTwoSources`.
	//
	// 🔴 IT REMOVES THE ISSUER FROM THE STARTUP PATH, AND THAT IS ITS WHOLE PURPOSE. With a
	// URL the first `Refresh` is a network call, and `cmd/cairn-server` refuses to start
	// when it fails — so an issuer that is down, or a DNS answer that is wrong, keeps a pod
	// that holds a perfectly good copy of the public keys from coming up at all. A file the
	// operator ships beside the pod (a mounted configuration object, a baked path) has no
	// such dependency: the read is local, and nothing on this source ever opens a socket.
	//
	// ⚠ WHAT IT DOES NOT CHANGE. The document goes through the SAME `parseJWKS` a fetch
	// does — one parser, so every key rule (a duplicate `kid` refuses the whole document,
	// `oct` is dropped, RSA below `MinRSABits` is dropped, an EC point is checked on the
	// curve) holds for a file exactly as for a URL — the same `maxJWKSBytes` cap, the same
	// last-known-good rule on a failed re-read, and the same `Run` schedule. A rotated file
	// is therefore picked up on the next `Refresh`: the timer, or a caller (the pod's
	// SIGHUP). And the file must be an ABSOLUTE path: a relative one resolves against the
	// process's working directory, which a container's entrypoint chooses and an operator
	// does not read.
	File string
	// Client is the HTTP client used for the fetch. nil means a client with the
	// timeouts below.
	Client *http.Client
	// Interval is the refresh schedule `Run` keeps. Zero means DefaultJWKSInterval —
	// `NewKeySet` substitutes it, so there is NO way to turn the timer off from here.
	//
	// ⚠ THIS SENTENCE USED TO SAY THE OPPOSITE ("zero disables the timer, which leaves
	// `Refresh` as the only way the set is ever built"), and the constructor has always
	// substituted. The code is the safe side of the disagreement and is left alone: a
	// zero reaching `time.NewTicker` panics, and nothing has asked for a no-timer mode.
	Interval time.Duration
	// Now is the clock. nil means `time.Now().UTC()`.
	Now func() time.Time
}

// DefaultJWKSInterval is the refresh schedule.
//
// Ten minutes: short enough that a key added at the provider is usable well inside a
// deploy window, long enough that this pod is not a meaningful load on the provider's
// JWKS endpoint. It bounds the rotated-out-key window in the other direction.
const DefaultJWKSInterval = 10 * time.Minute

// jwksRetryMin is `Run`'s first wait after a failed attempt. Each further consecutive
// failure doubles it, capped at the refresh interval; a success ends the failing run, and
// the next failure after it starts again from this floor.
//
// 🔴 WITHOUT IT, A KEY SET THAT FAILED ITS FIRST FETCH WAITED A WHOLE INTERVAL TO TRY AGAIN.
// `cmd/cairn-ui` does not exit on that failure — it withholds the provider button until
// `KeySetStatus.Fetched` flips — so a provider that became reachable seconds after startup
// left the button shut for up to two intervals: the next tick could still fail on a
// negative-cached DNS answer, and only the tick after it re-armed the door. Five seconds,
// doubling, reaches the interval cap in seven failures (5s…320s, then 10m), so a provider
// that is genuinely down is asked about as often as a healthy one after the first ~10
// minutes, and one that just came up is found within seconds of its cached refusal
// expiring.
//
// ⚠ THE SAME SCHEDULE RUNS AFTER A PREVIOUSLY-GOOD SET STARTS FAILING, not only before the
// first success. Last-known-good keeps serving either way (`Refresh` never discards it), but
// a key ADDED at the provider is refused until a fetch succeeds, so recovering in seconds
// rather than an interval narrows that window too, and the extra load is bounded by the
// same doubling.
const jwksRetryMin = 5 * time.Second

// jwksFetchTimeout bounds one fetch.
//
// 🔴 A TIMEOUT RATHER THAN A CONTEXT DEADLINE ALONE, because the refresher's context is
// the process's and would let one hung fetch occupy the goroutine until shutdown. The
// cache keeps serving either way; what this bounds is how long "the last attempt" stays
// unresolved in `Status`.
const jwksFetchTimeout = 10 * time.Second

// maxJWKSBytes caps the response body.
//
// 🔴 AN UNCAPPED `io.ReadAll` OVER A REMOTE BODY IS THE REMOTE END CHOOSING THIS POD'S
// MEMORY. A JWKS with a handful of keys is well under 8 KiB; 256 KiB is a wide margin
// that is still a bound.
const maxJWKSBytes = 256 << 10

var (
	// ErrJWKSURL refuses a JWKS endpoint that is not one.
	ErrJWKSURL = errors.New("identity: the JWKS URL is unusable")
	// ErrJWKSFile refuses a JWKS file path that is not one.
	ErrJWKSFile = errors.New("identity: the JWKS file path is unusable")
	// ErrJWKSTwoSources refuses a key set given BOTH a URL and a file.
	//
	// 🔴 BOTH SET IS A REFUSAL, NOT A PRECEDENCE RULE — the same ruling `secretFrom` makes
	// for the proxy secret, for the same reason. Two sources for one key set means "which
	// keys verify this token" depends on a precedence nobody reads, and a rotation that
	// updated the source that LOSES appears to work while the pod keeps serving the other.
	ErrJWKSTwoSources = errors.New("identity: the key set names both a JWKS URL and a JWKS file — one key set, one source")
	// ErrJWKSEmpty refuses a JWKS document that parses and yields no usable key.
	ErrJWKSEmpty = errors.New("identity: the JWKS document carries no usable key")
	// ErrJWKSUnmaterialized is what `key` answers before the first successful fetch.
	ErrJWKSUnmaterialized = errors.New("identity: the JWKS has never been fetched, so this build can verify nothing")
)

// NewKeySet builds a cached key set. It does NOT fetch.
//
// 🔴 IT STARTS EMPTY AND VERIFIES NOTHING, WHICH IS THE FAIL-CLOSED DIRECTION AND IS
// NOT THE SAME STATE AS AN OUTAGE — the distinction `control.Cache` draws between
// `unmaterialized` and `stale`, and for the same reason: a set that has never fetched
// has nothing to serve, while one whose provider died an hour ago has last-known-good.
// Conflating them would produce a verifier that accepts nobody while reporting itself
// healthy. Materializing is the caller's first `Refresh`, so a startup failure is the
// caller's to shout about.
func NewKeySet(opts JWKSOptions) (*KeySet, error) {
	if opts.URL != "" && opts.File != "" {
		return nil, ErrJWKSTwoSources
	}
	if opts.File != "" {
		if err := checkJWKSFile(opts.File); err != nil {
			return nil, err
		}
	} else if err := checkJWKSURL(opts.URL); err != nil {
		return nil, err
	}
	client := opts.Client
	if client == nil {
		client = &http.Client{Timeout: jwksFetchTimeout}
	}
	interval := opts.Interval
	if interval == 0 {
		interval = DefaultJWKSInterval
	}
	return &KeySet{
		url:        opts.URL,
		file:       opts.File,
		client:     client,
		interval:   interval,
		now:        opts.Now,
		maxBodyLen: maxJWKSBytes,
		retryMin:   jwksRetryMin,
		keys:       map[string]jwkKey{},
	}, nil
}

// checkJWKSURL refuses an endpoint that cannot carry a trustworthy key set.
//
// 🔴 `https`, OR LOOPBACK. The keys fetched from here decide who every browser session
// is; over plaintext, anyone on the path substitutes their own key set and mints
// sessions for any user in this control plane. The loopback exemption exists so a test
// (and a local proxy that terminates TLS) can run without a certificate, and it is
// keyed on the HOST being loopback rather than on a flag somebody can set in
// production.
func checkJWKSURL(raw string) error {
	if strings.TrimSpace(raw) == "" {
		return fmt.Errorf("%w: it is empty", ErrJWKSURL)
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrJWKSURL, err)
	}
	if parsed.Host == "" {
		return fmt.Errorf("%w: %q names no host", ErrJWKSURL, raw)
	}
	switch parsed.Scheme {
	case "https":
		return nil
	case "http":
		if isLoopbackHost(parsed.Hostname()) {
			return nil
		}
		return fmt.Errorf(
			"%w: %q is plaintext. The keys fetched from a JWKS endpoint decide who every session is, so anybody on the path could substitute their own and mint a session for any user here. Use https",
			ErrJWKSURL, raw)
	default:
		return fmt.Errorf("%w: scheme %q is not http(s)", ErrJWKSURL, parsed.Scheme)
	}
}

// checkJWKSFile refuses a file path that cannot name one document reliably. It does NOT
// read the file: like `checkJWKSURL`, construction checks the SHAPE of the source and the
// first `Refresh` is what materializes it, so `cmd/cairn-server`'s startup refusal has one
// place to come from whichever source is configured.
func checkJWKSFile(path string) error {
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("%w: it is empty", ErrJWKSFile)
	}
	if !filepath.IsAbs(path) {
		return fmt.Errorf("%w: %q is relative, so which file it names depends on the working directory the process was started in. Use an absolute path",
			ErrJWKSFile, path)
	}
	return nil
}

func isLoopbackHost(host string) bool {
	return host == "localhost" || host == "127.0.0.1" || host == "::1" || host == "[::1]"
}

// Refresh fetches the key set. On failure the previous one keeps serving.
//
// 🔴 ON FAILURE `fetchedAt` DOES NOT MOVE, AND THAT IS THE HALF THAT IS EASY TO GET
// WRONG — the same rule `control.Cache.Refresh` states. Advancing the timestamp on a
// failed attempt would reset the reported age to zero on every failure, so a set whose
// provider has been dead for a week would report itself seconds old: a staleness report
// that is most wrong exactly when it is most needed.
//
// 🔴 AND A DOCUMENT THAT YIELDS NO USABLE KEY IS A FAILURE, NOT AN EMPTY SUCCESS. This
// repository has already paid for the other reading one layer down: `tokenfile.Source`
// used to swallow an unreadable store root, and the EMPTY world it produced was
// committed, timestamped and then served through a root that was readable again. An
// empty key set committed here would refuse every session until the next successful
// fetch, and would report itself `fresh` while doing it.
//
// 🔴 AN OLDER READ NEVER OVERWRITES A NEWER ONE'S OUTCOME. Two refreshes can be in flight at
// once — the `Run` timer and a caller's (the pod's SIGHUP re-read of a file source) — and
// because the read happens outside the lock, the one that STARTED first can FINISH last.
// Committing in finish order would let a read taken before an operator removed a `kid`
// re-install that key after the read that saw the removal, and the revoked key would verify
// again until the next refresh. So each attempt is numbered when it begins, and a result
// whose number is below the newest one already committed is DISCARDED: it changes no key,
// no timestamp and no counter, and `Refresh` returns nil because the set it would have
// replaced is newer than anything it read. Pinned by
// `TestAnOlderRefreshFinishingLastDoesNotOverwriteANewerOne`, which blocks the first read
// on purpose.
func (k *KeySet) Refresh(ctx context.Context) error {
	k.mu.Lock()
	k.started++
	attempt := k.started
	k.mu.Unlock()

	// Outside the lock. See `mu`.
	keys, err := k.fetch(ctx)
	now := k.clock()

	k.mu.Lock()
	defer k.mu.Unlock()
	if attempt < k.recorded {
		return nil
	}
	k.recorded = attempt
	k.attempted = now
	if err != nil {
		k.failures++
		k.lastErr = err
		return err
	}
	k.keys = keys
	k.fetched = true
	k.fetchedAt = now
	k.fetches++
	k.lastErr = nil
	return nil
}

// FromFile reports whether this key set's source is a local file rather than a URL. It is
// what `cmd/cairn-server` decides its SIGHUP re-read on: a file is the source an operator
// edits in place and expects to reload on a signal, where a URL is the issuer's and a signal
// would only add a network call to every token-file reload.
func (k *KeySet) FromFile() bool { return k.file != "" }

func (k *KeySet) clock() time.Time {
	if k.now != nil {
		return k.now()
	}
	return time.Now().UTC()
}

func (k *KeySet) fetch(ctx context.Context) (map[string]jwkKey, error) {
	if k.file != "" {
		return k.readFile()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, k.url, nil)
	if err != nil {
		return nil, fmt.Errorf("identity: jwks request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := k.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("identity: jwks fetch: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("identity: jwks fetch: HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, k.maxBodyLen+1))
	if err != nil {
		return nil, fmt.Errorf("identity: jwks read: %w", err)
	}
	if int64(len(body)) > k.maxBodyLen {
		return nil, fmt.Errorf("identity: jwks body exceeds %d bytes", k.maxBodyLen)
	}
	return parseJWKS(body)
}

// readFile is the file source's `fetch`: the same cap and the same parser, and no network.
//
// ⚠ THE CAP IS APPLIED BY A BOUNDED READ, NOT BY A `Stat` FIRST. A size checked before the
// read describes a file that may be replaced before it is opened — a mounted configuration
// object is swapped by a symlink flip — so the bound is on the bytes actually read.
func (k *KeySet) readFile() (map[string]jwkKey, error) {
	f, err := os.Open(k.file)
	if err != nil {
		return nil, fmt.Errorf("identity: jwks file: %w", err)
	}
	defer f.Close()
	body, err := io.ReadAll(io.LimitReader(f, k.maxBodyLen+1))
	if err != nil {
		return nil, fmt.Errorf("identity: jwks file %s: %w", k.file, err)
	}
	if int64(len(body)) > k.maxBodyLen {
		return nil, fmt.Errorf("identity: jwks file %s exceeds %d bytes", k.file, k.maxBodyLen)
	}
	keys, err := parseJWKS(body)
	if err != nil {
		return nil, fmt.Errorf("identity: jwks file %s: %w", k.file, err)
	}
	return keys, nil
}

// Run refreshes on the schedule until ctx is done, logging every failed attempt through
// logf (nil discards).
//
// 🔴 A FAILED REFRESH DOES NOT STOP THE LOOP — the same rule `control.Cache.Run` states.
// Returning on the first error turns a transient provider outage into a permanently
// stale key set that never tries again, with the mechanism that could fix it already
// dead.
//
// 🔴 AND WHILE THE LAST ATTEMPT FAILED — OR NO ATTEMPT HAS EVER SUCCEEDED — THE NEXT ONE IS
// ON A SHORT BACKOFF, NOT THE INTERVAL. See `jwksRetryMin`. The state is read from the set
// itself when the loop starts, so a caller's failed startup `Refresh` puts the loop straight
// onto the backoff rather than a full interval away from its first retry.
//
// 🔴 A FAILED ATTEMPT IS LOGGED EVERY TIME, NOT ONLY ON A STATE CHANGE, AND THE ERROR USED
// TO BE DISCARDED OUTRIGHT. A swallowed error left a pod whose provider was unreachable
// saying nothing at all for the whole interval after its one startup warning — the operator
// could not tell "still failing" from "never retried". Logging only the transition into
// failure would recreate that silence one line later: a week-long outage would be one line
// that has long since rotated out of the log a reader tails. The volume is bounded by the
// backoff itself — at most seven lines before the cap, then one per interval, which is the
// refresh's own cadence — and each line carries the failure count and the next wait, so it
// is a progress report rather than a repeat. A SUCCESS is logged only when it ends a failing
// run, because that line is the "the door re-armed" signal; logging every healthy refresh
// would be noise at exactly the cadence nobody needs.
func (k *KeySet) Run(ctx context.Context, logf func(string)) error {
	if logf == nil {
		logf = func(string) {}
	}
	k.mu.RLock()
	failing := !k.fetched || k.lastErr != nil
	// A caller's failed startup `Refresh` is the first failure of the run this loop
	// inherits, so the count it logs is consecutive across that boundary.
	streak := 0
	if k.lastErr != nil {
		streak = 1
	}
	k.mu.RUnlock()
	backoff := k.retryMin
	for {
		wait := k.interval
		if failing {
			wait = min(backoff, k.interval)
		}
		fire, stop := k.timer(wait)
		select {
		case <-ctx.Done():
			stop()
			return ctx.Err()
		case <-fire:
		}
		err := k.Refresh(ctx)
		if ctx.Err() != nil {
			// A refresh interrupted by shutdown is not a provider failure to report.
			return ctx.Err()
		}
		if err != nil {
			// The ONE place the backoff is set: doubled within a failing run, back to its
			// floor on the first failure after a success.
			if failing {
				backoff = min(backoff*2, k.interval)
			} else {
				backoff = k.retryMin
			}
			failing = true
			streak++
			next := min(backoff, k.interval)
			logf(fmt.Sprintf("WARNING identity: JWKS refresh failed (%s); %d consecutive failure(s); %s; retrying in %s",
				err.Error(), streak, k.servingClause(), next))
			continue
		}
		if failing {
			st := k.Status()
			logf(fmt.Sprintf("identity: JWKS refresh succeeded, ending a failing run; serving %d key(s); %d failure(s) since start; back to every %s",
				st.Keys, st.Failures, k.interval))
		}
		failing, streak = false, 0
	}
}

// servingClause says what a failure leaves serving, which is the half of the line that
// tells an operator whether sessions are affected.
func (k *KeySet) servingClause() string {
	st := k.Status()
	if !st.Fetched {
		return "the key set has NEVER been fetched, so no token can verify"
	}
	return fmt.Sprintf("still serving the last-known-good %d key(s)", st.Keys)
}

func (k *KeySet) timer(d time.Duration) (<-chan time.Time, func()) {
	if k.after != nil {
		return k.after(d)
	}
	t := time.NewTimer(d)
	return t.C, func() { t.Stop() }
}

// key resolves a kid and an algorithm to a verification key. THE HOT PATH. No network.
//
// 🔴 THE ALGORITHM IS CHECKED AGAINST THE KEY'S TYPE HERE, AND IT IS THE SECOND OF THE
// TWO PLACES THE ALGORITHM-CONFUSION ATTACK DIES. `symmetricAlg` refuses an `alg: HS256`
// header in `Verify` before any resolution happens; if that ever stopped being true, the
// `algKeyType` lookup below is two-valued and a shared-secret algorithm has no entry, so
// this returns `ErrTokenAlg` rather than handing out an RSA public key as an HMAC
// secret. And a JWKS `oct` key is never returned at all: `parseJWKS` drops it, so a
// provider (or an attacker who can serve that document) cannot publish a symmetric key
// and have this pod verify tokens with a secret the whole world can read.
func (k *KeySet) key(kid string, alg Alg) (any, error) {
	want, known := algKeyType[alg]
	if !known {
		return nil, fmt.Errorf("%w: %q", ErrTokenAlg, alg)
	}
	k.mu.RLock()
	defer k.mu.RUnlock()
	if !k.fetched {
		return nil, ErrJWKSUnmaterialized
	}
	if kid == "" {
		// 🔴 A TOKEN WITH NO `kid` IS REFUSED WHENEVER THE SET IS AMBIGUOUS, AND
		// "AMBIGUOUS" MEANS MORE THAN ONE CANDIDATE — not more than one key. Trying
		// every key until one verifies is the shape that turns a rotation into a
		// permanent acceptance of the old key and makes the refusal reason depend on
		// iteration order; with exactly one candidate there is nothing to choose.
		var only jwkKey
		found := 0
		for _, candidate := range k.keys {
			if candidate.typ == want && candidate.usableFor(alg) {
				only, found = candidate, found+1
			}
		}
		if found != 1 {
			return nil, fmt.Errorf("%w: the token names no kid and %d keys could verify it", ErrNoKey, found)
		}
		return only.public, nil
	}
	got, held := k.keys[kid]
	if !held {
		return nil, fmt.Errorf("%w: kid", ErrNoKey)
	}
	if got.typ != want || !got.usableFor(alg) {
		return nil, fmt.Errorf("%w: kid %q is a %s key and cannot verify %s", ErrTokenAlg, kid, got.typ, alg)
	}
	return got.public, nil
}

// usableFor answers whether a key whose JWK declared an `alg` may verify this one.
// A key that declares nothing is usable for any algorithm its TYPE supports.
func (j jwkKey) usableFor(alg Alg) bool { return j.alg == "" || j.alg == alg }

// KeySetStatus is the key set's own staleness, as a VALUE.
//
// The same shape and the same reasoning as `control.Staleness`: a value can be rendered
// into a status surface, compared in a test and asserted on, where a log line is read by
// whoever happens to be tailing.
type KeySetStatus struct {
	// Fetched is false until a fetch has succeeded at least once.
	Fetched bool
	// Keys is how many usable keys are being served.
	Keys int
	// FetchedAt is when the serving set was built. Moves only on success.
	FetchedAt time.Time
	// Age is now minus FetchedAt. Zero when never fetched.
	Age time.Duration
	// LastAttempt is when a fetch was last TRIED, successful or not.
	LastAttempt time.Time
	// Failing is true when the LAST attempt failed. Independent of Age.
	Failing bool
	// Fetches and Failures count successful and failed attempts since start.
	Fetches  uint64
	Failures uint64
}

// Status reports the key set's age at this instant.
func (k *KeySet) Status() KeySetStatus {
	now := k.clock()
	k.mu.RLock()
	defer k.mu.RUnlock()
	s := KeySetStatus{
		Fetched:     k.fetched,
		Keys:        len(k.keys),
		FetchedAt:   k.fetchedAt,
		LastAttempt: k.attempted,
		Failing:     k.lastErr != nil,
		Fetches:     k.fetches,
		Failures:    k.failures,
	}
	if k.fetched {
		s.Age = now.Sub(k.fetchedAt)
	}
	return s
}

// String renders one line for a status surface. Every key is always present, so a
// missing field is a visible absence rather than a shorter line that still parses.
func (s KeySetStatus) String() string {
	age, at := "n/a", "never"
	if s.Fetched {
		age = s.Age.Round(time.Millisecond).String()
		at = s.FetchedAt.UTC().Format(time.RFC3339)
	}
	return fmt.Sprintf("jwks: fetched=%t keys=%d age=%s at=%s fetches=%d failures=%d failing=%t",
		s.Fetched, s.Keys, age, at, s.Fetches, s.Failures, s.Failing)
}

// --- JWK parsing ------------------------------------------------------------------

type jwksDocument struct {
	Keys []jwkDocument `json:"keys"`
}

type jwkDocument struct {
	Kty string `json:"kty"`
	Kid string `json:"kid"`
	Use string `json:"use"`
	Alg string `json:"alg"`
	// RSA
	N string `json:"n"`
	E string `json:"e"`
	// EC
	Crv string `json:"crv"`
	X   string `json:"x"`
	Y   string `json:"y"`
}

// parseJWKS turns a JWKS document into the keys this build will verify with.
//
// 🔴 A KEY THIS BUILD CANNOT USE IS SKIPPED, AND A DOCUMENT WITH NO USABLE KEY IS AN
// ERROR. Skipping is right per key — a provider publishing an encryption key or a curve
// this build does not implement is normal and must not take the whole set down — and an
// error is right for the document, because a set that yields nothing is
// indistinguishable from a successful fetch of a page that is not a JWKS at all.
//
// 🔴 AND AN `oct` KEY IS SKIPPED RATHER THAN PARSED, WHICH IS A SECURITY RULE AND NOT A
// COVERAGE GAP. A symmetric key in a PUBLISHED key set is a secret everybody has; a
// verifier that accepted one would verify tokens minted by anyone who read the document.
// This build verifies no shared-secret algorithm from ANY source — see `symmetricAlg` —
// so the skip is now belt to that braces rather than the only thing standing there.
func parseJWKS(body []byte) (map[string]jwkKey, error) {
	var doc jwksDocument
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, fmt.Errorf("identity: jwks is not JSON: %w", err)
	}
	out := map[string]jwkKey{}
	for _, entry := range doc.Keys {
		// `use` is optional; when present, only a signature key is relevant here.
		if entry.Use != "" && entry.Use != "sig" {
			continue
		}
		declared := Alg(entry.Alg)
		if entry.Alg != "" && !declared.Valid() {
			continue
		}
		parsed, err := entry.parse()
		if err != nil {
			continue
		}
		if entry.Alg != "" && algKeyType[declared] != parsed.typ {
			// The document contradicts itself: a key declaring an algorithm its own
			// type cannot perform. Dropped rather than reconciled.
			continue
		}
		// 🔴 A DUPLICATE `kid` IS A REFUSAL FOR THE WHOLE DOCUMENT, NOT A LAST-WINS.
		// Two keys under one id make "which key verifies this token" depend on map
		// iteration order, and a rotation that published both would verify against
		// whichever the range happened to reach.
		if _, clash := out[parsed.kid]; clash {
			return nil, fmt.Errorf("identity: jwks declares kid %q twice", parsed.kid)
		}
		out[parsed.kid] = parsed
	}
	if len(out) == 0 {
		return nil, ErrJWKSEmpty
	}
	return out, nil
}

// MinRSABits is the floor on an RSA modulus.
//
// 2048: the bar every certificate authority and every standards body has required for
// over a decade. A 1024-bit key is a key an attacker can factor with money, and a
// verifier that accepts one accepts sessions minted by whoever did.
const MinRSABits = 2048

func (d jwkDocument) parse() (jwkKey, error) {
	switch d.Kty {
	case string(keyRSA):
		n, err := decodeSegment(d.N)
		if err != nil {
			return jwkKey{}, fmt.Errorf("rsa n: %w", err)
		}
		e, err := decodeSegment(d.E)
		if err != nil {
			return jwkKey{}, fmt.Errorf("rsa e: %w", err)
		}
		if len(e) == 0 || len(e) > 8 {
			return jwkKey{}, errors.New("rsa e is out of range")
		}
		modulus := new(big.Int).SetBytes(n)
		if modulus.BitLen() < MinRSABits {
			return jwkKey{}, fmt.Errorf("rsa modulus is %d bits, floor is %d", modulus.BitLen(), MinRSABits)
		}
		exponent := new(big.Int).SetBytes(e)
		if !exponent.IsInt64() || exponent.Int64() < 3 || exponent.Int64() > 1<<31 {
			return jwkKey{}, errors.New("rsa e is out of range")
		}
		return jwkKey{kid: d.Kid, typ: keyRSA, alg: Alg(d.Alg), public: &rsa.PublicKey{
			N: modulus, E: int(exponent.Int64()),
		}}, nil

	case string(keyEC):
		if d.Crv != "P-256" {
			// Only the curve Supabase issues. A curve this build does not implement is
			// not a key it can use, and guessing a mapping is how a verifier ends up
			// checking a signature on the wrong group.
			return jwkKey{}, fmt.Errorf("unsupported curve %q", d.Crv)
		}
		x, err := decodeSegment(d.X)
		if err != nil {
			return jwkKey{}, fmt.Errorf("ec x: %w", err)
		}
		y, err := decodeSegment(d.Y)
		if err != nil {
			return jwkKey{}, fmt.Errorf("ec y: %w", err)
		}
		const coordLen = 32 // P-256
		if len(x) != coordLen || len(y) != coordLen {
			// RFC 7518 §6.2.1.2 fixes the coordinate length to the curve's field size.
			// A short coordinate that gets left-padded is a DIFFERENT point spelled to
			// look like this one.
			return jwkKey{}, fmt.Errorf("ec coordinates are %d/%d bytes, want %d", len(x), len(y), coordLen)
		}
		// 🔴 THE POINT IS VALIDATED ON THE CURVE, VIA `crypto/ecdh`, BEFORE IT BECOMES A
		// KEY. An off-curve or identity point is an invalid-curve attack primitive, and
		// `ecdsa.Verify` does not reject one for you. `ecdh.P256().NewPublicKey` performs
		// exactly this check on an uncompressed point and is not deprecated, where
		// `elliptic.Curve.IsOnCurve` is.
		uncompressed := append([]byte{0x04}, append(x, y...)...)
		if _, err := ecdh.P256().NewPublicKey(uncompressed); err != nil {
			return jwkKey{}, fmt.Errorf("ec point is not on P-256: %w", err)
		}
		return jwkKey{kid: d.Kid, typ: keyEC, alg: Alg(d.Alg), public: &ecdsa.PublicKey{
			Curve: elliptic.P256(),
			X:     new(big.Int).SetBytes(x),
			Y:     new(big.Int).SetBytes(y),
		}}, nil

	default:
		// `oct` lands here deliberately. See the function comment.
		return jwkKey{}, fmt.Errorf("unsupported kty %q", d.Kty)
	}
}

// ⚠ THERE IS DELIBERATELY NO SECOND `keyResolver` IN THIS FILE. A `staticSecret` type
// (one configured HMAC secret, no key set, no network) and a `resolverPair` that routed
// HS256 to it lived here while this build still verified Supabase's LEGACY symmetric
// tokens. Both are deleted: a deployment's keys now come from a JWKS document — fetched from
// a URL or read from a file, through the one `parseJWKS` — and from nowhere else, which is
// what makes `symmetricAlg`'s refusal one rule with one arm rather than a property of how a
// particular deployment was configured.
