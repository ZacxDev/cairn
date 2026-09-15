# Handoff: agents-migration — 2026-09-13

## Run this first — the index, one command
```bash
cairn recall --repo /home/zach/workspace/cairn
```
Terse pointers this doc does not carry, curated by past sessions and outliving it.
🔴 RECALL, NOT LIVE OBSERVATION — every line is a pointer to VERIFY, never a current
reading, and it may describe a gotcha already fixed. `scope-absent`/`scope-empty` means
nothing is recorded yet: ordinary, not an error, and not a clean bill of health.
Non-blocking: if it exits non-zero, print the stderr line and carry on.

## Goal
Make the cairn repo's docs accurate and agent-standard: fix stale/false claims in
`server/README.md`, add the missing root README, and migrate `CLAUDE.md` →
`AGENTS.md` (canonical rules in AGENTS.md; CLAUDE.md a one-line `@AGENTS.md`
stub — the pattern Anthropic's own docs prescribe).

## State now
- Branch: `docs/agents-migration` (off `main` @ `1e7aedf`). **MERGED.** **PR #14**
  (https://github.com/ZacxDev/cairn/pull/14) squash-merged to `main` as **`3c316ff`**;
  pushed branch head was `92d2e80`. Merged without an audit — the decision and how the
  merge was verified are under **Closed — do not re-derive these**, in the `Decide PR #14`
  item. (An earlier version of this line said "see rank 1 below", which is the
  snapshot-sync item and never held this content: a pointer to a RANK NUMBER dangles the
  moment the ranks are renumbered — which this doc had already recorded happening once, in
  the `IN FLIGHT` bullet below. Name the section and the item, never the rank.)
  Re-verified on the committed tree before the commit, not carried over from the session
  that wrote it: suite **1716 passed / 0 failed** (exit 0, counted from the runner's own
  summary line), leakscan **0 findings across 42 files** with the positive control
  producing 1 finding and every negative control refusing, `--self-test` rc 0.
  Also re-read every claim in the new `README.md` off the code: 9 `add_parser`
  subcommands, `EXIT_*` 3/4/5 read + 6/7/8/9 write, `HEALTH_BODY = b"ok\n"`, the
  `recall`/`search`/`snapshot` handlers and `WRITE_ROUTES`' two entry rows.
- DONE (now committed in `dae15b0`):
  - `AGENTS.md` = old `CLAUDE.md` via `git mv` (staged) + 3-line header note: canonical file, edit HERE never the stub.
  - `CLAUDE.md` = 6-line stub, body is `@AGENTS.md` (staged). Claude Code expands at session start; opencode/others read AGENTS.md directly.
  - `README.md` = NEW root README (staged): north-star tagline, nix install, 9 client verbs + read/write exit-code model, 6 server routes + token/auth model, layout, dev commands, naming aliases.
  - `server/README.md` (unstaged) — 5 accuracy fixes: header framing (phase-2 client SHIPPED; referenced from AGENTS.md, not "not referenced"), phase-table row "byte-identity → phase 2 SHIPPED", Files-table `scripts/lib` → `lib/`, Operating-it paths `scripts/subsystem-store-api/*` → `server/*`, Deferred section ("A CLI verb for writing — scripts/cairn still only reads" was FALSE → LANDED with exit-codes 6–9 note; rotate-token premise updated: wrapper exists, script still unbuilt).
  - `cairn:431` and `tests/testlib/public_ip_scan.py:5` docstrings now cite `AGENTS.md`; the latter had quoted pre-extraction rule text that existed NOWHERE in the repo.
  - `tests/test_subsystem_store_api.py` scan must-reach ledger now requires `AGENTS.md` AND `CLAUDE.md`.
- Verified: full suite **1716 passed, 0 failed**; leakscan clean with both controls watched (positive + negative), `--self-test` rc 0.
- Deploy/verify status: N/A (docs only). Pushed and merged. The task board resolved NOTHING for this session (rc 5, empty array — cannot distinguish "no task" from "wrong id"; no field written, no task created).
- Base clone re-synced `--ff-only` after the merge, so it is not silently behind `main`.
- IN FLIGHT: nothing. The docs work is on `main`; the follow-on comment fix is its own PR —
  #15, recorded under **Closed** below, which deliberately states no merge state for it.
  (An earlier version of this line pointed at "rank 2"; the ranks were renumbered and that
  pointer dangled.)

## Next steps (ranked)
🔴 **This effort is CLOSED and this list is EMPTY ON PURPOSE — do not draw work from it.**
Its goal (accurate docs; `CLAUDE.md` → `AGENTS.md`) was met, and every rank it carried is
in **Closed — do not re-derive these** below.

The one item that was still live here — conditional/incremental snapshot sync
(`GET /api/v1/snapshot` ships a full tar with no ETag/304) — **moved** to
`claudedocs/handoff-cairn-control-plane.md`, where it is **rank 4** and is scoped to key
on **principal + epoch** because that plan introduces multiple principals. It is not
listed here any more, and that is deliberate: two docs ranking one item is a shared queue
with two different `claim-work` slugs for the same work, which is how two sessions do it
twice. Claim it against the control-plane doc's rank 4, never against this file.

For anything cairn after 2026-09-14, read `claudedocs/handoff-cairn-control-plane.md`.

## Closed — do not re-derive these
Recorded with what closed each item, and with what was **not** done, so the next
`/resume` neither re-opens a settled decision nor credits work that never happened.

- ~~Commit and push the docs work~~ — DONE, `dae15b0`; claim `agents-migration-1` released.
- ~~Open a PR~~ — DONE, PR #14; claim `agents-migration-2` released.
- ~~Decide PR #14~~ — **RESOLVED: squash-merged to `main` as `3c316ff`.**
  🔴 **MERGED WITHOUT AN AUDIT.** The `/audit-pr 14` step the old rank 3 offered was
  skipped, not run-and-passed. Do not read this as "audited clean".
  **Verified by CONTENT, not by ancestry:** all 8 changed paths are byte-identical
  between the pushed head `92d2e80` and `origin/main`, and each was separately proven to
  EXIST on `origin/main` before being compared — a diff against an absent operand reports
  SAME, not MISSING. 🔴 Ancestry is not usable here and will mislead: a squash merge makes
  `git merge-base --is-ancestor 92d2e80 origin/main` **false forever**, because the squash
  is a new commit with different parents. That false reads as "not merged — redo the work".
  CI on `92d2e80` was green on all three jobs (`leakscan` 6s, `nix` 29s, `tests` 8m16s;
  run `34788483377`), and the branch was **0 commits behind `main`**, so that green was a
  merged-tree green rather than a branch-only one.
- ~~`lib/cairn_doctor.py`'s exit-code comment~~ — **RESOLVED on branch
  `fix/doctor-exit-code-comment`** — **PR #15** (https://github.com/ZacxDev/cairn/pull/15),
  base `main`.
  🔴 **THIS LINE DELIBERATELY RECORDS NO MERGE STATE FOR #15, AND NEITHER SHOULD YOU.**
  A merge claim a branch makes about ITS OWN PR cannot be true on both sides of that
  merge: "OPEN and not merged", written here, is false the instant #15 lands — a false
  claim in a tracked file in a public repo, and a `/resume` that believes it either
  re-opens finished work or re-pushes a merged branch. #14 above states "squash-merged as
  `3c316ff`" safely only because that sentence was written on `main` AFTER the merge, not
  inside the branch it describes. Ask the API instead —
  `gh pr view 15 --json state,mergedAt,mergeCommit` — and confirm by CONTENT, never by
  ancestry (a squash makes `--is-ancestor` false forever).
  **Audit: `/audit-pr 15` round 0 RAN** against the branch head, which is `de0b37c` in any
  clone of this repo. 🔴 **The commit the audit actually ran on was `634b01e`, and citing
  that sha here was a defect: it is UNREACHABLE from any ref** — the pre-rebase twin of
  `de0b37c`, alive only in one machine's object store, so a reader of the public repo
  cannot resolve it. `de0b37c` is the honest citation: rebasing onto `c536c52` added
  exactly one file to the tree (`claudedocs/plan-cairn-control-plane.md`, from #16), which
  the audit did not touch, so the audited content is `de0b37c`'s minus that file. **Never
  cite an orphan sha** — if a rebase moved the tree, cite the reachable commit and say what
  the rebase changed.
  Verdict *proceed to the checklist*, with two 🟡 findings, both fixed by the commit that
  rewrote this paragraph:
  (1) the exit-code comment enumerated the codes a `doctor` caller may observe as
  0/2/9/10 when the set is **0/1/2/9/10** — an incomplete enumeration inside the fix for
  an incomplete enumeration — and the test ledger was structurally unable to catch a
  doctor code of 1, since 1 is not one of the client's nine codes; (2) this line asserted
  #15's own merge state. Nothing is claimed here about rounds that have not run.
  What the comment claimed was that
  doctor's codes are "disjoint from every other `cairn` code" while enumerating only
  0/3/4/5 and 6/7/8 — the omission of 9 is how the overstatement survived, since
  `EXIT_DOCTOR_PROBLEM = 9` and the client's `EXIT_WRITE_EXISTS = 9`. It now states the
  true claim: the codes are scoped per command, 0 and 9 are shared deliberately, and no
  call site can receive an ambiguous 9. **No exit code was renumbered** — `EXIT_LEGEND` is
  printed on every `doctor` run, so the numbers are a contract.
  🔴 **A second, worse instance was found while fixing it, in the tests**:
  `test_doctors_codes_collide_with_NO_other_cairn_exit_code` asserted
  `EXIT_DOCTOR_PROBLEM not in others` against an `others` set that enumerated **eight of
  the client's nine** codes, omitting `EXIT_WRITE_EXISTS`. It passed because the one
  colliding number sat outside the set it looked at — a guard whose name claimed the
  coverage its body could not provide. Replaced by an intersection ledger pinned to
  `{0, 9}`, which fails when the shared set grows or shrinks. Labelled an **invariant
  guard**, not regression coverage: nothing ever violated the invariant, the defect was
  the claim about it.
  🔴 **The intersection ledger has a blind spot that a sibling test now covers**:
  `test_1_is_NOT_one_of_doctors_codes` pins that no `EXIT_DOCTOR_*` constant and no
  `EXIT_LEGEND` row is **1** — the code a `doctor` invocation returns for an uncaught
  exception (`cairn doctor` exits 1 with `ModuleNotFoundError` when `lib/cairn_doctor.py`
  is absent, because the lazy import at `cairn:cmd_doctor` is unguarded on purpose;
  measured again with an unparseable module, which raises `SyntaxError` and is not
  soft-caught either). It is separate because the ledger grades an intersection with the
  client's codes and 1 is not one of them, so it stays GREEN while 1 is genuinely
  ambiguous — demonstrated: adding `EXIT_DOCTOR_SOMETHING = 1` fails only the new test.
  It discovers the set by introspection rather than hand-listing it, since a hand list of
  three is what a FOURTH constant walks past. Also an **invariant guard**.

## Gotchas / decisions / dead-ends
- pytest is NOT in the default nix profile: `uv run --python 3.12 --with pytest -- pytest tests -q -p no:randomly` (CI pip-installs pytest the same way; 3.12 is the pinned interpreter — do not use bare `python3`).
- The byte-identity scan's must-reach test enumerates via the git index: a NEW file that is not staged fails `test_the_SCAN_actually_reaches_the_repo` (`the scan does not reach CLAUDE.md`). Stage new files before believing the suite.
- Pyright/LSP "errors" in `tests/test_subsystem_store_api.py` are pre-existing strictness noise (missing pytest stubs, stdlib typing) — not caused by doc edits; the suite is the gate, not the LSP.
- opencode does NOT expand `@`-imports, but that is fine here: opencode reads `AGENTS.md` directly (a project AGENTS.md suppresses CLAUDE.md); Claude Code reads the stub and expands the import. Both tools get full content from ONE file — no duplication, no drift.
- Anthropic memory docs (fetched this session): imports expand at launch, max 4 hops; block-level HTML comments in CLAUDE.md are stripped before injection (the stub's maintainer note uses one).

## How to verify
```bash
cd /home/zach/workspace/cairn
python3 tests/leakscan.py && python3 tests/leakscan.py --self-test
uv run --python 3.12 --with pytest -- pytest tests -q -p no:randomly
head -1 CLAUDE.md            # -> @AGENTS.md (stub intact)
git log --oneline -3 main    # 3c316ff is #14's squash; c536c52 is #16's
```
Expected counts are **per tree, so name the tree** rather than carrying one number
forward: **1716 passed / 0 failed** at `3c316ff` and at `c536c52`, **1717** on
`fix/doctor-exit-code-comment` once rebased onto `c536c52` (the extra one is
`test_1_is_NOT_one_of_doctors_codes`); still **1717** after round 1 of that PR's audit,
which added no test — it replaced two hand-listed operand sets with discovered ones.
leakscan scans the tracked text files, so its denominator moves with the repo: **42** at
`3c316ff`, **43** at `c536c52`, **44** on `fix/doctor-exit-code-comment` after round 1
(the new `tests/testlib/cairn_source.py`). A count that does not match is a question about
which tree you are on before it is a finding.
🔴 Do NOT verify #14 with `git merge-base --is-ancestor 92d2e80 main` — it is false after
every squash merge and always will be. Diff the 8 changed paths instead, proving each
exists on `main` first.
