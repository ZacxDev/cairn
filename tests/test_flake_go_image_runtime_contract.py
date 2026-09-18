"""The GO pod's image must run under the SAME contract the Python pod does.

🔴 WHY THIS FILE EXISTS, AND WHY IT IS NOT A COPY OF ITS SIBLING.
`tests/test_flake_image_matches_dockerfile.py` pins `packages.server-image` against
`server/Dockerfile`, because those are two hand-written statements of one contract and
either can move alone. `packages.server-image-go` has no Dockerfile counterpart, so that
shape does not transfer: there is no second text to diff it against.

What CAN drift is the thing that actually matters to a deployment. A cluster running the
Python pod has a Deployment that sets `SUBSYSTEM_STORE_ROOT`, mounts a token at
`SUBSYSTEM_STORE_TOKEN_FILE`, targets `SUBSYSTEM_STORE_PORT` and expects uid 65532 on the
PVC. Swapping the image must not require editing any of that. So the relationship this
file pins is not "two files agree" but "the Go image's contract is DERIVED from the same
`serverEnv`/`serverUid`/`serverPort` bindings the Python pod's is, and the derivation is
a SUBTRACTION of a named Python-only set rather than a second copy".

🔴 THE SUBTRACTION IS THE LOAD-BEARING PART, AND A COPY WOULD PASS A WEAKER TEST.
An assertion that merely compared today's two env sets would stay green the day someone
replaced `builtins.removeAttrs serverEnv …` with a literal attrset holding the same three
values — and then go red months later, for the first person to add a shared variable, in
a file they did not touch. The direction of failure matters: a derived Go env gains what
`serverEnv` gains; a copied one silently does not, and the pod that comes up is missing
the variable its Deployment already sets. So the DERIVATION is asserted structurally, and
the values are asserted on top of it.

⚠ THESE ARE INVARIANT GUARDS, NOT REGRESSION COVERAGE, WITH ONE EXCEPTION NAMED BELOW.
No defect ever shipped from `mkGoServerImage`; it did not exist before the commit that
added this file. What they pin is that a class of defect THIS REPOSITORY HAS ALREADY
MEASURED on the sibling image — an image that starts, passes health checks, serves, and
cannot be seeded or rotated, behind four green assertions — cannot be reintroduced by the
second image. `test_the_go_image_carries_the_operational_toolchain_and_declares_a_PATH`
is the one with a measured ancestor: the mutants it exists for (`contents` reverted to the
code alone, `serverPath` pointing nowhere) each SURVIVED the whole suite on the Python
image, and both are reachable here by construction.

🔴 THIS IS A STRUCTURAL CHECK ON SOURCE, AND SAYING SO IS PART OF IT. It reads
`flake.nix`, not a built image, because building one takes minutes and this suite runs on
every commit — so it pins the DECLARATION, and CI pins that the declaration builds. What
it therefore CANNOT see is layer contents: whether busybox's applets really land at
`/bin`, whether the CA bundle is really at the path `SSL_CERT_FILE` names, whether the
binary really starts. Those were read off the built image by hand at the commit that added
this file, and the general lesson from the sibling stands — before deploying an image,
diff it for what the test cannot read.
"""
from __future__ import annotations

import re
from pathlib import Path

import pytest

# 🔴 THE SHIPPED EXTRACTORS, NEVER A COPY OF THEM. A control re-implemented from the
# instrument it validates drifts from it and then certifies the drift — that failure is on
# record in this project already. `conftest.py` puts `tests/` on `sys.path`, which is what
# makes the bare-name import work.
from test_flake_image_matches_dockerfile import (  # noqa: E402
    dockerfile_env,
    flake_attrset,
    flake_cmd_script,
    flake_image_arg,
    flake_image_block,
    flake_int,
)

ROOT = Path(__file__).resolve().parents[1]
FLAKE = ROOT / "flake.nix"
DOCKERFILE = ROOT / "server" / "Dockerfile"

#: The maker this file is about. Passed explicitly to every shared extractor: their
#: default is the PYTHON image, and a forgotten argument would make this whole module a
#: second, weaker copy of its sibling — green, and about the wrong artefact.
GO_MAKER = "mkGoServerImage"

#: The variables a Deployment sets and a pod must therefore honour, whichever
#: implementation is inside the image. Spelled out by hand rather than derived, because
#: this is the claim: these three are what a manifest names, and a derivation that
#: dropped one would satisfy any assertion computed from the derivation itself.
DEPLOY_CONTRACT = (
    "SUBSYSTEM_STORE_ROOT",
    "SUBSYSTEM_STORE_PORT",
    "SUBSYSTEM_STORE_TOKEN_FILE",
)


# ---------------------------------------------------------------------------
# Extractors for the two bindings this file adds. Pure functions of text, so the
# controls below can drive the SAME code over hostile input.
# ---------------------------------------------------------------------------

def flake_string_list(text: str, name: str) -> list[str] | None:
    """A `name = [ "a" "b" ];` nix list of strings.

    Returns None when the binding is absent, and `[]` when it is present but empty —
    two different facts that a bare list would collapse into one. The empty case is not
    hypothetical: `serverEnvPythonOnly = [ ];` is a perfectly valid edit that would hand
    the Go pod CPython's knobs, and "absent" is the edit that deletes the mechanism.
    """
    m = re.search(rf'^\s*{re.escape(name)}\s*=\s*\[(.*?)\]\s*;', text, re.M | re.S)
    if m is None:
        return None
    return re.findall(r'"([^"]*)"', m.group(1))


def flake_binding(text: str, name: str) -> str | None:
    """The right-hand side of a top-level `name = <expr>;` binding, as text.

    Anchored to a line start for the reason the sibling file's `serverPath` regex is:
    an unanchored search matched the COMMENT above the binding, which quotes the very
    mutant it warns about, and read a value out of prose.
    """
    m = re.search(rf"^\s*{re.escape(name)}\s*=\s*([^;]*);\s*$", text, re.M)
    return m.group(1).strip() if m else None


def go_env(text: str) -> dict[str, str] | None:
    """The env the Go image ships, computed the way `flake.nix` computes it.

    `serverEnv` minus `serverEnvPythonOnly`. Deliberately NOT parsed out of
    `mkGoServerImage`'s own `Env` argument: that argument is supposed to be the
    derivation and nothing else, and a parser that read it directly would be just as
    happy with a literal — which is the defect this module is about.
    """
    base = flake_attrset(text, "serverEnv")
    drop = flake_string_list(text, "serverEnvPythonOnly")
    if not base or drop is None:
        return None
    return {k: v for k, v in base.items() if k not in drop}


@pytest.fixture(scope="module")
def flake() -> str:
    return FLAKE.read_text(encoding="utf-8")


@pytest.fixture(scope="module")
def dockerfile() -> str:
    return DOCKERFILE.read_text(encoding="utf-8")


# ---------------------------------------------------------------------------
# The controls. A zero here is indistinguishable from agreement.
# ---------------------------------------------------------------------------

class TestTheExtractorsSeeSomething:
    """🔴 READ THESE FIRST. Every assertion below is satisfiable by two empty results."""

    def test_the_go_image_block_parses(self, flake):
        block = flake_image_block(flake, GO_MAKER)
        assert block, (
            f"no `buildLayeredImage {{ … }}` block found for `{GO_MAKER}` in flake.nix. "
            f"Either the maker was renamed or the image was removed — and with no block "
            f"to read, every assertion in this module is a claim about nothing."
        )

    def test_the_new_bindings_parse(self, flake):
        drop = flake_string_list(flake, "serverEnvPythonOnly")
        assert drop is not None, "no `serverEnvPythonOnly` list parsed out of flake.nix"
        assert drop, (
            "`serverEnvPythonOnly` is EMPTY. That is not a no-op: it hands the Go pod "
            "CPython's `PYTHONDONTWRITEBYTECODE`/`PYTHONUNBUFFERED` and a `HOME` "
            "nothing reads, which makes the image's env a false statement about what "
            "runs inside it."
        )
        assert flake_binding(flake, "serverEnvGo") is not None, (
            "no `serverEnvGo` binding parsed — the Go image's env comes from somewhere "
            "this module cannot see"
        )
        assert flake_binding(flake, "goServerTools") is not None, (
            "no `goServerTools` binding parsed"
        )

    def test_the_computed_go_env_is_not_empty(self, flake):
        env = go_env(flake)
        assert env, (
            "the computed Go env is EMPTY — `serverEnv` or `serverEnvPythonOnly` "
            "stopped parsing, and the agreement assertions below would compare two "
            "empty sets and report perfect agreement"
        )

    def test_the_controls_can_fail(self):
        """🔴 THE NEGATIVE HALF: the extractors must refuse input where the subject is
        genuinely absent. Without this, "it found something in the real file" is only
        evidence that it matches SOMETHING.

        Each fixture below is realistic rather than a textbook non-match — a parser that
        only refuses obvious garbage passes the edit that actually happens.
        """
        assert flake_string_list("{ serverEnvOther = [ \"A\" ]; }", "serverEnvPythonOnly") is None
        # Present but empty is `[]`, NOT None — the two mean different things and the
        # caller branches on the difference.
        assert flake_string_list("  serverEnvPythonOnly = [ ];\n", "serverEnvPythonOnly") == []
        assert flake_binding("  serverEnvGoNot = x;\n", "serverEnvGo") is None
        # A binding that exists reads back as its right-hand side, not as a bool.
        assert flake_binding("  serverEnvGo = builtins.removeAttrs serverEnv l;\n",
                             "serverEnvGo") == "builtins.removeAttrs serverEnv l"
        # And the composite: no `serverEnv` means no computed env, rather than `{}`
        # standing in for one.
        assert go_env("  serverEnvPythonOnly = [ \"HOME\" ];\n") is None


# ---------------------------------------------------------------------------
# The contract.
# ---------------------------------------------------------------------------

class TestTheGoImageRunsUnderTheSameContract:

    def test_the_go_env_is_DERIVED_from_serverEnv_rather_than_restated(self, flake):
        """🔴 THE STRUCTURE, NOT THE VALUES — AND THE VALUES TEST BELOW CANNOT SEE THIS.

        `serverEnvGo` must be a SUBTRACTION from `serverEnv`. Replacing it with a literal
        attrset holding today's three values would leave every value assertion in this
        module green, and would break silently on the NEXT shared variable: the Python
        pod would get it, the Go pod would not, and nothing here would notice until a pod
        came up missing env its Deployment sets.

        Two things are asserted, because either alone is walkable: that the binding names
        `serverEnv` (so it cannot be a standalone literal) and that it names the
        removal list (so it cannot be `serverEnv` renamed, which would ship CPython's
        knobs in a Go image).
        """
        binding = flake_binding(flake, "serverEnvGo")
        assert binding is not None, "no `serverEnvGo` binding — see the controls"
        assert re.search(r"\bserverEnv\b", binding), (
            f"`serverEnvGo` is {binding!r} — it does not derive from `serverEnv`, so the "
            f"two pods now state the runtime contract twice and either can move alone"
        )
        assert re.search(r"\bserverEnvPythonOnly\b", binding), (
            f"`serverEnvGo` is {binding!r} — it does not subtract `serverEnvPythonOnly`, "
            f"so what the Go image drops is decided somewhere this guard cannot read"
        )

    def test_the_removal_list_drops_NOTHING_the_deployment_names(self, flake):
        """🔴 THE ONE WAY THE SUBTRACTION CAN BE WRONG, AND IT IS THE DANGEROUS ONE.

        A derivation that subtracts too much is still a derivation: adding
        `SUBSYSTEM_STORE_TOKEN_FILE` to the list satisfies every structural assertion
        above and ships a pod that looks for its bearer token at the compiled-in default
        instead of the mounted path. So the list is checked against what a Deployment
        actually sets, in the direction that matters.
        """
        drop = flake_string_list(flake, "serverEnvPythonOnly")
        assert drop, "no `serverEnvPythonOnly` entries — see the controls"
        overlap = sorted(set(drop) & set(DEPLOY_CONTRACT))
        assert not overlap, (
            f"`serverEnvPythonOnly` drops {overlap} from the Go pod. Those are the "
            f"variables a Deployment SETS — dropping one means the pod falls back to a "
            f"compiled-in default while the manifest says otherwise, which is a pod that "
            f"starts and serves the wrong store, or reads the wrong token."
        )

    def test_the_go_env_carries_the_deployment_contract_with_the_python_values(
        self, flake, dockerfile
    ):
        """The values, against BOTH other statements of them, in both directions.

        A subset check against `serverEnv` alone would pass while `serverEnv` itself
        drifted from `server/Dockerfile`; that drift is its sibling's job, and pinning
        the Dockerfile here too is what makes "swapping the image needs no manifest
        edit" a claim about the deployed pod rather than about one nix binding.
        """
        env = go_env(flake)
        assert env, "the computed Go env is empty — see the controls"
        docker = dockerfile_env(dockerfile)
        assert docker, "the Dockerfile env parse is empty — see the sibling's controls"
        for name in DEPLOY_CONTRACT:
            assert name in env, (
                f"the Go image does not set {name}. A cluster swapping the image would "
                f"have to edit its Deployment, which is exactly what this contract exists "
                f"to make unnecessary."
            )
            assert env[name] == docker[name], (
                f"{name} is {env[name]!r} in the Go image and {docker[name]!r} in "
                f"server/Dockerfile — the two pods disagree about where the store, the "
                f"port or the token is"
            )

    def test_the_go_env_carries_NO_interpreter_variable(self, flake):
        """A Go binary reads none of CPython's knobs, and an env var nothing reads is a
        false statement about what is inside the image — the next operator debugging a
        buffering question would reason from it.

        ⚠ AN INVARIANT GUARD. Nothing ever shipped with them; this pins that the
        subtraction is doing its job in the other direction from the test above.
        """
        env = go_env(flake)
        assert env, "the computed Go env is empty — see the controls"
        leaked = sorted(k for k in env if k.startswith("PYTHON"))
        assert not leaked, (
            f"the Go image sets {leaked} — CPython environment variables in an image "
            f"that contains no interpreter"
        )
        assert "HOME" not in env, (
            "the Go image sets HOME. It is in `serverEnv` because "
            "`lib/subsystem_read_store.py` resolves `Path.home()` at import time; "
            "`go list -deps ./cmd/cairn-server` reaches no package that reads $HOME "
            "(every `os.UserHomeDir` call is under `internal/client`, which the server "
            "does not import). If that changed, move it out of `serverEnvPythonOnly` "
            "and say what now reads it."
        )

    def test_the_uid_derives_from_serverUid_and_is_not_root(self, flake):
        """🔴 THE IMAGE'S OWN `User`, NOT ONLY THE `let` BINDING.

        Its sibling records the measurement: `User = "0:0";` with `serverUid = 65532;`
        left that file green while the built image reported `Config.User=0:0` and
        `docker run … id` returned `uid=0(root)`. The binding and the artefact are two
        different claims, and only one of them is what runs.
        """
        uid = flake_int(flake, "serverUid")
        assert uid is not None, "no serverUid parsed"
        user_arg = flake_image_arg(flake, "User", GO_MAKER)
        assert user_arg is not None, (
            f"no `User` argument inside `{GO_MAKER}`'s image block"
        )
        assert re.search(r"\bserverUid\b", user_arg), (
            f"the Go image's `User` is {user_arg!r} — it does not derive from "
            f"`serverUid`, so the uid the PVC was chowned for and the uid this pod runs "
            f"as can differ with nothing to say so"
        )
        assert uid != "0", "the Go image would run the pod as ROOT"

    def test_the_exposed_port_derives_from_serverPort_and_matches_the_env(self, flake):
        """🔴 `ExposedPorts` IS DOCUMENTATION; `SUBSYSTEM_STORE_PORT` IS THE BINDING.

        `cmd/cairn-server/main.go` takes its port from the env var and `ExposedPorts`
        only annotates the image. They can disagree, and if they do the pod listens
        somewhere the Service does not name — so both are pinned, to each other and to
        the same `serverPort` the Python pod uses.
        """
        port = flake_int(flake, "serverPort")
        assert port is not None, "no serverPort parsed"
        exposed = flake_image_arg(flake, "ExposedPorts", GO_MAKER)
        assert exposed is not None, f"no `ExposedPorts` inside `{GO_MAKER}`'s block"
        assert re.search(r"\bserverPort\b", exposed), (
            f"the Go image's `ExposedPorts` is {exposed!r} — a literal port here can "
            f"drift from the one the server binds"
        )
        env = go_env(flake)
        assert env and env["SUBSYSTEM_STORE_PORT"] == port, (
            f"the Go pod binds {env['SUBSYSTEM_STORE_PORT'] if env else None!r} and the "
            f"image exposes serverPort={port!r}"
        )

    def test_the_entrypoint_is_the_GO_SERVER_and_not_an_interpreter(self, flake):
        """🔴 THE ONE ASSERTION THAT SAYS WHICH IMPLEMENTATION IS IN THE IMAGE.

        Everything else in this file would be equally satisfied by a second copy of the
        Python pod wearing the Go pod's name — same env, same uid, same port. The `Cmd`
        is what distinguishes them, and it is also where a copy-paste from
        `mkServerImage` lands.
        """
        cmd = flake_image_arg(flake, "Cmd", GO_MAKER)
        assert cmd is not None, f"no `Cmd` argument inside `{GO_MAKER}`'s image block"
        assert flake_cmd_script(flake, GO_MAKER) is None, (
            f"the Go image's `Cmd` is {cmd!r} and it names a `.py` script — this image "
            f"is supposed to exec the compiled server"
        )
        assert re.search(r"\bgoServer\b", cmd), (
            f"the Go image's `Cmd` is {cmd!r} — it does not name the `mkGoServer` "
            f"derivation, so what the container executes is decided somewhere this "
            f"guard cannot read"
        )
        # …and the package itself has to be IN the image, or the Cmd names a store path
        # the container does not carry: a container that does not start at all.
        contents = flake_image_arg(flake, "contents", GO_MAKER)
        assert contents is not None, f"no `contents` argument inside `{GO_MAKER}`'s block"
        assert re.search(r"\bgoServer\b", contents), (
            f"the Go image's `contents` is {contents!r} — the server package is not in "
            f"it, so `Cmd`'s absolute store path is not in the image either"
        )

    def test_the_go_image_carries_the_operational_toolchain_and_declares_a_PATH(
        self, flake
    ):
        """🔴 THE ONE GUARD HERE WITH A MEASURED ANCESTOR — read its sibling's story.

        `packages.server-image`'s first version shipped the code alone: no `PATH`, no
        `sh`/`tar`/`find`/`cut`. It started, passed health checks and served, and every
        documented operation against it failed — `server/seed.sh` seeds through
        `kubectl exec … -- tar -xf -` and `server/README.md` revokes a leaked credential
        with `kubectl exec … -- sh -c 'kill -HUP 1'`. Both procedures are the POD's, not
        the interpreter's, and both survive the port: `cmd/cairn-server/main.go` installs
        a `SIGHUP` handler and logs a reload verdict, so an image with no shell would
        take away a revocation path the binary still implements.

        Two mutants are on record as having SURVIVED the whole suite on the sibling
        image, and both are reachable here by construction — `contents` reverted to the
        code alone, and `serverPath` pointing at a directory the applets do not land in.
        Each is closed by its own assertion below, because closing one does not close
        the other: they reach the same end state (tools present and unreachable, or
        absent) by different routes.
        """
        # The tools reach `contents` — declaring a binding is not wiring it.
        contents = flake_image_arg(flake, "contents", GO_MAKER)
        assert contents is not None, f"no `contents` argument inside `{GO_MAKER}`'s block"
        assert re.search(r"\bgoServerTools\b", contents), (
            f"the Go image's `contents` is {contents!r} — `goServerTools` is not in it, "
            f"so the image has no sh/tar/find/cut: `kubectl exec … -- tar` fails and the "
            f"pod can be neither seeded nor rotated"
        )

        # …and `goServerTools` is the operational set, not an empty list wearing its name.
        tools = flake_binding(flake, "goServerTools")
        assert tools is not None, "no `goServerTools` binding parsed"
        assert re.search(r"\bserverTools\b", tools), (
            f"`goServerTools` is {tools!r} — it does not build on `serverTools`, so the "
            f"two pods' operational toolchains are stated twice and can diverge"
        )
        assert re.search(r"\bcacert\b", tools), (
            f"`goServerTools` is {tools!r} — no CA bundle. The image ships no `/etc`, so "
            f"`internal/identity/jwks.go`'s https fetch has no roots to verify against"
        )

        # The PATH is declared, reaches the image config, and names where the applets
        # actually land. The literal is deliberate: the point is to fail when somebody
        # changes the layout without changing this.
        env_arg = flake_image_arg(flake, "Env", GO_MAKER)
        assert env_arg is not None, f"no `Env` argument inside `{GO_MAKER}`'s block"
        assert re.search(r"PATH\s*=\s*serverPath", env_arg), (
            f"the Go image's `Env` is {env_arg!r} — `serverPath` is never placed into "
            f"it, so `kubectl exec … -- tar` fails with `executable file not found in "
            f"$PATH` even though busybox is in the image"
        )
        m = re.search(r'^\s*serverPath\s*=\s*"([^"]+)"\s*;', flake, re.M)
        assert m and m.group(1) == "/bin", (
            f"serverPath is {m.group(1) if m else None!r}, but busybox's applets land at "
            f"/bin in the built image — a PATH pointing elsewhere leaves them present "
            f"and unreachable"
        )
        # And the CA bundle has to be NAMED, not merely present: Go's crypto/x509 reads
        # `$SSL_CERT_FILE` first and then searches a list of system paths this image does
        # not have, so shipping the closure without the variable changes nothing.
        assert "SSL_CERT_FILE" in env_arg, (
            f"the Go image's `Env` is {env_arg!r} — no `SSL_CERT_FILE`. The bundle would "
            f"be in the image and unreachable, which is the `serverPath = \"/nonexistent\"` "
            f"mutant in a different costume"
        )

    def test_the_go_image_does_not_reuse_the_python_pod_name(self, flake):
        """Two images with one `name` is a tag collision in whatever registry gets both,
        and `docker load` of the second silently replaces the first.

        ⚠ AN INVARIANT GUARD. Nothing has ever published `server-image-go`; this pins
        that the day something does, it does not publish over the pod that is deployed.
        """
        py = flake_image_arg(flake, "name", "mkServerImage")
        go = flake_image_arg(flake, "name", GO_MAKER)
        assert py and go, f"image names did not parse: python={py!r} go={go!r}"
        assert py != go, (
            f"both images are named {py} — loading or pushing one would replace the "
            f"other under the same reference"
        )
