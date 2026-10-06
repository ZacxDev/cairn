package ui

import (
	"errors"
	"net/http"
	"net/url"

	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"

	"github.com/ZacxDev/cairn/internal/arcs"
	"github.com/ZacxDev/cairn/internal/control"
	"github.com/ZacxDev/cairn/internal/identity"
	"github.com/ZacxDev/cairn/internal/report"
	"github.com/ZacxDev/cairn/internal/store"
	"github.com/ZacxDev/cairn/internal/touch"
)

// 🔴 S4 OF THE ARCS/SESSIONS PHASE: "WHO WROTE HERE, AND WHICH ARCS TOUCHED IT", IN A BROWSER. Two
// surfaces — a section on the scope page and an arc page at `/arc?home=<scope id>&slug=<slug>` —
// and NO NEW DERIVATION. Everything this file renders is a value S1–S3 already compute for the
// pod: `report.Sessions` (over `internal/touch`), `report.Arcs` and `report.Arc` (over
// `internal/arcs`' journal). This file only LAYS THEM OUT.
//
// 🔴 VISIBILITY IS NOT DECIDED HERE, AND THAT IS THE WHOLE DESIGN. The pod narrows every read through
// `rq.visible = who.Auth.VisibleScopes(control.VerbRead)`; this surface narrows every read through
// `scopeSetOf(auth.NamedScopes(control.VerbRead))` — `Authorization.VisibleScopes` spelled over the
// walk `Visible` already makes, the ONE narrowing `StoreSource.Visible` uses. That set is handed to
// the report functions unchanged, and THEY apply the arc rule (an arc exists for a caller iff its
// HOME scope is readable, operator decision Q1) — so there is no second visibility check in
// `internal/ui` to drift from the pod's. A browser that re-derived "may this caller see this arc"
// would be a third surface deciding it, and the plan's whole reason for a single renderer is that a
// second decision drifts in the direction nobody notices.
//
// 🔴 ENTRY STRUCTURE IS STILL `internal/store`'s, NEVER `internal/report`'s — that README decision
// is untouched. What comes from `internal/report` here is the ARCS/SESSIONS DATA and the
// sentences that state its coverage, which the plan requires to be THE SAME on every surface
// (pinned as whole strings): `RenderText`'s lines are exported methods for exactly this reader, so
// the page prints the pod's bytes rather than a second spelling of them.

// Touched is the scope page's sessions-and-arcs section, as one value.
type Touched struct {
	// Sessions is `report.Sessions` over this caller's visible set — the pod's `sessions/<scope>`.
	Sessions report.SessionsReport
	// Arcs is `report.Arcs` over the same set — the pod's `arcs/<scope>`. Its Status is
	// `registrations-unconfigured` on a deployment started without `-arc-journal`.
	Arcs report.ArcsReport
	// ArcsUnreadable is "the journal is CONFIGURED and could NOT be read" — the four-state rule's
	// "could not look", which the pod answers 503. Here it is a sentence in the section rather than
	// an error page, because the scope's entries were read fine and refusing the whole page for a
	// side file would hide them. Arcs is the zero value when this is set.
	ArcsUnreadable bool
}

// arcSnapshot is the journal read one request renders from, or nil when no journal is configured.
// Re-read on EVERY call, the pod's own arrangement (`api.Server.arcSnapshot`): a registration the pod
// appends is visible on the next page load, and there is no cache here to go stale.
//
// 🔴 THE UI NEVER WRITES IT. The reference deployment mounts the journal's volume READ-ONLY in the
// UI's pod; `arcs.Journal.Read` is a plain `os.ReadFile`, and nothing in this package reaches
// `Register`.
func (s StoreSource) arcSnapshot() (*arcs.Snapshot, error) {
	if s.ArcJournal == "" {
		return nil, nil
	}
	snap, err := arcs.Journal{Path: s.ArcJournal}.Read()
	if err != nil {
		return nil, err
	}
	return &snap, nil
}

// Touched answers the scope page's section for ONE scope, through the caller's visible set.
//
// ⚠ THE SCOPE PAGE HAS ALREADY REFUSED A SCOPE THIS CALLER CANNOT READ, before this runs —
// `handleScopePage` picks the scope out of `Visible`'s narrowed answer first. The report functions
// would answer `scope-absent` for it anyway, from the same set; that is the same rule reached twice,
// not a second rule.
func (s StoreSource) Touched(auth control.Authorization, scope string) (Touched, error) {
	visible := scopeSetOf(auth.NamedScopes(control.VerbRead))
	sessions, err := report.Sessions(s.Root, scope, visible)
	if err != nil {
		return Touched{}, err
	}
	out := Touched{Sessions: sessions}
	snap, err := s.arcSnapshot()
	if err != nil {
		var unreadable *arcs.JournalUnreadableError
		if errors.As(err, &unreadable) {
			out.ArcsUnreadable = true
			return out, nil
		}
		return Touched{}, err
	}
	out.Arcs, err = report.Arcs(s.Root, scope, visible, snap)
	if err != nil {
		return Touched{}, err
	}
	return out, nil
}

// Arc answers the arc page. `home` is a scope NAME; "" means the handler could not resolve the
// requested home id among this caller's readable scopes, and `report.Arc` then answers exactly what
// it answers for a key nobody registered (its `NormalizeKey` refuses the empty home) — unless no
// journal is configured, which it answers first and uniformly.
func (s StoreSource) Arc(auth control.Authorization, home, slug string) (report.ArcReport, error) {
	visible := scopeSetOf(auth.NamedScopes(control.VerbRead))
	snap, err := s.arcSnapshot()
	if err != nil {
		return report.ArcReport{}, err
	}
	return report.Arc(s.Root, home, slug, visible, snap)
}

// handleArcPage renders ONE arc.
//
// 🔴 EVERY WAY TO MISS IS ONE ANSWER: 404 WITH `report.ArcUnregisteredBody`, THE POD'S OWN SENTENCE.
// An unknown home id, a home this caller cannot read, an unregistered slug under a readable home,
// and a REGISTERED arc homed in an unreadable scope all reach it, and the last is the case it exists
// for: the browser equivalent of S3's `TestAnArcHomedInAnUnreadableScopeLeaksNothing`. The body
// names neither the home nor the slug, so the refused answer and the absent one are the same BYTES,
// not merely the same status.
//
// 🔴 THE HOME IS A `control.ID`, NOT A NAME, for `/scope?id=`'s reason: an id is minted over a
// URL-safe alphabet and a scope name is user text. It is MATCHED against the narrowed `Visible`
// answer (`pickScope`), never resolved against the store — the same refusal path as the scope page,
// so "not yours" and "does not exist" are one code path here too. The SLUG is matched against the
// registered set inside `report.Arc`; it is never a path.
//
// ⚠ A REQUEST NAMING NOTHING IS NOT A REFUSAL — it gets the navigation page, `handleScopePage`'s
// ruling, which is also what lets `TestEveryContentRouteConsultsTheAuthority` see this row ask.
//
// ⚠ AN UNCONFIGURED JOURNAL IS A 200 PAGE SAYING SO, NEVER AN ERROR, and it is decided before the
// home is looked at — so it says the same thing to every caller about every arc. A configured
// journal that cannot be read is a 503: "could not look", never "no such arc".
func (s *Server) handleArcPage(w http.ResponseWriter, r *http.Request, id identity.Identity) {
	scopes, err := s.source.Visible(id.Auth)
	if err != nil {
		writePlain(w, http.StatusInternalServerError, "the store could not be read")
		return
	}
	view := PageView{Viewer: id.Principal.Display, CSRF: csrfTokenFor(r), Scopes: scopes}

	q := r.URL.Query()
	homeID, slug := control.ID(q.Get(QueryHome)), q.Get(QuerySlug)
	if homeID == "" || slug == "" {
		s.renderNavigate(w, view)
		return
	}
	home := ""
	scope, found := pickScope(scopes, homeID)
	if found {
		home = scope.Name
	}
	rep, err := s.source.Arc(id.Auth, home, slug)
	if err != nil {
		var unreadable *arcs.JournalUnreadableError
		if errors.As(err, &unreadable) {
			writePlain(w, http.StatusServiceUnavailable, arcJournalUnreadable)
			return
		}
		writePlain(w, http.StatusInternalServerError, "the store could not be read")
		return
	}
	switch rep.Status {
	case report.StatusArcFound:
		view.Scope = &scope
	case report.StatusRegistrationsUnconfigured:
		// Nothing about the home is carried onto the page: the off state is one page for everybody.
	default:
		writePlain(w, http.StatusNotFound, report.ArcUnregisteredBody)
		return
	}
	view.Arc = &rep
	s.render(w, ArcPage(view))
}

// arcJournalUnreadable is the 503 body. It carries no path: the journal's location is a fact about
// the deployment, and the pod's operator log is where it goes (`handlePage`'s rule for store errors).
const arcJournalUnreadable = "the arc registration journal is configured and could not be read — this is NOT 'no arc registered'"

// arcHref is the ONE place an arc URL is built.
//
// ⚠ `safeHref` IS NOT REACHED AND MUST NOT BE — `scopeHref`'s ruling: it ALLOWLISTS absolute
// http(s), so it would refuse this same-origin path and render every arc as unlinked text. The
// safety of this href is that its PATH is a ledger constant and both operands go through
// `url.Values.Encode`, so neither a slug nor a scope id can end the attribute, add a parameter or
// supply a scheme: whatever they contain lands percent-encoded inside one query value.
func arcHref(home control.ID, slug string) string {
	return ArcPath + "?" + url.Values{QueryHome: []string{string(home)}, QuerySlug: []string{slug}}.Encode()
}

// idsByName keys the narrowed scope list's ids on the FOLDED name, for `scopeIDsByFoldedName`'s
// reason: the report names a scope as the index spells it, and the fold is what makes two spellings
// one scope.
func idsByName(scopes []Scope) map[string]control.ID {
	out := make(map[string]control.ID, len(scopes))
	for _, s := range scopes {
		out[store.NormalizeRef(s.Name)] = s.ID
	}
	return out
}

// noteLines renders fixed coverage sentences, one paragraph each, in the order given.
func noteLines(lines ...string) g.Node {
	out := make([]g.Node, 0, len(lines))
	for _, line := range lines {
		if line == "" {
			continue
		}
		out = append(out, h.P(h.Class("note"), g.Text(line)))
	}
	return g.Group(out)
}

// statusLine is the answer's status token, printed as the pod prints it so a reader can grep
// either surface for the same word.
func statusLine(status string) g.Node {
	return h.P(h.Class("card-stats"), h.Span(h.Class("stat"), g.Text("status: "+status)))
}

// touchedSections is the scope page's two cards: who wrote here, and which arcs touched it.
//
// 🔴 EVERY COVERAGE LINE IS PRINTED, ON EVERY STATUS, AND A ZERO IS PRINTED RATHER THAN OMITTED —
// the plan's honesty contract. Three bullets in four carry no trailer on the measured stores, so a
// session list without `attributed: K of N` reads as complete and is not.
//
// 🔴 SESSION IDS ARE TEXT, NEVER LINKS (the plan's UI section): a resume command is the tooling's
// rendering concern, and a session id is a value the WRITER declared — nothing here may turn it into
// a navigation target.
func touchedSections(t Touched, scopes []Scope) g.Node {
	return g.Group([]g.Node{sessionsCard(t.Sessions), arcsCard(t, scopes)})
}

func sessionsCard(r report.SessionsReport) g.Node {
	return h.Section(
		h.Class("card"),
		h.H2(g.Text("Sessions that wrote here")),
		h.P(h.Class("card-what"), g.Text(sessionsWhat)),
		statusLine(r.Status),
		noteLines(report.SessionsReadsLine, report.SessionsAttributionLine),
		g.If(r.Status != report.StatusScopeAbsent, noteLines(r.ScannedLine(), r.AttributedLine())),
		g.If(r.LowerBoundLine() != "", h.P(h.Class("card-warn"), g.Text(r.LowerBoundLine()))),
		g.If(r.StatusSentence() != "", h.P(h.Class("empty"), g.Text(r.StatusSentence()))),
		g.If(r.StatusSentence() == "", g.Group([]g.Node{
			h.P(h.Class("note"), g.Text(r.ListHeading())),
			h.Ul(h.Class("entry-list"), g.Map(r.Sessions, func(s touch.Session) g.Node {
				return h.Li(h.Class("entry-row"), h.Span(h.Class("ref"), g.Text(report.SessionLine(s))))
			})),
		})),
	)
}

func arcsCard(t Touched, scopes []Scope) g.Node {
	r := t.Arcs
	ids := idsByName(scopes)
	counted := r.Status == report.StatusNoArcRegistered || r.Status == report.StatusArcsListed
	return h.Section(
		h.Class("card"),
		h.H2(g.Text("Arcs that touched this scope")),
		h.P(h.Class("card-what"), g.Text(arcsWhat)),
		g.If(t.ArcsUnreadable, h.P(h.Class("card-warn"), g.Text(arcJournalUnreadable))),
		g.If(!t.ArcsUnreadable, g.Group([]g.Node{
			statusLine(r.Status),
			noteLines(report.SessionsReadsLine, report.SessionsAttributionLine,
				report.ArcsVisibilityLine, report.ArcsProvenanceLine, report.ArcsRegistrationLine),
			g.If(counted, noteLines(r.AttributedLine(), r.SessionsLine(), r.StatusesLine())),
			g.If(counted && r.Damaged, h.P(h.Class("card-warn"), g.Text(report.ArcsDamagedLine))),
			g.If(r.StatusSentence() != "", h.P(h.Class("empty"), g.Text(r.StatusSentence()))),
			g.If(r.StatusSentence() == "", g.Group([]g.Node{
				h.P(h.Class("note"), g.Text(r.ListHeading())),
				h.Ul(h.Class("entry-list"), g.Map(r.Arcs, func(a report.ArcLine) g.Node {
					return arcRow(a, ids[store.NormalizeRef(a.Home)])
				})),
			})),
		})),
	)
}

// arcRow is one listed arc.
//
// 🔴 THE STATUS IS `report.StatusWord`, IN A BADGE OF ITS OWN, AND `unknown` IS NOT STYLED OR WORDED
// AS `open` (Q4). Every status shares one badge class on purpose: a class keyed on the status would
// be a second mapping of the three words, and the first one to drift would be `unknown` reading as
// something it is not.
func arcRow(a report.ArcLine, home control.ID) g.Node {
	label := a.Home + "/" + a.Slug
	var name g.Node = h.Span(g.Text(label))
	if home != "" {
		name = h.A(h.Href(arcHref(home, a.Slug)), g.Text(label))
	}
	return h.Li(
		h.Class("entry-row"),
		h.Span(h.Class("ref"), h.TitleAttr(label), name),
		h.Span(h.Class("badge badge-quiet"), g.Text(a.Provenance())),
		h.Span(h.Class("badge"), g.Text("status "+report.StatusWord(a.Status))),
		h.Span(h.Class("entry-count"), g.Text("closing "+a.ClosingKind+" · "+a.MembersPhrase())),
	)
}

// ArcPage is ONE arc: its registration, its declared scopes narrowed to what this caller can read,
// and its members with the readable scopes each wrote in.
func ArcPage(v PageView) g.Node {
	rep := *v.Arc
	if rep.Status != report.StatusArcFound {
		// The off state. One page for every caller and every arc; nothing about the request is on it.
		return shell("cairn — arcs", v, []crumb{{Label: "arc"}},
			h.Section(
				h.Class("card"),
				h.H2(g.Text("Arc")),
				statusLine(rep.Status),
				noteLines(report.SessionsReadsLine, report.SessionsAttributionLine,
					report.ArcVisibilityLine, report.ArcsRegistrationLine),
				h.P(h.Class("empty"), g.Text(report.RegistrationsUnconfiguredBody)),
			),
		)
	}
	reg := rep.Reg
	s := *v.Scope
	ids := idsByName(v.Scopes)
	return shell("cairn — "+reg.Home+"/"+reg.Slug, v,
		[]crumb{{Label: s.Name, Href: scopeHref(s)}, {Label: reg.Slug}},
		h.Section(
			h.Class("card"),
			h.H2(g.Text(reg.Slug)),
			h.P(h.Class("card-what"), g.Text(arcWhat)),
			statusLine(rep.Status),
			noteLines(report.SessionsReadsLine, report.SessionsAttributionLine,
				report.ArcVisibilityLine, report.ArcsRegistrationLine),
			g.If(rep.Damaged, h.P(h.Class("card-warn"), g.Text(report.ArcsDamagedLine))),
			h.Dl(
				h.Class("provenance"),
				h.Dt(h.Class("prov-key"), g.Text("home scope")),
				h.Dd(h.Class("prov-val"), g.Text(reg.Home)),
				h.Dt(h.Class("prov-key"), g.Text("slug")),
				h.Dd(h.Class("prov-val"), g.Text(reg.Slug)),
				h.Dt(h.Class("prov-key"), g.Text("status")),
				h.Dd(h.Class("prov-val"), h.Span(h.Class("badge"), g.Text(report.StatusWord(reg.Status)))),
				h.Dt(h.Class("prov-key"), g.Text("closing condition")),
				h.Dd(h.Class("prov-val"), g.Text(reg.ClosingKind)),
			),
			g.If(reg.Status == arcs.StatusUnknown, h.P(h.Class("note"), g.Text(report.UnknownStatusGloss))),
			noteLines(rep.RegisteredLine(), rep.ToolingCoverageLine(), rep.CarriedLine(),
				rep.DeclaredLine(), rep.MemberWritesLine()),
			g.If(len(rep.DeclaredVisible) > 0, labelledList("Declared scopes",
				"the scopes this registration names, NARROWED to the ones you can read",
				h.Ul(h.Class("scope-links"), g.Map(rep.DeclaredVisible, func(name string) g.Node {
					return h.Li(scopeLink(Scope{ID: ids[store.NormalizeRef(name)], Name: name}))
				})))),
			labelledList("Members", rep.MembersHeading()+" — the sessions the registering tool attributes to this arc",
				h.Ul(h.Class("entry-list"), g.Map(rep.SortedMembers(), func(m arcs.Member) g.Node {
					return h.Li(h.Class("entry-row"), h.Span(h.Class("ref"), g.Text(rep.MemberLine(m))))
				}))),
		),
	)
}

const (
	sessionsWhat = "Derived from the write trailers already in this scope's entries — the " +
		"`[cairn: <actor>/<session>]` suffix at the end of a bullet. Nothing here is registered, and the " +
		"lines below say what was and was not measured."
	arcsWhat = "An arc is one effort tracked by one handoff doc, registered by the operator tooling. " +
		"Each is listed as declared (its registration names this scope) or inferred (one of its member " +
		"sessions wrote an attributed bullet here)."
	arcWhat = "One registered arc. Everything below is the registering tool's own word except who pushed " +
		"it and when, which the pod stamps; every scope it names is narrowed to the scopes you can read."
)
