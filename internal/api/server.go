// Package api is the HTTP layer over the store: one router, one 401, one audit
// line per API request.
//
// 🔴 IT REIMPLEMENTS NO RENDERING. The two report routes hand off to
// `internal/report`, which is the P1a/P1b seam; everything here is transport,
// authentication, authorization and the write verbs.
//
// 🔴 THE FOUR-STATE RULE, WHICH IS THE WHOLE POINT. "Reached the store and there is
// genuinely nothing recorded" and "could not read the store at all" MUST NOT render
// alike:
//
//	reached the store, nothing recorded  -> 200, X-Store-Status: scope-empty
//	could not read the store at all      -> 503, X-Store-Status: store-unreachable
//
// A 200 is a claim that the store was read. Only the first of those can make it, and
// this layer's job is to not throw that distinction away by catching everything into
// one answer.
//
// 🔴 ONE 401 RESPONSE, BYTE-IDENTICAL FOR EVERY REJECTION — no token, a malformed
// header, a wrong token, an unhandled verb, a non-API path, an absent or duplicated
// client IP. An unauthenticated caller cannot use this endpoint to learn which scopes
// exist. An error that discriminates is an enumeration API.
//
// ⚠ WHAT THIS PORT DELIBERATELY DOES NOT CARRY OVER, because `net/http` already owns
// it: the oracle's entity-body drain. `BaseHTTPRequestHandler` never reads a request
// body, so an unread body stayed in the socket buffer and was parsed as THE NEXT
// REQUEST on a keep-alive connection — measured as CL.0 request smuggling, with a
// victim's `Authorization` header completing an attacker's partial request line.
// `net/http` frames every request itself and rejects malformed framing before a
// handler runs, so that class cannot arise here. What IS carried over is the set of
// framings the oracle REFUSES, because those are the deployed contract rather than
// the fix — see readBody.
package api

import (
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/ZacxDev/cairn/internal/authz"
	"github.com/ZacxDev/cairn/internal/netid"
	"github.com/ZacxDev/cairn/internal/report"
	"github.com/ZacxDev/cairn/internal/snapshot"
	"github.com/ZacxDev/cairn/internal/store"
	"github.com/ZacxDev/cairn/internal/write"
)

// Constants the conformance goldens pin LITERALLY. They are spelled here and spelled
// again by hand in the tests, because a test that imports the value it asserts on
// asserts `x == x`.
const (
	APIPrefix  = "/api/v1/"
	HealthPath = "/healthz"

	// 🔴 ONE body, for every rejection. Terse: no scope, no ref, no reason.
	unauthorizedBody = "unauthorized\n"
	healthBody       = "ok\n"

	// The health endpoint says NOTHING: no version, no scope count, no store
	// revision. It is unauthenticated, so anything it reveals is public.
	serverBanner = "subsystem-store"

	// How much of a request body this server will read. The oracle's cap, kept: it
	// bounds what an authenticated caller can make the process hold in memory per
	// in-flight request.
	maxBodyBytes = 1 << 20
)

// Server is the wired handler. Its token table is swapped atomically so a SIGHUP
// reload is one rebind of one immutable value: a reader either resolves the pointer
// before the swap and sees the whole old table, or after it and sees the whole new
// one. Mutating a shared slice in place would expose a window in which the table is
// neither, and an emptied one rejects a credential that is valid in BOTH files —
// which reads to the client as a spurious 401 during every reload.
type Server struct {
	StoreRoot      string
	TrustedProxies []netip.Prefix
	Limiter        *netid.RateLimiter
	Renderer       report.Renderer
	// Audit is the sink for the one line per API request. nil means stdout.
	Audit func(string)
	// Warn is the sink for a renderer's own warning line — today only the
	// "nothing could be read" sentence a `*-unreachable` report carries. nil means
	// stderr, unprefixed.
	//
	// 🔴 IT IS STDERR AND NOT THE AUDIT SINK, AND NOT `log.Printf`. The oracle's reader
	// writes this sentence to `sys.stderr` from inside the library, which is how the pod
	// log gets it; `Audit` is stdout and carries one machine-readable record per request,
	// so putting prose in it would corrupt a stream whose record boundaries are the whole
	// point. `log.Printf` would prepend a timestamp the oracle does not write.
	Warn func(string)
	// Now is the clock the append date comes from. nil means time.Now.
	Now func() time.Time

	tokens atomic.Pointer[[]authz.TokenRecord]

	readRoutes  map[string]readRoute
	writeRoutes map[writeKey]writeRoute
}

// New wires a server. `tokens` must be non-empty and `trustedProxies` must be
// non-empty.
//
// 🔴 `trustedProxies` HAS NO DEFAULT AND AN EXPLICITLY EMPTY ONE IS REFUSED. A caller
// that forgot it fails here rather than getting a server whose client identity is
// silently wrong for a reason nobody can see.
//
// ⚠ AND THE REASON IS NOT "every request would be a 401". That was true of an earlier
// refuse-the-peer design and is false now: with no trusted peers every request is
// SERVED and bucketed under the gateway's own address, so one client's failures can
// lock out the others and nothing looks broken.
func New(storeRoot string, tokens []authz.TokenRecord, trustedProxies []netip.Prefix, limiter *netid.RateLimiter) (*Server, error) {
	if len(trustedProxies) == 0 {
		return nil, fmt.Errorf(
			"trusted_proxies is empty: no peer could ever be trusted, so no %s would ever be believed and every caller would be bucketed under the proxy's own address. Set $%s",
			netid.ClientIPHeader, netid.EnvTrustedProxies)
	}
	if len(tokens) == 0 {
		return nil, errors.New("tokens is empty: the API is not served without a credential")
	}
	s := &Server{
		StoreRoot:      storeRoot,
		TrustedProxies: trustedProxies,
		Limiter:        limiter,
		Renderer:       report.Reader{},
	}
	s.SetTokens(tokens)
	s.readRoutes = map[string]readRoute{
		"recall":   {arity: 2, handler: s.recall},
		"search":   {arity: 2, handler: s.search},
		"snapshot": {arity: 1, handler: s.snapshot},
	}
	s.writeRoutes = map[writeKey]writeRoute{
		{"POST", "entry"}: {arity: 4, tail: []string{"bullets"}, handler: s.appendBullet},
		{"PUT", "entry"}:  {arity: 3, handler: s.putEntry},
	}
	// 🔴 THE WIRING IS CHECKED AGAINST THE LEDGER AT CONSTRUCTION, because a table
	// built in code and a ledger declared beside it are two spellings, and the one
	// that goes stale is the ledger — which then reports full coverage over a set
	// that has grown. This refuses to start instead.
	if err := s.checkLedger(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Server) checkLedger() error {
	wired := map[string]bool{}
	for head := range s.readRoutes {
		for _, method := range safeReadMethods {
			wired[method+" "+head] = true
		}
	}
	for key := range s.writeRoutes {
		wired[key.method+" "+key.head] = true
	}
	declared := DeclaredRoutes()
	for _, name := range declared {
		if !wired[name] {
			return fmt.Errorf("route ledger names %q, which nothing dispatches", name)
		}
		delete(wired, name)
	}
	for name := range wired {
		return fmt.Errorf("route %q is dispatched but is not in the ledger: adding a row is adding a public, internet-reachable endpoint, and the ledger is where somebody has to think about it", name)
	}
	return nil
}

// SetTokens swaps the token table. ONE rebind of ONE immutable value.
func (s *Server) SetTokens(tokens []authz.TokenRecord) {
	frozen := make([]authz.TokenRecord, len(tokens))
	copy(frozen, tokens)
	s.tokens.Store(&frozen)
}

// Tokens is the table currently authorising requests.
func (s *Server) Tokens() []authz.TokenRecord {
	if p := s.tokens.Load(); p != nil {
		return *p
	}
	return nil
}

func (s *Server) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// request is the per-request state. It is a VALUE PER REQUEST rather than fields on
// the server, which removes by construction the class of bug the oracle needs four
// explicit resets to avoid: a keep-alive connection carrying the previous request's
// fingerprint, path or allowlist into this request's audit line.
//
// 🔴 `visible` DEFAULTS TO THE EMPTY SET, NEVER TO UNRESTRICTED, AND THE ZERO VALUE
// IS WHAT MAKES THAT TRUE. `store.ScopeSet{}` is "nothing is visible"; unrestricted
// is reachable only by calling `store.Unrestricted()`, which only a legacy token
// record does. A route reached without a successful authorization — today impossible,
// tomorrow one refactor away — therefore sees nothing rather than everything.
type request struct {
	srv       *Server
	w         http.ResponseWriter
	r         *http.Request
	path      string
	clientIP  string
	tokenFP   string
	identity  string
	peerState string
	visible   store.ScopeSet
	responded bool

	// matchedRecord is the credential this request authenticated with.
	//
	// 🔴 ONE MATCH, THREE FACTS. The fingerprint, the identity and the scope
	// allowlist all come off THIS record, so no route can be authenticated against
	// one credential and authorised against another. The write path reads
	// `IsLegacy()` off it rather than re-deriving "was that a bare row" from the
	// identity string, which would be a second spelling of the same question.
	matchedRecord authz.TokenRecord
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	rq := &request{srv: s, w: w, r: r, peerState: "-"}
	// 🔴 A BACKSTOP AROUND THE WHOLE DISPATCH, BECAUSE A METERED REQUEST MUST NEVER
	// VANISH FROM THE AUDIT TRAIL. On the oracle an unhandled exception dropped the
	// connection with no response, no status header and NO AUDIT LINE, on a request
	// that had already been metered and authenticated — twice, in two different
	// shapes. `net/http` would answer nothing at all and log a stack trace.
	//
	// 🔴 AND IT MUST NOT SEND A SECOND RESPONSE. The first version of the oracle's
	// backstop answered 500 unconditionally, so a route that had already answered 200
	// put a complete 200 AND a complete 500 on one socket — a desync a pooling proxy
	// hands to the NEXT client on the connection. So: ask whether bytes have gone out,
	// and if they have, close the connection and say so in the log only.
	defer func() {
		if recovered := recover(); recovered != nil {
			rq.backstop(recovered)
		}
	}()

	switch r.Method {
	case http.MethodGet, http.MethodHead:
		rq.handleRead()
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		rq.handleWrite()
	default:
		// 🔴 EVERY UNHANDLED METHOD ANSWERS THE ONE UNIFORM 401, NOT A 501 PAGE THAT
		// ECHOES THE VERB — and it is METERED like a bad token. The oracle reached
		// its framework's default error page here: a fourth distinct shape for what
		// its own docstring called "ONE 401, byte-identical for every rejection",
		// rendered pre-auth, unmetered and unaudited, so thirty invented-verb
		// requests wrote thirty audit lines and counted for nothing.
		rq.handleUnknownMethod()
	}
}

func (rq *request) handleUnknownMethod() {
	rq.path = rq.r.URL.Path
	rq.drainBody()
	if !rq.identifyAndMeter() {
		return
	}
	rq.refuse(rq.countFailure())
}

func (rq *request) handleRead() {
	rq.path = rq.r.URL.Path
	rq.drainBody()

	// 🔴 BEFORE EVERYTHING, and it is the ONLY thing before auth. Unauthenticated by
	// design, it says nothing but "ok", and it must not be rate-limited or require a
	// client-IP header the kubelet has no reason to send. A readiness probe broken by
	// a security guard is how the guard gets deleted.
	if rq.path == HealthPath {
		rq.respond(200, []byte(healthBody), "text/plain; charset=utf-8", nil)
		return
	}

	if !rq.identifyAndMeter() {
		return
	}
	if !strings.HasPrefix(rq.path, APIPrefix) {
		// Not an API path and not health. Answered with the SAME uniform 401 as a bad
		// token: a 404 here would let an unauthenticated caller map the URL space.
		//
		// 🔴 BUT IT IS NOT COUNTED, AND THAT IS A CORRECTION RATHER THAN AN
		// OVERSIGHT. It used to be, on the reasoning that URL probing is the same
		// attack as token probing. Combined with a success no longer forgiving a
		// streak, that measured as a legitimate client HOLDING THE RIGHT TOKEN
		// locking itself out for fifteen minutes by requesting five ordinary wrong
		// paths, one of them a missing trailing slash away from the real prefix. The
		// specification says five failed AUTHS per minute, and a request to a path
		// that never reaches the token check is not a failed auth.
		rq.refuse("unauthorized")
		return
	}
	if !rq.authenticate() {
		return
	}
	if rq.srv.Limiter != nil {
		rq.srv.Limiter.RecordSuccess(rq.clientIP)
	}

	parts := pathComponents(rq.path)
	if !rq.checkPathComponents(parts) {
		return
	}
	if len(parts) > 0 {
		if route, known := rq.srv.readRoutes[parts[0]]; known && len(parts) == route.arity {
			// 🔴 THE HANDLER'S ARGUMENT SLICE COMES FROM THE TABLE'S ARITY, NOT FROM
			// `len(parts)`. The oracle sliced with the request's own length, which is
			// attacker-controlled, and was correct only because the arity check
			// happened to reject a mismatch first — proven load-bearing by relaxing
			// that check, which passed five arguments to a four-parameter method and
			// dropped the connection with no response and no audit line.
			// 🔴 `parseQuery`, NOT `r.URL.Query()`. The framework's parser DROPS a
			// pair whose escape it cannot read, which turns a typo into a silent
			// default — see query.go for the measurement.
			rq.finish(route.handler(rq, parts[1:route.arity], parseQuery(rq.r.URL.RawQuery)))
			return
		}
	}
	rq.audit(404, "no-route")
	rq.respond(404, []byte("no such endpoint\n"), "text/plain; charset=utf-8",
		map[string]string{"X-Store-Status": "no-route"})
}

func (rq *request) handleWrite() {
	rq.path = rq.r.URL.Path
	// 🔴 THE ROUTE IS LOOKED UP BEFORE THE BODY IS READ, AND THAT ORDER IS THE POINT.
	// The lookup is a pure function of the method and the path — both known here — so
	// nothing is lost by asking early, and what is gained is that only a request with
	// somewhere to PUT a body retains one. Keeping every body unconditionally held up
	// to the cap for callers that could never reach a handler.
	key := writeKey{rq.r.Method, ""}
	parts := pathComponents(rq.path)
	var route writeRoute
	haveRoute := false
	if len(parts) > 0 {
		key.head = parts[0]
		if candidate, known := rq.srv.writeRoutes[key]; known && len(parts) == candidate.arity {
			if len(candidate.tail) == 0 || matchesTail(parts, candidate) {
				route, haveRoute = candidate, true
			}
		}
	}

	var body []byte
	framed := true
	if haveRoute {
		body, framed = rq.readBody()
	} else {
		rq.drainBody()
	}

	if !rq.identifyAndMeter() {
		return
	}
	// 🔴 AUTHENTICATE BEFORE ANSWERING ANYTHING. Otherwise an anonymous POST flood is
	// free: identified, not locked out, and never counted — 405 after 405 with nothing
	// charged. A write attempt with no valid credential is not a "wrong method", it is
	// an unauthorised request, and it is answered and charged as one.
	if !rq.authenticate() {
		return
	}

	if haveRoute {
		record := rq.matchedRecord
		if record.IsLegacy() {
			// 🔴 A LEGACY (BARE, UNMAPPED) TOKEN MAY NOT WRITE, AND THIS IS THE
			// DELIBERATE COST OF THE MIGRATION RATHER THAN AN OVERSIGHT. Every
			// appended bullet records an ACTOR and a SESSION; a bare row's identity is
			// the constant `legacy`, which names no holder, so there is no actor to
			// derive and the guarantee cannot be met. Attributing to `legacy` would
			// put a word in the store that reads like a person and is not one. READS
			// from a legacy token are unchanged.
			//
			// It is answered to an AUTHENTICATED caller, so it may say what it did
			// wrong; it discriminates nothing about the store, because a legacy row is
			// UNRESTRICTED and this answer is the same for every scope, existing or
			// not.
			//
			// 🔴 ORDER: ROUTE FIRST, THEN THIS REFUSAL. The refusal is about the
			// OPERATION, not about the verb — refusing before the route lookup would
			// answer 403 to a legacy caller's `POST /api/v1/snapshot`, which is a
			// wrong-method.
			rq.audit(403, "legacy-cannot-write")
			rq.respond(403, []byte(
				"forbidden: this credential has no identity, so a bullet written with it "+
					"could not record an actor. Give the holder a `<token> <identity> <scopes>` row\n"),
				"text/plain; charset=utf-8",
				map[string]string{"X-Store-Status": "legacy-cannot-write"})
			return
		}
		if !rq.checkPathComponents(parts) {
			return
		}
		if !framed {
			// The framing was refused. The caller is told, and NOTHING is written from
			// bytes this server could not frame.
			rq.audit(400, "bad-request")
			rq.respond(400, []byte("bad request: unreadable request body\n"),
				"text/plain; charset=utf-8",
				map[string]string{"X-Store-Status": "bad-request"})
			return
		}
		middle := parts[1 : route.arity-len(route.tail)]
		rq.finish(route.handler(rq, middle, body))
		return
	}

	// 🔴 THE UNCHANGED TAIL. `read-only` is a claim about THIS ROUTE, not about the
	// server: the report routes and `/snapshot` have no write verb and never will, and
	// that is what this answers.
	rq.audit(405, "method-not-allowed")
	rq.respond(405, []byte("read-only\n"), "text/plain; charset=utf-8",
		map[string]string{"Allow": "GET, HEAD"})
}

func matchesTail(parts []string, route writeRoute) bool {
	offset := route.arity - len(route.tail)
	for i, want := range route.tail {
		if parts[offset+i] != want {
			return false
		}
	}
	return true
}

// checkPathComponents refuses a component that cannot reach the filesystem safely.
//
// 🔴 A PATH COMPONENT REACHES THE FILESYSTEM. `%2e%2e` decodes to `..`, and the
// oracle's scope-revision read then opened `<store>/../.git/HEAD` and put the result
// in a response header. Harmless in the pod (the store root's parent holds nothing)
// and authenticated-only — but "harmless because of where it happens to be mounted"
// is not a property to rely on. The caller is authenticated, so it may be told what it
// did wrong.
func (rq *request) checkPathComponents(parts []string) bool {
	for _, part := range parts {
		if !authz.SafePathComponent.MatchString(part) {
			rq.audit(400, "bad-request")
			rq.respond(400, []byte("bad request: invalid path component\n"),
				"text/plain; charset=utf-8",
				map[string]string{"X-Store-Status": "bad-request"})
			return false
		}
	}
	return true
}

// --- authentication and metering ------------------------------------------------

func (rq *request) authenticate() bool {
	record, err := authz.Authorize(rq.r.Header.Get("Authorization"), rq.srv.Tokens())
	if err != nil {
		rq.refuse(rq.countFailure())
		return false
	}
	rq.matchedRecord = record
	rq.tokenFP = record.Fingerprint()
	rq.identity = record.Identity
	rq.visible = record.VisibleScopes()
	return true
}

// identifyAndMeter establishes the client and charges the lockout. False means the
// request has already been answered.
//
// Shared by every entry point, because a rule enforced at one call site and not the
// other is the failure this design keeps finding: the oracle's write path used to skip
// both checks entirely.
func (rq *request) identifyAndMeter() bool {
	ip, trusted, ok := netid.ResolveClient(rq.r.Header, rq.r.RemoteAddr, rq.srv.TrustedProxies)
	if trusted {
		rq.peerState = "trusted"
	} else {
		rq.peerState = "untrusted"
	}
	if !ok {
		// 🔴 FAIL CLOSED. The alternative — bucketing every unidentified request under
		// one shared key — is the failure the whole client-IP design exists to avoid:
		// one abuser would then lock out everybody. NOTHING is counted here, precisely
		// because there is no bucket to count into.
		rq.refuse("no-client-ip")
		return false
	}
	rq.clientIP = ip
	if limiter := rq.srv.Limiter; limiter != nil && limiter.LockedOut(ip) {
		// Checked BEFORE the token: a lockout a valid credential could walk through
		// would not be a lockout, and an attacker who guesses one token mid-run must
		// not have the record wiped.
		rq.refuse("locked-out")
		return false
	}
	return true
}

func (rq *request) countFailure() string {
	if rq.srv.Limiter == nil {
		return "unauthorized"
	}
	if rq.srv.Limiter.RecordFailure(rq.clientIP) {
		return "lockout-triggered"
	}
	return "unauthorized"
}

// refuse is the uniform 401 plus the audit line that says which reason it was.
//
// 🔴 THE WIRE DOES NOT DISCRIMINATE; THE LOG DOES. Every rejection — no client IP,
// locked out, not an API path, bad token, an unhandled verb — is the same code, the
// same body and the same header set, because an error that discriminates is an
// enumeration API. The `status=` field exists so the operator can still tell a
// credential-stuffing run from a misconfigured client, in a place the attacker cannot
// read.
func (rq *request) refuse(status string) {
	rq.audit(401, status)
	rq.respond(401, []byte(unauthorizedBody), "text/plain; charset=utf-8",
		map[string]string{"WWW-Authenticate": `Bearer realm="subsystem-store"`})
}

// --- responding ------------------------------------------------------------------

func (rq *request) respond(code int, body []byte, contentType string, headers map[string]string) {
	// 🔴 SET FIRST, BEFORE A SINGLE BYTE GOES OUT. Once the status line has been
	// written a SECOND complete response on the same connection is a desync, and a
	// partial write counts too: whatever the peer has already read cannot be unsaid.
	rq.responded = true
	h := rq.w.Header()
	// 🔴 THE BANNER IS A CONSTANT AND CARRIES NO VERSION. `net/http` sends no `Server`
	// header at all unless one is set, and the oracle's framework sends
	// `BaseHTTP/x.y Python/3.n` unless it is suppressed — so on both sides this line is
	// what stops an unauthenticated request reading the runtime version off the wire,
	// and it is what the goldens pin.
	setHeader(h, "Server", serverBanner)
	setHeader(h, "Content-Type", contentType)
	setHeader(h, "Content-Length", strconv.Itoa(len(body)))
	setHeader(h, "Cache-Control", "no-store")
	if code != 200 {
		// ⚠ READ AS "NOT 200", NOT AS "REJECTED": the create route answers 201, so a
		// SUCCESS takes this branch too and closes its connection. That is a wasted
		// handshake on a verb that runs once per entry, not a defect — and widening it
		// to `2xx` was declined because the value of this line is that it is
		// unconditional. The rule it enforces is stated for the codes it was written
		// for: 🔴 A REJECTED REQUEST NEVER KEEPS ITS CONNECTION. If framing was ever
		// mis-read the socket is already untrustworthy, and reusing it is the smuggling
		// primitive itself.
		//
		// Setting the header is also what tells the PEER: a pooling proxy that only saw
		// the socket close keeps its pool entry and discovers the close on its next use.
		setHeader(h, "Connection", "close")
	}
	for key, value := range headers {
		setHeader(h, key, value)
	}
	rq.w.WriteHeader(code)
	if rq.r.Method != http.MethodHead {
		_, _ = rq.w.Write(body)
	}
}

// setHeader writes a header under the EXACT NAME GIVEN, bypassing Go's
// canonicalisation.
//
// 🔴 TWO HEADERS IN THIS CONTRACT ARE NOT IN GO'S CANONICAL FORM, AND MEASURED THE
// SUITE SEES IT. `Header.Set` rewrites a name to `Title-Case-Per-Hyphen`, which turns
// `WWW-Authenticate` into `Www-Authenticate` and `ETag` into `Etag`. HTTP/1.1 field
// names are case-INSENSITIVE, so no correct client can tell — but the conformance
// corpus records the names a response actually carried, and it does so deliberately:
// header ORDER is declared unasserted (`wire.NOT_ASSERTED`) while the names themselves
// are compared literally, so a port that silently re-spelled them would be a port
// nobody could compare. Measured before this function existed: fourteen 401 cases and
// six write cases failed on nothing but those two spellings.
//
// 🔴 AND IT IS USED FOR **EVERY** HEADER, NOT JUST THE TWO. A helper applied only
// where it is currently needed is a helper the next non-canonical header skips —
// exactly the shape of a rule enforced at one call site and not the other. Spelling
// every name here also means the spellings this server emits are readable in one
// place. The framework's own special handling (`Content-Length`, `Connection`,
// `Content-Type`) is keyed on the CANONICAL name, and every name passed here that it
// cares about is already spelled canonically — so writing them verbatim changes what
// goes on the wire and not how the framework frames the response.
func setHeader(h http.Header, name, value string) {
	h[name] = []string{value}
}

// finish turns a handler's error into the response it means.
//
// 🔴 THE MAPPING IS ONE PLACE, because the four-state rule lives in it. "I could not
// look" is a 503 and "it is not there" is a 404 or a 200 with a named status, and a
// handler that decided this for itself would be free to conflate them.
func (rq *request) finish(err error) {
	if err == nil {
		return
	}
	var badReq *badRequestError
	var storeMissing *store.StoreMissingError
	var unreadable *store.EntryUnreadableError
	var revisionUnreadable *store.RevisionUnreadableError
	switch {
	case errors.As(err, &badReq):
		// A caller error, and the caller is authenticated, so it may be told what it
		// did wrong.
		rq.audit(400, "bad-request")
		rq.respond(400, []byte("bad request: "+badReq.message+"\n"), "text/plain; charset=utf-8",
			map[string]string{"X-Store-Status": "bad-request"})
	case errors.As(err, &storeMissing):
		rq.storeUnreachable(storeMissing.Error() + "\n")
	case errors.As(err, &unreadable):
		// 🔴 THE STATE THIS WHOLE DESIGN EXISTS TO KEEP SEPARATE. The store was NOT
		// read. Not a 200, not an empty digest, not "nothing recorded yet" — a 503 that
		// says so, carrying the reader's own sentence.
		rq.storeUnreachable(unreadable.Error() + "\n")
	case errors.As(err, &revisionUnreadable):
		// 🔴 A `.git/HEAD` THAT IS NOT VALID UTF-8 IS A CALLER-VISIBLE 400 AND NOT A 500,
		// because on the oracle the strict decode raises a `ValueError` and the dispatch's
		// `except ValueError` arm owns it. The caller is authenticated, so it may be told;
		// the sentence is the codec's own. See store.ScopeRevision for the one measured
		// difference this cannot reproduce (an extra 200 audit line there).
		rq.audit(400, "bad-request")
		rq.respond(400, []byte("bad request: "+revisionUnreadable.Error()+"\n"),
			"text/plain; charset=utf-8",
			map[string]string{"X-Store-Status": "bad-request"})
	default:
		rq.internalError(err)
	}
}

func (rq *request) storeUnreachable(body string) {
	rq.audit(503, "store-unreachable")
	rq.respond(503, []byte(body), "text/plain; charset=utf-8",
		map[string]string{"X-Store-Status": "store-unreachable", "X-Store-Exit": "3"})
}

func (rq *request) internalError(cause any) {
	// The BODY IS A CONSTANT and carries no error text. This is internet-reachable; an
	// error string names paths, values and types, and would be a new leak channel
	// opened by the very guard meant to close one. The detail goes to the process log
	// only.
	log.Printf("cairn: internal error on %s %s: %v", rq.r.Method, rq.path, cause)
	if rq.responded {
		return
	}
	rq.audit(500, "internal-error")
	rq.respond(500, []byte("internal error\n"), "text/plain; charset=utf-8",
		map[string]string{"X-Store-Status": "internal-error"})
}

func (rq *request) backstop(recovered any) {
	log.Printf("cairn: panic on %s %s: %v", rq.r.Method, rq.path, recovered)
	if rq.responded {
		// Nothing more may be written. Do not reuse this connection either: the request
		// ended in an unknown state, which is the same reason a non-200 closes.
		rq.audit(500, "internal-error-after-response")
		return
	}
	rq.audit(500, "internal-error")
	rq.respond(500, []byte("internal error\n"), "text/plain; charset=utf-8",
		map[string]string{"X-Store-Status": "internal-error"})
}

// badRequestError is a caller error whose sentence rides the response.
type badRequestError struct{ message string }

func (e *badRequestError) Error() string { return "bad request: " + e.message }

func badRequest(format string, args ...any) error {
	return &badRequestError{message: fmt.Sprintf(format, args...)}
}

// --- request bodies ---------------------------------------------------------------

// readBody reads the entity body a write handler will consume, and reports whether
// the framing was one this server accepts.
//
// 🔴 THE REFUSED FRAMINGS ARE THE DEPLOYED CONTRACT, NOT THE FIX, AND THAT
// DISTINCTION IS WHY THEY ARE STILL HERE. On the oracle every one of them was a
// measured smuggling or thread-pinning attack: `Transfer-Encoding: chunked` left the
// body queued to be parsed as the next request, a DUPLICATED `Content-Length` let a
// `0` hide a `154`, a NEGATIVE one told two different stories to two parsers, and an
// unbounded read let a caller drip one byte at a time and hold a worker forever.
// `net/http` makes all four impossible before a handler runs — it frames the request
// itself, rejects a duplicated or malformed length with its own 400, and enforces read
// deadlines. So these checks are no longer the guard they were.
//
// They are kept because a client that works against the deployed pod must work against
// this one: a chunked POST is a 400 there, and answering 200 here would be a silent
// widening discovered by whoever first sent one. ⚠ THE SUITE CANNOT SEE THIS — its
// runner always sends `Content-Length`, and `tests/conformance/README.md` names chunked
// framing under what it cannot observe — so this paragraph is the only place the
// decision is recorded.
//
// 🔴 A REFUSAL HERE RELIES ON THE NON-200 RULE TO CLOSE THE CONNECTION, NOT ON
// SETTING `Request.Close`. An earlier draft of this function set that field on every
// refusal, and it was a line that read as a guard and did nothing: `net/http` decides
// whether to reuse a connection while READING the request, before a handler runs, so
// an assignment from inside one is too late. Every path below answers a 400, and
// `respond` sets `Connection: close` for every non-200 — which is the lever that
// works, and the one the conformance goldens record.
//
// ⚠ TWO OF THESE CHECKS ARE UNREACHABLE THROUGH `net/http` TODAY, AND SAYING WHICH IS
// THE POINT: the framework rejects a request whose `Content-Length` headers DISAGREE
// with its own 400 before a handler exists, and collapses identical duplicates to one
// — so `len(values) > 1` cannot fire, and neither can a negative length, which the
// framework also refuses. They stay because they are the oracle's contract and because
// "the framework currently does this for me" is a property of a dependency rather than
// of this code; they are not counted as coverage of anything.
func (rq *request) readBody() ([]byte, bool) {
	if len(rq.r.TransferEncoding) > 0 {
		return nil, false
	}
	if values := rq.r.Header.Values("Content-Length"); len(values) > 1 {
		return nil, false
	}
	if rq.r.ContentLength < 0 {
		return nil, false
	}
	if rq.r.ContentLength > maxBodyBytes {
		return nil, false
	}
	if rq.r.ContentLength == 0 {
		return nil, true
	}
	body := make([]byte, rq.r.ContentLength)
	if _, err := io.ReadFull(rq.r.Body, body); err != nil {
		// A body shorter than its declared length is a framing this server will not
		// reason about, which is the same answer every other refusal above gets.
		return nil, false
	}
	return body, true
}

// drainBody discards a body no handler will read, bounded.
//
// It exists for the audit trail rather than for framing: `net/http` will drain a small
// body itself to reuse the connection, and every refusal here closes the connection
// anyway. Bounding it is what stops an unauthenticated caller making the process read
// a gigabyte before being told 401.
func (rq *request) drainBody() {
	if rq.r.Body == nil {
		return
	}
	// `CopyN` rather than a hand-rolled loop: one bound, one call, and no arithmetic
	// to get wrong. A body larger than the cap is left unread, and the refusal that
	// follows closes the connection through the non-200 rule — see readBody.
	_, _ = io.CopyN(io.Discard, rq.r.Body, maxBodyBytes)
}

// --- the audit line ---------------------------------------------------------------

var auditUnsafe = regexp.MustCompile(`[^\x20-\x7e]`)

// auditField makes a value safe to put in a whitespace-delimited log record.
//
// 🔴 THE AUDIT LINE IS A LOG-INJECTION SINK, AND IT IS REACHED BEFORE AUTH. The
// request path is percent-decoded, so `%0a` becomes a REAL NEWLINE — and this record
// is one formatted string with no escaping. An unauthenticated caller could therefore
// emit a second, syntactically perfect line of their choosing.
//
// That is not a cosmetic defect. This design's whole claim is that the `token=`
// fingerprint proves which credential a client used, and the rotation procedure says
// to delete the old token once its fingerprint stops appearing. A caller who can forge
// that line can keep any fingerprint alive forever (blocking rotation), fabricate an
// `auth=ok` from an address of their choice, and drown or forge the auth-failure alert.
//
// So every field passes through here: non-printable characters become `?`, spaces
// become `_` so a value cannot split into two fields, and the result is length-capped.
// Percent-encoding would be reversible, but reversibility is not what the log needs;
// unforgeable record boundaries are.
func auditField(value string, limit int) string {
	if len(value) > limit {
		value = value[:limit] + "...truncated"
	}
	return strings.ReplaceAll(auditUnsafe.ReplaceAllString(value, "?"), " ", "_")
}

// audit writes one line per API request.
//
// 🔴 CALL THIS BEFORE RESPONDING, NEVER AFTER — LOG THE DECISION, THEN ACT ON IT.
// Every call site on the oracle used to respond first, with two measured consequences:
// a sequential client's request N+1 could reach the sink before request N did, so the
// stream came out in the wrong ORDER; and anything raising between the two calls lost
// the record entirely. With the audit first, "the client holds response N" implies
// "line N is already in the sink".
//
// ⚠ AND THE PRICE, STATED BECAUSE IT IS A REAL BEHAVIOUR CHANGE: a sink that fails now
// does so before the response, so a request whose record cannot be written is answered
// 500 instead of being served with no record. On the write path that means a landed
// mutation can be reported as a 500. That is the fail-closed direction for a service
// whose reason to exist is a complete audit trail.
//
// `peer=` IS ITS OWN FIELD, AND THAT IS THE POINT. Reaching the pod without going
// through the gateway is worth detecting, but it is NOT an authentication failure and
// must not be spelled as one — an earlier version emitted it as
// `status=untrusted-peer` with `auth=fail`, which put every port-forward into the
// auth-failure alert and taught the operator to ignore it.
func (rq *request) audit(result int, status string) {
	ip := rq.clientIP
	if ip == "" {
		ip = "-"
	}
	fp := rq.tokenFP
	if fp == "" {
		fp = "-"
	}
	identity := rq.identity
	if identity == "" {
		identity = "-"
	}
	auth := "fail"
	if rq.tokenFP != "" {
		auth = "ok"
	}
	line := "store-api audit " +
		"ts=" + rq.srv.now().UTC().Format(time.RFC3339) + " " +
		"ip=" + auditField(ip, 256) + " " +
		"peer=" + rq.peerState + " " +
		"method=" + auditField(rq.r.Method, 16) + " " +
		"path=" + auditField(rq.path, 256) + " " +
		"token=" + auditField(fp, 16) + " " +
		"identity=" + auditField(identity, authz.MaxIdentityChars) + " " +
		"auth=" + auth + " " +
		"result=" + strconv.Itoa(result) + " status=" + auditField(status, 32)
	if rq.srv.Audit != nil {
		rq.srv.Audit(line)
		return
	}
	fmt.Fprintln(os.Stdout, line)
}

// --- handlers ---------------------------------------------------------------------

func (s *Server) recall(rq *request, parts []string, params url.Values) error {
	opts := report.RecallOptions{
		Scope: parts[0],
		Mode:  lastOr(params, "mode", report.DefaultMode),
		Limit: report.DefaultEntryLimit,
		Page:  1,
	}
	if raw, present := lastValue(params, "ref"); present {
		opts.Ref, opts.HasRef = raw, true
	}
	limit, err := intParam(params, "limit")
	if err != nil {
		return err
	}
	if limit != nil {
		opts.Limit = *limit
	}
	page, err := intParam(params, "page")
	if err != nil {
		return err
	}
	if page != nil {
		opts.Page = *page
	}
	if err := report.ValidateRecall(opts); err != nil {
		return &badRequestError{message: err.Error()}
	}
	rendered, err := s.Renderer.Recall(s.StoreRoot, opts, rq.visible)
	if err != nil {
		return err
	}
	return rq.serveReport(parts[0], rendered)
}

func (s *Server) search(rq *request, parts []string, params url.Values) error {
	query, present := lastValue(params, "q")
	if !present || strings.TrimSpace(query) == "" {
		// The server's own requirement, refused before the report's: there is no
		// default query, and a present-but-whitespace value is the shape a required
		// parameter is most often lost as.
		return badRequest("q is required and must be non-empty")
	}
	opts := report.SearchOptions{
		Scope:     parts[0],
		Query:     query,
		Context:   report.ContextBullet,
		Threshold: report.DefaultThreshold,
		MaxHits:   report.DefaultMaxHits,
	}
	contextParam, err := intParam(params, "context")
	if err != nil {
		return err
	}
	if contextParam != nil {
		opts.Context = *contextParam
	}
	maxHits, err := intParam(params, "max_hits")
	if err != nil {
		return err
	}
	if maxHits != nil {
		opts.MaxHits = *maxHits
	}
	threshold, err := floatParam(params, "threshold")
	if err != nil {
		return err
	}
	if threshold != nil {
		opts.Threshold = *threshold
	}
	// 🔴 `?all_scopes=1` NAMES NO SCOPE, so a per-scope refusal check has nothing to
	// refuse — it would search the CONTENT of every scope in the store. What makes it
	// safe is that the INDEX is narrowed, so "all scopes" means "all the CALLER'S
	// scopes".
	switch lastOr(params, "all_scopes", "0") {
	case "0", "", "false":
		opts.AllScopes = false
	default:
		opts.AllScopes = true
	}
	if err := report.ValidateSearch(opts); err != nil {
		return &badRequestError{message: err.Error()}
	}
	rendered, err := s.Renderer.Search(s.StoreRoot, opts, rq.visible)
	if err != nil {
		return err
	}
	return rq.serveReport(parts[0], rendered)
}

// serveReport is the ONE place a rendered report becomes a response, so neither the
// freshness stamp nor the scope revision can be forgotten by a future route.
//
// 🔴 THE STAMP GOES IN THE BODY, NOT ONLY THE HEADER, AND IT GOES FIRST. A header
// alone would not do: the measured failure was an AGENT reading the rendered text and
// believing its "none omitted" line, and an agent that pipes the body never sees a
// header. It precedes the report because a caveat printed after the thing it qualifies
// has already been believed.
//
// ⚠ `pathScope` IS THE URL's OWN SPELLING AND NOT THE REPORT'S NORMALIZED ONE. The revision
// is read from `<store>/<scope>/.git/HEAD`, so the answer has to be about the directory the
// caller's name reaches; every path component has already been refused unless it is a safe
// one. The report's own `Scope` field is normalized and is what the BODY prints.
func (rq *request) serveReport(pathScope string, rendered report.Rendered) error {
	// 🔴 COMPUTED BEFORE THE AUDIT LINE, because it can REFUSE. A revision read that fails
	// its strict decode is a 400, and a 200 audit record written first would claim an answer
	// this request never gave.
	revision, err := store.ScopeRevision(rq.srv.StoreRoot, pathScope, rq.visible)
	if err != nil {
		return err
	}
	if rendered.Warning != "" {
		// 🔴 FORWARDED, NOT DROPPED. This is the reader's own one-sentence summary of a
		// `*-unreachable` report, and the pod log is exactly where a
		// nothing-could-be-read reject should be visible. The per-entry detail is in the
		// body; this is the quotable line.
		rq.srv.warn(rendered.Warning)
	}
	freshHeader, freshProse := snapshot.Freshness(rq.srv.StoreRoot)
	body := freshProse + "\n\n" + rendered.Text + "\n"
	rq.audit(200, rendered.Status)
	rq.respond(200, []byte(body), "text/plain; charset=utf-8", map[string]string{
		"X-Store-Status": rendered.Status,
		"X-Store-Exit":   strconv.Itoa(rendered.Exit),
		// 🔴 GATED ON THE CALLER'S ALLOWLIST, INSIDE ScopeRevision. This one header does
		// NOT come from the narrowed index — it is read off `<store>/<scope>/.git/HEAD` —
		// so it is the one place a refused scope could still be told apart from an absent
		// one.
		"X-Store-Revision": revision,
		"X-Store-Snapshot": freshHeader,
	})
	return nil
}

func (s *Server) warn(line string) {
	if s.Warn != nil {
		s.Warn(line)
		return
	}
	fmt.Fprintln(os.Stderr, line)
}

func (s *Server) snapshot(rq *request, _ []string, params url.Values) error {
	scopeFilter, _ := lastValue(params, "scope")
	if scopeFilter != "" && !authz.SafePathComponent.MatchString(scopeFilter) {
		// The same refusal a bad path component gets: this value reaches the
		// filesystem, and the caller is authenticated so it may be told.
		rq.audit(400, "bad-request")
		rq.respond(400, []byte("bad request: invalid scope\n"), "text/plain; charset=utf-8",
			map[string]string{"X-Store-Status": "bad-request"})
		return nil
	}
	result, err := snapshot.Build(s.StoreRoot, scopeFilter, rq.visible)
	if err != nil {
		rq.storeUnreachable("store unreadable: " + err.Error() + "\n")
		return nil
	}
	if len(result.Unreadable) > 0 {
		// 🔴 AN UNREADABLE SCOPE IS NOT AN EMPTY ONE, AND A DIRECTORY LISTING CANNOT
		// TELL YOU WHICH IT SAW. The first version of the oracle's handler answered 200
		// with exit 0 for a mode-000 scope, the tar silently omitted it, and the client
		// rendered "nothing recorded". A partial snapshot served as 200 is worse than no
		// snapshot.
		body := "store unreadable:\n  " + strings.Join(result.Unreadable, "\n  ") + "\n"
		rq.storeUnreachable(body)
		return nil
	}
	freshHeader, _ := snapshot.Freshness(s.StoreRoot)
	rq.audit(200, "snapshot")
	rq.respond(200, result.Archive, "application/gzip", map[string]string{
		"X-Store-Status":   "snapshot",
		"X-Store-Exit":     "0",
		"X-Store-Snapshot": freshHeader,
		// The SERVER's count of what it put in. The client compares its own extracted
		// count against this and refuses a mismatch. ⚠ On a `?scope=` request the
		// counts still describe the same filtered set, so the check holds there too.
		"X-Store-Entries": strconv.Itoa(result.Entries),
	})
	return nil
}

// --- query parameters -------------------------------------------------------------

// lastValue is the LAST value of a repeated parameter, and whether it was present at
// all. The last-wins choice is a contract a port must reproduce, not a convenience:
// `?limit=1&limit=2` means 2.
func lastValue(params url.Values, name string) (string, bool) {
	values := params[name]
	if len(values) == 0 {
		return "", false
	}
	return values[len(values)-1], true
}

func lastOr(params url.Values, name, fallback string) string {
	if value, present := lastValue(params, name); present {
		return value
	}
	return fallback
}

// intParam parses an optional integer parameter.
//
// 🔴 IT REFUSES RATHER THAN SILENTLY DEFAULTING. A `?limit=abc` that quietly became
// the default is a caller believing a setting took effect.
func intParam(params url.Values, name string) (*int, error) {
	raw, present := lastValue(params, name)
	if !present {
		return nil, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return nil, badRequest("%s must be an integer, got %s", name, store.PyRepr(raw))
	}
	return &value, nil
}

func floatParam(params url.Values, name string) (*float64, error) {
	raw, present := lastValue(params, name)
	if !present {
		return nil, nil
	}
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return nil, badRequest("%s must be a number, got %s", name, store.PyRepr(raw))
	}
	return &value, nil
}

// --- write handlers ---------------------------------------------------------------

func (s *Server) appendBullet(rq *request, parts []string, body []byte) error {
	scope, ref := parts[0], parts[1]
	payload, err := write.DecodeBulletBody(body)
	if err != nil {
		return badRequest("body must be JSON (%s)", err.Error())
	}
	if problem := write.BulletRequestProblem(body, payload); problem != "" {
		return &badRequestError{message: problem}
	}
	entry, ok, err := s.resolveWritable(rq, scope, ref)
	if err != nil || !ok {
		return err
	}
	// 🔴 THE ACTOR IS THE AUTHENTICATED IDENTITY AND THE REQUEST BODY HAS NO WAY TO
	// REACH IT. An `actor` key in the JSON is accepted and DISCARDED — accepted because
	// a client library may send one and a 400 there would be a compatibility trap,
	// discarded because a client-supplied actor lets any token-holder attribute a
	// bullet to somebody else. The render function takes the actor as a parameter the
	// request cannot populate, so this is structural rather than a rule someone
	// remembers.
	status, line, revision, err := write.AppendBullet(
		s.entryPath(entry), write.BulletText(payload), rq.identity,
		write.BulletSession(payload), rq.srv.now().UTC().Format("2006-01-02"), nil)
	if err != nil {
		var shape *write.EntryShapeError
		if errors.As(err, &shape) {
			rq.audit(422, "entry-shape")
			rq.respond(422, []byte("unprocessable: "+shape.Error()+"\n"),
				"text/plain; charset=utf-8",
				map[string]string{"X-Store-Status": "entry-shape"})
			return nil
		}
		rq.storeUnreachable("store unreadable: " + err.Error() + "\n")
		return nil
	}
	rq.audit(200, status)
	rq.respond(200, []byte(line+"\n"), "text/plain; charset=utf-8", map[string]string{
		"X-Store-Status": status,
		"X-Cairn-Bullet": write.ContentHash(write.BulletText(payload)),
		"ETag":           `"` + revision + `"`,
	})
	return nil
}

func (s *Server) putEntry(rq *request, parts []string, body []byte) error {
	scope, ref := parts[0], parts[1]
	// 🔴 ONE ROUTE, TWO OPERATIONS, AND THE PRECONDITION HEADER IS WHAT CHOOSES.
	// `If-Match: <rev>` replaces an existing entry; `If-None-Match: *` creates one that
	// is absent. HTTP has no create verb, and inventing a second URL for the same noun
	// would mean two routes a caller has to choose between and two places every later
	// guard has to be added.
	rawMatch, haveMatch := soleHeader(rq.r.Header, "If-Match")
	rawNone, haveNone := soleHeader(rq.r.Header, "If-None-Match")
	if haveMatch && haveNone {
		// 🔴 BOTH HEADERS TOGETHER IS A CONTRADICTION AND IS REFUSED, NOT RESOLVED.
		// `If-Match: <rev>` says "it exists and is exactly this"; `If-None-Match: *`
		// says "it does not exist". Picking one would be guessing which of two mutually
		// exclusive intents the caller meant, on the verb that can destroy content.
		return &badRequestError{message: "If-Match and If-None-Match are contradictory on a " +
			"PUT - If-Match replaces an entry that exists, If-None-Match: * " +
			"creates one that does not. Send exactly one"}
	}
	if !haveMatch && !haveNone {
		// 🔴 A PRECONDITION IS REQUIRED. An optional precondition is no precondition,
		// and adding the create half must not turn "no precondition" into "create it
		// then".
		rq.audit(428, "precondition-required")
		rq.respond(428, []byte(
			"precondition required: send If-Match with the entry revision "+
				"(sha256 of the entry file, first 16 hex characters) to replace "+
				"an existing entry, or If-None-Match: * to create a new one\n"),
			"text/plain; charset=utf-8",
			map[string]string{"X-Store-Status": "precondition-required"})
		return nil
	}
	if haveNone {
		// 🔴 `*` AND NOTHING ELSE. RFC 9110 §13.1.2 also allows a LIST of entity-tags,
		// meaning "proceed only if the current representation matches none of these" —
		// which on a PUT is "overwrite unless it is one of the revisions I name", i.e. a
		// blind overwrite of every OTHER revision. That is the lost update `If-Match`
		// exists to refuse, spelled backwards.
		if tags := write.ParseIfMatch(rawNone); len(tags) != 1 || tags[0] != "*" {
			return &badRequestError{message: "only `If-None-Match: *` is supported - a list " +
				"of entity-tags there would mean 'overwrite unless it is one " +
				"of these', which is a blind overwrite of every other " +
				"revision. To replace a known revision send If-Match"}
		}
		return s.createEntry(rq, scope, ref, body)
	}
	ifMatch := write.ParseIfMatch(rawMatch)
	for _, tag := range ifMatch {
		if tag == "*" {
			// `If-Match: *` means "any current representation", which is the one value
			// that turns the guard off while looking like it is on — and it is NOT a
			// spelling of the create case: `*` on `If-Match` requires the entry to EXIST.
			return &badRequestError{message: "If-Match: * is refused - it matches any revision, " +
				"which is the same as sending no precondition at all. To create " +
				"an entry that does not exist yet, send If-None-Match: *"}
		}
	}
	if len(ifMatch) == 0 {
		// A header that is present but names NO entity-tag (`If-Match:` or
		// `If-Match: ,`) is not a precondition. Refusing it is the same answer `*`
		// gets, for the same reason: it would gate nothing.
		return &badRequestError{message: "If-Match names no entity-tag"}
	}
	return s.replaceEntry(rq, scope, ref, body, ifMatch)
}

func (s *Server) replaceEntry(rq *request, scope, ref string, body []byte, ifMatch []string) error {
	entry, ok, err := s.resolveWritable(rq, scope, ref)
	if err != nil || !ok {
		return err
	}
	revision, err := write.ReplaceEntry(
		s.entryPath(entry), body, ifMatch, entry.Scope, entry.Filename, nil)
	if err != nil {
		var precondition *write.PreconditionFailedError
		var shape *write.EntryShapeError
		var notUTF8 *write.BodyNotUTF8Error
		switch {
		case errors.As(err, &precondition):
			// 🔴 THE FILE IS UNCHANGED, and the CURRENT revision rides the response: a
			// client told only "no" cannot retry, and a client that cannot retry
			// re-sends without the precondition.
			rq.audit(412, "precondition-failed")
			rq.respond(412, []byte("precondition failed: the entry has changed since that revision\n"),
				"text/plain; charset=utf-8", map[string]string{
					"X-Store-Status": "precondition-failed",
					"ETag":           `"` + precondition.Current + `"`,
				})
		case errors.As(err, &shape):
			rq.unprocessable(shape.Error())
		case errors.As(err, &notUTF8):
			rq.unprocessable(notUTF8.Error())
		default:
			rq.storeUnreachable("store unreadable: " + err.Error() + "\n")
		}
		return nil
	}
	rq.audit(200, "replaced")
	rq.respond(200, []byte("replaced\n"), "text/plain; charset=utf-8", map[string]string{
		"X-Store-Status": "replaced",
		"ETag":           `"` + revision + `"`,
	})
	return nil
}

// createEntry is the `If-None-Match: *` half of PUT.
//
// 🔴 THE SCOPE CHECK IS EXPLICIT HERE AND CANNOT REUSE resolveWritable. That helper
// answers "which EXISTING entry does this address", and on a create there is none: its
// unknown-scope arm cannot tell a scope outside the caller's allowlist (404, and it
// must stay 404) from a scope the caller MAY write that simply has no directory yet,
// which is the genuine first-entry case this verb exists for. So the allowlist is
// consulted directly, through the same one place every other narrowing site uses.
//
// 🔴 THE FILENAME IS DERIVED FROM THE REF, NOT TAKEN FROM THE BODY. A caller could
// otherwise name one file in the URL and another in `service:`, and the loader would
// then serve an entry no ref reaches. The validator is handed exactly the filename this
// writes, so the loader's own "filename slug and `service:` must agree" check is what
// refuses the mismatch — one predicate, at the loader, as everywhere else.
//
// ⚠ ONLY A BARE `<slug>.md` CAN BE CREATED, and that is a consequence of the safe-path
// class rather than a decision made here: it excludes `.`, so a kind-qualified ref
// cannot reach any write route at all.
func (s *Server) createEntry(rq *request, scope, ref string, body []byte) error {
	if !rq.visible.Allows(scope) {
		// Indistinguishable from "that scope has never existed" — the same answer
		// resolveWritable gives, for the same reason.
		rq.notFound("scope-unknown")
		return nil
	}
	foldedScope := store.NormalizeRef(scope)
	foldedRef := store.NormalizeRef(ref)
	if foldedScope == "" || foldedRef == "" {
		// A component that is a valid path segment but normalizes away entirely
		// (`___`, `--`). It names no scope and no entry, and it is a caller error with a
		// defined remedy, so it is SAID rather than 404'd. The asymmetry with the append
		// route — where the same shape is a 404, because the allowlist answers first —
		// is real and is recorded rather than smoothed over.
		return &badRequestError{message: "the scope and the ref must each normalize to a non-empty slug"}
	}
	filename := foldedRef + ".md"
	index, err := store.LoadStore(s.StoreRoot, "written", rq.visible)
	if err != nil {
		// 🔴 THE STORE WAS NOT READ, so we do not know whether the ref is taken. "I
		// could not look" is never a 404 and never a create.
		return err
	}
	existing, _, resolveErr := store.ResolveRefTiered(ref, index, scope)
	var unknownScope *store.UnknownScopeError
	var ambiguous *store.AmbiguousRefError
	switch {
	case resolveErr == nil:
	case errors.As(resolveErr, &unknownScope):
		// 🔴 THE ONE PLACE THIS IS NOT AN ERROR. The allowlist already said yes above,
		// so an unknown scope here means the directory does not exist yet — a scope's
		// first entry, which is how the store gained every scope it has.
		existing = nil
	case errors.As(resolveErr, &ambiguous):
		return &badRequestError{message: ambiguous.Error()}
	default:
		return resolveErr
	}
	if existing != nil {
		// 🔴 RESOLVING TO AN ENTRY IS "IT EXISTS", EVEN THROUGH AN ALIAS.
		// `If-None-Match: *` is a question about the TARGET RESOURCE, and the resource
		// this path addresses is whatever a read of that ref returns. Creating
		// `<ref>.md` beside an entry that already answers `<ref>` would make the ref
		// AMBIGUOUS — the store's own 400 — for every later reader.
		rq.entryExists(filename, existing.Filename)
		return nil
	}
	target := filepath.Join(s.StoreRoot, foldedScope, filename)
	revision, err := write.CreateEntry(target, body, foldedScope, filename, nil)
	if err != nil {
		var exists *write.EntryExistsError
		var shape *write.EntryShapeError
		var notUTF8 *write.BodyNotUTF8Error
		switch {
		case errors.As(err, &exists):
			// The index did not know about it and the filesystem did: a file the loader
			// collected as MALFORMED, or a racing create that landed first. Either way
			// the name is taken and nothing was written.
			rq.entryExists(filename, "")
		case errors.As(err, &shape):
			rq.unprocessable(shape.Error())
		case errors.As(err, &notUTF8):
			rq.unprocessable(notUTF8.Error())
		default:
			rq.storeUnreachable("store unwritable: " + err.Error() + "\n")
		}
		return nil
	}
	// 🔴 201, NOT 200 — RFC 9110 §9.3.4 says a PUT that creates a representation MUST
	// tell the user agent so. It is also the only way a client can distinguish a create
	// that landed from a replace that did, since both carry an ETag.
	rq.audit(201, "created")
	rq.respond(201, []byte("created\n"), "text/plain; charset=utf-8", map[string]string{
		"X-Store-Status": "created",
		"ETag":           `"` + revision + `"`,
	})
	return nil
}

// entryExists is the `If-None-Match: *` refusal. 412, and a DISTINCT status token.
//
// 🔴 THE CODE IS THE SAME 412 AN `If-Match` MISS GETS AND THE REMEDY IS THE OPPOSITE,
// so the difference has to ride somewhere a client can read it. `X-Store-Status:
// already-exists` is that place: a caller told only "412" cannot tell "somebody moved
// it, re-sync and re-apply" from "it is already there, you wanted a replace", and one
// of those two is an infinite retry.
func (rq *request) entryExists(filename, resolved string) {
	detail := ""
	if resolved != "" {
		detail = " as `" + resolved + "`"
	}
	rq.audit(412, "already-exists")
	rq.respond(412, []byte(fmt.Sprintf(
		"precondition failed: `%s` already exists%s, and If-None-Match: * means create only if absent. Nothing was written; use PUT with If-Match to replace it, or POST .../bullets to append to it\n",
		filename, detail)), "text/plain; charset=utf-8",
		map[string]string{"X-Store-Status": "already-exists"})
}

func (rq *request) unprocessable(message string) {
	rq.audit(422, "entry-shape")
	rq.respond(422, []byte("unprocessable: "+message+"\n"), "text/plain; charset=utf-8",
		map[string]string{"X-Store-Status": "entry-shape"})
}

// notFound is 🔴 ONE 404 FOR EVERY WAY A WRITE TARGET CAN FAIL TO RESOLVE, and the
// uniformity is the enumeration property applied to writes.
//
// A scope OUTSIDE the caller's allowlist, a scope that has never existed, a ref that
// resolves to nothing and an entry the loader could not parse all answer these exact
// bytes with these exact headers. The read path closes this at the INDEX — a refused
// scope is simply not in it, so asking for it raises the same unknown-scope error a
// never-existed one raises — and the write path REUSES that narrowing rather than
// adding a second "is this scope yours" check with its own answer.
//
// The WIRE does not discriminate; the audit LOG does, in a place the caller cannot
// read.
func (rq *request) notFound(status string) {
	rq.audit(404, status)
	rq.respond(404, []byte("not found\n"), "text/plain; charset=utf-8",
		map[string]string{"X-Store-Status": "not-found"})
}

// resolveWritable is the entry a write targets. `ok` false means the request has
// already been answered.
//
// It loads through the SAME function both report routes use, with the SAME allowlist,
// so there is exactly one place that decides what a caller may see and it cannot come
// to disagree with itself.
func (s *Server) resolveWritable(rq *request, scope, ref string) (*store.Entry, bool, error) {
	index, err := store.LoadStore(s.StoreRoot, "written", rq.visible)
	if err != nil {
		// 🔴 THE STORE WAS NOT READ. Never a 404 — "I could not look" and "it is not
		// there" are the four-state rule's two states and a write must not conflate them
		// any more than a read may.
		return nil, false, err
	}
	entry, _, err := store.ResolveRefTiered(ref, index, scope)
	if err != nil {
		var unknownScope *store.UnknownScopeError
		var ambiguous *store.AmbiguousRefError
		switch {
		case errors.As(err, &unknownScope):
			// Refused OR absent — indistinguishable by construction.
			rq.notFound("scope-unknown")
			return nil, false, nil
		case errors.As(err, &ambiguous):
			// The caller is authenticated AND may see this scope, so it may be told: an
			// ambiguous ref is a caller error with a defined remedy, and it names only
			// entries this caller can already read.
			return nil, false, &badRequestError{message: ambiguous.Error()}
		}
		return nil, false, err
	}
	if entry == nil {
		// No such ref — and a MALFORMED entry lands here too, because the loader
		// collects it rather than listing it as an entry. That is the fail-closed
		// direction: this writer will not edit a file the reader cannot parse.
		rq.notFound("ref-unknown")
		return nil, false, nil
	}
	return entry, true, nil
}

// entryPath is located from the LOADER's own scope and filename, never rebuilt from
// the ref — `<slug>.<kind>.md` and `<slug>.md` are different files and only the loader
// knows which one this entry came from.
func (s *Server) entryPath(entry *store.Entry) string {
	return filepath.Join(s.StoreRoot, entry.Scope, entry.Filename)
}

// soleHeader is the value of a header iff it appears EXACTLY ONCE.
//
// 🔴 ONE RULE, ONE PLACE. This predicate existed twice on the oracle, open-coded, and
// was correct at one site and wrong at the other — which is the shape a duplicated
// predicate always takes. A bare "get the header" takes the FIRST value, and a working
// smuggle was built out of exactly that.
func soleHeader(h http.Header, name string) (string, bool) {
	values := h.Values(name)
	if len(values) != 1 {
		return "", false
	}
	return values[0], true
}
