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

- `main` @ **`ad74fd0`** (#110), and ⚠ **re-read rather than quoting it** — `main` moved twice while this
  session was working (`71041ff` #106, then `ad74fd0` #110, both from other sessions), and the second
  landed *after* this session had already fast-forwarded once. The DoD stands: **ADDRESSED ⇒ the arc
  stays CLOSED.** Run `pytest tests -q` and read **0 failed**; the total is deliberately not quoted
  here (its selection is unstated and the figure is unreproducible).
  ⚠ **THE DoD's EVIDENCE, CARRIED FORWARD BECAUSE THIS SECTION REPLACES:** the authz matrix; the
  share flow with its replica-honesty notice; identity resolving through both backends;
  `packages.default = mkGoClient` (in `flake.nix`'s `packages` block — deliberately **not** a line
  number); and `go test ./...` over the Go packages. Distance from the commit that first met it is a
  COMMAND, never a number: `git rev-list --count 7d7c9ea..origin/main`.
- 🔴 **THE DEPLOY IS DONE, AND THE PREVIOUS `State now` SAYING "STILL NOT STARTED" WAS ALREADY FALSE
  WHEN THIS SESSION READ IT.** Another session built the full manifest set and seeded the journal the
  day before this one, in the deployment-manifest repo, while this doc still said nothing had been
  written there. **Both were re-verified live this session rather than read off the commit messages:** the
  surface is `1/1 Running` with a ClusterIP on the UI port and its own PVC bound; the control journal
  on that volume holds **30 records** — `user-created 1 · project-created 1 · scope-created 26 ·
  member-set 1 · credential-issued 1 · granted 0`, matching the seeding commit's own count exactly.
  ⚠ **The lesson is not "the doc was stale" but that a `State now` bullet asserting a NEGATIVE
  ("not started") is the one shape a reader cannot falsify without going and looking.**
- ✅ **THE LOOPBACK-ONLY GAP THIS DOC NAMED IS CLOSED, AND ONLY FOR ONE OF THE FIVE CONTROLS.** The
  previous entry said every auth control *"was watched failing closed on the same binary over
  LOOPBACK, never off-mesh … that is the gap the precondition names"*. Measured off-mesh this session
  through the public edge: the anonymous refusal answers **401** on the entries route and on the share
  route. 🔴 **THAT IS ONE CONTROL, NOT FIVE** — the Origin pair, the CSRF pair, the `__Host-` cookie
  attributes and the five-failure lockout are still loopback-only readings, and two anonymous GETs say
  nothing about any of them. Do not read the ✅ as retiring the precondition.
- ✅ **THE DEPLOYED IMAGE WAS 10 COMMITS BEHIND AND GITHUB SIGN-IN WAS UNCONFIGURED; BOTH ARE FIXED,
  AND THE LAG IS THE PART WORTH REMEMBERING.** The surface had been serving `sha-899b4cbd…` (#99)
  since it first rolled — an artefact that PREDATES the sign-in flow entirely — while #106 sat merged
  on `main` and the identity provider sat fully configured beside it. Nothing reported the gap: the
  pod was healthy, the route answered, and every gate in both repos was green. **A deployment pinned
  by immutable tag does not drift; it stands still, which reads identically to "current" from
  outside.** Bumped to `sha-71041ff8…` and armed with three variables, in two commits on the
  deployment repo's trunk (`3a64a5764`, then `ce125e9db` correcting a claim the first one made).
- 🔴 **THE THREE SIGN-IN VARIABLES ARE A SET, AND WHICH ONE YOU DELETE DECIDES WHETHER THE POD
  SURVIVES — AN EARLIER DRAFT SAID "DELETING ONE TAKES THE POD DOWN" FOR ALL THREE, AND THAT IS
  FALSE FOR THE REDIRECT URL.** Read off `cmd/cairn-ui/main.go`'s `providerSignIn`, per variable:
  - delete **`CAIRN_SUPABASE_JWKS_URL`** or **`CAIRN_SUPABASE_ISSUER`** while the redirect URL is
    still set → the backend refuses to build and the program **exits 78. Pod DOWN.**
  - delete **`CAIRN_SUPABASE_REDIRECT_URL`** → `redirect == ""` returns `nil, nil` **before** the
    verifier is consulted → **pod UP**, credential form and existing sessions untouched, the button
    simply absent. The function's own comment names this a real configuration, not a mistake.
  🔴 **THE CORRECTION MATTERS UNDER INCIDENT, WHICH IS THE ONLY TIME ANYONE READS THIS.** If the
  GitHub flow misbehaves in production, deleting the redirect URL is the cheapest remedy available —
  button gone, no image roll, nothing else disturbed. The old wording said that path took the pod
  down, which would push an operator to roll the image back instead: a slower, wider change. ⚠ The
  deployment manifest's own comment is NOT wrong in this way — it says "deleting either of the first
  two", so fix the claim here and leave that one alone.
  With none of the three set the surface comes up with the button simply absent, which is not an error
  and was this pod's state until now. The audience variable is deliberately ABSENT: it defaults to the
  value the provider actually issues and the check stays armed, so spelling it out would be a second
  statement of an agreeing value.
- ✅ **ARMED AND VERIFIED ON THE SERVED SURFACE, off-mesh, after the tag went live.** The pod's own
  startup line names the credential form **and** the GitHub button with its callback, and carries **no
  key-set warning**, so the key fetch succeeded. The sign-in page answers 200 and carries the GitHub
  form action; the share route stays 401; the OAuth start refuses **403** both with no Origin and with
  a foreign Origin; the callback with no flight refuses **400** — a refusal, not a 500, which is the
  shape that would mean an unguarded handler.
- ⏳ **WHAT IS STILL NOT VERIFIED IS A *COMPLETED* SIGN-IN, AND EVERY READING ABOVE IS A REFUSAL OR AN
  UNAUTHENTICATED PAGE.** Nobody has carried a GitHub identity through the provider and back to a
  session on this deployment. That is a human's step and this list must not be read as standing in for
  it — it is rank 13.
- ⚠ **CARRIED FORWARD — RANK 10 (the publish ordering) IS DONE**, merged as `901b77d` (#104) and
  verified on the real publish run rather than on the PR: the merge triggered the first execution of
  the new order, with the Go half executing wholly before the Python half. Two things the agent got
  right against its brief are worth keeping: the brief's `first_go < last_python` is `min < max` and
  is satisfied by a fully INTERLEAVED job, so the shipped assertion is the true mirror; and the skopeo
  pin and the registry login sat INSIDE the Python half, so moving the Go half in front without
  hoisting them would have run the Go pushes with no binary and no credential. **Ask what the half you
  are moving was silently borrowing from the other one.**
- ⚠ **CARRIED FORWARD — A REORDER'S REAL BLAST RADIUS IS `steps.<id>.outputs`, AND NO TEST COVERS
  IT.** Verified by hand on #104: 43 references, all resolving to a strictly earlier step, reported as
  a PAIR because the checker was first proved able to go red (relocating the first step yields 24
  violations). The second cheap structural check worth copying: the workflow's non-comment, non-blank
  lines are an identical multiset before and after, which is what proves a pure reorder disturbed no
  `run:` body.
- ⏳ **CARRIED FORWARD — RANK 11 IS OPEN AS THE HANDOFF-TOOLING REPO'S #1867, UNMERGED ON PURPOSE**,
  `MERGEABLE`, its closing condition met and watched (a commit message carrying a denied identifier is
  refused at rc 13). It is held because that repo's `main` is red for an unrelated reason, below, and
  merging would bake that red into main's history. 🔴 **Its leak gate's scope is WIDER THAN THE
  COMMIT: it scans the whole `--advanced` value, not just the committed subject**, because
  `splitlines()[0]` and `[:100]` each drop operator text that still reaches stdout. Know that before
  reading a refusal as "this would have leaked".
- 🔴 **CARRIED FORWARD — THE HANDOFF-TOOLING REPO'S `main` IS RED ON ITS TEST LEG, CAUSED BY ITS OWN
  CAIRN PIN BUMP, AND THIS IS THE CROSS-REPO COUPLING NOBODY HAD WRITTEN DOWN.** Its
  `test_kills_the_readme_exclusion` mutates a pinned anchor, and cairn consolidated its open-coded
  README exclusion into one `is_entry_filename` predicate, so the anchor count went **1 → 0**. **A
  cairn consolidation can redden that repo, and no gate on either side says so.** Discriminated rather
  than assumed: the genuine-failure signature, with CI and local producing identical collected/passed/
  failed totals — identical totals across two environments is what rules out load.
- 🔴 **CARRIED FORWARD — THAT REPO'S BRANCH PROTECTION NO LONGER REQUIRES STATUS CHECKS**
  (`required_status_checks` returns 404), so two files asserting a red leg BLOCKS the merge are wrong
  in the permissive direction. ⚠ Do not restate either version — measure it. That is rank 12.
- ✅ **CARRIED FORWARD:** the UI image is published and anonymously pullable, measured twice with an
  absent-tag negative control each time; both publish controls ran for real; the Go pod is the
  DEPLOYED pod and both pods publish to two ghcr packages under one `sha-<40-hex>` scheme.
- 🔴 **CARRIED FORWARD — `cairn-control-plane-9` IS STILL HELD ON PURPOSE** and the browser step is
  still a human's. ⚠ Its handover is stale again by the mechanism this doc already records — the
  world, the token and the pid are session-scoped. **Rebuild the instance before handing any URL
  over**, and note that a long-lived instance from an earlier session was observed still running on
  the UI port this session; check who holds that port before binding it.

## Next steps (ranked)

🔴 **NUMBERING IS STABLE — ranks 1–12 keep their meaning; 13 is new.** Rank is half a `claim-work`
slug, and this doc has twice measured a shuffle re-pointing live claims.

1. ✅ **DONE — #74 merged as `93d0f03`.** forcing: gate.
2. ✅ **DONE — merged as `562a4f6f`.** forcing: gate.
3. ✅ **DONE — P7 merged as `c0f5b06`.** forcing: none.
4. **P8 — retire the Python oracle.** **Closing condition:** P8 opens when BOTH (a) the Go client
   has completed a real read AND a real write against the live pod from **at least two distinct
   hosts**, recorded — the POSITIVE CONTROL separating *held up* from *never ran*; and (b) no open
   defect names the Go client or `packages.default`. **BACKSTOP: if (a) has not happened by
   2026-11-01, P8 opens anyway and the residual risk is accepted EXPLICITLY, in writing.** Checked
   by `cairn doctor` output from two hosts plus `gh issue list`. ⚠ Sized, not measured: ~33,000
   deletable lines, 3 of 6 CI jobs, ~233 KB of prose, ~10 paired-ledger guards.
   forcing: none
5. ✅ **DONE — `cairn-server -issue-credential`, in #76.** forcing: gate.
6. ✅ **DONE.** forcing: user.
7. ✅ **DONE — rename landed as `56cc56e` (#69).** forcing: user.
8. ✅ **DONE — rule (o) merged as `c4490f07` in the handoff-tooling repo.** forcing: incident.
9. ⏳ **IN FLIGHT: the share flow's human verification.** Everything a `curl` can reach is green and
   the write half is **unconsumed**. What is left is genuinely a human's: JavaScript, layout, focus
   order, and whether the replica-honesty notice is actually READ. ⚠ **The previous handover is dead
   — rebuild the instance and re-read the token file; do not reuse a pasted value, and check who
   holds the UI port before binding it** (an instance from an earlier session was still holding it
   this session).
   forcing: user — the operator reserved the browser step to a human.
10. ✅ **DONE — merged as `901b77d` (#104) and verified on a real publish run.** The operator decided
    **reverse it, Go first**; the inverted rationale is rewritten at all eleven sites.
    forcing: user — an operator decision #85 declined to take.
11. ⏳ **OPEN AS the handoff-tooling repo's #1867, UNMERGED ON PURPOSE.** The closing condition is
    **met and watched**. Held because that repo's `main` is red for an unrelated reason (see
    `State now`) and merging would bake that red into main's history. **Closing condition for the
    MERGE:** its `test_kills_the_readme_exclusion` anchor retargeted to the consolidated
    `is_entry_filename` shape, its `pytests` check exiting 0 at that repo's main, then #1867 merged.
    forcing: incident — a denied identifier is on public `main` in a commit message today.
12. **CORRECT TWO FILES THAT ASSERT THE HANDOFF-TOOLING REPO'S CI CHECKS BLOCK A MERGE.** They do
    not: `required_status_checks` returns 404. Both sites are wrong in the PERMISSIVE direction,
    which is the dangerous one. 🔴 **THE TWO SITES, RESTORED — an earlier draft of this item
    compressed the names out, leaving a gate-forced closing condition nobody could check: with
    "two files" unnamed, a session cannot know it found BOTH.** They are **`scripts/run-tests.sh`'s
    own comment** and the **CI-platform skill's gotcha #9**, both in the handoff-tooling repo.
    Neither is a denied identifier — they are ordinary tool paths that have always been in this
    document and pass `leakscan` — so there was never a de-identification reason to drop them.
    **Closing condition:** both state the measured value with the command
    that re-measures it, or say the protection was deliberately removed and by whom.
    forcing: gate — a comment is a claim, and this one licenses merging through a red gate.
13. **NEW — COMPLETE A GITHUB SIGN-IN END TO END ON THE DEPLOYED SURFACE.** Everything a `curl` can
    reach is verified (see `State now`); what is unproven is that a real identity traverses the
    provider and lands as a session. **Closing condition:** a human opens the surface's public
    hostname in a browser, completes the GitHub flow, and the entries page renders for the seeded
    operator user — or the failure is recorded with the pod's log line for it.
    🔴 **RANK 9 IS NOT PART OF THIS SITTING, AND AN EARLIER DRAFT OF THIS ITEM SAID IT WAS.** It
    read *"Do the rank-9 share-flow check in the SAME sitting: both are browser work on the same
    deployment"* — **false**, and it sent a reader to a world where rank 9 cannot be done at all.
    Measured on the deployed journal: **1 `user-created`, 1 project, 1 `member-set`.** Rank 9
    verifies the SHARE FLOW, whose `Candidates` list is narrowed by CO-MEMBERSHIP, so one person
    means an EMPTY candidate select and nobody to share with. **Rank 9 needs the hand-run recipe
    under `## How to verify`** (which builds two users and joins them), or a second co-member
    provisioned in-cluster first. Do not fold the two.
    ⚠ **PRECONDITION NOBODY HAS RECORDED, AND ITS FAILURE IS DELIBERATELY UNREADABLE.** Sign-in
    resolves the exchanged token's `sub` against a user the control plane ALREADY holds
    (`UserByProviderSubject`); an unknown subject is REFUSED, never provisioned — that refusal is
    the anti-self-serve-signup guard, not a bug. The journal's `provider` is `supabase`, which
    matches what the backend resolves against; whether its one `subject` equals the `sub` the
    provider will actually issue for the operator's GitHub identity is **NOT recorded anywhere and
    was not re-verified here**. 🔴 On a mismatch the browser gets the SAME generic 401 as every
    other sign-in failure — `internal/ui/oauth.go` says the three distinct causes are collapsed on
    purpose and the REASON goes to the pod's log. **So read the pod's log on any failure; the page
    cannot tell you which of the three happened.**
    forcing: user — the operator reserved the browser step to a human.

## Defects (batched)
- ⚠ **CLOSED ENTRIES MOVE TO THE ARCHIVE, THEY DO NOT ACCUMULATE HERE.** This section
  REPLACES, so every closed entry left in it is retyped by hand each round until somebody
  drops it — which is how one sat 🔴 and 46,831 B wrong for two merges. The LESSONS from a
  closed entry belong under `Gotchas`, which appends; the entry itself belongs in the archive.
  ⚠ **#74's ✅ entry was DROPPED by this update rather than retyped** — its lessons are already
  under `Gotchas` in their own bullets, which is the condition for dropping one.
- 🟡 **FOUR OF ELEVEN JOURNAL EVENT KINDS HAVE NO WRITER, AND THE SELECTION RULE IS STATED SO
  THE NUMBER IS REPRODUCIBLE.** 🔴 **The previous two spellings of this entry were both wrong in
  the same way — they listed more names than the headline counted, because each round
  DECREMENTED the figure instead of recounting.** The rule: a kind has a writer iff non-test Go
  constructs it. Re-derive with
  `find . -name '*.go' -not -name '*_test.go' -not -path './.claude/*' -print0 | xargs -0 grep -ohE 'Kind:\s*(control\.)?Event[A-Za-z]+'`
  against `control.AllEventKinds` (11). **Constructed (7):** `user-created`, `project-created`,
  `scope-created`, `member-set`, `credential-issued`, `granted`, `grant-revoked`.
  **Writer-less (4):** `member-removed`, `scope-renamed`, `scope-moved`, `credential-revoked`.
  ⚠ `granted`/`grant-revoked` are constructed only by the browser surface; `tokenfile/source.go`
  also builds an `EventGranted` but as an in-memory projection, never a journal append — so
  *"the surface is their only writer"* stands. ✅ **`member-set` — the one that bit — closed by
  #86 (`d6fe1c3`), retired by measurement**: the rank-9 world was built with `-set-member` and
  `blake@example.invalid (user)` appeared in the share flow's candidate select, which is the
  state the hand-append existed to fake. ⚠ Hand-appending is the shape that let a 64-character
  secret into the journal (rank 5), and `-issue-credential`'s own output still says *"append a
  `credential-revoked` record BY HAND — nothing in this repository writes that event yet"*.
  **Closing condition:** a writer for `credential-revoked`, or a written line saying hand-append
  is the intended interface and naming where its schema is documented.
- 🔴 **`leakscan` EXITS 2 ON ANY UNTRACKED *DIRECTORY-LIKE* PATH IN ITS OWN ENUMERATION — THE
  CLASS IS WIDER THAN THE AGENT WORKTREE IT WAS FILED FOR, AND THIS ARC'S OWN BUILD ARTEFACTS
  ARE IN IT.** `git ls-files --others` yields the path and the scanner reads it:
  `[Errno 21] Is a directory` → exit 2, "could not vouch". Two instances measured:
  `.claude/worktrees/agent-…/` (a live sibling worktree, **not yours to remove**) and
  **`result`/`result-1`, nix gcroot symlinks pointing at store DIRECTORIES**, which a previous
  entry explicitly acquitted — *"leakscan reads them without complaint"* — and which were in
  fact the only cause named at the time it was re-measured. 🔴 **It is not cosmetic: the same
  enumeration puts EIGHT of this repo's own tests red** (`test_leakscan_covers_every_tracked_file.py`,
  `test_no_scrubbed_identifiers.py`), which nothing recorded, and `handoff_doc.py` now refuses a
  handoff write on ANY non-zero exit — so an operator override gets spent on a scanner artefact,
  which is how an override becomes reflexive. **Workaround that costs nothing:** build with
  `nix build --out-link <scratchpad>/…` so no gcroot lands in the repo root.
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
- 🟡 **COUNTS QUOTED IN PROSE THAT NOTHING ASSERTS ON** — now **`tests/test_parity_harness.py`'s
  floor alone**. ⚠ `lib/README.md`'s counts are closed twice over: the echo-site count by
  `tests/test_narrowing_echo_sites.py`, and the `STORE_IS_PER_HOST` total by `562a4f6f`, which
  **deleted** it rather than refreshing it — the same ruling #54 took for `README.md`'s
  101/102/70/23/8. The pattern is now settled for this repo and worth stating once: a count that
  moves whenever an artefact is regenerated gets DELETED, and one that should never move gets
  PINNED (`tests/test_control_mutant_count_is_pinned.py`). Deciding which a number is, is the work.
  **Closing condition:** a decision to pin the parity floor or a written line saying why not.
- 🔴 **A CHANGELOG ROW IS BORN WITH THE WRONG ANCHOR, STRUCTURALLY, AND IT HAS NOW HAPPENED
  TWICE IN ONE ARC.** `CHANGELOG.md`'s header states the anchor exists so
  `git log <your-rev>..<target-rev>` answers *"does this change sit between the revision I am
  pinned to and the one I am moving to"*. That anchor is the SQUASH commit — which cannot be
  known while the PR carrying the row is open. So every row is written wrong and is only
  correctable by a follow-up after the merge that makes it true. #69 shipped anchored to
  `4d1787d`, a branch commit the squash discarded, corrected in #78; #90 shipped a visible
  `<UNFILLED>` placeholder — the honest choice, a wrong sha being worse than a hole — corrected
  in #92. ⚠ **Nothing gates it**: a row with a placeholder or a dead sha is green in every
  suite. **Closing condition:** a check that refuses a `CHANGELOG.md` anchor which is not an
  ancestor-free commit present on `main`, or a written line saying the two-step is accepted.
- 🟡 **P7's TWO BINDING CLAIMS ARE NOT IN `AGENTS.md`, AND THE REASON IS THE BUDGET.** The
  conditional-sync work (`c0f5b06`) put its record in four READMEs because `AGENTS.md` has
  **73 bytes** free against its enforced ceiling, and that file's own rule forbids paying for
  a new claim by deleting an existing one. The two that belong there: the validator is a digest
  of the **uncompressed** tar (not gzip, not an authorization epoch), and a `304` is a FOURTH
  read state beside `live`/`cached`/`scope-empty`/`store-unreachable`. **Closing condition:** an
  eviction PR frees ≥400 B, then both sentences land and
  `tests/test_agent_instructions_weight.py` exits 0 with both present.
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
- 🔴 **A CONTROL THAT SETTLED "IS THE TREE DIRTY" WAS READ AS SETTLING "WHICH ARTEFACT DID IT",
  AND THE INSTRUMENT COULD NOT ANSWER THE SECOND QUESTION AT ALL.** ❌ **RETRACTED:** *"the
  scanner's single `COULD NOT READ` line named the worktree directory … the rival mechanism was
  named before concluding."* The base clone exited 2 with untracked `result` symlinks **and** a
  live agent worktree both present. The fresh-worktree scan (**rc 0**, same tree) is a valid
  control for *"the tracked tree is clean"* — and for nothing else. 🔴 **`tests/leakscan.py`
  `raise SystemExit(2)` on the FIRST `OSError`, and `git ls-files` is lexicographic, so
  `.claude/worktrees/…` is reached before `result` every time.** The scanner can therefore name
  exactly **one** artefact no matter how many are unreadable, and naming the worktree was
  evidence about **ordering**, not about `result` — which was separately measured to be a cause
  the moment the worktree was gone. **Ask what your instrument is capable of REPORTING before
  reading its output as an elimination.**
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

- 🔴 **A `STILL LIVE` INVESTIGATION BLOCK IS A CLAIM WITH NO EXPIRY, AND THIS DOC CARRIED ONE TWO
  MERGES PAST ITS FIX.** The host-HOME doctor-test block was re-measured as live by a
  later session, after `c47636b` (#60) had already applied the exact fix its own `Next probe` prescribed. The
  section APPENDS, so nothing deletes a stale block and a second session re-measuring the symptom
  reads as diligence rather than as a missed closure. ⚠ **The cheap check is one command and neither
  session ran it**: `git log <the block's as-of ref>..origin/main -- <the paths the block names>`.
  🔴 **And the tell that it was a real closure rather than a flake is that the blamed CONDITION was
  still present** — same host, same `~/.config/subsystem-store/instances`, same mtime — so "it did
  not reproduce" and "it was fixed" were distinguishable, and only by checking.

- 🔴 **A CLOSED BLOCK MOVED TO THE ARCHIVE IS INVISIBLE TO A SWEEP THAT READS THE TREE'S CURRENT
  PROSE — AND THAT COST A FALSE SECURITY CLAIM.** #85 found the busybox/setuid deferral, saw its
  trigger had fired, and recorded the cutover as having INVERTED the applet/credential comparison
  *"in the direction that reads as reassurance"* — asserting a risk INCREASE nobody had measured.
  The measurement already existed in `claudedocs/handoff-cairn-control-plane-archive.md`'s CLOSED
  block: `busybox --list` is **402 applets on BOTH nix images, zero difference**, and the image
  actually replaced carried **11 setuid binaries and TWO interpreters** (CPython 3.12 + Perl)
  against **zero and zero**. A **NARROWING on both axes.** **Before recording a deferral as
  spent, search the ARCHIVE for its discharge.**
- 🔴 **A DEFERRAL WHOSE TRIGGER IS A STATE NOTHING ASSERTS ON IS A DECISION WITH A DELAY ON IT.**
  Two found spent by one unrelated sweep — the busybox trade and `tests/dualrun/README.md`'s
  *"Revisit if the Go pod is ever the deployed one"*. Neither sentence had a watcher; both were
  found only because somebody happened to read the paragraph.
- 🔴 **RETRACTING A TRUE CLAIM IS THE SAME DEFECT AS LEAVING A FALSE ONE, POINTED THE OTHER WAY —
  AND #85 DID IT TWICE.** Once claiming `flake.nix` *"never said"* the Go image was undeployed
  (it did, at the `server-image-go` block, a site the same commit had missed while editing that
  file); once retracting *"neither had anything watching its condition"*, which was true.
- 🔴 **A CORRECTION APPLIED AT SOME OF ITS SITES READS COMPLETE AT WHICHEVER ONE YOU LAND ON.**
  The dominant failure across #85's four audit rounds. Measured: the publish-ordering rationale
  had **SIX** sites; round 1 fixed three and reported it done; round 2 found the other three —
  one of them the copy a *"See the 🔴 at the top of this file"* pointer routes readers to, in a
  file round 1 had edited four lines below that pointer.
- 🔴 **A CLAIM SCOPED TO "THIS COMMIT" STOPS BEING READ THAT WAY THE MOMENT THE COMMIT IS NOT THE
  NEWEST.** `cmd/cairn-server/main.go` read *"IT IS NOT DEPLOYED BY THIS COMMIT"* — true of its
  own commit, read by everyone afterwards as "this is not deployed". **Scope a claim to a STATE.**
- 🔴 **A GUARD'S STATED PREMISE CAN DIE WHILE ITS ASSERTION STAYS CORRECT — ONLY THE PROSE TELLS
  YOU.** `test_flake_go_image_runtime_contract.py`'s *"Nothing has ever published
  `server-image-go`"*, and `test_publish_workflow.py`'s ordering rationale, whose **both** legs
  are spent. The first guard's line is still worth keeping; the second now enforces the opposite
  of what it argues for (rank 10).
- 🔴 **LINE-ANCHORED `grep` FAILED FOUR TIMES ON ONE QUESTION.** The claims WRAP — across lines
  AND across comment leaders. Normalise both before sweeping, and prove the sweep with a positive
  control you watched HIT a known wrapped instance. ⚠ **And editing a wrapped claim MOVES the
  wrap**, so a later sweep finds what an earlier one could not — that is not a fix.
- 🔴 **A NOTICE THAT LICENSES WORK IS ITSELF WORK: RETIRE IT IN THE COMMIT THAT DOES THE WORK.**
  #81 recorded that the tree was deliberately left inconsistent pending an operator decision;
  #85 swept it on that decision and left the notice standing — while `AGENTS.md`, paid by every
  session, points at that exact block.
- 🔴 **A COUNT WITHOUT ITS SELECTION IS NOT REPRODUCIBLE.** *"82 passed"* appeared in three
  commit messages before an auditor tried to reproduce it and got 68 / 85 / 2042 depending on the
  file set. The selection is now named. Same defect class this repo already tracks for prose
  counts, committed in commit messages instead.
- ⚠ **A PROSE-PAYLOAD LADDER CANNOT FIRE THE ATTRIBUTION GATE** — the `.md` IS the payload, so
  every round scores non-zero by construction. Use the ladder-authored pre-image share instead,
  and **record the measurement**: #81's ladder stopped at **16/16 = 1.00** (threshold 2/3);
  #85's round 2 measured **8/28 = 0.29**, so that ladder had NOT left the PR and continuing was
  the correct call rather than a judgement.
- **Decision (operator, this session): the deployed pod is `cairn-store-go`.** The repo holds no
  manifest, so nothing in it could settle the question. **Sweeping without this would have
  propagated an unverified claim to every site it touched** — which is why #81 deliberately
  recorded the inconsistency rather than resolving it.
- **Decision (operator, this session): document the commit-message leak, widen the gate, do NOT
  rewrite public history.** A force-push breaks every clone and fork and would close every open
  PR's base.

- 🔴 **A CLEAN TEXTUAL MERGE DELETED FOUR TESTS, AND THE CONFLICT IT REPORTED WAS ABOUT A
  COMMENT.** *(Re-derived from #80, which lost the doc race and carried this nowhere else.)*
  Merging #69 into #76: `git` reported ONE conflict in `cmd/cairn-ui/main_test.go`, covering a
  comment; taking one side of that hunk removed four unrelated tests the other side had added
  further down the same file. No marker, no error, and reading the hunk could never have shown
  it. 🔴 **What caught it is a DECLARATION-SET DIFF AGAINST BOTH PARENTS** —
  `comm -23 <(git show <parent>:<f> | grep -oE '^func [A-Za-z][A-Za-z0-9_]*' | sort) <(… the
  merged file …)`, run for each parent. Do that on every merge where both sides touched one
  file; "no conflict markers left" is not the same claim.
- 🔴 **AND THE SAME MERGE LEFT A TREE THAT DID NOT COMPILE, FROM FILES THAT NEVER CONFLICTED.**
  *(Also re-derived from #80.)* The rename replaced an env helper; the new command's call site —
  added on the other branch, in a hunk the rename never touched — still called the old name.
  **Disjoint files are not safety: one side widened how something is read, the other added a
  caller.** The mutation battery said so first, by REFUSING TO VOUCH: *"the unedited copy is not
  green, so every mutant below would score KILLED for a reason that has nothing to do with its
  guard."* A non-compiling tree would otherwise have reported a perfect score.
- 🔴 **WHICH DOC PR TO MERGE FIRST: PREFER THE ONE WHOSE CLAIMS ARE STILL TRUE, NOT THE ONE
  OPENED FIRST.** The MERGEABLE-but-conflicting mechanism is already recorded twice above — this
  is only the tie-break, which was missing. Measured on #95 vs #96: #96's `State now` asserted
  facts a sibling session's work had just falsified (*"pid 3942268 … still true"*, *"leakscan
  exits 2 … still true"*), while #95 deliberately omitted `State now`. #95 could therefore land
  unchanged, and #96 needed re-deriving whichever order was chosen. **Seniority is the wrong
  key; staleness is the right one.** 🔴 **AND A THIRD PR CAN OPEN WHILE YOU DECIDE** — #97
  arrived four minutes after #95 merged, clean against `main` and conflicting with the branch
  written to replace #96. Run the open-PR sweep **again** immediately before `gh pr create`.
- 🔴 **MY PROBE REVOKED THE SESSION IT WAS ABOUT TO TEST, AND I FILED THE 401 AS AN INSTRUMENT
  QUIRK — THE RETRACTED DIAGNOSIS IS KEPT HERE BECAUSE IT WOULD HAVE TAUGHT THE NEXT READER TO
  PAPER OVER A REAL REVOCATION.** ❌ **RETRACTED:** *"the cookie is `__Host-…; Secure` and curl
  will not send a `Secure` cookie over `http://`, so a post-sign-in 401 is the instrument, not
  the server; force the header."* **Measured false in both directions.** curl 8.21.0 treats
  loopback as a secure context exactly as a browser does: sign in, `GET /` with the jar → **200**.
  The positive control that the Secure rule *can* withhold (a `secure` cookie for a non-loopback
  host over `http://`) was watched to fire, so the instrument was working.
  ✅ **THE REAL MECHANISM IS A SECURITY GUARD DOING ITS JOB.** `internal/ui/session.go` revokes
  the presented session before minting the new one — session-fixation defence, and the code says
  so. My chain re-ran `POST /sign-in` mid-probe with `-b <jar>` to read a `Location`, and threw
  the replacement cookie away with `-c /dev/null`; that call **revoked the jar's own session**,
  so the next `GET /` was a correct 401. Isolated: a second sign-in **without** presenting the
  cookie leaves the first session at 200; **presenting** it takes the first session to 401.
  🔴 **THE LESSON IS THE CONTROL I DID NOT RUN.** A 401 is the observable the most mechanisms
  share, and I picked the one I already suspected. The forced-header "control" changed **two**
  variables — it also re-signed-in, so it minted a fresh session; it could never have
  discriminated. **Never re-run an authenticating request inside a chain you are measuring**, and
  when an auth probe fails, suspect your own previous request before the server.
- 🔴 **A `nix build` IN THE REPO ROOT ARMS A GATE AGAINST YOU, AND AN ENTRY HAD ALREADY
  ACQUITTED IT.** `result`/`result-1` are untracked symlinks to store **directories**; the leak
  scanner's `--others` enumeration reads them and dies `[Errno 21]`, taking eight of the repo's
  own tests red with it. A previous `State now` said they were *"NOT the exit-2 cause —
  measured, not assumed"*; re-measured, they were the **only** cause named. **Build with
  `--out-link <scratchpad>/…`.** ⚠ And do not remove a gcroot without checking what still runs
  from it — here it was the gcroot of the very server a human was queued to verify, so the order
  had to be replace-then-remove.

- 🔴 **THE TEN BULLETS BELOW ARE RE-DERIVED FROM #96, WHICH LOST THE DOC RACE THREE TIMES.**
  They are its session's own records, moved into `Gotchas` (which APPENDS) rather than into
  `State now` (which REPLACES) because every one of them is a record of a round that has
  CLOSED, and that is what this document's own convention says to do with those. ⚠ **Six of
  #96's bullets were deliberately NOT carried and the reason is per-bullet, not editorial:**
  its `main` sha, its rank-9 status and its `leakscan` instance-naming are all falsified by
  the work above; its `SIX OF ELEVEN` event-kind count is superseded by the FOUR-of-eleven
  entry with a selection rule under `Defects`; its `leakscan` directory entry is superseded by
  the widened class there; and its *"the cheap control settled it"* bullet is **retracted**
  above, so re-landing it would reinstate a conclusion this file now measures as wrong.
- ✅ **THE RENAME ARC (rank 7) IS DONE AND VERIFIED ON `main`, CLAUSE BY CLAUSE.**
  `56cc56e`. Measured live, not recalled: **11** alias pairs in `internal/envalias`; every
  configuration read goes through the resolver (the only two raw reads are deliberate —
  `cairn-ui`'s control journal, which must SEE whitespace to refuse it, and
  `lib/host_identity.py`'s host-label names, which have no aliases); the warning is
  *"$X is a deprecated alias for $Y. Where both are set in the environment, $Y is the one that
  is read."*; and `RemovalAnchor` = *"the Python client (packages.cairn) is retired"* — the
  milestone the operator approved in place of a version, since this repo's version IS the git
  revision (`flake.nix`: `self.shortRev`) and `leakscan` refuses a dated one.
- 🔴 **P7'S KEY WAS REPLACED BEFORE IT WAS BUILT, AND THAT IS THE SESSION'S BEST RESULT.** Rank
  3 specified *"key it on principal + epoch"*. Measured: all fourteen journal event kinds are
  AUTHORIZATION events and `internal/write/write.go` makes no `control.` call, so an append
  moves no epoch — that key answers **304 to a client missing new bullets**. The validator is a
  digest of the **UNCOMPRESSED** tar instead: it covers content, the visible set and `?scope=`
  at once, and it is the only form both servers can agree on, because gzip identity between
  them is recorded as unattainable while `tests/dualrun/` compares the uncompressed tar
  byte-for-byte. **Independently re-verified after merge:** mutating the validator to a
  content-independent value makes `TestAnAppendMovesTheETag` fail with its own message.
- ⚠ **ONE DEBT, FILED RATHER THAN CARRIED IN A REPORT.** P7's two binding claims are not in
  `AGENTS.md` — it has **73 bytes** free and its own rule forbids paying by deleting a claim.
  It is `## Defects (batched)` entry with a mechanical closing condition; it is not an open end.
- 🔴 **AND THE GATE BUILT THIS SESSION REFUSED THIS VERY DOC, CORRECTLY.** The first draft of the
  bullet above spelled the task board's real name — the SAME denied identifier scrubbed off `main`
  this morning as #68, re-introduced by the sentence explaining that no task resolved. Rule (o)
  exited 13 on the delta, named the file and line, and nothing was written or pushed. **Not a
  pre-existing red and not an override case:** the remedy was the scratch file. The remedy for the
  NAME is the one `AGENTS.md` already prescribes — keep the mechanism, drop the particular.
- 🔴 **A SWEEP FOR THE PHRASINGS YOU HAVE SEEN IS A SPELLED CHECK, AND IT MISSED A CLAIM IN THE
  COMMIT WHOSE WHOLE SUBJECT WAS THAT CLAIM.** #78 corrected "which pod is deployed" at three
  sites, found by grepping three wordings (`deployed by nothing`, `not deployed by anything`,
  `DEPLOYED BY NOTHING`). A **fourth** claim — `AGENTS.md:399`, *"`server/Dockerfile` is what is
  deployed today"* — used none of them and survived; #81 had to finish the job, and its message
  records *"It survived #78, whose entire subject was correcting which server is deployed."*
  The right question is **"what else in this file asserts this?"**, never "where else does this
  phrase appear". A grep over wordings you already know cannot find the one you do not.
- 🔴 **TWO FALSE ZEROS ON ONE CLAIM, BOTH READING AS CONFIRMATION, IN UNDER A MINUTE.**
  Verifying that the handoff skill never mentions `leakscan`: (a) `grep -lc` combines two
  conflicting flags and prints **nothing at all** — not an error, just silence; (b)
  `find ~/.claude/skills/handoff -type f` returns **0 files**, because home-manager makes those
  entries **symlinks** and `-type f` does not follow them, while `grep` on the same path reads
  them fine. Both produced an empty result that looked like the answer. **`find -L` is the fix**,
  and a positive control on the same invocation is what exposed both.
- 🔴 **`gh pr checks` REPORTS THE LATEST RUN PER CHECK *NAME*, NOT PER COMMIT** — so after a
  rebase or a base move it can show a job's verdict from an older head, and a "pending" there
  can be a run that already finished on a sha you no longer care about. Read
  `gh api repos/<o>/<r>/commits/<sha>/check-runs` for the head you are actually merging.
  ⚠ And read BOTH surfaces: the same head answered `state=pending statuses=0` on the commit-
  status API while all six check-runs were `success` — this repo posts check-runs and no
  statuses, so a zero there is an ABSENCE, not a red.
- 🔴 **A MUTANT THAT DOES NOT COMPILE DIES AT THE BUILD AND PROVES NOTHING — HIT LIVE WHILE
  VERIFYING P7.** Replacing the ETag's digest with a constant left `hex` and `sha256Sum`
  unused, so `go test` reported `[build failed]` and the guard never ran. Rebuilt so the mutant
  still USES both symbols while being content-independent, it reached the guard and died with
  the guard's own message. **Mutate the narrowest expression that can be wrong**, and check the
  mutant compiles before reading its verdict.
- ⚠ **`mergeStateStatus: UNKNOWN` IS THE API COMPUTING LAZILY, NOT A PROBLEM WITH THE PR.**
  Seen immediately after a sibling PR merged and moved the base; it resolved to `CLEAN` on a
  re-read seconds later. Do not treat it as a conflict signal, and do not merge through it —
  re-read until it is one of the real values.
- **THE QUEUE WAS STALE IN TWO INDEPENDENT PLACES, BOTH ABOUT WORK ALREADY FINISHED**, and each
  cost real time before the work could start: rank 2 named a defect `tests/test_narrowing_echo_sites.py`
  had already closed, and rank 6 asked to close entries an earlier session had already moved to
  the archive. Neither was careless — it is what happens when sessions that cannot see each
  other write the same list. **Before acting on a ranked item, check whether it is already
  done**; the entry is a claim like any other.

- 🔴 **RE-DERIVED FROM #97**, which lost the doc race to #98 and then to #96. Three of its
  bullets were deliberately not carried — its `SIX OF ELEVEN` event-kind count, its
  `leakscan`-exits-2-on-a-directory entry, and its *"the cheap control settled it"* bullet —
  each superseded or retracted above rather than merely reworded.
- 🔴 **AN EMPTY RESULT COULD NOT DISTINGUISH TWO CAUSES OF A `leakscan` EXIT 2, AND THE CHEAP
  CONTROL SETTLED IT IN ONE COMMAND.** The base clone exited 2 with untracked `result` symlinks
  AND a live agent worktree both present, either a plausible culprit. A fresh worktree of
  `origin/main` — same tree, neither artefact — scanned **rc 0**, and the scanner's single
  `COULD NOT READ` line named the worktree directory. **The rival mechanism was named before
  concluding, and the discriminating control cost less than reasoning about it would have.**
- 🔴 **A HANDED-OVER `localhost:PORT` URL IS NOT A HANDOVER, AND THIS ONE WAS SILENTLY
  INVALIDATED WITHIN THE SESSION THAT GAVE IT.** `127.0.0.1:8103` was handed to the operator
  with a token path. That process later exited and **another session's `cairn-ui` took the
  port**, serving a different store and a different journal — so the URL still answered `401`,
  looking alive, while the handed-over token could not authenticate against it. Measured from
  `/proc/<pid>/cmdline`, which carries the owning session's scratchpad id. 🔴 **THE RECIPE IS
  THE CAUSE: `## How to verify` NAMES 8103, so every session that follows it collides.** Two
  fixes, and the second is the one that generalises: pick a free port (`ss -ltn` first), and
  **hand over the PID and the session id beside the URL** — a port answering 401 is
  indistinguishable from yours until you read whose process holds it.
- 🔴 **THE LEAK GATE BUILT THIS MORNING REFUSED THREE OF THIS SESSION'S OWN DELTAS, AND THE
  THIRD WAS A DIFFERENT RULE FROM THE FIRST TWO.** Two `denied-identifier` (a repo name in a
  `$`-variable spelling, which is why it did not LOOK like a name; and this repo's own
  synthetic canary, pasted in while documenting how to probe the gate) and one
  `dated-incident` (a real date in prose). ⚠ **A SYNTHETIC VALUE IS STILL A DENIED
  IDENTIFIER** — the canary exists to be refused, so quoting it in a committed doc is a real
  violation, not an exemption. The remedy every time was to describe by ROLE.
- 🔴 **AND IT CAUGHT A PRIVATE IP IN A TEST FIXTURE — THEN CAUGHT THE BULLET DESCRIBING THAT
  CATCH, BECAUSE THE BULLET QUOTED THE ADDRESS.** A new `internal/ui` test used an RFC1918 /24
  as a trusted-proxy allowlist entry; the remedy is RFC5737 TEST-NET-1, applied at all three
  sites together. ⚠ **THE SECOND REFUSAL IS THE ONE WORTH RECORDING**: this very bullet first
  spelled the offending prefix in order to explain it, and the gate refused the explanation on
  the line it added. `AGENTS.md`'s rule is *describe the shape, never instantiate it* — and the
  handoff tool's own write-gate doc records the identical mistake being made while documenting
  a different rule. **An example that IS the thing it forbids is the thing it forbids.**
  Four refusals in one session, all before anything was pushed, which is the whole point of
  the gate sitting inside the write tool rather than in a sentence.
- 🔴 **A GUARD THAT ASSERTS A FIELD BY SEARCHING THE WHOLE BLOCK IS SATISFIED BY A NEIGHBOUR,
  AND THIS REPOSITORY HAD ALREADY MEASURED IT ONCE.** `test_flake_ui_image_runtime_contract`'s
  "not root" assertion searched the maker block for `${toString serverUid}:${toString
  serverUid}` — a string the block's own `chown` line contains. MEASURED: `User = "0:0"` left
  **all 17 tests green** and the built image reported `Config.User: 0:0`. The sibling module
  records that identical mutation as a defect it had already shipped and closed, with a
  FIELD-SCOPED reader; the new module regressed to the pre-fix shape under the same test name.
  **Read a field, never a block.**
- 🔴 **A MUTANT SURVIVED THE TEST WRITTEN TO CATCH IT, AND FINDING THAT IS WHAT MADE THE TEST
  REAL.** `TestALockedOutClientIsRefusedBEFORETheCredentialIsRead` asserted the REFUSAL —
  which is identical whether the lockout runs before or after `Authenticate`, because both
  orders still precede the session mint. The mutant passed a green suite. It now counts
  CREDENTIAL RESOLUTIONS through a wrapped `TokenAuthority` and requires zero while locked
  out. **When a test's name says ORDER, assert something only the order changes.**
- 🔴 **TWO REASONS FOR THAT ORDER WERE FALSE AND ARE RETRACTED IN THE CODE RATHER THAN
  SWAPPED.** "A lockout checked after the token is one a valid credential walks through" —
  false, measured. "An attacker must not get the failure record wiped" — false for THIS
  limiter: `netid.RateLimiter.RecordSuccess` is a deliberate no-op whose own comment explains
  that a success-resets-counter design is the defect. `internal/api` states the first reason
  for ITSELF; **a reason does not transfer between packages unexamined**, and examining it is
  what produced the retraction.
- 🔴 **A BUCKET-SEPARATION TEST NEEDS A THRESHOLD ABOVE ONE, AND AT ONE IT FAILS AGAINST
  CORRECT CODE.** `RecordFailure` reports the lockout when the count REACHES the threshold, so
  at 1 every client trips on its own first failure — making "the second client's first failure
  must not trip" unsatisfiable. The first draft asserted exactly that and went red against a
  correct implementation.
- 🔴 **A `PINNED_*_STEPS` DICT PINS WHAT IT NAMES AND IS BLIND TO WHAT IT DOES NOT.** Adding a
  publish leg left both the push and control dicts at FOUR while SIX steps existed, and
  **every test in the file stayed green**: the membership check is `set(PINNED) - set(bodies)`,
  which asks whether the pinned ones are present. So the two steps deciding whether a
  root-owned session dir or a private package reaches a PUBLIC registry were exactly the two
  the file could not see. **A whole-text pin still needs a ledger of what must be pinned.**
- 🔴 **AN ANCHOR INSIDE A STEP'S BODY IS NOT A BOUNDARY.** Inserting that leg anchored on a
  comment line that occurs INSIDE the Go proof step, so the insert landed mid-step and a
  follow-up rewrite truncated its tail. The whole-text pin caught it with a diff naming
  exactly the missing lines; review would not have. Restored from `origin/main` and appended
  at EOF, where the last step genuinely ends.
- 🔴 **`$?` AFTER A PIPE, AGAIN, IN THE SESSION THAT HAD ALREADY READ THE WARNING.**
  `go build ./... 2>&1 | head -3; echo "rc=$?"` reports `head`'s 0 for a build that failed.
  Capture the rc before any pipe.
- ⚠ **A COUNT IN A DOCSTRING BESIDE THE DICT IT COUNTS WILL DRIFT** — "Four push steps", "the
  four CONTROL steps", "both pods", "Two repositories": ten stale phrasings swept in one pass.
  The docstrings now carry NO number, because the dict IS the count.
- **Decision (operator, this session): the UI deploys with a NEW UI-owned PVC** holding the
  control journal and the session table, with the store's volume mounted read-only. Offered and
  declined: the journal on the store's PVC (it needs write access to a volume `seed.sh`
  overwrites wholesale) and two separate PVCs.
- **Decision (operator, this session): the IngressRoute lands in the SAME PR as the workload**,
  accepting that the public hostname answers refusals until the journal is seeded. The
  alternative — cluster-internal first, route as a follow-up — was offered twice and declined.
- **Decision (operator, this session): seed with `-provider supabase`** and the real Supabase
  subject, so the user survives the JWT backend landing and needs no re-provision.
- **Decision (operator, this session): the in-cluster seeding is done by the ASSISTANT next
  session, with an explicit go-ahead first**, rather than handed to the operator as a runbook.

- 🔴 **THE ARCHITECTURE ANSWER TO "CAN FACT-ROT BE MADE STRUCTURALLY IMPOSSIBLE", AND IT IS
  NARROWER THAN THE QUESTION.** Only **deletion** and **derivation-WITH-COMPARISON** do it.
  Deletion is total: a claim that does not exist cannot rot. Derivation works only when a gate
  DIFFS the derived value against a declared expectation — that is what `api.DeclaredRoutes()`,
  `cairn -verbs` and `cairn -exit-codes` do, and why they have a zero rot rate while every
  hand-written sentence in this repo rots. ⚠ **Derivation MINUS the comparison is not a
  mechanism**, which is the whole finding of #101 below. A THIRD option — a TTL/staleness stamp
  for facts no command can answer — was designed and **rejected before building**: a check that
  reddens because a stamp aged goes red on a day nobody changed anything, and `RULES.md` is
  explicit that a permanently-red gate is worse than none.
- 🔴 **#101 WAS BUILT, AUDITED, AND CLOSED BY ITS OWN ROUND 0 — THE MOST USEFUL THING THIS ARC
  PRODUCED, AND IT PRODUCED NO CODE.** It added a "fact ledger" whose design decision was *"it
  asserts the question is still answerable, never the answer"*. That property was sold as what
  made it deterministic and durable. **It is also what made it blind to both incidents it cited
  as its basis**: through the "no image wraps it" rot (11 commits) and the "nine verbs" rot (39
  commits, 5 sites, one a live `assert >= 9`), `nix eval .#packages…` and `cairn -verbs` answered
  perfectly — the rot was in the ANSWER, which the design declined to look at. What actually
  closed the second was `assert len(go_verbs) == 10` plus `flake.nix`'s `want-verbs.txt` DIFF.
  **Ask of any proposed anti-rot gate: which of the incidents in its own rationale would it have
  caught?**
- 🔴 **A PROSE LINT FOR FACT-ROT IS REFUTED BY MEASUREMENT ON THIS TREE — do not re-derive it.**
  Three candidate rules were written and counted before building: `line-distance` **15 hits**,
  `bare-line-ref` **5**, `naive-count` **13**, and in each case the legitimate and the rotting
  uses are spelled IDENTICALLY — *"to be ON the verdict rather than in a comment 200 lines away"*
  is rhetoric about code placement; *"the paragraph N lines above"* is a navigation pointer that
  rots. `bare-line-ref`'s only live hits were the ref-parsing code discussing its own edge cases.
  **The defect lives in the relationship between the sentence and the world; a regex sees only the
  sentence.** `via: measurement`
- 🔴 **"BOTH CI TIERS SIMULATED GREEN" WAS A SIMULATION THAT COULD NOT REACH THE BRANCH IT
  CLAIMED TO TEST.** The refuse-on-skip path only executes when a toolchain is ABSENT; the host
  has `go` and `nix`, so `toolchain_missing` returned `None` and the `require` set was never
  consulted. `CAIRN_FACTS_REQUIRE=go`, `=nix` and `=all` all printed an identical `20 passed`.
  **Ask what a green simulation structurally cannot execute** — here, the only branch that
  mattered.
- 🔴 **I REPORTED `completed=6/6` AND NEVER READ THE CONCLUSIONS. THREE OF THE SIX HAD FAILED.**
  `completed` and `success` are different fields on the same object, and the easier one to assert
  is the one that says nothing. This happened while assembling an audit brief, in the session
  that had been quoting *read the content, never the exit code* all day. **Read
  `[.statusCheckRollup[]|select(.conclusion=="FAILURE")]|length`, not a completion count.**
- ⚠ **AND THE CI BREAKS WERE STRUCTURAL, NOT FLAKY:** the new step sat at `ci.yml:380` while the
  job's `pip install pytest` was at `:531`; the `nix` job installs pytest NOWHERE; and 20 added
  tests took the `tests` job's drift guard from gap 97 to 117 against its `MAX_GAP = 100`. **A new
  pytest step in the `go` or `nix` job must be placed after that job's own interpreter setup, and
  adding tests in bulk must move `FLOOR`.**
- 🔴 **A SPELLED READ-ONLY GUARD WALKED PAST EVERY DESTRUCTIVE COMMAND PUT THROUGH IT.**
  `{tok.lstrip("-") for tok in cmd} & {"rm","write","commit",…}` accepts `git clean -fdx`, a hard
  repo reset, and any `sh -c "…"` one-liner, because a shell string tokenises as ONE token.
  Labelling it "a spelled check" in its own docstring was NOT enough — **it had no negative
  control, and neither did its sibling**, in a PR whose other three controls all did.
- ⚠ **A GUARD WHOSE BODY IS A WORD-COUNT WHILE ITS DOCSTRING CLAIMS A RELATIONSHIP.** #101's
  `test_every_unanswerable_fact_names_who_CAN_answer_it` asserted `len(reason) > 80` and that one
  of four words appeared. A reason saying the OPPOSITE — *"nobody knows who could"* — passes both.
  Same shape as the guards-narrower-than-their-docstring family already recorded here.
- ⚠ **THE SALVAGE IS THE SHAPE TO COPY, NOT THE MECHANISM.** #102 does the one part with a
  measured incident behind it, in one file: delete the derivable claim, name the command once,
  add no guard. Measured `+8 lines, -4 prose words, derivable claims asserted 3 -> 0` — the line
  count ROSE because a fenced command costs lines, and the number that matters is the assertion
  count. **Report both rather than the flattering one.**
- ⚠ **`flake.nix` HAD ALREADY DECLINED #101'S SHAPE, IN WRITING** — *"a check that ran the binary
  to print that list and diffed it against a third hand-written copy would restate one claim down
  a longer path."* The repo's existing prose contained the refutation of the mechanism before it
  was built. **Grep the tree for a rejected-alternative note before building a gate.**

- 🔴 **A `completed` COUNT IS NOT A `conclusion`, AND I WALKED INTO IT WITH THE LESSON ON SCREEN.**
  Reading #102's rollup I printed `FAILURES: 0` from a parser using `CheckRun`'s field names
  (`name`/`conclusion`) against **`StatusContext`** objects (`context`/`state`), so every field came
  back `None` and the zero was a fact about the parser. `mergeStateStatus` was `UNSTABLE` throughout.
  **A rollup holds BOTH types — handle both, and cross-check the count against `mergeStateStatus`
  before believing a zero.**
- 🔴 **A TASK-COMPLETION NOTIFICATION'S "exit code 0" IS THE LAST COMMAND'S, NOT THE BUILD'S.** A
  backgrounded `nix build … | tail -80; echo "NIX_RC=$pipestatus[1]"` was reported as **exit code
  0** while `NIX_RC=1` and the derivation had FAILED. Same family as `$?`-after-a-pipe, arriving
  through the harness rather than the shell. **Capture the real rc into the OUTPUT and read the
  content; never quote a notification's exit code.**
- 🔴 **`git show <ref>:<path>` FOR A PATH THAT DOES NOT EXIST ON THAT REF, WITH STDERR SUPPRESSED,
  COUNTS AS ZERO.** Chasing that repo's red I reported "the mutation anchor is absent on `origin/main`"
  from a `git show … 2>/dev/null` piped into a counter: the file had been DELETED on main, python
  read an empty string, and the count was 0. That read as "the anchor was edited away" when the
  truth was "you are reading the wrong file" — the real target was `pinned("subsystem_resolver")`,
  in another repo. **Same class as `git diff --quiet <ref> -- <path>` on an absent operand: prove
  existence with `git cat-file -e` FIRST, and never suppress stderr on a read you will quote.**
- 🔴 **THE CONGESTION-vs-GENUINE DISCRIMINATOR FOR A RED CI LEG THERE, USED IN ANGER AND IT
  WORKED.** Read the STEP terminations, not the PipelineRun verdict: `step-pytests` exit **0** with
  `step-verdict` exit **1** is a real test failure; `step-pytests` exit **255/137** with no verdict
  printed is a congestion kill, and `ExceededNodeResources`/`TaskRunTimeout`-with-no-terminated-step
  is a third shape that posts an EMPTY description. **And the cheapest confirmation that it is not
  load: identical totals from two independent runs** (CI and a local `nix build` of the same check
  both gave `24096/24091/4/1`). Load moves wall times; it does not reproduce a count exactly.
- 🔴 **A MUTATION ANCHOR IS A CROSS-REPO DEPENDENCY WHEN `MODULE_PATH` IS A PINNED INPUT.** the handoff-tooling repo's
  battery mutates a module it gets from cairn's flake, so a cairn REFACTOR — consolidating an
  open-coded predicate, exactly the thing this repo's rules ask for — reddens it with nothing on
  either side declaring the coupling. ✅ **The anchor-count assert did its job**: it went RED rather
  than reporting a false `SURVIVED`, which is the whole reason that assert exists.
- ⚠ **`leakscan` rc 2 ON THE BASE CLONE IS STILL THE AGENT-WORKTREE DEFECT, AND REMOVING YOUR OWN
  AGENT'S WORKTREE IS THE DISCRIMINATOR.** Removed mine (clean, HEAD == the pushed PR head); rc
  stayed **2**, now naming a DIFFERENT session's worktree — which proves the cause is worktrees
  rather than content, and that the survivor is not yours to remove. **Quote the PR's own CI
  `leakscan` job instead; it was SUCCESS.**
- 🔴 **A WORKTREE CREATED FROM `origin/main` TRACKS `origin/main`, SO A BARE `git push` TARGETS
  MAIN** — hit again this session while setting up the handoff branch. Push with an explicit
  refspec.
- ⚠ **A THROWAWAY REPO BUILT TO TEST A GUARD STILL TRIPS THE never-commit-to-main HOOK**, and the
  hook resolves `-C` from the COMMAND TEXT: a shell variable it cannot expand, or a directory the
  command is about to CREATE, both make it judge the CALLER's repo instead. `git init -b work` and
  create the directory in an EARLIER tool call.
- ⚠ **A `no-change` (exit 5) SHORT-CIRCUITS BEFORE THE LEAK GATE.** Re-testing the gate with a delta
  already merged measures the no-change path, not the guard — a green there is about nothing. Use a
  fresh delta for every gate probe.

- 🔴 **AN IMMUTABLY-TAGGED DEPLOYMENT DOES NOT DRIFT — IT STANDS STILL, WHICH IS INDISTINGUISHABLE
  FROM "CURRENT" FROM OUTSIDE.** The browser surface served an artefact 10 commits stale for a day
  while a merged feature sat on `main` and its provider sat configured beside it. Every instrument
  was green and correct: the pod was healthy, the route answered, both repos' gates passed. The one
  reading that would have shown it — resolving the running image's sha back to a commit and counting
  the distance — is not something any gate does. **When you touch a deployment, resolve its image to
  a commit and count `git rev-list --count <that>..origin/main` before believing it is current.**
  ⚠ **AND THE SCOPE OF THAT NEGATIVE, BECAUSE AN EARLIER DRAFT OVERSTATED IT AS "nothing ANYWHERE
  compares a deployed tag against the branch it came from" — a claim spanning two repositories and a
  cluster, carrying no command to refute it, written three lines above the rule forbidding exactly
  that shape.** What is actually measured: **nothing in THIS repository** does it (checked here),
  and nothing found in the deployment repo's manifests or CI as of the commit that armed sign-in.
  Refute it by grepping either tree for a check that reads a running image's tag and compares it to
  a branch — a single hit retires the claim.
- 🔴 **A `State now` BULLET ASSERTING A NEGATIVE IS THE ONE SHAPE A READER CANNOT FALSIFY CHEAPLY.**
  This doc said *"the deploy is still not started … nothing has been written into the deployment-
  manifest repo"* for a day after both were done. A positive claim carries its own check (go read the
  thing it names); *"X has not happened"* names nothing to read, so it survives every review until
  somebody independently goes looking. **Give a negative claim the command that would refute it.**
- 🔴 **THE KICKOFF'S RANKS AND THE DOC'S RANKS DISAGREED, WHICH IS THE SHUFFLE HAZARD THIS DOC
  ALREADY RECORDS, ARRIVING FROM THE OTHER DIRECTION.** The resume message described rank 10 as the
  deploy and rank 11 as the seeding; by then the doc had renumbered those to the publish ordering and
  the leak gate. Acting on the kickoff's numbering would have claimed the wrong slug. **The doc is the
  authority over the kickoff that points at it, and a kickoff quoting a rank NUMBER goes stale the
  moment the list moves — quote the work, not the rank.**
- 🔴 **A CLEAN START PROVES NOTHING UNTIL THE CHECK THAT WOULD REFUSE HAS BEEN WATCHED REFUSING.**
  Before committing a config change to a repo where commit = live deploy, the binary was run against
  the exact values with two negative controls: redirect-URL-set-but-no-verifier → exit 78, and a
  callback path that does not end with the served route → exit 78. Only then was the passing run
  evidence. ⚠ The first attempt at this measured **the instrument**, not the program: `env -i` with a
  hand-written `PATH` put `timeout` out of reach and the run exited **127**. Reading the OUTPUT rather
  than the code is what caught it — a 127 read as a program failure would have sent the whole change
  back for a defect that did not exist.
- 🔴 **DISCRIMINATE A TRANSIENT BY FINDING THE STEP THAT DIFFERS, NOT BY RE-RUNNING IT.** The key-set
  fetch returned 502 five times and then 200 twenty-seven consecutive times. Re-running only produced
  more samples of one path; what identified it was probing the SAME service by a second route — from
  inside the cluster, from the consuming pod itself (6/6 OK) — plus a sibling endpoint on the external
  path that stayed healthy throughout, and the provider's own restart clock being 22h old. That
  triangulation put the fault in the edge path and nowhere else.
- ⚠ **A DEGRADED-BUT-SAFE FAILURE MODE IS WORTH READING THE CODE FOR BEFORE ACCEPTING THE RISK.**
  Against that 502 the surface does not refuse to start: it warns, withholds the provider button,
  answers 503 on those routes only, leaves the credential form, the entries page and existing sessions
  untouched, and re-arms itself when a fetch succeeds. Knowing that turned a deploy-blocking question
  into an accepted, documented window. **Decision (operator, this session): keep the external key-set
  URL rather than the in-cluster one, accepting that window.** The in-cluster alternative was measured
  working from the consuming pod and is recorded beside the variable so it is not re-derived as new.
- 🔴 **I SHIPPED A FALSE CLAIM IN A COMMIT MESSAGE AND IN A MANIFEST COMMENT, AND THE MEASUREMENT
  THAT CAUGHT IT WAS THE VERIFICATION I ALMOST SKIPPED.** Both said the pre-bump 401 on the root
  "is not the post-bump expectation". The root branches on `Accept`: a browser navigation gets **303**
  to the sign-in page, a non-browser client still gets **401**. The error was in the direction that
  wastes a rollback — somebody re-measuring with `curl` reads 401 and concludes the deploy failed.
  Corrected in a follow-up commit that keeps the wrong sentence inline, because what it got wrong is
  the useful part. **A prediction written into a comment before the deploy is a claim; go back and
  measure it after.**
- ⚠ **A COMMENT-ONLY CHANGE TO A WORKLOAD MANIFEST DOES NOT ROLL THE POD**, because YAML comments are
  stripped before apply and the applied object is byte-identical. Confirmed rather than assumed: the
  pod's age and restart count were unchanged across that reconcile. Useful when the correction above
  has to land on a surface you do not want to cycle twice.
- ⚠ **`claim-work` HAS NO SLUG FOR WORK THAT IS NOT ON THE RANKED LIST YET, AND TAKING ONE ANYWAY IS
  STILL RIGHT.** This session's deploy was not a ranked item under the doc's current numbering, so
  there was no `--slug-for` answer; a descriptive slug was claimed instead and released at the end.
  **The lock is worth taking even when the queue has no entry for the work** — the hazard it guards
  (two sessions doing the same thing) does not require the work to be enumerated.
- 🔴 **THE HAND-RUN BRING-UP RECIPE UNDER `## How to verify` MUST NOT BE DELETED, AND THE REASON IS
  RECORDED *HERE* BECAUSE THAT SECTION IS A REPLACE BUCKET.** The deployed browser surface holds
  **one** user, and the share flow's `Candidates` is narrowed by CO-MEMBERSHIP — so rank 9 cannot
  be verified there at all, and the recipe (two users, joined) is the only procedure that builds a
  world in which it can. ⚠ **A note saying "do not delete this" is self-deleting when it lives in
  the section it protects**: the next `/handoff` regenerates `State now`, `Next steps` and
  `How to verify` wholesale, so a guard written into any of the three survives exactly one update.
  It was written there first, which is the mistake this bullet exists to stop repeating — **put a
  guard in `Gotchas`, which appends, and leave only the procedure in the replaced section.**
- 🔴 **A RETRACTION IS A TREE-WIDE SWEEP — AND THE SITE I SWEPT WAS THE LESS-READ ONE.** A wrong
  claim about rank 9's target was corrected under `## How to verify` while an IDENTICAL copy
  survived in the ranked item itself, which is the section `/resume` and `claim-work` actually
  drive from. The audit round that caught it noted the corrected copy sat ~1,120 lines below the
  live one. **Grep the claim's own words across the whole document before calling a retraction
  done**, and when two copies exist, fix the one a reader reaches FIRST. This doc already carried
  that lesson in the abstract; it was read, and the sweep still missed a site.

- 🔴 **THE PRUNE ALREADY RAN ONCE AND THE DOCUMENT FULLY REGREW IN SEVEN DAYS — MEASURED, AND IT
  CHANGES WHAT "PRUNE THE DOC" MEANS.** At the prune commit (`3c4a1c6`, 2026-09-18) the whole
  document was **63,433 B** — *under* the 65,536 B guideline, so the prune WORKED. Seven days later
  it is **134,563 B**: ×2.1, roughly **10 KB and 8 `Gotchas` bullets per day**. `Gotchas` went
  **45,984 B / 89 bullets → 83,618 B / 147**, and is now **62% of the document**; every other
  section combined is ~51 KB and would sit inside the guideline on its own. **So re-running the
  prune buys about a week.** Treating it as a one-time fix is how a ceiling becomes decorative —
  the same pathology as a permanently-red gate, arrived at from the other side.
- 🔴 **`Gotchas` HAS AN ENTRY RULE AND NO EXIT RULE, WHICH MAKES IT MONOTONIC BY CONSTRUCTION — AND
  THE BUCKET RULE ACTIVELY FEEDS IT.** The write tool's semantics say durable content must not sit
  in a REPLACE section (`State now`, `Next steps`, `How to verify`), so the correct remedy for
  every such finding is "move it to `Gotchas`", which APPENDS and never shrinks. ⚠ **This session
  did exactly that twice in one PR** — once relocating an auth-coverage claim out of `State now`,
  once moving a do-not-delete guard out of `How to verify` — both correct by the bucket rule and
  both making the size problem worse in the one section that is the problem. **The two rules are in
  tension and nothing in the tooling says so.** Neither audit round caught it, because each was
  scoped to one axis. ⚠ **This very bullet is an instance**: it is being appended to `Gotchas`.
  **Closing condition:** an eviction rule for `Gotchas` — the candidate shape is *a bullet whose
  arc has closed moves to the archive in the SAME PR that closes the arc*, so eviction rides on
  work already happening instead of being a separate act of will. It belongs in the `/handoff`
  flow, which can enforce it; a sentence in this document cannot.
- ⚠ **AND THE ORDER MATTERS: FIX THE EXIT RULE BEFORE PRUNING, NOT AFTER.** Pruning first restores
  the number and leaves the mechanism, so the next reader sees a green document and no reason to
  look. The measurement above is what makes that argument, and it is recorded here so the next
  session does not have to re-derive it — or, worse, re-run the prune and believe it held.
- ⚠ **A RECORDED "I CHECKED THIS" CLAIM DRIFTED, AND IT WAS THE CLAIM LICENSING A DISMISSAL.** This
  document states that the write-back guard's demand for a handoff *about the archive file* is
  correctly dismissed, on the evidence that the archive carries "zero of `## Goal`, `## State now`,
  `## Next steps`, `## How to verify` **or a closing-condition**". Re-measured: the four headings
  are still **0**, but `closing condition` now appears **3 times** — all inside archived bullets
  *discussing* closing conditions, none the archive asserting its own. **The conclusion holds and
  the stated evidence does not**, which is the shape worth noticing: a check written once as
  "0 hits" rots into a false statement the moment the archived prose mentions the word. Assert the
  STRUCTURE (no handoff headings, header says nothing here is live), never a grep count.

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
# 🔴 --out-link INTO A SCRATCHPAD, NEVER THE REPO ROOT: a bare `nix build` leaves
#    `result`/`result-1`, untracked symlinks to store DIRECTORIES, which take leakscan
#    to exit 2 and eight of this repo's own tests red. See `Defects (batched)`.
nix build .#cairn-ui        --out-link <s>/gc-ui
nix build .#cairn-server-go --out-link <s>/gc-server
# 1. a synthetic store + token row, straight from the parity world builder:
#    python3 -c 'import sys; sys.path.insert(0,"tests/parity"); import world; …'
#    world.build_store(<root>) and world.TOKEN_ROW  → four scopes, one malformed entry
# 2. two users. 🔴 THE SAME -project NAME CREATES A SECOND PROJECT, so give them
#    DIFFERENT ones and join them in step 3; a shared name buys nothing:
export CAIRN_CONTROL_JOURNAL=<w>/journal.jsonl
<s>/gc-server/bin/cairn-server -create-user -provider supabase -subject sub-a \
  -email a@example.invalid -project orbit-works -scopes alpha-notes,beta-notes -store <w>/store
<s>/gc-server/bin/cairn-server -create-user -provider supabase -subject sub-b \
  -email b@example.invalid -project drift-lab -scopes "" -store <w>/store
# 3. ✅ -set-member PUTS B IN A'S PROJECT. #86 (`d6fe1c3`) added it; the hand-append this
#    step used to prescribe is retired. Ids, never display names — both are printed above:
<s>/gc-server/bin/cairn-server -set-member -member-project <prj_…> \
  -member-user <usr_…> -member-role member
# 4. a live credential for A. WITHOUT one the surface exits 78; `-create-user` does NOT
#    fix that, it mints a user and no credential:
<s>/gc-server/bin/cairn-server -issue-credential -principal <usr_…> -principal-kind user \
  -label '…' -token-out <w>/a.token -store <w>/store
<s>/gc-ui/bin/cairn-ui -store <w>/store -token-file <w>/tokens \
  -control-journal <w>/journal.jsonl -session-file <w>/sessions.jsonl \
  -host 127.0.0.1 -port 8103
```
🔴 **CHECK WHAT REVISION THE RUNNING BINARY IS, NOT JUST THAT SOMETHING IS LISTENING.**
`readlink -f /proc/<pid>/exe` carries the revision in the store path. An instance left up for a
human across sessions goes stale in the surface under test — one sat on `a88f60b` while `main`
had moved twelve commits, including the sign-in path #91 rewrote.
🔴 **`sharing writable` IN THE STARTUP LINE IS NOT EVIDENCE SIGN-IN WORKS** — the doc already
records a journal that printed it while refusing every sign-in. **Exercise the chain**: `GET /`
anonymous → **401**; `GET /sign-in` → a `password` field named `token`; `POST /sign-in` with an
`Origin` header → **303** to `/`; `GET /` with the cookie → only the scopes that principal can
reach. Re-measured on the `1838b82` instance: 401 · 200 · 303 · `alpha-notes` and `beta-notes`
and **not** `rubble-heap`/`hollow-set`; `/share?scope=…` **200** with the csrf field, the verb
checkboxes and the co-member in the candidate select.
🔴 **DO NOT RE-RUN `POST /sign-in` INSIDE THIS CHAIN — IT REVOKES THE SESSION YOU ARE HOLDING.**
`internal/ui/session.go` revokes the presented session before minting the new one (fixation
defence), so a second sign-in sent with `-b <jar>` kills that jar's session and step 4 answers a
perfectly correct **401**. Sign in ONCE, keep the jar, and do not fetch the `Location` with a
second POST. ⚠ An earlier revision of this file blamed that 401 on curl declining to send a
`Secure` cookie over `http://`. **That is false** — curl treats loopback as a secure context, and
`GET /` with the jar attached is **200**. The retraction is under `Gotchas`; do not re-derive it.
🔴 **AND VERIFY THE WRITE HALF ON A *COPY*, NOT ON THE WORLD YOU ARE HANDING OVER** — a share
POST is durable in an append-only journal, so probing the live one consumes the very action the
human was asked to take. `cp -a` the world, run a second instance on another port, POST
`/share` with the post-auth `csrf` hidden field and an `Origin` header, confirm a `granted`
event appended, then kill it **by resolved PID** (`ss -lptnH 'sport = :<port>'` →
`/proc/<pid>/cmdline`), never by a `-f` pattern.

**The DEPLOYED browser surface — RANK 13's target, and NOT rank 9's.** 🔴 **AN EARLIER DRAFT OF
THIS BLOCK SAID "ranks 9 and 13 are browser work against THIS, not a hand-run instance", AND THAT
WAS WRONG ABOUT RANK 9 — the retraction is kept because the wrong version sent a reader to the
wrong target.** Rank 9 verifies the SHARE FLOW, whose `Candidates` list is narrowed by
CO-MEMBERSHIP (operator decision, recorded under `Gotchas`: you may share only with somebody you
already share a project with). Measured on the deployed journal: **1 `user-created`, 1 project, 1
`member-set`** — one person, so the candidate select there is EMPTY and there is nobody to share
with. **Rank 9 therefore stays the hand-run recipe ABOVE** (which builds two users and joins them)
until somebody provisions a second co-member in-cluster; rank 13 is the only one this block is
about. ⚠ **Do not "simplify" by deleting the hand-run recipe** — it is the only procedure that
produces a world rank 9 can be verified in.
🔴 **Its public hostname is deliberately NOT written down in this PUBLIC repository** —
read it from the deployment-manifest repo's UI IngressRoute, and substitute it for `<surface>`:

```bash
# Anonymous refusals. The root answers TWO DIFFERENT THINGS and that is not a bug:
curl -s -o /dev/null -w '%{http_code}\n'                       https://<surface>/       # 401
curl -s -o /dev/null -w '%{http_code} %{redirect_url}\n' \
     -H 'Accept: text/html'                                    https://<surface>/       # 303 -> /sign-in
curl -s -o /dev/null -w '%{http_code}\n'                       https://<surface>/share  # 401
# The sign-in page must be 200 AND carry the provider button:
curl -s https://<surface>/sign-in | grep -c 'sign-in/github'                            # 1
# The cross-site gates must refuse, and the callback must REFUSE rather than 500:
curl -s -o /dev/null -w '%{http_code}\n' -X POST               https://<surface>/sign-in/github  # 403
curl -s -o /dev/null -w '%{http_code}\n' -X POST \
     -H 'Origin: https://evil.example'                         https://<surface>/sign-in/github  # 403
curl -s -o /dev/null -w '%{http_code}\n' \
  'https://<surface>/sign-in/github/callback?code=bogus&state=bogus'                    # 400
```

🔴 **`curl https://<surface>/` ANSWERING 401 IS CORRECT AND IS NOT A FAILED DEPLOY.** The root
branches on `Accept`: a browser navigation gets 303, `*/*` gets 401. This was written wrong once, in
the direction that wastes a rollback — pass `-H 'Accept: text/html'` or the answer is about curl.

🔴 **THE POD'S OWN STARTUP LINE IS THE AUTHORITY ON WHETHER THE PROVIDER BUTTON IS ARMED**, and a
200 on the sign-in page does NOT distinguish armed from withheld. It names the credential form and
the provider with its callback; a `key set could not be fetched` WARNING means the button is
withheld and its routes answer 503 until a fetch succeeds, re-arming by itself. Read it from the
workload's logs.

🔴 **RESOLVE THE RUNNING IMAGE BACK TO A COMMIT AND COUNT THE DISTANCE BEFORE BELIEVING THE
DEPLOYMENT IS CURRENT.** Read the image tag off the workload, then
`git rev-list --count <that-sha>..origin/main` here. Nothing gates this on either side, and it is
how a 10-commit lag sat unnoticed for a day behind a healthy pod and green gates.

## Open investigations — live diagnosis state

📄 **Five closed blocks were moved to `claudedocs/handoff-cairn-control-plane-archive.md`** —
PR #35's CI and the merged-tree byte budget, the P4 ladder, the round-1 fix round, and #38's
round-3/merged-gate block. They are resolved or superseded; the archive keeps them verbatim,
because a closed block's value is its measured values and eliminations. Read it on demand.

### FOUR of the five auth controls are still LOOPBACK-only readings, off-mesh unverified
- as-of: 2026-09-25
- **Symptom + exact repro:** not a defect — a coverage gap, recorded HERE because it previously
  lived only in `State now`, which REPLACES. It had already been hand-carried across two updates;
  the third would have dropped it, and the gap would then read as absent rather than open.
- **Observed (with values):** the anonymous refusal IS now measured off-mesh against the deployed
  surface — `GET /` (`Accept: */*`) → **401**, `GET /share` → **401**, through the public edge.
  Those are the ONLY two off-mesh readings. Everything else was watched failing closed over
  LOOPBACK on a hand-run binary: the Origin pair, the CSRF pair, the `__Host-` cookie attributes
  (`Path=/; HttpOnly; Secure; SameSite=Lax`), and the five-failure lockout answering 401 to a
  VALID credential. ⚠ Post-deploy probes added `POST /sign-in/github` → 403 with no Origin and 403
  with a foreign Origin, and the callback with no flight → 400. **Those exercise the OAuth routes'
  gates, NOT the four above** — the CSRF pair is a POST with a session and a wrong token, which
  none of these sends, so do not count them as closing this.
- **Ruled out:** that a trusted-network bypass could exist — every gate derives from the REQUEST
  (Origin vs Host, the session's own token, the cookie, the client key) and none branches on
  network position. `via: code` — a STRUCTURAL argument from reading the handlers, never a
  measurement, which is exactly why this block stays open.
- **Leading hypothesis:** the controls hold off-mesh; the structural argument is sound and the two
  measured refusals are consistent with it. Nothing contradicts it — but consistency is not the
  measurement.
- **Next probe:** drive the four remaining controls against the DEPLOYED surface, not a loopback
  binary: a POST with a valid session and a wrong CSRF token (expect 403), the `__Host-` attributes
  read off a real `Set-Cookie` at the edge, and five failed sign-ins followed by a VALID credential
  (expect 401, proving the lockout is not walkable). 🔴 **DO NOT FIRE THE LOCKOUT PROBE UNTIL YOU
  HAVE CONFIRMED THE TRUSTED-PROXY ALLOWLIST RESOLVES *YOUR* CLIENT ADDRESS, BECAUSE THE BLAST
  RADIUS OF A WRONG ONE IS THE WHOLE INTERNET.** `cmd/cairn-ui/main.go` states it: an allowlist
  that does not match the real proxy buckets every caller under the proxy's own address, so **one**
  abuser — here, you — locks out **everybody**. The variable being SET is not the check; the pod
  starts on a set-but-wrong value and cannot tell, and the tell is in the log rather than in any
  probe. Defaults are **5 failures / 900 s**, so getting this wrong is a **15-minute sign-in outage
  on a public surface**, and the advice below (wait it out rather than restart) EXTENDS it.
  ⚠ The probe is RATE-LIMITER STATE on a live surface and buckets on the client key — do it
  deliberately, and expect to wait out the window rather than restarting the pod to clear it.

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

### CLOSED — a live JWKS fetch has now been exercised against a real issuer
- as-of: 2026-09-18 · **CLOSED 2026-09-25, and the `Next probe:` line below is DISCHARGED — do
  not run it.** The probe it asked for was *"a pod, a network and a real issuer"*, and the deploy
  recorded in `State now` is exactly that: the browser surface runs in-cluster against a live
  self-hosted GoTrue, reached over TLS through the public edge. **A completed HANDSHAKE, which is
  what this block said no measurement had ever produced:** the pod's startup line carries **no**
  `key set could not be fetched` warning, and that warning is emitted on any fetch failure — so
  its absence is the positive evidence, not silence. The failure arm was ALSO observed, which is
  what makes the success arm a measurement rather than an assumption: the same binary run against
  a 502 printed the warning, withheld the provider button and answered 503 on those routes.
  ⚠ **SCOPE: this closes the HANDSHAKE question, NOT the `SSL_CERT_FILE` one.** The root-count
  observations below stand unchanged and the variable is still inert in both directions; nothing
  here re-opens or re-measures that. ⚠ And the handshake was made by the **UI** image, not the
  pod image the root counts were taken in — same CA-bundle mechanism, different artefact, so read
  it as evidence about the mechanism rather than about that image.
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

### ✅ CLOSED — the host-HOME-dependent doctor test was fixed two merges before this doc still called it LIVE
- as-of: 2026-09-23
- **Symptom + exact repro:** the block above — *"STILL LIVE, re-measured at `e15e331`"* — records
  `uv run --python 3.12 --with pytest -- pytest tests -q -p no:randomly` returning
  **`1 failed, 1969 passed`**, the failure being
  `tests/test_cairn_doctor.py::TestTheCliWiring::test_a_no_sync_run_still_reads_the_LOCAL_config`,
  and its `Next probe` says *"the real defect is that the test inherits the operator's HOME instead
  of pinning one; that is the fix when somebody wants it."*
- **Observed (with values):** on `feat/ui-image` at `a88f60b` the full suite is
  **2059 passed, 0 failed** in 525.86 s — that test among them, no failure, no skip.
  🔴 **AND THE CONDITION THE BLOCK BLAMES IS STILL PRESENT, WHICH IS WHAT MAKES THIS A CLOSURE
  RATHER THAN A DISAPPEARANCE**: `stat ~/.config/subsystem-store/instances` still reads mtime
  **2026-09-17 22:00:43** with one `.env` inside — byte-for-byte the state the two earlier
  blocks both name as the cause. Same host, same HOME, same config, test green.
  `via: measurement`
- **Ruled out:** that the symptom merely failed to reproduce. The fix is identifiable and dated:
  `git log e15e331..origin/main -- tests/test_cairn_doctor.py lib/ cairn` names **`c47636b`**,
  *"Pin the host configuration a doctor test inherited, and close three ladder-filed defects"*
  (#60) — which is the `Next probe`'s own prescription, already landed. `via: measurement`
- **Ruled out:** that this doc was merely behind by one merge. It is behind by **two** — `c47636b`
  (#60) predates both `93d0f03` (#74) and `a88f60b` (#76) — and the LATER of the two blocks,
  which re-measured it as LIVE, was written **after** the fix existed, which is the part worth recording.
  `via: measurement` — the ordering was read off
  `git log --oneline e15e331..origin/main -- tests/test_cairn_doctor.py lib/ cairn`, which lists
  `c47636b` below both later merges rather than inferred from the version numbers.
- **Leading hypothesis:** none needed; closed.
- **Next probe:** none. 🔴 **THE DURABLE LESSON IS ABOUT THIS DOCUMENT, NOT ABOUT THE TEST.** An
  `Open investigations` block is APPEND-ONLY, so a block saying `STILL LIVE` survives every update
  that does not retype it — including the update that lands its fix. Two sessions re-measured this
  one as live; neither ran `git log <the-as-of-ref>..origin/main -- <the paths the block names>`,
  which is one command and is what closed it. **Before re-measuring any `STILL LIVE` block, ask
  what landed since its `as-of` ref.**
