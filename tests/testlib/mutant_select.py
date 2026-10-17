"""Which mutation-battery rows a run must execute: all of them, or — on a pull request — only
the rows whose inputs the pull request changed.

Used by `tests/control_mutants.py` and `tests/routing_mutants.py` (`--scope-from-event`). Not a
test module.

🔴 A ROW'S VERDICT IS A FUNCTION OF ITS INPUTS, SO A PULL REQUEST THAT CHANGED NONE OF THEM
CANNOT CHANGE IT. A row's inputs are its TARGET file (the stale-pattern failure — "pattern occurs
0 time(s)" — is an edit to the target) and the TESTS that must kill it (a weakened killer is an
edit to a test file). Each battery says which files those are for one of its rows; this module
only intersects them with the diff.

🔴 IT FAILS SAFE — EVERY STATE IT CANNOT VOUCH FOR RUNS THE WHOLE BATTERY, NEVER FEWER ROWS:
  * any event but `pull_request` (a push to `main`, `workflow_dispatch`) — the full battery is
    what `main` is measured by;
  * a pull request whose diff cannot be computed, or is empty (`ci_paths.pull_request_paths`);
  * a change to the battery's HARNESS — its own file, the shared runner, this selector, the
    workflow that runs it, the Go module's dependency set — because those move every verdict;
  * and per row: a row whose inputs cannot be determined (the battery's mapping raised) RUNS.

⚠ WHAT IT DOES NOT SEE, STATED RATHER THAN IMPLIED: a change to a NON-target, NON-test file
that alters what a killer observes (a new guard elsewhere in the package masking the mutant).
The full battery on every push to `main` is what catches that, one commit later.
"""
from __future__ import annotations

import sys
from pathlib import Path
from typing import Callable, Iterable, Sequence, TypeVar

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

import ci_paths  # noqa: E402

T = TypeVar("T")

#: Files whose change re-scores EVERY row of either battery. An entry ending in `/` is a prefix.
SHARED_HARNESS = (
    ".github/workflows/ci.yml",
    "tests/ci_paths.py",
    "tests/testlib/mutant_tree.py",
    "tests/testlib/mutant_select.py",
    "go.mod",
    "go.sum",
)


def _matches(path: str, entries: Iterable[str]) -> bool:
    return any(path.startswith(e) if e.endswith("/") else path == e for e in entries)


def changed_paths(env: dict[str, str], git: ci_paths.Git = ci_paths._git) -> tuple[frozenset[str] | None, str]:
    """The pull request's changed paths, or `None` (run everything) and why."""
    event = env.get("EVENT", "")
    if event != "pull_request":
        return None, f"event {event!r} runs the whole battery"
    try:
        paths, why = ci_paths.pull_request_paths(env, git)
    except Exception as exc:  # noqa: BLE001 — failing safe IS the contract
        return None, f"the diff could not be computed ({type(exc).__name__}: {exc})"
    if paths is None:
        return None, why
    paths = [p.strip() for p in paths if p.strip()]
    if not paths:
        return None, "an EMPTY change list is not evidence that nothing changed"
    return frozenset(paths), f"{len(paths)} changed path(s), {why}"


def package_tests(pkg_dirs: Iterable[str]) -> Callable[[str], bool]:
    """A predicate over paths: a `*_test.go` directly in one of `pkg_dirs`, or its `testdata/`.

    `pkg_dirs` are spelled the way the batteries spell packages (`./internal/x/`, `./`).
    """
    dirs = tuple("" if d in ("./", ".") else d.removeprefix("./").rstrip("/") + "/" for d in pkg_dirs)

    def hit(path: str) -> bool:
        for d in dirs:
            if not path.startswith(d):
                continue
            rest = path[len(d):]
            if ("/" not in rest and rest.endswith("_test.go")) or rest.startswith("testdata/"):
                return True
        return False
    return hit


def select(rows: Sequence[T], changed: frozenset[str] | None, why: str, *,
           harness: Iterable[str], inputs: Callable[[T], Callable[[str], bool]],
           key: Callable[[T], str]) -> tuple[list[T], str]:
    """`(rows to run, the one line a log prints about why)`.

    `inputs(row)` returns a predicate that is true for a path the row's verdict depends on; it may
    raise, and a row whose inputs raised RUNS.
    """
    if changed is None:
        return list(rows), f"MUTANT SCOPE: FULL ({len(rows)} rows) — {why}"
    touched = sorted(p for p in changed if _matches(p, (*SHARED_HARNESS, *harness)))
    if touched:
        return list(rows), f"MUTANT SCOPE: FULL ({len(rows)} rows) — the battery's harness changed: {touched}"
    chosen: list[T] = []
    unknown: list[str] = []
    for r in rows:
        try:
            hit = inputs(r)
            run = any(hit(p) for p in changed)
        except Exception:  # noqa: BLE001 — an unmappable row runs, it is never skipped
            unknown.append(key(r))
            run = True
        if run:
            chosen.append(r)
    note = f"; {len(unknown)} with UNDETERMINED inputs run anyway: {unknown}" if unknown else ""
    return chosen, (f"MUTANT SCOPE: PR-SCOPED — {len(chosen)} of {len(rows)} rows' target or killing "
                    f"tests changed ({why}){note}: {[key(r) for r in chosen]}")
