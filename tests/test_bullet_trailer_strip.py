#!/usr/bin/env python3
"""The `[cb:]` ECHO HOLE in `content_hash`/`bullet_content` — the PYTHON half.

🔴 WHAT THE DEFECT WAS, AND WHY IT IS A REGRESSION RATHER THAN AN INVARIANT.
`bullet_content` stripped the attribution with a single `sub` over an expression
anchored at `\\Z` and nothing else (the `_ATTRIBUTION_RE` / `_CITATION_TOKEN_RE`
pair, now folded into the one `_BULLET_TRAILERS_RE` alternation). The moment a read
surface appends the per-bullet
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

Every test below was watched RED against the pre-fix source — ONE end-anchored `sub`
on the stored side and a RAW hash on the request side — and the matrix is in the
commit message.

⚠ "SINGLE-PASS" IS NOW AMBIGUOUS HERE AND THE WORD IS DELIBERATELY NOT USED. An
earlier draft of this docstring called the pre-fix source "the single-pass strip",
which stopped being a distinguishing description the moment the order-free LOOP that
replaced it was itself replaced by a single-pass ALTERNATION — see
`_BULLET_TRAILERS_RE` for why (the loop was quadratic on a write path with no
per-line length cap). What distinguished the pre-fix source was the ANCHOR, not the
number of passes.
"""
from __future__ import annotations

import importlib.util
import sys
import time
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


# ---------------------------------------------------------------------------
# THE OPENER SEAM between `_bullet_request_problem` and `bullet_content` — the
# PYTHON half.  `internal/write/openerseam_test.go` is the Go twin.
#
# 🔴 WHAT THE DEFECT WAS. `_append_bullet` asserted that `bullet_content`'s opener
# strip is "inert for a request" because `_bullet_request_problem` "refuses a `text`
# that opens a markdown bullet".  The two asked DIFFERENT questions of DIFFERENT
# strings: the validator tested `text.lstrip().startswith(("- ", "* "))`, an ASCII
# space, on the RAW text, while `bullet_content` strips `_BULLET_OPENER_RE` from the
# COLLAPSED text — and `str.split()`'s class carries U+00A0, U+1680, U+2000..U+200A,
# U+202F, U+205F and U+3000.
#
# 🔴 AND IT IS A REGRESSION, NOT AN INVARIANT: measured over one synthetic entry,
# `POST "-\u00a0<prose>"` straight after `POST "<prose>"` answered `appended` at
# 3fb8dc2 (the request side was hashed raw, so no opener strip ran), `duplicate` at
# 0aa5fc48 with the caller's text never written, and a 400 here.
#
# ⚠ EVERY NON-ASCII WHITESPACE BELOW IS SPELLED AS AN ESCAPE, NEVER PASTED. A literal
# U+00A0 in a source file is indistinguishable on screen from a space, which is the
# very confusion the defect is made of.
# ---------------------------------------------------------------------------

#: The non-ASCII members of the collapse class that REACH the opener clause. U+0085,
#: U+2028 and U+2029 are deliberately absent: they are line breaks, so the one-line
#: clause refuses them several clauses earlier, and a fixture built on one would pass
#: for the wrong reason.
COLLAPSE_WHITESPACE_OPENERS = (
    "\u00a0", "\u1680", "\u2000", "\u200a", "\u202f", "\u205f", "\u3000",
)


@pytest.mark.parametrize("ws", COLLAPSE_WHITESPACE_OPENERS)
@pytest.mark.parametrize("opener", ["-", "*"])
def test_an_opener_spelled_with_NON_ASCII_whitespace_is_REFUSED(ws, opener):
    text = f"{opener}{ws}{PROSE}"
    problem = api._bullet_request_problem({"text": text, "session": "s1"})
    assert problem is not None, (
        f"U+{ord(ws):04X} after {opener!r}: ACCEPTED, and `bullet_content` reduces it "
        f"to {api.bullet_content([text])!r} — the same content as a bare prose bullet, "
        "so the append answers `duplicate` and the caller's text is never stored")
    assert "must not open a markdown bullet" in problem, (
        f"refused for the wrong reason: {problem!r}")


def test_the_validator_refuses_every_text_whose_COLLAPSED_form_opens_a_bullet():
    """🔴 THE SEAM AS A RELATIONSHIP RATHER THAN A CHARACTER LIST. The property
    `_append_bullet` now asserts is "no accepted `text` reaches `_BULLET_OPENER_RE`",
    so this asks exactly that of every row — a spelled list of whitespace characters
    would pass while the next one nobody thought of walked straight through.

    ⚠ AN INVARIANT GUARD over the rows the test above already covers, so it is not
    counted twice as regression coverage; what it adds is the SHRINK direction."""
    ws = ["", " ", "  ",
          "\u00a0", "\u1680", "\u2000", "\u200a", "\u202f", "\u205f", "\u3000",
          " \u00a0", "\u00a0 "]
    heads = ["", "-", "*", "--", "-x", "x", "a-", " ", "\u00a0"]
    tails = ["", PROSE, f"2000-01-02: {PROSE}", "-", "*", "]"]

    accepted = refused = 0
    for h in heads:
        for w in ws:
            for t in tails:
                text = h + w + t
                problem = api._bullet_request_problem({"text": text, "session": "s1"})
                if problem is not None:
                    refused += 1
                    continue
                accepted += 1
                collapsed = " ".join(text.split())
                assert not api._BULLET_OPENER_RE.match(collapsed), (
                    f"{text!r} is ACCEPTED yet `_BULLET_OPENER_RE` matches its "
                    f"collapsed form {collapsed!r} — `bullet_content` will strip an "
                    "opener the CALLER sent, so the content hash is taken over "
                    "somebody else's bullet")
    # POSITIVE CONTROL: a zero would be indistinguishable from a loop that never
    # entered its body, and the accepted branch is the only one that asserts anything.
    assert accepted > 0, "not one row was ACCEPTED, so the assertion above never ran"
    assert refused > 0, "not one row was REFUSED, so this table cannot see the clause"


@pytest.mark.parametrize(
    "text", ["-", "*", "- ", "* ", "-  ", "-\u00a0", " -\u00a0"])
def test_an_opener_with_NOTHING_after_it_is_still_refused(text):
    """⚠ THE DUAL HAZARD THE SENTINEL SPACE CLOSES, AND THE REASON THE CLAUSE IS NOT
    SIMPLY `_BULLET_OPENER_RE` OVER THE COLLAPSED TEXT. The collapse drops a trailing
    whitespace run, so `"- "` arrives as `"-"`, which `[ \\t]+` does not match — and
    the raw-prefix check this replaced DID refuse it. Without the sentinel the fix
    would have been wider on one axis and NARROWER on another."""
    problem = api._bullet_request_problem({"text": text, "session": "s1"})
    assert problem is not None and "must not open a markdown bullet" in problem, (
        f"{text!r}: got {problem!r}, want the markdown-opener refusal")


@pytest.mark.parametrize("text", ["-foo", "*foo", "--foo", "x - y"])
def test_a_dash_with_NO_whitespace_after_it_is_still_PROSE(text):
    """The NEGATIVE control for the clause above: `bullet_content` does not strip
    these either, so refusing them would be a widening nobody chose."""
    assert api._bullet_request_problem({"text": text, "session": "s1"}) is None
    assert api.bullet_content([f"- 2000-01-02: {text}"]) == text


def test_a_NON_ASCII_opener_does_not_SILENTLY_DEDUPE_end_to_end(tmp_path):
    """🔴 THE DEFECT AS BEHAVIOUR, because the assertions above are about the
    validator and the damage lived in the SEAM: a unit test of either side alone
    stays green."""
    entry = tmp_path / "entry.md"
    entry.write_text(
        "---\nservice: synth\n---\n\n## What it is\n\nx\n\n## Pointers\n\n- y\n\n"
        f"{api.rc.NUANCE_HEADING}\n\n- 2000-01-01: an older note.\n",
        encoding="utf-8")
    status, _line, _rev = api.append_bullet(
        entry, text=PROSE, actor="zach", session="s1", today="2000-01-02")
    assert status == "appended", status

    disguised = f"-\u00a0{PROSE}"
    problem = api._bullet_request_problem({"text": disguised, "session": "s1"})
    if problem is None:
        status2, line2, _ = api.append_bullet(
            entry, text=disguised, actor="zach", session="s1", today="2000-01-03")
        stored = entry.read_text(encoding="utf-8")
        raise AssertionError(
            f"a U+00A0 opener was accepted and the append answered {status2!r} naming "
            f"{line2!r}; is the caller's own text in the file? {disguised in stored}")


#: The wall-clock ceiling for ONE `bullet_content` call over a stored line of many
#: trailers. A thousandfold-margin number rather than a tight one — the measurements
#: that set it are on the test below.
TRAILER_STRIP_BUDGET_SECONDS = 1.0


@pytest.mark.parametrize("n", [4000, 8000])
def test_the_trailer_strip_is_LINEAR_rather_than_QUADRATIC(n):
    """🔴 A REGRESSION GUARD ON CPU, AND THE REGRESSION WAS REACHABLE ON THE WRITE
    PATH. `0aa5fc48` made `_strip_bullet_trailers` a LOOP over two `\\Z`-anchored
    expressions, so every iteration re-scanned from position 0: O(trailers × length).
    `bullet_content` runs once per stored bullet on every `POST /bullets`, inside the
    per-entry write lock, and there is no per-line length cap on the bytes a write can
    put on disk — `BULLET_TEXT_MAX` governs only the REQUEST text, and the rate
    limiter counts failed auths only.

    MEASURED on this host over one stored line of n trailing ` [cb:deadbeef]` tokens,
    CPython 3.12, the strip alone:

        n        line bytes   loop        one alternation
        1                62   0.000002s   0.000001s
        1,000        14,048   0.1066s     0.000121s
        4,000        56,048   1.7052s     0.000404s
        16,000      224,048   27.4662s    0.001362s

    🔴 TWO POINTS, NAMED, because one measurement is not a claim about a curve. The
    budget sits three orders of magnitude ABOVE the linear cost at both points and
    below the quadratic cost at them — so no plausible machine slowdown turns a red
    into a green or the reverse.
    """
    line = f"- 2000-01-02: {PROSE}" + TOKEN * n
    started = time.perf_counter()
    got = api.bullet_content([line])
    elapsed = time.perf_counter() - started
    assert got == PROSE, (
        f"n={n}: reduced to {got[:80]!r} ({len(got)} chars) — this measures the wrong "
        "thing if the strip is not also CORRECT at scale")
    assert elapsed < TRAILER_STRIP_BUDGET_SECONDS, (
        f"n={n} ({len(line)} line chars): bullet_content took {elapsed:.3f}s, budget "
        f"{TRAILER_STRIP_BUDGET_SECONDS:.3f}s — the strip is super-linear in the "
        "number of trailers, which is a write-path CPU exhaustion reachable by an "
        "authenticated writer")
