package ui

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"sync"
	"time"

	"github.com/ZacxDev/cairn/internal/control"
	"github.com/ZacxDev/cairn/internal/identity"
	"github.com/ZacxDev/cairn/internal/netid"
)

// OAuthAuthority is the provider sign-in this surface drives, as an interface so the
// handlers can be tested without a network.
//
// 🔴 IT IS THE SAME SHAPE AS `Config.Credentials` AND FOR THE SAME REASON: values in, a
// principal out, and NO `*http.Request` in any signature. The exchange must resolve the code
// the provider handed back and NOTHING the request also carries — most of all not the
// session cookie the browser may already hold. An interface taking the request would put
// that cookie in scope, and a callback whose exchange was refused could "succeed" as
// whatever the browser arrived with.
//
// ⚠ IT RETURNS A PRINCIPAL AND NOT AN `identity.Identity`, so no caller here can freeze an
// authorization. `identity.Session`'s comment records why a session holds a principal
// reference rather than a snapshot of authority: the authority is re-resolved on every
// request, so a grant revoked mid-session takes effect on the next one.
type OAuthAuthority interface {
	// AuthorizeURL is where the browser is sent, carrying the PKCE challenge.
	AuthorizeURL(challenge string) string
	// Exchange turns the callback's authorization code plus the flight's verifier into a
	// principal this control plane holds. Every failure — an unreachable provider, a
	// refused code, a verified token naming nobody here — is an error.
	Exchange(ctx context.Context, code, verifier string) (control.Principal, error)
}

// oauthFlightCookieName is the cookie that binds a started sign-in to ONE browser.
//
// 🔴 `__Host-` FOR EXACTLY THE REASON `identity.SessionCookieName` CARRIES IT, and here the
// thing it protects is the PKCE binding. A cookie without the prefix can be set by any
// sibling host under the registrable domain, so a sibling could plant a flight id of its own
// choosing — and a flight id it chose is one it can also complete, which is the login
// fixation this cookie exists to prevent.
//
// ⚠ IT IS A SEPARATE COOKIE FROM THE SESSION ONE AND MUST STAY SEPARATE. Reusing the
// session cookie's name would mean a browser that starts a sign-in loses the session it
// already had — a signed-in user who clicks the GitHub button and then abandons the flow
// would be signed out by the click.
const oauthFlightCookieName = "__Host-cairn-oauth"

// GitHubLabel is what the button says and what the log line names, spelled ONCE. Two
// spellings of the provider's name is how a page ends up offering one provider and a log
// recording another.
const GitHubLabel = "GitHub"

// FlightTTL bounds how long a started sign-in may be completed in.
//
// 🔴 IT IS SHORT BECAUSE A FLIGHT IS A LIVE CREDENTIAL-IN-WAITING, NOT A SESSION. Five
// minutes is longer than any provider round trip a person will sit through and far shorter
// than the session lifetime; the cost of it being too short is one retry, and the cost of
// it being long is a window in which a planted flight is still completable.
const FlightTTL = 5 * time.Minute

// maxOpenFlights bounds the table's TOTAL size, and `maxFlightsPerClient` bounds one
// caller's share of it. Two bounds, because one of them alone is not a bound anybody wants.
//
// 🔴 A FLIGHT IS CREATED BY AN UNAUTHENTICATED REQUEST, SO WITHOUT A CAP THE START ROUTE IS
// A MEMORY-EXHAUSTION ENDPOINT. The same-origin gate refuses a cross-site POST, so an attacker
// has to drive it directly — which anybody can.
//
// 🔴 AND A GLOBAL CAP ALONE IS A DENIAL OF SERVICE WITH EXTRA STEPS, WHICH IS WHY THE
// PER-CLIENT ONE EXISTS. With only `maxOpenFlights`, one anonymous caller reaches the whole
// bound on its own — 1024 POSTs, refreshed every five minutes — and every OTHER person's
// GitHub button then refuses until the oldest flights expire. The per-client number is what
// stops one caller spending everybody else's share. `netid`'s own comment makes the same ruling
// about a limiter with one bucket: a single shared key means the first abuser locks out
// everybody.
//
// 🔴 WHAT THE PER-CLIENT NUMBER BOUNDS IS THE **RATE**, AND THAT IS TRUE ONLY BECAUSE A SPENT
// FLIGHT KEEPS ITS SLOT — the mechanism is in [take] and the earlier version of this sentence
// was FALSE. While the callback deleted the record, the slot freed at the start of the token
// exchange, so this cap bounded concurrency alone: measured, one client key drove 200
// completed exchanges and reached the provider 200 times with the table at 0. A consumed
// record now occupies its client's share until it EXPIRES, so the honest statement is: at most
// `maxFlightsPerClient` flight starts — and therefore at most that many outbound requests to
// the identity provider — per client per `FlightTTL`.
//
// 🔴 AND THE SAME MECHANISM SILENTLY TURNED THE *GLOBAL* CAP INTO A GLOBAL RATE BOUND, WHICH
// THIS PARAGRAPH OMITTED AND IS THE PARAGRAPH WRITTEN TO STATE SUCH THINGS. Because a spent
// record holds its slot, `maxOpenFlights` now bounds TOTAL SIGN-IN STARTS ACROSS ALL CLIENTS
// per `FlightTTL`: 1024 successful GitHub sign-ins inside any five-minute window make the
// button answer 503 for everybody until the oldest expire. Before the fix a completed sign-in
// freed its slot at once, so the global number really did bound only memory. It is academic at
// this surface's load — a single-replica notes UI — and it is an unstated behaviour change
// otherwise, which is the defect regardless of the load.
//
// ⚠ WHAT ELSE THEY COST. A caller behind a shared egress address shares a client identity, so a
// busy office shares `maxFlightsPerClient` sign-ins per `FlightTTL` between them — and a person
// who signs in, signs out and signs in again spends one slot each time rather than reusing it.
// Eight starts per five minutes is far above any human rate and well below an amplifier's. Every
// cost here is a delayed GitHub sign-in, never a delayed one through the credential form — that
// door does not touch this table at all, which is one more reason it is kept.
const (
	maxOpenFlights      = 1024
	maxFlightsPerClient = 8
)

// flight is one started, uncompleted sign-in.
//
// 🔴 IT HOLDS THE PKCE VERIFIER AND THE VERIFIER NEVER LEAVES THIS PROCESS. The alternative
// — carrying it in the browser's own cookie, which needs no table at all — was weighed and
// refused on ONE property: single use. A cookie is deleted by ASKING the browser to delete
// it, so a client that declines cannot be made to; a record the SERVER marks consumed cannot
// be presented twice whatever the client does. ⚠ THIS SAID "a map entry removed on read", WHICH
// WAS THE MECHANISM FOR ONE COMMIT AND IS NOT THE MECHANISM NOW — see [take]: deleting on read
// freed the caller's rate slot at the start of the token exchange, so a record is MARKED rather
// than removed and is pruned by expiry. The argument is unchanged and never depended on the
// delete: what a cookie cannot offer is a decision the SERVER makes. The second-order cost is
// stated at [flights]: this table is in memory, so a restart mid-flight loses it.
type flight struct {
	verifier string
	expires  time.Time
	// client is the `netid.ResolveClient` identity that opened this flight, and it is held
	// ONLY so `maxFlightsPerClient` can be counted. It is never compared at the callback:
	// the binding there is the flight COOKIE, which is unguessable, and re-checking the
	// client would break every legitimate sign-in that changes address mid-flow — a phone
	// moving from wifi to cellular between the button and the provider's redirect.
	client string
	// consumed marks a flight whose verifier has been handed to an exchange. It is what
	// makes the record single-use, and — because a consumed record keeps occupying its
	// client's share until it expires — it is also what turns `maxFlightsPerClient` into a
	// bound on the RATE of outbound exchanges rather than on their concurrency. See [take].
	consumed bool
}

// flights is the open-sign-in table.
//
// 🔴 IN MEMORY, DELIBERATELY, AND THE DECISION IS *NOT* THE ONE `identity/session.go` MAKES
// ABOUT SESSIONS. That file rejects in-memory storage for sessions because a restart signs
// everybody out and because a second replica would have no shared truth. Neither cost lands
// here, and the reason is the requirement: a session must survive a rolling deploy, while a
// flight is a five-minute window a human is actively waiting inside. A restart mid-flight
// costs ONE person ONE retry of a button they are looking at; a restart mid-session costs
// every signed-in user their session. So the durable store is not just unnecessary here, it
// would be worse — it would write a live PKCE verifier to disk, where the whole reason the
// session table holds `sha256(id)` rather than the id is that a file of live credentials is a
// file worth stealing.
//
// ⚠ THE REPLICA LIMIT IS REAL AND IS THE SAME ONE THE SURFACE ALREADY DECLARES. A flight
// started on replica A cannot be completed on replica B, so a second replica without sticky
// routing breaks the GitHub button — and `internal/identity/session.go` already records that
// `cairn-ui` is a single-replica surface today and that the intended answer for more than
// one is sticky routing. This adds no new limit; it inherits the declared one.
type flights struct {
	mu   sync.Mutex
	open map[string]flight
	now  func() time.Time
}

func newFlights(now func() time.Time) *flights {
	return &flights{open: map[string]flight{}, now: now}
}

// flightRefusal says WHICH bound refused, for the log line and for a test that must not
// credit one bound with the other's kill.
type flightRefusal uint8

const (
	flightOpened flightRefusal = iota
	// flightRefusedPerClient: this caller already holds `maxFlightsPerClient` live records.
	flightRefusedPerClient
	// flightRefusedGlobal: the whole table is at `maxOpenFlights`.
	flightRefusedGlobal
	// flightRefusedNoID: `crypto/rand` failed, so no unguessable id could be minted.
	//
	// 🔴 UNREACHABLE ON THE PINNED TOOLCHAIN, AND KEPT ANYWAY — WRITTEN DOWN BECAUSE A
	// BRANCH NOBODY CAN REACH IS EXACTLY WHAT THIS REPOSITORY CALLS A DEFECT WHEN IT IS
	// NOT LABELLED. Since Go 1.24 `crypto/rand.Read` "never returns an error and always
	// fills b entirely"; it calls `io.ReadFull` on `Reader` and **crashes the program
	// irrecoverably** if that fails, which a replaced `Reader` cannot turn back into a
	// returned error. So `newFlightID` cannot report this and the 500-vs-503 distinction
	// the handler draws for it cannot be observed on go.mod's 1.25. It stays for two
	// reasons: the signature is `(string, error)` and discarding an error because today's
	// implementation cannot produce one is how a guard rots, and the alternative to a
	// random id is a guessable one. ⚠ Do not write a test for it — there is no way to
	// reach it, and a test that appeared to would be measuring something else.
	flightRefusedNoID
)

func (r flightRefusal) String() string {
	switch r {
	case flightRefusedPerClient:
		return "this client already holds the maximum open flights"
	case flightRefusedGlobal:
		return "the flight table is full"
	case flightRefusedNoID:
		return "no flight id could be generated"
	default:
		return "opened"
	}
}

// start records a verifier and returns the id the browser must come back with.
//
// It prunes expired records on every insert rather than on a timer, for the reason
// `FileSessionStore.Create` gives about the same choice: a background goroutine whose
// failure is silent is worse than a bound maintained by the path that grows the table.
//
// 🔴 THE PRUNE AND THE PER-CLIENT COUNT ARE ONE PASS, AND THE ORDER IS LOAD-BEARING. Counting
// before pruning would charge a caller for flights that have already expired, so somebody who
// tried eight times across an hour would be refused on the ninth for records that no longer
// exist. The single pass also means the per-client number is a count of LIVE flights, which is
// the only number the bound is about.
func (f *flights) start(client, verifier string, ttl time.Duration) (string, flightRefusal) {
	id, err := newFlightID()
	if err != nil {
		return "", flightRefusedNoID
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	now := f.now()
	mine := 0
	for key, rec := range f.open {
		if !now.Before(rec.expires) {
			delete(f.open, key)
			continue
		}
		if rec.client == client {
			mine++
		}
	}
	// The per-client bound is checked FIRST, so a caller that has spent its own share is told
	// that rather than being told the table is full — which would be a fact about everybody
	// else and would send an operator looking in the wrong place.
	if mine >= maxFlightsPerClient {
		return "", flightRefusedPerClient
	}
	if len(f.open) >= maxOpenFlights {
		return "", flightRefusedGlobal
	}
	f.open[id] = flight{verifier: verifier, expires: now.Add(ttl), client: client}
	return id, flightOpened
}

// take marks a flight CONSUMED and returns its verifier. SINGLE USE: a second presentation of
// the same id finds a record that is already consumed and is refused.
//
// 🔴 IT MARKS RATHER THAN DELETES, AND THAT ONE WORD IS WHAT MAKES THE PER-CLIENT BOUND A
// BOUND. Deleting here freed the caller's slot at the START of the token exchange, so the
// cap bounded CONCURRENCY and nothing else — measured by probe: one client key drove **200
// completed exchanges, the provider was reached 200 times, and the table held 0 open
// flights**. That is a public, unauthenticated endpoint amplifying one request into one
// outbound request against the identity provider, each holding a socket for up to
// `supabaseOAuthTimeout`. Because a consumed record keeps its slot until it EXPIRES, one
// client now gets at most `maxFlightsPerClient` flight starts — and therefore at most that
// many outbound exchanges — per `FlightTTL`. The bound is on the RATE, which is what the
// comment on the cap claimed all along.
//
// ⚠ SO THE TABLE HOLDS SPENT RECORDS ON PURPOSE, AND `openCount` COUNTS THEM. A reader
// expecting the table to empty after a successful sign-in will be surprised; that is the
// mechanism, not a leak. The record is small, carries no secret once consumed (the verifier is
// zeroed below), and is pruned by the same expiry sweep as everything else.
//
// ⚠ THE VERIFIER IS CLEARED WHEN THE RECORD IS CONSUMED. It has served its only purpose by
// then, and a spent record that kept it would leave a live PKCE secret in memory for the rest
// of the TTL for no reason — which matters more now that a spent record is KEPT until expiry
// rather than deleted. `TestAFlightIsSingleUseAndBoundToItsBrowser` asserts it on the table's
// own internals, because nothing observable from outside can see a field that is not read.
func (f *flights) take(id string) (string, bool) {
	if id == "" {
		return "", false
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	rec, held := f.open[id]
	if !held || rec.consumed || !f.now().Before(rec.expires) {
		return "", false
	}
	verifier := rec.verifier
	rec.consumed = true
	rec.verifier = ""
	f.open[id] = rec
	return verifier, true
}

// openCount is the table's size. For a test's instrument control, never for a decision.
func (f *flights) openCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.open)
}

// pkceBytes is the entropy behind a flight id and behind a PKCE verifier.
//
// 🔴 32 BYTES, WHICH IS `identity.SessionIDBytes` AND THE SAME ARGUMENT: both values are
// bearer credentials for the length of the flight, and the only thing between an attacker
// and either of them is that they cannot be guessed. Base64url of 32 bytes is 43
// characters, which also lands inside RFC 7636's 43–128 range for a code verifier without
// any padding or trimming.
const pkceBytes = 32

func randomPKCEValue() (string, error) {
	buf := make([]byte, pkceBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// newFlightID mints a flight id. Named apart from the verifier mint so a reader can see
// they are two independent values drawn from the same entropy: the id is what the browser
// holds, the verifier is what it never sees. A single value used as both would make the
// verifier readable by anybody who sees the cookie.
func newFlightID() (string, error) { return randomPKCEValue() }

// pkceChallenge is `base64url(sha256(verifier))`, which is RFC 7636's `S256`.
//
// ⚠ RAW (UNPADDED) URL ENCODING IS REQUIRED, NOT COSMETIC. A `=`-padded challenge is a
// different string from the one the provider recomputes, so the exchange would be refused
// at the END of the flow — after the person has already authorised the app.
func pkceChallenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// The two things a failed provider sign-in can say, and they are DIFFERENT strings because
// they are different facts.
//
// 🔴 `oauthIncomplete` IS NOT A CREDENTIAL OUTCOME AND MUST NOT SHARE `signInRefused`'s
// SENTENCE. It means the request did not carry a completable flight — no cookie, an expired
// one, a replayed one, no code — which says nothing whatever about any credential. Merging
// the two would make the uniform credential refusal appear for a state that is usually just
// a stale tab, and an operator reading the log could not tell a refused sign-in from an
// abandoned one.
//
// ⚠ THE EXCHANGE'S OWN FAILURES DO *NOT* GET THIS SENTENCE. A code the provider refused, a
// token that does not verify, and a verified token naming a user this control plane does not
// hold all render `signInRefused` — the same sentence the token form gives — because those
// ARE credential outcomes and discriminating between them is the enumeration API
// `signInRefused`'s own comment refuses.
const (
	oauthIncomplete  = "That sign-in did not complete. Start again from this page."
	oauthUnavailable = "Signing in with GitHub is not configured on this deployment."
	// oauthNotReady is the TRANSIENT sibling of `oauthUnavailable`, and they are two
	// sentences because they need two different actions from two different people.
	// "not configured" is for an operator who must set `CAIRN_SUPABASE_REDIRECT_URL`;
	// this one is for a person who should use the other door and try again shortly.
	oauthNotReady = "Signing in with " + GitHubLabel + " is temporarily unavailable. " +
		"Use the credential form below, or try again shortly."
	// oauthNotStarted is for a failure that happened BEFORE any credential was involved:
	// this process could not identify the caller, or the caller is locked out, or its own
	// entropy source failed.
	//
	// 🔴 IT IS A THIRD NON-DISCRIMINATING STRING RATHER THAN A THIRD USE OF
	// `signInRefused`, AND THE DIFFERENCE IS THAT ONE OF THEM IS TRUE. The
	// non-disclosure argument establishes that these failures must be INDISTINGUISHABLE
	// from one another — it does not establish that they must borrow a sentence saying
	// "That credential was not accepted." to somebody who typed no credential. This
	// string satisfies the argument identically: it is fixed, it is the same for every
	// caller and every reason, and it discloses nothing about the credential table or
	// about which check fired. ⚠ It must stay that way — a variant per reason would
	// rebuild exactly the enumeration `signInRefused`'s comment refuses.
	oauthNotStarted = "That sign-in could not be started. Try again, or use the credential form below."
)

// oauthFlightCookie is the ONE place the flight cookie's attributes are chosen.
//
// 🔴 EVERY FLAG IS `identity.SessionCookie`'s, FOR THE SAME REASON, PLUS ONE DIFFERENCE
// THAT IS THE POINT: `MaxAge` is the flight's, not the session's. A flight cookie that
// outlived its record would be a browser holding a key to a lock that no longer exists,
// and every subsequent callback would spend a map lookup on it.
//
// ⚠ `SameSite=Lax` RATHER THAN `Strict`, AND HERE THAT IS LOAD-BEARING RATHER THAN A
// CONVENIENCE. The callback is a top-level GET navigation from the PROVIDER's origin, which
// is cross-site: `Strict` would not attach this cookie to it, so every GitHub sign-in would
// arrive with no flight and be refused. `Lax` attaches it to exactly that shape and still
// withholds it from a cross-site POST.
func oauthFlightCookie(id string, ttl time.Duration) *http.Cookie {
	return &http.Cookie{
		Name:     oauthFlightCookieName,
		Value:    id,
		Path:     "/",
		MaxAge:   int(ttl.Seconds()),
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	}
}

// clearedOAuthFlightCookie is sent on every callback, successful or not.
//
// 🔴 IT IS HOUSEKEEPING AND NOT THE SINGLE-USE GUARD, WHICH IS THE SAME DISTINCTION
// `identity.ClearedSessionCookie` DRAWS. `flights.take` MARKS THE RECORD CONSUMED, and that is
// what makes a replay useless; asking the browser to forget the id only stops it being sent
// again. Relying on this instead would be relying on the client to discard its own credential.
// ⚠ THIS NAMED THE WRONG MECHANISM — "`flights.take` deletes the record" — and it is the one
// sentence a maintainer reads to learn WHICH thing is the guard. It stopped deleting in the same
// commit that wrote that; see [take].
func clearedOAuthFlightCookie() *http.Cookie {
	return &http.Cookie{
		Name:     oauthFlightCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	}
}

// handleOAuthStart begins a GitHub sign-in. It is PUBLIC, and it is a POST.
//
// 🔴 A POST RATHER THAN A LINK, AND THE METHOD IS WHAT BUYS THE CROSS-SITE GATE. `GET` is
// in `stateChanging`'s safe set, so a link here would be reachable by any `<img src>` in the
// world and by every link prefetcher — each of which would mint a flight and overwrite the
// visitor's flight cookie. As a POST it is refused by gate (2) unless the request came from
// this origin, so a flight can only be started from this surface's own sign-in page. That
// is the same ruling `signOutForm` records for the same reason.
//
// ⚠ IT CARRIES NO CSRF TOKEN AND CANNOT: it is reached before any session exists, so there
// is nothing to derive one from. Gate (2) is what stands in front of it — which is why gate
// (2) is derived from the METHOD and runs BEFORE authentication.
func (s *Server) handleOAuthStart(w http.ResponseWriter, r *http.Request, _ identity.Identity) {
	if s.oauth == nil {
		s.refuseUnconfiguredOAuth(w)
		return
	}
	if !s.providerArmed() {
		s.refuseUnreadyOAuth(w)
		return
	}
	// 🔴 THE CLIENT IS RESOLVED BEFORE ANY WORK, AND `netid.ResolveClient` IS REUSED RATHER
	// THAN REIMPLEMENTED — the same ruling `handleSignIn` records, for the same reason: the
	// trust boundary is the whole difficulty and it is already decided there. The header is
	// read ONLY from a peer inside the allowlist; every other peer is keyed on its own
	// address.
	client, trusted, ok := netid.ResolveClient(r.Header, r.RemoteAddr, s.trustedProxies)
	if !ok {
		// FAIL CLOSED, AND COUNT NOTHING. There is no bucket to count into, and the
		// alternative — one shared key for every unidentifiable request — is the failure
		// `internal/netid` exists to avoid.
		//
		// ⚠ IT RENDERS `oauthNotStarted`, WHICH IS NEITHER OF THE OTHER TWO SENTENCES, AND
		// THE THREE-WAY SPLIT IS THE POINT. `oauthIncomplete` is for a request that did not
		// carry a completable FLIGHT — a stale tab, an abandoned flow. `signInRefused` is for
		// a CREDENTIAL that was not accepted. This refusal and the lockout below are neither:
		// nothing was presented and nothing was flying, so both borrow a third fixed string.
		// ⚠ AN EARLIER DRAFT USED `signInRefused` HERE, reasoning from `handleSignIn`'s rule
		// that refusals to begin must share one sentence. The rule is right and the
		// conclusion was one step too far: it requires these failures to be
		// indistinguishable FROM EACH OTHER, not to claim a credential was rejected to
		// somebody who typed none. `oauthNotStarted` is just as uninformative and is true.
		s.logf("github sign-in refused: no client identity could be resolved from peer %q", r.RemoteAddr)
		s.renderSignIn(w, http.StatusUnauthorized, oauthNotStarted)
		return
	}

	// 🔴 THE EXISTING SIGN-IN LOCKOUT COVERS THIS DOOR TOO, AND ITS ABSENCE HERE WAS A REAL
	// HOLE: a client locked out for five failed credential attempts could still drive the
	// start row without limit. Consulting it costs one map read and needs no new state.
	//
	// ⚠ BUT THIS HANDLER DELIBERATELY DOES NOT `RecordFailure`, AND THAT IS A DIVERGENCE
	// WORTH STATING RATHER THAN A FORGOTTEN CALL. `netid.RateLimiter` counts FAILED AUTH into
	// one bucket per client, shared with `POST /sign-in`, at `DefaultMaxFailures` = 5 with a
	// 15-minute lockout. Starting a sign-in is not a failed one: recording it would mean five
	// clicks of this button — back, retry, back, retry, which a slow provider makes ordinary —
	// lock the caller out of the CREDENTIAL FORM for fifteen minutes, behind a refusal that
	// deliberately explains nothing. That trades a real regression on the door that always
	// works for a bound the flight table can hold itself. So the rate of flight CREATION is
	// bounded by `maxFlightsPerClient`, where the thing being bounded lives, and the limiter
	// keeps meaning exactly what its own comment says it means.
	if s.limiter != nil && s.limiter.LockedOut(client) {
		s.logf("github sign-in refused: %s is locked out (client identity %s)", client, peerState(trusted))
		s.renderSignIn(w, http.StatusUnauthorized, oauthNotStarted)
		return
	}

	verifier, err := randomPKCEValue()
	if err != nil {
		// `crypto/rand` failing is not a condition to carry on through: the alternative to
		// a random verifier is a guessable one, and a guessable verifier is no PKCE at all.
		//
		// ⚠ ALSO UNREACHABLE ON THE PINNED TOOLCHAIN — same reason as `flightRefusedNoID`,
		// which says it once and at length. Both are defensive and neither is testable.
		s.logf("github sign-in aborted: no PKCE verifier could be generated: %v", err)
		s.renderSignIn(w, http.StatusInternalServerError, oauthNotStarted)
		return
	}
	id, outcome := s.flights.start(client, verifier, FlightTTL)
	if outcome != flightOpened {
		// The log names WHICH bound refused, because "the table is full" and "you have spent
		// your own share" send an operator to completely different places — and a surface
		// refusing every GitHub sign-in while the token form works is otherwise a silent
		// half-outage.
		s.logf("github sign-in refused: %s (%s, %d open, per-client cap %d, global cap %d, client identity %s)",
			outcome, client, s.flights.openCount(), maxFlightsPerClient, maxOpenFlights, peerState(trusted))
		// 🔴 AN ENTROPY FAILURE IS A 500 AND A SPENT BOUND IS A 503, WHICH THE FIRST DRAFT
		// CONFLATED. They are not the same fact: `crypto/rand` failing is this process being
		// broken, and a cap being reached is this process working exactly as configured. The
		// sibling `randomPKCEValue` failure above already answers 500, so answering 503 for
		// the identical failure one line down was two codes for one condition.
		if outcome == flightRefusedNoID {
			s.renderSignIn(w, http.StatusInternalServerError, oauthNotStarted)
			return
		}
		s.renderSignIn(w, http.StatusServiceUnavailable, oauthIncomplete)
		return
	}
	http.SetCookie(w, oauthFlightCookie(id, FlightTTL))
	// 303, not 302, for the reason `handleSignIn`'s redirect gives: the browser must follow
	// it with a GET, and a 302 leaves the method to the client.
	http.Redirect(w, r, s.oauth.AuthorizeURL(pkceChallenge(verifier)), http.StatusSeeOther)
}

// handleOAuthCallback completes a GitHub sign-in. It is PUBLIC, and it is a GET because the
// PROVIDER decides the method — a redirect is a GET, and no server can ask for another.
//
// 🔴 A STATE-CHANGING HANDLER BEHIND A SAFE METHOD, WHICH IS THE ONE PLACE THIS SURFACE HAS
// ONE, AND THE GUARD IS THE FLIGHT RATHER THAN THE METHOD. `stateChanging` calls `GET` safe,
// so neither cross-site gate covers this route; what covers it is that completing a sign-in
// requires BOTH the authorization code AND the flight cookie, and the two cannot be assembled
// by a third party. An attacker who obtains a code of their own and makes a victim's browser
// open this URL loses twice: with no flight cookie there is nothing to exchange with, and
// with the victim's OWN flight cookie the exchange presents the VICTIM's PKCE verifier
// against the ATTACKER's code, which the token endpoint refuses. That is the property a
// `state` parameter would otherwise buy, and `internal/identity`'s `AuthorizeURL` records why
// it is not spelled as one.
//
// ⚠ THE FLIGHT IS CONSUMED BEFORE THE QUERY IS READ. A handler that checked the query first
// and consumed the flight later would leave a replayable flight behind for every malformed
// callback, which is the cheapest request an attacker can send.
//
// 🔴 THAT IS NARROWER THAN THE SENTENCE THAT STOOD HERE, AND THE COMMIT THAT WROTE THAT
// SENTENCE IS THE ONE THAT FALSIFIED IT. It read "CONSUMED BEFORE ANYTHING ELSE IS READ, AND
// UNCONDITIONALLY" — and the same commit added a `providerArmed()` early return and a
// `ResolveClient` call ABOVE the consumption, so it was neither first nor unconditional. The
// wide version also reads as licensing the next early return, which is how the claim would
// have decayed further. The measured property is the QUERY ordering, and
// `TestTheProviderErrorIsNotReflectedIntoThePage` is what measures it; the two early returns
// above consume nothing and answer before any flight is looked at, which is why they are
// compatible with the narrow claim and would not have been with the wide one.
func (s *Server) handleOAuthCallback(w http.ResponseWriter, r *http.Request, _ identity.Identity) {
	if s.oauth == nil {
		s.refuseUnconfiguredOAuth(w)
		return
	}
	if !s.providerArmed() {
		s.refuseUnreadyOAuth(w)
		return
	}

	// 🔴 THE CLIENT IS RESOLVED FOR THE LOG, AND THAT IS NOT DECORATION HERE. Every
	// `POST /sign-in` refusal carries a client identity because this surface is reachable
	// from the internet and a refusal nobody can attribute is a refusal nobody can act on.
	// The callback's refusals carried none — which is exactly the half an operator needs
	// when the start row is being driven in bulk, since the start row's own refusals name a
	// client and its callbacks did not. A failure to resolve one is NOT fatal here: unlike
	// the start row this handler opens nothing, so there is no bucket to protect, and
	// refusing a legitimate callback because a header was odd would break a sign-in that
	// had already been authorised at the provider.
	client, trusted, resolved := netid.ResolveClient(r.Header, r.RemoteAddr, s.trustedProxies)
	who := "client identity " + peerState(trusted)
	if !resolved {
		client, who = "unresolved", "no client identity"
	}

	// The browser is asked to forget the flight id on every path from HERE on, including every
	// refusal: a stale id it keeps sending is a lookup per request and a reason to believe a
	// sign-in is still in progress when it is not.
	//
	// ⚠ "ON EVERY PATH THROUGH THIS HANDLER" IS WHAT THIS SAID, AND THE TWO EARLY RETURNS ABOVE
	// MAKE IT FALSE — an unconfigured or unready provider answers without clearing anything.
	// That is harmless (neither can have minted a flight for this browser) and it is not what
	// the sentence claimed, so the claim is scoped to where it is true rather than left as a
	// sentence a reader would check and find wrong.
	http.SetCookie(w, clearedOAuthFlightCookie())

	var flightID string
	if cookie, err := r.Cookie(oauthFlightCookieName); err == nil {
		flightID = cookie.Value
	}
	verifier, held := s.flights.take(flightID)
	if !held {
		// No cookie, an expired flight, or a replay. All three are the same observable
		// deliberately: a callback that said which would tell a caller whether a given
		// flight id had ever existed.
		s.logf("github sign-in refused: the callback carried no completable flight (%s, %s)", client, who)
		s.renderSignIn(w, http.StatusBadRequest, oauthIncomplete)
		return
	}

	// 🔴 THE PROVIDER'S OWN ERROR IS NOT REFLECTED BACK. GoTrue appends `error` and
	// `error_description` when a user declines, and `error_description` is attacker-
	// influenceable text arriving in a query parameter — rendering it would put somebody
	// else's string in this page. The fact that the provider refused is worth a log line;
	// the person sees the same sentence an abandoned flow gives, because that is what
	// declining is.
	query := r.URL.Query()
	if providerErr := query.Get("error"); providerErr != "" {
		s.logf("github sign-in refused: the provider declined (%s, %s)", client, who)
		s.renderSignIn(w, http.StatusBadRequest, oauthIncomplete)
		return
	}
	code := query.Get("code")
	if code == "" {
		s.logf("github sign-in refused: the callback carried no authorization code (%s, %s)", client, who)
		s.renderSignIn(w, http.StatusBadRequest, oauthIncomplete)
		return
	}

	principal, err := s.oauth.Exchange(r.Context(), code, verifier)
	if err != nil {
		// 🔴 THE SAME SENTENCE THE TOKEN FORM GIVES, FOR EVERY REASON THE EXCHANGE CAN
		// FAIL. "the provider refused that code", "that token does not verify" and "no user
		// here matches that subject" are three different facts about the control plane, and
		// the third one is the one that confirms a guess. The REASON goes to the operator's
		// log, which is where the pod puts its own verdicts.
		s.logf("github sign-in refused: %v (%s, %s)", err, client, who)
		s.renderSignIn(w, http.StatusUnauthorized, signInRefused)
		return
	}

	// 🔴 THE SAME MINT THE TOKEN FORM USES, NOT A SECOND ONE. `openSession` is where the
	// fixation guard, the session record and the cookie live; a copy of it here would be a
	// second place to forget the revoke-before-mint ordering, and the copy that forgot would
	// be the one on the newer door.
	s.openSession(w, r, principal, "the "+GitHubLabel+" provider")
}

// refuseUnconfiguredOAuth is what a deployment that has not configured the provider answers
// on the two OAuth rows.
//
// 🔴 THE ROWS ARE IN THE LEDGER UNCONDITIONALLY AND THIS IS WHY THAT IS NOT AN INERT ROW.
// `Config.Sharing`'s comment refuses a nil-means-disabled field on the grounds that it puts
// a row in the ledger whose handler is inert — "a row every guard walks and none measures".
// The answer here is the one `ShareView.ReadOnly` takes for the same shape: the state is a
// legitimate configuration rather than an error, so it is SAID, and it is MEASURED —
// `TestTheGitHubRowsAnswerAnHonestRefusalWhenTheProviderIsNotConfigured` drives both rows
// against a server with no provider and pins this status and this sentence. The alternative
// — making `Config.OAuth` required — would refuse to start every deployment that signs in
// with a credential token, which is the deployment that exists.
//
// ⚠ 501 RATHER THAN 404, AND THE DIFFERENCE IS HONESTY ABOUT WHICH FACT IS BEING REPORTED.
// A 404 would say the path is not a route, and it is one — it is in `DeclaredRoutes()`,
// which this repository publishes. 501 says the route exists and this deployment does not
// implement it, which is exactly the state.
func (s *Server) refuseUnconfiguredOAuth(w http.ResponseWriter) {
	s.logf("github sign-in refused: no provider is configured on this deployment")
	s.renderSignIn(w, http.StatusNotImplemented, oauthUnavailable)
}

// providerArmed answers whether the GitHub door can work RIGHT NOW. It is asked per request
// and per render, never once at construction.
//
// 🔴 THE SECOND HALF — READINESS — EXISTS BECAUSE THE ALTERNATIVE WAS A STARTUP FATALITY THAT
// DELETED THE PROPERTY THE CREDENTIAL FORM EXISTS TO PROVIDE. `cmd/cairn-ui` used to `os.Exit`
// when the first JWKS fetch failed, mirroring `cmd/cairn-server`. The pod can afford that: it
// has one way in, so refusing to start and refusing every request are the same outcome. THIS
// SURFACE HAS TWO DOORS, and the whole stated reason the credential form is kept is that it
// works when the identity provider does not. A GoTrue that is restarting while this pod is
// rescheduled would have produced CrashLoopBackOff — the entries page, the share flow, the
// credential form and every already-issued cookie session all unservable, because a door
// NOBODY WAS USING could not reach its key set. Two places in this tree asserted the opposite
// in as many words, and they were right about later fetches and wrong about the first.
//
// 🔴 IT IS STILL FAIL-CLOSED FOR AUTHENTICATION, WHICH IS THE PART THAT MATTERS. Without keys
// `Verify` can verify nothing, so no Supabase token authenticates — the backend stays in the
// chain and refuses. What is withheld is the BUTTON, not a check: this predicate can only make
// the surface offer LESS.
//
// ⚠ AND IT RE-ARMS WITHOUT A RESTART, WHICH IS WHY IT IS A PREDICATE AND NOT A BOOLEAN READ
// ONCE. `cmd/cairn-ui`'s refresh loop keeps trying; the moment a fetch succeeds,
// `KeySetStatus.Fetched` flips and the next render carries the button. A value sampled at
// construction would have left the door shut until somebody noticed and restarted the pod.
func (s *Server) providerArmed() bool {
	if s.oauth == nil {
		return false
	}
	// nil means "no readiness signal was supplied", which is every unit test and any caller
	// that has no key set to wait on. It must mean ARMED: the alternative silently disables
	// the flow for every configuration that did not opt in.
	return s.oauthReady == nil || s.oauthReady()
}

// refuseUnreadyOAuth is what the two rows answer while the provider is configured but its key
// set has never been fetched.
//
// ⚠ 503 RATHER THAN 501, AND THE DIFFERENCE IS THE ONE A CALLER CAN ACT ON. 501 says this
// deployment does not implement the route; 503 says it does and cannot serve it yet. They are
// different facts and a person retrying is only right about one of them.
func (s *Server) refuseUnreadyOAuth(w http.ResponseWriter) {
	s.logf("github sign-in refused: the provider's key set has never been fetched, so no token " +
		"it issued could be verified — the credential form is unaffected")
	s.renderSignIn(w, http.StatusServiceUnavailable, oauthNotReady)
}
