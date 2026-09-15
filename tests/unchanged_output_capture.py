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
"""
from __future__ import annotations

import argparse
import json
import os
import re
import shutil
import subprocess
import sys
import tempfile
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]

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
    ("ls-entries", ["ls-entries", "--no-sync"]),
    ("validate", ["validate", "--scope", SCOPE, "--no-sync"]),
]

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

    env = dict(os.environ)
    env.update({
        "HOME": str(home),
        "CAIRN_HOST": CAPTURE_HOST,
        # 🔴 POINTED AT A FILE THAT DOES NOT EXIST, DELIBERATELY, so neither arm falls back to
        # the operator's real `~/.config` and makes the run depend on the machine it ran on.
        "SUBSYSTEM_STORE_CONFIG": str(home / ".config" / "subsystem-store" / "env"),
        "SUBSYSTEM_STORE_URL": "http://127.0.0.1:1",
        "SUBSYSTEM_STORE_TOKEN": "x" * 48,
        "CAIRN_MIRROR_ROOT": "",
        "CAIRN_ROUTES": "",
        "PYTHONDONTWRITEBYTECODE": "1",
    })
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
    """THE POSITIVE CONTROL: one character of the caveat, changed.

    🔴 IT MUST MOVE EVERY SHAPE. The sentence is printed under the `store:` line of every read,
    so a capture that did not notice this one-character edit is a capture that is reading
    something other than the client's output.
    """
    path = tree / "lib" / "entry_shape.py"
    text = path.read_text(encoding="utf-8")
    needle = "a PER-HOST CACHE"
    if text.count(needle) != 1:
        raise SystemExit(f"the control's anchor appears {text.count(needle)} times in "
                         f"{path}, not once — the perturbation would be ambiguous")
    path.write_text(text.replace(needle, "a PER-HOST cache"), encoding="utf-8")


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
        for configuration in CONFIGURATIONS:
            home, cache = build_world(work / configuration)
            base_out = capture(base_tree, home, cache, configuration, work)
            head_out = capture(head_tree, home, cache, configuration, work)
            control_out = capture(control_tree, home, cache, configuration, work)

            differences = diff_lines(base_out, head_out)
            control = diff_lines(head_out, control_out)
            total_diffs += len(differences)
            control_diffs += len(control)
            print(f"CONFIGURATION {configuration}")
            for name, body in sorted(head_out.items()):
                print(f"  CAPTURED {name}: {len(body)} bytes, "
                      f"{body.splitlines()[0] if body else 'EMPTY'}")
            print(f"  DIFF-LINES {len(differences)}   CONTROL-DIFF-LINES {len(control)}")
            for line in differences[:60]:
                print("    " + line)
            if not control:
                print("  🔴 the positive control did NOT move in this configuration")

        print(f"SUMMARY configurations={len(CONFIGURATIONS)} shapes={len(SHAPES)} "
              f"diff-lines={total_diffs} control-diff-lines={control_diffs}")
        if control_diffs == 0:
            print("REFUSING TO VOUCH: the positive control produced no diff, so an identical "
                  "capture is indistinguishable from a harness wired to nothing.", file=sys.stderr)
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
