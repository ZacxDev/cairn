"""THE CAPABILITY LEDGER: one place that says what `cairn` can do, and on which
surfaces.

🔴 WHY A LEDGER AT ALL. `claudedocs/plan-cairn-control-plane.md` §I commits to a
UI and a Go rewrite with the requirement that the CLI cover every feature.
"Somebody will remember to add the verb" is not a mechanism: it is a promise, and
a promise cannot go red. So the capability set is DECLARED here, once, and
`tests/test_capability_ledger.py` asserts the CLI verb table and the HTTP route
table against it in BOTH directions — a surface that GROWS without a row fails,
and a surface that SHRINKS while its row stays fails. An asserted ledger that
only catches growth is half a ledger: it cannot tell you the feature you were
promised parity on was deleted.

🔴 WHAT A ROW CLAIMS, AND WHAT IT DOES NOT. A row says this capability is
REACHABLE on that surface. It does NOT say the CLI verb is a client of the route,
and for two rows it demonstrably is not: `cairn recall` and `cairn search` render
the LOCAL cache that `sync` populated and never request `/api/v1/recall/<scope>`
or `/api/v1/search/<scope>` — measured, the client has exactly one read request
builder and its target is `/api/v1/snapshot`. Those two capabilities therefore
have TWO implementations of one answer, which is the "two renderers drift" risk
the plan names, and the row is where that is written down rather than discovered
later by a diff. Pairing them is still the honest shape: the parity question is
"can a CLI user get a scope's digest", and the answer is yes.

🔴 THE ROW SHAPE HAS TO EXPRESS AN ASYMMETRY, NOT FORCE A MAPPING. The two
surfaces are deliberately not 1:1, so every absence is declared WITH ITS REASON
and `Capability` refuses a row that leaves one unexplained:

  * three verbs answer from the local cache and have no route at all
    (`validate`, `ls-entries`, `doctor`);
  * `/healthz` has no verb — it is the kubelet's probe, and `doctor` is the
    operator-facing reachability answer;
  * ONE route carries TWO capabilities: `PUT …/entry/<scope>/<ref>` replaces
    behind `If-Match` and CREATES behind `If-None-Match: *`. `server.py`'s
    `WRITE_ROUTES` comment is explicit that this is one route on purpose (two
    rows would mean two URLs for one noun), so the ledger mirrors it — and any
    route named by more than one row must say what discriminates them.

🔴 A ROW MAY BE `go_only` — every surface it names exists in the GO
implementation only, by decision (decision 3 of
`claudedocs/plan-cairn-arcs-sessions.md`). Such a row is left out of the
comparison against the Python parser and `server.py`, where it must be ABSENT,
and is compared against the Go binaries instead. See `Capability.go_only` and
the checkers at the bottom of this file.

⚠ THE UI COLUMN DOES NOT EXIST YET, deliberately. §I names three surfaces; the
third has no code (plan P5), and a field with nothing to assert against is a
column that drifts unchecked. It arrives with the UI, in the same commit as the
guard that reads it.

⚠ AND THIS IS A PYTHON TABLE, WHICH BOUNDS WHO CAN READ IT. The conformance
fixtures (plan P0/§H) are language-agnostic because a Go implementation has to run
against them; this ledger is asserted only by the Python suite today. If the Go
side must read it, it becomes a data file — and that is a one-file change
precisely because nothing outside this module knows the shape.
"""

from __future__ import annotations

import argparse
import importlib.machinery
import importlib.util
import re
import sys
from dataclasses import dataclass
from pathlib import Path

from testlib import cairn_source

#: The two values of the read/write axis. It is about THE SERVED STORE, not about
#: local disk: `sync` WRITES the local cache and is still a `READS` capability,
#: because nothing it does changes what another reader sees. Getting this axis
#: backwards for a write verb is not cosmetic — `cairn`'s own subparsers carry
#: `writes=True` so an unreachable store exits 7 ("the record was NOT made")
#: rather than 3 ("nothing was shown"), and the test file asserts this column
#: against that flag.
READS = "reads"
WRITES = "writes"

REPO = Path(__file__).resolve().parents[2]


@dataclass(frozen=True)
class Capability:
    """One thing cairn can do, and the surfaces it is reachable on.

    `name` is a stable id and deliberately NOT a verb spelling: renaming a
    subcommand should move one field, not the identity of the capability.
    `route` is `(METHOD, path)` where the path's `{placeholders}` are named for a
    reader — the guard normalises them away, so the names are documentation and
    the COUNT of them is what is checked.
    """

    name: str
    effect: str
    cli: str | None
    route: tuple[str, str] | None
    #: Required exactly when the matching surface is absent. A bare `None` would
    #: record that a surface is missing; these record WHY, which is the part a
    #: reader needs in order to tell a deliberate asymmetry from a parity gap.
    no_cli_because: str = ""
    no_route_because: str = ""
    #: What distinguishes this row from another row sharing its route. Mandatory
    #: in that case, unused otherwise.
    note: str = ""
    #: 🔴 EVERY SURFACE THIS ROW NAMES EXISTS IN THE GO IMPLEMENTATION ONLY — decision
    #: 3 of `claudedocs/plan-cairn-arcs-sessions.md`: new verbs and routes go into
    #: `cmd/cairn`/`cmd/cairn-server`, and the Python oracle is NOT extended. Such a
    #: row is compared against `cairn -verbs` / `cairn-server -routes` (in the `go`
    #: job, `tests/test_go_client_ledgers.py`) and is EXCLUDED from the comparison
    #: against the Python parser and `server.py`'s tables — where it must be ABSENT,
    #: or the declaration is stale. `go_only_why` is mandatory with the mark and
    #: refused without it, the same rule `requests.json`'s `go_only` follows.
    go_only: bool = False
    go_only_why: str = ""

    def __post_init__(self) -> None:
        if self.go_only != bool(self.go_only_why.strip()):
            raise ValueError(
                f"{self.name}: a go_only row must say why only Go has it, and a row "
                f"that is not go_only must not (go_only={self.go_only!r}, "
                f"go_only_why={self.go_only_why!r})"
            )
        if self.go_only and self.route is not None and not self.route[1].startswith(
            "/api/v1/"
        ):
            raise ValueError(
                f"{self.name}: a go_only route must be an /api/v1/ route — the Go "
                f"server's route ledger (`cairn-server -routes`) names nothing else, "
                f"so {self.route!r} could be checked against nothing"
            )
        if self.effect not in (READS, WRITES):
            raise ValueError(f"{self.name}: effect {self.effect!r} is not reads/writes")
        if (self.cli is None) != bool(self.no_cli_because):
            raise ValueError(
                f"{self.name}: a row with no CLI verb must say why, and a row "
                f"WITH one must not (cli={self.cli!r}, "
                f"no_cli_because={self.no_cli_because!r})"
            )
        if (self.route is None) != bool(self.no_route_because):
            raise ValueError(
                f"{self.name}: a row with no route must say why, and a row WITH "
                f"one must not (route={self.route!r}, "
                f"no_route_because={self.no_route_because!r})"
            )
        if self.cli is None and self.route is None:
            raise ValueError(
                f"{self.name}: a capability reachable on NO surface is not a "
                f"capability — nothing can be asserted about it in either "
                f"direction, so the row would only ever be prose"
            )


# 🔴 THE LEDGER. Adding a verb or a route means adding or amending a row HERE, in
# the same commit, or `tests/test_capability_ledger.py` goes red. Deleting one
# means deleting the row — and if a row looks stale, that is the question the
# gate exists to force somebody to answer out loud.
LEDGER: tuple[Capability, ...] = (
    Capability(
        name="snapshot-sync",
        effect=READS,
        cli="sync",
        route=("GET", "/api/v1/snapshot"),
        note="the one route the client actually requests on a read path",
    ),
    Capability(
        name="scope-digest",
        effect=READS,
        cli="recall",
        route=("GET", "/api/v1/recall/{scope}"),
        note="two implementations of one answer: the verb renders the synced "
             "cache, the route renders server-side. Plan P2 deletes one.",
    ),
    Capability(
        name="scope-search",
        effect=READS,
        cli="search",
        route=("GET", "/api/v1/search/{scope}"),
        note="as `scope-digest`: the verb searches the cache, not the route",
    ),
    Capability(
        name="scope-sessions",
        effect=READS,
        cli="sessions",
        route=("GET", "/api/v1/sessions/{scope}"),
        note="like `scope-digest`, the verb renders the synced cache and never "
             "requests the route — but both call ONE Go renderer "
             "(`report.Sessions`), so the two answers share every report byte "
             "rather than being two implementations of it",
        go_only=True,
        go_only_why="decision 3 of the arcs/sessions plan: the sessions surface "
                    "is added to `cmd/cairn-server` and `cmd/cairn` only, and the "
                    "Python oracle is not extended",
    ),
    Capability(
        name="scope-arcs",
        effect=READS,
        cli="arcs",
        route=("GET", "/api/v1/arcs/{scope}"),
        note="the verb IS a client of the route: registrations live only in the "
             "pod's journal, never in the cache, so `cairn arcs` prints the pod's "
             "body verbatim and there is one renderer (`report.Arcs`)",
        go_only=True,
        go_only_why="decision 3 of the arcs/sessions plan: the arc registry (S3) is "
                    "added to `cmd/cairn-server` and `cmd/cairn` only, and the Python "
                    "oracle is not extended",
    ),
    Capability(
        name="arc-show",
        effect=READS,
        cli="arc-show",
        route=("GET", "/api/v1/arc/{home}/{slug}"),
        note="a client of the route, like `scope-arcs`: the pod's body verbatim",
        go_only=True,
        go_only_why="decision 3 of the arcs/sessions plan: the arc registry (S3) is "
                    "Go-only",
    ),
    Capability(
        name="arc-register",
        effect=WRITES,
        cli="arc-register",
        route=("PUT", "/api/v1/arc/{home}/{slug}"),
        note="appends one record to the pod's arc journal, which lives OUTSIDE the "
             "store tree; it changes what a reader of the declared scopes sees",
        go_only=True,
        go_only_why="decision 3 of the arcs/sessions plan: the arc registry (S3) is "
                    "Go-only",
    ),
    Capability(
        name="scope-sources",
        effect=READS,
        cli=None,
        route=("GET", "/api/v1/sources/{scope}"),
        no_cli_because="S2 of the scope-refs plan lands the ROUTE alone; the proposed sources "
                       "verb, its client, is S3. Until then the route's readers are the browser "
                       "surface (S4) and the auditor (S5), neither of which is a verb yet",
        go_only=True,
        go_only_why="decision 10 of the scope-refs plan: the sources read is added to "
                    "`cmd/cairn-server` only, on the arcs precedent, and the Python oracle is "
                    "not extended",
    ),
    Capability(
        name="cache-validate",
        effect=READS,
        cli="validate",
        route=None,
        no_route_because="parse-checks the bytes already on this host; there is "
                         "nothing for the pod to answer",
    ),
    Capability(
        name="cache-list-entries",
        effect=READS,
        cli="ls-entries",
        route=None,
        no_route_because="lists the cached files as paths a local tool can open; "
                         "the remote equivalent is the snapshot's own listing",
    ),
    Capability(
        name="host-diagnosis",
        effect=READS,
        cli="doctor",
        route=None,
        no_route_because="its subject is THIS host and THIS credential — cache "
                         "siting, stamp age, which store the reader resolves. A "
                         "route could not observe any of it",
    ),
    Capability(
        name="instance-routing",
        effect=READS,
        cli="routes",
        route=None,
        no_route_because="its subject is THIS host's configuration — which "
                         "instances are configured here and which scope the "
                         "operator's own table sends where. No instance can "
                         "answer it: a pod knows nothing about its siblings, and "
                         "the table is deliberately not something this software "
                         "stores",
    ),
    Capability(
        name="append-bullet",
        effect=WRITES,
        cli="append",
        route=("POST", "/api/v1/entry/{scope}/{ref}/bullets"),
    ),
    Capability(
        name="replace-entry",
        effect=WRITES,
        cli="put",
        route=("PUT", "/api/v1/entry/{scope}/{ref}"),
        note="`If-Match: <revision>` — replaces bytes the caller has seen",
    ),
    Capability(
        name="create-entry",
        effect=WRITES,
        cli="create",
        route=("PUT", "/api/v1/entry/{scope}/{ref}"),
        note="`If-None-Match: *` — the same route, refusing to overwrite",
    ),
    Capability(
        name="liveness",
        effect=READS,
        cli=None,
        no_cli_because="the kubelet's unauthenticated probe, which says nothing "
                       "but `ok`. `doctor` is the operator-facing reachability "
                       "answer and reports more than a probe may reveal",
        route=("GET", "/healthz"),
    ),
)


def normalise_path(path: str) -> str:
    """`/api/v1/recall/{scope}` -> `/api/v1/recall/{}`.

    ⚠ SO THE PLACEHOLDER NAMES ARE NOT CHECKED BY ANYTHING — only how many
    components there are and where they sit. `{scope}` naming a ref would read
    wrong to a human and pass here. Named rather than assumed away: the guard
    that would close it is the conformance suite requesting the route, not a
    string comparison.
    """
    return re.sub(r"\{[^{}]*\}", "{}", path)


def _load_cairn_cli():
    """`cairn` as an executed module — it has no `.py` extension.

    ⚠ THE THIRD COPY OF THIS LOADER IN `tests/` (`test_cairn_doctor.py` and
    `test_cairn_write.py` each hold one). It is here rather than folded into one
    home because consolidating would mean editing both of those files while other
    sessions hold them open; the duplication is recorded so the next person to
    touch all three does it once. `testlib/cairn_source.py` is NOT that home: its
    header is explicit that it answers by AST and not by `exec`.
    """
    for extra in (REPO / "lib", REPO / "server"):
        if str(extra) not in sys.path:
            sys.path.insert(0, str(extra))
    loader = importlib.machinery.SourceFileLoader(
        "cairn_cli_ledger", str(cairn_source.CAIRN_CLI)
    )
    spec = importlib.util.spec_from_loader("cairn_cli_ledger", loader)
    assert spec and spec.loader
    module = importlib.util.module_from_spec(spec)
    # Registered BEFORE execution: a module missing from `sys.modules` while it
    # runs breaks the first `@dataclass` under `from __future__ import
    # annotations`. Same idiom, same reason, as every other loader in `tests/`.
    sys.modules[spec.name] = module
    spec.loader.exec_module(module)
    return module


def cli_verbs_from_parser() -> dict[str, bool]:
    """Every subcommand the BUILT parser dispatches -> whether it writes the store.

    🔴 THE AUTHORITATIVE HALF OF THE DISCOVERY, and the complement to
    `cairn_source.subcommand_names`. This one asks argparse what it will actually
    accept, so a verb registered by any mechanism at all is here; it cannot see a
    verb the source names but never registers. The AST half is blind to the
    mechanism and sees the source. A ledger that matched only one of them would
    be a claim about that mechanism, so the test file requires both to agree.

    The `writes` flag is read with `get_default`, i.e. from the parser argparse
    built, not from the text of a `set_defaults` call.

    ⚠ `_SubParsersAction` AND `_actions` ARE PRIVATE, and measured on the pinned
    3.12 interpreter only. There is no public way to ask a parser what subcommands
    it has, so the choice is this or parsing `--help` output — which would make the
    help text's FORMAT a dependency of the parity gate. A rename upstream fails
    loudly here (no subparsers action found) rather than quietly reporting an empty
    set — see the raise below, and the test that exercises it.
    """
    parser = _load_cairn_cli().build_parser()
    actions = [
        a for a in parser._actions if isinstance(a, argparse._SubParsersAction)
    ]
    if len(actions) != 1:
        raise AssertionError(
            f"expected exactly one subparsers action on `cairn`'s parser, found "
            f"{len(actions)}. Either the CLI grew a second subcommand level — in "
            f"which case this discovery and the ledger both need widening on "
            f"purpose — or `argparse` moved the private API this reads, in which "
            f"case every verb assertion built on it is vacuous."
        )
    return {
        verb: bool(sub.get_default("writes"))
        for verb, sub in actions[0].choices.items()
    }


def http_routes(tables: object | None = None) -> dict[tuple[str, str], str]:
    """`(METHOD, normalised path)` -> `READS`/`WRITES`, from the server's own tables.

    🔴 READ FROM THE DISPATCHER, NOT FROM THE SOURCE TEXT. `server.py` says it
    out loud: `API_ROUTES` IS the read dispatcher and `WRITE_ROUTES` IS the write
    dispatcher, after two earlier guards that parsed the router — a regex, then an
    AST walk — were each defeated by an ordinary re-spelling. A table the code
    dispatches from cannot be out of date with the code.

    The PATH is derived, not copied: the tables carry an arity (path components
    including the head) and, for writes, a fixed tail, so the full template is
    reconstructed here. That makes the ledger's path column a check on those
    numbers too — adding a component to a route moves the derived path and fails
    the comparison.

    ⚠ THIS IS BLIND TO A BOUND METHOD THAT CARRIES NO ROUTE. `do_PATCH` and
    `do_DELETE` are bound and answer 405 by reaching the write door and finding no
    row; `do_HEAD` shares the read dispatcher with `do_GET`. None of them appear
    here, and a NEW `do_OPTIONS` would not either. That claim belongs to
    `tests/test_subsystem_store_api.py::TestPhaseOneScope::
    test_the_verb_ledger_is_the_whole_bound_verb_surface`, which pins the whole
    `do_*` surface and what each verb resolves to. Two ledgers, two claims, named
    so neither is read as covering the other.

    ⚠ AND IT IS BLIND TO WHETHER `/healthz` IS DISPATCHED. `HEALTH_PATH` is a
    constant; that it is answered before auth is a branch in `_handle`, which no
    table declares. The behavioural owner of that claim is
    `test_subsystem_store_api.py::TestHealthSaysNothing::
    test_it_answers_without_a_token`.

    `tables` is for the INSTRUMENT CONTROL: anything carrying `HEALTH_PATH`,
    `API_PREFIX`, `API_ROUTES` and `WRITE_ROUTES` can be fed in, so a caller can
    hand over a synthetic table and watch a route it invented appear — which is
    the only way to tell a derivation that works from one that happens to agree
    with a hand-written path. Omitted, it reads the real server.
    """
    if tables is None:
        import server  # `server/` is on `sys.path` via the repo's conftest

        tables = server
    api = tables

    routes: dict[tuple[str, str], str] = {("GET", api.HEALTH_PATH): READS}

    prefix = api.API_PREFIX.rstrip("/")
    for head, (_handler, arity) in api.API_ROUTES.items():
        # arity counts the head itself, so `arity - 1` components follow it.
        path = "/".join([prefix, head, *["{}"] * (arity - 1)])
        routes[("GET", path)] = READS

    for (method, head), (_handler, arity, tail) in api.WRITE_ROUTES.items():
        middles = arity - 1 - len(tail)
        if middles < 0:
            raise AssertionError(
                f"`WRITE_ROUTES[{(method, head)!r}]` declares arity {arity} with a "
                f"{len(tail)}-component tail, which leaves no room for the head. "
                f"The derived path below would be nonsense, so this refuses "
                f"instead of comparing one."
            )
        path = "/".join([prefix, head, *["{}"] * middles, *tail])
        routes[(method, path)] = WRITES

    return routes


def ledger_routes(ledger: tuple[Capability, ...] = LEDGER) -> dict[tuple[str, str], str]:
    """The ledger's own `(METHOD, normalised path)` -> effect, rows collapsed —
    over the rows the ORACLE serves, i.e. every row not marked `go_only`.

    Two rows may name one route (`put` and `create`); they must agree on the
    effect, and a disagreement raises here rather than silently keeping whichever
    row came last.
    """
    out: dict[tuple[str, str], str] = {}
    for row in ledger:
        if row.route is None or row.go_only:
            continue
        method, path = row.route
        key = (method, normalise_path(path))
        if key in out and out[key] != row.effect:
            raise AssertionError(
                f"rows sharing {key} disagree on effect: {out[key]} vs "
                f"{row.effect} ({row.name})"
            )
        out[key] = row.effect
    return out


# ---------------------------------------------------------------------------
# The Go-only surfaces: DECLARED here, checked against both implementations.
# ---------------------------------------------------------------------------
#
# 🔴 THE DECLARATION IS THE `go_only` ROWS ABOVE AND NOTHING ELSE. `GO_ONLY_VERBS` and
# `go_only_route_names` are DERIVED from them, never restated, so there is one place a
# Go-only verb or route is declared and every checker below reads it. The checkers are
# pure functions of plain sets so each can be fed a SYNTHETIC Go-only surface — none
# exists yet — and watched to go red and green; the real operands are discovered by
# the callers (`tests/test_capability_ledger.py` for the Python side, which needs no
# compiler; `tests/test_go_client_ledgers.py` for the Go side, which does).


def go_only_verbs(ledger: tuple[Capability, ...] = LEDGER) -> frozenset[str]:
    """Every CLI verb a `go_only` row names."""
    return frozenset(row.cli for row in ledger if row.go_only and row.cli is not None)


#: The declared Go-only verb set, derived from the `go_only` rows above.
GO_ONLY_VERBS: frozenset[str] = go_only_verbs()


def go_only_route_names(ledger: tuple[Capability, ...] = LEDGER) -> frozenset[str]:
    """Every `<METHOD> <head>` a `go_only` row's route is dispatched as.

    That is the vocabulary `cairn-server -routes` and `requests.json` speak, so the
    path is reduced to its head — and a GET route is ALSO a HEAD route, because the Go
    server expands every read head over both safe methods (`safeReadMethods`).
    """
    out: set[str] = set()
    for row in ledger:
        if not row.go_only or row.route is None:
            continue
        method, path = row.route
        head = path[len("/api/v1/"):].split("/", 1)[0]
        out.add(f"{method} {head}")
        if method == "GET":
            out.add(f"HEAD {head}")
    return frozenset(out)


def oracle_side_go_only_problems(
    ledger: tuple[Capability, ...],
    python_verbs: set[str],
    python_routes: set[tuple[str, str]],
) -> list[str]:
    """The STALE-declaration direction the Python side can see: a `go_only` row whose
    verb the Python parser HAS, or whose route `server.py` DISPATCHES. Either means the
    oracle grew the surface and the mark now hides a comparison.

    `python_routes` is `http_routes()`'s key set — `(METHOD, normalised path)`.
    """
    problems: list[str] = []
    for row in ledger:
        if not row.go_only:
            continue
        if row.cli is not None and row.cli in python_verbs:
            problems.append(
                f"{row.name}: marked go_only, and the PYTHON client has the verb "
                f"{row.cli!r}. The declaration is stale — drop the mark (and add the "
                f"verb to the shared set), or the oracle grew a verb by mistake."
            )
        if row.route is not None:
            key = (row.route[0], normalise_path(row.route[1]))
            if key in python_routes:
                problems.append(
                    f"{row.name}: marked go_only, and `server.py` DISPATCHES {key!r}. "
                    f"The declaration is stale — the oracle serves the route, so it "
                    f"must be compared, not excused."
                )
    return problems


def go_side_verb_problems(
    declared_go_only: frozenset[str],
    python_verbs: set[str],
    go_verbs: set[str],
) -> list[str]:
    """`cairn -verbs` against the Python parser and the declaration, BOTH directions.

    🔴 THE EQUATION: Go's verbs minus Python's == the declared Go-only set, and
    Python's minus Go's == nothing. Each way it can fail has its own sentence:

      * a Go verb Python lacks and nothing declares — an UNDECLARED Go-only verb;
      * a declared Go-only verb the Go client does not have — a STALE declaration;
      * a declared Go-only verb Python DOES have — stale the other way (the oracle
        grew it), caught here too so the `go` job alone cannot be green over it;
      * a Python verb Go lacks — a verb the cutover would DROP.
    """
    problems: list[str] = []
    undeclared = sorted(go_verbs - python_verbs - declared_go_only)
    if undeclared:
        problems.append(
            f"UNDECLARED Go-only verb(s) {undeclared}: the Go client has them and the "
            f"Python client does not. A Go-only verb is a DECLARED divergence — add a "
            f"`go_only` row for it to `testlib/capability_ledger.LEDGER` (decision 3), "
            f"never a silent one."
        )
    missing = sorted(declared_go_only - go_verbs)
    if missing:
        problems.append(
            f"STALE Go-only declaration {missing}: declared go_only and the Go client "
            f"does not have the verb, so the row describes nothing."
        )
    grown = sorted(declared_go_only & python_verbs)
    if grown:
        problems.append(
            f"STALE Go-only declaration {grown}: declared go_only and the PYTHON client "
            f"HAS the verb, so the mark excuses a comparison that can be made."
        )
    dropped = sorted(python_verbs - go_verbs)
    if dropped:
        problems.append(
            f"Python-only verb(s) {dropped}: the Go client lacks them, so the cutover "
            f"would DROP them."
        )
    return problems


def go_side_route_problems(
    go_routes: set[str],
    oracle_routes: set[str],
    corpus_go_only: set[str],
    ledger_go_only: frozenset[str],
) -> list[str]:
    """`cairn-server -routes` against the oracle's tables and BOTH Go-only declarations.

    All four operands are `<METHOD> <head>`. 🔴 THE EQUATION: Go's routes minus the
    oracle's == the routes `go_only` corpus rows address == the routes `go_only`
    capability rows name; and the oracle's minus Go's == nothing. The two validators
    each hold one edge of this (`cases.validate_corpus` refuses a `go_only` row for a
    route the oracle declares; `checkRouteLedger` refuses one for a route Go does not
    dispatch); this is the only place the whole triangle is read at once, out of the
    running binary.
    """
    problems: list[str] = []
    divergent = go_routes - oracle_routes
    for label, declared in (
        ("tests/conformance/requests.json `go_only` rows", corpus_go_only),
        ("testlib/capability_ledger `go_only` rows", ledger_go_only),
    ):
        undeclared = sorted(divergent - declared)
        if undeclared:
            problems.append(
                f"UNDECLARED Go-only route(s) {undeclared}: the Go server dispatches "
                f"them, the oracle does not, and {label} do not declare them."
            )
        stale = sorted(declared - divergent)
        if stale:
            problems.append(
                f"STALE Go-only declaration {stale} in {label}: either the Go server "
                f"does not dispatch it or the oracle ALSO does, so it is not a Go-only "
                f"route."
            )
    dropped = sorted(oracle_routes - go_routes)
    if dropped:
        problems.append(
            f"oracle-only route(s) {dropped}: the Go server — the DEPLOYED pod — does "
            f"not dispatch them."
        )
    return problems
