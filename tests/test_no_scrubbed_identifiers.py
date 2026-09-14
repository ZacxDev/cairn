"""The extraction scrub left prose where a RUNNABLE IDENTIFIER has to be.

THE DEFECT (measured at `c536c52`)
-----------------------------------
This repo was extracted from a private one, and the extraction replaced the
writer module's real name with the prose phrase `a writer`. In a SENTENCE that
reads correctly and is exactly right -- "a reader and a writer that disagree here
send two operators to two different answers" is good prose and must stay.

Inside a CODE SPAN or a PATH it is broken, and it was broken at four sites:

  * `lib/subsystem_recall.py` told an operator, twice, to
    ``check a file with `a writer --validate <path>` `` -- a command with no
    program to run. Both strings are printed at the moment a MALFORMED entry is
    found, i.e. the one moment the reader has a real problem and is looking for
    the fix.
  * the same module's docstring named the writer's module path as ``lib/a writer``.
  * `cairn`'s `cmd_validate` docstring read "which is a **the writer half** flag",
    where the replacement landed inside a sentence built around the old name.

🔴 AND THE REMEDY WAS WRONG ABOUT THIS PACKAGE, NOT MERELY MIS-SPELLED. Fixing
the name alone would still have been wrong: `cmd_validate` takes `--scope`,
`--repo` and `--no-sync` and has NO single-file path, so "check a file with
<anything>" named a capability the CLI does not expose. The corrected strings
name `cairn validate --scope <scope>`, which does run here and names each file
that fails to parse.

⚠ AND THE FIRST FIX OVERCORRECTED INTO THE SAME CLASS, WHICH IS WHY THIS
PARAGRAPH EXISTS. It replaced the dead command with the sentence "a single-FILE
check belongs to the writer half, which this package does not ship" — and that is
FALSE. `subsystem_resolver.entry_mapping` + `SubsystemEntry.from_mapping` IS a
single-file check, it ships here, and `server/server.py:2570` already runs it to
REFUSE a malformed PUT. So a remedy naming a capability the package lacks was
swapped for a dis-claim of a capability it HAS — the same defect, inverted, in
the same two operator-facing strings. Both sentences are gone; the remedy now
says only what it can demonstrate.

WHAT THIS TEST IS
-----------------
REGRESSION, and a CLASS guard rather than four string pins. It fails on the
SHAPE -- the scrub phrase inside a code span or a path -- so the next blind
replacement is caught wherever it lands, including in files that do not exist
yet. It deliberately does NOT flag `a writer` in prose, because that is the
correct English and the repo is full of it.

🔴 THE ENUMERATION IS DERIVED, NOT LISTED. It reuses `leakscan.partition_tracked_files`,
which buckets every tracked file so `scanned | skipped` equals the enumeration by
construction. A hand-written suffix list here would reintroduce exactly the defect
that function was written to close: a scanner that prints "0 findings across N
files" where N is files SCANNED, so nothing in the output distinguishes *clean*
from *did not look*.
"""
import re
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))

import leakscan  # noqa: E402
from leakscan import partition_tracked_files  # noqa: E402

#: The phrases the extraction substituted for the writer module's real name.
#: There is more than one, which is what the first version of this guard got
#: wrong — see the note on `_IN_CODE_SPAN`.
SCRUB_PHRASES = ("a writer", "the writer half")

#: Two broken shapes, and ONLY these two:
#:   1. the phrase inside a backticked code span -- `... a writer ...`
#:   2. the phrase used as a path segment -- lib/a writer, scripts/a writer
#: Prose is untouched by both, which is the point: `a writer` in a sentence is
#: correct English and this repo uses it correctly in many places.
#
# 🔴 THE SCRUB USED MORE THAN ONE REPLACEMENT STRING, AND THE FIRST VERSION OF
# THIS GUARD ONLY KNEW ONE. It matched `a writer` and called itself a CLASS
# guard, while EIGHT sibling sites survived: four code spans reading
# `the writer half --validate`, three naming `entry_shape.build_report` (a symbol
# that does not exist in this package), and one line of prose reading "the full
# the writer half's own suite". A class claim you half-implement is WORSE than
# four string pins, because the guard's presence stops the next reader looking.
# Both phrases are matched now, and `test_the_patterns_can_fire` carries a
# fixture for each.
_IN_CODE_SPAN = re.compile(r"`[^`\n]*\b(?:a writer|the writer half)\b[^`\n]*`")
_AS_PATH = re.compile(r"[\w./-]+/(?:a writer|the writer half)\b")
#: "the full the writer half's own suite" — a determiner left stranded in front
#: of the replacement, the same shape as `_STRANDED_DETERMINER` but with `the`.
_DOUBLED_DETERMINER = re.compile(r"\bthe\s+(?:full\s+)?the\s+writer\s+half\b")

#: The scrub also landed inside sentences built around the old NAME, leaving a
#: determiner stranded in front of the replacement (`a **the writer half** flag`).
#: That is not a code span, so the two patterns above cannot see it.
_STRANDED_DETERMINER = re.compile(r"\b(?:a|an)\s+\*{0,2}the\s+writer\s+half\*{0,2}")


#: The one file the scan does not read, because it CONTAINS the fixtures — every
#: broken shape is quoted in the docstring above and in `test_the_patterns_can_fire`.
#: Same exemption `leakscan.SKIP_FILES` takes, for the same reason and in the same
#: shape: exempt BY NAME, and REPORT it, because an exemption nobody can see is a
#: silent hole. `test_the_self_exemption_is_exactly_one_file` is what stops it
#: widening into a general allowlist — which the repo's own
#: `test_runtime_shebangs` header reserves for sites that solve the problem a
#: verified way, never for going green.
SELF = Path(__file__).resolve()


def _findings() -> tuple[list[str], list[str]]:
    """`(findings, exempted)` over every tracked file.

    Returns the exemptions alongside the findings so a caller cannot read the
    empty list without also seeing what was not read.
    """
    out: list[str] = []
    exempted: list[str] = []
    scanned, _skipped = partition_tracked_files()
    for path in scanned:
        if path.resolve() == SELF:
            exempted.append(str(path))
            continue
        try:
            text = path.read_text(encoding="utf-8")
        except (UnicodeDecodeError, OSError):
            continue  # binary or unreadable; leakscan's own bucketing owns that
        for i, line in enumerate(text.splitlines(), 1):
            if (
                _IN_CODE_SPAN.search(line)
                or _AS_PATH.search(line)
                or _STRANDED_DETERMINER.search(line)
                or _DOUBLED_DETERMINER.search(line)
            ):
                out.append(f"{path}:{i}: {line.strip()}")
    return out, exempted


def test_the_self_exemption_is_exactly_one_file():
    """🔴 THE EXEMPTION IS THE HOLE, SO IT IS PINNED.

    This gate cannot read itself — it quotes every broken shape as a fixture. That
    is legitimate and is what `leakscan` does, but an exemption that can grow is
    how a gate goes quietly blind: add a file here to make it green and the class
    is re-opened with the suite still passing. So the count AND the name are
    asserted, and widening this requires editing an assertion that says why.

    🔴 THE FIRST VERSION OF THIS TEST DID NOT DO WHAT ITS DOCSTRING SAID, AND
    round 0 MEASURED IT. It only inspected the files this module skips, so the
    realistic widening path was invisible: add a name to `leakscan.SKIP_FILES`
    and that file never enters `scanned` at all, so it is never appended to
    `exempted` and this assertion still sees exactly one name. Demonstrated by
    planting `lib/planted_violation.py`, watching it go RED, then adding it
    upstream — **22 passed**, violation and all. `test_leakscan_covers_every_tracked_file`
    cannot catch it either; it explicitly excludes `SKIP_FILES`.

    So the UPSTREAM set is pinned too. That is the seam: this gate's coverage is
    `tracked − leakscan.SKIP_FILES − SELF`, and both subtrahends have to be
    asserted or the guard is only as honest as a file it does not control.
    """
    _found, exempted = _findings()
    assert [Path(e).resolve() for e in exempted] == [SELF], (
        f"the self-exemption is no longer exactly this file: {exempted}. Exempting "
        f"anything else re-opens the class while the suite stays green — fix the "
        f"site instead, or state here why that file cannot be fixed."
    )
    assert leakscan.SKIP_FILES == {"tests/leakscan.py"}, (
        f"`leakscan.SKIP_FILES` is {sorted(leakscan.SKIP_FILES)}, not just its own "
        f"fixtures file. Anything listed there is invisible to THIS gate as well — "
        f"it never reaches `scanned`, so the exemption check above cannot see it. "
        f"Adding a name there silently narrows this guard's corpus; if that is "
        f"intended, widen this assertion deliberately and say why."
    )


def test_the_scan_actually_reads_files():
    """POSITIVE CONTROL. Every assertion below is "the finding list is empty",
    and an empty list is what a scanner wired to nothing also returns. This
    refuses that zero first: the enumeration must be non-empty and must include
    the two modules the defect lived in.
    """
    scanned, _ = partition_tracked_files()
    assert scanned, "partition_tracked_files() enumerated NOTHING — every assertion below would pass vacuously"
    names = {p.name for p in scanned}
    for required in ("subsystem_recall.py", "cairn"):
        assert required in names, (
            f"{required!r} is not in the scanned set ({len(scanned)} files) — the "
            f"file this regression is about is not being read, so a green result "
            f"here says nothing about it."
        )


def test_the_patterns_can_fire():
    """POSITIVE CONTROL on the PATTERNS, not the corpus.

    `test_the_scan_actually_reads_files` proves files are read; it does not prove
    the regexes can match anything. A pattern that silently compiled to something
    unmatchable would make the corpus scan green forever. These are the exact
    shapes removed from the tree, so they double as the record of what was there.
    """
    assert _IN_CODE_SPAN.search("check a file with `a writer --validate <path>`.")
    assert _AS_PATH.search("`/handoff` (`lib/a writer`) — and no general reader.")
    assert _STRANDED_DETERMINER.search("which is a **the writer half** flag — the reader")
    # …and the mirror image: correct PROSE must NOT match, or the guard would
    # demand that good English be rewritten.
    for ok in (
        "a reader and a writer that disagree here send two operators to two",
        "Re-exported here so a writer does not have to re-spell them.",
        "That is right for a writer: it is answering \"what did I touch?\",",
    ):
        assert not _IN_CODE_SPAN.search(ok), ok
        assert not _AS_PATH.search(ok), ok
        assert not _STRANDED_DETERMINER.search(ok), ok


def test_no_tracked_file_puts_the_scrub_phrase_where_an_identifier_belongs():
    """REGRESSION. RED at `c536c52` on four sites across two files."""
    found, _exempt = _findings()
    assert not found, (
        "an extraction-scrub phrase (%s) appears inside a code span, a path, or a "
        "sentence built around the old name — none of which is prose, and each of "
        "which names something an operator cannot run:\n  %s\n"
        "🔴 The line below may contain ANY of those phrases, not just the first — "
        "an earlier message named only %r and then listed a site matching a "
        "DIFFERENT pattern, sending the reader grepping for a string that was not "
        "there.\n"
        "Fix the SITE, not this test: name something this package actually ships "
        "(`cairn validate --scope <scope>` runs here and names each unparseable "
        "file). Do NOT replace it with a claim about what the package does NOT "
        "ship unless you have checked — the first fix did, and was wrong."
        % (", ".join(repr(p) for p in SCRUB_PHRASES), "\n  ".join(found), SCRUB_PHRASES[0])
    )


def test_the_malformed_remedy_names_a_verb_THIS_PACKAGE_REGISTERS():
    """🔴 THE HALF A SPELLING GUARD CANNOT COVER.

    The test above pins the SHAPE. It would stay green against a remedy that is
    correctly spelled and still names a command this package does not have —
    which is the defect that shipped: `--validate <path>` was a real flag, of a
    program that is not here. So this pins the RELATIONSHIP instead: every
    ``cairn <verb>`` an operator-facing string tells someone to run must be a
    verb the CLI actually registers.
    """
    root = Path(__file__).resolve().parent.parent
    cli = (root / "cairn").read_text(encoding="utf-8")
    registered = set(re.findall(r'sub\.add_parser\(\s*"([a-z-]+)"', cli))
    assert registered, "no subcommands parsed out of the CLI — this check is vacuous"

    # 🔴 EVERY TRACKED FILE, NOT TWO. The first version read `cairn` and
    # `lib/subsystem_recall.py` only, while its docstring claimed "every
    # `cairn <verb>` an operator-facing string tells someone to run". Round 0
    # measured the gap: 57 citations across 14 files, including README.md and
    # AGENTS.md — the MOST operator-facing of all — and renaming a verb in
    # `lib/cairn_doctor.py` left the file green. A description claiming a
    # relationship over an implementation that inspects one slice is the same
    # defect this module exists to catch, one level up.
    cited: dict[str, list[str]] = {}
    scanned, _skipped = partition_tracked_files()
    for path in scanned:
        try:
            text = path.read_text(encoding="utf-8")
        except (UnicodeDecodeError, OSError):
            continue
        for verb in re.findall(r"`cairn ([a-z-]+)", text):
            cited.setdefault(verb, []).append(str(path))
    assert cited, "no `cairn <verb>` citations found anywhere — this check is vacuous"

    unknown = {v: sorted(set(f))[:3] for v, f in cited.items() if v not in registered}
    assert not unknown, (
        f"text tells someone to run a `cairn <verb>` the CLI does not register: "
        f"{unknown}. Registered: {sorted(registered)}. A remedy naming a verb that "
        f"does not exist sends the reader hunting for a flag instead of telling "
        f"them where the check lives."
    )
