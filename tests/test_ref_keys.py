"""The `tasks:` → `refs:` FRONT-MATTER ledger, pinned ACROSS THE TWO SPELLINGS.

🔴 WHY TWO SPELLINGS EXIST AT ALL, AND WHY A GATE IS THE ANSWER RATHER THAN A FIX.
`packages.cairn` installs the Python client script and `lib/` under `libexec` and nothing
else, so `lib/ref_keys.py` CANNOT import `internal/store`. That is packaging, not a design
choice anybody may undo here. The precedent is `tests/test_env_aliases.py` against
`internal/envalias`, which `AGENTS.md` already blesses for `server/Dockerfile` against
`flake.nix`'s `serverEnv`; this file is the same instrument for the same reason, and it is
deliberately the same SHAPE — a reader who knows one knows this one.

## What it pins, and what it structurally cannot see

It pins the PAIR SET (failing when it GROWS *or* SHRINKS), the ORDER (both sides emit in
ledger order and `tests/parity/harness.py` diffs the two clients' stderr byte-for-byte),
the removal anchor, and the warning text compared **as a whole normalised string**.
🔴 THE WHOLE STRING, NOT KEYWORDS: when the artifact under test is prose, a guard on words
is walkable by rewording, and a reworded warning on one side is exactly the drift this
exists to catch. A cosmetic reword therefore fails this test — that cost is paid on
purpose, for a machine-readable claim.

⚠ IT READS THE GO SIDE AS TEXT, so it is blind to the same thing its precedent is: a
Go-side ARGUMENT-ORDER mistake that still renders well-formed English (`RefKeyWarning`
passing `Old, Old, New`, say). The extractor knows the template but not the substitution.
What closes that here is `TestBothImplementationsAgreeOnBehaviour` below, which runs the
REAL `RefKeyWarning` out of a compiled Go test binary and compares its bytes — and it
SKIPS where no Go toolchain exists, which is why the agreement assertions above it are not
allowed to depend on it.
"""

from __future__ import annotations

import re
import shutil
import subprocess
import sys
from pathlib import Path

import pytest

REPO = Path(__file__).resolve().parents[1]
GO_SOURCE = REPO / "internal" / "store" / "refkeys.go"

sys.path.insert(0, str(REPO / "lib"))

import ref_keys  # noqa: E402


# --- reading the GO spelling -------------------------------------------------
#
# 🔴 A PARSER, NOT A `str.find`. The Go constant is written as a concatenation across
# source lines (`"…" + RefKeyRemovalAnchor + "."`), which is how gofmt wants a long string,
# so "take the text between two quotes" reads ONE fragment and silently compares a third of
# the sentence.

_PAIR = re.compile(r'\{New:\s*"([a-z0-9_-]+)",\s*Old:\s*"([a-z0-9_-]+)"\}')
_ANCHOR_IMPORT = re.compile(r"const RefKeyRemovalAnchor = envalias\.RemovalAnchor")
_ENVALIAS_ANCHOR = re.compile(r'const RemovalAnchor = "([^"]*)"')
_TOKEN = re.compile(r'"((?:[^"\\]|\\.)*)"|\b(RefKeyRemovalAnchor)\b')


def _go_ledger(source: str) -> tuple[tuple[str, str], ...]:
    """Every `(new, old)` pair declared in the Go ledger, in source order."""
    return tuple(_PAIR.findall(source))


def _go_anchor(source: str) -> str:
    """The anchor the Go side renders into the warning.

    🔴 IT FOLLOWS THE INDIRECTION RATHER THAN RE-SPELLING IT. `refkeys.go` does not declare
    an anchor string — it binds `envalias.RemovalAnchor`, deliberately, because both windows
    close on the same event. An extractor that expected a literal here would find none and
    fall back to an empty string, which then compares equal to nothing in a way that reads
    as agreement. So the indirection is asserted and then resolved in the other file.
    """
    assert _ANCHOR_IMPORT.search(source), (
        "refkeys.go no longer binds RefKeyRemovalAnchor to envalias.RemovalAnchor — "
        "if the anchor became a literal here, this extractor must read it"
    )
    envalias_source = (REPO / "internal" / "envalias" / "envalias.go").read_text(encoding="utf-8")
    match = _ENVALIAS_ANCHOR.search(envalias_source)
    assert match, "internal/envalias declares no RemovalAnchor — the extractor reads the wrong file"
    return match.group(1)


def _go_warning_template(source: str) -> str:
    """`refKeyWarningFormat`'s value, resolved across its `+` fragments."""
    marker = "const refKeyWarningFormat = "
    start = source.index(marker) + len(marker)
    rest = source[start:]
    end = rest.index("\n\n")
    body = rest[:end]
    anchor = _go_anchor(source)
    out: list[str] = []
    for literal, identifier in _TOKEN.findall(body):
        out.append(anchor if identifier else literal.encode().decode("unicode_escape"))
    return "".join(out)


def _go_warning(source: str, new: str, old: str) -> str:
    """The Go warning for one pair, rendered from the extracted template.

    The `%s` order mirrors `RefKeyWarning`'s call; see this module's docstring for the one
    thing that makes blind and what covers it instead.
    """
    return _go_warning_template(source) % (old, new, new)


@pytest.fixture(scope="module")
def go_source() -> str:
    return GO_SOURCE.read_text(encoding="utf-8")


# --- the instrument, before its verdict --------------------------------------


class TestTheExtractorIsAnInstrument:
    """🔴 VALIDATE THE INSTRUMENT BEFORE READING ITS VERDICT.

    Every assertion below this class compares against something the extractor produced. A
    regex that matched NOTHING — a renamed constant, a reformatted literal, a `gofmt` that
    split a line differently — yields an empty template and an empty ledger, and an empty
    set compares equal to nothing in a way that reads as agreement. So: watch the numbers
    move before quoting a zero.
    """

    def test_it_finds_a_non_empty_ledger(self, go_source: str) -> None:
        pairs = _go_ledger(go_source)
        assert len(pairs) == len(ref_keys.LEDGER) > 0, (
            f"the Go ledger extractor returned {len(pairs)} pairs against "
            f"Python's {len(ref_keys.LEDGER)}"
        )

    def test_it_resolves_the_whole_concatenated_template(self, go_source: str) -> None:
        # The POSITIVE CONTROL on the concatenation walk: the template spans more than one
        # source fragment, so a reader that took only the first would come back short. The
        # anchor lives in the LAST fragment and the three `%s` in the first two.
        template = _go_warning_template(go_source)
        assert template.count("%s") == 3, template
        assert ref_keys.REMOVAL_ANCHOR in template, template

    def test_a_mutated_go_source_moves_the_answer(self, go_source: str) -> None:
        # The NEGATIVE CONTROL. If this comparison could not go red, the agreement
        # assertions below would be a fact about the harness and not about the two files.
        mutated = go_source.replace("is a deprecated alias for", "is an obsolete name for")
        assert mutated != go_source, "the mutation did not apply — the control is inert"
        assert _go_warning_template(mutated) != _go_warning_template(go_source)

        dropped = go_source.replace('{New: "refs", Old: "tasks"},\n', "")
        assert dropped != go_source, "the mutation did not apply — the control is inert"
        assert len(_go_ledger(dropped)) == len(_go_ledger(go_source)) - 1


# --- the gate ----------------------------------------------------------------


class TestTheTwoSpellingsAgree:
    def test_the_pair_set_is_identical(self, go_source: str) -> None:
        """🔴 FAILS WHEN THE SET GROWS *OR* SHRINKS, which are different defects.

        A pair added on one side only is a front-matter key that works in one client; a
        pair REMOVED on one side only is a deprecated key that silently stops parsing for
        half the deployment — and since the two clients read ONE cache, that is a store
        whose entries report different refs depending on which binary read them.
        """
        assert set(_go_ledger(go_source)) == set(ref_keys.LEDGER)

    def test_the_order_is_identical(self, go_source: str) -> None:
        assert _go_ledger(go_source) == ref_keys.LEDGER

    def test_the_removal_anchor_is_identical(self, go_source: str) -> None:
        assert _go_anchor(go_source) == ref_keys.REMOVAL_ANCHOR

    def test_every_rendered_warning_is_byte_identical(self, go_source: str) -> None:
        for new, old in ref_keys.LEDGER:
            assert _go_warning(go_source, new, old) == ref_keys.warning(new, old)


class TestTheLedgerItself:
    def test_the_order_is_sorted_by_new_then_old(self) -> None:
        """The tie-break on the OLD key is load-bearing, not decoration.

        `env_aliases` sorts by new name alone because every new name there is distinct.
        BOTH pairs here share the new key `refs`, so new-key order alone would leave the
        two lines' relative order to whichever dict or map the emitter walked — and the
        parity harness diffs the two clients' stderr byte-for-byte.
        """
        assert list(ref_keys.LEDGER) == sorted(ref_keys.LEDGER)

    def test_every_old_key_is_one_the_parser_still_reads(self) -> None:
        """A ledger entry naming a key nothing parses is a declaration with nothing behind it.

        🔴 MEASURED THROUGH THE PARSER, not by grepping for the key name. The check is that
        an entry carrying ONLY the deprecated key still surfaces its ref, which is the
        property the deprecation window promises.
        """
        from subsystem_resolver import SubsystemEntry

        for _new, old in ref_keys.LEDGER:
            value: object = "clickup:kept" if old == "task" else ["clickup:kept"]
            entry = SubsystemEntry.from_mapping({"service": "alpha", "scope": "zone-one", old: value})
            assert [str(t) for t in entry.tasks] == ["clickup:kept"], (
                f"`{old}:` is in the ledger but the parser no longer reads it"
            )


class TestBothImplementationsAgreeOnBehaviour:
    """The half source-reading cannot cover: the REAL Go function's bytes.

    ⚠ IT SKIPS WITHOUT A GO TOOLCHAIN, and the assertions above deliberately do not depend
    on it — the `tests` CI job has no toolchain, and a gate that only ran in one job would
    be a gate nobody notices losing.
    """

    def test_the_go_function_renders_the_same_bytes(self, tmp_path: Path) -> None:
        if shutil.which("go") is None:
            pytest.skip("no Go toolchain; the source-text gate above still ran")
        program = tmp_path / "main_test.go"
        program.write_text(
            "package store\n\nimport (\n\t\"fmt\"\n\t\"testing\"\n)\n\n"
            "func TestPrintTheWarnings(t *testing.T) {\n"
            "\tfor _, p := range RefKeyLedger {\n"
            "\t\tfmt.Printf(\"WARN\\t%s\\t%s\\t%s\\n\", p.New, p.Old, RefKeyWarning(p))\n"
            "\t}\n}\n",
            encoding="utf-8",
        )
        target = REPO / "internal" / "store" / "zz_refkeys_bytes_probe_test.go"
        target.write_text(program.read_text(encoding="utf-8"), encoding="utf-8")
        try:
            proc = subprocess.run(
                ["go", "test", "./internal/store/", "-run", "TestPrintTheWarnings", "-v"],
                cwd=REPO, capture_output=True, text=True, timeout=600,
            )
        finally:
            target.unlink(missing_ok=True)
        assert proc.returncode == 0, proc.stdout + proc.stderr
        rendered = {}
        for line in proc.stdout.splitlines():
            if line.startswith("WARN\t"):
                _, new, old, text = line.split("\t", 3)
                rendered[(new, old)] = text
        # The POSITIVE CONTROL on the capture: a zero here would be indistinguishable from
        # agreement, so the count has to move before the comparison is read.
        assert len(rendered) == len(ref_keys.LEDGER) > 0, proc.stdout
        for new, old in ref_keys.LEDGER:
            assert rendered[(new, old)] == ref_keys.warning(new, old)
