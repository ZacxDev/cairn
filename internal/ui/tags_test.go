package ui

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/ZacxDev/cairn/internal/control"
)

// TestTheTagsKeyDescriptionIsPinnedWhole pins the line under the entry page's "Tags" heading as
// ONE NORMALISED STRING, the same way `TestTheRefsKeyDescriptionIsPinnedWhole` pins its sibling.
//
// 🔴 WHOLE-STRING, NOT KEYWORD, AND THE PRECEDENT IS MEASURED RATHER THAN CITED. The Refs line
// really did serve the word `deprecated` while the README shipped beside it said permanent, and
// the correction was asserted by nothing. This line makes TWO claims a tidying edit would drop
// without changing anything a keyword guard reads: that the tags shown are FOLDED (unlike the
// Aliases list directly above, which is as-written, so a reader comparing page against file
// otherwise sees two spellings and cannot tell which the store holds), and that the vocabulary
// is OPEN (there is no declared set, so a typo is a category of one that no gate catches — and
// the entry page is where an operator would look for the list that does not exist).
//
// ⚠ AN INVARIANT GUARD, LABELLED AS ONE. No bug ever removed this clause; what it catches is the
// next edit.
func TestTheTagsKeyDescriptionIsPinnedWhole(t *testing.T) {
	want := normalizeSpace(TagsKeyDescription)
	if want == "" {
		t.Fatal("TagsKeyDescription is EMPTY, so every comparison below is vacuous")
	}
	world := benignWorld()
	view := viewOf("operator@example.invalid", world)
	view.Scope = &world[0]
	view.Entry = &world[0].Entries[0]
	// INSTRUMENT CONTROL: the labelled list renders only when the entry carries tags, so a
	// fixture with none would satisfy nothing below by never emitting the line at all.
	if len(view.Entry.Tags) == 0 {
		t.Fatal("the fixture entry carries no tags, so the `Tags` list never renders")
	}
	got := pageText(renderNode(t, EntryPage(view)))
	if !strings.Contains(got, want) {
		t.Errorf("the entry page does not carry the tags-key description as a whole string."+
			"\nwant: %q\nThe line is pinned entire rather than by keyword because a reword that "+
			"drops the FOLDED clause or the OPEN-vocabulary clause is exactly what this guard "+
			"is for. If the wording changed on purpose, change `TagsKeyDescription` and this "+
			"test together.", want)
	}
	// NEGATIVE CONTROL on the comparison: a string the page does not carry must NOT be found,
	// or `Contains` over `pageText` would be satisfied by anything.
	if strings.Contains(got, normalizeSpace(TagsKeyDescription+" and something nobody wrote")) {
		t.Error("the page text contains a string nobody wrote, so the assertion above is vacuous")
	}
}

// TestEveryRenderedTagLinksToTheRootQueryAndNeverToAPath is criterion 7's structural half.
//
// 🔴 THE PATH IS THE WHOLE POINT. `routes` is an EXACT-MATCH map and its completeness is the
// claim `DeclaredRoutes()`, `TestEveryServedPathComesFromTheLedger` and the `stateChanging`
// cross-site classification all read. A `/tag/<name>` link would put USER TEXT in a path
// segment, which forces the dispatcher into prefix matching and makes that claim false — so this
// asserts the link is the ROOT path with a `?tag=` query, on both pages that render a tag.
//
// 🔴 AND IT IS ASSERTED OVER THE RENDERED MARKUP, NOT OVER `tagHref`. A unit test on the builder
// would pass while a page linked somewhere else; what matters is that no OTHER href appears
// around a tag.
func TestEveryRenderedTagLinksToTheRootQueryAndNeverToAPath(t *testing.T) {
	world := benignWorld()
	view := viewOf("operator@example.invalid", world)
	view.Scope = &world[0]
	entryView := view
	entryView.Entry = &world[0].Entries[0]

	tags := world[0].Entries[0].Tags
	if len(tags) < 2 {
		t.Fatal("the fixture must carry at least two tags, or a per-tag assertion cannot " +
			"distinguish a list from a single link")
	}
	for name, markup := range map[string]string{
		"entry page": renderNode(t, EntryPage(entryView)),
		"scope page": renderNode(t, ScopePage(view)),
		"tag page":   renderNode(t, Page(tagView(t, world, tags[0]))),
	} {
		for _, tag := range tags {
			want := `href="` + RootPath + "?" + url.Values{QueryTag: []string{tag}}.Encode() + `"`
			if !strings.Contains(markup, want) {
				t.Errorf("%s: tag %q does not link to the root query.\nwant: %s", name, tag, want)
			}
		}
		// 🔴 NO PATH-SHAPED TAG LINK ANYWHERE. Asserted as an absence over the whole markup
		// rather than per tag, because the hazard is a SECOND builder somebody adds later.
		for _, never := range []string{`href="/tag/`, `href="/tags/`, RootPath + "tag/"} {
			if strings.Contains(markup, never) {
				t.Errorf("%s: a tag is linked by PATH (%q), which would make the route ledger's "+
					"own completeness claim false", name, never)
			}
		}
	}
}

// TestAHostileTagIsEscapedInBothItsPositions covers the two sinks a tag reaches.
//
// 🔴 THE LOADER CANNOT PRODUCE A HOSTILE TAG, AND THAT IS WHY THIS EXISTS. `parseTagsField`
// folds every tag to `[a-z0-9.-]`, so no store FILE can put one on `ui.Entry.Tags`. A page whose
// escaping depended on that invariant would be one new writer of this projection away from
// broken with nothing to catch it — and `ui.Entry` already has three writers.
//
// 🔴 TWO POSITIONS, TWO MECHANISMS, AND THE ESCAPER COVERS ONLY ONE. The link TEXT goes through
// `g.Text`, which is gomponents' escaper; the HREF goes through `url.Values.Encode`, which is
// not the escaper at all and is the thing that stops a tag opening a second attribute. A guard
// on the text alone would be green for an href built by concatenation.
func TestAHostileTagIsEscapedInBothItsPositions(t *testing.T) {
	world := hostileWorld()
	view := viewOf("operator@example.invalid", world)
	view.Scope = &world[0]
	view.Entry = &world[0].Entries[0]
	if !slices.Contains(view.Entry.Tags, hostileTag) {
		t.Fatal("the hostile fixture carries no hostile tag, so this test measures nothing")
	}
	markup := renderNode(t, EntryPage(view))
	// POSITIVE CONTROL: the tag reached the page at all. Without it a renderer that dropped the
	// list entirely would satisfy every absence below.
	if !strings.Contains(markup, url.Values{QueryTag: []string{hostileTag}}.Encode()) {
		t.Fatalf("the hostile tag's encoded href is not on the page, so the absences below are "+
			"facts about a page that never rendered it.\ngot:\n%s", markup)
	}
	// The RAW hostile string must appear nowhere: not as text, not inside an attribute.
	if strings.Contains(markup, hostileTag) {
		t.Errorf("the hostile tag reached the page UNESCAPED, so it can close its attribute and "+
			"open an event handler.\ngot:\n%s", markup)
	}
	// And specifically not the two substrings that would mean it escaped its position.
	for _, never := range []string{`onfocus="fetch`, `data-y="`} {
		if strings.Contains(markup, never) {
			t.Errorf("the hostile tag produced %q in the markup", never)
		}
	}
}

// TestTheTagPageCannotSeeAScopeTheCallerCannotRead is the browser surface's half of criterion 5.
//
// 🔴 IT IS THE SAME AUTHORISATION-ORDER CLAIM `internal/report`'s guard makes, and this surface
// needs its own because it does not go through `report.Recall` at all: `handlePage` calls
// `Visible(auth)` and then `EntriesByTag` over that RESULT. The wrong implementation here is a
// tag lookup that loads the store itself — which is why `EntriesByTag` takes the scope LIST and
// is a package function rather than a method, so it has no `s.Root` to reach for.
//
// ⚠ IT IS A PAIR, NOT A ZERO: the principal who CAN see the scope must find the tag, or the
// other one's empty answer is a fact about a filter wired to nothing.
func TestTheTagPageCannotSeeAScopeTheCallerCannotRead(t *testing.T) {
	// Two scopes, the SAME tag in each, and only one visible to the narrow principal.
	wide := []Scope{
		{ID: control.ID("scp_wide00000000"), Name: "alpha-notes", Entries: []Entry{
			{Ref: "runbook", Filename: "runbook.md", Tags: []string{"marketing"}},
		}},
		{ID: control.ID("scp_narrow000000"), Name: "beta-notes", Entries: []Entry{
			{Ref: "ledger", Filename: "ledger.md", Tags: []string{"marketing"}},
			{Ref: "untagged", Filename: "untagged.md"},
		}},
	}
	narrow := wide[:1]

	// POSITIVE CONTROL: the wide list finds both.
	both := EntriesByTag(wide, "marketing")
	if len(both.Entries) != 2 || both.Scanned != 3 || both.ScopesScanned != 2 {
		t.Fatalf("POSITIVE CONTROL FAILED: matches=%d scanned=%d scopes=%d, want 2/3/2 — the "+
			"zero below is then a fact about a filter wired to nothing",
			len(both.Entries), both.Scanned, both.ScopesScanned)
	}

	// The narrowing: the same tag, over a list that does not hold the second scope.
	only := EntriesByTag(narrow, "marketing")
	if len(only.Entries) != 1 || only.Entries[0].Ref != "runbook" {
		t.Fatalf("the narrow list matched %+v, want exactly `runbook`", only.Entries)
	}
	if only.Scanned != 1 || only.ScopesScanned != 1 {
		t.Fatalf("the narrow list scanned %d entries in %d scopes, want 1 and 1 — a wider count "+
			"means the filter walked a scope the caller cannot read, whether or not it reported "+
			"a match from it", only.Scanned, only.ScopesScanned)
	}
	// 🔴 AND NOTHING FROM THE SECOND SCOPE REACHED ANY FIELD — not the entry, not the scope
	// NAME, not the id, which is the enumeration channel.
	for _, m := range only.Entries {
		if m.Ref == "ledger" || m.Scope == "beta-notes" || m.ScopeID == wide[1].ID {
			t.Errorf("something from an unreadable scope reached the listing: %+v", m)
		}
	}
	// …and the rendered page carries none of it either, because a leak through a count or a
	// summary sentence is still a leak.
	markup := renderNode(t, Page(tagView(t, narrow, "marketing")))
	for _, never := range []string{"ledger", "beta-notes", string(wide[1].ID)} {
		if strings.Contains(markup, never) {
			t.Errorf("the rendered tag page carries %q from a scope the caller cannot read", never)
		}
	}
}

// TestTheTagListingReportsWhatItLookedAt pins that a zero is readable.
//
// 🔴 AN EMPTY MATCH LIST CANNOT DISTINGUISH TWO MECHANISMS, and they have opposite next actions:
// "no entry carries this tag" (write the tag, or fix the typo) and "this credential can see
// nothing" (ask for access). That is `SearchResults.ScopesSearched`'s argument, and the counts
// are what make the difference visible.
//
// ⚠ AND THE TYPO CASE IS NAMED OUT LOUD, WHICH CLOSING THE WRITE-PATH VOCABULARY DID NOT
// RETIRE. This page's `?tag=` operand is checked against no valid-tag set at all, and an entry
// written before the closure can carry anything, so a zero over a non-empty store is still also
// what a misspelling looks like and nothing else on this surface can say so.
func TestTheTagListingReportsWhatItLookedAt(t *testing.T) {
	world := []Scope{{ID: control.ID("scp_one000000000"), Name: "alpha-notes", Entries: []Entry{
		{Ref: "runbook", Filename: "runbook.md", Tags: []string{"marketing"}},
		{Ref: "untagged", Filename: "untagged.md"},
	}}}

	hit := pageText(renderNode(t, Page(tagView(t, world, "marketing"))))
	if !strings.Contains(hit, "1 of 2 visible entries in 1 scope carry `marketing`.") {
		t.Errorf("the counted sentence is wrong or missing:\n%s", hit)
	}

	miss := pageText(renderNode(t, Page(tagView(t, world, "no-such-category"))))
	if !strings.Contains(miss, "0 of 2 visible entries in 1 scope carry `no-such-category`.") {
		t.Errorf("a zero over a non-empty store does not report what it looked at:\n%s", miss)
	}
	if !strings.Contains(miss, "This query's operand is checked against no vocabulary") {
		t.Errorf("a zero does not say that a typo looks exactly like this:\n%s", miss)
	}

	// The AUTHORITY zero is a different sentence, because it is a different fact.
	empty := pageText(renderNode(t, Page(tagView(t, nil, "marketing"))))
	if !strings.Contains(empty, "no entry is visible to this credential. That is an authority "+
		"answer, not a fact about the tag.") {
		t.Errorf("a zero over an empty authority reads as a fact about the tag:\n%s", empty)
	}
	if strings.Contains(empty, "This query's operand is checked against no vocabulary") {
		t.Errorf("the authority zero borrowed the typo sentence, so the two mechanisms are "+
			"indistinguishable again:\n%s", empty)
	}
	// ⚠ AND THE SCOPE CARDS ARE WITHHELD WHILE A TAG IS IN FORCE, on the same grounds `?q=`
	// withholds them: a full unfiltered list under a filtered one reads as more matches.
	if strings.Contains(hit, "No scope is visible") {
		t.Error("the empty-cards sentence rendered under a tag listing")
	}
	if !strings.Contains(hit, "Clear the tag and show every scope") {
		t.Errorf("there is no way back from the tag listing:\n%s", hit)
	}
}

// TestTheTagParameterAddsNoRoute is criterion 7's ledger half.
//
// 🔴 A LEDGER COMPARISON, NOT A COUNT. `DeclaredRoutes()` is keyed on `<METHOD> <path>`, so the
// claim is that the SET is byte-identical to what it was before `?tag=` existed — and a count
// would pass a change that swapped one row for another.
//
// ⚠ IT IS AN INVARIANT GUARD ON THE CURRENT SET AND A REGRESSION GUARD ON THE SHAPE. What it
// catches is a later edit that decides `/tag/<name>` reads better: the row would appear here,
// and `TestEveryServedPathComesFromTheLedger` would then have to classify a prefix route.
func TestTheTagParameterAddsNoRoute(t *testing.T) {
	// 🔴 THE `?tag=` PARAMETER IS SERVED, PROVEN BEFORE THE ABSENCE IS ASSERTED. A ledger with
	// no tag row is trivially true of a build that does not read the parameter at all.
	// `staticAuth` + `staticSource{benignWorld()}` is the default dispatch fixture, whose
	// entry carries the tags `benignWorld` now declares — so `?tag=marketing` really has
	// something to find here.
	srv := newTestServer(t, staticAuth{testIdentity()})
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
		RootPath+"?"+url.Values{QueryTag: []string{"marketing"}}.Encode(), nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("`%s?tag=marketing` answered %d, want 200 — the absence below would then be "+
			"about a parameter nothing reads", RootPath, rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "carry `marketing`") {
		t.Fatalf("the root answered 200 but rendered no tag listing, so `?tag=` is not wired:\n%s",
			rec.Body.String())
	}
	for _, row := range DeclaredRoutes() {
		if strings.Contains(row, "tag") {
			t.Errorf("the route ledger gained a tag row (%q). `?tag=` must be a QUERY parameter "+
				"on the existing root row: `routes` is an exact-match map, a tag is user text, "+
				"and a path-segment tag would force prefix matching and falsify the ledger's "+
				"own completeness claim", row)
		}
	}
}

// TestARepeatedTagParameterIsLASTWinsLikeThePod pins WHICH value a repeated `?tag=` uses.
//
// 🔴 REGRESSION COVERAGE, WATCHED RED: with `lastTagValue` replaced by `Query().Get` — which is
// what this line was — both sub-cases below report the FIRST value, and the two assertions fail
// naming the tag they got. The defect was live and shipped: the pod's `lastValue` is last-wins
// and `url.Values.Get` is first-wins, so `?tag=a&tag=b` answered `a` on this surface and `b` on
// `/api/v1/recall/{scope}` while [QueryTag]'s comment claimed a reader "does not have to learn
// that they disagree".
//
// 🔴 THE SCALAR DECISION DOES NOT COVER THIS, WHICH IS WHY THE GUARD IS SEPARATE FROM THE
// `--tag`/`?tag=` SEMANTICS ROWS. Making the operand scalar removed the AND/OR question — HOW
// MANY values are read — and left WHICH one entirely open. A comment asserted the second from
// the first.
//
// 🔴 BOTH ORDERS ARE SENT, AND THAT IS NOT REDUNDANCY. One order cannot distinguish "reads the
// last value" from "always answers `plain-tag`": a single fixture whose expected answer is a
// constant is satisfied by an implementation that hardcodes the constant. Sending the pair and
// requiring the answer to MOVE is the control on that. Both tags are ones `benignWorld`'s entry
// actually carries, so each order renders a real listing and the HEADING — not the hit count —
// is what separates them.
func TestARepeatedTagParameterIsLASTWinsLikeThePod(t *testing.T) {
	// The two tags `benignWorld`'s only entry carries. Distinct from each other, and each
	// distinct from every other literal this test names.
	const first, second = "marketing", "plain-tag"

	for _, order := range [][2]string{{first, second}, {second, first}} {
		sent, want := order, order[1]
		srv := newTestServer(t, staticAuth{testIdentity()})
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
			RootPath+"?"+url.Values{QueryTag: sent[:]}.Encode(), nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("`?tag=%s&tag=%s` answered %d, want 200", sent[0], sent[1], rec.Code)
		}
		body := rec.Body.String()
		if !strings.Contains(body, "carry `"+want+"`") {
			t.Errorf("`?tag=%s&tag=%s` did not narrow by the LAST value %q. The pod's "+
				"`internal/api.lastValue` reads the last value of every scalar parameter, and "+
				"`url.Values.Get` — which this surface used to call — reads the FIRST, so the "+
				"two answered the same URL differently. Body:\n%s",
				sent[0], sent[1], want, body)
		}
		if other := sent[0]; strings.Contains(body, "carry `"+other+"`") {
			t.Errorf("`?tag=%s&tag=%s` narrowed by the FIRST value %q — first-wins, which is "+
				"`Query().Get`'s rule and not the pod's", sent[0], sent[1], other)
		}
	}
}

// tagView builds the root page's state for a `?tag=` request, through the SAME function the
// handler uses.
//
// 🔴 THROUGH `EntriesByTag` AND NEVER BY HAND-BUILDING `TagMatches`. A fixture that assembled
// the struct itself would let every assertion above pass while the real filter was broken — the
// shape this repository calls deriving a test's expectation from nothing.
func tagView(t *testing.T, scopes []Scope, tag string) PageView {
	t.Helper()
	v := viewOf("operator@example.invalid", scopes)
	m := EntriesByTag(scopes, tag)
	v.TagMatches = &m
	return v
}
