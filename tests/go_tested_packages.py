#!/usr/bin/env python3
"""Refuse a change in which a Go package LOST its tests while keeping its code.

🔴 THE HALF OF THE `go` JOB'S `ok` CHECK THAT A DERIVED COUNT CANNOT SEE. That step
requires the `ok  ` lines of `go test -race ./...` to EQUAL the number of packages this
script's `--list` reports as having test files — derived on every run, so no PR edits a
number. Deleting a package's LAST `_test.go` removes it from both sides of that equality at
once, so it stays green. The hand-written floor it replaced did catch that, and
`internal/depspolicy`'s package doc relies on it ("a package whose tests disappear takes
CI red"). This script restores that property without a literal: it compares the set of
TESTED PACKAGES at HEAD against the same set at the base commit.

🔴 BOTH SIDES ARE READ BY THE SAME INSTRUMENT AS THE RUN — `go list -race` OVER `./...` —
AND A STATIC READER WAS TRIED FIRST AND WALKED FOUR WAYS. The first draft read both sides
with one rule over `git ls-tree` ("a directory holding a `_test.go`"). Its audit showed a
package's only test file could leave the run while still LOOKING like a test to that rule:
`//go:build never`, `//go:build !race` (the run passes `-race`, so the file is excluded
from exactly the build that counts), or a rename to `_x_test.go` / `.x_test.go`, which `go`
ignores. Each moved the derived count and the `ok` count down together, AND kept the
directory in the static set at HEAD, so both gates stayed green where the old floor went
red. So the base is now checked out (a detached worktree, removed afterwards) and asked the
same question HEAD is asked, with the same flags, by the same function: `go list` decides
what is a test file, so build tags, `-race` and ignored names count identically on both.

🔴 THE PATTERN IS PINNED HERE, NOT TAKEN FROM THE CALLER. Narrowing BOTH `./...` patterns
in the CI step to one subtree keeps the equality green. The base side here always walks
`./...` — `PATTERN` below, which `main` refuses to run with if it is anything else — so a
narrowed HEAD list reads as every package outside the subtree LOSING its tests.

A package present at the base and absent from the HEAD list refuses unless its directory
holds NO `.go` file of any kind at HEAD — removed outright, which is not a narrowing. Note
the asymmetry, deliberately conservative: a directory whose only remaining `.go` files are
ones `go` ignores (`_x.go`, tagged-out code) still counts as having code, because that is
the shape a test file hidden by renaming leaves behind.

Exit codes: 0 no package lost its tests; 1 at least one did (named); 2 could not vouch —
an empty tested set on either side, a module path that differs between the two sides, a
`go list` or `git worktree` failure, or the built-in control below failing.
"""

from __future__ import annotations

import argparse
import os
import shutil
import subprocess
import sys
import tempfile
from pathlib import Path

# The ONE spelling of the question. The CI equality step gets its HEAD list from `--list`,
# so the run's expected count and both sides of the base comparison are this template.
PATTERN = "./..."
TEMPLATE = "{{if or .TestGoFiles .XTestGoFiles}}{{.ImportPath}}{{end}}"
FLAGS = ("-race",)


class CouldNotVouch(Exception):
    pass


def _run(cmd: list[str], cwd: str | os.PathLike) -> str:
    proc = subprocess.run(cmd, cwd=cwd, capture_output=True, text=True)
    if proc.returncode != 0:
        raise CouldNotVouch(f"`{' '.join(cmd)}` in {cwd} exited {proc.returncode}:\n{proc.stderr}")
    return proc.stdout


def tested_packages(root: str | os.PathLike) -> list[str]:
    """Import paths of every package under `root` that has a test file FOR THIS BUILD."""
    out = _run(["go", "list", *FLAGS, "-f", TEMPLATE, PATTERN], root)
    return sorted({ln.strip() for ln in out.splitlines() if ln.strip()})


def module_path(root: str | os.PathLike) -> str:
    return _run(["go", "list", "-m"], root).strip()


def at_base(base: str, repo: str | os.PathLike) -> tuple[list[str], str]:
    """(tested packages, module path) of `base`, read from a detached worktree of it."""
    tmp = tempfile.mkdtemp(prefix="go-tested-base-")
    wt = os.path.join(tmp, "base")
    try:
        _run(["git", "worktree", "add", "--detach", "--quiet", wt, base], repo)
        try:
            return tested_packages(wt), module_path(wt)
        finally:
            subprocess.run(["git", "worktree", "remove", "--force", wt], cwd=repo,
                           capture_output=True)
    finally:
        shutil.rmtree(tmp, ignore_errors=True)


def has_go_files(root: Path, module: str, import_path: str) -> bool:
    """Whether the package's directory at `root` still holds ANY `.go` file."""
    if import_path == module:
        rel = ""
    elif import_path.startswith(module + "/"):
        rel = import_path[len(module) + 1:]
    else:
        raise CouldNotVouch(f"{import_path} is not under module {module}")
    d = root / rel
    return d.is_dir() and any(p.suffix == ".go" and p.is_file() for p in d.iterdir())


def narrowed(base_tested: list[str], head_tested: list[str], still_has_code) -> list[str]:
    head = set(head_tested)
    return sorted(p for p in base_tested if p not in head and still_has_code(p))


def _control(head_tested: list[str]) -> bool:
    """`narrowed` must name a package whose tests were removed, and nothing when none were.
    ⚠ A check of the COMPARISON only; the READER is controlled by
    `tests/test_go_tested_packages.py`, which drives this file end to end over a two-commit
    repository and is run in the `go` job before the real comparison."""
    if not head_tested:
        return False
    victim = head_tested[0]
    return narrowed(head_tested, head_tested[1:], lambda _: True) == [victim] and not narrowed(
        head_tested, head_tested, lambda _: True
    )


def main(argv: list[str] | None = None) -> int:
    ap = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    mode = ap.add_mutually_exclusive_group(required=True)
    mode.add_argument("--list", action="store_true",
                      help="print the tested packages of the tree in the working directory")
    mode.add_argument("--base", help="the commit to compare against")
    ap.add_argument("--head-list", help="the HEAD list `--list` wrote (default: derive it now)")
    args = ap.parse_args(argv)

    if PATTERN != "./...":
        print(f"COULD NOT VOUCH: PATTERN is {PATTERN!r}, not './...' — a narrowed walk on both sides")
        return 2
    root = Path.cwd()
    try:
        if args.list:
            pkgs = tested_packages(root)
            if not pkgs:
                print("COULD NOT VOUCH: go list derived 0 tested packages", file=sys.stderr)
                return 2
            print("\n".join(pkgs))
            return 0

        if args.head_list:
            head_tested = sorted({ln.strip() for ln in
                                  Path(args.head_list).read_text().splitlines() if ln.strip()})
        else:
            head_tested = tested_packages(root)
        head_module = module_path(root)
        base_tested, base_module = at_base(args.base, root)
    except CouldNotVouch as e:
        print(f"COULD NOT VOUCH: {e}")
        return 2

    print(f"tested packages: base={len(base_tested)} head={len(head_tested)}")
    if not base_tested or not head_tested:
        print("COULD NOT VOUCH: an empty tested set — the reader is broken, not the tree")
        return 2
    if base_module != head_module:
        print(f"COULD NOT VOUCH: module path moved ({base_module} -> {head_module}); "
              "import paths cannot be compared across it")
        return 2
    if not _control(head_tested):
        print("COULD NOT VOUCH: the control (one package's tests removed) was not reported")
        return 2
    print("control: a removed package's tests are reported (1 of 1)")

    try:
        lost = narrowed(base_tested, head_tested,
                        lambda p: has_go_files(root, head_module, p))
    except CouldNotVouch as e:
        print(f"COULD NOT VOUCH: {e}")
        return 2
    if lost:
        print("REFUSING: these packages had tests at the base and have none in this run, "
              "but their directories still hold .go files:")
        for p in lost:
            print(f"  {p}")
        return 1
    gone = len(set(base_tested) - set(head_tested))
    print(f"no package lost its tests ({gone} tested package(s) removed outright)")
    return 0


if __name__ == "__main__":
    sys.exit(main())
