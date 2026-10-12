package ui

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"

	"github.com/ZacxDev/cairn/internal/codesrc"
	"github.com/ZacxDev/cairn/internal/control"
	"github.com/ZacxDev/cairn/internal/identity"
)

// THE CODE-SOURCES SURFACE — S4 of `claudedocs/plan-cairn-scope-refs.md` (decisions 4 and 5): which
// repositories and branch a scope declares it describes, shown on the scope page and edited on
// `GET`/`POST /scope/sources`.
//
// 🔴 THIS SURFACE IS THE JOURNAL'S ONLY WRITER, AND `handleSourcesSave` IS ITS ONE CALL SITE OF
// `codesrc.Journal.Set`. The pod opens the journal read-only and serves it (`GET
// /api/v1/sources/<scope>`); the CLI only reads it. `TestTheUIWritesTheSourcesJournalFromOneSite`
// holds the call-site count, and `internal/api`'s own pin holds the pod at zero.
//
// 🔴 THE KEY IS `codesrc.Key(scope.Name)` — THE STORE DIRECTORY NAME OUT OF THE NARROWED INDEX — AND
// NEVER `scope.ID`. This surface's scope ids come from the CONTROL JOURNAL (random at creation) while
// the pod's come from the TOKEN-FILE projection (derived from the directory name), so a record keyed
// by id is one the pod can never find (the plan's round-3 🔴1, the operator's Q17 answer).
// `TestAUIWrittenDeclarationIsWhatThePodServes` builds both halves and is what would see it.
//
// 🔴 EDITING TAKES `admin` ON THE SCOPE — the share flow's verb. READING takes `read`: the edit page
// shows the declaration to any reader and the form only to an admin. The save answers ONE refusal,
// byte-identical, for a scope that does not exist, one the caller cannot read, and one they can read
// but not administer. A token-file deployment confers `admin` on nobody
// (`tokenfile/source.go`), so there nobody can edit — the designed state, not a defect.

// sourcesRefusal is the edit flow's one refusal. It names no reason, `scopeRefusal`'s discipline.
const sourcesRefusal = "no such scope, or it is not yours to administer"

// sourcesUnreadable is the BROKEN state's answer on this surface. 🔴 IT CARRIES NO PATH AND NO OS
// ERROR — the journal's location is a fact about the deployment (`arcJournalUnreadable`'s rule, one
// file over); the detail goes to the operator log.
const sourcesUnreadable = "the code-sources journal could not be read; this is NOT 'no sources declared'"

// NoSourceJournal is the OFF state's sentence, on the edit page and on a save.
const NoSourceJournal = "Code sources are not configured on this deployment — it was started without $" +
	codesrc.EnvJournal + ", so no declaration can be shown or saved. This is the designed off state, " +
	"NOT 'this scope declares no sources'."

// SourcesProvenance is the line every rendering of a declaration carries: whose word it is.
const SourcesProvenance = "A declaration is set here by an admin of the scope and is that admin's word: " +
	"its grammar is checked, never that the code exists."

// The edit form's fields.
const (
	FieldSources  = "sources"
	FieldRevision = "revision"
)

// outcomeSourcesSaved is the one outcome code the edit page renders from its query string (the share
// flow's closed-set rule: a code maps to a sentence written here, never reflected text).
const outcomeSourcesSaved = "saved"

// SourcesBlock is the scope page's code-sources block. nil on the view means no journal is
// configured, and the block is then absent.
type SourcesBlock struct {
	// Unreadable is the journal's BROKEN state: configured, and could not be read. Said, never shown
	// as "undeclared".
	Unreadable bool
	// Record is the scope's latest declaration (an empty list included), nil when it has none.
	Record *codesrc.Record
	// CanEdit is `admin` on the scope, read from the request's own authorization.
	CanEdit bool
}

// sourcesBlock reads the scope's declaration for the scope page; nil when the journal is off.
func (s *Server) sourcesBlock(id identity.Identity, scope Scope) *SourcesBlock {
	if s.sourceJournal.Path == "" {
		return nil
	}
	block := &SourcesBlock{CanEdit: scope.ID != "" && id.Auth.Allows(scope.ID, control.VerbAdmin)}
	snap, err := s.sourceJournal.Read()
	if err != nil {
		s.logf("sources: the journal could not be read: %v", err)
		block.Unreadable = true
		return block
	}
	if rec, ok := snap.For(scope.Name); ok {
		block.Record = &rec
	}
	return block
}

// sourcesBlockNode renders the block. Every source is TEXT (`g.Text`); the only href is
// [sourceHref]'s, through `safeHref`.
func sourcesBlockNode(s Scope, b SourcesBlock) g.Node {
	return h.Div(
		h.ID("scope-sources"),
		h.H3(g.Text("Code sources")),
		g.If(b.Unreadable, h.P(h.Class("read-only"), h.Data("sources", "unreadable"), g.Text(
			"The code-sources journal could not be read, so this scope's declaration cannot be shown. "+
				"That is NOT 'no sources declared'."))),
		g.If(!b.Unreadable && (b.Record == nil || len(b.Record.Sources) == 0),
			h.P(h.Class("empty"), h.Data("sources", "undeclared"), g.Text("This scope declares no code sources."))),
		g.Iff(!b.Unreadable && b.Record != nil && len(b.Record.Sources) > 0, func() g.Node {
			return h.Ul(g.Map(b.Record.Sources, sourceItem))
		}),
		g.Iff(!b.Unreadable && b.Record != nil, func() g.Node {
			return h.P(h.Class("note"), h.Data("sources", "set-by"),
				g.Text("Set by "+b.Record.SetBy+" at "+b.Record.SetAt+". "+SourcesProvenance))
		}),
		g.If(b.CanEdit, h.P(h.A(h.Href(sourcesHref(s.ID, nil)), h.Data("sources", "edit"), g.Text("Edit code sources")))),
	)
}

// sourceItem is one source: its canonical text, and a link only for the built-in GitHub shape.
func sourceItem(canonical string) g.Node {
	href, ok := sourceHref(canonical)
	if !ok {
		return h.Li(h.Code(g.Text(canonical)))
	}
	return h.Li(h.A(h.Href(href), h.Rel("noopener noreferrer"), h.Code(g.Text(canonical))))
}

// sourceHref is the browse URL of a source on `github.com` — `https://github.com/<repo-path>/tree/<branch>`
// — and false for every other host: this repository may not name a self-hosted host, so there is no
// URL shape to build for one (the ref registry's rule). The branch is escaped per SEGMENT, so a `#`,
// `?` or `%` in a valid branch name cannot end the path, and the result still goes through `safeHref`.
func sourceHref(canonical string) (string, bool) {
	src, err := codesrc.Parse(canonical)
	if err != nil || src.Host != "github.com" {
		return "", false
	}
	segments := strings.Split(src.Branch, "/")
	for i, seg := range segments {
		segments[i] = url.PathEscape(seg)
	}
	return safeHref("https://github.com/" + src.RepoPath + "/tree/" + strings.Join(segments, "/"))
}

// sourcesHref is the edit page for one scope, with an outcome when there is one.
func sourcesHref(scope control.ID, outcome url.Values) string {
	q := url.Values{QueryScope: {string(scope)}}
	for k, v := range outcome {
		q[k] = v
	}
	return SourcesPath + "?" + q.Encode()
}

// SourcesEdit is the edit page's view.
type SourcesEdit struct {
	Scope Scope
	// Configured is false in the OFF state: the page says so and renders no form.
	Configured bool
	// CanEdit is `admin` on the scope; without it the page shows the declaration and no form.
	CanEdit bool
	// Record is the CURRENT declaration, nil when the scope has none.
	Record *codesrc.Record
	// Revision is what the form carries back — the current revision, or `codesrc.RevisionNone`.
	Revision string
	// Text is the textarea's content: the current list, or what was typed on a refused save.
	Text string
	// Outcome is a sentence written here from a closed code; Problem is a refused save's reason.
	Outcome, Problem string
}

// sourcesTarget resolves a scope id to a scope the caller can READ (the narrowed index — the only
// source of the store directory name the key is built from) AND administer. Anything else is the
// one refusal, written here.
func (s *Server) sourcesTarget(w http.ResponseWriter, id identity.Identity, raw string) ([]Scope, Scope, bool) {
	scopes, err := s.source.Visible(id.Auth)
	if err != nil {
		writePlain(w, http.StatusInternalServerError, "the store could not be read")
		return nil, Scope{}, false
	}
	scope, found := pickScope(scopes, control.ID(raw))
	if !found || !id.Auth.Allows(scope.ID, control.VerbAdmin) {
		writePlain(w, http.StatusNotFound, sourcesRefusal)
		return nil, Scope{}, false
	}
	return scopes, scope, true
}

// handleSourcesPage is `GET /scope/sources?scope=<id>`: the scope's current declaration for anyone who
// may READ the scope, and — for an `admin` with a session — the form.
//
// 🔴 THE PAGE IS GATED BY READ, THE FORM BY ADMIN, AND THE WRITE BY ADMIN AGAIN ON THE POST. A request
// naming no scope gets the navigation page (`handleScopePage`'s ruling: it named nothing, so there is
// nothing for a refusal to protect), and a scope outside the narrowed answer gets the browse surface's
// one refusal, byte-identical to every other miss.
func (s *Server) handleSourcesPage(w http.ResponseWriter, r *http.Request, id identity.Identity) {
	scopes, err := s.source.Visible(id.Auth)
	if err != nil {
		writePlain(w, http.StatusInternalServerError, "the store could not be read")
		return
	}
	view := s.sourcesPageView(r, id, scopes)
	wanted := control.ID(r.URL.Query().Get(QueryScope))
	if wanted == "" {
		s.renderNavigate(w, view)
		return
	}
	scope, found := pickScope(scopes, wanted)
	if !found {
		writePlain(w, http.StatusNotFound, browseRefusal)
		return
	}
	edit := SourcesEdit{Scope: scope, Configured: s.sourceJournal.Path != "",
		CanEdit: id.Auth.Allows(scope.ID, control.VerbAdmin)}
	if r.URL.Query().Get(QueryOutcome) == outcomeSourcesSaved {
		edit.Outcome = "Saved."
	}
	if edit.Configured {
		snap, err := s.sourceJournal.Read()
		if err != nil {
			s.logf("sources: the journal could not be read: %v", err)
			writePlain(w, http.StatusServiceUnavailable, sourcesUnreadable)
			return
		}
		edit.withCurrent(snap)
	}
	s.render(w, SourcesPage(view, edit))
}

// withCurrent fills the view from the scope's current record — the form's starting state, and what a
// stale save re-renders.
func (e *SourcesEdit) withCurrent(snap codesrc.Snapshot) {
	e.Record, e.Revision, e.Text = nil, snap.RevisionFor(e.Scope.Name), ""
	if rec, ok := snap.For(e.Scope.Name); ok {
		e.Record = &rec
		e.Text = strings.Join(rec.Sources, "\n")
	}
}

func (s *Server) sourcesPageView(r *http.Request, id identity.Identity, scopes []Scope) PageView {
	return PageView{Viewer: id.Principal.Display, CSRF: csrfTokenFor(r), Scopes: scopes, Now: s.now(), App: s.app}
}

// splitSourceLines is the textarea's grammar: one source per line, a trailing `\r` stripped, an EMPTY
// line skipped. Anything else — a line of spaces included — goes to `codesrc.ParseList`, which refuses
// whitespace rather than repairing it. `lineOf[i]` is the form line of `raws[i]`, so a refusal names
// the line the user sees.
func splitSourceLines(text string) (raws []string, lineOf []int) {
	for i, line := range strings.Split(text, "\n") {
		line = strings.TrimSuffix(line, "\r")
		if line == "" {
			continue
		}
		raws = append(raws, line)
		lineOf = append(lineOf, i+1)
	}
	return raws, lineOf
}

// principalStamp is the `set_by` this surface writes: the AUTHENTICATED principal, never a form field.
func principalStamp(p control.Principal) string {
	if p.Display != "" {
		return p.Display
	}
	return p.String()
}

// handleSourcesSave is `POST /scope/sources`: one append through `codesrc.Journal.Set`.
//
// It runs behind both cross-site gates, inherited from the METHOD (`Server.ServeHTTP`). `set_by` is
// the signed-in principal and `set_at` this server's clock — nothing in the form can choose either. The
// form's `revision` is compared INSIDE the journal's lock, so of two admins saving over one revision
// exactly one lands; the other is shown the CURRENT list and nothing is written.
func (s *Server) handleSourcesSave(w http.ResponseWriter, r *http.Request, id identity.Identity) {
	if err := r.ParseForm(); err != nil {
		writePlain(w, http.StatusBadRequest, "the form could not be read")
		return
	}
	if s.sourceJournal.Path == "" {
		writePlain(w, http.StatusNotImplemented, NoSourceJournal)
		return
	}
	scopes, scope, ok := s.sourcesTarget(w, id, r.PostFormValue(FieldScope))
	if !ok {
		return
	}
	text := r.PostFormValue(FieldSources)
	edit := SourcesEdit{Scope: scope, Configured: true, CanEdit: true, Text: text, Revision: r.PostFormValue(FieldRevision)}
	raws, lineOf := splitSourceLines(text)
	if raws == nil {
		raws = []string{} // an empty textarea is an explicit "undeclared", recorded with its stamp
	}
	_, err := s.sourceJournal.Set(codesrc.Key(scope.Name), raws, edit.Revision, principalStamp(id.Principal), s.now(), nil)
	var parseErr *codesrc.ParseError
	var stale *codesrc.StaleRevisionError
	var unreadable *codesrc.JournalUnreadableError
	switch {
	case err == nil:
		http.Redirect(w, r, sourcesHref(scope.ID, url.Values{QueryOutcome: {outcomeSourcesSaved}}), http.StatusSeeOther)
		return
	case errors.As(err, &parseErr):
		line := parseErr.Index
		if line >= 1 && line <= len(lineOf) {
			line = lineOf[line-1]
		}
		edit.Problem = fmt.Sprintf("Line %d (%s) %s. Nothing was saved.", line, parseErr.Input, parseErr.Rule)
		if snap, readErr := s.sourceJournal.Read(); readErr == nil {
			edit.withRecordOnly(snap)
		}
		s.renderStatus(w, http.StatusUnprocessableEntity, SourcesPage(s.sourcesPageView(r, id, scopes), edit))
		return
	case errors.As(err, &stale):
		snap, readErr := s.sourceJournal.Read()
		if readErr != nil {
			s.logf("sources: the journal could not be read: %v", readErr)
			writePlain(w, http.StatusServiceUnavailable, sourcesUnreadable)
			return
		}
		edit.withCurrent(snap)
		edit.Problem = "These sources changed after you opened this page — someone saved first. " +
			"This is the CURRENT list; nothing was saved."
		s.renderStatus(w, http.StatusConflict, SourcesPage(s.sourcesPageView(r, id, scopes), edit))
		return
	case errors.As(err, &unreadable):
		s.logf("sources: the journal could not be written: %v", err)
		writePlain(w, http.StatusServiceUnavailable, sourcesUnreadable)
		return
	default:
		s.logf("sources: the save failed: %v", err)
		writePlain(w, http.StatusInternalServerError, "the sources could not be saved")
	}
}

// withRecordOnly attaches the current record for display beside a refused draft, keeping the
// draft's own text AND revision — a refused line is the user's to fix, not a reason to discard it.
func (e *SourcesEdit) withRecordOnly(snap codesrc.Snapshot) {
	if rec, ok := snap.For(e.Scope.Name); ok {
		e.Record = &rec
	}
}

// SourcesPage is the edit page.
func SourcesPage(v PageView, e SourcesEdit) g.Node {
	return shell("Code sources · "+e.Scope.Name, v,
		[]crumb{{Label: e.Scope.Name, Href: scopeHref(e.Scope)}, {Label: "Code sources"}},
		h.Section(
			h.Class("card"),
			h.H2(g.Text("Code sources · "+e.Scope.Name)),
			h.P(h.Class("note"), g.Text("The repositories and branch this scope documents, one per line, as "+
				"git:<host>/<repo-path>[//<subpath>]@<branch> — for example "+
				"git:github.com/example-org/example-repo@main. The first is the primary. At most 8. "+
				"An empty list records that the scope declares none.")),
			h.P(h.Class("note"), g.Text(SourcesProvenance)),
			g.If(!e.Configured, h.P(h.Class("read-only"), h.Data("sources", "unconfigured"), g.Text(NoSourceJournal))),
			g.If(e.Outcome != "", h.P(h.Class("outcome"), h.Data("sources", "outcome"), g.Text(e.Outcome))),
			g.If(e.Problem != "", h.P(h.Class("outcome"), h.Data("sources", "problem"), g.Text(e.Problem))),
			// 🔴 THE CURRENT LIST IS RENDERED OUTSIDE THE FORM, so a reader who may not edit still sees it —
			// the page's stated contract. It was once only the textarea's content, which only an admin gets.
			g.If(e.Configured && (e.Record == nil || len(e.Record.Sources) == 0),
				h.P(h.Class("empty"), h.Data("sources", "undeclared"), g.Text("This scope declares no code sources."))),
			g.Iff(e.Record != nil && len(e.Record.Sources) > 0, func() g.Node {
				return h.Ul(h.Data("sources", "current"), g.Map(e.Record.Sources, sourceItem))
			}),
			g.Iff(e.Record != nil, func() g.Node {
				return h.P(h.Class("note"), h.Data("sources", "set-by"),
					g.Text("Current declaration set by "+e.Record.SetBy+" at "+e.Record.SetAt+"."))
			}),
			g.If(e.Configured && !e.CanEdit, h.P(h.Class("note"), h.Data("sources", "read-only"), g.Text(
				"You can read this scope's code sources but not change them: that takes admin on the scope."))),
			g.If(e.Configured && e.CanEdit && v.CSRF != "", h.FormEl(
				h.Class("invite"),
				h.Method("post"),
				h.Action(SourcesPath),
				h.Input(h.Type("hidden"), h.Name(FieldCSRF), h.Value(v.CSRF)),
				h.Input(h.Type("hidden"), h.Name(FieldScope), h.Value(string(e.Scope.ID))),
				h.Input(h.Type("hidden"), h.Name(FieldRevision), h.Value(e.Revision)),
				h.Label(h.For("sources-text"), g.Text("Sources, one per line")),
				h.Textarea(h.ID("sources-text"), h.Name(FieldSources), h.Rows("8"), g.Text(e.Text)),
				h.Button(h.Type("submit"), g.Text("Save code sources")),
			)),
		),
	)
}
