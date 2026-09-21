#!/usr/bin/env python3
"""`cairn doctor` — and the one property it exists for: an UNMEASURED answer.

🔴 WHAT THIS FILE GRADES. Every other diagnostic in this subsystem has been
caught reporting a zero it could not distinguish from a failure to look: the
frozen mirror's "ALL 26 entries, none omitted"; the pod's "ALL 5 entries in
alpha-toolkit/" over a store holding 9; `cairn validate`'s "NOTHING WAS CHECKED" at exit
0. `doctor` exists to join those facts in one call, so it is exactly the place
that defect would arrive next, wearing a nicer word.

So the assertions here are almost all of one shape: **the same visible outcome,
reached two ways, must NOT produce the same report.** A cache with zero entries
and a cache that could not be read; a store that holds nothing for this token and
a store that never answered; a mirror that is absent and a mirror that is
unreadable.

🔴 EVERY EXPECTED VALUE IS A LITERAL WRITTEN OUT HERE. `assert X == module.X` —
a constant agreeing with itself — has shipped in this subsystem five times, each
fix narrower than the class. Where a fact is shared with another module
(`server.token_id`, `server.DEFAULT_TOKEN_FILE`) the test imports BOTH sides and
grades the RELATIONSHIP against a literal, rather than letting either define the
answer.
"""

from __future__ import annotations

import hashlib
import importlib.util
import io
import json
import subprocess
import sys
import tarfile
import urllib.error
from email.message import Message
from pathlib import Path

import pytest

REPO = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(REPO / "lib"))

import cairn_doctor as cd  # noqa: E402
import subsystem_read_store as srs  # noqa: E402
from testlib import cairn_source, env_pin  # noqa: E402

CAIRN_CLI = REPO / "cairn"
SERVER_PY = REPO / "server" / "server.py"

#: Which variables configure the client is `testlib/env_pin.py`'s question now,
#: not this module's. It was open-coded at every suite that drives the client and
#: they disagreed; the SET is asserted in `tests/test_env_pin.py` and deliberately
#: not counted here — four separate drafts of that number were wrong, including
#: one written in this very comment.

#: The check names `doctor` emits on a SINGLE-instance host, in order.
#:
#: 🔴 A LITERAL WRITTEN OUT HERE, ONCE. The module docstring's rule is that an
#: expected value is spelled in this file rather than read from `cd` — `assert
#: X == module.X` is a constant agreeing with itself and has shipped here five
#: times. It is NOT a licence for three copies: this list was written out three
#: separate times in this module, so a check renamed in `cd` reds two sites and
#: a THIRD copy sits there agreeing with whichever one somebody remembered.
#: One literal, three readers.
SINGLE_INSTANCE_CHECKS = (
    "reader-resolution", "cache-stamp", "frozen-mirror", "pod",
    "cache-vs-pod", "token-scopes", "token",
)


@pytest.fixture(autouse=True)
def _pin_the_hosts_configuration(tmp_path, monkeypatch):
    """🔴 A TEST THAT INHERITS THE OPERATOR'S HOME IS PINNED TO NO DIMENSION AT
    ALL, AND ONE IN THIS FILE WAS RED ON A DEVELOPER HOST FOR DAYS WHILE CI
    STAYED GREEN.

    The mechanism, measured rather than supposed: `cmd_doctor` asks
    `cairn_instances.discover` what this host holds, and `discover` reads
    `$SUBSYSTEM_STORE_CONFIG` or — absent it — `~/.config/subsystem-store/`.
    A second `instances/<alias>.env` in that REAL directory, dropped there by
    anything at all, makes `routing.multi_instance` true, and `_doctor_instance`
    then renames every check `<alias>/<check>`. `test_a_no_sync_run_still_reads_
    the_LOCAL_config` looks its answer up by the bare name `token`, so it raised
    `IndexError` on a host whose only sin was having two stores configured. CI
    never saw it: a fresh checkout has an empty HOME. A suite whose CONFIG pins a
    dimension is blind to that dimension's bugs — and a suite that pins it by
    ACCIDENT, to whatever the operator's disk happens to say, is worse: it is
    blind AND it varies.

    🔴 A PREFIX SWEEP, NOT A LEDGER OF NAMES, AND THE LEDGER WAS TRIED FIRST.
    The first draft enumerated the five variables the client reads and graded
    that list with an AST walker over `cairn` and `lib/`, so a sixth would be
    REPORTED as unpinned. An audit built the alternative and measured it: the
    sweep below is three lines against that draft's ~120, passes at both points
    the draft did, and is strictly WIDER — a sixth variable is *pinned
    automatically* rather than reported, which is what the requirement actually
    wanted. The walker was also a SECOND AST reader of the client's source in a
    module that already imports the first (`testlib.cairn_source`, whose own
    docstring cites "one rule, one place"), and the enumerated list was a FOURTH
    hand-maintained copy of the same surface — `test_cairn_instances.py` clears
    the same five, `unchanged_output_capture.py` sets them twice. Discovered
    coverage of that surface, if anyone wants it, belongs in `cairn_source.py`
    beside the other AST readers, graded for every consumer at once.

    What the sweep pins, in three kinds because they fail differently:

      * the ENVIRONMENT — every `SUBSYSTEM_STORE_*`/`CAIRN_*` name is cleared,
        then `$SUBSYSTEM_STORE_CONFIG` is pointed at an empty directory. That one
        variable moves the config file, the `instances/` directory beside it and
        the default `routes.json` together; `$CAIRN_ROUTES` is the one setting it
        does not move, and the sweep covers it.
      * `$HOME` — for anything resolving `Path.home()` at CALL time.
      * 🔴 `DEFAULT_CACHE_ROOT` — which no env clear can reach. It is bound at
        IMPORT time from `Path.home()`, so it is not an environment read at all
        and moving `$HOME` afterwards does nothing to it. `read_store_root()`
        reads the module global at call time precisely so a test can repoint it.
        Two audits independently found the first draft's docstring claiming to
        pin "the dimension" while leaving this one inherited — the implementation
        is widened here rather than the sentence narrowed.

    🔴 MEASURED AT TWO POINTS, NAMED. The empty directory here is the boundary;
    `TestAnExtraInstanceRenamesEveryCheck` is the other point — it writes an
    instance into this same pinned directory and watches the prefix appear. That
    second test is what makes this fixture load-bearing rather than decorative,
    and it is measurably the ONLY thing that notices on CI: with the fixture
    neutered, a clean-HOME run reds exactly that one test, while this host reds
    three.
    """
    for name in env_pin.inherited_config():
        monkeypatch.delenv(name, raising=False)
    config_home = tmp_path / "pinned-config"
    config_home.mkdir(parents=True, exist_ok=True)
    monkeypatch.setenv("SUBSYSTEM_STORE_CONFIG", str(config_home / "env"))
    monkeypatch.setenv("HOME", str(tmp_path / "pinned-home"))
    monkeypatch.setattr(srs, "DEFAULT_CACHE_ROOT", tmp_path / "pinned-cache")
    return config_home


def _load_cairn_cli():
    """Exec `scripts/cairn` as a module — it has no .py extension."""
    spec = importlib.util.spec_from_loader(
        "cairn_cli_doctor", loader=None, origin=str(CAIRN_CLI)
    )
    mod = importlib.util.module_from_spec(spec)
    mod.__file__ = str(CAIRN_CLI)
    exec(compile(CAIRN_CLI.read_text(encoding="utf-8"), str(CAIRN_CLI), "exec"), mod.__dict__)
    return mod


def _doctor_codes() -> set[int]:
    """Every integer `cairn doctor` can hand a caller BY DESIGN, DISCOVERED.

    🔴 DISCOVERED, NEVER HAND-LISTED, AND THAT IS THE WHOLE POINT OF THE HELPER.
    A hand list of the three codes that exist today is blind to a FOURTH being
    added, which is the case both callers below are graded on. Hand-listing is
    also how the guard this class replaced ended up narrower than its own name.

    `EXIT_LEGEND` is unioned in because that tuple is what `render` and `--json`
    publish, so a number can reach a caller through it as well as through a
    constant.

    ⚠ STILL BLIND TO A CODE SPELLED SOMETHING OTHER THAN `EXIT_DOCTOR_*` and not
    in the legend either. That is a convention, not a structure; it is named here
    rather than left for a reader to assume away.
    """
    names = {n for n in dir(cd) if n.startswith("EXIT_DOCTOR_")}
    # Validate the instrument before reading its verdict: an empty discovery
    # would pass either caller's claim vacuously. This is a positive control on
    # `dir`, not a second claim about the codes — and it cannot short-circuit the
    # assertions it protects, because renumbering a code leaves every NAME in
    # place, and ADDING one only grows this set.
    assert names >= {
        "EXIT_DOCTOR_OK", "EXIT_DOCTOR_PROBLEM", "EXIT_DOCTOR_UNMEASURED",
    }, f"discovery found no doctor exit codes to check: {sorted(names)}"
    return {getattr(cd, n) for n in names} | {c for c, _ in cd.EXIT_LEGEND}


def _client_codes() -> set[int]:
    """Every `EXIT_*` integer the `cairn` CLI DEFINES, DISCOVERED by AST.

    🔴 THE OPERAND SET THE LEDGER BELOW USED TO HAND-LIST, AND THE FIX FOR ITS
    ONE-DIRECTION BLINDNESS. Nine `cli.EXIT_*` names were written out there, so a
    TENTH client code colliding with one of doctor's left the intersection at
    {0, 9} and the ledger GREEN — measured: `EXIT_STALE_MIRROR = 10` added to
    `cairn` survived the whole file at 60 passed. Discovery closes that, and the
    walker is the one `tests/test_cairn_write.py` already uses — promoted to
    `testlib.cairn_source` so both suites read it from one place, rather than a
    second AST walker over one question.

    🔴 TWO CONTROLS, BECAUSE A DISCOVERED SET THAT SILENTLY COMES BACK EMPTY IS
    THE REASSURING ZERO WEARING A NEW WORD. The floor proves discovery found at
    least the nine codes that existed when this was written. The cross-check
    against the exec'd module's own namespace proves the AST saw everything the
    RUNTIME has: AST discovery reports module-level `NAME = <int literal>` only,
    so a computed `EXIT_X = EXIT_WRITE_EXISTS + 1` is invisible to it while `dir`
    sees it. Disagreement in the other direction is worth a human too — an
    `EXIT_*` that `dir` has and the AST does not may be a code IMPORTED from
    another module, which is not the client's to own and would silently widen
    this ledger's scope past doctor-vs-client.
    """
    names = cairn_source.exit_constant_names()
    assert len(names) >= 9, (
        f"discovery found only {len(names)} client exit code(s) — "
        f"{sorted(names)} — where nine existed when this was written. Either the "
        f"AST walker in testlib/cairn_source.py stopped working, in which case "
        f"every assertion built on it is vacuous, or a code was deleted."
    )
    runtime = {n for n in dir(_load_cairn_cli()) if n.startswith("EXIT_")}
    assert runtime == names, (
        f"the client's EXIT_* names read from the SOURCE and from the exec'd "
        f"MODULE disagree: source-only {sorted(names - runtime)}, "
        f"module-only {sorted(runtime - names)}. A module-only name is either a "
        f"computed constant (AST discovery cannot see one, so this ledger is "
        f"undercounting the client's codes) or one IMPORTED from another module "
        f"(which this ledger's doctor-vs-client scope does not cover). Decide "
        f"which, and widen testlib/cairn_source.py or this scope deliberately."
    )
    consts = cairn_source.module_constants()
    return {consts[n] for n in names}


def _load_api():
    """Import `server.py` by path. `sys.modules[...]` BEFORE `exec_module` —
    without it the first `@dataclass` raises under `from __future__ import
    annotations`. Same idiom as `test_cairn_write._load_api`."""
    spec = importlib.util.spec_from_file_location("srv_doctor", SERVER_PY)
    assert spec and spec.loader
    module = importlib.util.module_from_spec(spec)
    sys.modules[spec.name] = module
    spec.loader.exec_module(module)
    return module


def _store(root: Path, scopes: dict[str, list[str]], *, stamp: str | None = None) -> Path:
    root.mkdir(parents=True, exist_ok=True)
    for scope, entries in scopes.items():
        (root / scope).mkdir(parents=True, exist_ok=True)
        for name in entries:
            (root / scope / name).write_text("# entry\n", encoding="utf-8")
    if stamp is not None:
        (root / ".sync-stamp").write_text(stamp, encoding="utf-8")
    return root


def _collect(**over):
    """`collect` with every argument defaulted to a benign, MEASURED fact.

    Each test overrides exactly the one thing it is about, which is what keeps a
    mutation isolated to the branch under test rather than to the fixture.
    """
    base = dict(
        resolved_root=Path("/nowhere/cache"),
        stamp_lines=("synced=1", "entries=2"),
        stamp_reason=None,
        cache_root=Path("/nowhere/cache"),
        mirror_root=Path("/nowhere/mirror"),
        pod=cd.PodFacts(reached=True, visible_entries=2, store_wide_entries=2,
                        visible_scopes=("alpha",), snapshot_header="entry-files=2"),
        token="tok",
        token_reason="",
        identity_remedy="ask the operator",
    )
    base.update(over)
    return cd.collect(**base)


def _by_name(checks) -> dict[str, cd.Check]:
    return {c.name: c for c in checks}


# =============================================================================
# The four states, and the contract on each
# =============================================================================

class TestTheStateVocabulary:
    def test_the_four_states_are_exactly_these_literals(self) -> None:
        """Written out here, never read off the module. A caller — a human or a
        `--json` consumer — branches on these strings."""
        assert cd.OK == "OK"
        assert cd.PROBLEM == "PROBLEM"
        assert cd.UNMEASURED == "UNMEASURED"
        assert cd.NOT_OBSERVABLE == "NOT-OBSERVABLE"
        assert cd.STATES == ("OK", "PROBLEM", "UNMEASURED", "NOT-OBSERVABLE")

    def test_the_exit_codes_are_exactly_these_literals(self) -> None:
        """These three are the printed contract — `render` and `--json` both
        publish them — so they are written out here rather than read off the
        module. Which of them OVERLAP the client's codes is a separate question,
        graded against the CLI below."""
        assert cd.EXIT_DOCTOR_OK == 0
        assert cd.EXIT_DOCTOR_PROBLEM == 9
        assert cd.EXIT_DOCTOR_UNMEASURED == 10

    def test_the_codes_doctor_SHARES_with_the_client_are_EXACTLY_0_and_9(
        self,
    ) -> None:
        """🔴 AN INVARIANT GUARD, NOT REGRESSION COVERAGE. No bug ever made
        these two sets disagree; the defect was a COMMENT that claimed doctor's
        codes were "disjoint from every other `cairn` code" while
        `EXIT_DOCTOR_PROBLEM` and `EXIT_WRITE_EXISTS` are both 9. So this pins a
        fact nothing has yet violated, and must not be counted as a regression
        test for anything.

        🔴 AND IT REPLACES A GUARD THAT WAS NARROWER THAN ITS OWN NAME. The
        version here was called `..._collide_with_NO_other_cairn_exit_code` and
        asserted `EXIT_DOCTOR_PROBLEM not in others` — against an `others` set
        that enumerated EIGHT of the client's NINE codes and left
        `EXIT_WRITE_EXISTS` out. It passed because the one colliding number was
        outside the set it looked at, and it read as coverage of exactly the
        claim it could not see.

        🔴 ENUMERATING A SET BY HAND IS THE MECHANISM, AND THE FIRST VERSION OF
        THIS LEDGER DID IT TWICE — which left it blind in the one direction its
        own failure message advertised. Both operands were written out: nine
        `cli.EXIT_*` names and three `cd.EXIT_DOCTOR_*`. So a NEW constant on
        either side was invisible, and "red when the shared set GROWS" was false
        for the case a reader would picture. Measured on this file: adding
        `EXIT_STALE_MIRROR = 10` to `cairn` — a new client code colliding with
        `EXIT_DOCTOR_UNMEASURED` — left it at 60 passed. BOTH operands are now
        DISCOVERED (`_client_codes`, `_doctor_codes`), each with its own control
        against a vacuous discovery.

        The relationship is graded as a SET INTERSECTION against a literal, so it
        fails in BOTH directions a maintainer needs to hear about: red when the
        shared set GROWS (a new code on EITHER side reuses one of the other's
        numbers) and red when it SHRINKS (someone renumbers the 9 apart and leaves
        `lib/cairn_doctor.py` documenting an overlap that no longer exists).

        🔴 THE `client` PIN BELOW IS THE CONTROL, NOT A HAND LIST OF THE OPERAND.
        Once the set is discovered, a literal pin on its VALUES is what proves the
        discovery found something: `set() == {0, 2, …}` fails loudly, where
        `set() & set() == set()` would have passed the claim above vacuously. It
        is also a deliberate change-detector — adding a client code that collides
        with nothing still fails here, on purpose, because a new exit code is
        exactly when a human should look at the overlap question. Adding it to the
        literal is the acknowledgement.

        0 is in the ledger and is not a hazard: it means success on both sides.
        9 is the real overlap, and it is unambiguous because no single call site
        can produce both meanings — `doctor` never creates an entry and `create`
        never runs diagnostics. Nor is it the first: `cairn`'s
        `EXIT_REFRESH_FAILED` and `lib/subsystem_read_store`'s
        `EXIT_UNSTAMPED_READ_STORE` are both 4 across two tools. The scope of THIS
        ledger is doctor-vs-client only; the reader defines exactly one code and it
        is not in doctor's set.

        🔴 THE INTERSECTION IS ALSO WHY THERE IS NO SEPARATE `EXIT_USAGE` CHECK
        HERE. 2 is the one client code a `doctor` caller CAN receive alongside
        doctor's own — argparse returns it for a bad flag on every subcommand
        (measured: `cairn doctor --bogus-flag` -> 2) — so 2 entering this block
        WOULD be genuinely ambiguous, unlike any write code. But 2 is already in
        `client`, so a doctor code of 2 puts 2 in the intersection and this one
        assertion fails. A second assertion for it would be unreachable: nothing
        can add 2 to doctor's codes without first moving the set below.

        🔴 1 IS THE OPPOSITE CASE AND DOES NEED ITS OWN ASSERTION — the next test.
        A `doctor` caller can receive 1 as well as 2, but 1 is not one of the
        client's nine codes, so an `EXIT_DOCTOR_* = 1` leaves this intersection at
        {0, 9} and this test GREEN. Do not read "the ledger covers the codes a
        caller can also receive" off this docstring: it covers the ones that are
        `cairn` constants.
        """
        client = _client_codes()
        doctor_codes = _doctor_codes()
        # The substantive claim first, so a new colliding code fails with the
        # message about the OVERLAP rather than with the control's message about
        # the literal below having moved.
        assert doctor_codes & client == {0, 9}, (
            f"the codes doctor shares with the client are now "
            f"{sorted(doctor_codes & client)}, not [0, 9]. Doctor's discovered "
            f"codes are {sorted(doctor_codes)}, the client's {sorted(client)}. "
            f"GROWN means a new overlap nobody documented — and it can have "
            f"arrived from EITHER side, a new EXIT_DOCTOR_* or a new cairn "
            f"EXIT_*; SHRUNK means the 9 was renumbered and the comment above "
            f"EXIT_DOCTOR_OK in lib/cairn_doctor.py, which states that overlap "
            f"and says not to remove it, is now false."
        )
        # 11 is `EXIT_UNROUTED`: the client could not decide WHICH INSTANCE a
        # scope belongs to and did nothing. Added deliberately, and the overlap
        # question was looked at: doctor's codes are {0, 9, 10}, so 11 collides
        # with none of them and the intersection above is unmoved.
        assert client == {0, 2, 3, 4, 5, 6, 7, 8, 9, 11}, (
            f"cairn's exit codes are now {sorted(client)}, not [0, 2, 3, 4, 5, 6, "
            f"7, 8, 9]. This pin is the control on the discovery above — an empty "
            f"or truncated discovery fails HERE rather than passing the "
            f"intersection vacuously. If you added a code deliberately and it "
            f"collides with none of doctor's, add it to this literal; that edit "
            f"is the record that a human looked at the overlap question."
        )

    def test_1_is_NOT_one_of_doctors_codes(self) -> None:
        """🔴 THE LEDGER ABOVE IS STRUCTURALLY BLIND TO THIS, WHICH IS WHY IT IS
        ITS OWN TEST AND NOT ANOTHER LINE IN THAT ONE. The ledger grades
        `doctor_codes & client`. 1 is not one of the client's nine codes, so an
        `EXIT_DOCTOR_* = 1` leaves that intersection at {0, 9} and the ledger stays
        GREEN — while the new code is as ambiguous as a doctor code of 2 would be,
        reached by a path the intersection cannot see.

        1 is what a `doctor` invocation returns for an uncaught exception, so it is
        a CLASS of outcome rather than one bug: 1 is the interpreter's own code for
        any traceback that reaches the top. One route is reachable BY DESIGN —
        `cairn`'s lazy `_cairn_doctor()` import is deliberately unguarded at its
        `cmd_doctor` call site, so with `lib/cairn_doctor.py` absent `cairn doctor`
        exits 1 with `ModuleNotFoundError`; the docstring at `cairn:_doctor_epilog`
        states that the help-string import FAILS SOFT while `cmd_doctor` still
        fails LOUDLY. Measured at a second point: a module that is present but
        unparseable raises `SyntaxError`, which is not an `ImportError` and so is
        not soft-caught either — also exit 1. A doctor code of 1 is
        indistinguishable from both.

        🟡 AN INVARIANT GUARD, NOT REGRESSION COVERAGE. Nothing has ever set one of
        doctor's codes to 1. It exists because the comment it defends enumerates
        0/1/2/9/10 where the previous version of that comment enumerated 0/2/9/10:
        an INCOMPLETE ENUMERATION is the defect that keeps recurring in this spot,
        so the member that went missing gets a machine-readable claim instead of a
        sentence.

        The set is DISCOVERED, not hand-listed — by `_doctor_codes`, which the
        ledger above now shares, since a set discovered two ways is two things to
        keep true. Hand-listing is how the guard this class replaced ended up
        narrower than its own name, and the case in view — someone adding a FOURTH
        `EXIT_DOCTOR_*` constant — is precisely the one a hand list of three cannot
        see. That helper carries the positive control on `dir` and the reason
        `EXIT_LEGEND` is unioned in.
        """
        codes = _doctor_codes()
        assert 1 not in codes, (
            f"a doctor exit code is now 1, which a `doctor` caller cannot tell "
            f"from an uncaught exception: `cairn doctor` exits 1 with "
            f"ModuleNotFoundError when lib/cairn_doctor.py is absent, because the "
            f"lazy import at cairn:cmd_doctor is unguarded on purpose. The ledger "
            f"above cannot catch this — 1 is not one of the client's codes, so its "
            f"intersection stays [0, 9]. Doctor's codes are now {sorted(codes)}."
        )

    def test_a_check_with_an_EMPTY_detail_is_refused(self) -> None:
        """🔴 A bare `UNMEASURED` with no reason is the reassuring zero wearing
        a different word. The refusal is at construction so no render path can
        emit one."""
        with pytest.raises(ValueError, match="empty detail"):
            cd.Check("x", cd.UNMEASURED, "   ")

    def test_an_unknown_state_is_refused(self) -> None:
        with pytest.raises(ValueError, match="unknown check state"):
            cd.Check("x", "FINE", "detail")

    def test_PodFacts_ENFORCES_its_reason_it_does_not_merely_document_one(
        self,
    ) -> None:
        """🔴 THE ASYMMETRY WAS THE FINDING. `Check` refused an empty detail at
        construction; `PodFacts` said "Mandatory in that case" and checked
        nothing. An unreasoned `PodFacts(reached=False)` renders `… could not be
        established — .` and walks straight past `Check`'s guard, because the
        empty string is wrapped in literal f-string text before it gets there."""
        with pytest.raises(ValueError, match="requires a reason"):
            cd.PodFacts(reached=False)
        with pytest.raises(ValueError, match="requires a reason"):
            cd.PodFacts(reached=False, reason="   ")
        # A REACHED pod needs none — there is nothing unexplained about success.
        assert cd.PodFacts(reached=True).reason == ""


class TestTheVerdict:
    def test_a_problem_outranks_an_unmeasured(self) -> None:
        checks = [cd.Check("a", cd.UNMEASURED, "no"), cd.Check("b", cd.PROBLEM, "bad")]
        assert cd.exit_code(checks) == 9

    def test_an_unmeasured_outranks_an_ok(self) -> None:
        checks = [cd.Check("a", cd.OK, "fine"), cd.Check("b", cd.UNMEASURED, "no")]
        assert cd.exit_code(checks) == 10

    def test_all_ok_is_zero(self) -> None:
        assert cd.exit_code([cd.Check("a", cd.OK, "fine")]) == 0

    def test_NOT_OBSERVABLE_alone_is_ZERO_and_that_is_deliberate(self) -> None:
        """🔴 THE PERMANENTLY-RED-GATE GUARD. The pod exposes no identity route,
        so the credential check is NOT-OBSERVABLE on every healthy run forever.
        Escalating on it would make `doctor` non-zero always, which
        `claude/RULES.md` says trains everyone to click through. Pinned so the
        two states cannot be quietly merged."""
        checks = [cd.Check("a", cd.OK, "fine"), cd.Check("b", cd.NOT_OBSERVABLE, "ask")]
        assert cd.exit_code(checks) == 0

    def test_an_EMPTY_check_list_is_not_a_clean_bill(self) -> None:
        """A run that produced no checks exits 0 today, so this pins the shape
        the render carries instead: the counts are printed, and `OK=0` is
        visibly different from `OK=7`. Stated rather than left implied — this is
        an INVARIANT GUARD on the render, not a claim that zero checks is safe.
        """
        assert "OK=0" in cd.render([cd.Check("a", cd.NOT_OBSERVABLE, "x")])


# =============================================================================
# UNMEASURED is not a zero — the whole point, one check at a time
# =============================================================================

class TestAnUnmeasuredAnswerIsNotAZero:
    def test_an_unreached_pod_leaves_the_count_UNMEASURED_not_zero(self) -> None:
        checks = _by_name(_collect(
            pod=cd.PodFacts(reached=False, reason="connection refused")
        ))
        assert checks["cache-vs-pod"].state == cd.UNMEASURED
        assert "connection refused" in checks["cache-vs-pod"].detail
        assert checks["token-scopes"].state == cd.UNMEASURED

    def test_a_store_that_genuinely_holds_NOTHING_is_OK_not_unmeasured(
        self, tmp_path
    ) -> None:
        """🔴 THE OTHER HALF, and the one a lazier implementation gets wrong. An
        empty store REACHED is a measured fact, and reporting it as UNMEASURED
        would be the mirror-image lie: a working system described as unknowable.
        """
        cache = _store(tmp_path / "cache", {})
        checks = _by_name(_collect(
            cache_root=cache,
            pod=cd.PodFacts(reached=True, visible_entries=0, store_wide_entries=0,
                            visible_scopes=(), snapshot_header="entry-files=0"),
        ))
        assert checks["cache-vs-pod"].state == cd.OK
        assert "0 entry file(s) here" in checks["cache-vs-pod"].detail

    def test_the_two_zeroes_produce_DIFFERENT_reports(self, tmp_path) -> None:
        """The discriminating assertion: same visible count, two mechanisms."""
        cache = _store(tmp_path / "cache", {})
        reached = _by_name(_collect(
            cache_root=cache,
            pod=cd.PodFacts(reached=True, visible_entries=0, store_wide_entries=0,
                            snapshot_header="entry-files=0"),
        ))["cache-vs-pod"]
        unreached = _by_name(_collect(
            cache_root=cache,
            pod=cd.PodFacts(reached=False, reason="DNS failure"),
        ))["cache-vs-pod"]
        assert reached.state != unreached.state
        assert reached.detail != unreached.detail

    def test_a_missing_mirror_is_OK_and_an_UNREADABLE_one_is_UNMEASURED(
        self, tmp_path
    ) -> None:
        """Absent and unreadable are the classic pair. A fresh host has no
        mirror at all, which is fine; a mirror this process cannot read is a
        fact nobody has established, and the two must not render alike."""
        absent = _by_name(_collect(mirror_root=tmp_path / "never-existed"))["frozen-mirror"]
        assert absent.state == cd.OK

        blocked = tmp_path / "blocked"
        blocked.mkdir()
        (blocked / "scope").mkdir()
        blocked.chmod(0o000)
        try:
            unreadable = _by_name(_collect(mirror_root=blocked))["frozen-mirror"]
        finally:
            blocked.chmod(0o700)
        assert unreadable.state == cd.UNMEASURED
        assert absent.detail != unreadable.detail

    def test_the_absent_vs_unreadable_split_is_STRUCTURAL_not_a_sentence(
        self, tmp_path
    ) -> None:
        """🔴 A guard SPELLED rather than structural is walkable by rewording.
        The first version asked `reason.endswith("does not exist")`, so changing
        that message would have made an UNREADABLE mirror report as "nothing
        pre-cutover on this host" — an OK, silently. `Reading.absent` is a flag."""
        missing = cd._describe(tmp_path / "gone", cd.store_scopes)
        assert missing.absent is True and missing.value is None

        blocked = tmp_path / "blocked"
        blocked.mkdir()
        blocked.chmod(0o000)
        try:
            denied = cd._describe(blocked, cd.store_scopes)
        finally:
            blocked.chmod(0o700)
        assert denied.absent is False and denied.value is None
        assert not denied.ok and not missing.ok

        present = cd._describe(_store(tmp_path / "s", {"a": []}), cd.store_scopes)
        assert present.ok and present.absent is False and present.value == ("a",)


    def test_a_no_sync_run_says_SO_rather_than_reporting_an_outage(self) -> None:
        """`--no-sync` is a choice, not a failure. The reason has to name it, or
        an operator reads a deliberate offline run as a broken store."""
        checks = _by_name(_collect(pod=cd.PodFacts(
            reached=False,
            reason="--no-sync was given, so the store was never contacted",
        )))
        assert "--no-sync" in checks["pod"].detail
        assert checks["pod"].state == cd.UNMEASURED


# =============================================================================
# The individual checks
# =============================================================================

class TestTheReaderResolution:
    def test_an_unstamped_resolution_is_a_PROBLEM_naming_the_remedy(self) -> None:
        checks = _by_name(_collect(
            stamp_lines=None, stamp_reason="no `.sync-stamp` in /nowhere/cache",
        ))
        assert checks["reader-resolution"].state == cd.PROBLEM
        assert "cairn sync" in checks["reader-resolution"].detail
        assert checks["cache-stamp"].state == cd.PROBLEM

    def test_the_unstamped_detail_names_the_READERS_four_not_cairns(self) -> None:
        """🔴 THE TWO EXIT 4s. `cairn sync`'s own 4 (`EXIT_REFRESH_FAILED`)
        means the store was NOT reached but the cache survived — re-running
        `cairn sync` is the command that just failed. The 4 `cairn sync` FIXES
        is the reader's `EXIT_UNSTAMPED_READ_STORE`. Confusing them sends a
        reader to re-run the failure, so the detail names which four it is."""
        detail = _by_name(_collect(
            stamp_lines=None, stamp_reason="no stamp",
        ))["reader-resolution"].detail
        assert "EXIT_UNSTAMPED_READ_STORE" in detail
        assert "READER" in detail

    def test_a_stamped_resolution_relays_the_fields_UNPARSED(self) -> None:
        """The stamp's schema belongs to `cairn sync`. Doctor prints the lines;
        it does not interpret them, and it computes no age from them."""
        detail = _by_name(_collect(
            stamp_lines=("synced=1788000000", "coverage=ALL", "entries=7"),
        ))["cache-stamp"].detail
        assert "synced=1788000000" in detail
        assert "coverage=ALL" in detail
        assert "entries=7" in detail


class TestTheFrozenMirror:
    def test_a_WRITABLE_entry_file_is_a_PROBLEM(self, tmp_path) -> None:
        """🔴 THIS FOUND A LIVE DATA-LOSS PATH ON ITS FIRST RUN.

        Measured on the workbench: **7** entry files under the
        supposedly-frozen mirror were still mode 644, an append was watched to
        SUCCEED on one, and six entries created on the dead mirror after the
        cutover existed nowhere else. The operator has since completed the
        freeze; the tree now measures 159 files, all `0444`, writable=0, and
        `cairn doctor` reports `frozen-mirror OK`.

        ⚠ Which is exactly why this builds its OWN fixture rather than asserting
        against the live tree: those numbers moved within a day of being taken,
        and a guard pinned to them would now be red for the wrong reason.
        """
        mirror = _store(tmp_path / "m", {"alpha": ["a.md", "b.md"]})
        (mirror / "alpha" / "a.md").chmod(0o444)
        (mirror / "alpha" / "b.md").chmod(0o644)
        check = _by_name(_collect(mirror_root=mirror))["frozen-mirror"]
        assert check.state == cd.PROBLEM
        assert "alpha/b.md" in check.detail
        assert "1 entry file" in check.detail

    def test_a_fully_frozen_mirror_is_OK(self, tmp_path) -> None:
        mirror = _store(tmp_path / "m", {"alpha": ["a.md"]})
        (mirror / "alpha" / "a.md").chmod(0o444)
        assert _by_name(_collect(mirror_root=mirror))["frozen-mirror"].state == cd.OK

    def test_the_walk_counts_FILES_not_directories(self, tmp_path) -> None:
        """A writable scope DIRECTORY is expected and correct — `subsystem-index`
        documents that a first-ever entry is still created locally. Only the
        entry files are frozen, so a directory mode must not raise a PROBLEM."""
        mirror = _store(tmp_path / "m", {"alpha": ["a.md"]})
        (mirror / "alpha" / "a.md").chmod(0o444)
        (mirror / "alpha").chmod(0o755)
        assert cd.writable_entry_files(mirror) == ()


class TestThePodProbe:
    def test_an_UNAUTHORISED_answer_is_not_an_outage(self) -> None:
        """🔴 THE DISCRIMINATION THE BRIEF NAMES. 401 means the host is UP and
        the credential is wrong — a completely different remedy from a DNS
        failure, and `resolve_state` deliberately collapses the two because a
        READ degrades to the cache either way. Doctor must not."""
        refused = _by_name(_collect(pod=cd.PodFacts(
            reached=False, reason="answered HTTP 401", http_status=401,
        )))["pod"]
        outage = _by_name(_collect(pod=cd.PodFacts(
            reached=False, reason="unreachable: [Errno -2] Name or service not known",
        )))["pod"]
        assert refused.state == cd.PROBLEM
        assert outage.state == cd.UNMEASURED
        assert "NOT an outage" in refused.detail

    def test_a_403_names_the_edge_as_a_rival_explanation(self) -> None:
        """MEASURED: this host's edge 403s urllib's default User-Agent, and that
        403 arrives looking like a bad token AND like the store being down. A
        report that named only the token would send an operator to rotate a
        credential that is fine."""
        detail = _by_name(_collect(pod=cd.PodFacts(
            reached=False, reason="answered HTTP 403", http_status=403,
        )))["pod"].detail
        assert "User-Agent" in detail

    def test_a_500_is_a_PROBLEM_that_names_the_status(self) -> None:
        detail = _by_name(_collect(pod=cd.PodFacts(
            reached=False, reason="answered HTTP 503", http_status=503,
        )))["pod"].detail
        assert "503" in detail


class TestTheScopeVisibility:
    def test_a_local_scope_the_store_did_not_send_is_a_PROBLEM(self, tmp_path) -> None:
        """🔴 MEASURED LIVE: `delta-app-requests` and
        `delta-developer-docs` exist in the frozen mirror and are absent from
        the snapshot this token receives."""
        cache = _store(tmp_path / "c", {"alpha": ["a.md"]})
        mirror = _store(tmp_path / "m", {"alpha": ["a.md"], "orphan": ["o.md"]})
        check = _by_name(_collect(
            cache_root=cache, mirror_root=mirror,
            pod=cd.PodFacts(reached=True, visible_entries=1, store_wide_entries=1,
                            visible_scopes=("alpha",)),
        ))["token-scopes"]
        assert check.state == cd.PROBLEM
        assert "orphan" in check.detail

    def test_a_missing_scope_is_TAGGED_with_the_tree_it_came_from(
        self, tmp_path
    ) -> None:
        """🔴 THE TWO ROOTS MEAN DIFFERENT THINGS AND LEAD TO DIFFERENT ACTIONS.

        A scope in the SYNCED CACHE the pod no longer sends is a credential or a
        deletion. A scope in the FROZEN MIRROR only is a pre-cutover leftover
        that may never have reached the pod at all — so "add it to the allowlist"
        is the wrong first question. The first version merged the two sets and
        said "N scope(s) exist on this disk", and both scopes it named live were
        mirror-only.
        """
        cache = _store(tmp_path / "c", {"alpha": ["a.md"], "cache-only": ["c.md"]})
        mirror = _store(tmp_path / "m", {"alpha": ["a.md"], "mirror-only": ["m.md"]})
        check = _by_name(_collect(
            cache_root=cache, mirror_root=mirror,
            pod=cd.PodFacts(reached=True, visible_entries=1, store_wide_entries=1,
                            visible_scopes=("alpha",)),
        ))["token-scopes"]
        assert check.state == cd.PROBLEM
        assert "mirror-only [mirror]" in check.detail
        assert "cache-only [cache]" in check.detail
        # …and the mirror-only one gets the leftover reading spelled out, while
        # the cache-only one does not inherit it.
        assert "ONLY in the frozen pre-cutover mirror (mirror-only)" in check.detail

    def test_a_scope_in_BOTH_trees_is_tagged_with_both(self, tmp_path) -> None:
        cache = _store(tmp_path / "c", {"shared": ["a.md"]})
        mirror = _store(tmp_path / "m", {"shared": ["a.md"]})
        detail = _by_name(_collect(
            cache_root=cache, mirror_root=mirror,
            pod=cd.PodFacts(reached=True, visible_entries=0, store_wide_entries=0,
                            visible_scopes=()),
        ))["token-scopes"].detail
        assert "shared [cache+mirror]" in detail
        # Present in the live cache too, so it is NOT a pre-cutover leftover.
        assert "ONLY in the frozen" not in detail

    def test_it_refuses_to_GUESS_which_of_the_two_readings_is_right(
        self, tmp_path
    ) -> None:
        """🔴 The API answers "outside your allowlist" and "never existed" with
        BYTE-IDENTICAL bytes, deliberately, so that an error cannot enumerate
        the store. A report that picked one would be a coin flip recorded as a
        diagnosis — `claude/RULES.md` on an empty result naming no mechanism."""
        cache = _store(tmp_path / "c", {"alpha": ["a.md"]})
        mirror = _store(tmp_path / "m", {"orphan": ["o.md"]})
        detail = _by_name(_collect(
            cache_root=cache, mirror_root=mirror,
            pod=cd.PodFacts(reached=True, visible_entries=1, store_wide_entries=1,
                            visible_scopes=("alpha",)),
        ))["token-scopes"].detail
        assert "byte-identical" in detail
        assert "allowlist" in detail

    def test_a_store_wide_count_ABOVE_the_visible_one_is_reported(self) -> None:
        """The other channel, and the only client-side evidence that entries
        exist which this credential cannot reach: `X-Store-Snapshot` carries an
        UNFILTERED `entry-files=` while `X-Store-Entries` is this token's slice.
        """
        check = _by_name(_collect(pod=cd.PodFacts(
            reached=True, visible_entries=2, store_wide_entries=11,
            visible_scopes=("alpha",),
        )))["token-scopes"]
        assert check.state == cd.PROBLEM
        assert "11" in check.detail and "9 live in scopes" in check.detail

    def test_an_UNREADABLE_local_root_makes_this_UNMEASURED_not_OK(
        self, tmp_path
    ) -> None:
        """🔴 The first version returned OK with the hole named in its tail. An
        OK carrying a caveat is how a partial answer gets read as a clean one —
        "every scope on this disk is among them" is a claim about a set the walk
        could not finish building. Graded against the OK case below, which is
        identical except that both roots are readable."""
        cache = _store(tmp_path / "c", {"alpha": ["a.md"]})
        blocked = tmp_path / "m"
        blocked.mkdir()
        blocked.chmod(0o000)
        try:
            check = _by_name(_collect(
                cache_root=cache, mirror_root=blocked,
                pod=cd.PodFacts(reached=True, visible_entries=1, store_wide_entries=1,
                                visible_scopes=("alpha",)),
            ))["token-scopes"]
        finally:
            blocked.chmod(0o700)
        assert check.state == cd.UNMEASURED
        assert "could not be fully read" in check.detail

    def test_equal_counts_and_no_missing_scope_is_OK(self, tmp_path) -> None:
        cache = _store(tmp_path / "c", {"alpha": ["a.md"]})
        check = _by_name(_collect(
            cache_root=cache, mirror_root=tmp_path / "absent",
            pod=cd.PodFacts(reached=True, visible_entries=1, store_wide_entries=1,
                            visible_scopes=("alpha",)),
        ))["token-scopes"]
        assert check.state == cd.OK


class TestTheCredential:
    def test_no_token_configured_is_a_PROBLEM_carrying_the_config_reason(self) -> None:
        check = _by_name(_collect(
            token=None,
            token_reason="config incomplete: SUBSYSTEM_STORE_TOKEN not set",
        ))["token"]
        assert check.state == cd.PROBLEM
        assert "SUBSYSTEM_STORE_TOKEN" in check.detail

    def test_a_configured_token_is_NOT_OBSERVABLE_with_a_remedy(self) -> None:
        check = _by_name(_collect(token="tok", identity_remedy="RUN THIS THING"))["token"]
        assert check.state == cd.NOT_OBSERVABLE
        assert "RUN THIS THING" in check.detail

    def test_the_TOKEN_ITSELF_never_reaches_the_report(self) -> None:
        secret = "s3cr3t-not-in-any-output"
        rendered = cd.render(_collect(token=secret))
        assert secret not in rendered
        assert cd.token_fingerprint(secret) in rendered

    def test_the_fingerprint_is_the_SERVERS_token_id(self) -> None:
        """🔴 THE SEAM. The fingerprint is only useful because it MATCHES the
        handle the pod's audit log carries; two independent spellings that drift
        make it a number an operator cannot look up. `cairn` cannot import
        `server.py`, so the rule is spelled twice — and graded here against a
        literal digest, so the two agreeing with each other is not the test."""
        api = _load_api()
        sample = "a-known-fixture-token"
        # 🔴 THE EXPECTED DIGEST IS A LITERAL, PRODUCED BY A DIFFERENT TOOL:
        #     printf 'a-known-fixture-token' | sha256sum | cut -c1-12
        # Without it, `cd` and `server` agreeing with each other would satisfy
        # this test while both had drifted from sha256 — the "constant agreeing
        # with itself" shape this subsystem has shipped five times.
        expected = "d56f7752989e"
        assert hashlib.sha256(sample.encode("utf-8")).hexdigest()[:12] == expected
        assert cd.token_fingerprint(sample) == expected
        assert api.token_id(sample) == expected
        assert cd.TOKEN_FINGERPRINT_CHARS == 12

    def test_the_identity_remedy_names_the_pods_own_token_file(self) -> None:
        """The remedy is a command a human types under pressure. If it named a
        path the pod does not mount it would send them to an empty file and read
        as 'no rows', which is the same silent zero one layer out."""
        api = _load_api()
        cli = _load_cairn_cli()
        assert api.DEFAULT_TOKEN_FILE == "/run/secrets/subsystem-store/token"
        assert api.DEFAULT_TOKEN_FILE in cli.IDENTITY_REMEDY
        assert "subsystem-store" in cli.IDENTITY_REMEDY


# =============================================================================
# The rendered surface and the CLI wiring
# =============================================================================

class TestTheRenderedReport:
    def test_the_exit_legend_is_printed_by_the_COMMAND(self) -> None:
        """🔴 The brief's requirement: the codes are documented by the command,
        never by a skill. So every human-readable run carries them."""
        rendered = cd.render(_collect())
        for code, _why in cd.EXIT_LEGEND:
            assert f"{code} =" in rendered

    def test_every_state_is_counted_in_the_summary(self) -> None:
        rendered = cd.render(_collect())
        for state in cd.STATES:
            assert f"{state}=" in rendered

    def test_the_json_carries_the_legend_and_the_exit(self) -> None:
        payload = cd.to_dict(_collect())
        assert payload["exit"] in (0, 9, 10)
        assert set(payload["exit_legend"]) == {"0", "9", "10"}
        assert {c["name"] for c in payload["checks"]} == set(SINGLE_INSTANCE_CHECKS)

    def test_the_check_set_is_pinned_to_these_seven_names(self) -> None:
        """A LEDGER, not a count: a check silently disappearing is a fact nobody
        is checking any more, and a count would not say which.

        ⚠ The two assertions are NOT the same claim and the duplication is
        deliberate: this one pins the ORDER (`render` aligns on it), the
        `--json` one above pins the SET. They share `SINGLE_INSTANCE_CHECKS`
        because sharing the literal is what stops a third spelling drifting —
        not because either subsumes the other."""
        assert [c.name for c in _collect()] == list(SINGLE_INSTANCE_CHECKS)


class TestTheCliWiring:
    def test_doctor_is_a_real_subcommand_reaching_cmd_doctor(self) -> None:
        cli = _load_cairn_cli()
        args = cli.build_parser().parse_args(["doctor"])
        assert args.func is cli.cmd_doctor
        assert args.json is False and args.no_sync is False

    def test_doctor_takes_NO_scope_flag(self) -> None:
        """A `--scope` here would invite the filtered-cache mistake `cmd_sync`
        documents, and doctor asks about the HOST, not a scope."""
        cli = _load_cairn_cli()
        with pytest.raises(SystemExit):
            cli.build_parser().parse_args(["doctor", "--scope", "alpha-toolkit"])

    def test_an_ABSENT_doctor_module_does_not_kill_an_UNRELATED_subcommand(
        self, monkeypatch
    ) -> None:
        """🔴 MEASURED: with `cairn_doctor.py` absent, `cairn ls-entries` — which
        has nothing to do with doctor — died `ModuleNotFoundError`, exit 1, with
        a traceback. `build_parser` calls `_doctor_epilog()` eagerly to build a
        HELP STRING, so an unguarded import there put every verb behind a module
        only one of them needs.

        `cmd_doctor` itself must still fail loudly — that caller asked for it.
        """
        cli = _load_cairn_cli()

        def gone():
            raise ImportError("No module named 'cairn_doctor'")

        monkeypatch.setattr(cli, "_cairn_doctor", gone)
        epilog = cli._doctor_epilog()
        assert "UNAVAILABLE" in epilog and "cairn_doctor" in epilog
        # The parser still builds, so unrelated verbs still parse and dispatch.
        args = cli.build_parser().parse_args(["ls-entries"])
        assert args.func is cli.cmd_ls_entries
        # …and doctor itself does NOT quietly degrade.
        with pytest.raises(ImportError):
            cli.cmd_doctor(cli.build_parser().parse_args(["doctor"]))

    def test_the_help_epilog_carries_the_exit_codes(self) -> None:
        cli = _load_cairn_cli()
        epilog = cli._doctor_epilog()
        for code, _why in cd.EXIT_LEGEND:
            assert f"  {code}  " in epilog
        for state in cd.STATES:
            assert state in epilog

    def test_a_no_sync_run_still_reads_the_LOCAL_config(self, tmp_path, monkeypatch) -> None:
        """🔴 A FIXED DEFECT, PINNED. The first version skipped `load_config`
        under `--no-sync`, so a host with a perfectly good token in
        `~/.config/subsystem-store/env` was reported `token PROBLEM: no token is
        configured`. A check that says PROBLEM about a thing it never looked at
        is the same defect as one that says OK about it."""
        cli = _load_cairn_cli()
        monkeypatch.setenv("SUBSYSTEM_STORE_URL", "https://example.invalid")
        monkeypatch.setenv("SUBSYSTEM_STORE_TOKEN", "a-token-from-the-env")
        monkeypatch.setattr(cli._read_store, "DEFAULT_CACHE_ROOT", tmp_path / "cache")
        args = cli.build_parser().parse_args(["doctor", "--no-sync", "--json"])
        args.cache = tmp_path / "cache"
        import contextlib

        buf = io.StringIO()
        with contextlib.redirect_stdout(buf):
            rc = cli.cmd_doctor(args)
        payload = json.loads(buf.getvalue())
        token_check = [c for c in payload["checks"] if c["name"] == "token"][0]
        assert token_check["state"] == "NOT-OBSERVABLE", token_check
        assert cd.token_fingerprint("a-token-from-the-env") in token_check["detail"]
        assert rc in (9, 10)

    def test_probe_store_NEVER_installs_a_snapshot(self, monkeypatch, tmp_path) -> None:
        """🔴 A diagnostic that repaired the cache as a side effect would destroy
        the staleness it was run to measure — and it would be the one command
        you must not run twice. Graded by watching `install_snapshot`."""
        cli = _load_cairn_cli()
        installed = []
        monkeypatch.setattr(
            cli, "install_snapshot",
            lambda *a, **k: installed.append(a) or 0,
        )

        buf = io.BytesIO()
        with tarfile.open(fileobj=buf, mode="w:gz") as tar:
            data = b"# entry\n"
            info = tarfile.TarInfo("alpha/a.md")
            info.size = len(data)
            tar.addfile(info, io.BytesIO(data))
        body = buf.getvalue()
        headers = Message()
        headers["x-store-entries"] = "1"
        headers["x-store-snapshot"] = "seeded=X newest=Y entry-files=4"
        monkeypatch.setattr(cli, "fetch_snapshot", lambda *a, **k: (body, headers))

        facts = cli.probe_store("https://example.invalid", "tok", timeout=5)
        assert installed == [], "doctor installed a snapshot"
        assert facts.reached is True
        assert facts.visible_entries == 1
        assert facts.store_wide_entries == 4
        assert facts.visible_scopes == ("alpha",)

    def test_a_missing_X_Store_Entries_header_is_UNMEASURED_not_the_archive_count(
        self, monkeypatch, tmp_path
    ) -> None:
        """🔴 REACHABILITY OF THE `visible_entries is None` BRANCH, and a fixed
        defect. `probe_store` first fell back to counting the members it had
        received — which is the side of the comparison a TRUNCATED transfer
        moves, so cache-vs-pod would have agreed with itself and reported OK over
        a short answer. `install_snapshot` cross-checks against this header for
        exactly that reason. Without it, the count is unmeasured."""
        cli = _load_cairn_cli()
        buf = io.BytesIO()
        with tarfile.open(fileobj=buf, mode="w:gz") as tar:
            data = b"# entry\n"
            info = tarfile.TarInfo("alpha/a.md")
            info.size = len(data)
            tar.addfile(info, io.BytesIO(data))
        headers = Message()
        headers["x-store-snapshot"] = "entry-files=9"
        monkeypatch.setattr(cli, "fetch_snapshot", lambda *a, **k: (buf.getvalue(), headers))

        facts = cli.probe_store("https://example.invalid", "tok", timeout=5)
        assert facts.reached is True
        assert facts.visible_entries is None
        assert facts.visible_scopes == ("alpha",)
        cache = _store(tmp_path / "cache", {"alpha": ["a.md"]})
        check = _by_name(_collect(pod=facts, cache_root=cache))["cache-vs-pod"]
        assert check.state == cd.UNMEASURED
        assert "X-Store-Entries" in check.detail

    def test_probe_store_carries_the_HTTP_STATUS_not_a_parsed_message(
        self, monkeypatch
    ) -> None:
        """🔴 STRUCTURAL, NOT TEXTUAL. Before `StoreUnreachable.http_status` the
        only thing separating 401 from a DNS failure was the phrasing of an
        f-string, and a caller that greps a message has pinned a format."""
        cli = _load_cairn_cli()

        def boom(*a, **k):
            raise cli.StoreUnreachable("x answered HTTP 401", http_status=401)

        monkeypatch.setattr(cli, "fetch_snapshot", boom)
        facts = cli.probe_store("https://example.invalid", "tok", timeout=5)
        assert facts.reached is False
        assert facts.http_status == 401

    def test_a_connection_failure_carries_http_status_None(self, monkeypatch) -> None:
        cli = _load_cairn_cli()

        def boom(*a, **k):
            raise cli.StoreUnreachable("x unreachable: nope")

        monkeypatch.setattr(cli, "fetch_snapshot", boom)
        assert cli.probe_store("u", "t", timeout=5).http_status is None

    def test_fetch_snapshot_SETS_the_status_from_a_real_HTTPError(
        self, monkeypatch
    ) -> None:
        """The producing end of the same seam, so neither side is graded alone."""
        cli = _load_cairn_cli()

        def raise_http(*a, **k):
            raise urllib.error.HTTPError(
                "https://example.invalid", 401, "Unauthorized", Message(), io.BytesIO(b"no")
            )

        monkeypatch.setattr(cli.urllib.request, "urlopen", raise_http)
        with pytest.raises(cli.StoreUnreachable) as excinfo:
            cli.fetch_snapshot("https://example.invalid", "tok", scope=None, timeout=5)
        assert excinfo.value.http_status == 401

    def test_the_doctor_subcommand_is_TRACKED_and_the_module_ships(self) -> None:
        """🔴 A new file must be `git add`ed or the flake silently omits it from
        the deploy — the switch succeeds and the module is simply absent, which
        would make `cairn doctor` an ImportError on both hosts."""
        if not (REPO / ".git").exists():
            return
        for rel in ("lib/cairn_doctor.py", "cairn"):
            out = subprocess.run(
                ["git", "-C", str(REPO), "ls-files", "--error-unmatch", "--", rel],
                capture_output=True, text=True,
            )
            assert out.returncode == 0, f"{rel} is not tracked by git\n{out.stderr}"


class TestTheDiskHelpers:
    def test_the_entry_count_matches_the_SERVERS_walk_shape(self, tmp_path) -> None:
        """Depth 2, `*.md`, no dot-directories, no dot-files — the same shape
        `server.snapshot_freshness` counts, so this number and the pod's
        `entry-files=` are answers to one question. Fixture values are chosen so
        no wrong rule coincides: 3 is not the scope count, not the file count,
        and not the depth-any count."""
        root = tmp_path / "s"
        _store(root, {"alpha": ["a.md", "b.md"], "beta": ["c.md"]})
        (root / ".hidden").mkdir()
        (root / ".hidden" / "x.md").write_text("x", encoding="utf-8")
        (root / "alpha" / ".swap.md").write_text("x", encoding="utf-8")
        (root / "alpha" / "notes.txt").write_text("x", encoding="utf-8")
        (root / "alpha" / "deep").mkdir()
        (root / "alpha" / "deep" / "d.md").write_text("x", encoding="utf-8")
        (root / "top.md").write_text("x", encoding="utf-8")
        assert cd.store_entry_files(root) == 3

    def test_the_scope_list_skips_dot_directories_and_files(self, tmp_path) -> None:
        root = tmp_path / "s"
        _store(root, {"alpha": ["a.md"], "beta": []}, stamp="synced=1\n")
        (root / ".git").mkdir()
        assert cd.store_scopes(root) == ("alpha", "beta")

    def test_an_unreadable_root_RAISES_rather_than_counting_zero(self, tmp_path) -> None:
        """`Path.rglob` swallows a permission error and yields nothing, so an
        unreadable store would report `0` — the exact confusion
        `server.snapshot_freshness` uses `os.walk(onerror=…)` to avoid."""
        blocked = tmp_path / "b"
        blocked.mkdir()
        blocked.chmod(0o000)
        try:
            with pytest.raises(OSError):
                cd.store_entry_files(blocked)
        finally:
            blocked.chmod(0o700)


class TestAnUnconfiguredMirrorDoesNotCrashTheVisibilityCheck:
    """🔴 REGRESSION: `doctor` died on every default deployment.

    `mirror_root` became optional when the frozen mirror became configurable
    (`CAIRN_MIRROR_ROOT`, unset by default). The `frozen-mirror` check was
    widened to accept `None`; `_visibility_check` was NOT, so it handed `None`
    to `_describe` and `doctor` died with

        AttributeError: 'NoneType' object has no attribute 'iterdir'

    — zero stdout, exit 1 — whenever no mirror was configured AND the cache
    root existed. That is the ORDINARY state of a fresh install, so the command
    was broken for every user who had run `cairn sync` and never migrated from
    a pre-cutover local store.

    🔴 IT SURVIVED BECAUSE EVERY EXISTING TEST PINNED THE DIMENSION. `_collect`
    defaults `mirror_root` to a real path, and the packaged client's build-time
    check runs in a nix sandbox whose HOME has no cache root — so the crash
    needed BOTH `mirror_root=None` AND a cache that exists, and no fixture
    combined them. Reaching it is the whole point of this class.
    """

    def _cache_with_a_scope(self, tmp_path):
        cache = tmp_path / "cache"
        (cache / "alpha").mkdir(parents=True)
        (cache / "alpha" / "widget.md").write_text("# widget\n", encoding="utf-8")
        return cache

    def test_collect_does_not_raise_when_no_mirror_is_configured(self, tmp_path):
        """The crash itself: a real cache root plus `mirror_root=None`."""
        cache = self._cache_with_a_scope(tmp_path)
        checks = _by_name(_collect(
            resolved_root=cache, cache_root=cache, mirror_root=None,
            pod=cd.PodFacts(reached=True, visible_entries=1, store_wide_entries=1,
                            visible_scopes=("alpha",), snapshot_header="entry-files=1"),
        ))
        assert "token-scopes" in checks

    def test_an_unconfigured_mirror_is_not_reported_as_an_unreadable_root(self, tmp_path):
        """🔴 THE FIX MUST NOT OVERSHOOT, AND THIS IS THE HALF THAT CATCHES IT.

        Folding `None` into the "unreadable local root" list would stop the
        crash and downgrade the check to UNMEASURED on every default
        deployment — a false alarm forever, which is the same defect pointed
        the other way. An unconfigured mirror contributes no scopes and is not
        a hole in coverage, so with the pod's scopes matching the cache's this
        must be a clean OK.
        """
        cache = self._cache_with_a_scope(tmp_path)
        check = _by_name(_collect(
            resolved_root=cache, cache_root=cache, mirror_root=None,
            pod=cd.PodFacts(reached=True, visible_entries=1, store_wide_entries=1,
                            visible_scopes=("alpha",), snapshot_header="entry-files=1"),
        ))["token-scopes"]
        assert check.state == cd.OK, f"expected OK, got {check.state}: {check.detail}"
        assert "unreadable" not in check.detail.lower()

    def test_a_scope_the_token_cannot_reach_is_still_reported_without_a_mirror(self, tmp_path):
        """The check must still DO its job with no mirror — not merely not crash.

        A fix that returned early on `mirror_root is None` would pass both
        tests above while silently disabling the visibility check for every
        default deployment. This is the case that separates those.
        """
        cache = self._cache_with_a_scope(tmp_path)
        (cache / "beta").mkdir()
        (cache / "beta" / "thing.md").write_text("# thing\n", encoding="utf-8")
        check = _by_name(_collect(
            resolved_root=cache, cache_root=cache, mirror_root=None,
            pod=cd.PodFacts(reached=True, visible_entries=1, store_wide_entries=2,
                            visible_scopes=("alpha",), snapshot_header="entry-files=1"),
        ))["token-scopes"]
        assert check.state == cd.PROBLEM
        assert "beta" in check.detail


class TestAnExtraInstanceRenamesEveryCheck:
    """🔴 THE SECOND OF THE TWO POINTS `_pin_the_hosts_configuration` IS MEASURED
    AT, AND THE ONE THAT MAKES THAT FIXTURE LOAD-BEARING RATHER THAN DECORATIVE.

    The fixture pins the host's configuration to an EMPTY directory, which is the
    single-instance boundary. A guard that only ever measures the boundary cannot
    tell a fixture that pins the dimension from a fixture that pins nothing and is
    lucky — both are green on a host whose HOME happens to be empty. So this class
    writes a second instance INTO the pinned directory and watches the check names
    move, which is the exact mechanism that made
    `test_a_no_sync_run_still_reads_the_LOCAL_config` raise `IndexError` when the
    directory being read was the operator's own.

    ⚠ IT IS NOT A CLAIM THAT THE PREFIX IS WRONG. `_doctor_instance` renames on
    purpose — one reachable store must not make a second one that is down look
    measured. The defect was never the prefix; it was a test that could not say
    which world it was running in.
    """

    @staticmethod
    def _doctor_json(cli, monkeypatch, tmp_path, argv=("doctor", "--no-sync", "--json")):
        """Run `cmd_doctor` against the pinned world and return its parsed report.

        No `--cache`: an explicit one is refused at exit 2 on a multi-instance
        fan-out (`_refuse_shared_cache`), which would compare equal in both arms
        and measure nothing. Repointing `DEFAULT_CACHE_ROOT` moves EVERY
        instance's root together — `cache_root_for` reads it at call time — so
        both arms read tmp disk and neither touches the operator's cache.
        """
        import contextlib

        monkeypatch.setattr(cli._read_store, "DEFAULT_CACHE_ROOT", tmp_path / "cache")
        args = cli.build_parser().parse_args(list(argv))
        buf = io.StringIO()
        with contextlib.redirect_stdout(buf):
            rc = cli.cmd_doctor(args)
        return json.loads(buf.getvalue()), rc

    def test_the_boundary_an_empty_config_dir_leaves_every_name_BARE(
        self, tmp_path, monkeypatch
    ) -> None:
        cli = _load_cairn_cli()
        monkeypatch.setenv("SUBSYSTEM_STORE_URL", "https://example.invalid")
        monkeypatch.setenv("SUBSYSTEM_STORE_TOKEN", "a-token-from-the-env")
        payload, _rc = self._doctor_json(cli, monkeypatch, tmp_path)
        assert tuple(c["name"] for c in payload["checks"]) == SINGLE_INSTANCE_CHECKS

    def test_a_second_instance_prefixes_EVERY_name_with_its_alias(
        self, tmp_path, monkeypatch, _pin_the_hosts_configuration
    ) -> None:
        """The other point. One `.env` in the pinned `instances/` directory and
        every name grows an alias — including the DEFAULT instance's, which is
        what a test looking `token` up by its bare name cannot survive."""
        cli = _load_cairn_cli()
        monkeypatch.setenv("SUBSYSTEM_STORE_URL", "https://example.invalid")
        monkeypatch.setenv("SUBSYSTEM_STORE_TOKEN", "a-token-from-the-env")
        mirror = tmp_path / "frozen-mirror" / "alpha"
        mirror.mkdir(parents=True)
        (mirror / "a.md").write_text("# a\n", encoding="utf-8")
        (mirror / "a.md").chmod(0o444)
        monkeypatch.setenv("CAIRN_MIRROR_ROOT", str(tmp_path / "frozen-mirror"))
        instances = _pin_the_hosts_configuration / "instances"
        instances.mkdir()
        (instances / "beta.env").write_text(
            "SUBSYSTEM_STORE_URL=https://beta.example.invalid\n"
            "SUBSYSTEM_STORE_TOKEN=a-token-for-beta\n",
            encoding="utf-8",
        )
        payload, _rc = self._doctor_json(cli, monkeypatch, tmp_path)
        names = tuple(c["name"] for c in payload["checks"])
        by_name = {c["name"]: c for c in payload["checks"]}

        # The default instance is `personal`, and it is prefixed too: the alias
        # answers "which store", and omitting it for the default would make the
        # bare name mean two different things on two hosts.
        assert "token" not in names, names
        assert tuple(n for n in names if n.startswith("personal/")) == tuple(
            f"personal/{c}" for c in SINGLE_INSTANCE_CHECKS
        ), names
        assert tuple(n for n in names if n.startswith("beta/")) == tuple(
            f"beta/{c}" for c in SINGLE_INSTANCE_CHECKS
        ), names

        # 🔴 EVERY ROW IS RENAMED AND NO ROW IS DROPPED — the two halves of the
        # claim, and the second is why the sets are compared rather than counted.
        # ⚠ `frozen-mirror` was WRITTEN here as `beta` carrying one row fewer,
        # reasoning from `_doctor_instance`'s "the frozen mirror belongs to the
        # default instance ALONE" comment; measured, beta carries the row too and
        # what belongs to the default alone is the mirror's CONTENT. So the
        # discriminator is the STATE, not the row count, and it needs a mirror
        # actually configured — with `$CAIRN_MIRROR_ROOT` unset both sides read
        # NOT-OBSERVABLE and this assertion would pass on a build that reported
        # the default's mirror under every alias.
        assert by_name["personal/frozen-mirror"]["state"] == cd.OK, by_name[
            "personal/frozen-mirror"
        ]
        assert by_name["beta/frozen-mirror"]["state"] == cd.NOT_OBSERVABLE, by_name[
            "beta/frozen-mirror"
        ]
