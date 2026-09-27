package ui

import (
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
	want := []string{"entry", "navigate", "root", "scope", "search", "share-index", "share-scope"}
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
func TestTheSignInPageOffersNoAuthenticatedNavigation(t *testing.T) {
	for _, provider := range []bool{false, true} {
		html := renderNode(t, SignInPage("", provider))

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

	return map[string]string{
		"root":        renderNode(t, Page(v)),
		"navigate":    renderNode(t, NavigatePage(v)),
		"scope":       renderNode(t, ScopePage(scopeView)),
		"entry":       renderNode(t, EntryPage(entryView)),
		"search":      renderNode(t, Page(searchView)),
		"share-index": renderNode(t, SharePage(shareIndexView)),
		"share-scope": renderNode(t, SharePage(shareScopeView)),
	}
}
