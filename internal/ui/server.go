package ui

import (
	"errors"
	"net/http"
	"strings"

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
	auth   identity.Authenticator
	source Source
}

// ErrNoAuthenticator refuses a server with no way to authenticate anybody, at
// CONSTRUCTION time — the same fail-closed direction `identity.ErrNoBackends`
// takes one level down. A surface that authorises nobody serves nothing and passes
// every health check.
var ErrNoAuthenticator = errors.New("ui: no authenticator was supplied, so no request could ever be authenticated")

// ErrNoSource refuses a server with nothing to render.
var ErrNoSource = errors.New("ui: no source was supplied, so every page would render empty")

// New builds the server.
func New(auth identity.Authenticator, source Source) (*Server, error) {
	if auth == nil {
		return nil, ErrNoAuthenticator
	}
	if source == nil {
		return nil, ErrNoSource
	}
	return &Server{auth: auth, source: source}, nil
}

// ServeHTTP dispatches from the ledger and from nothing else.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Before everything, and the only thing before auth.
	if r.URL.Path == HealthPath {
		writePlain(w, http.StatusOK, healthBody)
		return
	}

	id, err := s.auth.Authenticate(r)
	if err != nil || !id.Valid() {
		// 🔴 THE SAME UNIFORM REFUSAL THE POD GIVES, FOR THE SAME REASON, AND IT
		// COVERS AN UNKNOWN PATH TOO. A 404 for a path that is not a route would let
		// an unauthenticated caller map the URL space; a 401 that differs from the
		// bad-credential 401 would let them enumerate which paths exist.
		//
		// ⚠ `WWW-Authenticate` IS NOT SENT, DELIBERATELY. A browser that receives it
		// raises a native basic-auth dialog, which is a credential prompt this
		// surface does not implement and cannot honour. The sign-in flow is a later
		// phase; until it exists, a bare 401 is the honest answer.
		writePlain(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	route, known := routes[routeKey{method: r.Method, path: r.URL.Path}]
	if !known {
		writePlain(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	route(s, w, r, id)
}

// handlePage is the ONE content handler, and there is one because a page that does
// not consult the authority must not render an answer about it. See `routes` for the
// route this replaced and the sentence that made it wrong.
func (s *Server) handlePage(w http.ResponseWriter, r *http.Request, id identity.Identity) {
	scopes, err := s.source.Visible(id.Auth)
	if err != nil {
		// The reason does not reach the wire. `store.StoreMissingError` and
		// `store.EntryUnreadableError` both carry a filesystem path, and a path is a
		// fact about the deployment rather than about the request.
		writePlain(w, http.StatusInternalServerError, "the store could not be read")
		return
	}
	s.renderPage(w, id, scopes)
}

func (s *Server) renderPage(w http.ResponseWriter, id identity.Identity, scopes []Scope) {
	var b strings.Builder
	if err := Page(id.Principal.Display, scopes).Render(&b); err != nil {
		writePlain(w, http.StatusInternalServerError, "the page could not be rendered")
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// 🔴 `nosniff` IS NOT DECORATION HERE. Every byte of the body below came out of
	// a store entry somebody wrote, and a browser that content-sniffs a response it
	// was told is HTML can be talked into a different type by the leading bytes.
	w.Header().Set("X-Content-Type-Options", "nosniff")
	// A content-security policy with no `script-src` permits no script at all,
	// which is a SECOND barrier behind the escaping rather than a replacement for
	// it — the escaping is the guard, and `TestHostileEntryTextIsEscaped` is what
	// measures it.
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; base-uri 'none'; form-action 'none'")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(b.String()))
}

func writePlain(w http.ResponseWriter, code int, body string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(code)
	_, _ = w.Write([]byte(body))
}
