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
must kill it, and a mutant killed only by some OTHER test is reported as a MISATTRIBUTED
kill, which is a finding rather than a pass.

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

Exit 0 only when the positive control is GREEN, every mutant is KILLED, and every kill
is attributed to the test that claims it. Anything else exits 1 and says which.
"""
from __future__ import annotations

import argparse
import re
import shutil
import subprocess
import sys
import tempfile
from dataclasses import dataclass, field
from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parents[1]

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
#: which is the same treatment the mutant count already has — so a `PKGS` edit that leaves
#: prose stale is a red test rather than an instruction nobody reads.
#:
#: The seam itself, which is the reason a battery scoped to ONE package would be wrong: a
#: mutant in the token-file projection is killed by a guard in the server and vice versa,
#: and a mutant in the identity backends is killed by a guard in the model — so scoping to
#: either side alone scores those SURVIVED while the suite that catches them was never run.
#: A SURVIVED mutant that only means "the killing test did not run" is a false finding that
#: reads as a coverage gap and sends the next reader to write a test that already exists.
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
    # The Go test function that must fail. A kill by anything else is MISATTRIBUTED.
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
    #: So the field now means "the seam PLUS these", and `run_tests` receives the union.
    #: A row naming a package already in `PKGS` is accepted and redundant; a row that omits
    #: one cannot exist.

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
        old="\t\tcase ObjectProject:\n\t\t\tfor _, scopeID := range m.ScopesIn(g.ObjectID) {\n\t\t\t\ta.add(scopeID, g.Verbs)\n\t\t\t}",
        new="\t\tcase ObjectProject:\n\t\t\tcontinue",
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
        old="\tif only == nil {\n\t\treturn a\n\t}",
        new="\tif len(only) == 0 {\n\t\treturn a\n\t}",
        killer="TestNarrowingIntersectsAndCannotWiden",
        why="the single most likely edit anybody makes to this function — `len(x) == 0` "
        "reads as the idiomatic nil check and collapses the two opposite states.",
    ),
    Mutant(
        name="narrowing-does-not-narrow",
        path="internal/control/resolve.go",
        old="\t\tif _, wanted := keep[id]; !wanted {\n\t\t\tcontinue\n\t\t}",
        new="\t\tif false {\n\t\t\tcontinue\n\t\t}",
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
        old="return p, Narrow(Resolve(m, p), matched.NarrowedScopes), nil",
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
    Mutant(
        name="ui-invite-flow-acts-as-a-narrowed-principal",
        path="internal/ui/invitehandlers.go",
        old="\tif id.Auth.Narrowed() {\n\t\treturn control.Principal{}\n\t}",
        new="\tif false && id.Auth.Narrowed() {\n\t\treturn control.Principal{}\n\t}",
        killer="TestANarrowedBearerCannotMintAnInvitation",
        why="membership authority is decided from the principal's project ROLE, which no "
        "scope narrowing bounds — so a narrowed bearer token mints an invitation into its "
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
        name="ui-invite-mint-display-bypasses-membership-actor",
        path="internal/ui/invitehandlers.go",
        old="pickProject(s.inviting.Invitable(membershipActor(id)), project)",
        new="pickProject(s.inviting.Invitable(id.Principal), project)",
        killer="TestEveryMembershipDecisionActsAsMembershipActor",
        why="behaviourally INVISIBLE today — this read only finds a display name, and `Mint` "
        "refuses on its own — which is exactly why the ledger, not a behavioural test, is its "
        "killer: the next edit that trusts this list would inherit an un-narrowed one.",
    ),
    Mutant(
        name="ui-share-page-candidates-bypass-membership-actor",
        path="internal/ui/sharehandlers.go",
        old="\tcandidates, err := s.sharing.Candidates(membershipActor(id))\n\tif err != nil {\n\t\twritePlain(w, http.StatusInternalServerError, \"the authority could not be read\")\n\t\treturn\n\t}\n\n\tview.Scope",
        new="\tcandidates, err := s.sharing.Candidates(id.Principal)\n\tif err != nil {\n\t\twritePlain(w, http.StatusInternalServerError, \"the authority could not be read\")\n\t\treturn\n\t}\n\n\tview.Scope",
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
        old='\t\tif e.Verbs.Empty() {\n\t\t\treturn fmt.Errorf("%s: verbs is empty',
        new='\t\tif false {\n\t\t\treturn fmt.Errorf("%s: verbs is empty',
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
        old="\tif g.ObjectKind != control.ObjectScope || !auth.Allows(g.ObjectID, control.VerbAdmin) {",
        new="\tif g.ObjectKind != control.ObjectScope {\n\t\t_ = auth",
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
        "\t\treturn\n"
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
        "it never consults `refuseOrphaning`, which lives only on the operator's "
        "`PlanMemberSet` path. So an `admin` could mint a `member` invitation into their own "
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
        extra_killers=("TestTheRoleChooserIsDrivenByTheREALModelsHeldRole",),
        why="THE MEASURED SEAM. This mutant SURVIVED a fully green suite before those two "
        "guards existed: one side tested the model against a hand-built Model, the other "
        "tested the chooser against a hand-built NamedProject, and neither built the "
        "combined state. Its only symptom was a role chooser that silently offered nothing.",
    ),
    Mutant(
        name="namedproject-reports-a-constant-role",
        path="internal/control/invite_authority.go",
        old="out = append(out, NamedProject{ID: p.ID, Name: p.Name, HeldRole: ship.Role})",
        new="out = append(out, NamedProject{ID: p.ID, Name: p.Name, HeldRole: RoleOwner})",
        killer="TestProjectsManagedByCarriesTheCallersOwnRole",
        why="the direction a zero-check cannot see: a role that is present and WRONG, in the "
        "permissive direction. It offers every admin the `owner` option, which the mint then "
        "refuses — so the visible symptom is a form that errors rather than one that is empty.",
    ),
    Mutant(
        name="ui-mint-stops-checking-conferrability",
        path="internal/ui/inviting.go",
        old="if !held.CanConfer(role) {",
        new="if false {",
        killer="TestAnAdminCannotMintAnOwnerInvitation",
        why="the escalation itself, at the one gate that is load-bearing. The chooser's "
        "filtering constrains a browser and nothing else; this is the check an HTTP client "
        "has to get past.",
    ),
    Mutant(
        name="ui-mint-error-loses-its-sentinel",
        path="internal/ui/inviting.go",
        old='"%w: a %q may not confer %q (see control.Role.CanConfer)",\n\t\t\tErrRoleNotConferrable, held, role)',
        new='"ui: a %q may not confer %q (see control.Role.CanConfer)", held, role)',
        killer="TestAnAdminCannotMintAnOwnerInvitation",
        why="a `fmt.Errorf` written without `%w`, which is the commonest way a sentinel stops "
        "being recognisable. `refuseInviteWrite` maps on `errors.Is`, so the refusal becomes a "
        "500 — the wrong status, and a sentence that sends an operator hunting a broken server.",
    ),
    Mutant(
        name="ui-invite-narrowing-skipped-before-the-unnarrowed-read",
        path="internal/ui/invitehandlers.go",
        old="\tchosen, found := pickProject(view.Projects, project)\n\tif !found {\n\t\twritePlain(w, http.StatusNotFound, inviteRefusal)\n\t\treturn\n\t}",
        new="\tchosen, _ := pickProject(view.Projects, project)",
        killer="TestAProjectThatIsNotInvitableIsRefusedBEFORETheUnnarrowedRead",
        why="`Inviting.Outstanding` performs NO authority check and says so in its own doc — "
        "the narrowing is the only thing in front of it. Dropping the refusal turns the page "
        "into a listing of who is being invited where, for any project id a caller names.",
    ),
    Mutant(
        name="ui-mint-response-loses-no-store",
        path="internal/ui/invitehandlers.go",
        old="\twriteHTMLNoStore(w, http.StatusOK, b.String())",
        new="\twriteHTML(w, http.StatusOK, b.String())",
        killer="TestTheMintedTokenIsRenderedOnceUnderNoStoreAndNeverLogged",
        why="the ordinary render helper, reached for because it is the one every other page "
        "uses. This response's BODY is a bearer capability that can create a principal, and a "
        "shared cache or a back-forward store keeping it is the whole exposure.",
    ),
    Mutant(
        name="ui-minted-link-becomes-an-anchor",
        path="internal/ui/render.go",
        old='h.P(h.Class("invite-link"), g.Text(m.Link)),',
        new='h.P(h.Class("invite-link"), h.A(h.Href(m.Link), g.Text(m.Link))),',
        killer="TestTheMintedTokenIsRenderedOnceUnderNoStoreAndNeverLogged",
        why="the single most natural improvement to make to this page — a URL rendered as "
        "text looks like an oversight. The link is a PATH with no origin, so an anchor "
        "resolves it against THIS page: one stray click, or one link-prefetcher, and the "
        "minter has redeemed the invitation on themselves.",
    ),
    Mutant(
        name="ui-mint-logs-the-token",
        path="internal/ui/invitehandlers.go",
        old='s.logf("an invitation was minted: project=%s role=%s expires=%s by=%s",\n\t\tinv.ProjectID, inv.Role, inv.ExpiresAt.UTC().Format(time.RFC3339), inv.Inviter)',
        new='s.logf("an invitation was minted: project=%s role=%s expires=%s by=%s token=%s",\n\t\tinv.ProjectID, inv.Role, inv.ExpiresAt.UTC().Format(time.RFC3339), inv.Inviter, token)',
        killer="TestTheMintedTokenIsRenderedOnceUnderNoStoreAndNeverLogged",
        why="one field added to a log line while debugging, and never taken out. This is the "
        "shape that put a 64-character secret into the control journal once already, and the "
        "value here can create a principal.",
    ),
    Mutant(
        name="ui-join-page-resolves-the-token",
        path="internal/ui/invitehandlers.go",
        old="\ts.render(w, JoinPage(token, s.providerArmed()))",
        new="\tif s.inviting != nil {\n\t\t_, _ = s.inviting.Outstanding(control.ID(token))\n\t}\n\ts.render(w, JoinPage(token, s.providerArmed()))",
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
        old="\tinviteToken := r.PostFormValue(inviteTokenField)",
        new="\tinviteToken := r.FormValue(inviteTokenField)",
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
    Mutant(
        name="ui-join-page-offers-a-form-with-no-token",
        path="internal/ui/render.go",
        old='\t\t\t\tg.If(token != "" && provider, joinForm(token)),',
        new="\t\t\t\tg.If(provider, joinForm(token)),",
        killer="TestTheJoinPageWithoutATokenSaysSoAndOffersNoForm",
        why="a simplification of a two-part condition. Submitting the empty form opens a "
        "flight carrying no invitation and completes as an ORDINARY sign-in, so somebody who "
        "was invited ends up signed in as nobody — or refused — with nothing saying the link "
        "was at fault.",
    ),
    Mutant(
        name="ui-role-chooser-stops-filtering",
        path="internal/ui/render.go",
        old="\tconferrable := conferrableRoles(v.Project.HeldRole)",
        new="\tconferrable := control.AllRoles",
        killer="TestTheRoleChooserOffersOnlyWhatTheCallerMayConfer",
        why="`grantableVerbs = control.AllVerbs` one file over is exactly this shape and is "
        "CORRECT there, which is what makes it the natural edit here. Roles differ: the mint "
        "refuses `owner` from an admin, so an unfiltered list offers a value that cannot work.",
    ),
    Mutant(
        name="ui-role-chooser-default-follows-the-list-order",
        path="internal/ui/render.go",
        old="g.If(r == leastPrivilegedRole, h.Selected()),",
        new="g.If(false, h.Selected()),",
        killer="TestTheRoleChooserOffersOnlyWhatTheCallerMayConfer",
        why="a `selected` attribute reads as cosmetic. `control.AllRoles` is in DESCENDING "
        "authority and a `select` with no explicit selection submits its FIRST option — so "
        "removing this makes an unread form confer OWNERSHIP of a project.",
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
        name="ui-invite-write-refusals-collapse-into-one",
        path="internal/ui/invitehandlers.go",
        old="\tcase errors.Is(err, ErrRoleNotConferrable):",
        new="\tcase false:",
        killer="TestTheTwoInviteWriteRefusalsAnswerDifferentlyForDifferentReasons",
        why="the tidy-up that makes every refusal uniform, which is this surface's own rule "
        "elsewhere and is wrong here: a person who picked `owner` from a list would be told "
        "'that invitation cannot be recorded' and could never learn why.",
    ),
    Mutant(
        name="ui-invite-read-refuses-instead-of-answering-with-no-store",
        path="internal/ui/invitehandlers.go",
        old="\tif s.inviting == nil {\n\t\t// Nothing to ask. The page says so — see [NoInviteStore] for why this is a page\n\t\t// rather than a refusal.\n\t\ts.renderInvite(w, view)\n\t\treturn\n\t}",
        new="\tif s.inviting == nil {\n\t\ts.refuseWithoutInviteStore(w)\n\t\treturn\n\t}",
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
        old='h.P(h.Class("nav-invite"), h.A(h.Href(InvitePath), g.Text("Invitations"))),',
        new='h.P(h.Class("nav-invite"), g.Text("Invitations")),',
        killer="TestEveryRenderedPageCarriesBothNavigationAffordances",
        why="an affordance that looks like a label is how the SHARE flow shipped deployed, "
        "authorised, route-registered, test-covered and reported MISSING. Same surface, same "
        "defect, one flow along — and every gate but this one stays green.",
    ),
    Mutant(
        name="ui-join-page-gains-authenticated-navigation",
        path="internal/ui/render.go",
        old='\t\t\th.Header(h.Class("page-header"), h.H1(g.Text("cairn"))),\n\t\t\th.Main(\n\t\t\t\th.Class("join-main"),',
        new='\t\t\th.Header(h.Class("page-header"), h.H1(g.Text("cairn")),\n\t\t\t\th.P(h.Class("nav-share"), h.A(h.Href(SharePath), g.Text("Sharing")))),\n\t\t\th.Main(\n\t\t\t\th.Class("join-main"),',
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
        old="\tscope, found := pickScope(scopes, wanted)\n\tif !found {\n\t\twritePlain(w, http.StatusNotFound, browseRefusal)\n\t\treturn\n\t}\n\t// 🔴 THE SESSIONS",
        new="\t_, _ = s.source.Touched(id.Auth, string(wanted))\n\tscope, found := pickScope(scopes, wanted)\n\tif !found {\n\t\twritePlain(w, http.StatusNotFound, browseRefusal)\n\t\treturn\n\t}\n\t// 🔴 THE SESSIONS",
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
)


def go_available() -> bool:
    return shutil.which("go") is not None


def prepare_tree(dest: Path) -> None:
    """Copy the module into an isolated tree.

    🔴 `.git` IS EXCLUDED RATHER THAN COPIED. A copy that carries `.git` shares the
    ORIGINAL's index, refs and reflog when the source is a linked worktree (where `.git`
    is a FILE holding `gitdir: ...`), so a stray command inside the copy lands on the real
    branch. Excluding it also means a mutated tree can never be committed by accident,
    which is the failure mode that matters for a battery that edits source in place.
    """
    shutil.copytree(
        REPO_ROOT,
        dest,
        ignore=shutil.ignore_patterns(".git", "__pycache__", "*.pyc", ".direnv", "result"),
        symlinks=True,
    )
    stray = dest / ".git"
    if stray.exists():  # belt and braces; the ignore above should have handled it
        if stray.is_dir():
            shutil.rmtree(stray)
        else:
            stray.unlink()


def run_tests(tree: Path, pkgs: tuple[str, ...] = ()) -> tuple[bool, set[str], str]:
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
        ["go", "test", "-count=1", "-timeout=2m", "-v", *(pkgs or PKGS)],
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


def apply_mutation(tree: Path, m: Mutant) -> None:
    target = tree / m.path
    text = target.read_text()
    found = text.count(m.old)
    if found != m.occurrences:
        raise MutationError(
            f"{m.name}: pattern occurs {found} time(s) in {m.path}, expected {m.occurrences}. "
            "A pattern that matches nothing scores the mutant SURVIVED without ever running; "
            "a pattern that matches too much mutates code this row does not name. Re-derive it."
        )
    target.write_text(text.replace(m.old, m.new))


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--show", action="store_true", help="print each edit and exit without running")
    ap.add_argument("--only", help="run one mutant by name")
    args = ap.parse_args()

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

    with tempfile.TemporaryDirectory(prefix="cairn-control-mutants-") as tmp:
        base = Path(tmp) / "base"
        prepare_tree(base)

        # 🔴 THE POSITIVE CONTROL, FIRST. Without it a KILLED verdict cannot be told
        # apart from a tree that never compiled.
        print("positive control (the copied tree, UNEDITED) ... ", end="", flush=True)
        green, failing, out = run_tests(base)
        if not green:
            print("RED")
            print(out[-4000:], file=sys.stderr)
            print(
                "\ncontrol_mutants: REFUSING TO VOUCH — the unedited copy is not green, so "
                "every mutant below would score KILLED for a reason that has nothing to do "
                "with its guard.",
                file=sys.stderr,
            )
            return 1
        print("GREEN")

        killed: list[Mutant] = []
        survived: list[Mutant] = []
        misattributed: list[tuple[Mutant, set[str]]] = []
        broken: list[tuple[Mutant, str]] = []
        stale_extras: list[tuple[Mutant, list[str], set[str]]] = []

        for m in selected:
            work = Path(tmp) / f"m-{m.name}"
            shutil.copytree(base, work, symlinks=True)
            try:
                apply_mutation(work, m)
            except MutationError as exc:
                print(f"  {m.name:<46} HARNESS ERROR")
                broken.append((m, str(exc)))
                continue

            green, failing, out = run_tests(work, m.pkgs)
            if green:
                verdict = "SURVIVED"
                survived.append(m)
            elif not failing:
                # Non-zero with no `--- FAIL:` line: the tree did not build. That is a
                # harness problem, not a kill.
                verdict = "DID NOT BUILD"
                broken.append((m, out[-1500:]))
            elif m.killer and m.killer not in failing:
                verdict = f"MISATTRIBUTED ({', '.join(sorted(failing))})"
                misattributed.append((m, failing))
            else:
                # 🔴 THE `extra_killers` LEDGER IS CHECKED HERE, AND ONLY ON A KILL. The
                # row has already been attributed to its named guard; what is left is the
                # claim that the OTHER listed guards saw it too. A missing entry is a
                # finding, not a verdict downgrade: the mutant WAS killed, and what has
                # gone stale is the ledger's description of who noticed.
                absent = [k for k in m.extra_killers if k not in failing]
                verdict = "killed" if not absent else f"killed, STALE EXTRAS ({', '.join(sorted(absent))})"
                if absent:
                    stale_extras.append((m, absent, failing))
                killed.append(m)
            print(f"  {m.name:<46} {verdict}")

    print()
    expected_survivors = {m.name for m in selected if m.equivalent}
    actual_survivors = {m.name for m in survived}
    print(
        f"SUMMARY mutants={len(selected)} killed={len(killed)} survived={len(survived)} "
        f"misattributed={len(misattributed)} harness-errors={len(broken)} "
        f"stale-extras={len(stale_extras)}"
    )

    ok = True
    for m, exc in broken:
        print(f"\n🔴 HARNESS: {m.name}\n{exc}", file=sys.stderr)
        ok = False
    for m, failing in misattributed:
        print(
            f"\n🔴 MISATTRIBUTED: {m.name} was killed by {sorted(failing)}, not by its named "
            f"guard {m.killer}. That proves the suite can fail and proves nothing about the "
            "guard this row exists for.",
            file=sys.stderr,
        )
        ok = False

    # 🔴 A LISTED EXTRA KILLER THAT DID NOT FAIL IS A FAILURE, NOT A NOTE. The whole point
    # of making the field real is that a guard which quietly stopped noticing is exactly
    # what a battery is for; reporting it without failing would be the inert field again,
    # one layer up. The remedy is per row and is a DECISION: either the guard was moved out
    # from under this mutant's observable — fix the guard — or the row's list was aspirational
    # and must be narrowed, with the reason written down beside it.
    for m, absent, failing in stale_extras:
        print(
            f"\n🔴 STALE extra_killers: {m.name} lists {sorted(absent)}, which did NOT fail. "
            f"What actually failed: {sorted(failing)}. A listed killer that has stopped "
            "killing reads as coverage and provides none — decide whether the GUARD moved "
            "or the LIST was wrong, and say which in the row.",
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

    for m in survived:
        print(f"\nEQUIVALENT {m.name}: {m.equivalent_reason}")

    return 0 if ok else 1


if __name__ == "__main__":
    sys.exit(main())
