"""The CI path classifier (`tests/ci_paths.py`) and its seam with `.github/workflows/ci.yml`.

Three kinds of guard, and they answer different questions:

- **the classifier's own contract** — literal lanes for literal change lists and events,
  including every fail-open arm. Expected values are spelled out here, never derived from
  the module under test;
- **the lists against what they describe** — `NIX` against `flake.nix`'s source filters and
  path literals, `UIAUDIT` against the `uiaudit` job's own commands. A list that falls behind
  its subject is wrong in the SKIP direction, which is the dangerous one;
- **the workflow wiring** — which job reads which lane, the fail-open expressions as WHOLE
  strings, and the output set in both directions. A lane the script computes and no job reads,
  or a job reading a lane the script never writes, would each leave every other test green.

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
FLAKE = ROOT / "flake.nix"

FULL = (True, True, True)
DOCS = (False, False, False)


def lanes(lz: Lanes) -> tuple[bool, bool, bool]:
    return (lz.heavy, lz.nix, lz.uiaudit)


# --------------------------------------------------------------------------------------------
# the classifier over literal change lists
# --------------------------------------------------------------------------------------------

@pytest.mark.parametrize(
    "paths, expected",
    [
        # (heavy, nix, uiaudit)
        (["claudedocs/handoff-x.md"], DOCS),
        (["claudedocs/handoff-x.md", "claudedocs/archive/old.md"], DOCS),
        (["claudedocs/handoff-x.md", "internal/api/server.go"], FULL),
        # A Go file the browser surface CANNOT import: heavy and nix, never uiaudit.
        (["claudedocs/handoff-x.md", "cmd/cairn-server/main.go"], (True, True, False)),
        (["cmd/cairn/main.go"], (True, True, False)),
        ([".github/workflows/publish-image.yml"], FULL),
        ([".github/workflows/ci.yml"], FULL),
        (["tests/ci_paths.py"], FULL),
        (["internal/ui/app.css"], FULL),
        (["cmd/cairn-ui/main.go"], FULL),
        (["uiaudit/run.sh"], FULL),
        (["flake.lock"], (True, True, False)),
        (["flake.nix"], (True, True, False)),
        # 🔴 `.md` IS NOT DOCS. Each of these is read by a test or a gate.
        (["AGENTS.md"], (True, False, False)),
        (["README.md"], (True, False, False)),
        (["tests/parity/README.md"], (True, False, False)),
        (["internal/ui/README.md"], (True, True, True)),
        # A prefix is a DIRECTORY prefix, not a string prefix.
        (["claudedocs.md"], (True, False, False)),
        (["docs/claudedocs/x.md"], (True, False, False)),
        # Python tests and fixtures the flake does not read.
        (["tests/test_cairn_cli.py"], (True, False, False)),
        (["tests/leakscan.py"], (True, True, False)),
        (["tests/reader_fixtures.py"], FULL),
        (["lib/subsystem_recall.py"], FULL),
        (["server/server.py"], FULL),
        (["server/Dockerfile"], (True, False, False)),
        (["cairn"], (True, True, False)),
    ],
)
def test_a_change_list_maps_to_LITERAL_lanes(paths, expected):
    assert lanes(classify(paths)) == expected


def test_an_EMPTY_change_list_runs_EVERYTHING():
    """Nothing to classify is not evidence that nothing changed — it fails OPEN."""
    assert lanes(classify([])) == FULL
    assert lanes(classify(["", "  "])) == FULL


def test_an_UNDETERMINED_change_list_runs_everything():
    assert lanes(classify(None)) == FULL


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


def test_a_pull_request_is_diffed_from_its_MERGE_BASE_to_its_head():
    git = FakeGit(DOCS_DIFF, merge_base="c" * 40)
    got = for_event({"EVENT": "pull_request", "PR_BASE": B, "PR_HEAD": H}, git)
    assert lanes(got) == DOCS
    assert git.calls == [
        ["merge-base", B, H],
        ["diff", "--name-only", "--no-renames", "c" * 40, H],
    ]


def test_a_pull_request_touching_go_is_not_docs():
    git = FakeGit(["claudedocs/a.md", "internal/store/load.go"])
    assert lanes(for_event({"EVENT": "pull_request", "PR_BASE": B, "PR_HEAD": H}, git)) == FULL


@pytest.mark.parametrize("env", [
    {"EVENT": "pull_request", "PR_BASE": "", "PR_HEAD": H},
    {"EVENT": "pull_request", "PR_BASE": B, "PR_HEAD": ""},
    {"EVENT": "pull_request", "PR_BASE": "0" * 40, "PR_HEAD": H},
])
def test_a_pull_request_without_both_shas_runs_everything_WITHOUT_ASKING_GIT(env):
    git = FakeGit(DOCS_DIFF)
    assert lanes(for_event(env, git)) == FULL
    assert git.calls == []


def test_a_pull_request_with_NO_merge_base_runs_everything():
    git = FakeGit(DOCS_DIFF, merge_base="")
    assert lanes(for_event({"EVENT": "pull_request", "PR_BASE": B, "PR_HEAD": H}, git)) == FULL


@pytest.mark.parametrize("event", ["schedule", "workflow_dispatch", "merge_group"])
def test_the_ALWAYS_FULL_events_never_consult_the_diff(event):
    git = FakeGit(DOCS_DIFF)
    assert lanes(for_event({"EVENT": event}, git)) == FULL
    assert git.calls == []


MAIN_PUSH = {"EVENT": "push", "REF": "refs/heads/main", "PUSH_BEFORE": B, "PUSH_AFTER": H}


def test_a_DOCS_ONLY_push_to_MAIN_takes_the_docs_lane():
    git = FakeGit(DOCS_DIFF)
    assert lanes(for_event(MAIN_PUSH, git)) == DOCS
    assert git.calls == [
        ["merge-base", "--is-ancestor", B, H],
        ["diff", "--name-only", "--no-renames", B, H],
    ]


@pytest.mark.parametrize("diff", [
    ["claudedocs/handoff-x.md", "internal/api/server.go"],
    # 🔴 Paths a PULL REQUEST would run only PARTIALLY for (heavy, but not nix/uiaudit):
    # `main` is strict for code, so each must still run EVERYTHING.
    ["AGENTS.md"],
    ["tests/test_cairn_cli.py"],
    ["cmd/cairn-server/main.go"],
    ["flake.lock"],
])
def test_a_push_to_MAIN_touching_ANY_non_docs_path_runs_EVERYTHING(diff):
    assert lanes(for_event(MAIN_PUSH, FakeGit(diff))) == FULL


@pytest.mark.parametrize("env, git", [
    ({**MAIN_PUSH, "PUSH_BEFORE": "0" * 40}, FakeGit(DOCS_DIFF)),
    ({**MAIN_PUSH, "PUSH_BEFORE": ""}, FakeGit(DOCS_DIFF)),
    (MAIN_PUSH, FakeGit(DOCS_DIFF, ancestor=False)),
    (MAIN_PUSH, FakeGit([])),
])
def test_a_push_to_MAIN_keeps_every_FAIL_OPEN_rule(env, git):
    """New-branch sha, missing `before`, force-push, empty diff — each runs everything on
    `main` exactly as on any other ref, even when the diff git WOULD return is docs-only."""
    assert lanes(for_event(env, git)) == FULL


def test_a_push_to_MAIN_with_a_GIT_ERROR_runs_everything():
    def git(args):
        raise subprocess.CalledProcessError(128, ["git", *args])
    assert lanes(safe_for_event(MAIN_PUSH, git)) == FULL


def test_a_push_elsewhere_is_diffed_before_to_after():
    git = FakeGit(DOCS_DIFF)
    env = {"EVENT": "push", "REF": "refs/heads/topic", "PUSH_BEFORE": B, "PUSH_AFTER": H}
    assert lanes(for_event(env, git)) == DOCS
    assert git.calls == [
        ["merge-base", "--is-ancestor", B, H],
        ["diff", "--name-only", "--no-renames", B, H],
    ]


def test_a_ZERO_SHA_push_a_new_branch_runs_everything_WITHOUT_ASKING_GIT():
    git = FakeGit(DOCS_DIFF)
    env = {"EVENT": "push", "REF": "refs/heads/topic", "PUSH_BEFORE": "0" * 40, "PUSH_AFTER": H}
    assert lanes(for_event(env, git)) == FULL
    assert git.calls == []


def test_a_FORCE_PUSH_runs_everything():
    git = FakeGit(DOCS_DIFF, ancestor=False)
    env = {"EVENT": "push", "REF": "refs/heads/topic", "PUSH_BEFORE": B, "PUSH_AFTER": H}
    assert lanes(for_event(env, git)) == FULL
    assert ["diff", "--name-only", "--no-renames", B, H] not in git.calls


def test_an_UNKNOWN_event_runs_everything():
    assert lanes(for_event({"EVENT": "issue_comment"}, FakeGit(DOCS_DIFF))) == FULL
    assert lanes(for_event({}, FakeGit(DOCS_DIFF))) == FULL


@pytest.mark.parametrize("exc", [
    subprocess.CalledProcessError(128, ["git", "diff"]),
    FileNotFoundError("git"),
    RuntimeError("anything at all"),
])
def test_ANY_classifier_error_fails_OPEN(exc):
    def git(args):
        raise exc
    env = {"EVENT": "pull_request", "PR_BASE": B, "PR_HEAD": H}
    with pytest.raises(type(exc)):
        for_event(env, git)  # the raw function propagates…
    assert lanes(safe_for_event(env, git)) == FULL  # …and the CLI's entry point runs everything


def test_the_CLI_reads_the_event_through_the_FAIL_OPEN_entry_point(monkeypatch, tmp_path):
    """The workflow calls `main()`; a `main()` that bypassed `safe_for_event` would crash on a
    `git` error and leave the outputs unwritten — still run-everything, but only by the
    workflow's own fallback, so this pins the script's half separately."""
    def boom(env, git=None):
        raise RuntimeError("classifier exploded")
    monkeypatch.setattr(ci_paths, "for_event", boom)
    out = tmp_path / "out"
    assert ci_paths.main(["--github-output", str(out)]) == 0
    assert out.read_text() == "heavy=true\nnix=true\nuiaudit=true\n"


def test_the_CLI_writes_EXACTLY_the_strings_the_workflow_compares(tmp_path):
    out = tmp_path / "out"
    ci_paths.main(["--paths", "claudedocs/a.md", "--github-output", str(out)])
    assert out.read_text() == "heavy=false\nnix=false\nuiaudit=false\n"


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
    assert lanes(safe_for_event({**pr, "PR_HEAD": docs_head}, real)) == DOCS
    assert lanes(safe_for_event({**pr, "PR_HEAD": go_head}, real)) == FULL
    # A sha the repository does not hold is a `git` failure, answered FULL.
    assert lanes(safe_for_event({**pr, "PR_HEAD": "f" * 40}, real)) == FULL


# --------------------------------------------------------------------------------------------
# the lists against the files they describe
# --------------------------------------------------------------------------------------------

def _code_lines(text: str) -> list[str]:
    return [ln for ln in text.splitlines() if not ln.lstrip().startswith("#")]


def _exists(rel: str) -> bool:
    return (ROOT / rel).exists()


def flake_inputs(text: str) -> set[tuple[str, bool]]:
    """Every repository path `flake.nix` reads, as `(path, is_dir)`.

    The filter rows (`rel == "x"`, `hasPrefix "x/" rel`) and every `./path` literal outside a
    comment. A `rel == "dir"` row is TRAVERSAL only — `cleanSourceWith` needs it to descend, and
    the file rows beneath carry the content — so it is not an input by itself. A `./x` that names
    nothing in the repository is a shell path inside a build script (`./data`), not a nix input.
    """
    found: set[tuple[str, bool]] = set()
    for ln in _code_lines(text):
        for rel in re.findall(r'rel == "([^"]+)"', ln):
            if _exists(rel) and not (ROOT / rel).is_dir():
                found.add((rel, False))
        for pre in re.findall(r'hasPrefix "([^"]+/)" rel', ln):
            found.add((pre.rstrip("/"), True))
        for lit in re.findall(r'(?<![\w.$/-])\./([A-Za-z0-9_][A-Za-z0-9_./-]*[A-Za-z0-9_])', ln):
            if _exists(lit):
                found.add((lit, (ROOT / lit).is_dir()))
    return found


def uncovered(inputs: set[tuple[str, bool]], entries) -> set[str]:
    def covered(path: str, is_dir: bool) -> bool:
        probe = path + "/" if is_dir else path
        return any(probe.startswith(e) if e.endswith("/") else (not is_dir and path == e)
                   for e in entries)
    return {p for p, d in inputs if not covered(p, d)}


def test_the_flake_extractor_FINDS_what_the_flake_is_known_to_read():
    """Positive control: an extractor that found nothing would make the coverage test below
    vacuously green. Each of these is a different extraction route."""
    got = {p for p, _ in flake_inputs(FLAKE.read_text(encoding="utf-8"))}
    for must in ("go.sum",                        # a `rel ==` file row
                 "cmd", "internal", "uiaudit",    # `hasPrefix` rows
                 "lib",                           # a `src = ./lib` literal
                 "server/server.py",              # a `${./…}` literal
                 "tests/ui_image_session_dir_check.py"):
        assert must in got, f"the flake extractor no longer finds {must!r}"


def test_EVERY_path_the_flake_reads_is_in_the_NIX_lane():
    missing = uncovered(flake_inputs(FLAKE.read_text(encoding="utf-8")), ci_paths.NIX)
    assert not missing, (
        f"flake.nix reads {sorted(missing)} but `tests/ci_paths.py`'s NIX lane does not cover "
        "them, so a change to one alone would SKIP the nix job. Add them to `NIX`."
    )


def test_the_NIX_coverage_check_can_go_RED():
    """Negative control: drop one entry and the check must name exactly it."""
    inputs = flake_inputs(FLAKE.read_text(encoding="utf-8"))
    narrowed = tuple(e for e in ci_paths.NIX if e != "tests/leakscan.py")
    assert uncovered(inputs, narrowed) == {"tests/leakscan.py"}


def test_NO_NIX_entry_is_DEAD():
    """The shrink direction: an entry the flake does not read makes the nix job run for a change
    it cannot see. `flake.nix` and `flake.lock` are the flake itself, read by definition."""
    inputs = flake_inputs(FLAKE.read_text(encoding="utf-8"))
    dead = [e for e in ci_paths.NIX
            if e not in ("flake.nix", "flake.lock") and len(uncovered(inputs, [e])) == len(inputs)]
    assert not dead, f"NIX entries that cover no path the flake reads: {dead}"


def _jobs(text: str) -> dict[str, str]:
    """Top-level job bodies of `ci.yml`, anchored to the `jobs:` key (the `on:` block carries
    keys at the same indent)."""
    lines = text.splitlines()
    start = lines.index("jobs:") + 1
    keys = [(i, ln[2:-1]) for i, ln in enumerate(lines[start:], start)
            if re.fullmatch(r"  [A-Za-z0-9_-]+:", ln)]
    bounds = [i for i, _ in keys] + [len(lines)]
    return {name: "\n".join(lines[bounds[k]:bounds[k + 1]]) for k, (_, name) in enumerate(keys)}


JOBS = _jobs(CI.read_text(encoding="utf-8"))


def uiaudit_job_paths(job: str) -> set[tuple[str, bool]]:
    found = set()
    for ln in _code_lines(job):
        for p in re.findall(r'(?<![\w/}-])(?:\./)?((?:cmd|internal|uiaudit|tests|lib|server)'
                            r'/[A-Za-z0-9_./-]*[A-Za-z0-9_])', ln):
            if _exists(p):
                found.add((p, (ROOT / p).is_dir()))
    return found


def test_EVERY_path_the_uiaudit_job_names_is_in_the_UIAUDIT_lane():
    paths = uiaudit_job_paths(JOBS["uiaudit"])
    # Positive control: the extractor must see the binary it builds and the scripts it runs.
    assert {("cmd/cairn-ui", True), ("uiaudit/run.sh", False),
            ("uiaudit/pwa_check.sh", False)} <= paths, sorted(paths)
    missing = uncovered(paths, ci_paths.UIAUDIT)
    assert not missing, f"the uiaudit job reads {sorted(missing)}; add them to `UIAUDIT`"
    # …and the check can go red.
    assert uncovered(paths, tuple(e for e in ci_paths.UIAUDIT if e != "uiaudit/")) >= {
        "uiaudit/run.sh"}


def test_the_uiaudit_FIXTURE_WORLD_and_its_imports_are_in_the_lane():
    """`uiaudit/boot.go` builds its store with `tests/reader_fixtures.py`, which imports `lib/`
    and parses `server/server.py` — none named in the job's own commands, so pinned here from
    the two files that establish the chain."""
    assert "tests/reader_fixtures.py" in (ROOT / "uiaudit" / "boot.go").read_text(encoding="utf-8")
    fixtures = (ROOT / "tests" / "reader_fixtures.py").read_text(encoding="utf-8")
    assert 'ROOT / "lib"' in fixtures and 'ROOT / "server" / "server.py"' in fixtures
    chain = {("tests/reader_fixtures.py", False), ("lib", True), ("server/server.py", False)}
    assert not uncovered(chain, ci_paths.UIAUDIT)


# --------------------------------------------------------------------------------------------
# the workflow wiring
# --------------------------------------------------------------------------------------------

JOB_GATE = ("if: ${{ !cancelled() && (needs.changes.result != 'success' "
            "|| needs.changes.outputs.%s != 'false') }}")
STEP_GATE = "if: ${{ needs.changes.result != 'success' || needs.changes.outputs.heavy != 'false' }}"

#: 🔴 THE LEDGER: which job reads which lane. Fails on GROW or SHRINK.
GATED_JOBS = {"parity": "heavy", "dualrun": "heavy", "nix": "nix",
              "uiaudit": "uiaudit", "pgtest": "heavy"}
GATED_GO_STEPS = {
    "the conformance corpus against the Go server",
    "the arcs/sessions end-to-end check, after proving it can go RED",
    "the arcs/presence end-to-end check, after proving every clause can go RED",
}
UNGATED_JOBS = {"leakscan", "tests", "changes"}


def _job_header(body: str) -> dict[str, str]:
    """The job's own 4-space-indented scalar keys (`needs`, `if`, …)."""
    return dict(m.groups() for m in re.finditer(r"^    ([a-z-]+): (.+)$", body, re.M))


def test_the_changes_job_declares_EXACTLY_the_lanes_the_classifier_writes():
    body = JOBS["changes"]
    declared = dict(re.findall(r"^      ([a-z_]+): \$\{\{ steps\.classify\.outputs\.([a-z_]+) \}\}$",
                               body, re.M))
    assert set(declared) == set(ci_paths.LANES)
    assert all(k == v for k, v in declared.items()), declared
    assert 'run: python3 tests/ci_paths.py --github-output "$GITHUB_OUTPUT"' in body
    assert "fetch-depth: 0" in body


def test_every_lane_is_READ_and_nothing_reads_a_lane_that_does_not_exist():
    text = CI.read_text(encoding="utf-8")
    read = set(re.findall(r"needs\.changes\.outputs\.([a-z_]+)", text))
    assert read == set(ci_paths.LANES)


def test_the_GATED_JOBS_ledger_with_the_fail_open_expression_as_a_WHOLE_string():
    gated = {name: body for name, body in JOBS.items() if "needs.changes.outputs." in
             _job_header(body).get("if", "")}
    assert set(gated) == set(GATED_JOBS)
    for name, lane in GATED_JOBS.items():
        hdr = _job_header(JOBS[name])
        assert hdr.get("needs") == "changes", name
        assert "if: " + hdr["if"] == JOB_GATE % lane, (name, hdr["if"])


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
    assert {"go vet", "go test, under `-race`, with an `ok` floor"} <= names - gated


def test_leakscan_and_the_pytest_suite_are_NEVER_gated():
    assert UNGATED_JOBS <= set(JOBS)
    for name in UNGATED_JOBS:
        hdr = _job_header(JOBS[name])
        assert "if" not in hdr and "needs" not in hdr, (name, hdr)


def test_every_job_is_in_exactly_one_ledger():
    assert set(JOBS) == set(GATED_JOBS) | UNGATED_JOBS | {"go"}


def test_every_TRIGGER_in_ci_yml_has_a_defined_classification():
    """The `on:` keys, read from the file: each must reach a rule rather than the unknown-event
    arm, and every one except a pull request and a push must classify FULL with no diff
    consulted. (A push to `main` is diffed — its two rules are pinned above.)"""
    text = CI.read_text(encoding="utf-8")
    on_block = text.split("\non:\n", 1)[1].split("\npermissions:", 1)[0]
    events = set(re.findall(r"^  ([a-z_]+):", on_block, re.M))
    assert events == {"push", "pull_request", "merge_group", "schedule", "workflow_dispatch"}
    assert re.search(r"^  push:\n    branches: \[main\]$", on_block, re.M), (
        "push is no longer main-only; `ci_paths.for_event`'s strict-for-code `main` rule is then "
        "not the only push rule that runs")
    for ev in events - {"pull_request", "push"}:
        env = {"EVENT": ev, "REF": "refs/heads/main", "PUSH_BEFORE": B, "PUSH_AFTER": H}
        git = FakeGit(DOCS_DIFF)
        assert lanes(for_event(env, git)) == FULL, ev
        assert git.calls == [], ev
