"""The `SUBSYSTEM_STORE_*` → `CAIRN_*` ledger, pinned ACROSS THE TWO SPELLINGS.

🔴 WHY TWO SPELLINGS EXIST AT ALL, AND WHY A GATE IS THE ANSWER RATHER THAN A FIX.
`packages.cairn` installs the Python client script and `lib/` under `libexec` and nothing
else, so `lib/env_aliases.py` CANNOT import `internal/envalias`. That is packaging, not a
design choice anybody may undo here. `AGENTS.md` already blesses this shape for
`server/Dockerfile` against `flake.nix`'s `serverEnv` — *"the runtime contract is written
in both, so `tests/test_flake_image_matches_dockerfile.py` pins them against each other
and goes red when one moves alone"* — and this file is the same instrument for the same
reason.

## What it pins, and what it structurally cannot see

It pins the PAIR SET (failing when it GROWS *or* SHRINKS), the removal anchor, and the two
warning texts compared **as whole normalised strings**. 🔴 THE WHOLE STRING, NOT KEYWORDS:
when the artifact under test is prose, a guard on words is walkable by rewording, and a
reworded warning on one side is exactly the drift this exists to catch. A cosmetic reword
therefore fails this test — that cost is paid on purpose, for a machine-readable claim.

⚠ IT READS THE GO SIDE AS TEXT, so it is blind to one thing: a Go-side ARGUMENT-ORDER
mistake that still renders well-formed English (`EnvWarning` passing `New, Old, Old`, say).
The extractor knows the template but not the substitution. **`tests/parity/harness.py` is
what closes that** — it runs both real clients with a deprecated name exported and diffs
their stderr byte-for-byte, which no amount of source reading can substitute for. Said
here rather than left to be discovered, because a reader who took this file for full
coverage would stop looking.
"""

from __future__ import annotations

import re
import subprocess
import sys
from pathlib import Path

import pytest

REPO = Path(__file__).resolve().parents[1]
GO_SOURCE = REPO / "internal" / "envalias" / "envalias.go"
CAIRN_CLI = REPO / "cairn"

sys.path.insert(0, str(REPO / "lib"))
sys.path.insert(0, str(Path(__file__).resolve().parent))

import env_aliases  # noqa: E402
from testlib import env_pin  # noqa: E402


# --- reading the GO spelling -------------------------------------------------
#
# 🔴 A PARSER, NOT A `str.find`. The Go constants are written as concatenations across
# source lines (`"…" + RemovalAnchor + "."`), which is how gofmt wants a long string, so
# "take the text between two quotes" reads ONE fragment and silently compares a third of
# the sentence. `_go_concatenation` walks the tokens instead and resolves the identifier.

_PAIR = re.compile(r'\{New:\s*"([A-Z0-9_]+)",\s*Old:\s*"([A-Z0-9_]+)"\}')
_ANCHOR = re.compile(r'const RemovalAnchor = "([^"]*)"')
_TOKEN = re.compile(r'"((?:[^"\\]|\\.)*)"|\b(RemovalAnchor)\b')


def _go_ledger(source: str) -> tuple[tuple[str, str], ...]:
    """Every `(new, old)` pair declared in the Go ledger, in source order."""
    return tuple(_PAIR.findall(source))


def _go_anchor(source: str) -> str:
    match = _ANCHOR.search(source)
    assert match, "the Go source declares no RemovalAnchor — the extractor is reading the wrong file"
    return match.group(1)


def _go_concatenation(source: str, const_name: str) -> str:
    """The value of a Go string constant written as a `+` concatenation."""
    start = source.index(f"{const_name} = ") + len(f"{const_name} = ")
    # A constant's value ends at the next line that is not a continuation: either the next
    # constant in the block, or the block's closing paren.
    rest = source[start:]
    end = len(rest)
    for terminator in ("\n\tenvWarningFormat", "\n\tfileWarningFormat", "\n)"):
        found = rest.find(terminator)
        if found != -1:
            end = min(end, found)
    body = rest[:end]
    anchor = _go_anchor(source)
    out: list[str] = []
    for literal, identifier in _TOKEN.findall(body):
        out.append(anchor if identifier else literal.encode().decode("unicode_escape"))
    return "".join(out)


def _go_env_warning(source: str, new: str, old: str) -> str:
    """The Go env warning for one pair, rendered from the extracted template.

    The `%s` order mirrors `EnvWarning`'s call; see this module's docstring for the one
    thing that makes blind and what covers it instead.
    """
    return _go_concatenation(source, "envWarningFormat") % (old, new, new)


def _go_file_warning(source: str, new: str, old: str, path: str) -> str:
    return _go_concatenation(source, "fileWarningFormat") % (old, path, new, new)


@pytest.fixture(scope="module")
def go_source() -> str:
    return GO_SOURCE.read_text(encoding="utf-8")


# --- the instrument, before its verdict --------------------------------------


class TestTheExtractorIsAnInstrument:
    """🔴 VALIDATE THE INSTRUMENT BEFORE READING ITS VERDICT.

    Every assertion below this class is a comparison against something the extractor
    produced. A regex that matched NOTHING — a renamed constant, a reformatted literal, a
    `gofmt` that split a line differently — yields an empty template and an empty ledger,
    and an empty set compares equal to nothing in a way that reads as agreement. So: watch
    the numbers move before quoting a zero.
    """

    def test_it_finds_a_non_empty_ledger(self, go_source: str) -> None:
        pairs = _go_ledger(go_source)
        assert len(pairs) == len(env_aliases.LEDGER) > 0, (
            "the Go ledger extractor returned "
            f"{len(pairs)} pairs against Python's {len(env_aliases.LEDGER)}"
        )

    def test_it_resolves_the_whole_concatenated_template(self, go_source: str) -> None:
        # The POSITIVE CONTROL on the concatenation walk: the template spans more than one
        # source fragment, so a reader that took only the first would come back short. The
        # anchor lives in the LAST fragment, and `%s` in the first.
        template = _go_concatenation(go_source, "envWarningFormat")
        assert template.count("%s") == 3, template
        assert env_aliases.REMOVAL_ANCHOR in template, template

    def test_a_mutated_go_source_moves_the_answer(self, go_source: str) -> None:
        # The NEGATIVE CONTROL. If this comparison could not go red, the agreement
        # assertions below would be a fact about the harness and not about the two files.
        mutated = go_source.replace("is a deprecated alias for", "is an obsolete name for")
        assert mutated != go_source, "the mutation did not apply — the control is inert"
        assert _go_concatenation(mutated, "envWarningFormat") != _go_concatenation(
            go_source, "envWarningFormat"
        )

        dropped = go_source.replace('{New: "CAIRN_URL", Old: "SUBSYSTEM_STORE_URL"},\n', "")
        assert dropped != go_source, "the mutation did not apply — the control is inert"
        assert len(_go_ledger(dropped)) == len(_go_ledger(go_source)) - 1


# --- the gate ----------------------------------------------------------------


class TestTheTwoSpellingsAgree:
    def test_the_pair_set_is_identical(self, go_source: str) -> None:
        """🔴 FAILS WHEN THE SET GROWS *OR* SHRINKS, which are different defects.

        A pair added on one side only is a variable that works in one language; a pair
        REMOVED on one side only is a deprecated name that silently stops resolving for
        half the deployment. Comparing sets catches both; comparing lengths catches
        neither when one adds and the other removes.
        """
        assert set(_go_ledger(go_source)) == set(env_aliases.LEDGER)

    def test_the_order_is_identical(self, go_source: str) -> None:
        # The ORDER is load-bearing: both sides emit warnings sorted by new name, and
        # `tests/parity/harness.py` diffs the two clients' stderr byte-for-byte.
        assert _go_ledger(go_source) == env_aliases.LEDGER

    def test_the_removal_anchor_is_identical(self, go_source: str) -> None:
        assert _go_anchor(go_source) == env_aliases.REMOVAL_ANCHOR

    def test_every_rendered_env_warning_is_byte_identical(self, go_source: str) -> None:
        for new, old in env_aliases.LEDGER:
            assert _go_env_warning(go_source, new, old) == env_aliases.env_warning(new, old)

    def test_every_rendered_file_warning_is_byte_identical(self, go_source: str) -> None:
        path = "/tmp/synthetic/config/env"
        for new, old in env_aliases.LEDGER:
            assert _go_file_warning(go_source, new, old, path) == env_aliases.file_warning(
                new, old, path
            )


class TestTheLedgerItself:
    def test_no_new_name_collides_with_a_live_variable(self) -> None:
        """🔴 THE `CAIRN_HOST` COLLISION, PINNED SO IT CANNOT BE RE-INTRODUCED.

        `CAIRN_HOST` is the human-readable machine LABEL — `host_identity.HOST_LABEL_ENV`'s
        first entry, read by `host_label()` and rendered into client output — while
        `SUBSYSTEM_STORE_HOST` is the pod's LISTEN ADDRESS. The mechanical prefix swap
        would have pointed a pod's `bind()` at an operator's machine label AND let a listen
        address hijack the label that lands in rendered output. `CAIRN_LISTEN_HOST` is the
        answer, and this is the guard that keeps a later "tidy-up" from undoing it.

        ⚠ AN INVARIANT GUARD, NOT REGRESSION COVERAGE. The collision was caught while
        choosing the names; no shipped code ever had it.
        """
        from host_identity import HOST_LABEL_ENV

        taken = set(HOST_LABEL_ENV) | {"CAIRN_ROUTES", "CAIRN_CACHE_ROOT", "CAIRN_MIRROR_ROOT"}
        for new, _old in env_aliases.LEDGER:
            assert new not in taken, (
                f"{new} is already a live variable meaning something else; a rename onto "
                "it makes two features read one name"
            )

    def test_the_removal_anchor_carries_no_date(self) -> None:
        """The window is anchored to a MILESTONE a reader can check, never to a date.

        `CHANGELOG.md` states the rule for the repository — *"a date says when somebody
        looked; a sha says which tree they looked at"* — and `tests/leakscan.py`'s
        `_DATED_STAMP` rule would refuse the commit besides. There is no semver here to
        hang a version on: `flake.nix` sets `version = self.shortRev`.
        """
        assert not re.search(r"\b(19|20)\d\d\b", env_aliases.REMOVAL_ANCHOR)
        assert "packages.cairn" in env_aliases.REMOVAL_ANCHOR


class TestTheResolver:
    def test_the_new_name_wins(self) -> None:
        env = {"CAIRN_URL": "new", "SUBSYSTEM_STORE_URL": "old"}
        assert env_aliases.value(env, "CAIRN_URL") == "new"

    @pytest.mark.parametrize("shadow", [{}, {"CAIRN_URL": ""}, {"CAIRN_URL": "  "}])
    def test_the_old_name_is_read_when_the_new_one_is_absent_or_blank(self, shadow) -> None:
        # Two points on the "is it set" dimension plus the middle, because "absent" and
        # "present but blank" are different states and the rule names both.
        env = {"SUBSYSTEM_STORE_URL": "old", **shadow}
        assert env_aliases.value(env, "CAIRN_URL") == "old"

    def test_a_name_that_was_never_renamed_resolves_as_itself(self) -> None:
        assert env_aliases.value({"CAIRN_ROUTES": "/t/r.json"}, "CAIRN_ROUTES") == "/t/r.json"
        assert env_aliases.old_name("CAIRN_ROUTES") == ""

    def test_a_shadowed_old_name_still_warns(self) -> None:
        # 🔴 THE RULE THE OBVIOUS IMPLEMENTATION GETS BACKWARDS. Warning from the branch
        # that actually falls through goes SILENT exactly when the operator has set both
        # and most needs to know the old one is still exported somewhere.
        env = {"CAIRN_URL": "new", "SUBSYSTEM_STORE_URL": "old"}
        assert len(env_aliases.deprecations(env)) == 1

    def test_a_blank_old_name_is_not_a_deprecation(self) -> None:
        assert env_aliases.deprecations({"SUBSYSTEM_STORE_URL": ""}) == []

    def test_deprecations_are_sorted_by_new_name(self) -> None:
        env = {old: "set" for _new, old in env_aliases.LEDGER}
        lines = env_aliases.deprecations(env)
        assert len(lines) == len(env_aliases.LEDGER)
        for line, (new, _old) in zip(lines, env_aliases.LEDGER):
            assert f"${new} " in line, line

    def test_warn_once_emits_each_line_once(self) -> None:
        env_aliases.reset_warned_for_test()
        got: list[str] = []
        lines = env_aliases.deprecations({"SUBSYSTEM_STORE_URL": "x"})
        env_aliases.warn_once(lines, got.append)
        env_aliases.warn_once(lines, got.append)
        assert len(got) == 1
        env_aliases.reset_warned_for_test()

    def test_the_file_warning_names_the_file_and_not_a_variable(self) -> None:
        line = env_aliases.file_warning("CAIRN_URL", "SUBSYSTEM_STORE_URL", "/t/env")
        assert "/t/env" in line
        assert "$" not in line, (
            "a file-key warning that says `$VAR` sends the operator to their shell "
            "profile, when the string lives in a file they have to edit"
        )


# --- the client, end to end --------------------------------------------------


def _run(tmp_path: Path, *args: str, **env_overrides: str):
    """The Python client in a hermetic environment, with only what a case names set.

    `env_pin.sanitized_env` is THE definition of the client's configuration surface and
    already sweeps BOTH prefixes, so nothing an operator exports can reach these runs.
    """
    env = env_pin.sanitized_env(**env_overrides)
    return subprocess.run(
        [sys.executable, str(CAIRN_CLI), "--cache", str(tmp_path / "cache"), *args],
        capture_output=True,
        text=True,
        env=env,
        timeout=120,
    )


class TestTheClientHonoursBothNames:
    """🔴 THE ONLY REGRESSION COVERAGE IN THIS FILE; everything above is an invariant guard.

    Measured red at `f74657d9` (pre-change) and green at HEAD for
    `test_the_new_name_configures_the_client`, `..._warns_naming_its_replacement` and
    `..._new_name_wins_over_the_old`: the pre-change client does not know `CAIRN_URL` at
    all, so it refuses with "config incomplete" where HEAD reaches the store.
    `test_the_old_name_still_configures_the_client` is GREEN on both sides by design — it
    is the deprecation window's own claim, and it is an INVARIANT GUARD, not evidence that
    anything was fixed.
    """

    #: A URL nothing serves. Every case here asserts on CONFIGURATION, not on a fetch, so
    #: the run is expected to fail to reach a store — what differs between cases is
    #: WHICH refusal comes back, and that is what the assertions read.
    DEAD = "http://127.0.0.1:1"
    TOKEN = "t" * 48

    def test_the_new_name_configures_the_client(self, tmp_path: Path) -> None:
        proc = _run(tmp_path, "recall", "--scope", "alpha-notes",
                    CAIRN_URL=self.DEAD, CAIRN_TOKEN=self.TOKEN)
        assert "config incomplete" not in proc.stderr, proc.stderr
        assert "CAIRN_URL" not in proc.stderr, proc.stderr

    def test_the_old_name_still_configures_the_client(self, tmp_path: Path) -> None:
        proc = _run(tmp_path, "recall", "--scope", "alpha-notes",
                    SUBSYSTEM_STORE_URL=self.DEAD, SUBSYSTEM_STORE_TOKEN=self.TOKEN)
        assert "config incomplete" not in proc.stderr, proc.stderr

    def test_the_old_name_warns_naming_its_replacement(self, tmp_path: Path) -> None:
        proc = _run(tmp_path, "recall", "--scope", "alpha-notes",
                    SUBSYSTEM_STORE_URL=self.DEAD, SUBSYSTEM_STORE_TOKEN=self.TOKEN)
        # 🔴 THE WHOLE LINE, NOT A KEYWORD. The artifact under test is prose, and a guard
        # on words is walkable by rewording — which is precisely the drift the
        # cross-language gate above exists to catch, so this one must not be weaker.
        for new, old in (("CAIRN_TOKEN", "SUBSYSTEM_STORE_TOKEN"), ("CAIRN_URL", "SUBSYSTEM_STORE_URL")):
            assert f"cairn: {env_aliases.env_warning(new, old)}" in proc.stderr.splitlines(), (
                proc.stderr
            )

    def test_the_warning_order_is_the_ledger_order(self, tmp_path: Path) -> None:
        # Sorted by NEW name: CAIRN_TOKEN before CAIRN_URL. Both clients must agree, and
        # a map iteration that happened to come out the other way would be a parity diff
        # nobody could reproduce.
        proc = _run(tmp_path, "recall", "--scope", "alpha-notes",
                    SUBSYSTEM_STORE_URL=self.DEAD, SUBSYSTEM_STORE_TOKEN=self.TOKEN)
        warnings = [line for line in proc.stderr.splitlines() if "deprecated alias" in line]
        assert len(warnings) == 2, proc.stderr
        assert "$CAIRN_TOKEN" in warnings[0] and "$CAIRN_URL" in warnings[1], warnings

    def test_the_new_name_wins_over_the_old(self, tmp_path: Path) -> None:
        # The old value points at a path on a server that does not exist either, so the
        # two are distinguished by what the REFUSAL names rather than by a fetch.
        proc = _run(tmp_path, "recall", "--scope", "alpha-notes",
                    CAIRN_URL=self.DEAD, CAIRN_TOKEN=self.TOKEN,
                    SUBSYSTEM_STORE_URL="http://127.0.0.1:2/shadowed",
                    SUBSYSTEM_STORE_TOKEN="ignored" * 8)
        assert "/shadowed" not in proc.stderr, (
            "the deprecated name won over its replacement:\n" + proc.stderr
        )

    def test_a_deprecated_config_FILE_key_warns_and_names_the_file(self, tmp_path: Path) -> None:
        """The file is as much an alias surface as the environment is.

        An operator who renamed only the exported variables would otherwise get a silent
        half-migration — the file key ignored, the environment honoured.
        """
        config = tmp_path / "config" / "env"
        config.parent.mkdir(parents=True)
        config.write_text(
            f"SUBSYSTEM_STORE_URL={self.DEAD}\nSUBSYSTEM_STORE_TOKEN={self.TOKEN}\n",
            encoding="utf-8",
        )
        proc = _run(tmp_path, "recall", "--scope", "alpha-notes", CAIRN_CONFIG=str(config))
        assert "config incomplete" not in proc.stderr, proc.stderr
        expected = env_aliases.file_warning("CAIRN_URL", "SUBSYSTEM_STORE_URL", str(config))
        assert f"cairn: {expected}" in proc.stderr.splitlines(), proc.stderr

    def test_a_new_config_FILE_key_is_read_and_does_not_warn(self, tmp_path: Path) -> None:
        config = tmp_path / "config" / "env"
        config.parent.mkdir(parents=True)
        config.write_text(
            f"CAIRN_URL={self.DEAD}\nCAIRN_TOKEN={self.TOKEN}\n", encoding="utf-8"
        )
        proc = _run(tmp_path, "recall", "--scope", "alpha-notes", CAIRN_CONFIG=str(config))
        assert "config incomplete" not in proc.stderr, proc.stderr
        assert "deprecated alias" not in proc.stderr, proc.stderr
