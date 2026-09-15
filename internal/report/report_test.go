package report

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ZacxDev/cairn/internal/store"
)

func TestTheValidationLADDERISORDERED(t *testing.T) {
	// 🔴 THE ORDER IS THE CONTRACT, NOT AN IMPLEMENTATION DETAIL. A request carrying
	// TWO bad parameters gets ONE message, and which one it gets is what the goldens
	// record — so a test that fed one bad parameter at a time would pass under any
	// ordering. Each row below is bad in EVERY position up to the one it names, which is
	// what makes the position observable.
	cases := []struct {
		name string
		opts RecallOptions
		want string
	}{
		{
			name: "limit is checked FIRST",
			opts: RecallOptions{Limit: 0, Page: 0, Mode: "telepathy"},
			want: "limit must be an int >= 1, got 0",
		},
		{
			name: "then page, which a valid limit still reaches",
			opts: RecallOptions{Limit: 1, Page: 0, Mode: "telepathy"},
			want: "page must be an int >= 1, got 0",
		},
		{
			name: "then mode, which a valid limit AND page still reach",
			opts: RecallOptions{Limit: 1, Page: 1, Mode: "telepathy"},
			want: "mode must be one of ('digest', 'list', 'full'), got 'telepathy'",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateRecall(tc.opts)
			if err == nil || err.Error() != tc.want {
				t.Fatalf("\n got: %v\nwant: %s", err, tc.want)
			}
		})
	}
	if err := ValidateRecall(RecallOptions{Limit: 12, Page: 1, Mode: DefaultMode}); err != nil {
		t.Fatalf("the positive control: a well-formed request must pass: %v", err)
	}

	searchCases := []struct {
		name string
		opts SearchOptions
		want string
	}{
		{
			name: "the query is checked FIRST",
			opts: SearchOptions{Query: "  ", Threshold: 9, MaxHits: 0, Context: -9},
			want: "query must be a non-empty string",
		},
		{
			name: "then the threshold range",
			opts: SearchOptions{Query: "x", Threshold: 9, MaxHits: 0, Context: -9},
			want: "threshold must be a number in [0, 1], got 9.0",
		},
		{
			name: "then max-hits",
			opts: SearchOptions{Query: "x", Threshold: 0.5, MaxHits: 0, Context: -9},
			want: "max-hits must be an int >= 1, got 0",
		},
		{
			name: "then context",
			opts: SearchOptions{Query: "x", Threshold: 0.5, MaxHits: 1, Context: -9},
			want: "context must be an int >= 0, got -9",
		},
	}
	for _, tc := range searchCases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateSearch(tc.opts)
			if err == nil || err.Error() != tc.want {
				t.Fatalf("\n got: %v\nwant: %s", err, tc.want)
			}
		})
	}
	// `ContextBullet` is the FLOOR, which is why the refusal reads `>= 0` while the
	// constant is negative: 0 is the smallest LINE count and this is the one value below
	// it that means something else.
	if err := ValidateSearch(SearchOptions{
		Query: "x", Threshold: DefaultThreshold, MaxHits: DefaultMaxHits, Context: ContextBullet,
	}); err != nil {
		t.Fatalf("the sentinel context value must be accepted: %v", err)
	}
}

func TestTheThresholdIsQuotedTheWayCPythonWouldQuoteIt(t *testing.T) {
	// The refusal echoes the value back, and the golden records CPython's `repr` of it —
	// `2.0`, not `2`.
	for _, tc := range []struct {
		in   float64
		want string
	}{{2, "2.0"}, {-1, "-1.0"}, {1.5, "1.5"}} {
		err := ValidateSearch(SearchOptions{Query: "x", Threshold: tc.in, MaxHits: 1, Context: 0})
		if err == nil || !strings.HasSuffix(err.Error(), "got "+tc.want) {
			t.Fatalf("threshold %v: got %v, want a refusal ending in %q", tc.in, err, tc.want)
		}
	}
}

func TestTheModeVocabularyIsQuotedAsAPythonTuple(t *testing.T) {
	// 🔴 THE ORACLE INTERPOLATES ITS OWN CONSTANT AND THE GOLDEN RECORDS THE RESULT, so
	// the quotes, the commas, the spaces and the parentheses are the contract for this
	// message — not a presentation choice this port is free to make.
	if got := pyTuple(RecallModes); got != "('digest', 'list', 'full')" {
		t.Fatalf("got %s", got)
	}
	// A one-element tuple needs its trailing comma, which is the shape a future
	// single-mode vocabulary would take.
	if got := pyTuple([]string{"only"}); got != "('only',)" {
		t.Fatalf("got %s", got)
	}
}

func TestAMissingStoreIsANAMEDErrorAndNotAnEmptyReport(t *testing.T) {
	// ⚠ THIS TEST REPLACES ONE THAT PINNED P1a's `ErrUnimplemented`. That error and its
	// renderer are gone, so what is pinned instead is the claim that mattered underneath
	// it: an error out of this seam is CLASSIFIABLE by a caller that is not an HTTP
	// handler, which is what lets the CLI (P2) map it to an exit code without re-deriving
	// the mapping from a message.
	var renderer Renderer = Reader{Host: func() string { return "test-host" }}
	_, err := renderer.Recall(filepath.Join(t.TempDir(), "absent"),
		RecallOptions{Scope: "alpha", Limit: 12, Page: 1, Mode: DefaultMode},
		store.Unrestricted())
	var missing *store.StoreMissingError
	if !errors.As(err, &missing) {
		t.Fatalf("got %#v, want a *store.StoreMissingError", err)
	}
	// 🔴 THE SENTENCE SAYS WHAT DID NOT HAPPEN. "store root not found" alone reads as the
	// ordinary nothing-recorded-yet case, which is the confident zero this whole reader
	// exists to prevent.
	if !strings.Contains(err.Error(), "Nothing was recalled; this is NOT 'nothing recorded yet'") {
		t.Fatalf("the verb must name the read that did not happen: %q", err)
	}
	_, err = renderer.Search(filepath.Join(t.TempDir(), "absent"),
		SearchOptions{Scope: "alpha", Query: "x", Threshold: DefaultThreshold,
			MaxHits: DefaultMaxHits, Context: ContextBullet},
		store.Unrestricted())
	if !errors.As(err, &missing) {
		t.Fatalf("got %#v, want a *store.StoreMissingError", err)
	}
	if !strings.Contains(err.Error(), "Nothing was searched;") {
		t.Fatalf("search must name ITS verb, not recall's: %q", err)
	}
}

// focusWorld writes a two-entry scope whose entries differ in EVERY field the selector
// reads, so no assertion below can pass by accident of two values agreeing:
// `widget-cfg` is the OLDER file and `ledger-svc` the newer one, so the fallback and a
// path window that names `widget-cfg` disagree about which entry wins.
func focusWorld(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	scope := filepath.Join(root, "alpha-notes")
	if err := os.MkdirAll(scope, 0o755); err != nil {
		t.Fatal(err)
	}
	body := func(service string) string {
		return "---\nservice: " + service + "\nscope: alpha-notes\n---\n\n" +
			"## What it is\n\nA synthetic entry.\n\n## Pointers\n\n- `x`\n\n" +
			"## Nuance / work-history\n\n- 2000-01-01: a synthetic bullet.\n"
	}
	for i, name := range []string{"widget-cfg", "ledger-svc"} {
		path := filepath.Join(scope, name+".md")
		if err := os.WriteFile(path, []byte(body(name)), 0o644); err != nil {
			t.Fatal(err)
		}
		// Distinct whole seconds, ascending with the loop, so `ledger-svc` is newest and
		// the mtime tie-break is not the thing under test here.
		stamp := time.Unix(int64(946684800+60*(i+1)), 0)
		if err := os.Chtimes(path, stamp, stamp); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestAFocusWindowIsRESOLVEDAndTheBasisSaysWhichSelectorFired(t *testing.T) {
	// 🔴 RED AT `1e593a8`: `Recall` returned `ErrFocusSelectorUnported` for every case
	// below that carries a window, so the first two subtests could not run at all. This
	// is the guard that replaces that refusal, and it pins the thing the refusal was
	// protecting: the printed basis must say WHICH selector chose the entry, and the two
	// fallback sentences must stay distinguishable.
	root := focusWorld(t)
	recall := func(paths []string, source string) RecallReport {
		t.Helper()
		rep, err := Recall(root, RecallOptions{
			Scope: "alpha-notes", Limit: DefaultEntryLimit, Page: 1, Mode: DefaultMode,
			FocusPaths: paths, FocusSource: source,
		}, store.Unrestricted())
		if err != nil {
			t.Fatalf("recall: %v", err)
		}
		return rep
	}

	// A window naming the OLDER entry must beat the fallback, which is what proves the
	// matcher ran rather than the pick being the newest file either way.
	rep := recall([]string{"claudedocs/handoff-alpha.md", "apps/widget-cfg/values.yaml"},
		"claudedocs/handoff-alpha.md")
	if len(rep.Entries) != 1 || rep.Entries[0].Ref != "widget-cfg" {
		t.Fatalf("a resolved window must feature widget-cfg, got %#v", rep.Entries)
	}
	want := "resolved via claudedocs/handoff-alpha.md — 1 of 2 quoted path(s) name it: " +
		"apps/widget-cfg/values.yaml"
	if rep.FeaturedBasis != want {
		t.Fatalf("basis\n got: %s\nwant: %s", rep.FeaturedBasis, want)
	}

	// A window that was READ and matched NOTHING is a different sentence from a window
	// that was never read. Collapsing the two is how a miss renders as an absence.
	rep = recall([]string{"claudedocs/handoff-alpha.md", "apps/unrelated/values.yaml"},
		"claudedocs/handoff-alpha.md")
	if rep.Entries[0].Ref != "ledger-svc" {
		t.Fatalf("a window that matched nothing must fall back to the newest: %#v", rep.Entries)
	}
	want = "most-recent fallback — newest entry file in `alpha-notes/` " +
		"(nothing quoted in claudedocs/handoff-alpha.md resolved to an entry)"
	if rep.FeaturedBasis != want {
		t.Fatalf("basis\n got: %s\nwant: %s", rep.FeaturedBasis, want)
	}

	// The pod's own case, unchanged by any of this: no window at all.
	rep = recall(nil, "")
	want = "most-recent fallback — newest entry file in `alpha-notes/` " +
		"(no handoff doc to read a path window from)"
	if rep.FeaturedBasis != want {
		t.Fatalf("basis\n got: %s\nwant: %s", rep.FeaturedBasis, want)
	}
}
