package ui

import (
	"net/http"
	"regexp"
	"strings"
	"testing"
)

// 🔴 THE SCOPE PAGE'S POLISH PASS (operator decisions): the scope-level "N entries" and "N bullets
// declared open" badges are gone from the scope page (the scope LIST's cards keep them), and an
// entry card renders its aliases HIDDEN — present for the filter, revealed only by `filter.js`.

// TestTheScopePageDropsTheEntryAndOpenBadgesButTheScopeListKeepsThem renders one scope with four
// entries carrying three declared-open bullets — numbers distinct from every other count on the page
// — and requires neither scope-level badge on the scope page, while the same scope's card on the
// scope list still carries both (the positive control: the strings are spellable by this renderer).
func TestTheScopePageDropsTheEntryAndOpenBadgesButTheScopeListKeepsThem(t *testing.T) {
	scope := Scope{ID: "scp_fixture", Name: "kiln-notes", Entries: []Entry{
		{Ref: "glaze", Title: "glaze", Filename: "glaze.md", OpenCount: 2, MTime: 1},
		{Ref: "slip", Title: "slip", Filename: "slip.md", OpenCount: 1, MTime: 2},
		{Ref: "grog", Title: "grog", Filename: "grog.md", MTime: 3},
		{Ref: "wedge", Title: "wedge", Filename: "wedge.md", MTime: 4},
	}}
	view := viewOf("operator@example.invalid", []Scope{scope})
	list := renderNode(t, Page(view))
	view.Scope = &scope
	page := renderNode(t, ScopePage(view))

	for _, badge := range []string{">4 entries<", ">3 bullets declared open<"} {
		if !strings.Contains(list, badge) {
			t.Fatalf("POSITIVE CONTROL: the scope list's card lacks %q, so its absence below proves nothing", badge)
		}
		if strings.Contains(page, badge) {
			t.Errorf("the scope page still carries the scope-level badge %q", badge)
		}
	}
	if strings.Contains(page, "no bullet declares itself open") {
		t.Error("the scope page still carries the quiet no-open stat")
	}
}

// TestTheScopePageRendersAliasesHiddenAndStillFiltersOnThem: each alias is in the row's `data-filter`
// (so the filter matches it) and in a HIDDEN chip list (so the card does not show it); nothing about
// an alias is visible without the script.
func TestTheScopePageRendersAliasesHiddenAndStillFiltersOnThem(t *testing.T) {
	root, id, orchard := recencyWorld(t)
	rec := getAs(t, recencyServer(t, root, id), ScopePath+"?"+QueryID+"="+string(orchard))
	if rec.Code != http.StatusOK {
		t.Fatalf("the scope page answered %d", rec.Code)
	}
	body := rec.Body.String()
	lists := regexp.MustCompile(`<ul class="aliases chips chips-alias"( hidden)?>`).FindAllStringSubmatch(body, -1)
	if len(lists) != 5 {
		t.Fatalf("%d alias lists on the page, want one per row (5) — the filter has nothing to reveal", len(lists))
	}
	for _, l := range lists {
		if l[1] == "" {
			t.Errorf("an alias list renders VISIBLE (%q); the card does not show aliases", l[0])
		}
	}
	if !strings.Contains(body, `<li hidden>birch-alias</li>`) {
		t.Error("birch's alias is not a hidden chip, so the script has nothing to reveal")
	}
	if !strings.Contains(body, "\nbirch-alias\n") {
		t.Error("birch's alias left `data-filter`, so the filter no longer matches it")
	}
	// What a reader sees without script carries no alias at all.
	visible := regexp.MustCompile(`(?s)<ul class="aliases chips chips-alias" hidden>.*?</ul>`).ReplaceAllString(body, "")
	if strings.Contains(visibleText(visible), "-alias") {
		t.Error("an alias is visible on the scope page outside the hidden chip list")
	}
}
