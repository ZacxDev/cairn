"""THE one definition of the `SUBSYSTEM_STORE_*` → `CAIRN_*` rename — Python side.

🔴 THERE ARE TWO SPELLINGS OF THIS LEDGER AND THAT IS PACKAGING, NOT DUPLICATION.
`packages.cairn` installs the client script and `lib/` under `libexec` and nothing
else, so this side CANNOT import `internal/envalias`. That Go package is the other
spelling, and `tests/test_env_aliases.py` pins the two against each other — failing
when the pair set GROWS *or* SHRINKS, and comparing the rendered warnings as WHOLE
NORMALISED STRINGS rather than by keyword, because a guard on words is walkable by
rewording. Two spellings plus a gate is the shape `AGENTS.md` already blesses for
`server/Dockerfile` against `flake.nix`'s `serverEnv`.

Read `internal/envalias/envalias.go`'s package doc for the reasoning in full — the
`CAIRN_HOST` collision, the removal anchor, and the one behaviour change.
The short form, because a reader editing THIS file should not have to open that one:

* **The new name wins WITHIN ONE SOURCE** — one environment, or one config file.
  The old name is read only when the new one is absent or blank *there*. Which
  SOURCE supplies the value composes the other way round (environment before file,
  and for the default instance only); `README.md` owns that, and it is why the two
  warning texts below each speak about one source.
* 🔴 **A PRESENT BUT EMPTY VALUE COUNTS AS ABSENT, AND THAT IS AN AUTHORISED CHANGE
  TO THE ORACLE** — decision: the operator, on PR #69. `server.py`'s `main()` used to
  hand argparse `""` for `--store` and to raise `ValueError` on a blank `--port`;
  every Go call site and this client's `load_config` already treated blank as absent,
  so the rule removes a divergence rather than creating one. 🔴 The conformance corpus
  is structurally blind to it — no row sends an empty value and none can, the corpus
  describing REQUESTS while this decides STARTUP — so a green corpus says nothing
  about this rule. The declaration, the accepted cost and the two guards that stand in
  the corpus's place are in `tests/conformance/README.md`.
* **An old name that is present and non-blank warns once per process**, including
  when the new name shadows it. A blank value is how a caller UNSETS an alias — it
  changes no resolution, so it is not a deprecation and does not warn. 🔴 **"Blank" is
  ONE predicate** — `_blank`, below — **read by both halves.** Spelled inline on each
  side, the two disagreed: a whitespace-only old value resolved *and* did not warn,
  which is the one combination that sentence rules out.
* **Warnings are sorted by NEW name.** `tests/parity/harness.py` compares the two
  clients' stderr BYTE-FOR-BYTE, so the order has to be a property of the ledger
  rather than of whatever order the lookups happened in.

🔴 TWO PAIRS ARE NOT THE MECHANICAL PREFIX SWAP. `SUBSYSTEM_STORE_HOST` is the pod's
LISTEN ADDRESS, while `CAIRN_HOST` already exists and is the machine LABEL
(`host_identity.HOST_LABEL_ENV[0]`, rendered into client output) — so the mechanical
swap would make a pod bind to an operator's machine label AND let a listen address
hijack the label in rendered output. Hence `CAIRN_LISTEN_HOST`. And `CAIRN_ROOT`
would read as a sibling of the client-side `*_ROOT` names when it is the POD's store
root and belongs to none of them. Hence `CAIRN_STORE_ROOT`. ⚠ The live one is
`CAIRN_MIRROR_ROOT` alone: nothing in either language reads `CAIRN_CACHE_ROOT`, which
an earlier draft of this paragraph named as live.

⚠ `SUBSYSTEM_STORE_ROUTES_ISH`, in `tests/test_env_pin.py`, is a negative-control
FIXTURE and not a variable anything reads. It is deliberately not in this ledger.
"""

from __future__ import annotations

from typing import Iterable, Mapping

__all__ = [
    "LEDGER",
    "REMOVAL_ANCHOR",
    "deprecations",
    "env_warning",
    "file_deprecations",
    "file_warning",
    "old_name",
    "reset_warned_for_test",
    "value",
    "value_or",
    "warn_once",
]

#: Every renamed variable as `(new, old)`, SORTED BY NEW NAME.
#:
#: 🔴 THE ORDER IS LOAD-BEARING, NOT COSMETIC — see the third bullet above.
#: `tests/test_env_aliases.py` pins it, so a pair appended in the wrong place is a
#: red test rather than a stderr diff the parity harness discovers later.
LEDGER: tuple[tuple[str, str], ...] = (
    ("CAIRN_CONFIG", "SUBSYSTEM_STORE_CONFIG"),
    ("CAIRN_FAILURE_WINDOW_S", "SUBSYSTEM_STORE_FAILURE_WINDOW_S"),
    ("CAIRN_LISTEN_HOST", "SUBSYSTEM_STORE_HOST"),
    ("CAIRN_LOCKOUT_S", "SUBSYSTEM_STORE_LOCKOUT_S"),
    ("CAIRN_MAX_FAILURES", "SUBSYSTEM_STORE_MAX_FAILURES"),
    ("CAIRN_PORT", "SUBSYSTEM_STORE_PORT"),
    ("CAIRN_STORE_ROOT", "SUBSYSTEM_STORE_ROOT"),
    ("CAIRN_TOKEN", "SUBSYSTEM_STORE_TOKEN"),
    ("CAIRN_TOKEN_FILE", "SUBSYSTEM_STORE_TOKEN_FILE"),
    ("CAIRN_TRUSTED_PROXIES", "SUBSYSTEM_STORE_TRUSTED_PROXIES"),
    ("CAIRN_URL", "SUBSYSTEM_STORE_URL"),
)

#: WHEN the old names stop being read, in the only terms this repo can state it: a
#: milestone a reader can CHECK — `packages.cairn` either exists in `flake.nix` or it
#: does not. A date could not be checked, there is no semver here to hang one on
#: (`flake.nix`: `version = self.shortRev`), and `tests/leakscan.py` refuses a dated
#: stamp in this tree anyway.
REMOVAL_ANCHOR = "the Python client (packages.cairn) is retired"

#: The two pinned warning texts.
#:
#: 🔴 EACH LINE SPEAKS ONLY ABOUT ITS OWN SOURCE, AND THE EARLIER WORDING DID NOT —
#: measured false on both real clients. They used to end "…is read only when ${new} is
#: unset" / "…absent from that file", which reads as a claim about the whole
#: configuration; it is not one, because precedence COMPOSES. A config file holding
#: `CAIRN_URL=…:19001` — what README.md's quickstart tells you to write — plus an
#: exported `SUBSYSTEM_STORE_URL=…:19002` sends both clients to 19002, while the line
#: said the old name is read "only when $CAIRN_URL is unset" and it was set. The mirror
#: case misleads identically. An operator who had just migrated would read either line
#: as "my migration is live" and be wrong.
#:
#: 🔴 SO THE LINES STATE THE WITHIN-SOURCE RULE, the only rule a warning can know: no
#: cross-source sentence is truthful here, because the environment beats the file for the
#: DEFAULT instance and is not consulted at all for a non-default one (see
#: `load_config`'s own `where` text). The composition, with that caveat, is in
#: `README.md`.
#:
#: 🔴 STILL ONE TEXT PER DESTINATION, USED WHETHER OR NOT THE NEW NAME SHADOWS THE OLD
#: ONE, and now for a reason that holds: the sentence states the RULE rather than this
#: run's outcome, so it is true in both cases. A second "…and it is being ignored
#: because you also set X" wording would double the strings the cross-language gate has
#: to pin and double the ways the two spellings can drift.
#:
#: 🔴 `tests/test_env_aliases.py` READS THESE TWO CONSTANTS AND THEIR GO TWINS and
#: compares the rendered results. Editing one alone is a red test, which is why they
#: are named constants rather than inline literals at the call sites.
ENV_WARNING_FORMAT = (
    "${old} is a deprecated alias for ${new}. Where both are set in the environment, "
    "${new} is the one that is read. Both are accepted until {anchor}."
)
FILE_WARNING_FORMAT = (
    "{old} in {path} is a deprecated alias for {new}. Where both appear in that file, "
    "{new} is the one that is read. Both are accepted until {anchor}."
)

_NEW_TO_OLD = {new: old for new, old in LEDGER}
_OLD_TO_NEW = {old: new for new, old in LEDGER}

#: The once-per-process-per-LINE set behind `warn_once`.
#:
#: Keyed on the WHOLE LINE rather than on the old name, so the same variable
#: deprecated in the environment AND named as a key in a config file produces both
#: warnings — they say different things and send the operator to different places —
#: while two lookups of the same variable produce one.
_WARNED: set[str] = set()


def old_name(new: str) -> str:
    """The deprecated spelling of `new`, or `""` if there is not one.

    `""` rather than a `KeyError` so a caller naming something that was never renamed
    — `CAIRN_ROUTES`, `CAIRN_UI_PORT` — gets plain single-name behaviour out of
    `value` instead of a crash.
    """
    return _NEW_TO_OLD.get(new, "")


def _blank(v: str) -> bool:
    """The ONE definition of "this variable is not set", applied to BOTH spellings.

    🔴 A NAMED FUNCTION BECAUSE THE TWO HALVES OF THE RULE DISAGREED WHILE BOTH WERE
    SPELLED INLINE, AND `tests/parity/` WAS BLIND TO IT BECAUSE `internal/envalias` HAD
    THE SAME DISAGREEMENT. `_present` tested `.strip()`; `value` returned the old name's
    value RAW. So `SUBSYSTEM_STORE_ROOT="  "` resolved to `"  "` — a pod would have taken
    a whitespace store root — and warned about nothing, while the module docstring said a
    blank value "changes no resolution, so it is not a deprecation". Two clients failing
    identically compare equal; only reading the two halves against each other found it.

    ⚠ WHICH HALF WAS WRONG IS A DECISION, AND THIS IS IT: the RESOLUTION half. The
    alternative — warn whenever an old name holds any non-empty string — keeps a resolved
    value no operator can have meant. Treating it as absent makes the old name behave
    exactly like the new one, which has been `.strip()`-tested since this module was
    written, and lets `value_or`'s fallback (the code default) run.

    ⚠ IT TESTS BLANKNESS AND DOES NOT TRIM: a non-blank value comes back RAW, because
    trimming a real value is the caller's business and would silently rewrite a store root
    that legitimately ends in a space.
    """
    return not v.strip()


def value(env: Mapping[str, str], new: str) -> str:
    """`new` if present and non-blank, else its old spelling, else `""`.

    The ONLY place the precedence rule is written. Every call site goes through this;
    none open-codes a fallback, because a predicate duplicated across call sites
    regenerates the same bug at every site.
    """
    direct = env.get(new, "")
    if not _blank(direct):
        return direct
    old = _NEW_TO_OLD.get(new, "")
    if old:
        fallback = env.get(old, "")
        if not _blank(fallback):
            return fallback
    return ""


def value_or(env: Mapping[str, str], new: str, fallback: str) -> str:
    """`value`, with a default for "neither name is set"."""
    return value(env, new) or fallback


def env_warning(new: str, old: str) -> str:
    """The pinned text for an environment variable."""
    return ENV_WARNING_FORMAT.format(old=old, new=new, anchor=REMOVAL_ANCHOR)


def file_warning(new: str, old: str, path: str) -> str:
    """The pinned text for a config-FILE key.

    🔴 IT NAMES THE FILE, NOT `$VAR`. `SUBSYSTEM_STORE_URL=` inside
    `~/.config/subsystem-store/env` is not an exported variable, and telling an
    operator to "unset $SUBSYSTEM_STORE_URL" when the string lives in a file they have
    to EDIT sends them looking in the wrong place.
    """
    return FILE_WARNING_FORMAT.format(old=old, new=new, path=path, anchor=REMOVAL_ANCHOR)


def _present(mapping: Mapping[str, str]) -> list[tuple[str, str]]:
    """Every `(new, old)` whose OLD name is present and non-blank, sorted by new.

    Blankness through `_blank`, the SAME predicate `value` resolves with. Spelled inline
    on both sides once, the two disagreed; see `_blank`.
    """
    return sorted(
        ((new, old) for new, old in LEDGER if not _blank(mapping.get(old, ""))),
        key=lambda pair: pair[0],
    )


def deprecations(env: Mapping[str, str]) -> list[str]:
    """One warning line per old name present in `env`, sorted by NEW name.

    A pure function of `env`: no process state, no emission. `warn_once` adds the
    once-per-process rule, and keeping them apart is what lets a test assert the ORDER
    without reaching into module state.
    """
    return [env_warning(new, old) for new, old in _present(env)]


def file_deprecations(from_file: Mapping[str, str], path: str) -> list[str]:
    """`deprecations` for the keys of one config file."""
    return [file_warning(new, old, path) for new, old in _present(from_file)]


def warn_once(lines: Iterable[str], emit) -> None:
    """Emit each line through `emit`, at most once per process.

    The caller supplies `emit` because the destination differs and the format differs
    with it: the CLI writes bare lines to stderr, the pod writes
    `subsystem-store-api: <line>` through `reload_safe`. A module that wrote to
    `sys.stderr` itself would need a knob for the prefix, and the prefix would then be
    a second thing to keep in step across two languages.
    """
    for line in lines:
        if line in _WARNED:
            continue
        _WARNED.add(line)
        emit(line)


def reset_warned_for_test() -> None:
    """Clear the once-per-process set.

    Public because `warn_once`'s whole contract is module-global state, and a test that
    could not clear it would be order-dependent: the second test to assert an emission
    would see nothing and pass for the wrong reason.
    """
    _WARNED.clear()
