"""Pin the ORACLE's provenance reader against the Go port's, case for case.

🔴 WHY THIS FILE EXISTS: WITHOUT IT THE PYTHON PROVENANCE PATH IS UNREACHABLE AND
UNCOMPARED, WHICH IS THE ONE COMBINATION WITH NO UPSIDE. Round 0 of PR #148's audit found
it and the finding is confirmed by measurement, not by reading:

  * NO Python code reads `Requirement.provenance`. `read_entry` consults `is_open`/`is_met`
    only; `listing_line` consults the two counts only; there is no Python browser surface
    (`internal/ui` is Go-only); and `server/server.py` reimplements no rendering. Measured
    with a positive control — the same search finds SIX Go consumers of `Provenance`.
  * `internal/report` renders provenance NOWHERE, in either language. So it reaches no
    stdout byte, which means **no parity case and no reader fixture can ever see a
    provenance divergence** — they compare rendered bytes, and provenance is not among
    them. That is the gap this file closes, and it is exactly the class `AGENTS.md` names:
    "two defects shipped in the first Go commit with all four CI jobs green, both DECODING
    differences."

So the hazard is not dead code for its own sake. It is that the two implementations can
drift on provenance with **every gate green** — feed `- OPEN: (operators) x` to both, let
the Python side ever accept a prefix, and nothing anywhere goes red.

⚠ THE SHAPE IS THE REPO'S OWN, NOT A NEW ONE. `internal/envalias` and `lib/env_aliases.py`
are two spellings of one rule "pinned against each other by `tests/test_env_aliases.py`"
because `packages.cairn` cannot import `internal/`. This is that pattern applied to
provenance, and the alternative considered was DELETING the Python path — rejected only
because the oracle is still shipped and still generates the reader fixture, so a reader
that silently lacks a field the port has is its own divergence.

🔴 THE TABLE IS THE GO TEST'S TABLE. Every row below is a row of
`internal/store/requirements_test.go`'s `TestProvenanceIsTheWholeParenthesizedWord` and
`TestALineWithNoParsedMarkerHasNoProvenance`. A DIFFERENT table would make this file a
second opinion rather than a pin: two tables that disagree about which inputs matter cannot
catch the implementations disagreeing about an input neither table names.
"""
from __future__ import annotations

import sys
from pathlib import Path

import pytest

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT / "lib"))

import subsystem_resolver as sr  # noqa: E402

#: The Go test's own rows, verbatim. `None` is the oracle's spelling of
#: `store.ProvenanceAbsent` — the idioms differ, the ANSWERS must not.
CASES: tuple[tuple[str, str | None], ...] = (
    # The two positive controls: without these the whole table passes by returning None.
    ("- OPEN: (operator) a real one", sr.PROVENANCE_OPERATOR),
    ("- OPEN: (inferred) a real one", sr.PROVENANCE_INFERRED),
    # The separator is optional, so a missing space must not change the answer.
    ("- OPEN:(operator) no space", sr.PROVENANCE_OPERATOR),
    # Whole word, not a prefix — the closing paren is required.
    ("- OPEN: (operators) a plural", None),
    # Case-sensitive.
    ("- OPEN: (Operator) capitalised", None),
    # No parsed marker ⇒ no provenance, however it is spelled.
    ("- (operator) no marker at all", None),
    ("- (operator) OPEN: the parenthetical came first", None),
    ("- 2000-07-08 OPEN: (operator) a near-miss marker, the date has no colon", None),
)


@pytest.mark.parametrize("line,want", CASES)
def test_the_oracle_reads_provenance_exactly_as_the_port_does(
    line: str, want: str | None
) -> None:
    assert sr._bullet_provenance(line) == want, (
        f"the oracle answered {sr._bullet_provenance(line)!r} for {line!r}; the Go port "
        f"answers {want!r}. Provenance reaches no rendered byte, so NO parity case and no "
        f"reader fixture can see this — this test is the only thing that can."
    )


def test_the_table_carries_BOTH_a_positive_and_a_negative_row() -> None:
    """The instrument's own control: a table of only-None rows would pass against a
    reader that always returns None, which is the failure mode of a pin over a field
    nothing renders."""
    answers = {want for _, want in CASES}
    assert sr.PROVENANCE_OPERATOR in answers, "no operator row — a positive control is missing"
    assert sr.PROVENANCE_INFERRED in answers, "no inferred row — a positive control is missing"
    assert None in answers, "no absent row — the narrowing is unpinned"


def test_provenance_survives_a_trailing_session_attribution() -> None:
    """A SUFFIX must not reach the provenance read.

    The reader fixture's boundary row carries a `[cairn: actor/session]` trailer, so this
    is the shape a real appended bullet has. Provenance is read immediately AFTER the
    marker, so anything at the END of the line is irrelevant — asserted rather than assumed,
    because "irrelevant" is a claim about where the read stops.
    """
    trailer = " [cairn: fixture-actor/sess-0000000000000001]"
    assert sr._bullet_provenance(
        "- OPEN: (operator) a requirement" + trailer
    ) == sr.PROVENANCE_OPERATOR
    assert sr._bullet_provenance("- OPEN: a requirement" + trailer) is None


def test_a_requirement_carries_its_provenance_through_parse_requirements() -> None:
    """The field is reachable through the PUBLIC entry point, not only the private reader.

    🔴 THIS IS THE HALF THAT MAKES THE FIELD MORE THAN A DTO DECLARATION. `claude/RULES.md`:
    "a field that exists in a DTO is not a guard — only a BRANCH on it is." Nothing in the
    oracle branches on provenance today, so this asserts the value at least ARRIVES —
    and names that no production consumer reads it.
    """
    body = "\n".join(
        [
            "- OPEN: (operator) the operator asked for this",
            "- OPEN: (inferred) an agent inferred this",
            "- OPEN: nobody recorded who asked",
        ]
    )
    reqs = sr.parse_requirements(body)
    assert [r.provenance for r in reqs] == [
        sr.PROVENANCE_OPERATOR,
        sr.PROVENANCE_INFERRED,
        None,
    ]
    # And the openness vocabulary still comes through, so this is a requirement and not
    # merely a provenance carrier.
    assert all(r.is_open for r in reqs)
    assert not any(r.is_met for r in reqs)
