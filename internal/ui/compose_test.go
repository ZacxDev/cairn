package ui

import (
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ZacxDev/cairn/internal/control"
	"github.com/ZacxDev/cairn/internal/identity"
	"github.com/ZacxDev/cairn/internal/store"
)

// The composition world. Every name in it is INVENTED — this repository is public and was
// extracted from a private one.
var (
	composeReader   = control.DerivedID(control.PrefixUser, "compose-reader")
	composeProject  = control.DerivedID(control.PrefixProject, "compose-both")
	composeScopeOne = control.DerivedID(control.PrefixScope, "alpha-notes")
	composeScopeTwo = control.DerivedID(control.PrefixScope, "beta-notes")
)

// composeWord is the search operand, and it appears in THREE of the five fixture entries —
// deliberately NOT the same three the tag selects.
//
// 🔴 THE TWO FILTERS MUST DISAGREE OR THE COMPOSED ANSWER IS UNATTRIBUTABLE. If `?q=` and
// `?tag=` picked the same entries, a build that silently ignored one of them would produce the
// correct composed page and every assertion below would pass. The fixture is built so the three
// answers are three DIFFERENT sets, and `TestTheQueryAndTheTagComposeIntoOneCard` checks all
// three rather than only the composed one.
const composeWord = "quarrying"

// composeTag is the category operand. Two of the five entries carry it, one of which also
// carries `composeWord` — so the intersection is exactly one entry.
const composeTag = "marketing"

// composeWorld is ONE principal who reads BOTH scopes, which is what a store-wide composed
// search needs.
//
// ⚠ IT IS NOT `twoScopeWorld`. That world's two principals read one scope each, which is right
// for the authority guards it serves and wrong here: a composition over a single scope could not
// see a tag filter that forgot to cross a scope boundary.
func composeWorld(t *testing.T) identity.Identity {
	t.Helper()
	at := browseClock
	m, err := control.Replay([]control.Event{
		{Kind: control.EventUserCreated, At: at, UserID: composeReader,
			Provider: "fixture-provider", Subject: "00000000-0000-4000-8000-000000000041",
			Email: "composer@notes.example.invalid"},
		{Kind: control.EventProjectCreated, At: at, ProjectID: composeProject, Name: "both", UserID: composeReader},
		{Kind: control.EventMemberSet, At: at, ProjectID: composeProject, UserID: composeReader, Role: control.RoleOwner},
		{Kind: control.EventScopeCreated, At: at, ScopeID: composeScopeOne, DisplayName: "alpha-notes", ProjectID: composeProject},
		{Kind: control.EventScopeCreated, At: at, ScopeID: composeScopeTwo, DisplayName: "beta-notes", ProjectID: composeProject},
	})
	if err != nil {
		t.Fatalf("building the composition world: %v", err)
	}
	p, known := m.PrincipalFor(control.KindUser, composeReader)
	if !known {
		t.Fatal("the world does not hold the composition principal")
	}
	id := identity.Identity{Principal: p, Auth: control.Resolve(m, p)}

	// INSTRUMENT CONTROL: the principal really reads BOTH scopes. Without it, a composed
	// answer of one entry could be a one-scope authority answer wearing a filter's clothes.
	if !id.Auth.Allows(composeScopeOne, control.VerbRead) || !id.Auth.Allows(composeScopeTwo, control.VerbRead) {
		t.Fatal("the composition principal does not read both scopes, so the counts below are " +
			"about a narrower world than the one this fixture describes")
	}
	return id
}

// composeStore writes five entries across two scopes, in the one arrangement that makes the
// three answers distinguishable:
//
//	ref        scope         carries `marketing`   contains `quarrying`
//	runbook    alpha-notes   yes                   yes      <- the INTERSECTION
//	ledger     alpha-notes   yes                   no       <- tag only
//	charter    beta-notes    no  (engineering)     yes      <- query only
//	untagged   beta-notes    no  (no `tags:`)      yes      <- query only, and no tags at all
//	appendix   beta-notes    no  (no `tags:`)      no       <- neither; see below
//
// So `?q=` alone finds three entries, `?tag=` alone lists two, and the two together name ONE.
//
// 🔴 `appendix` MATCHES NEITHER FILTER, AND IT IS THERE SO NO SUB-TEST'S COUNTS COLLIDE.
// Without it the intersection sub-test's two counts are BOTH 2, so a mutant swapping
// `EntriesSearched` for `TagSkipped` renders that sentence byte-identically and that sub-test
// cannot see it. The fifth entry makes the pair 2 and 3 — pairwise distinct, and distinct from
// the hit count of 1.
//
// ⚠ AND THE MEASUREMENT CORRECTED THE DRAFT OF THIS COMMENT, WHICH SAID THE SWAP WOULD HAVE
// SURVIVED A GREEN SUITE. It would not have: the swap mutant was run against BOTH fixtures, and
// on the four-entry one the composed-ZERO sub-test still killed it (its counts are 1 and 3, not
// 2 and 2). What the fifth entry buys is each sub-test seeing the swap on its OWN assertion
// instead of one of them passing vacuously — worth having, and a smaller claim than the one
// written first.
func composeStore(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	write := func(scope, name string, lines ...string) {
		dir := filepath.Join(root, scope)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("building the store: %v", err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(strings.Join(lines, "\n")), 0o644); err != nil {
			t.Fatalf("writing %s/%s: %v", scope, name, err)
		}
	}
	// The bullet shape is `twoScopeStore`'s: the operand lands inside a NUANCE bullet and in
	// no ref, alias or heading, so a hit is a hit on a line rather than on an entry name.
	body := func(service, scope string, tags []string, bullet string) []string {
		out := []string{"---", "service: " + service, "scope: " + scope}
		if len(tags) > 0 {
			out = append(out, "tags:")
			for _, tag := range tags {
				out = append(out, "  - "+tag)
			}
		}
		return append(out,
			"---",
			"",
			"## What it is",
			"",
			"A synthetic entry.",
			"",
			store.NuanceHeading,
			"",
			"- 2000-06-01 "+bullet,
			"",
		)
	}
	write("alpha-notes", "runbook.md", body("runbook", "alpha-notes",
		[]string{composeTag}, "the "+composeWord+" step is still unautomated.")...)
	write("alpha-notes", "ledger.md", body("ledger", "alpha-notes",
		[]string{composeTag}, "reconciliation runs nightly.")...)
	write("beta-notes", "charter.md", body("charter", "beta-notes",
		[]string{"engineering"}, "the "+composeWord+" rota is published monthly.")...)
	write("beta-notes", "untagged.md", body("untagged", "beta-notes",
		nil, "the "+composeWord+" budget is unowned.")...)
	write("beta-notes", "appendix.md", body("appendix", "beta-notes",
		nil, "an unrelated note about nothing in particular.")...)
	return root
}

// composeRefs is every ref the fixture holds, so an assertion can say which ones are ABSENT
// without hand-listing the complement at each call site.
var composeRefs = []string{"runbook", "ledger", "charter", "untagged", "appendix"}

// namesOnly asserts the page names exactly the wanted refs out of `composeRefs`.
//
// ⚠ IT CHECKS BOTH DIRECTIONS. Asserting only the presences passes for a page that renders
// every entry it can see, which is precisely the pre-change two-card answer.
func namesOnly(t *testing.T, label, markup string, want ...string) {
	t.Helper()
	wanted := map[string]bool{}
	for _, w := range want {
		wanted[w] = true
	}
	for _, ref := range composeRefs {
		// The ref is rendered as link text and inside an `?ref=` operand; either spelling
		// counts as naming it.
		got := strings.Contains(markup, ">"+ref+"<") || strings.Contains(markup, "ref="+ref)
		if wanted[ref] && !got {
			t.Errorf("%s: the page does not name `%s`, which it must", label, ref)
		}
		if !wanted[ref] && got {
			t.Errorf("%s: the page names `%s`, which neither filter admits", label, ref)
		}
	}
}

// resultCards counts the answer cards in the markup.
//
// 🔴 COUNTED IN THE MARKUP AND NEVER OFF THE VIEW, which is what makes "one card" a claim about
// what a reader sees. `handlePage`'s `switch` sets exactly one of `Results`/`TagMatches`, but
// `Page` still reads both pointers independently — so an edit that set both would restore the
// two-card answer with nothing in the handler to read as wrong.
func resultCards(markup string) int { return strings.Count(markup, `class="card results"`) }

// TestTheQueryAndTheTagComposeIntoOneCard is the closing guard for the declared
// `?q=`-and-`?tag=`-do-not-compose limitation.
//
// 🔴 THE RED/GREEN MATRIX, MEASURED RATHER THAN ASSERTED. At `ffa0eca` — the commit this branch
// forked from — this file does not compile, because `Source.Search` took no tag there; that is
// the weak red a new argument always produces. The INFORMATIVE red is the one that keeps the new
// shape and removes the new behaviour: with `handlePage`'s `switch` replaced by the two
// independent `if`s it used to be, and the tag dropped on the way into `s.source.Search`, the
// composed sub-test fails on THREE counts — `resultCards` is 2, the page names `ledger` and
// `charter`, and the summary carries no tag clause. Both reds were run; the second is the one
// that attributes.
//
// 🔴 THREE ANSWERS, NOT ONE, AND THAT IS NOT REDUNDANCY. A composed page naming exactly
// `runbook` is also what a build that ignored the QUERY would render if the tag happened to
// select one entry, and what a build that ignored the TAG would render if the query did. The two
// single-operand sub-tests are what make the third attributable: they pin that each filter alone
// answers a DIFFERENT, larger set, so the composed answer is the intersection rather than either
// operand acting alone.
func TestTheQueryAndTheTagComposeIntoOneCard(t *testing.T) {
	id := composeWorld(t)
	srv := browseServer(t, composeStore(t), id)

	get := func(t *testing.T, params url.Values) string {
		t.Helper()
		rec := getAs(t, srv, ScopesPath+"?"+params.Encode())
		if rec.Code != http.StatusOK {
			t.Fatalf("GET /?%s answered %d, want 200: %s", params.Encode(), rec.Code, rec.Body.String())
		}
		return rec.Body.String()
	}

	t.Run("the query alone finds three entries", func(t *testing.T) {
		markup := get(t, url.Values{QueryQuery: []string{composeWord}})
		if n := resultCards(markup); n != 1 {
			t.Fatalf("a bare `?q=` rendered %d answer cards, want 1", n)
		}
		namesOnly(t, "query alone", markup, "runbook", "charter", "untagged")
		// POSITIVE CONTROL on the composed clause: it must be ABSENT here, or its presence
		// below says nothing about a tag having been applied.
		if strings.Contains(pageText(markup), "narrowed this search") {
			t.Error("an untagged search claims a tag narrowed it")
		}
	})

	t.Run("the tag alone lists two entries", func(t *testing.T) {
		markup := get(t, url.Values{QueryTag: []string{composeTag}})
		if n := resultCards(markup); n != 1 {
			t.Fatalf("a bare `?tag=` rendered %d answer cards, want 1", n)
		}
		namesOnly(t, "tag alone", markup, "runbook", "ledger")
		if !strings.Contains(pageText(markup), "2 of 5 visible entries in 2 scopes carry `"+composeTag+"`.") {
			t.Errorf("the tag listing's counted sentence is wrong or missing:\n%s", pageText(markup))
		}
	})

	t.Run("together they are ONE card over the intersection", func(t *testing.T) {
		markup := get(t, url.Values{
			QueryQuery: []string{composeWord},
			QueryTag:   []string{composeTag},
		})
		if n := resultCards(markup); n != 1 {
			t.Fatalf("`?q=&tag=` rendered %d answer cards, want exactly 1. Two cards is the "+
				"declared pre-change answer: a search over everything beside a listing of "+
				"everything tagged, neither narrowing the other.\n%s", n, markup)
		}
		namesOnly(t, "composed", markup, "runbook")

		text := pageText(markup)
		// 🔴 THE SUMMARY NAMES BOTH OPERANDS AND BOTH COUNTS, pinned as the whole sentence
		// rather than by the presence of the tag's name. 5 visible entries, 2 carry the tag,
		// 1 of those matches — every number here is read off the fixture table, not off the
		// implementation, and 1/2/3 are pairwise distinct so a swapped pair cannot survive.
		want := "The tag `" + composeTag + "` narrowed this search to 2 entries before the " +
			"query ran, leaving out 3 visible entries that do not carry it."
		if !strings.Contains(text, want) {
			t.Errorf("the composed summary does not name both operands.\nwant substring: %s\ngot:\n%s", want, text)
		}
		// It is still a SEARCH card: the heading and the ranked shape are the pod's
		// composition, not a tag listing that got filtered.
		if !strings.Contains(markup, "<h2>Search</h2>") {
			t.Errorf("the composed card is not the search card:\n%s", markup)
		}

		// 🔴 AND THE CARD-WHAT SENTENCE IS PINNED WHOLE, AGAINST A HAND-TYPED LITERAL rather
		// than against `searchWithinTagWhat`. Comparing the page to the constant it renders
		// moves both sides of the comparison together and cannot see a reword at all — the
		// defect already recorded for `RefsKeyDescription` and `ReplicaHonesty`. The claim
		// being pinned is a NARROWING: the uncomposed sentence says the engine scored every
		// entry the credential can read, which under a tag is false.
		const wantWhat = "Scored over every line of every entry the credential can read " +
			"THAT CARRIES THIS TAG, by the same engine `cairn search --tag` uses — so a word " +
			"inside a bullet is findable, not just a word in a title."
		if !strings.Contains(text, wantWhat) {
			t.Errorf("the composed card's card-what sentence is wrong or missing.\nwant: %s\ngot:\n%s",
				wantWhat, text)
		}
		if strings.Contains(text, "Scored over every line of every entry the credential can read, by the") {
			t.Error("the composed card claims it scored every readable entry, which the tag " +
				"filter makes false — the over-claiming sentence is the uncomposed one")
		}
	})

	t.Run("a tag that selects nothing the query matches is a readable zero", func(t *testing.T) {
		// `engineering` + a word that only `marketing` entries carry: the composed answer is
		// empty, and the summary is what says WHY.
		markup := get(t, url.Values{
			QueryQuery: []string{"reconciliation"},
			QueryTag:   []string{"engineering"},
		})
		if n := resultCards(markup); n != 1 {
			t.Fatalf("a composed zero rendered %d answer cards, want 1", n)
		}
		namesOnly(t, "composed zero", markup)
		text := pageText(markup)
		if !strings.Contains(text, "narrowed this search to 1 entry before the query ran, "+
			"leaving out 4 visible entries that do not carry it") {
			t.Errorf("a composed zero does not say what it ran over, so it cannot be told "+
				"apart from a store with nothing in it:\n%s", text)
		}
	})
}

// TestTheSearchFormRoundTripsTheTag is the second half of the closing condition: a reader
// looking at a tag listing who types into the box keeps the tag.
//
// 🔴 WATCHED RED: before this change `searchbar` carried `name="q"` and nothing else, so the
// hidden-input assertion fails in both the listing and the composed state, and the tag is
// discarded at the one moment it is visible on screen.
//
// ⚠ THE NEGATIVE CASE IS HALF THE GUARD. A form that always rendered a `tag` input would pass
// every presence assertion and would put `&tag=` on every search URL a reader shares.
func TestTheSearchFormRoundTripsTheTag(t *testing.T) {
	id := composeWorld(t)
	srv := browseServer(t, composeStore(t), id)
	hidden := `<input type="hidden" name="` + QueryTag + `" value="` + composeTag + `">`

	t.Run("a tag listing carries it", func(t *testing.T) {
		markup := getAs(t, srv, ScopesPath+"?"+url.Values{QueryTag: []string{composeTag}}.Encode()).Body.String()
		if !strings.Contains(markup, hidden) {
			t.Errorf("the search form on a tag listing does not carry the tag, so typing into "+
				"the box discards it.\nwant: %s\ngot:\n%s", hidden, markup)
		}
	})

	t.Run("a composed page carries it", func(t *testing.T) {
		markup := getAs(t, srv, ScopesPath+"?"+url.Values{
			QueryQuery: []string{composeWord},
			QueryTag:   []string{composeTag},
		}.Encode()).Body.String()
		if !strings.Contains(markup, hidden) {
			t.Errorf("the search form on a composed page does not carry the tag, so refining "+
				"the words drops the filter.\nwant: %s\ngot:\n%s", hidden, markup)
		}
	})

	t.Run("a page with no tag carries no tag input", func(t *testing.T) {
		for _, path := range []string{
			ScopesPath,
			ScopesPath + "?" + url.Values{QueryQuery: []string{composeWord}}.Encode(),
		} {
			markup := getAs(t, srv, path).Body.String()
			if strings.Contains(markup, `name="`+QueryTag+`"`) {
				t.Errorf("%s renders a `%s` form control with no tag in force, so every search "+
					"URL a reader shares from here carries a parameter they never chose:\n%s",
					path, QueryTag, markup)
			}
		}
	})

	t.Run("the folded operand is what rides along", func(t *testing.T) {
		// `/?tag=Marketing` reaches the same entries as `/?tag=marketing`, and the form must
		// carry the FOLDED spelling — otherwise the next submission re-folds a string the
		// page already folded, and the two spellings diverge the day the fold changes.
		markup := getAs(t, srv, ScopesPath+"?"+url.Values{QueryTag: []string{"MARKETING"}}.Encode()).Body.String()
		if !strings.Contains(markup, hidden) {
			t.Errorf("an unfolded `?tag=` round-trips unfolded.\nwant: %s\ngot:\n%s", hidden, markup)
		}
	})
}

// TestTheComposedCardOffersEveryWayBack pins the navigation out of a two-filter state.
//
// 🔴 THERE ARE THREE NEIGHBOURS AND THIS SURFACE HAS NO SCRIPT, so a state no link names is a
// state a reader reaches by editing the URL. The pre-change two-card answer had one link per
// card and no way to drop one filter while keeping the other.
func TestTheComposedCardOffersEveryWayBack(t *testing.T) {
	id := composeWorld(t)
	srv := browseServer(t, composeStore(t), id)
	markup := getAs(t, srv, ScopesPath+"?"+url.Values{
		QueryQuery: []string{composeWord},
		QueryTag:   []string{composeTag},
	}.Encode()).Body.String()

	// The hrefs are built the way the page builds them — through the encoders — so this
	// asserts the DESTINATIONS rather than a second spelling of the URL grammar.
	for _, want := range []struct {
		href, what string
	}{
		{searchHref(composeWord), "drop the tag, keep the words"},
		{tagHref(composeTag), "drop the words, keep the tag"},
		{ScopesPath, "drop both"},
	} {
		if !strings.Contains(markup, `href="`+want.href+`"`) {
			t.Errorf("the composed card offers no way to %s (no link to %q):\n%s",
				want.what, want.href, markup)
		}
	}

	// ⚠ AND THE BARE-ROOT LINK SAYS IT CLEARS BOTH. "Clear the search and show every scope"
	// beside a composed answer describes one of the two things the link does.
	text := pageText(markup)
	if !strings.Contains(text, "Clear the search AND the tag, and show every scope") {
		t.Errorf("the clear-everything link does not say it also drops the tag:\n%s", text)
	}

	// The UNCOMPOSED search card keeps the label it has always had, so this is a composed-state
	// change rather than a reword of every search page.
	plain := pageText(getAs(t, srv, ScopesPath+"?"+url.Values{QueryQuery: []string{composeWord}}.Encode()).Body.String())
	if !strings.Contains(plain, "Clear the search and show every scope") {
		t.Errorf("an untagged search card lost its own way back:\n%s", plain)
	}
}
