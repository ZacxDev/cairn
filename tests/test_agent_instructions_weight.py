#!/usr/bin/env python3
"""A deterministic byte ceiling on the instructions EVERY session in this repo pays for.

WHY THIS EXISTS
---------------
`CLAUDE.md` is a 267-byte stub whose entire body is `@AGENTS.md`, so the two files are
one cost: an agent loads them both before it has read a line of code or been told what
to do. Measured off the git history with `git show <rev>:AGENTS.md | wc -c`:

    1e7aedf   10,617        1e593a8   25,426
    65b9723   10,800        871e6ff   41,598   <- 3.9x, four merges, one session
    3058af3   16,622

Every one of those commits was individually correct — a real measurement, really made.
Nothing in the process ever asked what the file COST, so nothing ever pushed back. The
same curve, for the same reason, has been measured before on a different always-loaded
rules file (8.3 KB → 41.6 KB over ten weeks, 2.9x of it in three days), and the answer
there is the shape this module copies: a hard ceiling, a working margin below it, and an
eviction playbook printed on failure rather than a request in a comment.

`AGENTS.md` already contains the prose version of this rule — "State the **claim and its
scope**, not the anecdote" — and the curve above is what the prose version achieved. It
also says to prefer a structural fix over a prose one. This is the structural one.

WHAT THE CEILING PROTECTS, AND WHAT IT MUST NEVER DO
----------------------------------------------------
🔴 **This gate must NEVER be satisfied by deleting a claim or narrowing a rule.** The
leak rules, the evidence rules, the `{0, 9}` overlap, "do not switch the deployed image",
which client each claim governs and what each gate CANNOT see are all decision input
before acting, and they stay. What leaves is HISTORY: round-by-round mutation narratives,
defect post-mortems, measured byte counts, superseded numbers, retracted theories. Those
belong in a file that is opened on demand and therefore costs nothing until it is —
`tests/parity/README.md` already holds P2's, and `tests/conformance/README.md` holds
P1's.

⚠ THIS IS AN INVARIANT GUARD, NOT REGRESSION COVERAGE. No defect ever grew `AGENTS.md`;
growth is not a defect, it is a cost. The gate pins the cost so the next quadrupling
cannot happen unseen. It was watched red by appending bytes until each of the two
thresholds fired with its own message, and each threshold's message names only itself so
a red is attributable to one of them.

⚠ AND IT IS A CEILING ON TODAY'S FILE, NOT AN ENDORSEMENT OF IT. At the commit that
added this module the sum is 36,594 B, still 3.4x the pre-P2 10,617 — because the P2
relocation was the scope of that change and the SERVER section (measured 15,864 B, 44%
of `AGENTS.md`) was left alone, even though `tests/conformance/README.md` already carries
much of it (the U+FFFD/surrogate-pair defects, the "byte-diff the two archives" lesson,
the gzip-identity ruling and the 13-of-40 renderer survivors). `_largest_sections()` is
what points the next person at it: the playbook names the real mass, measured at failure
time, rather than a number somebody wrote down.
"""
from __future__ import annotations

import re
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]

#: The two files a session loads before it does anything. 🔴 BOTH, NOT JUST `AGENTS.md`:
#: gating only the big one leaves the cheapest evasion open — move a section into the
#: stub, where it is loaded natively by Claude Code and measured by nothing.
AGENTS_MD = ROOT / "AGENTS.md"
CLAUDE_MD = ROOT / "CLAUDE.md"

#: Hard ceiling on the SUM of the two, in bytes.
#:
#: 🔴 THE NUMBER IS DERIVED, NOT CHOSEN. Measured after the P2 history was relocated to
#: `tests/parity/README.md`: `AGENTS.md` 36,327 B + `CLAUDE.md` 267 B = 36,594 B. The
#: ceiling is that plus 1,106 B of slack — ~3%, the same proportion the precedent named in
#: this module's docstring runs at (1,329 B over a 41,121 B file) — and it is deliberately
#: NOT room bought in advance: it is a bit over one large paragraph's worth, so the next
#: real addition pays ceiling too and this does not become a habit.
#:
#: ⚠ NAME THE BASE WHEN YOU RAISE THIS, and say what eviction was attempted first. A
#: bump with no attempted eviction is how a ceiling becomes decoration.
MAX_BYTES = 37_700

#: Required free margin below the ceiling, so "you are one paragraph from breaking it"
#: arrives as a signal rather than as a surprise on the commit that breaks it.
#:
#: Sized in units of a REAL edit, measured rather than guessed: splitting `AGENTS.md` on
#: blank lines gives 56 blocks of >=200 B, whose mean is 602 B, median 573 B and 90th
#: percentile 919 B. One full large block is therefore ~900 B. A margin below that would
#: fire at the same moment as the ceiling and deliver exactly the surprise it exists to
#: prevent.
MIN_HEADROOM_BYTES = 900

#: Where history goes. 🔴 EACH IS A FILE THAT IS READ ON DEMAND, which is the whole
#: mechanism: a claim in one of these costs nothing until somebody opens it, where the
#: same claim in `AGENTS.md` is paid by every session whether or not it is relevant.
#: Checked against the filesystem rather than trusted, because a playbook that names a
#: destination which does not exist leaves deleting a rule as the only way out.
EVICTION_TARGETS = {
    "tests/parity/README.md": "P2: the two CLIENTS, the parity gate, its mutation "
                              "rounds, its residuals, the P8 retirement ledger",
    "tests/conformance/README.md": "P1: the two SERVERS, the HTTP corpus, the renderer's "
                                   "differential fixture, their mutation rounds",
    "server/README.md": "operating the pod: seeding, rotation, byte-identity, limits",
    "lib/README.md": "the Python reader: multi-instance routing, the cache layout, "
                     "and what binds anyone editing a routing path",
    "README.md": "what the project IS, for a human arriving at the repo",
    "claudedocs/plan-cairn-control-plane.md": "the phase plan and the decision record",
}


def _session_cost() -> int:
    """The bytes an agent loads before it has been told anything."""
    return len(CLAUDE_MD.read_bytes()) + len(AGENTS_MD.read_bytes())


def _largest_sections(limit: int = 4) -> list[tuple[int, str]]:
    """The biggest `##` sections of `AGENTS.md` RIGHT NOW, largest first.

    🔴 MEASURED AT FAILURE TIME, NEVER HARD-CODED. The point of the playbook is to hand
    the next person the actual mass rather than whatever was biggest when this was
    written — a hand-maintained list would send them to a section somebody already
    trimmed while the real weight sat elsewhere.
    """
    lines = AGENTS_MD.read_text(encoding="utf-8").splitlines(keepends=True)
    starts = [i for i, ln in enumerate(lines) if ln.startswith("## ")]
    bounds = list(zip(starts, starts[1:] + [len(lines)]))
    sized = [
        (len("".join(lines[a:b]).encode()), lines[a][3:].strip())
        for a, b in bounds
    ]
    sized.sort(reverse=True)
    return sized[:limit]


def _eviction_playbook() -> str:
    destinations = "\n".join(
        f"      - {path:<44} {what}" for path, what in sorted(EVICTION_TARGETS.items())
    )
    biggest = "\n".join(
        f"      {size:>7,} B   {title[:76]}" for size, title in _largest_sections()
    )
    return f"""
  How to fix — and 🔴 NOT by deleting a claim or narrowing a rule:

    What STAYS in AGENTS.md (this is decision input BEFORE acting):
      - the leak rules, and that `leakscan.py` exits 2 for "could not vouch"
      - the evidence rules (the house style the guards are trusted on)
      - WHICH client or server each claim governs, and what each gate
        STRUCTURALLY CANNOT SEE
      - the `{{0, 9}}` exit-code overlap and why it is not renumbered
      - "do not switch the deployed image", and the pinned-toolchain rules
      - the `lib/`-beside-the-script rule, and its retirement condition

    What MOVES OUT (it is history, and history is read on demand):
      - round-by-round mutation batteries, and survivor labels whose authority
        is the label beside the code anyway
      - defect post-mortems: what was measured wrong, in what order, and how
      - superseded counts, retracted theories, byte counts of built artifacts
      - enumerated ground cases where one representative case carries the point

    Where it goes — each of these is opened on demand and therefore free:
{destinations}

    The biggest sections of AGENTS.md as measured right now:
{biggest}

    Mechanics:
      1. Move the prose into the destination file, under its own `##` heading,
         saying WHY it moved (so the next reader does not move it back).
      2. Leave ONE line in AGENTS.md naming what moved and where — a pointer,
         not a summary; a summary is the same bytes with less information.
      3. Re-run this file. The ceiling and the margin live in
         tests/test_agent_instructions_weight.py; if you raise one, NAME THE
         BASE and say what eviction you tried first.

    Remember what is being protected: this is paid by EVERY session in this
    repository, before any work starts, whether or not it is relevant.
"""


def test_the_two_files_this_gate_measures_exist():
    """Guard the guard. A renamed or moved `AGENTS.md` must not pass by measuring nothing.

    ⚠ Watched red by pointing `AGENTS_MD` at a path that does not exist: this fails with
    its own message while the two thresholds below raise `FileNotFoundError` instead, so
    it is the one that explains what happened.
    """
    for path in (AGENTS_MD, CLAUDE_MD):
        assert path.is_file(), (
            f"{path} not found — the ceiling below would be measuring the wrong thing or "
            f"nothing at all. If the file moved, update this module's constants."
        )


def test_CLAUDE_md_is_still_a_stub_that_IMPORTS_AGENTS_md():
    """🔴 THE GATE'S PREMISE, ASSERTED RATHER THAN ASSUMED.

    "Every session pays this" is only true because `CLAUDE.md` expands `AGENTS.md` at
    session start — Claude Code does not read `AGENTS.md` natively. Two different
    failures live here and the gate is blind to both without this:

      * the import is DELETED. The measured cost drops and this gate goes greener, while
        the rules silently stop loading for every Claude Code session. A ceiling that
        rewards breaking the thing it measures is worse than no ceiling.
      * the stub GROWS into a second rules file. The sum above still catches the bytes,
        but the repo now has two canonical files, which `AGENTS.md` forbids in prose and
        nothing checked.
    """
    text = CLAUDE_MD.read_text(encoding="utf-8")
    assert "@AGENTS.md" in text, (
        "CLAUDE.md no longer imports AGENTS.md. Claude Code does not read AGENTS.md "
        "natively, so the repository's rules now load for NOBODY — and this gate would "
        "read that as an improvement."
    )
    # The stub's own body, not the imported file: everything except the import line and
    # the paragraph explaining why the stub exists.
    assert len(CLAUDE_MD.read_bytes()) <= 1_200, (
        f"CLAUDE.md is {len(CLAUDE_MD.read_bytes()):,} bytes. It is supposed to be a "
        f"one-line import stub; AGENTS.md says to edit AGENTS.md and never this file. "
        f"Two canonical rules files is the failure this bound exists for."
    )


def test_the_eviction_playbook_NAMES_DESTINATIONS_THAT_EXIST():
    """Guard the guard, part 2 — the playbook only renders on FAILURE.

    🔴 A WRONG DESTINATION IS INVISIBLE UNTIL THE DAY SOMEBODY NEEDS IT, and on that day
    it is the difference between relocating a claim and deleting one. So the derivation
    is exercised directly, on every run, in the green case.
    """
    missing = sorted(p for p in EVICTION_TARGETS if not (ROOT / p).is_file())
    assert not missing, (
        f"the eviction playbook names destination(s) that do not exist: {missing}. "
        f"Someone following it would find nowhere to put the prose, which leaves "
        f"deleting a rule as the only way to satisfy the ceiling — the exact outcome "
        f"this module exists to prevent."
    )
    playbook = _eviction_playbook()
    for path in EVICTION_TARGETS:
        assert path in playbook, f"the rendered playbook does not name {path}"
    # And the measured-mass half, which is the part that actually steers the next edit.
    sections = _largest_sections()
    assert sections, "no `## ` sections found in AGENTS.md — the playbook would be blind"
    biggest_size, biggest_title = sections[0]
    assert f"{biggest_size:,} B" in playbook, (
        f"the playbook does not report the largest section's size ({biggest_size:,} B, "
        f"{biggest_title!r}), so it would tell the next person to evict without saying "
        f"from where"
    )


def test_the_session_instructions_stay_under_their_HARD_CEILING():
    size = _session_cost()
    assert size <= MAX_BYTES, (
        f"\n\nAGENTS.md + CLAUDE.md are OVER their hard ceiling.\n"
        f"  AGENTS.md:  {len(AGENTS_MD.read_bytes()):,} bytes\n"
        f"  CLAUDE.md:  {len(CLAUDE_MD.read_bytes()):,} bytes\n"
        f"  per-session cost: {size:,} bytes\n"
        f"  ceiling:          {MAX_BYTES:,} bytes\n"
        f"  OVER BY:          {size - MAX_BYTES:,} bytes\n"
        f"{_eviction_playbook()}"
    )


def test_the_session_instructions_keep_WORKING_HEADROOM():
    """The ceiling alone is not enough — keep a margin to edit into.

    Fires in the `MAX_BYTES - MIN_HEADROOM_BYTES .. MAX_BYTES` band, where the ceiling
    test is still green, so the warning arrives one large paragraph BEFORE the ceiling
    does. Above the ceiling both fire and the ceiling's message carries the overage.
    """
    size = _session_cost()
    headroom = MAX_BYTES - size
    assert headroom >= MIN_HEADROOM_BYTES, (
        f"\n\nAGENTS.md + CLAUDE.md have no working headroom left.\n"
        f"  per-session cost: {size:,} bytes\n"
        f"  ceiling:          {MAX_BYTES:,} bytes\n"
        f"  free:             {headroom:,} bytes  (minimum required: "
        f"{MIN_HEADROOM_BYTES:,}, one large paragraph)\n"
        f"  budget:           {MAX_BYTES - MIN_HEADROOM_BYTES:,} bytes "
        f"(ceiling minus the required margin)\n"
        f"  RECLAIM:          {MIN_HEADROOM_BYTES - headroom:,} bytes\n"
        f"{_eviction_playbook()}"
    )


def test_the_pointers_AGENTS_md_leaves_behind_RESOLVE():
    """🔴 A RELOCATION IS ONLY HONEST IF THE POINTER LANDS SOMEWHERE.

    Trimming `AGENTS.md` replaces prose with lines like "… is in `tests/parity/README.md`".
    A pointer at a file that does not exist is strictly worse than the prose it replaced:
    it tells the reader the evidence exists and then wastes the lookup, and the ceiling
    above would count the trim as a win. Every backticked path `AGENTS.md` names is
    therefore resolved against the tree.

    🔴 THE FILTER IS AN ALLOWLIST OVER THE TREE'S OWN TOP LEVEL, NOT A GUESS AT WHAT
    LOOKS LIKE A PATH. A regex for "something with a slash in backticks" also matches
    `compress/gzip`, `net/http`, `/api/v1/snapshot`, `.git/HEAD` and `nsec/1e9` — Go
    import paths, HTTP routes, a path inside a container and an arithmetic expression,
    none of which are files here. Measured: seven such false positives on the first cut.
    So a candidate counts only when its FIRST segment is a real top-level entry of this
    repository, which is the same property that makes it a pointer at all.
    """
    text = AGENTS_MD.read_text(encoding="utf-8")
    top_level = {p.name for p in ROOT.iterdir() if not p.name.startswith(".")}
    assert "tests" in top_level and "server" in top_level, (
        f"the top-level allowlist derived nothing usable ({sorted(top_level)[:6]}…), so "
        f"every candidate below would be filtered out and this test would pass vacuously"
    )
    candidates = {
        c for c in re.findall(r"`([A-Za-z0-9_./-]+/[A-Za-z0-9_.-]+)`", text)
        if c.split("/")[0] in top_level
    }
    assert candidates, (
        "AGENTS.md names no repo-relative paths at all, which cannot be true — the "
        "extraction is broken and a zero here would mean nothing"
    )
    dangling = sorted(c for c in candidates if not (ROOT / c).exists())
    assert not dangling, (
        f"AGENTS.md names {len(dangling)} path(s) that do not exist in the tree: "
        f"{dangling}\nA pointer that does not resolve is worse than the prose it "
        f"replaced — the reader is told the evidence exists and then cannot find it."
    )
