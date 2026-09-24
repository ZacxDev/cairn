"""Each battery's mutant and PACKAGE counts are quoted in prose. Pin them to the code.

🔴 A NUMBER QUOTED IN PROSE AND DERIVABLE FROM CODE IS A CLAIM WITH NO GATE, AND THIS
ONE HAS GONE STALE TWICE. Measured history of `internal/control/README.md`'s headline:
28/27/1 at the commit that introduced the battery, 61/60/1 when it grew to three
packages, 62/61/1 when it grew again — and it then sat at `62/61/1` across a commit
where the tree measured **72/70/2**, because that round added ten mutants and edited
forty-four lines of the same README without re-deriving its own headline. The round
after it corrected the number and left `.github/workflows/ci.yml` carrying the stale one
in TWO places, one of them a step NAME, which is the string a reader sees in the CI UI.

🔴 THE PREVIOUS REMEDY WAS PROSE AND PROSE IS WHAT FAILED. The file carries a
`⚠ RE-DERIVE THESE` instruction; it was present for both staleness events. The same
round that wrote it gave a *different* stale number — the epoch citation — a real pin
(`tokenfile.TestARevocationMakesTheEpochGoDOWN`) and that one has not drifted since.
This is that treatment for the count.

🔴 AND THE **PACKAGE** COUNT IS THE SAME CLAIM ONE LEVEL OVER, ADDED AFTER A ROUND FIXED
THE WRONG COPY. `control_mutants.py`'s header carried a stale `FOUR PACKAGES` above a tuple
of five; the round that noticed deleted that copy and wrote "the tuple below is the only
place the set is stated" — which was measured FALSE on the same tree: `FIVE packages` was
also written at two places in `internal/control/README.md` and one in
`.github/workflows/ci.yml`, none of them pinned, and appending a sixth entry to `PKGS` left
this file green with all three still reading FIVE. The copy that was deleted sat three
lines above `PKGS` and would have been in a `PKGS` edit's own diff; the three that survived
are the ones a `PKGS` editor never opens. So they are pinned here instead, which is the
remedy this file already exists to apply.

⚠ WHAT THIS PINS, AND WHAT IT DOES NOT. It pins the **mutant count** and the **package
count** — the two numbers mechanically derivable from `MUTANTS` and `PKGS`. The
kill/survivor split beside them is the OUTCOME of a run and cannot be asserted without
running the battery, which takes minutes and belongs in the `go` CI job where it already
lives; it is deliberately not quoted here either, because a second copy of it in this file
would be one more of exactly what this file exists to delete. So a wrong split can still
ship; this closes the halves that never had to.

🔴 AND IT COVERS **BOTH** BATTERIES, BECAUSE THE SECOND ONE WAS PINNED BY NOTHING AND THE
FIRST ONE'S HISTORY IS THE WHOLE ARGUMENT. `tests/publish_workflow_mutants.py` declares its
own `MUTANTS`; `.github/workflows/ci.yml` quoted the number in a step NAME — the same
string, in the same file, in the same UI, as the copy this module docstring records going
stale twice — and NOTHING in the tree referenced either the battery module or that step.
The count moved 8 -> 14 -> 19 across three rounds of one pull request. A number that has
moved three times and is checked by nothing is a stale number that has not happened yet.

⚠ THE PUBLISH BATTERY HAS NO PACKAGE COUNT AND NO README, so only the mutant-count half of
this file applies to it. Its sites are a ledger of exact strings (`PUBLISH_ANCHORS`) rather
than a file-wide sweep: `ci.yml` also carries the AUTHZ battery's counts and a historical
`at 62 mutants` timing note, so a sweep for `N mutants` over that file would have to
discriminate BY PHRASING among every such claim it carries — which is the walkable
discriminator this file already declares as a limit, multiplied. ⚠ THE COUNT THAT STOOD
HERE IS DELETED RATHER THAN CORRECTED: it read "three claims", an audit measured four,
and today `grep -coE '[0-9]+ mutants' .github/workflows/ci.yml` answers six. A number
that has been wrong at every reading is not worth a fourth; the instruction to count it
yourself is the part that stays true. A ledger fails on a DELETED anchor too, which is the
other way prose and code come apart.

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
README = REPO_ROOT / "internal" / "control" / "README.md"
CI = REPO_ROOT / ".github" / "workflows" / "ci.yml"


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
def publish_count() -> int:
    return len(_battery(PUBLISH_BATTERY, "cairn_publish_workflow_mutants").MUTANTS)


@pytest.fixture(scope="module")
def packages() -> tuple[str, ...]:
    return tuple(_battery().PKGS)


def test_the_battery_declares_a_plausible_number_of_mutants(count: int) -> None:
    """A POSITIVE CONTROL on this file's own instrument.

    🔴 EVERY ASSERTION BELOW SEARCHES FOR A NUMBER, AND A SEARCH FOR THE WRONG NUMBER
    FAILS THE SAME WAY A STALE DOCUMENT DOES. If the `count` fixture ever returned 0, the
    other tests would go red naming the documents, and a reader would edit the documents. So
    the count is checked for sanity before anything is checked against it.

    ⚠ ONLY AN *EMPTIED* `MUTANTS` PRODUCES THAT 0, AND THE OBVIOUS SECOND CAUSE IS MEASURED
    FALSE. A RENAMED `MUTANTS` raises `AttributeError` while the fixture is being set up, so
    this control never runs — measured both ways: renaming it in `publish_workflow_mutants.py`
    gives 7 passed / 2 errors, and in `control_mutants.py` 5 passed / 4 errors, with `count`
    returning 0 in neither. Naming a cause the code cannot reach reads as coverage of it.
    """
    assert count > 1, (
        f"the battery declares {count} mutant(s) — this file's instrument is broken, "
        "and the failures below would blame the documents for it"
    )


# The README states the count in the PRESENT tense at these anchors, and each must
# carry the current number. Declared as a ledger rather than matched loosely, so
# DELETING an anchor fails too — a silently-dropped claim is the other way prose and
# code come apart.
PRESENT_TENSE_ANCHORS = (
    "python3 tests/control_mutants.py          # {n} mutants",
    "Measured on this tree: {n} mutants",
    "The battery is {n} mutants",
)

# 🔴 A HISTORICAL MENTION IS NOT A STALE ONE, AND THE FIRST DRAFT OF THIS FILE COULD NOT
# TELL THEM APART. It matched every `N mutants` in the file and went RED on correct
# prose: the README's timing comparison says "2m46s **at 62 mutants** … against 2m01s for
# the same battery **at 61 mutants**", which is a measurement of two OLDER batteries on
# one host and is exactly as true as the day it was written. A guard that reds on a
# sentence nobody should change trains its reader to edit the sentence.
#
# ⚠ THE DISCRIMINATOR IS A PHRASING, AND A PHRASING IS WALKABLE — say so rather than
# imply otherwise. `at N mutants` reads as historical and is exempt; a NEW present-tense
# claim written as "at 99 mutants" would evade this. That is accepted: the anchors above
# are the sites that have actually gone stale, the exemption is the form the file already
# uses for history, and pinning whole normalised paragraphs would red on every reword.
#
# ⚠ `\s+`, NOT A LITERAL SPACE, AND THE FIRST DRAFT USED A SPACE AND WAS WRONG. The
# README wraps this very phrase across a line break — `…the same battery at\n61 mutants
# over three…` — so a literal space misses the one historical mention in the file and
# reds on it. A prose guard that cannot see a line wrap is a prose guard that fires on
# correctly-formatted prose.
HISTORICAL = re.compile(r"\bat\s+\d+\s+mutants\b")


def test_the_README_present_tense_claims_match_the_battery(count: int) -> None:
    text = README.read_text(encoding="utf-8")

    missing = [a.format(n=count) for a in PRESENT_TENSE_ANCHORS if a.format(n=count) not in text]
    assert not missing, (
        f"{README.relative_to(REPO_ROOT)} does not carry these present-tense claims at "
        f"{count} mutants:\n  " + "\n  ".join(repr(m) for m in missing) + "\n"
        "Either the count moved and the prose did not, or an anchor was reworded/deleted. "
        "Re-derive from `python3 tests/control_mutants.py` rather than editing the number "
        "to match — the kill/survivor split beside it is NOT pinned by anything, and it is "
        "the half that has been wrong before."
    )

    # And nothing ELSE in the file may state a different count in the present tense.
    stale = sorted(
        {
            m.group(1)
            for m in re.finditer(r"(\d+) mutants", text)
            if int(m.group(1)) != count
            # A window wide enough to hold `at` plus a line wrap plus the number.
            and not HISTORICAL.search(text[max(0, m.start() - 8) : m.end()])
        }
    )
    assert not stale, (
        f"{README.relative_to(REPO_ROOT)} states {stale} mutants outside a historical "
        f"`at N mutants` phrasing; the battery declares {count}."
    )


def test_the_CI_step_matches_the_battery(count: int) -> None:
    """🔴 THE STEP *NAME* IS IN SCOPE, NOT ONLY THE COMMENT.

    The name is what appears in the GitHub Actions UI, so a stale one is the most-read
    copy of the number. It carried `62` for a whole round while the comment above it had
    already been corrected.
    """
    text = CI.read_text(encoding="utf-8")
    step = [ln for ln in text.splitlines() if "prove every authz guard can go RED" in ln]
    assert len(step) == 1, f"expected exactly one battery step in {CI}, found {len(step)}"
    found = re.search(r"\((\d+) mutants", step[0])
    assert found, f"the battery step name quotes no count: {step[0].strip()}"
    assert int(found.group(1)) == count, (
        f"the CI step NAME says {found.group(1)} mutants; the battery declares {count}. "
        "That string is what a reader sees in the Actions UI."
    )


# ── The PUBLISH-WORKFLOW battery ─────────────────────────────────────────────────────
#
# The sites that state, in the PRESENT tense, how many mutants
# `tests/publish_workflow_mutants.py` declares. A ledger of exact strings, so DELETING an
# anchor fails as loudly as a stale one — which is how the authz battery's count came
# apart the first time. The step NAME is first because it is the copy a reader sees in
# the Actions UI, and the copy that carried a stale number for a whole round.
PUBLISH_ANCHORS = (
    "- name: prove every publish-workflow guard can go RED ({n} mutants)",
    "# prose. {n} mutants, each required to be killed by the test that NAMES its",
)


def test_the_publish_battery_declares_a_plausible_number_of_mutants(publish_count: int) -> None:
    """A POSITIVE CONTROL on this file's fourth instrument, in the shape of the first.

    Every assertion below searches for a number. A `publish_count` of 0 would send a
    reader to edit correct prose in `ci.yml` rather than to fix the instrument.

    ⚠ AND THE ONE WAY THAT CAN HAPPEN IS NARROWER THAN THIS DOCSTRING USED TO SAY. It
    named "a renamed `MUTANTS`" as a cause, and that is MEASURED FALSE: a rename raises
    `AttributeError` at collection (2 errors, 7 passed), so this control never runs and
    nobody is sent anywhere. Only an EMPTIED `MUTANTS` — the tuple still there, the rows
    gone — yields the 0 this control exists to catch. A docstring naming a failure mode
    the code cannot reach reads as coverage of it.
    """
    assert publish_count > 1, (
        f"the publish battery declares {publish_count} mutant(s) — this file's instrument "
        "is broken, and the failure below would blame `ci.yml` for it"
    )


def test_the_publish_battery_CI_anchors_match_its_own_count(publish_count: int) -> None:
    """🔴 THE COUNT MOVED 8 -> 14 -> 19 ACROSS THREE ROUNDS AND NOTHING READ IT.

    Measured on the tree this test was added to: `git grep publish_workflow_mutants`
    matched the battery itself and one `run:` line in `ci.yml`; nothing referenced the
    step NAME, and nothing derived the number in it from `MUTANTS`. That is the exact
    state `internal/control/README.md` was in when its headline went stale twice — the
    second time in a step name, in this same file.
    """
    text = CI.read_text(encoding="utf-8")
    missing = [a.format(n=publish_count) for a in PUBLISH_ANCHORS if a.format(n=publish_count) not in text]
    assert not missing, (
        f"{CI.relative_to(REPO_ROOT)} does not carry these present-tense claims at "
        f"{publish_count} mutants:\n  " + "\n  ".join(repr(m) for m in missing) + "\n"
        "Either the count moved and the prose did not, or an anchor was reworded/deleted. "
        "Re-derive it from `len(MUTANTS)` in `tests/publish_workflow_mutants.py` rather "
        "than editing the number to match — and note that the kill/survivor split is NOT "
        "pinned by anything, exactly as for the authz battery above."
    )


# ── The ROUTING-and-ANCHOR battery ───────────────────────────────────────────────────
#
# 🔴 THE THIRD BATTERY, ADDED IN THE SAME STATE THE SECOND ONE WAS FOUND IN: a count in a CI
# step NAME, a count in the battery's own header, and NOTHING in the tree reading either.
# Measured on the tree this arm was added to: `git grep -n routing_mutants` matched the battery
# itself, one `run:` line and one comment in `ci.yml`, and prose — `routing_mutants` appeared
# NOWHERE in this file. That is bit-for-bit the state the module docstring above records for
# the publish battery, whose count moved 8 -> 14 -> 19 while nothing read it.
#
# ⚠ THIS BATTERY HAS NO `PKGS` AND NO README, so — like the publish one — only the
# mutant-count half of this file applies to it. Its sites are a ledger of exact strings for
# the same reason: `ci.yml` carries SIX `N mutants` claims across three batteries, so a
# file-wide sweep there would have to discriminate among them by phrasing, which is the
# walkable discriminator this file already declares as a limit.
ROUTING_BATTERY = REPO_ROOT / "tests" / "routing_mutants.py"

ROUTING_ANCHORS = (
    (CI, "- name: prove every routing and ANCHOR guard can go RED ({n} mutants)"),
    (ROUTING_BATTERY, "The battery is {n} mutants"),
)

# The battery's own wiring claim, and the job it names. Pinned as a ledger entry so that
# REWORDING or DELETING it fails too: the sentence is only true while the step exists, and the
# battery's header says so in as many words.
ROUTING_WIRING_CLAIM = "the `go` job now runs this file"
ROUTING_JOB = "go"
ROUTING_RUN = "python3 tests/routing_mutants.py"


@pytest.fixture(scope="module")
def routing_count() -> int:
    return len(_battery(ROUTING_BATTERY, "cairn_routing_mutants").MUTANTS)


def test_the_routing_battery_declares_a_plausible_number_of_mutants(routing_count: int) -> None:
    """A POSITIVE CONTROL on this file's fifth instrument, in the shape of the first two.

    Every assertion below searches for a number. A `routing_count` of 0 would send a reader
    to edit correct prose in `ci.yml` and in the battery's own header rather than to fix the
    instrument. Only an EMPTIED `MUTANTS` produces it — a rename raises `AttributeError`
    while the fixture is being set up, so this control never runs at all, which is the
    measurement the two controls above already record.
    """
    assert routing_count > 1, (
        f"the routing battery declares {routing_count} mutant(s) — this file's instrument "
        "is broken, and the failures below would blame the documents for it"
    )


def test_the_routing_battery_anchors_match_its_own_count(routing_count: int) -> None:
    """🔴 A COUNT IN A CI STEP NAME AND IN A DOCSTRING, READ BY NOTHING — TWICE OVER NOW.

    The step NAME is first because it is the copy a reader sees in the Actions UI, and it is
    the copy that carried a stale number for a whole round in the authz battery's history.
    """
    missing = []
    for path, anchor in ROUTING_ANCHORS:
        wanted = anchor.format(n=routing_count)
        if wanted not in path.read_text(encoding="utf-8"):
            missing.append(f"{path.relative_to(REPO_ROOT)}: {wanted!r}")
    assert not missing, (
        f"these present-tense claims do not read as {routing_count} mutants:\n  "
        + "\n  ".join(missing)
        + "\n"
        "Either the count moved and the prose did not, or an anchor was reworded/deleted. "
        "Re-derive it from `len(MUTANTS)` in `tests/routing_mutants.py` rather than editing "
        "the number to match — and note that the kill/survivor split is NOT pinned by "
        "anything, exactly as for the two batteries above."
    )

    # And nothing ELSE in the battery's own source may state a different count in the present
    # tense. Scoped to that file, NOT to `ci.yml`: the workflow carries every battery's count.
    text = ROUTING_BATTERY.read_text(encoding="utf-8")
    stale = sorted(
        {
            m.group(1)
            for m in re.finditer(r"(\d+) mutants", text)
            if int(m.group(1)) != routing_count
            # A window wide enough to hold `at` plus a line wrap plus the number.
            and not HISTORICAL.search(text[max(0, m.start() - 8) : m.end()])
        }
    )
    assert not stale, (
        f"{ROUTING_BATTERY.relative_to(REPO_ROOT)} states {stale} mutants outside a "
        f"historical `at N mutants` phrasing; the battery declares {routing_count}. If the "
        "sentence is a correct measurement of an OLDER battery, write it as `… at <N> "
        "mutants …`, which is the form the exemption recognises."
    )


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


def test_the_routing_battery_is_RUN_BY_THE_JOB_ITS_HEADER_NAMES(routing_count: int) -> None:
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
        f"{CI.relative_to(REPO_ROOT)}) carries no `{ROUTING_RUN}`. The battery declares "
        f"{routing_count} mutants that nothing would then run — which is the exact state its "
        "own header says it was fixed out of."
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
# ⚠ THE `{n}` IN THE CI ANCHOR IS THE MUTANT COUNT, DELIBERATELY. That line states both
# numbers in one breath, and pinning the whole phrase is what stops a round correcting one
# of them and walking past the other — which is the exact history the module docstring
# above records for the step NAME.
PRESENT_TENSE_PACKAGE_ANCHORS = (
    (README, "python3 tests/control_mutants.py          # {n} mutants, over {W} packages"),
    (README, "IT RUNS OVER {W} PACKAGES NOW"),
    (README, "`cmd/cairn-server` IS THE {O}"),
    (CI, "{n} mutants over {W} packages"),
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


def test_the_present_tense_package_claims_match_the_battery(
    packages: tuple[str, ...], count: int
) -> None:
    n = len(packages)
    word = _spell(NUMBER_WORDS, n, "a cardinal")
    # 🔴 TWO DIFFERENT QUANTITIES, AND CONFLATING THEM IS WHAT THIS LINE EXISTS TO STOP.
    # The cardinal is `len(PKGS)`; the ordinal is where ONE package sits in it.
    ordinal = _spell(ORDINAL_WORDS, _ordinal_position(packages), "an ordinal")

    missing = []
    for path, anchor in PRESENT_TENSE_PACKAGE_ANCHORS:
        wanted = anchor.format(n=count, W=word.upper(), O=ordinal.upper())
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


def _flatten(text: str) -> str:
    """The file with comment markers and line wrapping normalised away.

    Both documents wrap the enumeration and one of them is a YAML comment, so a literal
    substring search would be a test about where the lines happen to break.
    """
    return re.sub(r"\s+", " ", re.sub(r"(?m)^\s*#+\s?", " ", text))


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
