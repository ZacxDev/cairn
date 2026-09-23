# Handoff: cairn-control-plane — 2026-09-22

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
Turn cairn from a store with a static allowlist into a multi-user product: per-scope
sharing with users and projects, project membership and invites, identity via Supabase
(GitHub/Google) or a fronting auth proxy, and a PWA — on a foundation where **one
renderer** serves pod, CLI and UI. Plan: `claudedocs/plan-cairn-control-plane.md` (#16).

- **closing-condition:** `check` — on `main`: a green principal × scope × verb authz
  matrix over projects+grants; the PWA's share flow serving a scope granted from one
  user to another, with its replica-honesty notice pinned by a test; identity resolving
  through both backends; and `packages.default` the Go client. A later session runs
  `pytest tests -q`, `go test ./...` and reads `flake.nix`. ADDRESSED ⇒ arc CLOSED.

## State now

- `main` @ **`a88f60b`** (#76 merged). This session's work landed in the **handoff-tooling
  repository**, not here: **`c4490f07`** (its #1854).
- ✅ **RANK 8 IS DONE, AND THE OPEN-PR SWEEP IS WHAT DID THE JOB.** `claim-work --list` showed
  `cairn-control-plane-8` free, and the ranked item read as unbuilt — but
  `gh pr list --state open` carried **#1854**, *"run the target repo's leak scanner on the
  handoff delta, and refuse on non-zero"*, three commits, `MERGEABLE`/`CLEAN`, opened twelve
  hours earlier and **claimed by nobody**. 🔴 **THE SWEEP IS NOT A FALLBACK TO THE LOCK — IT
  IS THE ONLY THING THAT SEES AN UNCLAIMED DUPLICATE**, and this is the first measured
  instance of it paying: taking the claim and building would have written the feature twice.
- ✅ **RULE (o) IS MERGED AS `c4490f07`, VERIFIED BY CONTENT** (payload paths diff empty
  between the PR head `977a7a99` and `origin/main`), and the base clone was fast-forwarded.
  Exit **13** `leak-refused`; scanner resolved from the TARGET repo out of a closed set of
  one path (`<target-repo>/tests/leakscan.py`); a repo with none PASSES as `PASS BY ABSENCE`;
  **any** non-zero exit refuses; `--leak-pre-existing-approved` is the recorded operator
  opt-in and does NOT cover a scanner that could not be RUN.
- 🔴 **THE GATE IS LIVE; ITS PRINTED LEGEND IS NOT.** `SKILL.md` invokes
  the handoff tool (`<handoff-tooling-repo>/scripts/lib/handoff_doc.py`) — the WORKING COPY — so the refusal was active
  the moment the base clone synced. `~/.claude/skills/handoff/SKILL.md` resolves into
  `/nix/store` (a `home.file` copy), so its `Seven refusals` legend needs a home-manager
  switch. **Measured, not inferred:** `readlink -f` on the live file, and `grep -c leakscan`
  on it = **0**. No switch was run — operator's call.
- ⏳ **RANK 9 IS UP AND WAITING ON A HUMAN, AND `cairn-control-plane-9` IS DELIBERATELY STILL
  HELD.** `cairn-ui` is serving the share flow on **`127.0.0.1:8103`** over a synthetic world
  in this session's scratchpad. Releasing the claim while a server is up and a human is
  mid-verification would advertise the item as free and invite a second instance onto the
  same port. Recipe and what was verified: `## How to verify`.
- 🔴 **`leakscan` ON THIS BASE CLONE EXITS 2 RIGHT NOW, AND IT IS NOT THE TREE.** A LIVE,
  `locked` sibling agent worktree sits at `.claude/worktrees/agent-a2b34fb066b3e2ebc/`
  (pid 355708), and the scanner's `--others` enumeration cannot read a directory:
  `COULD NOT READ .claude/worktrees/…/: [Errno 21] Is a directory`. ⚠ **The documented
  gotcha names a REMOVED agent's leftover; this one is a live worktree, so the remedy
  "check it is clean, then remove it" does not apply** — it is not mine to remove. Discriminated
  rather than assumed: a fresh worktree of `origin/main` scans **rc 0**, so the tree is clean
  and the directory is the whole cause. **No clean-scan claim is made for the base clone.**
- ⚠ **THE `result`/`result-1` SYMLINKS IN THE REPO ROOT ARE MINE AND ARE NOT GITIGNORED**
  (`git check-ignore` prints nothing for either). They are the gcroots for the running
  `cairn-ui`, kept until the human is done. leakscan reads them without complaint, so they are
  NOT the exit-2 cause — measured, not assumed.
- **No external-task-board field**: the resolver exited **5**, "0 tasks for this session". An unknown
  session id answers 200 with an empty array, so that zero cannot distinguish "touched no
  task" from "wrong id". No field was written.
- 📇 **THE ARC'S LANDED SHAs, CARRIED FORWARD BECAUSE `State now` REPLACES AND THIS UPDATE'S
  DURABLE-DROP WARNING NAMED THEM.** #64 → **`7d7c9ea`** (the share flow) · #72 → **`6db7179`**
  (the changelog row, which could not exist inside #64 because the file requires a MERGE SHA) ·
  #71 → **`1659663`** (the handoff) · #73 → **`f8a257e`** (the prune) · #74 → **`93d0f03`** ·
  #75 → `a41594c` · #78 → `8cd5108` · #69 → **`56cc56e`** (the rename) · #79 → `58dda11` ·
  #76 → **`a88f60b`**. All ten re-resolved with `git log -1 --format=%s <sha>` while writing
  this line rather than copied forward on faith. Each was verified
  by CONTENT rather than ancestry at the time it landed. Kept as a one-line ledger because a
  squash sha is not re-derivable from the branch and every one of these rows was a separate
  `--is-ancestor` false positive waiting to happen.

## Next steps (ranked)

🔴 **THE NUMBERING IS STABLE AND RANK 9 IS NEW AT THE END.** Rank is half a claim's slug, so a
shuffle re-points every live claim; ranks 1–8 keep their numbers and the DONE rows stay as
rows. **A session taking an item must `claim-work` it first and read the SUBJECT the claim
prints rather than trusting the number** — and must ALSO sweep `gh pr list --state open`,
because this session measured the sweep catching an unclaimed duplicate the lock structurally
cannot see.

1. ✅ **DONE — #74 merged as `93d0f03`.** Kept as a numbered row rather than deleted, because
   deleting it renumbers every item below and re-points every live `claim-work` slug.
   forcing: gate — the entries' own closing conditions, which named a PR.
2. **The remaining batched defects.** Still open: #48's (c) `lib/README.md` (the re-count recipe
   greps two literal phrases, so it is a SPELLED check that cannot see a reworded echo); the
   stale node-affinity comment, which lives in the **deployment-manifest repository** rather
   than this one — 🔴 that repo's NAME is a denied identifier in THIS tree, so take it from the
   operator rather than from a doc here; and the carried-forward list.
   forcing: gate — filed BY attribution gates rather than fixed, so nothing else surfaces them.
3. **P7 — conditional snapshot sync.** `/api/v1/snapshot` ships a full tar with no ETag/304.
   Key it on principal + epoch; `control.Authorization` already carries `Epoch`.
   forcing: none
4. **P8 — retire the Python oracle.** Gated on the default flip holding over real use, which
   is a waiting period rather than a task.
   forcing: none
5. ✅ **DONE — `cairn-server -issue-credential` over `control.IssueCredential`, in #76.**
   ⚠ **`EventCredentialRevoked` still has no writer** — and this session measured that the gap
   is SIX events wide, not one; see `## Defects (batched)`.
   forcing: gate — a shipped binary refused to start and named this as the missing piece.
6. **Fold the new defect entries in and mark #60/#66 closed**, once the doc PRs have merged.
   forcing: user — the operator chose this sequencing explicitly.
7. 🔴 **RENAME `SUBSYSTEM_STORE_*` → `CAIRN_*` BEHIND A DEPRECATION WINDOW — ⚠ LANDED as
   `56cc56e` (#69), so this row is now a DONE row rather than in-flight.** Both names work; the
   old one warns once per process naming its replacement; the removal version is stated. ⚠ The
   repo is PUBLIC and consumers pin the flake, so the warning text and the removal version are
   a PUBLISHED CONTRACT.
   forcing: user — item 5 of the five approved with "proceed as recommended".
8. ✅ **DONE — rule (o) merged as `c4490f07` in the handoff-tooling repo (its #1854).** The
   closing condition was *"a delta carrying a known-denied identifier is refused by the tool,
   watched"*, and it was watched on the **MERGED** tree rather than the PR branch, because
   `main` had moved two commits ahead. Four arms, all against cairn's REAL
   `tests/leakscan.py` in a detached throwaway worktree: the canary delta → **13** with the
   scanner's own `[denied-identifier]` line reproduced and the tree left `--porcelain` empty;
   a clean delta → **0** `status=written` (the control that stops "refuses everything" reading
   as "works"); an already-red tree with a CLEAN delta → **13** without the flag and **0**
   with it, printing `LEAK GATE APPROVED THROUGH`. `cairn-control-plane-8` released.
   forcing: incident — FOUR leak events, one of which reached `main` (#68).
9. ⏳ **IN FLIGHT: drive the share flow's human verification on `127.0.0.1:8103`.** The surface
   is up over a synthetic world and the read half plus the write half are both measured (see
   `## How to verify`); what is left is a human exercising the share/revoke click path, which
   is the half no curl proves. 🔴 **`cairn-control-plane-9` IS STILL HELD ON PURPOSE** — a
   server is listening and a release would invite a second instance onto the same port. Release
   it and stop the process when the human is done.
   forcing: user — the operator directed this item and reserved the browser step to a human.

## Defects (batched)
- ⚠ **CLOSED ENTRIES MOVE TO THE ARCHIVE, THEY DO NOT ACCUMULATE HERE.** This section
  REPLACES, so every closed entry left in it is retyped by hand each round until somebody
  drops it — which is how one sat 🔴 and 46,831 B wrong for two merges. The LESSONS from a
  closed entry belong under `Gotchas`, which appends; the entry itself belongs in the archive.
  ⚠ **#74's ✅ entry was DROPPED by this update rather than retyped** — its lessons are already
  under `Gotchas` in their own bullets, which is the condition for dropping one.
- 🔴 **SIX OF ELEVEN JOURNAL EVENT KINDS HAVE NO WRITER, AND THAT IS WIDER THAN THE ENTRY IT
  GENERALISES.** Rank 5 recorded `EventCredentialRevoked` as the one gap. Measured against
  `control.AllEventKinds` this session: only `user-created`, `project-created`,
  `scope-created` (all three via `-create-user`) and `credential-issued` have a command behind
  them. **`member-set`, `member-removed`, `scope-renamed`, `scope-moved`, `granted`,
  `grant-revoked` and `credential-revoked` are reachable only by HAND-APPENDING JSON** — and
  `granted`/`grant-revoked` at least have the browser surface as a writer, which the other five
  do not. 🔴 **`member-set` is the one that bit**: the share flow's candidate list narrows to
  co-members, `-create-user` creates a NEW project per invocation even for an identical
  `-project` name, so there is no supported path to the two-users-one-project state the flow
  needs. ⚠ Hand-appending is the exact shape that let a 64-character secret into the journal
  (rank 5) — an operator told to hand-write a record is an operator guessing at a schema.
  **Closing condition:** a `-set-member` command, or a written line saying hand-append is the
  intended interface and naming where its schema is documented.
- 🔴 **`leakscan` EXITS 2 ON A DIRECTORY IN ITS OWN ENUMERATION, AND RULE (o) HAS JUST MADE
  THAT CONSEQUENTIAL.** `git ls-files --others` yields `.claude/worktrees/agent-…/` and the
  scanner tries to read it: `COULD NOT READ … [Errno 21] Is a directory` → exit 2, "could not
  vouch". Any agent worktree makes the base clone unvouchable. ⚠ **Until today that was
  cosmetic; now `handoff_doc.py` refuses a handoff write on ANY non-zero exit**, so every
  handoff into this repo while an agent worktree exists lands on `leak-refused` 13 and has to
  be cleared with `--leak-pre-existing-approved` — an operator flag being spent on a
  scanner artefact rather than on a real finding, which is how an override becomes reflexive.
  **Closing condition:** the scanner skips directory entries (or names them as skipped rather
  than unreadable), with a control proving a genuinely unreadable FILE still exits 2.
- 🟡 **FOUR FILED BY #54's AND #57's LADDERS — AND (a) AND (b) WERE RE-MEASURED AND ARE WRONG AS
  FILED.** 🔴 **(a) IS CLOSED AND ITS ENTRY IS INVERTED.** `internal/ui/README.md` carries no
  "takes two backends" sentence at all — it records the removal explicitly, saying the sentence
  *"deliberately carries NO parameter COUNT"* — and `AuthBackends` now takes **two** parameters,
  landed by #58. Acting on this entry means hunting a sentence that is gone. 🔴 **(b) NAMES THE
  WRONG FILE.** `internal/control/tokenfile/source.go` was fixed by `c47636b`; the LIVE stale
  copy is **`internal/control/filestore.go:54`** — *"This module has no `require` block and
  `flake.nix` passes `vendorHash = null`"* — both halves false since #55. (c)
  `internal/report/testdata/reader_fixtures.json` contains **no `[cairn: …]` trailer at all**
  (0 hits against 8 in `server.py` as a positive control), so the differential reader fixture
  never exercises attribution rendering. (d) Whether `cairn-ui` should ever render through
  `internal/report` is **undecided and stated as open in `README.md`**. **Closing condition:**
  one PR for (b)–(c); a written line for (d).
- 🟡 **`apps.cairn` WAS UNPINNED UNTIL #50's FIX ROUND, AND THE NEAR-MISS IS THE RECORD WORTH
  KEEPING.** Repointing it at the Go client left **all three** of the guard's original assertions
  green while the announced hatch silently became the Go client — a guard written for attribute A
  not covering sibling attribute B, defeating a promise the same PR created. Closed by a fourth
  assertion; kept because the lesson generalises.
- 🟡 **`checks.default-is-the-go-client` IS INSENSITIVE ON THE PYTHON SIDE.** `mkCairn`'s pname is
  already `cairn`, so a `meta.mainProgram` removed *there* leaves the base-name assertions green.
  **Closing condition:** a decision to close it or a written line saying why not.
- 🟡 **COUNTS QUOTED IN PROSE THAT NOTHING ASSERTS ON** — `lib/README.md`'s echo-site count,
  `tests/test_parity_harness.py`'s floor. ⚠ **`README.md`'s 101/102/70/23/8 are GONE** — #54 deleted
  every count from that file rather than refreshing them. The repo owns the fix pattern
  (`tests/test_control_mutant_count_is_pinned.py`); applying it to what remains is separate work.
  **Closing condition:** a decision to pin each or a written line saying why not.
- 🟡 **THE DEPLOYMENT MANIFEST'S NODE-AFFINITY COMMENT IS STALE.** It keeps the pod off the off-LAN
  burst node *because the LAN registry does not resolve there*; the pod now pulls from ghcr, so that
  reason is void while the affinity may still be wanted (the PVC is ReadWriteOnce local-path).
  **A comment is a claim too. Closing condition:** the comment states the true reason, or it goes.
- 🟡 **`tests/dualrun/` cannot see image drift, structurally.** It runs the TREE's `server.py`;
  nothing compares the Go server against the artefact actually serving. **Closing condition:** decide
  whether a deployed-artefact arm is worth owning, or write the line saying it is not.
- Everything previously listed stands unchanged: #38's three residuals; `ScopeByNameIn`
  raw-vs-folded; P4 round 5's two prose defects; the degenerate-spelling limb; PR #15's six findings;
  the four deferred Go/oracle divergences; `server/seed.sh:110`'s `cd`; three files not `gofmt`-clean
  with nothing in CI grepping it; `-race` gated in one tier only; and P4 rounds 1 and 3's guards
  absent from the persistent battery.

## Gotchas / decisions / dead-ends
🔴 **THIS SECTION WAS PRUNED, AND PRUNED MEANS *MOVED*: THE PRUNE DELETED AND SHORTENED NOTHING.** ⚠ One of the 49 survivors has since been superseded by a fuller instance in this same section and removed, so a re-run of the prune's own verification now reconciles to 173 rather than 174 — the CLAIM survived, the bullet did not, and those are different statements.** It held 174 bullets and 89,588 B — 78% of a document read first thing every session. 125 of them are now in `claudedocs/handoff-cairn-control-plane-archive.md`, **verbatim**, under a heading that says which arc they came from. What stayed is what binds a NEXT edit: one instance of each general tripwire, the standing operator decisions, and the current arc's record. What moved is a record of a round or an arc that has CLOSED, plus every duplicate instance of a tripwire kept here — `isolation: "worktree"` alone was recorded six times, `$?`-after-a-pipe three, MERGEABLE-but-conflicting four.

⚠ **A DUPLICATE IS NOT A REDUNDANCY WHEN THE SECOND ONE RECORDS THAT THE LESSON WAS READ AND THEN HIT ANYWAY** — that is why the instance kept is usually the LATEST, which carries the re-occurrence, rather than the first, which carries only the discovery.

- **Never run two `uv run --with pytest` invocations concurrently** — one disposes of the
  other's ephemeral venv and produces a bogus `FileNotFoundError` red.
- **The flake's source filter is git-based**: an untracked new `.go` file compiles under
  `go build` and fails `nix build`. Stage new files before reading either tier as green.
- **`AGENTS.md` + `CLAUDE.md` are gated** to 37,700 B with a 900 B minimum headroom
  (`tests/test_agent_instructions_weight.py`); currently 36,354 B. Put narrative in a
  harness README behind a pointer — that is the pattern the gate's own playbook prints.
- **Decision (operator, this session):** an unencodable seed stamp resolves to
  `seeded=UNREADABLE` on **both** sides — an authorised exception to P1's "do not change
  the oracle", because a contract cannot include "sometimes truncate the response
  mid-stream". Regenerating all 98 goldens after it moved **none**.
- **Decision (operator, this session): a project principal has NO authority over its own
  project's scopes.** A project is not a member of itself, so a service account for a
  project reaches that project's scopes only if somebody granted it. The wide alternative
  — "a project credential implicitly holds what the project owns" — is a rule that never
  appears in the grant log, and "who could see this, and when" is the whole reason the log
  exists. The same behaviour is available VISIBLY, via an explicit self-grant at project
  creation; that is a policy decision for whoever wires this up, recorded in
  `internal/control/README.md` because the default will otherwise read as an oversight to
  the first person who creates a CI credential and finds it cannot write.
- 🔴 **`isolation: "worktree"` BRANCHES FROM THE DEFAULT BRANCH, NOT YOUR CHECKED-OUT ONE.**
  Measured twice: an agent dispatched to build on an unmerged branch got a worktree of
  `main` and correctly refused rather than recreating the missing package. **Always give a
  dispatched agent a base check it can fail** ("`ls <path>`; if missing, STOP"), and the
  recovery command.
- 🔴 **THE MUTATION BATTERY'S OWN ROWS ARE AS WRONG-ABLE AS THE CODE, AND FOUR WERE.** Across
  three rounds: a row naming a killer that NEVER RUNS (the named test reached the function by
  a different path); two patterns that failed to apply so the mutant died at the BUILD rather
  than at a guard; and an `EQUIVALENT` label whose stated reason — "a window between two
  adjacent statements no gate can open from outside" — was FALSE, because a caller-injected
  `clock()` sat between them. That last one is the worst shape: a label that reads as
  coverage while providing none, forecloses the test that would close the gap, and sits
  inside the battery built to refuse exactly that.
- 🔴 **AN `until`-LOOP CI WATCHER THAT COUNTS *INCOMPLETE* CHECKS REPORTS GREEN ON AN
  EMPTY ROLLUP.** Zero incomplete checks is also what "no checks exist yet" looks like —
  the state right after a push while GitHub clears the rollup. Measured: it returned
  `checks: []`, `mergeable: UNKNOWN`, **exit 0**, and I reported that as green. **Require
  a minimum check COUNT as well as completion, and print the count** so the reading
  carries its own proof it measured something.
- 🔴 **`$?` AFTER A PIPE IS THE LAST COMMAND'S STATUS.** `python3 tests/leakscan.py | tail`
  returns `tail`'s 0 for a scanner that exited **2**. Combined with the worktree defect
  above — failure on stderr, stdout ending in a wall of `PASS` — a piped read looks clean.
  Capture the rc before any pipe, and read stderr.
- 🔴 **`pgrep -f` MATCHED MY OWN SHELL WHILE I WAS SWEEPING FOR LEAKED PROCESSES — the exact
  documented trap, live.** A sweep for `cairn-server.test|control_mutants` returned one hit,
  and the hit was the sweeping command's own `zsh -c` line quoting the pattern. Read as a
  leak it would have sent me hunting a process that did not exist; read as a hit to `kill` it
  would have killed the shell. **Resolve PIDs, skip `$$`, and confirm each via
  `/proc/<pid>/cmdline` and `/proc/<pid>/cwd` before believing OR killing anything.** The
  real answer was zero.
- 🔴 **BASE-CLONE DRIFT HAPPENS ON FEATURE BRANCHES TOO, NOT JUST `main`.** The local
  `feat/identity-interface` was a stale leftover at `58ee284`, **four commits behind**
  `origin`, checked out in no worktree, while the PR head was `7ac810e`. A `worktree add` on
  it silently produces a tree missing the last four commits — including the ceiling raise —
  and every gate run there measures the wrong tree. `git merge --ff-only` is the safe sync
  precisely because it cannot conflict or autostash: it advanced (nothing was ahead), and had
  the branch diverged it would have REFUSED rather than guessed.
- 🔴 **A MUTANT THAT FAILS TO APPLY REPORTS A FALSE `SURVIVED`, AND IT HAPPENED TWICE HERE —
  ONCE ON THE MUTATION PROVING THE GATE.** Both were caught only by asserting the anchor's
  occurrence count BEFORE editing. **Assert the anchor count every time**; a "survived" you
  did not watch apply is not evidence.
- 🔴 **ASKING "WHICH GUARD DOES YOUR OWN CHANGE EMPTY?" *INSIDE* THE FIX ROUND FINALLY CAUGHT
  IT IN-ROUND — the sixth instance, and the first not discovered a round later.** The shape,
  six times across two PRs: a change moves a test's OBSERVABLE out from under an UNEDITED
  guard, and a green suite certifies the defect. Instances: a deletion made `Model` re-read the
  journal so `TestARejectedBatchLeavesNeitherBytesNorState` answered correctly regardless (and
  emptied `append-validates-against-the-live-cache` + `clone-is-shallow`); a new refusal's
  PRECEDENCE made all 15 ledger variables refuse via the new sentinel (`15/15 shadowed` vs
  `0/15` with an authority); a `cache.go` edit invalidated FOUR mutant anchors at once; and a
  within-request fold SUBSUMED the raw rule, leaving `apply`'s `ScopeByNameIn` check with zero
  tests. **Put the question in every fix brief.**
- 🔴 **VERIFY A SQUASH MERGE BY CONTENT, NEVER BY ANCESTRY — carried here from `State now`,
  which is a REPLACE section, so it would otherwise have been deleted by this very update.**
  `git merge-base --is-ancestor <branch-head> origin/main` returns **false** after every
  squash merge, forever, and reads as "not merged — redo the work". Measured on #35:
  ancestry false, while `git diff e708cba origin/main -- internal/identity/` was **empty** and
  `main` carried the new symbol. ⚠ **A WHOLE-TREE diff is NOT that check** — it showed 4 files
  / 930 insertions, which were another PR's files the branch never had. Diff the PAYLOAD
  PATHS, and separately confirm the merge commit exists.
- 🔴 **"EXECUTABLE" IS NOT "PAYLOAD", AND I GOT IT WRONG ON BOTH PRs BEFORE CORRECTING IT.** The
  attribution gate counts the payload lines a round's FIXES change, and the tie-breaker is the
  **REVERT TEST**: revert this file's diff — does the PR's stated deliverable still ship? On #38
  I posted `payload=13` because the change sat in a source file; it was a doc comment, and
  reverting it still ships `ProvisionUser`. On #41 a fix round reported `102` for changes
  confined to two guard modules; revert them and `server-image-go` still ships. **Both are 0.**
  Measure it — strip comments and blanks and compare — and run a **positive control** proving the
  method detects a real change. 🔴 **Correct such a figure IN PUBLIC on the PR:** a class that
  moves between rounds makes the stop unfalsifiable, and the only thing separating a correction
  from a manipulation is that it is stated, with its reasoning, where the next reader sees it.
- 🔴 **BOTH LADDERS ENDED ON THE ATTRIBUTION GATE, NOT ON A CLEAN ROUND, AND THAT IS THE DESIGNED
  OUTCOME.** Every round found something real, so the findings-keyed rule would have run forever.
  What the later rounds found were defects in **scaffolding the ladder itself had just written** —
  a false sentence inside a retraction, a count introduced by the very commit that deleted a count
  for being unpinned. Two consecutive rounds whose fixes change zero payload lines means the
  ladder has left the PR. **File the remainder with a closing condition rather than fixing it**,
  so it reads as open rather than absent.
- 🔴 **A GUARD SPELLED RATHER THAN STRUCTURAL PRODUCED A FOURTH SPELLING IN ONE MODULE, AND THE
  FIX WAS TO STOP PARSING.** #41's `Env` guard read keys out of the `//` override with the regex
  `[A-Za-z_][\w'-]*\s*=(?!=)`. Four spellings walked past it, each shipping
  `SUBSYSTEM_STORE_ROOT=/wrong` to the pod with every assertion green: `inherit` (no `=`),
  `${"NAME"} =` (no bare identifier), a mapper lambda, and `++ [ "…=/wrong" ]` appended to the
  resulting **list**, which the override reader never looks at. That last one has teeth — the pod
  then carries two `SUBSYSTEM_STORE_ROOT` entries and Go's env map takes the later, so the server
  starts, health-checks and serves an empty store. **Closed by pinning the WHOLE NORMALISED `Env`
  expression** for both images and DELETING the key parsing — 125 insertions against 430
  deletions. Teaching the parser two more spellings is how a fifth arrives. Cost accepted: a
  cosmetic reformat of either `Env` block now fails the test, which is what buys a
  machine-readable claim instead of a walkable one.
- 🔴 **A RANK SHUFFLE SILENTLY RE-POINTS EVERY LIVE CLAIM, BECAUSE THE RANK IS HALF THE SLUG.**
  `cairn-control-plane-1` was held with a subject describing the P5 slice after the cutover was
  promoted to rank 1, so `claim-work --slug-for <doc> 1` returned a ref naming different work, and
  `rc 12` ("already yours, carry on") would have let a session continue with the wrong item.
  **When you re-rank, re-subject the claims in the same breath.**
- ⚠ **A worktree created with `git worktree add -b <branch> origin/main` has upstream
  `origin/main`, so a bare `git push` there TARGETS MAIN.** Mine was refused only because
  `push.default=simple` requires the names to match — luck, not design. Push with an explicit
  refspec (`git push origin <branch>:<branch>`) and fix the upstream immediately.
- ⚠ **`xargs -0 command grep` FAILS** (`command` is a builtin xargs cannot exec) with 127 and
  empty output — indistinguishable from a clean zero. Use plain `grep` under `xargs`: it execs the
  binary, so this host's `.gitignore`-honouring `grep` FUNCTION never applies.
- 🔴 **A PRUNED-TO ARCHIVE IS NOT A HANDOFF DOC, AND THE WRITE-BACK GUARD CANNOT KNOW THAT.**
  Moving blocks into `handoff-cairn-control-plane-archive.md` made the guard treat that
  filename as its own topic and demand a handoff for it. Checked rather than asserted before
  dismissing: the archive carries **zero** of `## Goal`, `## State now`, `## Next steps`,
  `## How to verify` or a closing-condition, and the prune is already described inside the real
  doc. **Dismiss is the right answer there** — a handoff written "for the archive topic" would
  mint a doc nobody wants. ⚠ The dismissal is per-session; a new session starts fresh.
- ⚠ **A HANDOFF DOC CAN NEVER RECORD ITS OWN MERGE**, so a `State now` naming the branch sha
  is stale by exactly one commit the moment it lands. That is the mechanism, not rot — do not
  read it as the doc being behind, and do not open a PR solely to bump it.
- 🔴 **TWO PRs EDITING THIS DOC CONFLICTED WHILE BOTH REPORTED `MERGEABLE` — MEASURED AGAIN, AND
  THE RESOLUTION IS THE INTERESTING PART.** `git merge-tree --write-tree` between #48 and the
  handoff PR exited **1**; GitHub called both CLEAN because it compared each against a `main`
  where neither had landed. 🔴 **Branch on the EXIT CODE** — that command prints only a tree OID
  on success and emits NO conflict markers, so a marker grep finds nothing either way. ⚠ **And
  the fix was not a conflict resolution**: #48's own handoff edit had rewritten the same sections
  with facts that were now TRUE, while the handoff PR's narrative ("#48 is open, not merged") had
  gone stale in the minutes since it was written. The right move was to **reset the docs branch
  onto the new `main` and rebuild the delta against it**, not to merge a stale story into a fresh
  one. **A doc PR that loses a race does not need resolving — it needs re-deriving.**
- 🔴 **zsh ATE `$var:` A THIRD TIME, IN THE SESSION THAT HAD JUST READ THE WARNING.**
  `git show $B:tests/parity/README.md` expanded through the history modifier `:t` and produced
  `fatal: ambiguous argument 'route-the-read-verbsests/parity/README.md'`. That one is LOUD and
  therefore harmless; the same expansion inside a grep returns a confident wrong value.
  **Brace it: `${B}:path`.** Recorded as the third instance because two were not enough.
- 🔴 **THE LEAK GATE CAUGHT MY OWN HANDOFF DELTA — BEFORE THE PUSH, WHICH IS THE WHOLE POINT.**
  Scanning the scratch delta before handing it to the write tool found **two real leaks**: an
  external tool named directly, and four private repo names quoted out of a search result. Both
  rewritten to describe by ROLE. Re-scan: 5 findings → 0, same instrument. ⚠ **My positive control
  was badly chosen and did NOT go red** — reach was proven instead by the test run flagging the
  file directly. A control that fails to fire is not a passing control; say which one actually
  carried the proof.
- 🔴 **AND THEN IT CAUGHT THE *NEXT* HANDOFF DELTA, AFTER THE PUSH — THIRD EVENT OF THE SAME
  CLASS, WITH THE BULLET DIRECTLY ABOVE ALREADY WRITTEN.** ⚠ **Not the same identifier**, and
  the distinction matters because it is what rules out "one remedy already covers this": the
  first was an external tool's name, the second a repository name, both `denied-identifier`.
  🔴 **The failure is a SEQUENCING one: `handoff_doc.py --confirm --push` commits and pushes in
  ONE call, so there is no moment between them to scan in.** Scanning "the pushed tree" is
  scanning after the mistake is public.
- 🔴 **AND THE REMEDY I FIRST WROTE FOR THAT WAS A THIRD PHRASING OF ADVICE THAT HAD ALREADY
  FAILED TWICE — RECORDED BECAUSE THE REACH FOR A BETTER WORDING IS THE ERROR, NOT THE WORDING.**
  It said "scan the SCRATCH DELTA, before the tool is invoked at all". The bullet above it said
  the same thing in different words, and the leak happened anyway. **There is no mechanism:
  `~/.claude/skills/handoff/` contains no reference to `leakscan` at all, so nothing runs it and
  nothing refuses on it.** A guard spelled as a sentence is walkable by forgetting, and three
  events is enough evidence that it is being forgotten. **The deterministic fix is one line in
  `handoff_doc.py`: run `leakscan` on the delta and refuse the write on rc≠0** — filed as ranked
  work, in the repo that owns that script rather than this one. Until it exists, the honest
  statement is that this hazard is UNGUARDED, not that it is handled.
- ⚠ **`leakscan` EXIT 2 IS "COULD NOT VOUCH", AND A LEFTOVER AGENT WORKTREE CAUSES IT.** A removed
  agent's worktree directory under `.claude/worktrees/` made the scanner exit 2 with
  `COULD NOT READ … Is a directory`. Not a leak and not a pass. Check the worktree is clean and
  its commits are on `origin` **before** removing it — then re-run for a real verdict.
- 🔴 **CI NEVER RUNS `nix flake check` — THE `nix` JOB BUILDS EACH CHECK BY NAME, SO A `checks.*`
  ENTRY NOBODY NAMES IS A CHECK NOBODY RUNS.** Verified on `main`: no flake-check step, eight
  explicit `nix build .#…` steps. All pre-existing checks ARE named, so there was no latent hole —
  but the convention is one omission away from producing one, and the omission reads as covered
  because the check exists in `flake.nix`. **Adding a `checks.*` entry means adding a CI step in the
  same commit.**
- 🔴 **MY OWN GREP WAS THE WRONG INSTRUMENT THREE TIMES IN ONE SESSION, AND THE THIRD ONE ALMOST
  REOPENED A CLOSED FINDING.** (a) Grepping parity row names for `multi-instance` found none and read
  as an unsupported claim — the rows are named `recall-routed-…`. (b) A crude comment filter reported
  7 "non-comment" lines in an all-docstring diff. (c) Grepping for a retracted sentence counted the
  **quoted retraction** — the repo's house style of recording the old wording so nobody re-derives it
  — as a live claim. **A zero, or a hit, from a pattern you chose is a fact about the pattern. Read
  the match before believing the count.**
- 🔴 **A FORCE-PUSH TO EXACTLY THE BASE TIP AUTO-CLOSES A PR.** Re-pointing a docs branch at `main`
  left it with zero commits ahead and GitHub closed the PR; the follow-up commit then landed on a
  closed PR. It **reopened** cleanly — unlike the deleted-base-branch case this repo already records,
  which refuses to reopen — and nothing was lost. **Push the branch WITH its commit, or reopen and
  check.**
- 🔴 **I MERGED A PR THROUGH TWO PENDING CHECKS BECAUSE `--auto` DID NOT WAIT.** `gh pr merge --auto`
  merged immediately with `go` and `tests` still running; they passed afterwards on `main`, which is
  the outcome being lucky rather than the process being right. **Read the rollup yourself before
  merging** — require the full check set present AND completed — and treat `--auto` as a request, not
  a guarantee.
- 🔴 **A DOC PR THAT LOSES A RACE NEEDS RE-DERIVING, NOT RESOLVING.** #48 and the handoff PR both
  edited this file; `git merge-tree --write-tree` exited **1** while GitHub reported both
  `MERGEABLE`, because it compares each against a `main` where neither had landed. But the fix was
  not a conflict resolution: #48's own edit had rewritten those sections with facts that had become
  TRUE, while the handoff's narrative had gone stale in the minutes since it was written. **Reset the
  branch onto the new base and rebuild the delta.**
- **Decision (operator, this session): MCP is HELD.** Recorded above in `State now` with the reason
  the OLD blocker must not be cited when it is revisited.
- 🔴 **OPERATOR DECISION: the audit ladder on PRs touching only tests, prose and CI config is
  now ROUND 0 + ROUND 1 ONLY, and stops regardless of findings.** Measured basis: round 0
  changed the outcome on all three PRs this session, while later rounds increasingly audited
  the ladder's own prose — three of four findings on #66's round 1 were prose-about-prose.
  ⚠ The cost is named rather than hidden: **#66's round 1 found a live-credential hole round 0
  missed**, so the round-1 pass is the half that must not be dropped.
- 🔴 **FOUR NEW DEFECT ENTRIES, PARKED HERE BECAUSE THE `Defects` HEADING REPLACES AND WOULD
  HAVE DELETED THIRTEEN.** Move them under ranked item 6.
  **(a)** `tests/unchanged_output_capture.py` is run by NO CI job and no nix check — measured,
  zero references in `.github/workflows/` and `flake.nix`. #66 decided it stays MANUAL with a
  written trigger; a blanket job would be red by construction, since it is a base-vs-head
  differential. **(b)** The canonical handoff is ~99 KB against its own 65 KB guideline and
  does not record #60, #63 or #66; its prune PR is blocked behind #62 and #65. **(c)** Two
  entries #66 deliberately did not answer because #64 owns their files — the
  `checks.default-is-the-go-client` Python-side insensitivity (`flake.nix`) and 2(d), whether
  `cairn-ui` should render through `internal/report` (`internal/ui/README.md`); 2(d) is likely
  answered by #64 itself. **(d)** Raw environment copies remain in five NON-consumer files
  (`tests/test_subsystem_store_api.py`, `tests/routing_mutants.py`, `tests/parity/world.py`,
  `tests/dualrun/harness.py`, `tests/conformance/oracle.py`); most build a SERVER environment,
  which is a different predicate, and none was audited. Stated in `env_pin.py`'s docstring.
- **The publish-workflow mutation battery REFUSES in a shell without pytest** —
  `REFUSING TO VOUCH: the baseline run executed ZERO tests`. Run it as
  `uv run --with pytest python -u tests/publish_workflow_mutants.py`; it is **22 mutants,
  0 problems** at `f2ddf45`.
- 🔴 **A GUARD I WIDENED TO ACCOMMODATE A NEW FEATURE STOPPED GUARDING, AND ONLY A MUTANT
  FOUND IT.** `TestEveryContentRouteConsultsTheAuthority` is a regression test for a shipped
  defect (a page rendered without consulting the authority). Adding a second authority, the
  natural edit was `source.calls == 0` → `source.calls + sharing.reads == 0` — and **a sum is
  satisfiable by the WRONG authority**: re-applying the original defect with a call to the
  other one PASSES. Fixed with a per-route expectation where a content route missing from
  the map FAILS. 🔴 **THE GENERAL SHAPE: when you add a second way to do the thing a guard
  watches, the guard is part of the change, and widening it is the failure mode.**
- 🔴 **AN `EQUIVALENT` MUTATION LABEL IS A CLAIM ABOUT *EVERY* OBSERVABLE, AND ONE MADE HERE
  WAS FALSE.** Two authority checks looked redundant ("removing either leaves the other
  answering 403"). The discriminator missed: a request with **no verb field** answers 403
  unmutated and **400** with the handler's check removed, because that check runs BEFORE form
  validation — so an unauthorised caller learns their input was malformed. **An EQUIVALENT
  label is precisely what stops anybody writing the test that kills the mutant**, which is
  why the retracted reason is kept beside the row rather than tidied away.
- 🔴 **A GUARD'S OWN PRESCRIBED REMEDY PRODUCED THE STATE IT REFUSES.** The control-journal
  refusal said "seed it with `cairn-server -create-user`" — which mints a user and **no
  credential**. A journal seeded exactly as instructed had 1 user, 0 credentials, and the
  surface came up announcing `sharing writable` while refusing every sign-in. **Third
  spelling of that guard: a byte count (walked around by a one-byte journal), a user count
  (walked around by the remedy), then the sign-in precondition itself.** 🔴 **A PROXY CAN
  ALWAYS BE WALKED AROUND — ask the question the code asks.**
- 🔴 **AN OPERATOR-FACING REMEDY CAN INVITE A SECRET INTO A DURABLE STORE.** "Hand-append a
  `credential-issued` record" named neither the shape nor that `token_hash` is the SHA-256
  digest. `Event.validate` checked that field's **LENGTH ONLY** at the time — under a message
  reading "this journal is not a place a credential may ever land" — so a 64-character token
  was accepted and persisted, and sign-in then failed on a digest mismatch with no signal why.
  **When you tell somebody to hand-write a record, name the field that holds a digest.**
  ✅ The check now requires hex, in either case, and `Model.apply` lowercases what it stores;
  the tense here is past deliberately, because the lesson is the remedy's shape rather than
  the guard's state.
- 🔴 **A COMMENT THAT WOULD HAVE INSTRUCTED THE NEXT MAINTAINER TO UNDO THE GATE IT
  EXPLAINS.** A commit moved the `go` job's `ok` floor to `-lt 18` and left three claims at
  17 beside it, including in bold *"So: `ok < 17` refuses. 17 passes (today's tree)."*
  Somebody whose deletion failed the gate reads that, concludes it is misconfigured and sets
  it back — restoring the off-by-one the block exists to record. **Move every number in a
  gate's explanation with its condition, in one commit; the explanation is the thing people
  act on.**
- 🔴 **FIVE ROUNDS, AND THE FIX ROUND'S OWN PROSE WAS THE NEXT FINDING EVERY SINGLE TIME.**
  Measured across the ladder: rounds 2–5 each found the previous round's sentences wrong —
  four unswept retractions, a completeness claim ("the last copy standing") that was itself
  false, a cost conclusion that **contradicted its own numbers**, and a per-round payload
  figure off by one in the very commit message arguing the ladder had converged. **Budget
  for it; the correction is cheap and the belief is not.**
- ⚠ **A COST MODEL RETIRED ON EVIDENCE THAT CONFIRMED IT.** A round replaced
  `O(P × G × log G)` with four timings and concluded the cost was "worse than the expression
  implies … quadratic and not the log-linear the old expression reads as". `P × G × log G`
  **is** quadratic when both parameters grow together; the model predicted 4.49× against
  4.47× measured. The conclusion came from reading a two-parameter expression as if one were
  fixed. `BenchmarkAudience` now makes the table re-derivable, which is what the round should
  have committed in the first place.
- **Decision (operator, this session): land both halves of the share flow as-is**, rather
  than dropping the unreachable write half. Round 0 argued the write half is unattributed and
  unreachable; a probe showed the READ half is equally journal-gated, so dropping it buys a
  smaller diff and no reachability.
- **Decision (operator, this session): keep `Candidates`' co-membership narrowing** — you can
  only share with people you already share a project with — with the invite flow filed as P6.

- 🔴 **THE SHARE FLOW'S SIX-ROUND AUDIT LADDER, RECORDED HERE RATHER THAN IN `State now`
  BECAUSE THAT HEADING REPLACES AND THIS IS THE DURABLE HALF.** #64 carried round 0
  (requirements/deletion) plus rounds 1–5, each dispatched BLIND against a hand-built
  worktree at the PR head — `isolation: "worktree"` branches from the DEFAULT branch here and
  would have handed every auditor a tree of `main` without the package under audit. What it
  found, in order: **round 0** a 234-line mutation battery **no gate ran**; **round 1** a
  regression guard the PR had **widened until it stopped guarding** (re-applying the original
  defect PASSED it) and an `EQUIVALENT` label **measured false**; **round 2** four retractions
  written at one site each by the commit whose own message said a retraction is a tree-wide
  sweep; **round 3** the control-journal guard's **own prescribed remedy producing the state
  it refuses**; **round 4** a CI comment that would have instructed the next maintainer to
  undo the floor it had just moved; **round 5** a remedy that **invited a secret into the
  authority journal**. 🔴 **THE STOP WAS ARGUED, NOT TRIGGERED.** Executable payload per
  round — `--remerge-diff --not origin/main`, comments stripped — was **15 → 22 → 31 → 4 → 7**
  against **722** in the PR proper. The attribution gate's two-consecutive-zeroes never fired
  and this was not an all-prose PR, so neither mechanism ended it; it was stopped on the
  stated grounds that the ladder had left the PR, with the auditor concurring unprompted.
  **Every round's findings are on the PR as comments, which is the record.**
- 🔴 **A DOC PR I WROTE PREDICTED ITS OWN STALENESS AND I MERGED THE FEATURE FIRST ANYWAY —
  ON PURPOSE, AND THE ORDER IS THE POINT.** #71 said "still 3 of 4 on `main`", which merging
  #64 made false within the minute. Writing the handoff first and merging second would have
  shipped a doc that was wrong on arrival; merging first and re-deriving the delta costs one
  rebase and produces a doc whose verdict was MEASURED rather than predicted. **When a doc's
  claim is about a state your next action changes, take the action first.**
- **Decision (operator, this session): the credential gap is FILED, not built.** A
  credential-issuing command carries its own decisions — token generation, display-once,
  revocation UX — and the one hazard worth naming is that it is the single path where a
  secret could reach the journal. It is ranked work rather than a bolt-on to the share flow.
- **Decision (operator, this session): `Candidates`' co-membership narrowing STAYS.** You can
  share only with people you already share a project with; an invite flow is P6. Offered and
  declined: an exact-id lookup behind a uniform refusal, and widening to projects you
  administer.

- ✅ **THE CONTROL-PLANE ARC CLOSED AT `7d7c9ea`, AND THE VERDICT IS RECORDED HERE BECAUSE
  `State now` REPLACES AND THIS IS THE DURABLE HALF.** The condition — frozen at round 1, four
  clauses, naming the three commands a later session runs — was measured **on `main`** rather
  than inferred from the PR's six green checks: authz matrix PASS · share flow serving a scope
  granted from one user to another with its replica-honesty notice pinned PASS · identity
  through both backends PASS · `packages.default` the Go client PASS · `pytest tests -q` 1997
  passed / 0 failed · `go test ./...` 18 ok. **ADDRESSED ⇒ ARC CLOSED.** 🔴 **THE "ON `main`"
  WORDING IS THE WHOLE POINT AND IT COST ONE EXTRA STEP TO HONOUR**: green CI is a claim about
  a branch, the clause asked about `main`, and they agreed here only because they were both
  measured. A session that reported "addressed" off the PR's rollup would have been asserting.

- 🔴 **A CLOSING CONDITION MET BY A PR CLOSES NOTHING UNTIL SOMEBODY EDITS THE ENTRY — AND THE
  UPDATE HOLDING THE MEASUREMENT IS THE ONE THAT HAS TO DO IT.** `Defects (batched)` REPLACES, so
  a bullet survives every update that does not retype it; the prune deliberately did not touch
  that section; and a 🔴 entry whose work had landed two merges earlier sat at the top of the list
  ranked work is drawn from, asserting a byte count off by 46,831. ⚠ **This lesson is recorded
  HERE and not beside the entry it came from, because that entry is now marked ✅ CLOSED and the
  next session retyping the section will quite reasonably drop it** — which is the same structural
  mistake, inverted, and an audit caught it one round after I made the un-inverted version.
- 🔴 **A SELF-IMPOSED TARGET WITH NO INSTRUMENT MUST NOT BE REPORTED AS A GATE.** Nothing in this
  tree reads `claudedocs/handoff-*.md` — no test, no nix check — so this document's byte guideline
  is a number sessions quote to each other. `AGENTS.md`'s budget is different in kind: a test owns
  it and prints what to evict. ⚠ And the sentence asserting the guideline appeared "nowhere else
  in the tree" was itself false at the un-comma'd spelling, which is the count-defect shape one
  bullet down, committed while filing it.
- 🔴 **A COUNT FILED AS A PROSE DEFECT CAN BE A LIVE GATE DEFECT, AND THIS ONE WAS.** The
  "nine verbs" entry read as four stale comments. Measured: five sites, and one was
  `assert len(go_verbs) >= 9` against ten verbs — a floor ONE BELOW the count, which buys
  exactly one free deletion, which is the only deletion anybody would make. It is the same
  defect as the `go` job's `ok` floor (`-lt 16` against seventeen packages) and as the
  publish battery's, now three times in this repository. **Before batching a count defect as
  prose, grep for the number in an `assert` or an `if`.**
- 🔴 **MY OWN MEASUREMENT OF THAT COUNT WAS WRONG FIRST, AND IT WOULD HAVE HAD ME "CORRECT"
  NINE TO NINE.** `go run ./cmd/cairn -verbs | tail -n +2 | grep -c .` answered **9** — the
  `tail -n +2` assumed a header row that the command does not print, and silently dropped
  `append`. Reading the RAW output answered ten, and argparse agreed. **Parsing a tool's
  output makes its FORMAT a dependency you did not pin**, and the failure here was a
  confident number that matched the stale prose.
- 🔴 **DELETING DEAD CODE REPRODUCED THE FINDING INSIDE ITS OWN FIX.** #44's finding was that
  `normalise_shell` was dead *and* that a docstring still pointed at it. Deleting the function
  left TWO docstrings pointing at it — the same defect, freshly made, in the commit closing it.
  **After removing a symbol, grep its NAME, not just its call sites.**
- 🔴 **A GUARD'S MESSAGE CLAIMED EVERY STATE WHILE ITS BODY COUNTED ROWS, AND THE STATE IT
  MISSED WAS THE ONE THAT BREAKS THE NAIVE SPELLING.** `internal/client`'s `doctor` fixture
  renders `OK`, `UNMEASURED` and `NOT-OBSERVABLE` — never `PROBLEM`, whose marker `🔴` is a
  SINGLE RUNE where the other three are two. Closed with two guards rather than one: the
  fixture's states are a ledger that fails if it SHRINKS, and a separate unit case drives
  `doctor.ParseRow` over every state. ⚠ **`ParseRow` LANDED ON `main` AT `93d0f03`**, replacing the
  exported `Markers()` table; this bullet carried a "pending #74" label for exactly one merge,
  which is the point — a durable bullet naming a symbol one unmerged PR away greps to zero hits
  with nothing to tell a reader whether it was renamed or reverted. **When a fixture cannot reach a case, the honest fix is
  a second guard, not a wider sentence.**
- **Decision (operator, this session): the prune was chosen over feature work**, and its
  closing condition was met by MOVING rather than cutting. 🔴 **The safety property of a prune
  is assertable and was asserted**: every bullet verified present in exactly one of the two
  files, 0 missing and 0 duplicated, with every other section byte-identical. A prune that
  cannot prove it moved rather than cut is the `BYPASS`-deletion failure with a tidier diff.

- 🔴 **THE OPEN-PR SWEEP FOUND WORK THE LOCK COULD NOT — FIRST MEASURED INSTANCE, AND IT WAS
  THE WHOLE OF RANK 8.** `claim-work --list` showed `cairn-control-plane-8` free and the ranked
  item read as unbuilt; `gh pr list --state open` carried a three-commit, `MERGEABLE` PR
  implementing exactly it, twelve hours old and claimed by nobody. The lock is a
  compare-and-swap on a ref: it covers the FIRST mover and says nothing about work that was
  done without ever taking a claim. **Run both, every time, and read the PR TITLES rather than
  counting rows** — the duplicate does not announce itself as one.
- 🔴 **A PR BODY IS A CLAIM, AND THE CLAIM WAS ABOUT A DESIGN THAT NO LONGER SHIPPED.** #1854's
  body carried a watched end-to-end run, a red/green matrix and a mutation table — all taken
  against its FIRST shape, whose differential attribution machinery its third commit **deleted**
  in response to an operator decision. An audit-driven redesign resets the verification gate, so
  the evidence in the body was about code that had been removed. **Re-watch after a redesign;
  the body's confidence is the strongest reason to, not a reason not to.**
- 🔴 **GATE ON THE MERGED TREE, NOT THE PR BRANCH — AND THE TELL IS THAT THE BASE MOVED, NOT
  THAT ANYTHING OVERLAPPED.** #1854's four tekton checks were green on its own branch while
  `main` had advanced two commits. `git merge-tree --write-tree` exited **0** (branch on the
  EXIT CODE — it prints a tree OID and emits no conflict markers, so a marker grep finds
  nothing either way), and the merged tree then ran **575 passed / 0 failed**. Green on the
  branch was a claim about a tree nobody was going to ship.
- 🔴 **"IS THE EDIT LIVE?" IS TWO QUESTIONS WHEN A SKILL SHIPS BOTH PROSE AND A SCRIPT, AND THE
  ANSWERS DIFFERED.** `~/.claude/skills/handoff/SKILL.md` resolves into `/nix/store` — a
  `home.file` copy, so its edited legend needs a home-manager switch — while the same file
  invokes the handoff tool (`<handoff-tooling-repo>/scripts/lib/handoff_doc.py`), the WORKING COPY, so the gate itself went
  live on the base clone's fast-forward. **`readlink -f` answers only for the file you point it
  at; read what the skill EXECUTES as well as what it says.**
- 🔴 **AN EMPTY RESULT COULD NOT DISTINGUISH TWO CAUSES OF A `leakscan` EXIT 2, AND THE CHEAP
  CONTROL SETTLED IT IN ONE COMMAND.** The base clone exited 2 with untracked `result` symlinks
  AND a live agent worktree both present, either a plausible culprit. A fresh worktree of
  `origin/main` — same tree, neither artefact — scanned **rc 0**, and the scanner's single
  `COULD NOT READ` line named the worktree directory. **The rival mechanism was named before
  concluding, and the discriminating control cost less than reasoning about it would have.**
- ⚠ **THE DOCUMENTED `leakscan`-EXIT-2 GOTCHA NAMES A *REMOVED* AGENT'S LEFTOVER, AND MINE WAS
  A LIVE ONE — SO ITS REMEDY DID NOT APPLY.** The recorded fix is "check the worktree is clean
  and its commits are on `origin` before removing it". `git worktree list --porcelain` showed
  `locked claude agent … (pid 355708)`, created minutes earlier, and that pid was in the same
  run's own process list. **Read the lock line before acting on a remedy that ends in
  `rm`** — and when the directory is another session's, the answer is to report, not remove.
- 🔴 **A STARTUP BANNER SAYING THE FEATURE IS WRITABLE IS NOT EVIDENCE THE FEATURE WORKS, AND
  THIS REPO ALREADY KNEW IT.** `cairn-ui` printed `sharing writable (control journal …)` — the
  same sentence the doc records a journal printing while it refused every sign-in. The chain was
  exercised instead: 401 anonymous, a `token` password field, 303 on POST, and a cookie'd `GET /`
  showing exactly the two scopes that principal can reach and neither of the other two.
- 🔴 **THE SHARE PAGE LISTED A USER WITH NO GRANT BEHIND THEM, WHICH IS THE POSITIVE CONTROL FOR
  THE CLAIM `AGENTS.md` MAKES.** "Who has access to this" rendered the co-member
  `read,write via project membership` while "Shares you can take back" said *"No grant names this
  scope"*. A listing built from `Model.Grants` would have shown nobody. **The way to test
  "answered from `control.Resolve`, never from the grant rows" is a principal who has authority
  and no grant** — not a principal who has both, which both implementations render identically.
- 🔴 **VERIFYING A DURABLE WRITE CONSUMES THE ACTION YOU WERE ASKED TO HAND OVER, UNLESS YOU
  COPY THE WORLD FIRST.** The rank-9 task was to bring the flow up and let a human click it; a
  `POST /share` against the live instance would have appended a `granted` event to an
  append-only journal and spent the human's verification. `cp -a` the world, a second instance on
  another port, POST there, confirm the event, kill by resolved PID, delete the copy. The
  handed-over journal stayed at 10 lines — asserted, not assumed.
- 🔴 **`-create-user` WITH THE SAME `-project` NAME TWICE CREATES TWO PROJECTS, SILENTLY.** Two
  invocations naming `orbit-works` produced `prj_ejhc…` and `prj_wjwm…`, each with its caller as
  OWNER. Nothing warns, and the second user's own warning line (*"can reach NOTHING"*) reads as
  a scopes problem rather than a membership one. The flag's help says "the project created for
  this user", which is accurate and is not what a reader deriving co-membership expects.
- ⚠ **RULE (m) FIRES BEFORE RULE (o), WHICH COSTS A ROUND WHEN PROBING THE LEAK GATE.** A probe
  delta whose `## Goal` carried a prose closing condition rather than the
  `closing-condition:` key exited **11** `undefined-done` and never reached the scanner.
  Harmless and loud — but a probe of a LATER gate has to satisfy every earlier one first.
- 🔴 **THE GATE CAUGHT *THIS* HANDOFF, FORTY MINUTES AFTER I MERGED IT — FIFTH EVENT OF THE
  CLASS AND THE FIRST STOPPED BY CODE RATHER THAN BY HAND.** The `--confirm` run exited **13**
  with **five** `denied-identifier` findings in my own delta: the handoff-tooling repo's name
  three times (inside a `$`-variable spelling, which is why it did not look like a name), an
  external task board's name once, and — the one worth the whole bullet — **this repo's own
  synthetic canary**, which I had pasted into `## How to verify` while documenting how to probe
  the gate. ⚠ **A SYNTHETIC VALUE IS STILL A DENIED IDENTIFIER: the canary exists to be
  refused, so quoting it in a committed doc is a real violation, not an exemption.** 🔴 **THE
  OPT-IN WAS AVAILABLE AND WOULD HAVE BEEN A LIE** — the fresh worktree had scanned rc 0
  minutes earlier, so the tree was not already red and the findings were mine. All five were
  rewritten to describe by ROLE, re-scanned, and the write then landed. **The previous four
  events were each followed by a better-worded sentence; this one was followed by a refusal.
  That is the whole argument for the gate, and it arrived unprompted.**
- 🔴 **`git clean -f` IS HOOK-BLOCKED HERE AND THE BLOCK KILLS THE WHOLE `bash` CALL, NOT JUST
  THAT LINE.** A `set -e` script whose third command was `git clean -fd` ran **none** of its
  commands, including the `checkout --detach` on line one — and the follow-up read showed HEAD
  still where the reset was supposed to have moved it from, which reads exactly like a reset
  that silently failed. **After a blocked call, assume NOTHING in it ran**, and prefer
  `git checkout --detach <ref>` to a reset: it refuses rather than destroys.

## How to verify

```bash
cd /home/zach/workspace/cairn
python3 tests/leakscan.py; echo "rc=$?"      # CAPTURE THE RC BEFORE ANY PIPE
uv run --python 3.12 --with pytest -- pytest tests -q -p no:randomly
go vet ./... && go test ./... && go test -race ./...
python3 -u tests/control_mutants.py          # 120 mutants over SEVEN packages
uv run --python 3.12 --with pytest -- python -u tests/publish_workflow_mutants.py
python3 tests/conformance/suite.py run       # oracle: 0 failures
bash tests/conformance/run_go.sh             # Go: 0 failures, 4 skips
python3 tests/dualrun/harness.py             # SUMMARY … differences=0
python3 tests/parity/harness.py --break-pod  # MUST exit 2 — could not vouch
```

🔴 **RUN THE MUTATION BATTERIES UNDER AN INTERPRETER THAT HAS `pytest`.** A bare `python3`
has none. `control_mutants.py` itself needs only a Go toolchain and **REFUSES with exit 2**
when there is none, rather than skipping.
🔴 **`dualrun` and `parity` exit 2 for "COULD NOT VOUCH", which is NOT "failed"**.
🔴 **`leakscan` EXIT 2 ON THE BASE CLONE IS EXPECTED WHILE ANY AGENT WORKTREE EXISTS UNDER
`.claude/worktrees/` — scan a FRESH WORKTREE of `origin/main` for a real verdict.** Measured
both ways this session: base clone **2**, fresh worktree **0**, same tree.
🔴 **Verify a squash merge BY CONTENT, never by ancestry.**
🔴 **Reading CI: require the full check set present AND all COMPLETED.** ⚠ `mergeable` can read
`UNKNOWN` for minutes after the last check goes green — GitHub computing lazily, a property of
the API rather than of the PR. Re-read until it settles.

**Rule (o), the handoff leak gate — how to see it work (it is in the OTHER repo):**
```bash
# In a THROWAWAY detached worktree, with the scratch delta OUTSIDE the repo so it
# cannot pollute the baseline. A delta carrying this repo's OWN synthetic canary (`DENY_CANARY` in `tests/leakscan.py`) — a value that is
# OWN synthetic canary, in DENIED_IDENTIFIER_DIGESTS — must exit 13 and leave no trace.
python3 <handoff-tooling-repo>/scripts/lib/handoff_doc.py --repo <throwaway-wt> --topic <t> \
  --update <scratch-outside-the-repo>.md --advanced '…' --new-effort --confirm
# expect: rc 13, `status=leak-refused scanner=tests/leakscan.py exit=1`,
#         git status --porcelain EMPTY, the doc absent.
```
⚠ **Do NOT pass `--push` while probing, and do not probe in a worktree whose branch anyone
else holds.**

**The share flow, by hand (rank 9's bring-up, reproducible):**
```bash
nix build .#cairn-ui .#cairn-server-go      # local flake; main == origin/main
# 1. a synthetic store + token row, straight from the parity world builder:
#    python3 -c 'import sys; sys.path.insert(0,"tests/parity"); import world; …'
#    world.build_store(<root>) and world.TOKEN_ROW  → four scopes, one malformed entry
# 2. two users. 🔴 THE SAME -project NAME CREATES A SECOND PROJECT, so they are NOT
#    co-members and the share flow's candidate narrowing finds nobody:
export CAIRN_CONTROL_JOURNAL=<w>/journal.jsonl
./result-1/bin/cairn-server -create-user -provider supabase -subject sub-a \
  -email a@example.invalid -project orbit-works -scopes alpha-notes,beta-notes -store <w>/store
./result-1/bin/cairn-server -create-user -provider supabase -subject sub-b \
  -email b@example.invalid -project orbit-works -scopes "" -store <w>/store
# 3. 🔴 HAND-APPEND a `member-set` event putting B in A's project — NOTHING WRITES IT.
#    Required fields: kind, at, actor, user_id, project_id, role (owner|admin|member),
#    and `narrowed_scopes` which must be PRESENT (null), never omitted.
# 4. a live credential for A. WITHOUT one the surface exits 78; `-create-user` does NOT
#    fix that, it mints a user and no credential:
./result-1/bin/cairn-server -issue-credential -principal <usr_…> -principal-kind user \
  -label '…' -token-out <w>/a.token -store <w>/store
./result/bin/cairn-ui -store <w>/store -token-file <w>/tokens \
  -control-journal <w>/journal.jsonl -session-file <w>/sessions.jsonl \
  -host 127.0.0.1 -port 8103
```
🔴 **`sharing writable` IN THE STARTUP LINE IS NOT EVIDENCE SIGN-IN WORKS** — the doc already
records a journal that printed it while refusing every sign-in. **Exercise the chain**: `GET /`
anonymous → **401**; `GET /sign-in` → a `password` field named `token`; `POST /sign-in` with an
`Origin` header → **303** to `/`; `GET /` with the cookie → only the scopes that principal can
reach. Measured this session: 401 · 200 · 303 · `alpha-notes` and `beta-notes` and **not**
`rubble-heap`/`hollow-set`.
🔴 **AND VERIFY THE WRITE HALF ON A *COPY*, NOT ON THE WORLD YOU ARE HANDING OVER** — a share
POST is durable in an append-only journal, so probing the live one consumes the very action the
human was asked to take. `cp -a` the world, run a second instance on another port, POST
`/share` with the post-auth `csrf` hidden field and an `Origin` header, confirm a `granted`
event appended, then kill it **by resolved PID** (`ss -lptnH 'sport = :<port>'` →
`/proc/<pid>/cmdline`), never by a `-f` pattern.

## Open investigations — live diagnosis state

📄 **Five closed blocks were moved to `claudedocs/handoff-cairn-control-plane-archive.md`** —
PR #35's CI and the merged-tree byte budget, the P4 ladder, the round-1 fix round, and #38's
round-3/merged-gate block. They are resolved or superseded; the archive keeps them verbatim,
because a closed block's value is its measured values and eliminations. Read it on demand.

### A sibling session's HOME write turned a host-dependent test red mid-session
- as-of: 2026-09-18
- **Symptom + exact repro:** `python3 -m pytest tests -q` on the same tree reported
  `1942 passed` earlier and `1 failed, 1941 passed` later. The failure is
  `tests/test_cairn_doctor.py::TestTheCliWiring::test_a_no_sync_run_still_reads_the_LOCAL_config`,
  `IndexError` at `test_cairn_doctor.py:876`.
- **Observed (with values):** `stat` gives `~/.config/subsystem-store/instances` mtime
  **2026-09-17 22:00:43**, containing one `<a real project>.env` file; `routes.json` carries
  25 top-level keys.
  The green run finished **before** that write; the red runs came after. With that config
  present, `cmd_doctor` emits per-instance check names and the test's exact `"token"` lookup
  finds none.
- **Ruled out:** that it is PR #38's. It fails identically on plain `origin/main`.
  `via: measurement`
- **Ruled out:** intra-file test-ordering. It fails at file level too (`1 failed, 59 passed`)
  and alone. `via: measurement`
- **Ruled out:** `CAIRN_MIRROR_ROOT`. Unsetting it does not fix it. `via: measurement`
- **Leading hypothesis:** the session holding `cairn-oss-multi-instance-phase-c` (standing up a
  second store instance) wrote that config. CI is unaffected — fresh checkout, clean HOME.
- **Next probe:** none needed for #38. If it is to be fixed, the test should pin the HOME it
  reads rather than inheriting the operator's — that is the real defect.

### ✅ CLOSED — the `packages.default` flip's mechanical blocker (residual 8)
- as-of: closed at `feat/route-the-read-verbs`; the entry is kept rather than deleted because the
  measurement that made it a blocker is what stops the next session re-deriving the hold.
- **Was:** `cairn doctor` / `cairn ls-entries` via the Go client on a host with more than one
  instance configured → **exit 11**, refusal on stderr, 0 bytes stdout. All five read paths sat
  behind `RefuseUnportedMultiInstance` (`verbs.go:52,78,101,246`, `cli.go:614`).
- **Ruled out:** that this was a doc fix. It was raised as an undeclared narrowing whose blast
  radius could not be established from the code; running the packaged client on a real
  multi-instance host is what turned it into a blocker. `via: measurement`
- **Closed by:** `internal/report` taking an instance-aware caveat (four red-at-`d8b858a`
  differential fixture rows), `recall`/`search`/`validate` routing their scope and
  `sync`/`ls-entries`/`doctor` walking every instance, three multi-instance READ parity rows
  comparing stdout+stderr+exit, and the guard deleted with residual 8's table row.
- 🔴 **WHAT IS LEFT IS A DECISION, NOT A CAPABILITY — AND A GREEN GATE STILL DOES NOT LICENSE
  IT.** The flip **widens** the CLI contract (`-verbs`/`-exit-codes` answer 0 where the oracle
  exits 2) — residual 7 — and needs an announcement, because it changes what
  `nix run github:…/cairn` executes for consumers who never asked for a new client.

### A live JWKS fetch has never been exercised against a real issuer
- as-of: 2026-09-18
- **Symptom + exact repro:** not a defect — a coverage gap three rounds touched and none closed.
- **Observed (with values):** every measurement about the Go image's TLS trust is an
  `x509.SystemCertPool()` root COUNT, never a completed handshake: **121** roots with
  `SSL_CERT_FILE` as shipped, **121** unset, **121** pointed at `/nonexistent`, against a
  positive control of **0** in an image with no CA roots. The bundle is reachable and the
  variable is **inert in both directions** — which is now what the code says.
- **Ruled out:** the original justification, that the image "has no `/etc`" so the bundle would
  be unreachable without the variable. `pkgs.cacert` is root-merged by `buildLayeredImage`, so
  `/etc/ssl/certs/ca-bundle.crt` exists and `/etc/ssl/certs` is in Go's `certDirectories`.
  `via: measurement`
- **Next probe:** a pod, a network and a real issuer. Nothing in the repo covers it.

### CLOSED — the publish gate, and the second defect the first one hid
- as-of: 2026-09-19
- **Symptom + exact repro:** `gh run list --workflow=publish-image.yml` → 7 runs, 7 failures.
- **Observed (with values):** every run died in `pin skopeo` with exit 127 plus
  `Invalid format`, because `nix build --print-out-paths` prints **two** paths for
  `nixpkgs#skopeo` (the `man` output FIRST). Fixed by naming the output — `nixpkgs#skopeo.out`
  prints exactly one, measured at the pinned lock on nix 2.34.8.
- **Observed (with values):** with that closed, the run reached the push and failed
  **`denied: permission_denied: write_package`**. Cause measured: the package read
  `repository: null`, created **17 minutes BEFORE the workflow first landed** — a hand push had
  made it a user-scoped package with no repo for `packages: write` to be based on. Granting the
  repo write access fixed it; both packages now read `vis=public repo=<this repo>`.
- **Ruled out:** that the resolver needed a script. An audit refuted it by measurement and the
  111-line script was deleted. `via: measurement`
- **Next probe:** none — closed. A successful run is on record.

### STILL LIVE, re-measured at `e15e331`: the host-HOME-dependent doctor test
- as-of: 2026-09-21
- **Symptom + exact repro:** `uv run --python 3.12 --with pytest -- pytest tests -q -p no:randomly`
  → **`1 failed, 1969 passed`**, the failure being
  `tests/test_cairn_doctor.py::TestTheCliWiring::test_a_no_sync_run_still_reads_the_LOCAL_config`.
- **Observed (with values):** measured TWICE this session, on `main` at `0587ace` and again on the
  `docs/positioning-agent-swarm-memory` branch — **identical count both times, 1 failed / 1969
  passed**, so the branch introduced nothing. `stat ~/.config/subsystem-store/instances` →
  mtime **2026-09-17 22:00:43**, one `.env` inside. `via: measurement`
- **Ruled out:** that either of this session's PRs caused it — the same failure and the same pass
  count on plain `main` before any branch existed. `via: measurement`
- **Leading hypothesis:** unchanged from the 2026-09-18 block above — a sibling session's write to
  `~/.config/subsystem-store/instances` makes `cmd_doctor` emit per-instance check names, and the
  test's `"token"` lookup finds none. CI is unaffected (fresh checkout, clean HOME).
- **Next probe:** none needed for either merged PR. The real defect is that the test inherits the
  operator's HOME instead of pinning one; that is the fix when somebody wants it.

### A byte-identity test failed once and has not reproduced
- as-of: 2026-09-22
- **Symptom + exact repro:** `uv run --with pytest python -m pytest tests -q -p no:randomly`
  → `1 failed, 1971 passed`, the failure being
  `tests/test_subsystem_store_api.py::TestSeedThenVerify::test_a_seeded_copy_serves_byte_identical_digests`.
- **Observed (with values):** one failure in one full-suite run during #63's development.
  Re-run alone: **1 passed**. Re-run as a full suite immediately after: **1972 passed**.
  Every full-suite run since — four of them, up to **1994 passed** at `f2ddf45` — was green.
  Wall time of the failing run was **485.82 s** against an observed band of **477–495 s**.
  `via: measurement`
- **Ruled out:** load. A load flake inflates EVERY test in a run; this run's wall time sits
  inside the normal band and no sibling test was inflated. `via: measurement`
- **Ruled out:** caused by #63. That test imports neither `env_pin` nor
  `unchanged_output_capture` and shares no fixture with anything the PR touched. `via: code`
- **Leading hypothesis:** a port collision. The test runs `run_seed` then `running(stage)`,
  which binds a listening socket; a concurrent test or a sibling session's process taking the
  same ephemeral port gives exactly one failure with no timing signal. `via: assumed`
- **Next probe:** run the file alone in a loop of 20 and watch for a single failure —
  `for i in $(seq 20); do uv run --with pytest python -m pytest tests/test_subsystem_store_api.py -q -p no:randomly | tail -1; done`
