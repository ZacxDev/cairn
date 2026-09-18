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

#: The ONLY names `mkServerImage`'s `Env` argument may add on top of `serverEnv`.
#:
#: 🔴 AN EXACT SET, BECAUSE `//` IS THE ONE ROUTE INTO THE POD'S ENVIRONMENT THAT
#: `flake_attrset(flake, "serverEnv")` CANNOT SEE. Every env assertion in this file
#: compares the `serverEnv` BINDING against `server/Dockerfile`; the image's `Env`
#: argument composes that binding with a literal, and `//` is right-biased, so a name
#: written into the literal REPLACES the agreed value AFTER the agreement was checked.
#: Measured, not assumed: `serverEnv // { PATH = serverPath; SUBSYSTEM_STORE_ROOT =
#: "/wrong"; }` and the same with `SUBSYSTEM_STORE_TOKEN_FILE` each SURVIVED this module
#: and its Go sibling together — 33 passed, 0 failed — while shipping the deployed pod
#: at the wrong store, or reading its bearer token from a compiled-in path.
#:
#: ⚠ The Go image's set is a DIFFERENT constant (`ENV_OVERRIDE_KEYS` in
#: `tests/test_flake_go_image_runtime_contract.py`) and deliberately so: that image also
#: declares `SSL_CERT_FILE`, and sharing one list would make each image's guard pass for
#: a variable only the other one has a reason to set.
PY_ENV_OVERRIDE_KEYS = ("PATH",)


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


# ---------------------------------------------------------------------------
# The `//` reader. It lives HERE, beside the other shipped extractors, because BOTH
# image makers compose their `Env` with `//` and both guard modules have to read the
# same one — a second copy in the Go module would be the duplicated predicate this
# repository already has a rule about, and the two copies would disagree about the
# image nobody was looking at.
# ---------------------------------------------------------------------------

def nix_override_terms(expr: str) -> list[str]:
    """Every RIGHT-hand operand of a `//` in one nix expression, as text.

    `serverEnvGo // { A = …; } // extra` returns `['{ A = …; }', 'extra']`. An operand
    that is a literal attrset is brace-matched so a nested `{ … }` cannot end it early;
    anything else is returned verbatim, which is what lets the caller distinguish "an
    attrset whose keys I can read" from "an expression I cannot" — the two are different
    facts and a guard that collapsed them would pass an override it never parsed.

    `#` comments are stripped first: a comment is the cheapest place to hide a `//`, and
    this module already records a regex that read a value out of prose.
    """
    text = re.sub(r"#[^\n]*", "", expr)
    terms: list[str] = []
    i = 0
    while True:
        j = text.find("//", i)
        if j == -1:
            return terms
        k = j + 2
        while k < len(text) and text[k].isspace():
            k += 1
        if k < len(text) and text[k] == "{":
            depth = 0
            end = None
            for e in range(k, len(text)):
                if text[e] == "{":
                    depth += 1
                elif text[e] == "}":
                    depth -= 1
                    if depth == 0:
                        end = e
                        break
            if end is None:            # unbalanced: report the rest and stop
                terms.append(text[k:])
                return terms
            terms.append(text[k:end + 1])
            i = end + 1
        else:
            nxt = text.find("//", k)
            terms.append((text[k:] if nxt == -1 else text[k:nxt]).strip())
            if nxt == -1:
                return terms
            i = nxt


def nix_attrset_keys(term: str) -> list[str] | None:
    """The TOP-LEVEL keys of a literal `{ K = …; … }`, or None if it is not a literal.

    Depth-tracked rather than a flat `findall`, so a key inside a nested attrset or list
    is not reported as one of this set's own — the whole point of the caller's exact-set
    assertion is that the set it compares is the set the image actually gains.

    ⚠ SCOPE: this is a fact about `flake.nix`, not a nix parser. A `{` inside a string
    (`"${k}=${v}"`) would move the depth counter, which is why the caller hands it the
    `//` OPERAND rather than the whole `Env` value — the operand carries no such string
    today, and `TestTheExtractorsSeeSomething` pins that it still parses.
    """
    term = term.strip()
    if not (term.startswith("{") and term.endswith("}")):
        return None
    keys: list[str] = []
    depth = 0
    for m in re.finditer(r"[{}()\[\]]|([A-Za-z_][\w'-]*)\s*=(?!=)", term[1:-1]):
        if m.group(1) is not None:
            if depth == 0:
                keys.append(m.group(1))
        elif m.group(0) in "([{":
            depth += 1
        else:
            depth -= 1
    return keys


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

    def test_the_override_extractors_can_fail(self):
        """🔴 THE `//` READER, DRIVEN OVER THE SHAPES THAT ACTUALLY GET WRITTEN.

        Both image guards now assert an EXACT key set over whatever this returns, so a
        reader that quietly returned `[]` would turn both of those assertions into
        claims about nothing — and an empty override and an unparsed one are different
        facts the callers branch on. Controlled here rather than in the Go module for
        the reason the extractors themselves live here: one copy, one control.
        """
        # No override is an EMPTY list, not a failure — and a `//` inside a comment is
        # not an override, which is the cheapest place to hide one.
        assert nix_override_terms("serverEnv") == []
        assert nix_override_terms("# a // in a comment\nserverEnv") == []
        assert nix_override_terms("serverEnv // { A = x; }") == ["{ A = x; }"]
        # `//` chains, and the LAST term wins — so two overrides must read as two.
        assert nix_override_terms("e // { A = x; } // { B = y; }") == [
            "{ A = x; }", "{ B = y; }"
        ]
        # A non-literal operand comes back verbatim, so `nix_attrset_keys` can REFUSE it
        # rather than a guard silently reading no keys out of it and passing.
        assert nix_override_terms("e // extraEnv") == ["extraEnv"]
        assert nix_attrset_keys("extraEnv") is None
        assert nix_attrset_keys("{ A = x; B = y; }") == ["A", "B"]
        # 🔴 AND A KEY NESTED INSIDE THE OPERAND IS NOT ONE OF ITS OWN. The realistic
        # negative control rather than a textbook one: `{ PATH = x; N = { ROOT = y; }; }`
        # is exactly how an exact-set assertion gets walked by a flat `findall`, and the
        # nested name is the one an override would hide a store path behind.
        assert nix_attrset_keys("{ PATH = x; N = { ROOT = y; }; }") == ["PATH", "N"]

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

    def test_the_image_Env_OVERRIDE_adds_only_PATH(self, flake):
        """🔴 THE AGREEMENT IS CHECKED ON THE BINDING; THE POD RUNS THE COMPOSITION.

        `test_the_env_sets_are_identical` compares `serverEnv` to `server/Dockerfile`'s
        `ENV` block. The image does not ship `serverEnv` — it ships
        `serverEnv // { PATH = serverPath; }`, and `//` is right-biased, so anything
        written into that literal overrides the value the agreement test just approved.
        Nothing else in either guard module reads it.

        🔴 MEASURED, NOT ARGUED. `serverEnv // { PATH = serverPath;
        SUBSYSTEM_STORE_ROOT = "/wrong"; }` and the same with a
        `SUBSYSTEM_STORE_TOKEN_FILE` literal were each run against THIS module and its Go
        sibling together: 33 passed, 0 failed, both times. That is the DEPLOYED pod
        serving the wrong store, or reading its bearer token from a compiled-in path
        while the secret is mounted where the manifest says — behind a green suite whose
        headline claim is that the two builds agree.

        ⚠ AN INVARIANT GUARD. No such override ever shipped; the mutants were written by
        hand and watched to survive. It is the sibling of
        `test_the_Env_OVERRIDE_adds_only_PATH_and_the_CA_bundle`, which closes the same
        hole on the Go image, and the two are separate because the two images legitimately
        add different names.
        """
        env_arg = flake_image_arg(flake, "Env")
        assert env_arg is not None, "no `Env` argument inside `mkServerImage`'s block"

        terms = nix_override_terms(env_arg)
        assert len(terms) == 1, (
            f"the Python image's `Env` composes {len(terms)} `//` overrides onto "
            f"`serverEnv`: {terms!r}. `//` is right-biased, so the LAST term outranks "
            f"every earlier one and this module's env agreement describes none of them."
        )
        keys = nix_attrset_keys(terms[0])
        assert keys is not None, (
            f"the Python image's `Env` override is {terms[0]!r} — not a literal attrset, "
            f"so what it adds to the deployed pod's environment is decided somewhere no "
            f"guard here can read"
        )
        assert len(keys) == len(set(keys)), (
            f"the Python image's `Env` override names {keys!r} with a repeat — nix takes "
            f"the LAST, so the duplicate is what runs"
        )
        env = flake_attrset(flake, "serverEnv")
        assert env, "no `serverEnv` parsed — see the controls"
        clobbered = sorted(set(keys) & set(env))
        assert not clobbered, (
            f"the Python image's `Env` override sets {clobbered}, which `serverEnv` "
            f"already defines. `//` is right-biased, so the literal WINS — and "
            f"`test_the_env_sets_are_identical` keeps comparing `serverEnv` to the "
            f"Dockerfile and reporting agreement about a value the pod does not get."
        )
        assert sorted(set(keys)) == sorted(PY_ENV_OVERRIDE_KEYS), (
            f"the Python image's `Env` override adds {sorted(set(keys))!r}; the declared "
            f"set is {sorted(PY_ENV_OVERRIDE_KEYS)!r}. The catch-all: a name outside "
            f"`serverEnv` is still a value reaching the deployed pod that nothing here "
            f"describes, and a MISSING `PATH` is the seeding/revocation regression the "
            f"toolchain guard exists for. If the image needs another variable, add it "
            f"here and say what reads it."
        )

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
        #
        # 🔴 SCOPED TO *THIS* IMAGE'S `Env`, AND THE UNSCOPED VERSION WAS MEASURED WRONG
        # THE DAY A SECOND IMAGE LANDED. It used to search the WHOLE FILE. With only
        # `mkServerImage` in `flake.nix` that was unambiguous; `mkGoServerImage` also
        # writes `PATH = serverPath`, so deleting the override from the DEPLOYED image
        # entirely — `Env = … serverEnv;` — left this assertion matching the OTHER
        # image's line and the whole pair of modules green at 33 passed, 0 failed. A
        # guard reading the wrong artefact is the failure this file's `maker` parameter
        # exists for; it just had one site left that did not use it.
        # `(?<![\w'-])` for the reason the Go module's copy has it: nix identifiers take
        # `_`, `'` and `-`, so a bare `PATH\s*=` also matches `GOPATH = serverPath`.
        env_arg = flake_image_arg(flake, "Env")
        assert env_arg is not None, "no `Env` argument inside `mkServerImage`'s block"
        assert re.search(r"(?<![\w'-])PATH\s*=\s*serverPath\b", env_arg), (
            f"the Python image's `Env` is {env_arg!r} — `serverPath` is defined but "
            f"never placed into THIS image's Env"
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
