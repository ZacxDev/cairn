#!/usr/bin/env python3
"""The HTTP conformance suite: generate the goldens, or replay them.

    python3 tests/conformance/suite.py generate        # re-record every golden
    python3 tests/conformance/suite.py run             # replay against the oracle
    python3 tests/conformance/suite.py run --base-url http://host:8102 \\
                                           --token-file /path/to/tokens
    python3 tests/conformance/suite.py normalizations  # print the declared table
    python3 tests/conformance/suite.py build-store DIR # materialise the world

🔴 `generate` IS THE ONLY WAY A GOLDEN IS EVER WRITTEN. There is no flag that
edits one, and `read_golden` refuses a file whose recorded body does not hash to
its recorded digest — a hand-edited golden is a golden that asserts what
somebody believed.

🔴 `run` MAKES FIVE KINDS OF CLAIM, AND THREE OF THEM NO GOLDEN FILE CAN HOLD:

  1. PER-RESPONSE — each normalized answer equals its golden.
  2. FRAMING — `Content-Length` equals the length of the body ACTUALLY RECEIVED.
     Asserted ONLY on the `raw_request` cases, which read to EOF: through an HTTP
     client the header DICTATES how many bytes are read, so the comparison would
     be vacuous. See `_framing`; it was vacuous, and a mutation measured it.
  3. REFUSED == ABSENT — a scope the caller may not see answers exactly what a
     scope that never existed answers, IN THE SAME RUN. Recording the two in
     separate goldens and comparing each to its own file passes even when they
     diverge, because whoever regenerated captured the divergence.
  4. THE UNIFORM 401 — a bad token, a non-API path, a missing client IP, an
     unparseable target and an invented verb are INDISTINGUISHABLE on the wire.
     Same shape of claim, same reason it cannot live in a per-case file.
  5. HEAD == GET — a HEAD reports the `Content-Length` its GET would have sent.
     Both values are host-dependent in LENGTH (a report body names the machine
     and the store path), so neither can live in a golden; they can still be
     compared to each other.

`generate` checks 3, 4 and 5 as well, and refuses to record a corpus that
violates any of them. Otherwise the first divergence would be baked in and the
suite would then defend it.
"""

from __future__ import annotations

import argparse
import difflib
import socket
import sys
import tempfile
from dataclasses import dataclass
from pathlib import Path
from typing import Any

if __package__ in (None, ""):  # pragma: no cover - direct `python3 suite.py`
    sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
    __package__ = "conformance"

from . import cases as cases_mod  # noqa: E402
from . import oracle, wire  # noqa: E402

SUITE_DIR = Path(__file__).resolve().parent
GOLDEN_DIR = SUITE_DIR / "golden"


class SuiteError(RuntimeError):
    """The suite could not run. Distinct from `a server answered wrongly`."""


@dataclass
class Outcome:
    """What one run measured. Counts are reported, never inferred."""

    requests: int = 0
    assertions: int = 0
    failures: list[str] = None  # type: ignore[assignment]
    lines: list[str] = None  # type: ignore[assignment]
    #: Cases NOT compared on this run because they are marked `oracle_only` and the
    #: server under test is not the oracle. 🔴 REPORTED BY ID IN THE SUMMARY AND
    #: NAMED WITH ITS REASON ON ITS OWN LINE, because a skip nobody can see is
    #: indistinguishable from a pass — which is the whole failure mode this suite is
    #: built against, arriving through the one mechanism that is allowed to skip.
    skipped: list[str] = None  # type: ignore[assignment]
    #: `Content-Length` as the server sent it, per case, BEFORE normalization.
    #: The HEAD/GET relation needs the un-normalized value and nothing else does.
    raw_lengths: dict[str, int | None] = None  # type: ignore[assignment]

    def __post_init__(self) -> None:
        if self.failures is None:
            self.failures = []
        if self.lines is None:
            self.lines = []
        if self.raw_lengths is None:
            self.raw_lengths = {}
        if self.skipped is None:
            self.skipped = []

    def line(self, text: str) -> None:
        self.lines.append(text)

    def fail(self, case_id: str, detail: str) -> None:
        self.failures.append(f"{case_id}: {detail}")


# ---------------------------------------------------------------------------
# Executing the corpus
# ---------------------------------------------------------------------------


def execute(
    base_url: str,
    principals: dict[str, Any],
    corpus: cases_mod.Corpus,
    outcome: Outcome,
) -> dict[str, tuple[wire.Response, set[str]]]:
    """Issue every case IN DECLARED ORDER and normalize each answer.

    🔴 THE ORDER IS THE CORPUS'S ORDER AND IT IS LOAD-BEARING. Write cases
    mutate the store; `validate_corpus` refuses a read placed after one. A
    parallel or shuffled runner would be a different measurement.
    """
    answers: dict[str, tuple[wire.Response, set[str]]] = {}
    for case in corpus.cases:
        resp = wire.issue(base_url, case, principals)
        outcome.requests += 1
        outcome.raw_lengths[case.id] = _framing(case, resp, outcome)
        normalized, fired = wire.apply_normalizations(resp, case.normalize)
        answers[case.id] = (normalized, fired)
    return answers


def _framing(case: cases_mod.Case, resp: wire.Response, outcome: Outcome) -> int | None:
    """Check `Content-Length` against the bytes RECEIVED. Returns the header.

    🔴 IT IS ASSERTED ONLY WHERE IT CAN FAIL, AND THE FIRST VERSION OF THIS
    FUNCTION ASSERTED IT EVERYWHERE — WHICH WAS VACUOUS FOR 95 OF 97 CASES.
    MEASURED, by mutating the server to send `Content-Length: len(body) - 1`:
    exactly ONE case failed. The reason is structural rather than a coding slip —
    `http.client` reads EXACTLY `Content-Length` bytes, so `len(resp.body)` IS
    the header, and comparing them asks the client whether it agrees with itself.
    A guard that reads as coverage while providing none is worse than none,
    because it stops anyone looking.

    So the comparison runs only for a `raw_request` case, where the response is
    read to EOF and the body's length is an independent observation. That is what
    `raw-recall-read-to-eof` exists for: it asks for `Connection: close` and
    therefore measures a REPORT body, which is the one place
    `report-content-length` drops the header from the golden.

    A HEAD is skipped either way: there is no body to measure, and a HEAD's
    length is covered by `check_head_pairs` instead.
    """
    raw = None
    for key, value in resp.headers:
        if key.lower() == "content-length":
            raw = int(value)
    if raw is None:
        return None
    if not case.is_raw or case.method == "HEAD":
        return raw
    outcome.assertions += 1
    if raw != len(resp.body):
        outcome.fail(
            case.id,
            f"Content-Length says {raw} and the body is {len(resp.body)} bytes. "
            f"A mis-framed response on a keep-alive connection is a smuggling "
            f"primitive, not a cosmetic error.",
        )
    return raw


def check_declared_normalizations(
    corpus: cases_mod.Corpus,
    answers: dict[str, tuple[wire.Response, set[str]]],
    skipped: "set[str] | None" = None,
) -> list[str]:
    """A declared normalization that changed nothing is a silent widening.

    🔴 THIS IS THE ANTI-WIDENING GUARD, and it is the reason the table can be
    trusted. A row that keeps a normalization it no longer needs carries a
    licence to differ that no reviewer can see — the field simply stops being
    compared. So every name a row declares must have actually fired on that
    row's answer.
    """
    problems: list[str] = []
    skipped = skipped or set()
    for case in corpus.cases:
        if case.id in skipped:
            # The case is not being compared at all, so a normalization that did not
            # fire on it carries no licence to differ — there is nothing left to
            # widen. Asserting here would turn every oracle-specific row into a
            # failure about a field nobody is reading.
            continue
        _resp, fired = answers[case.id]
        for name in case.normalize:
            if name not in fired:
                problems.append(
                    f"{case.id}: declares normalization {name!r}, which matched "
                    f"NOTHING in the answer. An unused normalization is a licence "
                    f"to differ that nobody can see — delete it from the row, or "
                    f"fix the row."
                )
    return problems


# ---------------------------------------------------------------------------
# The relational assertions
# ---------------------------------------------------------------------------


def _relational_form(
    rec: dict[str, Any],
    *,
    substitute: str | None = None,
    drop_headers: tuple[str, ...] = (),
) -> list[str]:
    """One recorded answer, reduced to what a BETWEEN-RESPONSES claim compares.

    Three things are removed, each for a stated reason:

      * `case` — the id is the one field that is SUPPOSED to differ.
      * `body.sha256` / `body.bytes` — both are derived from the body that is
        itself being compared line by line here, and a substitution below
        changes the bytes without changing the answer. Keeping them would make
        every pair fail on a digest of the thing already under comparison.
      * any header in `drop_headers` — used for `Content-Length` on the scope
        pairs ONLY, because the two scope names differ in length. The body
        comparison is what covers the bytes it measured.

    `substitute` replaces one scope name with `<SCOPE>`, which is the single
    licence the refused-vs-absent claim grants. See `check_scope_pairs`.
    """
    rec = {k: v for k, v in rec.items() if k != "case"}
    body = {k: v for k, v in rec["body"].items() if k not in ("sha256", "bytes")}
    rec["body"] = body
    rec["headers"] = [
        h for h in rec["headers"] if h[0].lower() not in drop_headers
    ]
    text = wire.dumps(rec)
    if substitute is not None:
        text = text.replace(substitute, "<SCOPE>")
    return text.splitlines()


def check_uniform_401(
    corpus: cases_mod.Corpus,
    records: dict[str, dict[str, Any]],
    outcome: Outcome,
    skipped: "set[str] | None" = None,
) -> None:
    """Every declared 401 case must be byte-identical to the first.

    🔴 A PER-RESPONSE GOLDEN CANNOT SEE THIS. Each of these cases has its own
    golden, and each would keep passing if one of them grew a distinguishing
    header — the regenerated golden would simply record the difference. The
    property is that they are the SAME answer, so it is asserted between them.
    """
    skipped = skipped or set()
    ids = [i for i in corpus.uniform_401 if i not in skipped]
    for case_id in corpus.uniform_401:
        if case_id in skipped:
            # 🔴 A MEMBER DROPPED FROM THIS RELATION IS A WEAKER CLAIM, SO IT IS SAID
            # OUT LOUD RATHER THAN LEFT TO BE COUNTED. The relation still holds over
            # the members that remain; what it no longer covers is this one, and the
            # row's own `oracle_only_why` has to say where that is covered instead.
            outcome.line(
                f"SKIP relation uniform-401 {case_id} "
                f"(oracle-specific; the relation is asserted over the remaining "
                f"{len(ids)} members)"
            )
    if not ids:
        outcome.fail(
            "uniform-401",
            "every member of the uniform-401 relation is oracle-specific on this "
            "run, so the relation asserts nothing at all",
        )
        outcome.line("FAIL relation uniform-401 (no members left to compare)")
        return
    reference = ids[0]
    ref = _relational_form(records[reference])
    outcome.assertions += 1
    if records[reference]["status"] != 401:
        outcome.fail(
            reference,
            f"the uniform-401 reference answered {records[reference]['status']}, "
            f"so every comparison below would pin the wrong shape",
        )
        outcome.line(f"FAIL relation uniform-401 {reference} (reference)")
    else:
        outcome.line(f"PASS relation uniform-401 {reference} (reference)")
    for case_id in ids[1:]:
        other = _relational_form(records[case_id])
        outcome.assertions += 1
        if other != ref:
            outcome.fail(
                case_id,
                "is distinguishable from the uniform 401 "
                f"({reference}). An error that discriminates is an enumeration "
                "API.\n" + _diff(ref, other, reference, case_id),
            )
            outcome.line(f"FAIL relation uniform-401 {case_id}")
        else:
            outcome.line(f"PASS relation uniform-401 {case_id}")


def check_scope_pairs(
    corpus: cases_mod.Corpus,
    records: dict[str, dict[str, Any]],
    outcome: Outcome,
    skipped: "set[str] | None" = None,
    failed: "set[str] | None" = None,
) -> None:
    """A refused scope must answer what a never-existed scope answers.

    The only licence granted is the SCOPE NAME ITSELF: a report echoes the name
    it was asked about, so the two answers can never be byte-identical and
    demanding it would be a test that cannot pass. Each pair declares the two
    names; every other byte, and every header, must match.
    """
    skipped = skipped or set()
    for pair in corpus.scope_pairs:
        if pair.refused in skipped or pair.absent in skipped:
            outcome.line(
                f"SKIP relation refused-equals-absent {pair.name} (oracle-specific)"
            )
            continue
        left = _relational_form(
            records[pair.refused],
            substitute=pair.refused_scope,
            drop_headers=("content-length",),
        )
        right = _relational_form(
            records[pair.absent],
            substitute=pair.absent_scope,
            drop_headers=("content-length",),
        )
        outcome.assertions += 1
        if left != right:
            outcome.fail(
                pair.refused,
                f"a REFUSED scope ({pair.refused_scope}) is distinguishable from an "
                f"ABSENT one ({pair.absent_scope}) for the same caller. That is an "
                f"enumeration API: the answer tells an unauthorised caller which "
                f"scopes exist.\n"
                + _diff(left, right, pair.refused, pair.absent),
            )
            outcome.line(f"FAIL relation refused-equals-absent {pair.name}")
        else:
            outcome.line(
                f"PASS relation refused-equals-absent {pair.name}"
                + _both_wrong(pair.refused, pair.absent, failed)
            )


def check_head_pairs(
    corpus: cases_mod.Corpus,
    outcome: Outcome,
    skipped: "set[str] | None" = None,
    failed: "set[str] | None" = None,
) -> None:
    """A HEAD must report the Content-Length its GET would have sent.

    🔴 THE REASON THIS IS A RELATION AND NOT A GOLDEN. A report body carries the
    machine identity and the store's path, and both vary in LENGTH between hosts
    — so `Content-Length` is host-dependent even after the body normalizations
    make the BODY host-independent. Measured: two runs on one host agree
    (a same-host determinism check is structurally blind to it), and changing
    only `CAIRN_HOST` moved 21 goldens by exactly the difference in the label's
    length. Comparing the two answers to EACH OTHER keeps the claim and drops the
    dependence.
    """
    skipped = skipped or set()
    for pair in corpus.head_pairs:
        if pair.head in skipped or pair.get in skipped:
            outcome.line(f"SKIP relation head-matches-get {pair.name} (oracle-specific)")
            continue
        head = outcome.raw_lengths.get(pair.head)
        get = outcome.raw_lengths.get(pair.get)
        outcome.assertions += 1
        if head is None or get is None or head != get:
            outcome.fail(
                pair.head,
                f"the HEAD reported Content-Length {head!r} and its GET "
                f"({pair.get}) {get!r}. A HEAD that understates the length is how "
                f"a client truncates a report it never sees the rest of.",
            )
            outcome.line(f"FAIL relation head-matches-get {pair.name}")
        else:
            outcome.line(
                f"PASS relation head-matches-get {pair.name}"
                + _both_wrong(pair.head, pair.get, failed)
            )


#: 🔴 A RELATION BETWEEN TWO RESPONSES THAT BOTH FAILED THEIR OWN GOLDEN STILL
#: PASSES, AND SAYING SO ON THE LINE IS THE ONLY THING THAT STOPS IT READING AS
#: COVERAGE. The claim these relations make is "these two answers are the SAME"; it
#: is true of two answers that are identically WRONG, so the verdict is not a defect
#: — but a reader seeing `PASS relation refused-equals-absent recall` next to a
#: failing `recall-refused-scope` would reasonably conclude the refusal path is
#: correct. MEASURED, on the Go port at P1a: both report/relation pairs passed while
#: all four members answered `501 not-implemented`, because a not-implemented answer
#: is beautifully uniform. The per-case FAIL lines carried the information and the
#: relation line contradicted them; now it carries the caveat itself.
_BOTH_WRONG = (
    " (⚠ both members failed their own golden, so this compares two answers that "
    "are not the contract)"
)


def _both_wrong(left: str, right: str, failed: "set[str] | None") -> str:
    if failed and left in failed and right in failed:
        return _BOTH_WRONG
    return ""


def _diff(left: list[str], right: list[str], left_name: str, right_name: str) -> str:
    return "\n".join(
        difflib.unified_diff(
            left, right, fromfile=left_name, tofile=right_name, lineterm="", n=1
        )
    )


# ---------------------------------------------------------------------------
# The leak guard
# ---------------------------------------------------------------------------


#: Shortest machine-identifying string the leak guard will look for. Below this
#: a hostname is indistinguishable from ordinary text.
_MIN_SECRET_CHARS = 4


def _host_secrets() -> list[str]:
    """Strings that identify THIS machine and must never reach a golden.

    🔴 THE REPORT NAMES THE MACHINE ON PURPOSE, so the generator runs on a host
    whose hostname and machine-id are in the bytes it is about to write to a
    PUBLIC repository. Two normalizations remove them; this guard is what proves
    the normalizations worked, rather than assuming they did.

    ⚠ WHAT IT CANNOT SEE: a hostname shorter than `_MIN_SECRET_CHARS`. A
    three-character host name is a substring of ordinary English and the guard
    would fire on every golden, so the short case is uncovered rather than
    wrongly covered. The direction of the residual error is stated because it is
    a choice: a legitimate golden containing the hostname as a substring fails
    the generator and a human decides, which is the right way round for a public
    repository.
    """
    out = [socket.gethostname()]
    for candidate in ("/etc/machine-id", "/var/lib/dbus/machine-id"):
        try:
            value = Path(candidate).read_text(encoding="utf-8").strip()
        except OSError:
            continue
        if len(value) >= 12:
            # The display form is a 12-character prefix; see
            # `host_identity.MACHINE_ID_DISPLAY_CHARS`.
            out.append(value[:12])
            out.append(value)
    return [s for s in out if s and len(s) >= _MIN_SECRET_CHARS]


def check_no_host_leak(text: str, where: str) -> list[str]:
    problems = []
    for secret in _host_secrets():
        if secret in text:
            problems.append(
                f"{where} contains this machine's identity. The host and machine-id "
                f"normalizations did not cover it, and this repository is PUBLIC."
            )
    return problems


# ---------------------------------------------------------------------------
# generate
# ---------------------------------------------------------------------------


def generate(
    golden_dir: Path = GOLDEN_DIR,
    corpus: cases_mod.Corpus | None = None,
    **oracle_kwargs,
) -> Outcome:
    """Boot the oracle over a freshly-built world and re-record every golden.

    `oracle_kwargs` reaches `boot_oracle` — `server_py=` in particular, which is
    how `tests/test_conformance_suite.py` points the GENERATOR at a deliberately
    wrong server and proves it refuses to record the divergence.
    """
    corpus = corpus or cases_mod.load_corpus()
    outcome = Outcome()
    with tempfile.TemporaryDirectory(prefix="cairn-conformance-") as td:
        with oracle.running_oracle(Path(td), **oracle_kwargs) as ora:
            answers = execute(ora.base_url, ora.principals, corpus, outcome)
            problems = check_declared_normalizations(corpus, answers)
            texts: dict[str, str] = {}
            records: dict[str, dict[str, Any]] = {}
            for case in corpus.cases:
                resp, _fired = answers[case.id]
                rec = wire.record(case, resp)
                text = wire.dumps(rec)
                problems += check_no_host_leak(text, f"the golden for {case.id!r}")
                records[case.id] = rec
                texts[case.id] = text
            check_uniform_401(corpus, records, outcome)
            check_scope_pairs(corpus, records, outcome)
            check_head_pairs(corpus, outcome)
            if outcome.failures:
                raise SuiteError(
                    "refusing to record goldens: the ORACLE itself violates a "
                    "relational property, so recording would bake the violation in "
                    "and the suite would then defend it.\n  "
                    + "\n  ".join(outcome.failures)
                )
            if problems:
                raise SuiteError(
                    "refusing to record goldens:\n  " + "\n  ".join(problems)
                )
            written = 0
            for case_id, text in texts.items():
                path = wire.golden_path(golden_dir, case_id)
                path.parent.mkdir(parents=True, exist_ok=True)
                path.write_text(text, encoding="utf-8")
                written += 1
                outcome.line(f"WROTE {case_id}")
            # A golden nothing declares any more is a golden the runner will
            # never read: it would sit there looking like coverage.
            declared = {c.id for c in corpus.cases}
            for stale in sorted(golden_dir.glob("*.json")):
                if stale.stem not in declared:
                    stale.unlink()
                    outcome.line(f"REMOVED {stale.stem}")
            outcome.line(
                f"GENERATED cases={written} requests={outcome.requests} "
                f"assertions={outcome.assertions}"
            )
    return outcome


# ---------------------------------------------------------------------------
# run
# ---------------------------------------------------------------------------


def compare(case: cases_mod.Case, resp: wire.Response, golden: dict[str, Any], outcome: Outcome) -> bool:
    """Compare one normalized answer against its golden. Counts every claim."""
    actual = wire.record(case, resp)
    ok = True
    for field in ("status", "reason", "headers"):
        outcome.assertions += 1
        if actual[field] != golden[field]:
            ok = False
            outcome.fail(
                case.id,
                f"{field}: got {actual[field]!r}, golden has {golden[field]!r}",
            )
    if golden["body"]["kind"] == "tar_gz_manifest":
        for field in ("members", "mtime_order"):
            outcome.assertions += 1
            if actual["body"].get(field) != golden["body"].get(field):
                ok = False
                outcome.fail(
                    case.id,
                    f"snapshot {field} differs:\n"
                    + _diff(
                        wire.dumps(golden["body"]).splitlines(),
                        wire.dumps(actual["body"]).splitlines(),
                        "golden",
                        "actual",
                    ),
                )
    else:
        outcome.assertions += 1
        if actual["body"]["sha256"] != golden["body"]["sha256"]:
            ok = False
            outcome.fail(
                case.id,
                "body differs:\n"
                + _diff(
                    golden["body"].get("text_lines", ["<binary>"]),
                    actual["body"].get("text_lines", ["<binary>"]),
                    "golden",
                    "actual",
                ),
            )
    return ok


def run(
    base_url: str,
    token_file: Path,
    golden_dir: Path = GOLDEN_DIR,
    corpus: cases_mod.Corpus | None = None,
    *,
    assert_oracle_specific: bool = True,
) -> Outcome:
    """Replay the corpus against any server and diff against the goldens.

    🔴 `assert_oracle_specific` DEFAULTS TO **TRUE**, WHICH IS THE STRICT
    DIRECTION, AND THAT IS DELIBERATE. A row marked `oracle_only` records an answer
    that is the Python server's own shape rather than a contract any implementation
    can honour (see `cases.Case.oracle_only`). Asserting it against a port fails;
    skipping it against the ORACLE would silently stop covering the oracle's real
    behaviour, which is the worse of the two, so a caller that does not say gets the
    assertion. The CLI turns it off for `--base-url` and REPORTS every skip by id.
    """
    corpus = corpus or cases_mod.load_corpus()
    principals = oracle.principals_from_token_file(token_file)
    outcome = Outcome()
    answers = execute(base_url, principals, corpus, outcome)
    # 🔴 THE CASE IS STILL ISSUED. Only the COMPARISON is skipped — because the
    # request itself is part of the run's arithmetic: the corpus issues fifteen
    # deliberate refusals from one client address and the canary at the end is what
    # proves no lockout tripped. Dropping a request would change what the limiter
    # saw, so a skipped row would quietly alter the answers of the rows around it.
    skipped = set()
    if not assert_oracle_specific:
        skipped = {c.id for c in corpus.cases if c.oracle_only}
    for problem in check_declared_normalizations(corpus, answers, skipped):
        outcome.failures.append(problem)
        outcome.line("FAIL normalization " + problem.split(":")[0])
    records: dict[str, dict[str, Any]] = {}
    failed: set[str] = set()
    for case in corpus.cases:
        resp, _fired = answers[case.id]
        if case.id in skipped:
            outcome.skipped.append(case.id)
            outcome.line(
                f"SKIP {case.id} (oracle-specific: {case.oracle_only_why})"
            )
            continue
        records[case.id] = wire.record(case, resp)
        golden = wire.read_golden(golden_dir, case.id)
        ok = compare(case, resp, golden, outcome)
        if not ok:
            failed.add(case.id)
        outcome.line(("PASS " if ok else "FAIL ") + case.id)
    check_uniform_401(corpus, records, outcome, skipped)
    check_scope_pairs(corpus, records, outcome, skipped, failed)
    check_head_pairs(corpus, outcome, skipped, failed)

    # 🔴 THE CANARY, AND IT IS NOT DECORATION. The rate limiter answers the SAME
    # uniform 401 a bad token does, so once a lockout trips every authorized
    # request also answers 401 — and a corpus full of deliberate auth failures is
    # exactly what trips it. Re-issuing an AUTHENTICATED case at the END, and
    # requiring the same answer, is what makes `SUBSYSTEM_STORE_MAX_FAILURES`
    # something this run measured rather than something it assumed.
    # It has to be a case no WRITE can change, which rules out every report read:
    # their bodies carry `entry-files=` and `newest=`. See `requests.json`.
    for case in corpus.cases:
        if not case.canary:
            continue
        resp = wire.issue(base_url, case, principals)
        outcome.requests += 1
        normalized, _fired = wire.apply_normalizations(resp, case.normalize)
        golden = wire.read_golden(golden_dir, case.id)
        ok = compare(case, normalized, golden, outcome)
        outcome.line(("PASS " if ok else "FAIL ") + case.id + " (canary, re-issued last)")
        if not ok:
            outcome.fail(
                case.id,
                "the canary answered differently at the END of the run than at the "
                "start. Either a lockout tripped (every authorized request then "
                "answers the uniform 401) or the run mutated state a read depends "
                "on. Nothing after this point in the corpus can be trusted.",
            )
    outcome.line(
        f"SUMMARY requests={outcome.requests} assertions={outcome.assertions} "
        f"failures={len(outcome.failures)} skipped={len(outcome.skipped)}"
        + (" [" + ",".join(outcome.skipped) + "]" if outcome.skipped else "")
    )
    return outcome


def run_against_oracle(golden_dir: Path = GOLDEN_DIR, corpus: cases_mod.Corpus | None = None,
                       **oracle_kwargs) -> Outcome:
    """Boot the Python oracle over a fresh world and replay against it.

    Every `oracle_only` row is ASSERTED here — this function is the one place that
    knows the server it is talking to IS `server/server.py`.
    """
    with tempfile.TemporaryDirectory(prefix="cairn-conformance-") as td:
        with oracle.running_oracle(Path(td), **oracle_kwargs) as ora:
            return run(
                ora.base_url, ora.token_file, golden_dir, corpus,
                assert_oracle_specific=True,
            )


# ---------------------------------------------------------------------------
# CLI
# ---------------------------------------------------------------------------


def _print_normalizations() -> None:
    print("DECLARED NORMALIZATIONS — each is a licence to differ, with its reason.")
    for rule in wire.NORMALIZATIONS:
        scope = "every case" if rule.always else "only cases that declare it"
        print(f"\n  {rule.name}  [{rule.field}]  ({scope})")
        print(f"      {rule.reason}")
    print("\nMEASURED DETERMINISTIC, THEREFORE PINNED:")
    for name, why in wire.NOT_NORMALIZED:
        print(f"\n  {name}\n      {why}")
    print("\nDELIBERATELY NOT ASSERTED:")
    for name, why in wire.NOT_ASSERTED:
        print(f"\n  {name}\n      {why}")


def main(argv: list[str] | None = None) -> int:
    p = argparse.ArgumentParser(prog="conformance", description=__doc__)
    sub = p.add_subparsers(dest="command", required=True)

    g = sub.add_parser("generate", help="re-record every golden from the oracle")
    g.add_argument("--golden-dir", type=Path, default=GOLDEN_DIR)

    r = sub.add_parser("run", help="replay the corpus and diff against the goldens")
    r.add_argument("--base-url", default=None, help="default: boot the Python oracle")
    r.add_argument("--token-file", type=Path, default=None)
    r.add_argument("--golden-dir", type=Path, default=GOLDEN_DIR)
    r.add_argument(
        "--oracle-specific",
        choices=("skip", "assert"),
        default="skip",
        help=(
            "what to do with a row marked `oracle_only` — a response whose shape is "
            "the Python server's own artifact rather than a contract (see "
            "`cases.Case.oracle_only`). `skip` (the default for --base-url) does NOT "
            "compare it and names every skipped id with its reason in the output and "
            "in the SUMMARY; `assert` compares it, which is what you want when "
            "--base-url points at a hand-started ORACLE rather than at a port. "
            "Omitting --base-url boots the oracle and always asserts."
        ),
    )

    sub.add_parser("normalizations", help="print the declared normalization table")

    b = sub.add_parser("build-store", help="materialise the declared world")
    b.add_argument("dest", type=Path)

    args = p.parse_args(argv)
    if args.command == "normalizations":
        _print_normalizations()
        return 0
    if args.command == "build-store":
        root = oracle.build_store(args.dest)
        tokens = oracle.mint_tokens()
        token_file = oracle.write_token_file(args.dest.parent / "tokens", tokens)
        print(f"store={root}")
        print(f"token-file={token_file}")
        print("env: " + " ".join(f"{k}={v}" for k, v in oracle.ORACLE_ENV.items()))
        return 0
    if args.command == "generate":
        outcome = generate(args.golden_dir)
        for line in outcome.lines:
            print(line)
        return 0
    if args.base_url is None:
        outcome = run_against_oracle(args.golden_dir)
    else:
        if args.token_file is None:
            p.error("--token-file is required with --base-url")
        outcome = run(
            args.base_url, args.token_file, args.golden_dir,
            assert_oracle_specific=args.oracle_specific == "assert",
        )
    for line in outcome.lines:
        print(line)
    if outcome.failures:
        print("\nFAILURES:")
        for failure in outcome.failures:
            print("  " + failure)
        return 1
    return 0


if __name__ == "__main__":  # pragma: no cover
    raise SystemExit(main())
