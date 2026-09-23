#!/usr/bin/env python3
"""The fact ledger's gate: every declared question is still answerable.

🔴 READ `tests/facts.py`'s module docstring FIRST — it carries why this exists and,
more importantly, what it deliberately does NOT do. This file is only the runner.

🔴 A SKIP HERE IS REFUSED IN THE JOB THAT OWNS THE DEPENDENCY. The `tests` CI job
ships neither `go` nor `nix`, so facts needing one skip there — and a skip nobody
counts is indistinguishable from a pass. `CAIRN_FACTS_REQUIRE=go` (or `nix`, or
`all`) turns a skip into a FAILURE, and the `go` and `nix` jobs set it. Same shape
as `tests/test_go_client_ledgers.py`, and for the same stated reason.
"""

from __future__ import annotations

import os
import subprocess
from pathlib import Path

import pytest

from facts import (
    disposition,
    FACTS,
    UNANSWERABLE_HERE,
    Fact,
    FactUnanswerable,
    answer,
    toolchain_missing,
)

REPO = Path(__file__).resolve().parents[1]

#: Which toolchains this run REFUSES to skip. Read from the environment so the CI
#: job that owns a dependency can demand it without this file hard-coding a guess
#: about which runner image ships what.
_REQUIRE = {
    part.strip()
    for part in os.environ.get("CAIRN_FACTS_REQUIRE", "").split(",")
    if part.strip()
}


@pytest.mark.parametrize("fact", FACTS, ids=lambda f: f.fact_id)
def test_every_declared_fact_is_still_answerable(fact: Fact) -> None:
    """The command answers: exit 0, and NON-EMPTY output.

    🔴 BOTH HALVES, BECAUSE THE EXIT CODE ALONE IS THE WEAKER CLAIM. A command that
    exits 0 and prints nothing is exactly what a harness wired to nothing produces,
    and it reads as success to anything that only checks the status.
    """
    verdict, why = disposition(
        fact, missing=toolchain_missing(fact), require=_REQUIRE
    )
    if verdict == "fail":
        pytest.fail(f"{why} (CAIRN_FACTS_REQUIRE={os.environ.get('CAIRN_FACTS_REQUIRE')})")
    if verdict == "skip":
        pytest.skip(why)
    answer(fact, repo=str(REPO))


def _fact_needing(need: str) -> Fact:
    """A throwaway fact in one tier.

    🔴 THE FACT'S TIER AND THE MISSING TOOL MUST AGREE, AND THE FIRST DRAFT OF THIS
    CONTROL REUSED `FACTS[0]` WITHOUT CHECKING. That fact needs `nix`; the case
    passed `missing="go"`, so the two disagreed, `skip` was the CORRECT answer, and
    the control failed while the code under test was right. A fixture whose
    constants make the guarded branch unreachable measures nothing — the case
    exists to drive the tier MATCH, so it has to build the match.
    """
    return Fact(
        fact_id=f"control-needs-{need}",
        question="control",
        command=["true"],
        needs=need,
        replaces="control",
    )


@pytest.mark.parametrize(
    "need, missing, require, expected",
    [
        ("go", None, set(), "run"),
        ("go", None, {"go"}, "run"),
        ("go", "go", set(), "skip"),
        ("go", "go", {"nix"}, "skip"),
        ("go", "go", {"go"}, "fail"),
        ("go", "go", {"all"}, "fail"),
        ("nix", "nix", {"nix"}, "fail"),
        ("nix", "nix", {"all"}, "fail"),
        ("nix", "nix", {"go"}, "skip"),
    ],
)
def test_the_REFUSE_ON_SKIP_decision(need, missing, require, expected) -> None:
    """CONTROL — a skip becomes a FAILURE in the tier that owns the dependency.

    🔴 THIS IS THE HALF A GREEN RUN ON A DEVELOPER MACHINE CANNOT SHOW. Here both
    `go` and `nix` are on PATH, so every fact RUNS and the skip path is never
    taken — exactly the condition under which a broken refuse-on-skip would sit
    unnoticed until the one CI job depending on it silently passed.

    ⚠ It drives the PURE decision rather than the environment. `facts.disposition`
    records why: the first attempt hid `go` by filtering `PATH` and killed the
    interpreter's own loader, so the run died for a reason unrelated to the logic.

    🔴 EACH TIER IS DRIVEN IN BOTH DIRECTIONS — required and not-required — because
    a `disposition` that ignored `require` entirely and always returned `skip`
    would pass every case that expects `skip`. The `fail` rows are what separate it.
    """
    fact = _fact_needing(need)
    verdict, why = disposition(fact, missing=missing, require=require)
    assert verdict == expected, (
        f"{need=} {missing=} {require=} -> {verdict}, want {expected}"
    )
    if expected == "run":
        assert why == ""
    else:
        assert missing in why


def test_the_runner_REFUSES_a_command_that_cannot_run() -> None:
    """NEGATIVE CONTROL — the runner goes red on a broken command.

    🔴 WITHOUT THIS, A GREEN LEDGER IS A CLAIM ABOUT THE LEDGER AND NOT ABOUT THE
    TREE. A runner that cannot fail certifies everything, and this one's whole
    output is "the questions are still answerable".
    """
    broken = Fact(
        fact_id="control-nonexistent-command",
        question="control",
        command=["git", "cat-file", "-e", "0000000000000000000000000000000000000000"],
        needs="nothing",
        replaces="control",
    )
    with pytest.raises(FactUnanswerable) as caught:
        answer(broken, repo=str(REPO))
    assert "exited" in str(caught.value)


def test_the_runner_REFUSES_a_command_that_answers_NOTHING() -> None:
    """NEGATIVE CONTROL — the SECOND failure mode, which the first cannot show.

    🔴 A ZERO EXIT WITH EMPTY OUTPUT IS THE ONE THAT READS AS SUCCESS. The control
    above proves the runner notices a crash; this proves it notices silence. They
    are different claims and a runner can pass one while failing the other — which
    is why the repo's harnesses report a PAIR rather than a single green.
    """
    silent = Fact(
        fact_id="control-silent-command",
        question="control",
        command=["git", "hash-object", "--stdin-paths"],
        needs="nothing",
        replaces="control",
    )
    with pytest.raises(FactUnanswerable) as caught:
        answer(silent, repo=str(REPO))
    assert "printed NOTHING" in str(caught.value)


def test_the_runner_ACCEPTS_a_command_that_answers() -> None:
    """POSITIVE CONTROL — it is not simply refusing everything.

    🔴 THE TWO REFUSALS ABOVE ARE SATISFIED BY A RUNNER THAT REFUSES UNCONDITIONALLY.
    That shape passes both and measures nothing, so the narrowness has its own case.
    """
    fine = Fact(
        fact_id="control-working-command",
        question="control",
        command=["git", "rev-parse", "--show-toplevel"],
        needs="nothing",
        replaces="control",
    )
    assert answer(fine, repo=str(REPO)).strip()


def test_no_fact_command_writes_to_the_tree() -> None:
    """A gate that edits what it measures cannot be trusted about either.

    ⚠ THIS IS A SPELLED CHECK AND SAYS SO. It refuses a small set of known mutating
    verbs; it cannot prove an arbitrary command is read-only. It is here to catch
    the careless row, not an adversarial one — and the honest statement of a
    guard's reach belongs beside the guard, not in a commit message.
    """
    mutating = {"write", "put", "create", "append", "commit", "push", "add", "rm", "mv"}
    for fact in FACTS:
        tokens = {tok.lstrip("-") for tok in fact.command}
        overlap = tokens & mutating
        assert not overlap, (
            f"{fact.fact_id}'s command contains {sorted(overlap)}, which may write. "
            f"Fact commands must be read-only: {' '.join(fact.command)}"
        )


def test_every_unanswerable_fact_names_who_CAN_answer_it() -> None:
    """A declared gap without an owner is just an omission with extra words.

    The value of `UNANSWERABLE_HERE` is that it distinguishes "nobody thought of
    this" from "this was considered and the repo genuinely cannot answer it". That
    distinction only survives if each row says where the answer does live.
    """
    assert UNANSWERABLE_HERE, (
        "the unanswerable list is empty. At least one fact — which pod the cluster "
        "pulls — is not answerable from this repository, and deleting the row does "
        "not make it answerable."
    )
    for fact_id, reason in UNANSWERABLE_HERE:
        assert len(reason) > 80, f"{fact_id}: reason too thin to be useful"
        assert any(
            word in reason.lower()
            for word in ("operator", "repository", "archive", "judgement")
        ), (
            f"{fact_id}: the reason must name WHO or WHAT can answer it instead. "
            f"A gap with no owner is an omission."
        )


def test_the_ledger_and_the_tree_agree_about_the_git_dir() -> None:
    """The runner runs commands in the REPO, not in whatever cwd pytest inherited.

    🔴 A COMMAND RUN IN THE WRONG TREE ANSWERS A DIFFERENT QUESTION AND LOOKS
    IDENTICAL DOING IT. This repo has already measured that shape twice this arc —
    an agent worktree of the wrong repo, and a `nix build` pointed at a checkout
    holding none of the caller's changes.
    """
    top = subprocess.run(
        ["git", "rev-parse", "--show-toplevel"],
        cwd=str(REPO), capture_output=True, text=True, check=True,
    ).stdout.strip()
    assert Path(top).resolve() == REPO, (
        f"the runner would execute fact commands in {top}, not {REPO}"
    )
