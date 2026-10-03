#!/usr/bin/env python3
"""The `[cb:xxxxxxxx]` token on the PYTHON read surface — the half `internal/report`
cannot test.

🔴 WHY TWO SPELLINGS OF ONE ANNOTATOR EXIST, AND WHY A GATE IS THE ANSWER RATHER THAN A
FIX. `packages.cairn` installs the Python client and `lib/` under `libexec` and nothing
else, so `lib/subsystem_recall.py` CANNOT import `internal/report`. That is packaging, not
a design choice anybody may undo here — the identical constraint
`tests/test_env_aliases.py` and `tests/test_nuance_bullet_ceiling.py` exist under. This
file is that instrument for the citation token.

## The design this file is the proof of

Both renderers emit a surfaced section's body LINE BY LINE VERBATIM and append the token
to the lines that OPEN a top-level bullet. They do NOT re-emit the body from
`parse_journal_bullets`' groups, and THAT is the whole point: the parser DROPS text before
the first bullet and ABSORBS a bullet whose opening line was lost or indented. Today those
are an advisory `--validate` finding at exit 0; under a reconstruction they would be
SILENT DELETION on the read surface, and the measured history is 7 versions across two
entries carrying dropped lines for 2–8 days with that validator green the whole time.

So a test that only checks ids were appended would pass the dangerous implementation too —
and that is TRUE OF A SECTION, not of either test here. Measured at `902be517` with the
reconstruction installed in both languages: BOTH
`test_every_surfaced_sections_bullet_openings_carry_a_citation_id` and
`test_the_body_keeps_every_original_byte_even_where_the_parser_mishandles_it` fail. What
passes it is the Go twin's `## Nuance / work-history` and `## Requirements` SUBTESTS, whose
bodies are entirely absorbed into bullet groups and therefore reproduce line for line. So
the sentence above is a claim about COVERAGE SCOPE: a world of nothing but clean bullets
cannot see the reconstruction, which is why both worlds here carry a prose-only section and
a section with pre-bullet prose. An earlier draft of this paragraph named the id test as the
one that would pass; that was measured false and is retracted.

## What it structurally cannot see

It reads the GO spelling of the token as TEXT, so it cannot see a Go-side wiring mistake
that still produces a well-formed token — the annotator keyed on the wrong index, say.
Three other things close that and none is here: `internal/report`'s own guards,
`internal/report/testdata/reader_fixtures.json` (the ORACLE's rendered bytes replayed
against the Go renderer), and `tests/parity/harness.py` (both real clients over one cache
root). Said plainly rather than left to be discovered.
"""

from __future__ import annotations

import importlib.util
import re
import sys
from pathlib import Path

REPO = Path(__file__).resolve().parents[1]
GO_STORE_SOURCE = REPO / "internal" / "store" / "journal.go"
GO_REPORT_SOURCE = REPO / "internal" / "report" / "text.go"
SERVER_PATH = REPO / "server" / "server.py"

sys.path.insert(0, str(REPO / "lib"))

import subsystem_recall as rc  # noqa: E402
from subsystem_resolver import (  # noqa: E402
    citation_ids_by_start_line,
    citation_token,
    extract_sections,
    parse_journal_bullets,
)

SCOPE = "alpha-notes"

#: The rendered token as a READER sees it, spelled out rather than built from
#: `citation_token`: a pattern derived from the implementation agrees with it by
#: construction and could not fail when the format moves.
TOKEN_RE = re.compile(r" \[cb:[0-9a-f]{8}\]$")

#: Every line here is a line the ruled-out reconstruction loses or relocates: the two
#: prose lines precede the first bullet and are DROPPED from the bullet list; the indented
#: dash is ABSORBED into the bullet above it; the fenced dash is sample text that still has
#: to print, fence markers and all.
MISHANDLED_NUANCE = "\n".join(
    [
        "A paragraph somebody wrote under the heading before any bullet.",
        "It wraps onto a second line.",
        "",
        "- 2000-01-02: the first real bullet.",
        "  - an indented dash the parser absorbs into the bullet above",
        "",
        "- 2000-01-03: the second real bullet.",
        "```",
        "- a dash inside a fence",
        "```",
        "trailing prose after the fence",
    ]
)

MISHANDLED_POINTERS = "\n".join(
    [
        "orienting prose before the first pointer row",
        "- apps/gadget-one/values.yaml — the chart values",
    ]
)


def _entry(*, what: str, pointers: str, nuance: str, requirements: str | None = None) -> str:
    lines = [
        "---",
        "service: gadget-one",
        f"scope: {SCOPE}",
        "sensitivity: public",
        "---",
        "",
        "## What it is",
        "",
        what,
        "",
        "## Pointers",
        "",
        pointers,
        "",
        "## Nuance / work-history",
        "",
        nuance,
        "",
    ]
    if requirements is not None:
        lines += ["## Requirements", "", requirements, ""]
    return "\n".join(lines)


def _world(root: Path, body: str) -> Path:
    store = root / "s"
    (store / SCOPE).mkdir(parents=True)
    (store / SCOPE / "gadget-one.md").write_text(body, encoding="utf-8")
    return store


def _section_bodies(rendered: str) -> dict[str, list[str]]:
    """Each surfaced section's RENDERED body lines, six-space indent still attached.

    A heading line ends the previous section, and so does the `  ### ` entry line, so a
    notice printed after the last body cannot be mistaken for part of it.
    """
    out: dict[str, list[str]] = {}
    cur: str | None = None
    for line in rendered.splitlines():
        if line.startswith("  ### "):
            cur = None
            continue
        if line.startswith("    ") and line[4:] in rc.SURFACED_HEADINGS:
            cur = line[4:]
            continue
        if cur is None:
            continue
        if not line.startswith("      "):
            cur = None
            continue
        out.setdefault(cur, []).append(line)
    return out


def test_every_surfaced_sections_bullet_openings_carry_a_citation_id(tmp_path: Path) -> None:
    """REGRESSION guard, not an invariant guard: no surface printed a citation id at all
    before this change. Watched RED at `d7e1fec8`, where all four sections render zero
    tokens; green at HEAD.

    🔴 ALL FOUR SURFACED SECTIONS, BECAUSE THE SCOPE DECISION IS "EVERY SURFACED SECTION"
    AND A FIXTURE CARRYING ONLY NUANCE CANNOT SEE A RENDERER THAT NARROWED TO IT.
    `## Pointers` is the one that matters most: its rows are `- path — description`, NOT
    `- YYYY-MM-DD: prose`, so an annotator keyed off a DATE rather than off the bullet
    grammar annotates nothing there while every nuance row still gets a token. Measured
    over a real 344-entry store, `## Pointers` carries 1,589 of the 4,978 ids a full read
    would print — 32% of them — so narrowing would annotate the section where the signal
    is not. ⚠ That total is ONE reading of a LIVE store and will not re-derive; what this
    test pins instead is the four-section coverage, in literals, over a synthetic world.

    🔴 IT ASSERTS THE VALUE AND THE NEGATIVE HALF, NOT MERELY THE SHAPE. A renderer that
    appended a CONSTANT token, or annotated the wrong line, satisfies "ends in ` [cb:`"
    perfectly; and a renderer that annotated EVERY line satisfies the positive half on
    every row.
    """
    pointers = "\n".join(
        [
            "- apps/gadget-one/values.yaml — the chart values",
            "  a wrapped continuation of the row above",
            "  - an INDENTED dash, which is a continuation and not a bullet",
            "- `internal/widget/widget.go` — the loader",
        ]
    )
    nuance = "\n".join(
        [
            "- 2000-01-02: the retry budget is still unbounded.",
            "  wrapped prose under it",
            "- 2000-01-03: OPEN: the export should stream.",
            "```",
            "- a dash inside a fence, which is sample text",
            "```",
        ]
    )
    requirements = "\n".join(
        [
            "- OPEN: the archive should keep its original timestamps",
            "- RESOLVED def5678: (operator) the listing carries a freshness stamp",
        ]
    )
    store = _world(
        tmp_path,
        _entry(
            what="Prose that opens no bullet at all.",
            pointers=pointers,
            nuance=nuance,
            requirements=requirements,
        ),
    )
    report = rc.recall(store, SCOPE, mode="full")
    rendered = rc.render_text(report)
    bodies = _section_bodies(rendered)
    sections = report.entries[0].sections

    #: Per-section EXPECTED token counts, as LITERALS. A derived count agrees with a
    #: renderer that annotated nothing in a section the parser finds no bullets in.
    want_tokens = {
        rc.WHAT_HEADING: 0,  # prose only — the honest zero, and the control below
        rc.POINTERS_HEADING: 2,  # two `- path — description` rows
        rc.NUANCE_HEADING: 2,  # the fenced dash is sample text, so NOT three
        rc.REQUIREMENTS_HEADING: 2,
    }

    for heading in rc.SURFACED_HEADINGS:
        body = sections[heading]
        lines = body.splitlines()
        got = bodies[heading]
        assert len(got) == len(lines), (
            f"{heading} rendered {len(got)} body lines for a {len(lines)}-line body — the "
            f"body must print line for line:\n" + "\n".join(got)
        )
        ids = citation_ids_by_start_line(body)
        seen = 0
        for i, line in enumerate(lines):
            want = f"      {line}"
            if i in ids:
                want += f" [cb:{ids[i]}]"
                seen += 1
            assert got[i] == want, f"{heading} line {i}:\n  got  {got[i]!r}\n  want {want!r}"
        assert seen == want_tokens[heading], (
            f"{heading} carried {seen} citation tokens, want {want_tokens[heading]} — the "
            "world was written to produce exactly that many"
        )

    # 🔴 THE CONTROL THAT STOPS THE `## What it is` ROW BEING A HARNESS WIRED TO NOTHING. A
    # zero is indistinguishable from "this test never looked at that section", so the
    # non-zero rows have to be observable in the SAME read: four sections, three annotated,
    # one deliberately not.
    total = rendered.count(" [cb:")
    assert total == 6, (
        f"the whole render carried {total} tokens, want 6 (0 + 2 + 2 + 2) — a zero in the "
        "prose section only means something if the other three moved"
    )
    assert "[cb:" not in "\n".join(bodies[rc.WHAT_HEADING])


def test_the_body_keeps_every_original_byte_even_where_the_parser_mishandles_it(
    tmp_path: Path,
) -> None:
    """THE safety property the whole design exists for: the body survives annotation BYTE
    FOR BYTE AND IN ORDER.

    ⚠ IT IS NOT "THE ONE TEST THAT TELLS THE SHIPPED IMPLEMENTATION FROM THE RULED-OUT
    ONE", WHICH IS WHAT THIS LINE USED TO SAY. Measured: the id test above fails under the
    reconstruction too — see the matrix. What is unique here is the CLAIM, not the kill:
    this is the only test asserting the rendered body reconstructs to the original bytes in
    order, which a line COUNT (what the id test checks first) cannot do.

    🔴 THE MATRIX, RE-DERIVED — AND BOTH HALVES OF THE EARLIER ONE WERE WRONG. It said this
    guard is "GREEN at `d7e1fec8` … an INVARIANT guard [that] must not be counted as
    regression coverage", and that the ruled-out implementation reds it while
    `test_every_surfaced_sections_bullet_openings_carry_a_citation_id` "stays GREEN".
    Measured at `902be517`:

    * IT IS REGRESSION COVERAGE, NOT AN INVARIANT GUARD. With the emission loop reverted
      to base's shape — the narrowest expression — this test FAILS at its token-count
      floor (`tests/test_citation_render.py`'s last assertion): "this world must render 3
      tokens …, got 0". Its own anti-vacuity assertion is what makes it red at base, so
      the verbatim-body claim never gets to hold vacuously. ⚠ AND AT A LITERAL `d7e1fec8`
      CHECKOUT THE CLAIM IS NOT REACHABLE AT ALL: neither `citation_token` nor
      `citation_ids_by_start_line` exists there, so this module's imports fail. The
      measurement above is the only reading of "at base" that has an answer.

    * THE ID TEST DOES NOT STAY GREEN UNDER THE RECONSTRUCTION. With `render_text`'s loop
      replaced by `for b in parse_journal_bullets(body): for line in b.lines: …`, BOTH
      tests fail — each with `KeyError: '## What it is'`, the id test at its `bodies`
      lookup and this one at its own. A prose-only section emits NO body lines under the
      reconstruction, so `_section_bodies` never creates that key. (The Go twins report the
      same defect as a length mismatch rather than a KeyError, because a nil Go map yields
      an empty slice instead of raising.)

    ⚠ SO WHAT IS LEFT OF "A TEST THAT ONLY CHECKS IDS WERE APPENDED WOULD PASS THE
    DANGEROUS IMPLEMENTATION TOO"? It holds, and the measurement is its own example — but
    the weaker guard is a SUBTEST, not this file's id test. In the Go twin, which runs its
    four sections as subtests, `## Nuance / work-history` and `## Requirements` both PASSED
    under the reconstruction: those two bodies are entirely absorbed into bullet groups, so
    the reconstruction reproduces them line for line and the ids land correctly. A test
    scoped to a section like that, asserting only that bullet openings carry ids, passes
    the implementation this design rules out — which is why the worlds here carry a
    prose-only section and a pre-bullet-prose section at all.

    ⚠ IT IS A CLAIM ABOUT CONTENT, NOT ABOUT IDS. A body line carrying no token still has
    to be there in full. A MISSING ID IS A DEGRADATION IN COVERAGE AND NEVER IN CONTENT; a
    missing LINE is the thing this refuses.
    """
    store = _world(
        tmp_path,
        _entry(
            what="Prose that opens no bullet at all.",
            pointers=MISHANDLED_POINTERS,
            nuance=MISHANDLED_NUANCE,
        ),
    )
    report = rc.recall(store, SCOPE, mode="full")
    rendered = rc.render_text(report)
    bodies = _section_bodies(rendered)
    sections = report.entries[0].sections

    # The world must actually CONTAIN the mishandled shapes, or this test is a harness
    # wired to nothing — a positive control on the FIXTURE rather than on the renderer.
    for heading, want_dropped in (
        (rc.POINTERS_HEADING, 1),  # the one orienting prose line
        (rc.NUANCE_HEADING, 3),  # two prose lines plus the blank after them
    ):
        ids = citation_ids_by_start_line(sections[heading])
        assert ids, f"{heading} parsed no bullets at all"
        assert min(ids) == want_dropped, (
            f"{heading}: the fixture's first bullet is at body line {min(ids)}, want "
            f"{want_dropped} — this test cannot see the dropped-lines defect unless lines "
            "PRECEDE the first bullet"
        )

    for heading in rc.SURFACED_HEADINGS:
        body = sections.get(heading)
        if not body:
            continue
        want = body.splitlines()
        got = bodies[heading]
        assert len(got) == len(want), (
            f"{heading}: {len(got)} rendered body lines for a {len(want)}-line body. Lines "
            "went missing, which is the one failure this design exists to prevent."
        )
        # 🔴 THE RECONSTRUCTION, IN REVERSE: strip the six-space indent and any trailing
        # token, and the result must be the body BYTE FOR BYTE AND IN ORDER. That is a
        # stronger claim than "every original line appears somewhere" — a reconstruction
        # that merely REORDERED lines, or printed one twice, passes a containment check.
        rebuilt = []
        for i, line in enumerate(got):
            assert line.startswith("      "), f"{heading} line {i} is not a body line: {line!r}"
            rebuilt.append(TOKEN_RE.sub("", line[6:]))
        assert rebuilt == want, (
            f"{heading}: the body did not survive annotation verbatim.\n  rebuilt:\n"
            + "\n".join(rebuilt)
            + "\n  original:\n"
            + "\n".join(want)
        )

    # And the annotation really happened on this world, so the loop above is not passing
    # because nothing was added.
    assert rendered.count(" [cb:") == 3, (
        "a verbatim-body assertion over an UNANNOTATED render is vacuous; this world must "
        "render 3 tokens (one pointer row, two nuance bullets), got "
        f"{rendered.count(' [cb:')}"
    )


def test_the_token_lands_on_the_line_the_splitter_says_it_does(tmp_path: Path) -> None:
    """Drives a lone `\\r` through the WHOLE read path — an entry file on disk, `recall`,
    `render_text` — and pins the rendered body as whole normalised lines.

    🔴 AND IT CANNOT SEPARATE THE TWO SPLITTERS. AN EARLIER DRAFT OF THIS DOCSTRING CLAIMED
    IT COULD, AND THAT CLAIM WAS MEASURED FALSE RATHER THAN ARGUED AWAY. It read "a lone
    `\\r` is what separates the two splitters … a mutant swapping the splitter cannot pass
    by spelling a substring". The `\\r` never reaches a renderer: `extract_sections` builds
    a section body by splitting the FILE and re-joining with `"\\n"`, so every other break
    character is gone before `render_text` sees a body. Measured on the entry this test
    writes — the body arrives as `'- a\\nb\\n- c'`, for which `splitlines()` and
    `split("\\n")` return the SAME list. Mutation-proven at `902be517`: swapping
    `render_text`'s emission splitter to `body.split("\\n")` left
    `pytest tests/test_citation_render.py tests/test_citation_ids.py` at 17 passed, 0
    failed — this test among them. `test_the_RENDERERS_own_splitter_is_what_places_the_token`
    below is what actually separates them, by handing `render_text` a body DIRECTLY.

    ⚠ SO WHAT IS THIS TEST FOR? The end-to-end path, which the direct-body test deliberately
    skips: an entry on disk, through `recall`, renders its bullet openings annotated and its
    continuation line not. It is kept, re-labelled, and NOT counted as splitter coverage.

    ⚠ THE EXPECTED TOKEN IS SPELLED HERE RATHER THAN BUILT FROM `citation_token`, for the
    reason `TOKEN_RE` is: a format derived from the implementation agrees with it by
    construction and could not fail when the format moves. Only the 8 hex characters come
    from the derivation, because they are a `sha256` nobody can spell by hand.

    Watched RED at `d7e1fec8`: at base no token is emitted, so the expected bytes carry
    two tokens that are simply absent.
    """
    body = "- a\rb\n- c"
    ids = citation_ids_by_start_line(body)
    assert len(ids) == 2, f"the body parsed to {len(ids)} bullets, want 2: {ids}"
    assert 2 in ids, f"no bullet starts at body line 2, so `splitlines()` is not what the parser used: {ids}"

    entry = "\n".join(
        [
            "---",
            "service: gadget-one",
            f"scope: {SCOPE}",
            "sensitivity: public",
            "---",
            "",
            "## Nuance / work-history",
            "",
            body,
            "",
        ]
    )
    store = _world(tmp_path, entry)

    # 🔴 THE PREMISE THIS TEST RESTS ON, ASSERTED RATHER THAN ASSUMED — the retraction
    # above, made mechanical. If `extract_sections` ever STOPS collapsing the `\r`, the
    # sentence "this test cannot separate the splitters" becomes false and somebody has to
    # re-read both docstrings.
    extracted = extract_sections(entry, rc.SURFACED_HEADINGS)[rc.NUANCE_HEADING]
    assert "\r" not in extracted, (
        f"`extract_sections` preserved the lone \\r ({extracted!r}) — this docstring says "
        "it does not, and the direct-body test exists because of that. Re-read both."
    )

    report = rc.recall(store, SCOPE, mode="full")
    got = _section_bodies(rc.render_text(report))[rc.NUANCE_HEADING]
    want = [
        f"      - a [cb:{ids[0]}]",
        "      b",
        f"      - c [cb:{ids[2]}]",
    ]
    assert got == want, "the body annotated on the wrong line boundary"


def test_the_RENDERERS_own_splitter_is_what_places_the_token() -> None:
    """The guard `render_text`'s emission comment claims exists: swap that loop's
    `splitlines()` for `body.split("\\n")` and THIS test goes red.

    🔴 IT HANDS `render_text` A BODY DIRECTLY, AND THAT IS THE WHOLE DESIGN RATHER THAN A
    SHORTCUT. Every other test in this file reaches the renderer through `recall`, which
    reaches `extract_sections`, which splits the FILE and re-joins with `"\\n"` — so a lone
    `\\r` is already gone by the time any splitter runs and the two splitters cannot
    disagree. The hazard is only reachable where a body arrives WITHOUT that normalisation,
    which a `RecalledEntry` constructed in-process is. So this needs no store, no file and
    no `tmp_path`.

    🔴 WHY A LONE `\\r`, AND WHAT EACH SPLITTER DOES WITH IT. On `'- a\\rb\\n- c'`:
    `splitlines()` gives `['- a', 'b', '- c']` and `parse_journal_bullets` reports
    start_lines `[0, 2]`; `split("\\n")` gives `['- a\\rb', '- c']`, so the index-0 entry
    lands MID-LINE (after `b`) and index 2 does not exist at all. `splitlines()` breaks on
    TEN characters and `"\\n"` is one of them, which is exactly why every body that has
    been through `extract_sections` hides the difference.

    🔴 THE MATRIX. RED at `902be517` with the splitter swapped — measured, with the
    mutation isolated to that one expression. GREEN at `902be517` unmodified. ⚠ Against
    `d7e1fec8` the question does not arise: neither `citation_token` nor
    `citation_ids_by_start_line` exists there, so this module's imports fail.

    ⚠ IT IS A POSITION CLAIM, NOT A HASH CLAIM — the 8 hex come from the derivation under
    test. What pins the hash is `tests/test_citation_ids.py` against the GO answers; what
    pins the FORMAT is the literal ` [cb:` spelled below and
    `test_the_rendered_citation_token_is_stripped_by_the_PODS_write_path`.

    ⚠ AND IT READS THE **PYTHON** RENDERER ONLY. The Go twin is
    `report.TestTheRENDERERSOwnSplitterIsWhatPlacesTheToken`; `lib/` cannot import
    `internal/`, so neither can stand in for the other.
    """
    body = "- a\rb\n- c"
    ids = citation_ids_by_start_line(body)
    assert len(ids) == 2, (
        f"the body parsed to {len(ids)} bullets, want 2 — this case cannot see a splitter "
        f"difference otherwise: {ids}"
    )
    assert 2 in ids, f"no bullet starts at body line 2: {ids}"
    # 🔴 THE POSITIVE CONTROL ON THE FIXTURE, because this whole test is vacuous over a
    # body the two splitters agree on: they must DISAGREE on these bytes, here, before any
    # claim about which one the renderer picked means anything.
    assert len(body.splitlines()) != len(body.split("\n")), (
        f"the two splitters agree on {body!r}, so this test cannot see which one the "
        "renderer used"
    )

    report = rc.RecallReport(
        status="recalled",
        scope=SCOPE,
        store_root="/store",
        entries=(
            rc.RecalledEntry(
                ref="gadget-one",
                filename="gadget-one.md",
                sensitivity="public",
                # The body as the renderer receives it — NOT through `extract_sections`.
                sections={rc.NUANCE_HEADING: body},
            ),
        ),
        total_in_scope=1,
    )
    got = _section_bodies(rc.render_text(report))[rc.NUANCE_HEADING]
    want = [
        f"      - a [cb:{ids[0]}]",
        "      b",
        f"      - c [cb:{ids[2]}]",
    ]
    assert got == want, (
        "the renderer did not split the body with `splitlines()`, so the index from "
        f"`citation_ids_by_start_line` named the wrong line\n  got  {got}\n  want {want}"
    )


def test_the_rendered_citation_token_is_stripped_by_the_PODS_write_path() -> None:
    """The SEAM between the read surface's FORMAT and the pod's strip PATTERN, which
    neither side's own tests can see.

    🔴 A FORMAT STRING IS NOT A REGEXP, SO NOTHING CAN DERIVE ONE FROM THE OTHER. Both
    sides were hermetically tested before this guard existed — `server.py`'s battery over
    hand-written `[cb:deadbeef]` strings, the renderer's over whatever its own helper
    emits — and every one of those fixtures spelled the token BY HAND. The defect this
    refuses lives in the gap: a renderer printing `[cb: deadbeef]`, or `[CB:…]`, or
    dropping the leading space, passes everything on both sides and silently leaves the
    token in `content_hash`.

    🔴 WHY THAT MATTERS, MEASURED RATHER THAN FEARED: a downstream consumer's resume flow
    requires its report to echo the bullets it recalled, which fires on 451 of 454 runs
    (99.3%). A token left in the hash breaks idempotency in the direction that destroys
    nothing and is therefore silent — the re-POST stops matching the bullet on disk,
    appends a near-duplicate, and that duplicate carries a `[cb:…]` in its PROSE naming
    the bullet it was COPIED FROM, forever.

    ⚠ INVARIANT GUARD AGAINST `d7e1fec8`, NOT REGRESSION COVERAGE: at base the renderer
    printed no token, so no echo existed and nothing could have violated this. It becomes
    reachable in this change.
    """
    spec = importlib.util.spec_from_file_location("cairn_server_citation_seam", SERVER_PATH)
    assert spec and spec.loader
    api = importlib.util.module_from_spec(spec)
    sys.modules[spec.name] = api
    spec.loader.exec_module(api)

    # 🔴 THE ID COMES FROM THE REAL DERIVATION, NOT FROM A HAND-SPELLED 8 HEX, because the
    # hazard is precisely that a hand-spelled fixture agrees with both sides while the live
    # value does not.
    bullets = parse_journal_bullets("- 2000-01-02: the retry budget is still unbounded.")
    assert len(bullets) == 1
    stored = bullets[0].first_line
    echoed = stored + citation_token(bullets[0].citation_id)

    assert api.bullet_content([echoed]) == api.bullet_content([stored]), (
        "an echoed citation token survived the pod's strip, so a re-POST of a recalled "
        "bullet will not match the bullet on disk"
    )
    # ⚠ NOT THE ASSERTION — THE CONTROL. `content_hash` is taken over the RAW submitted
    # text, so these two must DIFFER; if they did not, the comparison above could pass
    # against a strip that does nothing because the inputs were already equal.
    assert api.content_hash(echoed) != api.content_hash(stored), (
        "the raw hashes of the echoed and stored lines are equal, so this fixture cannot "
        "tell a working strip from no strip"
    )

    # 🔴 THE ORDER-FREE HALF, REACHABLE AND NOT HYPOTHETICAL: the read surface appends its
    # token AFTER a stored line that already ends in a write attribution, so the trailer
    # run is `[cairn: …] [cb:…]` in that order on every appended bullet in a real store.
    attributed = api.render_bullet(
        "the retry budget is still unbounded.",
        actor="fixture-actor",
        session="sess-0000000000000001",
        today="2000-01-02",
    )
    attributed_bullets = parse_journal_bullets(attributed)
    assert len(attributed_bullets) == 1
    both = attributed + citation_token(attributed_bullets[0].citation_id)
    assert api.bullet_content([both]) == api.bullet_content([attributed])

    # 🔴 THE NEGATIVE CONTROL ON THE STRIP: a trailer the pattern cannot READ is not a
    # trailer and must stay in the hash. Without this, a pattern widened to
    # `\\[cb:[^]]*\\]` — which would swallow arbitrary bracketed prose — passes everything
    # above.
    #
    # ⚠ TWO SPELLINGS, NOT SIX, BECAUSE FOUR WERE ALREADY COVERED TWICE OVER. This loop
    # read six; `tests/test_bullet_trailer_strip.py`'s named near-miss test asserts the
    # upper-case, seven-hex, nine-hex and non-hex classes over the SAME function, and
    # `TRAILER_STRIP_AXIS_PIECES` sweeps the whole near-miss class over 84,210 inputs with
    # a cross-language digest — including `[cb:deadbee]`, `[cb:deadbeeff]`,
    # `[cb:DEADBEEF]`, an unclosed `[cb:deadbeef` and a bare `cb:deadbeef]`. The two
    # below are the only spellings in the original six that NO other test sends. What
    # earns this block its place is not the spellings at all — it is that `stored` above
    # comes from the real derivation and the real format rather than a hand-spelled
    # literal, and that is untouched by the deletion.
    for bad in (
        " [cb: deadbeef]",  # a space inside
        " (cb:deadbeef)",  # the wrong brackets
    ):
        assert api.bullet_content([stored + bad]) != api.bullet_content([stored]), (
            f"{bad!r} was stripped as a citation token, but it is not one — the pattern is "
            "wider than the format"
        )


def test_the_two_renderers_spell_the_token_identically() -> None:
    """The two FORMATS, pinned against each other as whole normalised strings.

    `lib/` cannot import `internal/`, so the Go side is read as TEXT. A format that moved
    on one side only does not error, does not drop an entry and does not change an exit
    code — it changes one trailer on some lines, which reads as a stale cache, and it is
    exactly the drift `tests/parity/` was built for. This is the cheaper, earlier half:
    parity needs a live pod and a store that happens to hold a bullet.
    """
    assert citation_token("deadbeef") == " [cb:deadbeef]", "the Python format moved"
    assert len(citation_token("deadbeef")) == 14, (
        "the token is not 14 bytes; every cost claim in this tree says 14"
    )

    go = GO_STORE_SOURCE.read_text(encoding="utf-8")
    # The whole function body as the Go source writes it — matched as a complete
    # statement rather than by hunting for `[cb:`, which also appears in the doc comments
    # above it and in `internal/write`'s strip pattern.
    match = re.search(
        r'^func CitationToken\(id string\) string \{ return " \[cb:" \+ id \+ "\]" \}$',
        go,
        re.MULTILINE,
    )
    assert match, (
        "`internal/store.CitationToken` is not the one-line function this test knows how "
        "to read, so the two formats are no longer pinned against each other — re-derive "
        "the comparison rather than loosening the pattern"
    )
    # The POSITIVE CONTROL on that pattern: it must be able to MISS. Without this, a typo
    # in the expression above makes the assertion unfalsifiable. ⚠ KEPT RATHER THAN DELETED
    # AS CEREMONY: this assertion's instrument is a hand-written regex over SOURCE TEXT,
    # which is the one class in this tree where a wrong pattern reports a confident PASS
    # and no other gate notices.
    assert not re.search(
        r'^func CitationToken\(id string\) string \{ return " \[cb\]" \+ id \+ "\]" \}$',
        go,
        re.MULTILINE,
    )

    # And the Go RENDERER reaches the format through that function rather than open-coding
    # it — one spelling per language is the claim, and an open-coded `" [cb:"` in text.go
    # would satisfy every byte comparison while leaving two formats to drift.
    report_go = GO_REPORT_SOURCE.read_text(encoding="utf-8")
    assert "store.CitationToken(id)" in report_go, (
        "`internal/report`'s emission site no longer calls `store.CitationToken`"
    )
    code = "\n".join(
        line for line in report_go.splitlines() if not line.lstrip().startswith("//")
    )
    assert '" [cb:"' not in code, (
        "`internal/report` open-codes the token format; it must go through "
        "`store.CitationToken` so there is one spelling per language"
    )
