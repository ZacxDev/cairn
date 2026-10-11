"""`tests/go_tested_packages.py` driven END TO END over a two-commit repository.

🔴 THE SCRIPT'S BUILT-IN CONTROL EXERCISES ONLY THE COMPARISON, AND THAT LEFT THE READER
UNGUARDED. Measured on the first draft: a `tracked()` hard-wired to read HEAD for BOTH sides
passed the built-in control and every real tree, because "base == head" is exactly what an
honest tree looks like. Only a repository whose base and head really DIFFER can tell a
reader that looks at the base from one that does not, so this file builds one: a base with
two tested packages, and a head that hides one package's test in each way the static draft
was walked (and the plain deletion it was written for), each of which must exit 1 naming
the package. The two shapes that are NOT a narrowing — a package removed outright, a package
added — must exit 0, as must an unmodified head, so a script that refuses everything fails
here too.

It needs a Go toolchain (and cgo, for `-race`), so it SKIPS in the `tests` job; the `go` job
runs it explicitly before the real comparison and refuses on a skip.
"""

from __future__ import annotations

import os
import shutil
import subprocess
import sys
from pathlib import Path

import pytest

SCRIPT = Path(__file__).resolve().parent / "go_tested_packages.py"
MODULE = "example.invalid/fixture"

pytestmark = pytest.mark.skipif(shutil.which("go") is None, reason="needs a Go toolchain")


def _git(repo: Path, *args: str) -> str:
    return subprocess.run(
        ["git", "-c", "user.name=fixture", "-c", "user.email=fixture@example.invalid",
         "-c", "commit.gpgsign=false", *args],
        cwd=repo, check=True, capture_output=True, text=True,
    ).stdout.strip()


def _write(repo: Path, rel: str, text: str) -> None:
    p = repo / rel
    p.parent.mkdir(parents=True, exist_ok=True)
    p.write_text(text)


def _pkg(repo: Path, name: str) -> None:
    _write(repo, f"{name}/{name}.go", f"package {name}\n\nfunc F() int {{ return 1 }}\n")
    _write(repo, f"{name}/{name}_test.go",
           f'package {name}\n\nimport "testing"\n\nfunc TestF(t *testing.T) {{\n'
           f"\tif F() != 1 {{\n\t\tt.Fatal(F())\n\t}}\n}}\n")


def _tag(repo: Path, rel: str, expr: str) -> None:
    p = repo / rel
    p.write_text(f"//go:build {expr}\n\n" + p.read_text())


def _rename(repo: Path, rel: str, new: str) -> None:
    (repo / rel).rename(repo / Path(rel).parent / new)


HEADS = {
    # name: (mutation of the head tree, expected exit, package that must be named on a 1)
    "unmodified": (lambda r: None, 0, None),
    "tagged never": (lambda r: _tag(r, "alpha/alpha_test.go", "never"), 1, "alpha"),
    "tagged !race": (lambda r: _tag(r, "alpha/alpha_test.go", "!race"), 1, "alpha"),
    "renamed _x_test.go": (lambda r: _rename(r, "alpha/alpha_test.go", "_alpha_test.go"), 1, "alpha"),
    "renamed .x_test.go": (lambda r: _rename(r, "alpha/alpha_test.go", ".alpha_test.go"), 1, "alpha"),
    "test deleted": (lambda r: (r / "alpha/alpha_test.go").unlink(), 1, "alpha"),
    "package removed outright": (lambda r: shutil.rmtree(r / "alpha"), 0, None),
    "package added": (lambda r: _pkg(r, "gamma"), 0, None),
}


@pytest.fixture
def repo(tmp_path: Path) -> tuple[Path, str]:
    r = tmp_path / "repo"
    r.mkdir()
    _git(r, "init", "-q", "-b", "main")
    _write(r, "go.mod", f"module {MODULE}\n\ngo 1.21\n")
    _pkg(r, "alpha")
    _pkg(r, "beta")
    _git(r, "add", "go.mod", "alpha", "beta")
    _git(r, "commit", "-q", "-m", "base")
    return r, _git(r, "rev-parse", "HEAD")


def _run(repo: Path, *args: str) -> subprocess.CompletedProcess:
    env = {**os.environ, "GOFLAGS": "-mod=mod", "GOWORK": "off"}
    return subprocess.run([sys.executable, str(SCRIPT), *args], cwd=repo,
                          capture_output=True, text=True, env=env)


@pytest.mark.parametrize("head", sorted(HEADS))
def test_the_reader_reads_the_BASE(repo: tuple[Path, str], head: str) -> None:
    r, base = repo
    mutate, want, named = HEADS[head]
    mutate(r)
    _git(r, "add", "-A", ".")
    _git(r, "commit", "-q", "--allow-empty", "-m", head)

    # The HEAD list exactly as the CI equality step writes it.
    listed = _run(r, "--list")
    assert listed.returncode == 0, listed.stdout + listed.stderr
    head_list = r.parent / "head-list.txt"
    head_list.write_text(listed.stdout)

    proc = _run(r, "--base", base, "--head-list", str(head_list))
    out = proc.stdout + proc.stderr
    assert proc.returncode == want, f"{head}: exit {proc.returncode}, wanted {want}\n{out}"
    if named:
        assert f"  {MODULE}/{named}\n" in proc.stdout, out
    # The base worktree is removed afterwards: only the fixture's own checkout is left.
    assert _git(r, "worktree", "list").count("\n") == 0, _git(r, "worktree", "list")
