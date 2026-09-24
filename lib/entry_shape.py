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
from dataclasses import dataclass
from pathlib import Path
from typing import Iterable, Sequence

from host_identity import this_host
from subsystem_resolver import (
    NUANCE_HEADING,
    POINTERS_HEADING,
    UNREACHABLE_MARKER,
    extract_sections,
    line_mentions_marker,
    line_openness,
    normalize_ref,
    parse_journal_bullets,
)
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
    "DroppedLineFinding", "UnreachableMarkerFinding",
    "derive_scope", "repo_path_missing_message", "scope_for_repo",
    "store_caveat", "store_host", "store_host_line",
    "line_carries_marker", "scan_dropped_lines", "scan_unreachable_markers",
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


def _nuance_body(path: Path) -> str | None:
    """The `## Nuance / work-history` body of one entry file, or None.

    Deliberately tolerant: a file that cannot be read, or has no nuance section,
    yields None rather than raising. Both scanners run BESIDE the parse check,
    never in front of it — a malformed file's own rejection is the finding that
    matters, and an advisory computed from its half-parsed body would bury it.
    """
    try:
        text = path.read_text(encoding="utf-8", errors="replace")
    except OSError:
        return None
    return extract_sections(text, (NUANCE_HEADING,)).get(NUANCE_HEADING) or None


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

    🔴 WHAT IT STRUCTURALLY CANNOT SEE, stated because the gap is the whole
    reason to read this twice. A bullet that loses its opening line while ANOTHER
    bullet sits above it is not detectable — `parse_journal_bullets` appends
    every non-bullet line to the bullet above, so the orphaned tail is absorbed
    into it and inherits its date, and the resulting file is BYTE-IDENTICAL to
    one where that bullet legitimately wrapped. No check can separate the two,
    and this one does not pretend to: it covers the case where the drop is
    decidable — text before the first bullet, which includes every entry whose
    NEWEST bullet lost its head, the store being newest-first.
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


#: The longest a quoted line runs before it is cut. A finding names a FILE and a
#: LINE NUMBER; the quote is there to recognise it by, and an entry may hold a
#: 4,000-character bullet.
ADVISORY_QUOTE_MAX = 120


def validation_advisory_lines(
    *,
    n_files: int,
    dropped: Sequence[DroppedLineFinding],
    unreachable: Sequence[UnreachableMarkerFinding],
) -> tuple[str, ...]:
    """The two write-protocol advisory blocks, as lines. UNPREFIXED.

    Each client prefixes every line with its own `cairn: <scope>: `, because
    `validate` with no `--scope` walks every scope the cache holds and an
    unprefixed block would not say which one it is about.

    🔴 THE DROPPED-LINE BLOCK COMES FIRST, deliberately. A dropped line is
    content NO reader reaches, so the marker scan never sees it — a
    `0 out-of-reach` printed above a `🔴 N DROPPED LINE(S)` is a fact about text
    the parser never got to, and reads as a reassurance it cannot support.

    🔴 EVERY BLOCK PRINTS ITS DENOMINATOR EVEN WHEN IT FINDS NOTHING. A bare zero
    is indistinguishable from a scanner wired to nothing, and each of these has a
    SECOND way to be vacuous that the zero must not hide: both read only
    `## Nuance / work-history`, so an entry whose heading is renamed contributes
    zero to both for a reason neither block can state.

    🔴 AND WHEN NOTHING WAS CHECKED THE BLOCKS DO NOT PRINT AT ALL — one
    `NOT CHECKED` line prints instead. "0 across 0 entry file(s)" is the
    reassuring zero from an instrument that walked nothing, and it must not
    render anywhere near a clean-looking count.
    """
    if n_files == 0:
        return (
            f"dropped lines / marker reachability: NOT CHECKED — 0 entry file(s) "
            f"to read, so a zero here would be a zero over nothing. "
            f"[{DROPPED_LINE}] [{UNREACHABLE_MARKER}]",
        )
    return tuple(
        _dropped_lines_block(n_files, dropped) + _reachability_block(n_files, unreachable)
    )


def _dropped_lines_block(
    n_files: int, dropped: Sequence[DroppedLineFinding]
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
            f"dropped lines: 0 across {n_files} entry file(s) [{DROPPED_LINE}] — "
            f"every non-blank `{NUANCE_HEADING}` line reaches a bullet some reader "
            f"will surface. 🔴 PARTIAL BY CONSTRUCTION: this sees text before the "
            f"FIRST bullet. A bullet that lost its opening line while another "
            f"bullet sat above it is absorbed into that one and is byte-identical "
            f"to a legitimate wrap — no check can see it, and this zero is not a "
            f"claim about that case."
        ]
    n = len(dropped)
    marked = sum(1 for d in dropped if d.carries_marker)
    out = [
        f"🔴 {n} DROPPED LINE(S) across {n_files} entry file(s) [{DROPPED_LINE}] — "
        f"present in the file, inside NO bullet, so EVERY reader skips them: "
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
    n_files: int, unreachable: Sequence[UnreachableMarkerFinding]
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
            f"marker reachability: 0 out-of-reach marker(s) across {n_files} entry "
            f"file(s) [{UNREACHABLE_MARKER}] — every `OPEN:`/`RESOLVED:` found is "
            f"on a bullet's OPENING line, where the parser reads.",
        ]
    n = len(unreachable)
    out = [
        "",
        f"🔴 {n} MARKER(S) OUT OF REACH across {n_files} entry file(s) "
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
