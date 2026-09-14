"""Put a DELIBERATELY WRONG server behind the suite, so its green means something.

🔴 A REASSURING ZERO IS INDISTINGUISHABLE FROM A HARNESS WIRED TO NOTHING. This
module exists so the conformance suite's own verdict can be validated the way
`tests/leakscan.py` validates its own: by feeding it a case it MUST fail.

  * NEGATIVE CONTROL — `mutated_server` copies `server/server.py`, applies one
    textual mutation, and returns a path `oracle.boot_oracle(server_py=...)` can
    start. The mutations the tests use are REALISTIC — a status code moved, a
    header dropped, a preserved mtime truncated to whole seconds, a refusal made
    distinguishable from an absence — not textbook edits the suite could only
    ever recognise in the form it was written for.
  * POSITIVE CONTROL — `unmutated_server` does the same copy with NO edit. It
    has to pass. Without it, "the mutant was caught" cannot be told apart from
    "the copied tree never booted", which is the same green-for-the-wrong-reason
    the mutation is meant to expose.

🔴 THE COPY IS A TREE, NOT A FILE. `server.py` finds its modules with
`Path(__file__).resolve().parents[1] / "lib"`, so a lone copy in `/tmp` imports
nothing. The mutant tree is `<tmp>/server/server.py` beside a SYMLINK to the
real `lib/` — read-only, shared, and never written to.
"""

from __future__ import annotations

from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parents[2]
SERVER_PY = REPO_ROOT / "server" / "server.py"
LIB = REPO_ROOT / "lib"


class MutationError(AssertionError):
    """The mutation did not apply, so the control would have proven nothing."""


def _tree(dest: Path) -> Path:
    (dest / "server").mkdir(parents=True, exist_ok=True)
    link = dest / "lib"
    if not link.exists():
        link.symlink_to(LIB)
    return dest / "server" / "server.py"


def unmutated_server(dest: Path) -> Path:
    """The POSITIVE control: the same copy mechanics, no edit."""
    target = _tree(dest)
    target.write_text(SERVER_PY.read_text(encoding="utf-8"), encoding="utf-8")
    return target


def mutated_server(dest: Path, old: str, new: str, *, count: int = 1) -> Path:
    """Copy the server with exactly `count` occurrences of `old` replaced.

    🔴 THE OCCURRENCE COUNT IS ASSERTED, because a `replace` that matched
    NOTHING would produce an unmutated server that the suite then passes — and
    the run would be reported as `this mutation was not caught`. That is the
    mutation sweep's classic false SURVIVED, and it is the one failure mode a
    negative control cannot afford.
    """
    source = SERVER_PY.read_text(encoding="utf-8")
    found = source.count(old)
    if found != count:
        raise MutationError(
            f"the mutation pattern occurs {found} time(s) in server.py, not "
            f"{count}. A pattern that matches nothing yields an UNMUTATED server "
            f"and the control would report the mutant as survived.\n  pattern: {old!r}"
        )
    target = _tree(dest)
    target.write_text(source.replace(old, new, count), encoding="utf-8")
    return target
