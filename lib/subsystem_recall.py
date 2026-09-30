#!/usr/bin/env python3
"""Surface what the `/analyze-service` index already records about a scope.

THE READ HALF. The store had two writers — `/analyze-service` (infra recon) and
`/handoff` (the writer half, which this package does not ship) — and no general
reader. `/resume`
never opened it: a fresh session read the handoff doc, reconciled live state via
the resume flow's own resolver, and the store's stated purpose — "the terse
pointer sheet that
outlives this handoff doc" — outlived the doc with nobody looking at it.

🔴 THE PATTERN IS COPIED, NOT INVENTED. `/analyze-service` step 1 is why the
store stuck at all: "Read at recon START… surface its `## Pointers` +
`## Nuance / work-history` before re-discovering gotchas", labelled `from index`,
never presented as live observation. This module is that step, made deterministic
and pointed at a scope instead of at one service.

🔴 IT NEVER WRITES. Not "does not today": there is no write path in this file,
and `TestRecallNeverWrites` hashes a store tree either side of every mode and
every failure. The store is curated, client-confidential and not re-derivable by
re-running recon, so the only writers stay the two diff-first ones in the skills —
`/handoff` and `/analyze-service`, which now follow ONE append
protocol (`claude/skills/subsystem-index/SKILL.md`) rather than a copy each. They
were "confirm-gated" too until the y/N was retired; the diff is still shown.
(This sentence used to justify the single-writer rule with "has no off-machine
backup". It IS backed up — hourly local commits, daily age-encrypted bundles to
MinIO; see `claude/skills/analyze-service/reference/index-store.md` -> "Store
safety". The conclusion is unchanged because it never depended on that half.)

🔴 IT DOES NOT REIMPLEMENT MATCHING. Ref normalization, kind splitting, tier
resolution, ambiguity and the on-disk index shape all come from
`subsystem_resolver`, which is the executable authority. Scope derivation comes
from `entry_shape.scope_for_repo` — the WRITER's own function, imported
rather than re-spelled, so a reader and a writer in the same repo can never
disagree about which scope directory they mean. Neither a second matcher nor a
second normalizer is introduced here.


WHY IT SURFACES THE WHOLE SCOPE, AND NOT THE WRITER'S PATH-DERIVED SET
---------------------------------------------------------------------
the writer half picks entries by resolving a PATH WINDOW — the session's
transcript, the PRs a branch landed, or git — and keeping what clears
`min_paths`. That is right for a writer: it is answering "what did I touch?",
and it must not propose a bullet against a subsystem the session never opened.

Pointing the same rule at `/resume` produces a reader that goes blind exactly
when recall is worth most. A resuming session has:

  * NO PRs of its own — it has not done anything yet;
  * a transcript one turn long, naming no files;
  * a git window that is empty in the ordinary resume state (a clean tree at
    `origin/main`), and otherwise describes the PREVIOUS session's leftovers.

All three collapse to `looked-at-nothing`. So the writer's selection rule would
answer "the index has nothing for you" on a fresh session in a repo whose scope
is full — a confident zero produced by the question, not by the store.

The reader therefore selects by SCOPE ALONE: every entry under `<scope>/`,
in canonical ref order. Three reasons it can afford to:

  1. The unit is a REPO. A scope holds one repo's subsystems, and the store's own
     bloat discipline — pointers not copies, dated bullets ≤2 lines, prune on
     resolve — is what keeps an entry small enough that "all of them" is a page
     rather than a dump.
  2. Any narrowing predicate would be a SECOND selection rule that the writer
     does not have, free to drift from it, and its failure mode is to hide an
     entry silently. That is the exact shape this codebase keeps paying for.
  3. The volume is bounded and observable rather than assumed: `--limit` caps the
     output and a truncation is PRINTED (`… N more`), never silent.

`--ref` narrows to one entry when the caller already knows what it is looking at,
and it goes through `resolve_ref_tiered` — the writer's resolver — so an
ambiguous ref is reported with its candidates and never picked.


…AND WHY "THE WHOLE SCOPE" IS NOW AN *INDEX* PLUS *ONE BODY*
------------------------------------------------------------
The argument above is about SELECTION — which entries the reader is allowed to
know about — and it stands: the reader still reads every entry in the scope, and
`--list`/the digest index still name every one of them. What did not stand is the
claim `claude/skills/resume/SKILL.md` made about the COST: "it costs a page, not a
dump".

Measured against the scope holding 25 of the store's 29 entries:

    old default (`--limit 12`)   31,485 B  (~7,871 tok)  and it hid 13 of 25
    old `--limit 25`             62,643 B  (~15,660 tok)
    fixed caveat + header         1,290 B
    per entry                    ~2,454 B  (min 1,245 / median 2,277 / max 5,183)

So the old default was the worst of both worlds — expensive AND incomplete — and
a step that displaces the task it was loaded for gets dropped from `/resume`,
which is how the store went unread the first time.

The digest splits the two things the old output conflated:

    the INDEX     one line per entry, EVERY entry, never truncated (~60 B/line).
                  What a resuming session actually needs: what is on record here,
                  how much of it there is, and how sensitive it is.
    ONE BODY      the single entry most likely to be the one being resumed,
                  printed in full — because an index with no worked example is a
                  menu, and a menu costs a round trip.

Selection of that one body goes through `subsystem_resolver.associate_paths` —
the WRITER's own path→subsystem matcher — over a path window read out of the
repo's newest handoff doc (`focus_window`), and falls back to the newest entry by
mtime. Which of the two fired is PRINTED, never implicit.


…AND WHY THE INDEX IS NOW PAGINATED AT 100
------------------------------------------
The index line is ~60 B, so an unbounded index is a cost that grows with the
store forever: today's biggest scope holds 26 entries (~1.6 KB of index), but the
store is append-mostly and pruning is manual. `LISTING_PAGE_SIZE` caps a page at
100 lines (~6 KB) and the remainder is announced, never dropped — `--page N`
reaches it. The order is NEWEST-FIRST BY FILE MTIME, tie-broken by ref, and the
header says so: capping an ALPHABETICAL list hides entries by an accident of
their name, while capping a RECENCY list hides the stale ones, which is the only
cut that means anything to a resuming session. (`full` mode's bodies keep the
canonical ref-ascending order — that output is a dump by request and its reader
is scanning for a name, not for recency.)


…AND WHY THERE IS A `--search`
------------------------------
Reading by WHOLE ENTRY does not scale on either axis: entry count grows and entry
size grows. `--search` reads by MATCH instead — it returns HUNKS, each carrying
its own `scope/ref`, section and `sensitivity=`, because hunk output interleaves
entries and a label printed once per invocation would get separated from the
content it governs.

🔴 IT SHELLS OUT TO NOTHING, and that is a measurement, not a preference. The
whole store is ~81 KB / 30 files; a pure-stdlib scan of it takes single-digit
milliseconds, so `rg` would buy nothing and would cost a nix-store binary that is
not guaranteed on PATH plus an output format nobody pinned.

The scorer is TWO-STAGE, which is the shape a prototype round found correct
(ranking whole lines by `difflib` ratio returned noise; ranking a line's token
set returned front matter):

  1. ENTRY — an entry qualifies when its NAME (ref + aliases) or its best BLOCK
     clears the threshold.
  2. BLOCK — within a qualifying entry, every block at or above the threshold is
     emitted. If none is but the NAME qualified, the entry's best block is
     emitted with `basis=entry-name`, so a name-only hit is a hit and not the
     silent nothing the two-stage prototype produced.

A BLOCK — not a line — is the unit because a bullet is the unit the store is
written in: `nginx` on one line and `rate-limit` on its continuation line is ONE
fact, and a per-line scorer scores each half at 0.5 and returns nothing.

Compound/concatenated query terms are handled DELIBERATELY rather than by
lowering the cutoff: a block's candidate token set includes every ADJACENT PAIR
JOINED (`rate`,`limit` ⇒ `ratelimit`), so `ratelimit` hits `rate-limit` at 1.00,
and the reverse direction is covered by the prefix/substring rules. Tokens
shorter than `MIN_INEXACT_LEN` must match EXACTLY — one rule, and it is why `pod`
does not fuzzily reach `pgo`.

🔴 A SUB-THRESHOLD ZERO IS ACCOUNTED FOR. `SearchReport.best_below` carries the
highest-scoring hunk that did NOT clear the threshold, and the renderer prints it
on a no-match. "Nothing matched" and "something nearly matched at 0.50" are
different facts and a searcher that prints the same thing for both is the
confident-wrong instrument this module exists to avoid.


CONTRACT SUMMARY
----------------
    extract_sections(text, headings)          -> dict[str, str]
    read_entry(store_root, entry)             -> RecalledEntry
    focus_paths_from_text(text)               -> tuple[str, ...]
    focus_window(repo)                        -> FocusWindow
    select_featured(entries, index, scope, …) -> (ref, basis)
    recall(store_root, scope, *, ref=…, limit=…, mode=…, page=…, focus_paths=…)
                                              -> RecallReport
    render_text(report) / report_json(report) -> str / dict
    tokenize(text) / pair_strength(q, t) / score_unit(q_tokens, unit_TEXT)
    entry_blocks(text)                        -> tuple[Block, ...]
    search(store_root, scope, query, *, context=…, threshold=…, …)
                                              -> SearchReport
    render_search(report) / search_json(report) -> str / dict
    main(argv)                                -> int

🔴 THE FAILURE MODE IS A CONFIDENT ZERO, and an empty surface has FOUR causes
that mean different things. `RecallReport.status` names which, in values that
share no spelling:

    "scope-absent"    THIS HOST's store has no `<scope>/` directory — NOTHING
                      RECORDED YET, HERE. The ordinary case in most repos (the
                      store holds few scopes; work spans ~12 repos) and NOT an
                      error. 🔴 IT IS ALSO NOT A FACT ABOUT THE FLEET: this is a
                      per-host CACHE of the hosted store, fresh only to its last
                      `cairn sync`, so the OTHER machine may already hold that
                      scope and it simply has not arrived here. Measured
                      BEFORE the cutover made the pod canonical —
                      workbench 115 entries / 14 scopes, laptop 33 / 11, seven
                      scopes on the laptop alone and ten on the workbench alone.
                      Those caches now converge via the pod, but not at the
                      instant of a read. Every renderer says so; do not report
                      this status as "unrecorded" without saying "on this host".
    "scope-empty"     `<scope>/` exists and holds no entries. Also nothing
                      recorded yet, by a DIFFERENT mechanism — someone made the
                      directory. Kept apart on purpose: collapsing them would
                      make "the store was pruned to nothing" indistinguishable
                      from "this repo was never indexed".
    "scope-unreadable" `<scope>/` holds entry files and NOT ONE of them could be
                      indexed. A THIRD mechanism behind an empty surface, and the
                      one that is not a reading at all: there IS content here and
                      the tool cannot see it. Exits non-zero. Kept apart from
                      `scope-empty` for the same reason `scope-empty` is kept
                      apart from `scope-absent` — `/resume` reports an empty
                      scope as an ordinary non-finding, and reporting "we could
                      not read anything" that way would be a confident zero with
                      a broken store behind it.
    "ref-absent"      a `--ref` was given and resolved to no entry.
    "ref-ambiguous"   a `--ref` named more than one entry. Never picked.
    "recalled"        at least one entry surfaced.

and the two conditions that are NOT readings but broken environments, each with
its own sentinel phrase so a caller — or a mutation test — can tell WHICH fired:

    "store root not found"   StoreMissingError  (reused from the writer half:
                             the same condition, so the same class, not a
                             second spelling of one error)
    "index entry unreadable" EntryUnreadableError (reused from
                             subsystem_resolver, for the same reason —
                             the writer half raises it on the same files)


🔴 ONE MALFORMED ENTRY USED TO COST THE WHOLE SCOPE
---------------------------------------------------
It no longer does. Measured on a synthetic store, before this change:

    2 good entries          -> rc=0, both listed
    2 good + 1 malformed    -> rc=3, good entries still listed: 0

A single wrapped `aliases:` list — the front-matter parser is line-based, so a
list broken across two physical lines reads as an unterminated bare string —
took `/resume` step 4, `--list`, `--ref` and `--search` down together, and the
writer that produced it had no signal at all (`/handoff`'s own template prints
`aliases:` on ONE line, so the confirm-gate diff shown to the human contained
the defect while being structurally incapable of revealing it).

The reader now loads with `ON_MALFORMED_COLLECT`, so the good entries are served
and every reject is reported PER ENTRY — named, with its reason — in the same
output. 🔴 THE REPORT IS THE POINT, NOT THE DEGRADATION. Silently skipping a bad
entry would be strictly worse than the collapse it replaces: a dropped entry is
indistinguishable from an entry nobody ever wrote, which is the confident zero
this whole module exists to prevent. So the MALFORMED block renders on EVERY
status, before anything else, and the index header stops claiming completeness
the moment a reject exists.

    "malformed index entry"  the row's sentinel, one per rejected file. It is a
                             ROW and no longer a raise for the reader; the
                             WRITER's probe
                             still raises it, because it gates a write into a
                             store it would otherwise be reading in part.

🔴 EVERY OUTPUT PATH CARRIES THE CAVEAT, INCLUDING THE EMPTY ONES. It is a
single property on the report (`RecallReport.caveat`) that both renderers print,
rather than a sentence per branch — a caveat spelled at N sites is wrong at N−1
of them. Index content is labelled `from index` and is RECALL: it was curated by
past sessions, was not re-derived, and was not matched against anything this
session did.
"""

from __future__ import annotations

import argparse
import difflib
import json
import re
import sys
from collections import Counter
from dataclasses import dataclass, field
from pathlib import Path
from typing import Mapping, Sequence

sys.path.insert(0, str(Path(__file__).resolve().parent))

from subsystem_resolver import (  # noqa: E402
    NUANCE_HEADING,
    POINTERS_HEADING,
    REQUIREMENTS_HEADING,
    WHAT_HEADING,
    AmbiguousRefError,
    ON_MALFORMED_COLLECT,
    EntryUnreadableError,
    MalformedEntry,
    MalformedEntryError,
    ResolverError,
    SubsystemEntry,
    SubsystemIndex,
    UnknownScopeError,
    associate_paths,
    entry_has_tag,
    entry_references,
    load_index,
    normalize_ref,
    parse_front_matter,
    parse_journal_bullets,
    parse_requirements,
    parse_task_ref,
    resolve_ref_tiered,
    TaskRef,
    TaskRefError,
    visible_scope_set,
)
from subsystem_resolver import extract_sections as _extract_sections  # noqa: E402

# 🔴 IMPORTED AS A MODULE, NOT `from … import`. The CLI resolves the read store
# through `_read_store.read_store_root()` so the module global stays the single
# point of truth (and the single point a test can repoint). A `from … import
# DEFAULT_CACHE_ROOT` here would bind the value at import and make both
# properties false.
#
# 🔴 THIS IS A CLI CONCERN ONLY. Nothing below `main()`/`_build_parser()` may
# consult it: `recall`, `search`, `load_store` and `read_entry` are the LIBRARY,
# and the subsystem-store POD imports them to serve `/data` — a directory that
# has no `.sync-stamp` and never will. A refusal in the library would take down
# the pod and `scripts/cairn` with it.
import subsystem_read_store as _read_store  # noqa: E402

# The refusal's exit code, imported rather than re-declared. Binding it by value
# at import is safe in a way `DEFAULT_CACHE_ROOT` is not: it is an `int` nothing
# repoints, and the whole point is that this number is the SAME one
# `scripts/subsystem-audit.py` returns.
from subsystem_read_store import EXIT_UNSTAMPED_READ_STORE  # noqa: E402,F401
from entry_shape import (  # noqa: E402
    StoreMissingError,
    TouchError,
    scope_for_repo,
    store_caveat,
    store_host,
    store_host_line,
)
# 🔴 `store_caveat`, NOT `STORE_IS_PER_HOST`. The constant was imported here and
# interpolated directly; it is now reached through the function that decides
# whether the sentence needs its multi-instance clause, so this module has ONE
# way to say "what does an absence here mean" and it is the writer's. Importing
# the constant as well would leave a second, always-single-instance spelling
# beside it — which is exactly the shape that goes stale on the day somebody
# adds a clause to one of them.
# 🔴 THE PER-HOST HEADER IS THE WRITER'S SPELLING, IMPORTED, NOT RE-TYPED — and
# `store_host` rather than `host_identity.this_host` for the same reason. The
# reader and the writer describe the SAME directory; two spellings of "whose disk
# is this" would disagree the first time one of them was edited, and this is a
# claim about the SCOPE of every verdict below it. One seam, in the writer half.

__all__ = [
    "RECALL_LABEL",
    "WHAT_HEADING",
    "POINTERS_HEADING",
    "NUANCE_HEADING",
    "REQUIREMENTS_HEADING",
    "SURFACED_HEADINGS",
    "COUNTED_HEADINGS",
    "DEFAULT_ENTRY_LIMIT",
    "DEFAULT_MODE",
    "RECALL_MODES",
    "LISTING_PAGE_SIZE",
    "DEFAULT_SEARCH_THRESHOLD",
    "FUZZY_FLOOR",
    "MIN_INEXACT_LEN",
    "DEFAULT_MAX_HITS",
    "CONTEXT_BULLET",
    "HUNK_BASES",
    "FOCUS_MIN_PATHS",
    "HANDOFF_GLOBS",
    "SENSITIVITY_FAIL_SAFE",
    "KNOWN_SENSITIVITIES",
    "STATUS_PRECEDENCE",
    "UNREADABLE_STATUSES",
    "EntryUnreadableError",
    "MalformedEntry",
    "StoreMissingError",
    "FocusWindow",
    "RecalledEntry",
    "RecallReport",
    "Block",
    "Hunk",
    "SearchReport",
    "caveat_text",
    "scope_label",
    "unreadable_summary",
    "render_malformed",
    "extract_sections",
    "read_entry",
    "fold_sensitivity",
    "discarded_sensitivity",
    "sensitivity_label",
    "load_store",
    "visible_scope_set",
    "listing_order",
    "listing_page",
    "tokenize",
    "pair_strength",
    "score_unit",
    "entry_blocks",
    "search",
    "render_search",
    "search_json",
    "focus_paths_from_text",
    "focus_window",
    "select_featured",
    "short_heading",
    "listing_line",
    "recall",
    "render_text",
    "report_json",
    "main",
]

# 🔴 THE WORD `/analyze-service` USES, REUSED VERBATIM. Its brief says nuance and
# pointers are `from index` while location and config are `re-derived live`, and
# "Never present index recall as live observation". A reader that invented its
# own label would put two spellings of one provenance claim in front of the same
# agent, in the same session, for the same store.
RECALL_LABEL = "from index"

# The sections a printed BODY renders, RE-EXPORTED from the resolver — they are
# schema headings, so they belong with the rest of the on-disk shape and not in
# one of the modules that read it.
#
# The tuple itself stays HERE because it is this reader's DISPLAY CHOICE, not a
# fact about the store: the writer half reads the same entries and wants only
# `NUANCE_HEADING`. A shared constant would have made one module's display
# decision binding on the other.
#
# 🔴 `## What it is` USED TO BE EXCLUDED, AND THE EXCLUSION WAS WRONG. The stated
# reason was "one line of durable boilerplate a resuming session either already
# knows or can read in the file, and including it turns a recall block into a
# dump". Measured against the live store, both halves fail:
#
#   * it is not one line — 73 of 73 entries carry it, median 3 lines / 297 chars,
#     p90 8 lines, max 12;
#   * NO BRIEFING PATH printed it. Not `--ref`, not the digest, not
#     `service_recon`'s `index:` block. An agent briefed only on an entry could
#     not say what the service WAS, where it lived or what it owned, because the
#     one section that answers that was parsed by none of the three. `--search`
#     is the exception and always was — it surfaces every section — but it only
#     reaches an entry a query MATCHED, so nobody is briefed through it.
#
# The dump worry was real but aimed at the wrong surface: the multiplier lives on
# the INDEX ROWS (one per entry, 37 in the largest scope), and those are untouched
# — see `listing_line`, which still renders `COUNTED_HEADINGS` only. A BODY is
# printed once per `--ref`, once per digest, and `--limit N` times in `full` mode,
# which is already an opt-in dump of N whole entries; across the live store
# `## What it is` is 26 KB against `## Pointers`' 49 KB and `## Nuance`' 235 KB,
# i.e. the SMALLEST of the three, ~8.6% of what a full dump already prints.
#
# It is rendered FIRST because it is the orienting sentence: pointers and nuance
# are both about a thing the reader is assumed to have already identified.
# ⚠ `## Requirements` IS SURFACED BUT NOT COUNTED, and the split is the same one
# `COUNTED_HEADINGS` draws below. A body that prints what the subsystem was SAID
# to do is worth the bytes; an entry that has never carried the section is not
# MISSING it, the way an entry with no `## Nuance / work-history` is missing a
# section whose absence makes `0 nuance` a lie. Putting it in `COUNTED_HEADINGS`
# would raise `🔴 NO Requirements` on every entry in the store — a defect report
# about a section nobody has written yet.
#
# It renders LAST because it is the newest section and the orienting order —
# what it is, where to look, what happened — is what a reader already knows.
SURFACED_HEADINGS: tuple[str, ...] = (
    WHAT_HEADING,
    POINTERS_HEADING,
    NUANCE_HEADING,
    REQUIREMENTS_HEADING,
)

# 🔴 THE SET WHOSE ABSENCE MAKES A NUMBER WRONG — a strictly different question
# from "what does a body print", and the two are kept apart rather than merged.
#
# `missing_sections`, `is_bare`, the index row's `🔴 NO <heading>` badge and the
# caveat clause that explains that badge all key off THIS tuple. Their shared
# meaning is "the parser never reached the bullets, so `0 nuance` and a missing
# `OPEN` badge on that row are a PARSE FAILURE and not an empty entry" — a claim
# about counts. `## What it is` feeds no count and no badge, so widening this
# would put a heading with no numeric consequence beside two whose consequence is
# measured, and grow the one line printed for EVERY entry. An entry that lacks it
# is instead named under its own body, where the reader is already looking.
#
# ⚠ This is the tuple `entry_shape.SHAPE_HEADINGS` is pinned against by
# the writer half's own suite — the validator checks what the counts depend on.
COUNTED_HEADINGS: tuple[str, ...] = (POINTERS_HEADING, NUANCE_HEADING)

# A cap, not a filter — see the module docstring on selection. Truncation is
# always PRINTED. 12 is chosen as "more than any scope currently holds per repo
# that is not the infra repo, and small enough to read at the top of a session";
# it is a display bound with an escape hatch (`--limit`), not a safety bound, so
# unlike `MAX_TRANSCRIPT_AGE_SECONDS` in the writer it is deliberately overridable.
#
# ⚠ IT IS A FULL-MODE CONCEPT ONLY. `digest` prints exactly one body by design and
# `list` prints none, so neither applies a cap and neither can truncate; `limit`
# is still carried on the report (and in the JSON) so a caller can see what a
# `--limit` run WOULD have used, but it changes nothing outside `full`. That is
# the same shape `--ref` already had: a narrowing ignores the display cap.
DEFAULT_ENTRY_LIMIT = 12

# The three display modes. `ref` is NOT one of them: a `--ref` run is a narrowing
# to a single named entry and prints that entry in full whatever the mode says,
# exactly as it did before the digest existed.
#
#   "digest"  the caveat + the FULL index (every entry, one line) + ONE body.
#             The default, and what `/resume` step 4 runs.
#   "list"    the caveat + the FULL index, and no body at all.
#   "full"    the pre-digest behaviour, byte-for-byte: up to `limit` bodies, with
#             a LOUD truncation notice. Selected by passing `--limit`.
RECALL_MODES: tuple[str, ...] = ("digest", "list", "full")
DEFAULT_MODE = "digest"

# 🔴 THE INDEX IS CAPPED, AND THE CAP IS LOUD. Every line is ~60 B, so an index
# that grows with the store forever is a per-session cost with no ceiling; 100
# lines is ~6 KB, which is still smaller than the ONE featured body it sits above
# on a large entry. Past the cap the remainder is COUNTED and `--page N` reaches
# it — nothing is dropped and nothing is silent.
#
# ⚠ It is a LISTING cap and not a selection rule: `total_in_scope` still counts
# every entry, and the truncation notice names both the count and the flag. The
# module docstring argues why the page order is newest-first by mtime rather than
# the canonical ref order that `full` mode's bodies keep.
LISTING_PAGE_SIZE = 100

# --- Search tuning. Every number here is gated by `TestSearchFixtureCorpus`, ----
# which scores a labelled query→expected-hunk set; none of them is taste.
#
# The threshold a hunk must clear. Scores are a COVERAGE MEAN over the query's
# tokens (see `score_unit`), so 0.60 means: on a two-token query both tokens must
# land (one alone scores 0.50), while on a three-token query two strong tokens
# (0.67) are enough. That asymmetry is deliberate — a longer query is a
# description, and demanding every word of a description is how a search returns
# a confident zero.
DEFAULT_SEARCH_THRESHOLD = 0.60

# Below this, `difflib`'s ratio is not evidence of a typo. 0.82 and not 0.80
# because 0.80 is exactly where a one-character SUBSTITUTION in a five-letter word
# lands — and so does the ordinary English pair `probe`/`prone`, which is not a
# typo of anything. Every typo class in the fixture set clears 0.88.
FUZZY_FLOOR = 0.82

# 🔴 ONE length rule, not three. A token shorter than this must match EXACTLY:
# no prefix rule, no substring rule, no fuzzy rule. Short tokens are where every
# inexact rule turns into noise (`pod` prefixes `podman`, `pgo`, `podinfo`), and a
# reader cannot tell a noisy hit from a real one once it is on the screen.
MIN_INEXACT_LEN = 4

# What an inexact match is WORTH, so a weak hit prints as visibly weak. Ordered,
# and each strictly below 1.0 — an exact token match must always outrank them.
PREFIX_STRENGTH = 0.92
SUBSTRING_STRENGTH = 0.85

# A display cap on HUNKS, with the same contract as `--limit`: printed when it
# bites, never silent. 10 because the point of searching is to read LESS than the
# digest, and it is measured: a one-word query for a heavily-mentioned subsystem
# returns 22 hunks on the real store, which at 20 shown costs ~9.4 KB — twice the
# 4.9 KB digest it was supposed to be cheaper than. At 10 it is ~4.6 KB, and the
# other 12 are counted on the screen with the flag that shows them.
DEFAULT_MAX_HITS = 10

# `context=CONTEXT_BULLET` means "the enclosing bullet/block", which is the
# DEFAULT and not merely one option — see `--help`. A sentinel rather than `None`
# so a report always carries a concrete value into JSON.
CONTEXT_BULLET = -1

# Why a hunk is on the screen. Two values sharing no spelling, printed per hunk:
#   "line"        this block itself cleared the threshold.
#   "entry-name"  the ENTRY's ref/aliases cleared it and no block did, so the
#                 entry's best block is shown as the worked example. Without this
#                 value a name-only hit is indistinguishable from a no-match.
HUNK_BASES: tuple[str, ...] = ("line", "entry-name")

# 🔴 ONE path is enough to feature an entry, where the WRITER needs two
# (`DEFAULT_MIN_PATHS = 2`). The asymmetry is deliberate and is about what the
# two are deciding. The writer is proposing a DURABLE journal bullet against a
# subsystem, so a single incidental path must not be enough. The reader is only
# choosing which of N already-listed entries to print first, it PRINTS the basis
# it used, and every other entry is one line above and one `--ref` away — so the
# cost of a weak signal is a slightly worse first pick, not a wrong record.
FOCUS_MIN_PATHS = 1

# Where the path window comes from, in the order the resume flow's own resolver
# resolves a handoff — lowercase family first, the uppercase `*HANDOFF*.md`
# family (delta-manager's `SESSION-HANDOFF.md`) only as a fallback. Same repo,
# same step, same doc: `/resume` has just read this file at step 2, so it is the
# session's own statement of what is being worked on, and reading it costs one
# file read with no git, no network and no subprocess.
HANDOFF_GLOBS: tuple[str, ...] = ("claudedocs/handoff-*.md", "claudedocs/*HANDOFF*.md")

# Path-shaped tokens are taken from BACKTICKED SPANS ONLY. Handoff docs are prose
# and the convention in this fleet is that a real path is code-quoted; harvesting
# bare prose would mint tokens out of ordinary English (the resume flow's own resolver learned
# this the hard way with branch tokens — see its comment on the fabricated
# "referenced by handoff no longer exists" line). The failure direction here is
# benign either way: a token that names no entry is dropped by the resolver, and
# a token that names the wrong one produces a differently-featured entry whose
# basis is printed on the same screen.
_BACKTICKED = re.compile(r"`([^`\n]+)`")
_PATH_TOKEN_STRIP = "`,;:()[]{}<>\"'*_"

# 🔴 FAIL-SAFE, from `analyze-service/SKILL.md`: "`sensitivity:` — fail-safe:
# absent means sensitive… absent or unrecognized ⇒ `client-confidential`, never
# public". Surfaced per entry because this reader's whole job is to put curated,
# client-identifying content in front of an agent that may be one paste away from
# a PUBLIC repo, and the marker is the only thing that says so.
SENSITIVITY_FAIL_SAFE = "client-confidential"
KNOWN_SENSITIVITIES: tuple[str, ...] = ("client-confidential", "personal", "public")

# Stated once so the renderers, the JSON and the tests read the SAME rule.
# Precedence, and why each outranks the next:
#   1. scope-absent     — nothing about the store's contents can change this answer.
#   2. scope-unreadable — the scope holds files and NOT ONE of them indexed. It
#                         outranks `scope-empty` and every ref outcome because it
#                         is the one case where the tool's answer is about ITSELF:
#                         a `--ref` into a scope nothing could be read from is
#                         `ref-absent` only in the sense that nothing is present
#                         to resolve against, and reporting it that way ("nothing
#                         recorded under that name yet") would be a lie about the
#                         store rather than a fact about the ref.
#   3. scope-empty      — the scope exists; there is simply nothing in it.
#   4. ref-ambiguous    — a ref was given and names more than one entry: never pick.
#   5. ref-absent       — a ref was given and names none.
#   5b. ref-to-absent   — a `--ref-to` was given and NO entry in the caller's
#                         reachable scopes carries that ref. 🔴 NOT `scope-empty`
#                         and NOT `ref-absent`, and collapsing it into either would
#                         print something false: `scope-empty` claims the DIRECTORY
#                         holds nothing, which is wrong about a full scope none of
#                         whose entries carries the ref; `ref-absent` is about an
#                         ENTRY NAME and sends the reader looking for a file. This
#                         one is about a POINTER. It is listed after `ref-absent`
#                         because the FILTER runs first: a `--ref-to` that matches
#                         nothing returns before any `--ref` is resolved, so
#                         `ref-to-absent` outranks the two ref outcomes in practice
#                         while sharing their "a narrowing was given" character.
#   5c. tag-absent      — a `--tag` was given and NO entry in the caller's reachable
#                         scopes carries it. 🔴 NOT `scope-empty`, which is
#                         the status a tag filter would otherwise fall into: that branch
#                         is downstream of every narrowing and its body opens "NOTHING
#                         RECORDED YET — `<scope>/` exists but holds no entries", a claim
#                         about the DIRECTORY that is false of a full scope none of whose
#                         entries carry the tag. Listed after `ref-to-absent` because that
#                         is the order the two filters run in, and reached the same TWO
#                         ways `ref-to-absent` is — the filter's own zero, and "the `--ref`
#                         operand is not among the entries that did".
#   6. recalled         — something was surfaced.
# `StoreMissingError` / `EntryUnreadableError` are NOT in this tuple: they raise.
# A status constant no code path could emit would be a declaration with nothing
# behind it. `MalformedEntryError` used to be in that list too and no longer is —
# for this reader it is a ROW (`MalformedEntry`), not a raise.
#
# The last three belong to `search`, which shares this vocabulary rather than
# minting a second one — a caller must be able to switch on ONE status field:
#   7. search-hit        at least one hunk cleared the threshold.
#   8. search-no-match   nothing did. NOT spelled `*-empty`: an empty scope and a
#                        query that matched nothing in a full scope are different
#                        facts, and two statuses that share a word get read as one.
#   9. search-unreadable the searched scopes held files, NONE could be indexed,
#                        and no readable entry was removed by a `--ref-to` OR a
#                        `--tag` filter either — so the query never ran against
#                        anything. Same argument as `scope-unreadable`, and the same
#                        reason it must not collapse into `search-no-match`: a zero
#                        from a scan that walked nothing is not a zero. 🔴 THE LAST
#                        TWO TERMS ARE NOT DECORATION: without the first, a `--ref-to`
#                        that removed every readable entry answered this status over a
#                        scope whose entries were all indexed fine, printing "NOT ONE
#                        of them could be indexed" about files that were — MEASURED,
#                        not hypothetical. The `--tag` term is the same rule one filter
#                        later, and the rule is: EVERY filter placed upstream of the
#                        searched counter falsifies this condition unless its own skip
#                        count is subtracted back out.
STATUS_PRECEDENCE: tuple[str, ...] = (
    "scope-absent",
    "scope-unreadable",
    "scope-empty",
    "ref-ambiguous",
    "ref-absent",
    "ref-to-absent",
    "tag-absent",
    "recalled",
    "search-hit",
    "search-no-match",
    "search-unreadable",
)

# 🔴 THE ONE PLACE THE EXIT CODE IS DECIDED, for both report types. `main()`
# switches on membership here rather than on either status by name, so a third
# "nothing could be read" outcome cannot be added and silently exit 0.
#
# WHY THESE TWO AND NOTHING ELSE — the rule is CONTENT SERVED, not "was anything
# wrong". `/resume` step 4's contract is "if it exits non-zero, print the stderr
# line verbatim, note that recall was unavailable, and continue", so a non-zero
# throws away every entry the run DID surface. A scope with 2 good entries and 1
# malformed therefore exits 0 with a loud in-band MALFORMED block: recall was
# available, and it was also honest about what it could not read. Only when
# nothing readable exists at all is "recall was unavailable" the truth.
UNREADABLE_STATUSES: tuple[str, ...] = ("scope-unreadable", "search-unreadable")


#: Badge kinds whose EXPLANATION is emitted only when that badge is on screen.
#: `OPEN` is deliberately NOT here — see `caveat_text`.
BADGE_NEAR_MISS = "near-miss"
BADGE_UNVERIFIABLE = "unverifiable"
BADGE_MISSING_HEADING = "missing-heading"
#: ONE kind for the PAIR: both badges come from the same section, so a reader seeing
#: either needs the same sentence and two kinds would emit it twice.
BADGE_REQUIREMENTS = "requirements"


def badges_present(entries: "Sequence[RecalledEntry]") -> frozenset[str]:
    """Which conditional badge kinds this report will actually render.

    🔴 READ OFF THE ENTRIES THE REPORT IS ABOUT, never off the store. A caveat
    that described the store rather than this output would explain a badge the
    reader cannot see, which is the whole defect this exists to fix.
    """
    kinds: set[str] = set()
    for e in entries:
        if getattr(e, "near_miss_count", 0):
            kinds.add(BADGE_NEAR_MISS)
        if getattr(e, "unverifiable_count", 0):
            kinds.add(BADGE_UNVERIFIABLE)
        if getattr(e, "missing_sections", ()):
            kinds.add(BADGE_MISSING_HEADING)
        if e.requirements_open or e.requirements_met:
            kinds.add(BADGE_REQUIREMENTS)
    return frozenset(kinds)


def _cardinal_badge_lead(n: int) -> str:
    """`"<N> further badge(s) say(s)"` — DERIVED, never a ternary listing the counts
    somebody thought of.

    🔴 IT REPLACED A CHAIN THAT TOPPED OUT AT THREE AND DEFAULTED TO THE SINGULAR, so the
    first reader to see four badges would have been told there was one. Adding the
    requirements clause made a fourth reachable. `claude/RULES.md`: "a count in prose is a
    claim" — and this is the second stale hand-written total an audit round has found in
    this repo. The fix is the FORM: nothing here needs editing when a fifth clause arrives.

    ⚠ Past five it prints the NUMERAL rather than guessing a word. An English cardinal
    table is exactly the list that goes stale silently; a digit cannot be wrong.
    """
    words = {1: "One", 2: "Two", 3: "Three", 4: "Four", 5: "Five"}
    if n == 1:
        return "One further badge says"
    if n in words:
        return f"{words[n]} further badges say"
    return f"{n} further badges say"


def caveat_text(scope: str, badges: "frozenset[str] | None" = None) -> str:
    """What every window this module opens can and cannot see. ONE spelling.

    🔴 A MODULE-LEVEL FUNCTION rather than a property on one report, because
    there are now TWO report types and a caveat spelled twice is wrong in one of
    them. `/analyze-service` words the provenance as `from index`; this reuses
    that exact label.

    🔴 THE BADGE EXPLANATIONS ARE CONDITIONAL; EVERY WARNING ABOUT AN ABSENCE IS
    NOT. Measured on the first real session to use this flow: the caveat is 1,513
    chars (~378 tokens) and is paid PER CALL, so a targeted `--ref` lookup — the
    cheap operation this design encourages — spent 27% of its output explaining
    `NEAR-MISS`, `UNVERIFIABLE` and `NO <heading>` when its output contained NONE
    of them. The badges themselves already render conditionally so the common row
    stays byte-identical; the prose explaining them did not, which made the text
    grow with every badge added while the reader's need for it did not.

    🔴 `OPEN` STAYS UNCONDITIONAL, and the asymmetry is the point. Its clause is
    not "here is what this badge means" — it ends "the absence of that marker
    means nothing was declared, NOT that nothing is open." That is a warning
    about a MISSING badge, so gating it on a badge being present would delete it
    in exactly the case it was written for. Same test for anything added later:
    if the sentence is only true when the reader can see the badge, gate it; if
    it warns about what the reader CANNOT see, it is unconditional.

    `badges=None` means "caller did not compute a set" and yields the full text —
    fail-safe toward saying MORE, never less.
    """
    show_all = badges is None
    if badges is None:
        badges = frozenset()

    optional = ""
    clauses = []
    if show_all or BADGE_NEAR_MISS in badges:
        clauses.append(
            f"`🔴 N NEAR-MISS` — N `{NUANCE_HEADING}` bullets TRIED to write a marker "
            "and missed the grammar, so they declare nothing and `N OPEN` is short by up to N"
        )
    if show_all or BADGE_UNVERIFIABLE in badges:
        clauses.append(
            f"`⚠ N UNVERIFIABLE` — N `{NUANCE_HEADING}` `RESOLVED:` bullets name no sha, "
            "so the closure cannot be checked"
        )
    if show_all or BADGE_MISSING_HEADING in badges:
        clauses.append(
            "`🔴 NO <heading>` — that heading is absent or renamed, so `N nuance` "
            "and every openness count on that row are 0 BY PARSE FAILURE and not "
            "by measurement, and the entry's content is on disk but invisible to "
            "this read"
        )
    if show_all or BADGE_REQUIREMENTS in badges:
        clauses.append(
            f"`🔴 N REQ OPEN` / `✅ N REQ MET` — N `{REQUIREMENTS_HEADING}` bullets "
            "declaring each state. They do NOT sum to that section's bullet count (an "
            "unmarked bullet is neither), the `NEAR-MISS` and `UNVERIFIABLE` counts to "
            "their left do NOT cover this section, and `MET` folds a sha-checked closure "
            "together with a sha-less one"
        )
    if clauses:
        lead = _cardinal_badge_lead(len(clauses))
        optional = f" {lead} the row's own numbers cannot be trusted: " + "; ".join(clauses) + "."

    return (
        f"{RECALL_LABEL} — RECALL, NOT LIVE OBSERVATION. These are notes curated by "
        f"PAST sessions in the local store under `{scope}`. Nothing here was "
        f"re-derived just now, nothing was matched against anything THIS session has "
        f"done, and an entry is exactly as fresh as the last time someone pruned it "
        f"(prune-on-resolve is manual), so a bullet may describe a gotcha already "
        f"fixed. This window CANNOT see: live state of any kind, any repo whose scope "
        f"has no directory in THIS HOST's store, and any work neither `/analyze-service` nor "
        f"`/handoff` ever recorded. Treat every line as a POINTER to verify, never as "
        f"a current reading. This window read a LOCAL CACHE of the hosted store, not "
        f"the store itself, so it is only as complete as the last `cairn sync` on THIS "
        f"machine: an entry written on the OTHER machine that has not synced here yet "
        f"is invisible, and an absence below is an absence AS OF THAT SYNC, not a fact "
        f"about the store. "
        f"`🔴 N OPEN` on an index row means N bullets DECLARE "
        f"unfinished business — re-check each against the repo, because a remedy that "
        f"has since landed reads exactly like one that has not; the absence of that "
        f"marker means nothing was declared, NOT that nothing is open."
        f"{optional} Sensitivity is "
        f"marked per entry; absent means `{SENSITIVITY_FAIL_SAFE}` — never copy an "
        f"entry's content into a public repo."
    )


def scope_label(scopes: Sequence[str]) -> str:
    """`a/, b/` — how a set of scopes is named in prose. ONE spelling.

    The caveat, the search header and the unreadable summary all need it, and the
    first two used to build it inline in two slightly different ways. A label
    spelled at N sites is wrong at N−1 of them, and this one is inside a sentence
    that says what the tool could not see.
    """
    return "/, ".join(scopes) + "/" if scopes else "(no scope)"


def unreadable_summary(label: str, malformed: Sequence[MalformedEntry]) -> str:
    """The ONE sentence that says "there is content here and none of it could be read".

    🔴 IT MUST SHARE NO PHRASE WITH THE EMPTY CASE. `render_text`'s `scope-empty`
    branch opens `NOTHING RECORDED YET`; this opens `NOTHING COULD BE READ`, and
    the two are different facts with opposite next actions — carry on versus fix
    the store. `/resume` reports `scope-empty` as an ordinary non-finding, so a
    shared opening word is all it would take for a broken store to be read as an
    empty one.

    Shared by both report types because both can reach the condition and a
    sentence written twice is a sentence that will differ.
    """
    n = len(malformed)
    return (
        f"NOTHING COULD BE READ — `{label}` holds {n} entry file"
        f"{'' if n == 1 else 's'} and NOT ONE of them could be indexed. This is NOT an "
        f"empty scope and NOT 'nothing recorded yet': there is content here and the tool "
        f"cannot see it. Every reason is listed above; fix the file(s) and re-run."
    )


def render_malformed(
    malformed: Sequence[MalformedEntry], elsewhere: Sequence[MalformedEntry], label: str
) -> list[str]:
    """The MALFORMED block — per entry, named, with its reason. Empty when clean.

    🔴 ONE RENDERER, PRINTED ON EVERY STATUS BY BOTH SURFACES, IMMEDIATELY AFTER
    THE CAVEAT. Not a footer and not a branch: a reject that renders only on the
    paths somebody remembered is a reject that will be missed on the path they
    did not, and `scope-absent` — the most common status in most repos, and a
    statement about THIS HOST's store only — is exactly where a store-wide defect
    would otherwise never be mentioned.

    `elsewhere` is a COUNT with its scopes named, never full rows. A reader is
    scope-scoped, so a broken entry in a scope nobody recalls today is invisible
    until someone does; naming the scopes makes it actionable without putting
    another scope's filenames (which are client-identifying) on this screen.
    """
    out: list[str] = []
    if malformed:
        n = len(malformed)
        out.append("")
        out.append(
            f"🔴 MALFORMED — {n} entry file{'' if n == 1 else 's'} in `{label}` could NOT be "
            f"indexed and {'is' if n == 1 else 'are'} therefore absent from EVERYTHING below "
            f"(the index, --ref, --search). Not dropped, not hidden: listed here, once each."
        )
        for m in malformed:
            out.append(f"  {m.line}")
        out.append(
            "  (A STORE DEFECT, not an absence of content. Front matter is parsed LINE BY "
            "LINE, so the usual cause is a value wrapped across two physical lines — an "
            "`aliases: [...]` list in particular must be on ONE line. Check a scope with "
            "`cairn validate --scope <scope>`, which names each file that fails to parse.)"
        )
    if elsewhere:
        by_scope: dict[str, int] = {}
        for m in elsewhere:
            by_scope[m.scope or "(no scope)"] = by_scope.get(m.scope or "(no scope)", 0) + 1
        where = ", ".join(f"{s} ({n})" for s, n in sorted(by_scope.items()))
        k = len(elsewhere)
        if not malformed:
            out.append("")
        out.append(
            f"  (+{k} further malformed entr{'y' if k == 1 else 'ies'} in OTHER scopes of "
            f"this store, not shown: {where}. Nothing on this screen is affected by them; "
            f"they are named so a defect in a scope nobody recalls today is still visible.)"
        )
    return out


# --- Errors --------------------------------------------------------------------
#
# ⚠ There is deliberately NO `RecallError` base any more. `EntryUnreadableError`
# was its only subclass and moved to `subsystem_resolver` when the writer half
# needed to raise the SAME condition on the SAME files; what was left was a base
# class with nothing under it — a declaration with no code path behind it, which
# is the shape this codebase argues against everywhere else. `main()` catches
# `(TouchError, ResolverError)`, which between them cover every error this module
# can now emit.


# --- Section extraction --------------------------------------------------------


def extract_sections(text: str, headings: Sequence[str] = SURFACED_HEADINGS) -> dict[str, str]:
    """This reader's default-binding shim over the ONE section parser.

    🔴 THE PARSER ITSELF LIVES IN `subsystem_resolver`, which is where the fence
    handling, the present-but-empty tracking and their mutation kills now live
    too. It moved there — unchanged — when the writer half needed the same
    extraction to show a `/handoff` what an entry ALREADY says before proposing
    an append. `subsystem_recall` imports the writer half, so the writer cannot
    import the reader back without closing a cycle, and a second copy in the
    writer would be a parser free to drift from the one measured against the real
    corpus.

    All this adds is the DEFAULT: which sections *this* reader surfaces. That is
    a display decision and stays with the display.
    """
    return _extract_sections(text, headings)


def fold_sensitivity(raw: object) -> str:
    """Apply the schema's fail-safe: absent OR unrecognized ⇒ client-confidential.

    🔴 `public` is "a deliberate operator claim a recon run may never infer", so
    every path that is not an exact, known, operator-written value folds to the
    sensitive end. This is the FIRST executable spelling of that rule — the
    writer only ever emits the literal at the fail-safe value and never reads one
    back — so it is not a second implementation of an existing predicate.
    """
    if isinstance(raw, str):
        v = raw.strip().lower()
        if v in KNOWN_SENSITIVITIES:
            return v
    return SENSITIVITY_FAIL_SAFE


def discarded_sensitivity(raw: object) -> str | None:
    """The marker a file DECLARED and `fold_sensitivity` overrode, if any.

    🔴 THE FOLD IS NEVER WEAKENED — this only makes it VISIBLE. `sensitivity:`
    is the one field this module treats as safety-critical, and there is a real
    difference between the two inputs that both render as `client-confidential`:

        (absent)              nobody said anything. The ordinary case, and the
                              fail-safe default is the whole answer.
        `sensitivity: internal`  somebody WROTE a value and the tool overrode it,
                              because `internal` is not one of the schema's three.

    Silently rewriting the second reads as "the file says client-confidential",
    which it does not — and the author who typed `internal` gets no signal that
    the schema does not know the word. So an overridden declaration is printed
    beside the effective value; an absent one prints nothing.

    DERIVED from `fold_sensitivity` rather than re-testing membership, so the two
    cannot drift into disagreeing about which values are known.
    """
    if not isinstance(raw, str):
        return None
    written = raw.strip()
    if not written:
        return None
    return None if fold_sensitivity(raw) == written.lower() else written


# --- The recalled entry --------------------------------------------------------


@dataclass(frozen=True)
class RecalledEntry:
    """One entry's surfaced sections, plus what was NOT there."""

    ref: str
    filename: str
    sensitivity: str
    sections: Mapping[str, str] = field(default_factory=dict)

    declared_sensitivity: str | None = None
    """A `sensitivity:` the file wrote that the schema does not know, which
    `fold_sensitivity` overrode — see `discarded_sensitivity`. `None` when the
    marker was absent or was honoured. Printed beside the effective value so an
    override is never silent."""

    bullet_count: int = 0
    """Top-level `## Nuance / work-history` bullets, via the resolver's own
    `parse_journal_bullets`. The index line's SIZE SIGNAL: it is what a reader
    wants in order to decide whether an entry is worth a `--ref`, and it is the
    unit the store's own prune-on-resolve discipline is denominated in. A byte
    count would have been cheaper and would have measured markdown, not history.
    """

    open_count: int = 0
    """Bullets DECLARING `OPEN:` — unfinished business the writer marked.

    On the index line this is the one field that changes what a reader should DO,
    which is why it earns a place beside the size signal: an entry carrying an
    open action may be describing a remedy that has since landed, and reading it
    as current is the `forgejo` failure (proposed a fix at 15:00:18 that shipped
    at 15:02:21, served as outstanding for 22 days).

    🔴 A ZERO HERE IS NOT "NOTHING IS OPEN". The marker is opt-in and every bullet
    written before it existed carries none, so zero means "nothing was declared".
    The digest's caveat says so; this docstring exists so a future caller cannot
    quietly promote the field to a completeness claim.
    """

    near_miss_count: int = 0
    """Bullets that TRIED to write an openness marker and missed the grammar.

    🔴 THE POPULATION MOST LIKELY TO HOLD A STALE OPEN ACTION, and until this
    field existed it was byte-identical to "no marker" on the read surface: the
    writer's `RESOLVED <sha> (<repo>):` or `**OPEN:**` declared nothing, the
    badge simply did not render, and the vanishing badge LOOKS like success.

    Measured over the live store — 53 entries, 323 top-level
    nuance bullets: **8 declare `OPEN:` and parse, 11 declare `RESOLVED <sha>:`,
    and 2 attempted a marker and missed** (one `OPEN`-shaped, one
    `RESOLVED`-shaped). The advisory that reported those 2 lived only in
    the writer's own validate pass, which `/resume` never runs.

    ⚠ The proposal this closes states "2 of 10 textual `OPEN:` markers do not
    parse". The near-miss count of 2 reproduces exactly; the denominator does
    not — a raw `grep -o 'OPEN:'` over the store returns **11**, not 10, and not
    every occurrence leads a top-level bullet. The rate is quoted here as the
    two populations rather than as a ratio, because the ratio's denominator is
    the part that moved.

    Counted from `JournalBullet.openness_population`, never from the raw
    `near_miss_marker` predicate, so this surface and `--validate` can never
    disagree about which population a bullet belongs to.
    """

    requirements_open: int = 0
    """`## Requirements` bullets DECLARING open, in the journal's own vocabulary."""

    requirements_met: int = 0
    """`## Requirements` bullets declaring met.

    🔴 `requirements_open + requirements_met` DOES NOT EQUAL the section's bullet
    count, and nothing should assume it does. A requirement bullet carrying no
    marker is neither — a sentence somebody wrote under the heading without
    declaring a state — so `open + met < len(bullets)` is an ordinary reading.
    Deriving either from the other, or either from the total, is how a badge
    starts claiming work nobody declared.

    ⚠ MET INCLUDES A SHA-LESS `RESOLVED:`. The writer closed it; only the CHECK
    is missing. Counting it as not-met would reopen an action somebody finished."""

    unverifiable_count: int = 0
    """`RESOLVED:` bullets naming no sha — closed, but the closure is unprovable.

    ⚠ Advisory, not a defect: closing an action is the point, and a sha-less
    `RESOLVED` is a real closure that simply cannot be checked with
    `git cat-file -e`. It rides the same row as `near_miss_count` because both
    are "the marker did not fully land" and a reader who sees one wants the
    other. Measured: **0** across all 53 live entries, so this badge
    does not fire on the store today — it is here because a sha-less `RESOLVED`
    is one hurried write away, not because the corpus is full of them.
    """

    mtime: float = 0.0
    """The entry file's mtime. Used ONLY as the featured-entry fallback, and
    deliberately NOT rendered or emitted in JSON — `render_text` must produce
    identical bytes for an unchanged store, and a printed timestamp would make a
    diff of two runs show movement that is not there."""

    missing_sections: tuple[str, ...] = ()
    """Requested headings this entry does not carry — REPORTED, never silent.

    An entry with pointers and no work-history is ordinary (nothing has been
    journaled against it yet). An entry with NEITHER is also ordinary — the
    writer's own `new_entry_template` ships a stub. What is not ordinary is a
    reader that shows nothing and lets a caller conclude the entry is empty when
    the extractor missed, so the two are told apart in the output.

    🔴 IT REACHES THE INDEX ROW, not only a printed body. It was computed here
    and rendered ONLY under an entry the digest printed in full — and the digest
    prints exactly ONE body out of N, so for every other entry the field existed
    and was discarded. Measured differential control (two synthetic entries
    differing in NOTHING but the nuance heading): the renamed one reported
    `0 nuance`, lost its `🔴 1 OPEN` badge entirely, and `--validate` called it
    `OK` at exit 0 — rendering byte-identical to a well-formed entry with an
    empty work-history. Heading matching is exact-string at column 0, so a
    rename, a trailing colon or an indent all land here.
    """

    tags: tuple[str, ...] = ()
    """The entry's `tags:`, already folded, deduped and sorted by the loader.

    ⚠ NO FLATTENING, WHERE `tasks` NEEDS ONE, AND THE DIFFERENCE IS THE TYPE RATHER THAN A
    SHORTCUT. `SubsystemEntry.tasks` is a tuple of `TaskRef` and has to be reduced to its
    printed spelling; `SubsystemEntry.tags` is already a tuple of `str` in exactly the form
    every surface renders, because a tag's canonical form IS its folded form and there is no
    raw spelling to choose between.
    """

    tasks: tuple[str, ...] = ()
    """The entry's refs as written (`<system>:<id>`), in file order.

    ⚠ THE FIELD NAME READS `tasks` WHILE THE FRONT-MATTER KEY IS `refs:`, AND THAT
    IS DEFERRED RATHER THAN OVERLOOKED. `tasks:`/`task:` are still accepted
    spellings on the way in — permanently, by operator decision — so the name is
    not stale about the schema, only narrower than it: the key carries repos, PRs,
    docs and dashboards, not only work-tracker items. Renaming the field is a
    mechanical change that would have to move `SubsystemEntry.tasks`, the JSON
    payload's key and `internal/report.RecalledEntry.Tasks` together.

    🔴 THE RENDERED TEXT SURFACE HAS FULLY MOVED, SO WHAT IS DEFERRED HERE IS NOW
    A NAME AND A JSON KEY, NOT A LABEL. The INDEX BADGE reads `🔗 N ref(s)` (see
    `listing_line`) and the BODY LABEL reads `refs: ` (see `render_text`); both
    re-based the goldens, the reader fixture and the parity byte diffs, the second
    on an operator ruling. Two spellings of `task` remain and they are NOT one
    deferral: this FIELD NAME, which no reader sees and which moving is purely
    mechanical; and `render_json`'s payload key `"tasks"`, which a consumer reads.
    ⚠ THE JSON KEY IS THE ONE WITH A COST — renaming it breaks a consumer's `[]`
    presence check silently, and it has no Go counterpart, so it is not held equal
    by `tests/parity/` either. That asymmetry is why the rendered words moved
    first and these did not move with them.

    Carried from `SubsystemEntry.tasks` rather than re-parsed from the front
    matter here: the loader already validated them, and a second parse at the
    read surface is the duplicated predicate that lets a reader show refs the
    validator rejected.

    🔴 STRINGS, NOT `TaskRef`s, ON PURPOSE. This dataclass is what the JSON
    payload is built from, and a `TaskRef` would have to be flattened at the
    boundary anyway — flattening once, here, keeps the text row and the JSON row
    quoting the same bytes. The structured form stays available on the
    `SubsystemEntry` for anything that needs the halves.

    Measured before this field existed: **0 of 120** entries in the
    live store carry a `tasks:` key, so every index row renders byte-identical
    to what it rendered before. That is the same conditionality the badges above
    rely on, and it is what makes this additive rather than a reflow of the
    index.
    """

    @property
    def is_bare(self) -> bool:
        """True when neither COUNTED section had any content.

        🔴 DELIBERATELY NOT `any(self.sections.values())`. `sections` now also
        carries `## What it is`, which the writer's own template pre-fills with a
        placeholder — so reading every value would make a freshly created stub
        report as filled-in and delete the "exists but has not been filled in"
        notice in exactly the case it was written for.
        """
        return not any(self.sections.get(h) for h in COUNTED_HEADINGS)


def read_entry(store_root: str | Path, entry: SubsystemEntry) -> RecalledEntry:
    """Read ONE entry's surfaced sections. READ-ONLY.

    The file is located from the loader's own `scope` + `filename`, never from a
    path reconstructed out of the ref — `<slug>.<kind>.md` and `<slug>.md` are
    different files and only the loader knows which one this entry came from.
    """
    path = Path(store_root) / entry.scope / entry.filename
    try:
        text = path.read_text(encoding="utf-8", errors="replace")
    except OSError as exc:
        raise EntryUnreadableError(
            f"index entry unreadable: {path} ({type(exc).__name__}: {exc}) — the store "
            f"was not fully read, so this report is INCOMPLETE; nothing was written"
        ) from exc
    sections = extract_sections(text)
    fm = parse_front_matter(text)
    try:
        mtime = path.stat().st_mtime
    except OSError:  # pragma: no cover - the read above already succeeded
        mtime = 0.0
    bullets = parse_journal_bullets(sections.get(NUANCE_HEADING, ""))
    # 🔴 ONE PASS, ONE PREDICATE. Every count on the index row is read off
    # `openness_population` — the resolver's single source of the precedence
    # order — rather than off `is_open` / `near_miss_marker` / `resolved_by`
    # separately. A delta audit already caught two surfaces disagreeing about
    # one bullet because each decided membership for itself; the index row is
    # now the third consumer, and it branches on the same thing `--validate`
    # does, so the two can never report different populations for one file.
    populations = Counter(b.openness_population for b in bullets)
    # 🔴 FROM THE SECTION BODY, WHICH IS THE WHOLE BOUNDARY GUARD. `sections` is
    # keyed by heading, so a bullet under `## Nuance / work-history` cannot reach
    # this lookup — the two populations are told apart by which section they sit
    # in and by nothing else. An absent heading yields "" here, which parses to no
    # requirements: the same answer as a present-but-empty section, and correct for
    # both, since neither states a requirement.
    requirements = parse_requirements(sections.get(REQUIREMENTS_HEADING, ""))
    return RecalledEntry(
        ref=entry.ref,
        filename=entry.filename,
        sensitivity=fold_sensitivity(fm.get("sensitivity")),
        declared_sensitivity=discarded_sensitivity(fm.get("sensitivity")),
        sections=sections,
        bullet_count=len(bullets),
        open_count=populations["open"],
        near_miss_count=populations["near-miss"],
        unverifiable_count=populations["unverifiable"],
        requirements_open=sum(1 for r in requirements if r.is_open),
        requirements_met=sum(1 for r in requirements if r.is_met),
        mtime=mtime,
        missing_sections=tuple(h for h in COUNTED_HEADINGS if h not in sections),
        tasks=tuple(str(t) for t in entry.tasks),
        tags=entry.tags,
    )


# --- The focus window ----------------------------------------------------------
#
# 🔴 THIS IS THE ONLY THING IN THE MODULE THAT LOOKS OUTSIDE THE STORE, and it
# reads exactly one file. It runs no git, no `gh` and no subprocess — the writer's
# path sources do all three, and importing that cost into `/resume` step 4 is the
# thing this whole change exists to avoid.


@dataclass(frozen=True)
class FocusWindow:
    """Repo-relative paths standing in for "what is being worked on", plus where
    they came from. `source` is `None` exactly when `paths` is empty, and the
    renderer says which of the two selectors fired either way — an unattributed
    pick is the failure this dataclass exists to prevent."""

    paths: tuple[str, ...] = ()
    source: str | None = None


def _is_repo_relative(token: str) -> bool:
    """Superset of `subsystem_resolver._validate_path`'s rejections, plus shape.

    Kept a hair STRICTER than the resolver on purpose: anything this lets
    through is handed to `associate_paths`, which raises `InvalidPathError` on a
    path it considers malformed. A reader that raised because a handoff doc
    quoted a URL would be a `/resume` step that fails on ordinary prose.
    """
    if not token or "/" not in token:
        return False
    if any(c.isspace() for c in token):
        return False
    if token[0] in "/~-":  # absolute, home-relative, a CLI flag
        return False
    if "$" in token or "://" in token or "@" in token:  # a var, a URL, a host
        return False
    return ".." not in token.split("/")


def focus_paths_from_text(text: str) -> tuple[str, ...]:
    """Repo-relative path tokens quoted in `text`, deduped, in order of first use."""
    out: dict[str, None] = {}
    for span in _BACKTICKED.findall(text):
        for raw in span.split():
            token = raw.strip(_PATH_TOKEN_STRIP).rstrip(".").rstrip("/")
            if _is_repo_relative(token):
                out[token] = None
    return tuple(out)


def focus_window(repo: str | Path) -> FocusWindow:
    """The repo's newest handoff doc, as a path window. READ-ONLY, one file read.

    Resolution order mirrors the NO-ARGUMENT chain of the resume flow's own resolver
    (see `HANDOFF_GLOBS`): lowercase family first, caps family second, newest
    within each.

    🔴 THAT IS A NARROWER GUARANTEE THAN THIS DOCSTRING USED TO CLAIM, and the
    old wording — "so step 3 and step 4 of `/resume` cannot end up reconciling
    one initiative while recalling against another" — was already false whenever
    `/resume` was given an argument. This function takes no topic, so a run with
    a topic slug reconciles `handoff-<slug>*.md` while recalling against the
    NEWEST doc, which is a different initiative exactly when it matters. #684
    (a prose argument carrying an explicit path) widens the same gap. Aligning
    the two is a real fix and is NOT done here: it needs a topic parameter, a
    decision about what a miss means on this side, and its own tests. Until then
    this docstring states what the code does, not what would be nice.

    An absent or unreadable doc is an ORDINARY
    outcome — most repos have no handoff at the moment they are resumed — and
    returns an empty window rather than raising: the caller's fallback is a real
    answer, not a degraded one.
    """
    root = Path(repo)
    doc: Path | None = None
    for pattern in HANDOFF_GLOBS:
        try:
            matches = [p for p in root.glob(pattern) if p.is_file()]
        except OSError:
            matches = []
        if matches:
            # mtime, then name: two docs written in the same second must still
            # resolve identically on every run.
            doc = max(matches, key=lambda p: (p.stat().st_mtime, p.name))
            break
    if doc is None:
        return FocusWindow()
    try:
        text = doc.read_text(encoding="utf-8", errors="replace")
        rel = doc.relative_to(root).as_posix()
    except (OSError, ValueError):
        return FocusWindow()
    paths = (rel,) + tuple(p for p in focus_paths_from_text(text) if p != rel)
    return FocusWindow(paths=paths, source=rel)


def select_featured(
    entries: Sequence[RecalledEntry],
    index: SubsystemIndex,
    scope: str,
    *,
    focus_paths: Sequence[str] = (),
    focus_source: str | None = None,
) -> tuple[str, str]:
    """Pick the ONE entry to print in full, and say why. Returns `(ref, basis)`.

    🔴 THE BASIS IS RETURNED, NOT LOGGED, so no caller can render a pick without
    also rendering how it was made. Two selectors, in this order:

      1. RESOLVED — `subsystem_resolver.associate_paths` over `focus_paths`.
         That is the WRITER's matcher, unmodified; this module adds no second
         one. Ranking is `path_count` descending, and the tie-break is the
         fallback's own mtime signal rather than a third rule.
      2. MOST-RECENT FALLBACK — the newest entry file by mtime.

    🔴 WHY MTIME AND NOT GIT RECENCY, WHICH WOULD OTHERWISE BE THE OBVIOUS
    CHOICE: mtime is trustworthy for THIS store specifically because the store
    has no remote and is never cloned, checked out or rebased — the only thing
    that ever touches those files is a session editing them in place, so mtime is
    the edit time. Git recency is NOT usable as the primary signal here: the
    store's history is mostly bulk autocommits ("4 change(s)"), so four entries
    share one commit timestamp and the intra-commit order is unrecoverable. The
    ordinary git-recency argument (a checkout rewrites mtime and git does not) is
    exactly backwards for a repo nobody ever checks out.
    """
    if len(entries) == 0:  # pragma: no cover - callers guard on `scope-empty` first
        # Spelled `len(...) == 0` and not `not entries` on purpose: the latter is
        # the mutation anchor for `recall`'s `scope-empty` guard, and a duplicate
        # anchor turns that kill into a mutation applied to whichever occurrence
        # came first (`claude/RULES.md` — the `count=1` replace hazard).
        raise ValueError("select_featured called with no entries")

    by_ref = {e.ref: e for e in entries}
    if focus_paths:
        matched = [
            m
            for m in associate_paths(
                focus_paths, index, scope, min_paths=FOCUS_MIN_PATHS
            ).matched
            if m.entry.ref in by_ref
        ]
        if matched:
            matched.sort(key=lambda m: (-m.path_count, -by_ref[m.entry.ref].mtime, m.entry.ref))
            best = matched[0]
            shown = ", ".join(best.paths[:3]) + (" …" if best.path_count > 3 else "")
            return best.entry.ref, (
                f"resolved via {focus_source or 'the supplied path window'} — "
                f"{best.path_count} of {len(focus_paths)} quoted path(s) name it: {shown}"
            )

    why = (
        "no handoff doc to read a path window from"
        if not focus_paths
        else f"nothing quoted in {focus_source} resolved to an entry"
    )
    newest = max(entries, key=lambda e: (e.mtime, e.ref))
    return newest.ref, f"most-recent fallback — newest entry file in `{scope}/` ({why})"


# --- The report ----------------------------------------------------------------


@dataclass(frozen=True)
class RecallReport:
    """One deterministic answer to "what does the index already record here?"."""

    status: str
    scope: str
    store_root: str
    entries: tuple[RecalledEntry, ...] = ()
    total_in_scope: int = 0
    """Entries the scope holds, BEFORE `--limit`. The truncation discriminator."""

    limit: int = DEFAULT_ENTRY_LIMIT
    ref: str | None = None

    ref_to: str | None = None
    """The `--ref-to`/`?ref-to=` REVERSE-LOOKUP narrowing, in its CANONICAL spelling.

    `None` means no filter was sent — the same discriminator `ref` uses, rather than a
    second `has_*` boolean, because `str(parse_task_ref(x))` can never be `""` and there
    is therefore no value for which the two would disagree. (The Go port carries an
    explicit `HasRefTo` because `RecallOptions` is a struct with no nil string.)

    ⚠ SET ON EVERY STATUS THE NARROWING REACHED, and NOT on `scope-absent` or
    `scope-unreadable`: both return before the filter runs, and printing "narrowed to X"
    above "this scope does not exist" would suggest the narrowing is why nothing came back.
    """

    ref_to_scope_total: int = 0
    """Entries the scope held BEFORE the ref-to filter ran.

    🔴 IT EXISTS SO THE FILTER CANNOT HIDE WHAT IT REMOVED. `total_in_scope` is the set the
    report is ABOUT, which after a narrowing is the matching entries; without this second
    number a reader could not tell "2 entries reference this" from "the scope holds 2
    entries". `render_text`'s ref-to line prints both.
    """

    ref_to_matched: int = 0
    """Entries in the scope that CARRY the ref-to: the ref-to line's numerator.

    🔴 A STORED COUNT RATHER THAN A DERIVATION, AND THE PENDULUM SWUNG TWICE BEFORE IT BECAME
    ONE. Both mistakes printed a false sentence about the store, in BOTH implementations at
    once, so no assertion comparing them could see either:

    1. Reading `total_in_scope` — the scope's own total on the `ref-to-absent` branch, so the
       report can say how much WAS read — printed "3 of 3 entries in `<scope>/` reference it"
       for a ref NONE of them carried.
    2. Fixing that by forcing the numerator to 0 whenever the status is `ref-to-absent`
       printed "0 of 3 …" and "NO ENTRY REFERENCES <X>" on a `--ref A --ref-to B` run where A
       does not carry B and other entries DO. `ref-to-absent` is reached TWO ways — nothing in
       the scope matched, and the named entry is not among the ones that did — and a status
       cannot tell them apart.

    Both were caught by reading a regenerated golden. It is set once, from the filter's own
    result, on the only branch that runs the filter.
    """

    tag: str = ""
    """The `--tag`/`?tag=` CATEGORY narrowing, FOLDED. `""` means no filter was sent.

    `""` rather than `None`, where `ref` and `ref_to` each carry a `has_*` companion, and the
    asymmetry is forced rather than untidy: `--ref ''` IS a real request that narrows and finds
    nothing, so "" and absent are two states one string cannot hold there. A tag operand that
    folds away is REFUSED by the option ladder, so a report can never hold a present-but-empty
    tag and `report.tag` being non-empty IS "a filter was sent". The OPTION side does need the
    three-state `str | None` — see `recall`'s `tag` parameter, which takes the operand as
    WRITTEN and must be able to refuse a present-but-empty one. (`internal/report` spells that
    same distinction as a `Tag`/`HasTag` pair, because Go has no `None`.)

    ⚠ SET ON EVERY STATUS THE NARROWING REACHED, and NOT on `scope-absent`,
    `scope-unreadable` or `ref-to-absent` — all three return before the tag filter runs, for
    the reason `ref_to`'s own note gives.
    """

    tag_scope_total: int = 0
    """Entries in play BEFORE the tag filter ran — the scope's own total, or the
    `ref-to`-narrowed count when both filters are present, because the tag filter runs second.

    🔴 IT EXISTS SO THE FILTER CANNOT HIDE WHAT IT REMOVED, exactly as `ref_to_scope_total`
    does: without a second number a reader cannot tell "2 entries carry this tag" from "the
    scope holds 2 entries". `render_text`'s tag line prints both.
    """

    tag_matched: int = 0
    """Entries in that set carrying the tag: the tag line's numerator.

    🔴 A STORED COUNT FROM THE FILTER'S OWN RESULT, NEVER DERIVED FROM A STATUS. That is not a
    preference — it is `ref_to_matched`'s measured lesson applied before it could be re-learned
    here, and the mechanism is identical: `tag-absent` is reached TWO ways (nothing in the set
    matched, and the `--ref` operand is not among the ones that did), and a status cannot tell
    them apart. Read this field; do not re-derive it.
    """

    candidates: tuple[str, ...] = ()
    """Filenames an ambiguous `--ref` named. The resolver never picks; nor does this."""

    known_scopes: tuple[str, ...] = ()
    """Every scope the store holds — printed on `scope-absent` so a typo'd or
    unexpectedly-normalized scope is visible instead of reading as "nothing
    recorded yet"."""

    malformed: tuple[MalformedEntry, ...] = ()
    """Entry files in THIS scope that could not be indexed. Rendered on every
    status, before anything else — see `render_malformed`. These are NOT in
    `entries`, `listing` or `total_in_scope`: they never became entries, and
    counting them as if they had would be the silent short index the collecting
    loader exists to avoid."""

    malformed_elsewhere: tuple[MalformedEntry, ...] = ()
    """The same, in every OTHER scope of the store. A count with its scopes named,
    so a defect outside the scope being recalled is still visible."""

    @property
    def malformed_total(self) -> int:
        return len(self.malformed) + len(self.malformed_elsewhere)

    mode: str = DEFAULT_MODE
    """Which display mode produced this report — see `RECALL_MODES`."""

    listing: tuple[RecalledEntry, ...] = ()
    """ONE PAGE of the index, one line per entry, newest-first by file mtime.

    Up to `LISTING_PAGE_SIZE` lines. It is a PAGE and not a filter: `listing_total`
    counts every entry in the scope, `listing_pages` says how many pages that is,
    and the renderer prints the remainder with the `--page` that reaches it — so
    the set of things on record stays complete even when only one of them is
    printed in full. Empty in `full` mode, which prints bodies and a truncation
    notice instead."""

    listing_total: int = 0
    """Entries the index covers, BEFORE the page cap. The pagination discriminator:
    `listing_total > len(listing)` is the ONLY thing that makes the notice fire."""

    listing_page: int = 1
    listing_pages: int = 1
    """1-based page, and how many pages `listing_total` makes at
    `LISTING_PAGE_SIZE`. `listing_pages` is at least 1 even for an empty index, so
    "page 1 of 1" never reads as "page 1 of 0"."""

    @property
    def page_is_past_the_end(self) -> bool:
        """🔴 THE ONE PLACE THIS QUESTION IS ASKED. Three renderer branches need
        it — the index header, the page notice and `--list`'s completeness line —
        and the first shipped version answered it separately in each, which is how
        a header printed `entries 801–800 of 150 (page 9 of 2)` while the notice
        directly under it was correct."""
        return self.listing_page > self.listing_pages

    @property
    def listing_before_page(self) -> int:
        """Index rows on EARLIER pages. 0 on a page past the end: nothing was
        listed there, so "before" describes no position."""
        if self.page_is_past_the_end:
            return 0
        return (self.listing_page - 1) * LISTING_PAGE_SIZE

    @property
    def listing_after_page(self) -> int:
        """Index rows on LATER pages — what a truncation notice is about.

        🔴 THE BUG THIS PROPERTY EXISTS FOR. The notice originally fired on
        `listing_total > len(listing)`, which is "this page does not hold the
        whole index" — TRUE on the LAST page too. On page 2 of 2 it therefore
        announced page 1's 100 entries as still unseen and routed the reader to a
        `--page 3` that does not exist: a false truncation notice contradicting
        the correct header on the line above it. What a notice must count is what
        comes AFTER this page, which is 0 on the last one.
        """
        if self.page_is_past_the_end:
            return 0
        return max(0, self.listing_total - self.listing_before_page - len(self.listing))

    featured_basis: str | None = None
    """Which selector chose `entries[0]`, in words — `select_featured`'s second
    return value. Set in `digest` mode only. Never None there: a featured entry
    with no stated basis is the implicit pick this module refuses to make."""

    @property
    def renders_narrowed_set(self) -> bool:
        """Does anything BELOW the ref-to line report on the entries the filter kept?

        🔴 THE RENDER DECISION AND NEVER A STATUS NAME — THE SAME MISTAKE AS
        `ref_to_matched`'s, ONE LEVEL OUT, AND MEASURED WRONG THE SAME WAY. The clause
        was first derived as `status != "ref-to-absent"`, whose comment asserted that
        was false on `ref-to-absent` and on nothing else. Three other shapes count
        matched entries and then render none of them, so each printed "everything below
        is about those N" over a body about no entry at all: `ref-absent`
        (`--ref <no such entry> --ref-to <carried by others>`), `ref-ambiguous` (whose
        own sentence says "nothing was surfaced"), and a `list`-mode `--page` past the
        end (whose notice says "Nothing was listed here").

        ⚠ AND `page_is_past_the_end` IS NOT THE MISSING TERM EITHER — the SAME page past
        the end in `digest` mode still prints the featured body, so it does report on a
        matched entry. That case is the control separating this predicate from any
        spelling built out of statuses and page arithmetic.

        `entries` and `listing` are the only two sets below the header that can hold an
        entry THE FILTER KEPT, and after the filter both hold matched entries exclusively
        — so their emptiness IS the question, and it stays the question if a status is
        added or a branch moves. `internal/report.RecallReport.RendersNarrowedSet` is the
        Go twin; `tests/parity/harness.py` diffs the two clients' bytes.

        ⚠ NOT "the only two sets the renderer prints below the header", which is what
        this said and is false: `candidates`, `malformed`, `malformed_elsewhere` and
        `known_scopes` all print below it. None of them can carry a matched entry —
        candidates are filenames offered by an AMBIGUOUS ref, the two malformed sets are
        rejects the filter never saw because they never reached the index, and
        `known_scopes` is a list of scope names — so the property is unaffected. The
        sentence was wrong; the claim under it was not.
        """
        return bool(self.entries) or bool(self.listing)

    @property
    def omitted(self) -> int:
        """Entries in the scope whose BODY was not printed.

        A `--ref` run reports 0: one of four is a NARROWING, not a truncation,
        and calling it an omission trains the reader to ignore the real one. The
        digest reports a real count — 24 of 25 bodies genuinely were not printed
        — and the renderer says so in the digest's own words, because there
        every one of those 24 is still LISTED and one `--ref` away, which is not
        what `--limit` truncation means.
        """
        return max(0, self.total_in_scope - len(self.entries)) if self.ref is None else 0

    @property
    def caveat(self) -> str:
        """What this window can and cannot see — `caveat_text`, and nothing local.

        🔴 Delegated rather than written into each renderer: the two renderers,
        every status branch AND the search report must make the SAME claim, and a
        sentence duplicated per branch is one edit away from a branch that
        promises more than the store can support.
        """
        # 🔴 `listing`, NOT `entries`. Badges are rendered by `listing_line`, which
        # iterates `report.listing`; `entries` holds only the FEATURED bodies and is
        # a different, usually smaller set. Reading `entries` here looked right and
        # was silently wrong — the explanation vanished on an index whose visible
        # `🔴 2 NEAR-MISS` row simply was not among the featured entries. Caught by
        # running it, not by reading it.
        return caveat_text(f"{self.scope}/", badges_present(self.listing))


def _store_unreadable(store: Path, exc: OSError) -> EntryUnreadableError:
    """The ONE store-wide "not fully read" sentence, with ONE writer.

    🔴 THESE BYTES ARE COMPARED ACROSS THE TWO CLIENTS, which is why the sentence
    lives behind a name rather than being spelled at its call site. The Go twin is
    `internal/store.StoreUnreadable`, and `tests/parity/harness.py`'s
    `validate-unreadable-entry` / `recall-unreadable-entry` /
    `*-unreadable-scope-dir` rows diff the two clients' stderr byte for byte — so a
    second spelling of it anywhere is a divergence waiting to happen.

    ⚠ ONE CALL SITE TODAY (`load_store`, below). Re-derive rather than trusting this
    sentence: `grep -n '_store_unreadable(' lib/subsystem_recall.py`.
    """
    return EntryUnreadableError(
        f"index entry unreadable: under {store} ({type(exc).__name__}: {exc}) — the "
        f"store was not fully read, so this report would be INCOMPLETE"
    )


def load_store(
    store_root: str | Path,
    *,
    verb: str,
    visible_scopes: Sequence[str] | None = None,
) -> tuple[Path, SubsystemIndex]:
    """Resolve the store root and load its index, or raise with a sentinel.

    🔴 ONE PLACE, because `recall` and `search` open the SAME store for the same
    reasons and a predicate open-coded at two sites is wrong at one of them.
    `verb` is the only thing that differs: a store-missing message has to say what
    did NOT happen ("nothing was recalled" / "nothing was searched") or it reads
    as the ordinary nothing-recorded-yet case, which is the confident zero this
    module exists to prevent.

    🔴 `visible_scopes` IS A NARROWING OF THE WHOLE INDEX, AND IT IS APPLIED HERE
    FOR EXACTLY THE REASON ABOVE — this is the single site both readers get their
    index from, so a caller that may only see some scopes cannot be given a wider
    one by a route that forgot to filter. `None` means UNRESTRICTED (every local
    CLI caller); a sequence means "these normalized scopes and nothing else".

    Filtering the INDEX rather than each answer is what closes four leaks at
    once, and every one of them was measured on the deployed API before this
    change:

      * `known_scopes` — a `scope-absent` report ends with "scopes the store does
        hold: …", so asking for a scope you do not have enumerated every scope
        you do not have either.
      * `malformed_elsewhere` — the "(+N further malformed entries in OTHER
        scopes …)" block names those scopes on EVERY status, not only on a miss.
      * `search?all_scopes=1` — it names no scope at all, so a per-scope refusal
        check cannot see it; it searched the CONTENT of every scope in the store.
      * the entries themselves.

    An empty sequence therefore means "no scope is visible", NOT "unrestricted".
    That direction is deliberate: a caller that forgot to resolve an allowlist
    gets an empty store, not the whole one.

    🔴 IT LOADS WITH `ON_MALFORMED_COLLECT`, AND THAT IS THIS MODULE'S POLICY
    DECISION, NOT THE LOADER'S. A malformed entry no longer raises here: it comes
    back on `index.malformed` and every caller of this function is obliged to
    render it (`render_malformed`). The measurement that forced the change is in
    the module docstring — fail-closed cost the whole scope, 2 good entries and 1
    bad one served ZERO. The WRITER's probe deliberately
    keeps the raise: it gates a write, and writing into a store you have read
    only part of is the one case where aborting is cheaper than degrading.

    ⚠ `MalformedEntryError` can therefore no longer escape this function from the
    validator, so nothing catches it — but an `OSError` still fails closed below,
    because "could not interpret this file" and "could not READ the store" are
    different facts and only the first has an honest degraded form.
    """
    store = Path(store_root)
    if not store.is_dir():
        raise StoreMissingError(
            f"store root not found: {store} — expected the `/analyze-service` index "
            f"store. Nothing was {verb}; this is NOT 'nothing recorded yet'"
        )
    try:
        # 🔴 THE ALLOWLIST GOES DOWN INTO THE LOADER, NOT ONLY ONTO ITS RESULT.
        # Narrowing afterwards still opened every file in every scope, so one
        # unreadable entry in a scope the caller may NOT see put that file's full
        # path into a 503 body, broke recall for everyone, and — for a FIFO named
        # `*.md` — hung the request thread outright. See `load_index`.
        index = load_index(
            store,
            on_malformed=ON_MALFORMED_COLLECT,
            visible_scopes=visible_scopes,
        )
    except OSError as exc:
        # ⚠ THE SENTENCE IS `_store_unreadable`'s, NOT SPELLED HERE, because the
        # parity gate compares these bytes against the Go client's
        # `store.StoreUnreadable`.
        raise _store_unreadable(store, exc) from exc
    if visible_scopes is None:
        return store, index
    # 🔴 REBUILT FROM THE TWO PUBLIC FIELDS, not by mutating a frozen dataclass
    # and not by adding a third field. `scopes`, `malformed_in`,
    # `malformed_outside`, `entries` and `__len__` are ALL derived from
    # `by_scope` + `malformed`, so narrowing exactly those two narrows every
    # derived answer at once — including the ones a future accessor adds. A
    # per-answer filter would have to be repeated at every one of them, which is
    # the shape that is wrong at all but one site.
    #
    # ⚠ REDUNDANT WITH THE LOADER'S OWN FILTER TODAY, AND KEPT DELIBERATELY.
    # `load_index` now skips a denied scope dir entirely, so this rebuild has
    # nothing left to drop — a mutation sweep will score it as an equivalent
    # mutant. It stays because the two filters answer different questions: the
    # loader's decides what is OPENED, this one decides what the RESULT SHAPE is,
    # and a future caller that hands this function an already-loaded index, or a
    # loader that learns a reason to register a scope it did not read, must not
    # silently widen the answer. Both derive the set from `visible_scope_set`, so
    # they cannot come to disagree about what an allowlist means.
    allowed = visible_scope_set(visible_scopes)
    assert allowed is not None  # `visible_scopes is None` returned above
    return store, SubsystemIndex(
        by_scope={k: v for k, v in index.by_scope.items() if k in allowed},
        malformed=tuple(m for m in index.malformed if m.scope in allowed),
    )


def recall(
    store_root: str | Path,
    scope: str,
    *,
    ref: str | None = None,
    ref_to: str | None = None,
    tag: str | None = None,
    limit: int = DEFAULT_ENTRY_LIMIT,
    mode: str = DEFAULT_MODE,
    page: int = 1,
    focus_paths: Sequence[str] = (),
    focus_source: str | None = None,
    visible_scopes: Sequence[str] | None = None,
) -> RecallReport:
    """Surface an entry's `## What it is` + `## Pointers` + `## Nuance / work-history`.

    READ-ONLY. No clock, no network, no git, no prompt — `/resume`'s job is to
    re-enter work, and a recall step that interrogated the network or blocked on
    a confirm would make the thing it is supposed to accelerate slower.

    `focus_paths` is INJECTED, exactly as `associate_paths` injects its index:
    this function performs no repo I/O of its own, so a test can pin the
    selection rule without a fixture repo and `main()` owns the one call to
    `focus_window`. `focus_source` is only ever quoted back in the printed basis.

    Guard order — each reachable by an input no earlier guard rejects:
      1. `limit` sanity      → ValueError
      2. `page` sanity       → ValueError  (a valid limit still reaches it)
      3. `mode` known        → ValueError  (a valid limit AND page still reach it)
      4. store root exists   → StoreMissingError
      5. store is readable   → EntryUnreadableError
         (a MALFORMED entry does not raise: it is collected and reported. Only an
         `OSError` — the store was not fully read — still fails closed here.)
      6. scope known         → status `scope-absent`, NOT an error
      7. anything readable   → status `scope-unreadable` when the scope holds
         entry files and none of them indexed. Checked BEFORE `--ref`, because a
         ref cannot be honestly called absent from a scope nothing was read from.

    🔴 AN ABSENT SCOPE IS A STATUS, NOT AN EXCEPTION, and that is the single most
    load-bearing decision here. The store holds 2 scopes while work spans ~12
    repos, so "this repo has nothing recorded yet" is the ORDINARY outcome, not a
    failure. Raising would make the common case the exceptional one, every caller
    would wrap the call, and a wrapped call is how the genuine errors above get
    swallowed too — the same argument `associate_paths` makes for an empty path
    set.
    """
    if not isinstance(limit, int) or isinstance(limit, bool) or limit < 1:
        raise ValueError(f"limit must be an int >= 1, got {limit!r}")
    if not isinstance(page, int) or isinstance(page, bool) or page < 1:
        raise ValueError(f"page must be an int >= 1, got {page!r}")
    if mode not in RECALL_MODES:
        raise ValueError(f"mode must be one of {RECALL_MODES}, got {mode!r}")
    # 🔴 LAST IN THE LADDER, DELIBERATELY, BECAUSE THE ORDER ABOVE IS A RECORDED
    # CONTRACT. A request carrying two bad parameters gets ONE message and which one it
    # gets is in the goldens; inserting a new guard anywhere but the end moves those.
    ref_to_ref = _validated_ref_to(ref_to)
    # AFTER `ref-to`, for the same reason `ref-to` is after `mode`: the ladder's order decides
    # which message a request carrying two bad parameters gets, and that order is recorded in
    # the goldens. A new guard goes at the END.
    want_tag = _validated_tag(tag)

    # 🔴 `visible_scopes` IS PASSED, NEVER RE-DERIVED. A scope the caller may not
    # see must be absent from the INDEX, not filtered out of each answer — see
    # `load_store`. Once it is absent, `scope-absent` (and its `known_scopes`
    # list) is what a refused scope produces, which is byte-for-byte what a scope
    # that never existed produces.
    store, index = load_store(
        store_root, verb="recalled", visible_scopes=visible_scopes
    )
    # Computed ONCE, before any status branch, and passed to every one of them —
    # the block renders on all of them (`render_malformed`), so deriving it per
    # branch would be the same predicate at six sites, wrong at five.
    bad = index.malformed_in(scope)
    bad_elsewhere = index.malformed_outside((scope,))

    try:
        entries = index.entries(scope)
    except UnknownScopeError:
        return RecallReport(
            status="scope-absent",
            scope=normalize_ref(scope),
            store_root=str(store),
            limit=limit,
            mode=mode,
            ref=ref,
            known_scopes=index.scopes,
            malformed=bad,
            malformed_elsewhere=bad_elsewhere,
        )

    # 🔴 BEFORE `--ref`, AND BEFORE `scope-empty`. A scope whose every file was
    # rejected reaches both of those branches looking identical to a scope that
    # holds nothing — `ref-absent` would say "nothing recorded under that name
    # yet" and `scope-empty` would say "NOTHING RECORDED YET", and both would be
    # false about a directory full of content. This is the discriminator, and it
    # is the ONLY thing standing between a broken store and a status `/resume`
    # reports as an ordinary non-finding.
    if not entries and bad:
        return RecallReport(
            status="scope-unreadable",
            scope=normalize_ref(scope),
            store_root=str(store),
            total_in_scope=0,
            limit=limit,
            mode=mode,
            ref=normalize_ref(ref) if ref is not None else None,
            known_scopes=index.scopes,
            malformed=bad,
            malformed_elsewhere=bad_elsewhere,
        )

    # 🔴 THE REVERSE-LOOKUP NARROWING GOES HERE: AFTER `index.entries(scope)`, WHICH
    # IS AFTER SCOPE AUTHORISATION, AND NEVER BEFORE IT.
    #
    # The index `load_store` returned is already narrowed to the scopes this caller
    # may read — that is the ONE narrowing, and `load_store`'s own header lists the
    # four leaks a per-answer filter re-opens. So this filter can only ever REMOVE
    # entries from a set the caller was already entitled to, and it is structurally
    # incapable of surfacing one they were not.
    #
    # 🔴 THE WRONG ORDER IS A REAL AND CHEAP MISTAKE, WHICH IS WHY THE ORDER IS STATED
    # RATHER THAN LEFT TO THE CALL SEQUENCE. "Which entries reference this?" reads like
    # a store-wide question, and the obvious implementation of a store-wide question is
    # a store-wide load — `load_index(root)` with `visible_scopes` quietly dropped —
    # followed by a ref filter. That answers correctly and LEAKS: the hit set names
    # entries, scopes and refs from directories the caller cannot read, which is
    # exactly the `search?all_scopes=1` hole `load_store` closed.
    #
    # ⚠ `total_in_scope` BECOMES THE NARROWED COUNT and `ref_to_scope_total` carries
    # the scope's own, for the reason `ref_to_scope_total`'s docstring gives.
    # 🔴 ONE DICT, BUILT ONCE AND SPLATTED INTO EVERY RETURN BELOW. Eleven literals build a
    # `RecallReport` in this function and a filter's fields belong on every one of them the
    # filter reached; assigning them per literal is the same assignment at eleven sites, wrong
    # at the one somebody forgets.
    #
    # ⚠ IT WAS `ref_to_fields` AND IS NOW `filter_fields`, BECAUSE TWO FILTERS WRITE INTO IT
    # AND A SECOND DICT WOULD RE-OPEN EXACTLY WHAT THIS ONE CLOSES. Two accumulators mean two
    # splats at every return, and the failure mode is the asymmetric one: a return site that
    # splatted one and forgot the other would print a `ref-to:` header with no `tag:` line
    # beside it, which reads as "no tag filter was sent" rather than as a missing field.
    # WHICH keys are present is what says which filters ran, and each block below adds its own
    # only after its own narrowing has happened.
    filter_fields: dict[str, object] = {}
    scope_total = len(entries)
    if ref_to_ref is not None:
        matching = tuple(e for e in entries if entry_references(e, ref_to_ref))
        # 🔴 `ref_to_matched` GOES IN HERE, FROM THE FILTER'S OWN RESULT, BEFORE ANY STATUS
        # BRANCH — it is the one number the rendered line's numerator may be read from. See
        # the field's docstring for the two false sentences that came out of deriving it.
        filter_fields.update(
            {
                "ref_to": str(ref_to_ref),
                "ref_to_scope_total": scope_total,
                "ref_to_matched": len(matching),
            }
        )
        if not matching:
            return RecallReport(
                status="ref-to-absent",
                scope=normalize_ref(scope),
                store_root=str(store),
                total_in_scope=scope_total,
                limit=limit,
                mode=mode,
                ref=normalize_ref(ref) if ref is not None else None,
                known_scopes=index.scopes,
                malformed=bad,
                malformed_elsewhere=bad_elsewhere,
                **filter_fields,
            )
        entries = matching

    # 🔴 THE SET THE `ref-to` MEMBERSHIP TEST BELOW IS ABOUT, CAPTURED BEFORE THE TAG FILTER
    # NARROWS `entries` AGAIN — AND KEEPING IT IS WHAT MAKES THE TWO NON-FINDINGS
    # ATTRIBUTABLE. Without it, `--ref X --ref-to Y --tag Z` where X carries Y but not Z takes
    # the `ref-to` branch and prints "`X` DOES NOT REFERENCE `Y`", which is FALSE: X references
    # Y, and the thing it lacks is the tag. Each filter's own non-finding has to be tested
    # against that filter's own result, and after a second narrowing `entries` is the
    # intersection and can answer for neither.
    ref_to_narrowed = entries

    # 🔴 THE CATEGORY NARROWING GOES HERE: AFTER `index.entries(scope)`, WHICH IS AFTER SCOPE
    # AUTHORISATION, AND NEVER BEFORE IT.
    #
    # The index `load_store` returned is already narrowed to the scopes this caller may read —
    # that is the ONE narrowing, and `load_store`'s own header lists the leaks a per-answer
    # filter re-opens. So this filter can only ever REMOVE entries from a set the caller was
    # already entitled to, and it is structurally incapable of surfacing one they were not.
    #
    # 🔴 THE WRONG ORDER IS THE SAME REAL, CHEAP MISTAKE THE `ref-to` FILTER NAMES, AND IT IS
    # EASIER TO MAKE HERE. "Every marketing entry, across scopes" reads even more like a
    # store-wide question than a reverse lookup does, and the obvious implementation of a
    # store-wide question is a store-wide load — `load_index(root)` with `visible_scopes`
    # quietly dropped — followed by a tag filter. That answers correctly and LEAKS: the hit set
    # names entries, scopes and tags from directories the caller cannot read. A TAG is exactly
    # the operand somebody reaches for across a whole store.
    #
    # ⚠ `total_in_scope` BECOMES THE NARROWED COUNT AND THE PRE-FILTER TOTAL GOES TO
    # `tag_scope_total`, which is the rule `ref_to_scope_total` states one block up.
    if want_tag is not None:
        tag_scope_total = len(entries)
        tag_matching = tuple(e for e in entries if entry_has_tag(e, want_tag))
        # 🔴 FROM THE FILTER'S OWN RESULT, BEFORE ANY STATUS BRANCH — see the field's docstring
        # for why a derivation from the status name is measured wrong.
        filter_fields.update(
            {
                "tag": want_tag,
                "tag_scope_total": tag_scope_total,
                "tag_matched": len(tag_matching),
            }
        )
        if not tag_matching:
            return RecallReport(
                status="tag-absent",
                scope=normalize_ref(scope),
                store_root=str(store),
                total_in_scope=tag_scope_total,
                limit=limit,
                mode=mode,
                ref=normalize_ref(ref) if ref is not None else None,
                known_scopes=index.scopes,
                malformed=bad,
                malformed_elsewhere=bad_elsewhere,
                **filter_fields,
            )
        entries = tag_matching

    if ref is not None:
        try:
            entry, _tier = resolve_ref_tiered(ref, index, scope)
        except AmbiguousRefError as exc:
            return RecallReport(
                status="ref-ambiguous",
                scope=normalize_ref(scope),
                store_root=str(store),
                total_in_scope=len(entries),
                limit=limit,
                mode=mode,
                ref=exc.ref,
                candidates=exc.candidates,
                known_scopes=index.scopes,
                malformed=bad,
                malformed_elsewhere=bad_elsewhere,
                **filter_fields,
            )
        if entry is None:
            return RecallReport(
                status="ref-absent",
                scope=normalize_ref(scope),
                store_root=str(store),
                total_in_scope=len(entries),
                limit=limit,
                mode=mode,
                ref=normalize_ref(ref),
                known_scopes=index.scopes,
                malformed=bad,
                malformed_elsewhere=bad_elsewhere,
                **filter_fields,
            )
        # 🔴 THE TWO NARROWINGS COMPOSE, AND THE RESOLVER DOES NOT KNOW ABOUT THE
        # FIRST ONE. `resolve_ref_tiered` resolves against the whole (authorised)
        # INDEX — it has to, since the alias and cross-scope tiers are properties of
        # the index and not of a tuple — so an entry it finds is not necessarily in
        # the ref-to set. Without this membership test, `--ref X --ref-to Y` would
        # print X in full whether or not X references Y: the report answering a
        # question nobody asked.
        #
        # ⚠ COMPARED BY (scope, filename), NOT BY `ref`. `SubsystemEntry.ref` is
        # `<slug>[.<kind>]` and carries no scope, so two entries in two scopes can
        # share one — a ref-only test would accept a cross-scope resolution as a
        # member of this scope's narrowed set. The pair used here is what the loader
        # itself locates a file by.
        #
        # 🔴 `total_in_scope` IS THE NARROWED COUNT HERE — `len(entries)`, NOT `scope_total`.
        # `entries` has already been replaced by the matching set above, and the rule this
        # function declares is that after a narrowing `total_in_scope` is the set the report is
        # ABOUT while `ref_to_scope_total` carries the scope's own. This branch passed
        # `scope_total` while the Go port passed the narrowed count, so the two
        # implementations disagreed about one field's MEANING.
        #
        # ⚠ AND NO CONSUMER COULD HAVE OBSERVED IT — FIXED BEFORE IT HAD ONE, WHICH IS A
        # NARROWER CLAIM THAN THE DRAFT MADE. That draft called it a LIVE divergence on the
        # grounds that `report_json` serialises the field, and that does not follow:
        # `report_json` has no caller outside `tests/`, `main` below passes no `ref_to` and
        # therefore cannot reach this branch at all, `cairn` renders text, and the Go port
        # has no JSON recall payload to disagree with. Nothing renders `total_in_scope` here
        # either, so no byte-diff gate could see it.
        #
        # ⚠ AGAINST `ref_to_narrowed` AND NOT `entries`, BECAUSE A SECOND FILTER NOW SITS
        # BETWEEN THEM. `entries` is the intersection of both narrowings, so an entry missing
        # only the TAG would be reported here as not referencing the ref — a sentence that is
        # false about the store. See `ref_to_narrowed`.
        if ref_to_ref is not None and not any(
            e.scope == entry.scope and e.filename == entry.filename for e in ref_to_narrowed
        ):
            return RecallReport(
                status="ref-to-absent",
                scope=normalize_ref(scope),
                store_root=str(store),
                total_in_scope=len(ref_to_narrowed),
                limit=limit,
                mode=mode,
                ref=normalize_ref(ref),
                known_scopes=index.scopes,
                malformed=bad,
                malformed_elsewhere=bad_elsewhere,
                **filter_fields,
            )
        # The same membership test for the TAG narrowing, against the tag filter's own result.
        # `ref-to` is checked FIRST so the precedence between the two is stated rather than
        # incidental: it is the order the filters ran in.
        if want_tag is not None and not any(
            e.scope == entry.scope and e.filename == entry.filename for e in entries
        ):
            return RecallReport(
                status="tag-absent",
                scope=normalize_ref(scope),
                store_root=str(store),
                total_in_scope=len(entries),
                limit=limit,
                mode=mode,
                ref=normalize_ref(ref),
                known_scopes=index.scopes,
                malformed=bad,
                malformed_elsewhere=bad_elsewhere,
                **filter_fields,
            )
        # A `--ref` run is a NARROWING and prints its one entry in full whatever
        # `mode` says — no index, no featured basis, byte-identical to what it
        # printed before the digest existed.
        return RecallReport(
            status="recalled",
            scope=normalize_ref(scope),
            store_root=str(store),
            entries=(read_entry(store, entry),),
            total_in_scope=len(entries),
            limit=limit,
            mode=mode,
            ref=normalize_ref(ref),
            known_scopes=index.scopes,
            malformed=bad,
            malformed_elsewhere=bad_elsewhere,
            **filter_fields,
        )

    if not entries:
        return RecallReport(
            status="scope-empty",
            scope=normalize_ref(scope),
            store_root=str(store),
            total_in_scope=0,
            limit=limit,
            mode=mode,
            known_scopes=index.scopes,
            malformed=bad,
            malformed_elsewhere=bad_elsewhere,
            **filter_fields,
        )

    # THE ONE ordering site: canonical ref ascending, so two runs over an
    # unchanged store produce identical bytes and a diff of them shows only real
    # movement.
    ordered = sorted(entries, key=lambda e: e.ref)

    if mode == "full":
        return RecallReport(
            status="recalled",
            scope=normalize_ref(scope),
            store_root=str(store),
            entries=tuple(read_entry(store, e) for e in ordered[:limit]),
            total_in_scope=len(ordered),
            limit=limit,
            mode=mode,
            known_scopes=index.scopes,
            malformed=bad,
            malformed_elsewhere=bad_elsewhere,
            **filter_fields,
        )

    # `digest` and `list` both read EVERY entry — the index line carries a bullet
    # count, which only the file can answer, and the featured pick needs every
    # entry's mtime. Reading 26 small files costs milliseconds; it is the OUTPUT
    # that had to shrink, not the input. The PAGE cap is applied to the rendered
    # listing only, never to `read`.
    read = tuple(read_entry(store, e) for e in ordered)
    page_slice, pages = listing_page(read, page)
    if mode == "list":
        return RecallReport(
            status="recalled",
            scope=normalize_ref(scope),
            store_root=str(store),
            total_in_scope=len(ordered),
            limit=limit,
            mode=mode,
            listing=page_slice,
            listing_total=len(read),
            listing_page=page,
            listing_pages=pages,
            known_scopes=index.scopes,
            malformed=bad,
            malformed_elsewhere=bad_elsewhere,
            **filter_fields,
        )

    featured_ref, basis = select_featured(
        read, index, scope, focus_paths=focus_paths, focus_source=focus_source
    )
    return RecallReport(
        status="recalled",
        scope=normalize_ref(scope),
        store_root=str(store),
        entries=tuple(e for e in read if e.ref == featured_ref),
        total_in_scope=len(ordered),
        limit=limit,
        mode=mode,
        listing=page_slice,
        listing_total=len(read),
        listing_page=page,
        listing_pages=pages,
        featured_basis=basis,
        known_scopes=index.scopes,
        malformed=bad,
        malformed_elsewhere=bad_elsewhere,
        **filter_fields,
    )


# --- The index page ------------------------------------------------------------


def listing_order(entries: Sequence[RecalledEntry]) -> tuple[RecalledEntry, ...]:
    """The index's OWN order: newest-first by file mtime, tie-broken by ref.

    🔴 A SECOND ORDERING SITE, on purpose, and the only one that is not
    ref-ascending. It exists because the index is now CAPPED (`LISTING_PAGE_SIZE`)
    and a cap makes the order load-bearing: cutting an alphabetical list at 100
    hides entries by an accident of their names, while cutting a recency list
    hides the stale ones — the only cut that means anything to a session
    re-entering work. `full` mode's bodies keep the canonical ref order, because
    that output is a requested dump whose reader is scanning for a name.

    Determinism is unaffected: the store is never cloned, checked out or rebased
    (see `select_featured` on why mtime is the edit time here), and the ref
    tie-break settles two entries written in the same nanosecond.
    """
    return tuple(sorted(entries, key=lambda e: (-e.mtime, e.ref)))


def listing_page(
    entries: Sequence[RecalledEntry], page: int
) -> tuple[tuple[RecalledEntry, ...], int]:
    """One page of the ordered index, and how many pages there are in total.

    A page PAST the end returns an empty slice rather than clamping to the last
    one: clamping would answer a question the caller did not ask and print a full
    page under a heading saying `--page 9`, which is the silent-wrong shape this
    module refuses everywhere else. The renderer says the page is past the end and
    names the valid range.
    """
    ordered = listing_order(entries)
    pages = max(1, -(-len(ordered) // LISTING_PAGE_SIZE))  # ceil, no float
    start = (page - 1) * LISTING_PAGE_SIZE
    return ordered[start : start + LISTING_PAGE_SIZE], pages


# --- Rendering -----------------------------------------------------------------


def sensitivity_label(effective: str, declared: str | None) -> str:
    """`<effective>` — or `<effective> (declared: <x>)` when a marker was overridden.

    ONE spelling, taking the two VALUES rather than a report type, because THREE
    surfaces print it — the index row, the `--ref`/featured entry header and the
    search hunk — and only two of them hold a `RecalledEntry`.
    """
    if declared is None:
        return effective
    return f"{effective} (declared: {declared})"


def short_heading(heading: str) -> str:
    """`## Nuance / work-history` → `Nuance / work-history`. ONE spelling.

    The index row names a heading in ~60 B of budget, so it drops the ATX
    marker the printed body keeps. It still names the heading in FULL — `NO
    Pointers` and `NO Nuance / work-history` are different facts with different
    next actions, and a badge that said only `NO SECTION` would make them one.
    """
    return heading.lstrip("#").strip()


def listing_line(entry: RecalledEntry, width: int) -> str:
    """ONE index line: `  <ref>   N nuance  <sensitivity>[  <badges>]`. ~60 B.

    The ref is what `--ref` takes, the count is the size signal that says whether
    a `--ref` is worth spending, and the sensitivity has to travel WITH the entry
    it describes — a sensitivity stated once at the top of a block is a
    sensitivity that gets copied away from.

    🔴 THIS SAID "THREE FIELDS AND NO FOURTH", AND THE FOURTH IS ADDED
    DELIBERATELY. The bar that wording set is the right one, so it is restated
    rather than dropped: a field earns this line only if it changes what the
    reader DOES, because the index is the one thing printed for every entry and
    the cost is paid on every read. `open` clears that bar where size does not —
    an entry with unfinished business may be describing a remedy that has since
    landed, and the reader cannot tell from the body. It is also **conditional**:
    entries with nothing declared open render byte-identical to before, so the
    common case pays nothing. Nothing else has cleared this bar; keep it that way.

    ⚠ THE COUNT IS `## Nuance / work-history` BULLETS ONLY, and the word says so
    rather than leaving it to be assumed. It was `N bullets`, which reads as
    ENTRY SIZE to anyone scanning the index — an entry with 5 pointers and 7
    nuance bullets showed `7 bullets` and has 12. Naming the section is free
    (`nuance` is one character shorter than `bullets`) and the count itself is
    deliberately not entry size: pointers are durable and do not grow, while
    work-history is what the store's prune-on-resolve discipline is denominated
    in, so it is the number that predicts whether a `--ref` is worth spending.

    🔴 THREE MORE BADGES, AND THE BAR ABOVE IS WHY THEY CLEAR IT — every one
    reports a state in which the NUMBERS TO THEIR LEFT ARE NOT MEASUREMENTS:

      `🔴 N NEAR-MISS`  N bullets tried to write a marker and missed the
                        grammar. They declare nothing, so `N OPEN` is short by
                        up to N and the reader cannot tell. Measured:
                        2 such bullets across 53 entries, against
                        8 that declare `OPEN:` and parse.
      `⚠ N UNVERIFIABLE` N `RESOLVED:` bullets name no sha. Advisory — closing
                        is the point — but the closure cannot be checked.
      `🔴 NO <heading>` the heading is absent or renamed, so the section was
                        never parsed: `0 nuance` and a missing `OPEN` badge on
                        that row mean PARSE FAILURE, not an empty entry. This
                        one used to render only under a printed BODY, and the
                        digest prints one body out of N — so on every other row
                        it was computed and thrown away.

    All three are CONDITIONAL, like `OPEN` and for the same reason: measured
    over the live store, 1 entry of 53 would carry a near-miss
    badge and 0 would carry either of the other two, so the other 52 rows stay
    byte-identical to what they render today.

    ⚠ ORDER IS `--validate`'S ORDER (declared → near-miss → unverifiable), so a
    reader who has seen one surface can read the other without re-learning it.
    The `NO <heading>` badge sits last because it is not a bullet population at
    all — it says the parser never reached the bullets.

    ⚠ THE BADGES ARE TEXT-ROW ONLY, deliberately, following `open_count`: the
    JSON `listing` rows carry `ref`/`file`/`sensitivity`/`nuance_bullets` and no
    openness field of any kind. A JSON consumer that wants the populations
    reads `entries[].missing_sections` or runs `--validate`; splitting the row's
    vocabulary across two payloads is how the two start to disagree.

    🔴 A FIFTH BADGE — `🔗 N ref(s)` — AND THE BAR ABOVE IS THE REASON IT IS A
    COUNT AND NOT THE REFS. It clears the "changes what the reader DOES" bar on
    the same grounds `OPEN` does: an entry joined to a ref is an entry whose
    work has a tracked owner and a closing condition somewhere else, and that is
    the single fact that decides whether to spend a `--ref`. What it does NOT do
    is print the refs themselves — `github:example-org/alpha-toolkit#428` is 36
    characters, three of them would triple the row, and the index's whole
    contract is one line per entry. The refs are printed in the ENTRY BODY, which
    `--ref <name>` and the featured entry already show; the row says only that
    there are some.

    🔴 THE WORD IS `ref`, AND THE CHANGE FROM `task` IS THE POINT RATHER THAN A
    TIDY-UP. `entry.tasks` is the parsed `refs:` front-matter sequence, and once
    `tasks:` folded into `refs:` the badge was naming a key the file format no
    longer has — a reader who grepped their entries for `tasks:` after reading
    this row found nothing and could not tell a renamed key from an absent one.
    The accepted older INPUT spellings (`tasks:`, `task:`) are untouched; this is
    the rendered word, and `internal/report`'s `listingLine` carries the byte-
    identical change in the same commit, because the two renderers diverging here
    is precisely the silent drift `tests/parity/` exists to catch.

    ⚠ THE BODY LABEL MOVED WITH IT — ONE CHANGE LATER, ON AN OPERATOR RULING, AND
    SAYING SO IS THE POINT RATHER THAN LEAVING A READER TO CHECK. This docstring
    read "the body's `tasks: ` LABEL still reads `task`" for exactly one change;
    both rendered words are `ref` now, so nothing on this text surface spells
    `task` any more. Two things still do, and they are different: the internal
    FIELD NAME (`RecalledEntry.tasks`, `SubsystemEntry.tasks`,
    `internal/report.RecalledEntry.Tasks`), which no reader sees; and
    `render_json`'s payload key `"tasks"`, which IS public but MACHINE-readable and
    is therefore out of scope by decision — both are recorded on `tasks` below.

    Conditional like the other four: measured, **0 of 120** live
    entries carry `tasks:`, so no row on the store today renders any differently
    than it did before this badge existed.
    """
    base = f"  {entry.ref.ljust(width)}  {entry.bullet_count:>3} nuance   {sensitivity_label(entry.sensitivity, entry.declared_sensitivity)}"
    badges: list[str] = []
    if entry.open_count:
        badges.append(f"🔴 {entry.open_count} OPEN")
    if entry.near_miss_count:
        badges.append(f"🔴 {entry.near_miss_count} NEAR-MISS")
    if entry.unverifiable_count:
        badges.append(f"⚠ {entry.unverifiable_count} UNVERIFIABLE")
    # The requirements pair sits after the nuance populations and before
    # `NO <heading>`, keeping `--validate`'s order for the bullet populations and
    # leaving the parser-never-reached badge where it was.
    #
    # 🔴 BOTH ARE CONDITIONAL, so an entry with no `## Requirements` renders
    # byte-identical to one that never heard of the section — which is what keeps
    # this additive for every entry in the store.
    #
    # ⚠ `MET` IS RENDERED, NOT ONLY `OPEN`, and that is deliberate rather than
    # symmetry for its own sake. A section showing only its open items reads as a
    # to-do list; the pair is what makes it a RECORD — "three asked for, two
    # delivered" is the sentence the operator asked to be able to read, and one
    # number cannot say it.
    if entry.requirements_open:
        badges.append(f"🔴 {entry.requirements_open} REQ OPEN")
    if entry.requirements_met:
        badges.append(f"✅ {entry.requirements_met} REQ MET")
    if entry.missing_sections:
        badges.append(
            "🔴 NO " + ", ".join(short_heading(h) for h in entry.missing_sections)
        )
    if entry.tasks:
        badges.append(f"🔗 {len(entry.tasks)} ref{'' if len(entry.tasks) == 1 else 's'}")
    if not badges:
        return base
    return base + "   " + "   ".join(badges)


def _render_listing(report: RecallReport) -> list[str]:
    """The index block, its ORDER stated, and its remainder counted.

    🔴 The single-page wording is unchanged from the pre-pagination default
    ("ALL N … none omitted"), because on every scope that fits in one page the
    claim is still exactly true and weakening it would train the reader to skim
    past the page notice on the day it is not. The multi-page wording shares no
    phrase with it.

    🔴 …AND IT IS WITHDRAWN THE MOMENT A REJECT EXISTS. "ALL N entries, none
    omitted" is a COMPLETENESS CLAIM, and it is simply false about a scope whose
    third file could not be indexed — the index really does omit it. The claim is
    a comment about the store like any other, and `claude/RULES.md` is explicit
    that a comment the implementation contradicts is a defect: so the clean
    branch keeps its exact historical bytes (nothing pinned to it moves) and a
    scope with rejects gets its OWN wording, sharing no phrase with either the
    clean or the paginated one.
    """
    width = max((len(e.ref) for e in report.listing), default=0)
    order = "newest-first by file mtime"
    shown = len(report.listing)
    n_bad = len(report.malformed)

    if report.page_is_past_the_end:
        # 🔴 NO ARITHMETIC ON A PAGE THAT DOES NOT EXIST. The first shipped
        # version computed the range unconditionally and printed
        # `entries 801–800 of 150 … (page 9 of 2)` — an inverted range and an
        # impossible page-of-page — directly above a correct guidance line. A
        # header is a claim like any other; it does not get to be wrong because
        # something below it is right.
        head = (
            f"INDEX ({RECALL_LABEL}) — no entries: page {report.listing_page} is past the "
            f"end of `{report.scope}/`, which holds {report.listing_total} in "
            f"{report.listing_pages} page{'' if report.listing_pages == 1 else 's'} "
            f"({order}):"
        )
    elif report.listing_pages <= 1 and n_bad:
        head = (
            f"INDEX ({RECALL_LABEL}) — the {shown} READABLE entr"
            f"{'y' if shown == 1 else 'ies'} in `{report.scope}/` ({order}). NOT complete: "
            f"{n_bad} further file{'' if n_bad == 1 else 's'} in this scope could not be "
            f"indexed and {'is' if n_bad == 1 else 'are'} named above, never here:"
        )
    elif report.listing_pages <= 1:
        head = (
            f"INDEX ({RECALL_LABEL}) — ALL {shown} entr"
            f"{'y' if shown == 1 else 'ies'} in `{report.scope}/`, "
            f"none omitted ({order}):"
        )
    else:
        first = report.listing_before_page + 1
        # The paginated header already declines to claim completeness, so a
        # reject only needs the count corrected: `listing_total` is READABLE
        # entries, and without this the reader would take it for the file count.
        rejects = (
            f", plus {n_bad} that could NOT be indexed (named above)" if n_bad else ""
        )
        head = (
            f"INDEX ({RECALL_LABEL}) — entries {first}–{first + shown - 1} "
            f"of {report.listing_total} in `{report.scope}/`{rejects}, {order} "
            f"(page {report.listing_page} of {report.listing_pages}):"
        )
    out = ["", head, *(listing_line(e, width) for e in report.listing)]

    if report.page_is_past_the_end:
        # Its own branch and its own words: a page past the end lists nothing,
        # and "nothing on this page" must not be readable as "nothing on record".
        out.append(
            f"  (PAGE {report.listing_page} IS PAST THE END — `{report.scope}/` holds "
            f"{report.listing_total} entr"
            f"{'y' if report.listing_total == 1 else 'ies'} across "
            f"{report.listing_pages} page"
            f"{'' if report.listing_pages == 1 else 's'} of {LISTING_PAGE_SIZE}. Nothing "
            f"was listed here and nothing is missing from the store; re-run with "
            f"`--page 1` … `--page {report.listing_pages}`.)"
        )
    elif report.listing_after_page:
        # 🔴 GATED ON WHAT COMES AFTER THIS PAGE, never on "this page is not the
        # whole index" — see `RecallReport.listing_after_page`. On the LAST page
        # this is 0 and nothing prints, because there is nothing left to announce
        # and a notice pointing at a page that does not exist is worse than none.
        remaining = report.listing_after_page
        nxt = report.listing_page + 1
        out.append(
            f"  (… {remaining} more entr{'y' if remaining == 1 else 'ies'} NOT LISTED on "
            f"this page — the index is capped at {LISTING_PAGE_SIZE} lines per page. "
            f"Nothing is hidden and nothing was filtered: `--page {nxt}`"
            f"{f' … `--page {report.listing_pages}`' if nxt < report.listing_pages else ''} "
            f"lists the rest, oldest last.)"
        )
    return out


def _validated_ref_to(ref_to: str | None) -> TaskRef | None:
    """The ONE place the reverse-lookup operand's shape is refused, for both readers.

    🔴 IT DELEGATES TO `parse_task_ref` RATHER THAN RE-SPELLING THE RULES. The operand is
    the SAME `<system>:<id>` grammar an entry's `refs:` item is, and a second parser for it
    would drift: a query the writer accepts but the reader refuses (or worse, the reverse)
    answers "no entries reference this" for a ref that is written in the store.

    🔴 THE SENTENCE IS PINNED AGAINST THE GO PORT'S. `report.validateRefTo` raises the same
    prefix over the same parser message, and `tests/conformance/` replays a malformed
    `?ref-to=` against both servers — so a reworded prefix here is a 400 body that differs
    between two implementations of one contract.
    """
    if ref_to is None:
        return None
    try:
        return parse_task_ref(ref_to)
    except TaskRefError as exc:
        raise ValueError(
            f"ref-to is not a well-formed `<system>:<id>` ref: {exc}"
        ) from exc


def _validated_tag(tag: str | None) -> str | None:
    """The ONE place a tag OPERAND is refused, and the ONE place it is canonicalised.

    `None` in is `None` out — "no filter was sent". Anything else must fold to a non-empty
    tag, and the returned value is the FOLDED one, which is what the filter compares and the
    header line prints.

    🔴 A REFUSAL RATHER THAN A NARROWING TO NOTHING, WHICH IS THE OPPOSITE CHOICE FROM
    `--ref`. `--ref ''` narrows and finds nothing, and that is right there: a ref names an
    ENTRY, and "no entry is called that" is a fact about the store worth reporting. A tag
    operand that folds away names NO CATEGORY AT ALL, so answering "no entry carries this"
    would be an empty result whose cause is invisible — indistinguishable from a tag nobody
    has used yet, when the real cause is that the query said nothing.

    🔴 VALIDATE AND CANONICALISE TOGETHER, IN ONE FUNCTION, BECAUSE SPLITTING THEM PUTS A
    TWO-STEP SEQUENCE AT EVERY CALL SITE. `cairn` and `server/server.py` both pass the operand
    as WRITTEN; a "validate then fold" pair spelled at each would be one predicate at two
    sites, wrong at the one that gets a new step first. `report.canonicalTag` is the Go
    spelling and it is called from inside `Recall`/`Search` for the same reason.

    ⚠ THE FOLD IS `normalize_ref`, THE SAME FUNCTION THE FILE's own tags went through — one
    call, because the operand is one tag. A second rule here is how a tag an operator can
    write becomes one they cannot ask for.

    ⚠ AND `""` IS REFUSED RATHER THAN TREATED AS ABSENT, which is why the parameter is
    `str | None` and not a bare `str`. A `?tag=` carrying nothing named no category, and
    folding first — or defaulting `""` to "no filter" — would answer 200 over the whole scope
    for it, the widening direction.

    🔴 THE SENTENCE IS PINNED AGAINST THE GO PORT'S `report.validateTag`, and it reaches the
    wire as a 400 body on `?tag=`.
    """
    if tag is None:
        return None
    folded = normalize_ref(tag) if isinstance(tag, str) else ""
    if not folded:
        raise ValueError(
            f"tag {tag!r} normalizes to the empty string — a tag must fold to at "
            f"least one of `[a-z0-9.-]`"
        )
    return folded


def _reach_clause(matched: int, *, narrowed_set_shown: bool, narrowed_further: bool) -> str:
    """The middle clause of BOTH filter headers: what the body below the line is about.

    THREE states, spelled once, because there are three and two of them were once one.

    🔴 THE THIRD STATE IS WHAT A SECOND FILTER MADE NECESSARY, AND ITS ABSENCE WOULD HAVE BEEN
    A FALSE SENTENCE RATHER THAN A MISSING ONE. With `--ref-to X --tag Y`, the `ref-to` line's
    numerator is the entries that reference X — which is correct and is NOT what the body
    renders, because the tag filter then removes some of them. "Everything below is about those
    N" was therefore false about the store in exactly the way four other shapes were measured
    false: a count of matched entries above a body reporting on fewer.

    🔴 `narrowed_set_shown` WINS OVER `narrowed_further`, AND THE ORDER IS LOAD-BEARING. When
    nothing below reports on a matched entry at all, "a further filter narrows those N again"
    would be true and useless — it invites the reader to look for a subset that is not on
    screen. The "NOTHING below" clause points at the sentence that explains the empty body.

    `internal/report.reachClause` is the Go spelling.
    """
    if not narrowed_set_shown:
        return "and NOTHING below is about them — the sentence below says why."
    if narrowed_further:
        return f"and a FURTHER filter below narrows those {matched} again."
    return f"and everything below is about those {matched}."


# The clause both filter headers END with, spelled ONCE.
#
# 🔴 A CONSTANT RATHER THAN A REPEATED LITERAL BECAUSE IT IS THE PART THAT MUST NOT DIVERGE.
# The counts and the verbs differ between the two lines by design; this sentence is the
# reader's instruction — "the rest were read and did not match", which is what stops a narrowed
# index reading as a truncated one — and a second spelling of it is the one that would go stale
# in whichever line was edited second.
NARROWING_CAVEAT = (
    " This is a NARROWING, not a truncation: the rest were read and did not match."
)


def _tag_line(
    tag: str, matched: int, total: int, label: str, *, narrowed_set_shown: bool
) -> str:
    """The ONE spelling of the category-filter header, shared by both renderers.

    🔴 A SEPARATE FUNCTION AND NOT A PARAMETERISED `_ref_to_line`, EVEN THOUGH BOTH OPERANDS
    ARE NOW SCALAR. The two sentences differ in every clause but the trailing caveat — "carry
    it" against "reference it", and two different pairs of counts — so folding them would put a
    conditional on each clause to save one shared word. That caveat is the half neither renderer
    may spell twice, and it is spelled once in `NARROWING_CAVEAT`.

    ⚠ IT TAKES NO `narrowed_further`, AND THAT IS A PROPERTY OF THE FILTER ORDER RATHER THAN AN
    OMISSION: the tag filter runs LAST at both call sites, so nothing narrows its result again.
    A third filter added after it would have to pass one — and would also have to re-derive
    this line's denominator, which is the set the tag filter SAW rather than the readable total.

    ⚠ `label` CARRIES ITS OWN TRAILING `/` — the trap `_ref_to_line`'s docstring records,
    inherited here because this function is copied from it.
    """
    return (
        f"  tag: `{tag}` — {matched} of {total} entr"
        f"{'y' if total == 1 else 'ies'} in `{label}` carry it, "
        f"{_reach_clause(matched, narrowed_set_shown=narrowed_set_shown, narrowed_further=False)}"
        f"{NARROWING_CAVEAT}"
    )


def _emptying_filters(ref_to_skipped: int, tag_skipped: int) -> tuple[str, str]:
    """The filter or filters that drove the searched set to zero, plus the agreeing possessive.

    🔴 TWO RETURN VALUES BECAUSE ONE SENTENCE NAMES THE SUBJECT TWICE. "the `ref-to` filter
    removed every readable entry, so this zero is the FILTER's" — the second half is a
    possessive of the first, and picking one plural form per site is how the two stop agreeing.

    ⚠ IT IS NEVER CALLED WITH BOTH ZERO. Its one call site is inside a branch guarded on
    `ref_to_skipped > 0 or tag_skipped > 0`, so the fall-through is unreachable — but it returns
    the `ref-to` wording rather than raising, because the alternative to an unreachable branch
    is a renderer that can fail on a report it was handed. Stated so nobody reads the
    fall-through as a claim that `ref-to` is the default filter.

    `internal/report.emptyingFilters` is the Go spelling, and `tests/parity/` diffs the bytes.
    """
    if ref_to_skipped > 0 and tag_skipped > 0:
        return "the `ref-to` and `tag` filters", "the FILTERS'"
    if tag_skipped > 0:
        return "the `tag` filter", "the FILTER's"
    return "the `ref-to` filter", "the FILTER's"


def _ref_to_line(
    ref_to: str,
    matched: int,
    total: int,
    label: str,
    *,
    narrowed_set_shown: bool,
    narrowed_further: bool = False,
) -> str:
    """The ONE spelling of the reverse-lookup header, shared by both renderers.

    🔴 ONE FUNCTION, TWO CALLERS, BECAUSE THE TWO REPORT TYPES HAVE DIFFERENT COUNTS TO
    PUT IN IT AND THE SENTENCE MUST NOT DIFFER. `caveat_text`'s own header gives the rule:
    a module with two report types spells a shared sentence once, or it is wrong in one of
    them. `internal/report.refToLine` is the Go spelling, and `tests/parity/harness.py`
    diffs the two clients' bytes.

    `matched`/`total` are the narrowed and pre-filter counts. `narrowed_set_shown` says
    whether the report BELOW this line reports on the matched entries — a PREDICATE each
    caller answers for itself, and deliberately not a status test: `RecallReport` passes
    `renders_narrowed_set` (see its docstring for the statuses a status test got wrong) and
    the search report passes a constant, justified at its own call site. It is a separate
    clause rather than a second function so the COUNT half stays spelled once.

    ⚠ `label` IS A FORMED LABEL AND CARRIES ITS OWN TRAILING `/`, so do not append one here.
    The draft passed `SearchReport.scope`, which on a store-wide search is the literal
    `(all scopes)`, plus a `/` — printing `` `(all scopes)/` ``. `SearchReport.label` is what
    it passes now: `scope_label` over the SEARCHED scopes, which cannot return `(all scopes)`
    at all (it returns `(no scope)` for an empty set, and a `/`-suffixed join otherwise).
    Caught by reading the regenerated golden, not by a test: both implementations agreed, and
    both were wrong.
    """
    reach = _reach_clause(
        matched, narrowed_set_shown=narrowed_set_shown, narrowed_further=narrowed_further
    )
    return (
        f"  ref-to: `{ref_to}` — {matched} of {total} entr"
        f"{'y' if total == 1 else 'ies'} in `{label}` reference it, {reach}"
        f"{NARROWING_CAVEAT}"
    )


def render_text(
    report: RecallReport,
    *,
    extra_header: Sequence[str] = (),
    instance: str | None = None,
) -> str:
    """The agent-facing recall block.

    Deterministic in the REPORT with ONE exception: `store_host_line` reads THIS
    machine's identity, which is the entire point of it — a recall that does not
    name whose disk it read states one host's store as the fleet's.

    `extra_header` is how the CLI puts the read store's snapshot stamp WITH the
    `store:`/`store host:` lines instead of somewhere else on the page. It is a
    keyword with an empty default because the subsystem-store POD calls this
    function with a report only, and its `/data` has no stamp to print.

    `instance` is the alias of the cairn instance this report was read from, and
    it is `None` — the single-instance case — for the pod and for every host
    configured with one instance. Passing it adds a clause to the caveat saying
    which instance was consulted; see `entry_shape.store_caveat`.
    """
    out: list[str] = [
        f"subsystem-recall: status={report.status} scope={report.scope}",
        f"  store: {report.store_root}",
        store_host_line(instance=instance),
        *extra_header,
        f"  caveat: {report.caveat}",
    ]

    # 🔴 THE NARROWING ANNOUNCES ITSELF, BECAUSE EVERY COUNT BELOW IT IS ABOUT THE
    # NARROWED SET. Without this line a `--ref-to` digest is byte-indistinguishable from a
    # digest of a scope that happens to hold exactly those entries — the reader would take
    # a filtered index for the whole one. It carries BOTH numbers so what the filter
    # removed stays visible.
    #
    # ⚠ EMITTED ONLY WHEN THE FILTER WAS SENT, so no existing golden moves. `ref_to` is
    # `None` on every request that does not carry the parameter.
    if report.ref_to is not None:
        # 🔴 THE NUMERATOR IS `ref_to_matched` AND NOTHING DERIVED. Two earlier spellings
        # printed a false sentence about the store — see that field's docstring for both
        # directions of the pendulum.
        #
        # ⚠ `narrowed_set_shown` IS THE RENDER DECISION, NOT A STATUS. "Everything below is
        # about those N" is a claim about the BODY, so the body is what has to answer it:
        # `renders_narrowed_set` is True exactly when a matched entry is printed below, and
        # its own docstring records the three shapes a status-name derivation got wrong plus
        # the digest-mode case that rules out the obvious second guess.
        #
        # ⚠ `narrowed_further` IS WHAT THE TAG FILTER MADE NECESSARY. `ref_to_matched` is the
        # entries that carry the ref; with a `--tag` also in force the body below reports on a
        # SUBSET of them, so "everything below is about those N" would be false — see
        # `_reach_clause`. And it is "the tag filter REMOVED something", not "a tag filter was
        # sent": a `--tag` that kept every entry narrows nothing, so the original clause is
        # still true there and saying otherwise sends a reader looking for a subset that is the
        # whole set.
        out.append(
            _ref_to_line(
                report.ref_to,
                report.ref_to_matched,
                report.ref_to_scope_total,
                f"{report.scope}/",
                narrowed_set_shown=report.renders_narrowed_set,
                narrowed_further=report.tag_scope_total > report.tag_matched,
            )
        )

    # The CATEGORY narrowing announces itself for the same reason, and AFTER the `ref-to` line
    # because that is the order the two filters ran in — so a report carrying both reads as the
    # composition it is rather than as two independent claims about one index.
    #
    # ⚠ EMITTED ONLY WHEN THE FILTER WAS SENT, so no existing golden moves: `tag` is empty on
    # every request that does not carry the parameter.
    if report.tag:
        out.append(
            _tag_line(
                report.tag,
                report.tag_matched,
                report.tag_scope_total,
                f"{report.scope}/",
                narrowed_set_shown=report.renders_narrowed_set,
            )
        )

    # 🔴 BEFORE EVERY STATUS BRANCH, INCLUDING THE ONES THAT RETURN IMMEDIATELY.
    # A reject reported only on the paths somebody remembered is a reject that
    # will be missed on the path they did not — and `scope-absent`, the most
    # common status in most repos, is precisely where a store-wide defect would
    # otherwise never be mentioned at all.
    out.extend(render_malformed(report.malformed, report.malformed_elsewhere, f"{report.scope}/"))

    if report.status == "scope-unreadable":
        out.append("")
        out.append(unreadable_summary(f"{report.scope}/", report.malformed))
        return "\n".join(out)

    if report.status == "scope-absent":
        out.append("")
        out.append(
            f"NOTHING RECORDED YET ON THIS HOST — {store_host()}'s store has no "
            f"`{report.scope}/` directory. This is the ordinary case in most repos "
            f"(the store is young and its scopes are few), NOT an error and NOT an "
            f"absence of drift: nothing was checked, so nothing can be concluded from "
            f"it. Carry on with the resume and say plainly that the index had nothing "
            f"for this repo ON THIS MACHINE."
        )
        # 🔴 THE SECOND SENTENCE IS THE ONE THE OLD WORDING LACKED. "The store"
        # reads as one thing; the CACHES are two. Measured, before the
        # cutover: seven scopes existed only on the laptop and ten only on the
        # workbench, so "not recorded" is routinely false of the fleet while true
        # of the disk that was read. Post-cutover the caches converge via the pod
        # — but only when each syncs, so a read still sees one disk at one time.
        out.append(
            f"  NOT A FACT ABOUT THE FLEET — {store_caveat(instance)}. The other host syncs "
            f"the SAME hosted store through its own cache, and may already hold "
            f"`{report.scope}/` where this one has not synced it yet."
        )
        out.append(
            f"  scopes THIS HOST's store holds: "
            f"{', '.join(report.known_scopes) or '(none)'}"
        )
        return "\n".join(out)

    if report.status == "scope-empty":
        out.append("")
        out.append(
            f"NOTHING RECORDED YET — `{report.scope}/` exists but holds no entries. Same "
            f"conclusion as an absent scope and a DIFFERENT mechanism: the directory was "
            f"made and never filled, or was pruned to nothing. Not an error."
        )
        return "\n".join(out)

    if report.status == "ref-ambiguous":
        out.append("")
        out.append(
            f"AMBIGUOUS REF `{report.ref}` — it names more than one entry, so nothing was "
            f"surfaced. The resolver never picks; neither does this. Candidates: "
            f"{', '.join(report.candidates)}. Re-run naming one of them."
        )
        return "\n".join(out)

    if report.status == "ref-to-absent":
        out.append("")
        # 🔴 TWO SENTENCES FOR ONE STATUS, BECAUSE `ref-to-absent` IS REACHED TWO WAYS AND
        # THE SECOND ONE IS NOT AN ABSENCE AT ALL. Nothing in the scope matched (the
        # filter's own zero) and "the `--ref` operand is not among the entries that DID
        # match" are different facts, and the second shipped wearing the first's words:
        # both clients printed "NO ENTRY REFERENCES <X>" over a scope where other entries
        # carried it, because the numerator had been forced to 0 whenever the status was
        # this one. The discriminator is `ref_to_matched`.
        if report.ref_to_matched > 0:
            # The `--ref` operand loaded fine, so the malformed rows cannot make THIS claim
            # wrong; they can only understate the count of entries that DO carry the ref,
            # which is what the qualification names.
            n_bad = len(report.malformed)
            also = (
                f" ⚠ AND THAT COUNT IS ONLY OF ENTRIES THAT LOADED: {n_bad} entry file"
                f"{'' if n_bad == 1 else 's'} in this scope could not be indexed (listed "
                f"above), and an entry that never loaded carries no refs a filter can see."
                if n_bad
                else ""
            )
            out.append(
                f"`{report.ref}` DOES NOT REFERENCE `{report.ref_to}` — it was read and "
                f"carries no such ref, so the two narrowings compose to nothing and no body "
                f"is printed. {report.ref_to_matched} of the {report.ref_to_scope_total} "
                f"entr{'y' if report.ref_to_scope_total == 1 else 'ies'} in "
                f"`{report.scope}/` {'DOES' if report.ref_to_matched == 1 else 'DO'} "
                f"reference it — re-run without `--ref` to see "
                f"{'it' if report.ref_to_matched == 1 else 'them'}. The comparison is on "
                f"THIS SCOPE's `refs:` keys and the id half is matched byte-for-byte, so a "
                f"different spelling of it would not be found here either.{also}"
            )
            return "\n".join(out)
        # 🔴 IT SAYS WHAT WAS LOOKED AT AND WHAT WAS NOT, because a reverse lookup's
        # zero is the most misreadable answer this reader produces: "no entry
        # references X" and "X is not a thing anybody tracks" are different facts, and
        # only the first is in evidence. The malformed rows are already above; this
        # sentence is what stops the reader concluding from them in the wrong
        # direction, exactly as `ref-absent`'s does.
        extra = (
            f" ⚠ BUT {len(report.malformed)} entry file"
            f"{'' if len(report.malformed) == 1 else 's'} in this scope could not be "
            f"indexed (listed above), and an entry that never loaded carries no refs a "
            f"filter can see — one of them may reference this. Check those before "
            f"concluding nothing does."
            if report.malformed
            else ""
        )
        out.append(
            f"NO ENTRY REFERENCES `{report.ref_to}` — the {report.ref_to_scope_total} entr"
            f"{'y' if report.ref_to_scope_total == 1 else 'ies'} in `{report.scope}/` were "
            f"read and none of them carries that ref. This is a fact about THIS SCOPE's "
            f"`refs:` keys and NOT about whether the reference exists: an entry may point "
            f"at it under a different spelling of the id half, which is compared "
            f"byte-for-byte.{extra}"
        )
        return "\n".join(out)

    if report.status == "tag-absent":
        out.append("")
        # 🔴 TWO SENTENCES FOR ONE STATUS, FOR THE REASON `ref-to-absent` ABOVE HAS TWO, AND
        # WRITTEN THAT WAY FROM THE START RATHER THAN AFTER A ROUND MEASURED IT WRONG. This
        # status is reached TWO ways — nothing in the set carried the tags (the filter's own
        # zero), and "the `--ref` operand is not among the entries that DID" — and a status
        # cannot tell them apart. The discriminator is `tag_matched`, from the filter's own
        # result.
        if report.tag_matched > 0:
            # The `--ref` operand loaded fine, so the malformed rows cannot make THIS claim
            # wrong; they can only understate the count of entries that DO carry the tag.
            n_bad = len(report.malformed)
            also = (
                f" ⚠ AND THAT COUNT IS ONLY OF ENTRIES THAT LOADED: {n_bad} entry file"
                f"{'' if n_bad == 1 else 's'} in this scope could not be indexed (listed "
                f"above), and an entry that never loaded carries no tags a filter can see."
                if n_bad
                else ""
            )
            out.append(
                f"`{report.ref}` IS NOT TAGGED `{report.tag}` — it was read "
                f"and does not carry it, so the two narrowings "
                f"compose to nothing and no body is printed. {report.tag_matched} of the "
                f"{report.tag_scope_total} "
                f"entr{'y' if report.tag_scope_total == 1 else 'ies'} in `{report.scope}/` "
                f"{'DOES' if report.tag_matched == 1 else 'DO'} — re-run without `--ref` to "
                f"see {'it' if report.tag_matched == 1 else 'them'}.{also}"
            )
            return "\n".join(out)
        # 🔴 IT SAYS WHAT WAS LOOKED AT AND WHAT WAS NOT, because a category filter's zero is
        # as misreadable as a reverse lookup's: "no entry is tagged X" and "X is not a category
        # anybody uses" are different facts, and only the first is in evidence. The vocabulary
        # is OPEN, so a typo in either the file or the query makes a silently separate category
        # — that is what the last clause names.
        extra = (
            f" ⚠ BUT {len(report.malformed)} entry file"
            f"{'' if len(report.malformed) == 1 else 's'} in this scope could not be "
            f"indexed (listed above), and an entry that never loaded carries no tags a "
            f"filter can see — one of them may be tagged this. Check those before "
            f"concluding nothing is."
            if report.malformed
            else ""
        )
        out.append(
            f"NO ENTRY IS TAGGED `{report.tag}` — the {report.tag_scope_total} "
            f"entr{'y' if report.tag_scope_total == 1 else 'ies'} in `{report.scope}/` were "
            f"read and none of them carries it. Both sides of "
            f"the comparison are FOLDED, so a differently-cased spelling would have been "
            f"found — but the tag vocabulary is OPEN and nothing declares it, so a typo in "
            f"the file or in this query is a category of one that no check can see.{extra}"
        )
        return "\n".join(out)

    if report.status == "ref-absent":
        out.append("")
        # 🔴 THE ABSENCE IS QUALIFIED WHEN THE SCOPE HAS REJECTS. "Nothing
        # recorded under that name yet" is a claim about the STORE, and it is
        # false if the entry exists in a file the loader refused. The malformed
        # rows are already above; this sentence is what stops the reader
        # concluding from them in the wrong direction.
        extra = (
            f" ⚠ BUT {len(report.malformed)} entry file"
            f"{'' if len(report.malformed) == 1 else 's'} in this scope could not be indexed "
            f"(listed above) — this ref may name one of them, in which case it IS recorded "
            f"and merely invisible. Check those before concluding the name is new; re-run "
            f"without `--ref` to see what IS readable."
            if report.malformed
            else " Nothing recorded under that name yet; re-run without `--ref` to see what "
            "IS recorded."
        )
        out.append(
            f"NO SUCH ENTRY — `{report.ref}` resolves to nothing in `{report.scope}/`, "
            f"which holds {report.total_in_scope} entr"
            f"{'y' if report.total_in_scope == 1 else 'ies'}.{extra}"
        )
        return "\n".join(out)

    # `listing_total`, not `listing`: a `--page` past the end has an EMPTY slice
    # and still has an index block to render — the notice saying so is the whole
    # point. Gating on the slice would make that page render as `full` mode.
    if report.listing_total:
        out.extend(_render_listing(report))

    if not report.entries:
        # `list` mode. LOUD about what it did not do: an index with no bodies is
        # not an empty scope, and the two must not read the same.
        #
        # 🔴 IT DESCRIBES THE PAGE, NOT THE SCOPE. This line originally asserted
        # "the N entries above are the complete index", with N the SCOPE total —
        # false on every paginated page (100 were above, not 150) and flatly
        # self-contradictory past the end (zero were). Same class as a false
        # truncation notice: it claims a completeness the page does not have.
        # `total_in_scope` still appears, because "this is not an empty scope" is
        # the other half of the sentence and has to stay true.
        shown = len(report.listing)
        if report.page_is_past_the_end:
            what = (
                f"NO entries were listed — see the page notice above. `{report.scope}/` "
                f"holds {report.total_in_scope}"
            )
        elif report.listing_pages > 1:
            what = (
                f"The {shown} entr{'y' if shown == 1 else 'ies'} above "
                f"{'is' if shown == 1 else 'are'} page {report.listing_page} of "
                f"{report.listing_pages} of the index for `{report.scope}/`, which holds "
                f"{report.total_in_scope} in all"
            )
        elif report.malformed:
            # 🔴 The same withdrawn completeness claim as the header above, in the
            # other place it is spelled. "the complete index" is false when a file
            # in the scope could not be indexed, and this line is the one a reader
            # quotes when they say "the store has N of them".
            n_bad = len(report.malformed)
            what = (
                f"The {report.total_in_scope} entr"
                f"{'y above is' if report.total_in_scope == 1 else 'ies above are'} every "
                f"READABLE entry in `{report.scope}/` — NOT the complete index: {n_bad} "
                f"further file{'' if n_bad == 1 else 's'} could not be indexed"
            )
        else:
            what = (
                f"The {report.total_in_scope} entr"
                f"{'y above is' if report.total_in_scope == 1 else 'ies above are'} the "
                f"complete index for `{report.scope}/`"
            )
        out.append("")
        out.append(
            f"NO ENTRY BODIES WERE PRINTED (--list). {what} — this is not an empty scope. "
            f"Run `--ref <name>` for one entry's `{WHAT_HEADING}` + `{POINTERS_HEADING}` "
            f"+ `{NUANCE_HEADING}`."
        )
        return "\n".join(out)

    n = len(report.entries)
    out.append("")
    if report.featured_basis is not None:
        # 🔴 THE BASIS, NEVER THE PICK ALONE. An entry featured without saying
        # which selector chose it is indistinguishable from an entry the tool
        # thinks is IMPORTANT, and that is a claim the store cannot support.
        out.append(
            f"FEATURED IN FULL ({RECALL_LABEL}) — 1 of {report.total_in_scope}, "
            f"{report.featured_basis}:"
        )
    else:
        out.append(
            f"RECALL ({RECALL_LABEL}) — {n} of {report.total_in_scope} entr"
            f"{'y' if report.total_in_scope == 1 else 'ies'} in `{report.scope}/`:"
        )
    for e in report.entries:
        out.append("")
        out.append(
            f"  ### {e.ref}  ({report.scope}/{e.filename}, "
            f"sensitivity={sensitivity_label(e.sensitivity, e.declared_sensitivity)})"
        )
        if e.tasks:
            # 🔴 THE REFS THEMSELVES, AND ONLY IN A BODY. The index row carries a
            # COUNT (`🔗 N refs`) because it is one line per entry and a ref is
            # up to 36 characters; the body is already many lines, so printing
            # them here costs nothing the reader has not already agreed to pay.
            # Rendered from `e.tasks` — the loader's validated refs — so this can
            # never show a ref that `--validate` would reject.
            #
            # Above the sections deliberately: "which ref does this answer" is
            # identity, like the ref and the sensitivity on the line above, not
            # content.
            #
            # 🔴 THE LABEL READS `refs:`, WHICH IS THE FRONT-MATTER KEY, AND THE
            # HISTORY IS KEPT RATHER THAN DELETED BECAUSE THE PREVIOUS STATE WAS A
            # RECORDED DEFERRAL AND NOT AN OVERSIGHT. It read `tasks:` from before
            # `tasks:` folded into `refs:` until an operator ruled on it; the comment
            # here said the re-base argument was SPENT (`listing_line`'s badge had
            # already moved the goldens, the reader fixture and the parity byte diffs)
            # and that what remained was a decision, since a body label is a different
            # public string from an index badge. 🔴 THAT CLOSING CONDITION IS NOW MET:
            # the ruling was given, and this line, `internal/report`'s
            # `RecallReport.RenderText`, the fixture and the corpus moved together. The
            # state it warned about — the badge saying `ref` while this said `task`,
            # two words for one field on one screen — lasted exactly one change.
            #
            # ⚠ THE ACCEPTED INPUT SPELLINGS ARE UNTOUCHED. `tasks:` and `task:` are
            # still read on the way in, permanently, by operator decision; only the
            # rendered word moved. So a reader who writes `tasks:` still sees `refs:`
            # here, and that is correct rather than a mismatch — the parser keeps no
            # record of which key an entry used, which is also why the browser surface
            # names both spellings. These are the bytes the Go renderer is diffed
            # against, so the two labels move together or not at all.
            #
            # ⚠ AND THE JSON PAYLOAD'S KEY IS STILL `tasks`, DELIBERATELY. `render_json`
            # is machine-readable: renaming a key there breaks a consumer's `[]` check
            # silently, where renaming a human-read label breaks nobody. It also has no
            # Go counterpart, so it is not part of the byte-identity contract this line
            # is under. Out of scope by decision, not by omission.
            out.append(f"    refs: {', '.join(e.tasks)}")
        if e.tags:
            # Identity, like the refs line above and for the same reason: "what category is
            # this" is not content.
            #
            # ⚠ THIS COMMENT USED TO SAY THE LABEL ABOVE DISAGREED WITH ITS KEY WHERE THIS
            # ONE AGREES, AND THAT DISTINCTION IS GONE: both labels now spell their own
            # front-matter key. It is rewritten rather than deleted because the sentence it
            # replaces was a true claim about a state that no longer exists, and a reader who
            # remembers it has to be told where it went. What survives is the narrower fact:
            # `tags:` never had an older spelling, so there was never anything here to defer
            # — the label above had one and its deferral is now closed.
            out.append(f"    tags: {', '.join(e.tags)}")
        for heading in SURFACED_HEADINGS:
            body = e.sections.get(heading)
            if body:
                out.append(f"    {heading}")
                for line in body.splitlines():
                    out.append(f"      {line}")
        if not e.sections.get(WHAT_HEADING):
            # 🔴 SAID, NOT LEFT BLANK — and BODY-ONLY, never on the index row.
            # Absent and present-but-empty are folded together on purpose: both
            # render as nothing above, and the reader's question ("what IS this
            # thing?") is unanswered either way. It is not routed through
            # `missing_sections` because that field drives the index-row badge
            # and the caveat clause explaining it, which are per-entry costs
            # this decision deliberately leaves at zero.
            #
            # 🔴 IT CLAIMS A PARSE, NEVER A FACT ABOUT THE ENTRY. The notice used
            # to read "this entry never says what the subsystem IS" — which the
            # extractor cannot know. A heading the parser does not match parses to
            # nothing and produced that same sentence while the answer sat on
            # disk, and `entry_shape.SHAPE_HEADINGS` deliberately excludes
            # this heading so `--validate` says nothing either. The sibling
            # `🔴 NO <heading>` badge draws exactly this line ("0 BY PARSE FAILURE
            # and not by measurement"); this notice now draws it too, and names
            # causes the reader can act on.
            #
            # 🔴 THE CAUSE LIST IS EXPLICITLY NON-EXHAUSTIVE ("among others"), and
            # it has to be: `_heading_blocks` matches at column 0 and skips fenced
            # regions, so a RENAME (`## What It Is`, `## What it is:`,
            # `### What it is`), an INDENTED heading and one inside a ``` FENCE all
            # reach this same branch — and only the first is literally a "rename".
            # An enumeration that reads as closed is a narrower claim than the
            # branch, which is how the previous wording ("absent, empty, or the
            # heading was renamed") left two real causes unnamed.
            #
            # 🔴 THE PREFIX THIS SHARES WITH `service_recon`'s TWIN NOTICE IS
            # QUOTED IN `claude/skills/analyze-service/SKILL.md` step 2, which
            # tells the agent to relay it AS WRITTEN. Pinned to this string by
            # `test_service_recon.py::TestTheSkillQuotesTheDegradeNotice`, which
            # DERIVES the expected text from both renderers — so rewording either
            # notice goes red naming the doc.
            out.append(
                f"    (no parsable `{WHAT_HEADING}` — absent, empty, or not parsed as a "
                f"heading [renamed, indented, fenced, among others], so this read cannot "
                f"say what the subsystem IS; re-derive it live)"
            )
        if e.is_bare:
            # 🔴 Said, not left blank. An entry that exists with nothing under
            # either COUNTED heading is a real state (the writer's own template
            # ships a stub), and printing nothing for it is indistinguishable
            # from an extractor that failed to find the sections.
            out.append(
                f"    (no `{POINTERS_HEADING}` or `{NUANCE_HEADING}` content — the entry "
                f"exists but has not been filled in)"
            )
        elif e.missing_sections:
            out.append(f"    (no {', '.join(f'`{h}`' for h in e.missing_sections)} section)")

    if report.omitted and report.listing_total:
        # The digest's own words. It is NOT the `--limit` truncation below: every
        # one of these entries IS listed above and is one `--ref` away, so
        # borrowing the truncation wording would train the reader to read a
        # complete index as a lossy one — and then to ignore the real notice.
        out.append("")
        out.append(
            f"… {report.omitted} further entr{'y' if report.omitted == 1 else 'ies'} in "
            f"`{report.scope}/` LISTED ABOVE but NOT shown in full. Nothing is hidden: this "
            f"is the default digest, not a judgement about relevance. `--ref <name>` prints "
            f"any one of them in full; `--limit {report.total_in_scope}` prints them all."
        )
    elif report.omitted:
        out.append("")
        out.append(
            f"… {report.omitted} more entr{'y' if report.omitted == 1 else 'ies'} in "
            f"`{report.scope}/` NOT shown (--limit {report.limit}). This is a display cap, "
            f"not a judgement about relevance — raise it to see the rest."
        )
    return "\n".join(out)


def _malformed_json(
    malformed: Sequence[MalformedEntry], elsewhere: Sequence[MalformedEntry]
) -> dict:
    """The reject rows + counts, shaped ONCE for both report types.

    Both JSON payloads carry the same three keys with the same meanings; writing
    the dict twice is how one of them ends up with a count and no rows.
    """
    return {
        "malformed": [
            {"scope": m.scope, "file": m.filename, "reason": m.reason, "line": m.line}
            for m in malformed
        ],
        "malformed_elsewhere": sorted({m.scope for m in elsewhere}),
        "malformed_elsewhere_count": len(elsewhere),
    }


def report_json(report: RecallReport) -> dict:
    return {
        "status": report.status,
        "scope": report.scope,
        "store_root": report.store_root,
        # WHOSE disk. The path is identical on both machines and the contents are
        # not, so `store_root` alone cannot tell two hosts' reports apart.
        "store_host": store_host(),
        "label": RECALL_LABEL,
        "caveat": report.caveat,
        "ref": report.ref,
        # ⚠ ALL THREE REVERSE-LOOKUP FIELDS, NOT JUST THE OPERAND. A consumer holding only
        # `ref_to` could not tell how much the filter removed; `total_in_scope` is the
        # NARROWED count once a filter ran; and `ref_to_matched` is the rendered line's own
        # numerator, which is NOT derivable from the other two on `ref-to-absent` — that
        # status is reached both by "nothing matched" and by "the `--ref` operand is not one
        # of the entries that did". Carrying it is what keeps this payload saying the same
        # thing the rendered text says.
        "ref_to": report.ref_to,
        "ref_to_scope_total": report.ref_to_scope_total,
        "ref_to_matched": report.ref_to_matched,
        # ⚠ ALL THREE TAG FIELDS, for the reason the three ref-to ones are all carried: a
        # consumer holding only `tag` could not tell how much the filter removed, and
        # `tag_matched` is the rendered line's own numerator, which is NOT derivable from the
        # other two on `tag-absent` — that status is reached both by "nothing matched" and by
        # "the `--ref` operand is not one of the entries that did".
        "tag": report.tag,
        "tag_scope_total": report.tag_scope_total,
        "tag_matched": report.tag_matched,
        "candidates": list(report.candidates),
        "limit": report.limit,
        "mode": report.mode,
        "total_in_scope": report.total_in_scope,
        "omitted": report.omitted,
        "known_scopes": list(report.known_scopes),
        "featured_basis": report.featured_basis,
        # 🔴 The rejects travel in the JSON too, as ROWS and not just a count. A
        # consumer reading `listing` has no other way to tell a complete index
        # from one three files short, and a count alone cannot be acted on.
        **_malformed_json(report.malformed, report.malformed_elsewhere),
        # The pagination facts travel WITH the page, or a JSON consumer reading
        # `listing` has no way to tell a complete index from page 1 of 3.
        "listing_total": report.listing_total,
        "listing_page": report.listing_page,
        "listing_pages": report.listing_pages,
        "listing_page_size": LISTING_PAGE_SIZE,
        "listing_order": "mtime-desc,ref-asc",
        # The index, as ROWS — ref, size signal, sensitivity. No `sections`:
        # a JSON consumer that wanted every body can ask for `--limit <n>`, and
        # putting them here would rebuild the dump the digest exists to avoid.
        "listing": [
            {
                "ref": e.ref,
                "file": e.filename,
                "sensitivity": e.sensitivity,
                "declared_sensitivity": e.declared_sensitivity,
                "nuance_bullets": e.bullet_count,
            }
            for e in report.listing
        ],
        "entries": [
            {
                "ref": e.ref,
                "file": e.filename,
                "sensitivity": e.sensitivity,
                "declared_sensitivity": e.declared_sensitivity,
                "sections": dict(e.sections),
                "missing_sections": list(e.missing_sections),
                "is_bare": e.is_bare,
                # On `entries` (which carry bodies) and NOT on `listing`, matching
                # where the text surface puts them: the row gets a count, the body
                # gets the refs. `[]` for an entry with none, never omitted — a
                # consumer branching on presence would read a missing key as "this
                # reader is too old to know about tasks", which is a different fact.
                "tasks": list(e.tasks),
                "tags": list(e.tags),
            }
            for e in report.entries
        ],
    }


# --- Search: the scorer --------------------------------------------------------
#
# 🔴 STDLIB ONLY, AND THAT IS A MEASUREMENT. The store is ~81 KB across 30 files;
# a `re`-based scan of all of it is single-digit milliseconds, so an external
# matcher would buy no speed while costing a binary that is not guaranteed on
# PATH and an output format nobody pinned. This module shells out to NOTHING and
# that property is asserted, not asserted-about.

_TOKEN = re.compile(r"[a-z0-9]+")


def tokenize(text: str) -> tuple[str, ...]:
    """Lowercase alphanumeric runs, in order. THE one tokenizer.

    Punctuation is a separator, so `rate-limit`, `rate_limit` and `rate limit` all
    tokenize identically — which is why the compound handling below only has to
    solve the CONCATENATED spelling (`ratelimit`) and not four punctuation
    variants.
    """
    return tuple(_TOKEN.findall(text.lower()))


# Punctuation that ENDS a compound instead of spelling one, so `candidate_tokens`
# must not join across it. `-`, `_` and a plain space are deliberately ABSENT:
# those are the three ways the store actually writes one compound term, and
# `tokenize` already folds them together.
#
# 🔴 A `.` counts ONLY at a sentence end — followed by whitespace or end-of-text.
# A dotted identifier (`activity.events`, `nginx.conf`, a dotted config key) is a
# single term whose halves must keep joining, and a rule that broke on every `.`
# would silently stop reaching them.
_CLAUSE_BREAK = re.compile(r"[,;:!?()\[\]]|\.(?=\s|$)")


def candidate_tokens(text: str) -> tuple[str, ...]:
    """Every token a unit's TEXT offers as a match candidate: its own tokens,
    PLUS every adjacent pair joined WITHIN one clause.

    🔴 THE JOIN IS THE COMPOUND-TERM FIX, and it is deliberate rather than a
    lowered cutoff. `ratelimit` never clears a fuzzy threshold against `rate` or
    `limit` — it is not a typo of either — so the corpus side grows the
    concatenation and the match becomes EXACT (1.00) instead of
    fuzzy-and-arguable. The other direction (`rate limit` searched against a
    corpus that writes `ratelimit`) is covered by `pair_strength`'s prefix and
    substring rungs.

    🔴 IT TAKES TEXT AND NOT TOKENS, because the clause boundary is exactly the
    information tokenizing throws away — and the join is unsafe without it. A
    bullet of the shape "drain that node, port-forward the socket" joins
    `node`+`port` across the comma and scores a PERFECT 1.00 for `nodeport` in an
    entry that never says the word — two facts glued into a term neither of them
    spells. A 1.00 with no evidence behind it is the worst shape this scorer can
    emit, because it out-ranks every genuine match on the page, so the clause
    boundary is a hard stop. Taking text also removes the bypass: a caller cannot
    tokenize first and lose the guard, because there is nothing to pass but the
    text. The trade is measured in `TestCandidateJoinStopsAtAClause` — every
    spelling of one compound still joins.

    Adjacent PAIRS only. Triples were not added: nothing in the corpus or the
    fixture set needs them, and each extra join widens the false-match surface.

    Plain tokens first, then the joins — `score_unit` scans in order and stops at
    1.00, so this keeps a whole-token exact hit cheaper than a joined one.
    """
    plain: list[str] = []
    joined: list[str] = []
    for clause in _CLAUSE_BREAK.split(text):
        toks = tokenize(clause)
        plain.extend(toks)
        joined.extend(a + b for a, b in zip(toks, toks[1:]))
    return tuple(plain) + tuple(joined)


def pair_strength(q: str, t: str) -> float:
    """How strongly one query token `q` matches one candidate token `t`, in [0, 1].

    The ladder, each rung strictly below the one above so an exact match always
    wins and a weak hit PRINTS as weak:

        1.00  identical
        0.92  the candidate EXTENDS the query   (`postgres` → `postgresql`)
        0.85  the candidate CONTAINS the query  (`limit` → `ratelimit`)
        ratio a `difflib` ratio ≥ FUZZY_FLOOR   (`conection` → `connection`, 0.95)
        0.00  otherwise

    🔴 THE TWO MIDDLE RUNGS ARE DIRECTIONAL, AND THAT IS THE WHOLE POINT. They
    ask whether the candidate spells MORE than the query, never the reverse. A
    symmetric rule ("either is a prefix of the other") scores a candidate that is
    a FRAGMENT of the query just as highly, and because `score_unit` is a mean
    over the QUERY's tokens, a single-token query then takes FULL coverage from
    one incidental short word: `logrotate` scores 0.85 off a bare `rotate`,
    `kubeconfig` 0.92 off `kube`, `nodeport` 0.92 off `node`. The symmetric form
    put a MAJORITY of every above-threshold hunk on the screen for queries whose
    word appeared nowhere in them, some pages entirely fabricated, and each one
    wearing a 0.85 or 0.92 that nothing in the entry justified.

    🔴 DO NOT QUOTE A FRACTION HERE — it is a property of a store that changes
    daily, and a stale one reads as a live claim. MEASURE IT: run single-token
    queries with `--all-scopes --json` and count the hunks whose `lines` do not
    contain the query. The behaviour itself is pinned by
    `TestSearchDirectionality` and the labelled fixture corpus.

    Both documented motivating cases are query ⊂ candidate and are UNCHANGED:
    `postgres` → `postgresql`, `limit` → `ratelimit`. The reverse direction buys
    no recall the join and the fuzzy rung do not already cover, and it is where
    the false positives lived.

    🔴 Tokens shorter than `MIN_INEXACT_LEN` take the first rung or nothing. One
    length rule guarding all three inexact rungs, not three — a per-rung length
    constant is how a predicate ends up wrong at N−1 of its sites. A length
    RATIO floor (`len(q)/len(t)`) was considered as a SECOND guard and REJECTED
    on measurement: once the rungs are directional it moves almost nothing —
    every pair it would reject is already one the candidate legitimately extends
    — while at 0.5 it refuses `rate` → `ratelimit` (4/9), a case this module
    names as motivating. A guard that costs a documented match to buy a rounding
    error is taste, not a rule.

    The `difflib` call is gated on a length window as well as on the floor: a
    ratio can only reach `FUZZY_FLOOR` when the lengths are close, so the window
    changes no answer and keeps a full-store scan in the millisecond range.
    """
    if q == t:
        return 1.0
    if len(q) < MIN_INEXACT_LEN or len(t) < MIN_INEXACT_LEN:
        return 0.0
    if t.startswith(q):
        return PREFIX_STRENGTH
    if q in t:
        return SUBSTRING_STRENGTH
    if abs(len(q) - len(t)) > 2:
        return 0.0
    ratio = difflib.SequenceMatcher(None, q, t).ratio()
    return ratio if ratio >= FUZZY_FLOOR else 0.0


def score_unit(query_tokens: Sequence[str], unit_text: str) -> float:
    """COVERAGE of the query by the unit: the mean best strength per query token.

    🔴 A MEAN AND NOT A MAX, which is the whole reason an absent term is
    observable. `nginx zzzz` against a block that says `nginx` scores 0.50 and
    does not clear the default threshold, so a query whose second word appears
    nowhere returns nothing rather than the first word's hits wearing the second
    word's authority. A max would have made every multi-token query as loose as
    its loosest word.

    An empty query scores 0 everywhere rather than matching everything: "the user
    asked for nothing" and "everything matches" are different answers and only one
    of them is honest.

    🔴 THE CANDIDATE SIDE IS TEXT, NOT TOKENS. `candidate_tokens` needs the clause
    boundaries to decide what it may join, so handing it a pre-tokenized sequence
    would silently disable that guard at whichever call site did it. There is one
    way in, and it carries the punctuation.
    """
    if not query_tokens:
        return 0.0
    cands = candidate_tokens(unit_text)
    if not cands:
        return 0.0
    total = 0.0
    for q in query_tokens:
        best = 0.0
        for t in cands:
            s = pair_strength(q, t)
            if s > best:
                best = s
                if best == 1.0:
                    break
        total += best
    return total / len(query_tokens)


# --- Search: what a hunk is ----------------------------------------------------


@dataclass(frozen=True)
class Block:
    """One searchable unit of an entry body: a bullet with its continuation
    lines, or a paragraph, ALWAYS under a named section.

    🔴 THE UNIT IS A BLOCK, NOT A LINE, and that is what makes multi-token queries
    work at all. The store is written in bullets that wrap: `nginx` on the first
    line and `rate-limit` on its continuation is ONE fact, and a per-line scorer
    gives each half 0.50 and returns nothing — the exact "returns NOTHING" failure
    a per-line two-stage prototype produced.
    """

    section: str
    start: int
    """1-based line number of the block's first line, in the entry FILE."""
    lines: tuple[str, ...] = ()

    @property
    def end(self) -> int:
        return self.start + len(self.lines) - 1

    @property
    def text(self) -> str:
        return "\n".join(self.lines)


_HEADING = re.compile(r"^(#{1,6})\s+(.*\S)\s*$")
_BULLET = re.compile(r"^\s*[-*+]\s+\S")
_FENCE = re.compile(r"^\s*(```|~~~)")


def entry_blocks(text: str) -> tuple[Block, ...]:
    """Split an entry body into searchable blocks, each under its section.

    🔴 EVERYTHING BEFORE THE FIRST HEADING IS SKIPPED, and that is the front-matter
    exclusion — expressed as a property of the OUTPUT rather than as a second
    front-matter parser. A hunk has to name its section (the caller prints it), so
    text with no section cannot be a hunk; front matter, which carries no `##`,
    falls out for free. That matters: a prototype that folded slug tokens into
    every line ranked the `---` fence line first, and `parse_front_matter` in
    `subsystem_resolver` stays the ONE thing that reads front matter.

    Headings themselves are not blocks — a query matching the literal words
    "Nuance / work-history" would otherwise hit every entry in the store.

    Fenced code is kept whole: a fence's contents are not bullets even when they
    begin with `-`, and splitting a snippet mid-command emits a fragment that
    reads like a complete instruction.
    """
    blocks: list[Block] = []
    section: str | None = None
    cur: list[str] = []
    start = 0
    in_fence = False

    def flush() -> None:
        nonlocal cur, start
        if cur and section is not None:
            blocks.append(Block(section=section, start=start, lines=tuple(cur)))
        cur = []
        start = 0

    for n, line in enumerate(text.splitlines(), start=1):
        if _FENCE.match(line):
            in_fence = not in_fence
            if not cur:
                start = n
            cur.append(line)
            continue
        if in_fence:
            cur.append(line)
            continue
        heading = _HEADING.match(line)
        if heading:
            flush()
            section = heading.group(0).strip()
            continue
        if not line.strip():
            flush()
            continue
        if _BULLET.match(line):
            flush()
        if not cur:
            start = n
        cur.append(line)
    flush()
    return tuple(blocks)


@dataclass(frozen=True)
class Hunk:
    """One match, carrying EVERYTHING needed to read it safely on its own.

    🔴 THE LABELS TRAVEL WITH THE CONTENT. The caveat prints once per invocation
    and sensitivity is a per-entry fact, but hunk output INTERLEAVES entries — so
    a `scope/ref` or a `sensitivity=` printed once at the top would end up
    describing somebody else's lines by the time a reader reaches them. Every
    hunk therefore restates its scope, ref, file, line, section and sensitivity.
    """

    scope: str
    ref: str
    filename: str
    sensitivity: str
    declared_sensitivity: str | None
    """A `sensitivity:` the schema does not know, which the fail-safe overrode.
    Carried onto the HUNK and not just the entry: hunk output interleaves
    entries, so an override noted anywhere but on the hunk itself is an override
    the reader never sees next to the lines it governs."""

    section: str
    start: int
    lines: tuple[str, ...]
    score: float
    basis: str
    """One of `HUNK_BASES`."""

    name_score: float = 0.0
    """How well the query matched this entry's ref/aliases. A TIE-BREAK ONLY — it
    is deliberately NOT folded into `score`, which stays a claim about the printed
    lines and nothing else.

    🔴 It exists because a one-word query for a subsystem's NAME hits that
    subsystem's own entry and half a dozen passing mentions elsewhere, ALL at
    1.00, and an alphabetical tie-break then puts the passing mentions first. That
    is the "ranks noise above signal" failure with a perfect-looking score beside
    it. Adding it to `score` instead would have made a printed 1.15 or a hunk
    whose number nothing on the screen explains."""

    @property
    def end(self) -> int:
        return self.start + len(self.lines) - 1


@dataclass(frozen=True)
class SearchReport:
    """One deterministic answer to "where does the index say anything about X?"."""

    status: str
    scope: str
    store_root: str
    query: str
    threshold: float = DEFAULT_SEARCH_THRESHOLD
    context: int = CONTEXT_BULLET
    hunks: tuple[Hunk, ...] = ()
    total_hits: int = 0
    """Hunks that cleared the threshold, BEFORE `max_hits`. The truncation
    discriminator, exactly as `total_in_scope` is for the digest."""

    max_hits: int = DEFAULT_MAX_HITS
    entries_searched: int = 0

    ref_to: str | None = None
    """The `--ref-to`/`?ref-to=` REVERSE-LOOKUP narrowing, in its canonical spelling.

    `None` means no filter was sent. See `RecallReport.ref_to` for why the parameter is
    spelled `ref-to` and not `ref`, and for why there is no second `has_*` boolean.
    """

    ref_to_skipped: int = 0
    """How many entries the ref-to filter REMOVED from the searched set.

    🔴 IT IS WHAT STOPS THE NARROWED ZERO READING AS AN EMPTY STORE. Without it, a
    `--ref-to` that matches nothing prints "searched 0 entries" — the exact shape
    `best_below` exists to refuse, an empty result that cannot distinguish "the query
    matched nothing" from "the query never ran against anything". `render_search`'s ref-to
    line prints this count beside the searched one, so a zero says WHY it is zero.
    """

    tag: str = ""
    """The `--tag`/`?tag=` CATEGORY narrowing, FOLDED. `""` means no filter was sent.
    See `RecallReport.tag` for why there is no second `has_*` discriminator."""

    tag_skipped: int = 0
    """How many entries the TAG filter removed from the searched set.

    🔴 IT IS THE THIRD TERM IN THE `search-unreadable` DISCRIMINATOR, NOT A DISPLAY FIELD,
    and that is the whole reason it is stored. `entries_searched + ref_to_skipped +
    tag_skipped` is the PRE-FILTER readable count, because every readable entry in the
    searched scopes lands in exactly one of the three: kept, removed by `ref-to`, or removed
    by `tag`. The status branch is about that count and never about `entries_searched` alone.
    """

    scopes_searched: tuple[str, ...] = ()
    known_scopes: tuple[str, ...] = ()
    best_below: tuple[str, float] | None = None
    """`(ref, score)` of the best hunk that did NOT clear the threshold.

    🔴 THIS IS WHAT MAKES A ZERO READABLE. `claude/RULES.md`: an empty result
    cannot distinguish two mechanisms. "The query matched nothing anywhere" and
    "the best candidate scored 0.50 against a threshold of 0.60" are different
    facts with different next actions — lower the threshold, or rephrase — and a
    searcher that prints the same blank for both has diagnosed nothing."""

    malformed: tuple[MalformedEntry, ...] = ()
    """Entry files in the SEARCHED scopes that could not be indexed — so they were
    never searched, and `entries_searched` does not count them. Rendered on every
    status: a hit list is incomplete in a way only this block can say."""

    malformed_elsewhere: tuple[MalformedEntry, ...] = ()
    """The same, outside the searched scopes. Empty under `--all-scopes`, which
    searches everything the store holds."""

    @property
    def omitted(self) -> int:
        return max(0, self.total_hits - len(self.hunks))

    @property
    def label(self) -> str:
        """The searched scopes, in prose. ONE spelling, shared with the caveat."""
        return scope_label(self.scopes_searched)

    @property
    def caveat(self) -> str:
        # The label is handed over UNQUOTED — `caveat_text` owns the backticks, so
        # quoting here too would print them twice and is exactly the kind of
        # near-miss a second spelling of one string produces.
        #
        # 🔴 EMPTY badge set, and that is a claim about THIS renderer, not a
        # shortcut: search prints `Hunk`s — matched excerpts — and never an index
        # row, so no badge can appear in its output and no badge explanation can
        # apply to it. `SearchReport` has no `entries` at all; asking it for one
        # would be an AttributeError, which is the loud version of the same fact.
        # If search ever grows an index-row view, compute the set here.
        return caveat_text(self.label, frozenset())


def _entry_hunks(
    store: Path,
    entry: SubsystemEntry,
    query_tokens: Sequence[str],
    *,
    threshold: float,
    context: int,
) -> tuple[list[Hunk], list[Hunk]]:
    """Every hunk this ONE entry offers, split into (cleared, below).

    Stage 2 of the two-stage scorer. Stage 1 — does the entry qualify at all —
    is the caller's, because it needs the NAME score and the best block score
    together.
    """
    recalled = read_entry(store, entry)
    path = store / entry.scope / entry.filename
    text = path.read_text(encoding="utf-8", errors="replace")
    raw = text.splitlines()

    name_text = " ".join((entry.ref, entry.slug, *entry.aliases))
    name_score = score_unit(query_tokens, name_text)

    def make(block: Block, score: float, basis: str) -> Hunk:
        if context == CONTEXT_BULLET:
            lines, start = block.lines, block.start
        else:
            # `-C N` — N RAW lines either side of the block's first line, clamped
            # to the file. Deliberately raw: the caller asked for a window, and a
            # window that quietly snapped back to a bullet would not be one.
            lo = max(1, block.start - context)
            hi = min(len(raw), block.end + context)
            lines, start = tuple(raw[lo - 1 : hi]), lo
        return Hunk(
            scope=entry.scope,
            ref=entry.ref,
            filename=entry.filename,
            sensitivity=recalled.sensitivity,
            declared_sensitivity=recalled.declared_sensitivity,
            section=block.section,
            start=start,
            lines=lines,
            score=score,
            basis=basis,
            name_score=name_score,
        )

    scored = [(score_unit(query_tokens, b.text), b) for b in entry_blocks(text)]
    cleared = [make(b, s, "line") for s, b in scored if s >= threshold]
    if cleared:
        return cleared, []

    best = max(scored, default=None, key=lambda sb: (sb[0], -sb[1].start))
    if best is None or (best[0] <= 0.0 and name_score < threshold):
        # 🔴 A ZERO-SCORING BLOCK IS NOT A NEAR MISS. Reporting it as one would
        # make `best_below` say "the closest candidate scored 0.00", which reads
        # as a weak match and is really an ABSENT TERM — the two need different
        # next actions (lower the threshold vs. rephrase), so they must not
        # collapse into one sentence.
        return [], []
    if name_score >= threshold:
        # 🔴 THE NAME-ONLY HIT. The entry IS the answer — its ref or an alias is
        # what was searched for — and no single block happened to clear. Emitting
        # nothing here is what made a per-line two-stage prototype return NOTHING
        # for a query that named an entry outright. The basis says which selector
        # fired, so a name hit is never mistaken for a content hit.
        return [make(best[1], name_score, "entry-name")], []
    return [], [make(best[1], best[0], "line")]


def search(
    store_root: str | Path,
    scope: str,
    query: str,
    *,
    context: int = CONTEXT_BULLET,
    threshold: float = DEFAULT_SEARCH_THRESHOLD,
    max_hits: int = DEFAULT_MAX_HITS,
    all_scopes: bool = False,
    ref_to: str | None = None,
    tag: str | None = None,
    visible_scopes: Sequence[str] | None = None,
) -> SearchReport:
    """Find HUNKS matching `query`. READ-ONLY, stdlib only, nothing is spawned.

    Guard order — each reachable by an input no earlier guard rejects:
      1. `query` non-empty      → ValueError
      2. `threshold` in [0, 1]  → ValueError  (a real query still reaches it)
      3. `max_hits` sanity      → ValueError
      4. `context` sanity       → ValueError
      5. store root exists      → StoreMissingError  (reused, same condition)
      6. scope known            → status `scope-absent`, NOT an error

    Statuses are `STATUS_PRECEDENCE`'s, not a second vocabulary: `search-hit`,
    `search-no-match`, and `scope-absent` when a single named scope is not in the
    store. `--all-scopes` cannot produce `scope-absent` — it names no scope.
    """
    if not query or not query.strip():
        raise ValueError("query must be a non-empty string")
    if not isinstance(threshold, (int, float)) or isinstance(threshold, bool):
        raise ValueError(f"threshold must be a number in [0, 1], got {threshold!r}")
    if not 0.0 <= float(threshold) <= 1.0:
        raise ValueError(f"threshold must be a number in [0, 1], got {threshold!r}")
    if not isinstance(max_hits, int) or isinstance(max_hits, bool) or max_hits < 1:
        raise ValueError(f"max-hits must be an int >= 1, got {max_hits!r}")
    if not isinstance(context, int) or isinstance(context, bool) or context < CONTEXT_BULLET:
        raise ValueError(f"context must be an int >= 0, got {context!r}")
    # Last, for the reason `recall`'s own trailing guard gives.
    ref_to_ref = _validated_ref_to(ref_to)
    # Last in the ladder, for the reason `recall`'s own trailing guard gives.
    want_tag = _validated_tag(tag)

    # 🔴 THE `all_scopes` PATH IS THE REASON THIS IS AN INDEX FILTER AND NOT A
    # PER-SCOPE REFUSAL CHECK. `?all_scopes=1` names NO scope, so there is
    # nothing for such a check to refuse — it would search the CONTENT of every
    # scope in the store and report hits from scopes the caller cannot name.
    # Narrowing the index instead makes `index.scopes` below already the
    # caller's own set, so the store-wide search is store-wide over what the
    # caller may see and nothing else.
    store, index = load_store(
        store_root, verb="searched", visible_scopes=visible_scopes
    )
    bad = index.malformed_in(scope)
    bad_elsewhere = index.malformed_outside((scope,))

    if all_scopes:
        scopes = index.scopes
    elif normalize_ref(scope) not in index.scopes:
        # Asked of `index.scopes` rather than by catching the loader's raise: the
        # ONE `except UnknownScopeError` in this module is `recall`'s, and a
        # second copy of a catch is a second place for the two to disagree about
        # what an unknown scope means.
        return SearchReport(
            status="scope-absent",
            scope=normalize_ref(scope),
            store_root=str(store),
            query=query,
            threshold=float(threshold),
            context=context,
            max_hits=max_hits,
            known_scopes=index.scopes,
            malformed=bad,
            malformed_elsewhere=bad_elsewhere,
        )
    else:
        scopes = (normalize_ref(scope),)

    # Re-derived AFTER `scopes` is settled, because `--all-scopes` changes what
    # "here" means: with it, every reject in the store is in the searched set and
    # nothing is elsewhere. Deriving it once above would have reported a
    # store-wide scan's own broken entries as somebody else's problem.
    bad = tuple(m for m in index.malformed if m.scope in scopes)
    bad_elsewhere = index.malformed_outside(scopes)

    # 🔴 THE REVERSE-LOOKUP OPERAND IS PARSED ONCE, OUTSIDE THE SCOPE LOOP, and
    # `ref_to_ref` is only consulted when it is not None — so a search with no
    # filter does no per-entry work it did not do before.
    # ONE dict, for the reason `recall`'s own says: a filter's fields belong on the single
    # `SearchReport` literal below, and WHICH keys are present is what says which filters ran.
    filter_fields: dict[str, object] = {}
    if ref_to_ref is not None:
        filter_fields["ref_to"] = str(ref_to_ref)
    if want_tag is not None:
        filter_fields["tag"] = want_tag

    query_tokens = tokenize(query)
    cleared: list[Hunk] = []
    below: list[Hunk] = []
    searched = 0
    ref_to_skipped = 0
    tag_skipped = 0
    for sc in scopes:
        for entry in sorted(index.entries(sc), key=lambda e: e.ref):
            # 🔴 HERE, INSIDE THE LOOP OVER `scopes`, WHICH IS AFTER SCOPE
            # AUTHORISATION AND NEVER BEFORE IT. `scopes` comes from `index.scopes`
            # — including on the `all_scopes` path — and that index was narrowed by
            # `visible_scopes` at load time. The filter therefore only ever REMOVES
            # entries from a set this caller was already entitled to read, and
            # cannot surface one from a scope they cannot name. The paragraph above
            # says why visibility is an index filter rather than a per-scope refusal
            # check; a ref-to filter applied to a store-wide load instead would
            # re-open exactly the hole it closed.
            if ref_to_ref is not None and not entry_references(entry, ref_to_ref):
                ref_to_skipped += 1
                continue
            # 🔴 HERE TOO, AND AFTER the ref-to filter, for the reason the paragraph above
            # gives: `scopes` was narrowed by `visible_scopes` at load time, so this filter can
            # only REMOVE entries from a set the caller was already entitled to read. A tag is
            # the operand most likely to tempt somebody into a store-wide load ("every
            # marketing entry, across scopes"), which is exactly the hole the index filter
            # closed. AFTER `ref-to` so the two counters PARTITION the readable set rather than
            # double-counting an entry both would have removed — the status branch below is a
            # claim about that partition.
            if want_tag is not None and not entry_has_tag(entry, want_tag):
                tag_skipped += 1
                continue
            searched += 1
            hits, misses = _entry_hunks(
                store, entry, query_tokens, threshold=float(threshold), context=context
            )
            cleared.extend(hits)
            below.extend(misses)

    # THE ONE ordering site for hunks: score descending, then the entry-name
    # tie-break (see `Hunk.name_score`), then scope/ref/line ascending so two runs
    # over an unchanged store produce identical bytes.
    cleared.sort(key=lambda h: (-h.score, -h.name_score, h.scope, h.ref, h.start))
    worst = max(below, default=None, key=lambda h: (h.score, -h.start))
    return SearchReport(
        # 🔴 `search-unreadable` OUTRANKS `search-no-match`, and the discriminator
        # is "NOTHING READABLE EXISTED" — not `cleared == 0`, and, since the
        # `ref-to` filter above, not `searched == 0` either. A query that ran over
        # zero readable entries produced a zero that says nothing about the query,
        # and `render_search`'s no-match branch would have printed "searched 0
        # entries … nothing cleared the threshold" — technically true, and read by
        # everyone as "the store has nothing on this". Same class of bug as
        # `scope-empty` swallowing a broken scope.
        #
        # 🔴 `ref_to_skipped == 0` IS THE TERM THE FILTER MADE NECESSARY, AND ITS
        # ABSENCE WAS A MEASURED DEFECT RATHER THAN A HYPOTHETICAL. `searched == 0
        # and bad` was SOUND while `searched` counted every readable entry in the
        # scope: a zero then meant nothing readable existed. The `ref-to` filter
        # `continue`s before that counter and does not touch `bad`, so it can
        # drive `searched` to 0 over a scope whose readable entries were all read
        # and indexed — and one malformed file beside them was enough to answer
        # `search-unreadable`, whose body says "NOT ONE of them could be indexed"
        # and "The query was never run against anything". Both false, and the
        # reader is sent to fix a file that had nothing to do with the empty
        # result.
        #
        # So the PRE-FILTER readable count is what this branch is about, and
        # `searched + ref_to_skipped` is that count: every readable entry in the
        # searched scopes lands in exactly one of the two. A filter-driven zero
        # therefore falls through to `search-no-match`, where the ref-to line's own
        # "0 of N" and the sentence `render_search` prints for that shape are the
        # honest answer: the query DID run, over a narrowed set that was empty.
        # 🔴 `tag_skipped == 0` IS THE SAME TERM ONE FILTER LATER, AND IT IS ADDED IN THE
        # SAME CHANGE AS THE FILTER RATHER THAN IN A LATER AUDIT ROUND. The `ref_to_skipped`
        # term was missing for three rounds because the commit that staled the condition WAS
        # the commit that introduced the filter, so no delta audit's range contained it. The
        # rule the terms share, stated once: EVERY filter placed upstream of `searched`
        # falsifies this condition unless its own skip count is subtracted back out.
        status=(
            "search-unreadable"
            if searched == 0 and ref_to_skipped == 0 and tag_skipped == 0 and bad
            else ("search-hit" if cleared else "search-no-match")
        ),
        scope=normalize_ref(scope) if not all_scopes else "(all scopes)",
        store_root=str(store),
        query=query,
        threshold=float(threshold),
        context=context,
        hunks=tuple(cleared[:max_hits]),
        total_hits=len(cleared),
        max_hits=max_hits,
        entries_searched=searched,
        ref_to_skipped=ref_to_skipped,
        tag_skipped=tag_skipped,
        scopes_searched=tuple(scopes),
        known_scopes=index.scopes,
        **filter_fields,
        best_below=(worst.ref, round(worst.score, 3)) if worst is not None else None,
        malformed=bad,
        malformed_elsewhere=bad_elsewhere,
    )


def render_search(
    report: SearchReport,
    *,
    extra_header: Sequence[str] = (),
    instance: str | None = None,
) -> str:
    """The agent-facing search block. Deterministic: same report in, same bytes out.

    `extra_header` — same contract as `render_text`'s: the CLI's read-store stamp
    belongs in the header block, and a search is a read like any other, so it
    carries its own freshness too.

    `instance` is the alias of the cairn instance this report was read from, and
    it is `None` — the single-instance case — for the pod and for every host
    configured with one instance. Passing it adds a clause to the caveat saying
    which instance was consulted; see `entry_shape.store_caveat`.
    """
    ctx = "bullet" if report.context == CONTEXT_BULLET else f"±{report.context} raw lines"
    out: list[str] = [
        f"subsystem-recall: status={report.status} scope={report.scope} "
        f"query={report.query!r} threshold={report.threshold:.2f} context={ctx}",
        f"  store: {report.store_root}",
        store_host_line(instance=instance),
        *extra_header,
        f"  caveat: {report.caveat}",
    ]

    # Same reasoning as `render_text`'s: the narrowing announces itself, because the
    # "searched N entries" count below is about the narrowed set and a zero from a filter
    # that matched nothing is otherwise indistinguishable from a zero from an empty store.
    # `ref_to_skipped` + `entries_searched` is the pre-filter total, so the line needs no
    # third field to carry it.
    if report.ref_to is not None:
        out.append(
            # A CONSTANT True. THE NUMERATOR IS WHY IT CAN BE ONE FOR THE COUNT; THE
            # SECOND CLAUSE IS CARRIED BY AN ENUMERATION AND BY NOTHING SHORTER.
            #
            # The count: `entries_searched` is incremented once per entry the filter KEPT,
            # so the number printed here is the kept count by construction. `render_text`'s
            # problem — a count of matched entries rendered over a body about none of them —
            # has no spelling here on the count side.
            #
            # The clause "everything below is about those N" is a claim about the BODY, and
            # the only thing that carries it is an enumeration of what can BE below it.
            # This line sits above exactly three statuses — `scope-absent` returns before
            # the operand is parsed, so it never carries one — and `search-no-match` has
            # THREE distinguishable bodies, which makes FIVE cases. ⚠ IT SAID "two bodies /
            # FOUR cases" AND THAT WAS MEASURED INCOMPLETE: `N == 0` is TWO bodies, not one,
            # and the second is live in this change's own `tests/dualrun/` gate. The
            # paragraph below says a new body must be added to this list and checked BY
            # HAND — so an incomplete list returns a false clean, which is why the fourth
            # row exists rather than being folded into the third. Each was re-derived
            # rather than covered by an absolute:
            #
            #   - `search-hit` — the hunks below come from kept entries only, because
            #     `_entry_hunks` is called after the filter's `continue` and nowhere else.
            #   - `search-no-match` with N > 0 — the body is "searched N entries in
            #     <scopes>, and nothing cleared the threshold", plus a near miss that is
            #     itself a hunk from a kept entry. A statement about exactly those N.
            #   - `search-no-match` with N == 0 and `ref_to_skipped > 0` — VACUOUS, not
            #     false: the body says the scan found nothing because the FILTER removed
            #     every readable entry (the middle branch of the no-match chain below).
            #     Nothing under the line reports on a REMOVED entry as though it had been
            #     kept, which is what the clause would have to be false about.
            #   - `search-no-match` with N == 0 and `ref_to_skipped == 0` — a
            #     PRESENT-BUT-EMPTY scope asked with `?ref-to=`. Nothing existed to keep OR
            #     to skip, so the chain falls through to the `else` and prints "No candidate
            #     scored above zero at all, so this is an absent term rather than a weak
            #     one." over a "0 of 0" line. Vacuous for the same reason as the row above —
            #     there is no removed entry for anything below to misreport — but it reaches
            #     the branch by a DIFFERENT mechanism, and the ⚠ note at the no-match chain
            #     below is what owns that route. Measured live on both servers, identical
            #     bytes: `genstore.py`'s empty scope is one of the `search-ref-to` dualrun
            #     targets.
            #
            # ⚠ THE FIFTH CASE IS `search-unreadable`, and after the fix it requires
            # `ref_to_skipped == 0` as well as `entries_searched == 0` — so the line there
            # reads "0 of 0", and the clause is about the empty set over a pre-filter set
            # that was also empty. ⚠ "0 of 0" IS THEREFORE NOT THAT STATUS'S SIGNATURE: the
            # fourth row prints it too, and the STATUS is what tells them apart. 🔴 NONE OF
            # THAT IS WHY THE PRE-FIX COMMENT WAS WRONG, AND THE DIFFERENCE IS THE POINT: it
            # reasoned only about the CLAUSE ("about the empty set rather than false about a
            # non-empty one") and was literally true while the STATUS it annotated was false
            # about the store. Re-deriving a clause says nothing about the branch that
            # reaches it.
            #
            # 🔴 AND THE ENUMERATION IS THE WHOLE JUSTIFICATION, WHICH IS WEAKER THAN A GUARD
            # AND IS SAID SO RATHER THAN DRESSED UP. It is PROSE: nothing mechanical asserts
            # that the set of statuses reachable under this line is still those three, the
            # way `renders_narrowed_set`'s table is now asserted over all four render shapes.
            # A new `search-*` status, or a new body under an existing one, has to be added to
            # this list and checked BY HAND today — or the constant has to become a property
            # the way `renders_narrowed_set` is.
            # 🔴 BOTH NUMBERS MOVED WHEN THE TAG FILTER LANDED, AND LEAVING THEM WOULD HAVE
            # BEEN A FALSE SENTENCE IN BOTH HALVES. The numerator is "entries that reference
            # this", which is the set the `ref-to` filter KEPT — `entries_searched` alone once
            # WAS that set and is not any more, because the tag filter removes some of it
            # downstream. The denominator is the readable set, which now partitions three ways.
            # So: kept-by-ref-to is `entries_searched + tag_skipped`, and readable is all three
            # counters. With no tag filter `tag_skipped` is 0 and this line is byte-identical to
            # what it was, which is what keeps the `search-ref-to-*` goldens still.
            _ref_to_line(
                report.ref_to,
                report.entries_searched + report.tag_skipped,
                report.entries_searched + report.ref_to_skipped + report.tag_skipped,
                report.label,
                narrowed_set_shown=True,
                narrowed_further=report.tag_skipped > 0,
            )
        )

    # The CATEGORY narrowing, after the reverse-lookup line because that is the order the
    # filters ran in.
    #
    # 🔴 THE CONSTANT `True` IS JUSTIFIED BY THE SAME ENUMERATION ABOVE, and the count half is
    # safe for the same reason: `entries_searched` counts entries the filters KEPT, so the
    # numerator here is the kept count whatever removed the rest. A `tag` filter adds no new
    # STATUS and no new BODY to that enumeration — every shape it can produce is a shape
    # `ref-to` can already produce, because the two differ only in their predicate and both run
    # over the same entry inside the same scope loop. That is why this line does not lengthen
    # the list; it is also exactly the kind of claim the list's own last paragraph says nothing
    # mechanical checks.
    #
    # 🔴 THE DENOMINATOR IS THE SET THIS FILTER SAW, NOT THE READABLE TOTAL, and the two differ
    # exactly when a `ref-to` filter also ran. `entries_searched + tag_skipped` is the set the
    # tag filter was handed; adding `ref_to_skipped` would print "2 of 7 entries carry it" while
    # the filter had only ever LOOKED at 3 — a claim about four entries whose tags were never
    # read. The recall side has the same shape and says so: `tag_scope_total` is assigned AFTER
    # the reverse-lookup narrowing, not before it.
    if report.tag:
        out.append(
            _tag_line(
                report.tag,
                report.entries_searched,
                report.entries_searched + report.tag_skipped,
                report.label,
                narrowed_set_shown=True,
            )
        )

    # Same rule as `render_text`: before every branch, on every status.
    out.extend(render_malformed(report.malformed, report.malformed_elsewhere, report.label))

    if report.status == "search-unreadable":
        out.append("")
        out.append(unreadable_summary(report.label, report.malformed))
        out.append(
            f"  The query {report.query!r} was never run against anything — this is NOT "
            f"'no matches'."
        )
        return "\n".join(out)

    if report.status == "scope-absent":
        out.append("")
        out.append(
            f"NOTHING RECORDED YET ON THIS HOST — {store_host()}'s store has no "
            f"`{report.scope}/` directory, so the query was never run. This is NOT "
            f"'no matches': nothing was searched, so nothing can be concluded from it."
        )
        out.append(
            f"  NOT A FACT ABOUT THE FLEET — {store_caveat(instance)}. The other host syncs "
            f"the SAME hosted store through its own cache, and may already hold "
            f"`{report.scope}/` where this one has not synced it yet."
        )
        out.append(
            f"  scopes THIS HOST's store holds: "
            f"{', '.join(report.known_scopes) or '(none)'}"
        )
        return "\n".join(out)

    scanned = (
        f"{report.entries_searched} entr"
        f"{'y' if report.entries_searched == 1 else 'ies'} in "
        f"{', '.join(f'`{s}/`' for s in report.scopes_searched) or '(none)'}"
    )

    if report.status == "search-no-match":
        out.append("")
        # 🔴 THE ZERO CARRIES ITS OWN EVIDENCE. How much was scanned (so a zero
        # from an empty scan is visible), and the best NEAR miss with its score
        # (so "matched nothing" and "just missed" are distinguishable).
        #
        # 🔴 AND THE MIDDLE BRANCH IS THE FILTER'S ZERO, SAID AS THE FILTER'S. With
        # nothing scanned, "an absent term rather than a weak one" is a claim about
        # the QUERY that no comparison was made to support — the same defect
        # `best_below` exists to refuse, one branch over. It is the shape the
        # `ref-to` filter introduced: every readable entry was read and indexed, and
        # then removed by the narrowing.
        #
        # ⚠ ORDERED AFTER `best_below` AND THE ORDER CANNOT MATTER: a near miss is a
        # hunk from a scanned entry, so `best_below is not None` and
        # `entries_searched == 0` cannot both hold. Written as a chain so a later
        # edit does not have to re-derive that.
        #
        # ⚠ REACHABLE ONLY WITH A FILTER THAT REMOVED SOMETHING, DELIBERATELY. The
        # zero-scanned sentence a present-but-empty scope prints is a DIFFERENT
        # mechanism reaching the same branch, it predates the filter, and it is left
        # exactly as it was rather than swept in here.
        if report.best_below is not None:
            near = (
                f" The closest candidate was `{report.best_below[0]}` at "
                f"{report.best_below[1]:.2f}, below the {report.threshold:.2f} threshold — "
                f"re-run with `--threshold {max(0.0, report.best_below[1] - 0.01):.2f}` to see "
                f"it, or rephrase."
            )
        elif report.entries_searched == 0 and (
            report.ref_to_skipped > 0 or report.tag_skipped > 0
        ):
            # 🔴 IT NAMES WHICH FILTER, AND WITH TWO OF THEM THAT STOPPED BEING A CONSTANT. A
            # sentence hardcoding "`ref-to`" would send a reader who typed only `--tag` to check
            # an operand they never gave — the empty-result hazard one level up, where the
            # diagnosis is WRONG rather than absent. Two filters make three subjects, and both
            # halves of the sentence have to agree with the one chosen.
            subject, possessive = _emptying_filters(
                report.ref_to_skipped, report.tag_skipped
            )
            near = (
                f" Nothing was scanned: {subject} removed every readable entry, "
                f"so this zero is {possessive} and says nothing about the query."
            )
        else:
            near = (
                " No candidate scored above zero at all, so this is an absent term rather "
                "than a weak one."
            )
        out.append(f"NO MATCH — searched {scanned}, and nothing cleared the threshold.{near}")
        return "\n".join(out)

    out.append("")
    out.append(
        f"SEARCH ({RECALL_LABEL}) — {len(report.hunks)} of {report.total_hits} hunk"
        f"{'' if report.total_hits == 1 else 's'} at or above {report.threshold:.2f}, "
        f"from {scanned}:"
    )
    for h in report.hunks:
        out.append("")
        out.append(
            f"  [{h.score:.2f} {h.basis}] {h.scope}/{h.ref}  {h.section}  "
            f"({h.scope}/{h.filename}:{h.start}-{h.end}, "
            f"sensitivity={sensitivity_label(h.sensitivity, h.declared_sensitivity)})"
        )
        for line in h.lines:
            out.append(f"    {line}")

    if report.omitted:
        out.append("")
        out.append(
            f"… {report.omitted} further hunk{'' if report.omitted == 1 else 's'} cleared "
            f"the threshold and were NOT shown (--max-hits {report.max_hits}). This is a "
            f"display cap, not a judgement about relevance — raise it to see the rest."
        )
    return "\n".join(out)


def search_json(report: SearchReport) -> dict:
    return {
        "status": report.status,
        "scope": report.scope,
        "store_root": report.store_root,
        # WHOSE disk. The path is identical on both machines and the contents are
        # not, so `store_root` alone cannot tell two hosts' reports apart.
        "store_host": store_host(),
        "label": RECALL_LABEL,
        "caveat": report.caveat,
        "query": report.query,
        "threshold": report.threshold,
        "context": report.context,
        "max_hits": report.max_hits,
        "total_hits": report.total_hits,
        "omitted": report.omitted,
        "entries_searched": report.entries_searched,
        "ref_to": report.ref_to,
        "ref_to_skipped": report.ref_to_skipped,
        "tag": report.tag,
        "tag_skipped": report.tag_skipped,
        "scopes_searched": list(report.scopes_searched),
        "known_scopes": list(report.known_scopes),
        **_malformed_json(report.malformed, report.malformed_elsewhere),
        "best_below": (
            {"ref": report.best_below[0], "score": report.best_below[1]}
            if report.best_below is not None
            else None
        ),
        "hunks": [
            {
                "scope": h.scope,
                "ref": h.ref,
                "file": h.filename,
                "sensitivity": h.sensitivity,
                "declared_sensitivity": h.declared_sensitivity,
                "section": h.section,
                "start_line": h.start,
                "end_line": h.end,
                "score": round(h.score, 4),
                "name_score": round(h.name_score, 4),
                "basis": h.basis,
                "lines": list(h.lines),
            }
            for h in report.hunks
        ],
    }


# --- CLI -----------------------------------------------------------------------

# 🔴 `EXIT_UNSTAMPED_READ_STORE` USED TO BE DECLARED HERE, AND MOVED to
# `subsystem_read_store` when `scripts/subsystem-audit.py` grew the same refusal:
# two `= 4` literals in two tools would have been two things to keep in step, with
# nothing reconciling them. It is imported at the top of this file (beside the
# resolver itself) and stays reachable as `subsystem_recall.EXIT_UNSTAMPED_READ_STORE`
# for every existing caller. The reasoning that used to sit here — why it is not
# folded into 3 — moved with it.


class _StoreAction(argparse.Action):
    """Records that `--store` was given EXPLICITLY, alongside its value.

    🔴 THE DISTINCTION IS THE CONTRACT, NOT AN IMPLEMENTATION DETAIL. The
    DEFAULT resolution of `--store` is refused when the store carries no
    snapshot stamp, because a default that silently lands on a frozen mirror is
    the defect this guard exists for. An EXPLICIT `--store <path>` stays
    permissive: that is an operator naming a directory deliberately, and the
    store-api's own `/data`, every test fixture and `prune-index`'s prescribed
    commands are all exactly that case.

    Comparing the parsed value against the default would NOT answer this —
    `--store` pointed at the cache by hand is byte-identical to not passing it,
    and the two must not behave the same. Recording the ACT of passing it is the
    only thing that can tell them apart, and it survives `--store=X`, `--store X`
    and argparse's prefix abbreviations alike.
    """

    def __call__(self, parser, namespace, values, option_string=None):  # noqa: D102
        setattr(namespace, self.dest, values)
        setattr(namespace, "store_explicit", True)


def _build_parser() -> argparse.ArgumentParser:
    p = argparse.ArgumentParser(
        prog="subsystem-recall",
        description=(
            "Surface what the /analyze-service index already records for this repo's "
            "scope: `## What it is` + `## Pointers` + `## Nuance / work-history`, and "
            "nothing else. "
            "READ-ONLY: it never writes to the store, and it never touches the network."
        ),
    )
    # 🔴 "PATH" IS LOAD-BEARING IN THE HELP TOO. The old text said "repo", which
    # is what invited `--repo beta-cluster` — a bare name that silently
    # becomes `$PWD/<name>`. The refusal explains it, but the flag's own
    # self-description should not have set the trap.
    p.add_argument(
        "--repo",
        default=".",
        help="PATH to the repo whose scope to read — not a repo name (default: cwd)",
    )
    p.add_argument("--scope", default=None, help="override the derived store scope")
    # 🔴 THE SYNCED CACHE, NOT THE WRITER'S `DEFAULT_STORE_ROOT`. Since the Cairn
    # cutover `~/.claude/<mirror-root>` is a FROZEN mirror that nothing
    # refreshes; defaulting to it made every `/resume` orient on a store that had
    # stopped moving while printing "ALL N entries … none omitted". Resolved at
    # parser-BUILD time through `read_store_root()`, so the module global is the
    # only place the path is written down.
    p.add_argument(
        "--store",
        action=_StoreAction,
        default=str(_read_store.read_store_root()),
        help="store root (default: the cairn-synced read cache)",
    )
    # The other half of `_StoreAction`: absent the flag, nothing sets this.
    p.set_defaults(store_explicit=False)
    p.add_argument(
        "--ref",
        default=None,
        help=(
            "surface ONE entry by ref instead of the whole scope, resolved by the "
            "writer's own resolver. An ambiguous ref is reported, never picked."
        ),
    )
    p.add_argument(
        "--list",
        action="store_true",
        dest="listing",
        help=(
            "the INDEX ONLY: one line per entry — ref, bullet count, sensitivity — for "
            "EVERY entry in the scope. Never truncated, and never a body."
        ),
    )
    p.add_argument(
        "--limit",
        type=int,
        default=None,
        help=(
            f"print up to N entry BODIES instead of the default digest (the pre-digest "
            f"behaviour; N={DEFAULT_ENTRY_LIMIT} was the old default). A truncation is "
            f"always printed; it is never silent."
        ),
    )
    p.add_argument(
        "--page",
        type=int,
        default=None,
        help=(
            f"which page of the INDEX to list, 1-based (default 1). The index is capped "
            f"at {LISTING_PAGE_SIZE} lines per page and the remainder is always counted "
            f"and never dropped. Order is NEWEST-FIRST by entry-file mtime, tie-broken by "
            f"ref, so page 1 is the freshest and the last page is the stalest. Applies to "
            f"the digest and to --list; --ref and --limit print no index."
        ),
    )
    p.add_argument(
        "-s",
        "--search",
        default=None,
        metavar="QUERY",
        help=(
            "search the scope for QUERY and print matching HUNKS instead of whole "
            "entries — for when the scope has grown past the point where reading entries "
            "whole is affordable. Fuzzy and stdlib-only: it shells out to nothing. Every "
            "hunk carries its own scope/ref, section and sensitivity=, plus its score and "
            "the threshold, so a weak match is visibly weak. A query term that matches "
            "nothing costs its share of the score, so a two-word query needs both words. "
            "Matching is one-way: a term is matched by corpus words that EXTEND it "
            "(`postgres` finds `postgresql`), never by ones it merely contains — so "
            "type the SHORTER form when you are unsure, not the longer."
        ),
    )
    p.add_argument(
        "-C",
        "--context",
        type=int,
        default=None,
        metavar="N",
        help=(
            "with --search: print N RAW lines either side of the match instead of the "
            "default, which is the ENCLOSING BULLET. Why the bullet is the default: an "
            "entry is structured (`## Pointers`, `## Nuance / work-history`) and its "
            "bullets wrap, so a fixed line window can cut one in half and emit a fragment "
            "that reads like a complete instruction when it is not — `-C 0` gives you the "
            "matched block's own lines only, and a small `-C` can still slice a "
            "neighbouring bullet's tail onto the screen. The tradeoff is yours: the "
            "bullet is safer to quote, a raw window shows you what SURROUNDS the match "
            "(the heading above it, the next bullet) which is what you want when you are "
            "orienting rather than quoting."
        ),
    )
    p.add_argument(
        "--all-scopes",
        action="store_true",
        help="with --search: search every scope in the store, not just this repo's.",
    )
    p.add_argument(
        "--threshold",
        type=float,
        default=None,
        metavar="F",
        help=(
            f"with --search: the score a hunk must clear, in [0, 1] "
            f"(default {DEFAULT_SEARCH_THRESHOLD:.2f}). A no-match prints the best "
            f"sub-threshold candidate and the exact --threshold that would surface it."
        ),
    )
    p.add_argument(
        "--max-hits",
        type=int,
        default=None,
        metavar="N",
        help=(
            f"with --search: print at most N hunks (default {DEFAULT_MAX_HITS}). A "
            f"truncation is always printed; it is never silent."
        ),
    )
    p.add_argument("--json", action="store_true")
    return p


# 🔴 ONE RULE, ONE PLACE. `--search`, `--ref` and `--list` are three SELECTORS
# over the same store and every pair of them is the same conflict; three pairwise
# `if`s would be that predicate open-coded at three sites, wrong at two of them
# the first time somebody adds a fourth selector. The message keeps the wording a
# pinned test already asserts ("select different things").
_SELECTORS: tuple[tuple[str, str], ...] = (
    ("--search", "hunks matching a query"),
    ("--ref", "one entry's body"),
    ("--list", "the whole index"),
)

# Flags that only mean something under `--search`. Rejected rather than ignored:
# a flag silently doing nothing is the failure mode where a caller believes a
# setting took effect.
_SEARCH_ONLY: tuple[tuple[str, str], ...] = (
    ("context", "-C/--context"),
    ("all_scopes", "--all-scopes"),
    ("threshold", "--threshold"),
    ("max_hits", "--max-hits"),
)


def _exit_for(status: str, label: str, malformed: Sequence[MalformedEntry]) -> int:
    """The exit code, and the one stderr line that goes with it. ONE decision site.

    🔴 CONTENT SERVED ⇒ 0; NOTHING READABLE ⇒ 3. `/resume` step 4 branches on
    zero/non-zero and its instruction is "print the stderr line verbatim, note
    that recall was unavailable, and continue". That instruction is TRUE only when
    recall really was unavailable — a scope with 2 good entries and 1 malformed
    one has just served both good ones, and exiting non-zero would throw them
    away to report a defect the output already names, loudly, in band. So the
    partial case exits 0 with the MALFORMED block, and only `*-unreadable` exits
    non-zero.

    🔴 IT REUSES 3 RATHER THAN MINTING A CODE. 3 is already "the store is broken"
    for this CLI (`StoreMissingError`, `EntryUnreadableError`), the skill's
    documented handling is identical for all of them, and a fourth code would
    need every consumer to learn it before it changed any behaviour — a
    declaration no code path honours.

    ⚠ THAT ARGUMENT DOES NOT COVER `EXIT_UNSTAMPED_READ_STORE` (4), which `main`
    returns before ever reaching this function. Its handling is NOT identical to
    3's: 3 means "nothing readable is there", while 4 means "the store is fine,
    this host has not fetched it" and names a one-command remedy. `/resume`'s
    step 4 documents the difference, so the code is honoured rather than merely
    declared. Everything decided HERE still lands on 0 or 3.

    The DETAIL is on stdout (per-entry rows, the whole point); this line is the
    quotable one-sentence summary, and it is the only thing written to stderr, so
    the verbatim-print instruction yields a sentence and not a wall.
    """
    if status not in UNREADABLE_STATUSES:
        return 0
    n = len(malformed)
    print(
        f"subsystem-recall: {status}: all {n} entry file{'' if n == 1 else 's'} under "
        f"`{label}` are MALFORMED — nothing could be read, so recall was unavailable. "
        f"This is NOT an empty scope and NOT 'nothing recorded yet'. Per-entry reasons "
        f"are on stdout; check a scope with `cairn validate --scope <scope>`, which "
        f"names each file that fails to parse.",
        file=sys.stderr,
    )
    return 3


def _with_stamp(payload: dict, store: "_read_store.ReadStore") -> dict:
    """The read store's stamp, into a JSON payload, UNPARSED as a list of lines.

    A `--json` consumer has no header block to read, so without this it is the
    one reader that gets no freshness at all — which is the whole defect, in the
    one output format nobody would think to check. Unparsed on purpose: this
    module does not own the stamp's schema and must not fork it.
    """
    return {**payload, "read_store_stamp": list(store.stamp or ())}


@dataclass(frozen=True)
class RecallSelection:
    """What a set of recall flags SELECTS: the render mode, the body cap, the page."""

    mode: str
    limit: int
    page: int


def recall_selection(
    *,
    listing: bool = False,
    limit: int | None = None,
    page: int | None = None,
) -> RecallSelection:
    """Map recall's FLAGS onto `recall()`'s arguments — the ONE place that does.

    🔴 THIS EXISTS BECAUSE THE MAPPING HAD A SECOND CALLER AND NO HOME. `main()`
    derived `mode`/`limit`/`page` inline, so the `cairn` wrapper — which reaches
    this module as a LIBRARY, not through `main()` — could not offer `--ref`,
    `--list`, `--limit` or `--page` without open-coding the same three
    expressions. The digest's own footer told readers to run `--ref <name>` and
    `--limit N`, and the wrapper exited 2 on both: the documentation prescribed a
    drill-down the shipped client did not have.

    `--limit` is what selects the pre-digest full-body mode, and nothing else
    does: defaulting `limit` to `DEFAULT_ENTRY_LIMIT` at the call site would make
    "the caller asked for a cap" indistinguishable from "the caller asked for
    nothing", which is the distinction `mode` is derived from.
    """
    return RecallSelection(
        mode="list" if listing else ("full" if limit is not None else DEFAULT_MODE),
        limit=limit if limit is not None else DEFAULT_ENTRY_LIMIT,
        page=page if page is not None else 1,
    )


def reject_recall_flags(
    *,
    search: str | None = None,
    ref: str | None = None,
    listing: bool = False,
    limit: int | None = None,
    page: int | None = None,
) -> str | None:
    """The flag combinations recall REFUSES, as a message — or None if coherent.

    🔴 REJECTED, NOT SILENTLY RECONCILED. Every combination below has an obvious
    "sensible" reading and they are DIFFERENT readings, so honouring one would
    give the caller output they did not ask for and no sign of it.

    🔴 AND IT IS SHARED, NOT COPIED. Two callers enforce these rules — this
    module's `main()` and the `cairn` client's `recall` subcommand — and a
    predicate open-coded at two sites is the shape that ends up wrong at one of
    them. The messages are returned rather than printed so both callers own
    their own stream and exit code; the WORDING is pinned by tests that drive
    this function, so the two cannot drift apart in what they say either.
    """
    chosen = [
        flag
        for flag, _what in _SELECTORS
        if (flag == "--search" and search is not None)
        or (flag == "--ref" and ref is not None)
        or (flag == "--list" and listing)
    ]
    if len(chosen) > 1:
        what = {f: w for f, w in _SELECTORS}
        return (
            " and ".join(chosen)
            + " select different things ("
            + " vs ".join(what[f] for f in chosen)
            + "). Pass one."
        )
    if listing and limit is not None:
        return (
            "--limit is a cap on entry BODIES and --list prints none; "
            "the index is never truncated. Drop one."
        )
    if page is not None and (ref is not None or limit is not None):
        return (
            "--page pages the INDEX, and --ref/--limit print no index "
            "at all. Drop one."
        )
    return None


def main(argv: Sequence[str] | None = None) -> int:
    args = _build_parser().parse_args(list(argv) if argv is not None else None)

    # 🔴 The rules themselves live in `reject_recall_flags`, which the `cairn`
    # client's `recall` subcommand also calls — see that function for why they
    # are refused rather than reconciled, and why they are shared rather than
    # copied. This site owns only the stream and the exit code.
    refusal = reject_recall_flags(
        search=args.search,
        ref=args.ref,
        listing=args.listing,
        limit=args.limit,
        page=args.page,
    )
    if refusal is not None:
        print(f"subsystem-recall: {refusal}", file=sys.stderr)
        return 2
    if args.search is None:
        stray = [
            flag
            for attr, flag in _SEARCH_ONLY
            if getattr(args, attr) not in (None, False)
        ]
        if stray:
            print(
                f"subsystem-recall: {', '.join(stray)} only mean something with --search, "
                f"and doing nothing quietly is how a caller comes to believe a setting "
                f"took effect. Add --search or drop them.",
                file=sys.stderr,
            )
            return 2

    # 🔴 THE READ STORE MUST BE ABLE TO DATE ITSELF — and the guard is placed
    # HERE, after the flag-combination rejections and before ANY store I/O, on
    # purpose. Every check above rejects a malformed COMMAND; this one is the
    # first that looks at the world, so a well-formed command reaches it and no
    # earlier check can win on its behalf.
    #
    # 🔴 ONLY THE DEFAULT RESOLUTION IS REFUSED. `--store <path>` given
    # explicitly is the operator naming a directory (see `_StoreAction`), and it
    # stays permissive whether or not that directory is stamped.
    read_store = _read_store.resolve_read_store(args.store)
    if not read_store.stamped and not args.store_explicit:
        print(_read_store.refusal_message("subsystem-recall", read_store), file=sys.stderr)
        return EXIT_UNSTAMPED_READ_STORE
    # Printed UNPARSED, one field per line, and NOT interpreted: no age is
    # computed here. `recall` documents itself "no clock" and `cairn.cache_age`
    # owns that arithmetic — a second, cheaper age here would be a second answer.
    # The PREFIX comes from `_read_store`, not from an f-string here: the recon's
    # `index:` block prints the same lines, and two copies of the spelling is how
    # one renderer quietly stops matching what a skill tells a reader to relay.
    stamp_header = list(_read_store.stamp_header(read_store.stamp))

    # The derivation lives in `recall_selection`, shared with the `cairn` client.
    selection = recall_selection(
        listing=args.listing, limit=args.limit, page=args.page
    )
    mode, limit = selection.mode, selection.limit

    try:
        repo = Path(args.repo).resolve()
        # `given=args.repo` is the RAW string, before `.resolve()` ate the
        # cwd-join: a bare repo name is the mistake this guard exists for, and a
        # message that shows only the resolved path cannot say where the prefix
        # came from. `store_root` lets the refusal name the scope that IS there.
        scope = (
            args.scope
            if args.scope is not None
            else scope_for_repo(repo, store_root=args.store, given=args.repo)
        )
        if args.search is not None:
            found = search(
                args.store,
                scope,
                args.search,
                context=args.context if args.context is not None else CONTEXT_BULLET,
                threshold=(
                    args.threshold if args.threshold is not None else DEFAULT_SEARCH_THRESHOLD
                ),
                max_hits=args.max_hits if args.max_hits is not None else DEFAULT_MAX_HITS,
                all_scopes=args.all_scopes,
            )
            print(
                json.dumps(_with_stamp(search_json(found), read_store), indent=2)
                if args.json
                else render_search(found, extra_header=stamp_header)
            )
            return _exit_for(found.status, found.label, found.malformed)
        # The ONE call to `focus_window`, and only where it is EVIDENCE. The
        # digest is the only mode that features an entry, and the window is
        # derived from `repo` — so an explicitly overridden `--scope` disables
        # it: the handoff doc in THIS repo says nothing about somebody else's
        # scope, and letting it vote there would be a relevance claim built on a
        # path window that never described the entries it ranked. The printed
        # basis then says `most-recent fallback`, which is the truth.
        window = (
            focus_window(repo)
            if mode == DEFAULT_MODE and args.scope is None
            else FocusWindow()
        )
        report = recall(
            args.store,
            scope,
            ref=args.ref,
            limit=limit,
            mode=mode,
            page=selection.page,
            focus_paths=window.paths,
            focus_source=window.source,
        )
    except ValueError as exc:
        print(f"subsystem-recall: {exc}", file=sys.stderr)
        return 2
    except (TouchError, ResolverError) as exc:
        print(f"subsystem-recall: {exc}", file=sys.stderr)
        return 3

    print(
        json.dumps(_with_stamp(report_json(report), read_store), indent=2)
        if args.json
        else render_text(report, extra_header=stamp_header)
    )
    return _exit_for(report.status, f"{report.scope}/", report.malformed)


if __name__ == "__main__":  # pragma: no cover
    raise SystemExit(main())
