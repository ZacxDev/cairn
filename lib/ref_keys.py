"""THE one definition of the `tasks:` → `refs:` FRONT-MATTER key rename — Python side.

🔴 THERE ARE TWO SPELLINGS OF THIS LEDGER AND THAT IS PACKAGING, NOT DUPLICATION.
`packages.cairn` installs the client script and `lib/` under `libexec` and nothing else,
so this side CANNOT import `internal/store`. That package's `refkeys.go` is the other
spelling, and `tests/test_ref_keys.py` pins the two against each other — failing when the
pair set GROWS *or* SHRINKS, and comparing the rendered warning as a WHOLE NORMALISED
STRING rather than by keyword, because a guard on words is walkable by rewording. The
precedent is `lib/env_aliases.py` against `internal/envalias`, which is the shape
`AGENTS.md` already blesses.

🔴 IT COPIES `env_aliases`'S RULES RATHER THAN INVENTING A THIRD MECHANISM. The repo had
two deprecated-name paths already — `from_mapping` reading `repo:` as `scope:`, and the
environment ledger — and the second is the one whose rules a reader can check:

* **The new key wins WITHIN ONE SOURCE**, a source here being one entry's front matter.
  🔴 And it wins by NOT CONSULTING the old spellings at all, which decides the one case
  that is not obvious: an entry carrying `refs:` beside BOTH `tasks:` and `task:` is not
  refused by their mutual-exclusion rule, because neither is read and there is therefore
  no disagreement to resolve. `from_mapping` carries that reasoning at the branch.
* **An old spelling that is present and truthy warns once per process**, naming its
  replacement. 🔴 TRUTHY, not merely present, and by the SAME test the parser uses: a
  bare `tasks:` line reads as `""`, which the parser treats as an absent key, so warning
  about it would tell an operator to migrate a key that is changing nothing.
* **Warnings are sorted by NEW key, then by OLD key.** `tests/parity/harness.py` compares
  the two clients' stderr BYTE-FOR-BYTE, so the order must be a property of the ledger.
  ⚠ The tie-break on the old key is not decoration: `env_aliases` sorts by new name alone
  because every new name there is distinct, while BOTH pairs here share the new key
  `refs`, so new-key order alone would leave the two lines' relative order to a dict walk.

🔴 WHY THE KEY WAS RENAMED, since `tasks:` parsed fine: it now carries repos, PRs, docs
and dashboards, not only work-tracker items, so `tasks:` NAMED A SUBSET of what it holds.
An operator decision, not a green gate.

The window closes when the PYTHON CLIENT is retired — the same anchor the environment
ledger uses, imported from it rather than restated, because both close on the same event
and two constants would be two places to edit with nothing going red if only one moved.
"""

from __future__ import annotations

from typing import Iterable, Mapping

from env_aliases import REMOVAL_ANCHOR

__all__ = [
    "LEDGER",
    "REMOVAL_ANCHOR",
    "WARNING_FORMAT",
    "deprecations",
    "warning",
]

#: Every renamed front-matter key as `(new, old)`, SORTED BY NEW KEY THEN OLD KEY.
#:
#: 🔴 THE ORDER IS LOAD-BEARING — see the third bullet in the module doc.
#: `tests/test_ref_keys.py` pins it, so a pair appended in the wrong place is a red test
#: rather than a stderr diff the parity harness discovers later.
LEDGER: tuple[tuple[str, str], ...] = (
    ("refs", "task"),
    ("refs", "tasks"),
)

#: The ONE pinned warning text.
#:
#: 🔴 IT STATES THE WITHIN-ENTRY RULE AND NOTHING WIDER, for the reason `env_aliases`'s two
#: formats do: a warning can only know about the source it was raised from. "Where both
#: appear on one entry" is checkable by the operator holding the file; "`refs:` is what the
#: store reads" would be a claim about every entry in the store, which this line has not
#: looked at.
#:
#: 🔴 ONE TEXT, USED WHETHER OR NOT THE NEW KEY SHADOWS THE OLD ONE — the sentence states
#: the RULE rather than this run's outcome, so it is true in both cases. A second "…and it
#: is being ignored because the entry also has `refs:`" wording would double the strings the
#: cross-language gate has to pin and double the ways the two spellings can drift.
#:
#: 🔴 `tests/test_ref_keys.py` EXTRACTS THIS CONSTANT AND `internal/store/refkeys.go`'S
#: `refKeyWarningFormat` BY TEXT and compares the rendered results. Editing one alone is a
#: red test, which is why both are named constants rather than inline format strings.
WARNING_FORMAT = (
    "`{old}:` in an entry's front matter is a deprecated alias for `{new}:`. "
    "Where both appear on one entry, `{new}:` is the one that is read. "
    "Both are accepted until {anchor}."
)


def warning(new: str, old: str) -> str:
    """The pinned text for one front-matter key pair."""
    return WARNING_FORMAT.format(old=old, new=new, anchor=REMOVAL_ANCHOR)


def deprecations(mappings: Iterable[Mapping[str, object]]) -> list[str]:
    """One warning line per OLD key PRESENT AND TRUTHY across `mappings`, in ledger order.

    A pure function of its argument: no module state, no emission. `env_aliases.warn_once`
    is what adds the once-per-process rule, and separating them is what lets a test assert
    the ORDER without reaching into module state.

    🔴 TRUTHINESS IS THE TEST, AND IT IS PYTHON'S OWN — the same `if mapping.get(k):` that
    `from_mapping` asks. A bare `tasks:` reads as `""`; the parser treats that as an absent
    key, so it is not a deprecation.

    ⚠ DEDUPLICATED ACROSS ENTRIES. A store where two hundred files carry `tasks:` produces
    ONE line: the warning is about a KEY the operator has to migrate, not about a file.
    """
    materialized = list(mappings)
    lines: list[str] = []
    for new, old in LEDGER:
        if any(mapping.get(old) for mapping in materialized):
            lines.append(warning(new, old))
    return lines
