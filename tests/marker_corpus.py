#!/usr/bin/env python3
"""The corpus the marker-transcription differential sweep runs over, and its axes.

🔴 THIS FILE EXISTS BECAUSE A CLOSING CONDITION POINTED AT AN ARTIFACT THAT DID NOT
EXIST. `internal/store/marker.go` declared one residual narrowing against the oracle
and closed the paragraph with *"the sweep below reports 0 divergences across all three
populations"* — and there was no sweep below, in that file or anywhere else. The
13,440 / 3,620 / 2,160 / 630 / 830 figures it quoted came from a one-off script in
somebody's scratchpad, so half the condition could not be evaluated by any reader. A
measurement nobody can re-run is an opinion, and this repository has the same note on
`tests/fixtures/near_miss_shapes.json` for the same reason.

## What is compared

Two hand-rolled Go transcriptions against the two CPython patterns they transcribe:

    internal/store/marker.go   LineMentionsMarker   ←→  `_MARKER_ANYWHERE.search`
    internal/store/openness.go nearMissMarker       ←→  `_NEAR_MISS_MARKER.match`

Both are hand-rolled because RE2 has no lookaround, so nothing about them is checked
by a compiler; the ONLY thing standing between a transcription and a silent narrowing
is a differential run over shapes chosen to hit the places the two grammars differ.

## Why the fixture holds AXES and a CHECKSUM rather than the corpus

The corpus is a cross product, and committing ~9,000 rendered lines twice over (once
as text, once as verdicts) would be a third of a megabyte of generated data in a repo
whose fixtures are hand-read. So `internal/store/testdata/marker_oracle_sweep.json` holds
the
axis lists, the assembly order, the oracle's verdicts as two bit-strings, and the
SHA-256 of the assembled corpus.

🔴 THE CHECKSUM IS THE SEAM GUARD, NOT A FORMALITY. Two assemblers in two languages
walking the same axes is exactly the shape that drifts silently — each side green
against its own idea of the corpus, agreeing about nothing. Both sides assemble, both
sides hash, and a side that assembles differently fails on the hash rather than
reporting a comfortable zero over a corpus the other side never saw.

🔴 AND THE FIXTURE IS NOT A GOLDEN OF THE GO BEHAVIOUR. It records the ORACLE's
verdicts only, and `tests/test_marker_oracle_sweep.py` re-derives them from the LIVE
`lib/subsystem_resolver` patterns on every run — so a change to either regex fails the
Python side immediately instead of leaving the Go side measured against a snapshot of
a pattern that no longer exists.

## Assembly order — LOAD-BEARING, and mirrored in `internal/store/markersweep_test.go`

    for prefix in prefixes:
        for date in dates:
            for word in words:
                for ref in refs:
                    for term in terminators:
                        emit(prefix + date + word + ref + term + tail)

then the literal `extra` lines, in order. Nothing else. Change the nesting and the
checksum moves, which is the point.
"""
from __future__ import annotations

import hashlib
import json
from pathlib import Path

#: 🔴 THE FIXTURE LIVES UNDER THE GO PACKAGE, NOT UNDER `tests/fixtures/`, AND THAT IS
#: NOT A STYLE CHOICE. `nix build` runs the Go tests from an ALLOWLISTED source copy
#: (`onlyGo` in `flake.nix`) that carries `cmd/`, `internal/` and almost nothing else.
#: A fixture in `tests/fixtures/` is simply absent there, so the sweep failed in the
#: sandbox tier while passing on the dev host — measured, and exactly the two-tier blind
#: spot `flake.nix`'s own comment about `internal/report/testdata/reader_fixtures.json`
#: warns about. That file is the precedent this follows: a Python generator writing into
#: the Go package's `testdata/`, with the path named explicitly in the filter.
FIXTURE = (Path(__file__).resolve().parents[1]
           / "internal" / "store" / "testdata" / "marker_oracle_sweep.json")

#: Bullet prefixes. `_NEAR_MISS_MARKER` is `re.match`-ed and REQUIRES `^[-*][ \t]+`,
#: so the unprefixed and indented rows are its false cases by construction, while
#: `_MARKER_ANYWHERE` is SEARCHED and ignores the prefix entirely. Both directions of
#: that asymmetry need rows or the sweep only measures one pattern.
PREFIXES = ("- ", "* ", "", "  - ")

#: Date prefixes. `_NEAR_MISS_MARKER`'s optional group is
#: `(?:\d{4}-\d{2}-\d{2}[^A-Za-z]{0,3})?`, and `\d` there is the Unicode `Nd`
#: category — the pattern is not compiled `re.ASCII`. The Arabic-Indic rows are what
#: make the Go `looksISODate` transcription's digit class observable; without them the
#: sweep cannot see an ASCII-only reading of it.
DATES = ("", "2000-01-04: ", "2000-01-04 ", "٢٠٠٠-٠١-٠٤: ",
         "٢٠٠٠-٠١-٠٤ ")

#: Marker-word spellings. `re.IGNORECASE` on a `str` pattern folds U+017F (LATIN SMALL
#: LETTER LONG S) onto `s`, which ASCII folding does not — so `reſolved` is the
#: population that isolates `hasFoldedPrefix`'s declared residual. `open` carries no
#: `s`, so it has no long-s row and that is not an omission.
WORDS = (
    "open", "OPEN", "Open", "OpEn", "oPEN",
    "resolved", "RESOLVED", "Resolved", "ReSoLvEd", "reſolved",
)

#: Ref-run atoms: `[0-9a-fA-F]{7,40}(?![0-9a-fA-F])`, `PR#\d+`, `#\d+`,
#: `\([^)]{1,30}\)`, `\[[^\]]{1,30}\]`. The lower-case `pr#` row is the population the
#: `foldPR` flag exists for (`_MARKER_ANYWHERE` folds it, `_NEAR_MISS_MARKER` does
#: not); the Arabic-Indic rows are `\d`-as-`Nd` again, one atom further in.
#:
#: ⚠ FIVE non-ASCII digits, never two. `[^A-Za-z0-9\n]{0,4}` absorbs up to four
#: non-alphanumerics, so a two-digit run lands exactly ON that boundary and a mutant
#: that drops the Unicode digit class survives. Five cannot be absorbed.
REFS = ("", " abc1234", " abcdef", " PR#12", " pr#12", " #12",
        " #١٢٣٤٥", " (repo)", " [x]")

#: Terminator runs: the oracle's tail is `[^A-Za-z0-9\n]{0,4}:`, and the SHOUTED branch
#: of `_NEAR_MISS_MARKER` may skip the colon entirely. The empty and `.` rows are what
#: separate the two branches; `!!!!!:` is five non-alphanumerics, one past the run's
#: bound, and must NOT match.
TERMINATORS = (":", " :", "**:", "!!!!!:", "", ".")

#: Appended to every product row so no line ends on its terminator.
TAIL = " a trailing clause."

#: Prose shapes that must stay false, carried literally because a cross product cannot
#: generate them. Each is a shape one side or the other got wrong at some point.
EXTRA = (
    "- OPEN SOURCE licences are listed below.",
    "- RESOLVED_ADDR appears in the trace…",
    "- RESOLVED_ADDR: the symbol named in the trace.",
    "- OPEN_: an identifier, not a declaration.",
    "- RESOLVED_: an identifier prefix, not a declaration.",
    "- resolved upstream in 1.2.3.",
    "- OPENED the lease and moved on.",
    "- REOPEN: the `OPEN` inside a longer word, which BOTH patterns match.",
    "- ordinary prose with no marker at all.",
    "  the remedy landed; RESOLVED abc1234: mid-line, still a declaration.",
    "- 2000-01-04: RESOLVED abc1234 (repo): a parenthetical before the colon.",
    "- 2000-01-04: OPEN : a space before the colon.",
    "- 2000-01-04: **OPEN:** a marker wrapped in emphasis.",
    "",
    "-\ta tab after the bullet character.",
    # 🔴 THE FOUR RUNES `re.IGNORECASE` FOLDS ONTO AN ASCII LETTER, IN THE TWO CLASS
    # POSITIONS THE CROSS PRODUCT CANNOT REACH. `re.I` widens `[A-Za-z0-9_]` to include
    # U+0130, U+0131, U+017F and U+212A, and therefore NARROWS the negated
    # `[^A-Za-z0-9\n]`. So on `_MARKER_ANYWHERE` — and only there — the word-boundary
    # lookahead rejects these and the terminator run cannot span them, while on
    # `_NEAR_MISS_MARKER`, which carries no flags, both classes stay literally ASCII.
    # Each row below was a line the Go walk matched and the oracle did not, i.e. the
    # WIDER direction, and none of them existed in any corpus before this one.
    "- OPENſ: a long s immediately after the marker word.",
    "- OPENK: the Kelvin sign immediately after the marker word.",
    "- OPENı: a dotless i immediately after the marker word.",
    "- OPENİ: a dotted capital I immediately after the marker word.",
    "- OPEN ſ: a long s inside the terminator run.",
    "- RESOLVED abc1234 K: a Kelvin sign inside the terminator run.",
    "- OPEN ſſſ: three of them, still inside the run's bound.",
)


def corpus() -> list[str]:
    """The assembled corpus, in the ONE order both clients walk."""
    lines: list[str] = []
    for prefix in PREFIXES:
        for date in DATES:
            for word in WORDS:
                for ref in REFS:
                    for term in TERMINATORS:
                        lines.append(prefix + date + word + ref + term + TAIL)
    lines.extend(EXTRA)
    return lines


def digest(lines: list[str]) -> str:
    """SHA-256 over `"\\n".join(lines)`, UTF-8 — the seam guard both sides recompute."""
    return hashlib.sha256("\n".join(lines).encode("utf-8")).hexdigest()


def axes() -> dict[str, object]:
    """The axis lists exactly as the fixture carries them."""
    return {
        "prefixes": list(PREFIXES),
        "dates": list(DATES),
        "words": list(WORDS),
        "refs": list(REFS),
        "terminators": list(TERMINATORS),
        "tail": TAIL,
        "extra": list(EXTRA),
    }


def load_fixture() -> dict:
    return json.loads(FIXTURE.read_text(encoding="utf-8"))


def oracle_bits(lines: list[str]) -> tuple[str, str]:
    """The oracle's two verdicts per line, as `0`/`1` strings.

    Imported lazily so this module stays usable as a plain data description.
    """
    import sys

    root = Path(__file__).resolve().parents[1]
    for p in (root / "lib", root / "tests"):
        if str(p) not in sys.path:
            sys.path.insert(0, str(p))
    import subsystem_resolver as sr  # noqa: E402

    anywhere = "".join(
        "1" if sr._MARKER_ANYWHERE.search(ln) else "0" for ln in lines)
    near_miss = "".join(
        "1" if sr._NEAR_MISS_MARKER.match(ln) else "0" for ln in lines)
    return anywhere, near_miss


def _regenerate() -> None:
    lines = corpus()
    anywhere, near_miss = oracle_bits(lines)
    FIXTURE.write_text(
        json.dumps(
            {
                "_comment": (
                    "GENERATED by `python3 tests/marker_corpus.py --regenerate`. "
                    "Read tests/marker_corpus.py first — the axes below are "
                    "assembled by BOTH clients and pinned by `corpus_sha256`."
                ),
                "axes": axes(),
                "corpus_lines": len(lines),
                "corpus_sha256": digest(lines),
                "oracle_marker_anywhere": anywhere,
                "oracle_near_miss_marker": near_miss,
            },
            ensure_ascii=False,
            indent=2,
        )
        + "\n",
        encoding="utf-8",
    )
    print(f"wrote {FIXTURE} — {len(lines)} lines, sha256 {digest(lines)}")


if __name__ == "__main__":
    _regenerate()
