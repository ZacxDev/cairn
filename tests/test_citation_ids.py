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
LIVE corpus, and nothing here speaks for RENDERING. ⚠ THAT SECOND CLAUSE USED TO READ
"no surface PRINTS an id yet — this PR adds the derivation and the position, not the
rendering", AND IT IS NO LONGER TRUE: both text renderers now append ` [cb:<id>]` to
every surfaced section line that opens a bullet, so a rendered byte depends on
`citation_id`. The differential reader fixture is what compares the two renderers' bytes
(138 of its lines carry a token), and `tests/test_citation_render.py` is what pins the
POSITION and the verbatim body on this side.

🔴 ONE GUARD WAS DELETED RATHER THAN KEPT, AND THE DELETION IS THE RECORD.
`test_the_id_is_STABLE_across_calls` asserted that two calls on one body agree.
`citation_id` has no state, cache, clock or randomness, so NO defect could violate it —
an *invariant* guard, which `AGENTS.md` says must be labelled and not counted as
regression coverage. Determinism ACROSS PROCESSES, which is the property anything
downstream actually needs, is covered strictly more strongly by the drift guard above:
it runs the generator in a subprocess and compares. Deleted rather than relabelled
because a labelled guard still costs a run and still reads as coverage in a list of
test names.
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

from subsystem_resolver import (  # noqa: E402
    CitationIDDomainError,
    JournalBullet,
    parse_journal_bullets,
)


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


#: 🔴 THE COVERED SET, AND IT CANNOT SHRINK. Each entry is a case name whose BODY
#: carries something no other case does, so deleting one is a RED run rather than a
#: quieter fixture — the same rule `reader_fixtures.json`'s coverage ledger carries.
#: The invalid-UTF-8 rows are here because the fixture had NONE until the decode
#: divergence was measured, and the `unicode-and-emoji` row that looked like coverage
#: exercises the ENCODE side only, where the defect was on the DECODE side.
REQUIRED_CASES = (
    "a-dash-line-INSIDE-A-FENCE-is-not-a-bullet",
    "a-TRUNCATED-multibyte-sequence-in-a-bullet",
    "a-LONE-continuation-byte-in-a-bullet",
    "an-OVERLONG-sequence-spans-three-replacements",
    "an-invalid-byte-on-a-CONTINUATION-line",
    "a-NON-newline-line-break-inside-a-bullet",
)

#: The count of hand-written cases, EXACT. A lower bound pins shrink only, so adding
#: a case would prompt nobody — which is what the Go side's `< 9` floor did while its
#: comment claimed otherwise. `internal/store/citationid_test.go` carries the same
#: number for its own tier; the drift guard above is what keeps them describing one
#: fixture.
HAND_WRITTEN_CASES = 14


def test_the_fixture_is_not_vacuous():
    """A floor, before any equality above is trusted. An empty `cases` list compares
    equal to an empty regeneration and would make the drift guard pass forever."""
    doc = json.loads(FIXTURE.read_text(encoding="utf-8"))
    cases = doc["cases"]
    assert len(cases) == HAND_WRITTEN_CASES, (
        f"{len(cases)} case(s), expected exactly {HAND_WRITTEN_CASES}. If you ADDED "
        "one, bump the constant here and in internal/store/citationid_test.go — that "
        "prompt is the point. If you did not, something truncated the fixture.")
    bullets = sum(len(c["bullets"]) for c in cases)
    assert bullets >= 12, f"only {bullets} bullet(s) across {len(cases)} cases"
    names = {c["name"] for c in cases}
    missing = [n for n in REQUIRED_CASES if n not in names]
    assert not missing, (
        f"the fixture lost case(s) {missing}. Each one is the only row carrying its "
        "shape; without it the seam is green over a case nobody runs.")
    # Every id present must be 8 lowercase hex. Asserted over the FIXTURE as well as
    # in Go, because a generator bug would otherwise be recorded as the oracle.
    for c in cases:
        for b in c["bullets"]:
            cid = b["citation_id"]
            assert len(cid) == 8 and all(ch in "0123456789abcdef" for ch in cid), (
                f"{c['name']}: {cid!r} is not 8 lowercase hex characters")
        # And every body round-trips its hex: a `body` that is not the decode of
        # `body_hex` would let the Go side compare ids over text the oracle never saw.
        raw = bytes.fromhex(c["body_hex"])
        assert raw.decode("utf-8", errors="replace") == c["body"], (
            f"{c['name']}: `body` is not the replace-decode of `body_hex`")


def test_at_least_one_fixture_case_carries_INVALID_utf8():
    """🔴 THE POSITIVE CONTROL ON THE ROW ABOVE. `REQUIRED_CASES` is a list of NAMES,
    and a name can be kept while its body is replaced with ASCII — the spelled-guard
    shape. This asserts the PROPERTY: some case's bytes do not decode strictly."""
    doc = json.loads(FIXTURE.read_text(encoding="utf-8"))
    malformed = []
    for c in doc["cases"]:
        raw = bytes.fromhex(c["body_hex"])
        try:
            raw.decode("utf-8")
        except UnicodeDecodeError:
            malformed.append(c["name"])
    assert len(malformed) >= 4, (
        f"only {len(malformed)} case(s) carry invalid UTF-8 ({malformed}); the decode "
        "divergence this fixture exists to catch is invisible without them")


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
    shifts the first bullet off 0, and a `- ` inside a FENCE is not a bullet.

    🔴 THE FENCED DASH IS AT COLUMN 0, AND THE INDENT IT USED TO CARRY MADE THIS
    ASSERTION VACUOUS. `_JOURNAL_BULLET` is `^[-*][ \\t]+`, anchored at column 0, so
    an INDENTED dash opens no bullet whether the fence rule exists or not — the
    assertion message said "the fenced dash must not be counted as a bullet" and
    nothing here could have counted it. MEASURED: with `_is_fence` disabled, every
    citation guard still PASSED. At column 0 the rule is load-bearing — ON gives 2
    bullets at [1, 5], OFF gives 3 at [1, 3, 5].
    """
    body = "preamble nobody can address\n- first\n```\n- not a bullet\n```\n- second\n"
    bullets = parse_journal_bullets(body)
    lines = body.splitlines()
    assert len(bullets) == 2, (
        "a `- ` line INSIDE A FENCE opened a bullet, so the fence rule is not being "
        f"applied: {[b.lines for b in bullets]}")
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


#: The entry bytes of the measured divergence: `\xe2\x82` is a TRUNCATED 3-byte
#: sequence, which CPython's `replace` handler collapses to ONE U+FFFD.
MALFORMED_ENTRY = b"- a\xe2\x82\n  tail\n"


def test_the_POD_and_the_CLIENT_decode_paths_give_ONE_id():
    """🔴 THE DOMAIN, ASSERTED AS BEHAVIOUR. The pod decodes entry files
    `errors="surrogateescape"` and the client `errors="replace"`, so one file reaches
    `citation_id` as two different `str`s. Before the fix the pod's form RAISED —
    measured `UnicodeEncodeError: 'utf-8' codec can't encode characters in position
    3-4: surrogates not allowed` — which would have been a 500 on a malformed entry
    the moment any surface printed an id.

    ⚠ AND THE TWO MUST AGREE, NOT MERELY BOTH ANSWER. `surrogatepass` would have made
    the pod total while making it disagree with Go and with the client, which is the
    divergence it looks like it closes.
    """
    pod = parse_journal_bullets(MALFORMED_ENTRY.decode("utf-8", "surrogateescape"))
    client = parse_journal_bullets(MALFORMED_ENTRY.decode("utf-8", "replace"))
    assert len(pod) == len(client) == 1
    assert "\udce2" in pod[0].text, "the premise moved: the pod's text has no surrogate"
    assert "�" in client[0].text, "the premise moved: the client's text has no U+FFFD"
    assert pod[0].citation_id == client[0].citation_id, (
        "one entry file has two citation ids depending on which handler read it")
    # And the value is the fixture's, so this agrees with Go rather than only itself.
    committed = json.loads(FIXTURE.read_text(encoding="utf-8"))
    row = next(c for c in committed["cases"]
               if c["body_hex"] == MALFORMED_ENTRY.hex())
    assert client[0].citation_id == row["bullets"][0]["citation_id"]


def test_a_surrogate_OUTSIDE_the_escape_range_is_a_NAMED_refusal():
    """⚠ THE RESIDUE, AND IT IS A REFUSAL RATHER THAN A GUESS. `surrogateescape`
    produces only U+DC80..U+DCFF, so a lone U+D800 cannot come from either decode path
    and Go cannot hold one at all — there is no measured answer to agree with. It
    raises `CitationIDDomainError` instead of an incidental `UnicodeEncodeError`, which
    is what makes the domain a declared thing rather than a stack trace.
    """
    with pytest.raises(CitationIDDomainError):
        JournalBullet(lines=("- a \ud800 lesson",), date=None, start_line=0).citation_id
    # The reachable half still answers, so the refusal above is not simply "surrogates
    # are refused" — which would be the pod's 500 again under a nicer name.
    assert JournalBullet(
        lines=("- a \udc80 lesson",), date=None, start_line=0).citation_id


def test_the_line_TERMINATOR_does_not_change_the_id():
    """🔴 A NORMALISATION THE DOCSTRING USED TO DENY, PINNED AS BEHAVIOUR SO THE
    CORRECTED CLAIM IS CHECKABLE. `splitlines()` strips the terminator and `text`
    re-imposes `"\\n"`, so thirteen byte-distinct spellings of one bullet share an id.
    Kept rather than fixed: a CRLF entry and an LF entry naming the same bullet should
    name it with the same id. The hazard is a future "restore byte-exactness", which
    would silently invalidate every id ever printed.
    """
    terminators = [chr(c) for c in (0x0A, 0x0D, 0x0B, 0x0C, 0x1C, 0x1D, 0x1E, 0x85)]
    terminators += [chr(0x0D) + chr(0x0A), chr(0x2028), chr(0x2029)]
    bodies = ["- one" + t for t in terminators] + ["- one", "- one" + chr(0x0A) * 2]
    assert len(set(bodies)) == 13, "the premise moved: the bodies are not all distinct"
    ids = {parse_journal_bullets(b)[0].citation_id for b in bodies}
    assert ids == {"3a98d5a7"}, (
        f"thirteen spellings of one bullet gave {len(ids)} id(s) ({sorted(ids)}); the "
        "terminator normalisation the docstring declares has changed")


def test_TRAILING_WHITESPACE_does_change_the_id():
    """The other half of the claim above, and the reason it is not "whitespace is
    normalised": nothing strips a trailing space, so two bullets that LOOK identical
    get different ids. Part of the declared invalidation set."""
    plain = parse_journal_bullets("- one" + chr(0x0A))[0].citation_id
    padded = parse_journal_bullets("- one" + chr(0x20) * 3 + chr(0x0A))[0].citation_id
    assert plain == "3a98d5a7" and padded == "7adcabe5", (plain, padded)


def test_the_id_is_NOT_SCOPED():
    """🔴 THE CLAIM THE DESIGN'S OWN FRAMING OVERSTATED. "A token existing nowhere
    else in a corpus" does not hold for duplicated TEXT: two byte-identical bullets in
    different entries or scopes collide deterministically, because `append_bullet`
    dedupes within ONE file only. Pinned so the docstring's correction is checkable.

    ⚠ AND THIS IS ONE OF **TWO** FAILURE MODES OF THAT FRAMING — NAMED HERE BECAUSE THIS
    TEST COVERS ONLY THIS ONE, AND A READER WHO FINDS IT WILL OTHERWISE READ IT AS THE
    WHOLE CORRECTION. The second is the CITATION ECHO: a writer who quotes `[cb:…]` inside
    new prose has that token stored verbatim (only a TRAILING token is stripped), so the
    token enters the corpus permanently and a later search cannot tell a use from a
    quotation. It is declared at `citation_id`'s docstring and in `README.md`; it is NOT
    pinned by a test anywhere, deliberately — the consequence is a rule for a DOWNSTREAM
    consumer ("take the LAST `[cb:…]` on a line"), not behaviour this tree implements, and
    the one piece of behaviour it rests on is already pinned by
    `internal/write`'s `TestACitationTokenInProseIsNotStripped`.
    """
    a = parse_journal_bullets("## one entry\n- one\n")[0].citation_id
    b = parse_journal_bullets("- one\n")[0].citation_id
    assert a == b, "the premise moved: the id now depends on something outside the bullet"


def test_a_NON_newline_break_shows_why_the_SPLITTER_matters():
    """⚠ THE HAZARD `start_line`'s DOCSTRING NAMES, AS A MEASUREMENT. The index is
    into `splitlines()`, which breaks on ten characters; a consumer splitting on `"\\n"`
    gets a different list and the index lands mid-line. `internal/ui/render.go`'s
    `inlineCode` already splits its text on `"\\n"`, so this is not hypothetical."""
    body = "- a\rb\n- c\n"
    bullets = parse_journal_bullets(body)
    assert [b.start_line for b in bullets] == [0, 2]
    right = body.splitlines()
    wrong = body.split("\n")
    assert right == ["- a", "b", "- c"], right
    assert wrong == ["- a\rb", "- c", ""], wrong
    for b in bullets:
        assert right[b.start_line] == b.first_line
    # The wrong splitter misses the second bullet entirely, which is the damage.
    assert wrong[bullets[1].start_line] == "", (
        "this body no longer demonstrates the hazard, so the docstring's third ⚠ is "
        "unguarded — pick one that still does")
