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
<anything>" promises a capability that does not exist here. The corrected strings
name `cairn validate --scope <scope>` and say plainly that a single-FILE check
belongs to the writer half, which this package does not ship. A remedy that names
a capability the package lacks is worse than no remedy: it sends the reader
looking for a flag instead of telling them where the check actually lives.

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

from leakscan import partition_tracked_files  # noqa: E402

#: The phrase the extraction substituted for the writer module's real name.
SCRUB = r"a writer"

#: Two broken shapes, and ONLY these two:
#:   1. the phrase inside a backticked code span -- `... a writer ...`
#:   2. the phrase used as a path segment -- lib/a writer, scripts/a writer
#: Prose is untouched by both, which is the point: `a writer` in a sentence is
#: correct English and this repo uses it correctly in many places.
_IN_CODE_SPAN = re.compile(r"`[^`\n]*\ba writer\b[^`\n]*`")
_AS_PATH = re.compile(r"[\w./-]+/a writer\b")

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
    """
    _found, exempted = _findings()
    assert exempted == [str(SELF.relative_to(SELF.parent.parent))] or [
        Path(e).resolve() for e in exempted
    ] == [SELF], (
        f"the self-exemption is no longer exactly this file: {exempted}. Exempting "
        f"anything else re-opens the class while the suite stays green — fix the "
        f"site instead, or state here why that file cannot be fixed."
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
        "the extraction scrub phrase %r appears inside a code span, a path, or a "
        "sentence built around the old name — none of which is prose, and each of "
        "which names something an operator cannot run:\n  %s\n"
        "Fix the SITE, not this test: name a command this package actually ships "
        "(`cairn validate --scope <scope>`), or say plainly that the capability "
        "belongs to the writer half and is not shipped here."
        % (SCRUB, "\n  ".join(found))
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

    recall = (root / "lib" / "subsystem_recall.py").read_text(encoding="utf-8")
    cited = set(re.findall(r"`cairn ([a-z-]+)", recall)) | set(
        re.findall(r"`cairn ([a-z-]+)", cli)
    )
    assert cited, "no `cairn <verb>` citations found — this check is vacuous"

    unknown = sorted(cited - registered)
    assert not unknown, (
        f"operator-facing text tells someone to run `cairn {unknown}`, which the "
        f"CLI does not register. Registered: {sorted(registered)}. A remedy naming "
        f"a verb that does not exist sends the reader hunting for a flag instead "
        f"of telling them where the check lives."
    )
