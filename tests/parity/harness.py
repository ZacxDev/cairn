#!/usr/bin/env python3
"""The P2 gate: run BOTH clients over ONE cache root and diff what they produced.

🔴 THIS IS THE DELIVERABLE'S EVIDENCE, THE SAME WAY THE CORPUS WAS P1's. Rewriting the server
alone left two renderers in two languages that must agree byte-for-byte forever, with drift
arriving as "a different order that reads as a stale cache" — no error, no missing entry. The
corpus keeps the two SERVERS honest; nothing kept the Python CLIENT and the Go one honest, and
"we ported it carefully" is not a measurement. This harness is the measurement: same store, same
pod, same cache root, same argv, and a byte comparison of stdout, stderr and the exit code.

🔴 THE COMPARISON IS AGAINST THE ORACLE, NOT AGAINST A GOLDEN FILE. Both clients run in the same
process tree against the same live pod. A recorded golden would let the two agree with a snapshot
of an older Python client and call it parity; this cannot.

🔴 WHAT IT STRUCTURALLY CANNOT SEE, stated rather than left to be found:

  * **argparse.** The Python client's usage text, its `--help` layout, its prefix abbreviation
    (`--sc` for `--scope`) and its refusal wording are `argparse`'s. Reproducing them in Go
    would be a second implementation of a library nobody reads twice, so the Go client has its
    own usage text and the harness compares only the EXIT CODE on those rows — declared per case
    with `compare="exit"`, never applied silently.
  * **anything needing a real network failure.** An unreachable pod is exercised by pointing at
    a closed port, which is a connect refusal; a DNS failure, a mid-transfer reset and a
    half-open socket are not built here, and the two clients' messages for those come from
    `urllib` and `net/http` respectively and would not match.
  * **concurrency.** Both clients hold the same `flock` around the cache swap, which is why they
    can share a root at all; nothing here runs them at the same instant.
  * **a store the client may not fully see.** The token is unrestricted on purpose — the
    narrowing is the SERVER's and the conformance corpus owns it.

🔴 AND THE NORMALIZATIONS ARE A LICENCE TO DIFFER, SO EACH ONE MUST FIRE. A declared
normalization that matched nothing is reported as `FAIL normalization <name>`, exactly as the
HTTP corpus does: a licence nobody used is either dead or hiding a real difference behind a
pattern that no longer matches.
"""
from __future__ import annotations

import argparse
import os
import re
import shutil
import socket
import subprocess
import sys
import tempfile
import time
from dataclasses import dataclass, field
from pathlib import Path

HERE = Path(__file__).resolve().parent
ROOT = HERE.parents[1]
sys.path.insert(0, str(HERE))
# `testlib` lives beside the suites, one level up from this harness's own dir.
# 🔴 INSERTED AT 1, NOT 0, SO THIS DIRECTORY KEEPS PRECEDENCE. At index 0 the
# parent would shadow `tests/parity/` for every later import — and this repo
# already has an incident in that exact class, `tests/parity/harness.py` and
# `tests/dualrun/harness.py` colliding on the module name `harness`. `world`
# and `hostile` are the names at risk here; neither exists in `tests/` today,
# which is why the ordering is a latent hazard rather than a live one.
# ⚠ Redundant under pytest, which already puts `tests/` on the path; it is the
# standalone-script path that needs it.
sys.path.insert(1, str(ROOT / "tests"))

import hostile  # noqa: E402
import world as W  # noqa: E402
from testlib import env_pin  # noqa: E402

#: The stamp filename, excluded from the mtime comparison below: it is written at sync time by
#: each client and its `synced=` line is wall-clock by design, so comparing it would be comparing
#: two clocks.
W_SYNC_STAMP = ".sync-stamp"

#: The host identity both clients must print. 🔴 SET EXPLICITLY, FOR TWO REASONS: the rendered
#: report names the machine it read, so an unset label would make the output carry this host's
#: real name into a PUBLIC repository's test log; and `CAIRN_HOST` is the one input that makes
#: the two implementations' `this_host()` comparable without either of them being patched.
PARITY_HOST = "parity-harness"

BOOT_TIMEOUT_S = 30.0

#: The negative control, by case id. 🔴 A REASSURING GREEN FROM THIS HARNESS IS INDISTINGUISHABLE
#: FROM A HARNESS WIRED TO NOTHING, and that is not hypothetical here: the first full run reported
#: 72/72 while every request was refused `401 status=no-client-ip` and no cache was ever written.
#: `--self-test` appends these arguments to the GO side of three rows and REFUSES unless the differ
#: reports all three — one row that must fail on stdout, one on stderr, and one on the exit code,
#: because those are three separate comparisons and a harness can lose any one of them alone.
#: 🔴 A SABOTAGE IS `("append", …)` OR `("replace", …)`, AND THE SECOND FORM EXISTS BECAUSE THE
#: `--help` ROWS CANNOT BE SABOTAGED BY APPENDING. Both clients handle `--help` before anything
#: else, so no extra argument changes either answer — which means an append-only mechanism had NO
#: control over the third comparison mode at all, and its two assertions (the exit code AND a
#: non-empty stdout) would have been vouched for by nothing.
SABOTAGE: dict[str, tuple[str, list[str]]] = {
    # stdout: a different page of the same index is a well-formed answer to a different question.
    "recall-list": ("append", ["--page", "9"]),
    # stderr: `--no-sync` changes the BANNER, which `validate` writes to stderr, while stdout's
    # per-scope counts stay identical — so this row can only fail on the stderr comparison.
    "validate-one-scope": ("append", ["--no-sync"]),
    # exit: this row is `compare="exit"`, so a stdout or stderr difference is INVISIBLE to it by
    # construction — which is exactly why the exit comparator needs its own control. A second
    # `--text` over the bullet cap makes the Go side refuse LOCALLY at exit 2 where the oracle
    # still reports the unreachable write at 7. Both command lines are well-formed; they mean
    # different things.
    "append-unreachable": ("append", ["--text", "x" * 2001]),
    # exit+stdout: REPLACED, for the reason above. An unknown subcommand makes the Go side exit 2
    # with an empty stdout where the oracle prints its help at 0 — so this one mutant exercises
    # BOTH of that mode's assertions at once.
    "help-top-level": ("replace", ["telepathy"]),
}


# =============================================================================
# Normalizations — each one a DECLARED licence to differ, each one required to fire.
# =============================================================================

@dataclass
class Normalization:
    name: str
    why: str
    pattern: re.Pattern[str]
    replacement: str
    fired: bool = False

    def apply(self, text: str) -> str:
        out, n = self.pattern.subn(self.replacement, text)
        if n:
            self.fired = True
        return out


def normalizations() -> list[Normalization]:
    return [
        Normalization(
            name="cache-age-seconds",
            why=(
                "The cached banner prints `cache <N>s old`, derived from wall-clock minus the "
                "stamp's `synced=`. The two clients run a fraction of a second apart, so the "
                "number can differ by one across a second boundary. The AGE VOCABULARY itself "
                "is not normalized — `Ns`/`Nm`/`Nh`/`Nd` and the `age UNKNOWN` sentinel all "
                "compare literally — only the integer inside the seconds form."
            ),
            pattern=re.compile(r"cache \d+s old"),
            replacement="cache <Ns> old",
        ),
        Normalization(
            name="temp-root",
            why=(
                "Every run builds its world under a fresh mktemp directory and the rendered "
                "report names the store path it read. The path is IDENTICAL for both clients "
                "within one run, so this does not hide a difference between them — it makes a "
                "run's output comparable to a previous run's when a human diffs two logs."
            ),
            pattern=re.compile(re.escape(str(Path(tempfile.gettempdir()))) + r"/cairn-parity-\w+"),
            replacement="<WORLD>",
        ),
    ]


# =============================================================================
# The cases
# =============================================================================

#: `compare` values. 🔴 `exit` IS A NARROWER CLAIM AND IT IS NAMED PER ROW, never defaulted: a
#: row that compares only the code is a row whose TEXT is allowed to differ, and the reason has
#: to be written down beside it or the gate quietly shrinks.
COMPARE_ALL = "all"
COMPARE_EXIT = "exit"
#: 🔴 A THIRD MODE, AND IT EXISTS BECAUSE `exit` ALONE WAS NOT ENOUGH FOR `--help`. Help text is
#: argparse's on the oracle and this port's own, so the bytes cannot be compared — but a client that
#: printed NOTHING and exited 0 would pass an exit-only row while telling the reader nothing, which
#: is the reassuring zero wearing a different hat. This mode asserts the code AND that both sides
#: put something on stdout.
COMPARE_EXIT_AND_STDOUT_NONEMPTY = "exit+stdout"


@dataclass
class Case:
    id: str
    why: str
    argv: list[str]
    compare: str = COMPARE_ALL
    #: Extra environment for BOTH clients. Used to break the config (an unreachable pod, a
    #: missing token) without touching argv.
    env: dict[str, str] = field(default_factory=dict)
    #: Run in the git repo rather than in the world root. The focus-window and
    #: scope-derivation cases need a real repo; everything else must NOT be in one, or a
    #: `--repo .` default would derive a scope from the harness's own checkout.
    in_repo: bool = False
    #: Reset the cache root before this case, so a `--no-sync` row can be about an ABSENT cache.
    wipe_cache: bool = False
    #: Run WITHOUT the harness's shared `--cache`, so each client resolves the default root PER
    #: INSTANCE.
    #:
    #: 🔴 IT EXISTS BECAUSE `--cache` IS REFUSED ON A MULTI-INSTANCE FAN-OUT, AND THAT REFUSAL
    #: IS WHAT KEPT THIS GATE BLIND. `routes --check` walking N instances with one explicit
    #: cache directory would make them overwrite each other, so both clients exit 2 before the
    #: walk begins — which compares equal and measures nothing about the walk. A row that wants
    #: to observe WHICH URL each instance was fetched from has to let the roots be derived.
    no_cache_flag: bool = False
    #: A command run with the ORACLE client, after the store is restored and before the measured
    #: pair, so a case can be about a store some earlier write already changed.
    #:
    #: 🔴 IT IS EXPLICIT BECAUSE THE ALTERNATIVE WAS AN ORDERING DEPENDENCY BETWEEN ROWS, AND
    #: THAT ORDERING PRODUCED A FALSE FAILURE. `create-exists` used to rely on `create-ok` having
    #: run first; since the two clients run in sequence, the ORACLE created the entry and the GO
    #: client then legitimately answered `already-exists` — a divergence the harness reported and
    #: the code did not have.
    setup: list[str] | None = None
    #: Sync with the ORACLE client against the HEALTHY pod before the measured run, whatever this
    #: row's own `env` points at.
    #:
    #: 🔴 IT EXISTS FOR THE HOSTILE-ARCHIVE ROWS, AND WITHOUT IT THEIR CLAIM WAS FALSE IN A SUBSET
    #: RUN. "a hostile archive must not be absorbed into `served from cache`" is a claim about a
    #: run that HAS a cache; those rows inherited one from an earlier row's sync, so
    #: `--only corrupt-…` measured the refusal with no cache at all — a weaker property wearing
    #: the same PASS.
    presync: bool = False


def cases(closed_port: int, hostile_port: int = 1) -> list[Case]:
    unreachable = {"SUBSYSTEM_STORE_URL": f"http://127.0.0.1:{closed_port}"}
    def hostile_env(kind: str) -> dict[str, str]:
        return {"SUBSYSTEM_STORE_URL": f"http://127.0.0.1:{hostile_port}/{kind}"}
    no_token = {"SUBSYSTEM_STORE_TOKEN": ""}
    return [
        # --- sync -------------------------------------------------------------
        Case("sync-live", "the LIVE banner: host, count and the pod's own freshness stamp",
             ["sync"], wipe_cache=True),
        Case("sync-again", "a second sync over an existing cache, which exercises the "
             "retire-and-rename swap rather than the create path", ["sync"]),
        Case("sync-scope-is-accepted-and-IGNORED",
             "🔴 `--scope` must NOT narrow the shared cache. Both clients accept the flag and "
             "pass `scope=None`; a client that threaded it through would fetch a one-scope "
             "archive and the entry COUNT in the banner would drop",
             ["sync", "--scope", "alpha-notes"]),
        Case("sync-unreachable", "exit 4, not 0 and not 3: not refreshed, but a cache survives",
             ["sync"], env=unreachable, compare=COMPARE_EXIT),
        Case("sync-unreachable-no-cache",
             "exit 3: nothing was read at all, which must not collapse into the 4 above. Text "
             "excluded because the banner quotes the transport's own connect-refusal wording",
             ["sync"], env=unreachable, compare=COMPARE_EXIT, wipe_cache=True),

        # --- ls-entries -------------------------------------------------------
        Case("ls-entries", "one `<scope>/<entry>.md` per line, and the ORDER is the claim",
             ["ls-entries"], wipe_cache=True),
        Case("ls-entries-no-sync", "the cached banner and the same listing, off the network",
             ["ls-entries", "--no-sync"]),
        Case("ls-entries-no-sync-no-cache", "`--no-sync` with no cache is exit 3, never an "
             "empty listing at 0", ["ls-entries", "--no-sync"], compare=COMPARE_EXIT,
             wipe_cache=True),

        # --- recall -----------------------------------------------------------
        Case("recall-digest", "the whole digest: every badge, the featured pick, the reject "
             "block and the omission notice", ["recall", "--scope", "alpha-notes"],
             wipe_cache=True),
        Case("recall-list", "the index alone", ["recall", "--scope", "alpha-notes", "--list"]),
        Case("recall-limit", "`--limit` selects full-body mode and caps it",
             ["recall", "--scope", "alpha-notes", "--limit", "2"]),
        Case("recall-limit-untruncated", "the same mode with room to spare, so the notice's "
             "ABSENCE is measured too", ["recall", "--scope", "alpha-notes", "--limit", "99"]),
        Case("recall-page", "the paginated index header on a scope with one page",
             ["recall", "--scope", "alpha-notes", "--list", "--page", "1"]),
        Case("recall-page-past-end", "no arithmetic on a page that does not exist",
             ["recall", "--scope", "alpha-notes", "--list", "--page", "9"]),
        Case("recall-ref-hit", "one entry in full, via the filename tier",
             ["recall", "--scope", "alpha-notes", "--ref", "widget-cfg"]),
        Case("recall-ref-alias", "the ALIAS tier of the resolver",
             ["recall", "--scope", "alpha-notes", "--ref", "ledger-holder"]),
        Case("recall-ref-absent", "an absence QUALIFIED by the scope's own reject",
             ["recall", "--scope", "alpha-notes", "--ref", "ghost-ref"]),
        Case("recall-mode-full", "`--mode full` explicitly, which stays authoritative over the "
             "flag-derived mode", ["recall", "--scope", "alpha-notes", "--mode", "full"]),
        Case("recall-scope-empty", "a directory that exists and holds nothing — and the LIVE "
             "state promotes to `scope-empty` while KEEPING the pod's freshness detail",
             ["recall", "--scope", "hollow-set"]),
        Case("recall-scope-absent", "a scope that never existed, with the known-scope list",
             ["recall", "--scope", "ghost-void"]),
        Case("recall-scope-unreadable", "the scope holds files and NOT ONE indexed: exit 3 AND "
             "the warning sentence on stderr, which the Go side RETURNS rather than printing "
             "from inside the library", ["recall", "--scope", "rubble-heap"]),
        Case("recall-no-sync", "the cached banner above an otherwise identical digest",
             ["recall", "--scope", "alpha-notes", "--no-sync"]),
        Case("recall-unreachable-serves-cache",
             "🔴 exit 0 WITH the digest and an `⚠ SERVED FROM CACHE` banner. A read degrades; "
             "this is the row that proves it still degrades the same way on both",
             ["recall", "--scope", "alpha-notes"], env=unreachable, compare=COMPARE_EXIT),

        # --- recall, in a real repo: the FOCUS WINDOW ---------------------------
        Case("recall-focus-resolved",
             "🔴 THE ROW P1b's REFUSAL MADE IMPOSSIBLE. No `--scope`, default mode, in a repo "
             "whose handoff doc quotes `apps/widget-cfg/values.yaml` — so the basis must read "
             "`resolved via claudedocs/handoff-parity.md`, and a client that fell back would "
             "feature the NEWEST entry instead and say so",
             ["recall"], in_repo=True),
        Case("recall-focus-suppressed-by-scope",
             "the same repo with `--scope` given: the window is meaningless once the caller "
             "names a scope, so the basis must be the FALLBACK's",
             ["recall", "--scope", "alpha-notes"], in_repo=True),
        Case("recall-focus-suppressed-by-mode",
             "the same repo with a non-default mode: `list` prints no featured entry at all",
             ["recall", "--mode", "list"], in_repo=True),
        Case("recall-derived-scope-no-repo",
             "outside a git repo, deriving a scope FAILS and that is exit 2 — not a traceback, "
             "and not a report about some other scope", ["recall"], compare=COMPARE_EXIT),

        # --- recall: the refusals ----------------------------------------------
        Case("recall-ref-and-list", "two selectors that select different things",
             ["recall", "--scope", "alpha-notes", "--ref", "widget-cfg", "--list"]),
        Case("recall-list-and-limit", "a cap on bodies where no body is printed",
             ["recall", "--scope", "alpha-notes", "--list", "--limit", "2"]),
        Case("recall-page-and-ref", "paging an index that is not printed",
             ["recall", "--scope", "alpha-notes", "--ref", "widget-cfg", "--page", "2"]),
        Case("recall-limit-zero", "the reader's own option ladder, reached through the CLI: "
             "exit 2 with the message alone, never a traceback",
             ["recall", "--scope", "alpha-notes", "--limit", "0"]),
        Case("recall-page-zero", "the ladder's SECOND rung, which a valid limit still reaches",
             ["recall", "--scope", "alpha-notes", "--page", "0"]),
        Case("recall-bad-mode", "the third rung, whose message quotes a Python tuple",
             ["recall", "--scope", "alpha-notes", "--mode", "telepathy"]),

        # --- search -----------------------------------------------------------
        Case("search-hit", "a hit, its rung and its context window",
             ["search", "--scope", "alpha-notes", "synthetic"]),
        Case("search-miss", "a term nothing scores on, which is its own sentence",
             ["search", "--scope", "alpha-notes", "zzzznothing"]),
        Case("search-all-scopes", "`--all-scopes` changes the LABEL and the searched set",
             ["search", "--scope", "alpha-notes", "synthetic", "--all-scopes"]),
        Case("search-unreadable", "the search-side of `scope-unreadable`, which has an extra "
             "sentence the recall side does not", ["search", "--scope", "rubble-heap", "x"]),
        Case("search-scope-absent", "a scope that never existed",
             ["search", "--scope", "ghost-void", "x"]),
        Case("search-no-sync", "off the network, over the cache as it stands",
             ["search", "--scope", "alpha-notes", "synthetic", "--no-sync"]),

        # --- validate ---------------------------------------------------------
        Case("validate-all-scopes", "no `--scope` validates EVERY scope the cache holds, and "
             "REPORTS WHAT WAS CHECKED — a clean scope printing nothing is byte-identical to a "
             "validate that parsed no files", ["validate"], wipe_cache=True),
        Case("validate-one-scope", "a scope with a malformed entry beside readable ones: the "
             "per-entry rows on stderr and the count on stdout",
             ["validate", "--scope", "alpha-notes"]),
        Case("validate-clean-scope", "a scope with nothing wrong, where the count is the only "
             "evidence anything happened", ["validate", "--scope", "beta-notes"]),
        Case("validate-absent-scope",
             "🔴 THE SILENT ZERO THE NO-SCOPE PATH WAS FIXED FOR AND THE EXPLICIT PATH THEN "
             "COMMITTED: a scope the cache does not hold must be exit 2 naming what IS held, "
             "never a clean 0", ["validate", "--scope", "ghost-void"]),
        Case("validate-no-sync", "the same over the cache, off the network",
             ["validate", "--no-sync"]),

        # --- doctor -----------------------------------------------------------
        Case("doctor-live", "seven checks, four states, the count line and the exit legend",
             ["doctor"], wipe_cache=True),
        Case("doctor-json", "the same document as JSON — `ensure_ascii`, key ORDER and "
             "two-space indent included, which `encoding/json` cannot produce",
             ["doctor", "--json"]),
        Case("doctor-no-sync", "pod-dependent checks UNMEASURED with a stated reason, and the "
             "credential check still measured because the config load is a LOCAL file read",
             ["doctor", "--no-sync"]),
        Case("doctor-unreachable", "the pod UNMEASURED, and the cache-vs-pod join refusing to "
             "report a zero", ["doctor"], env=unreachable, compare=COMPARE_EXIT),
        Case("doctor-no-token", "a missing credential is a PROBLEM about the config, not an "
             "outage", ["doctor"], env=no_token, compare=COMPARE_EXIT),
        Case("doctor-with-mirror", "`CAIRN_MIRROR_ROOT` set to a WRITABLE tree: the frozen-"
             "mirror check is a PROBLEM and the scope check tags provenance per scope",
             ["doctor"], env={"CAIRN_MIRROR_ROOT": "<MIRROR>"}),
        Case("doctor-mirror-absent", "a configured mirror that does not exist is OK — nothing "
             "pre-cutover on this host — which is a different answer from unconfigured",
             ["doctor"], env={"CAIRN_MIRROR_ROOT": "<MIRROR>/absent"}),

        # --- the write verbs ---------------------------------------------------
        Case("append-ok", "a bullet lands, and the `X-Store-Status` token is printed",
             ["append", "--scope", "alpha-notes", "--ref", "widget-cfg",
              "--text", "a synthetic parity bullet.", "--session", "parity-session"]),
        Case("append-duplicate", "the SAME bullet again: `duplicate` is PRINTED, not swallowed, "
             "and it exits 0 — the property that makes a retry safe. The server recognises a "
             "bullet by CONTENT HASH, so the setup below is what makes the second one a repeat",
             ["append", "--scope", "alpha-notes", "--ref", "widget-cfg",
              "--text", "a synthetic parity bullet.", "--session", "parity-session"],
             setup=["append", "--scope", "alpha-notes", "--ref", "widget-cfg",
                    "--text", "a synthetic parity bullet.", "--session", "parity-session"]),
        Case("append-unknown-ref", "exit 6: the store refused and the caller must change the "
             "request", ["append", "--scope", "alpha-notes", "--ref", "ghost-ref",
                         "--text", "x", "--session", "s"], compare=COMPARE_EXIT),
        Case("append-unknown-scope", "the same code for a scope the store does not hold",
             ["append", "--scope", "ghost-void", "--ref", "x", "--text", "x", "--session", "s"],
             compare=COMPARE_EXIT),
        Case("append-over-the-cap",
             "🔴 REFUSED LOCALLY, BEFORE THE NETWORK, AND THE OVERAGE IS NAMED. Both clients "
             "read the cap from the SAME constant the server enforces, so a drifted copy on "
             "either side moves this row's message",
             ["append", "--scope", "alpha-notes", "--ref", "widget-cfg",
              "--text", "x" * 2001, "--session", "s"]),
        Case("append-astral",
             "🔴 AN ASTRAL CHARACTER, BECAUSE `json.dumps` DEFAULTS TO `ensure_ascii=True`. It "
             "travels as a surrogate PAIR; the server's guard once could not tell a pair from "
             "a LONE surrogate and 400'd every one. A client sending raw UTF-8 here would "
             "exercise the other half of that guard and this row would be a 400 on one side",
             ["append", "--scope", "alpha-notes", "--ref", "ledger-svc",
              "--text", "a bullet with an astral character: \U0001F5FA.", "--session", "s"]),
        Case("append-unreachable", "exit 7 — the write did NOT happen, and it is NOT exit 3",
             ["append", "--scope", "alpha-notes", "--ref", "widget-cfg", "--text", "x",
              "--session", "s"], env=unreachable, compare=COMPARE_EXIT),

        Case("put-derives-if-match", "the revision is derived from a LIVE sync and echoed on "
             "stderr, then the replace lands",
             ["put", "--scope", "alpha-notes", "--ref", "gauge-api", "--file", "<PUTFILE>"]),
        Case("put-stale-if-match", "exit 8: the precondition failed, which is its own code "
             "because the remedy is unique",
             ["put", "--scope", "alpha-notes", "--ref", "gauge-api", "--file", "<PUTFILE>",
              "--if-match", "0000000000000000"], compare=COMPARE_EXIT),
        Case("put-missing-file",
             "🔴 exit 2 NAMING THE FILE, NOT exit 7 ABOUT THE STORE. The read happens above "
             "`ResolveState`, not merely above the config load — moving it only as far as the "
             "config load left this row reporting the store as unreachable",
             ["put", "--scope", "alpha-notes", "--ref", "gauge-api", "--file", "<ABSENT>"]),
        Case("put-ambiguous-ref", "two cached files match, so no revision can be derived: "
             "exit 2 with the count",
             ["put", "--scope", "alpha-notes", "--ref", "ghost-ref", "--file", "<PUTFILE>"],
             compare=COMPARE_EXIT),
        Case("put-no-cache-is-7-not-3",
             "🔴 THE MEASURED DEFECT THIS ROW EXISTS FOR. With no cache the read verdict is 3, "
             "and returning it from a WRITE is the one code the design insists a write must "
             "never return — on its likeliest path, a fresh host",
             ["put", "--scope", "alpha-notes", "--ref", "gauge-api", "--file", "<PUTFILE>"],
             env=unreachable, compare=COMPARE_EXIT, wipe_cache=True),

        Case("create-ok", "a NEW entry, behind `If-None-Match: *`",
             ["create", "--scope", "beta-notes", "--ref", "fresh-entry", "--file", "<NEWFILE>"]),
        Case("create-exists",
             "🔴 exit 9, NOT 8, over the SAME 412 the server answers. The two are told apart by "
             "`X-Store-Status` alone, and collapsing them gives a caller an exit that says "
             "`re-derive and try again` for a condition no retry can change",
             ["create", "--scope", "beta-notes", "--ref", "fresh-entry", "--file", "<NEWFILE>"],
             compare=COMPARE_EXIT,
             setup=["create", "--scope", "beta-notes", "--ref", "fresh-entry",
                    "--file", "<NEWFILE>"]),
        Case("create-missing-file", "exit 2 naming the file, before any network",
             ["create", "--scope", "beta-notes", "--ref", "other", "--file", "<ABSENT>"]),

        # --- the HOSTILE archive: exit 5, which no honest pod can produce -------
        # 🔴 EACH ROW IS HOSTILE IN EXACTLY ONE WAY, BECAUSE THE GUARD ORDER IS THE CONTRACT. An
        # archive that was both a link and a traversal would be refused by whichever guard runs
        # first and would say nothing about the other. And each runs with a HEALTHY CACHE present,
        # because the property under test is that the refusal is NOT absorbed into "served from
        # cache" at exit 0 — a hostile archive rendering as a reassuring `⚠ SERVED FROM CACHE` is
        # the whole reason `StoreCorrupt` is not a `StoreUnreachable`.
        Case("corrupt-link", "exit 5: a tar member that is a LINK, refused by name",
             ["sync"], env=hostile_env("link"), presync=True),
        Case("corrupt-traversal", "exit 5: a member that escapes the extraction root — and the "
             "predicate is on path COMPONENTS, not on the substring `..`, because an entry "
             "legitimately named `a..b.md` aborted an entire sync once and rendered as an outage",
             ["sync"], env=hostile_env("traversal"), presync=True),
        Case("corrupt-duplicate", "exit 5: the same member twice",
             ["sync"], env=hostile_env("duplicate"), presync=True),
        Case("corrupt-miscount",
             "🔴 exit 5 ON A PERFECTLY WELL-FORMED ARCHIVE whose header declares 99 entries and "
             "whose body holds 1. That is what a TRUNCATED TRANSFER looks like, and the server's "
             "own comment claims it is `visible as a disagreement` — which is true only if "
             "somebody compares",
             ["sync"], env=hostile_env("miscount"), presync=True),
        Case("corrupt-does-not-degrade-a-READ",
             "the same hostile archive under `recall`: exit 5 and NO digest, even though a healthy "
             "cache is sitting right there. A read degrades for an OUTAGE and must not for this",
             ["recall", "--scope", "alpha-notes"], env=hostile_env("duplicate"), presync=True),

        # --- `--help`, which is how a human finds out what the tool does ---------
        # 🔴 EVERY OTHER ROW ASSERTS SOMETHING A CALLER ASKED THE TOOL TO DO, AND THAT IS WHY
        # THESE FOUR WERE MISSING. Before they existed the Go client exited **2 with nothing on
        # stdout** for all four where the oracle exits **0** with its help text — on the single
        # most common invocation there is. The gap was found by asking what the gate does not
        # send, not by any test.
        Case("help-top-level",
             "`cairn --help` is exit 0 WITH output. Text excluded: argparse's layout is a "
             "library's (declared difference 1); the code and a non-empty stdout are the contract",
             ["--help"], compare=COMPARE_EXIT_AND_STDOUT_NONEMPTY),
        Case("help-short-flag",
             "`cairn -h` is the same answer. It looked like an unknown SUBCOMMAND to an earlier "
             "draft of the parser, which is a different code path from `--help`",
             ["-h"], compare=COMPARE_EXIT_AND_STDOUT_NONEMPTY),
        Case("help-per-verb",
             "`cairn recall --help` documents the VERB, and it looked like a flag `recall` does "
             "not take — a third code path, and the one a reader reaches from the digest's footer",
             ["recall", "--help"], compare=COMPARE_EXIT_AND_STDOUT_NONEMPTY),
        Case("help-doctor",
             "`cairn doctor --help` carries the exit legend, which `doctor` also prints on every "
             "run — so a reader never has to find a skill to learn what a number meant",
             ["doctor", "--help"], compare=COMPARE_EXIT_AND_STDOUT_NONEMPTY),

        # --- ARGUMENT-SHAPE ROWS: a token that looks like an option -------------
        # 🔴 FOUR DIVERGENCES LIVED HERE AND ALL FOUR WENT THE DANGEROUS WAY — the Go client
        # SUCCEEDED where the oracle refuses. They were found by following the `--help` finding
        # one step further: if `-h` is special, what happens when it is a VALUE?
        Case("argv-help-in-a-VALUE-position",
             "🔴 `append --text -h` is exit 2 ON BOTH. It exited 0 printing help here, so a "
             "caller scripting `--text \"$MSG\"` whose message began with `-` would have read "
             "exit 0 as `the bullet landed`. Text excluded: argparse's `expected one argument`",
             ["append", "--scope", "alpha-notes", "--ref", "widget-cfg", "--text", "-h",
              "--session", "s"], compare=COMPARE_EXIT),
        Case("argv-option-shaped-value",
             "`--scope -weird` is exit 2, not a scope named `-weird`. Text excluded for the same "
             "reason: the refusal is argparse's on the oracle",
             ["recall", "--scope", "-weird"], compare=COMPARE_EXIT),
        Case("argv-flag-as-a-value",
             "`--scope --repo .` is exit 2 rather than a scope named `--repo`. Text excluded",
             ["recall", "--scope", "--repo", "."], compare=COMPARE_EXIT),
        Case("argv-negative-number-IS-a-value",
             "🔴 THE OTHER HALF, AND IT IS THE ONE THAT KEEPS THE RULE FROM BEING `refuse every "
             "dash`. argparse consumes a negative NUMBER as a value, so `--limit -1` reaches the "
             "reader's option ladder and is refused there with the READER's own message — which "
             "is why this row compares the full text",
             ["recall", "--scope", "alpha-notes", "--limit", "-1"]),
        Case("argv-terminator-passes-a-dash-token",
             "🔴 `--` ENDS THE FLAGS AND THE ORACLE HONOURS IT: `search -- -h` searches for the "
             "literal `-h`. A port that kept scanning for help printed documentation instead, so "
             "a caller searching for `-h` got the wrong answer at exit 0",
             ["search", "--scope", "alpha-notes", "--", "-h"]),
        Case("argv-h-in-a-POSITIONAL-position",
             "`search --scope S -h` is HELP on both, because argparse handles a standalone `-h` "
             "wherever it appears — the opposite ruling from the value position above, and the "
             "pair is what makes the rule observable",
             ["search", "--scope", "alpha-notes", "-h"],
             compare=COMPARE_EXIT_AND_STDOUT_NONEMPTY),

        # 🔴 AND `--help` WINS OVER AN UNKNOWN FLAG, IN EITHER ORDER AND AT BOTH LEVELS. argparse
        # COLLECTS unrecognised arguments and reports them AFTER parsing, while `-h` fires the
        # moment it is consumed — so a port that refused on the unknown flag exited 2 where the
        # oracle exits 0 with its help text. Measured in all three shapes below.
        Case("argv-help-beats-an-unknown-flag",
             "`recall --bogus-flag --help` is exit 0 with help. A port that returned on the "
             "unknown flag pre-empted it, which is the wrong ruling and the wrong stream",
             ["recall", "--bogus-flag", "--help"], compare=COMPARE_EXIT_AND_STDOUT_NONEMPTY),
        Case("argv-help-beats-an-unknown-flag-in-either-order",
             "`recall --help --bogus-flag` — the same answer, reached by the other path, which is "
             "what tells a deferral from a lucky ordering",
             ["recall", "--help", "--bogus-flag"], compare=COMPARE_EXIT_AND_STDOUT_NONEMPTY),
        Case("argv-help-beats-an-unknown-GLOBAL-flag",
             "`--bogus-global --help` is exit 0 with help too: the deferral is needed in the "
             "GLOBAL loop as well, and a fix applied to one loop leaves the other wrong",
             ["--bogus-global", "--help"], compare=COMPARE_EXIT_AND_STDOUT_NONEMPTY),

        # --- the usage surface -------------------------------------------------
        # 🔴 `compare="exit"` ON EVERY ROW BELOW, AND THE REASON IS THE SAME ONE EACH TIME: the
        # Python client's refusal text is argparse's. The CODE is the part of the usage contract
        # both clients share, and it is 2 on all of these.
        Case("usage-no-subcommand",
             "a bare invocation is exit 2. Text excluded: the oracle prints argparse's own "
             "`usage:` block, which this port does not reproduce (declared difference 1)",
             [], compare=COMPARE_EXIT),
        Case("usage-unknown-subcommand",
             "an unknown verb is exit 2. Text excluded for the same reason as the row above: "
             "argparse's `invalid choice` wording is a library's, not a contract",
             ["telepathy"], compare=COMPARE_EXIT),
        Case("usage-unknown-flag",
             "a flag the verb does not take is exit 2 — NOT a run that ignored it, which is the "
             "failure mode worth pinning. Text excluded: argparse's `unrecognized arguments`",
             ["recall", "--bogus-flag"], compare=COMPARE_EXIT),
        Case("usage-missing-required",
             "`append` without `--session` is exit 2 and sends NOTHING: every appended bullet must "
             "record the actor and the session, so a default would be a value nobody chose "
             "attached to a durable record. Text excluded: argparse's `required` wording",
             ["append", "--scope", "alpha-notes", "--ref", "widget-cfg", "--text", "x"],
             compare=COMPARE_EXIT),
        Case("usage-non-integer-limit",
             "`--limit banana` is exit 2 at the PARSER, before the reader's option ladder — a "
             "different rung from `--limit 0`, which the ladder refuses. Text excluded: "
             "argparse's `invalid int value` wording",
             ["recall", "--scope", "alpha-notes", "--limit", "banana"], compare=COMPARE_EXIT),
        Case("usage-doctor-takes-no-scope",
             "🔴 `doctor` MUST REFUSE `--scope`. It asks about this host and this credential, "
             "and a `--scope` here would invite the filtered-cache mistake",
             ["doctor", "--scope", "alpha-notes"], compare=COMPARE_EXIT),

        # --- `routes`: the scope→instance table --------------------------------
        # 🔴 THESE ROWS ARE WHAT KEEPS A NEW VERB AND A NEW EXIT CODE FROM BEING DECLARED ON ONE
        # CLIENT ONLY. The verb ledger in `test_parity_harness.py` requires every CLI verb to
        # appear in a case, and the exit-code ledger requires every documented code to be named
        # by one; a `routes` that existed in Python alone would leave both green while the two
        # clients disagreed about what the tool can do.
        Case("routes-none-configured",
             "the ordinary host: ONE instance, no table, so every scope resolves to `personal` "
             "and nothing is labelled. The full text is compared — this row is the one that "
             "would catch a Go port that labelled a single-instance banner",
             ["routes"]),
        Case("routes-table-printed",
             "a table present on a ONE-instance host. It prints, sorted, and STILL labels "
             "nothing: a table says where scopes live, not how many stores this host can reach",
             ["routes"], env={"CAIRN_ROUTES": "<ROUTES>"}),
        Case("routes-check-finds-an-unconfigured-alias",
             "🔴 exit 11, AND IT IS THE ONLY SINGLE-INSTANCE ROW THAT REACHES THAT CODE. The "
             "table routes a scope to an alias this host has no config for — a refusal that "
             "fires at ONE instance as well as at many. The same table ALSO names a scope that "
             "holds no entries, which prints as a ⚠ note and does NOT move the exit code: both "
             "the 🔴 finding and the ⚠ note are compared byte for byte, so a client that "
             "re-promoted the note would differ here",
             ["routes", "--check"], env={"CAIRN_ROUTES": "<ROUTES>"}),
        Case("routes-multi-instance-check",
             "🔴 THE ROW THE GATE DID NOT HAVE, AND ITS ABSENCE IS WHY A MISROUTE SHIPPED. Every "
             "other `routes` row points `SUBSYSTEM_STORE_CONFIG` at a path that does not exist, "
             "so `instances/` never exists and the walk is one instance long. This one "
             "configures a SECOND instance against a SECOND pod: both clients must label each "
             "banner with its own alias AND name that instance's OWN URL, sync each into its own "
             "cache root, and grade the table against the UNION of the two scope sets. A client "
             "that read every instance from the default config prints the default pod's URL "
             "under `cairn[secondary]`, which is a byte difference on this row",
             ["routes", "--check"], no_cache_flag=True,
             env={"SUBSYSTEM_STORE_CONFIG": "<MULTICFG>", "CAIRN_ROUTES": "<ROUTES2>"}),
        Case("routes-multi-instance-refuses-an-explicit-cache",
             "🔴 exit 2: a fan-out over N instances with ONE explicit `--cache` would unpack two "
             "stores into one directory, interleaving their scopes while `.sync-stamp` dated "
             "whichever synced last. Both clients refuse BEFORE the walk, and this row is the "
             "one that keeps `no_cache_flag` above from being the only multi-instance path",
             ["routes", "--check"],
             env={"SUBSYSTEM_STORE_CONFIG": "<MULTICFG>", "CAIRN_ROUTES": "<ROUTES2>"}),
        Case("routes-check-refuses-a-STALE-cache",
             "🔴 exit 11 FOR A DIFFERENT REASON, AND THE DISTINCTION IS THE POINT: `--no-sync` "
             "with no cache means the scope set would be a fact about this disk rather than "
             "about the table, so the check REFUSES to grade rather than inventing findings in "
             "both directions",
             ["routes", "--check", "--no-sync"], env={"CAIRN_ROUTES": "<ROUTES>"},
             wipe_cache=True),

        # --- READ verbs on a MULTI-INSTANCE host --------------------------------
        # 🔴 THE ROWS DECLARED DIFFERENCE 8 NAMED AS ITS CLOSING CONDITION, AND THEY ARE FULL
        # BYTE COMPARISONS ON PURPOSE. Until this landed the Go client REFUSED every read verb
        # at exit 11 on a host with more than one instance, so an exit-only row would have
        # compared two refusals — or, worse, two different answers that happened to share a
        # code. What has to match is the TEXT: which instance's cache was read, which alias the
        # banner carries, and whether the rendered caveat gained its multi-instance clause.
        #
        # ⚠ ALL THREE NEED `no_cache_flag`, and not as a convenience. An explicit `--cache` on
        # a fan-out is refused at exit 2 by BOTH clients before the walk begins, which compares
        # equal and measures nothing — the same trap `routes-multi-instance-check` documents.
        Case("recall-routed-to-a-NON-DEFAULT-instance",
             "🔴 THE ROUTED READ, COMPARED BYTE FOR BYTE, AND IT IS THE ROW THE CAVEAT EXISTS "
             "FOR. `gamma-notes` lives on the SECOND pod and only there, so a client that read "
             "the default instance answers `scope-absent` — a confident nothing out of a store "
             "nobody chose. Every line is load-bearing: the banner must read `cairn[secondary]` "
             "and name the SECOND pod's URL, and the rendered caveat must carry `this run read "
             "the `secondary` instance ONLY`, which is what tells a reader an absence here is "
             "not an absence from the fleet",
             ["recall", "--scope", "gamma-notes"], no_cache_flag=True,
             env={"SUBSYSTEM_STORE_CONFIG": "<MULTICFG>", "CAIRN_ROUTES": "<ROUTES2>"}),
        Case("recall-routed-to-the-DEFAULT-instance-is-still-labelled",
             "🔴 THE OTHER DIRECTION, WHICH IS WHAT MAKES THE ROW ABOVE A MEASUREMENT RATHER "
             "THAN A COINCIDENCE. `alpha-notes` routes to `personal`, so the answer comes from "
             "the FIRST pod — and it is STILL labelled `cairn[personal]` with the clause naming "
             "`personal`, because the label asks the instance COUNT and not which alias won. A "
             "client that labelled only non-default instances passes the row above and fails "
             "this one",
             ["recall", "--scope", "alpha-notes"], no_cache_flag=True,
             env={"SUBSYSTEM_STORE_CONFIG": "<MULTICFG>", "CAIRN_ROUTES": "<ROUTES2>"}),
        Case("ls-entries-walks-EVERY-instance",
             "🔴 THE FAN-OUT, AND THE PREFIX IS A `[alias] ` ON THE LINE RATHER THAN A THIRD "
             "PATH SEGMENT. Both banners, both caches, both listings, in discovery order — and "
             "a consumer splits these on `/` expecting exactly two parts, so a client that "
             "wrote `alias/scope/entry.md` would re-point every such split at the wrong field "
             "while containing the alias just as happily. A client that walked only the default "
             "instance loses `gamma-notes/gauge-api.md` entirely",
             ["ls-entries"], no_cache_flag=True,
             env={"SUBSYSTEM_STORE_CONFIG": "<MULTICFG>", "CAIRN_ROUTES": "<ROUTES2>"}),
        Case("validate-routed-to-a-NON-DEFAULT-instance",
             "🔴 THE ROW THAT CAUGHT AN ORACLE DEFECT, AND IT HAD TO COMPARE STDOUT TO DO IT. "
             "`validate` routes `gamma-notes` to the SECOND pod, and the COUNT it prints is the "
             "whole evidence anything was checked — the command exists to make a zero mean "
             "something. The oracle globbed the DEFAULT instance's root while parsing the "
             "ROUTED one, so it answered `0 of 0 entry file(s) parse` for a scope holding one "
             "readable entry, and `-1 of 0 … 1 malformed` once the routed store held a bad "
             "file. Both sides exit 0 here and both exit 5 there, so `compare=\"exit\"` sees "
             "NEITHER: the count is on stdout or it is nowhere",
             ["validate", "--scope", "gamma-notes"], no_cache_flag=True,
             env={"SUBSYSTEM_STORE_CONFIG": "<MULTICFG>", "CAIRN_ROUTES": "<ROUTES2>"}),

        # --- a routed WRITE at a non-default alias -----------------------------
        # 🔴 THE REGION THAT HAD ZERO BYTE COMPARISON, AND IT IS THE ONE THIS WORK EXISTS TO
        # SHIP. When this row was written (`baee2f0`) the only `<MULTICFG>` rows above it were
        # the two `routes --check` GRADER rows, so the routed WRITE path was covered only by
        # each client's own unit tests, which cannot compare bytes across the two. A live
        # divergence was measured in exactly that gap (`put` against a routed instance with an
        # incomplete config: same exit code, different sentence, because the oracle loads the
        # credentials lazily and the port did not).
        #
        # ⚠ PAST TENSE, AND DELIBERATELY SO: THE SENTENCE ABOVE IS HISTORY AND STOOD IN THE
        # PRESENT TENSE FOR TWO COMMITS AFTER IT STOPPED BEING TRUE. Counted per commit —
        # `<MULTICFG>` env rows declared ABOVE this one — `baee2f0` 2, `0c187d7` 5, `d57f46b` 5,
        # `b28787b` 6. It was exact when written and false from the second of those; the same
        # commit that corrected the sibling sentence in `ci.yml` to past tense left this one.
        # The four added rows are routed READS (`recall` to a non-default alias, `recall` to the
        # default one, `ls-entries`' fan-out, `validate`'s routed COUNT), all four full
        # stdout/stderr/exit comparisons. Nothing asserts on that six, so COUNT the rows rather
        # than trusting it; what still holds, and is this row's whole reason, is that every one
        # of those six READS and this is the only one that WRITES.
        #
        # ⚠ IT GOES LAST ON PURPOSE. It is the only row that WRITES to the second pod, and the
        # `routes --check` rows above read that pod's scope set; `restore_store` on the second
        # store makes the order irrelevant, and this placement means nothing depends on that
        # being true.
        Case("put-routed-to-a-NON-DEFAULT-instance",
             "🔴 THE ROUTED WRITE, COMPARED BYTE FOR BYTE. `gamma-notes` lives on the SECOND "
             "pod and only there, so every step has to be the routed one: the table resolves "
             "the alias, the ROUTED credentials are loaded, the ROUTED cache is synced, the "
             "`If-Match` is derived from the SECOND store's bytes and echoed on stderr, and "
             "`instance=secondary` is printed. A client that used the default instance for any "
             "one of those hits pod 1, whose token does not carry `gamma-notes` at all — so "
             "this row moves rather than merely reporting a different revision",
             ["put", "--scope", "gamma-notes", "--ref", "gauge-api", "--file", "<PUTFILE>"],
             no_cache_flag=True,
             env={"SUBSYSTEM_STORE_CONFIG": "<MULTICFG>", "CAIRN_ROUTES": "<ROUTES2>"}),
    ]


# =============================================================================
# Running the pair
# =============================================================================

@dataclass
class Outcome:
    rc: int
    stdout: str
    stderr: str


def free_port() -> int:
    s = socket.socket()
    s.bind(("127.0.0.1", 0))
    port = s.getsockname()[1]
    s.close()
    return port


def start_oracle(store: Path, token_file: Path, log: Path, port: int,
                 break_pod: bool = False) -> subprocess.Popen:
    env = dict(os.environ)
    env.update({
        # The limiter answers the SAME uniform 401 a bad token does — that is the design — so
        # once it trips a correctly authorised request also answers 401 and every row after it
        # compares two wrong answers to each other. The corpus's own runner raises this ceiling
        # for exactly this reason.
        "SUBSYSTEM_STORE_MAX_FAILURES": "1000000",
        # 🔴 THE TRUSTED-PROXY SET MUST NOT CONTAIN THE HARNESS ITSELF, AND COPYING THE
        # CONFORMANCE RUNNER'S VALUE MADE THIS WHOLE GATE VACUOUS. `127.0.0.1/32` tells the
        # server that the loopback peer is a PROXY, after which every DIRECT request is refused
        # `401 status=no-client-ip` because no forwarded client-IP header accompanies it. Both
        # clients were refused identically, so all 72 rows PASSED — comparing two failures to
        # each other — and no cache was ever written. Caught by reading the oracle's audit log,
        # NOT by the green, which is why `preflight()` below now makes that state impossible.
        #
        # The server REFUSES TO START with no value at all (there is deliberately no default),
        # so the set names an address the harness cannot be: TEST-NET-1, reserved by RFC 5737
        # for documentation and assigned to nobody. An untrusted peer's client IP is its own
        # socket address, which is what the limiter then keys on.
        # 🔴 `--break-pod` PUTS THE LOOPBACK BACK IN THE TRUSTED SET, WHICH IS THE INCIDENT. It
        # exists as a CONTROL and nothing else: with it, every direct request is refused
        # `401 status=no-client-ip`, both clients fail identically, every row would compare equal
        # — and the pre-flight has to refuse to vouch instead of reporting that as a green.
        "SUBSYSTEM_STORE_TRUSTED_PROXIES": "127.0.0.1/32" if break_pod else "192.0.2.1/32",
        "CAIRN_HOST": PARITY_HOST,
    })
    handle = log.open("wb")
    return subprocess.Popen(
        [sys.executable, str(ROOT / "server" / "server.py"),
         "--store", str(store), "--host", "127.0.0.1", "--port", str(port),
         "--token-file", str(token_file)],
        stdout=handle, stderr=subprocess.STDOUT, env=env,
    )


def wait_for_health(port: int, proc: subprocess.Popen, log: Path) -> None:
    import urllib.error
    import urllib.request

    deadline = time.time() + BOOT_TIMEOUT_S
    while time.time() < deadline:
        if proc.poll() is not None:
            raise RuntimeError(
                f"the oracle exited {proc.returncode} before answering /healthz:\n"
                + log.read_text(errors="replace")
            )
        try:
            with urllib.request.urlopen(f"http://127.0.0.1:{port}/healthz", timeout=1) as r:
                if r.status == 200:
                    return
        except (urllib.error.URLError, OSError):
            time.sleep(0.05)
    raise RuntimeError("the oracle never answered /healthz")


def restore_store(pristine: Path, store: Path) -> None:
    """Put the store back exactly as built — CONTENT AND MTIMES.

    🔴 EVERY CASE RUNS AGAINST A PRISTINE STORE, AND EVERY CLIENT IN A PAIR RUNS AGAINST THE
    SAME ONE. Three of the write verbs CHANGE the store, and the two clients run in sequence, so
    without this the second client sees the first one's write: `create-ok` had the oracle create
    the entry and the Go client answer `already-exists`, which the harness reported as a
    divergence that did not exist. It also removes the ordering dependency between rows — a row
    that needs an earlier write declares `setup` instead.

    🔴 `copy2`, SO MTIMES SURVIVE. The index ORDER is decided by comparing mtimes, and a restore
    that reset them to now would make the featured pick and the listing order depend on the
    order the copy loop ran in — which is the silent reordering this whole project exists to
    prevent.
    """
    shutil.rmtree(store, ignore_errors=True)
    shutil.copytree(pristine, store, copy_function=shutil.copy2)


def preflight(port: int) -> tuple[int, int]:
    """Fetch the snapshot as the harness, and return `(status, declared entries)`.

    🔴 THIS IS THE POSITIVE CONTROL ON THE WHOLE GATE, AND IT EXISTS BECAUSE THE GATE WAS
    MEASURED VACUOUS WITHOUT IT. With `SUBSYSTEM_STORE_TRUSTED_PROXIES` set to the loopback —
    copied from the conformance runner, where it is correct — the server refused every DIRECT
    request `401 status=no-client-ip`. Both clients were refused identically, so all 72 rows
    reported PASS while comparing two failures to each other, no cache was ever written, and
    every report rendered `store-unreachable`. A zero here is indistinguishable from a harness
    wired to nothing; this is the number that must move.

    It refuses with exit 2 rather than 1, because "could not vouch" is not "failed".
    """
    import urllib.error
    import urllib.request

    req = urllib.request.Request(f"http://127.0.0.1:{port}/api/v1/snapshot", method="GET")
    req.add_header("Authorization", f"Bearer {W.TOKEN}")
    req.add_header("User-Agent", "cairn-parity-preflight/1")
    try:
        with urllib.request.urlopen(req, timeout=10) as resp:
            declared = resp.headers.get("X-Store-Entries")
            body = resp.read()
            return resp.status, int(declared) if declared and declared.isdigit() else -1
    except urllib.error.HTTPError as exc:
        return exc.code, -1


def run_client(cmd: list[str], cwd: Path, env: dict[str, str]) -> Outcome:
    proc = subprocess.run(cmd, cwd=str(cwd), env=env, capture_output=True)
    return Outcome(
        rc=proc.returncode,
        stdout=proc.stdout.decode("utf-8", "replace"),
        stderr=proc.stderr.decode("utf-8", "replace"),
    )


def unified(a: str, b: str, label_a: str, label_b: str) -> str:
    import difflib
    lines = list(difflib.unified_diff(
        a.splitlines(), b.splitlines(), fromfile=label_a, tofile=label_b, lineterm="", n=2))
    return "\n".join("    " + line for line in lines[:60])


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(
        prog="tests/parity/harness.py",
        description="Run both cairn clients over one cache root and diff what they produced.")
    parser.add_argument("--go-binary", default=None,
                        help="the built Go client; built into a temp dir when omitted")
    parser.add_argument("--only", default=None,
                        help="run a comma-separated subset of case ids, in declaration order")
    parser.add_argument("--keep", action="store_true", help="keep the world for inspection")
    parser.add_argument("--break-pod", action="store_true",
                        help="the NEGATIVE CONTROL ON THE PRE-FLIGHT: configure the pod to refuse "
                             "every direct request, and refuse to vouch instead of reporting a "
                             "green over two identical failures")
    parser.add_argument("--self-test", action="store_true",
                        help="the NEGATIVE CONTROL: sabotage the Go side of three rows and "
                             "refuse unless the differ reports each one")
    args = parser.parse_args(argv)

    work = Path(tempfile.mkdtemp(prefix="cairn-parity-", dir=tempfile.gettempdir()))
    norms = normalizations()
    try:
        store = W.build_store(work / "store")
        # The pristine copy every case is restored from. Built once, never served.
        pristine = work / "store-pristine"
        shutil.copytree(store, pristine, copy_function=shutil.copy2)
        repo = W.build_repo(work / "repos")
        token_file = work / "tokens"
        token_file.write_text(W.TOKEN_ROW, encoding="utf-8")
        cache = work / "cache"
        home = work / "home"
        home.mkdir()
        # A WRITABLE mirror tree, which is what makes the `frozen-mirror` PROBLEM row real.
        mirror = work / "mirror"
        (mirror / "alpha-notes").mkdir(parents=True)
        (mirror / "alpha-notes" / "stale-entry.md").write_text("stale\n", encoding="utf-8")
        (mirror / "mirror-only-scope").mkdir(parents=True)
        (mirror / "mirror-only-scope" / "left-behind.md").write_text("x\n", encoding="utf-8")
        put_file = work / "replacement.md"
        put_file.write_text(
            "---\nservice: gauge-api\nscope: alpha-notes\n---\n\n"
            "## What it is\n\nreplaced by the parity harness.\n\n"
            "## Pointers\n\n- `apps/gauge-api/values.yaml`\n\n"
            "## Nuance / work-history\n\n- 2000-01-04: replaced.\n", encoding="utf-8")
        # The scope→instance table the `routes` rows point `$CAIRN_ROUTES` at.
        #
        # 🔴 IT NAMES THREE THINGS DELIBERATELY, AND THEY GRADE DIFFERENTLY. `ghost-void` is
        # routed to an alias this host has no config for — a 🔴 PROBLEM at one instance as well
        # as at many, and what takes this row to exit 11. `hollow-set` is a scope the world
        # holds as a DIRECTORY WITH NO ENTRIES (`world.EMPTY_SCOPES`), so it is present to the
        # server and absent from every client cache: a ⚠ NOTE, which prints and does NOT move
        # the exit code. Both texts are compared byte for byte, so a client that graded the
        # note as a verdict — or dropped it — differs on this row.
        #
        # ⚠ THE EXISTS-BUT-EMPTY CASE WAS UNREACHABLE HERE BEFORE. The old table named
        # `retired-scope`, which exists nowhere at all, and the two states are indistinguishable
        # from a snapshot — so the row could not tell a demotion from a deletion.
        #
        # 🔴 IT LIVES IN A SUBDIRECTORY, AND THE OBVIOUS PLACE WAS WRONG. The comment here used
        # to say that writing it beside the world "keeps every OTHER row's 'no table at all'
        # state intact", and that was FALSE: the default table path is
        # `Path($SUBSYSTEM_STORE_CONFIG).parent / "routes.json"`, and this world points that
        # variable at `<work>/no-such-config` — so `<work>/routes.json` WAS the default path and
        # the table was live for every row in the gate, `CAIRN_ROUTES` or not. Nothing went red,
        # because both clients read the same table and compared equal; the cost was that
        # `routes-none-configured` measured a host that HAD a table while its own description
        # said otherwise. Found by adding an entry for `ghost-void` to this table and watching
        # three unrelated `*-absent-scope` rows change behaviour. A directory of its own makes
        # the description true, and makes `$CAIRN_ROUTES` the only way a row opts in.
        (work / "tables").mkdir()
        routes_table = work / "tables" / "routes.json"
        routes_table.write_text(
            '{"alpha-notes": "personal", "beta-notes": "personal", '
            '"hollow-set": "personal", "ghost-void": "no-such-instance"}\n', encoding="utf-8")
        new_file = work / "created.md"
        new_file.write_text(
            "---\nservice: fresh-entry\nscope: beta-notes\n---\n\n"
            "## What it is\n\ncreated by the parity harness.\n\n"
            "## Pointers\n\n- `apps/fresh-entry/values.yaml`\n\n"
            "## Nuance / work-history\n\n- 2000-01-04: created.\n", encoding="utf-8")

        # 🔴 A SECOND STORE AND A SECOND POD, FOR THE MULTI-INSTANCE ROWS. Pointing the second
        # instance at the FIRST pod would have been cheaper and would have measured nothing: the
        # defect these rows exist to catch is a client that fetches every instance from the
        # DEFAULT instance's config, and with one URL that client and a correct one are
        # byte-identical. The two pods must be distinguishable, so they are.
        #
        # ⚠ IT CARRIES A SCOPE THE FIRST STORE DOES NOT. The URL in the banner is one
        # observable; the graded scope set is the other, and a walk that never read this
        # instance reports `gamma-notes` as a table entry matching nothing.
        second_store = W.build_store(work / "store-second")
        (second_store / "gamma-notes").mkdir(parents=True, exist_ok=True)
        (second_store / "gamma-notes" / "gauge-api.md").write_text(
            "---\nservice: gauge-api\nscope: gamma-notes\n---\n\n"
            "## What it is\n\nthe second instance's own entry.\n\n"
            "## Pointers\n\n- `apps/gauge-api/values.yaml`\n\n"
            "## Nuance / work-history\n\n- 2000-01-05: synthetic.\n", encoding="utf-8")
        # 🔴 A PINNED MTIME, LIKE EVERY OTHER MEMBER OF THIS WORLD. Writing the file sets the
        # mtime to NOW, which lands a wall-clock date in `X-Store-Snapshot`'s `newest=` and
        # therefore in the banner this row compares. It is identical for both clients within a
        # run, so it hides no difference — but it makes two runs' logs incomparable by eye, and
        # it puts a real date in a repository whose fixtures are deliberately year-2000.
        os.utime(second_store / "gamma-notes" / "gauge-api.md",
                 ns=(W.EPOCH_NS + 9, W.EPOCH_NS + 9))
        # 🔴 THE SECOND STORE GETS A PRISTINE COPY TOO, FOR THE REASON `restore_store`'s
        # docstring already gives: a WRITE row runs the two clients in sequence, so without a
        # restore between them the Go client sees the oracle's write. The routed `put` row
        # below derives its `If-Match` from the bytes it finds, so the two arms would derive
        # DIFFERENT preconditions and the harness would report a divergence that is its own
        # doing. Nothing needed this while every `<MULTICFG>` row was a `routes --check`.
        second_pristine = work / "store-second-pristine"
        shutil.copytree(second_store, second_pristine, copy_function=shutil.copy2)
        second_tokens = work / "tokens-second"
        second_tokens.write_text(
            W.TOKEN_ROW.rstrip("\n").replace(
                ",".join(W.ALLOWED_SCOPES),
                ",".join(W.ALLOWED_SCOPES + ("gamma-notes",))) + "\n", encoding="utf-8")

        # The multi-instance HOME. 🔴 A DIRECTORY OF ITS OWN, NOT THE ONE EVERY OTHER ROW USES:
        # creating `instances/` beside the shared config path would make EVERY read row
        # multi-instance, so every banner would gain an alias and every rendered caveat a
        # clause — thirty rows whose stated subject is not routing, all comparing different
        # bytes than they were written to compare. Opting in per row is what keeps the
        # single-instance bytes the DEFAULT of this gate, which is the compatibility guarantee
        # the labelling rests on. (Before the read verbs routed, the same separation was needed
        # for a blunter reason: the Go client refused those rows outright at exit 11.)
        multi_dir = work / "multi-config"
        (multi_dir / "instances").mkdir(parents=True)
        multi_config = multi_dir / "env"          # deliberately NOT created: the DEFAULT
        multi_routes = multi_dir / "routes.json"  # instance comes from the environment

        closed = free_port()  # bound and released, so a connect to it is REFUSED
        port = free_port()
        second_port = free_port()
        log = work / "oracle.log"
        second_log = work / "oracle-second.log"

        # 🔴 EVERY LONG-LIVED HANDLE IS STARTED *INSIDE* THE `try`, AND THIS IS A MEASURED
        # LEAK RATHER THAN A TIDINESS RULE. The starts used to sit ABOVE the `try` whose
        # `finally` reaps them, so anything that raised between the first start and the `try`
        # — a second `start_oracle` that could not bind, a `hostile.start()` that failed —
        # left the handles already created running with nothing to reap them. Measured: four
        # orphaned `server/server.py` processes survived one round of this harness and had to
        # be resolved and killed by PID afterwards. The window grew from one handle to three
        # as the multi-instance rows were added, which is the shape to watch: a new handle is
        # cheap to add and its reaper is easy to forget.
        #
        # 🔴 AND THE NAMES ARE BOUND TO `None` FIRST, because `finally` runs on the way out of
        # a `try` the interpreter entered — including when the raise happened on the FIRST
        # line of it. Without these three bindings the reaper would itself raise
        # `UnboundLocalError`, which replaces the original exception and loses the reason.
        hostile_server = None
        proc = None
        second_proc = None
        try:
            hostile_server, hostile_port = hostile.start()
            proc = start_oracle(store, token_file, log, port, break_pod=args.break_pod)
            second_proc = start_oracle(second_store, second_tokens, second_log, second_port,
                                       break_pod=args.break_pod)
            # 🔴 PRINTED, SO "THEY WERE REAPED" IS CHECKABLE INSTEAD OF ASSERTED. A reader who
            # suspects a leak needs the PIDs this run owns; a `pgrep -f server/server.py`
            # sweep cannot tell this run's pods from a sibling run's — or from the shell
            # doing the sweeping.
            print(f"PODS pids={proc.pid},{second_proc.pid}")
            wait_for_health(port, proc, log)
            wait_for_health(second_port, second_proc, second_log)
            (multi_dir / "instances" / "secondary.env").write_text(
                f"SUBSYSTEM_STORE_URL=http://127.0.0.1:{second_port}\n"
                f"SUBSYSTEM_STORE_TOKEN={W.TOKEN}\n", encoding="utf-8")
            multi_routes.write_text(
                '{"alpha-notes": "personal", "beta-notes": "personal", '
                '"gamma-notes": "secondary"}\n', encoding="utf-8")

            go_binary = args.go_binary
            if not go_binary:
                go_binary = str(work / "cairn-go")
                subprocess.run(["go", "build", "-C", str(ROOT), "-o", go_binary, "./cmd/cairn"],
                               check=True)

            # 🔴 CLEARED BY PREFIX FIRST — AND THE OMISSION THAT MOTIVATED IT WAS
            # THIS BLOCK'S. It pinned five names over an inherited `os.environ`
            # and did NOT pin `CAIRN_ROUTES`, which every other site in the tree
            # clears. Measured: with `CAIRN_ROUTES=/nonexistent/routes.json`
            # exported, an explicit table that does not exist is an ERROR, the
            # oracle's sync produces no entry files, and this gate exits **2** —
            # `REFUSING TO VOUCH: no row produced a LIVE banner and a rendered
            # digest`. Not a false green: the content floor does its job. But the
            # operator is told their RUN measured refusals, not that their SHELL
            # did it, and a per-case `CAIRN_ROUTES` override cannot help the rows
            # that set none. `env_pin` clears the whole prefix, so the rows that
            # DO set one still get exactly what they set.
            base_env = env_pin.sanitized_env(
                HOME=str(home),
                CAIRN_HOST=PARITY_HOST,
                SUBSYSTEM_STORE_URL=f"http://127.0.0.1:{port}",
                SUBSYSTEM_STORE_TOKEN=W.TOKEN,
                # 🔴 POINTED AT A FILE THAT DOES NOT EXIST, DELIBERATELY. Both clients read a
                # config file when the environment does not supply a value; letting them fall
                # back to the operator's real `~/.config` would make the run depend on the
                # machine it ran on.
                SUBSYSTEM_STORE_CONFIG=str(work / "no-such-config"),
                CAIRN_MIRROR_ROOT="",
                GIT_CONFIG_GLOBAL="/dev/null",
                GIT_CONFIG_SYSTEM="/dev/null",
            )

            status, declared = preflight(port)
            print(f"PREFLIGHT status={status} declared-entries={declared}")
            if status != 200 or declared < 1:
                print("REFUSING TO VOUCH: the harness itself could not fetch a non-empty "
                      "snapshot, so every row below would compare two FAILURES to each other "
                      "and report PASS. That is the exact state this run was measured in "
                      "before the pre-flight existed.", file=sys.stderr)
                return 2

            passes = 0
            failures: list[str] = []
            #: 🔴 THE CONTENT FLOOR, AND IT IS A DIFFERENT CLAIM FROM THE PRE-FLIGHT. The
            #: pre-flight proves the POD answers; these prove the CLIENTS got as far as a live
            #: fetch and a rendered digest. A run where every report said `store-unreachable`
            #: would clear the pre-flight and fail these.
            saw_live_banner = False
            saw_rendered_digest = False
            wanted = None if args.only is None else set(args.only.split(","))
            selected = [c for c in cases(closed, hostile_port)
                        if wanted is None or c.id in wanted]
            if not selected:
                print(f"REFUSING: --only {args.only!r} selected no case", file=sys.stderr)
                return 2
            for case in selected:
                if case.wipe_cache:
                    shutil.rmtree(cache, ignore_errors=True)
                    for leftover in work.glob("cache.*"):
                        if leftover.is_dir():
                            shutil.rmtree(leftover, ignore_errors=True)
                        else:
                            leftover.unlink()
                env = dict(base_env)
                for key, value in case.env.items():
                    env[key] = (value
                                .replace("<MIRROR>", str(mirror))
                                .replace("<ROUTES>", str(routes_table))
                                .replace("<ROUTES2>", str(multi_routes))
                                .replace("<MULTICFG>", str(multi_config)))
                argv_case = [
                    a.replace("<PUTFILE>", str(put_file))
                     .replace("<NEWFILE>", str(new_file))
                     .replace("<ABSENT>", str(work / "no-such-file.md"))
                    for a in case.argv
                ]
                cwd = repo if case.in_repo else work
                # A `no_cache_flag` row passes NO `--cache`, so both clients resolve their own
                # per-alias roots; `once()` wipes those per client.
                shared = ([] if (case.no_cache_flag or not argv_case)
                          else ["--cache", str(cache)])

                def once(cmd: list[str]) -> Outcome:
                    # 🔴 THE DERIVED ROOTS ARE WIPED PER CLIENT, INSIDE HERE — AND THE COMMENT
                    # THAT USED TO SIT OUTSIDE CLAIMED ISOLATION THIS WIPE DID NOT DELIVER.
                    # Without `--cache` both clients resolve
                    # `$HOME/.cache/subsystem-store[-<alias>]`. The wipe ran ONCE per case,
                    # above both `once()` calls, so the ORACLE populated those roots and the Go
                    # client then ran against them — the exact state the comment said it
                    # prevented. It was harmless only because both rows fetch live and
                    # `install_snapshot` replaces the root wholesale, i.e. the safety came from
                    # a different function's behaviour; a `no_cache_flag` row combined with
                    # `--no-sync` would have compared the Go client against the oracle's cache
                    # and reported PASS. Per-client is the structural fix, and it is the same
                    # reason `restore_store` below is per-client rather than per-case.
                    if case.no_cache_flag:
                        for root in (home / ".cache").glob("subsystem-store*"):
                            shutil.rmtree(root, ignore_errors=True)
                    restore_store(pristine, store)
                    restore_store(second_pristine, second_store)
                    if case.presync:
                        run_client([sys.executable, str(ROOT / "cairn"), "--cache", str(cache),
                                    "sync"], work, base_env)
                    if case.setup:
                        run_client([sys.executable, str(ROOT / "cairn")] + shared +
                                   [a.replace("<NEWFILE>", str(new_file))
                                     .replace("<PUTFILE>", str(put_file))
                                    for a in case.setup], cwd, env)
                    return run_client(cmd, cwd, env)

                py = once([sys.executable, str(ROOT / "cairn")] + shared + argv_case)
                # 🔴 THE SABOTAGE IS APPLIED TO THE GO SIDE ONLY, AND WITH REALISTIC ARGUMENTS.
                # A textbook mutant (an empty argv, a nonexistent binary) would prove the runner
                # can crash, not that the COMPARISON can see a difference — the thing being
                # controlled for is the differ, and it has to be fed two well-formed runs that
                # genuinely disagree.
                go_argv = argv_case
                if args.self_test and case.id in SABOTAGE:
                    how, extra = SABOTAGE[case.id]
                    go_argv = (argv_case + extra) if how == "append" else extra
                go = once([go_binary] + shared + go_argv)

                if case.compare == COMPARE_EXIT_AND_STDOUT_NONEMPTY:
                    problems = []
                    if py.rc != go.rc:
                        problems.append(f"exit {py.rc} (oracle) vs {go.rc} (go)")
                    for label, out in (("oracle", py.stdout), ("go", go.stdout)):
                        if not out.strip():
                            problems.append(f"{label} put NOTHING on stdout")
                    if problems:
                        failures.append(case.id)
                        print(f"FAIL {case.id} — " + "; ".join(problems))
                        print(f"     why: {case.why}")
                    else:
                        print(f"PASS {case.id} (exit {py.rc}, both stdout non-empty: "
                              f"oracle {len(py.stdout)}B, go {len(go.stdout)}B)")
                        passes += 1
                    continue

                if case.compare == COMPARE_EXIT:
                    if py.rc == go.rc:
                        print(f"PASS {case.id} (exit only: {py.rc})")
                        passes += 1
                    else:
                        failures.append(case.id)
                        print(f"FAIL {case.id} — exit {py.rc} (oracle) vs {go.rc} (go)")
                        print(f"     why: {case.why}")
                        print(f"     oracle stderr: {py.stderr.strip()[:400]}")
                        print(f"     go stderr:     {go.stderr.strip()[:400]}")
                    continue

                def norm(text: str) -> str:
                    for n in norms:
                        text = n.apply(text)
                    return text

                if "cairn: live — fetched from" in py.stdout:
                    saw_live_banner = True
                if "FEATURED IN FULL" in py.stdout:
                    saw_rendered_digest = True

                py_out, go_out = norm(py.stdout), norm(go.stdout)
                py_err, go_err = norm(py.stderr), norm(go.stderr)
                problems = []
                if py.rc != go.rc:
                    problems.append(f"exit {py.rc} (oracle) vs {go.rc} (go)")
                if py_out != go_out:
                    problems.append("stdout differs\n" + unified(py_out, go_out, "oracle", "go"))
                if py_err != go_err:
                    problems.append("stderr differs\n" + unified(py_err, go_err, "oracle", "go"))
                if problems:
                    failures.append(case.id)
                    print(f"FAIL {case.id}")
                    print(f"     why: {case.why}")
                    for problem in problems:
                        print("     " + problem)
                else:
                    print(f"PASS {case.id}")
                    passes += 1

            # 🔴 THE DEAD-LICENCE REFUSAL IS A CLAIM ABOUT THE WHOLE SET, so a subset run
            # reports it and does not FAIL on it. A `--only` run that "failed" because a
            # normalization for a case it did not run went unused would be a permanently-red
            # diagnostic, which trains everyone to ignore the line that matters.
            # 🔴 THE MTIME CLAIM, MADE STRUCTURALLY RATHER THAN THROUGH THE RENDERED ORDER.
            # Each client syncs into its OWN root and the two trees are compared file by file. The
            # rendered order is what CAUGHT a dropped mtime; this is what PINS it, because a
            # rendered order can agree by accident of three files landing in the right sequence
            # while every timestamp is wrong.
            #
            # 🔴 THE COMPARISON IS ON THE DOUBLE THE READER ACTUALLY COMPARES, NOT ON RAW
            # NANOSECONDS, AND THAT IS A MEASUREMENT RATHER THAN A CONCESSION. The oracle's
            # `tarfile` carries a PAX mtime as a Python FLOAT and `os.utime` writes it back, so it
            # loses sub-microsecond precision that Go's exact decimal parse keeps: measured
            # 1789432421885236263 (oracle) against 1789432421885236300 (Go) on the seed stamp, a
            # 37 ns difference. Both collapse to the SAME double — `float(sec) + 1e-9*nsec`, which
            # is what `report.pyMtime` computes and what the index order is decided on — because
            # one ULP at that magnitude is 238 ns. Measured at FOUR epoch magnitudes rather than
            # one: year 2000 -> 119 ns, now -> 238 ns, 2038 -> 238 ns, 2100 -> 477 ns. The
            # divergence is bounded by one ULP of the seconds-scale double the oracle carried, so
            # it is STRUCTURALLY unable to resolve into a different order.
            #
            # The raw-nanosecond delta is reported anyway with that bound asserted: a change that
            # made the two mtimes differ by MORE than an ULP would be a real reordering risk, and
            # this is the number that would show it.
            mtime_note = ""
            if wanted is None:
                a, b = work / "cache-oracle", work / "cache-go"
                shutil.rmtree(a, ignore_errors=True)
                shutil.rmtree(b, ignore_errors=True)
                restore_store(pristine, store)
                run_client([sys.executable, str(ROOT / "cairn"), "--cache", str(a), "sync"],
                           work, base_env)
                run_client([go_binary, "--cache", str(b), "sync"], work, base_env)

                def tree(root: Path) -> dict[str, int]:
                    return {
                        str(f.relative_to(root)): f.stat().st_mtime_ns
                        for f in sorted(root.rglob("*"))
                        if f.is_file() and f.name != W_SYNC_STAMP
                    }

                def as_double(ns: int) -> float:
                    """`report.pyMtime` / CPython's `st_mtime`, rebuilt from nanoseconds."""
                    return float(ns // 10**9) + 1e-9 * float(ns % 10**9)

                oracle_stat, go_stat = tree(a), tree(b)
                #: One ULP at the current epoch magnitude, measured rather than derived.
                ULP_NS = 238
                if not oracle_stat:
                    failures.append("cache-mtime-parity")
                    print("FAIL cache-mtime-parity — the ORACLE's sync produced no entry files, "
                          "so this comparison would agree with anything")
                elif set(oracle_stat) != set(go_stat):
                    failures.append("cache-mtime-parity")
                    print("FAIL cache-mtime-parity — the two caches hold DIFFERENT FILE SETS: "
                          f"oracle-only={sorted(set(oracle_stat) - set(go_stat))} "
                          f"go-only={sorted(set(go_stat) - set(oracle_stat))}")
                else:
                    differing = [k for k in oracle_stat
                                 if as_double(oracle_stat[k]) != as_double(go_stat[k])]
                    worst = max(abs(oracle_stat[k] - go_stat[k]) for k in oracle_stat)
                    if differing:
                        failures.append("cache-mtime-parity")
                        print("FAIL cache-mtime-parity — the two caches disagree about an mtime "
                              "AS THE READER SEES IT, which is the silent reordering that reads "
                              "as a stale cache:")
                        for key in sorted(differing):
                            print(f"     {key}: oracle={oracle_stat[key]} go={go_stat[key]}")
                    elif worst > ULP_NS:
                        failures.append("cache-mtime-parity")
                        print(f"FAIL cache-mtime-parity — the worst raw-nanosecond divergence is "
                              f"{worst} ns, over the {ULP_NS} ns ULP that is the whole reason the "
                              f"doubles agree. They still agree today; the BOUND no longer holds, "
                              f"so the next store will not be so lucky.")
                    else:
                        print(f"PASS cache-mtime-parity ({len(oracle_stat)} files, identical as "
                              f"doubles; worst raw divergence {worst} ns of a {ULP_NS} ns ULP)")
                        passes += 1
                        mtime_note = (f" mtime-files={len(oracle_stat)} "
                                      f"worst-mtime-delta-ns={worst}")

            if args.self_test:
                # 🔴 THE CONTROL IS READ AS A SET, NOT AS "SOMETHING WENT RED". Each sabotaged
                # row exercises a DIFFERENT comparison — stdout, stderr, exit code — and a
                # harness can lose any one of them while the other two still fail loudly.
                expected = set(SABOTAGE)
                caught = expected & set(failures)
                missed = sorted(expected - caught)
                print(f"SELF-TEST sabotaged={len(expected)} caught={len(caught)}")
                if missed:
                    print("REFUSING TO VOUCH: the differ did NOT report " +
                          ", ".join(missed) + " — that comparison is wired to nothing, and "
                          "every PASS it produces is a fact about the harness.", file=sys.stderr)
                    return 2
                unexpected = sorted(set(failures) - expected)
                if unexpected:
                    print("NOTE: rows failed that were not sabotaged: " + ", ".join(unexpected) +
                          " — the self-test does not vouch for those; run without --self-test.")
                print("SELF-TEST OK: every sabotaged row was reported")
                return 0

            print(f"CONTENT-FLOOR live-banner={saw_live_banner} "
                  f"rendered-digest={saw_rendered_digest}{mtime_note}")
            floor_broken = wanted is None and not (saw_live_banner and saw_rendered_digest)
            if floor_broken:
                print("REFUSING TO VOUCH: no row produced a LIVE banner and a rendered digest, "
                      "so this run measured refusals rather than reports.", file=sys.stderr)

            dead = [n.name for n in norms if not n.fired]
            dead_is_fatal = wanted is None
            for name in dead:
                label = "FAIL" if dead_is_fatal else "NOTE"
                print(f"{label} normalization {name} — declared a licence to differ and matched "
                      f"NOTHING. A licence nobody used is either dead or hiding a real "
                      f"difference behind a pattern that no longer matches.")
            print(f"SUMMARY cases={len(selected)} passes={passes} failures={len(failures)} "
                  f"dead-normalizations={len(dead)}")
            if failures:
                print("failing: " + ", ".join(failures))
            if floor_broken:
                return 2
            return 1 if (failures or (dead and dead_is_fatal)) else 0
        finally:
            if hostile_server is not None:
                hostile_server.shutdown()
                hostile_server.server_close()
            for handle in (proc, second_proc):
                # 🔴 `None` MEANS THE START NEVER HAPPENED, which is exactly the case the
                # bindings above exist for — skip it rather than letting the reaper raise.
                if handle is None:
                    continue
                handle.terminate()
                try:
                    handle.wait(timeout=10)
                except subprocess.TimeoutExpired:
                    handle.kill()
                    handle.wait(timeout=10)
            if log.exists() and os.environ.get("PARITY_SHOW_SERVER_LOG"):
                print("--- oracle log ---")
                print(log.read_text(errors="replace"))
    finally:
        if args.keep:
            print(f"world kept at {work}")
        else:
            shutil.rmtree(work, ignore_errors=True)


if __name__ == "__main__":
    raise SystemExit(main())
