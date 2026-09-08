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
    """Every file the SCANNER would consider, before its suffix filter.

    🔴 THE FLAGS MUST MATCH `leakscan.tracked_files`, AND THE FIRST VERSION OF
    THIS DID NOT. It shelled a bare `git ls-files` — cached only — while the
    scanner enumerates `--cached --others --exclude-standard`, deliberately,
    because its own docstring says "`git ls-files` ALONE IS BLIND to a file not
    yet added, and 'I forgot to git add it' is not a reason for a leak to ship."
    So the guard written to make a coverage gap impossible re-introduced exactly
    that blindness one level up.

    MEASURED: an UNTRACKED `notes.rst` holding a real hostname and an email
    address scanned clean (`0 findings across 38 file(s)`, rc 0) and this guard
    passed, while the byte-identical content in `notes.md` was caught
    immediately — so the scanner could see the leak and the suffix set skipped
    it. The exposure window was precisely the pre-`git add` window leakscan
    exists to cover.

    The enumeration is duplicated rather than imported because this test must
    be able to see files the scanner's suffix filter has already dropped —
    that is the whole question it asks. What must not diverge is the FLAGS, so
    they are stated once here with the reason, and `test_this_guard_and_the_scanner_
    enumerate_the_same_files` pins the two against each other — the flags AND
    the `-z` framing, both of which `_enumerate`'s docstring explains.
    """
    return _enumerate(ROOT)


def _enumerate(root: Path) -> list[str]:
    """The enumeration itself, parameterised so a fixture can reach it.

    🔴 IT TAKES A ROOT BECAUSE THE PARITY TEST BELOW CANNOT OTHERWISE REACH THE
    DIFFERENCE IT CHECKS. In a clean checkout every file is committed, so
    `--cached` and `--cached --others` return the SAME set and a parity
    assertion is satisfied by two identical lists no matter which flags either
    side uses. Measured: with this hardcoded to ROOT, a mutant narrowing it
    back to cached-only SURVIVED the whole suite. The difference only exists
    when an UNTRACKED file does, so the test builds one.

    🔴 `-z` IS PART OF THE CONTRACT, NOT A DETAIL. `git ls-files` QUOTES a
    path containing non-ASCII bytes under the default `core.quotePath`, so
    without `-z` this returns `"caf\303\251.md"` where `leakscan.tracked_files`
    (which does pass `-z`) returns `café.md`. MEASURED with an untracked
    `café.md` in the tree: the coverage test failed with `tracked file type(s)
    ['.md"']` and told the developer to add `.md"` to `TEXT_SUFFIXES`, and the
    parity test failed saying "the flags have diverged" — **which was false**.
    The flags were identical; the OUTPUT ENCODING was not, and a maintainer
    following that message would have changed the one thing that was right.

    So what must not diverge is the flags AND the framing. Both are stated
    here once, beside the reason.
    """
    out = subprocess.run(
        ["git", "-C", str(root), "ls-files", "--cached", "--others",
         "--exclude-standard", "-z"],
        capture_output=True, text=True, check=True,
    ).stdout
    return [name for name in out.split("\0") if name]


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


def test_this_guard_and_the_scanner_enumerate_the_same_files():
    """🔴 PINS THE ONE THING THAT MUST NOT DIVERGE — the enumeration FLAGS.

    This guard asks "is any file type unscanned?", so it must start from the
    same candidate set the scanner does. If it starts from a NARROWER set, a
    file outside it is invisible to both and the guard reports coverage it does
    not have — which is what happened: a bare `git ls-files` here versus
    `--cached --others --exclude-standard` there.

    Every file the SCANNER returns must appear in this module's enumeration.
    The reverse does not hold and must not be asserted: the scanner has already
    applied its suffix filter, so it legitimately returns fewer files. Pinning
    equality would fail on every correctly-skipped binary.
    """
    mine = set(tracked_files())
    theirs = {str(Path(p).relative_to(ROOT)) for p in leakscan.tracked_files()}
    missing = sorted(theirs - mine)
    assert not missing, (
        f"the scanner considers {missing} which this guard's enumeration does "
        f"not see — the flags have diverged, so this guard is blind to exactly "
        f"the files it exists to check"
    )


def test_the_enumeration_sees_an_UNTRACKED_file(tmp_path, monkeypatch):
    """🔴 THE CASE THE PARITY TEST ABOVE CANNOT REACH ON ITS OWN.

    In a clean checkout every file is committed, so cached-only and
    cached-plus-others return identical sets and the parity assertion holds
    whatever flags either side uses. MEASURED: a mutant narrowing `_enumerate`
    to a bare `git ls-files` SURVIVED the entire suite. The blindness only
    becomes observable when an untracked file exists, so this builds one in a
    throwaway repo and drives BOTH enumerations against it — the shipped code,
    not a copy of it.

    This is the failure the whole module exists for: leakscan scans untracked
    files deliberately ("'I forgot to git add it' is not a reason for a leak to
    ship"), so a coverage guard that cannot see them certifies nothing about
    precisely the window that matters.
    """
    repo = tmp_path / "repo"
    (repo / "sub").mkdir(parents=True)
    (repo / "tracked.md").write_text("# tracked\n", encoding="utf-8")
    subprocess.run(["git", "-C", str(repo), "init", "-q"], check=True)
    subprocess.run(["git", "-C", str(repo), "add", "tracked.md"], check=True)

    # The untracked file, of a type the scanner reads.
    (repo / "sub" / "untracked.md").write_text("# untracked\n", encoding="utf-8")

    seen = _enumerate(repo)
    assert "sub/untracked.md" in seen, (
        f"the enumeration missed an UNTRACKED file (saw {seen}) — it is "
        f"cached-only, so it is blind to the pre-`git add` window that "
        f"leakscan deliberately covers"
    )
    assert "tracked.md" in seen, "the enumeration missed a TRACKED file"

    # And the scanner's own enumeration agrees, driven against the same repo.
    monkeypatch.setattr(leakscan, "ROOT", repo)
    theirs = {str(Path(p).relative_to(repo)) for p in leakscan.tracked_files()}
    assert "sub/untracked.md" in theirs, (
        "the SCANNER does not see the untracked file either — this test's "
        "premise about leakscan's flags is wrong, fix the premise not the flags"
    )


def test_a_quoted_path_does_not_produce_a_false_diagnosis(tmp_path, monkeypatch):
    """🔴 THE `-z` HALF, AND ITS ABSENCE GAVE A CONFIDENTLY WRONG REMEDY.

    `git ls-files` QUOTES non-ASCII paths under the default `core.quotePath`.
    Without `-z`, this module saw `"caf\\303\\251.md"` where the scanner saw
    `café.md`, and the two tests above then failed with diagnoses that named
    the wrong cause: one told the developer to add `.md"` (with a quote
    character) to `TEXT_SUFFIXES`, the other said "the flags have diverged"
    when the flags were identical.

    🔴 A WRONG REMEDY IS WORSE THAN A MISSING ONE — a maintainer following
    either message would have changed something that was already correct. So
    this pins the encoding, not just the flag list, with a filename that
    actually triggers quoting.
    """
    repo = tmp_path / "repo"
    repo.mkdir()
    subprocess.run(["git", "-C", str(repo), "init", "-q"], check=True)
    (repo / "café.md").write_text("# accented\n", encoding="utf-8")

    seen = _enumerate(repo)
    assert "café.md" in seen, (
        f"the enumeration returned {seen!r} rather than the real filename — "
        f"`git ls-files` quoted it, which means `-z` is missing and every "
        f"diagnosis this module produces about such a file names the wrong cause"
    )

    monkeypatch.setattr(leakscan, "ROOT", repo)
    theirs = {str(Path(p).relative_to(repo)) for p in leakscan.tracked_files()}
    assert seen and set(seen) == theirs, (
        f"this guard sees {sorted(seen)} and the scanner sees {sorted(theirs)} "
        f"for the same tree — the two enumerations disagree on ENCODING, not "
        f"on which files exist"
    )


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
