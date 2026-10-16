#!/usr/bin/env python3
"""Break `internal/control` on purpose, and name WHICH guard caught each break.

🔴 A TEST YOU HAVE NOT WATCHED FAIL PROVES NOTHING, AND THIS PACKAGE'S OUTPUT IS AN
AUTHORIZATION DECISION — the one kind of answer where a guard that is green for the
wrong reason is indistinguishable from one that works. The matrix test in
`internal/control/matrix_test.go` asserts sixty cells; a resolver that returned the
empty authority for everybody would satisfy every refusal in it, which is exactly the
shape `tests/parity/README.md` records the P2 harness shipping with (72 PASS / 0 FAIL
while measuring nothing). The matrix carries a positive control against that. This
module is the other half: it proves each individual guard can go RED.

🔴 ATTRIBUTION IS BY WHICH *TEST* FAILED, NOT BY "SOMETHING WENT RED". A mutant killed
by the wrong guard proves the suite can fail and proves nothing about the guard it was
built to exercise — the same lesson `tests/dualrun/mutants.py` states one layer down,
where attribution is by which COMPARISON failed. Each row below names the test that
must kill it, and a mutant killed only by some OTHER test is a finding rather than a pass.
Each row runs ONLY the tests it names (`scope_for`), so an unnamed test cannot fire: a row
whose named killer stays green reads SURVIVED, and MISATTRIBUTED is the one narrower case
still reachable — a listed `extra_killers` entry failed while the named killer did not.

🔴 THE MUTATION IS THE NARROWEST EXPRESSION THAT CAN BE WRONG. A mutant that removes a
guard together with its enclosing condition dies for the wrong reason and says nothing
about the guard; every edit here replaces one expression, and `--show` prints the exact
before/after so a reader can check that claim rather than take it.

🔴 AND THE POSITIVE CONTROL IS THE SAME COPY MECHANICS WITH NO EDIT. Without it,
"the mutant was caught" cannot be told apart from "the copied tree never compiled at
all" — a tree that does not build fails every test, and every mutant would score KILLED
for a reason that has nothing to do with the guard.

    python3 tests/control_mutants.py            # run the battery
    python3 tests/control_mutants.py --show      # print each edit without running
    python3 tests/control_mutants.py --jobs 4    # four reused worker trees in parallel
    python3 tests/control_mutants.py --shard 3/8 --results out/3.json   # one CI shard
    python3 tests/control_mutants.py --aggregate out/   # every shard ran every row ONCE

A shard enforces every refusal below over its own rows; `--aggregate` is the seam check over
all of them and prints the one SUMMARY an unsharded run prints.

Exit 0 only when the positive control is GREEN, every mutant is KILLED, and every kill is
attributed to the test that claims it. Anything else exits 1 and says which; exit 2 is "could
not vouch" — no toolchain, a failed restore, a drifted tree, or shards that do not cover the
battery exactly once.
"""
from __future__ import annotations

import argparse
import hashlib
import json
import queue
import re
import shutil
import subprocess
import sys
import tempfile
import threading
import time
from concurrent.futures import ThreadPoolExecutor
from dataclasses import asdict, dataclass, field
from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(REPO_ROOT / "tests"))

from testlib import mutant_tree  # noqa: E402

#: 🔴 MORE THAN ONE PACKAGE, BECAUSE THE GUARDS THIS BATTERY EXERCISES SPAN A SEAM — AND
#: NO TOTAL IS WRITTEN DOWN HERE ON PURPOSE. This header opened "FOUR PACKAGES, NOT ONE"
#: above a tuple of FIVE. Read out of the history: it was written "THREE" over three
#: entries, updated to "FOUR" when the fourth landed, and then the fifth landed and the
#: header did not move — one missed update out of two chances. A count kept beside the
#: thing it counts is a second spelling of the same fact and drifts silently, so no total is
#: written here and every entry below carries its own reason inline. Count them there if you
#: need a number.
#:
#: 🔴 THE TUPLE IS NOT THE ONLY PLACE THE SET IS ENUMERATED — IT IS THE ONLY PLACE IT IS
#: DECIDED, AND TWO DRAFTS OF THIS HEADER CLAIMED THE STRONGER THING. It read "the only place
#: the set is stated", then "the only place the set is ENUMERATED"; both were false on their
#: own tree, because `internal/control/README.md` spelled the package PATHS out in prose and
#: `.github/workflows/ci.yml` described the same five members a sentence at a time. The second
#: draft's reword was measured: swapping `./internal/api/` for `./internal/report/` here — the
#: COUNT unchanged — left all six assertions in `tests/test_control_mutant_count_is_pinned.py`
#: GREEN while the README still named `internal/api` by path and the CI comment still called it
#: "the server that authorises from all of them", because the only thing pinned was
#: `len(PKGS)`. That file now pins the
#: ENUMERATION itself, membership and order, as one normalised string in both documents. So the
#: uniqueness sentence is gone rather than reworded a third time, and what replaces it is
#: enforced: edit the tuple and the two documents go red until they follow.
#:
#: 🔴 IT IS NOT THE ONLY PLACE THE COUNT IS STATED, AND SAYING SO WAS THE ROUND AFTER'S
#: FINDING. Deleting the stale copy from this header fixed the copy a `PKGS` edit would
#: have had in its own diff and left the ones a `PKGS` editor never opens:
#: `internal/control/README.md` and `.github/workflows/ci.yml` both spell it out, and
#: appending a sixth entry here left `tests/test_control_mutant_count_is_pinned.py` at
#: 3 passed — its whole content then — with every one of them still reading FIVE.
#: They are pinned to `len(PKGS)` by `tests/test_control_mutant_count_is_pinned.py` now,
#: so a `PKGS` edit that leaves prose stale is a red test rather than an instruction nobody
#: reads. (The MUTANT count went further: it is no longer stated in prose at all, because every
#: PR adds rows and a pinned copy made each pair of them conflict; this file's run prints it.)
#:
#: The seam itself, which is the reason a battery scoped to ONE package would be wrong: a
#: mutant in the token-file projection is killed by a guard in the server and vice versa,
#: and a mutant in the identity backends is killed by a guard in the model — so scoping to
#: either side alone scores those SURVIVED while the suite that catches them was never run.
#: A SURVIVED mutant that only means "the killing test did not run" is a false finding that
#: reads as a coverage gap and sends the next reader to write a test that already exists.
#:
#: 🔴 WHAT THIS TUPLE SCOPES NOW, SINCE A ROW NO LONGER RUNS ALL OF IT. A killed-by row runs
#: only the packages that hold its named `killer` and `extra_killers` — LOCATED from the
#: `*_test.go` sources, wherever they sit, so the cross-seam killer above is always run and
#: the false SURVIVED it describes cannot arise from scoping. `scope_for` holds the rule and
#: the one verdict shift it causes. This tuple is still the scope of the POSITIVE CONTROL and
#: of every `equivalent` row, whose claim is that nothing anywhere in the seam notices.
PKGS = (
    # The model and the ONE authz predicate everything above authorises from.
    "./internal/control/",
    # The projection of the token file into that model. `./internal/control/...` would
    # cover this and the line above in one word; it is spelled out so that adding a
    # sub-package is a deliberate act rather than something a pattern absorbs silently.
    "./internal/control/tokenfile/",
    # P4's backends, which resolve a principal out of the model and hand it to the server
    # — so this package sits on BOTH sides of the seam described above.
    "./internal/identity/",
    # The server that authorises from the model.
    "./internal/api/",
    # 🔴 HERE BECAUSE OF WHAT LIVES ONLY IN `main`. The refresh loop that bounds the
    # divergence `tokenfile` declares is started here and nowhere else — measured:
    # deleting it left `go build`, `go vet` and every `internal/...` test package green,
    # and this battery never ran the package at all. A mitigation with no gate is a
    # mitigation nobody can be told has stopped working.
    "./cmd/cairn-server/",
    # 🔴 THE BROWSER SURFACE, BECAUSE IT IS THE SECOND CONSUMER OF THE SAME AUTHORITY —
    # and because the alternative was measured and refused. The share flow arrived with a
    # battery of its OWN (`tests/ui_share_mutants.py`, 234 lines) that NO gate ran:
    # `grep -l ui_share_mutants` over the whole tree returned the file and one README
    # line, while that README's table presented "8 mutants, 8 killed" as a standing
    # property rather than one afternoon's reading. It also mutated the LIVE working tree
    # where this harness mutates a `copytree`, so an interrupt left a mutated
    # `internal/ui/` in the checkout. Folding its rows in here buys the CI step, the
    # isolation and the count pin at once — one battery, one place.
    "./internal/ui/",
    # 🔴 THE BROWSER SURFACE'S PROGRAM, BECAUSE ITS STARTUP REFUSALS ARE THE ONLY THING
    # BETWEEN A MISCONFIGURED DEPLOYMENT AND A SURFACE THAT PASSES ITS HEALTH CHECK AND CAN
    # SERVE NOBODY — and because the evidence that they work was, for four rounds, a
    # hand-run sweep recorded in a commit message. That is the same ungated-instrument
    # shape the `./internal/ui/` entry above exists because of, one level along: the guard
    # had THREE spellings, the first two each walked around by a state nobody had tested,
    # and the sweep that found the third's uncovered clause was not gated either.
    "./cmd/cairn-ui/",
    # 🔴 PRESENCE (S2 of the arcs/presence plan), BECAUSE ITS OWNER PREDICATE IS AN AUTHZ SEAM
    # OVER THE SAME IDENTITIES. `presence.Store.For` decides who may see where a session runs and
    # who may ring it, from `identity.Identity` and `control.Authorization.Narrowed()` — so a
    # mutant in `internal/identity` or `internal/control` can be killed here, and its own guards
    # (the single-owner wall, the queue's owner filter) are reachable only at unit level.
    "./internal/presence/",
    # 🔴 SCOPE MAIL'S PREDICATES (S1 of the scope-mail plan), BECAUSE THEY ARE AN AUTHZ SEAM.
    # `memo.MaySend`, `MayRead` and `MayRetract` decide who may send, read and retract a memo
    # from a `control.Authorization` — so a mutant in `internal/control`'s narrowing (decision
    # 17's verb argument) is killed HERE, by the retraction rule's decision-17 case, and the
    # predicates' own guards are reachable at unit level here; through S2's listener they are
    # reached only in the Postgres tier, which this battery does not run (the S2 PR's matrix).
    "./internal/memo/",
    # 🔴 THE CLIENT (S11 of the transcripts/plugins plan), BECAUSE ITS READ LEDGER IS AN INPUT TO A
    # VISIBILITY PREDICATE. Every scope a served call touches is appended to the session's ledger,
    # and `cairn-ui` folds that ledger into `V` — the set `transcript.Visible` admits a viewer by.
    # A ledger that records the FLAG instead of the resolved scope, or nothing, shows a transcript to
    # a principal who cannot read what the session read; that is an authorization defect that
    # lives entirely in this package, with no guard on the pod that could notice it.
    "./internal/client/",
)


class MutationError(AssertionError):
    """The edit did not apply, so the control would have proven nothing.

    🔴 THE LOUD FAILURE IS THE POINT. A textual mutation whose pattern has drifted out
    of the source applies to nothing, the tests stay green, and the mutant is scored
    SURVIVED — a false finding that reads as a coverage gap and sends the next reader
    hunting a guard that is in fact fine. Worse in the other direction: a pattern that
    matches in more places than intended mutates code the row does not name. Both are
    refused by asserting the occurrence COUNT, not merely that a replacement happened.
    """


@dataclass(frozen=True)
class Mutant:
    """One deliberate defect, and the guard that must notice it."""

    name: str
    path: str
    old: str
    new: str
    # The Go test function that must fail. Only it and `extra_killers` are run (`scope_for`);
    # it staying green reads SURVIVED, or MISATTRIBUTED when a listed extra failed instead.
    killer: str
    # Why this edit is a plausible thing somebody would actually write, rather than a
    # textbook mutation the code happens to be shaped against.
    why: str
    occurrences: int = 1
    # Set when the mutant is expected to SURVIVE because it changes no behaviour. The
    # label is the claim; the run is what checks it.
    equivalent: bool = False
    equivalent_reason: str = ""
    # Other Go test functions that must ALSO fail for this mutant — a LEDGER, asserted.
    #
    # 🔴 IT WAS INERT FOR SEVERAL ROUNDS AND `internal/control/README.md` LEANED ON IT AS A
    # GATE, WHICH IS THIS REPOSITORY'S SIGNATURE DEFECT INSIDE THE BATTERY BUILT TO REFUSE
    # IT. The field was set on 23 rows (30 entries) and read by NOTHING: the verdict logic
    # required only `m.killer in failing` and tolerated any other failing test regardless,
    # so the README's "and is listed as an `extra_killers` on both rows" read as coverage
    # and provided none. A listed killer that quietly stopped killing was a silent
    # downgrade nobody could be told about.
    #
    # 🔴 SO IT IS NOW A DECLARED SET THAT MUST HOLD, AND THE DIRECTION MATTERS: every entry
    # must be in `failing`, and a row whose list has gone stale is a RED battery rather
    # than a quieter green. That is the same claim `killer` makes, one step wider — the
    # difference is that `killer` is "this guard, and no other, is what noticed" while this
    # is "these guards noticed too, and the day one of them stops is the day somebody has
    # to look".
    #
    # ⚠ NOT VALID ON AN `equivalent` ROW, AND REFUSED AT LOAD RATHER THAN AT RUN. An
    # equivalent mutant is expected to have NO failing test at all, so a listed extra there
    # is a contradiction that could only ever report itself as a failure of the run.
    extra_killers: tuple[str, ...] = field(default_factory=tuple)

    #: The packages to run for THIS row, overriding the module-wide `PKGS`.
    #:
    #: 🔴 IT EXISTS BECAUSE `PKGS` IS A DELIBERATELY NARROW SEAM AND WIDENING IT WOULD
    #: RE-SCOPE EVERY OTHER ROW. `PKGS` covers the control/identity/server seam, and the
    #: header above says why that scope is load-bearing: a mutant on one side of it is
    #: killed by a guard on the other, so the list must not shrink. But a row whose killer
    #: lives OUTSIDE that seam — `internal/store`, `internal/report` — scores SURVIVED for
    #: the one reason that header names as a FALSE FINDING: "the killing test did not run".
    #:
    #: MEASURED: the three `requirements-*` rows below reported `killed=0 survived=3` with
    #: an empty `pkgs`, while the same mutations die in seconds when the packages holding
    #: their killers are run. Adding those two packages to `PKGS` instead would change what
    #: 188 existing rows measure — every one of them would newly run two more packages,
    #: giving unrelated guards a chance to be the killer and turning correct rows into
    #: `misattributed` — and `len(PKGS)` is pinned to prose in three places by
    #: `test_control_mutant_count_is_pinned.py`. A per-row override changes nothing for any
    #: row that does not set it.
    pkgs: tuple[str, ...] = field(default_factory=tuple)

    #: 🔴 IT MAY ONLY *ADD* TO `PKGS`, NEVER REPLACE IT, AND `__post_init__` REFUSES
    #: OTHERWISE. Round 0 of #148 raised this against the field as first written: the
    #: header above calls `PKGS` a load-bearing seam that must not SHRINK, and a bare
    #: per-row override is a mechanism for shrinking it. The concrete failure it named —
    #: a future authz row copied from the `requirements-*` ones, setting
    #: `pkgs=("./internal/control/",)`, losing the cross-package killers the header calls
    #: load-bearing, and reporting a confident `killed=1` with nothing noticing, because
    #: the count pin counts ROWS and not SCOPE.
    #:
    #: So the field now means "the seam PLUS these". A row naming a package already in
    #: `PKGS` is accepted and redundant; a row that omits one cannot exist.
    #:
    #: ⚠ WHAT A RUN RECEIVES IS NARROWER THAN THAT UNION SINCE THE BATTERY WAS SHARDED: a
    #: killed-by row runs the packages its named tests are LOCATED in, plus what this field
    #: ADDS beyond `PKGS`; an `equivalent` row still runs the whole union. `scope_for` says why.

    def __post_init__(self) -> None:
        if self.pkgs:
            missing = tuple(p for p in PKGS if p not in self.pkgs)
            if missing:
                # Not a silent widening: the row is REFUSED so the author sees that the
                # seam is additive, rather than a run quietly measuring more than the row
                # claimed. `PKGS` is defined below this class, which is why this reads it
                # at call time rather than as a default.
                raise MutationError(
                    f"{self.name}: `pkgs` must ADD to the seam, not replace it — it omits "
                    f"{missing}. The battery's header calls `PKGS` load-bearing precisely "
                    f"because a mutant on one side of the seam is killed by a guard on the "
                    f"other; a row that drops a package scores SURVIVED for that reason and "
                    f"reads as a coverage gap. Write `PKGS + (\"./your/pkg/\",)`."
                )
        if self.equivalent and self.extra_killers:
            raise AssertionError(
                f"{self.name}: an EQUIVALENT row lists extra_killers {self.extra_killers}. "
                "An equivalent mutant is expected to leave the suite green, so those can "
                "never fail — the row is claiming two incompatible things."
            )


#: The lock-to-compare span of `codesrc.Journal.Set`, verbatim — the one row that MOVES a
#: statement across the lock needs both ends of it in one pattern.
_CODESRC_LOCKED_SPAN = (
    '\tf, err := os.OpenFile(j.Path, os.O_CREATE|os.O_RDWR|os.O_APPEND|syscall.O_NOFOLLOW, 0o600)\n'
    '\tif err != nil {\n'
    '\t\treturn Record{}, &JournalUnreadableError{Path: j.Path, Err: err}\n'
    '\t}\n'
    '\tdefer f.Close()\n'
    '\tif err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {\n'
    '\t\treturn Record{}, &JournalUnreadableError{Path: j.Path, Err: err}\n'
    '\t}\n'
    '\tdefer syscall.Flock(int(f.Fd()), syscall.LOCK_UN)\n'
    '\tif _, err := f.Seek(0, io.SeekStart); err != nil {\n'
    '\t\treturn Record{}, &JournalUnreadableError{Path: j.Path, Err: err}\n'
    '\t}\n'
    '\tdata, err := io.ReadAll(f)\n'
    '\tif err != nil {\n'
    '\t\treturn Record{}, &JournalUnreadableError{Path: j.Path, Err: err}\n'
    '\t}\n'
    '\tcurrent := fold(data).RevisionFor(scope)\n'
    '\tif current != ifRevision {\n'
    '\t\treturn Record{}, &StaleRevisionError{Scope: scope, Want: ifRevision, Current: current}\n'
    '\t}\n'
    '\tif interleave != nil {\n'
    '\t\tinterleave()\n'
    '\t}\n'
)

#: The compare-and-seam tail of that span: the lines the moved-compare row lifts OUT of the lock.
_CODESRC_COMPARE_AND_SEAM = (
    '\tcurrent := fold(data).RevisionFor(scope)\n'
    '\tif current != ifRevision {\n'
    '\t\treturn Record{}, &StaleRevisionError{Scope: scope, Want: ifRevision, Current: current}\n'
    '\t}\n'
    '\tif interleave != nil {\n'
    '\t\tinterleave()\n'
    '\t}\n'
)


MUTANTS: tuple[Mutant, ...] = (
    # ---- the resolver: the two sources of authority, and the union of them --------
    Mutant(
        name="role-verbs-ignored",
        path="internal/control/resolve.go",
        old="a.add(scopeID, roleVerbs[ms.Role])",
        # `AllSet|…` rather than a bare `AllSet` so `ms` stays used: Go refuses an
        # unused range variable, and a mutant that does not COMPILE dies at the
        # build rather than at the guard, which proves nothing about either.
        new="a.add(scopeID, AllSet|roleVerbs[ms.Role])",
        killer="TestTheAuthorizationMatrixIsExactlyThis",
        why="the shape of 'membership means access' written without looking up what the "
        "role actually confers — the single most natural way to get ownership wrong.",
    ),
    Mutant(
        name="member-gains-admin",
        path="internal/control/model.go",
        old="RoleMember: NewVerbSet(VerbRead, VerbWrite),",
        new="RoleMember: NewVerbSet(VerbRead, VerbWrite, VerbAdmin),",
        killer="TestTheAuthorizationMatrixIsExactlyThis",
        why="one word added to the role table. Nothing about the code reads wrong "
        "afterwards, which is why only a ledger over the whole matrix can see it.",
    ),
    Mutant(
        name="revoked-grants-still-apply",
        path="internal/control/resolve.go",
        old="\t\tif !g.Live() {\n\t\t\tcontinue\n\t\t}",
        new="\t\tif false {\n\t\t\tcontinue\n\t\t}",
        killer="TestRevocationIsWhatMakesBobDifferFromTheGrant",
        extra_killers=("TestTheAuthorizationMatrixIsExactlyThis",),
        why="a tombstone that is stored and then not consulted. This is the defect the "
        "whole append-only design exists to prevent, and it is invisible to any test "
        "that only asserts what a grant DOES confer.",
    ),
    Mutant(
        name="project-as-subject-dropped",
        path="internal/control/resolve.go",
        old="subjects[subjectRef{KindProject, projectID}] = struct{}{}",
        new="_ = projectID",
        killer="TestTheAuthorizationMatrixIsExactlyThis",
        why="a team share that silently reaches nobody. Every direct grant keeps "
        "working, so the failure only shows on principals who were never named.",
    ),
    Mutant(
        name="project-as-object-dropped",
        path="internal/control/resolve.go",
        # Re-derived when the expansion moved into `addObject` (group federation shares it).
        old="\tcase ObjectProject:\n\t\tfor _, scopeID := range m.ScopesIn(id) {\n\t\t\ta.add(scopeID, verbs)\n\t\t}",
        new="\tcase ObjectProject:\n\t\treturn",
        killer="TestTheAuthorizationMatrixIsExactlyThis",
        why="granting a whole project and having it confer nothing — the mirror of the "
        "row above, and reached by a completely different principal.",
    ),
    Mutant(
        name="union-becomes-overwrite",
        path="internal/control/resolve.go",
        old="a.byScope[scope] = a.byScope[scope].Union(verbs)",
        new="a.byScope[scope] = verbs",
        killer="TestTheAuthorizationMatrixIsExactlyThis",
        why="the last route to a scope wins instead of the widest. Reaches only "
        "principals who hold a scope by TWO routes, which is one cell of the fixture "
        "and the reason that cell was built.",
    ),
    # ---- the narrowing contract --------------------------------------------------
    Mutant(
        name="empty-narrowing-means-no-narrowing",
        path="internal/control/resolve.go",
        # Re-derived when decision 17 gave `Narrow` a verb argument (scope-mail S1): the early
        # return now needs BOTH narrowings absent, and the nil-vs-empty collapse is the same edit.
        old="\tif only == nil && verbs == nil {\n\t\treturn a\n\t}",
        new="\tif len(only) == 0 && verbs == nil {\n\t\treturn a\n\t}",
        killer="TestNarrowingIntersectsAndCannotWiden",
        why="the single most likely edit anybody makes to this function — `len(x) == 0` "
        "reads as the idiomatic nil check and collapses the two opposite states.",
    ),
    Mutant(
        name="narrow-builds-no-filter-for-an-empty-list",
        path="internal/control/resolve.go",
        old="\tif only != nil {\n\t\tkeep = make(",
        new="\tif len(only) > 0 {\n\t\tkeep = make(",
        killer="TestNarrowingIntersectsAndCannotWiden",
        why="the SECOND place the nil-vs-empty distinction lives since `Narrow` took a verb "
        "argument (scope-mail S1): a nil `keep` means 'no scope filter', so building it only for a "
        "non-empty list turns the see-nothing credential into an unrestricted one.",
    ),
    Mutant(
        name="narrowing-does-not-narrow",
        path="internal/control/resolve.go",
        old="\t\t\tif _, wanted := keep[id]; !wanted {\n\t\t\t\tcontinue\n\t\t\t}",
        new="\t\t\tif false {\n\t\t\t\tcontinue\n\t\t\t}",
        killer="TestNarrowingIntersectsAndCannotWiden",
        why="a narrowing that is computed, stored and then not applied.",
    ),
    Mutant(
        name="visible-scopes-goes-unrestricted",
        path="internal/control/resolve.go",
        old="\treturn store.VisibleScopeSet(names)",
        new="\tif len(names) > 0 {\n\t\treturn store.Unrestricted()\n\t}\n\treturn store.VisibleScopeSet(names)",
        killer="TestVisibleScopesNeverProjectsToUnrestricted",
        why="the 'optimisation' a reader reaches for on seeing an enumeration where a "
        "wildcard exists. It is observationally identical on every store whose "
        "directories all have scope records, which is every test store but not every "
        "real one.",
    ),
    # ---- authentication -----------------------------------------------------------
    Mutant(
        name="revoked-credential-still-authenticates",
        path="internal/control/resolve.go",
        old="\t\tif !c.Live() {\n\t\t\tcontinue\n\t\t}\n\t\tif EqualHash(presented, c.TokenHash) {",
        new="\t\tif EqualHash(presented, c.TokenHash) {",
        killer="TestAuthenticationRefusesUniformly",
        why="a revocation that reaches the journal and not the authenticator.",
    ),
    Mutant(
        name="narrowing-not-applied-at-authenticate",
        path="internal/control/resolve.go",
        old="return p, Narrow(Resolve(m, p), matched.NarrowedScopes, matched.NarrowedVerbs), nil",
        new="return p, Resolve(m, p), nil",
        killer="TestACredentialNarrowedAtIssueTimeIsAppliedAtAuthenticateTime",
        why="a restriction that is stored, displayed in the UI, and never enforced. "
        "Every test that only reads the credential row would still pass.",
    ),
    Mutant(
        name="narrowed-flag-misses-the-empty-narrowing",
        path="internal/control/resolve.go",
        old="\t\tnarrowed: true,\n",
        new="\t\tnarrowed: len(only) > 0,\n",
        killer="TestAnAuthorizationSaysWhetherACredentialNarrowedIt",
        why="the idiomatic-looking `len` check again, one field along. A credential narrowed "
        "to NOTHING then reads as un-narrowed, and every surface that refuses narrowed "
        "credentials lets through the one meant to see least.",
    ),
    Mutant(
        name="ui-sign-in-accepts-a-narrowed-credential",
        path="internal/ui/session.go",
        old="\tif auth.Narrowed() {\n\t\trefuse(",
        new="\tif false && auth.Narrowed() {\n\t\trefuse(",
        killer="TestANarrowedCredentialCannotSignIn",
        why="the sign-in door as it stood before the refusal: a session is keyed on the "
        "PRINCIPAL and re-resolves its full authority on every request, so a token narrowed "
        "to one scope opens a browser session that reads every scope its owner can.",
    ),
    # ---- the sign-in RETURN PATH: the open-redirect validator, and the bearer split ----
    #
    # ⚠ `ui-next-allows-a-sign-in-loop` WAS A ROW HERE AND IS DELETED WITH THE CHECK IT
    # MUTATED: its premise (`next=/sign-out` signs a person out) was false — the row is
    # POST-only — see `safeNext` and `TestANextOfSignOutSignsNobodyOut`.
    #
    # `safeNext` is the ONE function every read of `?next=` goes through, so a hole in it is
    # an open redirect on every door at once; and the dispatcher's redirect must never answer
    # a program that presented an `Authorization` header with HTML instead of the 401.
    Mutant(
        name="ui-next-allows-a-network-path-reference",
        path="internal/ui/returnto.go",
        old="\tif strings.Contains(p, \"//\") {",
        new="\tif false {",
        killer="TestSafeNextRefusesTheOpenRedirectCorpus",
        why="`strings.HasPrefix(next, \"/\")` is the check everybody writes first, and "
        "`//evil.invalid` satisfies it — a browser resolves it to another HOST, so a sign-in "
        "link anybody can craft lands a freshly authenticated victim on an attacker's page.",
    ),
    Mutant(
        name="ui-next-allows-a-backslash",
        path="internal/ui/returnto.go",
        old="c == '\\\\' || c == '#'",
        new="c == '#'",
        killer="TestSafeNextRefusesTheOpenRedirectCorpus",
        why="`/\\evil.invalid` passes every slash-based check and browsers read `\\` as `/` "
        "in the authority position, so it is `//evil.invalid` to the thing that follows it.",
    ),
    Mutant(
        name="ui-next-allows-control-characters",
        path="internal/ui/returnto.go",
        old="c == '#' || c < 0x21 || c > 0x7e",
        new="c == '#' || c > 0x7e",
        killer="TestSafeNextRefusesTheOpenRedirectCorpus",
        why="a browser STRIPS tab and newline from a URL before parsing it, so `/<TAB>/evil` "
        "passes the no-empty-segment check here and is `//evil` to the browser.",
    ),
    Mutant(
        name="ui-next-allows-a-fragment",
        path="internal/ui/returnto.go",
        old="c == '\\\\' || c == '#' || c < 0x21",
        new="c == '\\\\' || c < 0x21",
        killer="TestSafeNextRefusesTheOpenRedirectCorpus",
        why="a fragment never arrives in a request-URI, and one carrying an invalid escape makes "
        "`url.Parse` fail, so `http.Redirect` skips its `path.Clean` and emits the value raw.",
    ),
    Mutant(
        name="ui-next-allows-dot-segments",
        path="internal/ui/returnto.go",
        old="\t\tif seg == \".\" || seg == \"..\" {",
        new="\t\tif seg == \"\\x00\" {",
        killer="TestSafeNextRefusesTheOpenRedirectCorpus",
        why="`/..//evil.invalid` is one dot-segment removal away from the path `//evil.invalid`; "
        "refusing the shape keeps the value validated identical to the value sent.",
    ),
    Mutant(
        name="ui-redirect-answers-a-failed-bearer-with-html",
        path="internal/ui/returnto.go",
        old="\t\t!presentedAuthorization(r)",
        new="\t\ttrue",
        killer="TestAFailedBearerAndANonBrowserKeepTheUniform401",
        why="a program presenting a revoked token that also sends `Accept: text/html` would "
        "be told to go and sign in through a form, instead of the uniform 401 its error "
        "handling was written against — the machine contract moved with nothing saying so.",
    ),
    Mutant(
        name="ui-signed-in-shortcut-follows-the-sign-in-page",
        path="internal/ui/session.go",
        old="if u, perr := url.Parse(next); perr == nil && u.Path == SignInPath {",
        new="if u, perr := url.Parse(next); perr == nil && u.Path == \"/never-a-path\" {",
        killer="TestNoUnauthenticatedRequestShapeLoops",
        why="`safeNext` accepts `/sign-in` once its loop check is gone, so a shortcut that "
        "follows any valid `next` strips one `/sign-in?next=` level per hop — 140 redirects at "
        "depth 140, inside `maxNextLen` — and the browser reports too many redirects.",
    ),
    Mutant(
        name="ui-redirect-admits-head",
        path="internal/ui/returnto.go",
        old="\treturn r.Method == http.MethodGet &&",
        new="\treturn (r.Method == http.MethodGet || r.Method == http.MethodHead) &&",
        killer="TestNoUnauthenticatedRequestShapeLoops",
        extra_killers=("TestAFailedBearerAndANonBrowserKeepTheUniform401",),
        why="HEAD reads as the harmless twin of GET, and no row answers it — so a redirected "
        "HEAD lands on `/sign-in`, is not routed, and is redirected to `/sign-in` for ever.",
    ),
    Mutant(
        name="ui-invite-flow-acts-as-a-narrowed-principal",
        path="internal/ui/invitehandlers.go",
        old="\tif id.Auth.Narrowed() {\n\t\treturn control.Principal{}\n\t}",
        new="\tif false && id.Auth.Narrowed() {\n\t\treturn control.Principal{}\n\t}",
        # Re-aimed with the invite fold: `POST /invite` mints nothing for anybody now, so the door
        # a narrowed bearer would walk through is the team-link mint. The row keeps its name.
        killer="TestANarrowedBearerHasNoTeamLinkAuthority",
        why="membership authority is decided from the principal's project ROLE, which no "
        "scope narrowing bounds — so a narrowed bearer token mints a team link over its "
        "owner's project and redeems it as an identity its holder controls.",
    ),
    # ---- the membershipActor CALL SITES -------------------------------------------
    # The row above mutates the helper's BODY, which cannot see a call site that never calls
    # it. Each row below restores `id.Principal` at ONE site; the behavioural test named is
    # the killer, and `TestEveryMembershipDecisionActsAsMembershipActor` (the type-resolved ledger)
    # goes red beside it on every one.
    Mutant(
        name="ui-invite-page-bypasses-membership-actor",
        path="internal/ui/invitehandlers.go",
        old="view.Projects = s.inviting.Invitable(membershipActor(id))",
        new="view.Projects = s.inviting.Invitable(id.Principal)",
        killer="TestANarrowedBearerSeesNoInvitations",
        why="the project page is the narrowing in front of `Outstanding`, which checks nothing "
        "itself: a narrowed bearer would list who is being invited into its owner's projects.",
    ),
    Mutant(
        name="ui-invite-revoke-bypasses-membership-actor",
        path="internal/ui/invitehandlers.go",
        old="s.inviting.Revoke(r.Context(), membershipActor(id), digest)",
        new="s.inviting.Revoke(r.Context(), id.Principal, digest)",
        killer="TestANarrowedBearerCannotRevokeAnInvitation",
        why="a narrowed bearer withdrawing its owner's invitations — membership authority "
        "exercised through a credential meant to see one scope.",
    ),
    Mutant(
        name="ui-share-page-candidates-bypass-membership-actor",
        path="internal/ui/sharehandlers.go",
        # Re-derived when the share page became the Team page's `shareSection` (#214, O-a): its
        # returns carry the section's value now.
        old="\tcandidates, err := s.sharing.Candidates(membershipActor(id))\n\tif err != nil {\n\t\twritePlain(w, http.StatusInternalServerError, \"the authority could not be read\")\n\t\treturn ShareView{}, false\n\t}\n\n\tview.Scope",
        new="\tcandidates, err := s.sharing.Candidates(id.Principal)\n\tif err != nil {\n\t\twritePlain(w, http.StatusInternalServerError, \"the authority could not be read\")\n\t\treturn ShareView{}, false\n\t}\n\n\tview.Scope",
        killer="TestANarrowedAdminBearerIsOfferedNoShareCandidates",
        why="the share page lists every collaborator across the owner's projects to a caller "
        "whose narrowing excludes them.",
    ),
    Mutant(
        name="ui-share-write-candidates-bypass-membership-actor",
        path="internal/ui/sharehandlers.go",
        old="\tcandidates, err := s.sharing.Candidates(membershipActor(id))\n\tif err != nil {\n\t\twritePlain(w, http.StatusInternalServerError, \"the authority could not be read\")\n\t\treturn\n\t}\n\tsubject, ok",
        new="\tcandidates, err := s.sharing.Candidates(id.Principal)\n\tif err != nil {\n\t\twritePlain(w, http.StatusInternalServerError, \"the authority could not be read\")\n\t\treturn\n\t}\n\tsubject, ok",
        killer="TestANarrowedAdminBearerIsOfferedNoShareCandidates",
        why="the write half: the subject check accepts a membership-derived collaborator the "
        "page no longer offers, so the narrowing holds only for callers who use the form.",
    ),
    # ---- the journal boundary ------------------------------------------------------
    Mutant(
        name="unknown-event-kind-accepted",
        path="internal/control/journal.go",
        old="\tdefault:\n\t\t// 🔴 NO DEFAULT-ACCEPT ARM.",
        new="\tdefault:\n\t\treturn nil\n\t\t// 🔴 NO DEFAULT-ACCEPT ARM.",
        killer="TestTheJournalRefusesWhatItCannotEnforce",
        extra_killers=("TestEveryDeclaredEventKindHasAnApplyArm",),
        why="a forward-compatibility 'fix' — skip what you do not understand — which "
        "turns a newer build's revocation into a grant that keeps working.",
    ),
    Mutant(
        name="unknown-verb-dropped-instead-of-refused",
        path="internal/control/verbset.go",
        old="\t\tif !v.Valid() {\n\t\t\treturn fmt.Errorf(",
        new="\t\tif false {\n\t\t\treturn fmt.Errorf(",
        killer="TestTheJournalRefusesWhatItCannotEnforce",
        why="making the decode lenient to match `NewVerbSet`'s deliberate silence — the "
        "asymmetry between the two boundaries is exactly what a tidying pass removes.",
    ),
    Mutant(
        name="narrowed-scopes-gains-omitempty",
        path="internal/control/journal.go",
        old='NarrowedScopes []ID   `json:"narrowed_scopes"`',
        new='NarrowedScopes []ID   `json:"narrowed_scopes,omitempty"`',
        killer="TestAnEmptyNarrowingSurvivesTheDurableFormat",
        why="every other field in the struct carries `omitempty`; adding it here is a "
        "consistency edit that silently widens a credential through the durable format.",
    ),
    Mutant(
        name="empty-verb-grant-accepted",
        path="internal/control/journal.go",
        # Re-derived (scope-mail S1): `credential-verbs-narrowed` has an empty-verbs refusal too,
        # so the grant's is named by its own message.
        old='\t\tif e.Verbs.Empty() {\n\t\t\treturn fmt.Errorf("%s: verbs is empty — a grant',
        new='\t\tif false {\n\t\t\treturn fmt.Errorf("%s: verbs is empty — a grant',
        killer="TestTheJournalRefusesWhatItCannotEnforce",
        why="a row that reads like access in a grant log and confers none.",
    ),
    Mutant(
        name="raw-token-accepted-as-a-digest",
        path="internal/control/journal.go",
        old="\t\tif !isHexDigest(e.TokenHash) {",
        new="\t\tif false {",
        killer="TestTheJournalRefusesWhatItCannotEnforce",
        why="the guard standing between a caller's mistake and a credential written in "
        "clear text into a durable, operator-readable file.",
        extra_killers=("TestA64CharacterRawTokenIsRefusedAsADigest",),
    ),
    Mutant(
        name="token-hash-checked-by-LENGTH-only",
        path="internal/control/journal.go",
        old="\t\tif !isHexDigest(e.TokenHash) {",
        new="\t\tif len(e.TokenHash) != HashHexLen {",
        killer="TestA64CharacterRawTokenIsRefusedAsADigest",
        # 🔴 THIS MUTANT IS THE PRE-CHANGE CODE VERBATIM, WHICH IS WHY IT IS A SEPARATE ROW
        # FROM THE ONE ABOVE RATHER THAN A WIDENING OF IT. `raw-token-accepted-as-a-digest`
        # deletes the guard's operand entirely; a guard that is PRESENT and too NARROW
        # survives that edit, and "too narrow" is the state this field was actually shipped
        # in. The row's killer is deliberately NOT the table test: under this mutant
        # `TestTheJournalRefusesWhatItCannotEnforce` stays GREEN, because its raw-token row
        # is 26 characters and a length check still catches that — which is the measurement
        # that says the table could not see the real case.
        why="reverting to the length-only check that shipped: `base64.RawURLEncoding` of 48 "
        "random bytes is exactly 64 characters, so a raw secret had a natural spelling "
        "that cleared it and was persisted verbatim into the append-only authority.",
    ),
    Mutant(
        name="digest-shape-check-refuses-UPPERCASE-hex",
        path="internal/control/ids.go",
        old="\t\tif (c < '0' || c > '9') && (c < 'a' || c > 'f') && (c < 'A' || c > 'F') {",
        new="\t\tif (c < '0' || c > '9') && (c < 'a' || c > 'f') {",
        killer="TestAnUppercaseDigestReplaysAndTheCredentialItNamesAuthenticates",
        extra_killers=("TestOneSecretInTwoSpellingsIsStillRefusedAsADuplicate",),
        # 🔴 THIS MUTANT IS A SHIPPED DEFECT TOO, ONE ROUND LATER THAN THE ROW ABOVE, AND AT
        # THE OPPOSITE END OF THE SAME GUARD. The widening that closed the raw-token hole
        # was first written LOWERCASE-ONLY, which refuses a value that is unambiguously a
        # digest — and it refuses it on REPLAY, so `Model.apply` fails the journal WHOLE:
        # one hand-written uppercase row loads zero credentials and a pod falls back to an
        # empty `lastKnownGood()`. Narrowing a guard is not a safe direction when the guard
        # runs over a durable file somebody else already wrote.
        why="'a spelling `HashToken` never emits cannot be a real digest' — true about the "
        "spelling, false about the risk. Case does not discriminate a raw token (`-`, `_` "
        "and `g`..`z` do), so requiring lowercase buys no hazard coverage and costs every "
        "credential in any journal holding a hand-written uppercase row.",
    ),
    Mutant(
        name="credential-digest-not-normalised-on-replay",
        path="internal/control/journal.go",
        old="\t\tdigest := normalizedDigest(e.TokenHash)",
        new="\t\tdigest := e.TokenHash",
        killer="TestAnUppercaseDigestReplaysAndTheCredentialItNamesAuthenticates",
        extra_killers=("TestOneSecretInTwoSpellingsIsStillRefusedAsADuplicate",),
        # 🔴 ACCEPTING BOTH SPELLINGS WITHOUT THIS LINE IS WORSE THAN REFUSING ONE, WHICH IS
        # WHY IT IS A SEPARATE ROW: the two halves fail differently and neither implies the
        # other. Without normalisation an uppercase record replays CLEAN and authenticates
        # nobody (`EqualHash` is byte-exact, `HashToken` emits lowercase), and the duplicate
        # refusal — a string compare — stops seeing one secret recorded twice in two cases,
        # which is the ambiguity that loop exists to refuse.
        why="the obvious half of the fix taken alone: widen what `validate` accepts and "
        "store whatever arrived. It reads as compatibility and produces a credential that "
        "loads, looks right in the journal, and matches no token ever presented.",
    ),
    Mutant(
        name="duplicate-digest-accepted",
        path="internal/control/journal.go",
        old="\t\tfor id, c := range m.Credentials {\n\t\t\tif c.TokenHash == digest {",
        new="\t\tfor id, c := range m.Credentials {\n\t\t\tif false {\n\t\t\t\t_ = id\n\t\t\t\t_ = c",
        killer="TestTwoCredentialsCannotShareOneDigest",
        why="one secret bound to two principals, resolved arbitrarily by whichever the "
        "authenticator's iteration reaches last.",
    ),
    Mutant(
        name="failed-replay-returns-a-partial-model",
        path="internal/control/journal.go",
        # ⚠ THE ANCHOR MOVED WHEN `Replay` GAINED `dropHint`, AND A STALE ANCHOR IS A
        # HARNESS ERROR RATHER THAN A SURVIVOR — which is the right direction, but it is
        # also why every row here is checked against the tree before a run is believed.
        old='return Model{}, fmt.Errorf("event %d (%s): %w%s", i+1, e.Kind, err, dropHint(e, m.Dropped))',
        new='return m, fmt.Errorf("event %d (%s): %w%s", i+1, e.Kind, err, dropHint(e, m.Dropped))',
        killer="TestAFailedReplayReturnsNoModelAtAll",
        why="returning what you have alongside the error reads as helpful and hands the "
        "caller an authority missing every event after the failure.",
    ),
    # ---- the mutable metadata: a move and a rename are authorization changes ---------
    Mutant(
        name="scope-move-does-not-move",
        path="internal/control/journal.go",
        old="\t\tsc.ProjectID = e.ProjectID\n\t\tm.Scopes[e.ScopeID] = sc",
        new="\t\t_ = e.ProjectID\n\t\tm.Scopes[e.ScopeID] = sc",
        killer="TestMovingAScopeMovesItsOwnershipAuthority",
        why="a filing change that is accepted, logged, shown in the UI and never "
        "applied. Nothing errors; the scope simply stays where it was, and everybody "
        "who was supposed to gain or lose it keeps the authority they had.",
    ),
    Mutant(
        name="rename-does-not-rename",
        path="internal/control/journal.go",
        old="\t\tsc.DisplayName = e.DisplayName\n\t\tm.Scopes[e.ScopeID] = sc",
        new="\t\t_ = e.DisplayName\n\t\tm.Scopes[e.ScopeID] = sc",
        killer="TestRenamingAScopeChangesTheProjectedNameAndNotTheAuthority",
        why="the projection that a syncing client repairs its local directory from "
        "stops following the id. The authority is untouched, which is exactly why "
        "an authorization test alone cannot see it.",
    ),
    Mutant(
        name="a-member-may-administer-the-project",
        path="internal/control/model.go",
        old="\treturn ms.Role == RoleOwner || ms.Role == RoleAdmin",
        new="\treturn ms != Membership{} || true",
        killer="TestProjectAdministrationIsNotAScopeVerb",
        why="the owner/admin distinction collapsing into 'is a member', which is what "
        "happens when somebody reads `roleVerbs` (where owner and admin ARE identical) "
        "and concludes the role does not matter.",
    ),
    Mutant(
        name="an-admin-may-delete-the-project",
        path="internal/control/model.go",
        old="\treturn in && ms.Role == RoleOwner",
        new="\treturn in && ms.Role != RoleMember",
        killer="TestProjectAdministrationIsNotAScopeVerb",
        why="the one capability that separates owner from admin, widened by the "
        "plausible reading that an admin administers everything.",
    ),
    Mutant(
        name="an-ambiguous-scope-name-is-picked",
        path="internal/control/model.go",
        old="\tdefault:\n\t\tsort.Slice(found,",
        new="\tdefault:\n\t\treturn found[0], nil\n\t\tsort.Slice(found,",
        killer="TestAmbiguousScopeNamesAreReportedRatherThanPicked",
        why="returning the first match reads as a reasonable default and hands one "
        "project's caller another project's scope id — a cross-tenant read dressed as "
        "a lookup.",
    ),
    # ---- the store ------------------------------------------------------------------
    Mutant(
        name="append-validates-against-the-live-cache",
        path="internal/control/filestore.go",
        old="\tnext := current.clone()",
        new="\tnext := current",
        killer="TestARejectedBatchLeavesNeitherBytesNorState",
        # `TestARefusedProvisioningLeavesNeitherBytesNorState` now reads `lastKnownGood()`
        # rather than `Model()` — `Model` is unconditionally `Reload`, so it re-read a file
        # the refused batch never touched and its "…nor State" arm was vacuous. Repairing it
        # made it a second, legitimate killer of this row, one layer up.
        extra_killers=("TestARefusedProvisioningLeavesNeitherBytesNorState",),
        why="THE DEFECT THIS PACKAGE ACTUALLY SHIPPED WITH IN ITS FIRST DRAFT. A Model is "
        "six maps behind a struct header, so a struct copy shares every bucket and a "
        "rejected batch's earlier events stay applied to the served authority.",
    ),
    Mutant(
        name="clone-is-shallow",
        path="internal/control/model.go",
        old="\tout := NewModel()\n\tout.Epoch = m.Epoch",
        new="\tout := m\n\tout.Epoch = m.Epoch",
        killer="TestARejectedBatchLeavesNeitherBytesNorState",
        extra_killers=("TestARefusedProvisioningLeavesNeitherBytesNorState",),
        why="the same defect one level down, where the function is NAMED clone and so "
        "reads as if it cannot be wrong.",
    ),
    Mutant(
        name="failed-reload-empties-the-authority",
        path="internal/control/filestore.go",
        old='\t\treturn s.lastKnownGood(), fmt.Errorf("control journal %s: %w", s.path, err)\n\t}\n\tm, err := Replay(events)',
        new='\t\treturn Model{}, fmt.Errorf("control journal %s: %w", s.path, err)\n\t}\n\tm, err := Replay(events)',
        killer="TestAnUnreadableJournalKeepsTheLastKnownGoodAuthority",
        why="the intuitive 'return nothing on error' that turns a parse error into a "
        "total outage which reads like a permissions problem.",
    ),
    Mutant(
        name="copyids-flattens-nil",
        path="internal/control/ids.go",
        old="\tif src == nil {\n\t\treturn nil\n\t}",
        new="\tif false {\n\t\treturn nil\n\t}",
        # 🔴 THE KILLER ON THIS ROW WAS WRONG IN THE FIRST DRAFT, AND THE BATTERY IS
        # WHAT SAID SO. It named the narrowing test, on the reasoning that `copyIDs`
        # is the nil/empty asymmetry's carrier — but that test calls `Narrow`
        # DIRECTLY with literal slices and never reaches `copyIDs` at all. The path
        # that does is `apply` storing a credential, so the guards that see this are
        # the two authentication tests. Recorded rather than quietly corrected: a
        # row whose named killer never runs is the "reads as coverage while
        # providing none" shape, and it was inside the control built to refuse it.
        killer="TestAuthenticateResolvesOneCredentialToOnePrincipalAndItsAuthority",
        extra_killers=("TestAProjectCredentialCarriesTheProjectsOwnGrantsAndNoMembersRoles",),
        why="a defensive-copy helper that cannot distinguish 'no narrowing' from "
        "'narrowed to nothing' — the nil/empty asymmetry losing its last carrier. "
        "The damage is fail-CLOSED (every unnarrowed credential would see nothing), "
        "which is why it needs a guard rather than being dismissed as harmless.",
    ),
    # ---- the credential-ISSUING path: the only thing here that holds a raw secret ----
    Mutant(
        name="issued-renders-its-token",
        path="internal/control/credential_issue.go",
        old='\treturn fmt.Sprintf("credential=%s digest=%s epoch=%d token=<redacted: shown once, on issue>",\n\t\ti.Credential, i.TokenHash, i.Epoch)',
        # ⚠ `i.Token()` RATHER THAN `i.token`, AND THE REASON IS A MEASUREMENT RATHER THAN
        # A STYLE CHOICE. The field became a `*string` when the `%p` leak was closed, so
        # `%s` of it is `fmt.Sprintf format %s has arg i.token of wrong type *string` —
        # `go vet` runs inside `go test`, the tree DOES NOT BUILD, and the row scores a
        # harness error instead of exercising the guard. A mutant that dies at the build
        # proves nothing about any test. The accessor is also the more plausible edit now:
        # it is what somebody adding the field back to a log line would reach for.
        new='\treturn fmt.Sprintf("credential=%s digest=%s epoch=%d token=%s",\n\t\ti.Credential, i.TokenHash, i.Epoch, i.Token())',
        killer="TestNoRenderingOfIssuedContainsTheToken",
        why="the single most likely edit anybody makes to this type — putting the field "
        "back in the log line while debugging. The realistic leak is not a deliberate "
        "print of a secret; it is `%v` of a value that happens to hold one, in an error "
        "path or a test failure message.",
    ),
    Mutant(
        name="token-entropy-narrowed",
        path="internal/control/credential_issue.go",
        old="const TokenEntropyBytes = 32",
        new="const TokenEntropyBytes = 16",
        killer="TestTheMintedTokenIsExactlyTheDeclaredWidthAndAlphabet",
        # The width is the one property two packages have to agree on without being able
        # to see each other, so this row is also what stands on the seam guard beside it.
        extra_killers=("TestTheMintedWidthAgreesWithTheTokenFileFloor",),
        why="'128 bits is plenty for an id, so it is plenty here' — the reasoning "
        "`NewID` states for an id and that does not transfer to the secret itself. A "
        "narrower mint renders below `authz.MinTokenChars`, so the pod and `cairn-ui` "
        "would refuse at STARTUP a token this command had just told an operator to use.",
    ),
    Mutant(
        name="credential-issued-without-a-principal-check",
        path="internal/control/credential_issue.go",
        old="\tif err := current.checkSubject(req.SubjectKind, req.SubjectID); err != nil {",
        new="\tif err := current.checkSubject(req.SubjectKind, req.SubjectID); err != nil && false {",
        killer="TestIssuingToAPrincipalTheJournalDoesNotHoldIsRefusedBeforeAnythingIsWritten",
        # 🔴 THE KILL IS BY THE SENTINEL, NOT BY "IT ERRORED", AND THE ROW EXISTS TO PIN
        # THAT DISTINCTION. `apply` refuses the same batch under the lock, so the call still
        # fails with the mutant applied — a test that accepted any error would score this
        # SURVIVED while the two things the pre-check buys (no secret minted for a doomed
        # request; a message that separates a typo from a broken mount) went unguarded.
        why="deleting a check that looks redundant because the journal enforces the same "
        "rule — true of the refusal, false of WHEN it happens and of what it says.",
    ),
    Mutant(
        name="narrowing-flattened-on-issue",
        path="internal/control/credential_issue.go",
        old="\t\tNarrowedScopes: copyIDs(req.NarrowedScopes),",
        new="\t\tNarrowedScopes: append([]ID(nil), req.NarrowedScopes...),",
        killer="TestANarrowingRoundTripsThroughTheJournal",
        why="the idiomatic defensive copy, which flattens a non-nil EMPTY narrowing into "
        "nil — turning 'this credential sees nothing' into 'this credential is not "
        "narrowed at all', a silent WIDENING inside a line that reads like hygiene. The "
        "same defect `copyIDs` exists for, at the second site that has to reach for it.",
    ),
    Mutant(
        name="constant-time-compare-becomes-equality",
        path="internal/control/ids.go",
        old="\treturn subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1",
        # `_ = subtle.ConstantTimeCompare` keeps the import used. Without it Go
        # refuses the build, and a mutant that does not COMPILE dies at the build
        # rather than at a guard — which is the one outcome that proves nothing.
        new="\t_ = subtle.ConstantTimeCompare\n\treturn a == b",
        killer="",
        why="the readability edit that removes a timing-side-channel defence. It is "
        "FUNCTIONALLY identical, which is precisely why no functional test can see it.",
        equivalent=True,
        equivalent_reason=(
            "String equality and a constant-time compare agree on every input, so no "
            "behavioural test can distinguish them and none should be written to try. The "
            "property at stake is a TIMING one, and it is defended by the comment beside "
            "the call and by review, not by this battery. Recorded here so that a future "
            "reader finding it SURVIVED does not read that as 'the comparison does not "
            "matter'."
        ),
    ),
    # ---- the REPLAY exemption: the one place a bad record is dropped rather than -----
    #      refusing a file, and every direction it must not grow in
    #
    # 🔴 THE GUARD BEING MEASURED HERE IS A TRADE RATHER THAN A CHECK, WHICH IS WHY IT
    # NEEDS SIX ROWS. `Replay` drops a `credential-issued` record it cannot use and loads
    # the rest; a refusal there costs the operator their whole control plane, because
    # `Model.apply` fails a journal WHOLE and `FileStore.Reload` falls back to a
    # `lastKnownGood()` that is empty on a cold start. Both halves can be wrong: too
    # STRICT is an outage on upgrade (measured twice, on shipped builds), and too LOOSE is
    # a dropped revocation, which is the widening `validate`'s default arm refuses by
    # name. The rows below break it in both directions, plus the reporting that is the
    # only reason the loose direction is acceptable at all, plus the APPEND path that must
    # not inherit any of it.
    Mutant(
        name="replay-refuses-the-whole-file-on-an-unusable-digest",
        path="internal/control/journal.go",
        old="\tEventCredentialIssued: {ErrUnusableTokenDigest, ErrDuplicateTokenDigest},",
        new="\tEventCredentialIssued: {ErrDuplicateTokenDigest},",
        killer="TestAnUnusableDigestDropsOnlyItsOwnRecord",
        why="reverting to the shipped behaviour: one `token_hash` an older build's "
        "length-only check accepted loads ZERO credentials on upgrade, and the only remedy "
        "is hand-editing an append-only authority. Measured at `0fb61d4`.",
    ),
    Mutant(
        name="replay-refuses-the-whole-file-on-a-duplicate-digest",
        path="internal/control/journal.go",
        old="\tEventCredentialIssued: {ErrUnusableTokenDigest, ErrDuplicateTokenDigest},",
        new="\tEventCredentialIssued: {ErrUnusableTokenDigest},",
        killer="TestOneSecretRecordedTwiceDropsTheLaterRecord",
        # 🔴 A SEPARATE ROW FROM THE ONE ABOVE BECAUSE THE TWO FAILURES ARRIVE BY DIFFERENT
        # ROUTES AND NEITHER IMPLIES THE OTHER: the first is `Event.validate` refusing a
        # field's SHAPE, the second is `apply` refusing a MODEL-level ambiguity that only
        # exists once another record is present.
        why="reverting the half the PREVIOUS round's own normalisation fix created: "
        "lowering the digest at `apply` made the duplicate compare able to see one secret "
        "written in two case spellings, and a journal holding that then loaded zero "
        "credentials. Measured at `0fb61d4`.",
    ),
    Mutant(
        name="replay-may-drop-a-revocation",
        path="internal/control/journal.go",
        old="\tEventCredentialIssued: {ErrUnusableTokenDigest, ErrDuplicateTokenDigest},",
        new="\tEventCredentialIssued: {ErrUnusableTokenDigest, ErrDuplicateTokenDigest},\n"
        "\tEventCredentialRevoked: {ErrUnusableTokenDigest},",
        killer="TestOnlyCredentialIssuedMayBeDroppedAtReplay",
        # 🔴 THE MUTANT IS A TABLE ENTRY, WHICH IS EXACTLY HOW THIS WOULD ACTUALLY GO
        # WRONG. The exemption reads as a list of tolerated failures, so widening it is a
        # one-line edit with no visible consequence — and "a dropped revocation is a grant
        # that keeps working" is the sentence the rest of this file is built around. It
        # changes no BEHAVIOUR today (no revocation raises that sentinel), which is why the
        # guard has to be a LEDGER over the table rather than a behavioural case.
        why="'this failure is harmless, so tolerate it wherever it shows up' — the "
        "exemption growing along the KIND axis instead of the direction axis.",
    ),
    Mutant(
        name="replay-drops-every-failed-event",
        path="internal/control/journal.go",
        old="\t\tif droppable(e, err) {",
        new="\t\tif true {",
        killer="TestAKindWithNoDroppableEntryStillRefusesTheWholeJournal",
        why="the forward-compatibility 'fix' one level in from the `default:` arm: having "
        "decided that one bad record need not fail a file, apply it to all of them. A "
        "journal from a newer build then replays with its unrecognised records — possibly "
        "revocations — silently skipped.",
    ),
    Mutant(
        name="a-dropped-record-does-not-name-its-credential",
        path="internal/control/journal.go",
        old="\t\t\t\tPosition: i + 1, Kind: e.Kind, CredentialID: e.CredentialID, Reason: err.Error(),",
        new="\t\t\t\tPosition: i + 1, Kind: e.Kind, Reason: err.Error(),",
        killer="TestAnUnusableDigestDropsOnlyItsOwnRecord",
        # 🔴 THE REPORT IS WHAT MAKES DROPPING ACCEPTABLE, SO ITS CONTENT IS A GUARD RATHER
        # THAN A CONVENIENCE. Without the id an operator is told their authority is short
        # and not which line to delete — in a file whose refusal texts deliberately do NOT
        # echo the `token_hash`, so there is nothing else in the line to grep by.
        why="'the reason already describes it' — dropping the one field that identifies "
        "the record, from a diagnostic whose whole job is to let somebody find that line.",
    ),
    Mutant(
        name="append-inherits-the-replay-exemption",
        path="internal/control/filestore.go",
        old="\t\tif err := next.apply(e); err != nil {",
        new="\t\tif err := next.apply(e); err != nil && !droppable(e, err) {",
        killer="TestAppendStillRefusesWhatReplayWouldDrop",
        # 🔴 THE MOST PLAUSIBLE EDIT OF THE WHOLE SET: the two paths now answer the same
        # failure differently, which reads as an inconsistency somebody will "fix". It is
        # the difference between tolerating a file that already exists and CREATING one —
        # and through this arm the event is WRITTEN to the journal as well as skipped, so
        # the file gains a permanent record no replay will ever apply.
        why="making the write path agree with the read path. A caller can then append a "
        "raw secret into the append-only authority and be told it succeeded.",
    ),
    # ---- the credential-issuing COMMAND: the redaction, and the one unretryable exit --
    Mutant(
        name="issued-format-renders-the-token-for-unlisted-verbs",
        path="internal/control/credential_issue.go",
        old="\tdefault:\n\t\tio.WriteString(f, rendered)",
        new="\tdefault:\n\t\tio.WriteString(f, i.Token())",
        killer="TestNoRenderingOfIssuedContainsTheToken",
        # ⚠ THE POINTER INDIRECTION ON `Issued.token` HAS NO ROW HERE, AND THAT IS A LIMIT
        # RATHER THAN AN OVERSIGHT. Reverting it (`*string` back to `string`) is three
        # coordinated edits — the field, `Token()`'s nil arm, and the `&token` at the
        # return — and this battery replaces ONE expression per row on purpose. It was
        # measured by hand instead, and the number is the one that says which half of the
        # redaction is load-bearing: with `Format` present and the field a plain string the
        # widened sweep reports **24 of 154 (shape, verb) pairs** leaking — 21 verbs through
        # an `Issued` in another struct's UNEXPORTED field, where no formatting method is
        # consulted at all, plus `%p` on three shapes — while DELETING `Format` and keeping
        # the pointer reports 0. An earlier note here said "1 of 22", measured by a sweep
        # that only rendered depth 0.
        why="the debugging edit at the NEW site. It is caught at depth 0, where `Format` IS "
        "consulted; what it does not measure is the pointer, which is the half that holds "
        "at every depth below that.",
    ),
    Mutant(
        name="token-undelivered-shares-the-refusal-exit-code",
        path="cmd/cairn-server/issuecredential.go",
        old="\t\treturn exitTokenUndelivered\n",
        new="\t\treturn exitConfig\n",
        killer="TestAFailedTokenDeliveryExitsItsOwnCode",
        # 🔴 THE DANGER IS IN THE CALLER, NOT IN THIS PROGRAM. Every other non-zero exit
        # here happens BEFORE the journal append; this one happens after a durable,
        # unrevocable `credential-issued` record. A wrapper that retries on non-zero then
        # mints a fresh live credential per attempt, and each one is a token nobody holds
        # that nothing in this repository can revoke.
        why="'one program, one refusal code' — the tidying edit that reinstates the "
        "collision, since 78 genuinely is what every other failure here returns.",
    ),
    Mutant(
        name="the-DEFAULT-sinks-delivery-error-is-discarded",
        path="cmd/cairn-server/issuecredential.go",
        old="\t\t_, deliveryErr = fmt.Fprintln(out, issued.Token())",
        new="\t\tfmt.Fprintln(out, issued.Token())",
        killer="TestAFailedDeliveryToTheDEFAULTSinkExitsItsOwnCodeToo",
        # 🔴 THE STATE THIS RESTORES IS THE ONE THAT SHIPPED, AND IT MADE THE 74 A CLAIM
        # ABOUT THE PATH FEWER OPERATORS TAKE. Measured with an always-failing writer:
        # exit 0, no warning, and one credential durably live and unrevocable. The input is
        # ordinary — `cairn-server -issue-credential > /path/on/a/full/fs` returns ENOSPC
        # from a one-line write rather than SIGPIPE — and the operator was then told the
        # token was on stdout and would never be shown again.
        why="the idiomatic 'print it and move on', which is what the line was. Every "
        "assertion about the -token-out path stays green: that arm has its own seam, its "
        "own test and its own mutant, and none of them touch the DEFAULT sink.",
    ),
    Mutant(
        name="dropped-records-are-not-rendered-to-the-operator",
        path="cmd/cairn-server/issuecredential.go",
        old="\tif m, err := store.Model(ctx); err == nil {\n\t\twarnAboutDroppedRecords(m, journal, warn)\n\t}",
        new="\tif m, err := store.Model(ctx); err == nil {\n\t\t_ = m\n\t}",
        killer="TestADroppedJournalRecordReachesTheOperatorsStderr",
        # 🔴 THE SEAM ROW. `internal/control` decides to drop and can only record it as
        # DATA — that package holds no logger by design — so "an operator is told" is a
        # property of a wire neither side's own tests can see. Both halves stay green with
        # this applied: `Model.Dropped` is still populated and this command still issues.
        why="deleting a read whose result is 'only' printed. What it removes is the sole "
        "reason a silently shorter authority was an acceptable trade.",
    ),
    # ---- the materialized cache: staleness, the triggers, and the two write paths --
    #
    # 🔴 THE CACHE'S GUARDS ARE THE ONE PLACE WHERE A GREEN TEST AND A SILENT SECURITY
    # FAILURE ARE THE SAME OBSERVABLE. A cache that serves a revoked grant returns a
    # perfectly ordinary 200; nothing anywhere goes red. The rows below break each of
    # the four things that stand between that and the operator: last-known-good on a
    # failed refresh, an age that keeps growing, the bound that flags it, and the
    # synchronous path that bypasses the wait.
    Mutant(
        name="failed-refresh-empties-the-authority",
        path="internal/control/cache.go",
        old="\tif err != nil {\n\t\tc.failures++",
        new="\tif err != nil {\n\t\tc.model = m\n\t\tc.failures++",
        killer="TestAKilledAuthorityKeepsServingAndTheAgeGrows",
        why="the unconditional swap — assign what the Source returned, then check the "
        "error. An empty Model authorises NOBODY, so this turns a transient authority "
        "outage into a total read outage that looks exactly like a permissions problem, "
        "which is the failure the offline promise exists to rule out.",
    ),
    Mutant(
        name="failed-refresh-restamps-the-age",
        path="internal/control/cache.go",
        old="\t\tc.failures++\n\t\tc.lastErr = err\n\t\treturn err",
        new="\t\tc.failures++\n\t\tc.lastErr = err\n\t\tc.materializedAt = now\n\t\treturn err",
        killer="TestAFailedRefreshDoesNotResetTheReportedAge",
        extra_killers=("TestAKilledAuthorityKeepsServingAndTheAgeGrows",),
        why="stamping 'we tried' onto the field that means 'we succeeded' — one line, "
        "entirely plausible, and it makes a cache whose authority died a week ago report "
        "itself seconds old. The staleness report would then be most wrong exactly when "
        "it is the only instrument left.",
    ),
    Mutant(
        name="refresh-holds-the-lock-across-the-authority",
        path="internal/control/cache.go",
        old="\tm, err := c.src.Model(ctx)\n\tnow := c.clock()\n\n\tc.mu.Lock()",
        new="\tc.mu.Lock()\n\tm, err := c.src.Model(ctx)\n\tnow := c.clock()",
        killer="TestAHungAuthorityDoesNotBlockReads",
        why="the ordinary way to write it — take the lock, then do the work. A HUNG "
        "authority (as opposed to a failing one) then blocks every read behind it, "
        "reintroducing the outage the cache exists to survive INSIDE the thing built to "
        "survive it. No test that only kills the authority can see this.",
    ),
    Mutant(
        name="hot-path-reads-the-authority",
        path="internal/control/cache.go",
        old="func (c *Cache) Model() Model {\n\tc.mu.RLock()\n\tdefer c.mu.RUnlock()\n\treturn c.model\n}",
        new=(
            "func (c *Cache) Model() Model {\n"
            "\tif m, err := c.src.Model(context.Background()); err == nil {\n"
            "\t\treturn m\n"
            "\t}\n"
            "\tc.mu.RLock()\n"
            "\tdefer c.mu.RUnlock()\n"
            "\treturn c.model\n}"
        ),
        killer="TestTheHotPathNeverCallsTheAuthority",
        # 🔴 THIS LIST USED TO NAME `TestAKilledAuthorityKeepsServingAndTheAgeGrows`, AND
        # THAT ENTRY WAS FALSE FROM THE DAY IT WAS WRITTEN — surfaced the moment
        # `extra_killers` stopped being inert. That test `unplug()`s the source, so every
        # `c.src.Model(ctx)` the mutant inserts returns an ERROR and the read-through falls
        # straight back to `c.model`: its whole world is the one branch this edit does not
        # change, so it is structurally incapable of seeing it. The reasoning was already
        # written one row above, on `refresh-holds-the-lock-across-the-authority` — "no
        # test that only kills the authority can see this" — and this list contradicted it.
        # The GUARD did not move; the LIST was aspirational. Replaced with the two that
        # were MEASURED red under this mutant on this tree.
        extra_killers=(
            "TestTheHotPathDoesNotContactTheAuthority",
            "TestAScopeCreatedOutOfBandReachesABareRowAfterARefresh",
        ),
        why="'read through, fall back on error' — which reads like a strictly better "
        "cache and is the single most likely thing a later contributor writes. It puts "
        "a call to the authority on every authorization decision, so an authority that "
        "is merely SLOW becomes the request latency.",
    ),
    Mutant(
        name="exceeded-includes-the-boundary",
        path="internal/control/cache.go",
        old="s.Exceeded = c.maxAge > 0 && s.Age > c.maxAge",
        new="s.Exceeded = c.maxAge > 0 && s.Age >= c.maxAge",
        killer="TestTheStalenessRendersExactly",
        why="the off-by-one on the bound itself. A schedule that refreshes every MaxAge "
        "would then report `stale` on every single tick, which trains the operator to "
        "ignore the word.",
    ),
    Mutant(
        name="unmaterialized-reports-fresh",
        path="internal/control/cache.go",
        old="\tcase !s.Materialized:\n\t\ts.Status = CacheUnmaterialized",
        new="\tcase false:\n\t\ts.Status = CacheUnmaterialized",
        killer="TestAnUnmaterializedCacheAuthorisesNobodyAndSaysSo",
        extra_killers=("TestTheStalenessRendersExactly",),
        why="a cold start that never completed reporting itself healthy. It authorises "
        "nobody while the status surface says `fresh` — the instrument failing silently, "
        "which is the one shape this whole piece exists to refuse.",
    ),
    Mutant(
        name="a-failing-authority-reports-fresh",
        path="internal/control/cache.go",
        old="\tcase s.Failing:\n\t\ts.Status = CacheDegraded",
        new="\tcase false:\n\t\ts.Status = CacheDegraded",
        killer="TestTheStalenessRendersExactly",
        extra_killers=("TestAKilledAuthorityKeepsServingAndTheAgeGrows",),
        why="dropping the only status that distinguishes 'serving last-known-good with "
        "the authority unreachable' from 'serving a fresh read'. Inside the bound the "
        "two then render identically, so an outage is invisible until it is also stale.",
    ),
    Mutant(
        name="run-stops-on-a-failed-refresh",
        path="internal/control/cache.go",
        old="\t\tcase <-tick:\n\t\t\treport(c.refresh(ctx, RefreshTimer))",
        new="\t\tcase <-tick:\n\t\t\tif err := c.refresh(ctx, RefreshTimer); err != nil {\n\t\t\t\treturn err\n\t\t\t}",
        killer="TestAFailedRefreshDoesNotStopTheLoop",
        why="propagating the error, which is what a reviewer asks for on sight of `_ =`. "
        "A transient outage then kills the refresher for good: the age grows forever and "
        "the mechanism that could have fixed it is already dead, in a goroutine nobody "
        "is watching.",
    ),
    Mutant(
        name="timer-refresh-mislabelled",
        path="internal/control/cache.go",
        old="report(c.refresh(ctx, RefreshTimer))",
        new="report(c.refresh(ctx, RefreshExplicit))",
        killer="TestTheTimerTriggerRefreshes",
        why="a copy-paste in the trigger label. `LastTrigger` is the ONLY thing that "
        "distinguishes three mechanisms producing one observable, so a wrong label makes "
        "the report unable to answer 'did my SIGHUP do anything'.",
    ),
    Mutant(
        name="sighup-trigger-dropped",
        path="internal/control/cache.go",
        old="\t\tcase <-tr.Signals:\n\t\t\treport(c.refresh(ctx, RefreshSignal))",
        new="\t\tcase <-tr.Signals:\n\t\t\tcontinue",
        killer="TestSIGHUPRefreshesTheCache",
        why="draining the signal without acting on it — which is indistinguishable from "
        "a working reload to the operator, because `kill -HUP` returns 0 either way. The "
        "token file already trained that muscle memory; a HUP that silently does nothing "
        "is worse than no handler at all.",
    ),
    Mutant(
        name="change-trigger-dropped",
        path="internal/control/cache.go",
        old="\t\tcase <-tr.OnChange:\n\t\t\treport(c.refresh(ctx, RefreshChange))",
        new="\t\tcase <-tr.OnChange:\n\t\t\tcontinue",
        killer="TestTheChangeTriggerRefreshes",
        why="the same drop on the notification path. Here the revocation lag silently "
        "falls back to the timer, so a bound that was being kept by change notifications "
        "quietly becomes the worst case.",
    ),
    Mutant(
        name="bound-check-inverted",
        path="internal/control/cache.go",
        old="if tr.Interval > 0 && c.maxAge > 0 && tr.Interval > c.maxAge {",
        new="if tr.Interval > 0 && c.maxAge > 0 && tr.Interval < c.maxAge {",
        killer="TestRunRefusesAScheduleThatCannotKeepTheBound",
        why="the comparison written the wrong way round, which refuses every LEGAL "
        "schedule and accepts exactly the ones that cannot keep the bound they declare. "
        "A bound nothing keeps is read as a promise.",
    ),
    Mutant(
        name="synchronous-revoke-is-not-synchronous",
        path="internal/control/cache.go",
        # ⚠ THE CONDITION GAINED A SECOND OPERAND (`mine >= c.committed`) WHEN THE
        # GENERATION ORDERING LANDED, AND THIS PATTERN WENT STALE — reported as a
        # HARNESS ERROR rather than as a SURVIVED mutant, which is the whole reason
        # the occurrence count is asserted. Only `materialize` is replaced: mutating
        # the whole condition would remove the ordering guard at the same time and
        # the mutant would die for the wrong reason.
        old="\tif materialize && mine >= c.committed {",
        new="\tif false && mine >= c.committed {",
        killer="TestASynchronousRevokeIsInForceBeforeItReturns",
        extra_killers=("TestAConcurrentRefreshCannotUNDOApplyNow",),
        why="`ApplyNow` silently degrading to `Apply`. The caller is told the revocation "
        "is in force and it is not — the UI then says 'revoked' with no qualifier about "
        "a grant this cache will keep honouring until the next tick.",
    ),
    Mutant(
        name="effect-boundary-excludes-equality",
        path="internal/control/cache.go",
        old="\tif serving >= next.Epoch {",
        new="\tif serving > next.Epoch {",
        killer="TestASynchronousRevokeIsInForceBeforeItReturns",
        why="the off-by-one on the invariant `Effect == immediate iff serving >= "
        "written`. Every synchronous revoke would then report itself deferred — the safe "
        "direction to be wrong in, and still a report that cannot be trusted either way.",
    ),
    Mutant(
        name="effective-by-measured-from-now",
        path="internal/control/cache.go",
        old="\treturn c.materializedAt.Add(c.maxAge)",
        new="\treturn c.clock().Add(c.maxAge)",
        killer="TestAnOrdinaryRevokeIsDeferredAndSaysSo",
        why="measuring the deadline from the wrong end. The stale window did not restart "
        "because somebody wrote, so this promises a UI an 'effective by' later than the "
        "truth — and it is invisible to any fixture that writes at the same instant it "
        "materialized, which is why that fixture moves its clock first.",
    ),
    Mutant(
        name="readonly-refusal-returns-no-error",
        path="internal/control/cache.go",
        old="\t\treturn WriteResult{}, ErrAuthorityReadOnly",
        new="\t\treturn WriteResult{}, nil",
        killer="TestAWriteToAReadOnlyAuthorityIsRefused",
        why="a refusal that refuses silently. The caller gets a zero WriteResult and no "
        "error, so a revocation against a read-only backend reads as an immediate "
        "success over epoch 0.",
    ),
    Mutant(
        name="empty-write-accepted",
        path="internal/control/cache.go",
        old="\tif len(events) == 0 {\n\t\treturn WriteResult{}, ErrNoEvents\n\t}",
        new="\tif false {\n\t\treturn WriteResult{}, ErrNoEvents\n\t}",
        killer="TestAWriteOfNoEventsIsRefused",
        why="allowing the no-op write. It is the one input that makes `serving >= "
        "written` true without anything having happened, so it puts a hole in the "
        "structural invariant the Effect is derived from.",
    ),
    Mutant(
        name="refused-write-claims-an-effect",
        path="internal/control/cache.go",
        old="\t\treturn WriteResult{}, fmt.Errorf(\"control cache: %w\", err)",
        new="\t\treturn WriteResult{Effect: EffectImmediate}, fmt.Errorf(\"control cache: %w\", err)",
        killer="TestAWriteRefusesCleanlyWhenTheAuthorityIsDown",
        why="a populated result alongside an error — the shape a caller that checks the "
        "value before the error will believe. A write that failed against a dead "
        "authority would report itself immediately in force.",
    ),
    # ---- the token-file projection: where the legacy unrestricted row becomes -----
    # ---- explicit grants, and where the enumeration that replaces the sentinel ----
    # ---- is built. -----------------------------------------------------------------
    Mutant(
        name="legacy-row-gains-write",
        path="internal/control/tokenfile/source.go",
        old="\t\t\tVerbs: control.NewVerbSet(control.VerbRead),",
        new="\t\t\tVerbs: control.NewVerbSet(control.VerbRead, control.VerbWrite),",
        killer="TestALegacyRowReachesEveryScopeAndMayWriteNone",
        extra_killers=("TestTheServedAuthorizationMatrixIsExactlyThis",),
        why="one word added to the grant a bare row gets. The server's write refusal is "
        "DERIVED from the absence of that verb, so this silently hands the store's "
        "write verbs to a credential that names no actor — the one thing the refusal "
        "exists to prevent, and it reads like a typo rather than like a change.",
    ),
    Mutant(
        name="mapped-row-loses-write",
        path="internal/control/tokenfile/source.go",
        old="\t\t\tVerbs: control.NewVerbSet(control.VerbRead, control.VerbWrite),",
        new="\t\t\tVerbs: control.NewVerbSet(control.VerbRead),",
        killer="TestAMappedRowReachesAScopeThatHasNoDirectoryYet",
        extra_killers=("TestTheServedAuthorizationMatrixIsExactlyThis",),
        why="the mirror of the row above, and the direction a cautious author would "
        "actually take. It fails CLOSED, so nothing looks broken until somebody's "
        "`cairn append` starts answering 404 for a scope they own.",
    ),
    Mutant(
        name="enumeration-forgets-the-store",
        path="internal/control/tokenfile/source.go",
        old="\tfor _, dir := range dirs {",
        # `dirs[:0]` rather than a nil literal: `dirs` is now a local the root read
        # fills, and a nil literal leaves it unused — a mutant that does not COMPILE
        # dies at the build rather than at a guard.
        new="\tfor _, dir := range dirs[:0] {",
        killer="TestALegacyRowReachesEveryScopeAndMayWriteNone",
        extra_killers=("TestTheServedAuthorizationMatrixIsExactlyThis",),
        why="the enumeration built from the token file alone. It is the whole reason "
        "this adapter reads a directory at all, and it is INVISIBLE TO THE "
        "CONFORMANCE CORPUS — measured: the corpus stays at 116 PASS / 0 failures "
        "with this applied, because every scope in its world is named by a mapped "
        "row. The guards below are the only thing standing on it.",
    ),
    Mutant(
        name="enumeration-forgets-the-allowlist",
        path="internal/control/tokenfile/source.go",
        old="\t\tfor _, scope := range r.Scopes {",
        # `r.Scopes[:0]` rather than a nil literal: the latter leaves the range
        # variable `r` unused, and a mutant that does not COMPILE dies at the build
        # rather than at a guard. Measured — the first draft of this row did exactly
        # that and was reported DID NOT BUILD.
        new="\t\tfor _, scope := range r.Scopes[:0] {",
        killer="TestAMappedRowReachesAScopeThatHasNoDirectoryYet",
        why="the other half of the union, dropped. A store whose every scope already "
        "has a directory hides it completely — the only observable is a mapped row "
        "losing the ability to create its FIRST entry in a scope, which is how the "
        "store gained every scope it has.",
    ),
    Mutant(
        name="the-enumeration-and-the-grant-fold-DIFFERENTLY",
        path="internal/control/tokenfile/source.go",
        # 🔴 THE HAZARD IS A SPLIT, NOT AN ABSENCE, AND THIS ROW WAS WRONG ABOUT THAT
        # UNTIL IT WAS RUN. Its first draft replaced `foldScope`'s whole body and
        # SURVIVED, because `store.ScopeSet` folds BOTH sides of every comparison — so
        # an enumeration recorded in raw form is still reachable under its folded name.
        # What is not survivable is the two sites disagreeing: `scopeNames` records the
        # scope and `grantsFor` derives the grant's object id, and if one folds and the
        # other does not, the grant names a scope the model does not hold, `apply`
        # refuses it, `Replay` fails whole and the pod authenticates NOBODY.
        old="\t\tif folded := foldScope(raw); folded != \"\" {",
        new="\t\tif folded := raw; folded != \"\" {",
        killer="TestTwoDirectoriesThatFoldTogetherDoNotTakeTheAuthorityDown",
        why="one of the two folding sites reverted to the raw name — the shape an edit takes when somebody inlines a helper at the site they happen to be reading. It is not a narrowing and not a widening: it is a projection that cannot be built at all, and the failure lands on the credential table rather than on one scope.",
    ),
    Mutant(
        name="two-bare-rows-mint-two-principals",
        path="internal/control/tokenfile/source.go",
        old="\t\tif prior, minted := seen[identity]; minted {",
        new="\t\tif prior, minted := seen[identity]; false && minted {",
        killer="TestTwoBareRowsAreOnePrincipalWithTwoCredentials",
        why="one project per ROW instead of per identity. `authz.LoadTokens` exempts "
        "`legacy` from its duplicate-identity guard precisely so two bare rows can "
        "coexist during a rotation, so this breaks on the one file shape the "
        "rotation procedure prescribes — and it breaks by taking the whole authority "
        "down, not by narrowing it.",
    ),
    # ---- the projection's two REFUSALS, added in the round-1 fix -------------------
    Mutant(
        name="an-unreadable-store-root-is-swallowed",
        path="internal/control/tokenfile/source.go",
        old='\t\treturn nil, fmt.Errorf("token-file authority: %w: %w", ErrStoreRootUnreadable, err)',
        new="\t\treturn nil, nil",
        killer="TestAnUnreadableStoreRootIsRefusedAndTheCacheIsWhatKeepsTheCredentialTable",
        extra_killers=(
            "TestATransientStoreOutageIsNotBAKEDIntoTheAuthority",
            "TestAColdStartOverAnUnreadableStoreRootREFUSES",
            "TestARefusedReloadDoesNotClaimNothingChanged",
            "TestTheBinaryREFUSESToStartOverAStoreRootItCannotEnumerate",
        ),
        why="the leniency this function shipped with, and the argument for it was TRUE at "
        "the instant of the failure: every read route answers 503 through an unreadable "
        "root, so a principal's visible set is not observable through one. What it missed "
        "is that the projection is CACHED — the empty world gets committed, the status "
        "says `fresh`, and it is served through a root that is readable again. On the "
        "deployed shape the principals are bare rows, so this is not one scope, it is all "
        "of them.",
    ),
    Mutant(
        name="the-dedupe-ignores-the-authority",
        path="internal/control/tokenfile/source.go",
        old="\t\t\tif prior != key {",
        new="\t\t\tif prior != key && false {",
        killer="TestTwoRowsSharingAnIdentityWithDIFFERENTAuthoritiesAreRefused",
        why="the dedupe keyed on the identity STRING alone, which is exactly what this "
        "code did while its comment cited `IsLegacy()`. Every row after the first then "
        "contributes only a credential: a mapped row named `legacy` ahead of a real bare "
        "row gives that bare row `write` on a scope and takes `read` away on another. A "
        "wrong authority from a projection that reports success.",
    ),
    Mutant(
        name="the-authority-key-is-order-sensitive",
        path="internal/control/tokenfile/source.go",
        # `sort.Strings(folded[:0])` rather than deleting the call: `sort` is imported for
        # `scopeNames` too, so deleting it here would still build — but sorting an empty
        # slice is the narrowest edit that removes the ordering claim and nothing else.
        old="\tsort.Strings(folded)",
        new="\tsort.Strings(folded[:0])",
        killer="TestTwoRowsSharingAnIdentityWithDIFFERENTAuthoritiesAreRefused",
        why="the sort dropped from the key, which turns a rotation into a refusal: two "
        "rows for one holder whose allowlists are written in different orders describe "
        "the SAME authority and would be rejected. It is the direction that breaks a file "
        "that is fine, which is why the guard above needs a positive control at all.",
    ),
    Mutant(
        name="derived-id-ignores-its-key",
        path="internal/control/ids.go",
        old="\tsum := sha256.Sum256([]byte(prefix + \"\\x00\" + key))",
        new="\tsum := sha256.Sum256([]byte(prefix))",
        killer="TestALegacyRowReachesEveryScopeAndMayWriteNone",
        extra_killers=(
            "TestTheProjectionIsAPureFunctionOfItsInputs",
            "TestTheServedAuthorizationMatrixIsExactlyThis",
        ),
        why="a derivation that is deterministic and carries no information. Every id of "
        "one prefix collides, so the second scope is refused at replay. It is the "
        "shape an author reaches for when 'make it stable' is the only requirement "
        "they remember.",
    ),
    Mutant(
        name="projection-loses-its-order",
        path="internal/control/tokenfile/source.go",
        old="\tsort.Strings(out)\n\treturn out, nil",
        # Sorting a zero-length slice rather than deleting the call: deleting it
        # leaves the `sort` import unused and the tree does not build, which proves
        # nothing about the ordering guard.
        new="\tsort.Strings(out[:0])\n\treturn out, nil",
        killer="TestTheProjectionIsAPureFunctionOfItsInputs",
        why="map range order reaching the journal. Nothing about the AUTHORITY changes "
        "— the same ids, the same grants, the same epoch — so a comparison of those "
        "would score it EQUIVALENT. What moves is the event ORDER, which is why the "
        "guard compares the serialized journal instead.",
    ),
    # ---- the ordering between two concurrent refreshes, added in the round-1 fix ---
    Mutant(
        name="a-superseded-refresh-commits-anyway",
        path="internal/control/cache.go",
        old="\tif mine < c.committed {",
        new="\tif mine < c.committed && false {",
        killer="TestTwoConcurrentRefreshesCommitInSTARTOrderNotCompletionOrder",
        extra_killers=("TestTheStalenessRendersExactly", "TestAConcurrentRefreshCannotUNDOApplyNow"),
        why="the unconditional assignment this function shipped with. Two triggers now "
        "exist in one process — the timer and SIGHUP — so a refresh that read the "
        "pre-revocation table can commit after the one that read the new one. The revoked "
        "credential authenticates again and the status says `fresh`, which is the exact "
        "failure the whole cache is trusted not to have.",
    ),
    Mutant(
        name="the-refresh-generation-is-taken-AFTER-the-authority-read",
        path="internal/control/cache.go",
        old=(
            "\tmine := c.begin()\n\n"
            "\t// Outside the lock. See the `mu` comment: a hung authority must not block reads.\n"
            "\tm, err := c.src.Model(ctx)"
        ),
        new=(
            "\t// Outside the lock. See the `mu` comment: a hung authority must not block reads.\n"
            "\tm, err := c.src.Model(ctx)\n"
            "\tmine := c.begin()"
        ),
        killer="TestTwoConcurrentRefreshesCommitInSTARTOrderNotCompletionOrder",
        why="the stamp moved to where it reads more naturally — beside the value it "
        "orders. It compiles, it is monotone, and it orders COMPLETIONS instead of "
        "STARTS, which is the defect with a counter bolted on: the slow refresh now gets "
        "the higher generation and wins.",
    ),
    Mutant(
        name="the-write-does-not-publish-its-generation",
        path="internal/control/cache.go",
        old="\t\tc.model = next\n\t\tc.committed = mine",
        new="\t\tc.model = next\n\t\t_ = mine",
        killer="TestAConcurrentRefreshCannotUNDOApplyNow",
        why="`ApplyNow` materializing without recording that it did. A refresh already in "
        "flight then commits on top and republishes the pre-write world, while the call "
        "has already returned `EffectImmediate` — which a UI renders as 'revoked' with no "
        "qualifier. The one failure the synchronous path exists to rule out.",
    ),
    Mutant(
        name="the-write-stamps-BEFORE-the-append",
        path="internal/control/cache.go",
        # 🔴 A MOVE, NOT AN INSERTION, AND THE FIRST DRAFT OF THIS ROW WAS AN INSERTION
        # AND SURVIVED. Adding a second `c.begin()` before the call burns a generation and
        # changes NOTHING about which stamp `mine` holds, so the ordering was still right
        # and the mutant scored SURVIVED — a false finding that reads as a coverage gap.
        # The two statements are adjacent in the source precisely so that the real edit is
        # one contiguous replacement.
        old=(
            "\tnext, err := w.Append(ctx, events...)\n"
            "\tif err != nil {\n"
            '\t\treturn WriteResult{}, fmt.Errorf("control cache: %w", err)\n'
            "\t}\n"
            "\tmine := c.begin()"
        ),
        new=(
            "\tmine := c.begin()\n"
            "\tnext, err := w.Append(ctx, events...)\n"
            "\tif err != nil {\n"
            '\t\treturn WriteResult{}, fmt.Errorf("control cache: %w", err)\n'
            "\t}"
        ),
        killer="TestAConcurrentRefreshCannotUNDOApplyNow",
        why="the stamp taken where a reader expects it — at the top of the operation, "
        "symmetrically with `refresh`. A refresh that starts while `Append` is in flight "
        "then holds the HIGHER generation, so an ambiguous read (it may have seen either "
        "side of the write) overwrites the authority's own answer for the write.",
    ),
    Mutant(
        name="the-write-ignores-a-newer-commit",
        path="internal/control/cache.go",
        old="\tif materialize && mine >= c.committed {",
        new="\tif materialize {",
        killer="TestAWriteDoesNotCommitOverAnAttemptThatSTARTEDAfterIt",
        why="the write committing over a refresh that started AFTER its append returned "
        "— a read that is known to include the write and may include more. "
        "🔴 THIS ROW CARRIED `equivalent=True` FOR ONE ROUND ON A REASON THAT WAS FALSE: "
        "it said the interleaving needed a window 'between two adjacent statements that "
        "no gate can open from outside'. The statements are NOT adjacent — "
        "`now := c.clock()` sits between `c.begin()` and `c.mu.Lock()`, and `clock` is a "
        "caller-injected hook (`CacheOptions.Now`). The killer parks a whole refresh "
        "inside that hook, which is the same technique the round already used for "
        "`Append`, one hook over. A label that reads as coverage while providing none is "
        "worse than no label, and this one was sitting inside the battery built to "
        "refuse that shape.",
    ),
    # ---- the server: where the predicate is asked --------------------------------
    Mutant(
        name="write-gate-accepts-everybody",
        path="internal/api/server.go",
        old="\treturn len(rq.auth.ScopeIDs(control.VerbWrite)) > 0",
        new="\treturn len(rq.auth.ScopeIDs(control.VerbWrite)) >= 0",
        killer="TestTheServedAuthorizationMatrixIsExactlyThis",
        extra_killers=("TestALegacyTokenMayNotWriteButMayStillRead",),
        why="one character. `>= 0` is always true for a length, so the write-route "
        "refusal is gone while the expression still reads like a check — the "
        "classic off-by-one written in the direction that fails OPEN.",
    ),
    Mutant(
        name="write-path-narrows-with-the-read-set",
        path="internal/api/server.go",
        # 🔴 ANCHORED ON THE FUNCTION SIGNATURE, BECAUSE THE LINE ALONE OCCURS TWICE.
        # `createEntry` loads with the same set for the same reason, and a pattern
        # matching both would mutate code this row does not name — which the harness
        # refuses as an occurrence-count error rather than scoring.
        old="func (s *Server) resolveWritable(rq *request, scope, ref string) (*store.Entry, bool, error) {\n\tindex, err := store.LoadStore(s.StoreRoot, \"written\", rq.writable)",
        new="func (s *Server) resolveWritable(rq *request, scope, ref string) (*store.Entry, bool, error) {\n\tindex, err := store.LoadStore(s.StoreRoot, \"written\", rq.visible)",
        killer="TestTheWritePathNarrowsWithTheWriteVERB",
        why="the two sets swapped at the loader. It is INVISIBLE to every principal the "
        "token file can produce, because a mapped row's read and write sets are "
        "equal by construction — which is exactly why the guard builds a principal "
        "the token file cannot spell. A branch no test can reach is not a guard.",
    ),
    Mutant(
        name="create-narrows-with-the-read-set",
        path="internal/api/server.go",
        # ⚠ RE-ANCHORED AT S3 OF THE ARCS/SESSIONS PLAN: the bare `if` line became a substring
        # of `registerArc`'s per-scope check too, and the count assertion refused this row
        # ("occurs 2 time(s)") rather than mutating a site it does not name. The `func` line
        # pins it to the CREATE half again; the mutation itself is unchanged.
        old="func (s *Server) createEntry(rq *request, scope, ref string, body []byte) error {\n"
        "\tif !rq.writable.Allows(scope) {",
        new="func (s *Server) createEntry(rq *request, scope, ref string, body []byte) error {\n"
        "\tif !rq.visible.Allows(scope) {",
        killer="TestTheWritePathNarrowsWithTheWriteVERB",
        why="the same swap on the CREATE half, which consults the set directly rather "
        "than through the loader. Two sites, two mutants: a fix applied to one of "
        "them is the one-rule-two-places failure this repository keeps paying for.",
    ),
    Mutant(
        name="arc-register-narrows-with-the-read-set",
        path="internal/api/server.go",
        old="\tfor _, scope := range reg.DeclaredScopes {\n\t\tif !rq.writable.Allows(scope) {",
        new="\tfor _, scope := range reg.DeclaredScopes {\n\t\tif !rq.visible.Allows(scope) {",
        killer="TestRegisteringAnArcNarrowsWithTheWriteVERB",
        why="the same swap at the THIRD site that consults the write set directly: the arc "
        "registry's per-declared-scope check. A read-only principal could then put an arc "
        "into the listing of a scope it may not write. The token file makes the two sets "
        "equal, so no token-file test can see this; the killer measures it over the "
        "split-verb model.",
    ),
    Mutant(
        name="arcs-check-ignores-the-home-visibility",
        pkgs=PKGS + ("./internal/report/",),
        path="internal/report/arcscheck.go",
        old="\t\tif !visible.Allows(reg.Home) {",
        new="\t\tif false {",
        killer="TestAnArcHomedInAnUnreadableScopeIsNotChecked",
        why="the arc orphan check (S5) is a FOURTH reader of operator decision Q1 — an arc "
        "exists for a caller iff its HOME scope is readable — and it applies the rule in its "
        "own loop rather than through `report.Arcs`. Dropping the guard there is the 'it is "
        "only a health check, check everything' simplification: the check would then NAME "
        "the slug and home of an arc homed in a scope the caller cannot read, and score its "
        "findings, while every listing and the arc page stay correct. The killer feeds an "
        "arc homed in an unreadable scope that declares a readable absent scope, so the "
        "mutant both leaks the name and turns the exit to 9; its positive control is the "
        "same registry for a caller who CAN read the home.",
    ),
    Mutant(
        name="read-set-uses-the-write-verb",
        path="internal/api/server.go",
        # ⚠ RE-DERIVED AT P4: the assignment now reads the `identity.Identity` the
        # authenticator returned rather than a local `auth`. Same line, same claim, new
        # spelling — and the battery is what said so, by reporting a HARNESS ERROR rather
        # than scoring the row SURVIVED against a pattern that matched nothing.
        old="\trq.visible = who.Auth.VisibleScopes(control.VerbRead)",
        new="\trq.visible = who.Auth.VisibleScopes(control.VerbWrite)",
        killer="TestTheServedAuthorizationMatrixIsExactlyThis",
        why="one word in the line that builds the READ set. A bare row holds the write "
        "verb nowhere, so it would read nothing at all — a total read outage for the "
        "unrestricted credential, produced by a verb name.",
    ),
    Mutant(
        name="the-audit-identity-becomes-an-opaque-id",
        path="internal/api/server.go",
        # ⚠ RE-DERIVED AT P4, for the reason the row above records.
        old="\trq.identity = who.Principal.Display",
        new="\trq.identity = string(who.Principal.ID)",
        killer="TestTheAuditRecordIsTheORACLESSPELLINGFieldForField",
        extra_killers=("TestAppendAttributesFromTheTokenAndDiscardsABodyActor",),
        why="the principal's id where its display name belongs. Both are strings off "
        "the same object, and the id is arguably the MORE correct identifier — but "
        "it is what the audit line and every written bullet's actor carry, so this "
        "re-spells a machine-read stream and puts an opaque handle in the store.",
    ),
    Mutant(
        name="a-reload-does-not-rematerialize",
        path="internal/api/server.go",
        # `context.Background()` is kept in the expression so the import stays used;
        # discarding the RESULT is the whole mutation, and it is the narrowest form
        # of "the reload published a table and rebuilt nothing".
        old="\treturn s.authority.Refresh(context.Background())",
        new="\t_ = context.Background()\n\treturn nil",
        killer="TestTheAuthorityReMaterializesOnAReloadAndSaysSoWhenItCannot",
        why="publishing the table without rebuilding the authority from it. The reload "
        "line says LOADED, the operator walks away, and the credential they removed "
        "keeps authenticating until the next timer tick — a revocation that did not "
        "happen, reported as one that did.",
    ),
    Mutant(
        name="the-hot-path-refreshes",
        path="internal/api/server.go",
        # ⚠ RE-DERIVED AT P4. The authority call moved into
        # `identity.MachineToken.Authenticate`, which reaches it through the
        # `TokenAuthority` interface and so CANNOT refresh — the interface deliberately
        # offers only `Authenticate`. The mutation therefore lands one level out, on the
        # server's own call into the authenticator, where `rq.srv.authority` is still in
        # scope. Same edit, same claim: a refresh on every request.
        old="\twho, err := (*held).Authenticate(rq.r)",
        new="\t_ = rq.srv.authority.Refresh(rq.r.Context())\n\twho, err := (*held).Authenticate(rq.r)",
        killer="TestTheHotPathDoesNotContactTheAuthority",
        why="a refresh on every request — the 'just make it fresh' fix. It reads as an "
        "improvement and it deletes the property the whole cache exists for: an "
        "outage of the authority would stop every read, which is the promise cairn "
        "makes about an offline orient-me.",
    ),
    # ---- the program: the modes that EXIT, which is where a credential is minted ----
    Mutant(
        name="issue-credential-mode-is-not-dispatched",
        path="cmd/cairn-server/main.go",
        old="\tif *issue.enabled {",
        new="\tif false && *issue.enabled {",
        killer="TestTheBinaryActuallyDispatchesIssueCredential",
        # 🔴 KILLED BY A DEADLINE, NOT AN ASSERTION, AND THAT IS THE OBSERVABLE THE DEFECT
        # ACTUALLY HAS. A mode that is registered and never dispatched falls through into
        # the SERVER path: the command does not refuse, it starts listening. The child in
        # that test runs under a 20s context for exactly this shape.
        why="the flags registered and the dispatch forgotten — a capability that exists in "
        "`-help`, is exercised by every in-process test of `runIssueCredential`, and "
        "cannot be reached from the command line at all.",
    ),
    Mutant(
        name="two-modes-at-once-picks-a-precedence",
        path="cmd/cairn-server/main.go",
        old="\tif len(asked) > 1 {",
        new="\tif false {",
        killer="TestTheBinaryActuallyDispatchesIssueCredential",
        extra_killers=("TestTheBinaryActuallyDispatchesCreateUser",),
        why="the ledger replaced three pairwise `&&` checks, and a ledger can be wrong in "
        "a way a pair cannot — it governs every combination or none. Without it the "
        "FIRST mode in the dispatch order wins silently: `-routes -issue-credential` "
        "prints the route table and exits 0 having minted nothing, which is the worst of "
        "the three outcomes because the operator has a plausible success and no token.",
    ),
    Mutant(
        name="token-out-writes-a-world-readable-credential",
        path="cmd/cairn-server/issuecredential.go",
        old="const tokenFileMode = 0o600",
        new="const tokenFileMode = 0o644",
        killer="TestTokenOutWritesA0600FileAndNothingOnStdout",
        # 🔴 0644 IS NOT A TYPO HERE, IT IS THE DEFAULT THIS FLAG EXISTS TO REPLACE. The
        # command's own printed remedy was a shell redirection, which creates its file at
        # the umask — 022 on an ordinary host — so the state this mutant restores is
        # exactly the one that shipped, beside a journal that is 0600 by construction.
        why="reaching for the mode a redirection would have produced, on a file holding a "
        "live bearer credential no tool in this repository can revoke.",
    ),
    Mutant(
        name="token-out-clobbers-a-path-that-already-exists",
        path="cmd/cairn-server/issuecredential.go",
        old="os.O_WRONLY|os.O_CREATE|os.O_EXCL, tokenFileMode",
        new="os.O_WRONLY|os.O_CREATE|os.O_TRUNC, tokenFileMode",
        killer="TestTokenOutRefusesAPathThatAlreadyExistsAndMintsNothing",
        # 🔴 O_EXCL IS WHAT MAKES THE MODE A CLAIM AT ALL, which is why this row is separate
        # from the one above rather than a second spelling of it: `OpenFile`'s perm applies
        # only to a file the call CREATES, so with O_TRUNC a pre-existing 0644 path keeps
        # its mode and the 0600 constant becomes decorative. It also destroys whatever
        # credential that file held, and it starts following symlinks.
        why="the idiomatic 'overwrite the output file' flags, which read as convenience "
        "and silently turn the mode guarantee into a property of the lucky case.",
    ),
    Mutant(
        name="missing-principal-refusal-loses-its-branch",
        path="cmd/cairn-server/issuecredential.go",
        old="\t\tif errors.Is(err, control.ErrNoSuchPrincipal) {",
        new="\t\tif false && errors.Is(err, control.ErrNoSuchPrincipal) {",
        killer="TestAPrincipalThatIsNotThereIsRefusedInThisCommandsOwnWords",
        extra_killers=("TestAnIssueThatCannotSucceedIsRefusedAndWritesNothing",),
        # 🔴 THE MUTANT STILL REFUSES, WITH THE MODEL'S OWN TEXT, WHICH IS WHY THE KILLER
        # ASSERTS THE WORDING RATHER THAN THE EXIT CODE. Deleting this branch is how
        # `ErrNoSuchPrincipal` goes back to being a sentinel with no consumer outside its
        # own test — the state an audit measured, and the reason the pre-check's two other
        # stated justifications did not survive.
        why="deleting a branch that looks decorative because the command refuses either "
        "way. What is lost is the only thing the pre-check buys: an operator reading "
        "'this journal holds no user with id …' instead of a sentence about a batch that "
        "would not replay.",
    ),
    Mutant(
        name="the-cold-start-refusal-loses-its-operator-message",
        path="cmd/cairn-server/main.go",
        old="\t\tif errors.Is(err, tokenfile.ErrStoreRootUnreadable) {",
        new="\t\tif errors.Is(err, tokenfile.ErrStoreRootUnreadable) && false {",
        killer="TestTheBinaryREFUSESToStartOverAStoreRootItCannotEnumerate",
        why="the classification dropped, leaving the generic branch. The exit code is "
        "still 78, so nothing about the pod's lifecycle changes — what changes is that "
        "the only sentence the operator gets is the projection's own vocabulary about a "
        "control plane, rather than the name of the volume that is not mounted and the "
        "fact that the server refused to start over it.",
    ),
    Mutant(
        name="a-refused-reload-claims-nothing-changed",
        path="cmd/cairn-server/main.go",
        old='"THE TABLE IS ALREADY SWAPPED: the %d new identities [%s] are published, and the next refresh (within %s) "+',
        new='"NOTHING CHANGED: the %d new identities [%s] are published, and the next refresh (within %s) "+',
        killer="TestARefusedReloadDoesNotClaimNothingChanged",
        why="the wording this line shipped with, and it is false about the TABLE: "
        "`SetTokens` publishes before it refreshes, so on this branch the swap has "
        "already happened and only the projection is stale. An operator told nothing "
        "changed re-edits the file against a copy the pod no longer holds — and the old "
        "line also said `send SIGHUP again`, which describes a step the timer makes "
        "unnecessary. 🔴 THE ARTIFACT UNDER TEST IS PROSE, so the guard bans the phrase "
        "rather than asserting a synonym: a reword that reintroduces it is the defect.",
    ),

    Mutant(
        name="the-refresh-loop-has-no-triggers",
        path="cmd/cairn-server/main.go",
        # 🔴 EMPTYING THE TRIGGER SET RATHER THAN DELETING THE GOROUTINE, AND THAT IS
        # NOT A WEAKER MUTATION — it is the one that COMPILES. Deleting the block
        # leaves `context` and `internal/control` imported and unused, so the tree does
        # not build and the mutant dies at the build, which proves nothing about any
        # guard. Both edits produce the same behaviour: a loop that waits on nothing.
        old="control.RefreshTriggers{Interval: refreshInterval}",
        new="control.RefreshTriggers{}",
        killer="TestTheBinarysOwnTimerIsWhatClosesTheDivergence",
        why="the timer removed from the only loop that re-materializes the authority "
        "without an operator. The divergence `tokenfile` declares is DECLARED rather "
        "than closed, "
        "and the declaration rests entirely on the window being bounded — so this "
        "turns a bounded, reported lag into a permanent one, with every existing "
        "gate green: a bare row simply never sees a scope that `server/seed.sh` "
        "pushed, and nothing anywhere says so.",
    ),
    # ---- P4: the identity interface, and the two new backends ---------------------
    #
    # 🔴 THE TRUSTED-HEADER ROWS ARE THE MOST IMPORTANT IN THIS FILE. A defect there is
    # not a leaked scope, it is impersonation of any user in the control plane, on every
    # route, with the writes attributed to them. Each row below is a configuration
    # somebody could plausibly write while believing the backend was safe.
    Mutant(
        name="proxy-fronted-declaration-not-required",
        path="internal/identity/trustedheader.go",
        old="\tif !cfg.ProxyFronted {",
        new="\tif false {",
        killer="TestEveryTrustedHeaderConstructionRefusalIsReachable",
        why="the flag inferred from 'a header name was configured' rather than demanded. "
        "That is the accident the flag exists to prevent: a config fragment copied "
        "without the one sentence asserting a fact about the network, and a pod that is "
        "reachable directly now honours an identity header from anybody.",
    ),
    Mutant(
        name="trusted-header-needs-no-source-check",
        path="internal/identity/trustedheader.go",
        old="\tif len(cfg.Secret) == 0 && !cfg.RequireClientCert {",
        new="\tif false {",
        killer="TestEveryTrustedHeaderConstructionRefusalIsReachable",
        why="the single most dangerous edit in this repository: proxy-fronted declared, a "
        "header named, and NOTHING proving the request came from the proxy. Anybody who "
        "can open a socket to the pod sets the header and becomes any user.",
    ),
    Mutant(
        name="a-peer-allowlist-counts-as-a-source-check",
        path="internal/identity/trustedheader.go",
        # 🔴 THE REALISTIC WRONG BELIEF, NOT A TEXTBOOK MUTATION. "I restricted it to the
        # proxy's address, so it is safe" is what somebody actually writes. The plan says
        # shared secret OR mTLS for a reason: an address proves only that something
        # occupying it sent the request, which is a NetworkPolicy question.
        old="\tif len(cfg.Secret) == 0 && !cfg.RequireClientCert {",
        new="\tif len(cfg.Secret) == 0 && !cfg.RequireClientCert && len(cfg.ProxyPeers) == 0 {",
        killer="TestEveryTrustedHeaderConstructionRefusalIsReachable",
        why="a peer allowlist accepted in place of a source check — 'safe because of "
        "where it happens to be deployed', which is the reasoning this repository "
        "refuses at the path-component guard too.",
    ),
    Mutant(
        name="the-subject-header-may-arrive-twice",
        path="internal/identity/trustedheader.go",
        old="\tif len(subjects) != 1 {",
        new="\tif len(subjects) < 1 {",
        killer="TestAnAttackerReachingThePodDirectlyGetsNothing",
        why="a proxy that APPENDS rather than overwrites lets a caller smuggle a second "
        "identity past it; taking the first or the last value matches on whichever copy "
        "happens to be right. `netid.ClientIP` makes the identical ruling.",
    ),
    Mutant(
        name="the-secret-header-may-arrive-twice",
        path="internal/identity/trustedheader.go",
        old="\t\tif len(values) != 1 {",
        new="\t\tif len(values) < 1 {",
        killer="TestAnAttackerReachingThePodDirectlyGetsNothing",
        why="the same smuggling shape one header over: a caller appends a guess beside "
        "the real secret and one of them matches.",
    ),
    Mutant(
        name="the-proxy-secret-is-compared-with-equality",
        path="internal/identity/trustedheader.go",
        # 🔴 THE EDIT KEEPS `crypto/subtle` REFERENCED, AND THAT IS NOT COSMETIC. The
        # first draft replaced the call outright, which left the import unused so the
        # tree did not build — and a mutant that dies at the BUILD proves nothing about
        # any guard. `ConstantTimeEq(0, 0)` is 1, so the disjunct is always false and the
        # behaviour is exactly the string comparison this row is about.
        old="\t\tif subtle.ConstantTimeCompare([]byte(values[0]), t.secret) != 1 {",
        new="\t\tif subtle.ConstantTimeEq(0, 0) == 0 || string(values[0]) != string(t.secret) {",
        killer="",
        why="string equality in place of a constant-time compare, against a secret that "
        "grants impersonation of every user and is reachable by anyone who can address "
        "the pod.",
        equivalent=True,
        equivalent_reason=(
            "String equality and a constant-time compare agree on every input, so no "
            "behavioural test can distinguish them and none should be written to try — "
            "the property at stake is a TIMING one. It is listed so a reader finding it "
            "SURVIVED does not read that as 'the comparison does not matter'. The same "
            "label, for the same reason, as `constant-time-compare-becomes-equality` one "
            "package over."
        ),
    ),
    Mutant(
        name="the-user-lookup-ignores-the-provider",
        path="internal/control/resolve.go",
        old="\t\tif u.Provider == provider && u.Subject == subject {",
        new="\t\tif u.Subject == subject {",
        killer="TestTheProviderNamespaceIsPartOfTheLookup",
        why="a subject matched across identity-provider namespaces. Two IdPs can issue "
        "the same subject string, so this lets one provider's user become another's — "
        "and `control.User`'s own comment is the record of why the key is "
        "provider+subject.",
    ),
    # ---- the JWT verifier: algorithm confusion, and the claims that bound a session --
    Mutant(
        name="the-symmetric-algorithm-refusal-is-removed",
        path="internal/identity/jws.go",
        old="\tif symmetricAlg(alg) {",
        new="\tif false {",
        killer="TestTheAlgorithmConfusionForgeryIsREFUSED",
        why="the algorithm-confusion forgery, and the row that reads as EQUIVALENT until "
        "you look. `alg: HS256` signed with the deployment's own PUBLIC key as the HMAC "
        "secret is still refused with this gone — measured: an ordinary deployment gets "
        "`accepts` (nothing puts HS256 in `Algs`) and one that explicitly accepts HS256 "
        "gets `KeySet.key`'s two-valued `algKeyType` lookup, both `ErrTokenAlg`, both "
        "fail-closed. What dies with the guard is the refusal's NAME: the token stops "
        "being refused BECAUSE it is symmetric and starts being refused because nobody "
        "configured it, which is a property of today's tables rather than a rule. That "
        "distinction is the whole reason `ErrTokenAlgSymmetric` is a second sentinel "
        "narrowing `ErrTokenAlg` rather than a second message — and it is what makes a "
        "one-line re-addition of a symmetric algorithm to `algKeyType` insufficient to "
        "reopen the hole. Six arms go red, each naming this sentinel.",
    ),
    Mutant(
        name="exp-is-not-required",
        path="internal/identity/jws.go",
        old="\tif !c.Expiry.Present {",
        new="\tif false {",
        killer="TestEveryCLAIMRefusalIsReachable",
        why="a token with no `exp` never expires, so a session leaked once is a "
        "credential forever that nothing short of rotating the signing key can revoke. "
        "Every other claim rule here is a narrowing; this one is what makes it a session.",
    ),
    Mutant(
        name="the-issuer-is-not-checked",
        path="internal/identity/jws.go",
        old="\tif c.Issuer != opts.Issuer {",
        new="\tif false {",
        killer="TestEveryCLAIMRefusalIsReachable",
        why="any issuer whose key happened to reach the key set could then mint sessions "
        "here.",
    ),
    Mutant(
        name="the-audience-is-not-checked",
        path="internal/identity/jws.go",
        old="\tif !c.Audience.has(opts.Audience) {",
        new="\tif false {",
        killer="TestEveryCLAIMRefusalIsReachable",
        why="a token minted for a DIFFERENT application at the same issuer verifies here "
        "— the cross-application replay the `aud` claim exists for.",
    ),
    Mutant(
        name="the-clock-skew-allowance-is-unbounded",
        path="internal/identity/supabase.go",
        old="\tif cfg.Leeway < 0 || cfg.Leeway > MaxLeeway {",
        new="\tif false {",
        killer="TestEverySupabaseConstructionRefusalIsReachable",
        why="a generous skew allowance is how a revoked session outlives its revocation, "
        "and the value that produces it is one an operator types once and never rereads.",
    ),
    Mutant(
        name="a-negative-max-token-age-is-read-as-OFF",
        path="internal/identity/supabase.go",
        old="\tif cfg.MaxAge < 0 {",
        new="\tif false {",
        killer="TestEverySupabaseConstructionRefusalIsReachable",
        extra_killers=("TestAPartiallyConfiguredBackendRefusesToStart",),
        why="`checkClaims` tests `opts.MaxAge > 0`, so a negative duration is read as "
        "ZERO and zero means the check is OFF. An operator who wrote "
        "`CAIRN_OIDC_MAX_AGE=-1h` — a sign typo, or a value templated from a "
        "subtraction — gets a bound they configured and nothing enforcing it, which is "
        "the shape `parseBool` refuses one file over for the same reason.",
    ),
    Mutant(
        name="crit-extensions-are-ignored",
        path="internal/identity/jws.go",
        old="\tif len(header.Crit) != 0 {",
        new="\tif false {",
        killer="TestEveryTokenSHAPERefusalIsReachable",
        why="RFC 7515 4.1.11 requires a recipient to REJECT a token naming extensions it "
        "does not implement. Ignoring the field accepts a token whose issuer believes an "
        "extension was enforced.",
    ),
    # ---- the cached key set: the outage promise and its honesty -------------------
    Mutant(
        name="an-empty-jwks-document-is-adopted",
        path="internal/identity/jwks.go",
        old="\tif len(out) == 0 {",
        new="\tif false {",
        killer="TestAJWKSDocumentThatYieldsNoUsableKeyDoesNotReplaceAGoodOne",
        why="a provider answering 200 with a page that is not a JWKS replaces the good "
        "key set with an empty one — which refuses every session while reporting itself "
        "freshly fetched. The identical defect `tokenfile.Source` already shipped one "
        "layer down with an unreadable store root.",
    ),
    Mutant(
        name="a-failed-fetch-resets-the-reported-age",
        path="internal/identity/jwks.go",
        old="\t\tk.failures++\n\t\tk.lastErr = err\n\t\treturn err",
        new="\t\tk.failures++\n\t\tk.lastErr = err\n\t\tk.fetchedAt = now\n\t\treturn err",
        killer="TestAnIdentityProviderOutageDoesNotStopAnAlreadyIssuedSession",
        why="advancing the timestamp on a FAILED attempt makes a key set whose provider "
        "has been dead for a week report itself seconds old — a staleness report that is "
        "most wrong exactly when it is most needed. `control.Cache.Refresh` carries the "
        "same rule and the same reasoning.",
    ),
    # ---- the chain and the seam into the server -----------------------------------
    Mutant(
        name="the-chain-propagates-an-identity-naming-nobody",
        path="internal/identity/identity.go",
        old="\t\tif !got.Valid() {",
        new="\t\tif false {",
        killer="TestABackendThatNamesNobodyIsRefusedRatherThanPropagated",
        why="a backend returning `Identity{}, nil` has said yes without naming anybody. "
        "No scope leaks — a zero Authorization permits nothing — but the request is "
        "audited as authenticated and satisfies every guard that asks whether "
        "authentication succeeded.",
    ),
    Mutant(
        name="the-server-serves-an-identity-naming-nobody",
        path="internal/api/server.go",
        old="\tif !who.Valid() {",
        new="\tif false {",
        killer="TestTheServerRefusesAnIdentityNamingNobody",
        why="the SECOND of the two validity checks, and not a duplicate of the first: a "
        "server wired with a single backend rather than a chain never passes through the "
        "chain's check at all, so without this the zero identity reaches every route.",
    ),
    Mutant(
        name="the-audit-auth-field-is-derived-from-the-fingerprint",
        path="internal/api/server.go",
        old='\tauth := "fail"\n\tif rq.authenticated {',
        new='\tauth := "fail"\n\tif rq.tokenFP != "" {',
        killer="TestASessionWithNoTokenFingerprintIsAuditedAsAuthenticated",
        why="the pre-P4 derivation, which was correct while every credential was a bearer "
        "token and stops being correct the moment one is not. A browser session is then "
        "logged `auth=fail` on a fully authorised 200, so an operator grepping for failed "
        "authentications finds every successful sign-in.",
    ),
    Mutant(
        name="the-machine-token-backend-captures-the-authority",
        path="internal/api/server.go",
        old="identity.NewMachineToken(s.AuthorityView())",
        new="identity.NewMachineToken(s.authority)",
        killer="TestTheAuthorityIsNotHeldTwice",
        why="a SECOND holder of one cache. Anything that replaces the server's authority "
        "leaves the refresh loop maintaining one and every authentication reading "
        "another, with nothing observable except credentials resolved against a world "
        "nobody is keeping current. This is the defect P4's first draft shipped; an "
        "existing guard caught it.",
    ),
    # ---- the environment: a partial configuration must REFUSE ---------------------
    Mutant(
        name="a-partial-proxy-configuration-is-silently-off",
        path="internal/identity/config.go",
        old="\tif proxyArmed {",
        # 🔴 `false && proxyArmed`, NOT `false` — `proxyArmed` COMES OUT OF A `:=` WITH
        # SIBLINGS, so a mutant that stops referencing it leaves it declared and unused and
        # the tree does not build. A mutant that dies at the build is the one outcome that
        # proves nothing; the `an-unrecognised-boolean-reads-as-false` row below records the
        # same shape for `parseBool`'s `default` arm.
        new="\tif false && proxyArmed {",
        killer="TestAPartiallyConfiguredBackendRefusesToStart",
        why="the trigger becomes 'the required fields are present' rather than 'anybody "
        "touched any of these'. An operator who set the subject header and forgot the "
        "secret then gets a pod that comes up healthy with a backend they believe is "
        "live and that authenticates nobody.",
    ),
    Mutant(
        name="an-unrecognised-boolean-reads-as-false",
        path="internal/identity/config.go",
        # 🔴 THE WHOLE `default` ARM, REPLACED BY ONE THAT RETURNS NO ERROR — and it
        # keeps `fmt`, `name` and `raw` referenced for the reason the row above records.
        # The first draft deleted the arm by renaming it to a case nothing matches, which
        # left the reader with a path that returns nothing: the tree did not build, and a
        # mutant that dies at the build is the one outcome that proves nothing.
        old='\tdefault:\n\t\treturn false, fmt.Errorf(\n\t\t\t"%s: %q is not a boolean. Write yes or no — an unrecognised value is refused rather than read as `no`, because a typo that silently disables a security setting leaves the operator believing it is on",\n\t\t\tname, raw)',
        new='\tdefault:\n\t\t_ = fmt.Sprintf("%s %s", name, raw)\n\t\treturn false, nil',
        killer="TestAnUnrecognisedBooleanIsAnErrorRatherThanFalse",
        why="`CAIRN_TRUSTED_HEADER_PROXY_FRONTED=treu` read as 'not proxy-fronted'. The "
        "operator who typed it believes the backend is armed, and a setting that reads a "
        "typo as its own default is how a deployment ends up in a state nobody chose. "
        "The edit replaces the `default:` arm with a case nothing matches, which is the "
        "shape that COMPILES — deleting the arm leaves `fmt` unused in a function that "
        "must still return, and a mutant that dies at the build proves nothing. "
        "⚠ THE READER WAS CALLED `envBool` WHEN THIS ROW WAS WRITTEN and is `parseBool` now "
        "— it takes a value the declared blank policy already resolved rather than the "
        "environment. The `default:` arm and its message are byte-identical across that "
        "rename, which is why the anchor survived it.",
    ),
    Mutant(
        name="a-retired-setting-is-silently-ignored",
        path="internal/identity/config.go",
        old='\t\tif _, ok := touched(env, name); ok {\n\t\t\tnames = append(names, name)\n'
        '\t\t}\n\t}\n\tif len(names) == 0 {',
        new='\t\tif _, ok := touched(env, name); ok {\n\t\t\tnames = append(names, name)\n'
        '\t\t}\n\t}\n\tif len(names) >= 0 {',
        killer="TestAPartiallyConfiguredBackendRefusesToStart",
        extra_killers=("TestTheEnvironmentLedgersNameEveryVariableEachBackendReads",),
        why="the INVERSE of `a-partial-proxy-configuration-is-silently-off`, and the "
        "failure mode a deletion introduces rather than one a half-finished "
        "configuration does. `anySet` only counts names that are in a ledger, so a "
        "variable DROPPED from one is invisible to it by construction: an operator whose "
        "manifest still carries `CAIRN_SUPABASE_JWT_SECRET` gets a pod that comes up "
        "healthy having silently discarded the line they wrote. "
        "⚠ THE EDIT IS THE EARLY RETURN, NOT THE `TrimSpace` TEST INSIDE THE LOOP, AND "
        "THE FIRST DRAFT WAS THE LATTER — which is character-for-character identical to "
        "`anySet`'s own line, so the harness matched TWICE and reported a HARNESS ERROR "
        "rather than a result. Mutating the call site is no better: "
        "`if err := refuseRetiredSettings(env); false {` leaves `err` declared and "
        "unused, and a mutant that dies at the build proves nothing. "
        "🔴 AND THE EARLY RETURN IS NOT UNIQUE EITHER — IT HAPPENED AGAIN. `if len(names) "
        "== 0 {` alone was the anchor until `refuseBlankSettings` landed in the same file "
        "with a character-for-character identical early return, and the harness reported "
        "the same HARNESS ERROR a second time. The anchor is therefore the whole "
        "collect-then-return BLOCK, whose membership test is what distinguishes this "
        "function from its sibling. The general lesson the two sightings agree on: in a "
        "file of small guards written to one shape, no SINGLE line is a stable anchor — "
        "take enough of the block to name the function. "
        "🔴 THIRD SIGHTING, AND IT BROKE THE OTHER WAY: THE DISTINGUISHING LINE ITSELF "
        "MOVED. It was `strings.TrimSpace(env[name]) != \"\"` until the round that made a "
        "retired name holding only whitespace REFUSE instead of being discarded, which "
        "replaced it with `value, present := env[name]; present && value != \"\"` — so the "
        "anchor matched zero times and the harness reported an error a third time. A "
        "fixed anchor cannot be made drift-proof by choosing a better line; what it can "
        "be is LOUD, which it was. Widest reading: any edit to this function is an edit "
        "to this row. "
        "🔴 FOURTH SIGHTING, AND THE DISTINGUISHING LINE MOVED AGAIN — this time because "
        "the predicate it spelled was CONSOLIDATED. `present && value != \"\"` was "
        "open-coded here and again in the blank sweep; the design pass that gave every "
        "setting a declared blank policy replaced both with one `touched` call, so the "
        "anchor matched zero times a fourth time. The tell is the same and the lesson is "
        "one level up from \"pick a better line\": an anchor spells an EXPRESSION, and "
        "consolidating a duplicated predicate deletes expressions by design.",
    ),
    Mutant(
        name="a-secret-may-come-from-two-sources",
        path="internal/identity/config.go",
        old='\tif direct != "" && path != "" {',
        new="\tif false {",
        killer="TestASecretHasExactlyOneSource",
        why="two sources for one secret means 'which is live' depends on a precedence "
        "nobody reads, and a rotation that updated the other appears to work.",
    ),
    # ---- P5a: the user-creation path, and the authority the SESSIONS resolve against --
    #
    # 🔴 THESE THIRTEEN EXIST BECAUSE P4 SHIPPED THREE BACKENDS AND NO WAY TO PUT A USER IN
    # FRONT OF THEM. Every row above was green through a period in which no deployment
    # that could exist authenticated anybody through a session backend: the only authority
    # any binary wired was the token-file projection, whose one synthetic user sits at
    # provider `cairn-token-file` and holds no membership. Measured at `229c142`: a
    # trusted-header backend aimed at that exact pair authenticated, `Valid()` returned
    # true, and the authorization carried ZERO readable scopes. So the rows here mutate
    # the WIRING and the batch CONTENT — the two places where "it authenticated" and "it
    # can do something" come apart.
    # 🔴 TWO ROWS, ONE PER BACKEND, BECAUSE "THE SESSION BACKENDS RESOLVE AGAINST THE
    # CONFIGURED AUTHORITY" IS A CLAIM ABOUT A SET. There used to be one row here, over a
    # shared `sessionAuthority` local that both constructors read; that local is gone —
    # it existed only to hold the fallback to `authority`, which is now a refusal — so the
    # parameter is passed at two sites and a mutant at one leaves the other asserted by
    # nothing.
    Mutant(
        name="the-supabase-backend-resolves-against-the-token-file-authority-again",
        path="internal/identity/config.go",
        old="\t\tsupabase, err = supabaseFromEnv(supabaseValues, sessions)",
        new="\t\tsupabase, err = supabaseFromEnv(supabaseValues, authority)",
        killer="TestAnOperatorProvisionedSupabaseSessionAuthenticatesWithRealAuthority",
        why="the parameter ignored, which is exactly the state this slice found the "
        "repository in. Nothing fails to build, no backend is missing, the chain has the "
        "right length — and a browser sign-in resolves a provider-named subject in a "
        "projection that has none, or worse, in a DIFFERENT world's user of the same name.",
    ),
    Mutant(
        name="the-trusted-header-backend-resolves-against-the-token-file-authority-again",
        path="internal/identity/config.go",
        old="\t\ttrusted, err = trustedHeaderFromEnv(proxyValues, sessions)",
        new="\t\ttrusted, err = trustedHeaderFromEnv(proxyValues, authority)",
        killer="TestAnOperatorProvisionedTrustedHeaderSessionAuthenticatesWithRealAuthority",
        why="the same defect one backend over, and the one whose blast radius "
        "`TrustedHeader`'s own comment calls the most dangerous thing in P4. A proxy that "
        "has already established an identity hands it to a resolver looking in the wrong "
        "world; the request authenticates and reads somebody else's authorization or none.",
    ),
    Mutant(
        name="a-session-backend-with-no-authority-comes-up-quietly",
        path="internal/identity/config.go",
        old="\tif (supabaseArmed || proxyArmed) && sessions == nil {",
        new="\tif false {",
        killer="TestASessionBackendWithNoSessionAuthorityRefusesToStart",
        why="the MIRROR of the row below, and the direction the defect was measured in. "
        "Without it an armed session backend with no `$CAIRN_CONTROL_JOURNAL` falls back "
        "to the token-file projection: the pod starts, the JWT verifies, `Valid()` is "
        "true, an audit line names a principal — and the authorization is EMPTY. "
        "Measured at `e11c3a7`, where this guard did not exist: err=nil, a two-backend "
        "chain, and a sign-in that authenticates into nothing. It is the strictly worse "
        "of the two directions, because the loud one refuses everybody and this one "
        "refuses nobody.",
    ),
    Mutant(
        name="a-journal-nobody-reads-comes-up-quietly",
        path="internal/identity/config.go",
        old="\tif sessions != nil && supabase == nil && trusted == nil {",
        new="\tif false {",
        killer="TestASessionAuthorityNobodyReadsRefusesToStart",
        why="the partial-configuration refusal deleted one level up from the ledgers. An "
        "operator who provisioned users and forgot the Supabase block gets a pod that "
        "comes up healthy with nothing reading the journal — indistinguishable, from "
        "outside, from an empty journal or a wrong subject.",
    ),
    Mutant(
        name="duplicate-provider-subject-accepted",
        path="internal/control/journal.go",
        old="\t\tfor id, u := range m.Users {\n\t\t\tif u.Provider == e.Provider && u.Subject == e.Subject {",
        new="\t\tfor id, u := range m.Users {\n\t\t\tif false {\n\t\t\t\t_ = id\n\t\t\t\t_ = u",
        killer="TestTwoUsersCannotShareOneProviderSubjectPair",
        why="the uniqueness rule on the natural key every identity backend looks a "
        "session up by. `UserByProviderSubject` returns the FIRST match over a Go map, so "
        "two rows make 'who is this session' answer a different user id — with a different "
        "authorization — on different requests in one process. Nothing errors.",
    ),
    Mutant(
        name="scope-name-collision-across-projects-accepted",
        path="internal/control/provision.go",
        # The narrowest edit that still COMPILES: the check is called with the wrong
        # operand rather than deleted, because deleting the call leaves `current` unused
        # and the mutant would die at the build.
        old="\tif err := checkScopeNamesAreFree(current, req.ScopeNames); err != nil {",
        new="\tif err := checkScopeNamesAreFree(current, nil); err != nil {",
        killer="TestAScopeNameAlreadyInTheJournalIsRefused",
        # ⚠ THE THIRD ENTRY IS THE WITHIN-REQUEST HALF, WHICH THIS EDIT ALSO DISABLES —
        # `nil` removes the operand both halves read. It is listed because the ledger is
        # asserted now: if this row's edit ever stops reaching that half, the battery says
        # so rather than scoring a quieter kill.
        extra_killers=(
            "TestAScopeNameThatFOLDSOntoOneAlreadyHeldIsRefused",
            "TestTwoScopeNamesInONEREQUESTThatFoldAlikeAreRefused",
        ),
        why="the guard that is WIDER than the model's own rule, so it looks redundant "
        "beside `apply`'s within-a-project uniqueness and reads as a candidate for "
        "deletion. It is not: the reader narrows the store root's DIRECTORIES by display "
        "name, so two scope records sharing one name resolve to one directory and each "
        "project's members read the other's entries.",
    ),
    Mutant(
        name="scope-name-collision-by-FOLD-accepted",
        path="internal/control/provision.go",
        # The narrowest expression that can be wrong: the COMPARISON, with both folds
        # dropped — which is the guard exactly as it shipped one round earlier.
        old="\t\t\tif store.NormalizeRef(sc.DisplayName) == folded {",
        new="\t\t\tif sc.DisplayName == name {",
        killer="TestAScopeNameThatFOLDSOntoOneAlreadyHeldIsRefused",
        why="THE TENANCY BOUNDARY DEFEATED BY A CAPITAL LETTER, and the shipped state at "
        "`18df63d`. What decides 'one directory' is `store.NormalizeRef`, applied on both "
        "sides of `store.ScopeSet.Allows` and again on the write path — so `Quarry_Notes` "
        "and `quarry-notes` are ONE directory to every reader and every writer, and a raw "
        "`==` here is strictly narrower than the thing it protects. Measured: two "
        "`-create-user` runs in different projects both succeeded, both owners held "
        "`RoleOwner`, and the second read AND wrote the first's entries. There is no undo "
        "— nothing emits `scope-renamed` and the journal is append-only. ⚠ The row above "
        "does NOT cover this: it deletes the guard's operand, so a guard that is present "
        "but too narrow survives it.",
    ),
    Mutant(
        name="within-ONE-request-a-FOLDED-duplicate-accepted",
        path="internal/control/provision.go",
        # The narrowest expression that can be wrong: the key the request's own claim is
        # RECORDED under. Reading it back by `folded` while storing it by the RAW name is
        # the same guard written raw, which is what the journal half already shipped once.
        old="\t\tclaimed[folded] = name",
        new="\t\tclaimed[name] = name",
        killer="TestTwoScopeNamesInONEREQUESTThatFoldAlikeAreRefused",
        why="the WITHIN-REQUEST half, defeated by a capital letter exactly as the journal "
        "half was at `18df63d`. Measured at `8c06ea1`, before the half existed: "
        "`-create-user -project quarry -scopes \"Quarry_Notes,quarry-notes\"` was ACCEPTED "
        "— two `scope-created` events, two distinct scope ids, both folding to "
        "`quarry-notes`, i.e. ONE directory, append-only and with no undo. ⚠ Neither row "
        "above covers it: one deletes the operand both halves read, the other mutates the "
        "comparison against `m.Scopes`, and a request is not in `m.Scopes`. Bounded today "
        "(one project, one owner) and an authorization defect the moment SHARING lands, "
        "because a grant names a scope ID and granting one of the pair hands over the "
        "other's bytes while every id-keyed check agrees it was honoured exactly.",
    ),
    Mutant(
        name="membership-omitted-from-the-provisioning-batch",
        path="internal/control/provision.go",
        old="\t\t{\n\t\t\tKind: EventMemberSet, At: at, Actor: req.Actor,\n\t\t\tProjectID: projectID, UserID: userID, Role: RoleOwner,\n\t\t},\n\t}",
        new="\t}",
        killer="TestProvisioningAUserYieldsAnAuthorityThatAuthorisesThem",
        why="the user is created, the project is created, the project NAMES them as its "
        "owner — and `Resolve` reads membership, not `Project.OwnerUserID`. Every "
        "structural check passes and the authorization is empty, which is the precise "
        "state this whole slice exists to leave behind.",
    ),
    Mutant(
        name="filestore-model-serves-the-process-local-cache-again",
        path="internal/control/filestore.go",
        old="func (s *FileStore) Model(ctx context.Context) (Model, error) {\n\treturn s.Reload(ctx)\n}",
        new="func (s *FileStore) Model(ctx context.Context) (Model, error) {\n"
        "\ts.mu.RLock()\n\tif s.loaded {\n\t\tm := s.cached\n\t\ts.mu.RUnlock()\n\t\treturn m, nil\n\t}\n"
        "\ts.mu.RUnlock()\n\treturn s.Reload(ctx)\n}",
        killer="TestAFileStoreSeesAWriteFromAnotherProcessThroughItsOrdinaryReadPath",
        extra_killers=("TestAnUnreadableJournalLeavesTheFileStoreServingLastKnownGood",),
        why="the short-circuit this method shipped with, restored verbatim. It served a "
        "process-local projection invalidated only by THIS VALUE's appends, so a pod "
        "reading through it would materialize the journal once at startup and never see a "
        "user created by `cairn-server -create-user` in another process — the journal "
        "correct, the command successful, the sign-in still refused. ⚠ It is the ONE "
        "mutant here that is not a one-expression edit: the guard IS the branch, so the "
        "narrowest thing that can be wrong is its presence. `--show` prints it in full.",
    ),
    Mutant(
        name="unconfigured-deployment-gets-a-TYPED-nil-session-authority",
        path="cmd/cairn-server/createuser.go",
        old="\tif journal == \"\" {\n\t\treturn nil, nil\n\t}\n\tstore, err := control.OpenFileStore(journal)",
        new="\tif journal == \"\" {\n\t\tvar typed *control.Cache\n\t\treturn typed, nil\n\t}\n\tstore, err := control.OpenFileStore(journal)",
        killer="TestNoControlJournalMeansNoSessionAuthorityAtAll",
        extra_killers=(
            "TestARefusedReloadDoesNotClaimNothingChanged",
            "TestTheBinaryREFUSESToStartOverAStoreRootItCannotEnumerate",
            "TestTheBinarysOwnTimerIsWhatClosesTheDivergence",
        ),
        why="a typed nil in an interface is NOT nil, and that is the Go trap most likely "
        "to be written here by somebody tidying the two return paths into one. It turns "
        "every existing deployment — which configures no session backend — into "
        "`ErrSessionAuthorityUnread` and a refusal to start: a live pod's auth path broken "
        "by a feature it does not use. The three extra killers are the server-lifecycle "
        "tests, which is what that break looks like from outside.",
    ),
    Mutant(
        name="blank-control-journal-test-narrowed-to-TrimSpace",
        path="cmd/cairn-server/createuser.go",
        old="\tif identity.ValueReducesToNothing(raw) {",
        new="\tif strings.TrimSpace(raw) == \"\" {",
        killer="TestABlankControlJournalIsRefusedRatherThanReadAsUnset",
        why="the local spelling of the blank test, which is why the predicate is exported "
        "from `internal/identity` rather than re-written here. `TrimSpace` uses "
        "`unicode.White_Space`, which does NOT hold the zero-width runes — the measured "
        "bypass that cost this repository a live shared secret at a different setting.",
    ),
    Mutant(
        name="the-journal-refresh-failure-is-never-reported",
        path="cmd/cairn-server/createuser.go",
        # ⚠ THE PATTERN CARRIES THE REPORTER'S THIRD ARGUMENT, AND THE BATTERY'S OWN
        # HARNESS CHECK IS WHAT CAUGHT IT GOING STALE. When `journalRefreshReporter` grew
        # the new-drop announcer, this row's `old` matched 0 times — which the runner
        # reports as a HARNESS ERROR rather than scoring the mutant SURVIVED, because a
        # pattern that matches nothing never runs and would otherwise read as a guard that
        # cannot go red.
        old="\t\tInterval:  refreshInterval,\n\t\tOnRefresh: journalRefreshReporter(journal, warn, announceNewDrops),",
        new="\t\tInterval: refreshInterval,",
        killer="TestTheRunningPodSAYSSoWhenItsControlJournalGoesBad",
        why="the WIRING of the only signal a running pod gives about a control journal "
        "that has stopped loading. `Cache.Run` discarded every refresh error and "
        "`Cache.Staleness()` has no caller outside the tests, so before this field existed "
        "the sequence was: an append hits ENOSPC and leaves a torn last line, the pod keeps "
        "serving last-known-good (correctly), nothing is said anywhere, and the next restart "
        "is a permanent refusal to start. ⚠ Its sibling "
        "`TestABrokenControlJournalIsSaidONCEAndItsRecoverySaidONCE` calls "
        "`journalRefreshReporter` DIRECTLY and stays GREEN under this mutant — the same "
        "'a capability in a function nobody routes to' shape as the row below, which is "
        "why this row exists rather than trusting that one.",
    ),
    # ---- the drop announcement: three render sites, and the one that runs on a TIMER ---
    #
    # 🔴 EACH SITE FAILS ALONE, WHICH IS WHY THERE ARE FIVE ROWS AND NOT ONE. `control`
    # decides to DROP a record and can only carry it as data — the package holds no logger
    # by design — so "an operator is told" is a property of a wire neither side's own tests
    # can see, at each of the places a program reads a model. An audit round measured it:
    # stubbing out `cmd/cairn-server/createuser.go`'s render and `cmd/cairn-ui/main.go`'s
    # left `go test ./cmd/... ./internal/control/...` fully green, because the only guard
    # targeted the THIRD site.
    Mutant(
        name="the-pods-startup-never-announces-its-dropped-records",
        path="cmd/cairn-server/createuser.go",
        old="\tannounceNewDrops := newDropAnnouncer(journal, cache.Model, warn)\n\tannounceNewDrops()",
        new="\tannounceNewDrops := newDropAnnouncer(journal, cache.Model, warn)",
        killer="TestADropAlreadyInTheJournalIsAnnouncedWhenTheAuTHORITYOPENS",
        why="the render deleted at the site a pod reaches FIRST. A pod started over a "
        "journal that already holds a bad line comes up serving an authority quietly "
        "shorter than the file, and the person who cannot sign in is the signal.",
    ),
    Mutant(
        name="the-pods-refresh-never-announces-a-NEW-drop",
        path="cmd/cairn-server/createuser.go",
        old="\t\tOnRefresh: journalRefreshReporter(journal, warn, announceNewDrops),",
        new="\t\tOnRefresh: journalRefreshReporter(journal, warn, nil),",
        killer="TestADropAppearingWhileThePodIsRunningReachesTheOperator",
        # 🔴 THIS IS THE STATE THE PR SHIPPED, AND IT IS WHY THE ROW IS NOT A HYPOTHETICAL.
        # `warnAboutDroppedRecords` ran once, before `Cache.Run` started, and `OnRefresh`
        # takes an `error` and cannot see a model — so a `credential-issued` line appended
        # to a LIVE pod's journal with a raw token in `token_hash` was dropped with an
        # EMPTY operator stream. The round that introduced it had traded a loud outage for
        # a silent no-op and reported only the first half.
        why="the announcer unwired from the only per-refresh hook. The startup row above "
        "stays green under it, which is exactly how the silence shipped.",
    ),
    Mutant(
        name="a-standing-drop-is-re-announced-on-every-refresh",
        path="cmd/cairn-server/createuser.go",
        old="\t\t\tif _, said := announced[line]; said {\n\t\t\t\tcontinue\n\t\t\t}",
        new="\t\t\tif false {\n\t\t\t\tcontinue\n\t\t\t}",
        killer="TestADropIsAnnouncedOnceNoMatterHowOftenTheJournalIsREAD",
        extra_killers=("TestADropAppearingWhileThePodIsRunningReachesTheOperator",),
        why="the ledger dropped, which reads as a simplification and is the OTHER way to "
        "lose this warning. The refresh is 30 s, so one bad journal line becomes ~2,880 "
        "identical lines a day — a stream an operator filters, which is the same outcome "
        "as never warning. The same arithmetic `journalRefreshReporter`'s edge detector "
        "exists for, reintroduced one hook over.",
    ),
    Mutant(
        name="the-UIs-startup-never-renders-its-dropped-records",
        path="cmd/cairn-ui/main.go",
        old="\t\tfunc(line string) { fmt.Fprintln(os.Stderr, line) })\n\tannounceNewDrops()",
        new="\t\tfunc(line string) { fmt.Fprintln(os.Stderr, line) })",
        killer="TestTheBINARYSaysSoWhenItsJournalLostARecordAtReplay",
        # 🔴 THE RENDER IS INSIDE `main`, SO ITS GUARD HAS TO RUN THE BINARY. Every other
        # test in that package drives `openAuthority` and the refusal predicate directly
        # and stays green with this applied — the "a capability nobody routes to" shape.
        why="the render deleted on the surface whose own comment says it feels a dropped "
        "credential FIRST: the startup refusal may fire BECAUSE the only credential in the "
        "journal was the dropped one, and without this line that reads as an empty journal.",
    ),
    Mutant(
        name="the-UIs-refresh-never-announces-a-NEW-drop",
        path="cmd/cairn-ui/main.go",
        old="\t\t\t\t// read — and that is the case the startup render above cannot see.\n\t\t\t\tannounceNewDrops()",
        new="\t\t\t\t// read — and that is the case the startup render above cannot see.",
        killer="TestTheRUNNINGBinarySaysSoWhenARecordIsDroppedAfterStartup",
        why="the same unwiring as the pod's refresh row, on the surface where a dropped "
        "credential is a person failing to sign in RIGHT NOW. Its killer is the only test "
        "in this repository that runs `cairn-ui` as a long-lived process.",
    ),
    Mutant(
        name="main-never-dispatches-create-user",
        path="cmd/cairn-server/main.go",
        old="\tif *create.enabled {\n\t\t// `*store` is passed",
        new="\tif false {\n\t\t// `*store` is passed",
        killer="TestTheBinaryActuallyDispatchesCreateUser",
        why="the mode unreachable while every in-process test of it stays green, because "
        "those call `runCreateUser` directly. The same shape as the refresh-loop row: a "
        "capability that exists in a function nobody routes to.",
    ),

    # ---- the browser share flow: who can see a scope, and who may change that -------
    #
    # ---- the entry page's RAW VIEW: its escaping, and its authority ORDERING -------
    #
    # 🔴 THESE TWO WERE RUN BY HAND AND RECORDED IN PROSE, WHICH IS THE EXACT SHAPE THE
    # `./internal/ui/` ENTRY IN `PKGS` EXISTS BECAUSE OF. Worse than that precedent, in
    # fact: the prose record went stale inside ONE commit — it cited a `file:line` and an
    # assertion string that the very next commit's test-fold had already moved, so a reader
    # checking the evidence would have found nothing there. Prose cannot be re-run; these
    # rows can.
    Mutant(
        name="raw-view-renders-the-file-unescaped",
        path="internal/ui/render.go",
        old="h.Code(g.Text(e.Raw))",
        new="h.Code(g.Raw(e.Raw))",
        killer="TestHostileEntryTextIsEscapedOnEveryBrowsePage",
        # 🔴 THE AST BAN KILLS THIS TOO, WHICH IS WHY IT IS DECLARED RATHER THAN LEFT TO
        # LOOK LIKE A CLEAN SINGLE-GUARD KILL. A mutant killed by a different guard's error
        # says nothing about the guard you think you are testing, so the behavioural row and
        # the structural ban are both named: the day either stops killing it, somebody has
        # to look.
        extra_killers=(
            "TestNoRawNodeConstructorAppearsInTheUIPackage",
            "TestAHostileFileReachesTheRawViewAsTextThroughTheREALPIPELINE",
        ),
        why="the raw view emits the WHOLE entry file through one node, so it is the widest "
        "attacker-authored sink on this surface — and `g.Raw` is a one-token edit that "
        "renders it as markup. Every other sink here is something a parser accepted first.",
    ),
    Mutant(
        name="raw-view-refuses-differently",
        path="internal/ui/server.go",
        old="""	scope, found := pickScope(scopes, wanted)
	if !found {
		writePlain(w, http.StatusNotFound, browseRefusal)
		return
	}
	entry, found := pickEntry(scope, ref)""",
        new="""	scope, found := pickScope(scopes, wanted)
	if !found {
		if q.Get(QueryView) == ViewRaw {
			writePlain(w, http.StatusForbidden, browseRefusal)
			return
		}
		writePlain(w, http.StatusNotFound, browseRefusal)
		return
	}
	entry, found := pickEntry(scope, ref)""",
        killer="TestTheBrowsePagesRefuseAnotherPrincipalsScopeWithTheSameBytesAsAnAbsentOne",
        why="a second way to reach an entry's bytes is a second place the refusal can drift, "
        "and a raw view that answers its own status is an existence oracle over every scope "
        "in the deployment — the shape `browseRefusal` exists to refuse.",
    ),

    # 🔴 THESE EIGHT CAME FROM A SECOND BATTERY NOTHING RAN. See the `./internal/ui/`
    # entry in PKGS for what that cost and why they live here now.
    Mutant(
        name="ui-audience-reads-grant-rows-instead-of-Resolve",
        path="internal/ui/sharing.go",
        old="\tfor _, p := range principals(m) {\n"
        "\t\tauth := control.Resolve(m, p)\n"
        "\t\tverbs := auth.VerbsOn(scope)",
        # The defect stated as ONE edit: narrow the audience to the subjects of a live
        # grant on this scope. That is exactly what a grant-table listing computes, and
        # it is what the operator's instruction for this feature forbade. Expressed
        # in-place rather than by appending a helper, because a mutant that has to add a
        # function is a mutant that can fail to COMPILE — which dies at the build and
        # proves nothing about any guard.
        new="\tsubjects := map[control.ID]struct{}{}\n"
        "\tfor _, g := range m.Grants {\n"
        "\t\tif g.Live() && g.ObjectKind == control.ObjectScope && g.ObjectID == scope {\n"
        "\t\t\tsubjects[g.SubjectID] = struct{}{}\n"
        "\t\t}\n"
        "\t}\n"
        "\tfor _, p := range principals(m) {\n"
        "\t\tif _, granted := subjects[p.ID]; !granted {\n"
        "\t\t\tcontinue\n"
        "\t\t}\n"
        "\t\tauth := control.Resolve(m, p)\n"
        "\t\tverbs := auth.VerbsOn(scope)",
        killer="TestTheAudienceIsComputedFromResolveNotFromGrantRows",
        why="the obvious implementation of 'who has access to this' — read the sharing table. "
        "It under-reports every project member, silently, and in the direction that tells "
        "somebody their notes are more private than they are.",
    ),
    Mutant(
        name="ui-replica-notice-drops-a-clause",
        path="internal/ui/render.go",
        old='\t"revoking a share stops future syncs: it does not recall entries already copied onto " +\n'
        '\t"somebody\'s machine."',
        new='\t"revoking a share stops future syncs."',
        killer="TestTheReplicaHonestyNoticeIsPinnedWhole",
        why="a reword that tightens the prose and drops the clause making the product "
        "sound weakest — the exact edit a whole-string pin exists to refuse.",
    ),
    Mutant(
        name="ui-verbs-read-single-value",
        path="internal/ui/sharehandlers.go",
        old="\tfor _, raw := range r.PostForm[FieldVerb] {\n"
        "\t\tverbs = append(verbs, control.Verb(raw))\n"
        "\t}",
        new='\tif raw := r.PostFormValue(FieldVerb); raw != "" {\n'
        "\t\tverbs = append(verbs, control.Verb(raw))\n"
        "\t}",
        killer="TestEveryTickedVerbReachesTheGrant",
        why="`PostFormValue` is the reflex reach for a form field, and it returns the "
        "FIRST value only — so a checkbox group silently records a read-only grant for "
        "somebody who ticked read AND write.",
    ),
    Mutant(
        name="ui-share-subject-accepted-from-the-form",
        path="internal/ui/sharehandlers.go",
        old="\tsubject, ok := pick(candidates, control.ID(r.PostFormValue(FieldSubject)))\n"
        "\tif !ok {\n"
        "\t\twritePlain(w, http.StatusForbidden, shareWriteRefusal)\n"
        "\t\treturn\n"
        "\t}",
        new="\tsubject := Subject{Kind: control.KindUser, ID: control.ID(r.PostFormValue(FieldSubject))}\n"
        "\t_ = candidates",
        killer="TestTheSubjectIsValidatedAgainstCandidatesRatherThanAcceptedFromTheForm",
        why="trusting the `select` the page rendered. It constrains a browser and nothing "
        "else, so admin on one scope would let a caller widen it to any principal whose "
        "id they can name.",
    ),
    Mutant(
        name="ui-unshare-skips-the-objects-authority-check",
        path="internal/ui/sharing.go",
        # Re-derived when `Unshare`'s check became `mayRevokeGrant`, by object kind (#214, O-b):
        # the SCOPE arm is where this row's question lives now.
        old="\tcase control.ObjectScope:\n\t\treturn auth.Allows(g.ObjectID, control.VerbAdmin)",
        new="\tcase control.ObjectScope:\n\t\treturn auth.Allows(g.ObjectID, control.VerbAdmin) || true",
        killer="TestARevokeIsAuthorisedFromTheGrantRatherThanFromTheForm",
        why="the revocation already found the grant, so re-checking authority over its "
        "object reads like belt-and-braces — it is the only thing stopping admin on scope "
        "A from revoking a grant on scope B.",
    ),
    Mutant(
        name="ui-share-page-skips-its-authority-check",
        path="internal/ui/sharehandlers.go",
        old="\tif !id.Auth.Allows(scope, control.VerbAdmin) {\n"
        "\t\twritePlain(w, http.StatusNotFound, scopeRefusal)\n"
        "\t\treturn ShareView{}, false\n"
        "\t}",
        new="\t_ = scopeRefusal",
        killer="TestTheSharePageRefusesAScopeThisCallerCannotAdminister",
        why="the page only READS, so gating it looks like caution — it is what stops the "
        "audience of any scope being served to anybody who can name its id.",
    ),
    Mutant(
        name="ui-page-never-reports-a-read-only-authority",
        path="internal/ui/sharehandlers.go",
        old="\t\tReadOnly: !s.sharing.Writable(),",
        new="\t\tReadOnly: false,",
        killer="TestAReadOnlyDeploymentSaysSoOnThePageRatherThanAtTheClick",
        why="the banner looks like decoration. Without it a deployment that cannot record "
        "a share serves the whole flow and refuses at the click, which reads to an "
        "operator as a permission problem they do not have.",
    ),
    Mutant(
        name="ui-share-write-authority-check-removed-in-the-handler",
        path="internal/ui/sharehandlers.go",
        old="\tscope := control.ID(r.PostFormValue(FieldScope))\n"
        "\tif !id.Auth.Allows(scope, control.VerbAdmin) {\n"
        "\t\twritePlain(w, http.StatusForbidden, shareWriteRefusal)\n"
        "\t\treturn\n"
        "\t}",
        new="\tscope := control.ID(r.PostFormValue(FieldScope))",
        killer="TestTheSharePageRefusesAScopeThisCallerCannotAdminister",
        why="deleting the check that looks redundant because the layer below it checks "
        "too — the ordinary way a defence-in-depth pair quietly becomes a single point.",
        # 🔴 THIS ROW WAS LABELLED `equivalent=True` AND THE LABEL WAS MEASURED FALSE —
        # the retracted reason is kept here because an EQUIVALENT label is precisely what
        # stops anybody writing the test that kills the mutant, so the record of one being
        # wrong is worth more than the tidy row. It read: "Removing EITHER alone is
        # observably identical: the other still answers 403 with the same body." The
        # discriminator it missed is a request with NO verb field: unmutated the handler's
        # authority check refuses at **403** before the form is validated; with this site
        # removed the request reaches the verb validation and answers **400**, telling a
        # caller who may not touch the scope that their input was the problem. So the two
        # sites are NOT redundant — the handler's runs first and refuses without
        # disclosing. `TestTheSharePageRefusesAScopeThisCallerCannotAdminister` gained that
        # exact case and this row is an ordinary killable one.
        #
        # ⚠ THE GENERAL SHAPE, WHICH IS WHY THIS IS RECORDED RATHER THAN DELETED: an
        # `equivalent` label is a CLAIM about every observable, and it was made here by
        # comparing the one observable the author had in mind. A label that reads as
        # coverage while providing none is this repository's signature defect, and it
        # appeared inside the battery built to refuse it.
    ),
    # ---- the browser surface's startup refusals ------------------------------------
    Mutant(
        name="ui-startup-admits-a-revoked-credential",
        path="cmd/cairn-ui/main.go",
        old="\t\tif !c.Live() {\n\t\t\tcontinue\n\t\t}\n",
        new="",
        killer="TestAJournalWhoseCredentialsAreALLREVOKEDIsRefused",
        why="counting credentials without asking whether they are live — the reading that "
        "looks complete because the records are all there. A journal mid-rotation has its "
        "credential rows and none of them live, and the surface then starts and can "
        "authenticate nobody. This clause SURVIVED a sweep until the test that names it was "
        "written, which is why the row is here rather than in a commit message.",
    ),
    Mutant(
        name="ui-startup-admits-an-authority-with-no-credential",
        path="cmd/cairn-ui/main.go",
        old="\tif usable > 0 {\n\t\treturn nil\n\t}",
        new="\tif usable >= 0 {\n\t\treturn nil\n\t}",
        killer="TestAJournalWithUsersAndNoCredentialIsRefused",
        extra_killers=("TestAJournalWhoseCredentialsAreALLREVOKEDIsRefused",),
        why="the off-by-one that turns the whole refusal into a no-op while reading as a "
        "bounds check. `cairn-server -create-user` produces exactly this state — a user and "
        "no credential — so the mutant is the guard's own documented failure case.",
    ),
    # ---- the THIRD startup refusal: `controlJournalDefault`'s blank policy ----------
    #
    # 🔴 THE TWO ROWS ABOVE COVER ONE REFUSAL (`refuseAnAuthorityNobodyCanSignInTo`), AND
    # THE THREE BELOW WERE THE LEDGER'S GAP RATHER THAN AN UNCOVERED GUARD. The unit tests
    # in `cmd/cairn-ui/main_test.go` already kill every mutation below — measured, not
    # assumed — so what was missing was the ROW, not the test: the evidence that this
    # refusal can go red existed only as a hand-run sweep recorded in a commit message,
    # which is the exact form the `./cmd/cairn-ui/` entry in `PKGS` exists because of. A
    # battery that runs in CI, attributes by killer and is count-pinned says it every push;
    # a commit message says it once.
    Mutant(
        name="ui-startup-reads-a-blank-journal-line-as-unset",
        path="cmd/cairn-ui/main.go",
        # The CONDITION, not the refusal it guards: the `fmt.Errorf` below stays in the
        # tree, so `raw` stays used and this is the narrowest expression that can be wrong.
        # 🔴 DISAMBIGUATED BY THE REFUSAL TEXT BELOW IT, BECAUSE THIS ROW WENT `HARNESS ERROR`
        # THE DAY A SECOND BLANK POLICY LANDED. 28(c) added `databaseDSNDefault`, whose
        # condition is spelled identically, so `if identity.ValueReducesToNothing(raw) {`
        # occurs TWICE in this file and the count assertion refused the row — correctly, and
        # loudly, which is the whole reason it asserts a count rather than that a replacement
        # happened. ⚠ The lesson is not "disambiguate your new row": it is that ADDING a
        # second copy of a shape breaks the EXISTING row that matched the first, and only the
        # battery can see it. Re-run the battery after duplicating any guarded shape.
        old='\tif identity.ValueReducesToNothing(raw) {\n\t\treturn "", fmt.Errorf(\n'
        '\t\t\t"%s=%q reduces to nothing, so this surface would read it as UNSET and fall back to the "+',
        new='\tif false {\n\t\treturn "", fmt.Errorf(\n'
        '\t\t\t"%s=%q reduces to nothing, so this surface would read it as UNSET and fall back to the "+',
        killer="TestAWhitespaceControlJournalLineIsRefusedRatherThanReadAsUnset",
        extra_killers=("TestTheProcessExitsOnAWhitespaceControlJournalLine",),
        why="deleting the blank policy as belt-and-braces, on the reading that "
        "`openAuthority`'s `Stat` refuses a whitespace path anyway. It does — with `stat "
        "'   ': no such file or directory`, which sends an operator to check a mount for a "
        "path made of spaces instead of to the manifest line they wrote. The process still "
        "exits 78, so only a test that reads WHICH refusal spoke can see this.",
    ),
    Mutant(
        name="ui-startup-refuses-an-explicitly-empty-journal-line",
        path="cmd/cairn-ui/main.go",
        # Disambiguated by the `get(...)` line above it — same cause as the row above: 28(c)'s
        # `databaseDSNDefault` opens with the identical three lines.
        old='\traw := get(EnvUIControlJournal)\n\tif raw == "" {\n\t\treturn "", nil\n\t}',
        new='\traw := get(EnvUIControlJournal)\n\tif false {\n\t\treturn "", nil\n\t}',
        killer="TestAWhitespaceControlJournalLineIsRefusedRatherThanReadAsUnset",
        why="deleting the early return as redundant — `ValueReducesToNothing(\"\")` is true, "
        "so the refusal below appears to cover it. The direction is the dangerous one for a "
        "DEPLOYMENT rather than for authz: a manifest that emits every variable with an "
        "empty default now stops the surface from starting at all, which is the shape "
        "`controlJournalDefault`'s own comment declares must remain 'not set'.",
    ),
    Mutant(
        name="ui-startup-does-not-act-on-the-journal-refusal",
        path="cmd/cairn-ui/main.go",
        old='\tif journalErr != nil {\n\t\tfmt.Fprintln(os.Stderr, "cairn-ui: "+journalErr.Error())\n'
        "\t\tos.Exit(exitConfig)\n\t}",
        new="\t_ = journalErr",
        killer="TestTheProcessExitsOnAWhitespaceControlJournalLine",
        why="the wiring, not the predicate: a refusal that is COMPUTED and then not acted "
        "on. Every in-process test of `controlJournalDefault` stays green because the "
        "function still returns its error — the observable is a process that BINDS A "
        "LISTENER, which is why the one case that re-execs this binary as `cairn-ui` is the "
        "only thing that can see it. It is also why this row costs ~30s: the killing test "
        "kills it on its DEADLINE, the mutant having made the child serve rather than exit.",
    ),
    # ---- 28(c): the database wiring, and the FOUR refusals a tagless battery can see -
    #
    # 🔴 WHAT IS NOT HERE AND WHY, BECAUSE THE ABSENCE IS THE OPEN QUESTION THE HANDOFF
    # RECORDS RATHER THAN AN OVERSIGHT. The DSN-present branch has three more mutants,
    # all measured KILLED by hand against a real PostgreSQL 18.6: the invite store built
    # and never attached (`inviting` left nil), the session table left on the file store
    # while a database is configured, and the ignored-`-session-file` NOTE suppressed.
    # Their killer is `TestWithADatabaseTheSurfaceMovesItsStateThereAndHoldsInvitations`,
    # which is behind `//go:build pgtest` — so THIS battery, which runs `go test` with no
    # tag, would never compile it and would score all three SURVIVED. A row whose killer
    # cannot run is worse than no row: it reports a coverage gap that does not exist and
    # sends the next reader to write a test that is already written. So the answer to
    # "can a build-tagged tier be mutated by a battery running `go test` without the
    # tag" is NO, and the tier's own runner (`tests/pgtest/run.sh`) is where that
    # evidence has to live.
    #
    # ⚠ AND A FOURTH MUTANT IS ABSENT BECAUSE IT DOES NOT COMPILE, WHICH IS A STRONGER
    # OUTCOME THAN A ROW. Deleting `Inviting: inviting` from the `ui.Config` literal —
    # the most likely way to break this wiring, and silent before 28(c) hoisted the
    # config into a named value — now leaves `inviting` with no reader and `go build`
    # refuses it with `declared and not used`. Measured, not assumed.
    Mutant(
        name="ui-startup-reads-a-blank-dsn-line-as-unset",
        path="cmd/cairn-ui/main.go",
        # The CONDITION only, and disambiguated from `controlJournalDefault`'s identical
        # line by the refusal text below it: `identity.ValueReducesToNothing(raw)` appears
        # TWICE in this file, and a row that matched both would mutate a guard it does not
        # name. The `fmt.Errorf` stays, so `raw` stays used.
        old="\tif identity.ValueReducesToNothing(raw) {\n\t\treturn \"\", fmt.Errorf(\n"
        '\t\t\t"%s=%q reduces to nothing, so this surface would read it as UNSET: the session table would "+',
        new="\tif false {\n\t\treturn \"\", fmt.Errorf(\n"
        '\t\t\t"%s=%q reduces to nothing, so this surface would read it as UNSET: the session table would "+',
        killer="TestAWhitespaceDatabaseLineIsRefusedRatherThanReadAsUnset",
        extra_killers=("TestTheProcessRefusesAnUnreachableDatabaseBeforeTheListener",),
        why="deleting the blank policy as belt-and-braces, on the reading that a bad DSN "
        "fails at `pgstore.Open` anyway. It does not: a WHITESPACE value resolves to the "
        "empty string, which is 'no database', so the surface comes up with the session "
        "table back on a local file and every /invite page telling its reader the "
        "deployment holds no invitation store — while /healthz answers 200. The operator "
        "wrote a line and this program discarded it.",
    ),
    Mutant(
        name="ui-startup-refuses-an-explicitly-empty-dsn-line",
        path="cmd/cairn-ui/main.go",
        old='\traw := get(EnvUIDatabase)\n\tif raw == "" {\n\t\treturn "", nil\n\t}',
        new='\traw := get(EnvUIDatabase)\n\tif false {\n\t\treturn "", nil\n\t}',
        killer="TestAWhitespaceDatabaseLineIsRefusedRatherThanReadAsUnset",
        why="deleting the early return as redundant — `ValueReducesToNothing(\"\")` is true, "
        "so the refusal below appears to cover it. The direction is the dangerous one for a "
        "DEPLOYMENT: a manifest that emits every variable with an empty default would stop "
        "the surface from starting at all, and 'no database' is the configuration that "
        "exists today.",
    ),
    Mutant(
        name="ui-startup-does-not-act-on-the-dsn-refusal",
        path="cmd/cairn-ui/main.go",
        old='\tif dsnErr != nil {\n\t\tfmt.Fprintln(os.Stderr, "cairn-ui: "+dsnErr.Error())\n'
        "\t\tos.Exit(exitConfig)\n\t}",
        new="\t_ = dsnErr",
        killer="TestTheProcessRefusesAnUnreachableDatabaseBeforeTheListener",
        why="the wiring, not the predicate: a refusal that is COMPUTED and then not acted "
        "on. Every in-process test of `databaseDSNDefault` stays green because the function "
        "still returns its error — the observable is a process that BINDS A LISTENER. It is "
        "the same shape as the journal row above and it costs the same ~60s, because the "
        "killing test kills it on its DEADLINE rather than on an exit code.",
    ),
    Mutant(
        name="ui-startup-serves-an-unreachable-database",
        path="cmd/cairn-ui/main.go",
        old="\t\tpgDB, err = openDatabase(*dbDSN)\n\t\tif err != nil {",
        new="\t\tpgDB, err = openDatabase(*dbDSN)\n\t\tif false {",
        killer="TestTheProcessRefusesAnUnreachableDatabaseBeforeTheListener",
        why="the whole of 28(c) in one clause: connecting, ignoring the result, and serving. "
        "`sql.Open` validates nothing and `pgstore.Open` is what pings — so with this "
        "removed a wrong host, a wrong password or a missing database all produce a pod "
        "that passes its health check, renders every page, and fails every sign-in and "
        "every /invite one request at a time. That is the exact failure this program's "
        "startup refusals exist against, and nothing else in the tree would notice.",
    ),
    # ---- ROUND 1's FINDINGS: the two paths nothing was pointed at --------------------
    #
    # 🔴 BOTH OF THESE WERE FOUND BY AN AUDIT AND NEITHER WAS FOUND BY A GATE, WHICH IS WHY
    # THEY ARE ROWS RATHER THAN A PARAGRAPH IN A COMMIT MESSAGE. The callback's redemption
    # behaviour had NO test at all — `oauth_test.go` carried zero references to an invite —
    # while two places in the tree claimed it was covered. A false coverage claim is what
    # kept anybody from looking, and the defect it hid was total.
    Mutant(
        name="ui-known-user-invitation-is-silently-discarded",
        path="internal/ui/oauth.go",
        # The CONDITION, not the whole block: `red` and `rerr` stay declared, so this is the
        # narrowest expression that can be wrong rather than a deletion that takes its
        # enclosing scope with it.
        old="\tif inviteToken != \"\" && s.inviting != nil {\n\t\tred, rerr := s.inviting.RedeemFor(",
        new="\tif false {\n\t\tred, rerr := s.inviting.RedeemFor(",
        killer="TestAKnownUserCarryingAnInvitationRedeemsItOnTheCallback",
        why="this IS the shipped defect, restored. The provisioning arm is guarded on the "
        "exchange having FAILED with `UnprovisionedSubject`; a user the control plane already "
        "holds exchanges SUCCESSFULLY, so without this branch their invitation is read by "
        "nothing — they are signed in, it stays `open`, and no page or log says so. The "
        "handler's own comment sent the reader to an 'authenticated redeem route' that is not "
        "in `DeclaredRoutes()` and was never built.",
    ),
    Mutant(
        name="ui-redeemfor-overwrites-an-existing-membership",
        path="internal/ui/inviting.go",
        old="\tif _, held := c.Authority.Model().RoleIn(inv.ProjectID, principal.ID); held {\n"
        "\t\treturn Redemption{}, ErrAlreadyAMember\n\t}",
        new="\tif false {\n\t\treturn Redemption{}, ErrAlreadyAMember\n\t}",
        killer="TestRedeemForRefusesSomebodyTheProjectAlreadyHolds",
        why="deleting the already-a-member refusal as a UX nicety. It is not one: `Redeem` "
        "writes a bare `member-set` and `Model.apply` calls `setMembership` UNCONDITIONALLY — "
        "it never consults `refuseOrphaning`, which only the operator's writers call "
        "(`control.SetMember`, `control.RemoveMember`). So an `admin` could mint a `member` invitation into their own "
        "project, have the sole OWNER redeem it, and leave the project with no owner at all.",
    ),
    # ---- ROUND 2's FINDING: the guard above shipped on ONE of the two writers ----------
    #
    # 🔴 THE ROW ABOVE AND THIS ONE ARE A PAIR, AND THE PAIR IS THE POINT. Round 1 closed the
    # role-overwrite on `RedeemFor`; a round-2 DELTA audit found `Redeem` — the OTHER
    # serving-path redemption, the one the callback's provisioning arm reaches — still
    # writing `member-set` for an already-held user with no refusal at all. One rule, two
    # entry points, guarded at one of them: the shape this repository's own rules call a
    # predicate open-coded at N sites and wrong at N-1 of them. The fix DELEGATES to
    # `RedeemFor` rather than copying the check, so there is again ONE implementation of the
    # existing-user path; this row is what refuses a future edit that re-opens the second.
    Mutant(
        name="ui-redeem-stops-delegating-an-already-known-user",
        path="internal/ui/inviting.go",
        # 🔴 `held && false`, NOT `false` — AND THAT SPELLING WAS MEASURED, NOT CHOSEN.
        # Written as `if false {` this row scored `HARNESS ERROR` rather than a kill: the
        # mutant died at the BUILD with `declared and not used: held`, so the guard was never
        # executed and nothing was learned about it. Keeping `held` referenced makes the
        # predicate unsatisfiable while the program still compiles, which is the difference
        # between a mutant that scores and one that only looks like it does.
        old="\tif held {\n\t\tprincipal, ok := model.PrincipalFor(control.KindUser, user.ID)",
        new="\tif held && false {\n\t\tprincipal, ok := model.PrincipalFor(control.KindUser, user.ID)",
        killer="TestRedeemAlsoRefusesSomebodyTheProjectAlreadyHolds",
        why="the delegation dropped, which restores exactly the tree round 2 audited: "
        "`Redeem` computes `held` and uses it only to decide whether to MINT a user id, then "
        "writes `member-set` regardless. Reachable in production as a concurrent "
        "double-callback — one account, two open invitations into one project, two tabs — "
        "where both exchanges fail with `UnprovisionedSubject`, both take the provisioning "
        "arm, and the second overwrites the role the first conferred. `Model.apply` never "
        "consults `refuseOrphaning`, so a project whose sole owner arrived that way is left "
        "ownerless.",
    ),
    # ---- Phase G: the invite flow's HTTP surface ----------------------------------
    #
    # 🔴 THESE ROWS EXIST HERE RATHER THAN IN A SCRATCHPAD SCRIPT BECAUSE OF THE ENTRY
    # `./internal/ui/` IN `PKGS` ABOVE. That entry records that the share flow arrived with a
    # battery of its OWN which NO gate ever ran, and that folding its rows in here bought the
    # CI step, the copytree isolation and the count pin at once. The invite flow's sweep was
    # first written as exactly that scratchpad script, scored 24 killed / 0 survived, and
    # would have shipped as a number in a commit message — the same shape, one flow along.
    Mutant(
        name="role-escalation-check-dropped-from-canconfer",
        path="internal/control/invite_authority.go",
        old="return r == RoleOwner || other != RoleOwner",
        new="return true",
        killer="TestCanConferRefusesEveryEscalationAndPermitsEveryLegitimateHandOff",
        why="the rule written as 'anybody who can manage members can confer anything' — the "
        "obvious first draft, and it lets an admin mint an owner invitation and redeem it "
        "themselves, leaving a journal that reads as an ordinary join.",
    ),
    Mutant(
        name="canconfer-stops-asking-who-may-manage",
        path="internal/control/invite_authority.go",
        old="if !r.CanManageMembers() || !other.Valid() {",
        new="if !other.Valid() {",
        killer="TestCanConferRefusesEveryEscalationAndPermitsEveryLegitimateHandOff",
        why="a caller who checks only `CanConfer` would then be a hole: an ordinary member "
        "could confer `member`. The two predicates are deliberately nested so that one call "
        "site cannot satisfy the narrow one without the wide one.",
    ),
    Mutant(
        name="canconfer-stops-refusing-an-undefined-role",
        path="internal/control/invite_authority.go",
        old="if !r.CanManageMembers() || !other.Valid() {",
        new="if !r.CanManageMembers() {",
        killer="TestCanConferRefusesEveryEscalationAndPermitsEveryLegitimateHandOff",
        why="`resolve` treats a role absent from `roleVerbs` as the EMPTY verb set, so a role "
        "string from a newer build would be conferrable and would grant nothing — an "
        "invitation that succeeds and confers no access.",
    ),
    Mutant(
        name="allroles-loses-the-least-privileged-entry",
        path="internal/control/model.go",
        old="var AllRoles = []Role{RoleOwner, RoleAdmin, RoleMember}",
        new="var AllRoles = []Role{RoleOwner, RoleAdmin}",
        killer="TestAllRolesIsTheWholeRoleTable",
        why="the shape of a chooser list edited by hand. Every role the model grants stays "
        "grantable; what disappears is the ability to OFFER one, and the form would silently "
        "stop being able to express `member`.",
    ),
    Mutant(
        name="allroles-order-reverses",
        path="internal/control/model.go",
        old="var AllRoles = []Role{RoleOwner, RoleAdmin, RoleMember}",
        new="var AllRoles = []Role{RoleMember, RoleAdmin, RoleOwner}",
        killer="TestAllRolesIsTheWholeRoleTable",
        why="a tidy-up that looks cosmetic. `inviteForm`'s comment explains its explicit "
        "default BY REFERENCE to this order being descending, so flipping it makes that "
        "comment false while the form keeps working — a claim rotting with no symptom.",
    ),
    Mutant(
        name="namedproject-drops-the-callers-role",
        path="internal/control/invite_authority.go",
        old="out = append(out, NamedProject{ID: p.ID, Name: p.Name, HeldRole: ship.Role})",
        new="out = append(out, NamedProject{ID: p.ID, Name: p.Name})",
        killer="TestProjectsManagedByCarriesTheCallersOwnRole",
        extra_killers=("TestTheInviteIndexIsDrivenByTheREALModelsHeldRole",),
        why="THE MEASURED SEAM. This mutant SURVIVED a fully green suite before those two "
        "guards existed: one side tested the model against a hand-built Model, the other "
        "tested the renderer against a hand-built NamedProject, and neither built the "
        "combined state. (The field fed the invitation form's role chooser then; since the "
        "fold it feeds the index's 'you are <role>'.)",
    ),
    Mutant(
        name="namedproject-reports-a-constant-role",
        path="internal/control/invite_authority.go",
        old="out = append(out, NamedProject{ID: p.ID, Name: p.Name, HeldRole: ship.Role})",
        new="out = append(out, NamedProject{ID: p.ID, Name: p.Name, HeldRole: RoleOwner})",
        killer="TestProjectsManagedByCarriesTheCallersOwnRole",
        why="the direction a zero-check cannot see: a role that is present and WRONG, in the "
        "permissive direction — every admin is told they are an owner.",
    ),
    Mutant(
        name="ui-invite-narrowing-skipped-before-the-unnarrowed-read",
        path="internal/ui/invitehandlers.go",
        old="\tchosen, found := pickProject(view.Projects, project)\n\tif !found {\n\t\twritePlain(w, http.StatusNotFound, inviteRefusal)\n\t\treturn InviteView{}, false\n\t}",
        new="\tchosen, _ := pickProject(view.Projects, project)",
        killer="TestAProjectThatIsNotInvitableIsRefusedBEFORETheUnnarrowedRead",
        why="`Inviting.Outstanding` performs NO authority check and says so in its own doc — "
        "the narrowing is the only thing in front of it. Dropping the refusal turns the page "
        "into a listing of who is being invited where, for any project id a caller names.",
    ),
    Mutant(
        name="ui-mint-response-loses-no-store",
        path="internal/ui/team.go",
        # 🔴 RE-DERIVED FOUR TIMES, and the row keeps its name. Twice in S3 of the mobile plan (the
        # writers it named were folded into one), once in #214 (the mint moved onto the Team page),
        # and now with the invite fold: the invitation mint it bypassed is gone, so it bypasses the
        # one writer at the ONE mint left — the team link's, inside `renderTeamAfterMint`.
        old="\tset(&view)\n\ts.render(w, TeamPage(view))",
        new="\tset(&view)\n\tw.Header().Set(\"Content-Type\", \"text/html; charset=utf-8\")\n"
        "\t_ = TeamPage(view).Render(w)",
        killer="TestTheMintedTeamLinkIsRenderedOnceAsTextAndNeverLogged",
        extra_killers=("TestTheTeamLinkFormPassesEveryTickedTargetThrough",),
        why="rendering straight into the ResponseWriter to skip the buffer looks like an optimisation, "
        "and it skips the one writer that sets `no-store` — on the response whose BODY is a bearer "
        "capability. The ledger walk is GET-only and cannot see this POST.",
    ),
    Mutant(
        name="ui-minted-link-becomes-an-anchor",
        # Re-aimed with the invite fold: the invitation's minted section is gone; the team link's
        # (`mintedLinkSection`) renders the same line and is the one mint left.
        path="internal/ui/team.go",
        old='h.P(h.Class("invite-link"), g.Text(m.Link)),',
        new='h.P(h.Class("invite-link"), h.A(h.Href(m.Link), g.Text(m.Link))),',
        killer="TestTheMintedTeamLinkIsRenderedOnceAsTextAndNeverLogged",
        why="the single most natural improvement to make to this page — a URL rendered as "
        "text looks like an oversight. The link is a PATH with no origin, so an anchor "
        "resolves it against THIS page: one stray click, or one link-prefetcher, and the "
        "minter has redeemed the link on themselves.",
    ),
    Mutant(
        name="ui-mint-logs-the-token",
        # Re-aimed with the invite fold, at the one mint left.
        path="internal/ui/team.go",
        old='link.ExpiresAt.UTC().Format(time.RFC3339), link.Inviter)',
        new='link.ExpiresAt.UTC().Format(time.RFC3339), link.Inviter+" token="+control.ID(token))',
        killer="TestTheMintedTeamLinkIsRenderedOnceAsTextAndNeverLogged",
        why="one field added to a log line while debugging, and never taken out. This is the "
        "shape that put a 64-character secret into the control journal once already, and the "
        "value here can create a principal.",
    ),
    Mutant(
        name="ui-join-page-resolves-the-token",
        path="internal/ui/invitehandlers.go",
        old="\ts.render(w, JoinPage(token, s.providerArmed(), s.app))",
        new="\tif s.inviting != nil {\n\t\t_, _ = s.inviting.Outstanding(control.ID(token))\n\t}\n\ts.render(w, JoinPage(token, s.providerArmed(), s.app))",
        killer="TestTheJoinPageNeverConsultsTheInviteAuthority",
        why="the obvious way to make the page more helpful — look the invitation up so it can "
        "name the project. `GET /join` is dispatched BEFORE the authentication chain, so any "
        "resolution there is an oracle over which invitations exist, drivable at will.",
    ),
    Mutant(
        name="ui-start-row-reads-the-invite-from-the-url-query",
        path="internal/ui/oauth.go",
        # The narrowest expression that can be wrong: one method name on one read. Both
        # spellings compile, both return a string, and the difference is only WHICH half of
        # `r.Form` the value may come from — which is why nothing but a request carrying the
        # field in the query alone can tell them apart.
        old="\tcarried := flightInvite{token: r.PostFormValue(inviteTokenField)}",
        new="\tcarried := flightInvite{token: r.FormValue(inviteTokenField)}",
        killer="TestTheGitHubStartRowIgnoresAnInvitationTokenInTheQUERYString",
        why="this IS the shipped defect, restored: `FormValue` is the reflex reach for a form "
        "field and it reads the posted body UNION the URL query, so "
        "`POST /sign-in/github?invite=<token>` was accepted. The value is a bearer capability "
        "that can CREATE a principal, and a query parameter lands in browser history, in the "
        "referrer the next hop receives and in every access log en route — the leak "
        "`inviteTokenField` is declared body-only to prevent, under a comment promising the "
        "value 'never appears in a URL, a referrer or an access log'. `handleJoinPage` is "
        "the mirror image of the same distinction and was corrected for it separately, which "
        "is what makes the wrong spelling here a thing a reader walks past twice.",
    ),
    # ---- the team-link FRAGMENT (`join.go`, `join.js`): a reusable token never in a logged URL ----
    Mutant(
        name="ui-join-reusable-team-link-accepted-via-query",
        path="internal/ui/join.go",
        # The narrowest expression that can be wrong: the one branch that refuses.
        old="\tif reusable {\n",
        new="\tif false && reusable {\n",
        killer="TestAReusableTeamLinkInTheQUERYStringIsRefusedAndNotRedeemed",
        why="'already-sent links must keep working' read one step too wide. A REUSABLE link is open "
        "enrolment until it expires, and a `/join?invite=` token has already been written to every "
        "access log on the way — accepting it keeps the logged shape working, and therefore being "
        "sent, which is the leak the fragment exists to close.",
    ),
    Mutant(
        name="ui-start-row-drops-the-query-borne-mark",
        path="internal/ui/oauth.go",
        old="carried = flightInvite{token: legacy, viaQuery: true}",
        new="carried = flightInvite{token: legacy}",
        killer="TestAReusableTeamLinkInTheQUERYStringIsRefusedAndNotRedeemed",
        why="the tidy-up that treats the two fields as one value, since both carry 'the token'. The "
        "mark is the ONLY thing that remembers, by the callback, that a server's access log saw it.",
    ),
    Mutant(
        name="ui-query-borne-classification-fails-open",
        path="internal/ui/join.go",
        old="\t\t\terr, client)\n\t\treturn \"\"\n",
        new="\t\t\terr, client)\n\t\treturn carried.token\n",
        killer="TestAQueryBorneTokenThatCannotBeClassifiedIsNotRedeemed",
        why="'a lookup failure should not break sign-in' — it does not either way (a known user is "
        "signed in regardless); what failing open buys is redeeming a link this function could not "
        "rule out as reusable, exactly when the link store is misbehaving.",
    ),
    Mutant(
        name="ui-minted-link-travels-in-the-query",
        path="internal/ui/join.go",
        old='return JoinPath + "#" + url.Values',
        new='return JoinPath + "?" + url.Values',
        killer="TestNoMintedLinkCarriesItsTokenInAQueryString",
        why="one character, and the familiar one: every link on the web carries its parameters after "
        "`?`. The link still works — which is the problem: every gateway between the reader and this "
        "pod writes the token to disk.",
    ),
    Mutant(
        name="ui-join-script-not-allowlisted",
        path="internal/ui/script.go",
        old="\treturn []string{FilterScriptPath, PWAScriptPath, JoinScriptPath}\n",
        new="\treturn []string{FilterScriptPath, PWAScriptPath}\n",
        killer="TestTheFragmentJoinPageMovesTheTokenThroughTheScript",
        why="`JoinPage` emits the tag itself, so the page works without the list knowing — and "
        "`uiaudit`'s walk boots with no provider, where the join page renders no script at all.",
    ),
    Mutant(
        name="ui-join-page-offers-a-form-with-no-token",
        path="internal/ui/render.go",
        # Re-aimed with the fragment link: a bare `/join` is what every new link opens, so the
        # server renders the accept form inside a `hidden` block for `join.js` to reveal, and the
        # defect is that block rendered visible with its EMPTY field. The row keeps its name.
        old='h.Div(h.ID(joinAcceptID), g.Attr("hidden"), joinNote(), joinForm(inviteTokenField, "")),',
        new='h.Div(h.ID(joinAcceptID), joinNote(), joinForm(inviteTokenField, "")),',
        killer="TestTheJoinPageWithoutATokenSaysSoAndOffersNoForm",
        why="`hidden` on a wrapper reads as cosmetic, and the script reveals it anyway. With script "
        "off — or a fragment carrying nothing — the visible empty form opens a flight carrying no "
        "invitation and completes as an ORDINARY sign-in, so somebody who was invited ends up signed "
        "in as nobody — or refused — with nothing saying the link was at fault.",
    ),
    Mutant(
        name="ui-revoke-button-appears-on-a-spent-invitation",
        path="internal/ui/render.go",
        old='g.If(csrf != "" && row.State == "open", h.FormEl(',
        new='g.If(csrf != "", h.FormEl(',
        killer="TestTheRevokeButtonIsOfferedOnlyForAnOpenInvitation",
        why="the same two-part-condition simplification as the join form. Not a security "
        "hole — the store refuses a non-open revoke itself — but a control offered on every "
        "row that works on one of them, which teaches people to ignore the refusal.",
    ),
    Mutant(
        name="ui-invite-revoke-refusals-stop-being-uniform",
        path="internal/ui/invitehandlers.go",
        old="\tcase errors.Is(err, ErrNotInvitable), errors.Is(err, invite.ErrNotRedeemable):",
        new="\tcase errors.Is(err, ErrNotInvitable):",
        killer="TestTheInviteRevokeRefusalsAreUniform",
        why="replaces `ui-invite-write-refusals-collapse-into-one`, whose third arm (a role the "
        "caller could not confer) went with the invitation mint. What is left to get wrong is the "
        "opposite: dropping one of the two uniform arms, so an unknown digest answers 500 where a "
        "foreign one answers 403 — a revoke that says whether a digest exists.",
    ),
    # ---- THE INVITE FOLD (operator decision, option A) ------------------------------
    # Single-project invitations are no longer minted: `POST /invite` creates nothing and 303s to
    # the team-link form, which pre-ticks the project; invitations that already exist stay
    # listed, revocable and redeemable. Each row below is one way that comes undone.
    Mutant(
        name="ui-post-invite-mints-a-team-link-instead",
        path="internal/ui/invitehandlers.go",
        old="func (s *Server) handleInvite(w http.ResponseWriter, r *http.Request, _ identity.Identity) {\n",
        new="func (s *Server) handleInvite(w http.ResponseWriter, r *http.Request, id identity.Identity) {\n"
        "\tif s.teamLinks != nil && r.ParseForm() == nil {\n"
        "\t\t_, _, _ = s.teamLinks.Mint(r.Context(), membershipActor(id), []invite.Target{{Kind: invite.TargetProject, "
        "ID: control.ID(r.PostFormValue(FieldProject))}}, invite.LinkMember, 0, false)\n\t}\n",
        killer="TestPostInviteNoLongerMintsAnInvitation",
        why="the helpful reading of 'fold': translate the old request into the new capability instead of "
        "sending the person to the form. It creates a capability nobody asked for, at a role this handler "
        "chose, that nothing renders — so it can never be used and sits open until it expires.",
    ),
    Mutant(
        name="ui-post-invite-redirect-drops-the-project",
        path="internal/ui/invitehandlers.go",
        old="\t\tq.Set(QueryProject, project)\n\t}\n\tq.Set(QueryOutcome, inviteOutcomeFolded)",
        new="\t\t_ = project\n\t}\n\tq.Set(QueryOutcome, inviteOutcomeFolded)",
        killer="TestPostInviteNoLongerMintsAnInvitation",
        extra_killers=("TestPostInviteCarriesOnlyAProjectItWasGivenIntoItsRedirect",),
        why="a redirect that carries nothing caller-chosen reads as the safer one. It lands the person on "
        "a form with nothing ticked, having asked to invite somebody to ONE project.",
    ),
    Mutant(
        name="ui-post-invite-lands-on-the-invitation-list",
        path="internal/ui/invitehandlers.go",
        old="\thttp.Redirect(w, r, teamHref(q.Encode(), teamLinksAnchor), http.StatusSeeOther)",
        new="\thttp.Redirect(w, r, teamHref(q.Encode(), teamInviteAnchor), http.StatusSeeOther)",
        killer="TestPostInviteNoLongerMintsAnInvitation",
        why="`handleInviteRevoke` two functions down lands on `#invite`, so this is the anchor a copy "
        "of it carries — the section that no longer has a form on it.",
    ),
    Mutant(
        name="ui-team-link-form-preticks-every-project",
        path="internal/ui/team.go",
        old="g.If(m.Target == preselected, h.Checked())",
        new="g.If(m.Target.Kind == preselected.Kind, h.Checked())",
        killer="TestTheTeamPageHasOneMintFormAndPreselectsTheProject",
        why="a comparison narrowed to the half that reads as the discriminator. Every project the caller "
        "manages arrives ticked — with NO `?project=` too — so the unread form puts all of them on a link.",
    ),
    Mutant(
        name="ui-team-link-form-never-preticks",
        path="internal/ui/team.go",
        old="g.If(m.Target == preselected, h.Checked())",
        new="g.If(m.Target == preselected && v.Invite.Project.ID == \"\", h.Checked())",
        killer="TestTheTeamPageHasOneMintFormAndPreselectsTheProject",
        why="a guard for 'no project chosen' written the wrong way round, so the condition is never "
        "true: 'invite somebody to this project' lands on a form that has forgotten which project.",
    ),
    Mutant(
        name="ui-fold-notice-loses-a-cost-the-operator-accepted",
        path="internal/ui/invitehandlers.go",
        old="\"of one project cannot see or revoke each other's links.\"",
        new="\"of one project each manage their own links.\"",
        killer="TestTheFoldNoticeIsPinnedWhole",
        why="the clause that makes the product sound weakest, reworded into one that sounds like a "
        "feature. A guard on the words 'team link' or 'owner' passes it.",
    ),
    Mutant(
        name="ui-fold-notice-is-not-rendered",
        path="internal/ui/team.go",
        old="g.If(!v.NoInviteStore, h.P(h.Class(\"note\"), g.Text(InviteFoldNotice))),",
        new="g.If(false, h.P(h.Class(\"note\"), g.Text(InviteFoldNotice))),",
        killer="TestTheFoldNoticeIsPinnedWhole",
        why="a constant pinned word for word and shown to nobody: the constant-only half of the test "
        "stays green, which is why the test also reads the rendered page.",
    ),
    Mutant(
        name="ui-fold-hides-the-invitations-that-exist",
        path="internal/ui/invitehandlers.go",
        old="\tview.Outstanding = inviteRows(rows, s.now())",
        new="\tview.Outstanding = inviteRows(rows[:0], s.now())",
        killer="TestAnInvitationIssuedBeforeTheFoldStillRedeemsAndIsStillRevocable",
        why="'invitations are gone' taken one step too far. A link sent last week still redeems, and "
        "the page that could withdraw it says no invitation exists.",
    ),
    Mutant(
        name="ui-fold-stops-revoking-an-invitation",
        path="internal/ui/invitehandlers.go",
        old="\tif err := s.inviting.Revoke(r.Context(), membershipActor(id), digest); err != nil {",
        new="\tif err := error(nil); err != nil {",
        killer="TestAnInvitationIssuedBeforeTheFoldStillRedeemsAndIsStillRevocable",
        why="the revoke row made as inert as the mint row beside it. It still answers 303 with the "
        "'Revoked.' banner, and the invitation stays open.",
    ),
    Mutant(
        name="ui-fold-sends-every-token-to-the-link-store",
        path="internal/ui/inviting.go",
        old="\tif !known && c.Links != nil {\n\t\t// Not an invitation: a team link, or nothing. See [ControlInviting.Links].",
        new="\tif c.Links != nil {\n\t\t// Not an invitation: a team link, or nothing. See [ControlInviting.Links].",
        killer="TestAnInvitationIssuedBeforeTheFoldStillRedeemsAndIsStillRevocable",
        extra_killers=("TestAnInvitationStillRedeemsAsAnInvitationWithLinksWired",),
        why="'every invitation is a team link now' read as a statement about TOKENS. An invitation "
        "already in somebody's inbox is then looked up in a table that has never held it, and refuses.",
    ),
    Mutant(
        name="ui-invite-read-refuses-instead-of-answering-with-no-store",
        path="internal/ui/invitehandlers.go",
        old="\tif s.inviting == nil {\n\t\t// Nothing to ask. The page says so — see [NoInviteStore] for why this is a page\n\t\t// rather than a refusal.\n\t\treturn view, true\n\t}",
        new="\tif s.inviting == nil {\n\t\ts.refuseWithoutInviteStore(w)\n\t\treturn InviteView{}, false\n\t}",
        killer="TestTheInviteRowsAnswerHonestlyWithNoInviteStore",
        why="consistency with the two WRITES, and with `refuseUnconfiguredOAuth` one file "
        "over. `shell` links this path from the header of every page unconditionally, so a "
        "501 here is a dead link in the frame of the whole surface.",
    ),
    Mutant(
        name="ui-empty-invite-index-drops-the-authority-sentence",
        path="internal/ui/render.go",
        old='\t\tg.If(len(v.Projects) == 0 && !v.NoStore, h.P(h.Class("empty"), g.Text(',
        new='\t\tg.If(false, h.P(h.Class("empty"), g.Text(',
        killer="TestTheInviteIndexSaysAnEmptyListIsAnAuthorityAnswer",
        why="an empty list looks self-explanatory. 'nothing here' and 'nothing you may see' "
        "are different facts with different next actions, and `Page`'s equivalent sentence "
        "shipped as a measured lie for exactly this reason.",
    ),
    Mutant(
        name="ui-invite-nav-affordance-stops-linking",
        path="internal/ui/render.go",
        # Re-aimed in #214: the header's ONE entry to invitations (and sharing, and team links)
        # is the Team link now (O-a / round 0 D3); the row keeps its name and its defect.
        old='h.P(h.Class("nav-team"), h.A(h.Href(TeamPath), g.Text("Team"))),',
        new='h.P(h.Class("nav-team"), g.Text("Team")),',
        killer="TestEveryRenderedPageCarriesBothNavigationAffordances",
        why="an affordance that looks like a label is how the SHARE flow shipped deployed, "
        "authorised, route-registered, test-covered and reported MISSING. Same surface, same "
        "defect, one flow along — and every gate but this one stays green.",
    ),
    Mutant(
        name="ui-join-page-gains-authenticated-navigation",
        path="internal/ui/render.go",
        old='\t\t\th.Header(h.Class("page-header"), wordmark(app, false)),\n\t\t\th.Main(append([]g.Node{h.Class("join-main")',
        new='\t\t\th.Header(h.Class("page-header"), wordmark(app, false),\n\t\t\t\th.P(h.Class("nav-share"), h.A(h.Href(SharePath), g.Text("Sharing")))),\n\t\t\th.Main(append([]g.Node{h.Class("join-main")',
        killer="TestNoPublicPageOffersAuthenticatedNavigation",
        why="the duplicate-header tidy-up, which is what `TestTheSignInPageOffersNoAuthenticated"
        "Navigation` already exists to refuse on the OTHER public page. A `Sharing` link in "
        "front of an unauthenticated visitor points at a route that answers 401.",
    ),
    Mutant(
        name="ui-invite-honesty-notice-loses-its-weakest-clause",
        path="internal/ui/invitehandlers.go",
        old='"revoking it stops it being redeemed but takes nothing back from somebody who has " +\n\t"already joined."',
        new='"revoking it stops it being redeemed."',
        killer="TestTheInviteHonestyNoticeIsPinnedWhole",
        why="the clause most worth dropping is always the one that makes the product sound "
        "weakest, and a guard on WORDS would pass this. Revoking an invitation somebody has "
        "already redeemed changes nothing about their access, and an administrator who "
        "believes otherwise has withdrawn nothing. 🔴 THIS ROW SURVIVED WHEN IT WAS FIRST "
        "WRITTEN, and the verdict was right: the guard compared the page against the CONSTANT, "
        "so both sides of the comparison moved together and a reword was invisible. The guard "
        "now pins a LITERAL copy of the sentence.",
    ),
    Mutant(
        name="ui-share-replica-honesty-notice-loses-its-weakest-clause",
        path="internal/ui/render.go",
        old='"revoking a share stops future syncs: it does not recall entries already copied onto " +\n\t"somebody\'s machine."',
        new='"revoking a share stops future syncs."',
        killer="TestTheReplicaHonestyNoticeIsPinnedWhole",
        why="🔴 A PRE-EXISTING DEFECT THIS SWEEP FOUND, IN A SHIPPED GUARD, ON A CLAIM "
        "`AGENTS.md` MAKES. Writing the invite flow's notice guard by modelling it on the share "
        "flow's copied the share flow's hole: `TestTheReplicaHonestyNoticeIsPinnedWhole` read "
        "`normalizeSpace(ReplicaHonesty)`, so editing the constant moved BOTH sides and this "
        "mutation passed — while that test's own doc said 'any cosmetic reword reds this test, "
        "which is the intended cost' and `AGENTS.md` asserts the notice 'is pinned as a WHOLE "
        "NORMALISED STRING'. Its negative control proved only that the COMPARISON can fail, "
        "never that a change to the CONSTANT would. The row is here so the fix has a gate.",
    ),

    # ---- the Team page and its multi-target TEAM LINK (`internal/ui/team*.go`) ----------
    #
    # 🔴 ONE ROW PER AUTHZ RULE THE LINK ADDS, AND THE SQL HALF IS NOT HERE. The store's
    # conditional `UPDATE`, its `CHECK` and its expiry boundary are measured by the Postgres
    # tier (`internal/pgstore/teamlinks_pgtest_test.go`), which this battery does not run; the
    # `invite-teamlink-*` rows mutate `TeamLink.StateAt`, which the in-memory store the UI
    # tests drive asks inside its lock — the same predicate the SQL is pinned against.
    Mutant(
        name="ui-teamlink-scope-arm-stops-asking-for-every-verb",
        path="internal/ui/teamlinks.go",
        old="return have.Has(control.VerbAdmin) && have.Intersect(want) == want",
        new="return have.Has(control.VerbAdmin) && !want.Empty()",
        killer="TestALinkCannotConferVerbsItsMinterLacks",
        why="a LINK GRANTS BEYOND ITS MINTER'S AUTHORITY. Verbs are independent bits, so 'holds "
        "admin' reads like 'holds everything' and is not: an admin-only grantee would mint a "
        "reader link conferring a read it does not have.",
    ),
    Mutant(
        name="ui-teamlink-scope-arm-drops-the-admin-requirement",
        path="internal/ui/teamlinks.go",
        old="return have.Has(control.VerbAdmin) && have.Intersect(want) == want",
        new="return have.Intersect(want) == want",
        killer="TestAScopeLinkNeedsAdminOnTheScope",
        why="'you may hand out what you hold' without the share flow's admin rule: every "
        "project MEMBER could then put the project's scopes on a reusable link to anybody.",
    ),
    Mutant(
        name="ui-teamlink-project-arm-stops-asking-who-may-manage",
        path="internal/ui/teamlinks.go",
        old="if !member || !held.CanManageMembers() {",
        new="if !member {",
        killer="TestAProjectLinkNeedsAMemberManager",
        why="belonging taken for managing — a plain member minting a link that adds people to "
        "the project, which `CanManageMembers` exists to refuse.",
    ),
    Mutant(
        name="ui-teamlink-mint-skips-the-authority-check",
        path="internal/ui/teamlinks.go",
        old="\t\tif !mayLink(m, actor, t, role) {\n\t\t\treturn \"\", invite.TeamLink{}, ErrNotLinkable",
        new="\t\tif false && !mayLink(m, actor, t, role) {\n\t\t\treturn \"\", invite.TeamLink{}, ErrNotLinkable",
        killer="TestAProjectLinkNeedsAMemberManager",
        extra_killers=("TestAScopeLinkNeedsAdminOnTheScope", "TestALinkCannotConferVerbsItsMinterLacks"),
        why="the chooser trusted as the check. `Mintable` filters what a BROWSER is offered; a "
        "hand-made POST names any target, and this is the only gate it meets at mint time.",
    ),
    Mutant(
        name="ui-teamlink-redeem-skips-the-minter-recheck",
        path="internal/ui/teamlinks.go",
        old="\t\tif !mayLink(m, minter, t, link.Role) {",
        new="\t\tif false && !mayLink(m, minter, t, link.Role) {",
        killer="TestAMinterWhoLostAuthorityMintsNothingUsable",
        why="REDEEM SKIPS THE RE-CHECK: authority checked once, at mint, and trusted for the "
        "link's whole life. A reusable link outlives its minter's demotion by up to 30 days, "
        "and without this every link a removed admin made keeps enrolling strangers.",
    ),
    Mutant(
        name="invite-teamlink-single-use-stops-closing",
        path="internal/invite/teamlink.go",
        old="case !l.Reusable && l.Redemptions > 0:",
        new="case false && l.Redemptions > 0:",
        killer="TestASingleUseLinkRedeemsExactlyOnce",
        why="REUSE UNTICKED BUT REDEEMABLE TWICE — the count stored and not consulted, which "
        "turns every single-use link into open enrolment.",
    ),
    Mutant(
        name="invite-teamlink-revoke-stops-closing",
        path="internal/invite/teamlink.go",
        old="\tcase !l.RevokedAt.IsZero():\n\t\treturn StateRevoked",
        new="\tcase false:\n\t\treturn StateRevoked",
        killer="TestARevokedLinkIsNotRedeemable",
        why="A REVOKED LINK STILL REDEEMABLE: the tombstone written and not read, which is the "
        "one remedy a minter has for a leaked reusable link.",
    ),
    Mutant(
        name="invite-teamlink-expiry-stops-closing-at-the-boundary",
        path="internal/invite/teamlink.go",
        old="case !now.Before(l.ExpiresAt):",
        new="case now.After(l.ExpiresAt):",
        killer="TestAnExpiredLinkIsNotRedeemable",
        why="AN EXPIRED LINK REDEEMABLE, at the one instant the two spellings differ: the closed "
        "boundary written open, so `ExpiresAt` itself still redeems — and the SQL guard pinned "
        "against `StateAt` would then be pinned against the wrong rule.",
    ),
    Mutant(
        name="ui-teamlink-scope-target-joins-its-whole-project",
        path="internal/ui/teamlinks.go",
        old="if err := grant(control.ObjectScope, t.ID, want); err != nil {",
        new="if err := grant(control.ObjectProject, m.Scopes[t.ID].ProjectID, want); err != nil {",
        killer="TestARedemptionJoinsExactlyTheSelectedTargets",
        why="A MULTI-TARGET LINK JOINS A TARGET NOT SELECTED: a scope 'joined' through its owning "
        "project, which confers every sibling scope the minter never ticked.",
    ),
    Mutant(
        name="ui-teamlink-revoke-skips-the-ownership-check",
        path="internal/ui/teamlinks.go",
        old="if !known || link.Inviter != actor.ID {",
        new="if !known || link.Inviter == \"\" {",
        killer="TestOnlyTheMinterCanRevokeALink",
        why="REVOKE BY A NON-OWNER: any signed-in user who learns a digest (it is rendered into "
        "the minter's page source) could withdraw somebody else's link.",
    ),
    Mutant(
        name="ui-teamlink-overwrites-an-existing-membership",
        path="internal/ui/teamlinks.go",
        old="\t\t\tif _, member := m.RoleIn(t.ID, user); member {\n\t\t\t\tcontinue\n\t\t\t}\n\t\t\tif link.Role",
        new="\t\t\tif _, member := m.RoleIn(t.ID, user); member && false {\n\t\t\t\tcontinue\n\t\t\t}\n\t\t\tif link.Role",
        killer="TestALinkNeverOverwritesAnExistingMembership",
        why="`ErrAlreadyAMember`'s hazard on the new writer: `apply` calls `setMembership` "
        "unconditionally, so a member link redeemed by the sole owner DEMOTES them.",
    ),
    Mutant(
        name="ui-teamlink-reuse-tick-is-ignored",
        path="internal/ui/team.go",
        old='reusable := r.PostFormValue(FieldReuse) != ""',
        new="reusable := true",
        killer="TestTheTeamLinkFormPassesEveryTickedTargetThrough",
        why="the dangerous default: every link minted reusable whatever the box said.",
    ),
    Mutant(
        name="ui-team-page-offers-the-unnarrowed-principals-targets",
        path="internal/ui/team.go",
        old="view.Mintable = s.teamLinks.Mintable(membershipActor(id))",
        new="view.Mintable = s.teamLinks.Mintable(id.Principal)",
        killer="TestANarrowedBearerHasNoTeamLinkAuthority",
        extra_killers=("TestEveryMembershipDecisionActsAsMembershipActor",),
        why="`membershipActor` bypassed on the new page: a credential narrowed to one scope is "
        "shown every project its owner manages as something it can put on a link.",
    ),
    Mutant(
        name="ui-team-mint-acts-as-the-unnarrowed-principal",
        path="internal/ui/team.go",
        old="s.teamLinks.Mint(r.Context(), membershipActor(id), targets, role, ttl, reusable)",
        new="s.teamLinks.Mint(r.Context(), id.Principal, targets, role, ttl, reusable)",
        killer="TestANarrowedBearerHasNoTeamLinkAuthority",
        extra_killers=("TestEveryMembershipDecisionActsAsMembershipActor",),
        why="the escalation `membershipActor` exists for, through the new door: a leaked "
        "narrowed token mints a reusable link into every project its owner manages.",
    ),
    Mutant(
        name="ui-team-honesty-notice-loses-its-reuse-clause",
        path="internal/ui/team.go",
        old='"scope it names. A link that allows reuse can be redeemed by any number of people, any " +\n\t"number of times, until it expires or is revoked. A link stops working',
        new='"scope it names. A link stops working',
        killer="TestTheTeamHonestyNoticeIsPinnedWhole",
        why="the clause that makes the product sound weakest is the reuse one: a reusable link is "
        "open enrolment until it expires, and a notice without that sentence lets a minter "
        "believe a link is for the one person they sent it to.",
    ),
    # ---- #214's fix round: the audit's findings and the operator's decisions ----------------
    Mutant(
        name="ui-teamlink-log-confirms-before-the-authority-write",
        path="internal/ui/teamlinks.go",
        old="\tspent, err := c.Store.RedeemLink(token, userID, true, now)\n\tif err != nil {\n\t\treturn Redemption{}, err\n\t}",
        new="\tspent, err := c.Store.RedeemLink(token, userID, true, now)\n\tif err != nil {\n\t\treturn Redemption{}, err\n\t}\n"
        "\t_ = c.Store.ConfirmRedemption(spent.Digest, spent.Redemptions)",
        killer="TestTheRedemptionLogNamesOnlyRealJoins",
        why="round 1 🟡1, restored: the audit row trusted at the spend. Two tabs on one identity "
        "spend one reusable link, one journal write is refused, and the log names a principal that "
        "was never created as 'joined (account created by this link)'.",
    ),
    Mutant(
        name="ui-team-log-renders-an-unconfirmed-row-as-a-join",
        path="internal/ui/team.go",
        old="\t\t\tif !red.Confirmed {",
        new="\t\t\tif false {",
        killer="TestTheRedemptionLogNamesOnlyRealJoins",
        extra_killers=("TestTheTeamPageRendersEveryLinkWithItsLog",),
        why="the store records the truth and the page ignores it — an attempt rendered as a join.",
    ),
    Mutant(
        name="ui-share-revocable-drops-project-wide-grants",
        path="internal/ui/sharing.go",
        old="\t\tif !onScope && !onOwner {",
        new="\t\tif !onScope && (!onOwner || true) {",
        killer="TestAProjectWideGrantIsListedLabelledAndRevocable",
        why="round 1 🟡3 and O-b, restored: the take-back list back to scope grants only, so the "
        "page's note ('keeps it after every grant below is revoked') is false for a project-wide "
        "grantee and their access needs the CLI to remove.",
    ),
    Mutant(
        name="ui-share-audience-loses-the-project-wide-label",
        path="internal/ui/sharing.go",
        old="ByProjectGrant: projectGranted(m, p, owner),",
        new="ByProjectGrant: false && projectGranted(m, p, owner),",
        killer="TestAProjectWideGrantIsListedLabelledAndRevocable",
        why="an audience row that says neither 'via membership' nor 'via a project-wide grant' reads "
        "as a scope grant — which the take-back list then does not show.",
    ),
    Mutant(
        name="ui-unshare-refuses-every-project-wide-grant",
        path="internal/ui/sharing.go",
        old="\t\treturn member && role.CanManageMembers()",
        new="\t\treturn member && role.CanManageMembers() && false",
        killer="TestAProjectWideGrantIsListedLabelledAndRevocable",
        why="the pre-O-b rule ('a grant over a PROJECT is refused here'), restored: a project-wide "
        "grant is listed with a button that cannot work.",
    ),
    Mutant(
        name="ui-unshare-project-grant-ignores-the-narrowing",
        path="internal/ui/sharing.go",
        old="\t\tif auth.Narrowed() || actor.Kind != control.KindUser {",
        new="\t\tif actor.Kind != control.KindUser {",
        killer="TestANarrowedBearerCannotRevokeAProjectWideGrant",
        extra_killers=("TestAProjectWideGrantIsListedLabelledAndRevocable",),
        why="membership authority reached through a credential narrowed to one scope — the "
        "escalation `membershipActor` exists for, through `handleUnshare`'s attribution principal.",
    ),
    Mutant(
        name="ui-unshare-project-grant-for-any-member",
        path="internal/ui/sharing.go",
        old="\t\treturn member && role.CanManageMembers()",
        new="\t\treturn member && role != \"\"",
        killer="TestAProjectWideGrantIsListedLabelledAndRevocable",
        why="belonging taken for managing: a plain member withdrawing a colleague's project-wide access.",
    ),
    Mutant(
        name="ui-old-share-path-drops-its-query",
        path="internal/ui/sharehandlers.go",
        old="\tredirectToTeam(w, teamHref(r.URL.RawQuery, teamShareAnchor))",
        new="\tredirectToTeam(w, teamHref(\"\", teamShareAnchor))",
        killer="TestTheOldFlowPathsRedirectToTheTeamPage",
        why="every bookmarked `/share?scope=…` and every pre-move redirect landing on the index "
        "instead of the scope it named (O-a: the query is carried).",
    ),
    Mutant(
        name="ui-old-invite-path-keeps-the-pre-move-code",
        path="internal/ui/invitehandlers.go",
        old="\tif q.Get(QueryOutcome) == inviteOutcomeRevokedBeforeTeam {",
        new="\tif false {",
        killer="TestTheOldFlowPathsRedirectToTheTeamPage",
        why="on one page `revoked` is the SHARE flow's code: an old invite-revoke link would render "
        "the share banner about a write that never happened.",
    ),
    Mutant(
        name="ui-team-redirect-gains-an-html-body",
        path="internal/ui/team.go",
        old="\tw.Header().Set(\"Location\", href)\n\tw.WriteHeader(http.StatusSeeOther)",
        new="\tw.Header().Set(\"Location\", href)\n\tw.Header().Set(\"Content-Type\", \"text/html; charset=utf-8\")\n"
        "\tw.WriteHeader(http.StatusSeeOther)",
        killer="TestEveryNonPublicHTMLRowIsNoStore",
        why="`http.Redirect`'s shape: an HTML response this surface did not render through "
        "`writeHTML`, so it carries no `no-store`.",
    ),
    Mutant(
        name="ui-inviting-without-links-builds",
        path="internal/ui/server.go",
        old="if teamLinks = cfg.Inviting.TeamLinks(); teamLinks == nil {",
        new="if teamLinks = cfg.Inviting.TeamLinks(); false {",
        killer="TestAnInvitationHalfWithoutTeamLinksIsRefused",
        why="round 0 D2 / round 1 🟡4: a half-wired server built — invitations whose store never "
        "hands an unknown token to the link store, so every team link minted refuses at the callback.",
    ),
    Mutant(
        name="main-drops-the-link-store-from-the-invitation-half",
        path="cmd/cairn-ui/invitations.go",
        old="\treturn ui.ControlInviting{Authority: authority, Invites: invites, Links: team}",
        new="\t_ = team\n\treturn ui.ControlInviting{Authority: authority, Invites: invites}",
        killer="TestTheWiredInvitationHalfRedeemsATeamLink",
        why="round 1 🟡4, MEASURED: deleting `Links: links` in `main` left both tiers green. This is "
        "that edit, at the one function `main` now builds both halves with.",
    ),
    Mutant(
        name="ui-link-log-line-loses-the-link",
        path="internal/ui/inviting.go",
        old="\tif !r.Link {\n\t\treturn fmt.Sprintf(\"project=%s role=%s\", r.Project, r.Role)",
        new="\tif true {\n\t\treturn fmt.Sprintf(\"project=%s role=%s\", r.Project, r.Role)",
        killer="TestALinkRedemptionLogLineNamesTheLink",
        why="round 1 🟢5, restored: a provisioning line for a team link with a blank project and "
        "role — a principal created that nobody can attribute to a link.",
    ),
    # ---- #214 round 2: the revoke button and the project name are decided per viewer ----
    Mutant(
        name="ui-revoke-form-rendered-without-mayrevokegrant",
        path="internal/ui/render.go",
        old="\t\tg.If(csrf != \"\" && row.MayRevoke, h.FormEl(",
        new="\t\tg.If(csrf != \"\", h.FormEl(",
        killer="TestARevokeFormIsRenderedOnlyWhereTheRevokeWouldBeAuthorised",
        why="round 2 🟡A, restored: the button gated on a session token alone, so an outsider holding "
        "admin on one scope is offered Revoke on a project-wide grant that POST /unshare refuses (403).",
    ),
    Mutant(
        name="ui-project-wide-row-names-its-project-to-outsiders",
        path="internal/ui/sharing.go",
        old="\t\t\t\trow.Project = \"\"",
        new="\t\t\t\trow.Project = row.Project + \"\"",
        killer="TestARevokeFormIsRenderedOnlyWhereTheRevokeWouldBeAuthorised",
        why="the project-wide label naming the owning project to a scope admin who is in no project "
        "there — a fact about a project they were never shown.",
    ),

    # ---- the `## Requirements` section: the boundary, the count, the attribution ----
    Mutant(
        name="requirements-read-the-whole-entry-body",
        pkgs=PKGS + ("./internal/report/",),
        path="internal/report/entry.go",
        old="store.ParseRequirements(sections[store.RequirementsHeading])",
        new="store.ParseRequirements(text)",
        killer="TestTheRenderedBytesMatchTheORACLEOverShapesTheCorpusCannotSend",
        why="🔴 THE SECTION BOUNDARY, WHICH IS THE ONLY THING SEPARATING A STATED "
        "REQUIREMENT FROM A NOTE ABOUT HISTORY. Nothing in a bullet's TEXT distinguishes "
        "them — no keyword, no shape — so a reader handed the entry body instead of the "
        "section body silently promotes every `OPEN:` nuance bullet to a requirement. "
        "`marked-three` in the reader fixture carries one bullet VERBATIM under both "
        "headings for exactly this row: the mutant folds it in and `🔴 4 REQ OPEN` "
        "becomes 5, which is why the ledger pins the whole badge run rather than the two "
        "new badges alone. ⚠ The store-level boundary test does NOT kill this one — it "
        "calls `ParseRequirements` directly and so cannot see a caller passing the wrong "
        "body. That asymmetry is the reason the differential fixture earns its place.",
    ),
    Mutant(
        name="requirements-open-count-is-not-met",
        pkgs=PKGS + ("./internal/store/",),
        path="internal/store/requirements.go",
        old="return r.OpennessPopulation() == PopulationOpen",
        new="return !r.IsMet()",
        killer="TestMetAndOpenAreNotComplements",
        why="the open COUNT, written as the complement of met — which reads as obviously "
        "equivalent and is not. A bullet carrying no marker is NEITHER open nor met; it is "
        "unstated. This mutant promotes every unmarked bullet under the heading to an open "
        "requirement, so the badge starts claiming work nobody declared, and it does so in "
        "the direction that looks like diligence.",
    ),
    Mutant(
        name="requirements-provenance-accepts-a-prefix",
        pkgs=PKGS + ("./internal/store/",),
        path="internal/store/requirements.go",
        old="if s[1:1+len(word)] != word || s[1+len(word)] != ')' {",
        new="if s[1:1+len(word)] != word {",
        killer="TestProvenanceIsTheWholeParenthesizedWord",
        extra_killers=("TestProvenanceSpanEndsPastTheParentheticalAndNowhereElse",),
        why="⚠ THE THIRD ROW, ADDED BEYOND THE TWO THE TASK NAMED, AND ARGUED FOR RATHER "
        "THAN SMUGGLED IN. Provenance is the feature — `(operator)` versus `(inferred)` is "
        "the whole distinction being asked for — so the one failure it cannot have is being "
        "MANUFACTURABLE. Dropping the closing-paren check makes `(operators)`, "
        "`(operator-ish)` and anything else starting with the right letters resolve to "
        "`operator`, attributing a statement to the operator that the operator did not "
        "make. Nothing about the resulting code reads wrong. "
        "🔴 THE PATTERN WAS RE-DERIVED ONCE, AND THE REASON IS WORTH KEEPING. It read "
        "`return rs[1+len(w)] == ')'` → `return true` while `matchParenthesizedWord` "
        "indexed RUNES and returned a bool; the function now takes a byte offset and "
        "returns a LENGTH, so that pattern matched 0 times and this row reported a HARNESS "
        "ERROR — correctly, and loudly, which is the whole point of the occurrence check: "
        "a pattern matching nothing would otherwise score the mutant SURVIVED without ever "
        "running. ⚠ THE LESSON FOR THE NEXT PERSON REFACTORING `internal/store`: a battery "
        "row is coupled to the exact SPELLING of the line it mutates and names it NOWHERE "
        "ELSE — grepping for the function's NAME finds nothing, because the row does not "
        "carry it. Grep for `path=\"<file>\"` in this module instead. "
        "⚠ `extra_killers` is MEASURED, not assumed: the mutant reddens the span test too, "
        "because one scan now decides both the word and the offset a renderer cuts at.",
    ),
    # 🔴 S4 OF THE ARCS/SESSIONS PHASE — THE BROWSER SURFACE'S SCOPE-PAGE SECTION AND `/arc` PAGE.
    # Each row reverts ONE decision `internal/ui/arcs.go` records, and each was watched killed by
    # its named guard with that guard's own message before it was committed here. ⚠ TWO SINGLE
    # MUTANTS SURVIVE BY DESIGN AND ARE DELIBERATELY NOT ROWS: on the arc page the home id is
    # matched against the narrowed `Visible` list AND `report.Arc` re-checks the home against the
    # same set, so removing either layer alone leaves the other holding (measured; recorded in
    # `internal/ui/README.md`'s S4 section). A row for either would have to be EQUIVALENT, and an
    # equivalent row for a defence-in-depth layer reads as "this check does nothing".
    Mutant(
        name="ui-arc-page-refuses-an-unresolvable-home-with-the-scope-refusal",
        path="internal/ui/arcs.go",
        old="\tif found {\n\t\thome = scope.Name\n\t}",
        new="\tif !found {\n\t\twritePlain(w, http.StatusNotFound, browseRefusal)\n\t\treturn\n\t}\n\thome = scope.Name",
        killer="TestAnArcHomedInAnUnreadableScopeRendersExactlyLikeANeverRegisteredOne",
        why="the natural shortcut — refuse an unknown home id at the scope lookup with the scope "
        "page's own refusal — makes 'home unreadable' and 'slug unregistered' two different "
        "bodies, which is an oracle over which scopes hold registered arcs.",
    ),
    Mutant(
        name="ui-scope-section-lists-arcs-without-the-home-rule",
        path="internal/ui/arcs.go",
        old="\tout.Arcs, err = report.Arcs(s.Root, scope, visible, snap)",
        new="\tout.Arcs, err = report.Arcs(s.Root, scope, store.Unrestricted(), snap)",
        killer="TestTheScopePageListsOnlyArcsWhoseHomeIsReadable",
        extra_killers=("TestTheSectionIsTheSameAnswerTheReportsGiveForTheSameVisibleSet",),
        why="the scope is already proved readable by the page, so narrowing again looks redundant "
        "— but the arc rule (Q1) is about the arc's HOME, and an unnarrowed call lists arcs homed "
        "in scopes the caller cannot read, naming their homes and slugs on a page it can.",
    ),
    Mutant(
        name="ui-arc-row-renders-unknown-as-open",
        path="internal/ui/arcs.go",
        old='h.Span(h.Class("badge"), h.TitleAttr("status"), g.Text(report.StatusWord(a.Status))),',
        new='h.Span(h.Class("badge"), h.TitleAttr("status"), g.Text(map[bool]string{true: "closed", false: "open"}[a.Status == "closed"])),',
        killer="TestAnUnknownStatusIsRenderedAsUnknownAndNeverAsOpen",
        why="a two-state badge (closed, else open) is what a renderer written before Q4 looks "
        "like, and it turns every registration that carried no verdict into an open one.",
    ),
    Mutant(
        name="ui-unconfigured-journal-read-as-unreadable",
        path="internal/ui/arcs.go",
        old='\tif s.ArcJournal == "" {\n\t\treturn nil, nil\n\t}',
        new='\tif s.ArcJournal == "" {\n\t\treturn nil, &arcs.JournalUnreadableError{Path: ""}\n\t}',
        killer="TestAnUnconfiguredJournalSaysSoRatherThanFailing",
        why="treating 'no journal' as an error is the fail-closed reflex, and it renders the "
        "designed OFF state (Q2) as a broken one on every scope page of every deployment that "
        "has not made the mount change yet.",
    ),
    Mutant(
        name="ui-arc-page-errors-on-an-unconfigured-journal",
        path="internal/ui/arcs.go",
        old="\tcase report.StatusRegistrationsUnconfigured:\n",
        new="\tcase report.StatusRegistrationsUnconfigured:\n\t\twritePlain(w, http.StatusInternalServerError, \"x\")\n\t\treturn\n",
        killer="TestAnUnconfiguredJournalSaysSoRatherThanFailing",
        why="the arc page's switch has one success arm; folding the off state into the error "
        "arm is a one-line tidy that turns the designed off state into a 500.",
    ),
    Mutant(
        name="ui-arc-href-built-by-concatenation",
        path="internal/ui/arcs.go",
        old='return ArcPath + "?" + url.Values{QueryHome: []string{string(home)}, QuerySlug: []string{slug}}.Encode()',
        new='return ArcPath + "?home=" + string(home) + "&slug=" + slug + url.Values{}.Encode()',
        killer="TestEveryArcHrefIsASameOriginPathWithEncodedOperands",
        why="string concatenation is how every first draft builds a query, and with an operand "
        "that carries `&` or `#` it splits one value into a second parameter.",
    ),
    Mutant(
        name="ui-arc-link-href-is-its-label",
        path="internal/ui/arcs.go",
        old="name = h.A(h.Href(arcHref(home, a.Slug)), g.Text(label))",
        new="name = h.A(h.Href(label), g.Text(label))",
        killer="TestEveryArcHrefIsASameOriginPathWithEncodedOperands",
        why="linking the text you show is the obvious shortcut, and it makes registration DATA "
        "the href — so a home or slug spelled as a scheme becomes a live `javascript:` link.",
    ),
    Mutant(
        name="ui-scope-section-fetched-before-the-refusal",
        path="internal/ui/server.go",
        # ⚠ The anchor's tail moved when the scope page's body became `scopeView` (the S5 follow-up);
        # the mutation itself — a read keyed on the caller's `wanted` ahead of the refusal — did not.
        old="\tscope, found := pickScope(scopes, wanted)\n\tif !found {\n\t\twritePlain(w, http.StatusNotFound, browseRefusal)\n\t\treturn\n\t}\n\t// The tab is read AFTER",
        new="\t_, _ = s.source.Touched(id.Auth, string(wanted))\n\tscope, found := pickScope(scopes, wanted)\n\tif !found {\n\t\twritePlain(w, http.StatusNotFound, browseRefusal)\n\t\treturn\n\t}\n\t// The tab is read AFTER",
        killer="TestAScopeTheCallerCannotReadGetsTheExistingRefusalAndNoSection",
        why="fetching the page's data up front and refusing afterwards is an ordinary "
        "refactor, and it puts a read keyed on caller-chosen input ahead of the refusal that "
        "is supposed to stop it.",
    ),
    Mutant(
        name="ui-binary-ignores-the-arc-journal-refusal",
        path="cmd/cairn-ui/main.go",
        old="\tresolvedArcJournal, err := resolveArcJournal(*store, *arcJournal)\n\tif err != nil {",
        new="\tresolvedArcJournal, err := resolveArcJournal(*store, *arcJournal)\n\tif err != nil && false {",
        killer="TestTheBinaryREFUSESAnArcJournalInsideTheStoreRoot",
        why="whether `main` ACTS on the check is wiring no in-process test can see; a journal "
        "inside the store root then comes up serving, its directory a scope to every bare row.",
    ),
    Mutant(
        name="ui-binary-never-hands-the-journal-to-the-source",
        path="cmd/cairn-ui/main.go",
        old="Source: ui.StoreSource{Root: *store, RefBase: envalias.OSValue, ArcJournal: resolvedArcJournal},",
        # `[:0]` rather than deleting the field: deleting it leaves `resolvedArcJournal` unused and
        # the tree does not BUILD (measured), which this battery reports as a harness error.
        new="Source: ui.StoreSource{Root: *store, RefBase: envalias.OSValue, ArcJournal: resolvedArcJournal[:0]},",
        killer="TestTheBinaryREFUSESAnArcJournalInsideTheStoreRoot",
        why="a flag that is parsed and checked but never wired is green on every refusal arm; "
        "only the startup line read off the WIRED source says the journal reached the reader.",
    ),
    # 🔴 THE CROSS-SCOPE SESSION PAGE AND THE SCOPE TABS. The session page is the first browse page
    # keyed by a value that is NOT scoped, so its narrowing is the whole design; each row below was
    # watched killed by its named guard before it was committed here.
    Mutant(
        name="ui-session-page-walks-every-scope",
        path="internal/ui/sessionpage.go",
        old="rep, err := report.SessionAcross(s.Root, session, visible, snap)",
        new="_ = visible\n\trep, err := report.SessionAcross(s.Root, session, store.Unrestricted(), snap)",
        killer="TestTheSessionPageAggregatesReadableScopesNewestFirstAndOmitsAnUnreadableOne",
        extra_killers=("TestASessionOnlyInAnUnreadableScopeAnswersExactlyLikeOneNeverWritten",),
        why="a session id is global, so 'show everything this session wrote' is the natural first "
        "draft — and it lists writes in scopes the caller cannot read, and turns the 404 into an "
        "oracle over which hidden scopes a session touched.",
    ),
    Mutant(
        name="ui-session-report-lists-arcs-homed-in-an-unreadable-scope",
        path="internal/report/sessionacross.go",
        old="\t\t\tif !visible.Allows(reg.Home) {\n\t\t\t\tcontinue\n\t\t\t}\n\t\t\tfor _, m := range reg.Members {",
        new="\t\t\tfor _, m := range reg.Members {",
        killer="TestTheSessionAcrossAnswerGroupsReadableScopesNewestFirstAndDropsTheHiddenOne",
        extra_killers=("TestTheSessionPageAggregatesReadableScopesNewestFirstAndOmitsAnUnreadableOne",
                       "TestASessionOnlyInAnUnreadableScopeAnswersExactlyLikeOneNeverWritten"),
        pkgs=PKGS + ("./internal/report/",),
        why="membership looks like a fact about the SESSION, so filtering it by scope looks "
        "redundant — but the arc rule (Q1) is about the arc's HOME, and dropping it names hidden "
        "arcs and makes a hidden-only session a page instead of the uniform 404.",
    ),
    Mutant(
        name="ui-arc-row-names-a-hidden-declared-scope",
        path="internal/report/arcs.go",
        old="\t\t\tif visible.Allows(d) {",
        new="\t\t\tif true {",
        killer="TestAnArcDeclaringAHiddenScopeNeverNamesItOnAnyPage",
        pkgs=PKGS + ("./internal/report/",),
        why="the arc itself is readable (its home is), so its declared list looks like part of the "
        "arc — but each declared scope is its own authority question, and `scopeChip` falls back to "
        "the plain NAME for a scope whose id it cannot resolve, so an unfiltered list names a hidden "
        "scope on a page about a readable one.",
    ),
    Mutant(
        name="ui-session-id-bound-skipped",
        path="internal/ui/sessionpage.go",
        old="if !write.SessionComponent.MatchString(session) {",
        new="if false && !write.SessionComponent.MatchString(session) {",
        killer="TestAHostileSessionIdIsBoundedBeforeAnyRead",
        why="every hostile id still answers 404 without the bound, because none is FOUND — so the "
        "only observable is the store walk it costs, which is what an unbounded query string buys.",
    ),
    Mutant(
        name="ui-session-unseen-answer-differs",
        path="internal/ui/sessionpage.go",
        old="\t\twritePlain(w, http.StatusNotFound, sessionUnseenBody)\n\t\treturn\n\t}\n\tview.Session",
        new="\t\twritePlain(w, http.StatusNotFound, \"no such session\")\n\t\treturn\n\t}\n\tview.Session",
        killer="TestASessionOnlyInAnUnreadableScopeAnswersExactlyLikeOneNeverWritten",
        why="two refusal paths (the grammar bound and the empty answer) spelled two ways is the "
        "ordinary shape, and any difference between them is an oracle over who wrote where.",
    ),
    Mutant(
        name="ui-bullet-anchor-dropped",
        path="internal/ui/render.go",
        old='g.If(b.Anchor != "", h.ID(b.Anchor)),',
        new="",
        killer="TestEveryBulletLinkOnTheSessionPageLandsOnAnAnchorThatExists",
        why="the anchor and the link live on two different pages, so each page's own test is "
        "green without it; only following the link across the seam sees it land nowhere.",
    ),
    Mutant(
        name="ui-scope-tab-selection-ignored",
        path="internal/ui/render.go",
        old="case v.Tab == TabSessions && v.Touched != nil:",
        new="case false && v.Tab == TabSessions && v.Touched != nil:",
        killer="TestEachScopeTabRendersOnlyItsOwnPanel",
        why="a tab whose link changes the URL and not the panel looks like a working tab on every "
        "page but its own.",
    ),
    Mutant(
        name="ui-sessions-partial-badge-always-on",
        path="internal/ui/arcs.go",
        old='\tcase r.LowerBoundLine() != "":',
        new="\tcase true:",
        killer="TestThePartialBadgeRendersOnlyWhenTheAnswerIsPartial",
        why="a warning on every answer is a warning nobody reads; the badge is a signal only "
        "because it is ABSENT from a complete answer.",
    ),
    Mutant(
        name="ui-arcs-partial-badge-ignores-the-condition",
        path="internal/ui/arcs.go",
        old="if counted && arcsPartial(r) {",
        new="if counted {",
        killer="TestThePartialBadgeRendersOnlyWhenTheAnswerIsPartial",
        why="the arcs tab has three partial causes (damaged journal, rejected entries, unreadable "
        "entries) and the shortcut is to badge every counted answer.",
    ),
    # 🔴 THE ARCS-FIRST PAGE AND THE ARC PAGE'S TABS (S1 of `claudedocs/plan-cairn-arcs-presence.md`).
    # The plan's S1 test plan names four mutants — drop the clamp, use `reported_at`, count `unknown`
    # as open, skip the visibility check in the member walk — and each is a row here, plus the home
    # rule, the live window and the two tab rows. Each was watched killed by its named guard first.
    Mutant(
        name="ui-arcs-index-future-bullet-not-clamped",
        path="internal/ui/arcsindex.go",
        old="\tif today := utcDay(now); day.After(today) {",
        new="\tif today := utcDay(now); false && day.After(today) {",
        killer="TestAFutureDatedBulletDoesNotSortAboveToday",
        why="a bullet's date is the writer's word on a `put`, so it looks like data to sort by as "
        "written — and a member can then pin an arc to the top of everyone's page until the date passes.",
    ),
    Mutant(
        name="ui-arcs-index-last-updated-reads-reported-at",
        path="internal/ui/arcsindex.go",
        old="time.Parse(time.RFC3339, a.RegisteredAt)",
        new="time.Parse(time.RFC3339, a.ReportedAt)",
        killer="TestTheArcsPageListsLiveArcsNewestFirstAndCountsTheHidden",
        why="`reported_at` is the tooling's own 'when I measured' and reads like the natural 'last "
        "update' — but it is optional and on the tooling's clock, where `registered_at` is always "
        "present and the pod's.",
    ),
    Mutant(
        name="ui-arcs-index-unknown-counts-as-open",
        path="internal/ui/arcsindex.go",
        old="\tif a.Status == arcs.StatusOpen {",
        new="\tif a.Status != arcs.StatusClosed {",
        killer="TestTheArcsPageListsLiveArcsNewestFirstAndCountsTheHidden",
        why="'not closed' is the obvious spelling of 'still going', and it reads an arc whose tool "
        "reported NO verdict as open — Q4's one rule.",
    ),
    Mutant(
        name="ui-arcs-index-live-window-widened",
        path="internal/ui/arcsindex.go",
        old="const arcLiveDays = 14",
        new="const arcLiveDays = 15",
        killer="TestTheArcsPageListsLiveArcsNewestFirstAndCountsTheHidden",
        why="an off-by-a-day window is invisible on every arc that is not within a day of the "
        "boundary; only a fixture measured on BOTH sides of 14 days sees it.",
    ),
    Mutant(
        name="ui-arcs-index-live-window-compared-as-an-instant",
        path="internal/ui/arcsindex.go",
        old="return int(now.Sub(act.At)/(24*time.Hour)) <= arcLiveDays",
        new="return now.Sub(act.At) <= arcLiveDays*24*time.Hour",
        killer="TestARegistrationIsLiveExactlyWhileItReadsFourteenDaysAgo",
        extra_killers=("TestABulletDatedExactlyFourteenDaysAgoIsStillLive",
                       "TestLivenessDoesNotDependOnWhichSourceWon"),
        why="`now - last <= 14 days` is the obvious spelling, and it was this PR's first head: it hides "
        "whatever the row's own '14d ago' label calls fourteen days — a registration 14d23h old, a "
        "bullet dated today-14 at a noon clock (audit rounds 1 and 2).",
    ),
    Mutant(
        name="ui-arcs-index-member-walk-unrestricted",
        path="internal/report/arcsacross.go",
        old='index, err := store.LoadStore(storeRoot, "scanned", visible)',
        new='index, err := store.LoadStore(storeRoot, "scanned", store.Unrestricted())',
        killer="TestAMemberBulletInAnUnreadableScopeDoesNotMoveTheArc",
        extra_killers=("TestArcsAcrossListsReadableHomesWithTheirNewestReadableMemberBullet",),
        pkgs=PKGS + ("./internal/report/",),
        why="the arc is visible, so 'when did any of its members last write' looks like a fact about "
        "the ARC — but each write is in a scope with its own authority, and an unrestricted walk lets "
        "a hidden scope's activity reorder (and so reveal) what a reader sees.",
    ),
    Mutant(
        name="ui-arcs-index-lists-arcs-homed-in-an-unreadable-scope",
        path="internal/report/arcsacross.go",
        old="\t\tif !visible.Allows(reg.Home) {\n\t\t\tcontinue\n\t\t}\n",
        new="",
        killer="TestAnArcHomedInAnUnreadableScopeIsNeverListedEvenWhenItsMembersWroteWhereYouRead",
        extra_killers=("TestTheArcsPageListsLiveArcsNewestFirstAndCountsTheHidden",
                       "TestArcsAcrossListsReadableHomesWithTheirNewestReadableMemberBullet"),
        pkgs=PKGS + ("./internal/report/",),
        why="an index of EVERY arc is the first draft of an arcs index; the home rule (Q1) is the one "
        "thing that makes it a page about what this reader may see.",
    ),
    Mutant(
        name="ui-arc-tab-selection-ignored",
        path="internal/ui/arcs.go",
        old="\tif tab == TabSessions {\n\t\treturn h.Div(",
        new="\tif false && tab == TabSessions {\n\t\treturn h.Div(",
        killer="TestTheArcPageRendersOnlyTheSelectedTab",
        why="a tab whose link changes the URL and not the panel looks like a working tab on every "
        "page but its own.",
    ),
    Mutant(
        name="ui-arc-tab-unknown-value-passed-through",
        path="internal/ui/arcs.go",
        old='\tif raw == TabSessions {\n\t\treturn raw\n\t}\n\treturn ""',
        new='\tif raw != "" {\n\t\treturn raw\n\t}\n\treturn ""',
        killer="TestTheArcPageRendersOnlyTheSelectedTab",
        why="passing the operand through is the shortest fold, and it renders a page with NO current "
        "tab for every typo or scope-page tab name — a second URL for the default state.",
    ),
    # ---- presence (S2): the owner predicate, the target pick, the queue, the wall ---------
    Mutant(
        name="presence-predicate-ignores-narrowing",
        path="internal/presence/presence.go",
        old="if !viewer.Valid() || viewer.Auth.Narrowed() {",
        new="if !viewer.Valid() || false && viewer.Auth.Narrowed() {",
        killer="TestTheOwnerPredicateIsARelationship",
        extra_killers=("TestTheBearerNarrowingBitReachesThePredicate",),
        why="the bit is a fact about how the authority was PRODUCED, and a predicate that only "
        "compares owners reads a narrowed bearer credential of A as A — so a token narrowed to "
        "one scope sees where every one of A's sessions runs and can ring them.",
    ),
    Mutant(
        name="presence-predicate-compares-id-only",
        path="internal/presence/presence.go",
        old="if owner != OwnerOf(viewer.Principal) {",
        new="if owner.ID != viewer.Principal.ID {",
        killer="TestTheOwnerPredicateIsARelationship",
        why="the id is the obvious key and the kind looks redundant — until a project principal "
        "carries the same id string as a user and sees that user's panes.",
    ),
    Mutant(
        name="presence-predicate-ignores-owner",
        path="internal/presence/presence.go",
        old="if owner != OwnerOf(viewer.Principal) {",
        new="if false && owner != OwnerOf(viewer.Principal) {",
        killer="TestTheOwnerPredicateIsARelationship",
        extra_killers=("TestARingGoesThroughTheOwnerPredicate",),
        why="presence looks like session metadata, and session metadata is visible to anybody "
        "who can read the session — which is exactly what O3's owner-only rule refuses.",
    ),
    Mutant(
        name="presence-predicate-ignores-expiry",
        path="internal/presence/presence.go",
        old="return now.Before(expires)",
        new="return true || now.Before(expires)",
        killer="TestTheOwnerPredicateIsARelationship",
        why="whole-host replace already drops a closed pane, so the TTL looks redundant — until "
        "a host stops pushing at all and its last set is shown forever.",
    ),
    Mutant(
        name="presence-target-picks-oldest-activity",
        path="internal/presence/presence.go",
        old="return a.activity.After(b.activity)",
        new="return a.activity.Before(b.activity)",
        killer="TestTheTargetIsTheNewestActivityThenTheSmallerHost",
        extra_killers=("TestARingGoesThroughTheOwnerPredicate", "TestTheRingGoesToTheBadgesHost"),
        why="a comparator's direction is the single easiest thing to flip, and a ring aimed at the "
        "STALE host lights a window the operator is not looking for.",
    ),
    Mutant(
        name="presence-target-tie-goes-to-larger-host",
        path="internal/presence/presence.go",
        old="return a.Host < b.Host",
        new="return a.Host > b.Host",
        killer="TestTheTargetIsTheNewestActivityThenTheSmallerHost",
        extra_killers=("TestTheRingGoesToTheBadgesHost",),
        why="any tie-break is deterministic, so the wrong one looks as good as the right one — "
        "and the host side's expectation (decision 7) is the smaller label.",
    ),
    Mutant(
        name="presence-replace-merges-instead-of-replacing",
        path="internal/presence/presence.go",
        old="kept := append([]Row(nil), rows...)",
        new="kept := append(append([]Row(nil), s.hosts[hostKey{owner, host}].rows...), rows...)",
        killer="TestAPushReplacesItsHostsWholeSetAndNoOtherHosts",
        extra_killers=("TestAPushIsTheContractsReplace",),
        why="an upsert is the ordinary shape of a write, and it keeps a closed pane's badge until "
        "the TTL rather than until the next push.",
    ),
    Mutant(
        name="presence-ring-skips-the-predicate",
        path="internal/presence/queue.go",
        old="\tp, ok := s.Store.For(viewer, session)\n\tif !ok {",
        new="\tp, ok := s.Store.For(viewer, session)\n\tif false && !ok {",
        killer="TestARingGoesThroughTheOwnerPredicate",
        why="the ring button is only RENDERED where presence shows, so the handler looks guarded "
        "already — but a POST does not need the button.",
    ),
    Mutant(
        name="presence-queue-claim-ignores-owner",
        path="internal/presence/queue.go",
        old="if r.Owner != owner || r.Host != host {",
        new="if r.Host != host {",
        killer="TestTheQueueIsKeyedByOwnerAtUnitLevel",
        why="a claim token is bound to a host, so filtering on the host looks sufficient — and "
        "with the wall in place nothing end to end can show it is not.",
    ),
    Mutant(
        name="presence-queue-claim-ignores-host",
        path="internal/presence/queue.go",
        old="if r.Owner != owner || r.Host != host {",
        new="if r.Owner != owner {",
        killer="TestTheQueueIsKeyedByOwnerAtUnitLevel",
        extra_killers=("TestClaimsAreScopedToTheTokensHost",),
        why="one owner, so 'my rings' reads as the whole filter — and host-b's claim service "
        "swallows the ring meant for host-a's window.",
    ),
    Mutant(
        name="presence-wall-admits-a-foreign-owner",
        path="internal/presence/tokens.go",
        old="\tif row.Owner != owner {\n\t\treturn ErrForeignOwner",
        new="\tif false && row.Owner != owner {\n\t\treturn ErrForeignOwner",
        killer="TestTheWallRefusesAForeignRowAtStartupAndAsARowAfterwards",
        # The binary-level witness moved with the audit fix: a foreign row at startup no longer
        # exits the process, it leaves the agent listener unstarted.
        extra_killers=("TestATokenFileContentProblemLeavesTheBrowserServingAndTheAgentStopped",),
        why="the token file is the operator's own, so every row in it looks trustworthy — and a "
        "copied line makes a second owner's tokens authenticate on a personal instance.",
    ),
    Mutant(
        name="presence-wall-skipped-on-the-reread",
        path="internal/presence/agent.go",
        old="if err := admit(row, a.cfg.Owner); err != nil {",
        new="if err := admit(row, a.cfg.Owner); false && err != nil {",
        killer="TestTheWallRefusesAForeignRowAtStartupAndAsARowAfterwards",
        why="the startup read already refused foreign rows, so the per-request re-read looks like "
        "it only needs to parse — and a row added after startup walks straight past the wall.",
    ),
    Mutant(
        name="presence-wall-skipped-at-startup",
        path="internal/presence/tokens.go",
        old="\t\tif err := admit(r, owner); err != nil {",
        new="\t\tif err := admit(r, owner); false && err != nil {",
        killer="TestTheWallRefusesAForeignRowAtStartupAndAsARowAfterwards",
        extra_killers=("TestATokenFileContentProblemLeavesTheBrowserServingAndTheAgentStopped",),
        why="the per-request read refuses the row anyway, so the startup check looks redundant — "
        "but a copied manifest then starts a listener for somebody else's hosts.",
    ),
    Mutant(
        name="presence-token-file-content-takes-the-process-down",
        path="cmd/cairn-ui/main.go",
        old="\t\tcase errors.Is(err, presence.ErrTokenFileContent):\n",
        new="\t\tcase false && errors.Is(err, presence.ErrTokenFileContent):\n",
        killer="TestATokenFileContentProblemLeavesTheBrowserServingAndTheAgentStopped",
        why="every other startup error in this program exits 78, so one more looks consistent — and a "
        "single bad row in the agent's file takes the browser surface down for every user.",
    ),
    Mutant(
        name="presence-append-glues-onto-an-unterminated-line",
        path="internal/presence/tokens.go",
        old="len(existing) > 0 && existing[len(existing)-1] != '\\n' {",
        new="len(existing) > 0 && false && existing[len(existing)-1] != '\\n' {",
        killer="TestAMintIntoAFileWithoutATrailingNewlineKeepsBothRows",
        why="O_APPEND looks like all an append needs — until a hand-edited file without a final "
        "newline turns the next mint into one corrupt row and two dead tokens.",
    ),
    Mutant(
        name="presence-line-separators-accepted",
        path="internal/presence/wire.go",
        old="if unicode.IsControl(r) || r == '\\u2028' || r == '\\u2029' {",
        new="if unicode.IsControl(r) {",
        killer="TestPushBodyBoundsEachWithAJustUnderControl",
        why="`unicode.IsControl` reads as 'no line breaks', and U+2028/U+2029 are line breaks it "
        "does not cover.",
    ),
    Mutant(
        name="presence-imported-by-the-pod",
        path="internal/api/server.go",
        old='\t"github.com/ZacxDev/cairn/internal/netid"\n',
        new='\t"github.com/ZacxDev/cairn/internal/netid"\n\t_ "github.com/ZacxDev/cairn/internal/presence"\n',
        killer="TestOnlyTheBrowserProgramImportsPresence",
        why="the pod authenticates bearer tokens too, so teaching it presence tokens looks like reuse "
        "— and it is the edit that makes a presence token mean something outside the agent routes.",
    ),
    Mutant(
        name="presence-token-kind-ignored",
        path="internal/presence/agent.go",
        old="if hit && row.Kind == kind {",
        new="if hit {",
        killer="TestAPushTokenCannotClaimAndAClaimTokenCannotPush",
        why="one token file, one digest match — the kind column reads as a label rather than "
        "the capability split decision 3 makes it.",
    ),
    Mutant(
        name="presence-push-host-check-dropped",
        path="internal/presence/agent.go",
        old="if push.Host != row.Host {",
        new="if false && push.Host != row.Host {",
        killer="TestAPushNamingAnotherHostIs400AndWritesNothing",
        why="the store keys on the TOKEN's host anyway, so the body's host looks decorative — and "
        "a token file copied to the wrong machine silently reports that machine's panes as this one's.",
    ),
    Mutant(
        name="presence-lockout-never-recorded",
        path="internal/presence/agent.go",
        old="if a.cfg.Limiter.RecordFailure(client) {",
        new="if false && a.cfg.Limiter.RecordFailure(client) {",
        killer="TestFailedTokensLockTheClientOut",
        why="the refusal is already uniform, so counting it looks like logging — and a token "
        "guesser gets unlimited tries.",
    ),
    Mutant(
        name="presence-lockout-never-consulted",
        path="internal/presence/agent.go",
        old="if a.cfg.Limiter.LockedOut(client) {",
        new="if false && a.cfg.Limiter.LockedOut(client) {",
        killer="TestFailedTokensLockTheClientOut",
        why="recording failures with nothing reading them is a limiter in name only.",
    ),
    Mutant(
        name="presence-unknown-wire-fields-accepted",
        path="internal/presence/wire.go",
        old="\tdec.DisallowUnknownFields()\n",
        new="",
        killer="TestPushBodyBoundsEachWithAJustUnderControl",
        why="a decoder that ignores extra keys is Go's default, and it is how `pane_preview` — pane "
        "CONTENTS — would ride a push into this process unnoticed.",
    ),
    Mutant(
        name="presence-row-bound-off-by-one",
        path="internal/presence/wire.go",
        old="if len(*w.Rows) > MaxRows {",
        new="if len(*w.Rows) > MaxRows+1 {",
        killer="TestPushBodyBoundsEachWithAJustUnderControl",
        why="a bound tested only far outside itself survives an off-by-one at the edge.",
    ),
    Mutant(
        name="presence-string-bound-off-by-one",
        path="internal/presence/wire.go",
        old="if len(v) > MaxStringBytes {",
        new="if len(v) > MaxStringBytes+1 {",
        killer="TestPushBodyBoundsEachWithAJustUnderControl",
        why="the same edge, on every string field at once.",
    ),
    Mutant(
        name="presence-session-class-unchecked",
        path="internal/presence/wire.go",
        old="if !write.SessionComponent.MatchString(r.Session) {",
        new="if false && !write.SessionComponent.MatchString(r.Session) {",
        killer="TestPushBodyBoundsEachWithAJustUnderControl",
        why="presence only DISPLAYS the session id, so validating it looks like the write path's "
        "job — but it is the key every page joins presence on.",
    ),
    Mutant(
        name="presence-half-configuration-admitted",
        path="cmd/cairn-ui/presence.go",
        old="\tif len(missing) > 0 {\n",
        new="\tif false && len(missing) > 0 {\n",
        killer="TestThePresenceFlagsAreAllOrNoneAndNeverBlank",
        why="each flag fails later on its own when absent, so the all-or-none rule looks redundant "
        "— and the refusal an operator reads names the wrong cause.",
    ),
    Mutant(
        name="presence-owner-the-authority-lacks-admitted",
        path="cmd/cairn-ui/presence.go",
        old="if _, known := m.PrincipalFor(owner.Kind, owner.ID); !known {",
        new="if _, known := m.PrincipalFor(owner.Kind, owner.ID); false && !known {",
        killer="TestThePresenceFlagsAreAllOrNoneAndNeverBlank",
        why="a well-formed <kind>:<id> looks valid — and a typo'd id starts a listener whose pushes "
        "no page can ever show.",
    ),
    Mutant(
        name="presence-agent-bind-reachability-unchecked",
        path="cmd/cairn-ui/presence.go",
        old="if bindIsReachable(host) && proxyErr != nil {",
        new="if false && bindIsReachable(host) && proxyErr != nil {",
        killer="TestTheAgentBindGetsItsOwnReachabilityVerdict",
        extra_killers=("TestTheBinaryRefusesEachPresenceMisconfiguration",),
        why="the browser listener already passed this check, so a second listener looks covered — "
        "but a loopback browser bind says nothing about a 0.0.0.0 agent bind.",
    ),
    Mutant(
        name="presence-main-ignores-the-agent-bind-refusal",
        path="cmd/cairn-ui/main.go",
        old="if err := presenceBindRefusal(*presenceAddr, proxyErr); err != nil {",
        new="if err := presenceBindRefusal(*presenceAddr, proxyErr); false && err != nil {",
        killer="TestTheBinaryRefusesEachPresenceMisconfiguration",
        why="the predicate is tested in-process, so the call looks covered — whether `main` ACTS on "
        "it is wiring only a re-exec can see.",
    ),
    Mutant(
        name="presence-main-never-serves-the-agent-listener",
        path="cmd/cairn-ui/main.go",
        old="\tif agentServer != nil {\n\t\tgo func() {",
        new="\tif false && agentServer != nil {\n\t\tgo func() {",
        killer="TestTheAgentListenerExistsOnlyWhenConfigured",
        why="the listener is bound and announced, so the startup line looks right — and every push "
        "connects and then hangs.",
    ),
    Mutant(
        name="presence-mint-ignores-the-wall",
        path="cmd/cairn-ui/presence.go",
        old="if _, err := presence.LoadTokens(s.tokens, owner); err != nil {",
        new="if _, err := presence.LoadTokens(s.tokens, owner); false && err != nil {",
        killer="TestTheMintModeMintsOnceStoresTheDigestAndExits",
        why="the listener refuses a foreign row at its next start anyway — which is exactly the "
        "outage a mint that checked first would have prevented.",
    ),
    # ---- presence badges (S4): every surface asks `presence.Store.For` and nothing else ------
    Mutant(
        name="ui-presence-badge-ignores-the-predicate",
        path="internal/ui/presence.go",
        old="\tp, ok := l.at(session)\n\tif !ok {\n\t\treturn nil\n\t}",
        new="\tp, ok := l.at(session)\n\tif false && !ok {\n\t\treturn nil\n\t}",
        killer="TestPresenceIsInvisibleToEveryoneButItsOwner",
        why="rendering the badge from whatever the lookup returned is the shortest code, and the zero "
        "value it renders for a non-owner, a narrowed viewer or expired presence is still bytes — the "
        "page stops being the no-presence page for everybody.",
    ),
    Mutant(
        name="ui-presence-live-pane-ignores-the-predicate",
        path="internal/ui/presence.go",
        old="if _, ok := l.at(session); ok {",
        new="if _, ok := l.at(session); true || ok {",
        killer="TestPresenceIsInvisibleToEveryoneButItsOwner",
        why="\"is any member live\" reads like a membership question about the arc rather than an "
        "owner question about a pane, and answering it without the predicate tells every reader "
        "of a shared arc that its owner has a pane open.",
    ),
    Mutant(
        name="ui-presence-viewer-rebuilt-from-the-principal",
        path="internal/ui/presence.go",
        old="return store.For(id, session)",
        new="return store.For(identity.Identity{Principal: id.Principal}, session)",
        killer="TestPresenceIsInvisibleToEveryoneButItsOwner",
        why="the owner key is the principal, so passing just the principal looks equivalent — but the "
        "narrowing bit rides on `Auth`, and a viewer rebuilt without it reads a narrowed bearer "
        "credential as its owner.",
    ),
    Mutant(
        name="ui-presence-never-bound",
        path="internal/ui/presence.go",
        old="\tif s.presence == nil {\n\t\treturn nil\n\t}",
        new="\tif true || s.presence == nil {\n\t\treturn nil\n\t}",
        killer="TestTheBadgeSaysWhereTheSessionRuns",
        extra_killers=("TestPresenceIsInvisibleToEveryoneButItsOwner", "TestTwoLiveHostsShowTheTargetAndAlsoOn"),
        why="a surface that never renders presence satisfies every byte-identity assertion — the "
        "positive controls are what stop that, and this is the pre-S4 behaviour they must refuse.",
    ),
    Mutant(
        name="ui-presence-also-on-dropped",
        path="internal/ui/presence.go",
        old="g.If(len(p.AlsoOn) > 0,",
        new="g.If(false && len(p.AlsoOn) > 0,",
        killer="TestTwoLiveHostsShowTheTargetAndAlsoOn",
        why="the target row is the answer a ring is aimed at, so the second host looks like noise — "
        "and without it the operator cannot tell the session is open in two places.",
    ),
    Mutant(
        name="ui-presence-session-page-badge-dropped",
        path="internal/ui/sessionpage.go",
        old="g.If(pane != nil, h.Div(",
        new="g.If(false && pane != nil, h.Div(",
        killer="TestTheBadgeSaysWhereTheSessionRuns",
        why="the session page is the one surface the plan's e2e names; a summary card that forgot the "
        "badge would leave every byte-identity test green.",
    ),
    Mutant(
        name="ui-presence-session-row-badge-dropped",
        path="internal/ui/arcs.go",
        old="\t\tdateAgo(s.LastDate, now),\n\t\tpanes.badge(s.ID, now),",
        new="\t\tdateAgo(s.LastDate, now),\n\t\tnil,",
        killer="TestTheBadgeSaysWhereTheSessionRuns",
        why="the scope page's sessions tab is a second list of sessions, rendered by a second function; "
        "a surface the badge never reached is invisible to every byte-identity assertion.",
    ),
    Mutant(
        name="ui-presence-member-row-badge-dropped",
        path="internal/ui/arcs.go",
        old="\t\tinstantAgo(m.FirstSeen, now),\n\t\tpanes.badge(m.Session, now),",
        new="\t\tinstantAgo(m.FirstSeen, now),\n\t\tnil,",
        killer="TestTheBadgeSaysWhereTheSessionRuns",
        why="the arc page's sessions tab lists members through `memberRow`, not `sessionRow`, so "
        "badging one list does not badge the other.",
    ),
    Mutant(
        name="ui-presence-arc-summary-live-pane-dropped",
        path="internal/ui/arcs.go",
        old="v.Panes.liveBadge(memberIDs),",
        new="nil,",
        killer="TestTheBadgeSaysWhereTheSessionRuns",
        why="the arc page's summary is the only place its scopes tab says anything about presence.",
    ),
    Mutant(
        name="ui-presence-arcs-index-live-pane-dropped",
        path="internal/ui/arcsindex.go",
        old="panes.liveBadge(a.MemberSessions),",
        new="nil,",
        killer="TestTheBadgeSaysWhereTheSessionRuns",
        why="`/arcs` lists arcs, not sessions, so it is the surface most easily forgotten when "
        "\"session rows get a badge\" is the brief.",
    ),
    Mutant(
        name="ui-main-never-hands-presence-to-the-browser",
        path="cmd/cairn-ui/main.go",
        old="\t\t\tbrowserPresence = service\n",
        new="\t\t\t_ = browserPresence\n",
        killer="TestTheBrowserReadsTheStoreTheAgentListenerWrites",
        why="every `internal/ui` test hands a service in itself, so the one line that connects the "
        "listener's store to the browser is visible only to a test that drives `main`.",
    ),
    # ---- the bell (S5): `POST /ring`, the queue behind it, and the button ---------------------
    Mutant(
        name="ui-ring-row-declared-public",
        path="internal/ui/routes.go",
        old='{"POST", "/ring"}:     {(*Server).handleRing, 0},',
        new='{"POST", "/ring"}:     {(*Server).handleRing, classPublic},',
        killer="TestTheRingRowIsBehindBothCrossSiteGates",
        extra_killers=("TestTheRouteLedgerMatchesTheDispatchTable",),
        why="a ring reads like a harmless nudge, and a class is a one-word edit — but a public row "
        "dispatches ahead of the chain and so ahead of the CSRF gate, which is the bypass the S5 "
        "plan names: a future class quietly exempting a state-changing row.",
    ),
    Mutant(
        name="ui-ring-never-asks-presence",
        path="internal/ui/bell.go",
        old="if s.presence != nil && write.SessionComponent.MatchString(session) {",
        new="if false && s.presence != nil && write.SessionComponent.MatchString(session) {",
        killer="TestEveryRingAnswerIsTheSameRedirect",
        extra_killers=(
            "TestTheRingRowIsBehindBothCrossSiteGates",
            "TestARepeatWhilePendingQueuesNoSecondRing",
            "TestARungRingLivesSixtySeconds",
            "TestTheRingGoesToTheBadgesHost",
        ),
        why="every answer is the same 303, so a handler that never queued anything passes every "
        "check that reads only the response — the owner's queued ring is the positive control.",
    ),
    Mutant(
        name="ui-ring-answers-a-queued-ring-differently",
        path="internal/ui/bell.go",
        old="\t\tif _, err := s.presence.Ring(id, session); err != nil {",
        new="\t\tif ok, err := s.presence.Ring(id, session); ok {\n"
        "\t\t\thttp.Redirect(w, r, sessionHref(session)+\"&rang=1\", http.StatusSeeOther)\n"
        "\t\t\treturn\n"
        "\t\t} else if err != nil {",
        killer="TestEveryRingAnswerIsTheSameRedirect",
        why="telling the owner \"rang\" is the obvious UX — and it makes the answer depend on whether "
        "a live pane exists for this viewer, which is the one thing a ring must not say.",
    ),
    Mutant(
        name="presence-ring-reads-the-store-without-the-predicate",
        path="internal/presence/queue.go",
        old="\tp, ok := s.Store.For(viewer, session)\n",
        new="\tvar p Presence\n\tok := false\n\ts.Store.mu.Lock()\n"
        "\tfor key, set := range s.Store.hosts {\n\t\tfor _, r := range set.rows {\n"
        "\t\t\tif r.Session == session {\n"
        "\t\t\t\tp, ok = Presence{Target: Located{Row: r, Owner: key.owner, Host: key.host}}, true\n"
        "\t\t\t}\n\t\t}\n\t}\n\ts.Store.mu.Unlock()\n\t_ = viewer\n",
        killer="TestEveryRingAnswerIsTheSameRedirect",
        extra_killers=("TestARingGoesThroughTheOwnerPredicate",),
        why="the ring only needs the TARGET row's host, and reading it straight out of the table is "
        "shorter than asking on the viewer's behalf — it queues a ring at the OWNER's pane for any "
        "caller who can name the session. `presence-ring-skips-the-predicate` files a refused ring "
        "under the zero owner, which no claim can see; this one files it where the owner's host will.",
    ),
    Mutant(
        name="presence-queue-a-repeat-replaces-the-pending-ring",
        path="internal/presence/queue.go",
        old="if existing, ok := q.pending[key]; ok {",
        new="if existing, ok := q.pending[key]; false && ok {",
        killer="TestARepeatWhilePendingQueuesNoSecondRing",
        extra_killers=("TestOnePendingRingPerSessionAndItExpires",),
        why="the map key already holds one ring per (owner, session), so the dedupe branch looks "
        "redundant — but without it each click REPLACES the pending ring with a fresh one, and a "
        "ring stays pending past 60 s for as long as somebody keeps clicking.",
    ),
    Mutant(
        name="presence-ring-ttl-too-long",
        path="internal/presence/queue.go",
        old="const DefaultRingTTL = 60 * time.Second",
        new="const DefaultRingTTL = 62 * time.Second",
        killer="TestARungRingLivesSixtySeconds",
        extra_killers=("TestARepeatWhilePendingQueuesNoSecondRing",),
        why="a round number nearby reads as the same bound — and a ring a host claims late lights a "
        "window the operator stopped looking for. ⚠ `TestOnePendingRingPerSessionAndItExpires` does "
        "NOT kill this: it reads its expiry instant off `DefaultRingTTL` itself, so it moves with the "
        "mutant — measured, it stayed green. The route test's literal 61 s is what pins the bound.",
    ),
    Mutant(
        name="presence-ring-ttl-too-short",
        path="internal/presence/queue.go",
        old="const DefaultRingTTL = 60 * time.Second",
        new="const DefaultRingTTL = 58 * time.Second",
        killer="TestARungRingLivesSixtySeconds",
        extra_killers=("TestOnePendingRingPerSessionAndItExpires",),
        why="the other side of the same bound: a ring dropped before a ~5 s claim loop on a slow "
        "host gets to it is a click that silently does nothing.",
    ),
    Mutant(
        name="ui-bell-rendered-without-the-badge",
        path="internal/ui/sessionpage.go",
        old='h.Data("presence", "session"), pane,\n\t\t\t\tg.If(v.CSRF != "", bellForm(rep.ID, v.CSRF)))),',
        new='h.Data("presence", "session"), pane)),\n\t\t\tg.If(v.CSRF != "", bellForm(rep.ID, v.CSRF)),',
        killer="TestTheBellRendersOnlyBesideTheOwnersBadge",
        why="the button is harmless without presence (a ring queues nothing), so placing it beside the "
        "badge rather than inside its condition looks equivalent — and it tells every viewer of every "
        "session page that presence is switched on, breaking decision 5's byte-identity.",
    ),
    Mutant(
        name="ui-bell-container-is-a-paragraph",
        path="internal/ui/sessionpage.go",
        old='h.Div(h.Class("card-stats"), h.Data("presence", "session"), pane,',
        new='h.P(h.Class("card-stats"), h.Data("presence", "session"), pane,',
        killer="TestTheBellRendersOnlyBesideTheOwnersBadge",
        why="every other stats row on these pages is a `<p class=\"card-stats\">`, so a paragraph reads "
        "as the house shape — and a `<form>` start tag closes an open `<p>`, so a parser moves the bell "
        "out of the badge's container and leaves a stray empty paragraph. This shipped in the first S5 "
        "commit and was found by audit, not by the string-offset test that stood here.",
    ),
    Mutant(
        name="ui-bell-never-rendered",
        path="internal/ui/sessionpage.go",
        old='g.If(v.CSRF != "", bellForm(rep.ID, v.CSRF))',
        new='g.If(false && v.CSRF != "", bellForm(rep.ID, v.CSRF))',
        killer="TestTheBellRendersOnlyBesideTheOwnersBadge",
        why="a page that never renders the bell satisfies every byte-identity assertion; the owner's "
        "form is the positive control that refuses it.",
    ),
    Mutant(
        name="ui-bell-rendered-without-a-token",
        path="internal/ui/sessionpage.go",
        old='g.If(v.CSRF != "", bellForm(rep.ID, v.CSRF))',
        new='g.If(true, bellForm(rep.ID, v.CSRF))',
        killer="TestTheBellRendersOnlyBesideTheOwnersBadge",
        why="the badge is already conditional, so a second condition looks redundant — but a form "
        "with an empty token is a control that answers 403 every time it is pressed.",
    ),
    # ---- the installable surface (S2 of the mobile plan): manifest, icons, `pwaHead`, the flags ------
    Mutant(
        name="ui-manifest-row-requires-auth",
        path="internal/ui/routes.go",
        old='{"GET", ManifestPath}: {(*Server).handleManifest, classPublic},',
        new='{"GET", ManifestPath}: {(*Server).handleManifest, 0},',
        killer="TestTheManifestAnswersAnAnonymousCaller",
        extra_killers=("TestTheRouteLedgerMatchesTheDispatchTable",),
        why="every other row that renders configuration sits behind the chain, so dropping the class reads "
        "as tightening — and it leaves the sign-in page, where an install starts, linking a manifest the "
        "browser fetches WITHOUT credentials and is refused.",
    ),
    Mutant(
        name="ui-manifest-name-is-a-constant",
        path="internal/ui/pwa.go",
        old="Name: a.Name, ShortName: a.ShortName,",
        new='Name: "cairn", ShortName: a.ShortName,',
        killer="TestTheManifestIsBuiltFromTheConfiguredApp",
        why="the product IS called cairn, so a literal looks right on every single-instance check — and "
        "installs both instances under one title, which is the confusion the flag exists to prevent.",
    ),
    Mutant(
        name="ui-manifest-served-when-unarmed",
        path="internal/ui/pwa.go",
        old="\tif !s.app.Armed() {\n\t\twritePlain(w, http.StatusNotFound, noSuchRoute)",
        new="\tif false {\n\t\twritePlain(w, http.StatusNotFound, noSuchRoute)",
        killer="TestAnUnarmedServerServesNoManifestAndNoPWAHead",
        extra_killers=("TestEveryServedPathComesFromTheLedger",),
        why="a manifest with an empty name still parses, so serving one unconditionally looks harmless — "
        "and makes a deployment that never opted in advertise an installable app with no name.",
    ),
    Mutant(
        name="ui-pwa-head-missing-from-sign-in",
        path="internal/ui/render.go",
        old='Title:    documentTitle(app, "sign in"),\n\t\tLanguage: "en",\n\t\tHead:     []g.Node{stylesheetLink(), pwaHead(app)},',
        new='Title:    documentTitle(app, "sign in"),\n\t\tLanguage: "en",\n\t\tHead:     []g.Node{stylesheetLink()},',
        killer="TestEveryFrameCallsPWAHead",
        extra_killers=("TestEveryArmedHTMLPageCarriesThePWAHead",),
        why="the public frames build their own `c.HTML5` on purpose, so a head element added to `shell` "
        "silently misses them — and the sign-in page is the one an installed app opens first.",
    ),
    # 🔴 RE-POINTED, NOT DELETED, AT S4: it was `ui-pwa-head-emits-a-script-before-s4`, guarding S2's "no
    # script yet". S4 lands the real tag, so the plausible slip now is the UNVERSIONED spelling of it —
    # `/static/pwa.js`, the path a service-worker-shaped draft would want — instead of the hashed row.
    Mutant(
        name="ui-pwa-head-emits-an-unversioned-script",
        path="internal/ui/pwa.go",
        old="\t\th.Link(h.Rel(\"apple-touch-icon\"), h.Href(apple.Path)),\n\t\tpwaScriptTag(),\n\t})",
        new="\t\th.Link(h.Rel(\"apple-touch-icon\"), h.Href(apple.Path)),\n"
        "\t\th.Script(h.Src(\"/static/pwa.js\"), h.Defer()),\n\t})",
        killer="TestTheArmedPWAHeadAddsOnlyThePWAScript",
        why="`/static/pwa.js` is the obvious spelling of the script's URL and the one every PWA tutorial "
        "uses — but it is no row here, so every armed page would carry a 404ing script outside the allowlist.",
    ),
    Mutant(
        name="ui-app-name-blank-accepted",
        path="cmd/cairn-ui/app.go",
        old="range []appLine{name, short, variant} {\n\t\tif l.written && identity.ValueReducesToNothing(l.value) {",
        new="range []appLine{name, short, variant} {\n\t\tif false && l.written && identity.ValueReducesToNothing(l.value) {",
        killer="TestEachAppLineIsJudgedWithItsOwnRefusal",
        extra_killers=("TestTheBinaryRefusesEachAppMisconfiguration",),
        why="`Validate` already refuses an incomplete app, so a separate blank check looks redundant — "
        "but a whitespace name is ARMED to `Validate` and installs a nameless app.",
    ),
    Mutant(
        name="ui-icon-variant-optional-when-armed",
        path="internal/ui/pwa.go",
        old='\tif a.IconVariant == "" {\n\t\treturn ErrAppNoVariant',
        new='\tif false {\n\t\treturn ErrAppNoVariant',
        killer="TestAppValidateRefusesEachShape",
        extra_killers=("TestEachAppLineIsJudgedWithItsOwnRefusal", "TestTheBinaryRefusesEachAppMisconfiguration"),
        why="the set check below also refuses \"\", so this branch looks dead — but its sentinel is what "
        "lets the operator be told WHICH flag is missing rather than that \"\" is not a variant.",
    ),
    Mutant(
        name="ui-icon-variant-outside-the-set-accepted",
        path="internal/ui/pwa.go",
        old="\tif !slices.Contains(IconVariants(), a.IconVariant) {",
        new="\tif false && !slices.Contains(IconVariants(), a.IconVariant) {",
        killer="TestAppValidateRefusesEachShape",
        extra_killers=("TestEachAppLineIsJudgedWithItsOwnRefusal", "TestTheBinaryRefusesEachAppMisconfiguration"),
        why="a typo'd variant then starts, links icons at empty paths, and installs with no picture — a "
        "failure discovered on a phone instead of at startup.",
    ),
    Mutant(
        name="ui-manifest-links-every-variant",
        path="internal/ui/pwa.go",
        old='if f.Variant != a.IconVariant || f.Kind.ManifestPurpose == "" {',
        new='if f.Kind.ManifestPurpose == "" {',
        killer="TestTheManifestIsBuiltFromTheConfiguredApp",
        why="every variant is a served row, so listing every row's icon looks like completeness — and a "
        "browser may then pick another instance's picture.",
    ),
    # S4 of the mobile plan: `pwa.js`, its allowlist entry and spelling guard, and the screenshots.
    Mutant(
        name="ui-pwa-script-not-allowlisted",
        path="internal/ui/script.go",
        old="\treturn []string{FilterScriptPath, PWAScriptPath, JoinScriptPath}\n",
        new="\treturn []string{FilterScriptPath, JoinScriptPath}\n",
        killer="TestTheArmedPWAHeadAddsOnlyThePWAScript",
        why="the tag is emitted by `pwaHead`, so the page 'works' without the list knowing — and `uiaudit`'s "
        "walk boots UNARMED, so only the armed render guard would notice a script the allowlist never admitted.",
    ),
    Mutant(
        name="ui-pwa-script-uses-innerhtml",
        path="internal/ui/pwa.js",
        old="      deferred = event;\n      button.hidden = false;\n",
        new="      deferred = event;\n      button.innerHTML = \"Install <b>cairn</b>\";\n      button.hidden = false;\n",
        killer="TestThePWAScriptTouchesOnlyWhatItSays",
        why="a richer label is the first 'improvement' anyone makes to an install button, and `innerHTML` is "
        "the one-line way to get it — the sink the escaping story says no script here uses.",
    ),
    Mutant(
        name="ui-pwa-script-writes-a-second-storage-key",
        path="internal/ui/pwa.js",
        old="      window.localStorage.setItem(HINT_KEY, \"1\");\n",
        new="      window.localStorage.setItem(HINT_KEY, \"1\");\n"
        "      window.localStorage.setItem(HINT_KEY + \"At\", String(Date.now()));\n",
        killer="TestThePWAScriptTouchesOnlyWhatItSays",
        why="'when was it dismissed' is the obvious next feature (re-show after a month), and it puts a "
        "timestamp on the device — the second client-side write O8 and T9 say does not exist.",
    ),
    Mutant(
        name="ui-pwa-script-registers-a-service-worker",
        path="internal/ui/pwa.js",
        old="  var HINT_KEY = \"cairn.installHintDismissed\";\n",
        new="  var HINT_KEY = \"cairn.installHintDismissed\";\n"
        "  if (navigator.serviceWorker) { navigator.serviceWorker.register(\"/sw.js\"); }\n",
        killer="TestThePWAScriptTouchesOnlyWhatItSays",
        why="every PWA guide's first step is registering a worker, and 'installable' reads as needing one — "
        "O13 decided v1 has none, and this is the line that would quietly reverse it.",
    ),
    Mutant(
        name="ui-manifest-screenshot-not-the-committed-file",
        path="internal/ui/pwa.go",
        old="\t\troutes[routeKey{http.MethodGet, f.Path}] = route{screenshotHandler(f), classPublic}\n",
        new="\t\tf.Bytes = append(append([]byte{}, f.Bytes...), 'x')\n"
        "\t\troutes[routeKey{http.MethodGet, f.Path}] = route{screenshotHandler(f), classPublic}\n",
        killer="TestTheScreenshotSetIsExactlyTheCommittedFiles",
        why="a served screenshot that is not the committed file is a picture no derivation pinned — the "
        "provenance argument (leakscan skips PNGs) holds only while the row serves exactly those bytes.",
    ),
    # S3 of the mobile plan: decision 8, clause (d).
    Mutant(
        name="ui-html-no-store-dropped",
        path="internal/ui/server.go",
        old="\tw.Header().Set(\"Cache-Control\", htmlCacheControl)\n",
        new="\tw.Header().Set(\"Cache-Control\", \"no-cache\")\n",
        killer="TestEveryNonPublicHTMLRowIsNoStore",
        extra_killers=("TestTheMintedTeamLinkIsRenderedOnceAsTextAndNeverLogged",),
        why="`no-cache` reads as the cautious value and is not: it permits STORING the page and only "
        "asks for revalidation, so every authenticated page would sit in the device's HTTP cache — the "
        "team-link mint, whose body is a bearer capability, included.",
    ),
    # 🔴 THE HUB AND THE SESSIONS LIST. Every hub count and every listed session comes out of a read
    # narrowed by `auth.NamedScopes(VerbRead)`; each row below widens ONE of those reads (by the
    # exported `Unrestricted` field, so the mutated file needs no new import) or skips the root's 303.
    Mutant(
        name="ui-sessions-list-walks-every-scope",
        path="internal/ui/hub.go",
        old="\tvisible := scopeSetOf(auth.NamedScopes(control.VerbRead))\n\tsnap, unreadable, err := s.arcSnapshotOrUnreadable()",
        new="\tvisible := scopeSetOf(auth.NamedScopes(control.VerbRead))\n\tvisible.Unrestricted = true\n"
        "\tsnap, unreadable, err := s.arcSnapshotOrUnreadable()",
        killer="TestTheSessionsPageListsExactlyTheSessionsWhosePageIsFound",
        why="a list of 'every session' reads as a list over the STORE, and a session id is global — so "
        "the unnarrowed walk is the natural first draft. It lists sessions that wrote only where the "
        "viewer cannot read, each of which then 404s on its own page.",
    ),
    # 🔴 RE-POINTED, NOT DELETED, when the hub's arcs count was dropped (review round 0, D1): the read it
    # widens is `StoreSource.Arcs`, which the `/arcs` page still renders from, and that page's own guard
    # was already this row's extra killer. It now names that guard.
    Mutant(
        name="ui-arcs-index-read-includes-unreadable-homes",
        path="internal/ui/arcsindex.go",
        old="\tvisible := scopeSetOf(auth.NamedScopes(control.VerbRead))\n\tsnap, err := s.arcSnapshot()\n"
        "\tif err != nil {\n\t\treturn report.ArcsAcrossReport{}, err",
        new="\tvisible := scopeSetOf(auth.NamedScopes(control.VerbRead))\n\tvisible.Unrestricted = true\n"
        "\tsnap, err := s.arcSnapshot()\n\tif err != nil {\n\t\treturn report.ArcsAcrossReport{}, err",
        killer="TestAnArcHomedInAnUnreadableScopeIsNeverListedEvenWhenItsMembersWroteWhereYouRead",
        why="the arcs page lists arcs, and an arc 'belongs' to everybody who worked on it, so the "
        "unnarrowed journal read looks natural — but an arc exists for a caller only if its HOME is "
        "readable (Q1), and the unnarrowed read lists arcs homed where the viewer cannot read.",
    ),
    Mutant(
        name="ui-root-scope-list-query-not-redirected",
        path="internal/ui/hub.go",
        old="\tif redirectsToScopes(r) {",
        new="\tif false && redirectsToScopes(r) {",
        killer="TestTheOldScopeListQueriesRedirectToScopes",
        why="the hub ignores `?q=` and `?tag=` and still answers 200, so every bookmark and the Search "
        "shortcut look like they work — they land on the hub with the search silently dropped.",
    ),
    # 🔴 THE SCOPE PAGE'S "WHAT AN AGENT SEES" TAB: the recall read's own narrowing, the tab being
    # recognised at all, and the two properties its byte-equality test pins (the bytes, the cut).
    Mutant(
        name="ui-agent-recall-read-widened",
        path="internal/ui/agent.go",
        old="\tvisible := scopeSetOf(auth.NamedScopes(control.VerbRead))\n\topts := report.RecallOptions{",
        new="\tvisible := scopeSetOf(auth.NamedScopes(control.VerbRead))\n\tvisible.Unrestricted = true\n"
        "\topts := report.RecallOptions{",
        killer="TestTheRecallReadIsNarrowedByTheViewersAuthority",
        why="the page already refused an unreadable scope, so narrowing the recall again reads as "
        "redundant — and `cairn recall` itself runs unrestricted over its cache. Here there is no cache "
        "the pod narrowed first: an unrestricted read serves any scope NAME that reaches it.",
    ),
    Mutant(
        name="ui-agent-tab-not-recognised",
        path="internal/ui/render.go",
        old="\tcase TabSessions, TabArcs, TabAgent, TabMemos:",
        new="\tcase TabSessions, TabArcs, TabMemos:",
        killer="TestTheAgentTabIsByteForByteTheCLIRecall",
        why="an unrecognised tab silently renders the entries tab (the house ruling), so a tab link "
        "that lost its value still answers 200 with a page — just not the one it names.",
    ),
    Mutant(
        name="ui-agent-recall-rendered-under-another-label",
        path="internal/ui/agent.go",
        old='return AgentRecall{Text: rep.RenderText(s.host(), nil, "")}, nil',
        new='return AgentRecall{Text: rep.RenderText(s.host(), nil, "pod")}, nil',
        killer="TestTheAgentTabIsByteForByteTheCLIRecall",
        why="naming the instance the text was read from looks like helpful provenance, and it adds a "
        "clause to the caveat an agent on a single-instance host never sees — the tab stops being the "
        "agent's bytes.",
    ),
    Mutant(
        name="ui-agent-head-mark-ignores-the-banner",
        path="internal/ui/agent.go",
        old="\tcut := agentHeadLines - agentBannerLines\n",
        new="\tcut := agentHeadLines\n",
        killer="TestTheAgentTabIsByteForByteTheCLIRecall",
        why="`head -60` reads as 'line 60 of this text', but the client prints a banner and a blank line "
        "first, so the agent's cut falls two lines earlier than the obvious mark.",
    ),
    # S1 of the scope-refs plan (`claudedocs/plan-cairn-scope-refs.md`): `internal/codesrc`, the
    # code-source grammar and its journal. 🔴 EVERY ROW CARRIES `pkgs` RATHER THAN `PKGS` GROWING:
    # the killers live in `internal/codesrc` alone, and adding it to `PKGS` would re-scope every
    # other row (the field's own comment measures why that turns correct rows MISATTRIBUTED).
    Mutant(
        name="codesrc-accepts-a-non-dns-host",
        path="internal/codesrc/codesrc.go",
        old="\tif !isDNSName(host) {",
        new="\tif false && !isDNSName(host) {",
        killer="TestEachRefusalIsForItsOwnRule",
        pkgs=PKGS + ("./internal/codesrc/",),
        why="a host check that trusts whatever sits before the first '/' — which makes a forgotten host "
        "(`git:example-org/example-repo@main`) a declaration of a host called `example-org`.",
    ),
    Mutant(
        name="codesrc-branch-may-lead-with-a-dash",
        path="internal/codesrc/codesrc.go",
        old='\tif strings.HasPrefix(branch, "-") {',
        new='\tif false && strings.HasPrefix(branch, "-") {',
        killer="TestEachRefusalIsForItsOwnRule",
        pkgs=PKGS + ("./internal/codesrc/",),
        why="the ref-format rule written out from git-check-ref-format(1) has no leading-dash clause — "
        "only `--branch` adds it — so dropping the explicit check is invisible to a reader, and a branch "
        "of `-…` is an option to the auditor's `git` (T2).",
    ),
    Mutant(
        name="codesrc-whitespace-in-branch-accepted",
        path="internal/codesrc/codesrc.go",
        old="\t\tif unicode.IsSpace(r) {\n\t\t\treturn refuse(RuleWhitespace)",
        new="\t\tif false && unicode.IsSpace(r) {\n\t\t\treturn refuse(RuleWhitespace)",
        killer="TestEachRefusalIsForItsOwnRule",
        pkgs=PKGS + ("./internal/codesrc/",),
        why="the branch rule refuses a space anyway, so the whitespace check reads as redundant — and "
        "deleting it turns the form's one actionable message into a ref-format one.",
    ),
    Mutant(
        name="codesrc-repo-path-case-folded",
        path="internal/codesrc/codesrc.go",
        old='\tsrc := Source{Host: host, RepoPath: strings.Join(segs, "/"), Branch: branch}',
        new='\tsrc := Source{Host: host, RepoPath: strings.ToLower(strings.Join(segs, "/")), Branch: branch}',
        killer="TestEveryExampleParsesToItsLiteralCanonicalForm",
        pkgs=PKGS + ("./internal/codesrc/",),
        why="the host IS lowercased two lines up, so lowercasing the path beside it looks like "
        "consistency — and a self-hosted forge with case-sensitive paths then names another repository.",
    ),
    Mutant(
        name="codesrc-subpath-dotdot-accepted",
        path="internal/codesrc/codesrc.go",
        old='\t\t\tif s == ".." {\n\t\t\t\treturn refuse(RuleDotDot)',
        new='\t\t\tif false && s == ".." {\n\t\t\t\treturn refuse(RuleDotDot)',
        killer="TestEachRefusalIsForItsOwnRule",
        pkgs=PKGS + ("./internal/codesrc/",),
        why="the segment rule refuses a leading '.', so the '..' check looks dead; it is what names the "
        "traversal rather than reporting it as a spelling problem.",
    ),
    Mutant(
        name="codesrc-duplicate-refused-not-deduped",
        path="internal/codesrc/codesrc.go",
        old="\t\tif seen[c] {\n\t\t\tcontinue\n\t\t}",
        new='\t\tif seen[c] {\n\t\t\treturn nil, &ParseError{Index: i + 1, Input: raw, Rule: "is a duplicate"}\n\t\t}',
        killer="TestDuplicatesAreDroppedAndOrderIsKept",
        pkgs=PKGS + ("./internal/codesrc/",),
        why="refusing a duplicate reads as the stricter choice; it refuses a pasted list for a line that "
        "two spellings (`.git`, host case) make identical, which the user cannot see.",
    ),
    Mutant(
        name="codesrc-order-not-kept",
        path="internal/codesrc/codesrc.go",
        old="\tif len(out) > MaxSources {",
        new="\tfor i := 1; i < len(out); i++ {\n\t\tfor k := i; k > 0 && out[k].Canonical() < out[k-1].Canonical(); k-- {\n"
        "\t\t\tout[k], out[k-1] = out[k-1], out[k]\n\t\t}\n\t}\n\tif len(out) > MaxSources {",
        killer="TestDuplicatesAreDroppedAndOrderIsKept",
        pkgs=PKGS + ("./internal/codesrc/",),
        why="sorting for a deterministic answer — and the first source, the PRIMARY, becomes whichever "
        "sorts first.",
    ),
    Mutant(
        name="codesrc-revision-compared-outside-the-lock",
        path="internal/codesrc/journal.go",
        # ONE pattern spanning the lock and the compare, so the compare MOVES rather than being
        # duplicated — and the `interleave` seam moves WITH it, because the seam marks the
        # compare-to-append window wherever that window is. That is what lets call two REACH the
        # seam before call one appends; call one waits for it with a 300ms DEADLINE. ⚠ So the kill
        # still depends on that deadline — call two must arrive within it (a 1µs deadline left this
        # row SURVIVED 20 of 20). Too short a deadline fails LOUD, as a SURVIVED row here, never as
        # a false green on the real code, where call two is parked on `flock`.
        old=_CODESRC_LOCKED_SPAN,
        new="\tif pre, perr := j.Read(); perr == nil {\n"
        "\t\tif cur := pre.RevisionFor(scope); cur != ifRevision {\n"
        "\t\t\treturn Record{}, &StaleRevisionError{Scope: scope, Want: ifRevision, Current: cur}\n"
        "\t\t}\n\t}\n"
        "\tif interleave != nil {\n\t\tinterleave()\n\t}\n"
        + _CODESRC_LOCKED_SPAN.replace(_CODESRC_COMPARE_AND_SEAM, ""),
        killer="TestTwoWritesCarryingOneRevisionLandExactlyOnce",
        pkgs=PKGS + ("./internal/codesrc/",),
        why="checking the precondition before taking the lock is the natural order to write it in "
        "(validate, then act) — and it is the lost update the revision exists to prevent.",
    ),
    Mutant(
        name="codesrc-stale-revision-accepted",
        path="internal/codesrc/journal.go",
        old="\tif current != ifRevision {",
        new="\tif false && current != ifRevision {",
        killer="TestAStaleRevisionWritesNothing",
        extra_killers=("TestTwoWritesCarryingOneRevisionLandExactlyOnce",),
        pkgs=PKGS + ("./internal/codesrc/",),
        why="a revision that is carried, stored and compared nowhere — the form still round-trips it, so "
        "nothing reads wrong until two admins edit at once.",
    ),
    Mutant(
        name="codesrc-fold-earliest-wins",
        path="internal/codesrc/journal.go",
        old="\t\tsnap.Latest[r.Scope] = r\n",
        new="\t\tif _, dup := snap.Latest[r.Scope]; !dup {\n\t\t\tsnap.Latest[r.Scope] = r\n\t\t}\n",
        killer="TestTheFoldIsLatestWins",
        pkgs=PKGS + ("./internal/codesrc/",),
        why="'first write wins' is a common dedupe idiom; on an append-only journal it freezes every "
        "scope at its first declaration.",
    ),
    Mutant(
        name="codesrc-damaged-line-refuses-whole-journal",
        path="internal/codesrc/journal.go",
        old="\t\tif err := dec.Decode(&r); err != nil || !r.valid() {\n\t\t\tsnap.Skipped++\n\t\t\tcontinue\n\t\t}",
        new="\t\tif err := dec.Decode(&r); err != nil || !r.valid() {\n"
        "\t\t\treturn Snapshot{Latest: map[string]Record{}, Skipped: 1}\n\t\t}",
        killer="TestDamagedLinesAreSkippedAndCounted",
        pkgs=PKGS + ("./internal/codesrc/",),
        why="refusing on corruption is the control journal's rule (refuse-whole), so copying it here "
        "looks principled — and one hand-edited line makes every scope undeclared.",
    ),
    Mutant(
        name="codesrc-unknown-field-refused",
        path="internal/codesrc/journal.go",
        old="\t\tdec := json.NewDecoder(bytes.NewReader(line))\n\t\tif err := dec.Decode(&r)",
        new="\t\tdec := json.NewDecoder(bytes.NewReader(line))\n\t\tdec.DisallowUnknownFields()\n\t\tif err := dec.Decode(&r)",
        killer="TestAnUnknownFieldIsIgnoredNotRefused",
        pkgs=PKGS + ("./internal/codesrc/",),
        why="arcs' fold, which this one is copied from, DOES disallow unknown fields — the one line a "
        "faithful copy carries over, and a newer UI's record then reads as damaged on an older pod.",
    ),
    Mutant(
        name="codesrc-key-not-normalised",
        path="internal/codesrc/codesrc.go",
        old="\treturn store.NormalizeRef(scopeName)",
        new="\t_ = store.NormalizeRef\n\treturn scopeName",
        killer="TestKeyFoldsTheScopeNameToOneLiteral",
        extra_killers=("TestTheKeyIsTheFoldedName",),
        pkgs=PKGS + ("./internal/codesrc/",),
        why="the directory name already IS the scope name on most hosts, so the identity function passes "
        "every lowercase fixture — and `Alpha-Notes` declared in the browser is never found by the pod.",
    ),
    # S2 of the scope-refs plan: the pod's `sources/<scope>` route and its startup refusal. All
    # three killers live inside `PKGS` (`internal/api`, `cmd/cairn-server`), so no `pkgs` override.
    Mutant(
        name="api-sources-get-distinguishes-absent-from-unreadable",
        path="internal/report/sources.go",
        old='\tindex, err := store.LoadStore(storeRoot, "scanned", visible)',
        new='\tindex, err := store.LoadStore(storeRoot, "scanned", store.Unrestricted())',
        killer="TestAScopeTheCallerCannotReadAnswersExactlyLikeAbsence",
        why="'does the scope exist' reads as a question about the DISK, so asking it of the whole store "
        "looks correct — and a scope the caller may not read then answers with its declaration, telling "
        "refused apart from absent and leaking the repositories it names.",
    ),
    Mutant(
        name="api-sources-journal-inside-store-accepted",
        # Re-pointed when S2's round 1 moved the variable's reading out of `main` into
        # `codesrc.FromEnv`, its ONE reader: the old pattern named a line `main.go` no longer has.
        # The killer still reaches it — the test runs the BINARY, and `main` calls `FromEnv`.
        path="internal/codesrc/journal.go",
        old="\tresolved, err := ResolveJournalPath(storeRoot, v)",
        new="\tresolved, err := v, error(nil)",
        killer="TestTheBinaryREFUSESASourceJournalInsideTheStoreRoot",
        why="the variable is 'just a path', and the pod only READS it, so checking where it points looks "
        "like ceremony — and a journal beside the store becomes a scope every bare row reads, and is "
        "overwritten by the next re-seed.",
    ),
    Mutant(
        name="api-sources-unreadable-journal-answers-200",
        path="internal/api/server.go",
        old="\t\tif sourcesErr != nil {\n\t\t\treturn sourcesErr\n\t\t}",
        new="\t\tif sourcesErr != nil {\n\t\t\tgot = codesrc.Snapshot{Latest: map[string]codesrc.Record{}, Missing: true}\n\t\t}",
        killer="TestAnUnreadableSourcesJournalIsTheStoreUnreachable503",
        why="degrading to 'empty' keeps the page up, which reads as resilience — and a journal the pod "
        "cannot read answers `sources=undeclared`, the could-not-look state served as a fact.",
    ),
    Mutant(
        name="api-sources-damage-leaks-onto-the-wire",
        path="internal/report/sources.go",
        old="JournalAbsent: snap.Missing}",
        new="JournalAbsent: snap.Missing || snap.Damaged()}",
        killer="TestAHiddenScopesDamagedLineChangesNothingOnTheWire",
        why="'a damaged journal is as good as no journal' reads as caution — and a torn line in a scope the "
        "caller cannot read flips every other scope's answer, a signal about hidden activity on the wire.",
    ),
    # S4 of the scope-refs plan: the browser surface is the sources journal's ONLY writer
    # (`internal/ui/sources.go`). `./internal/ui/` is already in `PKGS`.
    Mutant(
        name="ui-sources-post-skips-the-admin-check",
        path="internal/ui/sources.go",
        old="if !found || !id.Auth.Allows(scope.ID, control.VerbAdmin) {",
        new="if !found {",
        killer="TestTheSaveIsBehindBothCrossSiteGatesAndTheAdminCheck",
        extra_killers=("TestATokenFileDeploymentRefusesEverySave",),
        why="'the scope is in the caller's narrowed answer, so they may edit it' reads as the check — and it "
        "lets anyone who can READ a scope repoint its audit, which is the T5 threat.",
    ),
    Mutant(
        name="ui-sources-links-a-non-github-host",
        path="internal/ui/sources.go",
        old='src.Host != "github.com"',
        new='src.Host == ""',
        killer="TestOnlyAGitHubSourceIsLinked",
        why="'link every source, they all live on a code host' is a helpful-looking widening — and it builds "
        "a GitHub-shaped URL on a host whose URL shape this repository cannot know.",
    ),
    Mutant(
        name="ui-sources-set-by-taken-from-the-form",
        path="internal/ui/sources.go",
        old="principalStamp(id.Principal), s.now()",
        new='func() string { if v := r.PostFormValue("set_by"); v != "" { return v }; return principalStamp(id.Principal) }(), s.now()',
        killer="TestAnAdminsSaveAppendsOneLineStampedByTheServerNotTheForm",
        why="'let an admin record who asked for the change' reads as a feature — and the journal's who/when "
        "history becomes whatever the form says.",
    ),
    Mutant(
        name="ui-sources-keyed-by-control-id",
        path="internal/ui/sources.go",
        old="codesrc.Key(scope.Name), raws",
        new="codesrc.Key(string(scope.ID)), raws",
        killer="TestAUIWrittenDeclarationIsWhatThePodServes",
        why="revision 3 of the plan: 'key by the immutable id, a rename should not orphan the record' — and "
        "the UI's ids come from the control journal while the pod's are derived from the directory name, "
        "so the pod never finds the record (round 3's red).",
    ),
    # S2 of the transcripts/plugins plan: `internal/transcript/scopeuse`, the READ half of a session's
    # visibility set. It is not in `PKGS` — it is an input to a predicate that does not exist until
    # S4, not a member of the control/identity/server seam — so each row ADDS its package, the
    # override the battery prescribes for a killer outside the seam.
    Mutant(
        name="scopeuse-all-scopes-header-names-a-scope",
        pkgs=PKGS + ("./internal/transcript/scopeuse/",),
        path="internal/transcript/scopeuse/scopeuse.go",
        old="\t\tif ValidScope(v) {",
        new="\t\tif ValidScope(v) || strings.HasPrefix(v, \"(\") {",
        killer="TestTheAllScopesHeaderAddsStar",
        why="a store-wide search renders `scope=(all scopes)`, and reading the field as whatever "
        "follows `scope=` is the natural parse — it turns the one header that names EVERY scope into "
        "a scope nobody has, so `V` loses its `*` and the session becomes readable by anyone who reads "
        "the other scopes it touched.",
    ),
    Mutant(
        name="scopeuse-scopeless-header-dropped",
        pkgs=PKGS + ("./internal/transcript/scopeuse/",),
        path="internal/transcript/scopeuse/scopeuse.go",
        # #261 round 3: the scan emits through a callback instead of appending to a slice.
        old="\t\tif !found {\n\t\t\temit(Star)\n\t\t}",
        new="\t\tif !found {\n\t\t}",
        killer="TestTheScopelessHeaderFormAddsStar",
        why="the `all N entry files … MALFORMED` header carries no `scope=` field, and \"no field, "
        "nothing to add\" reads as tidy — it drops a read whose scope the line does not say.",
    ),
    Mutant(
        name="scopeuse-fallback-empty-ledger-trusted",
        pkgs=PKGS + ("./internal/transcript/scopeuse/",),
        path="internal/transcript/scopeuse/scopeuse.go",
        old="\tr.F1 = d.namesProgram && d.ledgerRecords == 0",
        new="\tr.F1 = d.namesProgram && d.ledgerRecords < 0",
        killer="TestF1EmptyLedgerAddsStar",
        extra_killers=("TestTheVisibilityInputOfEveryFixtureSession",),
        why="an empty ledger READS as \"this session read nothing\" — exactly what a pre-ledger "
        "client, or one whose environment lost the session id, also produces. Trusting it makes every "
        "unrecorded read invisible to `V`.",
    ),
    Mutant(
        name="scopeuse-cache-path-read-ignored",
        pkgs=PKGS + ("./internal/transcript/scopeuse/",),
        path="internal/transcript/scopeuse/scopeuse.go",
        old="\tif strings.Contains(lower, \"subsystem-store\") {",
        new="\tif false && strings.Contains(lower, \"subsystem-store\") {",
        killer="TestF2CacheReadAddsStar",
        why="a file-tool read of the cache never passes through the client, so it is never ledgered; "
        "dropping F2 because \"the ledger covers reads now\" is the plausible simplification, and it "
        "makes every cache read invisible.",
    ),
    # S2 review round 1 (F3): four guards that each SURVIVED a mutation with no test reaching them.
    Mutant(
        name="scopeuse-restore-ignored",
        pkgs=PKGS + ("./internal/transcript/scopeuse/",),
        path="internal/transcript/scopeuse/scopeuse.go",
        old="func (d *Deriver) Restore(e Evidence) {\n",
        new="func (d *Deriver) Restore(e Evidence) {\n\tif true {\n\t\treturn\n\t}\n",
        killer="TestRestoreFoldsPersistedEvidence",
        why="a later run feeds only NEW records; forgetting the earlier runs' evidence reads as "
        "harmless because V is also persisted — and silently drops F1's 'names the program' flag "
        "the moment the ledger changes.",
    ),
    Mutant(
        name="capture-v-not-a-union",
        pkgs=PKGS + ("./internal/capture/",),
        path="internal/capture/agent.go",
        old="\tss.V = union(ss.V, res.V)\n",
        new="\tss.V = res.V\n",
        occurrences=2,
        killer="TestVOnlyGrowsWhenTheLedgerShrinks",
        why="'V is what this run derived' is the natural assignment; anything on the host can delete "
        "a ledger, and a non-union V then SHRINKS and moves the session to an instance it already "
        "read past.",
    ),
    Mutant(
        name="capture-child-ledger-ignored",
        pkgs=PKGS + ("./internal/capture/",),
        path="internal/capture/agent.go",
        old="\tfor _, c := range children {\n\t\ta.foldLedger(d, c)\n\t}\n",
        new="\tfor range children {\n\t}\n",
        killer="TestAChildSessionsLedgerCounts",
        why="the ledger is keyed by session id and a child is its own id in opencode; folding only "
        "the root's file reads as complete and loses every read a subagent made.",
    ),
    Mutant(
        name="capture-unreadable-ledger-ignored",
        pkgs=PKGS + ("./internal/capture/",),
        path="internal/capture/agent.go",
        old="\tcase err != nil:\n\t\td.LedgerUnreadable()\n",
        new="\tcase err != nil:\n",
        killer="TestAnUnreadableLedgerFailsClosed",
        why="treating a ledger that cannot be read like one that does not exist is the usual "
        "error-tolerant reflex, and it turns an unreadable record of reads into 'read nothing'.",
    ),
    # 🔴 TRANSCRIPT UPLOAD AND STORE (S3 of `claudedocs/plan-cairn-plugins.md`). The plan's eight S3
    # rows, less `transcript-capture-token-reads` (S3 has no read route for a capture token to reach
    # — the kind check it guards arrives with S5's plugin routes), plus the DISARM gate's two (O16),
    # and four more for guards whose hand mutation each went red on its own test. Every row's
    # killer lives in `internal/worker` or `internal/transcript/archive`, so each ADDS those to the
    # seam (`pkgs`), and `PKGS` itself — pinned in prose — does not move.
    Mutant(
        name="transcript-capture-armed-by-default",
        path="internal/worker/worker.go",
        old="\tif rt.upload && !w.cfg.Armed {",
        new="\tif false && rt.upload && !w.cfg.Armed {",
        killer="TestADisarmedListenerRefusesEveryUploadAndStoresNothing",
        why="a listener that is configured looks like a listener that is meant to accept; the arming "
        "is a separate operator decision (O16) and the configuration is not it.",
        pkgs=PKGS + ("./internal/worker/", "./internal/transcript/archive/"),
    ),
    Mutant(
        name="transcript-main-arms-regardless",
        path="cmd/cairn-ui/main.go",
        old="Armed: workerFlags.armed,",
        new="Armed: true,",
        killer="TestTheWorkerListenerIsDisarmedUnlessArmedByFlag",
        why="every in-process worker test sets `Armed` itself, so the one line in `main` that hands the "
        "flag through is visible only to a test that runs the binary.",
        pkgs=PKGS + ("./internal/worker/", "./internal/transcript/archive/"),
    ),
    Mutant(
        name="transcript-cas-ignores-from-offset",
        path="internal/transcript/archive/archive.go",
        old="\tpos := Position{}\n\tif m != nil {\n\t\tpos = m.Streams[stream]\n\t}\n\tif u.From != pos.StoredTo {",
        new="\tpos := Position{}\n\tif m != nil {\n\t\tpos = m.Streams[stream]\n\t}\n\tif false && u.From != pos.StoredTo {",
        killer="TestAResumedUploadLandsEveryRecordExactlyOnce",
        why="appending whatever arrives is the obvious store; without the compare-and-swap a retried "
        "upload after a lost acknowledgement lands every record twice (clause a).",
        pkgs=PKGS + ("./internal/worker/", "./internal/transcript/archive/"),
    ),
    Mutant(
        name="transcript-capture-token-host-unchecked",
        path="internal/worker/worker.go",
        old="\tif req.Host != row.Host {\n\t\tbadRequest(rw, fmt.Errorf(\"host %q is not this token's host\", req.Host))\n"
        "\t\treturn\n\t}\n\treq.Upload.Root = r.PathValue(\"root\")\n\tst, err := w.cfg.Archive.AppendRecords",
        new="\tif false && req.Host != row.Host {\n\t\tbadRequest(rw, fmt.Errorf(\"host %q is not this token's host\", req.Host))\n"
        "\t\treturn\n\t}\n\treq.Upload.Root = r.PathValue(\"root\")\n\tst, err := w.cfg.Archive.AppendRecords",
        killer="TestAHostTheTokenIsNotBoundToIs400AndWritesNothing",
        why="the store keys on the TOKEN's host anyway, so the body's host looks decorative — and a "
        "token file copied to the wrong machine uploads that machine's sessions as this one's.",
        pkgs=PKGS + ("./internal/worker/", "./internal/transcript/archive/"),
    ),
    Mutant(
        name="transcript-pod-recheck-skipped",
        path="internal/transcript/archive/archive.go",
        old="if _, hits := a.cfg.Recheck.Record(c.Bytes()); len(hits) > 0 {",
        new="if _, hits := a.cfg.Recheck.Record(c.Bytes()); false && len(hits) > 0 {",
        killer="TestThePodRefusesARecordTheTableMatches",
        why="the host already redacted, so a second scan reads as redundant — and it is the only thing "
        "between a bypassed or out-of-date agent and the store (clause c).",
        pkgs=PKGS + ("./internal/worker/", "./internal/transcript/archive/"),
    ),
    Mutant(
        name="transcript-pod-recheck-skips-blobs",
        path="internal/transcript/archive/archive.go",
        old="if _, hits := a.cfg.Recheck.Blob(name, data); len(hits) > 0 {",
        new="if _, hits := a.cfg.Recheck.Blob(name, data); false && len(hits) > 0 {",
        killer="TestThePodRefusesATextBlobTheTableMatches",
        why="a persisted tool result is a FILE, not a record, and a re-check written for records "
        "passes every blob unscanned (R4, clause c).",
        pkgs=PKGS + ("./internal/worker/", "./internal/transcript/archive/"),
    ),
    Mutant(
        name="transcript-root-owner-not-fixed",
        path="internal/transcript/archive/archive.go",
        old="return m == nil || (m.Owner == who.Owner && m.Host == who.Host)",
        new="return m == nil || m.Owner == who.Owner || m.Host == who.Host || true",
        killer="TestTheFirstUploadFixesTheOwnerAndHost",
        why="every uploader holds a valid token, so ownership looks settled by authentication — and a "
        "second host appends into a session it never ran (decision 15).",
        pkgs=PKGS + ("./internal/worker/", "./internal/transcript/archive/"),
    ),
    Mutant(
        name="transcript-pod-skips-scope-rederivation",
        path="internal/transcript/archive/archive.go",
        old="\tm.Derived = union(m.Derived, found.Scopes)",
        new="\tm.Derived = union(m.Derived, nil)",
        killer="TestThePodDerivesReadScopesTheAgentDidNotDeclare",
        why="the agent already declares what it derived with the same function, so folding the pod's own "
        "derivation in reads as redundant — and it makes the agent's DECLARATION the whole read half "
        "of V, which decision 3 exists to refuse.",
        pkgs=PKGS + ("./internal/worker/", "./internal/transcript/archive/"),
    ),
    Mutant(
        name="worker-token-accepted-by-browser-row",
        path="internal/ui/auth.go",
        old="import (\n",
        new="import (\n\t_ \"github.com/ZacxDev/cairn/internal/worker\"\n",
        killer="TestOnlyTheBrowserProgramImportsWorker",
        why="the browser chain authenticates bearer tokens too, so teaching it worker tokens looks like "
        "reuse — and it is the edit that lets a capture token mean something on a browser row.",
        pkgs=PKGS + ("./internal/worker/", "./internal/transcript/archive/"),
    ),
    Mutant(
        name="transcript-missing-middle-frame-stored",
        path="internal/transcript/archive/frames.go",
        old="} else if st == nil || st.next != f.Index ||",
        new="} else if st == nil || st.next > f.Index ||",
        killer="TestAMissingMiddleFrameStoresNothing",
        why="'not a frame we already have' reads as the whole order check, and a skipped frame then "
        "reassembles into a record with a hole in it.",
        pkgs=PKGS + ("./internal/worker/", "./internal/transcript/archive/"),
    ),
    Mutant(
        name="transcript-quota-unchecked",
        path="internal/transcript/archive/archive.go",
        old="\tif a.used+n > a.cfg.Quota {",
        new="\tif false && a.used+n > a.cfg.Quota {",
        killer="TestTheQuotaRefusesAndRetentionFreesIt",
        why="the retention sweep already bounds the disk eventually, so a quota looks redundant — at "
        "gigabytes a week, 'eventually' is the outage (T12).",
        pkgs=PKGS + ("./internal/worker/", "./internal/transcript/archive/"),
    ),
    Mutant(
        name="transcript-retention-boundary-inclusive",
        path="internal/transcript/archive/sweep.go",
        old="return now.Sub(last) > a.cfg.Retention }",
        new="return now.Sub(last) >= a.cfg.Retention }",
        killer="TestRetentionSweepsAtItsBoundary",
        why="`>=` and `>` read the same in a review; the boundary decides whether a session lives "
        "through the last instant of its retention.",
        pkgs=PKGS + ("./internal/worker/", "./internal/transcript/archive/"),
    ),
    # 🔴 THE ARMING PRECONDITIONS AP1–AP3 (`claudedocs/plan-cairn-plugins.md`, "### Rollout"): one
    # row per guard, each killed by the test its precondition's closing check names.
    Mutant(
        name="transcript-dir-ownership-unchecked",
        path="internal/transcript/archive/archive.go",
        old="\tif err := CheckOwnership(cfg.Dir); err != nil {\n\t\treturn nil, err\n\t}\n",
        new="",
        killer="TestOpenAcceptsAFreshAndItsOwnDirectoryAndNothingElse",
        why="`ResolveDir` already refuses the store root, so the directory looks vetted — and a control "
        "journal under the store's journal name receives a deletion line the next replay refuses whole (AP1).",
        pkgs=PKGS + ("./internal/worker/", "./internal/transcript/archive/"),
    ),
    Mutant(
        name="transcript-main-skips-state-paths",
        path="cmd/cairn-ui/main.go",
        old="if err := stateInsideTranscriptDir(workerConf.dir, statePaths); err != nil {",
        new="if err := stateInsideTranscriptDir(workerConf.dir, nil); err != nil {",
        killer="TestTheBinaryRefusesATranscriptDirHoldingItsOwnState",
        why="the store's own ownership check refuses what is THERE at startup, so a second check reads as "
        "redundant — and a state file this process creates later is refused one restart afterwards (AP1).",
        pkgs=PKGS + ("./internal/worker/", "./internal/transcript/archive/"),
    ),
    Mutant(
        name="transcript-state-path-compared-by-spelling",
        path="cmd/cairn-ui/worker.go",
        old="\t\tresolved, err := resolveMaybeAbsent(p.path)\n",
        new="\t\tresolved, err := filepath.Abs(p.path)\n",
        killer="TestStateInsideTheTranscriptDirIsResolvedNotSpelled",
        why="`Abs` is the obvious normalisation, and a symlinked spelling of the transcript directory then "
        "walks past it (AP1).",
        pkgs=PKGS + ("./internal/worker/", "./internal/transcript/archive/"),
    ),
    Mutant(
        name="transcript-assembled-record-uncapped",
        path="internal/transcript/archive/frames.go",
        old="if st.bytes+int64(len(chunk)) > MaxAssembledBytes {",
        new="if false && st.bytes+int64(len(chunk)) > MaxAssembledBytes {",
        killer="TestOneFramePastTheCapIs413AndStoresNothing",
        why="each REQUEST is already bounded, so the record looks bounded too — and the final frame reads "
        "every staged byte into memory, which the audit measured as an OOM kill in a 256 Mi pod (AP2).",
        pkgs=PKGS + ("./internal/worker/", "./internal/transcript/archive/"),
    ),
    Mutant(
        name="transcript-uncommitted-session-never-swept",
        path="internal/transcript/archive/sweep.go",
        old="\t\t\tok, err = a.deleteUncommitted(root, stale)\n",
        new="",
        killer="TestASessionWithNoMetaIsSweptByRetention",
        why="retention is read off `meta.json`, so a root without one looks like nothing to sweep — and it "
        "holds quota forever (AP3).",
        pkgs=PKGS + ("./internal/worker/", "./internal/transcript/archive/"),
    ),
    Mutant(
        name="transcript-abandoned-frames-hold-quota",
        path="internal/transcript/archive/sweep.go",
        old="\ta.sweepFrames(now)\n",
        new="",
        killer="TestAbandonedFramesStopHoldingQuotaWithoutARestart",
        why="a restart discards staged frames, so they look bounded — and an agent that never finishes a "
        "record holds its bytes against the quota until somebody restarts the pod (AP3).",
        pkgs=PKGS + ("./internal/worker/", "./internal/transcript/archive/"),
    ),
    Mutant(
        name="transcript-names-not-rechecked",
        path="internal/transcript/archive/archive.go",
        old="\t\tif _, hits := a.cfg.Recheck.String(f.value); len(hits) > 0 {",
        new="\t\tif _, hits := a.cfg.Recheck.String(f.value); false && len(hits) > 0 {",
        killer="TestTheRecheckReadsNamesSrcAndCursors",
        why="names and cursors are pattern-checked, so they look inert — and a 40-character token fits "
        "every one of those patterns and is stored as a file name or on every line (AP3).",
        pkgs=PKGS + ("./internal/worker/", "./internal/transcript/archive/"),
    ),
    Mutant(
        name="worker-owner-checked-only-at-startup",
        path="internal/worker/worker.go",
        old="\tif !w.cfg.OwnerHeld() {",
        new="\tif false && !w.cfg.OwnerHeld() {",
        killer="TestTheOwnerIsCheckedOnEveryRequest",
        why="startup already refused an owner the authority does not hold, so a per-request check looks "
        "redundant — and a principal removed afterwards keeps uploading until a restart (AP3).",
        pkgs=PKGS + ("./internal/worker/", "./internal/transcript/archive/"),
    ),
    Mutant(
        name="worker-wall-skipped-on-the-reread",
        path="internal/worker/worker.go",
        old="\t\tif err := admit(row, w.cfg.wall()); err != nil {\n\t\t\tw.once(",
        new="\t\tif err := admit(row, Wall{Owner: row.Owner, Plugins: w.cfg.wall().Plugins}); err != nil {\n\t\t\tw.once(",
        killer="TestTheWallRefusesAForeignRowAtStartupAndAsARowAfterwards",
        why="the startup read already refused foreign rows, so the per-request re-read looks safe to "
        "trust — and a row appended afterwards would authenticate a second owner (Q7's wall).",
        pkgs=PKGS + ("./internal/worker/", "./internal/transcript/archive/"),
    ),
    # 🔴 TRANSCRIPT VISIBILITY AND THE COLLAPSED READING (S4 of `claudedocs/plan-cairn-plugins.md`):
    # the plan's eleven S4 rows. The predicate, the visibility set and the reading model live in
    # `internal/transcript`, which is not in `PKGS` — it is a consumer of the control seam, not a
    # member of it — so every row ADDS it (and the archive it reads). `./internal/ui/` is in `PKGS`
    # already, and several rows name a page-level test there as an `extra_killers` entry: the
    # predicate is one function, and the page is where a viewer would see it fail.
    Mutant(
        name="transcript-section-without-predicate",
        path="internal/transcript/visible.go",
        old="\tif !Visible(viewer, meta, v) {\n\t\treturn nil, nil\n\t}\n\treturn &View{",
        new="\tif false && !Visible(viewer, meta, v) {\n\t\treturn nil, nil\n\t}\n\treturn &View{",
        killer="TestTranscriptVisibilityIsTheOwnerOrAReaderOfEveryScope",
        extra_killers=(
            "TestTheTranscriptSectionIsShownToExactlyThoseThePredicateAdmits",
            "TestARefusedViewerGetsTheBytesOfASessionWithNoTranscript",
            "TestTheRawRecordViewAsksThePredicate",
        ),
        why="the session page was already found for this viewer, so asking a second, stricter question "
        "before rendering one more section reads as redundant — and it shows a transcript to anyone "
        "who can read ONE scope the session wrote in (clause d).",
        pkgs=PKGS + ("./internal/transcript/", "./internal/transcript/archive/"),
    ),
    Mutant(
        name="transcript-written-set-from-viewer-scopes",
        path="internal/transcript/visible.go",
        old="written, err := WrittenScopes(s.storeRoot, ids)",
        new="written, err := writtenWithin(s.storeRoot, viewer.Auth.VisibleScopes(control.VerbRead), ids)",
        killer="TestTranscriptVisibilityIsTheOwnerOrAReaderOfEveryScope",
        why="every other walk on this surface is narrowed to the caller's readable scopes, and passing "
        "the same set here looks like the rule being followed — over that set every scope found is "
        "readable by construction, so 'reads every written scope' is vacuously true (clause e).",
        pkgs=PKGS + ("./internal/transcript/", "./internal/transcript/archive/"),
    ),
    Mutant(
        name="transcript-visibility-set-ignores-reads",
        path="internal/transcript/visible.go",
        old="out := slices.Concat(written.Scopes, written.Unscanned, meta.Written, meta.Derived, meta.Declared)",
        new="out := slices.Concat(written.Scopes, written.Unscanned, meta.Written)",
        killer="TestTranscriptVisibilityIsTheOwnerOrAReaderOfEveryScope",
        why="'the scopes a session touched' reads as the scopes it WROTE to, which is what the session "
        "page shows — and a session that recalled another scope's notes into its tool results is then "
        "visible to people who cannot read that scope (O8, clause f).",
        pkgs=PKGS + ("./internal/transcript/", "./internal/transcript/archive/"),
    ),
    Mutant(
        name="transcript-empty-set-visible",
        path="internal/transcript/visible.go",
        old="\tif len(v) == 0 {\n\t\treturn false\n\t}\n\treturn viewer.readsEvery(v)",
        new="\tif false && len(v) == 0 {\n\t\treturn false\n\t}\n\treturn viewer.readsEvery(v)",
        killer="TestTranscriptVisibilityIsTheOwnerOrAReaderOfEveryScope",
        extra_killers=("TestASessionWithNoRecordedWriteIsFoundForItsOwnerAlone",),
        why="'reads every scope in V' needs no special case to be written — and for a session with no "
        "recorded write or read it is vacuously true for EVERYBODY (O10, clause g).",
        pkgs=PKGS + ("./internal/transcript/", "./internal/transcript/archive/"),
    ),
    Mutant(
        name="transcript-owner-by-display-not-id",
        path="internal/transcript/visible.go",
        old='meta.Owner == string(v.Principal.Kind)+":"+string(v.Principal.ID)',
        new='meta.Owner == string(v.Principal.Kind)+":"+v.Principal.Display',
        killer="TestVisibleArms",
        why="the display name is what every page and audit line shows for a principal, so comparing it "
        "reads as comparing the principal — and it is mutable and not unique, so a rename makes one "
        "principal another's owner.",
        pkgs=PKGS + ("./internal/transcript/", "./internal/transcript/archive/"),
    ),
    Mutant(
        name="transcript-narrowed-owner-arm",
        path="internal/transcript/visible.go",
        old="return !v.Auth.Narrowed() && meta.Owner ==",
        new="return meta.Owner ==",
        killer="TestVisibleArms",
        why="a narrowed credential is still the owner's credential, so letting it see the owner's "
        "sessions reads as obvious — and it hands an agent credential cut down to one scope every "
        "owner-only session, which is what the narrowing withheld.",
        pkgs=PKGS + ("./internal/transcript/", "./internal/transcript/archive/"),
    ),
    Mutant(
        name="transcript-any-scope-not-every-scope",
        path="internal/transcript/visible.go",
        old="return !slices.ContainsFunc(scopes, func(scope string) bool { return !v.reads(scope) })",
        new="return len(scopes) == 0 || slices.ContainsFunc(scopes, v.reads)",
        killer="TestTranscriptVisibilityIsTheOwnerOrAReaderOfEveryScope",
        why="the session PAGE is found by a union — a write in any readable scope — and reusing that "
        "rule for its content is the natural reading of 'can this viewer see the session'.",
        pkgs=PKGS + ("./internal/transcript/", "./internal/transcript/archive/"),
    ),
    Mutant(
        name="transcript-child-visible-without-root",
        path="internal/transcript/visible.go",
        old="ids := append([]string{meta.Root}, meta.Children...)",
        new="ids := []string{meta.Root}",
        killer="TestTranscriptVisibilityIsTheOwnerOrAReaderOfEveryScope",
        why="a transcript is stored under its ROOT session id, so walking for that id reads as complete "
        "— and a child session's own writes then never reach the root's V, leaving the child's stream "
        "visible to people who cannot read where it wrote (decision 7).",
        pkgs=PKGS + ("./internal/transcript/", "./internal/transcript/archive/"),
    ),
    Mutant(
        name="transcript-unknown-scope-name-readable",
        path="internal/transcript/visible.go",
        old="\treturn v.Auth.VisibleScopes(control.VerbRead).Allows(scope)\n}",
        new="\treturn v.Auth.VisibleScopes(control.VerbRead).Allows(scope) || !slices.ContainsFunc(control.AllVerbs, "
        "func(vb control.Verb) bool { return v.Auth.VisibleScopes(vb).Allows(scope) })\n}",
        killer="TestTranscriptVisibilityIsTheOwnerOrAReaderOfEveryScope",
        extra_killers=("TestAScopeRenamedAfterTheWriteIsUnreadableUntilItIsRenamedBack",),
        why="'that scope no longer exists, so it cannot be hiding anything from anyone' — and the content "
        "read from it before the rename is still in the transcript.",
        pkgs=PKGS + ("./internal/transcript/", "./internal/transcript/archive/"),
    ),
    Mutant(
        name="transcript-star-sentinel-readable",
        path="internal/transcript/visible.go",
        old="\tif scope == Star {\n\t\treturn false\n\t}",
        new="\tif scope == Star {\n\t\treturn true\n\t}",
        killer="TestTranscriptVisibilityIsTheOwnerOrAReaderOfEveryScope",
        why="`*` reads as a wildcard — 'every scope' — and a wildcard a viewer need not hold is the "
        "usual meaning; here it means 'a scope nobody could identify', which nobody but the owner reads.",
        pkgs=PKGS + ("./internal/transcript/", "./internal/transcript/archive/"),
    ),
    Mutant(
        name="transcript-render-classifies-by-record-type",
        path="internal/transcript/read.go",
        old="\t\tclass := u.Class\n",
        new='\t\tclass := u.Class\n\t\tif kind == "user" {\n\t\t\tclass = HumanText\n\t\t}\n',
        killer="TestAToolResultInsideAUserRecordIsNotAUserMessage",
        extra_killers=("TestTheTranscriptRendersCollapsed",),
        why="a `user` record is what a user sent, by its own type — and a tool result of several hundred "
        "kilobytes rides INSIDE one, so 'user messages are shown in full' renders every one of them whole.",
        pkgs=PKGS + ("./internal/transcript/", "./internal/transcript/archive/"),
    ),
    # #261's audit: the derivation's two blind spots and the View's decision bound, each a row.
    Mutant(
        name="transcript-pod-derives-from-the-map-alone",
        path="internal/transcript/archive/archive.go",
        old="\t\td.Content(string(line))\n\t\tif duplicateMember(line) {",
        new="\t\tif duplicateMember(line) {",
        killer="TestARecordCannotHideAReadFromTheDerivation",
        why="the map walk already visits every value of the record, so scanning the record again as one "
        "string reads as redundant — and a header in a member NAME, or in the first of two members "
        "named alike, is then never derived while the raw view serves it.",
        pkgs=PKGS + ("./internal/worker/", "./internal/transcript/archive/"),
    ),
    Mutant(
        name="transcript-pod-reads-past-a-repeated-member",
        path="internal/transcript/archive/archive.go",
        old="\t\t\textra = []string{Star}",
        new="\t\t\textra = nil",
        killer="TestARecordCannotHideAReadFromTheDerivation",
        why="once the whole-record scan finds every header, a repeated member looks harmless — but which "
        "strings are tool inputs is still read off a map that drops the first copy.",
        pkgs=PKGS + ("./internal/worker/", "./internal/transcript/archive/"),
    ),
    Mutant(
        name="transcript-view-serves-past-its-decision",
        path="internal/transcript/visible.go",
        old="\t\tif r.Seq > limit {\n\t\t\treturn archive.StopScan",
        new="\t\tif false && r.Seq > limit {\n\t\t\treturn archive.StopScan",
        killer="TestAViewServesOnlyWhatItWasDecidedOver",
        why="a stream is append-only, so reading it to its current end looks like reading 'the transcript' "
        "— and a record uploaded after the predicate was asked can be one that hides it from this viewer.",
        pkgs=PKGS + ("./internal/transcript/", "./internal/transcript/archive/"),
    ),
    Mutant(
        name="transcript-turn-unclamped",
        path="internal/transcript/read.go",
        old="\tturn = min(turn, MaxTurn)\n",
        new="",
        killer="TestTurnsPageByFifty",
        why="a turn number is only a view selector, so bounding it reads as fussiness — and the page "
        "arithmetic overflows at the top of int, rendering negative page links.",
        # The ui's pager test WAS listed here (the ui kept its own copy of this clamp, which masked this
        # mutant from every ui test; #261 round 2), and it stopped killing in round 3 because the GUARD
        # moved, not the list: the pager no longer adds TurnsPerPage to FirstTurn (it links
        # `Reading.NextTurn`), so an unclamped `?turn=<MaxInt64>` renders an empty page with an
        # in-range back link instead of negative page links. The overflow left is readSession's own
        # window arithmetic, which `TestTurnsPageByFifty` catches.
        pkgs=PKGS + ("./internal/transcript/", "./internal/transcript/archive/"),
    ),
    # #261 round 2: a memo block is a read, the written half is kept, and a page is item-bounded.
    Mutant(
        name="scopeuse-memo-block-not-a-read",
        pkgs=PKGS + ("./internal/transcript/scopeuse/",),
        path="internal/transcript/scopeuse/scopeuse.go",
        # #261 round 3: the memo scan emits through a callback instead of returning a slice.
        old="\teachMemoScope(s, d.addHeader)\n",
        new="",
        killer="TestAMemoBlockIsARead",
        why="a memo is a NOTICE, not a recall — no `subsystem-recall:` header, nothing the client "
        "ledgered — so leaving it out of the read half reads as precise; and the hook has then shown "
        "a gamma-notes memo's text in a session an alpha-only reader is served.",
    ),
    Mutant(
        name="transcript-written-half-not-kept",
        path="internal/transcript/visible.go",
        old="out := slices.Concat(written.Scopes, written.Unscanned, meta.Written, meta.Derived, meta.Declared)",
        new="out := slices.Concat(written.Scopes, written.Unscanned, meta.Derived, meta.Declared)",
        killer="TestARewrittenEntryCannotShrinkV",
        why="the walk is the authority on where a session wrote, so a stored copy of its answer reads "
        "as a stale cache — and rewriting one entry without the trailer then turns a refusal into a 200.",
        pkgs=PKGS + ("./internal/transcript/", "./internal/transcript/archive/"),
    ),
    Mutant(
        name="transcript-page-items-uncapped",
        path="internal/transcript/read.go",
        old="if r.room <= 0 || (limit >= 0 && len(items) >= limit) {",
        new="if limit >= 0 && len(items) >= limit {",
        killer="TestATurnPastThePageItemCapIsCountedNotCarried",
        extra_killers=("TestOneTurnOfTwentyThousandCallsIsABoundedPage",),
        why="fifty turns and a 2 MiB byte budget read as bounding a page, so an item cap looks "
        "redundant — and one turn of 20,000 small calls fits both and renders 7.5 MB.",
        pkgs=PKGS + ("./internal/transcript/", "./internal/transcript/archive/"),
    ),
    Mutant(
        name="transcript-other-pages-charged",
        path="internal/transcript/read.go",
        old="\t\tfor _, it := range p.items {\n\t\t\tw.r.refund(it)\n\t\t}\n\t\treturn\n",
        new="\t\treturn\n",
        killer="TestAnEarlierPageDoesNotSpendThisPagesBudget",
        why="a record on another page is simply skipped, which reads as costing nothing — but it was "
        "classified, and charged, before the window knew, so page two shows elided bodies with room to spare.",
        pkgs=PKGS + ("./internal/transcript/", "./internal/transcript/archive/"),
    ),
    # #261 round 3: the derivation fits the assembly budget, an unscanned scope is not kept, and a
    # page ends at a turn boundary.
    Mutant(
        name="scopeuse-decode-gate-misses-unicode-escapes",
        pkgs=PKGS + ("./internal/transcript/scopeuse/",),
        path="internal/transcript/scopeuse/scopeuse.go",
        old=" || strings.Contains(s, `\\u`)",
        new="",
        killer="TestTheDecodeGateIsExact",
        extra_killers=("TestAHeaderInsideAJSONStringNamesItsScope",),
        why="a marker that is not in the raw text looks absent, so skipping the decode reads as a free "
        "saving — and JSON can spell any character of either marker as `\\uXXXX`, so the read vanishes.",
    ),
    Mutant(
        name="transcript-unscanned-scope-kept",
        path="internal/transcript/visible.go",
        old="s.archive.RecordWritten(meta.Root, written.Scopes)",
        new="s.archive.RecordWritten(meta.Root, slices.Concat(written.Scopes, written.Unscanned))",
        killer="TestARepairedEntryGivesTheTranscriptBack",
        why="an unscanned scope is in `V` and the written half is kept, so keeping both reads as "
        "consistent — but a malformed file is a fact about the store, and keeping it makes one "
        "transient file a permanent refusal.",
        pkgs=PKGS + ("./internal/transcript/", "./internal/transcript/archive/"),
    ),
    Mutant(
        name="transcript-page-does-not-end-at-a-turn",
        path="internal/transcript/read.go",
        old="\t\t\tif r.room <= 0 {\n\t\t\t\tr.roomOut = true\n\t\t\t}\n",
        new="",
        killer="TestEveryCallIsReachableAcrossPages",
        extra_killers=("TestATurnWhoseChildDoesNotFitStartsTheNextPage", "TestATurnPastThePageItemCapIsCountedNotCarried"),
        why="counting what the page cannot carry, with a raw link, reads as honest — but a turn cut that "
        "way on page one had no page of its own, so its calls were reachable only as raw records.",
        pkgs=PKGS + ("./internal/transcript/", "./internal/transcript/archive/"),
    ),
    Mutant(
        name="transcript-child-overflow-keeps-the-turn",
        path="internal/transcript/read.go",
        old="\tw.r.children(w.cur.Items, 0)\n\tif w.r.roomOut && len(w.turns) > 0 {",
        new="\tw.r.children(w.cur.Items, 0)\n\tif false && w.r.roomOut && len(w.turns) > 0 {",
        killer="TestATurnWhoseChildDoesNotFitStartsTheNextPage",
        why="a turn's own items fit, so it looks carried — but its subagent spends the same allowance, "
        "and the child's items past it were counted instead of moving with the turn.",
        pkgs=PKGS + ("./internal/transcript/", "./internal/transcript/archive/"),
    ),
    Mutant(
        name="transcript-pager-steps-fifty-turns",
        path="internal/ui/transcript.go",
        old="\tif rd.NextTurn > 0 {\n\t\tlinks = append(links, h.A(h.Href(transcriptPageHref(v.Session, rd.NextTurn)),",
        new="\tif nt := rd.FirstTurn + transcript.TurnsPerPage; nt <= rd.TotalTurns {\n\t\tlinks = append(links, h.A(h.Href(transcriptPageHref(v.Session, nt)),",
        killer="TestEveryCallIsReachableByFollowingThePager",
        why="a page holds fifty turns, so the next one starts fifty on — except when the item allowance "
        "ended it early, and then every turn in between is skipped.",
        pkgs=PKGS + ("./internal/ui/",),
    ),
    # A twelfth, beyond the plan's eleven: the one rendering rule the operator stated outright (O9 —
    # a user message is NEVER truncated). The test plan names it as a control; here it is a row.
    Mutant(
        name="transcript-user-message-clipped",
        path="internal/ui/transcript.go",
        old='\t\t\th.Div(h.Class("t-text"), g.Text(it.Text)))\n\tcase transcript.AssistantText:',
        new='\t\t\th.Div(h.Class("t-text"), g.Text(clipUTF8(it.Text, assistantHeadChars))))\n\tcase transcript.AssistantText:',
        killer="TestTheTranscriptRendersCollapsed",
        why="every other block on the page has a display limit, and giving the user's message the "
        "assistant's head limit reads as consistency — and the one thing the operator asked never to "
        "be cut is then cut at 1,200 characters with no reveal.",
    ),
    # ---- S5 of the transcripts plan: the plugin registry, toggles and API ------------------------
    # The toggle fold, ONE row per decision-10 case (the case number is in each name's killer), and
    # the five the plan names beside them. `internal/plugins` and `internal/worker` sit outside the
    # seam, so each row ADDS the package holding its killer (the S2/S3 rows' per-row rule).
    Mutant(
        name="plugins-toggle-default-on",
        path="internal/plugins/toggles.go",
        old="return s.Value(plugin, LevelScope, ScopeTarget(name), ValueOff)",
        new="return s.Value(plugin, LevelScope, ScopeTarget(name), ValueOn)",
        killer="TestFoldCase01NothingSetIsOff",
        extra_killers=("TestThePendingListIsEmptyUntilAToggleIsSet",),
        why="'a scope nobody has said anything about is fine' is the opt-out reading of consent — and "
        "every registered plugin then reads every session on day one (clause i).",
        pkgs=PKGS + ("./internal/plugins/", "./internal/worker/"),
    ),
    Mutant(
        name="plugins-scope-on-ignored",
        path="internal/plugins/toggles.go",
        old="return consented && (everyOn || arcOn) && !arcOff",
        new="return consented && (everyOn && false || arcOn) && !arcOff",
        killer="TestFoldCase02EveryScopeOnIsOn",
        why="reading `on` as just a louder `allowed` makes arcs the only way to run — and a scope whose "
        "admins turned a plugin ON gets nothing.",
        pkgs=PKGS + ("./internal/plugins/",),
    ),
    Mutant(
        name="plugins-arc-overrides-scope-off",
        path="internal/plugins/toggles.go",
        old="return consented && (everyOn || arcOn) && !arcOff",
        new="return (consented || arcOn) && (everyOn || arcOn) && !arcOff",
        killer="TestFoldCase03AnArcOnWithOneScopeOffIsOff",
        why="revision 1's fold: a finer level overriding a coarser one is the usual shape of settings — and "
        "arc membership is whatever an arc PUT names, so a scope's refusal is overridden by anyone who "
        "registers an arc (T14).",
        pkgs=PKGS + ("./internal/plugins/",),
    ),
    Mutant(
        name="plugins-arc-on-ignored",
        path="internal/plugins/toggles.go",
        old="\t\tcase ValueOn:\n\t\t\tarcOn = true",
        new="\t\tcase ValueOn:\n\t\t\tarcOn = false",
        killer="TestFoldCase04AnArcOnWithEveryScopeAllowedIsOn",
        why="an arc that can only veto looks like the safe half — and O5's per-arc toggle then enables "
        "nothing, so `allowed` scopes never run anywhere.",
        pkgs=PKGS + ("./internal/plugins/",),
    ),
    Mutant(
        name="plugins-scope-toggle-any-not-every",
        path="internal/plugins/toggles.go",
        old="\t\tdefault:\n\t\t\tconsented, everyOn = false, false",
        new="\t\tdefault:\n\t\t\teveryOn = false",
        killer="TestFoldCase05AnArcOnWithOneScopeAllowedAndOneOffIsOff",
        why="consent read as 'some scope agreed' — the union the session PAGE is found by — and a session "
        "that touched a refusing scope is sent to a third party on its neighbour's say-so.",
        pkgs=PKGS + ("./internal/plugins/",),
    ),
    Mutant(
        name="plugins-arc-off-ignored",
        path="internal/plugins/toggles.go",
        old="\t\tcase ValueOff:\n\t\t\tarcOff = true",
        new="\t\tcase ValueOff:\n\t\t\tarcOff = false",
        killer="TestFoldCase06EveryScopeOnWithAnArcOffIsOff",
        why="with every scope ON an arc's `off` looks redundant — and the one way to keep a single arc's "
        "sessions away from a plugin is gone.",
        pkgs=PKGS + ("./internal/plugins/",),
    ),
    Mutant(
        name="plugins-session-opt-out-ignored",
        path="internal/plugins/toggles.go",
        old="&& !arcOff && st.Session(plugin, sub.Root) != ValueOff",
        new="&& !arcOff",
        killer="TestFoldCase07EveryScopeOnWithTheOwnersOptOutIsOff",
        extra_killers=("TestThePendingListIsEmptyUntilAToggleIsSet",),
        why="the session level can only say OFF, so it reads as a no-op beside the scope consent — and "
        "the owner of a session that handled something sensitive has no way to keep it from a "
        "third-party-egress plugin (T7).",
        pkgs=PKGS + ("./internal/plugins/", "./internal/worker/"),
    ),
    Mutant(
        name="plugins-unknown-scope-toggle-falls-back",
        path="internal/plugins/toggles.go",
        old='if scope == "*" || !known(scope) {',
        new='if scope == "*" {',
        killer="TestFoldCase08AnUnknownScopeOrTheStarIsOff",
        why="the journal still holds the deleted scope's ON, so reading it is the obvious lookup — and a "
        "scope nobody can administer any more keeps consenting forever.",
        pkgs=PKGS + ("./internal/plugins/",),
    ),
    Mutant(
        name="plugins-empty-set-vacuously-on",
        path="internal/plugins/toggles.go",
        old="\tif len(sub.V) == 0 {\n\t\treturn false\n\t}\n\tconsented, everyOn := true, true",
        new="\tconsented, everyOn := true, true",
        killer="TestFoldCase09TheEmptySetIsOff",
        why="'every scope of V is on' needs no special case to be written — and for an owner-only session "
        "it is vacuously true, so a session no scope admin was ever asked about runs (clause g's shape).",
        pkgs=PKGS + ("./internal/plugins/",),
    ),
    Mutant(
        name="plugins-session-level-accepts-on",
        path="internal/plugins/toggles.go",
        old="\t\tif v == ValueOn {\n\t\t\treturn ErrSessionOn\n\t\t}\n\t\tok = v == ValueUnset || v == ValueOff",
        new="\t\tok = v == ValueUnset || v == ValueOff || v == ValueOn",
        killer="TestASessionLevelOnIsRefusedAndJournalsNothing",
        extra_killers=("TestWhoMayToggleIsDecidedPerLevel",),
        why="three values at every level is the symmetric shape — and a session-level ON is then a "
        "journaled claim of consent nobody who could give it gave (case 10).",
        pkgs=PKGS + ("./internal/plugins/",),
    ),
    Mutant(
        name="plugins-arc-home-outside-v-counts",
        path="internal/plugins/toggles.go",
        old="\t\tif !inV[ScopeTarget(k.Home)] {\n\t\t\tcontinue",
        new="\t\tif false && !inV[ScopeTarget(k.Home)] {\n\t\t\tcontinue",
        killer="TestAnArcSpeaksOnlyForSessionsThatTouchedItsHome",
        why="the plan's fold names no limit on which arcs count, so every arc listing the session looks like "
        "the rule — and an admin of an unrelated scope then turns a plugin ON (or vetoes it) for a session "
        "that scope never saw, by listing its id in their own arc (#270 round 1).",
        pkgs=PKGS + ("./internal/plugins/",),
    ),
    Mutant(
        name="plugins-transcript-read-ignores-toggle",
        path="internal/plugins/service.go",
        old="\tv, err := s.Transcripts.ForPlugin(root, on)\n\tif err != nil {\n\t\treturn nil, err\n\t}\n\tif v == nil {\n\t\treturn nil, ErrForbidden",
        new="\tv, err := s.Transcripts.ForPlugin(root, func(*transcript.Meta, []string) bool { return on != nil })\n\tif err != nil {\n\t\treturn nil, err\n\t}\n\tif v == nil {\n\t\treturn nil, ErrForbidden",
        killer="TestEveryPluginRefusalIsTheSameBytes",
        why="the capability is checked and the token is the plugin's own, so the session's toggle looks "
        "like a second check of the same thing — and a stolen plugin token reads every stored "
        "transcript (clause j, T6).",
        pkgs=PKGS + ("./internal/plugins/", "./internal/worker/"),
    ),
    Mutant(
        name="plugins-output-type-unchecked",
        path="internal/plugins/service.go",
        old="\tif !e.Writes(env.Type) {\n\t\treturn Output{}, ErrForbidden",
        new="\tif false && !e.Writes(env.Type) {\n\t\treturn Output{}, ErrForbidden",
        killer="TestEveryPluginRefusalIsTheSameBytes",
        why="the body is validated against its type's schema, which reads as the type check — and the "
        "summariser then mints ticket EDGES, which render as deterministic facts (clause j).",
        pkgs=PKGS + ("./internal/plugins/", "./internal/worker/"),
    ),
    Mutant(
        name="plugins-capability-unchecked",
        path="internal/worker/plugin.go",
        old="\tif !e.Has(plugins.CapTranscriptRead) {\n\t\tforbidden(rw)",
        new="\tif false && !e.Has(plugins.CapTranscriptRead) {\n\t\tforbidden(rw)",
        killer="TestEveryPluginRefusalIsTheSameBytes",
        why="the toggle already decides whether the plugin may see the session, so the manifest's "
        "capability looks redundant — and a plugin registered only to LIST reads every transcript it "
        "is on for.",
        pkgs=PKGS + ("./internal/plugins/", "./internal/worker/"),
    ),
    Mutant(
        name="plugins-toggle-unauthorised-level",
        path="internal/ui/plugins.go",
        old="\tscope, found := pickScope(scopes, control.ID(r.PostFormValue(FieldScope)))\n\tif !found || !id.Auth.Allows(scope.ID, control.VerbAdmin) {",
        new="\tscope, found := pickScope(scopes, control.ID(r.PostFormValue(FieldScope)))\n\tif !found || !id.Auth.Allows(scope.ID, control.VerbWrite) {",
        killer="TestWhoMayToggleIsDecidedPerLevel",
        why="a writer of a scope already puts content in it, so letting them say where it goes looks "
        "proportionate — and consent to third-party egress is an administration act the scope's "
        "admins never gave.",
        pkgs=PKGS + ("./internal/plugins/",),
    ),
    Mutant(
        name="plugins-plugin-name-from-body-not-token",
        path="internal/plugins/outputs.go",
        old="\t\tPlugin:         plugin.Name,",
        new="\t\tPlugin:         e.Plugin,",
        killer="TestTheEnvelopeIsStampedFromTheTokenNotTheBody",
        extra_killers=("TestAnOutputIsStoredUnderTheTokensNameAndAdvancesTheWatermark",),
        why="the envelope HAS a plugin field, so storing what it says looks faithful — and one plugin then "
        "writes outputs under another's name, and moves the other's watermark.",
        pkgs=PKGS + ("./internal/plugins/", "./internal/worker/"),
    ),
    # ---- S6 of the transcripts plan: outputs rendered, the deletion cascade, the withdraw route ---
    # The plan's six rows, by name. `internal/ui` is in `PKGS`; every row ADDS the package holding
    # its other killers (the S2/S3/S5 rows' per-row rule).
    Mutant(
        name="plugins-output-rendered-without-predicate",
        path="internal/transcript/plugin.go",
        old="\t\tcase viewer.owns(meta):\n\t\t\tout[root] = &View{Meta: meta, a: s.archive}",
        new="\t\tcase true:\n\t\t\tout[root] = &View{Meta: meta, a: s.archive}",
        killer="TestAnOutputIsShownToExactlyTheViewersOfItsTranscript",
        why="a summary is a few sentences ABOUT a session, not the session's content, so showing it on "
        "every row that lists the session reads as harmless context — and a reader of ONE scope the "
        "session wrote then reads a digest of everything it did in the scopes they cannot read "
        "(clause k).",
        pkgs=PKGS + ("./internal/transcript/", "./internal/transcript/archive/"),
    ),
    Mutant(
        name="plugins-delete-keeps-outputs",
        path="internal/transcript/archive/sweep.go",
        old="\tif err := os.RemoveAll(dir); err != nil {\n\t\treturn err\n\t}\n\ta.adjust(-freed)\n\treturn nil\n}",
        new="\tkept, _ := os.ReadFile(filepath.Join(dir, outputsName))\n"
        "\tif err := os.RemoveAll(dir); err != nil {\n\t\treturn err\n\t}\n"
        "\tif kept != nil {\n\t\t_ = os.MkdirAll(dir, dirMode)\n"
        "\t\t_ = os.WriteFile(filepath.Join(dir, outputsName), kept, fileMode)\n\t}\n"
        "\ta.adjust(-freed)\n\treturn nil\n}",
        killer="TestTheDeletionCascadeTakesEverythingDerived",
        extra_killers=("TestTheOwnerDeletesATranscriptAndEverythingDerivedGoesWithIt",
                       "TestAWithdrawalDeletesOnlyTheCallersOwnRoot"),
        why="the outputs are the plugin's work and cost money to make, so keeping them when the raw "
        "transcript goes reads as the considerate delete — and the summary of a transcript its owner "
        "deleted, and every ticket edge mined from it, stays on the instance (clause l).",
        pkgs=PKGS + ("./internal/transcript/", "./internal/transcript/archive/", "./internal/worker/"),
    ),
    Mutant(
        name="transcript-withdraw-keeps-directory",
        path="internal/transcript/archive/sweep.go",
        old="\t\treturn true, a.deleteLocked(m, ReasonWithdrawn)",
        new="\t\treturn true, nil",
        killer="TestAWithdrawalDeletesOnlyTheCallersOwnRoot",
        extra_killers=("TestAWithdrawalIsOwnerAndHostBoundAndIdempotent", "TestTheAgentWithdrawsThroughTheRealRoute"),
        why="the agent stops shipping the session there, so acknowledging the withdrawal and leaving "
        "the bytes for retention reads as enough — and a prefix the routing rule says must not be on "
        "this instance stays on it for ninety days, with the agent told it is gone (clause m).",
        pkgs=PKGS + ("./internal/worker/", "./internal/transcript/archive/", "./internal/capture/"),
    ),
    Mutant(
        name="transcript-withdraw-deletes-another-owners-root",
        path="internal/transcript/archive/sweep.go",
        old="\t\tif !owned(m, who) {\n\t\t\treturn false, nil\n\t\t}\n\t\treturn true, a.deleteLocked(m, ReasonWithdrawn)",
        new="\t\tif m.Host != who.Host {\n\t\t\treturn false, nil\n\t\t}\n\t\treturn true, a.deleteLocked(m, ReasonWithdrawn)",
        killer="TestAWithdrawalDeletesOnlyTheCallersOwnRoot",
        extra_killers=("TestAWithdrawalIsOwnerAndHostBoundAndIdempotent",),
        why="the listener admits one capture owner, so the token already IS the owner and only the host "
        "looks left to check — and the day a second owner's root is in the store (a changed "
        "`-worker-owner`, a restored volume), any capture token deletes it by id.",
        pkgs=PKGS + ("./internal/worker/", "./internal/transcript/archive/"),
    ),
    Mutant(
        name="transcript-withdraw-host-unchecked",
        path="internal/transcript/archive/sweep.go",
        old="\t\tif !owned(m, who) {\n\t\t\treturn false, nil\n\t\t}\n\t\treturn true, a.deleteLocked(m, ReasonWithdrawn)",
        new="\t\tif m.Owner != who.Owner {\n\t\t\treturn false, nil\n\t\t}\n\t\treturn true, a.deleteLocked(m, ReasonWithdrawn)",
        killer="TestAWithdrawalDeletesOnlyTheCallersOwnRoot",
        extra_killers=("TestAWithdrawalIsOwnerAndHostBoundAndIdempotent",),
        why="every host's token is the SAME owner's, so the owner's sessions look like one pool any of "
        "their hosts may tidy — and a token copied to (or minted for) another machine deletes "
        "transcripts that machine never uploaded (the precedent: `transcript-capture-token-host-unchecked`).",
        pkgs=PKGS + ("./internal/worker/", "./internal/transcript/archive/"),
    ),
    Mutant(
        name="transcript-withdraw-repeat-answers-false",
        path="internal/transcript/archive/sweep.go",
        old="\treturn a.deletedFor(who, root), nil\n}",
        new="\treturn false, nil\n}",
        killer="TestAWithdrawalDeletesOnlyTheCallersOwnRoot",
        extra_killers=("TestAWithdrawalIsOwnerAndHostBoundAndIdempotent",),
        why="the root is gone, so 'nothing of yours is held here' is the literal truth on the second "
        "call — and the agent, which treats `false` as final, turns a withdrawal whose acknowledgement "
        "was lost into a permanent hold (the plan's revision 4, retracted).",
        pkgs=PKGS + ("./internal/worker/", "./internal/transcript/archive/"),
    ),
    # Beyond the plan's six: the owner arm of the delete, the agent's withdrawal rules, the S5 audit
    # findings S6 closed (#270: the walk-skip, the arc veto's snapshot, the output totals, the 507
    # line), and #273's own audit (a deletion is final for its root, a withdrawal reaches a stopped
    # instance, no environment proxy, an arc toggle is saved only over the list its page showed).
    Mutant(
        name="transcript-delete-skips-the-owner-arm",
        path="internal/transcript/plugin.go",
        old="\treturn s.archive.Delete(root, archive.ReasonOwnerDelete, viewer.owns)",
        new="\treturn s.archive.Delete(root, archive.ReasonOwnerDelete, nil)",
        killer="TestTheOwnerDeletesATranscriptAndEverythingDerivedGoesWithIt",
        why="the form renders only on the owner's page, so the row looks reachable only by the owner — "
        "and any signed-in principal who knows a session id (they are on session pages) deletes its "
        "transcript with one POST.",
        pkgs=PKGS + ("./internal/transcript/", "./internal/transcript/archive/"),
    ),
    Mutant(
        name="capture-binding-not-saved-before-upload",
        path="internal/capture/agent.go",
        old="\tif ss.Instance != dec.Instance {\n\t\tss.Instance = dec.Instance\n"
        "\t\tif err := a.saveRouting(root); err != nil {\n\t\t\treturn false, false, err\n\t\t}\n\t}",
        new="\tif ss.Instance != dec.Instance {\n\t\tss.Instance = dec.Instance\n\t}",
        killer="TestWhereASessionIsBoundIsSavedBeforeItsFirstUpload",
        why="the state is saved within two seconds anyway and a save per new session costs a write, so "
        "leaving the binding to the spaced checkpoint reads as the efficient version — and a run killed "
        "in that window has shipped a prefix under a state that says 'nowhere', so a later routing "
        "change withdraws nothing (S3b's audit).",
        pkgs=PKGS + ("./internal/capture/",),
    ),
    Mutant(
        name="capture-false-withdrawal-reships",
        path="internal/capture/agent.go",
        old="\tif !deleted && ss.shipped() {",
        new="\tif false && !deleted && ss.shipped() {",
        killer="TestAWithdrawalAnsweredFalseHoldsTheSessionForGood",
        why="`false` means the instance holds nothing of this host's, which reads as 'nothing left to "
        "withdraw, carry on' — and a prefix that IS there under another host label is then duplicated "
        "onto a second instance instead of being surfaced (decision 16).",
        pkgs=PKGS + ("./internal/capture/",),
    ),
    Mutant(
        name="capture-plaintext-host-matched-by-suffix",
        path="internal/capture/httpsink.go",
        old="\t\t\tif !slices.Contains(plaintextHosts, u.Hostname()) {",
        new="\t\t\tif !slices.ContainsFunc(plaintextHosts, func(h string) bool { return strings.HasSuffix(u.Hostname(), h) }) {",
        killer="TestPlainHTTPToANamedHostIsAnExplicitExactOptIn",
        why="an operator with several hosts on one mesh would rather name the mesh's domain once, so a "
        "suffix match reads as the convenient form — and `evil-<host>`, or any host under a parent "
        "domain somebody else can register in, then receives the capture token in clear.",
        pkgs=PKGS + ("./internal/capture/",),
    ),
    Mutant(
        name="plugins-store-walk-skipped-on-a-stored-off",
        path="internal/transcript/plugin.go",
        old="\n\tids := append([]string{meta.Root}, meta.Children...)\n\twritten, err := WrittenScopes(s.storeRoot, ids)\n",
        new="\n\tif stored := VisibilitySet(meta, Written{}); len(stored) > 0 && !on(meta, stored) {\n\t\treturn nil, nil\n\t}\n"
        "\tids := append([]string{meta.Root}, meta.Children...)\n\twritten, err := WrittenScopes(s.storeRoot, ids)\n",
        killer="TestAnArcHomeFoundOnlyByTheWalkDoesNotChangeTheAnswer",
        why="'OFF over the scopes already known' looks like a sound reason to skip a whole-store walk — it "
        "was the shipped rule, under a comment calling the fold monotone — and a session whose arc home "
        "only the walk finds is answered OFF until an unrelated page view records the scope (#270).",
        pkgs=PKGS + ("./internal/plugins/", "./internal/worker/", "./internal/transcript/"),
    ),
    Mutant(
        name="plugins-pending-walk-skipped-on-a-stored-off",
        path="internal/transcript/plugin.go",
        old="\t\tneed = append(need, meta)\n",
        new="\t\tif stored := VisibilitySet(meta, Written{}); len(stored) > 0 && !on(meta, stored) {\n\t\t\tcontinue\n\t\t}\n"
        "\t\tneed = append(need, meta)\n",
        killer="TestAnArcHomeFoundOnlyByTheWalkDoesNotChangeTheAnswer",
        why="the pending query is where the walk's cost is felt, so skipping roots the stored half "
        "already refuses is the tempting half of the same shortcut — and those sessions are never "
        "offered to the plugin until something else records the scope (#270).",
        pkgs=PKGS + ("./internal/plugins/", "./internal/worker/", "./internal/transcript/"),
    ),
    Mutant(
        name="plugins-arc-veto-lifted-by-removing-the-member",
        path="internal/plugins/service.go",
        old="\tfor _, k := range st.ArcsVetoing(plugin, ids) {",
        new="\tfor _, k := range []arcs.Key(nil) {",
        killer="TestAnArcVetoOutlivesTheMembersRemovalAndAnArcOnIsReadLive",
        why="an arc toggle reads as a statement about the arc AS IT IS, so folding it over the live member "
        "list is the natural rule — and a writer of the home, who can PUT the arc without a session, "
        "lifts an admin's veto with no admin act (#270, T14).",
        pkgs=PKGS + ("./internal/plugins/",),
    ),
    Mutant(
        name="transcript-upload-resurrects-a-deleted-root",
        path="internal/transcript/archive/archive.go",
        old="\tif m != nil {\n\t\treturn nil\n\t}\n\ta.mu.Lock()\n\twas, deleted := a.deleted[root]",
        new="\tif m != nil || true {\n\t\treturn nil\n\t}\n\ta.mu.Lock()\n\twas, deleted := a.deleted[root]",
        killer="TestADeletedSessionIsNotResurrectedByALaterUpload",
        extra_killers=("TestADeletionIsFinalForItsRoot", "TestASessionDeletedOnItsInstanceIsNeverSentAgain"),
        why="a root with no meta is a root nobody holds, which is exactly what a first upload looks like — "
        "and a stream or blob the agent sends from its start after the owner's delete re-creates the "
        "session the owner deleted (#273).",
        pkgs=PKGS + ("./internal/worker/", "./internal/transcript/archive/", "./internal/capture/"),
    ),
    Mutant(
        name="capture-deleted-answer-retried-forever",
        path="internal/capture/agent.go",
        old="\tif errors.Is(err, ErrDeleted) {\n\t\ta.sawDeleted = true",
        new="\tif errors.Is(err, ErrDeleted) {\n\t\ta.sawDeleted = false",
        killer="TestASessionDeletedOnItsInstanceIsNeverSentAgain",
        why="every other refusal is one session's failure for one run and is retried, so treating the 410 "
        "the same reads as consistent — and a session deleted on its instance is re-read, re-redacted "
        "and refused every minute for as long as it exists on the host.",
        pkgs=PKGS + ("./internal/capture/",),
    ),
    Mutant(
        name="capture-withdrawal-not-sent-to-a-stopped-instance",
        path="internal/capture/agent.go",
        old="\treturn a.stopped(ss.Instance) && !ss.shipped()",
        new="\treturn a.stopped(ss.Instance)",
        killer="TestAWithdrawalReachesAnInstanceThatIsNotTakingUploads",
        why="an instance that refuses uploads has nothing to gain from a session being read, so skipping "
        "every session bound for it is the cheap rule — and a prefix on a quota-full or disarmed "
        "instance is never withdrawn while it stays that way (#273).",
        pkgs=PKGS + ("./internal/capture/",),
    ),
    Mutant(
        name="capture-sink-uses-the-environment-proxy",
        path="internal/capture/httpsink.go",
        old="\t\ttransport.Proxy = nil\n",
        new="",
        killer="TestNoEnvironmentProxyEverCarriesTheCaptureToken",
        why="honouring HTTP_PROXY is what every well-behaved client does — and for a plain-http worker "
        "URL the proxy named by a stray variable receives the capture token and every transcript byte "
        "in clear (#273).",
        pkgs=PKGS + ("./internal/capture/",),
    ),
    Mutant(
        name="plugins-arc-toggle-saved-from-a-stale-page",
        path="internal/ui/plugins.go",
        old="\tif r.PostFormValue(FieldMembers) != arcMembersDigest(memberSessions(rep.Reg)) {",
        new="\tif false && r.PostFormValue(FieldMembers) != arcMembersDigest(memberSessions(rep.Reg)) {",
        killer="TestAnArcToggleFromAStaleTabIsNotSaved",
        why="the handler re-reads the arc and checks admin on its home, which reads as binding enough — "
        "and an admin's `on` from a tab opened before a writer added a session turns the plugin on for "
        "a session that admin was never shown (#273).",
        pkgs=PKGS + ("./internal/plugins/",),
    ),
    Mutant(
        name="ui-plugin-text-bidi-controls-rendered",
        path="internal/ui/outputs.go",
        old="case plugins.BidiControl(r):\n\t\t\treturn -1",
        new="case plugins.BidiControl(r):\n\t\t\treturn r",
        killer="TestBidiControlsInPluginTextAreNotRendered",
        why="the text is escaped, so it looks inert — and a right-to-left override in a model's summary "
        "reorders how the page's own `derived` label and what follows it are displayed.",
    ),
    Mutant(
        name="plugins-arc-toggle-snapshot-not-taken",
        path="internal/ui/plugins.go",
        old="plugins.ArcTarget(rep.Reg.Key()), value, vetoSnapshot(value, rep.Reg)...) {",
        new="plugins.ArcTarget(rep.Reg.Key()), value) {",
        killer="TestAnArcVetoSnapshotsItsMembersAndThePageSaysWhoIsStillVetoed",
        why="the journal line already names the arc, and the arc's members are one registry read away, "
        "so recording them again looks redundant — and with no snapshot a veto covers only what the arc "
        "lists NOW, so a writer of the home lifts it by re-registering the arc without the session.",
        pkgs=PKGS + ("./internal/plugins/",),
    ),
    Mutant(
        name="transcript-output-totals-not-reduced-on-delete",
        path="internal/transcript/archive/sweep.go",
        old="\tif err := a.forgetOutputs(m.Root); err != nil {\n\t\treturn err\n\t}\n",
        new="",
        killer="TestTheOutputTotalsFollowDeletionsAndARestart",
        why="the totals only bound growth, so an over-count after a deletion reads as erring on the safe "
        "side — and a plugin whose outputs were all deleted with their sessions stays refused 507 until "
        "the pod restarts, which is the pressure that brought the per-refusal rescan in (#270).",
        pkgs=PKGS + ("./internal/transcript/archive/",),
    ),
    Mutant(
        name="worker-507-line-names-one-limit",
        path="internal/worker/plugin.go",
        old="\tswitch q.Share {\n\tcase transcript.SharePlugin:",
        new="\tswitch q.Share {\n\tdefault:",
        killer="TestThe507LineNamesWhichLimitWasHit",
        why="every 507 on this route is about a plugin's write, so one line naming the plugin reads as "
        "enough — and an INSTANCE full of transcripts sends the operator to a plugin that wrote nothing (#270).",
        pkgs=PKGS + ("./internal/worker/", "./internal/transcript/archive/"),
    ),
    # 🔴 THE AGENT READ API (S8 of the transcripts plan): the plan's two rows. ⚠ NEITHER CAN BE A
    # LISTENER-LOCAL "SKIP THE PREDICATE" EDIT, AND THAT IS #261's DESIGN RATHER THAN A GAP: a stored
    # record leaves `internal/transcript` only inside a `View`, and `Store.View` is the only constructor
    # OUTSIDE the package — so no compiling edit in `internal/ui` or `internal/clientapi` serves a
    # session without asking. ⚠ Inside the package one does compile (`Answer` building a `View` from
    # `ReadMeta` directly), and `TestTheAgentReadRoutesAskThePredicate` would see it, but it is not a
    # row here. Each row
    # therefore breaks the PREDICATE the read API asks, in a shape a reader of that API would plausibly
    # write, and names the S8 surface test as its killer: what it proves is that the JSON routes'
    # tests SEE a predicate that stopped refusing, on the route each row is named for.
    Mutant(
        name="transcript-read-api-without-predicate",
        path="internal/transcript/visible.go",
        old="\tif viewer.owns(meta) {\n\t\treturn true\n\t}",
        new="\tif true || viewer.owns(meta) {\n\t\treturn true\n\t}",
        killer="TestTheAgentReadRoutesAskThePredicate",
        why="the read API is for the owner's own agents, so 'an authenticated caller is reading its own "
        "sessions' reads as the rule — and every credential then reads every stored transcript, a stranger's "
        "and a narrowed agent's included.",
        pkgs=PKGS + ("./internal/transcript/", "./internal/transcript/archive/"),
    ),
    Mutant(
        name="transcript-raw-view-without-predicate",
        path="internal/transcript/visible.go",
        old="\tif len(v) == 0 {\n\t\treturn false\n\t}\n\treturn viewer.readsEvery(v)\n}",
        new="\tif len(v) == 0 {\n\t\treturn false\n\t}\n\treturn true\n}",
        killer="TestTheRawRecordsViewAsksThePredicate",
        extra_killers=("TestTheRawRecordViewAsksThePredicate",),
        why="a session that wrote or read SOMETHING has been vouched for by a scope, so admitting anyone "
        "once the empty-set case is refused reads as the rule's remainder — and the stored bytes, `view=raw` "
        "included, then reach a reader of ONE scope the session touched (clause d).",
        pkgs=PKGS + ("./internal/transcript/", "./internal/transcript/archive/"),
    ),
    # ---- transcripts S7: "My sessions" (O10, clause g — the listing half) ----------------------
    Mutant(
        name="my-sessions-lists-another-owner",
        path="internal/transcript/mine.go",
        old="\t\tif !viewer.owns(meta) {",
        new="\t\tif v, _ := s.View(viewer, root); v == nil {",
        killer="TestMySessionsListsOwnershipNotVisibility",
        why="the visibility predicate is THE gate on transcript content, so listing every session it "
        "admits reads as the careful version of the page — and turns a reader's \"My sessions\" into a "
        "feed of every session over their readable scopes that somebody else uploaded.",
        extra_killers=("TestMySessionsListsTheViewersOwnAndNoOneElses",),
        pkgs=PKGS + ("./internal/transcript/",),
    ),
    Mutant(
        name="redact-marker-drops-the-whole-span",
        path="internal/redact/marker.go",
        old="\t\tif m[0] < sp.lo {",
        new="\t\tif m[0] < sp.hi {",
        killer="TestAMarkerDoesNotShieldASecretBesideIt",
        why="a span that touches a marker reads as ABOUT the marker, so dropping it whole looks like the "
        "obvious way to keep the table quiet on its own output — and it ships a key-context value "
        "glued directly before a marker, which the base caught.",
        pkgs=PKGS + ("./internal/redact/",),
    ),
    # ---- scope mail S1: VERB narrowing (decision 17), the revoke command (decision 16), and the
    # memo predicates (decisions 9, 10, 12) — `internal/memo` is in `PKGS` for these ----------
    Mutant(
        name="narrow-ignores-its-verb-argument",
        path="internal/control/resolve.go",
        old="\t\t\tvs = vs.Intersect(*verbs)\n",
        new="\t\t\tvs = vs.Intersect(AllSet)\n",
        killer="TestAVerbNarrowedCredentialLacksAdmin",
        extra_killers=("TestAVerbNarrowedAgentCredentialCannotRevokeAGrant",
                       "TestMayRetractComposesTheSenderAndTheModerator",
                       "TestThePredicatesAreRelationshipsOverOneModel"),
        why="the base behaviour: scope narrowing kept each scope's FULL verb set, so an agent "
        "credential carried admin — `POST /unshare` and memo moderation both reachable (decision 17).",
    ),
    Mutant(
        name="narrow-returns-early-on-a-nil-scope-list",
        path="internal/control/resolve.go",
        old="\tif only == nil && verbs == nil {\n",
        new="\tif only == nil {\n",
        killer="TestVerbsOnlyNarrowingIsNotPassedThrough",
        extra_killers=("TestAVerbNarrowedAgentCredentialCannotRevokeAGrant",),
        why="the base `Narrow` returned on a nil scope list, so a credential narrowed by VERBS alone "
        "passed through un-narrowed and kept admin (round 2 🟡1).",
    ),
    Mutant(
        name="authenticate-drops-the-credentials-verbs",
        path="internal/control/resolve.go",
        old="matched.NarrowedScopes, matched.NarrowedVerbs), nil",
        new="matched.NarrowedScopes, nil), nil",
        killer="TestAVerbNarrowedCredentialLacksAdmin",
        why="the narrowing is recorded and applied in the model and then not passed at the one place "
        "authority is computed — every surface would serve the un-narrowed credential.",
    ),
    Mutant(
        name="verbs-narrowed-replaces-rather-than-intersects",
        path="internal/control/journal.go",
        old="\t\t\tverbs = c.NarrowedVerbs.Intersect(e.Verbs)\n",
        new="\t\t\tverbs = e.Verbs\n",
        killer="TestVerbNarrowingIntersectsAndASecondEventCannotWiden",
        why="'the latest event is the credential's verbs' reads naturally — and a second, wider "
        "line then hands admin back (round 2 🟡1 (1)).",
    ),
    Mutant(
        name="verbs-narrowed-to-nothing-accepted",
        path="internal/control/journal.go",
        old="\t\tif e.Verbs.Empty() {\n\t\t\treturn fmt.Errorf(\"%s: verbs is empty — a credential that should reach nothing",
        new="\t\tif false {\n\t\t\treturn fmt.Errorf(\"%s: verbs is empty — a credential that should reach nothing",
        killer="TestAVerbNarrowingOfNothingOrOfNobodyIsRefusedAndAppendsNothing",
        why="an empty set is a quieter second spelling of revocation that no surface reports as one.",
    ),
    Mutant(
        name="issue-drops-the-verb-narrowing-event",
        path="internal/control/credential_issue.go",
        old="\tif req.NarrowedVerbs != nil {\n",
        new="\tif false && req.NarrowedVerbs != nil {\n",
        killer="TestIssuingWithNarrowedVerbsAppendsBothEventsInOneBatch",
        extra_killers=("TestNarrowVerbsIssuesACredentialWithoutAdmin",),
        why="the flag parsed, the record line printing `verbs=read,write`, and the journal never told "
        "— an agent credential issued with admin while the operator was told otherwise.",
    ),
    Mutant(
        name="issue-command-accepts-narrow-scopes-without-narrow-verbs",
        path="cmd/cairn-server/issuecredential.go",
        old="\tif narrowed != nil && verbs == nil {\n",
        new="\tif false && narrowed != nil && verbs == nil {\n",
        killer="TestNarrowScopesWithoutNarrowVerbsIsRefusedAndWritesNothing",
        why="'verbs are optional' is the obvious flag design, and it is how a scope-narrowed "
        "agent credential inherits admin without anybody deciding it should (decision 17).",
    ),
    Mutant(
        name="revoke-reports-success-without-appending",
        path="internal/control/credential_revoke.go",
        old="\tafter, err := s.Append(ctx, Event{Kind: EventCredentialRevoked, At: at, Actor: actor, CredentialID: id})\n",
        new="\tafter, err := s.Model(ctx)\n",
        killer="TestRevokeCredentialAppendsOneEventAndRefusesUnknownAndRevoked",
        extra_killers=("TestRevokeCredentialAppendsOneEventPrintsTheLabelAndTheTokenStopsAuthenticating",
                       "TestTheBinaryActuallyDispatchesIssueCredential"),
        why="a revocation that prints the label and exits 0 is the whole operator-visible surface; "
        "only the journal and a re-authentication can tell the host was never cut off.",
    ),
    Mutant(
        name="revoke-accepts-an-already-revoked-credential",
        path="internal/control/credential_revoke.go",
        old="\tif !c.Live() {\n",
        new="\tif false {\n",
        killer="TestRevokeCredentialAppendsOneEventAndRefusesUnknownAndRevoked",
        extra_killers=("TestRevokeCredentialRefusesUnknownAndAlreadyRevokedInTheirOwnWordsAndAppendsNothing",),
        why="`apply` accepts a second revocation as a no-op, so success is the natural answer — and "
        "it tells an operator who typed the dead credential's id that the live one is cut off.",
    ),
    Mutant(
        name="memo-maysend-asks-read",
        path="internal/memo/policy.go",
        old="\treturn a.VisibleScopes(control.VerbWrite).Allows(scope)\n",
        new="\treturn a.VisibleScopes(control.VerbRead).Allows(scope)\n",
        killer="TestThePredicatesAreRelationshipsOverOneModel",
        # S5: the browser tab asks this predicate to decide the send FORM, so the page notices too.
        # ⚠ RE-POINTED (#242 round 0, D3): this named a UI test that drove a forged send over a fake
        # store which called `memo.MaySend` ITSELF — the kill was the fake's, not the surface's.
        extra_killers=("TestAReadOnlyViewerSeesTheMemosTabButNoSendForm",),
        why="a reader sending looks harmless — and O2 is that sending needs exactly the entry-write "
        "predicate (e2e sabotage (b)).",
    ),
    Mutant(
        name="memo-mayread-asks-write",
        path="internal/memo/policy.go",
        old="\treturn a.VisibleScopes(control.VerbRead).Allows(scope)\n",
        new="\treturn a.VisibleScopes(control.VerbWrite).Allows(scope)\n",
        killer="TestThePredicatesAreRelationshipsOverOneModel",
        extra_killers=("TestMayRetractComposesTheSenderAndTheModerator",),
        why="one verb confused for the other at the second of two near-identical lines.",
    ),
    Mutant(
        # S2 moved the read clause out of the sender branch to gate BOTH retractors (the S1 "rule in
        # two places" ruling), so this one row now covers the sender narrowed away AND the admin-only
        # grantee; the killer carries a case for each.
        name="memo-mayretract-needs-no-read",
        path="internal/memo/policy.go",
        old="\tif !MayRead(a, scope) {\n\t\treturn false\n",
        new="\tif false && !MayRead(a, scope) {\n\t\treturn false\n",
        killer="TestMayRetractComposesTheSenderAndTheModerator",
        why="'the sender may always retract' and 'an admin may always moderate' — through a credential "
        "narrowed AWAY from the scope, or an admin-only grant with no read, which decision 10 and the "
        "uniform miss (T12) both refuse.",
    ),
    Mutant(
        name="memo-mayretract-without-the-moderator-rule",
        path="internal/memo/policy.go",
        old="\treturn a.VisibleScopes(control.VerbAdmin).Allows(scope)\n",
        new="\treturn false\n",
        killer="TestMayRetractComposesTheSenderAndTheModerator",
        why="O8 names two retractors; a predicate written from the sender case alone drops moderation.",
    ),
    Mutant(
        name="memo-mayretract-ignores-the-senders-kind",
        path="internal/memo/policy.go",
        old="string(p.Kind) == senderKind && ",
        new="",
        killer="TestMayRetractComposesTheSenderAndTheModerator",
        why="ids look unique, but a project and a user are two principals; the stable key is (kind, id).",
    ),
    Mutant(
        name="memo-send-scan-skipped",
        path="internal/memo/policy.go",
        old="\t\t_, hits := scan.String(text)\n",
        new="\t\tvar hits []redact.Hit\n\t\t_ = text\n",
        killer="TestEachConfidentRuleRefusesAndTheTextWithoutTheValueIsAccepted",
        why="the positive control that the scan RUNS at all (e2e sabotage (e)).",
    ),
    Mutant(
        name="memo-send-refuses-on-any-redactor-hit",
        path="internal/memo/policy.go",
        old="\t\t\tif confidentSecretRules[h.Rule] {\n",
        new="\t\t\tif h.Rule != \"\" {\n",
        killer="TestNonConfidentHitsAreAccepted",
        why="'refuse anything the redactor flags' is the safe-sounding rule, and it turns away honest "
        "memos at the measured ~10% clean-line damage rate (decision 11).",
    ),
    Mutant(
        name="memo-unsafe-passes-default-ignorable-letters",
        path="internal/memo/predicates.go",
        old="\tcase unicode.Is(unicode.Other_Default_Ignorable_Code_Point, r):\n",
        new="\tcase false:\n",
        killer="TestEveryDefaultIgnorableCodePointIsUnsafe",
        extra_killers=("TestUnsafeByCategoryIncludingTagsAndVariationSelectors",),
        why="the category test reads as complete — and U+3164, an invisible LETTER, passed it (S0 audit).",
    ),
    # ---- scope mail S2: the CLIENT LISTENER (`internal/clientapi`, `cmd/cairn-ui -client-api-addr`) ----
    # 🔴 EVERY `internal/clientapi` ROW CARRIES `pkgs` RATHER THAN `PKGS` GROWING, the codesrc rows'
    # convention: the listener asks no authz predicate of its own (the store does), so it is not a
    # member of the seam — but its authentication, lockout, ledger and wire are guards, and these
    # rows prove each can go red. Its Postgres-tier guards (the uniform miss, the scope match, O9)
    # cannot run here — this battery runs no database — and their matrix is in the S2 PR.
    Mutant(
        name="clientapi-ledger-grows-a-route",
        path="internal/clientapi/clientapi.go",
        old="\t{http.MethodPost, RetractPath}: (*Handler).retract,\n",
        new="\t{http.MethodPost, RetractPath}: (*Handler).retract,\n\t{http.MethodDelete, MemoPath}: (*Handler).retract,\n",
        killer="TestTheClientRouteLedgerMatchesTheDispatchTable",
        pkgs=PKGS + ("./internal/clientapi/",),
        why="a DELETE spelling of retract reads as REST hygiene — and it is an endpoint an agent "
        "credential reaches that nobody wrote down.",
    ),
    Mutant(
        name="clientapi-lockout-never-consulted",
        path="internal/clientapi/clientapi.go",
        old="\tif h.cfg.Limiter.LockedOut(client) {\n",
        new="\tif false && h.cfg.Limiter.LockedOut(client) {\n",
        killer="TestFailedCredentialsLockTheClientOut",
        pkgs=PKGS + ("./internal/clientapi/",),
        why="a valid token is valid, so checking the lockout first looks like it only refuses honest "
        "callers — and it is the whole of the guessing bound (T9).",
    ),
    Mutant(
        name="clientapi-failure-never-recorded",
        path="internal/clientapi/clientapi.go",
        old="\t\tif h.cfg.Limiter.RecordFailure(client) {\n",
        new="\t\tif false {\n",
        killer="TestFailedCredentialsLockTheClientOut",
        pkgs=PKGS + ("./internal/clientapi/",),
        why="the line reads as logging; the call inside the condition is what feeds the lockout.",
    ),
    Mutant(
        name="clientapi-wire-encodes-stored",
        path="internal/clientapi/clientapi.go",
        old="\twriteJSON(w, http.StatusOK, MemoBody{Memo: memo.Render(st)})\n",
        new="\twriteJSON(w, http.StatusOK, MemoBody{}); _ = st\n",
        killer="TestTheWireCarriesRenderedTextNeverStored",
        pkgs=PKGS + ("./internal/clientapi/",),
        why="decision 19, Guard C: a body built without `memo.Render` carries none of the rendered "
        "fields (encoding `memo.Stored` itself gives `{}`, the same observable).",
    ),
    Mutant(
        name="cairn-ui-client-api-without-a-journal-admitted",
        path="cmd/cairn-ui/clientapi.go",
        old="\tif journal == \"\" {\n",
        new="\tif false && journal == \"\" {\n",
        killer="TestTheClientAPIFlagNeedsAJournalADatabaseAndAnAllowlist",
        extra_killers=("TestTheBinaryRefusesEachClientAPIMisconfiguration",),
        why="the flag only needs a database to store memos, so the journal looks optional — and "
        "without it the authority is the token file's shared row (O5, reversed by O9).",
    ),
    Mutant(
        name="cairn-ui-client-api-without-a-database-admitted",
        path="cmd/cairn-ui/clientapi.go",
        old="\tif dsn == \"\" {\n",
        new="\tif false && dsn == \"\" {\n",
        killer="TestTheClientAPIFlagNeedsAJournalADatabaseAndAnAllowlist",
        extra_killers=("TestTheBinaryRefusesEachClientAPIMisconfiguration",),
        why="the listener would still bind and announce itself, and answer every route with an error.",
    ),
    Mutant(
        name="cairn-ui-client-api-bind-reachability-unchecked",
        path="cmd/cairn-ui/clientapi.go",
        old="\treturn listenerBindRefusal(\"client API listener\", flagClientAPIAddr, addr, proxyErr)\n",
        new="\treturn nil\n",
        killer="TestTheClientAPIFlagNeedsAJournalADatabaseAndAnAllowlist",
        extra_killers=("TestTheBinaryRefusesEachClientAPIMisconfiguration",),
        why="the browser listener's bind already passed this check — and a loopback browser bind says "
        "nothing about a 0.0.0.0 client bind.",
    ),
    Mutant(
        name="cairn-ui-main-ignores-the-client-api-refusal",
        path="cmd/cairn-ui/main.go",
        old="trustedProxiesErr()); err != nil {",
        new="trustedProxiesErr()); false && err != nil {",
        killer="TestTheBinaryRefusesEachClientAPIMisconfiguration",
        why="the predicate is tested in-process, so the call looks covered — whether `main` ACTS on it "
        "is wiring only a re-exec can see.",
    ),
    # ---- scope mail S3: the memo VERBS (`internal/client/memo.go`, `memocursor.go`) ----
    # 🔴 EVERY ROW HERE SURVIVED AT `0d9ba01` (S3 as merged) OR RE-INTRODUCES A BEHAVIOUR #239's
    # ROUND 1 FIXED, and each carries `pkgs` (the clientapi rows' convention): the client asks no
    # authz predicate, so it is not a member of the seam, but its cursor, paging, routing and wire
    # decoding are what deliver a memo exactly once to the right session, and these rows prove each
    # guard can go red. What only a real listener can show is `tests/memo/e2e.sh`.
    Mutant(
        name="memo-cursor-margin-removed",
        path="internal/client/memocursor.go",
        old="\treturn hw.Add(-memo.CursorMargin).UTC().Format(time.RFC3339Nano)\n",
        new="\treturn hw.UTC().Format(time.RFC3339Nano)\n",
        killer="TestALateCommitterInsideTheMarginIsDelivered",
        pkgs=PKGS + ("./internal/client/",),
        why="asking from the high-water itself looks exact, and the seen-set already drops repeats — "
        "but `created_at` is allocation order, so a memo that commits late is older than the mark and LOST.",
    ),
    Mutant(
        name="memo-check-after-never-sent",
        path="internal/client/memo.go",
        old="\tif after := cursor.after(); after != \"\" {\n\t\tq.Set(\"after\", after)\n",
        new="\tif after := cursor.after(); after != \"\" {\n\t\t_ = after\n",
        killer="TestTheCheckSendsItsCursorSoAPrunedMemoIsNotReDelivered",
        pkgs=PKGS + ("./internal/client/",),
        why="the seen-set alone looks sufficient for once-only delivery — until it prunes, and the "
        "listener's full list hands back a memo it no longer remembers, on every check.",
    ),
    Mutant(
        name="memo-check-401-reported-as-unconfigured",
        path="internal/client/memo.go",
        old="\t\treturn status(\"refused\", ExitUnreachableNoCache, fmt.Sprintf(",
        new="\t\treturn status(\"unconfigured\", ExitUnreachableNoCache, fmt.Sprintf(",
        killer="TestEveryStatusTokenHasACaseThatProducesIt",
        pkgs=PKGS + ("./internal/client/",),
        why="both are exit 3 and both say 'memos not checked', so one token reads as enough — but only a "
        "401 SENT a credential the listener's per-host lockout counted, and the hook backs a host off on it.",
    ),
    Mutant(
        name="memo-read-stops-after-one-page",
        path="internal/client/memo.go",
        old="\t\t\tif len(page) < memoListPage || added == 0 {\n",
        new="\t\t\tif true || added == 0 {\n",
        killer="TestMemoReadPagesPastOneListingPage",
        pkgs=PKGS + ("./internal/client/",),
        why="one page of 200 holds every LIVE memo of a scope (the live bound), so a second request "
        "looks dead — but the listing carries tombstones too, and the oldest 200 is all it would show.",
    ),
    Mutant(
        name="memo-read-page-boundary-at-the-last-time",
        path="internal/client/memo.go",
        old="\t\t\tafter = last.Add(-time.Second).UTC().Format(time.RFC3339Nano)\n",
        new="\t\t\tafter = last.UTC().Format(time.RFC3339Nano)\n",
        killer="TestMemoReadPagesPastOneListingPage",
        pkgs=PKGS + ("./internal/client/",),
        why="the ids already held are dropped anyway, so asking from the last time itself looks "
        "exact — and a memo sharing that whole second with the page's last one is never asked for.",
    ),
    Mutant(
        name="memo-check-config-from-the-default-instance",
        path="internal/client/memo.go",
        old="\tcfg, err := LoadUIConfigFor(alias)\n\tif err != nil {\n",
        new="\tcfg, err := LoadUIConfigFor(DefaultAlias)\n\tif err != nil {\n",
        killer="TestMemoCheckRoutesByScopeAcrossTwoInstances",
        pkgs=PKGS + ("./internal/client/",),
        why="a one-instance host cannot tell the two apart, and the id verbs' routing test exercises "
        "`memoRoute`, which `memo-check` does not call.",
    ),
    Mutant(
        name="memo-response-unknown-field-refused",
        path="internal/client/memo.go",
        old="\tdec := json.NewDecoder(bytes.NewReader(data))\n\tif err := dec.Decode(into); err != nil {\n",
        new="\tdec := json.NewDecoder(bytes.NewReader(data))\n\tdec.DisallowUnknownFields()\n"
        "\tif err := dec.Decode(into); err != nil {\n",
        killer="TestAResponseFieldTheClientDoesNotKnowIsTolerated",
        pkgs=PKGS + ("./internal/client/",),
        why="strict decoding is this repository's default everywhere else, and reads as hardening — "
        "here it couples every client release to every listener release (S3 as merged).",
    ),
    Mutant(
        name="memo-response-required-keys-unchecked",
        path="internal/client/memo.go",
        old="\t\tif v, ok := fields[k]; !ok || bytes.Equal(bytes.TrimSpace(v), []byte(\"null\")) {\n",
        new="\t\tif v, ok := fields[k]; false && (!ok || bytes.Equal(bytes.TrimSpace(v), []byte(\"null\"))) {\n",
        killer="TestAResponseMissingAFieldTheClientReadsIsMalformed",
        pkgs=PKGS + ("./internal/client/",),
        why="the decoder already fails on a wrong TYPE, so presence looks checked — and a renamed field "
        "decodes as the empty string with no error.",
    ),
    Mutant(
        name="memo-send-sub-second-ttl-sent-as-the-default",
        path="internal/client/memo.go",
        old="\tif strings.TrimSpace(opts.TTL) != \"\" && ttl/time.Second == 0 {\n",
        new="\tif false && strings.TrimSpace(opts.TTL) != \"\" && ttl/time.Second == 0 {\n",
        killer="TestASubSecondTTLIsRefusedNotSentAsTheDefault",
        pkgs=PKGS + ("./internal/client/",),
        why="the parser accepted the value, so the send looks right — and 0 seconds on the wire is the "
        "listener's three-day DEFAULT, at exit 0 (S3 as merged).",
    ),
    Mutant(
        name="memo-check-saves-the-cursor-before-printing",
        path="internal/client/memo.go",
        old="\tcode, _ := status(\"new\", ExitOK, notes...)\n\tfmt.Fprint(env.Stdout, memo.RenderPreview(stored))\n"
        "\tif problem := save(); problem != \"\" {\n",
        new="\tproblem := save()\n\tcode, _ := status(\"new\", ExitOK, notes...)\n"
        "\tfmt.Fprint(env.Stdout, memo.RenderPreview(stored))\n\tif problem != \"\" {\n",
        killer="TestTheBlockIsPrintedBeforeTheCursorIsSaved",
        pkgs=PKGS + ("./internal/client/",),
        why="saving first reads as tidy bookkeeping, and every output is identical — only a process "
        "killed between the two sees the order, and it LOSES the memos (S3 as merged).",
    ),
    Mutant(
        name="memo-check-derivation-error-dropped",
        path="internal/client/memo.go",
        old="\"no scope could be derived from --repo, so there is nothing to check: \"+err.Error())\n",
        new="\"no scope could be derived from --repo, so there is nothing to check\")\n",
        killer="TestADerivationErrorIsSaidAfterTheNoScopeToken",
        pkgs=PKGS + ("./internal/client/",),
        why="the token and the exit are the contract and both are unchanged, so the error looks like "
        "noise — and without it a broken `git` is indistinguishable from a directory that is no repo.",
    ),
    # ---- scope mail S5: the browser's MEMOS TAB, `POST /memo` and `POST /memo/retract` ----------
    # Its Postgres-tier guard (`TestTheMemoTabsForgedSendIsRefusedByTheRealStore`, the real binary over
    # the real store) cannot run here — this battery runs no database — and its matrix is in the S5 PR.
    Mutant(
        name="ui-memo-send-row-declared-public",
        path="internal/ui/routes.go",
        old='{"POST", MemoPath}:        {(*Server).handleMemoSend, 0},',
        new='{"POST", MemoPath}:        {(*Server).handleMemoSend, classPublic},',
        killer="TestTheMemoRowsAreBehindBothCrossSiteGates",
        extra_killers=("TestTheRouteLedgerMatchesTheDispatchTable",),
        why="a send form reads like any other form, and a class is a one-word edit — but a public row "
        "dispatches ahead of the chain and so ahead of the CSRF gate (the S5 plan's named bypass).",
    ),
    Mutant(
        name="ui-memo-retract-row-declared-public",
        path="internal/ui/routes.go",
        old='{"POST", MemoRetractPath}: {(*Server).handleMemoRetract, 0},',
        new='{"POST", MemoRetractPath}: {(*Server).handleMemoRetract, classPublic},',
        killer="TestTheMemoRowsAreBehindBothCrossSiteGates",
        extra_killers=("TestTheRouteLedgerMatchesTheDispatchTable",),
        why="retracting only REDUCES exposure, so exempting it reads as safe — and a cross-site "
        "retraction is still a write somebody else chose.",
    ),
    Mutant(
        name="ui-memo-tab-not-recognised",
        path="internal/ui/render.go",
        old="\tcase TabSessions, TabArcs, TabAgent, TabMemos:",
        new="\tcase TabSessions, TabArcs, TabAgent:",
        killer="TestAReadOnlyViewerSeesTheMemosTabButNoSendForm",
        extra_killers=("TestEachScopeTabRendersOnlyItsOwnPanel",),
        why="`ui-agent-tab-not-recognised`'s shape one tab over: the link still answers 200 with the "
        "entries tab, so nothing but the panel's own id can tell.",
    ),
    Mutant(
        name="ui-memo-form-ignores-maysend",
        path="internal/ui/memos.go",
        old="tab := MemoTab{CanSend: memo.MaySend(id.Auth, scope.Name),",
        new="tab := MemoTab{CanSend: memo.MayRead(id.Auth, scope.Name),",
        killer="TestAReadOnlyViewerSeesTheMemosTabButNoSendForm",
        why="'anyone who can see the tab can use it' is the obvious condition, and it offers a reader "
        "a form the store then refuses.",
    ),
    Mutant(
        name="ui-memo-retract-button-asks-maysend",
        path="internal/ui/memos.go",
        old='row.Retractable = row.Tombstone == "" && memo.MayRetractMemo(id.Principal, id.Auth, st)',
        new='row.Retractable = row.Tombstone == "" && memo.MaySend(id.Auth, scope.Name)',
        killer="TestTheRetractButtonFollowsMayRetractMemo",
        why="'writers may retract' is a plausible rule and it is not Q19's: it offers a member another's "
        "memo and hides a reader's own.",
    ),
    Mutant(
        name="ui-memo-retract-button-on-a-tombstone",
        path="internal/ui/memos.go",
        old='row.Retractable = row.Tombstone == "" && memo.MayRetractMemo(',
        new='row.Retractable = memo.MayRetractMemo(',
        killer="TestTheRetractButtonFollowsMayRetractMemo",
        why="the predicate does not know a memo is retracted, so it answers true for an admin and "
        "offers a button the store answers `not-found`.",
    ),
    Mutant(
        name="ui-memo-body-rendered-raw",
        path="internal/ui/memos.go",
        old='h.Data("memo", "body"), g.Text(row.Body))',
        new='h.Data("memo", "body"), g.Raw(row.Body))',
        killer="TestMemoTextRendersAsTextAndThroughTheRenderSideReplacement",
        extra_killers=("TestNoRawNodeConstructorAppearsInTheUIPackage",),
        why="the body is 'already rendered', so writing it raw reads as avoiding a double escape — and "
        "rendering stops invisible code points, not markup.",
    ),
    Mutant(
        name="memo-render-skips-the-body-replacement",
        path="internal/memo/stored.go",
        old="\n\tr.Body = sanitize(t.body)\n\treturn r\n}",
        new="\n\tr.Body = t.body\n\treturn r\n}",
        killer="TestMemoTextRendersAsTextAndThroughTheRenderSideReplacement",
        extra_killers=("TestEveryFieldsStringIsSanitisedOnEveryOutput",),
        why="the store's send-side fence already refused the dangerous runes, so the render-side pass "
        "reads as redundant — and the store holds Cf and variation selectors by design (decision 5).",
    ),
    Mutant(
        name="ui-memo-lapsed-mark-never-shown",
        path="internal/ui/memos.go",
        old="SenderLapsed: !memo.SenderMaySend(m, st)}",
        new="SenderLapsed: false}",
        killer="TestTheMemosTabIsNewestFirstWithTombstonesAndLapsedSenders",
        why="a memo stays delivered after the grant goes (decision 12), so the mark looks optional — "
        "it is the only thing telling a reader the sender has lost write.",
    ),
    # ⚠ `ui-memo-listing-boundary-not-one-second-early` STOOD HERE AND IS GONE WITH ITS CODE: the tab
    # no longer pages (the store answers a whole scope in one call, `pgstore.MemoStore.All`), so the
    # boundary rule has one spelling, the client's — `memo-read-page-boundary-at-the-last-time` above.
    # `All` is SQL; its mutants are in the S5 follow-up PR, run against PostgreSQL.
    Mutant(
        name="ui-memo-refusal-drops-the-draft",
        path="internal/ui/memos.go",
        old="\tif errors.As(err, &ref) && memoDraftKept[ref.Reason] && memo.Scannable(draft.Subject, draft.Body) {\n",
        new="\tif errors.As(err, &ref) && false && memo.Scannable(draft.Subject, draft.Body) {\n",
        killer="TestARefusedOverLongSendKeepsTheTypedTextInTheForm",
        extra_killers=("TestOnlyTheClosedSetOfRefusalsKeepsTheDraft",),
        why="every other write outcome is a redirect, so one uniform answer reads as tidier — and the "
        "form's maxlength counts UTF-16 units, so non-ASCII text it accepted is refused and lost.",
    ),
    Mutant(
        name="ui-memo-draft-subject-not-rendered",
        path="internal/ui/memos.go",
        old="h.MaxLength(strconv.Itoa(memo.MaxSubjectBytes)), h.Value(d.Subject)),",
        new="h.MaxLength(strconv.Itoa(memo.MaxSubjectBytes))),",
        killer="TestARefusedOverLongSendKeepsTheTypedTextInTheForm",
        why="the text is the long field, so restoring only it looks like the whole fix.",
    ),
    Mutant(
        name="ui-memo-draft-body-not-rendered",
        path="internal/ui/memos.go",
        old="h.MaxLength(strconv.Itoa(memo.MaxBodyBytes)), g.Text(d.Body)),",
        new="h.MaxLength(strconv.Itoa(memo.MaxBodyBytes))),",
        killer="TestARefusedOverLongSendKeepsTheTypedTextInTheForm",
        why="a textarea has no `value` attribute, so a form restored attribute by attribute comes back "
        "with its largest field empty.",
    ),
    Mutant(
        name="ui-memo-draft-kept-for-a-suspected-secret",
        path="internal/ui/memos.go",
        old="\tmemo.RefusedLiveLimit:   true,\n}",
        new="\tmemo.RefusedLiveLimit:   true,\n\tmemo.RefusedSecret:      true,\n}",
        killer="TestOnlyTheClosedSetOfRefusalsKeepsTheDraft",
        why="'never lose what the user typed' reads as the rule, applied evenly — and the page then "
        "serves back the text the scan just called a credential.",
    ),
    Mutant(
        name="memo-secret-scan-after-size",
        path="internal/memo/policy.go",
        # The defect IS an order, so the edit is the order: the scan block moved back below the
        # two size checks, as it stood at e18efd1. Nothing else in either block changes.
        old="\tfor _, text := range []string{subject, body} {\n"
        "\t\t_, hits := scan.String(text)\n"
        "\t\tfor _, h := range hits {\n"
        "\t\t\tif confidentSecretRules[h.Rule] {\n"
        "\t\t\t\treturn Outgoing{}, &Refusal{Reason: RefusedSecret, Rule: h.Rule}\n"
        "\t\t\t}\n"
        "\t\t}\n"
        "\t}\n"
        "\tif len(subject) > MaxSubjectBytes {\n"
        "\t\treturn Outgoing{}, &Refusal{Reason: RefusedSubjectSize}\n"
        "\t}\n"
        "\tif len(body) > MaxBodyBytes {\n"
        "\t\treturn Outgoing{}, &Refusal{Reason: RefusedBodySize}\n"
        "\t}\n",
        new="\tif len(subject) > MaxSubjectBytes {\n"
        "\t\treturn Outgoing{}, &Refusal{Reason: RefusedSubjectSize}\n"
        "\t}\n"
        "\tif len(body) > MaxBodyBytes {\n"
        "\t\treturn Outgoing{}, &Refusal{Reason: RefusedBodySize}\n"
        "\t}\n"
        "\tfor _, text := range []string{subject, body} {\n"
        "\t\t_, hits := scan.String(text)\n"
        "\t\tfor _, h := range hits {\n"
        "\t\t\tif confidentSecretRules[h.Rule] {\n"
        "\t\t\t\treturn Outgoing{}, &Refusal{Reason: RefusedSecret, Rule: h.Rule}\n"
        "\t\t\t}\n"
        "\t\t}\n"
        "\t}\n",
        killer="TestTheSecretRefusalWinsOverSizeAndTTL",
        extra_killers=("TestASecretOverALimitIsNeverEchoed",),
        why="checking the cheap limits first and the expensive scan last is the obvious order — and "
        "the browser form echoes the text for a size refusal, so a credential over a limit is served "
        "back in the page.",
    ),
    Mutant(
        name="memo-scannable-off-by-one",
        path="internal/memo/policy.go",
        old="\treturn len(subject)+len(body) <= MaxScannedBytes\n",
        new="\treturn len(subject)+len(body) < MaxScannedBytes\n",
        killer="TestTheScanIsBoundedAndScannableSaysWhere",
        why="a bound is a bound either way — and text exactly at it is refused as a size unscanned.",
    ),
    Mutant(
        name="memo-scan-unbounded",
        path="internal/memo/policy.go",
        old="\tMaxScannedBytes = 4 * MaxBodyBytes\n",
        new="\tMaxScannedBytes = 1 << 30\n",
        killer="TestTheScanIsBoundedAndScannableSaysWhere",
        why="'scan everything' reads as the safe choice — and the browser form reaches the scan through "
        "a 10 MiB form cap, which is seconds of CPU per POST.",
    ),
    Mutant(
        name="ui-memo-draft-kept-unscanned",
        path="internal/ui/memos.go",
        old="memoDraftKept[ref.Reason] && memo.Scannable(draft.Subject, draft.Body) {",
        new="memoDraftKept[ref.Reason] && true {",
        killer="TestASecretOverALimitIsNeverEchoed",
        why="a size refusal is in the kept set, so the extra clause reads as redundant — and past the "
        "scan's bound the size refusal comes UNSCANNED, so the draft echoed can carry a credential.",
    ),
    Mutant(
        name="ui-memo-scannable-asks-about-the-body-only",
        path="internal/ui/memos.go",
        old="memoDraftKept[ref.Reason] && memo.Scannable(draft.Subject, draft.Body) {",
        new="memoDraftKept[ref.Reason] && memo.Scannable(\"\", draft.Body) {",
        killer="TestASecretOverALimitIsNeverEchoed",
        why="the bound is over subject AND body together; with the excess in the subject a short body "
        "alone looks scannable, so an UNSCANNED subject-size refusal echoes a body carrying a credential.",
    ),
    Mutant(
        name="memo-ttlof-multiplies-unguarded",
        path="internal/memo/policy.go",
        old="\tcase n > limit:\n\t\treturn MaxTTL + unit\n",
        new="\tcase false:\n\t\treturn MaxTTL + unit\n",
        killer="TestTTLOfNeverOverflowsAndLeavesTheCeilingToCheckSend",
        extra_killers=("TestTheSendTTLIsClampedWithoutOverflow",),
        pkgs=PKGS + ("./internal/clientapi/",),
        why="`CheckSend` refuses a TTL past the ceiling anyway, so the clamp reads as a second copy of "
        "its rule — and a count whose product wraps comes back small, positive and ACCEPTED.",
    ),
    Mutant(
        name="memo-required-keys-skip-a-rendered-field",
        path="internal/client/memo.go",
        old="\tfor i := 0; i < t.NumField(); i++ {\n\t\tkeys = append(keys, t.Field(i).Name)\n",
        new="\tfor i := 0; i < t.NumField()-1; i++ {\n\t\tkeys = append(keys, t.Field(i).Name)\n",
        killer="TestTheMemoKeysAClientRequiresAreExactlyRenderedsFields",
        pkgs=PKGS + ("./internal/client/",),
        why="a field the client's own output does not print looks safely optional — and "
        "`internal/memowire`'s deploy order is stated over the WHOLE type, so the rule and the code part.",
    ),
    Mutant(
        name="ui-memo-write-skips-the-scope-lookup",
        path="internal/ui/memos.go",
        old="\tif !found {\n\t\twritePlain(w, http.StatusNotFound, browseRefusal)\n\t\treturn nil, Scope{}, false\n",
        new="\tif !found && false {\n\t\twritePlain(w, http.StatusNotFound, browseRefusal)\n\t\treturn nil, Scope{}, false\n",
        killer="TestAWriteNamingAScopeTheViewerCannotReadIsTheBrowseRefusal",
        why="the store refuses an unwritable scope anyway, so the lookup reads as redundant — and the "
        "redirect it falls through to is not the browse surface's one miss.",
    ),
    Mutant(
        name="memo-mayretractmemo-asks-of-the-retractor",
        path="internal/memo/policy.go",
        old="return MayRetract(p, a, t.scope, t.senderKind, t.senderID)",
        new="return MayRetract(p, a, t.scope, t.retractorKind, t.retractorID)",
        killer="TestMayRetractMemoAsksMayRetractOfTheMemosOwnScopeAndSender",
        why="two (kind, id) pairs sit side by side in the stored memo, and the wrong one compiles.",
    ),
    Mutant(
        name="memo-sendermaysend-ignores-the-scope",
        path="internal/memo/policy.go",
        old="return MaySend(control.Resolve(m, p), t.scope)",
        new="return len(control.Resolve(m, p).NamedScopes(control.VerbWrite)) > 0",
        killer="TestSenderMaySendIsTheSendersOwnAuthorityNow",
        why="'can the sender still write' without saying WHERE reads as the question; a sender who "
        "writes elsewhere then hides a lapse here.",
    ),
    # S5 of the scope-refs plan: `internal/refaudit`, the auditor, and the `audit-refs` verb. Each row
    # ADDS `./internal/refaudit/` (outside the seam). The git-backed killers follow the two-tier rule:
    # the `go` job sets CAIRN_GIT_TESTS_REQUIRED, where a skip is a failure, never a kill.
    Mutant(
        name="refaudit-tags-count-as-reachable",
        path="internal/refaudit/gitcli.go",
        old='"--contains", sha,\n\t\t"refs/heads/", "refs/pull/")',
        new='"--contains", sha)',
        killer="TestATagInAnOlderMirrorNeverCountsAsReachable",
        why="'any ref that contains it' reads as reachability — and a tag an older mirror kept makes a "
        "SHA whose branch is gone look reachable: a false clean exit (round 3 of the plan).",
        pkgs=PKGS + ("./internal/refaudit/",),
    ),
    Mutant(
        name="refaudit-off-branch-counted-stale",
        path="internal/refaudit/refaudit.go",
        old="ExistsOffBranch: ClassInformational",
        new="ExistsOffBranch: ClassStale",
        killer="TestTheExitIsTheMixedCase",
        why="'not on the branch' reads as wrong — and every PR-merged-by-squash SHA would fail the audit.",
        pkgs=PKGS + ("./internal/refaudit/",),
    ),
    Mutant(
        name="refaudit-not-reachable-counted-stale",
        path="internal/refaudit/refaudit.go",
        old="NotReachable: ClassCouldNotLook",
        new="NotReachable: ClassStale",
        killer="TestTheExitIsTheMixedCase",
        why="absence from the fetched refs proves nothing about the remote (squash-and-delete, "
        "force-push); calling it stale is a verdict the auditor cannot support.",
        pkgs=PKGS + ("./internal/refaudit/",),
    ),
    Mutant(
        name="refaudit-presence-counted-as-reachability",
        path="internal/refaudit/audit.go",
        old="\t\tif len(refs) > 0 {",
        new="\t\tif true {",
        killer="TestReachabilityIsRecomputedFromTheRemotesCurrentRefs",
        why="'the commit object exists' is what the out-of-tree audit checked — and a SHA whose only ref "
        "was deleted stays in the mirror forever.",
        pkgs=PKGS + ("./internal/refaudit/",),
    ),
    Mutant(
        name="refaudit-ls-tree-exit-code-trusted",
        path="internal/refaudit/gitcli.go",
        old='"ls-tree", "-z", commit, "--", path)\n\tif err != nil {\n\t\treturn false, err\n\t}',
        new='"ls-tree", "-z", commit, "--", path)\n\tif err != nil {\n\t\treturn false, err\n\t}\n\treturn true, nil',
        killer="TestEachPlantedClaimMapsToOneLiteralState",
        why="`ls-tree` exiting 0 reads as 'found' — it exits 0 on a missing path (round 1 F8).",
        pkgs=PKGS + ("./internal/refaudit/",),
    ),
    Mutant(
        name="refaudit-directory-counted-missing",
        path="internal/refaudit/gitcli.go",
        old='ok && name == path {',
        new='ok && name == path && strings.Contains(rec, " blob ") {',
        killer="TestEachPlantedClaimMapsToOneLiteralState",
        why="'a path is a file' — and every `internal/` pointer row becomes a stale finding.",
        pkgs=PKGS + ("./internal/refaudit/",),
    ),
    Mutant(
        name="refaudit-literal-pathspecs-dropped",
        path="internal/refaudit/gitcli.go",
        old='"GIT_TERMINAL_PROMPT=0", "GIT_CONFIG_NOSYSTEM=1", "GIT_LITERAL_PATHSPECS=1",',
        new='"GIT_TERMINAL_PROMPT=0", "GIT_CONFIG_NOSYSTEM=1",',
        killer="TestEachPlantedClaimMapsToOneLiteralState",
        why="the variable looks like noise beside two others — and a cited `a*.go` greps every file the "
        "glob matches (T3).",
        pkgs=PKGS + ("./internal/refaudit/",),
    ),
    Mutant(
        name="refaudit-undeclared-scope-exits-0",
        path="internal/refaudit/run.go",
        old='res, detail = Result{ScopeReason: ReasonUndeclared}, "status="+r.SourcesStatus',
        new='return ReasonUndeclared + "\\n", "", ExitClean',
        killer="TestAuditRefsCouldNotLookIsTenForEveryScopeLevelReason",
        why="'nothing declared, nothing to audit' reads as a clean answer — and an undeclared scope is "
        "reported clean forever (decision 8).",
        pkgs=PKGS + ("./internal/refaudit/",),
    ),
    Mutant(
        name="refaudit-checked-zero-exits-0",
        path="internal/refaudit/refaudit.go",
        old="\tif checked == 0 {\n\t\treturn ExitCouldNotLook",
        new="\tif checked < 0 {\n\t\treturn ExitCouldNotLook",
        killer="TestTheExitIsTheMixedCase",
        why="'no findings' reads as clean — and a scope whose entries cite nothing checkable passes.",
        pkgs=PKGS + ("./internal/refaudit/",),
    ),
    Mutant(
        name="refaudit-unmapped-host-counted-clean",
        path="internal/refaudit/refaudit.go",
        old="\tif r.unlookedSource() {",
        new="\tif false {",
        killer="TestTheExitIsTheMixedCase",
        why="'the claims all held' reads as clean — while half the declaration was never fetched.",
        pkgs=PKGS + ("./internal/refaudit/",),
    ),
    # The operator's decision on #259 r1 🟡2, and #259 r2's interrupt: two rows each, one per direction.
    Mutant(
        name="refaudit-stale-beside-an-unlooked-source",
        path="internal/refaudit/audit.go",
        old="if unread && Severity[best.State] == ClassStale {",
        new="if unread && false && Severity[best.State] == ClassStale {",
        killer="TestAStaleVerdictMeansEverySourceWasRead",
        why="'missing from every source we read' reads as missing — while the unread source may hold "
        "it: a stale verdict nobody measured (the out-of-tree default the plan first pinned).",
        pkgs=PKGS + ("./internal/refaudit/",),
    ),
    Mutant(
        name="refaudit-stale-demoted-with-every-source-read",
        path="internal/refaudit/audit.go",
        old="unread := len(looked) < len(sources)",
        new="unread := len(looked) <= len(sources)",
        killer="TestAStaleVerdictMeansEverySourceWasRead",
        why="an off-by-one on the comparison demotes EVERY stale claim and the audit never exits 9.",
        pkgs=PKGS + ("./internal/refaudit/",),
    ),
    Mutant(
        name="refaudit-stale-beside-a-failed-claim-read",
        path="internal/refaudit/audit.go",
        old="\t\t\t\t\tunread = true // fetched",
        new="\t\t\t\t\t_ = true // fetched",
        killer="TestAStaleVerdictMeansEverySourceWasRead",
        why="'every source was FETCHED' reads as 'every source was read' — while one source's `git` "
        "failed on this very claim and the other's `missing` became the verdict (#267 r1 🟡1).",
        pkgs=PKGS + ("./internal/refaudit/",),
    ),
    Mutant(
        name="refaudit-interrupt-ignored-signal-unignored",
        path="internal/refaudit/gitcli.go",
        old="\t\tif !signal.Ignored(s) {",
        new="\t\tif true {",
        killer="TestAnIgnoredHangupStaysIgnored",
        why="handling every signal reads as thorough — and `nohup cairn audit-refs` then dies on the "
        "hangup it was started to survive (#267 r1 🟡2).",
        pkgs=PKGS + ("./internal/refaudit/",),
    ),
    Mutant(
        name="refaudit-interrupt-never-reaches-the-group",
        path="internal/refaudit/gitcli.go",
        old="\t\t\tw.caught = s\n\t\t\tcancel()",
        new="\t\t\tw.caught = s",
        killer="TestAnInterruptKillsTheStalledFetch",
        why="re-raising the signal reads as handling it — and the re-raise waits on a `git` that "
        "nothing killed, or `cairn` dies and leaves `git-remote-https` holding the remote (#259 r2).",
        pkgs=PKGS + ("./internal/refaudit/",),
    ),
    Mutant(
        name="refaudit-interrupt-swallowed-not-reraised",
        path="internal/refaudit/gitcli.go",
        old="\t_ = syscall.Kill(os.Getpid(), sig)",
        new="\t_ = sig",
        killer="TestAnInterruptKillsTheStalledFetch",
        why="exiting 130 by hand looks the same in a shell — but the parent sees an exit, not a death "
        "by SIGINT, and a caller that distinguishes the two (a shell loop's own Ctrl-C) carries on.",
        pkgs=PKGS + ("./internal/refaudit/",),
    ),
    Mutant(
        name="refaudit-mirror-dir-from-path-components",
        path="internal/refaudit/gitcli.go",
        old='return filepath.Join(g.MirrorRoot, hex.EncodeToString(sum[:])+".git")',
        new='_ = hex.EncodeToString(sum[:])\n\treturn filepath.Join(g.MirrorRoot, strings.ReplaceAll(canonical, ":", "/")+".git")',
        killer="TestTheMirrorDirectoryIsADigest",
        why="a readable mirror path is friendlier — and a declaration then chooses where on disk the "
        "auditor writes (T4).",
        pkgs=PKGS + ("./internal/refaudit/",),
    ),
    # ---- S11: the client read ledger, both ends (decision 3a) --------------------------------------
    Mutant(
        name="transcript-pod-ignores-ledger",
        path="internal/transcript/archive/archive.go",
        old="\tif stream == LedgerStream {\n\t\tfoldLedger(m, lines)\n\t} else {",
        new="\tif false && stream == LedgerStream {\n\t\tfoldLedger(m, lines)\n\t} else {",
        killer="TestAnUploadedLedgerPutsItsScopesInMeta",
        extra_killers=("TestAnUploadedLedgerIsInV", "TestALedgerArrivingLaterLiftsF1"),
        why="storing the ledger like any other stream looks complete — and then the only thing carrying "
        "a read the transcript never shows is the agent's DECLARATION, which an old or lying agent "
        "omits (clause n). Moved here from S3 in revision 14: the battery runs `go test`, and S3 had "
        "no fold to mutate.",
        pkgs=PKGS + ("./internal/transcript/archive/",),
    ),
    Mutant(
        name="client-ledger-write-skipped",
        path="internal/client/readledger.go",
        old="\tif e.ledger != nil {\n\t\t_ = e.ledger.record(instance, scope)",
        new="\tif false && e.ledger != nil {\n\t\t_ = e.ledger.record(instance, scope)",
        killer="TestNoSessionIdWritesNothing",
        extra_killers=("TestTheLedgerRecordsTheScopeEachServedCallResolved",),
        why="a ledger nobody reads on the client side looks optional — and every session that runs "
        "`cairn` is then owner-only by F1, or, beside a pre-ledger record, shows its transcript to a "
        "viewer who cannot read what it read.",
    ),
    Mutant(
        name="client-ledger-records-requested-not-resolved-scope",
        path="internal/client/verbs.go",
        old="\t\t\treturn 0, recallErr\n\t\t}\n\t\tenv.recordRead(alias, scope)",
        new="\t\t\treturn 0, recallErr\n\t\t}\n\t\tenv.recordRead(alias, opts.Scope)",
        killer="TestTheLedgerRecordsTheScopeEachServedCallResolved",
        why="the flag is what the caller asked for, so it reads as the honest record — but a bare "
        "`recall` asks for NOTHING and reads the scope `--repo` derived, and the empty flag then names "
        "no scope at all.",
    ),
    Mutant(
        name="client-ledger-write-visible-in-output",
        path="internal/client/readledger.go",
        old="\t\t_ = e.ledger.record(instance, scope)",
        new="\t\tif err := e.ledger.record(instance, scope); err != nil {\n"
        "\t\t\te.Stderr.WriteString(\"cairn: the read ledger was not written: \" + err.Error() + \"\\n\")\n\t\t}",
        killer="TestAFailedLedgerWriteIsInvisible",
        why="a failure nobody hears about looks like a hidden bug, so reporting it reads as diligence — "
        "and every call on a host with a read-only state directory then changes its stderr, which the "
        "parity gate diffs byte for byte and an agent's tool output carries.",
    ),
    # `cairn-server -create-scope` / `control.CreateScope`: the second writer of
    # `scope-created`. One row per refusal, each on the narrowest expression that decides it.
    Mutant(
        name="create-scope-accepts-a-project-the-journal-does-not-hold",
        path="internal/control/scope_create.go",
        old="\tif _, known := current.Projects[req.ProjectID]; !known {",
        new="\tif _, known := current.Projects[req.ProjectID]; !known && false {",
        killer="TestCreateScopeRefusesAProjectTheJournalDoesNotHold",
        extra_killers=("TestEveryCreateScopeRefusalWritesNOTHING",),
        why="`apply` refuses the same event under the lock, so the early check reads as "
        "redundant. It is what carries `ErrNoSuchProject`: without it the command cannot tell "
        "a missing project from any other refused batch, and the operator who typed a project "
        "NAME — what `-create-user`'s `-project` takes — is not shown the ids.",
    ),
    Mutant(
        name="create-scope-accepts-a-name-outside-the-path-component-class",
        path="internal/control/scope_create.go",
        old="\tif !scopeDirectoryName.MatchString(name) {",
        new="\tif false {",
        killer="TestCreateScopeRefusesANameOutsideThePathComponentClass",
        extra_killers=("TestEveryCreateScopeRefusalWritesNOTHING",),
        why="`-create-user -scopes` applies no class at all, so matching it reads as "
        "consistency. `quarry.inbox` and `..` then land in an append-only journal as scopes the "
        "pod's URL router refuses by name, with nothing that renames one.",
    ),
    Mutant(
        name="create-scope-class-widened-to-admit-a-dot",
        path="internal/control/scope_create.go",
        old="var scopeDirectoryName = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)",
        new="var scopeDirectoryName = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)",
        killer="TestTheScopeNameClassIsThePathComponentClass",
        extra_killers=(
            "TestCreateScopeRefusesANameOutsideThePathComponentClass",
            "TestEveryCreateScopeRefusalWritesNOTHING",
        ),
        why="the class is a RESTATEMENT of `authz.SafePathComponent` — this package does not "
        "import `internal/authz` — and a dot is what the fold keeps, so `quarry.notes` looks "
        "harmless. The no-dot rule is what makes `..` impossible by construction.",
    ),
    Mutant(
        name="create-scope-accepts-a-name-that-folds-to-nothing",
        path="internal/control/scope_create.go",
        old="\tif store.NormalizeRef(name) == \"\" {",
        new="\tif store.NormalizeRef(name) == \"\" && false {",
        killer="TestCreateScopeRefusesANameThatFoldsToNothing",
        extra_killers=("TestEveryCreateScopeRefusalWritesNOTHING",),
        why="the class clause above looks like it covers this. It does not: `-` and `___` are "
        "inside the class and fold to the empty string, a scope every reader narrows to no "
        "directory — and `validate` accepts them, because the name is not empty.",
    ),
    Mutant(
        name="create-scope-checks-no-name-against-the-journal",
        path="internal/control/scope_create.go",
        # The wrong OPERAND rather than a deleted call, as on `ProvisionUser`'s row: the
        # ledger row below is the deleted call.
        old="\tif err := checkScopeNamesAreFree(current, []string{req.Name}); err != nil {",
        new="\tif err := checkScopeNamesAreFree(current, nil); err != nil {",
        killer="TestCreateScopeRefusesANameTheJournalAlreadyFoldsTo",
        extra_killers=("TestEveryCreateScopeRefusalWritesNOTHING",),
        why="the tenancy boundary `checkScopeNamesAreFree` exists for, at the SECOND writer. "
        "`apply` accepts a name that folds onto a scope in another project, so a second "
        "project's members would read and write the first's directory.",
    ),
    Mutant(
        name="create-scope-never-calls-the-name-check",
        path="internal/control/scope_create.go",
        old="\tif err := checkScopeNamesAreFree(current, []string{req.Name}); err != nil {\n"
        "\t\treturn ScopeCreated{}, err\n\t}\n",
        new="",
        killer="TestEveryWriterOfScopeCreatedChecksTheNameIsFree",
        extra_killers=("TestCreateScopeRefusesANameTheJournalAlreadyFoldsTo",),
        why="the ledger's SHRINK-of-coverage direction: a writer of `scope-created` that does "
        "not call the one function holding the fold. `apply`'s own name rule is raw and per "
        "project, so nothing below this line notices.",
    ),
    Mutant(
        name="a-third-writer-of-scope-created-appears-unledgered",
        path="cmd/cairn-server/createscope.go",
        old="func renderProjects(m control.Model) string {",
        new="func aThirdWriter(project control.ID) control.Event {\n"
        "\treturn control.Event{Kind: control.EventScopeCreated, ProjectID: project}\n}\n\n"
        "func renderProjects(m control.Model) string {",
        killer="TestEveryWriterOfScopeCreatedChecksTheNameIsFree",
        why="the ledger's GROW direction, from a package OUTSIDE `internal/control`: the next "
        "surface that creates a scope builds the event where it stands and appends it, and "
        "the fold — which is not in `apply` — is skipped with every test green.",
    ),
    Mutant(
        name="create-scope-with-no-journal-falls-through",
        path="cmd/cairn-server/createscope.go",
        old="\tif journal == \"\" {\n\t\t// REFUSED rather than defaulted",
        new="\tif false {\n\t\t// REFUSED rather than defaulted",
        killer="TestCreateScopeWithNoJournalConfiguredRefusesRatherThanDefaulting",
        why="the refusal that names the variable to set. Without it the operator is handed "
        "whatever opening an empty path says, which names no remedy.",
    ),
    Mutant(
        name="create-scope-does-not-require-a-name",
        path="cmd/cairn-server/createscope.go",
        old="\tif project == \"\" || name == \"\" {",
        new="\tif project == \"\" {",
        killer="TestEveryCreateScopeRefusalWritesNOTHING",
        why="`control.CreateScope` refuses an empty name anyway, so the flag check reads as "
        "duplication — and the operator who forgot a flag is then told about a character class.",
    ),
    Mutant(
        name="create-scope-does-not-require-a-project",
        path="cmd/cairn-server/createscope.go",
        old="\tif project == \"\" || name == \"\" {",
        new="\tif name == \"\" {",
        killer="TestEveryCreateScopeRefusalWritesNOTHING",
        why="the other half: an absent `-scope-project` reported as a project with the id "
        "`\"\"` that the journal does not hold.",
    ),
    Mutant(
        name="create-scope-relays-a-missing-project-without-the-ids",
        path="cmd/cairn-server/createscope.go",
        old="\t\tcase errors.Is(err, control.ErrNoSuchProject):",
        # `&& false` rather than a bare `false`: this is the file's only use of `errors`, so
        # dropping the call dies at the build instead of at the guard.
        new="\t\tcase errors.Is(err, control.ErrNoSuchProject) && false:",
        killer="TestEveryCreateScopeRefusalWritesNOTHING",
        why="a sentinel is a guard only where something branches on it. The branch is what "
        "prints the journal's project ids beside their names — the remedy for the mistake the "
        "flag invites.",
    ),
    Mutant(
        name="create-scope-skips-the-store-root-warning",
        path="cmd/cairn-server/createscope.go",
        old="\twarnScopesThatAlreadyExistOnDisk(storeRoot, []string{made.Scope.Name}, errOut)",
        new="\twarnScopesThatAlreadyExistOnDisk(storeRoot, nil, errOut)",
        killer="TestCreateScopeCallsOutADirectoryThatAlreadyAnswersToTheName",
        why="the half of the name check only this program can make: a directory already under "
        "the store root that the new record hands to every member and every project grantee.",
    ),
    Mutant(
        name="create-scope-warns-about-the-directory-before-the-refusals",
        path="cmd/cairn-server/createscope.go",
        # The shape this mode SHIPPED with: the warning ahead of `CreateScope`, as in
        # `runCreateUser`. Added, not moved, so the edit is one insertion.
        old="\tmade, err := control.CreateScope(",
        new="\twarnScopesThatAlreadyExistOnDisk(storeRoot, []string{name}, errOut)\n"
        "\tmade, err := control.CreateScope(",
        killer="TestCreateScopeCallsOutADirectoryThatAlreadyAnswersToTheName",
        why="`-create-user` warns before its write, so matching it reads as consistency — and "
        "every refused re-run (a taken name, an unknown project) then tells the operator a "
        "directory is being handed over by a command that wrote nothing.",
    ),
    Mutant(
        name="create-scope-help-text-gets-a-backquoted-placeholder",
        path="cmd/cairn-server/createscope.go",
        old="append ONE scope-created record",
        new="append ONE `scope-created` record",
        killer="TestTheBinaryActuallyDispatchesCreateScope",
        why="backquotes read as code formatting, and `flag.PrintDefaults` takes the first "
        "backquoted word as the flag's placeholder: `-h` then reads `-create-scope scope-created`.",
    ),
    Mutant(
        name="a-new-scope-does-not-reach-a-PROJECT-grantee",
        path="internal/control/resolve.go",
        # Re-derived when the expansion moved into `addObject`: the grant's time is known only at
        # the GRANT call site, so the freeze is spelled there — a project grant expanded by hand
        # over the scopes that existed when it was granted, every other object as before.
        old="\t\ta.addObject(m, g.ObjectKind, g.ObjectID, g.Verbs)\n",
        new="\t\tif g.ObjectKind == ObjectProject {\n"
        "\t\t\tfor _, scopeID := range m.ScopesIn(g.ObjectID) {\n"
        "\t\t\t\tif m.Scopes[scopeID].CreatedAt.After(g.GrantedAt) {\n"
        "\t\t\t\t\tcontinue\n\t\t\t\t}\n"
        "\t\t\t\ta.add(scopeID, g.Verbs)\n\t\t\t}\n\t\t\tcontinue\n\t\t}\n"
        "\t\ta.addObject(m, g.ObjectKind, g.ObjectID, g.Verbs)\n",
        killer="TestANewScopeReachesItsProjectsMembersAndNoNarrowedCredential",
        why="the audience `-create-scope` names. Freezing a project grant to the scopes that "
        "existed when it was granted is a plausible policy, and it would make this command's "
        "stderr line — every project grantee reaches the new scope — false. The fixture creates "
        "the scope an hour AFTER the grant so this clause is reached.",
    ),
    Mutant(
        name="a-project-grantee-reaches-a-new-scope-with-more-than-its-verbs",
        path="internal/control/resolve.go",
        old="\t\tfor _, scopeID := range m.ScopesIn(id) {\n\t\t\ta.add(scopeID, verbs)",
        new="\t\tfor _, scopeID := range m.ScopesIn(id) {\n\t\t\ta.add(scopeID, verbs.Union(NewVerbSet(VerbWrite)))",
        killer="TestANewScopeReachesItsProjectsMembersAndNoNarrowedCredential",
        why="the grantee arm's SECOND half: a read grant must stay read on the new scope. An "
        "arm asserting only `read=true` passes a resolver that hands every project grantee write.",
    ),
    Mutant(
        name="an-ASSIGNMENT-shaped-writer-of-scope-created-appears-unledgered",
        path="cmd/cairn-server/createscope.go",
        old="func renderProjects(m control.Model) string {",
        new="func anAssignedWriter(project control.ID) control.Event {\n"
        "\tvar e control.Event\n\te.Kind = control.EventScopeCreated\n\te.ProjectID = project\n"
        "\treturn e\n}\n\nfunc renderProjects(m control.Model) string {",
        killer="TestEveryWriterOfScopeCreatedChecksTheNameIsFree",
        why="the shape the ledger's first draft could not see: it matched the composite literal "
        "`Kind: EventScopeCreated` only, so a writer that assigns the field walked past it.",
    ),
    Mutant(
        name="create-scope-missing-from-the-mode-ledger",
        path="cmd/cairn-server/main.go",
        old="\t\t{\"-create-scope\", *createScope.enabled},\n",
        new="",
        killer="TestTheBinaryActuallyDispatchesCreateScope",
        why="the copy that gets forgotten is the new one: `-routes -create-scope` then prints "
        "the route ledger, exits 0 and creates nothing.",
    ),
    Mutant(
        name="create-scope-never-dispatched",
        path="cmd/cairn-server/main.go",
        old="\tif *createScope.enabled {",
        new="\tif false {",
        killer="TestTheBinaryActuallyDispatchesCreateScope",
        why="flags registered, ledger entry present, and the mode falls through to SERVING — "
        "which every in-process test of `runCreateScope` is blind to.",
    ),
    Mutant(
        name="create-scope-flag-count-in-prose-goes-stale",
        path="cmd/cairn-server/main.go",
        # Re-anchored by `-remove-member`, which appended its own count after this one; the
        # old anchor (`three (counts`) matched nothing and scored a HARNESS ERROR.
        old="`-create-scope`'s three and",
        new="`-create-scope`'s two and",
        killer="TestTheRegisteredFlagCountInProseMatchesTheCode",
        why="proves the new mode is IN the flag-count gate: a mode absent from that test's "
        "table is one whose prose count nothing checks.",
    ),
    # `cairn-server -remove-member` / `control.RemoveMember`: the first writer of
    # `member-removed`. One row per refusal, per reported set, and per wiring claim, each on
    # the narrowest expression that decides it.
    Mutant(
        name="remove-member-accepts-a-project-the-journal-does-not-hold",
        path="internal/control/member_remove.go",
        old="\tif _, known := current.Projects[req.ProjectID]; !known {",
        new="\tif _, known := current.Projects[req.ProjectID]; !known && false {",
        killer="TestRemoveMemberRefusesAProjectTheJournalDoesNotHold",
        extra_killers=("TestEveryRemoveMemberRefusalWritesNOTHING",),
        why="`SetMember`'s twin check is an EARLY refusal that `apply` repeats, so this one reads "
        "as redundant too. It is not: `apply`'s `member-removed` arm checks nothing, so without "
        "it a mistyped project id is refused as 'not a member' and the operator is never told "
        "the project does not exist.",
    ),
    Mutant(
        name="remove-member-accepts-a-user-the-journal-does-not-hold",
        path="internal/control/member_remove.go",
        old="\tif _, known := current.Users[req.UserID]; !known {",
        new="\tif _, known := current.Users[req.UserID]; !known && false {",
        killer="TestRemoveMemberRefusesAUserTheJournalDoesNotHold",
        extra_killers=("TestEveryRemoveMemberRefusalWritesNOTHING",),
        why="the not-a-member refusal below catches the same input, so this reads as "
        "duplication — and the operator who typed an id that names nobody is told the person is "
        "merely not in the project.",
    ),
    Mutant(
        name="remove-member-removes-a-non-member",
        path="internal/control/member_remove.go",
        old="\tif !isMember {",
        new="\tif !isMember && false {",
        killer="TestRemoveMemberRefusesSomebodyWhoIsNotAMember",
        extra_killers=("TestEveryRemoveMemberRefusalWritesNOTHING",),
        why="`SetMember` accepts a restated role as idempotent, so accepting a removal of "
        "nobody looks consistent. `apply` folds it to a no-op and counts it as an event: a "
        "departure that never happened, in the log whose purpose is who left when.",
    ),
    Mutant(
        name="remove-member-asks-the-last-owner-rule-the-wrong-question",
        path="internal/control/member_remove.go",
        # The wrong OPERAND, not a deleted call: the ledger row below is the deleted call.
        old="refuseOrphaning(current, req.ProjectID, req.UserID, prior.Role, \"\")",
        new="refuseOrphaning(current, req.ProjectID, req.UserID, prior.Role, RoleOwner)",
        killer="TestTheLastOwnerCannotBeRemoved",
        extra_killers=("TestEveryRemoveMemberRefusalWritesNOTHING",),
        why="a removal has no next role, and `RoleOwner` is a plausible 'no change' placeholder "
        "— it is exactly the value `refuseOrphaning` waves through, so the sole owner is "
        "removed and the project is left with nobody who may administer or delete it.",
    ),
    Mutant(
        name="remove-member-never-calls-the-last-owner-rule",
        path="internal/control/member_remove.go",
        old="\tif err := refuseOrphaning(current, req.ProjectID, req.UserID, prior.Role, \"\"); err != nil {\n"
        "\t\treturn MemberRemoved{}, err\n\t}\n",
        new="",
        killer="TestEveryWriterOfMemberRemovedChecksTheLastOwner",
        extra_killers=("TestTheLastOwnerCannotBeRemoved",),
        why="the ledger's SHRINK-of-coverage direction: `apply` carries no last-owner rule, so a "
        "writer that skips the call is refused by nothing below it.",
    ),
    Mutant(
        name="a-second-writer-of-member-removed-appears-unledgered",
        path="cmd/cairn-server/removemember.go",
        old="func runRemoveMember(",
        new="func aSecondWriter(project, user control.ID) control.Event {\n"
        "\treturn control.Event{Kind: control.EventMemberRemoved, ProjectID: project, UserID: user}\n}\n\n"
        "func runRemoveMember(",
        killer="TestEveryWriterOfMemberRemovedChecksTheLastOwner",
        why="the ledger's GROW direction, from outside `internal/control`: the next surface that "
        "removes somebody (a browser 'leave project' button) builds the event where it stands, "
        "and the last-owner rule — not in `apply` — is skipped with every test green.",
    ),
    Mutant(
        name="orphaning-refusal-for-a-removal-says-making-them-empty",
        path="internal/control/member_set.go",
        old="\tif next == \"\" {\n\t\tact = \"removing them from it\"",
        new="\tif false {\n\t\tact = \"removing them from it\"",
        killer="TestTheLastOwnerCannotBeRemoved",
        extra_killers=("TestEveryRemoveMemberRefusalWritesNOTHING",),
        why="one message for both callers is the shape `refuseOrphaning` already had to correct "
        "once; for a removal it reads `making them \"\"`, which names no action the operator took.",
    ),
    Mutant(
        name="a-second-owner-does-not-license-removing-the-first",
        path="internal/control/member_set.go",
        old="\t\tif userID != user && ms.Role == RoleOwner {",
        new="\t\tif userID != user && ms.Role == RoleOwner && false {",
        killer="TestRemovingOneOfTwoOwnersIsAccepted",
        extra_killers=("TestASECONDOwnerMakesTheDemotionLEGITIMATE",),
        why="'the owner may never be removed' is the blunt version of the rule, and it makes a "
        "handover's last step impossible — the other half of the refusal above, without which a "
        "guard refusing every owner removal passes it.",
    ),
    Mutant(
        name="residue-read-from-the-model-before-the-removal",
        path="internal/control/member_remove.go",
        old="\t\tStillReaches: stillReaches(after, req.ProjectID, req.UserID),",
        new="\t\tStillReaches: stillReaches(current, req.ProjectID, req.UserID),",
        killer="TestRemovingAMemberTakesAwayTheirMembershipAuthority",
        extra_killers=("TestTheReportedResidueIsExactlyWhatResolveStillGrants",),
        why="`current` is already in hand and reads the same for grants, so it looks "
        "interchangeable. It still holds the membership being removed, so every removal reports "
        "the member as still reaching every scope of the project they just left.",
    ),
    Mutant(
        name="still-reaches-counts-scopes-of-other-projects",
        path="internal/control/member_remove.go",
        old="\tvar out []ScopeReach\n\tfor _, scopeID := range m.ScopesIn(project) {",
        new="\tvar out []ScopeReach\n\tfor _, scopeID := range auth.ScopeIDs(VerbRead) {",
        killer="TestRemovingAMemberTakesAwayTheirMembershipAuthority",
        why="`Resolve`'s whole answer is the obvious thing to report; it is the user's authority "
        "EVERYWHERE, so their own project's scopes read as residue of a removal from another.",
    ),
    Mutant(
        name="residual-grants-miss-the-route-through-another-project",
        path="internal/control/member_remove.go",
        old="\tfor other := range m.userProject[user] {",
        new="\tfor other := range m.userProject[\"\"] {",
        killer="TestASeparateGrantSurvivesTheRemovalAndIsReported",
        extra_killers=("TestTheReportedResidueIsExactlyWhatResolveStillGrants",),
        why="'grants whose subject is this user' is the natural reading of the question, and it "
        "misses a grant to a project they are still in — authority `Resolve` still honours and "
        "the report would leave unmentioned.",
    ),
    Mutant(
        name="residual-grants-report-a-revoked-grant",
        path="internal/control/member_remove.go",
        old="\t\tif !g.Live() {",
        new="\t\tif !g.Live() && false {",
        killer="TestASeparateGrantSurvivesTheRemovalAndIsReported",
        why="a grant row is still in the model after its tombstone, so a listing over "
        "`m.Grants` names it; the operator is then sent to revoke a grant that is already dead.",
    ),
    Mutant(
        name="residual-grants-ignore-the-object",
        path="internal/control/member_remove.go",
        old="\t\tif onThisProject {",
        new="\t\tif onThisProject || true {",
        killer="TestASeparateGrantSurvivesTheRemovalAndIsReported",
        why="every live grant of the user is 'authority left behind' in some sense; reporting "
        "grants on OTHER projects' objects buries the ones this removal is about.",
    ),
    Mutant(
        name="removal-reports-a-revoked-credential",
        path="internal/control/member_remove.go",
        old="\t\tif c.Live() && c.PrincipalKind == KindUser && c.PrincipalID == user {",
        new="\t\tif c.PrincipalKind == KindUser && c.PrincipalID == user {",
        killer="TestRemovalReportsEveryLiveCredentialOfTheUser",
        why="a revoked credential still has a row; reporting it tells the operator to revoke "
        "something `-revoke-credential` then refuses as already revoked.",
    ),
    Mutant(
        name="removal-reports-everybodys-credentials",
        path="internal/control/member_remove.go",
        old="\t\tif c.Live() && c.PrincipalKind == KindUser && c.PrincipalID == user {",
        new="\t\tif c.Live() && c.PrincipalKind == KindUser {",
        killer="TestRemovalReportsEveryLiveCredentialOfTheUser",
        why="the operator then revokes a credential belonging to somebody who was not removed.",
    ),
    Mutant(
        name="remove-member-with-no-journal-falls-through",
        path="cmd/cairn-server/removemember.go",
        old="\tif journal == \"\" {",
        new="\tif false {",
        killer="TestRemoveMemberWithNoJournalConfiguredRefusesRatherThanDefaulting",
        why="the refusal that names the variable to set. Without it the operator is handed "
        "whatever opening an empty path says, which names no remedy.",
    ),
    Mutant(
        name="remove-member-ignores-a-member-role",
        path="cmd/cairn-server/removemember.go",
        old="\tif strings.TrimSpace(*f.role) != \"\" {",
        new="\tif strings.TrimSpace(*f.role) != \"\" && false {",
        killer="TestEveryRemoveMemberRefusalWritesNOTHING",
        why="the flag is `-set-member`'s and a removal has no use for it, so ignoring it looks "
        "harmless — and `-remove-member … -member-role member`, an operator who meant to DEMOTE, "
        "removes the person instead.",
    ),
    Mutant(
        name="remove-member-does-not-require-a-user",
        path="cmd/cairn-server/removemember.go",
        old="\tif project == \"\" || user == \"\" {",
        new="\tif project == \"\" {",
        killer="TestEveryRemoveMemberRefusalWritesNOTHING",
        why="the prefix check catches an empty user anyway, so the flag check reads as "
        "duplication — and the operator who forgot a flag is told their value is malformed.",
    ),
    Mutant(
        name="remove-member-skips-the-swapped-ids-check",
        path="cmd/cairn-server/removemember.go",
        old="\tif !control.MustPrefix(project, control.PrefixProject) {",
        new="\tif false {",
        killer="TestEveryRemoveMemberRefusalWritesNOTHING",
        why="`control.RemoveMember` refuses a project id that names nothing, so this reads as a "
        "second authority; it is the only message that says the two flags were transposed.",
    ),
    Mutant(
        name="remove-member-relays-a-missing-project-without-the-remedy",
        path="cmd/cairn-server/removemember.go",
        old="\t\tcase errors.Is(err, control.ErrNoSuchProject):",
        new="\t\tcase errors.Is(err, control.ErrNoSuchProject) && false:",
        killer="TestEveryRemoveMemberRefusalWritesNOTHING",
        why="a sentinel is a guard only where something branches on it; the branch is what says "
        "the flag takes an id, not a name.",
    ),
    Mutant(
        name="remove-member-prints-no-grant-ids",
        path="cmd/cairn-server/removemember.go",
        old="\t\tfor _, g := range got.Grants {",
        new="\t\tfor _, g := range got.Grants[:0] {",
        killer="TestRemoveMemberWritesOneRecordAndReportsWhatItLeft",
        why="the summary line already says how many grants remain, so the per-grant lines read "
        "as verbosity. They are the only place the grant ids are — what the operator acts on.",
    ),
    Mutant(
        name="remove-member-prints-no-credential-ids",
        path="cmd/cairn-server/removemember.go",
        old="\t\tfor _, c := range got.Credentials {",
        new="\t\tfor _, c := range got.Credentials[:0] {",
        killer="TestRemoveMemberWritesOneRecordAndReportsWhatItLeft",
        why="the same, for the ids `-revoke-credential -credential` takes.",
    ),
    Mutant(
        name="remove-member-is-silent-about-an-empty-residue",
        path="cmd/cairn-server/removemember.go",
        old="\tif len(got.StillReaches) == 0 {",
        new="\tif len(got.StillReaches) == 0 && false {",
        killer="TestRemoveMemberSaysSoWhenItLeftNothing",
        why="printing only what remains looks tidier; then 'nothing remains' and 'the report "
        "never ran' are the same stderr.",
    ),
    Mutant(
        name="remove-member-missing-from-the-mode-ledger",
        path="cmd/cairn-server/main.go",
        old="\t\t{\"-remove-member\", *removeMember.enabled},\n",
        new="",
        killer="TestTheBinaryActuallyDispatchesRemoveMember",
        extra_killers=("TestTheBinaryActuallyDispatchesIssueCredential",),
        why="the copy that gets forgotten is the new one: `-set-member -remove-member` then "
        "runs ONE of them and exits 0, and `-routes -remove-member` removes nobody.",
    ),
    Mutant(
        name="a-mode-ledger-entry-nothing-sweeps",
        path="cmd/cairn-server/main.go",
        old="\t\t{\"-remove-member\", *removeMember.enabled},\n",
        # Borrows `-create-scope`'s flag so the tree compiles and `-routes` alone still runs:
        # the kill must be the ledger check's own message, not the positive control failing.
        new="\t\t{\"-remove-member\", *removeMember.enabled},\n\t\t{\"-phantom-mode\", *createScope.enabled},\n",
        killer="TestTheBinaryActuallyDispatchesIssueCredential",
        why="the pairwise sweep's own hand list (`modeArgs`) had stopped at five modes while the "
        "ledger grew to seven; a mode in the ledger and not in that list is never swept.",
    ),
    Mutant(
        name="remove-member-never-dispatched",
        path="cmd/cairn-server/main.go",
        old="\tif *removeMember.enabled {",
        new="\tif false {",
        killer="TestTheBinaryActuallyDispatchesRemoveMember",
        why="flags registered, ledger entry present, and the mode falls through to SERVING — "
        "which every in-process test of `runRemoveMember` is blind to.",
    ),
    Mutant(
        name="remove-member-flag-count-in-prose-goes-stale",
        path="cmd/cairn-server/main.go",
        old="`-remove-member`'s one",
        new="`-remove-member`'s two",
        killer="TestTheRegisteredFlagCountInProseMatchesTheCode",
        why="proves the new mode is IN the flag-count gate.",
    ),
    # --- group federation (`control.ResolveFederated`, `CAIRN_OIDC_GROUPS_CLAIM`) ---
    Mutant(
        name="federation-counts-only-the-first-mapped-group",
        path="internal/control/group_map.go",
        old="\t\t\tmapped = true\n\t\t\ta.addObject(m, gm.ObjectKind, gm.ObjectID, gm.Verbs)\n",
        new="\t\t\tmapped = true\n\t\t\tif len(report.Mapped) == 0 {\n\t\t\t\ta.addObject(m, gm.ObjectKind, gm.ObjectID, gm.Verbs)\n\t\t\t}\n",
        killer="TestTheAuthorizationMatrixIsExactlyThis",
        why="'first matching group wins' is how a role lookup is often written; it makes a caller in "
        "two groups reach only one of them (F2's write, F3's beacon-secrets write).",
    ),
    Mutant(
        name="federation-reports-an-unmapped-group-as-mapped",
        path="internal/control/group_map.go",
        old="\t\tmapped := false\n",
        new="\t\tmapped := true\n",
        killer="TestTheAuthorizationMatrixIsExactlyThis",
        why="an unmapped group then counts toward `mapped`, so a caller whose groups are all unmapped "
        "is told `mapped` and the projects-unmapped answer a self-view serves becomes unreachable.",
    ),
    Mutant(
        name="federation-claim-names-are-not-validated",
        path="internal/control/group_map.go",
        old="\t\tif err := ValidGroupName(g); err != nil {\n\t\t\treturn nil, fmt.Errorf(\"%w: %v\", ErrGroupsClaim, err)",
        new="\t\tif err := ValidGroupName(g); false && err != nil {\n\t\t\treturn nil, fmt.Errorf(\"%w: %v\", ErrGroupsClaim, err)",
        killer="TestAMalformedClaimIsRefusedWhole",
        extra_killers=("TestAMalformedGroupsClaimRefusesTheWholeToken",),
        why="'an unmapped name matches nothing anyway' reads as a reason to skip the check — and a "
        "claim the issuer got wrong is then read partially instead of refused.",
    ),
    Mutant(
        name="federation-ignores-the-provider-namespace",
        path="internal/control/group_map.go",
        old="\t\t\tif gm.Provider != provider || gm.Group != g {\n",
        new="\t\t\tif gm.Group != g {\n",
        killer="TestTheAuthorizationMatrixIsExactlyThis",
        why="a group name looks global; it is the issuer's vocabulary, and another issuer's `readers` "
        "would reach this issuer's callers (F1's atlas-runbook).",
    ),
    Mutant(
        name="group-unmapped-is-not-applied",
        path="internal/control/journal.go",
        old="\t\tdelete(m.GroupMaps, key)\n",
        new="\t\t_ = key\n",
        killer="TestTheAuthorizationMatrixIsExactlyThis",
        extra_killers=("TestMapGroupAndUnmapGroupRefuseBeforeWritingAnything",),
        why="an unmap that replays clean and removes nothing — the group keeps reaching the object "
        "(F5's atlas-runbook admin).",
    ),
    Mutant(
        name="identity-valid-refuses-a-federated-principal",
        path="internal/identity/identity.go",
        old="\treturn i.Principal.Kind.IsPrincipal() && i.Principal.ID",
        new="\treturn i.Principal.Kind.Valid() && i.Principal.ID",
        killer="TestTheServerServesAFederatedPrincipalExactlyItsMappedScope",
        extra_killers=("TestAFederatedTokenReachesExactlyItsMappedGroupsAndWritesNothing",),
        why="`Kind.Valid` reads as 'a real kind', and is the journal's SUBJECT check; using it here "
        "makes every federated token a uniform 401 — the seam neither half sees alone.",
    ),
    Mutant(
        name="federation-reads-the-claim-with-the-mode-off",
        path="internal/identity/supabase.go",
        old="\tif s.groupsClaim != \"\" {\n\t\tgroups, err := claimedGroups(",
        new="\tif true {\n\t\tgroups, err := claimedGroups(",
        killer="TestWithTheModeOffAGroupsClaimIsIgnored",
        why="the compatibility claim: a deployment that never set the variable must resolve journal "
        "users exactly as before, whatever claims its issuer happens to emit.",
    ),
)


#: 🔴 THE SLOWEST ROWS, DEALT FIRST WHEN THE BATTERY IS SHARDED, SO THEY CANNOT STACK IN ONE
#: SHARD AND MAKE IT THE JOB'S CRITICAL PATH. `partition` deals these round-robin before anything
#: else, which puts up to N of them in N different shards. MEASURED, not guessed: the rows that
#: took 40 s or more in a full serial run of the narrowed battery (24-core host, load ~6-11) —
#: each waits out a deadline inside its killing test. (One more row ran into the 2m `-timeout`
#: when measured WITHOUT the `-run` filter — a different test in its package deadlocks on the
#: mutant — and is not listed: with the filter that test never starts and the row takes seconds.)
#:
#: Named here, in the RUNNER, rather than as a field on the row: the rows are shared with other
#: branches and this is a scheduling hint, not a claim about the guard. ⚠ A name here that no row
#: carries is REFUSED at start-up, so a renamed row cannot leave the hint pointing at nothing —
#: and a slow row MISSING from it costs wall clock, never a verdict.
SLOW_ROWS: frozenset[str] = frozenset({
    "presence-main-ignores-the-agent-bind-refusal",
    "presence-agent-bind-reachability-unchecked",
    "unconfigured-deployment-gets-a-TYPED-nil-session-authority",
    "an-unreadable-store-root-is-swallowed",
    "ui-startup-serves-an-unreachable-database",
})


def go_available() -> bool:
    return shutil.which("go") is not None


def prepare_tree(dest: Path) -> None:
    """Copy the module into an isolated tree.

    🔴 `.git` IS EXCLUDED RATHER THAN COPIED. A copy that carries `.git` shares the
    ORIGINAL's index, refs and reflog when the source is a linked worktree (where `.git`
    is a FILE holding `gitdir: ...`), so a stray command inside the copy lands on the real
    branch. Excluding it also means a mutated tree can never be committed by accident,
    which is the failure mode that matters for a battery that edits source in place.
    `mutant_tree.copy_module` refuses if one arrives anyway.
    """
    mutant_tree.copy_module(REPO_ROOT, dest)


def run_tests(tree: Path, pkgs: tuple[str, ...] = (),
              run: str | None = None) -> tuple[bool, set[str], str]:
    """Run the package's tests, returning (green, failing test names, raw output).

    🔴 THE FAILING TEST NAMES COME FROM `--- FAIL:` LINES, NOT FROM THE EXIT CODE. An
    exit code says something went wrong; it cannot say WHICH guard noticed, and a mutant
    killed by the wrong guard is the green-for-the-wrong-reason shape this module exists
    to refuse. A build failure produces a non-zero exit with NO `--- FAIL:` lines at all,
    which is reported as its own outcome rather than silently scored as a kill.
    """
    proc = subprocess.run(
        # 🔴 `-timeout=2m`, NOT THE 10m DEFAULT, BECAUSE A MUTANT CAN DEADLOCK RATHER
        # THAN FAIL. `TestAWriteDoesNotCommitOverAnAttemptThatSTARTEDAfterIt` re-enters
        # `Cache.refresh` from inside `CacheOptions.Now`, which is only safe because
        # `clock()` is read OUTSIDE `c.mu` at all three of its call sites. A mutation
        # that moves any one of them below `c.mu.Lock()` deadlocks on a non-reentrant
        # `sync.Mutex` — the mutant is still KILLED, but at the default it costs ten
        # minutes of wall clock per occurrence instead of two, in a battery this job
        # runs on every push. The shortest real package here finishes in seconds.
        ["go", "test", "-count=1", "-timeout=2m", "-v",
         *(["-run", run] if run else []), *(pkgs or PKGS)],
        cwd=tree,
        capture_output=True,
        text=True,
    )
    out = proc.stdout + proc.stderr
    failing = set(re.findall(r"^\s*--- FAIL: (\S+)", out, re.MULTILINE))
    # A subtest failure is reported as `Parent/child`; attribute it to the parent, which
    # is the function a row names.
    failing = {name.split("/", 1)[0] for name in failing}
    passed = re.findall(r"^\s*--- PASS: (\S+)", out, re.MULTILINE)
    green = proc.returncode == 0 and not failing and bool(passed)
    return green, failing, out


def apply_mutation(tree: Path, m: Mutant):
    """The edit, as a context manager that restores the original bytes and VERIFIES it."""
    return mutant_tree.mutated(
        tree / m.path, m.old, m.new, m.occurrences,
        lambda found: MutationError(
            f"{m.name}: pattern occurs {found} time(s) in {m.path}, expected {m.occurrences}. "
            "A pattern that matches nothing scores the mutant SURVIVED without ever running; "
            "a pattern that matches too much mutates code this row does not name. Re-derive it."
        ),
    )


def scope_for(m: Mutant, index: dict[str, set[str]]) -> tuple[tuple[str, ...], str | None]:
    """The packages (and the `-run` filter) a row's verdict needs — and nothing more.

    🔴 NARROWED TO WHERE THE NAMED TESTS LIVE, LOCATED MECHANICALLY. Every package holding
    `killer` or an `extra_killers` entry — found by `func TestX(` in a `*_test.go`, not by a
    hand-kept map — plus whatever `pkgs` ADDS to the seam. That is every test whose result
    the verdict below reads: KILLED needs `killer` in `failing`, the ledger needs every extra
    there too. Running the other packages of `PKGS` bought one thing only, the chance that
    some OTHER guard went red and the row read MISATTRIBUTED; it cost a full recompile and
    run of the whole seam per row, which was most of the battery's wall clock.

    ⚠ THAT IS THE ONE SEMANTIC SHIFT, AND IT CANNOT TURN A FAILING ROW INTO A PASSING ONE.
    A row whose named killer did NOT fire, while some unnamed test did, used to read
    MISATTRIBUTED; now the unnamed test may not have run and the row reads SURVIVED. Both are
    refusals of the battery. A row whose killer DID fire reads exactly as before, because
    the killer and every extra still run.

    ⚠ A NAME THAT CANNOT BE LOCATED IS REFUSED, NEVER NARROWED TO NOTHING. A row naming a
    test that does not exist would otherwise run zero tests and the empty `go test` would
    be scored by whatever its exit code happened to be.

    ⚠ TWO ROW SHAPES KEEP THE WHOLE SEAM, ON PURPOSE: an `equivalent` row, whose claim is
    that NO test in the seam notices, so narrowing it would weaken the claim it makes; and a
    row with an empty `killer`, whose claim is "any failure counts" and which names nothing
    to locate.
    """
    if m.equivalent or not m.killer:
        return tuple(m.pkgs or PKGS), None
    names = (m.killer, *m.extra_killers)
    unlocated = [n for n in names if not index.get(n)]
    if unlocated:
        raise MutationError(
            f"{m.name}: cannot locate {unlocated} as `func <name>(` in any *_test.go. A row "
            "whose named guard does not exist would run nothing and be scored by an exit code."
        )
    found = set().union(*(index[n] for n in names)) | (set(m.pkgs) - set(PKGS))
    ordered = tuple(p for p in PKGS if p in found) + tuple(sorted(found - set(PKGS)))
    return ordered, mutant_tree.run_pattern(names)


@dataclass
class Tally:
    """One run's verdicts, by row name — the unit a shard writes and the aggregate sums."""

    killed: list[str] = field(default_factory=list)
    survived: list[str] = field(default_factory=list)
    misattributed: dict[str, list[str]] = field(default_factory=dict)
    broken: dict[str, str] = field(default_factory=dict)
    stale_extras: dict[str, dict[str, list[str]]] = field(default_factory=dict)


def judge(by_name: dict[str, Mutant], selected: list[str], t: Tally) -> bool:
    """Print the SUMMARY and every refusal for `selected`; True when the run may pass.

    🔴 ONE FUNCTION, CALLED BY AN UNSHARDED RUN, BY EACH SHARD AND BY THE AGGREGATE, so the
    refusals cannot drift apart between them — a shard that enforced a weaker rule than the
    whole battery would be a gate that loosened by being split.
    """
    print()
    expected_survivors = {n for n in selected if by_name[n].equivalent}
    actual_survivors = set(t.survived)
    print(
        f"SUMMARY mutants={len(selected)} killed={len(t.killed)} survived={len(t.survived)} "
        f"misattributed={len(t.misattributed)} harness-errors={len(t.broken)} "
        f"stale-extras={len(t.stale_extras)}"
    )

    ok = True
    # A row with NO verdict — a shard that stopped early still writes its results — must not
    # simply be absent from every count, or the SUMMARY reads short and nothing refuses.
    judged = set(t.killed) | set(t.survived) | set(t.misattributed) | set(t.broken)
    unjudged = [n for n in selected if n not in judged]
    if unjudged:
        print(f"\n🔴 NO VERDICT for {unjudged} — a count that omits rows is not a count of the "
              "battery.", file=sys.stderr)
        ok = False
    for name, exc in t.broken.items():
        print(f"\n🔴 HARNESS: {name}\n{exc}", file=sys.stderr)
        ok = False
    for name, failing in t.misattributed.items():
        print(
            f"\n🔴 MISATTRIBUTED: {name} was killed by {sorted(failing)}, not by its named "
            f"guard {by_name[name].killer}. That proves the suite can fail and proves nothing "
            "about the guard this row exists for.",
            file=sys.stderr,
        )
        ok = False

    # 🔴 A LISTED EXTRA KILLER THAT DID NOT FAIL IS A FAILURE, NOT A NOTE. The whole point
    # of making the field real is that a guard which quietly stopped noticing is exactly
    # what a battery is for; reporting it without failing would be the inert field again,
    # one layer up. The remedy is per row and is a DECISION: either the guard was moved out
    # from under this mutant's observable — fix the guard — or the row's list was aspirational
    # and must be narrowed, with the reason written down beside it.
    for name, row in t.stale_extras.items():
        print(
            f"\n🔴 STALE extra_killers: {name} lists {sorted(row['absent'])}, which did NOT "
            f"fail. What actually failed: {sorted(row['failing'])}. A listed killer that has "
            "stopped killing reads as coverage and provides none — decide whether the GUARD "
            "moved or the LIST was wrong, and say which in the row.",
            file=sys.stderr,
        )
        ok = False

    unexpected = actual_survivors - expected_survivors
    if unexpected:
        print(f"\n🔴 SURVIVED WITHOUT AN EQUIVALENT LABEL: {sorted(unexpected)}", file=sys.stderr)
        print("  Each is a guard the suite does not actually have.", file=sys.stderr)
        ok = False

    mislabelled = expected_survivors - actual_survivors
    if mislabelled:
        # Not a failure: a mutant labelled EQUIVALENT that gets KILLED means the label was
        # too generous and a real guard exists. It is reported so the label gets corrected.
        print(
            f"\n⚠ LABELLED EQUIVALENT BUT KILLED: {sorted(mislabelled)} — the label is wrong "
            "and should be removed; a guard does see this.",
            file=sys.stderr,
        )

    for name in t.survived:
        if by_name[name].equivalent:
            print(f"\nEQUIVALENT {name}: {by_name[name].equivalent_reason}")

    return ok


def table_digest(rows=None) -> str:
    """Names plus edits of every row, so an aggregate refuses shards cut from another table."""
    h = hashlib.sha256()
    for m in rows if rows is not None else MUTANTS:
        h.update(repr((m.name, m.path, m.old, m.new, m.killer, m.extra_killers)).encode())
    return h.hexdigest()


def aggregate(results_dir: Path) -> int:
    """The SEAM GUARD over the shards, and the battery's one SUMMARY rebuilt from them.

    🔴 EVERY SHARD CAN BE GREEN WHILE THE BATTERY IS NOT. A shard enforces the refusals over
    the rows it was handed; a row handed to NO shard is enforced by nobody, and a whole shard
    whose result never arrived leaves the others green. `mutant_tree.seam_violations` refuses
    unless the union of the shards' rows is this table's rows, each exactly once, from one
    shard count, cut from one table. Exit 2 for that — "could not vouch", never "failed" —
    and then `judge` over the union, which is the old single run's SUMMARY and refusals.
    """
    expected = [m.name for m in MUTANTS]
    loaded, problems = mutant_tree.load_shard_results(
        results_dir, "control", table_digest(), expected)
    if problems:
        return 2

    t = Tally()
    for r in loaded:
        v = r["tally"]
        t.killed += v["killed"]
        t.survived += v["survived"]
        t.misattributed.update(v["misattributed"])
        t.broken.update(v["broken"])
        t.stale_extras.update(v["stale_extras"])
    by_name = {m.name: m for m in MUTANTS}
    return 0 if judge(by_name, expected, t) else 1


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--show", action="store_true", help="print each edit and exit without running")
    ap.add_argument("--only", help="run one mutant by name")
    ap.add_argument("--shard", help="run shard i of N (`i/N`), a deterministic partition")
    ap.add_argument("--jobs", type=int, default=1,
                    help="worker trees run in parallel (default 1); each gets its own copy")
    ap.add_argument("--results", type=Path, help="write this run's verdicts as JSON here")
    ap.add_argument("--aggregate", type=Path, metavar="DIR",
                    help="check the shard results in DIR cover the battery exactly once, and "
                    "print the battery's SUMMARY from them; runs no mutant")
    args = ap.parse_args()

    stale_hint = sorted(SLOW_ROWS - {m.name for m in MUTANTS})
    if stale_hint:
        print(f"control_mutants: SLOW_ROWS names no row: {stale_hint}", file=sys.stderr)
        return 2

    if args.aggregate:
        return aggregate(args.aggregate)

    if args.show:
        for m in MUTANTS:
            label = "EQUIVALENT" if m.equivalent else f"must be killed by {m.killer}"
            print(f"\n=== {m.name}  [{label}]\n    {m.path}\n    why: {m.why}")
            print(f"    -   {m.old!r}\n    +   {m.new!r}")
        return 0

    if not go_available():
        print("control_mutants: REFUSING — no `go` on PATH.", file=sys.stderr)
        print(
            "  This is a refusal, not a skip. A battery that reports nothing because its "
            "toolchain is absent is indistinguishable from one that found nothing, and a "
            "skip nobody counts is a pass.",
            file=sys.stderr,
        )
        return 2

    selected = [m for m in MUTANTS if not args.only or m.name == args.only]
    if args.only and not selected:
        print(f"control_mutants: no mutant named {args.only!r}", file=sys.stderr)
        return 2
    shard = (1, 1)
    if args.shard:
        try:
            shard = mutant_tree.parse_shard(args.shard)
        except ValueError as exc:
            print(f"control_mutants: {exc}", file=sys.stderr)
            return 2
        selected = mutant_tree.partition(selected, shard[1], key=lambda m: m.name,
                                         front=SLOW_ROWS)[shard[0] - 1]
        print(f"SHARD {shard[0]}/{shard[1]}: {len(selected)} of {len(MUTANTS)} rows")
    jobs = max(1, min(args.jobs, len(selected) or 1))

    index = mutant_tree.go_test_index(REPO_ROOT)
    t = Tally()
    scopes: dict[str, tuple[tuple[str, ...], str | None]] = {}
    for m in selected:
        try:
            scopes[m.name] = scope_for(m, index)
        except MutationError as exc:
            t.broken[m.name] = str(exc)
    # The positive control covers every package any selected row runs — `PKGS` and whatever
    # a row's located killers add — so no row's KILLED can be a package that was red unedited.
    control_pkgs = PKGS + tuple(sorted(
        {p for pk, _ in scopes.values() for p in pk} - set(PKGS)))

    owned_tmp = tempfile.TemporaryDirectory(prefix="cairn-control-mutants-")
    work = Path(owned_tmp.name)
    trees = [work / f"w{k}" for k in range(jobs)]
    positive = "red"
    try:
        for tree in trees:
            prepare_tree(tree)
        before = mutant_tree.tree_digest(trees[0])
        # The positive control runs in the FIRST tree only, so every other tree must be
        # byte-identical to it or the control vouches for a tree the rows do not run in.
        unlike = [str(tr) for tr in trees[1:] if mutant_tree.tree_digest(tr) != before]
        if unlike:
            print(f"control_mutants: REFUSING — worker trees {unlike} differ from {trees[0]}",
                  file=sys.stderr)
            return 2

        # 🔴 THE POSITIVE CONTROL, FIRST. Without it a KILLED verdict cannot be told
        # apart from a tree that never compiled.
        print("positive control (the copied tree, UNEDITED) ... ", end="", flush=True)
        green, failing, out = run_tests(trees[0], control_pkgs)
        if not green:
            print("RED")
            # The verdict lines FIRST: with every package in one run, the failing one is
            # usually far above the last 4000 characters.
            verdicts = re.findall(r"^(?:\s*--- FAIL: .*|FAIL\s.*|panic: .*)$", out, re.MULTILINE)
            print("\n".join(verdicts) or "(no FAIL lines)", file=sys.stderr)
            print(out[-4000:], file=sys.stderr)
            print(
                "\ncontrol_mutants: REFUSING TO VOUCH — the unedited copy is not green, so "
                "every mutant below would score KILLED for a reason that has nothing to do "
                "with its guard.",
                file=sys.stderr,
            )
            return 1
        positive = "green"
        print("GREEN", flush=True)

        lock = threading.Lock()
        free: queue.Queue[Path] = queue.Queue()
        for tree in trees:
            free.put(tree)

        abort = threading.Event()
        fatal: list[str] = []

        def one(m: Mutant) -> None:
            if m.name in t.broken:
                with lock:
                    print(f"  {m.name:<46} HARNESS ERROR", flush=True)
                return
            pkgs, run = scopes[m.name]
            tree = None
            while tree is None:
                if abort.is_set():
                    return
                try:
                    tree = free.get(timeout=1)
                except queue.Empty:
                    pass
            started = time.monotonic()
            try:
                with apply_mutation(tree, m):
                    green, failing, out = run_tests(tree, pkgs, run)
            except MutationError as exc:
                free.put(tree)
                with lock:
                    print(f"  {m.name:<46} HARNESS ERROR", flush=True)
                    t.broken[m.name] = str(exc)
                return
            except mutant_tree.RestoreError as exc:
                # The tree is NOT handed back, and nothing else starts: no row may run on a
                # tree whose contents are no longer known.
                with lock:
                    fatal.append(f"{m.name}: {exc}")
                abort.set()
                return
            except BaseException:
                # 🔴 ANY OTHER EXCEPTION STOPS THE RUN, LOUDLY. A `UnicodeDecodeError` out of
                # `subprocess.run(text=True)`, an `OSError`: before this arm the tree never came
                # back, `abort` stayed clear, and every later task spun on `free.get` forever —
                # MEASURED, the run hung rather than failing. Nothing else starts (the tree's
                # state is unknown), and the exception re-raises out of `fut.result()` so the run
                # exits non-zero with its traceback, as the serial loop this replaced did.
                abort.set()
                raise
            free.put(tree)
            with lock:
                if green:
                    verdict = "SURVIVED"
                    t.survived.append(m.name)
                elif not failing:
                    # Non-zero with no `--- FAIL:` line: the tree did not build. That is a
                    # harness problem, not a kill.
                    verdict = "DID NOT BUILD"
                    t.broken[m.name] = out[-1500:]
                elif m.killer and m.killer not in failing:
                    verdict = f"MISATTRIBUTED ({', '.join(sorted(failing))})"
                    t.misattributed[m.name] = sorted(failing)
                else:
                    # 🔴 THE `extra_killers` LEDGER IS CHECKED HERE, AND ONLY ON A KILL. The
                    # row has already been attributed to its named guard; what is left is the
                    # claim that the OTHER listed guards saw it too. A missing entry is a
                    # finding, not a verdict downgrade: the mutant WAS killed, and what has
                    # gone stale is the ledger's description of who noticed.
                    absent = [k for k in m.extra_killers if k not in failing]
                    verdict = "killed" if not absent else f"killed, STALE EXTRAS ({', '.join(sorted(absent))})"
                    if absent:
                        t.stale_extras[m.name] = {"absent": absent, "failing": sorted(failing)}
                    t.killed.append(m.name)
                print(f"  {m.name:<46} {verdict}  [{time.monotonic() - started:.1f}s]", flush=True)

        with ThreadPoolExecutor(max_workers=jobs) as pool:
            for fut in [pool.submit(one, m) for m in selected]:
                fut.result()
        if fatal:
            print("\ncontrol_mutants: REFUSING TO VOUCH — a restore FAILED, so the run stopped:\n  "
                  + "\n  ".join(fatal), file=sys.stderr)
            return 2

        # 🔴 THE WHOLE-TREE CHECK. Per-file restores are verified as they happen; this is
        # the coarser claim that NOTHING else in a reused tree moved either — a test that
        # writes into its own package, say — since every later row would run on top of it.
        drifted = [str(tr) for tr in trees if mutant_tree.tree_digest(tr) != before]
        if drifted:
            print(f"\ncontrol_mutants: REFUSING TO VOUCH — worker tree(s) {drifted} no longer "
                  "match the copy they started as; verdicts after the drift describe a tree "
                  "nobody declared.", file=sys.stderr)
            return 2
        print(f"\nworker trees restored: {len(trees)} tree(s) hash {before[:16]}… before and after")
    finally:
        owned_tmp.cleanup()
        if args.results:
            args.results.parent.mkdir(parents=True, exist_ok=True)
            args.results.write_text(json.dumps({
                "battery": "control",
                "shard": list(shard),
                "table": table_digest(),
                "selected": [m.name for m in selected],
                "positive_control": positive,
                "tally": asdict(t),
            }, indent=1, sort_keys=True), encoding="utf-8")

    # Table order, so a sharded or parallel run reads the same as a serial one.
    order = {m.name: k for k, m in enumerate(MUTANTS)}
    t.killed.sort(key=order.get)
    t.survived.sort(key=order.get)
    by_name = {m.name: m for m in MUTANTS}
    return 0 if judge(by_name, [m.name for m in selected], t) else 1


if __name__ == "__main__":
    sys.exit(main())
