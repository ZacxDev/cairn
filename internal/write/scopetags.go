package write

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/ZacxDev/cairn/internal/store"
)

// This file holds the rule that APPLIES `tagVocabulary` to a store: which of the
// declared tags a given SCOPE's entries carry.
//
// 🔴 WHY IT IS HERE AND NOT IN SOMEBODY'S HOME DIRECTORY. The vocabulary says which
// tags exist; this rule decides which one an entry GETS, so it governs every tagged
// entry in the store — and it lived only in an uncommitted script, where nothing could
// test it, nothing could review it, and the one person holding it was the only person
// who could answer "why is this entry tagged that". Beside `tagVocabulary` is the
// correct home precisely because the two constrain each other: a value here that is not
// a term there is a rule nobody can apply, and that relationship is what
// `TestEveryMappedTagIsInTheVocabulary` pins. It is the relationship, not either side,
// that was unowned.
//
// 🔴 AND THE RULE IS **EXACT MATCH ON THE WHOLE SCOPE SLUG, NEVER A PREFIX** — THIS IS
// THE LOAD-BEARING PART AND IT IS COUNTER-INTUITIVE. Real tables put a short scope and a
// longer scope that starts with it in DIFFERENT categories: an organisation's own
// product scope sits under `product` while that same organisation's fleet and
// orchestration scopes sit under `infra`. A prefix match gets those backwards — it reads
// the longer name, finds the shorter key, and answers the shorter key's tag — so it is
// wrong in exactly the cases a short generic key exists to cover. Longest-prefix would
// be wrong differently: it silently invents a tag for a scope nobody has classified,
// which is worse than answering "unclassified" because nothing then prompts the
// decision. `ScopeTag` therefore answers `(_, false)` for an unknown scope and the
// caller decides.
//
// ⚠ AND THE COMMITTED TABLE BELOW IS **SYNTHETIC**, WHICH IS NOT A COMPROMISE ON THE
// RULE — IT IS THIS REPOSITORY'S FIRST RULE. `AGENTS.md`: never commit "a real project,
// client, customer, repository or scope name — fixtures must be synthetic", and
// `tests/leakscan.py` ENFORCES it in CI over a closed digest set. Measured, not argued:
// the operator's real 28-scope table was written to a file in this tree and
// `python3 tests/leakscan.py` answered `9 finding(s) across 477 file(s) — REFUSING`,
// naming seven denied identifiers and saying of each *"do not add it to
// DENIED_IDENTIFIER_DIGESTS' exceptions, because there are none"*. So what is committed
// here is the RULE, the INVARIANTS and the VALIDATOR — everything a second reader needs
// in order to check a table — and the deployment's own table is data this repository may
// not hold. `ValidateScopeTagTable` is exported for exactly that: whatever private tree
// carries the real rows can run this package's own check over them rather than a second
// spelling of it.

// scopeTagTable maps a normalized scope slug to the one vocabulary term its entries
// carry.
//
// ⚠ SYNTHETIC ROWS, CHOSEN TO EXHIBIT EVERY STRUCTURAL PROPERTY THE REAL TABLE HAS —
// which is what makes the tests over it measurements rather than decoration:
//
//   - all three vocabulary terms appear, so a term that stopped being mapped is visible;
//   - `alpha` and `alpha-fleet` DISAGREE, and `alpha` is a strict prefix of
//     `alpha-fleet` — the counterexample that makes "never a prefix rule" demonstrable
//     rather than merely asserted;
//   - `alpha` and `alpha-app` AGREE, so the disagreement above cannot be mistaken for
//     "every longer name flips";
//   - `beta` and `beta-infra` disagree too, so the property is not a fact about one row.
//
// 🔴 DUPLICATE KEYS ARE A COMPILE ERROR IN A GO MAP LITERAL, so "no key appears twice"
// is structural here and is deliberately NOT asserted by a test — an assertion for a
// state the compiler cannot produce reads as coverage and provides none.
var scopeTagTable = map[string]string{
	"alpha":       "product",
	"alpha-app":   "product",
	"alpha-fleet": "infra",
	"beta":        "product",
	"beta-infra":  "infra",
	"gadget-cli":  "tooling",
	"widget-kit":  "tooling",
}

// ScopeTag answers which vocabulary term a scope's entries carry.
//
// 🔴 EXACT MATCH, AND THE SECOND RETURN IS THE WHOLE POINT. An unknown scope is
// `("", false)` — never a guess, and never a fall-back to a prefix's answer. A caller
// that wants "leave it alone" and a caller that wants "stop and ask" both need to tell
// "unclassified" from "classified as X", and a single-value signature cannot say it.
//
// ⚠ THE OPERAND IS FOLDED FIRST, so a caller may pass a directory name as written. The
// fold is `store.NormalizeRef`, the same one the scope directory went through, for the
// reason `HasTag` gives: one rule, one place, on the side that owns the spelling.
func ScopeTag(scope string) (string, bool) {
	tag, ok := scopeTagTable[store.NormalizeRef(scope)]
	return tag, ok
}

// ValidateScopeTagTable answers "is this a table `ScopeTag` could serve", and it is
// EXPORTED so the deployment's own rows — which this repository may not hold, see the
// file header — can be checked by this code rather than by a second copy of it.
//
// Two invariants, and each is a way a table can be silently useless rather than wrong:
//
//   - every VALUE is a declared vocabulary term. A table that maps a scope to a term no
//     write may land classifies entries into a category the pod refuses, so the rule and
//     the vocabulary would disagree about what is writable. This is the invariant that
//     `client-work` would have broken when it was dropped.
//   - every KEY is already its own folded form. A key that folds to something else can
//     never be matched by `ScopeTag`, because the lookup folds the operand — so the row
//     is dead weight that reads like a classification.
//
// Every problem is reported, sorted, not just the first: an operator fixing a table
// wants the list.
func ValidateScopeTagTable(table map[string]string) error {
	var problems []string
	for scope, tag := range table {
		if !slices.Contains(tagVocabulary, tag) {
			problems = append(problems, fmt.Sprintf(
				"scope %s maps to tag %s, which is not one of %s — no write could land it",
				store.PyRepr(scope), store.PyRepr(tag), strings.Join(tagVocabulary, "|")))
		}
		if folded := store.NormalizeRef(scope); folded != scope {
			problems = append(problems, fmt.Sprintf(
				"scope key %s folds to %s, so `ScopeTag` can never match it",
				store.PyRepr(scope), store.PyRepr(folded)))
		}
	}
	if len(problems) == 0 {
		return nil
	}
	sort.Strings(problems)
	return fmt.Errorf("scope->tag table: %s", strings.Join(problems, "; "))
}

// prefixDisagreements lists every ordered pair (short, long) in a table where `short` is
// a strict prefix of `long` and the two carry DIFFERENT tags.
//
// 🔴 IT EXISTS TO MAKE "NEVER A PREFIX RULE" A MEASUREMENT RATHER THAN A COMMENT. The
// file header claims a prefix match gets real tables backwards; this function is what a
// test uses to show the committed table actually CONTAINS such a pair, so the claim is
// demonstrated by data instead of asserted by prose. If it ever returns nothing, the
// claim has lost its counterexample and the header needs re-deriving — which is a
// finding, not a pass.
func prefixDisagreements(table map[string]string) [][2]string {
	keys := make([]string, 0, len(table))
	for k := range table {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var out [][2]string
	for _, short := range keys {
		for _, long := range keys {
			if short == long || !strings.HasPrefix(long, short) {
				continue
			}
			if table[short] != table[long] {
				out = append(out, [2]string{short, long})
			}
		}
	}
	return out
}
