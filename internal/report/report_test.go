package report

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

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

func TestAFocusWindowIsREFUSEDRatherThanSilentlyIgnored(t *testing.T) {
	// 🔴 THE FAIL-LOUD DIRECTION. The oracle's featured pick has two selectors and only the
	// fallback is ported. A caller that passed a path window and got the fallback would read
	// a printed basis claiming a resolved pick — a wrong claim, silently — so the window is
	// refused by name until the matcher is ported.
	_, err := Recall(t.TempDir(),
		RecallOptions{Scope: "alpha", Limit: 12, Page: 1, Mode: DefaultMode,
			FocusPaths: []string{"claudedocs/handoff-x.md"}},
		store.Unrestricted())
	if !errors.Is(err, ErrFocusSelectorUnported) {
		t.Fatalf("got %#v, want ErrFocusSelectorUnported", err)
	}
	// The positive control: the SAME call with no window reaches the store, which is what
	// proves the guard is the window and not the arguments around it.
	if _, err := Recall(t.TempDir(),
		RecallOptions{Scope: "alpha", Limit: 12, Page: 1, Mode: DefaultMode},
		store.Unrestricted()); errors.Is(err, ErrFocusSelectorUnported) {
		t.Fatalf("an empty window must not be refused: %v", err)
	}
}
