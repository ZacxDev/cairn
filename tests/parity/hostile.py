#!/usr/bin/env python3
"""A deliberately HOSTILE snapshot server, so exit 5 has a parity case at all.

🔴 THE FOUR `StoreCorrupt` GUARDS ARE UNREACHABLE FROM A CORRECT POD, WHICH IS WHY THIS EXISTS.
`EXIT_CORRUPT` (5) is the one client code no honest server can produce: it means the store
answered and what it sent is not a store we will accept — a link member, a traversal member, a
duplicate member, or a count that disagrees with its own `X-Store-Entries`. Every one of those
guards was written for a server we do not fully trust, and until this file existed the parity gate
compared them at exactly zero inputs. The coverage ledger in `tests/test_parity_harness.py` is
what found that: it reads the client's own `EXIT_*` constants and refused because 5 was named by
no row.

🔴 AND THE REFUSAL MUST STAY DISTINCT FROM AN OUTAGE. An outage is absorbed into "serving from
cache" at exit 0, which is right for an outage and wrong for this — a hostile archive rendering as
a reassuring `⚠ SERVED FROM CACHE` is the whole reason `StoreCorrupt` is not a `StoreUnreachable`.
Each case below therefore asserts the refusal WITH a healthy cache present.

⚠ THE ARCHIVES ARE SYNTHETIC AND SO IS EVERY NAME IN THEM. This is a public repository.
"""
from __future__ import annotations

import gzip
import io
import tarfile
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

#: The four hostile shapes, keyed by the URL prefix that selects one.
#:
#: 🔴 THE ORDER OF THE CLIENT'S GUARDS IS THE CONTRACT, so each archive is hostile in exactly ONE
#: way. An archive carrying a link AND a traversal member would be refused by whichever guard runs
#: first and would say nothing about the other — which is how a reordering ships unnoticed.
KINDS = ("link", "traversal", "duplicate", "miscount")


def _entry_bytes(service: str) -> bytes:
    return (
        f"---\nservice: {service}\nscope: alpha-notes\n---\n\n"
        "## What it is\n\na synthetic entry.\n\n"
        "## Pointers\n\n- `x`\n\n"
        "## Nuance / work-history\n\n- 2000-01-02: a synthetic bullet.\n"
    ).encode("utf-8")


def _archive(kind: str) -> bytes:
    """One hostile (or, for `miscount`, one perfectly well-formed) gzipped tar."""
    raw = io.BytesIO()
    with tarfile.open(fileobj=raw, mode="w", format=tarfile.PAX_FORMAT) as tar:
        def add_regular(name: str, payload: bytes) -> None:
            info = tarfile.TarInfo(name)
            info.size = len(payload)
            info.mtime = 946684800
            tar.addfile(info, io.BytesIO(payload))

        # Every archive carries one legitimate member first, so the guard under test is reached
        # from a non-empty archive rather than from an empty one — a refusal on the FIRST member of
        # a one-member archive cannot tell "the guard fired" from "nothing was read".
        add_regular("alpha-notes/widget-cfg.md", _entry_bytes("widget-cfg"))
        if kind == "link":
            info = tarfile.TarInfo("alpha-notes/link-to-elsewhere.md")
            info.type = tarfile.SYMTYPE
            info.linkname = "../../../etc/passwd"
            info.mtime = 946684800
            tar.addfile(info)
        elif kind == "traversal":
            add_regular("../escaped.md", _entry_bytes("escaped"))
        elif kind == "duplicate":
            add_regular("alpha-notes/widget-cfg.md", _entry_bytes("widget-cfg"))
        elif kind == "miscount":
            pass  # well-formed; the LIE is in the header, below
        else:  # pragma: no cover - KINDS is the closed set
            raise ValueError(kind)
    return gzip.compress(raw.getvalue())


#: 🔴 THE MISCOUNT CASE IS THE ONE WHOSE ARCHIVE IS HONEST. It carries one `.md` member and
#: declares 99, which is what a TRUNCATED TRANSFER looks like — the server-side comment claims a
#: short transfer is "visible as a disagreement", and it is only visible if somebody compares.
DECLARED_ENTRIES = {"link": "2", "traversal": "2", "duplicate": "2", "miscount": "99"}


class _Handler(BaseHTTPRequestHandler):
    def do_GET(self) -> None:  # noqa: N802 - BaseHTTPRequestHandler's spelling
        kind = self.path.lstrip("/").split("/", 1)[0]
        if kind not in KINDS:
            self.send_response(404)
            self.end_headers()
            return
        body = _archive(kind)
        self.send_response(200)
        self.send_header("Content-Type", "application/x-tar")
        self.send_header("Content-Encoding", "gzip")
        self.send_header("X-Store-Entries", DECLARED_ENTRIES[kind])
        self.send_header("X-Store-Revision", "0000000000000000")
        self.send_header("X-Store-Snapshot", "seeded=2000-01-01T00:00:00Z newest=NONE entry-files=1")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def log_message(self, *_args) -> None:
        """Silent: its stderr would interleave with the harness's own verdict lines."""


def start() -> tuple[ThreadingHTTPServer, int]:
    """Start the hostile server on an ephemeral port. Returns `(server, port)`."""
    server = ThreadingHTTPServer(("127.0.0.1", 0), _Handler)
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    return server, server.server_address[1]
