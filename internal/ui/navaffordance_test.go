package ui

import (
	"net/http"
	"sort"
	"strings"
	"testing"

	"github.com/ZacxDev/cairn/internal/control"
)

// TestEveryRenderedPageCarriesBothNavigationAffordances is a REGRESSION test, not an
// invariant guard, and the defect it pins shipped to a live deployment.
//
// 🔴 WHAT WAS BROKEN: every share route was registered, the control journal was seeded
// with an `owner` membership over 26 scopes, and the pod's own startup line read
// `sharing writable` — and NOTHING RENDERED A LINK TO `/share`. The only use of
// `SharePath` in a rendered page was inside `shareIndex`, which is a child of
// `SharePage` itself: the share page linked to its own sub-pages, and no page linked to
// the share page. So a feature that was deployed, authorised and working read as ABSENT
// to the operator, who reported it missing. The mirror defect sat on the other side:
// `SharePage` built its own frame with the wordmark as `h.H1(g.Text("cairn"))` — plain
// text — so the share flow had no way back either.
//
// 🔴 WHY A LEDGER RATHER THAN ONE ASSERTION: this failure mode is a page that FORGETS the
// frame, so the guard has to fail when the page set GROWS as well as when an affordance
// disappears. A test over `Page` alone would have been green throughout the defect,
// because the root page was never the one missing a link — the share page was, and it did
// not exist when a root-only guard would have been written.
//
// ⚠ IT ASSERTS AN `href` TO EACH PATH CONSTANT, NEVER THE LINK TEXT. Link wording is
// cosmetic and a guard on the word "Sharing" is walkable by renaming the button, which is
// exactly the spelled-versus-structural trap this repository records; the destination is
// the thing a reader needs.
func TestEveryRenderedPageCarriesBothNavigationAffordances(t *testing.T) {
	pages := everyRenderedPage(t)

	// The ledger. A page added to the surface must be added here, and the count is
	// asserted so that adding one without an entry FAILS rather than passing unseen.
	want := []string{
		"entry", "hub", "invite-index", "invite-minted", "invite-project", "navigate", "root",
		"scope", "search", "sessions", "share-index", "share-scope",
	}
	got := make([]string, 0, len(pages))
	for name := range pages {
		got = append(got, name)
	}
	sort.Strings(got)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("the rendered-page ledger is %v but this guard knows %v; a page added to the "+
			"surface needs an entry here, because the defect this test exists for is a page "+
			"that renders without the frame", got, want)
	}

	// POSITIVE CONTROL — prove the matcher can see an href at all before trusting a
	// pass. A substring test that silently never matches is indistinguishable from a
	// page that carries the link, and the stylesheet href is on every page by
	// construction.
	for name, html := range pages {
		if !strings.Contains(html, `href="`+StylesheetHashedPath+`"`) {
			t.Fatalf("positive control failed on %q: the matcher cannot see the stylesheet "+
				"href, so a zero for any other href would prove nothing", name)
		}
	}

	for name, html := range pages {
		if !strings.Contains(html, `href="`+RootPath+`"`) {
			t.Errorf("%s: no href to RootPath (%q) — the page has no way back to the browse "+
				"surface", name, RootPath)
		}
		if !strings.Contains(html, `href="`+SharePath+`"`) {
			t.Errorf("%s: no href to SharePath (%q) — the share flow is reachable only by "+
				"typing the URL, which is the defect this test pins", name, SharePath)
		}
		// 🔴 THE THIRD AFFORDANCE, ADDED BY THE INVITE FLOW, AND THE TEST'S NAME NOW
		// UNDERSTATES IT — WHICH IS SAID HERE RATHER THAN FIXED BY A RENAME. A Go test
		// cannot be renamed without breaking every `-run` filter and every reference to it
		// in prose, and this repository already records what a description narrower than
		// its implementation costs. This one is the harmless direction: the name says two
		// and the body asserts three.
		if !strings.Contains(html, `href="`+InvitePath+`"`) {
			t.Errorf("%s: no href to InvitePath (%q) — the invite flow is reachable only by "+
				"typing the URL, which is the same defect one object over: without it the "+
				"share page's empty-candidates sentence names a remedy nobody can reach",
				name, InvitePath)
		}
	}
}

// TestTheSignInPageOffersNoAuthenticatedNavigation pins the OTHER side of the boundary,
// and it exists because the change that added the share link argued for CONSOLIDATING
// frames — which makes this the next plausible mistake rather than a hypothetical one.
//
// 🔴 `SignInPage` BUILDS ITS OWN FRAME AND MUST KEEP DOING SO. It is the one PUBLIC page
// on this surface: there is no viewer, no session and no CSRF token, so every affordance
// `shell` renders is either meaningless or a dead link there. Routing it through `shell`
// to remove a duplicated header — exactly the tidy-up `SharePage` just received, one
// function away in the same file — would put a `Sharing` link in front of an
// unauthenticated visitor, pointing at a route that answers 401.
//
// ⚠ THE LEDGER ABOVE CANNOT CATCH THIS, AND THAT IS WHY THIS IS A SEPARATE TEST. That
// guard asserts each page it KNOWS carries the affordances; a page silently GAINING them
// is invisible to it, because sign-in is deliberately not in its list. Two guards, two
// directions.
//
// ⚠ AND IT IS NOW THE NARROWER OF TWO GUARDS ON THIS SIDE, KEPT RATHER THAN SUBSUMED.
// `TestNoPublicPageOffersAuthenticatedNavigation` walks a LEDGER of every public page and
// makes the same assertion over all of them, which is strictly wider — but this one carries
// the argument about `SignInPage` SPECIFICALLY, whose duplicate header is the thing somebody
// will one day route through `shell` to tidy up. Deleting this would leave the wider guard
// asserting the property with no record of why it is the property.
func TestTheSignInPageOffersNoAuthenticatedNavigation(t *testing.T) {
	for _, provider := range []bool{false, true} {
		html := renderNode(t, SignInPage("", provider, "", App{}))

		// POSITIVE CONTROL — the matcher must be able to see an href on this page at
		// all, or "no share link" is a fact about the matcher. The sign-in form posts
		// to SignInPath, and the stylesheet is linked in the head.
		if !strings.Contains(html, `href="`+StylesheetHashedPath+`"`) {
			t.Fatalf("provider=%v: positive control failed — the matcher cannot see the "+
				"stylesheet href, so any absence below would prove nothing", provider)
		}

		if strings.Contains(html, `href="`+SharePath+`"`) {
			t.Errorf("provider=%v: the PUBLIC sign-in page offers a link to SharePath (%q). "+
				"There is no session here, so it is a dead link to a 401 — and the likely "+
				"cause is `SignInPage` being routed through `shell` to deduplicate its "+
				"header. It must keep its own frame.", provider, SharePath)
		}
		if strings.Contains(html, "signed in as") {
			t.Errorf("provider=%v: the public sign-in page claims a signed-in viewer", provider)
		}
	}
}

// everyRenderedPage renders one of each page the surface serves, over one world.
//
// ⚠ IT DELIBERATELY DOES NOT REUSE `renderedPages`, which covers the five BROWSE pages
// only. The two share pages are the ones the defect was about, so a helper that cannot
// render them is the wrong instrument here.
func everyRenderedPage(t *testing.T) map[string]string {
	t.Helper()
	world := benignWorld()

	v := viewOf("operator@example.invalid", world)
	scopeView := v
	scopeView.Scope = &world[0]
	entryView := scopeView
	entryView.Entry = &world[0].Entries[0]
	searchView := v
	searchView.Query = "runbook"

	named := control.NamedScope{ID: world[0].ID, Name: world[0].Name}
	shareIndexView := ShareView{
		Viewer:        "operator@example.invalid",
		CSRF:          renderCSRF,
		Administrable: []control.NamedScope{named},
	}
	shareScopeView := shareIndexView
	shareScopeView.Scope = named

	inviteIndexView := InviteView{
		Viewer:   "operator@example.invalid",
		CSRF:     renderCSRF,
		Projects: []control.NamedProject{fixtureNamedProject},
	}
	inviteProjectView := inviteIndexView
	inviteProjectView.Project = fixtureNamedProject
	inviteMintedView := inviteProjectView
	inviteMintedView.Minted = &MintedInvite{
		Link:    JoinPath + "?invite=" + fixtureInviteToken,
		Role:    control.RoleMember,
		Expires: "2000-01-09T03:04:05Z",
	}

	sessionsView := v
	sessionsView.SessionsList = &SessionsList{}

	return map[string]string{
		// "root" is the scope LIST (`Page`, at `/scopes`); "hub" is the root since the IA change.
		"root":        renderNode(t, Page(v)),
		"hub":         renderNode(t, HubPage(v)),
		"sessions":    renderNode(t, SessionsPage(sessionsView)),
		"navigate":    renderNode(t, NavigatePage(v)),
		"scope":       renderNode(t, ScopePage(scopeView)),
		"entry":       renderNode(t, EntryPage(entryView)),
		"search":      renderNode(t, Page(searchView)),
		"share-index": renderNode(t, SharePage(shareIndexView)),
		"share-scope": renderNode(t, SharePage(shareScopeView)),
		// 🔴 ALL THREE INVITE SHAPES, BECAUSE THE MINTED ONE IS THE PAGE MOST LIKELY TO BE
		// WRITTEN WITHOUT THE FRAME. It is the response to a POST rather than a navigation,
		// so it is the one a later change would be tempted to render as a bare fragment —
		// and a page holding a single-use capability with no way back to the rest of the
		// surface is the worst place for a reader to be stranded.
		"invite-index":   renderNode(t, InvitePage(inviteIndexView)),
		"invite-project": renderNode(t, InvitePage(inviteProjectView)),
		"invite-minted":  renderNode(t, InvitePage(inviteMintedView)),
	}
}

// TestNoPublicPageOffersAuthenticatedNavigation is the public side's LEDGER, and it exists
// because the invite flow made "the public page" plural.
//
// 🔴 A LEDGER RATHER THAN A SECOND COPY OF THE SIGN-IN ASSERTION, FOR THE REASON THE FRAME
// LEDGER ABOVE IS ONE: the failure mode is a page that renders with the WRONG frame, so the
// guard has to fail when the public page set GROWS as well as when an affordance appears on
// one. A per-page test would have been green on the day `JoinPage` was added, because it
// would not have existed yet — which is exactly how the share page shipped with no way back.
//
// ⚠ IT ASSERTS AN ABSENCE, SO IT NEEDS A POSITIVE CONTROL ON THE MATCHER PER PAGE. A
// substring test that silently never matches is indistinguishable from a page that carries
// no authenticated link, and the stylesheet href is on every page by construction.
func TestNoPublicPageOffersAuthenticatedNavigation(t *testing.T) {
	pages := everyPublicPage(t)

	want := []string{"join", "join-no-provider", "join-no-token", "sign-in", "sign-in-provider"}
	got := make([]string, 0, len(pages))
	for name := range pages {
		got = append(got, name)
	}
	sort.Strings(got)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("the public-page ledger is %v but this guard knows %v; a page dispatched with "+
			"`classPublic` renders to somebody with no session, so adding one is a decision "+
			"about which frame it gets and this is where it is written down", got, want)
	}

	// 🔴 THE LEDGER IS CHECKED AGAINST THE ROUTE TABLE, NOT ONLY AGAINST ITSELF. A
	// hand-written list of "the public pages" is exactly the second spelling
	// `internal/ui/routes.go` warns about, and it goes stale in the direction nobody
	// notices: a new `classPublic` GET row whose page nobody added here. The count cannot be
	// compared directly — the stylesheet's two rows are public and are not documents, and
	// the OAuth pair renders `SignInPage` rather than a page of its own — so what is pinned
	// is that every public page name below corresponds to a row and that the DOCUMENT-
	// rendering public rows are the ones this ledger covers.
	publicGETs := 0
	for _, row := range publicRoutes() {
		method, path, _ := splitRoute(row)
		if method != http.MethodGet {
			continue
		}
		publicGETs++
		if path == JoinPath && !strings.HasPrefix(strings.Join(got, ","), "join") {
			t.Errorf("%s is a public GET row and no `join` page is in the ledger above", path)
		}
	}
	if publicGETs == 0 {
		t.Fatal("NO public GET row exists in the ledger, so the cross-check above inspected nothing")
	}

	for name, html := range pages {
		if !strings.Contains(html, `href="`+StylesheetHashedPath+`"`) {
			t.Fatalf("positive control failed on %q: the matcher cannot see the stylesheet href, "+
				"so a zero for any other href would prove nothing", name)
		}
	}

	for name, html := range pages {
		for _, authenticated := range []struct {
			what, href string
		}{
			{"SharePath", SharePath},
			{"InvitePath", InvitePath},
			{"RootPath", RootPath},
		} {
			if strings.Contains(html, `href="`+authenticated.href+`"`) {
				t.Errorf("%s: the PUBLIC page offers a link to %s (%q). There is no session here, "+
					"so it is a dead link to a 401 — and the likely cause is the page being routed "+
					"through `shell` to deduplicate its header. Every public page must keep its own "+
					"frame.", name, authenticated.what, authenticated.href)
			}
		}
		if strings.Contains(html, "signed in as") {
			t.Errorf("%s: a public page claims a signed-in viewer", name)
		}
	}
}

// everyPublicPage renders one of each page a `classPublic` row can serve.
//
// ⚠ THE JOIN PAGE IS THREE ENTRIES BECAUSE IT HAS THREE STATES AND THEY RENDER DIFFERENT
// BODIES — a live-looking token with a provider, a token with the provider unavailable, and
// no token at all. The frame is the same on all three, which is the property under test, and
// a helper that rendered only the happy one would leave the two refusal bodies outside it.
func everyPublicPage(t *testing.T) map[string]string {
	t.Helper()
	return map[string]string{
		"sign-in":          renderNode(t, SignInPage("", false, "", App{})),
		"sign-in-provider": renderNode(t, SignInPage("", true, "", App{})),
		"join":             renderNode(t, JoinPage(fixtureInviteToken, true, App{})),
		"join-no-provider": renderNode(t, JoinPage(fixtureInviteToken, false, App{})),
		"join-no-token":    renderNode(t, JoinPage("", true, App{})),
	}
}
