"""The client read ledger — the PYTHON ORACLE's half (S11, decision 3a; `lib/read_ledger.py`).

The Go client's half is `internal/client/readledger_test.go`; the two are compared FILE AGAINST FILE
by `tests/parity/harness.py`'s `ledger-*` rows. These rows pin the oracle on its own, offline
(`--no-sync` over a cache this file builds), so a defect here is named by the oracle's own test
rather than surfacing only as a parity divergence:

  * a SERVED call records the scope it RESOLVED — a bare `recall` in a repository records the
    scope `--repo` derived, never the empty flag;
  * `--all-scopes`, `doctor` and `routes` record one `*`; `sync` and every refused call record
    nothing;
  * no session id → no file; a ledger that cannot be written changes nothing the caller sees.

RED before S11: `lib/read_ledger.py` did not exist, so every row asserting a record fails, and the
module import in the spelling row fails.
"""
from __future__ import annotations

import json
import subprocess
import sys
from pathlib import Path

import pytest

REPO = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(REPO / "tests"))
sys.path.insert(0, str(REPO / "lib"))

from testlib import env_pin  # noqa: E402

import read_ledger  # noqa: E402

CAIRN = REPO / "cairn"
SESSION = "ses-oracle-0001"


def _entry(service: str, scope: str) -> str:
    return (f"---\nservice: {service}\nscope: {scope}\n---\n\n## What it is\n\nsynthetic.\n\n"
            "## Pointers\n\n- none\n\n## Nuance / work-history\n\n- 2000-01-01: synthetic.\n")


@pytest.fixture
def world(tmp_path: Path):
    cache = tmp_path / "cache"
    for scope, ref in (("alpha-notes", "one"), ("beta-notes", "two")):
        (cache / scope).mkdir(parents=True)
        (cache / scope / f"{ref}.md").write_text(_entry(ref, scope), encoding="utf-8")
    (cache / ".sync-stamp").write_text("synced=2000-01-01T00:00:00Z\nrevision=abc\n", encoding="utf-8")
    repo = tmp_path / "repos" / "alpha-notes"
    repo.mkdir(parents=True)
    subprocess.run(["git", "init", "-q", str(repo)], check=True, timeout=60,
                   env=env_pin.sanitized_env(GIT_CONFIG_GLOBAL="/dev/null", GIT_CONFIG_SYSTEM="/dev/null"))
    tables = tmp_path / "tables"
    tables.mkdir()
    routes = tables / "routes.json"
    routes.write_text('{"alpha-notes": "personal", "beta-notes": "personal", '
                      '"ghost-scope": "no-such-instance"}\n', encoding="utf-8")
    return tmp_path, cache, repo, routes


def run(world, *argv: str, session: str | None = SESSION, state: Path | None = None):
    tmp, cache, repo, routes = world
    extra = {}
    if session is not None:
        extra["CLAUDE_CODE_SESSION_ID"] = session
    extra["XDG_STATE_HOME"] = str(state if state is not None else tmp / "state")
    env = env_pin.sanitized_env(
        HOME=str(tmp / "home"),
        CAIRN_HOST="ledger-harness",
        SUBSYSTEM_STORE_CONFIG=str(tmp / "no-such-config"),
        SUBSYSTEM_STORE_URL="http://127.0.0.1:1",
        SUBSYSTEM_STORE_TOKEN="x",
        CAIRN_ROUTES=str(routes),
        CAIRN_MIRROR_ROOT="",
        GIT_CONFIG_GLOBAL="/dev/null",
        GIT_CONFIG_SYSTEM="/dev/null",
        **extra,
    )
    return subprocess.run([sys.executable, str(CAIRN), "--cache", str(cache), "--timeout", "2", *argv],
                          cwd=str(repo), env=env, capture_output=True, text=True, timeout=120)


def ledger(world, session: str = SESSION) -> list[tuple[str, str, str]]:
    path = world[0] / "state" / "cairn" / "read-ledger" / f"{session}.jsonl"
    if not path.exists():
        return []
    out = []
    for line in path.read_text(encoding="utf-8").splitlines():
        rec = json.loads(line)
        out.append((rec["verb"], rec["instance"], rec["scope"]))
    return out


def clear(world, session: str = SESSION) -> None:
    path = world[0] / "state" / "cairn" / "read-ledger" / f"{session}.jsonl"
    path.unlink(missing_ok=True)


STAR = "*"
CASES = [
    (["recall", "--no-sync"], [("recall", "personal", "alpha-notes")]),
    (["recall", "--no-sync", "--scope", "beta-notes"], [("recall", "personal", "beta-notes")]),
    (["search", "synthetic", "--no-sync", "--scope", "beta-notes"], [("search", "personal", "beta-notes")]),
    (["search", "synthetic", "--no-sync", "--all-scopes"], [("search", "personal", STAR)]),
    (["validate", "--no-sync"], [("validate", "personal", "alpha-notes"), ("validate", "personal", "beta-notes")]),
    (["validate", "--no-sync", "--scope", "beta-notes"], [("validate", "personal", "beta-notes")]),
    (["ls-entries", "--no-sync"], [("ls-entries", "personal", "alpha-notes"), ("ls-entries", "personal", "beta-notes")]),
    (["doctor", "--no-sync"], [("doctor", "", STAR)]),
    (["routes"], [("routes", "", STAR)]),
    (["sync"], []),
    (["recall", "--no-sync", "--bogus-flag"], []),
    (["recall", "--no-sync", "--scope", "ghost-scope"], []),
    (["append", "--scope", "ghost-scope", "--ref", "one", "--text", "x", "--session", "s"], []),
]


@pytest.mark.parametrize("argv,want", CASES, ids=[" ".join(c[0]) for c in CASES])
def test_a_served_call_records_the_scope_it_resolved(world, argv, want) -> None:
    clear(world)
    proc = run(world, *argv)
    got = ledger(world)
    assert got == want, f"{argv}: rc={proc.returncode}\n{proc.stdout}\n{proc.stderr}"


def test_a_record_carries_the_declared_fields_in_the_go_clients_order(world) -> None:
    clear(world)
    run(world, "recall", "--no-sync")
    path = world[0] / "state" / "cairn" / "read-ledger" / f"{SESSION}.jsonl"
    raw = path.read_text(encoding="utf-8")
    assert raw.startswith('{"schema":1,"session":"ses-oracle-0001","verb":"recall","instance":"personal",'
                          '"scope":"alpha-notes","client":"python","client_version":'), raw
    rec = json.loads(raw)
    assert list(rec) == ["schema", "session", "verb", "instance", "scope", "client", "client_version", "at"]
    assert oct(path.stat().st_mode & 0o777) == oct(0o600)
    assert oct(path.parent.stat().st_mode & 0o777) == oct(0o700)


@pytest.mark.parametrize("bad", ["", "../escape", "a/b", ".hidden"])
def test_no_usable_session_id_writes_nothing(world, bad) -> None:
    proc = run(world, "recall", "--no-sync", session=bad)
    assert proc.returncode == 0, proc.stderr
    assert not (world[0] / "state").exists(), f"session id {bad!r} wrote a state directory"


def test_the_positive_control_a_session_id_writes(world) -> None:
    run(world, "recall", "--no-sync")
    assert ledger(world) == [("recall", "personal", "alpha-notes")]


def test_a_failed_ledger_write_is_invisible(world) -> None:
    """A read-only ledger directory: stdout, stderr and the exit code byte-identical to a run with
    no session id, for a served call and a refused one."""
    directory = world[0] / "state" / "cairn" / "read-ledger"
    directory.mkdir(parents=True)
    directory.chmod(0o500)
    try:
        for argv in (["recall", "--no-sync"], ["ls-entries", "--no-sync"],
                     ["recall", "--no-sync", "--scope", "ghost-scope"]):
            without = run(world, *argv, session=None)
            with_ = run(world, *argv)
            assert (with_.returncode, with_.stdout, with_.stderr) == (
                without.returncode, without.stdout, without.stderr), argv
        assert not list(directory.iterdir()), "the breaker did not break the write"
    finally:
        directory.chmod(0o700)


def test_the_go_client_spells_the_same_names() -> None:
    """`lib/read_ledger.py` cannot import `internal/client`, so the two spellings are pinned here."""
    go = (REPO / "internal" / "client" / "readledger.go").read_text(encoding="utf-8")
    for name in (read_ledger.CLAUDE_SESSION_ENV, read_ledger.OPENCODE_SESSION_ENV):
        assert f'= "{name}"' in go, name
    assert '"cairn", "read-ledger"' in go
    assert "LedgerAllScopes = \"*\"" in go and read_ledger.ALL_SCOPES == "*"
    assert f"LedgerSchema = {read_ledger.SCHEMA}" in go
