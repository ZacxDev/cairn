#!/usr/bin/env python3
"""The one-instance compatibility narrowing is echoed at five sites — PINNED.

🔴 TWO FILED DEFECTS, ONE GUARD, BECAUSE THEY ARE THE SAME DEFECT. The handoff
carried them separately:

  * *"COUNTS QUOTED IN PROSE THAT NOTHING ASSERTS ON — `lib/README.md`'s
    echo-site count."* Closing condition: a decision to pin it, or a written line
    saying why not. **Decision: PIN IT**, for the reason the count's own bullet
    records — it already said "all three" once and was an undercount.
  * #48's ladder, (c): *"`lib/README.md` — the re-count recipe greps two literal
    phrases, so it is a SPELLED check that cannot see a reworded echo."*

A ledger of the SITES closes both, and closes the second one in the direction
that matters. A reworded echo does not go quiet here: the reworded file stops
matching, drops out of the discovered set, and the SHRINK arm names it. That is
the whole difference between a recipe a human runs and a guard that runs itself.

## What the claim is, and why it is echoed at all

The multi-instance work ships routing machinery into a client every existing host
runs with ONE store. The compatibility claim is *"a one-instance host's bytes are
unchanged"* — and that sentence is true of **labelling** and was read as covering
**routing**, which it does not. Each of the five sites therefore carries the
narrowing beside the claim, so a contributor reading any one of them cannot
re-derive the wrong expectation. The narrowing is the thing being protected; the
count was only ever a note about how many places to visit.

## 🔴 NORMALISED BEFORE MATCHING, AND THAT IS NOT A DETAIL

`lib/README.md` states the claim across a LINE BREAK — *"a one-instance host's
bytes / are unchanged"*. A line-based `grep` for the phrase does not see it, and
returns a confident four. The recipe this guard replaces is line-based, which is
why `lib/README.md` appeared in its output only by accident: the file quotes the
OTHER phrase while describing the recipe itself. Match over whitespace-normalised
text or inherit the same blindness.

⚠ WHAT THIS DOES NOT DO. It does not check that each site's narrowing is
CORRECT, or that the five say the same thing — only that the claim is still
spelled at exactly these five files. A site that keeps the phrase and reverses
the narrowing around it passes. That is a real limit and it is named rather than
left to be discovered; closing it would need the whole normalised sentence
pinned at each site, which is the heavier instrument `AGENTS.md` prescribes for
prose whose exact words are the contract.
"""

from __future__ import annotations

import re
from pathlib import Path

REPO = Path(__file__).resolve().parents[1]

#: The two spellings the claim appears in. BOTH are needed: the sites split
#: between them, and either alone discovers a strict subset.
CLAIM_SPELLINGS = (
    "one-instance host's bytes are unchanged",
    "byte-for-byte what it was",
)

#: The five files that carry the claim and its narrowing. A LEDGER, failing on
#: GROW *or* SHRINK — GROW because a sixth site is one more place to keep true,
#: SHRINK because a site that stopped matching has been reworded or deleted, and
#: a reworded echo going quiet is the defect this replaces.
EXPECTED_SITES = {
    "cairn",
    "internal/client/instances.go",
    "internal/client/routes.go",
    "lib/README.md",
    "tests/parity/README.md",
    # ⚠ THE SIXTH, AND THIS GUARD CAUGHT ITS OWN AUTHOR ADDING IT — minutes after
    # the guard went green, in the same PR. `unchanged_output_capture.py` is the
    # INSTRUMENT that measures the claim, and the same commit gave it a docstring
    # section naming the claim it measures; the GROW arm fired and named the file.
    # Admitted deliberately rather than reworded away: a reader asking "where is
    # this claim written down" should find the harness that checks it. The episode
    # is the argument for the ledger in one line — the count was five for about
    # twenty minutes.
    "tests/unchanged_output_capture.py",
}

#: Where to look. Scoped to the trees that carry prose about the client; the
#: whole repo would drag in `.git` and build output for no gain.
SEARCH_ROOTS = ("cairn", "lib", "internal", "tests", "server", "README.md", "AGENTS.md")

#: Files that contain a spelling but make a DIFFERENT claim with it, excluded by
#: name and with the reason, because an unexplained exclusion is a hole.
#: `tests/test_subsystem_store_api.py` says "the pre-existing bytes are
#: unchanged" about a WRITE preserving neighbouring bytes — same words, nothing
#: to do with single-instance labelling. It is matched only by the unnormalised
#: substring, and is listed here so a reader does not have to re-derive that.
#:
#: ⚠ AND THIS FILE ITSELF, which quotes both spellings in order to search for
#: them — it discovered itself on its first run. Excluded by name rather than by
#: a cleverer match, because a guard that tried to avoid naming its own spellings
#: could not state them, and stating them is what makes this readable.
NOT_THIS_CLAIM = {
    "tests/test_subsystem_store_api.py",
    "tests/test_narrowing_echo_sites.py",
}


def _normalised(path: Path) -> str:
    """The file's text with every run of whitespace collapsed to one space.

    🔴 This is what makes a claim wrapped across two lines visible. See the
    module docstring: the recipe this replaces was line-based and undercounted
    for exactly that reason.
    """
    return re.sub(r"\s+", " ", path.read_text(encoding="utf-8", errors="replace"))


def _candidates() -> list[Path]:
    out: list[Path] = []
    for root in SEARCH_ROOTS:
        p = REPO / root
        if p.is_file():
            out.append(p)
        elif p.is_dir():
            out.extend(
                q for q in p.rglob("*")
                if q.is_file() and q.suffix in ("", ".py", ".go", ".md", ".nix", ".sh")
            )
    return out


def _sites_carrying_the_claim() -> set[str]:
    found = set()
    for path in _candidates():
        rel = path.relative_to(REPO).as_posix()
        if rel in NOT_THIS_CLAIM:
            continue
        text = _normalised(path)
        if any(spelling in text for spelling in CLAIM_SPELLINGS):
            found.add(rel)
    return found


def test_both_spellings_are_load_bearing_a_POSITIVE_CONTROL() -> None:
    """🔴 VALIDATE THE INSTRUMENT. Two spellings are declared; if either found
    nothing, the ledger below would be a claim about one spelling wearing the
    name of two, and dropping the dead one would look safe.

    It also refuses the degenerate case where one spelling's hits are a SUBSET of
    the other's — then the set is discoverable by one phrase and the second is
    decoration.
    """
    per_spelling = {}
    for spelling in CLAIM_SPELLINGS:
        hits = {
            p.relative_to(REPO).as_posix()
            for p in _candidates()
            if p.relative_to(REPO).as_posix() not in NOT_THIS_CLAIM
            and spelling in _normalised(p)
        }
        assert hits, f"the spelling {spelling!r} matches NOTHING — it is dead"
        per_spelling[spelling] = hits
    a, b = (per_spelling[s] for s in CLAIM_SPELLINGS)
    assert not a <= b and not b <= a, (
        f"one spelling's sites are a subset of the other's ({sorted(a)} vs {sorted(b)}), "
        f"so the union is discoverable by one phrase alone. Drop the redundant "
        f"spelling, or work out which site stopped needing it."
    )


def test_the_narrowing_is_carried_at_EXACTLY_these_sites() -> None:
    found = _sites_carrying_the_claim()
    assert found == EXPECTED_SITES, (
        f"the set of sites carrying the one-instance compatibility claim moved. "
        f"GREW by {sorted(found - EXPECTED_SITES)} — a new site is one more place "
        f"the narrowing has to be true, so add it here once you have written the "
        f"narrowing there. SHRANK by {sorted(EXPECTED_SITES - found)} — a site "
        f"that stopped matching has been reworded or deleted, which is the exact "
        f"failure this ledger replaced a human-run grep recipe to make loud."
    )
