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

- `main` @ **`cd86714`** — ⚠ re-read rather than quoting it; a handoff doc can never record its own
  merge, so a `State now` sha is stale by exactly one commit the moment it lands.
- ✅ **THE ARC REMAINS CLOSED AND WAS NOT RE-OPENED.** The closing condition was re-verified on `main`
  at `27bdeb3`; `main` has moved exactly one commit since, `cd86714`, and it is **docs-only — one file,
  this document, 37+/26-** (`git show --stat cd86714`). No code moved under that verification, so the
  ADDRESSED verdict carries forward. ⚠ **This session did NOT re-run the three commands**; it carried
  the earlier measurement forward on the strength of the docs-only delta, which is a weaker claim than
  a fresh green and is stated as one. Ranks 18–20 are the SUCCESSOR arc.
- ✅ **ZERO PRs OPEN IN `cairn`** (`gh pr list --state open` → `[]`), nothing blocked on code.
- ⏳ **RANK 18 IS CLAIMED AND IN FLIGHT** — `claim-work cairn-control-plane-18`, rc 0. An `opencode`
  dispatch (`deepseek-v4-pro`) is driving the operator's Brave through the `browser` bridge, **background
  tabs only, no window raised**, over a 24-item / 10-section feature matrix built from
  `internal/ui/routes.go`'s ledger. ⚠ Rank 9 was ALREADY CLAIMED by another session two days ago, which
  is the independent reason 18 was the right pick.
- 🔴 **THE DEPLOYED BROWSER SURFACE IS 13 COMMITS STALE, MEASURED TWO INDEPENDENT WAYS.** Filed under
  `Defects (batched)`. The bump is mechanical and the artefact already exists; it is an operator call
  because a commit in the deployment-manifest repo IS a deploy. **Operator decision this session: run
  the validation pass locally now and keep the bump a separate decision.**
- 🔴 **THE HAND-RUN WORLD FOR THIS PASS IS NOT ON PORT 8103.** 8103 was already held by another
  session's `cairn-ui` — exactly the collision this document predicted. This one is
  **127.0.0.1:8147, PID 3427887**, session `bb38a675`, built with `go build` from `cd86714` into a
  scratchpad, over a parity-world store plus two users joined by `-set-member` (journal 10 lines).
  It is EPHEMERAL and its credential is throwaway; nothing in it is handed over.
- ⏳ **CARRIED FORWARD, UNCHANGED:** a COMPLETED GitHub sign-in on the deployed surface is still
  unverified (rank 13), and the share flow still cannot be verified on the deployed surface with one
  user (rank 9). The three sign-in variables remain a SET: dropping `JWKS_URL` or `ISSUER` ⇒ **exit 78,
  pod DOWN**; dropping `REDIRECT_URL` ⇒ pod UP, button absent.
- 🔴 **THE MUTATION SWEEP FOR RULE (p)'s FIVE UNSCORED ROWS IS STILL DEAD AND WAS NOT RESTARTED THIS
  SESSION.** Unchanged from the previous update: re-run from scratch on a detached worktree of that
  repo's `main`, verified tree-identical before starting, with `PYTHONDONTWRITEBYTECODE=1`; budget
  ~3 min/row. The 24 rows the dead run scored were OTHER guards, not the five. Hand-driven kills are
  not battery scores.
- ⚠ **NO TASK-BOARD FIELD IS RECORDED, AND THAT IS A MEASURED ABSENCE AGAIN.** The resolver exited **5**
  while its own POSITIVE CONTROL answered 1 link for a different session — so the board is reachable
  and this session genuinely touched no task. A zero from that endpoint cannot distinguish "touched
  none" from "wrong id"; the control is what makes it the former. The field's own name still cannot be
  written in this repository — describe it by ROLE.

## Next steps (ranked)

🔴 **NUMBERING IS STABLE — every number below KEEPS ITS LINE even when the item is done, because a
rank is half a `claim-work` slug: delete the number and `claim-work --slug-for <doc> <n>` resolves to
an item nobody can find.** Re-ranking re-points every live claim.

🔴 **AND THESE ARE A NEW ARC, NOT THE CLOSED ONE'S REMAINDER.** Say so when you pick one up.

1. ✅ **DONE — #74 merged as `93d0f03`.** forcing: gate.
2. ✅ **DONE — merged as `562a4f6f`.** forcing: gate.
3. ✅ **DONE — P7 merged as `c0f5b06`.** forcing: none.
4. **P8 — retire the Python oracle.** **Closing condition:** a real read AND a real write against the
   live pod from **at least two distinct hosts**, recorded, AND no open defect naming the Go client or
   `packages.default`. **BACKSTOP: if that has not happened by 2026-11-01, P8 opens anyway and the
   residual risk is accepted EXPLICITLY, in writing.** Checked by `cairn doctor` from two hosts plus
   `gh issue list`. ⚠ Sized, not measured: ~33,000 deletable lines, 3 of 6 CI jobs, ~10 paired-ledger
   guards.
   forcing: none
5. ✅ **DONE — `cairn-server -issue-credential`, in #76.** forcing: gate.
6. ✅ **DONE.** forcing: user.
7. ✅ **DONE — rename landed as `56cc56e` (#69).** forcing: user.
8. ✅ **DONE — rule (o) merged as `c4490f07` in the handoff-tooling repo.** forcing: incident.
9. ⏳ **THE SHARE FLOW'S HUMAN VERIFICATION — ASKED THREE TIMES AND STILL NOT DONE.** 🔴 **CLAIMED BY
   ANOTHER SESSION** (`cairn-control-plane-9`, two days old) — check the lock before touching it.
   ⚠ **Rank 18 measured something that changes what this item has to say to its human:** the entries
   page carries NO link to the share flow, so "sign in and test sharing" lands a human on a page with
   no way to get there. Tell them the URL path explicitly, or fix the navigation first (see
   `Defects`). It still CANNOT be done on the deployed surface as it stands — one user, empty
   candidate select — so it needs the hand-run recipe under `## How to verify`, or a second co-member
   provisioned in-cluster.
   forcing: user — the operator reserved the browser step to a human, and has now asked three times.
10. ✅ **DONE — merged as `901b77d` (#104), verified on a real publish run.** forcing: user.
11. ⏳ **OPEN AS the handoff-tooling repo's `#1867`**, held because that repo's `main` is red for an
    unrelated reason. Its value rose when rule (p)'s ladder found a commit-message disclosure in a
    PUBLIC repo, a channel gated by NOTHING there.
    forcing: incident.
12. **CORRECT TWO FILES THAT ASSERT THAT REPO'S CI CHECKS BLOCK A MERGE.** They do not
    (`required_status_checks` → 404). Both wrong in the PERMISSIVE direction.
    forcing: gate.
13. **COMPLETE A GITHUB SIGN-IN END TO END ON THE DEPLOYED SURFACE.** ⚠ Precondition: sign-in resolves
    the token's `sub` against a user the control plane ALREADY holds; an unknown subject is refused by
    design and all three causes collapse into one 401, so **the pod's log is the only place the
    mechanism exists.** ⚠ **And the surface it would be tested against is 13 commits stale** — see
    `Defects`; #117 does not touch the sign-in path, so this item is not blocked on the bump, but say
    which artefact you measured. forcing: user.
14. ✅ **DONE — `#117` merged as `9c24bc4`.** 🔴 Its ladder stopped on the ATTRIBUTION GATE — two
    consecutive payload-zero rounds — **NOT on a clean round**; a later reader must not upgrade that.
    forcing: gate.
15. ✅ **DONE — rule (p) MERGED as `b4233ea9`.** Five rounds (0–4), stopped by the attribution gate on
    two consecutive MEASURED `payload=0`. Four operator decisions recorded on the PR; four findings
    FILED rather than fixed, which is what that gate firing means. ⏳ Five mutation rows remain
    unscored — tracked in `State now`, not here. forcing: gate.
16. **DECIDE THE TAILWIND BUILD-TOOLCHAIN QUESTION.** `#117` merged WITH the toolchain, so this is now
    keep-or-replace rather than a gate on a PR. ⚠ Its gcroot was living in the repo root as `result`
    and arming the leak gate; relocated this session to a path outside the repo. forcing: user.
17. ✅ **DONE — `cairn#108` merged as `84642ff`**, verified by CONTENT because ancestry is false after
    every squash. forcing: gate.
18. ⏳ **IN FLIGHT — THE BROWSER VALIDATION PASS OVER EVERY UI FEATURE, VIA `opencode`.** Claimed as
    `cairn-control-plane-18`. Target is a hand-run instance from `main`, by operator decision, because
    the deployed one is 13 commits stale and its share flow has nobody to share with. Artefacts live
    OUTSIDE this repo, under a `.cairn-r18` directory in the workspace root (brief, report,
    screenshots) and the dispatch log beside it. **Closing condition unchanged:** a recorded run naming
    the features covered and what it found, or a written line retiring the ask. ⚠ **Two findings are
    already recorded under `Defects` and were verified independently of the subagent** — do not
    re-derive them. forcing: user — asked directly and never actioned until now.
19. **RUN THE WHOLE DESIGN THROUGH `/the-algorithm`.** Asked 09-23, with the design stated in the same
    message. 🔴 **0 hits for `the-algorithm` across this doc, the archive and the plan.** The fact-rot
    sub-question DID get a delete-first pass; the whole-design trace has no record.
    **Closing condition:** a recorded pass, question-requirements → delete → simplify in that order, or
    a written line saying the fact-rot pass discharged it. forcing: user.
20. **THE ARCHIVE IS 26,781 B OVER ITS LEDGER ALLOWANCE AND NOTHING WILL NOTICE.** 174,237 B against a
    grandfathered 147,456 B; and the prune left THIS doc 20,160 B looser than the ledger's own
    tightest-quantum discipline requires. A FOREIGN ledger entry sits outside every one of that gate's
    corpus checks by construction, so no check fires either way. **Closing condition:** both entries
    re-derived from measured size, or a written line exempting foreign entries from tightness — closed
    when the numbers are READABLE by that gate rather than when someone raises them. forcing: gate.

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

- 🔴 **THE DEPLOYED BROWSER SURFACE IS 13 COMMITS STALE, AND THE ONE UI COMMIT AMONG THEM IS THE ONE A
  VALIDATION PASS IS FOR.** The workload runs `cairn-ui:sha-71041ff8…` (#106);
  `git rev-list --count 71041ff..origin/main` = **13**, and
  `git log 71041ff..origin/main -- internal/ui cmd/cairn-ui` lists exactly **one** — `9c24bc4` (#117),
  the Tailwind theming. **Measured two independent ways at the edge, each with its local control:**
  the stylesheet route serves **1,146 B** of hand-written CSS where `main` serves **28,109 B** of
  generated output (`internal/ui/app.css`, confirmed byte-for-byte on a hand-run instance from `main`);
  and the edge still sends the `Content-Security-Policy` header that #117 DELETED, where `main` sends
  none. So the live page is the pre-theme UI. 🔴 **THE BUMP IS MECHANICAL AND THE ARTEFACT ALREADY
  EXISTS** — the registry answers **200** for the manifest at `main`'s sha, with the deployed tag as a
  positive control (200) and a bogus short tag as a negative control (404) — it is a one-line image-tag
  change in the deployment-manifest repo's UI Deployment. ⚠ **AND THE BUMP HAS A CONSEQUENCE WORTH
  STATING BEFORE IT IS TAKEN: it REMOVES a CSP that is currently live.** #117's operator decision to
  delete that header has never actually reached production; the bump is the moment it does. **Closing
  condition:** the tag names a revision whose distance to `origin/main` is 0 at the moment it lands,
  recorded — or a written line accepting the lag and saying for how long.
- 🟡 **THE ENTRIES PAGE OFFERS NO WAY TO REACH THE SHARE FLOW, WHICH IS WHY "GO TEST SHARING IN THE
  BROWSER" KEEPS STALLING.** Measured on a hand-run instance from `cd86714`, signed in as a principal
  owning two scopes: `GET /` is **1,386 B** with **0** occurrences of the word "share", exactly **one**
  `href` (the stylesheet) and **one** form action (sign-out). Positive control: the share index itself
  contains **8** occurrences, so the grep can see the word. The flow is not broken — bare `GET /share`
  answers **200** with "Scopes you can share" and both scopes linked — but **nothing navigates there**,
  so a human sent to the landing page has to know to type the path. ⚠ **A SECOND HALF, AND IT IS A
  MESSAGE DEFECT RATHER THAN A LOGIC ONE:** `?scope=` is keyed on the scope **ID**, never the display
  name — deliberate, and `render.go`'s comment says why (a name is user text and would sit in a query
  position). But a human who hand-types the name they can see gets **404** and the words *"no such
  scope, or it is not yours to share"* for a scope that **is** theirs to share. The refusal is correct
  and its sentence is false. **Closing condition:** one PR that links the share index from the entries
  page, plus a decision on whether the name-keyed refusal should say something true.

## Gotchas / decisions / dead-ends
🔴 **THIS SECTION HAS BEEN PRUNED THREE TIMES, AND PRUNED MEANS *MOVED*: NO PRUNE HAS DELETED NOR
SHORTENED ANYTHING.** This is the THIRD prune, and it names its selection because a count without
one is not reproducible. Measured from the `## Gotchas` heading to the line before
`## How to verify`, at `9c24bc4`: **108 top-level bullets, 66,662 B**, inside a **105,456 B**
document — **63% of a file read first thing every session**, more than the whole 65,536 B
guideline by itself. Every one of the 108 was classified into exactly **one** of four buckets,
and **DROP was required to stay empty**: **57 STAY · 8 ARCHIVE · 43 ROUTE-OUT · 0 DROP**, moving
**26,804 B** of `Gotchas`, plus one settled `Open investigations` block (2,052 B) and one closed
`Defects` entry (440 B) — **29,296 B in total, all of it VERBATIM** into
`claudedocs/handoff-cairn-control-plane-archive.md`. Asserted rather than claimed: every bullet
body present in **exactly one** of the two files afterwards, **0 lost, 0 duplicated**, every other
section byte-identical, and the detector validated by a positive control that dropped a known
bullet and was watched reporting **1 lost**.
🔴 **43 OF THE 108 WERE NEVER ABOUT THIS ARC, AND THE ARCHIVE IS A WAY-STATION FOR THEM RATHER
THAN THEIR HOME** — generic git/shell/grep/CI/agent-tooling tripwires learned while sitting in
this repository. The `subsystem-index` ruling is to ask which repo a lesson is ABOUT, not which
one you were standing in, so their home is the operator's own shared rules-and-dotfiles repo
(`claude/RULES.md`, `claude/RULES-ARCHIVE.md`) or the owning skill there. **They were deliberately NOT routed in the same
change:** that is a cross-repo PR against files with their own *enforced* byte ceilings, and doing
it here would have made this one unreviewable. The ranked, per-bullet list naming each proposed
destination is on the third prune's PR. **Until that follow-up lands, the archive is the only
copy.**
🔴 **THAT LEAVES THIS DOCUMENT ABOVE THE 65,536 B GUIDELINE, AND THE GAP IS REPORTED RATHER THAN
CLOSED — WHICH IS THE CORRECT OUTCOME, NOT A SHORTFALL.** Closing it would have meant relocating
bullets that are still live, and a prune that reaches a number by moving a live tripwire has
deleted it from every reader who does not open the archive. The single largest cost of that
refusal is the `ROUND 0 OF A LADDER …` bullet: its round-0 record is discharged, but its tail is
the **sixth leak-gate event** and the latest instance of that tripwire, and a bullet cannot be
split without landing text in both files and failing the 0-duplicated assertion. What stays is
what binds a NEXT edit: one instance of each general tripwire still in force, the standing
operator decisions, and the record of the arcs still open — **ranks 4, 9, 11, 12, 13, 15 and 16**.
What moved is the record of a round, a PR or an arc that has CLOSED, plus every DUPLICATE instance
of a tripwire kept here. Earlier prunes' figures, since this paragraph replaces the one carrying
them: the first moved 125 of 174 bullets from 89,588 B; the second moved 70 of 147 from 84,556 B
and stopped 23,664 B over the guideline for this same reason.
⚠ **AND THE ARCHIVE'S OWN ALLOWANCE IS NOW A LIVE COUPLING NOBODY HAD RECORDED** — this move
takes it from 138,791 B to a measured **174,237 B** against a **grandfathered 147,456 B** in the
handoff-tooling repo's `handoff_budget.py` ledger (rule (p)). **Exceeding it will be noticed by
nothing**, because a foreign ledger entry sits outside every one of that gate's corpus checks by
construction — the same disjoint-key-spaces shape a bullet below already measures. Stated as a
follow-up with a closing condition on the PR rather than fixed here.

⚠ **A DUPLICATE IS NOT A REDUNDANCY WHEN THE SECOND ONE RECORDS THAT THE LESSON WAS READ AND THEN HIT ANYWAY** — that is why the instance kept is usually the LATEST, which carries the re-occurrence, rather than the first, which carries only the discovery.

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
- 🔴 **A PRUNED-TO ARCHIVE IS NOT A HANDOFF DOC, AND THE WRITE-BACK GUARD CANNOT KNOW THAT.**
  Moving blocks into `handoff-cairn-control-plane-archive.md` made the guard treat that
  filename as its own topic and demand a handoff for it. Checked rather than asserted before
  dismissing: the archive carries **zero** of `## Goal`, `## State now`, `## Next steps`,
  `## How to verify` or a closing-condition, and the prune is already described inside the real
  doc. **Dismiss is the right answer there** — a handoff written "for the archive topic" would
  mint a doc nobody wants. ⚠ The dismissal is per-session; a new session starts fresh.
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
- 🔴 **A CLAIM SCOPED TO "THIS COMMIT" STOPS BEING READ THAT WAY THE MOMENT THE COMMIT IS NOT THE
  NEWEST.** `cmd/cairn-server/main.go` read *"IT IS NOT DEPLOYED BY THIS COMMIT"* — true of its
  own commit, read by everyone afterwards as "this is not deployed". **Scope a claim to a STATE.**
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
- 🔴 **A `nix build` IN THE REPO ROOT ARMS A GATE AGAINST YOU, AND AN ENTRY HAD ALREADY
  ACQUITTED IT.** `result`/`result-1` are untracked symlinks to store **directories**; the leak
  scanner's `--others` enumeration reads them and dies `[Errno 21]`, taking eight of the repo's
  own tests red with it. A previous `State now` said they were *"NOT the exit-2 cause —
  measured, not assumed"*; re-measured, they were the **only** cause named. **Build with
  `--out-link <scratchpad>/…`.** ⚠ And do not remove a gcroot without checking what still runs
  from it — here it was the gcroot of the very server a human was queued to verify, so the order
  had to be replace-then-remove.

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
- 🔴 **A PROSE LINT FOR FACT-ROT IS REFUTED BY MEASUREMENT ON THIS TREE — do not re-derive it.**
  Three candidate rules were written and counted before building: `line-distance` **15 hits**,
  `bare-line-ref` **5**, `naive-count` **13**, and in each case the legitimate and the rotting
  uses are spelled IDENTICALLY — *"to be ON the verdict rather than in a comment 200 lines away"*
  is rhetoric about code placement; *"the paragraph N lines above"* is a navigation pointer that
  rots. `bare-line-ref`'s only live hits were the ref-parsing code discussing its own edge cases.
  **The defect lives in the relationship between the sentence and the world; a regex sees only the
  sentence.** `via: measurement`
- 🔴 **A MUTATION ANCHOR IS A CROSS-REPO DEPENDENCY WHEN `MODULE_PATH` IS A PINNED INPUT.** the handoff-tooling repo's
  battery mutates a module it gets from cairn's flake, so a cairn REFACTOR — consolidating an
  open-coded predicate, exactly the thing this repo's rules ask for — reddens it with nothing on
  either side declaring the coupling. ✅ **The anchor-count assert did its job**: it went RED rather
  than reporting a false `SURVIVED`, which is the whole reason that assert exists.
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
- 🔴 **A CLEAN START PROVES NOTHING UNTIL THE CHECK THAT WOULD REFUSE HAS BEEN WATCHED REFUSING.**
  Before committing a config change to a repo where commit = live deploy, the binary was run against
  the exact values with two negative controls: redirect-URL-set-but-no-verifier → exit 78, and a
  callback path that does not end with the served route → exit 78. Only then was the passing run
  evidence. ⚠ The first attempt at this measured **the instrument**, not the program: `env -i` with a
  hand-written `PATH` put `timeout` out of reach and the run exited **127**. Reading the OUTPUT rather
  than the code is what caught it — a 127 read as a program failure would have sent the whole change
  back for a defect that did not exist.
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
- 🔴 **THE HAND-RUN BRING-UP RECIPE UNDER `## How to verify` MUST NOT BE DELETED, AND THE REASON IS
  RECORDED *HERE* BECAUSE THAT SECTION IS A REPLACE BUCKET.** The deployed browser surface holds
  **one** user, and the share flow's `Candidates` is narrowed by CO-MEMBERSHIP — so rank 9 cannot
  be verified there at all, and the recipe (two users, joined) is the only procedure that builds a
  world in which it can. ⚠ **A note saying "do not delete this" is self-deleting when it lives in
  the section it protects**: the next `/handoff` regenerates `State now`, `Next steps` and
  `How to verify` wholesale, so a guard written into any of the three survives exactly one update.
  It was written there first, which is the mistake this bullet exists to stop repeating — **put a
  guard in `Gotchas`, which appends, and leave only the procedure in the replaced section.**

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

- 🔴 **A REWORD REMOVES A NAME FROM THE TIP, NOT FROM THE FORGE — AND THE REMEDIATION RECORD BECOMES
  THE SIGNPOST.** A commit message on a public repo spelled identifiers from a private one. It was
  reworded and force-pushed with `--force-with-lease`, proved message-only (`git diff` **0 bytes**,
  equal tree OIDs, `range-diff` carrying **zero file sections**). 🔴 **The abandoned objects still
  answer `HTTP 200` ANONYMOUSLY** — no token — with their original message intact, so the rewrite
  bought "not visible to a reader of the branch", never "gone". ⚠ **And the audit record published the
  abandoned SHAs**, which is what made them reachable: a 40-hex sha is unguessable, so the comment
  thread was the only pointer. Remedy applied: redact the shas of exactly the commits whose messages
  carried a name, with an edit note saying the rewrite was not a scrub — verified 0 mentions against a
  live sha still matching, so the zero is measured. **Whenever you rewrite to remove something, ask
  what now POINTS at the old object, and treat your own record as a candidate.**
- 🔴 **GRANDFATHERING "AT MEASURED SIZE" REPRODUCES THE DAY-ONE REFUSAL ONE WRITE LATER.** A ratchet
  that fires on `after > allowance AND delta > 0` refuses the very next growing update when the
  allowance equals today's bytes. The ledger's own **16 KB quantum** is what delivers the stated intent,
  and its check (e) refuses any other value — so the quantisation was not a preference to weigh but the
  only admissible reading. **When a number is "the current value", ask what the predicate does on the
  next byte.**
- 🔴 **A GUARD WHOSE TWO KEY SPACES BECOME DISJOINT BY CONSTRUCTION IS SILENTLY INERT, AND A GREEN RUN
  CANNOT TELL YOU.** A collision check was `set(A) & set(B)`; re-keying one side to digests made the
  intersection empty on **any** tree, forever, while the test still read as coverage. Caught by its own
  author asking *what else reads this by key SHAPE* — measured under a planted collision: old spelling
  **0**, resolving spelling **1**. **After changing a key's representation, enumerate every reader and
  ask of each whether it can still SEE what it claims to.**
- 🔴 **THE COMMIT-MESSAGE CHANNEL IS GATED BY NOTHING, AND THREE ROUNDS OF LEAK-CHECKING WERE EACH
  CORRECT AND EACH BLIND TO IT.** One round reported "leaks: clean" over the added lines; another
  scanned 1,545 tracked files with a validated positive control. Both were true of the surface they
  read — **a commit message is not a tracked file**, and there is no `commit-msg` hook. The scrub's own
  completeness paragraph therefore reads as "the disclosure is closed" while being silent about the
  channel the same PR created.
- ⚠ **A MUTATION SWEEP BELONGS AFTER A CLEAN ROUND, NOT BESIDE EACH ONE.** It was started and abandoned
  **three times**, because every fix round superseded the tree it was measuring, and a sweep of a
  superseded tree is evidence about nothing that will ship. ~111 rows × one full suite each is hours.
  **Treat it as the pre-merge gate it is.**

- 🔴 **A TRAILER-SEEDED ARC IS A LOWER BOUND ON ITS SESSIONS, NEVER THE ARC — MEASURED AT 15% MISSING.**
  Reconstructing this arc from `Claude-Session-Id` trailers on the doc found **11 sessions**; keyword
  search over the whole transcript store found **13**. The two missed are the EARLIEST in the arc, and
  that is structural rather than luck: a trailer seed can only see sessions that WROTE the doc, so it
  systematically under-samples early-arc sessions, sessions that did code work and let a sibling write
  the doc, and sessions killed by a limit before the doc commit — exactly the ones whose instructions
  are likeliest to have been dropped, because nothing they were told ever reached the artefact the next
  session reads. ⚠ **And the arc-scoping tools cannot see this arc at all**: both `--arc` selectors
  resolve docs only through four `$REPO` env handles and this repo has none, so they exit 3/5 —
  "nothing was measured", not "the arc is empty". One resolver, two consumers, one blind spot.
- 🔴 **A STANDING OPERATOR CONSTRAINT WAS RE-ISSUED FOUR TIMES ACROSS TEN DAYS AND DID NOT HOLD ONCE.**
  *"skip audit, merge and proceed"* (09-16) → *"enough audits, merge and proceed"* (09-23) → *"merge
  and proceed, fix-forward any issues that arise"* (09-23) → *"dont wait, merge and proceed"* (09-26).
  The measurement that makes it undeniable: of 265 messages in the arc's own corpus, **189 (71%) are
  injected task-notifications, the large majority audit-ladder rounds.** ⚠ **The earliest instance was
  invisible to the trailer seed**, so from the doc alone the pressure looks like it began 09-23; it
  began a week earlier. 🔴 **A DOC-AUTHORED CLOSING CONDITION IS NOT OPERATOR AUTHORITY** — rank 15
  demanded "round 0 then the nine axes", a previous session wrote that, and it was itself part of the
  self-authored chain its own round 0 flagged. A ladder ran five rounds against a standing instruction
  to stop. **When a doc's closing condition and the operator's own words disagree, the words win.**
  ⚠ A second constraint with the same shape: *"ask me clarifying questions first"*, five times in four
  sessions, one day apart — retyped every session because it never held.

- 🔴 **A SUBAGENT'S FINDING WAS RIGHT ABOUT THE OBSERVABLE AND WRONG ABOUT THE MECHANISM, AND
  RE-DERIVING IT MYSELF IS THE ONLY REASON A FALSE DEFECT WAS NOT FILED.** The browser pass reported
  *"the share page says `no such scope, or it is not yours to share` for BOTH scopes"* — true, and it
  reads as the share flow being broken for its own owner. The discriminating control took one command:
  `?scope=<display name>` → **404** for every scope, `?scope=<scp_ id>` → **200** for both owned ones
  and 404 for an unowned one. So the refusal is the ID/name distinction working exactly as
  `render.go`'s comment says it must, not an authority defect. 🔴 **AND THE SECOND CONTROL IS WHAT
  STOPPED THE NEXT WRONG HEADLINE**: before writing "the share flow is unreachable", bare `GET /share`
  was tried and answers **200** with a working index. The true finding is a NAVIGATION gap, which is
  much narrower than either draft. **Two rival mechanisms produced the same 404; only a request that
  distinguishes them says which.**
- 🔴 **A `nix build` GCROOT IN THE REPO ROOT CAN BE RETIRED WITHOUT LOSING IT, AND THAT IS THE MISSING
  HALF OF AN ENTRY THAT ONLY SAID "DO NOT REMOVE IT".** `result` here was a gcroot for the Tailwind
  toolchain, and it takes `leakscan` to exit 2 (and eight of this repo's own tests red) because
  `git ls-files --others` yields the symlink and the scanner reads it as a file. Deleting it would let
  the store path be collected; the recorded advice was replace-then-remove, with no recipe. The recipe
  is one command: `nix-store --add-root <a path outside the repo> --indirect --realise <the store
  path>` re-registers the SAME path under a new root, and only then is `rm result` free. Done this
  session; the repo root is now clean of gcroots.
- 🔴 **REMOVING THE GCROOT DID NOT MAKE `leakscan` GREEN, AND THE RESIDUAL IS THE OTHER HALF OF THE
  SAME FILED DEFECT.** rc stayed **2** on `.claude/worktrees/agent-…/` — **nine** abandoned agent
  worktrees, all `dirty=0`, none with any process cwd'd inside (checked by reading `/proc/*/cwd`), with
  branches for work that has all merged. **They were NOT removed**: this document's own entry says an
  agent worktree is not yours to remove, and the widest reading of that covers an abandoned one, since
  "abandoned" is a judgement and "clean" is not the same as "finished". Recorded here so the next
  session does not re-measure it: the gcroot half is closed, the worktree half is open, and the two
  were always one defect.
- **Decision (operator, this session): run rank 18's validation pass against a hand-run instance from
  `main` and keep the deployed image bump a SEPARATE decision.** Offered and declined: bumping the
  deployed image first and validating there (answers "not just localhost", but the share flow still
  has nobody to share with on that surface), and doing both.
- ⚠ **A SECRET HANDED TO A DISPATCHED AGENT BY FILE PATH STILL REACHED THE LOG, THROUGH THE TOOL'S OWN
  USAGE ERROR.** The brief deliberately passed a credential as a path to read rather than as a value,
  to keep it out of the brief file. The agent then called `browser type` with the value as an extra
  positional; the CLI refused — and its refusal message **echoed the value**, which the dispatch log
  captured verbatim. The credential was throwaway and its world loopback-only and ephemeral, so the
  cost here was zero, but the shape is general: **a tool that prints your argument back at you on a
  usage error turns "passed by path" into "passed by value".** Prefer a tool that reads the secret
  itself; failing that, treat the log as secret-bearing and destroy it with the world.
- ⚠ **8103 WAS ALREADY TAKEN, WHICH IS THE PREDICTED COLLISION ARRIVING ON SCHEDULE.** This document
  records that `## How to verify` naming a fixed port is the cause, and that the fix is `ss -ltn` first
  plus handing over the PID and session id. Followed: this session's instance is **127.0.0.1:8147, PID
  3427887, session `bb38a675`**. The occupant of 8103 was left alone. **The recipe still names 8103 —
  the prediction has now been confirmed twice and the recipe has still not been changed.**

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

📄 **Twelve closed blocks are now in `claudedocs/handoff-cairn-control-plane-archive.md`** — five
moved at the first prune, six at the second, and one at the THIRD: the `cairn#117` × `cairn#108`
merged-tree block, settled
when both PRs merged and the gate ran (recorded in `State now`). They are resolved or
superseded; the archive keeps them verbatim, because a closed block's value is its measured
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
