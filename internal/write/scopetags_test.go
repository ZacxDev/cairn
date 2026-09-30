package write

import (
	"slices"
	"sort"
	"strings"
	"testing"
)

// TestEveryMappedTagIsInTheVocabulary is the SEAM guard between the two halves of this
// package's tag rule, and it is the one the narrowing would have caught.
//
// 🔴 THE RELATIONSHIP, NOT EITHER SIDE. `tagVocabulary` says which tags exist;
// `scopeTagTable` says which one a scope gets. Each is individually well-formed under
// any edit to the other, and the defect lives between them: `client-work` was a declared
// term for one round and was then dropped, and a table still mapping a scope to it would
// classify every entry in that scope into a category the pod REFUSES — the rule and the
// vocabulary disagreeing about what is writable, with both files reading fine on their
// own. This test is what makes that a red rather than a support ticket.
//
// ⚠ RED/GREEN MATRIX: red when `"product"` in `scopeTagTable` is changed to
// `"client-work"` (mutant S1 — the historical shape exactly), naming the scope, the tag
// and the vocabulary; green at HEAD. Measured, reported in the pull request.
func TestEveryMappedTagIsInTheVocabulary(t *testing.T) {
	if len(scopeTagTable) == 0 {
		t.Fatal("the scope->tag table is EMPTY, so every assertion below is vacuous")
	}
	if err := ValidateScopeTagTable(scopeTagTable); err != nil {
		t.Fatalf("the committed table does not satisfy its own validator: %v", err)
	}
	// ⚠ AND EVERY TERM IS REACHED BY AT LEAST ONE SCOPE, which is a DIFFERENT claim from
	// the one above and the reason both are here. "No value is outside the vocabulary" is
	// satisfied by a table that maps everything to one term; that table would make every
	// per-term assertion in this file vacuous and would hide a term nobody can obtain.
	mapped := map[string]bool{}
	for _, tag := range scopeTagTable {
		mapped[tag] = true
	}
	var unreached []string
	for _, tag := range tagVocabulary {
		if !mapped[tag] {
			unreached = append(unreached, tag)
		}
	}
	sort.Strings(unreached)
	if len(unreached) > 0 {
		t.Fatalf("%v is/are declared in the vocabulary and mapped to by NO scope. Either "+
			"the term is unused — in which case it is a write nobody is supposed to make, "+
			"and dropping it is the honest edit — or a table row is missing", unreached)
	}
}

// TestTheValidatorRefusesEachWayATableCanBeUseless is the NEGATIVE CONTROL on
// `ValidateScopeTagTable`, and without it the green above is a fact about a function that
// may return nil unconditionally.
//
// 🔴 EACH CASE PASSES EVERY OTHER CHECK, which is the reachability half. A table whose
// only fault is an undeclared tag has folded keys; a table whose only fault is an
// unfolded key maps to a real term. So each refusal is attributable to the invariant it
// names rather than to whichever check happens to run first.
func TestTheValidatorRefusesEachWayATableCanBeUseless(t *testing.T) {
	// POSITIVE CONTROL FIRST: the validator must accept something, or every refusal
	// below is indistinguishable from a function wired to "no".
	good := map[string]string{"alpha": "product", "beta-infra": "infra"}
	if err := ValidateScopeTagTable(good); err != nil {
		t.Fatalf("a well-formed table was refused, so this test cannot tell a working "+
			"validator from a broken one: %v", err)
	}

	t.Run("a tag outside the vocabulary", func(t *testing.T) {
		err := ValidateScopeTagTable(map[string]string{"alpha": "client-work"})
		if err == nil {
			t.Fatal("a table mapping a scope to an undeclared tag was accepted — those " +
				"entries would be classified into a category every write REFUSES")
		}
		// THIS invariant's own sentence, not merely "an error came back".
		if !strings.Contains(err.Error(), "which is not one of infra|product|tooling") {
			t.Fatalf("the refusal does not name the vocabulary: %v", err)
		}
		if !strings.Contains(err.Error(), "'alpha'") {
			t.Fatalf("the refusal does not name the offending scope: %v", err)
		}
	})

	t.Run("a key that is not its own folded form", func(t *testing.T) {
		// `Alpha_One` folds to `alpha-one`, so the row can never be matched. Its VALUE is
		// a declared term, so the other invariant cannot be what fires.
		err := ValidateScopeTagTable(map[string]string{"Alpha_One": "product"})
		if err == nil {
			t.Fatal("a table key that folds to something else was accepted — the row is " +
				"dead weight that reads like a classification")
		}
		if !strings.Contains(err.Error(), "folds to 'alpha-one'") {
			t.Fatalf("the refusal does not name the folded form: %v", err)
		}
	})

	t.Run("every problem is reported, not just the first", func(t *testing.T) {
		err := ValidateScopeTagTable(map[string]string{
			"alpha":    "client-work",
			"Beta_Two": "product",
			"gadget":   "nonsense",
		})
		if err == nil {
			t.Fatal("a table with three faults was accepted")
		}
		// Three problems: two bad tags and one unfoldable key.
		if got := strings.Count(err.Error(), ";"); got != 2 {
			t.Fatalf("expected 3 problems joined by 2 separators, got %d:\n%v", got, err)
		}
	})
}

// TestScopeTagIsAnExactMatchAndNeverAPrefix is the rule itself, and the prefix case is
// the one a later "simplification" gets wrong.
//
// 🔴 THE COUNTEREXAMPLE IS IN THE COMMITTED TABLE, so this is a measurement of data
// rather than a restatement of a comment. `alpha` is `product` and `alpha-fleet` is
// `infra`: a prefix match asked about `alpha-fleet` finds the key `alpha` and answers
// `product`, which is the wrong category for the scope that most needs the right one.
//
// ⚠ RED/GREEN MATRIX: red when `ScopeTag`'s map lookup is replaced by a loop taking the
// first key that is a prefix of the operand (mutant S2), with this test naming
// `alpha-fleet` and the tag it got; green at HEAD.
func TestScopeTagIsAnExactMatchAndNeverAPrefix(t *testing.T) {
	// The exact-match answers, including the pair a prefix rule inverts.
	for scope, want := range map[string]string{
		"alpha":       "product",
		"alpha-app":   "product",
		"alpha-fleet": "infra",
		"beta":        "product",
		"beta-infra":  "infra",
	} {
		got, ok := ScopeTag(scope)
		if !ok {
			t.Fatalf("%q is in the table and ScopeTag says it is unknown", scope)
		}
		if got != want {
			t.Fatalf("ScopeTag(%q)=%q, want %q — a prefix match answers the SHORTER key's "+
				"tag here, which is exactly the mistake the rule forbids", scope, got, want)
		}
	}

	// 🔴 AN UNKNOWN SCOPE IS `false`, NEVER A NEIGHBOUR'S ANSWER. `alpha-unlisted` shares a
	// prefix with three mapped scopes and is itself mapped by none; a prefix or
	// longest-prefix rule invents `product` for it, which is worse than "unclassified"
	// because nothing then prompts anybody to classify it.
	if tag, ok := ScopeTag("alpha-unlisted"); ok {
		t.Fatalf("an unmapped scope sharing a prefix with three mapped ones answered %q — "+
			"the lookup is falling back to a prefix", tag)
	}
	// …and a scope sharing no prefix with anything, so the case above cannot pass merely
	// because the lookup is broken for everything.
	if tag, ok := ScopeTag("ghost-void"); ok {
		t.Fatalf("an entirely unmapped scope answered %q", tag)
	}

	// The operand is FOLDED, so a directory name as written reaches its row.
	if tag, ok := ScopeTag(" Alpha_Fleet "); !ok || tag != "infra" {
		t.Fatalf("ScopeTag(%q)=(%q,%v), want (infra,true) — the operand must fold the same "+
			"way the scope directory did", " Alpha_Fleet ", tag, ok)
	}
}

// TestTheTableSTILLCONTAINSAPrefixDisagreement guards the CLAIM in `scopetags.go`'s
// header rather than the code under it.
//
// ⚠ IT IS A CLAIM GUARD, NOT REGRESSION COVERAGE, AND THE DISTINCTION MATTERS HERE. No
// defect ever removed the counterexample. What this refuses is a table edit that leaves
// the header asserting "a prefix match gets it backwards" with nothing in the repository
// demonstrating it — at which point the most load-bearing sentence in that file is
// unfalsifiable prose, and the next reader simplifies `ScopeTag` into a prefix loop
// because every committed row agrees.
func TestTheTableSTILLCONTAINSAPrefixDisagreement(t *testing.T) {
	pairs := prefixDisagreements(scopeTagTable)
	if len(pairs) == 0 {
		t.Fatal("no (short, long) prefix pair in the committed table disagrees about its " +
			"tag. `scopetags.go`'s header claims a prefix match gets real tables backwards " +
			"and the table no longer shows it, so that claim is now unfalsifiable prose. " +
			"Either restore a disagreeing pair or re-derive the header.")
	}
	if !slices.Contains(pairs, [2]string{"alpha", "alpha-fleet"}) {
		t.Fatalf("the named counterexample (alpha -> product, alpha-fleet -> infra) is "+
			"gone; disagreeing pairs are %v. The header names that pair, so it has to be "+
			"the one that exists.", pairs)
	}
	// The NEGATIVE CONTROL on the detector itself: a table whose prefix pairs all AGREE
	// must produce nothing, or the assertion above would pass over any table at all.
	agreeing := map[string]string{"alpha": "product", "alpha-app": "product"}
	if got := prefixDisagreements(agreeing); len(got) != 0 {
		t.Fatalf("the detector reported %v for a table whose prefix pair AGREES, so a "+
			"non-empty result above proves nothing", got)
	}
	// …and it must find the pair in a minimal table that HAS one, so an empty result is
	// not simply what it always returns.
	disagreeing := map[string]string{"alpha": "product", "alpha-fleet": "infra"}
	if got := prefixDisagreements(disagreeing); len(got) != 1 {
		t.Fatalf("the detector found %v in a table with exactly one disagreeing prefix "+
			"pair", got)
	}
}
