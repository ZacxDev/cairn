"""The mechanics two mutation batteries share: ONE reused tree per worker, a mutation that
is applied and then PROVABLY undone, a deterministic shard partition, and the Go test
package a named test function lives in.

Used by `tests/control_mutants.py` and `tests/routing_mutants.py`. Not a test module.

🔴 WHY A REUSED TREE AND NOT A FRESH COPY PER MUTANT. Go's build cache keys a package's
compile on its DIRECTORY as well as its contents (the build is not `-trimpath`), so a copy
into a NEW directory is a cold build of the whole module every time — measured on a loaded
24-core host over three packages: a fresh directory 12.4-14.7 s, a cold cache 13.6 s, the same
directory warm 3.4 s. A battery of hundreds of mutants was paying the cold figure per row.
Mutating ONE tree in place and restoring it means only the mutated package and its
dependents recompile.

🔴 AND WHY THE RESTORE IS VERIFIED RATHER THAN TRUSTED. In a reused tree, a restore that
silently failed leaves mutant N's edit in place while mutant N+1 runs: every later verdict
is then a claim about TWO edits, and a positive-looking KILLED can be the previous row's
defect. So the original bytes are hashed before the edit, rewritten after the run, and
re-hashed; a mismatch raises `RestoreError` and the battery stops rather than continuing on
a tree it can no longer describe. A whole-tree digest (`tree_digest`) is the coarser check
around a whole run, for writers this module did not make — a test that writes into its own
package directory, say.
"""
from __future__ import annotations

import hashlib
import json
import os
import re
import shutil
import sys
from contextlib import contextmanager
from pathlib import Path
from typing import Callable, Iterable, Iterator, Sequence, TypeVar

T = TypeVar("T")

#: What a copy of the module leaves out. `.git` first, and for a reason beyond size: a
#: linked worktree's `.git` is a FILE holding `gitdir: …`, so a copy carrying it shares the
#: ORIGINAL's index and refs and a stray command inside the copy lands on the real branch.
#: Caches are left out because a stale `.pyc` can stand in for a mutated source.
COPY_IGNORE = (".git", "__pycache__", "*.pyc", ".pytest_cache", ".direnv", "result")

#: Never part of a tree's identity: interpreter caches a run is allowed to create.
DIGEST_SKIP_DIRS = frozenset({"__pycache__", ".pytest_cache"})


class RestoreError(AssertionError):
    """A mutated file could not be put back byte-for-byte. Every later verdict is void."""


def copy_module(src: Path, dest: Path) -> None:
    """Copy `src` to `dest` without `.git`, and prove the `.git` link did not come along."""
    shutil.copytree(src, dest, ignore=shutil.ignore_patterns(*COPY_IGNORE), symlinks=True)
    stray = dest / ".git"
    if stray.exists() or stray.is_symlink():
        raise AssertionError(f"{dest}: the copy carries a .git — refusing to mutate it")


def tree_digest(root: Path) -> str:
    """One sha256 over every regular file's relative path and bytes, and every symlink's
    target, in sorted order. Interpreter caches are excluded (see `DIGEST_SKIP_DIRS`)."""
    h = hashlib.sha256()
    for dirpath, dirnames, filenames in os.walk(root):
        dirnames[:] = sorted(d for d in dirnames if d not in DIGEST_SKIP_DIRS)
        for name in sorted(filenames):
            if name.endswith(".pyc"):
                continue
            path = Path(dirpath) / name
            rel = path.relative_to(root).as_posix()
            h.update(rel.encode() + b"\0")
            if path.is_symlink():
                h.update(b"L" + os.readlink(path).encode() + b"\0")
            else:
                h.update(hashlib.sha256(path.read_bytes()).digest())
    return h.hexdigest()


@contextmanager
def mutated(path: Path, old: str, new: str, occurrences: int,
            on_count: Callable[[int], BaseException]) -> Iterator[None]:
    """Replace `old` with `new` in `path` for the duration of the block, then restore.

    The occurrence count is checked BEFORE anything is written; `on_count(found)` builds the
    battery's own refusal, so each battery keeps its own wording and exception type.

    ⚠ The edit goes through `read_text`/`write_text` exactly as the per-copy code it replaces
    did, so a mutant's bytes are what they always were; only the RESTORE is byte-level.
    """
    original = path.read_bytes()
    digest = hashlib.sha256(original).hexdigest()
    text = path.read_text(encoding="utf-8")
    found = text.count(old)
    if found != occurrences:
        raise on_count(found)
    path.write_text(text.replace(old, new), encoding="utf-8")
    try:
        yield
    finally:
        path.write_bytes(original)
        after = hashlib.sha256(path.read_bytes()).hexdigest()
        if after != digest:
            raise RestoreError(
                f"{path}: restored bytes hash {after}, the original was {digest}. The tree no "
                "longer matches what it was copied from, so no later verdict is about one edit."
            )


# ── sharding ──────────────────────────────────────────────────────────────────────────


def parse_shard(spec: str) -> tuple[int, int]:
    """`"i/N"` → `(i, N)`, one-based, refusing anything that is not 1 <= i <= N."""
    m = re.fullmatch(r"(\d+)/(\d+)", spec or "")
    if not m:
        raise ValueError(f"--shard wants i/N (e.g. 3/8), got {spec!r}")
    i, n = int(m.group(1)), int(m.group(2))
    if not 1 <= i <= n:
        raise ValueError(f"--shard {spec}: need 1 <= i <= N")
    return i, n


def partition(items: Sequence[T], n: int, key: Callable[[T], str],
              front: Iterable[str] = ()) -> list[list[T]]:
    """Split `items` into `n` shards, deterministically, every item in exactly one.

    Items whose key is in `front` are dealt FIRST, round-robin, so up to `n` of them land in
    `n` DIFFERENT shards — that is how the expensive rows are spread rather than left to fall
    wherever their table position puts them. The rest follow in table order, round-robin.
    """
    if n < 1:
        raise ValueError("need at least one shard")
    front = set(front)
    ordered = [x for x in items if key(x) in front] + [x for x in items if key(x) not in front]
    shards: list[list[T]] = [[] for _ in range(n)]
    for k, x in enumerate(ordered):
        shards[k % n].append(x)
    return shards


def seam_violations(expected: Sequence[str], shard_rows: dict[int, Sequence[str]],
                    n: int) -> list[str]:
    """Why a set of shard results does NOT cover `expected` exactly once — `[]` when it does.

    🔴 THE SEAM NOBODY OWNS. Each shard enforces its own refusals over the rows it was
    handed, and a shard that was handed NOTHING — or a row that fell between two shards, or
    landed in both — leaves every shard green. Only a check over the UNION can see that, so
    it is a relationship pinned in both directions: a row missing is a failure, a row twice
    is a failure, a row nobody declared is a failure, and so is a shard index absent or
    out of range.
    """
    problems: list[str] = []
    missing_shards = sorted(set(range(1, n + 1)) - set(shard_rows))
    extra_shards = sorted(set(shard_rows) - set(range(1, n + 1)))
    if missing_shards:
        problems.append(f"no result for shard(s) {missing_shards} of {n}")
    if extra_shards:
        problems.append(f"result(s) for shard(s) {extra_shards}, outside 1..{n}")
    seen: dict[str, list[int]] = {}
    for i, rows in shard_rows.items():
        for r in rows:
            seen.setdefault(r, []).append(i)
    dup = {r: s for r, s in seen.items() if len(s) > 1}
    if dup:
        problems.append(f"row(s) run by more than one shard: {dict(sorted(dup.items()))}")
    want = set(expected)
    dropped = [r for r in expected if r not in seen]
    if dropped:
        problems.append(f"row(s) run by NO shard: {dropped}")
    unknown = sorted(set(seen) - want)
    if unknown:
        problems.append(f"row(s) no table declares: {unknown}")
    return problems


def shard_rows(rows: Sequence[T], spec: tuple[int, int], key: Callable[[T], str],
               front: Iterable[str] = (), always: Iterable[str] = ()) -> list[T]:
    """Shard `spec` of `rows`: the `always` rows first — in EVERY shard — then this shard's part.

    `always` is for a battery whose positive control is a ROW: every shard must carry its own,
    or every shard but one would have nothing proving its runner executes the tree it edits.
    """
    always = set(always)
    keep = [r for r in rows if key(r) in always]
    rest = [r for r in rows if key(r) not in always]
    return keep + partition(rest, spec[1], key=key, front=front)[spec[0] - 1]


def load_shard_results(results_dir: Path, battery: str, table: str, expected: Sequence[str],
                       always: Iterable[str] = ()) -> tuple[list[dict], list[str]]:
    """Read every `battery` shard result in `results_dir`; return them and every SEAM problem.

    🔴 ONE LOADER FOR BOTH BATTERIES, so the union check cannot be stricter for one than the
    other. A problem is: no results at all; shards disagreeing on N; a shard cut from another
    table (`table` is the caller's digest of its own rows); two results for one shard index;
    a shard whose positive control was not green; an `always` row missing from any shard; and
    everything `seam_violations` refuses over the remaining rows.
    """
    docs = [json.loads(f.read_text(encoding="utf-8")) for f in sorted(results_dir.glob("*.json"))]
    docs = [d for d in docs if d.get("battery") == battery]
    always = set(always)
    problems: list[str] = []
    if not docs:
        problems.append(f"no {battery}-battery shard results in {results_dir}")
    counts = {d["shard"][1] for d in docs}
    if len(counts) > 1:
        problems.append(f"shard results disagree on the shard COUNT: {sorted(counts)}")
    if {d["table"] for d in docs} - {table}:
        problems.append("a shard was cut from a different MUTANTS table than this one")
    by_shard: dict[int, list[str]] = {}
    for d in docs:
        i, n_of = d["shard"]
        rows = [r for r in d["selected"] if r not in always]
        lost = sorted(always - set(d["selected"]))
        if lost:
            problems.append(f"shard {i}/{n_of}: does not carry {lost}, which every shard must")
        if i in by_shard:
            problems.append(f"two results claim shard {i}")
            by_shard[i] = by_shard[i] + rows
        else:
            by_shard[i] = rows
        if d.get("positive_control") != "green":
            problems.append(f"shard {i}/{n_of}: positive control was not GREEN")
    n = max(counts) if counts else 0
    problems += seam_violations([r for r in expected if r not in always], by_shard, n)
    print(f"aggregate: {len(docs)} shard result(s) from {results_dir}, shard count {n}")
    for p in problems:
        print(f"🔴 SEAM: {p}", file=sys.stderr)
    if problems:
        print(f"{battery}: REFUSING TO VOUCH — the shards do not cover the battery exactly once, "
              "so no per-shard green adds up to a battery green.", file=sys.stderr)
    return sorted(docs, key=lambda d: d["shard"][0]), problems


# ── locating a Go test function ──────────────────────────────────────────────────────


def go_test_index(root: Path) -> dict[str, set[str]]:
    """Map every top-level `func TestX(` in `root`'s module to the package dir(s) holding it.

    Walks the tree the way `go` does for `./...`: directories starting with `.` or `_`, and
    `testdata`, are not packages, and a subdirectory with its own `go.mod` is another module.
    Package dirs are returned as `./rel/path/`, the spelling the batteries already use.
    """
    pattern = re.compile(r"^func (Test\w*)\(", re.MULTILINE)
    index: dict[str, set[str]] = {}
    for dirpath, dirnames, filenames in os.walk(root):
        here = Path(dirpath)
        dirnames[:] = sorted(
            d for d in dirnames
            if not d.startswith((".", "_")) and d != "testdata"
            and not (here / d / "go.mod").exists()
        )
        tests = [f for f in filenames if f.endswith("_test.go")]
        if not tests:
            continue
        rel = here.relative_to(root).as_posix()
        pkg = "./" if rel == "." else f"./{rel}/"
        for f in tests:
            for name in pattern.findall((here / f).read_text(encoding="utf-8")):
                index.setdefault(name, set()).add(pkg)
    return index


def run_pattern(names: Iterable[str]) -> str:
    """An anchored `go test -run` expression matching exactly these top-level tests."""
    names = sorted(set(names))
    for n in names:
        if not re.fullmatch(r"Test\w*", n):
            raise ValueError(f"{n!r} is not a top-level Go test name")
    return "^(" + "|".join(names) + ")$"
