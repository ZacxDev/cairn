#!/usr/bin/env python3
"""The FACT LEDGER: questions this repository answers with a COMMAND, not a sentence.

🔴 WHY THIS EXISTS, AND IT IS A MEASURED RESULT RATHER THAN A PREFERENCE. Every
defect found across four PRs and eight audit rounds on the deployed-pod arc was a
hand-written sentence, and NONE was visible to any gate. Over the same period the
DERIVED ledgers — `api.DeclaredRoutes()`, `cairn -verbs`, `cairn -exit-codes` —
produced zero. The discriminator is not care taken; it is whether the fact was
written or computed.

🔴 SO THE RULE THIS FILE ENFORCES IS "REPLACE THE CLAIM WITH THE COMMAND", AND THIS
REPOSITORY DISCOVERED IT BEFORE THIS FILE DID. `server/README.md` spent four
successive drafts undercounting the busybox applet set, then stopped correcting the
number and wrote: *"So: enumerate. `busybox --list` on the applet set the built
image ships … Do not write a fifth number here."* That count has not rotted since,
because it no longer exists. This file generalises that one paragraph.

🔴 IT ASSERTS THAT THE QUESTION IS STILL ANSWERABLE. IT NEVER ASSERTS THE ANSWER.
That single decision is what makes the ledger both deterministic and durable:

  - DETERMINISTIC — no clock, no network, no pinned value. The same tree gives the
    same verdict today and in a year. A staleness/TTL design was considered first
    and REJECTED for exactly this: a check that reddens because a stamp aged goes
    red on a day nobody changed anything, and `claude/RULES.md` is explicit that a
    permanently-red gate is worse than no gate because it trains everyone to click
    through.
  - DURABLE — there is no second copy of the answer to disagree with the first.
    Pinning the VALUE would re-create the defect one level down: the pin would need
    updating whenever the value legitimately moved, and a pin that is routinely
    edited to match reality is a sentence again.

⚠ WHAT IT THEREFORE DOES NOT DO, STATED RATHER THAN LEFT TO BE DISCOVERED. It
cannot tell you the answer CHANGED, and it cannot stop somebody writing a sentence
elsewhere that contradicts the command. Nothing can: a prose lint was built and
REFUTED by measurement on this tree — the rotting sentence and the correct sentence
are spelled identically, because the defect lives in the relationship between the
sentence and the world, and a regex only sees the sentence. The measurements are in
this file's test. The only complete remedy is the one the busybox paragraph used:
DELETE the claim, keep the command.

🔴 AND ONE FACT IS DECLARED UNANSWERABLE HERE, WHICH IS THE POINT AND NOT A GAP.
"Which pod does the cluster pull" has no command in this repository — no manifest
lives here — and that is precisely why nineteen sites disagreed about it. Naming it
as unanswerable-here is honest; a TTL that nags about it would be theatre.
"""

from __future__ import annotations

import shutil
import subprocess
from dataclasses import dataclass


#: Toolchains a fact's command may need. The `tests` CI job ships NEITHER go nor
#: nix, so a fact needing one is measured in the job that owns that dependency —
#: the same split `tests/test_go_client_ledgers.py` already uses, and for the same
#: reason it gives: a skip nobody counts is indistinguishable from a pass.
NEEDS_NOTHING = "nothing"
NEEDS_GO = "go"
NEEDS_NIX = "nix"

_TOOL_FOR = {NEEDS_GO: "go", NEEDS_NIX: "nix"}


@dataclass(frozen=True)
class Fact:
    """One question, and the command that answers it.

    `question` is written as a QUESTION on purpose. A ledger of assertions invites
    the reader to believe the assertion; a ledger of questions sends them to the
    command, which is the whole behaviour this file is trying to produce.
    """

    fact_id: str
    question: str
    command: list[str]
    needs: str
    #: Where a sentence stating this fact used to live, or still does. It is the
    #: audit trail for "was the claim actually deleted", which is the only test of
    #: whether replacing claims with commands is working.
    replaces: str


#: 🔴 THE LEDGER. Add a row here INSTEAD of writing the fact into prose.
#:
#: ⚠ EVERY COMMAND MUST BE READ-ONLY. These run in CI and on developer machines;
#: a row that mutates the tree would make the gate a writer, and a gate that edits
#: what it measures cannot be trusted about either.
FACTS: list[Fact] = [
    Fact(
        fact_id="packages",
        question="Which packages does the flake build — and therefore which "
        "artefacts exist at all?",
        command=[
            "nix", "eval", "--raw",
            ".#packages.x86_64-linux",
            "--apply", "s: builtins.concatStringsSep \"\\n\" (builtins.attrNames s)",
        ],
        needs=NEEDS_NIX,
        replaces="`flake.nix` and `internal/ui/README.md` each asserted the browser "
        "surface had NO image. `packages.ui-image` landed in #93 and four sites "
        "stayed false until #99; this command answers it and cannot.",
    ),
    Fact(
        fact_id="apps",
        question="Which apps does the flake expose — i.e. what does "
        "`nix run github:ZacxDev/cairn` actually execute?",
        command=[
            "nix", "eval", "--raw",
            ".#apps.x86_64-linux",
            "--apply", "s: builtins.concatStringsSep \"\\n\" (builtins.attrNames s)",
        ],
        needs=NEEDS_NIX,
        replaces="`apps` has no entry for it' is stated in prose at more than one "
        "site; the default flip is the kind of change that falsifies all of them "
        "at once.",
    ),
    Fact(
        fact_id="client-verbs",
        question="Which verbs does the Go client dispatch, and which of them write?",
        command=["go", "run", "./cmd/cairn", "-verbs"],
        needs=NEEDS_GO,
        replaces="a 'nine verbs' count that was filed as four stale comments, "
        "corrected to five, and measured at six — one of which was a live "
        "off-by-one in an `assert`.",
    ),
    Fact(
        fact_id="client-exit-codes",
        question="Which exit codes does the Go client declare?",
        command=["go", "run", "./cmd/cairn", "-exit-codes"],
        needs=NEEDS_GO,
        replaces="the exit-code model, which is a PRINTED contract; the `{0, 9}` "
        "overlap is deliberate and is the kind of thing prose renumbers by accident.",
    ),
    Fact(
        fact_id="server-routes",
        question="Which routes does the pod dispatch — i.e. what is "
        "internet-reachable?",
        command=["go", "run", "./cmd/cairn-server", "-routes"],
        needs=NEEDS_GO,
        replaces="adding a row to a dispatch table is adding a public endpoint. "
        "This has never rotted, which is the evidence the whole file rests on.",
    ),
]


#: 🔴 FACTS THIS REPOSITORY CANNOT ANSWER, DECLARED RATHER THAN OMITTED.
#:
#: An absent row reads as "nobody thought of it". A declared one reads as "this was
#: considered and the repo genuinely cannot answer it" — a different statement, and
#: the useful one. Each carries WHO can answer it instead.
UNANSWERABLE_HERE: list[tuple[str, str]] = [
    (
        "which-pod-is-deployed",
        "No manifest lives in this repository, so nothing here can establish which "
        "image a cluster pulls. NINETEEN sites disagreed about this before an "
        "operator settled it by hand. The authority is the deployment-manifest "
        "repository plus the operator; do not add a TTL'd stamp here pretending "
        "otherwise — a check that reddens on the calendar is not a measurement.",
    ),
    (
        "is-the-applet-surface-acceptable",
        "`busybox --list` enumerates the set (run it against the built image), but "
        "whether that surface is an acceptable trade beside a mounted credential is "
        "a JUDGEMENT over a threat model, not a command. The measurement was taken "
        "and the operator decided; the archive holds it.",
    ),
]


class FactUnanswerable(Exception):
    """A fact's command did not answer. Carries which half failed.

    🔴 TWO HALVES, AND REPORTING WHICH ONE IS THE DIFFERENCE BETWEEN A DIAGNOSIS AND
    A SHRUG. A non-zero exit means the command broke; a zero exit with EMPTY output
    means it ran and said nothing, which is the shape a harness wired to nothing
    produces and is the one that reads as success.
    """


def answer(fact: Fact, *, repo: str) -> str:
    """Run one fact's command and return its stdout.

    Raises `FactUnanswerable` when the command exits non-zero OR produces no
    output. Both are failures of the same claim — that the question is still
    answerable — and neither is allowed to pass quietly.
    """
    proc = subprocess.run(
        fact.command,
        cwd=repo,
        capture_output=True,
        text=True,
    )
    if proc.returncode != 0:
        raise FactUnanswerable(
            f"{fact.fact_id}: command exited {proc.returncode}.\n"
            f"  command: {' '.join(fact.command)}\n"
            f"  stderr : {proc.stderr.strip()[:400] or '(empty)'}\n"
            f"  This fact is no longer answerable. Either repair the command or "
            f"move the row to UNANSWERABLE_HERE with the reason — do NOT delete it "
            f"silently, because a question that quietly stops being asked is how "
            f"the prose that replaced it becomes the only answer again."
        )
    if not proc.stdout.strip():
        raise FactUnanswerable(
            f"{fact.fact_id}: command exited 0 and printed NOTHING.\n"
            f"  command: {' '.join(fact.command)}\n"
            f"  A zero exit with empty output is what a command wired to nothing "
            f"looks like, and it is indistinguishable from success if only the exit "
            f"code is read. That is why this check reads the CONTENT."
        )
    return proc.stdout


def toolchain_missing(fact: Fact) -> str | None:
    """The tool `fact` needs and this machine lacks, or None."""
    tool = _TOOL_FOR.get(fact.needs)
    if tool is None:
        return None
    return None if shutil.which(tool) else tool


def disposition(fact: Fact, *, missing: str | None, require: set[str]) -> tuple[str, str]:
    """What to do about `fact` given a missing tool and a set of required tiers.

    Returns `("run", "")`, `("skip", why)` or `("fail", why)`.

    🔴 THIS IS A PURE FUNCTION ON PURPOSE, AND THE REASON IS A MEASUREMENT. The
    first control for the refuse-on-skip path tried to hide `go` by filtering
    `PATH`, which on this host also hid the interpreter's own dynamic loader — the
    run died at exit 2 for a reason that had nothing to do with the logic under
    test. A control that cannot isolate what it claims to isolate is testing the
    environment, not the code. Lifting the decision out of the PATH lookup makes
    the control deterministic: pass `missing` and `require` directly.
    """
    if missing is None:
        return ("run", "")
    if "all" in require or fact.needs in require:
        return (
            "fail",
            f"{fact.fact_id} needs `{missing}` and this run REQUIRES the "
            f"`{fact.needs}` tier. A skip here is a real hole: this fact's "
            f"question would go unasked in the one job that can ask it.",
        )
    return (
        "skip",
        f"no `{missing}` on PATH. This fact is measured in the `{fact.needs}` CI "
        f"job, which sets CAIRN_FACTS_REQUIRE and REFUSES this skip.",
    )
