"""The synthetic transcript world (S0 of `claudedocs/plan-cairn-plugins.md`) is FRESH and SYNTHETIC.

🔴 REGENERATE AND DIFF, NEVER HAND-EDIT — the `reader_fixtures.json` rule. The Go shape ledger
(`internal/transcript/classify_test.go`) reads the committed file, so a stale file is a test
that measures a world nobody can rebuild; this module refuses one.

🔴 AND "SYNTHETIC" IS ASSERTED, NOT PROMISED. This repository is public, and the world is the
one fixture here shaped like a real session. Every timestamp must be in the year 2000 (what
`leakscan.py` allows), and the dated strings are COUNTED first so a regex that matched nothing
cannot read as a clean world. `leakscan.py` covers the credential and hostname shapes on every
tracked file already; the planted secrets of the redaction slice are generated at RUN time and
never land here.
"""
from __future__ import annotations

import importlib.util
import json
import re
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
GEN = ROOT / "tests" / "transcripts" / "gen.py"
WORLD = ROOT / "internal" / "transcript" / "testdata" / "synthetic_world.json"


def _gen():
    spec = importlib.util.spec_from_file_location("transcripts_gen", GEN)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def test_the_committed_world_is_a_FRESH_generation():
    gen = _gen()
    fresh = gen.render()
    committed = WORLD.read_text(encoding="utf-8")
    assert committed == fresh, (
        "internal/transcript/testdata/synthetic_world.json is STALE: run `python3 tests/transcripts/gen.py` "
        "and commit the result. Never hand-edit it."
    )
    # The control: the comparison above can see a one-character drift.
    assert committed[:-2] + "X\n" != fresh


def test_the_generator_is_DETERMINISTIC():
    gen = _gen()
    assert gen.render() == gen.render()


_ISO = re.compile(r"\b(\d{4})-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}")
# Epoch-millisecond values in opencode's `time` objects; year 2000 is [946684800000, 978307200000).
_EPOCH_MS = re.compile(r'"(?:created|updated|completed|start|end|startTime)": (\d{12,13})\b')


def test_every_date_in_the_world_is_in_the_SYNTHETIC_year():
    text = WORLD.read_text(encoding="utf-8")
    # JSONL is embedded as JSON strings, so decode the embedded documents' text as well.
    years = _ISO.findall(text)
    epochs = [int(v) for v in _EPOCH_MS.findall(text)]
    # POSITIVE CONTROL: the patterns must find dates, or "no real date" means nothing.
    assert len(years) > 100 and len(epochs) > 20, (years[:3], epochs[:3])
    assert set(years) == {"2000"}, sorted(set(years))
    out_of_year = [e for e in epochs if not 946684800000 <= e < 978307200000]
    assert not out_of_year, out_of_year[:5]


def test_the_world_carries_the_R7_read_paths_and_the_F1_empty_ledger():
    world = json.loads(WORLD.read_text(encoding="utf-8"))
    sessions = world["sessions"]
    # The empty ledger beside a program-naming input (F1) must EXIST and be EMPTY — an absent
    # file and an empty one are different facts to the capture agent.
    f1 = sessions["cc-f1-empty-ledger"]["id"] + ".jsonl"
    assert world["ledgers"][f1] == ""
    # A session with NO ledger at all exists too (the unrelated session).
    assert sessions["cc-plain"]["id"] + ".jsonl" not in world["ledgers"]
    text = json.dumps(world)
    for needle in ("scope=(all scopes)", "subsystem-recall: scope-unreadable: all 2 entry files",
                   "cairn ls-entries", "cairn search --repo", "&& cairn recall", "--scope beta-notes",
                   "subsystem-store/alpha-notes"):
        assert needle in text, needle
