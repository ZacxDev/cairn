#!/usr/bin/env python3
"""The `[cb:]` ECHO HOLE in `content_hash`/`bullet_content` — the PYTHON half.

🔴 WHAT THE DEFECT WAS, AND WHY IT IS A REGRESSION RATHER THAN AN INVARIANT. The
attribution parser `_ATTRIBUTION_RE` is anchored at `\\Z`, and `bullet_content`
stripped it with a single `sub`. The moment a read surface appends the per-bullet
citation token, a stored line ends `… [cairn: a/b] [cb:xxxxxxxx]` — the anchor no
longer reaches the attribution, so the WHOLE trailer entered the content hash, the
idempotency check stopped matching, and a retried append landed a near-duplicate
carrying a `[cb:…]` in its prose whose value is not that new bullet's id.

The echo is MEASURED, not feared: a downstream consumer's resume flow requires its
report to echo what it recalled, which fires on 451 of 454 runs (99.3%). So text
arriving at `POST /bullets` routinely ends in whatever a read surface printed.

🔴 TWO SIDES, AND FIXING ONE FIXES NOTHING. `append_bullet` reduced the STORED bullet
with `bullet_content` and hashed the REQUEST text raw, so even a perfect stored-side
strip left the two hashes taken over different strings. Both sides now go through
`bullet_content`.

Every test below was watched RED against the pre-fix source — the single-pass strip
and the raw request hash — and the matrix is in the commit message.
"""
from __future__ import annotations

import importlib.util
import sys
from pathlib import Path

import pytest

REPO = Path(__file__).resolve().parents[1]
SERVER_PATH = REPO / "server" / "server.py"


def _load_server():
    spec = importlib.util.spec_from_file_location("cairn_server_trailers", SERVER_PATH)
    assert spec and spec.loader
    module = importlib.util.module_from_spec(spec)
    sys.modules[spec.name] = module
    spec.loader.exec_module(module)
    return module


api = _load_server()

PROSE = "the drill head overheats above 40C"
ATTR = " [cairn: zach/s1]"
TOKEN = " [cb:deadbeef]"


@pytest.mark.parametrize("trailers,label", [
    (ATTR, "attribution only — the shape that already worked"),
    (TOKEN, "token only"),
    (ATTR + TOKEN, "attribution THEN token — the order a read surface produces"),
    (TOKEN + ATTR, "token THEN attribution — the order `render_bullet` produces"),
    (TOKEN + ATTR + TOKEN, "a second echo round trip"),
])
def test_every_trailer_ORDER_reduces_to_the_same_content(trailers, label):
    """🔴 ORDER-FREE, WHICH IS WHAT TWO SEQUENTIAL `sub` CALLS ARE NOT. With the
    attribution stripped first, `… [cairn: a/b] [cb:deadbeef]` keeps its attribution
    because the `\\Z` anchor no longer reaches it."""
    bare = api.bullet_content([f"- 2000-01-02: {PROSE}"])
    withTrailers = api.bullet_content([f"- 2000-01-02: {PROSE}{trailers}"])
    assert withTrailers == bare == PROSE, (
        f"{label}: reduced to {withTrailers!r}, expected {PROSE!r} — the trailer is in "
        "the content hash, so a re-POST of this bullet will not be recognised")


def test_an_ECHOED_bullet_is_recognised_as_a_DUPLICATE(tmp_path):
    """🔴 THE DEFECT AS BEHAVIOUR, END TO END, because the two unit assertions above
    are about `bullet_content` and the hole lived in the SEAM between it and the
    request-side hash. The entry is written, the bullet appended, then re-POSTed as a
    read surface would have printed it — prose plus both trailers."""
    entry = tmp_path / "entry.md"
    entry.write_text(
        "---\nservice: synth\n---\n\n## What it is\n\nx\n\n## Pointers\n\n- y\n\n"
        f"{api.rc.NUANCE_HEADING}\n\n- 2000-01-01: an older note.\n",
        encoding="utf-8")

    status, line, _rev = api.append_bullet(
        entry, text=PROSE, actor="zach", session="s1", today="2000-01-02")
    assert status == "appended", (status, line)
    assert line.endswith(ATTR), line

    # What a read surface prints for that stored bullet: the line it wrote, plus the
    # citation token. An agent echoing it back cannot send the `- ` opener —
    # `_bullet_request_problem` refuses that — so it sends the prose and the trailers.
    echoed = f"{PROSE}{ATTR}{TOKEN}"
    assert api._bullet_request_problem(
        {"text": echoed, "session": "s1"}) is None, (
        "the echoed text is not even an acceptable request, so this test would be "
        "measuring the validator rather than the hash")

    status2, line2, _rev2 = api.append_bullet(
        entry, text=echoed, actor="zach", session="s1", today="2000-01-03")
    assert status2 == "duplicate", (
        f"the echo appended a {status2!r} rather than being recognised: {line2!r}")
    assert line2 == line, "the duplicate must name the bullet already on disk"
    assert entry.read_text(encoding="utf-8").count("[cb:") == 0, (
        "a citation token reached the curated file, which is the second half of the "
        "damage: its value is not the new bullet's id")


def test_a_cb_token_in_PROSE_is_not_stripped():
    """⚠ THE NARROWING, SO THE STRIP IS NOT A FREE-FOR-ALL. Only a TRAILING token is
    machine-written. One mid-sentence is prose — a bullet may legitimately quote a
    citation id — and stripping it would make two different bullets hash alike."""
    quoted = "the id [cb:deadbeef] resolves to the wrong bullet"
    assert api.bullet_content([f"- 2000-01-02: {quoted}"]) == quoted


def test_a_MALFORMED_token_is_not_stripped():
    """The class is 8 LOWERCASE HEX. A looser pattern would eat prose; a tighter one
    would miss the real thing. Both directions, so neither drifts alone."""
    for bad in (" [cb:DEADBEEF]", " [cb:deadbee]", " [cb:deadbeef0]", " [cb:zzzzzzzz]"):
        reduced = api.bullet_content([f"- 2000-01-02: {PROSE}{bad}"])
        assert reduced != PROSE, (
            f"{bad!r} was stripped; the token class is 8 lowercase hex and nothing else")
    good = api.bullet_content([f"- 2000-01-02: {PROSE} [cb:0123456f]"])
    assert good == PROSE, "a well-formed token was NOT stripped — the class is too tight"


def test_the_token_COUNTS_against_BULLET_TEXT_MAX():
    """⚠ THE DECISION, PINNED SO THE COMMENT AND THE CODE CANNOT DRIFT APART.
    `bullet_content`'s docstring says an echoed token spends 14 of the 2000 characters
    rather than being exempt; this is the assertion that makes that a fact. The cap is
    measured on the SUBMITTED text, before any strip."""
    over = "x" * (api.BULLET_TEXT_MAX - len(TOKEN) + 1) + TOKEN
    assert len(over) == api.BULLET_TEXT_MAX + 1
    problem = api._bullet_request_problem({"text": over, "session": "s1"})
    assert "max" in problem and "1 over" in problem, (
        f"the token was exempted from the cap: {problem!r}")
