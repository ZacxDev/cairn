"""The two ways to build the pod must agree about how the pod RUNS.

🔴 WHY THIS FILE EXISTS. `server/Dockerfile` is the build that is deployed
today. `flake.nix` adds a second one (`packages.server-image`) so the image can
be produced reproducibly and from a pinned revision. Two build paths for one
artefact is a real hazard: the runtime contract — which env vars are set, which
port is exposed, which uid it drops to, which script is the entrypoint — is
stated in BOTH, and nothing stops one from moving alone. A pod built one way
then differs from the pod built the other way in a manner that is invisible
until it is running in a cluster.

The MODULE SET is deliberately not checked here, because it cannot drift: the
Dockerfile enumerates `COPY lib/<mod>.py` (and
`test_subsystem_store_api.py::test_the_image_copies_every_module_it_needs`
keeps that enumeration honest against the real import closure), while the flake
copies all of `lib/` and so covers any closure by construction. There is
nothing for these two to disagree about. The runtime contract is the part that
IS enumerated twice, so the runtime contract is what this pins.

🔴 THIS TEST PARSES TWO FILE FORMATS, WHICH MAKES BOTH FORMATS A DEPENDENCY IT
DID NOT OTHERWISE HAVE. A parser that quietly matches nothing reports perfect
agreement — an empty set equals an empty set — and that reads exactly like a
pass. Every extraction below is therefore paired with a positive control
asserting it found something, and those controls are the reason a green here is
a measurement rather than a claim. `test_the_controls_can_fail` drives the
point home by running the same extractors over text that must NOT satisfy them.
"""
from __future__ import annotations

import json
import re
from pathlib import Path

import pytest

ROOT = Path(__file__).resolve().parents[1]
DOCKERFILE = ROOT / "server" / "Dockerfile"
FLAKE = ROOT / "flake.nix"


# ---------------------------------------------------------------------------
# Extractors. Each is a pure function of text so the controls below can drive
# the SAME code over hostile input — a control that re-implements the parser it
# is validating certifies nothing about the parser that ships.
# ---------------------------------------------------------------------------

def dockerfile_env(text: str) -> dict[str, str]:
    """`ENV K=V \\` continuation blocks -> a dict."""
    joined = re.sub(r"\\\n\s*", " ", text)
    out: dict[str, str] = {}
    for line in joined.splitlines():
        if not line.startswith("ENV "):
            continue
        for tok in line[4:].split():
            if "=" in tok:
                k, v = tok.split("=", 1)
                out[k] = v
    return out


# 🔴 THESE RETURN EVERY OCCURRENCE, NOT THE FIRST, AND THAT IS A BUG FIX.
# They used to be `re.search`, which yields the FIRST match — while Docker
# applies the LAST `USER` and the LAST `CMD`, and ALL `EXPOSE` lines. So an
# appended `USER root` (a debugging step somebody forgot to revert) left the
# original `USER 65532:65532` as the only line these functions could see: the
# suite reported "the uid agrees" while the deployed pod ran as ROOT against
# the PVC. Measured as mutants — `append-USER-root` and `append-second-CMD`
# both scored 11 passed before this change. Returning all occurrences lets the
# assertions below pin BOTH the effective value and the count.

def dockerfile_users(text: str) -> list[str]:
    return re.findall(r"^USER\s+(\S+)\s*$", text, re.M)


def dockerfile_exposes(text: str) -> list[str]:
    return re.findall(r"^EXPOSE\s+(\d+)\s*$", text, re.M)


def dockerfile_cmd_scripts(text: str) -> list[str]:
    """The SCRIPT each CMD runs, ignoring which python resolves it.

    The Dockerfile says `python3` (resolved from the image's PATH); the flake
    names an absolute store path. Comparing the interpreter would pin a
    difference that is correct and intended, so only the script is compared.
    """
    out: list[str] = []
    for raw in re.findall(r"^CMD\s+(\[.*\])\s*$", text, re.M):
        scripts = [a for a in json.loads(raw) if a.endswith(".py")]
        if scripts:
            out.append(scripts[-1])
    return out


def dockerfile_user(text: str) -> str | None:
    """The EFFECTIVE user — Docker applies the last `USER`."""
    users = dockerfile_users(text)
    return users[-1] if users else None


def dockerfile_expose(text: str) -> str | None:
    ports = dockerfile_exposes(text)
    return ports[-1] if ports else None


def dockerfile_cmd_script(text: str) -> str | None:
    """The EFFECTIVE CMD — Docker applies the last one."""
    cmds = dockerfile_cmd_scripts(text)
    return cmds[-1] if cmds else None


def flake_attrset(text: str, name: str) -> dict[str, str]:
    """A `name = { K = "V"; … };` attribute set -> a dict."""
    m = re.search(rf"^\s*{re.escape(name)}\s*=\s*\{{(.*?)^\s*\}};", text, re.M | re.S)
    if not m:
        return {}
    return dict(re.findall(r'(\w+)\s*=\s*"([^"]*)"\s*;', m.group(1)))


def flake_int(text: str, name: str) -> str | None:
    m = re.search(rf"^\s*{re.escape(name)}\s*=\s*(\d+)\s*;", text, re.M)
    return m.group(1) if m else None


def flake_image_block(text: str) -> str | None:
    """The `buildLayeredImage { … }` argument set, brace-matched.

    🔴 THE SCOPE IS THE POINT, AND MATCHING LINE-ANYWHERE WAS NOT ENOUGH. Two
    separate guards were defeated by a binding of the right NAME in the wrong
    PLACE — the defect is always "declared but not wired", and a regex over the
    whole file cannot tell the two apart:

      * a decoy `contents = …` in `mkServerImage`'s `let`, with the argument
        left as `[ tree ]` — closed by counting, but then
      * the single real binding MOVED into that `let` with NO argument passed:
        count is still 1, the old assertion still matched, and the built image
        had 24 layers, no `/bin`, no `/sbin` and an EMPTY `/app` — the container
        does not start at all (`can't open file '/app/server/server.py'`).

    Reading the argument block instead makes both spellings unrepresentable,
    and it is also what removes the FALSE REDS the line-matching version
    introduced: a multi-line list, or an extra `++ [ … ]` term, are ordinary
    formatting that produced a byte-identical derivation and a red test.
    """
    i = text.find("buildLayeredImage")
    if i == -1:
        return None
    try:
        j = text.index("{", i)
    except ValueError:
        return None
    depth = 0
    for k in range(j, len(text)):
        if text[k] == "{":
            depth += 1
        elif text[k] == "}":
            depth -= 1
            if depth == 0:
                return text[j:k + 1]
    return None


def flake_image_arg(text: str, name: str) -> str | None:
    """One `name = <value>;` argument from inside the image block."""
    block = flake_image_block(text)
    if block is None:
        return None
    m = re.search(rf"^\s*{re.escape(name)}\s*=\s*(.*?);\s*$", block, re.M | re.S)
    return m.group(1).strip() if m else None


def flake_cmd_script(text: str) -> str | None:
    m = re.search(r"Cmd\s*=\s*\[(.*?)\];", text, re.S)
    if not m:
        return None
    scripts = [s for s in re.findall(r'"([^"]*)"', m.group(1)) if s.endswith(".py")]
    return scripts[-1] if scripts else None


@pytest.fixture(scope="module")
def dockerfile() -> str:
    return DOCKERFILE.read_text(encoding="utf-8")


@pytest.fixture(scope="module")
def flake() -> str:
    return FLAKE.read_text(encoding="utf-8")


# ---------------------------------------------------------------------------
# The positive controls. These do not compare the two files at all; they assert
# that each extractor CAN see its subject. Without them every assertion in the
# next section is satisfiable by two empty results.
# ---------------------------------------------------------------------------

class TestTheExtractorsSeeSomething:
    """🔴 A ZERO HERE IS INDISTINGUISHABLE FROM AGREEMENT. Read these first."""

    def test_the_dockerfile_is_where_this_test_thinks_it_is(self):
        assert DOCKERFILE.is_file(), f"no Dockerfile at {DOCKERFILE}"
        assert FLAKE.is_file(), f"no flake.nix at {FLAKE}"

    def test_the_dockerfile_env_parse_is_not_empty(self, dockerfile):
        env = dockerfile_env(dockerfile)
        assert env, (
            "parsed ZERO env vars out of the Dockerfile — the ENV format moved "
            "and every agreement assertion below is now vacuous"
        )
        # Named explicitly: the import-time `Path.home()` in
        # `lib/subsystem_read_store.py` raises without it, so its loss is a
        # startup crash rather than a cosmetic difference.
        assert "HOME" in env

    def test_the_flake_env_parse_is_not_empty(self, flake):
        env = flake_attrset(flake, "serverEnv")
        assert env, (
            "parsed ZERO env vars out of flake.nix `serverEnv` — the attrset "
            "moved or was renamed, and this whole file is now vacuous"
        )
        assert "HOME" in env

    def test_the_scalar_parses_are_not_empty(self, dockerfile, flake):
        assert dockerfile_user(dockerfile) is not None, "no USER line parsed"
        assert dockerfile_expose(dockerfile) is not None, "no EXPOSE line parsed"
        assert dockerfile_cmd_script(dockerfile) is not None, "no CMD script parsed"
        assert flake_int(flake, "serverUid") is not None, "no serverUid parsed"
        assert flake_int(flake, "serverPort") is not None, "no serverPort parsed"
        assert flake_cmd_script(flake) is not None, "no flake Cmd script parsed"

    def test_the_controls_can_fail(self):
        """🔴 THE NEGATIVE CONTROL: the extractors must refuse wrong input.

        Every check above asserts a parser found something in the real file.
        That is only evidence if the same parser returns EMPTY on text where
        its subject is genuinely absent — otherwise it might be matching
        anything at all. This drives the shipped extractors, not a copy of
        them, because a re-implemented control drifts from the thing it
        validates and then certifies the drift.
        """
        assert dockerfile_env("FROM scratch\nRUN true\n") == {}
        assert dockerfile_user("FROM scratch\n") is None
        assert dockerfile_expose("FROM scratch\n") is None
        assert dockerfile_cmd_script('CMD ["python3"]\n') is None
        assert flake_attrset("{ other = { A = \"b\"; }; }", "serverEnv") == {}
        assert flake_int("{ serverUid = \"not-a-number\"; }", "serverUid") is None
        assert flake_cmd_script("{ Cmd = [ \"python3\" ]; }") is None


# ---------------------------------------------------------------------------
# The agreement itself.
# ---------------------------------------------------------------------------

class TestTheTwoBuildsAgree:

    def test_the_env_sets_are_identical(self, dockerfile, flake):
        """Both the NAMES and the VALUES, and equality in BOTH directions.

        A subset check would pass while the flake silently dropped a variable
        the Dockerfile sets — which is how `HOME` would go missing and turn
        into an import-time crash in a cluster rather than a red test here.
        """
        assert flake_attrset(flake, "serverEnv") == dockerfile_env(dockerfile)

    def test_the_uid_agrees(self, dockerfile, flake):
        user = dockerfile_user(dockerfile)
        uid = flake_int(flake, "serverUid")
        assert user == f"{uid}:{uid}", (
            f"Dockerfile drops to {user!r} but the flake image uses uid {uid!r}; "
            "a PVC written by one is then unreadable by the other"
        )

    def test_the_pod_does_not_run_as_root(self, dockerfile, flake):
        """🔴 THE VALUE, NOT THE AGREEMENT — AND THE SUITE LOST THIS FOR A ROUND.

        `test_the_uid_agrees` asserts the two builds say the SAME uid. It says
        nothing about WHICH uid, so both sides moving together satisfy it.
        An earlier version of the count guard also pinned the literal
        `65532:65532`, which meant a legitimate uid change failed with the
        message "expected exactly one USER line" — a complaint about a problem
        the tree did not have. Fixing that message DROPPED the literal instead
        of MOVING it, and for one commit no test in the repo asserted a
        non-root uid at all.

        MEASURED at `465f8a3`: `USER 0:0` in the Dockerfile plus
        `serverUid = 0;` in the flake ran the full suite to **1695 passed**.
        The pod mounts a PVC and a bearer token at
        `/run/secrets/subsystem-store/token`; running it as root is a real
        change that nothing observed.

        This test owns the VALUE and says so in its own message, so a
        deliberate uid change fails HERE, with an explanation that fits.
        """
        uid = flake_int(flake, "serverUid")
        assert uid is not None, "no serverUid parsed"

        # 🔴 THE IMAGE'S OWN `User`, NOT ONLY THE `let` BINDING — AND THIS WAS
        # THE GAP THAT MADE THIS TEST'S DOCSTRING FALSE FOR A COMMIT. It read
        # `serverUid` and nothing else; no test in the repo touched
        # `config.User`. MEASURED: `User = "0:0";` with `serverUid = 65532;`
        # left the file at 15 passed while the built image reported
        # `Config.User=0:0` and `docker run … id` returned `uid=0(root)`.
        # "This test owns the VALUE" was a claim about a binding, not about the
        # artefact. Same declared-but-not-wired class as `contents` above.
        user_arg = flake_image_arg(flake, "User")
        assert user_arg is not None, "no `User` argument in the image block"
        assert "serverUid" in user_arg, (
            f"the image's `User` is {user_arg!r} — it does not derive from "
            f"`serverUid`, so the binding this test checks and the uid the pod "
            f"actually runs as can differ, and only the binding is guarded"
        )

        assert uid != "0", (
            "the flake image would run the pod as ROOT. It mounts a PVC and a "
            "bearer token; the non-root uid is the containment. If this is "
            "deliberate, change it here and say why in the commit."
        )
        user = dockerfile_user(dockerfile)
        assert user is not None and not user.startswith("0:"), (
            f"server/Dockerfile drops to {user!r} — the pod would run as ROOT"
        )
        # The specific uid is pinned too: 65532 is the clawgate precedent's
        # runAsUser, which is what makes the fsGroup story on the PVC hold.
        assert uid == "65532", (
            f"serverUid is {uid!r}, not the 65532 the PVC's fsGroup story "
            f"depends on. Changing it means editing FOUR sites deliberately: "
            f"`serverUid` in flake.nix, `USER` in server/Dockerfile, that "
            f"file's `chown -R` line, and this assertion."
        )

    def test_the_dockerfile_chowns_the_uid_it_actually_drops_to(self, dockerfile):
        """🔴 A SECOND, INDEPENDENT LITERAL — and nothing read it.

        `server/Dockerfile` runs `chown -R 65532:65532 …` on one line and
        `USER 65532:65532` on another. They are unrelated tokens: MEASURED,
        setting the chown to `1000:1000` while `USER` stays `65532:65532` left
        the suite at 15 passed, and the deployed pod could write neither
        `/data` nor `$HOME`.

        This is the same shape as the uid gap above — a value the docstring
        claimed to own while the assertion read a different site — so it is
        closed here rather than left as a note saying it exists.
        """
        m = re.search(r"^RUN .*chown -R (\d+):(\d+)", dockerfile, re.M)
        assert m, "no `chown -R <uid>:<gid>` line found in server/Dockerfile"
        user = dockerfile_user(dockerfile)
        assert user == f"{m.group(1)}:{m.group(2)}", (
            f"the Dockerfile chowns to {m.group(1)}:{m.group(2)} but drops to "
            f"{user!r} — the pod would run as a uid that cannot write /data "
            f"or $HOME"
        )

    def test_the_port_agrees(self, dockerfile, flake):
        assert dockerfile_expose(dockerfile) == flake_int(flake, "serverPort")

    def test_the_exposed_port_agrees_with_the_env_the_server_actually_reads(
        self, dockerfile, flake
    ):
        """🔴 EXPOSE IS DOCUMENTATION; `SUBSYSTEM_STORE_PORT` IS THE BINDING.

        `server.py` takes its port from the env var. `EXPOSE` only annotates
        the image. They can disagree, and if they do the pod listens somewhere
        the manifest does not name — so the two are pinned together here rather
        than each being pinned only to its own side of the other file.
        """
        assert dockerfile_expose(dockerfile) == dockerfile_env(dockerfile)["SUBSYSTEM_STORE_PORT"]
        assert flake_int(flake, "serverPort") == flake_attrset(flake, "serverEnv")["SUBSYSTEM_STORE_PORT"]

    def test_the_entrypoint_script_agrees(self, dockerfile, flake):
        assert dockerfile_cmd_script(dockerfile) == flake_cmd_script(flake)

    def test_the_flake_image_declares_a_PATH_and_carries_the_operational_toolchain(
        self, flake
    ):
        """🔴 THE POD IS OPERATED THROUGH `kubectl exec`, AND THIS PINS THAT.

        The Dockerfile inherits a shell, a tar and a PATH from `python:*-slim`
        and needs no declaration. The flake image gets ONLY what `contents`
        names, and the first version named the code alone — so the built image
        had no `PATH` at all and no `sh`/`tar`/`find`/`cut`. It started, passed
        health checks and served, and every documented operational procedure
        against it failed:

          * `server/seed.sh` pushes the store with `kubectl exec … -- tar -xf -`
            and runs its containment guard through `sh -c`, so the pod COULD NOT
            BE SEEDED — and `server/README.md` says hand-seeding is the only path;
          * `server/README.md`'s token revocation is
            `kubectl exec … -- sh -c 'kill -HUP 1'`, so a LEAKED CREDENTIAL
            could not be revoked without deleting the pod.

        None of the other assertions in this file can see that: they pin
        env/uid/port/entrypoint, and all four agreed while the image was unfit
        for its own documented operation. This is a STRUCTURAL check on the
        flake source rather than on a built image, because building one takes
        minutes and this suite runs on every commit — so it pins the DECLARATION
        and `checks`/CI pin that the declaration builds.
        """
        assert re.search(r'^\s*serverPath\s*=\s*"[^"]+"\s*;', flake, re.M), (
            "flake.nix declares no `serverPath` — without a PATH in the image "
            "config, `kubectl exec … -- tar` fails with `executable file not "
            "found in $PATH` even when the binary is present"
        )
        m = re.search(r"^\s*serverTools\s*=\s*pkgs:\s*\[(.*?)\];", flake, re.M | re.S)
        assert m, "flake.nix declares no `serverTools`"
        assert "busybox" in m.group(1), (
            "the image carries no busybox, so it has no sh/tar/find/cut — "
            "seeding and token revocation both go through `kubectl exec`"
        )
        # And the PATH must actually be handed to the image config, not merely
        # defined: a declared-but-unused binding is the shape that reads as
        # covered while changing nothing.
        assert re.search(r"PATH\s*=\s*serverPath", flake), (
            "`serverPath` is defined but never placed into the image's Env"
        )

        # 🔴 THE SAME RULE APPLIED TO `serverTools`, AND ITS ABSENCE WAS
        # MEASURED. With only the assertions above, a mutant reverting
        # `contents = [ tree ] ++ serverTools pkgs;` to `contents = [ tree ];`
        # SURVIVED the whole suite — busybox declared, never installed, and the
        # round-1 🔴 fully restored behind a green test whose docstring is forty
        # lines about that exact failure. Declaring a binding is not wiring it.
        # 🔴 EXACTLY ONE `contents` BINDING, AND IT MUST BE THE WIRED ONE.
        # Anchoring alone was not enough and this was MEASURED: a `contents =
        # [ tree ] ++ serverTools pkgs;` added to `mkServerImage`'s `let`, with
        # the ARGUMENT to `buildLayeredImage` set to `contents = [ tree ];`,
        # left this file fully green — 14 passed — while the built image had 25
        # layers, root entries `['app','data','home']`, NO `/bin`, no busybox,
        # and `PATH=/bin` pointing at a directory that does not exist. That is
        # the round-1 🔴 restored: a pod that starts, serves, and cannot be
        # seeded or rotated.
        #
        # `re.search` takes the FIRST match, so a decoy binding anywhere above
        # the real one satisfies it. Counting is what closes that: two bindings
        # named `contents` is itself the defect, whichever one is right.
        contents = flake_image_arg(flake, "contents")
        assert contents is not None, (
            "no `contents` argument found inside the `buildLayeredImage { … }` "
            "block — the image is built from something this guard cannot see, "
            "so nothing here is evidence about what it ships"
        )
        # 🔴 A SUBSTRING TEST, DELIBERATELY, AND THE STRUCTURAL VERSION WAS
        # WORSE. Requiring `[ … ] ++ serverTools pkgs;` in one line reddened on
        # a multi-line list and on an extra `++ [ pkgs.cacert ]` term — both
        # ordinary edits producing a BYTE-IDENTICAL derivation, both reported
        # with a message naming a cause the tree did not have. Scoping to the
        # image block is what makes the loose test safe: inside it, `serverTools`
        # appearing in `contents` can only mean it is wired in.
        assert "serverTools" in contents, (
            f"the image's `contents` is {contents!r} — `serverTools` is not in "
            f"it, so busybox is not in the image: `kubectl exec … -- tar` fails "
            f"and the pod can be neither seeded nor rotated"
        )

        # 🔴 AND THE PATH MUST NAME WHERE THE APPLETS ACTUALLY LAND. A mutant
        # setting `serverPath = "/nonexistent"` ALSO survived: the tools were in
        # the image and unreachable, which is the same end state by another
        # route. `buildLayeredImage` places a package's `bin/` at the image root
        # as `/bin`, so that is the value this pins — deliberately a literal,
        # because the point is to fail when someone changes it without changing
        # the layout.
        #
        # 🔴 ANCHORED TO A LINE-START BINDING, AND THE UNANCHORED VERSION WAS
        # WRITTEN FIRST AND CAUGHT BY THIS FILE'S OWN MUTATION BATTERY. A bare
        # `re.search(r'serverPath\s*=\s*"([^"]+)"')` matched the FIRST
        # occurrence in the file — which is the COMMENT above the binding,
        # quoting `serverPath = "/nonexistent"` as the mutant it describes. So
        # the guard read a value out of prose and failed on a correct tree.
        # That is the same first-occurrence defect this class fixes in the
        # Dockerfile extractors, re-committed in the fix for it: a guard
        # matching a WORD that another line can spell, rather than the
        # STRUCTURE it means to read.
        #
        # ⚠ THE ANCHOR DEFEATS `#` COMMENTS ONLY, AND A WIDER CLAIM WAS MADE
        # FOR IT ONCE. Nix also has `/* … */` block comments and `''…''`
        # multi-line strings, and a line inside either still begins with the
        # binding text after `^\s*`, so both can still satisfy these regexes.
        # `#` is the only comment form this repo uses, which is why the anchor
        # is enough IN PRACTICE — that is a fact about the codebase, not about
        # the regex, and it is written here rather than asserted so nobody
        # reads the guard as stronger than it is.
        m2 = re.search(r'^\s*serverPath\s*=\s*"([^"]+)"\s*;', flake, re.M)
        assert m2 and m2.group(1) == "/bin", (
            f"serverPath is {m2.group(1) if m2 else None!r}, but busybox's "
            f"applets land at /bin in the built image — a PATH pointing "
            f"elsewhere leaves them present and unreachable"
        )

    def test_exactly_one_user_and_one_cmd_are_declared(self, dockerfile):
        """🔴 A SECOND `USER` OR `CMD` IS THE HAZARD, NOT A STYLE POINT.

        The comparisons above now read the LAST occurrence, which is what
        Docker applies — so a stray appended `USER root` changes the pod and
        the uid comparison correctly goes red. This pins the other half: two
        such lines are a mistake even when the last one happens to be right,
        because the file then says two different things and the next reader
        edits whichever they see first.
        """
        # 🔴 THE COUNT, NOT THE VALUE. `test_the_uid_agrees` owns whether the
        # uid is right; this owns whether there is exactly one of it. An earlier
        # version asserted the literal here too, so a legitimate uid change
        # failed with "expected exactly one USER line" — a message describing a
        # problem the tree did not have, which is how a correct change gets
        # reverted.
        assert len(dockerfile_users(dockerfile)) == 1, (
            f"expected exactly one USER line, found "
            f"{len(dockerfile_users(dockerfile))}: Docker applies the last, so "
            f"a second one silently changes which uid the pod drops to"
        )
        assert len(dockerfile_cmd_scripts(dockerfile)) == 1, (
            "expected exactly one CMD; Docker applies the last, so an extra "
            "one changes the entrypoint without changing the first"
        )

    def test_every_exposed_port_is_the_one_the_server_binds(self, dockerfile, flake):
        """ALL `EXPOSE` lines apply, so checking one of them is not enough."""
        exposed = dockerfile_exposes(dockerfile)
        assert exposed, "no EXPOSE parsed"
        port = flake_attrset(flake, "serverEnv")["SUBSYSTEM_STORE_PORT"]
        assert set(exposed) == {port}, (
            f"EXPOSE declares {sorted(set(exposed))} but the server binds {port} — "
            "an exposed port nothing listens on reads as a working route"
        )

    def test_the_entrypoint_is_a_file_that_exists(self, dockerfile):
        """The path is `/app/...` inside the image; check the repo half of it.

        A CMD naming a script neither build copies is a container that starts
        and immediately exits, and both builds would agree about it perfectly.
        """
        script = dockerfile_cmd_script(dockerfile)
        # Asserted rather than assumed: without this the next line raises
        # AttributeError on None, which reports as an ERROR about this test's
        # own code and buries the fact that the CMD line stopped parsing.
        assert script is not None, "no CMD script parsed — see the controls above"
        assert script.startswith("/app/")
        assert (ROOT / script[len("/app/"):]).is_file(), (
            f"CMD names {script}, which does not exist in this repo"
        )
