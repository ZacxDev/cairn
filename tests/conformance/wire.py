"""Issue a declared case over HTTP, normalize the answer, record or compare it.

🔴 THIS MODULE SPEAKS HTTP AND NOTHING ELSE. It imports no part of the server
and no part of the client. Everything it knows about the implementation under
test arrives as a base URL and a token file, which is what makes the same corpus
runnable against the Python oracle today and an unmodified Go binary later.

🔴 EVERY NORMALIZATION IS A NAMED ROW IN `NORMALIZATIONS`, WITH A REASON, AND A
CASE DECLARES THE ONES THAT APPLY TO IT. There is deliberately no "ignore
headers" switch: a blanket rule hides exactly the regressions this suite exists
to catch, and a normalization that is not named cannot be reviewed. Two further
properties keep the set honest:

  * A DECLARED NORMALIZATION THAT MATCHES NOTHING IS AN ERROR. Silent widening
    is the failure mode — a row that keeps a normalization it no longer needs
    carries a licence to differ that nobody can see. `apply_normalizations`
    reports which ones fired and the generator refuses a case where one did not.
  * WHAT IS *NOT* NORMALIZED IS ALSO WRITTEN DOWN, under `NOT_NORMALIZED`, so a
    reader can tell "measured deterministic" from "nobody looked".
"""

from __future__ import annotations

import base64
import hashlib
import http.client
import io
import json
import re
import socket
import tarfile
from dataclasses import dataclass, replace
from pathlib import Path
from typing import Any, Callable, Iterable

from . import cases as cases_mod

#: Replacement text for a value that is real but not reproducible. Spelled in
#: angle brackets so it cannot be mistaken for a value the server sent.
HOST_PLACEHOLDER = "<HOST-IDENTITY>"
STORE_PLACEHOLDER = "<STORE-ROOT>"
TODAY_PLACEHOLDER = "<TODAY-UTC>"
ETAG_PLACEHOLDER = '"<SHA256-16-OF-DATE-STAMPED-CONTENT>"'

_ISO_DATE = re.compile(r"\d{4}-\d{2}-\d{2}")
_ETAG_SHAPE = re.compile(r'"[0-9a-f]{16}"')


class WireError(RuntimeError):
    """The request could not be issued, or a response could not be read."""


# ---------------------------------------------------------------------------
# The response record
# ---------------------------------------------------------------------------


@dataclass(frozen=True)
class Response:
    """One answer, as the wire carried it.

    `status`/`reason` are `None` only for an HTTP/0.9 response — which this
    server really does produce, for a request line so malformed that
    `parse_request` never established a version. `send_response_only` then
    suppresses the status line and every header, so the answer is a bare body.
    Recorded as its own shape rather than faked into a 401, because "no status
    line at all" is the observable.
    """

    status: int | None
    reason: str | None
    headers: tuple[tuple[str, str], ...]
    body: bytes


# ---------------------------------------------------------------------------
# Normalizations
# ---------------------------------------------------------------------------


@dataclass(frozen=True)
class Normalization:
    """One declared licence to differ, and the reason it is granted.

    `always=True` means the rule is applied to every case and is exempt from the
    must-have-matched guard — because the only such rule is `Date`, and a
    response with no status line has no headers to carry one.
    """

    name: str
    field: str
    reason: str
    apply: Callable[[Response], Response]
    always: bool = False


def _drop_header(name: str) -> Callable[[Response], Response]:
    def _apply(resp: Response) -> Response:
        kept = tuple((k, v) for k, v in resp.headers if k.lower() != name.lower())
        return replace(resp, headers=kept)

    return _apply


def _mask_etag(resp: Response) -> Response:
    out = []
    for key, value in resp.headers:
        if key.lower() == "etag":
            if not _ETAG_SHAPE.fullmatch(value):
                raise WireError(
                    f"the ETag {value!r} is not the documented shape (16 lowercase "
                    f"hex characters in double quotes). This normalization masks a "
                    f"value it cannot pin; it must not mask one that is WRONG."
                )
            value = ETAG_PLACEHOLDER
        out.append((key, value))
    return replace(resp, headers=tuple(out))


def _sub_body(pattern: re.Pattern[bytes], repl: bytes) -> Callable[[Response], Response]:
    def _apply(resp: Response) -> Response:
        return replace(resp, body=pattern.sub(repl, resp.body))

    return _apply


#: 🔴 THE STORE'S FILESYSTEM PATH. Every report opens with `  store: <path>`.
#: The suite builds its world in a temporary directory and the pod serves
#: `/data`, so the value is a fact about the deployment and not about the
#: contract. Anchored to the line so nothing else in the report can be eaten.
_STORE_LINE = re.compile(rb"(?m)^(  store: ).*$")

#: 🔴 THE MACHINE. `render_text` prints `  host: <identity>`, and `store_host()`
#: is DELIBERATELY per-machine — a recall that does not name whose disk it read
#: would state one host's store as the fleet's. It is normalized for two
#: independent reasons: the value differs on every host that runs this suite,
#: and it embeds a 12-character prefix of `/etc/machine-id`, which is an
#: installation identifier that must not be committed to a public repository.
_HOST_LINE = re.compile(rb"(?m)^(  host: )[^ ]+(  \()")

#: The same identity, in the `scope-absent` sentence.
_HOST_SENTENCE = re.compile(
    rb"(NOTHING RECORDED YET ON THIS HOST \xe2\x80\x94 )[^']+('s store has no)"
)

#: 🔴 THE APPEND'S OWN DATE. `render_bullet` stamps `- <today>: ` from the
#: server's UTC clock, so the ONE response that echoes a line this request
#: created cannot be pinned literally. Applied to the FIRST date only, and only
#: on cases that declare it: the `duplicate` verdict echoes a line already on
#: disk, whose year-2000 date is fixture data and stays pinned.
_BULLET_DATE = re.compile(rb"^(- )\d{4}-\d{2}-\d{2}(: )")

NORMALIZATIONS: tuple[Normalization, ...] = (
    Normalization(
        name="date-header",
        field="header:Date",
        reason=(
            "HTTP/1.1 requires a Date on every response and its value is the "
            "moment the response was generated. Nothing in this contract is "
            "derived from it."
        ),
        apply=_drop_header("Date"),
        always=True,
    ),
    Normalization(
        name="snapshot-content-length",
        field="header:Content-Length",
        reason=(
            "the snapshot body is a GZIP stream, and the compressed LENGTH is a "
            "property of the compressor build rather than of this contract. A Go "
            "implementation must be free to emit a differently-sized stream over "
            "identical members, which is what the extracted-tree comparison pins "
            "instead. Content-Length is kept verbatim on every other case."
        ),
        apply=_drop_header("Content-Length"),
    ),
    Normalization(
        name="report-content-length",
        field="header:Content-Length",
        reason=(
            "🔴 A REPORT'S LENGTH IS HOST-DEPENDENT, AND THE BODY NORMALIZATIONS "
            "CANNOT FIX IT. `Content-Length` counts the bytes the server sent, "
            "which include the machine identity (`conformance-oracle-<12 hex>` "
            "here, `<label>-machine-id-unreadable` where /etc/machine-id is "
            "absent) and the store's filesystem path — both of them variable in "
            "LENGTH, not merely in value. Two runs on one host agree, so a "
            "same-host determinism check is structurally blind to this; measuring "
            "a second host label is what found it. What replaces it: the body "
            "itself stays pinned line by line, so no fact about the report's "
            "CONTENT is lost — only the redundant restatement of its length; "
            "`raw-recall-read-to-eof` measures a report's length independently of "
            "the header (see `suite._framing`); and `head-matches-get` pins that a "
            "HEAD reports the length its GET would have."
        ),
        apply=_drop_header("Content-Length"),
    ),
    Normalization(
        name="append-etag",
        field="header:ETag",
        reason=(
            "the revision is sha256 of the entry file AFTER the append, and the "
            "appended line carries the server's UTC date — so this one ETag is "
            "transitively time-derived. The value is replaced only after its SHAPE "
            "is checked (16 lowercase hex in quotes), so a malformed or missing "
            "ETag still fails. Every other ETag in the corpus is a hash of bytes "
            "the REQUEST supplied or of an untouched fixture file, and is pinned "
            "literally."
        ),
        apply=_mask_etag,
    ),
    Normalization(
        name="report-store-path",
        field="body",
        reason=(
            "the `  store: ` line names the store's filesystem path: a temporary "
            "directory here, `/data` in the pod. Line-anchored."
        ),
        apply=_sub_body(_STORE_LINE, rb"\1" + STORE_PLACEHOLDER.encode()),
    ),
    Normalization(
        name="report-host-identity",
        field="body",
        reason=(
            "the `  host: ` line names THIS machine on purpose, and the value "
            "embeds a prefix of /etc/machine-id. It differs per host, and it must "
            "not be committed to a public repository."
        ),
        apply=_sub_body(_HOST_LINE, rb"\1" + HOST_PLACEHOLDER.encode() + rb"\2"),
    ),
    Normalization(
        name="report-host-sentence",
        field="body",
        reason=(
            "the same machine identity, in the `NOTHING RECORDED YET ON THIS HOST` "
            "sentence that only the scope-absent status renders."
        ),
        apply=_sub_body(
            _HOST_SENTENCE, rb"\1" + HOST_PLACEHOLDER.encode() + rb"\2"
        ),
    ),
    Normalization(
        name="bullet-date",
        field="body",
        reason=(
            "the appended bullet is stamped with the server's UTC date at the "
            "moment of the write. Only the leading `- YYYY-MM-DD: ` is replaced, "
            "so the rest of the rendered line — including the `[cairn: "
            "actor/session]` attribution this route's whole guarantee rests on — "
            "stays pinned byte for byte."
        ),
        apply=_sub_body(_BULLET_DATE, rb"\1" + TODAY_PLACEHOLDER.encode() + rb"\2"),
    ),
)

BY_NAME = {n.name: n for n in NORMALIZATIONS}

#: 🔴 MEASURED DETERMINISTIC, AND THEREFORE PINNED. Written down because "this
#: field is not in the normalization table" is indistinguishable from "nobody
#: asked whether it was stable". Each claim below was checked by running the
#: generator twice against a freshly-built world and diffing (see README.md).
NOT_NORMALIZED: tuple[tuple[str, str], ...] = (
    (
        "Server",
        "the constant banner `subsystem-store`. It carries no version and no "
        "interpreter string, deliberately, so it is content-free and stable.",
    ),
    (
        "ETag (everywhere but the append)",
        "CONTENT-DERIVED, in two shapes. On an ENTRY route: sha256 of the entry "
        "file's bytes, truncated to 16 hex — a create and a replace hash the bytes "
        "the request sent; a duplicate and a failed precondition hash a fixture file "
        "nothing in the run writes to. On `/snapshot`: `\"sha256:<64 hex>\"` over the "
        "UNCOMPRESSED tar, which is deterministic for the same reason the extracted "
        "manifest beside it is — every member's bytes and every mtime are DECLARED by "
        "world.json, to sub-second precision. 🔴 IT IS NOT A DIGEST OF THE BODY ON THE "
        "WIRE: the gzip envelope carries the compression time and differs on every run "
        "and between implementations, which is why `snapshot-content-length` exists one "
        "table up. All of them are reproducible, so all of them are pinned literally.",
    ),
    (
        "X-Cairn-Bullet",
        "content_hash of the REQUEST's own text with whitespace collapsed. It "
        "contains no date, no actor and no session by design, so it is a pure "
        "function of the declared body.",
    ),
    (
        "X-Store-Status / X-Store-Exit",
        "the four-state vocabulary and the CLI exit code derived from it. Both "
        "are functions of what the store held.",
    ),
    (
        "X-Store-Revision",
        "read from `<store>/<scope>/.git/HEAD`, which `world.json` declares as a "
        "synthetic sha. Deterministic BECAUSE the world declares it — and it is "
        "the one header that could tell a refused scope from an absent one, "
        "which is why the fixture gives the refused scope a HEAD to leak.",
    ),
    (
        "X-Store-Snapshot",
        "`seeded=` comes from the declared stamp file and `newest=`/`entry-files=` "
        "from the declared mtimes. Time-DERIVED but not clock-derived.",
    ),
    (
        "X-Store-Entries",
        "the server's own count of tar members, compared against the extracted "
        "manifest below it.",
    ),
    (
        "Content-Length (everywhere but the snapshot and a report)",
        "the exact length of a pinned body. The two exceptions each have their "
        "own row in the normalization table, with the reason.",
    ),
    (
        "the snapshot's tar member mtimes",
        "declared in `world.json` to sub-second precision, which is the whole "
        "reason the archive is PAX. Pinned, not normalized — a normalized tar is "
        "the regression this route's docstring exists to prevent.",
    ),
)

#: 🔴 AND WHAT IS NOT ASSERTED AT ALL, for the same reason.
NOT_ASSERTED: tuple[tuple[str, str], ...] = (
    (
        "header ORDER",
        "headers are recorded sorted. `_respond` emits them in a fixed order, but "
        "RFC 9110 gives that order no meaning and a port has no reason to "
        "reproduce it. Pinning it would fail a correct implementation.",
    ),
    (
        "the raw snapshot bytes",
        "the gzip member header carries the compression time, so the archive's "
        "bytes differ on every run by construction. The extracted tree is the "
        "contract; the bytes are not.",
    ),
    (
        "a mis-framed Content-Length, on any case issued through an HTTP client",
        "the client reads EXACTLY that many bytes, so `len(body)` IS the header "
        "and the comparison is vacuous. Measured: a server sending "
        "`len(body) - 1` failed exactly ONE case before `raw-recall-read-to-eof` "
        "existed. The raw cases read to EOF and are where the claim is made.",
    ),
    (
        "the audit log",
        "one line per /api/* request on the server's stdout. It is a real "
        "contract and it is NOT an HTTP surface, so a suite that speaks HTTP "
        "cannot reach it for a server it did not start.",
    ),
)


def apply_normalizations(resp: Response, names: Iterable[str]) -> tuple[Response, set[str]]:
    """Apply the always-on rules, then the declared ones. Returns what fired.

    A rule "fired" when it changed the response. The generator turns a declared
    rule that fired on nothing into an error — see this module's docstring.
    """
    fired: set[str] = set()
    for rule in NORMALIZATIONS:
        if not rule.always:
            continue
        after = rule.apply(resp)
        if after != resp:
            fired.add(rule.name)
        resp = after
    for name in names:
        rule = BY_NAME.get(name)
        if rule is None:
            raise WireError(
                f"unknown normalization {name!r}. The declared set is: "
                + ", ".join(sorted(BY_NAME))
            )
        after = rule.apply(resp)
        if after != resp:
            fired.add(rule.name)
        resp = after
    return resp, fired


# ---------------------------------------------------------------------------
# Issuing a case
# ---------------------------------------------------------------------------


def _expand(value: Any) -> Any:
    """Expand `{"repeat": ["x", 2001]}` into a long string, recursively.

    🔴 A LENGTH LIMIT NEEDS A BODY THAT BREACHES IT, and pasting 2001 literal
    characters into `requests.json` would make the row unreviewable. The
    expansion is spelled rather than the value, so the DECLARATION states the
    intent ("one character over the limit") instead of hiding it in a wall of
    text.
    """
    if isinstance(value, dict):
        if set(value) == {"repeat"}:
            unit, count = value["repeat"]
            return str(unit) * int(count)
        return {k: _expand(v) for k, v in value.items()}
    if isinstance(value, list):
        return [_expand(v) for v in value]
    return value


def _body_bytes(case: cases_mod.Case) -> bytes | None:
    if case.body is None:
        return None
    if "text_lines" in case.body:
        return "\n".join(case.body["text_lines"]).encode("utf-8")
    if "json" in case.body:
        # Sorted keys and no spaces: the request bytes are part of the contract
        # (a bullet's X-Cairn-Bullet is a hash of them), so they may not depend
        # on dict iteration order.
        return json.dumps(
            _expand(case.body["json"]), sort_keys=True, separators=(",", ":")
        ).encode("utf-8")
    if "raw_text" in case.body:
        return case.body["raw_text"].encode("utf-8")
    raise WireError(f"case {case.id!r}: unknown body shape {sorted(case.body)}")


def _authorization(case: cases_mod.Case, principals: dict[str, Any]) -> str | None:
    if case.token_literal is not None:
        return f"Bearer {case.token_literal}"
    if case.principal is None:
        return None
    principal = principals.get(case.principal)
    if principal is None:
        raise WireError(
            f"case {case.id!r} names principal {case.principal!r}, which the token "
            f"file does not provide"
        )
    return f"Bearer {principal.token}"


def _parse_raw(data: bytes) -> Response:
    """Parse a response read straight off a socket.

    Handles the HTTP/0.9 shape (no status line, no headers) that this server
    really produces for a request line it could not parse at all.
    """
    if not data.startswith(b"HTTP/"):
        return Response(status=None, reason=None, headers=(), body=data)
    head, _, body = data.partition(b"\r\n\r\n")
    lines = head.split(b"\r\n")
    parts = lines[0].decode("latin-1").split(" ", 2)
    status = int(parts[1])
    reason = parts[2] if len(parts) > 2 else ""
    headers = []
    for line in lines[1:]:
        name, _, value = line.decode("latin-1").partition(":")
        headers.append((name.strip(), value.strip()))
    return Response(
        status=status,
        reason=reason,
        headers=tuple(sorted(headers)),
        body=body,
    )


def issue(base_url: str, case: cases_mod.Case, principals: dict[str, Any]) -> Response:
    """Put one declared case on the wire and read the whole answer back."""
    host, _, port = base_url.split("//", 1)[1].partition(":")
    if case.is_raw:
        return _issue_raw(host, int(port or 80), case, principals)
    conn = http.client.HTTPConnection(host, int(port or 80), timeout=60)
    body = _body_bytes(case)
    try:
        # `skip_accept_encoding` because an `Accept-Encoding` this suite did not
        # declare would be a header the corpus cannot see itself sending.
        conn.putrequest(
            case.method, case.target, skip_host=False, skip_accept_encoding=True
        )
        if case.client_ip is not None:
            conn.putheader("CF-Connecting-IP", case.client_ip)
        authorization = _authorization(case, principals)
        if authorization is not None:
            conn.putheader("Authorization", authorization)
        for name, value in case.headers:
            # 🔴 `putheader`, NOT a dict, so a DUPLICATED header is expressible.
            # "two CF-Connecting-IPs" is a real case in this corpus and a mapping
            # cannot say it.
            conn.putheader(name, value)
        if body is not None:
            conn.putheader("Content-Length", str(len(body)))
        conn.endheaders()
        if body is not None:
            conn.send(body)
        resp = conn.getresponse()
        payload = resp.read()
        return Response(
            status=resp.status,
            reason=resp.reason,
            headers=tuple(sorted((k, v) for k, v in resp.getheaders())),
            body=payload,
        )
    finally:
        conn.close()


#: A raw request line may need the run's own credential, and the tokens are
#: minted per run (see `oracle.py`). `{{<name>}}` is substituted with that
#: principal's token — the same indirection `principal:` gives an ordinary row,
#: spelled so it survives inside a literal header line.
#:
#: 🔴 THE BRACES ARE NOT COSMETIC. `tests/leakscan.py` refuses 20+ characters of
#: `[A-Za-z0-9+/_.-]` after the word `Bearer`, which is exactly what a credential
#: looks like — and it REFUSED the first spelling of this placeholder
#: (`__PRINCIPAL_wide-reader__`, 25 characters of that class). That is the gate
#: working, not a false positive: a placeholder indistinguishable from a token is
#: a placeholder a reviewer cannot tell from a token either. A brace is outside
#: the class, so the pattern cannot begin to match.
_PRINCIPAL_REF = re.compile(r"\{\{([a-z0-9-]+)\}\}")


def _issue_raw(
    host: str, port: int, case: cases_mod.Case, principals: dict[str, Any]
) -> Response:
    """Write literal request bytes and read until the peer closes.

    Every case that reaches here is answered with `Connection: close` (or with a
    bare HTTP/0.9 body), so read-to-EOF is the whole response — which is the only
    way this suite can observe a body length that `Content-Length` did not
    dictate. See `suite._framing`.
    """

    def _token(match: re.Match[str]) -> str:
        name = match.group(1)
        principal = principals.get(name)
        if principal is None:
            raise WireError(
                f"case {case.id!r} names principal {name!r} in a raw request line, "
                f"which the token file does not provide"
            )
        return principal.token

    lines = [_PRINCIPAL_REF.sub(_token, line) for line in (case.raw_request or ())]
    payload = "\r\n".join(lines).encode("latin-1")
    sock = socket.create_connection((host, port), timeout=60)
    try:
        sock.sendall(payload)
        chunks = []
        while True:
            chunk = sock.recv(65536)
            if not chunk:
                break
            chunks.append(chunk)
    finally:
        sock.close()
    return _parse_raw(b"".join(chunks))


# ---------------------------------------------------------------------------
# Recording
# ---------------------------------------------------------------------------


def snapshot_manifest(body: bytes) -> dict[str, Any]:
    """The EXTRACTED tree of a `/snapshot` response, never its bytes.

    🔴 THE RAW ARCHIVE CANNOT BE A GOLDEN AND THE REASON IS NOT COMPRESSION
    NOISE ALONE. Two independent facts:

      * the gzip member header carries the compression time, so the bytes differ
        on every run;
      * the members' mtimes are PRESERVED WITH SUB-SECOND PRECISION on purpose
        (that is why the archive is PAX_FORMAT). The reader orders its index
        newest-first by entry mtime, so a tar built with normalized mtimes
        reorders every digest rendered from the extracted copy — same bytes,
        same count, different order, no error. See `_snapshot`'s docstring.

    So what is recorded is what a client actually consumes: each member's path,
    size, content hash and mtime, IN ARCHIVE ORDER, plus `mtime_order` — the
    member names sorted newest-first, which is the ordering the reader derives.
    `mtime_order` is the relationship restated: it is the thing a normalized tar
    destroys while every path and every byte of content still matches.
    """
    with tarfile.open(fileobj=io.BytesIO(body), mode="r:gz") as tar:
        members = []
        for info in tar.getmembers():
            extracted = tar.extractfile(info)
            data = b"" if extracted is None else extracted.read()
            members.append(
                {
                    "name": info.name,
                    "size": info.size,
                    "mtime": repr(info.mtime),
                    "mode": oct(info.mode),
                    "uid": info.uid,
                    "gid": info.gid,
                    "uname": info.uname,
                    "gname": info.gname,
                    "sha256": hashlib.sha256(data).hexdigest(),
                    "text_lines": _maybe_lines(data),
                }
            )
    order = sorted(members, key=lambda m: (-float(m["mtime"]), m["name"]))
    return {
        "kind": "tar_gz_manifest",
        "members": members,
        "mtime_order": [m["name"] for m in order],
    }


def _maybe_lines(data: bytes) -> list[str] | None:
    try:
        text = data.decode("utf-8")
    except UnicodeDecodeError:
        return None
    if "\r" in text:
        return None
    return text.split("\n")


def record(case: cases_mod.Case, resp: Response) -> dict[str, Any]:
    """One normalized response, as the golden file stores it.

    The body is stored as LINES when it is newline-separated UTF-8 with no `\\r`,
    because a golden that moves should move one line in the diff — a single
    JSON-escaped string would make every change a one-line rewrite. The join is
    `"\\n"` with nothing appended, so a trailing newline is a trailing `""`
    element and its absence is its absence. `sha256` is over the reconstructed
    bytes: it is what makes a HAND EDIT of the lines detectable, which is the
    one thing a generated fixture must never survive.
    """
    if case.body_kind == "snapshot" and resp.body:
        body: dict[str, Any] = snapshot_manifest(resp.body)
    else:
        lines = _maybe_lines(resp.body)
        if lines is None:
            body = {
                "kind": "base64",
                "base64": base64.b64encode(resp.body).decode("ascii"),
            }
        else:
            body = {"kind": "text_lines", "text_lines": lines}
        body["sha256"] = hashlib.sha256(resp.body).hexdigest()
        body["bytes"] = len(resp.body)
    return {
        "case": case.id,
        "status": resp.status,
        "reason": resp.reason,
        "headers": [list(h) for h in resp.headers],
        "body": body,
    }


def body_from_record(rec: dict[str, Any]) -> bytes:
    """Reconstruct the recorded body bytes, and refuse an inconsistent golden."""
    body = rec["body"]
    if body["kind"] == "tar_gz_manifest":
        raise WireError("a snapshot manifest has no single body byte string")
    if body["kind"] == "base64":
        data = base64.b64decode(body["base64"])
    else:
        data = "\n".join(body["text_lines"]).encode("utf-8")
    digest = hashlib.sha256(data).hexdigest()
    if digest != body["sha256"]:
        raise WireError(
            f"golden for case {rec['case']!r} is INTERNALLY INCONSISTENT: its body "
            f"hashes to {digest} but the file records {body['sha256']}. A golden is "
            f"generated, never hand-written — regenerate it instead of editing it."
        )
    return data


def golden_path(directory: Path, case_id: str) -> Path:
    return directory / f"{case_id}.json"


def write_golden(directory: Path, rec: dict[str, Any]) -> Path:
    path = golden_path(directory, rec["case"])
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(dumps(rec), encoding="utf-8")
    return path


def dumps(rec: dict[str, Any]) -> str:
    """One serialization, used for writing AND for diffing."""
    return json.dumps(rec, indent=2, ensure_ascii=False, sort_keys=True) + "\n"


def read_golden(directory: Path, case_id: str) -> dict[str, Any]:
    path = golden_path(directory, case_id)
    if not path.is_file():
        raise WireError(
            f"no golden for case {case_id!r} at {path}. Regenerate the fixtures: "
            f"`python3 tests/conformance/suite.py generate`"
        )
    rec = json.loads(path.read_text(encoding="utf-8"))
    if rec["body"]["kind"] != "tar_gz_manifest":
        body_from_record(rec)  # integrity check, raises on a hand edit
    return rec
