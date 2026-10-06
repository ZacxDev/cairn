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
	rootBody := get(RootPath)
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
	if !strings.Contains(entryBody, `<li class="tag"><a href="/?tag=timber">timber</a></li>`) {
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
