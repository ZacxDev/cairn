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
        and an advisory that raised here would replace it with a traceback."""
        missing = tmp_path / "never-written.md"
        assert entry_shape.scan_dropped_lines([missing]) == ()
        assert entry_shape.scan_unreachable_markers([missing]) == ()

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
            # 🔴 THE WORD-BOUNDARY GUARD'S OWN DISCRIMINATING INPUT, and it is
            # narrow on purpose. `RESOLVED_ADDR appears…` above is rejected by the
            # COLON rule and would pass with no boundary guard at all, so it
            # cannot witness the guard. These two put a colon within reach of the
            # token, so the only thing that can reject them is the boundary.
            "  RESOLVED_ADDR: the symbol named in the trace.",
            "  OPEN_: an identifier, not a declaration.",
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
            n_files=0, dropped=(), unreachable=()
        )
        assert len(lines) == 1
        assert "NOT CHECKED" in lines[0]
        assert "0 across 0" not in lines[0]
        assert entry_shape.DROPPED_LINE in lines[0]
        assert entry_shape.UNREACHABLE_MARKER in lines[0]

    def test_both_zeros_carry_their_DENOMINATOR(self):
        """A bare zero is indistinguishable from a scanner wired to nothing."""
        lines = entry_shape.validation_advisory_lines(
            n_files=7, dropped=(), unreachable=()
        )
        blob = "\n".join(lines)
        assert "dropped lines: 0 across 7 entry file(s)" in blob
        assert "marker reachability: 0 out-of-reach marker(s) across 7 entry file(s)" in blob

    def test_the_dropped_zero_states_its_OWN_blind_spot(self):
        """🔴 UNLIKE ITS SIBLING, THIS CHECK IS KNOWINGLY PARTIAL. A bare "0
        dropped" would read as "no bullet has lost its head", which is a claim it
        cannot make."""
        lines = entry_shape.validation_advisory_lines(
            n_files=1, dropped=(), unreachable=()
        )
        assert "PARTIAL BY CONSTRUCTION" in "\n".join(lines)

    def test_the_DROPPED_block_comes_BEFORE_the_reachability_block(self):
        """🔴 A dropped line is content NO reader reaches, so the marker scan never
        sees it. A `0 out-of-reach` printed ABOVE a `🔴 N DROPPED LINE(S)` is a
        fact about text the parser never got to, and reads as a reassurance it
        cannot support. That is the order the pre-extraction original shipped in
        until an audit read its rendered output for a real broken entry."""
        lines = entry_shape.validation_advisory_lines(
            n_files=1,
            dropped=(entry_shape.DroppedLineFinding("a.md", 1, "  lost."),),
            unreachable=(),
        )
        blob = "\n".join(lines)
        assert blob.index("DROPPED LINE(S)") < blob.index("marker reachability:")

    def test_findings_are_quoted_with_their_file_and_offset(self):
        lines = entry_shape.validation_advisory_lines(
            n_files=2,
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
        assert "🔴 2 DROPPED LINE(S) across 2 entry file(s)" in blob
        assert "1 of them looks like a `OPEN:`/`RESOLVED:` DECLARATION" in blob
        assert "    talus-svc.md: nuance line 1" in blob
        assert "    talus-svc.md: nuance line 2  ← looks like a DECLARATION" in blob
        assert "🔴 1 MARKER(S) OUT OF REACH across 2 entry file(s)" in blob
        assert "    talus-svc.md: line 2 of the bullet opening" in blob
        assert "(would declare `open`)" in blob

    def test_a_long_line_is_cut_to_the_declared_quote_length(self):
        """The quote is there to recognise a finding by; an entry may hold a
        4,000-character bullet, and a validator that reprints one has buried its
        own verdict."""
        long_line = "x" * 500
        lines = entry_shape.validation_advisory_lines(
            n_files=1,
            dropped=(entry_shape.DroppedLineFinding("a.md", 1, long_line),),
            unreachable=(),
        )
        quoted = [ln for ln in lines if ln.strip().startswith("x")]
        assert quoted and len(quoted[0].strip()) == entry_shape.ADVISORY_QUOTE_MAX
