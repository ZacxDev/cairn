"""`packages.ui-image`'s runtime contract, pinned against the Go source it must agree with.

🔴 WHY THIS FILE EXISTS, AND WHY IT IS NOT A COPY OF ITS SIBLING.
`tests/test_flake_go_image_runtime_contract.py` pins the Go POD, and it can lean on
`server/Dockerfile` as a second independent statement of the contract — that is what
`tests/test_flake_image_matches_dockerfile.py` compares against. **The UI surface has no
Dockerfile and never will**, so there is no second statement to compare with, and the
duplication that actually exists is between `flake.nix`'s `uiPort`/`uiSessionDir` bindings
and the `defaultPort`/`defaultSessionFile` constants in `cmd/cairn-ui/main.go`. This module
reads BOTH and fails when either moves alone.

🔴 THE FAILURE THIS IS FOR IS MEASURED IN THIS REPOSITORY, ONE ARTEFACT OVER, and the
sibling module's header records it: an image that started, passed health checks and served
while every documented operation against it failed. The agreement test that was supposed to
cover it is blind to LAYER CONTENTS. So the two assertions here that matter most are not
about the config block at all — they are that the session directory EXISTS IN A LAYER and
is OWNED by the run-as uid, which is the one thing this image needs that the pod's does not.

⚠ WHAT THIS MODULE CANNOT SEE, stated because a green run here reads wider than it is:
  * It reads `flake.nix` as TEXT and the built image's metadata as JSON. It does not run
    the container, so "the surface serves" is not among its claims.
  * It cannot tell you the CA bundle is reachable or that TLS egress works. The bundle is
    asserted NAMED rather than merely present, which is a strictly weaker claim than
    working, and `flake.nix`'s own comment records that the pod's equivalent was measured
    INERT in both directions.
  * The session-directory ownership assertion needs a BUILT image. When the build is
    unavailable those tests SKIP, and a skip nobody counts is a pass — so
    `test_the_layer_reader_can_go_red` is a negative control that runs unconditionally and
    proves the reader can fail at all.
"""

from __future__ import annotations

import re
import subprocess
import tarfile
from pathlib import Path

import pytest

ROOT = Path(__file__).resolve().parents[1]
FLAKE = ROOT / "flake.nix"
UI_MAIN = ROOT / "cmd" / "cairn-ui" / "main.go"

#: The maker this file is about, named once.
UI_MAKER = "mkGoUIImage"

#: The image's ghcr/OCI name. 🔴 PINNED AS A WHOLE STRING against BOTH pod names, because
#: the hazard is not a typo — it is publishing the UI over a pod's package, which would
#: replace a deployed artefact with one that does not serve `/api/v1/...` at all.
UI_IMAGE_NAME = "cairn-ui"
POD_IMAGE_NAMES = ("cairn-store", "cairn-store-go")

#: The ONLY `Env` expression `mkGoUIImage` may hand `buildLayeredImage`,
#: whitespace-normalised. Same reasoning as the sibling module's `GO_IMAGE_ENV_FORM`: a KEY
#: SET is not enough, because `//` is right-biased and an override naming the right keys
#: with wrong values satisfies every key-based assertion while shipping a broken surface.
#:
#: ⚠ IT IS A BARE ATTRSET, NOT A `//` OVER A SHARED BINDING, and that asymmetry with the pod
#: is deliberate rather than an oversight: `serverEnvGo` is `{ }` today, so `serverEnvGo //
#: { … }` here would read as inheriting a contract while inheriting nothing, and it would
#: silently start inheriting CPython knobs the day somebody adds one to `serverEnv` for the
#: Python pod. This surface has no interpreter.
UI_IMAGE_ENV_FORM = (
    'pkgs.lib.mapAttrsToList (k: v: "${k}=${v}") { PATH = serverPath; '
    "SSL_CERT_FILE = goServerCaBundle pkgs; }"
)


# ---------------------------------------------------------------------------
# Extractors. Pure functions of text so the controls below drive the SAME code
# over hostile input.
# ---------------------------------------------------------------------------

def _norm(s: str) -> str:
    """Collapse whitespace, so a reformat is not a failure but a reword is."""
    return re.sub(r"\s+", " ", s).strip()


def flake_int(text: str, name: str) -> int | None:
    m = re.search(rf"^\s*{re.escape(name)}\s*=\s*(\d+)\s*;", text, re.M)
    return int(m.group(1)) if m else None


def flake_str(text: str, name: str) -> str | None:
    m = re.search(rf'^\s*{re.escape(name)}\s*=\s*"([^"]*)"\s*;', text, re.M)
    return m.group(1) if m else None


def go_const_int(text: str, name: str) -> int | None:
    m = re.search(rf"^\s*{re.escape(name)}\s*=\s*(\d+)\s*$", text, re.M)
    return int(m.group(1)) if m else None


def go_const_str(text: str, name: str) -> str | None:
    m = re.search(rf'^\s*{re.escape(name)}\s*=\s*"([^"]*)"\s*$', text, re.M)
    return m.group(1) if m else None


def maker_block(text: str, maker: str) -> str | None:
    """The text of `<maker> = pkgs: … ;` up to the next top-level binding.

    Bounded by the next line at the SAME indentation that opens a binding, which is how
    the sibling module bounds its own and is why a nested `config = { … }` does not end it.
    """
    m = re.search(rf"^(\s*){re.escape(maker)}\s*=\s*pkgs:", text, re.M)
    if not m:
        return None
    indent = m.group(1)
    rest = text[m.end():]
    end = re.search(rf"^{indent}(?:[a-zA-Z_][\w-]*\s*=|in\b)", rest, re.M)
    return rest[: end.start()] if end else rest


@pytest.fixture(scope="module")
def flake() -> str:
    return FLAKE.read_text(encoding="utf-8")


@pytest.fixture(scope="module")
def ui_main() -> str:
    return UI_MAIN.read_text(encoding="utf-8")


@pytest.fixture(scope="module")
def ui_block(flake: str) -> str:
    block = maker_block(flake, UI_MAKER)
    assert block is not None, (
        f"{UI_MAKER} does not parse out of flake.nix. Every assertion below would then "
        f"be vacuous, so this is a hard failure rather than a skip."
    )
    return block


# ---------------------------------------------------------------------------
# Instrument controls. 🔴 These run FIRST and unconditionally: every assertion
# below reads a value out of text, and a reader wired to nothing returns None
# for a hostile input just as happily as for a correct one.
# ---------------------------------------------------------------------------

class TestTheExtractorsSeeSomething:
    def test_the_ui_bindings_parse(self, flake: str) -> None:
        assert flake_int(flake, "uiPort") is not None, "uiPort does not parse"
        assert flake_str(flake, "uiSessionDir") is not None, "uiSessionDir does not parse"

    def test_the_go_constants_parse(self, ui_main: str) -> None:
        assert go_const_int(ui_main, "defaultPort") is not None
        assert go_const_str(ui_main, "defaultSessionFile") is not None

    def test_the_maker_block_parses_and_is_bounded(self, ui_block: str) -> None:
        assert "buildLayeredImage" in ui_block
        # BOUNDED: the pod's maker must not have been swallowed, or assertions about
        # "the UI image" would be reading the pod's text and passing for it.
        assert "mkGoServerImage" not in ui_block, (
            "the UI maker block ran past its own end and absorbed the pod's — every "
            "assertion scoped to this block is then about the wrong artefact"
        )

    def test_the_controls_can_fail(self) -> None:
        """🔴 NEGATIVE CONTROL on every extractor at once. Without this, a reader that
        returns None for all input makes the whole module green by never finding a
        violation."""
        assert flake_int("uiPort = notanumber;", "uiPort") is None
        assert flake_str("uiSessionDir = 42;", "uiSessionDir") is None
        assert go_const_int("defaultPort = eight", "defaultPort") is None
        assert go_const_str("defaultSessionFile = nope", "defaultSessionFile") is None
        assert maker_block("mkSomethingElse = pkgs:", UI_MAKER) is None


# ---------------------------------------------------------------------------
# The duplication this file exists for.
# ---------------------------------------------------------------------------

class TestTheFlakeAgreesWithTheGoSource:
    """🔴 THE ONE THING NO OTHER GATE CHECKS. There is no Dockerfile here, so if these
    two drift the image exposes a port the binary does not bind, or owns a directory the
    binary does not use — and the container still starts."""

    def test_uiPort_equals_the_binarys_own_default(
        self, flake: str, ui_main: str
    ) -> None:
        flake_port = flake_int(flake, "uiPort")
        go_port = go_const_int(ui_main, "defaultPort")
        assert flake_port == go_port, (
            f"flake.nix exposes {flake_port} and cmd/cairn-ui/main.go binds {go_port}. "
            f"The container would start and expose a port nothing listens on. Move both "
            f"or neither."
        )

    def test_uiSessionDir_is_the_DIRNAME_of_the_binarys_session_file(
        self, flake: str, ui_main: str
    ) -> None:
        flake_dir = flake_str(flake, "uiSessionDir")
        go_file = go_const_str(ui_main, "defaultSessionFile")
        assert flake_dir is not None and go_file is not None
        assert flake_dir == str(Path(go_file).parent), (
            f"the image creates and owns {flake_dir!r} but the binary writes its session "
            f"table to {go_file!r}. The directory the deployment mounts would be the "
            f"wrong one, and `OpenFileSessionStore` refuses at startup (78) — so this "
            f"ships an image that cannot come up."
        )

    def test_the_session_dir_is_NOT_under_the_store_root(
        self, flake: str, ui_main: str
    ) -> None:
        """A live session table inside the volume whose documented operations are
        `enumerate` and `overwrite wholesale`. `cmd/cairn-ui/main.go` carries the long
        form; this pins it so a "simplification" onto one volume fails here."""
        session_dir = flake_str(flake, "uiSessionDir")
        store_root = go_const_str(ui_main, "defaultStore")
        assert session_dir is not None and store_root is not None
        assert not session_dir.startswith(store_root.rstrip("/") + "/"), (
            f"the session directory {session_dir!r} is under the store root "
            f"{store_root!r}; `server/seed.sh` replaces that volume's contents wholesale"
        )


class TestTheUIImageRunsUnderTheSameContractAsThePod:
    def test_the_uid_derives_from_serverUid_and_is_not_root(
        self, flake: str, ui_block: str
    ) -> None:
        assert "${toString serverUid}:${toString serverUid}" in _norm(ui_block), (
            "the UI image must DERIVE its uid from serverUid rather than restate a "
            "number — a second literal is how one image ends up running as root"
        )
        uid = flake_int(flake, "serverUid")
        assert uid is not None and uid != 0

    def test_the_exposed_port_derives_from_uiPort(self, ui_block: str) -> None:
        assert '"${toString uiPort}/tcp"' in _norm(ui_block), (
            "ExposedPorts must be derived from uiPort, not written as a literal"
        )
        assert "serverPort" not in ui_block, (
            "the UI image names serverPort — that is the POD's port (8102) and exposing "
            "it here would describe a surface nothing binds"
        )

    def test_the_env_is_EXACTLY_the_declared_expression(self, ui_block: str) -> None:
        norm = _norm(ui_block)
        assert UI_IMAGE_ENV_FORM in norm, (
            "the Env expression moved. A key-set check cannot bound this: `//` is "
            "right-biased, so an override naming the right keys with wrong values passes "
            "every other assertion here. Update UI_IMAGE_ENV_FORM deliberately, in the "
            "same commit, or revert the change."
        )

    def test_the_ca_bundle_is_NAMED_rather_than_merely_present(
        self, ui_block: str
    ) -> None:
        assert "goServerCaBundle pkgs" in ui_block, (
            "SSL_CERT_FILE must point at the named bundle derivation. A bundle that is "
            "merely in `contents` is reachable by accident of root-merging and moves "
            "when the layer set does."
        )

    def test_the_entrypoint_is_the_UI_BINARY_by_absolute_store_path(
        self, ui_block: str
    ) -> None:
        norm = _norm(ui_block)
        assert 'Cmd = [ "${pkgs.lib.getExe goUI}" ]' in norm, (
            "Cmd must name the UI binary by absolute store path. `/bin/cairn-ui` also "
            "resolves, which is the reason not to use it: the entrypoint would then "
            "depend on `contents` placing bin/ at the image root AND on PATH."
        )
        # 🔴 WORD-BOUNDED, AND A BARE SUBSTRING WAS MEASURED WRONG HERE. `goServer` is a
        # PREFIX of `goServerTools` and `goServerCaBundle`, both of which this block
        # legitimately names — so `"goServer" not in block` fails on a correct image and
        # would have been "fixed" by deleting the assertion. `\b` after the name requires
        # a non-word character, which `T` and `C` are not.
        assert re.search(r"getExe\s+goServer\b", ui_block) is None, (
            "the UI image's entrypoint names the POD binary — it would publish the pod "
            "under the UI's image name"
        )

    def test_the_image_does_not_reuse_either_pod_name(self, ui_block: str) -> None:
        m = re.search(r'name\s*=\s*"([^"]+)"', ui_block)
        assert m is not None, "the UI image declares no name"
        assert m.group(1) == UI_IMAGE_NAME
        assert m.group(1) not in POD_IMAGE_NAMES, (
            f"the UI image is named {m.group(1)!r}, which is a POD's package; publishing "
            f"it would replace a deployed pod with a surface that serves no /api/v1 route"
        )

    def test_it_declares_a_PATH_and_carries_the_toolchain(self, ui_block: str) -> None:
        """Same reason as the pod: operational procedures run through `kubectl exec …
        -- sh -c`, so an image without a shell removes a path the binary still supports."""
        assert "PATH = serverPath" in _norm(ui_block)
        assert "goServerTools pkgs" in ui_block


class TestTheSessionDirectoryIsCreatedAndOwned:
    """🔴 THE ASSERTION THE SIBLING MODULE HAS NO EQUIVALENT OF, because the pod writes
    nothing. This is read out of the BUILT IMAGE's layers rather than out of `flake.nix`,
    because the sibling module's header records an image whose config block was correct
    and whose LAYER CONTENTS were not."""

    def test_the_fakeRootCommands_create_AND_chown_the_session_dir(
        self, ui_block: str
    ) -> None:
        norm = _norm(ui_block)
        assert "mkdir -p .${uiSessionDir}" in norm, "the session dir is not created"
        assert (
            "chown -R ${toString serverUid}:${toString serverUid} .${uiSessionDir}"
            in norm
        ), (
            "the session dir is created but not chowned. `OpenFileSessionStore` refuses "
            "to start (78) when it cannot write there — measured — so this ships an "
            "image that cannot come up rather than one that serves badly."
        )

    def test_the_layer_reader_can_go_red(self, tmp_path: Path) -> None:
        """🔴 NEGATIVE CONTROL, and it runs whether or not a build is available. The test
        below SKIPS without one, and a skip nobody counts is a pass."""
        empty = tmp_path / "empty.tar"
        with tarfile.open(empty, "w"):
            pass
        assert _owner_of_dir_in_tar(empty, "var/lib/cairn-ui") is None

    def test_the_built_image_owns_the_session_dir(self, flake: str) -> None:
        session_dir = flake_str(flake, "uiSessionDir")
        uid = flake_int(flake, "serverUid")
        assert session_dir is not None and uid is not None
        tarball = _build_ui_image()
        if tarball is None:
            pytest.skip("nix build .#ui-image unavailable in this environment")
        assert tarball is not None
        owner = _owner_in_image(tarball, session_dir.lstrip("/"))
        assert owner is not None, (
            f"{session_dir} appears in NO layer of the built image. The deployment would "
            f"mount over a path the image never created, and ownership would be root's."
        )
        assert owner == (uid, uid), (
            f"{session_dir} is owned by {owner} in the built image, not ({uid}, {uid}). "
            f"The surface refuses to start."
        )


# ---------------------------------------------------------------------------
# Helpers that touch the filesystem, kept below the pure ones.
# ---------------------------------------------------------------------------

def _owner_of_dir_in_tar(tar_path: Path, member: str) -> tuple[int, int] | None:
    """`(uid, gid)` of `member` in one layer tar, or None if it is not there."""
    wanted = {member, member + "/", "./" + member, "./" + member + "/"}
    try:
        with tarfile.open(tar_path) as tf:
            for info in tf:
                if info.name in wanted or info.name.rstrip("/") in {
                    member,
                    "./" + member,
                }:
                    return (info.uid, info.gid)
    except tarfile.TarError:
        return None
    return None


def _build_ui_image() -> Path | None:
    try:
        out = subprocess.run(
            ["nix", "build", ".#ui-image", "--no-link", "--print-out-paths"],
            cwd=str(ROOT),
            capture_output=True,
            text=True,
            timeout=1800,
        )
    except (OSError, subprocess.TimeoutExpired):
        return None
    if out.returncode != 0:
        return None
    # 🔴 READ THE LAST LINE, NOT THE WHOLE CAPTURE. A derivation with several outputs
    # prints several paths — the measured `nixpkgs#skopeo` case in publish-image.yml,
    # where `man` came FIRST and `Invalid format` followed.
    paths = [p for p in out.stdout.split() if p.startswith("/nix/store/")]
    if len(paths) != 1:
        return None
    p = Path(paths[0])
    return p if p.is_file() else None


def _owner_in_image(tarball: Path, member: str) -> tuple[int, int] | None:
    """Walk every layer of an OCI tarball for `member`, newest layer wins."""
    import tempfile

    with tempfile.TemporaryDirectory() as td:
        root = Path(td)
        try:
            with tarfile.open(tarball) as tf:
                # `filter="data"` because 3.14 changes the default and warns here now.
                # ⚠ IT DOES NOT WEAKEN THE OWNERSHIP ASSERTION, and the reason is worth
                # stating because "data drops uid/gid" is true and reads as fatal: what
                # this extracts is the OUTER tarball, whose members are `layer.tar` FILES.
                # The ownership read below opens each layer and reads `info.uid` out of
                # its own tar headers — bytes inside the file, untouched by how the file
                # itself was extracted.
                tf.extractall(root, filter="data")  # noqa: S202 — our own build output
        except tarfile.TarError:
            return None
        found: tuple[int, int] | None = None
        for layer in sorted(root.rglob("layer.tar")):
            owner = _owner_of_dir_in_tar(layer, member)
            if owner is not None:
                found = owner
        return found
