#!/usr/bin/env python3
"""The GENERATED store the dual-run gate uses when no real one is pointed at.

🔴 EVERY NAME, DATE AND WORD HERE IS SYNTHETIC, AND THAT IS NOT A STYLE CHOICE. This
repository is PUBLIC and was extracted from a private one; the dual-run gate's other mode
runs against the operator's own store, whose content may NEVER enter this tree. So the
harness ships and the data does not: mode 1 is pointed at a path, mode 2 builds this.
Scopes are `alpha-index` / `gamma-paged` / `zeta-hollow`, refs are `widget-cfg` /
`plum` / …, and every timestamp is an offset from 2000-01-01, which is what
`tests/leakscan.py` allows and what makes a real date in here unambiguous.

🔴 IT IS DELIBERATELY RICHER AND LARGER THAN THE CONFORMANCE WORLD, BECAUSE THE WHOLE
POINT OF THE DUAL-RUN IS THE CASES A DECLARED CORPUS DOES NOT ENCODE. `world.json` holds
2 real scopes and 6 entries; this holds 9 scopes and 140-odd entries at scale 1, and each
of the following is a branch neither `world.json` nor the reader fixture reaches through
a SERVED response:

  * a scope that IS a git repo and a scope that is NOT — `X-Store-Revision` is read off
    `<scope>/.git/HEAD`, so the two answers are different code paths and both are served;
  * a scope OVER the reader's 100-line index page, so the paginated header, the
    `N more entries` notice, the last page and a page past the end are all served. ⚠ This
    is a widening over `server/verify-byte-identity.sh`, which REFUSES a paginated index —
    correctly, because it compares two STORES and page membership is mtime-derived. Here
    both servers read ONE store, so the page is a fact about the store and comparable;
  * entries that share a whole second and differ only in the FRACTION, plus a pair tied to
    the NANOSECOND, so both the index order's tie-break and the featured pick's are live;
  * a non-ASCII member name, and a member name over 100 bytes — the two shapes that make
    the snapshot tar emit a PAX `path` extended record at all;
  * a scope holding files and NOTHING indexable, an EMPTY scope directory, and malformed
    entries BESIDE readable ones;
  * an AMBIGUOUS bare ref (`plum.md` next to `plum.process.md`), which is the per-entry
    arm's own refusal path.

🔴 AND WHAT IT CANNOT STAND IN FOR: the operator's real store is the only thing that has
the real store's SHAPE — its scope count, its entry-size distribution, its mtime spread,
its actual `.git` contents, and the one-off malformations nobody would think to write
down. A green here is a claim about a world this file imagined. `--store` is the mode that
answers "are the two servers identical on MY data", and the two modes are not
substitutes; `README.md` says so under "What mode 2 cannot stand in for".

Deterministic from a seed so a failure reproduces:

    python3 tests/dualrun/genstore.py <dir> [--seed N] [--scale N]
"""
from __future__ import annotations

import argparse
import os
import random
from pathlib import Path

#: 2000-01-01T00:00:00Z in nanoseconds. Every mtime is this plus an offset, so every
#: timestamp in the generated world is in the synthetic year `leakscan.py` allows.
EPOCH_NS = 946_684_800 * 1_000_000_000

#: The default seed. A date-shaped literal in the allowed synthetic year, so the number
#: in a failure report reads as "the seed" rather than as an arbitrary constant.
DEFAULT_SEED = 20000101

#: The reader's index page cap. `gamma-paged` is generated one OVER it so page 2 exists.
#:
#: 🔴 IT IS RESTATED HERE RATHER THAN IMPORTED, DELIBERATELY. This generator's job is to
#: produce a store that CROSSES the reader's cap; importing the reader's constant would
#: make the crossing follow the cap, so a change that raised the cap would silently stop
#: generating a paginated scope while this file still claimed to. `page_cap_is_crossed`
#: below is the assertion that the literal is still on the right side of the real one, and
#: `tests/test_dualrun_harness.py` reads the reader's own value and compares.
LISTING_PAGE_SIZE = 100

#: The seed stamp the served copy dates itself from. Fixed ASCII, so `X-Store-Snapshot`
#: and the freshness banner are byte-stable across requests on both servers.
SEED_STAMP = "2000-01-01T00:00:00Z\n"

#: A 40-hex bare sha for a `.git/HEAD`. `scope_revision` reads HEAD directly and accepts a
#: bare sha, so nothing here spawns git — which also keeps the generated store buildable
#: in a sandbox with no git on PATH.
GIT_HEAD_SHA = "0" * 32 + "beef1234"

#: A member name over 100 bytes. 🔴 100 IS THE USTAR `name` FIELD WIDTH, and a name over
#: it is one of only two shapes that make CPython's PAX writer emit a `path` extended
#: record — the other being a non-ASCII name. Both are in this world because the four
#: header divergences `AGENTS.md` records were all in those records, and a store with
#: neither shape compares two archives that never exercise them.
LONG_STEM = "over-one-hundred-bytes-" + "x" * 84

#: A non-ASCII stem. The second PAX `path` shape: CPython emits the record for a
#: non-ASCII name at ANY length, because its ASCII test runs before its length test.
NON_ASCII_STEM = "crème-brûlée-café"


def _front(service: str, scope: str, *, aliases: str = "", sensitivity: str = "",
           tasks: str = "") -> list[str]:
    out = ["---", f"service: {service}", f"scope: {scope}"]
    if aliases:
        out.append(f"aliases: [{aliases}]")
    if sensitivity:
        out.append(f"sensitivity: {sensitivity}")
    if tasks:
        out.append(f"tasks: [{tasks}]")
    out.append("---")
    return out


def _entry(service: str, scope: str, *, body: str, aliases: str = "", sensitivity: str = "",
           tasks: str = "", pointers: list[str] | None = None,
           nuance: list[str] | None = None) -> str:
    lines = _front(service, scope, aliases=aliases, sensitivity=sensitivity, tasks=tasks)
    lines += ["", "## What it is", "", body, "", "## Pointers", ""]
    lines += pointers if pointers is not None else [f"- `apps/{service}/values.yaml`"]
    lines += ["", "## Nuance / work-history", ""]
    lines += nuance if nuance is not None else [
        "- 2000-01-02: OPEN: the synthetic action this entry records.",
        "- 2000-01-03: RESOLVED abc1234: the synthetic action that closed.",
    ]
    return "\n".join(lines) + "\n"


def page_cap_is_crossed(reader_cap: int) -> bool:
    """Does the generated `gamma-paged` scope still cross the READER's page cap?

    🔴 THE LITERAL ABOVE IS A COPY, SO SOMETHING HAS TO COMPARE IT TO THE ORIGINAL. A
    generator whose `gamma-paged` fell under a raised cap would produce an UNPAGINATED
    scope while this file's docstring still promised a paginated one — coverage in name
    only, and invisible because nothing about the run would change shape. The test reads
    the reader's own constant and calls this.

    `gamma-paged` holds `LISTING_PAGE_SIZE + 1` entries, so it paginates exactly when that
    count exceeds the reader's real cap.
    """
    return LISTING_PAGE_SIZE + 1 > reader_cap


# ---------------------------------------------------------------------------
# The declared world. `scale` multiplies only the generated FILLER scopes, so the
# hand-written shapes above stay exactly one of each however large the store gets.
# ---------------------------------------------------------------------------

#: Scope directories that exist and hold nothing indexable at all.
#:
#: `zeta-hollow` is EMPTY (status `scope-empty`); `epsilon-rubble` holds files that are not
#: entries (status `scope-unreadable`, a non-zero exit AND a warning line on the server's
#: stderr). They are different branches and a store with only one leaves the other unserved.
EMPTY_SCOPE = "zeta-hollow"
RUBBLE_SCOPE = "epsilon-rubble"

#: Scopes carrying a `.git/HEAD`, and scopes deliberately without one.
#:
#: 🔴 BOTH, BECAUSE `X-Store-Revision` IS A DIFFERENT CODE PATH EITHER WAY and the header
#: is the ONE value in a report response that is NOT read through the narrowed index — it
#: is read off the filesystem — so it is the one place a refused scope could still be told
#: apart from an absent one. A world where every scope is a repo never serves the empty
#: header; a world where none is never serves a revision.
GIT_SCOPES = ("alpha-index", "gamma-paged")


def build_store(dest: Path, seed: int = DEFAULT_SEED, scale: int = 1) -> Path:
    """Materialise the generated world under `dest`. Returns `dest`.

    🔴 MTIMES ARE SET IN A SECOND PASS, AFTER EVERY WRITE, AND FROM AN INTEGER NUMBER OF
    NANOSECONDS. Writing a file sets its mtime to now, so a build that set them inline
    would produce a store whose index ORDER is the order the loop ran in. Nanoseconds
    rather than a float because the order is decided by comparing these numbers and a
    float would be converted twice.
    """
    if scale < 1:
        raise ValueError(f"scale must be >= 1, got {scale}")
    dest = Path(dest)
    rng = random.Random(seed)
    files: list[tuple[str, int, str]] = []  # (relative path, mtime ns, text)

    def add(rel: str, offset_ns: int, text: str) -> None:
        files.append((rel, EPOCH_NS + offset_ns, text))

    # --- alpha-index: the shape scope. A git repo. ---------------------------
    # 🔴 TWO ENTRIES INSIDE ONE WHOLE SECOND, DIFFERING ONLY IN THE FRACTION. That is the
    # one input shape a normalised tar destroys: they tie on a truncated mtime, the reader
    # falls through to its ref tie-break, and the extracted copy orders its index
    # DIFFERENTLY with the same bytes, the same count and no error.
    add("alpha-index/widget-cfg.md", 250_000_000,
        _entry("widget-cfg", "alpha-index", body="A synthetic entry naming a rate-limit.",
               sensitivity="internal"))
    add("alpha-index/ledger-svc.md", 750_000_000,
        _entry("ledger-svc", "alpha-index", body="A second entry, newer by 500ms and no more.",
               aliases="ledger-holder, shared-alias", sensitivity="internal"))
    # A SHARED alias, so a bare `shared-alias` ref is AMBIGUOUS in the ALIAS tier — the
    # loader refuses a duplicate slug outright, so the filename tier cannot collide.
    add("alpha-index/gauge-api.md", 4_000_000_000,
        _entry("gauge-api", "alpha-index", body="The replace target.", aliases="shared-alias"))
    # Every openness population at once, plus `tasks:` and an honoured sensitivity.
    add("alpha-index/marked-open.md", 5_000_000_000,
        _entry("marked-open", "alpha-index", body="Carries every openness population.",
               sensitivity="public", tasks="github:example-org/example-repo#428",
               nuance=[
                   "- 2000-01-02: OPEN: the retry budget is still unbounded.",
                   "  a continuation line, which belongs to the bullet above it.",
                   "- 2000-01-03: RESOLVED abc1234: closed, and the sha proves it.",
                   "- 2000-01-04: RESOLVED: closed, and nothing proves it.",
                   "- 2000-01-05: **OPEN:** a marker that missed the grammar.",
                   "- 2000-01-06: an ordinary bullet that declares nothing.",
               ]))
    # A heading written TWICE (the sections CONCATENATE) and a FENCED region carrying a
    # `##` line and a `-` line, which the two heading parsers disagree about.
    add("alpha-index/dupes-fenced.md", 5_500_000_000,
        "\n".join(_front("dupes-fenced", "alpha-index") + [
            "", "## What it is", "", "First half.", "",
            "## Nuance / work-history", "", "- 2000-01-02: the first nuance section.", "",
            "## Interlude", "", "This paragraph is DROPPED by the section merge.", "",
            "## Nuance / work-history", "",
            "- 2000-01-03: the second nuance section, concatenated onto the first.", "",
            "## Pointers", "", "```", "## Nuance / work-history",
            "- sample text inside a fence, not a bullet", "```",
            "- a real pointer, after the fence",
        ]) + "\n")
    # A section PRESENT AND EMPTY, which is a different fact from an absent one and the
    # only input that tells the two apart.
    add("alpha-index/emptysec.md", 5_600_000_000,
        _entry("emptysec", "alpha-index", body="Pointers is present and EMPTY.", pointers=[]))
    # Neither counted heading: `is_bare`, which is a different branch from a half-filled one.
    add("alpha-index/bare-stub.md", 5_700_000_000,
        "\n".join(_front("bare-stub", "alpha-index") + [
            "", "## What it is", "", "An entry nobody has filled in.",
        ]) + "\n")
    # 🔴 NON-ASCII CONTENT *AND* A NON-ASCII REF. The content exercises the report body's
    # encoding; the FILENAME is one of the two shapes that make the snapshot's PAX writer
    # emit a `path` extended record, which is where four header divergences hid.
    add(f"alpha-index/{NON_ASCII_STEM}.md", 5_800_000_000,
        _entry(NON_ASCII_STEM, "alpha-index",
               body="Déjà vu: naïve café façade — ünïcödé in the body, too. 日本語 も。",
               aliases="café-alias"))
    # The other PAX `path` shape: a member name over the 100-byte ustar field.
    add(f"alpha-index/{LONG_STEM}.md", 5_900_000_000,
        _entry(LONG_STEM, "alpha-index", body="A name over one hundred bytes."))

    # --- beta-index: NOT a git repo, so the revision header is the empty answer ---
    add("beta-index/spindle-cfg.md", 6_000_000_000,
        _entry("spindle-cfg", "beta-index", body="The second scope's first entry."))
    add("beta-index/fresh-target.md", 6_100_000_000,
        _entry("fresh-target", "beta-index", body="A second entry, so the index has an order."))

    # --- delta-mixed: malformed entries BESIDE readable ones ------------------
    add("delta-mixed/readable-one.md", 7_000_000_000,
        _entry("readable-one", "delta-mixed", body="Readable, beside two that are not."))
    add("delta-mixed/readable-two.md", 7_100_000_000,
        _entry("readable-two", "delta-mixed", body="Also readable."))
    # `aliases:` as a bare string is what the schema refuses.
    add("delta-mixed/broken-aliases.md", 7_200_000_000,
        "---\nservice: broken-aliases\nscope: delta-mixed\n"
        "aliases: a bare string, which the schema refuses\n---\n\n")
    # No frontmatter at all: a different rejection from a schema violation.
    add("delta-mixed/broken-nofront.md", 7_300_000_000,
        "## What it is\n\nno frontmatter, so this is not an entry at all.\n")

    # --- theta-ambiguous: `plum.md` NEXT TO `plum.process.md` -----------------
    # 🔴 A BARE `plum` REF IS AMBIGUOUS BY A DOCUMENTED CONVENTION, not by a store defect:
    # `resolve_ref_tiered` matches a bare ref on the SLUG alone, so both files answer it.
    # `<slug>.<kind>.md` is the documented shape (`KINDS = service|process|org|doc`). It is
    # here because the per-entry arm's REFUSAL path is otherwise unserved by any gate.
    add("theta-ambiguous/plum.md", 8_000_000_000,
        _entry("plum", "theta-ambiguous", body="The service half of an ambiguous bare ref."))
    add("theta-ambiguous/plum.process.md", 8_100_000_000,
        "\n".join(["---", "service: plum", "kind: process", "scope: theta-ambiguous", "---",
                   "", "## What it is", "", "The process half.", "",
                   "## Pointers", "", "- `docs/plum.md`", "",
                   "## Nuance / work-history", "", "- 2000-01-02: nothing."]) + "\n")

    # --- iota-single: ONE entry, which has exactly one possible index order ----
    # 🔴 THE BOUNDARY IS ARITHMETIC: a one-entry index cannot diverge in order at all, so
    # it is the control that separates "the orders agree" from "there was one order".
    add("iota-single/lonely-one.md", 9_000_000_000,
        _entry("lonely-one", "iota-single", body="The only entry in its scope."))

    # --- kappa-tied: two entries tied to the NANOSECOND -----------------------
    # Named so the DIRECTION is observable: the index is ref-ASCENDING on a tie and the
    # featured pick takes the ref-GREATEST, so a mutant reversing either moves a line.
    add("kappa-tied/tied-alpha.md", 9_500_000_000,
        _entry("tied-alpha", "kappa-tied", body="Tied to the nanosecond with tied-zulu."))
    add("kappa-tied/tied-zulu.md", 9_500_000_000,
        _entry("tied-zulu", "kappa-tied", body="Tied to the nanosecond with tied-alpha."))

    # --- epsilon-rubble: files, NONE indexable -------------------------------
    add(f"{RUBBLE_SCOPE}/rubble-one.md", 10_000_000_000, "this is not an entry\n")
    add(f"{RUBBLE_SCOPE}/rubble-two.md", 10_100_000_000, "neither is this\n")

    # --- gamma-paged: ONE OVER the reader's page cap, times `scale` -----------
    # 🔴 THE MTIMES DESCEND WITH THE NAME, so the newest-first order is the REVERSE of the
    # alphabetical one and a wrong order is a visible difference rather than a coincidence.
    # The fraction is drawn from the seeded generator so entries land inside shared whole
    # seconds without the pattern being hand-written.
    paged = (LISTING_PAGE_SIZE + 1) * scale
    for i in range(paged):
        frac = rng.randrange(0, 1_000_000_000)
        add(f"gamma-paged/item-{i:04d}.md",
            (20_000 - i) * 1_000_000_000 + frac,
            _entry(f"item-{i:04d}", "gamma-paged",
                   body=f"Filler entry {i}, generated from seed {seed}.",
                   sensitivity="public"))

    # --- write it -------------------------------------------------------------
    dest.mkdir(parents=True, exist_ok=True)
    (dest / EMPTY_SCOPE).mkdir(parents=True, exist_ok=True)
    for rel, _ns, text in files:
        target = dest / rel
        target.parent.mkdir(parents=True, exist_ok=True)
        target.write_text(text, encoding="utf-8")
    for scope in GIT_SCOPES:
        git = dest / scope / ".git"
        git.mkdir(parents=True, exist_ok=True)
        head = git / "HEAD"
        head.write_text(GIT_HEAD_SHA + "\n", encoding="utf-8")
        # 🔴 THE `.git/HEAD` MTIME IS SET TOO, AND LEAVING IT OUT WAS A MEASURED
        # NON-DETERMINISM RATHER THAN A TIDINESS POINT. Nothing reads this file's mtime
        # today — `scope_revision` reads its CONTENT and the snapshot skips dot-directories
        # — so the store still SERVED identically. But "deterministic from a seed" is the
        # promise the mode-2 store is trusted on, and a promise that is true of the bytes
        # a reader happens to use today is one the next reader breaks. Found by a test
        # comparing two builds file by file, which is why that test hashes EVERY file
        # rather than only the `*.md` ones.
        os.utime(head, ns=(EPOCH_NS, EPOCH_NS))
    stamp = dest / ".seed-stamp"
    stamp.write_text(SEED_STAMP, encoding="utf-8")
    os.utime(stamp, ns=(EPOCH_NS, EPOCH_NS))
    for rel, ns, _text in files:
        os.utime(dest / rel, ns=(ns, ns))
    return dest


def scopes(dest: Path) -> list[str]:
    """The scope directory names the generated store holds, sorted."""
    return sorted(p.name for p in Path(dest).iterdir()
                  if p.is_dir() and not p.name.startswith("."))


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(prog="tests/dualrun/genstore.py",
                                     description=__doc__.splitlines()[0])
    parser.add_argument("dest")
    parser.add_argument("--seed", type=int, default=DEFAULT_SEED)
    parser.add_argument("--scale", type=int, default=1)
    args = parser.parse_args(argv)
    root = build_store(Path(args.dest), args.seed, args.scale)
    files = sum(1 for p in root.rglob("*.md") if p.is_file())
    print(f"store={root}")
    print(f"scopes={len(scopes(root))} md-files={files} seed={args.seed} scale={args.scale}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
