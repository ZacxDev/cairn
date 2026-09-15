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

        def add_empty_scope(self, store: "_Store", name: str) -> Path:
            """A scope DIRECTORY holding no entry file.

            🔴 IT IS NOT REACHABLE THROUGH THE SNAPSHOT, WHICH IS THE POINT. The
            server ships `<scope>/<x>.md` members and no directory members, so a
            scope in this state is present to the SERVER (its index registers it
            via `extra_scopes`) and absent from every client cache.
            """
            path = store.root / name
            path.mkdir(parents=True, exist_ok=True)
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
        # 🔴 AND IT IS THE *NO TABLE* REFUSAL, NOT THE *NOT IN THE TABLE* ONE.
        # Found by a surviving mutant: deleting the `routes is None` arm let this
        # case fall through to the missing-entry refusal, which exits 11 and
        # contains both strings above — so the assertions passed while the
        # message told the operator to add a line to a file named `None`. The
        # two remedies are different (CREATE a table vs EDIT one), so the two
        # messages have to be distinguishable.
        assert "Write a scope->alias table to" in proc.stderr, proc.stderr
        assert "is not in the routing table" not in proc.stderr, proc.stderr

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
# ONE INSTANCE *PLUS A TABLE* — the configuration `active` got wrong both ways.
# =============================================================================


class TestOneInstanceWithATablePresent:
    """🔴 THE THREE ROWS THAT SHARE ONE CONFIGURATION AND NEED DIFFERENT ANSWERS.

    A host with ONE instance and a routing table is what every operator reaches
    the moment they write their first table — the table has to exist before a
    second instance does, because the second instance is what it is written FOR.
    `Routing.active` was `routes is not None or len(instances) > 1`, so a table's
    PRESENCE switched routing on, and that is wrong in two directions at once:

        row                                  | active=…|or  | len>1 only | correct
        -------------------------------------|---------|----|------------|--------
        (1) no table                          | sole   | sole       | sole
        (2) table, scope ABSENT               | REFUSE | sole       | sole
        (3) table -> UNCONFIGURED alias       | REFUSE | sole       | REFUSE

    Column 2 is the shipped defect (row 2 refuses, and every recall claims "more
    than one instance configured" on a host that has one). Column 3 is the
    obvious fix and it opens a SILENT MISROUTE at row 3. Each row below is
    reachable by an input the others do not reject, so none of them is a
    restatement of another.
    """

    def test_row1_NO_table_resolves_to_the_sole_instance(self, world):
        proc = world.run("recall", "--scope", DEFAULT_SCOPE)
        assert proc.returncode == 0, (proc.returncode, proc.stdout, proc.stderr)
        assert "thing-alpha" in proc.stdout, proc.stdout

    def test_row2_a_table_that_does_NOT_name_the_scope_still_resolves(self, world):
        """🔴 THE SHIPPED DEFECT, IN THE FORM AN OPERATOR MEETS IT. The table
        names some other scope; this one is simply not in it yet. With one
        instance there is exactly one answer to `where did that go?`, which is
        the same reasoning the no-table row has always used."""
        world.write_routes({"some-other-scope": rs.DEFAULT_ALIAS})
        proc = world.run("recall", "--scope", DEFAULT_SCOPE)
        assert proc.returncode == 0, (proc.returncode, proc.stdout, proc.stderr)
        assert "thing-alpha" in proc.stdout, proc.stdout

    def test_row3_a_table_naming_an_UNCONFIGURED_alias_still_REFUSES(self, world):
        """🔴 THE ROW THAT MUST NOT FOLLOW ROW 2. The table says this scope lives
        on `no-such-instance`; resolving it to the sole instance would be a read
        — and then a WRITE — landing in a store nobody decided on."""
        world.write_routes({DEFAULT_SCOPE: "no-such-instance"})
        proc = world.run("recall", "--scope", DEFAULT_SCOPE)
        assert proc.returncode == 11, (proc.returncode, proc.stdout, proc.stderr)
        assert "no-such-instance" in proc.stderr, proc.stderr
        assert "subsystem-recall:" not in proc.stdout, proc.stdout

    def test_row3_also_refuses_a_WRITE_and_writes_nothing(self, world):
        """The same row through the path where a misroute is durable."""
        world.write_routes({DEFAULT_SCOPE: "no-such-instance"})
        before = (world.first.root / DEFAULT_SCOPE / "thing-alpha.md").read_text()
        proc = world.run("append", "--scope", DEFAULT_SCOPE, "--ref", "thing-alpha",
                         "--text", "a misrouted bullet", "--session", "s-1")
        assert proc.returncode == 11, (proc.returncode, proc.stdout, proc.stderr)
        after = (world.first.root / DEFAULT_SCOPE / "thing-alpha.md").read_text()
        assert after == before, "the refusal still wrote to the sole instance"

    def test_a_table_on_ONE_instance_LABELS_NOTHING(self, world):
        """🔴 THE OTHER HALF OF THE DEFECT, AND THE ONE NOBODY WOULD HAVE SEEN AS
        A BUG. With `active` keyed on the table, every recall on a one-instance
        host with a table asserted `with more than one instance configured …` —
        false on that host, on every call."""
        world.write_routes({DEFAULT_SCOPE: rs.DEFAULT_ALIAS})
        proc = world.run("recall", "--scope", DEFAULT_SCOPE)
        assert proc.returncode == 0, (proc.returncode, proc.stdout, proc.stderr)
        assert "cairn[" not in proc.stdout, proc.stdout
        assert "another instance" not in proc.stdout, proc.stdout
        assert "more than one" not in proc.stdout, proc.stdout
        # The control: the SAME table with a second instance configured DOES
        # label, so the assertions above pin the instance count rather than a
        # feature that stopped working.
        world.add_instance()
        world.write_routes({DEFAULT_SCOPE: rs.DEFAULT_ALIAS, SECOND_SCOPE: SECOND_ALIAS})
        proc = world.run("recall", "--scope", DEFAULT_SCOPE)
        assert proc.returncode == 0, (proc.returncode, proc.stdout, proc.stderr)
        assert f"cairn[{rs.DEFAULT_ALIAS}]" in proc.stdout, proc.stdout
        assert "another instance" in proc.stdout, proc.stdout

    def test_TWO_instances_keep_the_FULL_refusal_semantics(self, world):
        """The ≥2-instance column, unchanged: rows 2 and 3 both refuse there."""
        world.add_instance()
        world.write_routes({"some-other-scope": rs.DEFAULT_ALIAS})
        absent = world.run("recall", "--scope", DEFAULT_SCOPE)
        assert absent.returncode == 11, (absent.returncode, absent.stderr)
        assert DEFAULT_SCOPE in absent.stderr
        world.write_routes({DEFAULT_SCOPE: "no-such-instance"})
        unconfigured = world.run("recall", "--scope", DEFAULT_SCOPE)
        assert unconfigured.returncode == 11, (unconfigured.returncode, unconfigured.stderr)
        assert "no-such-instance" in unconfigured.stderr


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

    def _check(self, table, scopes, aliases=None):
        """The PROBLEMS half. `check` returns `(problems, notes)` — see
        `_notes` for the other half and `Routing.check` for why there are two."""
        return ci.routing_for(table, aliases or self.ALIASES).check(scopes)[0]

    def _notes(self, table, scopes, aliases=None):
        return ci.routing_for(table, aliases or self.ALIASES).check(scopes)[1]

    def test_a_scope_with_NO_entry_is_a_problem(self):
        problems = self._check(
            {DEFAULT_SCOPE: rs.DEFAULT_ALIAS}, {DEFAULT_SCOPE, SECOND_SCOPE}
        )
        assert len(problems) == 1, problems
        assert SECOND_SCOPE in problems[0]
        assert "REFUSE" in problems[0]

    def test_an_entry_naming_NO_scope_is_a_NOTE_and_not_a_problem(self):
        """🔴 DEMOTED, AND THE DEMOTION IS THE FIX. This direction subtracts the
        cache's DIRECTORY LISTING from the table's keys, and a snapshot ships
        entry FILES — so a scope holding nothing is missing from that set whether
        it is stale, pre-registered, or pruned back to empty. See
        `test_an_EXISTS_BUT_EMPTY_scope_is_not_graded_stale` for the case that
        forced it."""
        table = {DEFAULT_SCOPE: rs.DEFAULT_ALIAS, "retired-scope": SECOND_ALIAS}
        assert self._check(table, {DEFAULT_SCOPE}) == ()
        notes = self._notes(table, {DEFAULT_SCOPE})
        assert len(notes) == 1, notes
        assert "retired-scope" in notes[0]

    def test_an_entry_naming_an_UNCONFIGURED_alias_is_a_problem(self):
        problems = self._check({DEFAULT_SCOPE: "ghost"}, {DEFAULT_SCOPE})
        assert len(problems) == 1, problems
        assert "ghost" in problems[0]

    def test_an_UNCONFIGURED_alias_is_a_problem_at_ONE_instance_TOO(self):
        """🔴 THE ONE-INSTANCE ROW OF THE THREE-ROW TABLE, GRADED. A host with a
        single instance still cannot reach an alias it has no config for, so the
        finding survives — and it is the direction `Routing.check` now asks
        `alias_for` for rather than re-deriving."""
        problems = self._check(
            {DEFAULT_SCOPE: "ghost"}, {DEFAULT_SCOPE}, aliases=(rs.DEFAULT_ALIAS,)
        )
        assert len(problems) == 1, problems
        assert "ghost" in problems[0]

    def test_an_UNNAMED_scope_is_NOT_a_problem_at_ONE_instance(self):
        """🔴 THE FINDING'S OWN CLAIM IS THE THING BEING GRADED, and on a
        one-instance host it is FALSE. `a write to it will REFUSE` is a
        prediction about `alias_for`, which resolves an unnamed scope to the sole
        instance — so grading this direction by subtracting key sets printed a
        confident warning about behaviour that does not happen."""
        assert self._check(
            {DEFAULT_SCOPE: rs.DEFAULT_ALIAS},
            {DEFAULT_SCOPE, SECOND_SCOPE},
            aliases=(rs.DEFAULT_ALIAS,),
        ) == ()
        # …and the SAME table over TWO instances still reports it, so the line
        # above is a statement about the instance count and not about the check
        # having been disabled.
        assert len(self._check(
            {DEFAULT_SCOPE: rs.DEFAULT_ALIAS}, {DEFAULT_SCOPE, SECOND_SCOPE}
        )) == 1

    def test_a_table_that_agrees_with_reality_has_NO_problems(self):
        """The control. Without it every assertion above is satisfiable by a
        function that returns a problem for everything."""
        table = {DEFAULT_SCOPE: rs.DEFAULT_ALIAS, SECOND_SCOPE: SECOND_ALIAS}
        scopes = {DEFAULT_SCOPE, SECOND_SCOPE}
        assert self._check(table, scopes) == ()
        # …and no NOTE either, which is what keeps the demotion in the test
        # above from being satisfiable by a `check` that notes everything.
        assert self._notes(table, scopes) == ()

    def test_grading_a_host_with_NO_TABLE_refuses_rather_than_reporting_clean(self):
        """🔴 AN ABSENT TABLE READ AS AN EMPTY ONE WOULD REPORT EVERY SCOPE AS
        UNROUTED — findings about a table nobody wrote."""
        routing = ci.Routing(
            instances=(ci.Instance(alias=rs.DEFAULT_ALIAS, config_path=Path(os.devnull)),),
            routes=None, routes_source=None,
        )
        with pytest.raises(ci.RoutingConfigError):
            routing.check({DEFAULT_SCOPE})

    def test_the_CLI_REPORTS_a_stale_entry_without_FAILING_on_it(self, world):
        """🔴 REPORTED, NOT GRADED, AND THAT IS THE WHOLE OF FINDING 4. An entry
        naming a scope that holds nothing is indistinguishable from one naming a
        scope pre-registered before its first write, so it prints as a ⚠ note
        and the command still exits 0."""
        world.add_instance()
        world.write_routes({
            DEFAULT_SCOPE: rs.DEFAULT_ALIAS,
            SECOND_SCOPE: SECOND_ALIAS,
            "retired-scope": SECOND_ALIAS,
        })
        proc = world.run("routes", "--check")
        assert proc.returncode == 0, (proc.returncode, proc.stdout, proc.stderr)
        assert "retired-scope" in proc.stderr, proc.stderr
        assert "1 note(s)" in proc.stdout, proc.stdout
        assert "0 problem(s)" in proc.stdout, proc.stdout
        # 🔴 AND IT MUST NOT TELL THE OPERATOR TO DELETE THE LINE. With two
        # instances configured, deleting it makes the next write REFUSE — the
        # remedy the old wording implied was the harm.
        assert "rather than deleting" in proc.stderr, proc.stderr

    def test_an_EXISTS_BUT_EMPTY_scope_is_not_graded_stale(self, world):
        """🔴 THE CASE THAT FORCED THE DEMOTION, MEASURED THROUGH THE CLI. The
        scope EXISTS on the second instance — `recall` reaches it and says
        `scope-empty` — but it holds no entry file, and a snapshot ships entry
        files rather than directories, so the cache has no directory for it and
        the grader cannot tell it from a retired one.

        Before the fix this exited 11 naming `hollow-set`, and the two
        assertions below disagreed with each other on the same world."""
        world.add_instance()
        world.add_empty_scope(world.second, "hollow-set")
        world.write_routes({
            DEFAULT_SCOPE: rs.DEFAULT_ALIAS,
            SECOND_SCOPE: SECOND_ALIAS,
            "hollow-set": SECOND_ALIAS,
        })
        # The store's own answer about that scope, which is the fact the grader
        # was contradicting. Without this the test below is a claim about a
        # scope nobody proved exists.
        recalled = world.run("recall", "--scope", "hollow-set")
        assert recalled.returncode == 0, (recalled.returncode, recalled.stderr)
        assert "scope-empty" in recalled.stdout + recalled.stderr, recalled.stdout

        proc = world.run("routes", "--check")
        assert proc.returncode == 0, (proc.returncode, proc.stdout, proc.stderr)
        assert "hollow-set" in proc.stderr, proc.stderr
        assert "0 problem(s)" in proc.stdout, proc.stdout

    def test_the_CLI_grades_the_table_and_exits_NONZERO_on_a_missing_entry(self, world):
        """Direction ONE through the CLI: a scope that EXISTS and is unnamed. 🔴 IT
        IS STILL A VERDICT — only direction TWO was demoted to a note, and without
        this row "the check exits 0 now" would be satisfiable by a check that
        graded nothing at all."""
        world.add_instance()
        world.write_routes({DEFAULT_SCOPE: rs.DEFAULT_ALIAS})
        proc = world.run("routes", "--check")
        assert proc.returncode == 11, (proc.returncode, proc.stdout, proc.stderr)
        assert SECOND_SCOPE in proc.stderr, proc.stderr

    def test_a_CORRECT_table_passes_the_CLI_check(self, world):
        """🔴 THE CONTROL FOR EVERY ROW IN THIS CLASS, AND THE ORACLE HALF OF THE
        TWO-INSTANCE PAIR. `TestRoutesCheckReadsEACHInstanceFromITSOWNConfig` in
        `internal/client/instances_test.go` is its Go twin: the Go client fetched
        the DEFAULT pod twice and graded the second instance's scopes as missing,
        which is the state THIS test would have caught had the port had one."""
        world.add_instance()
        world.write_routes({DEFAULT_SCOPE: rs.DEFAULT_ALIAS, SECOND_SCOPE: SECOND_ALIAS})
        proc = world.run("routes", "--check")
        assert proc.returncode == 0, (proc.returncode, proc.stdout, proc.stderr)
        assert "0 problem(s)" in proc.stdout, proc.stdout

    def test_DELETING_a_pre_registered_line_is_what_makes_the_write_REFUSE(self, world):
        """🔴 THE REASON THE OLD REMEDY WAS WRONG, MEASURED RATHER THAN ARGUED.
        The old note said the entry "exists on no configured instance — a stale
        entry reads as coverage", which implies deleting the line. The line is
        what lets the write RESOLVE at all: with it, the client routes and the
        STORE decides; without it, the client refuses at 11 having contacted
        nothing.

        ⚠ NARROWED TO THE CLIENT'S DECISION ON PURPOSE. What the store then says
        about a scope it has never held is the store's business and a different
        contract — so this asserts `!= EXIT_UNROUTED` on the routed arm rather
        than `== 0`, which would be a claim about the server."""
        world.add_instance()
        new_entry = world.home / "new.md"
        new_entry.write_text(_entry("fresh-thing", "not-yet-written"))
        args = ("create", "--scope", "not-yet-written", "--ref", "fresh-thing",
                "--file", str(new_entry))

        world.write_routes({
            DEFAULT_SCOPE: rs.DEFAULT_ALIAS,
            SECOND_SCOPE: SECOND_ALIAS,
            "not-yet-written": SECOND_ALIAS,
        })
        routed = world.run(*args)
        assert routed.returncode != 11, (routed.returncode, routed.stderr)

        # …and with the line REMOVED — the remedy the old wording implied — the
        # same invocation refuses before the store is reached.
        world.write_routes({DEFAULT_SCOPE: rs.DEFAULT_ALIAS, SECOND_SCOPE: SECOND_ALIAS})
        refused = world.run(*args)
        assert refused.returncode == 11, (refused.returncode, refused.stdout, refused.stderr)
        assert "not-yet-written" in refused.stderr, refused.stderr


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
        assert routing.multi_instance is False
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

    def test_an_EDITOR_LOCK_FILE_does_not_take_every_verb_to_exit_11(self, tmp_path):
        """🔴 FINDING 6. Emacs' lock file for `secondary.env` is
        `.#secondary.env`: it ends in `.env`, its stem is not a usable alias, and
        it is a DANGLING SYMLINK so `is_dir()` is False — so it reached the hard
        refusal above and EVERY `cairn` invocation on that host exited 11 while
        the buffer was open.

        Two cases, because one of them is the realistic shape and the other is
        the one a test is tempted to write: a dangling symlink (what Emacs
        actually creates) and a plain file."""
        cfg = tmp_path / "config" / "env"
        cfg.parent.mkdir(parents=True)
        instances = cfg.parent / "instances"
        instances.mkdir()
        (instances / f"{SECOND_ALIAS}.env").write_text("")

        lock = instances / f".#{SECOND_ALIAS}.env"
        lock.symlink_to("zach@host.12345:1700000000")  # dangling, as Emacs writes it
        assert not lock.exists(), "the fixture must be a DANGLING link, as Emacs writes it"
        routing = ci.discover({"SUBSYSTEM_STORE_CONFIG": str(cfg)})
        assert routing.aliases == (rs.DEFAULT_ALIAS, SECOND_ALIAS), routing.aliases
        lock.unlink()

        plain = instances / ".#vim-style.env"
        plain.write_text("")
        routing = ci.discover({"SUBSYSTEM_STORE_CONFIG": str(cfg)})
        assert routing.aliases == (rs.DEFAULT_ALIAS, SECOND_ALIAS), routing.aliases
        plain.unlink()

        # 🔴 THE CONTROL: a NON-dotted file the operator really did write is
        # still an ERROR. Without it this test is satisfied by deleting the
        # refusal, which is the rule it is narrowing rather than removing.
        (instances / "Upper.env").write_text("")
        with pytest.raises(ci.RoutingConfigError):
            ci.discover({"SUBSYSTEM_STORE_CONFIG": str(cfg)})

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
