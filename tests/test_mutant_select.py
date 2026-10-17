"""`tests/testlib/mutant_select.py`: a pull request runs only the mutation rows it can move, and
every state the selector cannot vouch for runs the whole battery."""
from __future__ import annotations

import json
import subprocess
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT / "tests"))

import routing_mutants as rm  # noqa: E402
from testlib import mutant_select as ms  # noqa: E402

PR = {"EVENT": "pull_request", "PR_BASE": "b" * 40, "PR_HEAD": "h" * 40}
ROWS = ["a", "b", "c"]
TARGET = {"a": "x/a.go", "b": "x/b.go", "c": "y/c.go"}


def _git(paths):
    def git(args):
        return "m" * 40 + "\n" if args[0] == "merge-base" else "".join(f"{p}\n" for p in paths)
    return git


def _select(paths, inputs=lambda r: (lambda p: p == TARGET[r])):
    changed, why = ms.changed_paths(PR, _git(paths))
    return ms.select(ROWS, changed, why, harness=("tests/battery.py",), inputs=inputs,
                     key=lambda r: r)


def test_a_pull_request_runs_exactly_the_rows_whose_inputs_changed():
    assert _select(["x/b.go", "README.md"])[0] == ["b"]
    rows, line = _select(["README.md", "y/other.go"])
    assert rows == [] and "0 of 3" in line


def test_every_event_but_a_pull_request_runs_everything():
    # The PR shas are present, so only the EVENT can be what decides.
    for env in ({**PR, "EVENT": "push"}, {**PR, "EVENT": "workflow_dispatch"}, {**PR, "EVENT": ""}):
        changed, why = ms.changed_paths(env, _git(["x/a.go"]))
        assert changed is None
        assert ms.select(ROWS, changed, why, harness=(), inputs=lambda r: None,
                         key=lambda r: r)[0] == ROWS


def test_an_undeterminable_diff_runs_everything():
    def broken(args):
        raise subprocess.CalledProcessError(128, args)
    for env, git in ((PR, broken), (PR, _git([])), ({"EVENT": "pull_request"}, _git(["x/a.go"]))):
        changed, _ = ms.changed_paths(env, git)
        assert changed is None


def test_a_harness_change_runs_everything():
    for harness in ("tests/battery.py", ".github/workflows/ci.yml", "tests/testlib/mutant_tree.py",
                    "go.sum"):
        rows, line = _select([harness])
        assert rows == ROWS and "FULL" in line, harness


def test_a_row_whose_inputs_cannot_be_determined_RUNS():
    def inputs(r):
        if r == "c":
            raise LookupError("cannot locate the killer")
        return lambda p: p == TARGET[r]
    rows, line = _select(["x/a.go"], inputs)
    assert rows == ["a", "c"] and "UNDETERMINED" in line


def test_package_tests_are_a_packages_own_test_files_and_testdata():
    hit = ms.package_tests(["./internal/x/", "./"])
    assert hit("internal/x/a_test.go") and hit("internal/x/testdata/f.json") and hit("b_test.go")
    assert not hit("internal/x/a.go") and not hit("internal/x/sub/a_test.go")


def test_an_aggregate_of_a_scoped_run_with_EMPTY_shards_passes(tmp_path):
    """Scoped to one row: shard 1 runs it with the control, shard 2 was dealt nothing."""
    row = next(m.id for m in rm.MUTANTS if m.id != rm.CONTROL)
    base = {"battery": "routing", "table": rm.table_digest(), "survived": [], "wrong_reason": []}
    (tmp_path / "1.json").write_text(json.dumps({**base, "shard": [1, 2],
        "selected": [rm.CONTROL, row], "killed": [rm.CONTROL, row], "positive_control": "green"}))
    (tmp_path / "2.json").write_text(json.dumps({**base, "shard": [2, 2], "selected": [],
        "killed": [], "positive_control": "not-needed"}))
    assert rm.aggregate(tmp_path, [rm.CONTROL, row]) == 0
    # …and the same files are NOT a full battery.
    assert rm.aggregate(tmp_path) == 2
