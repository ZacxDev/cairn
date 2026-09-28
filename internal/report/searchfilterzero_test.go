package report

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ZacxDev/cairn/internal/store"
)

// filterZeroWorld is TWO scopes, each holding exactly the pair that separates the two
// mechanisms a search's `searched == 0` can come from:
//
//   - `kept-none/` — ONE readable entry that does NOT carry the ref, plus ONE file that
//     cannot be indexed. Every readable entry here was read and indexed; the FILTER is what
//     empties the searched set.
//   - `all-broken/` — ONE file that cannot be indexed and nothing else. Nothing readable
//     exists, so the searched set is empty for the OTHER reason.
//
// ⚠ NEITHER SCOPE'S READABLE ENTRY CARRIES THE REF, so the filter keeps nothing in either and
// `EntriesSearched` is 0 on both. That is the point: the two shapes are indistinguishable by
// the searched count alone, and a status derived from that count alone answers the same thing
// for both.
func filterZeroWorld(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	write := func(scope, name, body string) {
		dir := filepath.Join(root, scope)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("building the store: %v", err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatalf("writing %s/%s: %v", scope, name, err)
		}
	}
	// The malformed shape is a WRAPPED `aliases:` list, which the loader rejects — the same
	// reject `refto_clause_test.go` uses, so this fixture is not inventing a parse failure.
	const unindexable = "---\nservice: widget\naliases: [one,\n  two]\n---\n"
	write("kept-none", "readable.md", strings.Join([]string{
		"---", "service: readable", "scope: kept-none", "---", "",
		"## What it is", "", "Readable, indexed, and carrying no refs at all.", "",
		store.NuanceHeading, "", "- 2000-06-01 a bullet mentioning readiness", "",
	}, "\n"))
	write("kept-none", "wrapped-aliases.md", unindexable)
	write("all-broken", "wrapped-aliases.md", unindexable)
	return root
}

// TestAFilterDrivenZeroIsNotReportedAsAnUnreadableStore is the REGRESSION guard for the
// condition THIS BRANCH falsified.
//
// 🔴 RED/GREEN MATRIX, MEASURED. At `a6d1a69` — the branch head before this fix — the
// `kept-none` half FAILS: the status is `search-unreadable`, the body says "NOTHING COULD BE
// READ … NOT ONE of them could be indexed" and "The query was never run against anything",
// and `ExitFor` returns 3 with "nothing could be read, so recall was unavailable". All false
// about a scope whose one readable entry was read, indexed, and then removed by the `ref-to`
// filter — and the non-zero throws away a run that had an answer. The `all-broken` half
// passes there and must keep passing: this fix NARROWS the branch, it does not delete it.
//
// 🔴 AND THE DEFECT IS THIS BRANCH'S OWN, NOT INHERITED. `searched == 0 && len(bad) > 0` was
// sound while `searched` counted every readable entry in the scope — which it did until the
// `ref-to` filter was added upstream of that counter. No delta audit of a later round could
// see it: the commit that staled the condition is the one that introduced the filter.
func TestAFilterDrivenZeroIsNotReportedAsAnUnreadableStore(t *testing.T) {
	root := filterZeroWorld(t)
	search := func(scope string) SearchReport {
		t.Helper()
		rep, err := Search(root, SearchOptions{
			Scope: scope, Query: "readiness", Context: ContextBullet,
			Threshold: DefaultThreshold, MaxHits: DefaultMaxHits,
			RefTo: sharedRef, HasRefTo: true,
		}, store.Unrestricted())
		if err != nil {
			t.Fatalf("search over %s: %v", scope, err)
		}
		return rep
	}

	// ── The regression: readable entries existed, and the filter removed them all.
	filtered := search("kept-none")
	if filtered.EntriesSearched != 0 || filtered.RefToSkipped != 1 || len(filtered.Malformed) != 1 {
		t.Fatalf("the fixture did not build the shape this test is named for: searched=%d "+
			"skipped=%d malformed=%d, want 0/1/1", filtered.EntriesSearched,
			filtered.RefToSkipped, len(filtered.Malformed))
	}
	if filtered.Status != StatusSearchNoMatch {
		t.Errorf("a filter-driven zero answered %q, want %q. One readable entry was read and "+
			"indexed and then removed by the `ref-to` filter; %q claims nothing in the scope "+
			"could be indexed and sends the reader to fix the malformed file instead.",
			filtered.Status, StatusSearchNoMatch, StatusSearchUnreadable)
	}
	text := filtered.RenderText("synthetic-host", nil, "")
	for _, never := range []string{
		"NOTHING COULD BE READ",
		"NOT ONE of them could be indexed",
		"was never run against anything",
	} {
		if strings.Contains(text, never) {
			t.Errorf("the rendered answer still claims %q over a scope whose readable entry "+
				"WAS indexed.\ngot:\n%s", never, text)
		}
	}
	for _, want := range []string{
		"status=search-no-match",
		"  ref-to: `" + sharedRef + "` — 0 of 1 entry in `kept-none/` reference it",
		"🔴 MALFORMED — 1 entry file in `kept-none/`",
		"NO MATCH — searched 0 entries in `kept-none/`, and nothing cleared the threshold. " +
			"Nothing was scanned: the `ref-to` filter removed every readable entry, so this " +
			"zero is the FILTER's and says nothing about the query.",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("the rendered answer is missing %q.\ngot:\n%s", want, text)
		}
	}
	// 🔴 THE OPERATOR-VISIBLE STAKE. `search-unreadable` is in `UnreadableStatuses`, so the
	// wrong status also exited 3 with "recall was unavailable" — and a consumer told to print
	// that verbatim and continue discards a run that had an answer.
	if code, warning := ExitFor(filtered.Status, filtered.Label(), filtered.Malformed); code != 0 {
		t.Errorf("a filter-driven zero exits %d with %q, want 0: the query ran, over a "+
			"narrowed set that turned out to be empty", code, warning)
	}

	// ── The positive control: nothing readable existed, so the branch must still fire.
	broken := search("all-broken")
	if broken.EntriesSearched != 0 || broken.RefToSkipped != 0 || len(broken.Malformed) != 1 {
		t.Fatalf("the control did not build its shape: searched=%d skipped=%d malformed=%d, "+
			"want 0/0/1", broken.EntriesSearched, broken.RefToSkipped, len(broken.Malformed))
	}
	if broken.Status != StatusSearchUnreadable {
		t.Errorf("a scope whose ONLY file cannot be indexed answered %q, want %q — the fix "+
			"narrows this branch and must not delete it, or every assertion above is satisfied "+
			"by a status nothing can reach", broken.Status, StatusSearchUnreadable)
	}
	brokenText := broken.RenderText("synthetic-host", nil, "")
	if !strings.Contains(brokenText, "NOTHING COULD BE READ") ||
		!strings.Contains(brokenText, "was never run against anything") {
		t.Errorf("the unreadable body is not what it was.\ngot:\n%s", brokenText)
	}
	if code, _ := ExitFor(broken.Status, broken.Label(), broken.Malformed); code != 3 {
		t.Errorf("an unreadable store exits %d, want 3", code)
	}
}
