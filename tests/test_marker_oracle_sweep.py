#!/usr/bin/env python3
"""The ORACLE half of the marker-transcription differential sweep.

🔴 WHAT THIS FILE IS FOR, IN ONE SENTENCE: it stops
`internal/store/testdata/marker_oracle_sweep.json` from becoming a golden. The Go half
(`internal/store/markersweep_test.go`) compares two hand-rolled transcriptions against
the verdicts that fixture carries; if nothing re-derived those verdicts from the LIVE
patterns, an edit to `_MARKER_ANYWHERE` or `_NEAR_MISS_MARKER` would leave Go measured
against a regex that no longer exists — green, and about nothing. Every run here
recomputes every verdict — both patterns, every corpus line — from
`lib/subsystem_resolver` and requires the fixture to match bit for bit. The corpus
size is not written down here on purpose: `corpus_lines` in the fixture carries it,
this file asserts it against a freshly assembled corpus, and the last prose copy of
that number went stale inside the commit that wrote it.

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

    # 🔴 AND THE TARGET EACH ONE FOLDS ONTO, because a SET is not enough for the
    # sweep's attribution clause. `internal/store/markersweep_test.go`'s `reIFoldsOnto`
    # respells a divergent line the way `re.I` reads it and requires the divergence to
    # vanish; a wrong target there fails CLOSED (the divergence is reported as
    # undeclared) but for a reason no message would explain. Measured, not argued.
    #
    # 🔴 THIS CLAUSE PINS CPYTHON AGAINST A LITERAL *HERE*, AND THAT IS ALL IT DOES. It
    # does NOT read the Go map, and an earlier comment beside `reIFoldsOnto` claiming it
    # "checks this MAP's targets" was the F2 defect of ZacxDev/cairn#109 — a description
    # wider than its implementation, which is worse than no coverage because it stops
    # anyone looking. What covers the Go side is named where the map is; do not restate
    # it here, and do not read this clause as covering both.
    #
    # ⚠ A LIST PER RUNE, NEVER A SINGLE VALUE: a rune folding onto TWO ASCII letters
    # would make `reIFoldsOnto` ambiguous, and a dict comprehension would hide that by
    # keeping whichever came last.
    folds_onto = {
        c: [t for t in "abcdefghijklmnopqrstuvwxyz0123456789_"
            if re.compile(re.escape(t), re.I).fullmatch(chr(c))]
        for c in sorted(found)
    }
    assert folds_onto == {0x0130: ["i"], 0x0131: ["i"],
                          0x017F: ["s"], 0x212A: ["k"]}, folds_onto


#: 🔴 THE `EXTRA` ROWS NO COUNT CAN SEE, PINNED TWO-WAY SO THEY CANNOT BE DELETED
#: IN SILENCE. Every other axis of the corpus is a cross product, so dropping a value
#: moves `corpus_lines` and the checksum. These are literal rows, and at HEAD they
#: produce ZERO divergences — that is the POINT of them, not a reason to prune them:
#: they exist to observe the two class positions `re.IGNORECASE` narrows, and a
#: population whose healthy reading is zero is exactly the population a "prune the dead
#: weight" pass deletes. MEASURED by reverting `internal/store/openness.go` two ways —
#: and NAME WHICH, because "drop the `ignoreCase` guard" has two readings that move
#: different probes in opposite directions, and a round-1 auditor ran the other one and
#: derived a false finding from the mismatch (`markersweep_test.go`'s `hasFoldingRune`
#: paragraph carries both, measured):
#: DELETING THE CLAUSE — `terminatorColon` read ASCII-only for BOTH callers — makes FOUR
#: of these rows go WIDER than `_MARKER_ANYWHERE` (484 total, 4 outside), and they are
#: the only lines in the corpus that do, while `_NEAR_MISS_MARKER` is untouched at 211;
#: and DROPPING THE CONJUNCT — fold-aware for both callers — is the mirror: 212 total,
#: 2 OUTSIDE the declared residual on `_NEAR_MISS_MARKER`, `_MARKER_ANYWHERE` untouched.
#:
#: 🔴 THE SENTENCE-CASED ROW'S JUSTIFICATION WAS TRUE AND THIS PR FALSIFIED IT, four
#: lines from the ledger it was editing. It read: "with the sentence-cased row removed,
#: dropping that function's `ignoreCase` guard produces ZERO divergences over the ENTIRE
#: corpus and the Go sweep stays green" — i.e. `- Open ſ:` was the ONLY row reaching that
#: mutant. RE-MEASURED at this commit: remove it, regenerate, apply the
#: conjunct mutation, and the sweep reports 1 divergence OUTSIDE the declared residual and
#: goes RED. The new `- Reſolved ſ:` row is sentence-cased with a long s in the terminator
#: run too, so it reaches the same mutant. The row stays — two rows catching it is not a
#: reason to delete either — but it is no longer the ONLY one, and the old sentence would
#: have told a pruner otherwise.
#:
#: ⚠ TWO-WAY ON PURPOSE. Adding a folding-rune row fails this too, which is the
#: intent: a new one belongs in the ledger with its own reason, beside the others.
FOLDING_RUNE_EXTRA = (
    "- OPENſ: a long s immediately after the marker word.",
    "- OPENK: the Kelvin sign immediately after the marker word.",
    "- OPENı: a dotless i immediately after the marker word.",
    "- OPENİ: a dotted capital I immediately after the marker word.",
    "- OPEN ſ: a long s inside the terminator run.",
    "- RESOLVED abc1234 K: a Kelvin sign inside the terminator run.",
    "- OPEN ſſſ: three of them, still inside the run's bound.",
    "- Open ſ: a long s inside the terminator run, sentence-cased.",
)

#: 🔴 THE FOLDING-RUNE ROWS A COUNT *CAN* SEE — a SEPARATE tuple, because the ledger
#: above states an invariant ("no count can see these") that has to stay answerable
#: MECHANICALLY, per row, by a maintainer not holding this file in their head. Folding the
#: one divergent row in would make the answer "it depends which row", which is exactly the
#: property that made that ledger's failure message actionable.
#:
#: This row carries a folding rune in the marker word AND in the terminator run at once:
#: the first is the declared residual, the second a position `_NEAR_MISS_MARKER` reads as
#: literal ASCII. Respelling the WHOLE line conflated them and reported a declared-fold
#: divergence as UNDECLARED. MEASURED: delete it, regenerate, and
#: `go test ./internal/store/ -run TestTheGoMarkerTranscriptions` goes RED on its own —
#: `the declared residual is 210 divergence(s), and this ledger records 211`. So unlike the
#: rows above it is NOT invisible to a count; it is pinned here anyway so both populations
#: are enumerated in ONE place — the UNION cannot grow in silence, because the assertion
#: below is two-way against the filter.
#:
#: 🔴 THE SPLIT ITSELF IS DOCUMENTATION, NOT ENFORCEMENT, AND SAYING SO IS THE POINT.
#: The assertion pins the CONCATENATION, so any re-partition of the same nine rows passes.
#: MEASURED: move this row into `FOLDING_RUNE_EXTRA` — the exact revert of the
#: change that created this tuple — and `pytest tests/test_marker_oracle_sweep.py` is
#: 7 passed and the Go sweep stays green at 480/211. The mirror move passes too.
#: Nothing can close that: which side a row belongs on is a fact about the GO WALK, and
#: this file cannot run it (see this module's docstring — "nothing here is evidence about
#: Go"). A Go-side test pinning the partition was considered and REJECTED: it is a test
#: guarding a test's tuple, the same guard-on-a-guard shape cut from this PR earlier.
#: So the split buys a reader a per-row answer by list membership and buys no guard —
#: do not write, or read, a sentence here implying otherwise.
FOLDING_RUNE_EXTRA_COUNT_VISIBLE = (
    "- Reſolved ſ: a long s in the marker word AND the terminator run.",
)


def test_the_folding_rune_EXTRA_rows_are_pinned_because_NO_COUNT_CAN_SEE_THEM():
    """🔴 THE ONE POPULATION A PASSING SWEEP IS BLIND TO.

    `internal/store/markersweep_test.go` pins the size of the declared residual, so a
    corpus row that DIVERGES is covered by that number. The `FOLDING_RUNE_EXTRA` rows do
    not diverge at HEAD, so deleting them leaves both counts at 480/211 and every test
    green — while removing the only lines that can catch a revert of `terminatorColon`'s
    fold-awareness, in EITHER direction. The ledger above is the guard; this asserts the
    corpus still carries exactly it.

    🔴 TWO TUPLES, ONE FILTER, AND THE SPLIT IS NOT ENFORCED. The corpus does not
    separate the populations — the filter is "carries a folding rune" — so this asserts
    the CONCATENATION. Any re-partition of the same rows passes, measured. WHICH tuple a
    row sits in is the question a reader comes here with, and it is answered by list
    membership as documentation; no instrument checks it, and the tuple's own comment
    says why nothing can.
    """
    present = tuple(
        line for line in marker_corpus.EXTRA
        if any(ch in line for ch in "İıſK")
    )
    assert present == FOLDING_RUNE_EXTRA + FOLDING_RUNE_EXTRA_COUNT_VISIBLE, (
        "the folding-rune rows of `marker_corpus.EXTRA` have moved. The "
        "`FOLDING_RUNE_EXTRA` rows produce no divergence, so no count in either client "
        "can see them go; if one is genuinely obsolete, say in the commit which revert "
        "stops being observable. A row that DOES diverge belongs in "
        "`FOLDING_RUNE_EXTRA_COUNT_VISIBLE` instead. \u26a0 This assertion pins the "
        "UNION, never the split: moving a row between the two tuples passes here AND "
        "passes the Go sweep, so the split is documentation for a reader and nothing "
        "checks it."
    )


def test_the_hex_atom_class_gains_nothing_under_re_I():
    """The claim `refAtomEnds` makes about its own hex run, measured rather than argued.

    `[0-9a-fA-F]` is read ASCII-only in Go with a comment saying `re.I` adds nothing to
    it. That is only true because no non-ASCII rune folds onto a hex digit — `ſ` folds
    onto `s` and `K` onto `k`, neither of which is in the class.
    """
    klass = re.compile("[0-9a-fA-F]", re.I)
    assert not [c for c in range(0x110000) if c > 127 and klass.fullmatch(chr(c))]
