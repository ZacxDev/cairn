package store

import (
	"os"
	"path/filepath"
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

// TestTheTagPredicateIsWholeTokenMembership pins `HasTag`, which is the whole of what a scalar
// `--tag`/`?tag=` means.
//
// ⚠ AN INVARIANT GUARD, LABELLED AS ONE. Nothing ever shipped a prefix match or a substring
// match here; what the rows pin is that a tag is a WHOLE folded token, which is the property the
// `?tag=` links on the browser surface and the corpus's fold rows both rely on. It is not
// regression coverage for any defect — the AND/OR question it used to pin is gone with the
// repeatability, and there is no quantifier left to get wrong.
//
// 🔴 IT IS ALSO THE ONE BEHAVIOURAL CHECK ON THE PREDICATE ALL THREE CALLERS NOW SHARE
// (`report.Recall`, `report.Search`, `ui.EntriesByTag`), which is why it survives the deletion
// rather than going with the AND rows: a structural claim that three sites call one function
// type-checks past a function that answers wrongly.
func TestTheTagPredicateIsWholeTokenMembership(t *testing.T) {
	tags := []string{"internal", "marketing"}
	for _, row := range []struct {
		want string
		hit  bool
		why  string
	}{
		{"marketing", true, "a tag the set carries"},
		{"internal", true, "the other one, so neither row can pass on position alone"},
		{"finance", false, "a tag the set does not carry"},
		{"market", false, "no prefix matching: a tag is a whole folded token"},
		{"marketing-plan", false, "nor the other direction — the asked-for tag is not a prefix " +
			"of a carried one either"},
		{"", false, "the empty tag carries nothing, which is why a folded-away operand must be " +
			"REFUSED upstream rather than compared: it would narrow to nothing here and read " +
			"as `no entry is tagged ''`"},
	} {
		if got := HasTag(tags, row.want); got != row.hit {
			t.Errorf("HasTag(%q, %q) = %v, want %v — %s", tags, row.want, got, row.hit, row.why)
		}
	}
}

// TestTagOperandsFoldTheSameWayTheFileDoes is the seam between the two ends of this feature.
//
// 🔴 IT IS A RELATIONSHIP AND NOT TWO COMPONENT CHECKS, WHICH IS THE ONLY SHAPE THAT CAN SEE
// THE DEFECT. `parseTagsField` folding correctly and the QUERY side folding correctly are two
// hermetic claims; the thing that breaks a user is the two disagreeing, and neither test can
// see that on its own. So this asserts the WRITE side's answer and the QUERY side's answer are
// equal over the same inputs — a query an operator can write must reach a tag an operator can
// declare.
//
// ⚠ THE QUERY SIDE IS `NormalizeRef` DIRECTLY, WHERE IT USED TO BE AN OPERAND-SET NORMALISER.
// With a scalar operand there is nothing to dedupe or sort, so the fold is one call — and the
// seam is the same seam: `report.canonicalTag` is `NormalizeRef` plus the refusal, and
// `ui.handlePage` is `NormalizeRef` alone.
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
		operand := NormalizeRef(raw)
		if !reflect.DeepEqual(entry.Tags, []string{operand}) {
			t.Fatalf("%q: the file folds it to %q and a query folds it to %q — a tag that can be "+
				"WRITTEN but not ASKED FOR", raw, entry.Tags, operand)
		}
		// …and the predicate joins them, which is the behavioural half a structural equality
		// check would type-check straight past.
		if !HasTag(entry.Tags, operand) {
			t.Fatalf("%q: the entry declares %q and the query asks for %q and the predicate says "+
				"no", raw, entry.Tags, operand)
		}
	}
	// The control on the comparison itself: two inputs that fold DIFFERENTLY must not compare
	// equal, or the loop above would pass with either side wired to a constant.
	if NormalizeRef("marketing") == NormalizeRef("finance") {
		t.Fatal("NormalizeRef returns the same value for two different tags, so every " +
			"comparison above is vacuous")
	}
}

// TestTheReaderSTILLLOADSAnOffVocabularyTag is the guard that stops the closed tag
// vocabulary being implemented in the one place that would cause an outage.
//
// 🔴 WHAT A REFUSAL HERE WOULD COST, WHICH IS THE WHOLE REASON THIS TEST EXISTS. The
// declared set lives in `internal/write`'s `tagVocabulary` and is enforced by
// `write.validateEntryBytes` — the WRITE path. If it were enforced by `parseTagsField`
// instead, every entry already carrying an off-vocabulary tag would become MALFORMED, and a
// malformed entry is not merely unrendered: it is out of the index, so `--ref` and
// `--search` lose it, AND it is UNWRITABLE, because every write route resolves its target
// through that same index and answers 404 `ref-unknown`. Unreadable and unrepairable in one
// stroke, over data that was valid when it was written.
//
// 🔴 SO THIS ASSERTS FOUR SURFACES, NOT ONLY "IT PARSED". A parse that returns an entry
// proves nothing about the index it has to land in, and "the entry is in the index" is the
// claim the outage above is actually about. Measured over a real store on disk rather than a
// hand-built `FrontMatter`, because `LoadStore` is what the pod and both clients call.
//
// ⚠ AN INVARIANT GUARD AS WRITTEN — the reader has never refused an undeclared tag, so no
// defect is being pinned. It was watched RED anyway, by the only mutation that can produce
// the hazard: adding the vocabulary comparison to `parseTagsField`. Measured with that
// mutation, this test reports the entry as MALFORMED and `ResolveRefTiered` answers nothing
// for its ref — which is what a reader-side implementation looks like from the outside.
func TestTheReaderSTILLLOADSAnOffVocabularyTag(t *testing.T) {
	// Two tags NEITHER of which is in the write path's declared set, written the way an
	// operator would have written them before the vocabulary closed.
	const body = "---\n" +
		"service: legacy-note\n" +
		"scope: alpha-notes\n" +
		"tags: [Marketing, project-xyz]\n" +
		"---\n" +
		"\n" +
		"## What it is\n" +
		"A synthetic entry written before the vocabulary closed.\n" +
		"\n" +
		"## Nuance / work-history\n" +
		"- 2000-01-02: the synthetic action this entry records.\n"

	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "alpha-notes"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "alpha-notes", "legacy-note.md"),
		[]byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	index, err := LoadStore(root, "recalled", Unrestricted())
	if err != nil {
		t.Fatalf("a store holding one entry with off-vocabulary tags would not load: %v", err)
	}

	// 1. NOT MALFORMED. This is the assertion the whole test is for: a degrading load
	//    collects rejects rather than raising, so a refusal would show up here as a ROW
	//    and NOT as an error from `LoadStore` — which is exactly how such a change could
	//    ship looking green.
	if bad := index.Malformed; len(bad) > 0 {
		t.Fatalf("the reader classified an off-vocabulary tag as MALFORMED, which takes the "+
			"entry out of the index AND out of every write route: %+v", bad)
	}
	// 🔴 THE POSITIVE CONTROL ON THAT CHANNEL, IN THE SAME RUN. An empty `Malformed` is
	// indistinguishable from a field nothing ever writes, so a second scope holding a file
	// the loader really does refuse must show up in it. Without this the check above is the
	// reassuring zero this repository's rules name.
	if err := os.MkdirAll(filepath.Join(root, "rubble-heap"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "rubble-heap", "broken-four.md"),
		[]byte("---\nservice: broken-four\nscope: rubble-heap\n"+
			"aliases: a bare string, which the schema refuses\n---\n\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	control, err := LoadStore(root, "recalled", Unrestricted())
	if err != nil {
		t.Fatal(err)
	}
	if len(control.Malformed) == 0 {
		t.Fatal("a file the loader genuinely refuses did not appear in `Malformed`, so the " +
			"check above measured a channel wired to nothing")
	}
	for _, row := range control.Malformed {
		if strings.Contains(row.Filename, "legacy-note") {
			t.Fatalf("the entry under test appeared in `Malformed` once a sibling was "+
				"added: %+v", control.Malformed)
		}
	}

	// 2. IN THE INDEX, under its scope, with the tags folded rather than dropped.
	entries, scopeErr := index.Entries("alpha-notes")
	if scopeErr != nil {
		t.Fatalf("the scope is unknown to the index: %v", scopeErr)
	}
	if len(entries) != 1 {
		t.Fatalf("the index holds %d entry/entries, want 1: %+v", len(entries), entries)
	}
	if got := entries[0].Tags; !reflect.DeepEqual(got, []string{"marketing", "project-xyz"}) {
		t.Fatalf("Tags=%q — the reader must surface an undeclared tag folded, not drop it "+
			"and not refuse it", got)
	}

	// 3. RESOLVABLE BY REF, which is what `--ref` reads and what every write route resolves
	//    through. An entry in the index but unreachable by ref is still unwritable.
	resolved, _, resolveErr := ResolveRefTiered("legacy-note", index, "alpha-notes")
	if resolveErr != nil {
		t.Fatalf("`--ref legacy-note` does not resolve, so `put`/`append` would answer 404 "+
			"for an entry that is sitting right there: %v", resolveErr)
	}
	if resolved == nil || resolved.Filename != "legacy-note.md" {
		t.Fatalf("the ref resolved to %+v", resolved)
	}

	// 4. FINDABLE BY THE TAG FILTER, using the undeclared tag as the operand — the read
	//    surface deliberately does NOT consult the write vocabulary.
	if !HasTag(resolved.Tags, NormalizeRef("Marketing")) {
		t.Fatalf("`--tag Marketing` does not find an entry declaring it: %q", resolved.Tags)
	}
	// The control on that predicate: a tag the entry does NOT carry must not match, or the
	// assertion above would pass with `HasTag` wired to true. `infra` is a DECLARED term,
	// so this also pins that membership is not satisfied by the vocabulary.
	if HasTag(resolved.Tags, "infra") {
		t.Fatal("HasTag matched a tag the entry does not carry, so the check above is vacuous")
	}
}
