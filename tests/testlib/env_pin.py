"""Which environment variables configure the `cairn` client — THE one definition.

🔴 WHY THIS MODULE EXISTS: THE PREDICATE WAS OPEN-CODED AT FOUR SITES AND THEY
DISAGREED. Every suite that drives the client has to stop the developer's own
environment reaching it — a run that reads the operator's live store is not a
test — and each site answered "which variables are those?" for itself:

  * `tests/test_cairn_doctor.py` swept by PREFIX (`monkeypatch.delenv`);
  * `tests/test_cairn_instances.py` popped a hand-written list of FIVE names;
  * `tests/unchanged_output_capture.py` SET those five, at two separate sites,
    over an inherited `os.environ` copy.

`claude/RULES.md`: *"One rule, one place. A predicate duplicated across call
sites regenerates the same bug at every site."* And its corollary — consolidation
is a bug-finding instrument, because unifying the copies is what makes the
disagreement audible.

⚠ **WHAT THE DISAGREEMENT TURNED OUT TO BE, MEASURED RATHER THAN ASSERTED.** The
three list-based sites do not clear `CAIRN_HOST`/`ASIB_HOST`/`ACTIVITY_HOST`
(`host_identity.HOST_LABEL_ENV`), which a prefix sweep does. That is a REAL
divergence and it is **not** a live defect today: `tests/test_cairn_instances.py`
was run with `CAIRN_HOST=hostile-developer-box` exported and returned **63
passed**, identical to the unset run, because nothing it asserts on is derived
from the host label. Recorded as measured-not-reachable rather than written up as
a bug — an absence of successes is not evidence of a defect. What consolidating
buys is the FORWARD case: the next assertion that does read a host label, and the
sixth variable nobody has added yet.

🔴 A PREFIX, NOT A LEDGER OF NAMES, AND THAT IS THE WHOLE POINT. An enumerated
list is the thing that goes stale: a variable added to the client is inherited
silently by every suite until somebody remembers four files. A prefix sweep pins
a sixth variable AUTOMATICALLY. A discovered ledger — an AST walk over the client
naming exactly what it reads — was built and then deleted in favour of this:
~120 lines to REPORT an unpinned variable, against three lines that PIN it, which
is what the suites actually want. If discovered coverage of this surface is ever
wanted for its own sake, it belongs beside the other AST readers in
`testlib/cairn_source.py`, graded for all four sites at once.
"""
from __future__ import annotations

import os
from typing import Mapping

#: Every variable that configures the CLIENT carries one of these.
CONFIG_ENV_PREFIXES = ("SUBSYSTEM_STORE_", "CAIRN_")

#: …except the harness's own knobs, which are NOT client configuration.
#: `testlib/store_siting.py` reads `CAIRN_TEST_TMPFS` at call time to decide
#: where a test store is sited; clearing it would reconfigure the HARNESS rather
#: than the client, which is the opposite of what every caller here wants.
#: 🔴 The exemption is a PREFIX so a second harness knob is covered on the day it
#: is added — the same argument this module makes against enumerating anything.
HARNESS_ENV_PREFIX = "CAIRN_TEST_"


def is_client_config(name: str) -> bool:
    """Is `name` a variable that configures the client (rather than the harness)?"""
    return name.startswith(CONFIG_ENV_PREFIXES) and not name.startswith(
        HARNESS_ENV_PREFIX
    )


def inherited_config(env: Mapping[str, str] | None = None) -> list[str]:
    """Every client-configuration variable present in `env` (default: the real one).

    Returned as a list rather than yielded because callers mutate the mapping
    they are iterating — `monkeypatch.delenv` over a live `os.environ` is the
    case that made this explicit.
    """
    return [n for n in list(os.environ if env is None else env) if is_client_config(n)]


def sanitized_env(**overrides: str) -> dict[str, str]:
    """A copy of `os.environ` with the client's configuration REMOVED, then
    `overrides` applied — for a suite that drives the client as a subprocess.

    🔴 CLEAR FIRST, THEN OVERRIDE, AND THE ORDER IS THE MECHANISM. Three call
    sites used to build `dict(os.environ)` and `update()` a handful of pinned
    names onto it, which pins exactly the names somebody listed and lets every
    other one through. Clearing by prefix first means an override is a
    DELIBERATE value rather than the only value that happened to be named.

    Callers pass what they want pinned; everything else about the environment
    (`PATH`, `HOME` if they do not override it, the locale) is inherited on
    purpose, because a subprocess needs an interpreter it can find.
    """
    env = dict(os.environ)
    for name in inherited_config(env):
        env.pop(name, None)
    env.update(overrides)
    return env
