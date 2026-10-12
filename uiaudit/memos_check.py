#!/usr/bin/env python3
"""uiaudit/memos_check.py — the POPULATED memos tab, driven in a real browser over a real database.

    uiaudit/memos_check.py              # run the five clauses over this tree
    uiaudit/memos_check.py --self-test  # prove each clause can go RED on a sabotage of its own

🔴 THE ONE RUNNER OF A BUILD-TAGGED TEST. `TestTheMemosTabInARealBrowser`
(`uiaudit/memos_browser_test.go`) is behind `//go:build memobrowser` because it needs PostgreSQL and
this module's ordinary `go test ./...` has none. A tagged test nobody runs is as green as one that
skips, so this script is the invocation: it stands a database up, builds the real `cairn-ui`, runs
the test by name and reads EACH CLAUSE'S OWN result line. The `memos` leg of the `uiaudit` CI job
runs it; `tests/test_uiaudit_job_ledger.py` pins the leg.

🔴 AN ORCHESTRATOR, NEVER A SECOND IMPLEMENTATION (`pwa_check.sh`'s ruling). Every assertion lives in
the Go test; nothing here inspects a page. The mutation mechanics are `tests/testlib/mutant_tree`'s —
one reused scratch copy with no `.git`, each edit restored and the restore VERIFIED by digest.

🔴 THE `uiaudit` MODULE NEVER OPENS A DATABASE CONNECTION, and this script is what keeps that true:
the schema each run takes is created and dropped HERE, with `tests/memo/memotool` from the ROOT
module (which holds the driver), and the DSN is handed to the real binary. So the nested module's
dependency graph — which `internal/depspolicy` does not govern — gains nothing.

The database is `$CAIRN_PGTEST_DSN` when set, otherwise a throwaway server this script starts
(unix socket only, removed on exit) from `initdb`/`pg_ctl`/`createdb`/`postgres` on PATH — the dev
shell's, or the flake-pinned one the CI leg puts there.

EXIT: 0 every clause PASSED · 1 a clause FAILED (or, with --self-test, a sabotage was not caught by
its own clause, with its own message) · 2 COULD NOT VOUCH — no chromium, no toolchain, no database,
a build that failed, a clause with NO result line, or a control that misbehaved. 2 is never a pass
and never a skip.

🔴 VERDICTS ARE READ FROM THE RUNNER'S OWN RESULT LINES. A clause is PASS only on its `--- PASS:`
line and FAIL only on its `--- FAIL:` line; a run that printed neither for some clause is exit 2,
because a `go test` that never reached the subtest exits non-zero exactly like one whose subtest
failed.

ENV: MEMOS_CHECK_KEEP=1 keeps the work dir; MEMOS_CHECK_PORT moves the pod's port (default 18797).
"""
from __future__ import annotations

import os
import shutil
import subprocess
import sys
import tempfile
from pathlib import Path

HERE = Path(__file__).resolve().parent
ROOT = HERE.parent
sys.path.insert(0, str(ROOT / "tests"))

from testlib import mutant_tree  # noqa: E402

TEST = "TestTheMemosTabInARealBrowser"
TAG = "memobrowser"

#: The five clauses, in run order, and the text that is each one's OWN verdict when it fails.
CLAUSES = {
    "a_seeded": "memos clause (a) seeded",
    "b_send": "memos clause (b) send",
    "c_draft": "memos clause (c) draft",
    "d_retract": "memos clause (d) retract",
    "e_reader": "memos clause (e) reader",
}

#: A control INSIDE a clause that did not hold: exit 2, not a FAIL of the clause.
CONTROL_MARK = "CONTROL:"

#: 🔴 THE SABOTAGE LEDGER: (name, the clause that must catch it, file, old, new). Each edit is the
#: narrowest expression that can be wrong, asserted to occur exactly ONCE, and each must turn ITS
#: clause red with that clause's own message. Other clauses may go red too (reported, not required).
SABOTAGES = (
    ("listing-oldest-first", "a_seeded", "internal/ui/memos.go",
     "if c := strings.Compare(b.CreatedAt, a.CreatedAt); c != 0 {",
     "if c := strings.Compare(a.CreatedAt, b.CreatedAt); c != 0 {"),
    ("send-form-never-rendered", "b_send", "internal/ui/memos.go",
     'g.If(!t.NoStore && t.CanSend && csrf != "", memoSendForm(s, csrf, t.Draft)),',
     'g.If(false, memoSendForm(s, csrf, t.Draft)),'),
    ("refusal-drops-the-draft", "c_draft", "internal/ui/memos.go",
     "\tif errors.As(err, &ref) && memoDraftKept[ref.Reason] {\n",
     "\tif errors.As(err, &ref) && false {\n"),
    ("retract-is-a-no-op", "d_retract", "cmd/cairn-ui/memotab.go",
     "\treturn b.store.Retract(ctx, caller, auth, scope, id)\n",
     "\treturn nil\n"),
    ("tombstones-not-listed", "d_retract", "internal/pgstore/memos.go",
     "\t\t` WHERE expires_at > $1 AND m.scope_name = $2 ORDER BY m.created_at, m.id LIMIT $3`,",
     "\t\t` WHERE `+memoLive+` AND m.scope_name = $2 ORDER BY m.created_at, m.id LIMIT $3`,"),
    ("reader-shown-the-send-form", "e_reader", "internal/ui/memos.go",
     "tab := MemoTab{CanSend: memo.MaySend(id.Auth, scope.Name),",
     "tab := MemoTab{CanSend: memo.MayRead(id.Auth, scope.Name),"),
    ("reader-shown-retract-buttons", "e_reader", "internal/ui/memos.go",
     'row.Retractable = row.Tombstone == "" && memo.MayRetractMemo(id.Principal, id.Auth, st)',
     'row.Retractable = row.Tombstone == ""'),
)


class CouldNotVouch(Exception):
    pass


def say(msg: str) -> None:
    print(f"memos_check: {msg}", flush=True)


class Database:
    """`$CAIRN_PGTEST_DSN`, or a throwaway server; hands out one SCHEMA per run and drops it."""

    def __init__(self, work: Path, memotool: Path):
        self.memotool = memotool
        self.datadir: Path | None = None
        self.sockdir: Path | None = None
        self.n = 0
        dsn = os.environ.get("CAIRN_PGTEST_DSN", "")
        if dsn:
            self.base = dsn
            say("database: supplied by CAIRN_PGTEST_DSN (not echoed — a DSN can carry a password)")
        else:
            for tool in ("initdb", "pg_ctl", "createdb", "postgres"):
                if shutil.which(tool) is None:
                    raise CouldNotVouch(
                        f"CAIRN_PGTEST_DSN is unset and `{tool}` is not on PATH — run inside the dev "
                        "shell (nix develop) or set CAIRN_PGTEST_DSN to a throwaway database")
            # A SHORT socket directory: a unix socket path is capped at 107 bytes, and a scratch
            # TMPDIR can be longer than that on its own (`tests/memo/world.sh` measured it).
            self.sockdir = Path(tempfile.mkdtemp(prefix="cmb-", dir="/tmp"))
            datadir = work / "pgdata"
            run = subprocess.run(["initdb", "-D", str(datadir), "-A", "trust", "-U", "cairn_memos", "--no-sync"],
                                 capture_output=True, text=True)
            if run.returncode != 0:
                raise CouldNotVouch(f"initdb failed: {run.stdout[-400:]}{run.stderr[-400:]}")
            # 🔴 `listen_addresses=''`: a unix socket and NO TCP listener, `tests/pgtest/run.sh`'s
            # safety property — a throwaway database is never briefly a network service.
            run = subprocess.run(["pg_ctl", "-D", str(datadir), "-o",
                                  f"-k '{self.sockdir}' -c listen_addresses='' -c fsync=off",
                                  "-l", str(work / "pg.log"), "-w", "start"], capture_output=True, text=True)
            if run.returncode != 0:
                raise CouldNotVouch(f"the throwaway server did not start: {run.stdout[-400:]}{run.stderr[-400:]}")
            self.datadir = datadir
            run = subprocess.run(["createdb", "-h", str(self.sockdir), "-U", "cairn_memos", "cairn_memos"],
                                 capture_output=True, text=True)
            if run.returncode != 0:
                raise CouldNotVouch(f"createdb failed: {run.stderr[-400:]}")
            self.base = f"host={self.sockdir} user=cairn_memos dbname=cairn_memos sslmode=disable"
            say("database: a throwaway server on a unix socket (removed on exit)")
        # The server answers for itself: a tag or a PATH entry is a claim, `SHOW` is the artefact.
        say(f"server : PostgreSQL {self._sql(self.base, 'query', 'SHOW server_version').strip()}")

    def _sql(self, dsn: str, verb: str, stmt: str) -> str:
        run = subprocess.run([str(self.memotool), "sql", verb, stmt], env=dict(os.environ, MEMOTOOL_DSN=dsn),
                             capture_output=True, text=True)
        if run.returncode != 0:
            raise CouldNotVouch(f"the database refused `{stmt}`: {run.stderr.strip()[-300:]}")
        return run.stdout

    def schema(self) -> tuple[str, str]:
        """Create a schema of this run's own; return (its name, the DSN pointed at it)."""
        self.n += 1
        name = f"memobrowser_{os.getpid()}_{self.n}"
        self._sql(self.base, "exec", f'CREATE SCHEMA "{name}"')
        # `options=-csearch_path=<schema>`, attached with no space inside: libpq splits `options`
        # on whitespace. `public` is NOT on the path, so a missing table cannot resolve to a
        # leftover one (`internal/pgstore`'s harness says why).
        if self.base.startswith(("postgres://", "postgresql://")):
            sep = "&" if "?" in self.base else "?"
            return name, f"{self.base}{sep}options=-csearch_path%3D{name}"
        return name, f"{self.base} options=-csearch_path={name}"

    def drop(self, name: str) -> None:
        try:
            self._sql(self.base, "exec", f'DROP SCHEMA IF EXISTS "{name}" CASCADE')
        except CouldNotVouch as err:
            say(f"⚠ leaked schema {name}: {err}")

    def close(self) -> None:
        # 🔴 STOPPED BY DATA DIRECTORY, NEVER BY A PROCESS-NAME PATTERN — `tests/pgtest/run.sh`'s rule.
        if self.datadir is not None:
            subprocess.run(["pg_ctl", "-D", str(self.datadir), "-m", "immediate", "stop"], capture_output=True)
        if self.sockdir is not None:
            shutil.rmtree(self.sockdir, ignore_errors=True)


def build(tree: Path, out: Path, pkg: str, name: str) -> Path:
    out.mkdir(parents=True, exist_ok=True)
    binary = out / name
    run = subprocess.run(["go", "build", "-o", str(binary), pkg], cwd=tree, capture_output=True, text=True)
    if run.returncode != 0 or not binary.exists():
        raise CouldNotVouch(f"{pkg} did not build from {tree}:\n{run.stdout[-1500:]}{run.stderr[-1500:]}")
    return binary


def run_clauses(tree: Path, out: Path, db: Database, port: str) -> str:
    """Build `cairn-ui` from `tree`, run the tagged test from `tree/uiaudit`, return its output."""
    ui = build(tree, out / "bin", "./cmd/cairn-ui", "cairn-ui")
    # The tagged file is invisible to the module's ordinary `go vet ./...`, so it is vetted here.
    vet = subprocess.run(["go", "vet", "-tags", TAG, "."], cwd=tree / "uiaudit", capture_output=True, text=True)
    if vet.returncode != 0:
        raise CouldNotVouch(f"`go vet -tags {TAG}` failed in uiaudit:\n{vet.stdout[-1500:]}{vet.stderr[-1500:]}")
    schema, dsn = db.schema()
    try:
        env = dict(os.environ, UIAUDIT_REPO_ROOT=str(tree), UIAUDIT_CAIRN_UI=str(ui), UIAUDIT_MEMO_DSN=dsn,
                   UIAUDIT_MEMO_PORT=port)
        run = subprocess.run(["go", "test", "-tags", TAG, "-count=1", "-v", "-run", f"^{TEST}$", "."],
                             cwd=tree / "uiaudit", env=env, capture_output=True, text=True)
    finally:
        db.drop(schema)
    log = run.stdout + run.stderr
    (out / "test.log").write_text(log, encoding="utf-8")
    return log


def verdict(log: str, clause: str) -> str:
    if f"--- FAIL: {TEST}/{clause} " in log:
        return "FAIL"
    if f"--- PASS: {TEST}/{clause} " in log:
        return "PASS"
    return "NONE"


def report(log: str) -> int:
    """Print one line per clause; return 0 (all PASS), 1 (a FAIL) or raise CouldNotVouch."""
    if CONTROL_MARK in log:
        lines = [ln.strip() for ln in log.splitlines() if CONTROL_MARK in ln]
        raise CouldNotVouch("a control inside a clause did not hold: " + lines[0][:300])
    failed = 0
    for clause, own in CLAUSES.items():
        v = verdict(log, clause)
        say(f"{clause:<10} {v}")
        if v == "NONE":
            tail = "\n".join(log.splitlines()[-25:])
            raise CouldNotVouch(f"clause {clause} produced NO result line — the test did not reach it:\n{tail}")
        if v == "FAIL":
            failed += 1
            for ln in [ln.strip() for ln in log.splitlines() if own in ln][:3]:
                say(f"    {ln[:300]}")
    say(f"{len(CLAUSES)} clause(s): {len(CLAUSES) - failed} PASS, {failed} FAIL")
    return 1 if failed else 0


def self_test(work: Path, db: Database, port: str) -> int:
    tree = work / "tree"
    # ONE scratch copy, reused: a fresh directory per sabotage is a cold build each time
    # (`mutant_tree`'s measurement). It carries no `.git`, so nothing in it can reach the branch.
    mutant_tree.copy_module(ROOT, tree)
    say("positive control (an UNEDITED copy) ...")
    log = run_clauses(tree, work / "control", db, port)
    if report(log) != 0:
        raise CouldNotVouch("positive control: an UNEDITED copy did not pass every clause, so every sabotage "
                            "below would score caught for a reason that has nothing to do with it")
    say(f"positive control PASSED all {len(CLAUSES)} clause(s)")
    caught = 0
    for name, clause, rel, old, new in SABOTAGES:
        def refuse(found: int, name: str = name, rel: str = rel) -> BaseException:
            return CouldNotVouch(f"sabotage {name}: its anchor occurs {found} time(s) in {rel}, want exactly 1 — "
                                 "the source moved and the ledger did not")
        with mutant_tree.mutated(tree / rel, old, new, 1, refuse):
            log = run_clauses(tree, work / name, db, port)
        own = CLAUSES[clause]
        v = verdict(log, clause)
        also = [c for c in CLAUSES if c != clause and verdict(log, c) == "FAIL"]
        if v == "FAIL" and own in log:
            caught += 1
            line = next(ln.strip() for ln in log.splitlines() if own in ln)
            say(f"sabotage {name:<30} CAUGHT by {clause}" + (f" (also red: {' '.join(also)})" if also else ""))
            say(f"    {line[:260]}")
        else:
            say(f"sabotage {name:<30} NOT CAUGHT (own clause {clause}: {v}; red elsewhere: {' '.join(also) or 'none'})")
            say("    " + "\n    ".join(log.splitlines()[-15:]))
    print(f"SUMMARY memos-check-self-test: sabotaged={len(SABOTAGES)} caught={caught}", flush=True)
    return 0 if caught == len(SABOTAGES) else 1


def main(argv: list[str]) -> int:
    if argv not in ([], ["--self-test"]):
        print(f"usage: {sys.argv[0]} [--self-test]", file=sys.stderr)
        return 2
    work = Path(tempfile.mkdtemp(prefix="memos-check-"))
    db: Database | None = None
    try:
        chromium = next((c for c in ("chromium", "chromium-browser", "google-chrome", "google-chrome-stable",
                                     "headless-shell") if shutil.which(c)), None)
        if chromium is None:
            raise CouldNotVouch("no chromium on PATH (looked for chromium, chromium-browser, google-chrome, "
                                "google-chrome-stable, headless-shell)")
        if shutil.which("go") is None:
            raise CouldNotVouch("no go toolchain on PATH, so no cairn-ui can be built")
        version = subprocess.run([chromium, "--version"], capture_output=True, text=True).stdout.strip()
        say(f"chromium: {version or chromium + ' (version unreadable)'}")
        say(f"work dir {work}")
        # The schema tool is built from THIS tree, never from a sabotaged copy: it is the harness's.
        memotool = build(ROOT, work / "bin", "./tests/memo/memotool", "memotool")
        db = Database(work, memotool)
        port = os.environ.get("MEMOS_CHECK_PORT", "18797")
        if argv == ["--self-test"]:
            return self_test(work, db, port)
        return report(run_clauses(ROOT, work / "check", db, port))
    except CouldNotVouch as err:
        print(f"memos_check: COULD NOT VOUCH — {err}", file=sys.stderr)
        print("memos_check: (re-run with MEMOS_CHECK_KEEP=1 to read the work dir's logs)", file=sys.stderr)
        return 2
    except mutant_tree.RestoreError as err:
        print(f"memos_check: COULD NOT VOUCH — {err}", file=sys.stderr)
        return 2
    finally:
        if db is not None:
            db.close()
        if os.environ.get("MEMOS_CHECK_KEEP") == "1":
            print(f"memos_check: work dir KEPT (MEMOS_CHECK_KEEP=1): {work}", file=sys.stderr)
        else:
            shutil.rmtree(work, ignore_errors=True)


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
