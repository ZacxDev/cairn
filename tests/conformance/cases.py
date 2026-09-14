"""The declared request list, loaded and validated. DATA, not code.

🔴 ADDING A CASE IS ADDING A ROW TO `requests.json`, AND NOTHING ELSE. That is
the whole reason the list is a data file rather than a function full of calls: a
row can be reviewed as a diff, counted, and — the part that matters for a
port — read by an implementation that is not Python.

What this module adds on top of `json.load` is the set of structural properties
the corpus has to hold for its goldens to mean anything. Each is a GUARD with
its own message, because a corpus that violates one of them still produces a
green run:

  * UNIQUE IDS — a duplicate id would have the second case silently overwrite
    the first's golden, halving coverage while the count went up.
  * READS BEFORE WRITES — a successful write changes entry mtimes, and every
    report body carries `entry-files=N` and `newest=<mtime>`. A read recorded
    after a write is a golden that depends on the order the list happens to be
    in; the phase split is what makes the corpus reproducible from a fresh
    store.
  * EVERY DECLARED ROUTE IS ADDRESSED — see `declared_routes` for what this can
    and cannot see. This is the one guard that turns the list into a LEDGER.
  * EVERY RELATION NAMES CASES THAT EXIST, and every case in a relation is
    actually issued.
"""

from __future__ import annotations

import ast
import json
from dataclasses import dataclass
from pathlib import Path
from typing import Any

SUITE_DIR = Path(__file__).resolve().parent
REPO_ROOT = SUITE_DIR.parents[1]
REQUESTS_PATH = SUITE_DIR / "requests.json"
SERVER_SOURCE = REPO_ROOT / "server" / "server.py"

#: The two phases, in the only order they may appear in.
PHASES = ("read", "write")


class CorpusError(AssertionError):
    """The declared corpus is not well-formed. Never a server verdict."""


@dataclass(frozen=True)
class Case:
    id: str
    phase: str
    why: str
    method: str | None = None
    target: str | None = None
    principal: str | None = None
    token_literal: str | None = None
    client_ip: str | None = "203.0.113.7"
    headers: tuple[tuple[str, str], ...] = ()
    body: dict[str, Any] | None = None
    raw_request: tuple[str, ...] | None = None
    normalize: tuple[str, ...] = ()
    body_kind: str = "auto"
    canary: bool = False
    #: 🔴 THIS ROW RECORDS AN ANSWER THAT IS THE ORACLE'S OWN SHAPE RATHER THAN A
    #: CONTRACT A PORT CAN HONOUR, so it is asserted against the Python server and
    #: SKIPPED — by id, with its reason, reported in the summary — for any other
    #: implementation. `oracle_only_why` states which artifact, and
    #: `validate_corpus` refuses a mark with no reason: a licence to differ that
    #: nobody can read is the same defect as an unused normalization one level up.
    #:
    #: ⚠ IT IS THE LAST RESORT, NOT THE FIRST. A response that differs only in ONE
    #: FIELD belongs in `wire.NORMALIZATIONS`, which keeps every other byte pinned
    #: for both implementations; this mark stops the whole case being compared, so
    #: whatever the row was covering has to be covered somewhere else and the
    #: reason must say where. Four rows carry it today.
    oracle_only: bool = False
    oracle_only_why: str = ""
    #: 🔴 THIS ROW DELIBERATELY ADDRESSES A METHOD/HEAD THE SERVER HAS NO ROUTE
    #: FOR — a `PATCH` at the entry noun, a `GET` at it, a `POST` at a read
    #: route. Such a row must NOT count as coverage of a route (its answer is a
    #: 405 or a 404), and `validate_corpus` fails if the pair it names turns out
    #: to BE a declared route: that would mean the row's whole premise is stale.
    negative_route: bool = False

    @property
    def is_raw(self) -> bool:
        return self.raw_request is not None


@dataclass(frozen=True)
class ScopePair:
    """One refused-vs-absent pair, and the substitution that makes it testable.

    🔴 THE SUBSTITUTION IS DECLARED PER CASE, NOT GUESSED. A report echoes the
    scope it was asked about — in the status line, in the caveat, and in the
    `NOTHING RECORDED YET` sentence — so two responses for two different scope
    names cannot be byte-identical and demanding it would be a test that can
    never pass. Replacing the ASKED-FOR NAME (and nothing else) in each response
    leaves every other byte pinned, which is exactly the claim: apart from the
    name you asked about, a refusal and an absence are the same answer.
    """

    name: str
    refused: str
    absent: str
    refused_scope: str
    absent_scope: str


@dataclass(frozen=True)
class HeadPair:
    """A HEAD case and the GET whose Content-Length it must report.

    🔴 NEITHER VALUE CAN LIVE IN A GOLDEN. A report body carries the machine
    identity and the store path, both variable in LENGTH per host, so
    `Content-Length` is host-dependent even though the normalized BODY is not.
    The claim "a HEAD reports the length its GET would have sent" survives
    anyway, because it is a comparison between two answers in ONE run.
    """

    name: str
    head: str
    get: str


@dataclass(frozen=True)
class Corpus:
    cases: tuple[Case, ...]
    uniform_401: tuple[str, ...]
    scope_pairs: tuple[ScopePair, ...]
    head_pairs: tuple[HeadPair, ...] = ()

    def by_id(self, case_id: str) -> Case:
        for c in self.cases:
            if c.id == case_id:
                return c
        raise CorpusError(f"no case with id {case_id!r}")


def load_corpus(path: Path | None = None) -> Corpus:
    raw = json.loads((path or REQUESTS_PATH).read_text(encoding="utf-8"))
    cases: list[Case] = []
    for row in raw["cases"]:
        row = {k: v for k, v in row.items() if not k.startswith("_")}
        cases.append(
            Case(
                id=row["id"],
                phase=row["phase"],
                why=row["why"],
                method=row.get("method"),
                target=row.get("target"),
                principal=row.get("principal"),
                token_literal=row.get("token_literal"),
                client_ip=row.get("client_ip", "203.0.113.7"),
                headers=tuple((k, v) for k, v in row.get("headers", [])),
                body=row.get("body"),
                raw_request=(
                    tuple(row["raw_request"]) if "raw_request" in row else None
                ),
                normalize=tuple(row.get("normalize", [])),
                body_kind=row.get("body_kind", "auto"),
                canary=bool(row.get("canary", False)),
                negative_route=bool(row.get("negative_route", False)),
                oracle_only=bool(row.get("oracle_only", False)),
                oracle_only_why=row.get("oracle_only_why", ""),
            )
        )
    corpus = Corpus(
        cases=tuple(cases),
        uniform_401=tuple(raw["relations"]["uniform_401"]),
        scope_pairs=tuple(
            ScopePair(
                name=p["name"],
                refused=p["refused"],
                absent=p["absent"],
                refused_scope=p["refused_scope"],
                absent_scope=p["absent_scope"],
            )
            for p in raw["relations"]["refused_equals_absent"]
        ),
        head_pairs=tuple(
            HeadPair(name=p["name"], head=p["head"], get=p["get"])
            for p in raw["relations"]["head_matches_get"]
        ),
    )
    validate_corpus(corpus)
    return corpus


def validate_corpus(corpus: Corpus, *, routes: set[str] | None = None) -> None:
    """Every structural property the corpus must hold. Raises `CorpusError`.

    `routes` overrides what the oracle's source declares. It exists so a test
    can mutation-test the ledger guard — add a route, remove one, mark a real
    route negative — without editing `server/server.py`. Default: the real set.
    """
    declared = declared_routes() if routes is None else routes
    seen: set[str] = set()
    for case in corpus.cases:
        if case.id in seen:
            raise CorpusError(
                f"duplicate case id {case.id!r}: the second row's golden would "
                f"overwrite the first's, so one case would be recorded twice and "
                f"the other not at all"
            )
        seen.add(case.id)
        if case.phase not in PHASES:
            raise CorpusError(f"case {case.id!r}: unknown phase {case.phase!r}")
        if case.is_raw:
            if case.method is not None or case.target is not None:
                raise CorpusError(
                    f"case {case.id!r}: a raw_request case carries its own request "
                    f"line, so `method`/`target` would be ignored"
                )
        elif not case.method or not case.target:
            raise CorpusError(f"case {case.id!r}: needs both `method` and `target`")
        if case.principal is not None and case.token_literal is not None:
            raise CorpusError(
                f"case {case.id!r}: `principal` and `token_literal` are two ways to "
                f"spell one Authorization header; declare exactly one"
            )
        if not case.why.strip():
            raise CorpusError(f"case {case.id!r}: every row states why it is here")
        if case.oracle_only and not case.oracle_only_why.strip():
            raise CorpusError(
                f"case {case.id!r} is marked oracle_only and states no reason. The "
                f"mark stops the WHOLE case being compared for every implementation "
                f"but the Python one, so it has to say which artifact it is excusing "
                f"and where the part that IS a contract is covered instead. An "
                f"unexplained licence to differ is the defect `wire.py`'s "
                f"anti-widening guard exists to prevent, one level up."
            )
        if case.oracle_only_why.strip() and not case.oracle_only:
            raise CorpusError(
                f"case {case.id!r} states an oracle_only reason but is not marked "
                f"oracle_only, so the reason describes nothing and the row IS "
                f"compared against every implementation. Set the mark, or delete "
                f"the reason."
            )

    # 🔴 READS BEFORE WRITES. See this module's docstring: the freshness fields a
    # report carries move the instant anything is written, so a read recorded
    # after a write depends on list order rather than on the store.
    phases = [c.phase for c in corpus.cases]
    first_write = next((i for i, p in enumerate(phases) if p == "write"), None)
    if first_write is not None:
        for i, case in enumerate(corpus.cases[first_write:], start=first_write):
            if case.phase == "read":
                raise CorpusError(
                    f"case {case.id!r} is a read at position {i}, after the first "
                    f"write case at position {first_write}. A successful write "
                    f"changes entry mtimes, and every report body carries "
                    f"`entry-files=` and `newest=` — so this read's golden would "
                    f"depend on what ran before it. Move it into the read phase."
                )

    if not corpus.uniform_401:
        raise CorpusError("the uniform_401 relation names no cases")
    for case_id in corpus.uniform_401:
        corpus.by_id(case_id)
    if not corpus.head_pairs:
        raise CorpusError("no HEAD/GET pairs are declared")
    for head_pair in corpus.head_pairs:
        head = corpus.by_id(head_pair.head)
        get = corpus.by_id(head_pair.get)
        if head.method != "HEAD" or get.method != "GET":
            raise CorpusError(
                f"head pair {head_pair.name!r}: {head.id!r} must be a HEAD and "
                f"{get.id!r} a GET, got {head.method} and {get.method}"
            )
        if head.target != get.target:
            raise CorpusError(
                f"head pair {head_pair.name!r}: the two cases address different "
                f"targets ({head.target!r} vs {get.target!r}), so their "
                f"Content-Lengths have no reason to agree and the claim is empty"
            )
    if not corpus.scope_pairs:
        raise CorpusError("no refused-vs-absent pairs are declared")
    for pair in corpus.scope_pairs:
        refused = corpus.by_id(pair.refused)
        absent = corpus.by_id(pair.absent)
        if refused.principal != absent.principal:
            raise CorpusError(
                f"pair {pair.name!r}: the two cases use different principals "
                f"({refused.principal!r} vs {absent.principal!r}). A refusal and an "
                f"absence are only comparable for ONE caller — a wide principal's "
                f"answer for a scope it CAN see is a third thing entirely."
            )
        if pair.refused_scope not in (refused.target or ""):
            raise CorpusError(
                f"pair {pair.name!r}: refused_scope {pair.refused_scope!r} does not "
                f"appear in {refused.id!r}'s target"
            )
        if pair.absent_scope not in (absent.target or ""):
            raise CorpusError(
                f"pair {pair.name!r}: absent_scope {pair.absent_scope!r} does not "
                f"appear in {absent.id!r}'s target"
            )

    # 🔴 THE NEGATIVE SET IS CHECKED **BEFORE** COVERAGE, AND THE ORDER IS WHAT
    # MAKES THIS GUARD REACHABLE. A `negative_route` row EXCLUDES its pair from
    # the coverage set, so the day the server starts routing that pair the
    # coverage check below fires first — with the wrong sentence ("you never
    # address it") for a corpus that addresses it and calls it a non-route. The
    # specific diagnosis has to come first or it never runs at all: measured, by
    # asserting on this message and watching the other one arrive.
    #
    # The check itself closes the hole in the ledger: without it a row could be
    # marked `negative_route` for a pair the server DOES route, and that pair
    # would be excluded from the coverage set while looking deliberate.
    for case in corpus.cases:
        if not case.negative_route:
            continue
        named = _route_of(case)
        if named is None:
            raise CorpusError(
                f"case {case.id!r} is marked negative_route but addresses no "
                f"/api/v1/ route at all, so the mark claims nothing"
            )
        if named in declared:
            raise CorpusError(
                f"case {case.id!r} is marked negative_route for {named!r}, and the "
                f"server DOES declare that route. The mark excludes the row from "
                f"the coverage ledger, so leaving it here would hide a real route "
                f"behind a row that looks deliberate."
            )

    uncovered = sorted(declared - addressed_routes(corpus))
    if uncovered:
        raise CorpusError(
            "the server declares routes this request list never addresses: "
            + ", ".join(uncovered)
            + ". A suite that replays a recorded list is STRUCTURALLY BLIND to a "
            "route added after the list was written — this guard is what makes it "
            "a ledger instead. Add a row per principal shape for each, then "
            "regenerate."
        )
    stale = sorted(addressed_routes(corpus) - declared)
    if stale:
        raise CorpusError(
            "the request list addresses routes the server no longer declares: "
            + ", ".join(stale)
            + ". The ledger fails when the set SHRINKS as well as when it grows; a "
            "row for a deleted route records a 404 as if it were the contract."
        )


# ---------------------------------------------------------------------------
# The route ledger.
# ---------------------------------------------------------------------------
#
# 🔴 A ROUTE NAME HERE IS `<METHOD> <HEAD>` — the method and the first path
# component after the API prefix — because that pair is what the server's own
# two tables are keyed on. `API_ROUTES` is keyed on the head alone and reached
# only by GET/HEAD; `WRITE_ROUTES` is keyed on `(method, head)`. Flattening both
# into one vocabulary is what lets one ledger cover the read and write surfaces
# without a second spelling of "is this a write".


def declared_routes(source: Path | None = None) -> set[str]:
    """Every `<METHOD> <head>` the oracle's source declares a route for.

    🔴 READ BY AST FROM `server/server.py`, NOT BY IMPORTING IT. Importing would
    put the implementation inside the suite that exists to measure it from
    outside; `tests/testlib/cairn_source.py` reads the client's constants the
    same way, for the same reason.

    ⚠ WHAT THIS CANNOT SEE, stated rather than assumed away, because the guard
    that reads it is the corpus's only defence against a route added tomorrow:

      * a route dispatched from anywhere OTHER than a module-level
        `API_ROUTES = {...}` / `WRITE_ROUTES = {...}` dict literal. The server
        documents those two tables as the only dispatch, and a test in
        `tests/test_subsystem_store_api.py` pins `set(API_ROUTES)` against its
        own ledger — but an `if path == "/api/v1/x"` added beside them would be
        invisible here.
      * a route added to a NON-PYTHON implementation. There is no source for
        this function to read, so for the Go server this blind spot re-opens and
        has to be closed on that side. `README.md` says so under "what this
        suite cannot see".
      * the non-API paths: `/healthz` and the uniform-401 catch-all are not
        rows in either table and are covered by ordinary cases instead.
    """
    tree = ast.parse((source or SERVER_SOURCE).read_text(encoding="utf-8"))
    read_heads: set[str] = set()
    write_routes: set[str] = set()
    for node in tree.body:
        targets: list[ast.expr] = []
        if isinstance(node, ast.AnnAssign):
            targets = [node.target]
        elif isinstance(node, ast.Assign):
            targets = list(node.targets)
        if not targets or not isinstance(node.value, ast.Dict):
            continue
        names = {t.id for t in targets if isinstance(t, ast.Name)}
        if "API_ROUTES" in names:
            for key in node.value.keys:
                if isinstance(key, ast.Constant) and isinstance(key.value, str):
                    read_heads.add(key.value)
        if "WRITE_ROUTES" in names:
            for key in node.value.keys:
                if isinstance(key, ast.Tuple) and len(key.elts) == 2:
                    method, head = key.elts
                    if (
                        isinstance(method, ast.Constant)
                        and isinstance(head, ast.Constant)
                    ):
                        write_routes.add(f"{method.value} {head.value}")
    if not read_heads:
        raise CorpusError(
            f"no API_ROUTES dict literal found in {source or SERVER_SOURCE}. A "
            f"route ledger that discovers NOTHING reports full coverage over an "
            f"empty set, which is the reassuring zero this guard exists to avoid."
        )
    # Every read head is reachable by both safe methods the server allows.
    return {f"{verb} {head}" for head in read_heads for verb in ("GET", "HEAD")} | write_routes


def _route_of(case: Case) -> str | None:
    """`<METHOD> <head>` for one case, or `None` if it addresses no API route."""
    prefix = "/api/v1/"
    if case.is_raw or not case.target or not case.method:
        return None
    path = case.target.split("?", 1)[0]
    if not path.startswith(prefix):
        return None
    parts = [p for p in path[len(prefix):].split("/") if p]
    return f"{case.method} {parts[0]}" if parts else None


def addressed_routes(corpus: Corpus) -> set[str]:
    """`<METHOD> <head>` for every case that addresses an `/api/v1/` route.

    Derived from the declared target, so a row cannot claim coverage it does not
    exercise. Cases aimed at `/healthz`, at a non-API path, or at a path with no
    head contribute nothing — which is correct: they cover no route. So do rows
    marked `negative_route`, which exist to pin what a NON-route answers.
    """
    out: set[str] = set()
    for case in corpus.cases:
        if case.negative_route:
            continue
        named = _route_of(case)
        if named is not None:
            out.add(named)
    return out
