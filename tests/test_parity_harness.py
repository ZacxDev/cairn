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

import ast
import re
import sys
from pathlib import Path

import pytest

ROOT = Path(__file__).resolve().parents[1]
HARNESS_DIR = ROOT / "tests" / "parity"
sys.path.insert(0, str(HARNESS_DIR))

import harness  # noqa: E402
import world as parity_world  # noqa: E402

#: The nine verbs. 🔴 READ FROM THE CLIENT'S OWN PARSER, NOT LISTED HERE. A hand list is blind to
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
    assert len(rows) >= 5, (
        f"the declared-difference table has {len(rows)} row(s). Every residual the gate does not "
        f"close has to be written down; six were measured when this was written."
    )


def test_the_harness_module_has_no_syntax_error_and_declares_cases():
    """The instrument's own positive control: it parses, and it declares a non-trivial case set.

    A zero here would make every assertion above pass vacuously, which is the failure mode the
    harness itself exists to guard against one layer up.
    """
    tree = ast.parse((HARNESS_DIR / "harness.py").read_text(encoding="utf-8"))
    assert any(isinstance(node, ast.FunctionDef) and node.name == "cases" for node in tree.body)
    assert len(harness.cases(1)) >= 50, (
        f"the parity gate declares only {len(harness.cases(1))} cases; 72 were measured when it "
        f"was written, across nine verbs and every documented exit code"
    )
