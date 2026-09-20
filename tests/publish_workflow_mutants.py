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


def _move_go_build_first(text: str) -> str:
    """Relocate the Go image build to in front of the Python one.

    A step's BLOCK is `- name: …` through the next blank line; the comment above
    it is left where it was, which does not matter — the order guard reads step
    names out of the command text with comments already dropped.
    """
    found = re.search(
        r"      - name: build the Go server image from the flake\n(?:.*\n)*?\n", text
    )
    if not found:
        return text
    block = found.group(0)
    python_build = "      - name: build the Python server image from the flake\n"
    return text.replace(block, "", 1).replace(python_build, block + python_build, 1)


# The `/data`-is-empty control, as it appears in BOTH image-control steps: the
# probe, the branch that reads it, and the line that reports the zero. The two
# copies are textually near-identical, so the occurrence is selected by INDEX
# rather than by a Python-vs-Go word — which is also the honest statement of the
# hazard, since deleting EITHER copy was measured to leave the suite green.
_DATA_BLOCK = re.compile(
    r"          leaked=\$\(docker run(?:.*\n)*?"
    r'          echo "control: /data holds \$leaked files \(must be 0\) — OK"\n'
)


def _drop_data_block(text: str, index: int) -> str:
    """Delete the `index`-th `/data` emptiness control (0 = Python, 1 = Go)."""
    matches = list(_DATA_BLOCK.finditer(text))
    if len(matches) <= index:
        return text
    found = matches[index]
    return text[: found.start()] + text[found.end():]


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
            "      - name: push the Python pod's version tag, on a tag push only\n",
            '      - name: push latest\n'
            '        run: |\n'
            '          ${{ steps.skopeo.outputs.bin }} copy '
            '"docker-archive:x" "docker://${{ steps.ref.outputs.image }}:latest"\n'
            "      - name: push the Python pod's version tag, on a tag push only\n",
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
    # 🔴 THE SIX BELOW COVER THE MEASURED DEFECTS, NOT HYPOTHETICAL ONES. Every
    # run of this workflow failed at the skopeo step because `nixpkgs#skopeo` is
    # multi-output; nothing published the Go pod at all; the Python pod's positive
    # control cannot work on the Go image, which is the copy-paste this change was
    # most likely to ship; a hardcoded route name here is a FIFTH route ledger
    # that the four in `AGENTS.md` cannot see; and the Go half in front of the
    # Python publish is the ordering that made the original failure total.
    (
        "build-the-BARE-multi-output-skopeo-attribute",
        lambda t: t.replace(
            '          out=$(nix build --inputs-from . nixpkgs#skopeo.out \\\n'
            '                  --no-link --print-out-paths)\n',
            '          out=$(nix build --inputs-from . nixpkgs#skopeo --no-link --print-out-paths)\n',
            1,
        ),
        "test_the_skopeo_build_names_an_OUTPUT_and_not_the_bare_derivation",
    ),
    (
        # The Go BUILD moved in front of the Python build — the ordering the first
        # draft of this workflow shipped, where a first-execution Go step going red
        # leaves the DEPLOYED pod unpublished.
        "run-the-GO-half-before-the-PYTHON-publish",
        _move_go_build_first,
        "test_the_whole_PYTHON_half_runs_before_the_first_GO_step",
    ),
    (
        "hardcode-route-names-as-a-FIFTH-ledger",
        lambda t: t.replace(
            '          echo "positive control: the server ran, exited 0 and declared $declared ledger line(s) — OK"\n',
            "          for want in 'GET recall' 'POST entry'; do\n"
            '            case "$routes" in\n'
            '              *"$want"*) ;;\n'
            '              *) echo "REFUSING TO PUBLISH: the ledger does not name $want"\n'
            "                 exit 1 ;;\n"
            "            esac\n"
            "          done\n"
            '          echo "positive control: the server ran, exited 0 and declared $declared ledger line(s) — OK"\n',
            1,
        ),
        "test_the_GO_pods_positive_control_is_its_ROUTE_LEDGER_not_the_Pythons",
    ),
    (
        "drop-the-go-pods-push",
        lambda t: re.sub(
            r"      - name: push the Go pod's immutable sha tag\n(?:.*\n)*?\n",
            "",
            t,
            count=1,
        ),
        "test_both_pods_are_published_and_every_push_step_is_pinned_WHOLE",
    ),
    (
        "reword-a-push-step",
        lambda t: t.replace(
            '            "docker-archive:${{ steps.build-go.outputs.archive }}" \\\n'
            '            "docker://${{ steps.ref.outputs.image_go }}:${{ steps.ref.outputs.sha_tag }}"\n',
            '            "docker-archive:${{ steps.build.outputs.archive }}" \\\n'
            '            "docker://${{ steps.ref.outputs.image_go }}:${{ steps.ref.outputs.sha_tag }}"\n',
            1,
        ),
        "test_both_pods_are_published_and_every_push_step_is_pinned_WHOLE",
    ),
    (
        "copy-the-python-positive-control-into-the-go-control",
        lambda t: t.replace(
            "          routes=$(docker run --rm --entrypoint \"$server\" \"$loaded\" -routes)\n",
            "          routes=$(docker run --rm --entrypoint \"$server\" \"$loaded\" -c "
            "'import subsystem_recall')\n",
            1,
        ),
        "test_the_GO_pods_positive_control_is_its_ROUTE_LEDGER_not_the_Pythons",
    ),
    # 🔴 THE FIVE BELOW CLOSE FOUR HOLES A BLIND AUDIT FOUND AND MEASURED: the
    # mutant was applied, the suite stayed 16/16 green, and the workflow would
    # have published anyway. None of them is hypothetical.
    (
        # The Go proof re-aimed at the PYTHON package — the shape a copy-paste
        # produces. The job pushes the Go image, prints `ANONYMOUS PULL OK` for
        # `cairn-store` and exits 0; a ghcr package is PRIVATE on first publish,
        # so the one condition the step exists to surface goes invisible and the
        # operator never learns to do the one-time visibility flip.
        "aim-the-GO-proof-at-the-PYTHON-package",
        lambda t: t.replace(
            "          ref='${{ steps.ref.outputs.image_go }}:"
            "${{ steps.ref.outputs.sha_tag }}'\n",
            "          ref='${{ steps.ref.outputs.image }}:"
            "${{ steps.ref.outputs.sha_tag }}'\n",
            1,
        ),
        "test_each_anonymous_proof_inspects_ITS_OWN_package",
    ),
    (
        # The step this workflow's own prose calls "the control that matters
        # most here", deleted from the PYTHON pod — in a PUBLIC repository
        # publishing to a PUBLIC registry.
        "delete-the-PYTHON-pods-/data-emptiness-control",
        lambda t: _drop_data_block(t, 0),
        "test_both_image_controls_REFUSE_on_a_non_empty_data_directory",
    ),
    (
        # …and from the GO pod. Two sites, two mutants: a guard that covered one
        # of them would read as covering both, which is the defect the split
        # exists to make visible.
        "delete-the-GO-pods-/data-emptiness-control",
        lambda t: _drop_data_block(t, 1),
        "test_both_image_controls_REFUSE_on_a_non_empty_data_directory",
    ),
    (
        # A refusal that prints and returns success. `REFUSING TO PUBLISH` and
        # `EMPTY` both survive this edit, which is why the guard that asserted
        # those two words stayed green while the control published anyway.
        "let-the-empty-ledger-refusal-exit-ZERO",
        lambda t: t.replace(
            '            echo "  would also satisfy."\n            exit 1\n',
            '            echo "  would also satisfy."\n            exit 0\n',
            1,
        ),
        "test_no_control_step_can_REFUSE_and_exit_ZERO",
    ),
    (
        # The whole-body pin's own reachability: a cosmetic reword inside a
        # control step that no narrower guard in the file reads. If this
        # survives, the pin is not pinning.
        "reword-a-control-step",
        lambda t: t.replace(
            '          echo "server (from the image\'s own Cmd) = $server"\n',
            '          echo "server (read from the image\'s own Cmd) = $server"\n',
            1,
        ),
        "test_every_control_step_is_pinned_WHOLE",
    ),
]


def run_suite(wt: Path) -> tuple[set[str], int]:
    """Run the guard suite once; return (failing test names, tests that RAN).

    🔴 A COUNT OF FAILURES IS NOT A COUNT OF RUNS, AND THAT GAP IS THE ONE THIS
    BATTERY CANNOT SURVIVE. The failure set is parsed from `FAILED` lines, so an
    interpreter with no `pytest` emits none — which is byte-indistinguishable
    from a green suite. The baseline then reads `0 failures`, EVERY mutant scores
    SURVIVED because nothing ran to catch it, and the battery prints a confident
    verdict about the SHELL rather than about the workflow.

    MEASURED, both directions, on one tree at 14 mutants: `python3 -m pytest` ->
    `No module named pytest` -> 14 SURVIVED; the same tree under an interpreter
    carrying pytest -> 14 KILLED, 0 problems. Nothing in the output
    distinguished the two except the verdict itself.

    So the second element is this battery's POSITIVE control — a number that must
    move off zero before any verdict here is readable.
    """
    r = subprocess.run(
        [sys.executable, "-m", "pytest", TEST, "-p", "no:randomly", "-q", "--tb=no", "-rf"],
        cwd=wt, capture_output=True, text=True,
    )
    failed = set(re.findall(r"^FAILED .*::(\w+)", r.stdout, re.M))
    # Read the runner's OWN summary counts rather than inferring from the exit
    # code: a wrapper's status is the last command's, and "no tests ran" exits 5.
    ran = sum(int(n) for n in re.findall(r"(\d+) (?:passed|failed)", r.stdout))
    return failed, ran


def failing_tests(wt: Path) -> set[str]:
    return run_suite(wt)[0]


def main() -> int:
    original = WF.read_text()
    backup = WF.with_suffix(".yml.orig")
    shutil.copy2(WF, backup)

    # 🔴 THE BASELINE IS A CONTROL, NOT A COURTESY. Every row below reads "did
    # test X fail?", and a test already failing at HEAD would be scored as a kill
    # for every mutant in the list — a fully green battery over a guard that
    # never ran.
    baseline, ran = run_suite(WT)
    if baseline:
        print(f"REFUSING: the suite is already red at HEAD: {sorted(baseline)}")
        return 1
    # 🔴 THE POSITIVE CONTROL, AND IT EXITS 2 — "could not vouch", never "passed".
    # A zero here means the runner never executed the guards, in which case every
    # SURVIVED below would be a fact about this shell. See `run_suite`.
    if ran == 0:
        print("REFUSING TO VOUCH: the baseline run executed ZERO tests, so a "
              "'0 failures' reading says nothing about the guards. Usually a "
              f"shell without pytest — check `{sys.executable} -m pytest --version`.")
        return 2
    print(f"baseline: 0 failures over {ran} test(s) that RAN — the battery can attribute\n")

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
