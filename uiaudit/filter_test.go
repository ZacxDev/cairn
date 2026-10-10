package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"github.com/ZacxDev/cairn/internal/ui"
)

// filterState is what the scope page's filter has done to the list, read off the live DOM.
type filterState struct {
	Visible       []string `json:"visible"`       // the ref (first data-filter line) of every row NOT hidden
	HiddenDisplay []string `json:"hiddenDisplay"` // computed `display` of every hidden row
	Total         int      `json:"total"`
	Count         string   `json:"count"`
	EmptyHidden   bool     `json:"emptyHidden"`
	ControlHidden bool     `json:"controlHidden"`
}

const filterStateJS = `(() => {
  const rows = Array.from(document.querySelectorAll("#entry-list > li"));
  return {
    visible: rows.filter(r => !r.hidden).map(r => r.getAttribute("data-filter").split("\n")[0]),
    hiddenDisplay: rows.filter(r => r.hidden).map(r => getComputedStyle(r).display),
    total: rows.length,
    count: document.getElementById("entry-filter-count").textContent,
    emptyHidden: document.getElementById("entry-filter-empty").hidden,
    controlHidden: document.getElementById("entry-filter-control").hidden,
  };
})()`

// filterFixtures are three SYNTHETIC entries written into the booted world's first scope. Only
// `zz-filter-aardvark` carries the alias the query below is a subsequence of; neither its ref
// nor its title contains an `x`, so a match can only have come through the ALIAS field.
var filterFixtures = map[string]string{
	"zz-filter-aardvark": "xyzzy-plugh",
	"zz-filter-bison":    "",
	"zz-filter-cougar":   "",
}

// TestTheEntryFilterNarrowsRowsInARealBrowser is the behavioural half of the filter's guards:
// in a real Chromium, over the real `cairn-ui` binary, the script reveals its control, a query
// matching ONLY one entry's alias hides every other row (and the CSS really does take them out
// of the layout) and updates the count, and a query matching nothing shows the empty state.
//
// 🔴 THE QUERY IS A FUZZY SUBSEQUENCE, NOT A SUBSTRING — `xzyplgh` against `xyzzy-plugh` — so a
// script that fell back to `includes` would hide the very row this expects to see.
func TestTheEntryFilterNarrowsRowsInARealBrowser(t *testing.T) {
	chromiumOrRefuse(t)
	bin := os.Getenv("UIAUDIT_CAIRN_UI")
	if bin == "" {
		t.Fatal("UIAUDIT_CAIRN_UI must name a built cairn-ui: this is the only test that runs the filter " +
			"script in a browser, and skipping it leaves the script's behaviour unmeasured")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	world, err := BootWorld(ctx, os.Getenv("UIAUDIT_REPO_ROOT"), bin, t.TempDir(), 18783)
	if err != nil {
		t.Fatal(err)
	}
	defer world.Stop()

	// The pod parses the store on every page load, so files written after boot are on the page.
	scope := world.Scopes[0]
	for ref, alias := range filterFixtures {
		fm := []string{"---", "service: " + ref, "scope: " + scope}
		if alias != "" {
			fm = append(fm, "aliases:", "  - "+alias)
		}
		fm = append(fm, "---", "", "## What it is", "", "A synthetic entry for the filter test.", "")
		if err := os.WriteFile(filepath.Join(world.Store, scope, ref+".md"), []byte(strings.Join(fm, "\n")), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	b, err := NewBrowser(ctx, world.BaseURL, 2*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	if _, err := b.SignIn(fixtureToken); err != nil {
		t.Fatalf("%v\n--- cairn-ui log ---\n%s", err, world.Log())
	}

	var href string
	if err := chromedp.Run(b.ctx,
		chromedp.Navigate(world.BaseURL+ui.ScopesPath),
		chromedp.Evaluate(fmt.Sprintf(
			`(Array.from(document.querySelectorAll("a.card-name")).find(a => a.textContent === %q) || {getAttribute: () => ""}).getAttribute("href")`,
			scope), &href),
	); err != nil {
		t.Fatal(err)
	}
	if href == "" {
		t.Fatalf("the scope list links no card named %q", scope)
	}

	read := func() filterState {
		t.Helper()
		var s filterState
		if err := chromedp.Run(b.ctx, chromedp.Evaluate(filterStateJS, &s)); err != nil {
			t.Fatal(err)
		}
		return s
	}
	open := func() {
		t.Helper()
		// `WaitVisible` on the box IS the assertion that the script ran: the server renders the
		// control `hidden`, and only `filter.js` unhides it.
		if err := chromedp.Run(b.ctx,
			chromedp.Navigate(world.BaseURL+href),
			chromedp.WaitVisible(`#entry-filter`, chromedp.ByID),
		); err != nil {
			t.Fatalf("the filter control never became visible on %s, so the script did not run: %v", href, err)
		}
	}
	typeQuery := func(q string) {
		t.Helper()
		if err := chromedp.Run(b.ctx, chromedp.SendKeys(`#entry-filter`, q, chromedp.ByID)); err != nil {
			t.Fatal(err)
		}
	}

	// --- Before typing: every row shows, and the count says so. ---
	open()
	before := read()
	if before.ControlHidden || len(before.Visible) != before.Total || before.Total < len(filterFixtures) {
		t.Fatalf("before any query: %+v — want the control shown and all %d+ rows visible", before, len(filterFixtures))
	}
	for ref := range filterFixtures {
		if !slices.Contains(before.Visible, ref) {
			t.Fatalf("the fixture row %q is not on the scope page, so the queries below are about nothing: %v", ref, before.Visible)
		}
	}
	all := fmt.Sprintf("%d of %d entries", before.Total, before.Total)
	if before.Count != all {
		t.Errorf("before any query the count reads %q, want %q", before.Count, all)
	}

	// --- A query matching ONLY one entry's ALIAS. ---
	typeQuery("xzyplgh")
	got := read()
	if !slices.Equal(got.Visible, []string{"zz-filter-aardvark"}) {
		t.Errorf("after typing an alias-only subsequence the visible rows are %v, want only [zz-filter-aardvark]", got.Visible)
	}
	if want := fmt.Sprintf("1 of %d entries", got.Total); got.Count != want {
		t.Errorf("the count reads %q, want %q", got.Count, want)
	}
	if !got.EmptyHidden {
		t.Error("the empty-state line is shown while a row matches")
	}
	for _, d := range got.HiddenDisplay {
		if d != "none" {
			t.Errorf("a hidden row computes display %q, so CSS overrode `hidden` and the row is still on screen", d)
		}
	}
	if len(got.HiddenDisplay) != got.Total-1 {
		t.Errorf("%d rows hidden, want %d", len(got.HiddenDisplay), got.Total-1)
	}

	// --- A query matching nothing. ---
	open()
	typeQuery("qqqzzzqqqzzz")
	none := read()
	if len(none.Visible) != 0 {
		t.Errorf("a query matching nothing left rows visible: %v", none.Visible)
	}
	if want := fmt.Sprintf("0 of %d entries", none.Total); none.Count != want {
		t.Errorf("the count reads %q, want %q", none.Count, want)
	}
	if none.EmptyHidden {
		t.Error("nothing matches and the empty-state line is still hidden")
	}
	t.Logf("filter in a real browser: %d rows; alias-only query -> %v (%q); no-match query -> %d visible (%q), empty state shown",
		got.Total, got.Visible, got.Count, len(none.Visible), none.Count)
}
