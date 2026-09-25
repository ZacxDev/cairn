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
import re
import subprocess
from dataclasses import dataclass
from pathlib import Path
from typing import Iterable, Sequence

from host_identity import this_host
from subsystem_resolver import (
    NUANCE_HEADING,
    POINTERS_HEADING,
    TAKE,
    UNREACHABLE_MARKER,
    WHAT_HEADING,
    action_for,
    classify_path,
    extract_sections,
    line_mentions_marker,
    line_openness,
    normalize_ref,
    parse_journal_bullets,
    scan_headings,
)
# 🔴 THE LOADER'S OWN ACTION TABLE, IMPORTED PRIVATE RATHER THAN RE-DECIDED — see
# `_nuance_body`. The advisories must read EXACTLY the set of paths `load_index`
# reads: narrower and they print a zero over a file the loader counted, wider and
# they `open()` a fifo the loader refused. Either way the second copy of the rule
# is what drifts, so there is no second copy.
from subsystem_resolver import _LOADER_ENTRY_ACTIONS  # noqa: E402
# 🔴 THE READER'S OWN FENCE PREDICATE, IMPORTED PRIVATE RATHER THAN RE-SPELLED.
# `_is_fence` knows both ``` and ~~~; a hand-spelled copy that knew only the
# first would report a ~~~-fenced sample line as lost content, and the two
# scanners below would then disagree with `parse_journal_bullets`, which is the
# function that decides what a reader can actually see. Importing an underscore
# name across modules is deliberate here: a second copy of this predicate is the
# duplicated rule, and the Go port makes the same call (`store.IsFence`).
from subsystem_resolver import _is_fence  # noqa: E402

__all__ = [
    "CairnError", "GitError", "RepoPathMissingError", "StoreMissingError",
    "BULLET_TEXT_MAX", "SHAPE_HEADINGS", "STORE_IS_PER_HOST",
    "STORE_IS_ONE_INSTANCE",
    "DROPPED_LINE", "UNREACHABLE_MARKER",
    "SHAPE_ABSENT", "SHAPE_RENAMED", "SHAPE_DUPLICATED", "SHAPE_EMPTY",
    "SHAPE_INVENTORY_SHOWN",
    "DroppedLineFinding", "UnreachableMarkerFinding", "ShapeFinding", "OpenAction",
    "derive_scope", "repo_path_missing_message", "scope_for_repo",
    "store_caveat", "store_host", "store_host_line",
    "line_carries_marker", "scan_dropped_lines", "scan_unreachable_markers",
    "scan_entry_shape", "scan_open_actions",
    "validation_advisory_lines",
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

#: 🔴 PRINTED UNDER EVERY `store:` LINE, AND IT IS LOAD-BEARING. The CACHE is
#: per-host: two machines can hold the same scope with different entries in it
#: at any given instant, because each converges on the hosted store only when
#: `cairn sync` runs there. A verdict printed without naming the host it read is
#: therefore not a smaller claim than the truth — it is a different, false one
#: ("the store has no X" instead of "this disk has no X, as of its last sync").
#:
#: 🔴 THE FIRST CLAUSE USED TO READ "PER-HOST AND UNREPLICATED", AND THAT IS THE
#: DEFECT THIS WORDING EXISTS TO CORRECT. Since the Cairn cutover the hosted pod
#: is the canonical datastore and `~/.cache/subsystem-store` is a SYNCED
#: READ-THROUGH CACHE of it, so content written on one machine does reach the
#: other. Observed, not inferred from config: within one session that wrote
#: nothing, the pod's own snapshot moved `entry-files=232` -> `239` between two
#: reads about an hour apart. "Unreplicated" told readers an absence here was an
#: absence everywhere, and that false sentence propagated into a downstream
#: handoff doc before it was caught.
#:
#: The SECOND clause is untouched and still exactly true: the reader is offline
#: against the local cache and contacts no pod and no peer. What bounds it is
#: FRESHNESS, not isolation — an entry written elsewhere and not yet synced here
#: is invisible, and that is the honest caveat to print.
#:
#: 🔴 THE MULTI-INSTANCE CASE IS NOW HANDLED, AND IT IS HANDLED BY
#: `store_caveat`, NOT BY EDITING THIS STRING. The warning that used to sit here
#: said "whoever adds multi-instance routing must rewrite this string in the
#: same change", and the FIRST half of that is right: with several instances
#: configured an absence is also explainable by "that scope lives on another
#: instance", and a reader told only that the cache is per-host draws the wrong
#: conclusion. The second half — rewrite THIS constant — is what the code
#: measured wrong, and the reason is worth keeping because it is not obvious:
#:
#:   * this sentence is BYTE-MIRRORED across the tree, and the count is bigger
#:     than it looks. Enumerated over `git ls-files` at `38b358d`: **25 files,
#:     92 occurrences** — the Go port (`internal/hostid/hostid.go`), the reader
#:     fixture the Go renderer is compared against
#:     (`internal/report/testdata/reader_fixtures.json`, 64 of those
#:     occurrences), **20 of the 98** `tests/conformance/golden/*.json` (24),
#:     this file, `server/README.md` and `tests/test_subsystem_recall.py`. Two
#:     of those are regenerate-and-diff gates and one is prose in an operator
#:     README, which no gate covers at all.
#:
#:     ⚠ THIS COMMENT SAID "a four-place change" AND THAT WAS WRONG BY 2×, in
#:     the direction that makes the change look cheap: it counted the four
#:     mirror KINDS and read as a count of SITES. Re-derive rather than trusting
#:     the number — it moves whenever a golden is regenerated:
#:         git ls-files | xargs grep -c 'PER-HOST CACHE' 2>/dev/null | grep -v ':0$'
#:     (use a real `grep` binary; a `.gitignore`-aware wrapper is blind to
#:     generated trees, and `git ls-files` is what makes the population the
#:     TRACKED one rather than whatever is on disk);
#:   * and it would change the bytes of every SINGLE-instance recall, on every
#:     host, to warn about a second instance that does not exist there.
#:
#: So the caveat became a FUNCTION of the instance context: unchanged where
#: there is one instance (the server, the goldens, every existing host), and
#: extended — at the point of rendering — where there is more than one. The
#: clause below is the extension.
STORE_IS_PER_HOST = (
    "the store is read through a PER-HOST CACHE, only as fresh as its last sync; "
    "this run read THIS machine's disk and consulted no other"
)

#: The clause the caveat gains when this host is configured with more than one
#: instance. `{instance}` is the alias this run read.
#:
#: 🔴 IT NAMES THE INSTANCE, NOT JUST THE FACT OF ROUTING. "several instances
#: exist" leaves the reader with the same question they started with; the alias
#: is what makes an absence actionable — it says which store was consulted, so
#: "look on the other one" is a command they can type.
STORE_IS_ONE_INSTANCE = (
    "and this run read the `{instance}` instance ONLY — with more than one "
    "instance configured, an absence here is also explainable by the scope "
    "living on another instance, so it is NOT an absence from the fleet"
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


def store_caveat(instance: str | None = None) -> str:
    """The per-host caveat, extended when this host reads more than one instance.

    🔴 `None` IS THE SINGLE-INSTANCE CASE AND MUST STAY BYTE-IDENTICAL. The
    server renders with no instance context, and so does every client on a host
    with one instance configured; both produce exactly the sentence this
    module's readers, its Go port and two generated corpora already agree on.
    A caller passes an alias only when there IS more than one place the answer
    could have come from.
    """
    if instance is None:
        return STORE_IS_PER_HOST
    return f"{STORE_IS_PER_HOST}, {STORE_IS_ONE_INSTANCE.format(instance=instance)}"


def store_host_line(indent: str = "  ", *, instance: str | None = None) -> str:
    """`host: <id>  (<the per-host caveat>)` — printed under every `store:` line."""
    return f"{indent}host: {store_host()}  ({store_caveat(instance)})"


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


# --------------------------------------------------------------------------
# The WRITE-PROTOCOL advisories: content a reader cannot reach
# --------------------------------------------------------------------------
#
# 🔴 THESE TWO CHECKS ARE WHY `validate` IS THE POST-WRITE CHECK AND NOT ONLY A
# PARSE CHECK. The count line above them answers "would the loader accept these
# files?", and a file can pass that while holding text NO reader will ever
# surface. `dropped lines:` is the half that means content is ALREADY LOST.
#
# 🔴 NEITHER MOVES THE VERDICT, AND THAT IS NOT TIMIDITY. `validate` answers one
# question and the write protocol branches on its EXIT CODE to mean "write
# NOTHING". Failing here would stop a session recording anything into an entry
# whose only defect is that an OLDER write lost a line — which makes the store
# lossier, not safer. They are reported loudly and change no code.

DROPPED_LINE = "dropped-line"
"""The reason token for a nuance line that reaches NO bullet.

A population of its own, and the one furthest from every other: the openness
shapes are all readings OF a bullet, so each is about content a reader can at
least see. This is content the file HOLDS and no reader can reach at all —
`parse_journal_bullets` drops text that precedes the first bullet, so those lines
are in the entry, in the store, in the backup, and in no consumer's output.
"""


@dataclass(frozen=True)
class UnreachableMarkerFinding:
    """One bullet carrying a correctly-spelled marker where no parser looks.

    🔴 DELIBERATELY NOT AN OPENNESS POPULATION. `JournalBullet.openness_population`
    partitions bullets by a reading of their OPENING line; this is a fact about
    lines 2..n, and the bullet it is about is usually `none` — a bullet that
    declared nothing. Carried in its own field, counted in its own key and
    rendered in its own block, so there is no place it could be added to a
    near-miss total: a near-miss is a marker MIS-SPELLED where the parser looks
    and is fixed by editing that line, this is a marker spelled CORRECTLY where
    the parser never looks and is fixed by promoting it to a bullet of its own.
    One count covering both would send half the readers to the wrong remedy.
    """

    filename: str
    bullet_first_line: str
    """The bullet's OPENING line — what a reader will search the file for."""

    offset: int
    """1-based line index WITHIN the bullet. Always >= 2."""

    line: str
    """The continuation line carrying the marker, verbatim."""

    openness: str
    """`open` | `resolved` — what it would have declared at a bullet's head."""


@dataclass(frozen=True)
class DroppedLineFinding:
    """One `## Nuance / work-history` line that belongs to no parsed bullet.

    🔴 THE SHAPE, AND WHY IT IS NOT `UnreachableMarkerFinding`. That one is a
    marker on a line that IS inside a bullet — the bullet is read, the marker is
    not. This is a line inside NO bullet: nothing about it is read, marker or
    otherwise. The remedies differ for the same reason the populations do — an
    unreachable marker is fixed by PROMOTING one line, a dropped line by
    restoring the bullet opening that used to sit above it.

    🔴 MEASURED IN THE FIELD, NOT HYPOTHETICAL. Over every committed version of
    every entry file in a live store of several hundred entries, seven versions
    carried dropped lines — two entries whose whole bullet block had been
    indented, each broken for days. One of them held a `OPEN:` that raised no
    badge for the whole of that window, and would still raise none, because every
    marker reader here anchors at position 0; see `line_carries_marker`.
    """

    filename: str
    offset: int
    """1-based line index within the `## Nuance / work-history` section body."""

    line: str
    """The dropped line, verbatim."""

    carries_marker: bool = False
    """The line looks like it declared `OPEN:`/`RESOLVED:`.

    Recorded because it changes the URGENCY and nothing else: a dropped line is
    lost content either way, but a dropped DECLARATION is an open action the
    store is actively failing to report. It is never counted as an open action —
    the bullet it belonged to no longer exists, so there is nothing to declare
    it ON.
    """


def line_carries_marker(line: str) -> bool:
    """Does this dropped line carry an `OPEN:`/`RESOLVED:` declaration?

    🔴 IT RESTATES NO GRAMMAR. It asks `line_openness` — which runs the same
    `_bullet_openness` that `parse_journal_bullets` calls for a bullet's line 1 —
    and falls back to `line_mentions_marker` for a declaration sitting mid-line.
    A hand-spelled copy could not stay in step with the real pattern: the
    pre-extraction original WAS one, and measured against all seven historical dropped-line
    blobs in a live store it was `False` on every one of them, including the
    `OPEN:` this feature's own motivating incident cites — while returning True
    for the prose `resolved upstream in 1.2.3.`.

    ⚠ `line_mentions_marker` IS THE ONLY PLACE IN EITHER CLIENT THAT LOOKS FOR A
    MARKER MID-LINE, and that is deliberate rather than a gap elsewhere: every
    marker READER anchors at position 0, so a mid-line `OPEN:` declares nothing
    anywhere and did not declare anything while its bullet was intact either.
    Widening a reader to match would make this one flag disagree with every
    other surface. It may ONLY rank urgency — a dropped line is a finding on its
    own and this flag never gates reporting, so a miss costs a word of emphasis,
    never a silent pass.
    """
    openness, _sha = line_openness(line)
    return openness is not None or line_mentions_marker(line)


def _scanner_reads_path(path: Path) -> bool:
    """Will the two advisory scanners OPEN this path?

    🔴 THE GATE AND THE DENOMINATOR, SPELLED ONCE, BECAUSE AS TWO THINGS THEY
    DISAGREED. `_nuance_body` has always asked the loader's own table before
    `open()`; the printed denominator was `len(entry_files_in(...))`, an
    unfiltered listing, so the advisories counted files they never read.
    """
    return action_for(classify_path(path), _LOADER_ENTRY_ACTIONS) == TAKE


def scanned_entry_count(paths: Iterable[str | Path]) -> int:
    """How many of `paths` the advisory scanners actually open.

    ⚠ NOT `len(paths)`, AND THE GAP IS THE POINT. A FIFO, a dangling symlink, a
    directory or a device inside a scope directory is listed as an entry file and
    REFUSED before `open()`. It belongs in the parse line's denominator — the
    loader really did try it, and really did report it malformed — and it must
    not appear in a denominator that claims text was read.
    """
    return sum(1 for path in paths if _scanner_reads_path(Path(path)))


def _nuance_body(path: Path) -> str | None:
    """The `## Nuance / work-history` body of one entry file, or None.

    🔴 THE PATH'S KIND IS DECIDED BEFORE IT IS OPENED, THROUGH THE LOADER'S OWN
    TABLE. `try: read_text` is NOT tolerance for a path that is not a regular
    file, and measuring it was the point: `except OSError` cannot catch a FIFO,
    because reading one does not raise — it BLOCKS until somebody writes, and
    `validate` never returns. A character device does not raise either; it
    streams until the process dies of `MemoryError`, which is not an `OSError`
    and propagates. Measured on this tree at the CLI, both clients, against a
    cache holding one such path: the FIFO wedged until a `timeout 20` killed it
    (124), and a symlink to `/dev/zero` under an address-space limit exited 1
    (Python, bare traceback) and 2 (Go, runtime out-of-memory) — in each case
    abandoning every scope after the bad one, where the same tree one commit
    earlier reported the file as malformed and exited 5.

    `load_index` never had that exposure: it refuses `other`, `link-to-other`,
    `broken-link`, `directory` and `link-to-dir` before `open()`, for exactly
    this reason (a fifo measured wedging a request thread for 25s). This gate is
    that same table — `_LOADER_ENTRY_ACTIONS`, not a fresh predicate — so the
    advisories read precisely the paths the loader reads and no others.

    ⚠ THAT SENTENCE IS ABOUT THE SCANNERS, AND IT USED TO BE READ AS COVERING THE
    PRINTED DENOMINATOR TOO. It did not: the clients passed the unfiltered
    listing to `validation_advisory_lines`, so a scope holding one entry beside a
    FIFO printed `0 across 2 entry file(s)` over a file nothing opened.
    `scanned_entry_count` is the denominator now, built from this same predicate.

    ⚠ AND THE UNMAPPED-KIND BRANCH DIVERGES FROM THE GO CLIENT. `action_for`
    RAISES `AssertionError` here, uncaught; `internal/store.nuanceBody` folds the
    same condition into "no nuance section". UNREACHABLE in both —
    `TestClassifierIsTotal` and Go's `TestTheActionTablesAreTotal` pin the kind
    set against the table two-way — so it is recorded rather than closed.

    ⚠ THE RESIDUAL IS THE LOADER'S RESIDUAL, AND IT IS NOT EMPTY. The kinds the
    table TAKES are `regular-file`, `link-to-file`, `indeterminate` and `absent`;
    on the last three the read can still fail, and `except OSError` is what turns
    that into "contributes nothing". What it does NOT cover is a REGULAR file
    whose read is unbounded — a `/proc` file reached through a symlink, say. That
    is the loader's own residual ledger (`load_index`), unchanged here and not
    widened: this function is now exactly as exposed as the reader beside it,
    which is the property worth having. It is not a claim that nothing can raise.

    Deliberately tolerant otherwise: a file with no nuance section yields None.
    Both scanners run BESIDE the parse check, never in front of it — a malformed
    file's own rejection is the finding that matters, and an advisory computed
    from its half-parsed body would bury it.

    ⚠ EACH ENTRY IS READ THREE TIMES PER `validate` — once by `load_index` and
    once by each scanner — AND THAT IS A DECISION, NOT AN OVERSIGHT. Measured on
    this tree over a synthetic cache of 300 entries carrying 30 bullets apiece:
    118 ms end to end for the oracle and 36 ms for the Go client, interpreter and
    process start included. Caching the body would put mutable state into two
    functions whose whole contract is READ-ONLY and independent, to save a
    fraction of a tenth of a second on a store an order of magnitude larger than
    any real one. The re-read also has one honest property a cache would remove:
    each scanner sees the file as it is when IT runs, so a body that changed
    mid-command cannot be reported under offsets taken from an earlier read.
    """
    text = _entry_text(path)
    if text is None:
        return None
    return extract_sections(text, (NUANCE_HEADING,)).get(NUANCE_HEADING) or None


def _entry_text(path: Path) -> str | None:
    """The WHOLE entry file, or None when no scanner may open this path.

    🔴 THE GATE AND THE READ, SPELLED ONCE FOR EVERY SCANNER. `_nuance_body`
    carried both inline until a scanner appeared that needs the whole file rather
    than one section (`scan_entry_shape`, which has to see headings the nuance
    body cannot contain). A second copy of the gate is the shape this module
    already refuses everywhere else: the FIFO and character-device hazards
    `_nuance_body` documents are properties of the OPEN, not of the section
    extraction, so a scanner that reads the file by a different route inherits
    none of the protection. Read `_nuance_body`'s docstring for the measurements
    — they apply verbatim to every caller of this function.
    """
    if not _scanner_reads_path(path):
        return None
    try:
        return path.read_text(encoding="utf-8", errors="replace")
    except OSError:
        return None


def scan_unreachable_markers(
    paths: Iterable[str | Path],
) -> tuple[UnreachableMarkerFinding, ...]:
    """Markers typed into a bullet's BODY, where the marker parser never reads.

    🔴 IT IS A SEPARATE WALK FROM ANY OPEN-ACTION SCAN BY CHOICE. Merging them
    would mean deciding what to do with a bullet that declared nothing on its
    opening line, and the answer there is "report it, but never as an open
    action" — a second population inside a function whose whole contract is a
    one-branch precedence.
    """
    out: list[UnreachableMarkerFinding] = []
    for p in paths:
        path = Path(p)
        body = _nuance_body(path)
        if body is None:
            continue
        for b in parse_journal_bullets(body):
            for m in b.unreachable_markers:
                out.append(
                    UnreachableMarkerFinding(
                        filename=path.name,
                        bullet_first_line=b.first_line,
                        offset=m.offset,
                        line=m.line,
                        openness=m.openness,
                    )
                )
    return tuple(out)


def scan_dropped_lines(
    paths: Iterable[str | Path],
) -> tuple[DroppedLineFinding, ...]:
    """Nuance lines present on disk that reach no bullet. READ-ONLY.

    🔴 IT ASKS THE READER'S OWN PARSER AND DERIVES NOTHING ITSELF. The set of
    reachable lines is taken from `parse_journal_bullets`' output rather than
    re-deduced from a bullet pattern here, so this check cannot drift away from
    what consumers actually see. Re-deriving it would be a duplicated predicate
    with the checker's worst failure mode: blessing lines the reader drops.

    🔴 WHAT IT STRUCTURALLY CANNOT SEE — THREE CASES, NOT ONE, and an earlier
    version of this paragraph named only the first. The printed zero names all
    three (`_dropped_lines_block`), because a caveat the operator never reads is
    not a caveat. Each is pinned by an invariant guard in
    `tests/test_entry_shape_validate.py` and its Go twin.

      1. ABSORBED TAIL. A bullet that loses its opening line while ANOTHER
         bullet sits above it is not detectable — `parse_journal_bullets` appends
         every non-bullet line to the bullet above, so the orphaned tail is
         absorbed into it and inherits its date, and the resulting file is
         BYTE-IDENTICAL to one where that bullet legitimately wrapped. No check
         can separate the two.
      2. UNCLOSED FENCE. `_is_fence` toggles, so an odd count leaves everything
         after the last fence marked fenced — and fenced lines are deliberately
         skipped as sample text (see below). A bullet swallowed that way yields
         NO dropped-line finding and NO bullet, so its `OPEN:` is reported by
         nothing at all. This is the one case where the zero is actively
         misleading rather than merely partial, which is why it is named first
         in the printed caveat's remedy order.
      3. DUPLICATED `## Nuance / work-history` HEADING. `extract_sections`
         concatenates same-named sections into one body, so an orphan under the
         SECOND heading is absorbed by a bullet from the FIRST, and the offsets
         this function reports index the concatenation rather than the file.

    What it DOES cover is the case where the drop is decidable — text before the
    first bullet, which includes every entry whose NEWEST bullet lost its head,
    the store being newest-first.
    """
    out: list[DroppedLineFinding] = []
    for p in paths:
        path = Path(p)
        body = _nuance_body(path)
        if body is None:
            continue
        # 🔴 REACHABILITY IS KEYED ON (OFFSET, LINE), NOT ON THE LINE ALONE. A
        # `set[str]` masks an orphan whose text is byte-identical to any line
        # inside any bullet of the same file — and a read-modify-write race,
        # which is the shape that produces a decapitated bullet in the first
        # place, is exactly what duplicates a block.
        reachable: set[tuple[int, str]] = set()
        lines = body.splitlines()
        cursor = 0
        for b in parse_journal_bullets(body):
            for ln in b.lines:
                # `parse_journal_bullets` preserves order and never reorders or
                # rewrites a line, so a forward scan re-attaches each bullet line
                # to its own offset. Trailing blanks it stripped are simply not
                # looked for; the skip below treats them as reachable anyway.
                while cursor < len(lines) and lines[cursor] != ln:
                    cursor += 1
                if cursor < len(lines):
                    reachable.add((cursor + 1, ln))
                    cursor += 1
        in_fence = False
        for i, line in enumerate(lines, 1):
            # 🔴 FENCES ARE SKIPPED, as in the sibling scanner and in
            # `parse_journal_bullets` itself. A fenced snippet BEFORE the first
            # bullet is sample text, and reporting it would hand the operator the
            # remedy "restore the bullet opening line" for content that never had
            # one.
            if _is_fence(line):
                in_fence = not in_fence
                continue
            if in_fence or not line.strip() or (i, line) in reachable:
                continue
            out.append(
                DroppedLineFinding(
                    filename=path.name,
                    offset=i,
                    line=line,
                    carries_marker=line_carries_marker(line),
                )
            )
    return tuple(out)


# --------------------------------------------------------------------------
# The entry's SHAPE, as opposed to whether its front matter parses
# --------------------------------------------------------------------------
#
# 🔴 WHY A SHAPE CHECK EXISTS AT ALL. `validate` returned `OK` at exit 0 for an
# entry with all three spine headings renamed, for one with no headings at all,
# and for one whose sections were all empty. Only a missing `service:` went red.
# So the spine every consumer depends on was enforced by NOTHING: it held because
# two skills happen to describe it and one template happens to emit it.
#
# 🔴 AND IT IS NOT COSMETIC. `subsystem_recall` computes an entry's bullet count
# and its `🔴 N OPEN` badge from `extract_sections(...)[NUANCE_HEADING]`, so a
# heading that is renamed — or given a trailing colon, or shifted off column 0 —
# yields an empty body, and the index row a resume consumes renders an entry with
# genuine open actions as a well-formed empty one. The reader names a missing
# heading ON READ; this catches the same thing ON WRITE, in the turn that wrote
# it, which is the difference between "someone eventually notices" and "the
# writer is told".
#
# 🔴 WHICH HEADINGS, AND WHY STILL NOT `## What it is`. The checked set is
# `SHAPE_HEADINGS` — the set whose absence makes a NUMBER wrong. `## What it is`
# IS read: `subsystem_recall` surfaces it in every printed body. But it feeds no
# bullet count and no index badge, so a missing one cannot turn a parse failure
# into a well-formed empty entry the way a missing `## Nuance / work-history`
# can. Flagging it would report a convention with no numeric consequence beside
# two whose consequence is measured, and a writer cannot tell those apart in a
# list. The reader names its absence under the entry's own body, where the person
# reading that entry is already looking.

SHAPE_ABSENT = "absent"
SHAPE_RENAMED = "renamed"
SHAPE_DUPLICATED = "duplicated"
SHAPE_EMPTY = "empty"

#: Cap on the heading inventory printed beside an ABSENT finding. A bound, not a
#: filter — the remainder is always counted in the line.
SHAPE_INVENTORY_SHOWN = 6


def _heading_key(heading: str) -> str:
    """The LOOSE form, used ONLY to pair a heading with the schema one it missed.

    🔴 IT NEVER ACCEPTS A HEADING. `extract_sections` matches the exact string and
    keeps doing so; folding `## Pointers` and `## pointers` together there would
    quietly widen what the store is allowed to look like, which is the opposite of
    what this check is for. This exists so the report can say "you wrote
    `## pointers`" instead of "the section is absent" — the difference between a
    finding a writer can act on in one edit and one that sends them looking for
    prose that is already on disk.

    Folds exactly three near-misses: the `#` level, the case and surrounding
    whitespace, and a trailing colon.
    """
    return re.sub(r"\s+", " ", heading.lstrip("#").strip().rstrip(":").strip()).lower()


@dataclass(frozen=True)
class ShapeFinding:
    """One way an entry's spine departs from the schema. FOUR DISJOINT KINDS.

    🔴 THEY ARE NEVER SUMMED, for the same reason `OpenAction`'s populations are
    not: they are different facts with different remedies. `renamed` and `absent`
    are the same missing section reported at different resolutions — the writer
    typed something we can show them, or they did not — and collapsing them would
    throw away the only half that is actionable. `duplicated` is a section that IS
    parsed, twice, silently merged. `empty` is a section present and unfilled,
    which `extract_sections` tracks separately from absent precisely so this can.

    🔴 `duplicated` AND `empty` ARE NOT MUTUALLY EXCLUSIVE, and neither branch in
    `scan_entry_shape` excludes the other: a heading written twice whose merged
    body is still blank is BOTH, and reporting one of the two would hand the
    writer half a remedy.
    """

    filename: str
    heading: str
    """The SCHEMA heading this finding is about, always — never the typo."""

    kind: str
    found: tuple[str, ...] = ()
    """For `renamed`, the near-miss heading(s) actually written; for `absent`, the
    file's whole heading inventory, so the writer sees what they wrote instead."""

    count: int = 0
    """For `duplicated`, how many times the exact heading appears."""


def scan_entry_shape(paths: Iterable[str | Path]) -> tuple[ShapeFinding, ...]:
    """Report each entry's spine against `SHAPE_HEADINGS`. READ-ONLY.

    Tolerant in exactly the way the sibling scanners are, and gated the same way:
    a path the loader's own table refuses is never opened (`_entry_text`), and a
    file that cannot be read contributes nothing rather than raising, because this
    runs BESIDE the parse check and never in front of it — a malformed file's own
    rejection is the finding that matters.

    🔴 IT REUSES `extract_sections` AND `scan_headings` AND PARSES NOTHING ITSELF.
    Both are views over one walker in `subsystem_resolver`, so this cannot come to
    a different conclusion about what a heading is than the reader does — which
    would be the worst possible defect in a checker whose entire job is to predict
    what the reader will see.
    """
    out: list[ShapeFinding] = []
    for p in paths:
        path = Path(p)
        text = _entry_text(path)
        if text is None:
            continue
        present = extract_sections(text, SHAPE_HEADINGS)
        headings = scan_headings(text)
        for h in SHAPE_HEADINGS:
            if h not in present:
                near = tuple(x for x in headings if _heading_key(x) == _heading_key(h))
                out.append(
                    ShapeFinding(
                        filename=path.name,
                        heading=h,
                        kind=SHAPE_RENAMED if near else SHAPE_ABSENT,
                        found=near or headings,
                    )
                )
                continue
            # 🔴 BOTH REMAINING KINDS CAN BE TRUE OF ONE HEADING AT ONCE — a
            # duplicated heading whose merged body is still empty — so neither
            # branch excludes the other and neither is an `elif`.
            n = sum(1 for x in headings if x == h)
            if n > 1:
                out.append(
                    ShapeFinding(
                        filename=path.name, heading=h, kind=SHAPE_DUPLICATED, count=n
                    )
                )
            if not present[h].strip():
                out.append(ShapeFinding(filename=path.name, heading=h, kind=SHAPE_EMPTY))
    return tuple(out)


# --------------------------------------------------------------------------
# Unfinished business: the four openness populations, as a writer sees them
# --------------------------------------------------------------------------

@dataclass(frozen=True)
class OpenAction:
    """One bullet `validate` is reporting as unfinished business.

    🔴 FOUR POPULATIONS, AND THEY MUST NEVER BE ADDED TOGETHER INTO ONE NUMBER.
    A `OPEN:` bullet is a claim the WRITER made and is exact, while an unmarked
    one is this tool's guess from two measured phrasings and has unknown recall.
    Merging them would let a floor masquerade as a count.
    """

    filename: str
    declared: bool
    """The writer typed `OPEN:`. Exact — not a guess about the prose."""

    date: str | None
    first_line: str
    near_miss: bool = False
    """The bullet tried to declare a marker and missed the grammar.

    A THIRD population, never folded into either other one: it is not an open
    action and not a guess about one, it is a write that did not land.
    """

    unverifiable_closure: bool = False
    """A `RESOLVED:` that names no sha, so its claim cannot be checked.

    A FOURTH population. Not a problem — closing an action is the outcome this
    whole design wants — but the sha is what separates "closed, and here is the
    commit" from an assertion, and only a branch on `resolved_by` makes the field
    mean anything.
    """


def scan_open_actions(paths: Iterable[str | Path]) -> tuple[OpenAction, ...]:
    """Read each entry's journal and collect its unfinished business. READ-ONLY.

    Deliberately tolerant, exactly as the sibling scanners are: a file no scanner
    may open, one that cannot be read, or one with no `## Nuance / work-history`
    section contributes nothing rather than raising. This runs BESIDE the parse
    check, never in front of it.
    """
    out: list[OpenAction] = []
    for p in paths:
        path = Path(p)
        body = _nuance_body(path)
        if body is None:
            continue
        for b in parse_journal_bullets(body):
            # 🔴 ONE BRANCH, ON THE RESOLVER'S SINGLE PRECEDENCE SOURCE.
            # Re-deriving membership from the individual predicates here is what
            # let a bullet be both a near-miss and an unmarked action on one
            # surface while being one thing on another — the duplicated predicate
            # `openness_population` exists to remove.
            pop = b.openness_population
            if pop in ("none", "resolved"):
                continue
            out.append(
                OpenAction(
                    filename=path.name,
                    declared=pop == "open",
                    near_miss=pop == "near-miss",
                    unverifiable_closure=pop == "unverifiable",
                    date=b.date,
                    first_line=b.first_line,
                )
            )
    return tuple(out)


#: The longest a quoted line runs before it is cut. A finding names a FILE and a
#: LINE NUMBER; the quote is there to recognise it by, and an entry may hold a
#: 4,000-character bullet.
ADVISORY_QUOTE_MAX = 120


def validation_advisory_lines(
    *,
    n_scanned: int,
    shape: Sequence[ShapeFinding],
    dropped: Sequence[DroppedLineFinding],
    open_actions: Sequence[OpenAction],
    unreachable: Sequence[UnreachableMarkerFinding],
) -> tuple[str, ...]:
    """The four write-protocol advisory blocks, as lines. UNPREFIXED.

    Each client prefixes every line with its own `cairn: <scope>: `, because
    `validate` with no `--scope` walks every scope the cache holds and an
    unprefixed block would not say which one it is about.

    🔴 EVERY FINDING SET IS A REQUIRED KEYWORD, NOT A DEFAULT, and that is the
    point of the signature. A defaulted `shape=()` would let a client that never
    wired the scanner print a confident `entry shape: … each present exactly once`
    over files nothing examined — the reassuring zero from an instrument wired to
    nothing, arriving through a forgotten argument instead of an empty directory.
    Required, a missed call site is a `TypeError` here and a compile error in the
    Go port.

    🔴 THE ORDER IS LOAD-BEARING, IN BOTH ADJACENT PAIRS:

      * `entry shape:` COMES FIRST OF ALL. A renamed `## Nuance / work-history`
        makes every one of the three blocks below read an empty section, so their
        zeros are facts about a section the parser never reached. Read in the
        other order, `open actions: 0 declared` is simply false.
      * `dropped lines:` COMES BEFORE `open actions` AND `marker reachability:`.
        A dropped line is content NO reader reaches, so neither of those scans
        ever sees it — a zero printed above a `🔴 N DROPPED LINE(S)` reads as a
        reassurance it cannot support.

    🔴 EVERY BLOCK PRINTS ITS DENOMINATOR EVEN WHEN IT FINDS NOTHING. A bare zero
    is indistinguishable from a scanner wired to nothing, and three of these four
    have a SECOND way to be vacuous that the zero must not hide: they read only
    `## Nuance / work-history`, so an entry whose heading is renamed contributes
    zero to all three for a reason only the SHAPE block can state. The shape
    block's own zero carries the SET it checked for the mirror-image reason — a
    reader who assumes the third spine heading was checked would take it as a
    claim about a heading nothing examined.

    🔴 AND WHEN NOTHING WAS CHECKED THE BLOCKS DO NOT PRINT AT ALL — one
    `NOT CHECKED` line prints instead, naming all four. "0 across 0 entry
    file(s)" is the reassuring zero from an instrument that walked nothing, and it
    must not render anywhere near a clean-looking count.
    """
    if n_scanned == 0:
        return (
            f"write-protocol advisories: NOT CHECKED — 0 entry file(s) scanned, so "
            f"a zero here would be a zero over nothing. This withholds ALL FOUR "
            f"blocks — entry shape, dropped lines, open actions and marker "
            f"reachability. [{DROPPED_LINE}] [{UNREACHABLE_MARKER}]",
        )
    return tuple(
        _entry_shape_block(n_scanned, shape)
        + _dropped_lines_block(n_scanned, dropped)
        + _open_actions_block(n_scanned, open_actions)
        + _reachability_block(n_scanned, unreachable)
    )


def _entry_shape_block(
    n_scanned: int, shape: Sequence[ShapeFinding]
) -> list[str]:
    """The SHAPE advisory. Prints on every path that SCANNED something.

    🔴 IT PRINTS ITS DENOMINATOR EVEN WHEN IT FINDS NOTHING, AND IT PRINTS WHICH
    HEADINGS IT LOOKED FOR. A reassuring zero is indistinguishable from an
    instrument wired to nothing unless it carries the size of what it looked at —
    and here it must also carry the SET it looked at, because a reader who assumes
    the third spine heading was checked would take this zero as a claim about a
    heading nothing examined.

    🔴 THE FOUR KINDS ARE RENDERED SEPARATELY AND NEVER SUMMED — see
    `ShapeFinding`. Each KIND names its own remedy, and `renamed`'s ("you wrote
    this, the schema says that") is the only one a writer can act on in a single
    edit.
    """
    spine = ", ".join(f"`{h}`" for h in SHAPE_HEADINGS)
    absent = [s for s in shape if s.kind == SHAPE_ABSENT]
    renamed = [s for s in shape if s.kind == SHAPE_RENAMED]
    duplicated = [s for s in shape if s.kind == SHAPE_DUPLICATED]
    empty = [s for s in shape if s.kind == SHAPE_EMPTY]
    if not shape:
        return [
            "",
            f"entry shape: {n_scanned} entry file(s) checked for {spine} — each "
            f"present exactly once and non-empty. 🔴 `{WHAT_HEADING}` is NOT checked "
            f"here: the reader DOES surface it, but it feeds no count and no badge, "
            f"so its absence changes nothing this zero is about — `subsystem_recall` "
            f"names a missing one under that entry's own body instead."
        ]
    out = [
        "",
        f"entry shape across {n_scanned} entry file(s), checked for {spine} "
        f"(NOT `{WHAT_HEADING}`, which feeds no count and no badge):"
    ]
    if renamed:
        out.append(
            f"  🔴 {len(renamed)} section(s) RENAMED — the heading is close but not "
            f"exact, so NO reader reaches the section. Matching is exact-string after "
            f"a right-strip, case-sensitive, at column 0. On that entry's index row "
            f"this reads as `0 nuance` with no `OPEN` badge: PARSE FAILURE, not an "
            f"empty entry."
        )
        for s in renamed:
            wrote = ", ".join(f"`{h}`" for h in s.found)
            out.append(f"    {s.filename}: `{s.heading}` is written as {wrote}")
    if absent:
        out.append(
            f"  🔴 {len(absent)} section(s) ABSENT — the heading is not in the file at "
            f"all, under any spelling this tool can pair with it. Whatever the entry "
            f"says on that subject is invisible to every default read. Read the "
            f"inventory below against the schema heading: a section retitled far "
            f"enough that no folding pairs it lands here rather than under RENAMED."
        )
        for s in absent:
            shown = list(s.found[:SHAPE_INVENTORY_SHOWN])
            rest = len(s.found) - len(shown)
            inventory = ", ".join(f"`{h}`" for h in shown) if shown else "(none at all)"
            if rest > 0:
                inventory += f", … {rest} more"
            out.append(
                f"    {s.filename}: no `{s.heading}`; the file's headings are {inventory}"
            )
    if duplicated:
        out.append(
            f"  🔴 {len(duplicated)} heading(s) DUPLICATED — the sections silently "
            f"MERGE into one body and anything written under a heading BETWEEN them is "
            f"dropped from the read entirely. Fold them into one section."
        )
        for s in duplicated:
            out.append(f"    {s.filename}: `{s.heading}` appears {s.count} times")
    if empty:
        out.append(
            f"  ⚠ {len(empty)} section(s) PRESENT AND EMPTY — the heading is there "
            f"with nothing under it. Not a parse failure and not the same as absent: "
            f"the reader finds the section and prints a blank."
        )
        for s in empty:
            out.append(f"    {s.filename}: `{s.heading}`")
    out.append(
        "  (Advisory, and it changes no verdict: the loader accepts a file whose "
        "sections it cannot find, which is precisely the silent failure this block "
        "exists to make loud. Fix the heading, not the exit code.)"
    )
    return out


def _open_actions_block(
    n_scanned: int, open_actions: Sequence[OpenAction]
) -> list[str]:
    """The UNFINISHED-BUSINESS advisory. Prints on every path that SCANNED something.

    🔴 IT PRINTS ITS DENOMINATOR EVEN WHEN IT FINDS NOTHING, like every sibling.
    "0 declared across 29 entry file(s)" is a reading; a blank space is not.

    🔴 THE FOUR POPULATIONS ARE RENDERED SEPARATELY AND NEVER SUMMED — see
    `OpenAction`. `declared` is exact and the unmarked guess is a FLOOR with
    unknown recall; one total over both would let the floor masquerade as a count.
    """
    declared = [a for a in open_actions if a.declared]
    near = [a for a in open_actions if a.near_miss]
    unverifiable = [a for a in open_actions if a.unverifiable_closure]
    guessed = [
        a for a in open_actions
        if not a.declared and not a.near_miss and not a.unverifiable_closure
    ]
    if not open_actions:
        return [
            "",
            f"open actions: 0 declared across {n_scanned} entry file(s), 0 "
            f"attempted-but-unparsed, 0 `RESOLVED:` naming no sha, and 0 unmarked "
            f"bullets matched the two phrasings this tool can recognise. 🔴 The last "
            f"of those is a FLOOR with unknown recall, not a clean bill of health — "
            f"an unfinished action phrased any other way is invisible here.",
        ]
    out = ["", f"open actions across {n_scanned} entry file(s):"]
    if declared:
        out.append(
            f"  🔴 {len(declared)} declared `OPEN:` — exact, the writer said so. "
            f"Re-check against the repo; if it landed, rewrite as `RESOLVED <sha>:`."
        )
        for a in declared:
            out.append(f"    {a.filename}: {a.first_line[:ADVISORY_QUOTE_MAX]}")
    if near:
        out.append(
            f"  🔴 {len(near)} bullet(s) look like an ATTEMPTED marker that did not "
            f"parse — they declare nothing and show no badge. Fix the line: the "
            f"marker follows `YYYY-MM-DD: `, is upper-case, carries no emphasis or "
            f"parenthetical, and ends in `:`."
        )
        for a in near:
            out.append(f"    {a.filename}: {a.first_line[:ADVISORY_QUOTE_MAX]}")
    if guessed:
        out.append(
            f"  ⚠ {len(guessed)} unmarked bullet(s) that READ like an open action. "
            f"AT LEAST this many — two measured phrasings, unknown recall."
        )
        for a in guessed:
            out.append(f"    {a.filename}: {a.first_line[:ADVISORY_QUOTE_MAX]}")
    if unverifiable:
        out.append(
            f"  ⚠ {len(unverifiable)} `RESOLVED:` bullet(s) name no sha, so the "
            f"closure cannot be checked. Not a defect — closing is the point — but "
            f"`RESOLVED <sha>:` is what makes it verifiable rather than asserted."
        )
        for a in unverifiable:
            out.append(f"    {a.filename}: {a.first_line[:ADVISORY_QUOTE_MAX]}")
    out.append(
        "  (Advisory. None of this changes the verdict: an entry with unfinished "
        "business is still well-formed, and failing it here would be a red gate "
        "nobody could turn green by fixing the file.)"
    )
    return out


def _dropped_lines_block(
    n_scanned: int, dropped: Sequence[DroppedLineFinding]
) -> list[str]:
    """The DROPPED-LINE advisory — the half that means content is ALREADY LOST.

    🔴 THE ZERO CARRIES ITS OWN BLIND SPOT IN WORDS. This check is knowingly
    PARTIAL — the absorbed-tail case is undecidable, see `scan_dropped_lines` —
    so a bare "0 dropped" would read as "no bullet has lost its head", which is a
    claim it cannot make. Saying which half was checked is the difference between
    a measurement and a reassurance.
    """
    if not dropped:
        return [
            "",
            f"dropped lines: 0 across {n_scanned} entry file(s) scanned "
            f"[{DROPPED_LINE}] — every non-blank `{NUANCE_HEADING}` line reaches a bullet some reader "
            f"will surface. 🔴 PARTIAL BY CONSTRUCTION, IN THREE WAYS, and this "
            f"zero is a claim about none of them: (1) ABSORBED TAIL — a bullet that "
            f"lost its opening line while another bullet sat above it is absorbed "
            f"into that one and is byte-identical to a legitimate wrap, so no check "
            f"can separate them; (2) UNCLOSED FENCE — an odd number of ``` or ~~~ "
            f"makes every line after it fenced, and fenced lines are skipped as "
            f"sample text, so a whole bullet can be swallowed with its `OPEN:` and "
            f"reported nowhere; (3) DUPLICATED `{NUANCE_HEADING}` HEADING — the "
            f"sections are concatenated into one body, so text under the second is "
            f"absorbed by a bullet from the first and any offset printed here would "
            f"index the concatenation rather than the file."
        ]
    n = len(dropped)
    marked = sum(1 for d in dropped if d.carries_marker)
    out = [
        "",
        f"🔴 {n} DROPPED LINE(S) across {n_scanned} entry file(s) scanned "
        f"[{DROPPED_LINE}] — present in the file, inside NO bullet, so EVERY "
        f"reader skips them: "
        f"`--ref`, `--search`, the digest and every openness count. "
        f"`parse_journal_bullets` drops text that precedes the first bullet. The "
        f"cause is almost always a lost or indented bullet OPENING line; the fix "
        f"is to restore it, NOT to delete the text."
    ]
    if marked:
        out.append(
            f"  🔴 {marked} of them look{'s' if marked == 1 else ''} like a "
            f"`OPEN:`/`RESOLVED:` DECLARATION. "
            f"That is an open action the store is actively failing to report — it "
            f"raises no badge and is counted in no openness total, because the "
            f"bullet that would carry it no longer exists."
        )
    for d in dropped:
        flag = "  ← looks like a DECLARATION" if d.carries_marker else ""
        out.append(f"    {d.filename}: nuance line {d.offset}{flag}")
        out.append(f"      {d.line.strip()[:ADVISORY_QUOTE_MAX]}")
    out.append(
        "  🔴 RESTORE FROM HISTORY, NOT FROM MEMORY. The entry's previous version "
        "is one `git log -p -- <file>` away in the store's own history. "
        "Reconstructing the opening line by hand invents a date and an author the "
        "store never had."
    )
    out.append(
        "  (Advisory. It changes no verdict: the loader accepts the file, and the "
        "write protocol branches on this command's exit code to mean 'write "
        "NOTHING'.)"
    )
    return out


def _reachability_block(
    n_scanned: int, unreachable: Sequence[UnreachableMarkerFinding]
) -> list[str]:
    """The MARKER-REACHABILITY advisory.

    🔴 ITS OWN BLOCK, BECAUSE IT IS ITS OWN SHAPE. Every remedy a near-miss
    advisory names — "fix the LINE", "rewrite as `RESOLVED <sha>:`" — is wrong
    here. A marker on a continuation line is spelled correctly; the edit it needs
    is to be PROMOTED to a bullet of its own.
    """
    if not unreachable:
        return [
            "",
            f"marker reachability: 0 out-of-reach marker(s) across {n_scanned} entry "
            f"file(s) scanned [{UNREACHABLE_MARKER}] — every `OPEN:`/`RESOLVED:` "
            f"found is "
            f"on a bullet's OPENING line, where the parser reads.",
        ]
    n = len(unreachable)
    out = [
        "",
        f"🔴 {n} MARKER(S) OUT OF REACH across {n_scanned} entry file(s) scanned "
        f"[{UNREACHABLE_MARKER}] — spelled CORRECTLY, on a bullet's CONTINUATION "
        f"line, where NO reader looks. The marker pattern is anchored at position "
        f"0 of a bullet's OPENING line, so this declares NOTHING: it raises "
        f"neither the `OPEN` badge nor `NEAR-MISS`. 🔴 It is NOT a near-miss and "
        f"is NOT counted as one — a near-miss is mis-spelled where the parser "
        f"looks and is fixed by editing the line; this is fixed by PROMOTING the "
        f"line to a top-level bullet of its own.",
    ]
    for u in unreachable:
        out.append(f"    {u.filename}: line {u.offset} of the bullet opening")
        out.append(f"      bullet: {u.bullet_first_line[:ADVISORY_QUOTE_MAX]}")
        out.append(
            f"      marker: {u.line.strip()[:ADVISORY_QUOTE_MAX]}   "
            f"(would declare `{u.openness}`)"
        )
    out.append(
        "  🔴 BEFORE FIXING ANY MARKER ABOVE IT IN THE SAME SECTION, re-check this "
        "one against the store's history. Such a bullet can have raised its badge "
        "only BY ACCIDENT, through a broken `RESOLVED —` sitting above it — so "
        "repairing that line would SILENCE a still-open action."
    )
    out.append(
        "  (Advisory. It changes no verdict: the loader accepts the file, and the "
        "write protocol branches on this command's exit code to mean 'write "
        "NOTHING'.)"
    )
    return out
