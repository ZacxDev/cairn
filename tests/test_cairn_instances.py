#!/usr/bin/env python3
"""MULTI-INSTANCE ROUTING — where does a scope live, and what happens when nobody said?

🔴 WHAT THIS FILE IS GUARDING, IN ONE SENTENCE: that a client pointed at more
than one store never GUESSES which one a scope belongs to, and that a client
pointed at exactly one behaves as if none of this code existed.

Those two are the whole design. The first is the safety property — a write that
lands in a store nobody reads is discovered days later, by accident, if at all,
so an unregistered scope REFUSES and names itself. The second is what makes the
first shippable: the routing machinery is inert until an operator configures a
second instance, which is why it can go in before any data moves.

Every CLI test here runs the real client as a SUBPROCESS against a real server.
That is deliberate and it is what makes the red-at-baseline claims meaningful: a
unit test of a module that does not exist yet is red for an uninteresting
reason, while `cairn recall` exiting 0 and silently reading the wrong store is
the actual defect.
"""

from __future__ import annotations

import http.server
import importlib.machinery
import importlib.util
import ipaddress
import json
import os
import subprocess
import sys
import threading
from pathlib import Path

import pytest

REPO = Path(__file__).resolve().parents[1]
CAIRN_CLI = REPO / "cairn"
SERVER_PY = REPO / "server" / "server.py"

sys.path.insert(0, str(REPO / "lib"))
import cairn_instances as ci  # noqa: E402
import subsystem_read_store as rs  # noqa: E402

TOKEN_DEFAULT = "d" * 20 + "K" * 20 + "p" * 8
TOKEN_SECOND = "s" * 20 + "K" * 20 + "p" * 8
#: 🔴 NOT loopback. A TRUSTED peer must present `CF-Connecting-IP` and is refused
#: 401 without it, so trusting the address the client actually connects from
#: would make every request in this file fail for a reason that has nothing to do
#: with routing. TEST-NET-1, which nothing will ever connect from.
UNREACHED_PROXY = ipaddress.ip_network("192.0.2.1/32")
DEFAULT_SCOPE = "widget-cfg"
SECOND_SCOPE = "gizmo-notes"
SECOND_ALIAS = "secondary"


def _load_api():
    """Import `server.py` by path. `sys.modules[...]` BEFORE `exec_module` — see
    `test_cairn_cli._load_api` for why the first `@dataclass` raises without it."""
    spec = importlib.util.spec_from_file_location("srv_inst", SERVER_PY)
    assert spec and spec.loader
    module = importlib.util.module_from_spec(spec)
    sys.modules[spec.name] = module
    spec.loader.exec_module(module)
    return module


def _entry(service: str, scope: str) -> str:
    return "\n".join([
        "---",
        f"service: {service}",
        f"scope: {scope}",
        "sensitivity: internal",
        "---",
        "",
        "## What it is",
        f"The {service} component.",
        "",
        "## Pointers",
        f"- `{scope}: src/{service}.py`",
        "",
        "## Nuance / work-history",
        "- 2000-01-02: the probe lies for 40s after a restart.",
        "",
    ])


class _Store:
    """One running store: its root, its URL and its token."""

    def __init__(self, root: Path, token: str, scope: str) -> None:
        api = _load_api()
        self.root = root
        self.token = token
        # 🔴 A `TokenRecord`, NOT A BARE STRING. A bare token is the LEGACY row:
        # it has no identity, so the server refuses every WRITE through it
        # (`legacy-cannot-write`) — and this file has to measure where a write
        # LANDS, which a refusal cannot.
        self.httpd = api.build_server(
            host="127.0.0.1", port=0, store_root=str(root),
            tokens=(api.TokenRecord(token=token, identity="tester", scopes=(scope,)),),
            trusted_proxies=(UNREACHED_PROXY,), limiter=None, audit=None,
        )
        self.url = f"http://127.0.0.1:{self.httpd.server_address[1]}"
        threading.Thread(target=self.httpd.serve_forever, daemon=True).start()

    def stop(self) -> None:
        self.httpd.shutdown()
        self.httpd.server_close()


@pytest.fixture
def world(tmp_path: Path):
    """A HOME with one configured instance, and a SECOND store not yet configured.

    The second store exists from the start so a test can configure it in one
    line — which is the point: adding an instance must be additive, and a test
    that had to rebuild the world to add one would not be measuring that.
    """
    home = tmp_path / "home"
    (home / ".config" / "subsystem-store").mkdir(parents=True)

    first_root = tmp_path / "store-default"
    (first_root / DEFAULT_SCOPE).mkdir(parents=True)
    (first_root / DEFAULT_SCOPE / "thing-alpha.md").write_text(
        _entry("thing-alpha", DEFAULT_SCOPE)
    )
    second_root = tmp_path / "store-second"
    (second_root / SECOND_SCOPE).mkdir(parents=True)
    (second_root / SECOND_SCOPE / "other-thing.md").write_text(
        _entry("other-thing", SECOND_SCOPE)
    )

    first = _Store(first_root, TOKEN_DEFAULT, DEFAULT_SCOPE)
    second = _Store(second_root, TOKEN_SECOND, SECOND_SCOPE)
    cfg = home / ".config" / "subsystem-store" / "env"
    cfg.write_text(
        f"SUBSYSTEM_STORE_URL={first.url}\nSUBSYSTEM_STORE_TOKEN={first.token}\n"
    )
    cfg.chmod(0o600)

    class World:
        def __init__(self) -> None:
            self.home = home
            self.first = first
            self.second = second

        def add_instance(self, alias: str = SECOND_ALIAS, store: _Store | None = None):
            store = store or self.second
            directory = home / ".config" / "subsystem-store" / "instances"
            directory.mkdir(exist_ok=True)
            path = directory / f"{alias}.env"
            path.write_text(
                f"SUBSYSTEM_STORE_URL={store.url}\n"
                f"SUBSYSTEM_STORE_TOKEN={store.token}\n"
            )
            path.chmod(0o600)
            return path

        def write_routes(self, table: dict) -> Path:
            path = home / ".config" / "subsystem-store" / "routes.json"
            path.write_text(json.dumps(table))
            return path

        def run(self, *args: str, **env_extra) -> subprocess.CompletedProcess:
            env = dict(os.environ)
            env["HOME"] = str(home)
            # 🔴 EVERY INHERITED POINTER IS CLEARED. A developer's own
            # `$SUBSYSTEM_STORE_URL` would otherwise override the DEFAULT
            # instance — the one path where the environment wins — and a test
            # that reads the operator's live store is not a test.
            for key in ("SUBSYSTEM_STORE_URL", "SUBSYSTEM_STORE_TOKEN",
                        "SUBSYSTEM_STORE_CONFIG", "CAIRN_ROUTES",
                        "CAIRN_MIRROR_ROOT"):
                env.pop(key, None)
            env.update({k: str(v) for k, v in env_extra.items()})
            return subprocess.run(
                [sys.executable, str(CAIRN_CLI), "--timeout", "5", *args],
                capture_output=True, text=True, env=env, timeout=120,
            )

    try:
        yield World()
    finally:
        first.stop()
        second.stop()


# =============================================================================
# THE REFUSAL. (c) of the closing conditions.
# =============================================================================


class TestAnUnregisteredScopeRefuses:
    """🔴 RED AT BASELINE BY BEHAVIOUR, NOT BY IMPORT ERROR. Before this change
    the client had one store, so every one of these invocations read it and
    exited 0 — which is the defect: a scope nobody routed was served, silently,
    from whichever store happened to be configured."""

    def test_recall_of_an_unregistered_scope_EXITS_NONZERO_NAMING_THE_SCOPE(self, world):
        world.add_instance()
        world.write_routes({DEFAULT_SCOPE: rs.DEFAULT_ALIAS})
        proc = world.run("recall", "--scope", "never-registered")
        assert proc.returncode == 11, (proc.returncode, proc.stdout, proc.stderr)
        assert "never-registered" in proc.stderr, proc.stderr
        # 🔴 AND NOTHING WAS SERVED. A refusal that still printed a digest would
        # be a refusal a caller reads straight past.
        assert "subsystem-recall:" not in proc.stdout, proc.stdout

    def test_an_APPEND_to_an_unregistered_scope_writes_NOTHING(self, world):
        world.add_instance()
        world.write_routes({DEFAULT_SCOPE: rs.DEFAULT_ALIAS})
        proc = world.run("append", "--scope", "never-registered", "--ref", "x",
                         "--text", "a bullet", "--session", "s-1")
        assert proc.returncode == 11, (proc.returncode, proc.stdout, proc.stderr)
        assert "never-registered" in proc.stderr
        # The write codes mean "the store answered" or "the store was not
        # reached". Neither is true here: no store was ever chosen.
        assert proc.returncode not in (6, 7, 8, 9)

    def test_two_instances_and_NO_table_refuses_rather_than_picking_one(self, world):
        world.add_instance()
        proc = world.run("recall", "--scope", DEFAULT_SCOPE)
        assert proc.returncode == 11, (proc.returncode, proc.stdout, proc.stderr)
        assert DEFAULT_SCOPE in proc.stderr
        assert "routing table" in proc.stderr

    def test_a_route_to_an_UNCONFIGURED_alias_refuses(self, world):
        world.add_instance()
        world.write_routes({DEFAULT_SCOPE: "no-such-instance"})
        proc = world.run("recall", "--scope", DEFAULT_SCOPE)
        assert proc.returncode == 11, (proc.returncode, proc.stdout, proc.stderr)
        assert "no-such-instance" in proc.stderr

    def test_a_ROUTED_scope_is_read_from_the_instance_the_table_names(self, world):
        """The positive control for every refusal above: with the table right,
        the read reaches the OTHER store — which holds an entry the default one
        does not, so the wrong instance cannot produce this output."""
        world.add_instance()
        world.write_routes({
            DEFAULT_SCOPE: rs.DEFAULT_ALIAS,
            SECOND_SCOPE: SECOND_ALIAS,
        })
        proc = world.run("recall", "--scope", SECOND_SCOPE)
        assert proc.returncode == 0, (proc.returncode, proc.stdout, proc.stderr)
        assert "other-thing" in proc.stdout, proc.stdout
        assert f"cairn[{SECOND_ALIAS}]:" in proc.stdout, proc.stdout


# =============================================================================
# ONE INSTANCE IS NOT ROUTING. (d) of the closing conditions.
# =============================================================================


class TestOneInstanceBehavesAsBefore:
    def test_no_table_and_one_instance_reads_the_default_store(self, world):
        proc = world.run("recall", "--scope", DEFAULT_SCOPE)
        assert proc.returncode == 0, (proc.returncode, proc.stdout, proc.stderr)
        assert "thing-alpha" in proc.stdout

    def test_the_banner_carries_NO_instance_label(self, world):
        proc = world.run("recall", "--scope", DEFAULT_SCOPE)
        assert "cairn: live" in proc.stdout, proc.stdout
        assert "cairn[" not in proc.stdout, proc.stdout

    def test_the_caveat_does_NOT_mention_another_instance(self, world):
        proc = world.run("recall", "--scope", DEFAULT_SCOPE)
        assert "PER-HOST CACHE" in proc.stdout
        assert "another instance" not in proc.stdout, proc.stdout

    def test_doctor_reports_ONE_instance_with_UNPREFIXED_check_names(self, world):
        proc = world.run("doctor", "--json")
        payload = json.loads(proc.stdout)
        names = [c["name"] for c in payload["checks"]]
        assert "pod" in names, names
        assert not any("/" in n for n in names), names

    def test_doctor_reports_EVERY_instance_once_a_second_is_configured(self, world):
        world.add_instance()
        proc = world.run("doctor", "--json")
        payload = json.loads(proc.stdout)
        names = {c["name"] for c in payload["checks"]}
        assert f"{rs.DEFAULT_ALIAS}/pod" in names, names
        assert f"{SECOND_ALIAS}/pod" in names, names

    def test_a_HEALTHY_instance_cannot_mask_a_BROKEN_one(self, world):
        """🔴 THE PROPERTY, NOT THE LAYOUT. Two instances, one of them pointed at
        a port nothing serves: the run must be non-zero and must say so ABOUT
        THAT INSTANCE. A merged verdict would report the healthy one."""
        path = world.add_instance()
        path.write_text(
            "SUBSYSTEM_STORE_URL=http://127.0.0.1:1\nSUBSYSTEM_STORE_TOKEN=x\n"
        )
        proc = world.run("doctor", "--json")
        payload = json.loads(proc.stdout)
        states = {c["name"]: c["state"] for c in payload["checks"]}
        assert states[f"{rs.DEFAULT_ALIAS}/pod"] == "OK", states
        assert states[f"{SECOND_ALIAS}/pod"] in ("PROBLEM", "UNMEASURED"), states
        assert payload["exit"] != 0, payload["exit"]


# =============================================================================
# THE CAVEAT. (f) of the closing conditions.
# =============================================================================


class TestTheCaveatStopsBeingALie:
    def test_a_routed_recall_says_an_absence_may_be_on_ANOTHER_instance(self, world):
        world.add_instance()
        world.write_routes({DEFAULT_SCOPE: rs.DEFAULT_ALIAS, SECOND_SCOPE: SECOND_ALIAS})
        proc = world.run("recall", "--scope", DEFAULT_SCOPE)
        assert proc.returncode == 0, proc.stderr
        assert "another instance" in proc.stdout, proc.stdout
        assert f"`{rs.DEFAULT_ALIAS}` instance ONLY" in proc.stdout, proc.stdout

    def test_the_single_instance_sentence_is_UNCHANGED(self):
        """🔴 PINNED AS A WHOLE NORMALISED STRING, NOT BY KEYWORD. This sentence
        is byte-mirrored into the Go port and into two generated corpora; a
        reword here is a four-place change, and a keyword check would not see
        one."""
        import entry_shape

        assert entry_shape.store_caveat() == (
            "the store is read through a PER-HOST CACHE, only as fresh as its "
            "last sync; this run read THIS machine's disk and consulted no other"
        )

    def test_the_multi_instance_sentence_EXTENDS_rather_than_replaces(self):
        import entry_shape

        extended = entry_shape.store_caveat("work-notes")
        assert extended.startswith(entry_shape.store_caveat())
        assert "work-notes" in extended
        assert "NOT an absence from the fleet" in extended


# =============================================================================
# THE TWO-WAY REGISTRY PIN. (b) of the closing conditions.
# =============================================================================


class TestTheTableIsGradedBothWays:
    """🔴 BOTH DIRECTIONS, BECAUSE EACH MISSES A DIFFERENT DEFECT. A scope with
    no entry refuses its next write — loud. An entry naming a scope that exists
    nowhere is silent: it reads as coverage and survives a rename."""

    ALIASES = (rs.DEFAULT_ALIAS, SECOND_ALIAS)

    def test_a_scope_with_NO_entry_is_a_problem(self):
        problems = ci.check_routes(
            {DEFAULT_SCOPE: rs.DEFAULT_ALIAS},
            scopes={DEFAULT_SCOPE, SECOND_SCOPE},
            aliases=self.ALIASES,
        )
        assert len(problems) == 1, problems
        assert SECOND_SCOPE in problems[0]
        assert "REFUSE" in problems[0]

    def test_an_entry_naming_NO_scope_is_a_problem(self):
        problems = ci.check_routes(
            {DEFAULT_SCOPE: rs.DEFAULT_ALIAS, "retired-scope": SECOND_ALIAS},
            scopes={DEFAULT_SCOPE},
            aliases=self.ALIASES,
        )
        assert len(problems) == 1, problems
        assert "retired-scope" in problems[0]

    def test_an_entry_naming_an_UNCONFIGURED_alias_is_a_problem(self):
        problems = ci.check_routes(
            {DEFAULT_SCOPE: "ghost"}, scopes={DEFAULT_SCOPE}, aliases=self.ALIASES
        )
        assert len(problems) == 1, problems
        assert "ghost" in problems[0]

    def test_a_table_that_agrees_with_reality_has_NO_problems(self):
        """The control. Without it every assertion above is satisfiable by a
        function that returns a problem for everything."""
        assert ci.check_routes(
            {DEFAULT_SCOPE: rs.DEFAULT_ALIAS, SECOND_SCOPE: SECOND_ALIAS},
            scopes={DEFAULT_SCOPE, SECOND_SCOPE},
            aliases=self.ALIASES,
        ) == ()

    def test_the_CLI_grades_the_table_and_exits_NONZERO_on_a_stale_entry(self, world):
        world.add_instance()
        world.write_routes({
            DEFAULT_SCOPE: rs.DEFAULT_ALIAS,
            SECOND_SCOPE: SECOND_ALIAS,
            "retired-scope": SECOND_ALIAS,
        })
        proc = world.run("routes", "--check")
        assert proc.returncode == 11, (proc.returncode, proc.stdout, proc.stderr)
        assert "retired-scope" in proc.stderr, proc.stderr

    def test_the_CLI_grades_the_table_and_exits_NONZERO_on_a_missing_entry(self, world):
        world.add_instance()
        world.write_routes({DEFAULT_SCOPE: rs.DEFAULT_ALIAS})
        proc = world.run("routes", "--check")
        assert proc.returncode == 11, (proc.returncode, proc.stdout, proc.stderr)
        assert SECOND_SCOPE in proc.stderr, proc.stderr

    def test_a_CORRECT_table_passes_the_CLI_check(self, world):
        world.add_instance()
        world.write_routes({DEFAULT_SCOPE: rs.DEFAULT_ALIAS, SECOND_SCOPE: SECOND_ALIAS})
        proc = world.run("routes", "--check")
        assert proc.returncode == 0, (proc.returncode, proc.stdout, proc.stderr)
        assert "0 problem(s)" in proc.stdout, proc.stdout


# =============================================================================
# THE FAN-OUT.
# =============================================================================


class TestSearchAcrossInstances:
    def test_all_scopes_searches_EVERY_instance_and_labels_the_hits(self, world):
        world.add_instance()
        world.write_routes({DEFAULT_SCOPE: rs.DEFAULT_ALIAS, SECOND_SCOPE: SECOND_ALIAS})
        proc = world.run("search", "--all-scopes", "probe")
        assert proc.returncode == 0, (proc.returncode, proc.stdout, proc.stderr)
        assert f"cairn[{rs.DEFAULT_ALIAS}]" in proc.stdout, proc.stdout
        assert f"cairn[{SECOND_ALIAS}]" in proc.stdout, proc.stdout

    def test_all_scopes_does_NOT_require_THIS_repos_scope_to_be_registered(self, world):
        """🔴 `--all-scopes` NAMES NO SCOPE. Refusing it because the current
        repo's scope is unregistered would be a refusal about a question nobody
        asked — and it is the shape a naive "route everything" implementation
        produces."""
        world.add_instance()
        world.write_routes({SECOND_SCOPE: SECOND_ALIAS})
        proc = world.run("search", "--all-scopes", "probe", "--scope", "never-registered")
        assert proc.returncode == 0, (proc.returncode, proc.stdout, proc.stderr)

    def test_an_UNREACHABLE_instance_makes_the_fan_out_LOUD_and_nonzero(self, world):
        """🔴 A SILENT PARTIAL MAKES 'found nothing' A LIE. The hits that WERE
        found are still printed — discarding real answers is its own harm — but
        the run is non-zero and names the instance it could not read."""
        path = world.add_instance()
        path.write_text(
            "SUBSYSTEM_STORE_URL=http://127.0.0.1:1\nSUBSYSTEM_STORE_TOKEN=x\n"
        )
        world.write_routes({DEFAULT_SCOPE: rs.DEFAULT_ALIAS})
        proc = world.run("search", "--all-scopes", "probe")
        assert proc.returncode != 0, (proc.returncode, proc.stdout, proc.stderr)
        assert "PARTIAL" in proc.stderr, proc.stderr
        assert SECOND_ALIAS in proc.stderr, proc.stderr


# =============================================================================
# THE CACHE.
# =============================================================================


class TestPerInstanceCaches:
    def test_the_default_instance_keeps_the_EXACT_legacy_cache_root(self):
        """🔴 THE COMPATIBILITY GUARANTEE, PINNED AGAINST A LITERAL. Every host
        has a populated cache at this path and every recall PRINTS it, so a
        formula that moved it would orphan the cache and change the bytes of
        every report."""
        assert rs.cache_root_for(rs.DEFAULT_ALIAS) == rs.DEFAULT_CACHE_ROOT
        assert rs.cache_root_for(rs.DEFAULT_ALIAS) == Path.home() / ".cache" / "subsystem-store"

    def test_another_instance_is_a_SIBLING_not_a_child_of_the_default_root(self):
        """🔴 A CHILD WOULD LOOK LIKE A SCOPE. Every reader enumerates
        `<root>/<dir>` as the scope list, so an alias directory under the default
        root would be reported as a scope that exists and holds no entries."""
        root = rs.cache_root_for("work-notes")
        assert root != rs.DEFAULT_CACHE_ROOT
        assert rs.DEFAULT_CACHE_ROOT not in root.parents
        assert root.parent == rs.DEFAULT_CACHE_ROOT.parent

    @pytest.mark.parametrize("alias", ["../escape", "/abs", "Work", ".hidden", "", "a b"])
    def test_an_alias_that_is_not_a_PATH_SEGMENT_is_refused(self, alias):
        with pytest.raises(ValueError):
            rs.cache_root_for(alias)

    def test_two_instances_do_NOT_share_one_cache_directory(self, world):
        world.add_instance()
        world.write_routes({DEFAULT_SCOPE: rs.DEFAULT_ALIAS, SECOND_SCOPE: SECOND_ALIAS})
        assert world.run("sync").returncode == 0
        default_cache = world.home / ".cache" / "subsystem-store"
        second_cache = world.home / ".cache" / f"subsystem-store-{SECOND_ALIAS}"
        assert (default_cache / DEFAULT_SCOPE).is_dir()
        assert (second_cache / SECOND_SCOPE).is_dir()
        # 🔴 EACH DATES ITSELF. One stamp for two stores would say how fresh
        # whichever synced last is, while the entries came from both.
        assert (default_cache / rs.SYNC_STAMP).is_file()
        assert (second_cache / rs.SYNC_STAMP).is_file()
        assert not (default_cache / SECOND_SCOPE).exists()

    def test_ls_entries_walks_every_instance_and_PREFIXES_each_line(self, world):
        """🔴 A `[alias] ` PREFIX, NOT A THIRD PATH SEGMENT. A consumer splits
        these lines on `/` expecting two parts; `alias/scope/entry.md` would
        silently re-point every such split at the wrong field."""
        world.add_instance()
        world.write_routes({DEFAULT_SCOPE: rs.DEFAULT_ALIAS, SECOND_SCOPE: SECOND_ALIAS})
        proc = world.run("ls-entries")
        assert proc.returncode == 0, (proc.returncode, proc.stdout, proc.stderr)
        assert f"[{rs.DEFAULT_ALIAS}] {DEFAULT_SCOPE}/thing-alpha.md" in proc.stdout
        assert f"[{SECOND_ALIAS}] {SECOND_SCOPE}/other-thing.md" in proc.stdout

    def test_ls_entries_on_ONE_instance_prints_the_bare_two_part_path(self, world):
        proc = world.run("ls-entries")
        assert proc.returncode == 0, (proc.returncode, proc.stdout, proc.stderr)
        assert f"{DEFAULT_SCOPE}/thing-alpha.md" in proc.stdout
        assert "[" not in proc.stdout, proc.stdout

    def test_an_explicit_cache_with_TWO_instances_is_REFUSED(self, world, tmp_path):
        """🔴 THE DAMAGE WOULD BE TO THE CACHE, SILENTLY. Two snapshots unpacked
        into one root interleave their scopes and the stamp dates only the last
        one — a store that can say neither what it holds nor how old it is."""
        world.add_instance()
        proc = world.run("--cache", str(tmp_path / "shared"), "sync")
        assert proc.returncode == 2, (proc.returncode, proc.stdout, proc.stderr)
        assert "share one directory" in proc.stderr, proc.stderr


# =============================================================================
# WRITES NAME THEIR INSTANCE.
# =============================================================================


class TestAWriteSaysWhereItLanded:
    def test_append_prints_the_instance_even_with_ONE_configured(self, world):
        """🔴 UNCONDITIONAL, UNLIKE A READ'S LABEL. 'Where did that bullet go' is
        a question about a DURABLE record, asked later, by someone who no longer
        has this terminal."""
        proc = world.run("append", "--scope", DEFAULT_SCOPE, "--ref", "thing-alpha",
                         "--text", "a new bullet", "--session", "s-1")
        assert proc.returncode == 0, (proc.returncode, proc.stdout, proc.stderr)
        assert f"instance={rs.DEFAULT_ALIAS}" in proc.stdout, proc.stdout

    def test_append_lands_on_the_ROUTED_instance(self, world):
        world.add_instance()
        world.write_routes({DEFAULT_SCOPE: rs.DEFAULT_ALIAS, SECOND_SCOPE: SECOND_ALIAS})
        proc = world.run("append", "--scope", SECOND_SCOPE, "--ref", "other-thing",
                         "--text", "a routed bullet", "--session", "s-2")
        assert proc.returncode == 0, (proc.returncode, proc.stdout, proc.stderr)
        assert f"instance={SECOND_ALIAS}" in proc.stdout, proc.stdout
        # 🔴 MEASURED ON DISK, NOT IN THE MESSAGE. A client that PRINTED the
        # right instance and wrote to the other one would pass a message check.
        landed = (world.second.root / SECOND_SCOPE / "other-thing.md").read_text()
        assert "a routed bullet" in landed
        other = (world.first.root / DEFAULT_SCOPE / "thing-alpha.md").read_text()
        assert "a routed bullet" not in other


# =============================================================================
# THE CONFIGURATION ITSELF.
# =============================================================================


class TestDiscoveryAndTheTableFile:
    def test_the_default_instance_exists_with_NO_FILE_at_all(self, tmp_path):
        """🔴 THE ENVIRONMENT-ONLY DEPLOYMENT IS A REAL ONE — every test in this
        repository is one. Making the default instance conditional on a file
        would report 'nothing configured' on a host that is configured."""
        routing = ci.discover({"SUBSYSTEM_STORE_CONFIG": str(tmp_path / "nope" / "env")})
        assert routing.aliases == (rs.DEFAULT_ALIAS,)
        assert routing.active is False
        assert routing.alias_for("anything") == rs.DEFAULT_ALIAS

    def test_a_file_that_cannot_be_an_alias_is_an_ERROR_not_a_skip(self, tmp_path):
        """🔴 SKIPPING IT WOULD LEAVE NO MESSAGE ANYWHERE. The operator wrote a
        file, believes a store is configured, and the scopes they routed to it
        would later refuse naming the ALIAS — pointing at the table rather than
        at the file that was ignored."""
        cfg = tmp_path / "config" / "env"
        cfg.parent.mkdir(parents=True)
        (cfg.parent / "instances").mkdir()
        (cfg.parent / "instances" / "Upper.env").write_text("")
        with pytest.raises(ci.RoutingConfigError) as exc:
            ci.discover({"SUBSYSTEM_STORE_CONFIG": str(cfg)})
        assert "Upper" in str(exc.value)

    def test_an_instances_file_may_NOT_claim_the_default_alias(self, tmp_path):
        cfg = tmp_path / "config" / "env"
        cfg.parent.mkdir(parents=True)
        (cfg.parent / "instances").mkdir()
        (cfg.parent / "instances" / f"{rs.DEFAULT_ALIAS}.env").write_text("")
        with pytest.raises(ci.RoutingConfigError):
            ci.discover({"SUBSYSTEM_STORE_CONFIG": str(cfg)})

    def test_an_EXPLICIT_table_path_that_does_not_exist_is_an_ERROR(self, tmp_path):
        """🔴 NOT 'routing is off'. An operator who named a table meant to use
        one; ignoring a typo'd path turns a fail-loud configuration into a
        fail-open one, which is the exact inversion this design exists to avoid."""
        cfg = tmp_path / "config" / "env"
        cfg.parent.mkdir(parents=True)
        with pytest.raises(ci.RoutingConfigError):
            ci.discover({
                "SUBSYSTEM_STORE_CONFIG": str(cfg),
                "CAIRN_ROUTES": str(tmp_path / "no-such-table.json"),
            })

    @pytest.mark.parametrize("body", ['[]', '"x"', '{"a": 1}', '{"a": {"b": "c"}}',
                                      '{"a": "Bad Alias"}', 'not json'])
    def test_a_table_that_is_not_a_FLAT_MAP_OF_ALIASES_is_refused(self, tmp_path, body):
        """🔴 A TABLE THAT PARSED TO NOTHING WOULD LEAVE EVERY SCOPE UNROUTED,
        which LOOKS like a deliberate refusal and is not."""
        path = tmp_path / "routes.json"
        path.write_text(body)
        with pytest.raises(ci.RoutingConfigError):
            ci.load_routes(path)

    def test_a_WELL_FORMED_table_loads(self, tmp_path):
        """The control for the six refusals above."""
        path = tmp_path / "routes.json"
        path.write_text(json.dumps({DEFAULT_SCOPE: rs.DEFAULT_ALIAS}))
        assert ci.load_routes(path) == {DEFAULT_SCOPE: rs.DEFAULT_ALIAS}

    def test_the_environment_overrides_the_DEFAULT_instance_ONLY(self, world):
        """🔴 IF IT WON EVERYWHERE, A FAN-OUT WOULD MEASURE ONE STORE N TIMES and
        report agreement it never observed."""
        world.add_instance()
        world.write_routes({DEFAULT_SCOPE: rs.DEFAULT_ALIAS, SECOND_SCOPE: SECOND_ALIAS})
        proc = world.run(
            "recall", "--scope", SECOND_SCOPE,
            SUBSYSTEM_STORE_URL=world.first.url, SUBSYSTEM_STORE_TOKEN=world.first.token,
        )
        # The environment named the FIRST store; the route names the second, and
        # the second is what answers — its entry is the one that appears.
        assert proc.returncode == 0, (proc.returncode, proc.stdout, proc.stderr)
        assert "other-thing" in proc.stdout, proc.stdout
