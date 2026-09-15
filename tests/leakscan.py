#!/usr/bin/env python3
"""Refuse SENSITIVE content in this PUBLIC repository.

🔴 SCOPE: SECURITY, NOT TIDINESS. This gate blocks the things that would cause
harm if published — credentials, reachable hostnames, real private network
addresses, operator identities, and the specific private identifiers a hand
scrub removed from this tree.

🔴 IT STILL DOES NOT POLICE NAMES OR DATES BY HEURISTIC, AND THAT ARGUMENT IS
UNCHANGED. An earlier version of this file chased *every* project-looking name
and *every* date and produced 480 findings of which 4 mattered. A gate firing
476 times for nothing is a gate someone turns off, and then the 4 ship too.

What changed is the INSTRUMENT, not the appetite. `AGENTS.md` forbids five
things; this file used to be cited as enforcing all five while enforcing three,
so a real project name and a dated incident reference each had **no gate at
all** and the most-read file in the repo said they did. The two rules that
closed that are deliberately not heuristics:

  * `denied-identifier` — a CLOSED SET of identifiers, matched exactly (with
    `-`/`_` prefixes), never a guess at what looks private. A closed set cannot
    produce 476 false positives; it can only fail to know about a name, which
    is a gap you close by adding one.
  * `dated-incident` — the shape `<measurement verb> … <a real date>`, i.e. a
    claim that a specific thing was observed on a specific day. A date in
    FIXTURE DATA is not that shape and is not refused; `AGENTS.md` asks
    fixtures to use an obviously-synthetic year-2000 date, and that asking is
    not what this rule enforces.

Every rule here earns its place by being something you would not want on the
internet, or something `AGENTS.md` says must never be committed.

🔴 A CLEAN RUN IS NOT EVIDENCE UNTIL BOTH CONTROLS HAVE BEEN WATCHED TO WORK.
A scanner wired to nothing reports zero exactly like a clean tree does, so this
module ships its own controls and runs them on EVERY invocation:

  * NEGATIVE — a realistic sensitive string MUST be refused. Realistic, not a
    textbook fixture: a scanner that only recognises `example.com` passes a real
    leak.
  * POSITIVE — the matcher must be able to produce a NON-ZERO count at all.
  * NARROWNESS — legitimate content must NOT be refused, so the gate stays
    usable.

Exit codes:
  0  no findings (and both controls behaved)
  1  findings — sensitive content is present
  2  the gate itself could not run, or a control misbehaved. NOT a pass.
"""

from __future__ import annotations

import argparse
import hashlib
import re
import subprocess
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent

# --------------------------------------------------------------------------
# Documentation addresses that are NOT anybody's infrastructure.
#
# 🔴 AN EXPLICIT LITERAL LIST, NOT A RANGE, SO ANYTHING NEW FAILS CLOSED. These
# are the conventional example addresses this repo already uses to document
# trusted-proxy configuration and Kubernetes pod CIDRs. A private address that
# is not on this list is treated as real topology and refused.
#
# New examples should prefer RFC5737 TEST-NET (192.0.2.0/24, 198.51.100.0/24,
# 203.0.113.0/24), which are reserved for documentation and need no allowlisting.
# --------------------------------------------------------------------------
DOC_ADDRESSES = {
    "10.0.0.0", "10.0.0.1", "10.1.0.0", "10.1.2.3",
    "10.244.0.0", "10.244.0.13", "10.244.0.123",
}

_PRIVATE_IP = re.compile(
    r"\b(?:10\.\d{1,3}\.\d{1,3}\.\d{1,3}"
    r"|192\.168\.\d{1,3}\.\d{1,3}"
    r"|172\.(?:1[6-9]|2\d|3[01])\.\d{1,3}\.\d{1,3})\b"
)

# --------------------------------------------------------------------------
# DENIED IDENTIFIERS — the real project, repository, cluster and host names a
# hand scrub removed from this tree, so they cannot come back.
#
# 🔴 STORED AS DIGESTS, AND THAT IS THE WHOLE DESIGN, NOT A FLOURISH. A
# denylist of private names written out in plaintext, committed to a PUBLIC
# repository, publishes exactly the names it exists to remove — the gate would
# be the leak. SHA-256 of the lowercased identifier lets this file recognise a
# name it does not contain.
#
# 🔴 WHAT THAT COSTS, STATED RATHER THAN HIDDEN: nobody can verify from inside
# this repository that these digests are digests of the RIGHT strings. That is
# not a defect to fix, it is the property being bought — a set you could audit
# from here would be a set you could read from here. So the controls below
# prove the MECHANISM end to end (tokenise → prefix → digest → refuse) using a
# synthetic sentinel that travels the identical code path, and
# `test_leakscan_covers_every_tracked_file.py` pins the SET SIZE so it cannot
# shrink unnoticed. Adding a name is a one-line digest; removing one has to be
# argued for in a diff.
#
# ⚠ MATCHING IS EXACT OVER `-`/`_` PREFIXES, NEVER SUBSTRING — AND THE EXAMPLES
# BELOW USE THE SYNTHETIC SENTINELS, BECAUSE THE FIRST DRAFT OF THIS PARAGRAPH
# ILLUSTRATED THE RULE WITH TWO REAL ENTRIES AND ONE OF THEIR REAL COMPOUNDS.
# Three lines under "a set you could audit from here would be a set you could
# read from here", it handed back two members of the set in clear text, and a
# grep for either over the public repo still hit. The digests protect the set;
# an illustration that spells it undoes them. Sentinels only, from here on.
#
# Each identifier-shaped token on the line is lowercased, `_` is folded to `-`,
# and every leading segment run is hashed: `a-b-c` offers `a`, `a-b`, `a-b-c`.
# Taking BOTH sentinels as denied entries — `canarytoken` (one word) and
# `redacted-canary-scope` (a compound) — and every row below is asserted in
# `test_the_documented_matching_examples_are_TRUE_of_the_code`:
#
#   canarytoken-ci-jx5fq              FIRES  a one-word entry catches every
#                                            compound built on it
#   CANARYTOKEN_TEST_TMPFS            FIRES  an env var folds `_`→`-` and case,
#                                            reaching the same entry
#   clusters/canarytoken/apps/x       FIRES  a path segment is its own token
#   redacted-canary-scope-ci-jx5fq    FIRES  a compound entry catches its own
#                                            extensions
#   canarytokens                      clean  a longer WORD that merely starts
#                                            with the entry: one token, one
#                                            candidate, and it is not the entry
#   redacted-canary-scoped            clean  the entry is a literal SUBSTRING
#                                            here, and a substring is not a
#                                            segment run
#   scoped-canarytoken                clean  ⚠ a PREFIX walk, so an entry in the
#                                            TAIL is never reached. A real
#                                            limit, not a bug — matching at any
#                                            position starts matching English
#
# A denied first segment therefore catches every compound built on it, and a
# denied compound catches only itself and its own extensions.
# --------------------------------------------------------------------------
DENIED_IDENTIFIER_DIGESTS = frozenset({
    "0c3fa6e66c0b74955492996c5aeeb47189804ce63a2a25addc4867ed3af4d1e6",
    "1ac8ca4c444febcb84f6de0da68d3ed09124e9466a7d70e9b4d0137c71d1f10a",
    "338c052d380d56e4abd5da474847046b5d6a1c8ad67cc6ccd5d6b045cde6e8ef",
    "3c3e5b7f6bc16e849f457ed07d5cc060c103a0b7d49caad0fb99a37957f35301",
    "53baeccab19d14a6861ee3efba7122044898a44c562176106d4e445d9e120203",
    "5458dce063933a5e7a9a22bae6bf9bdddf72c56b8fe659546e156fd098394e29",
    "6686bf96fc55109d308a0f411cd3cf0b9c24b95be8f684e50e8abd0d4d3645d1",
    "54cc0e115ea67faee9adb38705ce84643d50fc1d639518c404472834407e52ae",
    "8be0b5445fca6eb16d14c6f4eaff1cf1d1a3362ef660bfb2b66e74bf309b7e61",
    "969572e7d6c32c2a7a68822816e907d2eda9b22a2d22022b225ae7309b0e5810",
    "a44950c2d4123f950705f81e7fa05858596c71b5f127c9b23f326bf9c6e4cddf",
    "c34ff9230f2feefa2d03e5ca45b2c087979cba0f60b07c99e0309c0e44a02c2f",
    "d40ec3acd23d9b72eb1ab4ad590460f24e8ac5e10467e6dad8ac41b4897c2afc",
    "e8410d8cab10b3a9efdbcc1fdf7bfba2962842886d9f4b6ade581cbfb9a16095",
    "e80b757b8ff429370ce09cd1b193e099d1dbb8413c234f7bf2e7e280d5dae424",
    "f55e9ac512ce8de1b70bca77f709d8a7df592e7f25a0b9773646e6b3014c0bcf",
})

#: TWO sentinels whose digests ARE in the set above, so the controls — and the
#: documented examples — can exercise the real path without naming a real
#: deployment. Their SHAPE is what the matcher sees, and that is the part a
#: realistic control has to get right; their CONTENT is synthetic because
#: realistic content here would be the leak this rule prevents.
#:
#: 🔴 THERE ARE TWO BECAUSE THE SET HOLDS TWO KINDS OF ENTRY AND THEY BEHAVE
#: DIFFERENTLY. A ONE-WORD entry fires on every compound built on it; a COMPOUND
#: entry fires only on itself and its extensions. One sentinel could only ever
#: demonstrate one of those, and the paragraph above would have had to ASSERT
#: the other — which is how a comment ends up documenting a rule the code does
#: not have. Both are exercised by a negative control below.
DENY_CANARY = "redacted-canary-scope"
DENY_CANARY_WORD = "canarytoken"

_IDENTIFIER = re.compile(r"[A-Za-z][A-Za-z0-9]*(?:[-_][A-Za-z0-9]+)*")


def denied_identifiers(line: str) -> list[str]:
    """Every denied identifier this line spells, in order of first appearance."""
    out: list[str] = []
    for m in _IDENTIFIER.finditer(line):
        parts = m.group(0).lower().replace("_", "-").split("-")
        for i in range(1, len(parts) + 1):
            candidate = "-".join(parts[:i])
            digest = hashlib.sha256(candidate.encode()).hexdigest()
            if digest in DENIED_IDENTIFIER_DIGESTS and candidate not in out:
                out.append(candidate)
    return out


# --------------------------------------------------------------------------
# DATED INCIDENT REFERENCES — "X was measured on <a real date>".
#
# 🔴 THE RULE IS THE CLAIM SHAPE, NOT THE DATE. `AGENTS.md`: "a dated incident
# reference (`<a real date>: …`). Keep the mechanism, drop the particulars."
# What leaks is the pairing of a real observation with a real day on a real
# deployment; the date alone is not sensitive and a date in fixture data is not
# an incident reference. Policing every date is the 480-finding version.
#
# Two spellings, because prose puts the verb on either side of the stamp, plus
# the continuation shape where a wrapped comment line STARTS with the date and
# the verb sits on the line above — which a line-oriented scanner cannot see
# any other way. Measured on the tree this rule was written for: 12 of the 70
# sites were that third shape, so a two-pattern version would have missed them
# and reported a confident zero.
#
# YEAR 2000 IS ALLOWED, because that is the synthetic date `AGENTS.md` names as
# the remedy. `2006-01-02` is allowed because it is Go's `time` reference
# instant and appears in every layout string in `internal/api`.
# --------------------------------------------------------------------------
_DATE = r"(?:19|20)\d\d-\d\d-\d\d"
SYNTHETIC_DATE_YEAR = "2000"
GO_REFERENCE_DATE = "2006-01-02"

_DATED_CLAIM = re.compile(
    r"(?i)\b(?:measured|re-measured|observed|reproduced|verified|recurred"
    r"|rotated|landed|regressed|reported)\b[^.\n]{0,48}?(" + _DATE + r")"
)
# ⚠ THE TRAILING GUARD IS `(?![\d-])`, NOT `\b`, AND A CONTROL IS WHY. A real
# stamp is often a full RFC 3339 instant — `at 2026-08-23T00:37Z` — and `\b`
# after `23` demands a non-word character, which `T` is not. The `\b` version
# matched the bare-date spelling and silently skipped every timestamped one,
# reading as a working rule; `_DATED_CLAIM` happened to cover the sites that
# existed, which is exactly how the gap stayed invisible.
_DATED_STAMP = re.compile(
    r"(?i)\b(?:on|at|since|until|during|as of)\s+(" + _DATE + r")(?![\d-])"
)
_DATED_CONTINUATION = re.compile(r"^\s*(?:#|//)\s*(" + _DATE + r")(?![\d-])")


def dated_incident_stamps(line: str) -> list[str]:
    """Every real-dated incident stamp on this line."""
    out: list[str] = []
    for rx in (_DATED_CLAIM, _DATED_STAMP, _DATED_CONTINUATION):
        for m in rx.finditer(line):
            d = m.group(1)
            if d.startswith(SYNTHETIC_DATE_YEAR) or d == GO_REFERENCE_DATE:
                continue
            if d not in out:
                out.append(d)
    return out


RULES: list[tuple[str, str, str]] = [
    (
        "credential",
        r"-----BEGIN [A-Z ]*PRIVATE KEY-----"
        r"|-----BEGIN CERTIFICATE-----"
        r"|\bAKIA[0-9A-Z]{16}\b"
        r"|\bgh[pousr]_[A-Za-z0-9]{20,}"
        r"|\bxox[abprs]-[A-Za-z0-9-]{10,}"
        r"|\bAGE-SECRET-KEY-[A-Z0-9]+"
        # 🔴 A SCOPED `(?i:…)`, NOT A BARE `(?i)`. Python refuses a global inline
        # flag that is not at the start of the expression, and this one sits in
        # the middle of an alternation — it raised at import, which is the right
        # failure (a crashing gate is visible; a silently-disabled one is not).
        # 🔴 THE SEPARATOR IS OPTIONAL, and a control is why. `Authorization:
        # Bearer <jwt>` puts a SPACE between the scheme and the token, so a
        # pattern demanding `:` or `=` immediately before the value matched
        # neither `Authorization` (followed by the short word `Bearer`) nor
        # `Bearer` (followed by a space). It read as a working rule and refused
        # nothing.
        r"|(?i:\b(?:authorization|bearer)\s*[:=]?\s*[\"']?[A-Za-z0-9+/_.-]{20,})",
        "a credential. Nothing else on this list is as bad as this one",
    ),
    (
        "reachable-hostname",
        # A host in a domain the origin deployment actually serves. Publishing a
        # reachable endpoint next to this repo's own notes on its weaknesses
        # turns ordinary security documentation into a roadmap for one host.
        #
        # 🔴 DO NOT "FIX" THIS INTO DIGESTS THE WAY `DENIED_IDENTIFIER_DIGESTS`
        # IS. That is the obvious next thought after reading the set above, and
        # it would silently narrow a SECURITY rule into a weaker one. Two
        # reasons, and the first is fatal on its own:
        #
        #   * this rule matches an UNBOUNDED set — every host under these
        #     registrable domains, including subdomains nobody has thought of.
        #     Its own negative control is a host that appears nowhere in this
        #     repo. A digest set can only recognise strings enumerated in
        #     advance, so digesting it would convert "any host in this domain"
        #     into "these exact hosts" and quietly stop catching the new one,
        #     which is the only kind this rule exists to catch.
        #   * the registrable domains are PUBLIC — resolvable in public DNS and
        #     already in certificate-transparency logs — and this repo's own
        #     `LICENSE`, `go.mod` and `README.md` name its owner anyway. The
        #     digest set protects identifiers that exist ONLY inside a private
        #     deployment, where publishing the name IS the disclosure. Naming a
        #     public domain here discloses nothing new; it prevents a
        #     disclosure.
        #
        # So the plaintext is load-bearing: a pattern must contain what it
        # matches, and here what it matches is a domain suffix rather than a
        # name. That asymmetry is the whole difference between the two rules.
        r"\b[a-z0-9-]+\.(?:zacx\.dev|homelab\.lan|civitai\.com|civitaic\.com)\b",
        "a reachable hostname belonging to a real deployment",
    ),
    (
        "operator-identity",
        r"\bzacxdev@|\b[a-z]+@\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}\b",
        "an operator identity or a host login",
    ),
]

SKIP_DIRS = {".git", "__pycache__", ".pytest_cache", "node_modules"}

#: The one file the scan does not read, because it CONTAINS the fixtures — a
#: realistic token, a real-looking hostname, an operator address. The exemption
#: is deliberate; REPORTING it is what keeps it from being a silent hole, so it
#: gets its own bucket in `partition_tracked_files` rather than being dropped.
SKIP_FILES = {"tests/leakscan.py"}

# --------------------------------------------------------------------------
# 🔴 COVERAGE IS DERIVED FROM CONTENT. IT USED TO BE AN ENUMERATION, AND THE
# ENUMERATION IS WHAT KEPT FAILING.
#
# A hand-written `TEXT_SUFFIXES` set decided what to read. The hazard was
# structural rather than accidental: a file type nobody had thought of was
# skipped SILENTLY while the run printed a confident `0 findings across N
# file(s)` — N being files SCANNED, never files present, so nothing in the
# output distinguished "clean" from "did not look". `.nix` was absent on the
# day `flake.nix` arrived — hand-written prose, in the repository whose single
# critical property is that private content stays out. `.dockerignore` was
# absent before that. Each gap was closed by hand, after the fact, which is a
# process that cannot get ahead of the next new file type in a PUBLIC repo.
#
# So "is this text?" is now asked of the BYTES: a file is binary if it holds a
# NUL within its first `BINARY_SNIFF_BYTES`, the same rule git itself uses, so
# this agrees with what every other tool in the tree already believes.
# Everything else is scanned. There is no list left to fall behind.
#
# 🔴 THE DIRECTION OF THE RESIDUAL ERROR IS THE DESIGN. A binary file whose
# first 8000 bytes happen to hold no NUL is SCANNED — harmless, since it is
# decoded with `errors="replace"`, and at worst a false positive a human
# resolves. No text file can be skipped. This gate's job is to fail toward
# reading too much, never toward reading too little.
# --------------------------------------------------------------------------
BINARY_SNIFF_BYTES = 8000


class Finding:
    __slots__ = ("path", "line", "rule", "text", "why")

    def __init__(self, path: str, line: int, rule: str, text: str, why: str):
        self.path, self.line, self.rule, self.text, self.why = path, line, rule, text, why

    def __str__(self) -> str:
        return (f"{self.path}:{self.line}: [{self.rule}] {self.text.strip()[:110]}"
                f"\n      -> {self.why}")


_COMPILED = [(n, re.compile(p), w) for n, p, w in RULES]


def scan_text(text: str, path: str = "<memory>") -> list[Finding]:
    out: list[Finding] = []
    for n, line in enumerate(text.splitlines(), start=1):
        for name, rx, why in _COMPILED:
            if rx.search(line):
                out.append(Finding(path, n, name, line, why))
        # Private addresses are matched separately so the documentation
        # allowlist can be applied per-occurrence rather than per-line: one real
        # address on a line full of examples must still be caught.
        for m in _PRIVATE_IP.finditer(line):
            if m.group(0) not in DOC_ADDRESSES:
                out.append(Finding(
                    path, n, "private-ip", line,
                    f"{m.group(0)} is a real private address — network topology. "
                    f"Use RFC5737 TEST-NET for examples",
                ))
        # The last two rules are per-OCCURRENCE for the same reason the address
        # rule is: one denied name on a line full of synthetic ones must still
        # be caught, and the message has to say WHICH.
        for ident in denied_identifiers(line):
            out.append(Finding(
                path, n, "denied-identifier", line,
                f"{ident!r} is a denied identifier — a real project, repository, "
                f"cluster or host name from the private deployment this repo was "
                f"extracted from. AGENTS.md: fixtures must be synthetic. Replace "
                f"it; do not add it to DENIED_IDENTIFIER_DIGESTS' exceptions, "
                f"because there are none",
            ))
        for stamp in dated_incident_stamps(line):
            out.append(Finding(
                path, n, "dated-incident", line,
                f"{stamp} pins a measurement to a real day on a real deployment "
                f"— AGENTS.md: keep the mechanism, drop the particulars. Delete "
                f"the stamp and keep the claim, or use a year-{SYNTHETIC_DATE_YEAR} "
                f"date if a FIXTURE genuinely needs one",
            ))
    return out


class Skipped:
    """A file the scan did NOT read, carrying the reason it did not.

    🔴 THE REASON IS NOT DECORATION. A skip with no stated cause is exactly the
    silent gap this module used to have; naming it is what lets a reader tell
    "binary, correctly ignored" from "the gate cannot see this".
    """

    __slots__ = ("path", "why")

    def __init__(self, path: str, why: str):
        self.path, self.why = path, why

    def __str__(self) -> str:
        return f"{self.path} — {self.why}"


def enumerate_repo(root: Path) -> list[str]:
    """Files git knows about, plus untracked-but-not-ignored ones.

    🔴 `git ls-files` ALONE IS BLIND to a file not yet added, and "I forgot to
    git add it" is not a reason for a leak to ship. The `-z` framing is part of
    the contract too: without it git QUOTES a non-ASCII path under the default
    `core.quotePath`, and every downstream message then names a filename that
    does not exist.
    """
    try:
        r = subprocess.run(
            ["git", "-C", str(root), "ls-files", "--cached", "--others",
             "--exclude-standard", "-z"],
            capture_output=True, text=True, check=True,
        )
    except (subprocess.CalledProcessError, FileNotFoundError) as e:
        print(f"leakscan: COULD NOT RUN — git enumeration failed: {e}", file=sys.stderr)
        raise SystemExit(2)
    return [x for x in r.stdout.split("\0") if x]


def is_binary(data: bytes) -> bool:
    """git's own rule: a NUL byte within the first `BINARY_SNIFF_BYTES`.

    Borrowed rather than invented so that this gate's idea of "binary" is the
    same one `git diff` and `git grep` already act on in this repository.
    """
    return b"\0" in data[:BINARY_SNIFF_BYTES]


def partition_tracked_files() -> tuple[list[Path], list[Skipped]]:
    """Split every enumerated file into exactly two buckets: scan, or skip.

    🔴 EVERY ENUMERATED FILE LANDS IN EXACTLY ONE OF THEM, AND THAT IS THE
    PROPERTY THAT REPLACED THE SUFFIX LIST. There is no branch that quietly
    drops a file — not even the directory skips, which produce a named entry
    like everything else — so `set(scanned) | set(skipped)` equals the
    enumeration exactly, and a test can assert that without re-implementing a
    single line of the filtering it is checking. `main` prints both counts and
    names every skip, so the reconciliation is visible in the OUTPUT rather
    than merely true in the code.

    A skip is only ever produced for a reason a reader can check: the path is
    under a non-source directory, it is the gate's own fixture file, or the
    BYTES are binary.
    """
    scan: list[Path] = []
    skipped: list[Skipped] = []
    for n in enumerate_repo(ROOT):
        p = Path(n)
        d = next((part for part in p.parts if part in SKIP_DIRS), None)
        if d is not None:
            skipped.append(Skipped(n, f"under {d}/, which is not source"))
            continue
        if n in SKIP_FILES:
            skipped.append(Skipped(n, "the gate's own fixtures, exempt by name"))
            continue
        try:
            with open(ROOT / n, "rb") as fh:
                head = fh.read(BINARY_SNIFF_BYTES)
        except OSError as e:
            # 🔴 UNREADABLE IS NOT CLEAN. A file the gate cannot open is the
            # one case where neither bucket is honest, so it stops the run
            # rather than being counted as skipped.
            print(f"leakscan: COULD NOT READ {n}: {e}", file=sys.stderr)
            raise SystemExit(2)
        if is_binary(head):
            skipped.append(Skipped(
                n, f"binary: a NUL byte within the first {BINARY_SNIFF_BYTES} bytes"))
            continue
        scan.append(ROOT / n)
    return scan, skipped


def tracked_files() -> list[Path]:
    """The files the scan reads. Kept as its own name because that is the
    question most callers are asking; the skip half is available beside it."""
    return partition_tracked_files()[0]


# --------------------------------------------------------------------------
# Controls
# --------------------------------------------------------------------------

NEGATIVE_CONTROLS = [
    ("credential", 'GITHUB_TOKEN = "ghp_A1b2C3d4E5f6G7h8I9j0K1l2M3n4O5p6Q7r8"'),
    ("credential", "-----BEGIN OPENSSH PRIVATE KEY-----"),
    ("credential", 'Authorization: Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9'),
    ("reachable-hostname", 'URL = "https://store.example-real.zacx.dev/api/v1/recall"'),
    ("private-ip", "    # the gateway listens on 192.168.50.94"),
    ("private-ip", "    NEBULA_GW = '10.42.0.10'"),
    ("operator-identity", "Co-Authored-By: someone <zacxdev@gmail.com>"),
    # 🔴 THE DENIED-IDENTIFIER CONTROL USES THE SENTINEL, NOT A REAL NAME, and
    # the reason is in `DENY_CANARY`'s own note: a realistic-CONTENT control
    # here would republish what the rule removes. It is realistic in the only
    # dimension the matcher reads — a lowercase hyphenated compound reached
    # through `_IDENTIFIER` — and it is embedded in a realistic SITE, a
    # fixture-scope tuple, which is where the real ones lived.
    ("denied-identifier",
     f'for scope in ("alpha-notes", "{DENY_CANARY}", "wide-reader"):'),
    # …and as a path segment and a compound EXTENSION, because the prefix walk
    # is the part most likely to be broken by a "tidy-up".
    ("denied-identifier", f"    store = tmp_path / \"{DENY_CANARY}-ci-jx5fq\""),
    ("denied-identifier", f"# manifests live at clusters/{DENY_CANARY}/apps/store/"),
    # 🔴 THE ONE-WORD SENTINEL GETS ITS OWN CONTROLS. Adding a digest nothing
    # exercises is a declaration, not coverage: the set would grow by one and no
    # test would notice if the entry were wrong. These are the two shapes a
    # one-word entry has to catch that a compound entry cannot demonstrate.
    ("denied-identifier", f"    for scope in (\"{DENY_CANARY_WORD}-ci-jx5fq\", \"alpha-notes\"):"),
    ("denied-identifier", f"        monkeypatch.setenv(\"{DENY_CANARY_WORD.upper()}_TEST_TMPFS\", str(tmp_path))"),
    # The dated-incident controls ARE realistic content — a date is not a
    # secret, and these are the exact three shapes removed from this tree.
    ("dated-incident",
     "# a read-through CACHE that may legitimately lag: MEASURED 2026-09-01, "
     "scope at 26 entries locally and 29 on the pod"),
    ("dated-incident",
     "    the public endpoint answered 200 on 2026-08-20, four days after cutover"),
    ("dated-incident", "# 2026-08-29: the rollout landed and the gate went green"),
]

POSITIVE_CONTROL = "trusted = '172.16.4.9'  # a real private address"

#: 🔴 CONTENT THAT MUST **NOT** BE REFUSED — these pin the rules' narrowness.
#: A false positive here is not cosmetic: it is how a security gate gets
#: disabled, and then the real findings ship alongside the noise.
ALLOWED_CONTROLS = [
    ('SUBSYSTEM_STORE_TRUSTED_PROXIES=10.0.0.1,10.1.0.0/24',
     "conventional example addresses in configuration documentation"),
    ('    "10.244.0.0/16",  # a pod CIDR: every pod in the cluster',
     "the standard Kubernetes pod-CIDR example"),
    ('resp = client.get("http://127.0.0.1:8080/api/v1/recall/alpha-notes")',
     "loopback, and a SYNTHETIC scope name"),
    ('for scope in ("alpha-notes", "wide-reader", "kelp-forest"):',
     "synthetic scope names in fixtures — the only kind this repo may carry"),
    ('secret = "s3cr3t-not-in-any-output"',
     "an obviously-fake value in a test asserting a token never leaks"),
    # 🔴 THE NARROWNESS CONTROLS FOR THE TWO NEW RULES, and they are the half
    # that decides whether either rule survives contact with this tree. A
    # denylist that fired on ordinary English, or a date rule that fired on
    # fixture data and Go layout strings, is the 480-finding gate again.
    # ⚠ THE SAMPLE CANNOT SPELL THE DENIED NAME IT IS ABOUT, and the first
    # draft of this line did — it wrote the real four-letter identifier in a
    # trailing comment and the run reported a FALSE POSITIVE on its own
    # narrowness control. That red was the rule refusing a real name in a real
    # file, which is the negative control the digest set cannot carry in
    # plaintext; it is recorded here rather than re-created. The sentinels
    # below say the same thing without spelling an answer.
    (f'    assert normalize_ref("{DENY_CANARY_WORD}s") == "{DENY_CANARY_WORD}s"',
     "a longer WORD that merely STARTS with a one-word denied entry: one token, "
     "one candidate, and it is not the entry"),
    (f'    scope = tmp_path / "{DENY_CANARY}d"   # one letter longer',
     "a token in which a denied COMPOUND is a literal substring — a substring "
     "is not a segment run"),
    (f'    legacy = "scoped-{DENY_CANARY_WORD}"',
     "a denied entry in the TAIL of a compound. The walk is over PREFIXES, so "
     "this is a declared LIMIT of the rule rather than a false negative to fix"),
    ('nix run github:ZacxDev/cairn -- doctor',
     "the repository's OWN public URL: the owner handle is not denied, and "
     "denying it would red every import path in the tree"),
    ('# a home-lab rig, two boxes and a switch',
     "an English compound whose segments are not a denied identifier"),
    ('- 2026-05-02: a synthetic journal bullet [cairn: someone/sess-1]',
     "FIXTURE DATA carrying a non-2000 date. Not an incident reference, and "
     "AGENTS.md's year-2000 preference for fixtures is guidance this gate "
     "deliberately does not enforce"),
    ('auditTimeLayout = "2006-01-02T15:04:05-07:00"',
     "Go's `time` reference instant, which every layout string spells"),
    ('    Measured on the live pod: local 16 scopes / 141 entries, pod 23 / 189',
     "a measurement with the DATE already dropped — the remedy must pass"),
    ('# MEASURED at `465f8a3`: `USER 0:0` in the Dockerfile plus a chown',
     "a measurement pinned to a COMMIT rather than a day, which is the "
     "reproducible form and is what this rule steers toward"),
    # 🔴 THE THREE BELOW REACH THE TWO ALLOWANCE BRANCHES, AND A MUTATION SWEEP
    # IS WHY THEY EXIST. Deleting the year-2000 allowance and deleting the Go
    # reference-date allowance were both scored SURVIVED against the whole
    # suite: every other sample here is either the wrong YEAR or the wrong
    # SHAPE, so neither branch ever executed and its assertion was unreachable.
    # A mutant that cannot be reached is not proof of a guard.
    (f'# MEASURED {SYNTHETIC_DATE_YEAR}-01-05 in the fixture world: the appended '
     f'bullet reads exactly this',
     f"a year-{SYNTHETIC_DATE_YEAR} date in a full CLAIM shape — the synthetic "
     f"date AGENTS.md names as the remedy, so the remedy must not be refused"),
    (f'#   - {SYNTHETIC_DATE_YEAR}-01-05: the bullet this suite appends',
     f"the same allowance reached through the CONTINUATION shape, which is a "
     f"different pattern and needs its own reach"),
    (f'// parsed at {GO_REFERENCE_DATE}T15:04:05-07:00, the reference instant',
     f"Go's reference instant after a preposition, timestamp and all — the one "
     f"shape the `(?![\\d-])` boundary exists for"),
]


def self_test() -> int:
    ok = True

    print("== POSITIVE CONTROL: the matcher can produce a non-zero count ==")
    hits = scan_text(POSITIVE_CONTROL, "<positive-control>")
    if hits:
        print(f"  PASS  {len(hits)} finding(s) on a line that certainly contains one")
    else:
        print("  FAIL  0 findings — the matcher is wired to nothing, so every "
              "clean report below is meaningless")
        ok = False

    print("== NEGATIVE CONTROL: each rule refuses a REALISTIC sensitive string ==")
    for expected, sample in NEGATIVE_CONTROLS:
        names = {f.rule for f in scan_text(sample, "<negative-control>")}
        if expected in names:
            print(f"  PASS  {expected:20} refused")
        else:
            print(f"  FAIL  {expected:20} NOT refused — rule is inert")
            print(f"        sample: {sample[:70]}")
            ok = False

    print("== NARROWNESS: legitimate content must NOT be refused ==")
    for sample, why in ALLOWED_CONTROLS:
        found = scan_text(sample, "<allowed>")
        if not found:
            print(f"  PASS  allowed: {why}")
        else:
            print(f"  FAIL  FALSE POSITIVE on {why}")
            print(f"        sample: {sample[:70]}")
            for f in found:
                print(f"        matched [{f.rule}]")
            ok = False

    return 0 if ok else 2


def main(argv: list[str] | None = None) -> int:
    """🔴 `argv` IS A PARAMETER SO THIS IS DRIVABLE FROM A TEST.

    With `parse_args()` reading `sys.argv` unconditionally, calling `main()`
    under pytest made argparse see the RUNNER's arguments and exit 2 — so the
    only tests that could exist were structural ones about the functions
    underneath, and the actual verdict a user sees was unreachable. That is the
    gap the behavioural tests in `test_leakscan_covers_every_tracked_file.py`
    needed closed; `None` still means `sys.argv` for the real entry point.
    """
    ap = argparse.ArgumentParser(
        description=(__doc__ or "refuse sensitive content").splitlines()[0])
    ap.add_argument("--self-test", action="store_true")
    ap.add_argument("--quiet", action="store_true")
    args = ap.parse_args(argv)

    if args.self_test:
        return self_test()

    # The controls run on EVERY invocation: a scan whose matcher is broken must
    # not be able to report a reassuring 0.
    if self_test() != 0:
        print("\nleakscan: COULD NOT VOUCH — a control misbehaved. Exit 2, not a "
              "pass.", file=sys.stderr)
        return 2
    print()

    files, skipped = partition_tracked_files()
    if not files:
        print("leakscan: COULD NOT RUN — enumerated 0 files. A zero here is a "
              "broken enumeration, not a clean tree.", file=sys.stderr)
        return 2

    # 🔴 NAME EVERY SKIP, ALWAYS — INCLUDING UNDER `--quiet`. An unread file is
    # the one thing a leak gate's output must never leave implicit: a count of
    # files scanned cannot distinguish a clean tree from a partly-read one, and
    # that ambiguity is the whole defect this accounting replaced.
    for s in skipped:
        print(f"  SKIPPED  {s}")

    findings: list[Finding] = []
    for f in files:
        try:
            findings.extend(
                scan_text(f.read_text(encoding="utf-8", errors="replace"),
                          str(f.relative_to(ROOT))))
        except OSError as e:
            print(f"leakscan: COULD NOT READ {f}: {e}", file=sys.stderr)
            return 2

    print(f"== UNDER TEST: {len(files)} file(s) scanned, "
          f"{len(skipped)} skipped ==")
    if findings:
        for f in findings:
            print(f"  {f}")
        print(f"\nleakscan: {len(findings)} finding(s) across {len(files)} file(s) "
              f"— REFUSING")
        return 1

    if not args.quiet:
        print(f"  0 findings across {len(files)} file(s)")
        print("\nleakscan: clean — and the controls above are what make that a "
              "measurement rather than a claim")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
