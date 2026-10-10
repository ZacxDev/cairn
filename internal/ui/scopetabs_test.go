package ui

import (
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/ZacxDev/cairn/internal/arcs"
	"github.com/ZacxDev/cairn/internal/report"
	"github.com/ZacxDev/cairn/internal/store"
)

// 🔴 THE SCOPE PAGE'S TABS AND THE PROSE CLEANUP, PINNED STRUCTURALLY — by element id, class and
// `data-tab`, never by a sentence a reword could walk around. Every name and date is synthetic.
//
// The world (principal A of `arcsWorld`, who reads `alpha-notes`): THREE entries, TWO writing
// sessions, ONE arc — pairwise-distinct counts, so a label that printed another tab's number, or the
// entry count everywhere, prints a different string.

func tabsStore(t *testing.T, partial bool) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "alpha-notes")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	put := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	entry := func(service, nuance string) string {
		return "---\nservice: " + service + "\nscope: alpha-notes\n---\n\n" + store.NuanceHeading + "\n\n" + nuance
	}
	put("anvil.md", entry("anvil", "- 2000-01-03: struck [cairn: smith-bot/s-anvil-1]\n- 2000-01-02: unsigned\n"))
	put("bellows.md", entry("bellows", "- 2000-01-04: pumped [cairn: smith-bot/s-bellows-2]\n"))
	put("crucible.md", entry("crucible", "- 2000-01-05: melted [cairn: smith-bot/s-anvil-1]\n"))
	if partial {
		// No `service:` — the index REJECTS it, so every count is a lower bound.
		put("dross.md", "---\nscope: alpha-notes\n---\n")
	}
	return root
}

func tabsJournal(t *testing.T, writersMeasured, damaged bool) string {
	t.Helper()
	reg := arcReg("alpha-notes", "forge-arc", arcs.StatusOpen, []string{"alpha-notes"},
		arcs.Member{Session: "s-anvil-1", Role: "originated", FirstSeen: "2000-01-01T00:00:00Z"})
	reg.WritersMeasured = writersMeasured
	path := arcsJournal(t, reg)
	if damaged {
		f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.WriteString("{not a record}\n"); err != nil {
			t.Fatal(err)
		}
		f.Close()
	}
	return path
}

var panelIDs = []string{"scope-entries", "scope-sessions", "scope-arcs"}

// TestEachScopeTabRendersOnlyItsOwnPanel — the selection, the default, and unknown values. RED with
// the `switch` in `ScopePage` always rendering the entries panel, and with `scopeTab` passing an
// unrecognised value through (no panel renders).
func TestEachScopeTabRendersOnlyItsOwnPanel(t *testing.T) {
	readsA, _, _ := arcsWorld(t)
	srv := arcsServer(t, StoreSource{Root: tabsStore(t, false), ArcJournal: tabsJournal(t, true, false)}, readsA)
	for _, tc := range []struct{ tab, panel, here string }{
		{"", "scope-entries", "entries"},
		{"entries", "scope-entries", "entries"},
		{TabSessions, "scope-sessions", "sessions"},
		{TabArcs, "scope-arcs", "arcs"},
		// UNKNOWN → the default tab, never a 400 and never an empty page (see `TabSessions`).
		{"bogus", "scope-entries", "entries"},
		{"Sessions", "scope-entries", "entries"}, // values are exact: a case variant is unknown
	} {
		rec := getAs(t, srv, scopeTabURL(browseScopeA, tc.tab))
		if rec.Code != http.StatusOK {
			t.Errorf("tab %q answered %d", tc.tab, rec.Code)
			continue
		}
		body := rec.Body.String()
		for _, id := range panelIDs {
			has := strings.Contains(body, `id="`+id+`"`)
			if has != (id == tc.panel) {
				t.Errorf("tab %q: panel %s present=%v — only %s may be in the document", tc.tab, id, has, tc.panel)
			}
		}
		if here := regexp.MustCompile(`<span class="view-tab view-tab-here" data-tab="([a-z]+)"`).FindStringSubmatch(body); here == nil || here[1] != tc.here {
			t.Errorf("tab %q: the current-tab marker is %v, want %q", tc.tab, here, tc.here)
		}
		entries := tc.panel == "scope-entries"
		if has := strings.Contains(body, `id="entry-filter-control"`); has != entries {
			t.Errorf("tab %q: filter control present=%v, want %v (only the entries tab drives it)", tc.tab, has, entries)
		}
		if n := strings.Count(body, "<script"); (n == 1) != entries || n > 1 {
			t.Errorf("tab %q: %d script element(s) — the filter script rides only on the entries tab", tc.tab, n)
		}
	}
}

// TestTheTabLabelsCarryPairwiseDistinctCounts — 3 entries, 2 sessions, 1 arc. Each non-current tab is
// a link to its own canonical URL.
func TestTheTabLabelsCarryPairwiseDistinctCounts(t *testing.T) {
	readsA, _, _ := arcsWorld(t)
	srv := arcsServer(t, StoreSource{Root: tabsStore(t, false), ArcJournal: tabsJournal(t, true, false)}, readsA)
	body := getAs(t, srv, scopeTabURL(browseScopeA, TabArcs)).Body.String()
	for _, want := range []string{
		// "Entries (3)", an operator decision — it read "Entries 3".
		`data-tab="entries" href="` + inAttr(scopeURL(browseScopeA)) + `">Entries (3)</a>`,
		`data-tab="sessions" href="` + inAttr(scopeTabURL(browseScopeA, TabSessions)) + `">Sessions (2)</a>`,
		`<span class="view-tab view-tab-here" data-tab="arcs">Arcs (1)</span>`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the tab strip is missing %q:\n%s", want, body)
		}
	}
}

// statsRow is the panel's ONE stats row: the `<p class="card-stats">` inside the element with `id`.
func statsRow(t *testing.T, body, id string) string {
	t.Helper()
	i := strings.Index(body, `id="`+id+`"`)
	if i < 0 {
		t.Fatalf("no element with id %s:\n%s", id, body)
	}
	rest := body[i:]
	j := strings.Index(rest, `<p class="card-stats">`)
	if j < 0 {
		t.Fatalf("the %s panel has no stats row:\n%s", id, rest)
	}
	k := strings.Index(rest[j:], "</p>")
	return rest[j : j+k]
}

// TestThePartialBadgeRendersOnlyWhenTheAnswerIsPartial — BOTH directions, on all three surfaces that
// carry one. RED with the badge condition forced true (the complete world grows a badge) and forced
// false (the partial world loses it).
func TestThePartialBadgeRendersOnlyWhenTheAnswerIsPartial(t *testing.T) {
	readsA, _, _ := arcsWorld(t)
	badge := `class="badge badge-warn"`
	for _, tc := range []struct {
		name                    string
		partial, measured, torn bool
		sessions, arcs, arcPage bool // which surfaces must carry a badge
	}{
		{"complete", false, true, false, false, false, false},
		{"an entry the index rejected", true, true, false, true, true, false},
		{"a damaged journal", false, true, true, false, true, true},
		{"an unmeasured writer leg", false, false, false, false, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := arcsServer(t, StoreSource{Root: tabsStore(t, tc.partial), ArcJournal: tabsJournal(t, tc.measured, tc.torn)}, readsA)
			sessions := statsRow(t, getAs(t, srv, scopeTabURL(browseScopeA, TabSessions)).Body.String(), "scope-sessions")
			arcsRow := statsRow(t, getAs(t, srv, scopeTabURL(browseScopeA, TabArcs)).Body.String(), "scope-arcs")
			arcPage := statsRow(t, getAs(t, srv, arcURL(browseScopeA, "forge-arc")).Body.String(), "arc-summary")
			for _, s := range []struct {
				where, row string
				want       bool
			}{{"sessions tab", sessions, tc.sessions}, {"arcs tab", arcsRow, tc.arcs}, {"arc page", arcPage, tc.arcPage}} {
				if got := strings.Contains(s.row, badge); got != s.want {
					t.Errorf("%s: partial badge present=%v, want %v:\n%s", s.where, got, s.want, s.row)
				}
			}
			// The NUMBERS are there in every state: the attributed K of N is never omitted.
			if !strings.Contains(sessions, "3 of 4 bullets attributed") || !strings.Contains(sessions, "2 sessions") {
				t.Errorf("the sessions stats row lost its numbers:\n%s", sessions)
			}
			if !strings.Contains(arcsRow, "1 arc") || !strings.Contains(arcsRow, "1 of 2 sessions in an arc") ||
				!strings.Contains(arcsRow, "3 of 4 bullets attributed") {
				t.Errorf("the arcs stats row lost its numbers:\n%s", arcsRow)
			}
		})
	}
}

// TestTheRemovedProseIsAbsentAndEveryCaveatIsATooltip — the explainers (`card-what`) and the visible
// caveat paragraphs (`note`) are gone from every tab, the arc page and the session page; and each
// caveat the report states is still IN THE BYTES, as an attribute — moved, not deleted.
func TestTheRemovedProseIsAbsentAndEveryCaveatIsATooltip(t *testing.T) {
	readsA, _, _ := arcsWorld(t)
	srv := arcsServer(t, StoreSource{Root: tabsStore(t, false), ArcJournal: tabsJournal(t, true, false)}, readsA)
	pages := map[string]string{
		"entries tab":  scopeURL(browseScopeA),
		"sessions tab": scopeTabURL(browseScopeA, TabSessions),
		"arcs tab":     scopeTabURL(browseScopeA, TabArcs),
		"arc page":     arcURL(browseScopeA, "forge-arc"),
		"session page": sessionURL("s-anvil-1"),
	}
	caveats := []string{report.SessionsReadsLine, report.SessionsAttributionLine, report.ArcsVisibilityLine,
		report.ArcsProvenanceLine, report.ArcsRegistrationLine, report.ArcVisibilityLine}
	inBytes := map[string]bool{}
	for name, path := range pages {
		rec := getAs(t, srv, path)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s answered %d: %s", name, rec.Code, rec.Body.String())
		}
		body := rec.Body.String()
		for _, class := range []string{`class="card-what"`, `class="note"`} {
			if strings.Contains(body, class) {
				t.Errorf("the %s still renders a %s element", name, class)
			}
		}
		text := visibleText(body)
		for _, c := range caveats {
			if strings.Contains(text, c) {
				t.Errorf("the %s prints the caveat %q as visible text — it belongs in a tooltip", name, c)
			}
			if strings.Contains(body, `"`+inAttr(c)) || strings.Contains(body, "\n"+inAttr(c)) {
				inBytes[c] = true
			}
		}
	}
	for _, c := range caveats {
		if !inBytes[c] {
			t.Errorf("the caveat %q is on NO page's tooltip — it was deleted rather than moved", c)
		}
	}
}
