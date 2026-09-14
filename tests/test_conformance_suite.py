"""The conformance suite is code, so it is tested like code.

🔴 WHAT THIS FILE IS FOR, STATED UP FRONT BECAUSE IT IS EASY TO CONFUSE WITH THE
SUITE ITSELF. `tests/conformance/` measures the SERVER. This file measures the
MEASURING INSTRUMENT, and the two claims it makes about it are:

  * NEGATIVE CONTROL — put a deliberately wrong server behind the suite and
    watch it fail. `TestTheSuiteGoesRedOnARealMutation` does that with realistic
    mutations (a status code moved, a header dropped, preserved mtimes truncated
    to whole seconds, a refusal made distinguishable from an absence), and pairs
    every one with the POSITIVE control: an unmutated copy of the same tree,
    booted the same way, which has to pass. Without that pair, `the mutant was
    caught` cannot be told from `the copied tree never booted`.
  * EVERY GUARD IN THE SUITE IS REACHABLE AND FAILS WITH ITS OWN MESSAGE. Each
    `TestCorpusGuards` case breaks one thing on purpose and asserts on the
    sentence THAT guard raises, never merely that something raised — a test
    satisfied by any exception is green when a neighbour's error arrives first.

⚠ WHICH OF THESE ARE INVARIANT GUARDS, labelled rather than counted as
regression coverage: `TestTheCorpusIsWellFormed`, and every part of
`TestTheGoldensAreGenerated` EXCEPT
`test_a_different_host_label_produces_the_same_goldens` — that one is regression
coverage, because the defect it pins was live in this branch and it is what
found it (the goldens were host-specific through `Content-Length`).
`TestTheFramingClaim::test_it_is_NOT_asserted_where_it_could_not_fail` is
regression coverage too, for a VACUOUS guard in an earlier version of this
suite. Everything under `TestCorpusGuards`, `TestNormalizations`,
`TestTheSuiteGoesRedOnARealMutation` and `TestTheRelationsCannotBeRegeneratedAway`
was watched to fail: each was run against the broken input before its assertion
was written.
"""

from __future__ import annotations

import json
import re
import sys
import tempfile
from pathlib import Path

import pytest

sys.path.insert(0, str(Path(__file__).resolve().parent))

from conformance import cases as cases_mod  # noqa: E402
from conformance import mutate, oracle, suite, wire  # noqa: E402

ROOT = Path(__file__).resolve().parents[1]
GOLDEN = ROOT / "tests" / "conformance" / "golden"

# 🔴 SPELLED OUT BY HAND, NEVER IMPORTED FROM THE THING UNDER TEST. These are the
# routes `server/server.py` declares today. `declared_routes` reads them by AST;
# if it ever silently discovered NOTHING it would report full coverage over an
# empty set, and comparing against an imported copy of the same dict would be
# `x == x`.
EXPECTED_ROUTES = {
    "GET recall", "HEAD recall",
    "GET search", "HEAD search",
    "GET snapshot", "HEAD snapshot",
    "POST entry",
    "PUT entry",
}


@pytest.fixture(scope="module")
def corpus() -> cases_mod.Corpus:
    return cases_mod.load_corpus()


def _corpus_from(raw: dict, tmp_path: Path, **kwargs) -> cases_mod.Corpus:
    path = tmp_path / "requests.json"
    path.write_text(json.dumps(raw), encoding="utf-8")
    return cases_mod.load_corpus(path)


def _raw_corpus() -> dict:
    return json.loads(cases_mod.REQUESTS_PATH.read_text(encoding="utf-8"))


# ---------------------------------------------------------------------------


class TestTheCorpusIsWellFormed:
    """INVARIANT GUARDS: properties the declared list holds today."""

    def test_it_loads(self, corpus):
        assert len(corpus.cases) > 50
        assert corpus.uniform_401
        assert corpus.scope_pairs

    def test_the_route_ledger_finds_exactly_the_routes_that_exist(self):
        assert cases_mod.declared_routes() == EXPECTED_ROUTES

    def test_every_declared_route_is_addressed_by_a_row(self, corpus):
        assert cases_mod.addressed_routes(corpus) == EXPECTED_ROUTES

    def test_every_case_has_a_golden_and_every_golden_has_a_case(self, corpus):
        declared = {c.id for c in corpus.cases}
        on_disk = {p.stem for p in GOLDEN.glob("*.json")}
        assert declared == on_disk

    def test_every_golden_is_internally_consistent(self, corpus):
        for case in corpus.cases:
            wire.read_golden(GOLDEN, case.id)

    def test_no_golden_carries_a_date_that_is_not_the_placeholder_or_the_year_2000(
        self, corpus
    ):
        """🔴 AGENTS.md: a fixture that needs a date uses an obviously-synthetic
        year-2000 one. A golden holding today's date would be a golden that goes
        stale at midnight — and the append case proves the point, since its
        rendered line is exactly where a live date would land."""
        offenders = []
        for case in corpus.cases:
            text = wire.golden_path(GOLDEN, case.id).read_text(encoding="utf-8")
            for match in wire._ISO_DATE.finditer(text):
                if not match.group(0).startswith("2000-"):
                    offenders.append(f"{case.id}: {match.group(0)}")
        assert offenders == []


class TestCorpusGuards:
    """Each guard, broken on purpose, asserting on ITS OWN message."""

    def test_a_duplicate_case_id_is_refused(self, tmp_path):
        raw = _raw_corpus()
        raw["cases"].append(dict(raw["cases"][0]))
        with pytest.raises(cases_mod.CorpusError) as exc:
            _corpus_from(raw, tmp_path)
        assert "duplicate case id" in str(exc.value)
        assert "overwrite" in str(exc.value)

    def test_a_read_after_a_write_is_refused(self, tmp_path):
        raw = _raw_corpus()
        # Move a `read` row to the end, after every `write` row.
        reads = [c for c in raw["cases"] if c["phase"] == "read"]
        raw["cases"] = [c for c in raw["cases"] if c is not reads[-1]] + [reads[-1]]
        with pytest.raises(cases_mod.CorpusError) as exc:
            _corpus_from(raw, tmp_path)
        assert "is a read at position" in str(exc.value)
        assert "entry-files" in str(exc.value)

    def test_a_route_the_list_does_not_address_is_refused(self, corpus):
        """The new-route blind spot. A route the server grows and the list does
        not name must fail LOUDLY, or the suite silently stops covering it."""
        with pytest.raises(cases_mod.CorpusError) as exc:
            cases_mod.validate_corpus(
                corpus, routes=EXPECTED_ROUTES | {"GET grants"}
            )
        assert "declares routes this request list never addresses" in str(exc.value)
        assert "GET grants" in str(exc.value)

    def test_a_route_the_server_no_longer_declares_is_refused(self, corpus):
        with pytest.raises(cases_mod.CorpusError) as exc:
            cases_mod.validate_corpus(corpus, routes=EXPECTED_ROUTES - {"GET search"})
        assert "addresses routes the server no longer declares" in str(exc.value)
        assert "GET search" in str(exc.value)

    def test_a_negative_route_row_that_names_a_real_route_is_refused(self, corpus):
        """`negative_route` excludes a row from the coverage ledger, so a row
        marked for a route that DOES exist would hide that route behind
        something that looks deliberate."""
        with pytest.raises(cases_mod.CorpusError) as exc:
            cases_mod.validate_corpus(
                corpus, routes=EXPECTED_ROUTES | {"GET raw_dump"}
            )
        assert "is marked negative_route" in str(exc.value)
        assert "GET raw_dump" in str(exc.value)

    def test_a_scope_pair_across_two_principals_is_refused(self, tmp_path):
        raw = _raw_corpus()
        raw["relations"]["refused_equals_absent"][0]["absent"] = "recall-empty-scope"
        raw["relations"]["refused_equals_absent"][0]["absent_scope"] = "hollow-set"
        with pytest.raises(cases_mod.CorpusError) as exc:
            _corpus_from(raw, tmp_path)
        assert "different principals" in str(exc.value)

    def test_a_pair_whose_scope_is_not_in_its_target_is_refused(self, tmp_path):
        raw = _raw_corpus()
        raw["relations"]["refused_equals_absent"][0]["refused_scope"] = "not-there"
        with pytest.raises(cases_mod.CorpusError) as exc:
            _corpus_from(raw, tmp_path)
        assert "does not appear in" in str(exc.value)

    def test_a_row_with_no_stated_reason_is_refused(self, tmp_path):
        raw = _raw_corpus()
        raw["cases"][0]["why"] = "   "
        with pytest.raises(cases_mod.CorpusError) as exc:
            _corpus_from(raw, tmp_path)
        assert "states why it is here" in str(exc.value)

    def test_the_route_ledger_refuses_a_source_it_found_no_table_in(self, tmp_path):
        """🔴 THE LEDGER'S OWN POSITIVE CONTROL. A discovery function that finds
        nothing reports FULL COVERAGE over an empty set — the reassuring zero.
        It must refuse instead."""
        empty = tmp_path / "server.py"
        empty.write_text("X = 1\n", encoding="utf-8")
        with pytest.raises(cases_mod.CorpusError) as exc:
            cases_mod.declared_routes(empty)
        assert "no API_ROUTES dict literal" in str(exc.value)

    def test_the_route_ledger_reads_a_synthetic_table(self, tmp_path):
        """…and the matching NEGATIVE control: it can see a table that is there,
        including a write row, so a green above is not `it never parses`."""
        src = tmp_path / "server.py"
        src.write_text(
            'API_ROUTES: dict[str, tuple[str, int]] = {"alpha": ("_a", 2)}\n'
            'WRITE_ROUTES: dict = {("POST", "beta"): ("_b", 3, ())}\n',
            encoding="utf-8",
        )
        assert cases_mod.declared_routes(src) == {
            "GET alpha", "HEAD alpha", "POST beta",
        }


class TestTheOracleSpecificMark:
    """🔴 THE ONE MECHANISM THAT IS ALLOWED TO SKIP A CASE, AND EVERY GUARD ON IT.

    A row marked `oracle_only` records an answer that is `http.server`'s or CPython's
    own shape rather than a contract any implementation can honour — an HTTP/0.9
    response with no status line, a request line the framework rejects before a handler
    exists, a body quoting the `json` module's diagnostic. It is asserted against the
    oracle and SKIPPED, by id and with its reason, for anything else.

    ⚠ IT IS THE LAST RESORT AND NOT THE FIRST. A response differing in ONE FIELD
    belongs in `wire.NORMALIZATIONS`, which keeps every other byte pinned for both
    implementations; this mark stops the whole case being compared, so the guards below
    exist to make sure it cannot be used quietly.
    """

    def test_the_marked_set_is_exactly_the_four_rows_that_carry_a_reason(self, corpus):
        """An INVARIANT GUARD on today's set, spelled by hand: four rows carry the
        mark, and the list is written out so a fifth cannot appear unreviewed."""
        marked = {c.id for c in corpus.cases if c.oracle_only}
        assert marked == {
            "raw-malformed-request-line",
            "raw-malformed-absolute-target",
            "post-bullets-not-json",
            "post-bullets-deeply-nested-json",
        }
        for case in corpus.cases:
            assert case.oracle_only == bool(case.oracle_only_why.strip())

    def test_a_mark_with_no_reason_is_refused(self, tmp_path):
        raw = _raw_corpus()
        for row in raw["cases"]:
            if row["id"] == "health-unauthenticated":
                row["oracle_only"] = True
        with pytest.raises(cases_mod.CorpusError) as exc:
            _corpus_from(raw, tmp_path)
        assert "states no reason" in str(exc.value)

    def test_a_reason_with_no_mark_is_refused(self, tmp_path):
        """The other direction: a reason on an unmarked row describes nothing, and
        the row IS compared against every implementation — so the prose would read
        as a licence that is not in force."""
        raw = _raw_corpus()
        for row in raw["cases"]:
            if row["id"] == "health-unauthenticated":
                row["oracle_only_why"] = "because reasons"
        with pytest.raises(cases_mod.CorpusError) as exc:
            _corpus_from(raw, tmp_path)
        assert "is not marked oracle_only" in str(exc.value)

    def test_the_oracle_run_SKIPS_NOTHING(self):
        """🔴 THE PROPERTY THAT MAKES THE MARK SAFE. Skipping against the oracle would
        silently stop covering the oracle's real behaviour, which is worse than
        asserting it against a port — so `run_against_oracle` asserts every marked row
        and `run`'s default is the strict direction."""
        outcome = suite.run_against_oracle()
        assert outcome.failures == []
        assert outcome.skipped == []
        assert not [ln for ln in outcome.lines if ln.startswith("SKIP")]

    def test_skipping_is_REPORTED_by_id_and_by_reason(self):
        """A skip nobody can see is indistinguishable from a pass, which is the whole
        failure mode this suite is built against — so the run names every skipped id on
        its own line, with the row's reason, and counts them in the summary."""
        with tempfile.TemporaryDirectory(prefix="cairn-conformance-") as td:
            with oracle.running_oracle(Path(td)) as ora:
                outcome = suite.run(
                    ora.base_url, ora.token_file, GOLDEN,
                    assert_oracle_specific=False,
                )
        assert sorted(outcome.skipped) == sorted(
            [
                "post-bullets-deeply-nested-json",
                "post-bullets-not-json",
                "raw-malformed-absolute-target",
                "raw-malformed-request-line",
            ]
        )
        # Per-CASE skip lines only: the relation lines below are their own claim and
        # are counted separately, because "a case was not compared" and "a relation
        # lost a member" are different facts and folding them would make the count
        # agree with either.
        skip_lines = [
            ln for ln in outcome.lines
            if ln.startswith("SKIP ") and not ln.startswith("SKIP relation ")
        ]
        assert len(skip_lines) == len(outcome.skipped)
        for line in skip_lines:
            assert "oracle-specific: " in line
            # …and the reason is the ROW's, not a constant this runner invented.
            assert len(line.split("oracle-specific: ", 1)[1]) > 80
        summary = [ln for ln in outcome.lines if ln.startswith("SUMMARY")][0]
        assert "skipped=4" in summary
        assert "raw-malformed-request-line" in summary
        # 🔴 THE REQUEST IS STILL ISSUED. Only the comparison is skipped, because the
        # request itself is part of the run's arithmetic: fifteen deliberate refusals
        # from one client address, and a canary at the end that proves no lockout
        # tripped. Dropping a request would change what the limiter saw.
        assert outcome.requests == len(cases_mod.load_corpus().cases) + 1
        # The oracle answers every one of them correctly, so a skip must not turn into
        # a failure — and the relation whose member was skipped must still be asserted
        # over what is left.
        assert outcome.failures == []
        relation_skips = [
            ln for ln in outcome.lines if ln.startswith("SKIP relation uniform-401")
        ]
        assert len(relation_skips) == 1
        assert "asserted over the remaining 14 members" in relation_skips[0]

    def test_a_relation_whose_every_member_is_skipped_FAILS(self, tmp_path):
        """🔴 THE ANTI-VACUITY GUARD ON THE MECHANISM. A relation left with no members
        asserts nothing at all, and reporting that as a pass is exactly the reassuring
        zero the rest of this suite is built to refuse."""
        raw = _raw_corpus()
        members = set(raw["relations"]["uniform_401"])
        for row in raw["cases"]:
            if row["id"] in members:
                row["oracle_only"] = True
                row["oracle_only_why"] = (
                    "a synthetic mark, used only to prove the relation refuses to "
                    "report a pass over an empty member set"
                )
        corpus = _corpus_from(raw, tmp_path)
        outcome = suite.Outcome()
        suite.check_uniform_401(corpus, {}, outcome, skipped=members)
        assert any("asserts nothing at all" in f for f in outcome.failures)

    def test_a_relation_between_two_FAILING_answers_says_so(self, tmp_path):
        """🔴 A RELATION BETWEEN TWO RESPONSES THAT BOTH FAILED THEIR OWN GOLDEN STILL
        PASSES, AND THE LINE HAS TO SAY SO. The claim is "these two answers are the
        SAME", which is true of two answers that are identically WRONG — so the verdict
        is not a defect, but a reader seeing `PASS relation refused-equals-absent
        recall` next to a failing `recall-refused-scope` would reasonably conclude the
        refusal path is correct. MEASURED on the Go port at P1a: both report relations
        passed while all four members answered `501 not-implemented`, because a
        not-implemented answer is beautifully uniform."""
        corpus = cases_mod.load_corpus()
        pair = corpus.scope_pairs[0]
        identical = {
            "case": "x",
            "status": 200,
            "reason": "OK",
            "headers": [],
            "body": {"kind": "text_lines", "text_lines": ["same"], "sha256": "x", "bytes": 4},
        }
        # Every pair needs a record, because the checker walks all of them — a
        # partial map would fail on a missing key instead of on the claim.
        records = {}
        for other in corpus.scope_pairs:
            records[other.refused] = dict(identical)
            records[other.absent] = dict(identical)

        clean = suite.Outcome()
        suite.check_scope_pairs(corpus, records, clean, skipped=set(), failed=set())
        assert any(
            ln == f"PASS relation refused-equals-absent {pair.name}" for ln in clean.lines
        ), clean.lines

        caveated = suite.Outcome()
        suite.check_scope_pairs(
            corpus, records, caveated,
            skipped=set(), failed={pair.refused, pair.absent},
        )
        assert any(
            ln.startswith(f"PASS relation refused-equals-absent {pair.name} (")
            and "both members failed their own golden" in ln
            for ln in caveated.lines
        ), caveated.lines


class TestNormalizations:
    def test_the_bullet_date_normalization_maps_two_different_dates_together(self):
        """🔴 A TWO-POINT MEASUREMENT ON THE DIMENSION THAT MOVES. The generator
        ran at one clock instant, so the run itself cannot show that a golden
        recorded tomorrow would match one recorded today. Feeding the rule two
        different dates is what measures it."""
        def answer(date: str) -> wire.Response:
            return wire.Response(
                status=200,
                reason="OK",
                headers=(),
                body=f"- {date}: a bullet [cairn: who/what]\n".encode(),
            )

        first, fired = wire.apply_normalizations(answer("2026-09-14"), ["bullet-date"])
        second, _ = wire.apply_normalizations(answer("2031-02-28"), ["bullet-date"])
        assert "bullet-date" in fired
        assert first.body == second.body
        assert wire.TODAY_PLACEHOLDER.encode() in first.body
        # And it does NOT eat the rest of the line.
        assert b"[cairn: who/what]" in first.body

    def test_the_bullet_date_normalization_touches_only_the_leading_date(self):
        resp = wire.Response(
            status=200, reason="OK", headers=(),
            body=b"- 2026-09-14: see also 2026-09-15 and 2000-01-02\n",
        )
        out, _ = wire.apply_normalizations(resp, ["bullet-date"])
        assert out.body == (
            f"- {wire.TODAY_PLACEHOLDER}: see also 2026-09-15 and 2000-01-02\n"
        ).encode()

    def test_the_etag_mask_refuses_a_value_of_the_wrong_shape(self):
        """A normalization that masks a value it cannot pin must not mask one
        that is WRONG — otherwise it launders a malformed ETag into a pass."""
        resp = wire.Response(
            status=200, reason="OK", headers=(("ETag", '"not-a-digest"'),), body=b""
        )
        with pytest.raises(wire.WireError) as exc:
            wire.apply_normalizations(resp, ["append-etag"])
        assert "not the documented shape" in str(exc.value)

    def test_an_unknown_normalization_name_is_refused(self):
        resp = wire.Response(status=200, reason="OK", headers=(), body=b"")
        with pytest.raises(wire.WireError) as exc:
            wire.apply_normalizations(resp, ["ignore-everything"])
        assert "unknown normalization" in str(exc.value)

    def test_a_declared_normalization_that_matched_nothing_is_reported(self, corpus):
        """🔴 THE ANTI-WIDENING GUARD. A row that keeps a normalization it no
        longer needs stops comparing that field, invisibly."""
        case = corpus.by_id("health-unauthenticated")
        widened = type(case)(**{**case.__dict__, "normalize": ("bullet-date",)})
        answers = {widened.id: (wire.Response(200, "OK", (), b"ok\n"), set())}
        fake = type(corpus)(
            cases=(widened,), uniform_401=corpus.uniform_401,
            scope_pairs=corpus.scope_pairs,
        )
        problems = suite.check_declared_normalizations(fake, answers)
        assert len(problems) == 1
        assert "matched\nNOTHING" in problems[0] or "matched NOTHING" in problems[0]

    def test_every_declared_normalization_states_a_reason(self):
        for rule in wire.NORMALIZATIONS:
            assert len(rule.reason) > 40, rule.name
            assert rule.field

    def test_the_duplicate_append_golden_keeps_its_year_2000_date(self):
        """The mirror image of the date normalization: the `duplicate` verdict
        echoes a line ALREADY ON DISK, so its date is fixture data and stays
        pinned. A blanket date rule would have erased it."""
        rec = wire.read_golden(GOLDEN, "post-bullets-duplicate")
        body = "\n".join(rec["body"]["text_lines"])
        assert body.startswith("- 2000-01-02: ")
        assert wire.TODAY_PLACEHOLDER not in body


class TestTheFramingClaim:
    """`Content-Length` is checked against the bytes received, not a golden.

    That is what lets `report-content-length` drop a header whose VALUE is
    host-dependent without dropping the claim — so the check has to be shown to
    fail, or the normalization really is a widening.
    """

    def test_a_mismatched_content_length_is_caught(self, corpus):
        outcome = suite.Outcome()
        resp = wire.Response(
            status=200, reason="OK",
            headers=(("Content-Length", "99"),),
            body=b"these bytes are not 99",
        )
        suite._framing(corpus.by_id("raw-recall-read-to-eof"), resp, outcome)
        assert outcome.failures
        assert "smuggling primitive" in outcome.failures[0]

    def test_a_matching_content_length_passes_and_is_counted(self, corpus):
        outcome = suite.Outcome()
        body = b"exactly this long\n"
        resp = wire.Response(
            status=200, reason="OK",
            headers=(("Content-Length", str(len(body))),), body=body,
        )
        got = suite._framing(corpus.by_id("raw-recall-read-to-eof"), resp, outcome)
        assert outcome.failures == []
        assert outcome.assertions == 1
        assert got == len(body)

    def test_it_is_NOT_asserted_where_it_could_not_fail(self, corpus):
        """🔴 THE VACUOUS-ASSERTION GUARD, and it pins a measured fact rather than
        a preference. `http.client` reads exactly `Content-Length` bytes, so for a
        case issued through it the comparison asks the client whether it agrees
        with itself. Counting it would inflate the assertion total with 97 checks
        that cannot fail."""
        outcome = suite.Outcome()
        resp = wire.Response(
            status=200, reason="OK",
            headers=(("Content-Length", "99"),), body=b"nowhere near 99 bytes",
        )
        got = suite._framing(corpus.by_id("recall-authorized"), resp, outcome)
        assert outcome.failures == []
        assert outcome.assertions == 0
        assert got == 99

    def test_a_server_that_understates_every_length_is_caught(self, tmp_path):
        """The live negative control for the framing claim, and the measurement
        that made the read-to-EOF case necessary: with only the malformed-target
        raw case in the corpus, this mutation failed exactly ONE framing check and
        no report at all."""
        server_py = mutate.mutated_server(
            tmp_path / "tree",
            'self.send_header("Content-Length", str(len(body)))',
            'self.send_header("Content-Length", str(max(0, len(body) - 1)))',
        )
        outcome = suite.run_against_oracle(server_py=server_py)
        framing = [f for f in outcome.failures if "smuggling primitive" in f]
        assert len(framing) >= 2, framing
        assert any(f.startswith("raw-recall-read-to-eof") for f in framing)

    def test_a_head_with_a_shorter_length_than_its_get_is_caught(self, corpus):
        outcome = suite.Outcome()
        outcome.raw_lengths = {
            "recall-authorized-head": 10,
            "recall-authorized": 3386,
            "search-authorized-head": 7,
            "search-authorized": 7,
        }
        suite.check_head_pairs(corpus, outcome)
        assert len(outcome.failures) == 1
        assert "understates the length" in outcome.failures[0]
        assert [ln for ln in outcome.lines if ln.startswith("FAIL relation head")]


class TestGoldenIntegrity:
    def test_a_hand_edited_golden_is_refused(self, tmp_path):
        """🔴 FIXTURES ARE GENERATED, NEVER HAND-WRITTEN. An edited body no
        longer hashes to its recorded digest, and the runner says so instead of
        asserting what somebody believed."""
        rec = json.loads(
            wire.golden_path(GOLDEN, "health-unauthenticated").read_text("utf-8")
        )
        rec["body"]["text_lines"] = ["not ok", ""]
        wire.golden_path(tmp_path, "health-unauthenticated").write_text(
            wire.dumps(rec), encoding="utf-8"
        )
        with pytest.raises(wire.WireError) as exc:
            wire.read_golden(tmp_path, "health-unauthenticated")
        assert "INTERNALLY INCONSISTENT" in str(exc.value)
        assert "generated, never hand-written" in str(exc.value)

    def test_a_missing_golden_names_the_regeneration_command(self, tmp_path):
        with pytest.raises(wire.WireError) as exc:
            wire.read_golden(tmp_path, "health-unauthenticated")
        assert "suite.py generate" in str(exc.value)


class TestTheLeakGuard:
    def test_it_finds_a_planted_machine_id(self):
        secrets = suite._host_secrets()
        assert secrets, "this host offers no identifying string to guard against"
        planted = f"  host: conformance-oracle-{secrets[-1]}  (per-host cache)"
        assert suite.check_no_host_leak(planted, "a golden")

    def test_it_passes_clean_text(self):
        assert suite.check_no_host_leak("  host: <HOST-IDENTITY>  (x)", "a golden") == []

    def test_no_committed_golden_carries_this_machine(self, corpus):
        problems = []
        for case in corpus.cases:
            text = wire.golden_path(GOLDEN, case.id).read_text(encoding="utf-8")
            problems += suite.check_no_host_leak(text, case.id)
        assert problems == []


class TestTheWorldIsBuiltFromItsDeclaration:
    def test_mtimes_are_set_from_the_declaration(self, tmp_path):
        """🔴 THE ONE THING GIT CANNOT STORE. The snapshot contract depends on
        entry mtimes to sub-second precision, so a build that forgot to set them
        would produce a store whose order is whatever order the loop ran in."""
        world = oracle.load_world()
        root = oracle.build_store(tmp_path / "store", world)
        for entry in world["entries"]:
            declared = entry["mtime"]
            actual = (root / entry["path"]).stat().st_mtime
            assert abs(actual - declared) < 1e-6, entry["path"]

    def test_two_entries_share_a_whole_second_and_differ_in_the_fraction(self):
        """The fixture shape that a normalized tar destroys. If this ever stops
        holding, the snapshot ordering assertion loses the only input it can be
        wrong about."""
        world = oracle.load_world()
        mtimes = [e["mtime"] for e in world["entries"]]
        same_second = [m for m in mtimes if int(m) == int(mtimes[0])]
        assert len(same_second) >= 2
        assert len(set(same_second)) == len(same_second)

    def test_the_token_file_round_trips_every_declared_principal(self, tmp_path):
        world = oracle.load_world()
        tokens = oracle.mint_tokens(world)
        path = oracle.write_token_file(tmp_path / "tokens", tokens, world)
        found = oracle.principals_from_token_file(path, world)
        assert set(found) == {p["name"] for p in world["principals"]}
        assert found["legacy"].identity is None
        assert found["narrow-reader"].scopes == ("beta-notes",)

    def test_a_token_file_missing_a_principal_is_refused(self, tmp_path):
        """A run that silently sends no credential would record a corpus of 401s
        as if it were the contract."""
        path = tmp_path / "tokens"
        path.write_text("a" * 50 + " wide-reader alpha-notes\n", encoding="utf-8")
        with pytest.raises(oracle.OracleError) as exc:
            oracle.principals_from_token_file(path)
        assert "not configured for this corpus" in str(exc.value)

    def test_no_declared_token_is_a_literal(self):
        """🔴 A REAL-LOOKING CREDENTIAL IS A FINDING IN A PUBLIC REPO whether or
        not it ever authenticated anything. The world declares principals, never
        tokens; the tokens are minted per run."""
        world = oracle.load_world()
        for spec in world["principals"]:
            assert "token" not in spec, spec["name"]
        # Nothing in the declaration is long enough to BE a credential: the
        # server's floor is 43 characters of `[A-Za-z0-9_-]`.
        text = (ROOT / "tests" / "conformance" / "world.json").read_text("utf-8")
        assert re.search(r"[A-Za-z0-9_-]{43,}", text) is None
        first = oracle.mint_tokens()
        second = oracle.mint_tokens()
        assert set(first.values()) & set(second.values()) == set()
        assert all(len(t) >= 43 for t in first.values())


# ---------------------------------------------------------------------------
# The instrument, validated end to end.
# ---------------------------------------------------------------------------


class TestTheSuitePassesAgainstTheOracle:
    def test_it_passes_and_it_issued_something(self):
        """🔴 THE POSITIVE CONTROL, AND THE COUNTS ARE REPORTED. `0 failures`
        over 0 requests is the failure mode this asserts against."""
        outcome = suite.run_against_oracle()
        assert outcome.failures == []
        assert outcome.requests == len(cases_mod.load_corpus().cases) + 1  # + canary
        assert outcome.assertions > 300
        result_lines = [
            ln for ln in outcome.lines if ln.startswith(("PASS ", "FAIL "))
        ]
        # Read the CONTENT, not an exit code: count the runner's own lines.
        assert len(result_lines) == len(outcome.lines) - 1  # all but SUMMARY
        assert not [ln for ln in result_lines if ln.startswith("FAIL")]

    def test_adding_a_case_moves_the_numbers(self, tmp_path):
        """…and the count is not a constant somebody typed: a corpus with one
        row removed issues one fewer request and makes fewer assertions."""
        raw = _raw_corpus()
        full = cases_mod.load_corpus()
        # Drop a row nothing else references.
        raw["cases"] = [c for c in raw["cases"] if c["id"] != "recall-mode-full"]
        smaller = _corpus_from(raw, tmp_path)
        assert len(smaller.cases) == len(full.cases) - 1
        outcome = suite.run_against_oracle(corpus=smaller)
        assert outcome.failures == []
        assert outcome.requests == len(full.cases)  # one fewer case, plus the canary


class TestTheSuiteGoesRedOnARealMutation:
    """🔴 THE NEGATIVE CONTROL. Realistic mutations, not textbook ones."""

    #: `(label, old, new, at_least)` — `at_least` is the smallest number of
    #: failures the mutation must produce. Deliberately not the exact count: the
    #: claim is `this is caught`, and pinning the blast radius would make every
    #: new case in the corpus a change to this table.
    MUTATIONS = [
        pytest.param(
            '404, b"not found\\n", headers={"X-Store-Status": "not-found"}',
            '403, b"not found\\n", headers={"X-Store-Status": "not-found"}',
            id="one-status-code-moved",
        ),
        pytest.param(
            '                "X-Store-Exit": str(code),\n',
            "",
            id="one-header-dropped",
        ),
        pytest.param(
            "info.mtime = st.st_mtime",
            "info.mtime = int(st.st_mtime)",
            id="snapshot-mtimes-truncated-to-whole-seconds",
        ),
        pytest.param(
            'selected.append((entry, f"{scope.name}/{entry.name}"))',
            'selected.insert(0, (entry, f"{scope.name}/{entry.name}"))',
            id="snapshot-members-reordered",
        ),
        pytest.param(
            "raise ValueError(f\"{name} must be an integer, got {values[-1]!r}\") from None",
            "return None",
            id="a-bad-query-parameter-silently-defaults",
        ),
    ]

    def test_the_unmutated_copy_passes(self, tmp_path):
        """🔴 THE PAIR THE NEGATIVE CONTROLS NEED. Same copy mechanics, same
        symlinked `lib/`, no edit — so a red below is the MUTATION and not the
        harness."""
        server_py = mutate.unmutated_server(tmp_path / "tree")
        outcome = suite.run_against_oracle(server_py=server_py)
        assert outcome.failures == []
        assert outcome.requests > 50

    @pytest.mark.parametrize("old,new", MUTATIONS)
    def test_a_mutation_is_caught(self, tmp_path, old, new):
        server_py = mutate.mutated_server(tmp_path / "tree", old, new)
        outcome = suite.run_against_oracle(server_py=server_py)
        assert outcome.failures, "the mutation was NOT caught"
        assert [ln for ln in outcome.lines if ln.startswith("FAIL")]

    def test_a_mutation_pattern_that_matches_nothing_is_refused(self, tmp_path):
        """🔴 THE MUTATION SWEEP'S OWN FALSE SURVIVED. A pattern that matched
        nothing yields an UNMUTATED server, which passes — and the run would be
        reported as `this mutation was not caught`."""
        with pytest.raises(mutate.MutationError) as exc:
            mutate.mutated_server(tmp_path / "tree", "this text is not in server.py", "x")
        assert "occurs 0 time(s)" in str(exc.value)


class TestTheRelationsCannotBeRegeneratedAway:
    """🔴 THE CLAIM A PER-RESPONSE GOLDEN CANNOT MAKE.

    Each of these mutations is one a fresh set of goldens would ABSORB: record
    the divergent responses, compare each to its own file, and the suite is green
    over a server that now discriminates. The relational assertions are the only
    thing standing in the way, so the test is that `generate` REFUSES.
    """

    def test_a_refused_scope_that_leaks_the_scope_revision_is_refused(self, tmp_path):
        server_py = mutate.mutated_server(
            tmp_path / "tree",
            "if visible_scopes is not None and rc.normalize_ref(scope) not in {",
            "if False and rc.normalize_ref(scope) not in {",
        )
        outcome = suite.run_against_oracle(server_py=server_py)
        red = [ln for ln in outcome.lines if ln.startswith("FAIL relation refused")]
        assert red, "the refused-equals-absent relation did not fire"
        with pytest.raises(suite.SuiteError) as exc:
            suite.generate(tmp_path / "golden", server_py=server_py)
        assert "violates a relational property" in str(exc.value)
        assert "enumeration API" in str(exc.value)

    def test_a_head_that_reports_a_length_of_zero_is_refused(self, tmp_path):
        """The classic naive-HEAD bug: no body, so report no length. The goldens
        CANNOT see it on the report routes — `Content-Length` is normalized away
        there, because its value is host-dependent — so the HEAD/GET relation is
        the only thing that does."""
        server_py = mutate.mutated_server(
            tmp_path / "tree",
            'self.send_header("Content-Length", str(len(body)))',
            'self.send_header("Content-Length", '
            'str(0 if self.command == "HEAD" else len(body)))',
        )
        outcome = suite.run_against_oracle(server_py=server_py)
        red = [ln for ln in outcome.lines if ln.startswith("FAIL relation head")]
        assert red, "the head-matches-get relation did not fire"
        with pytest.raises(suite.SuiteError) as exc:
            suite.generate(tmp_path / "golden", server_py=server_py)
        assert "violates a relational property" in str(exc.value)

    def test_a_401_that_says_why_is_refused(self, tmp_path):
        server_py = mutate.mutated_server(
            tmp_path / "tree",
            "        self._audit(path, 401, status)\n        self._unauthorized()",
            "        self._audit(path, 401, status)\n"
            "        self._respond(401, UNAUTHORIZED_BODY, headers={"
            "\"WWW-Authenticate\": 'Bearer realm=\"subsystem-store\"', "
            '"X-Store-Status": status})',
        )
        outcome = suite.run_against_oracle(server_py=server_py)
        red = [ln for ln in outcome.lines if ln.startswith("FAIL relation uniform-401")]
        assert red, "the uniform-401 relation did not fire"
        with pytest.raises(suite.SuiteError) as exc:
            suite.generate(tmp_path / "golden", server_py=server_py)
        assert "violates a relational property" in str(exc.value)


class TestTheGoldensAreGenerated:
    """INVARIANT GUARDS, and the determinism proof made permanent."""

    def test_two_generator_runs_agree_and_match_what_is_committed(self, tmp_path):
        """🔴 DETERMINISM IS PROVEN, NOT ASSUMED, AND THE SAME RUN PROVES THE
        GOLDENS ON DISK ARE GENERATED. Two independent runs — fresh world, fresh
        oracle, fresh tokens, a later wall clock — must agree with each other AND
        with the committed set. Any instability that survives the declared
        normalizations is a defect in the suite, not noise to retry past."""
        first = tmp_path / "one"
        second = tmp_path / "two"
        suite.generate(first)
        suite.generate(second)
        names = sorted(p.name for p in first.glob("*.json"))
        assert names == sorted(p.name for p in second.glob("*.json"))
        assert names == sorted(p.name for p in GOLDEN.glob("*.json"))
        unstable, uncommitted = [], []
        for name in names:
            a = (first / name).read_text(encoding="utf-8")
            b = (second / name).read_text(encoding="utf-8")
            c = (GOLDEN / name).read_text(encoding="utf-8")
            if a != b:
                unstable.append(name)
            if a != c:
                uncommitted.append(name)
        assert unstable == []
        assert uncommitted == [], (
            "the committed goldens are not what the generator produces. Run "
            "`python3 tests/conformance/suite.py generate` and commit the diff."
        )

    def test_a_different_host_label_produces_the_same_goldens(self, tmp_path):
        """🔴 THE SECOND POINT ON THE DIMENSION THAT ACTUALLY MOVED, and this is
        the check that found a real defect rather than confirming an assumption.

        Two generator runs on ONE host agree even when a field depends on the
        host, so a same-host determinism check is STRUCTURALLY BLIND to
        host-dependence. Changing only `CAIRN_HOST` moved 21 goldens: the body
        normalizations had removed the machine identity from the BODY, but
        `Content-Length` counts the bytes the server sent — and the identity
        varies in LENGTH, not merely in value. `report-content-length` plus
        `check_framing` is the fix; this test is what would have caught it.
        """
        out = tmp_path / "other-host"
        suite.generate(
            out, extra_env={"CAIRN_HOST": "a-completely-different-and-longer-label"}
        )
        differing = [
            p.name
            for p in sorted(out.glob("*.json"))
            if p.read_text(encoding="utf-8")
            != (GOLDEN / p.name).read_text(encoding="utf-8")
        ]
        assert differing == [], (
            "these goldens depend on the machine that generated them, so CI — or "
            "anyone else's checkout — cannot reproduce them"
        )
