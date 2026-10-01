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
- Branch: `main` on the mainline; **this doc's own branch `docs/handoff-cairn-tag-vocabulary`
  is UNMERGED (PR #169)**, so a session resuming from `main` cannot find this file. That is
  worth fixing before the next handoff: the canonical doc for the arc is not on the mainline.
- **Rank 1 is CLOSED and shipped to review: [PR #170](https://github.com/ZacxDev/cairn/pull/170),
  `feat/browse-q-and-tag-compose`** (`5e7646d` the feature, `9eacee0` the CHANGELOG row). ⚠ **NOT
  MERGED and NOT DEPLOYED** — at the time of writing CI had returned `leakscan`, `pgtest` and
  `parity` green with `tests`, `go`, `dualrun`, `nix` and `uiaudit` still running. Read the run
  before acting on this line; it is a reading, not a result.
- Five PRs merged and verified **by content** on `origin/main` (a squash never makes the branch
  head an ancestor, so ancestry is the wrong test):
  - **#164** — two private identifiers plus a private entry filename scrubbed out of this PUBLIC
    repo, and their digests added to `tests/leakscan.py` so recurrence is caught.
  - **#160** — the `tags:` vocabulary CLOSED at the WRITE path (`internal/write`).
  - **#162** — the rendered link badge and body label both read `refs`; a `## Requirements`
    bullet's provenance renders once (badge) instead of twice.
  - **#165**, **#167** — `.claude/hooks/base-clone-write-guard.py` now judges the write TARGET
    rather than the session cwd.
- **Store: 303 entries tagged** across both instances — `infra` 136 / `product` 99 / `tooling` 68.
  Zero off-vocabulary tags anywhere. ⚠ RECALL as of the previous session, not re-measured here.
- **Deployed and verified** (previous session): the deployment repo's pod and UI pins moved
  `e8839d9 -> ffa0eca` in one commit; the pod has rolled and both features are live at the edge.
  ⚠ The **UI** rollout is still NOT independently verified — Open investigations block 1.
- ⚠ **No task-board field is recorded on this doc, and that is a REFUSAL rather than a zero.**
  `<tooling>`'s task resolver exited **5** — nothing resolved. An unknown session id answers
  200 with an empty array, so this cannot distinguish "touched no task" from "wrong id".
  (The board's name is a denied identifier here; `tests/leakscan.py` refused an earlier draft
  of this very bullet that spelled it, which is the gate doing its job on a handoff delta —
  the exact path four past leak events took.)

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
🔴 **The numbering is UNCHANGED on purpose. Rank 1 stays in place as CLOSED rather than being
removed, because the rank is half a `claim-work` slug's identity — renumbering 2→1 would
re-point every live claim on this doc.** Closed items are struck here, never deleted.

1. ~~**Make `?q=` and `?tag=` compose in the browse surface.**~~ **CLOSED — PR #170, awaiting CI
   and merge.** One card, the composition the pod already performed (`report.SearchOptions.Tag`
   passed into the engine, so the narrowing lands after scope authorisation and before scoring),
   a summary naming both operands and both counts, a hidden `tag` input so the form round-trips
   the filter, and three ways back. `internal/ui/README.md`'s declared limitation is rewritten as
   closed. **What is left on this item is the MERGE, not the build.**
   forcing: user — the operator selected this option; it is built and under review.
2. **Close the three declared guard fail-opens.** All three are recorded with closing conditions
   in `.claude/hooks/base-clone-write-guard.py`'s docstring and all three were proven end to end
   by an audit: (a) `_shell_lines`' heredoc opener regex runs on the raw line, so ordinary text
   containing `<<WORD` — a quoted string, or even a `#` comment — silently drops every later
   line; (b) the program-name walk misses `if git commit`, `while`, `command`, `nohup`,
   `timeout`, `eval`, `stdbuf`, `exec`, `sudo`, `xargs`; (c) the `_REFUSED` complement is
   described as "reads" while `clean -fd`, `rm`, `mv`, `worktree remove` and `branch -D` all
   write shared state.
   forcing: security — each is a measured way to land a commit on the wrong branch in a shared
   clone, which is the single failure this guard exists to prevent.
3. **Confirm the UI pod rolled** (Open investigations block 1). One click path in a signed-in
   browser; everything else about the deploy is already verified. ⚠ **Once #170 merges and
   deploys there is a SECOND thing to look at on the same visit** — open `/?q=<word>&tag=<tag>`
   and confirm it renders ONE card rather than two.
   forcing: user — the operator asked for this deploy to be validated, and this is the one half
   that could not be closed from here.
4. **Teach the entry template to emit a `tags:` line.** `scripts/lib/subsystem_touch.py
   --template` emits none, so every entry born through the handoff flow starts untagged and
   invisible to `--tag`. That silently re-opens the gap this arc just closed, one entry at a
   time. Repo: the private tooling repo.
   forcing: regression — new entries reintroduce the untagged state this arc eliminated.
5. **Land this doc on the mainline.** PR #169 carries the only copy of this file; until it
   merges, `/resume` from `main` cannot find it and the next session re-derives the arc from
   `git log`. This session found it only by searching `--all`.
   forcing: gate — the resume path is the mechanism, and it is currently broken for this arc.

## Defects (batched)
- `.claude/hooks/base-clone-write-guard.py:702` still says the existence check lives "ahead of
  the probe budget" — #167 deleted that budget, so the sentence describes a mechanism that no
  longer exists.
- Same file: `_abs_path`'s claim that a second existence check "could never change a verdict"
  was measured FALSE when a NUL byte crashed the hook. #167 fixed the crash, which plausibly
  restores the claim — but it has not been re-measured since.
- The base clone of this repo carries a STAGED modification to the hook whose content is
  byte-identical to `origin/main`, while `HEAD` sits 4 commits behind. **Re-measured this
  session and STILL TRUE** (`git -C /home/zach/workspace/cairn status -s` → `M` staged;
  `rev-list --count HEAD..origin/main` → 4). Harmless (the running hook is the fixed one) but it
  makes `git status` misleading. Clearing it is `restore --staged --worktree` on that path, then
  an ff-only merge.
- **The pod's composed `?q=`+`?tag=` answer and the browse surface's are not compared against
  each other.** After #170 they share the ENGINE, which is strictly more than they shared
  before, but nothing sends the same two parameters to both and diffs the result. Declared in
  `internal/ui/README.md` rather than fixed.
- **`uiaudit` walks neither `/?tag=` nor the composed card**, which after #170 is a fifth card
  shape carrying three `note` links and a hidden form control. No axe pass has run over either.

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

## How to verify
Both halves of the ORIGINAL closing condition, plus rank 1's.

**The write gate — negative then positive** (the positive control is not optional: without it, a
refusal is indistinguishable from a broken write path):
```bash
# take any entry, set an off-vocabulary tag, and try to replace it
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
  | grep -c '🔗 [0-9]* ref'     # => 4
#   and the retired word must be gone:  grep -c '🔗 [0-9]* task'  => 0
```

**Tag coverage across the fleet** — read each scope on its RESOLVED instance; the two caches
replicate 12 scopes, so a naive walk double-counts:
```bash
<tooling>/scripts/cairn-ops/health.sh instances --scope <scope>   # which cache is authoritative
# expected totals: infra 136 / product 99 / tooling 68, and no fourth term
```

**Rank 1 — the composition, in the repo.** Three answers, not one: a composed page that is right
for the wrong reason is indistinguishable from either operand acting alone.
```bash
go -C <checkout> test ./internal/ui/ -run TestTheQueryAndTheTagComposeIntoOneCard -v
go -C <checkout> test ./internal/ui/ -run 'TestTheSearchFormRoundTripsTheTag|TestTheComposedCardOffersEveryWayBack'
#   => the three answers are three DIFFERENT sets: `?q=` alone names 3 entries,
#      `?tag=` alone names 2, together they name the 1 in both.
```

**Rank 1 — the composition, against the DEPLOYED UI** (only after #170 merges and the pin moves;
the test above is evidence about the checkout, never about the edge). In a signed-in browser open
`/?q=<a word in a tagged entry>&tag=<that tag>`:
- **ONE** card, headed `Search`, not two;
- its second line names both operands — "The tag `x` narrowed this search to N entries before the
  query ran, leaving out M visible entries that do not carry it";
- typing a new word in the box keeps the tag; emptying the box lands on the plain tag listing.

Two cards is the pre-change answer and means the UI pin did not move — which is also Open
investigations block 1's question, answered by the same visit.
