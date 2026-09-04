#!/usr/bin/env python3
"""One predicate for "is this a usable timeout", shared by every bounded call.

🔴 CONSOLIDATING THIS IS WHAT FOUND THE BUG IT GUARDS AGAINST. The rule was once
open-coded at two call sites and the copies DISAGREED: only one excluded `bool`.
A comment claiming the two mirrored each other was therefore false, and the
weaker copy was measured running with a ONE-SECOND bound while reporting a
nonsense duration — the "nobody notices" failure that motivates the rule.
"""
from __future__ import annotations

#: Seconds. A default, never a ceiling — callers may pass their own.
DEFAULT_TIMEOUT = 60


def unbounded_timeout_reason(value) -> str | None:
    """Why `value` is not a usable timeout, or `None` if it is fine.

    `bool` is the trap: it subclasses `int`, so a plain `isinstance(x, int)`
    accepts `True` and silently yields a 1-second timeout. `None` is the other:
    it means NO timeout to both `subprocess` and `urlopen` — an unbounded wait
    rather than a default.

    Returns a REASON rather than raising, so each caller can raise its own
    exception type without coupling them to one another's error class.
    """
    if isinstance(value, bool):
        return (f"timeout={value!r} is a bool — it subclasses int, so this "
                "would silently run with a 1-second bound")
    if not isinstance(value, int):
        return (f"timeout={value!r} is not an int — a missing bound is an "
                "UNBOUNDED wait, not a default")
    if value <= 0:
        return (f"timeout={value!r} is not positive — a non-positive bound is "
                "an UNBOUNDED wait, not a default")
    return None
