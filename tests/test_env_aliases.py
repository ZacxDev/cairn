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
import time
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


def _collisions(ledger, taken: set[str]) -> list[str]:
    """Ledger new-names that are ALREADY live variables meaning something else.

    🔴 A NAMED FUNCTION SO THE NEGATIVE CONTROL BELOW RUNS THE SAME CODE THE GUARD DOES.
    Spelled inline at the guard, the "control" could only ever re-assert facts about its
    own fixture — which is what it did: it built a `taken` set, checked two memberships in
    it, and never executed the loop. Breaking the real predicate to `new in set()` left it
    green. With one implementation, the control's fixture drives the predicate under test,
    so breaking the predicate reddens the control.
    """
    return sorted(new for new, _old in ledger if new in taken)


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

        🔴 `taken` IS DERIVED, AND THE HAND-WRITTEN SET IT REPLACED WAS ALREADY WRONG.
        It named six strings, one of which — `CAIRN_CACHE_ROOT` — is read by NOTHING in
        either language (`internal/client/readstore.go` says the env override was
        deliberately not added). A set that is wrong today cannot be trusted to see the
        next collision, which is the whole job.

        🔴 WHY A TREE-WIDE `CAIRN_[A-Z_]+` SWEEP CANNOT ANSWER THIS, stated because it is
        the obvious thing to reach for and it is vacuous: the ledger's OWN new names are
        live too, so "every CAIRN_* in the tree" minus "the ledger" is the ledger's
        complement by construction and the assertion below could never fail. What IS
        non-vacuous is a source that stays true without anybody maintaining it against the
        ledger — a name read DIRECTLY from the environment. This PR's rule is that a
        renamed variable is never read that way (`env_aliases` owns every one of them), so
        a direct read is, by construction, a name some other subsystem owns.
        """
        from host_identity import HOST_LABEL_ENV
        from cairn_instances import ROUTES_ENV

        # Direct-environment reads of a `CAIRN_*` literal, in BOTH clients. A ledger name
        # may not appear here; anything that does belongs to another feature.
        direct = re.compile(
            r"""os\.(?:environ\.get|getenv|Getenv|LookupEnv)\(\s*["'](CAIRN_[A-Z0-9_]+)["']"""
            r"""|os\.environ\[\s*["'](CAIRN_[A-Z0-9_]+)["']"""
        )
        sources = [REPO / "cairn"] + sorted((REPO / "lib").glob("*.py"))
        sources += [p for p in (REPO / "internal").rglob("*.go") if not p.name.endswith("_test.go")]
        sources += [p for p in (REPO / "cmd").rglob("*.go") if not p.name.endswith("_test.go")]
        found: set[str] = set()
        for path in sources:
            for a, b in direct.findall(path.read_text(encoding="utf-8")):
                found.add(a or b)

        # 🔴 THE POSITIVE CONTROL. An expression that matched nothing would make `taken`
        # the two imported constants alone and quietly narrow this guard; a regex is a
        # dependency on a spelling, and "no matches" means "possibly the wrong pattern".
        assert "CAIRN_MIRROR_ROOT" in found, (
            "the direct-read sweep found "
            f"{sorted(found)} and not CAIRN_MIRROR_ROOT, which `cairn` and "
            "`internal/client/cli.go` both read that way — the pattern is wrong, and a "
            "silently empty sweep would leave this guard asserting almost nothing"
        )

        taken = set(HOST_LABEL_ENV) | {ROUTES_ENV} | found
        offenders = _collisions(env_aliases.LEDGER, taken)
        assert not offenders, (
            f"{offenders} are already live variables meaning something else; a rename "
            "onto one makes two features read one name"
        )

    def test_the_collision_PREDICATE_can_go_red(self) -> None:
        """🔴 THE NEGATIVE CONTROL FOR THE GUARD ABOVE, WHICH IS OTHERWISE A ZERO.

        Every name in today's ledger passes, so the guard above is a claim about an empty
        intersection — indistinguishable from a `taken` set built by a broken sweep, or
        from a predicate that cannot reject anything.

        🔴 IT RUNS `_collisions`, THE PREDICATE ITSELF, WHICH IS THE WHOLE POINT AND IS
        WHAT THE PREVIOUS VERSION DID NOT DO. That one built a subset of `taken` and
        asserted two membership facts about it; the guard's loop never executed. It was a
        fixture sanity check wearing a negative control's label.

        Measured, one mutation — the collision predicate emptied to `new in set()`, so it
        can never report a collision:

            before the rewrite (inline `assert new not in set()`):  37 passed, 0 failed
            after  the rewrite (`_collisions(…, set())`):            36 passed, 1 failed

        and the one failure is THIS test, on the first `_collisions` assertion below. So
        the mutant was previously SURVIVED by the whole module and is now killed here and
        nowhere else — the attribution the row exists for.

        The hostile ledger is the MECHANICAL PREFIX SWAP — `SUBSYSTEM_STORE_HOST` ->
        `CAIRN_HOST` — which is the actual defect the real ledger avoids, not an invented
        collision. The second assertion is the other direction: the same predicate over
        the same `taken` accepts the spelling the ledger really uses, so a predicate that
        simply reported everything would fail here.
        """
        from host_identity import HOST_LABEL_ENV

        taken = set(HOST_LABEL_ENV)
        assert "CAIRN_HOST" in taken, (
            f"the fixture is wrong before the predicate is even reached: {sorted(taken)} "
            "does not contain CAIRN_HOST, the machine label this collision is about"
        )

        swapped = [("CAIRN_HOST", "SUBSYSTEM_STORE_HOST")]
        assert _collisions(swapped, taken) == ["CAIRN_HOST"], (
            "the collision predicate accepted the mechanical prefix swap, so the guard "
            "above is not measuring what its docstring claims"
        )

        chosen = [("CAIRN_LISTEN_HOST", "SUBSYSTEM_STORE_HOST")]
        assert _collisions(chosen, taken) == [], (
            "the collision predicate rejects the spelling the ledger actually uses — it "
            "is not discriminating, and the guard above would be red on a correct tree"
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

    @pytest.mark.parametrize("value", ["", "  ", "\t"])
    def test_a_blank_old_name_is_not_a_deprecation(self, value) -> None:
        """🔴 THE WHITESPACE ROWS ARE THE HALF THAT WAS FALSE — see the pair below.

        `deprecations` has always tested blankness with `.strip()`. `value` returned the
        old name's value RAW, so `SUBSYSTEM_STORE_ROOT="  "` resolved to `"  "` and warned
        about nothing at the same time — the one combination the stated rule ("a blank
        value changes no resolution, so it is not a deprecation") rules out.
        """
        assert env_aliases.deprecations({"SUBSYSTEM_STORE_URL": value}) == []

    @pytest.mark.parametrize("value", ["", "  ", "\t"])
    def test_a_blank_old_name_resolves_as_ABSENT(self, value) -> None:
        """🔴 REGRESSION COVERAGE, AND THE OTHER HALF OF THE PAIR ABOVE.

        Matrix: RED at `78679b9` (this branch's own pre-fix state) for `"  "` and `"\\t"` —
        `value` returned the whitespace and `value_or` therefore never reached its fallback,
        so a pod would have taken a whitespace store root — green here. The `""` row was
        already green and stays as the control that the fix did not invert the predicate.

        ⚠ IT IS IDENTICAL IN `internal/envalias`, WHICH IS WHY `tests/parity/` COULD NOT
        SEE IT: two clients failing the same way compare equal. Only reading the resolver
        against the deprecation sweep found it.
        """
        assert env_aliases.value({"SUBSYSTEM_STORE_URL": value}, "CAIRN_URL") == ""
        # And through `value_or`, which is the shape `server.py`'s `main()` reads its store
        # root with: the FALLBACK has to come back, not the blank. A fallback that no
        # fixture value can equal, so the assertion cannot pass by coincidence.
        assert env_aliases.value_or(
            {"SUBSYSTEM_STORE_URL": value}, "CAIRN_URL", "fallback"
        ) == "fallback"

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


# --- the AUTHORISED P1 exception: present-but-empty is ABSENT ------------------
#
# Declared in `tests/conformance/README.md`, § *An AUTHORISED exception to "do not change
# the oracle" — a present-but-empty value is ABSENT*. Read it before editing this class:
# it carries the decision, the author of record, and what the corpus structurally cannot
# see about this rule.


SERVER_PY = REPO / "server" / "server.py"

#: A free port, taken and given straight back.
#:
#: ⚠ THE SAME RACE `tests/conformance/oracle.py` AND `freePort` IN THE GO TESTS RUN, and
#: for the same reason: the alternative is passing a listener into a server that would
#: then not be the real `main`. It fails LOUDLY — the child refuses and this file prints
#: its output — rather than hanging.
def _free_port() -> int:
    import socket

    with socket.socket() as s:
        s.bind(("127.0.0.1", 0))
        return s.getsockname()[1]


class TestABlankValueIsTreatedAsAbsentByTheORACLE:
    """⚠ INVARIANT GUARD, AND THE EARLIER LABEL ON THIS CLASS WAS WRONG — MEASURED.

    It used to say "REGRESSION COVERAGE …, RED ON THE PRE-CHANGE ORACLE". It is not, and
    the correction is the interesting part. This fixture blanks the CURRENT names and
    supplies the deprecated ones. At `f74657d` the oracle has **no `CAIRN_*` handling at
    all** (`--store` reads `os.environ.get("SUBSYSTEM_STORE_ROOT", …)` directly), so
    blanking `CAIRN_STORE_ROOT` there exercises nothing. Run against
    `git show f74657d:server/server.py` with the base-era
    `SUBSYSTEM_STORE_TRUSTED_PROXIES` supplied, the base oracle prints EXACTLY the string
    asserted below:

        subsystem-store-api: listening on 127.0.0.1:19101 store=<the tmpdir> …

    — GREEN at base. Red appeared only when the fixture named `CAIRN_TRUSTED_PROXIES`,
    which base does not know, and the base oracle then died on `no trusted proxies`
    BEFORE `main()` ever evaluated a blank store root or port. That is a mutant dying for
    the wrong reason, on the only claimed coverage for the PR's one authorised oracle
    change.

    🔴 SO WHAT DOES THIS CLASS STILL BUY? Two things, neither of them attribution.
    `TestABlankOLDNameOnTheORACLE` below is the arm that attributes. This one pins that
    the blank CURRENT name falls through to the DEPRECATED one — the deprecation window's
    own claim, which no defect ever violated — and, in
    `test_the_resolved_values_MOVE_with_the_environment`, it is the control that makes the
    other class's `store=/data` assertion mean something: a mutant that hardcoded the
    default would satisfy an arm expecting the default and fail here, where the expected
    values are a `tmp_path` and a kernel-assigned port.

    🔴 WHY IT IS OBSERVED ON THE STARTUP LINE RATHER THAN ON `value_or`. A unit test on
    the resolver stays green while `main()` stops calling it — the seam nobody owns. Both
    servers print `listening on <host>:<port> store=<root> …`, so this reads the resolved
    values out of the running program, through the same text
    `cmd/cairn-server`'s `TestABlankEnvironmentValueIsTreatedAsABSENT` reads — which is an
    invariant guard for the same reason on the Go side.

    🔴 THE FIXTURE CANNOT COINCIDE WITH ANY CONSTANT UNDER TEST. The store is a `tmp_path`
    and never `DEFAULT_STORE` (`/data`), the port is kernel-assigned and asserted unequal
    to `DEFAULT_PORT` (8102), and the host is `127.0.0.1` against a `0.0.0.0` default —
    so a mutant that hardcoded any of the three defaults cannot survive.
    """

    #: 43 characters is `MIN_TOKEN_CHARS`. Built rather than written out: a real-looking
    #: 43-character literal in this repository is a leak-scanner finding whatever it is.
    TOKEN = "c" * 43

    def _start(self, tmp_path: Path, store: Path, port: int, label: str) -> str:
        """Boot the oracle with the CURRENT names blank and the OLD ones carrying values.

        No `--store`, `--host` or `--port` flag: each of those defaults is env-resolved,
        so the startup line reports exactly what the resolver decided. A flag would
        override the very thing under test.
        """
        store.mkdir(parents=True, exist_ok=True)
        token_file = tmp_path / f"tokens-{label}"
        token_file.write_text(self.TOKEN + "\n", encoding="utf-8")
        token_file.chmod(0o600)
        env = env_pin.sanitized_env(
            CAIRN_TRUSTED_PROXIES="192.0.2.0/24",
            # present, empty — the case under test
            CAIRN_STORE_ROOT="",
            CAIRN_LISTEN_HOST="",
            CAIRN_PORT="",
            # the deprecated spellings, which must therefore be what is read
            SUBSYSTEM_STORE_ROOT=str(store),
            SUBSYSTEM_STORE_HOST="127.0.0.1",
            SUBSYSTEM_STORE_PORT=str(port),
        )
        log = tmp_path / f"oracle-{label}.log"
        with log.open("wb") as handle:
            proc = subprocess.Popen(
                [sys.executable, str(SERVER_PY), "--token-file", str(token_file)],
                stdout=handle,
                stderr=subprocess.STDOUT,
                env=env,
            )
            try:
                deadline = time.monotonic() + 30
                while time.monotonic() < deadline:
                    text = log.read_text(encoding="utf-8", errors="replace")
                    if "listening on" in text:
                        return text
                    if proc.poll() is not None:
                        break
                    time.sleep(0.02)
                # A blank read as PRESENT is exactly this: `--store ""` and, for the
                # port, the `ValueError` the pre-change oracle raised. Neither reaches
                # the startup line, so the failure names what it did not see and prints
                # what the process said instead.
                raise AssertionError(
                    "the oracle never printed a `listening on` line with a blank "
                    "CAIRN_STORE_ROOT/CAIRN_LISTEN_HOST/CAIRN_PORT and the deprecated "
                    "spellings set — a present-but-empty value was NOT treated as "
                    "absent. Process output:\n"
                    + log.read_text(encoding="utf-8", errors="replace")
                )
            finally:
                proc.kill()
                proc.wait(timeout=30)

    def test_a_blank_current_name_falls_through_to_the_deprecated_one(
        self, tmp_path: Path
    ) -> None:
        store, port = tmp_path / "world-one", _free_port()
        assert port != 8102, (
            "the kernel handed out DEFAULT_PORT — this fixture must not be able to "
            "coincide with the constant under test"
        )
        text = self._start(tmp_path, store, port, "one")
        assert f"listening on 127.0.0.1:{port} store={store} " in text, text

    def test_the_resolved_values_MOVE_with_the_environment(self, tmp_path: Path) -> None:
        """🔴 THE CONTROL, WITHOUT WHICH THE CASE ABOVE PROVES NOTHING.

        An implementation that ignored the environment and printed a compiled-in answer
        satisfies a single-world assertion for every run. Two distinct worlds is what
        makes the first one a measurement.
        """
        first, first_port = tmp_path / "world-a", _free_port()
        second, second_port = tmp_path / "world-b", _free_port()
        assert first != second and first_port != second_port, (first, second)
        one = self._start(tmp_path, first, first_port, "a")
        two = self._start(tmp_path, second, second_port, "b")
        assert f"listening on 127.0.0.1:{first_port} store={first} " in one, one
        assert f"listening on 127.0.0.1:{second_port} store={second} " in two, two


class TestABlankOLDNameOnTheORACLE:
    """🔴 THE ARM THAT ATTRIBUTES: RED AT `f74657d` FOR THIS RULE, NOT FOR A NEIGHBOUR'S.

    `TestABlankValueIsTreatedAsAbsentByTheORACLE` blanks the CURRENT names, which the
    pre-change oracle does not read at all — so it is green at base and is labelled an
    invariant guard there. Blanking the **DEPRECATED** name is what exercises the rule on
    a tree that only knows that name: base takes the blank literally, HEAD treats it as
    absent and falls through to the built-in default.

    🔴 THE BASE-ERA SPELLING OF EVERY OTHER VARIABLE IS SUPPLIED, SO THE BASE CANNOT DIE
    FOR A NEIGHBOUR'S REASON. `SUBSYSTEM_STORE_TRUSTED_PROXIES` in particular: name it
    `CAIRN_TRUSTED_PROXIES` and the base oracle refuses with `no trusted proxies` before
    `main()` evaluates a store root or a port, which is a red that proves nothing.

    Measured against `git show f74657d:server/server.py`, one half at a time:

        store only, SUBSYSTEM_STORE_ROOT=   base: `… store= token-ids=…` (empty)
                                            HEAD: `… store=/data …`
        port only,  SUBSYSTEM_STORE_PORT=   base: ValueError: invalid literal for
                                                  int() with base 10: ''
                                            HEAD: comes up

    ⚠ THE PORT HALF PASSES `--port` AS A FLAG AND IS STILL RED AT BASE, WHICH IS THE
    POINT. An `argparse` default is evaluated at `add_argument`, so the pre-change
    `int(os.environ.get(...))` raises while the parser is being BUILT — a flag cannot
    save it. That also lets this bind a free port instead of `DEFAULT_PORT`.

    ⚠ THE STORE HALF EXPECTS `/data`, WHICH IS THE CONSTANT UNDER TEST, AND THAT IS
    UNAVOIDABLE: "a blank value falls through to the default" is a claim about the
    default. A mutant hardcoding `/data` would survive THIS arm — and dies in
    `TestABlankValueIsTreatedAsAbsentByTheORACLE`, whose expected store is a `tmp_path`
    and whose second world watches the value move. The two arms are each other's control;
    neither is sufficient alone, and that is stated here rather than left to be noticed.
    """

    TOKEN = "c" * 43

    def _start(self, tmp_path: Path, label: str, *flags: str, **env: str) -> str:
        token_file = tmp_path / f"tokens-{label}"
        token_file.write_text(self.TOKEN + "\n", encoding="utf-8")
        token_file.chmod(0o600)
        # 🔴 EVERY NAME HERE IS THE DEPRECATED SPELLING, INCLUDING TRUSTED_PROXIES. The
        # fixture has to be one the PRE-CHANGE oracle understands in full, or its red is
        # not about the rule under test.
        full = env_pin.sanitized_env(SUBSYSTEM_STORE_TRUSTED_PROXIES="192.0.2.0/24", **env)
        log = tmp_path / f"oracle-{label}.log"
        with log.open("wb") as handle:
            proc = subprocess.Popen(
                [sys.executable, str(SERVER_PY), "--token-file", str(token_file), *flags],
                stdout=handle, stderr=subprocess.STDOUT, env=full,
            )
            try:
                deadline = time.monotonic() + 30
                while time.monotonic() < deadline:
                    text = log.read_text(encoding="utf-8", errors="replace")
                    if "listening on" in text:
                        return text
                    if proc.poll() is not None:
                        break
                    time.sleep(0.02)
                raise AssertionError(
                    f"the oracle never printed a `listening on` line for case {label!r} — "
                    "a blank DEPRECATED name was NOT treated as absent. Process output:\n"
                    + log.read_text(encoding="utf-8", errors="replace")
                )
            finally:
                proc.kill()
                proc.wait(timeout=30)

    def test_a_blank_deprecated_STORE_falls_through_to_the_default(
        self, tmp_path: Path
    ) -> None:
        """RED at `f74657d`: that oracle prints `store=` with nothing after it."""
        port = _free_port()
        text = self._start(
            tmp_path, "store",
            SUBSYSTEM_STORE_ROOT="",
            SUBSYSTEM_STORE_HOST="127.0.0.1",
            SUBSYSTEM_STORE_PORT=str(port),
        )
        assert f"listening on 127.0.0.1:{port} store=/data " in text, text
        # The literal failure the pre-change oracle produced, asserted as an ABSENCE so
        # the arm names what it is distinguishing itself from.
        assert " store= " not in text, text

    def test_a_blank_deprecated_PORT_does_not_refuse_at_parser_construction(
        self, tmp_path: Path
    ) -> None:
        """RED at `f74657d`: `ValueError: invalid literal for int() with base 10: ''`.

        The store is supplied non-blank here so this arm is about the port alone, and
        `--port` is a FLAG — which does not rescue the pre-change oracle, because the
        default is evaluated while the parser is built.
        """
        store = tmp_path / "store"
        store.mkdir()
        port = _free_port()
        text = self._start(
            tmp_path, "port", "--host", "127.0.0.1", "--port", str(port),
            SUBSYSTEM_STORE_ROOT=str(store),
            SUBSYSTEM_STORE_PORT="",
        )
        assert f"listening on 127.0.0.1:{port} store={store} " in text, text
        assert "ValueError" not in text, text
