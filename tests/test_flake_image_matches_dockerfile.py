"""The two ways to build the pod must agree about how the pod RUNS.

🔴 WHY THIS FILE EXISTS. ⚠ This read "`server/Dockerfile` is the build that is
deployed today" until the Go cutover, and that was the stated reason for the
whole file. NEITHER Python image is deployed now — the cluster pulls
`cairn-store-go`. The reason that survives is narrower and is stated here rather
than inferred: `flake.nix` builds a second Python image (`packages.server-image`)
reproducibly and from a pinned revision, and two build paths for one
artefact is a real hazard: the runtime contract — which env vars are set, which
port is exposed, which uid it drops to, which script is the entrypoint — is
stated in BOTH, and nothing stops one from moving alone. A pod built one way
then differs from the pod built the other way in a manner that is invisible
until it is running in a cluster.

🔴 AND THIS FILE IS TWO GUARDS, ONLY ONE OF WHOSE PREMISES DIED. The agreement
half above is about two images nothing deploys. But
`test_both_implementations_resolve_the_deployment_contract_with_no_env` reads
`cmd/cairn-server/main.go` — the DEPLOYED pod — and pins its defaults against
what a Deployment assumes. That half is LIVE. P8 SPLITS this file; it does not
delete it.

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
import sys
from pathlib import Path

import pytest

ROOT = Path(__file__).resolve().parents[1]
DOCKERFILE = ROOT / "server" / "Dockerfile"
FLAKE = ROOT / "flake.nix"
SERVER_PY = ROOT / "server" / "server.py"
GO_SERVER = ROOT / "cmd" / "cairn-server" / "main.go"

#: The runtime contract a Deployment relies on: where the store is, which port the pod
#: binds, where the bearer token is read from.
#:
#: 🔴 IT IS PINNED AGAINST THE CODE DEFAULTS BECAUSE NEITHER IMAGE STATES IT ANY MORE, AND
#: THAT IS THE WHOLE POINT OF THIS CONSTANT. Both images used to set
#: `SUBSYSTEM_STORE_ROOT=/data`, `SUBSYSTEM_STORE_PORT=8102` and
#: `SUBSYSTEM_STORE_TOKEN_FILE=/run/secrets/subsystem-store/token` in their `ENV`. Every
#: one of those values was byte-identical to the default the server already falls back to,
#: so they configured nothing while costing three deprecation warnings at every pod start
#: (the resolver sweeps the whole process environment, and an image `ENV` is part of it).
#: They are gone. `docker inspect` therefore no longer documents the contract — the
#: accepted cost, recorded beside both `ENV` blocks — so the contract is pinned HERE,
#: against the two implementations' own constants, in both directions.
#:
#: 🔴 SPELLED OUT BY HAND, NEVER DERIVED FROM EITHER IMPLEMENTATION. A value computed from
#: `server.py` and compared to `server.py` is an identity. These literals are what a
#: manifest in a cluster assumes, and the assertion is that both implementations still
#: agree with them — so changing a default is a red test here, which is exactly the
#: notification an operator with a live Deployment needs.
STORE_DEFAULTS = {
    "store": "/data",
    "port": "8102",
    "token_file": "/run/secrets/subsystem-store/token",
}


def oracle_store_defaults(text: str) -> dict[str, str]:
    """`DEFAULT_STORE`/`DEFAULT_PORT`/`DEFAULT_TOKEN_FILE` out of `server/server.py`.

    Keys absent from the result mean the constant did not parse, which the callers below
    branch on — a partial parse must not read as partial agreement.
    """
    out: dict[str, str] = {}
    for key, name in (
        ("store", "DEFAULT_STORE"),
        ("token_file", "DEFAULT_TOKEN_FILE"),
    ):
        m = re.search(rf'^{name}\s*=\s*"([^"]*)"\s*$', text, re.M)
        if m:
            out[key] = m.group(1)
    m = re.search(r"^DEFAULT_PORT\s*=\s*(\d+)\s*$", text, re.M)
    if m:
        out["port"] = m.group(1)
    return out


def go_store_defaults(text: str) -> dict[str, str]:
    """`defaultStore`/`defaultPort`/`defaultTokenFile` out of `cmd/cairn-server/main.go`.

    Same shape as its oracle twin, and deliberately a SECOND function rather than one
    parameterised by a name table: the two languages spell a constant differently, and a
    shared regex that happened to match both would be a claim about the spelling rather
    than about either file.
    """
    out: dict[str, str] = {}
    for key, name in (
        ("store", "defaultStore"),
        ("token_file", "defaultTokenFile"),
    ):
        m = re.search(rf'^\s*{name}\s*=\s*"([^"]*)"\s*$', text, re.M)
        if m:
            out[key] = m.group(1)
    m = re.search(r"^\s*defaultPort\s*=\s*(\d+)\s*$", text, re.M)
    if m:
        out["port"] = m.group(1)
    return out

# The alias ledger, for `test_no_image_default_uses_a_name_that_would_SHADOW_a_deployment`.
# Read from `lib/` rather than restated here: a third copy of the name list is exactly the
# drift the rest of this module exists to refuse.
sys.path.insert(0, str(ROOT / "lib"))

import env_aliases  # noqa: E402

#: The ONLY `Env` expression `mkServerImage` may hand `buildLayeredImage`,
#: whitespace-normalised.
#:
#: 🔴 THE WHOLE STATEMENT, BECAUSE A KEY-SET READER OVER THE `//` OPERAND WAS MEASURED
#: WALKABLE AT FOUR SPELLINGS. `test_the_env_sets_are_identical` compares the `serverEnv`
#: BINDING against `server/Dockerfile`; the pod runs an EXPRESSION built from it, and
#: everything that expression does afterwards reached the pod unread. `inherit (x) K;`
#: and `${"K"} = "/wrong";` inside the operand carry no bare identifier before an `=`;
#: `… ++ [ "K=/wrong" ]` appends to the resulting LIST and is not an operand at all; a
#: mapper lambda can rewrite a value by key. Each shipped `SUBSYSTEM_STORE_ROOT=/wrong`
#: to a pod that starts, health-checks and serves the wrong store — a duplicate name in
#: the list resolves to the LAST entry — with both guard modules green. So this is the
#: repo's stated remedy for a guard on WORDS: pin the whole normalised string. A
#: cosmetic reformat fails here; that is the price, and it is the point.
#:
#: ⚠ The Go image's expression is a DIFFERENT constant (`GO_IMAGE_ENV_FORM` in
#: `tests/test_flake_go_image_runtime_contract.py`), because the two images legitimately
#: differ — sharing one would make each image's guard pass for the other's shape.
PY_IMAGE_ENV_FORM = (
    'pkgs.lib.mapAttrsToList (k: v: "${k}=${v}") (serverEnv // { PATH = serverPath; })'
)


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


def flake_image_block(text: str, maker: str = "mkServerImage") -> str | None:
    """The `buildLayeredImage { … }` argument set of ONE maker, brace-matched.

    🔴 `maker` IS NOT A CONVENIENCE PARAMETER — THERE IS MORE THAN ONE IMAGE NOW.
    `flake.nix` builds the Python pod (`mkServerImage`) and the Go pod
    (`mkGoServerImage`), so an unscoped `text.find("buildLayeredImage")` returns
    whichever appears FIRST in the file and every assertion below it becomes a claim
    about source ORDER. That is the same first-occurrence defect this module already
    fixed twice — in the Dockerfile extractors, and in the `serverPath` regex that read
    its value out of a COMMENT. The search therefore starts at the maker's own binding,
    and `TestTheExtractorsSeeSomething` pins that the two makers resolve to DIFFERENT
    blocks, which is the only thing that distinguishes a scoped lookup from a lucky one.

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
    start = re.search(rf"^\s*{re.escape(maker)}\s*=\s*pkgs:", text, re.M)
    if start is None:
        return None
    i = text.find("buildLayeredImage", start.end())
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


def flake_image_arg(text: str, name: str, maker: str = "mkServerImage") -> str | None:
    """One `name = <value>;` argument from inside ONE maker's image block.

    🔴 THE VALUE ENDS AT A `;` OUTSIDE ANY BRACKET, NOT AT THE FIRST `;` THAT ENDS A
    LINE. The line-ending version was written first and it TRUNCATES: an argument whose
    value is a multi-line attrset — `Env = … (serverEnvGo // {\\n PATH = …;\\n FOO = …;\\n
    });` — stops at `PATH = …;` and returns a prefix. Nothing about that reads as a
    parse failure. It returns a non-empty string, so a positive control asserting "the
    argument was found" passes, and every assertion about a LATER key in the same value
    then fails with a message naming a cause the tree does not have — the false red this
    module already records for its `serverPath` regex, arriving from the other side.

    Depth-counting over the three bracket kinds is enough here for the reason the `#`
    anchor elsewhere in this file is enough: it is a fact about this codebase, not about
    the parser. A `;` inside a nix STRING would still end the scan. `flake.nix` has
    none, and the alternative is a nix parser.
    """
    block = flake_image_block(text, maker)
    if block is None:
        return None
    m = re.search(rf"^\s*{re.escape(name)}\s*=\s*", block, re.M)
    if m is None:
        return None
    depth = 0
    for k in range(m.end(), len(block)):
        c = block[k]
        if c in "([{":
            depth += 1
        elif c in ")]}":
            if depth == 0:
                # The enclosing block closed before the value did: a malformed argument,
                # reported as absent rather than as a prefix of something else.
                return None
            depth -= 1
        elif c == ";" and depth == 0:
            return block[m.end():k].strip()
    return None


def flake_cmd_script(text: str, maker: str = "mkServerImage") -> str | None:
    """The `.py` script one maker's image `Cmd` runs.

    🔴 SCOPED TO THE MAKER, FOR THE REASON `flake_image_block` GIVES. Unscoped, this
    took the FIRST `Cmd = [ … ];` in the whole file, and the Go pod's image has one
    too — naming a BINARY, not a script. A `.py` filter over the wrong block returns
    None, which reads as "the CMD line stopped parsing" rather than "you read the
    other image".
    """
    block = flake_image_block(text, maker)
    if block is None:
        return None
    m = re.search(r"Cmd\s*=\s*\[(.*?)\];", block, re.S)
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

    def test_the_code_default_parses_find_all_three_in_BOTH_implementations(self):
        """🔴 THE POSITIVE CONTROL FOR THE CONTRACT'S NEW OPERAND.

        Since neither image states the contract, `STORE_DEFAULTS` is checked against two
        source files instead — and a regex that matched nothing would make
        `test_both_implementations_resolve_the_deployment_contract_with_no_env` compare
        `{}` against `{}` for one side and read as agreement. Each parse must yield all
        three keys, in each language.
        """
        for label, found in (
            ("server/server.py", oracle_store_defaults(SERVER_PY.read_text(encoding="utf-8"))),
            ("cmd/cairn-server/main.go", go_store_defaults(GO_SERVER.read_text(encoding="utf-8"))),
        ):
            assert set(found) == set(STORE_DEFAULTS), (
                f"the default-constant parse over {label} found {sorted(found)}, not "
                f"{sorted(STORE_DEFAULTS)} — the constant was renamed or the pattern is "
                f"wrong, and a missing key reads as agreement rather than as a failure"
            )

    def test_the_code_default_parses_can_fail(self):
        """🔴 THE NEGATIVE HALF, on realistic near-misses rather than on empty text.

        A parser that only refuses `""` passes the edit that actually happens: a constant
        renamed, moved into a function (so it is no longer at column zero / inside the Go
        `const` block with the expected indent), or turned into an expression.
        """
        # Renamed — the commonest real edit.
        assert oracle_store_defaults('DEFAULT_STORE_ROOT = "/data"\n') == {}
        assert go_store_defaults('\tdefaultStoreRoot = "/data"\n') == {}
        # Indented, i.e. moved inside a function: no longer the module-level constant.
        assert "store" not in oracle_store_defaults('    DEFAULT_STORE = "/data"\n')
        # An expression rather than a literal must not read as a literal.
        assert oracle_store_defaults("DEFAULT_PORT = PORT_BASE + 2\n") == {}
        assert go_store_defaults("\tdefaultPort = basePort + 2\n") == {}
        # And a value that IS present reads back whole, not truncated.
        assert oracle_store_defaults(
            'DEFAULT_TOKEN_FILE = "/run/secrets/subsystem-store/token"\n'
        ) == {"token_file": "/run/secrets/subsystem-store/token"}

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
        # 🔴 THE MAKER BINDING IS PRESENT IN THIS FIXTURE ON PURPOSE. Without it the
        # extractor returns None because it cannot find the SCOPE, which is a different
        # reason from the one this control means to test — "the Cmd names no `.py`". A
        # control that passes for the wrong reason is a control that stops testing the
        # day the real defect appears.
        assert flake_cmd_script(
            '  mkServerImage = pkgs:\n    buildLayeredImage {\n'
            '      Cmd = [ "python3" ];\n    };\n'
        ) is None

    def test_an_arguments_value_is_captured_WHOLE_not_truncated(self):
        """🔴 A TRUNCATED VALUE IS NOT A PARSE FAILURE, WHICH IS WHY IT NEEDS ITS OWN
        CONTROL. It returns a non-empty string, so "the argument was found" passes; only
        an assertion about the tail of the value notices, and it reports the tail as
        MISSING FROM THE SOURCE rather than as unread.

        Driven over the shipped extractor with a fixture whose shape is the one that
        broke it: a multi-line override set with a `;` at the end of an inner line.
        """
        fixture = (
            "  mkFixtureImage = pkgs:\n"
            "    buildLayeredImage {\n"
            "      Env = mapAttrsToList f (base // {\n"
            "        FIRST = a;\n"
            "        LAST = b;\n"
            "      });\n"
            "      after = 1;\n"
            "    };\n"
        )
        got = flake_image_arg(fixture, "Env", "mkFixtureImage")
        assert got is not None, "the fixture's Env argument did not parse at all"
        assert "LAST" in got, (
            f"the value was truncated to {got!r} — everything after the first "
            f"line-ending `;` was silently dropped"
        )
        assert "after" not in got, (
            f"the value ran past its own `;` into the next argument: {got!r}"
        )

    def test_the_extractors_are_SCOPED_to_one_maker(self, flake):
        """🔴 TWO IMAGES NOW LIVE IN `flake.nix`, AND AN UNSCOPED READ PICKS BY ORDER.

        `mkServerImage` builds the Python pod and `mkGoServerImage` the Go one. Every
        assertion in `TestTheTwoBuildsAgree` is about the FIRST of those, and before the
        `maker` parameter existed "the first" meant "whichever the file happens to
        mention earlier" — so moving the Go image above the Python one would silently
        re-point this whole file at the wrong artefact, with nothing going red.

        Three claims, because two of them are satisfiable by an extractor wired to
        nothing: both blocks parse, they are DIFFERENT, and a maker that does not exist
        yields None rather than a block belonging to somebody else.
        """
        python_block = flake_image_block(flake, "mkServerImage")
        go_block = flake_image_block(flake, "mkGoServerImage")
        assert python_block, "no buildLayeredImage block found for `mkServerImage`"
        assert go_block, "no buildLayeredImage block found for `mkGoServerImage`"
        assert python_block != go_block, (
            "both makers resolved to the SAME block — the scoping is not working, and "
            "every assertion in this file is about an image nobody chose"
        )
        assert flake_image_block(flake, "mkNoSuchImage") is None, (
            "an unknown maker resolved to a block, so the scope is decorative"
        )
        # And the scoping is load-bearing in the direction that matters: the Python
        # image's Cmd runs a `.py`, the Go image's runs a binary and has none.
        assert flake_cmd_script(flake, "mkServerImage") is not None
        assert flake_cmd_script(flake, "mkGoServerImage") is None, (
            "the Go pod's image Cmd names a `.py` script — it is supposed to exec the "
            "compiled server, so either the Cmd is wrong or this extractor is reading "
            "the Python block"
        )


# ---------------------------------------------------------------------------
# The agreement itself.
# ---------------------------------------------------------------------------

class TestTheTwoBuildsAgree:

    def test_the_image_Env_is_EXACTLY_the_declared_expression(self, flake):
        """🔴 THE AGREEMENT IS CHECKED ON THE BINDING; THE POD RUNS THE EXPRESSION.

        `test_the_env_sets_are_identical` compares `serverEnv` to `server/Dockerfile`'s
        `ENV` block. The image does not ship `serverEnv` — it ships `PY_IMAGE_ENV_FORM`,
        and every term of that expression after `serverEnv` is a value reaching the
        DEPLOYED pod that nothing else in either guard module reads.

        🔴 REGRESSION COVERAGE, NOT AN INVARIANT GUARD, AND THAT LABEL WAS WRONG ONCE.
        For the mutant that DELETES the override — `Env = … serverEnv;` — the matrix is
        red at `d443e31`, GREEN at `19975d6` (this branch's own regression: the only
        reader was an unscoped whole-file `PATH = serverPath` search, which the second
        image's identical line satisfied), red again since. The mutants that ADD a name
        are invariant guards; this one is not.
        """
        env_arg = flake_image_arg(flake, "Env")
        assert env_arg is not None, "no `Env` argument inside `mkServerImage`'s block"
        assert re.sub(r"\s+", " ", env_arg).strip() == PY_IMAGE_ENV_FORM, (
            f"the Python image's `Env` is {env_arg!r}; the declared expression is "
            f"{PY_IMAGE_ENV_FORM!r}. Every difference is a value reaching the deployed "
            f"pod that nothing else here describes — `test_the_env_sets_are_identical` "
            f"keeps comparing `serverEnv` to the Dockerfile and reporting agreement "
            f"about a value the pod does not get, and a MISSING `PATH` is the "
            f"seeding/revocation regression the toolchain guard exists for. If the image "
            f"genuinely needs another variable, change this constant and say what reads "
            f"it."
        )

    def test_the_env_sets_are_identical(self, dockerfile, flake):
        """Both the NAMES and the VALUES, and equality in BOTH directions.

        A subset check would pass while the flake silently dropped a variable
        the Dockerfile sets — which is how `HOME` would go missing and turn
        into an import-time crash in a cluster rather than a red test here.
        """
        assert flake_attrset(flake, "serverEnv") == dockerfile_env(dockerfile)

    def test_no_image_default_configures_the_store_in_EITHER_spelling(
        self, dockerfile, flake
    ):
        """🔴 AN IMAGE MUST SET NO VARIABLE THE SERVER'S RESOLVER READS — EITHER SPELLING.

        An image `ENV` is a DEFAULT, present in every container whether or not the manifest
        mentions it, and it costs on both sides of the resolver:

        * a CURRENT-spelling default INVERTS the layering. `lib/env_aliases` /
          `internal/envalias` resolve new-name-wins, so `CAIRN_STORE_ROOT` baked into the
          image beats a Deployment that explicitly sets `SUBSYSTEM_STORE_ROOT`, and the pod
          silently uses the image's store root, port or token path instead of the
          operator's. Not a deprecation window — for those variables the old name stops
          working the day the image ships.
        * a DEPRECATED-spelling default trips the resolver's whole-environment sweep at
          every pod start. Those warnings name a variable the operator cannot unset from a
          manifest, and migrating the Deployment to `CAIRN_*` does not clear them:
          measured 3 warnings with the image env present, still 3 after the manifest
          migrates, 0 once the image sets nothing.

        🔴 THE PREVIOUS VERSION OF THIS GUARD CHECKED ONLY THE CURRENT SPELLING, AND THE
        CHANGE THAT DROPPED THE THREE STORE VARIABLES WOULD HAVE LEFT IT PASSING
        TRIVIALLY — an invariant the tree could no longer violate, reading as coverage.
        It is widened rather than kept: BOTH columns of the ledger, so the assertion still
        has something it can fail on.

        🔴 REGRESSION COVERAGE IN BOTH DIRECTIONS, AND BOTH ARE REAL POINTS IN THIS
        REPOSITORY'S HISTORY — replayed by copying THIS file over a `git archive` of each
        commit and running it:

            0c23e0f   RED, `['CAIRN_PORT', 'CAIRN_STORE_ROOT', 'CAIRN_TOKEN_FILE']`
                      — both files baked the CURRENT spelling while a live Deployment
                      set the old one; unchanged only by luck, because it set the same
                      values
            e878f4c   RED, `['SUBSYSTEM_STORE_PORT', 'SUBSYSTEM_STORE_ROOT',
                      'SUBSYSTEM_STORE_TOKEN_FILE']` — both files baked the DEPRECATED
                      spelling
            HEAD      green

        Each died with this assertion's own message naming the offending file.

        ⚠ THE SECOND ROW USED TO CITE `78679b9`, WHICH DOES NOT EXIST — a local WIP commit
        discarded by a soft reset, reachable from no ref in any clone. `e878f4c` is the
        commit that actually carries that state, and the row above is a run against it
        rather than a re-spelling of the old sentence.

        ⚠ IT READS BOTH BUILDS, AND THE GO IMAGE IS COVERED BY DERIVATION. `serverEnv` is
        the single nix-side statement of the contract and `mkGoServerImage` subtracts from
        it (`tests/test_flake_go_image_runtime_contract.py`), so a name cannot enter the Go
        pod without entering `serverEnv` first.

        The rule is stated over the LEDGER rather than over literal names, so a variable
        renamed later is covered on the day it is added without anybody editing this.
        """
        banned = {new for new, _old in env_aliases.LEDGER}
        banned |= {old for _new, old in env_aliases.LEDGER}
        assert len(banned) == 2 * len(env_aliases.LEDGER) > 0, (
            f"the ledger yielded {len(banned)} names from {len(env_aliases.LEDGER)} pairs "
            f"— a spelling appears in both columns, or the ledger is empty, and either way "
            f"this guard is not covering what its docstring says"
        )
        for where, env in (
            ("flake.nix's serverEnv", flake_attrset(flake, "serverEnv")),
            ("server/Dockerfile's ENV", dockerfile_env(dockerfile)),
        ):
            assert env, f"parsed no environment out of {where}"
            offenders = sorted(set(env) & banned)
            assert not offenders, (
                f"{where} sets {offenders}. Neither image may configure the store, the "
                f"port or the token path: a CURRENT-spelling default outranks a "
                f"Deployment that sets the deprecated one, and a DEPRECATED-spelling "
                f"default emits a deprecation warning at every pod start that no manifest "
                f"can clear. Both files carry the note, and the values those variables "
                f"used to hold are pinned as CODE defaults by "
                f"`test_both_implementations_resolve_the_deployment_contract_with_no_env`."
            )

    def test_both_implementations_resolve_the_deployment_contract_with_no_env(self):
        """🔴 WHAT THE IMAGES STOPPED SAYING, PINNED WHERE IT NOW LIVES.

        Dropping the three `ENV` entries means `docker inspect` no longer tells an
        operator where the store is, which port the pod binds, or where the token is read
        from. That was accepted as a cost — it must not also become UNASSERTED. With no
        environment at all, both implementations must resolve the same three values, and
        those values must still be the ones a live Deployment assumes.

        Measured on the artefacts rather than only read off the source, at two points:
        `env -i … server/server.py` prints `listening on 0.0.0.0:8102 store=/data`, and
        `env -i … cairn-server` names `/run/secrets/subsystem-store/token` in its
        token-file refusal. This test pins the constants those runs resolved, in both
        implementations, so a change to either is a red test rather than a surprise in a
        cluster.

        ⚠ AN INVARIANT GUARD. No defect ever moved a default apart; what it refuses is a
        default moving now that no image `ENV` would contradict it.
        """
        oracle = oracle_store_defaults(SERVER_PY.read_text(encoding="utf-8"))
        go = go_store_defaults(GO_SERVER.read_text(encoding="utf-8"))
        assert oracle == STORE_DEFAULTS, (
            f"server/server.py's defaults are {oracle}, and a Deployment assumes "
            f"{STORE_DEFAULTS}. Neither image states these any more, so this constant is "
            f"the only place the contract is written down."
        )
        assert go == STORE_DEFAULTS, (
            f"cmd/cairn-server/main.go's defaults are {go}, and a Deployment assumes "
            f"{STORE_DEFAULTS}. Swapping the image must not require editing a manifest."
        )
        assert oracle == go, (
            f"the two implementations disagree: {oracle} vs {go}. One image would serve a "
            f"different store, or read a different token, from the other."
        )

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
        # The specific uid is pinned too: 65532 is the runAsUser of a sibling pod
        # already in the cluster, which is what makes the fsGroup story on the
        # PVC hold.
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

    def test_the_exposed_port_agrees_with_the_port_the_server_actually_binds(
        self, dockerfile, flake
    ):
        """🔴 EXPOSE IS DOCUMENTATION; THE CODE DEFAULT IS THE BINDING.

        `EXPOSE` only annotates the image. The pod binds whatever its port resolves to,
        and with no `SUBSYSTEM_STORE_PORT`/`CAIRN_PORT` anywhere in the image that is the
        code default. They can disagree, and if they do the pod listens somewhere the
        manifest does not name — so the two are pinned together here rather than each
        being pinned only to its own side of the other file.

        ⚠ IT USED TO READ THE IMAGE `ENV`, AND THAT OPERAND NO LONGER EXISTS. Both images
        stopped setting the port; re-aiming it at the resolved default is what keeps the
        claim ("EXPOSE names the port the server binds") true rather than unasked.
        """
        port = STORE_DEFAULTS["port"]
        assert dockerfile_expose(dockerfile) == port
        assert flake_int(flake, "serverPort") == port

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
        # That `serverPath` reaches THIS image's `Env` is
        # `test_the_image_Env_is_EXACTLY_the_declared_expression`'s, not this test's: it
        # pins the whole expression, so a second reader here would be the duplicated
        # predicate this repository already has a rule about.

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
        port = STORE_DEFAULTS["port"]
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
