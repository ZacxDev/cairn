package report

import (
	"errors"
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

func TestTheUnimplementedRendererIsADISTINCTError(t *testing.T) {
	// 🔴 501, NOT 500, AND THE TWO MEAN OPPOSITE THINGS TO AN OPERATOR RUNNING BOTH
	// SERVERS SIDE BY SIDE. A 500 says "this server broke"; a 501 says "this server does
	// not implement this yet, ask the other one". During a dual-run those are the only
	// two hypotheses worth telling apart, so the handler branches on THIS error and not
	// on a generic failure.
	var renderer Renderer = Unimplemented{}
	_, err := renderer.Recall("/store", RecallOptions{}, store.Unrestricted())
	if !errors.Is(err, ErrUnimplemented) {
		t.Fatalf("got %v", err)
	}
	_, err = renderer.Search("/store", SearchOptions{}, store.Unrestricted())
	if !errors.Is(err, ErrUnimplemented) {
		t.Fatalf("got %v", err)
	}
}
