#!/usr/bin/env python3
"""The two ledgers that were blind to a COMPILED client, closed by reading the binary.

🔴 THE BLIND SPOT, NAMED. `testlib/capability_ledger.cli_verbs_from_parser` asks the PYTHON
argparse parser what subcommands it has, and `test_cairn_doctor.py`'s exit-code ledger walks the
`cairn` script's AST for its `EXIT_*` constants. Neither has an equivalent for a compiled program.
So while two clients are alive:

  * a verb the Go client GAINED, or silently LOST, leaves the capability gate green;
  * a Go exit code colliding with `doctor`'s 10 leaves the shared-set ledger green, because that
    ledger computes an intersection over the PYTHON client's nine codes.

Both are closed the same way `api.DeclaredRoutes()` and `cairn-server -routes` closed the route
ledger's: the binary prints its own tables and this file reads them out of the RUNNING process.

🔴 THIS FILE IS MEASURED IN THE JOB THAT OWNS ITS DEPENDENCY, AND A SKIP IS REFUSED THERE. Whether
it skips in the `tests` job depends on whether that runner image happens to ship a `go` toolchain —
not something this repository controls, and therefore not something asserted anywhere. The `go` job
runs this file explicitly and REFUSES on a skip, because a skip nobody counts is indistinguishable
from a pass. Greening one tier while the other stays unobservable moves a bug rather than removing
it.
"""
from __future__ import annotations

import shutil
import subprocess
import sys
import tempfile
from pathlib import Path

import pytest

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT / "tests"))

GO_MISSING = (
    "no `go` toolchain on PATH. This file is the ONLY thing that reads the Go client's own verb "
    "and exit-code tables, so a skip here is a REAL hole in the capability gate and in the "
    "shared-exit-code ledger — not a detail. The `go` CI job runs this file explicitly; if you "
    "are reading this skip in the `tests` job, that is the job that measures it."
)


@pytest.fixture(scope="module")
def go_client() -> str:
    if shutil.which("go") is None:
        pytest.skip(GO_MISSING)
    workdir = tempfile.mkdtemp(prefix="cairn-go-ledger-")
    binary = str(Path(workdir) / "cairn-go")
    proc = subprocess.run(
        ["go", "build", "-C", str(ROOT), "-o", binary, "./cmd/cairn"],
        capture_output=True, text=True,
    )
    if proc.returncode != 0:
        pytest.fail(f"the Go client did not build, so neither ledger below can be read:\n"
                    f"{proc.stderr}")
    return binary


def _lines(binary: str, flag: str) -> list[str]:
    proc = subprocess.run([binary, flag], capture_output=True, text=True, check=True)
    out = [line for line in proc.stdout.splitlines() if line.strip()]
    # 🔴 THE POSITIVE CONTROL ON THE INSTRUMENT. A binary that printed NOTHING would make every
    # comparison below pass vacuously, which is the reassuring zero this repository keeps finding —
    # and `-routes` on the server side has the same assertion for the same reason.
    assert out, (
        f"`cairn {flag}` printed NOTHING, so a ledger built from this output would agree with "
        f"anything"
    )
    return out


def test_the_go_client_declares_EXACTLY_the_pythons_verb_set(go_client):
    """🔴 BOTH DIRECTIONS, AND BOTH OPERANDS DISCOVERED.

    A verb only Go has is a capability the ledger has no row for; a verb only Python has is a verb
    the cutover would DROP. While both clients ship, the sets must be equal — and the day Python is
    retired this test is what has to be deleted deliberately rather than quietly stopping to hold.
    """
    from testlib.capability_ledger import cli_verbs_from_parser

    python_verbs = cli_verbs_from_parser()
    go_rows = _lines(go_client, "-verbs")
    go_verbs = {}
    for row in go_rows:
        name, _, effect = row.partition(" ")
        assert effect in ("reads", "writes"), f"unparseable row {row!r}"
        go_verbs[name] = effect == "writes"

    assert set(go_verbs) == set(python_verbs), (
        f"the two clients' verb sets differ. Go-only: {sorted(set(go_verbs) - set(python_verbs))}; "
        f"Python-only: {sorted(set(python_verbs) - set(go_verbs))}. A Go-only verb is a capability "
        f"the ledger has no row for; a Python-only verb is one the cutover would drop."
    )
    # 🔴 AND THE WRITE FLAG MUST AGREE, BECAUSE IT DECIDES AN EXIT CODE. `writes` is what makes an
    # unreachable store exit 7 (the record was NOT made) rather than 3 (nothing was displayed), and
    # a verb that lost the flag on one client would report a failed write as a stale read.
    disagreeing = sorted(
        verb for verb in go_verbs if go_verbs[verb] != bool(python_verbs[verb])
    )
    assert not disagreeing, (
        f"{len(disagreeing)} verb(s) disagree about whether they WRITE: {disagreeing}. That flag "
        f"decides whether an unreachable store exits 7 or 3."
    )
    # The positive control on the discovery: nine verbs were measured when this was written, and a
    # truncated read would otherwise pass the equality above by agreeing with an equally truncated
    # other side.
    assert len(go_verbs) >= 9, f"discovered only {sorted(go_verbs)}"


def test_the_go_clients_exit_codes_keep_the_shared_set_at_0_and_9(go_client):
    """🔴 THE PYTHON LEDGER COMPUTES ITS INTERSECTION OVER THE PYTHON CLIENT'S CODES, so a Go-only
    code colliding with `doctor`'s 10 leaves it green. This is that intersection over the GO side's
    two sets, read out of the binary, and it fails in both directions for the same reasons.
    """
    rows = _lines(go_client, "-exit-codes")
    client: dict[str, int] = {}
    doctor: dict[str, int] = {}
    for row in rows:
        side, name, value = row.split()
        target = client if side == "client" else doctor
        assert side in ("client", "doctor"), f"unparseable row {row!r}"
        target[name] = int(value)

    assert len(client) >= 9 and len(doctor) >= 3, (
        f"discovered {len(client)} client and {len(doctor)} doctor codes; a truncated read would "
        f"pass the intersection below vacuously"
    )
    shared = set(doctor.values()) & set(client.values())
    assert shared == {0, 9}, (
        f"the Go side's shared exit-code set is {sorted(shared)}, not [0, 9]. GROWN means a new "
        f"overlap nobody documented — and it can have arrived from either side; SHRUNK means the 9 "
        f"was renumbered and the comment that states the overlap is now false."
    )
    # 🔴 1 AND 2 MUST BOTH STAY CLEAR OF DOCTOR'S CODES, and the intersection above is blind to 1:
    # it is not a client code, so an `EXIT_DOCTOR_* = 1` leaves the shared set at {0, 9}. A doctor
    # code of 1 is indistinguishable from a crash, and one of 2 from a usage error.
    for name, value in doctor.items():
        assert value not in (1, 2), (
            f"the Go doctor code {name}={value} collides with the interpreter's crash code or "
            f"with a usage error, which no caller can tell apart"
        )


def test_the_two_clients_declare_the_SAME_exit_code_values(go_client):
    """🔴 A CODE THAT MEANT TWO THINGS ACROSS THE TWO CLIENTS WOULD BE THE WORST DEFECT THIS PHASE
    COULD SHIP, because the whole point of the write codes is that a caller branches on the NUMBER
    to decide whether a record was made. Both operands are discovered — the Python side by AST, the
    Go side out of the binary — so this fails when either moves.
    """
    from testlib import cairn_source

    python_codes = {
        name: value for name, value in cairn_source.module_constants().items()
        if name.startswith("EXIT_")
    }
    rows = _lines(go_client, "-exit-codes")
    go_codes = {}
    for row in rows:
        side, name, value = row.split()
        if side == "client":
            go_codes[name] = int(value)
    assert go_codes == python_codes, (
        f"the two clients disagree about an exit code.\nPython: {sorted(python_codes.items())}\n"
        f"Go:     {sorted(go_codes.items())}\nA number that means two things across the two "
        f"clients defeats the entire read/write code split."
    )
