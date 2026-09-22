#!/usr/bin/env python3
"""The post-write parse check, in the package that an agent pod actually has.

🔴 WHY THIS EXISTS AT ALL. The write protocol mandates a parse check after every
entry write, and for a long time the only implementation of it lived in a writer
module that is not part of this package. An agent holding the packaged client had
read and write on the store and no way to run the check it was told to run — so
the mandated step was either skipped or replaced by `cairn validate`, whose count
was assembled from two different walks (see `TestTheREADMEIsNotAnEntryFile`).

🔴 AND THE POD IS THE CONDITION THAT SHAPED THE EXTRACTION. `kubectl exec` lands
on `/`, which is not a git checkout; the writer module needs git at IMPORT time
for things this path never calls, so importing it there fails before the check
runs. These two functions touch no git, no clock and no network, and
`TestItRunsWhereThePodRuns` is the measurement of that rather than the claim.

⚠ TWO OF THE CLASSES BELOW ARE INVARIANT GUARDS, NOT REGRESSION COVERAGE, and are
labelled as such in their own docstrings. Everything else here covers behaviour
that did not exist in this package before.
"""
from __future__ import annotations

import os
import subprocess
import sys
from pathlib import Path

import pytest

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT / "lib"))

import entry_shape  # noqa: E402
from entry_shape import (  # noqa: E402
    CairnError,
    EntryFileMissingError,
    StoreMissingError,
    TouchError,
    validate_entry_file,
    validate_scope,
)
from subsystem_resolver import MalformedEntry  # noqa: E402

SCOPE = "widget-cfg"


def _entry(service: str, scope: str, *, extra: str = "") -> str:
    """One well-formed entry file. Synthetic — no real service is named here."""
    head = ["---", f"service: {service}", f"scope: {scope}"]
    if extra:
        head.append(extra)
    return "\n".join(
        head
        + [
            "---",
            "",
            "## What it is",
            f"The {service} component, described durably.",
            "",
            "## Pointers",
            f"- ops skill `manage-{service}` — invoke it for restarts",
            "",
            "## Nuance / work-history",
            "- 2026-01-02: a synthetic bullet.",
            "",
        ]
    )


@pytest.fixture
def store(tmp_path: Path) -> Path:
    """A store root holding ONE scope with ONE readable entry."""
    (tmp_path / SCOPE).mkdir()
    (tmp_path / SCOPE / "thing-alpha.md").write_text(_entry("thing-alpha", SCOPE))
    return tmp_path


class TestOneFileAgainstTheLoadersOwnPredicate:
    def test_a_well_formed_entry_returns_None(self, store: Path):
        assert validate_entry_file(store / SCOPE / "thing-alpha.md") is None

    def test_a_malformed_entry_comes_back_as_DATA_not_a_raise(self, store: Path):
        """🔴 A REJECTION IS A ROW, NOT AN EXCEPTION. A writer validating a file
        it just wrote has to be able to PRINT what is wrong beside the file it is
        wrong in; raising would make the caller reconstruct that by parsing a
        message, which is a second parser for a format nothing pins."""
        bad = store / SCOPE / "thing-beta.md"
        bad.write_text(_entry("thing-beta", SCOPE, extra="aliases: not-a-list"))

        got = validate_entry_file(bad)

        assert isinstance(got, MalformedEntry)
        assert got.scope == SCOPE
        assert got.filename == "thing-beta.md"
        assert got.reason == "`aliases:` must be a list, not a bare string"

    def test_the_SCOPE_is_the_PARENT_DIRECTORY_not_the_files_own_field(
        self, store: Path
    ):
        """🔴 THE DIRECTORY IS THE AUTHORITY ON SCOPE, exactly as the loader
        takes it. Validating against the file's own `scope:` field would answer a
        question the loader never asks, so an entry that is fine where it sits
        could be blessed after being moved somewhere it is not.

        The file here declares a DIFFERENT scope from the directory it lives in;
        the rejection must be attributed to the directory.
        """
        (store / "gizmo-notes").mkdir()
        moved = store / "gizmo-notes" / "thing-gamma.md"
        moved.write_text(
            _entry("thing-gamma", "widget-cfg", extra="aliases: not-a-list")
        )

        got = validate_entry_file(moved)

        assert got is not None
        assert got.scope == "gizmo-notes", (
            "the rejection was attributed to the file's own `scope:` field, not "
            "to the directory the loader would read it from"
        )

    def test_a_MISSING_file_raises_rather_than_reading_as_malformed(
        self, store: Path
    ):
        """🔴 TWO FACTS WITH TWO DIFFERENT FIXES. 'the path is wrong' and 'the
        front matter is wrong' must not share a spelling — reporting the first as
        the second sends a writer to edit front matter that is not there."""
        with pytest.raises(EntryFileMissingError) as exc:
            validate_entry_file(store / SCOPE / "never-written.md")

        assert "index entry file not found" in str(exc.value)
        assert "malformed index entry" not in str(exc.value)

    def test_a_DIRECTORY_is_not_a_file_either(self, store: Path):
        """The predicate is `is_file()`, not `exists()`. A directory named
        `<something>.md` exists and cannot be read as an entry."""
        d = store / SCOPE / "not-an-entry.md"
        d.mkdir()

        with pytest.raises(EntryFileMissingError):
            validate_entry_file(d)


class TestTheErrorClassIdentity:
    """🔴 CLASS IDENTITY, NOT THE MESSAGE — and this is an INVARIANT GUARD.

    No bug ever changed this base; the guard exists because the failure it
    prevents is silent. A writer half matches on `except TouchError`, which is
    this module's alias for `CairnError`. Re-basing this class on a fresh
    exception would leave every one of those clauses NOT matching while the
    message stayed byte-identical.
    """

    def test_TouchError_IS_CairnError_not_merely_a_subclass_of_it(self):
        assert TouchError is CairnError

    def test_EntryFileMissingError_is_caught_by_BOTH_spellings(self):
        assert issubclass(EntryFileMissingError, TouchError)
        assert issubclass(EntryFileMissingError, CairnError)

    def test_StoreMissingError_is_the_SHARED_class_not_a_local_redefinition(self):
        """`validate_scope` must raise the class the reader already catches. A
        same-named class defined beside it would be a DIFFERENT class, and an
        `except entry_shape.StoreMissingError` elsewhere would not match it."""
        assert StoreMissingError is entry_shape.StoreMissingError
        assert issubclass(StoreMissingError, CairnError)


class TestOneScopeAgainstTheWholeIndex:
    def test_checked_names_every_file_WALKED_including_the_rejected_one(
        self, store: Path
    ):
        """🔴 `checked` IS 'FILES WALKED', NOT 'FILES THAT PARSED'. If a rejected
        file dropped out of it, the count a caller prints would have a numerator
        and a denominator taken from two different populations — and the zero it
        accompanies would mean nothing."""
        (store / SCOPE / "thing-beta.md").write_text(
            _entry("thing-beta", SCOPE, extra="aliases: not-a-list")
        )

        checked, malformed = validate_scope(store, SCOPE)

        assert checked == ("thing-alpha.md", "thing-beta.md")
        assert [m.filename for m in malformed] == ["thing-beta.md"]

    def test_a_scope_with_NOTHING_WRONG_reports_what_it_walked(self, store: Path):
        checked, malformed = validate_scope(store, SCOPE)
        assert checked == ("thing-alpha.md",)
        assert malformed == ()

    def test_the_DUPLICATE_ref_is_visible_here_and_NOWHERE_ELSE(self, store: Path):
        """🔴 THE CASE A PER-FILE LOOP STRUCTURALLY CANNOT SEE, and the reason
        this function goes through the index build rather than calling
        `validate_entry_file` in a loop.

        A duplicate is a RELATIONSHIP between two files. Both files below are
        individually well-formed — each one's filename slug agrees with its own
        `service:` — and they collide only once the index folds both refs. The
        assertion is in BOTH directions: each file alone is `None`, and the scope
        reports exactly one rejection naming the later of the pair.
        """
        (store / SCOPE / "thing_alpha.md").write_text(
            _entry("thing_alpha", SCOPE)
        )

        assert validate_entry_file(store / SCOPE / "thing-alpha.md") is None
        assert validate_entry_file(store / SCOPE / "thing_alpha.md") is None

        checked, malformed = validate_scope(store, SCOPE)

        assert checked == ("thing-alpha.md", "thing_alpha.md")
        assert [m.filename for m in malformed] == ["thing_alpha.md"]
        assert malformed[0].reason == (
            "duplicate 'thing-alpha' in scope 'widget-cfg' — already defined by "
            "'thing-alpha.md'"
        )

    def test_a_MISSING_STORE_ROOT_raises_and_says_it_is_not_a_clean_bill(
        self, tmp_path: Path
    ):
        """🔴 NOT `((), ())`. 'the store is not there' and 'the scope is empty'
        both print no rows, and one of them is a lie. The message has to say so
        in its own words, because a caller that only looks at the row count
        cannot tell them apart."""
        with pytest.raises(StoreMissingError) as exc:
            validate_scope(tmp_path / "no-such-store", SCOPE)

        assert "store root not found" in str(exc.value)
        assert "NOT 'the scope is clean'" in str(exc.value)

    def test_a_MISSING_SCOPE_DIRECTORY_is_two_empties_not_a_raise(
        self, store: Path
    ):
        """The other side of the pair above: the store IS there, this scope
        simply holds nothing yet. That is an honest empty, not an error."""
        assert validate_scope(store, "never-created") == ((), ())

    def test_an_EMPTY_scope_directory_is_two_empties_too(self, store: Path):
        (store / "hollow-area").mkdir()
        assert validate_scope(store, "hollow-area") == ((), ())

    def test_the_scope_name_is_FOLDED_before_the_directory_is_opened(
        self, store: Path
    ):
        """A caller spelling the scope `Widget_Cfg` must reach the same directory
        the loader's own folded key reaches. Without this the function answers
        `((), ())` — 'nothing here' — about the caller's OWN scope."""
        checked, malformed = validate_scope(store, "Widget_Cfg")
        assert checked == ("thing-alpha.md",)
        assert malformed == ()

    def test_a_malformed_entry_in_ANOTHER_scope_does_not_dirty_this_one(
        self, store: Path
    ):
        """The index is loaded whole, so the filter to one scope is load-bearing:
        without it a broken file anywhere in the store would make every scope
        report a rejection that is not in it."""
        (store / "gizmo-notes").mkdir()
        (store / "gizmo-notes" / "other-thing.md").write_text(
            _entry("other-thing", "gizmo-notes", extra="aliases: not-a-list")
        )

        checked, malformed = validate_scope(store, SCOPE)

        assert checked == ("thing-alpha.md",)
        assert malformed == ()


class TestTheREADMEIsNotAnEntryFile:
    """🔴 THE MISCOUNT THIS EXTRACTION FIXED, AND IT WAS LIVE.

    Every scope directory carries a `README.md` as its store-policy sheet and the
    loader skips it. The `cairn validate` verb globbed `*.md` for its denominator
    while taking its rejections from the loader — two walks, two populations — so
    a scope holding ONE entry beside its README printed `2 of 2 entry file(s)
    parse`, and with that entry malformed printed `1 of 2 … 1 malformed`: a file
    claimed to have parsed when none had, out of the one command whose entire job
    is to make a zero mean something.
    """

    def test_the_README_is_not_counted_as_walked(self, store: Path):
        (store / SCOPE / "README.md").write_text("# the scope's policy sheet\n")

        checked, malformed = validate_scope(store, SCOPE)

        assert checked == ("thing-alpha.md",), (
            "README.md is in `checked`, so the denominator counts a file the "
            "loader never parsed"
        )
        assert malformed == ()

    def test_a_scope_holding_ONLY_a_README_is_two_empties(self, store: Path):
        """The sharpest form: without the exclusion this scope reports one file
        walked and zero malformed — `1 of 1 entry file(s) parse` over a directory
        with no entries in it at all."""
        (store / "hollow-area").mkdir()
        (store / "hollow-area" / "README.md").write_text("# policy sheet\n")

        assert validate_scope(store, "hollow-area") == ((), ())

    def test_the_README_is_still_validatable_BY_NAME_if_you_ask_for_it(
        self, store: Path
    ):
        """⚠ THE EXCLUSION IS `validate_scope`'s, NOT `validate_entry_file`'s.
        The single-file entry point answers "would the loader accept THIS path",
        and a caller that hands it a README has asked a question with an answer.
        Pinned so the two are not "unified" into one rule that hides the other.
        """
        readme = store / SCOPE / "README.md"
        readme.write_text("# a policy sheet with no front matter\n")

        assert validate_entry_file(readme) is not None


class TestItRunsWhereThePodRuns:
    """🔴 NO GIT, AT IMPORT TIME OR AT CALL TIME — the condition that forced the
    extraction rather than a move.

    An agent pod's `kubectl exec` lands on `/`, which is not a checkout, and the
    writer module these functions came from needs git at import time for code this
    path never reaches. A test that merely ran them from a checkout would pass
    whether or not that dependency came along, so this one runs a subprocess with
    an EMPTY `PATH` (no `git` binary resolvable at all) and a cwd that is not a
    git repository.
    """

    def _run(self, cwd: Path, store: Path) -> subprocess.CompletedProcess:
        code = (
            "import sys\n"
            f"sys.path.insert(0, {str(ROOT / 'lib')!r})\n"
            "import entry_shape\n"
            f"print(entry_shape.validate_scope({str(store)!r}, {SCOPE!r}))\n"
            f"print(entry_shape.validate_entry_file("
            f"{str(store / SCOPE / 'thing-alpha.md')!r}))\n"
        )
        env = {
            "PATH": "",
            "HOME": str(cwd),
            "PYTHONDONTWRITEBYTECODE": "1",
        }
        return subprocess.run(
            [sys.executable, "-c", code],
            cwd=cwd,
            env=env,
            capture_output=True,
            text=True,
        )

    def test_the_cwd_used_is_REALLY_not_a_git_checkout(self, tmp_path: Path):
        """🔴 THE POSITIVE CONTROL FOR THE FIXTURE ITSELF. If `cwd` happened to
        sit inside a checkout the test above would prove nothing about the pod,
        and it would still be green — so the absence is measured, not assumed."""
        cwd = tmp_path / "not-a-checkout"
        cwd.mkdir()
        for parent in [cwd, *cwd.parents]:
            assert not (parent / ".git").exists(), f"{parent} is a git checkout"

    def test_git_is_REALLY_unreachable_under_the_env_this_test_uses(
        self, tmp_path: Path
    ):
        """🔴 THE SECOND CONTROL, ON THE OTHER HALF OF THE CLAIM. An empty `PATH`
        is only evidence if `git` is genuinely unresolvable under it — otherwise
        the run below proves the functions work WITH git available, which is the
        opposite of what it is quoted for."""
        cwd = tmp_path / "not-a-checkout"
        cwd.mkdir()
        probe = subprocess.run(
            [sys.executable, "-c", "import shutil,sys;sys.exit(0 if shutil.which('git') else 3)"],
            cwd=cwd,
            env={"PATH": "", "HOME": str(cwd)},
            capture_output=True,
            text=True,
        )
        assert probe.returncode == 3, (
            "`git` is still resolvable with PATH='' — the no-git claim below "
            f"would be vacuous (rc={probe.returncode})"
        )

    def test_both_functions_answer_from_a_non_git_cwd_with_no_git_on_PATH(
        self, store: Path, tmp_path: Path
    ):
        cwd = tmp_path / "not-a-checkout"
        cwd.mkdir()

        proc = self._run(cwd, store)

        assert proc.returncode == 0, proc.stderr
        assert "('thing-alpha.md',), ()" in proc.stdout, proc.stdout
        assert proc.stdout.rstrip().endswith("None"), proc.stdout

    def test_the_MODULE_IMPORTS_without_git_even_before_a_call(
        self, tmp_path: Path
    ):
        """🔴 IMPORT TIME IS ITS OWN FAILURE, and it is the one that forced this
        extraction: the writer module runs git while being imported, so it dies
        before any validation runs. Asserted separately from the call above
        because a passing call would mask which of the two was being proved."""
        cwd = tmp_path / "not-a-checkout"
        cwd.mkdir()
        proc = subprocess.run(
            [
                sys.executable,
                "-c",
                f"import sys;sys.path.insert(0, {str(ROOT / 'lib')!r});"
                "import entry_shape;print('imported')",
            ],
            cwd=cwd,
            env={"PATH": "", "HOME": str(cwd), "PYTHONDONTWRITEBYTECODE": "1"},
            capture_output=True,
            text=True,
        )
        assert proc.returncode == 0, proc.stderr
        assert proc.stdout.strip() == "imported"


class TestTheyAreExported:
    """⚠ AN INVARIANT GUARD. `__all__` is what a consumer vendoring these modules
    reads; a function missing from it is importable but undeclared, which is how
    a caller ends up reaching for the writer module again."""

    def test_both_functions_and_the_error_are_in___all__(self):
        for name in ("validate_entry_file", "validate_scope", "EntryFileMissingError"):
            assert name in entry_shape.__all__, name

    def test_nothing_in___all___is_missing_from_the_module(self):
        missing = [n for n in entry_shape.__all__ if not hasattr(entry_shape, n)]
        assert missing == [], missing


def test_the_functions_live_in_an_EXISTING_module():
    """🔴 NO NEW `lib/*.py` FILE FOR THIS. The consuming repo pins `lib/`'s module
    list in a ledger, so a new file here is not free — it has to grow that ledger
    at pin-bump time. These two went into the module that already holds the error
    taxonomy they raise, and whose import direction already points AT the resolver
    they need (the reverse would be a cycle).

    ⚠ An invariant guard. It pins WHERE, not how many: a module added for some
    other reason is nobody's business here.
    """
    assert validate_scope.__module__ == "entry_shape"
    assert validate_entry_file.__module__ == "entry_shape"
    assert EntryFileMissingError.__module__ == "entry_shape"


def test_validate_scope_does_not_WRITE_anything(tmp_path: Path):
    """READ-ONLY is in the docstring; this is the measurement. A post-write check
    that mutated the store would be changing the thing it is grading."""
    (tmp_path / SCOPE).mkdir()
    (tmp_path / SCOPE / "thing-alpha.md").write_text(_entry("thing-alpha", SCOPE))

    def snapshot() -> dict[str, tuple[int, bytes]]:
        return {
            str(p.relative_to(tmp_path)): (p.stat().st_size, p.read_bytes())
            for p in sorted(tmp_path.rglob("*"))
            if p.is_file()
        }

    before = snapshot()
    validate_scope(tmp_path, SCOPE)
    validate_entry_file(tmp_path / SCOPE / "thing-alpha.md")

    assert snapshot() == before
    assert not any(
        os.path.basename(str(p)).startswith(".") for p in tmp_path.rglob("*")
    ), "validation left a dotfile behind"
