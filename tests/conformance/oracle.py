"""Build the fixture world, and boot the PYTHON server over it.

🔴 THIS IS THE ONLY MODULE IN THE SUITE THAT KNOWS THE ORACLE IS PYTHON, AND
THAT IS THE WHOLE POINT OF SPLITTING IT OUT. `cases.py` and `wire.py` speak
HTTP and nothing else, so the runner can be pointed at a Go binary — or at a
pod — without a line of this file being involved. What lives here is exactly
the two things a language-agnostic suite cannot avoid owning:

  * BUILDING THE WORLD. The store cannot be checked into git, because git
    stores no mtime and this contract depends on mtimes (see `world.json`). So
    the store is materialised from a declaration, and any implementation under
    test is served the same bytes with the same timestamps.
  * LAUNCHING THE ORACLE. Only the generator needs this; the runner takes a
    base URL.

🔴 THE TOKENS ARE GENERATED, NEVER CHECKED IN. A real-looking 58-character
credential committed to a PUBLIC repository is a finding whether or not it ever
authenticated anything — `tests/leakscan.py` refuses one on sight, and it is
right to. So `write_token_file` mints fresh tokens per run and the request list
names principals SYMBOLICALLY; `principals_from_token_file` maps a name back to
whatever token the server under test was actually configured with, by reading
the same file the server reads. No response body or header in this contract
contains a token, so nothing about a golden depends on their value.
"""

from __future__ import annotations

import json
import os
import secrets
import socket
import subprocess
import sys
import time
import urllib.error
import urllib.request
from dataclasses import dataclass
from pathlib import Path

#: `<root>/tests/conformance/oracle.py` -> `<root>`.
REPO_ROOT = Path(__file__).resolve().parents[2]
SUITE_DIR = Path(__file__).resolve().parent
WORLD_PATH = SUITE_DIR / "world.json"
SERVER_PY = REPO_ROOT / "server" / "server.py"

#: The principal name a bare (legacy) token row is addressed by. A bare row has
#: no identity field in the file, so the suite needs one agreed word for it.
LEGACY_PRINCIPAL = "legacy"

# 🔴 THE SERVER CONFIGURATION IS PART OF THE CONTRACT THIS SUITE RECORDS, SO IT
# IS DECLARED RATHER THAN DEFAULTED, and an implementation under test MUST be
# started with the same values or the goldens do not apply to it.
#
# 🔴 `MAX_FAILURES` IS THE ONE THAT WOULD SILENTLY DESTROY A RUN. The lockout
# answers the SAME uniform 401 a bad token does — that is the design — so once
# it trips, a perfectly authorized request also answers 401 and matches no
# golden except by accident. This corpus issues 15 deliberate refusals from one
# client address, and the six that carry a WRONG-OR-ABSENT CREDENTIAL are the
# ones the limiter counts — over the production default of 5 per minute, which
# `_count_failure` would turn into a lockout partway through the read phase.
# (A non-API path and a missing client IP are deliberately NOT counted; see the
# rate-limit note in `_handle`.) Raising the ceiling is not a convenience: without
# it the suite cannot pass twice, and with it the LOCKOUT ITSELF becomes
# something the suite cannot see (`README.md` names it under that heading).
# `wire.py`'s canary is what keeps the raised ceiling honest rather than assumed.
#
# 🔴 `CAIRN_HOST` IS DEFENCE IN DEPTH, NOT THE FIX. Every report names the
# machine whose disk it read — deliberately, because a recall that does not
# would state one host's store as the fleet's. Setting the label keeps the real
# HOSTNAME out of the bytes the generator is about to write into a PUBLIC
# repository; the `report-host-identity` normalization is what actually makes a
# golden host-independent, because the machine-id prefix is appended regardless.
# `suite.check_no_host_leak` is what proves both worked instead of assuming it.
ORACLE_ENV = {
    "SUBSYSTEM_STORE_TRUSTED_PROXIES": "127.0.0.1/32",
    "SUBSYSTEM_STORE_MAX_FAILURES": "1000000",
    "CAIRN_HOST": "conformance-oracle",
}

#: How long to wait for the oracle to answer `/healthz` after exec.
BOOT_TIMEOUT_S = 30.0


class OracleError(RuntimeError):
    """The oracle could not be built, started, or reached."""


def load_world(path: Path | None = None) -> dict:
    return json.loads((path or WORLD_PATH).read_text(encoding="utf-8"))


def _text(lines: list[str]) -> str:
    """Declared `text_lines` -> the file's exact contents.

    The lines are joined with `\\n` and NOTHING is appended, so a declaration
    ending in `""` produces a trailing newline and one that does not, does not.
    The same convention is used for golden bodies in `wire.py`, deliberately:
    one round-trip rule, in two places that both have to agree about a trailing
    newline.
    """
    return "\n".join(lines)


def build_store(dest: Path, world: dict | None = None) -> Path:
    """Materialise the declared world under `dest`. Returns `dest`.

    🔴 MTIMES ARE SET LAST AND EXPLICITLY. Writing a file sets its mtime to now,
    so a build that forgot this step would produce a store whose entry ORDER is
    whatever order the loop ran in — and `/snapshot`'s contract is that the
    order survives the archive. The declaration owns the order; nothing here
    derives it.
    """
    world = world if world is not None else load_world()
    dest.mkdir(parents=True, exist_ok=True)

    for scope, spec in world["scopes"].items():
        (dest / scope).mkdir(parents=True, exist_ok=True)
        head = spec.get("git_head_lines")
        if head is not None:
            # A real `.git` would need a real object database; `scope_revision`
            # reads `HEAD` directly and accepts a bare sha, so a bare sha is
            # what is written. Nothing here spawns git.
            git = dest / scope / ".git"
            git.mkdir(exist_ok=True)
            (git / "HEAD").write_text(_text(head), encoding="utf-8")

    stamp = world.get("seed_stamp")
    if stamp is not None:
        target = dest / stamp["name"]
        target.write_text(_text(stamp["text_lines"]), encoding="utf-8")
        os.utime(target, (stamp["mtime"], stamp["mtime"]))

    for entry in world["entries"]:
        target = dest / entry["path"]
        target.parent.mkdir(parents=True, exist_ok=True)
        target.write_text(_text(entry["text_lines"]), encoding="utf-8")

    # A second pass, AFTER every write: creating `beta-notes/newcomer-five.md`
    # inside a directory does not change a sibling's mtime, but writing a file
    # twice would, and a future declaration may list two rows for one path.
    for entry in world["entries"]:
        target = dest / entry["path"]
        os.utime(target, (entry["mtime"], entry["mtime"]))
    return dest


@dataclass(frozen=True)
class Principal:
    """One row of the token file, addressed by the name the request list uses."""

    name: str
    token: str
    identity: str | None
    scopes: tuple[str, ...] | None


def mint_tokens(world: dict | None = None) -> dict[str, str]:
    """A fresh token per declared principal. Never written to a tracked file."""
    world = world if world is not None else load_world()
    # 43 bytes of entropy renders as 58 characters, comfortably over the
    # server's 43-character floor. The server refuses a shorter one at startup.
    return {p["name"]: secrets.token_urlsafe(43) for p in world["principals"]}


def write_token_file(path: Path, tokens: dict[str, str], world: dict | None = None) -> Path:
    """Write the server's token file for the declared principals."""
    world = world if world is not None else load_world()
    rows = []
    for spec in world["principals"]:
        token = tokens[spec["name"]]
        if spec["kind"] == "legacy":
            rows.append(token)
        elif spec["kind"] == "mapped":
            rows.append(f"{token} {spec['identity']} {','.join(spec['scopes'])}")
        else:
            raise OracleError(f"unknown principal kind {spec['kind']!r}")
    path.write_text("\n".join(rows) + "\n", encoding="utf-8")
    path.chmod(0o600)
    return path


def principals_from_token_file(path: Path, world: dict | None = None) -> dict[str, Principal]:
    """Read the token file the SERVER UNDER TEST was configured with.

    🔴 MATCHED BY IDENTITY, NOT BY LINE ORDER. The runner may be pointed at a
    server somebody else started, whose file lists the same principals in a
    different order or with extra rows this corpus does not address. A bare row
    is the legacy principal — that is the file format's own rule, not a
    convention invented here.

    Raises `OracleError` when a principal the request list needs is absent,
    because the alternative is a run that silently sends no credential and
    records a corpus of 401s as if it were the contract.
    """
    world = world if world is not None else load_world()
    found: dict[str, Principal] = {}
    for raw in path.read_text(encoding="utf-8").splitlines():
        row = raw.strip()
        if not row:
            continue
        fields = row.split()
        if len(fields) == 1:
            found[LEGACY_PRINCIPAL] = Principal(LEGACY_PRINCIPAL, fields[0], None, None)
        elif len(fields) == 3:
            found[fields[1]] = Principal(
                fields[1], fields[0], fields[1], tuple(fields[2].split(","))
            )
    out: dict[str, Principal] = {}
    for spec in world["principals"]:
        key = LEGACY_PRINCIPAL if spec["kind"] == "legacy" else spec["identity"]
        if key not in found:
            raise OracleError(
                f"the token file {path} has no row for principal {spec['name']!r} "
                f"(looked for {'a bare row' if spec['kind'] == 'legacy' else key!r}). "
                f"The server under test is not configured for this corpus."
            )
        out[spec["name"]] = found[key]
    return out


def _free_port() -> int:
    """A port nothing is listening on right now.

    ⚠ RACY BY CONSTRUCTION and there is no non-racy alternative here: the
    oracle is a separate process, so it cannot inherit a socket this one bound.
    The window is microseconds and a collision presents as a loud bind failure
    in `boot_oracle`, never as a wrong answer.
    """
    with socket.socket() as s:
        s.bind(("127.0.0.1", 0))
        return int(s.getsockname()[1])


@dataclass
class Oracle:
    base_url: str
    store_root: Path
    token_file: Path
    principals: dict[str, Principal]
    process: subprocess.Popen
    log: Path


def boot_oracle(
    workdir: Path,
    world: dict | None = None,
    *,
    server_py: Path | None = None,
    extra_env: dict[str, str] | None = None,
) -> Oracle:
    """Build the world under `workdir` and start `server/server.py` over it.

    🔴 A SUBPROCESS, NOT AN IN-PROCESS `build_server`. Two reasons, and the
    second is the one that matters: an in-process server would make the
    generator depend on importing the implementation it is recording, which is
    exactly the coupling this suite exists to remove; and `main()` is where the
    env, the argv and the token-file parsing live, so booting any other way
    would record a contract nobody deploys.
    """
    world = world if world is not None else load_world()
    store = build_store(workdir / "store", world)
    tokens = mint_tokens(world)
    token_file = write_token_file(workdir / "tokens", tokens, world)
    port = _free_port()
    log = workdir / "oracle.log"
    env = dict(os.environ)
    env.update(ORACLE_ENV)
    env.update(extra_env or {})
    # The audit stream is the server's stdout. It is kept for diagnosis and is
    # deliberately NOT part of the contract this suite records — see README.md.
    handle = log.open("wb")
    proc = subprocess.Popen(
        [
            sys.executable,
            str(server_py or SERVER_PY),
            "--store", str(store),
            "--host", "127.0.0.1",
            "--port", str(port),
            "--token-file", str(token_file),
        ],
        stdout=handle,
        stderr=subprocess.STDOUT,
        env=env,
    )
    base = f"http://127.0.0.1:{port}"
    deadline = time.monotonic() + BOOT_TIMEOUT_S
    while True:
        if proc.poll() is not None:
            handle.close()
            raise OracleError(
                f"the oracle exited {proc.returncode} before answering /healthz:\n"
                + log.read_text(encoding="utf-8", errors="replace")
            )
        try:
            with urllib.request.urlopen(base + "/healthz", timeout=1.0) as resp:
                if resp.status == 200:
                    break
        except (urllib.error.URLError, OSError, TimeoutError):
            pass
        if time.monotonic() > deadline:
            proc.kill()
            handle.close()
            raise OracleError(
                f"the oracle did not answer /healthz within {BOOT_TIMEOUT_S}s:\n"
                + log.read_text(encoding="utf-8", errors="replace")
            )
        time.sleep(0.02)
    return Oracle(
        base_url=base,
        store_root=store,
        token_file=token_file,
        principals=principals_from_token_file(token_file, world),
        process=proc,
        log=log,
    )


def stop_oracle(oracle: Oracle) -> None:
    oracle.process.terminate()
    try:
        oracle.process.wait(timeout=10)
    except subprocess.TimeoutExpired:  # pragma: no cover - belt and braces
        oracle.process.kill()
        oracle.process.wait(timeout=10)


class running_oracle:  # noqa: N801 - a context manager used as `with running_oracle(...)`
    """`with running_oracle(tmp) as oracle:` — boot, yield, always terminate."""

    def __init__(self, workdir: Path, world: dict | None = None, **kwargs) -> None:
        self._workdir = workdir
        self._world = world
        self._kwargs = kwargs
        self._oracle: Oracle | None = None

    def __enter__(self) -> Oracle:
        self._oracle = boot_oracle(self._workdir, self._world, **self._kwargs)
        return self._oracle

    def __exit__(self, *_exc) -> None:
        if self._oracle is not None:
            stop_oracle(self._oracle)
