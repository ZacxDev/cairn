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
PKG = "./internal/control/"


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
        ["go", "test", "-count=1", "-v", PKG],
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
