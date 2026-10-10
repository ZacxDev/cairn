package ui

import (
	"net/http"
	"strings"

	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"

	"github.com/ZacxDev/cairn/internal/control"
	"github.com/ZacxDev/cairn/internal/identity"
	"github.com/ZacxDev/cairn/internal/report"
)

// 🔴 THE HUB AND THE SESSIONS LIST — the root became an entry page on an operator decision, and the
// scope list it used to be moved to `/scopes` unchanged.
//
//   - 🔴 THE HUB READS ONE THING, `Visible`, AND ITS ONE COUNT IS `len(Visible)` — the scopes this
//     caller can read, the same list `/scopes` renders. The arcs and sessions cards carried counts in
//     the first cut (the arcs page's live rows, the sessions page's rows); they were DROPPED in review
//     (round 0, D1): each cost a whole-store walk on every hub load, roughly doubling its cost, for a
//     number one click away. The cards stay, with no count — so the hub reads nothing beyond what the
//     shell already needed, and can count nothing the viewer cannot read.
//   - ⚠ THE TEAM CARD CARRIES NO COUNT EITHER: "who has access" is the SHARING authority, and a hub
//     consulting it would be a content route answering from two authorities, which `contentAuthority`
//     refuses to express.
//   - 🔴 `/?q=` AND `/?tag=` ARE A 303 TO `/scopes` WITH THE QUERY RE-ENCODED, NEVER A 404. They were
//     the root's own parameters, so bookmarks, typed URLs and the mobile plan's Search shortcut carry
//     them. The `Location` is built by `url.Values.Encode` over the parsed query rather than by
//     echoing `RawQuery`, so it never carries a caller's raw bytes — every value survives, in sorted
//     key order.

// SessionsList is the sessions page's data: the report, plus whether the arcs half could not be read.
type SessionsList struct {
	Report report.SessionsAcrossReport
	// ArcsUnreadable is "the journal is CONFIGURED and could NOT be read" — `SessionAnswer`'s field,
	// for its reason: the writes half is still answered, and the page badges the arcs half unknown.
	ArcsUnreadable bool
}

// AllSessions answers the sessions page through the caller's visible set — the narrowing `Session`
// uses, handed to the walk `report.SessionAcross` shares.
func (s StoreSource) AllSessions(auth control.Authorization) (SessionsList, error) {
	visible := scopeSetOf(auth.NamedScopes(control.VerbRead))
	snap, unreadable, err := s.arcSnapshotOrUnreadable()
	if err != nil {
		return SessionsList{}, err
	}
	rep, err := report.SessionsAcross(s.Root, visible, snap)
	if err != nil {
		return SessionsList{}, err
	}
	return SessionsList{Report: rep, ArcsUnreadable: unreadable}, nil
}

// redirectsToScopes is whether a hub request carries one of the scope list's own parameters.
// PRESENCE, not value: `/?q=` (an empty search) is the Search shortcut's own spelling.
func redirectsToScopes(r *http.Request) bool {
	q := r.URL.Query()
	return q.Has(QueryQuery) || q.Has(QueryTag)
}

// handleHub renders the entry page, or sends an old scope-list URL to its new home.
func (s *Server) handleHub(w http.ResponseWriter, r *http.Request, id identity.Identity) {
	if redirectsToScopes(r) {
		http.Redirect(w, r, ScopesPath+"?"+r.URL.Query().Encode(), http.StatusSeeOther)
		return
	}
	scopes, err := s.source.Visible(id.Auth)
	if err != nil {
		writePlain(w, http.StatusInternalServerError, "the store could not be read")
		return
	}
	view := PageView{Viewer: id.Principal.Display, CSRF: csrfTokenFor(r), Scopes: scopes, Now: s.now(), App: s.app}
	s.render(w, HubPage(view))
}

// The hub cards' one-line explainers. The operator asked for one each; they are the ONLY `what`
// lines on the browse surface, because the hub is the one page whose job is to say what each part is.
const (
	hubArcsWhat     = "Named pieces of work, and the sessions and scopes each one touched."
	hubScopesWhat   = "Every scope you can read, newest first — with search and the tag filter."
	hubSessionsWhat = "Every writing session you can see, newest first."
	hubTeamWhat     = "Who can read and write each scope you administer, invitations, and team links."
)

// HubPage is the root: four cards, each a way in. Only the scopes card carries a count.
func HubPage(v PageView) g.Node {
	return shell("cairn", v, nil,
		h.Div(h.Class("scope-grid"), h.ID("hub"),
			hubCard("arcs", "Arcs", ArcsPath, hubArcsWhat, nil),
			hubCard("scopes", "Scopes", ScopesPath, hubScopesWhat,
				stat(plural(len(v.Scopes), "readable scope", "readable scopes"), "")),
			hubCard("sessions", "Sessions", SessionsPath, hubSessionsWhat, nil),
			// The Team page (#214): sharing, single invitations and team links on one page —
			// `/share` and `/invite` now only redirect there.
			hubCard("team", "Team", TeamPath, hubTeamWhat, nil),
		),
	)
}

// hubCard is one card: its title as the link, one line saying what it is, and its count when there
// is one (nil renders no stats row).
func hubCard(name, title, href, what string, count g.Node) g.Node {
	return h.Section(
		h.Class("card"),
		h.Data("hub", name),
		h.Div(h.Class("card-head"), h.H2(h.A(h.Class("card-name"), h.Href(href), g.Text(title)))),
		h.P(h.Class("note"), g.Text(what)),
		g.If(count != nil, h.P(h.Class("card-stats"), count)),
	)
}

// handleSessionsPage renders every session this caller can see anything of.
//
// ⚠ `Visible` IS ASKED FIRST, for `handleArcsPage`'s reasons: the shell is built from it, and the
// scope chips link by `control.ID`, which only the narrowed answer carries.
func (s *Server) handleSessionsPage(w http.ResponseWriter, r *http.Request, id identity.Identity) {
	scopes, err := s.source.Visible(id.Auth)
	if err != nil {
		writePlain(w, http.StatusInternalServerError, "the store could not be read")
		return
	}
	list, err := s.source.AllSessions(id.Auth)
	if err != nil {
		writePlain(w, http.StatusInternalServerError, "the store could not be read")
		return
	}
	view := PageView{Viewer: id.Principal.Display, CSRF: csrfTokenFor(r), Scopes: scopes, Now: s.now(), App: s.app}
	view.SessionsList = &list
	view.Panes = s.panesFor(id)
	s.render(w, SessionsPage(view))
}

// SessionsPage is the sessions list: one compact row per session, newest first, each the session
// page's link — `sessionRow`'s shape, over every readable scope rather than one.
func SessionsPage(v PageView) g.Node {
	list := *v.SessionsList
	rep := list.Report
	ids := idsByName(v.Scopes)
	all := make([]string, 0, len(rep.Sessions))
	for _, s := range rep.Sessions {
		all = append(all, s.ID)
	}
	labels := shortIDsIn(all)
	var badges []g.Node
	if rep.Partial() {
		badges = append(badges, partialBadge("lower bound", plural(rep.Coverage.EntriesMalformed+
			rep.Coverage.EntriesUnreadable, "entry file", "entry files")+
			" in scopes you can read were never scanned, so a session that wrote only there is NOT listed"))
	}
	if list.ArcsUnreadable {
		badges = append(badges, partialBadge("arcs unknown", arcJournalUnreadable))
	}
	return shell("cairn — sessions", v, []crumb{{Label: "sessions"}},
		h.Section(
			h.Class("card"),
			h.ID("sessions-index"),
			h.H2(g.Text("Sessions")),
			h.P(h.Class("card-stats"),
				stat(plural(len(rep.Sessions), "session", "sessions")+" in "+plural(rep.ScopesScanned, "scope", "scopes"),
					report.SessionsReadsLine),
				attributedStat(rep.Coverage, joinCaveats(report.SessionsAttributionLine, report.SessionsReadsLine)),
				g.If(!rep.ArcsConfigured && !list.ArcsUnreadable,
					h.Span(h.Class("stat stat-quiet"), h.TitleAttr(report.RegistrationsUnconfiguredBody), g.Text("arcs off"))),
				g.Group(badges),
			),
			g.If(len(rep.Sessions) == 0, h.P(h.Class("empty"), g.Text(
				"No session is visible to you: no bullet in a scope you can read carries a write trailer, "+
					"and no arc homed in one lists a member."))),
			g.If(len(rep.Sessions) > 0, h.Ul(h.Class("entry-list"), h.ID("sessions-list"),
				g.Map(rep.Sessions, func(s report.SessionAcrossReport) g.Node {
					return sessionsListRow(s, labels[s.ID], ids, v)
				}))),
		),
	)
}

// sessionsListRow is one session on the sessions page. The whole row is the session page's link
// (`row-link`); the scope and arc chips sit above it and keep their own links.
func sessionsListRow(s report.SessionAcrossReport, label string, ids map[string]control.ID, v PageView) g.Node {
	_, last := s.DateSpan()
	scopes := make([]string, 0, len(s.Scopes))
	for _, sc := range s.Scopes {
		scopes = append(scopes, sc.Scope)
	}
	return h.Li(
		h.Class("entry-row session-row"),
		h.Data("session", s.ID),
		h.Span(h.Class("ref"), h.TitleAttr(s.ID),
			h.A(h.Class("row-link"), h.Href(sessionHref(s.ID)), g.Text(label))),
		h.Span(h.Class("title"), g.Text(strings.Join(s.Actors(), ", "))),
		h.Span(h.Class("entry-count"), g.Text(plural(s.Bullets(), "bullet", "bullets"))),
		dateAgo(last, v.Now),
		v.Panes.badge(s.ID, v.Now),
		g.If(len(scopes) > 0, h.Ul(h.Class("chips chips-scope"), g.Map(scopes, func(name string) g.Node {
			return scopeChip(name, ids)
		}))),
		g.If(len(s.Arcs) > 0, h.Ul(h.Class("chips chips-arc"), g.Map(s.Arcs, func(a report.SessionArc) g.Node {
			return arcChip(a.Home, a.Slug, ids)
		}))),
	)
}
