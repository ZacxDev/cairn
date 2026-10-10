"""The ledger over the POSTGRES TIER, which the ordinary suite cannot otherwise see.

🔴 WHY THIS FILE EXISTS AT ALL. `internal/pgstore`'s tests are behind `//go:build pgtest`,
which is deliberate — the nix sandbox has no database and must not pretend to have
measured SQL. But a build tag MOVES the hazard rather than closing it: without the tag the
tier's files are invisible to `go test ./...`, to `go vet`, and to every `nix` check, so
deleting them, emptying them, or quietly adding a `t.Skip` are all changes that leave
every gate in this repository green. That is the same shape as
`tests/test_go_client_ledgers.py` (blind to a compiled binary) and
`internal/api.DeclaredRoutes` (blind to a dispatch table nothing reads) — a ledger that
reads the ARTEFACT is the only instrument that can see it.

🔴 IT FAILS ON GROW *OR* SHRINK, which is `depspolicy`'s rule and for its reason. A ledger
that only checked "every name I know is present" is satisfied by a tier that gained an
untested surface; one that only checked "nothing new" is satisfied by a tier that lost a
guard. Both directions are the point, so both are asserted, and a new test in the tier is
a deliberate line here.

⚠ WHAT THIS FILE CANNOT DO, said plainly so its green is not read as wider than it is: it
reads TEXT. It cannot tell whether any of these tests passes, whether they run against a
real server, or whether an assertion inside one still asserts anything. That is
`tests/pgtest/run.sh`'s job, and the CI step asserted below is what makes somebody run it.
"""

from __future__ import annotations

import re
import stat
from pathlib import Path

import pytest

REPO = Path(__file__).resolve().parent.parent

TIER_FILES = (
    "internal/pgstore/harness_pgtest_test.go",
    "internal/pgstore/invites_pgtest_test.go",
    # 🔴 THE SESSION HALF, ADDED BY ROUND 0 OF #139's AUDIT RATHER THAN BY THE CHANGE THAT
    # MADE IT LOAD-BEARING. 28(c) put `SessionStore` on the authentication path of a
    # deployed surface while the whole tier mentioned it exactly ONCE, in a comment — so
    # `Lookup`/`Create`/`Revoke`/`Prune` had never been executed against a real server by
    # any gate here. The compile-time `var _ identity.SessionStore` assertion says the
    # methods EXIST; it says nothing about what the SQL does.
    "internal/pgstore/sessions_pgtest_test.go",
    # The TEAM LINK tables (migration 2) and the up-path from version 1. The export file
    # carries no test: it is the tagged, package-internal opener the up-path needs, and it is
    # declared so that the tag on it is asserted like every other tier file's.
    "internal/pgstore/teamlinks_pgtest_test.go",
    "internal/pgstore/export_pgtest_test.go",
    # 🔴 THE TIER IS TWO PACKAGES NOW, AND THE SECOND ONE IS THE PROGRAM RATHER THAN THE
    # SQL. `cmd/cairn-ui`'s DSN branch — schema applied, session table moved off disk, an
    # `Inviting` that is not nil — is invisible without a server, exactly like the SQL, so
    # it lives behind the same tag and is declared here for the same reason. ⚠ A tagged
    # file in a package `run.sh` does not NAME is never compiled, so this ledger and
    # `PGTEST_PKGS` in the runner are two halves of one move; `test_the_runner_names_every_
    # package_the_tier_lives_in` is what refuses to let them drift.
    "cmd/cairn-ui/database_pgtest_test.go",
)
RUNNER = "tests/pgtest/run.sh"
CI = ".github/workflows/ci.yml"

BUILD_TAG = "//go:build pgtest"

# 🔴 THE LEDGER. Every test the tier declares, spelled once. Adding a test to the tier
# means adding a line here; that is the cost, and it buys the only instrument in this
# repository that can notice a Postgres test disappearing.
DECLARED_TESTS = {
    "internal/pgstore/harness_pgtest_test.go": {
        "TestTheServerIsTheMajorThisTierPinsIsAboutTheSERVERNotTheImageTag",
    },
    "internal/pgstore/invites_pgtest_test.go": {
        "TestTheSQLRedemptionGuardAgreesWithStateAt",
        "TestTheRedemptionGuardAgreesWithStateAtOnTheOtherTwoClauses",
        "TestAnExpiryIsRoundedToPostgresResolution",
        "TestTwoSimultaneousRedemptionsProduceExactlyOneWinner",
        "TestTheTableNeverHoldsTheToken",
        "TestCreateRefusesAnInvitationNobodyDecidedToGive",
    },
    "internal/pgstore/sessions_pgtest_test.go": {
        "TestASessionRoundTripsThroughPostgres",
        "TestTheSessionTableNeverHoldsThePresentedID",
        "TestTheLivenessBoundaryAgreesWithSessionLive",
        "TestRevokeRemovesTheSessionAndAnUnknownIDIsNotAnError",
        "TestPruneDeletesStrictlyLessThanLiveRejects",
        "TestReCreatingASessionDigestReplacesRatherThanDuplicates",
    },
    "internal/pgstore/teamlinks_pgtest_test.go": {
        "TestTheTeamLinkRedemptionGuardAgreesWithStateAt",
        "TestSimultaneousRedemptionsRespectTheReuseFlag",
        "TestTheSingleUseConstraintIsTheDatabasesOwn",
        "TestATeamLinkRoundTripsWithItsTargetsAndNeverHoldsTheToken",
        "TestMigrationTwoUpgradesAVersionOneDatabase",
    },
    "internal/pgstore/export_pgtest_test.go": set(),
    "cmd/cairn-ui/database_pgtest_test.go": {
        "TestWithADatabaseTheSurfaceMovesItsStateThereAndHoldsInvitations",
    },
}

# 🔴 EVERY PACKAGE A TIER FILE LIVES IN, DERIVED FROM `TIER_FILES` RATHER THAN RETYPED.
# A second hand-maintained list would be a second thing to forget, and the failure it
# would hide is silent: a tagged file in a package the runner does not name compiles
# never and runs never, while every assertion above about that FILE stays green.
TIER_PKGS = {"./" + rel.rsplit("/", 1)[0] + "/" for rel in TIER_FILES}

# The Postgres major this tier is a claim about. Three files choose it and they must
# agree; a fourth — the deployment repo's StatefulSet — cannot be read from here, which
# is stated rather than silently omitted.
PINNED_MAJOR = 18

FUNC_TEST = re.compile(r"^func (Test[A-Za-z0-9_]*)\(", re.MULTILINE)


def read(rel: str) -> str:
    path = REPO / rel
    if not path.exists():
        pytest.fail(
            f"{rel} is MISSING. The Postgres tier is the only coverage the SQL in "
            f"`internal/pgstore` has, and it is invisible to `go test ./...` because it "
            f"is build-tagged — so deleting it is green everywhere except here."
        )
    return path.read_text(encoding="utf-8")


def test_every_tier_file_exists_and_carries_the_build_tag():
    """The tag is what keeps the nix sandbox honest; without it the tier would run there
    and fail for want of a database, and the fix somebody would reach for is a skip."""
    for rel in TIER_FILES:
        text = read(rel)
        first = text.splitlines()[0].strip() if text.strip() else ""
        assert first == BUILD_TAG, (
            f"{rel} does not start with `{BUILD_TAG}`; its first line is {first!r}. "
            f"Without the tag this file runs in `go test ./...`, including inside the "
            f"nix sandbox, which has no database."
        )


def test_the_declared_test_set_is_exactly_what_the_tier_contains():
    """Fails on GROW or SHRINK. A test removed from the tier is the failure this ledger
    exists for; a test added without a line here is how the ledger stops being a ledger."""
    for rel, declared in DECLARED_TESTS.items():
        found = set(FUNC_TEST.findall(read(rel)))
        missing = declared - found
        extra = found - declared
        assert not missing, (
            f"{rel} no longer declares {sorted(missing)}. If a guard was deliberately "
            f"removed or renamed, change this ledger in the SAME commit and say why."
        )
        assert not extra, (
            f"{rel} declares {sorted(extra)}, which this ledger does not know. Add them "
            f"here — the ledger is what makes a later deletion visible."
        )


def test_the_tier_never_skips():
    """🔴 THE TIER'S WHOLE DESIGN IS THAT IT REFUSES RATHER THAN SKIPPING. A skip nobody
    counts is a pass, and this is the assertion that keeps the next person from reaching
    for the easy fix when they find the tier red without a database."""
    for rel in TIER_FILES:
        text = read(rel)
        for bad in ("t.Skip(", "t.Skipf(", "t.SkipNow("):
            assert bad not in text, (
                f"{rel} calls {bad}. This tier must refuse — see the header of "
                f"internal/pgstore/harness_pgtest_test.go. A skipped Postgres test is "
                f"indistinguishable from a passing one in any summary anybody reads."
            )
    # And the refusal itself must still be a Fatal, not something softer.
    harness = read("internal/pgstore/harness_pgtest_test.go")
    assert "t.Fatal(refusal)" in harness, (
        "the harness no longer calls `t.Fatal(refusal)` when no DSN is configured. "
        "Refusing is the one behaviour that distinguishes this tier from a skipping one."
    )


def test_the_runner_exists_is_executable_and_refuses_on_a_zero():
    """The runner is the half that makes the tier RUN. Its own refusals are asserted here
    because a runner that reported success over zero tests would make every green in this
    tier a fact about the selection rather than about the SQL."""
    path = REPO / RUNNER
    assert path.exists(), f"{RUNNER} is MISSING — nothing runs the Postgres tier."
    mode = path.stat().st_mode
    assert mode & stat.S_IXUSR, f"{RUNNER} is not executable ({stat.filemode(mode)})."

    text = path.read_text(encoding="utf-8")
    # A zero-tests run must be a refusal, not a pass.
    assert 'if [ "$ran" -eq 0 ]' in text, (
        f"{RUNNER} no longer refuses when zero tests ran. `go test` answers `ok` for a "
        f"`-run` that matches nothing, so without this its status is a claim about the "
        f"filter and not about the SQL."
    )
    # A skip must be a failure of the tier, not a shrug.
    assert 'if [ "$skipped" -gt 0 ]' in text, (
        f"{RUNNER} no longer refuses on a SKIP line."
    )
    # And the negative control must still run BEFORE the run under test.
    assert "negative control" in text and "REFUSING TO VOUCH" in text, (
        f"{RUNNER} no longer runs its no-database negative control. Without it a green "
        f"tier is indistinguishable from a tier wired to nothing."
    )
    # 🔴 AND THE CONTROL MUST COVER EVERY PACKAGE, NOT JUST THE FIRST. Each package
    # restates the refusal — `cmd/cairn-ui` cannot import `internal/pgstore`'s `_test`
    # helpers — and an unwatched second spelling is the thing a negative control exists
    # against. The check is that each package path appears inside the control loop.
    #
    # ⚠ THE ANCHORS ARE THE SHELL'S OWN `echo` LINES, NOT THE SECTION COMMENTS. A first
    # draft split on the phrase "negative control", whose FIRST occurrence is the section
    # banner — so the slice it produced was the eight characters between that banner and
    # the words "the run under test" inside the very same line, and the assertion failed
    # against `', BEFORE '`. It went red rather than green, which is the safe direction,
    # and it is recorded because the same sentence appearing in prose and in code is how a
    # text-slicing check silently measures the wrong region.
    start = text.index('echo "-- negative control')
    end = text.index('echo "-- the tier --"')
    assert start < end, (
        f"{RUNNER} prints its tier banner before its negative-control banner, so the "
        f"control no longer runs FIRST — and a control that runs after the thing it "
        f"vouches for has vouched for nothing."
    )
    control_block = text[start:end]
    for pkg in sorted(TIER_PKGS):
        assert pkg in control_block, (
            f"{RUNNER}'s negative control never runs {pkg}, so that package's own "
            f"REFUSING-TO-VOUCH spelling has never been watched to work. A package whose "
            f"refusal silently became a skip — or whose tagged file stopped compiling — "
            f"would pass the control and then report a green that measured nothing."
        )
    # The status must come off `go test`, not off the pipe it is teed through.
    assert "PIPESTATUS[0]" in text, (
        f"{RUNNER} reads a pipeline's exit status rather than `go test`'s. A pipe eats "
        f"the status — this repository has already filed a false defect that way."
    )


def test_the_runner_names_every_package_the_tier_lives_in():
    """🔴 A TAGGED FILE IN A PACKAGE THE RUNNER DOES NOT NAME IS NEVER COMPILED, AND EVERY
    OTHER ASSERTION IN THIS FILE STAYS GREEN.

    The ledger above reads FILES; `go test` takes PACKAGES. The two agreed for as long as
    the tier was one package, which is exactly how long a coupling stays invisible. This
    is the assertion that makes adding a tier file to a new package a two-part move with
    a gate on the second part — the same shape as `api.DeclaredRoutes()` against the
    conformance corpus, one tier along.
    🔴 IT READS THE `PGTEST_PKGS` ARRAY AND NOT THE WHOLE FILE, AND THE DIFFERENCE IS
    MEASURED RATHER THAN STYLISTIC. The first draft asserted `pkg in text`; the mutant
    that deletes `./cmd/cairn-ui/` from `PGTEST_PKGS` SURVIVED it, because the same path
    is spelled again in the negative-control loop twenty lines below. A whole-file
    substring check is satisfied by any mention — a comment included — so it measured
    that somebody had typed the package name somewhere, while the array `go test` is
    actually handed had lost it. That is this repository's "a guard can be SPELLED rather
    than STRUCTURAL" rule, committed inside the guard written to close a structural hole,
    and found by mutating the thing on purpose rather than by reading it again.
    """
    text = read(RUNNER)
    declaration = re.search(r"^PGTEST_PKGS=\((?P<pkgs>[^)]*)\)", text, re.MULTILINE)
    assert declaration, (
        f"{RUNNER} has no `PGTEST_PKGS=(...)` array. That array is what `go test` is "
        f"handed; without it this assertion has nothing to read and the tier's package "
        f"set is whatever a command line happens to say."
    )
    named = set(declaration.group("pkgs").split())
    assert named == TIER_PKGS, (
        f"{RUNNER}'s PGTEST_PKGS is {sorted(named)}, and the packages holding "
        f"build-tagged tier files are {sorted(TIER_PKGS)}. `go test` compiles only the "
        f"packages it is given, so a tier file in a package MISSING from that array has "
        f"never run — and nothing else in this file can tell, because the ledger above "
        f"reads the FILE rather than the run. A package listed with no tier file is the "
        f"other direction: a path `go test` walks for nothing."
    )


def test_ci_actually_invokes_the_runner():
    """🔴 A TIER NOBODY RUNS IS AS GREEN AS A TIER THAT SKIPS, and `flake.nix` cannot run
    this one — the tag keeps it out of `doCheck`, and a sandboxed server is a separate
    change. So CI is the only thing that runs it, and this is the assertion that notices
    the step being dropped. It is the `checks.ui-stylesheet-is-current` lesson: that guard
    was added to `flake.nix` and left out of `ci.yml`, and would have shipped inert."""
    ci = read(CI)
    assert RUNNER in ci, (
        f"{CI} does not mention {RUNNER}, so nothing runs the Postgres tier and its "
        f"absence is invisible. `cairn#117` added a check to flake.nix and left CI "
        f"untouched; it would have shipped inert for exactly this reason."
    )


def test_the_pinned_postgres_major_agrees_across_every_site_that_can_be_read():
    """🔴 PINNED, NOT INHERITED — and the pin is spelled in three readable places plus one
    that is not. A tier measured against an unknown major is a claim about an unknown
    server, which is the `pkgs.python3`-followed-nixpkgs-to-3.14 failure in a new costume.

    ⚠ THE FOURTH SITE IS THE DEPLOYMENT REPO'S StatefulSet AND NOTHING HERE CAN READ IT.
    Said rather than omitted: this test proves the three sites in THIS repository agree,
    and nothing more."""
    harness = read("internal/pgstore/harness_pgtest_test.go")
    assert f"wantServerMajor = {PINNED_MAJOR}" in harness, (
        f"the tier's own pin is not {PINNED_MAJOR}. Move all three sites together."
    )

    flake = read("flake.nix")
    assert f"pkgs.postgresql_{PINNED_MAJOR}" in flake, (
        f"flake.nix's devShell does not carry `pkgs.postgresql_{PINNED_MAJOR}`, so a "
        f"developer running the tier gets whatever major is on PATH — or none."
    )

    ci = read(CI)
    assert f"postgres:{PINNED_MAJOR}" in ci, (
        f"{CI} does not pin a `postgres:{PINNED_MAJOR}` image, so CI measures the tier "
        f"against a major nothing else here runs."
    )


def test_this_ledger_can_go_red():
    """The instrument's own control. A ledger asserting text is worth exactly as much as
    the proof that its assertions can fail, and every other test in this file would stay
    green if `read()` silently returned an empty string."""
    with pytest.raises(BaseException):
        read("internal/pgstore/a_file_that_does_not_exist_pgtest_test.go")

    # And the set comparison must actually compare: a ledger entry that is not in the
    # file has to be detected, which is the direction that matters.
    found = set(FUNC_TEST.findall(read("internal/pgstore/invites_pgtest_test.go")))
    assert "TestAGuardNobodyWrote" not in found, "internal: the control's own premise is wrong"
    assert found, "the regex matched NO test functions — it is not reading Go source"
