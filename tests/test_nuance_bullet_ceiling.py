"""The nuance-ceiling badge, pinned ACROSS THE TWO RENDERERS.

🔴 WHY TWO SPELLINGS OF ONE NUMBER EXIST, AND WHY A GATE IS THE ANSWER RATHER THAN A FIX.
`packages.cairn` installs the Python client and `lib/` under `libexec` and nothing else, so
`lib/subsystem_recall.py` CANNOT import `internal/report`. That is packaging, not a design
choice anybody may undo here — the identical constraint `tests/test_env_aliases.py` exists
under, and it names `tests/test_flake_image_matches_dockerfile.py` as the house precedent
for the shape. This file is that instrument for the index row's size badge.

## The hazard it closes, stated as a mechanism

Pod and CLI run ONE renderer (`internal/report`); `packages.cairn` runs the OTHER
(`lib/subsystem_recall.py`), and `AGENTS.md` requires the two to agree BYTE-FOR-BYTE until
the oracle is deleted at P8. A ceiling that moved on one side only does not error, does not
drop an entry and does not change an exit code — it changes ONE badge on SOME rows, which
reads as a stale cache. That is the exact drift `tests/parity/` was built for, and this file
is the cheaper, earlier half of it: parity needs a live pod and a store that happens to hold
an oversized entry, where this needs neither.

## What it pins, and what it structurally cannot see

It pins the two CONSTANTS equal, the two rendered BADGE STRINGS equal as whole normalised
strings, and the PREDICATE's exact boundary on the Python side (at the ceiling → silent, one
over → loud).

⚠ IT READS THE GO SIDE AS TEXT, so it cannot see a Go-side wiring mistake that still
produces a well-formed badge — the emitter moved to a different branch, say, or compared
against the wrong field. Two other things close that and neither is here:
`internal/report`'s own fixture replay compares the Go renderer's BYTES against the oracle's
over a world holding both sides of the boundary, and `tests/parity/harness.py` runs both
real clients over one cache root. Said plainly rather than left to be discovered, because a
reader who took this file for full coverage would stop looking.
"""

from __future__ import annotations

import dataclasses
import re
import sys
from pathlib import Path

import pytest

REPO = Path(__file__).resolve().parents[1]
GO_SOURCE = REPO / "internal" / "report" / "text.go"

sys.path.insert(0, str(REPO / "lib"))

import subsystem_recall as rc  # noqa: E402


# --- reading the GO spelling -------------------------------------------------

_CEILING = re.compile(r"^const NuanceBulletCeiling = (\d+)$", re.MULTILINE)
#: The emitting line, as the Go source actually writes it. It is matched as a WHOLE
#: statement rather than by hunting for the prose: the point of the comparison is the
#: literal fragments AROUND the interpolation, and a looser pattern would match the
#: identical sentence sitting in the doc comment above it.
_BADGE = re.compile(
    r'badges = append\(badges, '
    r'"((?:[^"\\]|\\.)*)"\+strconv\.Itoa\(NuanceBulletCeiling\)\+"((?:[^"\\]|\\.)*)"\)'
)
_PREDICATE = re.compile(r"if entry\.BulletCount (>=?) NuanceBulletCeiling \{")


@pytest.fixture(scope="module")
def go_source() -> str:
    return GO_SOURCE.read_text(encoding="utf-8")


def _go_ceiling(src: str) -> int:
    m = _CEILING.search(src)
    assert m is not None, (
        "no `const NuanceBulletCeiling = <int>` in internal/report/text.go — the Go side of "
        "this pin has moved or been renamed, and a pin that cannot find its operand must "
        "refuse rather than pass"
    )
    return int(m.group(1))


def _go_badge(src: str) -> str:
    """The Go badge, RESOLVED — the two literals with the constant substituted in.

    ⚠ THE SOURCE IS MATCHED AS WRITTEN, NOT WHITESPACE-STRIPPED. A first draft collapsed
    spaces before matching, which silently ate the ones INSIDE the literals (`"⚠ OVER "`
    became `"⚠OVER"`) and would have compared a badge neither renderer emits.
    """
    m = _BADGE.search(src)
    assert m is not None, (
        "could not find the badge's emitting statement in internal/report/text.go. It is "
        "matched as a whole statement on purpose; if gofmt or an edit reshaped it, update "
        "the pattern rather than loosening it to a substring search"
    )
    return m.group(1) + str(_go_ceiling(src)) + m.group(2)


# --- the instrument's own controls -------------------------------------------
#
# 🔴 A REASSURING PASS HERE IS INDISTINGUISHABLE FROM A REGEX THAT MATCHES NOTHING, because
# every assertion below compares two values this file EXTRACTS. So the extractors are
# exercised against a deliberately mutated source first, and the answer has to MOVE.


class TestTheExtractorsAreInstruments:
    def test_it_finds_a_ceiling_at_all(self, go_source: str) -> None:
        assert _go_ceiling(go_source) > 0

    def test_it_finds_the_badge_at_all(self, go_source: str) -> None:
        assert "nuance" in _go_badge(go_source)

    def test_a_mutated_ceiling_moves_the_answer(self, go_source: str) -> None:
        mutated = go_source.replace(
            "const NuanceBulletCeiling = 30", "const NuanceBulletCeiling = 29"
        )
        assert mutated != go_source, "the mutation did not apply — the control is inert"
        assert _go_ceiling(mutated) == 29
        # …and the rendered badge follows it, which is the whole reason the Go side
        # interpolates the constant instead of spelling the number.
        assert "29" in _go_badge(mutated)

    def test_a_mutated_badge_text_moves_the_answer(self, go_source: str) -> None:
        mutated = go_source.replace(" nuance — prune or split", " nuance — SPLIT IT")
        assert mutated != go_source, "the mutation did not apply — the control is inert"
        assert _go_badge(mutated) != _go_badge(go_source)

    def test_a_hardcoded_bar_is_visible_to_the_extractor(self, go_source: str) -> None:
        """🔴 THE FAILURE THE INTERPOLATION RULE EXISTS FOR, as a positive control.

        A Go side that spelled the number instead of interpolating it is exactly the
        silent drift this pin is about — the badge would go on PRINTING a bar the
        predicate no longer uses. The emitting statement then stops matching, and the
        extractor REFUSES rather than quietly comparing a stale literal.
        """
        mutated = go_source.replace(
            '"⚠ OVER "+strconv.Itoa(NuanceBulletCeiling)+" nuance — prune or split"',
            '"⚠ OVER 30 nuance — prune or split"',
        )
        assert mutated != go_source, "the mutation did not apply — the control is inert"
        with pytest.raises(AssertionError):
            _go_badge(mutated)


# --- the pin itself ----------------------------------------------------------


class TestTheTwoRenderersAgree:
    def test_the_ceiling_is_identical(self, go_source: str) -> None:
        assert _go_ceiling(go_source) == rc.NUANCE_BULLET_CEILING, (
            "the nuance-bullet ceiling disagrees between the two renderers. They render the "
            "same index row for the same entry and must agree byte-for-byte until P8 "
            "retires `packages.cairn`; move both or neither."
        )

    def test_the_rendered_badge_is_byte_identical(self, go_source: str) -> None:
        """🔴 THE WHOLE NORMALISED STRING, NOT A KEYWORD.

        When the artifact under test is prose, a guard on words is walkable by rewording —
        and a reworded badge on one side only IS the drift. A cosmetic reword therefore
        fails this test, and that cost is paid on purpose for a machine-readable claim.
        """
        py = _py_badge()
        assert _go_badge(go_source) == py, (
            f"the rendered badge differs: Go {_go_badge(go_source)!r} vs Python {py!r}"
        )

    def test_both_sides_are_STRICTLY_over(self, go_source: str) -> None:
        """`OVER` is a word, and `>=` would make it false at exactly one count.

        Pinned on the Go side by reading the operator, and on the Python side
        behaviourally by `TestThePythonBoundary` below.
        """
        m = _PREDICATE.search(go_source)
        assert m is not None, "no `if entry.BulletCount <op> NuanceBulletCeiling` found"
        assert m.group(1) == ">", (
            "the Go predicate is not strictly greater-than, so an entry AT the ceiling "
            "renders a badge reading `OVER <ceiling>` about itself"
        )


def _py_badge() -> str:
    """The oracle's badge, taken from its RENDERED OUTPUT rather than retyped here.

    🔴 A LITERAL IN THIS FILE WOULD BE A THIRD SPELLING, and a third spelling is a third
    thing that can drift — it would agree with both renderers on the day it is typed and
    nothing would assert it still does. So it is rendered out of `listing_line` and the
    badge is sliced off the end.
    """
    line = rc.listing_line(_entry(rc.NUANCE_BULLET_CEILING + 1), width=4)
    # The badge separator is three spaces and this entry carries no OTHER badge, so
    # everything after the last three-space run is the badge and nothing else.
    badge = line.rsplit("   ", 1)[-1]
    assert badge != line, "the row did not render a badge at all, so there is nothing to pin"
    return badge


def _entry(bullet_count: int) -> rc.RecalledEntry:
    """A minimal entry whose ONLY interesting property is its bullet count.

    ⚠ EVERY OTHER POPULATION IS ZERO ON PURPOSE. A fixture that also declared an open
    bullet would produce a two-badge run, and a slice taking "the last badge" could then
    pass while reading the wrong one.
    """
    return rc.RecalledEntry(
        ref="r",
        filename="r.md",
        sensitivity="public",
        declared_sensitivity=None,
        sections={},
        bullet_count=bullet_count,
        open_count=0,
        near_miss_count=0,
        unverifiable_count=0,
        requirements_open=0,
        requirements_met=0,
        mtime=0.0,
        missing_sections=(),
        tasks=(),
        tags=(),
    )


class TestThePythonBoundary:
    """The oracle's predicate, measured at BOTH sides of its edge.

    🔴 THE SILENT HALF IS THE LOAD-BEARING ONE. A test that only asserts the badge can
    appear passes for a predicate that fires one count too early, which is the mutation
    this pair exists to kill — and `OVER <ceiling>` printed about an entry that is not
    over it is a comment the implementation contradicts.
    """

    def test_one_under_the_ceiling_is_silent(self) -> None:
        assert "OVER" not in rc.listing_line(_entry(rc.NUANCE_BULLET_CEILING - 1), width=4)

    def test_AT_the_ceiling_is_silent(self) -> None:
        assert "OVER" not in rc.listing_line(_entry(rc.NUANCE_BULLET_CEILING), width=4)

    def test_one_OVER_the_ceiling_fires(self) -> None:
        assert "OVER" in rc.listing_line(_entry(rc.NUANCE_BULLET_CEILING + 1), width=4)

    def test_the_badge_names_the_CEILING_and_not_the_count(self) -> None:
        """🔴 ASSERT THE STATE, NOT A WORD ANOTHER FEATURE CAN SPELL.

        The row already carries the entry's own count three columns to the left, so a
        badge echoing it would be redundant — and a guard grepping for the count would
        be satisfied by that leftmost column rather than by the badge. The number in the
        badge must be the CEILING, and at a count of ceiling+1 the two differ, which is
        what makes the distinction observable at all.
        """
        count = rc.NUANCE_BULLET_CEILING + 1
        badge = rc.listing_line(_entry(count), width=4).rsplit("   ", 1)[-1]
        assert str(rc.NUANCE_BULLET_CEILING) in badge
        assert str(count) not in badge, (
            "the badge echoes the entry's own bullet count, which the row already prints"
        )

    def test_the_badge_is_LAST_in_a_multi_badge_run(self) -> None:
        """Position, pinned against a row carrying every other badge.

        ⚠ It is advisory, where the badges to its left report unfinished business, so a
        `🔴` must not be pushed right by it.
        """
        entry = _entry(rc.NUANCE_BULLET_CEILING + 1)
        entry = dataclasses.replace(entry, open_count=2, tasks=("github:example-org/x#1",))
        line = rc.listing_line(entry, width=4)
        assert line.rsplit("   ", 1)[-1].startswith("⚠ OVER ")
        assert line.index("OPEN") < line.index("OVER")
        assert line.index("ref") < line.index("OVER")
