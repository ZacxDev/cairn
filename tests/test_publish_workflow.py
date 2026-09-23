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
    catch. One control PER PACKAGE, because visibility is per-package — and,
    MEASURED, the POSITIVE half is per-package too: aiming the Go proof's `ref`
    at the Python package left the whole suite green while the job pushed the Go
    image, printed `ANONYMOUS PULL OK` for `cairn-store` and exited 0, so the
    one condition the step exists to surface went invisible. The negative
    controls are COUNTED one per package and the positive half is pinned by the
    step's own `ref` and by its whole command text;
  * both pods must be pushed, to their own packages, and each must carry ITS OWN
    controls — the Python pod's positive control is an interpreter `-c` probe and
    the Go image's `Cmd[0]` is a server binary that has no `-c`;
  * the whole PYTHON half must complete before the first GO step. ⚠ This read
    "Nothing in this repository has ever RUN the Go image … keeps the pod that IS
    deployed unpublished" — spent at the cutover: the Go image IS the deployed
    pod, so the ordering now exposes the DEPLOYED pod to an earlier red step.
    Unchanged deliberately; see the assertion's own message. The seven failed
    runs are still why a first-execution step is not put first;
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
#
# The sha tag is named separately because it is also the tag the anonymous-pull
# proofs inspect — one expression, read by two guards, rather than two spellings
# of it that can disagree.
SHA_TAG_EXPRESSION = "${{ steps.ref.outputs.sha_tag }}"
ALLOWED_PUSH_TAG_EXPRESSIONS = {
    SHA_TAG_EXPRESSION,
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


def _dropped(block: str) -> str:
    """A shell block with comments dropped and continuations joined, LINE
    STRUCTURE PRESERVED.

    It exists because two different questions are asked of the same text: "is this
    step's whole command text unchanged" wants one collapsed line, and "does this
    `if` branch exit non-zero" is a question about STATEMENTS, which only survive
    while the lines do. Sharing this half keeps both answers derived from the same
    view of the file.
    """
    kept = [l for l in block.splitlines() if not l.lstrip().startswith("#")]
    return re.sub(r"\\\n\s*", " ", "\n".join(kept))


def step_blocks(text: str) -> dict[str, str]:
    """`- name: <step>` → that step's `run: |` block, comments dropped and
    continuations joined, but still LINE BY LINE.

    `step_bodies` is this collapsed to one string per step; the statement-level
    guards below need the lines, so the walk lives here and both read it.
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
            out[name] = _dropped("\n".join(body))
            name = None
            continue
        i += 1
    return out


def step_bodies(text: str) -> dict[str, str]:
    """`- name: <step>` → that step's `run: |` block, collapsed to ONE LINE of
    single-spaced tokens (`" ".join(block.split())`).

    🔴 WHOLE NORMALISED TEXT, BECAUSE A GUARD ON WORDS IS WALKABLE BY REWORDING.
    The keyword form of "this workflow publishes both images" — a search for
    `cairn-store-go` somewhere in the file — is satisfied by a comment, by a
    deleted step's leftover prose, and by a push step whose destination was
    edited to something else. Pinning the step's entire command text means a
    cosmetic reword fails the test, which is the price of a machine-readable
    claim about what the step DOES.
    """
    return {name: " ".join(block.split()) for name, block in step_blocks(text).items()}


def exit_statements(block: str) -> list[str]:
    """Every `exit <code>` that is a STATEMENT in this block, in order.

    🔴 A STATEMENT, NOT THE TWO CHARACTERS. The Go control's own refusal prints
    `not exit 0 (rc=$routes_rc, …)` — correct prose describing the failure it
    caught — so a substring sweep for `exit 0` reds on a sentence nobody should
    change, which is how a guard trains its reader to edit correct text. A line
    that IS an `exit` starts with it; an echoed one does not. `exit 1 ;;` inside
    a `case` arm is a statement and is counted.
    """
    out: list[str] = []
    for line in _dropped(block).splitlines():
        m = re.match(r"^exit\s+(\S+)", line.strip())
        if m:
            out.append(m.group(1))
    return out


def guard_branches(block: str, variable: str) -> list[list[str]]:
    """For every `if` in this block whose CONDITION names `variable`, the exit
    codes that branch contains — one list per branch, in file order.

    🔴 A VARIABLE THAT EXISTS IS NOT A GUARD; ONLY A BRANCH ON IT IS. The probe
    `leaked=$(… ls -A /data | wc -l)` can be present, correct, and load-bearing
    in a step that never reads it — the measurement happens and nothing acts on
    it. This returns the branch, so the assertion can be about what the step
    DOES when the measurement says the hazard is present.

    The walk ends at the first line that is exactly `fi`, so a nested `if` inside
    such a branch would be mis-scoped. None of the four control steps nests one;
    if one ever does, this must grow a depth counter rather than be relaxed.
    """
    out: list[list[str]] = []
    lines = _dropped(block).splitlines()
    for i, line in enumerate(lines):
        stripped = line.strip()
        if not stripped.startswith("if ") or variable not in stripped:
            continue
        codes: list[str] = []
        for nxt in lines[i + 1:]:
            tail = nxt.strip()
            if tail == "fi":
                break
            m = re.match(r"^exit\s+(\S+)", tail)
            if m:
                codes.append(m.group(1))
        out.append(codes)
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


def proof_refs(block: str) -> list[str]:
    """Every `ref='…'` a PROVE step assigns — the reference it actually inspects.

    🔴 THIS IS THE POSITIVE HALF OF THE ANONYMOUS CHECK, AND NOTHING USED TO
    CONSTRAIN IT. `absent_tag_inspects` counts the NEGATIVE controls one per
    package, which is a claim about the tags that must not exist. The tag that
    must exist is reached through `$ref`, one indirection away, so the Go step's
    `ref` could name the PYTHON package with every count still correct — the
    shape a copy-paste produces, measured SURVIVED against a green suite.
    """
    return re.findall(r"ref='([^']*)'", block)


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
    assert step_blocks(text), "the step-block extractor found no `run:` block at all"
    assert absent_tag_inspects(text), "no anonymous inspect of the absent tag was found"
    assert step_names(text), "the step-name extractor found no `- name:` at all"
    assert skopeo_nix_builds(text), "no `nix build … nixpkgs#skopeo` found at all"
    # The statement-level extractors, which answer "can this step FAIL" rather
    # than "does this step say the right words". An empty answer from either is
    # agreement with every claim made about it.
    assert any(exit_statements(b) for b in step_blocks(text).values()), (
        "the `exit` extractor found no exit statement anywhere — every refusal "
        "assertion below would pass over a workflow that can only succeed"
    )
    assert any(
        guard_branches(step_blocks(text)[s], "$leaked") for s in IMAGE_CONTROL_STEPS
    ), "the branch extractor found no `if … $leaked …` branch at all"
    assert all(proof_refs(step_bodies(text)[s]) for s in PROOF_STEP_PACKAGES), (
        "the `ref='…'` extractor found nothing in one of the proof steps"
    )


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
IMAGE_CONTROL_STEPS = (PYTHON_CONTROL_STEP, GO_CONTROL_STEP)

# …and the two steps that prove a published image can be pulled by somebody who
# holds no credential, which is the question this whole workflow exists for.
PYTHON_PROOF_STEP = "PROVE the published image is pullable with NO credentials"
GO_PROOF_STEP = "PROVE the published GO image is pullable with NO credentials"

# Each proof step's OWN image expression and OWN ghcr package. The mapping is
# what `test_each_anonymous_proof_inspects_ITS_OWN_package` reads; a proof aimed
# at the other package is a green that certifies the wrong thing.
PROOF_STEP_PACKAGES = {
    PYTHON_PROOF_STEP: (IMAGE_EXPRESSION, PACKAGE),
    GO_PROOF_STEP: (IMAGE_EXPRESSION_GO, PACKAGE_GO),
}

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
    PYTHON_PROOF_STEP,
)
GO_HALF_STEPS = (
    "build the Go server image from the flake",
    GO_CONTROL_STEP,
    "push the Go pod's immutable sha tag",
    "push the Go pod's version tag, on a tag push only",
    GO_PROOF_STEP,
)


def test_the_whole_PYTHON_half_runs_before_the_first_GO_step(text: str) -> None:
    """🔴 A RELATION BETWEEN TWO GROUPS, NOT A PROPERTY OF ONE STEP.

    ⚠ THIS DOCSTRING'S RATIONALE IS SPENT — see the assertion message below,
    which carries the correction. It read: "Nothing in this repository has ever
    RUN the Go image … A first execution placed in front of the Python publish
    gates the pod that is actually deployed on a path nobody has exercised."
    The Go image IS the deployed pod now, so the Python publish gates a pod
    NOTHING runs, and this ordering exposes the deployed one to an earlier red
    step. `ci.yml` does still assert only that the Go image BUILDS.

    🔴 THE ORDERING IS UNCHANGED AND THAT IS DELIBERATE — reversing it is a CI
    behaviour change, and seven consecutive runs where nothing published because
    one unexercised step went red is still the reason a first-execution step is
    not put first. Decide it; do not drift into it.

    ⚠ The correction first went into the assertion message ONLY, leaving this
    docstring asserting the spent version — the message renders on failure, the
    docstring renders in `pytest -v` and to anyone opening the file. A correction
    applied at one of two sites reads as complete at whichever site you land on.

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
        "proof — must finish before the FIRST Go-image step.\n"
        "⚠ THE ORIGINAL RATIONALE HAS INVERTED AND THE ORDERING IS NOT RE-ARGUED "
        "HERE. It read: 'The Go image has never been run by anything in this "
        "repository; a first execution in front of the Python publish leaves the "
        "deployed pod unpublished when it goes red, which is what the last seven "
        "runs of this workflow did.' Both legs are spent — the Go image IS the "
        "deployed pod, so this ordering now publishes the DEPLOYED pod LAST and "
        "most exposed to an earlier red step, which is the opposite of what the "
        "rationale asked for. The ordering is left UNCHANGED deliberately: "
        "reversing it is a CI behaviour change with its own blast radius, not a "
        "docs edit, and the seven-failure history is still the reason a "
        "first-execution step is not put first. Decide it, do not drift into it."
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


#: The one credential-handling `run:` step in this workflow, pinned WHOLE.
#:
#: ✅ **DECIDED: PIN IT.** The handoff filed this as one of "three unpinned by
#: construction" in `publish-image.yml`, closing condition "one PR each, or a
#: written line saying why not". Of the three it is the one with a concrete
#: failure scenario, so it gets the PR rather than the line.
#:
#: 🔴 WHAT THE PIN BUYS, CONCRETELY. `--password-stdin` keeps the token off the
#: command line. The one-character-class edit that undoes it — `-p '${{
#: secrets.GITHUB_TOKEN }}'` — is the obvious "simplification" for somebody
#: debugging a login failure, and it puts a live credential in **argv**, where it
#: is visible in the process table and in any `set -x` trace the step or a future
#: `RUNNER_DEBUG` run produces. Nothing else in this file would notice: the
#: secret is still referenced, the step still logs in, the push still succeeds,
#: and every existing assertion stays green.
#:
#: Pinned whole for the reason `test_both_pods_are_published_and_every_push_step_
#: is_pinned_WHOLE` gives: a guard on the WORD `--password-stdin` is satisfied by
#: a step that also passes `-p`, and a guard on its absence is satisfied by a
#: reword. The cost is the same one that test accepts — reformatting this step
#: fails the test — and it buys a machine-readable claim about a credential.
PINNED_CREDENTIAL_STEP = {
    "log in to ghcr": (
        "set -euo pipefail "
        "printf '%s' '${{ secrets.GITHUB_TOKEN }}' "
        '| "${{ steps.skopeo.outputs.bin }}" login ghcr.io '
        "-u '${{ github.actor }}' --password-stdin"
    ),
}


def test_the_credential_step_is_pinned_WHOLE(text: str) -> None:
    """The only `run:` step that handles a secret, pinned by its entire text.

    ⚠ ITS POSITIVE CONTROL IS THE STEP'S PRESENCE, asserted separately: a step
    that had been renamed or deleted would otherwise make the comparison below
    vacuous by having nothing on either side.
    """
    bodies = step_bodies(text)
    missing = sorted(set(PINNED_CREDENTIAL_STEP) - set(bodies))
    assert not missing, (
        f"the credential step is absent under that name: {missing}\nthe file's "
        f"steps are: {sorted(bodies)}\nIf the login moved, move this pin with it — "
        f"an absent step makes the comparison below compare nothing."
    )
    actual = {name: bodies[name] for name in PINNED_CREDENTIAL_STEP}
    assert actual == PINNED_CREDENTIAL_STEP, (
        "the credential step's command text moved. pytest shows every difference "
        "below. This is pinned WHOLE because the hazard is a SPELLING — swapping "
        "`--password-stdin` for `-p '<secret>'` puts a live token in argv while "
        "leaving every other assertion in this file green."
    )


# ── THE THREE "UNPINNED BY CONSTRUCTION" ENTRIES, ALL THREE ANSWERED ─────────────
#
# The handoff filed them together, closing condition *"one PR each, or a written
# line saying why not"*. 🔴 IT NEVER NAMED THEM, so a reader could not identify
# what was open. Named here, with the disposition:
#
#   1. `log in to ghcr` — the one `run:` step handling a secret.  → PINNED below,
#      whole-body, plus a GROW arm refusing a SECOND secret-handling step.
#   2. the step-level `if:` on the two version-tag pushes.        → PINNED below.
#   3. `ci.yml`'s ARM-battery count — *"prove every ARM can go RED (7 mutants…)"*.
#      → **A WRITTEN LINE, NOT A PR, AND HERE IT IS.**
#
# ⚠ ON (3), AND WHY IT IS THE ONE THAT GETS THE LINE. It is the same defect class
# `tests/test_control_mutant_count_is_pinned.py` already closes twice — a count in
# a CI step NAME that nothing derives from the battery it describes — and closing
# it means extending that file's anchor ledger to a THIRD battery, in a different
# job, whose module this file does not read. That is coherent work with a clear
# shape and it is not this PR's: this file is about `publish-image.yml`, and the
# ARM battery is about `ci.yml`'s ARM job. Doing it here would mean a second file
# growing a third ledger as a side effect of a PR that names neither.
# **Closing condition, so it reads as open rather than absent:** an entry in
# `test_control_mutant_count_is_pinned.py`'s anchor ledger for the ARM battery,
# derived from its own `len(MUTANTS)`, with the same GROW/SHRINK shape as the
# control and publish ledgers beside it.


#: `- name: <step>` → the step-level `if:` expression guarding it, for every step
#: that has one. The whole-body pins above read the `run:` block and are
#: STRUCTURALLY BLIND to this line.
def step_conditions(text: str) -> dict[str, str]:
    out: dict[str, str] = {}
    name = None
    for line in text.splitlines():
        stripped = line.strip()
        if stripped.startswith("- name:"):
            name = stripped[len("- name:"):].strip()
        elif stripped.startswith("if:") and name is not None:
            out.setdefault(name, stripped[len("if:"):].strip())
        elif stripped.startswith("run:"):
            # The `run:` block ends the step's key region for our purposes; a
            # later `if:` inside a shell body is shell, not YAML.
            name = name if name not in out else name
    return out


#: The two steps whose publication is CONDITIONAL, and the condition.
#:
#: ✅ **DECIDED: PIN IT** — the second of the three "unpinned by construction"
#: entries. `step_bodies` normalises the `run:` block and never sees the `if:`,
#: so the line that makes "a version tag is published on a TAG PUSH ONLY" true is
#: unasserted, while the four push steps' bodies are pinned whole.
#:
#: 🔴 The failure it closes: delete the `if:` and both steps run on every push to
#: `main`. `steps.ref.outputs.version_tag` is empty there, so the destination
#: becomes `…/cairn-store:` — a push to an EMPTY tag, on every commit, from a
#: workflow whose stated contract is two immutable tags. Every existing assertion
#: stays green: the bodies are untouched, the tags they name are still the two
#: immutable ones, and `test_every_published_tag_is_one_of_the_two_immutable_ones`
#: reads the body it always read.
PINNED_STEP_CONDITIONS = {
    "push the Python pod's version tag, on a tag push only":
        "steps.ref.outputs.version_tag != ''",
    "push the Go pod's version tag, on a tag push only":
        "steps.ref.outputs.version_tag != ''",
}


def test_the_conditional_pushes_keep_their_CONDITION(text: str) -> None:
    """The `if:` the whole-body pins cannot see."""
    conditions = step_conditions(text)
    missing = sorted(set(PINNED_STEP_CONDITIONS) - set(conditions))
    assert not missing, (
        f"these steps no longer carry ANY step-level `if:`: {missing}\nA version "
        f"tag would then be pushed on every commit to the default branch, with an "
        f"empty tag value. Steps carrying a condition: {sorted(conditions)}"
    )
    actual = {name: conditions[name] for name in PINNED_STEP_CONDITIONS}
    assert actual == PINNED_STEP_CONDITIONS, (
        "a conditional push step's `if:` expression moved. pytest shows the "
        "difference below. This is what makes 'on a tag push only' true; the step "
        "NAME says so and nothing else asserted it."
    )


def test_NO_OTHER_run_STEP_HANDLES_A_SECRET(text: str) -> None:
    """🔴 THE GROW ARM, AND WITHOUT IT THE PIN ABOVE GUARDS ONE MEMBER OF A SET
    THAT CAN GROW.

    The pin's own docstring calls its subject *"the one credential-handling `run:`
    step"*. That is true today and nothing asserted it stays true — measured by an
    audit, which appended a second step:

        - name: annotate the release
          run: |
            curl -sS -H 'Authorization: Bearer ${{ secrets.GITHUB_TOKEN }}' …

    and watched **all 21 tests pass**. A live `GITHUB_TOKEN` in argv — visible in
    the process table and in any `RUNNER_DEBUG` trace, the precise hazard the pin
    below describes — arrived green, because the pinned step was unchanged and the
    pin has nothing to say about any other step.

    This is the discipline the same commit applied to the echo sites and not here:
    a ledger fails on GROW *or* SHRINK; a pin on one member fails only on SHRINK.

    ⚠ SCOPE: `run:` bodies only, which is where a secret reaches **argv**. A
    secret referenced from `env:` or a `with:` input is a different exposure with
    a different shape, is not what this asserts, and is not covered by anything
    here — said rather than left to be inferred from a green.
    """
    offenders = sorted(
        name for name, body in step_bodies(text).items()
        if "secrets." in body and name not in PINNED_CREDENTIAL_STEP
    )
    assert not offenders, (
        f"these `run:` steps reference a secret and are not the pinned credential "
        f"step: {offenders}. Every one is a place a token can reach argv. Either "
        f"fold the work into the pinned step, or add it to `PINNED_CREDENTIAL_STEP` "
        f"with its whole body — do not delete this assertion to make it quiet."
    )
    # The positive control: the pinned step itself MUST match the predicate, or
    # the comparison above is over an empty universe and would pass on a workflow
    # that handles no secrets at all — including one where the login was deleted.
    bodies = step_bodies(text)
    assert any(
        "secrets." in bodies[name] for name in PINNED_CREDENTIAL_STEP if name in bodies
    ), (
        "no pinned credential step references a secret, so the sweep above ran "
        "over nothing. The predicate or the login step has moved."
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
    # 🔴 THE `"EMPTY" in go` ASSERTION THAT USED TO STAND HERE WAS A SPELLED
    # GUARD, AND IT IS DELETED RATHER THAN WEAKENED. It read as "the Go control
    # refuses loudly on an empty ledger" and was satisfied by the refusal's own
    # sentence — `the route ledger is EMPTY` — so flipping that branch's `exit 1`
    # to `exit 0` left both words present and the mutant SURVIVED a green suite.
    # The step then printed a refusal and published anyway. The claim now lives
    # in `test_no_control_step_can_REFUSE_and_exit_ZERO`, which reads the BRANCH
    # on `$declared` and the exit STATEMENT rather than the words beside them.

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


# ---------------------------------------------------------------------------
# The four CONTROL steps, pinned the same way the four push steps are.
#
# 🔴 MEASURED: BEFORE THIS, THREE OF THE FILE'S MOST LOAD-BEARING STEPS WERE
# PINNED BY NOTHING, AND ALL THREE MUTANTS SURVIVED A FULLY GREEN SUITE.
#
#   * the Go proof's `ref` re-aimed at the PYTHON package — the shape a
#     copy-paste produces — left the job pushing the Go image, printing
#     `ANONYMOUS PULL OK` for `cairn-store`, and exiting 0. A ghcr package is
#     PRIVATE on first publish, so the one condition that step exists to
#     surface went invisible and the operator never learns to do the flip.
#   * the whole `/data`-is-empty block deleted, from EITHER pod's control. That
#     is the step this file's own prose calls "the control that matters most
#     here… a store entry baked into a layer is a private note published to the
#     internet", in a PUBLIC repository publishing to a PUBLIC registry, and it
#     was the only step in the workflow with no guard at all.
#   * the empty-ledger refusal's `exit 1` flipped to `exit 0`. The words
#     `REFUSING TO PUBLISH` and `EMPTY` both survive that edit, so the guard
#     that asserted them stayed green while the control printed a refusal and
#     published anyway.
#
# ⚠ AND THE GUARD THAT LOOKED LIKE COVERAGE WAS THE SPELLED KIND.
# `assert "EMPTY" in go` reads as "the Go control refuses on an empty ledger"
# and is satisfied by the refusal's own sentence, `the route ledger is EMPTY` —
# a word another part of the step can spell. It is replaced below by a branch
# assertion: a variable that EXISTS is not a guard, only a branch on it is.
#
# 🔴 SO: THE WHOLE NORMALISED BODY, BY EQUALITY, AND THE COST IS ACCEPTED. A
# cosmetic reformat of any of these four steps now fails this test. That is the
# price of a machine-readable claim about what the step DOES, and it is this
# repository's own recorded remedy: the `Env` guard was closed by pinning the
# whole normalised expression and deleting the key parsing, because teaching a
# parser one more spelling is how the next spelling arrives.
#
# ⚠ REGENERATE, DO NOT HAND-EDIT. `step_bodies(WORKFLOW.read_text())` is the
# authority; transcribing a thirty-line shell block by eye is how a pin ends up
# asserting a body nobody ships. Diff the regenerated value against this one and
# read the difference before accepting it.
PINNED_CONTROL_STEPS = {
    PYTHON_CONTROL_STEP: (
        'set -euo pipefail loaded=$(docker load -i "${{ steps.build.outputs'
        '.archive }}" | sed -n \'s/^Loaded image: //p\') echo "loaded = $load'
        'ed" test -n "$loaded" leaked=$(docker run --rm --entrypoint /bin/b'
        'usybox "$loaded" sh -c \'ls -A /data | wc -l\') if [ "$leaked" != "0'
        '" ]; then echo "REFUSING TO PUBLISH: /data in the image holds $lea'
        'ked entr(y|ies)." echo " This image is about to become PUBLIC. Sto'
        're content must arrive" echo " at runtime on a volume and must nev'
        'er be in a layer." docker run --rm --entrypoint /bin/busybox "$loa'
        'ded" sh -c \'ls -A /data\' exit 1 fi echo "control: /data holds $lea'
        'ked files (must be 0) — OK" py=$(docker inspect -f \'{{index .Confi'
        'g.Cmd 0}}\' "$loaded") echo "interpreter (from the image\'s own Cmd)'
        ' = $py" docker run --rm --entrypoint "$py" "$loaded" -c \'import sy'
        's; sys.path.insert(0, "/app/lib"); import subsystem_recall as r; p'
        'rint("positive control: subsystem_recall imported,", len(r.RECALL_'
        'MODES), "modes")\' set +e startup=$(docker run --rm "$loaded" 2>&1)'
        ' rc=$? set -e echo "no-config start: rc=$rc" echo "$startup" if [ '
        '"$rc" -eq 0 ]; then echo "REFUSING TO PUBLISH: the server exited 0'
        ' with no token source." echo " It is supposed to refuse. A 0 here '
        'means the entrypoint is not" echo " running the server at all." ex'
        'it 1 fi case "$startup" in *"subsystem-store-api:"*) ;; *) echo "R'
        'EFUSING TO PUBLISH: the refusal did not come from the server" exit'
        ' 1 ;; esac echo "control: the image runs and refuses by name — OK"'
    ),
    GO_CONTROL_STEP: (
        'set -euo pipefail loaded=$(docker load -i "${{ steps.build-go.outp'
        'uts.archive }}" | sed -n \'s/^Loaded image: //p\') echo "loaded = $l'
        'oaded" test -n "$loaded" leaked=$(docker run --rm --entrypoint /bi'
        'n/busybox "$loaded" sh -c \'ls -A /data | wc -l\') if [ "$leaked" !='
        ' "0" ]; then echo "REFUSING TO PUBLISH: /data in the Go image hold'
        's $leaked entr(y|ies)." echo " This image is about to become PUBLI'
        'C. Store content must arrive" echo " at runtime on a volume and mu'
        'st never be in a layer." docker run --rm --entrypoint /bin/busybox'
        ' "$loaded" sh -c \'ls -A /data\' exit 1 fi echo "control: /data hold'
        's $leaked files (must be 0) — OK" server=$(docker inspect -f \'{{in'
        'dex .Config.Cmd 0}}\' "$loaded") echo "server (from the image\'s own'
        ' Cmd) = $server" set +e routes=$(docker run --rm --entrypoint "$se'
        'rver" "$loaded" -routes) routes_rc=$? set -e declared=$(printf \'%s'
        '\\n\' "$routes" | grep -c . || true) echo "$routes" if [ "$routes_rc'
        '" -ne 0 ] || [ "$declared" -eq 0 ]; then echo "REFUSING TO PUBLISH'
        ': the route ledger is EMPTY, or the binary did" echo " not exit 0 '
        '(rc=$routes_rc, non-blank lines=$declared)." echo " A command that'
        ' printed nothing and exited 0 is indistinguishable" echo " from on'
        'e wired to nothing, and the /data zero above would then be" echo "'
        ' the only measurement left — which an image with no filesystem" ec'
        'ho " would also satisfy." exit 1 fi echo "positive control: the se'
        'rver ran, exited 0 and declared $declared ledger line(s) — OK" set'
        ' +e startup=$(docker run --rm "$loaded" 2>&1) rc=$? set -e echo "n'
        'o-config start: rc=$rc" echo "$startup" if [ "$rc" -eq 0 ]; then e'
        'cho "REFUSING TO PUBLISH: the Go server exited 0 with no token sou'
        'rce." echo " It is supposed to refuse. A 0 here means the entrypoi'
        'nt is not" echo " running the server at all." exit 1 fi case "$sta'
        'rtup" in *"subsystem-store-api:"*) ;; *) echo "REFUSING TO PUBLISH'
        ': the refusal did not come from the server" exit 1 ;; esac echo "c'
        'ontrol: the Go image runs and refuses by name — OK"'
    ),
    PYTHON_PROOF_STEP: (
        "set -euo pipefail ref='${{ steps.ref.outputs.image }}:${{ steps.re"
        "f.outputs.sha_tag }}' skopeo='${{ steps.skopeo.outputs.bin }}' set"
        ' +e $skopeo inspect --no-creds "docker://${{ steps.ref.outputs.ima'
        'ge }}:sha-0000000000000000000000000000000000000000" >/dev/null 2>&'
        '1 control_rc=$? set -e if [ "$control_rc" -eq 0 ]; then echo "REFU'
        'SING: an anonymous inspect of a tag that does not exist SUCCEEDED.'
        '" echo " This check cannot distinguish a public image from anythin'
        'g." exit 1 fi echo "negative control: absent tag refused anonymous'
        'ly (rc=$control_rc) — OK" set +e out=$($skopeo inspect --no-creds '
        '"docker://$ref" 2>&1) rc=$? set -e if [ "$rc" -ne 0 ]; then echo "'
        '$out" echo echo "REFUSING: $ref was PUSHED but cannot be pulled wi'
        'thout credentials." echo " A package created by Actions in a PUBLI'
        'C repo has been measured to" echo " come out PUBLIC, so this is NO'
        'T the expected first-publish state --" echo " something made this '
        'one private. GitHub exposes no REST route for" echo " the flip; it'
        ' is a ONE-TIME manual step, per package:" echo echo " https://gith'
        'ub.com/users/${{ github.repository_owner }}/packages/container/cai'
        'rn-store/settings" echo " -> Danger Zone -> Change visibility -> P'
        'ublic" echo echo " Until then no cluster outside this repository c'
        'an pull this image," echo " which is the entire reason this workfl'
        'ow exists." exit 1 fi echo "$out" | head -20 digest=$(printf \'%s\' '
        '"$out" | sed -n \'s/.*"Digest": "\\([^"]*\\)".*/\\1/p\' | head -1) echo'
        ' echo "ANONYMOUS PULL OK: $ref" echo "digest: $digest" echo "pin t'
        'his in a deployment: $ref"'
    ),
    GO_PROOF_STEP: (
        "set -euo pipefail ref='${{ steps.ref.outputs.image_go }}:${{ steps"
        ".ref.outputs.sha_tag }}' skopeo='${{ steps.skopeo.outputs.bin }}' "
        'set +e $skopeo inspect --no-creds "docker://${{ steps.ref.outputs.'
        'image_go }}:sha-0000000000000000000000000000000000000000" >/dev/nu'
        'll 2>&1 control_rc=$? set -e if [ "$control_rc" -eq 0 ]; then echo'
        ' "REFUSING: an anonymous inspect of a tag that does not exist SUCC'
        'EEDED." echo " This check cannot distinguish a public image from a'
        'nything." exit 1 fi echo "negative control: absent tag refused ano'
        'nymously (rc=$control_rc) — OK" set +e out=$($skopeo inspect --no-'
        'creds "docker://$ref" 2>&1) rc=$? set -e if [ "$rc" -ne 0 ]; then '
        'echo "$out" echo echo "REFUSING: $ref was PUSHED but cannot be pul'
        'led without credentials." echo echo " 🔴 IF THIS IS THE FIRST TIME '
        'THE GO PACKAGE HAS EVER PUBLISHED," echo " THIS FAILURE IS EXPECTE'
        'D. A ghcr package is PRIVATE on first" echo " publish and GitHub e'
        'xposes no REST route to change that. It is" echo " a ONE-TIME manu'
        'al step, per package:" echo echo " https://github.com/users/${{ gi'
        'thub.repository_owner }}/packages/container/cairn-store-go/setting'
        's" echo " -> Danger Zone -> Change visibility -> Public" echo echo'
        ' " Then re-run this workflow (workflow_dispatch). Until it is done'
        ', no" echo " cluster outside this repository can pull the Go pod."'
        ' exit 1 fi echo "$out" | head -20 digest=$(printf \'%s\' "$out" | se'
        'd -n \'s/.*"Digest": "\\([^"]*\\)".*/\\1/p\' | head -1) echo echo "ANON'
        'YMOUS PULL OK: $ref" echo "digest: $digest" echo "pin this in a de'
        'ployment: $ref"'
    ),
}


def test_every_control_step_is_pinned_WHOLE(text: str) -> None:
    """The two image controls and the two anonymous-pull proofs, by equality.

    These four steps are the whole of what this workflow ASSERTS before it makes
    two images public, and until now none of them was pinned by anything. A
    deleted block, a re-aimed reference and a softened refusal all left the suite
    green; each is a different string here.
    """
    bodies = step_bodies(text)
    missing = sorted(set(PINNED_CONTROL_STEPS) - set(bodies))
    assert not missing, (
        f"these control steps are absent: {missing}\nthe file's steps are: "
        f"{sorted(bodies)}\nEvery image this workflow publishes must carry its "
        "own controls; a deleted control step is a publish with no gate."
    )
    actual = {name: bodies[name] for name in PINNED_CONTROL_STEPS}
    assert actual == PINNED_CONTROL_STEPS, (
        "a control step's command text moved. Every difference is shown by "
        "pytest below. Read it rather than regenerating the constant: these are "
        "the steps that decide whether a store entry, a private package or a "
        "softened refusal reaches a PUBLIC registry."
    )


def test_each_anonymous_proof_inspects_ITS_OWN_package(text: str) -> None:
    """🔴 THE POSITIVE HALF IS PER-PACKAGE TOO, AND NOTHING USED TO SAY SO.

    `test_the_anonymous_check_carries_its_own_negative_control` counts the
    absent-tag inspects one per package — a claim about the reference that must
    NOT resolve. The reference that MUST resolve is reached through `$ref`, one
    indirection away, and was constrained by nothing: pointing the Go step's
    `ref` at `steps.ref.outputs.image` left this suite green while the job
    published the Go image and then proved the PYTHON one anonymously pullable.
    Both packages have separate visibility settings and the Go one has never
    published, so that is precisely the case the step exists to catch.
    """
    bodies = step_bodies(text)
    missing = sorted(set(PROOF_STEP_PACKAGES) - set(bodies))
    assert not missing, (
        f"these anonymous-pull proofs are absent: {missing}\nthe file's steps "
        f"are: {sorted(bodies)}"
    )

    for step, (expression, package) in sorted(PROOF_STEP_PACKAGES.items()):
        body = bodies[step]

        refs = proof_refs(body)
        wanted = expression + ":" + SHA_TAG_EXPRESSION
        assert refs == [wanted], (
            f"{step!r} inspects {refs}; it must inspect exactly [{wanted!r}] — "
            "its OWN package at the immutable tag this run just pushed. A proof aimed "
            "at the other package succeeds while the package it was supposed "
            "to certify stays private, and the failure then surfaces as "
            "`ImagePullBackOff` in a cluster that is not this one."
        )

        # …and NOTHING in that step may name the other package's expression.
        # The negative control and the settings URL are both per-package, and a
        # step that mixes them misdirects the operator who has to do the flip.
        other = [e for e in sorted(IMAGE_EXPRESSIONS) if e != expression]
        foreign = [e for e in other if e in body]
        assert not foreign, (
            f"{step!r} names {foreign}, which belong to the other package. The "
            "two are separate ghcr packages with separate visibility settings; "
            "an instrument proved honest about one has not been proved honest "
            "about the other."
        )

        urls = set(re.findall(r"packages/container/([\w-]+)/settings", body))
        assert urls == {package}, (
            f"{step!r} prints the settings page(s) for {sorted(urls)}; it must "
            f"print exactly {package!r}. GitHub exposes no REST route for the "
            "visibility flip, so this URL is the whole remedy — pointing it at "
            "the wrong package sends the operator to a page that is already "
            "public and leaves the one that is not."
        )


def test_both_image_controls_REFUSE_on_a_non_empty_data_directory(text: str) -> None:
    """🔴 THE CONTROL THAT MATTERS MOST HERE, AND IT WAS PINNED BY NOTHING.

    This repository is PUBLIC and this workflow pushes to a PUBLIC registry. A
    store entry baked into a layer is a private note published to the internet —
    the exact class `tests/leakscan.py` exists to stop, one step past its reach,
    because the leak gate reads the TREE and this reads the ARTEFACT.

    MEASURED: deleting the whole block from either pod's control left the suite
    16/16 green. The nearby `assert "EMPTY" in go` was not coverage — it is
    satisfied by the refusal text `the route ledger is EMPTY`, a word a
    different feature of the same step spells.

    Asserted as a PROBE plus a BRANCH: `leaked=$(…)` measuring something and
    nothing reading it is a step that runs the measurement and publishes anyway.
    """
    blocks = step_blocks(text)
    for step in IMAGE_CONTROL_STEPS:
        assert step in blocks, (
            f"the control step {step!r} is gone; the file's steps are "
            f"{sorted(blocks)}"
        )
        block = blocks[step]

        probes = re.findall(r"^\s*leaked=\$\(.*\n?", block, re.M)
        assert len(probes) == 1, (
            f"{step!r} makes {len(probes)} `/data` measurement(s); exactly one "
            "must set `leaked`"
        )
        assert re.search(r"ls -A /data \| wc -l", block), (
            f"{step!r} no longer counts the entries in `/data`. That count is "
            "the only thing standing between a store entry baked into a layer "
            "and a PUBLIC registry."
        )

        branches = guard_branches(block, "$leaked")
        assert len(branches) == 1, (
            f"{step!r} has {len(branches)} branch(es) on `$leaked`; exactly one "
            "must read the measurement. A variable that EXISTS is not a guard — "
            "only a branch on it is, and a step that counts `/data` and never "
            "reads the count publishes exactly as if it had not counted."
        )
        assert branches[0] and "0" not in branches[0], (
            f"{step!r}'s `/data` branch exits {branches[0]}; it must exit "
            "non-zero. Refusing on stdout and exiting 0 is a step that prints a "
            "refusal and publishes anyway."
        )


def test_no_control_step_can_REFUSE_and_exit_ZERO(text: str) -> None:
    """🔴 A REFUSAL IS AN EXIT CODE, NOT A SENTENCE.

    MEASURED: flipping the empty-ledger branch's `exit 1` to `exit 0` left both
    `REFUSING TO PUBLISH` and `EMPTY` present in the step and the suite 16/16
    green. Every guard over these steps was reading the MESSAGE; none was
    reading the ability to FAIL.

    A statement-level read rather than a substring one, because the Go control's
    own refusal PRINTS `not exit 0 (rc=$routes_rc, …)` — correct prose about the
    failure it caught. A sweep for those two characters would go red on it, and
    a guard that reds on a sentence nobody should change trains its reader to
    edit the sentence.
    """
    blocks = step_blocks(text)
    for step in IMAGE_CONTROL_STEPS + (PYTHON_PROOF_STEP, GO_PROOF_STEP):
        assert step in blocks, f"the step {step!r} is gone"
        codes = exit_statements(blocks[step])
        # The positive control: a step with no `exit` at all cannot violate the
        # assertion below, so "no `exit 0` here" would be true of a step that
        # had stopped refusing entirely.
        assert codes, (
            f"{step!r} contains no `exit` statement at all. It is a CONTROL: "
            "every branch it takes on a hazard has to be able to stop the "
            "publish, and a step that can only ever succeed is not one."
        )
        assert "0" not in codes, (
            f"{step!r} contains `exit 0`, and this guard refuses EVERY one of them "
            f"(it exits {codes}) rather than only the one on a hazard branch. The "
            "hazard is a control that finds the problem, prints a refusal and exits "
            "0 anyway — worse than no control, because the log says it checked. But "
            "`exit_statements` reads a TOKEN STREAM, not a control-flow graph, so it "
            "cannot tell which branch an `exit 0` sits on; refusing all of them is "
            "the safe direction and a plain trailing `exit 0` after the success "
            "message is caught too. That is not a false positive to argue with — "
            "DELETE the trailing `exit 0` and let the step fall off the end, which "
            "is what every other control step here does."
        )

    # …and the Go control's empty-ledger refusal specifically, which is the
    # branch the measured mutant softened. `$declared` is the ledger's line
    # count; the branch that reads it must be able to stop the run.
    ledger = guard_branches(blocks[GO_CONTROL_STEP], "$declared")
    assert len(ledger) == 1 and ledger[0] and "0" not in ledger[0], (
        f"the Go control's branch(es) on `$declared` exit {ledger}; exactly one "
        "branch must read the ledger's line count and exit non-zero. Without "
        "it, `/data holds 0 files` is the only measurement left — and a zero is "
        "what an image with no filesystem reports too."
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

    # 🔴 THE STATEMENT-LEVEL EXTRACTORS, OVER THE THREE MEASURED HOLES.
    #
    # `exit_statements` reads STATEMENTS. The discrimination that matters is the
    # Go control's own refusal text, which PRINTS `not exit 0 (…)` — a substring
    # sweep reds on that correct sentence, and a guard that reds on prose nobody
    # should change is a guard whose reader edits the prose.
    refusal = (
        'echo " not exit 0 (rc=$routes_rc, non-blank lines=$declared)."\n'
        "exit 1 ;;\n"
        "exit 0\n"
    )
    assert exit_statements(refusal) == ["1", "0"], (
        "the exit extractor must count the two STATEMENTS and must not count "
        "the `exit 0` inside the echoed sentence"
    )
    assert exit_statements('echo "exit 0"\n') == []

    # `guard_branches` sees a branch on the variable and the exits inside it —
    # and returns NOTHING when the variable is measured but never read, which is
    # the shape a deleted `/data` check leaves behind.
    branching = (
        'leaked=$(ls -A /data | wc -l)\n'
        'if [ "$leaked" != "0" ]; then\n'
        '  echo "REFUSING TO PUBLISH"\n'
        "  exit 1\n"
        "fi\n"
        'echo "done"\n'
        "exit 1\n"
    )
    assert guard_branches(branching, "$leaked") == [["1"]]
    assert guard_branches(branching, "$declared") == []
    # …the softened form: same words, same branch, and it publishes anyway.
    assert guard_branches(branching.replace("  exit 1\n", "  exit 0\n"), "$leaked") == [["0"]]
    # …and the measurement with nothing reading it. `leaked` still exists; the
    # step still prints a number; the branch that stops the publish is gone.
    assert guard_branches("leaked=$(ls -A /data | wc -l)\n", "$leaked") == []

    # `proof_refs` reads the reference a PROVE step actually inspects — the half
    # `absent_tag_inspects` cannot see, because it counts only the tags that must
    # NOT exist.
    assert proof_refs(
        "ref='${{ steps.ref.outputs.image }}:${{ steps.ref.outputs.sha_tag }}'"
    ) == ["${{ steps.ref.outputs.image }}:${{ steps.ref.outputs.sha_tag }}"]
    assert proof_refs("skopeo inspect --no-creds \"docker://$ref\"") == []


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
    assert step_blocks("") == {}
    assert absent_tag_inspects("") == []
    assert skopeo_nix_builds("") == []
    assert step_names("") == []
    assert exit_statements("") == []
    assert guard_branches("", "$leaked") == []
    assert proof_refs("") == []
