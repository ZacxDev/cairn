"""The leak gate's coverage is DERIVED from content — pin that it stays derived.

🔴 WHY. `tests/leakscan.py` used to decide what to read from a hand-written
`TEXT_SUFFIXES` set. A file type absent from it was skipped silently while the
run printed a confident `0 findings across N file(s)` — and N is files SCANNED,
never files present, so nothing in the output distinguished "clean" from "did
not look". This repository is PUBLIC and was extracted from a private one; the
leak gate is the reason it can be public at all, which makes a silent gap in its
coverage the most expensive kind of bug here.

It was not hypothetical. `.nix` was missing when `flake.nix` — hand-written
prose, the exact thing the scanner exists to read — was added, and the gate
reported clean over a tree it had not fully read. `.dockerignore` was missing
before that. Each was closed by hand, after the fact.

⚠ THE EARLIER VERSION OF THIS FILE PINNED THE ENUMERATION AGAINST THE TRACKED
TREE, and said in its own docstring that this "does not make the coverage
derived, and that is still the better fix. A genuinely derived scanner would not
need this file." That fix has now landed, so these tests changed shape: they no
longer ask "is every suffix declared?" — there is no list to declare into — but
"is every enumerated file accounted for, and is an unfamiliar text type actually
READ?"

🔴 THESE DRIVE THE SHIPPED FUNCTIONS, NEVER A COPY. The previous version
duplicated the git enumeration here and had to justify the duplication at
length; the module then grew a test whose only job was to police the drift
between the copy and the original. `leakscan.enumerate_repo` now takes a root,
so this file calls it. A control re-implemented from the instrument it validates
certifies nothing — that failure is on record in this project already.
"""
from __future__ import annotations

import subprocess
import sys
from pathlib import Path

import pytest

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT / "tests"))

import leakscan  # noqa: E402


def _leak() -> str:
    """A realistic leak — assembled at runtime, NEVER written as one literal.

    🔴 THAT IS NOT FASTIDIOUSNESS, IT IS THE ONLY WAY THIS FILE CAN EXIST. This
    module is itself scanned by the gate it tests: it is not in `SKIP_FILES` and
    must not be, since exempting a test file is how the exempt set grows until
    it hides something. But a payload realistic enough to prove the scanner READ
    a file is by definition a payload the scanner REFUSES — so writing it out
    whole here would turn the leak gate red on its own suite.

    The fragments are joined at run time, so no single SOURCE LINE contains the
    pattern while every RUNTIME value does. ⚠ Do not "tidy" this into a single
    string; `test_the_payload_is_one_the_gate_actually_refuses` below is what
    catches the opposite mistake, a payload so defanged it proves nothing.
    """
    return "deploy target: store.example-real." + "zacx" + "." + "dev" + "\n"


def _repo_with(tmp_path: Path, name: str, body: str | bytes) -> Path:
    """A throwaway git repo holding one committed file. Returns the repo root."""
    repo = tmp_path / "repo"
    repo.mkdir()
    subprocess.run(["git", "-C", str(repo), "init", "-q"], check=True)
    target = repo / name
    target.parent.mkdir(parents=True, exist_ok=True)
    if isinstance(body, bytes):
        target.write_bytes(body)
    else:
        target.write_text(body, encoding="utf-8")
    subprocess.run(["git", "-C", str(repo), "add", "-A"], check=True)
    return repo


def test_the_payload_is_one_the_gate_actually_refuses():
    """🔴 THE CONTROL ON THIS MODULE'S OWN FIXTURE, AND IT IS LOAD-BEARING.

    Every coverage test here proves a file was READ by planting `_leak()` in it
    and watching the gate refuse. If the payload were defanged — a fragment
    mis-joined, a rule later narrowed, a "tidy-up" that broke the concatenation
    — those tests would keep passing while asserting nothing at all, because a
    file that is read and a file that is skipped both produce zero findings for
    content that is not a leak.

    So: the assembled string must be refused, and refused as a hostname
    specifically. This is the positive control that makes every `0 findings`
    elsewhere in this module mean something.
    """
    found = leakscan.scan_text(_leak(), "<fixture-control>")
    assert found, (
        "the fixture payload is NOT refused by the gate, so every test in this "
        "module that plants it proves nothing — a read file and a skipped file "
        "would both come back clean"
    )
    assert {f.rule for f in found} == {"reachable-hostname"}, (
        f"the payload trips {sorted({f.rule for f in found})} rather than the "
        f"hostname rule it was written for"
    )


def test_the_tracked_tree_has_files_to_check():
    """🔴 POSITIVE CONTROL. Without this, an empty `git ls-files` — a wrong cwd,
    a missing git, a detached environment — makes every assertion below pass
    over nothing, which is the same silent zero this file exists to prevent."""
    files = leakscan.enumerate_repo(ROOT)
    assert len(files) > 20, f"expected a populated repo, got {len(files)} tracked file(s)"


def test_a_tracked_text_file_of_an_UNFAMILIAR_TYPE_is_scanned(tmp_path, monkeypatch):
    """🔴 THE REGRESSION TEST. RED BEFORE THIS CHANGE, GREEN AFTER.

    `.rst` was not in the old `TEXT_SUFFIXES`, and nothing about it is special —
    it stands in for whatever file type this repository gains next, which is the
    case the enumeration could never get ahead of.

    MEASURED at the merge base `9213726`, driving this same shipped
    `tracked_files()`: the file is ABSENT from the returned set, the scan reads
    38 of 39 files and prints `0 findings`, exit 0 — a clean bill of health over
    a hostname it never looked at. With coverage derived from the bytes it is
    read like any other text file.

    This asserts COVERAGE (the file is in the set to be read). That the leak is
    then actually reported is a separate claim, made by the test below — a file
    can be in the set while a second filter drops it.
    """
    repo = _repo_with(tmp_path, "notes.rst", _leak())
    monkeypatch.setattr(leakscan, "ROOT", repo)

    scanned = {str(Path(p).relative_to(repo)) for p in leakscan.tracked_files()}
    assert "notes.rst" in scanned, (
        f"a tracked, plainly-textual .rst file is not in the set the scanner "
        f"reads (it returned {sorted(scanned)}) — coverage is being decided by "
        f"the file's NAME rather than its CONTENT, so the next new file type in "
        f"this public repo ships unscanned under a confident `0 findings`"
    )


def test_the_leak_in_that_unfamiliar_type_is_actually_REFUSED(tmp_path, monkeypatch, capsys):
    """🔴 BEHAVIOURAL, NOT STRUCTURAL — a set membership is not a code path.

    The test above proves the file reaches the scan list. This drives `main()`
    end to end and asserts the run REFUSES: exit 1, with the finding naming the
    file. Without it, coverage could be correct while the reading, decoding or
    reporting leg dropped the file anyway.
    """
    repo = _repo_with(tmp_path, "notes.rst", _leak())
    monkeypatch.setattr(leakscan, "ROOT", repo)

    rc = leakscan.main([])
    out = capsys.readouterr().out

    assert rc == 1, (
        f"a real hostname in a tracked .rst file did not turn the gate red "
        f"(exit {rc}) — the scan looked at the file and published it anyway"
    )
    assert "notes.rst" in out, (
        "the run refused, but its output does not name the file that caused it"
    )


def test_a_BINARY_file_is_skipped_and_the_skip_is_NAMED(tmp_path, monkeypatch):
    """The other half of derived coverage: what is provably binary stays unread.

    🔴 AND IT MUST BE NAMED, NOT MERELY DROPPED. An unread file that appears
    nowhere in the output is exactly the silent gap this module exists to close;
    a skip is only acceptable while a reader can see it and check the reason.
    """
    blob = b"\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR" + b"\x00" * 64
    repo = _repo_with(tmp_path, "logo.png", blob)
    monkeypatch.setattr(leakscan, "ROOT", repo)

    scanned, skipped = leakscan.partition_tracked_files()
    assert not [p for p in scanned if p.name == "logo.png"], (
        "a NUL-bearing binary was queued for scanning"
    )
    entry = next((s for s in skipped if s.path == "logo.png"), None)
    assert entry is not None, (
        f"the binary was neither scanned nor reported as skipped — it fell out "
        f"of the accounting entirely (skips were {[s.path for s in skipped]})"
    )
    assert "binary" in entry.why, (
        f"the skip is unexplained ({entry.why!r}), so a reader cannot tell a "
        f"correctly-ignored binary from a file the gate cannot see"
    )


def test_a_TEXT_file_with_no_suffix_at_all_is_scanned(tmp_path, monkeypatch):
    """A name carries no evidence about content, and this is the extreme case.

    `Dockerfile`, `LICENSE` and `.dockerignore` are all real examples in this
    tree. The old set handled them by listing `""` and `".dockerignore"`
    explicitly — two more entries that had to be thought of in advance.
    """
    repo = _repo_with(tmp_path, "Dockerfile", _leak())
    monkeypatch.setattr(leakscan, "ROOT", repo)

    scanned = {str(Path(p).relative_to(repo)) for p in leakscan.tracked_files()}
    assert "Dockerfile" in scanned


def test_every_enumerated_file_lands_in_EXACTLY_one_bucket():
    """🔴 THE STRUCTURAL INVARIANT THAT REPLACED THE SUFFIX LIST.

    Scanned ∪ skipped must equal the enumeration exactly — no file dropped, no
    file counted twice. This is what makes "0 findings across N files" mean
    something: N plus the named skips accounts for every file git reports.

    Asserted against the REAL tree, and it re-implements none of the filtering
    it checks — `partition_tracked_files` buckets even the directory skips, so
    both sides of this comparison come from shipped code.
    """
    scanned, skipped = leakscan.partition_tracked_files()
    enumerated = set(leakscan.enumerate_repo(ROOT))

    got = [str(Path(p).relative_to(ROOT)) for p in scanned] + [s.path for s in skipped]
    assert len(got) == len(set(got)), (
        f"a file appears in more than one bucket: "
        f"{sorted({x for x in got if got.count(x) > 1})}"
    )
    assert set(got) == enumerated, (
        f"the accounting does not reconcile with the enumeration. Unaccounted "
        f"for: {sorted(enumerated - set(got))}; invented: "
        f"{sorted(set(got) - enumerated)}. A file in neither bucket is read by "
        f"nothing and reported by nothing."
    )


def test_a_real_run_PRINTS_every_skip(capsys):
    """The invariant above is about the data; this is about the OUTPUT.

    🔴 A PROPERTY TRUE IN MEMORY AND ABSENT FROM THE REPORT DOES NOT HELP THE
    PERSON READING CI. The run is the only artefact anyone sees, so the skips
    have to appear in it — this drives the real `main()` over the real tree and
    reads its stdout.
    """
    rc = leakscan.main([])
    out = capsys.readouterr().out
    assert rc == 0, f"the tree is not clean (exit {rc}); this test cannot judge output"

    _, skipped = leakscan.partition_tracked_files()
    assert skipped, (
        "no skips exist in this tree, so this test would pass vacuously — it "
        "asserts that skips are PRINTED, and needs at least one to exist. "
        "`tests/leakscan.py` itself is exempt by name and should be here."
    )
    for s in skipped:
        assert s.path in out, (
            f"{s.path} was not scanned and is not named in the run's output — "
            f"it is invisible to anyone reading the verdict"
        )
    assert "SKIPPED" in out


def test_the_types_this_repo_actually_carries_are_read():
    """Behavioural coverage of the tree as it stands, derived from the tree.

    The earlier version asserted suffix membership in `TEXT_SUFFIXES` and worried
    in its docstring that a tree-derived check "would go quiet if the last file
    of some type were removed — at which point dropping the suffix would look
    free". That concern is now void: there is no set to drop an entry from, so
    the only thing worth asserting is that the real files of each type are in
    fact read.
    """
    scanned = {str(Path(p).relative_to(ROOT)) for p in leakscan.tracked_files()}
    by_suffix: dict[str, list[str]] = {}
    for n in leakscan.enumerate_repo(ROOT):
        by_suffix.setdefault(Path(n).suffix, []).append(n)

    for suffix in (".nix", ".py", ".md", ".yml", ".sh", ".json"):
        present = by_suffix.get(suffix, [])
        assert present, f"this repo no longer carries any {suffix} file"
        unread = [n for n in present if n not in scanned and n not in leakscan.SKIP_FILES]
        assert not unread, f"{suffix} file(s) {unread} are enumerated but not read"


@pytest.mark.parametrize(
    "case,expected,why",
    [
        ("source", False, "plain source"),
        ("empty", False, "an empty file is not binary"),
        ("png", True, "a NUL early in the stream"),
        ("nul_at_last_sniffed_byte", True, "a NUL at the last sniffed byte"),
        ("nul_past_the_window", False, "a NUL PAST the sniff window"),
    ],
)
def test_is_binary_answers_both_ways_and_at_its_boundary(case, expected, why):
    """🔴 BOTH CONTROLS ON THE CLASSIFIER ITSELF, PLUS ITS BOUNDARY.

    A classifier stuck at False scans everything (noisy but safe); one stuck at
    True skips everything while the run still prints a reassuring `0 findings`.
    Only exercising both directions distinguishes them.

    The last two cases are the boundary, measured either side of it: the sniff
    window is bounded deliberately so a huge binary is not read in full, and the
    final case documents the accepted residual — a NUL beyond the window means
    the file is SCANNED, decoded with `errors="replace"`. That is the safe
    direction and it is stated here rather than left to be discovered.

    🔴 THE FIXTURES ARE BUILT IN THE BODY, NOT IN THE DECORATOR. A
    `parametrize` list that reads `leakscan.BINARY_SNIFF_BYTES` is evaluated at
    IMPORT time, so on any tree where that constant does not exist the whole
    module fails to COLLECT — and a collection error is not a test result. That
    is not hypothetical: it happened while measuring this change against its own
    merge base, and it hid the red that the regression test above exists to
    show. A test module must be importable against the code it is testing even
    when that code is the OLD version, or it cannot be used to demonstrate a
    regression at all.
    """
    window = leakscan.BINARY_SNIFF_BYTES
    data = {
        "source": b"#!/usr/bin/env python3\nprint('hi')\n",
        "empty": b"",
        "png": b"\x89PNG\r\n\x1a\n\x00\x00",
        "nul_at_last_sniffed_byte": b"x" * (window - 1) + b"\x00",
        "nul_past_the_window": b"x" * window + b"\x00",
    }[case]
    assert leakscan.is_binary(data) is expected, why


def test_the_enumeration_sees_an_UNTRACKED_file(tmp_path, monkeypatch):
    """🔴 leakscan SCANS UNTRACKED FILES DELIBERATELY, so this pins that it can.

    Its own docstring: "`git ls-files` ALONE IS BLIND to a file not yet added,
    and 'I forgot to git add it' is not a reason for a leak to ship." In a clean
    checkout every file is committed, so `--cached` and `--cached --others`
    return the SAME set and no assertion over the real tree can tell the flags
    apart. MEASURED previously: a mutant narrowing the enumeration to cached-only
    SURVIVED the whole suite. The difference only exists when an untracked file
    does, so this builds one.
    """
    repo = _repo_with(tmp_path, "tracked.md", "# tracked\n")
    (repo / "sub").mkdir()
    (repo / "sub" / "untracked.rst").write_text(_leak(), encoding="utf-8")

    seen = leakscan.enumerate_repo(repo)
    assert "sub/untracked.rst" in seen, (
        f"the enumeration missed an UNTRACKED file (saw {seen}) — it is "
        f"cached-only, so it is blind to the pre-`git add` window that "
        f"leakscan deliberately covers"
    )
    assert "tracked.md" in seen, "the enumeration missed a TRACKED file"

    monkeypatch.setattr(leakscan, "ROOT", repo)
    scanned = {str(Path(p).relative_to(repo)) for p in leakscan.tracked_files()}
    assert "sub/untracked.rst" in scanned, (
        "the untracked file is enumerated but not queued for scanning"
    )


def test_a_quoted_path_does_not_produce_a_false_diagnosis(tmp_path, monkeypatch):
    """🔴 THE `-z` HALF, AND ITS ABSENCE ONCE GAVE A CONFIDENTLY WRONG REMEDY.

    `git ls-files` QUOTES non-ASCII paths under the default `core.quotePath`.
    Without `-z` the enumeration returns `"caf\\303\\251.md"` where the file on
    disk is `café.md`, and every message naming that path names something that
    does not exist. When this module kept its own copy of the enumeration, the
    mismatch produced two failures that each blamed the wrong thing — one told
    the developer to add `.md"` (with a quote character) to the suffix set, the
    other said "the flags have diverged" when the flags were identical.

    🔴 A WRONG REMEDY IS WORSE THAN A MISSING ONE, so this pins the ENCODING.
    """
    repo = _repo_with(tmp_path, "café.md", "# accented\n")
    # 🔴 PIN THE DIMENSION THIS TEST'S PREMISE DEPENDS ON. Quoting is
    # `core.quotePath`, which defaults to true but is COMMONLY turned off in a
    # developer's global config. MEASURED: with `quotePath = false`, removing
    # `-z` left this test PASSING — the guard was silently inert on exactly the
    # hosts whose owners had customised git, and a mutation sweep run there
    # would have scored it SURVIVED. A test whose config leaves a dimension free
    # is structurally blind to that dimension's bugs, so this sets it rather
    # than inheriting it.
    subprocess.run(
        ["git", "-C", str(repo), "config", "core.quotePath", "true"], check=True
    )

    seen = leakscan.enumerate_repo(repo)
    assert "café.md" in seen, (
        f"the enumeration returned {seen!r} rather than the real filename — "
        f"`git ls-files` quoted it, which means `-z` is missing and every "
        f"diagnosis this module produces about such a file names the wrong cause"
    )

    monkeypatch.setattr(leakscan, "ROOT", repo)
    scanned = {str(Path(p).relative_to(repo)) for p in leakscan.tracked_files()}
    assert scanned == {"café.md"}, (
        f"the scan queue is {sorted(scanned)} for a one-file tree — the "
        f"enumeration and the scanner disagree on ENCODING, not on which files "
        f"exist, and the file would be read from a path that does not resolve"
    )


def test_the_scanner_reads_the_flake_when_it_walks_the_tree():
    """`flake.nix` is the file whose absence from coverage started all of this.

    Kept as a named case rather than folded into the derived checks: it is the
    one this project actually shipped unscanned, and a regression on it should
    say so by name.
    """
    scanned = {str(Path(p).relative_to(ROOT)) for p in leakscan.tracked_files()}
    assert "flake.nix" in scanned, (
        f"the scanner's own walk does not return flake.nix (returned "
        f"{len(scanned)} file(s))"
    )
