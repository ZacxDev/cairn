#!/usr/bin/env python3
"""Decide whether a change is docs-only — the ONE place `ci.yml`'s path tiering lives.

`.github/workflows/ci.yml` used to run every job on every event, so a commit touching only
`claudedocs/` paid for the parity gate, the dual-run gate, every nix build and the browser
walk. This module is the classifier the workflow's `changes` job runs; every gated job and
step reads its one output, `heavy`, and nothing else.

🔴 TWO LANES, NOT MORE. An earlier form (PR #225 before its round-0 review) also had `nix`
and `uiaudit` lanes, each derived from what that job reads. They were deleted because neither
can shorten a run: the ungated `go` and `tests` jobs run on every non-docs change, and with
the mutation batteries moved out of `go` the review's job times put `nix` (5-6 min) and
`uiaudit` (8-9 min) inside `go`'s ~10 — so a skip saves nothing on the critical path, while the
derivations added a way to skip `nix` WRONGLY. ⚠ Those times are the review's figures, not
re-measured here. So it is docs (`heavy=false`) or everything (`heavy=true`).

🔴 IT FAILS OPEN, AND EVERY ARM OF THAT IS DELIBERATE. A classifier that is wrong in the
"skip" direction ships an ungated change; one wrong in the "run" direction costs minutes.
So every state this module cannot vouch for answers FULL: an unknown event, a missing or
all-zero base (a new branch), a base that is not an ancestor of the head (a force-push), a
`git` that fails, an EMPTY change list (nothing to classify is not "nothing changed"), and
any exception at all. The workflow adds two more arms the module cannot provide for itself —
a job whose `changes` dependency did not SUCCEED runs, and an output that is absent runs —
and `tests/test_ci_paths.py` pins both expressions as whole strings.

🔴 ONLY `claudedocs/` IS DOCS. NOT `*.md`. `AGENTS.md` carries a byte budget a test
enforces, `tests/parity/README.md` and `tests/conformance/README.md` are read by tests, and
README prose is pinned in several places — a `.md` suffix rule would put those changes in
the lane that skips the gates reading them.

⚠ WHAT THE DOCS LANE STILL PAYS FOR, AND WHY: leakscan (ungated, always — `claudedocs/` is
committed prose in a public repository); the pytest suite, because tests READ `claudedocs/`
(`tests/test_base_clone_write_guard.py` pins `claudedocs/working-in-parallel.md`; the
public-IP scan walks it); and the `go` job's `vet`/`test`, which are cheap and are the only
Go tier left running — `internal/redact`'s live clean-damage sweep reads `claudedocs/`, though
only behind its opt-in flag, so the default `go test` is NOT measured to read it. What it
skips: `parity`, `dualrun`, `nix`, `uiaudit`, `uiaudit-touch`, `pgtest`, `mutants`,
`mutants-aggregate`, and the `go` job's conformance and end-to-end steps.

Usage (the workflow's form; the event fields arrive as environment variables):

    python3 tests/ci_paths.py --github-output "$GITHUB_OUTPUT"
    python3 tests/ci_paths.py --paths a/b.go claudedocs/x.md   # classify a literal list
"""
from __future__ import annotations

import argparse
import os
import subprocess
import sys
from dataclasses import dataclass
from typing import Callable, Iterable, Sequence

#: Every path under one of these prefixes is documentation no gate beyond the cheap ones reads.
DOCS_ONLY = ("claudedocs/",)

#: The flags the workflow reads. `heavy` is everything beyond leakscan, pytest and
#: `go vet`/`go test`. `test_ci_paths.py` pins this set against the `changes` job's declared
#: outputs in BOTH directions.
LANES = ("heavy",)

ZERO_SHA = "0" * 40


@dataclass(frozen=True)
class Lanes:
    heavy: bool
    reason: str

    def outputs(self) -> list[str]:
        return [f"{name}={'true' if getattr(self, name) else 'false'}" for name in LANES]


def full(reason: str) -> Lanes:
    return Lanes(heavy=True, reason=f"FULL: {reason}")


def _matches(path: str, entries: Iterable[str]) -> bool:
    """An entry ending in `/` is a directory prefix; any other entry is an exact path."""
    return any(path.startswith(e) if e.endswith("/") else path == e for e in entries)


def classify(paths: Sequence[str] | None) -> Lanes:
    """The lane a list of changed paths needs. `None` means "could not be determined"."""
    if paths is None:
        return full("the change list could not be determined")
    paths = [p.strip() for p in paths if p.strip()]
    if not paths:
        return full("an EMPTY change list is not evidence that nothing changed")
    # ⚠ NO SEPARATE "a workflow file or this classifier changed" RULE: with two lanes it is
    # subsumed — every such path is outside `claudedocs/`, so it already runs everything. A
    # mutation sweep scored the old rule SURVIVED (removing it changed no outcome) and it went.
    if all(_matches(p, DOCS_ONLY) for p in paths):
        return Lanes(heavy=False,
                     reason=f"docs-only: {len(paths)} path(s), all under {DOCS_ONLY[0]}")
    return full(f"{len(paths)} path(s), not all under {DOCS_ONLY[0]}")


Git = Callable[[Sequence[str]], str]


def _git(args: Sequence[str]) -> str:
    return subprocess.run(["git", *args], check=True, capture_output=True, text=True).stdout


def _diff(base: str, head: str, git: Git) -> list[str]:
    return git(["diff", "--name-only", "--no-renames", base, head]).splitlines()


def for_event(env: dict[str, str], git: Git = _git) -> Lanes:
    """The lane for one workflow run, from the event fields the workflow passes in `env`.

    A push is diffed `before..after` (the workflow only triggers on pushes to `main`, so a
    docs-only push to `main` takes the docs lane — most of the cost this exists to remove was
    `claudedocs/` commits pushed straight there). A pull request is diffed from its merge-base.
    `--no-renames` on the diff: a rename then lists BOTH its old and new path, so moving a file
    OUT of `claudedocs/` is not docs-only.
    """
    event = env.get("EVENT", "")
    if event == "workflow_dispatch":
        return full("a workflow_dispatch run always pays for every gate")
    if event == "push":
        before, after = env.get("PUSH_BEFORE", ""), env.get("PUSH_AFTER", "")
        if not after or not before or before == ZERO_SHA:
            return full("a push with no usable `before` (a new branch) has no base to diff")
        try:
            git(["merge-base", "--is-ancestor", before, after])
        except subprocess.CalledProcessError:
            return full("`before` is not an ancestor of `after` (a force-push)")
        return classify(_diff(before, after, git))
    if event == "pull_request":
        paths, why = pull_request_paths(env, git)
        return full(why) if paths is None else classify(paths)
    return full(f"unrecognised event {event!r}")


def pull_request_paths(env: dict[str, str], git: Git = _git) -> tuple[list[str] | None, str]:
    """A pull request's changed paths, diffed from its merge-base — or `None` and why not.

    The one place a PR's diff is computed: `for_event` above classifies it, and
    `tests/testlib/mutant_select.py` scopes the mutation batteries with it.
    """
    base, head = env.get("PR_BASE", ""), env.get("PR_HEAD", "")
    if not base or not head or ZERO_SHA in (base, head):
        return None, "a pull_request event without both a base and a head sha"
    merge_base = git(["merge-base", base, head]).strip()
    if not merge_base:
        return None, "the base and head share no merge-base"
    return _diff(merge_base, head, git), f"diffed {merge_base[:12]}..{head[:12]}"


def safe_for_event(env: dict[str, str], git: Git = _git) -> Lanes:
    """`for_event`, with every exception answered FULL. This is the function the CLI calls."""
    try:
        return for_event(env, git)
    except Exception as exc:  # noqa: BLE001 — failing open IS the contract
        return full(f"classifier error ({type(exc).__name__}: {exc})")


def main(argv: Sequence[str] | None = None) -> int:
    ap = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    ap.add_argument("--github-output", help="append the lane flag to this file ($GITHUB_OUTPUT)")
    ap.add_argument("--paths", nargs="*", help="classify this literal list instead of an event")
    args = ap.parse_args(argv)
    if args.paths is not None:
        lanes = classify(args.paths)
    else:
        lanes = safe_for_event(dict(os.environ))
    print(lanes.reason)
    for line in lanes.outputs():
        print(line)
    if args.github_output:
        with open(args.github_output, "a", encoding="utf-8") as fh:
            fh.write("".join(f"{line}\n" for line in lanes.outputs()))
    return 0


if __name__ == "__main__":
    sys.exit(main())
