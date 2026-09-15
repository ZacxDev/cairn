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
so `alias_for` answers with it and NOTHING about the single-instance client
changes — no label, no table, no refusal. Routing turns on when a second
instance appears OR when a routing table is configured, which are exactly the
two states in which "where did that go?" has more than one answer. The
alternative — refusing until a table exists — would break every existing
deployment on upgrade in exchange for a guarantee nobody needs while there is
one store.

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
import re
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
    "check_routes",
    "config_path",
    "discover",
    "instance_dir",
    "load_routes",
    "routes_file",
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
    def active(self) -> bool:
        """Is there more than one answer to "where did that go?".

        🔴 THE ONE PREDICATE. Every instance-aware behaviour in the client —
        labelling a banner, labelling a write, per-instance `doctor` sections,
        the search fan-out, the caveat's extra clause — asks THIS, so they
        cannot drift into disagreeing about whether this host is routing.
        """
        return self.routes is not None or len(self.instances) > 1

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

        Three refusals, each reachable by an input no earlier one rejects:
          1. instances but no table  -> nothing can decide
          2. a table without `scope` -> the table decided nothing about it
          3. a table naming an alias this host has no config for

        🔴 THE COMPARISON IS EXACT. The table's keys are scope names exactly as
        `entry_shape.derive_scope` produces them. A case or spelling mismatch
        therefore REFUSES rather than resolving to something adjacent, which is
        the safe direction for a value that decides where a write lands.
        """
        if not self.active:
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
        alias = self.routes.get(scope)
        if alias is None:
            raise UnroutedScope(
                scope,
                f"scope `{scope}` is not in the routing table "
                f"`{self.routes_source}`. REFUSING rather than defaulting to an "
                f"instance: an unregistered scope is a scope nobody decided about, "
                f"and guessing is how a write lands in a store nobody reads. Add "
                f"`\"{scope}\": \"<alias>\"` to that table (configured instances: "
                f"{', '.join(self.aliases)}).",
            )
        if alias not in self.aliases:
            raise UnroutedScope(
                scope,
                f"the routing table `{self.routes_source}` routes scope `{scope}` "
                f"to instance `{alias}`, which is not configured on this host "
                f"(configured: {', '.join(self.aliases)}). REFUSING rather than "
                f"falling back: the table names where this scope lives, and this "
                f"host cannot reach it. Add `{instance_dir()}/{alias}"
                f"{INSTANCE_SUFFIX}`.",
            )
        return alias


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


def check_routes(
    routes: Mapping[str, str],
    *,
    scopes: Mapping[str, str] | set[str] | tuple[str, ...] | list[str],
    aliases: tuple[str, ...] | set[str] | list[str],
) -> tuple[str, ...]:
    """Grade a routing table against reality, IN BOTH DIRECTIONS.

    🔴 BOTH DIRECTIONS, BECAUSE EACH MISSES A DIFFERENT DEFECT. A scope with no
    entry is a scope whose next write REFUSES — annoying but loud. An entry
    naming a scope that does not exist is the silent one: it reads as coverage,
    survives the scope being renamed or retired, and is exactly what a table
    accumulates when it is edited by hand. A one-way check would report a clean
    table over either.

    A third direction is graded here too because it is the same class: an entry
    naming an alias no instance provides. That one refuses at USE time with a
    message about the table; catching it here turns a future refusal into a
    present, fixable finding.

    Returns the problems, one string each, in a stable order. An empty tuple is
    the only clean result — callers must not read a truthy/falsy bare return,
    which is why this returns strings rather than a bool.
    """
    scope_set = set(scopes)
    alias_set = set(aliases)
    problems: list[str] = []
    for scope in sorted(scope_set - set(routes)):
        problems.append(
            f"scope `{scope}` exists but the routing table does not name it — "
            f"a write to it will REFUSE"
        )
    for scope in sorted(set(routes) - scope_set):
        problems.append(
            f"the routing table names scope `{scope}`, which exists on no "
            f"configured instance — a stale entry reads as coverage"
        )
    for scope in sorted(routes):
        alias = routes[scope]
        if alias not in alias_set:
            problems.append(
                f"the routing table routes `{scope}` to instance `{alias}`, "
                f"which is not configured on this host"
            )
    return tuple(problems)
