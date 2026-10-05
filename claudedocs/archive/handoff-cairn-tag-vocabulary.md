## Evicted from `claudedocs/handoff-cairn-tag-vocabulary.md` — 2026-10-05

Closed, retracted and duplicated Gotchas bullets from the cairn tag-vocabulary arc. Includes the RETRACTED "the canonical handoff is not on main" claim together with its own retraction (keep the pair: the retraction is the record), the operator decision on guard fail-open (c) that #171 implemented, the superseded "go job runs 25-35 min" figure later corrected to ~49 min, three stale worktree notes, and the older of four near-duplicate lesson bullets whose fuller copies stay in the doc.

From `Gotchas / decisions / dead-ends`:

- 🔴 **THE CANONICAL HANDOFF FOR THIS ARC IS NOT ON `main`.** It lives only on
  `docs/handoff-cairn-tag-vocabulary` (PR #169, open). A `/resume` kickoff naming
  `claudedocs/handoff-cairn-tag-vocabulary.md` dead-ends from a `main` checkout. The way to find
  it is `git log --all --oneline --diff-filter=A -- '*<topic>*'`, then read it out of the ref
  with `git show <branch>:<path>` — **a plain `grep -rl` over the tree finds nothing**, because
  the file is not in the working tree at all.
- 🔴 **RETRACTED — "THE CANONICAL HANDOFF FOR THIS ARC IS NOT ON `main`" IS NOW FALSE, AND SO IS ITS
  INSTRUCTION.** The bullet below (and its `git log --all --diff-filter=A` / `git show <branch>:<path>`
  recipe) applied while PR #169 was open. **#169 merged as `ffede27`: the doc IS on `main`.** Read it
  at `claudedocs/handoff-cairn-tag-vocabulary.md` directly — do not run the branch-ref dance, and do
  not conclude from a failed `git show origin/docs/...` that the doc is missing; that branch is
  deleted. ⚠ Recorded as a retraction rather than a deletion because `Gotchas` is an APPEND bucket:
  the superseded text survives verbatim below and would otherwise be read as current.
- 🔴 **OPERATOR DECISION on fail-open (c), made this session. Implement it; do not relitigate.**
  `clean`, `mv`, `rm` go **INTO** `_REFUSED` (with `clean -n` / `--dry-run` exempt).
  `worktree remove` and `branch -D` are recorded as **DELIBERATELY OUT**, for two reasons that
  both belong in the doc: (1) `claudedocs/working-in-parallel.md:33` **prescribes**
  `git -C "$REPO" worktree remove "$WT"` run FROM the base clone, so refusing it breaks the
  repo's own documented recipe — the failure mode this guard explicitly forbids itself;
  (2) both write refs / worktree registrations, which live in the **common** git dir and are
  writable identically from ANY worktree, so conditions 2+3 cannot scope the hazard, and
  refusing only in the base clone teaches that the worktree spelling is safe when it is not.
  ⚠ **Acknowledge rather than hide the tension:** `stash` IS in `_REFUSED` on a hazard that is
  equally repo-global; the docstring keeps it as "a cheap table row". The reasoning is not
  uniform and the doc should say so.
- 🔴 **A BASE MOVE RESETS `mergeable` TO `UNKNOWN`, AND `UNKNOWN` IS NOT `MERGEABLE`.**
  Immediately after #168 landed, `gh pr view 170 --json mergeable` answered `UNKNOWN` — GitHub
  recomputes asynchronously. **Poll until it is not `UNKNOWN` before believing either answer**;
  reading `UNKNOWN` as "fine" is how a conflicting PR gets a merge attempt.
- ⚠ **The `go` CI job on this repo runs ~25–35 min and `gh pr checks` shows it as
  `pending 0`.** Its steps are a 189-mutant authz battery, the Go client ledgers, a 57-mutant
  routing battery and the conformance corpus. `gh run view --job <id>` shows per-step ticks and
  is the only way to tell "slow but advancing" from "stuck" — do not read `pending 0` as stuck,
  and do not read a 20-minute watcher timeout as a failure.
- ⚠ **`/home/zach/workspace/cairn-hv2` was used as this session's worktree for the handoff
  branch**, because `cairn-handoff` is still the stale worktree on a closed arc. Remove it when
  done. 60 linked worktrees are now registered against the base clone.
- 🔴 **NINE INSTRUMENTS RETURNED A CONFIDENT WRONG ANSWER IN THIS ARC, AND EVERY ONE WAS CAUGHT ONLY
  BY A CONTROL.** `awk` with an end pattern matching its own start line (reported 0 prints for
  everything); a **case-SENSITIVE** sweep for a word written in capitals (0 hits vs 1); importing
  the hook as a module, which runs `main()` and `sys.exit(0)`s — **exit 0, no output, reads as
  "zero findings"**; a `" 1 passed"` matcher anchored mid-line; a differential pointed at the wrong
  tree (383 false fail-opens); a `-h` arity reader whose attached-arg branch matched empty; a
  marker-grep over `git merge-tree`, which prints no markers; a sweep whose own control string was
  the operator's real checkout path — in a PUBLIC repo; and a mutation anchor that never applied.
  **Validate the instrument, report the pair, and treat a reassuring zero as unproven.**
- 🔴 **A `claim-work`/`audit-claims` range endpoint TRUNCATED BY HAND silently disarms the gate.**
  `7ee84771` for `7ee8477c` made the range unresolvable; the assembler reported `PAYLOAD NOT
  VERIFIED … exited 128` and **fell back to the STATED count** — reverting to the behaviour it had
  before the measured unit existed. **Use full 40-char shas.**
- ⚠ **Two API limit outages hit mid-run.** One agent died after one line (resume works — re-anchor
  it and re-verify the world first); one died *between* its commit+push and its report, so the work
  had landed while the report was lost. **Check the pushed ref before assuming work was lost.**
- ⚠ `/home/zach/workspace/cairn-hv2` was this session's handoff worktree; `cairn-handoff` is still
  the stale one on a closed arc. Remove hv2 when done.
- 🔴 **THIS DELTA IS STACKED ON #172 ON PURPOSE, AND THE ALTERNATIVE WAS MEASURED WORSE.** #172 is
  doc-only, open, 8/8 green, and **declares this arc CLOSED while half 1 is false** — it also deletes
  the write-gate verification block from `How to verify`. Basing off `main` instead would have put two
  independent structured merges of the same 400-line doc on a collision course. Basing off #172's head
  (`239da57`) means this version is "#172's text plus the correction". 🔴 **Merge #172 WITHOUT
  `--delete-branch`, or retarget this PR to `main` first** — a deleted base auto-closes the child PR
  and GitHub refuses to reopen it.
- ⚠ **TWO `cairn` SESSIONS WERE LIVE WHILE THIS RAN** (`cairn-83` idle 17h, `cairn-06` busy 14h);
  #172 was opened 45 minutes before this session started, so its author was very likely still working.
  **Nothing was pushed to its branch** — a stacked branch of my own was used instead.
- ⚠ **`/home/zach/workspace/cairn-reopen` was this session's worktree** (branch
  `docs/tag-vocab-reopened`, off `239da57`). `cairn-handoff` is still the stale worktree on a closed
  arc; 14 `cairn-*` worktree paths now exist beside the base clone. Remove `cairn-reopen` when this
  lands.
