#!/usr/bin/env python3
"""The ORACLE half of the marker-transcription differential sweep.

🔴 WHAT THIS FILE IS FOR, IN ONE SENTENCE: it stops
`internal/store/testdata/marker_oracle_sweep.json` from becoming a golden. The Go half
(`internal/store/markersweep_test.go`) compares two hand-rolled transcriptions against
the verdicts that fixture carries; if nothing re-derived those verdicts from the LIVE
patterns, an edit to `_MARKER_ANYWHERE` or `_NEAR_MISS_MARKER` would leave Go measured
against a regex that no longer exists — green, and about nothing. Every run here
recomputes all 10,822 x 2 verdicts from `lib/subsystem_resolver` and requires the
fixture to match bit for bit.

🔴 AND IT IS THE ONLY SIDE THAT CAN SEE AN ORACLE CHANGE. The Go side has no CPython;
the fixture is the whole of what it knows about the oracle. So a failure here is never
"regenerate the fixture" on its own — it means one of the two patterns moved, and the
Go transcription has to be re-derived against the new one BEFORE the fixture is
rewritten. `python3 tests/marker_corpus.py` regenerates; do it second, not first.

⚠ WHAT IT DOES NOT DO. It does not run the Go walk and cannot: there is no CPython
binding to `internal/store`. The comparison is the Go test's, and it needs a `go`
toolchain the same way `test_go_client_ledgers.py` does. This file's whole job is to
make the fixture a faithful record of the oracle — nothing here is evidence about Go.
"""
from __future__ import annotations

import itertools
import re
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
for _p in (ROOT / "lib", ROOT / "tests"):
    if str(_p) not in sys.path:
        sys.path.insert(0, str(_p))

import marker_corpus  # noqa: E402
import subsystem_resolver  # noqa: E402


def test_the_fixture_records_the_LIVE_oracle_and_not_a_snapshot():
    """The whole point: re-derive, never trust."""
    lines = marker_corpus.corpus()
    fixture = marker_corpus.load_fixture()
    assert fixture["corpus_lines"] == len(lines)
    assert fixture["corpus_sha256"] == marker_corpus.digest(lines), (
        "the committed corpus checksum does not match the generator. The Go side "
        "assembles from the same axes and checks the same hash, so it is about to "
        "compare verdicts against lines nobody ran."
    )
    anywhere, near_miss = marker_corpus.oracle_bits(lines)
    assert fixture["oracle_marker_anywhere"] == anywhere, (
        "`_MARKER_ANYWHERE` no longer answers what the fixture records. Re-derive "
        "`store.LineMentionsMarker` against the NEW pattern before regenerating."
    )
    assert fixture["oracle_near_miss_marker"] == near_miss, (
        "`_NEAR_MISS_MARKER` no longer answers what the fixture records. Re-derive "
        "`store.nearMissMarker` against the NEW pattern before regenerating."
    )


def test_the_corpus_is_not_degenerate():
    """🔴 THE POSITIVE CONTROL ON THE CORPUS ITSELF.

    A sweep over lines that all answer the same way measures nothing, and so does a
    sweep over an axis somebody quietly emptied. Both verdicts must be MIXED, and the
    corpus must be big enough to be a product rather than a handful of cases.
    """
    lines = marker_corpus.corpus()
    assert len(lines) > 1000, len(lines)
    for key in ("oracle_marker_anywhere", "oracle_near_miss_marker"):
        bits = marker_corpus.load_fixture()[key]
        ones = bits.count("1")
        assert 0 < ones < len(bits), f"{key}: {ones} true of {len(bits)} — degenerate"


AXES = {
    "prefixes": marker_corpus.PREFIXES,
    "dates": marker_corpus.DATES,
    "words": marker_corpus.WORDS,
    "refs": marker_corpus.REFS,
    "terminators": marker_corpus.TERMINATORS,
}


def _oracle(line: str) -> tuple[bool, bool]:
    return (subsystem_resolver._MARKER_ANYWHERE.search(line) is not None,
            subsystem_resolver._NEAR_MISS_MARKER.match(line) is not None)


def _spines_where_axis_flips_the_oracle(axis: str) -> int:
    """How many fixed settings of the OTHER axes this one changes an oracle verdict on."""
    others = [name for name in AXES if name != axis]
    flips = 0
    for combo in itertools.product(*[AXES[name] for name in others]):
        row = dict(zip(others, combo))
        verdicts = set()
        for value in AXES[axis]:
            row[axis] = value
            verdicts.add(_oracle(
                row["prefixes"] + row["dates"] + row["words"] + row["refs"]
                + row["terminators"] + marker_corpus.TAIL))
        if len(verdicts) > 1:
            flips += 1
    return flips


def test_no_axis_carries_a_duplicated_or_single_value():
    """A duplicated axis value inflates the corpus and measures nothing twice."""
    for name, values in AXES.items():
        assert len(values) > 1, f"{name} has one value, so it varies nothing"
        assert len(set(values)) == len(values), f"{name} carries a duplicate"


def test_the_DATE_axis_flips_no_ORACLE_verdict_AND_THAT_IS_WHY_IT_IS_HERE():
    """🔴 THE ONE AXIS A "PRUNE THE DEAD WEIGHT" PASS WOULD DELETE, PINNED WITH ITS
    REASON — and the first draft of this file nearly deleted it for exactly that.

    An axis earns its place by discriminating THE COMPARISON, not by flipping the
    oracle. The date prefix is optional in `_NEAR_MISS_MARKER`, so a dated line and an
    undated one both match and the oracle's answer never moves along this axis —
    MEASURED below at 0 spines out of 2,160. It is in the corpus because it moved the
    GO answer: `looksISODate` read `\\d` as `[0-9]`, so `- ٢٠٠٠-٠١-٠٤: open:` matched
    the oracle and not the transcription, 1,020 lines of divergence that no other axis
    could reach.

    ⚠ SO A ZERO HERE IS THE EXPECTED READING, NOT A FAILURE, and a NON-zero is the
    finding: it would mean the oracle's date handling has changed shape. The other four
    axes flip freely and are pinned as non-zero in the same breath, so this is a
    two-way ledger rather than an excuse for one axis.
    """
    assert _spines_where_axis_flips_the_oracle("dates") == 0
    for axis in ("prefixes", "words", "refs", "terminators"):
        assert _spines_where_axis_flips_the_oracle(axis) > 0, axis


def test_the_re_I_folding_runes_are_exactly_the_four_the_code_names():
    """🔴 THE ENUMERATION `internal/store.foldsToASCIILetter` PINS, RE-MEASURED.

    That function hardcodes U+0130, U+0131, U+017F and U+212A as the complete set of
    non-ASCII runes CPython's `re.IGNORECASE` folds into `[A-Za-z0-9_]`. It is a claim
    about the interpreter, not about the repo, so it is measured here rather than
    asserted there — a new Unicode version adding a fifth would otherwise make the Go
    walk silently wider than the oracle again, in exactly the direction that
    manufactures a declaration nobody typed.

    ⚠ Deliberately a FULL sweep of the codepoint space. Testing the four known ones
    proves they still fold and says nothing about a fifth, which is the failure mode.
    """
    klass = re.compile("[A-Za-z0-9_]", re.I)
    found = {c for c in range(0x110000) if c > 127 and klass.fullmatch(chr(c))}
    assert found == {0x0130, 0x0131, 0x017F, 0x212A}, sorted(hex(c) for c in found)

    # The negated form is the other half of the same fact, and it is the one
    # `terminatorColon` reads: `re.I` WIDENS the class, so it NARROWS the negation.
    negated = re.compile(r"[^A-Za-z0-9\n]", re.I)
    excluded = {c for c in (0x0130, 0x0131, 0x017F, 0x212A)
                if negated.fullmatch(chr(c))}
    assert excluded == set(), sorted(hex(c) for c in excluded)


def test_the_hex_atom_class_gains_nothing_under_re_I():
    """The claim `refAtomEnds` makes about its own hex run, measured rather than argued.

    `[0-9a-fA-F]` is read ASCII-only in Go with a comment saying `re.I` adds nothing to
    it. That is only true because no non-ASCII rune folds onto a hex digit — `ſ` folds
    onto `s` and `K` onto `k`, neither of which is in the class.
    """
    klass = re.compile("[0-9a-fA-F]", re.I)
    assert not [c for c in range(0x110000) if c > 127 and klass.fullmatch(chr(c))]
