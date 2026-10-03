#!/usr/bin/env python3
"""Generate `internal/store/testdata/citation_ids.json` — the citation-id ORACLE.

🔴 WHY A GENERATED FIXTURE RATHER THAN TWO TEST SUITES. `JournalBullet.citation_id`
exists in BOTH languages (`lib/subsystem_resolver.py` and `internal/store`), because
`packages.cairn` installs the Python client and its `lib/` under `libexec` and nothing
else — so `lib/` CANNOT import `internal/`. That is packaging, not a choice anyone may
undo here, and it is the same shape `AGENTS.md` already blesses for
`server/Dockerfile` against `flake.nix`'s `serverEnv`.

Two independent implementations of a hash must agree FOREVER, or the id stops being a
shared name for a bullet and silently becomes two. This file writes the PYTHON answers
down; `internal/store`'s test replays the same bodies and asserts the same strings. So:

  * a change to either implementation turns the Go test red;
  * a REGENERATION that moves an id shows up as a diff in review, not as a green run.

⚠ IT PINS THE ID, NOT THE PARSE. The fixture also carries each bullet's `start_line`
and `lines`, so a parser change that re-groups bullets is caught too — but the bodies
below are hand-written, so this says nothing about the LIVE corpus. The differential
reader fixture (`tests/reader_fixtures.py`) is what compares rendered bytes over a
synthetic world, and `tests/parity/harness.py` is what compares the two real clients.

🔴 EVERY BODY IS **BYTES**, AND THAT IS NOT A STYLE CHOICE. An earlier revision wrote
the bodies as `str` and carried only the decoded text, so the Go side replayed a string
PYTHON had already decoded — which made the fixture structurally unable to see a decode
difference, and a decode difference is exactly what shipped: Go's
`pytext.DecodeUTF8Replace` emitted one U+FFFD per invalid BYTE where CPython emits one
per maximal SUBPART, so one entry file produced two different citation ids. The fixture
therefore carries `body_hex` (the raw bytes) beside `body` (their `errors="replace"`
decode), and `internal/store`'s test decodes the HEX with `store.DecodeReplace` and
asserts it equals `body` before comparing an id. A JSON string cannot hold invalid
UTF-8 at all, so hex is also the only way these cases can exist.

🔴 WHICH DECODE, AND WHY IT IS THE CONTRACT. `errors="replace"` is what the Python
client's read path uses (`read_text(encoding="utf-8", errors="replace")`) and what
Go's `store.DecodeReplace` does. The POD decodes `errors="surrogateescape"` instead,
and `JournalBullet.citation_id` coerces that form back to this one before hashing —
see `_citation_hash_bytes` in `lib/subsystem_resolver.py`. So one entry file has ONE id
across all three paths, and `tests/test_citation_ids.py` is where the pod's path is
asserted against this fixture's answers.

🔴 REGENERATE AND DIFF, NEVER HAND-EDIT — the same rule
`internal/report/testdata/reader_fixtures.json` carries. A hand-edited oracle agrees
with nothing.

    python3 tests/citation_ids.py > internal/store/testdata/citation_ids.json
"""
from __future__ import annotations

import json
import sys
from pathlib import Path

REPO = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(REPO / "lib"))

from subsystem_resolver import parse_journal_bullets  # noqa: E402

#: Each case is (name, body BYTES). Chosen so every rule the parser states is
#: exercised, because an id is only as stable as the grouping it is computed over.
CASES: tuple[tuple[str, bytes], ...] = (
    (
        "one-single-line-bullet",
        b"- 2026-01-01: a lesson.\n",
    ),
    (
        "wrapped-bullet-is-ONE-bullet",
        # The median live bullet is 3 lines. A per-line id would differ here, which
        # is exactly the mistake this case exists to catch.
        b"- 2026-01-01: a lesson that wraps\n  onto a second line\n  and a third.\n",
    ),
    (
        "two-bullets-with-IDENTICAL-opening-lines",
        # 🔴 THE CASE A MAP KEYED ON THE OPENING LINE GETS WRONG. Both bullets open
        # identically and differ only in their continuation, so they must receive
        # DIFFERENT ids and ASCENDING start_lines.
        b"- same opening\n  tail A\n- same opening\n  tail B\n",
    ),
    (
        "text-before-the-first-bullet-is-DROPPED",
        # The documented `dropped-lines` shape: the preamble belongs to no bullet, so
        # start_line of the first bullet is NOT 0.
        b"preamble prose nobody can address\n- 2026-01-01: the only bullet.\n",
    ),
    (
        "a-dash-line-INSIDE-A-FENCE-is-not-a-bullet",
        # 🔴 THE REASON `start_line` IS EXPOSED AT ALL, AND THE DASH IS AT COLUMN 0
        # DELIBERATELY. An earlier revision indented the fenced dash two spaces —
        # which the bullet pattern `^[-*][ \t]+` cannot match ANYWAY, because it is
        # anchored at column 0. So the case was green whether the fence rule existed
        # or not: MEASURED by disabling `_is_fence`/`IsFence`, which left every
        # citation guard PASSING. At column 0 the rule is load-bearing — ON gives 2
        # bullets with starts [0, 4], OFF gives 3 with starts [0, 2, 4].
        b"- real bullet\n```\n- not a bullet\n```\n- second real bullet\n",
    ),
    (
        "trailing-blank-lines-are-stripped-from-lines",
        # So `len(lines)` is NOT the distance to the next start_line — the property the
        # field's docstring warns about.
        b"- first\n\n\n- second\n",
    ),
    (
        "markers-are-just-BYTES-so-they-DO-change-the-id",
        # 🔴 THE NAME IS THE CLAIM, AND THE PREVIOUS ONE ASSERTED THE OPPOSITE OF THE
        # TRUTH: it read `markers-do-not-change-the-id-derivation`, which reads as
        # "a marker does not change the id". It does — the marker is part of the
        # opening line's bytes. What does not change is the DERIVATION. Measured:
        # `- 2000-01-02: a thing` is 76426877 and `- 2000-01-02: OPEN: a thing` is
        # b8b87896. Included so a future "hash the semantic fields instead" fails.
        b"- 2026-01-01: OPEN: unfinished.\n- 2026-01-02: RESOLVED abc1234: done.\n",
    ),
    (
        "unicode-and-emoji-survive-the-utf8-encode",
        # Both sides must hash UTF-8 BYTES. A latin-1 or UTF-16 encode on either side
        # diverges only on non-ASCII, which ASCII-only cases cannot see.
        "- 🔴 a lesson with émoji and accents — and an en dash.\n".encode("utf-8"),
    ),
    (
        "empty-body-yields-no-bullets",
        b"",
    ),
    # ── the FIVE cases the first revision of this fixture could not hold: four
    #    carry invalid UTF-8, which a `str` body cannot, and the fifth carries a
    #    NON-newline line break, which no case did ─────────────────────────────────
    (
        "a-TRUNCATED-multibyte-sequence-in-a-bullet",
        # 🔴 THE WORKED EXAMPLE OF THE MEASURED DIVERGENCE. `\xe2\x82` is the first two
        # bytes of a 3-byte sequence, so CPython's `replace` emits ONE U+FFFD for the
        # pair and Go's old per-byte rule emitted TWO — two different `lines`, two
        # different ids for one file. This case is RED on the pre-fix Go decoder and
        # green after, which is the only reason it is worth its row.
        b"- a\xe2\x82\n  tail\n",
    ),
    (
        "a-LONE-continuation-byte-in-a-bullet",
        # The shape that AGREED under both rules, carried so the pair is a contrast
        # rather than a single example: an invalid START byte is one subpart either way.
        b"- a lesson with a \x80 stray byte\n",
    ),
    (
        "an-OVERLONG-sequence-spans-three-replacements",
        # `\xe0\x80\x80` is three subparts, not one: 0xE0 followed by a byte below 0xA0
        # is overlong, so the lead alone is the subpart and each 0x80 after it is its
        # own invalid start. A "replace the whole invalid RUN" rule gives ONE here.
        b"- a lesson with \xe0\x80\x80 overlong bytes\n",
    ),
    (
        "an-invalid-byte-on-a-CONTINUATION-line",
        # The id is over every line, so a malformed byte in the TAIL must move it too.
        b"- a lesson that wraps\n  onto a \xf0\x9f\x98 truncated tail\n",
    ),
    (
        "a-NON-newline-line-break-inside-a-bullet",
        # 🔴 THE CASE A CONSUMER USING THE WRONG SPLITTER GETS WRONG. `splitlines()`
        # breaks on TEN characters; `split("\n")` breaks on one. On this body the two
        # disagree: `splitlines()` gives ['- a', 'b', '- c'] with starts [0, 2], while
        # `split("\n")` gives ['- a\rb', '- c', ''] — so start_line 0 would point
        # MID-LINE and start_line 2 at the empty string. `internal/ui/render.go`'s
        # `inlineCode` already reaches for the wrong one.
        b"- a\rb\n- c\n",
    ),
)


def main() -> int:
    cases = []
    for name, raw in CASES:
        # The CONTRACT decode. See the module docstring: this is what the Python
        # client's read path and Go's `store.DecodeReplace` both do, and what the
        # pod's `surrogateescape` text is coerced to before hashing.
        body = raw.decode("utf-8", errors="replace")
        bullets = parse_journal_bullets(body)
        # 🔴 THE POD'S DECODE, ASSERTED HERE RATHER THAN ONLY IN A TEST. These bytes
        # are what an entry file holds, so the generator itself refuses to write a
        # fixture whose ids depend on which handler read the file.
        pod = parse_journal_bullets(raw.decode("utf-8", errors="surrogateescape"))
        pod_ids = [b.citation_id for b in pod]
        if pod_ids != [b.citation_id for b in bullets]:
            raise SystemExit(
                f"{name}: the pod's surrogateescape decode yields ids {pod_ids} and "
                f"the client's replace decode {[b.citation_id for b in bullets]} — one "
                "entry file would have two citation ids")
        cases.append({
            "name": name,
            # 🔴 HEX FIRST: a JSON string cannot carry invalid UTF-8, and a decoder
            # that could would be free to normalise it. `body` is the decode of
            # `body_hex`, carried too so a human can read the case and so the Go side
            # can assert its own decoder reproduces it.
            "body_hex": raw.hex(),
            "body": body,
            "bullets": [
                {
                    "start_line": b.start_line,
                    "lines": list(b.lines),
                    "citation_id": b.citation_id,
                }
                for b in bullets
            ],
        })
    doc = {
        "_comment": (
            "GENERATED by tests/citation_ids.py from the PYTHON implementation, and "
            "replayed by internal/store's test. Regenerate and diff; never hand-edit."
        ),
        "cases": cases,
    }
    json.dump(doc, sys.stdout, ensure_ascii=False, indent=2, sort_keys=False)
    sys.stdout.write("\n")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
