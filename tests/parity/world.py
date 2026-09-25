#!/usr/bin/env python3
"""The synthetic world the parity harness runs both clients over.

🔴 EVERY NAME AND EVERY DATE HERE IS SYNTHETIC. This repository is PUBLIC and was extracted
from a private one; scopes are `alpha-notes` / `beta-notes` / `rubble-heap`, entries are
`widget-cfg` / `ledger-svc` / …, and every timestamp is an offset from 2000-01-01, which is
what `tests/leakscan.py` allows and what makes a real date in here unambiguous.

🔴 THE WORLD IS BUILT TO MAKE DIFFERENCES OBSERVABLE, NOT TO LOOK REALISTIC. Specifically:

  * two entries whose mtimes differ only in the FRACTION of one second, so the featured
    pick's tie-break is exercised rather than decided by whole seconds;
  * a scope holding files and NOTHING indexable, so `scope-unreadable` (exit 3) and the
    warning sentence are reached;
  * a malformed entry BESIDE readable ones, which is a different branch again;
  * a scope directory that exists and is EMPTY, so `scope-empty` is reachable;
  * a git repo with a handoff doc quoting one entry's path, so the focus-window selector
    RESOLVES — the code path P1b refused and P2 had to port. Without this the parity run
    would measure the fallback on both sides and prove nothing about the matcher;
  * 🔴 SCOPE-POLICY SHEETS (`README.md`) AND TWO LOOKALIKES, because without them this gate
    was STRUCTURALLY BLIND to an entire defect class. `README.md` is a scope's policy sheet
    and not an entry — both loaders skip it — but the rule was open-coded at four production
    sites and WRONG at two of them, so `cairn ls-entries` listed every scope's sheet as an
    entry. Both clients did it IDENTICALLY, so a byte-identity gate sat green over the
    miscount for as long as the corpus seeded no README anywhere. The corpus never presented
    the discriminating input. It does now, and a one-sided fix is RED.
"""
from __future__ import annotations

import os
import subprocess
from pathlib import Path

#: 2000-01-01T00:00:00Z. Every mtime is this plus an offset in nanoseconds.
EPOCH_NS = 946_684_800 * 1_000_000_000


def _entry(service: str, scope: str, *, aliases: str = "", body: str = "a synthetic entry") -> str:
    alias_line = f"aliases: [{aliases}]\n" if aliases else ""
    return (
        "---\n"
        f"service: {service}\n"
        f"scope: {scope}\n"
        f"{alias_line}"
        "---\n"
        "\n"
        "## What it is\n"
        "\n"
        f"{body}.\n"
        "\n"
        "## Pointers\n"
        "\n"
        "- `apps/" + service + "/values.yaml`\n"
        "\n"
        "## Nuance / work-history\n"
        "\n"
        "- 2000-01-02: OPEN: the synthetic action this entry records.\n"
        "- 2000-01-03: RESOLVED abc1234: the synthetic action that closed.\n"
    )


ENTRIES: list[tuple[str, int, str]] = [
    # (path, mtime offset ns, text)
    ("alpha-notes/widget-cfg.md", 250_000_000, _entry("widget-cfg", "alpha-notes")),
    # 🔴 THE SAME WHOLE SECOND AS `widget-cfg`, A DIFFERENT FRACTION. Two entries that tie on
    # a truncated mtime fall through to the ref, and getting that wrong produces a different
    # ORDER with no error and no missing entry — which reads as a stale cache.
    ("alpha-notes/ledger-svc.md", 750_000_000, _entry("ledger-svc", "alpha-notes",
                                                      aliases="ledger-holder")),
    ("alpha-notes/gauge-api.md", 4_000_000_000, _entry("gauge-api", "alpha-notes")),
    # A malformed entry BESIDE readable ones: `aliases:` as a bare string is what the schema
    # refuses, and the rejection has to render in the same report as the good entries.
    ("alpha-notes/broken-four.md",
     5_000_000_000,
     "---\nservice: broken-four\nscope: alpha-notes\n"
     "aliases: a bare string, which the schema refuses\n---\n\n"),
    ("beta-notes/spindle-cfg.md", 6_000_000_000, _entry("spindle-cfg", "beta-notes",
                                                        body="the second scope's plain entry")),
    # A scope holding files and NOT ONE indexable: `scope-unreadable`, exit 3, warning line.
    # 🔴 IT IS ALSO THE README-FREE CONTROL. Both its files are listed by `ls-entries`, so a
    # client that implemented "not an entry" as "drop one file per scope" — the arithmetic a
    # corpus of README-bearing scopes alone cannot tell apart from the rule — loses one of
    # these and diverges here.
    ("rubble-heap/rubble-one.md", 7_000_000_000, "this is not an entry\n"),
    ("rubble-heap/rubble-two.md", 8_000_000_000, "neither is this\n"),
    # --- the scope-policy sheets, and the lookalikes that are NOT sheets ----------------
    #
    # 🔴 A SHEET IS SKIPPED BY BOTH LOADERS AND MUST NOT BE LISTED BY `ls-entries`. Two of
    # them, in two different scopes, so a client that special-cased one scope is visible.
    # They are deliberately NOT entry-shaped: the loader never opens them, and a sheet that
    # parsed would make this row a second sample of the ordinary entry case.
    ("alpha-notes/README.md", 9_000_000_000,
     "# alpha-notes — the scope's own policy sheet, not an entry\n"),
    ("beta-notes/README.md", 10_000_000_000,
     "# beta-notes — the scope's own policy sheet, not an entry\n"),
    # 🔴 THE LOOKALIKES, AND THEY ARE ORDINARY ENTRIES THAT MUST BE LISTED. The rule is
    # `== "README.md"` EXACTLY — not a prefix, not a case fold — and that claim is only a
    # comment until a corpus holds a file that a prefix or a fold would swallow. `service:`
    # is each file's own slug, or the loader rejects the entry ("a ref reaches the wrong
    # file") and the row stops being about listing at all.
    #
    # ⚠ `README.md` AND `readme.md` SHARE `beta-notes`, WHICH IS TWO FILES ON LINUX AND ONE
    # ON A CASE-FOLDING FILESYSTEM. CI is `ubuntu-latest`; `build_store` asserts both landed
    # rather than leaving a silently-collapsed corpus to score a pass.
    ("beta-notes/readme.md", 11_000_000_000, _entry("readme", "beta-notes",
                                                    body="a lookalike that IS an entry")),
    ("beta-notes/README-old.md", 12_000_000_000, _entry("readme-old", "beta-notes",
                                                        body="a prefix lookalike, also an entry")),
    # --- the WRITE-PROTOCOL scope: content a reader cannot reach ------------------------
    #
    # 🔴 A SCOPE WHOSE ENTRIES PARSE AND STILL HOLD LOST CONTENT, because without one this
    # gate was STRUCTURALLY BLIND to the whole `dropped lines:` / `marker reachability:`
    # half of `validate`. Every other entry in this world has a well-formed nuance section,
    # so both advisories would print their ZERO branch on every row — and a client that
    # implemented neither, or implemented one of them differently, would compare equal for
    # as long as the corpus never presented the discriminating input.
    #
    # `talus-svc` carries BOTH defects at once, and they are different defects:
    #   * two lines BEFORE the first bullet, which `parse_journal_bullets` drops — the
    #     second of them a `OPEN:` DECLARATION, so the `carries_marker` flag is exercised
    #     rather than merely defined;
    #   * a correctly-spelled `OPEN:` on a bullet's CONTINUATION line, which every marker
    #     reader is anchored past.
    # `scree-api` is its clean sibling, so the denominator this scope prints is LARGER THAN
    # THE NUMBER OF FINDINGS and a client that counted FINDINGS where it should count FILES
    # is visible.
    #
    # ⚠ AND THE DENOMINATOR IS DELIBERATELY NOT WRITTEN DOWN HERE ANY MORE. It is the count
    # of entry files in THIS SCOPE, so every row appended below moves it: this comment read
    # "the denominator this scope prints is 2 rather than 1" and was stale three entries
    # later, ~37 lines above the addition that invalidated it. Nothing in this repo pins it
    # either — the harness diffs the TWO CLIENTS against each other live and stores no
    # golden, so a number transcribed into a comment here is the only copy and has no
    # checker. What the fixture pins is the PROPERTY: files, not findings.
    ("crag-notes/talus-svc.md", 13_000_000_000,
     "---\nservice: talus-svc\nscope: crag-notes\n---\n"
     "\n## What it is\n\n"
     "a synthetic entry whose nuance section lost a bullet opening.\n"
     "\n## Pointers\n\n"
     "- `apps/talus-svc/values.yaml`\n"
     "\n## Nuance / work-history\n\n"
     "  this line reaches no bullet at all: the `- ` that opened it is gone.\n"
     "  OPEN: and this one is a declaration nothing will ever surface.\n"
     "- 2000-01-04: RESOLVED abc1234: the bullet that did survive.\n"
     "  OPEN: a marker several lines in, where no parser looks.\n"),
    ("crag-notes/scree-api.md", 14_000_000_000, _entry("scree-api", "crag-notes",
                                                       body="the clean sibling of a lossy entry")),
    # --- the SHAPE and OPEN-ACTION halves of the same write-protocol report ------------
    #
    # 🔴 TWO MORE ENTRIES, BECAUSE THE TWO FILES ABOVE EXERCISE ONLY THE ZERO BRANCH OF
    # BOTH NEW BLOCKS AND THE CORPUS WOULD OTHERWISE BE BLIND TO THEM THE SAME WAY IT WAS
    # BLIND TO THE OTHER TWO. `talus-svc` and `scree-api` both carry an exact spine and no
    # reportable bullet, so `entry shape:` and `open actions` would print `each present
    # exactly once` / `0 declared` on every row in the world — and a client that
    # implemented neither, or implemented one of them differently, would compare equal for
    # as long as nothing presented the discriminating input. These two present it: between
    # them they produce ALL FOUR shape kinds and ALL FOUR open-action populations, each in
    # its own rendered sub-block.
    #
    # `moraine-cfg` is the SHAPE file, and it carries three of the four kinds at once:
    #   * `## pointers` — a CASE near-miss, so RENAMED rather than absent: the report has
    #     to print the heading the writer actually typed, not just "it is missing";
    #   * the nuance heading written TWICE with nothing under either — DUPLICATED *and*
    #     EMPTY together, which is the disjointness claim made observable. A client whose
    #     two branches excluded each other prints one of them and compares RED.
    # It contributes nothing to the other three blocks, which is itself the ordering
    # argument: its nuance section is unreachable, so `dropped lines`, `open actions` and
    # `marker reachability` are each silent about a file that is badly broken.
    ("crag-notes/moraine-cfg.md", 15_000_000_000,
     "---\nservice: moraine-cfg\nscope: crag-notes\n---\n"
     "\n## What it is\n\n"
     "a synthetic entry whose spine departs from the schema three ways at once.\n"
     "\n## pointers\n\n"
     "- `apps/moraine-cfg/values.yaml`\n"
     "\n## Nuance / work-history\n"
     "\n## Nuance / work-history\n"),
    # `cirque-api` is the OPEN-ACTION file, and it is the fourth shape kind at the same
    # time: it has NO `## Pointers` under any spelling this tool can pair with one, so the
    # report prints its heading INVENTORY instead of a near-miss. Its nuance section is
    # exact and non-empty, so all four openness populations render:
    #   * a declared `OPEN:` — exact, the writer said so;
    #   * an emphasised `**OPEN**:` — a near-miss, a write that did not land;
    #   * `Open items:` prose — the unmarked FLOOR with unknown recall;
    #   * a `RESOLVED:` naming no sha — an unverifiable closure.
    # A fifth bullet is a `RESOLVED <sha>:`, population `resolved`, which must be reported
    # by NOTHING — the control that stops this row passing a client which reported every
    # bullet it saw.
    ("crag-notes/cirque-api.md", 16_000_000_000,
     "---\nservice: cirque-api\nscope: crag-notes\n---\n"
     "\n## What it is\n\n"
     "a synthetic entry carrying every openness population at once.\n"
     "\n## Nuance / work-history\n\n"
     "- 2000-01-05: OPEN: the writer declared this one, exactly.\n"
     "- 2000-01-06: **OPEN**: emphasis, so the marker never parses.\n"
     "- 2000-01-07: Open items: the retry budget is not yet addressed.\n"
     "- 2000-01-08: RESOLVED: closed, and naming no sha at all.\n"
     "- 2000-01-09: RESOLVED abc1234: closed and verifiable, reported by nothing.\n"),
    # --- the CASE FOLD, which no other row in this world can reach -----------------------
    #
    # 🔴 UNTIL THIS FILE EXISTED THIS GATE WAS STRUCTURALLY BLIND TO A ONE-SIDED CASE FOLD,
    # AND THE DIVERGENCE IT COVERS WAS LIVE. Every heading in every other entry here is
    # ASCII, and over ASCII the two clients' lowercase mappings are the same function — so a
    # client that folded headings with Go's `strings.ToLower` (the SIMPLE Unicode mapping)
    # compared byte-identical to the oracle's `str.lower()` (the FULL one, which may expand
    # one code point into several) on every row, forever. The corpus never presented the
    # discriminating input. It does now.
    #
    # The heading is `## PO<U+0130>NTERS` — U+0130 LATIN CAPITAL LETTER I WITH DOT ABOVE,
    # written as an escape because a literal beside its neighbours is unreviewable, and the
    # expansion it produces (U+0307 COMBINING DOT ABOVE) renders ON TOP of the `i` and is
    # invisible in source. MEASURED at the commit this file landed in:
    #
    #   oracle  `.lower()`           -> "poi<U+0307>nters"  -> pairs with NOTHING -> ABSENT
    #   go      `strings.ToLower`    -> "pointers"          -> pairs with `## Pointers`
    #                                                         -> RENAMED
    #
    # Two different findings, two different rendered sub-blocks, and neither client errors.
    # `internal/pytext.Lower` is the function that agrees with the oracle ON THIS RULE; this
    # row is what makes reverting to `strings.ToLower` RED instead of silent.
    #
    # 🔴 "AGREES WITH THE ORACLE" USED TO BE WRITTEN WITHOUT THAT QUALIFIER AND IT WAS A
    # FALSE COMPLETENESS CLAIM. `str.lower()` differs from the simple mapping in TWO
    # language-independent rules, and `pytext.Lower` implements ONE of them: the
    # unconditional U+0130 expansion this row covers. The other is CONTEXTUAL — Final_Sigma:
    # U+03A3 lowercases to U+03C2 at the end of a word on the oracle and to U+03C3 here.
    # `pytext.Lower`'s docstring carries the decision NOT to implement it, and
    # `TestLowerIsCPythonExceptForFinalSigma` is the ledger.
    #
    # ⚠ AND THERE IS DELIBERATELY NO Σ ROW BESIDE THIS ONE, WHICH IS NOT THE SAME OMISSION
    # THIS ROW WAS ADDED TO FIX. The U+0130 row has discriminating power because Go's answer
    # (`pointers`) is ASCII and COLLIDES with the schema heading's key — that collision is
    # what makes one client say RENAMED and the other ABSENT. Final_Sigma's two answers are
    # BOTH non-ASCII, so a Σ-bearing heading pairs with no schema heading on EITHER client
    # and both report ABSENT with the heading quoted verbatim in the inventory. MEASURED at
    # the commit this paragraph landed in: an entry whose pointers heading is `## POINTERΣ`
    # renders 11 advisory lines that are BYTE-IDENTICAL across the two clients. A Σ row would
    # therefore be an INVARIANT row — one that cannot go red for the rule it names — and
    # counting it as coverage is the "reads as coverage while providing none" failure, not a
    # closure of it. It becomes worth seeding the day a schema heading stops being ASCII, or
    # a caller puts a folded key on a screen rather than only comparing it with another key.
    #
    # ⚠ IT IS AN INTERIOR CODE POINT, NOT A LEADING OR TRAILING ONE, and that is the
    # reachability argument rather than decoration: `NormalizeRef`'s own notes record that
    # the same expansion at the END of a string is trimmed away and the two clients AGREE
    # there — a fixture spelled `## POINTERS<U+0130>` would have scored a pass while the
    # defect stood.
    ("crag-notes/scarp-idx.md", 17_000_000_000,
     "---\nservice: scarp-idx\nscope: crag-notes\n---\n"
     "\n## What it is\n\n"
     "a synthetic entry whose spine heading folds differently under the two lowercase "
     "mappings.\n"
     "\n## PO\u0130NTERS\n\n"
     "- `apps/scarp-idx/values.yaml`\n"
     "\n## Nuance / work-history\n\n"
     "- 2000-01-10: RESOLVED abc1234: closed and verifiable, reported by nothing.\n"),
]

#: The sheets above, as store-relative paths. A sheet is NOT an entry: `ls-entries` must not
#: print it, `validate` must not count it, and the loader must not index it.
POLICY_SHEETS = ("alpha-notes/README.md", "beta-notes/README.md")

#: The lookalikes, which ARE entries and must appear everywhere an entry appears.
POLICY_SHEET_LOOKALIKES = ("beta-notes/readme.md", "beta-notes/README-old.md")

#: A scope directory that EXISTS and holds nothing. `scope-empty` must not read like
#: `scope-unreadable`, and only an empty directory reaches it.
EMPTY_SCOPES = ["hollow-set"]

#: The token file the pod authorises against: `<token> <identity> <scope>,<scope>`.
#:
#: 🔴 AN IDENTITY-AND-SCOPES ROW, NOT A BARE TOKEN, AND THE REASON IS THE WRITE VERBS. A bare
#: token row resolves to `scopes is None` — UNRESTRICTED — and is FORBIDDEN TO WRITE, so every
#: `append`/`put`/`create` case would compare two identical refusals and measure nothing about
#: the write path.
#:
#: 🔴 THE ALLOWLIST NAMES EVERY SCOPE THE WORLD HOLDS AND NOTHING ELSE. The parity question is
#: whether two CLIENTS agree, so the narrowing is deliberately not a variable here — the
#: SERVER's narrowing is the conformance corpus's claim. `ghost-void` is absent from the list
#: AND from the store, which is the point of the refused-scope cases: the two answers are
#: byte-identical by design, so a client cannot tell them apart either way.
#: 🔴 43 CHARACTERS, BECAUSE THE SERVER REFUSES TO START ON A SHORTER ONE. Measured: a 25-char
#: row exits 78 with `token on line 1 of 1 is too short: 25 chars, need >= 43 (256 bits
#: base64url)`. It is synthetic filler, not a credential — the value is a repeated literal
#: precisely so nobody can mistake it for one that ever authorised anything.
TOKEN = "parity-harness-synthetic-token-0000000000000"
ALLOWED_SCOPES = ("alpha-notes", "beta-notes", "crag-notes", "rubble-heap", "hollow-set")
TOKEN_ROW = f"{TOKEN} parity-harness {','.join(ALLOWED_SCOPES)}\n"

#: The handoff doc. 🔴 IT QUOTES `apps/widget-cfg/values.yaml`, WHICH IS THE OLDER OF THE TWO
#: TIED ENTRIES — so a client whose focus selector works features `widget-cfg` and a client
#: that silently fell back to the newest file features `gauge-api`. The two answers are
#: different bytes, which is the only reason this file is here.
HANDOFF = """# handoff — the synthetic parity world

Work in flight touches `apps/widget-cfg/values.yaml` and its chart. The `docs/unrelated.md`
note is prose, not a path window. A URL like `https://example.invalid/x` and a shell variable
like `$HOME/x` must both be REJECTED as path tokens rather than minted into refs.
"""


def build_store(root: Path) -> Path:
    """Materialise the store the pod serves. Returns the store root."""
    root = Path(root)
    for scope in EMPTY_SCOPES:
        (root / scope).mkdir(parents=True, exist_ok=True)
    for rel, _offset, text in ENTRIES:
        target = root / rel
        target.parent.mkdir(parents=True, exist_ok=True)
        target.write_text(text, encoding="utf-8")
    # 🔴 MTIMES LAST, AND FROM THE DECLARED INTEGER IN NANOSECONDS. Writing the file sets the
    # mtime to now; setting it afterwards is the only order that holds. Nanoseconds rather
    # than a float because the index order is decided by comparing these numbers.
    for rel, offset, _text in ENTRIES:
        ns = EPOCH_NS + offset
        os.utime(root / rel, ns=(ns, ns))
    # 🔴 THE REACHABILITY CONTROL FOR THE POLICY-SHEET ROWS, AND IT IS NOT CEREMONY.
    # `beta-notes` holds BOTH `README.md` and `readme.md`; on a case-folding filesystem those
    # are ONE file, the second write silently replaces the first, and the corpus goes back to
    # holding no discriminating input while every row still compares equal. A gate that
    # cannot see the class it was widened for must say so rather than score a pass.
    for rel in POLICY_SHEETS + POLICY_SHEET_LOOKALIKES:
        if not (root / rel).is_file():
            raise AssertionError(
                f"the world never materialised {rel!r} — on a case-folding filesystem "
                f"`README.md` and `readme.md` collapse into one file and this corpus stops "
                f"discriminating the policy-sheet rule from a case fold"
            )
    seen = {(root / rel).read_text(encoding="utf-8") for rel
            in POLICY_SHEETS + POLICY_SHEET_LOOKALIKES}
    if len(seen) != len(POLICY_SHEETS) + len(POLICY_SHEET_LOOKALIKES):
        raise AssertionError(
            "two policy-sheet fixtures hold the same bytes, so one of them overwrote the "
            "other — the filesystem folded case"
        )
    # The seed stamp the pod dates itself from. A fixed ASCII value, so `X-Store-Snapshot` is
    # byte-stable across requests and the LIVE banner is comparable at all.
    (root / ".seed-stamp").write_text("2000-01-01T00:00:00Z\n", encoding="utf-8")
    return root


def build_repo(root: Path) -> Path:
    """A git repo whose basename IS a scope, carrying a handoff doc. Returns its path.

    🔴 THE DIRECTORY IS NAMED `alpha-notes` BECAUSE THE SCOPE IS DERIVED FROM IT. `cairn
    recall` with no `--scope` runs `git rev-parse --git-common-dir` and takes its parent's
    basename, so the repo's NAME is what makes the no-`--scope` cases address a scope that
    exists. A repo named anything else would make every such case `scope-absent` — which is a
    real branch, and is covered by its own case rather than by accident here.
    """
    repo = Path(root) / "alpha-notes"
    (repo / "claudedocs").mkdir(parents=True, exist_ok=True)
    (repo / "claudedocs" / "handoff-parity.md").write_text(HANDOFF, encoding="utf-8")
    env = dict(os.environ)
    # A hermetic git: no user config, no global hooks, no signing.
    env.update({
        "GIT_CONFIG_GLOBAL": "/dev/null",
        "GIT_CONFIG_SYSTEM": "/dev/null",
        "GIT_TERMINAL_PROMPT": "0",
    })
    subprocess.run(["git", "init", "-q", str(repo)], check=True, env=env,
                   capture_output=True)
    return repo
