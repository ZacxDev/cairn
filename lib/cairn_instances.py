#!/usr/bin/env python3
"""WHICH INSTANCE does a scope live on, and where is each instance configured?

A deployment may point one client at more than one cairn. This module owns the
three questions that creates — what is configured, which instance a scope
belongs to, and whether the two agree — and it owns them for a client that
knows NOTHING about who routes where: the scope->alias table is an INPUT (a
file), never a constant in this repository. A public tool that shipped somebody's
taxonomy would be publishing their org chart.

🔴 AN UNROUTED SCOPE REFUSES. It does not fall back to the first instance, the
default instance, or the one that happens to answer. A default silently
recreates the shape that has already cost entries in this system's history —
writes landing in a store nobody reads, discovered days later by a diagnostic
built for something else. A refusal costs one error message, and it names the
scope so the remedy is a single line in the table.

🔴 ONE INSTANCE IS NOT ROUTING, AND THAT IS THE WHOLE COMPATIBILITY STORY.
With a single configured instance there is no second place a scope could go,
so an unregistered scope answers with it and NOTHING about the single-instance
client changes — no label, no table, no refusal. The alternative — refusing
until a table exists — would break every existing deployment on upgrade in
exchange for a guarantee nobody needs while there is one store.

🔴 AND "IS THIS HOST ROUTING?" IS NOT ONE QUESTION, WHICH IS THE CORRECTION
THIS MODULE CARRIES. It used to be, and the single boolean was
`routes is not None or len(instances) > 1` — table presence counted as routing.
That is wrong in both directions on a ONE-instance host that has a table, which
is the state an operator reaches the moment they write one:

    case (ONE instance configured)          | what must happen
    ----------------------------------------|------------------------------------
    no table at all                         | resolve to the sole instance
    a table, and the scope is NOT in it     | resolve to the sole instance
    a table entry naming an UNCONFIGURED    | REFUSE — the table names where the
      alias                                 | scope lives and this host cannot
                                            | reach it

Rows 2 and 3 need OPPOSITE answers from the same configuration, so no single
`active` flag can decide both: with the old predicate row 2 refused (a scope
nobody had added to the table yet became unreadable), and simply deleting the
`routes is not None` disjunct makes row 3 resolve to the sole instance — a
SILENT MISROUTE, a write landing in a store nobody decided on, which is worse
than the refusal it replaces. So there are two predicates: `multi_instance`
decides what is LABELLED, and `alias_for` consults the TABLE FIRST and falls
back to the sole instance only when the table said nothing at all.

🔴 THE DEFAULT INSTANCE IS THE EXISTING CONFIG FILE, UNMOVED. `~/.config/
subsystem-store/env` keeps its name, its contents and its environment-variable
precedence, and becomes the alias `personal`. Additional instances are ADDITIVE
files under `instances/<alias>.env` beside it. Nothing migrates; an upgrade
that adds no file changes nothing.

🔴 THE ENVIRONMENT OVERRIDES THE DEFAULT INSTANCE ONLY. `SUBSYSTEM_STORE_URL` /
`SUBSYSTEM_STORE_TOKEN` have always pointed the client at a throwaway server,
and every test relies on it. Applying them to EVERY instance would point all of
them at one store — a fan-out that reads N instances and measures one, reporting
agreement it never observed. A non-default alias reads its own file and nothing
else.
"""

from __future__ import annotations

import json
import os
from dataclasses import dataclass
from pathlib import Path
from typing import Mapping

from subsystem_read_store import DEFAULT_ALIAS, valid_alias

__all__ = [
    "CONFIG_ENV",
    "ROUTES_ENV",
    "INSTANCE_DIR_NAME",
    "INSTANCE_SUFFIX",
    "ROUTES_FILE_NAME",
    "Instance",
    "Routing",
    "RoutingConfigError",
    "UnroutedScope",
    "config_path",
    "discover",
    "instance_dir",
    "load_routes",
    "routes_file",
    "routing_for",
]

#: The environment variable naming the DEFAULT instance's config file. It
#: predates instances and keeps its meaning exactly.
CONFIG_ENV = "SUBSYSTEM_STORE_CONFIG"

#: The environment variable naming the routing table. Set it and the table is
#: MANDATORY — a missing file is an error, never "routing is off". An operator
#: who named a table meant to use one, and silently ignoring the name is how a
#: typo'd path turns a fail-loud design into a fail-open one.
ROUTES_ENV = "CAIRN_ROUTES"

#: Additional instances live beside the default config file, one file each.
INSTANCE_DIR_NAME = "instances"
INSTANCE_SUFFIX = ".env"

#: The table's default name, beside the config file. Absent => no routing.
ROUTES_FILE_NAME = "routes.json"


class RoutingConfigError(Exception):
    """The instance configuration or the routing table cannot be read.

    Separate from `UnroutedScope` because the remedies differ: this one means a
    FILE is wrong (malformed JSON, an alias that cannot be a directory name, two
    sources claiming one alias), and no scope is involved.
    """


class UnroutedScope(Exception):
    """This scope cannot be resolved to an instance, and nothing was guessed.

    🔴 THE SCOPE IS AN ATTRIBUTE, NOT ONLY A SUBSTRING OF THE MESSAGE. A caller
    that has to grep an f-string to learn which scope failed has pinned prose;
    the structural fact is carried so a consumer can act on it and a test can
    assert it without pinning a sentence.
    """

    def __init__(self, scope: str, detail: str) -> None:
        super().__init__(detail)
        self.scope = scope
        self.detail = detail


@dataclass(frozen=True)
class Instance:
    """One configured cairn: its alias and the file its URL and token come from.

    🔴 NO URL AND NO TOKEN HERE. This module enumerates and routes; it never
    reads a credential. Keeping the secret out of the object that gets printed,
    logged and rendered is what stops it being printed, logged and rendered.
    """

    alias: str
    config_path: Path

    @property
    def is_default(self) -> bool:
        return self.alias == DEFAULT_ALIAS


def config_path(env: Mapping[str, str] | None = None) -> Path:
    """The DEFAULT instance's config file — `$SUBSYSTEM_STORE_CONFIG` or the
    long-standing `~/.config/subsystem-store/env`."""
    env = os.environ if env is None else env
    raw = env.get(CONFIG_ENV, "").strip()
    if raw:
        return Path(raw).expanduser()
    return Path.home() / ".config" / "subsystem-store" / "env"


def instance_dir(env: Mapping[str, str] | None = None) -> Path:
    """Where additional instances live: `instances/` beside the config file.

    🔴 DERIVED FROM THE CONFIG PATH, NOT A SECOND ENVIRONMENT VARIABLE. One
    variable moves the whole configuration — which is what a test needs, and
    what keeps `$SUBSYSTEM_STORE_CONFIG` pointing somewhere while the instances
    it should sit beside are read from the operator's real home directory.
    """
    return config_path(env).parent / INSTANCE_DIR_NAME


def routes_file(env: Mapping[str, str] | None = None) -> tuple[Path, bool]:
    """`(path, explicitly_configured)` for the routing table.

    The second element is the difference between "the operator named this file"
    and "this is where a table would live if there were one". An explicit path
    that does not exist is an ERROR; a default path that does not exist means
    there is no routing table, which is the ordinary single-instance state.
    """
    env = os.environ if env is None else env
    raw = env.get(ROUTES_ENV, "").strip()
    if raw:
        return Path(raw).expanduser(), True
    return config_path(env).parent / ROUTES_FILE_NAME, False


def load_routes(path: Path) -> dict[str, str]:
    """The scope->alias table, as a flat JSON object of strings.

    🔴 ONE SHAPE, AND EVERYTHING ELSE IS REFUSED. A table is read by a machine
    to decide where a durable write lands, so "we accepted something odd and did
    our best with it" is the wrong failure mode: a nested object, a list, a
    non-string value or a non-object document each raise rather than silently
    contributing zero routes. A table that parsed to nothing would leave every
    scope unrouted, which LOOKS like a deliberate refusal and is not.
    """
    try:
        raw = path.read_text(encoding="utf-8")
    except OSError as exc:
        raise RoutingConfigError(f"the routing table `{path}` could not be read: {exc}") from exc
    try:
        parsed = json.loads(raw)
    except ValueError as exc:
        raise RoutingConfigError(f"the routing table `{path}` is not valid JSON: {exc}") from exc
    if not isinstance(parsed, dict):
        raise RoutingConfigError(
            f"the routing table `{path}` must be a JSON object mapping each scope "
            f"to an instance alias, e.g. {{\"alpha-notes\": \"personal\"}} — got "
            f"{type(parsed).__name__}"
        )
    table: dict[str, str] = {}
    for scope, alias in parsed.items():
        if not isinstance(alias, str) or not scope:
            raise RoutingConfigError(
                f"the routing table `{path}` maps scope `{scope}` to "
                f"{alias!r}, which is not an instance alias. Every value must be "
                f"the alias of a configured instance."
            )
        if not valid_alias(alias):
            raise RoutingConfigError(
                f"the routing table `{path}` maps scope `{scope}` to the alias "
                f"`{alias}`, which is not a usable alias. An alias becomes a "
                f"directory name under the cache root, so it is lowercase "
                f"letters, digits and hyphens only."
            )
        table[scope] = alias
    return table


@dataclass(frozen=True)
class Routing:
    """What is configured on this host, and the routing decision it supports."""

    instances: tuple[Instance, ...]
    routes: Mapping[str, str] | None
    routes_source: Path | None

    @property
    def aliases(self) -> tuple[str, ...]:
        return tuple(i.alias for i in self.instances)

    @property
    def multi_instance(self) -> bool:
        """Is there more than one PLACE on this host an answer could come from?

        🔴 THE LABELLING PREDICATE, AND IT IS DELIBERATELY NOT THE ROUTING ONE.
        Every place the client says WHICH instance it read — the banner label,
        `doctor`'s per-instance rows, `ls-entries`' prefix, the `--all-scopes`
        fan-out, the caveat's extra clause — asks THIS, so they cannot drift
        into disagreeing about whether this host has more than one store.

        🔴 IT ASKS THE INSTANCE COUNT AND NOT THE TABLE, AND THAT IS THE FIX.
        The predicate used to be `routes is not None or len(instances) > 1`, so
        writing a routing table on a ONE-instance host switched every label on:
        each recall then asserted "with more than one instance configured, an
        absence here is also explainable by the scope living on another
        instance" — a sentence that is FALSE on that host, printed on every
        call, which is precisely the always-false caveat the caveat rewrite
        exists to eliminate. A table is a statement about where scopes live,
        not about how many stores this machine can reach.

        ⚠ IT IS NOT THE ROUTING DECISION EITHER — see `alias_for`, which
        consults the table whatever this returns. A one-instance host with a
        table still REFUSES a scope routed to an alias it has no config for.
        """
        return len(self.instances) > 1

    def get(self, alias: str) -> Instance:
        for instance in self.instances:
            if instance.alias == alias:
                return instance
        raise RoutingConfigError(
            f"no instance `{alias}` is configured on this host "
            f"(configured: {', '.join(self.aliases)})"
        )

    def alias_for(self, scope: str) -> str:
        """The instance `scope` belongs to, or raise. NEVER a guess.

        🔴 THE TABLE IS CONSULTED FIRST, WHATEVER `multi_instance` SAYS, AND THE
        ORDER IS THE WHOLE POINT. Three cases share one configuration — a
        one-instance host with a table — and two of them must answer
        DIFFERENTLY, so a single "is this host routing" boolean cannot decide
        them. Measured, on a host with ONE instance configured:

            table state for this scope   | today  | `active = len(instances)>1`
            -----------------------------|--------|----------------------------
            (1) no table at all          | sole   | sole
            (2) table, scope ABSENT      | sole   | sole
            (3) table -> UNCONFIGURED    | REFUSE | sole  <- SILENT MISROUTE
                alias                    |        |

        Row 3 is a table explicitly routing a scope to an alias this host has no
        config for. Resolving it to the sole instance is a write landing in a
        store nobody decided on — the exact failure the refusal exists to
        prevent — so it refuses at ONE instance and at many. Rows 1 and 2 are
        the opposite: the table said nothing about this scope, and with one
        instance there is exactly one answer to "where did that go?", which is
        the same reasoning the no-table case has always used.

        The refusals, each reachable by an input no earlier one rejects:
          1. a table naming an alias this host has no config for  (any count)
          2. two or more instances and no table -> nothing can decide
          3. two or more instances and a table without `scope`

        🔴 THE COMPARISON IS EXACT. The table's keys are scope names exactly as
        `entry_shape.derive_scope` produces them. A case or spelling mismatch
        therefore falls through to the rows above rather than resolving to
        something adjacent, which is the safe direction for a value that decides
        where a write lands.
        """
        alias = None if self.routes is None else self.routes.get(scope)
        if alias is not None:
            if alias not in self.aliases:
                raise UnroutedScope(
                    scope,
                    f"the routing table `{self.routes_source}` routes scope "
                    f"`{scope}` to instance `{alias}`, which is not configured on "
                    f"this host (configured: {', '.join(self.aliases)}). REFUSING "
                    f"rather than falling back: the table names where this scope "
                    f"lives, and this host cannot reach it. Add "
                    f"`{instance_dir()}/{alias}{INSTANCE_SUFFIX}`.",
                )
            return alias
        # The table decided NOTHING about this scope — it has no entry, or there
        # is no table. With one instance that has exactly one answer.
        if not self.multi_instance:
            return DEFAULT_ALIAS
        if self.routes is None:
            raise UnroutedScope(
                scope,
                f"{len(self.instances)} instances are configured "
                f"({', '.join(self.aliases)}) and no routing table says which one "
                f"scope `{scope}` belongs to. REFUSING rather than picking one: a "
                f"write to the wrong instance lands in a store nobody reads. "
                f"Write a scope->alias table to "
                f"`{routes_file()[0]}`, or set ${ROUTES_ENV} to one.",
            )
        raise UnroutedScope(
            scope,
            f"scope `{scope}` is not in the routing table "
            f"`{self.routes_source}`. REFUSING rather than defaulting to an "
            f"instance: an unregistered scope is a scope nobody decided about, "
            f"and guessing is how a write lands in a store nobody reads. Add "
            f"`\"{scope}\": \"<alias>\"` to that table (configured instances: "
            f"{', '.join(self.aliases)}).",
        )

    def check(self, scopes: Mapping[str, str] | set[str] | tuple[str, ...] | list[str]
              ) -> tuple[str, ...]:
        """Grade this host's routing table against reality, IN BOTH DIRECTIONS.

        🔴 BOTH DIRECTIONS, BECAUSE EACH MISSES A DIFFERENT DEFECT. A scope with
        no entry is a scope whose next write REFUSES — annoying but loud. An
        entry naming a scope that does not exist is the silent one: it reads as
        coverage, survives the scope being renamed or retired, and is exactly
        what a table accumulates when it is edited by hand. A one-way check
        would report a clean table over either.

        A third direction is graded here too because it is the same class: an
        entry naming an alias no instance provides. 🔴 IT IS NOT RE-DERIVED — it
        ASKS `alias_for`, which already owns that refusal. This method used to
        recompute `alias not in aliases` from the same two inputs, so the rule
        had two implementations and the second one could have drifted without
        any test noticing; one rule, one place.

        Returns the problems, one string each, in a stable order. An empty tuple
        is the only clean result — callers must not read a truthy/falsy bare
        return, which is why this returns strings rather than a bool.
        """
        if self.routes is None:
            raise RoutingConfigError(
                "there is no routing table on this host to grade. A caller that "
                "reached here read an absent table as an empty one, which would "
                "report every scope as unrouted."
            )
        scope_set = set(scopes)
        problems: list[str] = []
        # 🔴 DIRECTION ONE ASKS THE RESOLVER, IT DOES NOT SUBTRACT KEY SETS. The
        # finding this raises is "a write to it will REFUSE", which is a claim
        # about what `alias_for` does — and on a ONE-instance host it does not
        # refuse: an unnamed scope resolves to the sole instance. Grading by the
        # table's keys alone printed that refusal warning on every unnamed scope
        # of every one-instance host, which is a confident statement about
        # behaviour that will not happen.
        for scope in sorted(scope_set - set(self.routes)):
            try:
                self.alias_for(scope)
            except UnroutedScope:
                problems.append(
                    f"scope `{scope}` exists but the routing table does not name "
                    f"it — a write to it will REFUSE"
                )
        # ⚠ DIRECTION TWO IS NOT A RESOLVER QUESTION, DELIBERATELY. `alias_for`
        # resolves a stale entry perfectly well — it names a configured alias —
        # so asking it here would grade this direction clean. The defect is that
        # the scope does not EXIST, which only the scope set can see.
        for scope in sorted(set(self.routes) - scope_set):
            problems.append(
                f"the routing table names scope `{scope}`, which exists on no "
                f"configured instance — a stale entry reads as coverage"
            )
        for scope in sorted(self.routes):
            try:
                self.alias_for(scope)
            except UnroutedScope:
                # 🔴 THE ONLY REFUSAL `alias_for` CAN RAISE FOR A SCOPE THE TABLE
                # NAMES is the unconfigured-alias one: the other two arms are
                # reached only when the table said nothing about the scope, and
                # this loop iterates the table's own keys. The short line here is
                # the FINDING's wording; the PREDICATE is `alias_for`'s.
                problems.append(
                    f"the routing table routes `{scope}` to instance "
                    f"`{self.routes[scope]}`, which is not configured on this host"
                )
        return tuple(problems)


def discover(env: Mapping[str, str] | None = None) -> Routing:
    """Everything configured on this host: instances first, then the table.

    🔴 THE DEFAULT INSTANCE ALWAYS EXISTS, FILE OR NO FILE. `load_config` has
    always accepted a URL and token from the environment with no file at all,
    and every test in this repository does exactly that. Making the default
    instance conditional on a file would have deleted that path — the client
    would have reported "no instances configured" on a host that is perfectly
    well configured, which is a statement about a file rather than about the
    store.
    """
    env = os.environ if env is None else env
    instances = [Instance(alias=DEFAULT_ALIAS, config_path=config_path(env))]

    directory = instance_dir(env)
    extra: list[Instance] = []
    try:
        entries = sorted(directory.iterdir())
    except FileNotFoundError:
        entries = []
    except OSError as exc:
        raise RoutingConfigError(
            f"the instance directory `{directory}` could not be read: {exc}. It is "
            f"not the same fact as 'no extra instances are configured', so this "
            f"refuses rather than reporting one instance."
        ) from exc
    for path in entries:
        if not path.name.endswith(INSTANCE_SUFFIX) or path.is_dir():
            continue
        alias = path.name[: -len(INSTANCE_SUFFIX)]
        # 🔴 A FILE THAT CANNOT BE AN INSTANCE IS AN ERROR, NOT A SKIP. Skipping
        # it would leave the operator with a file they wrote, a store they think
        # is configured, and no message anywhere — and the scopes they routed to
        # it would refuse later, naming the ALIAS, which points at the table
        # rather than at the file that was ignored.
        if not valid_alias(alias) or alias == DEFAULT_ALIAS:
            raise RoutingConfigError(
                f"`{path}` does not name a usable instance alias. An alias becomes "
                f"a directory name under the cache root, so it is lowercase "
                f"letters, digits and hyphens only, and `{DEFAULT_ALIAS}` is "
                f"reserved for the default instance's own config file — one alias "
                f"with two config sources is a store nobody can point at."
            )
        extra.append(Instance(alias=alias, config_path=path))

    instances.extend(extra)

    path, explicit = routes_file(env)
    routes: dict[str, str] | None = None
    source: Path | None = None
    if path.exists():
        routes = load_routes(path)
        source = path
    elif explicit:
        raise RoutingConfigError(
            f"${ROUTES_ENV} names `{path}`, which does not exist. A configured "
            f"table that cannot be read is an error, not an absence of routing — "
            f"treating it as 'no table' would silently turn a fail-loud "
            f"configuration into a fail-open one."
        )
    return Routing(instances=tuple(instances), routes=routes, routes_source=source)


def routing_for(
    routes: Mapping[str, str],
    aliases: tuple[str, ...] | set[str] | list[str],
) -> Routing:
    """A `Routing` over an alias list and a table, with no filesystem involved.

    🔴 IT EXISTS SO THE GRADER AND THE RESOLVER CANNOT BE TESTED SEPARATELY.
    `Routing.check` asks `Routing.alias_for` for the unconfigured-alias
    direction, so a test that wanted to grade a table without discovering a host
    would otherwise have had to re-implement one of them. The config paths are
    the placeholders they are because nothing on either path reads them: an
    `Instance` carries an alias and the file its credentials come from, and
    routing never opens that file.
    """
    return Routing(
        instances=tuple(Instance(alias=a, config_path=Path(os.devnull))
                        for a in sorted(set(aliases))),
        routes=dict(routes),
        routes_source=None,
    )
