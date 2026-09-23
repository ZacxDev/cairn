#!/usr/bin/env python3
"""The P1 dual-run gate: BOTH SERVERS, ONE STORE, every route, scope by scope, entry by entry.

    python3 tests/dualrun/harness.py                      # the generated store (mode 2)
    python3 tests/dualrun/harness.py --store ~/some/store # the operator's own (mode 1)
    python3 tests/dualrun/harness.py --self-test          # the NEGATIVE controls, seven mutants
    python3 tests/dualrun/harness.py --break-both         # the PRE-FLIGHT's control (rc 2)
    python3 tests/dualrun/harness.py --positive-control   # the count MOVES with the store
    python3 tests/dualrun/harness.py --only recall-digest:alpha-index --keep

🔴 WHY THIS EXISTS WHEN THE CONFORMANCE CORPUS IS GREEN FOR BOTH SERVERS. The corpus
replays a DECLARED list of requests against a DECLARED world and compares each answer to a
recorded golden. It is the reason both servers are trustworthy on the cases it encodes, and
it is silent about everything else — `tests/conformance/README.md` lists what that leaves
out, and the list is long. This gate asks the other question: given ONE store that nobody
wrote a fixture for, do the two implementations return the same bytes for every route, for
every scope, for every entry, for every principal?

🔴 THE COMPARISON IS AGAINST THE OTHER SERVER, LIVE, NEVER AGAINST A RECORDED GOLDEN. Both
processes are started here, over one store root, with one `CAIRN_HOST`, one token file. A
golden would let the Go server agree with a snapshot of an older oracle and have that called
byte-identity; this cannot.

🔴 AND THE HEADLINE NUMBER IS A PAIR, NEVER THE ZERO ALONE. `SUMMARY targets=N
comparisons=M differences=0` — because two servers failing identically compare equal, and
this repository has already measured a gate reporting 72 PASS / 0 FAIL while every request
was refused. The controls that stand between this gate and that state are named in
`tests/dualrun/README.md` under "Validating the instrument", and each refuses with exit
**2** — "could not vouch" — rather than reporting a pass.

🔴 BYTE-IDENTITY HERE MEANS THE *UNCOMPRESSED* TAR, AND THE SCOPING IS MEASURED RATHER
THAN CHOSEN. `/api/v1/snapshot` ships `tarfile.open(mode="w:gz")` on the oracle and
`compress/gzip` in Go, and the two cannot be made equal at any setting: Go hardcodes the
gzip header's OS byte to `0xff` with no API to change it, and the DEFLATE stream differs in
LENGTH as well as content because `compress/flate` and zlib make different block choices.
Both are permitted freedoms of the format. So the gzip envelope — and therefore
`Content-Length` on that one route — is a DECLARED difference, and the tar INSIDE it is
compared byte for byte. See `DIFFERENCES` below and `AGENTS.md`, which records the
measurement.
"""
from __future__ import annotations

import argparse
import datetime as _dt
import difflib
import gzip
import http.client
import io
import json
import os
import re
import shutil
import socket
import subprocess
import sys
import tarfile
import tempfile
import time
from dataclasses import dataclass, field
from pathlib import Path

HERE = Path(__file__).resolve().parent
ROOT = HERE.parents[1]
sys.path.insert(0, str(HERE))

import genstore  # noqa: E402
import mutants as M  # noqa: E402

BOOT_TIMEOUT_S = 30.0

#: The host identity BOTH servers print. 🔴 SET EXPLICITLY, FOR TWO INDEPENDENT REASONS:
#: every rendered report names the machine whose disk it read — deliberately — so an unset
#: label would put this host's real name into a run log that gets pasted into a pull request
#: on a PUBLIC repository; and it is the one input that makes the two implementations'
#: host line comparable without either being patched. ⚠ It is NOT what makes the two
#: agree: both read `CAIRN_HOST` and both append the same machine-id prefix, so the line
#: would match anyway. This keeps the value out of the log.
DUALRUN_HOST = "dualrun-gate"

#: The three principals. 🔴 ONE OF THEM IS NARROWER THAN THE STORE, ON PURPOSE. A gate that
#: only ever sent an unrestricted credential would compare the happy path and nothing about
#: the narrowing — and the narrowing is where the most recent real defect lived (a token
#: loader that granted a full-store read on input the oracle refuses). `narrow-reader` sees
#: TWO scopes of nine; every other scope must answer it exactly what a never-existed scope
#: answers, on BOTH servers, and that equality is what is compared.
#:
#: 🔴 43 CHARACTERS EACH, BECAUSE THE SERVER REFUSES TO START ON A SHORTER ROW. These are
#: synthetic filler — a repeated literal precisely so nobody can mistake one for a
#: credential that ever authorised anything — and they are minted into a file under a temp
#: directory, never written to a tracked path.
WIDE_TOKEN = "dualrun-synthetic-wide-token-00000000000000"
NARROW_TOKEN = "dualrun-synthetic-narrow-token-000000000000"
LEGACY_TOKEN = "dualrun-synthetic-legacy-token-000000000000"

WIDE = "wide-writer"
NARROW = "narrow-reader"
LEGACY = "legacy"

#: The scopes `narrow-reader` may see, for the GENERATED store. In mode 1 the allowlist is
#: derived from the store's own first two scopes — see `token_file`.
NARROW_SCOPES = ("beta-index", "iota-single")


# =============================================================================
# Declared differences — each a licence to differ, each REQUIRED to fire
# =============================================================================

@dataclass
class Difference:
    """One declared licence to differ, with the reason it is granted.

    🔴 A DECLARED LICENCE THAT MATCHED NOTHING IS A FAILURE, NOT A CONVENIENCE. A row that
    keeps a licence it no longer needs stops comparing that field invisibly — and the
    reverse is just as interesting: a licence that stops firing is a difference that has
    been CLOSED, which is something somebody should be told about rather than left to
    read as a pass. Both directions are reported on the `DIFFERENCE` lines.
    """

    name: str
    where: str
    why: str
    fired: bool = False

    def fire(self) -> None:
        self.fired = True


def differences() -> dict[str, Difference]:
    rows = [
        Difference(
            name="date-header",
            where="every response",
            why="`Date` is required by HTTP/1.1 and is the MOMENT of the response. Two "
                "servers answering the same question a millisecond apart legitimately "
                "disagree about it. Dropped on both sides; nothing else about the header "
                "set is relaxed.",
        ),
        Difference(
            name="snapshot-gzip-envelope",
            where="`/api/v1/snapshot` — the body's gzip framing and `Content-Length`",
            why="MEASURED UNATTAINABLE, not conceded. Go's `compress/gzip` hardcodes the "
                "10-byte header's OS field to 0xff with no API to change it (CPython "
                "writes 0x03), and the DEFLATE stream differs in LENGTH as well as "
                "content — 512 bytes from Go against 511 from zlib at level 9 on one "
                "20,480-byte tar — because `compress/flate` and zlib make different match "
                "and block choices. Both are permitted freedoms of the format. The TAR "
                "INSIDE is compared byte for byte instead, which is the property "
                "`AGENTS.md` names; "
                "`Content-Length` counts the gzip bytes, so it goes with them.",
        ),
        Difference(
            name="json-decoder-diagnostic",
            where="the append route's `body must be JSON (...)` message",
            why="the parenthesised tail is CPython's `json` diagnostic on one side and "
                "`encoding/json`'s on the other. The corpus rules the same way and marks "
                "its own row `oracle_only`; here the row is still COMPARED up to the open "
                "parenthesis — the status, every header but `Content-Length`, and the "
                "message a caller greps — and only the library's sentence is excused.",
        ),
        Difference(
            name="audit-timestamp-instant",
            where="the audit stream's `ts=` VALUE",
            why="two servers stamp two instants. Every DIGIT is replaced by `0` rather "
                "than the field being dropped, so the SPELLING is still compared — which "
                "is the point: `2026-01-02T03:04:05+00:00` and `...Z` are both RFC 3339 "
                "and are not the same bytes, and this gate is the only one that reads "
                "that stream at all.",
        ),
        Difference(
            name="listen-port",
            where="the startup banner's `listening on <host>:<port>`",
            why="two servers cannot share one port. Everything else on that line — the "
                "store root, the token fingerprints and identities in file order, the "
                "lockout settings, the trusted-proxy set, `reload=SIGHUP` — is compared "
                "literally, and so is the legacy-mode warning above it.",
        ),
    ]
    return {row.name: row for row in rows}


# =============================================================================
# Targets
# =============================================================================

#: How a target's answer is compared. 🔴 NAMED PER TARGET, NEVER DEFAULTED TO THE NARROW
#: ONE: a target compared less than fully is a target whose bytes are allowed to differ,
#: and the reason has to sit beside it or the gate quietly shrinks.
CMP_ALL = "all"           # status + reason, every header but `Date`, the body bytes
CMP_TAR = "tar"           # …minus `Content-Length`, and the body is the UNCOMPRESSED tar
CMP_LIBDIAG = "libdiag"   # …and the body is compared up to the library's own diagnostic


@dataclass(frozen=True)
class Target:
    """One request, issued to both servers and compared."""

    id: str
    why: str
    method: str
    path: str
    principal: str
    #: `"read"` or `"write"`. 🔴 READS BEFORE WRITES, AND THE STORE IS RESTORED AROUND
    #: EVERY WRITE. A successful write moves entry mtimes and every report body carries
    #: `entry-files=N` and `newest=<mtime>`, so one write leaking into a later read would
    #: make the two servers disagree about a store that had changed under them.
    phase: str = "read"
    body: bytes | None = None
    headers: dict[str, str] = field(default_factory=dict)
    compare: str = CMP_ALL
    #: Which arm this target belongs to, for the self-test's attribution.
    arm: str = "scope"
    #: Derive `If-Match` from the entry's CURRENT revision before issuing.
    #:
    #: 🔴 IT EXISTS BECAUSE A SUCCESSFUL REPLACE CANNOT BE ADDRESSED WITHOUT ONE, and a
    #: write route whose only compared outcomes are refusals is a route compared at its
    #: refusals. The revision is read off the `ETag` a deliberately STALE `If-Match` gets
    #: back with its 412 — the server sends the current revision there precisely so a client
    #: that cannot retry does not re-send without the precondition. Each server derives its
    #: OWN, so a disagreement about the revision shows up as a different answer to the real
    #: PUT rather than being papered over by one side's value.
    derive_if_match: bool = False


TOKENS = {WIDE: WIDE_TOKEN, NARROW: NARROW_TOKEN, LEGACY: LEGACY_TOKEN}

#: A search term the generated world spells, and one nothing spells. In mode 1 the hit term
#: is a word the store is overwhelmingly likely to contain; a miss on both servers is still
#: a compared answer, so neither choice can make the gate vacuous — only less interesting.
HIT_TERM = "synthetic"
MISS_TERM = "zzzznothingatall"
#: A near miss of `HIT_TERM`, so the sub-threshold rungs are reached with `threshold=0.3`.
NEAR_TERM = "synthetik"


def scope_targets(scope: str) -> list[Target]:
    """The per-scope sweep, for `wide-writer`.

    🔴 THE PARAMETERS THAT CHANGE THE OUTPUT SHAPE ARE ALL HERE, because a route compared
    at one parameter value is a route compared at one parameter value. `mode` selects three
    different renderers' worth of output, `limit` and `page` move what is printed AND — on
    a paginated index — the ref column's WIDTH, and search's four knobs each move the hunk
    set or the window around it.
    """
    q = f"?q={HIT_TERM}"
    return [
        Target(f"recall-digest:{scope}", "the default digest: every badge, the featured "
               "pick, the reject block and the omission notice", "GET",
               f"/api/v1/recall/{scope}", WIDE),
        Target(f"recall-list:{scope}", "the index alone, whose ORDER is mtime-derived",
               "GET", f"/api/v1/recall/{scope}?mode=list", WIDE),
        Target(f"recall-full:{scope}", "`mode=full` prints BODIES, which is the only shape "
               "that renders a non-featured entry's text at scope level", "GET",
               f"/api/v1/recall/{scope}?mode=full&limit=3", WIDE),
        Target(f"recall-full-wide:{scope}", "the same with room to spare, so the truncation "
               "notice's ABSENCE is compared too", "GET",
               f"/api/v1/recall/{scope}?mode=full&limit=9999", WIDE),
        Target(f"recall-limit-1:{scope}", "the tightest cap, which is where the omission "
               "notice's arithmetic is visible", "GET",
               f"/api/v1/recall/{scope}?limit=1", WIDE),
        Target(f"recall-page-2:{scope}", "page 2 — a real page on a paginated scope and a "
               "past-the-end block on every other, which are different branches", "GET",
               f"/api/v1/recall/{scope}?mode=list&page=2", WIDE),
        Target(f"recall-page-far:{scope}", "a page that cannot exist: no arithmetic, and "
               "the notice names the page the caller asked for", "GET",
               f"/api/v1/recall/{scope}?mode=list&page=9999", WIDE),
        Target(f"recall-bad-mode:{scope}", "the validation ladder's third rung, whose "
               "message quotes a Python tuple", "GET",
               f"/api/v1/recall/{scope}?mode=telepathy", WIDE),
        Target(f"recall-head:{scope}", "HEAD reports the `Content-Length` its GET would "
               "have sent, and sends no body", "HEAD", f"/api/v1/recall/{scope}", WIDE),
        Target(f"search-hit:{scope}", "a hit, its rung and its context window", "GET",
               f"/api/v1/search/{scope}{q}", WIDE),
        Target(f"search-miss:{scope}", "a term nothing scores on, which is its own sentence",
               "GET", f"/api/v1/search/{scope}?q={MISS_TERM}", WIDE),
        Target(f"search-tuned:{scope}", "a NEAR miss under a lowered threshold, a capped "
               "hit count and a line-count context window — three knobs at once, because "
               "each moves a different part of the output", "GET",
               f"/api/v1/search/{scope}?q={NEAR_TERM}&threshold=0.3&max_hits=3&context=2",
               WIDE),
        # 🔴 A SECOND VALUE FOR EVERY SEARCH KNOB, BECAUSE ONE VALUE IS NOT A VARIATION.
        # Found by a mutation sweep over this gate's own guards: a mutant that moved
        # `threshold=0.3` to `0.6` — the DEFAULT, so the parameter stops changing anything —
        # survived a guard whose sentence was "every parameter the server reads is sent".
        # Sending a parameter at one value compares its PARSING and nothing about its
        # EFFECT, and the two renderers are what this gate exists to compare.
        Target(f"search-strict:{scope}", "the same near miss under a STRICTER threshold, "
               "which is where the sub-threshold near-miss report lives rather than the hit",
               "GET", f"/api/v1/search/{scope}?q={NEAR_TERM}&threshold=0.95", WIDE),
        Target(f"search-maxhits-1:{scope}", "`max_hits=1` is the truncation notice's "
               "tightest cap, and a different number from the tuned row's 3", "GET",
               f"/api/v1/search/{scope}{q}&max_hits=1", WIDE),
        Target(f"search-context-0:{scope}", "`context=0` is a LINE count of zero, which is "
               "a different value from the tuned row's 2 and from the `-1` sentinel meaning "
               "`the whole bullet` that every other row gets by default", "GET",
               f"/api/v1/search/{scope}{q}&context=0", WIDE),
        Target(f"search-all-scopes-off:{scope}", "`all_scopes=0` EXPLICITLY, which is a "
               "different code path from the parameter being absent — the server branches "
               "on the literal `0`/``/`false` rather than on presence", "GET",
               f"/api/v1/search/{scope}{q}&all_scopes=0", WIDE),
        Target(f"search-head:{scope}", "the search route's HEAD", "HEAD",
               f"/api/v1/search/{scope}{q}", WIDE),
        Target(f"snapshot-scope:{scope}", "the snapshot NARROWED to one scope, whose "
               "member set and `X-Store-Entries` describe the filtered set", "GET",
               f"/api/v1/snapshot?scope={scope}", WIDE, compare=CMP_TAR, arm="tar"),
    ]


def narrowing_targets(scope: str, narrow_scopes: tuple[str, ...]) -> list[Target]:
    """The same scope, asked by a principal whose allowlist may not contain it.

    🔴 THE CLAIM IS THAT A REFUSED SCOPE ANSWERS EXACTLY WHAT A NEVER-EXISTED ONE ANSWERS,
    AND THAT BOTH SERVERS SAY THE SAME THING. Those are two different properties and this
    gate owns the second: the corpus owns the first. A scope INSIDE the allowlist is
    included as well, so the run compares the narrowing in both directions rather than only
    the refusal — a gate that only ever hit refused scopes would compare two uniform
    answers and learn nothing.
    """
    inside = "inside" if scope in narrow_scopes else "REFUSED"
    return [
        Target(f"narrow-recall:{scope}", f"`narrow-reader` on a scope {inside} its "
               "allowlist", "GET", f"/api/v1/recall/{scope}", NARROW, arm="narrow"),
        Target(f"narrow-search:{scope}", f"the search route, {inside} the allowlist", "GET",
               f"/api/v1/search/{scope}?q={HIT_TERM}", NARROW, arm="narrow"),
        Target(f"legacy-recall:{scope}", "a LEGACY bare row: unrestricted read, and "
               "forbidden to write. Its reads must be identical on both servers too",
               "GET", f"/api/v1/recall/{scope}", LEGACY, arm="narrow"),
    ]


def entry_targets(scope: str, refs: list[str], principal: str = WIDE) -> list[Target]:
    """One `?ref=` render per indexed ref — the per-entry sweep.

    🔴 IT IS WHAT `server/verify-byte-identity.sh` DOES, AND IT IS HERE FOR THE SAME
    REASON: a `?ref=` run is a NARROWING that prints that one entry in full and NO index.
    ⚠ AND ITS INDEPENDENCE IS PARTIAL, WHICH IS SAID HERE RATHER THAN LEFT TO BE
    ASSUMED. `recall-full-wide` above prints every entry's body too, so a pure BODY
    difference is visible at scope level as well. What only this arm reaches is
    `resolve_ref_tiered` — once per real ref, through the filename tier, the alias tier,
    the `<slug>.<kind>` form and the ambiguous-ref refusal — which no scope-level render
    calls at all. The mutation battery pins that: `resolver-tier-keyed-on-ref` is killed
    by these targets and by NOTHING else.

    🔴 THE ID CARRIES THE PRINCIPAL, AND LEAVING IT OUT WAS A MEASURED DEFECT IN THIS
    HARNESS RATHER THAN A STYLE POINT. The narrowed principal's entry sweep addresses the
    same `<scope>/<ref>` pairs as the wide one, so without it three targets shared an id
    with three others — and the difference count, which is `len(set(failures))`, reported
    **313** on a mutant that made all **316** targets differ. An id collision silently
    UNDER-REPORTS the headline number and makes `--only` ambiguous about which principal it
    meant. `run_once` now refuses a duplicate id outright.
    """
    return [
        Target(f"entry:{principal}:{scope}/{ref}", "one entry's own single-ref render, "
               "which carries no index and therefore no mtime-derived order", "GET",
               f"/api/v1/recall/{scope}?ref={_q(ref)}", principal, arm="entry")
        for ref in refs
    ]


def search_by_ref_targets(scope: str, refs: list[str]) -> list[Target]:
    """A search whose term is a REF the scope actually indexes.

    🔴 IT EXISTS BECAUSE `HIT_TERM` IS A GUESS, AND IN MODE 1 IT IS PROBABLY A WRONG ONE.
    `synthetic` is a word the GENERATED world spells; a real store almost certainly does not,
    so without this every `search-*` target in mode 1 would compare a MISS — a correct
    comparison of the sentence a miss produces, and nothing about the hit path, the hunk
    ranking or the context window. A ref is guaranteed to score, because the reader matches on
    the entry NAME as well as on its content, and the ref list is already read out of both
    servers' indexes.

    ⚠ It is a NAME-tier hit, not a content hit. What it cannot stand in for is a term that
    scores on the BODY, which in mode 1 remains a guess — and `run_once` refuses to vouch for
    a run in which neither server produced a single `search-hit` at all, which is the floor
    that makes the difference visible rather than assumed.
    """
    if not refs:
        return []
    return [
        Target(f"search-by-ref:{scope}", "a search whose term is a ref the scope really "
               "indexes, so the HIT path is reached on any store — a name-tier hit, which "
               "outranks a content hit of the same score", "GET",
               f"/api/v1/search/{scope}?q={_q(refs[0])}", WIDE),
    ]


def store_targets(scopes: list[str], narrow_scopes: tuple[str, ...]) -> list[Target]:
    """The whole-store routes and every refusal that is not about one scope."""
    absent = "dualrun-scope-that-never-existed"
    first = scopes[0] if scopes else absent
    return [
        Target("snapshot-wide", "the whole store, as the unrestricted-in-practice caller "
               "sees it", "GET", "/api/v1/snapshot", WIDE, compare=CMP_TAR, arm="tar"),
        Target("snapshot-narrow", "🔴 THE SAME ROUTE THROUGH A NARROWED ALLOWLIST. The "
               "snapshot's candidate list is filtered by the same predicate the reports "
               "use, so this is where a token loader that granted a full-store read would "
               "show up — as a tar with more members than the allowlist has scopes",
               "GET", "/api/v1/snapshot", NARROW, compare=CMP_TAR, arm="tar"),
        Target("snapshot-legacy", "a legacy bare row is UNRESTRICTED, which is the opposite "
               "end of the same predicate", "GET", "/api/v1/snapshot", LEGACY,
               compare=CMP_TAR, arm="tar"),
        Target("snapshot-head", "HEAD on the snapshot: no body, and the length it would "
               "have sent", "HEAD", "/api/v1/snapshot", WIDE, compare=CMP_TAR, arm="tar"),
        Target("snapshot-scope-absent", "a `?scope=` naming nothing: an empty archive is a "
               "different answer from a refusal, and both servers must pick the same one",
               "GET", f"/api/v1/snapshot?scope={absent}", WIDE, compare=CMP_TAR, arm="tar"),
        Target("snapshot-scope-unsafe", "a `?scope=` value that reaches the filesystem: "
               "refused, because the caller is authenticated and may be told",
               "GET", "/api/v1/snapshot?scope=%2e%2e", WIDE, arm="refusal"),
        Target("recall-scope-absent", "a scope that never existed, with the known-scope "
               "list the report prints", "GET", f"/api/v1/recall/{absent}", WIDE,
               arm="refusal"),
        Target("search-scope-absent", "the search side of the same absence", "GET",
               f"/api/v1/search/{absent}?q={HIT_TERM}", WIDE, arm="refusal"),
        Target("search-all-scopes", "`all_scopes=1` NAMES NO SCOPE, so the narrowing is the "
               "only thing that bounds it — `all scopes` means all the CALLER's", "GET",
               f"/api/v1/search/{first}?q={HIT_TERM}&all_scopes=1", WIDE),
        Target("search-all-scopes-narrow", "the same request through the narrowed "
               "allowlist, which is the half a wide token cannot measure", "GET",
               f"/api/v1/search/{narrow_scopes[0]}?q={HIT_TERM}&all_scopes=1", NARROW,
               arm="narrow"),
        Target("search-no-q", "`q` is required and a present-but-whitespace value is how a "
               "required parameter is most often lost", "GET",
               f"/api/v1/search/{first}?q=%20%20", WIDE, arm="refusal"),
        Target("recall-bad-limit", "a non-integer parameter REFUSES rather than silently "
               "defaulting, and the message quotes the value as Python would", "GET",
               f"/api/v1/recall/{first}?limit=abc", WIDE, arm="refusal"),
        Target("recall-limit-zero", "the ladder's FIRST rung", "GET",
               f"/api/v1/recall/{first}?limit=0", WIDE, arm="refusal"),
        Target("recall-page-zero", "the ladder's SECOND rung, which a valid limit still "
               "reaches", "GET", f"/api/v1/recall/{first}?page=0", WIDE, arm="refusal"),
        Target("recall-repeated-param", "`?limit=1&limit=2` means 2 — last wins, which is a "
               "contract a port must reproduce rather than a convenience", "GET",
               f"/api/v1/recall/{first}?mode=list&limit=1&limit=2", WIDE),
        Target("recall-unsafe-component", "a path component that decodes to `..` and would "
               "reach the filesystem", "GET", "/api/v1/recall/%2e%2e", WIDE, arm="refusal"),
        Target("no-route", "a path inside the API prefix that no table dispatches", "GET",
               "/api/v1/telepathy", WIDE, arm="refusal"),
        Target("non-api-path", "a path outside the prefix, with a VALID credential: the "
               "same uniform 401, because a 404 here would map the URL space", "GET",
               "/nope", WIDE, arm="refusal"),
        Target("health", "the one unauthenticated route, which must say nothing at all",
               "GET", "/healthz", WIDE, arm="refusal"),
        Target("unhandled-method", "an unhandled verb is the uniform 401 and is METERED, "
               "not a 501 page echoing the verb", "PATCH",
               f"/api/v1/recall/{first}", WIDE, body=b"", arm="refusal"),
        Target("write-on-read-route", "a POST to a read-only route is 405 with `Allow`",
               "POST", "/api/v1/snapshot", WIDE, body=b"{}",
               headers={"Content-Type": "application/json"}, arm="refusal"),
        Target("bad-token", "a credential that is not in the file", "GET",
               f"/api/v1/recall/{first}", "__bad__", arm="refusal"),
        Target("no-token", "no credential at all: byte-identical to a wrong one", "GET",
               f"/api/v1/recall/{first}", "__none__", arm="refusal"),
        Target("malformed-auth", "an `Authorization` header that is not a Bearer token",
               "GET", f"/api/v1/recall/{first}", "__malformed__", arm="refusal"),
    ]


def entry_body(scope: str, ref: str) -> bytes:
    """A body the entry-shape validator accepts, for `<scope>/<ref>`.

    🔴 IT IS CONFORMANT ON PURPOSE, AND AN EMPTY BODY WAS A MEASURED HOLE. The PUT targets
    first sent `b""`, so `create-new` answered **422 entry-shape** on both servers and the
    gate never saw a **201** at all — the one status no read route returns, and the one the
    parity gate found a real defect on (`urllib` raises only outside 200-299, so a
    `!= 200` test called a successful create a failure at exit 6). Two servers agreeing on a
    422 is a real comparison of the refusal and says nothing about the write.

    ⚠ In mode 1 the ref is whatever the store indexes, and a `<slug>.<kind>` ref will not
    match a body whose `service:` is the whole ref — so that target may answer 422 on a real
    store. That is still a compared answer; what keeps the SUCCESS path covered either way is
    `create-new`, whose ref this harness chooses.
    """
    slug = ref.split(".", 1)[0]
    return (
        "---\n"
        f"service: {slug}\n"
        f"scope: {scope}\n"
        "---\n"
        "\n"
        "## What it is\n"
        "\n"
        "written by the dual-run gate.\n"
        "\n"
        "## Pointers\n"
        "\n"
        f"- `apps/{slug}/values.yaml`\n"
        "\n"
        "## Nuance / work-history\n"
        "\n"
        "- 2000-01-02: written by the dual-run gate.\n"
    ).encode("utf-8")


def write_targets(scope: str, ref: str, absent_scope: str, refused_scope: str,
                  new_ref: str) -> list[Target]:
    """The two write routes, and every precondition branch of the PUT.

    🔴 THE STORE IS RESTORED BEFORE EACH SERVER'S REQUEST, NOT ONCE PER TARGET. The two
    servers run in sequence against one store root, so without the restore the second
    would see the first one's write: an append would be answered `duplicate` (the server
    recognises a bullet by content hash) and a create would be answered `already-exists`.
    Both are correct answers to a different question, and the harness would report a
    divergence the code does not have — which is the shape the parity gate measured.
    """
    js = {"Content-Type": "application/json"}
    body = entry_body(scope, ref)
    new_body = entry_body(scope, new_ref)

    def bullet(text: str, session: str = "dualrun") -> bytes:
        return json.dumps({"text": text, "session": session}).encode()

    return [
        Target("append-ok", "a bullet lands, the status token is printed, and the ETag is "
               "the new revision", "POST",
               f"/api/v1/entry/{scope}/{_q(ref)}/bullets", WIDE, phase="write",
               body=bullet("a synthetic dual-run bullet."), headers=js, arm="write"),
        Target("append-actor-in-body-is-DISCARDED",
               "🔴 AN `actor` KEY IS ACCEPTED AND THROWN AWAY. A client-supplied actor "
               "would let any token-holder attribute a bullet to somebody else, so the "
               "rendered trailer must name the AUTHENTICATED identity on both servers — "
               "which is a claim about the body bytes, not about the status", "POST",
               f"/api/v1/entry/{scope}/{_q(ref)}/bullets", WIDE, phase="write",
               body=json.dumps({"text": "a bullet naming a hostile actor.",
                                "session": "dualrun", "actor": "somebody-else"}).encode(),
               headers=js, arm="write"),
        Target("append-astral",
               "🔴 AN ASTRAL CHARACTER AS AN ESCAPED SURROGATE PAIR, WHICH IS WHAT "
               "`json.dumps` PUTS ON THE WIRE BY DEFAULT. A guard that could not tell a "
               "pair from a LONE surrogate 400'd every one of these", "POST",
               f"/api/v1/entry/{scope}/{_q(ref)}/bullets", WIDE, phase="write",
               body=json.dumps({"text": "a bullet with an astral character: \U0001F5FA.",
                                "session": "dualrun"}).encode(), headers=js, arm="write"),
        Target("append-over-the-cap", "a bullet over the server's own text cap is refused "
               "with the overage named", "POST",
               f"/api/v1/entry/{scope}/{_q(ref)}/bullets", WIDE, phase="write",
               body=bullet("x" * 4001), headers=js, arm="write"),
        Target("append-not-json", "a body that is not JSON. 🔴 THE ONE TARGET WHOSE BODY IS "
               "NOT COMPARED IN FULL: the parenthesised tail is the decoding library's own "
               "sentence — see the `json-decoder-diagnostic` licence", "POST",
               f"/api/v1/entry/{scope}/{_q(ref)}/bullets", WIDE, phase="write",
               body=b"{nope", headers=js, compare=CMP_LIBDIAG, arm="write"),
        Target("append-empty-body", "a zero-length body, which is a framing the server "
               "accepts and a payload it refuses", "POST",
               f"/api/v1/entry/{scope}/{_q(ref)}/bullets", WIDE, phase="write",
               body=b"", headers=js, compare=CMP_LIBDIAG, arm="write"),
        Target("append-unknown-ref", "a ref the index does not resolve", "POST",
               f"/api/v1/entry/{scope}/dualrun-ref-that-never-existed/bullets", WIDE,
               phase="write", body=bullet("x"), headers=js, arm="write"),
        Target("append-absent-scope", "a scope that never existed, on the WRITE path. 🔴 IT "
               "IS THE PAIR PARTNER OF `append-narrow-refused-scope` BELOW, and the pair is "
               "the point: refused must answer what absent answers, and the earlier version "
               "of this row pointed at a scope the WIDE principal can see, so it answered "
               "`200 appended` and compared nothing about a refusal at all", "POST",
               f"/api/v1/entry/{absent_scope}/{_q(ref)}/bullets", WIDE, phase="write",
               body=bullet("x"), headers=js, arm="write"),
        Target("append-narrow-refused-scope", "the narrowed principal on a scope that EXISTS "
               "and is outside its allowlist — the other half of the pair above", "POST",
               f"/api/v1/entry/{refused_scope}/{_q(ref)}/bullets", NARROW, phase="write",
               body=bullet("x"), headers=js, arm="write"),
        Target("append-narrow-absent-scope", "…and the narrowed principal on a scope that "
               "never existed, so the pair is compared from BOTH principals' sides", "POST",
               f"/api/v1/entry/{absent_scope}/{_q(ref)}/bullets", NARROW, phase="write",
               body=bullet("x"), headers=js, arm="write"),
        Target("append-legacy-forbidden", "🔴 A LEGACY BARE ROW MAY NOT WRITE: its identity "
               "names no holder, so a bullet written with it could record no actor",
               "POST", f"/api/v1/entry/{scope}/{_q(ref)}/bullets", LEGACY, phase="write",
               body=bullet("x"), headers=js, arm="write"),
        Target("put-no-precondition", "🔴 A PRECONDITION IS REQUIRED. An optional one is no "
               "precondition, and this is the 428 that says so", "PUT",
               f"/api/v1/entry/{scope}/{_q(ref)}", WIDE, phase="write",
               body=body, arm="write"),
        Target("put-stale-if-match", "a revision that is not the current one: 412, the file "
               "UNCHANGED, and the current revision on the response so a client can retry",
               "PUT", f"/api/v1/entry/{scope}/{_q(ref)}", WIDE, phase="write",
               body=body, headers={"If-Match": '"0000000000000000"'}, arm="write"),
        Target("put-correct-if-match",
               "🔴 THE ONE WRITE TARGET THAT REPLACES SOMETHING. Its `If-Match` is derived "
               "from the entry's CURRENT revision by each server SEPARATELY, so a "
               "disagreement about the revision shows up here as a different answer rather "
               "than being papered over by one side's value. Without it the PUT route was "
               "compared at five refusals and no write at all", "PUT",
               f"/api/v1/entry/{scope}/{_q(ref)}", WIDE, phase="write", body=body,
               arm="write", derive_if_match=True),
        Target("put-if-match-star", "`If-Match: *` matches any revision, which is the one "
               "value that turns the guard off while looking like it is on", "PUT",
               f"/api/v1/entry/{scope}/{_q(ref)}", WIDE, phase="write", body=body,
               headers={"If-Match": "*"}, arm="write"),
        Target("put-both-preconditions", "both headers together is a contradiction and is "
               "refused rather than resolved", "PUT",
               f"/api/v1/entry/{scope}/{_q(ref)}", WIDE, phase="write", body=body,
               headers={"If-Match": '"abcdef0123456789"', "If-None-Match": "*"},
               arm="write"),
        Target("put-if-none-match-list", "a LIST of entity-tags there would mean `overwrite "
               "unless it is one of these`, which is a blind overwrite spelled backwards",
               "PUT", f"/api/v1/entry/{scope}/{_q(ref)}", WIDE, phase="write", body=body,
               headers={"If-None-Match": '"abcdef0123456789", "0123456789abcdef"'},
               arm="write"),
        Target("create-new", "a NEW entry behind `If-None-Match: *`, which answers 201 — a "
               "status no read route ever returns, and the one the P2 parity gate found a "
               "real defect on", "PUT",
               f"/api/v1/entry/{scope}/{new_ref}", WIDE, phase="write",
               body=new_body, headers={"If-None-Match": "*"}, arm="write"),
        Target("create-existing", "the same precondition against a ref that DOES exist: "
               "412 with a status token that is not `precondition-failed`", "PUT",
               f"/api/v1/entry/{scope}/{_q(ref)}", WIDE, phase="write", body=body,
               headers={"If-None-Match": "*"}, arm="write"),
    ]


def _q(value: str) -> str:
    """Percent-encode a value for a query string or one path component."""
    from urllib.parse import quote
    return quote(value, safe="")


# =============================================================================
# Running the pair
# =============================================================================

@dataclass(frozen=True)
class Answer:
    status: int
    reason: str
    headers: tuple[tuple[str, str], ...]
    body: bytes


def free_port() -> int:
    s = socket.socket()
    s.bind(("127.0.0.1", 0))
    port = s.getsockname()[1]
    s.close()
    return port


def token_file(path: Path, narrow_scopes: tuple[str, ...], all_scopes: list[str]) -> Path:
    """Mint the token file both servers are configured with.

    🔴 ONE FILE, BOTH SERVERS. A gate whose two servers held two token tables would be
    comparing two authorization decisions as well as two implementations, and a difference
    would be unattributable.

    🔴 AND `wide-writer` IS A MAPPED ROW RATHER THAN A BARE ONE, BECAUSE OF THE WRITE
    VERBS. A bare token resolves to `scopes is None` — unrestricted — and is FORBIDDEN to
    write, so every write target would compare two identical refusals and measure nothing
    about the write path. The legacy row is present as its OWN principal for that case.
    """
    rows = [
        f"{WIDE_TOKEN} {WIDE} {','.join(all_scopes)}",
        f"{NARROW_TOKEN} {NARROW} {','.join(narrow_scopes)}",
        LEGACY_TOKEN,
    ]
    path.write_text("\n".join(rows) + "\n", encoding="utf-8")
    path.chmod(0o600)
    return path


def server_env(break_both: bool) -> dict[str, str]:
    env = dict(os.environ)
    env.update({
        # The lockout answers the SAME uniform 401 a bad token does — that is the design —
        # so once it trips a correctly authorised request also answers 401 and every target
        # after it compares two wrong answers to each other. This sweep issues several
        # deliberate refusals from one address, well over the production default of five
        # per minute.
        "CAIRN_MAX_FAILURES": "1000000",
        # 🔴 THE TRUSTED-PROXY SET MUST NOT CONTAIN THE HARNESS, AND COPYING THE
        # CONFORMANCE RUNNER'S VALUE IS WHAT MADE THE P2 PARITY GATE VACUOUS.
        # `127.0.0.1/32` tells the server the loopback peer is a PROXY, after which every
        # DIRECT request is refused `401 status=no-client-ip` — and two servers refusing
        # identically compare equal. So the set names an address the harness cannot be:
        # TEST-NET-1, reserved by RFC 5737 and assigned to nobody.
        # 🔴 `--break-both` PUTS THE LOOPBACK BACK, WHICH IS THE INCIDENT. It exists as a
        # CONTROL and nothing else: with it every target compares two identical 401s, and
        # the pre-flight has to refuse to vouch instead of reporting that as a green.
        "CAIRN_TRUSTED_PROXIES": "127.0.0.1/32" if break_both else "192.0.2.1/32",
        "CAIRN_HOST": DUALRUN_HOST,
    })
    return env


def start_oracle(server_py: Path, store: Path, tokens: Path, log: Path, port: int,
                 env: dict[str, str]) -> subprocess.Popen:
    handle = log.open("wb")
    return subprocess.Popen(
        [sys.executable, str(server_py), "--store", str(store), "--host", "127.0.0.1",
         "--port", str(port), "--token-file", str(tokens)],
        stdout=handle, stderr=subprocess.STDOUT, env=env)


def start_go(binary: Path, store: Path, tokens: Path, log: Path, port: int,
             env: dict[str, str]) -> subprocess.Popen:
    handle = log.open("wb")
    return subprocess.Popen(
        [str(binary), "--store", str(store), "--host", "127.0.0.1",
         "--port", str(port), "--token-file", str(tokens)],
        stdout=handle, stderr=subprocess.STDOUT, env=env)


def wait_for_health(port: int, proc: subprocess.Popen, log: Path, label: str) -> None:
    deadline = time.time() + BOOT_TIMEOUT_S
    while time.time() < deadline:
        if proc.poll() is not None:
            raise RuntimeError(f"the {label} server exited {proc.returncode} before "
                               f"answering /healthz:\n" + log.read_text(errors="replace"))
        try:
            conn = http.client.HTTPConnection("127.0.0.1", port, timeout=1)
            conn.request("GET", "/healthz")
            resp = conn.getresponse()
            resp.read()
            conn.close()
            return
        except OSError:
            time.sleep(0.05)
    raise RuntimeError(f"the {label} server never answered /healthz:\n"
                       + log.read_text(errors="replace"))


def issue(port: int, target: Target) -> Answer:
    """One request, one connection.

    🔴 A FRESH CONNECTION PER TARGET, WHICH IS WHAT THE CORPUS DOES AND FOR THE SAME
    REASON: a non-200 closes the connection on both servers, so a keep-alive sweep would
    interleave two different connection lifecycles into the comparison. What that leaves
    UNSEEN is connection REUSE and everything downstream of it, and `README.md` says so.
    """
    conn = http.client.HTTPConnection("127.0.0.1", port, timeout=60)
    headers = dict(target.headers)
    if target.principal == "__none__":
        pass
    elif target.principal == "__bad__":
        # 🔴 DELIBERATELY SHORT, AND THAT IS `tests/leakscan.py` WORKING RATHER THAN A
        # COMPROMISE. The first spelling was a 45-character synthetic string after the word
        # `Bearer`, and the scanner refused it on sight — correctly: a real-looking
        # credential committed to a PUBLIC repository is a finding whether or not it ever
        # authenticated anything, and a scanner that made an exception for "but this one is
        # obviously fake" would be a scanner that makes exceptions. The token's LENGTH
        # changes no code path here: both servers compare the presented value against the
        # table and neither pre-screens on length.
        headers["Authorization"] = "Bearer nope"
    elif target.principal == "__malformed__":
        headers["Authorization"] = "Basic ZHVhbHJ1bjpub3BlCg=="
    else:
        headers["Authorization"] = f"Bearer {TOKENS[target.principal]}"
    if target.derive_if_match:
        # 🔴 ASKED OF THE SAME SERVER, NOT COPIED FROM THE OTHER. Each side derives its own
        # revision, so a disagreement about what the current revision IS becomes a different
        # answer to the real PUT rather than being hidden by one side's value. The probe is a
        # deliberately stale `If-Match`: its 412 carries the CURRENT revision, and it leaves
        # the file unchanged.
        conn.close()
        probe = http.client.HTTPConnection("127.0.0.1", port, timeout=60)
        probe.request("PUT", target.path, body=target.body,
                      headers={**headers, "If-Match": '"0000000000000000"'})
        presp = probe.getresponse()
        presp.read()
        etag = presp.getheader("ETag") or ""
        probe.close()
        if not etag:
            # No revision to derive: return the probe's own answer so the two servers are
            # still COMPARED, rather than silently skipping the target on one side.
            return Answer(presp.status, presp.reason,
                          tuple(sorted((k, v) for k, v in presp.getheaders())), b"")
        headers["If-Match"] = etag
        conn = http.client.HTTPConnection("127.0.0.1", port, timeout=60)
    conn.request(target.method, target.path, body=target.body, headers=headers)
    resp = conn.getresponse()
    body = resp.read()
    answer = Answer(resp.status, resp.reason,
                    tuple(sorted((k, v) for k, v in resp.getheaders())), body)
    conn.close()
    return answer


def restore_store(pristine: Path, store: Path) -> None:
    """Put the store back exactly as it was built — CONTENT AND MTIMES.

    🔴 `copy2`, SO MTIMES SURVIVE TO THE NANOSECOND. The index order is decided by
    comparing mtimes, so a restore that reset them to now would make the listing order and
    the featured pick depend on the order this loop ran in — the silent reordering the
    whole project exists to prevent, arriving from inside the instrument.
    """
    shutil.rmtree(store, ignore_errors=True)
    shutil.copytree(pristine, store, copy_function=shutil.copy2, symlinks=True)


# =============================================================================
# Comparison
# =============================================================================

_DIGITS = re.compile(r"\d")
_AUDIT_TS = re.compile(r"(?<=ts=)\S+")
_LISTEN = re.compile(r"(listening on \S+?):\d+")


def _status_header(answer: Answer) -> str:
    """`X-Store-Status`, or `""`. Both servers set it in one place per outcome."""
    for key, value in answer.headers:
        if key.lower() == "x-store-status":
            return value
    return ""


def drop_header(headers: tuple[tuple[str, str], ...], *names: str
                ) -> tuple[tuple[str, str], ...]:
    lowered = {n.lower() for n in names}
    return tuple((k, v) for k, v in headers if k.lower() not in lowered)


def uncompressed_tar(body: bytes) -> bytes:
    """The tar INSIDE the gzip, which is the thing byte-identity is scoped to here."""
    return gzip.decompress(body)


def tar_members(raw: bytes) -> list[tuple[str, int, int, bytes]]:
    """`(name, mode, mtime_ns, contents)` per member, in ARCHIVE ORDER.

    Used only to DIAGNOSE a tar difference. The verdict is the byte comparison above: an
    extracted-tree comparison keyed on member name is exactly what hid four PAX header
    divergences, so it must never be the thing that decides.
    """
    out = []
    with tarfile.open(fileobj=io.BytesIO(raw), mode="r:") as tar:
        for info in tar:
            fh = tar.extractfile(info)
            data = fh.read() if fh is not None else b""
            out.append((info.name, info.mode, int(round(float(info.mtime) * 1e9)), data))
    return out


def unified(a: bytes, b: bytes) -> str:
    left = a.decode("utf-8", "replace").splitlines()
    right = b.decode("utf-8", "replace").splitlines()
    lines = list(difflib.unified_diff(left, right, "oracle", "go", lineterm="", n=2))
    return "\n".join("       " + line for line in lines[:40])


@dataclass
class Result:
    problems: list[str]
    comparisons: int
    #: Which COMPARISONS failed — `status`, `headers`, `body` or `tar`.
    #:
    #: 🔴 THE SELF-TEST ATTRIBUTES A KILL TO AN ARM FROM THIS SET AND NOT FROM THE FAILING
    #: TARGET'S NAME. The first draft did the latter, and then unconditionally added
    #: `status` and `headers` to every attribution — so the two mutants written for those
    #: arms were "caught by the right arm" whatever had gone red. See `mutants.Mutation`.
    kinds: set[str] = field(default_factory=set)


def compare(target: Target, oracle: Answer, go: Answer,
            diffs: dict[str, Difference]) -> Result:
    problems: list[str] = []
    kinds: set[str] = set()
    comparisons = 0

    comparisons += 1
    if (oracle.status, oracle.reason) != (go.status, go.reason):
        problems.append(f"status {oracle.status} {oracle.reason!r} (oracle) vs "
                        f"{go.status} {go.reason!r} (go)")
        kinds.add("status")

    oh, gh = oracle.headers, go.headers
    dropped_date = any(k.lower() == "date" for k, _ in oh) or \
        any(k.lower() == "date" for k, _ in gh)
    oh, gh = drop_header(oh, "Date"), drop_header(gh, "Date")
    if dropped_date:
        diffs["date-header"].fire()

    # 🔴 THE `Content-Length` LICENCE IS SCOPED TO THE TWO ROUTES THAT EARN IT, AND IT IS
    # NARROWER THAN THE CONFORMANCE CORPUS'S. That corpus drops the header on every REPORT
    # too, because its goldens must survive being replayed on another host, where the report
    # body names a different machine and a different store path and is therefore a different
    # LENGTH. Both servers here read one store on one host, so a report's length is a fact
    # they must agree about and it is compared literally — including on every HEAD, which is
    # how `head-matches-get` is made a byte claim rather than a relation.
    tar_body = (target.compare == CMP_TAR and oracle.status == 200 and go.status == 200)
    if tar_body or target.compare == CMP_LIBDIAG:
        had = any(k.lower() == "content-length" for k, _ in oh)
        oh, gh = drop_header(oh, "Content-Length"), drop_header(gh, "Content-Length")
        if had and tar_body:
            diffs["snapshot-gzip-envelope"].fire()

    comparisons += 1
    if oh != gh:
        kinds.add("headers")
        left, right = dict(oh), dict(gh)
        for name in sorted(set(left) | set(right)):
            if left.get(name) != right.get(name):
                problems.append(f"header {name}: {left.get(name)!r} (oracle) vs "
                                f"{right.get(name)!r} (go)")

    ob, gb = oracle.body, go.body
    comparisons += 1
    if tar_body and target.method != "HEAD":
        diffs["snapshot-gzip-envelope"].fire()
        try:
            ob, gb = uncompressed_tar(ob), uncompressed_tar(gb)
        except OSError as exc:
            problems.append(f"a snapshot body was not a readable gzip stream: {exc}")
            kinds.add("tar")
            return Result(problems, comparisons, kinds)
        if ob != gb:
            kinds.add("tar")
            problems.append(f"the UNCOMPRESSED tar differs ({len(ob)}B oracle vs "
                            f"{len(gb)}B go)")
            problems.extend(_tar_diagnosis(ob, gb))
        return Result(problems, comparisons, kinds)

    if target.compare == CMP_LIBDIAG:
        head_o, _, tail_o = ob.partition(b"(")
        head_g, _, tail_g = gb.partition(b"(")
        if head_o != head_g:
            kinds.add("body")
            problems.append("the message BEFORE the library's diagnostic differs\n"
                            + unified(head_o, head_g))
        elif tail_o != tail_g:
            # The licence only applies when there IS a parenthesised tail on both sides and
            # the two genuinely differ. A missing tail is a real difference, not a library's.
            if tail_o.endswith(b")\n") and tail_g.endswith(b")\n"):
                diffs["json-decoder-diagnostic"].fire()
            else:
                kinds.add("body")
                problems.append("the decoder's diagnostic is not parenthesised on both "
                                f"sides: {tail_o!r} (oracle) vs {tail_g!r} (go)")
        return Result(problems, comparisons, kinds)

    if ob != gb:
        kinds.add("body")
        problems.append(f"body differs ({len(ob)}B oracle vs {len(gb)}B go)\n"
                        + unified(ob, gb))
    return Result(problems, comparisons, kinds)


def _tar_diagnosis(a: bytes, b: bytes) -> list[str]:
    try:
        left, right = tar_members(a), tar_members(b)
    except tarfile.TarError as exc:
        return [f"       (the archive could not be walked for a diagnosis: {exc})"]
    out = []
    if [m[0] for m in left] != [m[0] for m in right]:
        out.append("       member ORDER or SET differs:")
        out.append(f"         oracle: {[m[0] for m in left][:12]}")
        out.append(f"         go:     {[m[0] for m in right][:12]}")
        return out
    for (name, mode, mtime, data), (_n, gmode, gmtime, gdata) in zip(left, right):
        if mode != gmode:
            out.append(f"       {name}: mode {mode:o} vs {gmode:o}")
        if mtime != gmtime:
            out.append(f"       {name}: mtime_ns {mtime} vs {gmtime}")
        if data != gdata:
            out.append(f"       {name}: contents differ ({len(data)}B vs {len(gdata)}B)")
    if not out:
        out.append("       every member NAME, MODE, MTIME and CONTENT agrees, so the "
                   "difference is in the HEADER BYTES a POSIX reader normalises away — "
                   "which is exactly the class an extracted-tree comparison cannot see.")
    return out


# =============================================================================
# Enumerating the store through the servers themselves
# =============================================================================

def index_refs(port: int, scope: str, principal: str) -> list[str]:
    """Every ref the scope's INDEX lists, walking every page, in listed order.

    🔴 THE SET COMES FROM THE INDEX, NEVER FROM A DIRECTORY LISTING. A `*.md` file the
    loader rejects is a real file that is NOT indexed and NOT `?ref=`-addressable, so a
    bare listing would demand a comparison the API cannot answer and report a difference
    that does not exist. `server/verify-byte-identity.sh` measured that and says so.

    🔴 AND IT IS READ FROM BOTH SERVERS AND THE TWO SETS COMPARED — see `enumerate_refs`.
    Taking the list from one side only would make a ref that one server indexes and the
    other does not invisible: the sweep would simply never ask about it.
    """
    refs: list[str] = []
    page = 1
    while True:
        answer = issue(port, Target("enumerate", "", "GET",
                                    f"/api/v1/recall/{scope}?mode=list&page={page}",
                                    principal))
        if answer.status != 200:
            return refs
        total = None
        in_index = False
        for line in answer.body.decode("utf-8", "replace").splitlines():
            if line.startswith("INDEX ("):
                in_index = True
                match = re.search(r"\(page (\d+) of (\d+)\)", line)
                if match:
                    total = int(match.group(2))
                continue
            if in_index:
                if line.strip() == "":
                    in_index = False
                    continue
                if line.startswith("  ("):
                    continue
                refs.append(line.split()[0])
        if total is None or page >= total:
            return refs
        page += 1


def enumerate_refs(port_o: int, port_g: int, scope: str, principal: str
                   ) -> tuple[list[str], list[str]]:
    """`(refs, problems)` — the agreed ref list for a scope, and any set disagreement."""
    left = index_refs(port_o, scope, principal)
    right = index_refs(port_g, scope, principal)
    if left == right:
        return left, []
    problems = [f"the two servers' INDEX for `{scope}` lists DIFFERENT refs: "
                f"oracle-only={sorted(set(left) - set(right))} "
                f"go-only={sorted(set(right) - set(left))}"]
    if set(left) == set(right):
        problems.append("the SETS are equal and the ORDER is not, which is the silent "
                        "reordering that reads as a stale cache: "
                        f"oracle={left[:8]} go={right[:8]}")
    # The per-entry sweep proceeds over the intersection, so the arm is not skipped
    # wholesale because of one ref.
    return [r for r in left if r in set(right)], problems


# =============================================================================
# The pre-flight, and the store copy
# =============================================================================

@dataclass
class Preflight:
    ok: bool
    lines: list[str]


def preflight(port_o: int, port_g: int, scope: str) -> Preflight:
    """Did BOTH servers, independently, answer something substantive?

    🔴 THIS IS THE CONTROL THE P2 PARITY GATE WAS MEASURED VACUOUS WITHOUT, AND THE FAILURE
    IT GUARDS IS NOT HYPOTHETICAL. With the loopback in the trusted-proxy set — a value
    copied from the conformance runner, where it is correct — the server refuses every
    DIRECT request `401 status=no-client-ip`. Both servers would be refused identically,
    every target would compare equal, and the run would print a green over nothing. A zero
    here is indistinguishable from a harness wired to nothing, so this is the number that
    must move, measured on EACH SERVER SEPARATELY rather than as an equality.

    Two independent claims, because one does not imply the other:

      * a RENDERED DIGEST — a 200 whose body carries the report's own status line, so the
        renderer ran rather than a refusal being formatted;
      * a NON-EMPTY SNAPSHOT — a 200 whose `X-Store-Entries` is at least 1 AND whose gzip
        body extracts to a tar with at least one member, because a header can count what
        an archive does not carry.
    """
    lines: list[str] = []
    ok = True
    for label, port in (("oracle", port_o), ("go", port_g)):
        digest = issue(port, Target("preflight-digest", "", "GET",
                                    f"/api/v1/recall/{scope}", WIDE))
        rendered = (digest.status == 200
                    and b"\nsubsystem-recall: status=" in b"\n" + digest.body)
        snap = issue(port, Target("preflight-snapshot", "", "GET", "/api/v1/snapshot", WIDE))
        declared = -1
        members = -1
        for key, value in snap.headers:
            if key.lower() == "x-store-entries" and value.isdigit():
                declared = int(value)
        if snap.status == 200:
            try:
                members = len(tar_members(uncompressed_tar(snap.body)))
            except (OSError, tarfile.TarError):
                members = -1
        good = rendered and declared >= 1 and members >= 1
        ok = ok and good
        lines.append(f"PREFLIGHT {label} digest-status={digest.status} "
                     f"rendered-digest={rendered} digest-bytes={len(digest.body)} "
                     f"snapshot-status={snap.status} declared-entries={declared} "
                     f"tar-members={members}")
    return Preflight(ok, lines)


def copy_real_store(source: Path, dest: Path) -> list[str]:
    """Copy the operator's store, and PROVE the copy preserved what the reader reads.

    🔴 THE REAL STORE IS COPIED RATHER THAN SERVED IN PLACE, AND THAT IS A DELIBERATE
    TRADE. The two write routes MUTATE, and mutating somebody's curated notes to measure a
    comparison is not a trade worth making — so the gate serves a copy, restores it around
    every write, and the operator's store is only ever READ. ⚠ WHAT THAT COSTS: the copy is
    made by this process as this user, so a file the operator cannot read is a loud copy
    failure rather than a served `store-unreachable`, and anything the filesystem will not
    reproduce (an exotic mode, an ACL, a hardlink's identity) is not part of what is
    compared. Both servers read the SAME copy, so neither difference can make the
    comparison unfair — only narrower than the real disk.

    🔴 AND THE MTIMES ARE VERIFIED, NOT ASSUMED. The index order is decided by comparing
    mtimes to the nanosecond; a copy that rounded them would produce a store whose order is
    not the real store's, and the gate would measure a world that does not exist while
    reporting it as the operator's. Returns the problems found, so the caller can refuse to
    vouch rather than fail.
    """
    shutil.copytree(source, dest, copy_function=shutil.copy2, symlinks=True)
    problems: list[str] = []
    checked = 0
    for src in sorted(source.rglob("*")):
        if src.is_symlink() or not src.is_file():
            continue
        rel = src.relative_to(source)
        copy = dest / rel
        if not copy.is_file():
            problems.append(f"the copy is missing {rel}")
            continue
        checked += 1
        if src.stat().st_mtime_ns != copy.stat().st_mtime_ns:
            problems.append(f"the copy of {rel} has mtime_ns {copy.stat().st_mtime_ns}, "
                            f"not {src.stat().st_mtime_ns}")
    if checked == 0:
        problems.append("the copy verified ZERO files, so it vouches for nothing")
    return problems


# =============================================================================
# The run
# =============================================================================

@dataclass
class Run:
    rc: int
    targets: int
    comparisons: int
    failures: list[str]
    #: The COMPARISONS that failed anywhere in the run, and the TARGET ARMS they failed on.
    #: Both are what the self-test attributes a mutant kill to.
    kinds: set[str] = field(default_factory=set)
    target_arms: set[str] = field(default_factory=set)
    entries: int = 0


def run_once(work: Path, store: Path, pristine: Path, go_binary: Path, server_py: Path,
             *, break_both: bool, only: set[str] | None, quiet: bool,
             narrow_scopes: tuple[str, ...], all_scopes: list[str]) -> Run:
    work.mkdir(parents=True, exist_ok=True)
    tokens = token_file(work / "tokens", narrow_scopes, all_scopes)
    env = server_env(break_both)
    port_o, port_g = free_port(), free_port()
    log_o, log_g = work / "oracle.log", work / "go.log"
    diffs = differences()
    failures: list[str] = []
    failed_kinds: set[str] = set()
    failed_arms: set[str] = set()
    comparisons = 0
    targets_run = 0
    entry_count = 0

    def emit(line: str) -> None:
        if not quiet:
            print(line)

    proc_o = start_oracle(server_py, store, tokens, log_o, port_o, env)
    proc_g = start_go(go_binary, store, tokens, log_g, port_g, env)
    try:
        wait_for_health(port_o, proc_o, log_o, "oracle")
        wait_for_health(port_g, proc_g, log_g, "go")

        # 🔴 THE UTC DATE IS READ BEFORE AND AFTER, BECAUSE THE APPEND ROUTE STAMPS IT.
        # `render_bullet` writes `- <today>: ` from the server's own clock and the ETag
        # hashes the stamped content, so a run that straddles UTC midnight compares two
        # different days and reports a difference the code does not have. That is a reason
        # to REFUSE TO VOUCH, not a licence to normalise the date away: normalising it
        # would stop comparing a field that is part of the contract on every other run.
        date_before = _dt.datetime.now(_dt.timezone.utc).date()

        pre = preflight(port_o, port_g, all_scopes[0])
        for line in pre.lines:
            emit(line)
        if not pre.ok:
            print("REFUSING TO VOUCH: one of the two servers did not answer a rendered "
                  "digest AND a non-empty snapshot, so every target below would compare "
                  "two FAILURES to each other and report a difference count of zero. That "
                  "is the exact state the P2 parity gate was measured in before its "
                  "pre-flight existed.", file=sys.stderr)
            return Run(2, 0, 0, ["preflight"], {"preflight"}, set(), 0)

        # --- build the target list ------------------------------------------
        targets: list[Target] = []
        for scope in all_scopes:
            targets += scope_targets(scope)
            targets += narrowing_targets(scope, narrow_scopes)
        targets += store_targets(all_scopes, narrow_scopes)

        entry_problems: list[str] = []
        refs_by_scope: dict[str, list[str]] = {}
        for scope in all_scopes:
            refs, problems = enumerate_refs(port_o, port_g, scope, WIDE)
            refs_by_scope[scope] = refs
            entry_problems += problems
            entry_count += len(refs)
            targets += entry_targets(scope, refs)
            targets += search_by_ref_targets(scope, refs)
        for scope in narrow_scopes:
            refs, problems = enumerate_refs(port_o, port_g, scope, NARROW)
            entry_problems += problems
            targets += entry_targets(scope, refs, NARROW)
        # 🔴 ONE REF-SET COMPARISON PER SCOPE, COUNTED. A scope whose two index blocks list
        # different refs is a finding in its own right, and it is the arm that stops the
        # per-entry sweep from silently narrowing to whatever the oracle happened to list.
        comparisons += len(all_scopes) + len(narrow_scopes)
        for problem in entry_problems:
            failures.append("ref-set")
            failed_kinds.add("body")
            failed_arms.add("entry")
            emit(f"FAIL ref-set — {problem}")

        # The write phase last, and its ref taken from a scope that HAS one. 🔴 READ OUT OF
        # THE ALREADY-ENUMERATED SETS RATHER THAN BY ASKING AGAIN: an extra request to ONE
        # server would desynchronise the two audit streams by a line and make the audit arm
        # report a difference that is the harness's, not the code's.
        write_scope, write_ref = "", ""
        for scope in all_scopes:
            if refs_by_scope.get(scope):
                write_scope, write_ref = scope, refs_by_scope[scope][0]
                break
        if write_scope:
            # 🔴 `refused` MUST BE A SCOPE THAT EXISTS AND THAT THE *NARROWED* PRINCIPAL
            # CANNOT SEE, and `absent` one that exists for nobody. The earlier version passed
            # the first scope outside the NARROW allowlist as the "refused" scope for the WIDE
            # principal — which can see every scope — so that target answered `200 appended`
            # and compared nothing about a refusal. The refused/absent PAIR is the claim.
            refused = next((s for s in all_scopes if s not in narrow_scopes), all_scopes[0])
            targets += write_targets(write_scope, write_ref,
                                     "dualrun-scope-that-never-existed", refused,
                                     "dualrun-created-entry")
        else:
            failures.append("write-phase")
            failed_arms.add("write")
            emit("FAIL write-phase — no scope in this store indexes a single entry, so no "
                 "write target could be addressed and the two write routes were NOT "
                 "compared at all.")

        # 🔴 EVERY TARGET ID MUST BE UNIQUE, AND THIS REFUSES RATHER THAN WARNS. The
        # headline difference count is `len(set(failures))`, so two targets sharing an id
        # collapse into one and the number UNDER-REPORTS — measured: a mutant that made all
        # 316 targets differ was reported as 313, because the narrowed principal's entry
        # sweep addresses the same `<scope>/<ref>` pairs as the wide one. It also makes
        # `--only` ambiguous about which of the two it selected.
        seen: dict[str, int] = {}
        for target in targets:
            seen[target.id] = seen.get(target.id, 0) + 1
        clashes = sorted(name for name, count in seen.items() if count > 1)
        if clashes:
            print(f"REFUSING TO VOUCH: {len(clashes)} target id(s) are not unique, so the "
                  f"difference count would under-report and `--only` would be ambiguous: "
                  f"{clashes[:8]}", file=sys.stderr)
            return Run(2, 0, 0, ["duplicate-target-id"], set(), set(), 0)

        if only is not None:
            targets = [t for t in targets if t.id in only]
            if not targets:
                print("REFUSING: --only selected no target", file=sys.stderr)
                return Run(2, 0, 0, ["only"], set(), set(), 0)

        # --- issue and compare ----------------------------------------------
        # 🔴 A TARGET BOTH SERVERS ANSWER `no-route` TO IS A MATRIX DEFECT, NOT A PASS, AND
        # THIS FLOOR WAS ADDED BECAUSE A MUTATION SWEEP FOUND IT MISSING. Two servers that
        # 404 identically compare equal, so a target addressing a path that dispatches
        # nowhere — a typo, or a route addressed at the wrong ARITY — is a green that
        # measured a refusal while the route it names looks covered. The unit-side route
        # ledger cannot see it: `GET /api/v1/recall` addresses `GET recall` exactly as
        # `GET /api/v1/recall/<scope>` does, and only one of them reaches a handler.
        #
        # ⚠ The one target that is SUPPOSED to be a no-route is named, so the floor cannot
        # be satisfied by deleting it.
        expected_no_route = {"no-route"}
        answered_no_route: set[str] = set()
        # 🔴 THE SEARCH ARM'S OWN CONTENT FLOOR, MEASURED PER SERVER. `HIT_TERM` is a word
        # the GENERATED world spells and a real store probably does not, so without this a
        # mode-1 run could compare the sentence a MISS produces on every search target — a
        # correct comparison, and nothing at all about the hit path, the hunk ranking or the
        # context window. `search-by-ref` is what makes a hit reachable on any store; this is
        # what proves one happened rather than assuming it.
        saw_search_hit = {"oracle": False, "go": False}
        # 🔴 AND THE WRITE ROUTES' OWN FLOOR, FOR THE SAME REASON AND FOUND THE SAME WAY.
        # Inspecting the write phase's outcomes showed `create-new` answering **422
        # entry-shape** on both servers, because the PUT targets first sent an EMPTY body —
        # so the gate compared a refusal on every PUT and never saw a 201 or a 200 replace at
        # all. Two servers agreeing on a 422 is a real comparison of the refusal and nothing
        # about the write. `write_landed` is what makes that visible rather than assumed.
        write_landed = {"oracle": set(), "go": set()}
        for target in targets:
            if target.phase == "write":
                restore_store(pristine, store)
            oracle = issue(port_o, target)
            if target.phase == "write":
                restore_store(pristine, store)
            go = issue(port_g, target)
            # 🔴 KEYED ON `X-Store-Status: no-route`, NOT ON THE BARE 404, AND THE FIRST
            # DRAFT USED THE BARE STATUS AND WAS WRONG. An empty result cannot distinguish
            # two mechanisms: a write to an unresolvable ref and a write to a refused scope
            # BOTH answer 404 from a dispatched handler — deliberately, because refused must
            # be indistinguishable from absent — and the floor flagged them as targets
            # addressing nothing. The header is the upstream signal the two mechanisms
            # disagree about.
            if (oracle.status == 404 and go.status == 404
                    and _status_header(oracle) == "no-route"
                    and _status_header(go) == "no-route"):
                answered_no_route.add(target.id)
            if _status_header(oracle) == "search-hit":
                saw_search_hit["oracle"] = True
            if _status_header(go) == "search-hit":
                saw_search_hit["go"] = True
            if target.phase == "write":
                for label, answer in (("oracle", oracle), ("go", go)):
                    if 200 <= answer.status < 300:
                        write_landed[label].add(_status_header(answer))
            result = compare(target, oracle, go, diffs)
            comparisons += result.comparisons
            targets_run += 1
            if result.problems:
                failures.append(target.id)
                failed_kinds |= result.kinds
                failed_arms.add(target.arm)
                emit(f"FAIL {target.id}")
                emit(f"     why: {target.why}")
                for problem in result.problems:
                    emit("     " + problem)
            else:
                emit(f"PASS {target.id}")
        if any(t.phase == "write" for t in targets):
            restore_store(pristine, store)

        emit(f"CONTENT-FLOOR writes-landed oracle={sorted(write_landed['oracle'])} "
             f"go={sorted(write_landed['go'])}")
        # 🔴 THREE DISTINCT SUCCESSES ARE DEMANDED, NOT "at least one". `appended` alone
        # would leave BOTH halves of `PUT entry` — replace and create — compared at their
        # refusals only, which is the hole this floor was added for. The floor names the
        # statuses rather than counting them, so a success that changed its meaning cannot
        # satisfy it.
        want_writes = {"appended", "replaced", "created"}
        if only is None and not all(want_writes <= write_landed[side]
                                    for side in ("oracle", "go")):
            print(f"REFUSING TO VOUCH: the write routes did not LAND all of "
                  f"{sorted(want_writes)} on both servers — oracle="
                  f"{sorted(write_landed['oracle'])}, go={sorted(write_landed['go'])}. "
                  f"Every PUT comparison above was then about a refusal, which is a real "
                  f"answer and is not the write path: no 201, no new revision, no ETag over "
                  f"content this gate put there.", file=sys.stderr)
            return Run(2, targets_run, comparisons, failures + ["write-landed-floor"],
                       failed_kinds, failed_arms, entry_count)

        emit(f"CONTENT-FLOOR search-hit oracle={saw_search_hit['oracle']} "
             f"go={saw_search_hit['go']}")
        if only is None and not (saw_search_hit["oracle"] and saw_search_hit["go"]):
            print("REFUSING TO VOUCH: no search target produced a HIT on both servers, so "
                  "every search comparison above was about a MISS. That is a real answer "
                  "and it is not the search path — the hunk ranking, the rung, the context "
                  "window and the truncation notice were all unmeasured.", file=sys.stderr)
            return Run(2, targets_run, comparisons, failures + ["search-hit-floor"],
                       failed_kinds, failed_arms, entry_count)

        stray = answered_no_route - expected_no_route
        wanted_ids = {t.id for t in targets}
        if stray and only is None:
            print(f"REFUSING TO VOUCH: {len(stray)} target(s) were answered 404 by BOTH "
                  f"servers and are not the declared no-route probe, so they compared a "
                  f"refusal while the route they name looks covered: {sorted(stray)[:8]}",
                  file=sys.stderr)
            return Run(2, targets_run, comparisons, failures + ["stray-no-route"],
                       failed_kinds, failed_arms, entry_count)
        missing_probe = (expected_no_route & wanted_ids) - answered_no_route
        if missing_probe:
            print(f"REFUSING TO VOUCH: the no-route probe(s) {sorted(missing_probe)} were "
                  f"NOT answered 404 by both servers, so the floor above is measuring "
                  f"nothing — it cannot tell a stray 404 from a run in which no 404 is "
                  f"reachable at all.", file=sys.stderr)
            return Run(2, targets_run, comparisons, failures + ["no-route-probe"],
                       failed_kinds, failed_arms, entry_count)

        date_after = _dt.datetime.now(_dt.timezone.utc).date()
        if date_before != date_after:
            print(f"REFUSING TO VOUCH: the UTC date rolled from {date_before} to "
                  f"{date_after} during this run. The append route stamps the date into the "
                  f"bullet and the ETag hashes the stamped content, so the two servers were "
                  f"asked about two different days and any write difference above is the "
                  f"clock, not the code.", file=sys.stderr)
            return Run(2, targets_run, comparisons, failures + ["utc-date-rolled"],
                       failed_kinds, failed_arms, entry_count)
    finally:
        for proc in (proc_o, proc_g):
            proc.terminate()
            try:
                proc.wait(timeout=10)
            except subprocess.TimeoutExpired:
                proc.kill()
                proc.wait(timeout=10)

    # --- the two streams the wire cannot carry -----------------------------
    audit_problems, audit_compared = compare_audit(log_o, log_g, diffs)
    comparisons += audit_compared
    for problem in audit_problems:
        failures.append("audit-stream")
        failed_kinds.add("audit")
        emit(f"FAIL audit-stream — {problem}")
    if not audit_problems:
        emit(f"PASS audit-stream ({audit_compared} lines compared, ts spelling included)")

    proc_problems, proc_compared = compare_process_stream(log_o, log_g, diffs)
    comparisons += proc_compared
    for problem in proc_problems:
        failures.append("process-stream")
        failed_kinds.add("process")
        emit(f"FAIL process-stream — {problem}")
    if not proc_problems:
        emit(f"PASS process-stream ({proc_compared} lines compared, port normalised)")

    # --- the declared licences ---------------------------------------------
    dead = [d for d in diffs.values() if not d.fired]
    dead_is_fatal = only is None
    for row in dead:
        label = "FAIL" if dead_is_fatal else "NOTE"
        emit(f"{label} difference {row.name} — declared a licence to differ at "
             f"{row.where} and matched NOTHING. Either it is dead, or the difference it "
             f"named has been CLOSED and should be deleted, or a pattern stopped matching "
             f"and a real difference is now hidden behind it.")
    for row in diffs.values():
        if row.fired:
            emit(f"DIFFERENCE {row.name} — declared at {row.where}")

    emit(f"SUMMARY targets={targets_run} comparisons={comparisons} "
         f"differences={len(set(failures))} entries={entry_count}")
    if failures:
        emit("differing: " + ", ".join(sorted(set(failures))))
        emit("failing comparisons: " + ", ".join(sorted(failed_kinds)))
    rc = 1 if (failures or (dead and dead_is_fatal)) else 0
    return Run(rc, targets_run, comparisons, failures, failed_kinds, failed_arms,
               entry_count)


def compare_audit(log_o: Path, log_g: Path, diffs: dict[str, Difference]
                  ) -> tuple[list[str], int]:
    """The audit stream, line for line, in order.

    🔴 NO OTHER GATE IN THIS REPOSITORY READS IT. `tests/conformance/README.md` names the
    audit log under what the corpus cannot see — correctly: a suite that speaks HTTP cannot
    reach the stdout of a server it did not start. This harness STARTS both servers, so it
    can, and the log is a real contract: one line per `/api/*` request, with the
    fingerprint that makes an overlap rotation checkable and the identity that says which
    allowlist applied.

    🔴 THE `ts=` VALUE IS MASKED DIGIT BY DIGIT RATHER THAN DROPPED, so the SPELLING is
    still compared. That distinction is the whole reason this arm found anything: both
    servers emit RFC 3339 and they did not emit the same bytes.
    """
    def lines(path: Path) -> list[str]:
        return [ln for ln in path.read_text(errors="replace").splitlines()
                if ln.startswith("store-api audit ")]

    left, right = lines(log_o), lines(log_g)
    problems: list[str] = []
    if not left or not right:
        return ([f"one of the two servers wrote NO audit line at all "
                 f"(oracle={len(left)} go={len(right)}), so this arm vouches for nothing"],
                0)
    if len(left) != len(right):
        problems.append(f"the two servers wrote a different NUMBER of audit lines: "
                        f"oracle={len(left)} go={len(right)}. One line per API request is "
                        f"the contract, and the requests were the same.")

    def mask(line: str) -> str:
        return _AUDIT_TS.sub(lambda m: _DIGITS.sub("0", m.group(0)), line)

    compared = 0
    shown = 0
    for index, (a, b) in enumerate(zip(left, right)):
        ma, mb = mask(a), mask(b)
        compared += 1
        if ma != mb:
            if shown < 5:
                problems.append(f"line {index + 1} differs\n       oracle: {a}\n"
                                f"       go:     {b}")
                shown += 1
        if ma != a or mb != b:
            diffs["audit-timestamp-instant"].fire()
    if shown == 5 and compared:
        problems.append("… further audit-line differences not shown")
    return problems, compared


def compare_process_stream(log_o: Path, log_g: Path, diffs: dict[str, Difference]
                           ) -> tuple[list[str], int]:
    """Everything the two servers wrote that is NOT an audit record.

    The startup banner (store root, token fingerprints in file order, lockout settings,
    trusted-proxy set) and the legacy-mode warning are operator-facing contract, and the
    rotation procedure greps them. The listen PORT is the one field two servers cannot
    agree about, so it is the one field masked.
    """
    def lines(path: Path) -> list[str]:
        return [ln for ln in path.read_text(errors="replace").splitlines()
                if ln and not ln.startswith("store-api audit ")]

    left, right = lines(log_o), lines(log_g)
    problems: list[str] = []
    if not left or not right:
        return ([f"one of the two servers wrote NO startup line "
                 f"(oracle={len(left)} go={len(right)})"], 0)

    def mask(line: str) -> str:
        masked = _LISTEN.sub(r"\1:<PORT>", line)
        return masked

    compared = 0
    for index, (a, b) in enumerate(zip(left, right)):
        ma, mb = mask(a), mask(b)
        compared += 1
        if ma != a or mb != b:
            diffs["listen-port"].fire()
        if ma != mb:
            problems.append(f"line {index + 1} differs\n       oracle: {a}\n"
                            f"       go:     {b}")
    if len(left) != len(right):
        problems.append(f"a different number of non-audit lines: oracle={len(left)} "
                        f"go={len(right)}")
    return problems, compared


# =============================================================================
# main
# =============================================================================

def build_go(work: Path, given: str | None) -> Path:
    if given:
        return Path(given)
    binary = work / "cairn-server"
    subprocess.run(["go", "build", "-C", str(ROOT), "-o", str(binary), "./cmd/cairn-server"],
                   check=True)
    return binary


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(
        prog="tests/dualrun/harness.py",
        description="Run both cairn servers over ONE store and compare every route.")
    parser.add_argument("--store", default=None,
                        help="an EXISTING store to compare over. It is COPIED and the copy "
                             "is what is served, so the write routes cannot mutate it")
    parser.add_argument("--seed", type=int, default=genstore.DEFAULT_SEED,
                        help="the generated store's seed (mode 2)")
    parser.add_argument("--scale", type=int, default=1,
                        help="multiply the generated store's filler scope (mode 2)")
    parser.add_argument("--go-binary", default=None)
    parser.add_argument("--only", default=None, help="a comma-separated subset of target ids")
    parser.add_argument("--keep", action="store_true", help="keep the world for inspection")
    parser.add_argument("--break-both", action="store_true",
                        help="the NEGATIVE CONTROL ON THE PRE-FLIGHT: configure BOTH servers "
                             "to refuse every direct request, and refuse to vouch instead of "
                             "reporting a green over two identical failures")
    parser.add_argument("--self-test", action="store_true",
                        help="the NEGATIVE CONTROLS: boot the ORACLE from a mutated copy, "
                             "once per mutation, and refuse unless every mutant is caught "
                             "BY THE ARM IT WAS WRITTEN FOR")
    parser.add_argument("--positive-control", action="store_true",
                        help="the POSITIVE CONTROL: run the gate over two stores of "
                             "different SIZE and refuse unless the comparison count moved")
    args = parser.parse_args(argv)

    if args.self_test:
        return self_test(args)
    if args.positive_control:
        return positive_control(args)

    work = Path(tempfile.mkdtemp(prefix="cairn-dualrun-"))
    try:
        rc, run = _one(work, args, ROOT / "server" / "server.py", quiet=False)
        # 🔴 PRINTED ONLY WHEN SOMETHING WAS ACTUALLY COMPARED. `targets=0 comparisons=0`
        # under a refusal reads like a measurement, and the one thing this line must never
        # do is put a number beside a run that vouched for nothing.
        if run is not None and run.targets:
            print(f"MODE {'1 (--store, copied)' if args.store else '2 (generated)'} "
                  f"targets={run.targets} comparisons={run.comparisons}")
        return rc
    finally:
        if args.keep:
            print(f"world kept at {work}")
        else:
            shutil.rmtree(work, ignore_errors=True)


def _one(work: Path, args, server_py: Path, *, quiet: bool,
         scale: int | None = None) -> tuple[int, Run | None]:
    work.mkdir(parents=True, exist_ok=True)
    store = work / "store"
    if args.store:
        source = Path(args.store).expanduser().resolve()
        if not source.is_dir():
            print(f"REFUSING: --store {source} is not a directory", file=sys.stderr)
            return 2, None
        problems = copy_real_store(source, store)
        if problems:
            print("REFUSING TO VOUCH: the copy of the store did not preserve what the "
                  "reader reads, so this run would measure a world that does not exist:",
                  file=sys.stderr)
            for problem in problems[:10]:
                print("  " + problem, file=sys.stderr)
            return 2, None
        if not quiet:
            # 🔴 THE PATH IS NOT PRINTED, AND THE OUTPUT STILL NAMES THE OPERATOR'S SCOPES —
            # every `PASS <target>` line carries one, because a verdict nobody can attribute
            # is not a verdict. This repository is PUBLIC and was extracted from a private
            # one, so mode 1's output is the operator's and belongs nowhere near a commit,
            # a CI log or a pull request. Mode 2 is the mode whose output is publishable.
            print(f"STORE copied from a path this run does not print, "
                  f"files={sum(1 for p in store.rglob('*.md') if p.is_file())} "
                  f"scopes={len(genstore.scopes(store))}, mtimes verified to the nanosecond")
            print("NOTE mode 1's per-target lines NAME THE OPERATOR'S SCOPES. Do not paste "
                  "this output into this repository, a commit message, or CI.")
    else:
        genstore.build_store(store, args.seed, scale if scale is not None else args.scale)
        if not quiet:
            print(f"STORE generated seed={args.seed} "
                  f"scale={scale if scale is not None else args.scale} "
                  f"files={sum(1 for p in store.rglob('*.md') if p.is_file())} "
                  f"scopes={len(genstore.scopes(store))}")

    all_scopes = genstore.scopes(store)
    if not all_scopes:
        print(f"REFUSING TO VOUCH: the store holds NO scope directory, so nothing would "
              f"be compared and a difference count of zero would mean nothing.",
              file=sys.stderr)
        return 2, None
    # 🔴 THE NARROWED ALLOWLIST IS DERIVED FROM THE STORE, SO MODE 1 NARROWS TOO. A fixed
    # list would name scopes a real store does not hold, `narrow-reader` would then see
    # NOTHING, and every narrowing target would compare two identical refusals — which is
    # the vacuous-green shape one level down, inside the arm added to avoid it. It must also
    # be a PROPER subset: if it named every scope there would be no narrowing to compare.
    wanted = tuple(NARROW_SCOPES) if not args.store else tuple(all_scopes[:2])
    narrow_scopes = tuple(s for s in wanted if s in all_scopes) or (all_scopes[0],)
    if len(narrow_scopes) >= len(all_scopes) and len(all_scopes) > 1:
        narrow_scopes = narrow_scopes[:len(all_scopes) - 1]
    if len(all_scopes) == 1 and not quiet:
        print("NOTE the store holds ONE scope, so `narrow-reader`'s allowlist cannot be a "
              "proper subset of it and the narrowing arm compares nothing about a REFUSED "
              "scope. The absent-scope targets still do.")

    pristine = work / "store-pristine"
    shutil.rmtree(pristine, ignore_errors=True)
    shutil.copytree(store, pristine, copy_function=shutil.copy2, symlinks=True)
    go_binary = build_go(work, args.go_binary)
    only = None if args.only is None else set(args.only.split(","))
    run = run_once(work, store, pristine, go_binary, server_py,
                   break_both=args.break_both, only=only, quiet=quiet,
                   narrow_scopes=narrow_scopes, all_scopes=all_scopes)
    return run.rc, run


def self_test(args) -> int:
    """Boot the ORACLE from a mutated copy, once per mutation, and require each to be caught.

    🔴 CAUGHT *BY THE ARM IT WAS WRITTEN FOR*, WHICH IS A STRICTLY STRONGER CLAIM THAN
    "something went red". A mutant killed by a different arm proves the gate can fail and
    proves nothing about the arm it was built to exercise — and this sweep has two
    mutations whose whole purpose is an arm no other gate has: the audit stream, and the
    per-entry `?ref=` sweep.

    🔴 AND THE POSITIVE CONTROL RUNS FIRST. Without it, "the mutant was caught" cannot be
    told apart from "the copied tree never booted", which is the same green-for-the-wrong-
    reason the mutation is meant to expose.
    """
    if args.store:
        print("REFUSING: --self-test needs the SHAPES the mutants were written for — an "
              "ambiguous bare ref, entries inside one whole second, a non-ASCII member "
              "name — and a real store is not guaranteed to hold any of them. A mutant "
              "that is unreachable in the world it is run against is scored SURVIVED for "
              "a reason that has nothing to do with the gate. Drop --store.",
              file=sys.stderr)
        return 2
    work = Path(tempfile.mkdtemp(prefix="cairn-dualrun-selftest-"))
    try:
        # 🔴 BUILT ONCE. Rebuilding per mutant would make the Go side a different binary
        # each round for no reason, and a mutation sweep whose CONTROL moves between rounds
        # is not a sweep.
        args.go_binary = str(build_go(work, args.go_binary))
        clean = M.unmutated_server(work / "control")
        rc, run = _one(work / "control-run", args, clean, quiet=True)
        if rc != 0 or run is None:
            print(f"REFUSING TO VOUCH: the POSITIVE control — the same copy mechanics with "
                  f"NO edit — exited {rc} with "
                  f"{len(set(run.failures)) if run else '?'} difference(s). Every mutant "
                  f"below would be 'caught' by whatever is already broken.", file=sys.stderr)
            if run:
                print("  differing: " + ", ".join(sorted(set(run.failures))), file=sys.stderr)
            return 2
        print(f"SELF-TEST control: unmutated copy PASSES "
              f"(targets={run.targets} comparisons={run.comparisons})")

        caught = 0
        refusals: list[str] = []
        for mutation in M.MUTATIONS:
            server_py = M.mutated_server(work / f"mut-{mutation.name}", mutation)
            rc, run = _one(work / f"run-{mutation.name}", args, server_py, quiet=True)
            kinds = run.kinds if run else set()
            arms = run.target_arms if run else set()
            failing = sorted(set(run.failures)) if run else []
            if not failing:
                refusals.append(f"{mutation.name} SURVIVED (rc={rc}, nothing differed), so "
                                f"the `{mutation.kind}` comparison is wired to nothing")
                print(f"SELF-TEST SURVIVED {mutation.name} — rc={rc}, nothing differed")
                continue
            if mutation.kind not in kinds:
                # Assertion 1: the arm this mutant was WRITTEN for went red. A mutant killed
                # only by some other comparison proves the gate can fail and proves nothing
                # about the arm it was built to exercise.
                refusals.append(f"{mutation.name} failed comparisons {sorted(kinds)}, which "
                                f"does not include `{mutation.kind}` — the arm it was "
                                f"written for is wired to nothing")
                print(f"SELF-TEST WRONG COMPARISON {mutation.name} — {sorted(kinds)}, "
                      f"which does not include `{mutation.kind}`")
                continue
            if kinds != set(mutation.expect):
                # Assertion 2: nothing ELSE moved, so the kill is attributable. The expected
                # set is declared per mutation rather than assumed to be `{kind}`, because a
                # mutation that changes the ANSWER moves several comparisons at once — see
                # the note on `resolver-tier-keyed-on-ref`.
                refusals.append(f"{mutation.name} failed comparisons {sorted(kinds)}, not "
                                f"the declared {sorted(mutation.expect)}")
                print(f"SELF-TEST NOT AS DECLARED {mutation.name} — failing comparisons "
                      f"{sorted(kinds)}, declared {sorted(mutation.expect)}")
                continue
            if mutation.only_target_arm is not None and arms != {mutation.only_target_arm}:
                refusals.append(f"{mutation.name} failed on target arms {sorted(arms)}, not "
                                f"only {{{mutation.only_target_arm!r}}} — so that arm is "
                                f"not the thing that sees it")
                print(f"SELF-TEST WRONG ARM {mutation.name} — target arms {sorted(arms)}, "
                      f"expected only ['{mutation.only_target_arm}']")
                continue
            caught += 1
            print(f"SELF-TEST caught {mutation.name} by the `{mutation.kind}` comparison "
                  f"(all of {sorted(kinds)})"
                  + (f" on the `{mutation.only_target_arm}` arm ALONE"
                     if mutation.only_target_arm else "")
                  + f" — {len(failing)} differing target(s), e.g. {failing[:3]}")
        print(f"SELF-TEST mutants={len(M.MUTATIONS)} caught={caught}")
        if refusals:
            print("REFUSING TO VOUCH: " + "; ".join(refusals) +
                  ". Every PASS those comparisons produce is a fact about the harness.",
                  file=sys.stderr)
            return 2
        print("SELF-TEST OK: every mutant was caught by the comparison it was written for, "
              "by exactly the comparisons it declares, and — where one is named — on that "
              "target arm alone")
        return 0
    finally:
        shutil.rmtree(work, ignore_errors=True)


def positive_control(args) -> int:
    """The comparison count must MOVE when the store grows.

    🔴 A REASSURING NUMBER IS INDISTINGUISHABLE FROM A CONSTANT. `comparisons=1200` proves
    nothing on its own: a harness that enumerated a hardcoded list would print the same
    number over an empty store. So the gate is run twice over stores of different SIZE and
    the counts are required to move in the right direction — which is a claim about the
    ENUMERATION, and the only one that cannot be satisfied by a wired-to-nothing sweep.

    ⚠ IT IS A MODE-2 CONTROL. Mode 1 is pointed at one real store and has no second size
    to compare against, so passing `--store` here is refused rather than silently measured
    over a generated one.
    """
    if args.store:
        print("REFUSING: --positive-control needs two stores of different size, so it runs "
              "over the GENERATED one. Drop --store.", file=sys.stderr)
        return 2
    work = Path(tempfile.mkdtemp(prefix="cairn-dualrun-positive-"))
    try:
        # ONE binary across both scales: the thing under test must not move between the two
        # runs whose counts are being compared.
        args.go_binary = str(build_go(work, args.go_binary))
        counts = {}
        for scale in (1, 2):
            rc, run = _one(work / f"scale-{scale}", args, ROOT / "server" / "server.py",
                           quiet=True, scale=scale)
            if rc != 0 or run is None:
                print(f"REFUSING TO VOUCH: the run at scale {scale} exited {rc}, so the "
                      f"counts below are not both from a clean gate.", file=sys.stderr)
                return 2
            counts[scale] = run
            print(f"POSITIVE-CONTROL scale={scale} targets={run.targets} "
                  f"comparisons={run.comparisons}")
        smaller, larger = counts[1], counts[2]
        if larger.comparisons <= smaller.comparisons or larger.targets <= smaller.targets:
            print(f"REFUSING TO VOUCH: doubling the store moved targets "
                  f"{smaller.targets} -> {larger.targets} and comparisons "
                  f"{smaller.comparisons} -> {larger.comparisons}. A count that does not "
                  f"grow with the store is not counting the store.", file=sys.stderr)
            return 2
        print(f"POSITIVE-CONTROL OK: targets {smaller.targets} -> {larger.targets}, "
              f"comparisons {smaller.comparisons} -> {larger.comparisons}")
        return 0
    finally:
        shutil.rmtree(work, ignore_errors=True)


if __name__ == "__main__":
    raise SystemExit(main())
