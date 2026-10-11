"""The memo trust boundary's hostile corpus (S0 of `claudedocs/plan-cairn-scope-mail.md`) is FRESH,
PARTITIONED exactly 8 + 8, and SYNTHETIC.

🔴 REGENERATE AND DIFF, NEVER HAND-EDIT. `internal/memo`'s Go tests read the committed file, and
`tests/memo/e2e.sh` (S3) will depend on its two halves; a stale file is a test that measures a
corpus nobody can rebuild.

The Go side pins each member's VERDICT against the real `memo.BreaksFence`
(`TestBreaksFencePartitionsTheHostileCorpusEightAndEight`); this side pins the NAMES of each half,
so a member silently moved between halves — which would change e2e (d)'s count arithmetic — fails
here even when both verdicts still hold.
"""
from __future__ import annotations

import importlib.util
import json
import re
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
GEN = ROOT / "tests" / "memo" / "hostile.py"
CORPUS = ROOT / "internal" / "memo" / "testdata" / "hostile.json"

FENCE_BREAKING = {
    "esc-csi-clear", "esc-osc-title", "nul", "u202e-before-marker", "u2066-isolate",
    "u2028-line-separator", "u2029-paragraph-separator", "tag-smuggling",
}
SEND_ACCEPTED = {
    "closing-marker-column-0", "closing-marker-mid-line", "guessed-nonce", "marker-word-in-subject",
    "fake-standing-line", "fake-header-line", "variation-selector-payload", "zero-width-mix",
}


def _gen():
    spec = importlib.util.spec_from_file_location("memo_hostile_gen", GEN)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def test_the_committed_corpus_is_a_FRESH_generation():
    assert CORPUS.read_text(encoding="utf-8") == _gen().render(), (
        "internal/memo/testdata/hostile.json is STALE: run `python3 tests/memo/hostile.py` and "
        "commit the result. Never hand-edit it."
    )


def test_the_two_halves_are_EXACTLY_the_pinned_eight_and_eight():
    c = json.loads(CORPUS.read_text(encoding="utf-8"))
    assert [m["name"] for m in c["fence_breaking"]] and len(c["fence_breaking"]) == 8
    assert {m["name"] for m in c["fence_breaking"]} == FENCE_BREAKING
    assert len(c["send_accepted"]) == 8
    assert {m["name"] for m in c["send_accepted"]} == SEND_ACCEPTED
    every = [m["name"] for k in ("fence_breaking", "send_accepted", "render_extra", "ordinary", "ordinary_refused")
             for m in c[k]]
    assert len(every) == len(set(every)), "member names must be unique across every list"


def test_the_committed_bytes_are_ASCII_so_nothing_in_the_file_is_invisible():
    raw = CORPUS.read_bytes()
    assert raw.isascii(), "write the corpus with ensure_ascii=True; a raw invisible code point hides from review"
    # POSITIVE CONTROL: the escapes are really there (the corpus is not ASCII because it is EMPTY).
    assert raw.count(b"\\u") > 50


def test_every_date_in_the_corpus_is_in_the_SYNTHETIC_year():
    text = CORPUS.read_text(encoding="utf-8")
    years = re.findall(r"\b(\d{4})-\d{2}-\d{2}T", text)
    assert years, "positive control: the fake-header member carries a date"
    assert set(years) == {"2000"}, sorted(set(years))
