package ui

import (
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

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
// sentences that state its coverage, which the plan requires to be THE SAME on every surface:
// `RenderText`'s lines are exported methods for exactly this reader. ⚠ Since the compact-layout
// change those sentences ride in `title=` tooltips beside the NUMBERS they qualify, rather than as
// visible paragraphs — still the pod's bytes, never a second spelling. See the block above `stat`.

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

// 🔴 THE CARDS' PROSE IS GONE, AND EVERY CLAIM IT MADE IS STILL ON THE PAGE — AS A NUMBER OR ONE HOVER
// AWAY. An operator decision: the sessions and arcs views read as a wall of coverage sentences on a
// page read every day. What replaced them, and the rule each half keeps:
//
//   - ONE stats row per view, of NUMBERS: "36 sessions · 72 of 102 bullets attributed". 🔴 THE
//     ATTRIBUTED K OF N IS NEVER OMITTED where something was scanned — a design requirement of the
//     arcs work ("each answer stating its own coverage"): three bullets in four carry no trailer on
//     the measured stores, so a session list without its denominator reads as complete and is not.
//   - every CAVEAT the cards printed (reads not recorded, self-reported attribution, the visibility
//     and provenance rules, the registration's freshness) rides on that stat's `title=` tooltip, and
//     the tooltip text is `internal/report`'s OWN sentence, read through the exported line methods —
//     never a second spelling, and the CLI's bytes are untouched.
//   - a WARNING BADGE (`badge-warn`) ONLY when the answer is partial: a lower bound (entries the index
//     rejected or could not read), nothing scannable, a damaged journal, an unmeasured writer leg. A
//     badge on every answer would be a badge nobody reads; one only on the partial ones is the signal.
//
// ⚠ AND THE ROWS ARE COMPACT. A session row is its short id (full id in `title=`), actor, bullet
// count, last write and arc chips, and the whole row links to the session page — which REVERSES this
// file's earlier "session ids are text, never links": the operator asked to click through, and the
// link is a ledger path plus `url.Values.Encode`, never the id as an href.

// stat is one number in a stats row, with the claim's caveat as its tooltip.
func stat(text, tooltip string) g.Node {
	return h.Span(h.Class("stat"), g.If(tooltip != "", h.TitleAttr(tooltip)), g.Text(text))
}

// partialBadge is the warning that an answer is partial. `why` is the report's own sentence.
func partialBadge(label, why string) g.Node {
	return h.Span(h.Class("badge badge-warn"), h.TitleAttr(why), g.Text(label))
}

// attributedStat is "K of N bullets attributed" — the denominator every sessions answer carries.
func attributedStat(c touch.Coverage, tooltip string) g.Node {
	return stat(strconv.Itoa(c.Attributed)+" of "+strconv.Itoa(c.Bullets)+" bullets attributed", tooltip)
}

// joinCaveats is the tooltip text: report sentences, one per line, in the order given.
func joinCaveats(lines ...string) string {
	var out []string
	for _, l := range lines {
		if l != "" {
			out = append(out, l)
		}
	}
	return strings.Join(out, "\n")
}

// sessionsCount is the Sessions tab's label count: "" when the number is not a measurement (nothing
// in the scope could be scanned), `≥N` when it is a lower bound.
func sessionsCount(r report.SessionsReport) string {
	switch r.Status {
	case report.StatusScopeUnreadable, report.StatusScopeAbsent:
		return ""
	}
	n := strconv.Itoa(len(r.Sessions))
	if r.Partial() {
		return "≥" + n
	}
	return n
}

// arcsTabCount is the Arcs tab's label count: "" when the journal is off or unreadable — no count is
// a different fact from a zero — and `≥N` when the journal is damaged or the scope partial.
func arcsTabCount(t Touched) string {
	r := t.Arcs
	if t.ArcsUnreadable || (r.Status != report.StatusNoArcRegistered && r.Status != report.StatusArcsListed) {
		return ""
	}
	n := strconv.Itoa(len(r.Arcs))
	if arcsPartial(r) {
		return "≥" + n
	}
	return n
}

// arcsPartial is the arcs answer's partial predicate: a damaged journal, or a scope some of whose
// entries were never scanned (so an arc inferred only from them is missing).
func arcsPartial(r report.ArcsReport) bool {
	return r.Damaged || r.Coverage.EntriesMalformed > 0 || r.Coverage.EntriesUnreadable > 0
}

// sessionsPanel is the scope page's Sessions tab.
func sessionsPanel(t Touched, scopes []Scope, now time.Time) g.Node {
	r := t.Sessions
	ids := idsByName(scopes)
	var all []string
	for _, s := range r.Sessions {
		all = append(all, s.ID)
	}
	labels := shortIDsIn(all)
	arcsBySession := map[string][]report.ArcLine{}
	for _, a := range t.Arcs.Arcs {
		for _, m := range a.MemberSessions {
			arcsBySession[m] = append(arcsBySession[m], a)
		}
	}
	var badge g.Node
	switch {
	case r.Status == report.StatusScopeUnreadable:
		badge = partialBadge("unmeasured", r.StatusSentence())
	case r.LowerBoundLine() != "":
		badge = partialBadge("lower bound", r.LowerBoundLine())
	}
	return h.Div(
		h.ID("scope-sessions"),
		h.P(h.Class("card-stats"),
			stat(plural(len(r.Sessions), "session", "sessions"), report.SessionsReadsLine),
			g.If(r.Status != report.StatusScopeAbsent, attributedStat(r.Coverage,
				joinCaveats(r.AttributedLine(), r.ScannedLine(), report.SessionsAttributionLine))),
			badge,
		),
		g.If(r.StatusSentence() != "", h.P(h.Class("empty"), h.TitleAttr(r.StatusSentence()),
			g.Text(sessionsEmpty[r.Status]))),
		g.If(len(r.Sessions) > 0, h.Ul(h.Class("entry-list"), g.Map(sessionsNewestFirst(r.Sessions),
			func(s touch.Session) g.Node { return sessionRow(s, labels[s.ID], arcsBySession[s.ID], ids, now) }))),
	)
}

// sessionsEmpty is the short visible line for each status that lists nothing; the report's full
// sentence is its tooltip.
var sessionsEmpty = map[string]string{
	report.StatusScopeAbsent:        "Not readable here.",
	report.StatusScopeEmpty:         "No entry here yet.",
	report.StatusScopeUnreadable:    "Nothing here could be scanned.",
	report.StatusNoAttributedWrites: "No bullet here carries a write trailer.",
}

// sessionsNewestFirst orders session rows by last write, newest first; undated sessions after every
// dated one; ties by id, byte-wise. A copy — the report's own order (by id) is what the pod prints.
func sessionsNewestFirst(in []touch.Session) []touch.Session {
	out := slices.Clone(in)
	slices.SortStableFunc(out, func(a, b touch.Session) int {
		if a.LastDate != b.LastDate {
			return strings.Compare(b.LastDate, a.LastDate)
		}
		return strings.Compare(a.ID, b.ID)
	})
	return out
}

// sessionRow is one compact session row. The whole row is the session page's link (`row-link`
// stretches over it); the arc chips sit above that layer and keep their own links.
func sessionRow(s touch.Session, label string, arcLines []report.ArcLine, ids map[string]control.ID, now time.Time) g.Node {
	return h.Li(
		h.Class("entry-row session-row"),
		h.Span(h.Class("ref"), h.TitleAttr(s.ID),
			h.A(h.Class("row-link"), h.Href(sessionHref(s.ID)), g.Text(label))),
		h.Span(h.Class("title"), g.Text(strings.Join(s.Actors, ", "))),
		h.Span(h.Class("entry-count"), g.Text(plural(len(s.Bullets), "bullet", "bullets"))),
		dateAgo(s.LastDate, now),
		g.If(len(arcLines) > 0, h.Ul(h.Class("chips chips-arc"), g.Map(arcLines, func(a report.ArcLine) g.Node {
			return arcChip(a.Home, a.Slug, ids)
		}))),
	)
}

// arcChip is one arc as a chip, linked to its page when its home's id is known.
func arcChip(home, slug string, ids map[string]control.ID) g.Node {
	id := ids[store.NormalizeRef(home)]
	if id == "" {
		return h.Li(h.TitleAttr(home+"/"+slug), g.Text(slug))
	}
	return h.Li(h.A(h.Href(arcHref(id, slug)), h.TitleAttr(home+"/"+slug), g.Text(slug)))
}

// scopeChip is one scope as a chip, linked to its page when it is readable (its id is known).
func scopeChip(name string, ids map[string]control.ID) g.Node {
	id := ids[store.NormalizeRef(name)]
	if id == "" {
		return h.Li(g.Text(name))
	}
	return h.Li(h.A(h.Href(scopeHref(Scope{ID: id, Name: name})), g.Text(name)))
}

// sessionChip is one session as a chip: its label in this list (`shortIDsIn`), the full id in its
// tooltip.
func sessionChip(id, label string) g.Node {
	return h.Li(h.A(h.Href(sessionHref(id)), h.TitleAttr(id), g.Text(label)))
}

// arcsPanel is the scope page's Arcs tab.
func arcsPanel(t Touched, scopes []Scope, now time.Time) g.Node {
	r := t.Arcs
	ids := idsByName(scopes)
	counted := !t.ArcsUnreadable && (r.Status == report.StatusNoArcRegistered || r.Status == report.StatusArcsListed)
	var badge g.Node
	if counted && arcsPartial(r) {
		why := r.AttributedLine()
		if r.Damaged {
			why = report.ArcsDamagedLine
		}
		badge = partialBadge("lower bound", why)
	}
	return h.Div(
		h.ID("scope-arcs"),
		// "Could not look" is an ERROR, not a caveat, so it stays a visible line.
		g.If(t.ArcsUnreadable, h.P(h.Class("card-warn"), g.Text(arcJournalUnreadable))),
		g.If(!t.ArcsUnreadable && r.Status == report.StatusRegistrationsUnconfigured,
			h.P(h.Class("empty"), h.TitleAttr(report.RegistrationsUnconfiguredBody), g.Text(arcsOff))),
		g.If(counted, h.P(h.Class("card-stats"),
			stat(plural(len(r.Arcs), "arc", "arcs"), joinCaveats(r.StatusesLine(), report.ArcsVisibilityLine,
				report.ArcsProvenanceLine, report.ArcsRegistrationLine)),
			stat(strconv.Itoa(r.InArcs)+" of "+strconv.Itoa(r.Sessions)+" sessions in an arc", r.SessionsLine()),
			attributedStat(r.Coverage, joinCaveats(r.AttributedLine(), report.SessionsAttributionLine,
				report.SessionsReadsLine)),
			badge,
		)),
		g.If(counted && len(r.Arcs) == 0, h.P(h.Class("empty"), h.TitleAttr(r.StatusSentence()),
			g.Text("No registered arc you can see touched this scope."))),
		g.If(len(r.Arcs) > 0, h.Ul(h.Class("entry-list"), g.Map(r.Arcs, func(a report.ArcLine) g.Node {
			return arcRow(a, ids, now)
		}))),
	)
}

// arcsOff is the short visible line for the designed off state; the pod's own sentence is its
// tooltip, and the arc page shows that sentence in full.
const arcsOff = "Arc registrations are not configured on this deployment."

// arcRow is one listed arc.
//
// 🔴 THE STATUS IS `report.StatusWord`, IN A BADGE OF ITS OWN, AND `unknown` IS NOT STYLED OR WORDED
// AS `open` (Q4). Every status shares one badge class on purpose: a class keyed on the status would
// be a second mapping of the three words, and the first one to drift would be `unknown` reading as
// something it is not.
func arcRow(a report.ArcLine, ids map[string]control.ID, now time.Time) g.Node {
	label := a.Home + "/" + a.Slug
	var name g.Node = h.Span(g.Text(label))
	if home := ids[store.NormalizeRef(a.Home)]; home != "" {
		name = h.A(h.Href(arcHref(home, a.Slug)), g.Text(label))
	}
	memberLabels := shortIDsIn(a.MemberSessions)
	provenance := "inferred"
	if a.Declared {
		provenance = "declared"
	}
	return h.Li(
		h.Class("entry-row arc-row"),
		h.Span(h.Class("ref"), h.TitleAttr(label), name),
		h.Span(h.Class("badge"), h.TitleAttr("status"), g.Text(report.StatusWord(a.Status))),
		h.Span(h.Class("badge badge-quiet"), h.TitleAttr(a.Provenance()), g.Text(provenance)),
		h.Span(h.Class("entry-count"), g.Text("closing "+a.ClosingKind)),
		instantAgo(a.RegisteredAt, now),
		g.If(len(a.DeclaredVisible) > 0, h.Ul(h.Class("chips chips-scope"), h.TitleAttr("declared scopes you can read"),
			g.Map(a.DeclaredVisible, func(n string) g.Node { return scopeChip(n, ids) }))),
		g.If(len(a.MemberSessions) > 0, h.Ul(h.Class("chips chips-session"), h.TitleAttr("member sessions"),
			g.Map(a.MemberSessions, func(id string) g.Node { return sessionChip(id, memberLabels[id]) }))),
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
				h.P(h.Class("empty"), g.Text(report.RegistrationsUnconfiguredBody)),
			),
		)
	}
	reg := rep.Reg
	s := *v.Scope
	ids := idsByName(v.Scopes)
	var memberIDs []string
	for _, m := range reg.Members {
		memberIDs = append(memberIDs, m.Session)
	}
	memberLabels := shortIDsIn(memberIDs)
	statusTip := "status"
	if reg.Status == arcs.StatusUnknown {
		statusTip = report.UnknownStatusGloss
	}
	var badge g.Node
	switch {
	case rep.Damaged:
		badge = partialBadge("lower bound", report.ArcsDamagedLine)
	case !reg.WritersMeasured:
		badge = partialBadge("unmeasured", rep.ToolingCoverageLine())
	case rep.CarriedLine() != "":
		badge = partialBadge("carried", rep.CarriedLine())
	}
	return shell("cairn — "+reg.Home+"/"+reg.Slug, v,
		[]crumb{{Label: s.Name, Href: scopeHref(s)}, {Label: reg.Slug}},
		h.Section(
			h.Class("card"),
			h.ID("arc-summary"),
			h.H2(g.Text(reg.Slug)),
			h.P(h.Class("card-stats"),
				h.Span(h.Class("badge"), h.TitleAttr(statusTip), g.Text(report.StatusWord(reg.Status))),
				stat("closing "+reg.ClosingKind, "closing condition"),
				h.Span(h.Class("stat"), h.TitleAttr(rep.RegisteredLine()),
					g.Text("registered "), instantAgo(reg.RegisteredAt, v.Now), g.Text(" by "+reg.RegisteredBy)),
				stat(strconv.Itoa(reg.CommitsUnstamped)+" of "+strconv.Itoa(reg.CommitsTotal)+" commits unstamped",
					rep.ToolingCoverageLine()),
				attributedStat(rep.Coverage, joinCaveats(rep.MemberWritesLine(), report.SessionsAttributionLine,
					report.SessionsReadsLine, report.ArcVisibilityLine, report.ArcsRegistrationLine)),
				badge,
			),
			g.If(len(rep.DeclaredVisible) > 0, labelledList("Declared scopes", rep.DeclaredLine(),
				h.Ul(h.Class("chips chips-scope"), g.Map(rep.DeclaredVisible, func(n string) g.Node {
					return scopeChip(n, ids)
				})))),
			labelledList("Members "+strconv.Itoa(len(reg.Members)), rep.MembersHeading(),
				h.Ul(h.Class("entry-list"), g.Map(rep.SortedMembers(), func(m arcs.Member) g.Node {
					return memberRow(rep, m, memberLabels[m.Session], ids, v.Now)
				}))),
		),
	)
}

// memberRow is one arc member: its session (linked), role, first-seen time, the READABLE scopes it
// wrote in as chips, and `carried` when merge rule 7 kept it.
func memberRow(rep report.ArcReport, m arcs.Member, label string, ids map[string]control.ID, now time.Time) g.Node {
	return h.Li(
		h.Class("entry-row session-row"),
		h.Span(h.Class("ref"), h.TitleAttr(m.Session),
			h.A(h.Class("row-link"), h.Href(sessionHref(m.Session)), g.Text(label))),
		h.Span(h.Class("title"), g.Text(m.Role)),
		instantAgo(m.FirstSeen, now),
		g.If(m.Carried, h.Span(h.Class("badge badge-quiet"), h.TitleAttr(rep.CarriedLine()), g.Text("carried"))),
		g.If(len(rep.WroteIn[m.Session]) > 0, h.Ul(h.Class("chips chips-scope"), h.TitleAttr("wrote in"),
			g.Map(rep.WroteIn[m.Session], func(n string) g.Node { return scopeChip(n, ids) }))),
	)
}

// instantAgo renders an RFC 3339 instant (a registration or first-seen time) the way [timeAgo] renders
// an mtime; a value that does not parse is shown as written rather than guessed at, and "" renders
// nothing.
func instantAgo(rfc3339 string, now time.Time) g.Node {
	if rfc3339 == "" {
		return nil
	}
	at, err := time.Parse(time.RFC3339, rfc3339)
	if err != nil {
		return h.Span(h.Class("updated"), g.Text(rfc3339))
	}
	at = at.UTC()
	return h.Time(h.Class("updated"), h.DateTime(at.Format(time.RFC3339)),
		h.TitleAttr(at.Format("2006-01-02 15:04:05 UTC")), g.Text(relativeTime(at, now)))
}

// dateAgo renders a bullet's `YYYY-MM-DD` date relative to `now`, AT DAY PRECISION: the bytes carry
// no time of day, so "5h ago" would be precision nobody measured. Today is "today", then "Nd ago"
// below 30 days, then the date itself — [relativeTime]'s buckets above the day. A zero clock, a
// future date and a value that does not parse all render the date as written; "" renders nothing.
func dateAgo(date string, now time.Time) g.Node {
	if date == "" {
		return nil
	}
	return h.Time(h.Class("updated"), h.DateTime(date), h.TitleAttr(date), g.Text(daysAgo(date, now)))
}

func daysAgo(date string, now time.Time) string {
	at, err := time.Parse("2006-01-02", date)
	if err != nil || now.IsZero() {
		return date
	}
	today := time.Date(now.UTC().Year(), now.UTC().Month(), now.UTC().Day(), 0, 0, 0, 0, time.UTC)
	days := int(today.Sub(at) / (24 * time.Hour))
	switch {
	case days < 0 || days >= 30:
		return date
	case days == 0:
		return "today"
	default:
		return strconv.Itoa(days) + "d ago"
	}
}
