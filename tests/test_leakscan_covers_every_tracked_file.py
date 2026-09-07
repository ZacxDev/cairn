"""The leak gate's coverage is an ENUMERATION, so pin what it enumerates.

🔴 WHY. `tests/leakscan.py` decides what to read from a hand-written
`TEXT_SUFFIXES` set. A file type absent from it is skipped silently while the
run prints a confident `0 findings across N file(s)` — and N is files SCANNED,
never files present, so nothing in the output distinguishes "clean" from "did
not look". This repository is PUBLIC and was extracted from a private one; the
leak gate is the reason it can be public at all, which makes a silent gap in
its coverage the most expensive kind of bug here.

It was not hypothetical. `.nix` was missing when `flake.nix` — hand-written
prose, the exact thing the scanner exists to read — was added, and the gate
reported clean over a tree it had not fully read.

⚠ THIS DOES NOT MAKE THE COVERAGE DERIVED, and that is still the better fix.
It pins the enumeration against the tracked tree, which catches the case that
actually happened (a new file type nobody added). A genuinely derived scanner
would not need this file.
"""
from __future__ import annotations

import subprocess
import sys
from pathlib import Path

import pytest

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT / "tests"))

import leakscan  # noqa: E402


def tracked_files() -> list[str]:
    out = subprocess.run(
        ["git", "-C", str(ROOT), "ls-files"],
        capture_output=True, text=True, check=True,
    ).stdout
    return [line for line in out.splitlines() if line]


# Types that are genuinely not text and must NOT be scanned. An entry here is a
# deliberate exemption, so it is spelled out rather than pattern-matched: the
# point of the test is that adding a type is a decision somebody makes on
# purpose, in the commit that adds it.
BINARY_SUFFIXES = {".png", ".jpg", ".jpeg", ".gif", ".ico", ".pdf", ".zip", ".gz", ".woff2"}


def test_the_tracked_tree_has_files_to_check():
    """🔴 POSITIVE CONTROL. Without this, an empty `git ls-files` — a wrong cwd,
    a missing git, a detached environment — makes every assertion below pass
    over nothing, which is the same silent zero this file exists to prevent."""
    files = tracked_files()
    assert len(files) > 20, f"expected a populated repo, got {len(files)} tracked file(s)"


def test_every_tracked_text_file_has_a_suffix_the_scanner_reads():
    """The enumeration must cover the tree as it actually is, today."""
    missed = sorted({
        Path(f).suffix
        for f in tracked_files()
        if Path(f).suffix not in leakscan.TEXT_SUFFIXES
        and Path(f).suffix not in BINARY_SUFFIXES
    })
    assert not missed, (
        f"tracked file type(s) {missed} are not in leakscan's TEXT_SUFFIXES and "
        f"are not declared binary, so the scan SKIPS them and still reports "
        f"`0 findings`. Add each to TEXT_SUFFIXES (or to BINARY_SUFFIXES here "
        f"if it is genuinely not text) in the commit that introduces it."
    )


@pytest.mark.parametrize("suffix", [".nix", ".py", ".md", ".yml", ".sh", ".json"])
def test_the_types_this_repo_actually_carries_are_scanned(suffix):
    """Named explicitly, so deleting one from `TEXT_SUFFIXES` fails HERE.

    The test above is derived from the tree and would go quiet if the last file
    of some type were removed — at which point dropping the suffix would look
    free, and re-adding a file of that type later would silently be unscanned.
    `.nix` is first in the list because it is the one that was actually missing.
    """
    assert suffix in leakscan.TEXT_SUFFIXES


def test_the_scanner_reads_the_flake_when_it_walks_the_tree():
    """Behavioural, not structural — a suffix set is not a code path.

    `TEXT_SUFFIXES` containing `.nix` is a declaration; this drives the
    scanner's own file walk and asserts `flake.nix` is in what it returns.
    Without it the set could be right while a second filter dropped the file.
    """
    scanned = {str(Path(p).relative_to(ROOT)) for p in leakscan.tracked_files()}
    assert "flake.nix" in scanned, (
        f"the scanner's own walk does not return flake.nix (returned "
        f"{len(scanned)} file(s)) — the suffix set is not the only filter"
    )
