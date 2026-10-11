#!/usr/bin/env python3
"""Map a change to the CI lanes it must pay for — the ONE place `ci.yml`'s path tiering lives.

`.github/workflows/ci.yml` used to run every job on every event, so a commit touching only
`claudedocs/` paid for the parity gate, the dual-run gate, every nix build and the browser
walk. This module is the classifier the workflow's `changes` job runs; every gated job and
step reads its outputs and nothing else. The path lists are declared ONCE, here, and
`tests/test_ci_paths.py` pins them against the files they claim to describe (`flake.nix`'s
source filters, the `uiaudit` job's own commands) so a list cannot quietly fall behind.

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
skips: `parity`, `dualrun`, `nix`, `uiaudit`, `pgtest`, and the `go` job's conformance and
end-to-end steps.

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

#: A change here can change WHAT runs, so classifying it with the rules it may be rewriting
#: is circular: the workflow files themselves and this classifier.
ALWAYS_FULL = (".github/workflows/", "tests/ci_paths.py")

#: Everything a `flake.nix` derivation or check READS. Derived from `flake.nix`: the `onlyCode`,
#: `onlyGo` and `uiauditSrc` allowlists plus every `./path` literal outside them. The flake's
#: sources are ALLOWLISTS, so a path not in them cannot reach a build — which is what makes a
#: closed list here exact rather than a guess. `test_ci_paths.py` re-derives the flake's set
#: on every run and refuses a member this list does not cover.
NIX = (
    "flake.nix",
    "flake.lock",
    "cairn",
    "lib/",
    "server/server.py",
    "go.mod",
    "go.sum",
    "cmd/",
    "internal/",
    "uiaudit/",
    "tests/conformance/requests.json",
    "tests/leakscan.py",
    "tests/reader_fixtures.py",
    "tests/ui_image_session_dir_check.py",
)

#: Everything the `uiaudit` job BUILDS or RUNS. Derived from that job's steps: it builds
#: `./cmd/cairn-ui` from the root module (so `go.mod`/`go.sum` and that binary's whole import
#: graph), and runs `uiaudit/` (its tests, `run.sh`, `pwa_check.sh`) — whose `boot.go` builds
#: the fixture world with `tests/reader_fixtures.py`, which imports `lib/` and parses
#: `server/server.py`. ⚠ `internal/` WHOLE RATHER THAN THE IMPORT GRAPH, DELIBERATELY: every
#: non-main package in this module lives under `internal/` (measured with `go list ./...`),
#: so the prefix is a superset BY CONSTRUCTION, where a per-package list would go stale the
#: first time `cmd/cairn-ui` gained an import — in the skip direction. ⚠ `flake.nix` and
#: `flake.lock` are NOT here: this job installs Go and chromium itself and reads neither.
UIAUDIT = (
    "go.mod",
    "go.sum",
    "cmd/cairn-ui/",
    "internal/",
    "uiaudit/",
    "tests/reader_fixtures.py",
    "lib/",
    "server/server.py",
)

#: The lane flags the workflow reads, in the order they are written. `heavy` is everything
#: beyond leakscan, pytest and `go vet`/`go test`: the `parity`, `dualrun` and `pgtest` jobs and
#: the `go` job's conformance/end-to-end steps. `test_ci_paths.py` pins this set against the
#: `changes` job's declared outputs in BOTH directions.
LANES = ("heavy", "nix", "uiaudit")

ZERO_SHA = "0" * 40


@dataclass(frozen=True)
class Lanes:
    heavy: bool
    nix: bool
    uiaudit: bool
    reason: str

    def outputs(self) -> list[str]:
        return [f"{name}={'true' if getattr(self, name) else 'false'}" for name in LANES]


def full(reason: str) -> Lanes:
    return Lanes(heavy=True, nix=True, uiaudit=True, reason=f"FULL: {reason}")


def _matches(path: str, entries: Iterable[str]) -> bool:
    """An entry ending in `/` is a directory prefix; any other entry is an exact path."""
    return any(path.startswith(e) if e.endswith("/") else path == e for e in entries)


def classify(paths: Sequence[str] | None) -> Lanes:
    """The lanes a list of changed paths needs. `None` means "could not be determined"."""
    if paths is None:
        return full("the change list could not be determined")
    paths = [p.strip() for p in paths if p.strip()]
    if not paths:
        return full("an EMPTY change list is not evidence that nothing changed")
    hits = [p for p in paths if _matches(p, ALWAYS_FULL)]
    if hits:
        return full(f"{hits[0]} can change what runs")
    if all(_matches(p, DOCS_ONLY) for p in paths):
        return Lanes(heavy=False, nix=False, uiaudit=False,
                     reason=f"docs-only: {len(paths)} path(s), all under {DOCS_ONLY[0]}")
    return Lanes(
        heavy=True,
        nix=any(_matches(p, NIX) for p in paths),
        uiaudit=any(_matches(p, UIAUDIT) for p in paths),
        reason=f"{len(paths)} path(s) beyond the docs lane",
    )


Git = Callable[[Sequence[str]], str]


def _git(args: Sequence[str]) -> str:
    return subprocess.run(["git", *args], check=True, capture_output=True, text=True).stdout


def _diff(base: str, head: str, git: Git) -> list[str]:
    return git(["diff", "--name-only", "--no-renames", base, head]).splitlines()


def for_event(env: dict[str, str], git: Git = _git) -> Lanes:
    """The lanes for one workflow run, from the event fields the workflow passes in `env`.

    `--no-renames` on the diff: a rename then lists BOTH its old and new path, so moving a file
    OUT of a gated directory still counts as a change to that directory.
    """
    event = env.get("EVENT", "")
    if event in ("schedule", "workflow_dispatch", "merge_group"):
        return full(f"a {event} run always pays for every gate")
    if event == "push":
        before, after = env.get("PUSH_BEFORE", ""), env.get("PUSH_AFTER", "")
        if not after or not before or before == ZERO_SHA:
            return full("a push with no usable `before` (a new branch) has no base to diff")
        try:
            git(["merge-base", "--is-ancestor", before, after])
        except subprocess.CalledProcessError:
            return full("`before` is not an ancestor of `after` (a force-push)")
        lanes = classify(_diff(before, after, git))
        # 🔴 `main` IS STRICT FOR CODE: a docs-only push takes the docs lane (most of the cost
        # this tiering exists to remove was `claudedocs/` commits pushed straight to `main`), but
        # a push touching ANY other path pays for every gate, never the partial `nix`/`uiaudit`
        # lanes a pull request may take. The nightly full run is the net under the docs half.
        if env.get("REF") == "refs/heads/main" and lanes.heavy:
            return full("a push to main touching code pays for every gate")
        return lanes
    if event == "pull_request":
        base, head = env.get("PR_BASE", ""), env.get("PR_HEAD", "")
        if not base or not head or ZERO_SHA in (base, head):
            return full("a pull_request event without both a base and a head sha")
        merge_base = git(["merge-base", base, head]).strip()
        if not merge_base:
            return full("the base and head share no merge-base")
        return classify(_diff(merge_base, head, git))
    return full(f"unrecognised event {event!r}")


def safe_for_event(env: dict[str, str], git: Git = _git) -> Lanes:
    """`for_event`, with every exception answered FULL. This is the function the CLI calls."""
    try:
        return for_event(env, git)
    except Exception as exc:  # noqa: BLE001 — failing open IS the contract
        return full(f"classifier error ({type(exc).__name__}: {exc})")


def main(argv: Sequence[str] | None = None) -> int:
    ap = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    ap.add_argument("--github-output", help="append the lane flags to this file ($GITHUB_OUTPUT)")
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
