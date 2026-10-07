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
)

// 🔴 THE ARCS-FIRST PAGE: EVERY ARC THIS CALLER CAN SEE, LIVE ONES FIRST. `GET /arcs` — slice S1 of
// `claudedocs/plan-cairn-arcs-presence.md` (decisions 10, 12, 13; operator decisions O1, O2). It adds
// NO visibility rule: the arcs come from `report.ArcsAcross`, handed the ONE narrowing `Visible`,
// `Touched`, `Arc` and `Session` use, and that function applies the arc rule (home readable, Q1).
// What THIS file decides is what needs the reader's clock, which `internal/report` deliberately does
// not hold:
//
//   - 🔴 LAST UPDATED = max(the latest registration's pod-clock `registered_at`, the newest attributed
//     bullet any member session wrote in a scope this caller can read). The bullet's date is the
//     WRITER'S word on a `put`, so it is CLAMPED TO TODAY first and taken as that day's 00:00 UTC —
//     a future date cannot sort an arc above today's. `reported_at` is the tooling's own clock and
//     optional, so it is shown in the tooltip and NEVER used ([arcLastUpdated]).
//   - 🔴 LIVE = status `open` OR last updated at most 14 WHOLE DAYS ago — the same truncation the row's
//     "Nd ago" label uses, so "14d ago" is live and "15d ago" is not, whichever source won.
//     `unknown` is NOT `open` (Q4 of the arcs plan): an `unknown` arc is live only by recency
//     ([arcIsLive]).
//   - newest first; ties by (home, slug), byte-wise. `?all=1` lists every visible arc; otherwise the
//     page lists the live ones and PRINTS HOW MANY IT HID — a count of visible arcs only, so it can
//     never reveal an arc homed in a scope the caller cannot read.
//
// 🔴 THE ATTRIBUTION COST IS ACCEPTED, NOT OVERLOOKED (O1). A trailer's session is self-declared, so
// anybody who can write a readable scope can keep an arc live by appending a bullet naming a member
// session. It reorders what the reader could already see; it never makes an arc visible.
// `TestABulletNamingAMemberByANonMemberKeepsTheArcLive` is a TRIPWIRE pinning it (an invariant guard,
// not regression coverage) so nobody "fixes" it silently.

// arcLiveDays is how recently an arc must have been updated to be live when it is not `open`, in
// WHOLE days ago ([arcIsLive]): inclusive, so an arc whose row reads "14d ago" is live.
const arcLiveDays = 14

// QueryAll is the arcs page's show-all toggle: `?all=1` lists every visible arc. Exactly one value is
// recognised, [QueryView]'s ruling — any other value is the default (live-only) page, never a 400.
const QueryAll = "all"

// Arcs answers the arcs page through the caller's visible set. An unreadable journal is returned as
// the `*arcs.JournalUnreadableError` it is, so the handler can answer 503 — "could not look".
func (s StoreSource) Arcs(auth control.Authorization) (report.ArcsAcrossReport, error) {
	visible := scopeSetOf(auth.NamedScopes(control.VerbRead))
	snap, err := s.arcSnapshot()
	if err != nil {
		return report.ArcsAcrossReport{}, err
	}
	return report.ArcsAcross(s.Root, visible, snap)
}

// arcActivity is one arc's "last updated" and which source won.
type arcActivity struct {
	At time.Time
	// FromBullet is true when a member's bullet is newer than the registration; BulletDate is then
	// the CLAMPED `YYYY-MM-DD` the row shows.
	FromBullet bool
	BulletDate string
}

// arcLastUpdated is decision 10: max(registration, clamped newest member bullet). A tie goes to the
// registration, which carries a time of day where the bullet carries only a date. `now` is always a
// real clock — `New` installs one — so the clamp has no off switch.
func arcLastUpdated(a report.ArcAcross, now time.Time) arcActivity {
	var out arcActivity
	if at, err := time.Parse(time.RFC3339, a.RegisteredAt); err == nil {
		out.At = at.UTC()
	}
	if a.NewestMemberBullet == "" {
		return out
	}
	day, err := time.Parse("2006-01-02", a.NewestMemberBullet)
	if err != nil {
		return out
	}
	if today := utcDay(now); day.After(today) {
		day = today
	}
	if day.After(out.At) {
		out = arcActivity{At: day, FromBullet: true, BulletDate: day.Format("2006-01-02")}
	}
	return out
}

// utcDay is `t`'s calendar day, as that day's 00:00 UTC.
func utcDay(t time.Time) time.Time {
	t = t.UTC()
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

// arcIsLive is decision 12: `open`, or last updated at most [arcLiveDays] WHOLE days ago. `unknown`
// is NOT `open`.
//
// 🔴 ONE RULE, OVER THE WINNING INSTANT, AT THE LABEL'S OWN TRUNCATION — and two earlier shapes were
// measured wrong. (1) `now − last ≤ 14×24h` hid a bullet dated today−14 (its 00:00 is 14d12h before a
// noon clock) while its row read "14d ago", and hid a registration 14d1h old under the same label.
// (2) A per-source rule (bullet by whole dates, registration by instant) let an arc with STRICTLY
// NEWER activity be hidden while an older one was live, because liveness then depended on which
// source won (`TestLivenessDoesNotDependOnWhichSourceWon`). So: whole days = ⌊(now − last) / 24h⌋,
// exactly what `relativeTime` prints and, for a bullet at 00:00, exactly `daysAgo`'s calendar count —
// "Nd ago" with N ≤ 14 ⇔ live. A last-update in the future (a skewed pod clock) is 0 days: live.
func arcIsLive(a report.ArcAcross, act arcActivity, now time.Time) bool {
	if a.Status == arcs.StatusOpen {
		return true
	}
	return int(now.Sub(act.At)/(24*time.Hour)) <= arcLiveDays
}

// arcIndexRow is one arc with its fold applied.
type arcIndexRow struct {
	Arc      report.ArcAcross
	Activity arcActivity
	Live     bool
}

// arcsIndexRows folds the report into rows, newest first (ties by home, then slug), and splits off
// the hidden count. With `all` every row is returned and hidden is still the number the live filter
// WOULD hide, so the page can say what the toggle changed.
func arcsIndexRows(rep report.ArcsAcrossReport, now time.Time, all bool) (rows []arcIndexRow, hidden int) {
	for _, a := range rep.Arcs {
		act := arcLastUpdated(a, now)
		live := arcIsLive(a, act, now)
		if !live {
			hidden++
		}
		if live || all {
			rows = append(rows, arcIndexRow{Arc: a, Activity: act, Live: live})
		}
	}
	slices.SortStableFunc(rows, func(x, y arcIndexRow) int {
		if c := y.Activity.At.Compare(x.Activity.At); c != 0 {
			return c
		}
		if c := strings.Compare(x.Arc.Home, y.Arc.Home); c != 0 {
			return c
		}
		return strings.Compare(x.Arc.Slug, y.Arc.Slug)
	})
	return rows, hidden
}

// arcsIndexHref is the ONE place an arcs-page URL is built: the plain path for the live view, and
// `?all=1` for the full one — so each state has exactly one canonical URL (`scopeTabHref`'s rule).
func arcsIndexHref(all bool) string {
	if !all {
		return ArcsPath
	}
	return ArcsPath + "?" + url.Values{QueryAll: []string{"1"}}.Encode()
}

// handleArcsPage renders every arc this caller can see.
//
// ⚠ `Visible` IS ASKED FIRST, for the same two reasons every browse page asks it: the shell's
// navigation is built from it, and the rows link each arc by its home's `control.ID`, which only the
// narrowed answer carries. The arcs themselves come from `Source.Arcs`, narrowed by the same
// authority.
//
// ⚠ AN UNCONFIGURED JOURNAL IS A 200 PAGE SAYING SO (the arc page's ruling); A CONFIGURED ONE THAT
// CANNOT BE READ IS A 503 — "could not look", never "no arc".
func (s *Server) handleArcsPage(w http.ResponseWriter, r *http.Request, id identity.Identity) {
	scopes, err := s.source.Visible(id.Auth)
	if err != nil {
		writePlain(w, http.StatusInternalServerError, "the store could not be read")
		return
	}
	rep, err := s.source.Arcs(id.Auth)
	if err != nil {
		var unreadable *arcs.JournalUnreadableError
		if errors.As(err, &unreadable) {
			writePlain(w, http.StatusServiceUnavailable, arcJournalUnreadable)
			return
		}
		writePlain(w, http.StatusInternalServerError, "the store could not be read")
		return
	}
	view := PageView{Viewer: id.Principal.Display, CSRF: csrfTokenFor(r), Scopes: scopes, Now: s.now()}
	view.ArcsIndex = &rep
	view.ArcsAll = r.URL.Query().Get(QueryAll) == "1"
	s.render(w, ArcsIndexPage(view))
}

// arcsLiveRule is the visible statement of the live filter, beside the count it explains.
const arcsLiveRule = "live = open, or last updated 14 or fewer whole days ago (\"14d ago\" is live, \"15d ago\" is not)"

// arcsUpdatedTip is the tooltip on every row's "last updated" — which two times were compared.
const arcsUpdatedTip = "last updated: the newer of the latest registration (pod clock) and the newest " +
	"attributed bullet a member session wrote in a scope you can read (a future date counts as today)"

// ArcsIndexPage is the arcs-first page.
func ArcsIndexPage(v PageView) g.Node {
	rep := *v.ArcsIndex
	crumbs := []crumb{{Label: "arcs"}}
	if !rep.Configured {
		return shell("cairn — arcs", v, crumbs,
			h.Section(h.Class("card"), h.ID("arcs-index"),
				h.H2(g.Text("Arcs")),
				h.P(h.Class("empty"), h.TitleAttr(report.RegistrationsUnconfiguredBody), g.Text(arcsOff)),
			),
		)
	}
	rows, hidden := arcsIndexRows(rep, v.Now, v.ArcsAll)
	ids := idsByName(v.Scopes)
	var badges []g.Node
	if rep.Damaged {
		badges = append(badges, partialBadge("lower bound", report.ArcsDamagedLine))
	}
	if rep.Partial() {
		badges = append(badges, partialBadge("lower bound", plural(rep.Coverage.EntriesMalformed+
			rep.Coverage.EntriesUnreadable, "entry file", "entry files")+
			" in scopes you can read were never scanned, so a member's newer bullet there is NOT counted"))
	}
	// The toggle is the one LINK in the stats row, so it borrows the accent stat's colour: a link
	// styled as muted metadata is `.nav-share`'s defect again.
	var toggle g.Node
	if v.ArcsAll {
		toggle = h.A(h.Class("stat stat-open"), h.Data("toggle", "live"), h.Href(arcsIndexHref(false)), g.Text("live only"))
	} else if hidden > 0 {
		toggle = h.A(h.Class("stat stat-open"), h.Data("toggle", "all"), h.Href(arcsIndexHref(true)), g.Text("show all "+strconv.Itoa(len(rep.Arcs))))
	}
	countText := plural(len(rows), "live arc", "live arcs")
	if v.ArcsAll {
		countText = plural(len(rows), "arc", "arcs")
	}
	return shell("cairn — arcs", v, crumbs,
		h.Section(
			h.Class("card"),
			h.ID("arcs-index"),
			h.H2(g.Text("Arcs")),
			h.P(h.Class("card-stats"),
				stat(countText, joinCaveats(arcsLiveRule, report.ArcsVisibilityLine, report.ArcsRegistrationLine)),
				h.Span(h.Class("stat"), h.Data("count", "not-live"), h.TitleAttr(arcsLiveRule),
					g.Text(strconv.Itoa(hidden)+" not live")),
				toggle,
				attributedStat(rep.Coverage, joinCaveats(report.SessionsAttributionLine, report.SessionsReadsLine)),
				g.Group(badges),
			),
			g.If(len(rows) == 0, h.P(h.Class("empty"), g.Text(arcsIndexEmpty(len(rep.Arcs), v.ArcsAll)))),
			g.If(len(rows) > 0, h.Ul(h.Class("entry-list"), h.ID("arcs-list"), g.Map(rows, func(row arcIndexRow) g.Node {
				return arcsIndexRow(row, ids, v.Now)
			}))),
		),
	)
}

// arcsIndexEmpty is the empty-list sentence: "nothing visible" and "nothing live" are different facts.
func arcsIndexEmpty(visible int, all bool) string {
	if visible == 0 || all {
		return "No registered arc is homed in a scope you can read."
	}
	return "No arc you can see is live — none is open or was updated in the last 14 days."
}

// arcsIndexRow is one arc on the arcs page.
//
// 🔴 THE STATUS IS `report.StatusWord` IN ONE SHARED BADGE CLASS, `arcRow`'s ruling (Q4): `unknown`
// is never styled or worded as `open`.
func arcsIndexRow(row arcIndexRow, ids map[string]control.ID, now time.Time) g.Node {
	a := row.Arc
	label := a.Home + "/" + a.Slug
	var name g.Node = h.Span(g.Text(label))
	if home := ids[store.NormalizeRef(a.Home)]; home != "" {
		name = h.A(h.Href(arcHref(home, a.Slug)), g.Text(label))
	}
	statusTip := "status"
	if a.Status == arcs.StatusUnknown {
		statusTip = report.UnknownStatusGloss
	}
	updatedTip := arcsUpdatedTip
	if a.ReportedAt != "" {
		updatedTip += "\nthe tooling reported at " + a.ReportedAt + " (its own clock; not used)"
	}
	var when g.Node
	if row.Activity.FromBullet {
		when = h.Span(h.Class("entry-count"), h.Data("updated-by", "bullet"), h.TitleAttr(updatedTip),
			g.Text("bullet "), dateAgo(row.Activity.BulletDate, now))
	} else {
		when = h.Span(h.Class("entry-count"), h.Data("updated-by", "registration"), h.TitleAttr(updatedTip),
			g.Text("registered "), instantAgo(a.RegisteredAt, now))
	}
	return h.Li(
		h.Class("entry-row arc-row"),
		h.Data("arc", label),
		g.If(!row.Live, h.Data("live", "false")),
		h.Span(h.Class("ref"), h.TitleAttr(label), name),
		h.Span(h.Class("badge"), h.TitleAttr(statusTip), g.Text(report.StatusWord(a.Status))),
		when,
		h.Span(h.Class("entry-count"), g.Text(plural(a.Members, "member", "members"))),
		g.If(len(a.DeclaredVisible) > 0, h.Ul(h.Class("chips chips-scope"), h.TitleAttr("declared scopes you can read"),
			g.Map(a.DeclaredVisible, func(n string) g.Node { return scopeChip(n, ids) }))),
	)
}
