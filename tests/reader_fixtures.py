#!/usr/bin/env python3
"""Generate `internal/report/testdata/reader_fixtures.json` FROM THE ORACLE'S READER.

    python3 tests/reader_fixtures.py generate
    python3 tests/reader_fixtures.py print <case-id>

🔴 WHY THIS EXISTS, GIVEN THE HTTP CONFORMANCE CORPUS IS GREEN. The corpus pins the
bytes it was TOLD to send, and `tests/conformance/README.md` names what that leaves
uncovered. For the renderer the gap is large and specific: no corpus row carries an
openness marker, a near-miss marker, a `tasks:` key, a duplicate heading, a fenced
region, a scope over the 100-line index page, an ambiguous ref, a bare entry, an entry
with no `## What it is`, an honoured `sensitivity:`, a name-only search hit, a
sub-threshold near miss, a fuzzy match, a prefix or substring match, a joined compound
term, a `--max-hits` truncation, a malformed entry BESIDE readable ones, or a
`search-unreadable` scope. Every one of those is a branch in the renderer, and a green
corpus says nothing whatever about them.

🔴 SO THE EXPECTATIONS ARE THE ORACLE'S OWN OUTPUT, NOT SOMEBODY'S BELIEF ABOUT IT.
This script builds a synthetic store, runs `subsystem_recall.recall`/`search` and
`render_text`/`render_search` over it, and writes the resulting BYTES into a fixture the
Go test replays. That makes the Go renderer's test a DIFFERENTIAL measurement against
the implementation it is a port of — which is the only kind of test that can catch the
failure this port exists to prevent, namely two renderers that drift.

⚠ AND IT IS A TEST FIXTURE, NOT A SECOND CORPUS. It compares one function's output, not
an HTTP response: no status, no headers, no framing, no normalizations table. What it
adds over the corpus is SHAPES; what the corpus adds over it is the whole transport.

MTIMES ARE DECLARED IN NANOSECONDS, not as floats, and both sides set them with the
integer. A float would be converted to nanoseconds twice — once by CPython's `os.utime`
and once by Go's `os.Chtimes` — and the index order is decided by comparing those
numbers, so a one-nanosecond disagreement would move a row for reasons that have nothing
to do with the renderer.

THE HOST IS PINNED BY PATCHING THE WRITER'S SEAM, which is the one place "whose disk is
this" is answered. The Go side is handed the same literal. That is deliberate rather than
convenient: the host line is the ONE line of this output that is supposed to be
machine-dependent, so pinning it is what lets every OTHER line be compared byte for byte.

DETERMINISM IS MEASURED ON THE ONE DIMENSION THAT MOVES, which is the store path's LENGTH.
The HTTP corpus found a real defect there — a longer host label moved 21 goldens, because
`Content-Length` counted the un-normalized body, and a same-directory check was
structurally blind to it. This fixture has no length-derived field, so the bytes should not
move; `test_the_bytes_do_not_depend_on_the_STORE_PATHS_LENGTH` is what replaces "should",
regenerating under a 240-character directory and comparing against what is committed.
"""
from __future__ import annotations

import difflib
import json
import os
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT / "lib"))

import entry_shape  # noqa: E402
import subsystem_recall as rc  # noqa: E402

FIXTURE = ROOT / "internal" / "report" / "testdata" / "reader_fixtures.json"

#: An obviously-synthetic host identity. It is what BOTH sides print, so it must look
#: nothing like a real one — twelve zeroes where a machine-id prefix would be.
HOST = "fixture-host-000000000000"

#: The placeholder the store's temporary path is replaced by. Each side builds the world
#: in its own temp directory, so the `  store: ` line cannot be compared literally.
STORE_PLACEHOLDER = "<STORE-ROOT>"

#: 2000-01-01T00:00:00Z, in nanoseconds. Every mtime below is an offset from it, which
#: keeps the declaration readable and every timestamp in the synthetic year 2000.
EPOCH_NS = 946684800 * 1_000_000_000


def _entry(path: str, mtime_ns: int, lines: list[str]) -> dict:
    return {"path": path, "mtime_ns": mtime_ns, "text_lines": lines + [""]}


def _many_notes() -> list[dict]:
    """101 minimal entries, so the index PAGE CAP is reachable.

    🔴 101 AND NOT 100: the cap is 100 lines per page, so 100 entries would leave every
    pagination branch — the paginated header, the `… N more` notice, the last page's
    SILENCE, and the page-past-the-end block — unreachable while looking covered. One
    over the boundary is what makes page 2 exist.

    Generated rather than written out because the content is irrelevant to what is being
    measured; what matters is the COUNT and that the mtimes descend, so the newest-first
    order is observable rather than accidental.
    """
    out = []
    for i in range(101):
        name = f"item-{i:03d}"
        out.append(
            _entry(
                f"many-notes/{name}.md",
                # Descending, so `item-000` is newest and page 1 is reverse-numeric. An
                # ASCENDING order would coincide with the alphabetical one and a
                # wrong-order bug would be invisible.
                EPOCH_NS + (200 - i) * 1_000_000_000,
                [
                    "---",
                    f"service: {name}",
                    "scope: many-notes",
                    "sensitivity: public",
                    "---",
                    "",
                    "## What it is",
                    f"Filler entry {i}.",
                    "",
                    "## Pointers",
                    "- nothing to point at",
                    "",
                    "## Nuance / work-history",
                    "- 2000-01-02: filler.",
                ],
            )
        )
    return out


SCOPES = ["alpha-notes", "beta-notes", "hollow-set", "rubble-heap", "many-notes", "tied-notes"]

ENTRIES: list[dict] = [
    # Two entries inside ONE WHOLE SECOND, differing only in the fraction — the shape a
    # truncated mtime destroys, and the reason the listing order has a ref tie-break.
    _entry("alpha-notes/gadget-one.md", EPOCH_NS + 250_000_000, [
        "---",
        "service: gadget-one",
        "scope: alpha-notes",
        "sensitivity: internal",
        "---",
        "",
        "## What it is",
        "A synthetic entry. It mentions a rate-limit and a postgresql connection.",
        "",
        "## Pointers",
        "- ops skill `manage-gadget` - invoke it for restarts",
        "",
        "## Nuance / work-history",
        "- 2000-01-02: the readiness probe reports ready 40s before it is.",
    ]),
    _entry("alpha-notes/gadget-two.md", EPOCH_NS + 750_000_000, [
        "---",
        "service: gadget-two",
        "scope: alpha-notes",
        "sensitivity: internal",
        "---",
        "",
        "## What it is",
        "A second synthetic entry, newer than gadget-one by 500ms and no more.",
        "",
        "## Pointers",
        "- ops skill `manage-gadget` - the same skill, deliberately",
        "",
        "## Nuance / work-history",
        "- 2000-01-02: the sidecar drops its lease during a rollout.",
    ]),
    # Every openness population at once, plus `tasks:` and an HONOURED sensitivity.
    _entry("alpha-notes/marked-three.md", EPOCH_NS + 3 * 86400 * 1_000_000_000, [
        "---",
        "service: marked-three",
        "scope: alpha-notes",
        "sensitivity: public",
        "tasks: [github:example-org/example-repo#428, linear:ENG-441]",
        "---",
        "",
        "## What it is",
        "The entry that carries every openness population.",
        "",
        "## Pointers",
        "- `docs/marked-three.md`",
        "",
        "## Nuance / work-history",
        "- 2000-01-02: OPEN: the retry budget is still unbounded.",
        "  a continuation line, which belongs to the bullet above it.",
        "- 2000-01-03: RESOLVED abc1234: closed, and the sha proves it.",
        "- 2000-01-04: RESOLVED: closed, and nothing proves it.",
        "- 2000-01-05: **OPEN:** a marker that missed the grammar.",
        "- 2000-01-06: the retry budget is not yet addressed.",
        "- 2000-01-07: an ordinary bullet that declares nothing.",
    ]),
    # Neither COUNTED heading, so `is_bare` AND both `missing_sections` fire.
    _entry("alpha-notes/bare-four.md", EPOCH_NS + 1_000_000, [
        "---",
        "service: bare-four",
        "scope: alpha-notes",
        "---",
        "",
        "## What it is",
        "An entry nobody has filled in.",
    ]),
    # No `## What it is`, which is a BODY-ONLY notice and never an index badge.
    _entry("alpha-notes/nowhat-five.md", EPOCH_NS + 2_000_000, [
        "---",
        "service: nowhat-five",
        "scope: alpha-notes",
        "sensitivity: personal",
        "---",
        "",
        "## Pointers",
        "- `docs/nowhat.md`",
        "",
        "## Nuance / work-history",
        "- 2000-01-02: nginx fronts this one.",
    ]),
    # 🔴 A SCOPE WHOSE TWO ENTRIES ARE TIED TO THE NANOSECOND, AND IT IS ITS OWN SCOPE ON
    # PURPOSE. The HTTP corpus has two entries inside one whole SECOND, which is the shape a
    # truncated mtime destroys — but their floats DIFFER, so no tie-break ever runs there.
    # Two tie-breaks depend on an exact tie: the index order's, and the featured pick's. The
    # second is only observable when the tie is at the MAXIMUM, which is why these two are
    # alone in a scope rather than added to `alpha-notes`, where an older entry would win
    # the featured pick outright. Both facts were found by mutating the tie-breaks and
    # watching nothing fail.
    #
    # Named `alpha` and `zulu` so the DIRECTION is observable: the index is ref-ASCENDING on
    # a tie (alpha first) and the featured pick takes the ref-GREATEST (zulu), so a mutant
    # that reverses either one moves a line.
    _entry("tied-notes/tied-alpha.md", EPOCH_NS + 9_000_000, [
        "---",
        "service: tied-alpha",
        "scope: tied-notes",
        "---",
        "",
        "## What it is",
        "Tied to the nanosecond with tied-zulu.",
        "",
        "## Pointers",
        "- `docs/tied.md`",
        "",
        "## Nuance / work-history",
        "- 2000-01-02: nothing.",
    ]),
    _entry("tied-notes/tied-zulu.md", EPOCH_NS + 9_000_000, [
        "---",
        "service: tied-zulu",
        "scope: tied-notes",
        "---",
        "",
        "## What it is",
        "Tied to the nanosecond with tied-alpha.",
        "",
        "## Pointers",
        "- `docs/tied.md`",
        "",
        "## Nuance / work-history",
        "- 2000-01-02: nothing.",
    ]),
    # A section that is PRESENT AND EMPTY, which is a different fact from an absent one —
    # and the only input that tells the two apart. Without it, a parser deriving presence
    # from a non-empty body renders identically and the divergence is invisible.
    _entry("alpha-notes/emptysec-fourteen.md", EPOCH_NS + 2_600_000, [
        "---",
        "service: emptysec-fourteen",
        "scope: alpha-notes",
        "---",
        "",
        "## What it is",
        "Pointers is present and EMPTY, so no `NO Pointers` badge may appear.",
        "",
        "## Pointers",
        "",
        "## Nuance / work-history",
        "- 2000-01-02: the work-history is filled in and the pointers are not.",
    ]),
    # 🔴 THE CLAUSE BOUNDARY, AS A LINE THAT WOULD SCORE A FABRICATED 1.00 WITHOUT IT.
    # Joining `node` + `port` across the comma spells `nodeport` in an entry that never says
    # the word — two facts glued into a term neither of them spells, wearing the highest
    # score on the page. Nothing in the corpus can see that.
    _entry("alpha-notes/clauses-fifteen.md", EPOCH_NS + 2_700_000, [
        "---",
        "service: clauses-fifteen",
        "scope: alpha-notes",
        "---",
        "",
        "## What it is",
        "The clause-boundary and dotted-identifier fixture.",
        "",
        "## Pointers",
        "- drain that node, port-forward the socket",
        "",
        "## Nuance / work-history",
        "- 2000-01-02: the activity.events table is the sink.",
    ]),
    # A capital I WITH DOT ABOVE, whose Python lowercase expands to TWO code points and
    # whose Go simple lowercase does not. The tokenizer runs over the lowered text, so the
    # token boundary moves — `ndex` is an EXACT token on the oracle and a 0.85 substring in
    # a port that reached for `strings.ToLower`.
    _entry("alpha-notes/dotted-sixteen.md", EPOCH_NS + 2_800_000, [
        "---",
        "service: dotted-sixteen",
        "scope: alpha-notes",
        "---",
        "",
        "## What it is",
        "The İNDEX-gadget writes here.",
        "",
        "## Pointers",
        "- `docs/dotted.md`",
        "",
        "## Nuance / work-history",
        "- 2000-01-02: nothing notable.",
    ]),
    # A SECOND entry mentioning nginx, so two sub-threshold hunks TIE and the reported near
    # miss is decided by "the first maximal" rather than by there being only one candidate.
    _entry("alpha-notes/nginx-seventeen.md", EPOCH_NS + 2_900_000, [
        "---",
        "service: nginx-seventeen",
        "scope: alpha-notes",
        "---",
        "",
        "## What it is",
        "nginx fronts this one too.",
        "",
        "## Pointers",
        "- `docs/nginx.md`",
        "",
        "## Nuance / work-history",
        "- 2000-01-02: nothing notable.",
    ]),
    # 🔴 A NAME-ONLY HIT THAT SORTS **LATER** THAN A CONTENT HIT OF THE SAME SCORE, which is
    # the only shape in which the hunk sort's name-score tie-break is observable. With it,
    # `zebra-lease` (name 1.00) comes first; without it the sort falls to scope/ref and
    # `gadget-two` wins — which is the "ranks noise above signal" failure the tie-break
    # exists for, wearing a perfect score. An earlier draft used an entry whose ref sorted
    # FIRST alphabetically, so the two orders coincided and the mutant survived.
    _entry("alpha-notes/zebra-lease.md", EPOCH_NS + 3_100_000, [
        "---",
        "service: zebra-lease",
        "scope: alpha-notes",
        "---",
        "",
        "## What it is",
        "The subsystem this query is NAMING, whose body never spells the word.",
        "",
        "## Pointers",
        "- `docs/zebra.md`",
        "",
        "## Nuance / work-history",
        "- 2000-01-02: nothing notable.",
    ]),
    # ONE counted heading missing and the other filled: not bare, so the BODY prints the
    # `(no <heading> section)` notice — which is a different branch from the fill-in notice
    # a bare entry gets, and the two are mutually exclusive.
    _entry("alpha-notes/halfway-thirteen.md", EPOCH_NS + 2_500_000, [
        "---",
        "service: halfway-thirteen",
        "scope: alpha-notes",
        "---",
        "",
        "## What it is",
        "Pointers, and no work-history at all.",
        "",
        "## Pointers",
        "- `docs/halfway.md`",
    ]),
    # A heading written TWICE (the sections CONCATENATE), a FENCED region carrying both a
    # `##` line and a `-` line, a seven-hash line and a bare `##` — the two heading
    # parsers disagree about the last two, and both disagreements are the oracle's.
    _entry("alpha-notes/dupes-six.md", EPOCH_NS + 3_000_000, [
        "---",
        "service: dupes-six",
        "scope: alpha-notes",
        "---",
        "",
        "## What it is",
        "First half.",
        "",
        "####### seven hashes is not a heading to one parser and is to the other",
        "",
        "## Nuance / work-history",
        "- 2000-01-02: the first nuance section.",
        "",
        "## Interlude",
        "This paragraph is DROPPED by the section merge.",
        "",
        "## Nuance / work-history",
        "- 2000-01-03: the second nuance section, concatenated onto the first.",
        "",
        "## Pointers",
        "```",
        "## Nuance / work-history",
        "- this is sample text inside a fence, not a bullet",
        "```",
        "- a real pointer, after the fence",
    ]),
    # Two entries sharing ONE ALIAS, which is the only way a ref is AMBIGUOUS on disk:
    # the loader refuses a duplicate slug outright, so the filename tier cannot collide.
    _entry("alpha-notes/alias-seven.md", EPOCH_NS + 4_000_000, [
        "---",
        "service: alias-seven",
        "scope: alpha-notes",
        "aliases: [shared-alias, lease-holder]",
        "---",
        "",
        "## What it is",
        "Reachable by two aliases.",
        "",
        "## Pointers",
        "- `docs/alias.md`",
        "",
        "## Nuance / work-history",
        "- 2000-01-02: nothing notable.",
    ]),
    _entry("alpha-notes/alias-eight.md", EPOCH_NS + 5_000_000, [
        "---",
        "service: alias-eight",
        "scope: alpha-notes",
        "aliases: [shared-alias]",
        "---",
        "",
        "## What it is",
        "The other holder of the shared alias.",
        "",
        "## Pointers",
        "- `docs/alias-eight.md`",
        "",
        "## Nuance / work-history",
        "- 2000-01-02: also nothing notable.",
    ]),
    # A malformed entry BESIDE readable ones, which is the branch every corpus row misses:
    # there the only malformed entries live in a scope where NOTHING is readable.
    {
        "path": "alpha-notes/broken-nine.md",
        "mtime_ns": EPOCH_NS + 6_000_000,
        "text_lines": ["no front matter here, so the loader collects this as MALFORMED", ""],
    },
    _entry("beta-notes/widget-ten.md", EPOCH_NS + 2 * 86400 * 1_000_000_000, [
        "---",
        "service: widget-ten",
        "scope: beta-notes",
        "sensitivity: public",
        "---",
        "",
        "## What it is",
        "Another scope, for the all-scopes search.",
        "",
        "## Pointers",
        "- `docs/widget.md`",
        "",
        "## Nuance / work-history",
        "- 2000-01-03: the lease renewal races the readiness gate.",
    ]),
    # 🔴 THE SUBSTRING RUNG NEEDS ITS OWN SCOPE, AND THE FIRST DRAFT OF THIS FIXTURE GOT
    # 1.00 WHERE IT EXPECTED 0.85. `rate-limit` TOKENIZES to `rate` + `limit`, so a query
    # for `limit` is an EXACT token hit there and the 0.85 rung never runs. The rung needs
    # a candidate that CONTAINS the query while spelling it as one word, in a block where
    # no exact token can outscore it — which is a second entry in a second scope, because
    # the score is the best candidate in the whole BLOCK.
    _entry("beta-notes/ratelimit-eleven.md", EPOCH_NS + 2 * 86400 * 1_000_000_000 + 500_000_000, [
        "---",
        "service: ratelimit-eleven",
        "scope: beta-notes",
        "sensitivity: public",
        "---",
        "",
        "## What it is",
        "The ratelimit sidecar, spelled as one word on purpose.",
        "",
        "## Pointers",
        "- `docs/ratelimit.md`",
        "",
        "## Nuance / work-history",
        "- 2000-01-03: the ratelimit sidecar sheds traffic under pressure.",
    ]),
    {
        "path": "rubble-heap/broken-eleven.md",
        "mtime_ns": EPOCH_NS + 7_000_000,
        "text_lines": ["neither does this one", ""],
    },
    {
        "path": "rubble-heap/broken-twelve.md",
        "mtime_ns": EPOCH_NS + 8_000_000,
        "text_lines": [
            "---",
            "service: broken-twelve",
            "scope: rubble-heap",
            "aliases: a bare string, which the schema refuses",
            "---",
            "",
        ],
    },
] + _many_notes()


def _recall(case_id: str, why: str, **kwargs) -> dict:
    row = {
        "id": case_id,
        "why": why,
        "kind": "recall",
        "scope": kwargs.pop("scope"),
        "ref": kwargs.pop("ref", None),
        "limit": kwargs.pop("limit", rc.DEFAULT_ENTRY_LIMIT),
        "mode": kwargs.pop("mode", rc.DEFAULT_MODE),
        "page": kwargs.pop("page", 1),
        "visible": kwargs.pop("visible", None),
        # 🔴 THE FOCUS WINDOW IS A FIXTURE FIELD BECAUSE P2 MADE IT REACHABLE. The store
        # API never sends one and the HTTP corpus therefore cannot carry one, so the
        # `associate_paths` selector — the WRITER's matcher, which the reader calls and the
        # Go port had to grow — has no gate anywhere else. These rows are the oracle's own
        # rendered BASIS line for a resolved pick, a miss, an alias hit, an ambiguous ref
        # and the `…` truncation, which is what makes the port a measurement.
        "focus_paths": kwargs.pop("focus_paths", []),
        "focus_source": kwargs.pop("focus_source", None),
    }
    assert not kwargs, kwargs
    return row


def _search(case_id: str, why: str, **kwargs) -> dict:
    row = {
        "id": case_id,
        "why": why,
        "kind": "search",
        "scope": kwargs.pop("scope"),
        "query": kwargs.pop("query"),
        "context": kwargs.pop("context", rc.CONTEXT_BULLET),
        "threshold": kwargs.pop("threshold", rc.DEFAULT_SEARCH_THRESHOLD),
        "max_hits": kwargs.pop("max_hits", rc.DEFAULT_MAX_HITS),
        "all_scopes": kwargs.pop("all_scopes", False),
        "visible": kwargs.pop("visible", None),
    }
    assert not kwargs, kwargs
    return row


CASES: list[dict] = [
    _recall("digest", "every badge, the featured pick, the reject wording and the digest's own omission notice, at once", scope="alpha-notes"),
    _recall("list", "the index alone, and `--list`'s NOT-the-complete-index wording for a scope with a reject", scope="alpha-notes", mode="list"),
    _recall("full-truncated", "`full` mode under a cap, which is the only path that prints the --limit truncation notice", scope="alpha-notes", mode="full", limit=2),
    _recall("full-untruncated", "the same mode with room to spare, so the notice's ABSENCE is measured too", scope="alpha-notes", mode="full", limit=99),
    _recall("ref-hit-marked", "one entry in full: tasks above the sections, every badge's body-side twin", scope="alpha-notes", ref="marked-three"),
    _recall("ref-hit-bare", "a bare entry's body, where the fill-in notice fires and the missing-section notice does not", scope="alpha-notes", ref="bare-four"),
    _recall("ref-hit-nowhat", "the `no parsable ## What it is` notice, which is body-only", scope="alpha-notes", ref="nowhat-five"),
    _recall("ref-hit-halfway", "ONE counted section missing on a non-bare entry, which is the OTHER notice", scope="alpha-notes", ref="halfway-thirteen"),
    _recall("list-clean", "list mode on a scope with nothing rejected, which keeps the historical completeness claim", scope="beta-notes", mode="list"),
    _recall("ref-hit-dupes", "a heading written twice, concatenated, with the intervening section dropped", scope="alpha-notes", ref="dupes-six"),
    _recall("ref-hit-alias", "the ALIAS tier of the resolver", scope="alpha-notes", ref="lease-holder"),
    _recall("ref-ambiguous", "two entries share one alias, so the resolver refuses to pick", scope="alpha-notes", ref="shared-alias"),
    _recall("ref-absent-with-rejects", "the absence is QUALIFIED because the scope has a reject that might be the entry", scope="alpha-notes", ref="ghost-ref"),
    _recall("ref-absent-clean", "the same absence in a scope with nothing rejected, which is the other wording", scope="beta-notes", ref="ghost-ref"),
    _recall("scope-unreadable", "the scope holds files and NOT ONE indexed: exit 3 and a warning line", scope="rubble-heap"),
    _recall("scope-unreadable-with-ref", "and the unreadable check runs BEFORE the ref, so this is not `ref-absent`", scope="rubble-heap", ref="ghost-ref"),
    _recall("scope-empty", "a directory that exists and holds nothing, which must not read like an unreadable one", scope="hollow-set"),
    _recall("scope-absent", "a scope that never existed, with the known-scope list", scope="ghost-void"),
    _recall("scope-refused", "the same answer for a scope the caller may not see, with the list NARROWED", scope="alpha-notes", visible=["beta-notes"]),
    # 🔴 AN EMPTY ALLOWLIST IS THE OPPOSITE OF AN ABSENT ONE, and this is the row that says
    # so in bytes. `visible=[]` means NOTHING is visible, so the index is empty and the
    # known-scope list must read `(none)` — a clean report over a store the caller cannot
    # see any of. A port that treated an empty allowlist as unrestricted would answer the
    # whole store here, and a port that lost the `(none)` sentinel would print a bare
    # `scopes THIS HOST's store holds: ` and read as a store with no scopes at all.
    _recall("scope-absent-nothing-visible", "an EMPTY allowlist: nothing visible, so the known-scope list is `(none)`", scope="alpha-notes", visible=[]),
    _recall("page-one-of-two", "the paginated index header and the `… N more` notice", scope="many-notes", mode="list"),
    _recall("page-two-of-two", "the LAST page, where the truncation notice must be SILENT", scope="many-notes", mode="list", page=2),
    _recall("page-past-the-end", "no arithmetic on a page that does not exist", scope="many-notes", mode="list", page=9),
    _recall("digest-page-two", "a digest whose index is on page 2 while its featured body is not", scope="many-notes", page=2),
    _recall("tied-digest", "two entries tied to the NANOSECOND: the index takes ref-ascending and the featured pick takes ref-greatest, and both tie-breaks are observable only here", scope="tied-notes"),
    # --- THE FOCUS-WINDOW SELECTOR. -------------------------------------------------
    # 🔴 EVERY ROW BELOW IS UNREACHABLE FROM THE HTTP CORPUS AND REACHABLE FROM `cairn
    # recall`, which is the whole reason they exist: the pod has no repo to read a handoff
    # doc out of, so `associate_paths` is a code path only the CLI drives. The default
    # featured pick for `alpha-notes` is `marked-three` (much the newest file), so a row
    # whose window resolves to anything else is a row where the matcher demonstrably ran.
    _recall("digest-focus-resolved", "a window that RESOLVES, beating the most-recent fallback: the filename tier", scope="alpha-notes", focus_paths=["claudedocs/handoff-alpha.md", "apps/gadget-two/values.yaml"], focus_source="claudedocs/handoff-alpha.md"),
    _recall("digest-focus-alias", "the ALIAS tier of the matcher, which resolves a component no filename carries", scope="alpha-notes", focus_paths=["claudedocs/handoff-alpha.md", "svc/lease-holder/main.go"], focus_source="claudedocs/handoff-alpha.md"),
    _recall("digest-focus-miss", "a window that was READ and matched NOTHING — a DIFFERENT sentence from a window that was never read", scope="alpha-notes", focus_paths=["claudedocs/handoff-alpha.md", "apps/nothing-here/values.yaml"], focus_source="claudedocs/handoff-alpha.md"),
    _recall("digest-focus-ambiguous", "an ambiguous ref contributes to NO subsystem rather than blinding the window, so this falls back", scope="alpha-notes", focus_paths=["claudedocs/handoff-alpha.md", "svc/shared-alias/main.go"], focus_source="claudedocs/handoff-alpha.md"),
    _recall("digest-focus-tie", "two entries each named ONCE: the tie-break is the fallback's own mtime signal, not a third rule", scope="alpha-notes", focus_paths=["a/gadget-one/x.md", "b/gadget-two/y.md"], focus_source="claudedocs/handoff-alpha.md"),
    _recall("digest-focus-truncated", "FOUR paths on one entry: the basis shows three and appends `…`, and the count is the full one", scope="alpha-notes", focus_paths=["a/gadget-one/1.md", "b/gadget-one/2.md", "c/gadget-one/3.md", "d/gadget-one/4.md"], focus_source="claudedocs/handoff-alpha.md"),
    # ⚠ AND ONE WITH NO SOURCE, which is the `the supplied path window` arm. It is not
    # reachable from `cairn` — `focus.Window` sets paths and source together — so this row
    # is the only thing that measures it, and it is labelled as covering an arm the CLI
    # cannot produce rather than as parity coverage.
    _recall("digest-focus-sourceless", "a window with no named source: the `the supplied path window` arm, which no CLI invocation can produce", scope="alpha-notes", focus_paths=["apps/gadget-two/values.yaml"]),
    _recall("ref-hit-emptysec", "a PRESENT-AND-EMPTY section, which must not render as an absent one", scope="alpha-notes", ref="emptysec-fourteen"),
    _search("hit", "the ordinary search", scope="alpha-notes", query="lease"),
    _search("hit-context", "a raw ±N window instead of the enclosing bullet", scope="alpha-notes", query="lease", context=2),
    _search("hit-context-zero", "a zero-line window, which is still a window and not a bullet", scope="alpha-notes", query="lease", context=0),
    _search("hit-truncated", "the --max-hits display cap, printed when it bites", scope="alpha-notes", query="lease", threshold=0.2, max_hits=1),
    _search("fuzzy", "the difflib rung: a typo, whose RATIO is the printed score", scope="alpha-notes", query="conection"),
    _search("prefix", "the prefix rung at 0.92", scope="alpha-notes", query="postgres"),
    _search("substring", "the substring rung at 0.85, which needs a candidate spelling the query as PART of one word", scope="beta-notes", query="limit"),
    _search("exact-token-beats-substring", "and the same query in the scope that writes `rate-limit`, where an EXACT token outranks the rung above", scope="alpha-notes", query="limit"),
    _search("joined-compound", "the adjacent-pair join, which makes a concatenated term an EXACT hit", scope="alpha-notes", query="ratelimit"),
    _search("name-only", "the entry NAME qualified and no block did, so basis=entry-name", scope="alpha-notes", query="alias-seven"),
    _search("near-miss-below", "a mean below the threshold, so the zero carries the best NEAR miss and its score — and TWO entries tie at it, so 'the FIRST maximal' is the claim", scope="alpha-notes", query="nginx zzzzqqqq"),
    # 🔴 A THIRD DECIMAL IN THE NEAR-MISS SCORE. Three query tokens, one of which matches, so
    # the mean is exactly 1/3 and `round(x, 3)` keeps a non-zero third decimal. The RENDERED
    # line cannot see the digit count — it formats to two places either way — so this row
    # exists for the direct assertion in `TestTheNearMissScoreKeepsThreeDecimals`, and the
    # rendered bytes are pinned here as well because that is free.
    _search("near-miss-thirds", "the near-miss score is rounded to THREE places, which two decimals of output cannot show", scope="alpha-notes", query="nginx zzzzqqqq wwwwqqqq"),
    _search("query-extends-candidate","the rungs are DIRECTIONAL: a query that spells MORE than the candidate must score ZERO, or one incidental short word gives a single-token query full coverage", scope="beta-notes", query="ratelimiting"),
    _search("clause-boundary", "a join across a comma would spell `nodeport` in an entry that never says the word, at a fabricated 1.00", scope="alpha-notes", query="nodeport"),
    _search("dotted-identifier", "and the mirror image: a dotted identifier is ONE term whose halves must keep joining", scope="alpha-notes", query="activityevents"),
    _search("full-unicode-lowercase", "the tokenizer lowercases the way CPython does, so U+0130 expands and the token boundary moves", scope="alpha-notes", query="ndex"),
    _search("name-score-tiebreak", "two hunks at 1.00 where the NAME score decides, and the ref order disagrees with it", scope="alpha-notes", query="lease"),
    _search("absent-term", "nothing scored above zero at all, which is a different sentence", scope="alpha-notes", query="zzzzqqqqwwww"),
    _search("all-scopes", "every scope the caller can see, so nothing is `elsewhere`", scope="alpha-notes", query="lease", all_scopes=True),
    _search("all-scopes-narrowed", "the same flag for a narrowed caller: `all scopes` means all of THEIRS", scope="alpha-notes", query="lease", all_scopes=True, visible=["beta-notes"]),
    _search("unreadable", "the query never ran: exit 3, a warning, and a sentence that is NOT 'no matches'", scope="rubble-heap", query="lease"),
    _search("scope-absent", "a scope that never existed", scope="ghost-void", query="lease"),
    _search("scope-refused", "and a scope the caller may not see answers the same", scope="alpha-notes", query="lease", visible=["beta-notes"]),
    # The all-scopes flag over an EMPTY allowlist: `scopes_searched` is empty, so the
    # scanned clause has to read `(none)` rather than collapsing to nothing. This is the
    # shape a silent empty index renders as, and the sentinel is the only thing that tells a
    # reader "0 entries in nothing" from "0 entries in your scope".
    _search("all-scopes-nothing-visible", "all scopes over an EMPTY allowlist: the searched set is `(none)`, not blank", scope="alpha-notes", query="lease", all_scopes=True, visible=[]),
]


# 🔴 `difflib.SequenceMatcher(None, a, b).ratio()` IS THE MOST INTRICATE THING IN THE PORT
# AND THE RENDERED BATTERY BARELY TOUCHES IT: one fuzzy query reaches it, at one score. A
# mutation that relaxed the longest-match tie-break from `>` to `>=` SURVIVED the whole
# rendered battery, which is the measurement that put this table here.
#
# The pairs are chosen for the mechanisms, not for coverage of the alphabet:
#   * typo classes the scorer exists for (substitution, transposition, omission);
#   * REPEATED characters, where the recursive longest-match decomposition has real ties
#     and a different tie-break yields a different TOTAL — the only shape that can see it;
#   * a pair whose greedy decomposition scores strictly BELOW the longest common
#     subsequence, which is what makes "this is not an LCS" a measurement;
#   * strings at and over 200 characters, which is where AUTOJUNK engages and stops popular
#     elements SEEDING a match. Reachable: a token is bounded only by the line it came from.
DIFFLIB_PAIRS: list[tuple[str, str]] = [
    ("", ""),
    ("", "a"),
    ("a", "a"),
    ("abcd", "badc"),          # greedy 0.5, LCS 0.75 — the two disagree
    ("conection", "connection"),
    ("reciept", "receipt"),
    ("postgres", "postgresql"),
    ("ratelimit", "rate"),
    ("aaaa", "aaab"),
    ("aaaaaa", "aabaaa"),      # repeated runs: the tie-break decides the split
    ("abab", "baba"),
    ("abcabc", "cabcab"),
    ("probe", "prone"),
    ("kube", "kubeconfig"),
    ("gadget", "widget"),
    ("x" * 199, "x" * 199 + "y"),
    ("x" * 200, "x" * 200),
    ("x" * 250, "y" + "x" * 249),
    ("ab" * 120, "ba" * 120),
    ("nanosecond", "nanosecnod"),
]

# CPython's `round(float, 3)` is "format correctly to N places, then parse back", and the
# result is then formatted AGAIN with two decimals — so a `.xx5` boundary is exactly where
# two implementations part company. A mutant changing the digit count SURVIVED the rendered
# battery, because engineering a score that lands on such a boundary through the scorer is
# not something a fixture can do reliably. Measuring the function directly can.
PY_ROUND_CASES: list[float] = [
    0.0, 1.0, 0.5, 0.125, 0.135, 0.145, 0.155, 0.4445, 0.4455, 0.5005, 0.9995,
    0.0005, 0.0015, 0.66666666666, 0.83333333333, 0.12345, 0.87654, 1e-9, 0.9999999,
]


def build_store(dest: Path) -> Path:
    """Materialise the declared world. MTIMES ARE SET LAST AND FROM THE INTEGER."""
    dest.mkdir(parents=True, exist_ok=True)
    for scope in SCOPES:
        (dest / scope).mkdir(parents=True, exist_ok=True)
    for entry in ENTRIES:
        target = dest / entry["path"]
        target.parent.mkdir(parents=True, exist_ok=True)
        target.write_text("\n".join(entry["text_lines"]), encoding="utf-8")
    for entry in ENTRIES:
        target = dest / entry["path"]
        os.utime(target, ns=(entry["mtime_ns"], entry["mtime_ns"]))
    return dest


def _expect(case: dict, store: Path) -> dict:
    visible = case["visible"]
    if case["kind"] == "recall":
        report = rc.recall(
            store,
            case["scope"],
            ref=case["ref"],
            limit=case["limit"],
            mode=case["mode"],
            page=case["page"],
            visible_scopes=visible,
            focus_paths=case["focus_paths"],
            focus_source=case["focus_source"],
        )
        text = rc.render_text(report)
        label = f"{report.scope}/"
        malformed = report.malformed
    else:
        report = rc.search(
            store,
            case["scope"],
            case["query"],
            context=case["context"],
            threshold=case["threshold"],
            max_hits=case["max_hits"],
            all_scopes=case["all_scopes"],
            visible_scopes=visible,
        )
        text = rc.render_search(report)
        label = report.label
        malformed = report.malformed

    # `_exit_for` PRINTS its warning to stderr, so the sentence is rebuilt here from the
    # same inputs rather than captured. ⚠ That is a second spelling of one string, and it
    # is the reason `test_reader_fixtures.py` asserts the two agree: the Go side returns
    # the sentence instead of printing it, and a fixture that invented its own wording
    # would compare the Go renderer against this file rather than against the oracle.
    code = _exit_and_warning(report.status, label, malformed)
    return {
        "status": report.status,
        "exit": code[0],
        "warning": code[1],
        "text_lines": text.replace(str(store), STORE_PLACEHOLDER).split("\n"),
    }


def _exit_and_warning(status: str, label: str, malformed) -> tuple[int, str]:
    if status not in rc.UNREADABLE_STATUSES:
        return 0, ""
    n = len(malformed)
    return 3, (
        f"subsystem-recall: {status}: all {n} entry file{'' if n == 1 else 's'} under "
        f"`{label}` are MALFORMED — nothing could be read, so recall was unavailable. "
        f"This is NOT an empty scope and NOT 'nothing recorded yet'. Per-entry reasons "
        f"are on stdout; check a scope with `cairn validate --scope <scope>`, which "
        f"names each file that fails to parse."
    )


def generate(store: Path) -> dict:
    """Build the world, run the ORACLE'S reader over it, and return the fixture.

    🔴 THE HOST PATCH IS RESTORED, AND THE FIRST VERSION OF THIS FUNCTION DID NOT RESTORE
    IT. Both names have to be rebound — `store_host_line` looks the function up in
    `entry_shape`'s globals while `render_text` looks it up in `subsystem_recall`'s — and
    leaving them rebound is a GLOBAL side effect of an import-time fixture. Measured: six
    tests in two other modules failed, all of them asserting that a reader's output names
    THIS machine, because this module had quietly replaced the machine for the whole pytest
    process. A generator that mutates the library it is measuring has to put it back.
    """
    build_store(store)
    saved = (entry_shape.store_host, rc.store_host)
    entry_shape.store_host = lambda: HOST
    rc.store_host = lambda: HOST
    try:
        return _generate(store)
    finally:
        entry_shape.store_host, rc.store_host = saved


def _generate(store: Path) -> dict:
    out = {
        "_comment": [
            "GENERATED BY tests/reader_fixtures.py -- DO NOT EDIT.",
            "",
            "Every `expect` below is the ORACLE's own rendered bytes, so the Go test that",
            "replays this file is a DIFFERENTIAL measurement against the implementation it",
            "is a port of. The shapes here are the ones the HTTP conformance corpus cannot",
            "send; see the module docstring for the list.",
            "",
            "Regenerate with `python3 tests/reader_fixtures.py generate`.",
            "`tests/test_reader_fixtures.py` regenerates and diffs, so a stale fixture is a",
            "test failure rather than a silently weaker comparison.",
        ],
        "host": HOST,
        "store_placeholder": STORE_PLACEHOLDER,
        "scopes": SCOPES,
        # 🔴 EACH ENTRY CARRIES THE `st_mtime` CPython COMPUTED FOR IT, AS A HEX FLOAT.
        # The port's mtime is `float64(sec) + 1e-9*float64(nsec)` because that is CPython's
        # own expression, and `float64(nsec)/1e9` is NOT the same double for every nsec —
        # `1e-9` is not exactly representable. That claim was DOCUMENTED and untested: a
        # mutant swapping the two survived every comparison, because it takes a pair of
        # timestamps whose ORDER flips to make it visible. Recording the exact double here
        # makes it a direct measurement instead. Hex because a decimal round trip is the
        # thing under test.
        "entries": [
            {**entry, "st_mtime_hex": (store / entry["path"]).stat().st_mtime.hex()}
            for entry in ENTRIES
        ],
        # The two functions no rendered case can exercise properly, measured DIRECTLY
        # against CPython. Hex floats because a decimal round trip is part of what is under
        # test in the second table.
        "difflib_pairs": [
            {"a": a, "b": b, "ratio_hex": difflib.SequenceMatcher(None, a, b).ratio().hex()}
            for a, b in DIFFLIB_PAIRS
        ],
        "py_round_cases": [
            {"value_hex": v.hex(), "digits": 3, "rounded_hex": round(v, 3).hex()}
            for v in PY_ROUND_CASES
        ],
        "cases": [],
    }
    for case in CASES:
        row = dict(case)
        row["expect"] = _expect(case, store)
        out["cases"].append(row)
    return out


def dumps(fixture: dict) -> str:
    # `ensure_ascii=False` so the emoji in a badge is readable in the diff, and a
    # trailing newline so the file is a well-formed text file.
    return json.dumps(fixture, indent=1, ensure_ascii=False, sort_keys=False) + "\n"


def main(argv: list[str]) -> int:
    import tempfile

    action = argv[0] if argv else "generate"
    with tempfile.TemporaryDirectory(prefix="cairn-reader-fixtures-") as td:
        fixture = generate(Path(td) / "store")
    if action == "generate":
        FIXTURE.parent.mkdir(parents=True, exist_ok=True)
        FIXTURE.write_text(dumps(fixture), encoding="utf-8")
        print(f"wrote {FIXTURE} ({len(fixture['cases'])} cases, "
              f"{len(fixture['entries'])} entries)")
        return 0
    if action == "print":
        wanted = argv[1]
        for case in fixture["cases"]:
            if case["id"] == wanted:
                print("\n".join(case["expect"]["text_lines"]))
                return 0
        print(f"no such case: {wanted}", file=sys.stderr)
        return 2
    print(__doc__)
    return 2


if __name__ == "__main__":
    raise SystemExit(main(sys.argv[1:]))
