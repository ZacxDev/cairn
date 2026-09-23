#!/usr/bin/env python3
"""(e) UNCHANGED BEHAVIOUR ON A SINGLE-INSTANCE HOST — measured, with its controls.

🔴 THE CLAIM THIS EXISTS TO MAKE, AND WHY IT NEEDED A HARNESS RATHER THAN A TEST. The
multi-instance work ships routing machinery into a client every existing host runs with ONE
store. The compatibility claim is not "the tests still pass" — it is "the BYTES a reader sees
are the same bytes" — and the only way to say that is to run the same invocations against two
TREES and diff what came out. A unit test cannot: it runs one tree.

```bash
python3 tests/unchanged_output_capture.py                    # BASE=origin/main, HEAD=this tree
python3 tests/unchanged_output_capture.py --base 871e6ff     # against a named ref
python3 tests/unchanged_output_capture.py --keep             # keep both captures for eyeballing
```

🔴 IT RUNS ITS OWN POSITIVE CONTROL ON EVERY INVOCATION, AND EXITS 2 — NOT 0 — IF THE CONTROL
DOES NOT MOVE. A byte-identity claim with no control is indistinguishable from a harness wired to
nothing: two runs of a command that never executed also compare equal. The control is arm B with
ONE character changed in `STORE_IS_PER_HOST` (`CACHE` → `cache`), which must produce a non-zero
diff in every captured shape. Exit 2 means "could not vouch", never "passed".

🔴 AND THE CONFIGURATIONS IT COVERS ARE THE POINT. The first version of this capture ran four
shapes with NO `routes.json` anywhere — the one configuration in which the old
`routes is not None or len(instances) > 1` predicate was `False` by construction, so it could not
see the defect that predicate had. **Every shape is captured twice: without a table and WITH one**,
because a one-instance host that has written a table is where the two answers differ.

## 🔴 NOTHING TRIGGERS THIS, AND THAT IS DECIDED RATHER THAN OVERLOOKED

**No CI job and no nix check runs this file** — measured, zero references in
`.github/workflows/` and in `flake.nix`. An audit surfaced that as a gap: two of the call sites
`testlib/env_pin` consolidated live here, and the only thing standing behind them is a human
typing the command above.

**The decision is that it stays MANUAL, with a named trigger — and the trigger names only what
this file actually runs.** Every invocation here is `sys.executable`: `server/server.py` below,
and `cairn` at the two client sites. It drives **the Python client against the Python pod, and
nothing else.** So: run it before merging a change to `lib/`'s recall/search renderers, to
`cairn` itself (`_instance_for`, the banner, the labelling), or to `server/server.py` — the
changes whose compatibility claim is *"a one-instance host's bytes are unchanged"*.
`tests/test_narrowing_echo_sites.py` holds the sites that claim is written at; this is the
instrument that measures it.

🔴 **THE FIRST DRAFT OF THIS TRIGGER NAMED `internal/report` AND `internal/client`'s ROUTING,
AND THAT WAS FALSE.** This harness never executes a line of Go. A reader following that trigger
would have got an "identical" that is a fact about the Python client and says nothing about
either path they were changing — a worse outcome than no trigger, because it reads as coverage.
The Go side's equivalents exist and are gated: `tests/parity/harness.py` compares the two
CLIENTS, and `internal/report/testdata/reader_fixtures.json` holds the oracle's own bytes.

🔴 **WHY NOT A CI JOB, WHICH IS THE OBVIOUS ANSWER AND IS WRONG HERE.** This is a BASE-versus-HEAD
differential: it builds two trees and diffs the bytes. On a feature branch that legitimately
changes rendered output — most feature work touching this path — a blanket job is RED by
construction, and a permanently-red gate trains everyone to click through, which is worse than
no gate. The thing that makes it valuable is that a human aims it at a *compatibility claim*;
a job that ran it on everything would destroy exactly that.

⚠ **ONE KNOWN, EXPECTED DIFF: A BASE FROM BEFORE THE `SUBSYSTEM_STORE_*` -> `CAIRN_*` RENAME.**
Every shape here exports the DEPRECATED spellings — it has to, because arm A runs the base
tree and a pre-rename client and server know only those names. At HEAD those names still
work and additionally print one `cairn: … is a deprecated alias for …` line per name on
stderr, which this harness captures alongside stdout. So a run across that boundary reports a
diff consisting of exactly those lines. **That is a true report of a real user-visible
change, not a regression** — read the diff before dismissing it, because a REAL divergence
would be mixed in with it.

⚠ **AND P8 IS THIS FILE'S RETIREMENT CONDITION, NOT ITS JUSTIFICATION — THE FIRST DRAFT HAD THAT
EXACTLY BACKWARDS.** It argued for keeping the harness *because* P8 retires the Python oracle and
poses the largest byte-identity question left. But both of this file's operands ARE the Python
oracle: P8 deletes `cairn` and `server/server.py`, after which this harness cannot run at all.
Keeping a tool because of the event that removes its subject is not a reason. **The real reason
to keep it is present-tense and much smaller: while the Python client ships, this is the only
instrument that can make a base-versus-head byte claim about it, because a unit test runs one
tree and this runs two.** When `packages.cairn` goes, this goes with it — and the two lines in
`tests/test_env_pin.py` that name it as a consumer go too.

⚠ **WHAT IT STILL CANNOT VOUCH FOR, so the trigger is not mistaken for coverage.** It compares
the bytes of the shapes it declares, on one host, against one base ref. It is not a gate, nobody
is required to run it, and a change that skips it leaves no trace. The static half — that this
file's subprocess environments are built from the one predicate rather than a hand-listed copy —
IS gated, by `tests/test_env_pin.py`'s consumer ledger.
"""
from __future__ import annotations

import argparse
import json
import os
import re
import shutil
import socket
import subprocess
import sys
import tempfile
import time
import urllib.error
import urllib.request
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]

# Runnable as a script AND importable as a module, so `testlib` has to be
# reachable either way: as a script `sys.path[0]` is already this directory, but
# under an importer it is not.
sys.path.insert(0, str(Path(__file__).resolve().parent))
from testlib import env_pin  # noqa: E402

#: The host label both arms must print. 🔴 SET EXPLICITLY: the rendered report names the machine
#: it read, and an unset label would put this host's real name into a PUBLIC repository's log.
CAPTURE_HOST = "capture-harness"

#: The scope the captured world holds, and a second one the table will NOT name.
SCOPE = "alpha-notes"
UNNAMED_SCOPE = "beta-notes"

ENTRY = """---
service: widget-cfg
scope: {scope}
sensitivity: internal
---

## What it is

The widget-cfg component, synthetic.

## Pointers

- `apps/widget-cfg/values.yaml`

## Nuance / work-history

- 2000-01-02: the probe lies for 40s after a restart.
"""

#: The invocations captured, `(id, argv)`. 🔴 READS ONLY, AND OFF THE NETWORK. `--no-sync` is what
#: makes the capture deterministic: a live fetch would put a wall-clock age and a fresh snapshot
#: stamp into the banner, and the diff would then be a clock comparison.
SHAPES: list[tuple[str, list[str]]] = [
    ("recall-scope", ["recall", "--scope", SCOPE, "--no-sync"]),
    ("recall-list", ["recall", "--scope", SCOPE, "--list", "--no-sync"]),
    ("recall-unnamed-scope", ["recall", "--scope", UNNAMED_SCOPE, "--no-sync"]),
    ("search", ["search", "--scope", SCOPE, "probe", "--no-sync"]),
    # ⚠ THE CAPTURED WORLD SEEDS NO `README.md`, SO THIS SHAPE IS BLIND TO THE ONE CHANGE
    # `ls-entries` HAS HAD. A scope's `README.md` is its policy sheet and not an entry; this
    # verb listed them until the entry-file predicate was consolidated, which is a REAL
    # change to the bytes a reader sees — and a run of this harness across that commit
    # reports the shape IDENTICAL, because `build_world` below writes one entry per scope and
    # no sheet. Read that identity as "the world held no sheet", never as "the output did not
    # move". Same shape of blindness as `tests/parity/world.py` had, recorded rather than
    # closed: widening this world changes what a compatibility claim is made OVER, and no
    # gate runs this file, so the widening would be unobserved either way.
    ("ls-entries", ["ls-entries", "--no-sync"]),
    ("validate", ["validate", "--scope", SCOPE, "--no-sync"]),
]

#: The WRITE invocations captured. 🔴 THE (e) CAPTURE WAS READS-ONLY AND THEREFORE COULD NOT
#: SEE THE ONE PLACE THE OUTPUT REALLY DID CHANGE. `README.md` promised "every byte of output is
#: what it was before any of this existed" on a one-instance host, and `append`/`put`/`create`
#: print `instance=personal` unconditionally — deliberately, and pinned by a test — so the
#: promise was false on every host from the day the writes landed. Six read shapes are
#: structurally blind to that: none of them is a write. These three are what make the (e)
#: evidence match the sentence it is offered as proof of.
#:
#: ⚠ THEY NEED A LIVE STORE, WHICH IS WHY THEY ARE A SEPARATE LIST. The reads run `--no-sync`
#: against a dead URL, which is what makes them deterministic; a write that cannot reach a store
#: prints a failure and never reaches the `instance=` line at all — i.e. capturing writes
#: offline would have reproduced the blindness in a new shape.
WRITE_SHAPES: list[tuple[str, list[str]]] = [
    ("append", ["append", "--scope", SCOPE, "--ref", "widget-cfg",
                "--text", "a synthetic bullet from the capture harness",
                "--session", "capture-harness"]),
    ("put", ["put", "--scope", SCOPE, "--ref", "widget-cfg", "--file", "<REPLACEMENT>"]),
    ("create", ["create", "--scope", SCOPE, "--ref", "fresh-entry", "--file", "<REPLACEMENT>"]),
]

#: The token the captured world's store accepts. 🔴 A `TokenRecord` WITH AN IDENTITY, NOT A BARE
#: STRING: a bare token is the LEGACY row and the server refuses every WRITE through it
#: (`legacy-cannot-write`), so all three write shapes would capture the same refusal and the
#: extension would measure nothing. Synthetic filler, never a credential.
CAPTURE_TOKEN = "capture-harness-synthetic-token-000000000000"
CAPTURE_TOKEN_ROW = f"{CAPTURE_TOKEN} capture-harness {SCOPE},{UNNAMED_SCOPE}\n"

#: The two configurations every shape is captured under.
#:
#: 🔴 `with-table` IS THE ONE THE ORIGINAL CAPTURE DID NOT HAVE, and its absence is what let a
#: defect through a green (e): with no `routes.json` the old routing predicate was `False` by
#: construction, so four shapes measured the one state in which the bug could not appear.
CONFIGURATIONS = ("no-table", "with-table")


def build_tree(base: str, work: Path) -> Path:
    """A clean checkout of `base`, or of the working tree when `base` is `WORKTREE`."""
    target = work / f"tree-{re.sub(r'[^A-Za-z0-9]+', '-', base)}"
    if base == "WORKTREE":
        shutil.copytree(
            ROOT, target,
            ignore=shutil.ignore_patterns(".git", "__pycache__", ".pytest_cache", "*.pyc"))
        # 🔴 A COPY OF A WORKTREE SHARES ITS `.git` FILE, which points at the REAL git dir — a
        # commit inside the copy would land on the real branch. `ignore_patterns` above drops
        # it; this is the assertion that it did.
        assert not (target / ".git").exists(), "the copy must not carry a .git link"
        return target
    subprocess.run(["git", "-C", str(ROOT), "worktree", "add", "--detach", str(target), base],
                   check=True, capture_output=True)
    return target


def build_world(work: Path) -> tuple[Path, Path]:
    """`(home, cache)` — a one-instance host with a populated cache and no network."""
    home = work / "home"
    (home / ".config" / "subsystem-store").mkdir(parents=True)
    cache = work / "cache"
    (cache / SCOPE).mkdir(parents=True)
    (cache / SCOPE / "widget-cfg.md").write_text(ENTRY.format(scope=SCOPE), encoding="utf-8")
    (cache / UNNAMED_SCOPE).mkdir(parents=True)
    (cache / UNNAMED_SCOPE / "gauge-api.md").write_text(
        ENTRY.format(scope=UNNAMED_SCOPE).replace("widget-cfg", "gauge-api"), encoding="utf-8")
    # A stamp, so the reader reports `cached` rather than `no cache`. 🔴 FIXED FIELDS: a
    # wall-clock value here would make the two arms differ by the seconds between them.
    (cache / ".sync-stamp").write_text(
        "synced=2000-01-02T03:04:05Z\nrevision=0000000000000000\nentries=2\n", encoding="utf-8")
    return home, cache


def free_port() -> int:
    s = socket.socket()
    s.bind(("127.0.0.1", 0))
    port = s.getsockname()[1]
    s.close()
    return port


def start_store(server_tree: Path, store: Path, work: Path, name: str):
    """A live pod over a FRESH copy of the captured world, for the write shapes.

    🔴 THE SERVER ALWAYS COMES FROM ONE TREE — the HEAD one — FOR ALL THREE ARMS. Neither
    commit under comparison touches `server/`, and the claim being measured is about the
    CLIENT's bytes; running each arm against its own server would let a server difference
    masquerade as a client one, and running the CONTROL's server would apply the control's
    perturbation to both sides of the wire at once.

    🔴 AND THE STORE IS A FRESH COPY PER ARM. `append` is idempotent by CONTENT HASH and
    `create` refuses an entry that exists, so a shared store makes the second arm print
    `duplicate` and `exists` where the first printed `appended` and `created` — a diff that is
    an artefact of the ordering rather than of the code.
    """
    token_file = work / f"tokens-{name}"
    token_file.write_text(CAPTURE_TOKEN_ROW, encoding="utf-8")
    port = free_port()
    env = dict(os.environ)
    env.update({
        # 🔴 TEST-NET-1, AND NEVER THE LOOPBACK. Naming `127.0.0.1` here tells the server the
        # harness is a PROXY, after which every direct request is refused `401
        # status=no-client-ip` — all three arms then capture the same refusal and compare equal.
        # That exact copied value once made the parity gate vacuous over 72 rows.
        # 🔴 THE **DEPRECATED** SPELLINGS, AND THEY MUST STAY THAT WAY. This harness runs
        # the BASE tree's `server.py` as well as HEAD's, and a base from before the
        # `SUBSYSTEM_STORE_*` -> `CAIRN_*` rename does not know the new names at all — it
        # would refuse to start with no trusted proxies and arm A would capture a dead
        # server. Aliases resolve on HEAD, so one spelling serves both arms; the new
        # spelling would serve only one. See the note at the top of this file about the
        # diff the rename itself produces.
        "SUBSYSTEM_STORE_TRUSTED_PROXIES": "192.0.2.1/32",
        "SUBSYSTEM_STORE_MAX_FAILURES": "1000000",
        "CAIRN_HOST": CAPTURE_HOST,
    })
    log = (work / f"store-{name}.log").open("wb")
    proc = subprocess.Popen(
        [sys.executable, str(server_tree / "server" / "server.py"),
         "--store", str(store), "--host", "127.0.0.1", "--port", str(port),
         "--token-file", str(token_file)],
        stdout=log, stderr=subprocess.STDOUT, env=env)
    deadline = time.time() + 30
    while time.time() < deadline:
        if proc.poll() is not None:
            raise SystemExit(f"the capture's store exited {proc.returncode} before answering; "
                             f"see {work / f'store-{name}.log'}")
        try:
            with urllib.request.urlopen(f"http://127.0.0.1:{port}/healthz", timeout=1) as resp:
                if resp.status == 200:
                    return proc, port
        except (urllib.error.URLError, OSError):
            time.sleep(0.1)
    proc.kill()
    raise SystemExit("the capture's store never became healthy")


def capture_writes(tree: Path, home: Path, server_tree: Path, work: Path, name: str,
                   require_success: bool = True) -> dict:
    """The three write shapes, against a fresh live store.

    ⚠ `require_success` IS FALSE FOR THE CONTROL ARM, AND THE ASYMMETRY IS THE POINT. A
    refused write never reaches the `instance=` line, so on the arms whose bytes ARE the claim
    a non-zero exit is a capture that measured nothing and this refuses to vouch. The control
    arm is the opposite: it is perturbed in order to move, and one of its perturbations
    (`DEFAULT_ALIAS` → `personai`) legitimately turns the `with-table` configuration into a
    routing refusal, because the table names `personal` and that alias no longer exists.
    """
    store = work / f"store-{name}"
    if store.exists():
        shutil.rmtree(store)
    (store / SCOPE).mkdir(parents=True)
    (store / SCOPE / "widget-cfg.md").write_text(ENTRY.format(scope=SCOPE), encoding="utf-8")
    replacement = work / f"replacement-{name}.md"
    replacement.write_text(ENTRY.format(scope=SCOPE).replace("synthetic.", "replaced."),
                           encoding="utf-8")
    # 🔴 A SEPARATE FILE FOR `create`, BECAUSE `service:` MUST MATCH THE FILENAME STEM. Reusing
    # the replacement made the store refuse with `entry-shape` at rc 6 — "filename
    # 'fresh-entry.md' has slug 'fresh-entry' but `service:` normalizes to 'widget-cfg'" — and a
    # refused write never reaches the `instance=` line this capture exists to compare.
    created = work / f"created-{name}.md"
    created.write_text(
        ENTRY.format(scope=SCOPE).replace("service: widget-cfg", "service: fresh-entry")
             .replace("The widget-cfg component", "The fresh-entry component"),
        encoding="utf-8")
    # A cache of its own, so a write's live sync cannot disturb the read shapes' fixed stamp.
    cache = work / f"write-cache-{name}"

    proc, port = start_store(server_tree, store, work, name)
    # 🔴 CLEARED BY PREFIX FIRST, THEN PINNED. `dict(os.environ)` plus an
    # `update()` pins exactly the names somebody listed and inherits every other
    # one — so a sixth `CAIRN_*` variable would reach the client and move these
    # captured bytes, which is the one thing this harness exists to hold still.
    env = env_pin.sanitized_env(
        HOME=str(home),
        CAIRN_HOST=CAPTURE_HOST,
        SUBSYSTEM_STORE_CONFIG=str(home / ".config" / "subsystem-store" / "env"),
        SUBSYSTEM_STORE_URL=f"http://127.0.0.1:{port}",
        SUBSYSTEM_STORE_TOKEN=CAPTURE_TOKEN,
        CAIRN_MIRROR_ROOT="",
        CAIRN_ROUTES="",
        PYTHONDONTWRITEBYTECODE="1",
    )
    out: dict[str, str] = {}
    try:
        for shape, argv in WRITE_SHAPES:
            target = created if shape == "create" else replacement
            argv = [str(target) if a == "<REPLACEMENT>" else a for a in argv]
            run = subprocess.run(
                [sys.executable, str(tree / "cairn"), "--cache", str(cache), *argv],
                capture_output=True, text=True, env=env, cwd=str(work), timeout=120)
            body = (run.stdout + run.stderr)
            # The tree paths and the RANDOM PORT are normalised and nothing else is. A port
            # differs between arms by construction; anything else differing is the comparison.
            body = (body.replace(str(tree), "<TREE>").replace(str(work), "<WORK>")
                        .replace(f"127.0.0.1:{port}", "127.0.0.1:<PORT>")
                        .replace(str(replacement), "<REPLACEMENT>")
                        .replace(str(created), "<REPLACEMENT>"))
            out[f"write-{shape}"] = f"rc={run.returncode}\n{body}"
            # 🔴 A SHAPE THAT CAPTURED A REFUSAL MEASURES NOTHING ABOUT THE CLAIM. The
            # `instance=` field is printed on SUCCESS only, so a write the store rejected
            # contributes a reassuring "identical" to the (e) evidence while saying nothing
            # about the sentence that evidence is offered for. Refuse to vouch instead.
            if require_success and run.returncode != 0:
                raise SystemExit(
                    f"REFUSING TO VOUCH: the `{shape}` write shape exited {run.returncode} "
                    f"rather than succeeding, so it never reached the `instance=` line this "
                    f"capture exists to compare:\n{body}")
    finally:
        proc.terminate()
        try:
            proc.wait(timeout=10)
        except subprocess.TimeoutExpired:
            proc.kill()
    return out


def capture(tree: Path, home: Path, cache: Path, configuration: str, work: Path) -> dict:
    """Every shape, under one configuration, as `{id: "rc=…\\n<stdout>\\n<stderr>"}`."""
    table = home / ".config" / "subsystem-store" / "routes.json"
    if configuration == "with-table":
        # 🔴 A TABLE THAT NAMES ONE SCOPE AND NOT THE OTHER. `recall-unnamed-scope` is then the
        # row that separates "the table said nothing, resolve to the sole instance" from the
        # old predicate's refusal.
        table.write_text(json.dumps({SCOPE: "personal"}), encoding="utf-8")
    elif table.exists():
        table.unlink()

    # Cleared by prefix first, then pinned — see the sibling site above.
    env = env_pin.sanitized_env(
        HOME=str(home),
        CAIRN_HOST=CAPTURE_HOST,
        # 🔴 POINTED AT A FILE THAT DOES NOT EXIST, DELIBERATELY, so neither arm falls back to
        # the operator's real `~/.config` and makes the run depend on the machine it ran on.
        SUBSYSTEM_STORE_CONFIG=str(home / ".config" / "subsystem-store" / "env"),
        SUBSYSTEM_STORE_URL="http://127.0.0.1:1",
        SUBSYSTEM_STORE_TOKEN="x" * 48,
        CAIRN_MIRROR_ROOT="",
        CAIRN_ROUTES="",
        PYTHONDONTWRITEBYTECODE="1",
    )
    out: dict[str, str] = {}
    for name, argv in SHAPES:
        proc = subprocess.run(
            [sys.executable, str(tree / "cairn"), "--cache", str(cache), *argv],
            capture_output=True, text=True, env=env, cwd=str(work), timeout=120)
        # 🔴 THE TREE PATH IS NORMALISED AND NOTHING ELSE IS. The two arms live at different
        # paths by construction, so that one substitution is the difference that MUST be
        # allowed; normalising anything else would be hiding the comparison from itself.
        body = (proc.stdout + proc.stderr).replace(str(tree), "<TREE>").replace(str(work), "<WORK>")
        out[name] = f"rc={proc.returncode}\n{body}"
    return out


def diff_lines(a: dict, b: dict) -> list[str]:
    import difflib

    lines: list[str] = []
    for key in sorted(set(a) | set(b)):
        left, right = a.get(key, "<MISSING>"), b.get(key, "<MISSING>")
        if left == right:
            continue
        lines += list(difflib.unified_diff(
            left.splitlines(), right.splitlines(),
            fromfile=f"base/{key}", tofile=f"head/{key}", lineterm="", n=1))
    return lines


def perturb(tree: Path) -> None:
    """THE POSITIVE CONTROL — TWO of them, one per HALF of the capture.

    🔴 ONE CONTROL PER HALF, BECAUSE A CONTROL LICENSES A CONCLUSION ABOUT THE DIMENSION IT WAS
    BUILT TO TEST AND NOT A NEIGHBOURING ONE. The caveat edit moves every READ shape — the
    sentence is printed under the `store:` line of every read — and it moves NO write shape,
    because a write prints no caveat. Extending the capture to the writes while keeping only
    the read control would have added three shapes whose "identical" verdict was backed by
    nothing, which is the same blindness one layer over.

      * READS  — one character of the caveat (`CACHE` → `cache`).
      * WRITES — one character of `DEFAULT_ALIAS` (`personal` → `personai`), which is the value
        the `instance=` field of every write line carries. It cannot move a read on a
        one-instance host, and that asymmetry is exactly why it is the write half's control.
    """
    read_path = tree / "lib" / "entry_shape.py"
    text = read_path.read_text(encoding="utf-8")
    needle = "a PER-HOST CACHE"
    if text.count(needle) != 1:
        raise SystemExit(f"the read control's anchor appears {text.count(needle)} times in "
                         f"{read_path}, not once — the perturbation would be ambiguous")
    read_path.write_text(text.replace(needle, "a PER-HOST cache"), encoding="utf-8")

    write_path = tree / "lib" / "subsystem_read_store.py"
    text = write_path.read_text(encoding="utf-8")
    needle = 'DEFAULT_ALIAS = "personal"'
    if text.count(needle) != 1:
        raise SystemExit(f"the write control's anchor appears {text.count(needle)} times in "
                         f"{write_path}, not once — the perturbation would be ambiguous")
    write_path.write_text(text.replace(needle, 'DEFAULT_ALIAS = "personai"'), encoding="utf-8")


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(prog="tests/unchanged_output_capture.py")
    parser.add_argument("--base", default="origin/main",
                        help="the ref arm A is built from (default: origin/main)")
    parser.add_argument("--keep", action="store_true", help="keep both captures on disk")
    args = parser.parse_args(argv)

    work = Path(tempfile.mkdtemp(prefix="cairn-capture-"))
    built: list[Path] = []
    try:
        base_tree = build_tree(args.base, work)
        built.append(base_tree)
        head_tree = build_tree("WORKTREE", work)
        control_tree = work / "tree-control"
        shutil.copytree(head_tree, control_tree)
        perturb(control_tree)

        total_diffs = 0
        control_diffs = 0
        # 🔴 EACH HALF'S CONTROL IS COUNTED SEPARATELY. A single total lets the READ control's
        # movement vouch for the WRITE shapes, which it cannot: the caveat it perturbs is not
        # printed by any write.
        moved = {"read": 0, "write": 0}
        for configuration in CONFIGURATIONS:
            home, cache = build_world(work / configuration)
            base_out = capture(base_tree, home, cache, configuration, work)
            head_out = capture(head_tree, home, cache, configuration, work)
            control_out = capture(control_tree, home, cache, configuration, work)
            base_out.update(capture_writes(base_tree, home, head_tree, work,
                                           f"{configuration}-base"))
            head_out.update(capture_writes(head_tree, home, head_tree, work,
                                           f"{configuration}-head"))
            control_out.update(capture_writes(control_tree, home, head_tree, work,
                                              f"{configuration}-control",
                                              require_success=False))

            differences = diff_lines(base_out, head_out)
            control = diff_lines(head_out, control_out)
            total_diffs += len(differences)
            control_diffs += len(control)
            for key in set(head_out) | set(control_out):
                if head_out.get(key) != control_out.get(key):
                    moved["write" if key.startswith("write-") else "read"] += 1
            print(f"CONFIGURATION {configuration}")
            for name, body in sorted(head_out.items()):
                print(f"  CAPTURED {name}: {len(body)} bytes, "
                      f"{body.splitlines()[0] if body else 'EMPTY'}")
            print(f"  DIFF-LINES {len(differences)}   CONTROL-DIFF-LINES {len(control)}")
            for line in differences[:60]:
                print("    " + line)
            if not control:
                print("  🔴 the positive control did NOT move in this configuration")

        print(f"SUMMARY configurations={len(CONFIGURATIONS)} "
              f"shapes={len(SHAPES) + len(WRITE_SHAPES)} "
              f"(reads={len(SHAPES)} writes={len(WRITE_SHAPES)}) "
              f"diff-lines={total_diffs} control-diff-lines={control_diffs} "
              f"control-moved-reads={moved['read']} control-moved-writes={moved['write']}")
        if control_diffs == 0 or not moved["read"] or not moved["write"]:
            print("REFUSING TO VOUCH: a positive control did not move "
                  f"(reads={moved['read']}, writes={moved['write']}), so an identical capture "
                  "on that half is indistinguishable from a harness wired to nothing.",
                  file=sys.stderr)
            return 2
        return 1 if total_diffs else 0
    finally:
        if args.keep:
            print(f"captures kept at {work}")
        else:
            for tree in built:
                subprocess.run(["git", "-C", str(ROOT), "worktree", "remove", "--force", str(tree)],
                               capture_output=True)
            shutil.rmtree(work, ignore_errors=True)


if __name__ == "__main__":
    raise SystemExit(main())
