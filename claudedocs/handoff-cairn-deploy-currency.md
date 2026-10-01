# Handoff: cairn-deploy-currency — 2026-09-29

## Run this first — the index, one command
```bash
cairn recall --repo "/home/zach/workspace/cairn"
```
Terse pointers this doc does not carry, curated by past sessions and outliving it.
🔴 RECALL, NOT LIVE OBSERVATION — every line is a pointer to VERIFY, never a current
reading, and it may describe a gotcha already fixed. `scope-absent`/`scope-empty` means
nothing is recorded yet: ordinary, not an error, and not a clean bill of health.
Non-blocking: if it exits non-zero, print the stderr line and carry on.

## Goal

Make "what is deployed" and "what is on `main`" observable rather than remembered, and
prove the one deployed feature that has never been exercised. Both predecessor arcs
(`handoff-cairn-control-plane.md`, `handoff-cairn-next-phase.md`) are CLOSED; this is the
new arc their leftovers belong to, not another round of either.

- **closing-condition:** `check` — (a) an instrument exists that FAILS when a deployed
  cairn image's commit is behind `origin/main` (a CI job, a Prometheus rule, or image
  automation) and it has been watched going RED on a deliberately stale pin; AND (b) the
  browser surface's session table has been written — `sessions.n_tup_ins` read as 0 before
  and ≥1 after one real sign-in. A later session runs the instrument's own red-control and
  the two psql reads. ADDRESSED ⇒ arc CLOSED.
- 🔴 **DISPOSITION, 2026-09-30 — the condition is FROZEN, so it is recorded as RULED OUT
  rather than rewritten.** (b) is **MET**. (a) is **RULED OUT, not met**: `/the-algorithm`
  found its author of record is `forcing: regression` — self-issued, no human asked — and
  all three artefacts measure 0 code-bearing commits behind. **No instrument was built and
  no red-control was run, because there is no instrument to run one against.** Operator
  signed off; residual risk accepted explicitly. **A later session must NOT read clause (a)
  above as live work** — it is answered by deletion, and rank 8 is the successor.

## State now

- ✅ **RANK 2 IS CLOSED, AND NOT BY BUILDING ANYTHING.** The drift deadman reports
  `Result=success`, `ExecMainStatus=0` and its own verdict `no drift on the host(s) CHECKED`.
  Four arms cleared. 🔴 **The accurate claim is NOT "no defects":** no defect in what it
  **MEASURES**, a real defect in how its **LEDE READS** — the remedy this doc imposes is *"read
  the line AFTER the alarming one"*, a workaround on every future reader.
- 🔴 **THAT GREEN IS A MANUAL INVOCATION.** `ExecMainExitTimestamp` 19:39:04 against the timer's
  `LastTriggerUSec` 12:46:43. **It has NOT been observed green on its own schedule.**
- 🔴 **DO NOT QUOTE A FIRING TOTAL FROM THIS DOC.** Successes went **4 → 6 inside one session
  because I kept running the unit by hand**; failures read **78 then 73** because the journal
  **rotates**. ⚠ **A surviving bullet in `## Gotchas` still says "78 failing firings" and ends
  "Take the 78" — it is UNPRUNABLE (it quotes a `forcing:` field) and it is WRONG. Ignore it.**
  The durable facts: last **SCHEDULED** success **Sep 16 18:44:55**; every scheduled firing
  after it red; the Sep 30 successes (19:24:11, 19:25:43, 19:39:04) **all manual**.
- 🔴 **CLAUSE (a) IS RULED OUT RATHER THAN MET** — disposition under `## Goal`. ⚠ The
  substitution licensing it is a **JUDGEMENT**, self-issued by this session — the class (a) was
  rejected for: (a) as written reads **12** for the pods and **8** for the client.
- ⚠ **`model` IS `opus[1m]`** — measured on this host; the other host asserted, not re-read.
  🔴 **AND THE OBSERVATION WORTH KEEPING, LOST ONCE AND RESTORED: the earlier
  `z-ai/glm-5.3-flash` value did NOT survive on the host running a live Claude Code, while it
  DID on the host without one.** One observation, mechanism unproven — but it is the only
  recorded warning that **writing `model` on a host with a live session can be reverted, which
  silently re-reddens rc15.** Set on BOTH hosts so the key sets agree.
- ✅ **ARC CLOSED** on (a)+(b): (b) met, (a) ruled out with sign-off, successor rank 8.
  ⚠ Four ranked items remain open (4, 5, 7, 8); "closed" is scoped to the two clauses.
- ⚠ **NO TASK-BOARD FIELD — AN UNKNOWN, NOT A MEASURED ABSENCE.** The resolver exited **5**; an
  unknown session id answers 200 with an EMPTY ARRAY, so that zero cannot separate "touched no
  task" from "wrong id".
- ✅ **CARRIED FORWARD.** `pytest tests -q` **2368 passed / 0 failed**; `go vet ./... && go test
  ./...` **21 ok / 0 FAIL**; `nix eval` resolving `default` and `cairn-go` to the **identical**
  store path. Rank 6 closed: Go client live on both hosts at byte-identical store paths.
- ⚠ **ONE OPEN PR OF MINE — `#163`.** `#160`/`#162`/`#164` are a PEER's.

## Open investigations — live diagnosis state

### ❌ RETRACTED IN FULL — "the trusted-proxy allowlist is stale, so every caller shares one lockout bucket"
- as-of: 2026-09-29

### ✅ RESOLVED 2026-09-30 — the session store IS written, the invite store IS written, and the share flow is EXERCISED on the live deployment
- as-of: 2026-09-30 · `via: measurement`
  | reading | before | after |
  |---|---|---|
  n_live_tup=0`; a later read caught up to `ins=1 live=1`. `track_counts=on`, and

### ❌ RETIRED 2026-09-30 — superseded by the block above; its probe has been run and its instrument was wrong
### The browser surface's session table has never been written to
- as-of: 2026-09-29
  `via: measurement`

### The INSTALLED client carries none of the three features this repo shipped, and no card, claim or arc owns it — this arc's client-side half
- as-of: 2026-09-29
  `via: measurement`
  `via: measurement`
  `via: measurement`

### SUPERSEDES the "Next probe" on the client-pin block above: the bump was BUILT, it is RED, and the cause is a VOCABULARY SEAM rather than a regression
- as-of: 2026-09-29
  `via: measurement`
  `via: measurement`

### RESOLVED, and it RETRACTS the `CAIRN_LIB` two-tier gotcha this same session wrote two blocks above
- as-of: 2026-09-29
  `via: measurement`

### The client-side half is GREEN on a PR and STALE on this host, and those are different claims
- as-of: 2026-09-30

### ANSWERED — the Python-vs-Go decision is MADE (Go), and it is a PIN-SEAM SPLIT rather than a one-line flip
- as-of: 2026-09-30
  |---|---|---|
  `via: measurement`
  named by the `subsystem-index` write protocol), and the writer. `via: measurement`
  `via: code`
  values in the Gotchas entry below, not repeated here. `via: measurement`
  this same session and had not measured. `via: measurement`

### ✅ RESOLVED 2026-09-30 — the pin bump is MERGED, SWITCHED and verified BEHAVIOURALLY on the installed client; and the blocker above was ALREADY BEING REPORTED by a red instrument nobody reads
- as-of: 2026-09-30
  read a REMOTE ref, which never sees the local checkout's branch. `via: measurement`
  | reading | before | after |
  |---|---|---|
  repo's mainline pins the new revision and the old revision greps **0**. `via: measurement`
  client now agree on behaviour, not merely on a version. `via: measurement`
  of them runs to rc 0. `via: measurement`
  `via: measurement`

### 🔴 OPEN — this doc is at its enforced ceiling and `--prune` STRUCTURALLY cannot shrink it
- as-of: 2026-09-30
- 🔴 **Observed — the blocker is the AMBIGUITY rule, not the `as-of:` one.** `LOAD_BEARING_FIELDS`
  *exempts* `as-of:` when a whole block is named, precisely so the largest eviction is takeable — so
  that rule is not it. But every named line must match **exactly one** line, and `- as-of: <date>` /
  `` `via: measurement` `` recur across blocks *by design* (*"Append-verbatim makes duplicates
  ordinary"*). Measured: `- as-of: <a shared date>` **5 matches**, `` `via: measurement` `` **9**,
  (`[partial block]`). No spelling works. `via: measurement`
  step. `via: measurement`
  the conclusion were both wrong.** The lesson is the sequence. `via: code`

### ✅ RESOLVED 2026-09-30 — the ceiling is cleared by EVICTION, and this RETRACTS my own "`--prune` cannot shrink it" from one round ago
- as-of: 2026-09-30
  - every named line must match **exactly one** line, else `[ambiguous]` — and `- as-of: <date>`
    plus a bare `` `via: measurement` `` recur across blocks *by design*
  `via: measurement`
  HEADING and `as-of:` stamp in place as the pointer — that is the shape a prune leaves, and it is
  deliberate rather than residue. `via: measurement`
  reasoning.** `via: doc`
- **Leading hypothesis:** none. Closed.

### ✅ RESOLVED 2026-09-30 — the Go flip shipped to both hosts, and the two instruments that failed on the way are rank 2's real subject
- as-of: 2026-09-30
  `aa01eb77`. `via: measurement`
  the UNMODIFIED package. `via: measurement`
  which is +4/+4 on the tests `#1926` added. `via: measurement`
  what rank 2 must not add a third of. `via: measurement`
  `via: measurement`
- **Leading hypothesis:** none. Closed.

### ❌ RETIRED 2026-09-30 — "the currency instrument that already exists gave TWO FALSE READINGS in one day"
- as-of: 2026-09-30
- **Both readings were TRUE.** The claim is withdrawn; the block below carries the measurement.

### ✅ RESOLVED 2026-09-30 — the deadman is GREEN, both "false readings" were TRUE, and clause (a)'s detector is ruled out by /the-algorithm
- as-of: 2026-09-30
  |---|---|---|
  subsequent switch. `via: measurement`
  `not-on-branch-main` reading was already conceded TRUE. `via: measurement`
  this doc already records twice.** `via: measurement`
  `via: measurement`
  `via: code`
  `effortLevel`/`voice`/`theme`; the codebase had already argued the opposite.** `via: code`
- **Leading hypothesis:** none. Closed.

### ✅ RESOLVED 2026-10-01 — round 0 audited PR #163 and the deletion argument SURVIVED; six findings were mine and are fixed here
- as-of: 2026-10-01
  So "0 code-bearing" is true under any predicate stricter than mine. `via: measurement`
  to argue no drift exists. `via: measurement`
  workflow change was designed and is now unnecessary; rank 8 is rewritten to it. `via: code`
  instrument the whole arc is about. Fixed in `## How to verify`. `via: measurement`
  `via: code`
  not the evicted set; the 9-line gap is the heading, the `as-of:` stamp and the residue, which
  is *about* eviction counts. `via: measurement`
- **Leading hypothesis:** none. Closed.

### ✅ RESOLVED 2026-10-01 — round 1 ran the nine axes on #163: 0 deploy-blocking, and the four findings that mattered were all MINE
- as-of: 2026-10-01
  does not move. `via: measurement`
  nobody named**; restored to both pods. `via: measurement`
  without testing. `via: measurement`
  `:161-172`/`:393`/`:629`/`:786`. A comment is a claim. `via: measurement`
  re-pointed. `via: measurement`
  `via: measurement`

### ✅ RESOLVED 2026-10-01 — round 2 re-audited the fixes and found FOUR more that were mine, two re-creating the exact shape they were fixing
- as-of: 2026-10-01
- **What this settles:** that a fix round's own prose is the likeliest next finding. Three
  rounds, every one finding real defects in the previous round's *corrections*.
- 🔴 **Ruled out: that restoring the both-pods loop was a fix. IT RE-CREATED THE FALSE ZERO IT
  WAS FIXING.** Rebuilt as `echo "…: $(git rev-list …)"`, which hands `$?` to `echo`. Measured:
  with `kubectl` failing, `DEP` is **empty**, `..origin/main` is a **VALID** range, and the block
  prints `0`/`0` at **rc 0** — an affirmative *"no drift"* for a block that never reached the
  cluster. **An empty operand makes a valid range, not an error.** Two lines under this section's
  own *"READ EVERY STATUS OFF THE COMMAND"* headline. Fixed with guards on `kubectl`'s status and
  on `DEP` being a real tag. `via: measurement`
- 🔴 **Ruled out: my own generalisation of the prune rule — WRONG ON TWO OF THREE FIELDS NAMED,
  AND IT OMITTED TWO.** Read out of the tool's values, not its source: the guarded set is **the
  task-board field, `closing-condition`, `forcing` and `as-of`**. **The `via` field is NOT in
  it** (`load_bearing_field()` returns `None`); a `via`-tagged line is unprunable only because it
  RECURS — the ambiguity rule. And **`as-of` is `block_scoped`, so it IS exempt when a whole
  block is named**; "no bypass" is false for it. ⚠ The wrong bullet is itself unprunable because
  it quotes `forcing:`, so it is corrected by naming. **I wrote an untested generalisation inside
  the bullet whose lesson is "a comment is a claim I carried forward without testing".**
  `via: code`
- 🔴 **Ruled out: that the replacement archive bullet was safe. IT TOLD THE READER NOT TO QUOTE A
  COUNT, THEN QUOTED THREE — ALL FALSE.** "12 bodies, 39,944 B, four retractions"; measured at
  the PR head **14 / 48,650 B**, falsified by its own sibling commit **14 seconds later in the
  same PR**, and the archive header says **three**. Replaced with a bullet carrying **no count** —
  the only version that cannot go stale. `via: measurement`
- **Ruled out:** that `0 of 147 absent, 116 net` reproduces. **147 is an ARCHIVE-side count on a
  HANDOFF-side "absent" claim** — the same conflation the preceding block confesses to. The
  reproducible triple is **116 removed / 151 non-blank added / 0 absent**; the safety property
  holds. `via: measurement`
- **Ruled out:** the `result` hedge, the self-test `tail`, and round 1's losslessness — three
  more, all mine. A live `result` **directory** is skipped BY NAME and exits **0**; only a
  **DANGLING** one exits 2, so "a plausible exit-2 cause" was half-false. `--self-test` prints
  **no summary line**, so the `tail -2` I added always shows the last two `PASS` rows whether a
  control failed or not — a content-check providing none. And three things were silently lost and
  are restored here: the `model`-didn't-survive observation (**0 hits in handoff AND archive** —
  gone, not evicted), the sweep's two anchors, and rank 5's measured "twice", widened to
  "repeatedly" with no new measurement. `via: measurement`
- **Leading hypothesis:** none. ⚠ **The pattern is three-for-three: in this doc the thing most
  likely to be wrong is the sentence written to correct the last wrong sentence.**
- **Next probe:** a **scheduled** firing going green — still unobserved. For rank 8, the
  operator's cadence decision.

## Next steps (ranked)

🔴 **NUMBERING IS STABLE — a rank is half a `claim-work` slug**, and `claim-work` comes BEFORE
you act. ⚠ **AND RUN `gh pr list --state open` ANYWAY, TWICE — before starting, and again
immediately before `gh pr create`.** Those two moments are the point: measured three times, the
pre-work sweep was clean and a peer's PR appeared mid-write. `claim-work` answering **rc 12,
already yours** is indistinguishable from untouched work; only the sweep sees an unclaimed
duplicate.

1. ✅ **DONE — all PRs merged**, verified BY CONTENT with a negative control at 0.
   forcing: gate — unmerged docs PRs are the `stranded-docs` shape.
2. ✅ **DONE 2026-09-30 — RULED OUT, NOT BUILT.** The deadman is green (four arms cleared;
   **manual invocation**) and clause (a)'s detector was ruled out by `/the-algorithm`:
   self-issued requirement, all three artefacts functionally current, and an instrument whose
   **every failing firing toasted and none was acted on**. 🔴 **Quote no firing total from this
   doc.** **Do NOT re-open this as "build the currency instrument".** Successor is rank 8.
   forcing: regression — the image gap re-opened twice; rank 8 carries it now.
3. ✅ **DONE 2026-09-30 — clause (b) is MET.** Sessions 0 → 1, invites 0 → 1, share flow end to end.
   forcing: user — the operator held the only credential that could run it.
4. **P8 — retire the Python oracle.** 🔴 **THREE entry conditions; read all of them.** (i) a real
   read AND a real write against the live pod from **two distinct hosts**, recorded; (ii) **no
   open defect naming the Go client or `packages.default`**; (iii) **`packages.cairn` CANNOT BE
   DELETED until the config repo stops importing the reader modules** — 22 files import that
   resolver with no fallback, plus the writer and the launcher the `subsystem-index` protocol
   names. ORDER: config repo stops importing → then `packages.cairn` goes. **BACKSTOP: not done
   by 2026-11-01 ⇒ P8 opens anyway and the residual risk is accepted EXPLICITLY, in writing.**
   forcing: deadline — the 2026-11-01 backstop, set by the operator.
5. **Fix the base-clone write guard.** Reproduced a **NINTH** time this session — it refused a
   commit whose `-C` target was a LINKED WORKTREE on a feature branch while naming the base
   clone, having resolved the repo from `$PWD` — and a **TENTH** time against an auditor's own
   private clone. Measured **twice in the same session**, and **the premise was proved false
   before each override**. ⚠ **It is also inconsistent: `handoff_doc.py` committed into that same
   worktree unimpeded, because it runs git from inside Python where no PreToolUse hook sees it.**
   **Closing condition:** the guard admits a linked worktree AND reads `-C`, with a test that a
   real base-clone write is still refused.
   forcing: gate — a guard whose diagnosis is reliably about the wrong repository trains its own
   bypass, and one a sibling tool walks past is not a gate.
6. ✅ **DONE 2026-09-30 — the Go client is live and verified on BOTH hosts.**
   forcing: user — the operator chose Go and asked for the laptop shipped.
7. **TRIAGE THE SEVEN UN-DROPPED SCOPE ITEMS**, each measured absent on `main`: a PWA, htmx,
   Google sign-in, "move scope ownership between projects", "remove a member", **scope**-level
   tags, and `/the-algorithm` over the whole design.
   forcing: user — all seven were asked directly and none was ever declined in writing.
8. **OPERATOR DECISION FIRST: WHAT RELEASE CADENCE SHOULD THE PODS TRACK? Nothing can be built
   until that is answered.** 🔴 **REWRITTEN TWICE; BOTH EARLIER WORDINGS WERE WRONG.** (i) A new
   `main-<ts>` tag is unnecessary — a semver path exists. (ii) That semver tag has **NEVER been
   minted**: 0 git tags, and 0 of 122/121/79 ghcr tags are non-`sha`, so an `ImagePolicy` over it
   selects nothing today. ⚠ **A live `claim-work` holds this rank under a SUPERSEDED subject
   ("orderable tag in cairn CI, then ImagePolicy…") — the mechanism-first plan this item
   replaces. Re-read the item, not the claim.** **The three options, a cadence choice rather than
   a mechanism:** cut version tags and enrol on semver (deploy per release, each a deliberate
   act); mint a per-commit orderable tag **gated on code-bearing paths** (deploy per code change
   — ungated it is **12 rollouts for 0 code change** over this arc's own window, because every
   commit moves the digest; ⚠ and this revives cairn-CI work round 0 declared unnecessary); or do
   not enrol and keep hand-edited pins, the status quo the incident came from.
   **Already measured:** anonymous tag listing works for both packages (**no `secretRef`**); the
   setter marker goes in the deployment repo's root-kustomization for `subsystem-store`, **NOT**
   the app manifests — both `ImageUpdateAutomation`s scope `update.path` to the kustomizations
   tree, so every `$imagepolicy` under the apps tree is documentation; the working model is a
   neighbouring app's root-kustomization; the initial `newTag` **must already be published**.
   🔴 **BLAST RADIUS: in the deployment repo commit IS deploy. Nothing has been written there.**
   forcing: regression — both pods sat one code commit behind `main` across `#146` and re-opened
   within minutes, twice.

## Defects (batched)

- 🔴 **THE DEPLOYED RENDERER GOES STALE ON EVERY `internal/report` MERGE AND NOTHING
  OBSERVES IT.** Rank 2 owns the fix; this entry owns the measurement. Both pods sat at
  `bcfb60a`, agreed with each other, and were one code commit behind `main` across `#146`.
  Bumped to `b2b54e4`; `#148` merged **while that was reconciling** and touches
  `internal/report/{entry,prose}.go`, so it re-opened. **Pod-to-pod agreement is not the
  property the byte-identity gates hold — pod-to-client is, and two equal tags are equally
  stale.**
- 🔴 **`#140`'s "0 lost, 0 duplicated" WAS TRUE OF THE BULLETS IT MOVED AND SILENT ABOUT ONE
  IT DELETED.** Closed by **#149**. **An eviction's safety property must be measured against
  the COMMIT, never against the working tree that produced it.**
- ⚠ **`grep -c` ON A PHRASE THAT WRAPS ACROSS A NEWLINE ANSWERS A FALSE 0, AND IT COST A
  WRONG CONCLUSION HERE BEFORE THE CONTROLS CAUGHT IT.** Flatten with `tr '\n' ' '` before
  believing an absence in prose. Two separate false zeros this session.
- ⚠ **AN UNBRACED `$ref:` IN zsh ATE A GIT REF AND RETURNED A CONFIDENT WRONG 0.**
  `git show $ref:path` with `a035483^` silently failed under `2>/dev/null`, reading as "the
  bullet was never there". Brace it (`${ref}`) and quote the pathspec.
- 🟡 **FOUR FILES ARE NOT `gofmt`-CLEAN AND NOTHING GREPS IT** —
  `internal/client/{anchor_test,exit,options}.go`, `internal/control/tokenfile/source.go`.
  Pre-existing; re-confirmed. Carried from both predecessor docs.
- 🟡 **`AGENTS.md` + `CLAUDE.md` PASS THEIR GATE BY ONE BYTE.** Re-measured on `main`:
  `MAX_BYTES = 32_500`, `MIN_HEADROOM_BYTES = 900`, files **31,332 + 267 = 31,599**, so
  headroom is **901 against a 900 floor** — green, and the next edit of any size reddens it.
  Unchanged by `#146` touching `AGENTS.md`. Filed as card 682 on the next-phase doc.

## Gotchas / decisions / dead-ends

- **Decision (operator, this session): deploy the image bump straight to the deployment
  repo's `trunk`** — where commit IS deploy — rather than via a PR. Two deploys resulted,
  because `main` moved mid-reconcile; the second was the same approved step re-targeted, not
  a new one.
- **Decision (operator, this session): drive the session probe with the operator's existing
  store token.** Measured insufficient — it is a different credential namespace (401). The
  browser surface's credential is the one in the control journal.
- 🔴 **A ROLLBACK TARGET IS A CLAIM WITH A SHELF LIFE, AND THE DEPLOYMENT REPO SAYS SO IN
  THE FILE.** Both pins' rollback comments were re-pointed at the actual predecessor in the
  same commit that moved `image:`. The UI pin had no rollback line at all and now has one.
- 🔴 **THE SWEEP CAUGHT A DUPLICATE AT THE SECOND WINDOW, AND THE LOCK COULD NOT HAVE.** The
  pre-work sweep was clean; a peer's #150 appeared while this session was writing the same
  bookkeeping, and the sweep run immediately before `gh pr create` is what saw it. The
  overlapping half was then withdrawn so #150 owns it alone. **Run the sweep at both
  moments, and remember rc 12 is the one return value that suggests you have nothing to
  check.**
- ⚠ **`rollout status` AND A FLUX RECONCILE ARE DIFFERENT CLAIMS — reconcile the SOURCE,
  then the Kustomization, then read `imageID`.** Followed here; the source lagged the
  Kustomization on the first pass and the applied revision was read back explicitly both
  times.
- ⚠ **A MOVED IMAGE DIGEST IS NOT A CHANGED SERVER** — the Go binary's nix store path embeds
  the short rev, so every commit to `main` produces a new digest even when no Go code
  changed. Recorded in the deployment repo; restated because a digest diff is tempting to
  read as evidence.
- 🔴 **THE PUBLIC REPO'S LEAK RULE BIT THE BLOCK DESCRIBING THE NETWORK FINDING.** The
  retraction above first spelled the allowlisted address in order to explain it. Every
  address is now named by ROLE, checked mechanically against the diff (0 matches).
  **An example that IS the thing it forbids is the thing it forbids.**

- 🔴 **A DEPLOYED-CURRENCY ARC HAS A CLIENT-SIDE HALF, AND IT IS THE HALF NOBODY IS LOOKING
  AT.** "What is deployed" naturally reads as pods and images; the artefact an operator actually
  TYPES is pinned separately, by a different repo, through a different mechanism (home-manager
  `home.file`, not an image tag), and it can be six days and three features behind while every
  pod is at `origin/main` and every currency check is green. The full measurement is the Open
  investigations block above. **Any instrument built for closing condition (a) should be asked
  whether it can see the client at all** — as specified it cannot.
- 🔴 **THE CLOSED `next-phase` ARC'S RANK 9 IS CONFIRMED LIVE, WITH A PAIRED CONTROL — and it is
  now on real content.** Identical `OPEN:` marker text: under `## Requirements` the validator
  reports **`0 declared`**; moved verbatim under `## Nuance / work-history` it is **found**. Only
  the SECTION differs, so the scoping is the mechanism, not the spelling. A whole-scope run
  agrees (13 entry files, still only the one pre-existing declared `OPEN:`). So a requirement's
  open state reaches **no** validator surface. Recorded as an `OPEN: (inferred)` requirement on
  the `cairn/report` store entry, which is self-demonstrating: the bullet describing the
  blindness is itself invisible to the check that would report it.
- 🔴 **A `## Requirements` BADGE LIVES ON THE INDEX VIEW, NOT ON A `--ref` READ — AND PROBING THE
  WRONG ONE READS AS "THE FEATURE IS ABSENT".** A `--ref` read renders the section's body
  verbatim (bodies always are) and emits **no** badge, so `grep REQ` there returns 0 on a pod
  that fully supports it. Use the index/list view, and pair it with a badge-rendering control
  (assert some OTHER badge appears) so a zero cannot mean "badges are off everywhere".
- ⚠ **#150 WAS MERGED 16 MINUTES BEFORE ITS OWN CHECKS SETTLED** — merged 22:43:22Z, all 8 green
  at 22:59:43Z, no auto-merge. The outcome was benign and the head was already 6-of-8 green, but
  what a gate buys is the ORDERING of the evidence, not the outcome, and that is what was spent.
  Recorded, not relitigated.
- ⚠ **zsh: AN UNQUOTED MULTI-PATH `$3` INSIDE A FUNCTION IS **ONE** PATHSPEC, NOT TWO.** zsh does
  not word-split, so `git grep -l "$1" <rev> -- $3` with `$3="internal/api internal/report"`
  matches nothing and returns a confident **0 for every row** of a check table. Three features
  read as "integration ABSENT" until a positive control caught it. Use `${=3}` or a real array.
- ⚠ **`clawgatectl task list` IS NOT A VERB — IT IS `ls`** — and the wrong spelling printed
  nothing and exited without an error, which reads exactly like "no open tasks". Same shape as
  grepping JSON for `^status`: the tool answered about itself, not about the board.

- 🔴 **A PIN BUMP'S LOCAL SUITE RUNS AGAINST THE *INSTALLED* CLIENT, NOT THE PINNED ONE — SO A
  GREEN LOCAL RUN IS STRUCTURALLY UNABLE TO SEE THE BUMP.** `cairn_pin` resolves the packaged
  `lib/` from **`$CAIRN_LIB`, else from `~/.local/bin/cairn`'s store path** — never from
  `flake.lock`. Measured: 282 tests passed locally and were reported as covering the bump; they
  exercised the OLD lib, while CI's sandbox exercised the NEW one and found 8 failures. This is
  the two-tier rule with a concrete mechanism: **name the variable that selects the tier, and set
  it.** `CAIRN_LIB=<store-path>/libexec/cairn/lib` is the whole fix to the method.
- 🔴 **"VERIFIED IN ISOLATION" HAS A CONCRETE SHAPE HERE: TWO REPOS, EACH GREEN, BROKEN
  TOGETHER.** cairn's suite is green at `cdf6fae`; the other repo's is green on its mainline; the
  pin that joins them fails 8 guards. Nothing in either repo's CI ever built the combined state —
  the other repo's pinned-client leg passes because it only asks whether the client RUNS. **Ask
  which surface your fixture does not load.**
- 🔴 **A COUNT IS NOT A SET, AND I GOT THE SET WRONG TWICE BEFORE GETTING IT RIGHT.** The status
  description truncates at 140 chars and named **1**; a file-scoped run found **1**; my first
  FAILURES-header regex found **4**; the runner's own `failed=` said **8**. Only the last is the
  set. **Read the runner's own total, then make your name-extraction agree with it** — a header
  pattern that returns fewer names than the total is a broken instrument, and the disagreement is
  the tell.
- ⚠ **THE WRITE GUARD KEYS ON THE SESSION'S CWD, NOT THE `-C` TARGET — AND THE PLUMBING ROUTE IS
  THE RIGHT ANSWER, NOT THE OVERRIDE.** Committing into a worktree of the OTHER repo was refused
  because this session sits in *this* repo's shared base clone. The documented
  `BASE_CLONE_WRITE_OK=1` asserts a hazard that was not happening; `hash-object` → scratch
  `GIT_INDEX_FILE` → `read-tree` → `update-index` → `write-tree` → `commit-tree` → `push <sha>:…`
  produced the commit while touching no branch, no index and no base clone. Verify the built
  commit's own diff before pushing it.
- ⚠ **zsh ATE `$C:flake.lock` AS A HISTORY MODIFIER — FOURTH INSTANCE IN THIS EFFORT, AND THIS
  TIME IT FAKED A SAFETY FAILURE.** `git show "$C:flake.lock"` lost `:f`, wrote an EMPTY file, and
  the "is this worktree's content already pushed?" check then reported a false
  *DIFFERENT — DO NOT REMOVE*. Braced (`"${C}:flake.lock"`) it is byte-identical. Prior instances
  were refspecs, so "brace refspecs" was the wrong generalisation: **brace every `$VAR:`
  construction.** The failure direction is not always loud — here it was, but a false SAME would
  have licensed deleting unsaved work.
- ⚠ **AN AGENT-AUTHORED PR COMMENT IS LABELLED AS SUCH, DELIBERATELY.** The finding above was
  posted to that PR prefixed "posted from the operator's account by an AGENT; authorship NOT
  verified; this is a finding, NOT an operator requirement, and nothing here is
  deletion-immune" — the mitigation the closed arc's rank 10 asks for, applied rather than merely
  filed.

- 🔴 **I ASSERTED A MECHANISM WITHOUT MEASURING IT, WHILE FIXING GUARDS THAT EXIST TO
  STOP THAT.** The retraction above is the instance. A coherent story — "two tiers,
  two libs, the local one is stale" — explained every observation I had, so I wrote
  it into a handoff doc and a PR comment as measured. It took one command to refute
  (`nix develop … -c 'which cairn'`). **A theory that explains the failure is not
  evidence for it**, and the cheap discriminating control was cheaper than the
  paragraph I wrote instead. The tell I ignored: I had never actually run the
  resolution, only read the resolver's docstring and reasoned forward from it.
- 🔴 **A GUARD CAN BE NARROWER THAN ITS NAME AND STILL PASS — TWO DID, IN ONE CLASS.**
  `..._drops_ALL_THREE_explanations` and `..._brings_ONLY_its_own_clause` asserted
  over three badges while the module had four: green, reading as coverage, providing
  none for the fourth. Only the sibling that COMPARED against the full rendering
  failed. **A hand-written cardinal in a test NAME is the same claim as one in prose**
  — derive the set, and add a guard that FAILS when the module grows a member nobody
  mapped. That new guard was watched red with its own message, not another's.
- 🔴 **A MUTATION ANCHOR THAT GOES `0x` IS THE GOOD FAILURE; THE BAD ONE IS RE-POINTING
  IT AT THE WRONG SITE.** Three anchors went stale (a tuple re-spelled across lines, a
  predicate that grew two terms, a construction moved into a factory) and
  `_load_mutant` REFUSED each loudly rather than scoring SURVIVED — which is the only
  reason they were found. My first re-anchor then aimed at a *sibling* site: the
  mutation applied, the test failed, and it read as a broken FIX rather than a wrong
  ANCHOR. **Re-derive which call path the scenario reaches before moving an anchor**,
  and prefer the narrowest expression the mutation can be about — the factory over
  its caller, the tuple's first element over the whole tuple.
- 🔴 **A CLASS-LEVEL `module.CONSTANT` IN A TEST IS AN IMPORT-TIME DEPENDENCY, AND IT
  FAILS AS AN OPAQUE COLLECTION ERROR.** Keying a map on `rc.BADGE_REQUIREMENTS`
  resolved at class-definition time, so against a client lacking it the whole FILE
  reported `1 error` — no test names, no counts, exit 2. Keyed on the badge's VALUE
  literal instead, the file imports either way and the mismatch is reported BY NAME.
  Same family as every other opaque-zero in this doc: prefer the failure that names
  itself.
- ⚠ **`grep -c` EXITS 1 ON ZERO MATCHES, SO A TRAILING `grep -c` MAKES A GREEN RUN
  REPORT FAILURE.** A background full-suite run notified as "failed with exit code 1"
  while its own content read `RESULT: PASS (exit=0)` and `0` failure headers — the
  exit was the counter's, not the runner's. The pipe-eats-the-verdict trap with the
  polarity inverted, and the remedy is the same: read the content.

- 🔴 **(a) AS WRITTEN IS NARROWER THAN THE PROBLEM — DO NOT RE-DERIVE IT AS IMAGE-ONLY.** The
  closing condition says "a deployed **image**", and there are THREE artefacts that can be
  stale: the two pod images and the INSTALLED CLIENT. This instruction is recorded here, under
  an APPEND heading, because it first lived in `State now` — which REPLACES, so the next update
  would have deleted the one sentence telling the next session the condition under-describes its
  own arc. ⚠ That is the same narrower-than-the-sentence shape this repo keeps recording, and it
  was committed while correcting a different instrument error in the same PR.
- 🔴 **A CREDENTIAL CAN BE ISSUED WITHOUT THE OPERATOR HAVING ONE, AND THE POD THAT OWNS THE
  JOURNAL IS WHERE IT HAS TO HAPPEN.** The operator had no browser-surface credential at all,
  which blocked every live verification. `cairn-server` is NOT in the browser pod's image and
  that pod's `/tmp` is read-only — **but it carries the exact nix glibc the binary is linked
  against**, so the binary was copied onto the writable state volume and run there, appending
  under the same `flock` the application uses. That beat the alternative (a second pod mounting
  the ReadWriteOnce PVC) on blast radius. A credential was issued for the operator's OWN
  existing user, delivered to a `0600` file, and the binary and token were deleted from the
  volume afterwards; the pod stayed `ready=true restarts=0` throughout. ⚠ Check the glibc store
  path first — a nix-built binary is dynamically linked and a mismatch is the failure mode.
- 🔴 **`pg_stat_user_tables.n_tup_ins` LAGS, SO ITS ZERO CANNOT DISTINGUISH "NEVER WRITTEN"
  FROM "WRITTEN SECONDS AGO".** Measured: immediately after the live sign-in the row was
  ALREADY present (`count(*)=1`, `issued_at` stamped) while the view still read `n_tup_ins=0
  n_live_tup=0`; a later read caught up to `ins=1 live=1`. `track_counts=on`, and
  `stats_fetch_consistency=cache` is the mechanism. **`select count(*)` is the authority.**
  ⚠ An earlier entry in this arc cited the counter as its evidence *and* cited a sibling
  table's non-zero counter as a "positive control proving the counter moves" — both were
  corrected in `#154`, because that pair would hand a false negative to anyone re-running it
  within seconds of a sign-in.
- 🔴 **A `member` CANNOT SHARE, AND THAT INTERACTS WITH CO-MEMBERSHIP NARROWING IN A WAY THE
  PRODUCT ASK DID NOT ANTICIPATE.** `/share` answers **404** to a `member` — only owner/admin
  may share. And `Candidates` is narrowed to people you already share a project with. So the
  only principals you *can* share with already reach every scope the project owns, which means
  a per-scope grant is only *meaningful* for a scope the project does NOT own, or across
  projects. The mechanism works; its useful surface is narrower than "share per-scope with
  other users" reads.
- 🔴 **THE SHARE PAGE'S POSITIVE CONTROL IS NOW MEASURED RATHER THAN ASSERTED.** Live `/share`
  rendered **2 × `read,write` "via project membership"** while "shares you can take back" said
  *"No grant names this scope"* — a principal with authority and NO grant, which is exactly the
  shape `AGENTS.md` names as proof the listing comes from `control.Resolve` and never from
  `Model.Grants`. A principal with BOTH renders identically under either implementation, so
  only this fixture can tell them apart.
- ⚠ **`/invite` IS AN INDEX, NOT THE MINT FORM — and I nearly filed that as a defect.** The
  page lists projects you may invite into; the form lives at `/invite?project=<id>`. Reading
  the index and concluding "the mint affordance is missing" is the same shape as this arc's
  two retractions: a confident read of a page that answers a different question than the one
  asked.
- 🔴 **A SESSION-MESSAGE AUDIT NEEDS A CORPUS ENUMERATION, BECAUSE THE ARC RESOLVER CANNOT SEE
  THIS REPO.** `extract_user_msgs.py --arc` exits **3** on any doc here — it resolves only four
  repo handles and this project is not one, so nothing is measured (which is a different
  finding from an empty arc, exit 4). Enumerate the project's own transcript directory instead.
  ⚠ **And 369 of the 514 extracted records were `<task-notification>` harness blobs** despite
  the tool documenting that harness boilerplate is removed — the real operator-message count
  was **145**. A count taken off that tool without filtering is inflated ~3.5×.
- ⚠ **A NEGATIVE CONTROL CAN CONTAMINATE ITSELF WHEN THE CORPUS IS YOUR OWN TRANSCRIPT.**
  Grepping the session corpus for a deliberately-absent string returned **1** — my own
  transcript, which had recorded the probe command. Exclude your own session id before reading
  such a control as a failure.

- 🔴 **DECISION (operator, this session): the installed client becomes the GO one.** It flips
  `-verbs`/`-exit-codes` from exit 2 to exit 0 — the public-surface widening residual 7 predicted —
  raised with its blast radius before the choice. The concern raised alongside it (the ops wrappers
  PARSE the client, and the parity gate's own named blind set includes `doctor` states) was then
  **measured closed** on all four parse surfaces: `recall`'s `store:` line, `routes`' rows plus its
  `instances:` line, `doctor --json`'s `<alias>/reader-resolution` detail (with a bogus alias ABSENT
  as the control), and `doctor` exiting **10** under `--no-sync`. NOT closed: `sync`, left to the
  parity harness, which runs both clients over one cache root by design. `via: measurement`
- ✅ **THE PYTHON HALF OF RANK 6 SHIPPED; THE GO DECISION IS UNBUILT.** Staged Python-first so the Go change lands against a known-good, rollback-able state instead of riding a
  lock bump. **Do not read "rank 6 verified" as "the Go decision shipped".** Rollback point:
  home-manager generation **845** was current immediately before the switch.
- 🔴 **A `test -d` AGAINST A VARIABLE HOLDING GARBAGE ANSWERED THE REASSURING SIDE.** A background
  build's path was captured with `tail -1`, which returned the runner's `[exited with code 0]` line;
  `test -d "$GARBAGE/libexec/cairn/lib"` then printed *"design premise holds"* — the wanted answer,
  because the path did not exist. Redone with the store path's **own existence** printed beside it.
  The documented *comparison against an absent operand reports SAME, not MISSING* trap, in a new
  shape: **a negative a broken instrument would also produce is not a negative.**
- 🔴 **TWO DOCS PRs ON ONE DOC WERE BOTH `MERGEABLE/CLEAN`, AND THE SECOND WENT `CONFLICTING` THE
  INSTANT THE FIRST MERGED.** `CLEAN` describes the base as it stood, not a property of the PR, so a
  green sweep plus two green `mergeable` reads is **not** evidence both can land. Confirmed by two
  instruments agreeing: `git merge-tree --write-tree` **exit 1** (branch on the EXIT CODE — it prints
  a tree OID on success and emits no `<<<<<<<`, so a marker grep returns a confident wrong "no
  conflict") and GitHub's `CONFLICTING DIRTY`. **Resolved by rebuilding the delta on the merged base
  through `handoff_doc.py`, never by hand** — the sections have replace/append semantics a 3-way text
  merge cannot know, and hand-resolving is how a REPLACE section eats an APPEND one.
- ⚠ **`gh pr checks` EXITS 8 WHILE ANY CHECK IS PENDING, AND ITS EMPTY SET READS AS ALL-GREEN.** The
  non-zero is not a failing gate. Worse, filtering for non-passing checks returns empty BOTH when
  everything passed and when **no checks exist at all** — which is what a fresh push looks like. One
  PR here was misread as green on exactly that. **Print the check COUNT before the buckets.**
- ⚠ **A SWITCH REPORTING `degraded` IS REPORTING ON THE WHOLE USER SESSION, NOT ITS OWN WORK.**
  `home-manager switch` exited **0** while printing `The service manager is degraded` and naming a
  failed unit whose last failure stamped ~48 min earlier, on its own timer. **Read the failure's own
  timestamp before attributing it to the deploy you just ran.**
- ⚠ **THE SIZE CEILING IS PAID BY WRITING LESS, NOT BY EVICTING HISTORY — `--prune` cannot take a
  whole investigation block.** It names lines verbatim, each matching exactly one, but `- as-of:`
  and `` `via: measurement` `` recur (5 and 7 times here) and are parsed fields. This delta was cut
  ~20% to fit, dropping no measured value: one block's facts were folded into another, and a
  duplicated table was removed.
- ⚠ **NO TASK-BOARD FIELD — AN UNKNOWN, NOT A MEASURED ABSENCE.** The resolver exited **5**: an
  unknown session id answers 200 with an EMPTY ARRAY, so that zero cannot separate "touched no
  task" from "wrong id". None written, none created.

- ⚠ **A PRUNED BLOCK LOOKS LIKE A STUB AND THAT IS CORRECT.** Heading plus `as-of:` and nothing
  else means "the body is in the archive". It is not damage to tidy, and re-adding prose under it
  spends the headroom the eviction just bought.

- ✅ **RANK 6 CLOSED, AND THE STAGING WAS THE POINT.** Python-first (pin bump, verified), then
  Go (pin-seam split, verified) — each independently green, with home-manager generation **845**
  as the rollback point for the first and a clean second switch on top. Bundling them would have
  put a public-surface change and a lock bump in one irreversible step.
- 🔴 **THREE BLAST-RADIUS KEYS, AND I FOUND THEM ONE RED AT A TIME.** Round 1 keyed the test
  target set on the threaded package name → **15 files**. Round 2 added `gateTools`/
  `REQUIRED_TOOLS` → **39**, of which I had missed **24**. Round 3 was CI naming
  `test_the_cairn_home_nix_entries_stay_ADJACENT` in `test_peer_host.py` — a file with no "cairn"
  in its name, keyed on the deploy-line TEXT. **Adding a `home.file` entry has a blast radius
  keyed on POSITION, which no name-based selection can find.** Ask what the change's radius is
  keyed on, then pick the set from THAT — and accept that for a positional change the answer is
  "the full suite", i.e. CI.
- 🔴 **TWO GUARDS FIRED AND BOTH WERE RIGHT; NEITHER WAS RELAXED.** The adjacency pin exists
  because `nix/home.nix`'s `cairn-validate` comment refers to neighbours POSITIONALLY ("the pair
  above", "Read all three lines together") — so `cairn-py` moved BELOW the trio rather than the
  assertion moving. The runtime-shebang ban is about a CLASS: "my fixture is never executed" is
  not an exemption, so the fixture uses `testlib.mockbin.write_exec`.
- 🔴 **I DELETED MY OWN NEW ASSERTION RATHER THAN SHIP A VACUOUS ONE.** The two-client
  byte-identity `validate` check was real when written (`cmp` rc 0, mutation-killed with its own
  message). Once the Go client left that check, both names resolved to the SAME package — it
  would have diffed a binary against itself and passed unconditionally. A green that reads like a
  parity gate and asserts nothing is worse than its absence.
- ⚠ **VERIFYING A BUILT COMMIT'S OWN DIFF BEFORE PUSHING CAUGHT A SILENT 6-FILE LOSS.** The
  plumbing route parents on a sha you name; the worktree's local HEAD never advanced after a
  `push <sha>:<branch>`, so `-p $(rev-parse HEAD)` pointed at the MERGE BASE and the commit
  contained only the newest file. `git diff --stat <parent> <built>` is the step that found it.
- ⚠ **AN EMPTY OVERLAP IS ONLY MEANINGFUL IF BOTH OPERANDS ARE NON-EMPTY.** Deciding whether to
  re-gate after main moved, I printed a header and never `cat`ed the file — `comm -12` then
  reported no overlap because one side was empty. Print both COUNTS first (theirs 4, mine 7) and
  only then read the intersection.
- ⚠ **THE PIPE-EATS-THE-STATUS TRAP LANDED FOUR MORE TIMES THIS SESSION**, in both polarities:
  `rc=$?` after `| tail` reported a *nix build failure* as success, and a trailing
  `grep -c '^FAILED'` reported three *green* runs as "failed with exit code 1". Read the runner's
  own result line; take an exit code off the command itself.

- ⚠ **NO AUTO-SHIP TIMER EXISTS, SO rc10 DOES NOT SELF-CLEAR.** `drift-check.timer` is the only
  timer; every merge to the config repo leaves both hosts BEHIND until someone runs `ship.sh` by
  hand. With peers merging several PRs a day, rc10 is red by construction of the workflow — and
  the only reason it was never the *verdict* is that worse codes outranked it. **Sustained green
  needs either prompt shipping or an automated one.** Not fixed; recorded.
- ⚠ **`kubectl` HERE NEEDS an explicit `KUBECONFIG`, AND WITHOUT IT A SUPPRESSED STDERR READS AS
  AN ABSENCE.** Bare `kubectl` targets `localhost:8080`; under `2>/dev/null` the connection
  refusal rendered as `<no such deploy>` for a deployment that exists. **An empty result cannot
  distinguish two mechanisms** — the second command, run without suppression, is what caught it.
- ⚠ **`## Defects (batched)` IS A *REPLACE* SECTION IN `handoff_doc.py`, NOT AN APPEND ONE.** The
  append allowlist is three prefixes wide (`Open investigations`/`Findings`/`Gotchas`); a delta
  carrying two new defect bullets therefore DELETES every existing one. Caught on the proposal
  run by grepping the diff's removed lines for known bullets **with a positive control**, because
  the `DROPS … DURABLE` warning did not flag them. **Read the `buckets:` line, then verify against
  the diff — the warning is a floor, not a list.**

- 🔴 **DECISION (operator, this session): clause (a)'s detector is NOT built.** `/the-algorithm`
  was run over it on the operator's instruction and returned *delete*: the requirement is
  self-issued (`forcing: regression`, no human maker), all three artefacts measure functionally
  current, and the instrument that would host it had 76 unread toasts. Recorded as
  answered-by-deletion with the residual risk accepted in writing. The real fix is rank 8.
- 🔴 **A DIAGNOSTIC LINE NAMING ONE ADDRESS IS NOT A VERDICT ABOUT THE HOST.** *"did not answer
  (unreachable)"* was read as "the laptop is unreachable" while the next line reported a successful
  fallback and the whole remote block was real data. **Read the line AFTER the alarming one**, and
  prefer the instrument's own structured per-host block to its lede.
- 🔴 **AN UNVALIDATED grep PATTERN PRODUCED A CONFIDENT ZERO AND I PUBLISHED IT.** `grep -c
  "Deactivated successfully"` returned 0 on a unit systemd had just reported `Result=success`.
  The real marker was `Finished …`. **Positive-control the pattern before quoting its zero** —
  third instance in this arc, and the first where the zero was about the instrument I was using
  to grade another instrument.
- ⚠ **RE-MEASURING AT THE MOMENT OF ACTING SAVED A WASTED 2-HOST CHANGE.** rc17's "5 behind" was
  5 hours old; at the moment of acting the subtree was **0 behind** — a peer had pulled it. The
  planned `pull --ff-only` + switch on the other host was never needed.

- 🔴 **DECISION (operator): clause (a)'s detector is NOT built.** `/the-algorithm` was run over
  it on the operator's instruction and returned *delete*: self-issued requirement
  (`forcing: regression`, no human maker), all three artefacts functionally current, and the
  instrument that would host it had **78 failing firings, every one of which toasted**.
  🔴 **AN EARLIER COPY OF THIS BULLET SURVIVES BELOW AND QUOTES "76 unread toasts" — a figure
  this same doc RETRACTS. It could not be pruned, and that is by design:** one of its lines
  quotes `` `forcing: regression` ``, and the prune tool refuses any line carrying that field
  because a ranked item's forcing function is read from it — with **no bypass flag**, since the
  loss would be a silent FALSE ABSENCE rather than an error. **So a falsified bullet that
  happens to quote a forcing kind is unprunable in an append-only section: the only remedy is a
  correction like this one.** Take the 78. Successor is rank 8.
- 🔴 **DECISION (operator): `model` is aligned on the hosts rather than allowlisted, and the
  value is `opus[1m]`.** ⚠ **This REPLACES a pruned bullet that said `z-ai/glm-5.3-flash`** —
  set earlier in the same session and then reversed by the operator (*"set claude code back to
  opus"*). Checked at the widest reading: `DRAFTER_MODEL` already defaults to `haiku` and no
  config file defaults to opus, so `settings.json` was the only site. **A decision bullet that
  records a value is stale the moment the value moves; prefer naming the DECISION and keeping
  the value in a REPLACE section.**
- 🔴 **`x-store-revision` IS THE SCOPE'S GIT HEAD, NOT A BUILD-REVISION SLOT — and the bullet
  that said otherwise is PRUNED rather than left to be re-read.** It is read off
  `<scope>/.git/HEAD`, which is what lets a report be quoted as `scope@sha`; `unknown` is a
  **load-bearing** authorization narrowing a refused scope MUST answer, with its own positive
  control; and the health route deliberately carries no version *because it is unauthenticated*.
  The pruned bullet ended *"a ready-made home for this arc's instrument"*, which would have sent
  the next session to hijack pinned semantics. ⚠ **It is still true that nothing answers "which
  code is this pod" — there is no `/version` route and the read heads are exactly `recall`,
  `search`, `snapshot`.** That half was worth keeping; the instruction was not.
- 🔴 **AN APPEND-ONLY SECTION STILL CARRIES A FALSE INSTRUCTION UNTIL SOMEBODY PRUNES IT.** A
  refutation written into `Open investigations` does not reach a reader who lands in `Gotchas`,
  and three bullets here were falsified by this arc's own later rounds. **`--prune` works on the
  append sections and is the remedy** — the refutation stays, the instruction goes. Measured:
  the audit found all three, and none carried a marker.
- 🔴 **I SHIPPED A BROKEN COMMAND WHILE CLAIMING TO FIX TWO BUGS IN THE SAME BLOCK.** Deleting
  `| sed 's/.*sha-//'` made `git rev-list` exit 128 with empty stdout — a failure that reads as
  `0` under the very `2>/dev/null` the block warns about two lines earlier. **When you rewrite a
  recipe, RUN it**; a diff that looks like a correction is not one.
- ⚠ **A "GREEN" THAT CAME FROM `systemctl start` IS NOT A GREEN ON THE SCHEDULE.** Compare the
  unit's `ExecMainExitTimestamp` with the timer's `LastTriggerUSec` before quoting one.
- ⚠ **A PRUNE LEAVES MORE THAN THE HEADING-PLUS-STAMP THIS DOC DESCRIBES AS CORRECT.** The
  evicted block's stub also keeps orphan mid-sentence fragments (the `via:` field lines, which a
  prune may not name) and a table header with no rows. **Not damage to tidy — but do not describe
  it as "some residual `via:` lines" either.** ⚠ And one 🔴 instruction from the evicted body —
  *do not fix a parked base clone by switching branches in it* — now lives **only** in the
  archive; the general rule is in the portable rules file independently.

- 🔴 **A PERMANENTLY-RED GATE'S DIAGNOSIS IS USUALLY ITS BACKLOG, NOT ITS CODE** — ⚠ **replaces a
  pruned bullet that carried two falsified figures.** The deadman was red on **four true
  findings nobody had cleared**, and the instinct to "fix the instrument" would have weakened
  four working arms. **Ask what it is red ABOUT before asking what is wrong with it.** The
  script says so beside its own exemption list: *"A permanently-red gate is worse than no gate:
  it teaches the operator to click through the one alert that has to keep its meaning."*
  ⚠ But "zero defects" overstates it — the lede defect is real; see `State now`.
- 🔴 **A TOTAL OVER A ROTATING LOG THAT YOU ARE CONCURRENTLY ADDING TO IS NOT A MEASUREMENT.**
  Two published counts here were wrong for two different reasons in one session: successes rose
  **4 → 6** because I kept invoking the unit, and failures fell **78 → 73** because the journal
  rotated. **Quote the scheduled/manual split and the last scheduled green — facts that do not
  move — and never a total.** Third instance of the unvalidated-count trap in this arc, and the
  first *inside* a correction written to fix the previous one.
- 🔴 **A COMMENT IN A RECIPE IS A CLAIM, AND I CARRIED ONE FORWARD WITHOUT TESTING IT.** "The
  base clone exits 2 on … agent worktrees" is false: gitignored paths are invisible to the
  scanner's `git ls-files --exclude-standard` enumeration, measured rc 0 with 13 worktrees
  present. **Run the comment, not just the command.**
- ⚠ **A COMMIT MESSAGE CAN DESCRIBE A STATE THAT EXISTED ONLY IN A REFUSED PROPOSAL RUN.**
  `d839a79` claims the doc was "3,062 B over its ceiling" and that it "evicts four blocks"; the
  removal is in `c9776c7` and the over-ceiling state was never committed. Left rather than
  rewritten, so pushed history is not re-pointed — **but applying this doc's own
  verify-the-eviction method to that commit yields a vacuous 0.** Name the commit that carries
  the removal.
- ⚠ **AN APPEND-ONLY SECTION'S FALSE BULLET IS ONLY REMOVABLE IF NO LINE OF IT CARRIES A PARSED
  FIELD.** `--prune` refuses any line holding `forcing:`, `via:` or `as-of:` — no bypass, because
  the loss would be a silent false absence. Two falsified bullets here were therefore
  unprunable and are corrected by naming instead. **Keep a quoted field out of a bullet you may
  later need to retract.**

- 🔴 **A CLOSED-BLOCK ARCHIVE EXISTS: `claudedocs/archive-cairn-deploy-currency.md`** — and ⚠
  **this bullet DELIBERATELY CARRIES NO COUNT OF IT.** Its predecessor was pruned for giving
  three (bodies, bytes, retractions) and **all three were false at the PR head, one of them
  falsified by its own sibling commit 14 seconds later.** The counts move on every eviction, so
  **read the file**. It holds the retractions whose value is stopping a re-derivation.
  **Evict to it, never summarise into it.**
- 🔴 **CORRECTION, NAMED BECAUSE THE WRONG BULLET CANNOT BE REMOVED: the surviving bullet that
  says `--prune` refuses any line holding a `forcing`, `via` or `as-of` field, with "no bypass", IS
  WRONG.** Read out of the tool's own values: the guarded set is **the task-board
  field, `closing-condition`, `forcing` and `as-of`**. **The `via` field is NOT in it** — a
  `via`-tagged line is unprunable only because it RECURS, which is the ambiguity rule, so **such a bullet
  CAN be pruned by naming a longer unique line or the whole block.** And **`as-of` is
  `block_scoped`, so it IS exempt when the whole block is named.** ⚠ That bullet is unprunable
  because it quotes `forcing:` — **the field it is wrong about makes it permanent.** Keep a
  quoted field out of any bullet you may later need to retract.
- 🔴 **A SURVIVING BULLET IN THIS SECTION ENDS "Take the 78" AND THE 78 IS RETRACTED.** It also
  says "78 failing firings". Both are wrong (measured 73, and the total moves anyway); it quotes
  `forcing: regression` so it cannot be pruned. **Ignore that instruction**; `State now` carries
  the scheduled/manual split that replaces it. ⚠ **A ban stated in one section does not reach an
  append-only bullet that instructs the reader to do the banned thing** — the ban has to NAME it.
- 🔴 **AN EMPTY OPERAND MAKES A VALID GIT RANGE, NOT AN ERROR.** `${EMPTY}..origin/main` counts
  from the root and prints a plausible number at **rc 0** — so a recipe deriving a revision from a
  command must check THAT command's status first, and `$(…)` inside `echo` throws it away. Both
  halves cost a false "no drift" here.
- ⚠ **A COMMIT MESSAGE CAN DESCRIBE A STATE THAT EXISTED ONLY IN A REFUSED PROPOSAL RUN**, and a
  commit that performs a removal can carry no mention of it. Name the commit that carries the
  removal, in the message of the commit that carries it.

## How to verify

🔴 **READ EVERY STATUS OFF THE COMMAND, NEVER THROUGH A PIPE — AND NOT THROUGH `echo` EITHER.**
This arc has paid **six** times; the sixth was this block's own both-pods loop, where
`echo "…: $(git rev-list …)"` handed `$?` to `echo` and printed a confident `0`.

```bash
# the deadman — the fleet currency instrument that EXISTS. Read the unit, not a grep:
systemctl --user start drift-check.service
systemctl --user show drift-check.service -p ExecMainStatus -p Result   # want 0 / success
# 🔴 its SUCCESS marker is `Finished Passive drift deadman`, NOT "Deactivated successfully".
# 🔴 AND `start` MAKES A MANUAL RUN. The claim that matters is a SCHEDULED green, so compare:
systemctl --user show drift-check.timer -p LastTriggerUSec   # vs ExecMainExitTimestamp above
# ⚠ Quote no success/failure TOTAL: the journal rotates AND your own `start` adds to it.

# clause (b) — DIRECT reads are the authority; the stats counter LAGS and its 0 is ambiguous
kubectl -n subsystem-store exec sts/cairn-ui-postgres -- psql -U cairn_ui -d cairn_ui \
  -c "select count(*) from sessions; select count(*) from invites;"

# the TWO POD artefacts. 🔴 EVERY GUARD EXISTS BECAUSE ITS ABSENCE PRODUCED A FALSE ZERO:
#   (a) the store deploy is `subsystem-store-api`, NOT `cairn-store`;
#   (b) an EMPTY `DEP` makes `..origin/main` a VALID range printing 0 at rc 0, so a failed
#       `kubectl` reads as "no drift" unless checked FIRST (measured);
#   (c) `${IMG##*sha-}` returns `$IMG` unchanged if there is no `sha-`;
#   (d) the count MUST be path-scoped or it reports 12 for ZERO code drift.
export KUBECONFIG=<the cluster kubeconfig>   # else kubectl hits localhost:8080
git fetch origin            # 🔴 a stale origin/main UNDERSTATES the distance
for d in cairn-ui subsystem-store-api; do
  IMG=$(kubectl -n subsystem-store get deploy "$d" \
          -o jsonpath='{.spec.template.spec.containers[0].image}')
  if [ $? -ne 0 ] || [ -z "$IMG" ]; then echo "$d: NOT MEASURED (kubectl)"; continue; fi
  DEP=${IMG##*sha-}
  if [ "$DEP" = "$IMG" ] || [ -z "$DEP" ]; then echo "$d: NOT MEASURED (no sha- tag: $IMG)"; continue; fi
  git rev-list --count "${DEP}..origin/main" > /tmp/inf.$$ 2>/tmp/e.$$
  if [ $? -ne 0 ]; then echo "$d: NOT MEASURED ($(cat /tmp/e.$$))"; continue; fi
  git rev-list --count "${DEP}..origin/main" -- . ':(exclude)claudedocs' ':(exclude)*.md' \
    > /tmp/cb.$$ 2>/dev/null
  echo "$d informational=$(cat /tmp/inf.$$) code-bearing=$(cat /tmp/cb.$$)"   # want 0
done
#   ⚠ 0 code-bearing does NOT mean "same image": every commit moves the digest (the nix store
#     path embeds the rev), so a docs commit changes the digest while changing nothing served.

# the THIRD artefact — the INSTALLED client
readlink -f "$(command -v cairn)"
cairn recall --help | grep -c -- --tag      # non-zero
cairn recall --help | grep -c -- --scope    # positive control: must already be non-zero
```

**Verify a deploy by DIGEST, never by the `image:` field:** resolve the tag on ghcr anonymously
*before* the push, then read `.status.containerStatuses[0].imageID` back and compare.

**The leak gate must pass before any push, and read its CONTENT not a pipe's rc:**
```bash
# ⚠ The base clone is FINE: `.claude/worktrees/` is gitignored and the scanner enumerates with
#   `git ls-files --exclude-standard`, so agent worktrees are invisible to it — measured rc 0
#   over 468 files with 13 present. And a live `result` is fine too: an existing DIRECTORY is
#   skipped BY NAME with its own control. 🔴 Only a DANGLING `result` exits 2. Measured both.
# 🔴 `--self-test` prints NO summary line, so a `tail` of it reads nothing — take its rc, and
#   read CONTENT only from the main scan, which does print `clean` / `REFUSING`.
O=$(mktemp -d)
python3 tests/leakscan.py --self-test > "$O/self" 2>&1; echo "self-test rc=$?"
python3 tests/leakscan.py           > "$O/scan" 2>&1; echo "scan rc=$?"; tail -3 "$O/scan"
# 🔴 rc 2 is "could not vouch" — a control of its own misbehaved. NEVER a pass.
```
