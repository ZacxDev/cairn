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
    # ---- the program: the only thing that bounds the declared divergence ----------
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
        "`CAIRN_SUPABASE_MAX_AGE=-1h` — a sign typo, or a value templated from a "
        "subtraction — gets a bound they configured and nothing enforcing it, which is "
        "the shape `envBool` refuses one file over for the same reason.",
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
        old="\tif anySet(env, proxyEnv) {",
        new="\tif false {",
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
        # left `envBool` with a path that returns nothing: the tree did not build, and a
        # mutant that dies at the build is the one outcome that proves nothing.
        old='\tdefault:\n\t\treturn false, fmt.Errorf(\n\t\t\t"%s: %q is not a boolean. Write yes or no — an unrecognised value is refused rather than read as `no`, because a typo that silently disables a security setting leaves the operator believing it is on",\n\t\t\tname, raw)',
        new='\tdefault:\n\t\t_ = fmt.Sprintf("%s %s", name, raw)\n\t\treturn false, nil',
        killer="TestAnUnrecognisedBooleanIsAnErrorRatherThanFalse",
        why="`CAIRN_TRUSTED_HEADER_PROXY_FRONTED=treu` read as 'not proxy-fronted'. The "
        "operator who typed it believes the backend is armed, and a setting that reads a "
        "typo as its own default is how a deployment ends up in a state nobody chose. "
        "The edit replaces the `default:` arm with a case nothing matches, which is the "
        "shape that COMPILES — deleting the arm leaves `fmt` unused in a function that "
        "must still return, and a mutant that dies at the build proves nothing.",
    ),
    Mutant(
        name="a-retired-setting-is-silently-ignored",
        path="internal/identity/config.go",
        old='\t\tif value, present := env[name]; present && value != "" {\n\t\t\tnames = append(names, name)\n'
        '\t\t}\n\t}\n\tif len(names) == 0 {',
        new='\t\tif value, present := env[name]; present && value != "" {\n\t\t\tnames = append(names, name)\n'
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
        "to this row.",
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
        # 🔴 `-timeout=2m`, NOT THE 10m DEFAULT, BECAUSE A MUTANT CAN DEADLOCK RATHER
        # THAN FAIL. `TestAWriteDoesNotCommitOverAnAttemptThatSTARTEDAfterIt` re-enters
        # `Cache.refresh` from inside `CacheOptions.Now`, which is only safe because
        # `clock()` is read OUTSIDE `c.mu` at all three of its call sites. A mutation
        # that moves any one of them below `c.mu.Lock()` deadlocks on a non-reentrant
        # `sync.Mutex` — the mutant is still KILLED, but at the default it costs ten
        # minutes of wall clock per occurrence instead of two, in a battery this job
        # runs on every push. The shortest real package here finishes in seconds.
        ["go", "test", "-count=1", "-timeout=2m", "-v", *PKGS],
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
