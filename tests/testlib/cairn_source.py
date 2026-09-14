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
away. `module_constants` reports module-level `NAME = <int literal>` only, so a
constant bound any OTHER way is invisible to it: a computed value
(`EXIT_X = EXIT_OK + 10`), a name bound inside an `if`, or one IMPORTED from
another module. The first two are undercounts; the third would be an OVERcount if
it were visible, since a code another module owns is not the client's. Reading
the namespace of the exec'd module (`dir()`) has exactly the complementary
blindness — it sees all three and cannot tell them apart. A caller whose claim
depends on discovery being COMPLETE should cross-check the two, which is what
`tests/test_cairn_doctor.py`'s ledger does; the mismatch is then a loud failure
asking a human which set the claim covers, rather than a silent undercount.
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


def module_constants() -> dict[str, int]:
    """Module-level `NAME = <int>` from `cairn`, read by AST.

    See this module's header for what that does and does not see. `bool` is a
    subclass of `int`, so a `NAME = True` would be reported as 1; no such
    constant exists in `cairn` today and the callers all filter by name prefix.
    """
    out: dict[str, int] = {}
    for node in cairn_ast().body:
        if isinstance(node, ast.Assign) and len(node.targets) == 1:
            target = node.targets[0]
            if isinstance(target, ast.Name) and isinstance(node.value, ast.Constant):
                if isinstance(node.value.value, int):
                    out[target.id] = node.value.value
    return out


def exit_constant_names() -> set[str]:
    """The `EXIT_*` names `cairn` ASSIGNS an integer literal to, at module level.

    ⚠ THE `EXIT_` PREFIX IS A SPELLING, NOT A STRUCTURE, and that is a limit
    worth naming: a client exit code named anything else — `RC_STALE` — is not
    discovered here. It is the convention the file has followed for all nine of
    its codes, and the same convention `cairn_doctor`'s own discovery relies on
    (`EXIT_DOCTOR_*`), so the two sides are at least blind in the same way.
    """
    return {n for n in module_constants() if n.startswith("EXIT_")}
