"""The reader fixture is GENERATED, and this is what stops it going stale.

🔴 A SNAPSHOT NOBODY REGENERATES STOPS BEING A DIFFERENTIAL TEST. `internal/report`'s
Go test replays `testdata/reader_fixtures.json` and compares its own bytes against the
ORACLE's — which is only a measurement while the file still holds what the oracle
produces TODAY. The moment the reader changes and the fixture does not, that test is
comparing the port against a snapshot of an older implementation and calling the
agreement byte-identity.

So this file regenerates the whole fixture and diffs it against what is committed, the
same way `TestTheGoldensAreGenerated` does for the HTTP corpus. If the two disagree, the
committed fixture is not what the generator produces, and the failure names the one
command that fixes it.
"""
from __future__ import annotations

import difflib
import json
import sys
import tempfile
from pathlib import Path

import pytest

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT / "tests"))
sys.path.insert(0, str(ROOT / "lib"))

import entry_shape  # noqa: E402
import reader_fixtures  # noqa: E402


@pytest.fixture(scope="module")
def regenerated() -> dict:
    with tempfile.TemporaryDirectory(prefix="cairn-reader-fixtures-") as td:
        return reader_fixtures.generate(Path(td) / "store")


@pytest.fixture(scope="module")
def committed() -> dict:
    return json.loads(reader_fixtures.FIXTURE.read_text(encoding="utf-8"))


def test_the_committed_fixture_is_what_the_generator_produces(regenerated, committed):
    want = reader_fixtures.dumps(regenerated)
    have = reader_fixtures.FIXTURE.read_text(encoding="utf-8")
    if want == have:
        return
    diff = "\n".join(
        difflib.unified_diff(
            have.splitlines(), want.splitlines(),
            fromfile="committed", tofile="regenerated", lineterm="", n=2,
        )[:80]
    )
    pytest.fail(
        "the committed reader fixture is NOT what the generator produces, so the Go "
        "renderer's differential test is comparing against a snapshot of an older "
        "reader. Regenerate it:\n\n    python3 tests/reader_fixtures.py generate\n\n"
        + diff
    )


def test_generating_does_not_leave_the_HOST_SEAM_REBOUND(regenerated):
    """🔴 THE GENERATOR PATCHES A LIBRARY GLOBAL, AND IT MUST PUT IT BACK.

    Measured: the first version did not, and because the fixture above runs at module
    import, SIX tests in two other modules failed — every one of them asserting that a
    reader's output names THIS machine, on a process where this module had silently
    replaced the machine. The failure did not look like contamination; it looked like the
    reader had stopped printing the host.

    `regenerated` is requested so the generation has definitely already happened; without
    it this test would pass by running first.
    """
    assert entry_shape.store_host() != reader_fixtures.HOST
    import subsystem_recall as rc

    assert rc.store_host() != reader_fixtures.HOST
    # And the positive control: the fixture really does carry the patched host, so the two
    # assertions above are about a value that WAS in force rather than one that never was.
    assert regenerated["host"] == reader_fixtures.HOST
    assert any(
        reader_fixtures.HOST in "\n".join(case["expect"]["text_lines"])
        for case in regenerated["cases"]
    )


def test_the_warning_sentence_is_the_READERS_own_and_not_a_second_spelling(regenerated):
    """🔴 THE ONE STRING IN THIS FIXTURE THAT IS REBUILT RATHER THAN CAPTURED.

    `_exit_for` PRINTS its sentence to stderr, so the generator cannot read it back off a
    return value and spells it again. That is a predicate at two sites, which is wrong at
    one of them — so the two are compared here by CAPTURING the real function's stderr.
    Without this, a fixture whose wording drifted would make the Go side agree with the
    fixture and disagree with the reader.
    """
    import io
    import contextlib

    import subsystem_recall as rc

    unreadable = [
        case for case in regenerated["cases"]
        if case["expect"]["status"] in rc.UNREADABLE_STATUSES
    ]
    # The positive control on this test itself: a zero here would make it vacuous, and
    # `*-unreachable` is exactly the status a fixture is most likely to stop reaching.
    assert unreadable, "no fixture case reaches an unreadable status"

    for case in unreadable:
        # The label is the report's own: `<scope>/` for a recall, the searched-scope label
        # for a search. Both are single-scope rows here, so the two coincide — which is why
        # the assertion below reads the sentence rather than reconstructing the label.
        captured = io.StringIO()
        with contextlib.redirect_stderr(captured):
            code = rc._exit_for(
                case["expect"]["status"],
                _label_for(case),
                # The COUNT is what the sentence interpolates, so a list of the right
                # length is the whole input this needs.
                [None] * _malformed_count(case),
            )
        assert code == case["expect"]["exit"], case["id"]
        assert captured.getvalue().rstrip("\n") == case["expect"]["warning"], case["id"]


def _label_for(case: dict) -> str:
    """`<scope>/` — every unreadable row in this fixture names ONE scope."""
    return f"{case['scope']}/"


def _malformed_count(case: dict) -> int:
    """How many of the declared entries live in the row's scope and are malformed.

    Derived from the rendered text rather than re-parsed out of the world: the sentence
    the test is checking interpolates this number, and the MALFORMED block above it lists
    one row per file, so counting those rows is reading the report's own answer instead of
    re-deriving it from the fixture's inputs.
    """
    return sum(
        1 for line in case["expect"]["text_lines"]
        if line.strip().startswith("malformed index entry `")
    )


def test_the_fixture_world_is_synthetic_and_dated_year_2000(committed):
    """The repository is PUBLIC and was extracted from a private one. Every timestamp in
    the world is an offset from 2000-01-01, which is what `tests/leakscan.py` allows and
    what makes a real date in here unambiguous."""
    year_2001_ns = 978307200 * 1_000_000_000
    for entry in committed["entries"]:
        assert 946684800 * 1_000_000_000 <= entry["mtime_ns"] < year_2001_ns, entry["path"]
    for case in committed["cases"]:
        body = "\n".join(case["expect"]["text_lines"])
        # The only dates the bodies carry are the ones the entries declare.
        assert "2026-" not in body and "2025-" not in body, case["id"]
