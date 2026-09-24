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
    # ⚠ THIS CLAUSE PINS CPYTHON AGAINST A LITERAL *HERE*, AND THAT IS ALL IT DOES —
    # `test_the_GO_fold_target_map_is_pinned_TWO_WAY_against_CPython` below is what pins
    # the GO map. Reading this one as covering both is the F2 defect of ZacxDev/cairn#109.
    assert _measured_folds_onto() == {0x0130: ["i"], 0x0131: ["i"],
                                      0x017F: ["s"], 0x212A: ["k"]}, _measured_folds_onto()


def _measured_folds_onto() -> dict[int, list[str]]:
    """Each folding rune's ASCII target(s), measured on the pinned interpreter.

    ⚠ A LIST PER RUNE, NEVER A SINGLE VALUE: a rune folding onto TWO ASCII letters would
    make `reIFoldsOnto` ambiguous, and a dict comprehension would hide that by keeping
    whichever came last.
    """
    return {
        c: [t for t in "abcdefghijklmnopqrstuvwxyz0123456789_"
            if re.compile(re.escape(t), re.I).fullmatch(chr(c))]
        for c in _re_i_folding_runes()
    }


def _re_i_folding_runes() -> tuple[int, ...]:
    """The non-ASCII codepoints `[A-Za-z0-9_]` matches under `re.I`, swept not assumed."""
    klass = re.compile("[A-Za-z0-9_]", re.I)
    return tuple(sorted(c for c in range(0x110000)
                        if c > 127 and klass.fullmatch(chr(c))))


#: The Go source carrying `reIFoldsOnto` — the map the Go sweep's attribution clause
#: actually reads. Not a copy of it: the tests below parse THIS file.
GO_SWEEP = ROOT / "internal" / "store" / "markersweep_test.go"

_GO_MAP_BLOCK = re.compile(r"^var reIFoldsOnto = map\[rune\]rune\{$(?P<body>.*?)^\}$",
                           re.S | re.M)
_GO_MAP_ENTRY = re.compile(
    r"^\s*'(?P<key>\\u[0-9a-fA-F]{4}|\\U[0-9a-fA-F]{8}|[^'\\])'"
    r"\s*:\s*'(?P<val>[^'\\])',\s*(?://.*)?$")


def _parse_go_fold_map(text: str) -> dict[int, str]:
    """`reIFoldsOnto` as the Go file spells it, as {codepoint: ASCII target}.

    Takes the SOURCE TEXT rather than reading the file, so the control below can feed it
    a mutated copy and watch the answer move. A parser whose output nobody has seen
    change is indistinguishable from one returning a constant.
    """
    block = _GO_MAP_BLOCK.search(text)
    assert block, (
        "no `var reIFoldsOnto = map[rune]rune{…}` block found in the Go source. Either "
        "it was renamed or reshaped — in which case this pin is measuring nothing and "
        "must be re-pointed, not deleted — or this regex went stale."
    )
    out: dict[int, str] = {}
    for line in block.group("body").splitlines():
        if not line.strip() or line.lstrip().startswith("//"):
            continue
        entry = _GO_MAP_ENTRY.match(line)
        assert entry, (
            f"unparsed line inside the `reIFoldsOnto` block: {line!r}. This pin reads "
            "the map by shape; a line it cannot read would otherwise be silently "
            "skipped, which is exactly the blindness it exists to close."
        )
        key = entry.group("key")
        cp = int(key[2:], 16) if key.startswith("\\") else ord(key)
        assert cp not in out, f"U+{cp:04X} appears twice in the Go map"
        out[cp] = entry.group("val")
    assert out, "the `reIFoldsOnto` block parsed to NOTHING, which would pass vacuously"
    return out


def test_the_GO_fold_target_map_is_pinned_TWO_WAY_against_CPython():
    """🔴 THE PIN F2 OF ZacxDev/cairn#109 EXISTS FOR: THE GO MAP ITSELF, NOT A COPY.

    `markersweep_test.go`'s comment claimed this file "checks this MAP's targets". It did
    not — it measured CPython against a dict spelled a few lines further up in THIS file,
    so a Go-side drift was invisible to it. MEASURED by mutation before this test existed,
    one mutant per detached `cp -a` copy, each run against BOTH
    `go test ./internal/store/ -run TestTheGoMarkerTranscriptions` and this file:
    retargeting or deleting the U+0130, U+0131 or U+212A entry — SIX mutants — all
    SURVIVED; only the two U+017F mutants were killed, and an unmutated control was green.
    Three of four entries were unexercised under a comment saying they were measured.
    They are unreachable BY CONSTRUCTION, not by accident: `asReadUnderReI` respells
    `open`/`resolved`, and `i`/`k` occur in neither word. With this test, all eight of
    those mutants die here.

    ⚠ TWO-WAY, IN BOTH DIRECTIONS THAT CAN GO WRONG. A rune CPython folds that the Go map
    omits fails here (the Go sweep would then report a real declared-fold divergence as
    undeclared); a rune in the Go map that CPython does not fold fails here too (dead
    weight that reads as coverage). Equality, never containment.
    """
    measured = _measured_folds_onto()
    for cp, targets in measured.items():
        assert len(targets) == 1, (
            f"U+{cp:04X} folds onto {targets} — more than one ASCII target makes "
            "`reIFoldsOnto` ambiguous and the sweep's respelling arbitrary."
        )
    expected = {cp: targets[0] for cp, targets in measured.items()}
    assert _parse_go_fold_map(GO_SWEEP.read_text(encoding="utf-8")) == expected, (
        "`internal/store/markersweep_test.go`'s `reIFoldsOnto` disagrees with what "
        f"CPython {sys.version_info.major}.{sys.version_info.minor} folds. The map is a "
        "claim about the interpreter; fix the Go map, do not relax this."
    )


def test_the_GO_MAP_PARSER_can_see_a_changed_or_missing_entry():
    """🔴 THE POSITIVE AND NEGATIVE CONTROLS ON THE PARSER ABOVE.

    A pin built on a regex over source text is only as good as the regex, and "no match"
    reads identically to "nothing wrong". So: feed it a mutated copy of the REAL file and
    require the answer to MOVE, and feed it one with an entry removed and require that
    entry to be gone. Without this pair, a stale pattern would make the test above pass
    over a map it never read.
    """
    real = GO_SWEEP.read_text(encoding="utf-8")
    parsed = _parse_go_fold_map(real)
    assert parsed[0x017F] == "s", parsed

    retargeted = real.replace(r"'\u017f': 's'", r"'\u017f': 'k'", 1)
    assert retargeted != real, (
        "the U+017F entry is not spelled the way this control expects, so the control "
        "mutated nothing and proves nothing."
    )
    assert _parse_go_fold_map(retargeted)[0x017F] == "k"

    without = "\n".join(ln for ln in real.splitlines()
                        if not ln.lstrip().startswith(r"'\u212a':"))
    assert without != real, "the U+212A entry was not found — this control removed nothing"
    assert 0x212A not in _parse_go_fold_map(without)


#: 🔴 THE `EXTRA` ROWS NO COUNT CAN SEE, PINNED TWO-WAY SO THEY CANNOT BE DELETED
#: IN SILENCE. Every other axis of the corpus is a cross product, so dropping a value
#: moves `corpus_lines` and the checksum. These are literal rows, and at HEAD they
#: produce ZERO divergences — that is the POINT of them, not a reason to prune them:
#: they exist to observe the two class positions `re.IGNORECASE` narrows, and a
#: population whose healthy reading is zero is exactly the population a "prune the dead
#: weight" pass deletes. MEASURED by reverting `internal/store/openness.go` two ways:
#: with `terminatorColon` read ASCII-only for BOTH callers, FOUR of these rows go
#: WIDER than `_MARKER_ANYWHERE`, and they are the only lines in the corpus that do;
#: with the sentence-cased row removed, dropping that function's `ignoreCase` guard
#: produces ZERO divergences over the ENTIRE corpus and the Go sweep stays green.
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
    #: ⚠ THE ONE ROW HERE THAT DIVERGES, so it IS inside the Go sweep's `211` and is not
    #: blind to a count — it is in this ledger because it belongs beside its siblings, and
    #: because what it observes is the sweep's ATTRIBUTION clause rather than a walk.
    #: A folding rune in the marker word AND in the terminator run at once: the first is
    #: the declared residual, the second is the position `_NEAR_MISS_MARKER` reads ASCII.
    #: Respelling the WHOLE line conflated them and reported the divergence as UNDECLARED.
    "- Reſolved ſ: a long s in the marker word AND the terminator run.",
)


def test_the_folding_rune_EXTRA_rows_are_pinned_because_NO_COUNT_CAN_SEE_THEM():
    """🔴 THE ONE POPULATION A PASSING SWEEP IS BLIND TO.

    `internal/store/markersweep_test.go` pins the size of the declared residual, so a
    corpus row that DIVERGES is covered by that number. The first EIGHT rows do not
    diverge at HEAD, so deleting them leaves both counts at 480/211 and every test
    green — while removing the only lines that can catch a revert of
    `terminatorColon`'s fold-awareness, in EITHER direction. The ledger above is the
    guard; this asserts the corpus still carries exactly it.

    ⚠ THE NINTH ROW IS DIFFERENT AND IS LISTED ANYWAY. `- Reſolved ſ: …` DOES diverge,
    so the Go count would see it go; it is pinned here because it is the same
    population and because what it guards is the sweep's ATTRIBUTION clause — delete
    it and `asReadUnderReI` may go back to respelling the whole line with nothing red.
    """
    present = tuple(
        line for line in marker_corpus.EXTRA
        if any(ch in line for ch in "İıſK")
    )
    assert present == FOLDING_RUNE_EXTRA, (
        "the folding-rune rows of `marker_corpus.EXTRA` have moved. All but the last "
        "produce no divergence, so no count in either client can see them go; if a row "
        "is genuinely obsolete, say in the commit which revert stops being observable."
    )


def test_the_hex_atom_class_gains_nothing_under_re_I():
    """The claim `refAtomEnds` makes about its own hex run, measured rather than argued.

    `[0-9a-fA-F]` is read ASCII-only in Go with a comment saying `re.I` adds nothing to
    it. That is only true because no non-ASCII rune folds onto a hex digit — `ſ` folds
    onto `s` and `K` onto `k`, neither of which is in the class.
    """
    klass = re.compile("[0-9a-fA-F]", re.I)
    assert not [c for c in range(0x110000) if c > 127 and klass.fullmatch(chr(c))]
