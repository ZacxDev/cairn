#!/usr/bin/env python3
"""The dual-run gate DECLARES a lot; this is what makes the declarations load-bearing.

🔴 THE GATE ITSELF IS NOT RUN HERE, AND THAT IS DELIBERATE — the same ruling
`tests/test_parity_harness.py` makes for the same reason. It starts two servers, builds a Go
binary and issues about 1,300 comparisons; folding that into the unit suite would put a `go`
toolchain in the `tests` job's dependencies and make one collected-test floor answer two
questions. The gate's own verdict is read in its own CI job, exactly as the conformance
corpus's and the parity harness's are.

🔴 WHAT *IS* CHECKED HERE IS THE SHAPE OF THE CLAIM, BECAUSE A GATE'S COVERAGE IS A CLAIM
TOO. A target matrix can silently stop addressing a route, an arm can lose its negative
control, a declared licence to differ can arrive with no reason, and a mutation pattern can
stop matching — none of which the gate's own green would notice, since a gate that measures
fewer things passes more easily.

⚠ MOST OF THESE ARE INVARIANT GUARDS, NOT REGRESSION COVERAGE, and they say which. FOUR are
the exceptions, each RED on the harness as first written:

  * `test_every_target_id_is_unique` — the per-entry sweep's ids did not carry the principal,
    so the headline difference count under-reported 316 differing targets as 313;
  * `test_this_file_imported_the_DUALRUN_harness_and_not_the_PARITY_one` — a bare
    `import harness` collided with `tests/parity/harness.py` and the parity ledger went
    2 failed / 6 errors;
  * `test_the_write_phase_addresses_a_SUCCESS_on_both_halves_of_the_PUT` — every PUT target
    sent an empty body, so the gate never saw a 201 or a 200 replace;
  * `test_the_write_phase_compares_REFUSED_against_ABSENT_on_both_principals` — the
    refused-scope row was sent by the principal that can see every scope, so it answered
    `200 appended`.

`tests/dualrun/README.md` records each with its matrix.
"""
from __future__ import annotations

import importlib.util
import re
import sys
from pathlib import Path

import pytest

ROOT = Path(__file__).resolve().parents[1]
GATE_DIR = ROOT / "tests" / "dualrun"
sys.path.insert(0, str(ROOT / "tests"))


def _load(name: str, path: Path):
    """Import a module under a UNIQUE name, from an explicit file path.

    🔴 A BARE `import harness` HERE BROKE `tests/test_parity_harness.py`, AND THAT IS
    MEASURED RATHER THAN HYPOTHETICAL. Both gates call their entry point `harness.py`, both
    test files put their own directory on `sys.path` and `import harness`, and `sys.modules`
    holds exactly ONE module called `harness` — so whichever file pytest collected first won,
    and the other silently got the wrong module. Under `-p no:randomly` that is this file,
    and the parity ledger went **2 failed, 6 errors** with `AttributeError`s about attributes
    the dual-run harness does not have.

    ⚠ THE FAILURE MODE THAT MATTERS IS THE ONE THAT DID NOT HAPPEN: had the order been the
    other way round, THIS file would have imported the parity harness and its guards would
    have measured the wrong gate — possibly while still passing, since some of them only
    look for the presence of an attribute. A collision on a module name is not a naming
    nit; it is a silent substitution of the thing under test.

    So nothing here relies on `sys.path` order: the modules are loaded from their paths under
    names that cannot collide, and `sys.modules["harness"]` is left alone for the parity
    ledger to claim.
    """
    spec = importlib.util.spec_from_file_location(name, path)
    assert spec is not None and spec.loader is not None, f"cannot load {path}"
    module = importlib.util.module_from_spec(spec)
    sys.modules[name] = module
    spec.loader.exec_module(module)
    return module


# `harness.py` puts its own directory on `sys.path` when it executes, so its bare
# `import genstore` / `import mutants` resolve; those two names are unique in this tree.
H = _load("cairn_dualrun_harness", GATE_DIR / "harness.py")
genstore = _load("cairn_dualrun_genstore", GATE_DIR / "genstore.py")
M = _load("cairn_dualrun_mutants", GATE_DIR / "mutants.py")


def test_this_file_imported_the_DUALRUN_harness_and_not_the_PARITY_one():
    """🔴 THE GUARD OVER THE IMPORT ABOVE, BECAUSE THE COLLISION WAS SILENT ONE WAY ROUND.

    Two gates both call their entry point `harness.py`. If this file ever went back to a bare
    `import harness`, it would import whichever one pytest happened to load first — and when
    that is the parity harness, the guards below measure the wrong gate. Some of them would
    still pass, because a missing attribute is an error and a *present* one is not proof it
    came from the right module. This asserts the file on disk.
    """
    assert Path(H.__file__).resolve() == (GATE_DIR / "harness.py").resolve(), (
        f"the module under test is {H.__file__}, not {GATE_DIR / 'harness.py'} — the "
        f"`harness` module-name collision has re-opened and these guards are measuring "
        f"the wrong gate")
    assert Path(M.__file__).resolve() == (GATE_DIR / "mutants.py").resolve()
    assert Path(genstore.__file__).resolve() == (GATE_DIR / "genstore.py").resolve()
    # …and the parity harness is still importable under its own name, which is the half that
    # says this file did not take the name away from it.
    sys.path.insert(0, str(ROOT / "tests" / "parity"))
    try:
        parity = importlib.import_module("harness")
    finally:
        sys.path.remove(str(ROOT / "tests" / "parity"))
    assert Path(parity.__file__).resolve() == (ROOT / "tests" / "parity" / "harness.py"), (
        f"`import harness` resolves to {parity.__file__}, so `tests/test_parity_harness.py` "
        f"would import the wrong module")


# ---------------------------------------------------------------------------
# The target matrix against the ROUTE LEDGER, discovered rather than listed
# ---------------------------------------------------------------------------

@pytest.fixture(scope="module")
def targets() -> list:
    """Every target the gate builds, over a synthetic scope/ref shape.

    The store is not built and no server is started: the matrix is a pure function of the
    scope list and the ref list, which is exactly what makes it checkable here.
    """
    scopes = ["alpha-index", "beta-index", "theta-ambiguous"]
    narrow = ("beta-index",)
    out: list = []
    for scope in scopes:
        out += H.scope_targets(scope)
        out += H.narrowing_targets(scope, narrow)
    out += H.store_targets(scopes, narrow)
    for scope in scopes:
        out += H.entry_targets(scope, [f"{scope}-one", f"{scope}-two"])
        out += H.search_by_ref_targets(scope, [f"{scope}-one", f"{scope}-two"])
    for scope in narrow:
        out += H.entry_targets(scope, [f"{scope}-one", f"{scope}-two"], H.NARROW)
    out += H.write_targets("alpha-index", "alpha-index-one",
                           "dualrun-scope-that-never-existed", "theta-ambiguous",
                           "dualrun-created-entry")
    return out


def _route_of(target) -> str | None:
    """`<METHOD> <head>` for a target that addresses an API route, else None."""
    path = target.path.split("?", 1)[0]
    if not path.startswith("/api/v1/"):
        return None
    parts = [p for p in path[len("/api/v1/"):].split("/") if p]
    if not parts:
        return None
    return f"{target.method} {parts[0]}"


def test_every_DECLARED_ROUTE_is_addressed_by_at_least_one_target(targets):
    """🔴 THE GATE'S ROUTE COVERAGE, IN BOTH DIRECTIONS, AGAINST A DISCOVERED SET.

    A route with no target is a route whose two implementations this gate never compares —
    and "all 8 dispatch entries are covered" is a sentence, not a measurement, until
    something reads the dispatch tables. It reads the ORACLE's two module-level dicts by
    AST, which is `tests/conformance/cases.declared_routes`; the Go side's equivalent is
    `api.DeclaredRoutes()`, checked in the `go` job because it needs a compiler.

    The other direction matters too: a target addressing a head no table dispatches would be
    a target that can only ever compare two `no-route` answers.
    """
    from conformance import cases as cases_mod

    declared = cases_mod.declared_routes()
    # The positive control on the discovery itself: an empty or tiny set would make this
    # guard pass over nothing, which is the shape it exists to refuse.
    assert len(declared) == 8, (
        f"the oracle declares {len(declared)} routes, and 8 were measured "
        f"(GET/HEAD x recall/search/snapshot, POST entry, PUT entry): {sorted(declared)}")

    addressed = {r for r in (_route_of(t) for t in targets) if r}
    missing = declared - addressed
    assert not missing, (
        f"{len(missing)} declared route(s) are addressed by NO target, so nothing compares "
        f"the two servers on them: {sorted(missing)}")

    # 🔴 THE OTHER DIRECTION IS NOT "NOTHING ELSE", AND THE FIRST DRAFT OF THIS GUARD SAID
    # SO AND WAS WRONG. Three targets deliberately address heads no table dispatches, and the
    # thing they compare is exactly the REFUSAL: `GET .../telepathy` must be the same
    # `no-route` on both, `PATCH .../recall/...` the same answer to an unhandled verb, and
    # `POST /api/v1/snapshot` the same 405 with the same `Allow`. Demanding an empty set
    # would have deleted three refusal comparisons to satisfy a sentence.
    #
    # They are ALLOWLISTED BY NAME rather than the check being dropped, so a genuine typo in
    # a head — the thing this direction exists to catch — still fails.
    deliberate_non_routes = {
        "GET telepathy": "no-route: a path inside the prefix that no table dispatches",
        "PATCH recall": "an unhandled verb, which must be the uniform refusal and metered",
        "POST snapshot": "a write verb on a read-only route: 405 with `Allow`",
    }
    extra = addressed - declared - set(deliberate_non_routes)
    assert not extra, (
        f"{len(extra)} target(s) address a head no dispatch table declares and that is not "
        f"in the deliberate-refusal allowlist, so they can only ever compare two "
        f"`no-route` answers by accident: {sorted(extra)}")
    # …and every allowlisted refusal really is addressed, so the allowlist cannot rot into a
    # set of exemptions for targets that no longer exist.
    stale = set(deliberate_non_routes) - addressed
    assert not stale, (
        f"the deliberate-refusal allowlist names {sorted(stale)}, which no target addresses "
        f"— an exemption for a comparison that is not being made")


def test_every_QUERY_PARAMETER_the_server_reads_is_sent_by_some_target(targets):
    """🔴 A ROUTE COMPARED AT ONE PARAMETER VALUE IS A ROUTE COMPARED AT ONE VALUE.

    The parameter set is DISCOVERED from the Go server's own reads — `lastValue`, `lastOr`,
    `intParam`, `floatParam` — rather than written here, because a hand list is blind to a
    parameter added after it. Same shape as the Go client's ledger-flag guard, which greps
    `cmd/cairn/main.go` for its dispatch rather than trusting a declaration.

    ⚠ IT IS A REGEX OVER SOURCE, WHICH IS NARROWER THAN AN AST WALK, and the narrowing is
    named: a parameter read through a variable rather than a literal would be invisible. The
    assertion on the discovered set's SIZE below is the positive control — a regex that
    stopped matching would produce a small set and pass this guard over nothing.
    """
    source = (ROOT / "internal" / "api" / "server.go").read_text(encoding="utf-8")
    discovered = set(re.findall(
        r'\b(?:lastValue|lastOr|intParam|floatParam)\(params,\s*"([a-z_]+)"', source))
    assert discovered >= {"mode", "ref", "limit", "page", "q", "threshold", "max_hits",
                          "context", "all_scopes", "scope"}, (
        f"the parameter discovery found {sorted(discovered)}, which is missing one of the "
        f"ten this server is known to read — the regex has stopped matching and this guard "
        f"would pass over nothing")

    sent: dict[str, set[str]] = {}
    for target in targets:
        _, _, query = target.path.partition("?")
        for pair in query.split("&"):
            name, _, value = pair.partition("=")
            if name:
                sent.setdefault(name, set()).add(value)
    missing = discovered - set(sent)
    assert not missing, (
        f"the server reads {sorted(missing)} and NO target sends it, so the two "
        f"implementations' parsing of it is never compared")

    # 🔴 …AND AT MORE THAN ONE VALUE, WHICH IS WHAT "VARY THE PARAMETER" MEANS. A mutation
    # sweep over this guard moved `threshold=0.3` to `0.6` — the DEFAULT, so the parameter
    # stops changing anything — and the guard SURVIVED, because its sentence was only about
    # the name appearing. A parameter sent at one value compares its PARSING and nothing
    # about its EFFECT, and the two renderers are what this gate exists to compare.
    #
    # ⚠ WHAT THIS STILL DOES NOT SEE, stated rather than assumed away: whether either value
    # differs from the SERVER'S DEFAULT. Reading the defaults would mean a second discovery
    # mechanism over `internal/report/report.go`, and two distinct values already make the
    # parameter's effect observable — at most one of them can be the default.
    for name, values in sorted(sent.items()):
        if name not in discovered:
            continue
        assert all(v != "" for v in values), (
            f"`{name}` is sent with an EMPTY value by some target; a present-but-empty "
            f"parameter is how a required one is most often lost, and it must be a "
            f"deliberate target rather than the only way this parameter is exercised")
        assert len(values) >= 2, (
            f"`{name}` is only ever sent as {sorted(values)} — ONE value. That compares the "
            f"two implementations' parsing of it and nothing about its effect on the "
            f"rendered output, which is the half a shared renderer would not give you.")


def test_every_target_id_is_unique(targets):
    """🔴 NOT AN INVARIANT GUARD — THIS ONE IS RED ON THE HARNESS AS FIRST WRITTEN.

    The headline difference count is `len(set(failures))`, so two targets sharing an id
    collapse into one. Measured: the per-entry sweep's ids did not carry the principal, the
    narrowed principal addresses the same `<scope>/<ref>` pairs as the wide one, and a mutant
    that made ALL 316 targets differ was reported as **313**. An id collision under-reports
    the number the whole gate is read through and makes `--only` ambiguous about which
    principal it selected.
    """
    seen: dict[str, int] = {}
    for target in targets:
        seen[target.id] = seen.get(target.id, 0) + 1
    clashes = sorted(name for name, count in seen.items() if count > 1)
    assert not clashes, (
        f"{len(clashes)} target id(s) are not unique, so the difference count would "
        f"under-report and `--only` would be ambiguous: {clashes}")
    # The positive control: the fixture really does build the colliding SHAPE — the same
    # scope and ref addressed by two principals — so a run of this guard over a matrix that
    # happened not to contain one would be vacuous.
    entries = [t for t in targets if t.arm == "entry"]
    pairs = {(t.path, t.method) for t in entries}
    assert len(pairs) < len(entries), (
        "no two per-entry targets share a path, so this guard measured nothing about the "
        "collision it exists for")


def test_the_write_phase_addresses_a_SUCCESS_on_both_halves_of_the_PUT(targets):
    """🔴 A WRITE ROUTE COMPARED ONLY AT ITS REFUSALS IS A WRITE ROUTE COMPARED AT ITS
    REFUSALS, AND THAT IS WHAT THE FIRST DRAFT DID.

    Every PUT target sent an EMPTY body, so `create-new` answered **422 entry-shape** on both
    servers: the gate never saw a 201, never saw a 200 `replaced`, and never compared an ETag
    over content it had put there. Two servers agreeing on a 422 is a real comparison of the
    refusal and says nothing about the write.

    ⚠ THIS IS THE STRUCTURAL HALF ONLY — it asserts that a conformant body and a derived
    precondition are ADDRESSED. Whether they actually land is the runtime `writes-landed`
    floor, which names `appended`, `replaced` and `created` and refuses to vouch without all
    three on both servers. A structural check type-checks past a wrong argument; the pair is
    what covers it.
    """
    writes = [t for t in targets if t.phase == "write"]
    assert writes, "no write target at all"
    puts = [t for t in writes if t.method == "PUT"]
    assert puts, "no PUT target"
    # A conformant body for the replace and the create, not `b""`.
    for target in puts:
        assert target.body, f"{target.id} sends no body at all"
        assert b"## What it is" in target.body, (
            f"{target.id} sends a body the entry-shape validator will refuse, so it can only "
            f"ever compare a 422")
    derived = [t for t in puts if t.derive_if_match]
    assert len(derived) == 1, (
        f"{len(derived)} PUT target(s) derive their `If-Match` from the current revision. "
        f"Without exactly one, no target can REPLACE anything: a hardcoded revision is stale "
        f"by construction and answers 412")
    creates = [t for t in puts if t.headers.get("If-None-Match") == "*"]
    assert len(creates) >= 2, (
        "the create half of `PUT entry` needs BOTH a ref that does not exist (201) and one "
        f"that does (412); found {len(creates)} `If-None-Match: *` target(s)")


def test_the_write_phase_compares_REFUSED_against_ABSENT_on_both_principals(targets):
    """🔴 REFUSED MUST ANSWER WHAT ABSENT ANSWERS, AND THE PAIR IS THE CLAIM.

    The first draft's `append-refused-scope` used the first scope outside the NARROWED
    allowlist — but sent it as the WIDE principal, which can see every scope. It answered
    `200 appended`, so the row named a refusal and compared a successful write. A target that
    cannot produce the state it is named for is worse than no target: it reads as coverage.
    """
    ids = {t.id for t in targets if t.phase == "write"}
    for needed in ("append-absent-scope", "append-narrow-refused-scope",
                   "append-narrow-absent-scope"):
        assert needed in ids, f"the write phase no longer addresses {needed!r}"
    by_id = {t.id: t for t in targets}
    # The refused row must be sent by the NARROWED principal, or it is not a refusal at all.
    assert by_id["append-narrow-refused-scope"].principal == H.NARROW
    assert by_id["append-absent-scope"].principal == H.WIDE
    # …and the two narrow rows must address DIFFERENT scopes, or the pair is one case twice.
    assert (by_id["append-narrow-refused-scope"].path
            != by_id["append-narrow-absent-scope"].path)


def test_every_narrower_comparison_mode_states_WHY_it_is_narrower(targets):
    """A target compared less than fully is a target whose bytes are allowed to differ."""
    for target in targets:
        if target.compare == H.CMP_ALL:
            continue
        assert len(target.why) > 40, (
            f"{target.id} is compared as {target.compare!r} — narrower than the full byte "
            f"comparison — with no stated reason. The reason has to sit beside it or the "
            f"gate quietly shrinks.")


def test_every_target_declares_an_ARM_the_harness_has(targets):
    """An arm nobody named is an arm the self-test cannot attribute a kill to."""
    known = {"scope", "entry", "narrow", "tar", "write", "refusal"}
    for target in targets:
        assert target.arm in known, f"{target.id} declares arm {target.arm!r}"
    covered = {t.arm for t in targets}
    assert covered == known, f"arms with no target at all: {sorted(known - covered)}"


# ---------------------------------------------------------------------------
# The declared licences to differ
# ---------------------------------------------------------------------------

def test_every_declared_difference_carries_a_reason_and_a_place():
    rows = H.differences()
    assert len(rows) >= 5, f"the licence table shrank to {len(rows)} rows"
    for name, row in rows.items():
        assert row.name == name, f"{name!r} is keyed under a different name than it carries"
        assert len(row.why) > 80, (
            f"the licence {name!r} has no substantive reason. A licence to differ that "
            f"nobody justified is a comparison quietly switched off.")
        assert row.where, f"the licence {name!r} does not say WHERE it applies"
        assert not row.fired, "a freshly built licence must not claim to have fired"


def test_the_gzip_envelope_licence_names_the_measurement_it_rests_on():
    """🔴 THE ONE LICENCE THAT WOULD OTHERWISE READ AS A CONCESSION.

    Byte-identity is scoped to the UNCOMPRESSED tar because gzip identity is measured
    unattainable, and a reader who finds this licence has to be able to tell that from
    "nobody looked". Pinned on the SUBSTANCE — both independent reasons — rather than on a
    word, because a guard on a word is walkable by rewording.
    """
    why = H.differences()["snapshot-gzip-envelope"].why
    for phrase in ("0xff", "DEFLATE", "LENGTH", "compress/flate", "byte for byte"):
        assert phrase in why, (
            f"the gzip licence no longer states {phrase!r}. It rests on TWO independent "
            f"measurements — the hardcoded OS byte and the differing stream length — and a "
            f"licence that states neither reads as a concession.")


# ---------------------------------------------------------------------------
# The negative controls
# ---------------------------------------------------------------------------

def test_every_mutation_pattern_occurs_EXACTLY_ONCE_in_its_target():
    """🔴 THE FALSE `SURVIVED` A NEGATIVE CONTROL CANNOT AFFORD.

    A `replace` that matched nothing yields an UNMUTATED server, the gate passes, and the
    run is reported as "this mutation was not caught" — the mutation sweep's classic false
    SURVIVED. `mutated_server` asserts this at apply time; this asserts it without starting
    anything, so an ordinary refactor of `server.py` fails in the unit suite rather than
    thirty seconds into a CI job.
    """
    for mutation in M.MUTATIONS:
        path = M.SERVER_PY if mutation.target == "server" else M.LIB / mutation.target
        assert path.is_file(), f"{mutation.name} names {path}, which does not exist"
        found = path.read_text(encoding="utf-8").count(mutation.old)
        assert found == 1, (
            f"the pattern for {mutation.name!r} occurs {found} time(s) in {path.name}, not "
            f"1. At 0 the mutant is scored SURVIVED against an unmutated server; above 1 "
            f"the edit is not the one the mutation describes.\n  pattern: {mutation.old!r}")
        assert mutation.new != mutation.old, f"{mutation.name} replaces text with itself"


def test_every_ARM_has_a_negative_control():
    """🔴 AN ARM NO MUTANT REACHES IS AN ARM NOBODY HAS WATCHED GO RED.

    Six comparisons, and every one of them must be the declared `kind` of some mutation.
    `process` is the reason this guard exists rather than being assumed: the first six
    mutants all reached the wire or the audit stream, so the startup banner and the
    reader's warning line were compared by an arm with no control at all.
    """
    arms = {"status", "headers", "body", "tar", "audit", "process"}
    covered = {m.kind for m in M.MUTATIONS}
    assert covered == arms, (
        f"arms with no negative control: {sorted(arms - covered)}; "
        f"mutants naming an arm the harness does not have: {sorted(covered - arms)}")


def test_every_mutation_declares_its_expected_comparison_set_and_includes_its_own_arm():
    for mutation in M.MUTATIONS:
        assert mutation.kind in mutation.expect, (
            f"{mutation.name} declares kind {mutation.kind!r} but expects "
            f"{sorted(mutation.expect)}, which does not include it — the two assertions "
            f"would contradict each other and the mutant could never be scored caught")
        assert len(mutation.why) > 80, f"{mutation.name} has no substantive reason"


def test_the_ISOLATED_mutant_names_the_arm_that_makes_the_PER_ENTRY_sweep_load_bearing():
    """🔴 THE ONE CLAIM THAT MAKES AN ARM LOAD-BEARING RATHER THAN MERELY LIVE.

    `recall-full` with a generous `limit` prints every entry's body at scope level, so a pure
    BODY difference is visible without the per-entry sweep. What only that sweep reaches is
    `resolve_ref_tiered`, once per real ref — and the mutant with `only_target_arm="entry"`
    is what turns that argument into a measurement.
    """
    isolated = [m for m in M.MUTATIONS if m.only_target_arm is not None]
    assert isolated, (
        "no mutation names a target arm, so nothing measures that any arm sees something "
        "the others do not — every arm would be defensible as redundant")
    assert {m.only_target_arm for m in isolated} == {"entry"}, (
        f"unexpected isolated arms: {sorted({m.only_target_arm for m in isolated})}")


# ---------------------------------------------------------------------------
# The generated world
# ---------------------------------------------------------------------------

def test_the_generated_world_crosses_the_READERS_OWN_page_cap():
    """🔴 `genstore.LISTING_PAGE_SIZE` IS A COPY, SO SOMETHING MUST COMPARE IT TO THE REAL ONE.

    A generator whose paginated scope fell under a raised cap would produce an UNPAGINATED
    scope while its own docstring still promised a paginated one — coverage in name only, and
    invisible because nothing about the run would change shape.
    """
    sys.path.insert(0, str(ROOT / "lib"))
    import subsystem_recall as rc

    assert genstore.page_cap_is_crossed(rc.LISTING_PAGE_SIZE), (
        f"the generator builds {genstore.LISTING_PAGE_SIZE + 1} entries into its paginated "
        f"scope and the reader's cap is {rc.LISTING_PAGE_SIZE}, so the index does NOT "
        f"paginate and every pagination branch is unserved while looking covered")


def test_the_generated_world_is_synthetic_and_dated_year_2000(tmp_path):
    """🔴 THIS REPOSITORY IS PUBLIC. The harness ships and the operator's data never does, so
    the generated world must be buildable, self-contained and free of anything real — which
    `tests/leakscan.py` also enforces over the source. This checks the BUILT tree, which the
    scanner never sees.
    """
    root = genstore.build_store(tmp_path / "store")
    files = sorted(p for p in root.rglob("*.md") if p.is_file())
    assert len(files) > 100, f"the generated world built {len(files)} entry files"
    date = re.compile(r"\b(\d{4})-\d{2}-\d{2}\b")
    for path in files:
        for year in date.findall(path.read_text(encoding="utf-8")):
            assert year == "2000", (
                f"{path.relative_to(root)} carries a {year} date. Fixtures that need a date "
                f"use an obviously-synthetic year 2000 one, which is what makes a real date "
                f"in here unambiguous.")
    # Determinism, which is what makes a failure reproduce from the seed alone. Measured on
    # the two dimensions a second build can move: the file set, and every mtime.
    again = genstore.build_store(tmp_path / "again")
    left = {p.relative_to(root): (p.read_bytes(), p.stat().st_mtime_ns)
            for p in root.rglob("*") if p.is_file()}
    right = {p.relative_to(again): (p.read_bytes(), p.stat().st_mtime_ns)
             for p in again.rglob("*") if p.is_file()}
    assert left == right, (
        "two builds from one seed produced different stores, so a failure cannot be "
        "reproduced from the seed and the mtime-derived order is not deterministic")


def test_the_generated_world_holds_the_SHAPES_the_gates_arms_need(tmp_path):
    """A ledger over the generated store, keyed on the shape each arm depends on.

    🔴 IT IS A LEDGER RATHER THAN A COUNT, because every one of these is what makes some
    comparison non-vacuous: without the ambiguous bare ref the resolver mutant is a no-op,
    without a non-ASCII or over-100-byte member name the snapshot never emits a PAX `path`
    record, without a non-git scope the empty `X-Store-Revision` is never served, and
    without an mtime tie neither tie-break runs.
    """
    root_source = (GATE_DIR / "genstore.py").read_text(encoding="utf-8")
    for needed in ("theta-ambiguous/plum.md", "theta-ambiguous/plum.process.md",
                   "NON_ASCII_STEM", "LONG_STEM", "kappa-tied/tied-alpha.md",
                   "kappa-tied/tied-zulu.md", "EMPTY_SCOPE", "RUBBLE_SCOPE",
                   "GIT_SCOPES", "iota-single/lonely-one.md"):
        assert needed in root_source, f"the generated world no longer declares {needed!r}"
    # 🔴 …AND THE REVISION CLAIM IS A RELATIONSHIP, NOT A PRESENCE, so it is asserted over
    # the BUILT tree and in BOTH directions. A world where every scope is a git repo never
    # serves the empty `X-Store-Revision`; a world where none is never serves a revision.
    # Checking only `GIT_SCOPES` is non-empty would be the guard-narrower-than-its-sentence
    # shape: it reads as "both shapes are present" and measures one of them.
    root = genstore.build_store(tmp_path / "revision-shapes")
    with_git = [s for s in genstore.scopes(root) if (root / s / ".git" / "HEAD").is_file()]
    without_git = [s for s in genstore.scopes(root)
                   if not (root / s / ".git" / "HEAD").is_file()]
    assert with_git and without_git, (
        f"the generated world must hold a scope that IS a git repo and one that is NOT, "
        f"because `X-Store-Revision` is a different code path either way: "
        f"with-git={with_git} without-git={without_git}")
