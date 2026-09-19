"""The publish workflow's safety properties, pinned so they cannot move alone.

🔴 WHY THIS FILE EXISTS. `.github/workflows/publish-image.yml` pushes TWO images
to a PUBLIC registry using a token with `packages: write`. Several of its
properties are load-bearing and none of them is visible in a green run:

  * a pull request — including one from a fork — must never reach it;
  * the tags it publishes must be IMMUTABLE, so "both clusters pull the same tag"
    is a checkable sentence rather than a hopeful one;
  * the check that proves an image is anonymously pullable must actually be
    anonymous. The job is logged in to ghcr two steps earlier, so an `inspect`
    without `--no-creds` passes on the job's OWN credential and certifies
    nothing — a green that is indistinguishable from the failure it exists to
    catch. One control PER PACKAGE, because visibility is per-package;
  * both pods must be pushed, to their own packages, and each must carry ITS OWN
    controls — the Python pod's positive control is an interpreter `-c` probe and
    the Go image's `Cmd[0]` is a server binary that has no `-c`;
  * the whole PYTHON half must complete before the first GO step. Nothing in this
    repository has ever RUN the Go image, so every Go step is a first execution;
    in front of the Python publish, one of them going red keeps the pod that IS
    deployed unpublished — which is exactly what the seven failed runs did;
  * the `nix build` that resolves skopeo must name an OUTPUT. `nixpkgs#skopeo` is
    multi-output, so `--print-out-paths` prints two paths with the `-man` one
    FIRST; the step that appended `/bin/skopeo` to that value ran a two-line
    command and exited 127 on every run this workflow ever had.

A workflow file is configuration, so nothing in the suite would otherwise read
it, and a mistake in any of these is silent until it is expensive: the first
surfaces as a fork publishing an image, the second as a pod restarting on
somebody else's code, the third as `ImagePullBackOff` in a cluster that is not
this one, and the last as a workflow that has never once succeeded.

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

# The images' PACKAGE names are pinned; the OWNER is not, because it is derived
# from `github.repository_owner` so a fork publishes into its own namespace
# rather than failing against one it cannot write.
PACKAGE = "cairn-store"
PACKAGE_GO = "cairn-store-go"

# Every push destination must name one of these two expressions, so the
# assertion about what they RESOLVE to covers all of them rather than one.
IMAGE_EXPRESSION = "${{ steps.ref.outputs.image }}"
IMAGE_EXPRESSION_GO = "${{ steps.ref.outputs.image_go }}"
IMAGE_EXPRESSIONS = {IMAGE_EXPRESSION, IMAGE_EXPRESSION_GO}

# The flake outputs this workflow is allowed to publish, as a SET of package
# attribute names rather than a substring search. `server-image` is a PREFIX of
# `server-image-go`, so `"…x86_64-linux.server-image" in text` is satisfied by a
# file that builds only the Go one — a guard that reads as covering both while
# covering neither.
PUBLISHED_FLAKE_PACKAGES = {"server-image", "server-image-go"}

# The tag whose only job is to not exist. Written as a concatenation so the 40
# zeros are produced by the code rather than counted by eye, and asserted to be
# 40 in `test_the_anonymous_check_carries_its_own_negative_control` — the control
# must be refused because the tag is ABSENT, not because it is the wrong shape
# for a tag this workflow could ever push.
ABSENT_TAG = "sha-" + "0" * 40

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


def flake_packages_built(text: str) -> set[str]:
    """Every `.#packages.<system>.<name>` this workflow builds, as a NAME SET.

    🔴 A SET RATHER THAN A SUBSTRING SEARCH, AND THE DIFFERENCE IS LOAD-BEARING.
    `server-image` is a prefix of `server-image-go`, so
    `"…x86_64-linux.server-image" in text` is satisfied by a file that builds
    ONLY the Go image — the guard reads as "the Python pod still comes from the
    flake" while asserting nothing about it. Reading the names and comparing the
    set cannot be walked that way.
    """
    return set(
        re.findall(r"\.#packages\.[\w-]+\.([\w-]+)", "\n".join(commands(text)))
    )


def skopeo_nix_builds(text: str) -> list[str]:
    """Every command line that runs `nix build … nixpkgs#skopeo…`.

    🔴 THIS IS THE MEASURED DEFECT, NOT A STYLE RULE. `nixpkgs#skopeo` is a
    MULTI-OUTPUT derivation (`outputs = ["out", "man"]`), so
    `nix build … --print-out-paths` prints TWO store paths, the `-man` one first
    — re-measured at this flake's pinned lock on nix 2.34.8: 2 lines for the bare
    attribute, 1 for `nixpkgs#skopeo.out`. A step that captured the bare output
    into `out` and then ran `"$out/bin/skopeo"` built a two-line command: every
    run of this workflow died at exit 127, and the `$GITHUB_OUTPUT` write of the
    same multi-line value was rejected with `Invalid format`.
    """
    return [
        line.strip()
        for line in commands(text)
        if re.search(r"\bnix build\b", line) and "nixpkgs#skopeo" in line
    ]


def step_names(text: str) -> list[str]:
    """Every step's `- name:`, IN FILE ORDER, prose about one excluded.

    🔴 ORDER IS THE POINT, SO THIS RETURNS A LIST. The property it serves is a
    RELATION between two groups of steps — every Python-half step before every
    Go-half step — and a set or a membership check cannot express it.
    """
    return re.findall(r"^\s*- name:\s*(\S.*?)\s*$", "\n".join(commands(text)), re.M)


def normalise_shell(block: str) -> str:
    """A shell block as one NORMALISED line: comments dropped, continuations
    joined, every run of whitespace collapsed to a single space."""
    kept = [l for l in block.splitlines() if not l.lstrip().startswith("#")]
    joined = re.sub(r"\\\n\s*", " ", "\n".join(kept))
    return " ".join(joined.split())


def step_bodies(text: str) -> dict[str, str]:
    """`- name: <step>` → that step's `run: |` block, NORMALISED by `normalise_shell`.

    🔴 WHOLE NORMALISED TEXT, BECAUSE A GUARD ON WORDS IS WALKABLE BY REWORDING.
    The keyword form of "this workflow publishes both images" — a search for
    `cairn-store-go` somewhere in the file — is satisfied by a comment, by a
    deleted step's leftover prose, and by a push step whose destination was
    edited to something else. Pinning the step's entire command text means a
    cosmetic reword fails the test, which is the price of a machine-readable
    claim about what the step DOES.
    """
    lines = text.splitlines()
    out: dict[str, str] = {}
    name: str | None = None
    i = 0
    while i < len(lines):
        named = re.match(r"^\s*- name:\s*(\S.*?)\s*$", lines[i])
        if named:
            name = named.group(1)
            i += 1
            continue
        run = re.match(r"^(\s*)run:\s*\|\s*$", lines[i])
        if run and name is not None:
            indent = len(run.group(1))
            body: list[str] = []
            i += 1
            while i < len(lines):
                nxt = lines[i]
                if nxt.strip() and (len(nxt) - len(nxt.lstrip())) <= indent:
                    break
                body.append(nxt)
                i += 1
            out[name] = normalise_shell("\n".join(body))
            name = None
            continue
        i += 1
    return out


def absent_tag_inspects(text: str) -> list[str]:
    """Every anonymous inspect of the tag that must not exist — the negative control.

    🔴 A LIST, NOT A BOOLEAN. The earlier guard asked `ABSENT_TAG in text`, which
    two published images turn into a guard on ONE of them: delete either
    package's control and the string is still there, so the mutant survives. A
    count against the number of published images cannot be satisfied by the
    other image's control.
    """
    return [line for line in inspect_invocations(text) if ABSENT_TAG in line]


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
    assert flake_packages_built(text), "the `.#packages.…` extractor found no build at all"
    assert step_bodies(text), "the step-body extractor found no `run:` block at all"
    assert absent_tag_inspects(text), "no anonymous inspect of the absent tag was found"
    assert step_names(text), "the step-name extractor found no `- name:` at all"
    assert skopeo_nix_builds(text), "no `nix build … nixpkgs#skopeo` found at all"


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
    assert images == IMAGE_EXPRESSIONS, (
        f"the push destinations name {sorted(images)}; every one of them must be "
        f"one of {sorted(IMAGE_EXPRESSIONS)}. Two repositories, each computed "
        "once, is what makes the next assertion — about the registries those "
        "expressions resolve to — cover every push rather than one."
    )
    # …and those two expressions resolve to the pinned packages on ghcr, which is
    # the only registry an unauthenticated cluster is being promised.
    for package in (PACKAGE, PACKAGE_GO):
        assign = "image_go=" if package == PACKAGE_GO else "image="
        assert f'{assign}"ghcr.io/$owner/{package}"' in text, (
            f"the derive step no longer builds `ghcr.io/$owner/{package}`. The "
            "owner is a variable on purpose — a fork must publish into its own "
            "namespace — but the registry and the package name are the contract."
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

    🔴 ONE CONTROL PER PUBLISHED IMAGE, COUNTED. The earlier form of this test
    asked `ABSENT_TAG in text`, which was a correct guard while one image was
    published and became a guard on ONE OF TWO the moment a second was: deleting
    either package's control leaves the string present, so the mutant survives.
    """
    # The assertion `ABSENT_TAG`'s own comment names. `github.sha` is 40 hex, so
    # the control tag is built to the same shape a real one has — it must be
    # refused for being ABSENT rather than for being a malformed reference, which
    # would make the negative control pass for the wrong reason.
    assert len(ABSENT_TAG) == len("sha-") + 40, (
        f"the absent-tag control is {ABSENT_TAG!r}, which is not `sha-` plus 40 "
        "characters — the shape every tag this workflow pushes has"
    )

    controls = absent_tag_inspects(text)
    assert len(controls) == len(IMAGE_EXPRESSIONS), (
        f"{len(controls)} anonymous inspect(s) of the absent tag, for "
        f"{len(IMAGE_EXPRESSIONS)} published image(s):\n  "
        + "\n  ".join(controls)
        + "\nWithout one per image, that image's green anonymous pull is "
        "indistinguishable from an instrument that says yes to everything."
    )
    for expression in sorted(IMAGE_EXPRESSIONS):
        naming = [line for line in controls if expression in line]
        assert len(naming) == 1, (
            f"{len(naming)} absent-tag control(s) name {expression!r}; exactly "
            "one must. A control aimed at the OTHER package proves nothing about "
            "this one — the two are separate ghcr packages with separate "
            "visibility settings."
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
    built = flake_packages_built(text)
    assert built == PUBLISHED_FLAKE_PACKAGES, (
        f"this workflow builds {sorted(built)}; the ledger is "
        f"{sorted(PUBLISHED_FLAKE_PACKAGES)}. Every published artefact must come "
        "from a flake output, and both pods must come from THIS file rather than "
        "one of them quietly dropping out of the publish path."
    )
    builds = docker_build_invocations(text)
    assert not builds, (
        "a `docker build` appeared in the publish path:\n  "
        + "\n  ".join(builds)
        + "\nIt would be a THIRD way to produce this pod, outside the "
        "flake/Dockerfile pin, and it would be the one that actually ships."
    )


# ---------------------------------------------------------------------------
# The skopeo build — the structural fix for the measured failure.
#
# 🔴 THE MEASUREMENT, SO NOBODY RE-DERIVES IT. `nixpkgs#skopeo` declares
# `outputs = ["out", "man"]`, so `nix build … --print-out-paths` prints TWO store
# paths and the `-man` one comes FIRST. The step that captured that into a single
# variable and appended `/bin/skopeo` therefore ran a two-line command — exit 127
# — and wrote a multi-line value into `$GITHUB_OUTPUT`, which rejects it with
# `Invalid format`. Every run of this workflow failed there, at the same step.
#
# 🔴 AND THE FIX IS NIX'S OWN SELECTION, WHICH IS WHY THE GUARD BELOW IS THIS
# NARROW. `nixpkgs#skopeo.out` names ONE output, so `--print-out-paths` prints one
# line — measured at this flake's pinned lock on nix 2.34.8: 2 lines bare with
# `…-man` first, 1 line with `.out`, and `$out/bin/skopeo --version` answering
# `skopeo version 1.24.0`. Every alternative parses the same text instead ("take
# the last line", "drop anything ending in `-man`", or probe each printed
# candidate for an executable `bin/skopeo`) and every one of them is walked by a
# new `dev` output, a changed output order, or a platform-specific `debug`
# output — none of which change what `.out` NAMES.
# ---------------------------------------------------------------------------

# The spellings that select a single output. `.out` is the attribute path and
# `^out` is the output selector; nix resolves both to the same derivation output,
# and the workflow uses `.out` because it needs no shell quoting.
SKOPEO_OUTPUT_SELECTORS = ("nixpkgs#skopeo.out", "nixpkgs#skopeo^out")


def test_the_skopeo_build_names_an_OUTPUT_and_not_the_bare_derivation(text: str) -> None:
    """THE REGRESSION GUARD for the measured failure, on the narrowest thing
    that can be wrong: the attribute the build names.

    A bare `nixpkgs#skopeo` builds every output and prints every path, so
    whatever the step does with that value afterwards — append `/bin/skopeo`,
    write it to `$GITHUB_OUTPUT`, run it — it is doing it to two lines. This
    asserts the build asks nix for ONE output; what the step then does with a
    single-line answer is ordinary shell.
    """
    builds = skopeo_nix_builds(text)
    assert builds, (
        "no `nix build … nixpkgs#skopeo…` found at all. skopeo must come from the "
        "flake's own pinned nixpkgs rather than from whatever the runner image "
        "happens to ship, and with no build here this guard would be vacuous."
    )
    unselected = [
        line
        for line in builds
        if not any(selector in line for selector in SKOPEO_OUTPUT_SELECTORS)
    ]
    assert not unselected, (
        "these `nix build` lines name the bare multi-output derivation:\n  "
        + "\n  ".join(unselected)
        + f"\nOne of {list(SKOPEO_OUTPUT_SELECTORS)} is required. `nixpkgs#skopeo` "
        "declares `outputs = [\"out\" \"man\"]`, so `--print-out-paths` prints TWO "
        "store paths and the `-man` one FIRST: the value becomes a two-line "
        "command (exit 127) and a `$GITHUB_OUTPUT` write the runner rejects with "
        "`Invalid format`. That is what every run of this workflow did."
    )


# ---------------------------------------------------------------------------
# Both pods are published, and each carries ITS OWN controls.
# ---------------------------------------------------------------------------

# 🔴 THE WHOLE NORMALISED COMMAND TEXT OF EVERY PUSH STEP, PINNED BY EQUALITY.
# The repository's own lesson: a guard on WORDS is walkable by rewording, so when
# the artefact under test is text, pin the whole normalised string and pay the
# cost of a cosmetic reword failing the test. A keyword search for
# `cairn-store-go` would be satisfied by a comment mentioning it.
PINNED_PUSH_STEPS = {
    "push the Python pod's immutable sha tag": (
        "set -euo pipefail "
        '"${{ steps.skopeo.outputs.bin }}" copy --all '
        '"docker-archive:${{ steps.build.outputs.archive }}" '
        '"docker://${{ steps.ref.outputs.image }}:${{ steps.ref.outputs.sha_tag }}"'
    ),
    "push the Python pod's version tag, on a tag push only": (
        "set -euo pipefail "
        '"${{ steps.skopeo.outputs.bin }}" copy --all '
        '"docker-archive:${{ steps.build.outputs.archive }}" '
        '"docker://${{ steps.ref.outputs.image }}:${{ steps.ref.outputs.version_tag }}"'
    ),
    "push the Go pod's immutable sha tag": (
        "set -euo pipefail "
        '"${{ steps.skopeo.outputs.bin }}" copy --all '
        '"docker-archive:${{ steps.build-go.outputs.archive }}" '
        '"docker://${{ steps.ref.outputs.image_go }}:${{ steps.ref.outputs.sha_tag }}"'
    ),
    "push the Go pod's version tag, on a tag push only": (
        "set -euo pipefail "
        '"${{ steps.skopeo.outputs.bin }}" copy --all '
        '"docker-archive:${{ steps.build-go.outputs.archive }}" '
        '"docker://${{ steps.ref.outputs.image_go }}:${{ steps.ref.outputs.version_tag }}"'
    ),
}

# The step whose body is the Go pod's own control set, and the Python one it must
# not be a copy of.
GO_CONTROL_STEP = "control — the GO image is EMPTY of store data, declares its routes, and refuses"
PYTHON_CONTROL_STEP = "control — /data is EMPTY in the image, and the code is really there"

# A line of `api.DeclaredRoutes()` is `"<METHOD> <head>"` — `internal/api/routes.go`
# builds every entry as `method + " " + head`. The methods are spelled in upper
# case and the heads in lower, which is what keeps this from matching the step's
# own prose (`REFUSING TO PUBLISH`, `head -20`).
ROUTE_LITERAL = re.compile(r"\b(?:GET|HEAD|POST|PUT|PATCH|DELETE|OPTIONS)\s+[a-z][\w-]*\b")

# 🔴 THE TWO HALVES, IN THE ORDER THE JOB MUST RUN THEM. Every member of the
# first must appear before every member of the second.
PYTHON_HALF_STEPS = (
    "build the Python server image from the flake",
    PYTHON_CONTROL_STEP,
    "push the Python pod's immutable sha tag",
    "push the Python pod's version tag, on a tag push only",
    "PROVE the published image is pullable with NO credentials",
)
GO_HALF_STEPS = (
    "build the Go server image from the flake",
    GO_CONTROL_STEP,
    "push the Go pod's immutable sha tag",
    "push the Go pod's version tag, on a tag push only",
    "PROVE the published GO image is pullable with NO credentials",
)


def test_the_whole_PYTHON_half_runs_before_the_first_GO_step(text: str) -> None:
    """🔴 A RELATION BETWEEN TWO GROUPS, NOT A PROPERTY OF ONE STEP.

    Nothing in this repository has ever RUN the Go image — `ci.yml` asserts only
    that it BUILDS — so every Go step in this workflow is a FIRST execution. A
    first execution placed in front of the Python publish gates the pod that is
    actually deployed on a path nobody has exercised, which is the exact shape of
    the failure this workflow was rewritten to fix: seven consecutive runs where
    nothing published because one unexercised step went red.

    The earlier draft had the Go BUILD and the Go CONTROLS before the Python
    push, and carried a comment claiming "it runs last" — true of the Go
    anonymous-pull proof alone, and false of the half it was read as covering.
    """
    names = step_names(text)
    missing = [n for n in PYTHON_HALF_STEPS + GO_HALF_STEPS if n not in names]
    assert not missing, (
        f"these steps are absent, so the ordering below would compare nothing: "
        f"{missing}\nthe file's steps are: {names}"
    )
    last_python = max(names.index(n) for n in PYTHON_HALF_STEPS)
    first_go = min(names.index(n) for n in GO_HALF_STEPS)
    assert last_python < first_go, (
        f"{names[first_go]!r} (step {first_go + 1}) runs before "
        f"{names[last_python]!r} (step {last_python + 1}).\n"
        "Every Python step — build, control, both pushes and the anonymous-pull "
        "proof — must finish before the FIRST Go-image step. The Go image has "
        "never been run by anything in this repository; a first execution in "
        "front of the Python publish leaves the deployed pod unpublished when it "
        "goes red, which is what the last seven runs of this workflow did."
    )


def test_both_pods_are_published_and_every_push_step_is_pinned_WHOLE(text: str) -> None:
    """Four push steps, each pinned by its entire normalised command text.

    A reword cannot walk past this, a deleted step cannot hide behind the other
    three, and a destination edited to a different package fails on the string
    rather than on a keyword that happens to survive elsewhere in the file.
    """
    bodies = step_bodies(text)
    missing = sorted(set(PINNED_PUSH_STEPS) - set(bodies))
    assert not missing, (
        f"these push steps are absent: {missing}\nthe file's steps are: "
        f"{sorted(bodies)}\nBoth pods must be pushed from this one workflow; a "
        "second place that publishes an image is a second contract."
    )
    actual = {name: bodies[name] for name in PINNED_PUSH_STEPS}
    assert actual == PINNED_PUSH_STEPS, (
        "a push step's command text moved. Every difference is shown by pytest "
        "below; this is pinned WHOLE rather than by keyword because a guard on "
        "words is walkable by rewording, and what these four lines do — which "
        "archive goes to which repository under which tag — is the contract."
    )


def test_the_GO_pods_positive_control_is_its_ROUTE_LEDGER_not_the_Pythons(text: str) -> None:
    """MEASURED: the Python positive control does not transfer to the Go image.

    The Python control runs the image's own `Cmd[0]` — an interpreter — with
    `-c 'import subsystem_recall …'`. The Go image's `Cmd[0]` is the SERVER
    BINARY, which has no `-c`: it answers `flag provided but not defined: -c` and
    exits non-zero, so the copied control would fail for a reason that has
    nothing to do with what it claims to measure. `cairn-server -routes` is the
    Go pod's own equivalent — a ledger read out of the RUNNING image, which is
    the one claim a build cannot make.
    """
    bodies = step_bodies(text)
    assert GO_CONTROL_STEP in bodies, (
        f"the Go pod has no control step named {GO_CONTROL_STEP!r}; the file's "
        f"steps are {sorted(bodies)}"
    )
    assert PYTHON_CONTROL_STEP in bodies, (
        "the PYTHON control step is gone, so the comparison this test makes is "
        "vacuous — it would be asserting a difference against nothing"
    )

    go = bodies[GO_CONTROL_STEP]
    assert "import subsystem_recall" not in go, (
        "the Go pod's control imports a PYTHON module. Measured: the Go image's "
        "`Cmd[0]` is the server binary and rejects `-c` with `flag provided but "
        "not defined: -c`, so this control cannot pass for the reason it names."
    )
    assert "-routes" in go, (
        "the Go pod's positive control no longer reads the route ledger out of "
        "the running image. Without it, `/data holds 0 files` is the only "
        "measurement left — and a zero is what an image with no filesystem "
        "reports too."
    )
    assert "routes_rc" in go and "declared" in go, (
        "the Go control no longer captures the binary's EXIT CODE and the "
        "ledger's LINE COUNT. Both are needed: a zero-line ledger and a binary "
        "that would not start are different facts, and `set -e` alone cannot "
        "tell the log which one happened."
    )
    assert "EMPTY" in go and "REFUSING TO PUBLISH" in go, (
        "the Go control does not refuse loudly on an empty ledger. A command "
        "that printed nothing and exited 0 would otherwise read as a pass, which "
        "is the reassuring zero this repository's own rules forbid."
    )

    # 🔴 AND IT MUST NOT NAME ROUTES — THAT IS THE ASSERTION, NOT AN OMISSION.
    # `AGENTS.md` says a route moves FOUR ledgers together (`api.DeclaredRoutes()`,
    # `tests/conformance/requests.json`, the construction check and
    # `checks.go-server-declares-its-routes`). A route name written into this
    # workflow is a FIFTH spelling none of those four can see: rename a route and
    # all four stay green while this job refuses mid-push. The earlier version of
    # this test asserted the opposite — that `'GET recall'` and `'POST entry'`
    # appear here — which is how the fifth site got written in the first place.
    named_routes = ROUTE_LITERAL.findall(go)
    assert not named_routes, (
        f"the Go control names route(s) {named_routes}. That is a FIFTH route "
        "ledger, and the four `AGENTS.md` lists cannot see it — renaming a route "
        "would leave all four green and this job refusing mid-push, after the "
        "Python pod has already been published. A publish control's question is "
        "'can this binary ever observe the thing': assert the ledger is non-empty "
        "and the binary exited 0, and leave WHICH routes to the four ledgers."
    )

    # …and the Python control is the only place its own import appears, so the
    # assertion above is about the whole file rather than one step's spelling.
    assert text.count("import subsystem_recall") == 1, (
        f"`import subsystem_recall` appears {text.count('import subsystem_recall')} "
        "times. It belongs to the Python pod's control and nowhere else."
    )
    assert "import subsystem_recall" in bodies[PYTHON_CONTROL_STEP]


def test_the_go_package_documents_that_its_FIRST_publish_will_fail(text: str) -> None:
    """A ghcr package is PRIVATE on first publish, and the new one has never published.

    The Go pod's anonymous-pull proof is therefore EXPECTED to fail on its first
    run, and the failure looks identical to a broken workflow unless the file
    says so and prints the one-time settings URL. An invariant guard, labelled:
    it pins a property no measured bug has violated, because the property is new.
    """
    assert f"packages/container/{PACKAGE_GO}/settings" in text, (
        f"the exact settings URL for the `{PACKAGE_GO}` package is not printed. "
        "GitHub exposes no REST route for the visibility flip, so an operator "
        "who cannot find the page cannot finish the publish."
    )
    assert f"packages/container/{PACKAGE}/settings" in text, (
        f"the `{PACKAGE}` package's settings URL went missing while the Go one "
        "was added — the two failures are separate and so are the two pages"
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
    assert {d.rpartition(":")[0] for d in dests} != IMAGE_EXPRESSIONS
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

    # 🔴 THE NEW EXTRACTORS, DRIVEN OVER THE HAZARD EACH EXISTS FOR.
    #
    # The package-set extractor must SEE a build that names only the Go output —
    # the exact text a substring search for `…server-image` reads as "both are
    # still built", because `server-image` is a prefix of `server-image-go`.
    go_only = "      - run: nix build .#packages.x86_64-linux.server-image-go --no-link\n"
    assert flake_packages_built(go_only) == {"server-image-go"}
    assert flake_packages_built(go_only) != PUBLISHED_FLAKE_PACKAGES
    assert "nix build .#packages.x86_64-linux.server-image" in go_only, (
        "the naive substring search is satisfied by Go-only text — which is why "
        "the shipped guard compares a SET of names instead"
    )

    # The skopeo-build extractor sees the shape that actually failed: a build of
    # the BARE multi-output attribute, whose output the step then appended to.
    old_step = (
        "        run: |\n"
        "          out=$(nix build --inputs-from . nixpkgs#skopeo "
        "--no-link --print-out-paths)\n"
        "          printf 'bin=%s/bin/skopeo\\n' \"$out\" >> \"$GITHUB_OUTPUT\"\n"
        '          "$out/bin/skopeo" --version\n'
    )
    old_builds = skopeo_nix_builds(old_step)
    assert len(old_builds) == 1, (
        f"the skopeo-build extractor found {old_builds} in the step that failed; "
        "it must see exactly the one `nix build` line"
    )
    assert not any(s in old_builds[0] for s in SKOPEO_OUTPUT_SELECTORS), (
        "the extractor saw the pre-fix build but the selector check accepted it "
        "— `test_the_skopeo_build_names_an_OUTPUT_and_not_the_bare_derivation` "
        "would then be green over the exact text that exited 127 seven times"
    )
    # …and the FIXED spelling passes the same check, so the guard is a
    # discrimination rather than a ban on the two words `nix build`.
    fixed_step = (
        "        run: |\n"
        "          out=$(nix build --inputs-from . nixpkgs#skopeo.out "
        "--no-link --print-out-paths)\n"
    )
    assert any(s in skopeo_nix_builds(fixed_step)[0] for s in SKOPEO_OUTPUT_SELECTORS)
    # …and prose ABOUT the hazard is not the hazard, for the same reason
    # `docker_build_invocations` drops comments.
    assert skopeo_nix_builds(
        "      # never `nix build … nixpkgs#skopeo` without an output selector\n"
    ) == []

    # The step-NAME extractor reads names in FILE ORDER — the ordering guard is a
    # comparison of indices, so a set-like or reordered answer would make it
    # vacuous — and prose naming a step is not a step.
    ordered = (
        "      - name: second thing\n"
        "        run: |\n"
        "          echo two\n"
        "      - name: first thing\n"
        "        run: |\n"
        "          echo one\n"
    )
    assert step_names(ordered) == ["second thing", "first thing"]
    assert step_names("      # - name: a step described in prose\n") == []

    # The route-literal pattern SEES a hardcoded ledger entry — the fifth
    # spelling — and does NOT fire on the control step's own prose.
    assert ROUTE_LITERAL.findall("for want in 'GET recall' 'POST entry'; do") == [
        "GET recall",
        "POST entry",
    ]
    assert ROUTE_LITERAL.findall(
        'echo "REFUSING TO PUBLISH: the route ledger is EMPTY"; echo "$out" | head -20'
    ) == []

    # The step-body extractor reads a whole `run: |` block and stops at the next
    # step, rather than swallowing the file from there on.
    two_steps = (
        "      - name: first\n"
        "        run: |\n"
        "          set -euo pipefail\n"
        "          echo one \\\n"
        "            two\n"
        "          # a comment, which is not command text\n"
        "      - name: second\n"
        "        run: |\n"
        "          echo three\n"
    )
    assert step_bodies(two_steps) == {
        "first": "set -euo pipefail echo one two",
        "second": "echo three",
    }

    # …and a step whose body was REWORDED is a different string, which is the
    # whole point of pinning the normalised text rather than a keyword.
    assert step_bodies(two_steps)["second"] != "echo four"

    # The absent-tag extractor counts controls; two images with ONE control is a
    # different answer from two images with two.
    one_control = (
        "      - run: skopeo inspect --no-creds "
        f'"docker://${{{{ steps.ref.outputs.image }}}}:{ABSENT_TAG}"\n'
    )
    assert len(absent_tag_inspects(one_control)) == 1
    assert len(absent_tag_inspects(one_control)) != len(IMAGE_EXPRESSIONS)


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
    assert flake_packages_built("") == set()
    assert step_bodies("") == {}
    assert absent_tag_inspects("") == []
    assert skopeo_nix_builds("") == []
    assert step_names("") == []
