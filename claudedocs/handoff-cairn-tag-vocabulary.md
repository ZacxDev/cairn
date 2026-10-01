# Handoff: cairn-tag-vocabulary — 2026-10-01

## Run this first — the index, one command
```bash
<tooling>/scripts/cairn-ops/read.sh recall --repo "/home/zach/workspace/cairn"
```
`<tooling>` is the private tooling checkout; its path is already exported in the shell. It is
not named here because this repo is PUBLIC and its name is a denied identifier.
Terse pointers this doc does not carry, curated by past sessions and outliving it.
🔴 RECALL, NOT LIVE OBSERVATION — every line is a pointer to VERIFY, never a current
reading, and it may describe a gotcha already fixed. `scope-absent`/`scope-empty` means
nothing is recorded yet: ordinary, not an error, and not a clean bill of health.
Non-blocking: if it exits non-zero, print the stderr line and carry on.

## Goal
Put the three shipped entry features (`tags:`, `refs:`, `## Requirements`) into actual
use across the store, and close the tag vocabulary so a typo cannot land silently. The
predecessor arc `handoff-cairn-next-phase.md` shipped the features; this arc is about
them being USED and ENFORCED.

- **closing-condition:** `check` — every entry on its resolved instance carries a tag
  from the closed set, the pod REFUSES an off-vocabulary tag at the write path (422,
  `X-Store-Status: entry-shape`) with a valid tag still accepted as the control, and both
  halves are verified against the DEPLOYED pod rather than against `main`.
  **MET 2026-10-01** — see "How to verify" for the exact probes and their outputs.

## State now
- Branch: `main` at **`fc2ddfe`** (NOT `ffa0eca` — see the base-moved gotcha below). This
  doc's own branch `docs/handoff-cairn-tag-vocabulary` is still **UNMERGED (PR #169)**,
  `MERGEABLE`/`UNSTABLE`, 1 commit behind `origin/main`.
- 🔴 **RANK 1 IS NOT MERGED, AND IT NOW CONFLICTS.** PR #170 went
  `MERGEABLE` → **`CONFLICTING`/`DIRTY`** mid-session, because **#168 merged under it**
  (another session owns `cairn-recall-entry-bullet-badge`). Measured, not inferred:
  `git merge-tree --write-tree origin/main origin/feat/browse-q-and-tag-compose` exits **1**,
  and the conflict is **ONE file — `CHANGELOG.md` (content)**; `README.md` auto-merged.
  ⚠ **#170's 7-of-8 green is evidence about the PRE-#168 tree and nothing else.** It must be
  re-run after the resolution: #168 touches `internal/report/text.go` while #170 touches
  `internal/ui/render.go`, and disjoint files are not safety.
- **Rank 2 is CLAIMED (`cairn-tag-vocabulary-2`) and IN FLIGHT, not verified.** A subagent is
  building all three declared fail-opens in `.claude/hooks/base-clone-write-guard.py` in an
  isolated worktree, plus the two triaged comment defects. **No PR, no review, no red/green
  matrix seen at the time of writing.** Treat every claim about it as unmeasured.
- **Base clone is CLEAN and the staged-orphan defect is CLOSED** — see Defects.
- ⚠ **No task-board field is recorded, and that is a REFUSAL rather than a zero.** The resolver
  exited **5** — nothing resolved. An unknown session id answers 200 with an empty array, so
  this cannot distinguish "touched no task" from "wrong id". Not a clean bill of health.
  🔴 **And this bullet is where the leak gate fired on THIS session's delta**: naming the board
  is a `denied-identifier`, so the field's own spelling cannot appear in this repo. The previous
  handoff recorded that exact trap and I reproduced it anyway — the gate is what caught it.
- Carried forward, still true and NOT re-measured this session: **five PRs merged and verified
  by content** on `origin/main` (#164 scrub + digests, #160 `tags:` closed at the write path,
  #162 `refs` badge/label, #165 + #167 the write guard judging the TARGET); **303 entries
  tagged** across both instances (`infra` 136 / `product` 99 / `tooling` 68, zero
  off-vocabulary); the pod and UI pins moved `e8839d9 -> ffa0eca` in one commit and **the POD
  is confirmed rolled** while **the UI is not** (Open investigations block 1).

## Open investigations — live diagnosis state

### The UI pod's rollout was never independently confirmed
- as-of: 2026-10-01
- **Symptom + exact repro:** the pod and UI pins moved in one commit and the POD is
  confirmed rolled; nothing confirms the UI container is serving the new image.
- **Observed (with values):** pod-rendered `GET /api/v1/recall/cairn` with a valid bearer
  token returned **200**, body 7422 B, containing `🔗 N ref` **×4** and `🔗 N task` **×0**.
  That is the pod. The UI's equivalent signal is behind a cookie session.
- **Ruled out:** "the CSS fingerprint will tell us" — `/static/app.<hash>.css` is a build
  fingerprint but the change touched no CSS, so it cannot move. `via: measurement`
  (fetched `/sign-in` unauthenticated, hash `app.e9b33ecf4a5b.css`).
- **Ruled out:** "use the browser bridge" — the extension reports `connected: 0`, so no
  authenticated page can be read from here. `via: command`
  (`scripts/browser-bridge/browser whoami`).
- **Leading hypothesis:** it rolled. Both pins are in one commit, applied by one
  reconcile, and the pod demonstrably moved — but that is inference, not observation.
- **Next probe:** in a signed-in browser, open the browse page filtered by a tag, click any
  entry, and read the Refs panel heading. `refs` ⇒ rolled; `tasks` ⇒ pod and UI are out of
  step and the UI deployment needs looking at.

### A hook that fails to START is asserted to be an ALLOW, and nobody has measured it
- as-of: 2026-10-01
- **Symptom + exact repro:** `.claude/hooks/base-clone-write-guard.py`'s own docstring says
  every unexpected condition exits 0 and says nothing, i.e. a hook that cannot start lets
  the command run. The entire fail-open/fail-closed design rests on this.
- **Observed (with values):** an independent audit measured that **exit 1 produces no
  `deny` on stdout**, so a crashing hook does not block. It had no read of the harness's
  handling of a hook that never starts at all (missing interpreter, syntax error, chmod).
- **Ruled out:** nothing yet — this is unprobed rather than narrowed. `via: assumed`
  (the claim is the file's own, carried forward across three PRs without measurement).
- **Leading hypothesis:** a hook that fails to start is indeed an ALLOW, which means the
  guard's protection is only as good as the file being loadable.
- **Next probe:** in a THROWAWAY project dir (never this repo, and never while agents are
  working against the live hook), register a hook that exits non-zero before doing
  anything, and a second that is not executable, then attempt a write the guard would
  refuse. Record which of the two, if either, still blocks.

## Next steps (ranked)
🔴 **Numbering UNCHANGED on purpose — the rank is half a `claim-work` slug's identity.** Closed
items are struck, never deleted.

1. ~~**Make `?q=` and `?tag=` compose in the browse surface.**~~ **THE BUILD IS CLOSED** — PR
   [#170](https://github.com/ZacxDev/cairn/pull/170), `feat/browse-q-and-tag-compose`
   (`5e7646d` the feature, `9eacee0` the CHANGELOG row). 🔴 **BUT THE MERGE IS NO LONGER A
   MERGE — IT IS A CONFLICT RESOLUTION.** Resolve `CHANGELOG.md` in a worktree (both sides
   append rows; keep both), push, **wait for a FULL CI re-run**, then merge. Do not merge on
   the stored green: it was measured before #168 existed.
   forcing: gate — the PR cannot merge while GitHub reports `CONFLICTING`.
2. **Close the three declared guard fail-opens.** IN FLIGHT: claimed `cairn-tag-vocabulary-2`,
   subagent building. All three are recorded with closing conditions in
   `.claude/hooks/base-clone-write-guard.py`'s docstring: (a) `_shell_lines`' heredoc opener
   regex runs on the raw line, so ordinary text containing `<<WORD` silently drops every later
   line; (b) the program-name walk misses `if`, `while`, `command`, `nohup`, `timeout`, `eval`,
   `stdbuf`, `exec`, `sudo`, `xargs`; (c) the `_REFUSED` complement is called "reads" while
   `clean -fd`, `rm`, `mv`, `worktree remove` and `branch -D` all write shared state.
   **The (c) decision is MADE — see Gotchas; do not relitigate it.**
   forcing: security — each is a measured way to land a commit on the wrong branch in a shared
   clone, which is the single failure this guard exists to prevent.
3. **Confirm the UI pod rolled** (Open investigations block 1), and on the SAME visit open
   `/?q=<word>&tag=<tag>` and confirm it renders ONE card rather than two. ⚠ The second half
   only exists at the edge **after #170 merges AND the pin moves** — so this is gated on rank 1,
   and doing it early spends the operator's screen twice for half an answer.
   forcing: user — the operator asked for this deploy to be validated, and this is the one half
   that could not be closed from here.
4. **Teach the entry template to emit a `tags:` line.** `scripts/lib/subsystem_touch.py
   --template` emits none, so every entry born through the handoff flow starts untagged and
   invisible to `--tag`. Repo: the private tooling repo.
   forcing: regression — new entries reintroduce the untagged state this arc eliminated.
5. **Land this doc on the mainline.** PR #169 carries the only copy; until it merges, `/resume`
   from `main` dead-ends for this arc. It is `MERGEABLE` now and 1 behind.
   forcing: gate — the resume path is the mechanism, and it is currently broken for this arc.

## Defects (batched)
🔴 **This heading REPLACES on every update — so everything still open has to be re-listed here
or it is deleted.** That is how this list is maintained; it is not a sign the earlier text was
wrong.

- ✅ **CLOSED: the base clone's STAGED modification to the hook.** It was byte-identical to
  `origin/main` (both blob `28ca42c`), so `git restore --staged --worktree` on that path
  followed by `git merge --ff-only origin/main` was **content-neutral** — verified by
  re-hashing the file after: still `28ca42c`. The earlier entry describing it as open, and its
  instruction to clear it, are **superseded — do not re-run them.**
- **IN FLIGHT under rank 2** (carried forward, not closed): `base-clone-write-guard.py:702`
  still says the existence check lives "ahead of the probe budget", which #167 deleted; and
  `_abs_path`'s claim that a second existence check "could never change a verdict" was measured
  FALSE when a NUL byte crashed the hook, has plausibly been restored by #167's fix, and **has
  not been re-measured.** Both are in rank 2's brief. Re-verify from the PR, not from this line.
- ⚠ **STILL OPEN: the pod's composed `?q=`+`?tag=` answer and the browse surface's are not
  compared against each other.** After #170 they share the ENGINE, which is strictly more than
  they shared before, but nothing sends the same two parameters to both and diffs the result.
  Declared in `internal/ui/README.md` rather than fixed.
- ⚠ **STILL OPEN: `uiaudit` walks neither `/?tag=` nor the composed card**, which after #170 is
  a fifth card shape carrying three `note` links and a hidden form control. No axe pass has run
  over either.
- ⚠ **Newly measured as REACHED: #170 and #168 share `CHANGELOG.md` and `README.md`.** The
  previous handoff predicted the interaction; #168 merging made it a real conflict. Nothing in
  this repo serialises two PRs that both append a CHANGELOG row.

## Gotchas / decisions / dead-ends
- 🔴 **The per-scope tag table cannot live in this repo.** `tests/leakscan.py` denies the
  scope names as `denied-identifier`s and this repo is PUBLIC. The RULE (exact match,
  never a prefix — with a counterexample) ships here with a SYNTHETIC table; the real rows
  live in the store entry `cairn/tag-vocabulary`. An attempt to commit them was refused by
  the leak gate with 9 findings, which is the gate working.
- 🔴 **A write-time gate, never a parse-time one.** A refusal inside the reader's front
  matter parser makes an entry MALFORMED — out of the index, out of `--ref`/`--search`,
  AND unwritable, because the write route resolves through the index. That would have
  bricked every entry carrying an off-vocabulary tag. Verified against the code, not
  assumed.
- ⚠ **A fourth vocabulary term was tried and removed.** `client-work` answers *whose* work
  where the other three answer *what kind*; mixing axes in one closed set under a
  one-tag-per-entry rule makes both unassertable, and the scope name already carries
  whose. 65 entries moved to `product`.
- ⚠ **`--tag` is scalar and the vocabulary is CLOSED, but a tag query is NOT checked
  against it** — a typo in the QUERY still returns a clean zero. Read the denominator the
  reader prints (`N of M entries in <scope> carry it`); a bare zero proves nothing.
- 🔴 **Variables in a `git -C` argument are refused by the write guard, by design.** #167
  deleted the shell-variable resolver because it opened four fail-opens. Pass `-C` a
  LITERAL absolute path; a `$VAR` is unresolvable and falls back to refusing. This bit the
  authoring session twice in one hour.
- ⚠ **An image pin bump deploys every commit merged since.** The one landed here carried
  19 commits (PRs #149–#167), not the two it is named for; the commit message enumerates
  them. Say what a bump carries before pushing it.
- ⚠ The client instance's migration to the Go image is owned by a DIFFERENT session. Do
  not touch that cluster's manifests from this arc.

- 🔴 **THE CANONICAL HANDOFF FOR THIS ARC IS NOT ON `main`.** It lives only on
  `docs/handoff-cairn-tag-vocabulary` (PR #169, open). A `/resume` kickoff naming
  `claudedocs/handoff-cairn-tag-vocabulary.md` dead-ends from a `main` checkout. The way to find
  it is `git log --all --oneline --diff-filter=A -- '*<topic>*'`, then read it out of the ref
  with `git show <branch>:<path>` — **a plain `grep -rl` over the tree finds nothing**, because
  the file is not in the working tree at all.
- 🔴 **`git -C "$VAR"` IS REFUSED BY THE BASE-CLONE WRITE GUARD, AND THE REFUSAL NAMES THE WRONG
  DIRECTORY.** #167 deleted the shell-variable resolver, so a `$VAR` is unresolvable and the
  guard falls back to refusing — and the refusal text says the base clone is the target even
  when the variable holds a worktree path. This is already in the doc as a decision; recording
  the SYMPTOM because it is what a reader actually sees. Pass `-C` a LITERAL absolute path. Bit
  this session once, and the previous session twice in one hour.
- ⚠ **`/home/zach/workspace/cairn-handoff` is a STALE worktree sitting on
  `docs/handoff-share-flow-audited` at `b707ab9`, a closed arc.** `worktree add` to that path
  fails with "already exists", and the obvious recovery — `switch -c` in it — silently moves
  somebody else's worktree off its branch. Restored this session; use a fresh path
  (`cairn-hv` was used here). 59 linked worktrees are registered against the base clone.
- 🔴 **THE COMPOSED SHAPE WAS NOT A FREE CHOICE, AND THE REASONING IS WORTH KEEPING.** The query
  decides the ANSWER SHAPE and the tag decides WHAT IT RAN OVER. A search is ranked; a tag
  listing is a membership test with nothing to rank; there is no one card that is both. The
  reverse order — a tag listing filtered by the query — would answer a two-operand URL with an
  unranked list, which is the shape the pod does not produce. So `?tag=` alone keeps its listing
  and adding words turns it into a search WITHIN that tag.
- ⚠ **A rendered card-what sentence is a claim, and composing made one of them FALSE.** "Scored
  over every line of every entry the credential can read" describes an unnarrowed search; under
  a tag it over-claims. Two whole constants now, pinned separately — concatenating a clause onto
  a shared half would let one edit move both pinned strings at once.
- 🔴 **A MUTATION MEASUREMENT CORRECTED MY OWN COMMENT, AND THE CORRECTION IS THE RECORD.** The
  draft said a four-entry fixture would let an `EntriesSearched`⇄`TagSkipped` swap survive a
  green suite. Run against both fixtures, it would NOT have: the composed-ZERO sub-test killed
  it there (counts 1 and 3, not 2 and 2). The fifth entry buys each sub-test seeing the swap on
  its OWN assertion rather than rescuing the suite — a smaller claim, and the one now written in
  `internal/ui/compose_test.go`.
- ⚠ **Local Go here is 1.26.7 against a 1.25 `go.mod`.** It builds and tests fine, but the
  pinned-toolchain run is CI's, not a local one. Do not report a local green as a pinned green.

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
- 🔴 **`_REFUSED` IS PINNED AGAINST THE DOC AND FAILS ON GROW *OR* SHRINK.**
  `tests/test_base_clone_write_guard.py::test_the_refused_set_matches_the_documented_table`
  parses the `_REFUSED` literal out of the hook source and compares it to the subcommands in
  backticks under `claudedocs/working-in-parallel.md`'s `| refused | why it collides |` table.
  **Both move in ONE commit**, and two companion tests exist so the comparison cannot be two
  empty sets.
- 🔴 **THE BASE CLONE MOVES UNDER YOU, AND `git log` IS WHERE YOU FIND OUT.** This session
  ff-merged to `ffa0eca`, then found HEAD at `fc2ddfe` later in the same session — another
  session merged #168 and re-synced the shared clone. **Re-read `git log`/`rev-parse` at the
  moment you act on a PR's mergeability, never from the survey that motivated it.** The cost
  here was a stated "all three PRs are 0 behind" that was true when measured and false twenty
  minutes later, in the direction that matters.
- 🔴 **A BASE MOVE RESETS `mergeable` TO `UNKNOWN`, AND `UNKNOWN` IS NOT `MERGEABLE`.**
  Immediately after #168 landed, `gh pr view 170 --json mergeable` answered `UNKNOWN` — GitHub
  recomputes asynchronously. **Poll until it is not `UNKNOWN` before believing either answer**;
  reading `UNKNOWN` as "fine" is how a conflicting PR gets a merge attempt.
- ⚠ **`git merge-tree --write-tree` exits non-zero on conflict and prints the conflicting paths
  — branch on the EXIT CODE.** It emits NO `<<<<<<<` markers, so grepping for them finds
  nothing whether or not a conflict exists. Used correctly here it named `CHANGELOG.md` in one
  command.
- ⚠ **The `go` CI job on this repo runs ~25–35 min and `gh pr checks` shows it as
  `pending 0`.** Its steps are a 189-mutant authz battery, the Go client ledgers, a 57-mutant
  routing battery and the conformance corpus. `gh run view --job <id>` shows per-step ticks and
  is the only way to tell "slow but advancing" from "stuck" — do not read `pending 0` as stuck,
  and do not read a 20-minute watcher timeout as a failure.
- ⚠ **`/home/zach/workspace/cairn-hv2` was used as this session's worktree for the handoff
  branch**, because `cairn-handoff` is still the stale worktree on a closed arc. Remove it when
  done. 60 linked worktrees are now registered against the base clone.

## How to verify
Rank 1's state, first — it is the item whose status changed:
```bash
gh pr view 170 --json mergeable,mergeStateStatus      # must not be UNKNOWN before you believe it
git -C /home/zach/workspace/cairn merge-tree --write-tree \
    origin/main origin/feat/browse-q-and-tag-compose; echo "exit=$?"   # non-zero = still conflicting
```

**The write gate — negative then positive** (the positive control is not optional: without it a
refusal is indistinguishable from a broken write path):
```bash
<tooling>/scripts/cairn-ops/write.sh put --scope cairn --ref tag-vocabulary --file <bad copy> --no-verify
#   => 🔴 the store REFUSED the write [entry-shape] — ... is not one of infra|product|tooling
<tooling>/scripts/cairn-ops/write.sh put --scope cairn --ref tag-vocabulary --file <good copy> --no-verify
#   => cairn: replaced instance=... revision=...
```

**The renderer, read from the POD rather than the local client** (the installed CLI renders
locally, so it is NOT evidence about what is deployed):
```bash
set -a; . /home/zach/.config/subsystem-store/env; set +a
curl -s -H "Authorization: Bearer ${CAIRN_TOKEN}" "${CAIRN_URL}/api/v1/recall/cairn" \
  | grep -c '🔗 [0-9]* ref'     # => 4   and  '🔗 [0-9]* task'  => 0
```

**Tag coverage across the fleet** — read each scope on its RESOLVED instance; the two caches
replicate 12 scopes, so a naive walk double-counts:
```bash
<tooling>/scripts/cairn-ops/health.sh instances --scope <scope>
# expected totals: infra 136 / product 99 / tooling 68, and no fourth term
```

**Rank 1 — the composition, in the repo.** Three answers, not one: a composed page that is right
for the wrong reason is indistinguishable from either operand acting alone.
```bash
go -C <checkout> test ./internal/ui/ -run TestTheQueryAndTheTagComposeIntoOneCard -v
go -C <checkout> test ./internal/ui/ -run 'TestTheSearchFormRoundTripsTheTag|TestTheComposedCardOffersEveryWayBack'
#   => `?q=` alone names 3 entries, `?tag=` alone names 2, together they name the 1 in both.
```

**Rank 1 — against the DEPLOYED UI** (only after #170 merges and the pin moves). Open
`/?q=<a word in a tagged entry>&tag=<that tag>`: **ONE** card headed `Search`, its second line
naming both operands and both counts; typing a new word keeps the tag; emptying the box lands on
the plain tag listing. **Two cards is the pre-change answer and means the pin did not move** —
which is Open investigations block 1's question, answered by the same visit.
