#!/usr/bin/env python3
"""The WRITE-PROTOCOL half of `validate`: content a reader cannot reach.

🔴 WHAT THIS FILE IS COVERAGE FOR, AND WHAT IT IS NOT. The packaged `validate`
already answered "would the loader accept this file?" — that half is pinned by
`tests/test_cairn_cli.py` and is untouched here. These two scanners answer a
DIFFERENT question, the one the write protocol actually mandates: does the entry
hold text NO reader will ever surface? `dropped lines:` is the half that means
content is ALREADY LOST.

Every fixture is synthetic and dated year 2000, per `AGENTS.md`.
"""
from __future__ import annotations

import os
import socket
import subprocess
import sys
from pathlib import Path

import pytest

import entry_shape


# --------------------------------------------------------------------------
# helpers
# --------------------------------------------------------------------------

def _entry(nuance: str, *, service: str = "talus-svc", scope: str = "crag-notes") -> str:
    return (
        "---\n"
        f"service: {service}\n"
        f"scope: {scope}\n"
        "---\n"
        "\n## What it is\n\n"
        "a synthetic entry.\n"
        "\n## Pointers\n\n"
        f"- `apps/{service}/values.yaml`\n"
        f"\n## Nuance / work-history\n\n{nuance}\n"
    )


def _write(tmp_path: Path, nuance: str, name: str = "talus-svc.md") -> Path:
    path = tmp_path / name
    path.write_text(_entry(nuance), encoding="utf-8")
    return path


# --------------------------------------------------------------------------
# scan_dropped_lines
# --------------------------------------------------------------------------

class TestScanDroppedLines:
    def test_text_before_the_first_bullet_is_REPORTED_with_its_offset(self, tmp_path: Path):
        """🔴 THE DEFECT THIS EXISTS FOR, MEASURED IN THE FIELD ON A LIVE STORE.

        `parse_journal_bullets` drops every line that precedes the first bullet,
        so the text is in the file, in the store and in the backup, and in no
        consumer's output. Two entries were broken this way for days apiece.
        """
        path = _write(
            tmp_path,
            "  the bullet opening that carried this line is gone.\n"
            "- 2000-01-04: RESOLVED abc1234: the bullet that did survive.\n",
        )
        found = entry_shape.scan_dropped_lines([path])
        assert [(d.filename, d.offset, d.carries_marker) for d in found] == [
            ("talus-svc.md", 1, False)
        ]
        assert found[0].line == "  the bullet opening that carried this line is gone."

    def test_a_dropped_DECLARATION_is_flagged_and_a_plain_line_is_not(self, tmp_path: Path):
        """`carries_marker` ranks urgency: a lost `OPEN:` is an open action the
        store is actively failing to report. It is never counted as one."""
        path = _write(
            tmp_path,
            "  ordinary lost prose.\n"
            "  OPEN: a declaration nothing will ever surface.\n"
            "- 2000-01-04: a bullet.\n",
        )
        found = entry_shape.scan_dropped_lines([path])
        assert [d.carries_marker for d in found] == [False, True]

    def test_lines_INSIDE_a_bullet_are_not_reported(self, tmp_path: Path):
        path = _write(
            tmp_path,
            "- 2000-01-04: a bullet.\n"
            "  its wrapped continuation, which every reader surfaces.\n",
        )
        assert entry_shape.scan_dropped_lines([path]) == ()

    def test_an_orphan_whose_TEXT_repeats_inside_a_bullet_is_still_reported(
        self, tmp_path: Path
    ):
        """🔴 REACHABILITY IS KEYED ON (OFFSET, LINE), NOT ON THE LINE ALONE.

        A set of strings masks an orphan whose text is byte-identical to a line
        inside a bullet of the same file — and a read-modify-write race, the very
        shape that decapitates a bullet, is exactly what duplicates a block.

        This is a MUTATION guard: drop the offset from the key and the duplicate
        below is silently blessed.
        """
        repeated = "  the same wrapped sentence, twice."
        path = _write(
            tmp_path,
            f"{repeated}\n- 2000-01-04: a bullet.\n{repeated}\n",
        )
        found = entry_shape.scan_dropped_lines([path])
        assert [(d.offset, d.line) for d in found] == [(1, repeated)]

    def test_a_FENCED_line_before_the_first_bullet_is_not_reported(self, tmp_path: Path):
        """A fenced snippet is sample text. Reporting it would hand the operator
        "restore the bullet opening line" for content that never had one."""
        path = _write(
            tmp_path,
            "```\n- OPEN: sample text inside a fence.\n```\n- 2000-01-04: a bullet.\n",
        )
        assert entry_shape.scan_dropped_lines([path]) == ()

    def test_a_TILDE_fence_is_a_fence_too(self, tmp_path: Path):
        """🔴 THE READER'S OWN `_is_fence` IS IMPORTED RATHER THAN RE-SPELLED, and
        this is the case a hand-spelled ``` -only copy gets wrong. The
        pre-extraction original WAS such a copy."""
        path = _write(
            tmp_path,
            "~~~\n- OPEN: sample text inside a tilde fence.\n~~~\n- 2000-01-04: a bullet.\n",
        )
        assert entry_shape.scan_dropped_lines([path]) == ()

    def test_blank_lines_are_not_dropped_content(self, tmp_path: Path):
        path = _write(tmp_path, "\n\n- 2000-01-04: a bullet.\n")
        assert entry_shape.scan_dropped_lines([path]) == ()

    def test_a_file_with_no_nuance_heading_contributes_nothing(self, tmp_path: Path):
        path = tmp_path / "shapeless.md"
        path.write_text(
            "---\nservice: shapeless\nscope: crag-notes\n---\n\n## What it is\n\nprose.\n",
            encoding="utf-8",
        )
        assert entry_shape.scan_dropped_lines([path]) == ()
        assert entry_shape.scan_unreachable_markers([path]) == ()

    def test_an_unreadable_file_contributes_nothing_rather_than_raising(
        self, tmp_path: Path
    ):
        """Both scanners run BESIDE the parse check, never in front of it: a
        malformed or unreadable file's own rejection is the finding that matters,
        and an advisory that raised here would replace it with a traceback.

        ⚠ INVARIANT GUARDS, NOT REGRESSION COVERAGE, AND THAT IS THE WHOLE POINT
        OF SPLITTING THEM OUT. Every kind below fails the read by RAISING an
        `OSError`, which `_nuance_body` caught before the classifier gate existed
        and catches now — so all five pass on pre-change code. The docstring they
        used to sit under claimed to prevent a traceback and this was the only
        evidence for it; the kinds that actually produced one do not raise at all
        and are in `TestNonRegularPathsAreRefusedBeforeOpen` below.
        """
        cases = {
            "absent": tmp_path / "never-written.md",
            "directory": tmp_path / "a-directory.md",
            "broken-link": tmp_path / "dangling.md",
            "link-to-dir": tmp_path / "to-a-directory.md",
            "other (socket)": tmp_path / "a-socket.md",
        }
        cases["directory"].mkdir()
        cases["broken-link"].symlink_to(tmp_path / "nothing-here")
        cases["link-to-dir"].symlink_to(cases["directory"])
        sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
        try:
            sock.bind(str(cases["other (socket)"]))
            for kind, path in cases.items():
                assert entry_shape.scan_dropped_lines([path]) == (), kind
                assert entry_shape.scan_unreachable_markers([path]) == (), kind
        finally:
            sock.close()

    def test_a_SYMLINK_to_a_regular_entry_IS_still_scanned(self, tmp_path: Path):
        """🔴 THE NEGATIVE CONTROL ON THE CLASSIFIER GATE, AND IT IS THE CELL THE
        WHOLE NARROW-VS-BROAD RULING WAS ABOUT.

        `link-to-file` is `TAKE` in `_LOADER_ENTRY_ACTIONS`: the loader reads a
        symlink to a regular `*.md` and always has. A gate spelled "regular files
        only" would make these scanners NARROWER than the loader — printing
        `dropped lines: 0 across N entry file(s)` over a file the denominator
        counted and the scanner never opened, which is the reassuring zero this
        whole block of prose exists to refuse. A mutant that flips the gate to
        `classify_path(path) == KIND_REGULAR_FILE` is killed here and nowhere
        else.
        """
        real = _write(tmp_path, "  the bullet opening that carried this line is gone.\n"
                                "- 2000-01-04: a bullet.\n")
        link = tmp_path / "linked-svc.md"
        link.symlink_to(real)
        found = entry_shape.scan_dropped_lines([link])
        assert [(d.filename, d.offset) for d in found] == [("linked-svc.md", 1)]

    def test_the_ABSORBED_TAIL_case_is_KNOWN_INVISIBLE(self, tmp_path: Path):
        """🔴 AN INVARIANT GUARD ON A DECLARED BLIND SPOT — NOT regression coverage.

        A bullet that loses its opening line while ANOTHER bullet sits above it
        is absorbed into that one, and the file is byte-identical to a legitimate
        wrap. No check can separate the two. This pins that the scanner does not
        pretend otherwise, so the zero it prints keeps carrying its PARTIAL
        caveat rather than being read as "no bullet has lost its head".
        """
        path = _write(
            tmp_path,
            "- 2000-01-04: a bullet that is genuinely here.\n"
            "  2000-01-05: the NEXT bullet, whose `- ` is gone.\n",
        )
        assert entry_shape.scan_dropped_lines([path]) == ()

    def test_an_UNCLOSED_FENCE_is_KNOWN_INVISIBLE(self, tmp_path: Path):
        """🔴 AN INVARIANT GUARD ON BLIND SPOT (2) — NOT regression coverage, and
        the one where the zero is actively misleading rather than partial.

        `_is_fence` toggles, so an odd count leaves every following line fenced,
        and fenced lines are skipped as sample text by design. A bullet swallowed
        that way produces no bullet AND no dropped-line finding, so the `OPEN:`
        below is surfaced by nothing in either scanner. Pinned so that the
        printed caveat keeps naming it — the previous caveat named only the
        absorbed tail, and this shape reads exactly like a clean entry.
        """
        path = _write(
            tmp_path,
            "```\n- 2000-01-04: OPEN: a whole bullet swallowed by one stray fence.\n",
        )
        assert entry_shape.scan_dropped_lines([path]) == ()
        assert entry_shape.scan_unreachable_markers([path]) == ()

    def test_a_DUPLICATED_nuance_heading_is_KNOWN_INVISIBLE(self, tmp_path: Path):
        """🔴 AN INVARIANT GUARD ON BLIND SPOT (3) — NOT regression coverage.

        `extract_sections` concatenates same-named sections, so the orphan under
        the SECOND heading arrives immediately after the FIRST section's bullet
        and is absorbed into it. The orphan contributes NO finding; the offsets
        this scanner reports are into the concatenated body, which is not the
        same coordinate system as the file once a heading repeats.

        ⚠ THE FILE LINE IS ASSERTED BELOW, NOT STATED HERE. An earlier draft of
        this docstring read "the orphan sits on file line 16"; it sits on 21, and
        the wrong number was copied onward into a PR comment before an audit
        re-derived it from `_entry`. A measurement written into prose is a claim
        nothing re-runs, so the number now lives in the positive control, where a
        fixture edit that moves the line fails this test rather than silently
        making a sentence false.
        """
        path = tmp_path / "twice-headed.md"
        path.write_text(
            _entry("- 2000-01-04: a bullet under the FIRST heading.\n")
            + "\n## Nuance / work-history\n\n"
            "  an orphan under the SECOND heading.\n",
            encoding="utf-8",
        )
        assert entry_shape.scan_dropped_lines([path]) == ()
        # The positive control on the fixture: the orphan really is in the file,
        # so the zero above is about absorption and not about a fixture that
        # never wrote the line. The LINE NUMBER is asserted, not described —
        # `_entry` is shared with every other case here, so a header line added
        # to it moves this orphan and the docstring above would otherwise go
        # quietly wrong (it already did once, by five lines).
        lines = path.read_text(encoding="utf-8").splitlines()
        orphans = [n for n, ln in enumerate(lines, 1) if "SECOND heading" in ln]
        assert orphans == [21], (
            f"the orphan is on file line(s) {orphans}, not 21 — `_entry`'s header "
            f"changed shape, so re-derive rather than editing this number"
        )


class TestNonRegularPathsAreRefusedBeforeOpen:
    """🔴 THE TWO KINDS THAT DO NOT RAISE, WHICH IS WHY THEY GOT PAST AN
    `except OSError` AND PAST THE TEST THAT CLAIMED TO COVER THEM.

    Reading a FIFO BLOCKS until somebody writes to it — no error, no return, and
    `validate` never finishes. `load_index` has refused `other` and
    `link-to-other` before `open()` since a fifo was measured wedging a request
    thread for 25s; the advisories added beside it opened every path
    unconditionally, so `cairn validate` over a cache holding one hung on BOTH
    clients where the commit before it exited 5.

    ⚠ IT RUNS IN A SUBPROCESS BECAUSE THE PRE-CHANGE FAILURE IS A HANG. An
    in-process call cannot be watched to fail — it never comes back, and a test
    that wedges the whole suite is not a red, it is a dead runner. The timeout is
    what makes the symptom observable and bounded.
    """

    #: Long enough that a loaded box does not flake it, short enough that a real
    #: wedge is not mistaken for slowness. The pre-change failure is INFINITE, so
    #: no value of this can be too small in the direction that matters.
    TIMEOUT_S = 20

    def _scan_in_a_subprocess(self, path: Path) -> subprocess.CompletedProcess:
        root = Path(__file__).resolve().parent.parent
        env = dict(os.environ)
        env["PYTHONPATH"] = os.pathsep.join(
            [str(root / "lib"), env.get("PYTHONPATH", "")]
        ).rstrip(os.pathsep)
        return subprocess.run(
            [sys.executable, "-c",
             "import sys, entry_shape\n"
             "p = sys.argv[1]\n"
             "assert entry_shape.scan_dropped_lines([p]) == ()\n"
             "assert entry_shape.scan_unreachable_markers([p]) == ()\n",
             str(path)],
            env=env, timeout=self.TIMEOUT_S, capture_output=True, text=True,
        )

    def test_a_FIFO_named_like_an_entry_does_not_wedge_the_scanners(
        self, tmp_path: Path
    ):
        """`other`. RED at `d6a4b91`: the child never returns and this raises
        `subprocess.TimeoutExpired`."""
        fifo = tmp_path / "wedge.md"
        os.mkfifo(fifo)
        done = self._scan_in_a_subprocess(fifo)
        assert done.returncode == 0, done.stderr

    def test_a_SYMLINK_to_a_fifo_does_not_wedge_them_either(self, tmp_path: Path):
        """`link-to-other`. `open()` does not care which path shape reached the
        fifo, which is exactly why the loader's table refuses both kinds and why
        one case cannot stand for the other: they are two cells."""
        fifo = tmp_path / "real-fifo"
        os.mkfifo(fifo)
        link = tmp_path / "wedge-through-a-link.md"
        link.symlink_to(fifo)
        done = self._scan_in_a_subprocess(link)
        assert done.returncode == 0, done.stderr

    def test_the_DENOMINATOR_counts_what_was_SCANNED_not_what_was_LISTED(
        self, tmp_path: Path
    ):
        """🔴 THE ZERO OVER A FILE NOBODY OPENED. RED before `scanned_entry_count`
        existed, because the clients passed the unfiltered listing.

        The two scanners refuse a FIFO before `open()` — the tests above are the
        whole reason they do — but the count printed beside their zero came from
        `entry_files_in`, which lists it. So a scope holding one real entry and
        one FIFO printed `dropped lines: 0 across 2 entry file(s)`, asserting
        that every line of a file nothing had read reaches a bullet. That is the
        reassuring zero the `NOT CHECKED` branch exists to prevent, arriving
        through the numerator instead of through an empty directory.

        ⚠ THE FIFO IS NOT LOST FROM THE OUTPUT and this is not a softening: the
        parse line above the advisories still counts it and reports it malformed,
        and `validate` still exits 5. Only the advisory denominator changes.
        """
        real = _write(tmp_path, "- 2000-01-04: a bullet.\n")
        fifo = tmp_path / "wedge.md"
        os.mkfifo(fifo)
        paths = [real, fifo]

        assert entry_shape.scanned_entry_count(paths) == 1
        assert len(paths) == 2, "the fixture must present BOTH, or this proves nothing"

        blob = "\n".join(entry_shape.validation_advisory_lines(
            n_scanned=entry_shape.scanned_entry_count(paths),
            dropped=entry_shape.scan_dropped_lines(paths),
            unreachable=entry_shape.scan_unreachable_markers(paths),
        ))
        assert "across 1 entry file(s) scanned" in blob, blob
        assert "across 2 entry file(s)" not in blob, blob

    def test_a_scope_of_NOTHING_BUT_unreadable_kinds_says_NOT_CHECKED(
        self, tmp_path: Path
    ):
        """The other end of the same change, and the one that makes it a fix
        rather than a re-labelling: when the scanners open NOTHING, the honest
        output is `NOT CHECKED`, not `0 across 1`.
        """
        fifo = tmp_path / "wedge.md"
        os.mkfifo(fifo)
        assert entry_shape.scanned_entry_count([fifo]) == 0
        lines = entry_shape.validation_advisory_lines(
            n_scanned=entry_shape.scanned_entry_count([fifo]),
            dropped=(), unreachable=(),
        )
        assert len(lines) == 1 and "NOT CHECKED" in lines[0], lines


# --------------------------------------------------------------------------
# scan_unreachable_markers
# --------------------------------------------------------------------------

class TestScanUnreachableMarkers:
    def test_a_marker_on_a_CONTINUATION_line_is_reported(self, tmp_path: Path):
        """🔴 THE SHAPE THAT COST A REAL OPEN ACTION ITS BADGE. `_bullet_openness`
        is anchored at position 0 of a bullet's OPENING line, so this declares
        nothing: it raises neither the `OPEN` badge nor `NEAR-MISS`."""
        path = _write(
            tmp_path,
            "- 2000-01-04: RESOLVED abc1234: the bullet that did survive.\n"
            "  OPEN: a marker several lines in, where no parser looks.\n",
        )
        found = entry_shape.scan_unreachable_markers([path])
        assert [(u.filename, u.offset, u.openness) for u in found] == [
            ("talus-svc.md", 2, "open")
        ]
        assert found[0].bullet_first_line.startswith("- 2000-01-04: RESOLVED abc1234:")

    def test_a_marker_on_the_OPENING_line_is_NOT_reported(self, tmp_path: Path):
        """Reachable by definition — that line IS what the parser reads."""
        path = _write(tmp_path, "- 2000-01-04: OPEN: a reachable declaration.\n")
        assert entry_shape.scan_unreachable_markers([path]) == ()

    def test_it_is_NOT_suppressed_when_the_bullet_already_declares_one(
        self, tmp_path: Path
    ):
        """The field case was exactly a bullet carrying two markers. A bullet with
        a good head marker AND a second one further down is two claims stored as
        one, which is worth saying either way."""
        path = _write(
            tmp_path,
            "- 2000-01-04: OPEN: the head marker, which parses.\n"
            "  RESOLVED abc1234: a second claim, in the body.\n",
        )
        found = entry_shape.scan_unreachable_markers([path])
        assert [u.openness for u in found] == ["resolved"]

    def test_a_marker_inside_a_FENCE_within_a_bullet_is_not_reported(
        self, tmp_path: Path
    ):
        path = _write(
            tmp_path,
            "- 2000-01-04: a bullet quoting a snippet.\n"
            "  ```\n"
            "  OPEN: sample text.\n"
            "  ```\n",
        )
        assert entry_shape.scan_unreachable_markers([path]) == ()

    def test_an_INDENTED_bullet_marker_is_reached_through_the_real_parser(
        self, tmp_path: Path
    ):
        """🔴 THE FIELD SHAPE. A nested list item is a CONTINUATION line, and
        `_as_opening_line` strips its indentation rather than manufacturing a
        second `- ` prefix. A scanner that only handled unprefixed prose would be
        inert on exactly the shape that was observed."""
        path = _write(
            tmp_path,
            "- 2000-01-04: a bullet.\n"
            "  - OPEN: a nested item that declares nothing at all.\n",
        )
        found = entry_shape.scan_unreachable_markers([path])
        assert [u.openness for u in found] == ["open"]


# --------------------------------------------------------------------------
# line_carries_marker
# --------------------------------------------------------------------------

class TestLineCarriesMarker:
    @pytest.mark.parametrize(
        "line",
        [
            "  OPEN: a declaration.",
            "  - 2000-01-04: OPEN: a dated, nested declaration.",
            "  RESOLVED abc1234: a closing claim.",
            "  the remedy landed; RESOLVED abc1234: mid-line, still a declaration.",
            # 🔴 THE ORACLE HALF OF A TWO-WAY PIN, and the oracle is the side that
            # was already right. `_MARKER_ANYWHERE` is `re.IGNORECASE` over the
            # WHOLE pattern (so `PR#\\d+` folds) and is not `re.ASCII` (so `\\d` is
            # the Unicode `Nd` category). The Go port transcribed both narrower and
            # was silently missing these; the twin cases live in
            # `internal/store/validate_test.go`'s `TestLineCarriesMarker`. Pinned
            # HERE so a future "simplification" of the oracle pattern — adding
            # `re.ASCII`, or scoping the `(?i:…)` the way `_NEAR_MISS_MARKER` does
            # — is a red rather than a silent re-narrowing of both clients.
            "  OPEN pr#12: a lowercase PR reference.",
            "  OPEN #١٢٣٤٥: a non-ASCII decimal digit reference.",
        ],
    )
    def test_true_on_a_declaration(self, line: str):
        assert entry_shape.line_carries_marker(line) is True

    @pytest.mark.parametrize(
        "line",
        [
            # 🔴 EACH OF THESE IS A SHAPE THE HAND-SPELLED PRE-EXTRACTION ORIGINAL GOT
            # WRONG, measured against the live store: it required no colon and no
            # word boundary, so prose fired and the field shapes did not.
            "  resolved upstream in 1.2.3.",
            "  RESOLVED_ADDR appears in the trace…",
            # ⚠ AND SO IS THIS ONE, WHICH THE COMMENT BELOW USED TO CLAIM AS A
            # BOUNDARY WITNESS. Re-derived by mutation: delete the word-boundary
            # guard and this line is STILL rejected, by the terminator rule —
            # `[^A-Za-z0-9\n]{0,4}:` cannot cross `ADDR`, and `_ADDR` is no ref
            # atom either. It is a second sample of the line above it, one colon
            # richer. Kept because it is a real prose shape, relabelled because a
            # comment asserting coverage it does not provide is worse than none:
            # it stops the next reader looking for a case that does.
            "  RESOLVED_ADDR: the symbol named in the trace.",
            # 🔴 THE WORD-BOUNDARY GUARD'S OWN DISCRIMINATING INPUT — ONE PER
            # TOKEN, because the guard is spelled once per token and a mutant can
            # remove either. The colon sits within the terminator's reach of the
            # token itself (`_` is not alphanumeric, so `[^A-Za-z0-9\n]{0,4}:`
            # spans it), which means nothing but the boundary can reject them:
            # delete it and both flip to True.
            "  OPEN_: an identifier, not a declaration.",
            "  RESOLVED_: an identifier prefix, not a declaration.",
            "  OPEN SOURCE licences are listed below.",
            "  OPENED the lease and moved on.",
            "  ordinary prose with no marker at all.",
        ],
    )
    def test_false_on_prose(self, line: str):
        assert entry_shape.line_carries_marker(line) is False


# --------------------------------------------------------------------------
# validation_advisory_lines
# --------------------------------------------------------------------------

class TestValidationAdvisoryLines:
    def test_nothing_checked_prints_NOT_CHECKED_and_neither_zero(self):
        """🔴 "0 across 0 entry file(s)" IS THE REASSURING ZERO FROM AN INSTRUMENT
        THAT WALKED NOTHING, and it must not render anywhere near a clean-looking
        count."""
        lines = entry_shape.validation_advisory_lines(
            n_scanned=0, dropped=(), unreachable=()
        )
        assert len(lines) == 1
        assert "NOT CHECKED" in lines[0]
        assert "0 across 0" not in lines[0]
        assert entry_shape.DROPPED_LINE in lines[0]
        assert entry_shape.UNREACHABLE_MARKER in lines[0]

    def test_both_zeros_carry_their_DENOMINATOR(self):
        """A bare zero is indistinguishable from a scanner wired to nothing."""
        lines = entry_shape.validation_advisory_lines(
            n_scanned=7, dropped=(), unreachable=()
        )
        blob = "\n".join(lines)
        assert "dropped lines: 0 across 7 entry file(s) scanned" in blob
        assert ("marker reachability: 0 out-of-reach marker(s) across 7 entry "
                "file(s) scanned") in blob

    def test_the_dropped_zero_states_ALL_THREE_of_its_blind_spots(self):
        """🔴 UNLIKE ITS SIBLING, THIS CHECK IS KNOWINGLY PARTIAL — AND IT IS
        PARTIAL IN THREE WAYS, WHERE THE PRINTED SENTENCE USED TO NAME ONE.

        A bare "0 dropped" would read as "no bullet has lost its head", which is
        a claim it cannot make. Naming only the absorbed tail was the same defect
        one size smaller: the sentence read as a complete enumeration and was not,
        so an operator reading it would have concluded that an unclosed fence or
        a duplicated heading WAS covered. Each name below is pinned to an
        invariant guard above (`test_the_ABSORBED_TAIL_case_is_KNOWN_INVISIBLE`,
        `test_an_UNCLOSED_FENCE_is_KNOWN_INVISIBLE`,
        `test_a_DUPLICATED_nuance_heading_is_KNOWN_INVISIBLE`), so the claim and
        the behaviour move together.
        """
        blob = "\n".join(
            entry_shape.validation_advisory_lines(n_scanned=1, dropped=(), unreachable=())
        )
        assert "PARTIAL BY CONSTRUCTION, IN THREE WAYS" in blob
        for named in ("ABSORBED TAIL", "UNCLOSED FENCE", "DUPLICATED"):
            assert named in blob, named

    def test_the_DROPPED_block_comes_BEFORE_the_reachability_block(self):
        """🔴 A dropped line is content NO reader reaches, so the marker scan never
        sees it. A `0 out-of-reach` printed ABOVE a `🔴 N DROPPED LINE(S)` is a
        fact about text the parser never got to, and reads as a reassurance it
        cannot support. That is the order the pre-extraction original shipped in
        until an audit read its rendered output for a real broken entry."""
        lines = entry_shape.validation_advisory_lines(
            n_scanned=1,
            dropped=(entry_shape.DroppedLineFinding("a.md", 1, "  lost."),),
            unreachable=(),
        )
        blob = "\n".join(lines)
        assert blob.index("DROPPED LINE(S)") < blob.index("marker reachability:")

    def test_findings_are_quoted_with_their_file_and_offset(self):
        lines = entry_shape.validation_advisory_lines(
            n_scanned=2,
            dropped=(
                entry_shape.DroppedLineFinding("talus-svc.md", 1, "  lost prose."),
                entry_shape.DroppedLineFinding(
                    "talus-svc.md", 2, "  OPEN: lost claim.", carries_marker=True
                ),
            ),
            unreachable=(
                entry_shape.UnreachableMarkerFinding(
                    "talus-svc.md", "- 2000-01-04: a bullet.", 2, "  OPEN: x.", "open"
                ),
            ),
        )
        blob = "\n".join(lines)
        assert "🔴 2 DROPPED LINE(S) across 2 entry file(s) scanned" in blob
        assert "1 of them looks like a `OPEN:`/`RESOLVED:` DECLARATION" in blob
        assert "    talus-svc.md: nuance line 1" in blob
        assert "    talus-svc.md: nuance line 2  ← looks like a DECLARATION" in blob
        assert "🔴 1 MARKER(S) OUT OF REACH across 2 entry file(s) scanned" in blob
        assert "    talus-svc.md: line 2 of the bullet opening" in blob
        assert "(would declare `open`)" in blob

    def test_a_long_line_is_cut_to_the_declared_quote_length(self):
        """The quote is there to recognise a finding by; an entry may hold a
        4,000-character bullet, and a validator that reprints one has buried its
        own verdict."""
        long_line = "x" * 500
        lines = entry_shape.validation_advisory_lines(
            n_scanned=1,
            dropped=(entry_shape.DroppedLineFinding("a.md", 1, long_line),),
            unreachable=(),
        )
        quoted = [ln for ln in lines if ln.strip().startswith("x")]
        assert quoted and len(quoted[0].strip()) == entry_shape.ADVISORY_QUOTE_MAX
