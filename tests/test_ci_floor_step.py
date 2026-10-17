"""The `tests` job's collected-test floor, RUN rather than read.

`.github/workflows/ci.yml`'s step "run the suite with a collected-test floor" ends in a
Python heredoc that reads pytest's junit file and decides the job's exit code. Nothing
local ran it, so a change to it was only ever seen in CI. This extracts that heredoc
verbatim, points it at a synthetic junit file, and checks its verdict on both sides of
the floor:

  * a suite ABOVE the floor by any amount passes. The upper-gap refusal
    (`ran - FLOOR > 100`) that used to fail a fully green run for having ADDED tests
    was removed; `test_a_suite_far_ABOVE_the_floor_passes` is red with it present.
  * a suite BELOW the floor is refused with the narrowing message — the half kept.
  * a failing test still fails the step at the floor exactly.
"""
import re
import subprocess
import sys
import textwrap
from pathlib import Path

import pytest

CI = Path(__file__).resolve().parent.parent / ".github" / "workflows" / "ci.yml"
_STEP = "- name: run the suite with a collected-test floor"
_JUNIT = 'ET.parse("/tmp/r.xml")'


def _floor_script() -> str:
    text = CI.read_text(encoding="utf-8")
    step = text[text.index(_STEP):]
    body = re.search(r"python3 - <<'PY'\n(.*?)\n\s*PY\n", step, re.S)
    assert body, "the floor heredoc was not found in its step — this parse is wrong"
    script = textwrap.dedent(body.group(1))
    assert script.count(_JUNIT) == 1, f"expected the heredoc to call {_JUNIT} exactly once"
    return script


def _floor() -> int:
    m = re.search(r"^\s*FLOOR = (\d+)\s*$", _floor_script(), re.M)
    assert m, "no `FLOOR = <n>` in the heredoc"
    return int(m.group(1))


def _run(tmp_path: Path, tests: int, failures: int = 0) -> subprocess.CompletedProcess:
    junit = tmp_path / "r.xml"
    junit.write_text(
        f'<testsuites><testsuite name="pytest" tests="{tests}" failures="{failures}" '
        f'errors="0"/></testsuites>',
        encoding="utf-8",
    )
    script = _floor_script().replace(_JUNIT, f"ET.parse({str(junit)!r})")
    return subprocess.run([sys.executable, "-c", script], capture_output=True, text=True)


def test_the_heredoc_reads_the_synthetic_junit(tmp_path):
    """POSITIVE CONTROL: the script really reads the file this test writes, so the
    verdicts below are about the count, not about a script that never ran."""
    floor = _floor()
    out = _run(tmp_path, floor + 7).stdout
    assert f"collected={floor + 7} failed=0 floor={floor}" in out, out


@pytest.mark.parametrize("over", [101, 1000])
def test_a_suite_far_ABOVE_the_floor_passes(tmp_path, over):
    """REGRESSION: red while the upper-gap refusal existed (it refused at gap > 100)."""
    r = _run(tmp_path, _floor() + over)
    assert r.returncode == 0, r.stdout + r.stderr
    assert "REFUSING" not in r.stdout, r.stdout


def test_a_suite_BELOW_the_floor_is_refused_as_a_narrowing(tmp_path):
    """The KEPT half: one test under the floor, zero failures, still red, by name."""
    floor = _floor()
    r = _run(tmp_path, floor - 1)
    assert r.returncode == 1, r.stdout + r.stderr
    assert f"REFUSING: collected {floor - 1} < floor {floor}" in r.stdout, r.stdout
    assert "the suite silently narrowed" in r.stdout, r.stdout


def test_exactly_the_floor_passes_and_a_failure_still_fails(tmp_path):
    floor = _floor()
    ok = _run(tmp_path, floor)
    assert ok.returncode == 0, ok.stdout + ok.stderr
    bad = _run(tmp_path, floor, failures=1)
    assert bad.returncode == 1, bad.stdout + bad.stderr
    assert "REFUSING" not in bad.stdout, bad.stdout
