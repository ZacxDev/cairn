#!/usr/bin/env python3
"""The one-instance compatibility narrowing, and every site that spells it.

🔴 TWO FILED DEFECTS, ONE GUARD, BECAUSE THEY ARE THE SAME DEFECT. The handoff
carried them separately:

  * *"COUNTS QUOTED IN PROSE THAT NOTHING ASSERTS ON — `lib/README.md`'s
    echo-site count."* Closing condition: a decision to pin it, or a written line
    saying why not. **Decision: PIN IT**, for the reason the count's own bullet
    records — it already said "all three" once and was an undercount.
  * #48's ladder, (c): *"`lib/README.md` — the re-count recipe greps two literal
    phrases, so it is a SPELLED check that cannot see a reworded echo."*

A ledger of the SITES closes them in the direction that matters. A reworded
NAMED site does not go quiet: it stops matching, drops out of the discovered set,
and the SHRINK arm names it — the difference between a recipe a human runs and a
guard that runs itself.

🔴 **THE FIRST DRAFT OVERCLAIMED EXACTLY WHERE #48's RECIPE DID, TWO AUDITS CAUGHT
IT, AND IT IS THE MOST IMPORTANT THING ON THIS PAGE.** It asserted the set of
files "carrying the claim" while discovering them with THE SAME TWO LITERAL
PHRASES the deleted recipe grepped. So it was loud about a site reworded AFTER it
was written, and blind to one written in different words from birth — and two such
sites were already in the tree: `tests/test_cairn_instances.py`, whose own
docstring reads *"this file is the site that sweep missed"*, and
`internal/client/state.go`. Both are covered now by ADDING THEIR SPELLINGS, not by
widening the sentence.

## What the claim is, and why it is echoed at all

The multi-instance work ships routing machinery into a client every existing host
runs with ONE store. The compatibility claim is *"a one-instance host's bytes are
unchanged"* — and that sentence is true of **labelling** and was read as covering
**routing**, which it does not. Each site therefore carries the narrowing
beside the claim, so a contributor reading any one of them cannot
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
CORRECT, or that the sites say the same thing — only that one of the known
spellings is still present at each named file, and that no file inside the search
roots has started using a known spelling without being listed.

🔴 **THE IRREDUCIBLE GAP, STATED BECAUSE IT CANNOT BE CLOSED.** A site that states
the claim in words no spelling below matches is invisible here, exactly as it was
to the recipe this replaced. Nothing mechanical finds a semantic claim; what this
buys is that every site we KNOW of is watched, and that adding a site's spelling
is what admits it. When you write this claim somewhere new, add the spelling.

⚠ AND A SITE THAT KEEPS ITS PHRASE WHILE REVERSING THE NARROWING AROUND IT PASSES. That is a real limit and it is named rather than
left to be discovered; closing it would need the whole normalised sentence
pinned at each site, which is the heavier instrument `AGENTS.md` prescribes for
prose whose exact words are the contract.
"""

from __future__ import annotations

import re
from pathlib import Path

REPO = Path(__file__).resolve().parents[1]

#: Every spelling of the claim this repository is known to use. 🔴 EACH ONE MUST
#: EARN ITS PLACE — `test_every_spelling_finds_a_site_NO_OTHER_ONE_DOES` refuses a
#: redundant entry, so this cannot silently become a list of near-duplicates.
#:
#: The last two were added because two audits found the sites they name: the first
#: draft declared only the top pair — the SAME two literal phrases #48's deleted
#: recipe grepped — and was therefore blind to a site phrased differently from
#: birth. `tests/test_cairn_instances.py` is the one whose own docstring says
#: *"this file is the site that sweep missed"*, which it then was again.
CLAIM_SPELLINGS = (
    "one-instance host's bytes are unchanged",
    "byte-for-byte what it was",
    "prints the SAME BYTES it always did",
    "renders exactly what this client has always rendered",
)

#: The files that carry the claim and its narrowing. A LEDGER, failing on
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
    # The two an audit found AFTER that, both phrased differently from birth and
    # therefore invisible to the original pair of spellings. `state.go` reaches
    # the set through the spelling it shares with `cairn`.
    "tests/test_cairn_instances.py",
    "internal/client/state.go",
}

#: Where to look. 🔴 `cmd/` AND THE TOP-LEVEL FILES WERE MISSING, AND AN AUDIT
#: MEASURED THE HOLE: the claim planted in `cmd/cairn/main.go`, `CHANGELOG.md`,
#: `CLAUDE.md`, `flake.nix` and `.github/workflows/ci.yml` left BOTH arms green.
#: `cmd/cairn` is the client's own entry point and the single most likely home for
#: a new echo, given the ledger already names two files under `internal/client/`.
#: Still scoped rather than whole-repo, because `.git` and build output buy
#: nothing — `claudedocs/` is deliberately out, being a record of rounds rather
#: than a place the claim binds an edit.
SEARCH_ROOTS = (
    "cairn", "lib", "internal", "tests", "server", "cmd",
    "README.md", "AGENTS.md", "CHANGELOG.md", "CLAUDE.md", "flake.nix",
)

#: This file itself, which quotes every spelling in order to search for them — it
#: discovered itself on its first run. Excluded by name rather than by a cleverer
#: match, because a guard that tried to avoid naming its own spellings could not
#: state them, and stating them is what makes this readable.
#:
#: 🔴 IT HAD A SECOND ENTRY AND THAT ENTRY WAS INERT, ON A REASON THAT WAS
#: IMPOSSIBLE. `tests/test_subsystem_store_api.py` was excluded "because it is
#: matched only by the unnormalised substring" — which cannot happen: `_normalised`
#: only collapses whitespace runs, so a raw match IMPLIES a normalised one, never
#: the reverse. Measured: deleting the entry changed nothing, because that file
#: contains no declared spelling and was never a candidate. It is removed rather
#: than corrected — an exclusion by filename is a standing mask, and that one told
#: the next reader not to look at a 16k-line file if it ever grew the real claim.
NOT_THIS_CLAIM = {"tests/test_narrowing_echo_sites.py"}


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


def _sites_per_spelling() -> dict[str, set[str]]:
    out: dict[str, set[str]] = {}
    for spelling in CLAIM_SPELLINGS:
        out[spelling] = {
            q.relative_to(REPO).as_posix()
            for q in _candidates()
            if q.relative_to(REPO).as_posix() not in NOT_THIS_CLAIM
            and spelling in _normalised(q)
        }
    return out


def test_every_spelling_finds_a_site_NO_OTHER_ONE_DOES() -> None:
    """🔴 VALIDATE THE INSTRUMENT, AND KEEP THE SPELLING LIST FROM ROTTING.

    A dead spelling — one matching nothing — would make the ledger a claim about
    fewer phrases than it names, and deleting it would look safe. A REDUNDANT
    spelling is the subtler rot: it finds only sites another spelling already
    finds, so it reads as widening coverage while adding none, and the list grows
    into near-duplicates nobody dares prune.

    So each spelling must contribute at least one site that no other spelling
    finds. That is the generalisation of the two-spelling subset check this
    replaced, and it is what admits a fourth entry only when it buys a site.
    """
    per_spelling = _sites_per_spelling()
    for spelling, hits in per_spelling.items():
        assert hits, f"the spelling {spelling!r} matches NOTHING — it is dead"
    for spelling, hits in per_spelling.items():
        others: set[str] = set()
        for other, other_hits in per_spelling.items():
            if other != spelling:
                others |= other_hits
        unique = hits - others
        assert unique, (
            f"the spelling {spelling!r} finds only sites other spellings already "
            f"find ({sorted(hits)}), so it widens nothing. Drop it, or work out "
            f"which site stopped needing it."
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
