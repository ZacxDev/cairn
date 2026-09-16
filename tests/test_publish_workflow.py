"""The publish workflow's safety properties, pinned so they cannot move alone.

🔴 WHY THIS FILE EXISTS. `.github/workflows/publish-image.yml` pushes an image to
a PUBLIC registry using a token with `packages: write`. Three of its properties
are load-bearing and none of them is visible in a green run:

  * a pull request — including one from a fork — must never reach it;
  * the tags it publishes must be IMMUTABLE, so "both clusters pull the same tag"
    is a checkable sentence rather than a hopeful one;
  * the check that proves the image is anonymously pullable must actually be
    anonymous. The job is logged in to ghcr two steps earlier, so an `inspect`
    without `--no-creds` passes on the job's OWN credential and certifies
    nothing — a green that is indistinguishable from the failure it exists to
    catch.

A workflow file is configuration, so nothing in the suite would otherwise read
it, and a mistake in any of the three is silent until it is expensive: the first
surfaces as a fork publishing an image, the second as a pod restarting on
somebody else's code, the third as `ImagePullBackOff` in a cluster that is not
this one.

🔴 THIS FILE PARSES YAML BY HAND, WHICH MAKES THE FORMAT A DEPENDENCY IT DID NOT
OTHERWISE HAVE, AND A PARSER THAT MATCHES NOTHING REPORTS PERFECT AGREEMENT — an
empty set satisfies "contains no forbidden member" exactly as well as a correct
file does. Every extractor below is therefore paired with a positive control
asserting it found something, and `test_the_controls_can_fail` drives the SAME
extractors over text that must NOT satisfy them. (It is hand-parsed rather than
`yaml.safe_load`ed because the `tests` CI job installs `pytest` and nothing else;
a dependency added for one file would either be a new install line for everyone
or an `importorskip` — and a skip nobody counts is indistinguishable from a pass,
which is this repository's whole objection to unread skips.)

The precedent is `tests/test_flake_image_matches_dockerfile.py`: a fact stated in
a file no test would otherwise read, pinned so it goes red when it moves.
"""

from __future__ import annotations

import re
from pathlib import Path

import pytest

REPO_ROOT = Path(__file__).resolve().parents[1]
WORKFLOW = REPO_ROOT / ".github" / "workflows" / "publish-image.yml"

# The image's PACKAGE name is pinned; the OWNER is not, because it is derived
# from `github.repository_owner` so a fork publishes into its own namespace
# rather than failing against one it cannot write.
PACKAGE = "cairn-store"

# Every push destination must name this one expression, so the assertion about
# what it RESOLVES to covers all of them rather than one.
IMAGE_EXPRESSION = "${{ steps.ref.outputs.image }}"

# 🔴 AN EQUALITY, NOT AN EXCLUSION LIST. The hazard is "an event a pull request
# can raise reaches a job holding `packages: write`", and that class has several
# members — `pull_request`, `pull_request_target`, and `workflow_run` chained off
# a workflow that itself runs on pull requests. A guard that named the members
# would be a guard on WORDS, walkable by picking the member nobody listed. This
# pins the STATE: exactly these two triggers, and adding any third is red
# whatever it is called.
ALLOWED_TRIGGERS = {"push", "workflow_dispatch"}

# The destinations this workflow is allowed to push to, as TAG EXPRESSIONS. A
# ledger rather than a ban on the string `latest`: a mutable tag introduced under
# any other name adds a third member and fails here.
ALLOWED_PUSH_TAG_EXPRESSIONS = {
    "${{ steps.ref.outputs.sha_tag }}",
    "${{ steps.ref.outputs.version_tag }}",
}


# ---------------------------------------------------------------------------
# Extractors. Pure functions of text, so the controls below drive the SAME code
# over hostile input — a control that re-implements the parser it is validating
# certifies nothing about the parser that ships.
# ---------------------------------------------------------------------------

def workflow_triggers(text: str) -> set[str]:
    """The top-level keys of the `on:` block.

    The block runs from a bare `on:` at column 0 to the next line at column 0.
    """
    lines = text.splitlines()
    try:
        start = next(i for i, line in enumerate(lines) if line.rstrip() == "on:")
    except StopIteration:
        return set()
    out: set[str] = set()
    for line in lines[start + 1:]:
        if line.strip() and not line.startswith((" ", "\t", "#")):
            break
        m = re.match(r"^  (\w+):", line)
        if m:
            out.add(m.group(1))
    return out


def workflow_permissions(text: str) -> dict[str, str]:
    """The top-level `permissions:` block as a dict."""
    lines = text.splitlines()
    try:
        start = next(
            i for i, line in enumerate(lines) if line.rstrip() == "permissions:"
        )
    except StopIteration:
        return {}
    out: dict[str, str] = {}
    for line in lines[start + 1:]:
        if line.strip() and not line.startswith((" ", "\t", "#")):
            break
        m = re.match(r"^  ([\w-]+):\s*(\S+)", line)
        if m:
            out[m.group(1)] = m.group(2)
    return out


def commands(text: str) -> list[str]:
    """The file's lines as SHELL sees them: comments dropped, continuations joined.

    🔴 BOTH HALVES WERE FOUND BY THIS FILE'S OWN CONTROLS, WHICH IS THE ONLY
    REASON THEY ARE HERE. The first version of `test_the_publish_builds_…`
    searched for `^\\s*docker build`, and a workflow step is written
    `- run: docker build …` — so the anchored form matched nothing and the guard
    was walkable by the single most likely spelling of the thing it bans. The
    first version of `push_destinations` returned every `docker://` in the file,
    which swept up the destinations the ANONYMOUS-PULL CHECK inspects and then
    judged them as if they were pushes. Dropping comments matters for the same
    reason in the other direction: this file's own prose names `docker build` as
    the thing not to do, and a guard that read its own warning as a violation
    would be permanently red.
    """
    stripped = [l for l in text.splitlines() if not l.lstrip().startswith("#")]
    return re.sub(r"\\\n\s*", " ", "\n".join(stripped)).splitlines()


def push_destinations(text: str) -> list[str]:
    """Every `docker://…` a `copy` writes TO.

    🔴 EVERY OCCURRENCE, NOT THE FIRST. A third `skopeo copy` appended below the
    two real ones is exactly the shape that publishes a mutable tag while the
    first two still read correctly — the same defect `dockerfile_users` in
    `test_flake_image_matches_dockerfile.py` was fixed for.
    """
    out: list[str] = []
    for line in commands(text):
        if re.search(r"\bcopy\b", line):
            out += re.findall(r'"docker://([^"]+)"', line)
    return out


def inspect_invocations(text: str) -> list[str]:
    """Every command that `inspect`s a registry reference, however skopeo is spelled."""
    return [
        line.strip()
        for line in commands(text)
        if re.search(r"\binspect\b", line) and "docker://" in line
    ]


def docker_build_invocations(text: str) -> list[str]:
    """Every real `docker build`, prose about one excluded."""
    return [l.strip() for l in commands(text) if re.search(r"\bdocker build\b", l)]


def fetch_depths(text: str) -> list[str]:
    """Every `fetch-depth:` VALUE actually set, prose about one excluded.

    🔴 THIS EXTRACTOR EXISTS BECAUSE THE GUARD IT SERVES SURVIVED ITS OWN MUTANT.
    The first version searched the whole file for `fetch-depth:\\s*0`, and the
    step's comment explains the setting by NAMING it — so flipping the real value
    to `1` left the guard matching the sentence that describes it, and the mutant
    scored SURVIVED against a green suite. A guard on a WORD is walkable by the
    word appearing somewhere that is not the setting; this reads the setting.
    """
    return re.findall(r"^\s*fetch-depth:\s*(\S+)", "\n".join(commands(text)), re.M)


# ---------------------------------------------------------------------------


@pytest.fixture(scope="module")
def text() -> str:
    assert WORKFLOW.is_file(), (
        f"{WORKFLOW} is missing — every assertion below would pass vacuously "
        "over an empty string if this were not checked first"
    )
    return WORKFLOW.read_text(encoding="utf-8")


def test_the_extractors_find_something(text: str) -> None:
    """The positive control for this whole file, run before anything is judged.

    Each assertion below is a search. A search over text a broken extractor read
    as empty reports agreement, which reads exactly like a pass — so the
    extractors are proved to have found a non-empty answer FIRST, and the rest of
    the file is then a measurement rather than a claim.
    """
    assert workflow_triggers(text), "the `on:` extractor found no triggers at all"
    assert workflow_permissions(text), "the `permissions:` extractor found nothing"
    assert push_destinations(text), "no `docker://` destination found — nothing is pushed"
    assert inspect_invocations(text), "no `skopeo inspect` found — nothing is verified"
    assert fetch_depths(text), "the `fetch-depth:` extractor found no setting at all"


def test_no_pull_request_event_can_reach_this_workflow(text: str) -> None:
    """The fork guard, and it is structural rather than conditional.

    A job-level `if: github.event_name != 'pull_request'` is one edit away from
    being wrong and reads as correct while it is. Having no pull-request trigger
    at all means there is no event a fork can raise that reaches a job holding
    `packages: write`.
    """
    triggers = workflow_triggers(text)
    assert triggers == ALLOWED_TRIGGERS, (
        f"the trigger set is {sorted(triggers)}, and the pinned set is "
        f"{sorted(ALLOWED_TRIGGERS)}. This job holds `packages: write`; any "
        "trigger a pull request can raise hands a fork the ability to publish "
        "an image under this owner's namespace."
    )


def test_the_workflow_asks_for_exactly_the_permissions_it_needs(text: str) -> None:
    perms = workflow_permissions(text)
    assert perms.get("packages") == "write", (
        f"`packages` is {perms.get('packages')!r}; the push needs `write` and "
        "the failure without it is a 403 at the very last step, after the build"
    )
    assert perms.get("contents") == "read", (
        f"`contents` is {perms.get('contents')!r}. Declaring the block at all "
        "drops every other scope to none, which is the point of declaring it; "
        "`contents: read` is what the checkout needs and nothing here writes."
    )


def test_every_published_tag_is_one_of_the_two_immutable_ones(text: str) -> None:
    """The mutable-tag guard, as a ledger over destinations.

    `server/build-push.sh` states this repository's position in its own header:
    "A `:latest` default is how a mutable tag gets clobbered by a concurrent
    build and a pod silently restarts on somebody else's code." The consuming
    design asks both clusters to pull the SAME tag, and a tag whose contents can
    change under a running deployment does not satisfy that — it only looks like
    it does.
    """
    pushed = push_destinations(text)
    assert pushed, "no push destination found — the extractor is wrong"

    images = {dest.rpartition(":")[0] for dest in pushed}
    assert images == {IMAGE_EXPRESSION}, (
        f"the push destinations name {sorted(images)}; every one of them must be "
        f"{IMAGE_EXPRESSION!r}. One repository, computed once, is what makes the "
        "next assertion — about the registry that repository resolves to — cover "
        "all of them."
    )
    # …and that one expression resolves to the pinned package on ghcr, which is
    # the only registry an unauthenticated cluster is being promised.
    assert f'image="ghcr.io/$owner/{PACKAGE}"' in text, (
        f"the derive step no longer builds `ghcr.io/$owner/{PACKAGE}`. The owner "
        "is a variable on purpose — a fork must publish into its own namespace — "
        "but the registry and the package name are the contract."
    )

    tags = {dest.rpartition(":")[2] for dest in pushed}
    assert tags == ALLOWED_PUSH_TAG_EXPRESSIONS, (
        f"the published tag expressions are {sorted(tags)}, and the ledger is "
        f"{sorted(ALLOWED_PUSH_TAG_EXPRESSIONS)}. A tag added here is a tag some "
        "deployment will pin; if it can move, the deployment's pin is a fiction."
    )


def test_the_immutable_tag_is_the_full_commit_sha(text: str) -> None:
    """`sha-<40 hex>`, derived from the rev — not the short one, not a counter.

    "Both clusters pull the same tag" is only checkable if the tag is derivable
    from a revision, and only safe if that derivation cannot collide. The short
    rev satisfies the first and not the second.
    """
    assert 'sha_tag="sha-${{ github.sha }}"' in text, (
        "the immutable tag is no longer `sha-${{ github.sha }}`. `github.sha` is "
        "the full 40-hex commit sha; a short rev is derivable too but can collide, "
        "and a registry tag is what a cluster pins for months."
    )


def test_every_inspect_is_anonymous(text: str) -> None:
    """The one that closes the trap this workflow's whole purpose rests on.

    The job logs in to ghcr before it pushes and stays logged in. An `inspect`
    without `--no-creds` therefore reads the registry AS THIS REPOSITORY, which
    succeeds for a private package — so the check that is supposed to prove "any
    cluster can pull this" would pass in exactly the case where no cluster can.
    """
    bad = [line for line in inspect_invocations(text) if "--no-creds" not in line]
    assert not bad, (
        "these `inspect` invocations do not pass `--no-creds`:\n  "
        + "\n  ".join(bad)
        + "\nThe run is authenticated to ghcr; without the flag the check passes "
        "on its own credential and cannot see a private package."
    )


def test_the_anonymous_check_carries_its_own_negative_control(text: str) -> None:
    """An anonymous inspect that would succeed for ANY name proves nothing.

    The workflow inspects a tag that cannot exist and requires that to FAIL
    before it believes the real one. Pinned here because it is the kind of step
    that gets deleted as noise by someone who reads it as a duplicate.
    """
    assert "sha-0000000000000000000000000000000000000000" in text, (
        "the negative control (an anonymous inspect of an absent tag, which must "
        "fail) is gone. Without it a green anonymous pull is indistinguishable "
        "from an instrument that says yes to everything."
    )


def test_the_checkout_is_not_shallow(text: str) -> None:
    """`flake.nix` falls back to the literal string `unknown` for a tag it cannot
    derive, and a shallow checkout is the condition that triggers it.

    That fallback is silent — the build succeeds and ships an artefact labelled
    `unknown` — which is precisely the mislabelling the `version` comment in
    `flake.nix` says a derived version exists to prevent.
    """
    depths = fetch_depths(text)
    assert depths == ["0"], (
        f"the checkout sets fetch-depth {depths}, and the ledger is ['0']. "
        "`flake.nix` derives the image's own tag from `self.shortRev` and "
        'silently falls back to the literal "unknown" when it cannot resolve '
        "one — a build that succeeds and ships a mislabelled artefact."
    )


def test_the_publish_builds_the_FLAKE_image_and_not_a_third_one(text: str) -> None:
    """Two build paths are pinned against each other; a third is pinned to nothing.

    `tests/test_flake_image_matches_dockerfile.py` exists because `server/Dockerfile`
    and `packages.server-image` both state the runtime contract and nothing else
    stops one moving alone. A `docker build` added here would be a third statement
    of it, outside that pin, and the artefact that actually ships would be the
    unpinned one.
    """
    assert "nix build .#packages.x86_64-linux.server-image" in text, (
        "the published artefact no longer comes from the flake output"
    )
    builds = docker_build_invocations(text)
    assert not builds, (
        "a `docker build` appeared in the publish path:\n  "
        + "\n  ".join(builds)
        + "\nIt would be a THIRD way to produce this pod, outside the "
        "flake/Dockerfile pin, and it would be the one that actually ships."
    )


def test_the_controls_can_fail() -> None:
    """Drive every extractor over text that must NOT satisfy it.

    🔴 THIS IS THE NEGATIVE HALF, AND IT RUNS THE SHIPPING EXTRACTORS. Each of
    the assertions above is a search, and a search that cannot find anything
    agrees with every claim made about it. Planting the hazard and watching the
    extractor SEE it is what makes the green ones above a measurement.
    """
    hostile = (
        "name: hostile\n"
        "on:\n"
        "  push:\n"
        "    branches: [main]\n"
        "  pull_request:\n"
        "  workflow_dispatch:\n"
        "permissions:\n"
        "  contents: write\n"
        "  packages: write\n"
        "jobs:\n"
        "  publish:\n"
        "    steps:\n"
        "      - uses: actions/checkout@v4\n"
        "      - run: docker build -t x .\n"
        "      - run: skopeo copy a \"docker://ghcr.io/o/cairn-store:latest\"\n"
        "      - run: skopeo inspect \"docker://ghcr.io/o/cairn-store:latest\"\n"
    )

    # The trigger extractor SEES the planted pull-request trigger.
    triggers = workflow_triggers(hostile)
    assert "pull_request" in triggers, (
        "the trigger extractor did not see a planted `pull_request:` — every "
        "fork-guard assertion above would pass over a file that has one"
    )
    assert triggers != ALLOWED_TRIGGERS

    # The permissions extractor reads the planted `contents: write`.
    assert workflow_permissions(hostile).get("contents") == "write"

    # The destination extractor sees the mutable tag, and BOTH halves of the
    # ledger reject it: the repository is a literal rather than the one computed
    # expression, and the tag is `latest`.
    dests = push_destinations(hostile)
    assert "ghcr.io/o/cairn-store:latest" in dests
    assert {d.rpartition(":")[0] for d in dests} != {IMAGE_EXPRESSION}
    assert {d.rpartition(":")[2] for d in dests} != ALLOWED_PUSH_TAG_EXPRESSIONS

    # …and the inspect extractor sees an invocation with no `--no-creds`.
    bad = [l for l in inspect_invocations(hostile) if "--no-creds" not in l]
    assert bad, (
        "the inspect extractor found no credentialed inspect in text that "
        "contains one — `test_every_inspect_is_anonymous` would be vacuous"
    )

    # And the shallow-checkout guard fires: the hostile text sets no fetch-depth
    # at all, and — the case that actually bit — a COMMENT naming the right value
    # is not a setting of it.
    assert fetch_depths(hostile) == []
    assert fetch_depths("        # fetch-depth: 0 is what this needs\n") == []
    assert fetch_depths("        with:\n          fetch-depth: 1\n") == ["1"]

    # And the third-build-path guard SEES a `docker build` written the way a
    # workflow step actually writes one — `- run: docker build …`, which is not
    # at the start of its line. The first version of this guard was anchored and
    # therefore blind to exactly that spelling.
    assert docker_build_invocations(hostile) == ["- run: docker build -t x ."]

    # …and prose ABOUT a `docker build` is not one. A guard that read this
    # repository's own warnings as violations would be permanently red, and a
    # permanently-red guard is one nobody reads.
    assert docker_build_invocations(
        "      # a `docker build` here would be a third path\n"
    ) == []


def test_an_EMPTY_file_does_not_satisfy_the_extractors() -> None:
    """The other half of the control: nothing must read as agreement.

    An extractor returning an empty answer satisfies "contains no forbidden
    member" perfectly, which is the failure `test_the_extractors_find_something`
    exists to catch. This pins that the empty answer really is empty, so that
    guard is reachable rather than decorative.
    """
    assert workflow_triggers("") == set()
    assert workflow_permissions("") == {}
    assert push_destinations("") == []
    assert inspect_invocations("") == []
    assert docker_build_invocations("") == []
    assert fetch_depths("") == []
