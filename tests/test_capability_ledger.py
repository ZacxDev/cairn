#!/usr/bin/env python3
"""The capability ledger is a GATE, and this file is what makes it one.

`testlib/capability_ledger.py` declares what cairn can do and on which surfaces.
Nothing about a declaration is load-bearing until something fails when it stops
being true, so every assertion here compares the ledger against a surface
DISCOVERED from the code — the CLI's own parser and `server.py`'s own dispatch
tables — and fails in BOTH directions: a verb or a route that appears without a
row, and a row whose verb or route has been deleted.

🔴 WHAT EACH GUARD OWNS, because a message that claims the wrong thing is how a
reader stops looking:

  * `TestTheRowShapeIsEnforced` proves the VALIDATOR is live. Those are invariant
    guards — no bug ever wrote a row with an unexplained absence, because the
    dataclass has refused one since the hour it was written. They are not
    regression coverage, and they are not the parity gate either: the parity rule
    ("a capability reachable over HTTP must be reachable from the CLI or say why")
    is enforced at CONSTRUCTION, so what a test can add is proof that the
    enforcement still runs.
  * `TestTheDiscoveryInstruments` validates the instruments the gate reads. A
    discovery that silently returns an empty or truncated set makes every
    "every row is accounted for" assertion below pass vacuously, so each
    mechanism is fed a case that MUST produce a non-zero count and a case that
    MUST produce zero, and the pair is reported.
  * `TestTheCliVerbTableMatchesTheLedger` and
    `TestTheHttpRouteTableMatchesTheLedger` are the gate. Their floors are BARE
    non-emptiness — deliberately not a count of today's verbs, because a
    `len >= 9` floor would fire FIRST when a verb is deleted and answer the
    growth question with a message about vacuity. The vacuity claim belongs to
    the instrument tests; these own the set claim.

🔴 A SET THIS FILE DOES NOT OWN. The bound HTTP method surface — `do_GET`,
`do_HEAD`, the four mutating verbs, and any `do_OPTIONS` somebody adds — is
pinned by `test_subsystem_store_api.py::TestPhaseOneScope::
test_the_verb_ledger_is_the_whole_bound_verb_surface`. A method with no row in a
dispatch table carries no capability and is invisible here.
"""

from __future__ import annotations

import ast
from types import SimpleNamespace

import pytest

from testlib import cairn_source
from testlib.capability_ledger import (
    LEDGER,
    READS,
    WRITES,
    Capability,
    cli_verbs_from_parser,
    http_routes,
    ledger_routes,
    normalise_path,
)


# =============================================================================
# The row shape. INVARIANT GUARDS: they pin what `Capability.__post_init__` has
# refused since it was written, not a defect anything ever shipped.
# =============================================================================

class TestTheRowShapeIsEnforced:
    """Invariant guards over the validator that makes the ledger's asymmetries
    honest. Not regression coverage — no bug ever violated these; they exist so
    that deleting the validator fails a test instead of silently allowing a row
    that records a missing surface without recording why."""

    def test_a_row_with_no_cli_verb_must_say_why(self):
        with pytest.raises(ValueError, match="must say why"):
            Capability(name="x", effect=READS, cli=None, route=("GET", "/x"))

    def test_a_row_WITH_a_cli_verb_must_not_explain_an_absence(self):
        """The other direction, which matters more than it looks: a stale
        `no_cli_because` left behind when a verb was added is a row that reads as
        a declared gap while the gap is closed."""
        with pytest.raises(ValueError, match="must say why"):
            Capability(
                name="x", effect=READS, cli="x", route=("GET", "/x"),
                no_cli_because="stale",
            )

    def test_a_row_with_no_route_must_say_why(self):
        with pytest.raises(ValueError, match="no route must say why"):
            Capability(name="x", effect=READS, cli="x", route=None)

    def test_a_row_reachable_on_NO_surface_is_refused(self):
        with pytest.raises(ValueError, match="reachable on NO surface"):
            Capability(
                name="x", effect=READS, cli=None, route=None,
                no_cli_because="a", no_route_because="b",
            )

    def test_the_effect_axis_takes_only_reads_or_writes(self):
        with pytest.raises(ValueError, match="is not reads/writes"):
            Capability(name="x", effect="maybe", cli="x", route=("GET", "/x"))

    def test_every_capability_name_is_unique(self):
        names = [row.name for row in LEDGER]
        assert len(names) == len(set(names)), (
            f"two rows share a name: {sorted({n for n in names if names.count(n) > 1})}"
            f" — the name is the capability's identity, so a duplicate makes one "
            f"of the two unaddressable"
        )

    def test_no_cli_verb_is_claimed_by_two_rows(self):
        verbs = [row.cli for row in LEDGER if row.cli is not None]
        assert len(verbs) == len(set(verbs)), (
            f"a verb appears on two rows: "
            f"{sorted({v for v in verbs if verbs.count(v) > 1})}. One verb cannot "
            f"be two capabilities; a route can be (see the PUT row), a verb cannot."
        )

    def test_a_route_named_by_more_than_one_row_says_what_discriminates_them(self):
        """🔴 THE ASYMMETRY THAT IS ALLOWED, KEPT HONEST. `PUT …/entry/<scope>/
        <ref>` carries both `replace-entry` and `create-entry`; `server.py` says
        that is deliberate. What must not happen is a second row quietly landing
        on an existing route with nothing saying how a caller picks between them.
        """
        by_route: dict[tuple[str, str], list[Capability]] = {}
        for row in LEDGER:
            if row.route is not None:
                key = (row.route[0], normalise_path(row.route[1]))
                by_route.setdefault(key, []).append(row)
        shared = {k: v for k, v in by_route.items() if len(v) > 1}
        for key, rows in shared.items():
            for row in rows:
                assert row.note, (
                    f"{row.name} shares {key} with "
                    f"{[r.name for r in rows if r is not row]} and its `note` is "
                    f"empty — a reader cannot tell which of the two a request "
                    f"reaches, and nothing else in the row says"
                )
        assert shared, (
            "no route is shared by two rows, so this guard measured nothing this "
            "run. That is not a failure of the ledger — but if the PUT pair was "
            "split into two routes, delete this guard rather than leave it "
            "reading as coverage it no longer provides."
        )

    def test_every_placeholder_in_a_ledger_path_is_NAMED(self):
        """`{scope}`, never a bare `{}`. The guard normalises the names away, so
        they exist purely for a human — which means nothing but this stops the
        column decaying into `/api/v1/entry/{}/{}`."""
        for row in LEDGER:
            if row.route is None:
                continue
            assert "{}" not in row.route[1], (
                f"{row.name}: the path {row.route[1]!r} carries an unnamed "
                f"placeholder. Name it for the reader; the comparison does not "
                f"care, and that is exactly why nothing else would catch this."
            )


# =============================================================================
# The instruments. Validate them before reading their verdicts.
# =============================================================================

class TestTheDiscoveryInstruments:
    """🔴 A REASSURING ZERO IS INDISTINGUISHABLE FROM A HARNESS WIRED TO NOTHING.
    Each discovery below is fed a case that MUST come back non-empty and a case
    that MUST come back empty, and the assertion reports the pair. Without this,
    a walker that returned `set()` for any reason would make every ledger
    comparison in this file pass with nothing on either side.
    """

    #: A synthetic module, not `cairn`: the verbs are invented so they cannot
    #: collide with a real one, and so a walker that hard-coded today's answer
    #: would fail here. Two calls, one of them wrapped the way `cairn` wraps its
    #: shared-argument helper, because that wrapping is the shape a walker looking
    #: only at bare statements would miss.
    SYNTHETIC = (
        "import argparse\n"
        "def build():\n"
        "    sub = argparse.ArgumentParser().add_subparsers()\n"
        "    a = sub.add_parser('ochre-probe')\n"
        "    b = shared(sub.add_parser('lantern-tally'))\n"
        "    return a, b\n"
    )

    def test_the_verb_walker_finds_verbs_that_exist_ONLY_in_a_synthetic_module(self):
        found = cairn_source.subcommand_names(ast.parse(self.SYNTHETIC))
        empty = cairn_source.subcommand_names(ast.parse("x = 1\n"))
        assert (found, empty) == ({"ochre-probe", "lantern-tally"}, set()), (
            f"the verb walker reported {sorted(found)} on a module holding two "
            f"invented verbs and {sorted(empty)} on one holding none. The pair is "
            f"the control: a non-zero count on the positive case proves it can "
            f"see a verb at all, and zero on the negative proves the count is not "
            f"a constant."
        )

    def test_the_verb_walker_REFUSES_a_verb_name_it_cannot_read(self):
        """A dynamic registration is the case where a silent skip would be worst:
        the verb exists, the ledger cannot see it, and the gate reports parity."""
        source = (
            "def build(sub):\n"
            "    for name in ('a', 'b'):\n"
            "        sub.add_parser(name)\n"
        )
        with pytest.raises(AssertionError, match="does not name its verb"):
            cairn_source.subcommand_names(ast.parse(source))

    def test_the_constant_walker_sees_BOTH_assignment_spellings(self):
        """🔴 REGRESSION COVERAGE FOR THE WALKER, NOT AN INVARIANT GUARD — this
        one has been watched red. `module_constants` handled `ast.Assign` alone,
        so `NAME: int = <n>` came back as `{}`: measured on the pre-change walker,
        `EXIT_FOO: int = 11` added to `cairn` was INVISIBLE to every ledger reading
        this function, including the client-vs-doctor exit-code pin whose whole
        job is to notice a new code. `cairn` already uses the annotated spelling
        for two dict constants, so the gap was one int away from being live.
        """
        seen = cairn_source.module_constants(
            ast.parse("PLAIN = 11\nANNOTATED: int = 12\n")
        )
        assert seen == {"PLAIN": 11, "ANNOTATED": 12}, (
            f"the constant walker reported {seen} for a module binding one int "
            f"each way. An absent `ANNOTATED` is the pre-change blindness; an "
            f"absent `PLAIN` means the walker stopped working entirely."
        )

    def test_the_constant_walker_reports_no_value_it_cannot_read(self):
        """The other half of the same claim, and the reason the blindness list in
        `cairn_source`'s header is written out: every shape here is a value this
        walker will NOT guess at. Reporting one wrongly would be worse than
        missing it, because a ledger would then assert against a wrong number."""
        seen = cairn_source.module_constants(
            ast.parse(
                "BASE = 1\n"
                "COMPUTED = BASE + 10\n"   # not a Constant
                "NEGATIVE = -1\n"          # UnaryOp, not a negative Constant
                "TEXT = 'nope'\n"          # a literal, not an int
                "DECLARED: int\n"          # binds nothing at all
                "PAIR_A, PAIR_B = 1, 2\n"  # names visible, values not resolved
            )
        )
        assert seen == {"BASE": 1}, (
            f"the constant walker reported {seen}; only `BASE` is a module-level "
            f"name bound to an int literal. Anything else appearing here is a "
            f"value the walker guessed at."
        )

    def test_exit_constant_discovery_REFUSES_an_EXIT_name_it_cannot_read(self):
        """🔴 THE LOUD HALF. A non-int `EXIT_*` used to vanish into the int
        filter, leaving a caller that cross-checks against the exec'd module's
        `dir()` to mis-diagnose it as "computed or imported". Silence in a
        discovery is the undercount every ledger built on it inherits."""
        with pytest.raises(AssertionError, match="cannot read the value as an int"):
            cairn_source.exit_constant_names(ast.parse('EXIT_FOO = "nope"\n'))
        assert cairn_source.exit_constant_names(
            ast.parse("EXIT_FOO: int = 11\nOTHER = 3\n")
        ) == {"EXIT_FOO"}, "the annotated spelling must be SEEN, not refused"

    #: A synthetic dispatch table. The heads are invented, the arities differ
    #: from every real route's, and the tail is two components rather than one —
    #: so a derivation that special-cased today's shapes cannot satisfy this.
    SYNTHETIC_TABLES = SimpleNamespace(
        HEALTH_PATH="/probe",
        API_PREFIX="/z/v9/",
        API_ROUTES={"tally": ("_tally", 3)},
        WRITE_ROUTES={("POST", "slab"): ("_slab", 5, ("cut", "mark"))},
    )

    def test_the_route_derivation_reconstructs_a_path_from_arity_and_tail(self):
        found = http_routes(self.SYNTHETIC_TABLES)
        assert found == {
            ("GET", "/probe"): READS,
            ("GET", "/z/v9/tally/{}/{}"): READS,
            ("POST", "/z/v9/slab/{}/{}/cut/mark"): WRITES,
        }, (
            f"the route derivation reported {sorted(found)} for a synthetic table "
            f"whose arities are 3 and 5 with a two-component tail. The paths are "
            f"DERIVED from those numbers, so this is what proves the real "
            f"comparison below is checking arity and tail rather than agreeing "
            f"with a hand-copied string."
        )

    def test_the_route_derivation_REFUSES_an_arity_too_small_for_its_tail(self):
        broken = SimpleNamespace(
            HEALTH_PATH="/probe", API_PREFIX="/z/v9/", API_ROUTES={},
            WRITE_ROUTES={("POST", "slab"): ("_slab", 1, ("cut", "mark"))},
        )
        with pytest.raises(AssertionError, match="leaves no room for the head"):
            http_routes(broken)

    def test_the_subparsers_discovery_refuses_a_parser_it_cannot_read(self):
        """The private-API risk named in `cli_verbs_from_parser`, measured: with
        no subparsers action to find it must raise, not report `{}` — an empty
        verb set would make the gate's floor the only thing between a rename in
        `argparse` and a green, vacuous parity claim."""
        import argparse

        import testlib.capability_ledger as cl

        class _Bare:
            @staticmethod
            def build_parser():
                return argparse.ArgumentParser()

        original = cl._load_cairn_cli
        cl._load_cairn_cli = lambda: _Bare
        try:
            with pytest.raises(AssertionError, match="exactly one subparsers action"):
                cl.cli_verbs_from_parser()
        finally:
            cl._load_cairn_cli = original


# =============================================================================
# The gate: the CLI verb table
# =============================================================================

class TestTheCliVerbTableMatchesTheLedger:
    def test_the_source_and_the_BUILT_PARSER_agree_on_the_verb_set(self):
        """🔴 TWO MECHANISMS, COMPLEMENTARY BLINDNESS, COMPARED TO EACH OTHER.
        The AST sees what the source spells and cannot see a verb registered by a
        mechanism it does not model; the built parser sees what argparse will
        accept and cannot see a verb the source names but never registers. Each
        alone would make the ledger a claim about one mechanism. A disagreement
        is a question for a human — which set does the ledger cover? — not
        something to average out.
        """
        from_source = cairn_source.subcommand_names()
        from_parser = set(cli_verbs_from_parser())
        assert from_source and from_parser, (
            f"a verb discovery came back EMPTY (source={sorted(from_source)}, "
            f"parser={sorted(from_parser)}), so this comparison would be vacuous. "
            f"The instrument controls in TestTheDiscoveryInstruments prove both "
            f"mechanisms CAN see a verb; this is the floor that stops an empty "
            f"result reading as agreement."
        )
        assert from_source == from_parser, (
            f"`cairn`'s verbs read from the SOURCE and from the BUILT PARSER "
            f"disagree: source-only {sorted(from_source - from_parser)}, "
            f"parser-only {sorted(from_parser - from_source)}. A parser-only verb "
            f"is registered by a mechanism `subcommand_names` does not model, so "
            f"the ledger gate is blind to it; a source-only verb is an "
            f"`add_parser` call that never reaches the parser. Widen the walker "
            f"or fix the CLI — do not adjust the ledger to whichever set is "
            f"convenient."
        )

    def test_the_ledger_names_every_verb_and_only_verbs_that_exist(self):
        """🔴 THE GATE, IN BOTH DIRECTIONS. A verb added without a row fails
        here; a row whose verb was deleted fails here too. This is the assertion
        that turns "the CLI covers every capability" from a promise into a
        measurement."""
        discovered = set(cli_verbs_from_parser())
        declared = {row.cli for row in LEDGER if row.cli is not None}
        assert discovered, (
            "verb discovery came back EMPTY, so this equality would hold against "
            "an empty ledger and prove nothing. This floor is bare non-emptiness "
            "ON PURPOSE: a count of today's verbs would fire before the equality "
            "below whenever a verb is DELETED, answering a set question with a "
            "message about vacuity."
        )
        assert discovered == declared, (
            f"the CLI verb table and the capability ledger disagree.\n"
            f"  verbs with no ledger row: {sorted(discovered - declared)}\n"
            f"  ledger rows naming no live verb: {sorted(declared - discovered)}\n"
            f"Add the row to `testlib/capability_ledger.LEDGER` — with its HTTP "
            f"route or an explicit reason there is none — or delete the row whose "
            f"verb has gone. Both directions fail deliberately: a capability the "
            f"UI and the Go client are supposed to cover can be LOST as easily as "
            f"it can be added unnoticed."
        )

    def test_the_write_verbs_are_exactly_the_rows_that_declare_they_write(self):
        """🔴 THE READ/WRITE AXIS, ASSERTED AGAINST THE FLAG THE CLI DISPATCHES
        ON. `writes=True` on a subparser is what makes an unreachable store exit 7
        ("the record was NOT made") instead of 3 ("nothing was shown"), so a new
        write verb that forgets the flag is a caller reading a lost write as a
        stale read. A row declaring WRITES with no flag behind it fails here."""
        flags = cli_verbs_from_parser()
        assert flags, "verb discovery came back EMPTY; nothing was compared"
        writing_verbs = {verb for verb, writes in flags.items() if writes}
        declared = {
            row.cli for row in LEDGER
            if row.cli is not None and row.effect == WRITES
        }
        assert writing_verbs == declared, (
            f"the verbs argparse marks `writes=True` and the ledger rows whose "
            f"effect is {WRITES!r} disagree: flagged-only "
            f"{sorted(writing_verbs - declared)}, ledger-only "
            f"{sorted(declared - writing_verbs)}. A flagged-only verb is a write "
            f"the ledger calls a read; a ledger-only row is a write verb whose "
            f"subparser is missing `writes=True`, which collapses exit 7 into "
            f"exit 3 on an unreachable store."
        )


# =============================================================================
# The gate: the HTTP route table
# =============================================================================

class TestTheHttpRouteTableMatchesTheLedger:
    def test_the_ledger_names_every_route_and_only_routes_that_exist(self):
        """🔴 THE GATE, IN BOTH DIRECTIONS, over `server.py`'s own dispatch
        tables. Adding a row to `API_ROUTES` or `WRITE_ROUTES` is publishing an
        internet-reachable endpoint; doing it without a capability row fails here.
        Deleting a route whose row remains fails here too."""
        discovered = http_routes()
        declared = ledger_routes()
        assert discovered, (
            "route discovery came back EMPTY, so this equality would hold against "
            "an empty ledger. Bare non-emptiness on purpose, for the same reason "
            "as the verb gate: a count would pre-empt the set question on a "
            "DELETED route."
        )
        assert set(discovered) == set(declared), (
            f"the HTTP route table and the capability ledger disagree.\n"
            f"  routes with no ledger row: {sorted(set(discovered) - set(declared))}\n"
            f"  ledger rows naming no live route: "
            f"{sorted(set(declared) - set(discovered))}\n"
            f"A route is `(METHOD, path)` with its placeholders normalised, and "
            f"the path is DERIVED from the table's arity and fixed tail — so "
            f"adding a path component to an existing route lands here as well. "
            f"Amend `testlib/capability_ledger.LEDGER` on purpose."
        )

    def test_each_routes_read_or_write_effect_matches_the_table_it_came_from(self):
        """Over the routes both sides name — the SET claim belongs to the test
        above, and duplicating it here would make this one fail for a reason its
        own message does not describe. `WRITE_ROUTES` is the write dispatcher, so
        a row calling one of its routes a read is simply wrong."""
        discovered = http_routes()
        declared = ledger_routes()
        shared = set(discovered) & set(declared)
        assert shared, (
            "the route sets share nothing, so no effect was compared. See "
            "`test_the_ledger_names_every_route_and_only_routes_that_exist`, "
            "which owns that failure."
        )
        mismatched = {k: (discovered[k], declared[k]) for k in shared
                      if discovered[k] != declared[k]}
        assert not mismatched, (
            f"a route's effect disagrees with the table it is dispatched from "
            f"(route: (server, ledger)): {mismatched}. A row that calls a "
            f"`WRITE_ROUTES` route a read is a capability the parity gate will "
            f"let a read-only surface claim to cover."
        )

    def test_no_two_table_rows_DERIVE_the_same_route(self):
        """An invariant guard on the derivation, not on the server: `http_routes`
        returns a dict, so two table rows collapsing to one `(method, path)` would
        silently drop a route from the gate's view — and a route the gate cannot
        see is a route that needs no ledger row. Counted against the tables' own
        row counts, `+ 1` for `/healthz`.

        Labelled an invariant guard: nothing has ever collapsed. The shape that
        would is an arity edited to match a sibling route's."""
        import server as api

        expected = len(api.API_ROUTES) + len(api.WRITE_ROUTES) + 1
        found = http_routes()
        assert len(found) == expected, (
            f"the server declares {len(api.API_ROUTES)} read routes, "
            f"{len(api.WRITE_ROUTES)} write routes and one health path — "
            f"{expected} in all — but the derivation produced {len(found)}: "
            f"{sorted(found)}. Two rows derive one `(method, path)`, so one of "
            f"them is invisible to every assertion in this file."
        )
