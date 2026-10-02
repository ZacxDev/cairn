#!/usr/bin/env python3
"""The PYTHON half of the citation-id seam.

🔴 WHY THIS EXISTS BESIDE THE GO TEST, AND WHY NEITHER IS ENOUGH ALONE.
`internal/store/citationid_test.go` replays `internal/store/testdata/citation_ids.json`
and asserts Go agrees with it. That catches a Go-side change — and NOTHING about the
Python side, because the fixture was generated FROM Python: if `citation_id` changes and
nobody regenerates, the fixture still records the OLD answers and the Go test keeps
agreeing with a file that no longer describes the oracle. The seam would read green
while the two implementations disagreed.

So this file regenerates the fixture from the LIVE implementation on every pytest run
and compares, which makes the pair two-sided:

  * Go moves  ⇒ `citationid_test.go` goes red;
  * Python moves without regenerating ⇒ THIS goes red;
  * both move together ⇒ the regeneration shows as a reviewable diff.

That is the arrangement `tests/marker_corpus.py` + `tests/test_marker_oracle_sweep.py`
+ `internal/store/markersweep_test.go` already use; this is the same instrument for a
different function, and it is named here so nobody takes either half for the whole.

⚠ WHAT NEITHER HALF SEES: the bodies are hand-written, so nothing here speaks for the
LIVE corpus, and no surface PRINTS an id yet — this PR adds the derivation and the
position, not the rendering. The differential reader fixture is what compares rendered
bytes once it does.
"""
from __future__ import annotations

import json
import subprocess
import sys
from pathlib import Path

import pytest

REPO = Path(__file__).resolve().parents[1]
FIXTURE = REPO / "internal" / "store" / "testdata" / "citation_ids.json"
GENERATOR = REPO / "tests" / "citation_ids.py"

sys.path.insert(0, str(REPO / "lib"))

from subsystem_resolver import parse_journal_bullets  # noqa: E402


def _regenerated() -> dict:
    """The generator's CURRENT output, as data. Run as a subprocess rather than
    imported so this exercises the exact command the docstring tells a human to
    run — an in-process call could pass while the script itself was broken."""
    proc = subprocess.run(
        [sys.executable, str(GENERATOR)],
        capture_output=True, text=True, timeout=120, cwd=str(REPO),
    )
    assert proc.returncode == 0, (
        f"tests/citation_ids.py exited {proc.returncode}, so the fixture cannot be "
        f"regenerated and this seam measures nothing:\n{proc.stderr}")
    return json.loads(proc.stdout)


def test_the_committed_fixture_matches_the_LIVE_python_implementation():
    """🔴 THE DRIFT GUARD. A stale fixture is a failure, not a weaker comparison —
    the same rule `internal/report/testdata/reader_fixtures.json` carries."""
    assert FIXTURE.is_file(), (
        f"{FIXTURE} is missing, so `internal/store`'s citation test has nothing to "
        "replay and both halves of this seam measure nothing")
    committed = json.loads(FIXTURE.read_text(encoding="utf-8"))
    fresh = _regenerated()
    assert committed == fresh, (
        "the committed fixture no longer matches what the live Python implementation "
        "produces. REGENERATE AND DIFF — never hand-edit:\n"
        "  python3 tests/citation_ids.py > internal/store/testdata/citation_ids.json\n"
        "If an id moved, that is a CONTRACT change: every previously printed id stops "
        "resolving, so say so in the commit rather than letting the diff speak.")


def test_the_fixture_is_not_vacuous():
    """A floor, before any equality above is trusted. An empty `cases` list compares
    equal to an empty regeneration and would make the drift guard pass forever."""
    doc = json.loads(FIXTURE.read_text(encoding="utf-8"))
    cases = doc["cases"]
    assert len(cases) >= 9, f"only {len(cases)} case(s) — something truncated the fixture"
    bullets = sum(len(c["bullets"]) for c in cases)
    assert bullets >= 12, f"only {bullets} bullet(s) across {len(cases)} cases"
    # Every id present must be 8 lowercase hex. Asserted over the FIXTURE as well as
    # in Go, because a generator bug would otherwise be recorded as the oracle.
    for c in cases:
        for b in c["bullets"]:
            cid = b["citation_id"]
            assert len(cid) == 8 and all(ch in "0123456789abcdef" for ch in cid), (
                f"{c['name']}: {cid!r} is not 8 lowercase hex characters")


def test_two_bullets_with_IDENTICAL_opening_lines_get_DIFFERENT_ids():
    """🔴 THE CASE THAT KILLS THE PLAUSIBLE SIMPLIFICATION, asserted directly rather
    than only via the fixture. Hashing `lines[0]` is the obvious shortcut and it
    COLLIDES here — measured: under that mutant both bullets returned `3edbbdf1`."""
    bullets = parse_journal_bullets("- same opening\n  tail A\n- same opening\n  tail B\n")
    assert len(bullets) == 2, bullets
    assert bullets[0].first_line == bullets[1].first_line, "the fixture premise moved"
    assert bullets[0].citation_id != bullets[1].citation_id, (
        "two bullets differing only in a continuation line share an id, so the hash "
        "is reading the opening line alone")


def test_start_line_points_at_the_OPENING_line_in_the_BODY():
    """Including the two shapes a re-implementation gets wrong: a dropped preamble
    shifts the first bullet off 0, and a `- ` inside a FENCE is not a bullet."""
    body = "preamble nobody can address\n- first\n  ```\n  - not a bullet\n  ```\n- second\n"
    bullets = parse_journal_bullets(body)
    lines = body.splitlines()
    assert len(bullets) == 2, [b.lines for b in bullets]
    assert bullets[0].start_line == 1, "the dropped preamble must shift the first bullet"
    assert bullets[1].start_line == 5, "the fenced dash must not be counted as a bullet"
    # The property that makes the field usable: the line it names IS the opening line.
    for b in bullets:
        assert lines[b.start_line] == b.first_line, (
            f"start_line {b.start_line} names {lines[b.start_line]!r}, not {b.first_line!r}")


def test_start_line_is_NOT_a_length():
    """The hazard the field's docstring names: trailing blanks are stripped from
    `lines` but remain in the body, so `start_line + len(lines)` is not the next
    bullet's position. A caller that added them would annotate the wrong line."""
    bullets = parse_journal_bullets("- first\n\n\n- second\n")
    assert len(bullets) == 2
    assert bullets[0].start_line == 0 and len(bullets[0].lines) == 1
    assert bullets[1].start_line == 3, bullets[1].start_line
    assert bullets[0].start_line + len(bullets[0].lines) != bullets[1].start_line, (
        "this case no longer demonstrates the gap, so the docstring's warning is "
        "unguarded — pick a body that still does")


@pytest.mark.parametrize("body", [
    "- plain\n",
    "- 🔴 unicode émoji — en dash\n",
    "- wrapped\n  tail\n",
])
def test_the_id_is_STABLE_across_calls(body):
    """It is a pure function of the bytes, so two parses of one body agree. Cheap, and
    it is the property every downstream use depends on."""
    a = parse_journal_bullets(body)[0].citation_id
    b = parse_journal_bullets(body)[0].citation_id
    assert a == b == parse_journal_bullets(body)[0].citation_id
