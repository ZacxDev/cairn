package ui

import (
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/ZacxDev/cairn/internal/arcs"
	"github.com/ZacxDev/cairn/internal/control"
	"github.com/ZacxDev/cairn/internal/identity"
	"github.com/ZacxDev/cairn/internal/report"
	"github.com/ZacxDev/cairn/internal/store"
	"github.com/ZacxDev/cairn/internal/touch"
)

// sessionWith is a one-bullet `touch.Session` for the planted-value render.
func sessionWith(id, ref string) touch.Session {
	return touch.Session{ID: id, Actors: []string{`a"<i>`}, FirstDate: "2000-01-02", LastDate: "2000-01-02",
		Bullets: []touch.BulletRef{{EntryRef: ref, CitationID: "0a1b2c3d", Date: "2000-01-02"}}}
}

// 🔴 THE SESSION PAGE'S GUARDS, THROUGH THE REAL DISPATCHER OVER THE REAL `StoreSource`, A STORE ON
// DISK AND A JOURNAL FILE OUTSIDE IT. Every name, session id and date is SYNTHETIC.
//
// The world: principal R ("roamer") reads `north-notes` and `south-notes` (one project); principal E
// reads ONLY `east-notes` (another project). `s-roam-01` wrote in all three — NEWEST in east, so a
// walk that ignored the narrowing would put east FIRST rather than merely add it. `s-hide-02` wrote
// ONLY in east. Bullet counts per scope are pairwise distinct (north 1, south 2, east 3).
var (
	sessReader   = control.DerivedID(control.PrefixUser, "session-roamer")
	sessEastUser = control.DerivedID(control.PrefixUser, "session-easterner")
	sessProjectR = control.DerivedID(control.PrefixProject, "session-r")
	sessProjectE = control.DerivedID(control.PrefixProject, "session-e")
	sessNorth    = control.DerivedID(control.PrefixScope, "north-notes")
	sessSouth    = control.DerivedID(control.PrefixScope, "south-notes")
	sessEast     = control.DerivedID(control.PrefixScope, "east-notes")
	// sessNow is the injected clock: 2000-01-10, so a 2000-01-07 bullet is "3d ago".
	sessNow = time.Date(2000, 1, 10, 12, 0, 0, 0, time.UTC)
	// sessLong is a session id EXACTLY at the grammar's bound (64 bytes) — the inside of the boundary.
	sessLong = "s" + strings.Repeat("x", 63)
)

func sessionWorld(t *testing.T) (roamer, easterner identity.Identity) {
	t.Helper()
	at := browseClock
	m, err := control.Replay([]control.Event{
		{Kind: control.EventUserCreated, At: at, UserID: sessReader, Provider: "fixture-provider",
			Subject: "00000000-0000-4000-8000-000000000051", Email: "roamer@notes.example.invalid"},
		{Kind: control.EventUserCreated, At: at, UserID: sessEastUser, Provider: "fixture-provider",
			Subject: "00000000-0000-4000-8000-000000000052", Email: "easterner@notes.example.invalid"},
		{Kind: control.EventProjectCreated, At: at, ProjectID: sessProjectR, Name: "roam", UserID: sessReader},
		{Kind: control.EventMemberSet, At: at, ProjectID: sessProjectR, UserID: sessReader, Role: control.RoleOwner},
		{Kind: control.EventScopeCreated, At: at, ScopeID: sessNorth, DisplayName: "north-notes", ProjectID: sessProjectR},
		{Kind: control.EventScopeCreated, At: at, ScopeID: sessSouth, DisplayName: "south-notes", ProjectID: sessProjectR},
		{Kind: control.EventProjectCreated, At: at, ProjectID: sessProjectE, Name: "east", UserID: sessEastUser},
		{Kind: control.EventMemberSet, At: at, ProjectID: sessProjectE, UserID: sessEastUser, Role: control.RoleOwner},
		{Kind: control.EventScopeCreated, At: at, ScopeID: sessEast, DisplayName: "east-notes", ProjectID: sessProjectE},
	})
	if err != nil {
		t.Fatalf("building the session world: %v", err)
	}
	resolve := func(id control.ID) identity.Identity {
		p, known := m.PrincipalFor(control.KindUser, id)
		if !known {
			t.Fatalf("no principal %s", id)
		}
		return identity.Identity{Principal: p, Auth: control.Resolve(m, p)}
	}
	roamer, easterner = resolve(sessReader), resolve(sessEastUser)
	// INSTRUMENT CONTROL: the authorities are what the assertions assume.
	if !roamer.Auth.Allows(sessNorth, control.VerbRead) || !roamer.Auth.Allows(sessSouth, control.VerbRead) ||
		roamer.Auth.Allows(sessEast, control.VerbRead) {
		t.Fatal("the roamer does not read exactly north and south")
	}
	if !easterner.Auth.Allows(sessEast, control.VerbRead) || easterner.Auth.Allows(sessNorth, control.VerbRead) {
		t.Fatal("the easterner does not read exactly east")
	}
	return roamer, easterner
}

func sessionStore(t *testing.T) string {
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
	put("north-notes", "kettle",
		"- 2000-01-03: boiled the kettle [cairn: roam-bot/s-roam-01]\n- 2000-01-02: unsigned\n"+
			"- 2000-01-01: the long one [cairn: roam-bot/"+sessLong+"]\n")
	put("south-notes", "ladle",
		"- 2000-01-07: stirred the pot [cairn: roam-bot/s-roam-01]\n- 2000-01-05: ladled out [cairn: other-bot/s-roam-01]\n")
	put("east-notes", "spoon",
		"- 2000-01-09: hidden one [cairn: roam-bot/s-roam-01]\n"+
			"- 2000-01-08: hidden two [cairn: hide-bot/s-hide-02]\n"+
			"- 2000-01-06: hidden three [cairn: hide-bot/s-hide-02]\n")
	return root
}

func sessionJournal(t *testing.T) string {
	reg := func(home, slug, status string, members ...arcs.Member) arcs.Registration {
		return arcs.Registration{Schema: arcs.Schema, Home: home, Slug: slug, Status: status,
			ClosingKind: arcs.ClosingCheck, DeclaredScopes: []string{home}, WritersMeasured: true, Members: members,
			RegisteredBy: "fixture-registrar", RegisteredAt: "2000-01-08T07:08:09Z"}
	}
	return arcsJournal(t,
		reg("north-notes", "kettle-arc", arcs.StatusOpen, arcs.Member{Session: "s-roam-01", Role: "wrote"}),
		reg("east-notes", "spoon-arc", arcs.StatusClosed,
			arcs.Member{Session: "s-roam-01", Role: "originated"}, arcs.Member{Session: "s-hide-02", Role: "originated"}))
}

func sessionServer(t *testing.T, src Source, id identity.Identity) *Server {
	t.Helper()
	cfg := testConfig(t, staticAuth{id})
	cfg.Source = src
	cfg.Now = func() time.Time { return sessNow }
	srv, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return srv
}

func sessionURL(id string) string {
	return SessionPath + "?" + url.Values{QuerySession: []string{id}}.Encode()
}

// TestTheSessionPageAggregatesReadableScopesNewestFirstAndOmitsAnUnreadableOne — the cross-scope
// aggregation, its grouping and order, and the narrowing, through the dispatcher.
func TestTheSessionPageAggregatesReadableScopesNewestFirstAndOmitsAnUnreadableOne(t *testing.T) {
	roamer, easterner := sessionWorld(t)
	src := StoreSource{Root: sessionStore(t), ArcJournal: sessionJournal(t)}

	rec := getAs(t, sessionServer(t, src, roamer), sessionURL("s-roam-01"))
	if rec.Code != http.StatusOK {
		t.Fatalf("the roamer's session page answered %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	order := regexp.MustCompile(`data-scope="([^"]+)"`).FindAllStringSubmatch(body, -1)
	var got []string
	for _, m := range order {
		got = append(got, m[1])
	}
	if strings.Join(got, ",") != "south-notes,north-notes" {
		t.Errorf("scope cards %v, want [south-notes north-notes]: grouped by scope, newest activity first", got)
	}
	for _, leak := range []string{"east-notes", "hidden one", "spoon-arc", "hide-bot"} {
		if strings.Contains(body, leak) {
			t.Errorf("THE SESSION PAGE CONTAINS %q — a write or arc in a scope the roamer cannot read", leak)
		}
	}
	text := strings.Join(strings.Fields(visibleText(body)), " ")
	for _, want := range []string{
		"3 bullets in 2 scopes", "4 of 5 bullets attributed", // north 3 bullets (2 attributed) + south 2 (2)
		"first 7d ago", "last 3d ago", "roam-bot", "other-bot", "kettle-arc",
		"stirred the pot", "boiled the kettle",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("the session page is missing %q:\n%s", want, text)
		}
	}
	// Within the south card the newer bullet is first.
	if i, j := strings.Index(text, "stirred the pot"), strings.Index(text, "ladled out"); i < 0 || j < 0 || i > j {
		t.Errorf("south's bullets are not newest first (stirred %d, ladled %d)", i, j)
	}

	// POSITIVE CONTROL: the same id, for the easterner, IS east — so the absences above measure the
	// narrowing rather than a page that never shows east.
	east := getAs(t, sessionServer(t, src, easterner), sessionURL("s-roam-01"))
	if east.Code != http.StatusOK || !strings.Contains(east.Body.String(), `data-scope="east-notes"`) ||
		!strings.Contains(east.Body.String(), "spoon-arc") {
		t.Fatalf("POSITIVE CONTROL FAILED: the easterner's page for s-roam-01 (%d) does not show east:\n%s", east.Code, east.Body.String())
	}
	if strings.Contains(east.Body.String(), "north-notes") {
		t.Error("the easterner's page shows north — the narrowing is not per caller")
	}
}

// TestASessionOnlyInAnUnreadableScopeAnswersExactlyLikeOneNeverWritten — no existence oracle. A
// session that wrote only where the caller cannot read, an id never written, and an id the trailer
// grammar cannot even produce answer the SAME status and the SAME bytes, naming nothing.
func TestASessionOnlyInAnUnreadableScopeAnswersExactlyLikeOneNeverWritten(t *testing.T) {
	roamer, easterner := sessionWorld(t)
	src := StoreSource{Root: sessionStore(t), ArcJournal: sessionJournal(t)}
	srv := sessionServer(t, src, roamer)

	misses := map[string]string{
		"written only in an unreadable scope (and a member of an arc homed there)": "s-hide-02",
		"never written by anybody":            "s-never-03",
		"not a possible session id at all":    "<s>",
		"one byte over the grammar's bound":   sessLong + "x",
		"the hidden id with its case changed": "S-HIDE-02",
	}
	var first string
	for name, id := range misses {
		rec := getAs(t, srv, sessionURL(id))
		if rec.Code != http.StatusNotFound || rec.Body.String() != sessionUnseenBody {
			t.Errorf("%s answered %d %q, want 404 %q", name, rec.Code, rec.Body.String(), sessionUnseenBody)
		}
		if first == "" {
			first = rec.Body.String()
		} else if rec.Body.String() != first {
			t.Errorf("%s answered DIFFERENT bytes from another miss — an oracle over who wrote where", name)
		}
		for _, leak := range []string{"east", "hide", "spoon"} {
			if strings.Contains(rec.Body.String(), leak) {
				t.Errorf("the refusal for %s names %q", name, leak)
			}
		}
	}
	// POSITIVE CONTROLS: the hidden session IS a page for the easterner, and the bound's INSIDE edge
	// (exactly 64 bytes) is a page for the roamer — so the misses above are refusals, not a page that
	// refuses everything, and the bound is where the grammar puts it rather than one byte early.
	if rec := getAs(t, sessionServer(t, src, easterner), sessionURL("s-hide-02")); rec.Code != http.StatusOK {
		t.Fatalf("POSITIVE CONTROL FAILED: the easterner got %d for s-hide-02", rec.Code)
	}
	if rec := getAs(t, srv, sessionURL(sessLong)); rec.Code != http.StatusOK {
		t.Fatalf("POSITIVE CONTROL FAILED: a 64-byte session id that WAS written answered %d", rec.Code)
	}
}

// TestAnUnreadableJournalWithNothingVisibleIsA503NotTheUnseen404 — "could not look" is not "nothing
// there": with the journal configured but unreadable, a session with no visible WRITE might still be
// a member of an arc nobody could read, so a grammar-valid unseen id answers 503, never the uniform
// 404. RED with that branch disabled (`if false && answer.ArcsUnreadable`).
func TestAnUnreadableJournalWithNothingVisibleIsA503NotTheUnseen404(t *testing.T) {
	roamer, _ := sessionWorld(t)
	broken := StoreSource{Root: sessionStore(t), ArcJournal: t.TempDir()} // a directory: the read fails
	srv := sessionServer(t, broken, roamer)
	for _, id := range []string{"s-never-03", "s-hide-02"} {
		rec := getAs(t, srv, sessionURL(id))
		if rec.Code != http.StatusServiceUnavailable || rec.Body.String() != arcJournalUnreadable {
			t.Errorf("%s with an unreadable journal answered %d %q, want 503 %q", id, rec.Code, rec.Body.String(),
				arcJournalUnreadable)
		}
	}
	// A grammar-REFUSED id is still the 404: it cannot be a member of anything, journal or not.
	if rec := getAs(t, srv, sessionURL("<s>")); rec.Code != http.StatusNotFound {
		t.Errorf("a grammar-refused id answered %d with an unreadable journal, want 404", rec.Code)
	}
	// POSITIVE CONTROL: a session with visible writes is still a page, badged as arcs-unknown.
	rec := getAs(t, srv, sessionURL("s-roam-01"))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), ">arcs unknown</span>") {
		t.Fatalf("POSITIVE CONTROL FAILED: s-roam-01 answered %d without the arcs-unknown badge:\n%s", rec.Code, rec.Body.String())
	}
}

// sessionCounting counts `Session`, so a test can see the bound run BEFORE any read.
type sessionCounting struct {
	StoreSource
	asked int
}

func (c *sessionCounting) Session(a control.Authorization, s string) (SessionAnswer, error) {
	c.asked++
	return c.StoreSource.Session(a, s)
}

// TestAHostileSessionIdIsBoundedBeforeAnyRead — `<`, `"`, `&`, a NUL and a very long value never
// reach the store walk, and the answer never echoes them.
func TestAHostileSessionIdIsBoundedBeforeAnyRead(t *testing.T) {
	roamer, _ := sessionWorld(t)
	src := &sessionCounting{StoreSource: StoreSource{Root: sessionStore(t), ArcJournal: sessionJournal(t)}}
	srv := sessionServer(t, src, roamer)
	for _, id := range []string{`<script>x</script>`, `s-roam-01"onmouseover="x`, "s-roam-01&session=s-hide-02",
		"s-roam-01\x00", strings.Repeat("s", 10000), " s-roam-01", "s-roam-01\n"} {
		rec := getAs(t, srv, sessionURL(id))
		if rec.Code != http.StatusNotFound || rec.Body.String() != sessionUnseenBody {
			t.Errorf("id %.40q answered %d %q", id, rec.Code, rec.Body.String())
		}
	}
	if src.asked != 0 {
		t.Errorf("the store walk ran %d time(s) for ids the grammar cannot produce — the bound must come first", src.asked)
	}
	// POSITIVE CONTROL: a valid id DOES reach the walk.
	getAs(t, srv, sessionURL("s-roam-01"))
	if src.asked != 1 {
		t.Fatalf("POSITIVE CONTROL FAILED: a valid id reached the walk %d time(s), want 1", src.asked)
	}
}

// TestTheSessionPageEscapesAPlantedHostileValueInEveryPosition renders the page directly over a
// REPORT VALUE carrying markup — the grammar refuses such an id at the handler, so this is
// `hostileWorld`'s ruling: a page whose safety depended on a loader invariant would be one new
// writer away from broken. Every href must round-trip its operands exactly.
func TestTheSessionPageEscapesAPlantedHostileValueInEveryPosition(t *testing.T) {
	hostile := `s"><script>alert(1)</script>&session=x#y`
	hostileScope := `n"><img src=x onerror=y>`
	hostileRef := `ref"&scope=evil#z`
	view := PageView{Viewer: "v", Now: sessNow, Scopes: []Scope{{ID: "scp_planted", Name: hostileScope}},
		Session: &SessionAnswer{Report: report.SessionAcrossReport{ID: hostile, ArcsConfigured: true,
			Scopes: []report.SessionInScope{{Scope: hostileScope, Session: sessionWith(hostile, hostileRef)}},
			Arcs:   []report.SessionArc{{Home: hostileScope, Slug: `slug"<b>`, Status: "open"}}}}}
	var b strings.Builder
	if err := SessionPage(view).Render(&b); err != nil {
		t.Fatal(err)
	}
	page := b.String()
	for _, bad := range []string{"<script", "<img", "<b>", `href="javascript:`} {
		if strings.Contains(page, bad) {
			t.Errorf("the page contains %q unescaped", bad)
		}
	}
	links := 0
	for _, m := range regexp.MustCompile(`href="([^"]*)"`).FindAllStringSubmatch(page, -1) {
		raw := strings.ReplaceAll(m[1], "&amp;", "&")
		u, err := url.Parse(raw)
		if err != nil || u.Scheme != "" || u.Host != "" {
			t.Errorf("href %q is not a same-origin path", raw)
			continue
		}
		switch u.Path {
		case EntryPath:
			links++
			if q := u.Query(); len(q) != 2 || q.Get(QueryRef) != hostileRef || q.Get(QueryScope) != "scp_planted" {
				t.Errorf("entry href %q does not round-trip (got %v)", raw, q)
			}
		case ArcPath:
			links++
			if q := u.Query(); len(q) != 2 || q.Get(QuerySlug) != `slug"<b>` {
				t.Errorf("arc href %q does not round-trip (got %v)", raw, q)
			}
		case ScopePath:
			links++
		}
	}
	if links < 3 {
		t.Fatalf("INSTRUMENT: %d entry/arc/scope link(s) rendered, want ≥3 — the round-trip checks measured too little", links)
	}
}

// TestEveryBulletLinkOnTheSessionPageLandsOnAnAnchorThatExists: fetch the session page, follow every
// bullet link to its entry page, and require the fragment to be an element id THERE — the seam
// between two pages, which neither page's own test can see.
func TestEveryBulletLinkOnTheSessionPageLandsOnAnAnchorThatExists(t *testing.T) {
	roamer, _ := sessionWorld(t)
	srv := sessionServer(t, StoreSource{Root: sessionStore(t), ArcJournal: sessionJournal(t)}, roamer)
	page := getAs(t, srv, sessionURL("s-roam-01")).Body.String()
	followed := 0
	for _, m := range regexp.MustCompile(`href="(/entry\?[^"]*)"`).FindAllStringSubmatch(page, -1) {
		raw := strings.ReplaceAll(m[1], "&amp;", "&")
		target, fragment, found := strings.Cut(raw, "#")
		if !found || fragment == "" {
			t.Errorf("bullet link %q carries no fragment, so it lands on the top of the entry, not the bullet", raw)
			continue
		}
		entry := getAs(t, srv, target)
		if entry.Code != http.StatusOK {
			t.Errorf("the entry page %s answered %d", target, entry.Code)
			continue
		}
		if !strings.Contains(entry.Body.String(), `<li class="bullet" id="`+fragment+`">`) {
			t.Errorf("the entry page %s has no bullet with id %q — the link lands nowhere", target, fragment)
		}
		followed++
	}
	if followed != 3 {
		t.Fatalf("followed %d bullet link(s), want 3 (south 2 + north 1) — the walk above measured the wrong set", followed)
	}
}

// TestEveryBulletAnchorOnAnEntryPageIsUnique: two BYTE-IDENTICAL bullets share a citation id, and an
// HTML id must be unique — the first gets `b-<cid>`, the repeat `b-<cid>-2`, and a session link
// (which names `b-<cid>`) lands on the first.
func TestEveryBulletAnchorOnAnEntryPageIsUnique(t *testing.T) {
	bullets := store.ParseJournalBullets("- 2000-01-02: same [cairn: a-bot/s-1]\n- 2000-01-03: other\n- 2000-01-02: same [cairn: a-bot/s-1]\n")
	got := bulletAnchors(bullets)
	cid := bullets[0].CitationID()
	if cid == "" || cid != bullets[2].CitationID() {
		t.Fatalf("INSTRUMENT: identical bullets must share a non-empty citation id (%q vs %q)", cid, bullets[2].CitationID())
	}
	want := []string{"b-" + cid, "b-" + bullets[1].CitationID(), "b-" + cid + "-2"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("anchors %v, want %v", got, want)
	}
	seen := map[string]bool{}
	for _, a := range got {
		if seen[a] {
			t.Errorf("anchor %q is not unique", a)
		}
		seen[a] = true
	}
}
