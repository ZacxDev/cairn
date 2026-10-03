#!/usr/bin/env python3
"""The PYTHON half of the `DecodeUTF8Replace` seam — the DRIFT GUARD.

🔴 WHY THIS EXISTS BESIDE THE GO TEST, AND WHY NEITHER IS ENOUGH ALONE.
`internal/pytext/decodereplace_test.go` replays
`internal/pytext/testdata/decode_replace.json` and asserts Go agrees with it. That
catches a Go-side change — and NOTHING about the Python side, because the fixture was
generated FROM CPython: until this file existed, NOTHING in `pytest tests` read that
fixture at all, so the Go tier replayed committed bytes and nobody ever re-asked the
interpreter. MEASURED as a pair in a scratch copy of the tree, each mutation isolated to
ONE character: editing `internal/store/testdata/citation_ids.json`'s first
`citation_id` turned its drift guard in `tests/test_citation_ids.py` RED — the positive
control — while THIS file stayed green; editing this fixture's `sweep.digest` and one
`output_hex` did it the other way round. ⚠ The scope of that pair is
`pytest tests/test_pytext_decode_replace.py tests/test_citation_ids.py`, 14 tests, not
the whole suite — stated because a claim about a two-file run is not a claim about
`pytest tests`. The whole-suite measurement is the separate one above: with this file
ABSENT and the fixture edited, a full `pytest tests` reported no failure about it.

🔴 AND THE THING IT RECORDS IS A PROPERTY OF THE INTERPRETER, WHICH IS WHY A ONE-WAY
SNAPSHOT IS NOT ENOUGH. The fixture is CPython's own
`bytes.decode("utf-8", errors="replace")` — the maximal-subpart rule of Unicode
TR#36, which no one can transcribe from prose. `AGENTS.md` records that a bare
`pkgs.python3` already followed nixpkgs to 3.14 once and shipped an interpreter
nothing here had run the suite under. If a subpart boundary moved under a new
interpreter, the Go test would stay green against a stale fixture while the pod and
the Python client disagreed about a citation id — the exact divergence class this
fixture's own generator exists to close.

So this regenerates the fixture from the LIVE implementation on every pytest run and
compares BYTES, which makes the pair two-sided:

  * Go moves  ⇒ `decodereplace_test.go` goes red;
  * CPython moves without regenerating ⇒ THIS goes red;
  * both move together ⇒ the regeneration shows as a reviewable diff.

Same shape as `tests/test_citation_ids.py`'s drift guard and
`tests/test_reader_fixtures.py`, and named here so nobody takes either half for the
whole.

⚠ WHAT THIS DOES NOT SEE: it compares the generator's output against the committed
file, so a defect in the ENUMERATION — a group that stopped yielding what it means to
yield — moves both sides together and reads as a clean regeneration. The Go side's
per-group counts and its `>= 80000` floor are what notice that; the floors below are
the minimum needed to keep the byte comparison from being a comparison of nothing.
"""
from __future__ import annotations

import json
import subprocess
import sys
from pathlib import Path

import pytest

REPO = Path(__file__).resolve().parents[1]
FIXTURE = REPO / "internal" / "pytext" / "testdata" / "decode_replace.json"
GENERATOR = REPO / "tests" / "pytext_decode_replace.py"


def _regenerated_bytes() -> bytes:
    """The generator's CURRENT output. Run as a SUBPROCESS rather than imported so
    this exercises the exact command the generator's docstring tells a human to run —
    an in-process call could pass while the script itself was broken."""
    proc = subprocess.run(
        [sys.executable, str(GENERATOR)],
        capture_output=True, timeout=300, cwd=str(REPO),
    )
    assert proc.returncode == 0, (
        f"tests/pytext_decode_replace.py exited {proc.returncode}, so the fixture "
        "cannot be regenerated and this seam measures nothing:\n"
        f"{proc.stderr.decode('utf-8', 'replace')}")
    return proc.stdout


def test_the_committed_fixture_matches_the_LIVE_cpython_decoder():
    """🔴 THE DRIFT GUARD. A stale fixture is a failure, not a weaker comparison."""
    assert FIXTURE.is_file(), (
        f"{FIXTURE} is missing, so `internal/pytext`'s decode test has nothing to "
        "replay and both halves of this seam measure nothing")
    committed = FIXTURE.read_bytes()
    fresh = _regenerated_bytes()
    if committed == fresh:
        return
    # A 24 KB byte diff is unreadable, so localise it: name the top-level keys that
    # moved and, inside `cases`, the case names. The remedy is the same either way.
    detail = []
    try:
        c, f = json.loads(committed), json.loads(fresh)
        for key in sorted(set(c) | set(f)):
            if c.get(key) != f.get(key):
                detail.append(f"  `{key}` differs")
        cc = {x["name"]: x for x in c.get("cases", [])}
        ff = {x["name"]: x for x in f.get("cases", [])}
        for name in sorted(set(cc) | set(ff)):
            if cc.get(name) != ff.get(name):
                detail.append(f"    case `{name}`: {cc.get(name)} -> {ff.get(name)}")
        for key in ("count", "digest", "sample_stride"):
            if c.get("sweep", {}).get(key) != f.get("sweep", {}).get(key):
                detail.append(f"    sweep.{key}: "
                              f"{c.get('sweep', {}).get(key)!r} -> "
                              f"{f.get('sweep', {}).get(key)!r}")
    except ValueError as exc:
        detail.append(f"  (one side is not JSON: {exc})")
    pytest.fail(
        "the committed decode fixture is NOT what the live CPython decoder produces, "
        "so `internal/pytext`'s differential test is comparing the port against a "
        "snapshot of a DIFFERENT interpreter. REGENERATE AND DIFF — never hand-edit:\n"
        "\n    python3 tests/pytext_decode_replace.py > "
        "internal/pytext/testdata/decode_replace.json\n\n"
        f"committed {len(committed)} bytes, regenerated {len(fresh)} bytes\n"
        + "\n".join(detail[:40]))


#: The counts the committed fixture carries, EXACT rather than a floor, taken on this
#: tree. A floor pins shrink only, so adding a case or widening a group would prompt
#: nobody — and `internal/pytext/decodereplace_test.go` already owns the floors
#: (`>= 20` cases, `>= 80000` sweep inputs, `>= 100` samples), which is a different
#: claim from "the corpus is still THIS corpus".
FIXTURE_CASES = 21
FIXTURE_SWEEP_COUNT = 82176
FIXTURE_SAMPLES = 123


def test_the_fixture_is_not_vacuous():
    """A floor, before the equality above is trusted. An empty document regenerates
    to an empty document and would make the drift guard pass forever."""
    doc = json.loads(FIXTURE.read_bytes())
    assert len(doc["cases"]) == FIXTURE_CASES, (
        f"{len(doc['cases'])} named case(s), expected exactly {FIXTURE_CASES}. If you "
        "ADDED one, bump this constant — and check the `>= 20` floor in "
        "internal/pytext/decodereplace_test.go is still below it.")
    assert doc["sweep"]["count"] == FIXTURE_SWEEP_COUNT, (
        f"the sweep claims {doc['sweep']['count']} input(s), expected exactly "
        f"{FIXTURE_SWEEP_COUNT}. A count that moved means the ENUMERATION drifted, "
        "which the drift guard above cannot tell apart from a decoder change: it "
        "regenerates with the SAME enumeration, so both sides move together.")
    assert len(doc["sweep"]["samples"]) == FIXTURE_SAMPLES, (
        f"{len(doc['sweep']['samples'])} sample(s), expected exactly "
        f"{FIXTURE_SAMPLES} — the strided samples are what a red Go digest quotes.")
