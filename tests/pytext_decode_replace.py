#!/usr/bin/env python3
"""Generate `internal/pytext/testdata/decode_replace.json` — the ORACLE for
`pytext.DecodeUTF8Replace`, which is `bytes.decode("utf-8", errors="replace")`.

🔴 WHY A GENERATED FIXTURE AND NOT A HAND-WRITTEN TABLE. The rule CPython actually
implements is **substitution of maximal subparts** (Unicode TR#36 / WHATWG): the
decoder emits ONE U+FFFD for each maximal prefix of a would-be-valid sequence, not
one per undecodable byte. Nobody can transcribe that from the prose — the branch
structure decides it, and the only instrument is running both over the same bytes.
The Go side's first version emitted one U+FFFD per byte, which is RIGHT for a lone
`0x80` and WRONG for `b"\\xe2\\x82"`, so every ASCII-plus-one-stray-byte example
agreed and the shapes that matter did not.

⚠ THE FIXTURE IS TWO INSTRUMENTS, NOT ONE, AND THE SECOND IS THE ONE WITH REACH:

  * `cases` is a small NAMED table — reviewable by eye, and each row says which
    branch it is about. It is what a human reads when the Go test goes red.
  * `sweep` is a DIGEST over tens of thousands of inputs enumerated by a rule both
    sides spell. A table that size is unreviewable, and a digest is unreadable, so
    the pair is deliberate: the table localises a failure and the digest finds one.
    `sweep.count` is pinned too, because a digest over an empty enumeration is a
    perfectly stable digest over nothing.

⚠ AND A DIGEST CANNOT SAY WHICH INPUT MOVED, so `sweep.samples` carries CPython's
exact answer for every `SAMPLE_STRIDE`-th input of the enumeration. That is the row
a red Go test can quote: it is an expected VALUE, not a hash, and it is drawn from
the same walk rather than hand-picked, so it cannot be a sample of only the shapes
somebody already suspected.

🔴 THE ENUMERATION RULE IS THE CONTRACT, AND BOTH SIDES MUST SPELL IT IDENTICALLY.
It is written out in `GROUPS` below and transcribed in
`internal/pytext/decodereplace_test.go`. A disagreement about the enumeration is a
RED test that is not about the decoder, which is why `count` is pinned beside the
digest: a count that moves says "the enumeration drifted", a digest that moves with
the count intact says "a decoded byte moved".

🔴 REGENERATE AND DIFF, NEVER HAND-EDIT — the same rule
`internal/report/testdata/reader_fixtures.json` and
`internal/store/testdata/citation_ids.json` carry, AND IT IS NOW ENFORCED THE SAME WAY
THEY ARE. `tests/test_pytext_decode_replace.py` re-runs this script in a subprocess on
every pytest run and compares BYTES. ⚠ Until that file existed the rule above was a
REQUEST: nothing in `pytest tests` read this fixture, so the Go tier replayed committed
bytes and nobody ever re-asked the interpreter — measured, by hand-editing a digest and
one `output_hex` with the guard absent and watching a full `pytest tests` report NOT ONE
failure about it. (That run was not green outright: it carried 10 unrelated failures
from the scratch copy having no `.git`. "Reported nothing about this fixture" is the
claim; "green" would be a wider one than was measured.)

    python3 tests/pytext_decode_replace.py > internal/pytext/testdata/decode_replace.json
"""
from __future__ import annotations

import hashlib
import json
import sys

#: Named rows: (name, input bytes, what branch it is about). The expected OUTPUT is
#: never written here — it is whatever CPython answers, which is the whole point.
CASES: tuple[tuple[str, bytes, str], ...] = (
    ("valid-ascii", b"- a lesson.\n", "the fast path: valid in, identical out"),
    ("valid-multibyte", "- café 🔴 — ok\n".encode("utf-8"),
     "a valid 2/3/4-byte sequence must survive byte-for-byte"),
    ("lone-continuation-byte", b"\x80",
     "an invalid START byte: one U+FFFD, and the per-byte rule agrees here"),
    ("two-lone-continuation-bytes", b"\x80\x80",
     "TWO invalid starts, so TWO U+FFFD — neither is a prefix of the other"),
    ("truncated-two-byte-lead", b"\xc2",
     "a 2-byte lead with nothing after it: ONE maximal subpart"),
    ("two-byte-lead-then-ascii", b"\xc2A",
     "the lead is the maximal subpart; the `A` decodes normally"),
    ("overlong-two-byte-c0", b"\xc0\x80",
     "0xC0/0xC1 are invalid STARTS, so the continuation is its own subpart"),
    ("overlong-two-byte-c1", b"\xc1\xbf", "the other overlong 2-byte lead"),
    ("truncated-three-byte-at-one", b"\xe2",
     "a 3-byte lead alone: ONE subpart"),
    ("truncated-three-byte-at-two", b"\xe2\x82",
     "🔴 THE SHAPE THE PER-BYTE RULE GETS WRONG: lead + one valid continuation is "
     "ONE maximal subpart, so ONE U+FFFD, not two"),
    ("truncated-three-byte-then-ascii", b"\xe2\x82A",
     "the 2-byte prefix is one subpart and the `A` is kept"),
    ("overlong-three-byte", b"\xe0\x80\x80",
     "0xE0 with a continuation below 0xA0 is overlong: the lead alone is the subpart"),
    ("surrogate-three-byte", b"\xed\xa0\x80",
     "0xED with a continuation at/above 0xA0 encodes a surrogate: lead-only subpart"),
    ("truncated-four-byte-at-three", b"\xf0\x9f\x98",
     "lead + two valid continuations is ONE subpart"),
    ("truncated-four-byte-then-ascii", b"\xf0\x9f\x98A",
     "the 3-byte prefix is one subpart and the `A` is kept"),
    ("overlong-four-byte", b"\xf0\x80\x80\x80",
     "0xF0 with a continuation below 0x90 is overlong"),
    ("beyond-max-codepoint", b"\xf4\x90\x80\x80",
     "0xF4 with a continuation at/above 0x90 is past U+10FFFF"),
    ("invalid-start-f5", b"\xf5\x80\x80\x80",
     "0xF5..0xFF is never a lead, so each byte is its own subpart"),
    ("invalid-start-ff", b"\xff\xfe", "the two bytes a UTF-16 BOM would start with"),
    ("mixed-run", b"ok \xe2\x82 then \x80\x80 and \xf0\x9f\x98 end",
     "several subparts in one string, so the OFFSETS of the replacements matter"),
    # 🔴 THE WORKED EXAMPLE THE CITATION-ID DIVERGENCE WAS MEASURED ON. One entry
    # file's bullet, three decoders, three different ids — the Go per-byte rule being
    # the odd one out. Carried here so the shape has a named row rather than only a
    # digest.
    ("journal-bullet-with-a-truncated-sequence", b"- a\xe2\x82\n  tail\n",
     "a bullet whose prose carries a truncated 3-byte sequence"),
)


#: 🔴 THE SWEEP ENUMERATION, SPELLED ONCE HERE AND TRANSCRIBED IN GO. Each group is
#: (name, generator of byte strings). The order is part of the contract: the digest
#: is over the concatenation in exactly this order.
#:
#: The leads and continuation probes are not a random sample — each is a BOUNDARY of
#: a branch in CPython's decoder: 0xE0 (overlong gate), 0xED (surrogate gate), 0xEF
#: (ordinary 3-byte), 0xF0 (overlong gate), 0xF4 (max-codepoint gate), 0xF5 (never a
#: lead), with 0xE1/0xF1 as the ordinary middles. ⚠ Boundaries alone would be a
#: sweep that only sees what someone already suspected, so every group also walks a
#: FULL 0x00..0xFF byte position.
def _groups():
    def singles():
        for b in range(256):
            yield bytes([b])

    def two_byte():
        for a in range(0x80, 0x100):
            for b in range(0x100):
                yield bytes([a, b])

    def two_byte_embedded():
        # The same pairs with ASCII on both sides: a decoder that gets the SUBPART
        # LENGTH right but the resume OFFSET wrong is invisible without this.
        for a in range(0x80, 0x100):
            for b in range(0x100):
                yield b"x" + bytes([a, b]) + b"z"

    def three_byte():
        for a in (0xE0, 0xE1, 0xED, 0xEF):
            for b in range(0x100):
                for c in (0x00, 0x41, 0x80, 0xA0, 0xBF, 0xC0, 0xFF):
                    yield bytes([a, b, c])

    def four_byte():
        for a in (0xF0, 0xF1, 0xF4, 0xF5):
            for b in range(0x100):
                for c in (0x41, 0x80, 0xBF):
                    for d in (0x41, 0x80, 0xBF):
                        yield bytes([a, b, c, d])

    return (
        ("single-bytes", singles),
        ("two-byte-leads", two_byte),
        ("two-byte-leads-embedded-in-ascii", two_byte_embedded),
        ("three-byte-leads", three_byte),
        ("four-byte-leads", four_byte),
    )


GROUPS = _groups()


#: Every Nth input of the whole enumeration is carried with its answer. A prime, so
#: the sample does not land on a period of any group's inner loops — a stride of 256
#: over `two-byte-leads` would only ever sample one value of the trailing byte.
SAMPLE_STRIDE = 673


def sweep() -> tuple[int, str, list[dict], list[dict]]:
    """The digest, the count, a per-group breakdown, and the strided samples.

    The per-group counts are carried so a count mismatch names WHICH group drifted;
    with one total only, "the enumeration moved" is all a red test could say.
    """
    digest = hashlib.sha256()
    total = 0
    breakdown = []
    samples: list[dict] = []
    for name, gen in GROUPS:
        n = 0
        for raw in gen():
            out = raw.decode("utf-8", errors="replace")
            # Encoding the OUTPUT back is lossless: `replace` never emits a
            # surrogate, so the decoded string is always encodable as strict UTF-8.
            digest.update(raw.hex().encode("ascii"))
            digest.update(b":")
            digest.update(out.encode("utf-8").hex().encode("ascii"))
            digest.update(b"\n")
            if total % SAMPLE_STRIDE == 0:
                samples.append({
                    "index": total,
                    "group": name,
                    "input_hex": raw.hex(),
                    "output_hex": out.encode("utf-8").hex(),
                })
            n += 1
            total += 1
        breakdown.append({"name": name, "count": n})
    return total, digest.hexdigest(), breakdown, samples


def main() -> int:
    total, digest, breakdown, samples = sweep()
    cases = []
    for name, raw, about in CASES:
        out = raw.decode("utf-8", errors="replace")
        cases.append({
            "name": name,
            "about": about,
            # 🔴 HEX, NOT A JSON STRING. The input is not valid UTF-8 by
            # construction, so no JSON decoder could carry it as text — and the
            # OUTPUT is hex for the same reason the input is: a `�` pasted into
            # a JSON string is indistinguishable on screen from two of them.
            "input_hex": raw.hex(),
            "output_hex": out.encode("utf-8").hex(),
            "replacement_count": out.count("�"),
        })
    doc = {
        "_comment": (
            "GENERATED by tests/pytext_decode_replace.py from CPython's own "
            "bytes.decode('utf-8', errors='replace'), and replayed by "
            "internal/pytext's test. Regenerate and diff; never hand-edit."
        ),
        "cases": cases,
        "sweep": {
            "_comment": (
                "A digest over the enumeration spelled in GROUPS, in that order, as "
                "'<input hex>:<output hex>\\n' per case. The count is pinned beside "
                "it because a digest over an empty enumeration is perfectly stable."
            ),
            "count": total,
            "digest": digest,
            "groups": breakdown,
            "sample_stride": SAMPLE_STRIDE,
            "samples": samples,
        },
    }
    json.dump(doc, sys.stdout, ensure_ascii=False, indent=2, sort_keys=False)
    sys.stdout.write("\n")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
