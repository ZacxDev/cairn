package ui

import (
	"errors"
	"io"
	"net/http"
	"net/netip"
	"strings"
	"time"

	"github.com/ZacxDev/cairn/internal/control"
	"github.com/ZacxDev/cairn/internal/identity"
	"github.com/ZacxDev/cairn/internal/netid"
	"github.com/ZacxDev/cairn/internal/store"
)

// Source is the read half the one page needs, as an interface so the renderer's
// tests can build a world without a store on disk.
//
// 🔴 IT TAKES THE AUTHORIZATION, NOT THE PRINCIPAL. `control.Authorization` is the
// half of an `identity.Identity` that answers "what may this caller see", and it
// came out of the SAME model read that produced the principal. Handing the
// principal instead would make this interface's implementer resolve the authority
// a second time, which is the authenticated-against-one-world-authorised-against-
// another window `control.Principal`'s own comment forbids.
type Source interface {
	Visible(auth control.Authorization) ([]Scope, error)
}

// Scope is one scope's worth of entries, as the page renders them.
type Scope struct {
	// Name is the scope's display name. USER TEXT.
	Name string
	// Entries are its entries in index order. Every string below is USER TEXT.
	Entries []Entry
}

// Entry is one entry, reduced to what the page shows.
//
// 🔴 EVERY FIELD HERE IS ATTACKER-INFLUENCED, AND `Tasks` IS THE ONE THAT LANDS IN
// A URL POSITION. A store entry's `tasks:` front-matter key is `<system>:<id>`,
// which is the same shape as a URL scheme followed by an opaque part — so
// `javascript:alert(document.domain)` is a WELL-FORMED task ref. See [safeHref].
type Entry struct {
	Ref     string
	Title   string
	Aliases []string
	Tasks   []string
}

// StoreSource reads the real store, narrowed by the caller's authority.
type StoreSource struct{ Root string }

// Visible loads the index the caller may read and projects it to page shapes.
//
// 🔴 `VisibleScopes(control.VerbRead)` IS THE ONLY NARROWING, AND IT IS THE SAME
// SEAM THE POD USES. A second scope check here would be a second implementation of
// visibility, which `internal/control/README.md` exists to refuse.
func (s StoreSource) Visible(auth control.Authorization) ([]Scope, error) {
	visible := auth.VisibleScopes(control.VerbRead)
	index, err := store.LoadStore(s.Root, "recall", visible)
	if err != nil {
		return nil, err
	}
	var out []Scope
	for _, name := range index.Scopes() {
		entries, err := index.Entries(name)
		if err != nil {
			// An unknown scope cannot happen for a name the index just listed. It is
			// returned rather than skipped so a loader that starts disagreeing with
			// itself is loud instead of quietly rendering a short page.
			return nil, err
		}
		page := Scope{Name: name}
		for _, e := range entries {
			item := Entry{Ref: e.Ref(), Title: e.Slug, Aliases: e.RawAliases}
			for _, t := range e.Tasks {
				// `Raw`, not `String()`: the page shows the ref the FILE carries,
				// because the normalisation that produces `System` lowercases and
				// `-`-folds, and a reader comparing the page against the file would
				// otherwise see two spellings of one ref and not know which is real.
				item.Tasks = append(item.Tasks, t.Raw)
			}
			page.Entries = append(page.Entries, item)
		}
		out = append(out, page)
	}
	return out, nil
}

// Server is the UI's HTTP surface.
type Server struct {
	auth        identity.Authenticator
	credentials identity.TokenAuthority
	source      Source
	sharing     Sharing
	sessions    identity.SessionStore
	ttl         time.Duration
	now         func() time.Time
	log         io.Writer

	// oauth and flights are the provider sign-in, and they are one pair rather than two
	// settings for the reason the pair below is: a flight table with no provider to send
	// anybody to holds nothing, and a provider with nowhere to record a PKCE verifier
	// cannot complete a flow. `oauth` is nil on a deployment that has not configured one —
	// see `refuseUnconfiguredOAuth` for why that is SAID rather than refused at
	// construction — and `flights` is never nil, because a table nobody writes to costs an
	// empty map.
	oauth   OAuthAuthority
	flights *flights

	// trustedProxies and limiter are the client-identity pair, and they are one pair
	// rather than two settings: the limiter's key IS what `netid.ResolveClient` returns,
	// so a lockout without a trusted-proxy allowlist would bucket every request behind an
	// edge under that edge's own address — one shared key, and the first abuser locks
	// everybody out. `internal/netid`'s own comment calls that the failure the whole
	// client-IP design exists to avoid.
	trustedProxies []netip.Prefix
	limiter        *netid.RateLimiter
}

// Config is what [New] needs. A struct rather than seven positional parameters,
// because six of them are interfaces and a call site that transposed two would still
// compile.
type Config struct {
	// Auth is the chain every request is resolved against. See `AuthBackends`.
	Auth identity.Authenticator
	// Credentials resolves the bearer token a SIGN-IN FORM carries, and it is
	// deliberately a `TokenAuthority` rather than an `Authenticator`.
	//
	// 🔴 A STRING IN, A PRINCIPAL OUT, AND NO `*http.Request` ANYWHERE NEAR IT. The
	// sign-in exchange must resolve the credential the form carried and NOTHING the
	// request also happens to carry — most of all not the session cookie the browser
	// already holds. An `Authenticator` here would take the whole request, so the
	// cookie backend would be in scope and a form submitted with a wrong token but a
	// live cookie would "succeed" as the cookie's principal, minting a fresh session
	// for a credential that was refused. Taking a string makes that unrepresentable
	// rather than avoided by care.
	Credentials identity.TokenAuthority
	// OAuth is the PROVIDER sign-in the GitHub button drives, and it is the one field on
	// this struct that may legitimately be nil.
	//
	// 🔴 NIL MEANS "THIS DEPLOYMENT HAS NO PROVIDER", WHICH IS A CONFIGURATION AND NOT A
	// DEFECT — AND IT IS THE ONE PLACE THIS STRUCT DIVERGES FROM `Sharing`'s RULING, SO THE
	// DIVERGENCE IS ARGUED RATHER THAN ASSUMED. `Sharing` is required precisely because a
	// nil-means-disabled field would put a row in the ledger whose handler was inert. The
	// same objection lands here and is answered rather than ignored: the two OAuth rows
	// answer **501 with a sentence naming the configuration**, and
	// `TestTheGitHubRowsAnswerAnHonestRefusalWhenTheProviderIsNotConfigured` measures both
	// of them, so neither is an unmeasured row. What makes required impossible is the
	// deployment that exists: a surface whose only door is a credential token today would
	// refuse to start, so requiring this would turn a new feature into an outage.
	//
	// ⚠ AND THE BUTTON IS NOT RENDERED WHEN THIS IS NIL. A control that is present and
	// cannot work teaches a user that sign-in is unreliable — the same ruling [Page] makes
	// about the sign-out button it withholds from a caller with no session.
	OAuth OAuthAuthority
	// TrustedProxies is the peer allowlist that makes `netid.ClientIPHeader` readable,
	// and it is REQUIRED whenever this surface is reachable by anybody but the local
	// host — `cmd/cairn-ui` refuses to start otherwise, mirroring the pod.
	//
	// 🔴 EMPTY IS NOT "TRUST NOBODY'S HEADER AND CARRY ON" — it is "there is no proxy",
	// which is correct only for a loopback bind. `netid.ResolveClient` then keys on the
	// TCP peer, which for a loopback listener is always the local host, so the limiter
	// would have exactly one bucket. That is fine on a developer's machine and wrong
	// anywhere else, which is why the refusal lives at the bind address rather than here.
	TrustedProxies []netip.Prefix
	// Limiter throttles failed sign-ins. Nil disables it, which is what the unit tests
	// use and what a loopback bring-up gets.
	//
	// ⚠ A NIL LIMITER IS A REAL ABSENCE AND THE HANDLER SAYS SO RATHER THAN PRETENDING:
	// `POST /sign-in` is then unbounded. It is nil-able because the alternative is a
	// mandatory dependency in every test that never signs in, which is how a guard ends
	// up constructed wrongly in fifty places.
	Limiter *netid.RateLimiter
	// Source is the store read, narrowed by the caller's authority.
	Source Source
	// Sharing is the control-plane read and write the share flow needs.
	//
	// 🔴 IT IS REQUIRED, NOT OPTIONAL, EVEN THOUGH A DEPLOYMENT OVER A TOKEN FILE
	// CANNOT WRITE. A nil-means-disabled field would put a route in the ledger whose
	// handler was inert — and the ledger is the thing this surface's guards read to
	// decide what to probe, so an inert row is a row every guard walks and none
	// measures.
	//
	// 🔴 AND THE SENTENCE THAT STOOD HERE WAS FALSE, RETRACTED RATHER THAN QUIETLY
	// REPLACED. It read: "A read-only authority is answered by the WRITE failing with
	// `control.ErrAuthorityReadOnly` … the READS still work, and 'who can see this' is
	// worth serving whether or not this deployment can change it." The SECOND half is
	// wrong for the only read-only authority this tree has, and the first is wrong about
	// which read: `GET /share` (the index) answers 200 and carries the banner —
	// `TestAReadOnlyDeploymentSaysSoOnThePageRatherThanAtTheClick` measures exactly that.
	// What does NOT work is the SCOPE page, "who has access to this", which 404s. `control/tokenfile` confers `admin`
	// on NOBODY, so on such a deployment no scope is administrable, every scope page
	// answers 404, and the write never reaches the sentinel — see `refuseWrite`. ⚠ The
	// same claim was corrected in `README.md` one commit earlier and this copy was left
	// standing: a retraction is a TREE-WIDE SWEEP, not an edit at the site you happened
	// to be reading.
	Sharing Sharing
	// Sessions is the durable session table sign-in writes to and sign-out removes
	// from. It is the SAME store the cookie backend in `Auth` reads; two stores would
	// be a logout that revokes a session nothing authenticates from.
	Sessions identity.SessionStore
	// TTL is a session's absolute lifetime. Zero means `identity.DefaultSessionTTL`;
	// negative is refused.
	TTL time.Duration
	// Now is the clock, injected so expiry is testable without sleeping. It must be
	// the same clock the session store uses, or a session can be live to one and dead
	// to the other.
	Now func() time.Time
	// Log is where operational lines go. Nil means `io.Discard`.
	//
	// 🔴 NOTHING WRITTEN HERE MAY CARRY A SESSION ID, A CSRF TOKEN OR A PRESENTED
	// CREDENTIAL. `TestNoSecretReachesTheLogOrThePage` is what measures that, with a
	// positive control so the zero it reports is not a sink wired to nothing.
	Log io.Writer
}

// ErrNoAuthenticator refuses a server with no way to authenticate anybody, at
// CONSTRUCTION time — the same fail-closed direction `identity.ErrNoBackends`
// takes one level down. A surface that authorises nobody serves nothing and passes
// every health check.
var ErrNoAuthenticator = errors.New("ui: no authenticator was supplied, so no request could ever be authenticated")

// ErrNoSource refuses a server with nothing to render.
var ErrNoSource = errors.New("ui: no source was supplied, so every page would render empty")

// ErrNoSharing refuses a server whose share routes are in the ledger and wired to
// nothing. Separate from `ErrNoSource` because they are separate wirings, and an
// operator reading a startup refusal needs to know which one is missing.
var ErrNoSharing = errors.New("ui: no sharing authority was supplied, so the share routes would be declared and inert")

// ErrNoCredentials refuses a server whose sign-in form could never resolve anything.
// Separate from `ErrNoAuthenticator` because they are separate wirings and an operator
// reading a startup refusal needs to know which one is missing.
var ErrNoCredentials = errors.New("ui: no credential authority was supplied, so no sign-in could ever succeed")

// ErrNoSessions refuses a server with nowhere to put a session. Without it sign-in
// would return a cookie nothing can resolve, which looks like a working sign-in
// followed by an immediate, unexplained sign-out.
var ErrNoSessions = errors.New("ui: no session store was supplied, so a sign-in could mint no session")

// ErrNegativeTTL refuses a session lifetime that is negative. Zero is legal and means
// the default; a negative one would mint sessions that are already expired, so every
// sign-in would appear to succeed and every subsequent request would be refused.
var ErrNegativeTTL = errors.New("ui: the session TTL is negative, so every session would be born expired")

// New builds the server, refusing each missing part with its own sentinel.
func New(cfg Config) (*Server, error) {
	if cfg.Auth == nil {
		return nil, ErrNoAuthenticator
	}
	if cfg.Credentials == nil {
		return nil, ErrNoCredentials
	}
	if cfg.Source == nil {
		return nil, ErrNoSource
	}
	if cfg.Sharing == nil {
		return nil, ErrNoSharing
	}
	if cfg.Sessions == nil {
		return nil, ErrNoSessions
	}
	if cfg.TTL < 0 {
		return nil, ErrNegativeTTL
	}
	ttl := cfg.TTL
	if ttl == 0 {
		ttl = identity.DefaultSessionTTL
	}
	now := cfg.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	out := cfg.Log
	if out == nil {
		out = io.Discard
	}
	return &Server{
		auth:        cfg.Auth,
		credentials: cfg.Credentials,
		source:      cfg.Source,
		sharing:     cfg.Sharing,
		sessions:    cfg.Sessions,
		ttl:         ttl,
		now:         now,
		log:         out,

		oauth: cfg.OAuth,
		// The SAME clock the server and the session store take, for the reason
		// `Config.Now` records: a flight live to one and dead to the other is a sign-in
		// that fails at the last step for no visible reason.
		flights: newFlights(now),

		trustedProxies: cfg.TrustedProxies,
		limiter:        cfg.Limiter,
	}, nil
}

// ServeHTTP dispatches from the ledger and from nothing else.
//
// 🔴 THE ORDER OF THE GATES IS THE SECURITY MODEL, AND EACH ONE IS DERIVED FROM THE
// REQUEST RATHER THAN OPTED INTO BY A ROW:
//
//  1. the health path, before everything, because a readiness probe broken by a
//     security guard is how the guard gets deleted;
//  2. the SAME-ORIGIN gate, on every state-changing method, before authentication —
//     it costs nothing, it needs no credential, and it is the only thing standing in
//     front of a cross-site POST to the PUBLIC sign-in row, which by definition has no
//     session to carry a token;
//  3. public rows, dispatched with a zero `identity.Identity`;
//  4. the authentication chain, whose refusal is uniform across every remaining path —
//     except for ONE content-negotiated branch on `GET /`, see below;
//  5. the ledger, which answers 404 for a path that is not a row;
//  6. the CSRF TOKEN gate, on every state-changing method that got this far.
//
// 🔴 GATE (5) ANSWERS 404 WHERE IT ANSWERED THE UNIFORM 401, AND THE PREMISE THAT MADE THE
// 401 WORTH ITS COST IS VOID. It read: "a 404 for a path that is not a route would let an
// unauthenticated caller map the URL space." That is true and it does not matter, because
// this repository is PUBLIC and `routes.go` publishes every row — the URL space is mappable
// by reading the file the server is built from. What the 401 bought was therefore nothing an
// attacker did not already have, and what it cost was real: a browser landing on a mistyped
// path was told it was unauthorized, and the "no route" case was indistinguishable from the
// "wrong credential" case in this surface's OWN logs and tests.
//
// 🔴 WHAT IS *KEPT* IS THE PROPERTY THAT WAS ALWAYS THE VALUABLE HALF: A BAD CREDENTIAL IS
// STILL ANSWERED UNIFORMLY. Gate (4) runs BEFORE gate (5), so an UNAUTHENTICATED caller
// still cannot tell a route from a typo — the uniform answer is now a consequence of the
// gate ORDER rather than a guard spelled into gate (5) — and no refusal anywhere on this
// surface says which half of a credential was wrong. That is the oracle over the credential
// table, and it is a different property from the URL space.
//
// 🔴 AND THE ONE CONTENT-NEGOTIATED BRANCH, WHICH IS AN OPERATOR DECISION RATHER THAN A
// CONSEQUENCE. An unauthenticated `GET /` from something that `Accept`s `text/html` is
// answered 303 to the sign-in page; everything else — every other path, every other method,
// and any client that did not ask for HTML — keeps the uniform 401 byte for byte. So a
// browser landing on the root is shown the way in, and a script or a machine client sees
// exactly what it saw before: the machine contract is unmoved, which is the whole reason the
// branch is derived from `Accept` and from the path rather than from a route class.
// `TestTheRootRedirectsABrowserAndRefusesEverythingElse` is what measures both halves.
//
// 🔴 THE CSRF GATE IS AFTER AUTHENTICATION ON PURPOSE, AND THAT IS WHAT MAKES IT
// REACHABLE RATHER THAN SHADOWED. A token check placed ahead of the chain would refuse
// every unauthenticated request before the chain ever ran, so a test asserting "a
// request without a token is refused" would pass against a server whose token check did
// nothing at all. Here the only way to reach it is to be authenticated, which is the
// case `TestTheCSRFGuardIsReachedByAnAUTHENTICATEDRequest` builds.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// (1) Before everything, and the only thing before the origin gate.
	if r.URL.Path == HealthPath {
		writePlain(w, http.StatusOK, healthBody)
		return
	}

	// (2) Same origin, for every method that can change something.
	if stateChanging(r) && !sameOrigin(r) {
		writePlain(w, http.StatusForbidden, crossSiteRefusal)
		return
	}

	rt, known := routes[routeKey{method: r.Method, path: r.URL.Path}]

	// (3) A public row runs with no identity at all. The zero value is passed rather
	// than a synthesized one so a handler that mistakenly read `id.Auth` would see the
	// zero `control.Authorization`, which permits nothing.
	if known && rt.class&classPublic != 0 {
		rt.handle(s, w, r, identity.Identity{})
		return
	}

	// (4)
	id, err := s.auth.Authenticate(r)
	if err != nil || !id.Valid() {
		// 🔴 THE ONE WIDENING, AND IT IS SCOPED TO THE ROOT PATH AND TO A CLIENT THAT
		// ASKED FOR HTML. See [Server.ServeHTTP]'s own comment for the decision; what is
		// here is its narrowness. It is NOT derived from a route class, because a class
		// can only make a route less protected and this branch must not be reachable by
		// declaring one; it is derived from the PATH and the `Accept` header, which is the
		// same "derive it from the request" rule both cross-site gates follow.
		if r.Method == http.MethodGet && r.URL.Path == RootPath && acceptsHTML(r) {
			http.Redirect(w, r, SignInPath, http.StatusSeeOther)
			return
		}
		// 🔴 THE SAME UNIFORM REFUSAL THE POD GIVES, FOR THE SAME REASON, AND IT STILL
		// COVERS AN UNKNOWN PATH — not because gate (5) spells it, but because this gate
		// runs FIRST. A 401 that differed from the bad-credential 401 would let a caller
		// enumerate which paths exist, and THAT half of the property is the one that was
		// always worth its cost: it is about the credential table, not about the URL space.
		//
		// ⚠ `WWW-Authenticate` IS NOT SENT, DELIBERATELY. A browser that receives it
		// raises a native basic-auth dialog, which is a credential prompt this
		// surface does not implement and cannot honour.
		writePlain(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	// (5) An authenticated caller asking for a path that is not a row gets the honest
	// answer. See [Server.ServeHTTP] for why this is no longer the uniform 401, and for
	// what is kept instead.
	if !known {
		writePlain(w, http.StatusNotFound, noSuchRoute)
		return
	}

	// (6)
	if stateChanging(r) && !csrfTokenValid(r) {
		writePlain(w, http.StatusForbidden, csrfRefusal)
		return
	}

	rt.handle(s, w, r, id)
}

// stateChanging is the ONE predicate that decides which requests the two cross-site
// gates apply to, so there is no second spelling to disagree with it.
//
// 🔴 IT IS A DENYLIST OF SAFE METHODS RATHER THAN AN ALLOWLIST OF UNSAFE ONES, WHICH IS
// THE OPPOSITE OF `safeHref`'s ruling AND CORRECT FOR THE OPPOSITE REASON. There the
// permitted set is small and closed (two URL schemes) while the dangerous set is open;
// here the SAFE set is the closed one — `GET`, `HEAD` and `OPTIONS` are defined as
// having no side effects — and the unsafe set is open, because a method this server does
// not dispatch today is a method somebody may add tomorrow. Listing the unsafe ones
// would make a new verb default to unguarded.
func stateChanging(r *http.Request) bool {
	switch r.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return false
	default:
		return true
	}
}

// noSuchRoute is what gate (5) says. A fixed sentence that does not echo the path: a
// response that repeated what was asked for would reflect caller-chosen text, which is the
// same rule `crossSiteRefusal` follows one file over.
const noSuchRoute = "no such route"

// acceptsHTML answers whether this client asked for HTML.
//
// 🔴 IT LOOKS FOR `text/html` EXPLICITLY AND DOES NOT HONOUR `*/*`, WHICH IS THE WHOLE
// NARROWNESS OF THE ROOT REDIRECT. Every browser sends `text/html` at the front of its
// `Accept`; `curl` sends `*/*`, and a Go client that sets nothing sends no header at all.
// Treating `*/*` as "wants HTML" would move the machine contract — every script that GETs
// `/` with no credential would start receiving a redirect instead of the 401 it was written
// against — which is precisely what this branch is scoped to avoid.
//
// ⚠ IT DOES NOT PARSE `Accept` PROPERLY, AND THAT IS STATED RATHER THAN IMPLIED. A full
// parse would weigh `q=0` — `Accept: text/html;q=0` means "anything BUT html" — so a client
// that spelled that would be redirected here. The cost is a redirect to a page such a client
// will not render; the benefit of not writing a media-type parser is that there is no
// media-type parser. If a caller ever depends on the distinction, this is the function to
// make honest.
func acceptsHTML(r *http.Request) bool {
	return strings.Contains(r.Header.Get("Accept"), "text/html")
}

func writePlain(w http.ResponseWriter, code int, body string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(code)
	_, _ = w.Write([]byte(body))
}

// handlePage is the entries page's handler. It was once the ONE content handler and is
// no longer — `GET /share` is classed `content` too, and `contentAuthority` in
// `routes_test.go` is where each content route declares WHICH authority it answers from.
// See `routes` for the route this replaced and the sentence that made it wrong.
func (s *Server) handlePage(w http.ResponseWriter, r *http.Request, id identity.Identity) {
	scopes, err := s.source.Visible(id.Auth)
	if err != nil {
		// The reason does not reach the wire. `store.StoreMissingError` and
		// `store.EntryUnreadableError` both carry a filesystem path, and a path is a
		// fact about the deployment rather than about the request.
		writePlain(w, http.StatusInternalServerError, "the store could not be read")
		return
	}
	// 🔴 THE CSRF TOKEN RENDERED INTO THIS PAGE IS DERIVED FROM THE COOKIE ON THIS
	// REQUEST, NOT FROM THE IDENTITY. A page reached with an `Authorization` header and
	// no cookie therefore renders an EMPTY token, and its sign-out button will be
	// refused by gate (6) — which is correct rather than a gap: there is no session for
	// that caller to sign out of. Deriving it from the identity would require the
	// identity to carry a session id, and `identity.Identity` deliberately carries no
	// backend discriminator at all.
	s.renderPage(w, id, scopes, csrfTokenFor(r))
}

func (s *Server) renderPage(w http.ResponseWriter, id identity.Identity, scopes []Scope, csrf string) {
	var b strings.Builder
	if err := Page(id.Principal.Display, scopes, csrf).Render(&b); err != nil {
		writePlain(w, http.StatusInternalServerError, "the page could not be rendered")
		return
	}
	writeHTML(w, http.StatusOK, b.String())
}

// writeHTML is the ONE place an HTML response's headers are chosen, so the sign-in page
// and the content page cannot end up under different policies.
func writeHTML(w http.ResponseWriter, code int, body string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// 🔴 `nosniff` IS NOT DECORATION HERE. Every byte of the body below came out of
	// a store entry somebody wrote, and a browser that content-sniffs a response it
	// was told is HTML can be talked into a different type by the leading bytes.
	w.Header().Set("X-Content-Type-Options", "nosniff")
	// The content-security policy is a SECOND barrier behind the escaping rather than a
	// replacement for it — the escaping is the guard, and `TestHostileEntryTextIsEscaped`
	// is what measures it.
	w.Header().Set("Content-Security-Policy", ContentSecurityPolicy)
	w.WriteHeader(code)
	_, _ = w.Write([]byte(body))
}

// handleStylesheet serves the one static asset. It is PUBLIC and it reads nothing.
//
// 🔴 IT SERVES A GO CONSTANT AND TOUCHES NO FILESYSTEM, WHICH IS WHY THERE IS NO PATH
// TRAVERSAL TO GET WRONG. The alternative — an `http.FileServer` over a directory — is the
// shape that has to be argued safe: it needs a prefix strip, it follows symlinks, it serves
// whatever somebody drops in the directory, and its route is a PREFIX match, which is a
// second way for a request to reach a handler and one `TestEveryServedPathComesFromTheLedger`
// structurally cannot probe (see `routes`, where the share flow makes the same ruling about
// path parameters). One constant at one exact path has none of those questions.
//
// ⚠ `nosniff` IS SET HERE TOO, AND NOT BECAUSE THE BYTES ARE UNTRUSTED — they are a constant
// in this repository. It is set because a browser that content-sniffs a stylesheet into
// something else is a browser this response has to be explicit with; the header costs one
// line and its absence is the kind of thing a reader assumes is deliberate.
func (s *Server) handleStylesheet(w http.ResponseWriter, _ *http.Request, _ identity.Identity) {
	w.Header().Set("Content-Type", "text/css; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	// 🔴 A SHORT `max-age` AND NOT AN IMMUTABLE ONE, BECAUSE THE URL CARRIES NO VERSION. A
	// long-lived cache on an unversioned path means a deployment that changes the stylesheet
	// serves a stale one to every returning browser until the entry expires, with nothing to
	// invalidate it. Five minutes keeps the page off the wire on a reload and cannot outlive
	// a deploy by long. A content-hashed path is the answer that would license `immutable`,
	// and it needs a build step this binary does not have.
	w.Header().Set("Cache-Control", "public, max-age=300")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(stylesheet))
}

// ContentSecurityPolicy is the policy every HTML response carries.
//
// 🔴 WHAT CHANGED FROM THE PHASE A/B POLICY, AND WHICH DIRECTION EACH MOVE WENT — because
// "we simplified the CSP" reads as "we relaxed it" and one of these is strictly stricter:
//
//   - `style-src 'unsafe-inline'` → `style-src 'self'`. This is a TIGHTENING, and it is the
//     one that cost something: an inline `<style>` element is now IMPOSSIBLE in a conforming
//     browser, so the stylesheet moved out of the document and is served from
//     [StylesheetPath] as its own route. `'unsafe-inline'` was defensible while the
//     stylesheet was a Go constant no input reached — that argument was true — but it
//     licensed every inline style on the page, including one a later edit adds from a
//     value that is not a constant. `'self'` licenses a same-origin FILE and nothing else.
//   - `script-src 'self'` is NEW, and it is the only WIDENING. There was no `script-src` at
//     all, so `default-src 'none'` forbade script outright. This admits script served from
//     this origin — which is what makes a progressive enhancement possible without another
//     policy edit — and still refuses an inline `<script>`, a `javascript:` URL and any
//     third-party script host. No page here serves script today.
//   - 🔴 THERE IS NO `img-src`, AND ITS ABSENCE IS A DELETION RATHER THAN AN OVERSIGHT. A
//     draft of this policy carried `img-src 'self' data:` for a favicon nobody had asked for,
//     which is a clause permitting something the CODE forbids: there is no `<img>` anywhere
//     in this package, and `TestHostileEntryTextIsEscaped` in `render_test.go` asserts the
//     literal `"<img"` can NEVER appear in a legitimately rendered page. A policy that is
//     wider than the code is a policy nobody can read as a claim about the code.
//     ⚠ WHOEVER ADDS AN IMAGE MEETS BOTH SIDES AT ONCE: adding `img-src` here without
//     reworking that escaping guard leaves the guard red, and reworking the guard without
//     adding `img-src` leaves the image blocked by `default-src 'none'`. They move in ONE
//     commit, and this sentence is the warning that they collide.
//   - `default-src 'none'`, `base-uri 'none'` and `form-action 'self'` are UNCHANGED.
//     `base-uri 'none'` is what stops an injected `<base href>` re-pointing every relative
//     URL on the page, and `form-action 'self'` admits this surface's own forms while
//     refusing an injected `<form action="//elsewhere">` that would exfiltrate what
//     somebody types.
//
// 🔴 AND THE ACTUAL XSS DEFENCE IS UNCHANGED BY ALL OF IT, WHICH IS THE POINT RATHER THAN A
// CAVEAT. The guard is gomponents' text and attribute-value escaping plus the AST ban on
// `Raw`/`Rawf` and on non-constant element and attribute NAMES — see this package's doc
// comment, `safeHref`, and `TestNoRawNodeConstructorAppearsInTheUIPackage`. The policy is
// the barrier BEHIND that, and `script-src 'self'` does not weaken the escaping by one
// character: a store entry's text still cannot become an element, and an injected
// `<script src>` still cannot name a host this origin does not serve.
const ContentSecurityPolicy = "default-src 'none'; script-src 'self'; style-src 'self'; " +
	"base-uri 'none'; form-action 'self'"
