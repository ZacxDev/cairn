#!/usr/bin/env python3
"""The closed `tags:` vocabulary, pinned ACROSS THE TWO IMPLEMENTATIONS and measured on
the oracle's own write path.

WHAT IS BEING PROTECTED
-----------------------
`tags:` is the entry front matter's category axis, and its vocabulary is CLOSED on the
WRITE path: `infra`, `product`, `tooling`. Two servers enforce it —
`internal/write`'s `validateEntryBytes` (the DEPLOYED Go pod) and `server.py`'s
`_validate_entry_bytes` (the oracle) — and they must refuse the same bodies with the same
bytes, because `tests/conformance/` replays one corpus against both.

🔴 THE REFUSAL IS ON THE **WRITE** PATH AND MUST NEVER MIGRATE INTO THE READER, AND THAT
IS A MEASURED OUTAGE RATHER THAN A PREFERENCE. A vocabulary refusal raised by
`SubsystemEntry.from_mapping` / `store.EntryFromMapping` makes the entry MALFORMED, and a
malformed entry is not merely unrendered: it is out of the index, so `--ref` and `--search`
lose it, AND it is UNWRITABLE, because every write route resolves its target through that
same index and answers 404 `ref-unknown`. Closing the vocabulary in the reader would
therefore take every entry that already carries an off-vocabulary tag and make it
unreadable and unrepairable in one stroke. The reader-side half of that claim is measured
here (`test_the_reader_still_loads_an_off_vocabulary_tag`) and in Go
(`TestTheReaderSTILLLOADSAnOffVocabularyTag`).

WHY THIS FILE EXISTS RATHER THAN A CONSTANT IMPORTED IN TWO PLACES
------------------------------------------------------------------
`packages.cairn` installs the Python client script and `lib/` under `libexec` and nothing
else, so `lib/entry_shape.py` CANNOT import `internal/write`. That is packaging, not a
design choice anybody may undo here — the same shape `tests/test_env_aliases.py` exists
for, and `AGENTS.md` already blesses it for `server/Dockerfile` against `flake.nix`.

WHAT IT PINS, AND WHAT IT STRUCTURALLY CANNOT SEE
-------------------------------------------------
It pins the vocabulary SET **and its order** (failing when it grows, shrinks or is
reordered — the order is part of the refusal, which joins the terms with `|`), and the
refusal sentence as a WHOLE NORMALISED STRING. 🔴 THE WHOLE STRING, NOT KEYWORDS: when the
artifact under test is prose, a guard on words is walkable by rewording. A cosmetic reword
therefore fails this test; that cost is paid on purpose, for a machine-readable claim.

⚠ IT READS THE GO SIDE AS TEXT, so it is blind to the same thing `test_env_aliases.py` is:
an argument-ORDER mistake on the Go side that still renders well-formed English (passing
the joined vocabulary where the tag goes). **`tests/conformance/` is what closes that** —
its `put-*-tag-outside-the-vocabulary` rows send one body to both servers and compare the
answered bytes, which no amount of source reading can substitute for. Said here rather
than left to be discovered, because a reader who took this file for full coverage would
stop looking.

🔴 A BLIND SPOT THAT WAS HERE AND IS NOW CLOSED, KEPT AS THE RECORD RATHER THAN DELETED.
`test_the_refusal_sentence_is_byte_identical_across_the_two_servers` used to build the
Python side from an f-string **in this file** rather than from `server.py`, so it pinned
the Go source against this module's own spelling and nothing about the oracle: rewording
`CLOSED` to `SEALED` inside `server.py` left it GREEN — measured, not reasoned. It now RUNS
the oracle's `_validate_entry_bytes` and reads the exception, so the same reword reds it.
Written down because the SHAPE is the interesting part: a test whose NAME claims a
relationship while its body compares one side to itself reads as coverage and provides
none, and the name is what a later reader trusts.

⚠ AND IT SAYS NOTHING ABOUT THE CLIENTS. Neither client pre-validates a tag: both send the
body and relay the server's refusal, deliberately, so there is no third spelling of the
vocabulary to drift. `tests/parity/` is what measures that the two clients relay it
identically.
"""
from __future__ import annotations

import re
import sys
from pathlib import Path

import pytest

REPO = Path(__file__).resolve().parents[1]
GO_VOCAB_SOURCE = REPO / "internal" / "write" / "tagvocab.go"
GO_WRITE_SOURCE = REPO / "internal" / "write" / "write.go"

sys.path.insert(0, str(REPO / "lib"))

import entry_shape  # noqa: E402
import subsystem_resolver as sr  # noqa: E402

#: The vocabulary, SPELLED HERE BY HAND. Neither side is imported for this one: an
#: expectation derived from the implementation asserts `x == x` and stays green through
#: the exact edit this file exists to catch.
DECLARED = ("infra", "product", "tooling")

#: The refusal, likewise spelled by hand, with the two substitutions left as the
#: implementations write them.
DECLARED_REFUSAL = (
    "tag 'marketing' is not one of infra|product|tooling — the tag "
    "vocabulary is CLOSED on the WRITE path, so widening it is a code change. An entry "
    "already carrying this tag still READS and still accepts appends; what is refused is "
    "this WRITE, re-sending such an entry unchanged included. Edit the tag to one of "
    "those terms and resend"
)


# --- reading the GO spelling -------------------------------------------------
#
# 🔴 A TOKEN WALKER, NOT A `str.find`. The Go message is written as a concatenation across
# source lines, which is how gofmt wants a long string, so "take the text between two
# quotes" reads ONE fragment and silently compares a third of the sentence.

_VOCAB_BLOCK = re.compile(r"var\s+tagVocabulary\s*=\s*\[\]string\{([^}]*)\}")
_QUOTED = re.compile(r'"((?:[^"\\]|\\.)*)"')
_ESCAPE = re.compile(r"\\(.)")
_ESCAPES = {"n": "\n", "t": "\t", '"': '"', "\\": "\\"}


def _unescape(text: str) -> str:
    """Resolve the Go escape sequences a source literal can carry.

    ⚠ NOT `codecs.unicode_escape`, AND THE DIFFERENCE IS A BUG THIS FILE ALREADY HAD.
    That codec decodes through latin-1, so the em dash in the refusal came back as three
    mojibake characters and the comparison failed on a message that was byte-for-byte
    correct. Only the escapes Go actually writes are resolved here; every other byte is
    already the UTF-8 the file holds.
    """
    return _ESCAPE.sub(lambda m: _ESCAPES.get(m.group(1), m.group(0)), text)


def _go_vocabulary() -> tuple[str, ...]:
    text = GO_VOCAB_SOURCE.read_text(encoding="utf-8")
    block = _VOCAB_BLOCK.search(text)
    assert block, (
        f"no `var tagVocabulary = []string{{…}}` in {GO_VOCAB_SOURCE.relative_to(REPO)}. "
        f"If the declaration was reshaped, reshape this extractor with it — a silent "
        f"no-match here would report agreement between two things it never read."
    )
    return tuple(m.group(1) for m in _QUOTED.finditer(block.group(1)))


def _go_refusal_template() -> str:
    """The Go format string, reassembled from its concatenated fragments.

    Bounded by the `fmt.Sprintf(` that opens the call and the first ARGUMENT after the
    format string (`store.PyRepr(`), so a later literal in the same function cannot be
    swept in.
    """
    text = GO_WRITE_SOURCE.read_text(encoding="utf-8")
    start = text.find("if tag, outside := tagOutsideVocabulary(")
    assert start != -1, (
        "no `tagOutsideVocabulary` call in internal/write/write.go — the write-time "
        "validator no longer consults the closed vocabulary, or this extractor is stale. "
        "Check the second before believing the first."
    )
    sprintf = text.find("fmt.Sprintf(", start)
    assert sprintf != -1, "the vocabulary refusal no longer formats a message"
    end = text.find("store.PyRepr(", sprintf)
    assert end != -1 and end > sprintf, (
        "the vocabulary refusal no longer interpolates `store.PyRepr(tag)`, so this "
        "extractor cannot find where the format string ends"
    )
    fragments = [m.group(1) for m in _QUOTED.finditer(text[sprintf:end])]
    assert fragments, "the refusal's format string extracted to NOTHING"
    return _unescape("".join(fragments))


def _normalize(text: str) -> str:
    """Collapse whitespace, so a line rewrap is not a difference and a reword is."""
    return " ".join(text.split())


# --- the pins ----------------------------------------------------------------


def test_the_extractors_can_observe_something():
    """🔴 THE POSITIVE CONTROL, FIRST. Every comparison below is between two values this
    file read out of source text, and a reassuring pass is indistinguishable from an
    extractor wired to nothing. So each is required to produce a NON-EMPTY, well-shaped
    value with the number of substitution points the message needs."""
    vocab = _go_vocabulary()
    assert len(vocab) >= 2, f"the Go vocabulary extracted to {vocab!r}"
    template = _go_refusal_template()
    assert template, "the Go refusal template extracted to the empty string"
    assert template.count("%s") == 2, (
        f"the Go refusal takes {template.count('%s')} substitution(s), not 2 (the tag and "
        f"the joined vocabulary): {template!r}"
    )
    # …and the negative control on `_normalize`: two genuinely different sentences must
    # not compare equal after it, or every whole-string comparison here is vacuous.
    assert _normalize("a  b") == _normalize("a\nb")
    assert _normalize("a b") != _normalize("a c")


def test_the_vocabulary_is_the_declared_set_in_both_languages():
    """🔴 THE SET **AND ITS ORDER**, BECAUSE THE ORDER IS ON THE WIRE. The refusal joins
    the terms with `|`, so reordering one side changes the served bytes and the
    conformance corpus goes red — this is the cheaper red that says which side moved."""
    assert entry_shape.TAG_VOCABULARY == DECLARED, (
        f"lib/entry_shape.py TAG_VOCABULARY={entry_shape.TAG_VOCABULARY!r}, "
        f"want {DECLARED!r}"
    )
    assert _go_vocabulary() == DECLARED, (
        f"internal/write/tagvocab.go tagVocabulary={_go_vocabulary()!r}, want {DECLARED!r}"
    )


def test_the_refusal_sentence_is_byte_identical_across_the_two_servers(oracle):
    """Both implementations' refusals, rendered for one tag and compared whole.

    🔴 THE PYTHON SIDE IS THE ORACLE'S OWN RENDERED EXCEPTION, AND IT WAS NOT ALWAYS.
    For one round this test built the "Python refusal" from an f-string typed in THIS
    file and compared it to another literal in this file — so its name and its docstring
    claimed both implementations while its body provided the Go half plus a
    self-comparison. That is the `guards-narrower` shape (a description that claims a
    RELATIONSHIP over a body that inspects one SIDE), and it was not hypothetical: a
    one-word reword INSIDE `server.py` left this test GREEN, measured. The fix is to make
    the body as wide as the sentence rather than to narrow the sentence — so the Python
    half now comes from RUNNING the oracle's validator and reading the exception it
    raises, which is the only spelling a client ever sees.

    ⚠ THE GO HALF IS STILL SOURCE TEXT, because the `tests` CI job has no Go toolchain.
    Its residual blind spot — an argument-ORDER mistake that still renders well-formed
    English — is closed by `tests/conformance/`, which sends one body to both servers and
    diffs the answered bytes. That is now the ONLY half of this comparison that source
    reading has to carry.
    """
    offender = entry_shape.tag_outside_vocabulary(("marketing",))
    assert offender == "marketing"

    # The ORACLE's own bytes, from the live code path a `PUT` takes.
    with pytest.raises(oracle.EntryShapeError) as caught:
        oracle._validate_entry_bytes(
            _entry_bytes("marketing"), scope="alpha-notes", filename="gadget-one.md"
        )
    python_refusal = str(caught.value)

    go_refusal = _go_refusal_template() % (
        repr(offender),
        "|".join(_go_vocabulary()),
    )
    assert _normalize(go_refusal) == _normalize(DECLARED_REFUSAL), (
        "the GO refusal drifted from the declared sentence.\n"
        f"want: {_normalize(DECLARED_REFUSAL)}\n"
        f"got:  {_normalize(go_refusal)}"
    )
    assert _normalize(python_refusal) == _normalize(DECLARED_REFUSAL), (
        "the PYTHON refusal drifted from the declared sentence.\n"
        f"want: {_normalize(DECLARED_REFUSAL)}\n"
        f"got:  {_normalize(python_refusal)}"
    )
    # …and the two servers against EACH OTHER, which is the claim in the name. It is not
    # implied by the two assertions above only in the sense that a future edit could
    # loosen one of them; asserting it directly costs one line and says what this test is
    # for.
    assert _normalize(go_refusal) == _normalize(python_refusal)


def test_the_helper_reports_the_first_offender_and_nothing_for_a_clean_set():
    """`tag_outside_vocabulary`'s own contract, in both directions.

    ⚠ THE ENTRY'S TAG SET IS SORTED BY THE LOADER, so the FIRST offender is deterministic
    and the two servers name the same one for one body. Both input orders are sent,
    because "the one written first" and "the first in sorted order" are different rules
    that agree on every single-offender case."""
    for tags in (("marketing", "zeta-tag"), ("zeta-tag", "marketing")):
        assert entry_shape.tag_outside_vocabulary(tags) == tags[0], tags
    # Every declared term, one at a time and all together: the positive control that makes
    # the refusals above a measurement rather than a gate that refuses everything.
    for tag in entry_shape.TAG_VOCABULARY:
        assert entry_shape.tag_outside_vocabulary((tag,)) is None, tag
    assert entry_shape.tag_outside_vocabulary(entry_shape.TAG_VOCABULARY) is None
    # …and an entry with NO tags, which is every entry in the store today. Treating that
    # as "not in the vocabulary" would refuse every write in the repository.
    assert entry_shape.tag_outside_vocabulary(()) is None


def test_the_reader_still_loads_an_off_vocabulary_tag(tmp_path: Path):
    """🔴 THE ANTI-OUTAGE GUARD, ON THE PYTHON SIDE. See this module's docstring for what a
    reader-side refusal would cost. Asserted over a real store on disk through
    `load_index`, because that is what the pod and the client call — a successful
    `from_mapping` says nothing about the index the entry has to land in, and "it is in
    the index" is the claim the outage is about.

    ⚠ AN INVARIANT GUARD AS WRITTEN: the reader has never refused an undeclared tag. It
    was watched RED by the only mutation that produces the hazard — adding the vocabulary
    comparison to `from_mapping` — which makes the entry MALFORMED and the ref
    unresolvable."""
    scope = tmp_path / "alpha-notes"
    scope.mkdir()
    (scope / "legacy-note.md").write_text(
        "---\n"
        "service: legacy-note\n"
        "scope: alpha-notes\n"
        "tags: [Marketing, project-xyz]\n"
        "---\n"
        "\n"
        "## What it is\n"
        "A synthetic entry written before the vocabulary closed.\n"
        "\n"
        "## Nuance / work-history\n"
        "- 2000-01-02: the synthetic action this entry records.\n",
        encoding="utf-8",
    )

    index = sr.load_index(tmp_path, on_malformed=sr.ON_MALFORMED_COLLECT)

    # 1. NOT MALFORMED. A degrading load COLLECTS rejects rather than raising, so a
    #    refusal would arrive as a row here and not as an exception — which is exactly how
    #    such a change could ship looking green.
    assert not index.malformed, (
        f"the reader classified an off-vocabulary tag as MALFORMED, which takes the entry "
        f"out of the index AND out of every write route: {index.malformed!r}"
    )
    # 🔴 THE POSITIVE CONTROL ON THAT CHANNEL, IN THE SAME RUN. An empty `malformed` is
    # indistinguishable from a field nothing ever writes, so a second scope holding a file
    # the loader really does refuse must show up in it. Without this the assertion above
    # is the reassuring zero this repository's rules name.
    rejected = tmp_path / "rubble-heap"
    rejected.mkdir()
    (rejected / "broken-four.md").write_text(
        "---\nservice: broken-four\nscope: rubble-heap\n"
        "aliases: a bare string, which the schema refuses\n---\n\n",
        encoding="utf-8",
    )
    control = sr.load_index(tmp_path, on_malformed=sr.ON_MALFORMED_COLLECT)
    assert control.malformed, (
        "a file the loader genuinely refuses did not appear in `malformed`, so the "
        "assertion above measured a channel wired to nothing"
    )
    assert all("legacy-note" not in row.filename for row in control.malformed), (
        f"the entry under test appeared in `malformed` once a sibling was added: "
        f"{control.malformed!r}"
    )

    # 2. IN THE INDEX, folded rather than dropped.
    entries = index.entries("alpha-notes")
    assert len(entries) == 1, entries
    assert entries[0].tags == ("marketing", "project-xyz"), entries[0].tags

    # 3. RESOLVABLE BY REF — what `--ref` reads and what every write route resolves
    #    through. An entry in the index but unreachable by ref is still unwritable.
    resolved, _how = sr.resolve_ref_tiered("legacy-note", index, "alpha-notes")
    assert resolved is not None, (
        "`--ref legacy-note` does not resolve, so `put`/`append` would answer 404 for an "
        "entry that is sitting right there"
    )
    assert resolved.filename == "legacy-note.md", resolved

    # 4. FINDABLE BY THE TAG FILTER, using the undeclared tag as the operand: the read
    #    surface deliberately does NOT consult the write vocabulary.
    assert sr.entry_has_tag(resolved, sr.normalize_ref("Marketing"))
    # The control on that predicate: `infra` is a DECLARED term the entry does not carry,
    # so this pins that membership is not satisfied by the vocabulary.
    assert not sr.entry_has_tag(resolved, "infra")


# --- the ORACLE's own write path ---------------------------------------------


@pytest.fixture(scope="module")
def oracle():
    """`server/server.py`, loaded by path the way `tests/test_subsystem_store_api.py`
    loads it."""
    import importlib.util

    spec = importlib.util.spec_from_file_location(
        "_tag_vocabulary_oracle", REPO / "server" / "server.py"
    )
    assert spec and spec.loader
    module = importlib.util.module_from_spec(spec)
    sys.modules[spec.name] = module
    spec.loader.exec_module(module)
    return module


def _entry_bytes(tags_flow: str) -> bytes:
    tag_line = f"tags: [{tags_flow}]\n" if tags_flow else ""
    return (
        "---\n"
        "service: gadget-one\n"
        "scope: alpha-notes\n"
        f"{tag_line}"
        "---\n"
        "\n"
        "## What it is\n"
        "A synthetic entry.\n"
        "\n"
        "## Nuance / work-history\n"
        "- 2000-01-02: the synthetic action this entry records.\n"
    ).encode()


def test_the_oracle_refuses_an_off_vocabulary_tag_on_create(oracle, tmp_path: Path):
    """🔴 AND THE NAME IS LEFT FREE. `create_entry` validates BEFORE it claims the name
    precisely so a caller that fixes its body can retry into the same ref; a refusal that
    left a file there would answer 412 `already-exists` on the retry, which reads as
    "somebody else took it"."""
    target = tmp_path / "alpha-notes" / "gadget-one.md"
    with pytest.raises(oracle.EntryShapeError) as caught:
        oracle.create_entry(
            target,
            data=_entry_bytes("marketing"),
            scope="alpha-notes",
            filename="gadget-one.md",
        )
    assert _normalize(str(caught.value)) == _normalize(DECLARED_REFUSAL), str(caught.value)
    assert not target.exists(), "a refused create left a file behind"


def test_the_oracle_refuses_an_off_vocabulary_tag_on_replace(oracle, tmp_path: Path):
    """🔴 AND THE BYTES ARE UNCHANGED. A refusal that has already written is the failure
    the whole design exists to prevent, and an exception is not evidence about the disk."""
    scope = tmp_path / "alpha-notes"
    scope.mkdir()
    path = scope / "gadget-one.md"
    original = _entry_bytes("infra")
    path.write_bytes(original)
    with pytest.raises(oracle.EntryShapeError) as caught:
        oracle.replace_entry(
            path,
            data=_entry_bytes("marketing"),
            if_match=[oracle.entry_revision(original)],
            scope="alpha-notes",
            filename="gadget-one.md",
        )
    assert _normalize(str(caught.value)) == _normalize(DECLARED_REFUSAL), str(caught.value)
    assert path.read_bytes() == original, "a refused replace changed the file"


def test_every_declared_tag_lands_through_both_oracle_write_primitives(
    oracle, tmp_path: Path
):
    """🔴 THE POSITIVE CONTROL, AND WITHOUT IT THE TWO REFUSALS ABOVE ARE SATISFIED BY A
    VALIDATOR THAT REFUSES EVERY BODY. Each declared term is written through BOTH
    primitives and the write is OBSERVED to land.

    ⚠ IT WALKS THE VOCABULARY RATHER THAN NAMING THE TERMS, which is the one place
    deriving from the implementation is correct: the claim here is "every declared term is
    writable", not "these are the declared ones" — that second claim is
    `test_the_vocabulary_is_the_declared_set_in_both_languages`', with literals."""
    assert entry_shape.TAG_VOCABULARY, "the vocabulary is EMPTY, so this test is vacuous"
    for n, tag in enumerate(entry_shape.TAG_VOCABULARY):
        root = tmp_path / f"world-{n}"
        target = root / "alpha-notes" / "gadget-one.md"
        target.parent.parent.mkdir(parents=True)
        data = _entry_bytes(tag)
        rev = oracle.create_entry(
            target, data=data, scope="alpha-notes", filename="gadget-one.md"
        )
        assert target.read_bytes() == data, tag
        assert rev == oracle.entry_revision(data), tag
        # …and a replace of the same file with the same term.
        again = _entry_bytes(f"{tag}, {tag}")
        oracle.replace_entry(
            target,
            data=again,
            if_match=[rev],
            scope="alpha-notes",
            filename="gadget-one.md",
        )
        assert target.read_bytes() == again, tag

    # An entry carrying NO `tags:` at all, which is every entry in the store today.
    root = tmp_path / "world-untagged"
    target = root / "alpha-notes" / "gadget-one.md"
    target.parent.parent.mkdir(parents=True)
    oracle.create_entry(
        target, data=_entry_bytes(""), scope="alpha-notes", filename="gadget-one.md"
    )
    assert target.exists()


def test_the_oracle_sees_the_FOLDED_tag(oracle, tmp_path: Path):
    """🔴 THE FOLD IS WHY THIS IS NOT A CASE-SENSITIVE ALLOWLIST. An operator writes
    `tags: [Infra]` or `tags: [ToOlInG]`; `normalize_ref` lowercases and trims, so both are
    declared terms by the time the comparison happens.

    ⚠ The `_`→`-` half of the fold is measured by the REFUSAL below rather than here: all
    three declared terms are single words, so no underscore spelling of a declared term
    exists to accept. `Not_Infra` folding to `not-infra` is what exercises it."""
    for n, raw in enumerate(("Infra", " infra ", "INFRA", "Product", "ToOlInG", "  tooling")):
        target = tmp_path / f"fold-{n}" / "alpha-notes" / "gadget-one.md"
        target.parent.parent.mkdir(parents=True)
        oracle.create_entry(
            target,
            data=_entry_bytes(raw),
            scope="alpha-notes",
            filename="gadget-one.md",
        )
        assert target.exists(), raw
    # …and the refusal names the FOLDED spelling, not what the file wrote, because the
    # folded form is the only one the store would ever have carried.
    target = tmp_path / "fold-bad" / "alpha-notes" / "gadget-one.md"
    target.parent.parent.mkdir(parents=True)
    with pytest.raises(oracle.EntryShapeError) as caught:
        oracle.create_entry(
            target,
            data=_entry_bytes("Not_Infra"),
            scope="alpha-notes",
            filename="gadget-one.md",
        )
    assert "tag 'not-infra' is not one of" in str(caught.value), str(caught.value)


def test_the_oracles_two_write_primitives_share_ONE_validator(oracle):
    """🔴 THE SEAM, PINNED BY SOURCE RATHER THAN BY BEHAVIOUR, AND IT FAILS BOTH WAYS.

    The predicate was open-coded at both write primitives before this change — identical
    down to the refusal string — and a rule spelled twice is a rule wrong at one of the
    two sites. `_validate_entry_bytes` is now the single spelling, and the hazard is the
    CALLER SET moving: shrinking it lands an off-vocabulary tag, and GROWING it into
    `append_bullet` — which looks symmetrical and is not — would make every append to an
    entry that already carries one fail, the exact outage the vocabulary was kept out of
    the reader to avoid.

    ⚠ IT READS FUNCTION BODIES BY AST, so a mention in a docstring or a comment can
    neither satisfy nor break it. This is the Python twin of
    `TestTheWriteTimeValidatorHasExactlyTheDeclaredCallers`."""
    import ast

    declared = {"replace_entry", "create_entry"}
    tree = ast.parse((REPO / "server" / "server.py").read_text(encoding="utf-8"))
    callers: dict[str, int] = {}
    for node in ast.walk(tree):
        if not isinstance(node, (ast.FunctionDef, ast.AsyncFunctionDef)):
            continue
        for inner in ast.walk(node):
            if (
                isinstance(inner, ast.Call)
                and isinstance(inner.func, ast.Name)
                and inner.func.id == "_validate_entry_bytes"
            ):
                callers[node.name] = callers.get(node.name, 0) + 1
    # The POSITIVE CONTROL on the walker: a zero is indistinguishable from an AST walk
    # wired to nothing.
    assert callers, (
        "the AST walk found NO call to `_validate_entry_bytes` anywhere in server.py. "
        "Either every write primitive stopped validating, or this walker is broken — "
        "check the second before believing the first."
    )
    undeclared = sorted(set(callers) - declared)
    assert not undeclared, (
        f"{undeclared} call(s) `_validate_entry_bytes` and are not declared callers. "
        f"Adding a caller is a DECISION, not a tidy-up: the validator refuses a `tags:` "
        f"outside the closed vocabulary, so any function that starts calling it starts "
        f"failing on entries that already carry one. `append_bullet` is the specific "
        f"mistake this guard exists for. If the addition is right, add it to `declared` "
        f"here and say why in the commit."
    )
    missing = sorted(declared - set(callers))
    assert not missing, (
        f"{missing} declare(s) no call to `_validate_entry_bytes`, so caller-supplied "
        f"bytes reach the store without the loader check OR the closed tag vocabulary. "
        f"If a function was renamed, rename it in `declared` here too."
    )
    # And the function it names really is defined in the module under test, so the ledger
    # is about live code rather than about a name nothing binds.
    assert callable(oracle._validate_entry_bytes)
