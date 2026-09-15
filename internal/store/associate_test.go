package store

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

// 🔴 WHAT THIS FILE OWNS, AND WHAT IT DOES NOT. The RENDERED effect of the matcher — which
// entry gets featured and what the basis sentence says — is measured against the ORACLE's
// own bytes by `internal/report`'s `reader_fixtures.json` rows whose ids start
// `digest-focus-`. That is the differential gate, and it is red at `1e593a8` for a
// different reason (the whole call refused). This file owns the surfaces those rendered
// rows CANNOT see: the guard ladder, the two kinds of zero, the stem rule, and the
// accounting fields no report prints.

func testIndex(t *testing.T) *Index {
	t.Helper()
	mk := func(service, scope string, aliases ...string) FrontMatter {
		fm := FrontMatter{"service": service, "scope": scope, "filename": service + ".md"}
		if len(aliases) > 0 {
			fm["aliases"] = append([]string{}, aliases...)
		}
		return fm
	}
	ix, err := BuildIndex([]FrontMatter{
		mk("widget-cfg", "alpha-notes", "Widget_Config"),
		mk("ledger-svc", "alpha-notes"),
		mk("twin-one", "alpha-notes", "shared"),
		mk("twin-two", "alpha-notes", "shared"),
	}, nil, Raise)
	if err != nil {
		t.Fatal(err)
	}
	return ix
}

func TestPathRefsOffersEveryComponentAndOneStem(t *testing.T) {
	// 🔴 ONE EXTENSION, NOT A GREEDY STRIP. `foo.tar.gz` offering `foo` would let any
	// dotted filename impersonate a short slug, so the stem of the LAST component is
	// `foo.tar` and nothing shorter. Each row's components are pairwise distinct so no
	// expectation can be satisfied by the wrong pair.
	for _, tc := range []struct {
		path string
		want [][2]string
	}{
		{"apps/widget/values.yaml", [][2]string{
			{"apps", "apps"}, {"widget", "widget"},
			{"values.yaml", "values.yaml"}, {"values", "values"},
		}},
		{"pkg/foo.tar.gz", [][2]string{
			{"pkg", "pkg"}, {"foo.tar.gz", "foo.tar.gz"}, {"foo.tar", "foo.tar"},
		}},
		// A leading dot SEPARATES nothing, so `.gitignore` offers no stem — and its ref
		// keeps the dot, because `normalize_ref` folds only characters outside
		// `[a-z0-9.-]`. Both halves measured against the oracle's own `path_refs`.
		{"cfg/.gitignore", [][2]string{{"cfg", "cfg"}, {".gitignore", ".gitignore"}}},
		// Duplicate (component, ref) pairs collapse with insertion order preserved.
		{"redis/redis/values.yaml", [][2]string{
			{"redis", "redis"}, {"values.yaml", "values.yaml"}, {"values", "values"},
		}},
		// `.` components are dropped, and a trailing slash contributes no empty part.
		{"./apps/widget/", [][2]string{{"apps", "apps"}, {"widget", "widget"}}},
	} {
		if got := PathRefs(tc.path); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("PathRefs(%q)\n got %v\nwant %v", tc.path, got, tc.want)
		}
	}
}

func TestTheAssociationGuardLadderIsORDERED(t *testing.T) {
	// Each row is bad in EVERY position up to the one it names, which is what makes the
	// ORDER observable — a test feeding one bad input at a time passes under any ordering.
	ix := testIndex(t)
	if _, err := AssociatePaths([]string{"/abs"}, ix, "ghost-scope", 0); err == nil ||
		!strings.Contains(err.Error(), "min_paths must be an int >= 1, got 0") {
		t.Fatalf("min_paths is checked FIRST: %v", err)
	}
	_, err := AssociatePaths([]string{"/abs"}, ix, "ghost-scope", 1)
	var unknown *UnknownScopeError
	if !errors.As(err, &unknown) {
		t.Fatalf("then the scope, which a valid min_paths still reaches: %#v", err)
	}
	_, err = AssociatePaths([]string{"/abs"}, ix, "alpha-notes", 1)
	var invalid *InvalidPathError
	if !errors.As(err, &invalid) || !strings.Contains(err.Error(), "manufacture matches") {
		t.Fatalf("then each path, which a valid scope still reaches: %#v", err)
	}
	if _, err := AssociatePaths([]string{"a/../b"}, ix, "alpha-notes", 1); !errors.As(err, &invalid) ||
		!strings.Contains(err.Error(), "`..` escapes the repo root") {
		t.Fatalf("`..` is its OWN sentence, not the absolute-path one: %#v", err)
	}
	// The positive control: a well-formed call over the same operands returns.
	if _, err := AssociatePaths([]string{"apps/widget-cfg/x"}, ix, "alpha-notes", 1); err != nil {
		t.Fatalf("the positive control must pass: %v", err)
	}
}

func TestTheTwoZEROSAreDistinguishable(t *testing.T) {
	// 🔴 `len(Matched) == 0` IS BOTH "nothing was supplied" AND "nothing matched", and
	// downstream those mean opposite things. `ConsideredPaths` is the discriminator, which
	// is what `LookedAtNothing` names.
	ix := testIndex(t)
	none, err := AssociatePaths(nil, ix, "alpha-notes", 1)
	if err != nil {
		t.Fatal(err)
	}
	if !none.LookedAtNothing() || len(none.UnmatchedPaths) != 0 {
		t.Fatalf("an empty window: considered=%v unmatched=%v", none.ConsideredPaths, none.UnmatchedPaths)
	}
	missed, err := AssociatePaths([]string{"apps/nothing-here/x"}, ix, "alpha-notes", 1)
	if err != nil {
		t.Fatal(err)
	}
	if missed.LookedAtNothing() || len(missed.UnmatchedPaths) != 1 {
		t.Fatalf("a window that matched nothing: considered=%v unmatched=%v",
			missed.ConsideredPaths, missed.UnmatchedPaths)
	}
}

func TestAnAmbiguousRefContributesToNoSubsystemAndIsRecorded(t *testing.T) {
	// 🔴 ONE UNDECIDABLE REF MUST NOT BLIND THE WHOLE WINDOW. `shared` names two entries,
	// so it resolves to neither — but the path that produced it is still accounted for, and
	// a path naming a resolvable entry in the SAME call still matches.
	ix := testIndex(t)
	assoc, err := AssociatePaths(
		[]string{"svc/shared/main.go", "apps/widget-cfg/values.yaml"}, ix, "alpha-notes", 1)
	if err != nil {
		t.Fatalf("an ambiguous ref must not be an error: %v", err)
	}
	if len(assoc.Ambiguous) != 1 || assoc.Ambiguous[0].Ref != "shared" ||
		assoc.Ambiguous[0].Tier != "alias" {
		t.Fatalf("ambiguous: %#v", assoc.Ambiguous)
	}
	if want := []string{"twin-one.md", "twin-two.md"}; !reflect.DeepEqual(assoc.Ambiguous[0].Candidates, want) {
		t.Fatalf("the candidates are listed, never picked from: %v", assoc.Ambiguous[0].Candidates)
	}
	if got := assoc.SubsystemRefs(); !reflect.DeepEqual(got, []string{"widget-cfg"}) {
		t.Fatalf("the resolvable path must still match: %v", got)
	}
	// The ambiguous path matched no subsystem, so it is UNMATCHED — accounted for rather
	// than dropped, which is the property that makes `matched` readable on its own.
	if !reflect.DeepEqual(assoc.UnmatchedPaths, []string{"svc/shared/main.go"}) {
		t.Fatalf("unmatched: %v", assoc.UnmatchedPaths)
	}
}

func TestMinPathsPartitionsRatherThanDropping(t *testing.T) {
	// An entry below the floor is in `BelowThreshold`, not gone: a caller reading only
	// `Matched` still has somewhere to look for "something was touched and discarded".
	ix := testIndex(t)
	assoc, err := AssociatePaths([]string{
		"a/widget-cfg/1", "b/widget-cfg/2", "c/ledger-svc/1",
	}, ix, "alpha-notes", 2)
	if err != nil {
		t.Fatal(err)
	}
	if got := assoc.SubsystemRefs(); !reflect.DeepEqual(got, []string{"widget-cfg"}) {
		t.Fatalf("matched: %v", got)
	}
	if len(assoc.BelowThreshold) != 1 || assoc.BelowThreshold[0].Entry.Ref() != "ledger-svc" {
		t.Fatalf("below: %#v", assoc.BelowThreshold)
	}
	// A path that matched is NOT unmatched even when its entry fell below the floor: the
	// two questions are about different things, and folding them would make a discarded
	// match look like a path nothing recognised.
	if len(assoc.UnmatchedPaths) != 0 {
		t.Fatalf("unmatched: %v", assoc.UnmatchedPaths)
	}
}

func TestTheEVIDENCENamesTheAliasAsWRITTEN(t *testing.T) {
	// The alias is normalized for COMPARISON and reported as the file spelled it, so a
	// human reading the evidence sees their own text rather than the fold.
	ix := testIndex(t)
	assoc, err := AssociatePaths([]string{"svc/Widget_Config/main.go"}, ix, "alpha-notes", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(assoc.Matched) != 1 {
		t.Fatalf("matched: %#v", assoc.Matched)
	}
	ev := assoc.Matched[0].Evidence
	if len(ev) != 1 || ev[0].Tier != "alias" || ev[0].MatchedAlias != "Widget_Config" ||
		ev[0].Component != "Widget_Config" || ev[0].Ref != "widget-config" {
		t.Fatalf("evidence: %#v", ev)
	}
}

func TestTheOutputOrderIsCanonicalRefAndNotArrivalOrder(t *testing.T) {
	// 🔴 A RE-RUN OVER THE SAME WINDOW MUST PRODUCE THE SAME ROWS IN THE SAME ORDER, or a
	// diff of two runs shows churn that is not there. The paths below arrive in the reverse
	// of canonical order, which is what makes the rule observable.
	ix := testIndex(t)
	assoc, err := AssociatePaths([]string{"a/widget-cfg/1", "b/ledger-svc/1"}, ix, "alpha-notes", 1)
	if err != nil {
		t.Fatal(err)
	}
	if got := assoc.SubsystemRefs(); !reflect.DeepEqual(got, []string{"ledger-svc", "widget-cfg"}) {
		t.Fatalf("order: %v", got)
	}
	// …and ConsideredPaths keeps INPUT order, which is the opposite rule on purpose: it is
	// evidence about what the caller supplied, not a canonical set.
	if !reflect.DeepEqual(assoc.ConsideredPaths, []string{"a/widget-cfg/1", "b/ledger-svc/1"}) {
		t.Fatalf("considered: %v", assoc.ConsideredPaths)
	}
}
