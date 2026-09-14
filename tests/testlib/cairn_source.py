"""Read facts out of the `cairn` client script's SOURCE, by AST.

🔴 SHARED BECAUSE TWO SUITES ASK THE SAME QUESTION, AND A SECOND COPY WOULD
DRIFT. `tests/test_cairn_write.py` reads the client's exit-code constants to prove
the write codes are disjoint from the read codes; `tests/test_cairn_doctor.py`
reads the same constants to grade which of them OVERLAP `cairn doctor`'s. Those
are two different claims over one operand set, and the operand set is the part
that must not be hand-listed — a hand list is blind to a constant added after it
was written, which is the defect this module was extracted to close.
`claude/RULES.md` → "One rule, one place".

🔴 NOT `exec`, AND NOT A REGEX. `exec`ing the file (even a prefix of it) fails on
`__file__`, which is absent from a synthetic namespace — measured, it raised
`NameError` at `Path(__file__)`. A regex over the source would silently miss a
re-spelling. The AST answers the question the test is actually asking: what
integer does this module bind to this name?

⚠ WHAT AST DISCOVERY CANNOT SEE, so a caller can guard it rather than assume it
away. `module_constants` reports a module-level `NAME = <literal>` whose literal
is an `int` — including the `NAME: int = <literal>` spelling, which it did NOT
see until the capability ledger needed the discovery to be right (measured: with
`ast.Assign` alone, `EXIT_ANN: int = 12` came back as `{}`, a silent undercount,
and `cairn` already uses that spelling for two dict constants). A constant bound
any OTHER way is still invisible:

  * a computed value (`EXIT_X = EXIT_OK + 10`) or any non-literal expression,
    `-1` INCLUDED — unary minus is an `ast.UnaryOp`, not a negative `Constant`;
  * a name bound by tuple unpacking (`EXIT_A, EXIT_B = 1, 2`) — the binding is
    seen, the per-element value is deliberately not resolved;
  * a name bound inside an `if`/`try`/function at module level — only
    `Module.body` is walked;
  * a name IMPORTED from another module;
  * `NAME: int` with NO value, which binds nothing and is correctly absent.

The first three are undercounts; the import would be an OVERcount if it were
visible, since a code another module owns is not the client's. Reading the
namespace of the exec'd module (`dir()`) has exactly the complementary blindness
— it sees them all and cannot tell them apart. A caller whose claim depends on
discovery being COMPLETE should cross-check the two, which is what
`tests/test_cairn_doctor.py`'s exit-code ledger does and what
`tests/test_capability_ledger.py` does for the SUBCOMMAND set; the mismatch is
then a loud failure asking a human which set the claim covers, rather than a
silent undercount.

🔴 AND FOR THE `EXIT_*` SET THE UNDERCOUNT IS LOUD AT THIS LAYER TOO.
`exit_constant_names` refuses rather than under-reports: a module-level name
spelled `EXIT_*` that this walker sees BOUND but cannot read as an int raises,
naming the shape. That covers the string case (`EXIT_X = "nope"`, which the int
filter otherwise drops in silence — leaving a caller cross-checking against
`dir()` to mis-diagnose it as "computed or imported"), the computed case, unary
minus, and a tuple/list/dict display. It does NOT cover a name this walker never
sees bound at all — the `if`-nested and imported cases above — so the `dir()`
cross-check is still what catches those, not this.
"""

from __future__ import annotations

import ast
from pathlib import Path

#: The client script. `parents[2]` is the repo root: this file is
#: `<root>/tests/testlib/cairn_source.py`.
CAIRN_CLI = Path(__file__).resolve().parents[2] / "cairn"


def cairn_ast() -> ast.Module:
    """`cairn` parsed. It has no `.py` extension, so it is read as text."""
    return ast.parse(CAIRN_CLI.read_text(encoding="utf-8"))


#: What `_module_bindings` reports for a name it saw BOUND but whose value it
#: will not claim to know — any non-`Constant` expression, and every element of a
#: tuple/list unpacking. Distinguishing "not bound here" from "bound to something
#: I cannot read" is the whole point: the first is a gap `dir()` has to close, the
#: second is a shape this walker can refuse LOUDLY.
_UNREADABLE = object()


def _module_bindings(tree: ast.Module) -> dict[str, object]:
    """Every module-level name bound by an assignment -> its literal, or `_UNREADABLE`.

    `Module.body` only, so nothing nested in an `if`/`try`/`def` is here. Both
    assignment spellings count: `NAME = v` (every target, so a chained
    `A = B = 13` binds both) and `NAME: T = v`. An annotation with no value
    (`NAME: int`) binds nothing and is skipped.
    """
    out: dict[str, object] = {}
    for node in tree.body:
        if isinstance(node, ast.Assign):
            targets: list[ast.expr] = list(node.targets)
        elif isinstance(node, ast.AnnAssign) and node.value is not None:
            targets = [node.target]
        else:
            continue
        literal = (
            node.value.value if isinstance(node.value, ast.Constant) else _UNREADABLE
        )
        for target in targets:
            if isinstance(target, ast.Name):
                out[target.id] = literal
            elif isinstance(target, (ast.Tuple, ast.List)):
                # The NAMES are visible; which value each one gets is not read.
                for element in target.elts:
                    if isinstance(element, ast.Name):
                        out[element.id] = _UNREADABLE
    return out


def module_constants(tree: ast.Module | None = None) -> dict[str, int]:
    """Module-level `NAME = <int literal>` from `cairn`, read by AST.

    See this module's header for what that does and does not see. `bool` is a
    subclass of `int`, so a `NAME = True` would be reported as 1; no such
    constant exists in `cairn` today and the callers all filter by name prefix.

    `tree` is for the INSTRUMENT CONTROLS: a caller can feed a synthetic module
    and watch the count move, which is the only way to prove a reassuring result
    came from a walker that works rather than from one wired to nothing. Omitted,
    it reads `cairn` — every production caller does that.
    """
    bindings = _module_bindings(cairn_ast() if tree is None else tree)
    return {n: v for n, v in bindings.items() if isinstance(v, int)}


def exit_constant_names(tree: ast.Module | None = None) -> set[str]:
    """The `EXIT_*` names `cairn` ASSIGNS an integer literal to, at module level.

    ⚠ THE `EXIT_` PREFIX IS A SPELLING, NOT A STRUCTURE, and that is a limit
    worth naming: a client exit code named anything else — `RC_STALE` — is not
    discovered here. It is the convention the file has followed for all nine of
    its codes, and the same convention `cairn_doctor`'s own discovery relies on
    (`EXIT_DOCTOR_*`), so the two sides are at least blind in the same way.

    🔴 RAISES ON AN `EXIT_*` IT CANNOT READ AS AN INT rather than omitting it.
    See the header: silence here is an undercount that every caller's claim then
    inherits, and the int filter alone cannot tell "there is no such code" from
    "there is one and I could not read it".
    """
    bindings = _module_bindings(cairn_ast() if tree is None else tree)
    unreadable = sorted(
        n for n, v in bindings.items()
        if n.startswith("EXIT_") and not isinstance(v, int)
    )
    if unreadable:
        raise AssertionError(
            f"`cairn` binds {unreadable} at module level and this walker cannot "
            f"read the value as an int literal — a computed expression, a unary "
            f"minus, a string, a display, or a tuple unpacking. Reporting the "
            f"name would put a non-code in an exit-code set and DROPPING it "
            f"silently undercounts every ledger built on this function. Bind it "
            f"to an int literal, rename it out of the `EXIT_` space if it is not "
            f"a code, or widen `_module_bindings` on purpose."
        )
    return {
        n for n, v in bindings.items()
        if isinstance(v, int) and n.startswith("EXIT_")
    }


def subcommand_names(tree: ast.Module | None = None) -> set[str]:
    """Every verb `cairn` registers with `<subparsers>.add_parser("<verb>")`, by AST.

    🔴 DISCOVERED, NEVER HAND-LISTED. `tests/test_capability_ledger.py` asserts
    the capability ledger against this set in BOTH directions, so a hand list
    here would make the "every verb has a row" half of that gate an assertion
    about the hand list. The same reasoning as `module_constants` above, applied
    to the CLI surface instead of the exit codes.

    🔴 RAISES rather than skipping when it finds an `add_parser` whose verb it
    cannot read — no positional argument, or one that is not a string literal
    (`add_parser(name)` inside a loop over a table). A skip there is the silent
    undercount that makes "every verb has a ledger row" vacuous for exactly the
    verb somebody built dynamically.

    ⚠ WHAT THIS CANNOT SEE, and why the runtime cross-check in
    `tests/test_capability_ledger.py` is not decoration:

      * it matches on the ATTRIBUTE NAME `add_parser`, not on the receiver being
        an `argparse` subparsers action, so an unrelated `x.add_parser("y")`
        would be an OVERcount;
      * a verb registered by any other mechanism — `choices=`, a second
        `add_subparsers()`, a parser assembled in a helper called with a computed
        name — is invisible where it does not raise;
      * `writes=True`, which is what separates a write verb from a read verb,
        arrives through `set_defaults` on a variable this walker would have to
        track back to its `add_parser` call. It is read from the BUILT parser
        instead, where argparse itself has resolved it.
    """
    out: set[str] = set()
    for node in ast.walk(cairn_ast() if tree is None else tree):
        if not isinstance(node, ast.Call):
            continue
        if not isinstance(node.func, ast.Attribute):
            continue
        if node.func.attr != "add_parser":
            continue
        first = node.args[0] if node.args else None
        if not (isinstance(first, ast.Constant) and isinstance(first.value, str)):
            raise AssertionError(
                f"`add_parser` at line {node.lineno} of the module under "
                f"discovery ({CAIRN_CLI.name}, unless a caller supplied its own "
                f"tree) does not "
                f"name its verb with a string literal, so AST discovery cannot "
                f"see the verb it registers. Every ledger assertion built on this "
                f"function would be silently blind to it. Name the verb "
                f"literally, or widen this walker and the ledger together."
            )
        out.add(first.value)
    return out
