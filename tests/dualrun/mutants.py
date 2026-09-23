#!/usr/bin/env python3
"""Put a DELIBERATELY WRONG server behind the dual-run gate, so its green means something.

🔴 TWO SERVERS FAILING IDENTICALLY COMPARE EQUAL, AND THIS GATE'S WHOLE OUTPUT IS AN
EQUALITY. That is not a hypothetical failure mode in this repository: the P2 parity
harness's first full run reported 72 PASS / 0 FAIL while every request was refused
`401 status=no-client-ip` and both clients rendered `store-unreachable`
(`tests/parity/README.md`). A comparison gate therefore needs a case it MUST fail, and
that case has to be a REALISTIC divergence rather than a textbook one — a scanner that
only recognises its own examples passes a real leak.

So this module boots the ORACLE from a mutated copy and the gate has to go RED. The
mutations are the four the brief for this gate names, plus three more that reach arms the
first four cannot:

  * a STATUS CODE moved            -> the status arm
  * a response HEADER dropped      -> the header arm
  * a tar member's mtime TRUNCATED -> the uncompressed-tar arm, sub-second precision
  * the tar members REORDERED      -> the uncompressed-tar arm, member sequence
  * an AUDIT FIELD dropped         -> the audit-stream arm, which no other gate reads
  * a STARTUP BANNER FIELD dropped -> the process-stream arm, which likewise
  * a RESOLVER TIER re-keyed       -> the per-entry `?ref=` arm, and ONLY that arm

🔴 THE LAST THREE ARE WHY THIS IS NOT `tests/conformance/mutate.py`. That module symlinks
the real `lib/`, which is correct for a corpus that only ever mutates `server.py`; the
resolver mutant needs a mutated `lib/` and the gate needs to know WHICH arm killed each
mutant, not merely that something went red. A mutant killed by the wrong arm is the
"green for the wrong reason" shape one level up: it proves the gate can fail, and nothing
about the arm it was built to exercise.

🔴 AND THE POSITIVE CONTROL IS THE SAME COPY MECHANICS WITH NO EDIT. Without it, "the
mutant was caught" cannot be told apart from "the copied tree never booted at all", which
is the same green-for-the-wrong-reason the mutation exists to expose.
"""
from __future__ import annotations

import shutil
from dataclasses import dataclass
from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parents[2]
SERVER_PY = REPO_ROOT / "server" / "server.py"
LIB = REPO_ROOT / "lib"


class MutationError(AssertionError):
    """The mutation did not apply, so the control would have proven nothing."""


@dataclass(frozen=True)
class Mutation:
    """One realistic edit to the oracle, and the arm it is built to exercise.

    🔴 ATTRIBUTION IS BY WHICH *COMPARISON* FAILED, NOT BY THE FAILING TARGET'S NAME, AND
    THE FIRST DRAFT OF THIS FILE GOT THAT WRONG IN THE FLATTERING DIRECTION. It read the
    target id and then added `status` and `headers` to every attribution unconditionally —
    so the two mutants written FOR those arms were "caught by the right arm" whatever had
    actually gone red. That is the reads-as-coverage-while-providing-none shape, inside the
    control that exists to stop it. The comparisons are:

        status | headers | body | tar | audit | process

    🔴 AND THERE ARE THREE ASSERTIONS, NOT ONE, BECAUSE THE FIRST TWO ARE DIFFERENT CLAIMS
    AND THE THIRD WAS MEASURED NECESSARY:

      1. `kind` must be AMONG the failing comparisons — "the arm this mutant was written
         for is wired to something".
      2. `expect` must EQUAL them — "nothing else moved", so the kill is attributable. It
         is a per-mutation declaration rather than `{kind}`, because a mutation that changes
         the ANSWER necessarily moves more than one comparison: MEASURED on
         `resolver-tier-keyed-on-ref`, which re-resolves a ref and therefore also moves
         `X-Store-Status`, `X-Store-Exit`, `Content-Length` and the audit record's
         `status=` field. Writing `{kind}` there and calling the extra kinds a defect would
         have been a guard demanding something untrue.
      3. `only_target_arm`, when set, must EQUAL the failing target arms — which is the
         claim that makes an arm LOAD-BEARING rather than merely live. It is what says the
         per-entry `?ref=` sweep sees something no scope-level render does.
    """

    name: str
    #: `"server"` for `server/server.py`, or a filename under `lib/`.
    target: str
    old: str
    new: str
    #: The comparison this mutant was WRITTEN for. Must be among the failing ones.
    kind: str
    #: Every comparison that must fail, exactly. Declared per mutation — see assertion 2.
    expect: frozenset[str]
    #: When set, the one TARGET ARM that must fail, and the only one.
    only_target_arm: str | None
    why: str


#: 🔴 EVERY PATTERN OCCURS EXACTLY ONCE, AND THAT IS ASSERTED AT APPLY TIME. A `replace`
#: that matched nothing yields an UNMUTATED server, the gate passes, and the run is
#: reported as "this mutation was not caught" — the mutation sweep's classic false
#: SURVIVED, and the one failure mode a negative control cannot afford.
MUTATIONS: tuple[Mutation, ...] = (
    Mutation(
        name="status-code-moved",
        target="server",
        old='self._respond(405, b"read-only\\n"',
        new='self._respond(403, b"read-only\\n"',
        kind="status",
        expect=frozenset({"status"}),
        only_target_arm=None,
        why="a write verb on a read-only route answers 403 instead of 405. The SHAPE a "
            "renumbered refusal takes, and the body and every header still agree — so "
            "only the status arm can see it.",
    ),
    Mutation(
        name="response-header-dropped",
        target="server",
        old='        self.send_header("Cache-Control", "no-store")\n',
        new="",
        kind="headers",
        expect=frozenset({"headers"}),
        only_target_arm=None,
        why="`Cache-Control: no-store` is gone from every response. The status and the "
            "body are untouched, so a gate that compared only those would report this "
            "store as cacheable-by-a-proxy and call it identical.",
    ),
    Mutation(
        name="tar-mtime-truncated",
        target="server",
        old="info.mtime = st.st_mtime  # float",
        new="info.mtime = int(st.st_mtime)  # float",
        kind="tar",
        expect=frozenset({"tar", "headers"}),
        only_target_arm=None,
        why="the snapshot's member mtimes lose their FRACTION — the usual move for "
            "reproducibility, and the one that makes two entries written in the same "
            "second tie, so an extracted copy orders its index differently with the same "
            "bytes and no error. `X-Store-Entries` and the member NAMES are unchanged. "
            "⚠ `headers` JOINED THE DECLARED SET WHEN THE SNAPSHOT GREW AN `ETag`, AND "
            "THAT IS THE VALIDATOR EARNING ITS KEEP RATHER THAN A WIDENED LICENCE: the "
            "tag is a digest of the UNCOMPRESSED tar, so any mutation of the archive is "
            "now visible in a HEADER as well as in the bytes. Before the ETag this "
            "mutant was caught by the `tar` arm alone.",
    ),
    Mutation(
        name="tar-members-reordered",
        target="server",
        old="for entry, arcname in selected:",
        new="for entry, arcname in reversed(selected):",
        kind="tar",
        expect=frozenset({"tar", "headers"}),
        only_target_arm=None,
        why="the same members in the opposite order. Every name, every byte and every "
            "mtime is present, so an extracted-TREE comparison keyed on member name "
            "normalises it away; the archive's own byte order shows it — and, since the "
            "snapshot grew an `ETag` over exactly those bytes, so does a header. Same "
            "note as `tar-mtime-truncated`: `headers` is in the declared set because the "
            "validator makes it so, not because the claim was loosened.",
    ),
    Mutation(
        name="audit-identity-field-dropped",
        target="server",
        old="            f\"identity={audit_field(self._identity or '-', limit=MAX_IDENTITY_CHARS)} \"\n",
        new="",
        kind="audit",
        expect=frozenset({"audit"}),
        only_target_arm=None,
        why="the audit line loses `identity=`, which answers WHOSE request it was and "
            "therefore which allowlist applied. It is the newest field in that record, "
            "so it is exactly the one a port forgets — and NOTHING on the wire changes, "
            "so every other arm passes.",
    ),
    Mutation(
        name="startup-banner-field-dropped",
        target="server",
        old='        f"trusted-proxies={\',\'.join(str(n) for n in trusted_proxies)} "\n',
        new="",
        kind="process",
        expect=frozenset({"process"}),
        only_target_arm=None,
        why="the startup banner loses `trusted-proxies=`, which is the only place "
            "\"which peers may set CF-Connecting-IP\" is readable out of a running pod. "
            "It exists because the `process` arm would otherwise have NO negative control "
            "at all — every other mutant reaches the wire or the audit stream, and an arm "
            "no mutant reaches is an arm nobody has watched go red.",
    ),
    Mutation(
        name="resolver-tier-keyed-on-ref",
        target="subsystem_resolver.py",
        old="hits = [e for e in entries if e.slug == nref]",
        new="hits = [e for e in entries if e.ref == nref]",
        kind="body",
        # 🔴 MEASURED, NOT PREDICTED, AND THE FIRST DRAFT DECLARED `{"body"}` AND WAS
        # WRONG. Re-resolving a ref changes the ANSWER, so `X-Store-Status`,
        # `X-Store-Exit` and `Content-Length` move with the body and the audit record's
        # `status=` field moves with them: three comparisons, one cause. Declaring
        # `{"body"}` here would have been a guard demanding something untrue — and the
        # isolating claim was never the comparison set anyway, it is `only_target_arm`.
        expect=frozenset({"body", "headers", "audit"}),
        only_target_arm="entry",
        why="the filename tier matches `e.ref` instead of `e.slug`, which is a real "
            "resolver-version skew (it is the counterexample `verify-byte-identity.sh` "
            "records). It changes NO scope-level render — the index prints the same rows "
            "in the same order — so only the per-entry `?ref=` arm can see it. It is only "
            "REACHABLE at all because the generated store holds `plum.md` beside "
            "`plum.process.md`: for every other entry the slug and the ref are the same "
            "string and the edit is a no-op.",
    ),
)

MUTATIONS_BY_NAME = {m.name: m for m in MUTATIONS}


def _tree(dest: Path) -> Path:
    """`<dest>/server/server.py` beside `<dest>/lib/`. Returns the server path.

    🔴 THE COPY IS A TREE, NOT A FILE. `server.py` finds its modules with
    `Path(__file__).resolve().parents[1] / "lib"`, so a lone copy in a temp directory
    imports nothing at all and the control would measure a boot failure.
    """
    (dest / "server").mkdir(parents=True, exist_ok=True)
    return dest / "server" / "server.py"


def unmutated_server(dest: Path) -> Path:
    """The POSITIVE control: the same copy mechanics, no edit."""
    target = _tree(dest)
    target.write_text(SERVER_PY.read_text(encoding="utf-8"), encoding="utf-8")
    lib = dest / "lib"
    if not lib.exists():
        lib.symlink_to(LIB)
    return target


def mutated_server(dest: Path, mutation: Mutation) -> Path:
    """Copy the oracle with exactly one occurrence of `mutation.old` replaced."""
    target = _tree(dest)
    if mutation.target == "server":
        source = SERVER_PY.read_text(encoding="utf-8")
        found = source.count(mutation.old)
        if found != 1:
            raise MutationError(
                f"the pattern for {mutation.name!r} occurs {found} time(s) in "
                f"server/server.py, not 1. A pattern that matches nothing yields an "
                f"UNMUTATED server and the control would report the mutant as SURVIVED.\n"
                f"  pattern: {mutation.old!r}")
        target.write_text(source.replace(mutation.old, mutation.new, 1), encoding="utf-8")
        lib = dest / "lib"
        if not lib.exists():
            lib.symlink_to(LIB)
        return target

    # A `lib/` mutation needs a real COPY of the package rather than the symlink above,
    # because the mutation is inside it. Everything else in `lib/` is copied verbatim, so
    # a failure still attributes to the one edited module.
    target.write_text(SERVER_PY.read_text(encoding="utf-8"), encoding="utf-8")
    lib = dest / "lib"
    # ⚠ THE SYMLINK CASE IS HANDLED FIRST, AND THE HAZARD IS A CRASH RATHER THAN A FALSE
    # SURVIVED — SAYING WHICH, BECAUSE THE FIRST DRAFT OF THIS COMMENT CLAIMED THE WORSE ONE.
    # `shutil.rmtree` refuses a symbolic link, and under `ignore_errors=True` it refuses
    # SILENTLY; MEASURED on this interpreter, `copytree` then raises `FileExistsError:
    # [Errno 17] File exists` on the surviving link. That is LOUD, so it could not have
    # produced a wrong verdict — it would have aborted `--self-test` with a traceback.
    # Unreachable today in any case: every mutation gets its own `dest`. Kept because a
    # control path that crashes on a reachable arrangement is a control nobody runs, and
    # because the reading this replaces — `rmtree` walking THROUGH the link into the real
    # `lib/` — is the one that would have edited the operator's source tree. It does not;
    # `sweep6` confirmed the real `lib/` untouched and the copy mutated.
    if lib.is_symlink():
        lib.unlink()
    elif lib.exists():
        shutil.rmtree(lib)
    shutil.copytree(LIB, lib)
    module = lib / mutation.target
    if not module.is_file():
        raise MutationError(f"{mutation.name!r} names lib/{mutation.target}, which is not a file")
    source = module.read_text(encoding="utf-8")
    found = source.count(mutation.old)
    if found != 1:
        raise MutationError(
            f"the pattern for {mutation.name!r} occurs {found} time(s) in "
            f"lib/{mutation.target}, not 1. A pattern that matches nothing yields an "
            f"UNMUTATED reader and the control would report the mutant as SURVIVED.\n"
            f"  pattern: {mutation.old!r}")
    module.write_text(source.replace(mutation.old, mutation.new, 1), encoding="utf-8")
    return target
