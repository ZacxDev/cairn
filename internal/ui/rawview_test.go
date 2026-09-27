package ui

import (
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ZacxDev/cairn/internal/control"
	"github.com/ZacxDev/cairn/internal/report"
	"github.com/ZacxDev/cairn/internal/store"
)

// The entry page's rendered/raw pair, and every guard here is about the SEAM rather than
// about either view on its own.
//
// 🔴 THE QUERY PARAMETER AND ITS VALUE ARE SPELLED AS LITERALS IN THIS FILE, DELIBERATELY,
// AND IT IS THE ONE PLACE IN THIS PACKAGE'S TESTS THAT DOES NOT USE THE `Query*` CONSTANTS.
// `?view=raw` is a WIRE CONTRACT: a URL a reader bookmarks, pastes into a ticket and sends
// to somebody else. A test that wrote `QueryView` would assert that the code agrees with
// itself — rename the constant's value and every guard below follows it silently, which is
// this repository's own rule about deriving an expectation from the implementation it
// tests. The literal is what makes a renamed parameter a RED test rather than a quiet
// break of every URL already in the world.
const (
	rawViewParam = "view"
	rawViewValue = "raw"
)

// The discriminating tokens. Each sits somewhere in the file that the RENDERED view
// structurally cannot show, which is what makes "the raw view shows the file" measurable
// rather than a restatement of "the raw view shows something".
//
// 🔴 A GUARD THAT ONLY CHECKED THE RAW PAGE FOR THE ENTRY'S SURFACED PROSE WOULD PASS ON
// THE RENDERED PAGE TOO, and would therefore be satisfied by a `?view=raw` that silently
// did nothing. These three are the pair's other half: the rendered page must NOT carry
// them.
const (
	// Under a `##` heading that is not in `report.SurfacedHeadings`, so `readEntry` drops
	// the whole section and no rendered element can hold it.
	tokenUnsurfacedSection = "hoistgear"
	// Prose ABOVE the first heading. `store.ExtractSections` keys on headings, so text
	// before the first one belongs to no section.
	tokenPreamble = "flintwork"
	// A front-matter KEY. The VALUES reach the page (`provenance`, the alias list); the
	// `key:` spelling of the file's own YAML does not.
	tokenFrontMatterKey = "quarry-grade"
)

// rawViewStore writes ONE scope with ONE entry whose file carries all three tokens.
//
// ⚠ IT IS A SEPARATE FIXTURE FROM `twoScopeStore` ON PURPOSE. That one's entry bodies are
// tuned to the search-narrowing guards and every line of them is surfaced; adding
// unsurfaced content to it would change what those guards measure. This one exists to be
// partly invisible.
func rawViewStore(t *testing.T) (root string, wholeFile string) {
	t.Helper()
	root = t.TempDir()
	dir := filepath.Join(root, "alpha-notes")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("building the store: %v", err)
	}
	wholeFile = strings.Join([]string{
		"---",
		"service: runbook",
		"scope: alpha-notes",
		tokenFrontMatterKey + ": coarse",
		"aliases:",
		"  - rollout",
		"---",
		"",
		"Some " + tokenPreamble + " prose above the first heading, which belongs to no section.",
		"",
		"## What it is",
		"",
		"The alpha rollout runbook.",
		"",
		"## Operational risk",
		"",
		"The " + tokenUnsurfacedSection + " needs a second operator.",
		"",
		store.NuanceHeading,
		"",
		"- 2000-06-01 an ordinary bullet with no marker at all",
		"",
	}, "\n")
	if err := os.WriteFile(filepath.Join(dir, "runbook.md"), []byte(wholeFile), 0o644); err != nil {
		t.Fatalf("writing the entry: %v", err)
	}

	// INSTRUMENT CONTROL: `## Operational risk` really is unsurfaced. If it were ever
	// added to the surfaced set, `tokenUnsurfacedSection` would appear on BOTH views and
	// the discriminating pair below would be vacuous while still green.
	for _, h := range report.SurfacedHeadings {
		if h == "## Operational risk" {
			t.Fatal("`## Operational risk` is now a SURFACED heading, so it cannot be this " +
				"test's unsurfaced discriminator: the rendered page would carry " +
				"`tokenUnsurfacedSection` too and the raw-vs-rendered comparison would " +
				"measure nothing. Pick a heading the renderer still drops.")
		}
	}
	return root, wholeFile
}

func rawViewEntryPath(scope control.ID, ref string, raw bool) string {
	v := url.Values{QueryScope: []string{string(scope)}, QueryRef: []string{ref}}
	if raw {
		v.Set(rawViewParam, rawViewValue)
	}
	return EntryPath + "?" + v.Encode()
}

// TestTheRawViewShowsWhatTheRenderedViewStructurallyCannot is the feature's own guard, and
// it is a PAIR rather than a single assertion.
//
// 🔴 THE RENDERED HALF IS NOT A COURTESY — IT IS WHAT MAKES THE RAW HALF A MEASUREMENT. A
// `?view=raw` that was ignored entirely would satisfy every "the raw page contains the
// entry's text" assertion anybody would think to write, because the rendered page contains
// the entry's text too. The three tokens below are chosen so that exactly one view can
// hold them.
func TestTheRawViewShowsWhatTheRenderedViewStructurallyCannot(t *testing.T) {
	readsA, _ := twoScopeWorld(t)
	root, wholeFile := rawViewStore(t)
	srv := browseServer(t, root, readsA)

	rendered := getAs(t, srv, rawViewEntryPath(browseScopeA, "runbook", false))
	raw := getAs(t, srv, rawViewEntryPath(browseScopeA, "runbook", true))

	if rendered.Code != http.StatusOK {
		t.Fatalf("the rendered view answered %d, want 200: %s", rendered.Code, rendered.Body.String())
	}
	if raw.Code != http.StatusOK {
		t.Fatalf("the raw view answered %d, want 200: %s", raw.Code, raw.Body.String())
	}

	rawBody, renderedBody := raw.Body.String(), rendered.Body.String()

	for _, token := range []string{tokenPreamble, tokenUnsurfacedSection, tokenFrontMatterKey} {
		if !strings.Contains(rawBody, token) {
			t.Errorf("the RAW view does not carry %q, which is in the file. The raw view is "+
				"supposed to be the file's own bytes; a raw view missing a line of the file "+
				"is showing something else.", token)
		}
		if strings.Contains(renderedBody, token) {
			t.Errorf("the RENDERED view carries %q. That token was chosen because the renderer "+
				"structurally cannot surface it, so either the renderer changed or this test's "+
				"discriminators no longer discriminate — and if they do not, every raw-view "+
				"assertion here passes whether or not the raw view works.", token)
		}
	}

	// The whole file, not merely the tokens. This is what "raw" claims, and asserting the
	// tokens alone would be satisfied by a view that showed three lines of it.
	//
	// ⚠ COMPARED AFTER HTML-ESCAPING, because the page is HTML and `<` in the file is
	// `&lt;` in the response. The escaping itself is pinned separately, below.
	if !strings.Contains(rawBody, htmlEscapeForTest(strings.TrimRight(wholeFile, "\n"))) {
		t.Error("the raw view does not carry the file's whole text as one contiguous block. It " +
			"is showing a transformation of the file rather than the file.")
	}
}

// TestBothEntryViewsOfferTheOtherOneAndMarkTheCurrentOne is the navigation half.
//
// ⚠ IT IS A SEPARATE GUARD FROM THE CONTENT ONE BECAUSE THEY FAIL SEPARATELY: a raw view
// that renders perfectly and is unreachable is the exact defect this repository already
// has an open entry for on the share flow — deployed, authorised and linked from nothing.
func TestBothEntryViewsOfferTheOtherOneAndMarkTheCurrentOne(t *testing.T) {
	readsA, _ := twoScopeWorld(t)
	root, _ := rawViewStore(t)
	srv := browseServer(t, root, readsA)

	renderedHref := rawViewEntryPath(browseScopeA, "runbook", false)
	rawHref := rawViewEntryPath(browseScopeA, "runbook", true)

	for _, tc := range []struct {
		name string
		at   string
		// here is the tab that must be marked as the current one, and there is the tab
		// that must be a link.
		here, there string
	}{
		{"the rendered view", renderedHref, "rendered", "raw"},
		{"the raw view", rawHref, "raw", "rendered"},
	} {
		rec := getAs(t, srv, tc.at)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s answered %d, want 200: %s", tc.name, rec.Code, rec.Body.String())
		}
		body := rec.Body.String()

		// The OTHER view is reachable from this one. Asserted as an `href` to the other
		// view's own URL rather than as the word: a page carrying the word "raw" in prose
		// would satisfy a word check while linking nowhere.
		wantHref := rawHref
		if tc.there == "rendered" {
			wantHref = renderedHref
		}
		if !strings.Contains(body, `href="`+htmlEscapeForTest(wantHref)+`"`) {
			t.Errorf("%s does not link the %s view (want href %q). A view nothing navigates to "+
				"is a view nobody finds.", tc.name, tc.there, wantHref)
		}

		// And the CURRENT one is marked rather than linking to itself, which is
		// `breadcrumbs`' own rule for the same shape.
		if !strings.Contains(body, "view-tab-here") {
			t.Errorf("%s marks no tab as the current one, so both tabs read as somewhere else "+
				"to go and neither says where the reader is.", tc.name)
		}
		if strings.Contains(body, `href="`+htmlEscapeForTest(tc.at)+`"`) {
			t.Errorf("%s links to ITSELF (%q). The current tab is state, not a destination.",
				tc.name, tc.at)
		}
	}
}

// TestAnUnrecognisedViewValueRendersTheRenderedView pins the default, in both directions.
//
// ⚠ THE DEFAULT IS "RENDERED" AND NOT A REFUSAL, WHICH IS `handlePage`'s RULING FOR `?q=`
// RESTATED: a view selector is not an authority question, so a value nobody recognises is
// answered with the page rather than with a 404. The cost is that a typo is silent, and
// that is stated rather than discovered — it is why exactly ONE value is recognised and
// why the recognised spelling is pinned as a literal at the top of this file.
func TestAnUnrecognisedViewValueRendersTheRenderedView(t *testing.T) {
	readsA, _ := twoScopeWorld(t)
	root, _ := rawViewStore(t)
	srv := browseServer(t, root, readsA)

	base := url.Values{QueryScope: []string{string(browseScopeA)}, QueryRef: []string{"runbook"}}
	for _, value := range []string{"", "RAW", "raw ", "rendered", "source", "raw-ish"} {
		v := url.Values{}
		for k, vs := range base {
			v[k] = vs
		}
		v.Set(rawViewParam, value)
		rec := getAs(t, srv, EntryPath+"?"+v.Encode())
		if rec.Code != http.StatusOK {
			t.Fatalf("?%s=%q answered %d, want 200: %s", rawViewParam, value, rec.Code, rec.Body.String())
		}
		if strings.Contains(rec.Body.String(), tokenUnsurfacedSection) {
			t.Errorf("?%s=%q rendered the RAW view. Exactly one value selects it and that value "+
				"is %q; anything else is the rendered view, so that a URL nobody meant cannot "+
				"change what the page shows.", rawViewParam, value, rawViewValue)
		}
	}

	// 🔴 THE POSITIVE CONTROL. The recognised value DOES select the raw view, so the loop
	// above is not measuring a `?view=` that never works at all.
	ok := getAs(t, srv, rawViewEntryPath(browseScopeA, "runbook", true))
	if !strings.Contains(ok.Body.String(), tokenUnsurfacedSection) {
		t.Fatalf("POSITIVE CONTROL FAILED: ?%s=%s did NOT render the raw view, so every "+
			"assertion above is satisfied by a parameter that is ignored outright.",
			rawViewParam, rawViewValue)
	}
}

// TestAHostileFileReachesTheRawViewAsTextThroughTheREALPIPELINE is the half of the
// escaping story the renderer differential structurally cannot see.
//
// 🔴 WHAT MOVED AND WHY, BECAUSE DELETING A GUARD DESERVES MORE THAN A DIFF. This test
// used to hand-roll four `strings.Contains` checks over the response. Those now live in
// `render_test.go`'s `TestHostileEntryTextIsEscapedOnEveryBrowsePage`, which renders the
// SAME page over a hostile and a benign world of identical shape and compares MARKUP
// SHAPE — a strictly stronger instrument than substring matching, because it detects an
// injection the author never thought to grep for. `renderedPages` now carries an
// `entry-raw` row and both worlds carry a `Raw` fixture, and that row was mutation-tested:
// `g.Raw(e.Raw)` makes it RED naming the state and the `<script` count.
//
// ⚠ WHAT IT CANNOT SEE IS WHY THIS REMAINS. That differential builds `Entry.Raw` from a
// FIXTURE. Nothing in it exercises `readEntry` reading a real file off disk, decoding it
// and putting it on the struct, nor the HTTP layer carrying it out unchanged — so a defect
// anywhere in that chain would leave every escaping guard green. This drives a hostile
// FILE through the whole pipeline and asserts the two things that survive it.
func TestAHostileFileReachesTheRawViewAsTextThroughTheREALPIPELINE(t *testing.T) {
	readsA, _ := twoScopeWorld(t)
	root := t.TempDir()
	dir := filepath.Join(root, "alpha-notes")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("building the store: %v", err)
	}
	const hostile = `<script>alert(document.domain)</script> and "quoted" & 'single' <img src=x onerror=alert(1)>`
	body := strings.Join([]string{
		"---", "service: runbook", "scope: alpha-notes", "---", "",
		"## What it is", "", hostile, "",
		store.NuanceHeading, "", "- 2000-06-01 a bullet carrying " + hostile, "",
	}, "\n")
	if err := os.WriteFile(filepath.Join(dir, "runbook.md"), []byte(body), 0o644); err != nil {
		t.Fatalf("writing the hostile entry: %v", err)
	}

	srv := browseServer(t, root, readsA)
	rec := getAs(t, srv, rawViewEntryPath(browseScopeA, "runbook", true))
	if rec.Code != http.StatusOK {
		t.Fatalf("the raw view of the hostile entry answered %d, want 200: %s", rec.Code, rec.Body.String())
	}
	page := rec.Body.String()

	// INSTRUMENT CONTROL: the file's text reached the page at all. Without this the
	// assertions below are satisfied by a pipeline that rendered nothing.
	if !strings.Contains(page, "alert(document.domain)") {
		t.Fatal("the hostile file's text is not on the page in any form, so the assertions below " +
			"would pass over an empty raw view — the pipeline, not the escaping, is what failed")
	}
	if !strings.Contains(page, "&lt;script&gt;") {
		t.Error("the file's `<script>` is neither escaped nor present, so what the pipeline did " +
			"with it is unknown — which is not a pass")
	}
	// 🔴 ZERO SCRIPT ELEMENTS IN WHAT THIS ORIGIN RENDERS, counted over the whole HTTP
	// response rather than over a rendered node — the shell included, which is the part
	// the renderer differential does not assemble. Scope it honestly: this says nothing
	// about bytes an edge inserts downstream, which is `#130`'s correction.
	if n := strings.Count(strings.ToLower(page), "<script"); n != 0 {
		t.Errorf("the raw view's RESPONSE carries %d `<script` occurrence(s), want 0", n)
	}
}

// htmlEscapeForTest is the escaping the ASSERTIONS need, written out here rather than
// imported from the renderer.
//
// 🔴 IT IS A SECOND IMPLEMENTATION ON PURPOSE, WHICH IS THE ONE CASE THIS REPOSITORY'S
// "one rule, one place" RULE DOES NOT COVER. Escaping the expectation with the same
// function the page escapes with would make every assertion below a tautology: any bug in
// that function would move both sides together and the comparison would still pass.
// `html.EscapeString` is deliberately NOT used for the same reason — it is what gomponents
// itself calls.
func htmlEscapeForTest(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '&':
			b.WriteString("&amp;")
		case '<':
			b.WriteString("&lt;")
		case '>':
			b.WriteString("&gt;")
		case '"':
			b.WriteString("&#34;")
		case '\'':
			b.WriteString("&#39;")
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}
