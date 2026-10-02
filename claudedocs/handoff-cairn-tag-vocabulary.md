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
  🔴 **WAS DECLARED MET BY THE PREVIOUS SESSION; RE-MEASURED THE NEXT DAY AND IT IS NOT MET.** The
  write-gate half still holds live; the "every entry" half does not — 10 of 339. 🔴 **The condition is
  UNCHANGED and is NOT being rewritten to match the measurement** — it is frozen at round 1, and
  a verdict that moves when the answer is inconvenient is not a closing condition. See "State
  now" for both halves and "How to verify" for the probes that produced them.

## State now
- Branch: `main` at **`ffede27`**. Base clone clean, 0 behind. 🔴 **ONE OPEN PR — #172**
  (`docs/handoff-arc-closeout`, doc-only, 8/8 checks pass, `MERGEABLE`), and this delta is stacked
  ON it (`docs/tag-vocab-reopened`, branched from its head `239da57`) rather than off `main`, so the
  two cannot conflict on a 400-line doc. 🔴 **Do NOT merge #172 with `--delete-branch`** — GitHub
  auto-closes a PR whose base is deleted and refuses to reopen it.
- ✅ **THREE RANKS CLOSED AND MERGED, verified by content on `origin/main`**: rank 1 `5c96ffd` (#170),
  rank 2 `306164c` (#171), rank 5 `ffede27` (#169). Unchanged by this session.
- 🔴 **THE CLOSING CONDITION HAS RE-OPENED, AND IT WAS RE-OPENED BY A MECHANISM THIS DOC ALREADY
  PREDICTED AS RANK 4.** It was declared MET 2026-10-01. Re-run 2026-10-02, it is MET on one half and
  NOT MET on the other. Both halves below were measured this session; neither was taken from the doc.
- ✅ **HALF 2 — the write gate — STILL MET, measured live against the DEPLOYED pod**
  (the `personal` instance's `CAIRN_URL`; the host is a `reachable-hostname` and cannot be written
  here), negative then positive in one run:
  `PUT /api/v1/entry/cairn/tag-vocabulary` with `tags: [zzz-not-a-real-tag]` ⇒ **422**,
  `x-store-status: entry-shape`, body `tag 'zzz-not-a-real-tag' is not one of infra|product|tooling`;
  the same request with the tag restored ⇒ **200**, `x-store-status: replaced`,
  `etag: "f1040929673545e9"`. The positive control's ETag came back **equal to the `If-Match` sent**,
  and `cmp` then confirmed the cache copy byte-identical, so that write was content-neutral.
  ⚠ A first attempt without `If-Match` answered **428 `precondition-required`** — the precondition is
  checked BEFORE the shape, so a probe that omits it measures the precondition and reports nothing
  about the vocabulary.
- 🔴 **HALF 1 — "every entry on its resolved instance carries a tag from the closed set" — NOT MET.
  10 of 339 carry NO tag at all.** 0 carry an off-vocabulary tag, so the gate is holding; these ten
  did not defeat it, they were **born tagless and never went through it**. Spread over **9 of 29
  scopes** — one scope holds 2, eight hold 1 each. 🔴 **THE TEN FILENAMES CANNOT BE WRITTEN HERE:
  the scope names are `denied-identifier`s and this repo is PUBLIC** (the same ruling that put the
  per-scope tag table in the store rather than in `internal/write/scopetags.go`). They are recorded
  in the store entry `cairn/tag-vocabulary`; re-derive them any time with the sweep in
  "How to verify".
- 🔴 **ROOT CAUSE MEASURED, NOT INFERRED — IT IS RANK 4, AND RANK 4 HAS ALREADY FIRED TEN TIMES.**
  All ten are `created_by: handoff` and dated **2026-10-01/02**, i.e. on or after the day the
  condition was declared met. `subsystem_touch.py --template <slug> --writer handoff` emits
  `service:`, `scope:`, `sensitivity:`, `created_by:` and a commented `aliases:` — and **no `tags:`
  line and no `refs:` line**, for `--writer handoff` AND `--writer analyze-service`. So every entry
  born through either flow starts untagged and invisible to `--tag`. Rate so far: ~5/day.
- ⚠ **No task-board field, and that is a REFUSAL rather than a zero** — the resolver exited **5**
  again this session. An unknown session id answers 200 with an empty array, so a zero cannot
  distinguish "touched no task" from "wrong id".
- ⚠ **This session changed NO repository file and wrote NO entry content.** Its only write to the
  store was the content-neutral positive control above.

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

### Half 1 re-opened: are the ten untagged entries the template's doing, or authors forgetting?
- as-of: 2026-10-02
- **Symptom + exact repro:** `cairn recall --scope <s> --tag <t>` sums to fewer than the denominator
  it prints, for 9 of 29 scopes. Reproduce per scope with the three vocabulary terms and compare the
  sum of `N` against `M` in `tag: \`<t>\` — N of M entr(y|ies) in \`<s>/\` carry it`.
- **Observed (with values):** 339 entries on their resolved instances, 329 tagged, **10 untagged, 0
  off-vocabulary**. Two independent instruments agree on the same ten files and the same 339: a
  front-matter walk of both instance caches restricted by `routes.json`, and the reader's own printed
  denominators. All ten carry `created_by: handoff`; every one contains a date of 2026-10-01 or
  2026-10-02 and none earlier.
- **Ruled out:** "authors forgot, so it is a discipline problem" — the template itself emits no
  `tags:` line, so a tagless entry is the DEFAULT rather than an omission. `via: measurement`
  (`subsystem_touch.py --template probe-slug --writer handoff` and `--writer analyze-service`, each
  with a positive control: `grep -c '^service:'` = 1 in the same output where `grep -c '^tags:'` = 0).
- **Ruled out:** "the write gate regressed and let them through" — the gate answers 422 on an
  off-vocabulary tag right now, and none of the ten carries a bad tag; they carry none. `via:
  measurement` (the live 422/200 pair in `State now`).
- **Leading hypothesis:** the template is the whole mechanism, and the ten are its complete output
  since the condition was declared met. Fixing rank 4 stops the bleed; backfilling the ten closes
  the arc. Doing the backfill FIRST re-opens it within a day.
- **Next probe:** decide rank 4's shape — a placeholder `tags: []` the author must fill (fails loudly
  at nothing, so it needs a checker) versus a per-scope default read from `cairn/tag-vocabulary`'s
  real table (self-correcting, but a new scope has no row). That decision is the work, not the edit.

### An instrument lied three times in a row this session, and the controls are the only reason it is not in the numbers above
- as-of: 2026-10-02
- **Symptom + exact repro:** three consecutive greps over `subsystem_touch.py --template` output
  returned `0` for `^tags:` while measuring nothing at all.
- **Observed (with values):** (1) `--template` alone ⇒ `error: argument --template: expected one
  argument`, output empty, `grep -c` = 0. (2) `--template probe-slug` ⇒ `--template needs --writer`,
  output empty, `grep -c` = 0 — **and the positive control `grep -c '^service:'` was also 0, which is
  what exposed it.** (3) only `--template probe-slug --writer handoff` produced a real template, where
  `service:` = 1 and `tags:` = 0. A fourth: the denominator sweep's regex required the literal
  `entries`, so every ONE-entry scope read as `M=0` and six scopes were silently scored empty; caught
  by cross-checking against the filesystem walk, whose total disagreed by exactly 6.
- **Ruled out:** nothing — this is recorded as a method note, not a narrowing. `via: measurement`.
- **Leading hypothesis:** both are the same failure: a zero from a command that never ran what you
  think it ran. A positive control in the SAME output is what distinguishes them, and a plural-only
  regex is the cheapest possible version of it.
- **Next probe:** none needed. Carry the pattern: every sweep prints a line it MUST find beside the
  line it is counting.

## Next steps (ranked)
🔴 **Numbering UNCHANGED — the rank is half a `claim-work` slug's identity.** Closed items struck.

1. ~~**Make `?q=` and `?tag=` compose in the browse surface.**~~ **CLOSED, MERGED `5c96ffd`.**
   forcing: gate — merged; nothing remains.
2. ~~**Close the three declared guard fail-opens.**~~ **CLOSED, MERGED `306164c`.**
   forcing: security — closed.
3. **Confirm the deploy, in ONE signed-in browser visit.** Two answers from one visit: the Refs panel
   heading (`refs` ⇒ the UI pod rolled; `tasks` ⇒ pod and UI are out of step), and
   `/?q=<word>&tag=<tag>` rendering **ONE** card headed `Search` rather than two.
   🔴 **BLOCKED ON A PIN BUMP, WHICH IS THE OPERATOR'S CALL** — see State now. The browser bridge was
   measured disconnected (`connected: 0`) and has not been re-measured.
   forcing: user — the operator asked for this deploy to be validated.
4. 🔴 **Teach the entry template to emit a `tags:` line — THIS IS NOW THE ARC'S OWN BLOCKER, NOT A
   FOLLOW-ON.** `subsystem_touch.py --template … --writer {handoff,analyze-service}` emits none, so
   every entry born through either flow starts untagged; **10 such entries have landed since
   2026-10-01 and they are what re-opened the closing condition.** Repo: the private tooling repo.
   The open decision is placeholder-vs-per-scope-default (see the investigation block).
   forcing: regression — measured, ten occurrences, ~5/day, re-opening a condition already declared met.
5. ~~**Land this doc on the mainline.**~~ **CLOSED, MERGED `ffede27`.**
   forcing: gate — closed.
6. **Replace `ci.yml`'s hand-edited collected-test `FLOOR` with a baseline derived at the merge base.**
   Found by audit round 0, deliberately out of #171's scope. The literal has been moved by 22+ commits
   (and once per round during #171: 2458→2495→2529→2566→2588→2609→2617); the step is ~382 comment lines
   guarding one integer; its own comment says no bug ever narrowed this suite; it MISSED the one real
   deletion (the name-diff recipe caught that); and it has gone **RED with zero failing tests four
   times**. **Closing condition:** the literal is gone, the baseline is collected at `git merge-base
   origin/main HEAD` in the same job, the removals-must-be-renames check still runs, and one PR that
   adds tests goes green without touching `ci.yml`.
   forcing: gate — a permanently-red-prone gate trains everyone to click through, which
   `claude/RULES.md` names as worse than no gate.
7. **Backfill the ten untagged entries**, one vocabulary term each, AFTER rank 4 lands. The write gate
   is the checker: a wrong term is refused 422, so no separate validator is needed. 🔴 **Pick the term
   by EXACT scope match against `cairn/tag-vocabulary`'s real table, never by prefix** — the
   synthetic pair in `internal/write/scopetags.go` (`alpha` vs `alpha-fleet`) disagrees on purpose
   for exactly this reason, and the real table has a pair that disagrees the same way, so a prefix
   match gets it backwards.
   forcing: regression — these ten ARE the unmet half of this arc's closing condition.

## Defects (batched)
🔴 **This heading REPLACES on every update — everything still open must be re-listed or it is
deleted.** That is how the list is maintained, not a sign the earlier text was wrong.

- 🔴 **NEW, AND IT IS THE ARC'S OWN GAP: 10 of 339 entries on their resolved instances carry no tag.**
  Enumerated in `State now`. Rank 7 fixes them; rank 4 stops more arriving.
- 🔴 **NEW: `subsystem_touch.py`'s template emits no `tags:` AND no `refs:` line**, for both writers.
  `refs:` is the same class of silent omission as `tags:` and is not covered by any gate — the write
  path refuses a BAD tag, and has nothing to say about an ABSENT one. Rank 4 should fix both lines or
  say why not.
- ⚠ **NEW, pre-existing and NOT this arc: `cairn doctor` exits 9.** `personal/token-scopes` reports
  **two** scopes that exist on disk and not in the store's answer to that token, both **mirror-only**
  (they are named in the check's own output; both are `denied-identifier`s here) — so the question is
  whether they were ever seeded, not whether access was lost.
- ⚠ **NEW: six scopes hold their single entry on BOTH instances' caches.** Harmless for the tag
  question once `routes.json` is honoured, but it is why a flat two-cache walk reports **138**
  untagged where the routes-aware one reports **10**. Any future sweep over "every entry" must filter
  by resolved instance or it is measuring copies.
- ⚠ **STILL OPEN: the pod's composed `?q=`+`?tag=` answer and the browse surface's are not compared
  against each other.** After #170 they share the ENGINE, but nothing sends the same two parameters
  to both and diffs the result. Declared in `internal/ui/README.md` rather than fixed.
- ⚠ **STILL OPEN: `uiaudit` walks neither `/?tag=` nor the composed card** — a fifth card shape
  carrying three `note` links and a hidden form control. No axe pass has run over either.
- ⚠ **DECLARED LIMIT, not a defect: a comment claiming "this test reports/prints X" is NOT
  machine-checked.** The *existence* half is (`test_EVERY_TEST_A_PAYLOAD_COMMENT_NAMES_STILL_EXISTS`,
  shipped in #171). The *print/report* half was built, measured as a spelled guard firing on 7
  legitimate sites out of 8, and **deleted rather than narrowed to self-satisfaction**.
- ✅ CLOSED (carried forward — the RETRACTION is the load-bearing half): the base clone's staged
  orphan on the hook (was byte-identical to `origin/main`; cleared content-neutrally). ✅ CLOSED:
  both `_abs_path` comment defects — and **the retraction is recorded rather than the claim
  restored**, because #167 closed the NUL route while restoring the check would flip
  `GIT_INDEX_FILE=<clone>/.git/index git add` from deny to **ALLOW**. Do not "re-fix" it.

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

- 🔴 **WHAT THE AUDIT LADDER CAUGHT THAT A GREEN SUITE DID NOT — the single most useful record here.**
  Five rounds, each finding real defects, on a PR whose CI was **8/8 green at every step**:
  (a) `git rm -n --no-pathspec-from-file --no-dry-run seed.txt` **deleted a TRACKED file** in the
  base clone while the guard said allow — invisible to 315 passing tests, found only by a
  differential against real git; (b) a **regression a fix round itself shipped** (`\$'` leaving the
  escape flag set, disarming the guard for every later line); (c) **`git revert`** writing the
  shared tree and HEAD while the PR *pinned a claim* that the out-set was complete; (d) three
  classes of exact-string option matching that really destroyed files and refs.
  🔴 **EVERY FIX ROUND INTRODUCED THE NEXT FINDING.** Rounds 4 and 5 found no behavioural defects —
  by then the hook was AST-identical to base modulo comments, and what remained were claims about
  code. **That is where to stop**: the remaining class reproduced in every single fix round, so
  another round buys occurrence six, not convergence.
- 🔴 **A COUNT STATED IN PROSE BESIDE A THING THAT CHANGES IS THIS REPO'S MOST RELIABLE DEFECT.**
  It recurred in **every** round of #171, including inside the paragraph written to correct the
  previous occurrence (the floor heading was wrong three rounds running). **The fix is never better
  prose** — it is to delete the restatement, derive the number in a test, or state the INVARIANT
  instead of the enumeration. An invariant does not rot when a table grows; a count does.
- 🔴 **NINE INSTRUMENTS RETURNED A CONFIDENT WRONG ANSWER IN THIS ARC, AND EVERY ONE WAS CAUGHT ONLY
  BY A CONTROL.** `awk` with an end pattern matching its own start line (reported 0 prints for
  everything); a **case-SENSITIVE** sweep for a word written in capitals (0 hits vs 1); importing
  the hook as a module, which runs `main()` and `sys.exit(0)`s — **exit 0, no output, reads as
  "zero findings"**; a `" 1 passed"` matcher anchored mid-line; a differential pointed at the wrong
  tree (383 false fail-opens); a `-h` arity reader whose attached-arg branch matched empty; a
  marker-grep over `git merge-tree`, which prints no markers; a sweep whose own control string was
  the operator's real checkout path — in a PUBLIC repo; and a mutation anchor that never applied.
  **Validate the instrument, report the pair, and treat a reassuring zero as unproven.**
- 🔴 **`git merge --ff` IS NOT AN ABBREVIATION OF `--ff-only`** — they are two distinct options
  (`git merge -h`: `--[no-]ff` = "allow fast-forward (default)"), so `--ff` permits a merge commit
  and is correctly REFUSED. Read git's own option list rather than reasoning about prefixes; this
  nearly became a false finding against the guard.
- ⚠ **`git commit --dry-run` WRITES** — it adds a tree object. The discriminator is the **added
  object**, not the index: `git status` rewrites `.git/index` too (stat-cache refresh), so citing
  the index is not evidence. `git merge --no-commit` and `git cherry-pick -n` also write.
- 🔴 **A `claim-work`/`audit-claims` range endpoint TRUNCATED BY HAND silently disarms the gate.**
  `7ee84771` for `7ee8477c` made the range unresolvable; the assembler reported `PAYLOAD NOT
  VERIFIED … exited 128` and **fell back to the STATED count** — reverting to the behaviour it had
  before the measured unit existed. **Use full 40-char shas.**
- ⚠ **`--claims-file` leaves placeholders the brief's own commands then carry** — `<the PR's head
  sha>` ×2 and a repo-unknown spelling ×3, plus an EMPTY `WHERE TO WORK` section. Substitute all of
  them, assert zero remain, and verify the base-branch *assumption* against the PR.
- ⚠ **The `go` CI job runs ~49 min** (189-mutant authz battery, client ledgers, 57-mutant routing
  battery, conformance corpus) and shows as `pending 0`. Read `gh run view --job <id>` for per-step
  ticks; do not read a watcher timeout as a failure.
- ⚠ **A base move resets `mergeable` to `UNKNOWN`, and `UNKNOWN` is not `MERGEABLE`.** Poll until it
  is neither before believing either answer.
- ⚠ **Two API limit outages hit mid-run.** One agent died after one line (resume works — re-anchor
  it and re-verify the world first); one died *between* its commit+push and its report, so the work
  had landed while the report was lost. **Check the pushed ref before assuming work was lost.**
- ⚠ `/home/zach/workspace/cairn-hv2` was this session's handoff worktree; `cairn-handoff` is still
  the stale one on a closed arc. Remove hv2 when done.

- 🔴 **RETRACTED — "THE CANONICAL HANDOFF FOR THIS ARC IS NOT ON `main`" IS NOW FALSE, AND SO IS ITS
  INSTRUCTION.** The bullet below (and its `git log --all --diff-filter=A` / `git show <branch>:<path>`
  recipe) applied while PR #169 was open. **#169 merged as `ffede27`: the doc IS on `main`.** Read it
  at `claudedocs/handoff-cairn-tag-vocabulary.md` directly — do not run the branch-ref dance, and do
  not conclude from a failed `git show origin/docs/...` that the doc is missing; that branch is
  deleted. ⚠ Recorded as a retraction rather than a deletion because `Gotchas` is an APPEND bucket:
  the superseded text survives verbatim below and would otherwise be read as current.
- 🔴 **THE LADDER'S OWN LESSON, AND THE MOST REUSABLE THING THIS ARC PRODUCED: every fix round
  introduced the next finding, and the fix for a defect recreated that defect one token over.**
  Rank 2 ran five audit rounds on a PR whose CI was **8/8 green at every single step**. What the green
  suite never saw: a **tracked file deleted** in the base clone while the guard answered allow; a
  **regression a fix round itself shipped**; `git revert` writing the shared tree while the PR *pinned
  a claim* that the out-set was complete. 🔴 **Rounds 4 and 5 found NO behavioural defects** — by then
  the hook was AST-identical to its base modulo comments — **so the ladder was stopped on a STATED
  criterion rather than a clean round**, because the remaining class had reproduced in every single
  fix round and another round buys occurrence six, not convergence. Write the criterion down when you
  do that; an unexplained stop reads identically to a convergence.
- 🔴 **NINE INSTRUMENTS RETURNED A CONFIDENT WRONG ANSWER IN THIS ARC. EVERY ONE WAS CAUGHT BY A
  CONTROL AND NONE BY THE RESULT LOOKING WRONG.** An `awk` range whose end pattern matched its own
  start line (reported 0 for everything); a **case-SENSITIVE** sweep for a word written in capitals
  (0 hits against 1); importing the hook as a module, which runs `main()` and `sys.exit(0)`s —
  **exit 0, no output, indistinguishable from "zero findings"**; a `" 1 passed"` matcher anchored
  mid-line; a differential pointed at the wrong tree (383 false fail-opens); a `-h` arity reader whose
  attached-arg branch matched empty; a marker-grep over `git merge-tree`, which prints no markers; a
  sweep whose own control string was the operator's real checkout path — in a PUBLIC repo; and a
  mutation anchor that never applied. **Validate the instrument, report the pair, treat a reassuring
  zero as unproven.**
- ⚠ **An `audit-claims`/range sha TRUNCATED BY HAND silently disarms the attribution gate.**
  `7ee84771` for `7ee8477c` made the range unresolvable; the assembler said `PAYLOAD NOT VERIFIED …
  exited 128` and **fell back to the STATED count**, reverting to its pre-measurement behaviour. Use
  full 40-char shas. ⚠ `--claims-file` also leaves placeholders the brief's own commands then carry
  (`<the PR's head sha>` ×2, a repo-unknown spelling ×3, and an EMPTY `WHERE TO WORK` section):
  substitute all of them, assert zero remain, and verify the base-branch *assumption* against the PR.
- ⚠ **Two API limit outages hit mid-run, and they fail differently.** One agent died after one line —
  resume works, but re-anchor it and re-verify the world first. One died **between its commit+push and
  its report**, so the work had landed while the report was lost: **check the pushed ref before
  assuming anything was lost.**

- 🔴 **"MET" ON A CLOSING CONDITION IS A READING WITH A DATE, NOT A PROPERTY — AND THIS ARC IS THE
  WORKED EXAMPLE.** The condition was declared MET 2026-10-01 and was FALSE 24 hours later, because
  one half of it ranges over a growing population (every entry in the store) while the other is a
  code property (the write gate). **A condition with a population half re-opens on its own**; only
  the code half stays closed. When a closing condition quantifies over data, say which half decays
  and name the mechanism that feeds it — here, rank 4, which the doc had already written down as the
  predicted regression and which had already fired ten times before anyone re-ran the check.
- 🔴 **A ROUTES-BLIND SWEEP OVER "EVERY ENTRY" IS WRONG BY 13×, IN THE ALARMING DIRECTION.** Both
  instance caches hold directories for scopes the OTHER instance owns, so a flat walk of
  `*/*.md` across `~/.cache/subsystem-store` and `~/.cache/subsystem-store-civitai` reports **138**
  untagged entries where the routes-aware count is **10**. Resolve each scope through
  `~/.config/subsystem-store/routes.json` and keep only the copy on its own instance.
- ⚠ **`*/README.md` IS NOT AN ENTRY, AND THE READER IS THE ARBITER.** 12 scope-level READMEs have no
  front matter at all. The reader's own denominators exclude them (`cli` prints `M=14` where the
  filesystem holds 14 entries + 1 README), which is what settled it — do not count them as untagged.
- 🔴 **THE WRITE PRECONDITION IS CHECKED BEFORE THE SHAPE, SO A PROBE WITHOUT `If-Match` MEASURES THE
  WRONG THING.** `PUT /api/v1/entry/<scope>/<ref>` with no `If-Match` answers **428
  `precondition-required`** whatever the body's tag says. The revision is `sha256` of the entry file,
  first 16 hex chars — re-derive it at the moment of the probe, and read the ETag that comes back: if
  it EQUALS what you sent, the positive control was content-neutral.
- ⚠ **A `grep -c` OVER A COMMAND THAT REFUSED TO RUN IS A ZERO ABOUT NOTHING, AND IT HAPPENED THREE
  TIMES IN A ROW HERE.** `subsystem_touch.py --template` needs BOTH a slug and `--writer`; each
  missing one exits with a usage error, empty stdout, and `grep -c '^tags:'` = 0 — identical to the
  real finding. The control that caught it was `grep -c '^service:'` in the SAME output: a line the
  template definitely emits. **Never report a zero without a non-zero from the same invocation.**
- ⚠ **A PLURAL-ONLY REGEX SILENTLY ZEROED SIX SCOPES.** The reader prints `N of M entry` (singular)
  for a one-entry scope and `entries` otherwise; a sweep matching only `entries` scored those six as
  `M=0`. Found by cross-checking against a second instrument whose total disagreed by exactly 6 —
  **the disagreement was the finding**, not either number on its own.
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

## How to verify
**Everything merged, from the repo** (nothing is deployed, so none of this is edge evidence):
```bash
git -C /home/zach/workspace/cairn log --oneline -4   # ffede27, 306164c, 5c96ffd, fc2ddfe
go -C /home/zach/workspace/cairn test ./internal/ui/ -run TestTheQueryAndTheTagComposeIntoOneCard -v
nix-shell -p python312Packages.pytest python312 git --run \
  "python3 -m pytest tests/test_base_clone_write_guard.py -q"      # => 344 passed
python3 tests/leakscan.py && python3 tests/leakscan.py --self-test  # => both exit 0
```

**The guard's own hazard, end to end** (rank 2 — CARRIED FORWARD, not re-measured this session) — the
spellings that used to destroy things, with the dry runs that must still be ALLOWED as the control.
Run in a THROWAWAY clone carrying one linked worktree:
```
git rm -n --no-pathspec-from-file --no-dry-run seed.txt  => deny   (deleted a TRACKED file before)
git clean -n --exclude= --no-dry-run -f -d               => deny   (deleted files before)
git clean -f --exc -n                                    => deny   (prefix ate its value before)
git symbolic-ref -qd refs/heads/alias                    => deny   (deleted a ref before)
git clean --dry · git rm --dry · git merge --ff-o <ref>   => ALLOW  (real reads, refused before)
git commit -m x => deny  ·  git status => allow           (the pair that makes the above readable)
```
🔴 **A verdict is only half a claim about a destructive command** — run the real command in a copy and
report what moved. ⚠ `git merge --ff` must still DENY: it is a distinct option from `--ff-only` and
permits a merge commit.

**The closing condition, both halves, in the order that makes them readable.** Half 2 first — it is
cheap and it tells you the gate still exists before you interpret half 1's numbers.

```bash
# HALF 2 — the write gate, against the DEPLOYED pod. Negative THEN positive; the positive is not
# optional, and a run without If-Match answers 428 and measures the precondition instead.
set -a; . /home/zach/.config/subsystem-store/env; set +a
URL="${CAIRN_URL:?}"; TOK="${CAIRN_TOKEN:?}"
cp ~/.cache/subsystem-store/cairn/tag-vocabulary.md /tmp/good.md
sed 's/^tags: \[tooling\]$/tags: [zzz-not-a-real-tag]/' /tmp/good.md > /tmp/bad.md
diff <(grep '^tags:' /tmp/good.md) <(grep '^tags:' /tmp/bad.md) \
  || echo 'FIXTURE CONTROL: the two copies differ on the tags line'   # else the sed missed
REV=$(sha256sum /tmp/good.md | cut -c1-16)                            # re-derive NOW, not earlier
for f in bad good; do
  curl -sS -o /tmp/$f.body -D /tmp/$f.hdr -w "$f => HTTP %{http_code}\n" \
    -X PUT "$URL/api/v1/entry/cairn/tag-vocabulary" \
    -H "Authorization: Bearer $TOK" -H 'Content-Type: text/markdown' -H "If-Match: $REV" \
    --data-binary @/tmp/$f.md
  grep -i '^x-store-status\|^etag' /tmp/$f.hdr
done
#   bad  => HTTP 422   x-store-status: entry-shape   "... is not one of infra|product|tooling"
#   good => HTTP 200   x-store-status: replaced      etag: "f1040929673545e9"  == the If-Match sent,
#                                                     so the control changed nothing (confirm with cmp)
```

```bash
# HALF 1 — every entry on ITS RESOLVED INSTANCE. Ask the READER, scope by scope, and compare the sum
# of the three vocabulary terms against the denominator the reader itself prints.
#   POSITIVE CONTROL 1: an off-vocabulary tag must read `0 of <non-zero>`.
#   POSITIVE CONTROL 2: a ONE-entry scope must read a non-zero denominator — the reader says
#                       `of 1 entry` (SINGULAR), and a plural-only regex scores six scopes as empty.
cairn sync
cairn recall --scope cairn --tag zzz-not-a-real-tag --no-sync | grep -oE 'tag: .* carry it'
ONE=$(cairn ls-entries --no-sync | sed -E 's/^\[[^]]*\] //' | cut -d/ -f1 | sort | uniq -c \
      | awk '$1==1{print $2; exit}')          # any scope holding exactly ONE entry
cairn recall --scope "$ONE" --tag tooling --no-sync | grep -oE 'tag: .* carry it'
for s in $(python3 -c 'import json;print(" ".join(sorted(json.load(open("/home/zach/.config/subsystem-store/routes.json")))))'); do
  for t in infra product tooling; do
    cairn recall --scope "$s" --tag "$t" --no-sync 2>/dev/null \
      | grep -oE 'tag: `[^`]+` — [0-9]+ of [0-9]+ (entry|entries)'
  done
done
# => the three N must sum to M for EVERY scope. 9 of 29 fell short when this was written, gap 10.
```

```bash
# RANK 4's premise — does the template emit a tags: line? The `service:` count is the positive
# control; without it a usage error reads as a clean zero.
T=<tooling>/scripts/lib/subsystem_touch.py   # <tooling> = the private tooling checkout
for w in handoff analyze-service; do
  python3 "$T" --template probe-slug --writer "$w" \
    | awk '/^service:/{s++} /^tags:/{t++} END{printf "%s: service=%d tags=%d\n", "'"$w"'", s, t}'
done
# => both writers: service=1 tags=0   (service=0 means the command refused and you measured nothing)
```

**Rank 3, against the DEPLOYED edge — only after a pin bump.** In a signed-in browser open
`/?q=<a word in a tagged entry>&tag=<that tag>`: **ONE** card headed `Search`, its second line naming
both operands and both counts; typing a new word keeps the tag; emptying the box lands on the plain
tag listing. **Two cards means the pin did not move** — which is also the UI-rollout question,
answered by the same visit.
