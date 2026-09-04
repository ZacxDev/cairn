#!/usr/bin/env python3
"""Refuse SENSITIVE content in this PUBLIC repository.

🔴 SCOPE: SECURITY, NOT TIDINESS. This gate blocks the things that would cause
harm if published — credentials, reachable hostnames, real private network
addresses, operator identities. It deliberately does NOT police project names,
dates, or machine nicknames. Those are cosmetic, and an earlier version of this
file that chased them produced 480 findings of which 4 mattered.

That ratio is the argument. A gate firing 476 times for nothing is a gate
someone turns off, and then the 4 ship too. Every rule here earns its place by
being something you would not want on the internet.

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
SKIP_FILES = {"tests/leakscan.py"}
TEXT_SUFFIXES = {
    ".py", ".sh", ".md", ".yml", ".yaml", ".json", ".toml", ".txt", ".cfg",
    ".mjs", ".js", ".ts", "",
}


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
    return out


def tracked_files() -> list[Path]:
    """Files git knows about, plus untracked-but-not-ignored ones.

    🔴 `git ls-files` ALONE IS BLIND to a file not yet added, and "I forgot to
    git add it" is not a reason for a leak to ship.
    """
    try:
        r = subprocess.run(
            ["git", "-C", str(ROOT), "ls-files", "--cached", "--others",
             "--exclude-standard", "-z"],
            capture_output=True, text=True, check=True,
        )
    except (subprocess.CalledProcessError, FileNotFoundError) as e:
        print(f"leakscan: COULD NOT RUN — git enumeration failed: {e}", file=sys.stderr)
        raise SystemExit(2)
    keep: list[Path] = []
    for n in (x for x in r.stdout.split("\0") if x):
        if n in SKIP_FILES:
            continue
        p = Path(n)
        if any(part in SKIP_DIRS for part in p.parts):
            continue
        if p.suffix and p.suffix not in TEXT_SUFFIXES:
            continue
        keep.append(ROOT / n)
    return keep


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
    ('resp = client.get("http://127.0.0.1:8080/api/v1/recall/devrc")',
     "loopback, and a project name — project names are NOT policed here"),
    ('# 2026-08-29: the rollout landed and the gate went green',
     "a date — dates are NOT policed here"),
    ('for scope in ("civitai", "homelab-talos", "devrc"):',
     "project names in fixtures — cosmetic, deliberately allowed"),
    ('secret = "s3cr3t-not-in-any-output"',
     "an obviously-fake value in a test asserting a token never leaks"),
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


def main() -> int:
    ap = argparse.ArgumentParser(
        description=(__doc__ or "refuse sensitive content").splitlines()[0])
    ap.add_argument("--self-test", action="store_true")
    ap.add_argument("--quiet", action="store_true")
    args = ap.parse_args()

    if args.self_test:
        return self_test()

    # The controls run on EVERY invocation: a scan whose matcher is broken must
    # not be able to report a reassuring 0.
    if self_test() != 0:
        print("\nleakscan: COULD NOT VOUCH — a control misbehaved. Exit 2, not a "
              "pass.", file=sys.stderr)
        return 2
    print()

    files = tracked_files()
    if not files:
        print("leakscan: COULD NOT RUN — enumerated 0 files. A zero here is a "
              "broken enumeration, not a clean tree.", file=sys.stderr)
        return 2

    findings: list[Finding] = []
    for f in files:
        try:
            findings.extend(
                scan_text(f.read_text(encoding="utf-8", errors="replace"),
                          str(f.relative_to(ROOT))))
        except OSError as e:
            print(f"leakscan: COULD NOT READ {f}: {e}", file=sys.stderr)
            return 2

    print(f"== UNDER TEST: {len(files)} file(s) scanned ==")
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
