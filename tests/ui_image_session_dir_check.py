"""Assert the built UI image OWNS its session directory. Driven by `checks.ui-image-owns-its-session-dir`.

Usage: ui_image_session_dir_check.py <image.tar.gz> <session-dir> <uid>

🔴 WHY THIS IS A FILE AND NOT A HEREDOC INSIDE `flake.nix`. Nix's `''…''` strings
interpolate `${…}`, which is Python's own f-string and shell's parameter syntax too — a
heredoc here means escaping `''${` at every mention and getting a silent wrong value when
one is missed. A real file is also runnable by hand against any image tarball, which is how
this was developed and how the next person will debug it.

🔴 THE NEGATIVE CONTROL RUNS BEFORE THE ASSERTION, AND IT IS NOT OPTIONAL. Everything here
reduces to "did the reader find this path in a layer", so a reader wired to nothing returns
None for the real path exactly as happily as for a missing one — and None would read as
"the image does not own its session directory", a confident FAIL for the wrong reason. Worse
in the other direction: were the assertion inverted, a broken reader would PASS. So the
first thing measured is that the reader returns absent for a path this image cannot contain,
and the script says so in its success line.
"""

from __future__ import annotations

import pathlib
import sys
import tarfile
import tempfile

#: A path no cairn image can contain. Used only as the negative control.
IMPOSSIBLE = "var/lib/definitely-not-in-this-image"


def owner_in_layer(layer: pathlib.Path, want: str) -> tuple[int, int] | None:
    """`(uid, gid)` of `want` in one layer tar, or None if it is not a member.

    The four spellings are the ones `dockerTools` actually emits across versions — a bare
    name, a trailing slash for a directory, and either with a `./` prefix. Matching one
    spelling is how this returns a confident None for an image that is perfectly correct.
    """
    names = {want, want + "/", "./" + want, "./" + want + "/"}
    with tarfile.open(layer) as tf:
        for info in tf:
            if info.name in names:
                return (info.uid, info.gid)
    return None


def owner_in_image(root: pathlib.Path, want: str) -> tuple[int, int] | None:
    """Walk every layer, newest wins — a later layer legitimately re-owns a path."""
    found: tuple[int, int] | None = None
    for layer in sorted(root.rglob("layer.tar")):
        got = owner_in_layer(layer, want)
        if got is not None:
            found = got
    return found


def main(argv: list[str]) -> int:
    if len(argv) != 4:
        print(f"usage: {argv[0]} <image.tar.gz> <session-dir> <uid>")
        return 2
    tarball, session_dir, uid_s = argv[1], argv[2], argv[3]
    uid = int(uid_s)
    member = session_dir.lstrip("/")

    with tempfile.TemporaryDirectory() as td:
        root = pathlib.Path(td)
        with tarfile.open(tarball) as tf:
            # `filter="data"` for 3.14's changed default. It cannot weaken the result:
            # these members are `layer.tar` FILES, and every ownership value read below
            # comes from tar headers INSIDE them, not from how they were extracted.
            tf.extractall(root, filter="data")

        layers = sorted(root.rglob("layer.tar"))
        if not layers:
            print("FAIL: the image tarball contains NO layer.tar, so this check read")
            print("      nothing and its verdict would be about the reader.")
            return 1

        # 🔴 NEGATIVE CONTROL, before the real read.
        if owner_in_image(root, IMPOSSIBLE) is not None:
            print(f"FAIL: the layer reader found {IMPOSSIBLE!r}, which cannot exist.")
            print("      It is not reading what it claims to read, so the assertion")
            print("      below would be meaningless in either direction.")
            return 1

        got = owner_in_image(root, member)

    if got is None:
        print(f"FAIL: {session_dir} appears in NO layer of the built image.")
        print("      The Deployment would mount over a path the image never created,")
        print("      leaving it root-owned, and `OpenFileSessionStore` then refuses to")
        print("      start (78) rather than serving badly.")
        return 1

    if got != (uid, uid):
        print(f"FAIL: {session_dir} is owned by {got} in the built image, not")
        print(f"      ({uid}, {uid}). The session table cannot be written, so the")
        print("      surface refuses to start (78).")
        return 1

    print(
        f"ok: {session_dir} is owned by {uid}:{uid} across {len(layers)} layer(s), "
        f"and the reader's negative control returned absent for {IMPOSSIBLE!r}"
    )
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
