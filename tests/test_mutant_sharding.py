"""The SEAM between the authz battery's shards, and the reused-tree mechanics under both batteries.

🔴 SPLITTING A GATE IS A WAY TO LOOSEN IT WITHOUT TOUCHING A SINGLE ROW. Each shard of
`tests/control_mutants.py` enforces every refusal over the rows it was handed, so a row
handed to NO shard — or a shard whose result never arrived — leaves every shard green and the
battery unmeasured. `--aggregate` is the check over the union; these tests prove it can go
RED for each way the union can be wrong, and that it reproduces the unsharded SUMMARY when
the union is right. They run in the `tests` job, which has no Go toolchain: nothing here
runs a mutant.
"""
from __future__ import annotations

import importlib.util
import json
import subprocess
import sys
from pathlib import Path

import pytest

REPO_ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(REPO_ROOT / "tests"))

from testlib import mutant_tree  # noqa: E402


def _load():
    spec = importlib.util.spec_from_file_location(
        "cairn_control_mutants_sharding", REPO_ROOT / "tests" / "control_mutants.py")
    module = importlib.util.module_from_spec(spec)
    sys.modules[spec.name] = module
    spec.loader.exec_module(module)
    return module


cm = _load()
NAMES = [m.name for m in cm.MUTANTS]


@pytest.mark.parametrize("n", [1, 2, 7, 8, 9])
def test_every_row_lands_in_exactly_one_shard(n: int) -> None:
    shards = cm.mutant_tree.partition(cm.MUTANTS, n, key=lambda m: m.name, front=cm.SLOW_ROWS)
    assert len(shards) == n
    rows = [m.name for s in shards for m in s]
    assert sorted(rows) == sorted(NAMES) and len(rows) == len(NAMES)
    assert mutant_tree.seam_violations(NAMES, {i + 1: [m.name for m in s]
                                               for i, s in enumerate(shards)}, n) == []
    # Balanced to within one row, so no shard is a whole extra row's wall clock behind.
    assert max(map(len, shards)) - min(map(len, shards)) <= 1


def test_the_partition_is_deterministic() -> None:
    a = mutant_tree.partition(NAMES, 8, key=str, front=cm.SLOW_ROWS)
    b = mutant_tree.partition(list(NAMES), 8, key=str, front=set(cm.SLOW_ROWS))
    assert a == b


def test_front_rows_are_spread_one_per_shard() -> None:
    # Synthetic, with front rows spaced EXACTLY N apart — the shape a plain round-robin by
    # table index stacks into ONE shard. (Adjacent rows would not do: round-robin spreads those
    # on its own, and a fixture built that way survives the `front` logic being deleted —
    # measured, it did.)
    items = [f"r{i:02d}" for i in range(40)]
    front = {"r03", "r11", "r19", "r27"}
    shards = mutant_tree.partition(items, 8, key=str, front=front)
    holders = [i for i, s in enumerate(shards) for x in s if x in front]
    assert len(holders) == 4 and len(set(holders)) == 4


def test_every_slow_row_hint_names_a_real_row() -> None:
    assert cm.SLOW_ROWS <= set(NAMES), sorted(cm.SLOW_ROWS - set(NAMES))


# ── the seam guard, red for each way the union can be wrong ─────────────────────────


def _clean_split(n: int = 3) -> dict[int, list[str]]:
    shards = mutant_tree.partition(NAMES, n, key=str)
    return {i + 1: list(s) for i, s in enumerate(shards)}


def test_seam_guard_is_quiet_on_an_exact_cover() -> None:
    # The positive control for the four reds below: without it, a guard that ALWAYS
    # complained would pass all of them.
    assert mutant_tree.seam_violations(NAMES, _clean_split(), 3) == []


def test_seam_guard_refuses_a_dropped_row() -> None:
    split = _clean_split()
    gone = split[2].pop()
    problems = mutant_tree.seam_violations(NAMES, split, 3)
    assert problems == [f"row(s) run by NO shard: [{gone!r}]"]


def test_seam_guard_refuses_a_duplicated_row() -> None:
    split = _clean_split()
    twice = split[1][0]
    split[3].append(twice)
    problems = mutant_tree.seam_violations(NAMES, split, 3)
    assert problems == [f"row(s) run by more than one shard: {{{twice!r}: [1, 3]}}"]


def test_seam_guard_refuses_a_missing_shard() -> None:
    split = _clean_split()
    lost = split.pop(2)
    problems = mutant_tree.seam_violations(NAMES, split, 3)
    assert problems[0] == "no result for shard(s) [2] of 3"
    assert problems[1] == f"row(s) run by NO shard: {lost!r}"


def test_seam_guard_refuses_a_row_no_table_declares() -> None:
    split = _clean_split()
    split[1].append("a-row-from-another-branch")
    assert mutant_tree.seam_violations(NAMES, split, 3) == [
        "row(s) no table declares: ['a-row-from-another-branch']"]


# ── the aggregate, end to end over synthetic shard results ──────────────────────────


def _write_results(tmp: Path, n: int, survivors=(), mutate=None) -> None:
    """Shard results as `--results` writes them, with every non-equivalent row KILLED."""
    equivalent = {m.name for m in cm.MUTANTS if m.equivalent}
    for i, rows in enumerate(mutant_tree.partition(cm.MUTANTS, n, key=lambda m: m.name,
                                                   front=cm.SLOW_ROWS), 1):
        names = [m.name for m in rows]
        doc = {
            "battery": "control",
            "shard": [i, n],
            "table": cm.table_digest(),
            "selected": names,
            "positive_control": "green",
            "tally": {
                "killed": [x for x in names if x not in equivalent and x not in survivors],
                "survived": [x for x in names if x in equivalent or x in survivors],
                "misattributed": {},
                "broken": {},
                "stale_extras": {},
            },
        }
        if mutate:
            mutate(i, doc)
        (tmp / f"control-{i}.json").write_text(json.dumps(doc), encoding="utf-8")


def _aggregate(tmp: Path) -> subprocess.CompletedProcess:
    return subprocess.run(
        [sys.executable, str(REPO_ROOT / "tests" / "control_mutants.py"), "--aggregate", str(tmp)],
        capture_output=True, text=True)


def test_the_aggregate_of_a_clean_split_is_the_unsharded_summary(tmp_path: Path) -> None:
    _write_results(tmp_path, 8)
    proc = _aggregate(tmp_path)
    eq = sum(1 for m in cm.MUTANTS if m.equivalent)
    want = (f"SUMMARY mutants={len(NAMES)} killed={len(NAMES) - eq} survived={eq} "
            "misattributed=0 harness-errors=0 stale-extras=0")
    assert proc.returncode == 0, proc.stdout + proc.stderr
    assert want in proc.stdout.splitlines(), proc.stdout


def test_the_aggregate_refuses_a_shard_that_dropped_a_row(tmp_path: Path) -> None:
    def drop(i, doc):
        if i == 5:
            gone = doc["selected"].pop()
            for verdict in ("killed", "survived"):
                if gone in doc["tally"][verdict]:
                    doc["tally"][verdict].remove(gone)
    _write_results(tmp_path, 8, mutate=drop)
    proc = _aggregate(tmp_path)
    assert proc.returncode == 2, proc.stdout + proc.stderr
    assert "🔴 SEAM: row(s) run by NO shard:" in proc.stderr


def test_the_aggregate_refuses_a_row_run_twice(tmp_path: Path) -> None:
    def dup(i, doc):
        if i == 2:
            doc["selected"].append(NAMES[0])
    _write_results(tmp_path, 8, mutate=dup)
    proc = _aggregate(tmp_path)
    assert proc.returncode == 2, proc.stdout + proc.stderr
    assert "🔴 SEAM: row(s) run by more than one shard:" in proc.stderr


def test_the_aggregate_refuses_a_missing_shard_file(tmp_path: Path) -> None:
    _write_results(tmp_path, 8)
    (tmp_path / "control-8.json").unlink()
    proc = _aggregate(tmp_path)
    assert proc.returncode == 2
    assert "🔴 SEAM: no result for shard(s) [8] of 8" in proc.stderr


def test_the_aggregate_refuses_a_red_positive_control(tmp_path: Path) -> None:
    def red(i, doc):
        if i == 3:
            doc["positive_control"] = "red"
    _write_results(tmp_path, 8, mutate=red)
    proc = _aggregate(tmp_path)
    assert proc.returncode == 2
    assert "shard 3/8: positive control was not GREEN" in proc.stderr


def test_the_aggregate_refuses_a_shard_cut_from_another_table(tmp_path: Path) -> None:
    def other(i, doc):
        if i == 1:
            doc["table"] = "0" * 64
    _write_results(tmp_path, 8, mutate=other)
    proc = _aggregate(tmp_path)
    assert proc.returncode == 2
    assert "different MUTANTS table" in proc.stderr


def test_the_aggregate_still_refuses_an_unlabelled_survivor(tmp_path: Path) -> None:
    # The seam is clean here; the refusal must come from the SAME rule an unsharded run applies.
    victim = next(m.name for m in cm.MUTANTS if not m.equivalent)
    _write_results(tmp_path, 8, survivors={victim})
    proc = _aggregate(tmp_path)
    assert proc.returncode == 1, proc.stdout + proc.stderr
    assert f"SURVIVED WITHOUT AN EQUIVALENT LABEL: [{victim!r}]" in proc.stderr


# ── scoping a row to its named tests ─────────────────────────────────────────────────


def test_every_named_killer_is_located_in_a_test_file() -> None:
    """An INVARIANT guard over the table, not regression coverage: a row whose killer
    cannot be found is refused at run time as a harness error; this says so before CI does."""
    index = mutant_tree.go_test_index(REPO_ROOT)
    for m in cm.MUTANTS:
        pkgs, run = cm.scope_for(m, index)
        assert pkgs, m.name
        if m.equivalent or not m.killer:
            assert pkgs == tuple(m.pkgs or cm.PKGS) and run is None, m.name
        else:
            assert m.killer in run, m.name


def test_an_unlocatable_killer_is_refused_not_narrowed_to_nothing() -> None:
    row = cm.Mutant(name="synthetic", path="internal/control/resolve.go", old="x", new="y",
                    killer="TestThatDoesNotExistAnywhere", why="synthetic")
    with pytest.raises(cm.MutationError, match="cannot locate"):
        cm.scope_for(row, mutant_tree.go_test_index(REPO_ROOT))


def test_the_run_pattern_is_anchored() -> None:
    assert mutant_tree.run_pattern(["TestB", "TestA", "TestA"]) == "^(TestA|TestB)$"
    with pytest.raises(ValueError):
        mutant_tree.run_pattern(["TestA/sub"])


# ── the reused tree: restored, and refused when it cannot be ────────────────────────


def _refuse(found: int) -> AssertionError:
    return AssertionError(f"count {found}")


def test_a_mutation_is_undone_byte_for_byte(tmp_path: Path) -> None:
    f = tmp_path / "a.go"
    original = b"package a\n\nfunc F() int { return 1 }\n"
    f.write_bytes(original)
    with mutant_tree.mutated(f, "return 1", "return 2", 1, _refuse):
        assert b"return 2" in f.read_bytes()
    assert f.read_bytes() == original


def test_a_failed_restore_is_refused(tmp_path: Path, monkeypatch) -> None:
    f = tmp_path / "a.go"
    f.write_bytes(b"return 1\n")
    real = Path.write_bytes
    monkeypatch.setattr(Path, "write_bytes", lambda self, data: real(self, b"corrupt\n"))
    with pytest.raises(mutant_tree.RestoreError):
        with mutant_tree.mutated(f, "return 1", "return 2", 1, _refuse):
            pass


def test_a_wrong_occurrence_count_writes_nothing(tmp_path: Path) -> None:
    f = tmp_path / "a.go"
    f.write_bytes(b"x x\n")
    with pytest.raises(AssertionError, match="count 2"):
        with mutant_tree.mutated(f, "x", "y", 1, _refuse):
            pass
    assert f.read_bytes() == b"x x\n"


def test_the_tree_digest_sees_an_edit_and_ignores_caches(tmp_path: Path) -> None:
    (tmp_path / "p").mkdir()
    (tmp_path / "p" / "a.go").write_text("package p\n")
    before = mutant_tree.tree_digest(tmp_path)
    (tmp_path / "__pycache__").mkdir()
    (tmp_path / "__pycache__" / "x.pyc").write_bytes(b"\0")
    assert mutant_tree.tree_digest(tmp_path) == before
    (tmp_path / "p" / "a.go").write_text("package q\n")
    assert mutant_tree.tree_digest(tmp_path) != before


def test_a_copy_never_carries_the_git_link(tmp_path: Path) -> None:
    src = tmp_path / "src"
    src.mkdir()
    (src / ".git").write_text("gitdir: /elsewhere\n")
    (src / "f").write_text("x")
    mutant_tree.copy_module(src, tmp_path / "dst")
    assert not (tmp_path / "dst" / ".git").exists()
    assert (tmp_path / "dst" / "f").read_text() == "x"


def test_the_aggregate_refuses_a_row_with_no_verdict(tmp_path: Path) -> None:
    # A shard that stopped early still writes its results; its unfinished rows must not just
    # vanish from every count.
    def unfinished(i, doc):
        if i == 6:
            gone = doc["tally"]["killed"].pop()
            assert gone in doc["selected"]
    _write_results(tmp_path, 8, mutate=unfinished)
    proc = _aggregate(tmp_path)
    assert proc.returncode == 1, proc.stdout + proc.stderr
    assert "🔴 NO VERDICT for" in proc.stderr


# ── the ROUTING battery: the same seam, with its positive control in EVERY shard ─────


def _load_routing():
    spec = importlib.util.spec_from_file_location(
        "cairn_routing_mutants_sharding", REPO_ROOT / "tests" / "routing_mutants.py")
    module = importlib.util.module_from_spec(spec)
    sys.modules[spec.name] = module
    spec.loader.exec_module(module)
    return module


rm = _load_routing()
RIDS = [m.id for m in rm.MUTANTS]


def _routing_shards(n: int):
    return [rm.mutant_tree.shard_rows(rm.MUTANTS, (i, n), key=lambda m: m.id,
                                      front=rm.slow_rows(), always={rm.CONTROL})
            for i in range(1, n + 1)]


@pytest.mark.parametrize("n", [1, 4, 8])
def test_every_routing_row_lands_once_and_the_control_lands_everywhere(n: int) -> None:
    shards = _routing_shards(n)
    assert all(s[0].id == rm.CONTROL for s in shards)
    rest = [m.id for s in shards for m in s[1:]]
    assert sorted(rest) == sorted(r for r in RIDS if r != rm.CONTROL)
    assert len(rest) == len(set(rest))
    # The pytest half is ~95% of the wall clock, so it is what must be balanced.
    py = [sum(1 for m in s if not m.go_package) for s in shards]
    assert max(py) - min(py) <= 1


def _write_routing(tmp: Path, n: int, mutate=None) -> None:
    for i, rows in enumerate(_routing_shards(n), 1):
        ids = [m.id for m in rows]
        doc = {"battery": "routing", "shard": [i, n], "table": rm.table_digest(),
               "selected": ids, "positive_control": "green",
               "killed": list(ids), "survived": [], "wrong_reason": []}
        if mutate:
            mutate(i, doc)
        (tmp / f"routing-{i}.json").write_text(json.dumps(doc), encoding="utf-8")


def _aggregate_routing(tmp: Path) -> subprocess.CompletedProcess:
    return subprocess.run(
        [sys.executable, str(REPO_ROOT / "tests" / "routing_mutants.py"), "--aggregate", str(tmp)],
        capture_output=True, text=True)


def test_the_routing_aggregate_of_a_clean_split_is_the_unsharded_summary(tmp_path: Path) -> None:
    _write_routing(tmp_path, 8)
    proc = _aggregate_routing(tmp_path)
    assert proc.returncode == 0, proc.stdout + proc.stderr
    # The control ran in all eight shards and is counted ONCE.
    assert (f"SUMMARY mutants={len(RIDS)} killed={len(RIDS)} survived=0 "
            "killed-by-the-wrong-test=0") in proc.stdout.splitlines(), proc.stdout


def test_the_routing_aggregate_refuses_a_dropped_row(tmp_path: Path) -> None:
    def drop(i, doc):
        if i == 3:
            gone = doc["selected"].pop()
            doc["killed"].remove(gone)
    _write_routing(tmp_path, 8, mutate=drop)
    proc = _aggregate_routing(tmp_path)
    assert proc.returncode == 2, proc.stdout + proc.stderr
    assert "🔴 SEAM: row(s) run by NO shard:" in proc.stderr
    # The SEAM refusal returns before any SUMMARY; without this line the no-verdict refusal
    # (also exit 2) kept this test green with the seam check deleted — measured.
    assert "SUMMARY" not in proc.stdout, proc.stdout


def test_the_routing_aggregate_refuses_a_duplicated_row(tmp_path: Path) -> None:
    other = _routing_shards(8)[0][1].id
    def dup(i, doc):
        if i == 5:
            doc["selected"].append(other)
            doc["killed"].append(other)
    _write_routing(tmp_path, 8, mutate=dup)
    proc = _aggregate_routing(tmp_path)
    assert proc.returncode == 2, proc.stdout + proc.stderr
    assert "🔴 SEAM: row(s) run by more than one shard:" in proc.stderr
    assert "SUMMARY" not in proc.stdout, proc.stdout


def test_the_routing_aggregate_refuses_a_shard_without_its_control(tmp_path: Path) -> None:
    def strip(i, doc):
        if i == 2:
            doc["selected"].remove(rm.CONTROL)
            doc["killed"].remove(rm.CONTROL)
    _write_routing(tmp_path, 8, mutate=strip)
    proc = _aggregate_routing(tmp_path)
    assert proc.returncode == 2, proc.stdout + proc.stderr
    assert f"shard 2/8: does not carry ['{rm.CONTROL}'], which every shard must" in proc.stderr


def test_the_routing_aggregate_refuses_a_red_control(tmp_path: Path) -> None:
    def red(i, doc):
        if i == 7:
            doc["positive_control"] = "red"
    _write_routing(tmp_path, 8, mutate=red)
    proc = _aggregate_routing(tmp_path)
    assert proc.returncode == 2
    assert "shard 7/8: positive control was not GREEN" in proc.stderr


def test_the_routing_aggregate_refuses_a_row_with_no_verdict(tmp_path: Path) -> None:
    def unfinished(i, doc):
        if i == 4:
            doc["killed"].pop()
    _write_routing(tmp_path, 8, mutate=unfinished)
    proc = _aggregate_routing(tmp_path)
    assert proc.returncode == 2, proc.stdout + proc.stderr
    assert "REFUSING TO VOUCH: no verdict for" in proc.stderr


def test_the_routing_aggregate_still_refuses_a_survivor(tmp_path: Path) -> None:
    victim = _routing_shards(8)[0][1].id
    def survive(i, doc):
        if victim in doc["killed"]:
            doc["killed"].remove(victim)
            doc["survived"].append(victim)
    _write_routing(tmp_path, 8, mutate=survive)
    proc = _aggregate_routing(tmp_path)
    assert proc.returncode == 1, proc.stdout + proc.stderr
    assert f"surviving: {victim}" in proc.stdout
