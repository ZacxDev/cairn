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

- `main` @ **`9c24bc4`** (#117) — ⚠ **re-read rather than quoting it.** The DoD stands: **ADDRESSED ⇒
  the arc stays CLOSED.** Run `pytest tests -q` and read **0 failed**; the total is deliberately NOT
  quoted — its selection is unstated, so a number would rot on sight.
- ✅ **RANK 14 IS DONE.** `#117` merged as **`9c24bc4`**. 🔴 **The ladder did NOT end on a clean round —
  it stopped on the ATTRIBUTION gate, two consecutive payload-zero rounds after rounds 0/1/2.** That is
  a different claim from "no findings remain", and a later reader must not upgrade it.
- ✅ **`#108`'s `flake.nix` CONFLICT IS RESOLVED AND PUSHED — `371ad76`, and the PR now reads
  `MERGEABLE`.** One region in the `onlyGo` filter, diff3 base section EMPTY: `main`/#117 added
  `internal/ui/app.css`, #108 added `uiaudit/go.mod` + `uiaudit/go.sum`. **Both rows AND both comments
  kept** — each is false alone (#117's says `tailwind.css` is deliberately OUT of the filter; #108's
  says `uiaudit/`'s `.go` files deliberately STAY out). `ci.yml` auto-merged and is semantically clean
  rather than only textually clean: the two additions land in **different jobs** (#117 → a `nix`-job
  step; #108 → a `go`-job step plus the whole new `uiaudit` job).
  🔴 **`git rerere` supplied that resolution, so it was treated as a CLAIM** — the assertion that
  settles it, and why a marker grep cannot, is the `Gotchas` bullet beginning *"`git rerere` CAN
  RESOLVE A CONFLICT FOR YOU SILENTLY"*. Result: 519 added lines, **0 missing, 0 removed**.
  Merged-tree gates, run at `371ad76` on TODAY's `main`: `go vet` rc 0 · `go test` **19 `ok` / 0
  `FAIL`** counted from result lines · `leakscan` rc 0 and `--self-test` rc 0 · `checks.…
  ui-stylesheet-is-current` rc 0 · `packages.{cairn-go,cairn-ui}` rc 0 under the **pinned Go 1.25**,
  with `internal/depspolicy` `ok` in BOTH sandboxes — which is the test #108's own comment says would
  report *"the set SHRANK"* without the lock-file rows, so the resolution is measured in the FILTERED
  source rather than merely compiling. ⚠ The host `go` is **1.26.7**, not the pin; the pinned reading
  is the `nix build` row, and both were read.
  ⏳ **CI at `371ad76` was still settling when this was written:** `nix`/`parity`/`leakscan`/`dualrun`/
  `uiaudit` green, `go` and `tests` in progress. **Not merged, deliberately** — see rank 17.
- 🔴 **RANK 15 IS BLOCKED ON AN OPERATOR LINE, NOT ON WORK — round 0 RETURNED
  `requirement questioned — R1`.** `claim-work cairn-control-plane-15` is held by this session.
  The full verdict and why it is a Fork rather than a nit are in the `Gotchas` bullet beginning
  *"ROUND 0 OF A LADDER CAN BLOCK A MERGE"*.
  In one line: #1871's requirement chain is **self-authored end to end by one agent session** and it
  arms a refusal on the only step that records a session, in **every** repo — so it needs one operator
  decision before merge. 🔴 **`/audit-pr`'s own rule: round 0 REPORTS and does not move the ladder** —
  it is not a finding for the findings-keyed stop rule and it licenses skipping no round, so rounds 1+
  are still owed whatever the operator decides about R1. ⚠ **The battery repair inside that PR is
  cleanly attributable and SEPARABLE** — it unbreaks a gate red at its own baseline control since
  #1815 and does not depend on rule (p).
- 🔴 **#1871 REACHES *THIS* DOCUMENT — the `Gotchas` bullet beginning *"THIS DOCUMENT IS NOT
  GRANDFATHERED"* carries the measurement and the ledger check.** Once rule (p) lands, the next
  `/handoff` update that GROWS this doc is refused
  (`status=size-ratchet`, exit **14**) unless the delta nets ≤ 0 or the run carries
  `--override-size-ratchet "<why>"`. ⚠ **`14` is free** on `main` (`EXIT_*` run 0,2–13), and rule (p)
  fires **before** the leak gate, which is why `SKILL.md`'s refusal list now reads 11 · 12 · 14 · 13.
- 🔴 **CARRIED FORWARD — THE CSP IS DELETED FROM THE BROWSER SURFACE BY OPERATOR DECISION, CHALLENGED
  ONCE AND REAFFIRMED.** Gone: `frame-ancestors` (the grant POST becomes framable), `form-action`,
  `base-uri`, `default-src 'none'`. ⚠ The CSP was NEVER what blocked Tailwind — `style-src 'self'`
  already permits a compiled same-origin stylesheet; the absent build step was the blocker.
  🔴 `sameOrigin`, `csrfTokenFor` and the `stateChanging` gates are NOT the CSP and are UNTOUCHED.
- ✅ **CARRIED FORWARD — THE DEPLOY IS DONE AND GITHUB SIGN-IN IS ARMED.** The surface runs the current
  image with `CAIRN_SUPABASE_{JWKS_URL,ISSUER,REDIRECT_URL}`; off-mesh readings are all refusals or
  unauthenticated pages. 🔴 **The three sign-in variables are a SET and WHICH one you delete decides
  survival:** dropping `JWKS_URL` or `ISSUER` while the redirect is set ⇒ **exit 78, pod DOWN**;
  dropping `REDIRECT_URL` ⇒ **pod UP**, button absent, sessions untouched — the cheapest remedy.
- ⏳ **CARRIED FORWARD — A *COMPLETED* SIGN-IN IS STILL UNVERIFIED** (rank 13, a human's), and
  🔴 **RANK 9 CANNOT BE DONE ON THE DEPLOYED SURFACE** — 1 `user-created`, 1 project, 1 `member-set`,
  so `Candidates`' co-membership narrowing leaves the share-flow select EMPTY.
- ⚠ **CARRIED FORWARD, UNCHANGED:** rank 11 open as the handoff-tooling repo's #1867 (held: that repo's
  `main` is red for an unrelated reason); that repo's branch protection requires no status checks
  (`required_status_checks` 404); `cairn-control-plane-9` still held on purpose; the UI image published
  and anonymously pullable; the reorder blast radius (`steps.<id>.outputs`, 43 references, untested).

## Next steps (ranked)

🔴 **NUMBERING IS STABLE — 1–16 keep their meaning; 17 is new.** Rank is half a `claim-work` slug, and
this doc has twice measured a shuffle re-pointing live claims.

1. ✅ **DONE — #74 merged as `93d0f03`.** forcing: gate.
2. ✅ **DONE — merged as `562a4f6f`.** forcing: gate.
3. ✅ **DONE — P7 merged as `c0f5b06`.** forcing: none.
4. **P8 — retire the Python oracle.** **Closing condition:** P8 opens when BOTH (a) the Go client has
   completed a real read AND a real write against the live pod from **at least two distinct hosts**,
   recorded; and (b) no open defect names the Go client or `packages.default`.
   **BACKSTOP: if (a) has not happened by 2026-11-01, P8 opens anyway and the residual risk is accepted
   EXPLICITLY, in writing.** Checked by `cairn doctor` output from two hosts plus `gh issue list`.
   ⚠ Sized, not measured: ~33,000 deletable lines, 3 of 6 CI jobs, ~233 KB of prose, ~10 paired-ledger
   guards.
   forcing: none
5. ✅ **DONE — `cairn-server -issue-credential`, in #76.** forcing: gate.
6. ✅ **DONE.** forcing: user.
7. ✅ **DONE — rename landed as `56cc56e` (#69).** forcing: user.
8. ✅ **DONE — rule (o) merged as `c4490f07` in the handoff-tooling repo.** forcing: incident.
9. ⏳ **IN FLIGHT: the share flow's human verification.** 🔴 **It CANNOT be done on the deployed
   surface** — one user, empty candidate select. Use the hand-run recipe under `## How to verify`,
   which builds two users and joins them, or provision a second co-member in-cluster first.
   ⚠ The previous handover is dead — rebuild the instance, re-read the token file, and check who holds
   the UI port before binding it.
   forcing: user — the operator reserved the browser step to a human.
10. ✅ **DONE — merged as `901b77d` (#104), verified on a real publish run.** forcing: user.
11. ⏳ **OPEN AS the handoff-tooling repo's #1867, UNMERGED ON PURPOSE.** Closing condition met and
    watched; held because that repo's `main` is red for an unrelated reason.
    forcing: incident — a denied identifier is on public `main` in a commit message.
12. **CORRECT TWO FILES THAT ASSERT THE HANDOFF-TOOLING REPO'S CI CHECKS BLOCK A MERGE.** They do not:
    `required_status_checks` returns 404. The two sites are **`scripts/run-tests.sh`'s own comment** and
    the **CI-platform skill's gotcha #9**. Both are wrong in the PERMISSIVE direction.
    **Closing condition:** both state the measured value with the command that re-measures it, or say
    the protection was deliberately removed and by whom.
    forcing: gate — a comment is a claim, and this one licenses merging through a red gate.
13. **COMPLETE A GITHUB SIGN-IN END TO END ON THE DEPLOYED SURFACE.** **Closing condition:** a human
    opens the surface, completes the GitHub flow, and the entries page renders for the seeded operator
    user — or the failure is recorded with the pod's log line. ⚠ **Precondition nobody has recorded:**
    sign-in resolves the token's `sub` against a user the control plane ALREADY holds; an unknown
    subject is REFUSED by design and all three failure causes collapse into one generic 401 on purpose,
    so **the pod's log is the only place the mechanism exists**.
    forcing: user — the operator reserved the browser step to a human.
14. ✅ **DONE — `#117` merged as `9c24bc4`.** The merged-tree gate on #117+#108 ran and was recorded on
    both PRs (rc 1, `flake.nix` only, resolution additive, merged tree green), then rounds 1 and 2 ran.
    🔴 **It stopped on the ATTRIBUTION gate — two consecutive payload-zero rounds — NOT on a clean
    round.** forcing: gate.
15. 🔴 **HELD ON AN OPERATOR DECISION — the handoff-tooling repo's `#1871` (rule (p), the size
    ratchet). Round 0 is DONE and its verdict is `requirement questioned — R1`; rounds 1+ are still
    owed.** `MERGEABLE`/`CLEAN`, four commit statuses `success`, **0 check-runs** (that repo posts
    statuses, not check-runs — read both surfaces). **The decision to take, in one question:** land
    rule (p) as written, land it scoped to repos that ship no size gate (round 0's F4/D3 — that keeps
    100% of the motivating value, since cairn ships no `test_handoff_doc_size.py`), land only the
    separable battery repair, or close it. 🔴 **Do NOT merge it on this doc's own say-so** — this doc's
    rank 15 is part of the self-authored chain round 0 flagged. Verified independently, not accepted:
    `main`'s battery copy list lacks `handoff-audit.py`, `skill-audit.py` and
    `browser-bridge/tests/test_skill_size.py`, all three of which the branch adds; and round 0's F3 —
    two byte figures that reproduce nowhere in the tree — was re-measured here and CONFIRMED with a
    positive control; the corrected figures are in `Gotchas`.
    **Closing condition:** a written operator line choosing among those four, then rounds 1+ with
    findings fixed or filed and a claims block posted with `--payload`, then the PR merged or closed.
    forcing: gate — the ratchet is what stops the prune being undone, and this document has regrown
    measurably since the prune (its own figures are corrected in `Gotchas`) without it.
16. **DECIDE THE TAILWIND BUILD-TOOLCHAIN QUESTION (`cairn#117`'s D1).** Round 0 measured the delta:
    the `@source` scan yields **~18 real utility selectors across 4 call sites**, against a generated
    artefact checked into the tree, a nix derivation, a second nix app, a flake check, a CI step and an
    unpinnable upstream version. ~40% of the 28 KB `app.css` is Tailwind machinery. "Use Tailwind" is
    the operator's explicit ask and stands; "Tailwind as a build toolchain" is the implementer's.
    ⚠ **#117 has MERGED with the toolchain in it**, so this is now a keep-or-replace question rather
    than a gate on a PR. **Closing condition:** a written operator line either accepting the toolchain
    or directing the hand-written-modern-CSS alternative.
    forcing: user — it is a requirement question only the operator can answer.
17. **NEW — `cairn#108`: READ THE SETTLED CI AT `371ad76`, THEN DECIDE WHETHER THE LADDER IS DONE
    BEFORE MERGING.** The conflict is resolved and the merge is measured (see `State now`); what is NOT
    settled is (a) `go` and `tests` were still in progress, and (b) **no audit round has run against
    `371ad76`** — the merge changed one `flake.nix` region and nothing else, but that is the ladder's
    question, not a merger's assumption. ⚠ The `uiaudit` job is `continue-on-error` and its own comment
    records that it can still show a RED ROW; **attribute a red by the failing TEST, never by the job
    name.** **Closing condition:** both surfaces read at the head sha actually being merged
    (`…/commits/<sha>/check-runs` AND `…/status`), a stated verdict on whether a further round is owed,
    and either a merge or a written line saying why it is held.
    forcing: gate — an open PR sitting `MERGEABLE` with unread CI is the shape this arc has already
    merged through twice.

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
🔴 **THIS SECTION HAS BEEN PRUNED TWICE, AND PRUNED MEANS *MOVED*: NEITHER PRUNE DELETED NOR
SHORTENED ANYTHING.** This is the SECOND prune, and it names its selection because a count
without one is not reproducible. Measured from the `## Gotchas` heading to the line before
`## How to verify`, at `299386a`: **147 top-level bullets, 84,556 B**, inside a **136,100 B**
document — 62% of a file read first thing every session, and ×2.1 in the seven days since the
first prune had landed the whole document at 63,433 B. **70 of those 147 bullets are now in
`claudedocs/handoff-cairn-control-plane-archive.md`, verbatim**, together with six closed
`Open investigations` blocks and this header's own predecessor. Same selection after, with
#115's four bullets rebased in and KEPT because they are live: **81 bullets, 47,920 B**, in an
**89,200 B** document. 🔴 **THAT IS STILL 23,664 B OVER THE
65,536 B GUIDELINE, AND THE GAP IS REPORTED RATHER THAN CLOSED** — closing it would have meant
moving bullets that are not closed, and a prune that reaches a number by relocating a live
tripwire has deleted it from every reader who does not open the archive. The first prune's own
figures, since this paragraph replaced the one carrying them: 174 bullets, 89,588 B, 125 moved.
What stays is what binds a NEXT edit: one instance of each general tripwire, the standing
operator decisions still in force, and the record of the arcs still open — ranks 4, 9, 11, 12
and 13. What moved is a record of a round, a PR or an arc that has CLOSED, plus every duplicate
instance of a tripwire kept here.

⚠ **A DUPLICATE IS NOT A REDUNDANCY WHEN THE SECOND ONE RECORDS THAT THE LESSON WAS READ AND THEN HIT ANYWAY** — that is why the instance kept is usually the LATEST, which carries the re-occurrence, rather than the first, which carries only the discovery.

- **Never run two `uv run --with pytest` invocations concurrently** — one disposes of the
  other's ephemeral venv and produces a bogus `FileNotFoundError` red.
- **The flake's source filter is git-based**: an untracked new `.go` file compiles under
  `go build` and fails `nix build`. Stage new files before reading either tier as green.
- **`AGENTS.md` + `CLAUDE.md` are gated** to 37,700 B with a 900 B minimum headroom
  (`tests/test_agent_instructions_weight.py`); currently 36,354 B. Put narrative in a
  harness README behind a pointer — that is the pattern the gate's own playbook prints.
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
- 🔴 **AN `until`-LOOP CI WATCHER THAT COUNTS *INCOMPLETE* CHECKS REPORTS GREEN ON AN
  EMPTY ROLLUP.** Zero incomplete checks is also what "no checks exist yet" looks like —
  the state right after a push while GitHub clears the rollup. Measured: it returned
  `checks: []`, `mergeable: UNKNOWN`, **exit 0**, and I reported that as green. **Require
  a minimum check COUNT as well as completion, and print the count** so the reading
  carries its own proof it measured something.
- 🔴 **A MUTANT THAT FAILS TO APPLY REPORTS A FALSE `SURVIVED`, AND IT HAPPENED TWICE HERE —
  ONCE ON THE MUTATION PROVING THE GATE.** Both were caught only by asserting the anchor's
  occurrence count BEFORE editing. **Assert the anchor count every time**; a "survived" you
  did not watch apply is not evidence.
- 🔴 **A RANK SHUFFLE SILENTLY RE-POINTS EVERY LIVE CLAIM, BECAUSE THE RANK IS HALF THE SLUG.**
  `cairn-control-plane-1` was held with a subject describing the P5 slice after the cutover was
  promoted to rank 1, so `claim-work --slug-for <doc> 1` returned a ref naming different work, and
  `rc 12` ("already yours, carry on") would have let a session continue with the wrong item.
  **When you re-rank, re-subject the claims in the same breath.**
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
- 🔴 **CI NEVER RUNS `nix flake check` — THE `nix` JOB BUILDS EACH CHECK BY NAME, SO A `checks.*`
  ENTRY NOBODY NAMES IS A CHECK NOBODY RUNS.** Verified on `main`: no flake-check step, eight
  explicit `nix build .#…` steps. All pre-existing checks ARE named, so there was no latent hole —
  but the convention is one omission away from producing one, and the omission reads as covered
  because the check exists in `flake.nix`. **Adding a `checks.*` entry means adding a CI step in the
  same commit.**
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
- **The publish-workflow mutation battery REFUSES in a shell without pytest** —
  `REFUSING TO VOUCH: the baseline run executed ZERO tests`. Run it as
  `uv run --with pytest python -u tests/publish_workflow_mutants.py`; it is **22 mutants,
  0 problems** at `f2ddf45`.
- **Decision (operator, this session): `Candidates`' co-membership narrowing STAYS.** You can
  share only with people you already share a project with; an invite flow is P6. Offered and
  declined: an exact-id lookup behind a uniform refusal, and widening to projects you
  administer.

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
- 🔴 **A CLAIM SCOPED TO "THIS COMMIT" STOPS BEING READ THAT WAY THE MOMENT THE COMMIT IS NOT THE
  NEWEST.** `cmd/cairn-server/main.go` read *"IT IS NOT DEPLOYED BY THIS COMMIT"* — true of its
  own commit, read by everyone afterwards as "this is not deployed". **Scope a claim to a STATE.**
- 🔴 **LINE-ANCHORED `grep` FAILED FOUR TIMES ON ONE QUESTION.** The claims WRAP — across lines
  AND across comment leaders. Normalise both before sweeping, and prove the sweep with a positive
  control you watched HIT a known wrapped instance. ⚠ **And editing a wrapped claim MOVES the
  wrap**, so a later sweep finds what an earlier one could not — that is not a fix.
- 🔴 **A COUNT WITHOUT ITS SELECTION IS NOT REPRODUCIBLE.** *"82 passed"* appeared in three
  commit messages before an auditor tried to reproduce it and got 68 / 85 / 2042 depending on the
  file set. The selection is now named. Same defect class this repo already tracks for prose
  counts, committed in commit messages instead.
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
- 🔴 **WHICH DOC PR TO MERGE FIRST: PREFER THE ONE WHOSE CLAIMS ARE STILL TRUE, NOT THE ONE
  OPENED FIRST.** The MERGEABLE-but-conflicting mechanism is already recorded twice above — this
  is only the tie-break, which was missing. Measured on #95 vs #96: #96's `State now` asserted
  facts a sibling session's work had just falsified (*"pid 3942268 … still true"*, *"leakscan
  exits 2 … still true"*), while #95 deliberately omitted `State now`. #95 could therefore land
  unchanged, and #96 needed re-deriving whichever order was chosen. **Seniority is the wrong
  key; staleness is the right one.** 🔴 **AND A THIRD PR CAN OPEN WHILE YOU DECIDE** — #97
  arrived four minutes after #95 merged, clean against `main` and conflicting with the branch
  written to replace #96. Run the open-PR sweep **again** immediately before `gh pr create`.
- 🔴 **A `nix build` IN THE REPO ROOT ARMS A GATE AGAINST YOU, AND AN ENTRY HAD ALREADY
  ACQUITTED IT.** `result`/`result-1` are untracked symlinks to store **directories**; the leak
  scanner's `--others` enumeration reads them and dies `[Errno 21]`, taking eight of the repo's
  own tests red with it. A previous `State now` said they were *"NOT the exit-2 cause —
  measured, not assumed"*; re-measured, they were the **only** cause named. **Build with
  `--out-link <scratchpad>/…`.** ⚠ And do not remove a gcroot without checking what still runs
  from it — here it was the gcroot of the very server a human was queued to verify, so the order
  had to be replace-then-remove.

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
- 🔴 **A `PINNED_*_STEPS` DICT PINS WHAT IT NAMES AND IS BLIND TO WHAT IT DOES NOT.** Adding a
  publish leg left both the push and control dicts at FOUR while SIX steps existed, and
  **every test in the file stayed green**: the membership check is `set(PINNED) - set(bodies)`,
  which asks whether the pinned ones are present. So the two steps deciding whether a
  root-owned session dir or a private package reaches a PUBLIC registry were exactly the two
  the file could not see. **A whole-text pin still needs a ledger of what must be pinned.**
- 🔴 **`$?` AFTER A PIPE, AGAIN, IN THE SESSION THAT HAD ALREADY READ THE WARNING.**
  `go build ./... 2>&1 | head -3; echo "rc=$?"` reports `head`'s 0 for a build that failed.
  Capture the rc before any pipe.
- **Decision (operator, this session): the UI deploys with a NEW UI-owned PVC** holding the
  control journal and the session table, with the store's volume mounted read-only. Offered and
  declined: the journal on the store's PVC (it needs write access to a volume `seed.sh`
  overwrites wholesale) and two separate PVCs.
- **Decision (operator, this session): seed with `-provider supabase`** and the real Supabase
  subject, so the user survives the JWT backend landing and needs no re-provision.
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

- 🔴 **THREE INSTRUMENT FAILURES IN ONE FINDING, NONE OF WHICH CHANGED A CONCLUSION — BECAUSE EACH
  WAS CAUGHT BY A CONTROL RATHER THAN BY INSPECTION.** Closing one 🔴 on `cairn#117` produced: (a)
  the implementer's sweep sliced each test body to the next `^func`, swallowing the FOLLOWING test's
  doc comment — exactly where the evidence lived — so it reported the table clean; (b) its first
  control restored with `git checkout -- internal/ui/*.go` while those files carried the
  UNCOMMITTED fix, destroying it; (c) my own regeneration control reported ✅ BYTE-IDENTICAL when
  the generator had **refused to run at all** (`no …/tailwind.css — run this from the repository
  root, or pass the root as $1`) — the file was unchanged because nothing regenerated it. **Every
  one was caught by reading OUTPUT rather than an exit code or a diff.**
- 🔴 **A GENERATOR POINTED AT SOURCE FILES READS THE COMMENTS, AND IN A REPO THAT MANDATES DENSE
  COMMENTS THAT IS A LIVE FALSE-RED FACTORY.** `@source "./*.go"` made Tailwind emit `.sticky`,
  `.table`, `.hidden`, `.visible`, `.fixed`, `.static` — each present in the generated CSS with
  **zero** occurrences in any `Class()` literal, generated purely from English prose (`table` 64
  occurrences in comments, `visible` 15, `fixed` 14). Rewording three comment lines changed the
  stylesheet. The check that would fire says *"app.css is BUILD OUTPUT and must never be
  hand-edited"* — a **correct remedy with a wrong diagnosis**, sending the reader to hunt an edit
  nobody made. **Closed by scanning NOTHING** (`source(none)` with no `@source` at all): a dedicated
  constants file or an inline source list both leave prose in the input set, so only "scan nothing"
  is structural rather than a discipline one edit can break.
- 🔴 **PROVE A DISCONNECTION WITH BOTH ARMS: the change is byte-identical AND the instrument can
  still move the output.** "Reword a comment ⇒ identical" is satisfied by a generator that writes
  nothing. The second arm — change the real input, watch the output move (28,109 → 28,145 B) — is
  what makes the first mean anything.
- 🔴 **A `checks.*` ENTRY NOBODY NAMES IS A CHECK NOBODY RUNS, AND THIS ARC PRODUCED THE FIRST LIVE
  INSTANCE.** `cairn#117` added `checks.ui-stylesheet-is-current` to `flake.nix` and left
  `.github/workflows/ci.yml` untouched; CI named five checks and not it. The guard for the WHOLE
  generated-stylesheet approach would have shipped inert, reading as covered precisely because it
  exists in `flake.nix`. Fixed in the same PR, and **validated as an instrument before being
  trusted**: rc 0 restored, rc 1 with one rule appended, `nix eval` resolving the attribute so a
  typo could not silently never match.
- 🔴 **A MUTATION BATTERY CAN BE UNABLE TO SCORE A SWEEP FOR WEEKS AND READ AS MERELY RED.** the
  handoff-tooling repo's `mutants-handoff-cap.sh` has been exiting 1 at its own BASELINE control
  since #1815: its copy list never moved when a new function landed, so the mutated copy lacked a
  module the tests need. **The chain is three files deep and the traceback names an unrelated
  subsystem's size gate**, which reads as a foreign breakage rather than a missing copy. Verified
  statically rather than by running it: the branch adds three `cp -a` lines `origin/main` lacks.
  **A gate nobody can run is not a gate, and its redness is not evidence about the code.**
- 🔴 **`__HARNESS_BROKE__ … only 0 test(s) ran` IS A "COULD NOT MEASURE" SIGNAL, NOT A RED RESULT —
  AND I NEARLY READ IT AS CORROBORATION.** Trying to confirm the claim above by running the battery
  at `origin/main`, I got rc 1 and almost reported it as agreement. Two things were wrong: my
  `worktree add` had failed with `already exists`, so I ran in a directory that was **not a git
  repository at all**, and the shell had no pytest, making "0 tests ran" inevitable. **A true claim
  corroborated by a broken instrument is still unevidenced.**
- ⚠ **THE SHARED SCRATCHPAD IS ONE PATH AND DISPATCHED AGENTS WRITE INTO IT.** An implementer
  working in another repo overwrote this session's own PR-comment drafts at the same filenames.
  Nothing was lost because those comments were already posted, but the collision is the documented
  one: **name scratch files per-agent, and do not assume a path you wrote is still yours.**
- 🔴 **A PRE-START COMMENT MUST PRECEDE THE `in_progress` FLIP, BECAUSE ONLY THE COMMENT NOTIFIES.**
  A status flip to `in_progress` pushes to nobody; the notification fires on entering
  `ready_for_review`. So the pre-start comment is the operator's ONLY chance to object BEFORE the
  work, and posting it after the flip inverts that. Both tasks this session dispatched follow that
  order, with the criteria quoted verbatim so the timestamped copy is auditable.
- 🔴 **WHEN YOU AUTHOR THE CRITERIA, YOU MAY NOT GRADE THEM — EVEN WHEN THE DETECTOR SAYS
  AUTHOR-SPECIFIED.** The pickup detector is deterministic: a `## Acceptance criteria` heading ⇒
  AUTHOR-SPECIFIED ⇒ a local pickup may set `complete`. But this session WROTE both cards minutes
  before picking them up, so the heading's presence is an artefact of its own authorship. Both are
  committed to ending at `ready_for_review`. **Read the detector's verdict against who actually
  wrote the words, not against the heading.**
- ⚠ **`--arc` CANNOT SEE A REPO OUTSIDE ITS FOUR HANDLES, AND ITS REFUSAL IS EXPLICITLY "NOT
  MEASURED".** `find-session --arc` resolves a doc against a FIXED set of four repo-handle
  environment variables only; for a repo outside that set — and **this repo is outside it** — it
  exits **5**, which its own text says must never be reported as an empty arc. Pointing an unused handle at the repo for one invocation is a working
  read-only workaround; the repo LABEL in the output is then cosmetically wrong.
- ⚠ **AN ARC AUDIT'S COVERAGE IS BOUNDED BY COMMIT-TRAILER ATTRIBUTION, AND HERE IT WAS 16 OF 33.**
  `--arc` unions WRITERS (from the doc's commit trailers, which include the originating session) with
  READERS (sessions whose OPENING message names the doc). It printed **"17 of 33 commit(s) on this
  doc carry no session id — those writers are NOT in this chain"**, and the opencode corpus was not
  searched at all. **Quote the uncovered half whenever you report an arc as complete.**
- 🔴 **A MULTI-PART INSTRUCTION LOSES ITS TAIL, AND NOTHING NOTICES.** An arc audit over 16 sessions
  found exactly one ask that was given directly and then dropped rather than deferred: the third
  clause of a three-part process feedback. The first two shipped in one PR whose title names them
  both; the third had **0 mentions in this doc and 0 tasks**. The tell is a PR title that enumerates
  *some* of a numbered instruction. **When an instruction has parts, record the parts, not the PR.**

- 🔴 **`git rerere` CAN RESOLVE A CONFLICT FOR YOU SILENTLY, AND ITS OUTPUT IS A CLAIM RATHER THAN AN
  ANSWER.** Merging `origin/main` into #108 printed `Resolved 'flake.nix' using previous resolution.` on
  **stderr** and left zero conflict markers — from a resolution recorded by an EARLIER session's
  integration run, against a DIFFERENT `main`. The file still shows as `UU` until you `add` it, which is
  the only reason it was read at all. **The check that settles it is not a marker grep** (there are
  none) **and not "it compiles"**: diff each PARENT against the merge base, collect the lines each side
  ADDED, and assert every one is present in the merged file — plus that neither side REMOVED any. Here
  that was 519 added lines, 0 missing, 0 removed. Same family as the declaration-set diff this doc
  already records for a merge that deleted four tests.
- 🔴 **A HOOK-BLOCKED `bash` CALL RUNS *NOTHING* IN IT — INCLUDING THE HEREDOC THAT WAS GOING TO WRITE
  THE COMMIT MESSAGE.** A single call wrote a message with `cat > msg.txt <<'EOF'` and then ran
  `git commit -F msg.txt`; the commit guard refused the call, so the message file was never created and
  the follow-up run died `could not read log file … No such file or directory` — which reads like a
  path mistake and is not. **After a blocked call, assume nothing in it ran**, and the guard says the
  right fix out loud: write the message with the `Write` tool and pass `-F <file>`, never a heredoc whose
  lines a guard parses as real commands.
- 🔴 **THE COMMIT GUARD CANNOT RESOLVE `git -C $VAR` AND JUDGES YOUR *CALLER'S* DIRECTORY INSTEAD.**
  `git -C $WT commit` in a detached scratch worktree was refused as a commit to `main`, because the
  guard could not see `$WT`'s value and fell back to the session's cwd — which really was `main`. It
  says so in its own message. **Pass `-C` an ABSOLUTE path** (or assign the variable in the same
  command) when a guard is in the path of the call.
- 🔴 **A PR WHOSE BRANCH IS HELD BY A STALE AGENT WORKTREE IS STILL UPDATABLE — DETACH AND PUSH BY
  REFSPEC.** `refs/heads/uiaudit-browser-harness` was checked out in
  `.claude/worktrees/agent-…` (idle ~20 h, unlocked), so a second `worktree add` of that branch is
  refused. `worktree add --detach <path> <head-sha>` → merge → commit → `push origin
  HEAD:refs/heads/<branch>` updates the PR without touching the other worktree. ⚠ **The local branch
  ref is then BEHIND `origin` by exactly that merge** — `origin` is authoritative, and the drift is
  reported rather than silently repaired, because repairing it means writing to a checkout that is not
  yours.
- 🔴 **THIS DOCUMENT IS NOT GRANDFATHERED, AND THAT IS WHY RANK 15 LANDS ON IT FIRST.**
  `handoff_budget.GRANDFATHERED` holds twelve paths and none of them is
  `claudedocs/handoff-cairn-control-plane.md`, so its allowance is the base **65,536 B** against a
  measured **93,006 B**. The other cairn arcs' docs ARE listed (`handoff-cairn-oss-multi-instance.md`
  98,304 · `handoff-cairn-phase3.md` 163,840 · `handoff-cairn-task-linkage.md` 81,920), which is
  exactly the shape that makes "surely ours is grandfathered too" a wrong guess. **Check the ledger,
  do not infer it from a sibling.**

- 🔴 **TWO BYTE FIGURES THIS DOCUMENT ASSERTS DO NOT REPRODUCE AGAINST THE TREE, AND THEY HAVE ALREADY
  PROPAGATED INTO ANOTHER REPO'S CODE COMMENTS.** This file says the first prune landed the document at
  **63,433 B** (lines 257 and 757) and that it was **134,563 B** seven days later (line 758), ×2.1.
  MEASURED with a positive control: `git cat-file -s 3c4a1c6:claudedocs/handoff-cairn-control-plane.md`
  is **64,097 B**, the pre-prune peak is **139,371 B** at `219d58e`, and a scan of **every** revision of
  the file finds **no** revision measuring 63,433 or 134,563 — while the control value 64,097 hits two
  revisions, so the scan works. The true ratio is **×2.17**. 🔴 **The CONCLUSION survives** — 64,097 is
  under the 65,536 B guideline, so "the prune WORKED and the document then doubled" is unaffected, which
  is exactly why the wrong numbers are dangerous rather than obviously wrong. ⚠ **They are no longer
  only ours:** the handoff-tooling repo's #1871 quotes them at FOUR sites (two in `handoff_doc.py`, one
  in its test module, one in `write-gate.md` §I), each beside the sha that contradicts it — found by
  this arc's round 0, re-verified here rather than accepted. That repo has already ruled on this shape:
  its size gate's own module says any other mention *"must cross-reference this module rather than
  restate the literal."* **Closing condition:** both figures in THIS file corrected to the measured
  values with the `git cat-file -s` command that re-measures them beside each, and the four downstream
  sites either corrected or reduced to a cross-reference. ⚠ **Same defect class this document already
  tracks as "a count without its selection is not reproducible" — but one level worse, because these
  two DID carry a sha, and the sha is what refutes them.**
- 🔴 **ROUND 0 OF A LADDER CAN BLOCK A MERGE THAT EVERY CORRECTNESS ROUND WOULD HAVE PASSED, AND THIS IS
  THE FIRST INSTANCE IN THIS ARC.** #1871's round 0 verdict is `requirement questioned — R1`: the
  measurement, the task-board item carrying the requirement, the acceptance criteria stamped
  **AUTHOR-SPECIFIED**, the implementation, and the ranked next-step instructing a future session to
  merge it were all authored by **one agent session** (`268378fb-…`, the task created 57 minutes before
  the first commit), and three separate labels make that read as external specification. No operator
  line authorises it: the only operator decision in this arc authorises **a prune**, not a tool
  refusal. 🔴 **The blast radius is what makes it a Fork rather than a nit** — rule (p) arms a refusal
  on `/handoff`'s write path, which is the ONLY step that records a session, in EVERY repo. ⚠ **The
  block is "get an operator line", NOT "the code is wrong"** — round 0 separately re-verified that all
  15 mutation rows apply and change exactly one line each, and that the battery repair is cleanly
  attributable to `claude/RULES.md`'s *"a permanently-red gate is worse than no gate"*. **The repair is
  SEPARABLE and could land on its own.**
  ⚠ **SIXTH LEAK-GATE EVENT, AND IT FIRED ON *THIS* BULLET.** Writing the attribution chain above
  required naming the system that holds the task — and that system's NAME is a denied identifier, so
  `status=leak-refused` (exit 13, one `denied-identifier` finding) rolled the write back. The lesson
  this arc already records was READ and hit anyway, in a new shape: the previous five were fixture
  data, a commit message and a canary quoted while documenting the gate, and this one is **an external
  system named while describing WHO AUTHORED a requirement** — a sentence whose whole subject is
  attribution, where the name feels like the evidence. Describe the system by its ROLE
  (*"the task board"*) and the attribution is unharmed.

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

📄 **Eleven closed blocks are now in `claudedocs/handoff-cairn-control-plane-archive.md`** —
five moved at the first prune (PR #35's CI and the merged-tree byte budget, the P4 ladder, the
round-1 fix round, and #38's round-3/merged-gate block) and six at the second (the two
host-HOME doctor-test blocks and the ✅ CLOSED block that closed them, the `packages.default`
flip's mechanical blocker, the live-JWKS-fetch block, and the publish gate). They are resolved
or superseded; the archive keeps them verbatim, because a closed block's value is its measured
values and eliminations. Read it on demand.

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

### `cairn#117` and `cairn#108` overlap in two shared files and no merged tree has been measured
- as-of: 2026-09-25
- **Symptom + exact repro:** both PRs are open, both report `MERGEABLE` against `main`, and neither
  has been merged-tree gated against the other. `gh pr view <n> --json mergeable` answers about each
  PR versus `main`; it says nothing about the tree their merge creates.
- **Observed (with values):** `gh pr view 108 --json files` and `gh pr view 117 --json files` share
  **`flake.nix`** and **`.github/workflows/ci.yml`**. Round 0 located the `flake.nix` hunks: #108 at
  `@@ -357` and `@@ -387`, #117's first hunk at `@@ -387` — the **same `onlyGo` filter region**. Both
  also append a step to the same `nix` CI job. #117 head `6bbcddf`; #108 open, unmerged.
- **Ruled out:** that the overlap is only prose. #117's PR body originally said #108 "touches
  `internal/ui/render.go` prose only in a residual it left unapplied"; the file lists refute that —
  two shared files, both load-bearing. `via: command` (`gh pr view --json files` on both).
- **Ruled out:** that `MERGEABLE` settles it. `RULES.md` records a measured instance of two PRs both
  reporting `MERGEABLE` while `git merge-tree --write-tree` exited **1**, because GitHub compared
  each against a `main` where neither had landed. `via: doc`
- **Leading hypothesis:** a textual conflict is likely in `ci.yml` (both append to the same job's
  step list) and possible in `flake.nix`'s `onlyGo` list; a semantic conflict is possible regardless,
  since #117 REMOVES an `onlyGo` row (`tailwind.css`, which existed only for the now-deleted
  `@source` scan) while #108 edits the same region.
- **Next probe:** build an integration branch off current `main`, merge both, and run the full gate
  set there — `git merge-tree --write-tree` first and **branch on its EXIT CODE, never on a marker
  grep** (it prints only a tree OID on success and emits no `<<<<<<<`). Then bisect any failure to
  the merge commits. Do this BEFORE round 1, because round 1 cannot see it.
