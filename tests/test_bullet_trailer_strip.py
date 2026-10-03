#!/usr/bin/env python3
"""The `[cb:]` ECHO HOLE in `content_hash`/`bullet_content` — the PYTHON half.

🔴 WHAT THE DEFECT WAS, AND WHY IT IS A REGRESSION RATHER THAN AN INVARIANT.
`bullet_content` stripped the attribution with a single `sub` over an expression
anchored at `\\Z` and nothing else (the `_ATTRIBUTION_RE` / `_CITATION_TOKEN_RE`
pair, now folded into the one `_BULLET_TRAILER_PIECE_RE` alternation that
`_strip_bullet_trailers` peels the run with). The moment a read
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
replaced it was itself replaced by a single-pass ALTERNATION — and then stopped
again when THAT was replaced by a backwards PEEL, because the whole-run alternation
was itself quadratic on any suffix the run does not end (see
`_BULLET_TRAILER_PIECE_RE` for both measurements). Three implementations, one
grammar, and the number of passes never distinguished any of them: what
distinguished the pre-fix source was the ANCHOR.
"""
from __future__ import annotations

import hashlib
import importlib.util
import itertools
import random
import re
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
#:
#: ⚠ IT IS A SAMPLE OF A RANGE, NOT THE RANGE: U+2000 and U+200A are the ENDPOINTS of
#: U+2000..U+200A and the nine between them are not listed. Said because the list reads
#: like an enumeration. The whole class was swept at this head instead — all 29 code
#: points `str.isspace()` (and therefore `str.split()`) accepts, each as
#: `"-" + ws + PROSE` through `_bullet_request_problem` — and it leaves no gap behind
#: the sample: 0 ACCEPTED; 17 refused by the OPENER clause (U+0020, U+00A0, U+1680, all
#: of U+2000..U+200A, U+202F, U+205F, U+3000); 10 refused EARLIER by the one-line clause
#: (U+000A..U+000D, U+001C, U+001D, U+001E, U+0085, U+2028, U+2029); and 2 refused
#: earlier by the category clause (U+0009, U+001F). So the nine unsampled members are
#: covered by the opener clause exactly as the endpoints are, and U+001C..U+001F are
#: absent for TWO different reasons rather than one.
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
#: trailers. A loose number rather than a tight one: at the two asserted points it
#: sits 383x-5,600x above the peel's measured cost on both shapes. The measurements
#: that set it, and
#: the one row where it does NOT refuse the implementation this replaced, are on the
#: test below.
TRAILER_STRIP_BUDGET_SECONDS = 1.0

#: The ceiling on NOT-END cost ÷ AT-END cost at the same n, measured back to back in
#: ONE process. This is the assertion that pins the SHAPE axis rather than the machine:
#: a wall-clock budget can be met by a fast box, a ratio cannot. Sampled at this head,
#: five times each at n=4,000 and n=8,000 in one process: 0.118-0.142 for the peel, and
#: 1,134x-2,917x for the `+`-and-`\\Z` expression it replaced. So 50 clears the peel by
#: a factor of 352 and refuses that expression by a factor of 23 — a separation no
#: plausible machine or load difference closes, because both halves of each ratio move
#: together.
#:
#: 🔴 IT IS REGRESSION COVERAGE, NOT AN INVARIANT GUARD, AND THAT IS THE MATRIX: at
#: `cee28805` the n=4,000 row goes RED on THIS assertion (1174x) and the n=8,000 row on
#: the BUDGET (2.098s); at HEAD both are green. Isolated to the narrowest expression as
#: well as to the whole file — swapping only `_strip_bullet_trailers`' body for the
#: whole-run expression reds exactly these two and leaves the other 36 tests in this
#: file green, which is what proves the mutant CORRECT and this guard the only thing
#: that sees it.
TRAILER_STRIP_SHAPE_RATIO_MAX = 50.0


@pytest.mark.parametrize("n", [4000, 8000])
def test_the_trailer_strip_is_LINEAR_rather_than_QUADRATIC(n):
    """🔴 A REGRESSION GUARD ON CPU, AND THE REGRESSION WAS REACHABLE ON THE WRITE
    PATH — TWICE, BY TWO DIFFERENT MECHANISMS, WHICH IS WHY THIS NOW MEASURES TWO
    SUFFIX SHAPES. `0aa5fc48` made `_strip_bullet_trailers` a LOOP over two
    `\\Z`-anchored expressions, so every iteration re-scanned from position 0.
    `cee28805` replaced it with ONE `(?:[ \\t]*(?:ATTR|TOKEN))+\\Z` alternation, which
    is fast when the run reaches the anchor and QUADRATIC when it does not: CPython
    retries the alternation from every start position, and each of the ~n positions
    INSIDE the run matches its way to the final non-trailer word before failing, O(n)
    apiece. Both are O(trailers × length) on a path that runs once per stored
    bullet on every `POST /bullets`, inside the per-entry write lock, with no per-line
    length cap on the bytes a write can put on disk — `BULLET_TEXT_MAX` governs only
    the REQUEST text, and the rate limiter counts failed auths only.

    🔴 WHICH SHAPES THIS FIXTURE COVERS, NAMED, BECAUSE IT COVERS EXACTLY TWO. Both
    are `"- 2000-01-02: " + PROSE + TOKEN * n`: AT-END as-is, so the trailer run
    reaches end-of-line; NOT-END with one non-trailer word appended, so it does not.
    The AT-END shape ALONE is what this fixture built before `cee28805`'s quadratic
    expression was measured, and it is why that expression shipped green. What is
    still NOT covered: a run interleaving attributions and tokens, a run of malformed
    near-misses, a trailer-like suffix in the MIDDLE of a long line, and any
    multi-line bullet.

    MEASURED through `bullet_content`, both implementations in ONE process in ONE run,
    this host, CPython 3.12 (load average 5.3-5.9):

        n        line chars   `+`\\Z AT-END  `+`\\Z NOT-END  peel AT-END  peel NOT-END
        4,000        56,059   0.000434s     0.492542s      0.001338s    0.000179s
        8,000       112,059   0.001018s     2.108743s      0.002609s    0.000322s

    🔴 TWO POINTS, NAMED, AND THE BUDGET'S MARGIN IS NOT THE SAME AT BOTH. Against
    the quadratic expression the budget refuses n=8,000 NOT-END (2.11s, 2.1x over) and
    does NOT refuse n=4,000 NOT-END (0.49s, under it) — so on the budget alone that
    row is a curve point, not a refusal. `TRAILER_STRIP_SHAPE_RATIO_MAX` is what makes
    it a refusal: 1,134x-2,461x at n=4,000 against a bound of 50.
    """
    at_end = f"- 2000-01-02: {PROSE}" + TOKEN * n
    not_end = at_end + " tail"

    started = time.perf_counter()
    got_at_end = api.bullet_content([at_end])
    at_end_seconds = time.perf_counter() - started

    started = time.perf_counter()
    got_not_end = api.bullet_content([not_end])
    not_end_seconds = time.perf_counter() - started

    assert got_at_end == PROSE, (
        f"n={n} AT-END: reduced to {got_at_end[:80]!r} ({len(got_at_end)} chars) — this "
        "measures the wrong thing if the strip is not also CORRECT at scale")
    # NOT-END: the run does not reach the anchor, so NOTHING is a trailer and only the
    # opener comes off. Pinned so a strip that got fast by stripping too much cannot
    # pass this test.
    assert got_not_end == PROSE + TOKEN * n + " tail", (
        f"n={n} NOT-END: the strip removed a suffix that does not reach end-of-line — "
        f"{len(got_not_end)} chars, want {len(PROSE + TOKEN * n + ' tail')}")

    for label, elapsed, line in (("AT-END", at_end_seconds, at_end),
                                 ("NOT-END", not_end_seconds, not_end)):
        assert elapsed < TRAILER_STRIP_BUDGET_SECONDS, (
            f"n={n} {label} ({len(line)} line chars): bullet_content took "
            f"{elapsed:.3f}s, budget {TRAILER_STRIP_BUDGET_SECONDS:.3f}s — the strip "
            f"is super-linear in the number of trailers on the {label} suffix shape, "
            "which is a write-path CPU exhaustion reachable by an authenticated writer")

    ratio = not_end_seconds / at_end_seconds
    assert ratio < TRAILER_STRIP_SHAPE_RATIO_MAX, (
        f"n={n}: NOT-END cost {not_end_seconds:.6f}s is {ratio:.0f}x AT-END's "
        f"{at_end_seconds:.6f}s, bound {TRAILER_STRIP_SHAPE_RATIO_MAX:.0f}x — the "
        "strip's cost depends on WHERE the trailer run ends, which is the signature of "
        "a backtracking re-scan and a write-path CPU exhaustion reachable by an "
        "authenticated writer")


# ---------------------------------------------------------------------------
# THE TWO MECHANISMS, AND THE TWO THINGS THAT HOLD THEM TOGETHER.
#
# 🔴 THE PYTHON STRIP AND THE GO STRIP ARE NO LONGER ONE EXPRESSION IN TWO
# SPELLINGS. `server.py` peels the trailer run one piece at a time; `internal/write`
# keeps the whole-run `(?:[ \t]*(?:ATTR|TOKEN))+\z` alternation, because RE2 cannot
# backtrack and CPython's engine can — see `_BULLET_TRAILER_PIECE_RE` for the
# measurement. So "they agree" stopped being readable off a shared pattern and became
# a property that has to be MEASURED, which is what the two tests below do:
#
#   * the ORACLE half — this peel against the whole-run expression it replaced, in
#     ONE language, over a corpus wide enough to find a disagreement;
#   * the CROSS-LANGUAGE half — a digest of this peel's output over a DECLARED
#     enumeration, pinned to the same hex string that
#     `internal/write/trailerdigest_test.go` pins for `stripBulletTrailers`. Two
#     independent implementations, one constant: if either
#     drifts on any of the enumerated inputs, both sides go red.
#
# ⚠ AND THE SECOND IS A DIGEST, SO IT NAMES NO INPUT. It answers "do the two agree",
# never "where do they differ" — when it goes red, run the oracle half and the
# per-row tests above, which name rows. A digest is what makes the comparison
# affordable in two languages without shipping a 10 MB fixture.
# ---------------------------------------------------------------------------

#: One axis of the declared enumeration: 20 trailer-ish pieces — 5 attribution
#: spellings, 5 token spellings, 10 malformed near-misses. Spelled IDENTICALLY in
#: `internal/write/trailerdigest_test.go`; the digest below is only a cross-language
#: claim while both lists are the same, so change them together or not at all.
TRAILER_STRIP_AXIS_PIECES = (
    " [cairn: zach/s1]", "[cairn: a/b]", "  [cairn: a-b/c.d_e]",
    "\t[cairn: z9/Z9]", " [cairn: abcdefghijklmnopqrstuvwxyz0123/S]",
    " [cb:deadbeef]", "[cb:00000000]", "\t[cb:ffffffff]",
    "   [cb:0123abcd]", " [cb:abcdef01]",
    " [cairn: /b]", " [cairn: a/]", " [cb:deadbee]", " [cb:deadbeeff]",
    " [cb:DEADBEEF]", " [cairn:a/b]", " [cairn: A/b]", " [cb:deadbeef",
    "cb:deadbeef]", " []",
)

#: The other axis: 10 prose prefixes, chosen so the enumeration reaches the shapes a
#: suffix peel can get wrong — a prose that ENDS in `]`, one that CONTAINS a
#: bracket, one that is itself a trailer, an empty one, a whitespace-only one, and
#: one that already carries a bullet opener.
TRAILER_STRIP_AXIS_PROSES = (
    "the drill head overheats", "x", "", "   ", "- 2000-01-02: a note",
    "ends in a bracket]", "starts with [", "[cb:deadbeef] mid prose",
    "a\tb", "trailing space ",
)

#: 10 proses × (1 + 20 + 400 + 8000) suffix sequences of length 0..3 = 84,210 inputs,
#: and the axes are collision-free so the count is exact rather than a ceiling. It is
#: asserted SEPARATELY from the digest, because a digest over an EMPTY enumeration is a
#: perfectly stable hex string — and that assertion is reachable rather than
#: decorative: removing ONE of the 20 pieces reds it at 72,400, before the digest is
#: read.
TRAILER_STRIP_AXIS_INPUTS = 84210

#: sha256 over `_strip_bullet_trailers(input) + b"\x00"` for every enumerated input,
#: in enumeration order. 🔴 `internal/write/trailerdigest_test.go` PINS THE SAME
#: STRING for `stripBulletTrailers`, and that is the whole point: it is the only place
#: the two implementations meet. Re-derive it by running either side, never by
#: copying the other's failure message — a message quotes what the code DID.
TRAILER_STRIP_AXIS_DIGEST = (
    "a5f4c2e4116e202169a3d00cc1c39f9e4fc3e6380d4c5f128d7a8ab9212e970e")


def _trailer_strip_axis_inputs():
    """The declared enumeration, in the order the digest is taken over."""
    for prose in TRAILER_STRIP_AXIS_PROSES:
        for k in range(0, 4):
            for combo in itertools.product(TRAILER_STRIP_AXIS_PIECES, repeat=k):
                yield prose + "".join(combo)


def test_the_strip_DIGESTS_to_the_same_string_the_GO_half_pins():
    """🔴 THE CROSS-LANGUAGE HALF, AND IT IS A SEAM GUARD RATHER THAN A COMPONENT
    ONE. Both languages were verified in isolation before this change and both were
    green; what nothing measured was the RELATIONSHIP, because it used to be readable
    off a shared regex and now is not.

    ⚠ AN INVARIANT GUARD AS WRITTEN, LABELLED AS ONE: it was authored green, since
    the peel was already equivalent when it landed. What it is FOR is the next edit
    to either side — and it is mutation-proven rather than assumed. Replacing the
    peel's body with an UNANCHORED `sub` (strips trailers anywhere, not only off the
    end) moves the digest to `f347293a…` and reds this assertion with its own
    message. ⚠ The whole-run expression does NOT move it — the two agree on all
    84,210, which is the point of the oracle test below and the reason that mutant is
    caught by the COST guards instead.

    The count is asserted first and separately, because a digest over an empty
    generator is a perfectly stable hex string.
    """
    digest = hashlib.sha256()
    count = 0
    for text in _trailer_strip_axis_inputs():
        digest.update(api._strip_bullet_trailers(text).encode("utf-8"))
        digest.update(b"\x00")
        count += 1
    assert count == TRAILER_STRIP_AXIS_INPUTS, (
        f"the enumeration produced {count} inputs, not {TRAILER_STRIP_AXIS_INPUTS} — "
        "the axes moved, so the digest below is a claim about a different corpus and "
        "the Go half is now comparing something else")
    assert digest.hexdigest() == TRAILER_STRIP_AXIS_DIGEST, (
        f"the strip digests to {digest.hexdigest()} over {count} enumerated inputs, "
        f"pinned {TRAILER_STRIP_AXIS_DIGEST} — either this implementation changed "
        "answer on one of them, or the axes above drifted from "
        "`internal/write/trailerdigest_test.go`'s. Run the oracle test below and the "
        "per-row tests above: those name rows, this one cannot")


def test_the_PEEL_agrees_with_the_WHOLE_RUN_expression_it_replaced():
    """🔴 THE ORACLE HALF: this peel against `(?:[ \\t]*(?:ATTR|TOKEN))+\\Z`, the
    expression `cee28805` carried and the one `internal/write` still carries. The two
    are different MECHANISMS over one grammar, so the only thing that makes the swap
    safe is a corpus wide enough to find a disagreement.

    ⚠ AN INVARIANT GUARD, LABELLED: it was authored green. The whole-run expression
    is rebuilt here FROM THE SAME FRAGMENTS the peel uses, so it cannot drift into
    testing a different grammar — which also means this test says nothing about
    whether the grammar is right, only that the two implementations of it agree.

    ⚠ AND THE RANDOM HALF IS NOT A REPRODUCIBLE CORPUS, DELIBERATELY. It is seeded,
    so a failure is re-runnable, but `random`'s stream is not a contract and the
    assertion is "zero disagreements" rather than a digest — so a CPython change
    moves which strings are drawn and breaks nothing. The structured half is the
    reproducible one, and it is the half the digest above is taken over.
    """
    whole_run = re.compile(
        r"(?:[ \t]*(?:" + api._ATTRIBUTION_PATTERN + r"|"
        + api._CITATION_TOKEN_PATTERN + r"))+\Z")

    alphabet = list("ab09:/-_. \t[]") + ["cairn: ", "cb:", "deadbeef", "[", "]"]
    rng = random.Random(20000102)

    def corpus():
        yield from _trailer_strip_axis_inputs()
        for _ in range(200_000):
            yield "".join(rng.choice(alphabet) for _ in range(rng.randint(0, 24)))

    disagreements = []
    compared = 0
    for text in corpus():
        compared += 1
        peeled = api._strip_bullet_trailers(text)
        subbed = whole_run.sub("", text, count=1)
        if peeled != subbed and len(disagreements) < 10:
            disagreements.append((text, peeled, subbed))
    # POSITIVE CONTROL on the differ itself: a reassuring zero is indistinguishable
    # from a comparison that never ran, so prove the same loop CAN report a
    # disagreement before believing the zero. One-piece-only is the realistic wrong
    # implementation here — it is what two sequential `sub` calls degrade to.
    one_piece = re.compile(
        r"[ \t]*(?:" + api._ATTRIBUTION_PATTERN + r"|"
        + api._CITATION_TOKEN_PATTERN + r")\Z")
    control = sum(
        1 for text in _trailer_strip_axis_inputs()
        if one_piece.sub("", text, count=1) != whole_run.sub("", text, count=1))
    assert control > 0, (
        "the comparison found no disagreement even against a strip that removes ONE "
        "piece — it is wired to nothing, and its zero below means nothing")
    assert compared == TRAILER_STRIP_AXIS_INPUTS + 200_000, compared
    assert not disagreements, (
        f"{len(disagreements)} of {compared} inputs reduce differently under the peel "
        f"and under the whole-run expression (positive control: {control} "
        f"disagreements against a one-piece strip): {disagreements}")
