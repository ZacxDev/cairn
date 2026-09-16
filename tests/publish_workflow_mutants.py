#!/usr/bin/env python3
"""Break `.github/workflows/publish-image.yml` one way at a time; require the
test that NAMES that property to be the one that goes red.

🔴 WHY A BATTERY AND NOT JUST THE TESTS. `tests/test_publish_workflow.py` reads a
YAML file with regular expressions, and a regular expression over prose is the
easiest kind of guard to write so that it can never fail: the file it scans
CONTAINS, in its own explanatory comments, every string the guards look for. That
is not hypothetical. The `fetch-depth` guard was first written as a search for
`fetch-depth:\\s*0` over the whole file, and the step's comment names that exact
setting while explaining it — so flipping the real value to `1` left the guard
matching the SENTENCE ABOUT the setting, and this battery is the only thing that
saw it: the mutant scored SURVIVED against a fully green suite and a clean
reading of the test. Rewriting it to read the setting rather than the word is
what made it a guard.

🔴 A MUTANT KILLED BY SOME OTHER TEST IS NOT A KILL. It proves the suite can fail
and proves nothing about the guard the row exists for, so it is reported
MISATTRIBUTED. Each row therefore names the single test that must be among the
failures, and the row's own mutation is the narrowest edit that can make that
property false.

Run it directly; it needs nothing but `pytest`:

    python3 tests/publish_workflow_mutants.py
"""
import re
import shutil
import subprocess
import sys
from pathlib import Path

WT = Path(__file__).resolve().parents[1]
WF = WT / ".github" / "workflows" / "publish-image.yml"
TEST = "tests/test_publish_workflow.py"

MUTANTS = [
    (
        "add-pull_request-trigger",
        lambda t: t.replace("  workflow_dispatch:\n", "  pull_request:\n  workflow_dispatch:\n", 1),
        "test_no_pull_request_event_can_reach_this_workflow",
    ),
    (
        "downgrade-packages-permission",
        lambda t: t.replace("  packages: write\n", "  packages: read\n", 1),
        "test_the_workflow_asks_for_exactly_the_permissions_it_needs",
    ),
    (
        "push-a-mutable-latest-tag",
        lambda t: t.replace(
            '      - name: push the version tag, on a tag push only\n',
            '      - name: push latest\n'
            '        run: |\n'
            '          ${{ steps.skopeo.outputs.bin }} copy '
            '"docker-archive:x" "docker://${{ steps.ref.outputs.image }}:latest"\n'
            '      - name: push the version tag, on a tag push only\n',
            1,
        ),
        "test_every_published_tag_is_one_of_the_two_immutable_ones",
    ),
    (
        "drop-no-creds-from-the-real-inspect",
        lambda t: t.replace(
            'out=$($skopeo inspect --no-creds "docker://$ref" 2>&1)',
            'out=$($skopeo inspect "docker://$ref" 2>&1)',
            1,
        ),
        "test_every_inspect_is_anonymous",
    ),
    (
        "make-the-checkout-shallow",
        lambda t: t.replace("          fetch-depth: 0\n", "          fetch-depth: 1\n", 1),
        "test_the_checkout_is_not_shallow",
    ),
    (
        "add-a-second-docker-build-path",
        lambda t: t.replace(
            "      - name: pin skopeo to the flake's nixpkgs\n",
            "      - run: docker build -f server/Dockerfile -t x .\n"
            "      - name: pin skopeo to the flake's nixpkgs\n",
            1,
        ),
        "test_the_publish_builds_the_FLAKE_image_and_not_a_third_one",
    ),
    (
        "shorten-the-immutable-tag",
        lambda t: t.replace(
            'sha_tag="sha-${{ github.sha }}"',
            'sha_tag="sha-$(git rev-parse --short HEAD)"',
            1,
        ),
        "test_the_immutable_tag_is_the_full_commit_sha",
    ),
    (
        "delete-the-anonymous-checks-negative-control",
        lambda t: re.sub(
            r"^.*sha-0000000000000000000000000000000000000000.*\n", "", t, count=1, flags=re.M
        ),
        "test_the_anonymous_check_carries_its_own_negative_control",
    ),
]


def failing_tests(wt: Path) -> set[str]:
    r = subprocess.run(
        [sys.executable, "-m", "pytest", TEST, "-p", "no:randomly", "-q", "--tb=no", "-rf"],
        cwd=wt, capture_output=True, text=True,
    )
    return set(re.findall(r"^FAILED .*::(\w+)", r.stdout, re.M))


def main() -> int:
    original = WF.read_text()
    backup = WF.with_suffix(".yml.orig")
    shutil.copy2(WF, backup)

    # 🔴 THE BASELINE IS A CONTROL, NOT A COURTESY. Every row below reads "did
    # test X fail?", and a test already failing at HEAD would be scored as a kill
    # for every mutant in the list — a fully green battery over a guard that
    # never ran.
    baseline = failing_tests(WT)
    if baseline:
        print(f"REFUSING: the suite is already red at HEAD: {sorted(baseline)}")
        return 1
    print("baseline: 0 failures — the battery can attribute\n")

    bad = 0
    try:
        for name, mutate, expected in MUTANTS:
            mutated = mutate(original)
            if mutated == original:
                print(f"{name:45s} NO-OP — the mutation matched nothing")
                bad += 1
                continue
            WF.write_text(mutated)
            failed = failing_tests(WT)
            if not failed:
                print(f"{name:45s} SURVIVED")
                bad += 1
            elif expected not in failed:
                print(f"{name:45s} MISATTRIBUTED -> {sorted(failed)}")
                bad += 1
            else:
                extra = sorted(failed - {expected})
                note = f"  (also: {extra})" if extra else ""
                print(f"{name:45s} KILLED by {expected}{note}")
    finally:
        WF.write_text(original)
        backup.unlink()

    print(f"\n{len(MUTANTS)} mutants, {bad} problem(s)")
    return 1 if bad else 0


if __name__ == "__main__":
    sys.exit(main())
