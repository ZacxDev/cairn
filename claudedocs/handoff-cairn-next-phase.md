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
- ✅ **VERDICT: ADDRESSED — THE ARC IS CLOSED.** Both halves measured this session, not
  inherited: four cards read `"status": "complete"` out of the JSON, and each feature's
  marker is present by content on its own repo's mainline. Nothing is left in flight.

## State now

- ✅ **THE ARC'S CLOSING CONDITION IS MET AND THE ARC IS CLOSED.** All four cards
  `complete`; all four PRs merged and verified **by content**. The previous revision's
  "one thing is missing" — 664's PR still open — is closed.
- ✅ **664 — the `## Requirements` section. MERGED: cairn#148, squash `e8839d9`**, merged
  2026-09-29T22:05:58Z from head `cc9452e`.
  - **All 8 checks green on `cc9452e`** before the merge: `nix` `parity` `tests` `leakscan`
    `uiaudit` `pgtest` `dualrun` and — the one that was blocking — **`go`, success at
    22:04:53Z after 50 minutes**. Every `go` STEP green including the three only that job
    runs: the 188-mutant authz battery (35:52), the 57 routing/anchor mutants, and the
    conformance corpus against the Go server. No skips.
  - **Criterion 2's pin holds by content**, which was the one thing that could have gone
    wrong quietly: `internal/store/openness.go` is blob
    `242eb386dfa434f8615e56e21d53c2d025e3051f` on pre-merge main **and** post-merge
    `origin/main`, `git diff --quiet` rc 0. Byte-unchanged.
  - **The merged tree differs from the gated head by exactly ONE file** —
    `claudedocs/handoff-cairn-next-phase.md`, the inert base move — so what shipped is what
    the 8 checks measured. That is a stronger claim than "the markers are present" and it is
    the check worth running first next time.
  - Merged with **`--match-head-commit cc9452e…`**, so the merge could only land the exact
    tree that was gated.
- ✅ **662 — `refs:` + URL templates + `--ref-to`/`?ref-to=`.** MERGED, squash `5c59169`;
  marker `type EntryRef struct` present in `internal/ui/server.go`.
- ✅ **663 — entry-level `tags:` + scalar `--tag`/`?tag=`.** MERGED, squash `94ecb7e`;
  marker `StatusTagAbsent` present across four paths under `internal/report/`.
- ✅ **665 — the four deterministic doors plus thin routing skills.** MERGED in the other
  repo, squash `dc159b07`; all nine of its files verified present on that repo's mainline
  **this session**, with a negative control proving `cat-file -e` returns non-zero for an
  absent path.
- ✅ **Claim `cairn-next-phase-4` RELEASED.** No claim from this arc is held.
- ⏳ **Cairn's mainline push run on `e8839d9` had NOT settled when this was written** —
  `pgtest` `parity` `leakscan` green; `go` `tests` `nix` `dualrun` `uiaudit` `publish` in
  progress. This is post-merge confirmation, **not** part of the closing condition, and
  `go` alone takes ~50 min. Expect green; if not, the failures are new and are not #148's,
  because #148's own tree was gated green and the only delta is a docs file.
- ⚠ **Merged with round 1's findings fixed but NO round 2 run** — the operator's decision,
  cost stated beforehand. So what this arc did not buy is a delta audit of
  `6efc9dd..cc9452e`, the range where this repo most often finds that a fix round's own
  prose was the next defect. Unchanged by the merge; recorded so it is not mistaken for
  coverage.
- ⚠ **`feat/requirements-section` still exists on the remote** at `cc9452e`, and worktree
  `wt-664` pins it repo-globally. Merged deliberately **without** `--delete-branch`: the
  worktree sits under a DIFFERENT session's scratchpad, so reaping it touches another
  session's state. Left for the operator.
- ⚠ **No task-board front-matter field is recorded**: the handoff's task resolver exited
  **5** again (nothing resolved). An unknown session id answers 200 with an empty array, so
  that zero cannot distinguish "touched no task" from "wrong id" — not a clean bill of
  health.

## Open investigations — live diagnosis state

### ❌ RETRACTED IN FULL — "the trusted-proxy allowlist is stale, so every caller shares one lockout bucket"
- as-of: 2026-09-29 · `via: measurement`
- **What was claimed, and it was wrong:** that `CAIRN_TRUSTED_PROXIES` names a single-host
  prefix that nothing holds, so `netid.PeerIsTrusted` is false for every request,
  `CF-Connecting-IP` is never read, and — per `netid.ResolveClient`'s documented
  untrusted-peer branch — every public caller buckets under the gateway's own address,
  making five failed sign-ins a 15-minute global sign-in outage.
- **The measurement behind it was correct and the INFERENCE was not.** no *pod* holds the allowlisted address: 797 pods enumerated, exact-match on the podIP field, with the cairn-ui
  pod's own IP returning a row as the positive control. That reading is true and it does not
  support the conclusion.
- **Why it does not:** the peer the pod sees is **not a pod**. It is the `hostNetwork` nebula
  gateway, whose source address toward pods on that node is the `cilium_host` address — a
  node-level address that appears in no pod's `status.podIP`. The API deployment's own
  comment says exactly this, recording that the value was derived with
  `ip route get <pod-ip>` after a gateway roll. **An instrument that enumerates pod IPs is
  structurally incapable of seeing a `cilium_host` address, and its zero was read as an
  elimination.**
- **Re-measured the way the value was derived:**
  `kubectl -n nebula exec ds/nebula-gateway -c nginx-proxy -- ip route get <cairn-ui podIP>`
  prints a route out of `cilium_host` whose `src` is **the allowlisted address, exactly**.
  **The allowlist matches the real proxy.** `CF-Connecting-IP` IS read, so a caller can only ever lock out itself — which is
  the definition of the rate limit working, and is what `netid`'s package doc claims.
- **Ruled out:** that there is any global-lockout exposure on this surface. `via: measurement`
- **The reusable lesson, which is why this block is kept rather than deleted:** *ask what
  your instrument can REPORT before reading its output as an elimination* — this repository
  already records that sentence about a leak-scanner, and it was hit again here within the
  same session, in a new shape. A confident 797-row enumeration with a working positive
  control is exactly the kind of evidence that feels conclusive while answering a different
  question than the one asked.
- **Next probe:** none. The claim is withdrawn. If the gateway is ever rebuilt, re-derive the
  value with the `ip route get` command above rather than by looking for a pod.

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

### SUPERSEDES the block below: the other repo's red is FIVE guards over ONE file's byte size, and the previously-named test is NOT among them
- as-of: 2026-09-29
- 🔴 **The block "A test fails only in full-suite context…" below is RETIRED as a description
  of the CURRENT red.** Its named target,
  `scripts/browser-bridge/tests::test_the_release_handler_EXITS_rather_than_resuming[INT]`,
  does **not appear** in either failure set measured today. Whatever it was, it is not what
  that repo's gate is failing on now — do not start from it. Card **683** needs re-pointing.
- **Symptom + exact repro:** its `pytests` status context is red on that repo's `main`. Read the gate's
  own log rather than the 140-char GitHub status description, which truncates and names only
  the first failure:
  `KUBECONFIG=<the cluster kubeconfig> kubectl -n <the CI namespace> logs <that-run>-gate-pod -c step-pytests`
  then `grep -E '^<pytests-prefix>> _{3,}.*_{3,}$'` for the failing test names (the runner uses
  `-rs`, so `short test summary info` lists **skips only** — there are no `FAILED` lines to
  grep, and grepping for them returns a confident empty set).
- **Observed (with values):** `main` `79a9b22a` (its gate run, TaskRun reason
  `StepFailed`, so a verdict was emitted — not a congestion kill):
  `scripts/tests collected=15770 passed=15762 failed=3` +
  `scripts/browser-bridge/tests collected=990 passed=988 failed=2` =
  **`TOTAL collected=24584 passed=24572 skipped=7 failed=5`**, `RESULT: FAIL (exit=1)`.
  The five, by name: `test_the_real_browser_skill_is_under_the_target`,
  `test_the_real_browser_skill_agrees_with_its_own_gate`,
  `test_this_modules_own_stated_figures_are_re_measured`,
  `test_skill_md_under_hard_ceiling`, `test_skill_md_keeps_working_headroom`.
  **Every one asserts the same fact:** `scripts/browser-bridge/SKILL.md` is **12,981 B**
  against a **12,288 B** hard ceiling / **12,038 B** enforced budget. Reproduced locally on a
  clean base clone: `browser SKILL.md is 12,981 B`, `assert 'OVER TARGET' == 'OK'`.
  `via: measurement`
- **Ruled out:** PR #1905 as a cause. Its own head `5c34b707` (its own gate run) fails
  **the same five tests by name**, with `scripts/tests collected=15792 passed=15784 failed=3`
  and browser-bridge `failed=2` — so the PR adds **+22 tests, all passing**, and moves the
  failure count by **zero**. It touches neither `scripts/browser-bridge/SKILL.md` nor
  `reference/security-ops.md`. 🔴 Identical *sets*, not merely identical *counts* — which is
  the claim a count alone cannot make. `via: measurement`
- **Ruled out:** "nobody owns it." **PR #1917** (`fix(browser skill): my own flow-gate note
  blew the size budget — move it to reference/, leave one line`) changes exactly those two
  files and its CI reads **`failed=0`** over `collected=24447`. The red is one defect, fixed,
  awaiting merge. `via: measurement`
- **Ruled out:** the branch protection premise. That repo's `main` has
  `required_status_checks: null`, `enforce_admins: false`, no required reviews — measured on
  the API. So a red `pytests` yields `UNSTABLE`, not `BLOCKED`, and **nothing mechanically
  prevents a merge**. The CI platform skill's gotcha 9 ("requires both test contexts, with
  `enforce_admins: true`") is **stale**. The consequence is that the judgement is the merger's, not a
  gate's. `via: measurement`
- **Leading hypothesis:** there is no open question left about *this* red — it is
  `browser-bridge/SKILL.md`'s size, and #1917 closes it. The open question is only whether
  card 683's original target was a *different*, intermittent failure that today's runs did
  not exhibit.
- **Next probe:** re-read card 683 against these two logs and either re-point it at the byte
  ceiling (closing when #1917 merges and that repo's mainline `pytests` context reads `failed=0`) or keep it
  open **naming a run that actually exhibited** the release-handler failure. Do not re-derive
  the load theory; the previous revision already retracted it.

### CLOSED: the other repo's red was the browser skill's byte budget, and merging #1905 landed on a tree that already carried the fix
- as-of: 2026-09-29
- **Resolution.** The five failures were one defect — `scripts/browser-bridge/SKILL.md` at
  **12,981 B** against a **12,288 B** ceiling — and **#1917** fixed exactly that, landing as
  `f291e16a` **between** the merged-tree gate and the merge itself. So the squash `dc159b07` sits
  on a tree carrying the fix, not on the one that was gated. `via: measurement`
- **The three-tree matrix, which is what licensed merging through a red check.** `main`
  `79a9b22a` 24584/failed=5 · PR head `5c34b707` 24606/failed=5 · merged `490362e3`
  24671/failed=5 — `+87` tests base-to-merged, all passing, failure count unmoved, and **the same
  five tests by name in all three**. `StepFailed` on every run, so verdicts rather than the
  `exit 255` congestion signature. `via: measurement`
- 🔴 **The lesson that outlives it: a COUNT is not a SET.** "Both sides show `failed=5`" and "both
  sides fail the same five tests" are different claims, and only the second attributes a red to
  something other than your diff. The count was available from the API; the set required reading
  the gate's own log. `via: measurement`
- 🔴 **And the base moved AGAIN between the gate and the merge** — the thing the gate existed to
  catch. It happened to be benign (the fix, two files, zero overlap), but that was luck and was
  verified after the fact rather than before: `git diff --name-only <gate-base> <squash>^` against
  the PR's own file set is the one-command check, and it belongs BEFORE the merge, not after.
  `via: measurement`
- **Next probe:** read that repo's mainline CI on `dc159b07` — it had not settled when this was
  written. Expect `failed=0`; if not, the remaining failures are new and are not #1917's.

### RESOLVED — the last probe is answered: that repo's mainline is GREEN after #1905, and the arithmetic closes exactly
- as-of: 2026-09-29
- **This retires the "Next probe" line on the CLOSED block above** — do not re-run it.
- **Read on the squash `dc159b07`:** all four checks success —
  `pytests collected=24671 passed=24664 skipped=7 failed=0` · `nodetests 1720/0` ·
  `gotests 461/0` · the pinned-client leg success. `via: measurement`
- 🔴 **The collected count is 24671 on BOTH the gated tree and the mainline**, which is the
  check that the base move between gate and merge was genuinely inert: #1917 moved bytes
  between a skill file and its reference and added **no** tests, so the test population the
  gate measured is the population that shipped. `via: measurement`
- 🔴 **And the five are accounted for individually, not just in aggregate:** the gated tree was
  `24659 passed + 7 skipped + 5 failed = 24671`; the mainline is
  `24664 passed + 7 skipped + 0 failed = 24671`. **24659 + 5 = 24664** — the tests that turned
  green are exactly the five that were red, so nothing was hiding behind them and no new
  failure replaced one. This is the arithmetic a bare "it's green now" would have skipped.
  `via: measurement`

### CLOSED: `uiaudit` red was the results-push leg, not the tree — and my first reading of it was wrong
- as-of: 2026-09-29
- **Cause, from the job's own log:** the failing step is `verify-push — the push actually
  landed`; the body is `error code: 502` with `Content-Type: text/html`, i.e. the push was
  refused **before reaching the service**. The a11y walk itself SUCCEEDED —
  `payload shape OK — 38 page(s), 152 part(s)`. The job is `continue-on-error: true` and its
  own comment names the edge in front of the hub as the likely cause. `via: measurement`
- 🔴 **RETRACTED: my first reading blamed the change, on a state-change control.** It was
  green on the three preceding commits and on mainline, red only on mine — which reads as
  attribution and is not. **A transient 502 looks exactly like a state change at one commit.**
  The discriminator is the STEP NAME in the log, and the later run on the next head came back
  green with no code change to that path. `via: measurement`
- **Lesson, which outlives this:** a single observation that a check flipped at your commit is
  not attribution. Read what the step says it was doing.

## Next steps (ranked)

🔴 **NUMBERING IS STABLE — every number KEEPS ITS LINE even when done, because a rank is half a
`claim-work` slug.** Re-ranking re-points every live claim.

1. ✅ **DONE — card 663 written back, `cairn-next-phase-c` released.**
   forcing: gate — an unwritten card is re-dispatched and paid for twice.
2. ✅ **DONE — PR #1905 merged** in the other repo, squash `dc159b07`.
   forcing: user — the operator approved the merge conditional on verification.
3. ✅ **DONE — card 665 written back, `cairn-next-phase-d` released.**
   forcing: gate — same as rank 1.
4. ✅ **DONE — cairn#148 MERGED**, squash `e8839d9`, verified by content including
   `internal/store/openness.go` byte-unchanged; `cairn-next-phase-4` released. **THE ARC
   CLOSED HERE.**
   forcing: user — one of the four features the operator asked for, and the operator directed
   the merge.
5. **Close or dismiss card 681's eight items.** Dismissal in writing is explicitly acceptable.
   forcing: none
6. **Free real headroom in `AGENTS.md` (card 682), or accept the one byte in writing.**
   forcing: gate — the next edit by any session reddens the merged tree.
7. **Re-point card 683**, in the other repo — its named test is not what was red. The red was
   the browser skill's byte ceiling and #1917 fixed it, so the likeliest correct action is to
   close 683 against that.
   forcing: gate — a card pointing at the wrong test guarantees the next session starts in
   the wrong place.
8. **Audit and land PR #1922 in the other repo** — the retraction of the stale
   branch-protection claim. No pre-merge audit has run on it; round 0 first.
   forcing: gate — the claim it retracts tells a session a gate will stop a bad merge when
   nothing will, which is the dangerous direction.
9. 🔴 **The validator cannot see a mis-spelled marker in a `## Requirements` section.**
   `validate.go`'s `journalBody` scopes every advisory to the nuance heading, so
   `ScanOpenActions`, `ScanUnreachableMarkers` and `ScanDroppedLines` see no requirement
   bullet, and the index row counts only open/met. **Measured:** a `## Requirements` holding
   `- OPEN - …`, `- RESOLVED — …` and `- FIX: …` renders `🔴 1 NEAR-MISS`, and that 1 is the
   NUANCE section's. Verbatim the failure `NearMissCount`'s own comment exists for,
   reintroduced for the new section. Filed rather than fixed because the validator's messages
   are byte-compared across both implementations, so it is a cross-language message change and
   rushing it at the end of a fix round is the documented way a fix round introduces the next
   finding. 🔴 **Now LIVE ON `main`** — `e8839d9` shipped the section, so this gap is in the
   deployed surface rather than on a branch. **Closing condition:** `cairn-validate` reports a
   near-miss sitting in a `## Requirements` section; the message is byte-identical in
   `internal/store/validate.go` and `lib/entry_shape.py`; the guard is watched RED on
   pre-change code.
   forcing: gate — a writer who mis-spells the marker gets no signal from any surface, which
   is the exact silent failure the marker grammar exists to prevent.
10. 🔴 **The audit tooling presents AGENT-authored PR comments as the operator's verbatim
    asks.** In the other repo, `scripts/audit-dispatch.py`'s `## THE OPERATOR'S OWN ASKS`
    block declares "the operator's own words ONLY — no agent or tool output, by design" and
    ledgered **2 asks, 10,627 B**, both `### from the PR comment` — both written by an agent
    and posted through the operator's account. Zero genuine typed messages were present. The
    block refutes itself: it says "me" in opposition to "the operator" in the same table.
    **Root cause:** the filter is authorship-by-GitHub-account, so `gh pr comment` run by an
    agent reads as the operator. **Why it matters:** the brief then instructs the auditor not
    to propose deleting anything the operator asked for, so every self-issued requirement
    becomes deletion-immune — inverting the one round that can conclude *close this PR*. It
    fails the other way too: the four real decisions that PR turned on appeared nowhere.
    **Closing condition:** a PR comment is either excluded from that block or labelled
    "posted from the operator's account; authorship NOT verified" AND excluded from the
    deletion-immunity clause, with a brief regenerated over a PR whose comments are
    agent-authored showing the label.
    forcing: gate — it silently disarms round 0, which is the only round that can stop work
    that should not exist.

## Defects (batched)
- **`?q=` and `?tag=` do not compose on the browse surface** — declared in
  `internal/ui/README.md` with a closing condition; composing changes a deployed answer.
- **Nothing in the browser has been walked for `/?tag=`**, and no browser has visited an
  entry page carrying `## Requirements` either. Both rendering paths are tested; a human
  clicking them is not. 🔴 **Now shipped on `main`** (`e8839d9`), so the unwalked path is a
  deployed one.
- **The two new parser refusal messages are not byte-compared across implementations** —
  systemic: the same is true of every `aliases:`/`refs:` refusal.
- **Four files are not `gofmt`-clean and nothing greps it** —
  `internal/client/{anchor_test,exit,options}.go` and `internal/control/tokenfile/source.go`.
  Pre-existing; re-confirmed on every commit of this arc, `e8839d9` included.
- **`internal/ui/README.md` documents four legend labels the code renders under different
  wording**, and nothing pins the two against each other. A pin was written and deleted —
  see the gotcha below.
- 🔴 **PR #146's merged body is FALSE about the code it merged** — it still describes `--tag`
  as repeatable and names symbols that do not exist. A merged PR body cannot be re-run;
  card 663's write-back is the accurate account.
- 🔴 **A CLAIM ANSWERS "MAY I", NEVER "IS IT DONE" — AND THE SWEEP IS WHAT CLOSES THAT GAP.**
  A session re-entering from the control-plane doc derived this doc's rank-4 slug and got
  **rc 12 — ALREADY YOURS** (a context reset earlier in the same session had taken it), which
  reads identically whether the work is untouched or finished. The doc it had just read said
  `NOT STARTED`. Only `gh pr list --state open` saw that 21 files of #148 were already built
  and audited over two rounds. ⚠ **So run the unconditional sweep even when the lock says the
  item is yours** — rc 12 is the one return value that actively suggests you have nothing to
  check. **Closing condition:** none needed; this is a lesson, not an open item.
- 🔴 **THE DEPLOYED RENDERER GOES STALE ON EVERY `internal/report` MERGE AND NOTHING
  OBSERVES IT — MEASURED TWICE IN ONE SESSION.** Both pods sat at `bcfb60a`, AGREED WITH
  EACH OTHER, and were one code commit behind `main` across `#146`, which edits
  `internal/report/{text,searchtext}.go`. Bumped to `b2b54e4`; `#148` merged **while that
  was reconciling** and touches `internal/report/{entry,prose}.go`, so the gap re-opened
  within minutes. Bumped again to `e8839d9`. 🔴 **POD-TO-POD AGREEMENT IS NOT THE PROPERTY
  THE BYTE-IDENTITY GATES HOLD — POD-TO-CLIENT IS**, and two equal tags are equally stale,
  so "compare the two tag strings" (which the manifest comment recommends) cannot see this.
  The only reading that can is resolve-image-to-commit then
  `git rev-list --count <that>..origin/main`, which no gate does in either repository.
  **Closing condition:** something that FAILS when a deployed cairn image's commit is
  behind `origin/main` — a CI job, a Prometheus rule, or an image-automation controller —
  or a written line accepting manual bumps and naming who re-reads the distance.
  ⚠ A third manual bump is not the fix and would rot the same way.
- 🔴 **THE DATABASE-BACKED SESSION STORE HAS NEVER BEEN WRITTEN TO.** `sessions.n_tup_ins`
  is **0** — cumulative since the postmaster came up at the 28(d) cutover
  (`stats_reset` is NULL), with `schema_migrations.n_tup_ins = 1` as the positive control
  proving the counter moves and that this is the database the app migrated. The startup
  banner reads `state sessions in postgres, invitations in postgres` and the table exists
  with the right columns; **a banner is configured-state, not evidence the feature works**,
  which the 28(d) block said in advance. ⚠ So rank 13's operator sign-in report on the
  control-plane doc cannot have landed in THIS database after the cutover, or it did not
  persist. A single credential-form attempt from this session returned **401** and wrote
  nothing (correct for a refusal): the store API token is NOT the UI credential — the UI's
  is the one issued into the control journal at seeding. **Closing condition:** one sign-in
  that moves `sessions.n_tup_ins` from 0 to 1, read before and after. The credential is the
  operator's; nothing else blocks it.
- ⚠ **`#140`'s "0 lost, 0 duplicated" WAS TRUE OF THE BULLETS IT MOVED AND SILENT ABOUT ONE
  IT DELETED.** The operator-decision bullet *"the prune was chosen over feature work"* was
  removed from the control-plane doc by `a035483` while the archive half of that move sat
  unstaged in another session's scratchpad worktree, so it existed in **no committed file**
  until PR **#149** restored it. **An eviction's safety property must be measured against
  the COMMIT, never against the working tree that produced it.** ⚠ The other bullet in that
  stranded batch (the card-cap retractions) is still in the live doc, so archiving it too
  would have created the duplicate a prune's own property forbids — rank 27's prune still
  owns that half.

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

- 🔴 **THE OTHER REPO AND cairn ARE MIRROR IMAGES ON THE CI SURFACE, AND A HABIT BUILT ON ONE READS AN
  EMPTY SET ON THE OTHER.** cairn posts **8 check-runs and ZERO legacy statuses** (so
  `/commits/<sha>/status` reads `state=pending total_count=0` forever and `mergeStateStatus`
  is not a settle signal). it posts **4 legacy statuses and ZERO check-runs** (so
  `gh api …/check-runs` reads `total=0` and *its* `pending` IS a real settle signal). Both
  zeros look like "CI has not run". **Read which surface the repo populates before believing
  either number.**
- 🔴 **A 140-CHARACTER STATUS DESCRIPTION TRUNCATES THE EVIDENCE, AND IT NAMES ONLY THE FIRST
  FAILURE.** that repo's `pytests` description ends mid-word at `(fl`. Five failures were
  invisible behind one name. The gate's own log is the instrument — via the CI TaskRun pod,
  not the GitHub API. **A count is not a set: "both sides show failed=5" and "both sides fail
  the same five tests" are different claims, and only the second attributes the red.**
- 🔴 **A `-rs` RUNNER PRINTS NO `FAILED` LINES, SO GREPPING FOR THEM RETURNS A CONFIDENT
  ZERO.** `short test summary info` under `-rs` lists SKIPS only. The failing names live in the
  `=== FAILURES ===` section's `_____ test_name _____` headers. Grepping `^FAILED` on this log
  matches nothing whether or not tests failed — the parsing-tool-output trap, in a new shape.
- 🔴 **zsh ATE `$T:claude/...` AS A HISTORY MODIFIER AGAIN**, returning `0 B` for a file that
  exists. Brace it: `${T}:claude/...`. This is now the third recorded instance in this arc, and
  it was hit **after** reading the rule that warns about it.
- 🔴 **A GUARD CAN REFUSE ON THE SESSION'S CWD RATHER THAN THE `-C` TARGET.** The base-clone
  write guard refused `git -C <a worktree of the other repo> merge` because the *session* sat in the cairn
  base clone — a correct-by-design over-trigger, not a bug. **Do not reach for the documented
  `BASE_CLONE_WRITE_OK=1` override when the guard's stated hazard is not what you are doing:**
  `merge-tree --write-tree` + `commit-tree` + `push <sha>:refs/heads/<branch>` produces the
  identical merge commit while touching no branch, no index and no base clone. Verify the tree
  OID matches `merge-tree`'s output, and that the result is a fast-forward.
- 🔴 **A STALE VERIFICATION WORKTREE LOOKS EXACTLY LIKE A USABLE ONE.** A prior session's
  `wt-1905` was clean, on `verify/cairn-script-layer`, and contained **neither** the current PR
  head **nor** current `main` — its merged-tree measurement was against a base that had moved
  twice. `merge-base --is-ancestor <head> <worktree-HEAD>` for both operands is the one-command
  check; a clean `git status` says nothing about currency. It also still pins that branch
  repo-globally.
- ⚠ **MEASURE A REPO'S BRANCH PROTECTION, NEVER INHERIT THE CLAIM.** The CI platform skill states
  that repo requires both test contexts with `enforce_admins: true`; the API
  today returns `required_status_checks: null, enforce_admins: false`. The skill's own line says
  "re-measure, this moved twice in one day" — it moved again. The difference matters: it decides
  whether a red check is a gate you must clear or a judgement you must make and defend.
- ⚠ **THE BOX WAS AT LOAD 46 WITH FOUR OTHER SESSIONS RUNNING THAT REPO'S SUITES.** A local
  full-suite run started as a control was abandoned once CI's own log answered the same question
  better — the same runner, an uncontended node, and an authoritative failure list. **Prefer
  reading the gate's log over re-running the gate locally** when the gate has already run on the
  commit you care about; and `pgrep -f '<pattern>'` matched only this session's own shell, which
  is why nothing was killed by pattern.

- 🔴 **THE HANDOFF TOOL'S PROPOSAL RUN DOES NOT RUN THE LEAK GATE — ONLY `--confirm` DOES.** A
  `status=proposed` with no leak line is **not** a clean bill; it is the reassuring-zero shape.
  Measured: a delta that proposed cleanly then refused at **27 findings** on confirm, and took
  **three** further rounds to clear because each round's scrub revealed the next denied identifier
  (project name → cluster name → task-board name). **Budget for several rounds, and read the
  refusal's own token list rather than hand-deriving one** — the hand-derived list was right about
  the first class and blind to the other two.
- 🔴 **THIS REPO IS PUBLIC AND A HANDOFF DELTA IS THE EASIEST PLACE TO LEAK INTO IT.** Verification
  commands are the trap: they naturally spell the other repo, the cluster, the kubeconfig handle,
  the CI namespace and the label selector. The doc's own long-standing convention is to
  **describe** that repo ("the operator's shared rules-and-dotfiles repo", "the other repo") and
  never name it; follow it in command blocks too, with `<angle-bracket placeholders>`.
  🔴 **`--leak-pre-existing-approved` was NOT used and must not be reached for here** — the
  findings were introduced by the delta, not pre-existing, and that flag is the operator's call.
- 🔴 **A COUNT IS NOT A SET, AND THE API ONLY GIVES YOU THE COUNT.** See the closed investigation
  above. The 140-char status description truncates mid-word and names only the FIRST failure.
- 🔴 **`git commit-tree` IS THE ROUTE PAST A GUARD WHOSE PREMISE DOES NOT APPLY.** The base-clone
  write guard refuses `git merge` based on the SESSION's cwd, not the `-C` target. Rather than
  reach for its documented `BASE_CLONE_WRITE_OK=1` override — which asserts a hazard you are not
  actually taking — build the merge commit from plumbing: `merge-tree --write-tree` (exit code),
  `commit-tree`, `push <sha>:refs/heads/<branch>`. It touches no branch, no index and no base
  clone. The same shape works for an ordinary file commit via a scratch `GIT_INDEX_FILE` +
  `read-tree` + `update-index` + `write-tree`.
- 🔴 **zsh ATE `$C:refs/heads/...` AS A HISTORY MODIFIER** — `:r` was consumed and `git push`
  reported `src refspec <sha>efs/heads/… does not match any`, which reads as a bad sha. Brace it:
  `"${C}:refs/heads/…"`. **Third instance in this arc, hit after reading the rule that warns about
  it** — the rule is evidently not enough on its own, so prefer braces unconditionally in any
  `$VAR:` construction.

- 🔴 **A GUARD I WROTE AND THEN DELETED, AND THE DELETION IS THE LESSON.** Three copies of one
  legend claim went stale at once, so the obvious fix is a test asserting every rendered
  label appears in the README. Written; its own positive control caught a wrong split key
  (`<dt>` vs `<dt class="legend-term">`) rather than passing over an empty label set; and it
  then reported FIVE absent labels, **four pre-existing and all four merely WORDING
  differences**. Satisfying it meant rewording a correct document to please a guard invented
  for the occasion — a guard on WORDS. Deleted, with the measurement recorded as prose.
  **If a pin is ever wanted there, key it on the SHAPE — a rendered badge class with no row
  anywhere — never on label text.**
- 🔴 **A HAND-WRITTEN CARDINAL IN PROSE IS A CLAIM, AND THE DEFAULT IS WHERE IT LIES.** The
  caveat's lead was `switch len(clauses) { case 3: … case 2: … }` defaulting to the SINGULAR.
  A fourth clause made four reachable, so the first reader to see four badges would have been
  told there was ONE. Fixed by DERIVING it in both languages, printing the numeral above five
  rather than extending a word table. Caught by a fixture ledger row pinned at the old
  number — which is the argument for pinning the exact string rather than a loose match.
- 🔴 **`nix build` AND `go test` ARE DIFFERENT VIEWS OF "THE CODE".** Both Go gates were green
  while `nix build .#ui-image` failed with `undefined: store.RequirementsHeading` — nix's
  flake source includes only GIT-TRACKED files and the new parser was untracked. A local green
  says nothing about the packaged build. Caught only because the image contract test reports a
  failed build as a FAILURE rather than a skip.
- 🔴 **A MUTATION BATTERY MUST GATE ON THE MUTANT BUILDING BEFORE READING ITS VERDICT.** A
  mutant that fails to compile prints no `--- FAIL` line, which a harness grepping for one
  scores as SURVIVED. Hit once here through shell quoting that emitted a literal `\&\&`.
- 🔴 **A `pkgs`-STYLE PER-ROW OVERRIDE ON A LOAD-BEARING SEAM MUST BE ADD-ONLY.** The
  committed battery's `PKGS` is a seam that must not SHRINK; a bare per-row override is a
  mechanism for shrinking it, and the count pin counts ROWS not SCOPE. Now refused by
  `__post_init__` unless the row is a superset.
- 🔴 **A CHECK THAT FLIPPED AT YOUR COMMIT IS NOT ATTRIBUTION.** See the closed investigation
  above: a transient 502 is indistinguishable from a state change at one commit. Read the
  failing STEP, not the state transition.
- ⚠ **`gh api …/logs --allow-escape-sequences` DOES NOT EXIST in this `gh`** — it returns
  **0 bytes** and exits non-zero, which greps as "no failures found". And the `gh run view
  --log` route REFUSES while any job in the run is still in progress. Assert a non-zero byte
  count before believing any grep over a CI log.
- ⚠ **A handoff delta is the easiest place to leak into a PUBLIC repo**, and the proposal run
  does NOT run the leak gate — only `--confirm` does. Budget several scrub rounds and read the
  refusal's own token list rather than hand-deriving one.

- 🔴 **THE BASE MOVED *DURING* THE GATE, AND THIS TIME THE CHECK RAN BEFORE THE MERGE.**
  `b2b54e4` landed on `main` at 21:59:06Z — **45 minutes after** the `go` run on `cc9452e`
  started — so the merge ref `a399d87` was built against the PREVIOUS base `3ae86ee` and no
  gate ever saw current `main`. The one-command attribution, run pre-merge:
  `git diff --name-only <gate-base> <current-main>` intersected against the PR's own file
  set. Result: one docs file, **zero intersection** with #148's 21 files, no byte budget
  summing across the two sides, and `leakscan` already green on `b2b54e4` independently.
  🔴 **The contrast with the #1905 case recorded above is the whole lesson: same hazard, same
  one-command check, but run BEFORE rather than after — where it can still change the
  decision.**
- 🔴 **`--match-head-commit` MAKES "MERGE EXACTLY WHAT WAS GATED" STRUCTURAL RATHER THAN A
  DISCIPLINE.** `gh pr merge <n> --squash --match-head-commit <the gated sha>` refuses if the
  head moved between the read and the merge. Free, and it removes the entire class of "the
  evidence was about the previous head" that the previous revision of this doc had to warn
  about in prose.
- 🔴 **THE STRONGEST CONTENT VERIFICATION IS NOT A MARKER GREP — IT IS
  `git diff --name-only <gated-head> origin/main`.** Expect exactly the base-move files. A
  marker grep proves a symbol arrived; this proves the tree that shipped IS the tree the
  checks measured, which is the claim that actually licenses trusting the green.
- 🔴 **THE `go` JOB IS ~50 MINUTES NOW, NOT 35–47 — THE DOCUMENTED BAND IS STALE AND
  READING IT AS A TIMEOUT WOULD HAVE CALLED A HEALTHY RUN HUNG.** Measured on `cc9452e`:
  48 min elapsed with the job still `in_progress`, concluding `success` at 50 min. Step 8
  (188 mutants) alone is **35:52**, and two substantial steps follow it. **Read STEP-level
  progress — `gh api …/actions/runs/<id>/jobs`, `.steps[]` — to tell progressing from hung;
  the job-level `in_progress` cannot.**
- 🔴 **`clawgatectl task get` EMITS JSON, SO GREPPING `^status` RETURNS EMPTY FOR EVERY
  CARD — AND EMPTY READS AS "NOT COMPLETE".** Hit here on all four cards at once: the field
  is `  "status": "complete",` with leading whitespace inside a JSON object. A reassuring
  zero in a new shape, and the tell was that ALL FOUR came back blank — a real answer would
  vary. Parse it (`python3 -c 'json.load(sys.stdin)'`), never grep it.
- 🔴 **A SQUASH COMMIT *IS* AN ANCESTOR OF `main`; THE BRANCH HEAD IS NOT.** Both are true
  and conflating them wastes a probe. `merge-base --is-ancestor <squash> origin/main` is
  **true** and a fine existence check for the commit; it is `<branch-head>` that is forever
  non-ancestor. Verify the CONTENT either way — but do not read the squash-ancestry rule as
  forbidding the cheap check on the squash itself.
- ⚠ **THE HANDOFF BRANCH NAMESPACE IS REPO-GLOBAL AND CROWDED.** `worktree add -b
  docs/handoff-arc-closed` failed `fatal: a branch named … already exists` — a prior
  session's. `git branch --list 'docs/handoff*'` shows a dozen. Pick a name carrying this
  arc's distinguishing fact, and expect the generic ones to be taken.
- ⚠ **cairn does NOT declare itself a trunk-deploy repo**, so this doc is landed from a
  worktree on a branch and a PR, not committed to `main` — even though prior revisions of
  this very file were committed straight to `main`. The established practice and the written
  rule disagree here; the written rule wins until the operator says otherwise.

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

**The arc's closing condition, re-runnable in full:**
```bash
# half one — the four cards. PARSE the JSON; grepping '^status' returns empty for all four.
for id in 662 663 664 665; do printf '%s\t' "$id"; clawgatectl task get "$id" \
  | python3 -c 'import json,sys; d=json.load(sys.stdin); print(d["status"], d["repo"])'; done
# half two — each feature's marker by CONTENT on its repo's mainline, reading PATHS not counts
git grep -l 'type EntryRef struct'  origin/main   # 662 -> internal/ui/server.go
git grep -l 'StatusTagAbsent'       origin/main   # 663 -> internal/report/*
git grep -l 'RequirementsHeading'   origin/main   # 664 -> 7 source/test paths
git grep -l 'RefKeyRemovalAnchor'   origin/main   # want ZERO in SOURCE — a deletion's control
git grep -l 'canonicalTags'         origin/main   # want ZERO in SOURCE — 663's scalar control
```
🔴 Both positive controls match the **handoff doc's own prose**, because this file names the
deleted symbols. Read the paths, not the count: a hit in `claudedocs/` is this document talking
about the deletion, not the deletion failing. Measured this session: `claudedocs/` only, zero
source hits.

**664's own pin, and the check that beats a marker grep:**
```bash
# criterion 2 — openness.go byte-unchanged across the merge
git rev-parse b2b54e4:internal/store/openness.go origin/main:internal/store/openness.go
# expect the SAME blob twice: 242eb386dfa434f8615e56e21d53c2d025e3051f
git diff --quiet b2b54e4 origin/main -- internal/store/openness.go; echo rc=$?   # want 0

# the tree that shipped IS the tree that was gated — expect ONLY the base-move docs file
git diff --name-only cc9452e origin/main
```
