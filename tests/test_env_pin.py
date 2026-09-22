#!/usr/bin/env python3
"""`testlib.env_pin` — the one definition of the client's configuration surface.

🔴 WHY THIS FILE EXISTS: THE MECHANISM SHIPPED WITHOUT A GUARD, AND THE MUTANT
SURVIVED THE ENTIRE SUITE. `env_pin` was added to stop seven suites inheriting
the developer's environment, and no test imported it. An audit removed its
clear-first loop — restoring the `dict(os.environ)` + `update()` the module
exists to replace — and measured **1972 passed, rc 0**: identical to the
unmutated tree. So the three lines the module is for could be deleted and all
four CI jobs stayed green, which is the definition of an unguarded property.

That mutant is now killed by `test_a_stray_client_variable_is_CLEARED` below.

⚠ WHAT THIS FILE IS NOT. It does not re-test the seven consumers; each of those
has its own suite. It pins the PREDICATE and the ORDERING, which is what the
consumers assume and what nothing else looks at.
"""

from __future__ import annotations

import ast
import os
import sys
import textwrap
from pathlib import Path

REPO = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(REPO / "lib"))

import host_identity  # noqa: E402
from testlib import env_pin  # noqa: E402

#: Files that define the mechanism rather than consume it.
_NOT_CONSUMERS = ("tests/testlib/env_pin.py", "tests/test_env_pin.py")


def _is_environ(node: ast.AST) -> bool:
    """`os.environ` (or any `<x>.environ`), as an expression."""
    return isinstance(node, ast.Attribute) and node.attr == "environ"


def _environ_copy_lines(path: Path) -> list[int]:
    """Every line where `path` COPIES the environment, by AST.

    🔴 STRUCTURAL, BECAUSE THE SPELLED VERSION WAS MEASURABLY WALKABLE. The first
    draft grepped for `dict(os.environ)` and `os.environ.copy()`. An audit
    reverted a consumer off `env_pin` the way a person would — restoring the copy
    spelled `{**os.environ, …}` — and **all 14 tests passed**. Three further
    spellings walked it. So the shapes are enumerated against the AST, and
    `test_the_copy_detector_sees_every_spelling` feeds it each one: a spelling
    this function cannot see is a RED test rather than a silent pass.

    ⚠ It reports COPIES, not reads. `os.environ.get(...)`, `os.environ[...]` and
    `os.environ` passed to `monkeypatch.delenv` are not copies and are not
    flagged — a consumer legitimately reads the environment.
    """
    tree = ast.parse(path.read_text(encoding="utf-8"), filename=str(path))
    hits: list[int] = []
    for node in ast.walk(tree):
        # `{**os.environ, ...}` — a dict display with `os.environ` splatted in.
        if isinstance(node, ast.Dict) and any(
            k is None and _is_environ(v) for k, v in zip(node.keys, node.values)
        ):
            hits.append(node.lineno)
        elif isinstance(node, ast.Call):
            func = node.func
            # `os.environ.copy()`
            if isinstance(func, ast.Attribute) and func.attr == "copy" and _is_environ(
                func.value
            ):
                hits.append(node.lineno)
            # `dict(os.environ)` / `dict(**os.environ)` / `copy.copy(os.environ)`
            elif (isinstance(func, ast.Name) and func.id == "dict") or (
                isinstance(func, ast.Attribute) and func.attr in ("copy", "deepcopy")
            ):
                if any(_is_environ(a) for a in node.args) or any(
                    kw.arg is None and _is_environ(kw.value) for kw in node.keywords
                ):
                    hits.append(node.lineno)
    return sorted(set(hits))


def _imports_env_pin(path: Path) -> bool:
    """Does `path` actually IMPORT `env_pin`?

    🔴 AN IMPORT, NEVER A SUBSTRING. The first draft asked whether the text
    `env_pin` appeared anywhere in the file, so a consumer that had been reverted
    off the module stayed in the discovered set on the strength of a surviving
    COMMENT — measured, and the SHRINK direction the ledger exists for never
    fired.
    """
    tree = ast.parse(path.read_text(encoding="utf-8"), filename=str(path))
    for node in ast.walk(tree):
        if isinstance(node, ast.ImportFrom) and any(
            a.name == "env_pin" for a in node.names
        ):
            return True
        if isinstance(node, ast.Import) and any(
            a.name.split(".")[-1] == "env_pin" for a in node.names
        ):
            return True
    return False


def _walk_consumers() -> list[tuple[str, Path]]:
    """Every file under `tests/` that imports `env_pin`."""
    return [
        (path.relative_to(REPO).as_posix(), path)
        for path in sorted((REPO / "tests").rglob("*.py"))
        if path.relative_to(REPO).as_posix() not in _NOT_CONSUMERS
        and _imports_env_pin(path)
    ]


class TestTheInstrumentsAreValidated:
    """🔴 VALIDATE THE INSTRUMENT BEFORE READING ITS VERDICT.

    The two ledgers below this class are only as good as the two walkers above
    it, and the spelled versions of both were measurably walkable: a consumer
    reverted off `env_pin` stayed in the discovered set on a surviving COMMENT,
    and a raw copy spelled `{**os.environ}` was invisible. A reassuring zero from
    a walker wired to nothing is indistinguishable from a clean tree.
    """

    #: Every way this tree could copy the environment. The detector must see all
    #: of them; the negative cases must not be flagged.
    COPY_SPELLINGS = (
        "env = dict(os.environ)",
        "env = os.environ.copy()",
        "env = {**os.environ}",
        'env = {**os.environ, "A": "b"}',
        "env = dict(**os.environ)",
        "env = copy.copy(os.environ)",
        "env = copy.deepcopy(os.environ)",
    )
    NOT_COPIES = (
        'v = os.environ.get("X")',
        'v = os.environ["X"]',
        'monkeypatch.delenv("X", raising=False)',
        "env = dict(a=1)",
        'v = something.get("X")',
    )

    def test_the_copy_detector_sees_every_spelling(self, tmp_path) -> None:
        for src in self.COPY_SPELLINGS:
            p = tmp_path / "probe.py"
            p.write_text(textwrap.dedent(f"import copy, os\n{src}\n"), encoding="utf-8")
            assert _environ_copy_lines(p), f"NOT DETECTED: {src}"

    def test_the_copy_detector_does_not_flag_a_READ(self, tmp_path) -> None:
        """The other half — a detector that flagged everything would make the
        exemption mapping grow until it covered the tree."""
        for src in self.NOT_COPIES:
            p = tmp_path / "probe.py"
            p.write_text(
                textwrap.dedent(f"import copy, os\nmonkeypatch = something = None\n{src}\n"),
                encoding="utf-8",
            )
            assert not _environ_copy_lines(p), f"FALSELY FLAGGED: {src}"

    def test_the_consumer_walker_needs_an_IMPORT_not_a_mention(self, tmp_path) -> None:
        """The measured revert: the module named only in a comment."""
        mention = tmp_path / "mention.py"
        mention.write_text("# env_pin used to be imported here\nx = 1\n", encoding="utf-8")
        real = tmp_path / "real.py"
        real.write_text("from testlib import env_pin\n", encoding="utf-8")
        assert not _imports_env_pin(mention)
        assert _imports_env_pin(real)


class TestThePredicate:
    def test_a_swept_prefix_is_client_config(self) -> None:
        assert env_pin.is_client_config("SUBSYSTEM_STORE_URL")
        assert env_pin.is_client_config("CAIRN_ROUTES")
        # …including a name nobody has added yet, which is the whole argument
        # for a prefix over a ledger of names.
        assert env_pin.is_client_config("CAIRN_SOMETHING_INVENTED_LATER")

    def test_a_HARNESS_knob_is_NOT_client_config(self) -> None:
        """🔴 The exemption, which reaches no consumer today and is kept anyway.

        `testlib/store_siting.py` reads `CAIRN_TEST_TMPFS` at call time; clearing
        it would reconfigure the HARNESS rather than the client. Asserted here so
        the exemption cannot be dropped as dead weight without a test going red.
        """
        assert not env_pin.is_client_config("CAIRN_TEST_TMPFS")

    def test_an_unrelated_variable_is_untouched(self) -> None:
        for name in ("PATH", "HOME", "LANG", "PYTHONDONTWRITEBYTECODE"):
            assert not env_pin.is_client_config(name), name

    def test_the_HOST_LABEL_names_are_covered_and_TWO_carry_NO_swept_prefix(
        self,
    ) -> None:
        """🔴 THE CLAIM THAT WAS FALSE, PINNED SO IT CANNOT GO FALSE AGAIN.

        The module's first draft asserted — in a paragraph labelled MEASURED —
        that a prefix sweep cleared all three of `host_identity.HOST_LABEL_ENV`.
        Two of them carry no swept prefix, so it cleared one. Worse, it INVERTED
        a behaviour: clearing `CAIRN_HOST` while leaving `ASIB_HOST` makes the
        latter authoritative in `host_label()`, where the inherited `CAIRN_HOST`
        had masked it — putting the operator's real machine name into a PUBLIC
        repository's logs.

        The second assertion is the load-bearing one: it fails if somebody
        "simplifies" `EXTRA_CONFIG_ENV` away on the belief that the prefixes
        already cover it. Its own positive control is the first — if every name
        DID carry a prefix, the second would be vacuous and says so.
        """
        assert host_identity.HOST_LABEL_ENV, "discovery returned nothing"
        no_prefix = [
            n for n in host_identity.HOST_LABEL_ENV
            if not n.startswith(env_pin.CONFIG_ENV_PREFIXES)
        ]
        assert no_prefix, (
            "every HOST_LABEL_ENV name now carries a swept prefix, which makes "
            "the assertion below vacuous. If that is a real change, `EXTRA_CONFIG_ENV` "
            "may be retired — but decide it, do not let this test stop measuring."
        )
        for name in host_identity.HOST_LABEL_ENV:
            assert env_pin.is_client_config(name), name


class TestSanitizedEnv:
    def test_a_stray_client_variable_is_CLEARED(self, monkeypatch) -> None:
        """🔴 THE KILLER FOR THE SURVIVING MUTANT. Restore the pre-`env_pin`
        `dict(os.environ)` + `update()` and this goes red; nothing else in the
        suite did."""
        monkeypatch.setenv("CAIRN_A_SIXTH_THING", "leaked")
        monkeypatch.setenv("SUBSYSTEM_STORE_ROUTES_ISH", "leaked")
        env = env_pin.sanitized_env(SUBSYSTEM_STORE_URL="http://pinned")
        assert "CAIRN_A_SIXTH_THING" not in env
        assert "SUBSYSTEM_STORE_ROUTES_ISH" not in env

    def test_the_HARNESS_knob_SURVIVES(self, monkeypatch) -> None:
        monkeypatch.setenv("CAIRN_TEST_TMPFS", "/keep/me")
        assert env_pin.sanitized_env()["CAIRN_TEST_TMPFS"] == "/keep/me"

    def test_an_override_is_APPLIED_over_an_inherited_value(self, monkeypatch) -> None:
        monkeypatch.setenv("SUBSYSTEM_STORE_URL", "http://the-operators-real-store")
        env = env_pin.sanitized_env(SUBSYSTEM_STORE_URL="http://pinned")
        assert env["SUBSYSTEM_STORE_URL"] == "http://pinned"

    def test_an_UNRELATED_variable_is_INHERITED(self) -> None:
        """A subprocess needs an interpreter it can find. Clearing everything
        would be a different and much worse bug than the one this closes."""
        assert env_pin.sanitized_env().get("PATH") == os.environ.get("PATH")

    def test_the_real_environment_is_NOT_mutated(self, monkeypatch) -> None:
        """It returns a copy. A helper that cleared the live `os.environ` would
        reconfigure the test process itself, and every later test in the run."""
        monkeypatch.setenv("CAIRN_A_SIXTH_THING", "still-here")
        env_pin.sanitized_env()
        assert os.environ["CAIRN_A_SIXTH_THING"] == "still-here"


class TestSanitizedEnvWith:
    def test_the_keyword_overrides_WIN_over_the_base_mapping(self) -> None:
        """The precedence `tests/test_cairn_instances.py` depends on, and which
        the first rewrite of that call site got backwards — undetectably, because
        no caller passes a colliding key today."""
        env = env_pin.sanitized_env_with(
            {"SUBSYSTEM_STORE_URL": "from-base"}, SUBSYSTEM_STORE_URL="from-override"
        )
        assert env["SUBSYSTEM_STORE_URL"] == "from-override"

    def test_a_key_in_BOTH_does_not_raise(self) -> None:
        """⚠ THE DEFECT THIS FUNCTION EXISTS FOR. `f(HOME=..., **extra)` raises
        `TypeError: got multiple values for keyword argument` when `extra` also
        carries `HOME` — turning a working `env.update(extra)` override into a
        crash. Reached by no caller, which is why only a direct test finds it."""
        env = env_pin.sanitized_env_with({"HOME": "/base"}, HOME="/override")
        assert env["HOME"] == "/override"


class TestTheConsumerSetIsPinned:
    """🔴 A LEDGER OF WHO CONSUMES THIS, FAILING ON GROW **OR** SHRINK.

    The module's own docstring twice carried a wrong count — "four sites" when
    there were five, then five when there were seven — and each time the omitted
    site was one with a live defect. A count in prose that nothing asserts on is
    the repo's own named failure shape; this is the assertion.

    GROW matters because a new consumer means the docstring's list is stale.
    SHRINK matters because a consumer reverting to `dict(os.environ)` is exactly
    the regression `env_pin` exists to prevent, and it would otherwise be silent.
    """

    #: Every file that consumes `env_pin`, discovered against this literal.
    EXPECTED = {
        "tests/parity/harness.py",
        "tests/test_cairn_cli.py",
        "tests/test_cairn_doctor.py",
        # ⚠ ADDED BY THE `SUBSYSTEM_STORE_*` -> `CAIRN_*` RENAME, and it is the
        # ledger working rather than a rubber stamp: this test went RED on the
        # new file's first full run. It drives the client end-to-end over
        # deliberately-deprecated names, so it needs exactly the hermetic
        # environment `env_pin` defines — a `dict(os.environ)` there would let
        # the operator's own `CAIRN_URL` decide which store the run reached.
        "tests/test_env_aliases.py",
        "tests/test_cairn_instances.py",
        "tests/test_cairn_write.py",
        "tests/unchanged_output_capture.py",
    }

    def test_the_consumer_set_is_exactly_these_files(self) -> None:
        found = {rel for rel, _ in _walk_consumers()}
        # Validate the instrument before reading its verdict: a walker that found
        # nothing would satisfy neither direction by accident, but it would make
        # the SHRINK message unreadable.
        assert found, "the walk found no consumers at all, which cannot be right"
        assert found == self.EXPECTED, (
            f"the consumer set moved. GREW by {sorted(found - self.EXPECTED)}; "
            f"SHRANK by {sorted(self.EXPECTED - found)}. A new consumer means "
            f"`env_pin`'s docstring list is stale; a lost one usually means a "
            f"site went back to `dict(os.environ)`, which is the regression this "
            f"module exists to prevent."
        )

    #: Consumers that ALSO build a raw environment copy, each with the reason.
    #:
    #: 🔴 A MAPPING AND NOT A SET, SO AN EXEMPTION COSTS A SENTENCE. Both entries
    #: are SERVER-environment sites, which are a genuinely different predicate:
    #: they pin `SUBSYSTEM_STORE_TRUSTED_PROXIES` and the limiter settings, which
    #: configure the pod rather than the client, and `is_client_config` would
    #: sweep them by prefix while meaning something else by it.
    #:
    #: ⚠ THE SECOND ENTRY WAS FOUND BY THIS TEST ON ITS FIRST RUN, which is the
    #: only reason it is written down rather than being an eighth silent copy.
    #: Both inherit `SUBSYSTEM_STORE_FAILURE_WINDOW_S`/`_LOCKOUT_S`; a bad value
    #: makes the server REFUSE TO START and name the variable, so the failure is
    #: self-diagnosing — which is what makes this a defensible scope call rather
    #: than the same defect one predicate over. Consolidating the server sites is
    #: separate work; it is not done here and is not pretended to be.
    RAW_COPY_EXEMPT = {
        "tests/parity/harness.py": "start_oracle builds the SERVER's environment",
        "tests/unchanged_output_capture.py": "start_store builds the SERVER's environment",
    }

    def test_no_consumer_still_builds_a_RAW_environment_copy(self) -> None:
        """The other half: a file can import `env_pin` and still hand-roll a
        second copy beside it, which is how seven copies happened in the first
        place."""
        assert all(self.RAW_COPY_EXEMPT.values()), "an exemption with no reason"
        offenders = []
        for rel in sorted(self.EXPECTED - set(self.RAW_COPY_EXEMPT)):
            if _environ_copy_lines(REPO / rel):
                offenders.append(rel)
        assert not offenders, (
            f"these consume `env_pin` AND build a raw environment copy: {offenders}. "
            f"One of the two is the real predicate; a second one beside it is how "
            f"the seven copies happened. If it is a SERVER environment, add it to "
            f"`RAW_COPY_EXEMPT` with the reason — do not delete this assertion."
        )

    def test_every_exemption_still_HAS_a_raw_copy(self) -> None:
        """🔴 THE SHRINK DIRECTION, WITHOUT WHICH THE MAPPING ABOVE ROTS. An
        exemption for a file that no longer builds a raw copy reads as coverage
        and provides none — the dead-allowlist-entry shape `internal/depspolicy`
        refuses on the Go side, and the reason its allowlist fails on SHRINK as
        well as GROW."""
        dead = [
            rel for rel in sorted(self.RAW_COPY_EXEMPT)
            if not _environ_copy_lines(REPO / rel)
        ]
        assert not dead, (
            f"these are exempted from the raw-copy rule and no longer break it: "
            f"{dead}. Drop the exemption — it is now a sentence saying a hazard "
            f"exists where it does not."
        )
