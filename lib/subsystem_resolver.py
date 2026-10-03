"""Map changed repo-relative paths onto `/analyze-service` index entries.

P0 of the derived session→subsystem association described in
`claudedocs/decision-subsystem-store-rejected.md` → "What replaced the
premise: derived session association". This module is the WHOLE of P0: a pure,
source-agnostic function plus a thin disk loader. It emits nothing, knows nothing
about ClickHouse, and has never heard of Claude Code or opencode — P1 feeds it
paths from either.

🔴 THIS FILE IS THE EXECUTABLE AUTHORITY FOR THE REF-NORMALIZATION AND
RESOLUTION RULES.
`claude/skills/analyze-service/SKILL.md` states the same rules in prose, for a reader
that is an LLM rather than an interpreter, so the predicate necessarily exists at
two sites — and `claude/RULES.md` → "One rule, one place" says a duplicated
predicate regenerates the same bug at both. It cannot be literally deduplicated
(you cannot import a function into a markdown file), so it is made DETECTABLE
instead:

  * this module is named in that skill as the authority;
  * `tests/test_subsystem_resolver.py` → `TestCommandDocIsPinned` holds
    the skill's normative sentences as literal substrings AND the behaviour
    each one asserts. Reword the prose without touching the code (or vice versa)
    and that test goes red naming the sentence that moved.

That matters more here than usual because the failure is SILENT: if the two
implementations drift, associations simply stop matching, and a zero reads as
"this subsystem had no sessions" rather than "the matcher is broken".

WHAT IT MATCHES ON, AND WHY NOT A STORED PATH
---------------------------------------------
The index deliberately never persists WHERE a subsystem lives — location is
re-derived live on every recon (`analyze-service.md`: "Phase 1 never caches
location"). So association matches PATH COMPONENTS against each entry's slug and
its `aliases:`, after normalization. No `paths:` field is added to the schema and
none is inferred and cached here.

Matching is EXACT normalized-component equality, never substring. `redis` does
not match a component `redis-wedge-relief`, and `handoff-redis-2026-01-01.md`
does not match `redis`. Substring matching would make every short slug (`db`,
`s3`, `rum`) match most of the tree.

CONTRACT SUMMARY
----------------
    normalize_ref(raw)                -> str            (the shared predicate)
    split_kind(ref)                   -> (slug, kind|None)
    SubsystemEntry.from_mapping(m)    -> SubsystemEntry (raises MalformedEntryError)
    build_index(mappings, *, on_malformed=RAISE)
                                      -> SubsystemIndex (raises MalformedEntryError
                                         under RAISE; collects into
                                         `.malformed` under COLLECT)
    resolve_ref(ref, index, scope)    -> SubsystemEntry|None
                                        (raises UnknownScopeError, AmbiguousRefError)
    associate_paths(paths, index, scope, *, min_paths=DEFAULT_MIN_PATHS)
                                      -> Association
                                        (raises UnknownScopeError,
                                         InvalidPathError, ValueError)
    entry_mapping(text, *, filename, scope)
                                      -> dict  (ONE entry file -> the mapping the
                                         loader would build; the shared step a
                                         validator and the loader must not spell
                                         twice)
    load_index(root, *, on_malformed=RAISE, visible_scopes=None)
                                      -> SubsystemIndex (the thin disk loader;
                                         `visible_scopes=None` is UNRESTRICTED and
                                         an EMPTY sequence is its opposite — see
                                         `visible_scope_set`)
    visible_scope_set(visible_scopes) -> set[str]|None (the one allowlist folding,
                                         shared with `subsystem_recall.load_store`
                                         and the API's `/snapshot`)
    classify_path(path)               -> str  (one of `ALL_KINDS`; the ONE answer
                                         to "what IS this path", shared with
                                         `subsystem-store-api/server.py`)
    action_for(kind, actions)         -> SKIP|TAKE|REFUSE (raises AssertionError on
                                         an unmapped kind — never a default)

Every raise carries a distinct sentinel phrase so a caller — or a mutation test —
can tell WHICH guard fired, not merely that something did:

    "malformed index entry"        MalformedEntryError
    "unknown scope"                UnknownScopeError
    "ambiguous ref"                AmbiguousRefError
    "invalid repo-relative path"   InvalidPathError

An EMPTY PATH SET is not among them — it returns an empty, accounted result, and
`Association.considered_paths` is what keeps that zero distinguishable from
"we looked and matched nothing". See `associate_paths`.

A guard whose test passes because a NEIGHBOUR's error fired is green for the
wrong reason and stays green with the guard deleted (`claude/RULES.md` →
"Mutation-test a guard before certifying it").
"""

from __future__ import annotations

import errno
import hashlib
import re
import stat
from collections.abc import Sequence as _AbcSequence
from dataclasses import dataclass
from datetime import date as _date
from pathlib import Path, PurePosixPath
from typing import Iterable, Mapping, Sequence

__all__ = [
    "KINDS",
    "DEFAULT_MIN_PATHS",
    "WHAT_HEADING",
    "POINTERS_HEADING",
    "NUANCE_HEADING",
    "ResolverError",
    "MalformedEntryError",
    "UnknownScopeError",
    "AmbiguousRefError",
    "InvalidPathError",
    "EntryUnreadableError",
    "MalformedEntry",
    "ON_MALFORMED",
    "ON_MALFORMED_RAISE",
    "ON_MALFORMED_COLLECT",
    "OPENNESS_OPEN",
    "OPENNESS_RESOLVED",
    "UNREACHABLE_MARKER",
    "UnreachableMarker",
    "JournalBullet",
    "REQUIREMENTS_HEADING",
    "PROVENANCE_OPERATOR",
    "PROVENANCE_INFERRED",
    "Requirement",
    "parse_requirements",
    "extract_sections",
    "scan_headings",
    "parse_journal_bullets",
    "citation_ids_by_start_line",
    "citation_token",
    "line_openness",
    "line_mentions_marker",
    "SubsystemEntry",
    "SubsystemIndex",
    "Evidence",
    "SubsystemMatch",
    "AmbiguousRef",
    "Association",
    "TaskRef",
    "TaskRefError",
    "TAG_MAX_RUNES",
    "parse_task_ref",
    "format_task_refs",
    "lossy_tag_for",
    "entry_references",
    "entry_has_tag",
    "normalize_ref",
    "split_kind",
    "path_refs",
    "build_index",
    "resolve_ref",
    "resolve_ref_tiered",
    "associate_paths",
    "parse_front_matter",
    "entry_mapping",
    "SCOPE_POLICY_SHEET",
    "is_entry_filename",
    "entry_files_in",
    "load_index",
    "visible_scope_set",
    "KIND_BROKEN_LINK",
    "KIND_LINK_TO_DIR",
    "KIND_LINK_TO_FILE",
    "KIND_LINK_TO_OTHER",
    "KIND_DIRECTORY",
    "KIND_REGULAR_FILE",
    "KIND_OTHER",
    "KIND_INDETERMINATE",
    "KIND_ABSENT",
    "ALL_KINDS",
    "SKIP",
    "TAKE",
    "REFUSE",
    "classify_path",
    "action_for",
]

# The kind enum from `analyze-service.md`: "kind ∈ `service` | `process` | `org`
# | `doc`". A trailing dot-segment is a kind ONLY if it is in this tuple —
# otherwise it is part of the slug, which is what keeps a slug like
# `forgejo.example.com` or a component like `values.yaml` intact.
KINDS: tuple[str, ...] = ("service", "process", "org", "doc")

# --- The precision threshold ---------------------------------------------------
# "A session that grazes one file under a directory should not tag that subsystem
# the same as one that rewrites it." The unit is DISTINCT PATHS, not commits:
# this function is source-agnostic and receives paths, so a commit count is not
# something it can see (a caller that has commits can supply its own threshold
# semantics by calling once per commit and aggregating).
#
# 🔴 The value is a NAMED DEFAULT, not a tuned constant, and it is deliberately
# the weakest defensible one. There is no measurement to justify 3 or 5 — the
# association has never been run over real sessions, which is P1's job. 2 is
# chosen because it is the smallest value that excludes the case the brief names
# (a single grazed file) while admitting everything else, so P1 measures against
# a floor rather than against a guess. Callers pass `min_paths=` explicitly;
# raise this only with the measurement quoted.
DEFAULT_MIN_PATHS = 2

# Derived from the command's normalization sentence — the character class it
# names is `[a-z0-9.-]`, so `_` and everything else outside it folds to `-`.
#
# 🔴 `_` IS FOLDED HERE, AND ONLY HERE. `normalize_ref` used to open with an
# explicit `.replace("_", "-")` mirroring the doc's "`_` → `-`" clause — but `_`
# is already outside this class, so that line could be deleted with no
# behavioural change at all. A mutation test aimed at it was UNKILLABLE, which is
# how the redundancy was found: it read as the guard implementing the `_` rule
# while this regex was the thing actually doing it. One site, so a mutation of it
# is observable.
_NON_SLUG = re.compile(r"[^a-z0-9.-]")
_DASH_RUN = re.compile(r"-{2,}")

# ⚠ The `\A` is REDUNDANT-BUT-KEPT (labelled, so a sweep does not re-derive it):
# this pattern is only ever used with `.match()`, which already anchors at
# position 0. It stays because it makes "front matter must be at the TOP of the
# file" readable at the pattern rather than at the call site.
_FRONT_MATTER = re.compile(r"\A---[ \t]*\r?\n(.*?)\r?\n---[ \t]*(?:\r?\n|\Z)", re.S)


# --- Errors --------------------------------------------------------------------


class ResolverError(Exception):
    """Base for every error this module raises."""


class MalformedEntryError(ResolverError):
    """An index entry cannot be interpreted. Sentinel: 'malformed index entry'.

    `.why` is the REASON with the sentinel prefix stripped, and `.source` is the
    entry it was raised about. Both are carried STRUCTURALLY rather than left to
    be recovered by splitting `str(exc)` on `": "` — a degrading loader has to
    print one row per bad entry, and a row assembled by re-parsing an error
    message is a second parser for a format nothing pins. `str(exc)` is unchanged
    and still leads with the sentinel, so every existing caller and every `in`
    assertion over it keeps working.
    """

    def __init__(self, message: str, *, source: str | None = None, why: str | None = None) -> None:
        self.source = source
        self.why = message if why is None else why
        super().__init__(message)


class UnknownScopeError(ResolverError):
    """The requested scope is not in the index. Sentinel: 'unknown scope'."""


# ⚠ There is deliberately NO EmptyPathSetError. An empty path set returns an
# empty, accounted `Association`; see `associate_paths`' docstring for why, and
# for the `considered_paths` property that keeps the two kinds of zero apart
# without an exception. Do not reintroduce it without re-reading that.


class InvalidPathError(ResolverError):
    """A path is not usable as a repo-relative path. Sentinel: 'invalid repo-relative path'.

    Absolute paths and `..` traversal are rejected rather than normalized away.
    An absolute path drags its whole prefix into the component set — `/home/zach/
    workspace/<repo>/...` offers `home`, `zach` and `workspace` as refs — so
    accepting one silently converts a caller bug into plausible-looking
    associations.
    """


class AmbiguousRefError(ResolverError):
    """A ref resolved to more than one entry within a tier. Sentinel: 'ambiguous ref'.

    `analyze-service.md`: ">1 in a tier → never pick: stop, call the ref
    ambiguous and list the candidates". Never resolved by preference order, never
    by first-hit; `.candidates` is the list for a human to choose from.
    """

    def __init__(self, ref: str, tier: str, candidates: Sequence[str], scope: str) -> None:
        self.ref = ref
        self.tier = tier
        self.scope = scope
        self.candidates: tuple[str, ...] = tuple(candidates)
        super().__init__(
            f"ambiguous ref {ref!r} in scope {scope!r}: {len(self.candidates)} candidates "
            f"in the {tier} tier ({', '.join(self.candidates)}). The resolver never picks — "
            f"disambiguate the ref or the index."
        )


class EntryUnreadableError(ResolverError):
    """An entry file cannot be read. Sentinel: 'index entry unreadable'.

    🔴 It exists because the alternative is an unnamed `OSError` escaping from
    inside `load_index`. The store is a plain directory of hand-curated files on
    a machine that also runs an hourly autocommit: a file can be mid-rename, a
    directory can be sitting where a `.md` is expected, a mode can be wrong. Any
    of those would otherwise reach the caller as `IsADirectoryError` or
    `PermissionError` with no indication that the SUBSYSTEM STORE was the thing
    that failed.

    🔴 IT LIVES HERE, IN THE MODULE BOTH READERS IMPORT, RATHER THAN IN EITHER OF
    THEM. `subsystem_recall` raises it when a `/resume` read fails and
    the writer half raises it when a `/handoff` read fails — the SAME condition
    on the SAME files. Two classes spelling one condition is how a caller ends up
    catching the reader's and missing the writer's; the precedent is
    `StoreMissingError`, which `subsystem_recall` imports from the writer half
    for exactly this reason rather than declaring a second one. It cannot live in
    either of those two modules, because `subsystem_recall` already imports
    the writer half and the reverse edge would close a cycle.
    """


# --- Degrading: what a rejected entry looks like when it is not an exception ----
#
# 🔴 ONE BAD ENTRY USED TO COST THE WHOLE SCOPE. Measured on a synthetic store:
# 2 good entries listed 2; 2 good + 1 malformed listed **0** and exited 3, so
# `/resume` step 4, `--list`, `--ref` and `--search` all died together on one
# wrapped `aliases:` line. `RAISE` is still the default — every existing caller
# keeps its fail-closed contract and no test changes meaning — and `COLLECT` is
# opt-in, per call site, because whether a rejection should abort is a POLICY of
# the caller and not a property of the store.
#
# 🔴 COLLECT IS NOT "SKIP". A collected entry is carried on the index, counted,
# and every reader that uses this mode is obliged to print it: silently serving a
# short index would be a WORSE failure than the collapse it replaces, because a
# missing entry is indistinguishable from an entry that was never written.
ON_MALFORMED_RAISE = "raise"
ON_MALFORMED_COLLECT = "collect"
ON_MALFORMED: tuple[str, ...] = (ON_MALFORMED_RAISE, ON_MALFORMED_COLLECT)


@dataclass(frozen=True)
class MalformedEntry:
    """One entry file that could not be interpreted, and WHY — as data, not a raise.

    `scope` is the NORMALIZED owning scope, which for a disk load is the
    directory name (the authority on scope; see `load_index`). It is what lets a
    reader report this entry against the scope it belongs to and no other — a
    malformed entry in `scope-b/` must not appear while recalling `scope-a/`, and
    must not make `scope-a` look broken.
    """

    scope: str
    filename: str
    reason: str
    """The `why` clause — the sentence after the sentinel, e.g. "`aliases:` must
    be a list, not a bare string"."""

    @property
    def label(self) -> str:
        """`<scope>/<filename>` — how every surface names this file."""
        return f"{self.scope}/{self.filename}" if self.scope else self.filename

    @property
    def line(self) -> str:
        """ONE row, carrying the sentinel phrase.

        🔴 The sentinel is repeated PER ROW rather than hoisted into a block
        header, because a row is what gets copied into a report, quoted in a
        message, or grepped for — and a row that has left its header behind is a
        row that no longer says what kind of problem it describes.
        """
        return f"malformed index entry `{self.label}`: {self.reason}"


# --- Task refs -----------------------------------------------------------------
#
# 🔴 THE ID HALF IS OPAQUE AND THIS MODULE ENUMERATES NO SYSTEMS.
#
# An entry says which tasks it answers as `<system>:<id>`. The split is on the
# FIRST colon and nothing else is interpreted, which is what makes the schema
# outlive the three systems in use today: `linear:ENG-441` and `jira:PROJ-7`
# store, validate and round-trip here without this file learning either name.
# URL resolution IS system-specific and lives elsewhere, deliberately — that is
# the one place a system list is legitimate, and keeping it out of the parser is
# what stops an unknown system becoming an unstorable one.
#
# 🔴 VERBATIM MEANS VERBATIM: `#` SURVIVES, AND THAT IS THE POINT.
# GitHub's lossless form is `owner/repo#N`, so `github:example-org/alpha-toolkit#428`
# is one ref whose id half is `example-org/alpha-toolkit#428`. Any encoding that
# cannot carry a `#` cannot carry a GitHub reference — see `lossy_tag_for` below
# for what happens when one tries.
#
# The system half is normalized (lowercase, `-`-folded) because it is an
# identifier this code compares. The id half is NEVER normalized because it is an
# identifier some OTHER system compares, and folding `ENG-441` to `eng-441` would
# hand Linear a key it does not recognise.

_TASK_REF_SPLIT = ":"

TAG_MAX_RUNES = 64
"""The downstream tag surface's length limit, which `lossy_tag_for` derives output for."""


@dataclass(frozen=True)
class TaskRef:
    """One `<system>:<id>` reference from an entry's `tasks:` front matter."""

    system: str
    """Normalized system name — lowercased and `-`-folded, like every other ref."""

    ident: str
    """The id half, BYTE-IDENTICAL to what the file carried. Never normalized."""

    raw: str
    """The whole ref exactly as written, for evidence and for error messages."""

    def __str__(self) -> str:
        return f"{self.system}{_TASK_REF_SPLIT}{self.ident}"


class TaskRefError(ValueError):
    """A `tasks:` entry that is not a well-formed `<system>:<id>` ref."""


def parse_task_ref(raw: object) -> TaskRef:
    """`<system>:<id>` -> `TaskRef`, or raise `TaskRefError` naming the fix.

    Split on the FIRST colon only. Both halves must be non-empty after stripping
    surrounding whitespace — an empty half is the failure this rejects, because
    `:428` and `github:` each look like a ref and address nothing.

    Whitespace INSIDE either half is rejected too. A ref is a single token and a
    space in one almost always means an inline list lost its brackets
    (`tasks: clickup:868abc123 github:o/r#1`), which would otherwise store as one
    ref with a nonsense id and never resolve.
    """
    if not isinstance(raw, str):
        raise TaskRefError(
            f"task ref {raw!r} is {type(raw).__name__}, not a string — "
            f"write it as `<system>:<id>`"
        )
    text = raw.strip()
    if not text:
        raise TaskRefError("task ref is empty — write it as `<system>:<id>`")
    system, sep, ident = text.partition(_TASK_REF_SPLIT)
    if not sep:
        raise TaskRefError(
            f"task ref {raw!r} has no `:` — write it as `<system>:<id>`, "
            f"e.g. `clickup:868abc123` or `github:owner/repo#428`"
        )
    system, ident = system.strip(), ident.strip()
    if not system:
        raise TaskRefError(
            f"task ref {raw!r} has an empty system half — write it as `<system>:<id>`"
        )
    if not ident:
        raise TaskRefError(
            f"task ref {raw!r} has an empty id half — write it as `<system>:<id>`"
        )
    if any(c.isspace() for c in system) or any(c.isspace() for c in ident):
        raise TaskRefError(
            f"task ref {raw!r} contains whitespace — one ref per list item; "
            f"write several as `tasks: [a, b]`"
        )
    # 🔴 A COMMA IS A SEPARATOR IN THE FORM THIS SCHEMA IS WRITTEN IN, so a ref
    # containing one cannot survive its own serialization: `format_task_refs`
    # emits `refs: [clickup:a,b]`, the inline-list reader splits on `,`, and the
    # entry comes back MALFORMED and invisible to every reader. Rejecting it at
    # parse time is the only place that keeps "accepted" and "round-trips" the
    # same set — a writer-side check would still let a hand-written file through.
    #
    # ⚠ THE TWO REFUSAL MESSAGES AROUND THIS COMMENT STILL SPELL THE EXAMPLE
    # `tasks: [a, b]` WHILE THE KEY TO WRITE IS `refs:`, AND THAT IS DEFERRED RATHER THAN
    # MISSED: their bytes are pinned by `tests/conformance/` goldens and diffed against
    # the Go port's, so the wording moves in a change whose subject is that re-base.
    if "," in text:
        raise TaskRefError(
            f"task ref {raw!r} contains a comma, which separates items in "
            f"`tasks: [a, b]` — a ref cannot contain one"
        )
    # Control characters (NUL, ESC, a stray newline) round-trip through the file
    # and reach a terminal, a JSON payload and an error message. Nothing legible
    # needs them, and a task id containing one is a corrupted read, not an id.
    # C0 (0x00-0x1F), DEL (0x7F) and C1 (0x80-0x9F). C1 is included because the
    # claim is "printable text": omitting it left the check narrower than the
    # sentence describing it, which is the defect this module keeps finding in
    # its own guards.
    if any(ord(c) < 0x20 or 0x7F <= ord(c) <= 0x9F for c in text):
        raise TaskRefError(
            f"task ref {raw!r} contains a control character — task ids are "
            f"printable text"
        )
    normalized_system = normalize_ref(system)
    if not normalized_system:
        raise TaskRefError(
            f"task ref {raw!r} has a system half that normalizes to the empty string"
        )
    return TaskRef(system=normalized_system, ident=ident, raw=text)


def format_task_refs(refs: "Sequence[TaskRef | str]") -> str:
    """Refs -> the single front-matter LINE that reads back as the same refs.

    🔴 IT EMITS `refs:`, WHICH IS THE CANONICAL KEY, AND IT USED TO EMIT `tasks:`.
    `tasks:`/`task:` are ACCEPTED spellings on the way in and are not the spelling to
    write: the key carries repos, PRs, docs and dashboards, so `tasks:` names a subset of
    what it holds. This is the ONLY serializer in either language, so a writer emitting the
    older key was the one place a store could keep acquiring files written in it — which is
    the whole reason it is an alias rather than the name. No live caller today; it is
    exported, and being unreached is not being right.

    Inline flow form on ONE line, on purpose and not as a style preference: a
    wrapped list on a key the parser type-checks is what makes a whole entry
    MALFORMED and invisible to every reader, and this repo has already paid for
    that once (the writer half carries the incident). A writer that emits one
    line cannot produce the wrapped shape at all.

    Returns `""` for no refs, so a caller can omit the key entirely rather than
    writing `refs: []` — an empty list and an absent key mean the same thing and
    the absent one is what 120 of 120 existing entries carry today.

    🔴 EVERY INPUT IS RE-PARSED, INCLUDING A `TaskRef`. An earlier version
    short-circuited on `isinstance(r, TaskRef)` and re-parsed only bare strings —
    which left the claim below false, because `TaskRef` is an exported frozen
    dataclass with no validation of its own: `TaskRef("clickup", "a,b", "x")`
    constructs happily and rendered `refs: [clickup:a,b]`, a line the inline
    reader splits into `clickup:a` and `b` — MALFORMED entry, invisible to every
    reader. A hand-built `TaskRef` carrying a newline was worse still.

    So the shortcut is gone. `parse_task_ref` is the single validator and it runs
    on the way out as well as the way in, which is what makes "accepted by the
    writer" and "accepted by the reader" the same set rather than merely a
    sentence claiming they are. Re-parsing a `TaskRef` is a few microseconds
    against a function that writes one line per entry.
    """
    items = [str(parse_task_ref(str(r))) for r in refs]
    if not items:
        return ""
    return f"refs: [{', '.join(items)}]"


def lossy_tag_for(ref: TaskRef) -> str:
    """A ref -> the flattened `<system>:<slug>` shape a TAG surface can hold.

    🔴 DERIVATION ONLY. There is deliberately no inverse, and adding one would be
    a bug rather than a feature. The downstream tag grammar is `[a-z0-9._/-]`, at most
    one colon, 64 runes — `#` is illegal — so a GitHub ref must lose structure to
    become a tag at all, and the flattening is not injective:

        github:example-org/gamma-infra#429   ->  github:example-org-gamma-infra-429
        github:example-org-gamma/infra#429   ->  github:example-org-gamma-infra-429

    Two distinct refs, one tag — a silent correlation collapse. So the lossless
    ref is the source of truth and
    the tag is computed FROM it on the way out; anything that parses a tag back
    into a ref is inventing one of the two originals with even odds.

    🔴 THE GRAMMAR IS ENFORCED, NOT MERELY DESCRIBED. A docstring that states a
    constraint the code does not apply is a claim no reader can rely on, and the
    two failures here are reachable from refs `parse_task_ref` accepts:

      * an ident of pure punctuation (`github:###`) normalizes to the empty
        string, yielding the tag `github:` — an EMPTY slug half that collides
        with every other such ref, which is the correlation collapse again;
      * a long ident (a deep path, a 200-character id) exceeds 64 runes.

    Both raise rather than returning a tag the destination will reject or, worse,
    silently truncate. Raising is the correct direction: the caller has a
    lossless ref in hand and can decide, whereas a bad tag propagates.

    ⚠ `TAG_MAX_RUNES` is the downstream surface's limit, restated here because this function's
    output is destined for it. It is not enforced anywhere else in this module.
    """
    slug = normalize_ref(ref.ident)
    if not slug:
        raise TaskRefError(
            f"task ref {str(ref)!r} has an id half that normalizes to the empty "
            f"string, so it has no distinguishable tag form"
        )
    tag = f"{ref.system}{_TASK_REF_SPLIT}{slug}"
    if len(tag) > TAG_MAX_RUNES:
        raise TaskRefError(
            f"the tag form of {str(ref)!r} is {len(tag)} runes, over the "
            f"{TAG_MAX_RUNES}-rune tag limit — the ref itself is still valid; "
            f"only its lossy tag encoding is not"
        )
    return tag


def entry_references(entry: "SubsystemEntry", want: TaskRef) -> bool:
    """Does this entry carry this ref — the REVERSE LOOKUP's one predicate.

    🔴 ONE FUNCTION, BECAUSE THE FILTER RUNS AT SEVERAL CALL SITES AND A PREDICATE
    OPEN-CODED AT N SITES IS WRONG AT N-1. `recall` narrows a scope's entries, `search`
    narrows each searched scope's entries, and `internal/store.EntryReferences` is the
    Go spelling; a further caller asking "which entries reference this" must reach this
    and not re-derive it.

    🔴 THE SYSTEM HALF IS COMPARED NORMALIZED AND THE ID HALF BYTE-IDENTICALLY, WHICH IS
    THE SCHEMA'S OWN ASYMMETRY RATHER THAN A CHOICE MADE HERE. `parse_task_ref`
    normalizes the system half (`GitHub:` and `github:` are one system) and preserves the
    id half exactly, because a tracker's id may be case-sensitive and folding it would
    make two different tasks compare equal. So a query for
    `GitHub:example-org/example-repo#428` finds an entry written `github:…`, and a query
    for `clickup:ABC` does NOT find `clickup:abc`. Both halves of that are load-bearing;
    passing both sides through `parse_task_ref` applies the asymmetry once.
    """
    return any(
        have.system == want.system and have.ident == want.ident for have in entry.tasks
    )


def entry_has_tag(entry: "SubsystemEntry", want: str) -> bool:
    """Does this entry carry this folded tag — the `?tag=`/`--tag` predicate.

    🔴 ONE TAG, BECAUSE THE PARAMETER IS SCALAR, AND THE OPERATOR DECIDED THAT RATHER THAN A
    GATE. A repeatable `--tag` was built first, with AND semantics; nobody had asked for
    either, and neither the repetition nor the choice between AND and OR had an author of
    record. The surface is one tag, so there is no set to quantify over and no semantics to
    pick — which is also what makes reading ONE value out of a query string correct rather
    than merely usual.

    🔴 ONE FUNCTION, for the reason `entry_references` gives: the filter runs at several call
    sites and a predicate open-coded at N sites is wrong at N-1. `recall` narrows a scope's
    entries, `search` narrows each searched scope's, and `internal/store.EntryHasTag` is the
    Go spelling — which `internal/ui.EntriesByTag` reaches too, because a browser listing
    that open-coded membership would be the third site the rule names.

    ⚠ THE OPERAND IS NOT CHECKED AGAINST THE CLOSED WRITE VOCABULARY, AND THAT IS DELIBERATE
    RATHER THAN AN OVERSIGHT. `entry_shape.TAG_VOCABULARY` decides what a WRITE may land; a
    read filter must still be able to name a tag the store already carries — including one
    written before the vocabulary closed — or the entries an operator most needs to find
    would be the ones they could not ask for. So `?tag=`/`--tag` takes any folded token, a
    zero is the honest answer for an undeclared one, and the rendered non-finding says so.

    ⚠ MEMBERSHIP AND NEVER A PREFIX: a tag is a whole folded token, so `market` does not find
    `marketing`. The fold is `normalize_ref` on BOTH sides, applied once per side.
    """
    return want in entry.tags


# --- The shared predicate ------------------------------------------------------


def normalize_ref(raw: str) -> str:
    """Fold a ref/slug/alias/path-component to its canonical form.

    The rule, from `claude/skills/analyze-service/SKILL.md`: lowercase, `_` → `-`,
    any other char outside `[a-z0-9.-]` → `-`, collapsed, trimmed. Applied
    identically on read and write and to `aliases:` before comparing.

    `.` SURVIVES — it is inside the character class. That is what lets kind
    qualification (`<slug>.<kind>`) and dotted slugs work at all.

    Returns "" for input that normalizes away entirely; every caller treats an
    empty result as "not a ref", never as a wildcard.
    """
    s = raw.strip().lower()
    s = _NON_SLUG.sub("-", s)  # `_` folds here too — see the note on _NON_SLUG
    s = _DASH_RUN.sub("-", s)
    return s.strip("-")


def split_kind(ref: str) -> tuple[str, str | None]:
    """Split an ALREADY-NORMALIZED ref into (slug, kind|None).

    A trailing dot-segment is a kind only if it is in `KINDS`; otherwise the
    whole ref is the slug. A leading-dot ref (`.process`) has no slug, so it is
    not a kind qualification either.

    Used for BOTH index filenames and incoming refs, on purpose — one splitter,
    so a ref and the filename it should reach can never disagree about where the
    kind boundary is.
    """
    head, sep, tail = ref.rpartition(".")
    if sep and head and tail in KINDS:
        return head, tail
    return ref, None


def path_refs(path: str) -> tuple[tuple[str, str], ...]:
    """Candidate `(raw_component, normalized_ref)` pairs for one repo-relative path.

    Every path component contributes its normalized self. The FINAL component
    additionally contributes its stem (one extension stripped), so
    `apps/minio/values.yaml` offers `values` as well as `values.yaml`, and a file
    named `pg_hero.yaml` reaches the `pghero` entry via its `pg_hero` alias.

    Only ONE extension is stripped: `foo.tar.gz` offers `foo.tar`, not `foo`.
    Stripping greedily would let any dotted filename impersonate a short slug.

    Duplicate pairs are collapsed, insertion order preserved, so a path like
    `redis/redis/values.yaml` counts once per distinct (component, ref).
    """
    parts = [p for p in path.split("/") if p not in ("", ".")]
    out: dict[tuple[str, str], None] = {}
    for i, part in enumerate(parts):
        ref = normalize_ref(part)
        if ref:
            out[(part, ref)] = None
        if i == len(parts) - 1 and "." in part.strip("."):
            stem = part.rsplit(".", 1)[0]
            stem_ref = normalize_ref(stem)
            if stem_ref:
                out[(stem, stem_ref)] = None
    return tuple(out)


# --- The index -----------------------------------------------------------------


@dataclass(frozen=True)
class SubsystemEntry:
    """One `/analyze-service` index entry, normalized and validated."""

    slug: str
    """Normalized slug — the filename's slug part, `service:` and filename agreeing."""

    kind: str | None
    """From a kind-qualified filename (`<slug>.<kind>.md`); None for a bare `<slug>.md`."""

    scope: str
    """Normalized owning scope. `repo:` is read as `scope:` (older files carry it)."""

    aliases: tuple[str, ...]
    """Normalized, deduped, sorted. Deduping is load-bearing — see `from_mapping`."""

    raw_aliases: tuple[str, ...]
    """As written in the file, for evidence and for reporting a candidate honestly."""

    filename: str
    """`<slug>.md` or `<slug>.<kind>.md` — the name a candidate list must show."""

    tags: tuple[str, ...] = ()
    """The `tags:` this entry carries — normalized, deduped and SORTED.

    🔴 THE VOCABULARY IS OPEN **TO THIS PARSER** AND CLOSED ON THE WRITE PATH, AND THE
    ASYMMETRY IS THE DESIGN RATHER THAN A GAP. The declared set is
    `entry_shape.TAG_VOCABULARY`, enforced by `server.py`'s `_validate_entry_bytes`, so a
    `PUT` carrying a tag outside it answers 422 and nothing lands. This parser accepts ANY
    folded tag and must keep doing so: a refusal here makes the entry MALFORMED, which takes
    it out of the index, out of `--ref`, out of `--search` AND out of every write route
    (they resolve their target through the index), so closing the vocabulary HERE would make
    every entry already carrying an off-vocabulary tag unreadable and unrepairable in one
    stroke. The read side therefore still sees an open set, and the residual cost is named
    rather than hidden: a tag written before the vocabulary closed, or written by some other
    tool straight onto the disk, is a category of one that no read can catch.

    ⚠ IT IS A DIFFERENT AXIS FROM `kind`, WHICH IS WHY THERE ARE TWO CLOSED SETS AND NOT
    ONE. `kind` is service/process/org/doc — what SHAPE of thing the entry describes; the
    tag axis is the technical domain it belongs to. An entry carries one value from each and
    neither set refines the other, so conflating them in one enum would make both
    unassertable.

    ⚠ SORTED, WHERE `tasks` KEEPS FILE ORDER. A ref list is hand-maintained and has an
    author's ordering, so re-sorting it would make every read-write cycle a diff; a tag set
    has no ordering to preserve and IS rendered — the `tag:` header line and the `?tag=`
    links built from it must be byte-identical across two runs over an unchanged store.
    `aliases` is the field this copies.

    ⚠ THERE IS NO `raw_tags`, WHERE `aliases` HAS `raw_aliases`. An alias is an ADDRESS, so
    the spelling an operator wrote is evidence when two of them collide; a tag is a GROUPING
    and the only thing any surface renders or links to is the folded form.

    Defaulted and appended LAST for the reason `tasks` is: every existing construction site,
    the test suite included, builds an entry without it.
    """

    tasks: tuple[TaskRef, ...] = ()
    """The `refs:` this entry carries, in FILE ORDER, deduped, never normalized.

    ⚠ THE FIELD KEEPS THE OLDER SPELLING WHILE THE KEY CHANGED, AND THAT IS A DECISION
    RATHER THAN AN OVERSIGHT. The KEY is `refs:` (with `tasks:`/`task:` as PERMANENTLY
    ACCEPTED aliases); renaming this attribute would break every construction site in the suite
    and every read in `subsystem_recall`, in a change whose whole blast-radius claim is
    "additive", for no behavioural difference. `internal/store`'s `Entry.Tasks` carries
    the same note for the same reason.


    Defaulted and appended LAST on purpose: every existing construction site —
    including the ones in the test suite — builds an entry without it, and a
    field with no default in the middle of the list would break all of them for
    a key that 120 of 120 live entries do not carry.

    Order is the file's, not sorted, because a hand-maintained list has an author's
    ordering and re-sorting it would make every read-write cycle a diff.
    """

    @property
    def ref(self) -> str:
        """The canonical ref that addresses this entry unambiguously."""
        return f"{self.slug}.{self.kind}" if self.kind else self.slug

    @classmethod
    def from_mapping(cls, mapping: Mapping[str, object], *, source: str = "<in-memory>") -> "SubsystemEntry":
        """Validate one entry mapping. Every rejection says 'malformed index entry'.

        Accepted keys: `service` (required), `scope` or `repo` (required, one of),
        `aliases` (optional sequence), `kind` (optional), `filename` (optional —
        supplied by the loader, otherwise derived), `refs` (optional sequence of
        `<system>:<id>` refs) with `tasks` (older sequence) and `task` (older
        scalar sugar for a one-element list) as PERMANENTLY ACCEPTED aliases.

        🔴 EVERY OTHER KEY IS IGNORED, NOT REFUSED, AND THAT IS MEASURED ON BOTH
        IMPLEMENTATIONS RATHER THAN READ OFF THIS FUNCTION'S BODY. It reads only the
        keys it names and never enumerates the mapping, which is a CODE reading; the
        measurement is `test_unknown_front_matter_keys_are_ignored`, which hands it
        `tags:`, `refs:` and a nonsense key in turn beside a valid `{service, scope}`
        and asserts all three load, with a no-`service:` mapping in the same test as
        the negative control proving the probe can observe a refusal. The Go loader was
        probed the same way and agrees. That pair is what makes `refs:`
        backward-compatible: an older reader handed a `refs:` file loads it and reports
        no refs, rather than refusing the file.
        """

        def bad(why: str) -> MalformedEntryError:
            return MalformedEntryError(
                f"malformed index entry {source!r}: {why}", source=source, why=why
            )

        raw_service = mapping.get("service")
        if not isinstance(raw_service, str) or not raw_service.strip():
            raise bad("missing or empty `service:` — an entry with no name cannot be addressed")
        slug_all = normalize_ref(raw_service)
        if not slug_all:
            raise bad(f"`service: {raw_service!r}` normalizes to the empty string")

        raw_scope = mapping.get("scope")
        if raw_scope is None:
            raw_scope = mapping.get("repo")
        if not isinstance(raw_scope, str) or not raw_scope.strip():
            raise bad("missing or empty `scope:` (or the older `repo:`)")
        scope = normalize_ref(raw_scope)
        if not scope:
            raise bad(f"`scope: {raw_scope!r}` normalizes to the empty string")

        # Kind may arrive from the filename, the front matter, or both. Both must
        # agree: a `kind:` field the filename contradicts is a claim about the
        # entry that nothing enforces, and `claude/RULES.md` is explicit that a
        # declaration which no code path honours is worse than no declaration.
        filename = mapping.get("filename")
        file_kind: str | None = None
        if filename is not None:
            if not isinstance(filename, str) or not filename.endswith(".md"):
                raise bad(f"`filename` {filename!r} is not a `.md` name")
            file_slug, file_kind = split_kind(normalize_ref(filename[: -len(".md")]))
            if file_slug != slug_all:
                raise bad(
                    f"filename {filename!r} has slug {file_slug!r} but `service:` normalizes to "
                    f"{slug_all!r} — the two must agree or a ref reaches the wrong file"
                )

        declared_kind = mapping.get("kind")
        if declared_kind is not None:
            if not isinstance(declared_kind, str) or normalize_ref(declared_kind) not in KINDS:
                raise bad(
                    f"`kind: {declared_kind!r}` is not one of {'|'.join(KINDS)}"
                )
            declared_kind = normalize_ref(declared_kind)
        if declared_kind is not None and file_kind is not None and declared_kind != file_kind:
            raise bad(
                f"`kind: {declared_kind!r}` contradicts the filename's kind {file_kind!r}"
            )
        kind = file_kind if file_kind is not None else declared_kind

        # The slug is the filename's SLUG PART. If no filename was supplied and
        # `service:` itself carries a kind suffix, honour it — otherwise
        # `service: repo-cos.process` would produce slug `repo-cos.process`,
        # which no ref could ever reach.
        slug = slug_all
        if filename is None:
            slug, service_kind = split_kind(slug_all)
            if service_kind is not None:
                if kind is not None and kind != service_kind:
                    raise bad(
                        f"`service: {raw_service!r}` carries kind {service_kind!r} but "
                        f"`kind: {kind!r}` was declared"
                    )
                kind = service_kind

        raw_aliases_in = mapping.get("aliases") or ()
        if isinstance(raw_aliases_in, (str, bytes)):
            raise bad("`aliases:` must be a list, not a bare string")
        if not isinstance(raw_aliases_in, _AbcSequence):
            raise bad(f"`aliases:` must be a list, got {type(raw_aliases_in).__name__}")
        raw_aliases: list[str] = []
        normalized: set[str] = set()
        for alias in raw_aliases_in:
            if not isinstance(alias, str) or not alias.strip():
                raise bad(f"alias {alias!r} is not a non-empty string")
            na = normalize_ref(alias)
            if not na:
                raise bad(f"alias {alias!r} normalizes to the empty string")
            raw_aliases.append(alias)
            # DEDUPED, not rejected. The live corpus carries `pghero.md` with
            # BOTH `pg-hero` and `pg_hero` in one `aliases:` list; they fold to
            # one ref. Two spellings of one alias on ONE entry are a single
            # address, not an ambiguity — that distinction is the whole reason
            # ambiguity is measured per ENTRY and never per alias-occurrence.
            normalized.add(na)

        # --- `refs:`, with `tasks:` / `task:` as ACCEPTED ALIASES ---------------
        # 🔴 WHY THE KEY WAS RENAMED, since `tasks:` parsed fine: it carries repos, PRs,
        # docs and dashboards, not only work-tracker items, so `tasks:` NAMED A SUBSET of
        # what it holds. An operator decision, not a green gate. `refs:` is the key to
        # WRITE — `format_task_refs` is the only serializer and emits it.
        #
        # ⚠ NOTHING ANNOUNCES THE OLD SPELLINGS, AND THAT IS A DECISION RATHER THAN AN
        # OVERSIGHT. A per-process stderr notice naming the replacement was built and then
        # DELETED: measured across the two real stores, 0 of 440 entries carried `tasks:` or
        # `task:`, so the line was unreachable in practice and its cross-client coverage was
        # vacuous until a fixture was planted for it. The ALIAS is what operators depend on,
        # and it is pinned by `test_the_older_spellings_still_parse` here and
        # `TestTheOlderRefKeysStillParseAndRefsWins` on the Go side.
        #
        # 🔴 VALIDATED HERE AND NOWHERE ELSE. The writer's own validate pass answers
        # "would the loader accept this file?" by constructing exactly what the
        # loader constructs (see `entry_mapping`), so putting the check here is
        # what makes the validator and the reader agree by construction rather
        # than by two people remembering to edit both. A second spelling at the
        # validator is the duplicated predicate `claude/RULES.md` names.
        #
        # 🔴 THE NEW KEY WINS WITHIN ONE ENTRY, AND IT WINS BY NOT CONSULTING THE OLD
        # ONES AT ALL. It decides the one case that is
        # not obvious: an entry carrying `refs:` AND BOTH older spellings does NOT
        # hit the "both `tasks:` and `task:` are set" refusal below, because neither is
        # read. That refusal is about two spellings DISAGREEING over what the entry's
        # refs are; with `refs:` present the entry says exactly one thing, and refusing
        # a file whose meaning is unambiguous is the opposite of what an ACCEPTED
        # alias is for. The refusal is UNCHANGED for every entry that reaches it
        # (`refs:` absent), which is every file the conformance corpus sends.
        raw_refs_in = mapping.get("refs")
        raw_tasks_in = mapping.get("tasks")
        raw_task_in = mapping.get("task")
        if not raw_refs_in and raw_tasks_in and raw_task_in:
            raise bad(
                "both `tasks:` and `task:` are set — `task:` is sugar for a "
                "one-element `tasks:`; keep one of them"
            )
        if raw_refs_in:
            if isinstance(raw_refs_in, (str, bytes)):
                # `refs:` is a LIST by definition, so a scalar there is a mistake
                # worth naming rather than silently flattening.
                raise bad(
                    "`refs:` must be a list, not a bare string — write "
                    "`refs: [<system>:<id>]`"
                )
            if not isinstance(raw_refs_in, _AbcSequence):
                raise bad(f"`refs:` must be a list, got {type(raw_refs_in).__name__}")
            task_items: object = raw_refs_in
        elif raw_tasks_in:
            # `task:` is a SCALAR by definition, so a list there is a mistake worth
            # naming rather than silently flattening.
            task_items = raw_tasks_in
        elif raw_task_in:
            if not isinstance(raw_task_in, str):
                raise bad(
                    f"`task:` must be a single `<system>:<id>` ref, got "
                    f"{type(raw_task_in).__name__} — use `tasks: [...]` for several"
                )
            task_items = [raw_task_in]
        else:
            task_items = []
        if isinstance(task_items, (str, bytes)):
            raise bad(
                "`tasks:` must be a list, not a bare string — write "
                "`tasks: [<system>:<id>]`, or use `task:` for a single ref"
            )
        if not isinstance(task_items, _AbcSequence):
            raise bad(f"`tasks:` must be a list, got {type(task_items).__name__}")
        tasks: list[TaskRef] = []
        seen_tasks: set[tuple[str, str]] = set()
        for item in task_items:
            try:
                ref = parse_task_ref(item)
            except TaskRefError as exc:
                # The ref's own message already names the fix; `bad` prefixes the
                # source, so the operator gets file AND remedy in one line.
                raise bad(str(exc)) from exc
            # Deduped, not rejected — the same reasoning as `aliases:` above. One
            # task written twice is one task, and refusing the file over it would
            # make a harmless duplicate invisible-until-fixed.
            key = (ref.system, ref.ident)
            if key in seen_tasks:
                continue
            seen_tasks.add(key)
            tasks.append(ref)

        # --- `tags:` — the CATEGORY axis, which `kind` is not -------------------
        # 🔴 IT COPIES `aliases:`' SHAPE, WHICH IS ALREADY PROVEN IN THIS PARSER, AND THAT
        # IS WHY THIS KEY NEEDED NO MIGRATION. The three answers are the same: absent or an
        # EMPTY scalar yields nothing (`or ()` is what makes a bare `tags:` an absent key
        # rather than a bare string), a non-empty SCALAR is a named refusal, and a list is
        # itself. An older reader handed a `tags:` file loads it and reports no tags, because
        # `from_mapping` reads only the keys it names — measured on both implementations.
        #
        # 🔴 ONE REFUSAL FOR A TAG'S CONTENT, WHERE `aliases:` HAS TWO, AND THE DIFFERENCE IS
        # DELIBERATE. The alias loop refuses a whitespace-only item ("is not a non-empty
        # string") AND one that folds away ("normalizes to the empty string"); the first is a
        # SUBSET of the second, since `normalize_ref` returns "" for anything whose every
        # character is outside `[a-z0-9.-]`. A second sentence would be a branch nothing can
        # reach that the first does not, and an operator reading two refusals for one mistake
        # has to work out which they hit. The refusal a tag CAN hit names the FOLD, which is
        # the thing they cannot see.
        #
        # 🔴 AND THE CLOSED VOCABULARY DID NOT MAKE IT TWO, WHICH IS THE CLAIM ABOVE
        # SURVIVING A CHANGE THAT LOOKS LIKE IT SHOULD HAVE BROKEN IT. The declared tag set
        # is enforced by `server.py`'s `_validate_entry_bytes`, NOT here, so the count of
        # refusals THIS parser can raise about a tag's content is still exactly one. A
        # reader that refused an undeclared tag would emit a SECOND content refusal — and
        # the worst possible one, because unlike the fold it fires on entries that already
        # exist and already load, making them malformed and therefore unwritable (the write
        # routes resolve through the index). See `entry_shape.TAG_VOCABULARY` for why that
        # outage is what put the check in the module this one CANNOT import.
        #
        # ⚠ AND A NORMALIZED-AWAY TAG IS REFUSED RATHER THAN DROPPED. Dropping it would leave
        # a file that DECLARES a category and an index that does not carry it, so `?tag=`
        # would answer "no entry carries this" about an entry whose front matter says it does
        # — an empty result whose cause is invisible at both ends.
        #
        # 🔴 THE SHAPE IS VALIDATED HERE AND NOWHERE ELSE, which is the ruling the refs block
        # above states: the writer's validate pass answers "would the loader accept this
        # file?" by building exactly what the loader builds, so the shape check belongs here
        # or the two disagree.
        #
        # ⚠ "HERE AND NOWHERE ELSE" IS ABOUT THE SHAPE, NOT ABOUT THE VALUE, AND THE
        # DISTINCTION IS NEW. The writer's pass is now STRICTLY STRONGER than the loader by
        # exactly one rule — the closed vocabulary — and that is the one place the two are
        # MEANT to disagree, for the reason the block above gives. Anything else the writer
        # refuses that a reader accepts is a defect.
        raw_tags_in = mapping.get("tags") or ()
        if isinstance(raw_tags_in, (str, bytes)):
            raise bad("`tags:` must be a list, not a bare string — write `tags: [<name>]`")
        if not isinstance(raw_tags_in, _AbcSequence):
            raise bad(f"`tags:` must be a list, got {type(raw_tags_in).__name__}")
        normalized_tags: set[str] = set()
        for tag in raw_tags_in:
            nt = normalize_ref(tag) if isinstance(tag, str) else ""
            if not nt:
                raise bad(
                    f"tag {tag!r} normalizes to the empty string — a tag must fold to at "
                    f"least one of `[a-z0-9.-]`"
                )
            # DEDUPED, not rejected, for the reason `aliases:` is: two spellings of one tag
            # on ONE entry are a single category, not a conflict.
            normalized_tags.add(nt)

        derived_filename = f"{slug}.{kind}.md" if kind else f"{slug}.md"
        return cls(
            slug=slug,
            kind=kind,
            scope=scope,
            aliases=tuple(sorted(normalized)),
            raw_aliases=tuple(raw_aliases),
            filename=filename if isinstance(filename, str) else derived_filename,
            tasks=tuple(tasks),
            tags=tuple(sorted(normalized_tags)),
        )


@dataclass(frozen=True)
class SubsystemIndex:
    """Entries grouped by scope. A scope may legitimately be present but EMPTY.

    That distinction is load-bearing: an existing-but-empty scope dir yields an
    honest empty result, while a scope the index has never heard of raises
    `UnknownScopeError`. Collapsing the two would turn a typo'd scope into
    "0 subsystems", which is the exact silent zero this work exists to avoid.
    """

    by_scope: Mapping[str, tuple[SubsystemEntry, ...]]

    malformed: tuple[MalformedEntry, ...] = ()
    """Entries that were REJECTED, when the index was built with `COLLECT`.

    Always empty under the default `RAISE` — there the first rejection is the
    whole answer. It is a field on the index rather than a second return value so
    that "the entries" and "what could not become an entry" cannot be separated
    by a caller that only unpacks the first thing: a reader holding a
    `SubsystemIndex` is holding the bad news too, whether or not it asked.
    """

    @property
    def scopes(self) -> tuple[str, ...]:
        return tuple(sorted(self.by_scope))

    def malformed_in(self, scope: str) -> tuple[MalformedEntry, ...]:
        """The rejected entries belonging to ONE scope, normalized like every ref.

        No `UnknownScopeError`: this answers "what is broken here", and an
        unknown scope has nothing broken in it. Raising would force every caller
        to wrap a question it asks on every code path.
        """
        key = normalize_ref(scope)
        return tuple(m for m in self.malformed if m.scope == key)

    def malformed_outside(self, scopes: Iterable[str]) -> tuple[MalformedEntry, ...]:
        """The rejected entries in EVERY OTHER scope — the store-wide defect count.

        A reader is scope-scoped, so without this a broken entry in a scope
        nobody happens to recall today is invisible until someone recalls it. The
        surfaces print it as a COUNT with its scopes named, never as full rows:
        loud enough to be actionable, cheap enough to sit on every output.
        """
        keys = {normalize_ref(s) for s in scopes}
        return tuple(m for m in self.malformed if m.scope not in keys)

    def entries(self, scope: str) -> tuple[SubsystemEntry, ...]:
        key = normalize_ref(scope)
        if key not in self.by_scope:
            known = ", ".join(self.scopes) or "(none)"
            raise UnknownScopeError(
                f"unknown scope {scope!r} (normalized {key!r}); the index holds: {known}"
            )
        return self.by_scope[key]

    def __len__(self) -> int:
        return sum(len(v) for v in self.by_scope.values())


def _rejection(
    mapping: Mapping[str, object], source: str, exc: MalformedEntryError
) -> MalformedEntry:
    """Turn one raise into one row. The scope comes from the MAPPING, deliberately.

    On a disk load `scope` was set from the directory name before validation ran,
    so it is known even for an entry too broken to construct — which is what lets
    a rejected entry be reported against the scope it lives in instead of against
    the store at large. An in-memory mapping with no usable scope yields `""`,
    and `malformed_in` then matches no scope, so such a row surfaces only through
    the store-wide count. Stated rather than left to be discovered.
    """
    raw_scope = mapping.get("scope")
    if not isinstance(raw_scope, str) or not raw_scope.strip():
        raw_scope = mapping.get("repo")
    scope = normalize_ref(raw_scope) if isinstance(raw_scope, str) else ""
    filename = mapping.get("filename")
    return MalformedEntry(
        scope=scope,
        filename=filename if isinstance(filename, str) and filename else source,
        reason=exc.why,
    )


def _check_on_malformed(on_malformed: str) -> str:
    """Validate the policy name and return it. ONE place, TWO callers.

    `load_index` has to branch on this policy BEFORE `build_index` ever sees it
    (its entry-kind guard decides raise-vs-collect for itself), so without this
    the predicate would be spelled at two sites — and the site that got it wrong
    would answer a bogus policy string with a `MalformedEntryError` about the
    first hostile file instead of "you passed a policy that does not exist".
    """
    if on_malformed not in ON_MALFORMED:
        raise ValueError(
            f"on_malformed must be one of {ON_MALFORMED}, got {on_malformed!r}"
        )
    return on_malformed


def build_index(
    mappings: Iterable[Mapping[str, object]],
    *,
    extra_scopes: Iterable[str] = (),
    on_malformed: str = ON_MALFORMED_RAISE,
) -> SubsystemIndex:
    """Validate and group entry mappings.

    `extra_scopes` registers scopes that exist but hold no entries (a scope dir
    the loader found empty), so they resolve to an empty result rather than
    `UnknownScopeError`.

    `on_malformed` picks the POLICY, and the default keeps the historical one:

      `RAISE`   — the first rejection aborts the whole build. Right for a WRITER,
                  which is about to modify a curated store and must not act on a
                  partial picture of it.
      `COLLECT` — a rejection becomes a `MalformedEntry` on the index and the
                  build continues. Right for a READER, where aborting spends
                  every good entry in the scope to report one bad one. **A
                  collecting caller MUST print what it collected**; see
                  `ON_MALFORMED`'s note.

    🔴 BOTH REJECTION SITES COLLECT — the per-entry validator AND the duplicate
    check. A duplicate is a relationship between two files, and the one recorded
    is the LATER of the pair in the loader's sorted order, so the first spelling
    of a ref keeps serving and the collision is still named. Collecting only the
    first site would have left `COLLECT` able to raise, which is the shape a
    caller cannot defend against because it looks handled.
    """
    collecting = _check_on_malformed(on_malformed) == ON_MALFORMED_COLLECT
    by_scope: dict[str, list[SubsystemEntry]] = {}
    malformed: list[MalformedEntry] = []
    seen: dict[tuple[str, str, str | None], str] = {}
    for mapping in mappings:
        source = str(mapping.get("filename") or mapping.get("service") or "<unnamed>")
        try:
            entry = SubsystemEntry.from_mapping(mapping, source=source)
        except MalformedEntryError as exc:
            if not collecting:
                raise
            malformed.append(_rejection(mapping, source, exc))
            continue
        key = (entry.scope, entry.slug, entry.kind)
        if key in seen:
            dup = MalformedEntryError(
                f"malformed index entry {source!r}: duplicate {entry.ref!r} in scope "
                f"{entry.scope!r} — already defined by {seen[key]!r}",
                source=source,
                why=(
                    f"duplicate {entry.ref!r} in scope {entry.scope!r} — already defined "
                    f"by {seen[key]!r}"
                ),
            )
            if not collecting:
                raise dup
            malformed.append(_rejection(mapping, source, dup))
            continue
        seen[key] = entry.filename
        by_scope.setdefault(entry.scope, []).append(entry)
    # 🔴 A SCOPE THAT HOLDS ONLY BROKEN ENTRIES STILL EXISTS. Without this the
    # scope would be unknown to the index and a reader would answer `scope-absent`
    # — "nothing recorded yet" — about a directory full of content it simply
    # could not parse. That is the exact conflation this store guards against
    # everywhere else. (On a disk load `extra_scopes` already registers every
    # directory; this covers `build_index` called directly.)
    for m in malformed:
        if m.scope:
            by_scope.setdefault(m.scope, [])
    for scope in extra_scopes:
        key = normalize_ref(scope)
        if key:
            by_scope.setdefault(key, [])
    return SubsystemIndex(
        by_scope={k: tuple(v) for k, v in by_scope.items()},
        malformed=tuple(malformed),
    )


# --- Resolution ----------------------------------------------------------------


def resolve_ref_tiered(
    ref: str, index: SubsystemIndex, scope: str
) -> tuple[SubsystemEntry | None, str | None]:
    """Resolve one ref within one scope, reporting WHICH tier hit.

    The direct port of the #362 rule. Two tiers; an alias can never outrank a
    filename:
      1. FILENAME — normalized ref vs `<slug>.md` and every `<slug>.<kind>.md`.
         A ref naming its own kind (`repo-cos.process`) matches ONLY that
         qualified file.
      2. ALIAS — normalized `aliases:` across the scope, consulted **only if
         tier 1 returned zero hits**.

    One hit → `(entry, tier)`. >1 in a tier → `AmbiguousRefError` listing
    candidates, never a pick. Zero in both → `(None, None)` — an honest miss,
    not an error.

    The tier is RETURNED rather than re-derived by the caller: `associate_paths`
    needs it for evidence, and a second expression computing "was that a
    filename or an alias hit?" is the same predicate twice, one edit from
    disagreeing with the branch that actually chose the entry.
    """
    entries = index.entries(scope)
    nref = normalize_ref(ref)
    # ⚠ REDUNDANT-BUT-KEPT, and labelled so the next mutation sweep does not
    # re-derive it: no entry can have an empty slug (`from_mapping` rejects one),
    # so an empty ref would miss both tiers anyway. This is a cheap short-circuit
    # that states the intent, not a guard — mutating it away changes nothing.
    if not nref:
        return None, None

    slug, kind = split_kind(nref)
    if kind is not None:
        hits = [e for e in entries if e.slug == slug and e.kind == kind]
    else:
        hits = [e for e in entries if e.slug == nref]
    if len(hits) > 1:
        raise AmbiguousRefError(nref, "filename", sorted(e.filename for e in hits), scope)
    if hits:
        return hits[0], "filename"

    hits = [e for e in entries if nref in e.aliases]
    if len(hits) > 1:
        raise AmbiguousRefError(nref, "alias", sorted(e.filename for e in hits), scope)
    if hits:
        return hits[0], "alias"
    return None, None


def resolve_ref(ref: str, index: SubsystemIndex, scope: str) -> SubsystemEntry | None:
    """`resolve_ref_tiered` without the tier — the plain "which entry is this?"."""
    return resolve_ref_tiered(ref, index, scope)[0]


# --- Association ---------------------------------------------------------------


@dataclass(frozen=True)
class Evidence:
    """Why one path was attributed to one entry."""

    path: str
    component: str
    """The RAW path component (or filename stem) that produced the ref."""
    ref: str
    """The component after normalization — what was actually compared."""
    tier: str
    """`"filename"` or `"alias"`."""
    matched_alias: str | None = None
    """The alias as WRITTEN in the entry, when `tier == "alias"`."""


@dataclass(frozen=True)
class SubsystemMatch:
    entry: SubsystemEntry
    paths: tuple[str, ...]
    evidence: tuple[Evidence, ...]

    @property
    def path_count(self) -> int:
        return len(self.paths)


@dataclass(frozen=True)
class AmbiguousRef:
    """A ref that could not be resolved because it named more than one entry."""

    ref: str
    tier: str
    candidates: tuple[str, ...]
    paths: tuple[str, ...]
    """The paths that produced it — so a human can see what would have been tagged."""


@dataclass(frozen=True)
class Association:
    """The full, accounted-for result. Nothing is dropped silently.

    `matched + below_threshold + ambiguous + unmatched_paths` is the complete
    story of the input: a caller reading only `matched` still has somewhere to
    look for the difference between "nothing was touched" and "something was
    touched and discarded".
    """

    scope: str
    min_paths: int

    matched: tuple[SubsystemMatch, ...]
    """Subsystems that cleared `min_paths`, in canonical ref order.

    🔴 `if not assoc.matched:` CONFLATES THE TWO ZEROS. It is empty both when no
    paths were supplied and when paths were supplied and matched nothing — and
    downstream those mean opposite things ("we have no reading" vs "this
    subsystem was genuinely not touched"). Use `looked_at_nothing` to separate
    them; `considered_paths` is the underlying evidence.
    """

    below_threshold: tuple[SubsystemMatch, ...]
    ambiguous: tuple[AmbiguousRef, ...]
    unmatched_paths: tuple[str, ...]
    considered_paths: tuple[str, ...]
    """Every path actually examined, deduped, in input order. The discriminator."""

    @property
    def looked_at_nothing(self) -> bool:
        """True when NO paths were supplied — as distinct from "matched nothing".

        The discriminator is `considered_paths`; this names it, so a consumer
        does not have to know which field carries the distinction or re-derive
        `len(considered_paths) == 0` at every call site (one rule, one place).

        🔴 It is an AFFORDANCE, not a gate. Nothing can force a consumer to
        consult it — that was the one thing raising an exception did buy, and it
        was traded away deliberately because it made the ordinary case
        exceptional. In particular `if not assoc.matched:` still conflates the
        two zeros; see the note on `matched`.
        """
        return not self.considered_paths

    @property
    def subsystem_refs(self) -> tuple[str, ...]:
        """Canonical refs of the subsystems that CLEARED the threshold.

        Already in canonical (ref-sorted) order — `associate_paths` establishes
        it once, in ONE place. This used to re-`sorted()` here, and an
        independent mutation sweep found that mutation unkillable: two sites
        asserting one ordering means neither can be observed to be wrong.
        """
        return tuple(m.entry.ref for m in self.matched)


def _validate_path(path: object) -> str:
    if not isinstance(path, str) or not path.strip():
        raise InvalidPathError(f"invalid repo-relative path {path!r}: empty or not a string")
    if path.startswith("/"):
        raise InvalidPathError(
            f"invalid repo-relative path {path!r}: absolute paths drag their prefix "
            f"into the component set (home, zach, workspace, …) and manufacture matches"
        )
    parts = path.split("/")
    if ".." in parts:
        raise InvalidPathError(
            f"invalid repo-relative path {path!r}: `..` escapes the repo root"
        )
    return path


def associate_paths(
    paths: Iterable[str],
    index: SubsystemIndex,
    scope: str,
    *,
    min_paths: int = DEFAULT_MIN_PATHS,
) -> Association:
    """Map changed repo-relative paths onto the subsystems they touch.

    Pure: no I/O, no clock, no globals. `index` is injected.

    Guard order — each is reachable by an input no earlier guard rejects:
      1. `min_paths` sanity            → ValueError
      2. scope known                   → UnknownScopeError
      3. each path repo-relative       → InvalidPathError
    An ambiguous ref does NOT raise here: one undecidable ref must not blind the
    whole session. It is recorded in `.ambiguous`, contributes to no subsystem,
    and a caller that ignores that field is discarding a known unknown.

    🔴 AN EMPTY PATH SET IS NOT AN ERROR. It returns a fully empty, fully
    accounted `Association`. This raised `EmptyPathSetError` until review: the
    argument for raising was that a manufactured zero is indistinguishable from
    a real one, but in P1's per-session call pattern a session with no git
    activity is an ORDINARY input, not an exceptional one. Making the common
    case an exception forces every caller to wrap the call, and a wrapped call
    is how a caller ends up swallowing the genuine errors too.

    The property the exception was protecting is kept STRUCTURALLY instead:
    `considered_paths` distinguishes the two zeros without any exception.

        no paths supplied      → considered_paths == ()   unmatched_paths == ()
        paths, none matched    → considered_paths != ()   unmatched_paths != ()

    So a consumer can still tell "we were given nothing to look at" from "we
    looked and found nothing", which is the whole reason the guard existed.
    Genuine programmer errors — a malformed entry, an unknown scope, a path that
    is not repo-relative — still raise.
    """
    if not isinstance(min_paths, int) or isinstance(min_paths, bool) or min_paths < 1:
        raise ValueError(f"min_paths must be an int >= 1, got {min_paths!r}")

    # Scope guard, before a single path is counted: an unknown scope must not be
    # able to produce a well-formed empty result. (An empty path set may — see
    # the docstring; a typo'd scope may not.)
    index.entries(scope)

    ordered: list[str] = []
    for raw in paths:
        p = _validate_path(raw)
        if p not in ordered:
            ordered.append(p)

    paths_by_entry: dict[str, list[str]] = {}
    evidence_by_entry: dict[str, list[Evidence]] = {}
    entry_by_ref: dict[str, SubsystemEntry] = {}
    ambiguous: dict[tuple[str, str], AmbiguousRef] = {}
    matched_paths: set[str] = set()

    for path in ordered:
        for component, ref in path_refs(path):
            try:
                entry, tier = resolve_ref_tiered(ref, index, scope)
            except AmbiguousRefError as exc:
                key = (exc.ref, exc.tier)
                prev = ambiguous.get(key)
                seen_paths = (prev.paths if prev else ()) + (path,)
                ambiguous[key] = AmbiguousRef(
                    ref=exc.ref,
                    tier=exc.tier,
                    candidates=exc.candidates,
                    paths=tuple(dict.fromkeys(seen_paths)),
                )
                continue
            # `tier` is None exactly when `entry` is — they are returned
            # together — so testing both was defensiveness no input could
            # distinguish (a mutation sweep found the `or` unkillable).
            if entry is None:
                continue

            matched_alias: str | None = None
            if tier == "alias":
                matched_alias = next(
                    (a for a in entry.raw_aliases if normalize_ref(a) == ref), None
                )

            key = entry.ref
            entry_by_ref[key] = entry
            bucket = paths_by_entry.setdefault(key, [])
            if path not in bucket:
                bucket.append(path)
            evidence_by_entry.setdefault(key, []).append(
                Evidence(
                    path=path,
                    component=component,
                    ref=ref,
                    tier=tier,
                    matched_alias=matched_alias,
                )
            )
            matched_paths.add(path)

    matched: list[SubsystemMatch] = []
    below: list[SubsystemMatch] = []
    # 🔴 THE ONE ordering site. Output order is by canonical ref, NOT by the
    # order paths happened to arrive — P1 emits one row per (session, subsystem)
    # and a re-run over the same session must produce the same rows in the same
    # order, or a diff of two runs shows churn that is not there.
    for key in sorted(paths_by_entry):
        m = SubsystemMatch(
            entry=entry_by_ref[key],
            paths=tuple(paths_by_entry[key]),
            evidence=tuple(evidence_by_entry[key]),
        )
        (matched if m.path_count >= min_paths else below).append(m)

    return Association(
        scope=normalize_ref(scope),
        min_paths=min_paths,
        matched=tuple(matched),
        below_threshold=tuple(below),
        ambiguous=tuple(ambiguous[k] for k in sorted(ambiguous)),
        unmatched_paths=tuple(p for p in ordered if p not in matched_paths),
        considered_paths=tuple(ordered),
    )


# --- Entry markdown shape ------------------------------------------------------
#
# 🔴 ONE PARSER, TWO CONSUMERS. `subsystem_recall` (the `/resume` reader) and
# the writer half (the `/handoff` writer) both have to read an entry's prose:
# the reader to surface it, the writer to show what is ALREADY THERE before it
# proposes an append. These functions started in `subsystem_recall`, where they
# were verified against the real 23-entry corpus, and moved DOWN here unchanged
# when the writer needed them — because `subsystem_recall` already imports
# the writer half, so the writer cannot import the reader without closing a
# cycle, and a copy in the writer would be a second parser free to drift from the
# one the corpus was measured against.
#
# They are pure and touch no filesystem, which is why they sit above the disk
# loader with the rest of the pure functions.


WHAT_HEADING = "## What it is"
POINTERS_HEADING = "## Pointers"
NUANCE_HEADING = "## Nuance / work-history"

# The FOURTH canonical section: what the subsystem was SAID to do, with open/met
# state.
#
# 🔴 A SECTION AND NOT A NEW `kind:`, AND NOT A FIRST-CLASS OBJECT. The kind enum
# is closed and asserted, so a fifth kind would move every kind-keyed table; a
# requirement with its own routes and store would be a tracker, and this store is
# deliberately not one. A heading is additive — `_heading_blocks` and
# `extract_sections` are generic over headings — so an entry that has never heard
# of this one parses exactly as it did before.
REQUIREMENTS_HEADING = "## Requirements"

# The three answers `_bullet_provenance` gives. 🔴 ABSENT (None) IS A DECIDED
# ANSWER: "who said this" is the whole feature, so a requirement nobody
# attributed has to stay distinguishable from one attributed to an agent.
PROVENANCE_OPERATOR = "operator"
PROVENANCE_INFERRED = "inferred"

# A top-level journal bullet starts at COLUMN 0. Measured over the whole live
# corpus (26 entries, 110 top-level bullets): every bullet line is
# at indent 0 and every one of the 250 continuation lines is at indent 2. So an
# indented `-` is a CONTINUATION (a nested list, or prose that happens to start
# with a dash), never a new bullet — folding the two together would split one
# bullet into several and report a history longer than the entry has.
_JOURNAL_BULLET = re.compile(r"^[-*][ \t]+")

# `- YYYY-MM-DD: …` — the dated form. The date is OPTIONAL: 62 of those 110
# bullets carry one and 48 do not, so a parser that required a date would drop
# 44% of the real corpus on the floor and call the result a complete read.
_JOURNAL_DATE = re.compile(r"^[-*][ \t]+(\d{4}-\d{2}-\d{2})(?=[:,)\]\s]|$)")

# `- [YYYY-MM-DD: ]OPEN: …` / `- [YYYY-MM-DD: ]RESOLVED <sha>: …` — the openness
# marker, and the reason it is a PREFIX rather than a phrase.
#
# 🔴 WHY THIS IS SCHEMA AND NOT A PROSE DETECTOR. The motivating entry
# (`beta-cluster/forgejo`) carries a bullet proposing a one-line config change
# as future work. That change landed at 15:02:21 (the sha is in the
# client repo and is deliberately not reproduced here);
# the entry was written at
# 15:00:18 — stale 2m03s after it was written, and still being served as an open
# action 22 days later. Nothing could have noticed, because "this remedy is not
# applied yet" was a claim made only in prose.
#
# The obvious repair is to grep the prose for remedy words. Measured over the live
# corpus (196 nuance bullets) that finds TWO bullets — and
# `claude/RULES.md` names the failure it would be: "a guard on WORDS is walkable by
# REWORDING". A writer who says "the endpoint is already correct" instead of
# "FIX:" walks past it, and the walk is silent. A prefix a writer must TYPE cannot
# be walked by rewording the sentence after it; that is the whole reason the
# marker sits before the prose rather than inside it.
#
# `RESOLVED` takes the sha that closed it so the claim is checkable — `git cat-file
# -e <sha>` answers it — rather than being a second unverifiable assertion.
_JOURNAL_OPENNESS = re.compile(
    r"^[-*][ \t]+"
    r"(?:\d{4}-\d{2}-\d{2}:[ \t]+)?"           # the optional leading date
    r"(OPEN|RESOLVED)"
    r"(?:[ \t]+([0-9a-fA-F]{7,40}))?"          # RESOLVED carries the closing sha
    r":"                                        # exact terminator, no fuzz
)

OPENNESS_OPEN = "open"
OPENNESS_RESOLVED = "resolved"

# The retrospective advisory — DELIBERATELY NARROW, and a FLOOR, not a list.
#
# These two shapes are the only ones that scored 2 hits and 0 false positives over
# the 196-bullet corpus. The ones rejected, and why, so nobody re-adds them:
#   `TODO`        1 hit, FALSE — an entry describing an UPSTREAM project's TODO as
#                 a fact about that project, not a remedy this operator owes.
#   `not yet`     3 hits, 2 FALSE — one describes a mechanism (deps not yet on
#                 npm), one is an explicit WONTFIX that says "don't re-litigate".
#   `should be` / `next step` / `pending` / `deferred` / `proposed fix` — 0 hits
#                 each. Adding a marker with no corpus evidence buys recall that
#                 cannot be demonstrated and precision that cannot be defended.
#
# 🔴 RECALL IS UNKNOWN AND MUST BE REPORTED AS SUCH. This finds bullets that
# HAPPEN to be phrased the two ways already seen. It is an advisory that says
# "at least these"; it is never evidence that an entry has no open actions. The
# schema marker above is the mechanism; this is a net under it for entries written
# before the marker existed.
_UNMARKED_ACTION = re.compile(r"\bFIX\s*[(:]|\bnot (yet )?addressed\b", re.I)

# A bullet that MEANT to carry a marker and just missed the grammar.
#
# 🔴 THE SILENT FAILURE IS THE SAME CLASS AS THE ORIGINAL BUG, which is why this
# exists at all. Measured shapes that parse as NO MARKER today:
#
#   - 2026-08-15 OPEN: …                 (date not followed by `:`)
#   - 2026-08-15: RESOLVED abc1234 (repo): …   (parenthetical before the colon)
#   - 2026-08-15: **OPEN:** …            (marker wrapped in emphasis)
#   - 2026-08-15: OPEN : …               (space before the colon)
#   - 2026-08-15: RESOLVED PR#505: …     (a non-sha reference)
#
# The first matters most: `_JOURNAL_DATE` accepts a date followed by any of
# `[:,)\]\s]` while `_JOURNAL_OPENNESS` requires `date:` + whitespace, and **7 of
# 147 dated bullets in the live corpus (4.8%) already use a date form the
# openness regex cannot parse**. So a writer follows the skill, closes an action
# as `RESOLVED <sha> (<repo>):`, the `🔴 N OPEN` badge disappears — which LOOKS
# like success — and the claim is discarded. Symmetrically a typo'd `OPEN`
# reverts the entry to "nothing declared", reintroducing the exact 22-day failure
# the marker exists to prevent, through a typo, silently.
#
# Deliberately NOT fixed by widening `_JOURNAL_OPENNESS`. A lenient marker
# regex starts matching prose, and inventing a marker is worse than missing one:
# a false `RESOLVED` closes an action nobody closed. So the strict grammar stands
# and the near-misses are REPORTED instead, which is the fail-loud half.
#
# ⚠ ONLY THE BARE `^` IS REDUNDANT. The `[-*][ \t]+` after it is LOAD-BEARING in
# BOTH patterns, and two earlier versions of this comment said the opposite.
#
# Measured against the writer half's own full suite (baseline 712 passed):
#     delete `^[-*][ \t]+` from `_JOURNAL_OPENNESS`  → 22 FAILURES
#     delete `^[-*][ \t]+` from `_NEAR_MISS_MARKER`  → 19 FAILURES
#     delete only the `^` from either                → 712 passed (equivalent)
#
# The `^` is redundant because both are consumed with `re.match`, which anchors at
# position 0, over patterns with no `re.MULTILINE`. The bullet marker is not: the
# optional date and punctuation prefixes mean that without it these match happily
# mid-string. A maintainer acting on the old wording — "do not count it as a
# guard" — would have deleted the whole prefix and broken 22 tests.
#
# The `^` stays regardless: it costs nothing, and it is what keeps the anchoring
# true if a consumer ever switches to `search`.
_NEAR_MISS_MARKER = re.compile(
    r"^[-*][ \t]+"
    r"(?:\d{4}-\d{2}-\d{2}[^A-Za-z]{0,3})?"    # a date in any of its corpus forms
    r"[^A-Za-z0-9]{0,4}"                        # `**`, quotes, stray punctuation
    r"(?:"
    # (a) SHOUTED — all-caps marker, no terminator needed. Prose does not shout.
    r"(?:OPEN|RESOLVED)(?![A-Za-z0-9_])"
    r"|"
    # (b) sentence-cased — then a `:` IS required, optionally after a sha /
    #     PR reference / parenthetical / bracketed ref.
    r"(?i:OPEN|RESOLVED)"
    r"(?:[ \t]*(?:[0-9a-fA-F]{7,40}(?![0-9a-fA-F])|PR#\d+|#\d+"
    r"|\([^)]{1,30}\)|\[[^\]]{1,30}\]))*"
    r"[^A-Za-z0-9\n]{0,4}:"
    r")",
)
# 🔴 TWO BRANCHES, BECAUSE THREE ROUNDS PROVED NEITHER ALONE IS ENOUGH — and the
# matrix that says so is COMMITTED at `tests/fixtures/near_miss_shapes.json`
# rather than living in whoever's scratchpad wrote the last version. Each previous
# pattern was justified by a private matrix, and round 4 built a different one and
# reached the opposite verdict; a matrix nobody can re-run is an opinion.
#
# Measured over that fixture (16 attempted shapes, 10 prose shapes) plus the live
# 196-bullet corpus:
#
#     re.I, no terminator    (round 1)    16/16 found   6 prose FP   0 corpus
#     no re.I, no terminator (round 2)     ?/16 found   0 prose FP   0 corpus
#     terminator + re.I      (round 3)     9/16 found   0 prose FP   0 corpus
#     this union                          16/16 found   0 prose FP   0 corpus
#
# Round 3's terminator requirement was the subtler failure: it demanded the exact
# character whose OMISSION is the likeliest way to miss the grammar, so
# `- OPEN the retry budget is not addressed.` went silent — the failure this
# detector exists to prevent, reintroduced by the fix for the previous one.
#
# (a) carries no terminator because an all-caps `OPEN`/`RESOLVED` opening a bullet
# is a marker attempt in every sample examined.
#
# 🔴 THE GUARD IS `(?![A-Za-z0-9_])`, NOT `(?![a-z])`, and the difference is a
# whole class of false positive. `(?![a-z])` blocks only a LOWERCASE continuation,
# so it stopped `Opening` and let through every all-caps identifier a real bullet
# quotes: `OPENSSL_CONF`, `OPEN_MAX`, `RESOLVED_ADDR`, `OPENTELEMETRY`, `OPENED`.
# Each produced a 🔴 "attempted marker that DID NOT PARSE — fix the LINE" advisory
# about a correct sentence, which is how a loud path gets ignored.
#
# ⚠ AND THE FIXTURE COULD NOT SEE IT: the `prose` arm shipped with this branch held
# ZERO all-caps shapes, so the matrix introduced alongside the branch was
# structurally blind to the class the branch introduced. All-caps prose is pinned
# in the fixture now, and a test asserts the arm can still express it — a fixture
# that cannot express the failure mode is not covering it, however green it is.
# (b) needs the terminator because sentence case IS ordinary English — "Open
# questions remain" must not fire, while "Open:" must.
#
# 🔴 THE `(?![0-9a-fA-F])` ON THE HEX ATOM IS A ReDoS FIX, NOT STYLE. `{7,40}`
# inside a `*` loop is exponentially ambiguous when the trailing `:` never
# arrives — which is branch (b) only, since (a) matches before the loop is
# reached. Measured on a SENTENCE-CASED bullet quoting long shas without a colon:
# 48 hex 0.0007 s, 64 hex 0.028 s, three 40-char shas did not return in 30 s,
# hanging `scan_open_actions` and therefore `/handoff` and `--validate` with no
# output. An all-caps bullet with the identical payload is unaffected, which is
# exactly why the regression test for this must be sentence-cased.
# The lookahead makes the atom non-splittable and drops the pathological case to
# ~0 s with zero behavioural change across the whole fixture.


def _is_fence(line: str) -> bool:
    s = line.lstrip()
    return s.startswith("```") or s.startswith("~~~")


def _heading_blocks(text: str) -> list[tuple[str | None, list[str]]]:
    """Split `text` into `(heading, body-lines)` blocks, in document order.

    🔴 THE ONE HEADING PARSER, and the reason it exists as its own function is
    that it now has TWO views over it: `extract_sections` ("which of these
    sections does the entry have, and what is in them") and `scan_headings`
    ("what headings does it have at all"). A second walker would be free to
    disagree with the first about what a heading IS — and `the writer half
    --validate` prints both answers in the SAME block, where the disagreement
    would render as "the section is absent" directly beside "the heading is
    right there". `claude/RULES.md` → "One rule, one place".

    A heading is a line beginning with `#` at COLUMN 0, outside a fence; the
    block key is that line `rstrip()`ed and otherwise verbatim. Every line that
    is not such a heading — fence lines included — belongs to the block it sits
    in. The FIRST block's heading is `None` whenever the text opens with
    anything other than a heading (front matter, prose), so a caller can tell
    "before the first heading" from any real section.

    🔴 FENCED BLOCKS ARE SKIPPED. A `#` line inside a code fence is not a
    heading, and treating it as one would END the section early — surfacing
    HALF an entry's nuance while looking exactly like a complete read. That is a
    silent under-report, the failure class this whole module is built against,
    so it is handled rather than left to "entries probably don't contain
    fences".

    A REPEATED heading yields a SEPARATE block each time. That is deliberate and
    it is the only reason duplicate detection is possible at all: the sections
    are merged by `extract_sections` (see there), so a walker that merged them
    here would destroy the evidence before anyone could report it.
    """
    blocks: list[tuple[str | None, list[str]]] = [(None, [])]
    in_fence = False
    for line in text.splitlines():
        if _is_fence(line):
            in_fence = not in_fence
            blocks[-1][1].append(line)
            continue
        if not in_fence and line.startswith("#"):
            blocks.append((line.rstrip(), []))
            continue
        blocks[-1][1].append(line)
    return blocks


def scan_headings(text: str) -> tuple[str, ...]:
    """Every ATX heading in `text`, in document order, REPEATS INCLUDED.

    The inventory `extract_sections` cannot give you: it answers only about the
    headings you asked for, so "the entry has no `## Pointers`" and "the writer
    called it `## pointers`" are the same answer there. This is what separates
    them — and what makes a duplicate visible, since `extract_sections` merges
    duplicates by design.

    Verbatim and unnormalized, for the same reason matching is exact: the caller
    reporting a near-miss must be able to print the heading the writer actually
    typed, not a folded form of it.
    """
    return tuple(h for h, _ in _heading_blocks(text) if h is not None)


def extract_sections(text: str, headings: Sequence[str]) -> dict[str, str]:
    """Return `{heading: body}` for each requested heading found in `text`.

    Bodies are VERBATIM — the store is markdown precisely so prose survives a
    read unmangled — with surrounding blank lines trimmed. A heading that is
    absent is simply not a key; the caller reports it by name rather than
    printing an empty block (an absent section and an empty one are different
    facts about a curated entry).

    A section runs from its heading to the next ATX heading of any level, or to
    end of file. Fenced blocks are skipped — see `_heading_blocks`, which is the
    parser this is a view over.

    Matching is on the EXACT heading string, not a normalized one: these are
    schema headings from `analyze-service/SKILL.md`, not user refs. Normalizing
    them would fold `## Pointers` and `## pointers!` together and quietly widen
    what the store is allowed to look like.

    A heading written TWICE has its blocks CONCATENATED under the one key, and
    whatever sat under an intervening heading is dropped. That is a silent merge
    and it is why the writer's own validate pass reports duplicates from
    `scan_headings` rather than from this mapping, which cannot show them.
    """
    wanted: dict[str, list[str]] = {h: [] for h in headings}
    # 🔴 PRESENCE IS TRACKED SEPARATELY FROM CONTENT. A heading that appears with
    # nothing under it must be reported as PRESENT-AND-EMPTY, not as absent —
    # "the section was never started" and "the section is there and unfilled"
    # are different facts about a curated entry, and only one of them is a
    # reason to go look somewhere else. Deriving presence from a non-empty body
    # collapses them, and it did: an empty section followed by another heading
    # read as absent while the same empty section at end-of-file read as
    # present, purely because of what came after it.
    seen: set[str] = set()
    for heading, body in _heading_blocks(text):
        if heading is None or heading not in wanted:
            continue
        seen.add(heading)
        wanted[heading].extend(body)
    return {h: "\n".join(wanted[h]).strip("\n") for h in headings if h in seen}


UNREACHABLE_MARKER = "unreachable-marker"
"""The reason token for the third openness shape. NOT a near-miss, and the two
counts are never added.

A near-miss is a marker MIS-SPELLED where the parser looks. This is a marker
spelled CORRECTLY where the parser never looks — so it raises neither badge, and
its remedy is different: a near-miss is fixed by editing the line, this one by
PROMOTING it to a top-level bullet of its own.
"""


@dataclass(frozen=True)
class UnreachableMarker:
    """One correctly-spelled openness marker sitting where NO reader looks.

    🔴 THE SHAPE THAT COST A REAL OPEN ACTION ITS BADGE. Measured in the field
    (`claudedocs/handoff-subsystem-store.md`): one bullet carried a
    second, correctly-spelled marker several lines into its body. `_bullet_openness`
    reads a bullet's OPENING line and the pattern is anchored at position 0, so
    that declaration reached no surface at all — it had only ever raised a badge
    BY ACCIDENT, through a broken `RESOLVED —` sitting above it in the same
    section. Fixing the broken line would therefore have SILENCED a still-open
    action, which is the failure this whole marker exists to prevent, arriving
    through the fix for a different one.
    """

    offset: int
    """1-based index of the line WITHIN THE BULLET. Always >= 2 — line 1 is what
    the parser already reads, so a marker there is reachable by definition."""

    line: str
    """The continuation line, VERBATIM — indentation and all. The report quotes
    it, and a stripped copy would send a writer looking for a line as typed."""

    openness: str
    """`open` | `resolved` — what this marker WOULD have declared had it been at
    the head of a bullet. Derived by running the real parser, never re-spelled."""

    resolved_by: str | None
    """The sha a `RESOLVED <sha>:` names, same normalisation as the real parser."""


def _as_opening_line(line: str) -> str:
    """Put a CONTINUATION line into OPENING-line position, verbatim otherwise.

    🔴 THIS IS THE WHOLE DERIVATION, AND IT IS DELIBERATELY THE ONLY NEW GRAMMAR.
    The marker vocabulary is NOT restated here: the continuation scanner hands
    each line to `_bullet_openness` — the same function `parse_journal_bullets`
    calls for line 1 — and this normalisation exists solely because both patterns
    require the `^[-*][ \\t]+` bullet prefix, which a wrapped prose line does not
    have. A ledger that restated `OPEN|RESOLVED` could not catch what it was
    written for: the point is that this stays in step with the real pattern even
    if that pattern changes. `test_subsystem_resolver.py` pins the two call sites
    against each other over the committed shape fixture.

    A line that ALREADY opens with a bullet marker (a nested list item — the
    field case) is passed through with only its indentation removed, so nothing
    is manufactured; anything else is given the minimal `- ` prefix.
    """
    stripped = line.strip()
    if _JOURNAL_BULLET.match(stripped):
        return stripped
    return f"- {stripped}"


_MARKER_ANYWHERE = re.compile(
    r"(?:OPEN|RESOLVED)(?![A-Za-z0-9_])"        # word-boundaried, either case…
    r"(?:[ \t]*(?:[0-9a-fA-F]{7,40}(?![0-9a-fA-F])|PR#\d+|#\d+"
    r"|\([^)]{1,30}\)|\[[^\]]{1,30}\]))*"       # …optional sha / PR / paren ref…
    r"[^A-Za-z0-9\n]{0,4}:",                    # …and a COLON, always.
    re.IGNORECASE,
)
"""A declaration ANYWHERE in a line — used ONLY to rank a DROPPED line's urgency.

🔴 THE COLON IS NOT OPTIONAL HERE, and that is the whole difference from
`_NEAR_MISS_MARKER`'s shouted branch. That branch may skip the terminator
because it is ANCHORED at a bullet head, where "prose does not shout"; unanchored
over a whole line the same rule fires on `OPEN SOURCE`. Requiring the colon keeps
`OPEN:` and `RESOLVED <sha>:` and rejects `OPEN SOURCE`, `RESOLVED_ADDR in the
trace…` (word boundary) and `resolved upstream in 1.2.3.` (no colon follows the
token or a ref form).

🔴 IT IS NOT A PARSE AND MUST NEVER GATE ONE. A marker mid-line declares nothing
to any reader in this module — that is deliberate and unchanged. This exists
because a line that is ALREADY LOST is a different question from a line being
parsed: the whole motivating incident is a bullet whose `OPEN:` sat mid-line, so
a signal anchored at position 0 is 0 on the only shape ever observed in the wild.
Ranking that as ordinary lost text, rather than as a lost DECLARATION, is what
the audit called inert.
"""


def line_mentions_marker(line: str) -> bool:
    """Does this line MENTION a declaration anywhere in it? Ranking only.

    Distinct from `line_openness`, which asks whether the line PARSES as one.
    Every reader in this module uses the latter; only the dropped-line scanner's
    urgency flag uses this, and it never gates whether a finding is reported.
    """
    return _MARKER_ANYWHERE.search(line) is not None


def line_openness(line: str) -> tuple[str | None, str | None]:
    """`(openness, resolved_by)` for ANY line, opening or continuation.

    🔴 THE ONE PLACE THAT ANSWERS "DOES THIS NON-OPENING LINE CARRY A MARKER".
    `JournalBullet.unreachable_markers` asks it of a bullet's lines 2..n, and
    `entry_shape.scan_dropped_lines` asks it of a line that reaches no
    bullet at all. Both had the same question and only one of them had the
    answer: the dropped-line scanner shipped a hand-spelled copy that fired on
    `* OPEN:` (a bullet char the corpus does not use), missed `- OPEN:` and
    every dated `- 2026-08-19: OPEN: …`, and returned True for the prose
    `resolved upstream in 1.2.3.` — inert on the field shape and loose on
    everything else. Consolidating is what made that audible.

    It restates no vocabulary: `_as_opening_line` puts the line in opening
    position and `_bullet_openness` — the same function `parse_journal_bullets`
    calls for line 1 — decides. A marker MID-line declares nothing, here as
    everywhere else in this module, because that pattern is anchored at
    position 0.
    """
    return _bullet_openness(_as_opening_line(line))


class CitationIDDomainError(ValueError):
    """A `str` `citation_id` has no defined answer for. See `_citation_hash_bytes`."""


def _citation_hash_bytes(text: str) -> bytes:
    """The bytes `citation_id` hashes — the REPLACE-DECODED form of `text`.

    🔴 THE DOMAIN IS DECLARED HERE BECAUSE THE TWO IMPLEMENTATIONS DID NOT AGREE ON
    IT. Go's `JournalBullet.CitationID` is TOTAL: a Go string is a byte sequence, so
    hashing it cannot fail. The Python side encoded with the STRICT handler, which is
    total for a client-side read (`read_text(errors="replace")` can only produce
    well-formed text) and PARTIAL on the pod, which decodes entry files
    `errors="surrogateescape"` — so a bullet off a malformed entry holds lone
    surrogates and a strict encode raises. MEASURED on `b"- a\\xe2\\x82\\n  tail\\n"`:
    `UnicodeEncodeError: 'utf-8' codec can't encode characters in position 3-4:
    surrogates not allowed`. The pod's own `append_bullet` carries a 🔴 comment one
    function above the call site saying "every re-encode below has to be the same
    handler or the round trip raises. It did." — this was a re-encode that was not.

    🔴 THE RULE: THE ID IS A FUNCTION OF THE ENTRY'S BYTES AS A `replace` DECODE
    SEES THEM, whichever handler the caller's own decode used. The coercion is
    `encode(surrogateescape)` — the exact inverse of the pod's decode, so it
    recovers the file's original bytes — followed by `decode(replace)`, which is
    what both the Python client and Go's `store.DecodeReplace` already do. All three
    paths therefore answer the SAME id for one file. On text with no surrogates the
    coercion is the identity, so nothing on the ordinary path moves.

    🔴 `surrogatepass` IS NOT THE FIX, AND IT LOOKS LIKE ONE. It is total and it
    round-trips, but it re-encodes each surrogate as its own three bytes where Go
    hashes the replacement character — so it would close the exception and OPEN a
    cross-language divergence. MEASURED on that bullet: `surrogatepass` gives the
    bytes `2d2061edb3a2edb2820a20207461696c` and the id `2e6d42b3`, while the
    coercion gives `2d2061efbfbd0a20207461696c` and `041430ff` — which is the id
    Go answers and the one `internal/store/testdata/citation_ids.json` records.

    ⚠ WHAT STILL REFUSES, STATED RATHER THAN LEFT TO BE MET. `surrogateescape`
    encodes only U+DC80..U+DCFF — the range its own decode produces. A lone
    surrogate OUTSIDE that range (say U+D800) cannot come from either decode path,
    and Go cannot hold one at all, so there is NO measured answer to agree with; it
    raises `CitationIDDomainError` rather than an incidental `UnicodeEncodeError`,
    which is a named refusal instead of a 500 with a codec message in it. If a path
    is ever found that produces one, that is a decision to make then, not a default
    to guess now.
    """
    try:
        raw = text.encode("utf-8", errors="surrogateescape")
    except UnicodeEncodeError as exc:
        raise CitationIDDomainError(
            "citation_id is defined over text produced by decoding bytes as UTF-8 "
            "with the 'replace' or 'surrogateescape' handler; this text carries a "
            f"surrogate outside U+DC80..U+DCFF, which neither produces ({exc})"
        ) from exc
    return raw.decode("utf-8", errors="replace").encode("utf-8")


@dataclass(frozen=True)
class JournalBullet:
    """One top-level bullet of a `## Nuance / work-history` section, VERBATIM.

    🔴 `lines` IS A TUPLE, NOT A STRING, because a real bullet is WRAPPED PROSE.
    Measured over the live corpus: 110 top-level bullets carry 250
    continuation lines between them — a median bullet is 3 lines and the longest
    is 19. Any model that assumed one line per bullet would silently truncate
    most of the corpus, and a truncated bullet is exactly the thing an agent
    would fail to recognize as a near-duplicate of the line it is about to write.
    """

    lines: tuple[str, ...]
    date: str | None
    """The ISO date the bullet is dated with, or None. ~44% of the real corpus
    carries no date; `None` is an ordinary reading, not a parse failure."""

    start_line: int
    """0-based index, WITHIN THE SECTION BODY this bullet was parsed from, of the
    line that OPENED it.

    🔴 IT EXISTS SO A RENDERER NEED NOT RE-DECIDE WHAT A BULLET IS. A caller that
    wants to annotate bullet openings while emitting a body VERBATIM would
    otherwise re-detect them, and the detection is not trivial: a `- ` line
    INSIDE A FENCE is sample text rather than a bullet, and text before the first
    bullet is dropped. A second copy of that rule would be wrong about fences on
    its first day. `internal/store`'s `JournalBullet.StartLine` is the same field
    for the same reason.

    ⚠ AN INDEX INTO THE BODY, NOT INTO THE FILE — the body is what
    `extract_sections` returned. Named because an off-by-a-section is silent: it
    would annotate the wrong lines rather than fail.

    ⚠ NOT A LENGTH, either: `len(lines)` can exceed the distance to the next
    bullet's `start_line`, because trailing blank lines are stripped from `lines`
    while remaining in the body. Walk `start_line`s; never add `len(lines)`.

    ⚠ AND IT IS AN INDEX INTO `body.splitlines()`, WHICH BREAKS ON **TEN**
    CHARACTERS — not on `"\\n"`. A consumer that re-splits the body with
    `text.split("\\n")` (or Go's `strings.Split(text, "\\n")`) gets a DIFFERENT
    list and this index then names the wrong line, silently. MEASURED on
    `"- a\\rb\\n- c\\n"`: `splitlines()` gives `['- a', 'b', '- c']` and the
    start_lines are `[0, 2]`; `split("\\n")` gives `['- a\\rb', '- c', '']`, so
    start_line 0 points MID-LINE and start_line 2 points at `''`. Use the same
    splitter the parser used — `pytext.SplitLines` on the Go side — and note that
    `internal/ui/render.go`'s `inlineCode` already reaches for the wrong one.

    🔴 NO DEFAULT, DELIBERATELY, AND THE ONE IT USED TO CARRY WAS JUSTIFIED BY
    CALL SITES THAT DO NOT EXIST. The removed docstring said the `= 0` existed
    "only so the many call sites that construct a bullet without a body position
    keep working". There is exactly ONE PRODUCTION construction site —
    `parse_journal_bullets` — and it supplies this field; the rest are TESTS, which
    construct the value they are asserting about and would rather fail loudly than
    inherit a 0.

    ⚠ AND THE COUNT IS STATED THAT WAY ON PURPOSE, BECAUSE A TOTAL WENT STALE INSIDE
    ITS OWN COMMIT. An earlier draft read "measured over every `.py` in the tree there
    is exactly ONE construction site": true at `3fb8dc2`, and already false at
    `0aa5fc48`, where the same commit's new `tests/test_citation_ids.py` added two
    more. The number that cannot rot is the one about PRODUCTION code, and the next
    test to construct a bullet does not invalidate it.

    A default would be worse than absent here: `0` is a LEGITIMATE value (the body's
    first line), so it cannot be read as "unset", and a bullet nobody positioned
    would annotate line 0 rather than fail. If you ever need "position unknown",
    add a separate field rather than overloading this one.
    """

    openness: str | None = None
    """`'open'` | `'resolved'` | None — the bullet's DECLARED openness marker.

    None is by far the common reading — **195 of 196** bullets in the live
    corpus — and means only that nothing was declared. 🔴 It does NOT mean
    "this bullet proposes no work": an unmarked bullet that proposes a remedy is
    exactly the `forgejo` failure, and `unmarked_action` is the (narrow,
    floor-only) net for that case.

    ⚠ The 196th is why this reads 195 and not 196. A past session had ALREADY
    written `- OPEN: rotate …` by hand, with no tooling asking it to and no
    convention documented — so this schema is a formalisation of a shape the
    corpus invented on its own, not one imposed on it. An earlier revision of
    this docstring claimed all 196 were unmarked; that was written before the
    corpus was grepped for markers, and the grep is what corrected it.
    """

    resolved_by: str | None = None
    """The sha a `RESOLVED <sha>:` bullet names as having closed it, or None.

    Carried so the claim is CHECKABLE — a reader can run `git cat-file -e` on it.
    A `RESOLVED` with no sha parses fine and leaves this None: the marker is still
    worth having, it just cannot be verified.

    🔴 BRANCHED ON, not merely stored. An audit found this field read by nothing
    outside the tests while its docstring claimed "the renderer says which it
    got" — a field in a DTO is not a guard (`claude/RULES.md`), and the design's
    headline rationale ("the sha makes the claim checkable") was unimplemented.
    `--validate` now reports every sha-less `RESOLVED` as an UNVERIFIABLE closure,
    which is the branch that makes the field load-bearing.
    """

    @property
    def citation_id(self) -> str:
        """The opaque per-bullet token the read surfaces print — 8 lowercase hex.

        🔴 WHAT IT IS FOR. "Was this recalled bullet actually used?" has no answer
        today: every available proxy is a SPELLED one, walkable by rewording, and
        the obvious one is saturated by a MANDATE rather than by use — a downstream
        consumer's resume flow requires its report to echo what it recalled, so "a
        printed ref reappears later" fires 451/454 = 99.3% and measures COMPLIANCE,
        not use. A token existing nowhere else in a corpus turns that question into
        a match.

        🔴 IT IS NOW PRINTED, AND THE CLOSING CONDITION THAT SAID SO IS MET. This
        docstring read "NOT PRINTED BY ANY SURFACE AT THIS COMMIT" and named the
        condition that would retire that sentence: a
        `internal/report/testdata/reader_fixtures.json` row carrying `[cb:`. There
        are such rows. BOTH text renderers append `citation_token` to every surfaced
        section line that OPENS a bullet — `render_text` here, which is also the
        oracle pod's, and `internal/report`'s `RecallReport.RenderText` — so this
        property is on the read path of every recall and its guards are no longer
        contract pins over a dead payload. ⚠ WHAT IS STILL NOT PRINTED: the
        `report_json` payload carries `sections` as RAW bodies and is deliberately
        unannotated (its own note says why), and no browser surface reads
        `start_line`.

        🔴 DERIVED FROM THE BULLET'S OWN BYTES, WHICH IS WHAT MAKES CROSS-LANGUAGE
        AGREEMENT REACHABLE RATHER THAN DISCIPLINED. `sha256` over `text` (the
        lines joined with "\\n"), first 8 hex characters. `text` is reused rather
        than re-joined here so there is one definition of "the bullet's bytes".

        🔴 TWO NORMALISATIONS, NAMED, BECAUSE AN EARLIER DRAFT OF THIS DOCSTRING
        SAID "**no** normalisation" AND THAT WAS FALSE. Both are in the id whether
        anyone wants them or not, and both are KEPT:

          1. THE LINE TERMINATOR. `parse_journal_bullets` splits on
             `str.splitlines()`, which strips the terminator, and `text` re-imposes
             `"\\n"`. So THIRTEEN byte-distinct spellings of one bullet — LF,
             CRLF, a lone CR, `\\v`, `\\f`, `\\x1c`, `\\x1d`, `\\x1e`, U+0085,
             U+2028, U+2029, no final terminator at all, and a trailing blank
             line — all yield ONE id. MEASURED: all thirteen give `3a98d5a7` for
             `- one`.
          2. THE DECODE FORM. The text is coerced to what a
             `errors="replace"` decode would have produced before hashing; see
             `_citation_hash_bytes` below for why, and for the one input that
             refuses.

        ⚠ SO DO NOT "RESTORE BYTE-EXACTNESS" BY HASHING THE RAW SLICE. The
        behaviour above is desirable — a CRLF entry and an LF entry naming the
        same bullet should name it with the same id — but it is a CONTRACT: every
        previously printed id stops resolving the day somebody hashes the
        unsplit bytes instead.

        ⚠ THE INVALIDATION SET IS WIDER THAN "EDITING A BULLET", AND EVERY MEMBER
        BELOW IS MEASURED. An id names a parsed bullet's bytes, so it moves on:

          * a TRAILING-WHITESPACE change — `- one` is `3a98d5a7` and `- one   ` is
            `7adcabe5`. Nothing strips it.
          * UNICODE FORM — NFC `- café` is `60bec6cc` and NFD is `83a886e0`. Two
            visually identical bullets, two ids, and no normalisation closes it
            (adding one would be a third normalisation and a contract change).
          * ADDING OR REMOVING an `OPEN:` / `RESOLVED <sha>:` marker, because the
            marker is part of the opening line's bytes.
          * THE WRITE TRAILER. ` [cairn: <actor>/<session>]` lives inside
            `lines[0]`, so an id is partly a function of the SESSION that wrote
            the bullet.
          * FIXING THE RECORDED `dropped-lines` DEFECT, which moves a NEIGHBOUR's
            id: lines currently absorbed into the preceding bullet would leave its
            `lines`, so a bullet nobody touched loses its id. MEASURED, with the
            bodies named, because the one number this row was first given arrived
            without one: over `"- first\\nstray line\\n- second\\n"` the first bullet
            is `608a5be6`, and over `"- first\\n- second\\n"` — the same bullet after
            the absorbed line leaves — it is `6b4d8edb`.

            ⚠ AND `ad08c42d`, WHICH `0aa5fc48`'s COMMIT MESSAGE PAIRED WITH
            `6b4d8edb` HERE, IS RETRACTED AS UNREPRODUCIBLE. It appears in NO tracked
            file — measured at `0aa5fc48`, 0 of 491, with `6b4d8edb` as the positive
            control for the same grep (found, in
            `internal/store/testdata/citation_ids.json`) — and two differently-shaped
            sweeps over 24,088 absorbing constructions produced it zero times:
            `"- first"` plus 0..3 stray lines drawn from a 25-item word space, and
            twelve opening-line spellings plus 0..2 of the same. Both sweeps'
            positive controls reproduced the two ids above. No body is offered for
            it, deliberately: inventing one that happens to hash to it would be a
            worse record than the retraction.

        🔴 AND THE ID IS **NOT SCOPED**. Two byte-identical bullets in different
        entries, or in different scopes, get the SAME id, deterministically —
        `write.AppendBullet` dedupes within ONE file only, so the corpus does not
        forbid it. The framing "a token existing nowhere else in a corpus"
        therefore holds for the PROSE a bullet carries and not for a duplicate of
        it, and the arithmetic below models RANDOM collisions only; it says nothing
        about duplicated text, which collides with probability 1.

        ⚠ 8 HEX, NOT 4, AND THE REASON IS ARITHMETIC. At 16 bits a 3,129-bullet
        corpus collides with probability ≈1 (birthday: ~50% by ~300 bullets); 32
        bits puts it near 0.1% corpus-wide. Do not shorten it to fit a column.

        ⚠ AND THIS FILE NOW CARRIES THREE CORPUS SIZES THAT ARE **THREE DIFFERENT
        POPULATIONS**, NOT A DISAGREEMENT. Said here because the 3,129 is the newest
        and a reader meeting it after the other two would read it as a correction:

          * **110** — TOP-LEVEL bullets across 26 entries, with 250 continuation
            lines between them. That is the population `_JOURNAL_BULLET`'s comment
            counts, and its subject is INDENTATION.
          * **196** — bullets in the openness survey (`openness`'s docstring, "195
            of 196"). A later measurement of a grown store, and its subject is
            MARKERS.
          * **3,129** — the population the collision arithmetic is sized for: the
            store as measured when the citation id was designed.

        🔴 NONE OF THE THREE IS A RE-MEASUREMENT OF ANOTHER, AND NOTHING HERE CAN
        RECONCILE THEM — the live store is not in this repository, so no test can
        re-derive any of these numbers. What is checkable is the only thing the
        arithmetic needs: 8 hex is sufficient at ALL THREE sizes, and 4 is
        insufficient at all three.
        """
        return hashlib.sha256(_citation_hash_bytes(self.text)).hexdigest()[:8]

    @property
    def first_line(self) -> str:
        return self.lines[0] if self.lines else ""

    @property
    def text(self) -> str:
        return "\n".join(self.lines)

    @property
    def is_open(self) -> bool:
        return self.openness == OPENNESS_OPEN

    @property
    def unmarked_action(self) -> bool:
        """Does this bullet LOOK like an unmarked open action? A FLOOR, never a list.

        True only for the two phrasings measured to occur with no false positives
        over the live corpus (see `_UNMARKED_ACTION`). An unmarked bullet phrased
        any other way returns False, and that False is not evidence of anything —
        which is why every renderer of this prints it as "at least N", never as a
        count of what exists.

        Suppressed once the bullet declares openness: a bullet that already says
        `OPEN:` is not *unmarked*, and one that says `RESOLVED:` has been closed —
        re-flagging either would train the reader to ignore the advisory.
        """
        if self.openness is not None:
            return False
        return bool(_UNMARKED_ACTION.search(self.text))

    @property
    def openness_population(self) -> str:
        """WHICH of the six populations this bullet belongs to. Exactly one.

        🔴 THE SINGLE SOURCE OF THE PRECEDENCE ORDER, and the reason it exists.
        A delta re-audit found one bullet counted TWICE in the writer-facing
        block — `- Open items: the retry budget is not yet addressed.` is both a
        near-miss and an unmarked action, so it rendered under both headings with
        its own line quoted under each, while `--validate` classified it once.
        Two surfaces disagreed about the same input because each decided
        membership for itself (`claude/RULES.md` → "One rule, one place: a
        predicate duplicated across call sites regenerates the same bug at every
        site"). Every consumer now branches on THIS.

        Precedence, most-certain first — an earlier case wins outright:

          `open`          the writer declared `OPEN:`. Exact.
          `unverifiable`  a `RESOLVED:` naming no sha; closed but unprovable.
          `resolved`      a `RESOLVED <sha>:`. Nothing to report.
          `near-miss`     no marker parsed, but the line looks like an attempt.
                          Beats `unmarked` because "your write did not land" is
                          actionable and specific, where "this reads like an open
                          action" is a guess about the same line.
          `unmarked`      no marker, and the prose matches the narrow floor.
          `none`          everything else — the overwhelming majority.

        ⚠ ONLY `near-miss` > `unmarked` IS OBSERVABLE, and the docstring says so
        rather than implying all five levels are load-bearing. `near_miss_marker`
        and `unmarked_action` both self-suppress when `openness` is set, so
        reordering `open`/`resolved`/`unverifiable` against them are EQUIVALENT
        mutants that no test can kill (measured — they survive the battery). The
        order is still written most-certain-first because that is what makes it
        readable; just do not count those levels as guards.
        """
        if self.openness == OPENNESS_OPEN:
            return "open"
        if self.openness == OPENNESS_RESOLVED:
            return "resolved" if self.resolved_by else "unverifiable"
        if self.near_miss_marker:
            return "near-miss"
        if self.unmarked_action:
            return "unmarked"
        return "none"

    @property
    def near_miss_marker(self) -> bool:
        """Did this bullet TRY to carry a marker and miss the grammar?

        The fail-loud half of a deliberately strict `_JOURNAL_OPENNESS`. True only
        when no marker parsed AND the first line opens with something that reads
        like an attempt — so a writer whose `RESOLVED <sha> (<repo>):` silently
        did nothing is told, instead of seeing the badge vanish and reading that
        as success.
        """
        if self.openness is not None:
            return False
        return bool(_NEAR_MISS_MARKER.match(self.first_line))

    @property
    def unreachable_markers(self) -> tuple[UnreachableMarker, ...]:
        """Markers on lines 2..n that WOULD have parsed at the head of a bullet.

        🔴 A THIRD SHAPE, AND IT IS NOT A POPULATION. `openness_population` is
        untouched by this: that property answers "what did this bullet DECLARE",
        and a bullet whose only marker is out of reach declared nothing — which
        is precisely the finding. Folding this in would have changed the answer
        to a different question and silently moved existing counts.

        🔴 NOT SUPPRESSED WHEN THE BULLET ALREADY DECLARES ONE. The field case
        was exactly a bullet carrying two markers, and the head one was broken;
        a bullet with a good head marker AND a second one further down is two
        claims stored as one, which is worth saying either way.

        Blank lines contribute nothing, and FENCED regions are skipped for the
        same reason `parse_journal_bullets` skips them: a `- OPEN:` inside a code
        fence is sample text, and reporting it would send a writer to promote a
        line that is quoting something.
        """
        out: list[UnreachableMarker] = []
        in_fence = False
        for offset, line in enumerate(self.lines[1:], start=2):
            if _is_fence(line):
                in_fence = not in_fence
                continue
            if in_fence or not line.strip():
                continue
            # 🔴 THE REAL PARSER, not a second copy of its vocabulary. See
            # `line_openness`, which `scan_dropped_lines` shares.
            openness, sha = line_openness(line)
            if openness is None:
                continue
            out.append(
                UnreachableMarker(
                    offset=offset, line=line, openness=openness, resolved_by=sha
                )
            )
        return tuple(out)


def parse_journal_bullets(body: str) -> tuple[JournalBullet, ...]:
    """Group a `## Nuance / work-history` body into top-level bullets.

    Order is preserved exactly as stored — this function makes NO claim about
    which bullet is newest. The store's convention is newest-first, but that is a
    convention a writer can break, so recency is derived from the DATES (see
    `entry_shape.EntryJournal.newest_date`) rather than from position.

    Rules, each measured against the corpus rather than assumed:

      * A bullet starts at column 0 (`_JOURNAL_BULLET`). Every other non-blank
        line attaches to the bullet above it, indented or not.
      * Text BEFORE the first bullet is dropped from the bullet list. The caller
        must not read an empty tuple as "the section is empty" — a non-empty body
        that yields no bullets is its own state, and the writer half reports it
        as one rather than showing a blank.
      * 🔴 FENCED BLOCKS ARE SKIPPED, for the same reason `extract_sections`
        skips them: a `- ` line inside a fence is sample text, and promoting it
        to a bullet invents history the entry does not have. No fence appears in
        the corpus today (measured: 0 fence lines across all 26 nuance sections)
        — this is here because the sibling parser one screen up already had to
        learn it, and a fence is one pasted snippet away.
      * Trailing blank lines are stripped from each bullet so a blank separator
        cannot inflate a bullet's line count.
    """
    bullets: list[list[str]] = []
    # 🔴 PARALLEL TO `bullets`, APPENDED IN LOCKSTEP WITH IT. A dict keyed on the
    # opening LINE would be wrong wherever two bullets open identically, which the
    # corpus does not forbid; the index is recorded where the group is created, so
    # the two lists cannot disagree about which bullet is which.
    starts: list[int] = []
    in_fence = False
    for i, line in enumerate(body.splitlines()):
        if _is_fence(line):
            in_fence = not in_fence
            if bullets:
                bullets[-1].append(line)
            continue
        if not in_fence and _JOURNAL_BULLET.match(line):
            bullets.append([line])
            starts.append(i)
            continue
        if bullets:
            bullets[-1].append(line)
    out: list[JournalBullet] = []
    for gi, group in enumerate(bullets):
        while group and not group[-1].strip():
            group.pop()
        openness, resolved_by = _bullet_openness(group[0])
        out.append(
            JournalBullet(
                lines=tuple(group),
                date=_bullet_date(group[0]),
                openness=openness,
                resolved_by=resolved_by,
                start_line=starts[gi],
            )
        )
    return tuple(out)


def citation_token(citation_id: str) -> str:
    """The RENDERED spelling of one citation id. The LEADING SPACE is part of it.

    🔴 ONE SPELLING OF THE RENDERED TOKEN, AND THE REASON IS A SEAM RATHER THAN
    TIDINESS. `server.py`'s `_BULLET_TRAILER_PIECE_RE` is what STRIPS this trailer
    before a bullet's content hash is taken, and the echo it strips is measured
    rather than feared — a downstream consumer's resume flow requires its report
    to echo what it recalled, firing on 451 of 454 runs (99.3%). A format that
    drifted from that pattern would leave the token in the hash, and the damage is
    the silent kind: the re-POST stops matching the bullet on disk, appends a
    near-duplicate, and that duplicate carries a `[cb:…]` in its PROSE naming the
    bullet it was copied FROM. A format string is not a regexp, so nothing can
    derive one from the other; what binds them is a test that renders through this
    function and strips through that one.

    ⚠ 14 BYTES PER ANNOTATED LINE, and that is the whole cost of the feature: 1
    space + 4 for `[cb:` + 8 hex + 1 for `]`. `internal/store.CitationToken` is the
    Go twin, two spellings for the reason `citation_id` has two — `lib/` cannot
    import `internal/`.
    """
    return f" [cb:{citation_id}]"


def citation_ids_by_start_line(body: str) -> dict[int, str]:
    """Map a SECTION BODY's bullet-opening line index to that bullet's `citation_id`.

    🔴 IT PARSES FOR IDS ONLY — THE CALLER STILL EMITS THE BODY VERBATIM, AND THAT
    SEPARATION IS THE WHOLE DESIGN RATHER THAN A STYLE CHOICE. The obvious
    implementation of an annotated read is to walk `parse_journal_bullets` and print
    each bullet's `lines`; that is RULED OUT, because this parser has a recorded
    `dropped-lines` defect — text BEFORE the first bullet never enters the bullet
    list, and a bullet whose opening line was lost or indented is ABSORBED into the
    bullet above it. Today those are an advisory validator finding and nothing is
    lost. Under a re-emission they become SILENT DELETION on the read surface:
    measured history is 7 versions across two entries carrying dropped lines for 2–8
    days with that validator green throughout. So this function hands back
    POSITIONS, never content, and a renderer that uses it cannot lose a byte it was
    given.

    🔴 A LINE WITH NO ENTRY HERE IS NOT EVIDENCE ITS BULLET WAS NOT PRINTED. Every
    body line is printed either way; the map only says which ones carry an id. Four
    shapes are absent from it BY DESIGN — prose, a blank, an INDENTED dash (a
    continuation, see `_JOURNAL_BULLET`), and a dash INSIDE A FENCE (sample text) —
    and a fifth, the `dropped-lines` population above, is absent by DEFECT. Measured
    over a real 344-entry store, by surfaced section: `## Pointers` 1,589 of 2,635
    body lines carry an id and 15 are pre-first-bullet prose; `## Nuance /
    work-history` 3,375 of 14,094; `## What it is` 7 of 1,274, because that section
    is prose and 1,258 of its lines precede any bullet; `## Requirements` 7 of 7.
    ⚠ THOSE ARE ONE READING OF A LIVE STORE AND WILL NOT RE-DERIVE — the store is not
    in this repository, and the nuance pair moved 3,367/14,086 -> 3,375/14,094
    between two measurements an hour apart while this docstring was being written.
    What is STABLE, and what the sentence actually rests on, is the last clause: ZERO
    column-0 dash lines in any of the four sections parsed to no bullet, at both
    readings. So missing ids are a DEGRADATION IN COVERAGE, never in content, and
    anyone reading a gap as "that bullet was withheld" has the direction backwards.

    ⚠ THE KEYS ARE INDICES INTO `body.splitlines()`, WHICH BREAKS ON **TEN**
    CHARACTERS — not on `"\\n"`. A caller that re-splits with `body.split("\\n")`
    annotates the wrong line, silently; the worked measurement is on
    `JournalBullet.start_line`. Split with `splitlines()` or do not use this map.

    ⚠ AND IT IS PER SECTION BODY, NOT PER FILE — `start_line` is an index into
    whatever `extract_sections` returned, so handing this a whole entry file
    annotates lines that merely share an offset with a bullet.

    ⚠ `citation_id` CAN RAISE `CitationIDDomainError`, AND NOTHING HERE CATCHES IT,
    DELIBERATELY. The domain is a `replace`-decode form, and NEITHER read path can
    leave it: the client reads entries `errors="replace"` (`read_entry`) and the pod
    reads them `errors="surrogateescape"`, which `_citation_hash_bytes` inverts
    exactly. The refusal needs a lone surrogate OUTSIDE U+DC80..U+DCFF, which no
    decode produces and which Go cannot represent at all. Swallowing it would trade
    a loud, unreachable failure for a SILENT id gap that is indistinguishable from
    the `dropped-lines` degradation above — and it would diverge from the Go
    renderer, which has no such branch.

    Collisions are impossible: `parse_journal_bullets` records one start index per
    group, in lockstep with the groups, so no two bullets claim one line.
    """
    return {b.start_line: b.citation_id for b in parse_journal_bullets(body)}


@dataclass(frozen=True)
class Requirement:
    """One bullet of a `## Requirements` section: a journal bullet plus WHO said it.

    🔴 IT WRAPS `JournalBullet` RATHER THAN RE-DECLARING ITS FIELDS, because the
    openness state machine is the thing being reused and a parallel dataclass is
    how two readers come to disagree about what `RESOLVED <sha>:` means.
    `openness_population`, `first_line`, `text` and `date` are all reached
    through `bullet` and none of them is re-spelled here.
    """

    bullet: JournalBullet
    provenance: str | None
    """`'operator'` | `'inferred'` | None, read from a prefix AFTER the marker.

    🔴 None IS A DECIDED ANSWER, NOT AN ACCIDENT. The whole feature is "who
    stated this", so a requirement nobody attributed must stay distinguishable
    from one attributed to an agent. Folding None into `'inferred'` would assert
    an attribution the file does not make."""

    @property
    def is_open(self) -> bool:
        """DECLARED open, exactly — and deliberately NOT `not is_met`.

        A bullet carrying no marker is neither open nor met; it is unstated, and
        both predicates return False for it. A `not is_met` spelling would
        promote every unmarked bullet to an open requirement and inflate the
        badge with work nobody declared."""
        return self.bullet.openness_population == "open"

    @property
    def is_met(self) -> bool:
        """Closed, in the journal's own two-state vocabulary.

        🔴 READS `openness_population`, NOT `openness`, so it cannot disagree
        with the badge beside it. `unverifiable` — a `RESOLVED:` naming no sha —
        counts as MET: the writer closed it and only the CHECK is missing, which
        is exactly what that population exists to report separately. Folding it
        into not-met would reopen an action somebody finished."""
        return self.bullet.openness_population in ("resolved", "unverifiable")


def parse_requirements(body: str) -> tuple[Requirement, ...]:
    """Group a REQUIREMENTS-section body into requirement bullets.

    🔴 IT TAKES THE SECTION BODY, NOT THE ENTRY, AND THAT IS THE SECTION
    BOUNDARY GUARD. The claim a requirement makes is "the operator said this
    subsystem should do X"; a bullet saying the same words under
    `## Nuance / work-history` is a note about history. The two are told apart
    by WHICH SECTION THEY SIT IN and by nothing else — no keyword, no shape — so
    the only way this can be wrong is a caller handing it the wrong body.
    Callers get it from `extract_sections(...)[REQUIREMENTS_HEADING]`, a lookup
    that cannot silently widen.

    Bullet grouping is `parse_journal_bullets` verbatim: same column-0 rule,
    same fence skipping, same continuation handling. A second grouper here would
    be the duplicated predicate that diverges the day one side learns a new
    fence spelling — invisibly, because both look right on the simple case.
    """
    return tuple(
        Requirement(bullet=b, provenance=_bullet_provenance(b.first_line))
        for b in parse_journal_bullets(body)
    )


def _bullet_provenance(first_line: str) -> str | None:
    """`(operator)` / `(inferred)` from the run immediately AFTER the marker.

    🔴 AFTER THE MARKER, INSIDE THE TEXT — NEVER AS A CAPTURE GROUP INSIDE
    `_JOURNAL_OPENNESS`. That pattern is anchored, terminator-exact and among
    the most heavily mutation-tested expressions here; widening it to carry an
    optional parenthetical would change what counts as a marker for EVERY
    journal bullet in every entry, to add a field only this section reads.

        - OPEN: (operator) the share page should name the project
               ^ the match ends here

    ⚠ SO A LINE WITH NO PARSED MARKER HAS NO PROVENANCE, BY CONSTRUCTION — the
    intended narrowing, and the safe direction: a near-miss bullet's whole
    finding is that its marker did not parse, and attaching an author to it
    would dress a failed write up as a recorded one.

    🔴 THE MATCH IS THE WHOLE PARENTHESISED WORD. `(operators)` and
    `(operator-ish)` are None, and so is `(Operator)` — a guard that accepted a
    prefix, or folded case, would be walkable by writing anything close enough,
    and provenance is precisely the claim that must not be manufacturable by
    accident.
    """
    m = _JOURNAL_OPENNESS.match(first_line)
    if not m:
        return None
    rest = first_line[m.end() :].lstrip()
    for candidate in (PROVENANCE_OPERATOR, PROVENANCE_INFERRED):
        if rest.startswith(f"({candidate})"):
            return candidate
    return None


def _bullet_openness(first_line: str) -> tuple[str | None, str | None]:
    """`(openness, resolved_by)` for one bullet's first line.

    Takes the first line only, but 🔴 THAT IS NOT WHAT ENFORCES IT — the guard is
    `re.match`, which anchors at position 0, over a pattern with no `re.MULTILINE`.
    A marker on a continuation line is therefore unreachable whether this is passed
    one line or the whole joined bullet.

    Stated because a mutation battery proved it: replacing `group[0]` with the
    joined bullet text is an EQUIVALENT mutant and survives the suite, and an
    earlier version of this docstring claimed the argument was the protection.
    Two mechanisms reaching one outcome cannot be told apart by any test
    (`claude/RULES.md`), so the honest form is to name the one that actually holds
    and keep the narrower argument as defence-in-depth — if the pattern ever gains
    `re.MULTILINE`, passing one line is what stops markers being invented from
    wrapped prose, and that is a real hazard rather than a hypothetical one.

    The sha is normalised to lower case so `RESOLVED B83BFB58:` and
    `RESOLVED b83bfb58:` are one claim, not two.
    """
    m = _JOURNAL_OPENNESS.match(first_line)
    if not m:
        return None, None
    marker = OPENNESS_OPEN if m.group(1) == "OPEN" else OPENNESS_RESOLVED
    sha = m.group(2)
    return marker, sha.lower() if sha else None


def _bullet_date(first_line: str) -> str | None:
    """The bullet's ISO date, or None — VALIDATED, not just shaped.

    `2026-13-45` matches the shape and is not a date; returning it would put a
    nonexistent day into a recency claim and into any arithmetic done on it.
    `fromisoformat` is the check, so what comes back is always a real date.
    """
    m = _JOURNAL_DATE.match(first_line)
    if not m:
        return None
    try:
        _date.fromisoformat(m.group(1))
    except ValueError:
        return None
    return m.group(1)


# --- The thin disk loader ------------------------------------------------------
#
# Deliberately separate from everything above: the pure functions never touch the
# filesystem, so the tests exercise them with in-memory fixtures and NEVER read
# the real store (which is client-confidential, curated, and rewritten hourly by
# the autocommit timer while other sessions write to it).


def parse_front_matter(text: str) -> dict[str, object]:
    """Parse the leading `---` block of an index file into a str/list-of-str dict.

    Hand-rolled rather than PyYAML, for one correctness reason and one
    deployment reason:

      * YAML's implicit typing is actively wrong for this schema. `service: no`
        parses to the boolean False, `service: 1.0` to a float, and an alias
        `on` / `yes` / `off` to a bool — every one of which then fails to
        normalize as a string. The values here are ALWAYS strings.
      * P1 runs this from the collector timer's environment; a lib with no
        third-party import is one fewer thing that can be absent there.

    Handles the three shapes: `key: value`, an inline flow list `key: [a, b, c]`,
    and a block list (`key:` on its own line followed by `- item` lines). Quotes
    are stripped. Unknown keys are preserved so a caller can see them;
    `SubsystemEntry.from_mapping` ignores what it does not need.

    🔴 THE BLOCK FORM IS PARSED BECAUSE NOT PARSING IT CORRUPTED THE MAPPING —
    it was never merely "ignored". Measured on the real parser before this
    change, `tasks:` followed by `  - clickup:868abc123` and
    `  - github:example-org/alpha-toolkit#428` produced::

        {'service': 'thing', 'tasks': '',
         '- clickup': '868abc123',
         '- github': 'example-org/alpha-toolkit#428'}

    — the key silently empty and EVERY item promoted to a phantom front-matter
    key by its own internal colon. A caller reading that mapping sees keys nobody
    wrote and loses the data that was written. (A ref-shaped item always has a
    colon, so the promotion is the rule here, not the exception: an item WITHOUT
    one is instead dropped silently. Both halves of that are data loss.)
    Rejecting the shape would have been the other option and is strictly worse:
    it leaves the two-line hazard in the parser for every future key.

    Widening this was verified SAFE by measurement rather than assumed: across
    all 120 front-matter blocks in the live store, the count of lines beginning
    `- ` is **zero**, so no existing entry changes meaning. That measurement is
    what makes this additive; re-take it before widening further.

    A block item is still ONE line. A wrapped item is not rescued here, and the
    inline flow list remains the writer's form (`format_task_refs`) for exactly
    that reason.

    🔴 A BARE `key:` WITH NO ITEMS UNDER IT STILL READS AS `""`, NOT `[]`.
    A block list is recognised by LOOKAHEAD — the key opens one only when a
    following item actually exists — so no existing key changes type. That
    matters concretely: `sensitivity:` is read by callers that call `.strip()` on
    it, and handing them a list where they have always had a string would be an
    `AttributeError` in a reader, raised from a file the operator would have to
    guess at. The narrower rule costs one peek and cannot do that.
    """
    m = _FRONT_MATTER.match(text)
    if not m:
        return {}
    out: dict[str, object] = {}
    lines = m.group(1).splitlines()

    def _is_skippable(raw_line: str) -> bool:
        return not raw_line.strip() or raw_line.lstrip().startswith("#")

    def _is_block_item(raw_line: str) -> bool:
        """🔴 ANY `-`-led line is an ITEM, INCLUDING AN EMPTY ONE.

        The narrower `startswith("- ")` is a defect, not a style choice, and it
        resurrected the exact corruption this parser was widened to fix. A bare
        `-`, or `- ` with only trailing whitespace, does not satisfy it, so the
        block scan TERMINATED there and every ref below was promoted to a
        phantom key again. Worse than the original bug: `tasks` then reads as
        falsy, `from_mapping` treats the key as absent, and the entry LOADS
        CLEAN reporting no tasks — the data is gone and every surface says the
        file is fine. An empty item in the MIDDLE truncates instead, which is
        the same failure wearing a success costume.

        So membership is decided by the leading `-` alone; whether the item
        carries a payload is a separate question, answered in `_block_items_from`.
        """
        s = raw_line.strip()
        return s == "-" or s.startswith("- ")

    def _block_items_from(start: int) -> list[str]:
        """The `- item` run beginning at `start`.

        Stops at the first line that is neither an item, a comment, nor blank.
        An EMPTY item contributes nothing but does NOT stop the scan — see
        `_is_block_item`.

        🔴 A BLANK LINE DOES NOT END THE BLOCK, AND AN EARLIER VERSION OF THIS
        FUNCTION SAID THE OPPOSITE. Making a blank line terminate the scan was a
        REGRESSION, measured against PyYAML: a list separated from its key by a
        blank line is ordinary, valid YAML, and breaking there dropped the whole
        list back out of the scan so its items were promoted to phantom keys —
        reproducing byte for byte the corruption quoted above, with the entry
        then LOADING CLEAN because `tasks` was falsy. It was introduced to bound
        how far a bare `key:` reaches for items; that bound was a fix for a
        hazard that does not exist, and it cost a correct parse.

        🔴 THE `key:` / list BINDING IS NOT OURS TO NARROW. In YAML a `- ` list
        belongs to the nearest preceding key, however many blank and comment
        lines intervene — `yaml.safe_load` on `sensitivity:` + blank + comment +
        `- x` returns `{'sensitivity': ['x']}`. Diverging from that to keep a key
        string-typed would make this parser disagree with every other reader of
        the same bytes. The type question belongs to the CONSUMERS, and they
        already answer it: every reader of `sensitivity` / `created_by` /
        `namespace` is `isinstance(..., str)`-guarded and degrades to its
        fail-safe rather than raising.

        What actually closes the phantom-key class is the outer loop refusing to
        treat a `-`-led line as a key at all — see the `_is_block_item` skip
        there. That is the right place for it: it holds no matter where a block
        scan starts or stops, including for an ORPHAN list with no owning key,
        which is not valid YAML in the first place (`yaml.safe_load` raises).
        """
        items: list[str] = []
        for j in range(start, len(lines)):
            candidate = lines[j]
            if not candidate.strip() or candidate.lstrip().startswith("#"):
                continue
            if not _is_block_item(candidate):
                break
            item = candidate.strip()[1:].strip().strip("'\"")
            if item:
                items.append(item)
        return items

    def _block_ends_at(start: int) -> int:
        """The index of the last line belonging to the block opened before
        `start` — items, interleaved comments and blank lines alike.

        🔴 SWALLOWS EMPTY ITEMS TOO, and must agree with `_block_items_from`
        about MEMBERSHIP even where it disagrees about CONTENT. The two share
        `_is_block_item` for exactly that reason — two spellings of one
        membership rule is what produced the bug this parser was widened to fix.

        ⚠ Trailing blanks and comments are NOT swallowed: `last` only advances on
        a real member, so a block followed by a blank line and then a key leaves
        that key readable. Consuming to the last blank would be harmless today
        and is not done, because "swallow exactly the members" is the property
        that stays true if the surrounding loop changes.

        ⚠ TWO EQUIVALENT MUTANTS LIVE IN THIS NEIGHBOURHOOD, both measured and
        stated rather than left for the next sweep to rediscover. The second is
        narrowing the OUTER loop's skip from `_is_block_item(line)` to
        `line.strip().startswith("- ")`: a bare `-` has no colon, so
        `partition(":")` skips it either way. (An earlier note here said "one
        equivalent mutant remains", which was one short — the same
        count-nobody-re-derives problem this file keeps correcting elsewhere.)

        The first: removing the blank-line skip here is an EQUIVALENT mutant. It would stop the scan at a
        blank inside a list, leaving the items after it unswallowed — but the
        outer loop now SKIPS every `-`-led line, so those items produce no
        phantom key and nothing downstream can tell the difference. That is
        defence-in-depth working as intended, not dead code: this function is
        what keeps the members out of the key space if that outer skip is ever
        narrowed, and the outer skip is what keeps them out if this is. Neither
        is testable while the other holds, so BOTH are documented instead of one
        being deleted as unreachable.
        """
        last = start - 1
        for j in range(start, len(lines)):
            if not lines[j].strip() or lines[j].lstrip().startswith("#"):
                continue
            if _is_block_item(lines[j]):
                last = j
                continue
            break
        return last

    consumed_through = -1
    for i, line in enumerate(lines):
        if i <= consumed_through or _is_skippable(line):
            continue
        # 🔴 A `-`-LED LINE IS NEVER A KEY. This single skip is what closes the
        # phantom-key class, and it closes it independently of where any block
        # scan begins or ends — which is why it lives here rather than being
        # implied by a well-behaved scan. Without it, ANY list item the scan does
        # not claim gets `partition(":")`-ed and its own internal colon makes it
        # a front-matter key nobody wrote (`- clickup: 868abc123`), while the
        # real key reads empty.
        #
        # ⚠ TWO SHAPES REACH HERE, and an earlier version of this comment named
        # only the rarer one. (a) A genuine ORPHAN list — no owning key at all —
        # which is not valid YAML (`yaml.safe_load` raises), so there is nothing
        # to preserve and skipping is the whole correct behaviour. (b) FAR more
        # commonly, a `- ` line under a key that already has a SCALAR value,
        # where an owning key does exist: YAML folds that into a multi-line plain
        # scalar, and this parser drops it. That is a pre-existing limit of
        # reading YAML line by line, not something this skip introduces — the
        # alternative here is not "fold it correctly", it is "invent a key" —
        # but the justification has to cover the case it actually meets most
        # often, or it reads as a proof it has not done.
        if _is_block_item(line):
            continue
        key, sep, value = line.partition(":")
        if not sep:
            continue
        key = key.strip()
        value = value.strip()
        if not key:
            continue
        if value.startswith("[") and value.endswith("]"):
            items = [v.strip().strip("'\"") for v in value[1:-1].split(",")]
            out[key] = [v for v in items if v]
        elif not value:
            block = _block_items_from(i + 1)
            if block:
                out[key] = block
            else:
                # No items followed, so this is an empty scalar, which is what a
                # bare `key:` has always meant here. ⚠ NOT the same claim as "a
                # bare key can never become a list": if a list DOES follow, it
                # binds, exactly as YAML says.
                out[key] = value.strip("'\"")
            consumed_through = _block_ends_at(i + 1)
        else:
            out[key] = value.strip("'\"")
    return out


def entry_mapping(text: str, *, filename: str, scope: str) -> dict[str, object]:
    """ONE entry file's bytes -> the mapping `from_mapping` would be handed.

    🔴 EXTRACTED FROM `load_index` SO A VALIDATOR CANNOT BUILD A DIFFERENT ONE.
    The writer's own validate pass has to answer "would the loader accept this
    file?", and the only honest way to answer it is to construct exactly what the
    loader constructs. Re-spelling these four lines at the validator would be the
    duplicated predicate `claude/RULES.md` names: the day one side learns a new
    identity field, the validator starts blessing entries the reader rejects —
    the precise drift a write-time check exists to prevent.

    The directory name is the authority on scope: it is where the file actually
    lives. A `scope:`/`repo:` field that disagrees is stale front matter, not a
    relocation, so `scope` is set unconditionally and `repo` is dropped.
    """
    fm = dict(parse_front_matter(text))
    fm["filename"] = filename
    fm["scope"] = scope
    # ⚠ REDUNDANT-BUT-KEPT, labelled: `from_mapping` reads `scope` in preference
    # to `repo`, and `scope` was just set unconditionally on the line above, so
    # the pop cannot change any outcome. It stays to keep the mapping honest —
    # leaving a contradicted `repo:` in a dict that is also the malformed-entry
    # error's source label would put a stale value in front of whoever reads that
    # error.
    fm.pop("repo", None)
    return fm


# =============================================================================
# 🔴 THE TOTAL CLASSIFIER — why this exists instead of a fourth `if` arm.
#
# ⚠ IT LIVED IN `subsystem-store-api/server.py` UNTIL `load_index` NEEDED IT.
# It moved here rather than being copied: "what IS this path" open-coded at N
# sites is wrong at N-1 of them, and disagreeing about a FIFO named `*.md` is
# exactly what costs a request thread. What did NOT move is the ACTION TABLES:
# each context maps every kind explicitly, and the loader's table
# (`_LOADER_ENTRY_ACTIONS`, below) is deliberately NARROWER than the server's
# `_ENTRY_ACTIONS`. `server.py` imports these names.
#
# 🔴 THE TWO GUARDED SITES ARE `/snapshot` AND THIS INDEX LOADER — AND THEY ARE
# NOT ALL THE SITES. An earlier wording here said they were, and that claim was
# false: `entry_shape.census()` (`glob("*.md")` then `read_text`) is a THIRD
# glob-and-read site with NO kind check at all, so it still hangs on a fifo and
# still 503-equivalents on a directory. It is left that way ON PURPOSE, by
# ruling: `census()` is CLI-only, no server path imports the writer half, and
# the operator declined to widen the guard to it. `validate_scope` also globs,
# but only for NAMES — it reads nothing itself and defers to `load_index`, so it
# inherits this guard rather than needing one. Said here so the next reader does
# not have to rediscover it, and so "two sites" stops reading as "all of them".
#
# Four consecutive audit rounds found the same shape of defect in `_snapshot`,
# and every fix added one more predicate to a sequence:
#
#   r1  entry symlinks followed          -> refuse symlinked ENTRIES
#   r2  symlinked SCOPE dirs filtered    -> silently omitted, read as scope-empty
#   r3  `is_symlink()` before `is_dir()` -> a symlinked README 503'd everything
#   r4  `is_dir()` first                 -> a DANGLING scope link vanished,
#                                           read as scope-empty at exit 0
#
# Each round the stated predicate ("refuse a thing that IS a scope but cannot be
# served safely; skip a thing that is not one") failed to DECIDE the next input
# class, because a broken pointer is neither. Adding an arm fixes the instance;
# it does not make the rule total, so the next class falls through the same gap.
#
# So: classify the path's TYPE exhaustively, in one place, and have each context
# map EVERY kind to an action explicitly. `_ROOT_ACTIONS`, `_ENTRY_ACTIONS` and
# `_LOADER_ENTRY_ACTIONS` are asserted complete by `TestClassifierIsTotal`, and
# an unmapped kind raises rather than defaulting — a fallthrough is a test
# failure, not a silent skip. Name-based rules (dotfiles, the `.md` suffix) stay
# SEPARATE from type, because conflating them is what made the dotfile and
# symlink rules interfere.
# =============================================================================

KIND_BROKEN_LINK = "broken-link"      # dangling target, or a symlink loop
KIND_LINK_TO_DIR = "link-to-dir"
KIND_LINK_TO_FILE = "link-to-file"
KIND_LINK_TO_OTHER = "link-to-other"  # link to a fifo/socket/device
KIND_DIRECTORY = "directory"
KIND_REGULAR_FILE = "regular-file"
KIND_OTHER = "other"                  # fifo, socket, device, door…
# 🔴 THE LAST CELL OF THE LOOP: the first version of this classifier was total
# over KIND STRINGS but not over KNOWLEDGE. "I could not determine what this is"
# is its own answer and must never share a bucket with "I know exactly what this
# is".
#
# ⚠ CORRECTED — AND THE CORRECTION IS THE INTERESTING PART. This comment used to
# say "every pathlib predicate returns False when the stat itself fails, so an
# EACCES fell into KIND_OTHER and was skipped", and cited a measurement of
# `/snapshot` answering 200 / exit 0 / entries=0. BOTH WERE WRONG, and the
# second was inherited from an audit report and written up here as first-hand.
# ⚠ And the raise did NOT come from `classify_path` — that function is INTRODUCED
# by this fix. In the failing version it came from `candidate.is_dir()` in
# `_snapshot`, which this commit deletes. Corrected after an audit caught the
# anachronism; the docstring below was already accurate.
# Measured on the pinned interpreter:
#     pathlib._IGNORED_ERRNOS == (ENOENT, ENOTDIR, EBADF, ELOOP)
#     child of a 0o600 dir: is_symlink/is_dir/is_file/exists each RAISE
#                           PermissionError — none returns False
# So the old failure was not a silent empty store; it was an UNCAUGHT
# PermissionError out of `classify_path`, crashing the handler with no response
# and no audit line. Loud, not quiet.
#
# The fix stands — a crashed handler becoming a typed 503 is strictly better —
# but the reasoning had to be corrected, because "pathlib returns False on stat
# failure" is exactly the kind of false premise that gets a guard deleted
# somewhere else in this file on the strength of a comment.
KIND_INDETERMINATE = "indeterminate"
# Vanished between `readdir` and the stat — a benign race, not a hazard, and
# distinct from INDETERMINATE because the right action differs.
KIND_ABSENT = "absent"

ALL_KINDS: frozenset[str] = frozenset(
    {
        KIND_BROKEN_LINK,
        KIND_LINK_TO_DIR,
        KIND_LINK_TO_FILE,
        KIND_LINK_TO_OTHER,
        KIND_DIRECTORY,
        KIND_REGULAR_FILE,
        KIND_OTHER,
        KIND_INDETERMINATE,
        KIND_ABSENT,
    }
)

# What a context does with each kind. `SKIP` = not the thing we are looking for,
# so its absence is not a fact worth reporting. `TAKE` = use it. `REFUSE` = it
# IS (or claims to be) the thing, and we cannot serve it — which must be
# REPORTED, never skipped, because a skip renders as "nothing recorded".
SKIP, TAKE, REFUSE = "skip", "take", "refuse"


def classify_path(path: Path) -> str:
    """The path's type, as exactly one of `ALL_KINDS`. Total by construction.

    🔴 ONE `lstat`, THEN THE MODE BITS — not a sequence of pathlib predicates.
    Those predicates fail in TWO different ways and neither is usable here:
    they return False for the errnos in `pathlib._IGNORED_ERRNOS` (ENOENT,
    ENOTDIR, EBADF, ELOOP), so "not a directory" and "no such path" are
    indistinguishable; and they RAISE for every other errno (EACCES, ESTALE,
    EIO), so a sequence built from them can abort mid-classification and take
    the handler with it. The previous version did exactly that on an
    unstat-able child.

    Reading the mode bits from one explicit `lstat` makes both cases answerable:
    a failure is an exception we must classify, not a False we might miss or a
    raise we did not expect. It also halves the syscalls, which narrows the
    TOCTOU window between them.
    """
    try:
        st = path.lstat()
    except FileNotFoundError:
        return KIND_ABSENT
    except OSError:
        # EACCES on the parent, ESTALE, EIO… We do not know what this is, and
        # saying so is the point — see KIND_INDETERMINATE.
        return KIND_INDETERMINATE

    if not stat.S_ISLNK(st.st_mode):
        if stat.S_ISDIR(st.st_mode):
            return KIND_DIRECTORY
        if stat.S_ISREG(st.st_mode):
            return KIND_REGULAR_FILE
        return KIND_OTHER

    try:
        target = path.stat()  # follows the link
    except OSError as exc:
        # ENOENT = dangling, ELOOP = a cycle (measured: self-loop, mutual loop
        # and a 45-link chain all give ELOOP). Both are broken POINTERS, which
        # is a fact we know; anything else means the stat failed for a reason we
        # cannot interpret, which is not.
        #
        # ⚠ NO ACTION DEPENDS ON THIS BRANCH IN THE SERVER'S TWO TABLES, and
        # saying so is honest rather than leaving it to read as coverage:
        # BROKEN_LINK and INDETERMINATE map to the SAME action in both of them,
        # so there the split only changes the word in the 503 body. ⚠ THAT IS NO
        # LONGER TRUE OF EVERY TABLE: `_LOADER_ENTRY_ACTIONS` REFUSES
        # `broken-link` and TAKES `indeterminate`, so a mutant collapsing this
        # branch is now killable through the loader (it turns the Emacs lock file
        # back into a store-wide `EntryUnreadableError`). Errnos measured to land
        # in `indeterminate` rather than `broken-link`: ENOTDIR (`/etc/hosts/x`),
        # ENAMETOOLONG (300-char target), EACCES (link into an unsearchable dir).
        if exc.errno in (errno.ENOENT, errno.ELOOP):
            return KIND_BROKEN_LINK
        return KIND_INDETERMINATE
    if stat.S_ISDIR(target.st_mode):
        return KIND_LINK_TO_DIR
    if stat.S_ISREG(target.st_mode):
        return KIND_LINK_TO_FILE
    return KIND_LINK_TO_OTHER


def action_for(kind: str, actions: Mapping[str, str]) -> str:
    """Look up the action, refusing to guess. An unmapped kind is a BUG."""
    try:
        return actions[kind]
    except KeyError:  # pragma: no cover - pinned by TestClassifierIsTotal
        raise AssertionError(
            f"unclassified path kind {kind!r}: every kind must be mapped "
            f"explicitly, because a default is how the last four rounds of this "
            f"defect happened"
        ) from None


def visible_scope_set(visible_scopes: Sequence[str] | None) -> set[str] | None:
    """One allowlist -> the normalized set every narrowing site compares against.

    🔴 ONE PLACE, because this predicate is now spelled at three of them — the
    index loader below, `subsystem_recall.load_store`, and the API's `/snapshot`
    candidate list, which walks the store root directly and so cannot use the
    index at all. Open-coded at three sites it would be wrong at two of them in
    the same direction, and the direction that matters here is "wider than the
    caller's allowlist".

    🔴 `None` IN, `None` OUT — UNRESTRICTED. An EMPTY sequence returns an EMPTY
    SET, which is its opposite: nothing is visible. Both are falsy, so every
    caller must test `is None`, never truthiness; that asymmetry is the whole
    fail-closed direction of the scoped-token design.
    """
    if visible_scopes is None:
        return None
    return {normalize_ref(s) for s in visible_scopes}


# 🔴 THE LOADER'S OWN TABLE, AND IT IS DELIBERATELY NARROWER THAN THE SERVER'S.
#
# The broad form — mirroring `server.py`'s `_ENTRY_ACTIONS` wholesale — was
# written, reviewed and REJECTED, because it also refuses `link-to-file` and
# `indeterminate`, which this loader READS (or honestly fails on) today: that is
# a behaviour change for every local CLI caller and for the writer's probe, and
# those two cells are still `TAKE` for exactly that reason. The cells that HAVE
# been decided, one ruling at a time, each on the same criterion — "this loader
# has never successfully read one, so refusing it changes no legitimate caller"
# — are the five below. The first ruling decided two:
#
#   `broken-link`  a dangling symlink. `is_entry_filename` ACCEPTS A LEADING
#                  DOT — measured, not assumed — so an Emacs lock file
#                  (`.#entry.md`, a dangling link to `user@host.pid:boot`) is a
#                  candidate entry. Opening it raised, and because an OSError
#                  fails closed in BOTH policies that took `/recall` down for
#                  EVERY caller, naming the file and its scope in the 503.
#   `other`        fifo, socket, device. `read_text` on a FIFO BLOCKS until
#                  somebody writes to it: on a `replicas: 1` Deployment the
#                  request thread never returns. This is the cell that is not a
#                  degradation but a hang.
#
# 🔴 AND NOW A THIRD, WHICH IS THE SECOND ONE IN A DIFFERENT SHAPE:
#
#   `link-to-other`  a symlink POINTING AT a fifo/socket/device. It shipped
#                  `TAKE` for one round and was written up here as a NAMED
#                  RESIDUAL — the decision that created this table named `other`
#                  and `broken-link` and nothing else. It was then MEASURED
#                  rather than argued about, on the tip that carried it:
#
#                      store/bravo/link-to-fifo.md -> /tmp/thefifo (a real fifo)
#                      GET /api/v1/recall/alpha, UNRESTRICTED legacy token
#                      -> no response at 25s, request thread WEDGED (curl 124)
#                      -> /healthz 200 throughout, so the process was UP and the
#                         worker was gone — the positive control for the probe
#
#                  `open()` does not care which path shape reached the fifo, so
#                  this was never a lesser defect than `other`; it was the same
#                  one, and on a `replicas: 1` / `strategy: Recreate` service a
#                  wedged thread is the worst outcome in this file. Refused.
#
# 🔴 AND NOW A FOURTH AND FIFTH, ON THE SAME CRITERION THE NARROW RULING USED:
#
#   `directory`    a DIRECTORY named `*.md`. `read_text` on one raises
#   `link-to-dir`  `IsADirectoryError`, and because an OSError fails closed in
#                  BOTH policies that was a store-wide `503 index entry
#                  unreadable` for EVERY caller — `/recall` AND `/search` — off
#                  one stray `mkdir <scope>/<slug>.md` or an rsync/restore
#                  artefact. Measured on the tip that carried them, with a
#                  paired control:
#
#                      store/beta/notes.md created as a DIRECTORY
#                      GET /api/v1/recall/alpha, UNRESTRICTED legacy token
#                      -> 503 "index entry unreadable: … (IsADirectoryError:
#                         … /beta/notes.md)"
#                      CONTROL, same shape but a dangling `.#lock.md` -> 200
#
#                  A symlink to a directory measured identically. 🔴 THE
#                  CRITERION IS UNCHANGED, WHICH IS WHY THIS IS NOT A WIDENING
#                  OF THE RULING: this loader has NEVER successfully read a
#                  directory — it has only ever raised on one — so refusing it
#                  changes no legitimate caller's behaviour, exactly as with
#                  `broken-link`, `other` and `link-to-other`. It also makes the
#                  loader AGREE with `/snapshot`'s `_ENTRY_ACTIONS`, which
#                  already refused both kinds; two readers of one store
#                  disagreeing about what a directory named `*.md` is was the
#                  divergence, not the fix.
#
# None of the five is ever a legitimate entry, so no legitimate caller changes
# behaviour.
#
# 🔴 `link-to-file` IS `TAKE`, AND THAT IS THE POINT OF THE NARROW FORM — it is
# the cell the narrow-vs-broad ruling was actually about, and closing
# `link-to-other`, `directory` and `link-to-dir` did not touch it. A symlink to
# a regular `*.md` is read today and keeps being read. A mutant that flips this
# cell to REFUSE is the exact over-broad regression the narrow ruling exists to
# prevent, and `test_a_SYMLINKED_entry_is_STILL_READ_the_guard_is_NOT_the_broad_one`
# kills it.
#
# ⚠ `indeterminate` and `absent` are `TAKE`, and for a DIFFERENT reason from the
# two cells that just left this list — one this round was told explicitly not to
# fold in. `indeterminate` means "the `lstat` failed and I could not look",
# which is not "this kind can never be an entry"; `absent` is a file that
# vanished between the listing and `classify_path`. `read_text` raises on each
# (EACCES, FileNotFoundError) and that raise is the four-state rule's "the store
# was not fully READ" — a DIFFERENT fact from "this entry is malformed", which
# must not be quietly folded into it. `regular-file` and `link-to-file` are
# `TAKE` because they are what an entry IS; the residuals they carry are
# enumerated in `load_index`'s RESIDUAL LEDGER and pinned by
# `test_the_LOADER_RESIDUAL_SET_is_pinned`.
_LOADER_ENTRY_ACTIONS: dict[str, str] = {
    KIND_BROKEN_LINK: REFUSE,
    KIND_OTHER: REFUSE,
    KIND_LINK_TO_OTHER: REFUSE,
    KIND_DIRECTORY: REFUSE,
    KIND_LINK_TO_DIR: REFUSE,
    KIND_REGULAR_FILE: TAKE,
    KIND_LINK_TO_FILE: TAKE,
    KIND_INDETERMINATE: TAKE,
    KIND_ABSENT: TAKE,
}

# The `why` clause each refused kind carries — the sentence after the sentinel,
# so a refused entry reads exactly like every other unusable one. It names the
# SHAPE and never invents a fix, because the operator's fix differs per shape
# (delete the lock file; delete the fifo).
_LOADER_REFUSAL_REASON: dict[str, str] = {
    # 🔴 IT NAMES `is_entry_filename`, NOT A GLOB, AND THAT IS A CORRECTION THIS CHANGE
    # HAD TO MAKE. This string said "`glob('*.md')` matches a leading dot"; there is no
    # glob in the entry walk any more — `entry_files_in` uses `iterdir()` +
    # `is_entry_filename` — so it cited a mechanism that no longer exists while its
    # conclusion stayed true. Worse than a stale comment, because it is a RUNTIME STRING:
    # it reaches a 503 body and `validate`'s stderr. Named for the PREDICATE rather than
    # the WALK on purpose — `is_entry_filename` is what decides, so a future change of
    # walk cannot make this stale again.
    # 🔴 AND IT MUST STAY BYTE-IDENTICAL TO `internal/store/load.go`'s
    # `loaderRefusalReason[KindBrokenLink]` — measured identical before this edit and
    # after, and moved in the same commit. ⚠ NOTHING ASSERTS THAT IDENTITY: no test
    # compares the two literals, and no parity or dualrun world seeds a dangling
    # `.#*.md`, so the requirement rests on this comment and its twin. Change one side
    # and you must change the other by hand.
    KIND_BROKEN_LINK: (
        "broken symlink (a dangling target, or a link loop) — not an entry, and "
        "refused before `open()`. `is_entry_filename` accepts a leading dot, so an "
        "editor lock file such as `.#<entry>.md` lands here; reading it raised "
        "`index entry unreadable`, which took the whole store down for every "
        "caller and named this file in the error"
    ),
    KIND_OTHER: (
        "not a regular file (a fifo, socket or device) — refused before "
        "`open()`, which on a fifo blocks until somebody writes to it and never "
        "returns, wedging the reader"
    ),
    KIND_LINK_TO_OTHER: (
        "a symlink to something that is not a regular file (a fifo, socket or "
        "device) — refused before `open()`, which blocks on a fifo whether it "
        "is named directly or reached through a link. Measured wedging a "
        "`/recall` request thread for 25s while the process stayed healthy"
    ),
    KIND_DIRECTORY: (
        "a directory named `*.md` — refused before `open()`, which on a "
        "directory raises `IsADirectoryError`; that OSError fails closed, so "
        "one stray `mkdir <scope>/<slug>.md` (or an rsync/restore artefact) "
        "answered `/recall` and `/search` with a store-wide 503 for every "
        "caller. Delete the directory, or move its contents into a `*.md` file"
    ),
    KIND_LINK_TO_DIR: (
        "a symlink to a directory — refused before `open()`, which raises "
        "`IsADirectoryError` whether the directory is named directly or "
        "reached through a link, and that OSError fails closed into the same "
        "store-wide 503"
    ),
}


#: The ONE filename a scope directory carries that is not an entry: each scope
#: dir holds one as its store-policy sheet, and `/snapshot` ships it, so every
#: real cache has them.
#:
#: 🔴 THE SPELLING IS EXACT AND THAT IS THE WHOLE RULE — not a prefix, not a case
#: fold. `readme.md` and `README-old.md` are ORDINARY ENTRIES: the loader walks
#: them, indexes them and can reject them as malformed, so a consumer that
#: excluded them would drop a file the loader counted and drive a printed
#: numerator below zero.
SCOPE_POLICY_SHEET = "README.md"


def is_entry_filename(name: str) -> bool:
    """Is this filename an ENTRY in a cached scope — the one rule, in one place.

    🔴 IT EXISTS BECAUSE THE RULE WAS OPEN-CODED AT FOUR PRODUCTION SITES AND WAS
    WRONG AT TWO OF THEM, IN THE SAME DIRECTION. `load_index` below skipped the
    sheet; `cairn validate` did not (fixed separately — it printed `3 of 3` over
    two entries beside one sheet); `cairn ls-entries` did not either, and that is
    the verb `README.md` advertises as *"what the cache actually holds"*, so it
    asserted every scope's policy sheet was an entry. A predicate open-coded at N
    sites is typically wrong at N-1 of them; consolidating it is what made the
    disagreement audible.

    ⚠ THE `.md` HALF IS NOT REDUNDANT WITH A CALLER'S GLOB EVEN WHERE THE GLOB HAS
    ALREADY APPLIED IT. Stating the whole rule here is what lets a caller that has
    NOT globbed ask the same question and get the same answer.

    🔴 AND IT COMPARES THE BASE NAME, BECAUSE THE SENTENCE ABOVE IS A PROMISE THE
    FIRST CUT DID NOT KEEP. That cut compared the WHOLE argument, so an un-globbed
    caller holding `scope + "/" + name` — the same shape `ls-entries` prints — got
    the opposite answer: `is_entry_filename("notes/README.md")` returned True, the
    policy sheet classified as an entry, which is the exact defect the
    consolidation exists to eliminate, regenerated inside the consolidated rule.
    Basing the comparison makes the predicate TOTAL over its input rather than
    narrowing the promise to "base names only": the answer is the same however a
    caller spells the path.

    🔴 THE SNAPSHOT WALK IS NOT THAT CALLER, AND AN EARLIER FORM OF THIS DOCSTRING
    POINTED AT IT — it said "an archive member list IN THIS REPO", of which
    `/snapshot` is the only one. `/snapshot` does build `scope + "/" + name`
    arcnames, but it MUST SHIP every scope's policy sheet — that is how each cache
    gets one — so routing its member list through this predicate would drop them
    from the archive and break the thing `SCOPE_POLICY_SHEET`'s own first paragraph
    depends on.

    🔴 THE CITATION IS RE-DERIVED, BECAUSE THE NAME THAT STOOD HERE HAS NEVER
    EXISTED. This paragraph named `internal/snapshot.chooseEntries`;
    `find … -print0 | xargs -0 grep` over the tree returns it ONLY from this
    docstring and its Go twin, `git log -S chooseEntries -- internal/snapshot/` is
    empty, and the same grep DOES return `Build`, `Freshness` and `RootAction` from
    that package — so the zero is the grep working, not a broken pattern. A
    maintainer greps a name like this to decide whether the snapshot walk may route
    through this predicate, and a name with no hits leaves them unable to tell a
    stale citation from a measurement never taken.

    There is no such function. The two member lists are `server.py`'s `_snapshot`,
    filtering `p.name.endswith(".md") and not p.name.startswith(".")`, and — built
    INLINE, not in a helper — `internal/snapshot.Build`, filtering
    `strings.HasSuffix(name, ".md") && !strings.HasPrefix(name, ".")`. MEASURED at
    this head by READING both, and then BEHAVIOURALLY over a scope holding
    `README.md` beside one ordinary entry: `GET /api/v1/snapshot` shipped
    `widget-cfg/README.md` among its four members, and `Build` shipped
    `<scope>/README.md` beside `<scope>/<entry>.md` (entries=2). Neither carries a
    README exclusion. The SHAPE is the
    point; the snapshot/transfer walks are an explicit EXCLUSION from it, because
    they compare against `X-Store-Entries` and must include sheets.

    ⚠ THIS CHANGES NOTHING FOR TODAY'S CALLERS, AND THAT WAS CHECKED RATHER THAN
    ASSUMED. `entry_files_in` below passes `p.name` and `cairn ls-entries` passes
    `path.name`, so both were already handing it base names, for which
    `PurePosixPath(...).name` is the identity. `PurePosixPath("").name` is `""`,
    which carries no `.md` suffix, so the empty name stays False.
    """
    base = PurePosixPath(name).name
    return base.endswith(".md") and base != SCOPE_POLICY_SHEET


def entry_files_in(scope_dir: Path) -> list[Path]:
    """The ENTRY files in ONE cached scope directory, sorted.

    🔴 THE LOADER AND `cairn validate` BOTH GO THROUGH THIS, WHICH IS THE POINT.
    The original defect was two walks behind one printed line: the numerator came
    from `load_index` and the denominator from a bare `*.md` glob, so the two
    could disagree about what an entry is. They now cannot.

    🔴 `iterdir()`, NEVER `glob("*.md")` — THE GLOB SWALLOWS `EACCES` AND SERVES A
    FALSE ABSENCE AT EXIT 0. `pathlib.Path.glob` suppresses the `OSError` its own
    directory scan raises and yields NOTHING; `iterdir()` raises. Over a scope
    directory at mode `000` that difference is the whole of the answer, because an
    empty listing from THIS function is indistinguishable downstream from a
    directory somebody made and never filled: the scope registers with zero
    entries, `recall` reports `status=scope-empty`, and the client prints
    "NOTHING RECORDED YET — `<scope>/` exists but holds no entries … Not an
    error." at exit **0** over a directory whose contents were never read.
    MEASURED on CPython 3.12.14 over one directory holding one `*.md` file at
    `chmod 000`: `glob("*.md")` → `[]`, `iterdir()` → `PermissionError [Errno 13]`.
    And MEASURED end-to-end on both clients over a cached scope at mode `000`,
    before this line changed: `cairn recall` and `cairn validate` each answered
    **0** with `status=scope-empty` on the oracle and **3** with `index entry
    unreadable: under <root> (PermissionError: …)` on the Go client, which reaches
    the same directory through `os.ReadDir` and has never had the suppression.

    🔴 AND IT IS **NOT** CACHE-ONLY — IT MOVES SERVER AVAILABILITY TOO, in the
    fail-closed direction. `load_index` — which calls this function for every
    visible scope — is what the POD loads through (`subsystem_recall.load_store`,
    and `server/server.py`'s read and write routes). MEASURED over a store holding
    `good` and `locked` at mode 000, by calling `load_store` with each allowlist
    against `lib/` at both commits:

        visible_scopes   278b8df (`glob`)               e162746 (`iterdir`)
        --------------   ----------------------------   ------------------------
        None             `good` + `locked` registered,  EntryUnreadableError
                         `locked` served `scope-empty`  -> 503 for EVERY scope
        ['good']         `good` alone, 200              `good` alone, 200

    🔴 THE TRADE IS DELIBERATE, AND IT IS THE ONE THIS STORE ALREADY MAKES ONE
    LEVEL DOWN RATHER THAN A NEW POLICY. A single mode-000 ENTRY FILE has always
    taken `/recall` and `/search` down for every UNRESTRICTED caller — that
    measurement is in `load_index`'s `visible_scopes` block below, and
    `test_an_UNREADABLE_REGULAR_FILE_still_RAISES_and_THAT_is_the_residual` is its
    honest record. A directory whose listing could not be taken is strictly less
    knowable than a file that could not be opened, so serving its siblings while
    calling it empty was the inconsistency; closing it is what makes `load_index`'s
    ⚠ `OSError` paragraph true at the directory level too. A SCOPED caller is
    unaffected either way, because the allowlist is applied before any walk. Gated
    server-side by `TestAnUnreadableSCOPEDIRECTORYIsNotAnEmptyScopeOnTheSERVER`,
    whose unrestricted arm is RED at `278b8df`.

    ⚠ AND IT CLOSES A SERVER-SIDE DIVERGENCE RATHER THAN CREATING ONE. The Go
    store has never had the suppression — `mdNamesIn` walks with `os.ReadDir` — so
    `internal/store.LoadStore` already answered a mode-000 scope directory with
    this exact sentence, pinned as an INVARIANT guard by
    `TestAnUnreadableScopeDirFailsClosedWhileAnEmptyOneLoads`. So before this line
    the two servers disagreed about the same directory; they now agree. Reverting
    toward availability would have to move the GO side too.

    🔴 IT RAISES RATHER THAN REPORTING, AND THAT IS THE EXISTING POLICY, NOT A NEW
    ONE. `load_index`'s ⚠ `OSError` paragraph already says an unreadable path
    "fails closed in both modes … the set of entries is then unknown, so there is
    nothing honest to degrade to", and `load_store` owns the one wrap that turns
    it into the named `index entry unreadable` sentence. This function therefore
    adds no `except` of its own: the `PermissionError` travels the route a
    mode-000 ENTRY FILE already travels, and both clients print the same bytes.

    ⚠ AN EMPTY SCOPE IS STILL EMPTY, AND THE TWO ARE SEPARATED BY MECHANISM
    RATHER THAN BY A PREDICATE. `iterdir()` over a readable directory holding no
    entries returns `[]` exactly as the glob did — so `scope-empty` at exit 0,
    with its "Not an error" sentence, is unchanged for the state it is true of.
    Only the case that CANNOT produce an honest listing now raises.

    ⚠ THE FILENAME FILTER IS UNCHANGED, AND THE EQUIVALENCE WAS MEASURED RATHER
    THAN ASSUMED. Over one directory holding `a.md`, `.#lock.md`, `.md`,
    `README.md`, `b.MD`, `c.md.txt`, `d.markdown` and a DIRECTORY named `sub.md`,
    `glob("*.md")` and `iterdir()` filtered through `is_entry_filename` return the
    identical list `['.#lock.md', '.md', 'a.md', 'sub.md']`. The leading dot and
    the directory are both KEPT on purpose — see the ⚠ note below.

    ⚠ A LEADING DOT IS IN THIS SET — measured, not assumed — so a dangling
    `.#entry.md` editor lock file IS a candidate. That is deliberate:
    `classify_path` is what refuses it, and refusing it is a REPORTED rejection
    rather than a silent drop.

    🔴 A VANISHED DIRECTORY NOW RAISES `FileNotFoundError` WHERE THE GLOB RETURNED
    `[]`, AND FOR ONE CALLER THAT RAISE IS UNWRAPPED — STATED HERE BECAUSE IT IS
    NOT CLOSED. `load_index`'s own call site is inside `load_store`, which owns the
    `EntryUnreadableError` wrap. `cairn validate` reads the scope directory a
    SECOND time, for the printed line's DENOMINATOR, and that read is outside every
    wrap while the CLI's reader-error arm catches `(StoreMissingError,
    ResolverError)` and deliberately NOT `OSError`. So a scope directory that
    vanishes BETWEEN the two walks — a concurrent `cairn sync`, whose
    `install_snapshot` renames the cache root aside and `rmtree`s the retired one —
    escapes as a traceback at exit 1, where the Go client prints `0 of 0 entry
    file(s) parse, 0 malformed` at exit 0 (MEASURED at `e162746`). It needs the
    world to CHANGE MID-RUN, so no static world and no `chmod` reaches it: an
    unreadable directory fails the WRAPPED first walk and never gets this far.
    UNGATED and declared, in `tests/parity/README.md` → "What the gate structurally
    cannot see" — not fixed.
    """
    return sorted(p for p in Path(scope_dir).iterdir() if is_entry_filename(p.name))


def load_index(
    root: Path,
    *,
    on_malformed: str = ON_MALFORMED_RAISE,
    visible_scopes: Sequence[str] | None = None,
) -> SubsystemIndex:
    """Read `<root>/<scope>/*.md` into a `SubsystemIndex`. READ-ONLY.

    `README.md` is skipped in every scope — each scope dir carries one as its
    store-policy sheet, and it is not an entry. The skip is `entry_files_in`'s,
    not this function's: three other sites answer the same question and two of
    them answered it differently until the rule was consolidated there.

    A scope dir with no entries is REGISTERED, not dropped: "Lazy — a scope dir
    or service file may not exist yet". An existing empty scope must resolve to
    an honest empty result, while a scope that does not exist stays an error.

    🔴 FAIL-CLOSED BY DEFAULT, AND THE CALLER CHOOSES. `on_malformed=RAISE` (the
    default) aborts the whole index on the first bad entry — correct for a WRITER
    about to modify a curated store. `COLLECT` degrades: the good entries load
    and the rejects come back on `index.malformed` for the caller to PRINT.

    The question this docstring used to defer is now answered, and the answer was
    measured. Fail-closed cost the whole scope: on a synthetic store, 2 good
    entries listed 2, and 2 good + 1 malformed listed **0** with exit 3 — one
    wrapped `aliases:` line took `/resume` step 4, `--list`, `--ref` and
    `--search` down together. The reader (`subsystem_recall`) therefore loads
    with `COLLECT` and reports every reject per-entry in the same output; the
    writer's probe keeps `RAISE`, because it
    gates a WRITE into a store it would then be reading only partially.

    🔴 What `COLLECT` is NOT is "skip the bad entry". Silently serving a short
    index is a worse failure than the collapse, because a dropped entry is
    indistinguishable from an entry nobody ever wrote. The obligation to print is
    part of the mode; see `ON_MALFORMED`.

    ⚠ AN `OSError` STILL FAILS CLOSED IN BOTH MODES, and that is a different
    fact, not an oversight: a malformed entry is a file we read and could not
    interpret, while an unreadable one means the store was not fully READ — the
    set of entries is then unknown, so there is nothing honest to degrade to.
    `load_store`/`build_report` name it `index entry unreadable`.

    🔴 `visible_scopes` IS APPLIED BEFORE ANY FILE IS OPENED, AND THAT IS THE
    POINT OF IT BEING HERE RATHER THAN ON THE RESULT. Narrowing the index
    afterwards still WALKS and READS every scope, which with a scoped API caller
    in front of it is three separate defects, all measured:

      * DISCLOSURE. One unreadable entry made `/recall` and `/search` answer
        `503 index entry unreadable: under <store> (PermissionError: …
        '<store>/<denied-scope>/locked-entry.md')` — the full path of a file in
        a scope that caller is not allowed to know exists. A scoped reader had
        no other way to learn that name.
      * DENIAL OF SERVICE. One unreadable file anywhere in the store broke
        recall for EVERY caller, including the ones whose own scopes were fine.
      * A HUNG THREAD. `read_text` on a FIFO named `*.md` blocks until somebody
        writes to it. On a `replicas: 1` service that is worse than the 503: the
        request never returns and the worker is gone. `/snapshot` already
        refuses non-regular files by kind; this path had no such guard, and now
        does not need one for a scope the caller cannot name.

    An unreadable file in a scope the caller CAN see still raises — that is the
    four-state rule and it is unchanged. The local CLI passes no allowlist at
    all (`None`), so its behaviour is byte-identical to before.

    🔴 AND THE ALLOWLIST ALONE WAS NOT THE WHOLE FIX — `visible_scopes=None`
    SKIPS NOTHING. For an UNRESTRICTED caller the narrowing above does exactly
    nothing, and a bare legacy row is unrestricted, which is what the pod is
    deployed with today. Measured on this tree BEFORE the entry-kind guard
    below, `load_store` over a store whose `quartz-mine` scope holds one hostile
    file:

        visible_scopes=None        FIFO `hang.md`     -> HUNG (>6s, killed)
        visible_scopes=None        `.#locked.md`      -> EntryUnreadableError
        visible_scopes=('kelp-forest',)  both         -> loads, scopes=(kelp-forest,)
        visible_scopes=('quartz-mine',)  FIFO         -> HUNG (its own scope)

    🔴 SO THE CANDIDATE'S KIND IS NOW CHECKED BEFORE `open()`, FOR EVERY CALLER
    — `_LOADER_ENTRY_ACTIONS`, above, which reuses `classify_path` rather than
    asking the question a second way. It is NARROW on purpose. THE REFUSED SET,
    named by KIND so this sentence is machine-readable and cannot drift the way
    the ledger below twice did: a dangling symlink (`broken-link`), a
    fifo/socket/device (`other`), a symlink POINTING AT one (`link-to-other`), a
    directory named `*.md` (`directory`) and a symlink pointing at a directory
    (`link-to-dir`) are refused. (END OF REFUSED SET.) Everything this loader has
    ever successfully READ — regular files AND symlinks to regular files — is
    still read, so no legitimate caller changes behaviour.
    `test_the_DOCSTRING_REFUSED_SET_names_every_REFUSE_kind_and_no_other` pins
    the marked span above against the table in both directions — it was PROSE
    for a round, naming none of the kinds, and nothing read it: dropping three
    of the five refusals from it left the whole suite green.
    (Cells 3-5 shipped in later rounds than the first two, each after the shape
    was measured: `link-to-other` wedging an unrestricted `/recall` thread for
    25s, and `directory`/`link-to-dir` 503ing the whole store on an
    `IsADirectoryError` off one stray `mkdir`.) A refusal is reported the way
    every other unusable entry is, through `on_malformed`: a `MalformedEntry` on
    the index under `COLLECT`, a `MalformedEntryError` under `RAISE`. One
    hostile file therefore costs that ONE entry, and is NAMED, instead of
    costing the whole store.

    ⚠ THE RESIDUAL LEDGER — what this guard does **NOT** cover, enumerated
    rather than left to be found, and machine-checked so it cannot silently go
    stale the way it twice did. It is exactly the kinds `_LOADER_ENTRY_ACTIONS`
    still maps to `TAKE`, because `TAKE` means `read_text` runs and any `OSError`
    it raises fails closed into a store-wide `EntryUnreadableError` — a 503, and
    for an unrestricted caller one that names the path:

      * `regular-file` — a `chmod 000` entry. `PermissionError`. This is the
        four-state rule working as designed: "the store was not fully READ" is
        a different fact from "this entry is malformed", and only the second has
        an honest degraded form.
      * `link-to-file` — the SAME shape reached through a symlink, when the
        TARGET is unreadable. Measured, not assumed: a link to a `chmod 000`
        regular file 503s identically. Listed separately because the kind is
        separate and the ledger must not read shorter than it is.
      * `indeterminate` — the `lstat` itself failed (EACCES on the parent,
        ESTALE, EIO…). Deliberately NOT refused: "I could not look" is a
        different premise from "this kind can never be an entry", which is the
        criterion every REFUSE cell above rests on.
      * `absent` — the candidate vanished between the LISTING and
        `classify_path`. A TOCTOU race, `FileNotFoundError`, unreproduced here
        rather than measured.

    `test_the_LOADER_RESIDUAL_SET_is_pinned` pins that set, and
    `test_the_RESIDUAL_LEDGER_names_every_TAKE_kind_and_no_REFUSE_one` pins THIS
    PARAGRAPH against the table in both directions — so a cell that becomes
    REFUSE cannot stay listed here (the drift that left a closed hang recorded
    as open for a whole round), and a new `TAKE` cell cannot go unlisted.
    (END OF RESIDUAL LEDGER)

    ⚠ A LEADING DOT IS IN THE CANDIDATE SET — measured, not assumed — so an Emacs
    lock file (`.#entry.md`, a dangling symlink) is a candidate and had been
    observed 503ing `/api/v1/recall/<scope>` in practice. That is the
    `broken-link` cell's whole reason for existing. ⚠ THE `.md` HALF OF THE SHAPE
    IS APPLIED RATHER THAN INHERITED, and this sentence used to say otherwise:
    `entry_files_in` walks with `iterdir()` and filters through
    `is_entry_filename`, which spells the suffix test itself — there is no glob to
    inherit it from. The candidate set is unchanged: over one directory holding
    `a.md`, `.#lock.md`, `.md`, `README.md`, `b.MD`, `c.md.txt`, `d.markdown` and a
    directory named `sub.md`, both walks return
    `['.#lock.md', '.md', 'a.md', 'sub.md']`.

    ⚠ NOT A SUBSTITUTE FOR THE RESULT NARROWING in `load_store`. That one is
    still authoritative for the shape of the answer; this one exists so the
    denied scope is never TOUCHED. Both call `visible_scope_set`, so they cannot
    come to disagree about what an allowlist means.
    """
    collecting = _check_on_malformed(on_malformed) == ON_MALFORMED_COLLECT
    allowed = visible_scope_set(visible_scopes)
    mappings: list[Mapping[str, object]] = []
    scopes: list[str] = []
    # Entries REFUSED by kind before `open()`. Kept separate from `mappings`
    # because they never become one — `build_index` never sees them, so nothing
    # downstream has to learn a second rejection shape.
    refused: list[MalformedEntry] = []
    # Both `sorted()` calls here are DETERMINISM guards: they fix which entry a
    # malformed-index error names, so the same store always produces the same
    # message. ⚠ The scope-level one is not observable from a test — no test can
    # control `iterdir()` order — so it is the one guard in this module a
    # mutation sweep cannot kill. Stated rather than left to look covered.
    for scope_dir in sorted(p for p in Path(root).iterdir() if p.is_dir()):
        if allowed is not None and normalize_ref(scope_dir.name) not in allowed:
            # 🔴 `continue` BEFORE `scopes.append`, not after. Appending first
            # still skips the READ, so it looks equivalent — and THROUGH
            # `load_store` it is, because that function's result narrowing drops
            # the key again on the way out. Measured: the swap survives a sweep
            # driven entirely through `load_store`. It is NOT equivalent for a
            # caller that uses this function directly (the writer half, and
            # `TestTheLoaderItselfTakesTheAllowlist`), which gets a denied
            # scope's NAME on `index.scopes` — the `known_scopes` enumeration
            # channel — for a directory nothing ever opened.
            #
            # 🔴 `is not None`, NEVER truthiness: an EMPTY allowlist means
            # nothing is visible, and `if allowed and …` would read it as
            # unrestricted. Same asymmetry, same fail-closed direction, same
            # invisibility through `load_store`.
            #
            # 🔴 The DIRECTORY NAME is folded before comparing, because the index
            # key `build_index` derives from it is folded (`extra_scopes` →
            # `normalize_ref`). A raw comparison drops a scope dir spelled
            # `Kelp_Forest` out of an allowlist that names `kelp-forest` — the
            # caller's OWN scope, silently emptied.
            continue
        scopes.append(scope_dir.name)
        # 🔴 THE ENTRY SET COMES FROM ONE FUNCTION, NOT FROM A README TEST
        # OPEN-CODED HERE. This loop used to spell `if md.name == "README.md":
        # continue` itself; three other sites spelled the same rule, and two of
        # them spelled it WRONG. See `entry_files_in`.
        for md in entry_files_in(scope_dir):
            # 🔴 WHAT IS THIS PATH — ASKED BEFORE IT IS OPENED, AND ASKED ONCE.
            # `classify_path` is the same function `/snapshot` uses; only the
            # action table differs, because the action is a property of the
            # context. The narrow-vs-broad argument is on the table itself.
            kind = classify_path(md)
            if action_for(kind, _LOADER_ENTRY_ACTIONS) == REFUSE:
                reason = _LOADER_REFUSAL_REASON[kind]
                if not collecting:
                    # 🔴 THE SAME POLICY, NOT A NEW ONE. Under `RAISE` this is
                    # indistinguishable from any other rejection — the writer's
                    # probe must not act on a store it read only part of, and
                    # that is as true of a fifo as of a bad `aliases:` line.
                    raise MalformedEntryError(
                        f"malformed index entry {md.name!r}: {reason}",
                        source=md.name,
                        why=reason,
                    )
                refused.append(
                    MalformedEntry(
                        scope=normalize_ref(scope_dir.name),
                        filename=md.name,
                        reason=reason,
                    )
                )
                continue
            mappings.append(
                entry_mapping(
                    md.read_text(encoding="utf-8", errors="replace"),
                    filename=md.name,
                    scope=scope_dir.name,
                )
            )
    index = build_index(mappings, extra_scopes=scopes, on_malformed=on_malformed)
    if not refused:
        return SubsystemIndex(by_scope=index.by_scope, malformed=index.malformed)
    # 🔴 MERGED HERE RATHER THAN PASSED INTO `build_index`. These rows have
    # ALREADY been through the `on_malformed` policy above (a non-collecting
    # caller never reaches this line), so handing them to a function whose whole
    # job is to APPLY that policy would be a second, silent policy site. The
    # scope is already registered by `extra_scopes`, so the empty-scope rule
    # `build_index` implements for its own rejects needs nothing here.
    return SubsystemIndex(
        by_scope=index.by_scope,
        malformed=index.malformed + tuple(refused),
    )
