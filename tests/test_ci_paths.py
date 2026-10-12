"""The CI path classifier (`tests/ci_paths.py`) and its seam with `.github/workflows/ci.yml`.

Two kinds of guard, and they answer different questions:

- **the classifier's own contract** — the literal lane for literal change lists and events,
  including every fail-open arm. Expected values are spelled out here, never derived from
  the module under test;
- **the workflow wiring** — which jobs and steps read the lane, the fail-open expressions as
  WHOLE strings, the output set in both directions, and the trigger set read STRUCTURALLY out
  of `on:`. A lane the script computes and no job reads, or a job reading one the script never
  writes, would each leave every other test green.

⚠ WHAT THIS FILE CANNOT DO: it reads `ci.yml` as text. Whether GitHub evaluates the
expressions as pinned here is measured only by a live run — the docs-lane draft PR recorded in
the change that introduced this file.
"""
from __future__ import annotations

import re
import subprocess
from pathlib import Path

import pytest

import ci_paths
from ci_paths import Lanes, classify, for_event, safe_for_event

ROOT = Path(__file__).resolve().parent.parent
CI = ROOT / ".github" / "workflows" / "ci.yml"

FULL = True
DOCS = False


def heavy(lz: Lanes) -> bool:
    return lz.heavy


# --------------------------------------------------------------------------------------------
# the classifier over literal change lists
# --------------------------------------------------------------------------------------------

@pytest.mark.parametrize(
    "paths, expected",
    [
        (["claudedocs/handoff-x.md"], DOCS),
        (["claudedocs/handoff-x.md", "claudedocs/archive/old.md"], DOCS),
        (["claudedocs/handoff-x.md", "internal/api/server.go"], FULL),
        (["cmd/cairn/main.go"], FULL),
        ([".github/workflows/publish-image.yml"], FULL),
        ([".github/workflows/ci.yml"], FULL),
        (["tests/ci_paths.py"], FULL),
        (["internal/ui/app.css"], FULL),
        (["flake.lock"], FULL),
        # 🔴 `.md` IS NOT DOCS. Each of these is read by a test or a gate.
        (["AGENTS.md"], FULL),
        (["README.md"], FULL),
        (["tests/parity/README.md"], FULL),
        (["internal/ui/README.md"], FULL),
        # A prefix is a DIRECTORY prefix, not a string prefix.
        (["claudedocs.md"], FULL),
        (["docs/claudedocs/x.md"], FULL),
        (["tests/test_cairn_cli.py"], FULL),
    ],
)
def test_a_change_list_maps_to_a_LITERAL_lane(paths, expected):
    assert heavy(classify(paths)) is expected


def test_an_EMPTY_change_list_runs_EVERYTHING():
    """Nothing to classify is not evidence that nothing changed — it fails OPEN."""
    assert heavy(classify([])) is FULL
    assert heavy(classify(["", "  "])) is FULL


def test_an_UNDETERMINED_change_list_runs_everything():
    assert heavy(classify(None)) is FULL


# --------------------------------------------------------------------------------------------
# the event rules, over a fake `git` that records what it was asked
# --------------------------------------------------------------------------------------------

class FakeGit:
    def __init__(self, diff: list[str], merge_base: str = "m" * 40, ancestor: bool = True):
        self.diff, self.merge_base, self.ancestor = diff, merge_base, ancestor
        self.calls: list[list[str]] = []

    def __call__(self, args):
        self.calls.append(list(args))
        if args[0] == "merge-base" and "--is-ancestor" in args:
            if not self.ancestor:
                raise subprocess.CalledProcessError(1, ["git", *args])
            return ""
        if args[0] == "merge-base":
            return self.merge_base + "\n"
        if args[0] == "diff":
            return "".join(f"{p}\n" for p in self.diff)
        raise AssertionError(f"unexpected git call {args}")


DOCS_DIFF = ["claudedocs/handoff-x.md"]
B, H = "b" * 40, "h" * 40
PUSH = {"EVENT": "push", "PUSH_BEFORE": B, "PUSH_AFTER": H}


def test_a_pull_request_is_diffed_from_its_MERGE_BASE_to_its_head():
    git = FakeGit(DOCS_DIFF, merge_base="c" * 40)
    got = for_event({"EVENT": "pull_request", "PR_BASE": B, "PR_HEAD": H}, git)
    assert heavy(got) is DOCS
    assert git.calls == [
        ["merge-base", B, H],
        ["diff", "--name-only", "--no-renames", "c" * 40, H],
    ]


def test_a_pull_request_touching_go_is_not_docs():
    git = FakeGit(["claudedocs/a.md", "internal/store/load.go"])
    assert heavy(for_event({"EVENT": "pull_request", "PR_BASE": B, "PR_HEAD": H}, git)) is FULL


@pytest.mark.parametrize("env", [
    {"EVENT": "pull_request", "PR_BASE": "", "PR_HEAD": H},
    {"EVENT": "pull_request", "PR_BASE": B, "PR_HEAD": ""},
    {"EVENT": "pull_request", "PR_BASE": "0" * 40, "PR_HEAD": H},
])
def test_a_pull_request_without_both_shas_runs_everything_WITHOUT_ASKING_GIT(env):
    git = FakeGit(DOCS_DIFF)
    assert heavy(for_event(env, git)) is FULL
    assert git.calls == []


def test_a_pull_request_with_NO_merge_base_runs_everything():
    git = FakeGit(DOCS_DIFF, merge_base="")
    assert heavy(for_event({"EVENT": "pull_request", "PR_BASE": B, "PR_HEAD": H}, git)) is FULL


def test_workflow_dispatch_never_consults_the_diff():
    git = FakeGit(DOCS_DIFF)
    assert heavy(for_event({"EVENT": "workflow_dispatch"}, git)) is FULL
    assert git.calls == []


def test_a_DOCS_ONLY_push_takes_the_docs_lane():
    """The workflow only triggers `push` on `main` (pinned below), so this is a push to `main`."""
    git = FakeGit(DOCS_DIFF)
    assert heavy(for_event(PUSH, git)) is DOCS
    assert git.calls == [
        ["merge-base", "--is-ancestor", B, H],
        ["diff", "--name-only", "--no-renames", B, H],
    ]


@pytest.mark.parametrize("diff", [
    ["claudedocs/handoff-x.md", "internal/api/server.go"],
    ["AGENTS.md"],
    ["tests/test_cairn_cli.py"],
    ["flake.lock"],
])
def test_a_push_touching_ANY_non_docs_path_runs_EVERYTHING(diff):
    assert heavy(for_event(PUSH, FakeGit(diff))) is FULL


@pytest.mark.parametrize("env, git", [
    ({**PUSH, "PUSH_BEFORE": "0" * 40}, FakeGit(DOCS_DIFF)),   # a new branch
    ({**PUSH, "PUSH_BEFORE": ""}, FakeGit(DOCS_DIFF)),
    ({**PUSH, "PUSH_AFTER": ""}, FakeGit(DOCS_DIFF)),
    (PUSH, FakeGit(DOCS_DIFF, ancestor=False)),                # a force-push
    (PUSH, FakeGit([])),                                       # an empty diff
])
def test_a_push_keeps_every_FAIL_OPEN_rule(env, git):
    """Each runs everything even when the diff git WOULD return is docs-only."""
    assert heavy(for_event(env, git)) is FULL


def test_a_FORCE_PUSH_is_never_diffed():
    git = FakeGit(DOCS_DIFF, ancestor=False)
    assert heavy(for_event(PUSH, git)) is FULL
    assert ["diff", "--name-only", "--no-renames", B, H] not in git.calls


def test_a_NEW_BRANCH_push_never_asks_git():
    git = FakeGit(DOCS_DIFF)
    assert heavy(for_event({**PUSH, "PUSH_BEFORE": "0" * 40}, git)) is FULL
    assert git.calls == []


@pytest.mark.parametrize("event", ["issue_comment", "merge_group", "schedule", ""])
def test_an_UNKNOWN_event_runs_everything(event):
    """`merge_group` and `schedule` are no longer triggers; were either re-added without a rule,
    it would land here and run everything."""
    git = FakeGit(DOCS_DIFF)
    assert heavy(for_event({"EVENT": event}, git)) is FULL
    assert git.calls == []


@pytest.mark.parametrize("exc", [
    subprocess.CalledProcessError(128, ["git", "diff"]),
    FileNotFoundError("git"),
    RuntimeError("anything at all"),
])
@pytest.mark.parametrize("env", [
    {"EVENT": "pull_request", "PR_BASE": B, "PR_HEAD": H},
    PUSH,
])
def test_ANY_classifier_error_fails_OPEN(exc, env):
    def git(args):
        raise exc
    if env["EVENT"] == "push" and isinstance(exc, subprocess.CalledProcessError):
        # The ancestry check's own failure IS the force-push arm, so the raw function answers.
        assert heavy(for_event(env, git)) is FULL
    else:
        with pytest.raises(type(exc)):
            for_event(env, git)  # the raw function propagates…
    assert heavy(safe_for_event(env, git)) is FULL  # …and the CLI's entry point runs everything


def test_the_CLI_reads_the_event_through_the_FAIL_OPEN_entry_point(monkeypatch, tmp_path):
    """The workflow calls `main()`; a `main()` that bypassed `safe_for_event` would crash on a
    `git` error and leave the output unwritten — still run-everything, but only by the
    workflow's own fallback, so this pins the script's half separately."""
    def boom(env, git=None):
        raise RuntimeError("classifier exploded")
    monkeypatch.setattr(ci_paths, "for_event", boom)
    out = tmp_path / "out"
    assert ci_paths.main(["--github-output", str(out)]) == 0
    assert out.read_text() == "heavy=true\n"


def test_the_CLI_writes_EXACTLY_the_strings_the_workflow_compares(tmp_path):
    out = tmp_path / "out"
    ci_paths.main(["--paths", "claudedocs/a.md", "--github-output", str(out)])
    assert out.read_text() == "heavy=false\n"


def test_REAL_git_classifies_a_docs_only_pull_request(tmp_path):
    """Positive control on the git commands themselves, against a real repository: the fake
    above proves the call shape, this proves `git` answers it the way the module parses."""
    def g(*a):
        return subprocess.run(["git", "-C", str(tmp_path), *a], check=True,
                              capture_output=True, text=True).stdout.strip()
    g("init", "-q", "-b", "main")
    g("config", "user.email", "ci@example.invalid")
    g("config", "user.name", "ci")
    (tmp_path / "internal").mkdir()
    (tmp_path / "internal" / "a.go").write_text("package a\n")
    g("add", "internal/a.go")
    g("commit", "-q", "-m", "base")
    base = g("rev-parse", "HEAD")
    g("checkout", "-q", "-b", "topic")
    (tmp_path / "claudedocs").mkdir()
    (tmp_path / "claudedocs" / "h.md").write_text("notes\n")
    g("add", "claudedocs/h.md")
    g("commit", "-q", "-m", "docs")
    docs_head = g("rev-parse", "HEAD")
    (tmp_path / "internal" / "a.go").write_text("package a\n\n// x\n")
    g("add", "internal/a.go")
    g("commit", "-q", "-m", "go")
    go_head = g("rev-parse", "HEAD")

    def real(args):
        return subprocess.run(["git", "-C", str(tmp_path), *args], check=True,
                              capture_output=True, text=True).stdout

    pr = {"EVENT": "pull_request", "PR_BASE": base}
    assert heavy(safe_for_event({**pr, "PR_HEAD": docs_head}, real)) is DOCS
    assert heavy(safe_for_event({**pr, "PR_HEAD": go_head}, real)) is FULL
    # A push is diffed before..after on the same repository.
    push = {"EVENT": "push", "PUSH_BEFORE": base}
    assert heavy(safe_for_event({**push, "PUSH_AFTER": docs_head}, real)) is DOCS
    assert heavy(safe_for_event({**push, "PUSH_AFTER": go_head}, real)) is FULL
    # A sha the repository does not hold is a `git` failure, answered FULL.
    assert heavy(safe_for_event({**pr, "PR_HEAD": "f" * 40}, real)) is FULL


# --------------------------------------------------------------------------------------------
# the workflow wiring
# --------------------------------------------------------------------------------------------

def _top_level_block(text: str, key: str) -> list[str]:
    """The lines of one top-level mapping key, read by INDENTATION: from `key:` to the next line
    that starts at column 0 and is not a comment. Never bounded by naming the key that happens
    to follow — a block inserted between them (a `concurrency:` after `on:`) would leak in."""
    lines = text.splitlines()
    start = lines.index(f"{key}:") + 1
    out = []
    for ln in lines[start:]:
        if ln and not ln[0].isspace() and not ln.startswith("#"):
            break
        out.append(ln)
    return out


def trigger_events(text: str) -> set[str]:
    """The event names under `on:` — its own direct children, at exactly two spaces."""
    return {m.group(1) for ln in _top_level_block(text, "on")
            if (m := re.fullmatch(r"  ([a-z_]+):.*", ln))}


def _jobs(text: str) -> dict[str, str]:
    """Top-level job bodies of `ci.yml`, read from the `jobs:` block only."""
    block = _top_level_block(text, "jobs")
    keys = [(i, ln[2:-1]) for i, ln in enumerate(block) if re.fullmatch(r"  [A-Za-z0-9_-]+:", ln)]
    bounds = [i for i, _ in keys] + [len(block)]
    return {name: "\n".join(block[bounds[k]:bounds[k + 1]]) for k, (_, name) in enumerate(keys)}


CI_TEXT = CI.read_text(encoding="utf-8")
JOBS = _jobs(CI_TEXT)

JOB_GATE = ("if: ${{ !cancelled() && (needs.changes.result != 'success' "
            "|| needs.changes.outputs.heavy != 'false') }}")
STEP_GATE = "if: ${{ needs.changes.result != 'success' || needs.changes.outputs.heavy != 'false' }}"

#: 🔴 THE LEDGER of jobs the docs lane skips. Fails on GROW or SHRINK.
GATED_JOBS = {"parity", "dualrun", "nix", "uiaudit", "uiaudit-touch", "pgtest", "mutants", "mutants-aggregate"}
GATED_GO_STEPS = {
    "the conformance corpus against the Go server",
    "the arcs/sessions end-to-end check, after proving it can go RED",
    "the arcs/presence end-to-end check, after proving every clause can go RED",
    # S5 of the scope-refs plan: the closing check, tiered like the arcs checks beside it.
    "the scope-refs end-to-end check, after proving it can go RED",
}
UNGATED_JOBS = {"leakscan", "tests", "changes"}


def _job_header(body: str) -> dict[str, str]:
    """The job's own 4-space-indented scalar keys (`needs`, `if`, …)."""
    return dict(m.groups() for m in re.finditer(r"^    ([a-z-]+): (.+)$", body, re.M))


def test_the_changes_job_declares_EXACTLY_the_lanes_the_classifier_writes():
    body = JOBS["changes"]
    declared = dict(re.findall(r"^      ([a-z_]+): \$\{\{ steps\.classify\.outputs\.([a-z_]+) \}\}$",
                               body, re.M))
    assert set(declared) == set(ci_paths.LANES) == {"heavy"}
    assert all(k == v for k, v in declared.items()), declared
    assert 'run: python3 tests/ci_paths.py --github-output "$GITHUB_OUTPUT"' in body
    assert "fetch-depth: 0" in body


#: 🔴 THE EVENT FIELDS THE CLASSIFIER IS HANDED, AS WHOLE STRINGS. Every arm above is tested by
#: passing `env` directly, so none of them can see the workflow handing it the WRONG field: with
#: `PR_BASE`/`PR_HEAD` swapped, a PR changing Go on a base that moved only by a `claudedocs/`
#: commit diffs `merge-base..BASE` and classifies docs-only — every heavy gate skipped, every
#: other test green (measured: the suite passed 58/58 under that swap before this pin).
CLASSIFY_ENV = {
    "EVENT": "${{ github.event_name }}",
    "PR_BASE": "${{ github.event.pull_request.base.sha }}",
    "PR_HEAD": "${{ github.event.pull_request.head.sha }}",
    "PUSH_BEFORE": "${{ github.event.before }}",
    "PUSH_AFTER": "${{ github.event.after }}",
}


def classify_step_env(job: str) -> dict[str, str]:
    """The `env:` mapping of the step with `id: classify`, read by indentation."""
    steps = re.split(r"^      - ", job, flags=re.M)[1:]
    (step,) = [s for s in steps if re.search(r"^        id: classify$", s, re.M)]
    lines = step.splitlines()
    start = lines.index("        env:") + 1
    env = {}
    for ln in lines[start:]:
        m = re.fullmatch(r"          ([A-Z_]+): (.+)", ln)
        if not m:
            break
        env[m.group(1)] = m.group(2)
    return env


def test_the_classify_step_hands_the_script_EXACTLY_the_right_event_fields():
    assert classify_step_env(JOBS["changes"]) == CLASSIFY_ENV


def test_the_classify_env_is_EXACTLY_what_the_script_reads():
    """Both directions: a variable the script reads that the step does not pass is always empty
    (and silently FULL, or worse), and one the step passes that nothing reads is a dead pin."""
    src = (ROOT / "tests" / "ci_paths.py").read_text(encoding="utf-8")
    read = set(re.findall(r'env\.get\("([A-Z_]+)"', src))
    assert read == set(CLASSIFY_ENV), read


def test_every_lane_is_READ_and_nothing_reads_a_lane_that_does_not_exist():
    read = set(re.findall(r"needs\.changes\.outputs\.([a-z_]+)", CI_TEXT))
    assert read == set(ci_paths.LANES)


def test_the_GATED_JOBS_ledger_with_the_fail_open_expression_as_a_WHOLE_string():
    gated = {name for name, body in JOBS.items()
             if "needs.changes.outputs." in _job_header(body).get("if", "")}
    assert gated == GATED_JOBS
    for name in GATED_JOBS:
        hdr = _job_header(JOBS[name])
        needs = hdr.get("needs", "")
        # `needs: changes`, or a list that names it (`mutants-aggregate` also needs `mutants`).
        assert needs == "changes" or "changes" in re.findall(r"[\w-]+", needs.strip("[]")), name
        assert "if: " + hdr["if"] == JOB_GATE, (name, hdr["if"])


def test_the_go_job_ALWAYS_runs_and_only_its_expensive_steps_are_tiered():
    hdr = _job_header(JOBS["go"])
    assert hdr.get("needs") == "changes" and hdr.get("if") == "${{ !cancelled() }}"
    steps = re.split(r"^      - ", JOBS["go"], flags=re.M)[1:]
    gated = set()
    for step in steps:
        name = re.match(r"name: (.+)", step)
        if STEP_GATE in step:
            assert name, step[:80]
            gated.add(name.group(1))
        elif "needs.changes" in step:
            raise AssertionError(f"a go step reads `needs.changes` in another shape: {step[:120]}")
    assert gated == GATED_GO_STEPS
    names = {m.group(1) for s in steps if (m := re.match(r"name: (.+)", s))}
    assert {"go vet", "go test, under `-race`, with the `ok` count derived from `go list`",
            # its other half: the tests the race walk skips, run without the detector. Gating it
            # would leave those tests unrun on exactly the events the race walk still covers.
            "the race walk's declared exemptions, run without `-race` and counted by name"} <= names - gated


def test_leakscan_and_the_pytest_suite_are_NEVER_gated():
    assert UNGATED_JOBS <= set(JOBS)
    for name in UNGATED_JOBS:
        hdr = _job_header(JOBS[name])
        assert "if" not in hdr and "needs" not in hdr, (name, hdr)


def test_every_job_is_in_exactly_one_ledger():
    assert set(JOBS) == GATED_JOBS | UNGATED_JOBS | {"go"}


def test_the_TRIGGER_set_is_exactly_the_events_the_classifier_has_a_rule_for():
    """Read structurally out of `on:`. `push` and `pull_request` are diffed (pinned above);
    `workflow_dispatch` is FULL with no diff. `merge_group` and `schedule` are deliberately
    absent; re-adding either without a rule here is caught by this equality."""
    assert trigger_events(CI_TEXT) == {"push", "pull_request", "workflow_dispatch"}
    on_block = "\n".join(_top_level_block(CI_TEXT, "on"))
    assert re.search(r"^  push:\n    branches: \[main\]$", on_block, re.M), (
        "push is no longer main-only; the docs lane would then also apply to pushes elsewhere, "
        "which nothing here has decided")


# A `concurrency:` block between `on:` and `permissions:` — the shape a sibling change adds —
# with keys at the same two-space indent as an event. The old parser (text between `\non:\n`
# and `\npermissions:`) read `group:` and `cancel-in-progress:` as events.
CONCURRENCY_FIXTURE = """name: ci

on:
  push:
    branches: [main]
  pull_request:
  # a comment at the trigger indent
  workflow_dispatch:

# a top-level comment between the blocks
concurrency:
  group: ci-${{ github.workflow }}
  cancel_in_progress: true

permissions:
  contents: read

jobs:
  leakscan:
    runs-on: ubuntu-latest
"""


def test_the_trigger_parser_is_NOT_fooled_by_a_following_top_level_block():
    assert trigger_events(CONCURRENCY_FIXTURE) == {"push", "pull_request", "workflow_dispatch"}
    # Control: the old text-bounded parse DOES read the concurrency keys, so the fixture can
    # tell the two parsers apart.
    old = CONCURRENCY_FIXTURE.split("\non:\n", 1)[1].split("\npermissions:", 1)[0]
    assert "group" in set(re.findall(r"^  ([a-z_]+):", old, re.M))
    # And the job reader sees only `jobs:` children.
    assert set(_jobs(CONCURRENCY_FIXTURE)) == {"leakscan"}
