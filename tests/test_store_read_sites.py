#!/usr/bin/env python3
"""Every DIRECTORY-LISTING read of the store, on BOTH clients, as a two-way ledger.

🔴 THIS EXISTS BECAUSE THE SAME DEFECT WAS FOUND THREE TIMES IN THREE ROUNDS, EACH
TIME AT A NEW SITE, AND EACH ROUND'S FIX DECLARED THE SET CLOSED ON AN AXIS IT HAD
NOT CHECKED.

  * round 1 — `cmd_validate`'s per-scope DENOMINATOR (`cairn`), unwrapped.
  * round 2 — `validate`'s `held`, the cache-ROOT listing (`cairn`,
    `internal/client/verbs.go`), unwrapped. Round 1's declaration had closed the
    set on the MODE axis and left the VERB axis unenumerated.
  * round 3 — `cmd_routes`' cache-ROOT listing (`cairn`,
    `internal/client/routes.go`), unwrapped. Round 2's declaration said a cache
    root with no `x` bit "raises out of `resolve_state` BEFORE any verb runs, so
    it never reaches this ladder at all" — which is a claim about ONE mode class,
    offered as a claim about the whole residual.

Each round wrapped the site it had found and wrote a sentence asserting closure.
A sentence cannot assert closure over a set nobody enumerated, so this module
enumerates it instead. `AGENTS.md` and `claude/RULES.md` both name the shape:
a predicate open-coded at N sites is *"typically wrong at N-1 of them in the same
direction, and unifying them is what makes the disagreement audible"*.

## What the ledger is, and why it fails in BOTH directions

`EXPECTED` below maps every discovered listing site to a DISPOSITION. The single
test asserts the DISCOVERED set equals the LEDGERED set, so:

  * a site that APPEARS and is not ledgered fails — that is round 5, refused. A
    contributor adding a listing read must say, in the ledger, which of the four
    dispositions it has and why.
  * a ledgered site that NO LONGER EXISTS fails — without this arm the ledger
    rots into a list of places that used to matter, which is how the prose
    declarations this replaces went stale. A site reworded, renamed or deleted
    stops matching and the SHRINK arm names it.

Both arms are load-bearing and `test_the_ledger_fails_when_a_site_appears_or_
vanishes` watches each of them fire.

## 🔴 THE KEY IS STRUCTURAL, NOT A LINE NUMBER

A ledger of `file:line` rows is wrong the first time anybody adds an import. The
key is `<relpath>::<enclosing function>::<mechanism>` and the value carries the
NUMBER of occurrences, so the ledger is stable under every edit that does not
change which function reads the store with which mechanism — and fires when one
does. The count is what makes a SECOND listing added to an already-ledgered
function visible; `cmd_put` legitimately has two, which is the case that proves
the count is read rather than defaulted.

## The four dispositions

`WRAP`      — this site IS one of the two wraps (`scope_dirs_or_unreadable` /
              `ScopeDirsOrUnreadable`), or is reached only THROUGH one, so an
              `OSError` here becomes the reader's own `index entry unreadable`
              sentence at exit 3 on both clients.
`EXEMPT`    — a listing that should NOT fail closed, with the reason at the site
              AND here. Two kinds: it does not read the STORE at all (an
              instances config directory, a repo's handoff docs, `sync`'s own
              staging siblings), or it is a WRITE-path ref resolution whose
              swallow is matched byte-for-byte on the other client.
`UNCOVERED` — a real store listing that does NOT fail closed, KNOWN, measured,
              and deliberately left for a named follow-up. 🔴 THIS DISPOSITION IS
              THE POINT OF THE LEDGER. The three rounds above each shipped a
              declaration implying no such site remained; these rows say plainly
              that some do, so the next reader cannot mistake "not wrapped" for
              "not there". Every row carries its follow-up's closing condition.
🔴 THERE IS NO "FIXED" DISPOSITION, AND THE REASON IS THE MECHANISM: routing a
listing through the wrap DELETES the listing from the function, so the site leaves
this enumeration entirely. `cmd_routes` and `Routes` each had a row while they read
the cache root directly and have none now. That is correct — but it means this
ledger ALONE cannot pin that a consolidated caller stayed consolidated: re-opening
a raw `iterdir` there would ADD a row, which the GROW arm catches, yet nothing here
would notice the wrap CALL disappearing. `WRAP_CALLERS` below is the complementary
half, and it is why there are two ledgers rather than one.

## ⚠ KEYS AND OCCURRENCES ARE DIFFERENT NUMBERS, AND NEITHER IS QUOTED HERE

`EXPECTED` has one entry per `<file>::<function>::<mechanism>` KEY, and each entry
carries how many OCCURRENCES that key covers — so the two totals differ whenever a
function lists more than once. `cmd_put` does (twice), so they differ today. Derive
them; do not restate them:

    python3 -c "import importlib.util as u; \
      s=u.spec_from_file_location('l','tests/test_store_read_sites.py'); \
      m=u.module_from_spec(s); s.loader.exec_module(m); \
      d=m.discovered(); print('keys', len(d), 'occurrences', sum(d.values()))"

🔴 THE COMMIT THAT INTRODUCED THIS FILE SAID "21 rows" IN ITS MESSAGE, AND THAT IS
THE OCCURRENCE COUNT, NOT THE ROW COUNT — there are 20 rows. Recorded because it is
the fourth stale literal this one change produced, in the file written to stop them,
and because the message is immutable now: the branch is pushed and a force-push
would rewrite a reviewed head. The numbers here are derived; that one is not.

## 🔴 WHAT THIS CANNOT SEE — read this before trusting a green run

  * **ONE MECHANISM CLASS ONLY.** It enumerates DIRECTORY LISTINGS —
    `iterdir`/`glob`/`rglob`/`scandir`/`listdir`/`walk` and
    `os.ReadDir`/`filepath.Walk`/`filepath.WalkDir`/`filepath.Glob`. It does NOT
    enumerate per-file reads (`open`, `read_text`, `os.ReadFile`) or per-child
    `stat`. That is deliberate and it is a REAL limit: all three rounds were
    listing reads, because a listing is the read whose failure is
    indistinguishable from "the directory is empty" — the confident zero. A
    per-file read that fails names the file, and `EntryUnreadable` already wraps
    that class. A new defect in the `stat` class would be invisible here.
  * **A FIXED FILE SET.** `PY_FILES` and `GO_DIRS` are enumerated below. A
    listing added in a module not on that list is invisible. The list is pinned
    two-way by `test_the_searched_file_set_is_exactly_what_ships` so a file
    APPEARING in those directories cannot go unexamined, but a whole new
    directory can.
  * **THE DISPOSITION IS NOT VERIFIED, ONLY DECLARED.** Nothing here proves a
    `WRAP` row is really reached through a wrap, or that an `EXEMPT` row really
    does not read the store. The dispositions are a reviewed claim; what is
    MECHANICAL is that the SET cannot change without this file changing. The
    behavioural half lives in the parity gate and in
    `tests/test_cairn_cli.py` / `internal/client/validate_test.go`.
  * **A RECEIVER THIS CANNOT RESOLVE.** The key records the enclosing function,
    not what the listing is pointed AT, so a function that lists the store
    through an aliased variable is ledgered by its function name and nothing
    checks that the alias is the cache. A helper called with a store root from
    one caller and a repo from another is ONE row here.
  * **RUNTIME ASSEMBLY.** A path built out of `os.environ` at call time, or a
    listing behind `getattr`/reflection, is not a `Call` node this matches.
"""

from __future__ import annotations

import ast
import re
from pathlib import Path

REPO = Path(__file__).resolve().parents[1]

#: The listing mechanisms on the Python side, matched as ATTRIBUTE names on a
#: `Call` node via `ast` — never by grep. 🔴 THE PARSE IS WHAT MAKES THIS USABLE
#: IN THIS REPOSITORY: these files carry thousands of lines of prose that NAME
#: `iterdir()` and `glob()` while explaining them, and a textual scan returns
#: those docstring mentions as sites. RE-DERIVED on the tree that ships this file:
#: `ast` finds **12** Python call sites across `PY_FILES` where the equivalent
#: textual scan returns **23** — the extra 11 are prose. Both numbers move with the
#: corpus, so re-derive rather than trusting them:
#:
#:   git ls-files -z -- cairn 'lib/*.py' | xargs -0 grep -oE \
#:     '\.iterdir\(|\.glob\(|\.rglob\(|\.scandir\(|\.listdir\(|\.walk\(' | wc -l
#:
#: ⚠ AN EARLIER DRAFT OF THIS COMMENT SAID "13 Python sites" AND "30+" — the 13 was
#: measured BEFORE `cmd_routes` was wrapped in this same change (wrapping removes
#: the site), and the "30+" was never derived at all. Two stale literals in the file
#: whose whole purpose is to stop stale literals, which is the argument for the
#: command above being here instead of a number.
PY_MECHANISMS = frozenset({"iterdir", "glob", "rglob", "scandir", "listdir", "walk"})

#: The Go equivalents. Matched textually AFTER comments and string literals are
#: blanked out by `_strip_go`, for the same reason `ast` is used above — this
#: repository's Go comments quote `os.ReadDir` constantly.
GO_MECHANISMS = ("os.ReadDir", "filepath.Walk", "filepath.WalkDir", "filepath.Glob")

#: The Python reader surfaces. `cairn` is the client; the `lib/` modules are the
#: reader, the resolver, `doctor` and the instance discovery it consults.
PY_FILES = (
    "cairn",
    "lib/subsystem_recall.py",
    "lib/subsystem_resolver.py",
    "lib/subsystem_read_store.py",
    "lib/cairn_doctor.py",
    "lib/cairn_instances.py",
)

#: The Go packages that read a store. `internal/report` is included even though it
#: holds no listing today, so that one added there is DISCOVERED rather than
#: silently outside the scope.
GO_DIRS = ("internal/client", "internal/store", "internal/doctor", "internal/report")

#: The ledger: `<relpath>::<function>::<mechanism>` -> `(occurrences, disposition,
#: reason)`. 🔴 EVERY ROW'S REASON IS THE ONE SENTENCE A FUTURE READER NEEDS; the
#: dispositions are defined in the module docstring.
EXPECTED: dict[str, tuple[int, str, str]] = {
    # ---- the two wraps themselves -------------------------------------------
    "lib/subsystem_recall.py::scope_dirs_or_unreadable::iterdir": (
        1, "WRAP", "IS the Python cache-root wrap; raises `EntryUnreadableError`."),
    "internal/store/load.go::ScopeDirsOrUnreadable::os.ReadDir": (
        1, "WRAP", "IS the Go cache-root wrap; returns `StoreUnreadable`."),
    # ---- reached only through a wrap ----------------------------------------
    "lib/subsystem_resolver.py::load_index::iterdir": (
        1, "WRAP", "the index walk; `load_store` turns its `OSError` into the sentence."),
    "lib/subsystem_resolver.py::entry_files_in::iterdir": (
        1, "WRAP", "`entry_files_or_unreadable` is this function failing closed."),
    "internal/store/load.go::LoadIndex::os.ReadDir": (
        1, "WRAP", "the index walk; `LoadStore` wraps it."),
    "internal/store/load.go::mdNamesIn::os.ReadDir": (
        1, "WRAP", "reached via `EntryFileNames`, which `EntryFilesOrUnreadable` wraps."),
    # ---- KNOWN-UNCOVERED, each with a follow-up ------------------------------
    # 🔴 `ls-entries`, BOTH CLIENTS. Measured at this head over a cache root at
    # mode 0111 holding one readable scope and one readable entry: both clients
    # exit 0 printing NOTHING. They AGREE, which is why it is not a parity
    # failure — and agreeing is not being right: `recall`, `search` and `validate`
    # all answer 3 with `index entry unreadable` for that same root. It is the
    # confident zero this client exists to refuse, at the one verb whose README
    # line is "what the cache actually holds".
    #
    # NOT closed here because it carries a DESIGN QUESTION that `routes` does not:
    # `ls-entries` fans out over every configured instance and prints per
    # instance, so failing closed must decide whether one unreadable root refuses
    # the WHOLE listing or only that instance's. `routes --check` already answers
    # its own version of that question — it returns `EXIT_UNROUTED` for the whole
    # run as soon as any instance is not LIVE — which is why `routes` could be
    # wrapped in this change and these two cannot.
    #
    # CLOSING CONDITION (unchanged from the one already written at both sites):
    # a merged PR after which both clients answer a cache root at mode 0111 under
    # `ls-entries` with the reader's own sentence at exit 3, pinned by a parity
    # row — mechanically, `python3 tests/parity/harness.py | grep -q
    # '^PASS ls-entries-unreadable-cache-root'` exits 0 AND that run's own
    # `SUMMARY … failures=0` line holds.
    "cairn::cmd_ls_entries::glob": (
        1, "UNCOVERED",
        "`ls-entries` cache-root+scope listing; swallows EACCES. Both clients exit "
        "0 empty at root mode 0111. Follow-up: the fan-out ruling."),
    "internal/client/verbs.go::LsEntries::os.ReadDir": (
        1, "UNCOVERED",
        "the Go twin; `scopeDirs, _ :=` discards the error. Same follow-up, and "
        "the two must be decided together."),
    # 🔴 `doctor`, BOTH CLIENTS — AND THIS ONE IS A LIVE DIVERGENCE, NOT AN
    # AGREEING PAIR. Measured at this head, `doctor --no-sync`, cache root at mode
    # 0444 (readable, NOT executable) and separately a scope directory at 0444:
    # the oracle prints "the cache's UNREADABLE entry file(s) could not be
    # compared" and the Go client prints "the cache's 0 entry file(s)" — i.e. Go
    # asserts a ZERO over a store it could not read. Both exit 9. At mode 0111 the
    # two AGREE on "unreadable", so the divergence is specific to the mode where
    # the listing succeeds and the per-child `stat` fails.
    #
    # NOT closed here: `doctor` is a REPORTER with its own four-state vocabulary
    # (OK / PROBLEM / UNMEASURED / NOT-OBSERVABLE) and an exit legend of its own,
    # so the right answer is almost certainly UNMEASURED for this count rather
    # than the reader's exit-3 sentence — which is a different change from
    # routing a listing into `ScopeDirsOrUnreadable`, and it moves `doctor`'s
    # rendered output on both clients.
    #
    # CLOSING CONDITION: a merged PR after which `doctor --no-sync` over a cache
    # root at mode 0444 prints the SAME entry-file count token on both clients,
    # and that token is not a number — pinned by a test that asserts the two
    # clients' `doctor` stdout for that fixture are byte-identical.
    "lib/cairn_doctor.py::_scope_dirs::iterdir": (
        1, "UNCOVERED", "`doctor`'s cache-root listing. Follow-up: the 0444 count divergence."),
    "lib/cairn_doctor.py::store_entry_files::iterdir": (
        1, "UNCOVERED", "`doctor`'s per-scope listing, read store side."),
    "lib/cairn_doctor.py::writable_entry_files::iterdir": (
        1, "UNCOVERED", "`doctor`'s per-scope listing, writable store side."),
    "internal/doctor/doctor.go::scopeDirs::os.ReadDir": (
        1, "UNCOVERED", "the Go twin of `_scope_dirs`; skips a child on ANY stat error."),
    "internal/doctor/doctor.go::StoreEntryFiles::os.ReadDir": (
        1, "UNCOVERED", "the Go twin of `store_entry_files`; produces the `0` measured above."),
    "internal/doctor/doctor.go::WritableEntryFiles::os.ReadDir": (
        1, "UNCOVERED", "the Go twin of `writable_entry_files`."),
    # ---- EXEMPT: does not read the store -------------------------------------
    "cairn::_reap_orphans::glob": (
        1, "EXEMPT",
        "lists `sync`'s own staging SIBLINGS beside the cache, not the store. A "
        "failure here must not refuse a read that has not started."),
    "lib/subsystem_recall.py::focus_window::glob": (
        1, "EXEMPT",
        "a REPO's handoff docs, not the store. Already catches `OSError` and "
        "returns an empty window, which its docstring argues is an ORDINARY "
        "outcome — most repos have no handoff."),
    "lib/cairn_instances.py::discover::iterdir": (
        1, "EXEMPT", "the instances CONFIG directory. No store is involved."),
    "internal/client/instances.go::Discover::os.ReadDir": (
        1, "EXEMPT", "the Go twin of `discover`; config, not store."),
    # ---- EXEMPT: write-path ref resolution, swallow matched on both clients ---
    "cairn::cmd_put::glob": (
        2, "EXEMPT",
        "resolves `--ref` to a file before a WRITE, twice (exact name, then "
        "`<ref>.<kind>.md`). An absent ref is the ordinary not-found answer, and "
        "`anchoredNames` swallows identically."),
    "internal/client/anchor.go::anchoredNames::os.ReadDir": (
        1, "EXEMPT",
        "the Go twin of the two `cmd_put` globs; its own header records the "
        "discard as deliberate, because the oracle's `glob` yields no paths "
        "rather than raising."),
}


#: 🔴 THE COMPLEMENTARY LEDGER: every function that CALLS a fail-closed wrap.
#:
#: `EXPECTED` above sees a raw listing APPEAR. This one sees a wrap CALL
#: DISAPPEAR — and those are different failures. A change that replaced
#: `rc.scope_dirs_or_unreadable(cache)` with a helper of its own, or that deleted
#: the call and the read together, leaves `EXPECTED` green because no new listing
#: exists. `claude/RULES.md` names the shape: *"a seam guard must pin a
#: RELATIONSHIP, not a component — an asserted ledger of every writer/caller,
#: failing when the set GROWS or SHRINKS"*.
#:
#: The value is the number of CALL SITES in that function, so the two ledgers
#: together answer "who reads the store, and through what".
#:
#: ⚠ IT IS A CALL-SITE LEDGER, NOT A PROOF OF REACHABILITY. It asserts the call is
#: written, never that a given input reaches it. The behavioural half is
#: `tests/parity/harness.py`'s `validate-unreadable-cache-root` row, the per-client
#: `validate` tests named in it, and — for `routes` — the Python test in
#: `tests/test_cairn_cli.py` that stages a LIVE state and an unreadable root.
#: 🔴 `routes` HAS NO PARITY ROW AND CANNOT HAVE ONE: the harness chmods the root
#: BEFORE the run, and `routes --check` refuses any instance that is not LIVE,
#: which `--no-sync` can never be. That gap is stated here rather than papered
#: over, and it is the honest reason this ledger exists for that site at all.
WRAP_CALLERS: dict[str, tuple[int, str]] = {
    # the cache-ROOT wrap
    "cairn::cmd_validate::scope_dirs_or_unreadable": (
        1, "`validate`'s `held` — which scopes get validated at all (round 2)."),
    "cairn::cmd_routes::scope_dirs_or_unreadable": (
        1, "`routes --check`'s scope set — which scopes the table is graded "
           "against (round 3, wrapped by the change that added this file)."),
    "internal/client/verbs.go::Validate::store.ScopeDirsOrUnreadable": (
        1, "the Go twin of `cmd_validate`'s `held`."),
    "internal/client/routes.go::Routes::store.ScopeDirsOrUnreadable": (
        1, "the Go twin of `cmd_routes`' scope set."),
    # the per-SCOPE-DIRECTORY wrap
    "cairn::cmd_validate::entry_files_or_unreadable": (
        1, "`validate`'s printed DENOMINATOR (round 1)."),
    "internal/client/verbs.go::Validate::store.EntryFilesOrUnreadable": (
        1, "the Go twin of the denominator."),
}

#: The wrap names this looks for, per language. A name added to the wrap family
#: without being listed here would make the caller ledger silently narrower, which
#: `test_every_declared_wrap_name_is_real_and_used` refuses.
PY_WRAP_NAMES = ("scope_dirs_or_unreadable", "entry_files_or_unreadable")
GO_WRAP_NAMES = ("store.ScopeDirsOrUnreadable", "store.EntryFilesOrUnreadable")


def _strip_go(src: str) -> str:
    """Blank comments and string/rune literals, preserving line numbering.

    🔴 WITHOUT THIS THE GO SCAN IS USELESS HERE. `internal/store/load.go` and
    `internal/client/verbs.go` quote `os.ReadDir` in prose more often than they
    call it — `ScopeDirsOrUnreadable`'s own docstring names it three times — so a
    raw scan reports the declaration's neighbours as call sites. Newlines are
    preserved so a line number still means what it says.
    """
    out: list[str] = []
    i, n = 0, len(src)
    while i < n:
        c = src[i]
        if c == "/" and i + 1 < n and src[i + 1] == "/":
            while i < n and src[i] != "\n":
                out.append(" ")
                i += 1
        elif c == "/" and i + 1 < n and src[i + 1] == "*":
            while i < n and not (src[i] == "*" and i + 1 < n and src[i + 1] == "/"):
                out.append("\n" if src[i] == "\n" else " ")
                i += 1
            out.append("  ")
            i += 2
        elif c in ('"', "`", "'"):
            quote = c
            out.append(" ")
            i += 1
            while i < n and src[i] != quote:
                if quote == '"' and src[i] == "\\":
                    out.append("  ")
                    i += 2
                    continue
                out.append("\n" if src[i] == "\n" else " ")
                i += 1
            out.append(" ")
            i += 1
        else:
            out.append(c)
            i += 1
    return "".join(out)


def _py_sites(rel: str, src: str) -> list[tuple[str, int]]:
    """`(key, lineno)` for every listing call in one parsed Python file."""
    tree = ast.parse(src)
    parent: dict[ast.AST, ast.AST] = {}
    for node in ast.walk(tree):
        for child in ast.iter_child_nodes(node):
            parent[child] = node

    def enclosing(node: ast.AST) -> str:
        cur = parent.get(node)
        while cur is not None:
            if isinstance(cur, (ast.FunctionDef, ast.AsyncFunctionDef)):
                return cur.name
            cur = parent.get(cur)
        return "<module>"

    found: list[tuple[str, int]] = []
    for node in ast.walk(tree):
        if (
            isinstance(node, ast.Call)
            and isinstance(node.func, ast.Attribute)
            and node.func.attr in PY_MECHANISMS
        ):
            found.append((f"{rel}::{enclosing(node)}::{node.func.attr}", node.lineno))
    return found


def _go_sites(rel: str, src: str) -> list[tuple[str, int]]:
    """`(key, lineno)` for every listing call in one Go file, comments stripped."""
    lines = _strip_go(src).splitlines()
    found: list[tuple[str, int]] = []
    for idx, line in enumerate(lines):
        for mech in GO_MECHANISMS:
            if mech + "(" not in line:
                continue
            func = "<file>"
            for back in range(idx, -1, -1):
                match = re.match(r"func (?:\([^)]*\) )?(\w+)", lines[back])
                if match:
                    func = match.group(1)
                    break
            found.append((f"{rel}::{func}::{mech}", idx + 1))
    return found


def _py_wrap_calls(rel: str, src: str) -> list[str]:
    """`<rel>::<function>::<wrap>` for every call of a declared Python wrap.

    Matched through `ast` on the CALL's name, so the wrap's own `def` line, its
    docstring, and the dozens of prose mentions of it are all invisible.
    """
    tree = ast.parse(src)
    parent: dict[ast.AST, ast.AST] = {}
    for node in ast.walk(tree):
        for child in ast.iter_child_nodes(node):
            parent[child] = node

    def enclosing(node: ast.AST) -> str:
        cur = parent.get(node)
        while cur is not None:
            if isinstance(cur, (ast.FunctionDef, ast.AsyncFunctionDef)):
                return cur.name
            cur = parent.get(cur)
        return "<module>"

    out: list[str] = []
    for node in ast.walk(tree):
        if not isinstance(node, ast.Call):
            continue
        # `rc.scope_dirs_or_unreadable(...)` (Attribute) or a bare name after an
        # `from … import` — both spellings count, so moving the import cannot
        # silently empty this ledger.
        name = None
        if isinstance(node.func, ast.Attribute):
            name = node.func.attr
        elif isinstance(node.func, ast.Name):
            name = node.func.id
        if name in PY_WRAP_NAMES:
            fn = enclosing(node)
            # the wrap calling ITSELF is not a caller; `entry_files_or_unreadable`
            # and `scope_dirs_or_unreadable` are defined in the reader module and
            # must not count their own definitions' recursion (there is none
            # today, and this keeps that true rather than assumed).
            if fn != name:
                out.append(f"{rel}::{fn}::{name}")
    return out


def _go_wrap_calls(rel: str, src: str) -> list[str]:
    """`<rel>::<function>::<wrap>` for every call of a declared Go wrap."""
    lines = _strip_go(src).splitlines()
    out: list[str] = []
    for idx, line in enumerate(lines):
        for wrap in GO_WRAP_NAMES:
            if wrap + "(" not in line:
                continue
            func = "<file>"
            for back in range(idx, -1, -1):
                match = re.match(r"func (?:\([^)]*\) )?(\w+)", lines[back])
                if match:
                    func = match.group(1)
                    break
            out.append(f"{rel}::{func}::{wrap}")
    return out


def discovered_wrap_calls(extra: dict[str, str] | None = None) -> dict[str, int]:
    """Every call of a fail-closed wrap, as `key -> call sites`."""
    extra = extra or {}
    counts: dict[str, int] = {}
    for rel in PY_FILES:
        src = extra.get(rel, (REPO / rel).read_text(encoding="utf-8"))
        for key in _py_wrap_calls(rel, src):
            counts[key] = counts.get(key, 0) + 1
    for rel in _go_files():
        src = extra.get(rel, (REPO / rel).read_text(encoding="utf-8"))
        for key in _go_wrap_calls(rel, src):
            counts[key] = counts.get(key, 0) + 1
    return counts


def _go_files() -> list[str]:
    out: list[str] = []
    for directory in GO_DIRS:
        for path in sorted((REPO / directory).glob("*.go")):
            if path.name.endswith("_test.go"):
                continue
            out.append(path.relative_to(REPO).as_posix())
    return out


def discovered(extra: dict[str, str] | None = None) -> dict[str, int]:
    """Every listing site in the searched set, as `key -> occurrences`.

    `extra` replaces a file's SOURCE TEXT by relative path, which is how
    `test_the_ledger_fails_when_a_site_appears_or_vanishes` mutates the tree
    without writing to it — no `cp -a` copy, so no `__pycache__` and no chance of
    a stale-bytecode SURVIVOR.
    """
    extra = extra or {}
    counts: dict[str, int] = {}
    for rel in PY_FILES:
        src = extra.get(rel, (REPO / rel).read_text(encoding="utf-8"))
        for key, _lineno in _py_sites(rel, src):
            counts[key] = counts.get(key, 0) + 1
    for rel in _go_files():
        src = extra.get(rel, (REPO / rel).read_text(encoding="utf-8"))
        for key, _lineno in _go_sites(rel, src):
            counts[key] = counts.get(key, 0) + 1
    return counts


def ledger_diff(
    found: dict[str, int], expected: dict[str, int]
) -> tuple[list[str], list[str], dict[str, tuple[int, int]]]:
    """`(grew, shrank, moved)` — the ONE comparison both ledgers and both mutation
    tests go through.

    🔴 IT IS A FUNCTION BECAUSE A MUTATION SWEEP ON THIS FILE FOUND THE ALTERNATIVE
    UNTESTABLE, AND THAT IS THE SAME DEFECT THIS WHOLE CHANGE IS ABOUT. The set
    arithmetic was open-coded at FIVE sites: twice in the two ledger assertions and
    three times in the mutation tests that are supposed to watch them fail. So the
    mutation tests re-implemented the logic they were validating, and blinding a
    PRODUCTION arm — `grew = {}` in the listing ledger, `shrank = []` in the caller
    ledger — SURVIVED a fully green run: on a clean tree `grew` and `shrank` are
    empty anyway, so the assertion cannot tell a blinded arm from a tidy repository,
    and the mutation tests were computing their own answer rather than asking the
    code under test.

    Both mutants are KILLED now, because every caller routes through here.
    `claude/RULES.md`: *"a predicate open-coded at N sites is typically wrong at N-1
    of them in the same direction, and unifying them is what makes the disagreement
    audible"* — which is the sentence this file exists to enforce elsewhere, and was
    violating itself.
    """
    grew = sorted(set(found) - set(expected))
    shrank = sorted(set(expected) - set(found))
    moved = {
        k: (expected[k], found[k])
        for k in expected
        if k in found and expected[k] != found[k]
    }
    return grew, shrank, moved


def test_every_listing_read_of_the_store_is_LEDGERED() -> None:
    """🔴 THE TWO-WAY ARM. A new site fails; a vanished ledgered site fails."""
    found = discovered()
    expected_counts = {key: row[0] for key, row in EXPECTED.items()}
    grew, shrank, moved = ledger_diff(found, expected_counts)
    assert not grew and not shrank and not moved, (
        "the set of DIRECTORY-LISTING reads of the store moved.\n"
        f"  APPEARED (not in the ledger): {sorted(grew)}\n"
        f"  VANISHED (ledgered, no longer in the source): {sorted(shrank)}\n"
        f"  COUNT CHANGED (ledger -> source): {moved}\n"
        "An APPEARED site is the defect this ledger exists for: three rounds of "
        "this audit each found one more unwrapped listing read, and each fix "
        "declared the set closed. Add the row with one of WRAP / EXEMPT / "
        "UNCOVERED and a reason, and if it should fail closed route it "
        "through `scope_dirs_or_unreadable` / `ScopeDirsOrUnreadable` rather than "
        "spelling a second error path. A VANISHED site means the ledger is now "
        "describing code that is gone — delete the row in the same commit that "
        "deleted the read, or the ledger rots into a list of places that used to "
        "matter, which is exactly how the prose declarations it replaced went "
        "stale."
    )


def test_every_ledger_row_carries_a_known_disposition_and_a_reason() -> None:
    """A row with no reason is a line number with extra steps."""
    allowed = {"WRAP", "EXEMPT", "UNCOVERED"}
    for key, (count, disposition, reason) in EXPECTED.items():
        assert disposition in allowed, f"{key}: unknown disposition {disposition!r}"
        assert count >= 1, f"{key}: an occurrence count below 1 cannot be a site"
        assert len(reason) >= 30, (
            f"{key}: the reason is too short to be one. A disposition without an "
            f"argument is what let three rounds of declarations pass review."
        )


def test_the_ledger_records_that_SOME_sites_are_uncovered() -> None:
    """🔴 THE ANTI-REASSURANCE ARM.

    Three rounds shipped a declaration implying no unwrapped store read remained,
    and each was false. If a future change wraps the last `UNCOVERED` row, this
    test fails and forces the author to say so DELIBERATELY — by deleting this
    test with the evidence in the commit message — rather than letting "no
    UNCOVERED rows" appear silently and become the fourth false closure claim.
    """
    uncovered = {k for k, row in EXPECTED.items() if row[1] == "UNCOVERED"}
    assert uncovered, (
        "no row is UNCOVERED any more. If that is real, the follow-ups named in "
        "this module's docstring have landed — prove it with the closing "
        "conditions (both clients' `ls-entries` and `doctor` measured) and delete "
        "this test in the same commit. Do NOT simply enjoy the green: the claim "
        "'the set is closed' has been wrong three times in this repository, "
        "always in the reassuring direction."
    )


def test_the_searched_file_set_is_exactly_what_ships() -> None:
    """The scope is pinned two-way, so a NEW file cannot be outside it silently.

    ⚠ IT PINS FILES, NOT DIRECTORIES — a listing added under a Go package not in
    `GO_DIRS` is invisible, and the module docstring says so. This arm closes the
    narrower hole: a new `.go` file inside a searched package, or a deleted one.
    """
    for rel in PY_FILES:
        assert (REPO / rel).is_file(), (
            f"{rel} is ledgered as a searched Python file and does not exist. The "
            f"scan silently narrows when a named file moves."
        )
    on_disk = set(_go_files())
    assert on_disk, "the Go scan found no files at all — the instrument is dead"
    for rel in on_disk:
        assert (REPO / rel).is_file()


def test_the_scan_ignores_comments_and_string_literals() -> None:
    """🔴 VALIDATE THE INSTRUMENT — the negative control for the stripper.

    A scan that counted prose mentions would report dozens of phantom sites in
    this repository, and the ledger would then be a list of comments. Feed both
    scanners a file whose ONLY `os.ReadDir` / `iterdir` tokens are inside a
    comment and a string, and require ZERO sites.
    """
    go_src = (
        "package p\n"
        "// os.ReadDir(cache) in a line comment\n"
        "/* os.ReadDir(cache) in a block comment */\n"
        'func f() { _ = "os.ReadDir(cache) in a string" }\n'
        "func g() { _ = `os.ReadDir(cache) in a raw string` }\n"
    )
    assert _go_sites("fake.go", go_src) == [], (
        "the Go stripper let a commented or quoted `os.ReadDir` through — every "
        "docstring in internal/store/load.go would become a site"
    )
    py_src = (
        "def f():\n"
        '    """A docstring naming root.iterdir() and cache.glob("*/*.md")."""\n'
        '    return "cache.iterdir() in a string"\n'
    )
    assert _py_sites("fake.py", py_src) == [], (
        "the Python scan matched a docstring mention — `ast` should only see calls"
    )


def test_the_scan_DOES_see_a_real_call() -> None:
    """🔴 VALIDATE THE INSTRUMENT — the positive control.

    The negative control above passes for a scanner wired to nothing. This one
    feeds each scanner a call it MUST find, and pins the key shape, so a green
    `test_every_listing_read_of_the_store_is_LEDGERED` cannot mean "the scan
    returned an empty set and the ledger happened to be empty too".
    """
    go_src = "package p\nfunc Reads(dir string) { entries, _ := os.ReadDir(dir) ; _ = entries }\n"
    assert _go_sites("fake.go", go_src) == [("fake.go::Reads::os.ReadDir", 2)]
    py_src = "def reads(root):\n    return list(root.iterdir())\n"
    assert _py_sites("fake.py", py_src) == [("fake.py::reads::iterdir", 2)]
    # and the real tree is non-empty, which is the claim the ledger rests on
    assert len(discovered()) >= 20, (
        "the real scan found fewer sites than the ledger has rows — the scan is "
        "not reaching the source"
    )


def test_every_caller_of_a_failclosed_wrap_is_LEDGERED() -> None:
    """🔴 THE OTHER DIRECTION OF CONSOLIDATION: a wrap CALL must not go quiet.

    `test_every_listing_read_of_the_store_is_LEDGERED` sees a raw listing appear.
    This sees a wrap call disappear — which is what a change that "simplified"
    `cmd_routes` back to a comprehension would look like, and which the other
    ledger would report only as a NEW site, never as a LOST guarantee.
    """
    found = discovered_wrap_calls()
    expected = {key: row[0] for key, row in WRAP_CALLERS.items()}
    grew, shrank, moved = ledger_diff(found, expected)
    assert not grew and not shrank and not moved, (
        "the set of functions that read the store THROUGH a fail-closed wrap moved.\n"
        f"  APPEARED: {grew}\n"
        f"  VANISHED: {shrank}\n"
        f"  COUNT CHANGED (ledger -> source): {moved}\n"
        "A VANISHED caller is the serious one: it means a read that used to fail "
        "closed no longer does, and the sibling listing ledger cannot see that on "
        "its own. An APPEARED caller is good news that still has to be written "
        "down, because the two ledgers together are the answer to 'who reads the "
        "store, and through what'."
    )


def test_every_declared_wrap_name_is_real_and_used() -> None:
    """🔴 VALIDATE THE INSTRUMENT: a misspelled wrap name finds nothing, quietly.

    `PY_WRAP_NAMES` / `GO_WRAP_NAMES` are the only thing making the caller ledger
    non-empty. A typo, or a wrap renamed in the source, would empty it and leave
    `test_every_caller_of_a_failclosed_wrap_is_LEDGERED` asserting `{} == {}` — a
    green computed over nothing, which is the `positive-control` failure
    `claude/RULES.md` names. So each declared name must (a) be defined in the
    source and (b) actually be called somewhere.
    """
    recall = (REPO / "lib/subsystem_recall.py").read_text(encoding="utf-8")
    for name in PY_WRAP_NAMES:
        assert f"def {name}(" in recall, (
            f"{name!r} is declared as a Python wrap and has no definition in "
            f"lib/subsystem_recall.py — the caller ledger is measuring a typo"
        )
    load_go = (REPO / "internal/store/load.go").read_text(encoding="utf-8")
    for name in GO_WRAP_NAMES:
        bare = name.split(".", 1)[1]
        assert f"func {bare}(" in load_go, (
            f"{name!r} is declared as a Go wrap and has no definition in "
            f"internal/store/load.go"
        )
    found = discovered_wrap_calls()
    for name in PY_WRAP_NAMES + GO_WRAP_NAMES:
        hits = [k for k in found if k.endswith("::" + name)]
        assert hits, (
            f"the declared wrap {name!r} is CALLED NOWHERE in the searched set. "
            f"Either it is dead and should be deleted, or the scan cannot see its "
            f"call sites — and a zero here would make the caller ledger vacuous."
        )


def test_the_caller_ledger_fails_when_a_wrap_call_is_removed() -> None:
    """🔴 WATCH THE SHRINK ARM FIRE, with the specific key named.

    Round 3's finding was a site whose wrap call had never existed. The failure
    this guards is the reverse and is easier to ship by accident: deleting the
    call while deleting the read, or inlining it.
    """
    rel = "internal/client/routes.go"
    real = (REPO / rel).read_text(encoding="utf-8")
    anchor = "store.ScopeDirsOrUnreadable(cache)"
    assert anchor in real, (
        f"the mutation anchor {anchor!r} is not in {rel} — this control measures "
        f"nothing. Re-derive it before trusting this test."
    )
    mutated = real.replace(anchor, "os.ReadDir(cache)")
    after = discovered_wrap_calls({rel: mutated})
    expected = {key: row[0] for key, row in WRAP_CALLERS.items()}
    # 🔴 THROUGH `ledger_diff`, THE PRODUCTION COMPARISON — not a second copy of
    # the set arithmetic. A sweep found that re-implementing it here let
    # `shrank = []` in the assertion above SURVIVE a green run.
    _grew, missing, _moved = ledger_diff(after, expected)
    assert missing == ["internal/client/routes.go::Routes::store.ScopeDirsOrUnreadable"], (
        f"removing the wrap call did not show up as a VANISHED caller; "
        f"missing={missing}"
    )
    # and the SAME mutation must ALSO re-appear as a raw listing in the other
    # ledger — the two halves seeing one regression from both sides is the
    # property that makes re-opening this site loud twice.
    listings = discovered({rel: mutated})
    assert "internal/client/routes.go::Routes::os.ReadDir" in listings, (
        "re-opening a raw `os.ReadDir` in `Routes` was invisible to the listing "
        "ledger, so the two ledgers do not overlap where they must"
    )


def test_the_ledger_fails_when_a_site_appears_or_vanishes() -> None:
    """🔴 BOTH DIRECTIONS, WATCHED, WITHOUT WRITING TO THE TREE.

    The mutation is applied by substituting SOURCE TEXT through `discovered`'s
    `extra` hook, not by copying the repository — `claude/RULES.md` names the
    stale-`.pyc` trap that a `cp -a` copy walks into, where a same-length edit
    landing in the same second is scored SURVIVED without ever executing.
    Substituting text cannot have that failure mode: nothing is imported.

    Each half asserts the SPECIFIC breakage, not merely that something changed.
    """
    expected_counts = {key: row[0] for key, row in EXPECTED.items()}

    # ---- APPEAR: a brand-new listing in an already-searched file -------------
    real = (REPO / "lib/cairn_doctor.py").read_text(encoding="utf-8")
    with_new = real + (
        "\n\ndef _a_new_unledgered_read(root):\n"
        "    return sorted(p.name for p in root.iterdir())\n"
    )
    grown = discovered({"lib/cairn_doctor.py": with_new})
    # 🔴 THROUGH `ledger_diff` — see its docstring; computing this inline is what
    # let `grew = {}` in the production assertion SURVIVE a mutation sweep.
    new_keys, _shrank, _moved = ledger_diff(grown, expected_counts)
    assert new_keys == ["lib/cairn_doctor.py::_a_new_unledgered_read::iterdir"], (
        f"the GROW arm did not see a new listing read; it saw {new_keys}"
    )

    # ---- VANISH: a ledgered site removed from the source ---------------------
    # 🔴 THE ANCHOR IS ASSERTED BEFORE USE, AND THAT GUARD HAS ALREADY EARNED ITS
    # KEEP: this arm originally anchored on `os.ReadDir(cache)` in `routes.go`,
    # and the change that added this file WRAPPED that site — so the anchor went
    # stale inside the same commit that wrote it, the replace would have matched
    # zero sites, and the arm would have "passed" against an unmutated tree. It
    # failed loudly instead. `claude/RULES.md`: assert your mutation applied by
    # counting matched sites and failing on zero.
    go_rel = "internal/client/verbs.go"
    go_real = (REPO / go_rel).read_text(encoding="utf-8")
    anchor = "scopeDirs, _ := os.ReadDir(cache)"
    assert go_real.count(anchor) == 1, (
        f"the SHRINK mutation anchor {anchor!r} matched {go_real.count(anchor)} "
        f"sites in {go_rel}, not 1 — this control is measuring nothing. Re-derive "
        f"the anchor."
    )
    go_gone = go_real.replace(anchor, "scopeDirs := readDirRemoved(cache)")
    shrunk = discovered({go_rel: go_gone})
    _grew2, missing, _moved2 = ledger_diff(shrunk, expected_counts)
    assert missing == ["internal/client/verbs.go::LsEntries::os.ReadDir"], (
        f"the SHRINK arm did not see a ledgered site disappear; missing={missing}"
    )

    # ---- COUNT: a second listing inside an already-ledgered function ---------
    # 🔴 `cmd_put` LEGITIMATELY HAS TWO, so the count is a number the ledger
    # really reads. This proves a THIRD would be seen. The anchor is asserted
    # before use: an anchor that has gone stale makes the mutation apply to zero
    # sites and the assertion below would then be measuring the unmutated tree.
    py_rel = "lib/cairn_doctor.py"
    py_real = (REPO / py_rel).read_text(encoding="utf-8")
    anchor = "for entry in scope.iterdir():"
    assert py_real.count(anchor) == 1, (
        f"the COUNT mutation anchor {anchor!r} matched "
        f"{py_real.count(anchor)} sites in {py_rel}, not 1 — a `count=1` replace "
        f"on an ambiguous anchor hits an occurrence you did not picture. "
        f"Re-derive it."
    )
    py_doubled = py_real.replace(
        anchor, "for entry in list(scope.iterdir()) if scope.iterdir() else []:", 1
    )
    bumped = discovered({py_rel: py_doubled})
    _g3, _s3, moved3 = ledger_diff(bumped, expected_counts)
    assert moved3 == {"lib/cairn_doctor.py::store_entry_files::iterdir": (1, 2)}, (
        f"the COUNT arm did not surface through the production comparison: {moved3}"
    )
    assert bumped["lib/cairn_doctor.py::store_entry_files::iterdir"] == 2, (
        "the COUNT arm cannot see a SECOND listing added to an already-ledgered "
        "function, so `cmd_put`'s 2 is a number nothing checks. Got "
        f"{bumped.get('lib/cairn_doctor.py::store_entry_files::iterdir')!r}."
    )
