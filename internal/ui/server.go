package ui

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/ZacxDev/cairn/internal/control"
	"github.com/ZacxDev/cairn/internal/identity"
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
//  4. the authentication chain, whose refusal is uniform across every remaining path;
//  5. the ledger, so an unknown path is indistinguishable from a bad credential;
//  6. the CSRF TOKEN gate, on every state-changing method that got this far.
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
		// 🔴 THE SAME UNIFORM REFUSAL THE POD GIVES, FOR THE SAME REASON, AND IT
		// COVERS AN UNKNOWN PATH TOO. A 404 for a path that is not a route would let
		// an unauthenticated caller map the URL space; a 401 that differs from the
		// bad-credential 401 would let them enumerate which paths exist.
		//
		// ⚠ `WWW-Authenticate` IS NOT SENT, DELIBERATELY. A browser that receives it
		// raises a native basic-auth dialog, which is a credential prompt this
		// surface does not implement and cannot honour.
		//
		// ⚠ AND IT IS NOT A REDIRECT TO THE SIGN-IN PAGE, WHICH IS THE OBVIOUS
		// BROWSER-FRIENDLY THING AND WAS REFUSED. A 303 for `GET /` beside a 401 for
		// `GET /admin` tells an unauthenticated caller which paths are real, which is
		// the enumeration this uniform answer exists to prevent. The cost is that a
		// browser landing on `/` sees plain text; the entry point is `/sign-in`, and
		// widening the answer is a decision a later phase can make deliberately.
		writePlain(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	// (5)
	if !known {
		writePlain(w, http.StatusUnauthorized, "unauthorized")
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
	// A content-security policy with no `script-src` permits no script at all,
	// which is a SECOND barrier behind the escaping rather than a replacement for
	// it — the escaping is the guard, and `TestHostileEntryTextIsEscaped` is what
	// measures it.
	w.Header().Set("Content-Security-Policy", ContentSecurityPolicy)
	w.WriteHeader(code)
	_, _ = w.Write([]byte(body))
}

// ContentSecurityPolicy is the policy every HTML response carries.
//
// 🔴 `form-action 'self'` WHERE PHASE A HAD `'none'`, AND THE WIDENING IS A DECISION
// RATHER THAN A CONSEQUENCE. `'none'` forbids a form submission outright, so the sign-in
// and sign-out forms would be inert in a conforming browser — the policy would have
// silently disabled the feature rather than refusing to ship it. `'self'` still refuses
// a form that posts anywhere but this origin, which is the property `'none'` was buying
// on a page that had no forms: it means an injected `<form action="//elsewhere">` cannot
// exfiltrate whatever a user types. Nothing else moved; there is still no `script-src`,
// so no script runs at any origin.
const ContentSecurityPolicy = "default-src 'none'; style-src 'unsafe-inline'; base-uri 'none'; form-action 'self'"
