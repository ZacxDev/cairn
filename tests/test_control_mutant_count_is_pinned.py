"""The battery's mutant COUNT is quoted in prose at five sites. Pin it to the code.

🔴 A NUMBER QUOTED IN PROSE AND DERIVABLE FROM CODE IS A CLAIM WITH NO GATE, AND THIS
ONE HAS GONE STALE TWICE. Measured history of `internal/control/README.md`'s headline:
28/27/1 at the commit that introduced the battery, 61/60/1 when it grew to three
packages, 62/61/1 when it grew again — and it then sat at `62/61/1` across a commit
where the tree measured **72/70/2**, because that round added ten mutants and edited
forty-four lines of the same README without re-deriving its own headline. The round
after it corrected the number and left `.github/workflows/ci.yml` carrying the stale one
in TWO places, one of them a step NAME, which is the string a reader sees in the CI UI.

🔴 THE PREVIOUS REMEDY WAS PROSE AND PROSE IS WHAT FAILED. The file carries a
`⚠ RE-DERIVE THESE` instruction; it was present for both staleness events. The same
round that wrote it gave a *different* stale number — the epoch citation — a real pin
(`tokenfile.TestARevocationMakesTheEpochGoDOWN`) and that one has not drifted since.
This is that treatment for the count.

⚠ WHAT THIS PINS, AND WHAT IT DOES NOT. It pins the **mutant count** only — the number
that is mechanically derivable from `MUTANTS`. The kill/survivor split (`71 killed,
1 EQUIVALENT`) is the OUTCOME of a run and cannot be asserted without running the
battery, which takes minutes and belongs in the `go` CI job where it already lives. So a
wrong split can still ship; this closes the half that never had to.

The precedent is `tests/test_flake_image_matches_dockerfile.py`: two files stating one
fact, pinned against each other, red when one moves alone.
"""

from __future__ import annotations

import importlib.util
import re
import sys
from pathlib import Path

import pytest

REPO_ROOT = Path(__file__).resolve().parents[1]
BATTERY = REPO_ROOT / "tests" / "control_mutants.py"
README = REPO_ROOT / "internal" / "control" / "README.md"
CI = REPO_ROOT / ".github" / "workflows" / "ci.yml"


def _mutant_count() -> int:
    """Import the battery and count its declared mutants.

    Imported rather than parsed: the module is the authority, and a regex over its
    source would be a second way to count that can disagree with the first — which is
    the shape this whole file exists to close.
    """
    spec = importlib.util.spec_from_file_location("cairn_control_mutants", BATTERY)
    assert spec and spec.loader, f"cannot load {BATTERY}"
    module = importlib.util.module_from_spec(spec)
    sys.modules[spec.name] = module
    spec.loader.exec_module(module)
    return len(module.MUTANTS)


@pytest.fixture(scope="module")
def count() -> int:
    return _mutant_count()


def test_the_battery_declares_a_plausible_number_of_mutants(count: int) -> None:
    """A POSITIVE CONTROL on this file's own instrument.

    🔴 EVERY ASSERTION BELOW SEARCHES FOR A NUMBER, AND A SEARCH FOR THE WRONG NUMBER
    FAILS THE SAME WAY A STALE DOCUMENT DOES. If `_mutant_count` ever returned 0 — an
    import that half-executed, a renamed `MUTANTS` — the other tests would go red naming
    the documents, and a reader would edit the documents. So the count is checked for
    sanity before anything is checked against it.
    """
    assert count > 1, (
        f"the battery declares {count} mutant(s) — this file's instrument is broken, "
        "and the failures below would blame the documents for it"
    )


# The README states the count in the PRESENT tense at these anchors, and each must
# carry the current number. Declared as a ledger rather than matched loosely, so
# DELETING an anchor fails too — a silently-dropped claim is the other way prose and
# code come apart.
PRESENT_TENSE_ANCHORS = (
    "python3 tests/control_mutants.py          # {n} mutants",
    "Measured on this tree: {n} mutants",
    "The battery is {n} mutants",
)

# 🔴 A HISTORICAL MENTION IS NOT A STALE ONE, AND THE FIRST DRAFT OF THIS FILE COULD NOT
# TELL THEM APART. It matched every `N mutants` in the file and went RED on correct
# prose: the README's timing comparison says "2m46s **at 62 mutants** … against 2m01s for
# the same battery **at 61 mutants**", which is a measurement of two OLDER batteries on
# one host and is exactly as true as the day it was written. A guard that reds on a
# sentence nobody should change trains its reader to edit the sentence.
#
# ⚠ THE DISCRIMINATOR IS A PHRASING, AND A PHRASING IS WALKABLE — say so rather than
# imply otherwise. `at N mutants` reads as historical and is exempt; a NEW present-tense
# claim written as "at 99 mutants" would evade this. That is accepted: the anchors above
# are the sites that have actually gone stale, the exemption is the form the file already
# uses for history, and pinning whole normalised paragraphs would red on every reword.
#
# ⚠ `\s+`, NOT A LITERAL SPACE, AND THE FIRST DRAFT USED A SPACE AND WAS WRONG. The
# README wraps this very phrase across a line break — `…the same battery at\n61 mutants
# over three…` — so a literal space misses the one historical mention in the file and
# reds on it. A prose guard that cannot see a line wrap is a prose guard that fires on
# correctly-formatted prose.
HISTORICAL = re.compile(r"\bat\s+\d+\s+mutants\b")


def test_the_README_present_tense_claims_match_the_battery(count: int) -> None:
    text = README.read_text(encoding="utf-8")

    missing = [a.format(n=count) for a in PRESENT_TENSE_ANCHORS if a.format(n=count) not in text]
    assert not missing, (
        f"{README.relative_to(REPO_ROOT)} does not carry these present-tense claims at "
        f"{count} mutants:\n  " + "\n  ".join(repr(m) for m in missing) + "\n"
        "Either the count moved and the prose did not, or an anchor was reworded/deleted. "
        "Re-derive from `python3 tests/control_mutants.py` rather than editing the number "
        "to match — the kill/survivor split beside it is NOT pinned by anything, and it is "
        "the half that has been wrong before."
    )

    # And nothing ELSE in the file may state a different count in the present tense.
    stale = sorted(
        {
            m.group(1)
            for m in re.finditer(r"(\d+) mutants", text)
            if int(m.group(1)) != count
            # A window wide enough to hold `at` plus a line wrap plus the number.
            and not HISTORICAL.search(text[max(0, m.start() - 8) : m.end()])
        }
    )
    assert not stale, (
        f"{README.relative_to(REPO_ROOT)} states {stale} mutants outside a historical "
        f"`at N mutants` phrasing; the battery declares {count}."
    )


def test_the_CI_step_matches_the_battery(count: int) -> None:
    """🔴 THE STEP *NAME* IS IN SCOPE, NOT ONLY THE COMMENT.

    The name is what appears in the GitHub Actions UI, so a stale one is the most-read
    copy of the number. It carried `62` for a whole round while the comment above it had
    already been corrected.
    """
    text = CI.read_text(encoding="utf-8")
    step = [ln for ln in text.splitlines() if "prove every authz guard can go RED" in ln]
    assert len(step) == 1, f"expected exactly one battery step in {CI}, found {len(step)}"
    found = re.search(r"\((\d+) mutants", step[0])
    assert found, f"the battery step name quotes no count: {step[0].strip()}"
    assert int(found.group(1)) == count, (
        f"the CI step NAME says {found.group(1)} mutants; the battery declares {count}. "
        "That string is what a reader sees in the Actions UI."
    )
