#!/usr/bin/env python3
"""Refuse private content in this PUBLIC repository.

🔴 THIS FILE IS THE REASON THE REPOSITORY CAN BE PUBLIC AT ALL. Everything here
was extracted from a private monorepo whose comments narrated real incidents on
named hosts. The extraction was sanitised by hand; this gate is what stops the
next commit undoing that.

🔴 A CLEAN RUN IS NOT EVIDENCE UNTIL BOTH CONTROLS HAVE BEEN WATCHED TO WORK.
A scanner wired to nothing reports zero exactly like a clean tree does, so this
module ships its own controls and `--self-test` runs them:

  * NEGATIVE CONTROL — a realistic private string MUST be refused. Realistic,
    not a textbook fixture: a scanner that only recognises its own canonical
    examples passes a real leak. The control strings below are shaped like the
    things that actually appear in this code's history (a hostname, an RFC1918
    address, a dated incident note), not like `example.com`.
  * POSITIVE CONTROL — the matcher must be able to produce a NON-ZERO count at
    all, against text that certainly contains a hit.

Report the pair. `0 findings` on its own is indistinguishable from a broken
matcher, and this codebase's own history is full of that mistake.

Exit codes:
  0  no findings (and, under --self-test, both controls behaved)
  1  findings — private content is present
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
# What counts as private.
#
# 🔴 Each rule is a CLASS, not a spelling. A rule that matched one literal
# hostname would be walked around by the next hostname. Where a rule must name
# specifics (the private-hostname list), it is written as a domain-suffix class
# so a new subdomain is caught without an edit.
# --------------------------------------------------------------------------

RULES: list[tuple[str, str, str]] = [
    (
        "private-hostname",
        r"\b[a-z0-9-]+\.(?:zacx\.dev|homelab\.lan|civitai\.com|civitaic\.com)\b",
        "a hostname from the private infrastructure this code was extracted from",
    ),
    (
        "private-ip",
        # RFC1918 + CGNAT. Deliberately not 127.0.0.1 or 0.0.0.0, which are
        # generic and appear legitimately in bind addresses and tests.
        r"\b(?:10\.\d{1,3}\.\d{1,3}\.\d{1,3}"
        r"|192\.168\.\d{1,3}\.\d{1,3}"
        r"|172\.(?:1[6-9]|2\d|3[01])\.\d{1,3}\.\d{1,3})\b",
        "a private network address from the origin infrastructure",
    ),
    (
        "incident-date",
        # An ISO date in PROSE is how the origin repo recorded incidents. The
        # mechanism survives extraction; the timeline does not.
        #
        # 🔴 SCOPED TO NARRATIVE, NOT TO EVERY DATE, AND THAT IS DELIBERATE. The
        # store's wire format is literally `- <date>: <bullet>`, so fixtures and
        # round-trip tests MUST contain dates. A rule refusing all of them fires
        # on correct test data, and a gate that is unusably noisy gets disabled
        # by the first person it inconveniences — which is worse than no gate.
        # So: a date is refused when it sits in a COMMENT, a DOCSTRING line, or
        # a dated prose bullet — the three shapes an incident note actually
        # takes.
        #
        # ⚠ KNOWN BLIND SPOT, stated rather than hidden: a dated narrative on a
        # continuation line of a docstring, with no marker of its own, is not
        # matched. Reviewers must still read prose. This rule removes the bulk
        # mechanically; it is not a substitute for reading.
        r"(?:#|\"\"\"|''')[^\n]*\b20\d{2}-[01]\d-[0-3]\d\b"
        r"|^\s*[-*]\s*20\d{2}-[01]\d-[0-3]\d\s*:",
        "a dated incident reference in prose — keep the mechanism, drop the particulars",
    ),
    (
        "private-scope",
        # Scope names from the origin store. Bounded so `civitai` inside a URL
        # is caught by private-hostname instead of doubly reported.
        r"\b(?:civitai(?:-[a-z0-9-]+)?|datapacket-talos|homelab-talos|homelab-infra"
        r"|vetr(?:-[a-z0-9]+)?|naida-ai|auditloop|claude-pool|flipt-state"
        r"|storage-resolver|kubeclaw|devrc)\b",
        "a scope name from the private store",
    ),
    (
        "private-hostname-bare",
        r"\b(?:workbench|nebula)\b",
        "a private host or network name",
    ),
    (
        "operator-identity",
        r"\bzacxdev@|zach@[0-9]",
        "an operator identity or a host login",
    ),
]

#: Files the scanner does not read. Deliberately tiny — an allowlist is how a
#: scanner ends up scanning nothing. `.git` is excluded because it is not
#: source; this file is excluded because it necessarily contains the patterns.
SKIP_DIRS = {".git", "__pycache__", ".pytest_cache", "node_modules"}
SKIP_FILES = {"tests/leakscan.py"}

TEXT_SUFFIXES = {
    ".py", ".sh", ".md", ".yml", ".yaml", ".json", ".toml", ".txt", ".cfg",
    ".mjs", ".js", ".ts", ".Dockerfile", "",
}


class Finding:
    __slots__ = ("path", "line", "rule", "text", "why")

    def __init__(self, path: str, line: int, rule: str, text: str, why: str):
        self.path, self.line, self.rule, self.text, self.why = path, line, rule, text, why

    def __str__(self) -> str:
        return f"{self.path}:{self.line}: [{self.rule}] {self.text.strip()[:110]}\n      -> {self.why}"


_COMPILED = [(name, re.compile(pat, re.I), why) for name, pat, why in RULES]


def scan_text(text: str, path: str = "<memory>") -> list[Finding]:
    """Every rule, every line. Returns a list so a caller can count it."""
    out: list[Finding] = []
    for n, line in enumerate(text.splitlines(), start=1):
        for name, rx, why in _COMPILED:
            if rx.search(line):
                out.append(Finding(path, n, name, line, why))
    return out


def tracked_files() -> list[Path]:
    """Files git knows about, plus untracked-but-not-ignored ones.

    🔴 `git ls-files` ALONE IS BLIND to a file that has not been added yet, and
    "I forgot to git add it" is not a reason for a leak to ship. `--others
    --exclude-standard` closes that. A generated or ignored file is genuinely
    out of scope: it is not published.
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
    names = [n for n in r.stdout.split("\0") if n]
    keep: list[Path] = []
    for n in names:
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

#: 🔴 REALISTIC, not textbook. Each is shaped like something that genuinely
#: appeared in the pre-extraction source. If the scanner cannot refuse these it
#: cannot refuse a real leak.
NEGATIVE_CONTROLS = [
    ("private-hostname", 'URL = "https://store.zacx.dev/api/v1/recall"'),
    ("private-ip", "    # measured on 192.168.50.250 during the rollout"),
    ("incident-date", "# 2026-08-29: a printed token forced a credential rotation"),
    ("private-scope", "for scope in ('civitai-gpu-fleet', 'datapacket-talos'):"),
    ("private-hostname-bare", "# the workbench reads this over nebula"),
    ("operator-identity", "Co-Authored-By: someone <zacxdev@gmail.com>"),
]

#: A string that MUST produce a non-zero count, proving the matcher can fire.
POSITIVE_CONTROL = "# see notes from 2026-01-02 about the rollout"

#: 🔴 CONTENT THAT MUST **NOT** BE REFUSED. These pin the rules' NARROWNESS.
#: Without them, someone broadens `incident-date` back to "any ISO date", the
#: gate starts firing on the store's own `- <date>: <bullet>` wire format, and
#: the next person turns the gate off. A false positive here is not cosmetic:
#: it is how a security gate gets disabled.
ALLOWED_CONTROLS = [
    ('bullet = "- 2026-01-02: the pod answered 200"',
     "a date as fixture data — the store's wire format literally contains one"),
    ('assert render(e) == "- 2024-06-01: something"',
     "a date inside an asserted literal"),
    ('    stamp = "seeded=2026-01-02T03:04:05Z"',
     "a timestamp in a snapshot stamp"),
    ('resp = client.get("http://127.0.0.1:8080/api/v1/recall/alpha")',
     "loopback and a synthetic scope name are generic, not private"),
    ('for scope in ("alpha", "beta-svc", "proj-one"):',
     "synthetic fixture scope names"),
]


def self_test() -> int:
    """Both controls. Returns a process exit code."""
    ok = True

    print("== POSITIVE CONTROL: the matcher can produce a non-zero count ==")
    hits = scan_text(POSITIVE_CONTROL, "<positive-control>")
    if hits:
        print(f"  PASS  {len(hits)} finding(s) on a line that certainly contains one")
    else:
        print("  FAIL  0 findings — the matcher is wired to nothing, so every "
              "clean report below is meaningless")
        ok = False

    print("== NEGATIVE CONTROL: each rule refuses a REALISTIC private string ==")
    for expected_rule, sample in NEGATIVE_CONTROLS:
        found = scan_text(sample, "<negative-control>")
        names = {f.rule for f in found}
        if expected_rule in names:
            print(f"  PASS  {expected_rule:24} refused")
        else:
            print(f"  FAIL  {expected_rule:24} NOT refused — rule is inert")
            print(f"        sample: {sample}")
            print(f"        matched instead: {sorted(names) or 'nothing'}")
            ok = False

    print("== CONTROL FOR THE CONTROL: clean text must produce ZERO ==")
    clean = "def render(entry: str) -> str:\n    return entry.strip()\n"
    n = len(scan_text(clean, "<clean>"))
    if n == 0:
        print("  PASS  ordinary code produces 0 findings")
    else:
        print(f"  FAIL  ordinary code produced {n} finding(s) — the gate is "
              "unusably noisy and will be disabled by whoever hits it")
        ok = False

    print("== NARROWNESS: legitimate content must NOT be refused ==")
    for sample, why in ALLOWED_CONTROLS:
        found = scan_text(sample, "<allowed>")
        if not found:
            print(f"  PASS  allowed: {why}")
        else:
            print(f"  FAIL  FALSE POSITIVE on {why}")
            print(f"        sample: {sample}")
            for f in found:
                print(f"        matched [{f.rule}]")
            ok = False

    return 0 if ok else 2


def main() -> int:
    # `__doc__` is None under `python -OO`; the gate must still run there.
    ap = argparse.ArgumentParser(
        description=(__doc__ or "refuse private content").splitlines()[0])
    ap.add_argument("--self-test", action="store_true",
                    help="run the controls and exit; proves the gate is an instrument")
    ap.add_argument("--quiet", action="store_true")
    args = ap.parse_args()

    if args.self_test:
        return self_test()

    # 🔴 The controls run on EVERY invocation, not only under --self-test. A
    # scan whose matcher is broken must not be able to report a reassuring 0.
    rc = self_test()
    if rc != 0:
        print("\nleakscan: COULD NOT VOUCH — a control misbehaved. This is exit 2, "
              "not a pass.", file=sys.stderr)
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
            text = f.read_text(encoding="utf-8", errors="replace")
        except OSError as e:
            print(f"leakscan: COULD NOT READ {f}: {e}", file=sys.stderr)
            return 2
        findings.extend(scan_text(text, str(f.relative_to(ROOT))))

    print(f"== UNDER TEST: {len(files)} file(s) scanned ==")
    if findings:
        for f in findings:
            print(f"  {f}")
        print(f"\nleakscan: {len(findings)} finding(s) across {len(files)} file(s) — REFUSING")
        return 1

    if not args.quiet:
        print(f"  0 findings across {len(files)} file(s)")
        print("\nleakscan: clean — and the controls above are what make that a "
              "measurement rather than a claim")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
