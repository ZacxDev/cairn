# Handoff: cairn-next-phase — 2026-09-29

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
Four operator-asked features for cairn's next phase: external references, user-stated
requirements, scope tags, and extracting the store's command surface out of the agent
skills into deterministic scripts. API, CLI and UI integration each where appropriate,
with test coverage.

- **closing-condition:** `check` — all four task-board cards **662, 663, 664, 665** at
  `complete`, each one's PR merged and verified **by content** on its repo's mainline (a
  squash never makes the branch head an ancestor, so ancestry is the wrong test). A later
  session runs `clawgatectl task get <id>` for the four and `git grep` for each feature's
  marker on `origin/main`. ADDRESSED ⇒ arc CLOSED.

## State now

- Base clone `main` is **behind 1** — `git -C /home/zach/workspace/cairn fetch origin && git merge --ff-only origin/main`.
- ✅ **662 — `refs:` + URL templates + `--ref-to`/`?ref-to=` reverse lookup. MERGED, squash `5c59169`.**
  Verified by content on `main` with a positive control (the deleted `RefKeyRemovalAnchor` is
  absent). Card is `complete`. Ladder: round 0 + four delta rounds, every round's findings
  fixed and verified, four `audit-claims` blocks on the PR.
- ✅ **663 — entry-level `tags:` + scalar `--tag`/`?tag=`. MERGED, squash `94ecb7e`.**
  Verified by content; deleted repeatability machinery absent as the positive control. Ladder:
  round 0 + round 1. 🔴 **Card 663 has NOT been written back** — that is rank 1 below.
- ⏳ **665 — the store's command surface as four scripts plus thin routing skills. PR #1905
  OPEN at `5c34b707`** in the operator's shared rules-and-dotfiles repo. Fully verified; gating
  only on that repo's four CI checks against the current base. Card not written back.
- ⏳ **664 — `## Requirements` section. NOT STARTED**, card `open`. It was sequenced behind
  663 deliberately: all three data features regenerate the same golden fixtures.
- 📋 Filed rather than fixed: **681** (eight items 662 declared open), **682** (`AGENTS.md`
  one-byte headroom), **683** (a pre-existing full-suite failure in the other repo, plus a
  second one and the eight-char listing headroom).
- ⚠ **Claims still held:** `cairn-next-phase-c` and `cairn-next-phase-d`. Release both when
  663's write-back and #1905's merge land (`claim-work --release <slug>`).

## Open investigations — live diagnosis state

### A test fails only in full-suite context, so no local full-suite run in the other repo can be green
- as-of: 2026-09-29
- **Symptom + exact repro:** `scripts/browser-bridge/tests::test_the_release_handler_EXITS_rather_than_resuming[INT]`
  fails in every full-suite run and passes in isolation. Repro: the repo's own runner with
  `--set all`; then the same target alone with `--targets`.
- **Observed (with values):** four full runs, all `1 failed / 989 passed` in that target —
  under 3–4 concurrent suites at load 40; at load 13→39; on a **quiet box at load 5–8**
  (49:58); and in a paired control on that repo's mainline **alone**, carrying none of the
  candidate PR's ~47 added tests (260.99s). Isolated: **990/990 PASS** both via bare pytest
  (209.98s) and via the runner (240.81s, `RESULT: PASS`). `exit=1` throughout — a real code.
  `via: measurement`
- **Ruled out:** load. Run 3 on a quiet box produced the identical failure; the first
  diagnosis called it load-induced, leaning on the test's own comment about starvation, and
  that call is **retracted**. `via: measurement`
- **Ruled out:** the candidate merge. The paired control fails identically on the mainline
  alone, which also excludes the one channel a byte-identical suite leaves open — a preceding
  target shifting machine state before a ~3-second race. `via: measurement`
- **Ruled out:** two runs that reported `exit=143`. That is 128+15, SIGTERM — aborted runs,
  not failures, and neither is evidence about anything. `via: measurement`
- **Leading hypothesis:** something in full-suite context — the target passes alone and fails
  only after the other 30 targets have run. That is the discriminator, and it is not yet a
  mechanism. `via: assumed`
- **Next probe:** bisect the target set — run `browser-bridge` after progressively larger
  prefixes of the other 30 targets until it flips, then read what the last-added target leaves
  behind. 🔴 **Run targets SOLO.** A concurrency attempt contaminated a different target
  because `test_run_tests_preconditions.py` spawns nested runner instances from 11 call sites.

## Next steps (ranked)

🔴 **NUMBERING IS STABLE — every number KEEPS ITS LINE even when done, because a rank is half a
`claim-work` slug.** Re-ranking re-points every live claim.

1. **Write back task-board card 663** (`tags:`, merged as `94ecb7e`) and release the
   `cairn-next-phase-c` claim. The card is author-specified and every criterion is validated,
   so the status gate permits `complete`.
   forcing: gate — an unwritten card is re-dispatched and paid for twice; the board has two
   recorded instances.
2. **Land PR #1905** in the operator's shared rules-and-dotfiles repo (the four scripts).
   Gate on that repo's four CI checks against the **current** base — its mainline moved four
   times during the work. ⚠ A clean local full-suite run is **not available** (see the
   investigation above and card 683); gate on CI and the byte ceilings, and say so in the
   merge note so nobody reads it as ignoring a red gate.
   forcing: user — the operator approved the merge conditional on verification.
3. **Write back card 665** and release `cairn-next-phase-d`, once 2 lands.
   forcing: gate — same as rank 1.
4. **Build 664 — the `## Requirements` section.** Its blocker (663) has cleared. Operator
   decision already recorded on the card: a fourth canonical section reusing the existing
   `OPEN:`/`RESOLVED <sha>:` markers, with provenance read from a prefix **after** the marker
   so the anchored marker regex stays byte-unchanged.
   forcing: user — one of the four features the operator asked for.
5. **Close or dismiss card 681's eight items.** Dismissal in writing is explicitly acceptable.
   forcing: none
6. **Free real headroom in `AGENTS.md` (card 682), or accept the one byte in writing.**
   forcing: gate — the next edit by any session reddens the merged tree.
7. **Diagnose card 683's full-suite failure**, in the other repo.
   forcing: gate — a permanently-red local gate trains everyone to click through, and there
   are now two of them.

## Defects (batched)
- **`?q=` and `?tag=` do not compose on the browse surface** — `/?q=a&tag=b` renders two
  independent cards, and the search form carries no hidden tag field, so refining a query
  silently drops the tag. On the pod they compose into one narrowed search. Declared in
  `internal/ui/README.md` with a closing condition; composing changes a deployed answer.
- **Nothing in the browser has been walked for `/?tag=`** — `uiaudit` does not visit it, no axe
  pass has run over the new card. Declared in `internal/ui/README.md`.
- **`internal/report/testdata/reader_fixtures.json` carries no `ref-to` and no `tag` case**, so
  the renderer's own differential fixture never covered either filter.
- **The two new parser refusal messages are not byte-compared across implementations** — the
  corpus world holds one malformed entry and no row renders a `tags:`-specific reason. Systemic:
  the same is true of every `aliases:`/`refs:` refusal.
- **Four files are not `gofmt`-clean and nothing greps it** — `internal/client/{anchor_test,exit,options}.go`
  and `internal/control/tokenfile/source.go`. Pre-existing.

## Gotchas / decisions / dead-ends

- **Operator decision: `refs:` replaces `tasks:`, aliases PERMANENT.** Deleting the warning
  machinery also deleted the removal anchor, which silently made the aliases permanent; the
  operator confirmed that and the wording was swept to match. Measured before deciding:
  **0 of 440 entries** across both live caches carried `tasks:`/`task:`, with a positive
  control proving the reader works — so the warning was unreachable in practice.
- **Operator decision: `--tag`/`?tag=` is SCALAR.** Repeatability had **no author of record** —
  the operator asked for a tag filter; set-algebra-over-operands was written into the criteria
  by the dispatching session and never decided. Round 0 is the only round that can find that
  class; four correctness rounds would not have.
  🔴 **The deletion removed a defect rather than fixing one:** the browse surface read only the
  first `?tag=` value while the pod ANDed all of them, so the same parameter meant two things.
- 🔴 **THE LADDER'S RECURRING DEFECT: a sentence written WHILE fixing the previous round's
  sentence.** Three of 662's five rounds found exactly that, and one comment had gone stale
  **four times while warning about its own staleness**. It is now pinned by a gate that reads
  `ci.yml`'s actual value rather than restating it — derivation-with-comparison is the only
  structure that resists this.
- 🔴 **A BYTE-GATED SHARED FILE TURNS MERGE ORDER INTO A GATE NOBODY AUTHORED.** Twice this
  session: `20,234 + 41 + 29 = 20,304` against a floor at 20,300 — neither side breached, the
  sum did, git merged cleanly, and the second PR to land was blocked by arithmetic neither
  author performed. Cleared by **eviction** (freed 91 B), not a 4-byte shave. `AGENTS.md`
  landed at **one byte** of headroom the same way. The fix is real headroom; a shave leaves it
  true for the next pair.
- 🔴 **A GUARD CAN BE NARROWER THAN ITS OWN DESCRIPTION, AND A LEDGER'S REGEX IS WHERE IT
  HIDES.** A parameter ledger claimed every read parameter was swept; its character class
  `[a-z_]+` **excluded every hyphenated name**, so two parameters were unledgered — one of them
  shipped by the PR the ledger was supposed to cover.
- 🔴 **A MUTANT SURVIVED A FULLY GREEN SUITE IN BOTH LANGUAGES.** A two-term predicate had one
  term exercised by nine rows and the other by none; deleting it left 8/8 rows, 19 Go packages
  and 2206 Python tests green. Ask which TERM a table covers, not which cases.
- 🔴 **A POSITIVE CONTROL SURVIVED ON ITS FIRST RUN, and that was the finding.** A substring
  match was satisfied by `status=tag-absent-XX`. Pin the whole normalised line.
- 🔴 **`mergeStateStatus: CLEAN` IS NOT A SETTLE SIGNAL — and here it is structural.** This repo
  posts **no legacy commit statuses at all**, so `/commits/<sha>/status` reads `pending` forever
  with `total_count=0` and `CLEAN` appears within seconds of a push while all 8 jobs run. Read
  **check-runs**. Three agents were fooled before the cause was found. The `go` job legitimately
  takes **35–47 minutes** while the other seven finish inside ~11.
- 🔴 **A PIPE EATS THE VERDICT.** A mutation battery run through `| tail -18` swallowed the
  `SUMMARY` line *and* the real exit status — the reported "exit code 0" was `tail`'s. Same trap
  hit a `go vet` check where the captured status was `grep -v`'s.
- 🔴 **A SAME-LENGTH `.py` EDIT CAN BE SERVED FROM A STALE `.pyc`** — CPython validates on
  mtime-in-whole-seconds plus size, so a *restored* file appeared to fail. Run mutation work
  under `PYTHONDONTWRITEBYTECODE=1` or clear `__pycache__` between mutants.
- 🔴 **`git clone` MAPS THE SOURCE'S LOCAL BRANCHES ONTO THE NEW CLONE'S `origin/*`.** A clone of
  a base clone inherited a stale local branch ref and produced a confident, wrong merge-tree
  reading. Resolve a PR head from `gh pr view --json headRefOid`, never from a local ref.
- 🔴 **`ls --time-style` PRINTS LOCAL TIME.** Polling with `date -u` produced a five-hour phantom
  gap that nearly read two live runs as dead; `/proc/<pid>/fd/1` settled it.
- 🔴 **zsh ATE `$r:claude/...` AS A HISTORY MODIFIER**, returning a confident `0 B` for every ref.
  Brace it: `${r}:...`. Hit twice this session, once by an agent and once by the coordinator.
- ⚠ **An agent's own worktree pins its branch repo-globally.** A *finished* agent's worktree
  held the PR branch twice, which would have failed the next agent's checkout. Reap finished
  worktrees after verifying clean + fully pushed.
- ⚠ **Decision: merge main INTO a branch rather than rebasing when another session holds a claim
  on it** — with `--squash` the landed content is identical and it avoids force-pushing onto
  someone else's claim.

## How to verify

```bash
cd /home/zach/workspace/cairn
nix develop -c bash -c 'go vet ./... ; echo vet=$?'
nix develop -c bash -c 'go test ./... > /tmp/t.out 2>&1; echo rc=$?; grep -c "^ok" /tmp/t.out; grep -c FAIL /tmp/t.out'
nix develop -c python3 -m pytest tests -q -p no:randomly
tests/conformance/run_go.sh
nix develop -c python3 tests/conformance/suite.py run
nix develop -c python3 tests/parity/harness.py
nix develop -c python3 tests/parity/harness.py --self-test     # sabotaged=4 caught=4
nix develop -c python3 tests/dualrun/harness.py                # read BOTH arm lines vs ci.yml's floors
python3 tests/leakscan.py --self-test && python3 tests/leakscan.py
```
🔴 Read every status **off the command, never through a pipe**, and count the runner's own
result lines. All gates as `nix develop -c …` — bare `go` here is 1.26.7 against a pinned
1.25.14.

**The two merged features, verified by content rather than ancestry:**
```bash
git grep -l 'type EntryRef struct' origin/main     # 662
git grep -l 'StatusTagAbsent' origin/main          # 663
git grep -l 'RefKeyRemovalAnchor' origin/main      # want ZERO — the deletion's positive control
```
