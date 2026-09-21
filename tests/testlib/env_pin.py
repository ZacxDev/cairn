"""Which environment variables configure the `cairn` client — THE one definition.

🔴 WHY THIS MODULE EXISTS: THE PREDICATE WAS OPEN-CODED AT SEVEN SITES AND THEY
DISAGREED. Every suite that drives the client has to stop the developer's own
environment reaching it — a run that reads the operator's live store is not a
test — and each site answered "which variables are those?" for itself:

  * `tests/test_cairn_doctor.py` swept by PREFIX (`monkeypatch.delenv`);
  * `tests/test_cairn_instances.py` popped a hand-written list of FIVE names;
  * `tests/unchanged_output_capture.py`, at TWO sites, and
    `tests/parity/harness.py`, and `tests/test_cairn_cli.py`, and
    `tests/test_cairn_write.py` each SET a handful of names over an inherited
    `os.environ` copy — which pins the names somebody listed and inherits every
    other one.

`claude/RULES.md`: *"One rule, one place. A predicate duplicated across call
sites regenerates the same bug at every site."* And its corollary — consolidation
is a bug-finding instrument, because unifying the copies is what makes the
disagreement audible.

🔴 **THE COUNT IN THE HEADING WAS "FOUR", THEN "FIVE", AND BOTH WERE WRONG — THE
MISCOUNT IS THE POINT, NOT AN ERRATUM.** The first draft of this module listed
four sites and OMITTED `tests/parity/harness.py`, which is the one site whose
omission was the measured defect. Two independent audits then found two more
(`test_cairn_cli.py`, `test_cairn_write.py`). A module that claims to be the one
definition while undercounting its own consumers sends the next reader to the
wrong set, which is the failure it was written to end. **The set is not restated
anywhere it can drift from: `tests/test_env_pin.py` asserts it, failing when it
GROWS or SHRINKS.**

## What the swept set is, and why it is not a list of names

`CONFIG_ENV_PREFIXES` plus `EXTRA_CONFIG_ENV`, minus `HARNESS_ENV_PREFIX`.

🔴 A PREFIX, NOT A LEDGER OF NAMES. An enumerated list is the thing that goes
stale: a variable added to the client is inherited silently by every suite until
somebody remembers seven files. A prefix sweep pins a new one AUTOMATICALLY, and
that is not a speculative benefit — `CAIRN_ROUTES` was added in #24 and reached
three of the four sites that existed, missing the fourth. A discovered ledger —
an AST walk over the client naming exactly what it reads — was built and then
deleted in favour of this: ~120 lines to REPORT an unpinned variable, against
three lines that PIN it.

🔴 **BUT A PREFIX CANNOT COVER A NAME THAT DOES NOT CARRY ONE, AND THIS MODULE'S
FIRST DRAFT CLAIMED OTHERWISE.** It asserted — in the paragraph it labelled
MEASURED — that the sweep clears `CAIRN_HOST`, `ASIB_HOST` and `ACTIVITY_HOST`.
Measured: `CAIRN_HOST swept=True`, **`ASIB_HOST swept=False`,
`ACTIVITY_HOST swept=False`** — neither of the last two begins with a swept
prefix, so the sweep never touched them. Worse than a false sentence, it INVERTED
a behaviour: clearing `CAIRN_HOST` while leaving `ASIB_HOST` makes the latter
authoritative in `host_identity.host_label()`, where before the inherited
`CAIRN_HOST` masked it — so an operator's real machine name could reach a PUBLIC
repository's failure logs, which is the exact hazard the harnesses pin
`CAIRN_HOST` to prevent.

`EXTRA_CONFIG_ENV` closes it by DERIVING the names from the client's own
declaration rather than restating them, so a fourth host-label variable is
covered on the day it is added.
"""
from __future__ import annotations

import os
import sys
from pathlib import Path

_LIB = Path(__file__).resolve().parents[2] / "lib"
if str(_LIB) not in sys.path:
    sys.path.insert(0, str(_LIB))

from host_identity import HOST_LABEL_ENV  # noqa: E402

#: Every variable that configures the client and carries a shared prefix.
CONFIG_ENV_PREFIXES = ("SUBSYSTEM_STORE_", "CAIRN_")

#: …and the ones that do not. DERIVED from `lib/host_identity.py`, never
#: restated: `HOST_LABEL_ENV` is `("CAIRN_HOST", "ASIB_HOST", "ACTIVITY_HOST")`
#: and only the first carries a swept prefix. Deriving it is what makes a fourth
#: host-label name covered without anybody editing this file.
EXTRA_CONFIG_ENV = frozenset(HOST_LABEL_ENV)

#: …except the harness's own knobs, which are NOT client configuration.
#: `testlib/store_siting.py` reads `CAIRN_TEST_TMPFS` at call time to decide
#: where a test store is sited; clearing it would reconfigure the HARNESS rather
#: than the client, which is the opposite of what every caller here wants.
#:
#: ⚠ REACHABLE FROM NO CURRENT CONSUMER, AND THAT SCOPE IS PART OF THE CLAIM.
#: `store_siting` is imported by `tests/test_subsystem_store_api.py` alone, which
#: does not use this module, and no subprocess any consumer spawns reads the
#: variable. It is cheap insurance against the day one does — stated with its
#: scope rather than as a live guarantee, because a previous draft dropped the
#: caveat while moving the claim somewhere more central, which is widening a
#: rule by deleting what bounded it.
HARNESS_ENV_PREFIX = "CAIRN_TEST_"


def is_client_config(name: str) -> bool:
    """Is `name` a variable that configures the client (rather than the harness)?"""
    if name.startswith(HARNESS_ENV_PREFIX):
        return False
    return name.startswith(CONFIG_ENV_PREFIXES) or name in EXTRA_CONFIG_ENV


def inherited_config() -> list[str]:
    """Every client-configuration variable present in the real environment.

    A list rather than a generator because callers mutate the mapping while
    iterating it — `monkeypatch.delenv` over a live `os.environ` is the case that
    made that explicit.
    """
    return [n for n in list(os.environ) if is_client_config(n)]


def sanitized_env(**overrides: str) -> dict[str, str]:
    """A copy of `os.environ` with the client's configuration REMOVED, then
    `overrides` applied — for a suite that drives the client as a subprocess.

    🔴 CLEAR FIRST, THEN OVERRIDE, AND THE ORDER IS THE MECHANISM. Five call
    sites used to build `dict(os.environ)` and `update()` a handful of pinned
    names onto it, which pins exactly the names somebody listed and lets every
    other one through. Clearing by prefix first means an override is a
    DELIBERATE value rather than the only value that happened to be named.

    Callers pass what they want pinned; everything else about the environment
    (`PATH`, the locale, `HOME` if they do not override it) is inherited on
    purpose, because a subprocess needs an interpreter it can find.
    """
    env = dict(os.environ)
    for name in inherited_config():
        env.pop(name, None)
    env.update(overrides)
    return env


def sanitized_env_with(base: dict[str, str], **overrides: str) -> dict[str, str]:
    """`sanitized_env` for a caller that already has a dict of pins to apply.

    ⚠ IT EXISTS BECAUSE `f(HOME=..., **extra)` RAISES WHEN `extra` ALSO CARRIES
    `HOME`. The call it replaced was `env.update(env_extra)`, which honoured such
    an override; routing the same data through keyword arguments turned a
    working override into `TypeError: got multiple values for keyword argument`.
    No caller does it today — which is exactly why it would have been found by
    somebody writing a new test rather than by this suite.
    """
    return sanitized_env(**{**base, **overrides})
