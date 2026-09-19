# Handoff archive: cairn-control-plane — closed investigation blocks

Blocks moved out of `claudedocs/handoff-cairn-control-plane.md` once they were resolved or
superseded. They are kept VERBATIM rather than summarised: the value of a closed block is the
measured values and the eliminations, which is exactly what a summary drops. Nothing here is
live — every block below was closed at the time it was moved, and the live ones stay in the
handoff. This file is read on demand and therefore costs nothing per session.

### PR #35's `tests` job was RED on the previous head; the fix is pushed but unconfirmed
- as-of: 2026-09-16
- **Symptom + exact repro:** `gh run view <run> --repo ZacxDev/cairn --log-failed` on the
  run for head `1dd2815` ends `assert 851 >= 900` → `1 failed, 1937 passed`. The failing
  test is `tests/test_agent_instructions_weight.py::test_the_session_instructions_keep_WORKING_HEADROOM`.
- **Observed (with values):** the branch alone measured `AGENTS.md`+`CLAUDE.md` = **30,915 B,
  headroom 935** — green. The **merged** tree measured **30,999 B, headroom 851** — red by
  49. `git diff 83c6ba4 origin/main -- AGENTS.md` shows `main` gained one 84-byte row
  (`| \`python3\` on \`PATH\` | ... |`, from #36); our branch gained one 166-byte layout row.
  Each green alone; the merge is 49 B over.
- **Ruled out:** a defect in either row — both are single, minimal, correct index rows.
  `via: measurement` (diffed each side's AGENTS.md against the common base `83c6ba4`).
- **Ruled out:** trimming our row to fit. Three candidates measured; the best that fits
  (108 B) **drops the word "BYPASS" from the one row describing an auth-bypass surface**
  and leaves 9 B of slack. `via: measurement`
- **Leading hypothesis:** resolved, not hypothetical — the 202 B margin was derived for
  ONE edit and the unit is CONCURRENT edits. `MAX_BYTES` raised to 32,500 in `7ac810e`.
- **Next probe:** `gh pr view 35 --repo ZacxDev/cairn --json statusCheckRollup` — require
  **six** checks present AND all COMPLETED before reading the verdict (see the watcher
  Gotcha below), then confirm `tests` is SUCCESS on `7ac810e`.

### ✅ RESOLVED — PR #35's `tests` job, and the merged-tree byte budget
- as-of: 2026-09-16
- **Resolution:** both halves are closed by measurement, and this supersedes the block above
  titled "PR #35's `tests` job was RED on the previous head; the fix is pushed but unconfirmed".
- **Observed (with values):** six checks present and all `COMPLETED`/`SUCCESS` on run
  `35163448166`; that run's `headSha` read back as `7ac810e8a49bcca6b9b4871cddc3a7abb7b1cb51`,
  so the rollup is about the tree we care about. On the locally-built merged tree:
  30,999 B, headroom 1,501, 1938 pytest passes, 15 Go `ok` lines, leakscan rc 0 over 293 files.
- **Ruled out:** that the raised ceiling is a gate that no longer fires. Re-measured the
  negative control **on the merged tree** rather than the branch: `+601` green · `+602`
  headroom RED · `+1501` headroom RED · `+1502` both RED. `via: measurement`
- **Ruled out:** that the auditor's `1927 collected` contradicted the orchestrator's `1938`.
  The PR adds **zero** Python test functions; `main` added **11** since the merge base
  `83c6ba4`; 1927 + 11 = 1938. Both readings are correct and describe different trees.
  `via: measurement`
- **Next probe:** none for this block — it is closed. The live question has moved to the fix
  round and round 2, below.

### The round-1 fix round, and the round-2 delta that must follow it
- as-of: 2026-09-16
- **Symptom + exact repro:** not a defect — an unfinished ladder. Round 1 returned five
  findings, so by the stop rule another round is owed; a ladder that stops on findings has
  shipped the finding.
- **Observed (with values):** round 1 ledger — five findings (3 🟡, 2 🟢), zero 🔴, verdict
  "safe to merge" **which is explicitly not the stop signal**. Each finding re-verified at
  source by the orchestrator: the `config_test.go` comment claiming "discovered rather than
  restated" sits directly above a hand-written 15-element literal; `anySet` is
  `strings.TrimSpace(env[name]) != ""`; `jwks.go:89` says "Zero disables the timer" while
  `:144` maps zero to `DefaultJWKSInterval`. Tree-wide enumeration for `-race`
  (`find … -print0 | xargs -0 grep`, **not** the `.gitignore`-blind `grep -r`) returned five
  hits, **none of them a runner**.
- **Ruled out:** that the fix agent could safely use `isolation: "worktree"`. On this repo
  that flag branches from the DEFAULT branch, so it would hand back a tree of `main` with no
  `internal/identity` at all. The worktree was built by hand at the PR head instead.
  `via: code` — the flag's documented behaviour, already measured twice on this repo.
- **Leading hypothesis:** round 2's likeliest finding is a **sentence the fix round wrote to
  explain itself**, not code — the shape this ladder has hit repeatedly. The named trap here
  is the `-race` sentence: `internal/control/cache_test.go:81` and `:460` already record that
  `-race` is structurally blind to, and green on, a real defect in this tree, so a fix-round
  sentence implying `-race` covers concurrency correctness would be a fresh false claim
  inside the section whose job is honest inventory.
- **Next probe:** once the fix round pushes, re-run the full gate set on a freshly-built
  MERGED tree, then dispatch round 2 with
  the `/audit-pr` skill's own brief assembler (`audit-dispatch.py 35 --round 2`, whose path
  this doc deliberately does not name) and **read its stderr**
  for the "newest claims block says round=N" line before dispatching.

### ✅ RESOLVED — the P4 audit ladder, five rounds plus a design pass
- as-of: 2026-09-17
- **Resolution:** closed. This supersedes the blocks above about PR #35's CI and the
  round-1 fix round. P4 merged as `2665ebb`.
- **Observed (with values):** round 1 → 3🟡 2🟢 · round 2 → 2🟡 3🟢, **every finding against
  round 1's own fix prose** · round 3 → 3🟡, including a **security-relevant regression
  caused by round 2's fix** · round 4 → 3🟡 2🟢 plus the verdict that the code wanted a
  design pass · round 5 → **3🟢, nothing requiring a code change**. Payload per round:
  81 → 99 → 173 → 1,089, cumulative 1,442, never zero, so the attribution gate never fired.
- **Ruled out:** that the ladder had left the PR and was auditing its own scaffolding. The
  payload count was non-zero every round, measured with
  `git log --numstat --format= --remerge-diff <audited>..HEAD --not origin/main`, rc 0,
  stderr silent, range non-empty each time. `via: measurement`
- **Ruled out:** that incremental fixes were converging. Four consecutive rounds each found
  one MORE instance of the same class, each in a place the previous round's prose said was
  covered — 7 instances over 6 settings. That is what bought the design pass. `via: measurement`
- **Next probe:** none — closed. The residuals are in Defects with closing conditions.

### PR #38 round 3 is owed, and the merged-tree gates were unfinished at handoff
- as-of: 2026-09-18
- **Symptom + exact repro:** not a defect — an unfinished ladder plus an unfinished
  measurement. Rounds 1 and 2 each found real defects, so by the stop rule round 3 is owed.
- **Observed (with values):** merged tree `d6aac74` (`origin/main` + `96be4ec`) confirmed
  `go vet` rc 0, `go test` 15 ok / 0 FAIL, `go test -race` 15 ok / 0 DATA RACE. `pytest`,
  `leakscan`, conformance and the battery did **not finish** before handoff. CI on `96be4ec`
  read `UNSTABLE` (still running), not failing.
- **Ruled out:** that the ladder had left the PR and was auditing its own scaffolding. Payload
  per round was non-zero every time — round 1 fixes **291 lines**, measured with
  `git log --numstat --format= --remerge-diff <audited>..HEAD --not origin/main`, rc 0, stderr
  silent, range non-empty. `via: measurement`
- **Next probe:** rebuild the merged tree and run the full gate set with **`python3 -u`** on
  the battery (see the Gotcha — its stdout is block-buffered), then
  the `/audit-pr` skill's own brief assembler (`audit-dispatch.py 38 --round 3`, whose path
  this doc deliberately does not name) and dispatch it against a detached worktree at the PR
  head, never `isolation: "worktree"`.

