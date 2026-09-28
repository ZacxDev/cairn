package report

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ZacxDev/cairn/internal/store"
)

// tagZeroWorld is the `filterZeroWorld` shape one filter over: TWO scopes, each holding exactly
// the pair that separates the two mechanisms a search's `searched == 0` can come from.
//
//   - `tag-kept-none/` — ONE readable entry that does NOT carry the tag, plus ONE file that
//     cannot be indexed. Every readable entry here was read and indexed; the FILTER is what
//     empties the searched set.
//   - `tag-all-broken/` — ONE file that cannot be indexed and nothing else. Nothing readable
//     exists, so the searched set is empty for the OTHER reason.
//
// ⚠ NEITHER SCOPE'S READABLE ENTRY CARRIES THE TAG, so `EntriesSearched` is 0 on both. That is
// the point: the two shapes are indistinguishable by the searched count alone, and a status
// derived from that count alone answers the same thing for both.
//
// ⚠ IT IS A SECOND FIXTURE RATHER THAN A `filterZeroWorld` PARAMETER, and that is deliberate.
// The scopes are separate so this test and `TestAFilterDrivenZeroIsNotReportedAsAnUnreadableStore`
// cannot mask each other: a change that made one scope's readable entry carry both a ref and a
// tag would quietly satisfy both tests' preconditions while measuring one filter.
func tagZeroWorld(t *testing.T) string {
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
	// reject `searchfilterzero_test.go` uses, so this fixture is not inventing a parse failure.
	const unindexable = "---\nservice: widget\naliases: [one,\n  two]\n---\n"
	write("tag-kept-none", "readable.md", strings.Join([]string{
		"---", "service: readable", "scope: tag-kept-none", "---", "",
		"## What it is", "", "Readable, indexed, and carrying no tags at all.", "",
		store.NuanceHeading, "", "- 2000-06-01 a bullet mentioning readiness", "",
	}, "\n"))
	write("tag-kept-none", "wrapped-aliases.md", unindexable)
	write("tag-all-broken", "wrapped-aliases.md", unindexable)
	return root
}

// TestATagDrivenZeroIsNotReportedAsAnUnreadableStore is the guard for the status condition THIS
// change could falsify, and it is the single highest-value thing in this file.
//
// 🔴 WHY IT EXISTS BEFORE THE DEFECT DOES. `searched == 0 && refToSkipped == 0 && len(bad) > 0`
// was SOUND on `5c59169`: those two counters partitioned the readable set, so a zero on both
// meant nothing readable existed. A tag filter placed upstream of `searched` — which is where it
// has to go, because it must run after scope authorisation — breaks that partition. Without the
// `tagSkipped == 0` term, one readable entry that does not carry the tag plus one malformed file
// answers `search-unreadable`: the body says "NOTHING COULD BE READ", "NOT ONE of them could be
// indexed" and "The query was never run against anything", all false about a scope whose entry
// was read and indexed, and `ExitFor` returns 3 with "recall was unavailable" — which a consumer
// told to print that verbatim and continue reads as "throw the run away".
//
// 🔴 THAT IS NOT A HYPOTHETICAL: IT IS THE EXACT DEFECT #141 SHIPPED AND TOOK THREE AUDIT ROUNDS
// TO FIND, because the commit that staled the condition WAS the commit that introduced the filter,
// so no delta audit's range contained it. The general rule is written beside the switch: every
// filter placed upstream of `searched` falsifies this condition unless its own skip count is
// subtracted back out.
//
// ⚠ RED/GREEN, MEASURED: with `tagSkipped == 0` deleted from the switch arm — the NARROWEST
// mutation, the enclosing condition and the other two terms untouched — the `tag-kept-none` half
// fails on the status, on all three false sentences and on the exit code, and the
// `tag-all-broken` half keeps PASSING. That asymmetry is the point: the term narrows the branch
// rather than deleting it.
func TestATagDrivenZeroIsNotReportedAsAnUnreadableStore(t *testing.T) {
	root := tagZeroWorld(t)
	search := func(scope string) SearchReport {
		t.Helper()
		rep, err := Search(root, SearchOptions{
			Scope: scope, Query: "readiness", Context: ContextBullet,
			Threshold: DefaultThreshold, MaxHits: DefaultMaxHits,
			Tags: []string{sharedTag},
		}, store.Unrestricted())
		if err != nil {
			t.Fatalf("search over %s: %v", scope, err)
		}
		return rep
	}

	// ── The regression: readable entries existed, and the filter removed them all.
	filtered := search("tag-kept-none")
	if filtered.EntriesSearched != 0 || filtered.TagSkipped != 1 || len(filtered.Malformed) != 1 {
		t.Fatalf("the fixture did not build the shape this test is named for: searched=%d "+
			"tagSkipped=%d malformed=%d, want 0/1/1", filtered.EntriesSearched,
			filtered.TagSkipped, len(filtered.Malformed))
	}
	if filtered.Status != StatusSearchNoMatch {
		t.Errorf("a tag-driven zero answered %q, want %q. One readable entry was read and "+
			"indexed and then removed by the `tag` filter; %q claims nothing in the scope could "+
			"be indexed and sends the reader to fix the malformed file instead.",
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
		"  tag: `" + sharedTag + "` — 0 of 1 entry in `tag-kept-none/` carry it",
		"🔴 MALFORMED — 1 entry file in `tag-kept-none/`",
		// 🔴 AND THE SENTENCE NAMES THE `tag` FILTER, NOT THE `ref-to` ONE. The pre-existing
		// branch hardcoded "`ref-to`"; reusing it here would send a reader who typed only
		// `--tag` to check an operand they never gave — a diagnosis that is WRONG rather than
		// absent, which is worse than the empty result it explains.
		"NO MATCH — searched 0 entries in `tag-kept-none/`, and nothing cleared the threshold. " +
			"Nothing was scanned: the `tag` filter removed every readable entry, so this zero " +
			"is the FILTER's and says nothing about the query.",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("the rendered answer is missing %q.\ngot:\n%s", want, text)
		}
	}
	// 🔴 THE OPERATOR-VISIBLE STAKE. `search-unreadable` is in `UnreadableStatuses`, so the wrong
	// status also exits 3.
	if code, warning := ExitFor(filtered.Status, filtered.Label(), filtered.Malformed); code != 0 {
		t.Errorf("a tag-driven zero exits %d with %q, want 0: the query ran, over a narrowed set "+
			"that turned out to be empty", code, warning)
	}

	// ── The positive control: nothing readable existed, so the branch must still fire.
	broken := search("tag-all-broken")
	if broken.EntriesSearched != 0 || broken.TagSkipped != 0 || len(broken.Malformed) != 1 {
		t.Fatalf("the control did not build its shape: searched=%d tagSkipped=%d malformed=%d, "+
			"want 0/0/1", broken.EntriesSearched, broken.TagSkipped, len(broken.Malformed))
	}
	if broken.Status != StatusSearchUnreadable {
		t.Errorf("a scope whose ONLY file cannot be indexed answered %q, want %q — the term "+
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

// TestBothFiltersEmptyingTheSetNamesBothOfThem is the third arm of the no-match sentence.
//
// 🔴 A SENTENCE THAT NAMES ONE FILTER WHEN TWO RAN IS A WRONG DIAGNOSIS, NOT A MISSING ONE, and
// that is why this row exists rather than being left to the `||`. An operator who passed both
// `--ref-to` and `--tag` and is told "the `ref-to` filter removed every readable entry" will go
// and check the ref — and the ref may be exactly right.
//
// ⚠ AND BOTH HALVES OF THE SENTENCE HAVE TO AGREE WITH THE SUBJECT. `emptyingFilters` returns the
// possessive alongside the subject for that reason: "the FILTERS'" with a plural subject, "the
// FILTER's" with a singular one.
func TestBothFiltersEmptyingTheSetNamesBothOfThem(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "mixed-set")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(name string, lines ...string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(strings.Join(lines, "\n")), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// ONE entry the `ref-to` filter removes, ONE the `tag` filter removes. Both counters
	// therefore end non-zero and `searched` ends at 0 — the only shape in which a single-filter
	// sentence is observably wrong.
	write("has-the-tag.md", "---", "service: has-the-tag", "scope: mixed-set",
		"tags: ["+sharedTag+"]", "---", "",
		"## What it is", "", "Carries the tag and no refs.", "",
		store.NuanceHeading, "", "- 2000-06-01 a bullet mentioning readiness", "")
	write("has-the-ref.md", "---", "service: has-the-ref", "scope: mixed-set",
		"refs: ["+sharedRef+"]", "---", "",
		"## What it is", "", "Carries the ref and no tags.", "",
		store.NuanceHeading, "", "- 2000-06-01 a bullet mentioning readiness", "")

	rep, err := Search(root, SearchOptions{
		Scope: "mixed-set", Query: "readiness", Context: ContextBullet,
		Threshold: DefaultThreshold, MaxHits: DefaultMaxHits,
		RefTo: sharedRef, HasRefTo: true, Tags: []string{sharedTag},
	}, store.Unrestricted())
	if err != nil {
		t.Fatal(err)
	}
	if rep.EntriesSearched != 0 || rep.RefToSkipped != 1 || rep.TagSkipped != 1 {
		t.Fatalf("searched=%d refToSkipped=%d tagSkipped=%d, want 0/1/1 — this test is about the "+
			"case where BOTH filters removed something", rep.EntriesSearched, rep.RefToSkipped,
			rep.TagSkipped)
	}
	if rep.Status != StatusSearchNoMatch {
		t.Fatalf("status=%q, want %q", rep.Status, StatusSearchNoMatch)
	}
	want := "Nothing was scanned: the `ref-to` and `tag` filters removed every readable entry, " +
		"so this zero is the FILTERS' and says nothing about the query."
	got := rep.RenderText("synthetic-host", nil, "")
	if !strings.Contains(got, want) {
		t.Errorf("the sentence does not name both filters.\nwant: %s\ngot:\n%s", want, got)
	}
	// The CONTROL on the subject choice, both singular directions, so the three-way switch is
	// pinned rather than only its new arm. Asserted as WHOLE clauses because a guard on the word
	// `tag` would be satisfied by a sentence that named the wrong filter and mentioned the right
	// one somewhere else.
	for _, row := range []struct {
		refToSkipped, tagSkipped int
		want                     string
	}{
		{1, 0, "the `ref-to` filter removed every readable entry, so this zero is the FILTER's"},
		{0, 1, "the `tag` filter removed every readable entry, so this zero is the FILTER's"},
		{2, 3, "the `ref-to` and `tag` filters removed every readable entry, so this zero is the FILTERS'"},
	} {
		subject, possessive := emptyingFilters(row.refToSkipped, row.tagSkipped)
		line := subject + " removed every readable entry, so this zero is " + possessive
		if line != row.want {
			t.Errorf("emptyingFilters(%d, %d) builds %q, want %q",
				row.refToSkipped, row.tagSkipped, line, row.want)
		}
	}
}
