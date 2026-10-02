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
- Branch: `main` at **`306164c`**. Base clone clean and re-synced. **60 linked worktrees** registered.
- ✅ **RANK 1 CLOSED AND MERGED** — PR #170, squash **`5c96ffd`**, verified by content on `origin/main`
  (5 feature markers in `internal/ui/render.go`, `compose_test.go` present, both CHANGELOG rows).
  ⚠ It did NOT merge cleanly: **#168 landed under it mid-review and it went `CONFLICTING`**. Resolved
  by merging `origin/main` into the branch (not rebasing — review threads anchor to the commits),
  keeping BOTH CHANGELOG rows newest-first. Full 8/8 CI re-run on the MERGED tree before merging.
- ✅ **RANK 2 CLOSED AND MERGED** — PR #171, squash **`306164c`**, verified by content. All three
  declared fail-opens closed, plus `revert`/`update-index`/`read-tree`/`symbolic-ref` added to the
  ledger, the dry-run and `--help` exemptions consolidated, and git's option GRAMMAR modelled once
  in `_OPTION_GRAMMAR` rather than matched as spellings at three sites.
- ⚠ **NOT DEPLOYED. Nothing from either PR is at the edge.** Rank 3 needs a deployment-repo image-pin
  bump first, which is an operator decision (a bump carries every commit merged since).
- ⚠ **No task-board field, and that is a REFUSAL rather than a zero** — the resolver exited 5.
  🔴 Naming the board is a `denied-identifier`; the leak gate fired on exactly that in this arc's
  first handoff delta, so the field's own spelling cannot appear in this repo.

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
🔴 **Numbering UNCHANGED — the rank is half a `claim-work` slug's identity.** Closed items struck.

1. ~~**Make `?q=` and `?tag=` compose in the browse surface.**~~ **CLOSED, MERGED `5c96ffd` (#170).**
   forcing: gate — it is merged; nothing remains.
2. ~~**Close the three declared guard fail-opens.**~~ **CLOSED, MERGED `306164c` (#171)**, after a
   full audit ladder (round 0 + rounds 1–5). See Gotchas for what the ladder caught.
   forcing: security — closed.
3. **Confirm the deploy, in ONE signed-in browser visit.** Two things on the same visit: the Refs
   panel heading (`refs` ⇒ the UI pod rolled; `tasks` ⇒ pod and UI are out of step), and
   `/?q=<word>&tag=<tag>` rendering **ONE** card headed `Search` rather than two.
   🔴 **BLOCKED ON A PIN BUMP THAT IS THE OPERATOR'S CALL** — neither #170 nor #171 is at the edge,
   and a bump carries every commit merged since (the last one carried 19). The browser bridge was
   measured disconnected (`connected: 0`) and is not re-measured here.
   forcing: user — the operator asked for this deploy to be validated.
4. **Teach the entry template to emit a `tags:` line.** `scripts/lib/subsystem_touch.py --template`
   emits none, so every entry born through the handoff flow starts untagged and invisible to
   `--tag`. Repo: the private tooling repo.
   forcing: regression — new entries reintroduce the untagged state this arc eliminated.
5. ~~**Land this doc on the mainline.**~~ **CLOSED by this update** — PR #169 merged carrying it.
   forcing: gate — closed.
6. **Replace `ci.yml`'s hand-edited collected-test `FLOOR` with a baseline derived at the merge
   base.** Found by audit round 0 and unfixed on purpose (out of #171's scope). The literal has
   been moved by **22+ commits**; the step is ~382 comment lines guarding one integer; its own
   comment says no bug ever narrowed this suite; it MISSED the one real deletion (the name-diff
   recipe caught that); and it has gone **RED with zero failing tests four times**, each time
   costing a round. ⚠ Every round of #171 moved it again (2458→2495→2529→2566→2588→2609→2617).
   **Closing condition:** the literal is gone, the baseline is collected at `git merge-base
   origin/main HEAD` in the same job, the removals-must-be-renames check still runs, and one PR
   that adds tests goes green without touching `ci.yml`.
   forcing: gate — a permanently-red-prone gate trains everyone to click through, which
   `claude/RULES.md` names as worse than no gate.

## Defects (batched)
🔴 **This heading REPLACES on every update — everything still open must be re-listed or it is
deleted.** That is how the list is maintained, not a sign the earlier text was wrong.

- ⚠ **STILL OPEN: the pod's composed `?q=`+`?tag=` answer and the browse surface's are not compared
  against each other.** After #170 they share the ENGINE, but nothing sends the same two parameters
  to both and diffs the result. Declared in `internal/ui/README.md` rather than fixed.
- ⚠ **STILL OPEN: `uiaudit` walks neither `/?tag=` nor the composed card** — a fifth card shape
  carrying three `note` links and a hidden form control. No axe pass has run over either.
- ⚠ **DECLARED LIMIT, not a defect: a comment claiming "this test reports/prints X" is NOT
  machine-checked.** The *existence* half is (`test_EVERY_TEST_A_PAYLOAD_COMMENT_NAMES_STILL_EXISTS`,
  shipped in #171). The *print/report* half was built, measured as a spelled guard firing on 7
  legitimate sites out of 8 (`git worktree list` "reports REGISTRATIONS", a ledger "read there" in
  an assertion, `symbolic-ref` "prints the ref"), and **deleted rather than narrowed to
  self-satisfaction**. The imperative is written at the site.
- ✅ CLOSED: the base clone's staged orphan on the hook (was byte-identical to `origin/main`;
  cleared content-neutrally). ✅ CLOSED: both `_abs_path` comment defects — and the retraction is
  recorded rather than the claim restored, because #167 closed the NUL route but restoring the
  check flips `GIT_INDEX_FILE=<clone>/.git/index git add` from deny to ALLOW.

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

## How to verify
**Both merged features, from the repo** (neither is deployed):
```bash
git -C /home/zach/workspace/cairn log --oneline -3   # 306164c (#171), 5c96ffd (#170)
go -C /home/zach/workspace/cairn test ./internal/ui/ -run TestTheQueryAndTheTagComposeIntoOneCard -v
nix-shell -p python312Packages.pytest python312 git --run \
  "python3 -m pytest tests/test_base_clone_write_guard.py -q"     # => 344 passed
python3 tests/leakscan.py && python3 tests/leakscan.py --self-test # => both exit 0
```

**The guard's own hazard, end to end** — the three that used to destroy things, with the dry runs
that must still be allowed as the control (run in a THROWAWAY clone with one linked worktree):
```
git rm -n --no-pathspec-from-file --no-dry-run seed.txt   => deny   (deleted a TRACKED file before)
git clean -n --exclude= --no-dry-run -f -d                => deny   (deleted files before)
git symbolic-ref -qd refs/heads/alias                     => deny   (deleted a ref before)
git clean --dry / git rm --dry / git merge --ff-o <ref>    => ALLOW  (real reads, refused before)
git commit -m x  => deny   ·   git status => allow         (the pair that makes the above readable)
```

**The write gate — negative then positive** (the positive control is not optional):
```bash
<tooling>/scripts/cairn-ops/write.sh put --scope cairn --ref tag-vocabulary --file <bad copy> --no-verify
#   => 🔴 REFUSED [entry-shape] — ... is not one of infra|product|tooling
<tooling>/scripts/cairn-ops/write.sh put --scope cairn --ref tag-vocabulary --file <good copy> --no-verify
#   => cairn: replaced instance=... revision=...
```

**Rank 3, against the DEPLOYED edge — only after a pin bump.** In a signed-in browser open
`/?q=<a word in a tagged entry>&tag=<that tag>`: **ONE** card headed `Search`, its second line
naming both operands and both counts; typing a new word keeps the tag; emptying the box lands on
the plain tag listing. **Two cards means the pin did not move** — which is also the UI-rollout
question, answered by the same visit.
