"""No battery's MUTANT count is hand-stated in prose; its PACKAGE set is pinned to the code.

🔴 A NUMBER QUOTED IN PROSE AND DERIVABLE FROM CODE IS A CLAIM WITH NO GATE, AND THIS
ONE WENT STALE TWICE. Measured history of `internal/control/README.md`'s headline:
28/27/1 at the commit that introduced the battery, 61/60/1 when it grew to three
packages, 62/61/1 when it grew again — and it then sat at `62/61/1` across a commit
where the tree measured **72/70/2**, because that round added ten mutants and edited
forty-four lines of the same README without re-deriving its own headline. The round
after it corrected the number and left `.github/workflows/ci.yml` carrying the stale one
in TWO places, one of them a step NAME, which is the string a reader sees in the CI UI.

🔴 THE FIRST REMEDY WAS A PIN, AND THE PIN TURNED STALENESS INTO MERGE CONFLICTS. This
file used to require every present-tense copy (the README's three anchors, the CI step
NAMES and comments of all three batteries, the routing battery's header) to equal
`len(MUTANTS)`. That stopped the copies drifting, but every PR that added a row then had to
edit the same five-to-seven lines, so ANY two PRs adding rows conflicted on them — and the
merged value was neither side's number. The copies are therefore GONE: each battery prints
its own count when it runs (`SUMMARY mutants=<N> …` for the authz and routing batteries,
`<N> mutants, <K> problem(s)` for the publish one), which is the only copy that cannot
disagree with `MUTANTS`. What this file pins now is that NO present-tense count comes back
at the sites that carried one, so the class cannot regrow one PR at a time.

⚠ WHAT IS STILL ALLOWED: a HISTORICAL count, phrased `at N mutants` — a measurement of an
older battery, as true as the day it was taken (the README's `2m46s at 62 mutants …`
timing comparison is the canonical one). That exemption is a PHRASING, and a phrasing is
walkable: a new present-tense claim written as "at 99 mutants" evades the sweep. Accepted,
because it is the form both documents already use for history and a looser exemption is a
larger hole; the failure message tells a writer which phrasing to reach for.

🔴 THE **PACKAGE** SET IS A DIFFERENT CASE AND STAYS PINNED. `control_mutants.py`'s header
once carried a stale `FOUR PACKAGES` above a tuple of five, and `FIVE packages` was also
written at two places in `internal/control/README.md` and one in `.github/workflows/ci.yml`,
none pinned. The package cardinal, the ordinal of `cmd/cairn-server` and the full ordered
enumeration are pinned against `PKGS` below. Unlike the mutant count, they move only when
`PKGS` moves — new rows add packages through each row's own `pkgs` override instead — so the
pin does not make unrelated PRs conflict, and it is kept.

🔴 AND THE ROUTING BATTERY'S WIRING CLAIM STAYS PINNED: its header argues the anchor mutants
may live there BECAUSE the `go` job runs it, and that argument was false when written. That
is a claim about CI behaviour, not a count, and it is checked against the job's own steps.

The precedent is `tests/test_flake_image_matches_dockerfile.py`: two files stating one
fact, pinned against each other, red when one moves alone.
"""

from __future__ import annotations

import importlib.util
import re
import sys
from pathlib import Path

import pytest

REPO_ROOT = Path(__file__).resolve().parents[1]
BATTERY = REPO_ROOT / "tests" / "control_mutants.py"
PUBLISH_BATTERY = REPO_ROOT / "tests" / "publish_workflow_mutants.py"
ROUTING_BATTERY = REPO_ROOT / "tests" / "routing_mutants.py"
README = REPO_ROOT / "internal" / "control" / "README.md"
CI = REPO_ROOT / ".github" / "workflows" / "ci.yml"
PARITY_README = REPO_ROOT / "tests" / "parity" / "README.md"
DUALRUN_HARNESS = REPO_ROOT / "tests" / "dualrun" / "harness.py"


def _battery(path: Path = BATTERY, name: str = "cairn_control_mutants"):
    """Import a battery module.

    Imported rather than parsed: the module is the authority, and a regex over its
    source would be a second way to count that can disagree with the first — which is
    the shape this whole file exists to close.
    """
    spec = importlib.util.spec_from_file_location(name, path)
    assert spec and spec.loader, f"cannot load {path}"
    module = importlib.util.module_from_spec(spec)
    sys.modules[spec.name] = module
    spec.loader.exec_module(module)
    return module


@pytest.fixture(scope="module")
def count() -> int:
    return len(_battery().MUTANTS)


@pytest.fixture(scope="module")
def packages() -> tuple[str, ...]:
    return tuple(_battery().PKGS)


def _flatten(text: str) -> str:
    """The file with comment markers and line wrapping normalised away.

    Both documents wrap their prose and one of them is a YAML comment, so a literal
    substring search would be a test about where the lines happen to break.
    """
    return re.sub(r"\s+", " ", re.sub(r"(?m)^\s*#+\s?", " ", text))


# ── NO HAND-STATED MUTANT COUNT ──────────────────────────────────────────────────────
#
# The files that carried a present-tense mutant count before this file stopped pinning one.
# A count reappearing ANYWHERE in them reds, not only at the old anchors — a new sentence is
# the way the class would come back.
#
# ⚠ THE FIRST THREE WERE THE SITES THAT CARRIED ONE; THE OTHER THREE ARE WHERE A BATTERY'S SIZE
# IS ALSO DESCRIBED (each battery's own header, the P2 record) and were added after an audit
# found the sweep did not read them.
COUNT_FREE = (README, CI, ROUTING_BATTERY, PUBLISH_BATTERY, PARITY_README, DUALRUN_HARNESS)

# 🔴 CASE-INSENSITIVE, AND `mutant` WITH OR WITHOUT THE `s`, JOINED BY SPACE OR HYPHEN. The first
# spelling, `(\d+)\s+mutants\b`, was measured missing `339 MUTANTS` (a shouted heading),
# `339-mutant` (an adjective), and `339 mutant rows` — three ways the same claim is written.
# ⚠ DIGITS ONLY: a spelled-out count (`seven mutants`) is NOT swept, because these files use
# number words for descriptions that are not a battery's size ("the two mutants written for
# those arms"), and a sweep that red on those would train its reader to reword correct prose.
MUTANT_COUNT = re.compile(r"(?i)\b(\d+)[\s-]+mutants?\b")
# Both read a window ending at the match (24 characters back covers `currently at` + the number).
# Case-insensitive: a sentence may open with it (`**At 339 mutants DECLARED …`).
HISTORICAL = re.compile(r"(?i)\bat\s+\d+[\s-]+mutants?$")
# 🔴 BUT `at N mutants` IS NOT HISTORICAL WHEN A PRESENT-TENSE WORD OPENS IT: "now at 339
# mutants" walked the exemption while stating the current size, which is the exact claim the
# exemption exists to keep out.
PRESENT_AT = re.compile(r"(?i)\b(?:now|currently|is|stands|sits)\s+at\s+\d+[\s-]+mutants?$")


def present_tense_counts(text: str) -> list[str]:
    """Every `<N> mutant(s)` in `text` NOT phrased as history (`at <N> mutants`), in order."""
    flat = _flatten(text)
    hits = []
    for m in MUTANT_COUNT.finditer(flat):
        window = flat[max(0, m.start() - 24) : m.end()]
        if HISTORICAL.search(window) and not PRESENT_AT.search(window):
            continue
        hits.append(m.group(0))
    return hits


def test_the_sweep_is_an_instrument() -> None:
    """🔴 BOTH CONTROLS, ON THE SWEEP ITSELF, BEFORE ANY VERDICT IS READ FROM IT.

    A sweep that matches nothing reports every file clean. So: a planted present-tense count
    MUST be found (positive control) — including one split across a YAML comment wrap, the
    shape `ci.yml` actually has — and the historical form MUST be exempt, including across the
    same wrap (negative control). The numbers are chosen to equal no battery's size.
    """
    assert present_tense_counts("The battery is 9137 mutants now.") == ["9137 mutants"]
    assert present_tense_counts("      # kill. 9137\n      # mutants over EIGHT packages") == [
        "9137 mutants"
    ]
    assert present_tense_counts("timing at 9137 mutants, against 2m01s") == []
    assert present_tense_counts("**At 9137 mutants DECLARED** — a run record") == []
    assert present_tense_counts("the same battery at\n      # 9137 mutants over three") == []
    # A `mutants=<N>` record of a run is a different spelling and never a hit.
    assert present_tense_counts("`mutants=9137 killed=9135`") == []
    # The four shapes the first spelling of `MUTANT_COUNT` was measured missing.
    assert present_tense_counts("## THE BATTERY: 9137 MUTANTS") == ["9137 MUTANTS"]
    assert present_tense_counts("a 9137-mutant battery") == ["9137-mutant"]
    assert present_tense_counts("it holds 9137 mutant rows") == ["9137 mutant"]
    assert present_tense_counts("the battery is now at 9137 mutants") == ["9137 mutants"]
    assert present_tense_counts("currently at\n      # 9137 mutants") == ["9137 mutants"]
    # …and the historical exemption still covers the hyphenated and singular forms.
    assert present_tense_counts("measured at 9137-mutant size") == []


def test_the_sweep_reads_the_real_files() -> None:
    """A POSITIVE CONTROL ON THE INPUT: the sweep must SEE a count in each guarded file.

    Every file but the dual-run harness carries at least one historical `at N mutants`
    measurement today, so a sweep that finds no `N mutants` at all — present or historical — in
    one of them is reading the wrong file or an empty one, and its clean verdict would mean
    nothing. The harness carries none, so for EVERY file the control is also planted: a
    present-tense count appended to the file's real text must be the one hit reported, which
    fails if the file's own content (an unclosed fence, a stray `#`) swallows what follows.
    """
    historical_bearing = [p for p in COUNT_FREE if p != DUALRUN_HARNESS]
    blind = [
        str(p.relative_to(REPO_ROOT))
        for p in historical_bearing
        if not MUTANT_COUNT.search(_flatten(p.read_text(encoding="utf-8")))
    ]
    assert not blind, f"the sweep finds no `N mutants` at all in {blind}; it is reading nothing"
    unplanted = [
        str(p.relative_to(REPO_ROOT))
        for p in COUNT_FREE
        if "9137 mutants" not in present_tense_counts(
            p.read_text(encoding="utf-8") + "\nThe battery holds 9137 mutants.\n"
        )
    ]
    assert not unplanted, f"a count planted in the real text of {unplanted} was not seen"


def test_no_guarded_file_hand_states_a_mutant_count() -> None:
    hits = {
        str(p.relative_to(REPO_ROOT)): present_tense_counts(p.read_text(encoding="utf-8"))
        for p in COUNT_FREE
    }
    hits = {k: v for k, v in hits.items() if v}
    assert not hits, (
        f"these state a mutant count in the present tense: {hits}\n"
        "Every battery prints its own count when it runs; a copy in prose is what every PR "
        "adding a row would have to edit, so any two such PRs conflict on it. Delete the "
        "number. If the sentence is a correct measurement of an OLDER battery, phrase it "
        "`… at <N> mutants …`, which is the form the exemption recognises."
    )


# The sites that claim a battery prints its own count. Pinned because three documents now
# point a reader at that output INSTEAD of a number; if the print went, they would point at
# nothing.
PRINTS_ITS_COUNT = (
    (BATTERY, 'f"SUMMARY mutants={len(selected)} '),
    (ROUTING_BATTERY, 'f"SUMMARY mutants={len(selected)} '),
    (PUBLISH_BATTERY, 'f"\\n{len(MUTANTS)} mutants, {bad} problem(s)"'),
)


def test_each_battery_prints_its_own_count() -> None:
    missing = [
        f"{p.relative_to(REPO_ROOT)}: {s!r}"
        for p, s in PRINTS_ITS_COUNT
        if s not in p.read_text(encoding="utf-8")
    ]
    assert not missing, (
        "the documents send a reader to the battery's own output for its count, and these no "
        "longer print it:\n  " + "\n  ".join(missing)
    )


def test_the_battery_declares_a_plausible_number_of_mutants(count: int) -> None:
    """A POSITIVE CONTROL kept for the package tests below, which share `_battery()`.

    ⚠ ONLY AN *EMPTIED* `MUTANTS` PRODUCES A 0; a RENAMED one raises `AttributeError` while
    the fixture is being set up, so this control never runs (measured on this file's previous
    shape, where it showed as errors rather than this failure).
    """
    assert count > 1, f"the battery declares {count} mutant(s) — this file's instrument is broken"


# ── The ROUTING-and-ANCHOR battery's WIRING claim ───────────────────────────────────
#
# 🔴 ITS COUNT IS NOT PINNED ANY MORE — it is in neither its header nor the step name — but
# the claim its header makes about WHERE it runs is a claim about CI behaviour, and it is the
# one that was false when written.
#
# The battery's own wiring claim, and the job it names. Pinned as a ledger entry so that
# REWORDING or DELETING it fails too: the sentence is only true while the step exists, and the
# battery's header says so in as many words.
ROUTING_WIRING_CLAIM = "the `go` job now runs this file"
ROUTING_JOB = "go"
ROUTING_RUN = "python3 tests/routing_mutants.py"


def _job_spans(text: str) -> dict[str, tuple[int, int]]:
    """Line spans of each top-level job in `ci.yml`, derived rather than transcribed.

    🔴 ANCHORED TO THE `jobs:` KEY, BECAUSE THE INDENT ALONE IS AMBIGUOUS. `on:` carries
    `push:`, `pull_request:` and `merge_group:` at the SAME two-space indent as a job name,
    so a bare indent scan invents three jobs that do not exist and would happily place a
    step inside one of them.
    """
    lines = text.splitlines()
    try:
        start = lines.index("jobs:") + 1
    except ValueError:  # pragma: no cover - the assertion below reports it
        return {}
    keys = [
        (i, ln[2:-1])
        for i, ln in enumerate(lines[start:], start)
        if re.fullmatch(r"  [A-Za-z0-9_-]+:", ln)
    ]
    bounds = [i for i, _ in keys] + [len(lines)]
    return {name: (bounds[k], bounds[k + 1]) for k, (_, name) in enumerate(keys)}


def test_the_routing_battery_is_RUN_BY_THE_JOB_ITS_HEADER_NAMES() -> None:
    """🔴 THE WIRING CLAIM IS A CLAIM LIKE ANY OTHER, AND IT IS THE ONE THAT WENT FALSE.

    `tests/routing_mutants.py` argues that placing the anchor mutants in it is acceptable
    *because* the `go` job runs it — and its own header records that the same argument was
    FALSE of this file when it was written: the battery was invoked by nothing. Its header
    then says "correct this paragraph in the SAME commit" if the step ever comes out. That
    instruction is prose, and prose is what this module exists to stop relying on.

    So: the claim must be present, and it must be true — the `go` job must actually carry a
    `run:` for the battery. Deleting the step now fails here rather than leaving a rationale
    asserting a step that does not exist.
    """
    battery_text = _flatten(ROUTING_BATTERY.read_text(encoding="utf-8"))
    occurrences = battery_text.count(ROUTING_WIRING_CLAIM)
    # 🔴 EXACTLY ONE, AND THE `== 1` HALF IS NOT TIDINESS — IT IS WHAT MAKES THIS PIN WORK.
    # MEASURED while this arm was being written: a paragraph elsewhere in the battery QUOTED
    # the sentence to explain that it was pinned, and the mutation "reword the real sentence"
    # then SURVIVED a fully green run, because the ledger found the quotation instead. A
    # guard satisfied by a copy of the claim is a guard that stops reading the claim.
    assert occurrences == 1, (
        f"{ROUTING_BATTERY.relative_to(REPO_ROOT)} carries the wiring claim "
        f"{ROUTING_WIRING_CLAIM!r} {occurrences} time(s); exactly 1 is required.\n"
        "0 — if the step really was removed, the battery's whole 'it is acceptable to put "
        "the anchor mutants here' argument is void: fix that argument and this arm together, "
        "in the same commit. If it was merely reworded, update `ROUTING_WIRING_CLAIM` so the "
        "pin keeps reading the sentence it names.\n"
        "2+ — a second copy (a quotation, a changelog line) makes this pin satisfiable "
        "without the real sentence being there at all. Paraphrase the copy."
    )

    ci_text = CI.read_text(encoding="utf-8")
    spans = _job_spans(ci_text)
    assert ROUTING_JOB in spans, (
        f"{CI.relative_to(REPO_ROOT)} declares no `{ROUTING_JOB}:` job; "
        f"jobs found: {sorted(spans)}"
    )
    lo, hi = spans[ROUTING_JOB]
    lines = ci_text.splitlines()
    span = lines[lo:hi]

    # A NEGATIVE CONTROL on the span finder, derived rather than hardcoded: a span that
    # swallowed the rest of the file would contain another job's key line, and then the
    # assertion below would be green for a `run:` belonging to a different job entirely.
    intruders = [name for name, (other_lo, _) in spans.items() if name != ROUTING_JOB and lo < other_lo < hi]
    assert not intruders, (
        f"the derived span for `{ROUTING_JOB}` (lines {lo + 1}-{hi}) swallows {intruders}, "
        "so a `run:` found inside it would prove nothing about which job runs it"
    )

    assert any(ROUTING_RUN in ln for ln in span), (
        f"{ROUTING_BATTERY.relative_to(REPO_ROOT)} says {ROUTING_WIRING_CLAIM!r}, but the "
        f"`{ROUTING_JOB}` job (lines {lo + 1}-{hi} of "
        f"{CI.relative_to(REPO_ROOT)}) carries no `{ROUTING_RUN}`. Nothing would then run the "
        "battery's mutants — which is the exact state its own header says it was fixed out of."
    )


# ── The PACKAGE count ────────────────────────────────────────────────────────────────
#
# 🔴 SPELLED AS A WORD IN EVERY SITE, WHICH IS WHY THE MAPPING IS EXPLICIT AND WHY A
# NUMBER OUTSIDE IT IS A FAILURE RATHER THAN A SKIP. A pin that quietly did nothing for a
# count it could not spell would be green for exactly the reason it was written to refuse.
NUMBER_WORDS = {
    1: "one",
    2: "two",
    3: "three",
    4: "four",
    5: "five",
    6: "six",
    7: "seven",
    8: "eight",
    9: "nine",
    10: "ten",
}

ORDINAL_WORDS = {
    1: "first",
    2: "second",
    3: "third",
    4: "fourth",
    5: "fifth",
    6: "sixth",
    7: "seventh",
    8: "eighth",
    9: "ninth",
    10: "tenth",
}


# The package whose ORDINAL one anchor states. Its POSITION in `PKGS`, never the size of
# the set.
CMD_SERVER = "./cmd/cairn-server/"


def _ordinal_position(packages: tuple[str, ...]) -> int:
    """Where `cmd/cairn-server` sits in `PKGS`, one-based.

    🔴 DERIVED FROM THE INDEX, BECAUSE `len(PKGS)` IS A DIFFERENT QUANTITY THAT HAPPENS TO
    AGREE TODAY. The README says "`cmd/cairn-server` IS THE FIFTH", which is a claim about
    POSITION; it is true only because that entry is currently last. Measured on the draft
    that derived it from the count: appending a SIXTH package made the anchor demand
    "`cmd/cairn-server` IS THE SIXTH" while the package was still the fifth, and the failure
    message told the reader to "re-derive from `PKGS` rather than editing it to match the
    prose" — an instruction to write a falsehood into the README. A guard that demands a
    false sentence is worse than no guard: the reader obeys it.
    """
    assert CMD_SERVER in packages, (
        f"{CMD_SERVER} is not in PKGS, so the ordinal anchor is about a package the battery "
        "does not run over. Either the entry was renamed — update this constant and the "
        "prose together — or it was dropped, in which case the anchor must go too."
    )
    return packages.index(CMD_SERVER) + 1


def _spell(table: dict[int, str], n: int, what: str) -> str:
    word = table.get(n)
    assert word, (
        f"this file cannot spell {n} as {what}, so the assertions below would be "
        f"comparing against nothing. Extend the table rather than letting the pin lapse — "
        "a guard that silently narrows is the defect this file exists to close"
    )
    return word


# The sites that state, in the PRESENT tense, how many packages the battery runs over.
# A ledger of exact strings rather than a loose match, so DELETING one fails too.
#
# ⚠ THESE ANCHORS USED TO CARRY THE MUTANT COUNT TOO (`{n} mutants over {W} packages`), and
# it is deleted from them rather than kept: the module docstring says why the mutant count no
# longer appears in prose at all.
PRESENT_TENSE_PACKAGE_ANCHORS = (
    (README, "python3 tests/control_mutants.py          # over {W} packages; prints its own mutant count"),
    (README, "IT RUNS OVER {W} PACKAGES NOW"),
    (README, "`cmd/cairn-server` IS THE {O}"),
    (CI, "The battery runs over {W} packages, in `PKGS` order"),
)

# 🔴 A HISTORICAL MENTION IS NOT A STALE ONE — THE SAME TRAP THE MUTANT-COUNT SWEEP FELL
# INTO, IN THE PACKAGE AXIS. `internal/control/README.md`'s timing comparison says
# "2m46s **at 62 mutants over four packages**, against 2m01s for the same battery at 61
# mutants over three", which is a measurement of an OLDER battery on one host and is
# exactly as true as the day it was written. The discriminator is the `at N mutants`
# that precedes it, and `\s+` rather than a literal space because that paragraph wraps.
#
# ⚠ IT ENDS AT `mutants`, NOT AT `over`, AND THE FIRST DRAFT HERE GOT THAT WRONG AND WENT
# RED ON THAT CORRECT SENTENCE. `over` is the first word of the match this exempts, so it
# is not in the text BEFORE it; a pattern requiring it there matches nothing and exempts
# nothing. That red was this sweep's own positive control arriving by accident — it proves
# the sweep can see a package count it disagrees with.
#
# ⚠ THE DISCRIMINATOR IS A PHRASING, AND A PHRASING IS WALKABLE BOTH WAYS — THE SAME TRADE
# THE MUTANT SWEEP ABOVE DECLARES, STATED HERE BECAUSE THIS SWEEP DID NOT AND THAT WAS THE
# GAP. A NEW present-tense claim written as "at 99 mutants over nine packages" evades this
# sweep; and a correct new HISTORICAL sentence phrased any other way reds on it — measured,
# "The battery ran over four packages before …" fails. Neither is a defect to fix by
# widening the regex: a looser exemption (any past-tense verb, say) is a larger hole than
# the one it closes, and `at N mutants over …` is the form this repository already uses for
# history in both axes. What was missing was saying so, and telling the writer which
# phrasing to reach for — which the failure message now does, so the guard cannot train its
# reader to edit a correct sentence without offering an alternative.
HISTORICAL_PACKAGES = re.compile(r"\bat\s+\d+\s+mutants\s+$")

# ⚠ `over N packages`, NOT `N packages`, AND THAT NARROWNESS IS LOAD-BEARING. The same
# README says "all thirteen `internal/...` test packages green" about a DIFFERENT set in a
# historical measurement; a sweep for any `<word> packages` reds on it, which is a guard
# firing on a sentence nobody should change.
PACKAGE_MENTION = re.compile(r"(?i)\bover\s+(\S+)\s+packages\b")


def test_the_battery_declares_a_plausible_number_of_packages(packages: tuple[str, ...]) -> None:
    """A POSITIVE CONTROL on this file's second instrument.

    🔴 THE SAME SHAPE AS THE MUTANT CONTROL ABOVE, FOR THE SAME REASON. If `PKGS` were
    renamed or half-imported, `packages` would be empty and every assertion below would
    go red naming the DOCUMENTS — sending a reader to edit correct prose. The battery's
    own header states why more than one package is the point, so a count of 1 is already
    a contradiction.
    """
    assert len(packages) > 1, (
        f"the battery declares {len(packages)} package(s) — this file's instrument is "
        "broken, and the failures below would blame the documents for it"
    )
    assert all(p.startswith("./") for p in packages), (
        f"PKGS does not look like a set of package paths: {packages}"
    )


def test_the_present_tense_package_claims_match_the_battery(packages: tuple[str, ...]) -> None:
    n = len(packages)
    word = _spell(NUMBER_WORDS, n, "a cardinal")
    # 🔴 TWO DIFFERENT QUANTITIES, AND CONFLATING THEM IS WHAT THIS LINE EXISTS TO STOP.
    # The cardinal is `len(PKGS)`; the ordinal is where ONE package sits in it.
    ordinal = _spell(ORDINAL_WORDS, _ordinal_position(packages), "an ordinal")

    missing = []
    for path, anchor in PRESENT_TENSE_PACKAGE_ANCHORS:
        wanted = anchor.format(W=word.upper(), O=ordinal.upper())
        if wanted not in path.read_text(encoding="utf-8"):
            missing.append(f"{path.relative_to(REPO_ROOT)}: {wanted!r}")
    assert not missing, (
        f"these present-tense claims do not read as {n} package(s):\n  "
        + "\n  ".join(missing)
        + "\n"
        "Either `PKGS` moved and the prose did not, or an anchor was reworded/deleted. "
        "Re-derive from `tests/control_mutants.py`'s `PKGS`: the CARDINAL from `len(PKGS)`, "
        f"and the ORDINAL for `cmd/cairn-server` from its POSITION in `PKGS` "
        f"(index {packages.index(CMD_SERVER)} + 1 = {_ordinal_position(packages)}) — the two "
        "are different quantities and agree only while that entry is last."
    )


def _enumeration(packages: tuple[str, ...]) -> str:
    """The package set as the documents must spell it, derived rather than transcribed."""
    return ", ".join(f"`{p.removeprefix('./').removesuffix('/')}`" for p in packages)


def test_the_documents_enumerate_the_same_packages_in_the_same_order(
    packages: tuple[str, ...],
) -> None:
    """🔴 A COUNT IS NOT A SET, AND PINNING ONLY THE COUNT LEFT A SWAP INVISIBLE.

    Measured on the tree that had only the count pinned: replacing `./internal/api/` with
    `./internal/report/` in `PKGS` — five entries either way — left all six assertions in
    this file GREEN while `internal/control/README.md` still named `internal/api` by path
    and `.github/workflows/ci.yml` still called it "the server that authorises from all of
    them". The battery's own header called the tuple "the only place the set is ENUMERATED",
    which was false on that same tree; this is the pin that makes the weaker, true sentence
    — the only place it is DECIDED — worth writing.

    ⚠ THE CI COMMENT GAINED THE PATHS TO BE PINNABLE, AND THAT IS PART OF THE FIX RATHER
    THAN A TIDY-UP. Its five members were a prose description apiece; a description is not a
    set, so nothing could compare it to `PKGS` and a swap could not be seen there at all.

    ⚠ IT PINS THE WHOLE NORMALISED STRING, WHICH MEANS A COSMETIC REWORD OF THE
    ENUMERATION FAILS. That is the trade, taken deliberately: a guard on WORDS is walkable
    by rewording, and the enumeration is the one sentence here whose exact content is the
    claim. Reflowing it is free; renaming a package in prose is not.
    """
    wanted = _enumeration(packages)
    # A POSITIVE CONTROL on this file's third instrument, in the shape the two above use:
    # a `packages` that half-imported would make `wanted` a string every document trivially
    # contains, and the assertion below would pass while measuring nothing.
    assert wanted.count("`") == 2 * len(packages) and len(packages) > 1, (
        f"the derived enumeration is not a list of package paths: {wanted!r}"
    )

    missing = [
        str(path.relative_to(REPO_ROOT))
        for path in (README, CI)
        if wanted not in _flatten(path.read_text(encoding="utf-8"))
    ]
    assert not missing, (
        f"these do not enumerate `PKGS` — membership and order — as\n  {wanted}\n"
        + "\n  ".join(missing)
        + "\nA package was added, removed, renamed or reordered in "
        "`tests/control_mutants.py` and the prose did not follow. Re-derive from `PKGS`; "
        "the count alone is not enough, which is why this assertion exists beside it."
    )


def test_no_document_states_a_different_package_count(packages: tuple[str, ...]) -> None:
    n = len(packages)
    word = _spell(NUMBER_WORDS, n, "a cardinal")

    stale = []
    for path in (README, CI):
        text = path.read_text(encoding="utf-8")
        for match in PACKAGE_MENTION.finditer(text):
            if match.group(1).lower() == word:
                continue
            # A window wide enough to hold `at <digits> mutants over ` plus a line wrap.
            if HISTORICAL_PACKAGES.search(text[max(0, match.start() - 40) : match.start()]):
                continue
            stale.append(f"{path.relative_to(REPO_ROOT)}: {match.group(0)!r}")
    assert not stale, (
        f"these state a package count other than {word.upper()} ({n}) outside a "
        f"historical `at N mutants over …` phrasing:\n  "
        + "\n  ".join(stale)
        + "\nIf the number is STALE, re-derive it from `PKGS`. If the sentence is a correct "
        "measurement of an OLDER battery, it is this sweep's phrasing rule you hit and not "
        "an error in the prose: write it as `… at <N> mutants over <word> packages …`, which "
        "is the form the exemption recognises and the form the rest of both documents "
        "already use for history."
    )
