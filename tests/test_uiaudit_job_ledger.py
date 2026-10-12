"""The `uiaudit` job's step ledger: every step, in order, with the leg it runs in.

`uiaudit` runs as a three-leg matrix (`walk`, `pwa`, `memos`) so its serial ~9.5 minutes became two
parallel legs, and the memos tab's browser check — the only leg with a database — a third beside them. Splitting a job is how a step gets DROPPED without anything going red — a step whose `if:`
names a leg the matrix does not have never runs and never fails — and how an ORDERING breaks
silently: the walk must stay behind the guards (they hold the positive control) and `verify-push`
must stay behind the walk, in the same leg, because steps in different legs share no state.

So the ledger below is the whole job, pinned as WHOLE `if:` strings in order: a step added,
removed, renamed, reordered or moved to another leg fails here, and so does a leg the matrix no
longer declares. The step set is the one the job had as a single job (PR "E" of the CI speed-up
carries the before/after list); only the `if:` column is new.

⚠ WHAT THIS FILE CANNOT DO: it reads `ci.yml` as text. That GitHub runs each step in the leg
pinned here is measured only by a live run — the PR that introduced this file carries one.
"""
from __future__ import annotations

import re

from test_ci_paths import JOBS

WALK = "matrix.part == 'walk'"
PWA = "matrix.part == 'pwa'"
MEMOS = "matrix.part == 'memos'"

#: (step label, its `if:` exactly, or None for a step that runs in EVERY leg).
#: The label is the step's `name:`, or its `uses:` when it has no name.
LEDGER = [
    ("actions/checkout@v4", None),
    ("actions/setup-go@v5", None),
    ("the toolchain that will actually run, asserted rather than assumed", None),
    ("actions/setup-python@v5", None),
    ("cachix/install-nix-action@v31", None),
    ("chromium from the flake's pinned nixpkgs, its version asserted", None),
    ("the pinned chromium is the one on PATH", None),
    ("the harness's own guards, including the positive control", WALK),
    ("the walk — boot cairn-ui, drive chromium, push, read the diff back", WALK),
    ("pwa_check.sh — the mobile plan's closing-condition clauses (a)–(e)", PWA),
    ("postgres from the flake's pinned nixpkgs, its major asserted", MEMOS),
    ("memos_check.py — the populated memos tab in a real browser, after proving each clause can go RED", MEMOS),
    ("verify-push — the push actually landed", f"{WALK} && steps.walk.outputs.credentialed == 'yes'"),
    ("the deterministic diff, in the log and in the step summary", f"always() && {WALK}"),
    ("upload the captures", f"always() && {WALK}"),
]


def _steps(job: str) -> list[str]:
    return re.split(r"^      - ", job, flags=re.M)[1:]


def _label(step: str) -> str:
    m = re.match(r"(?:name|uses): (.+)", step)
    assert m, step[:80]
    return m.group(1)


def _if(step: str) -> str | None:
    m = re.search(r"^        if: (.+)$", step, re.M)
    return m.group(1) if m else None


def test_every_step_is_in_the_ledger_in_order_with_its_leg_as_a_WHOLE_string():
    got = [(_label(s), _if(s)) for s in _steps(JOBS["uiaudit"])]
    assert got == LEDGER


def test_the_matrix_declares_exactly_the_three_legs_and_never_fails_fast():
    """With the ledger's `if:` strings pinned whole, this is what makes every leg a step names exist."""
    assert re.search(r"^    strategy:\n      fail-fast: false\n      matrix:\n        part: \[walk, pwa, memos\]$",
                     JOBS["uiaudit"], re.M)


def test_the_memos_leg_runs_the_tagged_browser_test_through_its_one_runner():
    """`TestTheMemosTabInARealBrowser` is behind `//go:build memobrowser`, so the module's own
    `go test ./...` (the walk leg) cannot see it, and the walk leg's declared-test count excludes
    its file by name. Both are only safe while SOMETHING runs it: this pins the three places that
    must agree — the tag on the file, the runner naming that tag and that test, and the leg
    invoking the runner (self-test first) and counting the five clause lines.

    ⚠ TEXT ONLY, like the rest of this file: that the leg passes is a live run's claim.
    """
    from pathlib import Path
    repo = Path(__file__).resolve().parent.parent
    test = (repo / "uiaudit/memos_browser_test.go").read_text(encoding="utf-8")
    assert test.startswith("//go:build memobrowser\n")
    assert "\nfunc TestTheMemosTabInARealBrowser(t *testing.T) {\n" in test
    assert "t.Skip" not in test, "the browser test must refuse, never skip"
    clauses = re.findall(r'^\tt\.Run\("([a-e]_[a-z]+)", ', test, re.M)
    assert clauses == ["a_seeded", "b_send", "c_draft", "d_retract", "e_reader"]

    runner = (repo / "uiaudit/memos_check.py").read_text(encoding="utf-8")
    assert 'TEST = "TestTheMemosTabInARealBrowser"' in runner
    assert 'TAG = "memobrowser"' in runner
    assert re.findall(r'^    "([a-e]_[a-z]+)": "memos clause \(', runner, re.M) == clauses
    sabotages = re.findall(r'^    \("[a-z-]+", "([a-e]_[a-z]+)", ', runner, re.M)
    assert len(sabotages) == 7 and set(sabotages) == set(clauses), sabotages

    job = JOBS["uiaudit"]
    (step,) = [s for s in _steps(job) if _label(s).startswith("memos_check.py")]
    assert "./uiaudit/memos_check.py --self-test" in step
    assert "'SUMMARY memos-check-self-test: sabotaged=7 caught=7'" in step
    assert '[ "$passes" != "5" ]' in step
    assert "':(exclude)memos_browser_test.go'" in job, "the walk leg's static count must exclude the tagged file by name"


HUB_SECRETS = ["CAIRN_AUDIT_PUSH_URL", "CAIRN_AUDIT_PUSH_TOKEN", "CAIRN_AUDIT_API_URL", "CAIRN_AUDIT_API_TOKEN"]


def test_the_hub_secrets_reach_ONE_step_in_ONE_leg():
    """One leg pushes, so one run pushes at most once — the property the split must not break.

    🔴 COUNTED OVER THE WHOLE JOB TEXT, NOT OVER `_steps()`: the step split drops the job header,
    so a secret in the JOB-level `env:` (beside `GOTOOLCHAIN`) — which reaches BOTH legs — was
    invisible to a steps-only scan and left this test green.
    """
    job = JOBS["uiaudit"]
    (walk,) = [s for s in _steps(job)
               if _label(s) == "the walk — boot cairn-ui, drive chromium, push, read the diff back"]
    assert _if(walk) == WALK
    in_job = re.findall(r"secrets\.([A-Z_]+)", job)
    in_walk = re.findall(r"secrets\.([A-Z_]+)", walk)
    assert in_walk == HUB_SECRETS
    assert in_job == in_walk, f"a secret is referenced outside the walk step: {in_job}"
