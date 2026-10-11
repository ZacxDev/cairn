#!/usr/bin/env python3
"""Refuse a change in which a Go package LOST its tests while keeping its code.

🔴 THE HALF OF THE `go` JOB'S `ok` CHECK THAT A DERIVED COUNT CANNOT SEE. That step
requires the `ok  ` lines of `go test -race ./...` to EQUAL the number of packages
`go list` reports as having test files — derived on every run, so no PR edits a number.
Deleting a package's LAST `_test.go` removes it from both sides of that equality at once,
so it stays green. The hand-written floor it replaced did catch that, and
`internal/depspolicy`'s package doc relies on it ("a package whose tests disappear takes
CI red"). This script restores that property without a literal: it compares the set of
directories holding a `_test.go` at HEAD against the same set at the base commit.

A directory that had tests at the base and has none at HEAD refuses if it still holds a
non-test `.go` file — the package is still there and nothing tests it. A package removed
outright is not a narrowing and passes.

Both sides are read by one static rule over `git ls-tree`, never by `go list`, so the
comparison needs no checkout of the base and treats a build-tag-gated test file the same
on both sides. Paths `go` itself ignores under `./...` — any component starting with `.`
or `_`, any `testdata` — and every nested module (a directory with its own `go.mod`,
today `uiaudit/`) are excluded on both sides, matching what `go test ./...` walks.

Exit codes: 0 no package lost its tests; 1 at least one did (named); 2 could not vouch —
an empty tested set on either side, or the built-in control below failing.
"""

from __future__ import annotations

import argparse
import posixpath
import subprocess
import sys


def tracked(ref: str) -> list[str]:
    out = subprocess.run(
        ["git", "ls-tree", "-r", "--name-only", ref],
        check=True,
        capture_output=True,
        text=True,
    ).stdout
    return [p for p in out.splitlines() if p]


def _nested_modules(paths: list[str]) -> set[str]:
    return {posixpath.dirname(p) for p in paths if p.endswith("/go.mod")}


def _excluded(path: str, nested: set[str]) -> bool:
    parts = path.split("/")
    if any(c.startswith((".", "_")) or c == "testdata" for c in parts[:-1]):
        return True
    return any(path.startswith(m + "/") for m in nested)


def package_dirs(paths: list[str]) -> tuple[set[str], set[str]]:
    """(directories holding a `_test.go`, directories holding a non-test `.go`)."""
    nested = _nested_modules(paths)
    tested: set[str] = set()
    code: set[str] = set()
    for p in paths:
        if not p.endswith(".go") or _excluded(p, nested):
            continue
        d = posixpath.dirname(p) or "."
        (tested if p.endswith("_test.go") else code).add(d)
    return tested, code


def narrowed(base_tested: set[str], head_tested: set[str], head_code: set[str]) -> list[str]:
    return sorted(d for d in base_tested - head_tested if d in head_code)


def _control(head_tested: set[str], head_code: set[str]) -> bool:
    """The comparison must name a package whose tests were removed. Built from the real
    HEAD sets, so it exercises `narrowed` over this tree's own shape rather than a fixture."""
    candidates = sorted(head_tested & head_code)
    if not candidates:
        return False
    victim = candidates[0]
    return narrowed(head_tested, head_tested - {victim}, head_code) == [victim] and not narrowed(
        head_tested, head_tested, head_code
    )


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    ap.add_argument("--base", required=True)
    ap.add_argument("--head", default="HEAD")
    args = ap.parse_args()

    base_tested, _ = package_dirs(tracked(args.base))
    head_tested, head_code = package_dirs(tracked(args.head))
    print(f"tested directories: base={len(base_tested)} head={len(head_tested)}")

    if not base_tested or not head_tested:
        print("COULD NOT VOUCH: an empty tested set — the reader is broken, not the tree")
        return 2
    if not _control(head_tested, head_code):
        print("COULD NOT VOUCH: the control (one package's tests removed) was not reported")
        return 2
    print("control: a removed package's tests are reported (1 of 1)")

    lost = narrowed(base_tested, head_tested, head_code)
    if lost:
        print("REFUSING: these packages had tests at the base and have none now, but still hold code:")
        for d in lost:
            print(f"  {d}")
        return 1
    print(f"no package lost its tests ({len(base_tested - head_tested)} tested directories removed outright)")
    return 0


if __name__ == "__main__":
    sys.exit(main())
