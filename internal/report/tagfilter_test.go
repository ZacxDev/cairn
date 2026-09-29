package report

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ZacxDev/cairn/internal/store"
)

// twoPrincipalTagWorld is a store with ONE tag written into TWO scopes, so a principal who can
// read only one of them is a real discriminator rather than a fixture detail.
//
// ⚠ THE TAG IS ON AN ENTRY IN EACH SCOPE, AND THE ENTRY NAMES DIFFER. A fixture where only the
// unreachable scope carried the tag could not tell "the filter narrowed after authorisation"
// from "the filter is broken and finds nothing"; a fixture where only the reachable one carried
// it could not see a leak at all. Both scopes carry it, and the assertions below read WHICH.
//
// ⚠ AND THE READABLE SCOPE HOLDS AN ENTRY THAT DOES *NOT* CARRY IT, so the filter has something
// to remove and its numerator is not trivially the scope total — the shape a filter wired to
// "keep everything" cannot answer correctly.
func twoPrincipalTagWorld(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	write := func(scope, name string, lines ...string) {
		dir := filepath.Join(root, scope)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("building the store: %v", err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(strings.Join(lines, "\n")), 0o644); err != nil {
			t.Fatalf("writing %s/%s: %v", scope, name, err)
		}
	}
	write("alpha-notes", "runbook.md",
		"---", "service: runbook", "scope: alpha-notes",
		"tags: [marketing, internal]", "---", "",
		"## What it is", "", "The alpha rollout runbook.", "",
		store.NuanceHeading, "", "- 2000-06-01 a bullet mentioning readiness", "")
	// The SAME tag, in a scope the first principal cannot read.
	write("beta-notes", "ledger.md",
		"---", "service: ledger", "scope: beta-notes",
		"tags: [marketing]", "---", "",
		"## What it is", "", "The beta ledger.", "",
		store.NuanceHeading, "", "- 2000-06-01 a bullet mentioning readiness", "")
	// An entry in the READABLE scope carrying no tags at all.
	write("alpha-notes", "unrelated.md",
		"---", "service: unrelated", "scope: alpha-notes", "---", "",
		"## What it is", "", "Carries no tags at all.", "")
	return root
}

// sharedTag is the tag both scopes carry.
const sharedTag = "marketing"

// TestTheTagFilterNarrowsAfterScopeAuthorisation is criterion 5 — the one that matters.
//
// 🔴 WATCHED RED WITH THE NARROW APPLIED IN THE WRONG ORDER. The mutation is the obvious
// implementation of a store-wide question: `store.LoadStore(storeRoot, verb, store.Unrestricted())`
// in place of the caller's `visible`, then the same tag filter. Measured with it, the
// `alpha-notes`-only principal's recall of `beta-notes` answered `recalled` with
// `beta-notes/ledger.md` in the index and `beta-notes` on `KnownScopes` — an entry, a scope name
// and a tag from a directory that principal cannot read. The filter is CORRECT in both orderings;
// only the AUTHORISATION differs, which is precisely why a single-principal test cannot see it.
//
// 🔴 AND A TAG IS THE OPERAND MOST LIKELY TO INVITE THAT MISTAKE. "Every marketing entry, across
// scopes" is the question the key exists for, and a store-wide load is the obvious way to answer
// a store-wide question — more obvious than for a reverse lookup, because a reverse lookup at
// least names one external thing while a tag names a set of the store's own contents.
//
// ⚠ IT IS A PAIR, NOT A ZERO. The `beta-notes` principal must FIND the tag, or the first
// principal's empty answer is indistinguishable from a filter wired to nothing.
func TestTheTagFilterNarrowsAfterScopeAuthorisation(t *testing.T) {
	root := twoPrincipalTagWorld(t)
	onlyAlpha := store.VisibleScopeSet([]string{"alpha-notes"})
	onlyBeta := store.VisibleScopeSet([]string{"beta-notes"})

	t.Run("recall", func(t *testing.T) {
		opts := func(scope string) RecallOptions {
			return RecallOptions{
				Scope: scope, Mode: DefaultMode, Limit: DefaultEntryLimit, Page: 1,
				Tag: sharedTag, HasTag: true,
			}
		}
		// POSITIVE CONTROL: the principal who CAN read `beta-notes` finds the tag there.
		asBeta, err := Recall(root, opts("beta-notes"), onlyBeta)
		if err != nil {
			t.Fatalf("recall as beta: %v", err)
		}
		if asBeta.Status != StatusRecalled || asBeta.TotalInScope != 1 {
			t.Fatalf("POSITIVE CONTROL FAILED: the principal who can read `beta-notes` got "+
				"status=%q total=%d, want recalled/1. The zero below is then a fact about a "+
				"filter wired to nothing.", asBeta.Status, asBeta.TotalInScope)
		}

		// The narrowing: `alpha-notes`' principal asking for `beta-notes` gets the answer a
		// scope that does not exist gives, because the scope is absent from their INDEX.
		asAlpha, err := Recall(root, opts("beta-notes"), onlyAlpha)
		if err != nil {
			t.Fatalf("recall as alpha: %v", err)
		}
		if asAlpha.Status != StatusScopeAbsent {
			t.Fatalf("a principal who cannot read `beta-notes` got status=%q, want %q — a "+
				"refused scope and a never-existed one are indistinguishable by design",
				asAlpha.Status, StatusScopeAbsent)
		}
		// 🔴 AND NO BETA ENTRY OR SCOPE NAME CAME BACK ON ANY FIELD. This is the assertion the
		// wrong ordering fails: it answered `recalled` here.
		assertNoBetaLeak(t, asAlpha.KnownScopes, asAlpha.Entries, asAlpha.Listing,
			asAlpha.Malformed, asAlpha.MalformedElsewhere)

		// …and asking about their OWN scope still narrows correctly: 1 of 2 entries.
		own, err := Recall(root, opts("alpha-notes"), onlyAlpha)
		if err != nil {
			t.Fatalf("recall as alpha on alpha: %v", err)
		}
		if own.Status != StatusRecalled {
			t.Fatalf("own scope: status=%q, want recalled", own.Status)
		}
		if own.TotalInScope != 1 || own.TagScopeTotal != 2 || own.TagMatched != 1 {
			t.Fatalf("own scope: matched=%d of %d (TotalInScope=%d), want 1 of 2 and 1 — the "+
				"filter must remove the entry that carries no tags and report what it removed",
				own.TagMatched, own.TagScopeTotal, own.TotalInScope)
		}
		assertNoBetaLeak(t, own.KnownScopes, own.Entries, own.Listing, own.Malformed,
			own.MalformedElsewhere)
	})

	t.Run("search all scopes", func(t *testing.T) {
		// 🔴 `AllScopes` IS THE HARDER HALF AND IT IS WHY THIS SUBTEST EXISTS. `?all_scopes=1`
		// NAMES NO SCOPE, so a per-scope refusal check has nothing to refuse — `Search`'s own
		// header says the index filter is what makes it safe. A tag filter over a store-wide
		// load would re-open exactly that hole, and the scope name cannot be the thing that
		// stops it. This is also the literal shape of "every marketing entry, across scopes".
		opts := SearchOptions{
			Query: "readiness", Context: ContextBullet, Threshold: DefaultThreshold,
			MaxHits: DefaultMaxHits, AllScopes: true,
			Tag: sharedTag, HasTag: true,
		}
		asBeta, err := Search(root, opts, onlyBeta)
		if err != nil {
			t.Fatalf("search as beta: %v", err)
		}
		if asBeta.EntriesSearched != 1 || len(asBeta.Hunks) == 0 {
			t.Fatalf("POSITIVE CONTROL FAILED: beta's store-wide search over the tag searched "+
				"%d entries and found %d hunks, want 1 and >0", asBeta.EntriesSearched,
				len(asBeta.Hunks))
		}
		if got := asBeta.ScopesSearched; len(got) != 1 || got[0] != "beta-notes" {
			t.Fatalf("beta's store-wide search walked %v, want exactly [beta-notes]", got)
		}

		asAlpha, err := Search(root, opts, onlyAlpha)
		if err != nil {
			t.Fatalf("search as alpha: %v", err)
		}
		if got := asAlpha.ScopesSearched; len(got) != 1 || got[0] != "alpha-notes" {
			t.Fatalf("alpha's store-wide search walked %v, want exactly [alpha-notes] — a wider "+
				"set means the tag filter ran over a scope alpha cannot see, whether or not it "+
				"reported a hit from it", got)
		}
		if asAlpha.EntriesSearched != 1 || asAlpha.TagSkipped != 1 {
			t.Fatalf("alpha searched %d and tag-skipped %d, want 1 and 1 — the filter must keep "+
				"the one alpha entry carrying the tag and remove the one that does not",
				asAlpha.EntriesSearched, asAlpha.TagSkipped)
		}
		for _, h := range asAlpha.Hunks {
			if h.Scope == "beta-notes" {
				t.Fatalf("a hunk from `beta-notes` reached a principal who cannot read it: %+v", h)
			}
		}
		assertNoBetaLeak(t, asAlpha.KnownScopes, nil, nil, asAlpha.Malformed,
			asAlpha.MalformedElsewhere)
	})
}

// TestTheTagFilterAnswersItsOwnNonFinding is criterion 6: a `--tag` matching nothing exits with
// the store's EXISTING empty-result vocabulary rather than a new exit code.
//
// 🔴 THE EXIT CODE IS THE CRITERION AND THE STATUS IS NOT. A new status is a new
// `X-Store-Status` value and the two clients' printed tables do not carry statuses; what they
// carry is the EXIT MODEL, and `UnreadableStatuses` is the one place a non-zero is decided. So
// `tag-absent` must be outside that set — "nothing carries this tag" is an ANSWER, and only
// "nothing could be read" is a non-zero.
func TestTheTagFilterAnswersItsOwnNonFinding(t *testing.T) {
	root := twoPrincipalTagWorld(t)
	rep, err := Recall(root, RecallOptions{
		Scope: "alpha-notes", Mode: DefaultMode, Limit: DefaultEntryLimit, Page: 1,
		Tag: "no-such-category", HasTag: true,
	}, store.Unrestricted())
	if err != nil {
		t.Fatalf("recall: %v", err)
	}
	if rep.Status != StatusTagAbsent {
		t.Fatalf("status=%q, want %q — `scope-empty` would claim the directory holds nothing, "+
			"which is false of a scope holding two entries", rep.Status, StatusTagAbsent)
	}
	if rep.TagScopeTotal != 2 || rep.TagMatched != 0 {
		t.Fatalf("TagScopeTotal=%d TagMatched=%d, want 2 and 0 — the answer must say how much "+
			"WAS read", rep.TagScopeTotal, rep.TagMatched)
	}
	text := rep.RenderText("synthetic-host", nil, "")
	for _, want := range []string{
		// 🔴 THE WHOLE HEADER LINE, NOT `status=tag-absent`. A `Contains` on the bare status is
		// walkable by any status with it as a PREFIX — measured: a mutant renaming the constant
		// to `tag-absent-XX` SURVIVED this assertion, because the rendered `status=tag-absent-XX`
		// contains `status=tag-absent`. The scope clause is what closes the prefix.
		"subsystem-recall: status=tag-absent scope=alpha-notes",
		"  tag: `no-such-category` — 0 of 2 entries in `alpha-notes/` carry it",
		"NO ENTRY IS TAGGED `no-such-category` — the 2 entries in `alpha-notes/` were read and " +
			"none of them carries it.",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("the rendered non-finding does not carry %q.\ngot:\n%s", want, text)
		}
	}
	// 🔴 IT MUST NOT SAY WHAT `scope-empty` SAYS. A consumer reports `scope-empty` as an
	// ordinary non-finding about a DIRECTORY; a shared opening phrase is all it would take for
	// "no entry in this scope is tagged X" to be read as "this scope is empty".
	if strings.Contains(text, "NOTHING RECORDED YET") {
		t.Errorf("the tag filter's zero borrowed `scope-empty`'s words:\n%s", text)
	}
	// 🔴 AND THE EXIT CODE IS THE DOCUMENTED READ OUTCOME, 0.
	if code, warning := ExitFor(rep.Status, rep.Scope+"/", rep.Malformed); code != 0 {
		t.Errorf("tag-absent exits %d with %q, want 0", code, warning)
	}
	// The CONTROL on that claim: a status that IS in `UnreadableStatuses` still exits 3, so the
	// zero above is not "ExitFor returns 0 for everything".
	if code, _ := ExitFor(StatusScopeUnreadable, "x/", nil); code != 3 {
		t.Error("ExitFor cannot return a non-zero, so the assertion above measures nothing")
	}
}

// TestTheTagNarrowingAnnouncesItself pins the header line on BOTH report types, because the
// counts under it are about the narrowed set.
func TestTheTagNarrowingAnnouncesItself(t *testing.T) {
	root := twoPrincipalTagWorld(t)

	// The operand is SCALAR, so the verb never moves: the line always reads "carry it".
	one, err := Recall(root, RecallOptions{
		Scope: "alpha-notes", Mode: DefaultMode, Limit: DefaultEntryLimit, Page: 1,
		Tag: "Marketing", HasTag: true,
	}, store.Unrestricted())
	if err != nil {
		t.Fatalf("recall: %v", err)
	}
	// The CANONICAL spelling: the query said `Marketing`, the line says `marketing`.
	wantOne := "  tag: `marketing` — 1 of 2 entries in `alpha-notes/` carry it, and everything " +
		"below is about those 1. This is a NARROWING, not a truncation: the rest were read and " +
		"did not match."
	if got := one.RenderText("synthetic-host", nil, ""); !strings.Contains(got, wantOne) {
		t.Errorf("the recall header lacks the one-tag line.\nwanted: %s\ngot:\n%s", wantOne, got)
	}

	// ⚠ AND THE OTHER TAG ON THE SAME ENTRY GETS ITS OWN LINE, which is what keeps the line above
	// from passing with the operand ignored: `internal` is carried by `runbook` and by nothing
	// else in this scope, so its line names a DIFFERENT tag over the same counts.
	other, err := Recall(root, RecallOptions{
		Scope: "alpha-notes", Mode: DefaultMode, Limit: DefaultEntryLimit, Page: 1,
		Tag: "internal", HasTag: true,
	}, store.Unrestricted())
	if err != nil {
		t.Fatalf("recall: %v", err)
	}
	wantOther := "  tag: `internal` — 1 of 2 entries in `alpha-notes/` carry it, and everything " +
		"below is about those 1."
	if got := other.RenderText("synthetic-host", nil, ""); !strings.Contains(got, wantOther) {
		t.Errorf("the recall header lacks the second tag's line.\nwanted: %s\ngot:\n%s",
			wantOther, got)
	}

	srch, err := Search(root, SearchOptions{
		Scope: "alpha-notes", Query: "readiness", Context: ContextBullet,
		Threshold: DefaultThreshold, MaxHits: DefaultMaxHits,
		Tag: sharedTag, HasTag: true,
	}, store.Unrestricted())
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if got := srch.RenderText("synthetic-host", nil, ""); !strings.Contains(got, wantOne) {
		t.Errorf("the search header lacks the tag line.\nwanted: %s\ngot:\n%s", wantOne, got)
	}

	// ⚠ AND IT IS ABSENT WITHOUT THE FILTER, which is what keeps every existing golden still.
	plain, err := Recall(root, RecallOptions{
		Scope: "alpha-notes", Mode: DefaultMode, Limit: DefaultEntryLimit, Page: 1,
	}, store.Unrestricted())
	if err != nil {
		t.Fatalf("recall: %v", err)
	}
	if strings.Contains(plain.RenderText("synthetic-host", nil, ""), "  tag:") {
		t.Error("a recall with no `--tag` printed a tag line, which would move every golden")
	}
	// …and the BODY line is emitted for a tagged entry and absent for an untagged one, which is
	// the other place this key reaches a rendered byte.
	//
	// ⚠ ASKED BY `--ref`, NOT READ OFF THE DIGEST. The digest features the NEWEST entry by mtime,
	// which in this fixture is the untagged one — so a digest-based assertion here would measure
	// the featured-entry selector rather than the tags line, and would flip on any change to the
	// fixture's write order.
	tagged, err := Recall(root, RecallOptions{
		Scope: "alpha-notes", Mode: DefaultMode, Limit: DefaultEntryLimit, Page: 1,
		Ref: "runbook", HasRef: true,
	}, store.Unrestricted())
	if err != nil {
		t.Fatalf("recall: %v", err)
	}
	if !strings.Contains(tagged.RenderText("synthetic-host", nil, ""), "    tags: internal, marketing") {
		t.Errorf("a tagged entry's body does not print its tags:\n%s",
			tagged.RenderText("synthetic-host", nil, ""))
	}
	untagged, err := Recall(root, RecallOptions{
		Scope: "alpha-notes", Mode: DefaultMode, Limit: DefaultEntryLimit, Page: 1,
		Ref: "unrelated", HasRef: true,
	}, store.Unrestricted())
	if err != nil {
		t.Fatalf("recall: %v", err)
	}
	if strings.Contains(untagged.RenderText("synthetic-host", nil, ""), "    tags:") {
		t.Error("an entry with no `tags:` printed an empty tags line")
	}
}

// TestTagComposesWithRef pins that the two narrowings compose rather than one shadowing the
// other — including the case the resolver cannot see.
func TestTagComposesWithRef(t *testing.T) {
	root := twoPrincipalTagWorld(t)
	rep := func(ref, tag string) RecallReport {
		t.Helper()
		out, err := Recall(root, RecallOptions{
			Scope: "alpha-notes", Mode: DefaultMode, Limit: DefaultEntryLimit, Page: 1,
			Ref: ref, HasRef: true, Tag: tag, HasTag: true,
		}, store.Unrestricted())
		if err != nil {
			t.Fatalf("recall: %v", err)
		}
		return out
	}
	// The entry DOES carry the tag: one body, in full.
	hit := rep("runbook", sharedTag)
	if hit.Status != StatusRecalled || len(hit.Entries) != 1 {
		t.Fatalf("`--ref runbook --tag marketing`: status=%q entries=%d, want recalled/1",
			hit.Status, len(hit.Entries))
	}
	// 🔴 THE ENTRY DOES NOT CARRY IT. `ResolveRefTiered` resolves against the whole index and
	// knows nothing about the tag set, so without the membership test this would print
	// `unrelated` in full — the report answering a question nobody asked.
	miss := rep("unrelated", sharedTag)
	if miss.Status != StatusTagAbsent {
		t.Fatalf("`--ref unrelated --tag marketing`: status=%q, want %q; entries=%d",
			miss.Status, StatusTagAbsent, len(miss.Entries))
	}
	if len(miss.Entries) != 0 {
		t.Fatalf("a body was printed for an entry that does not carry the tag: %+v", miss.Entries)
	}
	// 🔴 AND THE RENDERED TEXT, BECAUSE STATUS AND `len(Entries)` CAN BOTH BE CORRECT WHILE THE
	// PROSE IS FALSE — that is the shape #141 shipped twice on the `ref-to` side, in both
	// implementations at once, so no assertion comparing them could see it. `alpha-notes` holds
	// two readable entries and ONE of them carries this tag, so a reader is entitled to learn
	// that `unrelated` is not it, rather than to be told nothing is.
	text := miss.RenderText("synthetic-host", nil, "")
	wantHeader := "  tag: `marketing` — 1 of 2 entries in `alpha-notes/` carry it, and NOTHING " +
		"below is about them — the sentence below says why."
	if !strings.Contains(text, wantHeader) {
		t.Errorf("the tag header does not report the entries that DO carry the tag."+
			"\nwant: %s\ngot:\n%s", wantHeader, text)
	}
	wantBody := "`unrelated` IS NOT TAGGED `marketing` — it was read and does not carry it, so " +
		"the two narrowings compose to nothing and no body is printed. 1 of the 2 entries in " +
		"`alpha-notes/` DOES — re-run without `--ref` to see it."
	if !strings.Contains(text, wantBody) {
		t.Errorf("the tag-absent sentence claims the scope carries nothing."+
			"\nwant: %s\ngot:\n%s", wantBody, text)
	}
	// The CONTROL on the wording swap: the other route to this status — nothing in the scope
	// carried the tag at all — must still get the original sentence, or the branch above has
	// simply replaced it everywhere.
	none := rep("runbook", "no-such-category")
	if !strings.Contains(none.RenderText("synthetic-host", nil, ""),
		"NO ENTRY IS TAGGED `no-such-category` — the 2 entries in `alpha-notes/` were read") {
		t.Errorf("the filter's own zero lost its sentence:\n%s",
			none.RenderText("synthetic-host", nil, ""))
	}
}

// TestEachFiltersNonFindingIsAttributedToThatFilter is the THREE-WAY composition guard, and it
// exists because a mutation battery found nothing here.
//
// 🔴 THE DEFECT: `--ref X --ref-to Y --tag Z` WHERE X REFERENCES Y BUT IS NOT TAGGED Z. After a
// second filter, `entries` is the INTERSECTION of both narrowings, so a `ref-to` membership test
// reading it answers "`X` DOES NOT REFERENCE `Y`" — false about the store, in the same shape
// #141's round 1 shipped and three audit rounds took to find. Each filter's own non-finding has
// to be tested against that filter's own result, which is what `refToNarrowed` is for.
//
// ⚠ MEASURED: with `containsEntry(refToNarrowed, *entry)` reverted to `containsEntry(entries,
// *entry)` — the one-identifier mutation — this test fails on the status AND on the rendered
// sentence, and NOTHING else in `internal/report`, `internal/store`, `internal/api`,
// `internal/client` or `internal/ui` fails. It SURVIVED the whole suite before this test existed,
// which is why the guard is here rather than only in the comment beside the variable.
func TestEachFiltersNonFindingIsAttributedToThatFilter(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "alpha-notes")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(name string, lines ...string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(strings.Join(lines, "\n")), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// `ref-only` references the ref and is NOT tagged; `both` is both. So a `--ref ref-only`
	// composed with both filters must be attributed to the TAG, and the scope must still report
	// the entry that does carry both.
	write("both.md", "---", "service: both", "scope: alpha-notes",
		"refs: ["+sharedRef+"]", "tags: ["+sharedTag+"]", "---", "",
		"## What it is", "", "Carries the ref AND the tag.", "")
	write("ref-only.md", "---", "service: ref-only", "scope: alpha-notes",
		"refs: ["+sharedRef+"]", "---", "",
		"## What it is", "", "Carries the ref and no tags.", "")

	rep, err := Recall(root, RecallOptions{
		Scope: "alpha-notes", Mode: DefaultMode, Limit: DefaultEntryLimit, Page: 1,
		Ref: "ref-only", HasRef: true,
		RefTo: sharedRef, HasRefTo: true,
		Tag: sharedTag, HasTag: true,
	}, store.Unrestricted())
	if err != nil {
		t.Fatalf("recall: %v", err)
	}
	if rep.Status != StatusTagAbsent {
		t.Fatalf("`--ref ref-only --ref-to <carried> --tag <not carried>`: status=%q, want %q — "+
			"the entry DOES reference the ref, so attributing this to the reverse lookup is a "+
			"sentence that is false about the store", rep.Status, StatusTagAbsent)
	}
	text := rep.RenderText("synthetic-host", nil, "")
	wantBody := "`ref-only` IS NOT TAGGED `" + sharedTag + "` — it was read and does not carry it"
	if !strings.Contains(text, wantBody) {
		t.Errorf("the non-finding is attributed to the wrong filter.\nwant: %s\ngot:\n%s",
			wantBody, text)
	}
	if strings.Contains(text, "DOES NOT REFERENCE") {
		t.Errorf("the report says the entry does not reference a ref it DOES reference:\n%s", text)
	}
	// The MIRROR CASE, so the attribution is pinned in both directions rather than only the one
	// the mutation broke: an entry that carries the TAG and not the ref must be attributed to the
	// reverse lookup.
	write("tag-only.md", "---", "service: tag-only", "scope: alpha-notes",
		"tags: ["+sharedTag+"]", "---", "",
		"## What it is", "", "Carries the tag and no refs.", "")
	mirror, err := Recall(root, RecallOptions{
		Scope: "alpha-notes", Mode: DefaultMode, Limit: DefaultEntryLimit, Page: 1,
		Ref: "tag-only", HasRef: true,
		RefTo: sharedRef, HasRefTo: true,
		Tag: sharedTag, HasTag: true,
	}, store.Unrestricted())
	if err != nil {
		t.Fatalf("recall: %v", err)
	}
	if mirror.Status != StatusRefToAbsent {
		t.Fatalf("`--ref tag-only`: status=%q, want %q", mirror.Status, StatusRefToAbsent)
	}
	mirrorText := mirror.RenderText("synthetic-host", nil, "")
	if !strings.Contains(mirrorText, "`tag-only` DOES NOT REFERENCE `"+sharedRef+"`") {
		t.Errorf("the mirror case is not attributed to the reverse lookup:\n%s", mirrorText)
	}
	if strings.Contains(mirrorText, "IS NOT TAGGED") {
		t.Errorf("the report says an entry is not tagged something it IS tagged:\n%s", mirrorText)
	}
}

// TestAMalformedTagIsRefusedByTheOptionLadder pins that a tag operand that folds away is REFUSED
// rather than narrowing to nothing — and it exists because a mutation battery found nothing here.
//
// 🔴 A REFUSAL AND NOT A NARROWING, WHICH IS THE OPPOSITE CHOICE FROM `?ref=`. A ref operand that
// folds away names an ENTRY nothing is called, and "no entry is called that" is a fact about the
// store worth reporting. A tag operand that folds away names NO CATEGORY, so answering "no entry
// carries this" is an empty result whose cause is invisible — indistinguishable from a tag nobody
// has used, when the real cause is that the query said nothing.
//
// ⚠ MEASURED: with `validateTag`' condition short-circuited to `false` — the operand refused by
// nothing — this test fails and NOTHING ELSE in five packages does. It SURVIVED before this test
// existed.
//
// ⚠ AND `""` IS THE ROW THE SCALAR SURFACE MADE LOAD-BEARING RATHER THAN INCIDENTAL. `?tag=`
// carrying nothing is a PRESENT operand that names no category, and the only thing separating it
// from an absent parameter is `HasTag`; a `Tag string` alone would make this row unreachable and
// answer 200 over the whole scope for it.
func TestAMalformedTagIsRefusedByTheOptionLadder(t *testing.T) {
	for _, bad := range []string{"", "!!!", "   ", "***", " @ "} {
		recallErr := ValidateRecall(RecallOptions{
			Scope: "alpha-notes", Mode: DefaultMode, Limit: DefaultEntryLimit, Page: 1,
			Tag: bad, HasTag: true,
		})
		if recallErr == nil {
			t.Errorf("ValidateRecall accepted tag %q", bad)
			continue
		}
		want := "tag " + store.PyRepr(bad) + " normalizes to the empty string — a tag must fold " +
			"to at least one of `[a-z0-9.-]`"
		if recallErr.Error() != want {
			t.Errorf("tag %q: refusal is %q, want %q", bad, recallErr, want)
		}
		if searchErr := ValidateSearch(SearchOptions{
			Scope: "alpha-notes", Query: "x", Context: ContextBullet,
			Threshold: DefaultThreshold, MaxHits: DefaultMaxHits,
			Tag: bad, HasTag: true,
		}); searchErr == nil || searchErr.Error() != want {
			t.Errorf("tag %q on search: refusal is %v, want %q", bad, searchErr, want)
		}
	}
	// ⚠ THE LADDER'S ORDER IS UNCHANGED, AND THE TAG GUARD IS LAST. A request with a bad LIMIT
	// and a bad tag still gets the limit's message, because that order is what the goldens
	// record; and a request with a bad REF-TO and a bad tag gets the ref-to's, because this guard
	// was appended after it.
	if err := ValidateRecall(RecallOptions{
		Scope: "alpha-notes", Mode: DefaultMode, Limit: 0, Page: 1, Tag: "!!!", HasTag: true,
	}); err == nil || !strings.HasPrefix(err.Error(), "limit must be") {
		t.Errorf("the guard order moved: got %v, want the limit refusal first", err)
	}
	if err := ValidateRecall(RecallOptions{
		Scope: "alpha-notes", Mode: DefaultMode, Limit: DefaultEntryLimit, Page: 1,
		RefTo: "no-colon-here", HasRefTo: true, Tag: "!!!", HasTag: true,
	}); err == nil || !strings.HasPrefix(err.Error(), "ref-to is not a well-formed") {
		t.Errorf("the tag guard jumped ahead of the ref-to guard: got %v", err)
	}
	// A well-formed operand passes, so the refusals above are not "everything is refused" — and so
	// does an ABSENT one, which is the state `HasTag` separates from the `""` row above.
	for _, good := range []string{"Marketing", "project_xyz", "a.b-c"} {
		if err := ValidateRecall(RecallOptions{
			Scope: "alpha-notes", Mode: DefaultMode, Limit: DefaultEntryLimit, Page: 1,
			Tag: good, HasTag: true,
		}); err != nil {
			t.Errorf("a well-formed tag %q was refused: %v", good, err)
		}
	}
	if err := ValidateRecall(RecallOptions{
		Scope: "alpha-notes", Mode: DefaultMode, Limit: DefaultEntryLimit, Page: 1,
		Tag: "", HasTag: false,
	}); err != nil {
		t.Errorf("no `--tag` at all was refused as a malformed one: %v", err)
	}
	// 🔴 AND THE RENDERER REFUSES IT TOO, RATHER THAN NARROWING TO EVERYTHING. `canonicalTag`
	// re-validates inside `Recall`/`Search` for exactly this reason: a folded-away operand leaves
	// the empty string, which the filter reads as "no filter was sent" — so a caller that skipped
	// the ladder would turn a malformed filter into NO filter, which is the widening direction.
	// Driven directly, past the ladder.
	root := twoPrincipalTagWorld(t)
	if _, err := Recall(root, RecallOptions{
		Scope: "alpha-notes", Mode: DefaultMode, Limit: DefaultEntryLimit, Page: 1,
		Tag: "!!!", HasTag: true,
	}, store.Unrestricted()); err == nil {
		t.Error("Recall accepted a folding-away tag from a caller that skipped ValidateRecall, " +
			"which narrows to EVERYTHING rather than to nothing")
	}
	if _, err := Search(root, SearchOptions{
		Scope: "alpha-notes", Query: "readiness", Context: ContextBullet,
		Threshold: DefaultThreshold, MaxHits: DefaultMaxHits, Tag: "!!!", HasTag: true,
	}, store.Unrestricted()); err == nil {
		t.Error("Search accepted a folding-away tag from a caller that skipped ValidateSearch")
	}
}

// TestTheRefToLineStaysTrueWhenATagFilterNarrowsAfterIt is the CROSS-FILTER guard, and it is the
// highest-value thing in this file after criterion 5.
//
// 🔴 THE DEFECT IT PINS IS THE ONE THIS BRANCH WOULD HAVE SHIPPED. `refToLine`'s two counts are
// "entries that reference this" over "entries that were readable". Both were derived from
// `EntriesSearched` and `RefToSkipped` on the search side, which was correct while those two
// partitioned the readable set — and the tag filter, placed downstream of the same counter,
// falsified BOTH: the numerator became the entries that reference the ref AND carry the tag, and
// the denominator lost every entry the tag filter removed. That is the same class of defect as
// the `search-unreadable` term one file over, in PROSE rather than in a status, and it would have
// been invisible to `tests/parity/` and `tests/conformance/` because both implementations would
// have derived it identically.
//
// 🔴 AND THE REACH CLAUSE IS THE THIRD HALF. "Everything below is about those N" is false when a
// further filter removes some of the N, so `reachClause` grew a third state rather than being
// left to read as a claim about a set the body does not show.
//
// ⚠ RED/GREEN: with the search-side call restored to
// `refToLine(r.RefTo, r.EntriesSearched, r.EntriesSearched+r.RefToSkipped, r.Label(), true, false)`
// — the pre-fix arguments — the `search` subtest fails on the whole line, quoting `1 of 1` where
// the store holds 3 readable entries of which 2 reference the ref. The `recall` subtest fails on
// the reach clause alone with `narrowedFurther` forced false.
func TestTheRefToLineStaysTrueWhenATagFilterNarrowsAfterIt(t *testing.T) {
	root := t.TempDir()
	write := func(name string, lines ...string) {
		t.Helper()
		dir := filepath.Join(root, "alpha-notes")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(strings.Join(lines, "\n")), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// THREE readable entries: two carry the ref (one of them also the tag), one carries neither.
	// That is the minimum shape in which the ref-to numerator, the ref-to denominator and the
	// tag numerator are three DIFFERENT numbers — a fixture where any two coincide cannot see a
	// count read off the wrong set.
	write("both.md", "---", "service: both", "scope: alpha-notes",
		"refs: ["+sharedRef+"]", "tags: [marketing]", "---", "",
		"## What it is", "", "Carries the ref AND the tag.", "",
		store.NuanceHeading, "", "- 2000-06-01 a bullet mentioning readiness", "")
	write("ref-only.md", "---", "service: ref-only", "scope: alpha-notes",
		"refs: ["+sharedRef+"]", "---", "",
		"## What it is", "", "Carries the ref and no tags.", "",
		store.NuanceHeading, "", "- 2000-06-01 a bullet mentioning readiness", "")
	write("neither.md", "---", "service: neither", "scope: alpha-notes", "---", "",
		"## What it is", "", "Carries neither.", "",
		store.NuanceHeading, "", "- 2000-06-01 a bullet mentioning readiness", "")

	// The three numbers, spelled out so a failure says which one moved:
	//   readable                        = 3
	//   reference the ref               = 2   (the ref-to numerator)
	//   reference it AND carry the tag  = 1   (what the body renders)
	const wantRefTo = "  ref-to: `" + sharedRef + "` — 2 of 3 entries in `alpha-notes/` " +
		"reference it, and a FURTHER filter below narrows those 2 again. This is a NARROWING, " +
		"not a truncation: the rest were read and did not match."
	// …and the tag line's denominator is the set the TAG filter saw — the ref-to-narrowed 2,
	// never the readable 3, because the tag filter never looked at `neither.md`.
	const wantTag = "  tag: `marketing` — 1 of 2 entries in `alpha-notes/` carry it, and " +
		"everything below is about those 1."

	t.Run("recall", func(t *testing.T) {
		rep, err := Recall(root, RecallOptions{
			Scope: "alpha-notes", Mode: DefaultMode, Limit: DefaultEntryLimit, Page: 1,
			RefTo: sharedRef, HasRefTo: true, Tag: sharedTag, HasTag: true,
		}, store.Unrestricted())
		if err != nil {
			t.Fatalf("recall: %v", err)
		}
		if rep.RefToScopeTotal != 3 || rep.RefToMatched != 2 {
			t.Fatalf("ref-to counts: %d of %d, want 2 of 3", rep.RefToMatched, rep.RefToScopeTotal)
		}
		if rep.TagScopeTotal != 2 || rep.TagMatched != 1 {
			t.Fatalf("tag counts: %d of %d, want 1 of 2 — the tag filter's denominator is the "+
				"set it SAW, which is the ref-to-narrowed one", rep.TagMatched, rep.TagScopeTotal)
		}
		text := rep.RenderText("synthetic-host", nil, "")
		for _, want := range []string{wantRefTo, wantTag} {
			if !strings.Contains(text, want) {
				t.Errorf("missing line.\nwant: %s\ngot:\n%s", want, text)
			}
		}
		// …and the body really is about ONE entry, which is what makes the reach clause's
		// "narrows those 2 again" a true statement rather than a decorative one.
		if len(rep.Entries) != 1 || rep.Entries[0].Ref != "both" {
			t.Errorf("the body reports on %d entries (%+v), want exactly `both` — the reach "+
				"clause is a claim about this set", len(rep.Entries), rep.Entries)
		}
	})

	t.Run("search", func(t *testing.T) {
		rep, err := Search(root, SearchOptions{
			Scope: "alpha-notes", Query: "readiness", Context: ContextBullet,
			Threshold: DefaultThreshold, MaxHits: DefaultMaxHits,
			RefTo: sharedRef, HasRefTo: true, Tag: sharedTag, HasTag: true,
		}, store.Unrestricted())
		if err != nil {
			t.Fatalf("search: %v", err)
		}
		if rep.EntriesSearched != 1 || rep.RefToSkipped != 1 || rep.TagSkipped != 1 {
			t.Fatalf("searched=%d refToSkipped=%d tagSkipped=%d, want 1/1/1 — the three counters "+
				"must PARTITION the three readable entries", rep.EntriesSearched,
				rep.RefToSkipped, rep.TagSkipped)
		}
		text := rep.RenderText("synthetic-host", nil, "")
		for _, want := range []string{wantRefTo, wantTag} {
			if !strings.Contains(text, want) {
				t.Errorf("missing line.\nwant: %s\ngot:\n%s", want, text)
			}
		}
	})

	// ⚠ THE CONTROL ON THE REACH CLAUSE: a `--tag` that removes NOTHING must leave the original
	// clause, or the third state has simply replaced the first everywhere. `both.md` and
	// `ref-only.md` both reference the ref; tagging by nothing they lack keeps both.
	t.Run("a tag filter that removes nothing keeps the original clause", func(t *testing.T) {
		rep, err := Recall(root, RecallOptions{
			Scope: "alpha-notes", Mode: "full", Limit: DefaultEntryLimit, Page: 1,
			RefTo: sharedRef, HasRefTo: true, Tag: "marketing", HasTag: true,
		}, store.Unrestricted())
		if err != nil {
			t.Fatal(err)
		}
		// This run DOES remove one, so it is the negative side of the control; the positive side
		// is below, over a tag both ref-carrying entries hold.
		if !strings.Contains(rep.RenderText("synthetic-host", nil, ""), "narrows those 2 again") {
			t.Error("the removing case lost the third clause")
		}
		write("ref-only.md", "---", "service: ref-only", "scope: alpha-notes",
			"refs: ["+sharedRef+"]", "tags: [marketing]", "---", "",
			"## What it is", "", "Now carries the tag too.", "",
			store.NuanceHeading, "", "- 2000-06-01 a bullet mentioning readiness", "")
		kept, err := Recall(root, RecallOptions{
			Scope: "alpha-notes", Mode: "full", Limit: DefaultEntryLimit, Page: 1,
			RefTo: sharedRef, HasRefTo: true, Tag: "marketing", HasTag: true,
		}, store.Unrestricted())
		if err != nil {
			t.Fatal(err)
		}
		got := kept.RenderText("synthetic-host", nil, "")
		if strings.Contains(got, "narrows those 2 again") {
			t.Errorf("a `--tag` that removed nothing still claimed a further narrowing:\n%s", got)
		}
		if !strings.Contains(got, "reference it, and everything below is about those 2.") {
			t.Errorf("the original reach clause is gone from the non-narrowing case:\n%s", got)
		}
	})
}
