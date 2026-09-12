#!/usr/bin/env python3
"""The vocabulary a cairn READER and any WRITER must agree on.

🔴 THIS MODULE EXISTS SO THERE IS ONE SPELLING, NOT TWO. A reader and a writer
that disagree about which scope directory they mean is a silent, total failure:
the writer accrues entries under one name and the reader surfaces an empty scope
under another, which renders as "nothing recorded yet" and is indistinguishable
from the ordinary case. Everything here is imported by both halves rather than
re-typed in each.

It holds the scope-derivation rule, the per-host caveat, the entry headings, and
the error taxonomy. It deliberately holds no I/O policy and no store layout: a
writer decides where it writes, and this module only decides what things are
called.
"""
from __future__ import annotations

import os
import subprocess
from pathlib import Path
from typing import Sequence

from host_identity import this_host
from subsystem_resolver import NUANCE_HEADING, POINTERS_HEADING, normalize_ref

__all__ = [
    "CairnError", "GitError", "RepoPathMissingError", "StoreMissingError",
    "BULLET_TEXT_MAX", "SHAPE_HEADINGS", "STORE_IS_PER_HOST",
    "derive_scope", "repo_path_missing_message", "scope_for_repo",
    "store_host", "store_host_line",
]


# --------------------------------------------------------------------------
# Errors
# --------------------------------------------------------------------------

class CairnError(Exception):
    """Base for every error this module raises."""


#: Kept as an alias because the writer half historically raised `TouchError` and
#: callers match on the class. Renaming without an alias would be a silent
#: behaviour change for any `except` clause naming the old spelling.
TouchError = CairnError


class GitError(CairnError):
    """A git invocation failed. Sentinel: 'git command failed'.

    🔴 `stderr` IS CARRIED AS AN ATTRIBUTE, NOT ONLY INSIDE THE MESSAGE, so a
    WRAPPING error can quote git's own words WITHOUT embedding this class's
    sentinel in its own text. Without that, a wrapper's message carries two
    sentinels at once and "which guard fired" stops being measurable.
    """

    def __init__(self, message: str, *, stderr: str = "") -> None:
        super().__init__(message)
        self.stderr = stderr


class StoreMissingError(CairnError):
    """The store root does not exist. Sentinel: 'store root not found'."""


class RepoPathMissingError(CairnError):
    """`--repo` names something that is not a directory.

    Sentinels: 'repo path does not exist' when nothing is there, 'repo path is
    not a directory' when something is. TWO, because they are two mistakes with
    two next moves — see `repo_path_missing_message`.

    🔴 ITS OWN SENTINEL, NOT `git command failed`. "The repo path does not exist"
    is a first-class READING the caller can act on, while `GitError`'s sentinel
    is a true statement about the subprocess and a useless one about the
    argument. `--repo` takes a PATH resolved against the cwd, so a bare repo NAME
    silently becomes `$PWD/<name>` — and a raw git error names neither that rule
    nor the way out. It matters more than wording usually does: this is the
    store's primary read surface, and a prescribed first command that answers
    with a git internals dump is how a store goes unread.
    """


# --------------------------------------------------------------------------
# Entry shape
# --------------------------------------------------------------------------

#: The two headings every entry is built from. Re-exported here so a writer does
#: not import them from the renderer, and pinned as a tuple so a reader can
#: assert the set rather than two separate strings.
SHAPE_HEADINGS: tuple[str, ...] = (POINTERS_HEADING, NUANCE_HEADING)

#: The longest `text` an append may carry, in characters.
#:
#: 🔴 WHAT THIS IS **NOT**, BECAUSE BOTH READINGS ARE AVAILABLE AND BOTH ARE
#: WRONG — AND THE VALUE SAT HERE UNJUSTIFIED LONG ENOUGH THAT "RAISE IT" READ AS
#: THE OBVIOUS FIX:
#:
#:   * NOT an integrity constraint. Nothing about a long `text` can corrupt the
#:     bullet or its attribution trailer; that hazard is one bullet becoming two,
#:     or a trailer that reads as somebody else's, and it is closed by the
#:     line-break and character-category predicates in the server — measured
#:     failures, each with its own comment. Length breaks nothing.
#:   * NOT a resource bound. The request body is already capped (1 MiB) and
#:     bounded in time by the server's drain deadline, and this value is three
#:     orders of magnitude below that cap. Deleting it would not widen the
#:     server's exposure by one byte.
#:   * NOT a bound on what a READ costs, which is the plausible one. A recall
#:     prints an entry's nuance bullets in FULL and the bullet COUNT is
#:     uncapped — measured, one entry holds 111 — so worst-case read cost is
#:     count × this value and the count is the free variable. Capping the text
#:     cannot bound a product whose other factor is unbounded.
#:
#: 🔴 WHAT IT IS: an EDITORIAL tripwire on the tail, and the number is the p99 of
#: the corpus the store already holds. Measured over 2,211 dated bullets: median
#: 629, p90 1,418, p95 1,746, **p99 2,049**, longest 4,015. So this value fires
#: on roughly the densest 1% and on nothing else, and what it says to a writer is
#: "a bullet this long is a document, and a document belongs in an entry's prose
#: or its own entry, not in one line of work-history". That is a curation
#: judgement rather than a safety property — which is exactly why it has to be
#: WRITTEN DOWN as one. An unexplained constant invites being raised to a second
#: unexplained constant.
#:
#: ⚠ THE UNIT IS THE CALLER'S `text`, AND MEASURING THE STORED BULLET INSTEAD
#: OVERSTATES THE TAIL BY ~2×. The server prepends `- <date>: ` and appends the
#: attribution trailer, so a stored line is longer than the `text` that produced
#: it: over this same corpus, 27 bullets (1.2%) exceed 2,000 characters measured
#: as `text`, against 48 (2.2%) measured as stored bytes. A reading of "how many
#: existing bullets would this cap reject" must use the former; the latter counts
#: characters no caller ever sent.
#:
#: Existing over-cap bullets are GRANDFATHERED, deliberately and permanently:
#: this value gates WRITES only, nothing in the API rewrites a bullet that is
#: already stored, and those bullets are the densest records in the store.
#: Shortening them to satisfy a constant would be the cap deciding the content.
#:
#: To justify a DIFFERENT number, the thing to measure is not the corpus again —
#: it is whether a reader's comprehension actually falls off somewhere, or a
#: per-entry read budget with the bullet count capped alongside. Re-measuring the
#: same distribution can only ever re-derive a percentile.
BULLET_TEXT_MAX = 2000

#: 🔴 PRINTED UNDER EVERY `store:` LINE, AND IT IS LOAD-BEARING. A local cache is
#: per-host: two machines can hold the same scope with different entries in it,
#: and nothing reconciles them automatically. A verdict printed without naming
#: the host it read is therefore not a smaller claim than the truth — it is a
#: different, false one ("the store has no X" instead of "this disk has no X").
#:
#: ⚠ THIS SENTENCE BECOMES INCOMPLETE THE MOMENT A CLIENT IS POINTED AT MORE
#: THAN ONE INSTANCE. With several instances configured, an absence is also
#: explainable by "that scope lives on another instance", and a reader told only
#: that the store is per-host will draw the wrong conclusion. Whoever adds
#: multi-instance routing must rewrite this string in the same change.
STORE_IS_PER_HOST = (
    "the store is PER-HOST and unreplicated; this run read THIS machine's disk "
    "and consulted no other"
)


def store_host() -> str:
    """THIS machine's identity — the ONE call site of `host_identity.this_host`.

    🔴 A SINGLE SEAM FOR EVERY CONSUMER. Callers import THIS rather than
    `this_host` itself, so the name is looked up in this module's globals
    wherever it is called from: one injection point makes reader and writer
    agree, and a test needing byte-stable output patches one thing instead of
    two that can drift apart.
    """
    return this_host()


def store_host_line(indent: str = "  ") -> str:
    """`host: <id>  (<the per-host caveat>)` — printed under every `store:` line."""
    return f"{indent}host: {store_host()}  ({STORE_IS_PER_HOST})"


# --------------------------------------------------------------------------
# Scope derivation
# --------------------------------------------------------------------------

def _git(repo: Path, args: Sequence[str]) -> str:
    argv = ["git", "-C", str(repo), *args]
    env = dict(os.environ)
    # Read-only invocations must not take the index lock: another process in the
    # same checkout is a normal case, and a helper that can block someone else's
    # commit is not read-only in the way that matters.
    env["GIT_OPTIONAL_LOCKS"] = "0"
    proc = subprocess.run(argv, capture_output=True, text=True, env=env)
    if proc.returncode != 0:
        raise GitError(
            f"git command failed ({' '.join(argv)}): exit {proc.returncode}: "
            f"{proc.stderr.strip() or '(no stderr)'}",
            stderr=proc.stderr.strip(),
        )
    return proc.stdout


def _toplevel(repo: str | Path) -> Path:
    """The repo ROOT for a directory that may be anywhere inside it. Runs git.

    🔴 ONE FRAME, ONE CALL SITE. Open-coding this is how two callers end up with
    two different path frames in one path set: `diff`/`diff-tree` are always
    repo-root-relative while `ls-files --others` is cwd-relative AND cwd-scoped,
    so a caller passing a subdirectory gets components both manufactured and lost.
    """
    return Path(_git(Path(repo), ["rev-parse", "--show-toplevel"]).strip())


def derive_scope(repo_root: str | Path, git_common_dir: str | Path) -> str:
    """The store scope for a repo, normalized. Worktree-stable.

    `git_common_dir` is `git rev-parse --path-format=absolute --git-common-dir`:
    for BOTH a base clone and any worktree of it that is the base clone's `.git`,
    so its parent is the repo everyone means. `--show-toplevel` is not used for
    this because in a worktree it is the worktree's own directory.

    Fallback: when the common dir is not literally named `.git` — a bare repo, or
    a submodule whose common dir is `<super>/.git/modules/<name>` — the parent
    basename would be meaningless (`modules`), so the repo root's basename is
    used instead. Stated because the fallback is otherwise silent.
    """
    common = Path(git_common_dir)
    if common.name == ".git":
        return normalize_ref(common.parent.name)
    return normalize_ref(Path(repo_root).name)


def repo_path_missing_message(
    given: str | Path | None,
    resolved: str | Path,
    *,
    store_root: str | Path | None = None,
) -> str:
    """The ONE spelling of "that `--repo` value is not a directory". READ-ONLY.

    🔴 ONE PLACE, EVERY CLI — the same reason the scope RULE lives in one
    function: a reader and a writer that disagree here send two operators to two
    different remedies for one mistake.

    `given` is the RAW value the caller typed and `resolved` is what it became.
    Both are printed when they differ, because the cwd-join is the whole defect:
    seeing only the resolved path leaves "where did that prefix come from?"
    unanswered, and that is the question the reader actually has. `given` is None
    for internal callers that never had a raw string.
    """
    given_s = str(given) if given is not None else str(resolved)
    resolved_s = str(resolved)
    # 🔴 TWO SPELLINGS, because they are two different mistakes. A path that is
    # absent and a path that exists as a FILE need different next moves, and
    # telling someone their `notes.md` "does not exist" while they are looking at
    # it is the kind of confidently-wrong line that makes a reader distrust the
    # rest of the message.
    lead = (
        "repo path does not exist" if not Path(resolved_s).exists()
        else "repo path is not a directory"
    )
    # 🔴 THE PARENTHETICAL IS ABOUT A *RELATIVE* INPUT — not about the two strings
    # merely differing. They also differ when an ABSOLUTE path resolves through a
    # SYMLINK, and telling that caller their absolute path "is resolved against
    # the current directory" is a false statement in the one message whose entire
    # job is to be accurate about their mistake.
    joined_from_cwd = not Path(given_s).is_absolute()
    if given_s == resolved_s:
        head = f"{lead}: '{resolved_s}'."
    elif joined_from_cwd:
        head = (
            f"{lead}: '{given_s}' → '{resolved_s}' "
            f"(a bare name is resolved against the current directory)."
        )
    else:
        head = f"{lead}: '{given_s}' → '{resolved_s}'."
    remedy = (
        "--repo takes a PATH, not a repo NAME. Pass an absolute path, or "
        "--scope <name>, which names the store directory directly and runs no "
        "git at all."
    )
    hint = ""
    if store_root is not None:
        candidate = normalize_ref(Path(given_s).name)
        if (Path(store_root) / candidate).is_dir():
            hint = f" Did you mean --scope {candidate}?"
    return f"{head} {remedy}{hint}"


def scope_for_repo(
    repo: str | Path,
    *,
    store_root: str | Path | None = None,
    given: str | Path | None = None,
) -> str:
    """Ask git where `repo` really lives, then `derive_scope` it. Runs git.

    🔴 ONE SCOPE-DERIVATION CALL SITE, not two. The READ half and any WRITE half
    need exactly the same answer; see this module's docstring for what disagreeing
    costs.

    🔴 THE NON-DIRECTORY CASE IS CHECKED BEFORE GIT RUNS, and belongs HERE for the
    same reason the scope rule does — this is the one seam both halves cross, so a
    guard placed in the reader alone would leave the writer answering the identical
    mistake with an identical git dump. `store_root` and `given` exist only for the
    message: `given` carries the raw pre-`resolve()` string so the cwd-join is
    visible, and `store_root` is what lets the refusal name the scope the caller
    probably wanted. Both default to None so every call site keeps working, at a
    slightly poorer message rather than a TypeError.
    """
    repo = Path(repo)
    if not repo.is_dir():
        raise RepoPathMissingError(
            repo_path_missing_message(given, repo, store_root=store_root)
        )
    common = _git(
        repo, ["rev-parse", "--path-format=absolute", "--git-common-dir"]
    ).strip()
    return derive_scope(_toplevel(repo), common)
