"""An UNEXPECTED exception in a mutant worker must end the run, not hang it.

🔴 MEASURED HANG. `tests/control_mutants.py`'s worker handed its tree back to the pool only on a
`MutationError` or a normal finish. Any other exception — a `UnicodeDecodeError` out of
`subprocess.run(text=True)` on non-UTF-8 `go test -v` output, an `OSError` — left the tree out
of the pool and the abort flag unset, so every later task spun on `free.get(timeout=1)` forever
and the executor's shutdown waited on them: the run never exited, and a CI job would sit until
its own timeout with nothing in the log saying why. The serial loop this pool replaced let the
exception propagate and exited non-zero with the traceback; this pins that behaviour back.

The battery's Go-facing pieces are stubbed, so this runs in the `tests` job, which has no Go
toolchain: the positive control is reported green, every row's edit is a no-op, and the FIRST
row's test run raises. A hang is caught by the subprocess timeout and fails the test.
"""
from __future__ import annotations

import subprocess
import sys
import textwrap
from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parents[1]

DRIVER = textwrap.dedent(
    """
    import contextlib, importlib.util, sys
    spec = importlib.util.spec_from_file_location("cm_pool", sys.argv[1])
    cm = importlib.util.module_from_spec(spec)
    sys.modules["cm_pool"] = cm
    spec.loader.exec_module(cm)

    calls = []
    def run_tests(tree, pkgs=(), run=None):
        calls.append(pkgs)
        if len(calls) == 1:                      # the positive control
            return True, set(), "--- PASS: TestX"
        raise UnicodeDecodeError("utf-8", b"\\xff", 0, 1, "INJECTED by the pool regression test")

    cm.go_available = lambda: True
    cm.prepare_tree = lambda dest: dest.mkdir(parents=True)
    cm.apply_mutation = lambda tree, m: contextlib.nullcontext()
    cm.run_tests = run_tests
    sys.argv = ["control_mutants.py", "--shard", "1/120", "--jobs", sys.argv[2]]
    sys.exit(cm.main())
    """
)


def _run(jobs: str) -> subprocess.CompletedProcess:
    return subprocess.run(
        [sys.executable, "-c", DRIVER, str(REPO_ROOT / "tests" / "control_mutants.py"), jobs],
        capture_output=True, text=True, timeout=60,
    )


def test_a_worker_exception_exits_non_zero_promptly_at_one_job() -> None:
    # One worker tree: the raising row is the one that held it, so before the fix every later
    # row waited for a tree that never came back.
    proc = _run("1")
    assert proc.returncode != 0, proc.stdout + proc.stderr
    assert "INJECTED by the pool regression test" in proc.stderr, proc.stderr


def test_a_worker_exception_exits_non_zero_promptly_at_two_jobs() -> None:
    proc = _run("2")
    assert proc.returncode != 0, proc.stdout + proc.stderr
    assert "INJECTED by the pool regression test" in proc.stderr, proc.stderr
