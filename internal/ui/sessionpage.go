package ui

import (
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"

	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"

	"github.com/ZacxDev/cairn/internal/arcs"
	"github.com/ZacxDev/cairn/internal/control"
	"github.com/ZacxDev/cairn/internal/identity"
	"github.com/ZacxDev/cairn/internal/report"
	"github.com/ZacxDev/cairn/internal/store"
	"github.com/ZacxDev/cairn/internal/touch"
	"github.com/ZacxDev/cairn/internal/write"
)

// 🔴 THE SESSION PAGE: ONE WRITING SESSION, ACROSS EVERY SCOPE THIS CALLER CAN READ. `/session?session=
// <id>`. The operator saw "Sessions that wrote here" and could not click through; this is where the
// click lands. It is the FIRST browse page keyed by a value that is not scoped — a session id is
// global, written by whoever wrote the trailer — so it is the one page that AGGREGATES ACROSS SCOPES,
// and that makes its narrowing the whole design:
//
//   - 🔴 THE SCOPES WALKED ARE `scopeSetOf(auth.NamedScopes(control.VerbRead))` — the ONE narrowing
//     `Visible`, `Touched` and `Arc` use — handed to `report.SessionAcross` unchanged. Nothing the
//     request supplies names a scope. The arc rule (home readable, Q1) is applied inside the report.
//   - 🔴 NO EXISTENCE ORACLE. A session that wrote ONLY in scopes this caller cannot read, an id
//     nobody ever wrote, and an id that is not even a possible session (`write.SessionComponent`)
//     all answer 404 with [sessionUnseenBody] — the same BYTES, which name nothing. The report makes
//     the first two the same VALUE (`report.SessionAcross`'s comment); this handler makes the third
//     the same answer without a store walk.
//   - 🔴 THE ID IS OPAQUE AND BYTE-EXACT. Never lowercased, never trimmed, never normalised — the
//     handoff's rule, and `write.Attribution`'s. It is BOUNDED by `write.SessionComponent` (64 bytes
//     of `[A-Za-z0-9_.-]`, the trailer grammar's own class) before any read, so a hostile query string
//     costs one regexp match. Every position it reaches on the page is a `g.Text` or an attribute
//     value, and the 404 never echoes it.

// SessionAnswer is the session page's data: the report, plus whether the arcs half could not be read.
type SessionAnswer struct {
	Report report.SessionAcrossReport
	// ArcsUnreadable is "the journal is CONFIGURED and could NOT be read". The writes half is still
	// answered — it reads the store, not the journal — and the page badges the arcs half as unknown.
	ArcsUnreadable bool
}

// Session answers the session page through the caller's visible set.
func (s StoreSource) Session(auth control.Authorization, session string) (SessionAnswer, error) {
	visible := scopeSetOf(auth.NamedScopes(control.VerbRead))
	snap, err := s.arcSnapshot()
	unreadable := false
	if err != nil {
		var u *arcs.JournalUnreadableError
		if !errors.As(err, &u) {
			return SessionAnswer{}, err
		}
		unreadable, snap = true, nil
	}
	rep, err := report.SessionAcross(s.Root, session, visible, snap)
	if err != nil {
		return SessionAnswer{}, err
	}
	return SessionAnswer{Report: rep, ArcsUnreadable: unreadable}, nil
}

// sessionUnseenBody is the ONE answer for every session this caller cannot see anything of. It names
// no id, no scope and no count, so the refused answer and the absent one are the same bytes.
const sessionUnseenBody = "no write by that session is visible to you — never written, written only in scopes " +
	"you cannot read, or written only in entries that could not be scanned"

// handleSessionPage renders ONE session's writes across every readable scope.
//
// ⚠ A REQUEST NAMING NOTHING IS NOT A REFUSAL — it gets the navigation page, `handleScopePage`'s
// ruling, which is also what lets `TestEveryContentRouteConsultsTheAuthority` see this row ask.
//
// ⚠ AN UNREADABLE JOURNAL WITH NOTHING ELSE VISIBLE IS A 503, NEVER THE 404: the session might be a
// member of an arc nobody could look at, and "could not look" is not "nothing there". It names
// nothing either, and every unseen id answers it alike on such a deployment.
func (s *Server) handleSessionPage(w http.ResponseWriter, r *http.Request, id identity.Identity) {
	scopes, err := s.source.Visible(id.Auth)
	if err != nil {
		writePlain(w, http.StatusInternalServerError, "the store could not be read")
		return
	}
	view := PageView{Viewer: id.Principal.Display, CSRF: csrfTokenFor(r), Scopes: scopes, Now: s.now(), App: s.app}
	session := r.URL.Query().Get(QuerySession)
	if session == "" {
		s.renderNavigate(w, view)
		return
	}
	// 🔴 THE BOUND, BEFORE ANY READ. An id the trailer grammar cannot produce was never written by
	// anybody, so it gets the unseen answer — the same bytes as every other miss.
	if !write.SessionComponent.MatchString(session) {
		writePlain(w, http.StatusNotFound, sessionUnseenBody)
		return
	}
	answer, err := s.source.Session(id.Auth, session)
	if err != nil {
		writePlain(w, http.StatusInternalServerError, "the store could not be read")
		return
	}
	if !answer.Report.Found() {
		if answer.ArcsUnreadable {
			writePlain(w, http.StatusServiceUnavailable, arcJournalUnreadable)
			return
		}
		writePlain(w, http.StatusNotFound, sessionUnseenBody)
		return
	}
	view.Session = &answer
	// Presence is bound AFTER every refusal above (the plan's P5): it decorates a page that was already
	// found and can never make one.
	view.Panes = s.panesFor(id)
	s.render(w, SessionPage(view))
}

// sessionHref is the ONE place a session URL is built: a ledger path plus `url.Values.Encode`, so an
// id can never end the attribute, add a parameter or supply a scheme — `arcHref`'s ruling.
func sessionHref(session string) string {
	return SessionPath + "?" + url.Values{QuerySession: []string{session}}.Encode()
}

// shortID is the first eight bytes of a session id — enough to tell rows apart at a glance; the full
// id is always in the element's `title=`. Byte slicing is safe: the grammar is ASCII.
func shortID(session string) string {
	if len(session) <= 8 {
		return session
	}
	return session[:8]
}

// shortIDsIn is the displayed label for every id in ONE rendered list: the shortest prefix of at
// least eight bytes that no OTHER id in the list shares, so two rows are never labelled alike.
//
// ⚠ THE SAME ID CAN THEREFORE RENDER AT TWO LENGTHS ON TWO PAGES — the label is a property of the
// list it sits in, not of the id. Deterministic for a given list (it depends on the SET, not the
// order). An id that is itself a prefix of another renders in full. The full id is always the
// element's `title=` and the link's operand; the label is never an identifier.
//
// O(N log N): the label length is min(len(id), max(8, L+1)) where L is the longest common prefix
// the id shares with any OTHER distinct id, and over a SORTED, de-duplicated list that maximum is
// always reached at an immediate neighbour. A first draft compared every pair (O(N²): measured
// 0.8–2.7 s at 16,000 ids); `TestShortIDsInMatchesThePairwiseOracle` holds the two to identical
// labels, with the pairwise version kept in the test as the oracle.
func shortIDsIn(ids []string) map[string]string {
	sorted := slices.Clone(ids)
	slices.Sort(sorted)
	sorted = slices.Compact(sorted)
	out := make(map[string]string, len(sorted))
	for i, id := range sorted {
		shared := 0
		if i > 0 {
			shared = commonPrefixLen(id, sorted[i-1])
		}
		if i+1 < len(sorted) {
			shared = max(shared, commonPrefixLen(id, sorted[i+1]))
		}
		out[id] = id[:min(len(id), max(8, shared+1))]
	}
	return out
}

// commonPrefixLen is the length in bytes of the longest common prefix of a and b.
func commonPrefixLen(a, b string) int {
	n := min(len(a), len(b))
	for i := 0; i < n; i++ {
		if a[i] != b[i] {
			return i
		}
	}
	return n
}

// bulletAnchors is each nuance bullet's fragment id on the entry page: `b-<citation id>`, the id
// `internal/touch` records as `BulletRef.CitationID` — so a session page link and the bullet it
// names are keyed by ONE value, computed by ONE function (`store.JournalBullet.CitationID`).
//
// 🔴 A CITATION ID IS NEVER EMPTY BUT IS NOT UNIQUE: it is the first 8 hex of the bullet text's
// SHA-256, so two byte-identical bullets in one entry share it (`touch.WriteEdge`'s own warning). An
// HTML id must be unique, so the FIRST bullet carrying an id gets `b-<cid>` and the n-th repeat gets
// `b-<cid>-<n>` (n = 2, 3, …), in file order. A link names `b-<cid>`, so it lands on the first of the
// identical bullets — which carries the same text, the same date and the same trailer, so nothing a
// reader could see differs. `TestEveryBulletAnchorOnAnEntryPageIsUnique` pins the fallback.
func bulletAnchors(bullets []store.JournalBullet) []string {
	out := make([]string, len(bullets))
	seen := map[string]int{}
	for i, b := range bullets {
		cid := b.CitationID()
		seen[cid]++
		out[i] = bulletAnchor(cid)
		if n := seen[cid]; n > 1 {
			out[i] += "-" + strconv.Itoa(n)
		}
	}
	return out
}

// bulletAnchor is the fragment for a citation id. ONE spelling for the anchor and the link.
func bulletAnchor(cid string) string { return "b-" + cid }

// SessionPage is ONE session: a summary card, then one card per scope it wrote in, newest first.
func SessionPage(v PageView) g.Node {
	answer := *v.Session
	rep := answer.Report
	ids := idsByName(v.Scopes)
	excerpts := bulletExcerpts(v.Scopes)
	first, last := rep.DateSpan()
	actors := rep.Actors()
	coverage := strconv.Itoa(rep.Coverage.Attributed) + " of " + strconv.Itoa(rep.Coverage.Bullets) +
		" bullets carry a write trailer across the " + plural(rep.ScopesScanned, "scope", "scopes") +
		" you can read — a bullet with no trailer names nobody, so this session may have written more"
	var badges []g.Node
	if rep.Partial() {
		badges = append(badges, partialBadge("lower bound", plural(rep.Coverage.EntriesMalformed+
			rep.Coverage.EntriesUnreadable, "entry file", "entry files")+
			" in scopes you can read were never scanned, so a write by this session there is NOT listed"))
	}
	if answer.ArcsUnreadable {
		badges = append(badges, partialBadge("arcs unknown", arcJournalUnreadable))
	}
	// nil — no node, so no bytes — unless the ONE predicate shows this viewer a pane (`presence.go`).
	pane := v.Panes.badge(rep.ID, v.Now)
	return shell("session "+shortID(rep.ID), v, []crumb{{Label: "session " + shortID(rep.ID)}},
		h.Section(
			h.Class("card"),
			h.ID("session-summary"),
			h.H2(h.Class("session-id"), g.Text(rep.ID)),
			// The bell (S5, `bell.go`) rides INSIDE the badge's own `g.If`, so it can never render where
			// the badge does not — and only with a CSRF token to carry (a bearer request with no session
			// cookie has none, and a form it could not submit would be a dead control).
			// ⚠ A `div`, NEVER A `p`: a `<form>` start tag closes an open `<p>`, so a parser would move
			// the bell out of the container and leave a stray empty paragraph behind it.
			g.If(pane != nil, h.Div(h.Class("card-stats"), h.Data("presence", "session"), pane,
				g.If(v.CSRF != "", bellForm(rep.ID, v.CSRF)))),
			h.P(h.Class("card-stats"),
				stat(plural(rep.Bullets(), "bullet", "bullets")+" in "+plural(len(rep.Scopes), "scope", "scopes"), ""),
				stat(strconv.Itoa(rep.Coverage.Attributed)+" of "+strconv.Itoa(rep.Coverage.Bullets)+" bullets attributed",
					joinCaveats(coverage, report.SessionsAttributionLine, report.SessionsReadsLine)),
				g.If(first != "", h.Span(h.Class("stat"), h.TitleAttr("first write"), g.Text("first "), dateAgo(first, v.Now))),
				g.If(last != "", h.Span(h.Class("stat"), h.TitleAttr("last write"), g.Text("last "), dateAgo(last, v.Now))),
				g.If(!rep.ArcsConfigured && !answer.ArcsUnreadable,
					h.Span(h.Class("stat stat-quiet"), h.TitleAttr(report.RegistrationsUnconfiguredBody), g.Text("arcs off"))),
				g.Group(badges),
			),
			g.If(len(actors) > 0, labelledList("Actors", "the <actor> half of each trailer, as written — self-reported",
				h.Ul(h.Class("chips chips-alias"), g.Map(actors, plainItem)))),
			g.If(len(rep.Arcs) > 0, labelledList("Arcs", "registered arcs homed in a scope you can read that list this session as a member",
				h.Ul(h.Class("chips chips-arc"), g.Map(rep.Arcs, func(a report.SessionArc) g.Node {
					return arcChip(a.Home, a.Slug, ids)
				})))),
		),
		g.Map(rep.Scopes, func(sc report.SessionInScope) g.Node {
			return sessionScopeCard(sc, ids[store.NormalizeRef(sc.Scope)], excerpts, v)
		}),
	)
}

// sessionScopeCard is the session's bullets in ONE scope, newest first, each linked to the bullet's
// own anchor on its entry page.
func sessionScopeCard(sc report.SessionInScope, id control.ID, excerpts map[string]string, v PageView) g.Node {
	return h.Section(
		h.Class("card"),
		h.Data("scope", sc.Scope),
		h.Div(h.Class("card-head"),
			h.H2(scopeLink(Scope{ID: id, Name: sc.Scope})),
			h.Span(h.Class("entry-count"), g.Text(plural(len(sc.Session.Bullets), "bullet", "bullets"))),
			dateAgo(sc.Session.LastDate, v.Now),
		),
		h.Ul(h.Class("entry-list"), g.Map(bulletsNewestFirst(sc.Session.Bullets), func(b touch.BulletRef) g.Node {
			href := entryHref(id, b.EntryRef, false) + "#" + bulletAnchor(b.CitationID)
			return h.Li(
				h.Class("entry-row"),
				h.Span(h.Class("ref"), h.TitleAttr(b.EntryRef), h.A(h.Href(href), g.Text(b.EntryRef))),
				excerptSpan(excerpts[excerptKey(sc.Scope, b.EntryRef, b.CitationID)]),
				g.If(b.Date == "", h.Span(h.Class("stat stat-quiet"), g.Text("undated"))),
				dateAgo(b.Date, v.Now),
			)
		})),
	)
}

// excerptSpan renders an excerpt CLAMPED to three lines by CSS (`.excerpt`, `line-clamp`), with the
// whole collapsed text in `title=` — so a long bullet costs three lines of the list and nothing is
// lost, with no script. "" renders an empty span with no tooltip.
func excerptSpan(text string) g.Node {
	return h.Span(h.Class("title excerpt"), g.If(text != "", h.TitleAttr(text)), g.Text(text))
}

// bulletsNewestFirst orders one scope's bullets by date, newest first, undated last, keeping the
// report's edge order within a date. A copy.
func bulletsNewestFirst(in []touch.BulletRef) []touch.BulletRef {
	out := slices.Clone(in)
	slices.SortStableFunc(out, func(a, b touch.BulletRef) int { return strings.Compare(b.Date, a.Date) })
	return out
}

// bulletExcerpts maps (scope, entry ref, citation id) to the bullet's [bulletExcerpt] — its whole body,
// whitespace-collapsed to one line, minus the end-anchored trailer run — read out
// of the SAME narrowed `Visible` answer the page already holds — no second read, and no bullet the
// caller could not already open on its entry page.
func bulletExcerpts(scopes []Scope) map[string]string {
	out := map[string]string{}
	for _, s := range scopes {
		for _, e := range s.Entries {
			for _, sec := range e.Sections {
				if sec.Heading != store.NuanceHeading {
					continue
				}
				for _, b := range sec.Bullets {
					if b.Anchor == "" || len(b.Lines) == 0 {
						continue
					}
					key := excerptKey(s.Name, e.Ref, strings.TrimPrefix(b.Anchor, "b-"))
					if _, dup := out[key]; !dup {
						out[key] = bulletExcerpt(b)
					}
				}
			}
		}
	}
	return out
}

// bulletExcerpt is the bullet as the SESSION PAGE shows it: its body (the date and markers the page
// shows elsewhere already removed by `Bullet.Body`), whitespace-collapsed into one line, with the
// end-anchored `[cairn: actor/session]` trailer run removed by `write.WithoutTrailers` — the page's
// header already says who wrote it. The entry page and the raw view are untouched: they show the
// bullet as the file has it. A `[cairn:` token that is NOT in trailer position is prose and stays.
func bulletExcerpt(b Bullet) string {
	return write.WithoutTrailers(strings.Join(strings.Fields(strings.Join(b.Body(), " ")), " "))
}

func excerptKey(scope, ref, cid string) string {
	return store.NormalizeRef(scope) + "\x00" + ref + "\x00" + cid
}
