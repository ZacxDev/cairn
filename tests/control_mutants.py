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

#: 🔴 THREE PACKAGES, NOT ONE, BECAUSE THE GUARDS THIS BATTERY EXERCISES NOW SPAN A
#: SEAM. `internal/control` is the model and its predicate; `internal/control/tokenfile`
#: is the projection of the token file into that model; `internal/api` is the server that
#: authorises from it. A mutant in the projection is killed by a guard in the server and
#: vice versa — so a battery scoped to one package would score those SURVIVED while the
#: suite that catches them was never run. `./internal/control/...` would cover the first
#: two in one word; it is spelled out so that adding a sub-package is a deliberate act
#: rather than something the pattern absorbs silently.
PKGS = ("./internal/control/", "./internal/control/tokenfile/", "./internal/api/")


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
    extra_killers: tuple[str, ...] = field(default_factory=tuple)


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
        old="\t\tif len(e.TokenHash) != HashHexLen {",
        new="\t\tif false {",
        killer="TestTheJournalRefusesWhatItCannotEnforce",
        why="the guard standing between a caller's mistake and a credential written in "
        "clear text into a durable, operator-readable file.",
    ),
    Mutant(
        name="duplicate-digest-accepted",
        path="internal/control/journal.go",
        old="\t\tfor id, c := range m.Credentials {\n\t\t\tif c.TokenHash == e.TokenHash {",
        new="\t\tfor id, c := range m.Credentials {\n\t\t\tif false {\n\t\t\t\t_ = id\n\t\t\t\t_ = c",
        killer="TestTwoCredentialsCannotShareOneDigest",
        why="one secret bound to two principals, resolved arbitrarily by whichever the "
        "authenticator's iteration reaches last.",
    ),
    Mutant(
        name="failed-replay-returns-a-partial-model",
        path="internal/control/journal.go",
        old='return Model{}, fmt.Errorf("event %d (%s): %w", i+1, e.Kind, err)',
        new='return m, fmt.Errorf("event %d (%s): %w", i+1, e.Kind, err)',
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
        extra_killers=("TestAKilledAuthorityKeepsServingAndTheAgeGrows",),
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
        old="\t\tcase <-tick:\n\t\t\t_ = c.refresh(ctx, RefreshTimer)",
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
        old="_ = c.refresh(ctx, RefreshTimer)",
        new="_ = c.refresh(ctx, RefreshExplicit)",
        killer="TestTheTimerTriggerRefreshes",
        why="a copy-paste in the trigger label. `LastTrigger` is the ONLY thing that "
        "distinguishes three mechanisms producing one observable, so a wrong label makes "
        "the report unable to answer 'did my SIGHUP do anything'.",
    ),
    Mutant(
        name="sighup-trigger-dropped",
        path="internal/control/cache.go",
        old="\t\tcase <-tr.Signals:\n\t\t\t_ = c.refresh(ctx, RefreshSignal)",
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
        old="\t\tcase <-tr.OnChange:\n\t\t\t_ = c.refresh(ctx, RefreshChange)",
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
        old="\tif materialize {\n\t\tc.model = next",
        new="\tif false {\n\t\tc.model = next",
        killer="TestASynchronousRevokeIsInForceBeforeItReturns",
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
        old="\tfor _, dir := range s.storeDirs() {",
        new="\tfor _, dir := range []string(nil) {",
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
        old="\t\tif !seen[identity] {",
        new="\t\tif true {",
        killer="TestTwoBareRowsAreOnePrincipalWithTwoCredentials",
        why="one project per ROW instead of per identity. `authz.LoadTokens` exempts "
        "`legacy` from its duplicate-identity guard precisely so two bare rows can "
        "coexist during a rotation, so this breaks on the one file shape the "
        "rotation procedure prescribes — and it breaks by taking the whole authority "
        "down, not by narrowing it.",
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
        old="\tsort.Strings(out)\n\treturn out",
        # Sorting a zero-length slice rather than deleting the call: deleting it
        # leaves the `sort` import unused and the tree does not build, which proves
        # nothing about the ordering guard.
        new="\tsort.Strings(out[:0])\n\treturn out",
        killer="TestTheProjectionIsAPureFunctionOfItsInputs",
        why="map range order reaching the journal. Nothing about the AUTHORITY changes "
        "— the same ids, the same grants, the same epoch — so a comparison of those "
        "would score it EQUIVALENT. What moves is the event ORDER, which is why the "
        "guard compares the serialized journal instead.",
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
        old="\tif !rq.writable.Allows(scope) {",
        new="\tif !rq.visible.Allows(scope) {",
        killer="TestTheWritePathNarrowsWithTheWriteVERB",
        why="the same swap on the CREATE half, which consults the set directly rather "
        "than through the loader. Two sites, two mutants: a fix applied to one of "
        "them is the one-rule-two-places failure this repository keeps paying for.",
    ),
    Mutant(
        name="read-set-uses-the-write-verb",
        path="internal/api/server.go",
        old="\trq.visible = auth.VisibleScopes(control.VerbRead)",
        new="\trq.visible = auth.VisibleScopes(control.VerbWrite)",
        killer="TestTheServedAuthorizationMatrixIsExactlyThis",
        why="one word in the line that builds the READ set. A bare row holds the write "
        "verb nowhere, so it would read nothing at all — a total read outage for the "
        "unrestricted credential, produced by a verb name.",
    ),
    Mutant(
        name="the-audit-identity-becomes-an-opaque-id",
        path="internal/api/server.go",
        old="\trq.identity = principal.Display",
        new="\trq.identity = string(principal.ID)",
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
        old="\tprincipal, auth, err := rq.srv.authority.Authenticate(presented)",
        new="\t_ = rq.srv.authority.Refresh(rq.r.Context())\n\tprincipal, auth, err := rq.srv.authority.Authenticate(presented)",
        killer="TestTheHotPathDoesNotContactTheAuthority",
        why="a refresh on every request — the 'just make it fresh' fix. It reads as an "
        "improvement and it deletes the property the whole cache exists for: an "
        "outage of the authority would stop every read, which is the promise cairn "
        "makes about an offline orient-me.",
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


def run_tests(tree: Path) -> tuple[bool, set[str], str]:
    """Run the package's tests, returning (green, failing test names, raw output).

    🔴 THE FAILING TEST NAMES COME FROM `--- FAIL:` LINES, NOT FROM THE EXIT CODE. An
    exit code says something went wrong; it cannot say WHICH guard noticed, and a mutant
    killed by the wrong guard is the green-for-the-wrong-reason shape this module exists
    to refuse. A build failure produces a non-zero exit with NO `--- FAIL:` lines at all,
    which is reported as its own outcome rather than silently scored as a kill.
    """
    proc = subprocess.run(
        ["go", "test", "-count=1", "-v", *PKGS],
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

        for m in selected:
            work = Path(tmp) / f"m-{m.name}"
            shutil.copytree(base, work, symlinks=True)
            try:
                apply_mutation(work, m)
            except MutationError as exc:
                print(f"  {m.name:<46} HARNESS ERROR")
                broken.append((m, str(exc)))
                continue

            green, failing, out = run_tests(work)
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
                verdict = "killed"
                killed.append(m)
            print(f"  {m.name:<46} {verdict}")

    print()
    expected_survivors = {m.name for m in selected if m.equivalent}
    actual_survivors = {m.name for m in survived}
    print(
        f"SUMMARY mutants={len(selected)} killed={len(killed)} survived={len(survived)} "
        f"misattributed={len(misattributed)} harness-errors={len(broken)}"
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
