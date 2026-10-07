package ui

import (
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ZacxDev/cairn/internal/arcs"
	"github.com/ZacxDev/cairn/internal/control"
	"github.com/ZacxDev/cairn/internal/identity"
	"github.com/ZacxDev/cairn/internal/report"
	"github.com/ZacxDev/cairn/internal/store"
)

// 🔴 S1'S GUARDS (`claudedocs/plan-cairn-arcs-presence.md`): THE ARCS-FIRST PAGE AND THE ARC PAGE'S
// TABS, DRIVEN THROUGH THE REAL DISPATCHER OVER THE REAL `StoreSource`, A STORE ON DISK AND A JOURNAL
// FILE OUTSIDE IT. Every name, session id and date is SYNTHETIC (year 2000) — this repository is
// public. The principals are `arcsWorld`'s: A reads alpha-notes only, W reads alpha and beta.

// idxNow is the server clock for every test here.
var idxNow = time.Date(2000, 3, 1, 12, 0, 0, 0, time.UTC)

// ago is an RFC 3339 instant `d` before idxNow — a registration time.
func ago(d time.Duration) string { return idxNow.Add(-d).Format(time.RFC3339) }

const day = 24 * time.Hour

// idxReg is one registration homed in `home`, registered at `at`, with `members`.
//
// ⚠ EVERY `reported_at` IS THE YEAR 2000'S FIRST SECOND, deliberately far older than every
// registration: a page that read `reported_at` instead of `registered_at` would hide every non-open
// arc, so `TestTheArcsPageListsLiveArcsNewestFirstAndCountsTheHidden` is what kills that mutant.
func idxReg(home, slug, status, at string, members ...string) arcs.Registration {
	var ms []arcs.Member
	for _, m := range members {
		ms = append(ms, arcs.Member{Session: m, Role: "wrote"})
	}
	return arcs.Registration{Schema: arcs.Schema, Home: home, Slug: slug, Status: status,
		ClosingKind: arcs.ClosingCheck, DeclaredScopes: []string{home}, WritersMeasured: true,
		ReportedAt: "2000-01-01T00:00:00Z", Members: ms, RegisteredBy: "fixture-registrar", RegisteredAt: at}
}

// idxStore writes one entry per scope whose nuance section is exactly `bullets[scope]`.
func idxStore(t *testing.T, bullets map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for _, scope := range []string{"alpha-notes", "beta-notes"} {
		dir := filepath.Join(root, scope)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		body := "---\nservice: kiln\nscope: " + scope + "\n---\n\n## What it is\n\nsynthetic.\n\n" +
			store.NuanceHeading + "\n\n" + bullets[scope] + "- 2000-01-01: an unsigned bullet\n"
		if err := os.WriteFile(filepath.Join(dir, "kiln.md"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func idxServer(t *testing.T, src Source, id identity.Identity) *Server {
	t.Helper()
	cfg := testConfig(t, staticAuth{id})
	cfg.Source = src
	cfg.Now = func() time.Time { return idxNow }
	srv, err := New(cfg)
	if err != nil {
		t.Fatalf("the server did not build: %v", err)
	}
	return srv
}

// listedArcs is the arcs page's rows, in rendered order, read off `data-arc`.
func listedArcs(t *testing.T, body string) []string {
	t.Helper()
	var out []string
	for _, m := range regexp.MustCompile(`<li class="entry-row arc-row" data-arc="([^"]*)"`).FindAllStringSubmatch(body, -1) {
		out = append(out, m[1])
	}
	return out
}

// notLive is the page's printed "N not live" count.
func notLive(t *testing.T, body string) string {
	t.Helper()
	m := regexp.MustCompile(`data-count="not-live"[^>]*>([^<]*)<`).FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("the page prints no not-live count:\n%s", body)
	}
	return m[1]
}

func getArcs(t *testing.T, srv *Server, all bool) string {
	t.Helper()
	target := ArcsPath
	if all {
		target += "?all=1"
	}
	rec := getAs(t, srv, target)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s answered %d: %s", target, rec.Code, rec.Body.String())
	}
	return rec.Body.String()
}

// TestTheArcsPageListsLiveArcsNewestFirstAndCountsTheHidden pins decisions 10 and 12 as LITERAL
// expectations. The registration times are pairwise distinct and their order is NOT the slugs'
// alphabetical order, so a page sorted by name (or not sorted) cannot pass; the 14-day boundary is
// measured on BOTH sides (13d23h live, 14d1h hidden); `open` is kept at 400 days; `unknown` at 20
// days is hidden (it is NOT `open`); `?all=1` shows every visible arc; the hidden count is printed.
//
// 🔴 AND THE COUNT IS OVER VISIBLE ARCS ONLY: beta-notes homes a closed, old arc that A cannot see,
// so A's count is 2 where W's is 3 — the relation, with W as the positive control that the beta arc
// is in the journal and old enough to count.
func TestTheArcsPageListsLiveArcsNewestFirstAndCountsTheHidden(t *testing.T) {
	readsA, _, readsW := arcsWorld(t)
	journal := arcsJournal(t,
		idxReg("alpha-notes", "zinc-arc", arcs.StatusClosed, ago(1*time.Hour)),
		idxReg("alpha-notes", "amber-arc", arcs.StatusUnknown, ago(3*day)),
		idxReg("alpha-notes", "cedar-arc", arcs.StatusClosed, ago(13*day+23*time.Hour)),
		idxReg("alpha-notes", "birch-arc", arcs.StatusClosed, ago(14*day+1*time.Hour)),
		idxReg("alpha-notes", "dune-arc", arcs.StatusUnknown, ago(20*day)),
		idxReg("alpha-notes", "moss-arc", arcs.StatusOpen, ago(400*day)),
		idxReg("beta-notes", "shroud-arc", arcs.StatusClosed, ago(90*day)),
	)
	src := StoreSource{Root: idxStore(t, nil), ArcJournal: journal}
	srvA := idxServer(t, src, readsA)

	live := getArcs(t, srvA, false)
	if got, want := listedArcs(t, live), []string{"alpha-notes/zinc-arc", "alpha-notes/amber-arc",
		"alpha-notes/cedar-arc", "alpha-notes/moss-arc"}; !slices.Equal(got, want) {
		t.Errorf("A's live arcs are %v, want %v (newest first; open kept at 400d; 13d23h kept; 14d1h and an "+
			"unknown at 20d hidden)", got, want)
	}
	if got := notLive(t, live); got != "2 not live" {
		t.Errorf("A's page prints %q, want %q — birch (14d1h) and dune (unknown, 20d), and NOT beta's arc", got, "2 not live")
	}
	if !strings.Contains(live, `href="/arcs?all=1"`) {
		t.Error("the live view does not offer the show-all toggle")
	}

	all := getArcs(t, srvA, true)
	if got, want := listedArcs(t, all), []string{"alpha-notes/zinc-arc", "alpha-notes/amber-arc",
		"alpha-notes/cedar-arc", "alpha-notes/birch-arc", "alpha-notes/dune-arc", "alpha-notes/moss-arc"}; !slices.Equal(got, want) {
		t.Errorf("A's ?all=1 arcs are %v, want %v", got, want)
	}
	if !strings.Contains(all, `href="/arcs"`) {
		t.Error("the show-all view does not offer the way back to live only")
	}
	// Any other value of `all` is the default view, never a 400.
	if other := getAs(t, srvA, ArcsPath+"?all=yes"); other.Code != http.StatusOK || other.Body.String() != live {
		t.Errorf("?all=yes answered %d and differs from the live view; only `1` is recognised", other.Code)
	}

	// W reads beta too: the same arcs plus beta's hidden one.
	wide := getArcs(t, idxServer(t, src, readsW), false)
	if got := notLive(t, wide); got != "3 not live" {
		t.Errorf("POSITIVE CONTROL: W's page prints %q, want %q — beta's closed old arc counts for W", got, "3 not live")
	}
	for _, page := range []string{live, all} {
		if strings.Contains(page, "shroud-arc") {
			t.Error("A's arcs page names an arc homed in beta-notes, which A cannot read")
		}
	}
	// The page is reachable from the frame of every page.
	if !strings.Contains(live, `<p class="nav-arcs"><a href="/arcs">Arcs</a></p>`) {
		t.Error("the header carries no link to the arcs page")
	}
}

// TestAnArcHomedInAnUnreadableScopeIsNeverListedEvenWhenItsMembersWroteWhereYouRead is the S1
// negative control: beta's arc has a member that wrote in ALPHA, today, and is OPEN — every reason to
// list it except the one rule that decides. A sees no trace of it anywhere on either view, and its
// counts are what they would be with no such arc; W, who reads beta, sees it (positive control).
func TestAnArcHomedInAnUnreadableScopeIsNeverListedEvenWhenItsMembersWroteWhereYouRead(t *testing.T) {
	readsA, _, readsW := arcsWorld(t)
	root := idxStore(t, map[string]string{"alpha-notes": "- 2000-03-01: today [cairn: kiln-bot/s-ember-0001]\n"})
	with := StoreSource{Root: root, ArcJournal: arcsJournal(t,
		idxReg("alpha-notes", "own-arc", arcs.StatusClosed, ago(60*day)),
		idxReg("beta-notes", "veil-arc", arcs.StatusOpen, ago(1*time.Hour), "s-ember-0001"))}
	without := StoreSource{Root: root, ArcJournal: arcsJournal(t,
		idxReg("alpha-notes", "own-arc", arcs.StatusClosed, ago(60*day)))}

	for _, all := range []bool{false, true} {
		a := getArcs(t, idxServer(t, with, readsA), all)
		baseline := getArcs(t, idxServer(t, without, readsA), all)
		if a != baseline {
			t.Errorf("all=%v: A's page with beta's arc in the journal DIFFERS from the page without it — any "+
				"difference is an oracle over arcs homed in a scope A cannot read", all)
		}
		for _, leak := range []string{"veil-arc", "beta-notes"} {
			if strings.Contains(a, leak) {
				t.Errorf("all=%v: A's arcs page names %q", all, leak)
			}
		}
	}
	if got := listedArcs(t, getArcs(t, idxServer(t, with, readsW), false)); !slices.Contains(got, "beta-notes/veil-arc") {
		t.Fatalf("POSITIVE CONTROL FAILED: W's live arcs %v omit beta's open arc, so A's absence proves nothing", got)
	}
}

// TestAMemberBulletInAnUnreadableScopeDoesNotMoveTheArc is the relationship the plan names: TWO
// viewers, ONE store. alpha's closed arc was registered 30 days ago; its member's only bullet is in
// BETA, two days ago. For W (reads beta) the bullet wins — the arc is live and says "bullet"; for A
// the same arc is not live and its row says "registered", because a write A cannot read never
// reaches A's walk.
func TestAMemberBulletInAnUnreadableScopeDoesNotMoveTheArc(t *testing.T) {
	readsA, _, readsW := arcsWorld(t)
	src := StoreSource{
		Root:       idxStore(t, map[string]string{"beta-notes": "- 2000-02-28: poured [cairn: kiln-bot/s-wick-0002]\n"}),
		ArcJournal: arcsJournal(t, idxReg("alpha-notes", "lamp-arc", arcs.StatusClosed, ago(30*day), "s-wick-0002")),
	}
	w := getArcs(t, idxServer(t, src, readsW), false)
	if got := listedArcs(t, w); !slices.Equal(got, []string{"alpha-notes/lamp-arc"}) {
		t.Fatalf("POSITIVE CONTROL FAILED: W's live arcs are %v — the beta bullet should keep lamp-arc live for W", got)
	}
	if !strings.Contains(w, `data-updated-by="bullet"`) {
		t.Error("W's row does not say a member's bullet updated it")
	}
	a := getArcs(t, idxServer(t, src, readsA), false)
	if got := listedArcs(t, a); len(got) != 0 {
		t.Errorf("A's live arcs are %v — a member's bullet in beta-notes, which A cannot read, moved the arc for A", got)
	}
	if aAll := getArcs(t, idxServer(t, src, readsA), true); !strings.Contains(aAll, `data-updated-by="registration"`) ||
		strings.Contains(aAll, `data-updated-by="bullet"`) {
		t.Error("A's show-all row is not dated by its registration alone")
	}
}

// TestAFutureDatedBulletDoesNotSortAboveToday: a bullet's date is the WRITER's word on a `put`, so a
// member can date one at the end of the year. Clamped to today 00:00, it sorts BELOW an arc
// registered an hour ago (11:00 today) and its row says "bullet today" — never the future date.
func TestAFutureDatedBulletDoesNotSortAboveToday(t *testing.T) {
	readsA, _, _ := arcsWorld(t)
	src := StoreSource{
		Root: idxStore(t, map[string]string{"alpha-notes": "- 2000-12-31: from the future [cairn: kiln-bot/s-ember-0001]\n"}),
		ArcJournal: arcsJournal(t,
			idxReg("alpha-notes", "aster-arc", arcs.StatusClosed, ago(90*day), "s-ember-0001"),
			idxReg("alpha-notes", "yarrow-arc", arcs.StatusClosed, ago(1*time.Hour))),
	}
	page := getArcs(t, idxServer(t, src, readsA), false)
	if got, want := listedArcs(t, page), []string{"alpha-notes/yarrow-arc", "alpha-notes/aster-arc"}; !slices.Equal(got, want) {
		t.Errorf("live arcs are %v, want %v — a future-dated bullet must count as today 00:00, below 11:00 today", got, want)
	}
	if strings.Contains(page, "2000-12-31") {
		t.Error("the page shows the future date rather than the clamped one")
	}
	if !strings.Contains(page, `<time class="updated" datetime="2000-03-01" title="2000-03-01">today</time>`) {
		t.Error("the clamped bullet is not rendered as today")
	}
}

// TestABulletDatedExactlyFourteenDaysAgoIsStillLive measures the window on the BULLET path at both
// sides, by whole UTC dates: a bullet carries a date and no time of day, so "within 14 days" for it
// means its date is on or after today−14. The clock is 12:00, so an instant comparison against the
// bullet's 00:00 would put today−14 at 14d12h and hide it while its row says "14d ago".
func TestABulletDatedExactlyFourteenDaysAgoIsStillLive(t *testing.T) {
	readsA, _, _ := arcsWorld(t)
	src := StoreSource{
		Root: idxStore(t, map[string]string{"alpha-notes": "- 2000-02-16: day fourteen [cairn: kiln-bot/s-ember-0001]\n" +
			"- 2000-02-15: day fifteen [cairn: kiln-bot/s-wick-0002]\n"}),
		ArcJournal: arcsJournal(t,
			idxReg("alpha-notes", "edge-arc", arcs.StatusClosed, ago(60*day), "s-ember-0001"),
			idxReg("alpha-notes", "past-arc", arcs.StatusClosed, ago(60*day), "s-wick-0002")),
	}
	page := getArcs(t, idxServer(t, src, readsA), false)
	if got, want := listedArcs(t, page), []string{"alpha-notes/edge-arc"}; !slices.Equal(got, want) {
		t.Errorf("live arcs are %v, want %v — a bullet dated today−14 (2000-02-16) is within 14 days; today−15 is not", got, want)
	}
	if all := getArcs(t, idxServer(t, src, readsA), true); !strings.Contains(all, ">14d ago</time>") || !strings.Contains(all, ">15d ago</time>") {
		t.Error("INSTRUMENT: the edge row does not say 14d ago, so the boundary measured is not the one a reader sees")
	}
}

// TestABulletNamingAMemberByANonMemberKeepsTheArcLive is an INVARIANT GUARD — a TRIPWIRE, not
// regression coverage: no defect ever violated it. It pins operator decision O1's ACCEPTED COST so
// nobody "fixes" it silently: a trailer's session is self-declared, and `report.ArcsAcross` keys on
// the session id and never reads the actor, so a bullet written under ANOTHER actor that NAMES a
// member session moves the arc. The control is the same store without that bullet, where the arc is
// not live. If it goes red, that is a decision to record, not a bug fixed.
func TestABulletNamingAMemberByANonMemberKeepsTheArcLive(t *testing.T) {
	readsA, _, _ := arcsWorld(t)
	journal := arcsJournal(t, idxReg("alpha-notes", "fern-arc", arcs.StatusClosed, ago(45*day), "s-ember-0001"))
	quiet := StoreSource{Root: idxStore(t, nil), ArcJournal: journal}
	if got := listedArcs(t, getArcs(t, idxServer(t, quiet, readsA), false)); len(got) != 0 {
		t.Fatalf("CONTROL: with no member bullet fern-arc should not be live, got %v", got)
	}
	nudged := StoreSource{
		Root:       idxStore(t, map[string]string{"alpha-notes": "- 2000-02-29: a stranger's note [cairn: stranger-bot/s-ember-0001]\n"}),
		ArcJournal: journal,
	}
	page := getArcs(t, idxServer(t, nudged, readsA), false)
	if got := listedArcs(t, page); !slices.Equal(got, []string{"alpha-notes/fern-arc"}) {
		t.Errorf("a bullet naming member s-ember-0001 under another actor did NOT keep fern-arc live (got %v). "+
			"Operator decision O1 ACCEPTED this cost; if it is being removed, that is a decision to record, not a fix.", got)
	}
}

// TestTheArcsPageOffAndBrokenStates: no journal is a 200 page saying so; a configured journal that
// cannot be read is a 503 — "could not look", never "no arc".
func TestTheArcsPageOffAndBrokenStates(t *testing.T) {
	readsA, _, _ := arcsWorld(t)
	off := getAs(t, idxServer(t, StoreSource{Root: idxStore(t, nil)}, readsA), ArcsPath)
	if off.Code != http.StatusOK || !strings.Contains(off.Body.String(), arcsOff) {
		t.Errorf("the unconfigured arcs page answered %d without the off sentence: %s", off.Code, off.Body.String())
	}
	dir := filepath.Join(t.TempDir(), "journal-is-a-directory")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	broken := getAs(t, idxServer(t, StoreSource{Root: idxStore(t, nil), ArcJournal: dir}, readsA), ArcsPath)
	if broken.Code != http.StatusServiceUnavailable || broken.Body.String() != arcJournalUnreadable {
		t.Errorf("an unreadable journal answered %d %q, want 503 %q", broken.Code, broken.Body.String(), arcJournalUnreadable)
	}
}

// TestTheArcsPageEscapesAPlantedHostileHomeAndSlug renders the page directly over a REPORT VALUE
// carrying markup (`hostileWorld`'s ruling: the journal's validator refuses such a slug, and a page
// whose safety depended on that would be one new writer away from broken). Every arc href must
// round-trip both operands exactly.
func TestTheArcsPageEscapesAPlantedHostileHomeAndSlug(t *testing.T) {
	hostileHome := `n"><img src=x onerror=y>`
	hostileSlug := `slug"<b>&home=evil#z`
	rep := report.ArcsAcrossReport{Configured: true, Arcs: []report.ArcAcross{{
		Home: hostileHome, Slug: hostileSlug, Status: arcs.StatusOpen,
		DeclaredVisible: []string{hostileHome}, RegisteredAt: ago(time.Hour), ReportedAt: `r"<i>`,
	}}}
	view := PageView{Viewer: "v", Now: idxNow, Scopes: []Scope{{ID: "scp_planted", Name: hostileHome}}, ArcsIndex: &rep}
	var b strings.Builder
	if err := ArcsIndexPage(view).Render(&b); err != nil {
		t.Fatal(err)
	}
	page := b.String()
	for _, bad := range []string{"<img", "<b>", "<i>", `href="javascript:`} {
		if strings.Contains(page, bad) {
			t.Errorf("the arcs page contains %q unescaped", bad)
		}
	}
	arcLinks := 0
	for _, m := range regexp.MustCompile(`href="([^"]*)"`).FindAllStringSubmatch(page, -1) {
		raw := strings.ReplaceAll(m[1], "&amp;", "&")
		u, err := url.Parse(raw)
		if err != nil || u.Scheme != "" || u.Host != "" {
			t.Errorf("href %q is not a same-origin path", raw)
			continue
		}
		if u.Path == ArcPath {
			arcLinks++
			if q := u.Query(); len(q) != 2 || q.Get(QueryHome) != "scp_planted" || q.Get(QuerySlug) != hostileSlug {
				t.Errorf("arc href %q does not round-trip (got %v)", raw, q)
			}
		}
	}
	if arcLinks != 1 {
		t.Fatalf("INSTRUMENT: %d arc link(s) rendered, want 1 — the round-trip check measured nothing", arcLinks)
	}
}

// arcPageWorld is one arc homed in alpha with two members, one of whom wrote in alpha and one in
// beta, and a declaration of beta too — so the scopes tab has a declared row, an inferred row (for
// W) and a hidden one (for A).
func arcPageWorld(t *testing.T) StoreSource {
	t.Helper()
	reg := idxReg("alpha-notes", "loom-arc", arcs.StatusOpen, ago(2*time.Hour), "s-ember-0001", "s-wick-0002")
	reg.DeclaredScopes = []string{"alpha-notes", "beta-notes"}
	return StoreSource{
		Root: idxStore(t, map[string]string{
			"alpha-notes": "- 2000-02-20: wove [cairn: kiln-bot/s-ember-0001]\n",
			"beta-notes":  "- 2000-02-21: spun [cairn: kiln-bot/s-wick-0002]\n",
		}),
		ArcJournal: arcsJournal(t, reg),
	}
}

// TestTheArcPageRendersOnlyTheSelectedTab: `/arc` has exactly two tabs, scopes (the default) and
// sessions; only the selected panel is in the document; the current tab is a `<span>`; scope rows
// link `/scope`, member rows link `/session`; an unknown `tab` — including the SCOPE page's tab
// names — renders the default tab byte for byte.
func TestTheArcPageRendersOnlyTheSelectedTab(t *testing.T) {
	readsA, _, _ := arcsWorld(t)
	srv := idxServer(t, arcPageWorld(t), readsA)

	scopes := getAs(t, srv, arcURL(browseScopeA, "loom-arc"))
	if scopes.Code != http.StatusOK {
		t.Fatalf("the arc page answered %d: %s", scopes.Code, scopes.Body.String())
	}
	sp := scopes.Body.String()
	if !strings.Contains(sp, `id="arc-scopes"`) || strings.Contains(sp, `id="arc-sessions"`) {
		t.Error("the default tab is not the scopes panel alone")
	}
	if !strings.Contains(sp, `<span class="view-tab view-tab-here" data-tab="scopes">Scopes 1</span>`) {
		t.Error("the scopes tab is not the current tab, or its count is not A's one readable scope")
	}
	if !strings.Contains(sp, `href="`+inAttr(scopeHref(Scope{ID: browseScopeA, Name: "alpha-notes"}))+`"`) {
		t.Error("the scopes panel does not link alpha-notes' scope page")
	}
	if strings.Contains(sp, `href="/session?`) {
		t.Error("the scopes panel links a session page — only the selected panel may render")
	}
	if !strings.Contains(sp, `href="`+inAttr(arcTabURL(browseScopeA, "loom-arc", TabSessions))+`"`) {
		t.Error("the scopes tab does not link the sessions tab")
	}

	sessions := getAs(t, srv, arcTabURL(browseScopeA, "loom-arc", TabSessions)).Body.String()
	if !strings.Contains(sessions, `id="arc-sessions"`) || strings.Contains(sessions, `id="arc-scopes"`) {
		t.Error("?tab=sessions is not the sessions panel alone")
	}
	if !strings.Contains(sessions, `<span class="view-tab view-tab-here" data-tab="sessions">Sessions 2</span>`) {
		t.Error("the sessions tab is not the current tab, or its count is not the two members")
	}
	for _, m := range []string{"s-ember-0001", "s-wick-0002"} {
		if !strings.Contains(sessions, `href="`+inAttr(sessionHref(m))+`"`) {
			t.Errorf("the sessions panel does not link member %s's session page", m)
		}
	}
	if strings.Contains(sessions, `data-scope="alpha-notes"`) {
		t.Error("the sessions panel renders the scopes panel's rows")
	}

	for _, unknown := range []string{"entries", "arcs", "SESSIONS", "sessions ", "x"} {
		got := getAs(t, srv, arcTabURL(browseScopeA, "loom-arc", unknown))
		if got.Code != http.StatusOK || got.Body.String() != sp {
			t.Errorf("?tab=%q answered %d and is not byte-identical to the default tab", unknown, got.Code)
		}
	}
}

// TestTheArcScopesTabNarrowsAndMarksProvenance: for A, beta-notes (declared, and written in by a
// member) is NEVER named; for W it is a declared row and alpha-notes a declared one too, each with
// its member count. The relation, with W as the positive control.
func TestTheArcScopesTabNarrowsAndMarksProvenance(t *testing.T) {
	readsA, _, readsW := arcsWorld(t)
	src := arcPageWorld(t)
	a := getAs(t, idxServer(t, src, readsA), arcURL(browseScopeA, "loom-arc")).Body.String()
	if strings.Contains(a, "beta-notes") {
		t.Error("A's arc page names beta-notes, which A cannot read")
	}
	w := getAs(t, idxServer(t, src, readsW), arcURL(browseScopeA, "loom-arc")).Body.String()
	for _, sc := range []string{"alpha-notes", "beta-notes"} {
		if !strings.Contains(w, `data-scope="`+sc+`"`) {
			t.Errorf("POSITIVE CONTROL FAILED: W's scopes tab has no row for %s", sc)
		}
	}
	if !strings.Contains(w, `>Scopes 2</span>`) {
		t.Error("W's scopes tab count is not 2")
	}
}

// TestEveryArcMissIsTheSameBytesOnEveryTab: the tab is read AFTER the refusal, so `?tab=` can never
// distinguish an arc homed in an unreadable scope from one never registered.
func TestEveryArcMissIsTheSameBytesOnEveryTab(t *testing.T) {
	readsA, _, readsW := arcsWorld(t)
	reg := idxReg("beta-notes", "veil-arc", arcs.StatusOpen, ago(time.Hour), "s-wick-0002")
	src := StoreSource{Root: idxStore(t, nil), ArcJournal: arcsJournal(t, reg)}
	srv := idxServer(t, src, readsA)
	for _, tab := range []string{"", TabSessions, "bogus"} {
		for name, target := range map[string]string{
			"hidden home":      arcTabURL(browseScopeB, "veil-arc", tab),
			"never registered": arcTabURL(browseScopeA, "never-registered-anywhere", tab),
		} {
			rec := getAs(t, srv, target)
			if rec.Code != http.StatusNotFound || rec.Body.String() != report.ArcUnregisteredBody {
				t.Errorf("tab=%q, %s: answered %d %q, want 404 with the arc-unregistered body", tab, name, rec.Code, rec.Body.String())
			}
		}
	}
	if rec := getAs(t, idxServer(t, src, readsW), arcTabURL(browseScopeB, "veil-arc", TabSessions)); rec.Code != http.StatusOK {
		t.Fatalf("POSITIVE CONTROL FAILED: W's sessions tab for veil-arc answered %d", rec.Code)
	}
}

// TestTheArcTabHrefsEscapeAPlantedHostileSlug: the tab strip's links round-trip a hostile slug and
// home id, rendered directly over a report value.
func TestTheArcTabHrefsEscapeAPlantedHostileSlug(t *testing.T) {
	hostileSlug := `slug"<b>&tab=x#z`
	home := control.ID(`scp_"<i>`)
	var b strings.Builder
	if err := arcTabs(home, hostileSlug, "", 1, 1).Render(&b); err != nil {
		t.Fatal(err)
	}
	page := b.String()
	if strings.Contains(page, "<b>") || strings.Contains(page, "<i>") {
		t.Errorf("the tab strip carries markup unescaped: %s", page)
	}
	m := regexp.MustCompile(`href="([^"]*)"`).FindAllStringSubmatch(page, -1)
	if len(m) != 1 {
		t.Fatalf("INSTRUMENT: %d tab link(s), want 1 (the current tab is a span)", len(m))
	}
	u, err := url.Parse(strings.ReplaceAll(m[0][1], "&amp;", "&"))
	if err != nil || u.Path != ArcPath {
		t.Fatalf("the tab href %q does not parse as %s", m[0][1], ArcPath)
	}
	if q := u.Query(); len(q) != 3 || q.Get(QuerySlug) != hostileSlug || q.Get(QueryHome) != string(home) || q.Get(QueryTab) != TabSessions {
		t.Errorf("the tab href does not round-trip: %v", q)
	}
}
