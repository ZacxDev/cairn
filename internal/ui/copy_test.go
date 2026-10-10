package ui

import (
	"net/http"
	"regexp"
	"strings"
	"testing"
)

// The operator's copy decisions, pinned STRUCTURALLY: what was removed is absent, and what
// replaced it is present by CLASS and ATTRIBUTE rather than by spelling — so a later reword of a
// tooltip is not a failure here, and a page that lost its chips is.

// classLists returns the class attribute of every `<tag class="…">` in body, split into tokens.
func classLists(body, tag string) [][]string {
	var out [][]string
	re := regexp.MustCompile(`<` + tag + ` class="([^"]*)"`)
	for _, m := range re.FindAllStringSubmatch(body, -1) {
		out = append(out, strings.Fields(m[1]))
	}
	return out
}

// hasClassSet reports whether some `<tag>` in body carries EVERY class in want.
func hasClassSet(body, tag string, want ...string) bool {
	for _, classes := range classLists(body, tag) {
		all := true
		for _, w := range want {
			found := false
			for _, c := range classes {
				if c == w {
					found = true
				}
			}
			all = all && found
		}
		if all {
			return true
		}
	}
	return false
}

// TestNoPageCarriesALegendOrTheScopeExplainer pins the SECOND round of the operator's trim: the
// entry page's "What am I looking at?" legend (both views) and the scope explainer — on the root
// cards AND the scope page — are gone, asserted by ELEMENT and CLASS rather than by sentence, and
// the scope definition survives as a `title=` on the root card's "scope" label.
func TestNoPageCarriesALegendOrTheScopeExplainer(t *testing.T) {
	root, id, orchard := recencyWorld(t)
	srv := recencyServer(t, root, id)
	pages := map[string]string{
		// "root" is the scope LIST, which was the root until the root became the hub; the hub is walked
		// too, for the no-legend half.
		"root":      ScopesPath,
		"hub":       RootPath,
		"scope":     ScopePath + "?" + QueryID + "=" + string(orchard),
		"entry":     entryHref(orchard, "birch", false),
		"entry raw": entryHref(orchard, "birch", true),
	}
	bodies := map[string]string{}
	for name, path := range pages {
		rec := getAs(t, srv, path)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s answered %d", name, rec.Code)
		}
		bodies[name] = rec.Body.String()
		// No legend of any kind: no `<details>` at all on a browse page, and no legend class.
		if strings.Contains(bodies[name], "<details") || hasClassSet(bodies[name], "details", "legend") {
			t.Errorf("the %s page still carries a <details> legend", name)
		}
	}
	// No explainer paragraph on the root cards, nor in the scope page's own card (its head, between
	// the heading and the filter control). The sessions/arcs cards further down keep theirs — they
	// were not part of this decision — which is why the scope page is checked in that span only.
	if hasClassSet(bodies["root"], "p", "card-what") {
		t.Error("the root page still renders a card-what explainer")
	}
	scopeHead := bodies["scope"]
	a, b := strings.Index(scopeHead, "<h2>orchard-notes</h2>"), strings.Index(scopeHead, `id="entry-filter-control"`)
	if a < 0 || b < a {
		t.Fatalf("the scope page's head span was not found (h2 at %d, filter at %d)", a, b)
	}
	if hasClassSet(scopeHead[a:b], "p", "card-what") {
		t.Error("the scope page's own card still renders a card-what explainer")
	}
	// POSITIVE CONTROL on the `card-what` probe: the raw view still renders its own explainer, so
	// the class scan above can see one when it is there.
	if !hasClassSet(bodies["entry raw"], "p", "card-what") {
		t.Fatal("the raw view's `card-what` explainer is not found, so the absence check above is vacuous " +
			"(or `entryRawWhat` was removed, which this change did not ask for)")
	}
	// The definition moved to a tooltip on the root card's kind label.
	if !regexp.MustCompile(`<span class="kind" title="[^"]+">scope</span>`).MatchString(bodies["root"]) {
		t.Error("the root card's \"scope\" label carries no title= tooltip")
	}
}

func TestTheRemovedDefinitionCopyIsAbsentAndChipsArePresent(t *testing.T) {
	root, id, orchard := recencyWorld(t)
	srv := recencyServer(t, root, id)
	get := func(path string) string {
		t.Helper()
		rec := getAs(t, srv, path)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s answered %d", path, rec.Code)
		}
		return rec.Body.String()
	}
	rootBody := get(ScopesPath)
	scopeBody := get(ScopePath + "?" + QueryID + "=" + string(orchard))
	entryBody := get(entryHref(orchard, "birch", false))

	// INSTRUMENT CONTROL: the entry carries aliases, refs, tags and history notes, so every list
	// below has a reason to render and an absence is not a fixture with nothing in it.
	for _, must := range []string{"birch-alias", "example-repo", "timber", "the first history note"} {
		if !strings.Contains(entryBody, must) {
			t.Fatalf("the entry page lacks %q, so the assertions below are about an empty fixture", must)
		}
	}

	// --- Removed: the root and scope legends, and every definition line. ---
	for where, body := range map[string]string{"root": rootBody, "scope": scopeBody} {
		if hasClassSet(body, "details", "legend") {
			t.Errorf("the %s page still carries a definition legend", where)
		}
	}
	for _, gone := range []string{
		// the rendered entry view's explainer
		"One entry file. The sections below are its",
		// the three definition lines under Aliases / Refs / Tags
		"front-matter key, FOLDED",
		"the vocabulary is CLOSED on the write path",
		"(or the accepted older",
		"the `aliases:` front-matter key, as written",
	} {
		if strings.Contains(entryBody, gone) {
			t.Errorf("the entry page still carries %q", gone)
		}
	}
	// No visible definition paragraph under a labelled list's heading.
	if regexp.MustCompile(`<div class="labelled"><h3[^>]*>[^<]*</h3><p`).MatchString(entryBody) {
		t.Error("a labelled list still renders a paragraph under its heading")
	}
	// …and the short definitions moved INTO the headings as tooltips.
	for _, h := range []string{"Aliases", "Refs", "Tags"} {
		if !regexp.MustCompile(`<h3 class="section-head" title="[^"]+">` + h + `</h3>`).MatchString(entryBody) {
			t.Errorf("the %s heading carries no title= tooltip", h)
		}
	}

	// --- Chips: structurally, by class, on BOTH the entry page and the scope rows. ---
	for where, body := range map[string]string{"entry page": entryBody, "scope rows": scopeBody} {
		for _, set := range [][]string{
			{"aliases", "chips", "chips-alias"},
			{"tasks", "chips", "chips-ref"},
			{"tags", "chips", "chips-tag"},
		} {
			if !hasClassSet(body, "ul", set...) {
				t.Errorf("%s: no <ul> carries %v", where, set)
			}
		}
	}
	// Tags stay LINKS to the tag filter; aliases are NOT links.
	if !strings.Contains(entryBody, `<li class="tag"><a href="/scopes?tag=timber">timber</a></li>`) {
		t.Error("a tag chip is not a link to its tag filter")
	}
	if !strings.Contains(entryBody, `<ul class="aliases chips chips-alias"><li>birch-alias</li></ul>`) {
		t.Error("an alias chip is not plain, unlinked text")
	}

	// --- "journal bullets" → History. ---
	if !strings.Contains(entryBody, `<h3 class="section-head" title="## Nuance / work-history">History</h3>`) {
		t.Error("the nuance section is not displayed as History with the file's heading as its tooltip")
	}
	for where, body := range map[string]string{"entry": entryBody, "scope": scopeBody} {
		if strings.Contains(body, "journal bullet") {
			t.Errorf("the %s page still says \"journal bullet\"", where)
		}
	}
	if strings.Contains(entryBody, "2 history notes") {
		t.Error("the entry page repeats the bullet count the History section below it already shows")
	}
	if n := strings.Count(scopeBody, ">2 history notes</span>"); n != 5 {
		t.Errorf("the scope page shows \"2 history notes\" on %d row(s), want all 5", n)
	}
}
