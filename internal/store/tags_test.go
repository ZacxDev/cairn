package store

import (
	"reflect"
	"strings"
	"testing"
)

// TestTheTagsKeyIsRead is criterion 1: `tags:` is accepted as a sequence, normalized, deduped
// and sorted, and an entry carrying it surfaces its tags.
//
// 🔴 THE RED/GREEN MATRIX, AND WHAT "RED AT THE BASE COMMIT" CAN AND CANNOT MEAN FOR A NEW
// FIELD. At `5c59169` — the base this branch forked from — this file does not COMPILE, because
// `Entry.Tags` does not exist there. That is a red, and it is a weak one: a compile failure is
// the same red for a typo as for the missing feature, so it measures the symptom only in the
// sense that the field it needs is absent.
//
// The informative red is the one that keeps the post-change SHAPE and removes the post-change
// BEHAVIOUR, which is exactly the pre-change behaviour of a reader that has never heard of the
// key: `tags, tagErr := parseTagsField(...)` rewritten to `_, tagErr := …`, so the parse still
// RUNS and its refusals still fire while its RESULT is discarded. Measured with that mutation,
// an entry carrying `tags: [Marketing, internal]` surfaces ZERO tags, `Tags=[]` is named in the
// failure, and the scalar, empty-scalar and non-sequence subtests all keep PASSING — because
// those three are about `sequenceField`, which is shared with `aliases:` and was never broken.
// Both reds were run; the second is the one that attributes.
func TestTheTagsKeyIsRead(t *testing.T) {
	load := func(t *testing.T, tags any) (Entry, error) {
		t.Helper()
		fm := FrontMatter{"service": "widget", "scope": "alpha-notes", "filename": "widget.md"}
		if tags != nil {
			fm["tags"] = tags
		}
		return EntryFromMapping(fm, "widget.md")
	}

	t.Run("a list is normalized, deduped and sorted", func(t *testing.T) {
		// `Marketing` folds to `marketing`, ` internal ` strips and folds, and `MARKETING`
		// is the same tag written a second way — one category, not two.
		entry, err := load(t, []string{"Marketing", " internal ", "MARKETING", "Project_Xyz"})
		if err != nil {
			t.Fatalf("an entry carrying `tags:` was refused: %v", err)
		}
		want := []string{"internal", "marketing", "project-xyz"}
		if !reflect.DeepEqual(entry.Tags, want) {
			t.Fatalf("Tags=%q, want %q — folded by `NormalizeRef`, deduped, sorted",
				entry.Tags, want)
		}
		// ⚠ AND THE OTHER FIELDS ARE UNTOUCHED, so "the key is read" is not satisfied by a
		// parser that read it into the wrong place.
		if entry.Slug != "widget" || entry.Scope != "alpha-notes" || len(entry.Aliases) != 0 {
			t.Fatalf("reading `tags:` disturbed the rest of the entry: %+v", entry)
		}
	})

	t.Run("a non-empty bare string is refused by a message naming the fix", func(t *testing.T) {
		_, err := load(t, "marketing")
		if err == nil {
			t.Fatal("`tags: marketing` loaded — a scalar is a mistake worth naming rather than " +
				"silently flattening to a one-element list")
		}
		const want = "`tags:` must be a list, not a bare string — write `tags: [<name>]`"
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("the refusal does not name the fix.\nwant substring: %s\ngot: %v", want, err)
		}
		// The sentinel, which is the HTTP contract a `PUT` quotes.
		if !strings.HasPrefix(err.Error(), "malformed index entry ") {
			t.Fatalf("the refusal lost its sentinel: %v", err)
		}
	})

	t.Run("an empty scalar reads as an absent key", func(t *testing.T) {
		// 🔴 `tags:` WITH NOTHING AFTER IT IS THE `or ()` RULE, which `sequenceField` owns and
		// `aliases:` proved: an empty scalar is an ABSENT key, so the bare-string refusal fires
		// only on a key that really carries a scalar.
		empty, err := load(t, "")
		if err != nil {
			t.Fatalf("`tags:` with nothing after it was refused: %v", err)
		}
		absent, err := load(t, nil)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(empty, absent) {
			t.Fatalf("an empty `tags:` and an absent one differ.\nempty:  %+v\nabsent: %+v",
				empty, absent)
		}
		// And an EMPTY LIST is the same answer again, the way `tasks: []` is.
		emptyList, err := load(t, []string{})
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(emptyList, absent) {
			t.Fatalf("`tags: []` and an absent `tags:` differ.\nlist:   %+v\nabsent: %+v",
				emptyList, absent)
		}
	})

	t.Run("a non-sequence non-string is refused", func(t *testing.T) {
		// The `default` arm of `sequenceField`, reachable only from a mapping a caller built by
		// hand — the line parser cannot produce it. Pinned so the arm is not dead.
		if _, err := load(t, 42); err == nil {
			t.Fatal("`tags: 42` loaded")
		} else if !strings.Contains(err.Error(), "`tags:` must be a list, got int") {
			t.Fatalf("unexpected refusal: %v", err)
		}
	})
}

// TestATagThatNormalizesAwayIsRefused is criterion 2: refused, not silently dropped.
//
// 🔴 WHY DROPPING IT WOULD BE THE WORSE FAILURE, which is the reason this is a criterion of its
// own rather than a detail of the one above. A dropped tag leaves a file that DECLARES a
// category beside an index that does not carry it, so `?tag=` answers "no entry carries this"
// about an entry whose own front matter says it does — an empty result whose cause is invisible
// at both ends, and the reader has no way to tell it from a tag nobody has used yet.
//
// ⚠ RED/GREEN: the same matrix `TestTheTagsKeyIsRead` carries. With the fold's result discarded
// the whole file does not reach this assertion at all; with `parseTagsField`'s `normalized ==
// ""` arm deleted — the isolated mutation, the enclosing loop untouched — every row below loads
// clean and this test fails on the first one, naming the tag that vanished.
func TestATagThatNormalizesAwayIsRefused(t *testing.T) {
	// Each of these strips to something non-empty and folds to nothing, because every
	// character is outside `[a-z0-9.-]`. `NormalizeRef` also trims the dashes it produces,
	// which is why a string of pure punctuation ends up empty rather than as a run of dashes.
	for _, away := range []string{"!!!", "***", "///", " @ ", "â"} {
		if got := NormalizeRef(away); got != "" {
			t.Fatalf("the fixture is wrong: %q folds to %q, not away — this row cannot reach "+
				"the refusal it is here for", away, got)
		}
		_, err := EntryFromMapping(FrontMatter{
			"service":  "widget",
			"scope":    "alpha-notes",
			"filename": "widget.md",
			"tags":     []string{"marketing", away},
		}, "widget.md")
		if err == nil {
			t.Fatalf("tag %q folded away and the entry loaded anyway — the file declares a "+
				"category the index does not carry", away)
		}
		want := "tag " + PyRepr(away) + " normalizes to the empty string — a tag must fold to " +
			"at least one of `[a-z0-9.-]`"
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("tag %q: the refusal does not name the fold.\nwant substring: %s\ngot: %v",
				away, want, err)
		}
	}
	// The control: a tag that folds to SOMETHING is accepted, so the rows above are not "every
	// tag is refused".
	entry, err := EntryFromMapping(FrontMatter{
		"service": "widget", "scope": "alpha-notes", "filename": "widget.md",
		"tags": []string{"!!!marketing!!!"},
	}, "widget.md")
	if err != nil {
		t.Fatalf("a tag with punctuation AROUND something was refused: %v", err)
	}
	if !reflect.DeepEqual(entry.Tags, []string{"marketing"}) {
		t.Fatalf("Tags=%q, want [marketing]", entry.Tags)
	}
}

// TestTheTagPredicateIsAND pins `EntryHasAllTags`' semantics, which are what a repeatable
// `--tag` means.
//
// ⚠ AN INVARIANT GUARD ON THE EMPTY-SET ROW, REGRESSION COVERAGE ON THE REST. Nothing ever
// shipped an OR here; what the rows pin is that a second `--tag` can only ever NARROW, which is
// the property that makes the flag a filter rather than a widener.
func TestTheTagPredicateIsAND(t *testing.T) {
	entry := Entry{Tags: []string{"internal", "marketing"}}
	for _, row := range []struct {
		want []string
		hit  bool
		why  string
	}{
		{nil, true, "no tags asked for matches every entry — `len(want) == 0` is how the callers " +
			"make 'no filter' and 'a filter that removes nothing' one code path"},
		{[]string{}, true, "an empty non-nil set is the same answer as nil"},
		{[]string{"marketing"}, true, "one tag the entry carries"},
		{[]string{"marketing", "internal"}, true, "both tags, in the other order"},
		{[]string{"marketing", "finance"}, false, "AND: one carried, one not — an OR would " +
			"return true here, and a second --tag would WIDEN the result"},
		{[]string{"finance"}, false, "a tag the entry does not carry"},
		{[]string{"market"}, false, "no prefix matching: a tag is a whole folded token"},
	} {
		if got := EntryHasAllTags(entry, row.want); got != row.hit {
			t.Errorf("EntryHasAllTags(%q) = %v, want %v — %s", row.want, got, row.hit, row.why)
		}
	}
}

// TestTagOperandsFoldTheSameWayTheFileDoes is the seam between the two ends of this feature.
//
// 🔴 IT IS A RELATIONSHIP AND NOT TWO COMPONENT CHECKS, WHICH IS THE ONLY SHAPE THAT CAN SEE
// THE DEFECT. `parseTagsField` folding correctly and `NormalizeTags` folding correctly are two
// hermetic claims; the thing that breaks a user is the two disagreeing, and neither test can
// see that on its own. So this asserts the WRITE side's answer and the QUERY side's answer are
// equal over the same inputs — a query an operator can write must reach a tag an operator can
// declare.
func TestTagOperandsFoldTheSameWayTheFileDoes(t *testing.T) {
	for _, raw := range []string{
		"Marketing", "MARKETING", " marketing ", "Project_Xyz", "project--xyz",
		"a.b", "Ops-Runbook", "İa",
	} {
		entry, err := EntryFromMapping(FrontMatter{
			"service": "widget", "scope": "alpha-notes", "filename": "widget.md",
			"tags": []string{raw},
		}, "widget.md")
		if err != nil {
			t.Fatalf("declaring tag %q was refused: %v", raw, err)
		}
		operands := NormalizeTags([]string{raw})
		if !reflect.DeepEqual(entry.Tags, operands) {
			t.Fatalf("%q: the file folds it to %q and a query folds it to %q — a tag that can be "+
				"WRITTEN but not ASKED FOR", raw, entry.Tags, operands)
		}
		// …and the predicate joins them, which is the behavioural half a structural equality
		// check would type-check straight past.
		if !EntryHasAllTags(entry, operands) {
			t.Fatalf("%q: the entry declares %q and the query asks for %q and the predicate says "+
				"no", raw, entry.Tags, operands)
		}
	}
	// The control on the comparison itself: two inputs that fold DIFFERENTLY must not compare
	// equal, or the loop above would pass with either side wired to a constant.
	if reflect.DeepEqual(NormalizeTags([]string{"marketing"}), NormalizeTags([]string{"finance"})) {
		t.Fatal("NormalizeTags returns the same value for two different tags, so every " +
			"comparison above is vacuous")
	}
	// And `NormalizeTags` dedupes across operands, so `?tag=Marketing&tag=marketing` narrows by
	// one tag rather than asserting one tag twice.
	if got := NormalizeTags([]string{"Marketing", "marketing", "internal"}); !reflect.DeepEqual(
		got, []string{"internal", "marketing"}) {
		t.Fatalf("NormalizeTags did not dedupe across operands: %q", got)
	}
}
