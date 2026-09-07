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


def dockerfile_user(text: str) -> str | None:
    m = re.search(r"^USER\s+(\S+)\s*$", text, re.M)
    return m.group(1) if m else None


def dockerfile_expose(text: str) -> str | None:
    m = re.search(r"^EXPOSE\s+(\d+)\s*$", text, re.M)
    return m.group(1) if m else None


def dockerfile_cmd_script(text: str) -> str | None:
    """The SCRIPT the CMD runs, ignoring which python resolves it.

    The Dockerfile says `python3` (resolved from the image's PATH); the flake
    names an absolute store path. Comparing the interpreter would pin a
    difference that is correct and intended, so only the script is compared.
    """
    m = re.search(r"^CMD\s+(\[.*\])\s*$", text, re.M)
    if not m:
        return None
    argv = json.loads(m.group(1))
    scripts = [a for a in argv if a.endswith(".py")]
    return scripts[-1] if scripts else None


def flake_attrset(text: str, name: str) -> dict[str, str]:
    """A `name = { K = "V"; … };` attribute set -> a dict."""
    m = re.search(rf"^\s*{re.escape(name)}\s*=\s*\{{(.*?)^\s*\}};", text, re.M | re.S)
    if not m:
        return {}
    return dict(re.findall(r'(\w+)\s*=\s*"([^"]*)"\s*;', m.group(1)))


def flake_int(text: str, name: str) -> str | None:
    m = re.search(rf"^\s*{re.escape(name)}\s*=\s*(\d+)\s*;", text, re.M)
    return m.group(1) if m else None


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
