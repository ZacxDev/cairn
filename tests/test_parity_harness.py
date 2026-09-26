#!/usr/bin/env python3
"""The parity harness DECLARES a lot; this is what makes the declarations load-bearing.

🔴 THE HARNESS ITSELF IS NOT RUN HERE, AND THAT IS DELIBERATE. It starts a pod, builds a Go
binary and drives two clients for about half a minute; folding that into the unit suite would put
a `go` toolchain in the `tests` job's dependencies and make one floor answer two questions. The
harness's own verdict is read in its own CI job, exactly as the conformance corpus's is.

🔴 WHAT *IS* CHECKED HERE IS THE SHAPE OF THE CLAIM, because a gate's coverage is a claim too. A
case list can silently stop covering a verb, a `compare="exit"` row can appear with no stated
reason, and a normalization can be added with no justification — none of which the harness's own
green would notice, since a gate that measures fewer things passes more easily.

⚠ THESE ARE INVARIANT GUARDS, NOT REGRESSION COVERAGE. No bug ever dropped a verb from the case
list; they pin the property so that one cannot pass unseen.
"""
from __future__ import annotations

import re
import sys
from pathlib import Path

import pytest

ROOT = Path(__file__).resolve().parents[1]
HARNESS_DIR = ROOT / "tests" / "parity"
sys.path.insert(0, str(HARNESS_DIR))

import harness  # noqa: E402
import world as parity_world  # noqa: E402

#: The verbs. 🔴 READ FROM THE CLIENT'S OWN PARSER, NOT LISTED HERE — AND NOT COUNTED HERE
#: EITHER, because a count is a hand list one word long. A hand list is blind to
#: a verb added after it was written, which is the whole defect the capability ledger exists to
#: close — and a parity gate that quietly stopped covering a verb would be the same failure one
#: layer down.
def _cli_verbs() -> set[str]:
    from testlib.capability_ledger import cli_verbs_from_parser

    return set(cli_verbs_from_parser())


@pytest.fixture(scope="module")
def cases() -> list:
    # A port number is required to build the unreachable-URL rows; any integer will do, and
    # nothing in this file connects to it.
    return harness.cases(1)


def test_every_CLI_verb_appears_in_at_least_one_case(cases):
    """🔴 THE GATE'S COVERAGE OF THE VERB SET, IN BOTH DIRECTIONS.

    A verb with no case is a verb whose two implementations nothing compares; a case naming a verb
    the CLI does not have is a case that cannot run. Both are failures, and the verb set is
    DISCOVERED from the parser rather than written here.
    """
    verbs = _cli_verbs()
    # ⚠ A ROW WHOSE FIRST ARGUMENT IS A FLAG NAMES NO VERB. `cairn --help` and `cairn -h` are
    # rows about the tool rather than about a subcommand, and counting `--help` as a "verb the CLI
    # does not have" would have made this guard fail for a row that is correct.
    covered = {case.argv[0] for case in cases
               if case.argv and not case.argv[0].startswith("-")}
    missing = sorted(verbs - covered)
    assert not missing, (
        f"{len(missing)} CLI verb(s) have NO parity case: {missing}. A verb the gate does not "
        f"exercise is a verb whose Python and Go implementations nothing compares, and the gate's "
        f"green says nothing about it."
    )
    # ⚠ THE OTHER DIRECTION HAS ONE DELIBERATE MEMBER, AND IT IS EXEMPTED BY ID RATHER THAN BY
    # ITS SPELLING. `usage-unknown-subcommand` exists precisely to name a verb the CLI does NOT
    # have; exempting "any verb the parser rejects" would exempt a real verb that had been renamed,
    # which is the case this direction is for.
    deliberate = {case.argv[0] for case in cases if case.id == "usage-unknown-subcommand"}
    unknown = sorted(covered - verbs - deliberate)
    assert not unknown, (
        f"parity case(s) name verb(s) the CLI does not have: {unknown}. Either the verb was "
        f"renamed and the case was not, or the case is measuring nothing."
    )
    assert deliberate, (
        "no row exercises an UNKNOWN subcommand, so nothing compares how the two clients refuse "
        "one — and the exemption above would then be vouching for an empty set"
    )


def test_every_output_shaping_FLAG_appears_in_at_least_one_case(cases):
    """The flags that change output SHAPE, each exercised at least once.

    🔴 NOT "every flag". `--repo` and `--timeout` do not change the shape of a successful answer,
    and `--if-match` is covered by the row that exercises its ABSENCE deriving one. The set below
    is the one the phase's brief names, which is what makes it a commitment rather than a list of
    whatever happened to be covered.
    """
    wanted = {"--scope", "--ref", "--list", "--limit", "--page", "--all-scopes", "--json",
              "--no-sync", "--repo"}
    used = {arg for case in cases for arg in case.argv if arg.startswith("--")}
    # `--repo` is exercised through `in_repo`, which changes the CWD rather than the argv — so its
    # coverage is that flag's DEFAULT being reached in a real repo. Counted explicitly rather than
    # quietly dropped from the set.
    if any(case.in_repo for case in cases):
        used.add("--repo")
    missing = sorted(wanted - used)
    assert not missing, (
        f"{len(missing)} output-shaping flag(s) have no parity case: {missing}"
    )


def test_every_documented_exit_code_is_asserted_by_some_case(cases):
    """🔴 THE EXIT-CODE MODEL IS A PRINTED CONTRACT, so every code must be REACHED by a row.

    The set is read out of the client's own constants, not restated. A code no row produces is a
    code whose two implementations nothing compares — and the write codes (6/7/8/9) are exactly
    the ones a caller branches on to decide whether a record was made.
    """
    from testlib import cairn_source

    codes = cairn_source.module_constants()
    documented = {name: value for name, value in codes.items() if name.startswith("EXIT_")}
    # The positive control on the discovery: a zero here would make the loop below vacuous.
    assert len(documented) >= 9, f"discovered only {sorted(documented)}"
    # The rows name the code they are about in their `why`, which is prose — so the CODES are
    # instead cross-checked against the harness's own source text, where each appears in a
    # sentence. That is a weaker instrument than running the gate and it is the honest one here:
    # this file does not run the gate.
    text = (HARNESS_DIR / "harness.py").read_text(encoding="utf-8")
    unmentioned = sorted(
        name for name, value in documented.items()
        # 0 is "success" and appears in no sentence as a number; every non-error row asserts it.
        if value != 0 and not re.search(rf"\bexit {value}\b", text)
    )
    assert not unmentioned, (
        f"{len(unmentioned)} documented exit code(s) are named by no parity case: {unmentioned}. "
        f"The exit model is a printed contract; a code the gate never produces is a code the two "
        f"implementations are free to disagree about."
    )


def test_every_exit_only_row_states_WHY_it_is_narrower(cases):
    """🔴 A `compare="exit"` ROW IS A ROW WHOSE TEXT IS ALLOWED TO DIFFER.

    That is a real narrowing of the gate, so each such row has to say something about why — and
    the check is that its `why` is substantive, not that it contains a magic word, because a guard
    on a WORD is walkable by rewording.
    """
    thin = [case.id for case in cases
            if case.compare == harness.COMPARE_EXIT and len(case.why) < 40]
    assert not thin, (
        f"{len(thin)} exit-only row(s) carry a `why` too short to be a justification: {thin}. "
        f"An exit-only row narrows the gate; the reason belongs beside it."
    )


def test_every_normalization_carries_a_justification():
    """A normalization is a LICENCE TO DIFFER, and an unexplained one is a hole with a name."""
    for norm in harness.normalizations():
        assert len(norm.why) > 80, (
            f"normalization {norm.name!r} has no real justification. A licence to differ that "
            f"nobody argued for is how a gate shrinks without anyone deciding to shrink it."
        )
        # The pattern has to be a pattern: a literal string would normalize exactly one run's
        # output and silently stop firing on the next.
        assert any(c in norm.pattern.pattern for c in r"\d\w+*["), (
            f"normalization {norm.name!r} has no metacharacter, so it can only ever match one "
            f"literal run's output"
        )


def test_the_case_ids_are_unique(cases):
    ids = [case.id for case in cases]
    duplicates = sorted({i for i in ids if ids.count(i) > 1})
    assert not duplicates, (
        f"two parity rows share an id: {duplicates} — `--only` would run one of them and the "
        f"failure list would name a row nobody can rerun"
    )


def test_every_sabotage_row_is_a_real_case(cases):
    """🔴 THE NEGATIVE CONTROL CAN ONLY VOUCH FOR ROWS THAT EXIST.

    A sabotage entry naming a deleted case would make `--self-test` refuse forever, which is a
    permanently-red gate; naming a case that is skipped would make it vouch for nothing.
    """
    ids = {case.id for case in cases}
    missing = sorted(set(harness.SABOTAGE) - ids)
    assert not missing, f"the self-test sabotages rows that do not exist: {missing}"
    # …and it must cover all three comparisons, which is the whole reason there is more than one.
    assert len(harness.SABOTAGE) >= 4, (
        "the self-test sabotages fewer than four rows. stdout, stderr and the exit code are three "
        "separate comparisons, an `exit`-only row is structurally blind to the first two, and the "
        "`exit+stdout` mode is a fourth assertion again — so a smaller set would vouch for a "
        "differ that had lost some of them."
    )
    by_compare = {case.id: case.compare for case in cases}
    sabotaged_compares = {by_compare[cid] for cid in harness.SABOTAGE}
    every_mode = {harness.COMPARE_ALL, harness.COMPARE_EXIT,
                  harness.COMPARE_EXIT_AND_STDOUT_NONEMPTY}
    assert sabotaged_compares == every_mode, (
        f"the sabotaged rows cover only {sorted(sabotaged_compares)}, not {sorted(every_mode)}. "
        f"EVERY comparison mode has to be controlled: an `exit`-only row cannot see a text "
        f"difference at all, and `exit+stdout` asserts something neither of the others does."
    )
    # 🔴 AND EVERY SABOTAGE MUST DECLARE HOW IT IS APPLIED. `--help` rows cannot be sabotaged by
    # APPENDING — both clients handle it before anything else — so an append-only mechanism had no
    # control over that mode at all.
    for cid, (how, extra) in harness.SABOTAGE.items():
        assert how in ("append", "replace"), f"{cid}: unknown sabotage mode {how!r}"
        assert extra, f"{cid}: an empty sabotage changes nothing and would vouch for nothing"


def test_the_world_is_synthetic_and_dated_year_2000():
    """The repository is PUBLIC and was extracted from a private one."""
    year_2001_ns = 978307200 * 1_000_000_000
    for _rel, offset, _text in parity_world.ENTRIES:
        ns = parity_world.EPOCH_NS + offset
        assert parity_world.EPOCH_NS <= ns < year_2001_ns
    blob = "\n".join(text for _rel, _off, text in parity_world.ENTRIES) + parity_world.HANDOFF
    assert "2026-" not in blob and "2025-" not in blob
    # 🔴 AND THE TOKEN IS NOT A CREDENTIAL. The server refuses a row under 43 characters, so the
    # value has to be long enough to look like one; it is repeated filler precisely so nobody can
    # mistake it for a token that ever authorised anything.
    assert len(parity_world.TOKEN) >= 43
    assert "synthetic" in parity_world.TOKEN


def test_the_harness_declares_what_it_cannot_see():
    """🔴 A GATE'S BLIND SPOTS ARE PART OF ITS VERDICT.

    The README carries the residual-difference table and the blind-spot list; this asserts both
    sections exist and that the table has rows, because a green gate whose limits are undocumented
    reads as a wider claim than it is.
    """
    readme = (HARNESS_DIR / "README.md").read_text(encoding="utf-8")
    assert "Declared differences" in readme
    assert "structurally cannot see" in readme
    # One row per declared difference, numbered. A table that lost its rows would still contain
    # the heading.
    rows = re.findall(r"^\| \d+ \|", readme, re.MULTILINE)
    assert len(rows) >= 7, (
        f"the declared-difference table has {len(rows)} row(s). Every residual the gate does not "
        f"close has to be written down; SEVEN are measured on this tree — the seventh is the Go "
        f"client's `-verbs`/`-exit-codes` ledger flags, which exit 0 where the oracle exits 2."
    )
    # 🔴 AND THE P8 LEDGER, WHICH IS THE ONE SECTION THAT EXISTS FOR A READER WHO HAS NOT ARRIVED
    # YET. Everything in this repository that exists only while the Python oracle does is listed
    # there; `AGENTS.md` names the retirement condition for the `lib/` rule and for nothing else,
    # so without this section P8 starts by rediscovering the set. A guard on the HEADING only,
    # deliberately: pinning its rows would fail on every honest addition to it.
    assert "P8 retirement ledger" in readme, (
        "the P8 retirement ledger is gone from tests/parity/README.md. It is the only written "
        "record of what `tests/parity/`, `internal/store/pyoserror.go`'s CPython spelling, the "
        "cross-client ledger tests and the 31 narrower rows are FOR — and all of them look like "
        "dead weight to whoever retires the oracle."
    )


def test_the_parity_gate_still_declares_a_NON_TRIVIAL_case_set():
    """🔴 NOT EVERY GUARD ABOVE GOES RED ON AN EMPTY CASE LIST — MEASURED, WHICH IS WHY THIS FLOOR
    IS NOT REDUNDANT WITH THEM.

    The previous version of this test claimed "a zero here would make EVERY assertion above pass
    vacuously". That was wrong in both directions and the control was run rather than reasoned
    about: with `harness.cases()` forced to `[]`, the verb ledger, the flag ledger and the
    sabotage-row check all go RED on their own, while `test_every_exit_only_row_states_WHY_it_is_
    narrower` and `test_the_case_ids_are_unique` pass VACUOUSLY — an empty list has no thin `why`
    and no duplicate id. So the floor covers those two, and nothing else in this file does.

    ⚠ It is also the only floor that runs in the `tests` job. CI's `parity` job refuses below the
    PASS count `.github/workflows/ci.yml` pins — **110 at this head** — but that job needs a Go
    toolchain and a running pod; a developer running `pytest tests` reaches this one and not that
    one. 🔴 NOTHING ASSERTS THAT THE TWO NUMBERS AGREE, which is exactly how this one went stale:
    read `ci.yml`'s `-lt` comparison rather than this sentence.
    🔴 **IT HAS GONE STALE IN BOTH DIRECTIONS, INCLUDING IN A COMMIT THAT MOVED `ci.yml` AND THIS
    FILE TOGETHER.** So: **every number in this file is a MEASUREMENT with a command beside it**,
    and the two commands are
    `nix develop … -c python3 tests/parity/harness.py | grep -c '^PASS '` for the PASS count and
    `python3 -c "import harness; print(len(harness.cases(1)))"` for the case count — not
    `grep -c 'Case('`, which happens to agree today and is a different question.

    The AST half of the old test is deleted as genuinely redundant: `harness` is imported at module
    scope (line 30) and the `cases` fixture calls `harness.cases(1)`, so a syntax error or a
    missing `cases` function already fails all ten tests in this file, more loudly.

    ⚠ INVARIANT GUARD, NOT REGRESSION COVERAGE — no defect ever narrowed the case list.
    """
    declared = len(harness.cases(1))
    # 107 measured on this tree — by `len(harness.cases(1))`, which is what the assertion below
    # compares and is therefore the only measurement that can be right. The floor is the
    # repository's own formula for a collected-count floor — `m - min(50, max(1, m / 20))` for a
    # measured `m`, which `.github/workflows/ci.yml` owns and justifies: close enough that a real
    # narrowing cannot hide under it. The previous floor was 50 against 90, which could not
    # see a 44% narrowing — the same blindness that comment describes one layer up.
    #
    # ⚠ THIS USED TO ADD "loose enough that adding or dropping a handful of rows in a PR does not
    # make it permanently red", AND THE DRIFT GUARD BELOW MADE THAT FALSE IN THE GROWTH
    # DIRECTION — at the measured `m` the formula gives exactly this literal, so adding ONE case
    # reds `pytest tests` until the literal moves. The clause is deleted rather than the guard
    # loosened, and the asymmetry with `ci.yml`'s equivalent is deliberate: a parity CASE is
    # added a few times a year, so an exact guard costs an edit nobody notices, while the
    # collected count there moves on most PRs and an exact guard would red every one. Same
    # formula, different movement rate, different tolerance — stated at both sites.
    # ⚠ AND IT WAS 85 AGAINST 101 ONCE, because the measured `m` moved by eleven rows and the
    # floor did not — a floor left behind by its own formula loosens silently, which is the same
    # failure one size larger. Move BOTH when a row lands, and both when one is DELETED.
    # ⚠ 102 -> 101 WHEN `validate-unreadable-cache-root` WAS REMOVED: `m` moved 108 -> 107 and
    # 101 is the literal the formula prescribes for it (`107 - min(50, max(1, 107/20)) = 101.65
    # -> 101`, re-derived by RUNNING the formula on `len(harness.cases(1))`, not by arithmetic on
    # the previous literal). That row gated the cache-ROOT read; the wraps behind it were removed
    # after the triggering condition was measured never to occur, and the cache-root depth is
    # declared open again in `tests/parity/README.md` row 4. What remains in this family is the
    # four rows that gate a mode-000 ENTRY FILE and a mode-000 SCOPE DIRECTORY on both clients —
    # the conditions an ordinary `chmod` reaches.
    floor = 101
    # ✅ **DECIDED: PINNED TO ITS OWN FORMULA, BECAUSE IT HAS GONE STALE TWICE.**
    # The handoff filed this under "counts quoted in prose that nothing asserts
    # on", closing condition "a decision to pin each or a written line saying why
    # not". The measured history decides it: 50 against 90, then 85 against 101 —
    # both times the floor stayed put while `m` moved, and a floor left behind by
    # its own formula LOOSENS silently. The comment above says "Move BOTH when a
    # row lands"; this is that sentence made mechanical.
    #
    # 🔴 THE ASSERTION BELOW CANNOT DO IT, AND THE REASON IS THE TRAP. Deriving
    # `floor` from `declared` would make `declared >= floor` TRUE BY
    # CONSTRUCTION — a vacuous guard wearing the name of a tripwire. The floor
    # must stay a literal somebody edits; what is checked here is that the
    # literal has not drifted BELOW what the formula prescribes for the current
    # measurement. Adding a row therefore reds this until the floor moves, which
    # is the intended cost and is what the two stale readings above bought.
    # ⚠ REAL DIVISION, THEN FLOORED — AND THE FIRST DRAFT OF THIS LINE USED `//`,
    # WHICH IS A DIFFERENT FORMULA. `ci.yml` writes it as `m - min(50, max(1,
    # m / 20))`; at m=102 that is 96.9 → 96, which is the literal above, while
    # integer division gives 97 and made this guard red on a correct tree. The
    # guard caught its own transcription, which is the only reason the difference
    # was ever visible — nothing else in the repo evaluates that sentence.
    want_floor = int(declared - min(50, max(1, declared / 20)))
    assert floor >= want_floor, (
        f"the case floor is {floor} but the formula prescribes {want_floor} for "
        f"{declared} declared cases, so it has loosened by {want_floor - floor}. "
        f"It has gone stale this way twice (50/90, then 85/101). Move the literal."
    )
    # ⚠ AND THE OTHER HALF IS DECIDED THE OPPOSITE WAY — NO GUARD, BY CHOICE.
    # The docstring above says "NOTHING ASSERTS THAT THE TWO NUMBERS AGREE",
    # meaning this floor and the PASS count `.github/workflows/ci.yml` pins for
    # the `parity` job. They must NOT be asserted equal: this one counts CASES
    # DECLARED by `harness.cases()`, that one counts PASSES a run produced, and
    # the two differ by design — a structural check is a pass with no declared
    # case behind it, which is why the run reports 110 passes over 107 cases —
    # re-derived here from one run's own `SUMMARY cases=107 passes=110 failures=0`
    # line, not from arithmetic on the previous literal:
    # `cache-mtime-parity`, `orphan-reap-parity` and `nonregular-path-parity`,
    # THREE structural checks. ⚠ It was 104/102, then 105/103, then 106/103, then
    # 111/108 — and one of those pairs was WRONG for a whole round, because rows
    # moved `m` and only `ci.yml` was updated. The gap itself
    # widens every time a claim turns out to be unreachable from any row. A
    # guard equating them would be red on a correct tree and would train its
    # reader to edit whichever number was handier. The docstring's instruction —
    # read `ci.yml`'s comparison rather than that sentence — remains the answer.
    assert declared >= floor, (
        f"the parity gate declares only {declared} cases, and the floor is {floor} (107 were "
        f"measured on this tree, across every verb and every documented exit code). Two guards in "
        f"this file — the exit-only `why` check and the unique-id check — pass vacuously on a "
        f"narrowed list, so a shrinking case set gets quieter, not louder."
    )


def test_the_CI_content_floor_grep_names_EVERY_field_the_harness_prints() -> None:
    """🔴 THE `CONTENT-FLOOR` ANCHOR IN `ci.yml`, PINNED TO THE HARNESS THAT FEEDS IT.

    `.github/workflows/ci.yml` asserts the harness's in-run content controls with an
    ANCHORED PREFIX `grep`. A prefix says nothing about the fields after the ones it
    spells, so the check silently stops covering every field the harness later adds
    — while its own comment goes on claiming completeness.

    🔴 THAT HAS HAPPENED TWICE, WHICH IS WHY THIS IS A TEST AND NOT A THIRD COMMENT.
    The grep read TWO fields while the harness printed three, was then widened and
    re-commented to claim it named them all while the harness had already grown another.
    Each time the gap was found by a human re-reading the line, and each time the
    comment was the thing that discouraged the re-read. ⚠ It fails on a REMOVAL too,
    which is not hypothetical: a sentinel was deleted with the row it covered, and this
    guard is what required the anchor to shrink in the same commit.

    So: derive the field names from the harness's own `CONTENT-FLOOR` f-string and
    require `ci.yml`'s anchor to name all of them. Fails in BOTH directions — a field
    added to the harness and not to the grep, and a field in the grep the harness no
    longer prints.

    ⚠ IT PINS THE FIELD NAMES, NOT THE VALUES. `mtime-files=…` is deliberately absent
    from the anchor because it carries a measured number rather than a verdict, so it
    is excluded here too — by the same rule `ci.yml` states, not a second one.
    """
    harness = (ROOT / "tests/parity/harness.py").read_text(encoding="utf-8")
    block = re.search(
        r'print\(f"CONTENT-FLOOR (.*?)\)\n', harness, re.S)
    assert block, (
        "the harness no longer prints a `CONTENT-FLOOR` line in a shape this test can "
        "read. That is not licence to delete this guard: re-derive the anchor, because "
        "`ci.yml` still greps for one."
    )
    printed = re.findall(r"([a-z-]+)=\{", block.group(1))
    assert printed, "no CONTENT-FLOOR field names were parsed — the instrument is dead"
    # the trailing measured-number field is excluded by `ci.yml`'s own stated rule
    verdict_fields = [f for f in printed if f != "mtime-files"]
    assert len(verdict_fields) >= 4, (
        f"only {verdict_fields} parsed; the harness has printed at least four verdict "
        f"fields since this guard was written, so a shorter list means the parse broke"
    )
    ci = (ROOT / ".github/workflows/ci.yml").read_text(encoding="utf-8")
    anchor = re.search(r"grep -q '\^CONTENT-FLOOR ([^']*)'", ci)
    assert anchor, "ci.yml no longer carries an anchored `^CONTENT-FLOOR` grep"
    named = re.findall(r"([a-z-]+)=", anchor.group(1))
    assert named == verdict_fields, (
        f"`ci.yml`'s CONTENT-FLOOR anchor and the harness disagree about the fields.\n"
        f"  harness prints: {verdict_fields}\n"
        f"  ci.yml names:   {named}\n"
        f"MISSING from the grep: {[f for f in verdict_fields if f not in named]}\n"
        f"EXTRA in the grep:    {[f for f in named if f not in verdict_fields]}\n"
        f"A field the harness prints and the grep does not name is UNCHECKED by that "
        f"line — which is how this anchor went stale twice, both times while its "
        f"comment claimed it named them all. Add the field to the anchor (and fix the "
        f"count in the comment above it), or, if the harness dropped a field, drop it "
        f"from the anchor in the same commit."
    )
