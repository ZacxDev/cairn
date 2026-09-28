package report

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ZacxDev/cairn/internal/store"
)

// twoPrincipalRefWorld is a store with ONE ref written into TWO scopes, so a principal who can
// read only one of them is a real discriminator rather than a fixture detail.
//
// ⚠ THE REF IS ON AN ENTRY IN EACH SCOPE, AND THE TEXT DIFFERS. A fixture where only the
// unreachable scope carried the ref could not tell "the filter narrowed after authorisation"
// from "the filter is broken and finds nothing"; a fixture where only the reachable one carried
// it could not see a leak at all. Both scopes carry it, and the assertions below read WHICH.
func twoPrincipalRefWorld(t *testing.T) string {
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
		"refs: [github:example-org/example-repo#428]", "---", "",
		"## What it is", "", "The alpha rollout runbook.", "",
		store.NuanceHeading, "", "- 2000-06-01 a bullet mentioning readiness", "")
	// The SAME ref, in a scope the first principal cannot read.
	write("beta-notes", "ledger.md",
		"---", "service: ledger", "scope: beta-notes",
		"refs: [github:example-org/example-repo#428]", "---", "",
		"## What it is", "", "The beta ledger.", "",
		store.NuanceHeading, "", "- 2000-06-01 a bullet mentioning readiness", "")
	// An entry in the READABLE scope that does NOT carry the ref, so the filter has
	// something to remove and its count is not trivially the scope total.
	write("alpha-notes", "unrelated.md",
		"---", "service: unrelated", "scope: alpha-notes", "---", "",
		"## What it is", "", "Carries no refs at all.", "")
	return root
}

const sharedRef = "github:example-org/example-repo#428"

// TestTheRefToFilterNarrowsAfterScopeAuthorisation is criterion 7 — the one that matters.
//
// 🔴 WATCHED RED WITH THE NARROW APPLIED IN THE WRONG ORDER. The mutation is the obvious
// implementation of a store-wide question: `store.LoadStore(storeRoot, verb, store.Unrestricted())`
// in place of the caller's `visible`, then the same ref filter. With it, the `alpha-notes`-only
// principal's recall answered `recalled` with `beta-notes/ledger.md`'s row in the index and
// `beta-notes` on `KnownScopes` — an entry, a scope name and a ref from a directory that
// principal cannot read. The filter is correct in both orderings; only the AUTHORISATION
// differs, which is precisely why a single-principal test cannot see it.
//
// ⚠ IT IS A PAIR, NOT A ZERO. The `beta-notes` principal must FIND the ref, or the first
// principal's empty answer is indistinguishable from a filter wired to nothing.
func TestTheRefToFilterNarrowsAfterScopeAuthorisation(t *testing.T) {
	root := twoPrincipalRefWorld(t)
	onlyAlpha := store.VisibleScopeSet([]string{"alpha-notes"})
	onlyBeta := store.VisibleScopeSet([]string{"beta-notes"})

	t.Run("recall", func(t *testing.T) {
		opts := func(scope string) RecallOptions {
			return RecallOptions{
				Scope: scope, Mode: DefaultMode, Limit: DefaultEntryLimit, Page: 1,
				RefTo: sharedRef, HasRefTo: true,
			}
		}
		// POSITIVE CONTROL: the principal who CAN read `beta-notes` finds the ref there.
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
		// 🔴 AND NO BETA ENTRY, SCOPE NAME OR REF CAME BACK ON ANY FIELD. This is the
		// assertion the wrong ordering fails: it answered `recalled` here.
		assertNoBetaLeak(t, asAlpha.KnownScopes, asAlpha.Entries, asAlpha.Listing, asAlpha.Malformed, asAlpha.MalformedElsewhere)

		// …and asking about their OWN scope still narrows correctly: 1 of 2 entries.
		own, err := Recall(root, opts("alpha-notes"), onlyAlpha)
		if err != nil {
			t.Fatalf("recall as alpha on alpha: %v", err)
		}
		if own.Status != StatusRecalled {
			t.Fatalf("own scope: status=%q, want recalled", own.Status)
		}
		if own.TotalInScope != 1 || own.RefToScopeTotal != 2 {
			t.Fatalf("own scope: matched=%d of %d, want 1 of 2 — the filter must remove the "+
				"entry that carries no refs and report what it removed",
				own.TotalInScope, own.RefToScopeTotal)
		}
		assertNoBetaLeak(t, own.KnownScopes, own.Entries, own.Listing, own.Malformed, own.MalformedElsewhere)
	})

	t.Run("search all scopes", func(t *testing.T) {
		// 🔴 `AllScopes` IS THE HARDER HALF AND IT IS WHY THIS SUBTEST EXISTS. `?all_scopes=1`
		// NAMES NO SCOPE, so a per-scope refusal check has nothing to refuse — `Search`'s own
		// header says the index filter is what makes it safe. A ref-to filter over a
		// store-wide load would re-open exactly that hole, and the scope name cannot be the
		// thing that stops it.
		opts := SearchOptions{
			Query: "readiness", Context: ContextBullet, Threshold: DefaultThreshold,
			MaxHits: DefaultMaxHits, AllScopes: true,
			RefTo: sharedRef, HasRefTo: true,
		}
		asBeta, err := Search(root, opts, onlyBeta)
		if err != nil {
			t.Fatalf("search as beta: %v", err)
		}
		if asBeta.EntriesSearched != 1 || len(asBeta.Hunks) == 0 {
			t.Fatalf("POSITIVE CONTROL FAILED: beta's store-wide search over the ref searched "+
				"%d entries and found %d hunks, want 1 and >0", asBeta.EntriesSearched, len(asBeta.Hunks))
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
				"set means the ref filter ran over a scope alpha cannot see, whether or not it "+
				"reported a hit from it", got)
		}
		if asAlpha.EntriesSearched != 1 || asAlpha.RefToSkipped != 1 {
			t.Fatalf("alpha searched %d and skipped %d, want 1 and 1 — the filter must keep the "+
				"one alpha entry carrying the ref and remove the one that does not",
				asAlpha.EntriesSearched, asAlpha.RefToSkipped)
		}
		for _, h := range asAlpha.Hunks {
			if h.Scope == "beta-notes" {
				t.Fatalf("a hunk from `beta-notes` reached a principal who cannot read it: %+v", h)
			}
		}
		assertNoBetaLeak(t, asAlpha.KnownScopes, nil, nil, asAlpha.Malformed, asAlpha.MalformedElsewhere)
	})
}

// assertNoBetaLeak is the ONE leak predicate, over every field of either report that could
// carry a scope name, an entry or a ref.
//
// 🔴 A NAMED FUNCTION SO EVERY CALL SITE CHECKS THE SAME SET. Spelled inline per assertion, the
// call that mattered would check the field somebody remembered — and the wrong ordering leaked
// through `KnownScopes` and `Listing` before it leaked through `Entries`.
func assertNoBetaLeak(t *testing.T, knownScopes []string, entries, listing []RecalledEntry, bad, badElsewhere []store.MalformedEntry) {
	t.Helper()
	for _, s := range knownScopes {
		if s == "beta-notes" {
			t.Errorf("`beta-notes` appeared on KnownScopes — the enumeration channel, for a "+
				"directory this principal cannot read. Scopes: %v", knownScopes)
		}
	}
	// ⚠ MATCHED ON `Ref`/`Filename`, BECAUSE `RecalledEntry` CARRIES NO SCOPE. That is the
	// type's own shape — a recalled body is addressed by ref within the report's scope — so the
	// leak has to be spotted by the entry's NAME, and the fixture gives the beta entry a name
	// no alpha entry has for exactly that reason.
	for _, set := range [][]RecalledEntry{entries, listing} {
		for _, e := range set {
			if e.Ref == "ledger" || e.Filename == "ledger.md" {
				t.Errorf("an entry from `beta-notes` reached a principal who cannot read it: %+v", e)
			}
		}
	}
	for _, set := range [][]store.MalformedEntry{bad, badElsewhere} {
		for _, m := range set {
			if m.Scope == "beta-notes" {
				t.Errorf("a malformed row from `beta-notes` reached a principal who cannot read it: %+v", m)
			}
		}
	}
}

// TestTheRefToFilterAnswersItsOwnNonFinding pins `ref-to-absent`: a ref no entry carries is an
// ANSWER, not an empty scope and not an absent entry name.
func TestTheRefToFilterAnswersItsOwnNonFinding(t *testing.T) {
	root := twoPrincipalRefWorld(t)
	rep, err := Recall(root, RecallOptions{
		Scope: "alpha-notes", Mode: DefaultMode, Limit: DefaultEntryLimit, Page: 1,
		RefTo: "clickup:no-such-task", HasRefTo: true,
	}, store.Unrestricted())
	if err != nil {
		t.Fatalf("recall: %v", err)
	}
	if rep.Status != StatusRefToAbsent {
		t.Fatalf("status=%q, want %q — `scope-empty` would claim the directory holds nothing "+
			"and `ref-absent` would send the reader looking for a file", rep.Status, StatusRefToAbsent)
	}
	if rep.RefToScopeTotal != 2 {
		t.Fatalf("RefToScopeTotal=%d, want 2 — the answer must say how much WAS read", rep.RefToScopeTotal)
	}
	text := rep.RenderText("synthetic-host", nil, "")
	for _, want := range []string{
		"status=ref-to-absent",
		"NO ENTRY REFERENCES `clickup:no-such-task`",
		"the 2 entries in `alpha-notes/` were read and none of them carries that ref",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("the rendered non-finding does not carry %q.\ngot:\n%s", want, text)
		}
	}
	// 🔴 IT EXITS 0. "Nothing references this" is an answer; only "nothing could be read" is a
	// non-zero, which is `UnreadableStatuses`' whole rule.
	if code, _ := ExitFor(rep.Status, rep.Scope+"/", rep.Malformed); code != 0 {
		t.Errorf("ref-to-absent exits %d, want 0", code)
	}
}

// TestTheRefToNarrowingAnnouncesItself pins the header line on BOTH report types, because the
// counts under it are about the narrowed set.
func TestTheRefToNarrowingAnnouncesItself(t *testing.T) {
	root := twoPrincipalRefWorld(t)

	rec, err := Recall(root, RecallOptions{
		Scope: "alpha-notes", Mode: DefaultMode, Limit: DefaultEntryLimit, Page: 1,
		RefTo: "GitHub:example-org/example-repo#428", HasRefTo: true,
	}, store.Unrestricted())
	if err != nil {
		t.Fatalf("recall: %v", err)
	}
	// The CANONICAL spelling: the query's system half was `GitHub`, the line says `github`.
	wantLine := "  ref-to: `" + sharedRef + "` — 1 of 2 entries in `alpha-notes/` reference it, " +
		"and everything below is about those 1. This is a NARROWING, not a truncation: the " +
		"rest were read and did not match."
	if got := rec.RenderText("synthetic-host", nil, ""); !strings.Contains(got, wantLine) {
		t.Errorf("the recall header lacks the ref-to line.\nwanted: %s\ngot:\n%s", wantLine, got)
	}

	srch, err := Search(root, SearchOptions{
		Scope: "alpha-notes", Query: "readiness", Context: ContextBullet,
		Threshold: DefaultThreshold, MaxHits: DefaultMaxHits,
		RefTo: sharedRef, HasRefTo: true,
	}, store.Unrestricted())
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if got := srch.RenderText("synthetic-host", nil, ""); !strings.Contains(got, wantLine) {
		t.Errorf("the search header lacks the ref-to line.\nwanted: %s\ngot:\n%s", wantLine, got)
	}

	// ⚠ AND IT IS ABSENT WITHOUT THE FILTER, which is what keeps every existing golden still.
	plain, err := Recall(root, RecallOptions{
		Scope: "alpha-notes", Mode: DefaultMode, Limit: DefaultEntryLimit, Page: 1,
	}, store.Unrestricted())
	if err != nil {
		t.Fatalf("recall: %v", err)
	}
	if strings.Contains(plain.RenderText("synthetic-host", nil, ""), "ref-to:") {
		t.Error("a recall with no `--ref-to` printed a ref-to line, which would move every golden")
	}
}

// TestRefToComposesWithRef pins that the two narrowings compose rather than one shadowing the
// other — including the case the resolver cannot see.
func TestRefToComposesWithRef(t *testing.T) {
	root := twoPrincipalRefWorld(t)
	rep := func(ref, refTo string) RecallReport {
		t.Helper()
		out, err := Recall(root, RecallOptions{
			Scope: "alpha-notes", Mode: DefaultMode, Limit: DefaultEntryLimit, Page: 1,
			Ref: ref, HasRef: true, RefTo: refTo, HasRefTo: true,
		}, store.Unrestricted())
		if err != nil {
			t.Fatalf("recall: %v", err)
		}
		return out
	}
	// The entry DOES carry the ref: one body, in full.
	hit := rep("runbook", sharedRef)
	if hit.Status != StatusRecalled || len(hit.Entries) != 1 {
		t.Fatalf("`--ref runbook --ref-to <carried>`: status=%q entries=%d, want recalled/1",
			hit.Status, len(hit.Entries))
	}
	// 🔴 THE ENTRY DOES NOT CARRY IT. `ResolveRefTiered` resolves against the whole index and
	// knows nothing about the ref-to set, so without the membership test this printed
	// `unrelated` in full — the report answering a question nobody asked.
	miss := rep("unrelated", sharedRef)
	if miss.Status != StatusRefToAbsent {
		t.Fatalf("`--ref unrelated --ref-to <not carried by it>`: status=%q, want %q; entries=%d",
			miss.Status, StatusRefToAbsent, len(miss.Entries))
	}
	if len(miss.Entries) != 0 {
		t.Fatalf("a body was printed for an entry that does not carry the ref: %+v", miss.Entries)
	}

	// 🔴 AND THE RENDERED TEXT, BECAUSE STATUS AND `len(Entries)` WERE BOTH CORRECT WHILE THE
	// PROSE WAS FALSE. `alpha-notes` holds two readable entries and ONE of them carries this
	// ref, so a reader is entitled to learn that `unrelated` is not it — instead both clients
	// printed "0 of 2 entries in `alpha-notes/` reference it" and "NO ENTRY REFERENCES …",
	// which is a claim about the STORE and it was wrong. Asserted as whole substrings rather
	// than by keyword: a guard on words is walkable by rewording, and these bytes are compared
	// against the oracle's by `tests/parity/` and `tests/conformance/`.
	//
	// 🔴 RED/GREEN MATRIX, MEASURED RATHER THAN CLAIMED: at `f806d6c1` (the pre-fix head) this
	// block fails on BOTH assertions — `RefToMatched()` short-circuited to 0 on this status,
	// so the header read `0 of 2` and the body took the no-entry branch. Green at HEAD.
	text := miss.RenderText("synthetic-host", nil, "")
	wantHeader := "  ref-to: `" + sharedRef + "` — 1 of 2 entries in `alpha-notes/` reference " +
		"it, and NOTHING below is about them — the sentence below says why. This is a " +
		"NARROWING, not a truncation: the rest were read and did not match."
	if !strings.Contains(text, wantHeader) {
		t.Errorf("the ref-to header does not report the entries that DO carry the ref."+
			"\nwant: %s\ngot:\n%s", wantHeader, text)
	}
	wantBody := "`unrelated` DOES NOT REFERENCE `" + sharedRef + "` — it was read and carries " +
		"no such ref, so the two narrowings compose to nothing and no body is printed. 1 of " +
		"the 2 entries in `alpha-notes/` DOES reference it — re-run without `--ref` to see it."
	if !strings.Contains(text, wantBody) {
		t.Errorf("the ref-to-absent sentence claims the scope references nothing."+
			"\nwant: %s\ngot:\n%s", wantBody, text)
	}
	// The CONTROL on the wording swap: the other route to this status — nothing in the scope
	// matched at all — must still get the original sentence, or the branch above has simply
	// replaced it everywhere.
	none, err := Recall(root, RecallOptions{
		Scope: "alpha-notes", Mode: DefaultMode, Limit: DefaultEntryLimit, Page: 1,
		RefTo: "clickup:nothing-carries-this", HasRefTo: true,
	}, store.Unrestricted())
	if err != nil {
		t.Fatalf("recall: %v", err)
	}
	noneText := none.RenderText("synthetic-host", nil, "")
	if !strings.Contains(noneText, "NO ENTRY REFERENCES `clickup:nothing-carries-this` — the 2 "+
		"entries in `alpha-notes/` were read and none of them carries that ref.") {
		t.Errorf("the filter's own zero lost its sentence:\n%s", noneText)
	}
	if !strings.Contains(noneText, "— 0 of 2 entries in `alpha-notes/` reference it") {
		t.Errorf("the filter's own zero does not report a zero numerator:\n%s", noneText)
	}
}

// TestAMalformedRefToIsRefusedByTheOptionLadder pins that the operand goes through the SAME
// parser an entry's `refs:` item does, with one sentence naming the query rather than a file.
func TestAMalformedRefToIsRefusedByTheOptionLadder(t *testing.T) {
	for _, bad := range []string{"no-colon-here", ":428", "github:", "has space:1", "a,b:1"} {
		recallErr := ValidateRecall(RecallOptions{
			Scope: "alpha-notes", Mode: DefaultMode, Limit: DefaultEntryLimit, Page: 1,
			RefTo: bad, HasRefTo: true,
		})
		if recallErr == nil {
			t.Errorf("ValidateRecall accepted ref-to %q", bad)
			continue
		}
		if !strings.HasPrefix(recallErr.Error(), "ref-to is not a well-formed `<system>:<id>` ref: ") {
			t.Errorf("ref-to %q: refusal does not name the parameter: %v", bad, recallErr)
		}
		searchErr := ValidateSearch(SearchOptions{
			Scope: "alpha-notes", Query: "x", Context: ContextBullet,
			Threshold: DefaultThreshold, MaxHits: DefaultMaxHits,
			RefTo: bad, HasRefTo: true,
		})
		if searchErr == nil {
			t.Errorf("ValidateSearch accepted ref-to %q", bad)
		}
	}
	// ⚠ THE LADDER'S ORDER IS UNCHANGED: a request with a bad LIMIT and a bad ref-to still
	// gets the limit's message, because that order is what the goldens record.
	err := ValidateRecall(RecallOptions{
		Scope: "alpha-notes", Mode: DefaultMode, Limit: 0, Page: 1,
		RefTo: "no-colon-here", HasRefTo: true,
	})
	if err == nil || !strings.HasPrefix(err.Error(), "limit must be") {
		t.Errorf("the guard order moved: got %v, want the limit refusal first", err)
	}
	// A well-formed operand passes, so the refusals above are not "everything is refused".
	if err := ValidateRecall(RecallOptions{
		Scope: "alpha-notes", Mode: DefaultMode, Limit: DefaultEntryLimit, Page: 1,
		RefTo: sharedRef, HasRefTo: true,
	}); err != nil {
		t.Errorf("a well-formed ref-to was refused: %v", err)
	}
}
