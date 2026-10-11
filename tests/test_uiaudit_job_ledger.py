"""The `uiaudit` job's step ledger: every step, in order, with the leg it runs in.

`uiaudit` runs as a two-leg matrix (`walk`, `pwa`) so its serial ~9.5 minutes become two parallel
legs. Splitting a job is how a step gets DROPPED without anything going red — a step whose `if:`
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


def test_the_matrix_declares_exactly_the_two_legs_and_never_fails_fast():
    """With the ledger's `if:` strings pinned whole, this is what makes every leg a step names exist."""
    assert re.search(r"^    strategy:\n      fail-fast: false\n      matrix:\n        part: \[walk, pwa\]$",
                     JOBS["uiaudit"], re.M)


def test_the_hub_secrets_reach_ONE_step_in_ONE_leg():
    """One leg pushes, so one run pushes at most once — the property the split must not break."""
    holders = [(_label(s), _if(s)) for s in _steps(JOBS["uiaudit"]) if "secrets." in s]
    assert holders == [("the walk — boot cairn-ui, drive chromium, push, read the diff back", WALK)]
