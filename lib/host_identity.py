#!/usr/bin/env python3
"""WHICH MACHINE am I — one implementation, every consumer.

🔴 A HOSTNAME IS NOT AN IDENTITY. Machines provisioned from one image commonly
report the SAME hostname, and cairn's local cache is PER-HOST and unreplicated:
two machines holding the same scope can hold different entries in it.

A tool that reads such a store and reports a GLOBAL fact ("the store has no
`billing/` scope") is stating one machine's disk as though it were the fleet's.
That is the defect this module exists to make un-writable: every consumer prints
the identity of the machine it actually read.

WHY THE LABEL ALONE IS NOT ENOUGH
---------------------------------
`host_label()` reads an operator-set environment variable and falls back to the
hostname. Under a systemd unit that variable is typically set to something
machine-specific; in an INTERACTIVE shell it is usually unset and the label
degrades to the hostname — which is exactly the value that may be shared.

So a header printing `host_label()` alone would read as coverage while providing
none: identical on the very machines it is supposed to distinguish. `this_host()`
joins the readable label to the machine id, which is distinct per host by
construction and needs no systemd.
"""
from __future__ import annotations

import os
import re
import socket
from pathlib import Path

#: Where the machine id is read from. A module-level tuple so a test can point a
#: reader at synthetic files and exercise the REAL function, rather than
#: re-implementing its shape check in the test — which would only ever prove the
#: test agrees with itself.
MACHINE_ID_FILES: tuple[str, ...] = ("/etc/machine-id", "/var/lib/dbus/machine-id")

#: What `/etc/machine-id` is defined to hold: 32 lowercase hex digits.
_MACHINE_ID_SHAPE = re.compile(r"[0-9a-f]{32}")

#: Printed instead of an id when no file could be read or none had the right
#: shape. A SENTENCE, not an empty string: the whole point of this module is that
#: a host claim is never silently unqualified.
MACHINE_ID_UNREADABLE = "machine-id-unreadable"

#: Environment variables consulted for a readable label, in precedence order.
#: A tuple rather than a hardcoded pair so a deployment can add its own without
#: editing the function.
HOST_LABEL_ENV: tuple[str, ...] = ("CAIRN_HOST", "ASIB_HOST", "ACTIVITY_HOST")


def machine_id(files: "tuple[str, ...] | None" = None) -> str | None:
    """The only reliable "which machine am I" signal available without config.

    🔴 SHAPE-CHECKED, NOT JUST NON-EMPTY. Returning whatever junk a file happened
    to hold would make a caller's "does this prefix belong to this host" answer
    True for any prefix containing it — an error in the FALSE DATA-LOSS
    direction, which is the one that gets someone to act destructively.

    Returns `None` when no candidate file is readable or none parses. The caller
    decides what an unknown machine means; this never guesses.
    """
    for p in (MACHINE_ID_FILES if files is None else files):
        try:
            v = Path(p).read_text(encoding="utf-8").strip()
        except OSError:
            continue
        if _MACHINE_ID_SHAPE.fullmatch(v):
            return v
    return None


def host_label() -> str:
    """The READABLE name of this machine, for a human reading a key or a header.

    🔴 The fallback is the hostname, which may be SHARED across machines — that
    is precisely why `this_host()` exists and why nothing should print this value
    on its own as a per-host claim.
    """
    for var in HOST_LABEL_ENV:
        v = os.environ.get(var)
        if v and v.strip():
            return re.sub(r"[^A-Za-z0-9._-]", "-", v.strip())
    return re.sub(r"[^A-Za-z0-9._-]", "-", socket.gethostname() or "unknown")


#: How much of the machine id `this_host()` prints.
#: 🔴 A PREFIX, NEVER THE WHOLE ID. `/etc/machine-id` is a stable, unique
#: installation identifier, and `this_host()` is a DISPLAY value: it lands in
#: rendered headers and JSON payloads, and tool output gets pasted into issues
#: and pull requests routinely. A dozen hex characters separate machines with
#: room to spare; the job is to tell a fleet apart, not to identify hardware.
#: 🔴 THIS DOES NOT TOUCH `machine_id()` OR `host_label()`. A caller that builds
#: a storage key from the full id must keep doing so — truncating a key prefix
#: would repoint every future object, which is a data-loss shape, not a privacy
#: fix.
MACHINE_ID_DISPLAY_CHARS = 12


def this_host() -> str:
    """An identity that DIFFERS between machines even on a hand-run.

    `<label>-<machine-id-prefix>` — see `MACHINE_ID_DISPLAY_CHARS` for why it is
    a prefix. Collapses to just the label when the label already carries the id
    (an operator-chosen shape, left as they set it), and to
    `<label>-machine-id-unreadable` when the id cannot be read at all — never to
    a bare, possibly-shared hostname that would read as a fact about the fleet.
    """
    label = host_label()
    mid = machine_id()
    if mid is None:
        return f"{label}-{MACHINE_ID_UNREADABLE}"
    if mid in label:
        return label
    return f"{label}-{mid[:MACHINE_ID_DISPLAY_CHARS]}"
