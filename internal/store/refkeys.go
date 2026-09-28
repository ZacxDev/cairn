package store

import (
	"fmt"
	"sort"

	"github.com/ZacxDev/cairn/internal/envalias"
)

// This file is THE one definition of the `tasks:` → `refs:` FRONT-MATTER key rename, and
// of the deprecation window both spellings live in.
//
// 🔴 IT COPIES `internal/envalias`'S SHAPE DELIBERATELY RATHER THAN INVENTING A THIRD. The
// repo already had two deprecated-name mechanisms — `EntryFromMapping` reading `repo:` as
// `scope:`, and the `SUBSYSTEM_STORE_*` → `CAIRN_*` ledger — and the second is the one with
// rules a reader can check: the new name wins WITHIN one source, an old name that is present
// warns ONCE PER PROCESS naming its replacement, and the warnings are emitted SORTED BY NEW
// NAME because `tests/parity/harness.py` diffs both clients' stderr byte-for-byte. All three
// rules apply here unchanged; the only thing that differs is what a "source" is (one entry's
// front matter, rather than one environment or one config file).
//
// 🔴 WHY THE KEY WAS RENAMED AT ALL, since `tasks:` parsed fine: the key now carries repos,
// PRs, docs and dashboards, not only work-tracker items, so `tasks:` NAMED A SUBSET of what
// it holds. An operator decision, not a green gate.
//
// 🔴 THERE ARE TWO SPELLINGS OF THIS LEDGER AND THAT IS PACKAGING, NOT DUPLICATION — the
// same reason `internal/envalias` has `lib/env_aliases.py` beside it. `packages.cairn`
// installs the Python client and `lib/` under `libexec` and nothing else, so the Python side
// CANNOT import a file under `internal/`. The second spelling is `lib/ref_keys.py`, and
// `tests/test_ref_keys.py` pins the two against each other — failing when the pair set GROWS
// *or* SHRINKS, and comparing the rendered warning as a WHOLE NORMALISED STRING rather than
// by keyword, because a guard on words is walkable by rewording.

// RefKeyPair is one front-matter key rename: the key to use, and the key that still works.
type RefKeyPair struct {
	New string
	Old string
}

// RefKeyLedger is every renamed front-matter key, SORTED BY NEW NAME THEN BY OLD NAME.
//
// 🔴 THE ORDER IS LOAD-BEARING, NOT COSMETIC. `Deprecations` emits in this order and
// `tests/parity/harness.py` compares the two clients' stderr byte-for-byte, so "whatever
// order the map iteration happened in" is not an option — the order has to be a property of
// the ledger, not of the call sequence.
//
// ⚠ THE TIE-BREAK ON OLD NAME IS NOT DECORATION. `internal/envalias`'s ledger sorts by new
// name alone because every new name there is distinct; here BOTH pairs share the new name
// `refs`, so new-name order alone leaves the two lines' relative order undefined and the
// parity diff would be decided by a map walk. `TestTheRefKeyLedgerIsSorted` pins it.
var RefKeyLedger = []RefKeyPair{
	{New: "refs", Old: "task"},
	{New: "refs", Old: "tasks"},
}

// RefKeyRemovalAnchor is WHEN the old spellings stop being read, in the only terms this repo
// can state it: a milestone a reader can check, never a date.
//
// 🔴 IT IS `envalias.RemovalAnchor` RATHER THAN A SECOND STRING SAYING THE SAME THING. Both
// windows close on the same event — the Python client's retirement — and two constants would
// be two places to edit on the day it happens, with nothing going red if only one moved.
const RefKeyRemovalAnchor = envalias.RemovalAnchor

// refKeyWarningFormat is the ONE pinned warning text.
//
// 🔴 IT STATES THE WITHIN-ENTRY RULE AND NOTHING WIDER, for the reason
// `internal/envalias`'s two formats do: a warning can only know about the source it was
// raised from. "Where both appear on one entry" is checkable by the operator holding the
// file; "`refs:` is what the store reads" would be a claim about every entry in the store,
// which this line has not looked at.
//
// 🔴 ONE TEXT, USED WHETHER OR NOT THE NEW SPELLING SHADOWS THE OLD ONE — the sentence
// states the RULE rather than this run's outcome, so it is true in both cases. A second
// "…and it is being ignored because the entry also has `refs:`" wording would double the
// strings the cross-language gate has to pin and double the ways the two spellings can drift.
//
// 🔴 `tests/test_ref_keys.py` EXTRACTS THIS CONSTANT FROM THIS FILE BY TEXT and compares the
// rendered result against `lib/ref_keys.py`'s. Editing one of them alone is a red test, which
// is the whole reason it is a named constant rather than an inline format string at the call
// site below.
const refKeyWarningFormat = "`%s:` in an entry's front matter is a deprecated alias for " +
	"`%s:`. Where both appear on one entry, `%s:` is the one that is read. Both are accepted " +
	"until " + RefKeyRemovalAnchor + "."

// RefKeyWarning is the pinned text for one front-matter key pair.
func RefKeyWarning(p RefKeyPair) string {
	return fmt.Sprintf(refKeyWarningFormat, p.Old, p.New, p.New)
}

// refKeyOlds maps an old spelling to its replacement.
var refKeyOlds = func() map[string]string {
	m := make(map[string]string, len(RefKeyLedger))
	for _, p := range RefKeyLedger {
		m[p.Old] = p.New
	}
	return m
}()

// RefKeyDeprecations is one warning line per OLD front-matter key that is PRESENT AND
// TRUTHY across `mappings`, sorted by new name then old name, deduplicated.
//
// It is a pure function of its argument: no process state, no emission. `WarnOnce` — the one
// in `internal/envalias`, reached through `client.WarnDeprecations` — is what adds the
// once-per-process rule, and separating them is what lets a test assert the ORDER without
// reaching into package state.
//
// 🔴 TRUTHY, NOT MERELY PRESENT, AND IT IS THE SAME `truthy` THE PARSER READS. A bare
// `tasks:` line reads as the empty string, which the parser treats as an absent key — so
// warning about it would tell an operator to migrate a key that is changing nothing. The
// predicate is shared rather than restated for the reason `envalias.blank` is: spelled
// inline on each side, the two disagreed.
func RefKeyDeprecations(mappings []FrontMatter) []string {
	present := map[RefKeyPair]bool{}
	for _, fm := range mappings {
		for old, newKey := range refKeyOlds {
			if _, ok := truthy(fm, old); ok {
				present[RefKeyPair{New: newKey, Old: old}] = true
			}
		}
	}
	pairs := make([]RefKeyPair, 0, len(present))
	for p := range present {
		pairs = append(pairs, p)
	}
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].New != pairs[j].New {
			return pairs[i].New < pairs[j].New
		}
		return pairs[i].Old < pairs[j].Old
	})
	lines := make([]string, 0, len(pairs))
	for _, p := range pairs {
		lines = append(lines, RefKeyWarning(p))
	}
	return lines
}
