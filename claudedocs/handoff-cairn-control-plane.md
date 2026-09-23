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

- `main` @ **`a88f60b`**. **No open PRs.** No `cairn-control-plane-*` claim is held — rank 8 was
  released at the end of this session precisely so the next one can take it.
- ✅ **THE BROWSER'S WRITE HALF WORKS, AND IT WAS DRIVEN END TO END ON `main` RATHER THAN
  INFERRED.** `cairn-server -issue-credential` (#76, `a88f60b`) is the tool that did not exist
  this morning. Measured loop, all against `origin/main`:
  `-create-user` ×2 → `-issue-credential -token-out` (file mode **600**) → `cairn-ui
  -control-journal` starts and prints **"sharing writable"** (it REFUSED to start before) →
  `POST /sign-in` **303** → `GET /share` lists **"Scopes you can share: alpha-notes"** (it said
  "No scope is administrable by this credential" before) → `POST /share` **303**, journal 8 → 9
  lines, `GRANTED grt_hw7c… → project prj_ktsg… verbs=[read write]`.
  Both cross-site gates hold on the live binary: **wrong CSRF → 403**, **missing `Origin` → 403**.
- 🔴 **AND IT IS STILL DEPLOYED BY NOTHING — THAT HAS NOT CHANGED.** No image derivation, no
  manifest; `publish-image.yml` pushes the two POD images only. "Usable from a browser" means
  *you run the binary yourself*. Two further limits, both by design and both named on the page:
  sharing reaches only principals you already share a project with (invite flow is P6), and
  `EventCredentialRevoked` **still has no writer**, so rotation is issue-then-hand-append.
- ✅ **FOUR MERGED THIS SESSION, EACH VERIFIED BY CONTENT** (a squash makes ancestry false
  forever, so content and merge-state are checked separately): **#74 → `93d0f03`**,
  **#75 → `a41594c`**, **#76 → `a88f60b`**. A sibling landed **#69 → `56cc56e`** (the
  `CAIRN_*` rename), **#78** and **#79** alongside.
- 🔴 **#76 RAN A FOUR-ROUND LADDER AND EVERY ROUND FOUND WHAT THE PREVIOUS FIX INTRODUCED.**
  Round 0: the new hex guard refused an EXISTING journal **whole** on replay. Round 1: the token
  leaked through **14 of 22 `fmt` verbs**, and the drop leniency broke its own "never silent"
  precondition. Round 2: the prose credited the wrong half of the redaction. Round 3: three stale
  claims, **no behaviour defect**. The ladder was stopped by operator decision at round 3, not by
  a clean round — stated because those are different claims.
- **No external-task-board field**: the resolver exited **5** — 0 tasks for this session, which
  cannot distinguish "touched none" from "wrong id". Not a clean bill of health. The doc's
  existing field was left untouched.

## Next steps (ranked)

🔴 **THE NUMBERING IS STABLE AND DONE ITEMS ARE MARKED, NOT DELETED.** Rank is half a
`claim-work` slug; this session measured a renumber re-pointing **two** live claims at work they
did not name, and `rc 12` ("ALREADY YOURS — carry on") waving it through. **Read the SUBJECT the
claim prints, never the number.**

1. ✅ **DONE — #74 merged as `93d0f03`**, entries closed in `## Defects (batched)`.
   forcing: gate — the entries' own closing conditions, which named a PR.
2. **The remaining batched defects.** Still open: #48's (c) `lib/README.md` (the re-count recipe
   greps two literal phrases, so it is a SPELLED check that cannot see a reworded echo); the
   stale node-affinity comment, which lives in the **deployment-manifest repository** rather than
   this one — 🔴 that repo's NAME is a denied identifier here, so take it from the operator; and
   the carried-forward list.
   forcing: gate — filed BY attribution gates rather than fixed, so nothing else surfaces them.
3. **P7 — conditional snapshot sync.** `/api/v1/snapshot` ships a full tar with no ETag/304.
   Key it on principal + epoch; `control.Authorization` already carries `Epoch`.
   forcing: none
4. **P8 — retire the Python oracle.** Gated on the default flip holding over real use, which is a
   waiting period rather than a task.
   forcing: none
5. ✅ **DONE — `cairn-server -issue-credential` over `control.IssueCredential`, in #76**
   (`a88f60b`). ⚠ **`EventCredentialRevoked` STILL HAS NO WRITER** — the same shape as the gap
   this closed, one event over.
   forcing: gate — a shipped binary refused to start and named this as the missing piece.
6. **Fold the new defect entries in and mark #60/#66 closed.**
   forcing: user — the operator chose this sequencing explicitly.
7. ✅ **DONE BY A SIBLING — `SUBSYSTEM_STORE_*` → `CAIRN_*` landed as #69 (`56cc56e`)**, with
   `internal/envalias` as the resolver. ⚠ Merging it into an in-flight branch is what produced
   this session's worst damage — see the Gotchas bullet on declaration-set diffs.
   forcing: user — item 5 of the five approved with "proceed as recommended".
8. 🔴 **MAKE `handoff_doc.py` RUN `leakscan` ON THE DELTA AND REFUSE ON rc≠0.** **Released, and
   the recommended next item.** Four `denied-identifier` events have now happened on handoff
   deltas, one reaching `main` as #68; the standing remedy is a SENTENCE, written four times in
   four wordings, and `~/.claude/skills/handoff/` mentions `leakscan` nowhere. The write tool
   commits and pushes in ONE call, so the delta file is the only place a check can sit. ⚠ It
   lives in the repository that owns the handoff tooling, NOT this one, and must find the scanner
   from the TARGET repo — a repo with no `tests/leakscan.py` has to PASS, not fail.
   **Closing condition:** a delta carrying a known-denied identifier is refused by the tool,
   watched.
   forcing: incident — four leak events, one of which reached `main`; the gate's own refusals are
   the evidence, and the last two happened with the lesson already written down.
9. 🔴 **MANUAL BROWSER VALIDATION OF THE SHARE FLOW, DRIVEN BY A HUMAN IN A REAL BROWSER.**
   Everything in `State now` was measured with `curl`, which is a different instrument: it sends
   no `Origin` unless told, runs no JavaScript, keeps no cookie jar semantics, and cannot see
   layout, focus order, or whether the replica-honesty notice is actually READ. **Bring the
   surface up with the `How to verify` recipe below and click it.** ⚠ 🔴 **DO NOT RAISE OR FOCUS
   A WINDOW FROM AN AGENT** — that is a `pkill`-class action on the operator's screen; hand the
   URL over and let the human open it.
   **Closing condition:** a named person reports the click path worked in a real browser, naming
   what they saw on `/share` before and after the grant.
   forcing: user — asked for explicitly at this session's handoff.

## Defects (batched)
- ⚠ **CLOSED ENTRIES MOVE TO THE ARCHIVE, THEY DO NOT ACCUMULATE HERE.** Three went there
  with this update, verbatim. This section REPLACES, so every closed entry left in it is
  retyped by hand each round until somebody drops it — which is how one sat 🔴 and
  46,831 B wrong for two merges. The LESSONS from a closed entry belong under `Gotchas`,
  which appends; the entry itself belongs in the archive.
- 🟡 **FOUR FILED BY #54's AND #57's LADDERS — AND (a) AND (b) WERE RE-MEASURED AND ARE WRONG AS
  FILED.** 🔴 **(a) IS CLOSED AND ITS ENTRY IS INVERTED.** `internal/ui/README.md` carries no
  "takes two backends" sentence at all — it records the removal explicitly, saying the sentence
  *"deliberately carries NO parameter COUNT"* because refreshing the number would have been wrong
  on merge with no textual conflict to warn anybody — and `AuthBackends` now takes **two**
  parameters, landed by #58. Acting on this entry means hunting a sentence that is gone, or
  re-adding a count the file deleted on purpose. 🔴 **(b) NAMES THE WRONG FILE.**
  `internal/control/tokenfile/source.go` was fixed by `c47636b`; it now names `internal/depspolicy`
  and carries the retraction in place. The LIVE stale copy is
  **`internal/control/filestore.go:54`** — *"This module has no `require` block and `flake.nix`
  passes `vendorHash = null`"* — both halves false since #55. ⚠ **That is the same wrong-site
  failure the "NINE VERBS" entry above was corrected for, in the same section, found by the same
  audit.** (c)
  `internal/report/testdata/reader_fixtures.json` contains **no `[cairn: …]` trailer at all** (0
  hits against 8 in `server.py` as a positive control), so the differential reader fixture never
  exercises attribution rendering — which is why an attribution-rendering defect was invisible to
  every gate. (d) Whether `cairn-ui` should ever render through `internal/report` rather than its
  own code is **undecided and now stated as open in `README.md`**. **Closing condition:** one PR for
  (a)–(c); a written line for (d).
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
  every count from that file rather than refreshing them, because the file itself recorded that they
  had already gone stale once. The repo owns the fix pattern
  (`tests/test_control_mutant_count_is_pinned.py`); applying it to what remains is separate work.
  **Closing condition:** a decision to pin each or a written line saying why not.
- ✅ **CLOSED BY #74 (`93d0f03`) — #48's (a) AND (b), "NINE VERBS", AND #44's FOUR.** Verified by
  CONTENT, not ancestry: the payload paths diff empty between the PR head and `origin/main`, and
  `func Markers` is gone from `internal/doctor/render.go` while `func ParseRow` is there.
  🔴 **(c) `lib/README.md` IS NOT CLOSED AND IS NOT COVERED** — the re-count recipe greps two
  literal phrases, so it is a SPELLED check that cannot see a reworded echo, and it overlaps the
  `COUNTS QUOTED IN PROSE` entry above. The entry's own condition said "all three"; the ruling
  recorded before the merge was that it should have been three conditions, because (a) and (b)
  share a package and a fixture while (c) shares nothing with them but the ladder that filed it.
  ⚠ **The "NINE VERBS" count was re-measured twice and wrong BOTH times** — filed as four,
  corrected to five, measured at six; the site it kept missing was the comment three lines above
  the floor, explaining the very floor the correction had just added. 🔴 **And the sixth was a
  GATE, not a comment**: `assert len(go_verbs) >= 9` against ten verbs, a floor one below the
  count, which buys exactly one free deletion and passed for it. It is now `== 10`, which reds
  when the set grows as well as when it shrinks. **A count filed as a prose defect can be a live
  gate defect — grep the number in an `assert` or an `if` before batching it as prose.**
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

- 🔴 **A CLEAN TEXTUAL MERGE DELETED FOUR TESTS, AND THE CONFLICT IT REPORTED WAS ABOUT A
  COMMENT.** Merging #69 into #76: `git` reported ONE conflict in `cmd/cairn-ui/main_test.go`,
  covering a comment; taking one side of that hunk removed four unrelated tests the other side
  had added further down the same file. No marker, no error, and reading the hunk could never
  have shown it. 🔴 **What caught it is a DECLARATION-SET DIFF AGAINST BOTH PARENTS** —
  `comm -23 <(git show <parent>:<f> | grep -oE '^func [A-Za-z][A-Za-z0-9_]*' | sort) <(… the
  merged file …)`, run for each parent. Do that on every merge where both sides touched one file;
  "no conflict markers left" is not the same claim.
- 🔴 **AND THE SAME MERGE LEFT A TREE THAT DID NOT COMPILE, FROM FILES THAT NEVER CONFLICTED.**
  The rename replaced an env helper; the new command's call site — added on the other branch, in
  a hunk the rename never touched — still called the old name. **Disjoint files are not safety:
  one side widened how something is read, the other added a caller.** The mutation battery is
  what said so first, by REFUSING TO VOUCH: *"the unedited copy is not green, so every mutant
  below would score KILLED for a reason that has nothing to do with its guard."* A non-compiling
  tree would otherwise have reported a perfect score.
- 🔴 **TWO COUNTED THINGS COLLIDED IN THAT MERGE AND NEITHER SIDE'S NUMBER SURVIVED.** The
  battery went 120→123 on one side and 120→147 on the other; the answer is **150**, and adding
  them is a guess. **Take a merged count from the gate that owns it** — here
  `tests/test_control_mutant_count_is_pinned.py` — and take the KILLED figure from an actual run
  (`150/148/2, 0 misattributed, 0 harness-errors`), never carry it forward.
- 🔴 **A REDACTION'S PROSE CREDITED THE HALF THAT DOES NOT WORK, AND I REPEATED IT BEFORE
  MEASURING.** `Issued` hid its token behind `String()`/`GoString()` plus a `*string` field. The
  code, the README and my own report said `Format` closed 21 of 22 verbs and the pointer closed
  the last. Measured over 7 shapes × 21 verbs: **pointer alone → 0 leaks; `Format` alone → 22**,
  19 of them through an `Issued` in an **unexported field of another struct**. `fmt` follows a
  pointer only at depth 0, so the field renders as an ADDRESS at any depth; `Format` is never
  consulted for a value `fmt` cannot `Interface()`, which is exactly what an unexported field is.
  🔴 **The sweep that "proved" the wrong attribution rendered at depth 0 only** — it structurally
  could not see the case its own docstring called realistic. **Ask what depth your sweep reaches
  before believing which half it credits.**
- 🔴 **A GUARD OVER A DURABLE FILE IS TWO DECISIONS — APPEND-TIME AND REPLAY-TIME — AND ONLY ONE
  IS USUALLY REASONED ABOUT.** Tightening `token_hash` to lowercase hex refused an EXISTING
  journal **whole** on replay: one bad record, zero credentials loaded, authority falls back to
  empty on a cold start. The population was the one the old docs created, since the pre-change
  text told operators to hand-append. **Narrowing a guard is not a safe direction when the guard
  runs over a file somebody else already wrote.** Operator decision: replay DROPS an unusable or
  duplicate `credential-issued` with a diagnostic, append still refuses outright — safe only
  because a dropped ISSUE narrows authority while a dropped REVOCATION would widen it, so the
  exemption is a kind-keyed table that structurally cannot reach any other event.
- 🔴 **"IT IS NEVER SILENT" IS A CLAIM ABOUT EVERY PATH, INCLUDING THE BACKGROUND ONE.** That
  same leniency announced drops at startup only, so a record hand-appended to a RUNNING pod
  produced an empty operator stream — the change had traded a loud outage for a silent no-op and
  reported only the first half. **When you make a failure quieter, ask which surface stops seeing
  it.**
- ⚠ **A LONG-RUNNING READER IS AS UNSAFE AS A WRITER IN A SHARED WORKTREE.** A full mutation
  battery was backgrounded in a tree that was then handed to a mutating agent; its result was
  meaningless and was discarded rather than quoted. The worktree-isolation rule is written about
  two writers; it applies to a reader whose answer you intend to believe.
- **Decision (operator, this session): the audit ladder on #76 STOPS AT ROUND 3**, with round 3's
  three stale-claim findings fixed. Not a clean round — a clean round would have ENDED it, and
  saying which happened is the point.
- ⚠ **`curl` IS NOT A BROWSER, AND THE DIFFERENCE IS A GATE.** `POST /sign-in` answers **403**
  from `curl` with no `Origin` header and **303** with one, because the same-origin gate runs
  BEFORE auth and a real browser always sends it. A session measuring the sign-in path without
  that header would file a working gate as a broken flow.

## How to verify

🔴 **THE BROWSER LOOP, END TO END, ON `main`.** Every step below was run at `a88f60b`; the
`curl` half is what an agent can do and the click half is ranked item 9's, for a human.

```bash
R=/home/zach/workspace/cairn; T=$(mktemp -d); mkdir -p $T/store/alpha-notes
printf '# Alpha notes\n\nSynthetic.\n' > $T/store/alpha-notes/index.md
nix develop $R -c bash -c "cd $R && go build -o $T/srv ./cmd/cairn-server && go build -o $T/ui ./cmd/cairn-ui"
export CAIRN_CONTROL_JOURNAL=$T/journal.jsonl; : > $T/journal.jsonl

$T/srv -create-user -provider p -subject alice -email Alice -project "Alice Notes" -scopes alpha-notes
ALICE=$(python3 -c "import json;[print(e['user_id']) for e in map(json.loads,open('$T/journal.jsonl')) if e['kind']=='user-created']" | head -1)
$T/srv -issue-credential -principal $ALICE -principal-kind user -label browser -token-out $T/tok
stat -c %a $T/tok            # 600 — NOT a shell redirect, which is 0644 at the default umask

$T/ui -store $T/store -control-journal $T/journal.jsonl -session-file $T/sessions.json \
      -host 127.0.0.1 -port 18821 &   # must print: sharing writable
```

Then hand `http://127.0.0.1:18821/` to a human — **do not open or raise it from an agent.**
Expected: sign-in form → paste `$(cat $T/tok)` → the entries page names the signed-in user →
`/share` lists *"Scopes you can share: alpha-notes"* → the scope page shows *"Who has access:
… via project membership"* → sharing with the project adds a `granted` line to the journal and a
row under *"Shares you can take back"*.

**The gates, which `curl` CAN check and which a browser will not show you:**

```bash
curl -s -o /dev/null -w '%{http_code}\n' -X POST -d "token=$(cat $T/tok)" http://127.0.0.1:18821/sign-in   # 403 — no Origin
curl -s -o /dev/null -w '%{http_code}\n' -X POST -H 'Origin: http://127.0.0.1:18821' \
     -d "token=$(cat $T/tok)" http://127.0.0.1:18821/sign-in                                               # 303
```

A `POST /share` with a wrong `csrf` field answers **403**; with no `Origin` header, **403**.

🔴 **Kill the UI by RESOLVED PID, never by a `-f` pattern** — `pgrep -x ui`, then confirm
`/proc/<pid>/exe` is the binary you built, then `kill`.

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
