package ui

import (
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/ZacxDev/cairn/internal/arcs"
	"github.com/ZacxDev/cairn/internal/identity"
	"github.com/ZacxDev/cairn/internal/store"
)

// 🔴 THE HUB AND THE SESSIONS LIST, DRIVEN THROUGH THE REAL DISPATCHER OVER THE REAL `StoreSource`, A
// STORE ON DISK AND A JOURNAL OUTSIDE IT. Every name, id and date is SYNTHETIC. The world is
// `arcsWorld`'s three principals — A reads alpha, B reads beta, W reads both — over a store and a
// journal built so A's numbers are PAIRWISE DISTINCT (1 scope, 2 live arcs, 3 arcs, 4 sessions) and
// every one of them MOVES for W (2, 3, 4, 6): a list or count that included an unreadable item lands
// on a different number.
const (
	hubSessionLamp   = "s-lamp-0001"   // wrote in alpha
	hubSessionWick   = "s-wick-0002"   // wrote ONLY in beta
	hubSessionBoth   = "s-both-0003"   // wrote in alpha (2000-01-05) AND in beta (2000-01-07)
	hubSessionMember = "s-member-0004" // never wrote; a member of an arc homed in alpha
	hubSessionHidden = "s-hidden-0005" // never wrote; a member ONLY of an arc homed in beta
	hubSessionFlint  = "s-flint-0006"  // wrote in alpha, newest there
)

func hubStore(t *testing.T) string {
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
		"- 2000-01-06: struck a flint [cairn: tallow-bot/"+hubSessionFlint+"]\n"+
			"- 2000-01-05: wrote on both sides [cairn: tallow-bot/"+hubSessionBoth+"]\n"+
			"- 2000-01-03: trimmed the wick [cairn: tallow-bot/"+hubSessionLamp+"]\n"+
			"- 2000-01-02: an unsigned bullet\n")
	put("beta-notes", "chandlery",
		"- 2000-01-07: the hidden half [cairn: wax-bot/"+hubSessionBoth+"]\n"+
			"- 2000-01-04: poured the wax [cairn: wax-bot/"+hubSessionWick+"]\n")
	return root
}

// hubArcs: three arcs homed in alpha (two open, one closed long ago) and one open arc homed in beta.
func hubArcs() []arcs.Registration {
	return []arcs.Registration{
		arcReg("alpha-notes", "lantern-arc", arcs.StatusOpen, []string{"alpha-notes"},
			arcs.Member{Session: hubSessionLamp, Role: "originated"},
			arcs.Member{Session: hubSessionMember, Role: "wrote"}),
		arcReg("alpha-notes", "ember-arc", arcs.StatusOpen, []string{"alpha-notes"},
			arcs.Member{Session: hubSessionFlint, Role: "originated"}),
		arcReg("alpha-notes", "cinder-arc", arcs.StatusClosed, []string{"alpha-notes"},
			arcs.Member{Session: hubSessionLamp, Role: "wrote"}),
		arcReg("beta-notes", "veiled-arc", arcs.StatusOpen, []string{"beta-notes"},
			arcs.Member{Session: hubSessionWick, Role: "originated"},
			arcs.Member{Session: hubSessionHidden, Role: "wrote"}),
	}
}

func hubServer(t *testing.T, id identity.Identity) *Server {
	t.Helper()
	return arcsServer(t, StoreSource{Root: hubStore(t), ArcJournal: arcsJournal(t, hubArcs()...)}, id)
}

// hubCardText is the visible text of ONE hub card, found by its `data-hub` name.
func hubCardText(t *testing.T, body, name string) string {
	t.Helper()
	m := regexp.MustCompile(`(?s)<section class="card" data-hub="` + name + `">(.*?)</section>`).FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("the hub has no %q card", name)
	}
	return strings.Join(strings.Fields(visibleText(m[1])), " ")
}

// TestTheHubCountsOnlyWhatTheViewerCanRead is the hub's authorisation guard: its ONE count, the
// scopes card's, is the viewer's own narrowed answer, and the SAME request from W — who reads both
// scopes — moves it, so a count that ignored the narrowing would read W's number for A. The other
// three cards carry no number at all (the arcs and sessions counts were dropped in review, D1).
func TestTheHubCountsOnlyWhatTheViewerCanRead(t *testing.T) {
	readsA, _, readsW := arcsWorld(t)
	want := map[string]string{"A": "1 readable scope", "W": "2 readable scopes"}
	for who, id := range map[string]identity.Identity{"A": readsA, "W": readsW} {
		rec := getAs(t, hubServer(t, id), RootPath)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: the hub answered %d: %s", who, rec.Code, rec.Body.String())
		}
		body := rec.Body.String()
		if got := hubCardText(t, body, "scopes"); !strings.Contains(got, want[who]) {
			t.Errorf("%s: the scopes card reads %q, want it to carry %q — the count of what THIS viewer can read",
				who, got, want[who])
		}
		for _, card := range []string{"arcs", "sessions", "team"} {
			if got := hubCardText(t, body, card); strings.ContainsAny(got, "0123456789") {
				t.Errorf("%s: the %s card carries a number (%q); only the scopes card has a count", who, card, got)
			}
		}
	}
	// And A's hub names nothing of beta's.
	body := getAs(t, hubServer(t, readsA), RootPath).Body.String()
	for _, hidden := range []string{"beta-notes", "veiled-arc", hubSessionWick, hubSessionHidden} {
		if strings.Contains(body, hidden) {
			t.Errorf("A's hub names %q, which only beta's readers may learn", hidden)
		}
	}
}

// TestTheHubIsFourCardsEachLinkingItsPage pins the hub's shape: the four ways in, each title a link
// to its page — the team card to `/team` (#214), which `/share` and `/invite` now redirect to.
func TestTheHubIsFourCardsEachLinkingItsPage(t *testing.T) {
	readsA, _, _ := arcsWorld(t)
	body := getAs(t, hubServer(t, readsA), RootPath).Body.String()
	for card, href := range map[string]string{"arcs": "/arcs", "scopes": "/scopes", "sessions": "/sessions", "team": "/team"} {
		re := regexp.MustCompile(`(?s)<section class="card" data-hub="` + card + `">.*?<a class="card-name" href="` +
			regexp.QuoteMeta(href) + `">`)
		if !re.MatchString(body) {
			t.Errorf("the %s card does not link %s from its title", card, href)
		}
	}
	if n := strings.Count(body, `data-hub="`); n != 4 {
		t.Errorf("the hub renders %d cards, want 4", n)
	}
	// The scope list moved: no scope card and no search box on the hub.
	if strings.Contains(body, `class="searchbar"`) || strings.Contains(body, `class="kind"`) {
		t.Error("the hub still carries the scope list's search box or scope cards")
	}
}

// TestTheOldScopeListQueriesRedirectToScopes pins the 303: `/?q=` and `/?tag=` were the root's own
// parameters, so a bookmark or the mobile plan's Search shortcut must land on `/scopes` with the
// query intact — and nothing else at `/` may be redirected.
func TestTheOldScopeListQueriesRedirectToScopes(t *testing.T) {
	srv := newTestServer(t, staticAuth{testIdentity()})
	for from, to := range map[string]string{
		"/?q=quarrying":                  "/scopes?q=quarrying",
		"/?tag=marketing":                "/scopes?tag=marketing",
		"/?q=":                           "/scopes?q=",
		"/?q=a+b&tag=x&tag=y":            "/scopes?q=a+b&tag=x&tag=y",
		"/?tag=x&q=lease&utm=1":          "/scopes?q=lease&tag=x&utm=1",
		"/?q=%22%3E%3Cscript%3Ealert(1)": "/scopes?q=%22%3E%3Cscript%3Ealert%281%29",
	} {
		rec := getAs(t, srv, from)
		if rec.Code != http.StatusSeeOther {
			t.Errorf("GET %s answered %d, want 303 to the scope list", from, rec.Code)
			continue
		}
		if got := rec.Header().Get("Location"); got != to {
			t.Errorf("GET %s redirected to %q, want %q", from, got, to)
		}
		// The target answers: the redirect is a way in, not a hop to a refusal.
		if follow := getAs(t, srv, to); follow.Code != http.StatusOK {
			t.Errorf("following %s to %s answered %d", from, to, follow.Code)
		}
	}
	// POSITIVE CONTROL: the bare root and a query naming neither parameter are the hub, not a 303.
	for _, path := range []string{"/", "/?view=raw", "/?query=x"} {
		rec := getAs(t, srv, path)
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `id="hub"`) {
			t.Errorf("GET %s answered %d without the hub, want 200 and the hub", path, rec.Code)
		}
	}
}

// TestTheSessionsPageListsExactlyTheSessionsWhosePageIsFound is the sessions list's authorisation
// guard, and it pins a RELATION rather than a list: for each principal, every listed session's page
// answers 200, and every session it does NOT list answers the uniform unseen 404. A list that named a
// session only an unreadable scope holds would fail the second half for that id.
func TestTheSessionsPageListsExactlyTheSessionsWhosePageIsFound(t *testing.T) {
	readsA, _, readsW := arcsWorld(t)
	all := []string{hubSessionLamp, hubSessionWick, hubSessionBoth, hubSessionMember, hubSessionHidden, hubSessionFlint}
	wantOrder := map[string][]string{
		// Newest visible bullet first, arc-only members (no date) last, ties by id. For A, s-both's
		// newest date is its ALPHA one (01-05): the beta bullet (01-07) is not A's to order by.
		"A": {hubSessionFlint, hubSessionBoth, hubSessionLamp, hubSessionMember},
		"W": {hubSessionBoth, hubSessionFlint, hubSessionWick, hubSessionLamp, hubSessionHidden, hubSessionMember},
	}
	for who, id := range map[string]identity.Identity{"A": readsA, "W": readsW} {
		srv := hubServer(t, id)
		rec := getAs(t, srv, SessionsPath)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: %s answered %d: %s", who, SessionsPath, rec.Code, rec.Body.String())
		}
		body := rec.Body.String()
		var listed []string
		for _, m := range regexp.MustCompile(`<li class="entry-row session-row" data-session="([^"]+)"`).FindAllStringSubmatch(body, -1) {
			listed = append(listed, m[1])
		}
		if strings.Join(listed, ",") != strings.Join(wantOrder[who], ",") {
			t.Errorf("%s: the sessions page lists %v, want %v (newest first)", who, listed, wantOrder[who])
		}
		for _, s := range all {
			page := getAs(t, srv, SessionPath+"?"+url.Values{QuerySession: []string{s}}.Encode())
			isListed := strings.Contains(","+strings.Join(listed, ",")+",", ","+s+",")
			switch {
			case isListed && page.Code != http.StatusOK:
				t.Errorf("%s: %s is LISTED and its page answers %d — the list names a session the page cannot find", who, s, page.Code)
			case !isListed && (page.Code != http.StatusNotFound || page.Body.String() != sessionUnseenBody):
				t.Errorf("%s: %s is NOT listed and its page answers %d — the list and the page disagree", who, s, page.Code)
			}
		}
	}
	// A's list names nothing of beta's: not its scope, not the hidden date s-both wrote there.
	body := getAs(t, hubServer(t, readsA), SessionsPath).Body.String()
	for _, hidden := range []string{"beta-notes", "veiled-arc", "wax-bot", "2000-01-07", hubSessionWick, hubSessionHidden} {
		if strings.Contains(body, hidden) {
			t.Errorf("A's sessions page names %q, which only beta's readers may learn", hidden)
		}
	}
	// POSITIVE CONTROL on that scan: W's page names the very strings A's must not.
	wide := getAs(t, hubServer(t, readsW), SessionsPath).Body.String()
	for _, seen := range []string{"beta-notes", "veiled-arc", "wax-bot", hubSessionWick, hubSessionHidden} {
		if !strings.Contains(wide, seen) {
			t.Errorf("W's sessions page lacks %q, so its absence from A's proves nothing", seen)
		}
	}
}
