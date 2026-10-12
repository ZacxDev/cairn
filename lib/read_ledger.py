"""The client read ledger — the PYTHON ORACLE's half (S11 of `claudedocs/plan-cairn-plugins.md`,
decision 3a, operator decision O11).

When this process runs inside an agent session — `CLAUDE_CODE_SESSION_ID` or `OPENCODE_SESSION_ID`
is set — every scope a SERVED call resolved and touched is appended, one JSON line per scope, to
`$XDG_STATE_HOME/cairn/read-ledger/<session>.jsonl`. It is the Go client's
`internal/client/readledger.go` spelled a second time, ONLY because `packages.cairn` cannot import
`internal/`; `tests/parity/` runs both clients with one session id and compares their ledger files
(`at`, `client` and `client_version` normalised), so the two cannot record different scopes. It is
on the P8 retirement ledger (`tests/parity/README.md`): it goes when the oracle does.

🔴 THE RECORD NAMES THE SCOPE THE CALL RESOLVED, NEVER THE FLAG AS TYPED, and a call whose reach
the client cannot enumerate records `*`. Every call site records at the point the content is in
hand, so a refused call records nothing.

🔴 INVISIBLE TO THE CLIENT'S CONTRACT: every error is swallowed — nothing on stdout or stderr, no
change to the exit code. The parity rows diff all three byte for byte.
"""

from __future__ import annotations

import datetime
import json
import os
import re
from pathlib import Path

CLAUDE_SESSION_ENV = "CLAUDE_CODE_SESSION_ID"
OPENCODE_SESSION_ENV = "OPENCODE_SESSION_ID"
STATE_HOME_ENV = "XDG_STATE_HOME"
SCHEMA = 1
ALL_SCOPES = "*"
CLIENT = "python"
#: The oracle carries no build revision; the Go client reports its build's. Informational only —
#: the parity row normalises it.
CLIENT_VERSION = "oracle"

#: The pod's own root-id grammar (`internal/write.SessionComponent`, `server/server.py`'s
#: `SESSION_COMPONENT`), anchored. A value outside it is no session: it could not name a file
#: safely and the pod would refuse it as a root anyway.
_SESSION = re.compile(r"[A-Za-z0-9][A-Za-z0-9_.-]{0,63}")

_call: "_Call | None" = None


class _Call:
    def __init__(self, path: Path, session: str, verb: str) -> None:
        self.path = path
        self.session = session
        self.verb = verb
        self.seen: set[tuple[str, str]] = set()


def session_id(environ=None) -> str:
    env = os.environ if environ is None else environ
    value = env.get(CLAUDE_SESSION_ENV, "") or env.get(OPENCODE_SESSION_ENV, "")
    return value if _SESSION.fullmatch(value) else ""


def ledger_dir(environ=None) -> Path | None:
    """`$XDG_STATE_HOME/cairn/read-ledger`, `~/.local/state` when unset or not absolute."""
    env = os.environ if environ is None else environ
    base = env.get(STATE_HOME_ENV, "")
    if not base or not os.path.isabs(base):
        home = env.get("HOME", "")
        if not home:
            return None
        base = os.path.join(home, ".local", "state")
    return Path(base) / "cairn" / "read-ledger"


def begin(verb: str) -> None:
    """Open the ledger for one call of `verb` — or none, with no session id. Called by `main()`
    AFTER argparse has accepted the command line, so a usage refusal cannot record."""
    global _call
    _call = None
    try:
        session = session_id()
        directory = ledger_dir()
        if session and directory is not None:
            _call = _Call(directory / f"{session}.jsonl", session, verb)
    except Exception:  # noqa: BLE001 — invisible by contract
        _call = None


def record(instance: str, scope: str) -> None:
    """Append one record for (instance, scope), once per call. Every error is swallowed."""
    call = _call
    if call is None:
        return
    key = (instance, scope)
    if key in call.seen:
        return
    call.seen.add(key)
    try:
        _append(call, instance, scope)
    except Exception:  # noqa: BLE001 — invisible by contract: never stdout, stderr or exit code
        pass


def record_all(instance: str, scopes) -> None:
    for scope in scopes:
        record(instance, scope)


def _append(call: _Call, instance: str, scope: str) -> None:
    line = json.dumps(
        {
            "schema": SCHEMA,
            "session": call.session,
            "verb": call.verb,
            "instance": instance,
            "scope": scope,
            "client": CLIENT,
            "client_version": CLIENT_VERSION,
            "at": datetime.datetime.now(datetime.timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ"),
        },
        separators=(",", ":"),
        ensure_ascii=False,
    ) + "\n"
    os.makedirs(call.path.parent, mode=0o700, exist_ok=True)
    # ONE `O_APPEND` write, file 0600, and `O_NOFOLLOW`: a symlink planted at the ledger's name is
    # refused rather than followed — the Go client's `appendLedgerLine`.
    fd = os.open(call.path, os.O_WRONLY | os.O_APPEND | os.O_CREAT | os.O_NOFOLLOW, 0o600)
    try:
        os.write(fd, line.encode("utf-8"))
    finally:
        os.close(fd)
