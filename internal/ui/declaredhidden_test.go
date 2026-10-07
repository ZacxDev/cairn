package ui

import (
	"net/http"
	"strings"
	"testing"

	"github.com/ZacxDev/cairn/internal/arcs"
)

// TestAnArcDeclaringAHiddenScopeNeverNamesItOnAnyPage — the arc is homed in alpha (readable by A) and
// DECLARES beta (not readable by A). The arc is A's to see; the hidden declared scope is not. The arcs
// tab's declared-scope chips read `ArcLine.DeclaredVisible`, and `scopeChip` falls back to the plain
// NAME for a scope whose id it cannot resolve — so the filter in `report.Arcs` is the only thing that
// keeps beta's name off A's page. RED with that filter mutated to `if true {`.
func TestAnArcDeclaringAHiddenScopeNeverNamesItOnAnyPage(t *testing.T) {
	readsA, _, readsW := arcsWorld(t)
	const slug = "spans-arc"
	src := StoreSource{Root: arcsStore(t), ArcJournal: arcsJournal(t,
		arcReg("alpha-notes", slug, arcs.StatusOpen, []string{"alpha-notes", "beta-notes"},
			arcs.Member{Session: arcsSessionAlpha, Role: "originated"}))}

	pages := map[string]string{
		"arcs tab":     scopeTabURL(browseScopeA, TabArcs),
		"sessions tab": scopeTabURL(browseScopeA, TabSessions),
		"arc page":     arcURL(browseScopeA, slug),
		"session page": sessionURL(arcsSessionAlpha),
	}
	for name, path := range pages {
		rec := getAs(t, arcsServer(t, src, readsA), path)
		if rec.Code != http.StatusOK {
			t.Fatalf("A's %s answered %d: %s", name, rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), slug) && name != "sessions tab" {
			t.Errorf("INSTRUMENT: A's %s does not carry the arc at all, so the absence below measures nothing", name)
		}
		if strings.Contains(rec.Body.String(), "beta-notes") {
			t.Errorf("A's %s NAMES the hidden declared scope beta-notes — a declared scope A cannot read must be "+
				"omitted, not counted and not named", name)
		}
	}
	// POSITIVE CONTROL: W reads beta, so the same arcs tab carries beta as a linked declared chip.
	wide := getAs(t, arcsServer(t, src, readsW), scopeTabURL(browseScopeA, TabArcs)).Body.String()
	if !strings.Contains(wide, `<ul class="chips chips-scope" title="declared scopes you can read">`) ||
		!strings.Contains(wide, ">beta-notes</a>") {
		t.Fatalf("POSITIVE CONTROL FAILED: W's arcs tab does not chip beta-notes as a declared scope:\n%s", wide)
	}
}
