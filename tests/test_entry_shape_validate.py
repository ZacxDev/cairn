#!/usr/bin/env python3
"""The WRITE-PROTOCOL half of `validate`: content a reader cannot reach.

🔴 WHAT THIS FILE IS COVERAGE FOR, AND WHAT IT IS NOT. The packaged `validate`
already answered "would the loader accept this file?" — that half is pinned by
`tests/test_cairn_cli.py` and is untouched here. These scanners answer a
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
        """Every scanner runs BESIDE the parse check, never in front of it: a
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
        below is surfaced by nothing in any scanner. Pinned so that the
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

        The scanners refuse a FIFO before `open()` — the tests above are the
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
            shape=entry_shape.scan_entry_shape(paths),
            dropped=entry_shape.scan_dropped_lines(paths),
            open_actions=entry_shape.scan_open_actions(paths),
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
            shape=(), dropped=(), open_actions=(), unreachable=(),
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
            n_scanned=0, shape=(), dropped=(), open_actions=(), unreachable=()
        )
        assert len(lines) == 1
        assert "NOT CHECKED" in lines[0]
        assert "0 across 0" not in lines[0]
        assert entry_shape.DROPPED_LINE in lines[0]
        assert entry_shape.UNREACHABLE_MARKER in lines[0]

    def test_both_zeros_carry_their_DENOMINATOR(self):
        """A bare zero is indistinguishable from a scanner wired to nothing."""
        lines = entry_shape.validation_advisory_lines(
            n_scanned=7, shape=(), dropped=(), open_actions=(), unreachable=()
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
            entry_shape.validation_advisory_lines(
                n_scanned=1, shape=(), dropped=(), open_actions=(), unreachable=()
            )
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
            shape=(),
            dropped=(entry_shape.DroppedLineFinding("a.md", 1, "  lost."),),
            open_actions=(),
            unreachable=(),
        )
        blob = "\n".join(lines)
        assert blob.index("DROPPED LINE(S)") < blob.index("marker reachability:")

    def test_findings_are_quoted_with_their_file_and_offset(self):
        lines = entry_shape.validation_advisory_lines(
            n_scanned=2,
            shape=(),
            open_actions=(),
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
            shape=(),
            dropped=(entry_shape.DroppedLineFinding("a.md", 1, long_line),),
            open_actions=(),
            unreachable=(),
        )
        quoted = [ln for ln in lines if ln.strip().startswith("x")]
        assert quoted and len(quoted[0].strip()) == entry_shape.ADVISORY_QUOTE_MAX


# --------------------------------------------------------------------------
# scan_entry_shape — the entry's SPINE, as opposed to whether it parses
# --------------------------------------------------------------------------

def _shaped(tmp_path: Path, name: str, body: str) -> Path:
    """An entry file whose SECTIONS are given verbatim, front matter prepended.

    Deliberately not `_entry`, which hard-codes a correct spine — the whole point
    of these fixtures is that the spine is wrong.
    """
    path = tmp_path / name
    path.write_text(
        f"---\nservice: {name[:-3]}\nscope: crag-notes\n---\n{body}", encoding="utf-8"
    )
    return path


def _kinds(findings, kind: str):
    return [f for f in findings if f.kind == kind]


class TestScanEntryShape:
    def test_a_CLEAN_entry_yields_NOTHING(self):
        """The positive control for every test below: if this returned findings,
        every other assertion here would be about noise."""
        import tempfile
        with tempfile.TemporaryDirectory() as d:
            path = _write(Path(d), "- 2000-01-04: a bullet.", "clean-svc.md")
            assert entry_shape.scan_entry_shape([path]) == ()

    def test_a_CASE_near_miss_is_RENAMED_and_QUOTES_WHAT_THE_WRITER_TYPED(
        self, tmp_path: Path
    ):
        """🔴 RENAMED AND ABSENT ARE THE SAME MISSING SECTION AT TWO RESOLUTIONS,
        and the renamed one is the only half a writer can act on in a single edit.
        Collapsing them into "absent" sends someone looking for prose that is
        already on disk.

        The heading is checked as an EXACT string, so `## pointers` reaches no
        reader — the fold exists only to PAIR the two for the report.
        """
        path = _shaped(
            tmp_path, "moraine-cfg.md",
            "\n## pointers\n\n- `apps/moraine-cfg/values.yaml`\n"
            "\n## Nuance / work-history\n\n- 2000-01-04: a bullet.\n",
        )
        renamed = _kinds(entry_shape.scan_entry_shape([path]), entry_shape.SHAPE_RENAMED)
        assert len(renamed) == 1, renamed
        assert renamed[0].heading == entry_shape.POINTERS_HEADING
        assert renamed[0].found == ("## pointers",)
        # 🔴 AND IT IS NOT ALSO REPORTED ABSENT. Two findings for one heading would
        # double the count a reader acts on.
        assert _kinds(entry_shape.scan_entry_shape([path]), entry_shape.SHAPE_ABSENT) == []

    def test_a_LEVEL_a_COLON_and_a_WHITESPACE_RUN_all_pair_as_RENAMED(
        self, tmp_path: Path
    ):
        """The three near-misses the fold is declared to cover, each in its own
        file so a single assertion cannot pass on the wrong one.

        🔴 THE WHITESPACE CASE IS AN *INTERNAL* RUN, NOT A LEADING ONE, AND THE
        FIRST DRAFT OF THIS TEST GOT IT WRONG. `##   Pointers` is folded by the
        `.strip()` that already sits in the chain, so the whitespace-run collapse
        never executes — a mutant that DELETED the collapse survived a green run
        of this very test. Only a run BETWEEN two words reaches it. That is the
        difference between a mutant that is breakable and a guard that is
        REACHABLE.
        """
        cases = [
            ("level-svc.md", "### Pointers", entry_shape.POINTERS_HEADING),
            ("colon-svc.md", "## Pointers:", entry_shape.POINTERS_HEADING),
            ("leading-svc.md", "##   Pointers", entry_shape.POINTERS_HEADING),
            ("internal-svc.md", "## Nuance /  work-history",
             entry_shape.NUANCE_HEADING),
        ]
        for name, written, schema in cases:
            # The heading NOT under test is spelled correctly, so exactly one
            # finding can exist and a passing assertion cannot be about the other.
            other = (
                entry_shape.NUANCE_HEADING
                if schema == entry_shape.POINTERS_HEADING
                else entry_shape.POINTERS_HEADING
            )
            path = _shaped(
                tmp_path, name,
                f"\n{written}\n\nbent section body.\n"
                f"\n{other}\n\n- 2000-01-04: a bullet.\n",
            )
            findings = entry_shape.scan_entry_shape([path])
            renamed = _kinds(findings, entry_shape.SHAPE_RENAMED)
            assert len(renamed) == 1, (name, findings)
            assert renamed[0].heading == schema, (name, renamed[0].heading)
            assert renamed[0].found == (written,), (name, renamed[0].found)

    def test_the_FOLD_NEVER_WIDENS_WHAT_extract_sections_ACCEPTS(self, tmp_path: Path):
        """🔴 THE ONE PROPERTY THAT MAKES THE FOLD SAFE, AND IT IS A CLAIM ABOUT
        THE READER, NOT ABOUT THIS SCANNER.

        `_heading_key` folds `## pointers` onto `## Pointers` so the report can
        name the typo. If that fold ever reached `extract_sections`, the store
        would quietly start accepting a wider set of headings than every reader
        parses — the exact opposite of what a write-time check is for. So the
        assertion is on the READER: the folded spelling must still resolve to NO
        section.
        """
        text = (
            "\n## pointers\n\n- `apps/x/values.yaml`\n"
            "\n## Nuance / work-history\n\n- 2000-01-04: a bullet.\n"
        )
        assert entry_shape._heading_key("## pointers") == entry_shape._heading_key(
            entry_shape.POINTERS_HEADING
        ), "the fold must pair them, or the RENAMED report is impossible"
        sections = entry_shape.extract_sections(
            text, (entry_shape.POINTERS_HEADING,)
        )
        assert entry_shape.POINTERS_HEADING not in sections, sections

    def test_a_heading_NOTHING_PAIRS_WITH_is_ABSENT_and_carries_the_INVENTORY(
        self, tmp_path: Path
    ):
        """🔴 THE INVENTORY IS WHAT MAKES AN ABSENT FINDING ACTIONABLE. "no
        `## Pointers`" alone leaves the writer re-reading a file they just wrote;
        the list of what IS there is how they see that they called it something
        else entirely."""
        path = _shaped(
            tmp_path, "cirque-api.md",
            "\n## Links and references\n\n- `apps/cirque-api/values.yaml`\n"
            "\n## Nuance / work-history\n\n- 2000-01-04: a bullet.\n",
        )
        absent = _kinds(entry_shape.scan_entry_shape([path]), entry_shape.SHAPE_ABSENT)
        assert len(absent) == 1, absent
        assert absent[0].heading == entry_shape.POINTERS_HEADING
        # The WHOLE inventory, in document order — `## What it is` is absent from
        # this fixture on purpose, so the two headings below are the file's only
        # two and a scanner that returned a fixed-length slice cannot match.
        assert absent[0].found == (
            "## Links and references", "## Nuance / work-history",
        ), absent[0].found

    def test_the_INVENTORY_IS_CAPPED_AND_THE_REMAINDER_IS_COUNTED(self, tmp_path: Path):
        """A bound, not a filter. A file with a great many headings must not push
        the verdict off the operator's screen, and the count is what keeps the
        truncation honest.

        NINE headings against a cap of six, deliberately: nine OVERSHOOTS the cap
        rather than being a multiple of it, so a mutant that dropped the slice, or
        sliced by a different constant, cannot land on the same rendered string.
        """
        nine = "".join(f"\n## Section {i}\n\nprose {i}.\n" for i in range(1, 10))
        path = _shaped(tmp_path, "many-svc.md", nine)
        findings = entry_shape.scan_entry_shape([path])
        absent = _kinds(findings, entry_shape.SHAPE_ABSENT)
        assert len(absent) == 2, absent
        assert len(absent[0].found) == 9, absent[0].found
        blob = "\n".join(
            entry_shape.validation_advisory_lines(
                n_scanned=11, shape=findings, dropped=(), open_actions=(),
                unreachable=(),
            )
        )
        assert "… 3 more" in blob, blob
        assert "`## Section 6`" in blob, blob
        assert "`## Section 7`" not in blob, blob

    def test_a_DUPLICATED_heading_whose_MERGED_BODY_IS_EMPTY_yields_BOTH_findings(
        self, tmp_path: Path
    ):
        """🔴 THE DISJOINTNESS CLAIM, MADE OBSERVABLE. `duplicated` and `empty`
        are facts about the same heading with different remedies — "fold them into
        one section" and "write something under it" — and a reader handed one of
        the two has half a remedy. Neither branch in `scan_entry_shape` is an
        `elif`, and this is the fixture that can tell.

        THREE occurrences, not two: a mutant that hard-coded the count, or that
        reported presence as a boolean, cannot produce the rendered `appears 3
        times`.
        """
        path = _shaped(
            tmp_path, "dupempty-svc.md",
            "\n## Pointers\n\n- `apps/dupempty-svc/values.yaml`\n"
            "\n## Nuance / work-history\n"
            "\n## Nuance / work-history\n"
            "\n## Nuance / work-history\n",
        )
        findings = entry_shape.scan_entry_shape([path])
        dup = _kinds(findings, entry_shape.SHAPE_DUPLICATED)
        empty = _kinds(findings, entry_shape.SHAPE_EMPTY)
        assert len(dup) == 1 and dup[0].count == 3, findings
        assert len(empty) == 1, findings
        assert dup[0].heading == empty[0].heading == entry_shape.NUANCE_HEADING
        # 🔴 AND THE TWO ARE NEVER SUMMED INTO ONE NUMBER.
        blob = "\n".join(
            entry_shape.validation_advisory_lines(
                n_scanned=5, shape=findings, dropped=(), open_actions=(),
                unreachable=(),
            )
        )
        assert "1 heading(s) DUPLICATED" in blob, blob
        assert "1 section(s) PRESENT AND EMPTY" in blob, blob
        assert "2 heading(s) DUPLICATED" not in blob, blob

    def test_a_DUPLICATED_heading_with_a_NON_EMPTY_body_is_NOT_also_EMPTY(
        self, tmp_path: Path
    ):
        """The other side of the same disjointness: the `empty` branch must be a
        branch on the BODY, not a side effect of the duplicate count."""
        path = _shaped(
            tmp_path, "dupfull-svc.md",
            "\n## Pointers\n\n- `apps/dupfull-svc/values.yaml`\n"
            "\n## Nuance / work-history\n\n- 2000-01-04: one.\n"
            "\n## Nuance / work-history\n\n- 2000-01-05: two.\n",
        )
        findings = entry_shape.scan_entry_shape([path])
        assert len(_kinds(findings, entry_shape.SHAPE_DUPLICATED)) == 1, findings
        assert _kinds(findings, entry_shape.SHAPE_EMPTY) == [], findings

    def test_a_PRESENT_BUT_EMPTY_heading_is_NOT_reported_ABSENT(self, tmp_path: Path):
        """`extract_sections` tracks presence separately from content precisely so
        this distinction survives, and the remedies differ: "the section was never
        started" sends you to another file, "it is there and unfilled" does not."""
        path = _shaped(
            tmp_path, "empty-svc.md",
            "\n## Pointers\n"
            "\n## Nuance / work-history\n\n- 2000-01-04: a bullet.\n",
        )
        findings = entry_shape.scan_entry_shape([path])
        assert [f.kind for f in findings] == [entry_shape.SHAPE_EMPTY], findings
        assert findings[0].heading == entry_shape.POINTERS_HEADING

    def test_a_path_NO_SCANNER_MAY_OPEN_contributes_NOTHING(self, tmp_path: Path):
        """🔴 THE SAME GATE AS THE OTHER SCANNERS, AND IT IS NOT AN `except`.
        Reading a FIFO does not raise — it BLOCKS until somebody writes, and
        `validate` never returns. This scanner reads the WHOLE file rather than one
        section, which is exactly the route that could have bypassed the gate.
        """
        fifo = tmp_path / "wedge.md"
        os.mkfifo(fifo)
        real = _write(tmp_path, "- 2000-01-04: a bullet.", "real-svc.md")
        assert entry_shape.scan_entry_shape([real, fifo]) == ()


# --------------------------------------------------------------------------
# scan_open_actions — the four openness populations, never summed
# --------------------------------------------------------------------------

#: One bullet per population, plus the two the scan must be SILENT about. Kept as
#: a mapping so every assertion below names the population it is about rather than
#: an index into a list.
_POPULATION_BULLETS = {
    "open": "- 2000-01-05: OPEN: the writer declared this one, exactly.",
    "near-miss": "- 2000-01-06: **OPEN**: emphasis, so the marker never parses.",
    "unmarked": "- 2000-01-07: Open items: the retry budget is not yet addressed.",
    "unverifiable": "- 2000-01-08: RESOLVED: closed, and naming no sha at all.",
    "resolved": "- 2000-01-09: RESOLVED abc1234: closed, and reported by nothing.",
    "none": "- 2000-01-10: an ordinary bullet about an ordinary thing.",
}


class TestScanOpenActions:
    def test_ALL_FOUR_POPULATIONS_are_reported_and_the_OTHER_TWO_ARE_SILENT(
        self, tmp_path: Path
    ):
        """🔴 SIX POPULATIONS IN, FOUR OUT. `resolved` and `none` are the control:
        a scanner that reported every bullet it saw would produce six records and
        satisfy every count assertion below if they only checked "at least".

        Each of the four carries EXACTLY ONE flag — the populations are disjoint
        because `openness_population` is the single precedence source and this
        scanner branches on it once. A record with two flags is how one bullet came
        to be counted twice on one surface while being one thing on another.
        """
        path = _write(
            tmp_path, "\n".join(_POPULATION_BULLETS.values()), "populations-svc.md"
        )
        actions = entry_shape.scan_open_actions([path])
        assert len(actions) == 4, [a.first_line for a in actions]
        flagged = {
            "open": [a for a in actions if a.declared],
            "near-miss": [a for a in actions if a.near_miss],
            "unverifiable": [a for a in actions if a.unverifiable_closure],
        }
        guessed = [
            a for a in actions
            if not a.declared and not a.near_miss and not a.unverifiable_closure
        ]
        for pop, rows in flagged.items():
            assert len(rows) == 1, (pop, [a.first_line for a in rows])
            assert rows[0].first_line == _POPULATION_BULLETS[pop], (pop, rows[0])
        assert len(guessed) == 1
        assert guessed[0].first_line == _POPULATION_BULLETS["unmarked"]
        # The two controls appear in NO record at all.
        quoted = {a.first_line for a in actions}
        for silent in ("resolved", "none"):
            assert _POPULATION_BULLETS[silent] not in quoted, silent
        # EXACTLY ONE flag each: three booleans, at most one True.
        for a in actions:
            assert sum((a.declared, a.near_miss, a.unverifiable_closure)) <= 1, a

    def test_the_DATE_is_carried_from_the_BULLET_not_reconstructed(self, tmp_path: Path):
        """The dates in the fixture are pairwise distinct and none of them is a
        constant any assertion or any renderer names, so a scanner that invented a
        date — or reused one bullet's — cannot land on the right answer."""
        path = _write(
            tmp_path, "\n".join(_POPULATION_BULLETS.values()), "dates-svc.md"
        )
        by_line = {a.first_line: a.date for a in entry_shape.scan_open_actions([path])}
        assert by_line[_POPULATION_BULLETS["open"]] == "2000-01-05"
        assert by_line[_POPULATION_BULLETS["unverifiable"]] == "2000-01-08"

    def test_a_RENAMED_nuance_heading_makes_this_scan_find_NOTHING(
        self, tmp_path: Path
    ):
        """🔴 THE ORDERING ARGUMENT, AS BEHAVIOUR RATHER THAN AS A COMMENT. This
        scan reads only `## Nuance / work-history`, so an entry whose heading is
        renamed yields zero open actions NO MATTER HOW MANY IT HOLDS — and the
        `entry shape:` block is the only one that can say why. That is why it
        prints ABOVE this one: read in the other order, `0 declared` is false.
        """
        path = _shaped(
            tmp_path, "renamed-svc.md",
            "\n## Pointers\n\n- `apps/renamed-svc/values.yaml`\n"
            "\n## Nuance / Work-History\n\n"
            + "\n".join(_POPULATION_BULLETS.values()) + "\n",
        )
        assert entry_shape.scan_open_actions([path]) == ()
        # …and the shape scan is what reports it, in the SAME run.
        renamed = _kinds(entry_shape.scan_entry_shape([path]), entry_shape.SHAPE_RENAMED)
        assert len(renamed) == 1 and renamed[0].heading == entry_shape.NUANCE_HEADING

    def test_a_path_NO_SCANNER_MAY_OPEN_contributes_NOTHING(self, tmp_path: Path):
        fifo = tmp_path / "wedge.md"
        os.mkfifo(fifo)
        assert entry_shape.scan_open_actions([fifo]) == ()


# --------------------------------------------------------------------------
# The two NEW blocks, rendered
# --------------------------------------------------------------------------

class TestEntryShapeBlock:
    def test_the_ZERO_carries_its_DENOMINATOR_and_the_SET_IT_CHECKED(self):
        """🔴 A ZERO THAT DOES NOT NAME ITS SET IS A CLAIM ABOUT A HEADING NOTHING
        EXAMINED. The denominator says how many files; the SET says which headings
        — and without the second half a reader who assumes `## What it is` was
        checked takes this line as a guarantee about it.

        The denominator here is 13, which is not any length in this module's
        fixtures and not any constant the block names, so a renderer that printed
        a finding count, a cap, or a hard-coded number cannot produce it.
        """
        lines = entry_shape.validation_advisory_lines(
            n_scanned=13, shape=(), dropped=(), open_actions=(), unreachable=()
        )
        block = [ln for ln in lines if ln.startswith("entry shape:")]
        assert len(block) == 1, lines
        assert "13 entry file(s) checked for" in block[0], block[0]
        for heading in entry_shape.SHAPE_HEADINGS:
            assert f"`{heading}`" in block[0], heading
        assert "`## What it is` is NOT checked here" in block[0], block[0]

    def test_the_SPINE_IS_READ_FROM_SHAPE_HEADINGS_not_re_typed(self, monkeypatch):
        """🔴 THE PRINTED SET AND THE CHECKED SET ARE ONE OBJECT. A block that
        spelled its own heading list would keep printing the old pair after
        `SHAPE_HEADINGS` grew — the reassuring zero over a heading nothing looks
        at, arriving through a stale string.

        🔴 AND THAT CLAIM IS ONLY OBSERVABLE BY GROWING THE SET, WHICH THIS TEST
        DID NOT USED TO DO. It asserted `SHAPE_HEADINGS == (POINTERS, NUANCE)` and
        then looked for those two names in the block — which a HARDCODED literal
        spelling the same two names satisfies exactly, because the literal and the
        derived string are the same bytes as long as the set never moves.
        MEASURED, and the figures are the ones a re-runner reproduces rather than
        a remembered total: replacing the `spine = ", ".join(...)` line in
        `validation_advisory_lines` with `spine = "`## Pointers`, `## Nuance /
        work-history`"` leaves the ASSERT-ONLY version of this test GREEN — `68
        passed`, the whole module — and is RED here, `1 failed, 67 passed` of the
        68 this module collects, failing on THIS test. Run as
        `pytest tests/test_entry_shape_validate.py -q` with the mutant applied to
        `lib/entry_shape.py`. The guard read as coverage and provided none.

        ⚠ AN EARLIER WORDING OF THIS PARAGRAPH SAID `153 passed`, AND NO RUN OF
        THIS MODULE PRODUCES THAT NUMBER — `--collect-only -q` reports 68 for this
        file and 68 for the assert-only version of it. A wrong denominator sends a
        re-runner looking for a different suite, and finding none is a perfectly
        good reason to disbelieve the substantive claim beside it.

        So the set is PATCHED to carry a third heading this module never spells,
        and every expectation below is derived from the PATCHED value. A re-typed
        list cannot follow it, and the mutant above is RED.

        ⚠ THE OLD `SHAPE_HEADINGS == (POINTERS_HEADING, NUANCE_HEADING)` ASSERT IS
        GONE AND NOTHING WAS LOST WITH IT: `entry_shape` DEFINES the tuple as
        exactly that expression, from the same two imported constants, so the
        assert restated its own definition. It also could not survive the set
        growing, which is the property this test is about.
        """
        grown = entry_shape.SHAPE_HEADINGS + ("## Provenance / where it came from",)
        monkeypatch.setattr(entry_shape, "SHAPE_HEADINGS", grown)
        block = [
            ln for ln in entry_shape.validation_advisory_lines(
                n_scanned=13, shape=(), dropped=(), open_actions=(), unreachable=()
            )
            if ln.startswith("entry shape:")
        ][0]
        for heading in grown:
            assert f"`{heading}`" in block, (heading, block)
        # The ORDER is part of the printed contract the parity gate compares, and
        # it is checked across the WHOLE set rather than one pair: a renderer that
        # sorted, reversed or appended out of band is visible only at length 3.
        positions = [block.index(f"`{h}`") for h in grown]
        assert positions == sorted(positions), block

    def test_the_FINDINGS_branch_renders_EACH_KIND_SEPARATELY_and_sums_NOTHING(self):
        """🔴 FOUR DISJOINT KINDS, FOUR SUB-BLOCKS, FOUR COUNTS. A single
        "4 problems" total would be the number a writer acts on, and every one of
        the four needs a different edit."""
        shape = (
            entry_shape.ShapeFinding(
                "a.md", entry_shape.POINTERS_HEADING, entry_shape.SHAPE_RENAMED,
                found=("## pointers",),
            ),
            entry_shape.ShapeFinding(
                "b.md", entry_shape.POINTERS_HEADING, entry_shape.SHAPE_ABSENT,
                found=("## Elsewhere",),
            ),
            entry_shape.ShapeFinding(
                "c.md", entry_shape.NUANCE_HEADING, entry_shape.SHAPE_DUPLICATED,
                count=3,
            ),
            entry_shape.ShapeFinding(
                "d.md", entry_shape.NUANCE_HEADING, entry_shape.SHAPE_EMPTY,
            ),
        )
        blob = "\n".join(entry_shape.validation_advisory_lines(
            n_scanned=13, shape=shape, dropped=(), open_actions=(), unreachable=()
        ))
        for want in (
            "entry shape across 13 entry file(s), checked for",
            "🔴 1 section(s) RENAMED",
            "    a.md: `## Pointers` is written as `## pointers`",
            "🔴 1 section(s) ABSENT",
            "    b.md: no `## Pointers`; the file's headings are `## Elsewhere`",
            "🔴 1 heading(s) DUPLICATED",
            "    c.md: `## Nuance / work-history` appears 3 times",
            "⚠ 1 section(s) PRESENT AND EMPTY",
            "    d.md: `## Nuance / work-history`",
        ):
            assert want in blob, (want, blob)
        # 🔴 NO SUMMED TOTAL. Four findings, and "4" must not appear as a count of
        # them anywhere in the block.
        assert "4 section(s)" not in blob and "4 heading(s)" not in blob, blob

    def test_an_ABSENT_finding_with_NO_headings_at_all_says_so(self):
        """An empty inventory must read as a sentence, not as a dangling `are `."""
        blob = "\n".join(entry_shape.validation_advisory_lines(
            n_scanned=13,
            shape=(entry_shape.ShapeFinding(
                "e.md", entry_shape.NUANCE_HEADING, entry_shape.SHAPE_ABSENT,
            ),),
            dropped=(), open_actions=(), unreachable=(),
        ))
        assert "the file's headings are (none at all)" in blob, blob

    def test_it_is_ADVISORY_and_says_so(self):
        blob = "\n".join(entry_shape.validation_advisory_lines(
            n_scanned=13,
            shape=(entry_shape.ShapeFinding(
                "f.md", entry_shape.NUANCE_HEADING, entry_shape.SHAPE_EMPTY,
            ),),
            dropped=(), open_actions=(), unreachable=(),
        ))
        assert "changes no verdict" in blob, blob
        assert "Fix the heading, not the exit code." in blob, blob


class TestOpenActionsBlock:
    def test_the_ZERO_carries_its_DENOMINATOR_and_names_the_FLOOR(self):
        """🔴 THE SECOND HALF OF THIS ZERO IS A FLOOR WITH UNKNOWN RECALL, NOT A
        CLEAN BILL OF HEALTH. `OPEN:` is exact; the unmarked guess is two measured
        phrasings, so an unfinished action phrased any other way is invisible — and
        a line that did not say so would be read as "there are none"."""
        lines = entry_shape.validation_advisory_lines(
            n_scanned=13, shape=(), dropped=(), open_actions=(), unreachable=()
        )
        block = [ln for ln in lines if ln.startswith("open actions:")]
        assert len(block) == 1, lines
        assert "0 declared across 13 entry file(s)" in block[0], block[0]
        assert "FLOOR with unknown recall" in block[0], block[0]

    def test_the_FOUR_POPULATIONS_render_SEPARATELY_and_are_NEVER_SUMMED(self):
        """🔴 THE FLOOR MUST NOT MASQUERADE AS A COUNT. `declared` is exact and
        `unmarked` is a guess; one total over both is a number nobody can act on.

        The four sub-counts are 2, 3, 5 and 7 — pairwise distinct, none equal to
        their total (17) or to any pairwise sum, so a renderer that added any two
        of them cannot land on a string these assertions accept.
        """
        def rows(tag: str, n: int, **flags):
            return tuple(
                entry_shape.OpenAction(
                    filename=f"{tag}-{i}.md",
                    declared=flags.get("declared", False),
                    near_miss=flags.get("near_miss", False),
                    unverifiable_closure=flags.get("unverifiable_closure", False),
                    date="2000-01-04",
                    first_line=f"- 2000-01-04: {tag} row {i}.",
                )
                for i in range(n)
            )
        actions = (
            rows("declared", 2, declared=True)
            + rows("near", 3, near_miss=True)
            + rows("guess", 5)
            + rows("unverifiable", 7, unverifiable_closure=True)
        )
        assert len(actions) == 17
        blob = "\n".join(entry_shape.validation_advisory_lines(
            n_scanned=13, shape=(), dropped=(), open_actions=actions, unreachable=()
        ))
        assert "open actions across 13 entry file(s):" in blob, blob
        assert "🔴 2 declared `OPEN:`" in blob, blob
        assert "🔴 3 bullet(s) look like an ATTEMPTED marker" in blob, blob
        assert "⚠ 5 unmarked bullet(s)" in blob, blob
        assert "⚠ 7 `RESOLVED:` bullet(s) name no sha" in blob, blob
        # 🔴 NO TOTAL, AND NO PAIRWISE SUM.
        for summed in (17, 5 + 7, 2 + 3, 2 + 5, 3 + 7, 2 + 7, 3 + 5):
            if summed in (2, 3, 5, 7):
                continue
            assert f"{summed} declared" not in blob, summed
        assert "17" not in blob, blob

    def test_a_long_first_line_is_cut_to_the_declared_quote_length(self):
        """The same bound the sibling blocks use, and the same reason: a validator
        that reprints a 4,000-character bullet has buried its own verdict.

        RUNES, not bytes — the oracle slices a `str`, and the Go port must agree.
        """
        long = "é" * 500
        blob = "\n".join(entry_shape.validation_advisory_lines(
            n_scanned=13, shape=(), dropped=(),
            open_actions=(entry_shape.OpenAction(
                "a.md", True, "2000-01-04", long
            ),),
            unreachable=(),
        ))
        quoted = [ln.strip() for ln in blob.splitlines() if ln.strip().startswith("a.md: é")]
        assert quoted, blob
        assert len(quoted[0]) == len("a.md: ") + entry_shape.ADVISORY_QUOTE_MAX

    def test_it_is_ADVISORY_and_says_so(self):
        blob = "\n".join(entry_shape.validation_advisory_lines(
            n_scanned=13, shape=(), dropped=(),
            open_actions=(entry_shape.OpenAction(
                "a.md", True, "2000-01-04", "- 2000-01-04: OPEN: x."
            ),),
            unreachable=(),
        ))
        assert "None of this changes the verdict" in blob, blob
        assert "red gate nobody could turn green" in blob, blob


class TestTheFourBlocksAreOrdered:
    """🔴 THE ORDER IS THE CLAIM, NOT A LAYOUT PREFERENCE.

    Three of the four blocks read only `## Nuance / work-history`. A renamed
    heading makes every one of them read an empty section, so each prints a zero
    about a section no parser reached — and `entry shape:` is the only block that
    can say why. Printed below them, it is an afterthought; printed above them, it
    is the reason.

    `dropped lines:` then precedes the two bullet-level blocks for the sibling
    reason: a dropped line is content NO reader reaches, so neither of those scans
    ever sees it.
    """

    #: The four block leaders, in the order they must print.
    ORDER = ("entry shape:", "dropped lines:", "open actions:", "marker reachability:")

    def test_the_ZERO_branch_prints_all_four_IN_ORDER(self):
        lines = entry_shape.validation_advisory_lines(
            n_scanned=13, shape=(), dropped=(), open_actions=(), unreachable=()
        )
        leaders = [ln.split(" — ")[0] for ln in lines if ln]
        at = [
            next(i for i, ln in enumerate(lines) if ln.startswith(want))
            for want in self.ORDER
        ]
        assert at == sorted(at), (at, leaders)

    def test_the_FINDINGS_branch_prints_all_four_IN_ORDER(self):
        blob = "\n".join(entry_shape.validation_advisory_lines(
            n_scanned=13,
            shape=(entry_shape.ShapeFinding(
                "a.md", entry_shape.NUANCE_HEADING, entry_shape.SHAPE_EMPTY,
            ),),
            dropped=(entry_shape.DroppedLineFinding("a.md", 1, "  lost."),),
            open_actions=(entry_shape.OpenAction(
                "a.md", True, "2000-01-04", "- 2000-01-04: OPEN: x."
            ),),
            unreachable=(entry_shape.UnreachableMarkerFinding(
                "a.md", "- 2000-01-04: a bullet.", 2, "  OPEN: x.", "open"
            ),),
        ))
        at = [
            blob.index(want) for want in (
                "entry shape across", "DROPPED LINE(S)", "open actions across",
                "MARKER(S) OUT OF REACH",
            )
        ]
        assert at == sorted(at), at

    def test_NOT_CHECKED_withholds_ALL_FOUR_and_names_them(self):
        """🔴 ALL-OR-NOTHING. A run that withheld the two older blocks and still
        printed `entry shape: 0 entry file(s) checked` would emit exactly the
        reassuring zero the `NOT CHECKED` branch exists to prevent — and the
        existing `0 across 0` assertion cannot see it, because that is not the
        spelling either new block uses."""
        lines = entry_shape.validation_advisory_lines(
            n_scanned=0, shape=(), dropped=(), open_actions=(), unreachable=()
        )
        assert len(lines) == 1, lines
        assert "NOT CHECKED" in lines[0]
        for leader in self.ORDER:
            assert not lines[0].startswith(leader), leader
        for named in ("entry shape", "dropped lines", "open actions",
                      "marker reachability"):
            assert named in lines[0], named
