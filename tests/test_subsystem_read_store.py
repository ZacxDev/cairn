#!/usr/bin/env python3
"""The host-local READ store: which directory, and can it date itself?

🔴 WHAT THIS FILE GUARDS, IN ONE SENTENCE. The Cairn cutover made a hosted pod
the canonical datastore, FROZE the per-host mirror at
`~/.claude/analyze-service-index` (entry files `0444`, nothing refreshes it) and
introduced a synced cache at `~/.cache/subsystem-store`. Nobody repointed the
READ path, so both prescribed read surfaces — `subsystem_recall.py`'s CLI (what
`/resume` step 4 runs) and `service_recon.py`'s recon (what `/analyze-service`
runs) — went on reading the frozen copy. MEASURED 2026-09-02 on the workbench:
the frozen mirror served **26** `devrc/` entries and the cache **29**, and the
frozen one printed "ALL 26 entries in `devrc/`, none omitted" with no staleness
stamp anywhere in the output. A completeness claim, about a store that had
stopped moving, with nothing in the render able to say so.

🔴 THE DISCRIMINATOR IS THE STAMP, NOT THE PATH. `cairn sync` writes
`.sync-stamp` into the cache; the frozen mirror has none. So the guard is
"REFUSE a store that cannot date itself", which is strictly wider than "do not
read that one path" — it also catches a cache that was never synced on a fresh
host, and it cannot be walked by moving the frozen tree somewhere else.

🔴 THE LIBRARY IS DELIBERATELY EXEMPT, AND `TestThePodContractIsUnchanged`
BELOW IS THE PIN. `subsystem-store-api/server.py` imports `subsystem_recall` as
a library (`rc.recall`, `rc.search`, `rc.load_store`) and serves `/data`, which
has no `.sync-stamp` and never will; `scripts/cairn` imports it the same way. A
refusal inside `recall()` would take down the pod AND the very client whose sync
produces the stamp — the whole store, offline, in one commit. Verified by grep
before this file was written: neither shells the CLI, neither calls `rc.main`.
"""

from __future__ import annotations

import ast
import dataclasses
import importlib.machinery
import importlib.util
import json
import sys
from pathlib import Path

import pytest

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT / "lib"))

import subsystem_read_store as rs  # noqa: E402
import subsystem_recall as rc  # noqa: E402
import entry_shape as st  # the reader/writer vocabulary

SCOPE = "workbench-cfg"

# The stamp `cairn sync` actually writes, field-for-field. Values are synthetic
# but the SHAPE is the writer's — a stamp missing `coverage` or `entries` would
# be a different artifact and would not exercise the multi-line header render.
STAMP_LINES = (
    "synced=1788363567",
    "revision=r-fixture-9",
    "snapshot=seeded=2026-09-01T20:38:36Z staged_entries=49 newest=2026-09-02T15:38:28Z",
    "entries=201",
    "coverage=ALL",
)


def _entry(service: str, scope: str) -> str:
    return "\n".join(
        [
            "---",
            f"service: {service}",
            f"scope: {scope}",
            "---",
            "",
            "## What it is",
            "A durable thing a recall block MUST name.",
            "",
            "## Pointers",
            "- ops skill `manage-widget` — invoke it for restarts",
            "",
            "## Nuance / work-history",
            "- 2026-01-02: the readiness probe lies for 40s after a reload.",
            "",
        ]
    )


def _store(root: Path, *, stamped: bool) -> Path:
    """A real store, with or without the stamp. Nothing else differs."""
    store = root / "store"
    scope = store / SCOPE
    scope.mkdir(parents=True)
    (scope / "collector.md").write_text(_entry("collector", SCOPE), encoding="utf-8")
    if stamped:
        (store / rs.SYNC_STAMP).write_text("\n".join(STAMP_LINES) + "\n", encoding="utf-8")
    return store


@pytest.fixture()
def repointed(tmp_path: Path, monkeypatch: pytest.MonkeyPatch):
    """Repoint the DEFAULT read-store resolution at a tmp directory.

    🔴 EVERY TEST BELOW THAT EXERCISES THE DEFAULT USES THIS. Without it the
    default resolves to the operator's real `~/.cache/subsystem-store` and the
    suite would read — and report on — a live, client-confidential store.
    `read_store_root()` reads the module global at call time precisely so this
    one assignment reaches every consumer.
    """

    def _point_at(store: Path) -> Path:
        monkeypatch.setattr(rs, "DEFAULT_CACHE_ROOT", store)
        return store

    return _point_at


# =============================================================================
# THE WIRE CONSTANTS. One ledger, because this defect kept coming back.
# =============================================================================
#
# 🔴 THIS CLASS EXISTS BECAUSE THE SAME FINDING RECURRED FIVE TIMES.
# `SYNC_STAMP`, then `REMEDY` and `DEFAULT_CACHE_ROOT`, then `NOT_READ_PREFIX`
# and `INDEX_UNSTAMPED` — every one asserted only as `<constant> in <output>`,
# i.e. the constant agreeing with itself, and every one SURVIVING a rename
# against a fully green suite. Fixing them one at a time is what let the fourth
# happen: the F1 fix introduced `NOT_READ_PREFIX` unpinned in the same session
# that pinned the other three.
#
# So the remedy is a LEDGER rather than another one-off pin. Each entry maps a
# constant to the literal it must equal, and `test_the_ledger_is_two_way` fails
# when the new module grows a constant nobody put here — which is the only part
# that can stop the sixth.
#
# 🔴 THE FIFTH HID INSIDE THE LEDGER ITSELF. The sweep was `isupper() and
# isinstance(value, str)` while its docstring claimed the module's "whole
# surface" was in scope. MEASURED: `FROZEN_MIRROR = Path.home() / ".claude" /
# "analyze-service-index"` (a `Path`), `STAMP_FIELDS = ("synced", …)` (a tuple)
# and `Wire_Fact_v2 = "another-status"` (mixed case) each SURVIVED it — and
# `DEFAULT_CACHE_ROOT`, the most load-bearing wire fact in the module, is a
# `Path` and was therefore invisible to the sweep, surviving only on a
# hand-written pin below: exactly the one-off pattern the ledger replaces. The
# sweep now enumerates module-scope ASSIGNMENTS from the SOURCE, so the shape of
# the value cannot excuse one.
#
#: `(module, attribute, the value it MUST equal)`. The expected value is written
#: out HERE — never read back off the module — so no entry can be satisfied by a
#: constant agreeing with itself.
WIRE_CONSTANTS: tuple[tuple[object, str, object], ...] = (
    # A FILENAME on disk, written by a deployed `cairn sync`.
    (rs, "SYNC_STAMP", ".sync-stamp"),
    # A COMMAND a human is told to type, by the refusal and by two SKILL.mds.
    (rs, "REMEDY", "cairn sync"),
    # An EXIT CODE two CLIs return and a script branches on. Not a `str` either,
    # and the number is written out here rather than read off `subsystem_recall`
    # — which now imports it — so the pin cannot be satisfied by the two agreeing
    # with each other while both drifted from the documented contract.
    (rs, "EXIT_UNSTAMPED_READ_STORE", 4),
    # A DIRECTORY every reader resolves and `scripts/cairn` writes. Not a `str`,
    # which is precisely why the old sweep could not see it.
    (rs, "DEFAULT_CACHE_ROOT", Path.home() / ".cache" / "subsystem-store"),
    # A RENDERED TOKEN two renderers emit and `analyze-service/SKILL.md` tells
    # the reader to relay ("the `stamp:` lines").
    (rs, "STAMP_PREFIX", "  stamp: "),
)

#: Module-scope names of `subsystem_read_store` that are NOT wire facts.
#: Enumerated, not pattern-matched: an unlisted one is a wire fact by default,
#: which is the direction that fails safe.
NOT_WIRE_FACTS: frozenset[str] = frozenset()


#: Statements that carry MODULE scope into their bodies. A name bound inside one
#: of these at a module's top level is a module attribute exactly like one bound
#: beside it — Python has no block scope. `FunctionDef`/`ClassDef`/`Lambda` are
#: deliberately absent: those DO introduce a scope, and descending into them is
#: what makes a sweep report function locals (N3).
_SCOPE_TRANSPARENT: tuple[type[ast.stmt], ...] = tuple(
    node
    for node in (
        getattr(ast, name, None)
        for name in ("If", "Try", "TryStar", "With", "AsyncWith", "For", "AsyncFor", "While")
    )
    if node is not None
)


def _module_scope_statements(body: list[ast.stmt]):
    """Every statement executing at module scope, including nested block bodies."""
    for node in body:
        yield node
        if isinstance(node, _SCOPE_TRANSPARENT):
            yield from _module_scope_statements(list(node.body))
            yield from _module_scope_statements(list(getattr(node, "orelse", [])))
            yield from _module_scope_statements(list(getattr(node, "finalbody", [])))
            for handler in getattr(node, "handlers", []):
                yield from _module_scope_statements(list(handler.body))


def _bound_names(target: ast.expr):
    """Every name a single assignment TARGET binds, unpacking tuples and lists."""
    if isinstance(target, ast.Name):
        yield target.id
    elif isinstance(target, (ast.Tuple, ast.List)):
        for element in target.elts:
            yield from _bound_names(element)
    elif isinstance(target, ast.Starred):
        yield from _bound_names(target.value)


#: Nodes that open a NEW scope, so a binding inside them is not a module
#: attribute. A `lambda` is here for the same reason a `def` is.
_NEW_SCOPE = (ast.FunctionDef, ast.AsyncFunctionDef, ast.ClassDef, ast.Lambda)


def _walrus_names(node: ast.AST):
    """Names bound by `:=` in this node's own scope, comprehensions included."""
    for child in ast.iter_child_nodes(node):
        if isinstance(child, _NEW_SCOPE):
            continue
        if isinstance(child, ast.NamedExpr):
            yield from _bound_names(child.target)
        yield from _walrus_names(child)


def _module_scope_assignments(path: Path) -> set[str]:
    """Every non-underscore name ASSIGNED at a module's top level, from source.

    🔴 READ OFF THE AST, NOT `vars()`. `vars()` cannot tell a constant this
    module declares from a name it imported, so a sweep over it has to filter by
    NAME SHAPE (`isupper()`) and VALUE TYPE (`isinstance(..., str)`) — and both
    filters are holes: a `Path`, a tuple, a frozenset or a mixed-case name walks
    straight through. An assignment is an assignment whatever it holds.

    🔴 "TOP LEVEL" IS NOT "IN `tree.body`", AND THAT GAP WAS THE FIFTH RECURRENCE
    OF THIS FILE'S OWN DEFECT CLASS. This walked `tree.body` and required
    `isinstance(target, ast.Name)` under the sentence above. Python has no block
    scope, so a name bound inside a top-level `try:`/`if:`/`with:`/`for:` — or by
    a tuple unpack — is a module attribute exactly like one bound beside it, and
    MEASURED, all three SURVIVED: `FROZEN_MIRROR = …` in a `try:` body,
    `LEGACY_STATUS = …` under `if True:`, and `LEGACY_STATUS, LEGACY_REMEDY = …`.
    So the walk now follows module scope wherever it goes and unpacks targets.
    It still does NOT enter a `def`/`class` body: those really are new scopes,
    and a sweep that entered them would report function locals as
    re-declarations.

    🔴 AND "ASSIGNED" IS NOT ONLY `=`. Widening to the block bodies above and
    stopping there reproduced the defect one level in: MEASURED,
    `for [LOOP_STATUS] in [["store-frozen"]]:` at module scope SURVIVED the
    widened walk, because a `for` TARGET is a binding this collected no targets
    from. Every statement-level binding form that leaves a DURABLE module
    attribute is now collected — `for … in`, `with … as`, and a `:=` anywhere in
    a module-scope expression. `except … as` is excluded ON PURPOSE; the reason
    is in the body. KNOWN RESIDUAL, stated rather than discovered: a `:=` inside
    a module-scope `def`'s decorator or default argument is not seen, because
    that hangs off a node this walk skips whole.
    """
    tree = ast.parse(path.read_text(encoding="utf-8"))
    found: set[str] = set()
    for node in _module_scope_statements(list(tree.body)):
        targets: list[ast.expr] = []
        if isinstance(node, ast.Assign):
            targets = list(node.targets)
        elif isinstance(node, ast.AnnAssign):
            targets = [node.target]
        elif isinstance(node, (ast.For, ast.AsyncFor)):
            targets = [node.target]
        elif isinstance(node, (ast.With, ast.AsyncWith)):
            targets = [it.optional_vars for it in node.items if it.optional_vars]
        for target in targets:
            for name in _bound_names(target):
                if not name.startswith("_"):
                    found.add(name)
        # 🔴 `except … as NAME` is DELIBERATELY NOT COLLECTED. Python unbinds it
        # at the end of the handler, so it is never a module attribute — and
        # collecting it would make this sweep report a name the `hasattr` check
        # below can never find, i.e. a false RED on an ordinary
        # `except OSError as exc:` at module scope.
        # `:=` binds in the ENCLOSING scope — including from inside a
        # comprehension, which is why an expression walk is the only way to see
        # it. A `def`/`class` statement is skipped WHOLE (not merely its body):
        # walking it here would report every walrus in the function as a module
        # attribute, which is the N3 over-width defect in a new place.
        if not isinstance(node, _NEW_SCOPE):
            for name in _walrus_names(node):
                if not name.startswith("_"):
                    found.add(name)
    return found


def _module_scope_bindings(path: Path) -> set[str]:
    """Every non-underscore module attribute a file DEFINES: assignments AND
    top-level `def`/`class` names.

    Kept separate from `_module_scope_assignments` because the two answer
    different questions. The ledger asks "which CONSTANTS must be pinned", so a
    function name there would demand a `WIRE_CONSTANTS` row for every `def`. The
    re-declaration guard asks "which names must a consumer IMPORT rather than
    define", and a `def` is as re-declarable as a constant.
    """
    tree = ast.parse(path.read_text(encoding="utf-8"))
    defined = {
        node.name
        for node in _module_scope_statements(list(tree.body))
        if isinstance(node, (ast.FunctionDef, ast.AsyncFunctionDef, ast.ClassDef))
        and not node.name.startswith("_")
    }
    return _module_scope_assignments(path) | defined


class TestTheWireConstants:
    """Every string that crosses a boundary, pinned to its literal."""

    @pytest.mark.parametrize(
        "module, name, literal",
        [(m, n, lit) for m, n, lit in WIRE_CONSTANTS],
        ids=[f"{n}" for _m, n, _lit in WIRE_CONSTANTS],
    )
    def test_the_constant_equals_its_literal(self, module, name, literal) -> None:
        """The literal is written out HERE, so the assertion cannot be satisfied
        by the constant being consistent with itself."""
        assert getattr(module, name) == literal

    def test_the_ledger_is_two_way_over_the_new_module(self) -> None:
        """🔴 THE HALF THAT STOPS THE NEXT ONE.

        Pinning five constants does nothing about the sixth. This enumerates
        every module-scope ASSIGNMENT in `subsystem_read_store` — the module this
        change introduced, so its whole surface really is in scope — and requires
        each to be either in `WIRE_CONSTANTS` or explicitly excused. Adding one
        without a pin fails here.

        🔴 SHAPE-BLIND ON PURPOSE. The previous version filtered `isupper() and
        isinstance(value, str)` under this same docstring, so a wire fact
        arriving as a `Path`, a tuple, a frozenset or a mixed-case name passed
        silently — and `DEFAULT_CACHE_ROOT` was already such a fact, covered only
        by a hand-written pin. Reading assignments out of the SOURCE removes both
        filters: what a constant HOLDS stops being an escape hatch.

        `service_recon` is deliberately NOT swept: it predates this change and
        carries many string constants that are not wire facts, so a sweep there
        would either be noise or need an exclusion list long enough to hide a
        real omission. Its two are pinned by name above.
        """
        source = ROOT / "lib" / "subsystem_read_store.py"
        declared = _module_scope_assignments(source)
        # POSITIVE CONTROL. A walk that returned nothing — wrong path, a parse
        # that found no `Assign` — would report a clean sweep over zero names,
        # which is the silent zero this whole change exists to kill.
        assert "SYNC_STAMP" in declared, declared
        # Every swept name must really be on the module: an AST name the module
        # does not expose would mean the sweep and the import disagree.
        for name in declared:
            assert hasattr(rs, name), f"{name} is assigned but not importable"

        pinned = {n for m, n, _lit in WIRE_CONSTANTS if m is rs}
        assert declared - pinned - NOT_WIRE_FACTS == set(), (
            f"`subsystem_read_store` grew module-scope constant(s) "
            f"{sorted(declared - pinned - NOT_WIRE_FACTS)} with no pin. "
            f"Add them to WIRE_CONSTANTS, or to NOT_WIRE_FACTS with a reason."
        )
        assert pinned <= declared, (
            f"WIRE_CONSTANTS names {sorted(pinned - declared)}, which "
            f"`subsystem_read_store` no longer declares."
        )

    def test_the_analyze_service_step_does_NOT_chain_the_sync_with_double_ampersand(
        self,
    ) -> None:
        """🔴 `&&` HERE DELETES THE WHOLE BRIEF, AND THE PROSE PROMISES OTHERWISE.

        `cairn sync` exits 4 whenever the pod is unreachable but a usable cache
        survives — that is `cmd_sync`'s stated contract, not an edge case. Under
        `&&` that non-zero short-circuits and `service_recon.py` never runs, so
        `/analyze-service` emits NOTHING during an outage, exactly when the
        stamped cache would have served the index at full fidelity. Eight lines
        below, the same file promises "a failed sync costs you the index block,
        never the brief".

        Pinned on the COMMAND, because nothing else could: the REMEDY test only
        requires the substring `cairn sync`, which `&&` satisfies.
        """
        doc = (ROOT / "claude/skills/analyze-service/SKILL.md").read_text(encoding="utf-8")
        step = [
            ln for ln in doc.splitlines()
            if rs.REMEDY in ln and "service_recon.py" in ln
        ]
        assert len(step) == 1, f"expected one step-1 command line, found {step}"
        assert "&&" not in step[0], (
            f"step 1 chains the sync with `&&`: {step[0]!r}. A failed sync then "
            f"suppresses the entire recon. Use `;`."
        )
        assert f"{rs.REMEDY};" in step[0], (
            f"step 1 must separate the two commands with `;`: {step[0]!r}"
        )
        # …and the promise the separator has to keep is still there to keep.
        assert "costs you the index block, never the brief" in doc

    def test_cairns_cache_default_FOLLOWS_a_repointed_resolver(
        self, tmp_path: Path, monkeypatch: pytest.MonkeyPatch
    ) -> None:
        """🔴 THE WRITER MUST MOVE WITH THE READER.

        `scripts/cairn` used to do `from subsystem_read_store import
        DEFAULT_CACHE_ROOT as DEFAULT_CACHE`, which binds at IMPORT — the exact
        spelling `read_store_root`'s docstring warns defeats repointing. Nothing
        depended on it yet, so the hazard was invisible: a future cairn test
        repointing the resolver would have moved the reader and left cairn
        writing to the operator's real cache. Asserted behaviourally, through
        cairn's own parser, rather than by grepping for the import shape.
        """
        path = ROOT / "cairn"
        assert "DEFAULT_CACHE_ROOT as DEFAULT_CACHE" not in path.read_text(encoding="utf-8")
        # `scripts/cairn` has no `.py` suffix, so `spec_from_file_location`
        # returns None. Same loader-less exec the cairn suites use.
        spec = importlib.util.spec_from_loader(
            "cairn_cache_default_probe", loader=None, origin=str(path)
        )
        module = importlib.util.module_from_spec(spec)
        module.__file__ = str(path)
        exec(compile(path.read_text(encoding="utf-8"), str(path), "exec"), module.__dict__)
        monkeypatch.setattr(module._read_store, "DEFAULT_CACHE_ROOT", tmp_path / "moved")
        assert module.build_parser().parse_args(["sync"]).cache == tmp_path / "moved"



# =============================================================================
# The resolver itself.
# =============================================================================


class TestTheResolver:
    def test_a_stamped_store_reports_its_stamp_UNPARSED(self, tmp_path: Path) -> None:
        store = _store(tmp_path, stamped=True)
        got = rs.resolve_read_store(store)
        assert got.stamped is True
        assert got.reason is None
        assert got.stamp == STAMP_LINES  # unparsed, in file order

    def test_an_unstamped_store_is_not_stamped_and_says_why(self, tmp_path: Path) -> None:
        store = _store(tmp_path, stamped=False)
        got = rs.resolve_read_store(store)
        assert got.stamped is False
        assert got.stamp is None
        assert rs.SYNC_STAMP in (got.reason or "")

    def test_an_EMPTY_stamp_is_ABSENT_not_a_stamp_with_no_fields(self, tmp_path: Path) -> None:
        """🔴 "The store is stamped" must not be satisfiable by a zero-byte file.

        A truncated write is a real failure mode for a file `cairn sync` creates
        during an interrupted sync, and an empty stamp would otherwise pass the
        guard while carrying no freshness at all — the exact silent-zero shape
        the refusal exists to prevent.
        """
        store = _store(tmp_path, stamped=False)
        (store / rs.SYNC_STAMP).write_text("\n  \n\n", encoding="utf-8")
        got = rs.resolve_read_store(store)
        assert got.stamped is False
        assert "empty" in (got.reason or "")


    def test_the_cache_root_is_ANCHORED_AT_HOME(self) -> None:
        """🔴 THE `$HOME` ANCHOR WAS THE UNPINNED HALF, AND IT IS THE DANGEROUS ONE.

        MEASURED: `Path("/var/tmp") / ".cache" / "subsystem-store"` SURVIVED all
        668 tests. The two assertions above check the tail; `read_store_root() ==
        DEFAULT_CACHE_ROOT` is the constant agreeing with itself. Nothing checked
        the anchor — so a mutant put a CLIENT-CONFIDENTIAL store in a
        world-writable directory, and `scripts/cairn` imports this same constant
        for its `--cache` default, so the WRITER would have followed it there.

        🔴 THE LITERAL ITSELF NOW LIVES IN `WIRE_CONSTANTS`, NOT HERE. This test
        held the only pin on it, which is exactly the one-off shape the ledger
        exists to replace — and because the constant is a `Path`, the ledger's
        old `isinstance(..., str)` sweep could not see that it was unpinned. The
        two-way sweep fails if the entry is ever removed, so deleting the ledger
        row cannot quietly re-open this. What stays here is the PROPERTY the
        `/var/tmp` mutant violated and a literal cannot state on its own.
        """
        assert rs.DEFAULT_CACHE_ROOT.is_relative_to(Path.home())
        assert rs.DEFAULT_CACHE_ROOT != Path.home()

    def test_the_REMEDY_names_a_subcommand_cairn_ACTUALLY_HAS(self) -> None:
        """🔴 THE REMEDY IS A WIRE FACT: it is the one thing the refusal tells a
        human to TYPE.

        MEASURED: `REMEDY = "cairn resync-the-store"` SURVIVED all 668 tests,
        because every assertion was `rs.REMEDY in msg` — the message quoting the
        constant that produced it. Two SKILL.md files hardcode `cairn sync` in
        prose with nothing cross-checking, so the prose and the code could drift
        apart silently in either direction.

        Pinned three ways: the literal; that `scripts/cairn` really declares that
        subparser (so a renamed subcommand fails HERE rather than at a user's
        terminal); and that both prescribing skills say the same words.
        """
        assert rs.REMEDY == "cairn sync"
        prog, _, verb = rs.REMEDY.partition(" ")
        assert prog == "cairn"
        cairn = (ROOT / "cairn").read_text(encoding="utf-8")
        assert f'sub.add_parser("{verb}"' in cairn, (
            f"the refusal tells a human to run `{rs.REMEDY}`, but `scripts/cairn` "
            f"declares no `{verb}` subcommand."
        )
        for rel in (
            "claude/skills/resume/SKILL.md",
            "claude/skills/analyze-service/SKILL.md",
        ):
            doc = (ROOT / rel).read_text(encoding="utf-8")
            assert rs.REMEDY in doc, f"{rel} no longer prescribes `{rs.REMEDY}`"

    def test_the_stamp_FILENAME_is_pinned_to_the_literal_cairn_writes(
        self, tmp_path: Path
    ) -> None:
        """🔴 FOUND BY THE MUTATION SWEEP, AND IT IS A REAL GAP.

        Every fixture in this file writes the stamp through `rs.SYNC_STAMP`, so
        a mutant that RENAMES the constant renames the fixture with it and the
        suite stays green: MEASURED, `SYNC_STAMP = ".sync-stampX"` SURVIVED all
        615 tests in the three files that cover this change. That is the
        expectation-derived-from-the-implementation shape — the suite could not
        tell the two names apart because it never named either.

        The filename is a WIRE fact, not an internal choice. Caches already on
        disk were written by a deployed `cairn sync`; a reader looking for a
        different name reports every one of them as unstamped and refuses the
        lot. So it is pinned to the literal, and the behavioural half writes the
        literal by hand — never through the constant — so the pin cannot be
        satisfied by the constant agreeing with itself.
        """
        assert rs.SYNC_STAMP == ".sync-stamp"
        store = _store(tmp_path, stamped=False)
        (store / ".sync-stamp").write_text("synced=1\n", encoding="utf-8")
        got = rs.resolve_read_store(store)
        assert got.stamped is True
        assert got.stamp == ("synced=1",)

    def test_the_ONLY_normalisation_is_trailing_whitespace_and_blank_lines(
        self, tmp_path: Path
    ) -> None:
        """🔴 THE DOCSTRING SAID "VERBATIM" WHILE THE BODY `rstrip()`s.

        No test could see the difference — every fixture writes clean lines — so
        the word survived review. Rather than pick one and hope, this pins the
        normalisation itself: trailing whitespace goes (a `\\r` from a CRLF write
        must not reach the rendered header), blank lines go, and NOTHING else
        moves — order, LEADING whitespace, `=` signs, duplicate keys and unknown
        fields all survive untouched, because this module does not own the
        stamp's schema.
        """
        store = _store(tmp_path, stamped=False)
        (store / rs.SYNC_STAMP).write_text(
            "synced=1   \r\n"          # trailing spaces + CR
            "\n"                        # blank line
            "  indented=kept\n"         # LEADING whitespace survives
            "coverage=ALL\n"
            "coverage=DUPLICATE\n"      # duplicate key survives, in order
            "unknown-field=passed through\n",
            encoding="utf-8",
        )
        got = rs.resolve_read_store(store)
        assert got.stamp == (
            "synced=1",
            "  indented=kept",
            "coverage=ALL",
            "coverage=DUPLICATE",
            "unknown-field=passed through",
        )

    def test_the_refusal_names_the_store_the_reason_and_the_remedy(self, tmp_path: Path) -> None:
        """A refusal that does not say what to do next is a dead end.

        All three are asserted because a message with any one missing still
        reads like a complete sentence — which is how an unactionable error
        survives review.
        """
        store = _store(tmp_path, stamped=False)
        msg = rs.refusal_message("prog", rs.resolve_read_store(store))
        assert str(store) in msg
        assert rs.SYNC_STAMP in msg
        assert rs.REMEDY in msg
        assert msg.startswith("prog: ")

    def test_the_constants_have_exactly_one_definition(self) -> None:
        """🔴 `scripts/cairn` IMPORTS these; it must not re-declare them.

        The cache path and the stamp name lived in `cairn` alone, which is why
        the readers could not see them — a second copy in a reader would have
        been a second thing to keep in step, and the whole defect is the two
        sides disagreeing.

        🔴 THE ARM THIS REPLACES GUARDED A SPELLING THAT CAN NO LONGER EXIST.
        It matched the TEXT `'DEFAULT_CACHE = Path.home()'`, and the round that
        deleted the `DEFAULT_CACHE` name from `cairn` left the check behind
        matching nothing: MEASURED, re-declaring `DEFAULT_CACHE_ROOT =
        Path.home() / ".cache" / "subsystem-store"` at `cairn`'s module scope
        SURVIVED it. Reading as text also made the arm sensitive to whitespace
        and blind to prose containing the same characters, so it is now an AST
        walk. Parsed rather than imported because `cairn` has no `.py` suffix and
        importing it would run its argparse module scope.

        🔴 `__all__` IS A SECOND LIST, AND THE ARM THAT SAID OTHERWISE COULD NOT
        SEE IT DRIFT. The forbidden set was `set(rs.__all__)` alone under the
        sentence "no second list to keep in step" — but `__all__` is exported by
        hand, and it starts with `_`, so the two-way ledger above deliberately
        excludes it and nothing anywhere reconciles the two. MEASURED, both
        SURVIVED: a `SECOND_STAMP` added to the module and to `WIRE_CONSTANTS`
        (ledger green) but omitted from `__all__`, then re-declared verbatim in
        `cairn`; and deleting `stamp_header` from `__all__` and defining a second
        one in `cairn`. So the forbidden set is the UNION of `__all__` and what
        the module actually DEFINES at module scope. It covers `def`s and classes
        as well as constants, because the second survivor above was a `def` and a
        set that stopped at constants would have left it standing — the same
        "narrower than the sentence above it" shape as the sweep itself.

        🔴 THE CAIRN SIDE IS MODULE SCOPE, NOT EVERY DEPTH. The old walk was
        `ast.walk`, so a FUNCTION-LOCAL in `cairn` named after any exported symbol
        tripped it. MEASURED: a helper assigning `stamp_header = [...]` turned
        this red with "`scripts/cairn` assigns ['stamp_header'], which it must
        IMPORT" — a re-declaration claim about a local, and not hypothetical:
        `subsystem_recall.main` already uses `stamp_header` as exactly such a
        local. A local shadows nothing another module reads; the hazard this
        guards is a second MODULE-SCOPE definition.
        """
        path = ROOT / "cairn"
        src = path.read_text(encoding="utf-8")
        assert "from subsystem_read_store import" in src
        assigned = _module_scope_bindings(path)
        # POSITIVE CONTROL: the walk really saw `cairn`'s definitions, so an
        # empty intersection below means "no re-declaration", not "no parse".
        # Both halves, because they are collected by different code paths.
        assert "EXIT_REFRESH_FAILED" in assigned, sorted(assigned)[:20]
        assert "cmd_sync" in assigned, sorted(assigned)[:20]
        swept = _module_scope_bindings(
            ROOT / "lib" / "subsystem_read_store.py"
        )
        # POSITIVE CONTROL on the half `__all__` cannot supply: the sweep really
        # read names off the module, so a union that quietly collapsed back to
        # `__all__` alone fails here rather than passing as a clean run.
        assert {"SYNC_STAMP", "stamp_header"} <= swept, sorted(swept)
        clash = assigned & (set(rs.__all__) | swept)
        assert clash == set(), (
            f"`scripts/cairn` assigns {sorted(clash)}, which it must IMPORT from "
            f"`subsystem_read_store` — a second copy is a second thing to keep in "
            f"step, and the readers cannot see it."
        )
        # The pre-cutover name is gone and must not come back under its old
        # spelling either; that one is dead, so it is asserted as text.
        assert "DEFAULT_CACHE = " not in src


# =============================================================================
# The CLI: refuse the default when it cannot date itself.
# =============================================================================


class TestTheCliRefusesAnUnstampedDefaultStore:
    def test_an_unstamped_DEFAULT_store_is_refused_and_renders_no_index(
        self, tmp_path: Path, repointed, capsys
    ) -> None:
        """🔴 (a) THE HEADLINE CASE. Non-zero exit, and — the half that actually
        matters — NOTHING that could be mistaken for an index."""
        repointed(_store(tmp_path, stamped=False))
        rc_code = rc.main(["--scope", SCOPE])
        cap = capsys.readouterr()
        assert rc_code == rc.EXIT_UNSTAMPED_READ_STORE
        assert rc_code != 0
        # The refusal is unmistakable: it is on stderr and it names the remedy.
        assert "REFUSING" in cap.err
        assert rs.REMEDY in cap.err
        # 🔴 AND NOTHING RENDERED. A refusal that still printed the digest would
        # be an advisory, and an advisory is exactly what the frozen mirror
        # already had (none) — the caller must not be able to read an index here.
        assert cap.out.strip() == ""
        assert "subsystem-recall: status=" not in cap.out
        assert "collector" not in cap.out

    def test_an_unstamped_default_is_refused_for_search_too(
        self, tmp_path: Path, repointed, capsys
    ) -> None:
        """The guard sits before the search/recall fork, so BOTH read surfaces
        of this CLI are covered by one check rather than one each."""
        repointed(_store(tmp_path, stamped=False))
        assert rc.main(["--scope", SCOPE, "--search", "readiness"]) == (
            rc.EXIT_UNSTAMPED_READ_STORE
        )
        cap = capsys.readouterr()
        assert "REFUSING" in cap.err
        assert cap.out.strip() == ""

    def test_the_refusal_exit_code_is_NOT_the_store_broken_code(
        self, tmp_path: Path, repointed, capsys
    ) -> None:
        """🔴 THE MUTATION-TEST IDENTITY OF THIS GUARD.

        3 already means "nothing readable is there, recall was unavailable".
        This is a different fact with a different one-command remedy, and a
        caller that cannot tell them apart cannot act on it. Asserted so a
        mutant that returns 3 (or 2, or 0) here dies on THIS guard's own code
        rather than passing for somebody else's reason.
        """
        repointed(_store(tmp_path, stamped=False))
        assert rc.main(["--scope", SCOPE]) == 4
        capsys.readouterr()
        assert rc.EXIT_UNSTAMPED_READ_STORE not in (0, 2, 3)

    def test_the_guard_is_REACHABLE_no_earlier_check_wins(
        self, tmp_path: Path, repointed, capsys
    ) -> None:
        """🔴 REACHABILITY, not just breakability.

        Every check ahead of this one rejects a malformed COMMAND (two selectors,
        `--limit` with `--list`, `--page` with `--ref`, a search-only flag with
        no `--search`). This input trips none of them: it is a well-formed
        command whose ONLY problem is the store it would read. If an earlier
        check ever grew wide enough to swallow it, the exit code here would stop
        being 4 and this test says so.
        """
        repointed(_store(tmp_path, stamped=False))
        # A flag combination the parser accepts outright.
        assert rc.main(["--scope", SCOPE, "--list"]) == rc.EXIT_UNSTAMPED_READ_STORE
        cap = capsys.readouterr()
        assert "REFUSING" in cap.err
        # The proof that this argv is otherwise-VALID — and not merely rejected
        # by something else — is the next test: same argv, stamped store, exit 0.

    def test_the_same_command_succeeds_once_the_store_is_stamped(
        self, tmp_path: Path, repointed, capsys
    ) -> None:
        """The positive half of the reachability pair above: same argv, same
        store contents, stamp present ⇒ exit 0 and a real index."""
        repointed(_store(tmp_path, stamped=True))
        assert rc.main(["--scope", SCOPE, "--list"]) == 0
        out = capsys.readouterr().out
        assert "collector" in out

    def test_a_flag_error_still_beats_the_store_guard(
        self, tmp_path: Path, repointed, capsys
    ) -> None:
        """Guard ORDER, pinned. A caller who passed two selectors gets told
        about the selectors — being additionally told to run `cairn sync` would
        describe a store the command was never going to read."""
        repointed(_store(tmp_path, stamped=False))
        assert rc.main(["--scope", SCOPE, "--list", "--ref", "collector"]) == 2
        cap = capsys.readouterr()
        assert "select different things" in cap.err
        assert "REFUSING" not in cap.err


class TestAStampedDefaultStoreCarriesItsFreshness:
    def test_the_header_carries_every_stamp_line_UNPARSED(
        self, tmp_path: Path, repointed, capsys
    ) -> None:
        """🔴 (b) UNPARSED, and in the HEADER — beside `store:` and `store host:`.
        (This said "VERBATIM" while the reader `rstrip()`s each line; the word
        was retired in the module and left standing here.)

        Every line is asserted, not a sampled one: a render that dropped
        `coverage=ALL` would let a scope-filtered cache read as complete, and a
        render that dropped `synced=` would take the freshness back out of a
        read that exists to carry it.
        """
        repointed(_store(tmp_path, stamped=True))
        assert rc.main(["--scope", SCOPE]) == 0
        out = capsys.readouterr().out
        for line in STAMP_LINES:
            assert f"  stamp: {line}" in out, line
        head = out.splitlines()
        store_at = next(i for i, ln in enumerate(head) if ln.startswith("  store: "))
        caveat_at = next(i for i, ln in enumerate(head) if ln.startswith("  caveat: "))
        stamp_at = [i for i, ln in enumerate(head) if ln.startswith("  stamp: ")]
        assert stamp_at, out
        assert store_at < min(stamp_at) and max(stamp_at) < caveat_at, out

    def test_NO_AGE_IS_COMPUTED(self, tmp_path: Path, repointed, capsys) -> None:
        """🔴 `subsystem_recall` documents itself "no clock", and `cairn` owns
        `cache_age`. A second age here would be a second answer to one question.

        Asserted STRUCTURALLY — the reader's module imports no clock — rather
        than by grepping the output for a duration, because a duration string
        is a word another feature could spell.
        """
        src = (ROOT / "lib" / "subsystem_recall.py").read_text(encoding="utf-8")
        assert "\nimport time" not in src and "\nfrom time import" not in src
        assert "\nimport datetime" not in src and "\nfrom datetime import" not in src
        rd = (ROOT / "lib" / "subsystem_read_store.py").read_text(encoding="utf-8")
        assert "\nimport time" not in rd and "\nfrom time import" not in rd
        # And behaviourally: the stamp's own epoch is echoed, never converted.
        repointed(_store(tmp_path, stamped=True))
        assert rc.main(["--scope", SCOPE]) == 0
        out = capsys.readouterr().out
        assert "  stamp: synced=1788363567" in out

    def test_the_search_header_carries_the_stamp_too(
        self, tmp_path: Path, repointed, capsys
    ) -> None:
        repointed(_store(tmp_path, stamped=True))
        assert rc.main(["--scope", SCOPE, "--search", "readiness"]) == 0
        out = capsys.readouterr().out
        for line in STAMP_LINES:
            assert f"  stamp: {line}" in out, line

    def test_the_json_payload_carries_the_stamp_UNPARSED(
        self, tmp_path: Path, repointed, capsys
    ) -> None:
        """A `--json` consumer has no header block, so without this it is the
        one reader that gets no freshness — in the format least likely to be
        eyeballed."""
        repointed(_store(tmp_path, stamped=True))
        assert rc.main(["--scope", SCOPE, "--json"]) == 0
        blob = json.loads(capsys.readouterr().out)
        assert blob["read_store_stamp"] == list(STAMP_LINES)


class TestAnExplicitStoreStaysPermissive:
    def test_an_explicit_store_at_an_unstamped_path_still_serves(
        self, tmp_path: Path, repointed, capsys
    ) -> None:
        """🔴 (c) THE OPERATOR NAMED A PATH. Every fixture in this repo does
        this, `prune-index` prescribes it, and the store-api's `/data` is
        exactly it — the refusal is about the DEFAULT resolution only."""
        # The default is repointed at an unstamped store as well, so a pass here
        # cannot come from the default happening to be fine.
        repointed(_store(tmp_path / "default", stamped=False))
        elsewhere = _store(tmp_path / "named", stamped=False)
        assert rc.main(["--store", str(elsewhere), "--scope", SCOPE]) == 0
        cap = capsys.readouterr()
        assert "collector" in cap.out
        assert "REFUSING" not in cap.err

    def test_an_explicit_store_prints_no_stamp_lines_when_it_has_none(
        self, tmp_path: Path, repointed, capsys
    ) -> None:
        """Permissive is not the same as silent-about-freshness: with no stamp
        there is simply nothing to print, and nothing is invented."""
        repointed(_store(tmp_path / "default", stamped=False))
        elsewhere = _store(tmp_path / "named", stamped=False)
        assert rc.main(["--store", str(elsewhere), "--scope", SCOPE]) == 0
        assert "  stamp: " not in capsys.readouterr().out

    def test_an_explicit_STAMPED_store_still_prints_its_stamp(
        self, tmp_path: Path, repointed, capsys
    ) -> None:
        repointed(_store(tmp_path / "default", stamped=False))
        elsewhere = _store(tmp_path / "named", stamped=True)
        assert rc.main(["--store", str(elsewhere), "--scope", SCOPE]) == 0
        assert "  stamp: coverage=ALL" in capsys.readouterr().out


# =============================================================================
# The pod's contract. Breaking this is the catastrophic failure mode.
# =============================================================================


class TestThePodContractIsUnchanged:
    """🔴 THE LIBRARY SERVES AN UNSTAMPED STORE, FOREVER.

    `subsystem-store-api/server.py` calls `rc.recall` / `rc.search` /
    `rc.load_store` against `/data` — a directory with no `.sync-stamp`, because
    the pod is what the stamp is ABOUT. `scripts/cairn` calls the same functions
    against a cache that is unstamped on a host that has never synced. A refusal
    in any of them takes the whole store offline; these pin that it cannot
    happen by accident.
    """

    def test_recall_serves_an_unstamped_store(self, tmp_path: Path) -> None:
        store = _store(tmp_path, stamped=False)
        rep = rc.recall(str(store), SCOPE)
        assert rep.status == "recalled"
        assert [e.ref for e in rep.entries] == ["collector"]

    def test_search_serves_an_unstamped_store(self, tmp_path: Path) -> None:
        store = _store(tmp_path, stamped=False)
        rep = rc.search(str(store), SCOPE, "readiness")
        assert rep.status not in ("search-unreadable",)

    def test_load_store_serves_an_unstamped_store(self, tmp_path: Path) -> None:
        store = _store(tmp_path, stamped=False)
        _root, index = rc.load_store(str(store), verb="recalled")
        assert index is not None

    def test_render_text_with_no_extra_header_is_byte_identical(
        self, tmp_path: Path
    ) -> None:
        """The pod calls `render_text(report)` positionally with no keyword.
        The stamp hook must be inert for it — not merely harmless."""
        store = _store(tmp_path, stamped=False)
        rep = rc.recall(str(store), SCOPE)
        assert rc.render_text(rep) == rc.render_text(rep, extra_header=())
        assert "  stamp: " not in rc.render_text(rep)

    #: 🔴 THE THREE CLI-ONLY DEFINITIONS, ENUMERATED. Everything else in
    #: `subsystem_recall.py` is library surface some caller may reach without an
    #: argv, so none of it may consult the read-store resolver.
    CLI_ONLY = frozenset({"_build_parser", "_with_stamp", "main"})

    def test_only_the_enumerated_CLI_functions_consult_the_resolver(self) -> None:
        """🔴 A POSITIONAL GUARD MISSED A FUNCTION THE POD CALLS EVERY REQUEST.

        This test used to split the source at `def _build_parser(` and scan only
        ABOVE it, while its docstring claimed that proved "the whole library half
        cannot grow a refusal". It did not: `_exit_for` is defined ~160 lines
        BELOW that marker, and `server.py` calls `rc._exit_for(...)` inside
        `_serve_report` on every `/recall` and `/search`, as does `scripts/cairn`
        on every read. MEASURED: a refusal grown inside `_exit_for` SURVIVED the
        old guard in both shapes (`return 4` and `raise`).

        Moving the marker down would not fix it either — `_build_parser`,
        `_StoreAction` and the exit-code constant all sit between the two and
        legitimately name the resolver.

        So the guard is no longer POSITIONAL. It enumerates, by AST, every
        top-level definition whose source mentions the resolver and asserts that
        set EQUALS `CLI_ONLY`. Pinned both ways: a library function that grows a
        reference fails, and so does removing a name from the allowlist while the
        reference remains. Source text rather than attribute nodes, so a string
        annotation (`_with_stamp`'s) counts too; comment lines are excluded so
        prose about the resolver stays free.
        """
        src = (ROOT / "lib" / "subsystem_recall.py").read_text(encoding="utf-8")
        referencing = set()
        for node in ast.parse(src).body:
            segment = ast.get_source_segment(src, node) or ""
            body = "\n".join(
                ln for ln in segment.splitlines() if not ln.lstrip().startswith("#")
            )
            if "_read_store." in body:
                referencing.add(getattr(node, "name", None) or type(node).__name__)
        assert referencing == set(self.CLI_ONLY), (
            f"definitions consulting the read-store resolver changed.\n"
            f"  expected (CLI only): {sorted(self.CLI_ONLY)}\n"
            f"  found:               {sorted(referencing)}\n"
            f"Anything not on that list is library surface the POD and `cairn` "
            f"reach without an argv — `_exit_for` is called on every pod request."
        )

    def test_exit_for_is_named_by_the_pod_and_is_NOT_in_the_cli_allowlist(self) -> None:
        """The premise of the test above, pinned rather than recalled.

        If `server.py` stops calling `_exit_for`, or `_exit_for` is added to
        `CLI_ONLY`, the guard above silently stops covering the function that
        motivated it — and nothing else would say so.
        """
        server = (ROOT / "server" / "server.py").read_text(
            encoding="utf-8"
        )
        cairn = (ROOT / "cairn").read_text(encoding="utf-8")
        assert "rc._exit_for(" in server
        assert "rc._exit_for(" in cairn
        assert "_exit_for" not in self.CLI_ONLY

    def test_cairns_exit_4_is_SYNC_ONLY_and_is_not_the_readers_refusal(self) -> None:
        """🔴 TWO TOOLS, ONE NUMBER, OPPOSITE REMEDIES — pinned from the code.

        The reader's 4 means "this host has not fetched the store; run
        `cairn sync`". `cairn`'s 4 is `EXIT_REFRESH_FAILED` and means "the store
        was NOT reached but a usable cache survived" — where re-running
        `cairn sync` is precisely the command that just failed. `/resume` step 4
        told the reader to do that, because the two numbers collide and prose
        cannot see a collision.

        Asserted structurally: the numbers really are equal (so the hazard is
        real and not imagined), and `EXIT_REFRESH_FAILED` is returned from
        `cmd_sync` and nowhere else — which is what makes "`cairn recall` never
        returns 4" true rather than remembered.
        """
        src = (ROOT / "cairn").read_text(encoding="utf-8")
        assert "EXIT_REFRESH_FAILED = 4" in src
        assert rc.EXIT_UNSTAMPED_READ_STORE == 4, "the collision is the premise"
        returning = set()
        for node in ast.parse(src).body:
            # The constant's own `EXIT_REFRESH_FAILED = 4` is a module-level
            # Assign, not a USE — only definitions can return it.
            if not isinstance(node, (ast.FunctionDef, ast.AsyncFunctionDef, ast.ClassDef)):
                continue
            segment = ast.get_source_segment(src, node) or ""
            body = "\n".join(
                ln for ln in segment.splitlines() if not ln.lstrip().startswith("#")
            )
            if "EXIT_REFRESH_FAILED" in body:
                returning.add(node.name)
        assert returning == {"cmd_sync"}, (
            f"`EXIT_REFRESH_FAILED` (4) escaped `cmd_sync`: {sorted(returning)}. "
            f"`/resume` step 4 tells the reader that a 4 from `cairn` is NOT the "
            f"reader's unstamped-store refusal — that sentence is only true while "
            f"this set is exactly {{'cmd_sync'}}."
        )

    def test_exit_for_serves_an_unstamped_default_store(self, tmp_path: Path, repointed) -> None:
        """🔴 THE BEHAVIOURAL BACKSTOP, MADE HOST-INDEPENDENT.

        `_exit_for` is what the pod calls per request. Without repointing, this
        host's real cache IS stamped, so a refusal grown here would never execute
        and every store-api test would pass — the mutant would be scored SURVIVED
        for want of a reachable input, not for want of a defect. So the default
        resolution is forced somewhere with no stamp first, and BOTH verdicts are
        exercised: a served status (0) and an unreadable one (3).
        """
        repointed(tmp_path / "never-synced")  # does not exist: unstamped by any reading
        assert rc._exit_for("recalled", "devrc/", []) == 0
        assert rc._exit_for("scope-unreadable", "devrc/", []) == 3

    def test_the_pod_and_cairn_use_the_library_not_the_cli(self) -> None:
        """The claim this whole exemption rests on, pinned rather than recalled.

        Both import `subsystem_recall` and call its FUNCTIONS; neither shells the
        CLI nor calls `main`. If one ever switches to the CLI it would inherit
        the refusal, and this test is where that gets noticed.
        """
        for rel in ("server/server.py", "cairn"):
            src = (ROOT / rel).read_text(encoding="utf-8")
            assert "import subsystem_recall as rc" in src, rel
            assert "rc.main(" not in src, rel
            assert "_build_parser(" not in src, rel
        server = (ROOT / "server" / "server.py").read_text(
            encoding="utf-8"
        )
        assert "rc.recall(" in server and "rc.load_store(" in server
        cairn = (ROOT / "cairn").read_text(encoding="utf-8")
        assert "rc.recall(" in cairn and "rc.search(" in cairn


# =============================================================================
# service_recon: degrade ONE section, never the brief.
# =============================================================================




# =============================================================================
# The recon DATES the store it serves. The refusal never covered staleness.
# =============================================================================




# =============================================================================
# The THIRD read surface: `scripts/subsystem-audit.py`, whose verb is DELETE.
# =============================================================================
#
# 🔴 #1233 REPOINTED TWO OF THREE. `subsystem_recall`'s CLI and `service_recon`
# were fixed; `scripts/subsystem-audit.py` kept its own
# `DEFAULT_STORE_ROOT = ~/.claude/analyze-service-index` and defaulted `--store`
# to it. It was left out deliberately — it is the tool `prune-index` computes
# DELETIONS from, so repointing it has its own blast radius — and that is also
# exactly why it is the worst one to leave: a stale mirror is missing entries the
# canonical store has, so a cut planned against it is planned against the wrong
# denominator, and §6 of `prune-index` compares an OPEN count across two runs.
#
# The prescribed invocations all pass `--store` explicitly and were never broken;
# the BARE one in `claudedocs/handoff-analyze-service-index-backup.md` was.





def _python_code_only(src: str) -> str:
    """`src` with comment lines and every triple-quoted block removed.

    A literal ban has to read CODE. This file's own first attempt stripped the
    module docstring alone and then failed on prose inside a FUNCTION docstring —
    a guard going red for a sentence, which is how a literal ban gets deleted
    rather than fixed. Both quote styles, because either delimits a docstring.
    """
    stripped, inside, closer = [], False, ""
    for line in src.splitlines():
        rest = line
        while rest:
            if inside:
                idx = rest.find(closer)
                if idx < 0:
                    rest = ""
                    break
                rest, inside, closer = rest[idx + 3:], False, ""
                continue
            hits = [(rest.find(q), q) for q in ('"""', "'''") if rest.find(q) >= 0]
            if not hits:
                break
            at, quote = min(hits)
            stripped.append(rest[:at])
            rest, inside, closer = rest[at + 3:], True, quote
        if not inside and rest and not rest.lstrip().startswith("#"):
            stripped.append(rest)
    return "\n".join(stripped)





#: The auditor's own store-missing code, written out here rather than read off
#: the module. It is the number the refusal must NOT collide with.
AUDIT_EXIT_STORE_MISSING = 2


class TestTheAuditorResolvesThroughTheSharedResolver:
    def test_the_default_reads_the_repointed_cache_and_names_it(
        self, tmp_path: Path, repointed, capsys
    ) -> None:
        """🔴 THE HEADLINE. With no `--store`, the auditor reads whatever
        `read_store_root()` resolves to — so repointing the resolver moves it,
        which is only true if it stopped carrying a path of its own.

        Asserted through the OUTPUT, not by reading a constant back off the
        module: `store: <path>` is the line a human uses to answer "which store
        did this audit?", and it is the one that was wrong.
        """
        store = repointed(_store(tmp_path, stamped=True))
        assert sa.main([]) == 0
        out = capsys.readouterr().out
        assert f"  store: {store}" in out
        # …and it really audited THAT store, rather than printing its name over
        # somebody else's counts.
        assert "collector" in out



class TestTheAuditorRefusesAnUnstampedDefault:
    def test_an_unstamped_DEFAULT_is_refused_and_audits_nothing(
        self, tmp_path: Path, repointed, capsys
    ) -> None:
        """🔴 Non-zero exit, and — the half that matters — no report a reader
        could act on. An audit that still printed its counts would be an
        advisory, and `prune-index` step 1 says to stop when the verdict is
        clean: a clean verdict computed off a frozen mirror is the failure."""
        repointed(_store(tmp_path, stamped=False))
        assert sa.main([]) == 4
        cap = capsys.readouterr()
        assert "REFUSING" in cap.err
        assert rs.REMEDY in cap.err
        assert cap.out.strip() == ""
        assert "index audit" not in cap.out

    def test_the_refusal_names_DELETION_as_the_stake(
        self, tmp_path: Path, repointed, capsys
    ) -> None:
        """This tool is not just another reader, and its refusal says so — a
        reader who has seen the recall refusal must not read this one as the
        same, weaker fact."""
        repointed(_store(tmp_path, stamped=False))
        assert sa.main([]) == 4
        err = capsys.readouterr().err
        assert "DELETIONS" in err
        assert "prune-index" in err

    def test_the_exit_code_is_the_SAME_number_BOTH_read_surfaces_return(
        self, tmp_path: Path, repointed, capsys
    ) -> None:
        """🔴 ONE VOCABULARY, OR A CALLER NEEDS TWO RULES.

        The literal is written out here, and the two tools are compared to it
        rather than to each other — two modules agreeing while both drifted from
        the documented contract is the failure this file has already seen five
        times. `2` is asserted distinct because it is this auditor's OWN
        store-missing code, and a refusal that returned it would be
        indistinguishable from "you named a path that is not a directory".
        """
        repointed(_store(tmp_path, stamped=False))
        assert sa.main([]) == 4
        capsys.readouterr()
        assert rs.EXIT_UNSTAMPED_READ_STORE == 4
        assert rc.EXIT_UNSTAMPED_READ_STORE == 4
        assert rc.main(["--scope", SCOPE]) == 4
        capsys.readouterr()
        assert 4 != AUDIT_EXIT_STORE_MISSING
        assert 4 != 0

    def test_the_guard_is_REACHABLE_the_store_missing_check_does_not_win(
        self, tmp_path: Path, repointed, capsys
    ) -> None:
        """🔴 REACHABILITY, and this one is not hypothetical — it is the ONLY
        case a fresh host produces.

        A host that has never run `cairn sync` has no `~/.cache/subsystem-store`
        at all. `root.is_dir()` sits in this same function and answers that with
        `store root not found`, which is TRUE and carries no remedy. Placed
        after it, the refusal would be dead code for the exact population it
        exists to serve. So the input here is a default that does not exist, and
        the assertion is that the answer is 4 with `cairn sync` in it — never 2.
        """
        repointed(tmp_path / "never-synced")
        assert sa.main([]) == rs.EXIT_UNSTAMPED_READ_STORE
        err = capsys.readouterr().err
        assert rs.REMEDY in err
        assert "store root not found" not in err

    def test_the_same_command_succeeds_once_the_store_is_stamped(
        self, tmp_path: Path, repointed, capsys
    ) -> None:
        """The positive half of the reachability pair: same argv, same store
        contents, stamp present ⇒ exit 0 and a real audit. Without this, a
        `main` that refused everything would pass every test above."""
        repointed(_store(tmp_path, stamped=True))
        assert sa.main([]) == 0
        assert "index audit" in capsys.readouterr().out


class TestTheAuditorStaysPermissiveOnAnExplicitStore:
    """🔴 THE HALF THE CHANGE MUST NOT MOVE.

    Every prescribed invocation of this tool passes `--store` explicitly
    (`prune-index/SKILL.md` x4), as does every fixture in
    `test_subsystem_audit.py`, and a `cp -a` backup or a restored bundle is the
    same case. A refusal that reached them would be a regression dressed as a
    guard.

    🔴 LABELLED HONESTLY, AND THE LABEL IS NOT UNIFORM — measured at
    `c616b7ae`, not assumed. Two of these three are INVARIANT GUARDS: they pass
    on pre-change code and are NOT regression coverage. The first is RED at that
    base, but for a DIFFERENT defect than the store repoint — `render`'s
    `out=sys.stdout` default bound the stream at import, so `capsys` saw nothing
    and the content assertion failed. It is regression coverage for THAT fix, and
    it is the permissive contract only at HEAD.
    """

    def test_an_explicit_store_at_an_unstamped_path_still_audits(
        self, tmp_path: Path, repointed, capsys
    ) -> None:
        # The default is repointed at an unstamped store too, so a pass here
        # cannot come from the default happening to be fine.
        repointed(_store(tmp_path / "default", stamped=False))
        elsewhere = _store(tmp_path / "named", stamped=False)
        assert sa.main(["--store", str(elsewhere)]) == 0
        cap = capsys.readouterr()
        assert "collector" in cap.out
        assert "REFUSING" not in cap.err

    def test_an_explicit_MISSING_store_is_store_missing_not_the_refusal(
        self, tmp_path: Path, repointed, capsys
    ) -> None:
        """The two codes divide on WHO named the path. `store-missing` still
        answers an operator who typed one, and the remedy for that is not
        `cairn sync`."""
        repointed(_store(tmp_path / "default", stamped=True))
        assert sa.main(["--store", str(tmp_path / "nope")]) == AUDIT_EXIT_STORE_MISSING
        err = capsys.readouterr().err
        assert "store root not found" in err
        assert rs.REMEDY not in err



class TestTheAuditCarriesTheSnapshotItMeasured:
    """🔴 THE PRESCRIBED PATH IS EXPLICIT, SO THE REFUSAL NEVER FIRES ON IT.

    Repointing the default fixes the bare invocation and nothing else. What the
    prescribed `--store ~/.cache/subsystem-store` run gains is this: the counts
    now print the snapshot they were measured against. `prune-index` §6 compares
    an OPEN count from a run before the cut with one after, and two runs of
    DIFFERENT snapshots is precisely the comparison that silently means nothing —
    the same shape as the completeness claim that started all this.
    """

    def test_the_stamp_prints_between_the_store_line_and_the_budget_block(
        self, tmp_path: Path, repointed, capsys
    ) -> None:
        """Every line, and POSITIONED. A freshness fact printed below the size
        table dates a page the reader has already believed."""
        repointed(_store(tmp_path, stamped=True))
        assert sa.main([]) == 0
        lines = capsys.readouterr().out.splitlines()
        store_at = next(i for i, ln in enumerate(lines) if ln.startswith("  store: "))
        budget_at = next(i for i, ln in enumerate(lines) if ln.startswith("budget "))
        for i, line in enumerate(STAMP_LINES):
            assert lines[store_at + 1 + i] == f"  stamp: {line}", lines[:12]
        assert store_at + len(STAMP_LINES) < budget_at

    def test_the_stamp_prefix_is_the_ONE_spelling_ALL_THREE_renderers_use(
        self, tmp_path: Path, repointed, capsys
    ) -> None:
        """🔴 THREE RENDERERS NOW, ONE TOKEN. `analyze-service/SKILL.md` tells a
        reader to relay "the `stamp:` lines"; that instruction is true of every
        surface or of none. Compared as RENDERED OUTPUT — the recall CLI's
        against the auditor's — never by grepping two sources for one f-string.
        """
        store = _store(tmp_path, stamped=True)
        repointed(store)
        assert rc.main(["--scope", SCOPE]) == 0
        reader_lines = {
            ln for ln in capsys.readouterr().out.splitlines() if ln.startswith("  stamp:")
        }
        assert sa.main([]) == 0
        audit_lines = {
            ln for ln in capsys.readouterr().out.splitlines() if ln.startswith("  stamp:")
        }
        assert reader_lines == audit_lines != set()
        assert audit_lines == {f"{rs.STAMP_PREFIX}{ln}" for ln in STAMP_LINES}

    def test_an_explicitly_named_store_is_dated_when_it_can_be_and_not_when_it_cannot(
        self, tmp_path: Path, repointed, capsys
    ) -> None:
        """🔴 BOTH HALVES, because either alone passes over a renderer that
        always prints or never does. `--store` is exempt from the refusal, not
        from the date — and where there is no stamp, nothing is invented."""
        repointed(_store(tmp_path / "default", stamped=True))
        fresh = _store(tmp_path / "fresh", stamped=True)
        bare = _store(tmp_path / "bare", stamped=False)

        assert sa.main(["--store", str(fresh)]) == 0
        dated = capsys.readouterr().out
        for line in STAMP_LINES:
            assert f"{rs.STAMP_PREFIX}{line}" in dated, line

        assert sa.main(["--store", str(bare)]) == 0
        undated = capsys.readouterr().out
        assert rs.STAMP_PREFIX not in undated
        assert "index audit" in undated

    def test_NO_AGE_IS_COMPUTED_by_the_auditor_either(
        self, tmp_path: Path, repointed, capsys
    ) -> None:
        """`cairn.cache_age` owns that arithmetic. Asserted behaviourally — the
        stamp's own epoch is echoed, never converted — because a structural
        `import time` ban cannot apply here: this auditor legitimately shells
        `git` and has other clock-free reasons to hold none."""
        repointed(_store(tmp_path, stamped=True))
        assert sa.main([]) == 0
        assert "  stamp: synced=1788363567" in capsys.readouterr().out




