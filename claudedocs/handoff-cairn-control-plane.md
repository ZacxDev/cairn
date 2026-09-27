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

- `main` @ **`d003708`** (#133) — re-read rather than quoting it; a handoff can never record its own
  merge.
- ✅ **THE ARC REMAINS CLOSED.** Re-run last session on `38bea8b`, pinned toolchain: `pytest tests -q`
  **2194 passed / 0 failed**, `go vet ./...` rc 0, `default = mkGoClient`. **Not re-run this session**
  — the change was `internal/ui` only and `go test ./...` was green on it. Ranks 18–26 are the
  successor arc.
- 🔴 **THE SHARE FLOW WAS DEPLOYED, AUTHORISED AND UNREACHABLE — REPORTED AS MISSING, AND IT WAS NOT
  MISSING.** Measured on the running pod before changing anything: `GET /share` → **401, not 404**;
  startup line `sharing writable`; journal holding **1 user, 1 project, 26 `scope-created`, 1
  `member-set` at `owner`, 0 grants**, so `control.Resolve` had 26 administrable scopes. **Nothing
  linked to it**: `Href(SharePath)` = **0** against a positive control of `Href(RootPath)` = **3**.
  ✅ Fixed in **#133 → `d003708`**, deployed as **`40797d1ef`** on the deployment-manifest repo's `trunk`. Full lesson,
  including why every gate stayed green, is the `cairn`/`ui` index entry — `cairn recall --ref ui`.
- ✅ **DEPLOY VERIFIED BY CONTENT, AND THE IMAGE NOW MATCHES `main`** (it was 10 commits behind). Pod
  READY on `sha-d003708…`; the image was confirmed anon-pullable BEFORE the bump (`sha256:c1b8f37a…`)
  **with an absent tag refused rc 2** as the control; the served stylesheet is **byte-identical
  (`cmp`) to `main`'s `app.css`** and carries `.nav-share a {`; the PUBLIC `/sign-in` carries **0**
  hrefs to `/share`. 🔴 **NOT VERIFIED: the authenticated header link in a browser** — unreachable
  without a session, so it is the operator's click. Rank 9.
- 🔴 **`go test ./...` IS STILL RED ON THE BASE CLONE AND IT IS NOT THE TREE.** Re-measured: two
  `.claude/worktrees/agent-*`, `internal/envalias` **rc 1**, every offender path under them. Rank 23,
  and **`ZacxDev/cairn#135` is OPEN for it**.
- ✅ **#134 STILL OPEN** (`feat/ui-entry-raw-view`), re-tested against the moved base — rank 26. Not
  audited — rank 24.
- 🔴 **THE AMBIENT `go` IS NOT THE PINNED ONE:** bare **1.26.7**, `nix develop -c` **1.25.14**. No
  `.envrc` here. Run every Go gate as `nix develop -c`.
- ⚠ **NO TASK-BOARD FIELD — AN UNKNOWN, NOT A MEASURED ABSENCE.** Resolver exited **5**; an unknown
  session id answers 200 with an EMPTY ARRAY, and no positive control was run, so this zero cannot
  distinguish "touched no task" from "wrong id".
- ⏳ **OPEN OPERATOR CHOICE (1):** the capped card is LEFT-ALIGNED, not centred. Unchanged.
- ⏳ **OPEN OPERATOR CHOICE (2): the rank-9 local instance is STILL RUNNING and was never released.**
  pid **2728234** on `127.0.0.1:8103`, the old `cairn-ui-1838b82` build over a synthetic **two-user**
  world in a dead session's scratchpad, journal unconsumed (10 lines, 0 `granted`). It is the only
  world that HAS a second co-member — exactly what the deployed one lacks (rank 25). **Stop it and
  release `cairn-control-plane-9`, or keep it as the two-user case?** ⚠ That claim **LAPSED ON TTL
  mid-session while the work was live and was re-taken** — expiring is not releasing.

## Next steps (ranked)

🔴 **NUMBERING IS STABLE — every number below KEEPS ITS LINE even when the item is done, because a
rank is half a `claim-work` slug: delete the number and `claim-work --slug-for <doc> <n>` resolves to
an item nobody can find.** Re-ranking re-points every live claim.

🔴 **AND THESE ARE A NEW ARC, NOT THE CLOSED ONE'S REMAINDER.** Say so when you pick one up.

1. ✅ **DONE — #74 merged as `93d0f03`.** forcing: gate.
2. ✅ **DONE — merged as `562a4f6f`.** forcing: gate.
3. ✅ **DONE — P7 merged as `c0f5b06`.** forcing: none.
4. **P8 — retire the Python oracle.** **Closing condition:** a real read AND a real write against
   the live pod from **at least two distinct hosts**, recorded, AND no open defect naming the Go
   client or `packages.default`. **BACKSTOP: if that has not happened by 2026-11-01, P8 opens
   anyway and the residual risk is accepted EXPLICITLY, in writing.** Checked by `cairn doctor`
   from two hosts plus `gh issue list`. ⚠ Sized, not measured: ~33,000 deletable lines, 3 of 6 CI
   jobs, ~10 paired-ledger guards.
   forcing: none
5. ✅ **DONE — `cairn-server -issue-credential`, in #76.** forcing: gate.
6. ✅ **DONE.** forcing: user.
7. ✅ **DONE — rename landed as `56cc56e` (#69).** forcing: user.
8. ✅ **DONE — rule (o) merged as `c4490f07` in the handoff-tooling repo.** forcing: incident.
9. ⏳ **THE SHARE FLOW'S HUMAN VERIFICATION — THE NAVIGATION HALF IS NOW DONE AND DEPLOYED; WHAT IS
   LEFT IS ONE CLICK AND ONE MISSING CO-MEMBER.** ✅ **#133 MERGED as `d003708` and DEPLOYED as
   `40797d1ef`** (the deployment-manifest repo's `trunk`, where the commit IS the deploy): every framed page now renders a
   `Sharing` link, and `SharePage` goes through `shell` so the flow has a way back. The deployed
   surface serves it — **verified by CONTENT, not by the rollout**: the served stylesheet is
   byte-identical (`cmp`) to `main`'s `app.css` and carries `.nav-share a {`, and the PUBLIC
   `/sign-in` page carries **0** hrefs to `/share`. 🔴 **THE AUTHENTICATED HEADER LINK IS THE ONE
   THING NOBODY HAS CONFIRMED IN A BROWSER** — it cannot be reached without a session, so it is
   still the operator's click. ⚠ **AND THE PICKER STILL HAS NOBODY TO OFFER:** one user in the
   journal, and `Candidates` narrows to co-members, so the subject select shows only the project.
   Rank 25 is that blocker; this item is *navigable but not exercisable* until it moves.
   forcing: user — the operator reported the feature missing from the deployed surface, and the
   remaining browser step was reserved to a human.
10. ✅ **DONE — merged as `901b77d` (#104), verified on a real publish run.** forcing: user.
11. ⏳ **OPEN AS the handoff-tooling repo's `#1867`**, held because that repo's `main` is red for an
    unrelated reason. **CLAIMED** (`cairn-control-plane-11`).
    forcing: incident.
12. **CORRECT TWO FILES THAT ASSERT THAT REPO'S CI CHECKS BLOCK A MERGE.** They do not
    (`required_status_checks` → 404). Both wrong in the PERMISSIVE direction.
    forcing: gate.
13. **COMPLETE A GITHUB SIGN-IN END TO END ON THE DEPLOYED SURFACE.** ⚠ Precondition: sign-in
    resolves the token's `sub` against a user the control plane ALREADY holds; an unknown subject
    is refused by design and all three causes collapse into one 401, so **the pod's log is the only
    place the mechanism exists.** ✅ The provider button IS armed on the deployed surface — the
    precondition, not the verification.
    forcing: user.
14. ✅ **DONE — `#117` merged as `9c24bc4`.** 🔴 Its ladder stopped on the ATTRIBUTION GATE — two
    consecutive payload-zero rounds — **NOT on a clean round**; a later reader must not upgrade
    that. forcing: gate.
15. ✅ **DONE — rule (p) MERGED as `b4233ea9`.** Stopped by the attribution gate, so **four findings
    were FILED rather than fixed — open, not closed** — and five mutation rows remain unscored.
    forcing: gate.
16. **DECIDE THE TAILWIND BUILD-TOOLCHAIN QUESTION.** ⚠ Its footprint GREW AGAIN: #134 regenerates
    `internal/ui/app.css`, making **three PRs in two sessions** that had to rebuild it, and the
    #133 × #134 merged-tree check had to regenerate it a fourth time to prove a textual merge was
    safe. Keep-or-replace, not a gate on a PR.
    forcing: user.
17. ✅ **DONE — `cairn#108` merged as `84642ff`**, verified by CONTENT because ancestry is false
    after every squash. forcing: gate.
18. ✅ **DONE.** 🔴 Two of its three headline findings were WRONG and only re-derivation caught
    them — **a dispatched pass is a witness, not a verdict.** forcing: user.
19. **RUN THE WHOLE DESIGN THROUGH `/the-algorithm`.** Asked 09-23, with the design stated in the
    same message. 🔴 **Still 0 hits for `the-algorithm` across this doc, the archive and the plan.**
    **Closing condition:** a recorded pass, question-requirements → delete → simplify in that order,
    or a written line saying the fact-rot pass discharged it.
    forcing: user.
20. **THE ARCHIVE IS OVER ITS LEDGER ALLOWANCE AND NOTHING WILL NOTICE.** 174,237 B against a
    grandfathered 147,456 B. A FOREIGN ledger entry sits outside every one of that gate's corpus
    checks by construction, so no check fires either way. **Closing condition:** both entries
    re-derived from measured size, or a written line exempting foreign entries from tightness.
    forcing: gate.
21. ✅ **DONE — API pod `sha-a0c4d07` → `sha-953ad36`, verified by a DISCRIMINATOR not by health:**
    a second snapshot fetch answered `304 not-modified` (P7 conditional sync), which the old image
    cannot do, confirmed on both sides. ⚠ **A SECOND INSTANCE EXISTS AND WAS NOT TOUCHED.**
    forcing: user.
22. ❌ **WITHDRAWN — THE EDGE CACHE TTL IS LEFT AS IT IS**, on a later operator instruction that
    reversed an earlier one in the same session. 🔴 **DO NOT RE-PROPOSE THIS AS THOUGH IT WERE
    UNDECIDED.** Accepted cost: the versionless `/static/app.css` keeps answering `max-age=14400`
    against the origin's `300`, and any future sub-four-hour asset inherits the override silently.
    Not at risk: the content-hashed stylesheet and HTML. **Closing condition: none — closed by
    decision, not by work.**
    forcing: user.
23. **FIX THE ROOT-WALKING GUARD THAT REDDENS ANY CLONE HOLDING A NESTED WORKTREE, THEN REMOVE THE
    TWO STALE ONES — IN THAT ORDER.** `internal/envalias/envalias_test.go`: `SkipDir` on `.claude`
    (`:286` covers only `.git` and `tests`), and make the ledger exemption at `:294` a path SUFFIX
    rather than an exact equality against `<root>/internal/envalias`. Deleting the worktrees first
    fixes today and leaves the guard defective, which is why the order is stated. The stale pair is
    `.claude/worktrees/agent-aa77eb09fda6ae3f3` and `…/agent-ae3dc7c8402873ce8`, both on MERGED
    branches (#130, #131). ⚠ CI is green because `.claude/` is untracked — a LOCAL red only, on
    exactly the command the arc's closing condition runs.
    forcing: gate — `go test ./...` is one of the two commands that closing condition runs.
24. **DECIDE #134 — AUDIT THEN MERGE, OR SEND IT BACK.** `ZacxDev/cairn#134`, the entry page's
    rendered/raw pair. Not yet audited: `/audit-pr 134` **ROUND 0 FIRST** (requirements and
    deletion — the only round that can conclude *close this, do not audit it*, and only actionable
    while the merge decision is open), then the correctness axes. ⚠ **Coordinate with #133**:
    merged-tree tested green this session, but against `38bea8b` — **the base MOVES when either
    lands and a merged-tree test does not survive its base moving.** Re-run it, and regenerate
    `app.css`.
    forcing: user — the operator asked for the feature in this session.
25. **GIVE THE DEPLOYED SHARE FLOW SOMEBODY TO SHARE WITH.** The flow is now reachable and
    authorised on the deployed surface and still cannot complete: the control journal holds **1
    user, 1 project, 26 scope-created, 1 `owner` membership, 0 grants** (read off the live pod), and
    `internal/ui/sharing.go`'s `Candidates` narrows to co-members by a stated SECURITY decision — a
    picker listing every user would turn one scope's admin rights into a directory of the
    deployment. So the select offers only the project itself. **Two ways, and they are not
    equivalent:** provision a second user + `-set-member` into the same project in-cluster (cheap,
    proves the flow end to end, needs an in-cluster `cairn-server` invocation against
    `/var/lib/cairn-ui/journal.jsonl`), or build the invite flow (P6 — the narrowing's stated lift,
    a real feature). ⚠ The journal is append-only and on the UI-owned PVC, so a botched hand-append
    is not undoable — and `AGENTS.md` records a hand-append being exactly how a 64-character secret
    once reached a journal. Prefer `-set-member`.
    forcing: user — the operator asked for working share functionality; it is navigable but not
    usable, which is the same report one step further along.
26. **RE-TEST #134 AGAINST THE MOVED BASE BEFORE MERGING IT — ALREADY DONE ONCE, AND IT EXPIRES
    AGAIN.** The previous session's #133 × #134 merged-tree measurement said it "expires the moment
    either PR lands"; #133 landed, so it was **re-run this session against `d003708`**:
    `git merge-tree` rc 0, a real `git merge` rc 0, and on the merged tree `go test ./...`
    **19 ok / 0 FAIL**, both features' guards green, and `checks.ui-stylesheet-is-current` rc 0.
    🔴 **THE GENERATED `app.css` IS WHY THIS NEEDS MEASURING RATHER THAN REASONING:** both PRs
    regenerate it from `tailwind.css`, so a textually clean merge could have produced a stylesheet
    matching NEITHER input. It did not — the merged digest differs from both parents, which is also
    what proves the currency check evaluated instead of returning a cached green. ⚠ **That
    measurement now expires when #135 or anything else touching `internal/ui` lands.**
    forcing: gate — `checks.ui-stylesheet-is-current` is a build failure through nix, so a bad merge
    breaks the build rather than degrading quietly.
27. 🔴 **PRUNE THIS DOCUMENT — THE SIZE RATCHET NOW BLOCKS EVERY HANDOFF INTO IT, MEASURED.** This
    session's update was REFUSED with `status=size-ratchet`: **117,103 B against a grandfathered
    allowance of 98,304 B**, and `handoff-audit.py` measures only **5,162 B net evictable** (2,373 B
    of 14 completed ranks + 5,589 B of retracted/dead-end bullets, less 200 B per rank whose NUMBER
    must stay). The delta was trimmed from **+10,600 B to +5,385 B** and still exceeded what eviction
    could free — so a MINIMAL honest handoff no longer fits, and this round landed only by
    `--override-size-ratchet`. 🔴 **`Gotchas` is 73,869 B — 63% of the file** — and it APPENDS, so the
    tool structurally cannot shrink it for you. **Eviction means MOVE to
    `claudedocs/handoff-cairn-control-plane-archive.md`, verbatim, leaving a pointer** — nothing in
    the rule can tell a deletion from an eviction, and the arithmetic is identical. ⚠ Rank 20 says
    the ARCHIVE is over its own ledger allowance with nothing to notice, so do not treat the archive
    as free. **Closing condition:** a proposal run on this doc returning `status=proposed` rather
    than `size-ratchet`, with no override.
    forcing: gate — `handoff_doc.py` exit 14 refuses the write; the next session hits the same wall
    on its first handoff attempt.

## Defects (batched)
- ⚠ **CLOSED ENTRIES MOVE TO THE ARCHIVE, THEY DO NOT ACCUMULATE HERE.** This section REPLACES, so
  every closed entry left in it is retyped by hand each round until somebody drops it. The LESSONS
  from a closed entry belong under `Gotchas`, which appends; the entry itself belongs in the
  archive. ⚠ **The no-JavaScript-claim entry was DROPPED by this update rather than retyped** — its
  closing condition was *"#130 merged"* and #130 merged as `b574a61`; its lessons are under
  `Gotchas`, which is the condition for dropping one.
- 🔴 **A ROOT-WALKING GO GUARD GOES RED ON ANY CLONE THAT HOLDS A NESTED WORKTREE, AND IT NAMES REAL
  SOURCE FILES WHILE DOING SO.** Mechanism and remedy are rank 23. The part that belongs here is the
  CLASS: a guard that walks from the repo root and skips only `.git` and `tests` cannot tell this
  repo's files from a checkout of this repo sitting INSIDE it. **Breadth measured: exactly one Go
  guard walks from the root**, and its `.claude` skip count is 0. ⚠ The leak scanner has the same
  blindness and the doc already records it; what is new is that this one reports **FAIL** where
  leakscan reports "could not vouch". **Closing condition:** rank 23 merged.
- 🔴 **THE EDGE OVERRIDES THE ORIGIN'S CACHE DECISION, BLAST RADIUS EXACTLY ONE ROUTE.** ❌ **CLOSED
  BY DECISION, NOT BY WORK — AND THE DECISION REVERSED ITSELF WITHIN THE SESSION.** Nothing was
  changed at the edge and **this is not an open defect**; it is an ACCEPTED one (rank 22). Retained
  only so the accepted cost is not rediscovered as news: the versionless route keeps answering
  `14400` against the origin's `300`. ⚠ Two readings of the mechanism remain consistent with the
  observable, and since the setting is being left alone, that will stay unresolved.
- 🔴 **THE AMBIENT GO TOOLCHAIN IS NOT THE PINNED ONE, AND `AGENTS.md` SAYS IT IS.** Re-measured:
  bare `go version` → **1.26.7**; `nix develop -c go version` → **1.25.14**. Every local `go
  vet`/`go test` outside the devShell is a green about a toolchain this repo does not ship, and
  nothing warns. **Closing condition:** either a `direnv`/`.envrc` putting the pinned toolchain on
  `PATH` here, or a written line in `AGENTS.md`'s verification section saying the Go gates must run
  as `nix develop -c …` — plus a decision on whether anything should refuse a bare run.
- 🟡 **THREE FILES ARE NOT `gofmt`-CLEAN WITH NOTHING IN CI GREPPING IT — AND FOR ONE THE CAUSE IS
  NOW MEASURED AND THE REMEDY IS "DO NOT".** `internal/ui/render.go`'s ENTIRE gofmt diff is two
  lines: gofmt rewrites a literal double-backtick in a doc comment into a curly quotation mark
  (`go/doc/comment`'s old-style quoting) — **inside a comment whose whole subject is backticks**, so
  `gofmt -w` would make it assert something false about its own topic. `browse_test.go`'s is an
  ordinary string-concatenation spacing nit. Measured at `origin/main`, so neither is #134's.
  **Closing condition:** exempt `render.go` in writing and fix the other two, or accept all three
  and say why nothing gates it.
- 🟡 **FOUR OF ELEVEN JOURNAL EVENT KINDS HAVE NO WRITER; THE SELECTION RULE IS STATED SO THE
  NUMBER IS REPRODUCIBLE.** A kind has a writer iff non-test Go constructs it. Re-derive with
  `find . -name '*.go' -not -name '*_test.go' -not -path './.claude/*' -print0 | xargs -0 grep -ohE 'Kind:\s*(control\.)?Event[A-Za-z]+'`
  against `control.AllEventKinds` (11). **Constructed (7):** `user-created`, `project-created`,
  `scope-created`, `member-set`, `credential-issued`, `granted`, `grant-revoked`. **Writer-less
  (4):** `member-removed`, `scope-renamed`, `scope-moved`, `credential-revoked`. ⚠
  `granted`/`grant-revoked` are constructed only by the browser surface. ⚠ Hand-appending is the
  shape that let a 64-character secret into the journal (rank 5), and `-issue-credential`'s own
  output still says *"append a `credential-revoked` record BY HAND"*. **Closing condition:** a
  writer for `credential-revoked`, or a written line saying hand-append is the intended interface
  and naming where its schema is documented.
- 🟡 **THREE FILED BY #54's AND #57's LADDERS REMAIN.** 🔴 **(b) NAMES THE WRONG FILE.**
  `internal/control/tokenfile/source.go` was fixed by `c47636b`; the LIVE stale copy is
  **`internal/control/filestore.go:54`** — *"This module has no `require` block and `flake.nix`
  passes `vendorHash = null`"* — both halves false since #55. (c)
  `internal/report/testdata/reader_fixtures.json` contains **no `[cairn: …]` trailer at all** (0
  hits against 8 in `server.py` as a positive control), so the differential reader fixture never
  exercises attribution rendering. (d) Whether `cairn-ui` should ever render through
  `internal/report` is **undecided and stated as open in `README.md`**. **Closing condition:** one
  PR for (b)–(c); a written line for (d).
- 🟡 **`checks.default-is-the-go-client` IS INSENSITIVE ON THE PYTHON SIDE.** `mkCairn`'s pname is
  already `cairn`, so a `meta.mainProgram` removed *there* leaves the base-name assertions green.
  **Closing condition:** close it, or a written line saying why not.
- 🟡 **COUNTS QUOTED IN PROSE THAT NOTHING ASSERTS ON** — now **`tests/test_parity_harness.py`'s
  floor alone**. The pattern is settled: a count that moves whenever an artefact is regenerated gets
  DELETED, one that should never move gets PINNED
  (`tests/test_control_mutant_count_is_pinned.py`). Deciding which a number is, is the work.
  **Closing condition:** pin the parity floor, or a written line saying why not.
- 🔴 **A CHANGELOG ROW IS BORN WITH THE WRONG ANCHOR, STRUCTURALLY.** The anchor is the SQUASH
  commit, unknowable while the PR carrying the row is open, so every row is written wrong and is
  only correctable by a follow-up. #69 shipped anchored to a commit the squash discarded (fixed in
  #78); #90 shipped a visible `<UNFILLED>` — the honest choice — fixed in #92. ⚠ **Nothing gates
  it.** **Closing condition:** a check refusing a `CHANGELOG.md` anchor absent from `main`, or a
  written line accepting the two-step.
- 🟡 **P7's TWO BINDING CLAIMS ARE NOT IN `AGENTS.md`, AND THE REASON IS THE BUDGET.** The two that
  belong there: the validator is a digest of the **uncompressed** tar (not gzip, not an
  authorization epoch), and a `304` is a FOURTH read state beside
  `live`/`cached`/`scope-empty`/`store-unreachable`. **Closing condition:** an eviction PR frees
  ≥400 B, then both sentences land and `tests/test_agent_instructions_weight.py` exits 0 with both
  present.
- 🟡 **THE DEPLOYMENT MANIFEST'S NODE-AFFINITY COMMENT IS STALE.** It keeps the pod off the off-LAN
  burst node *because the LAN registry does not resolve there*; the pod now pulls from ghcr, so that
  reason is void while the affinity may still be wanted (the PVC is ReadWriteOnce local-path). **A
  comment is a claim too. Closing condition:** the comment states the true reason, or it goes.
- 🟡 **`tests/dualrun/` cannot see image drift, structurally.** It runs the TREE's `server.py`;
  nothing compares the Go server against the artefact actually serving. **Closing condition:** decide
  whether a deployed-artefact arm is worth owning, or write the line saying it is not.
- 🟡 **THE ENTRIES PAGE OFFERS NO WAY TO REACH THE SHARE FLOW.** `GET /` carries **0** occurrences of
  the word "share"; positive control, the share index itself contains **8**. The flow is not broken —
  bare `GET /share` answers **200** — but **nothing navigates there**. ⚠ **A SECOND HALF, A MESSAGE
  DEFECT RATHER THAN A LOGIC ONE:** `?scope=` is keyed on the scope **ID**, never the display name —
  deliberate, and `render.go` says why. But a human who hand-types the name they can see gets **404**
  and the words *"no such scope, or it is not yours to share"* for a scope that **is** theirs to
  share. The refusal is correct and its sentence is false. ⏳ **The navigation half is IN FLIGHT as
  `ZacxDev/cairn#133`.** **Closing condition:** #133 merged, plus a decision on whether the
  name-keyed refusal should say something true.
- 🟡 **`AGENTS.md` + `CLAUDE.md` ARE EFFECTIVELY FULL — three bytes of headroom.** Any new sentence
  there requires evicting an existing one — a decision about somebody else's claim, not a formatting
  change. ⚠ **A false zero worth keeping:** `grep -c` on a phrase that WRAPS ACROSS A NEWLINE answers
  **0**. A line-oriented grep is a claim about lines, not about the file; flatten whitespace, and keep
  a positive control beside the count. **Closing condition:** an eviction PR that frees a stated
  number of bytes, or a written line accepting that the file is closed to new claims.
- Everything previously listed stands unchanged: #38's three residuals; `ScopeByNameIn`
  raw-vs-folded; P4 round 5's two prose defects; the degenerate-spelling limb; PR #15's six findings;
  the four deferred Go/oracle divergences; `server/seed.sh:110`'s `cd`; `-race` gated in one tier
  only; and P4 rounds 1 and 3's guards absent from the persistent battery.

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

- 🔴 **THE BROWSER PASS'S OWN REPORT CARRIED TWO WRONG HEADLINES OUT OF THREE, AND BOTH WERE RETIRED BY
  ONE EXTRA REQUEST EACH. A DISPATCHED PASS IS A WITNESS, NOT A VERDICT.** (a) It filed *"every unknown
  path is auth-gated — 401, not a 404"* as a SURPRISE. Measured with the discriminating pair the report
  never sent: **anonymous → 401, AUTHENTICATED → 404**, with `/healthz` → 200 as the control proving the
  cookie jar was attached. That is exactly the gate-order exception `routes.go` documents, so the
  finding is retired, not filed. (b) It filed the share page refusing `?scope=<display name>` — true,
  and correct by design. **The one real navigation finding it did NOT make is the one that matters**,
  because it reached `/share` by typing the URL and so never asked how a human would. **A pass that
  navigates by address bar cannot see a missing link.** Brief the next one to reach every page by
  CLICKING from the landing page, and to report any page it could only reach by typing.
- ⚠ **AND THE PASS RAN WITHOUT TAKING THE OPERATOR'S SCREEN, WHICH IS A PROPERTY OF THE TOOL WORTH
  KNOWING BEFORE THE NEXT ONE.** `browser open` creates the tab in the BACKGROUND and `screenshot`
  captures an occluded tab, so a full multi-page walk needs **zero** raises — the raise is `browser
  activate` and nothing else. Twenty-plus navigations, seven screenshots, a human working in the same
  browser throughout: **zero workspace switches and zero focus changes**, which is the stronger of the
  two claims that rule distinguishes.
- 🔴 **A HANDOFF WRITE REFUSED `leak-refused` ON AN ARTEFACT, AND THE FRESH-WORKTREE ROUTE IS THE FIX
  THAT DOES NOT SPEND AN OPERATOR OVERRIDE.** The gate refuses on ANY non-zero scanner exit, and the
  base clone sits at exit 2 on abandoned agent worktrees — so the documented remedy,
  `--leak-pre-existing-approved`, is an OPERATOR decision being burned on a scanner artefact, which is
  exactly how an override becomes reflexive. Instead: `git worktree add <path> -b <branch> origin/main`,
  run the scanner there to confirm **rc 0 with 0 `COULD NOT READ` lines on the same tree**, then run
  `handoff_doc.py --repo <that worktree> --confirm --push` and open a PR from the branch. The gate then
  vouches for the delta on its own terms — `leakscan: … exited 0 with this delta written` — which is a
  STRONGER result than an approved override, not a workaround for it.

- 🔴 **A GUARD ON A REFUSAL IS NOT A GUARD ON A LEAK, AND THE GREEN VERSION SHIPPED FIRST.** The new
  browse surface's first authority guard asserted a uniform refusal in BOTH directions with BOTH
  positive controls, and was green — while a `store.Unrestricted()` mutant in `StoreSource.Visible`
  **survived** it. The per-scope refusal held (a foreign scope resolves to an empty id, which
  `pickScope` refuses), so every assertion passed **while the ROOT page listed the other tenant's
  scope name, entry refs and counts as an unlinked card.** It died only once the guard asserted
  root-page CONTENT. **Ask what the mutant makes VISIBLE, not only what it makes reachable.**
- 🔴 **CASCADE ORDER IS NOT VISIBLE IN A GREP, AND A CORRECT-LOOKING RULE WAS DEAD FOR ITS WHOLE
  EXISTENCE.** `--breakpoint-ultra` was spelled `2000px` among `rem` defaults. Tailwind v4 orders
  breakpoint variants by resolved size and cannot rank a `px` length against `rem` without assuming a
  root font size, so the `ultra` blocks were emitted **FIRST**; media queries add no specificity, all
  four matched, and the last-declared `80rem` rung won. Measured both ways: pre-fix emitted order
  `2000px, 2000px, 40rem, 64rem, 64rem, 80rem`; post-fix `40rem, 64rem, 64rem, 80rem, 125rem,
  125rem`. **Keep every breakpoint in ONE unit, and verify the EMITTED order in the generated CSS —
  the source cannot show you this.** ⚠ I first called the rule a selectorless orphan and was WRONG:
  it was valid CSS nesting inside `body`. Retracted before it reached a subagent, which is the only
  reason a wrong diagnosis was not implemented.
- 🔴 **A LAYOUT GATE THAT ASSERTS "NO HORIZONTAL OVERFLOW" IS STRUCTURALLY BLIND TO "THE PAGE IGNORES
  THE VIEWPORT" — A TOO-NARROW CONTAINER NEVER OVERFLOWS.** `uiaudit` reported `0 overflow` across 65
  captures at five widths and was CORRECT, while content used 41% of the display. The remedy is a
  guard on content width as a **FRACTION OF VIEWPORT**, which is a different claim and must not
  replace the overflow one. ⚠ **And the floor's value is itself a decision:** 45% refused the dead
  rung but would have stayed green on a silent revert to the previous cap — i.e. green on a
  regression against an operator decision. It was re-derived to 80%.
- 🔴 **"VERIFIED" KEPT MEANING THE ARTEFACT WHEN THE QUESTION WAS THE SERVED PAGE — THREE TIMES IN ONE
  SESSION, IN THREE SHAPES.** (a) A healthy pod, green gates and reproducing probes while the image
  was **13 commits stale**. (b) A stylesheet verified at origin while every warm browser held the old
  one for four hours — *"the edge refreshed"* is not *"clients refreshed"*, and I reported one as
  covering the other. (c) `document.scripts.length == 0` asserted across 65 captures of a **hermetic
  local pod** while the served page carries Cloudflare's injected script. **Name which artefact your
  reading is about, every time: the binary, the origin, the edge, or the client.**
- 🔴 **A PIPE EATS THE EXIT STATUS, AND IT COST ME A FALSE DEFECT AGAINST A CORRECT GATE.** I ran
  `python3 <checker> 2>&1 | tail -15; echo rc=$?` and read `tail`'s status, then reported that the
  deployment repo's phase-1 gate "exits 0 while saying it could not run". Re-measured with no pipe:
  **rc 3** without its dependency, **rc 0** with it — exactly as its own docstring says, and for the
  reason the docstring gives. **Read `$?` off the command itself; a pipeline's status belongs to its
  last stage.**
- ⚠ **REMOVING A FINISHED AGENT'S WORKTREE MAKES THAT AGENT UNRESUMABLE, AND THAT TRADE WAS NOT
  WEIGHED.** Sweeping it restored the leak gate to rc 0 and simultaneously ended the ability to send
  the agent a two-line follow-up; the next round cost a fresh dispatch with a hand-written brief. The
  two goods are in direct tension — worktree isolation is mandatory AND an agent worktree reds the
  gate. ✅ `#127` closed the gate half (directory entries are now NAMED as skipped, and a genuinely
  unreadable FILE still exits 2, proven in both directions). **So sweep for tidiness, not for the
  gate — and not while you may still want the agent.**
- 🔴 **SQUASH-MERGING A STACKED PARENT MAKES THE CHILD `CONFLICTING`, PREDICTABLY, AND THE FIX IS
  `--onto`.** `main` gains the parent's content as a NEW commit while the child still carries the
  parent's originals, so the same work collides with itself. `git rebase --onto origin/main
  <old-base> <child>` replays only the child's own commits. ⚠ **And merge the parent WITHOUT
  `--delete-branch`** — GitHub auto-closes any PR whose base branch is deleted and refuses to reopen
  it, so the child's review thread is lost. Delete the parent branch only after confirming no open PR
  still targets it. ⚠ `mergeable` reads `UNKNOWN` for a while after the base moves; it settles.
- **Decision (operator, this session): the wide layout DEPLOYS to be judged rather than being
  iterated first.** Offered and declined: one more round (`auto-fit` → `auto-fill`, cap the entry
  CARD) before deploying, and reverting the cap to `112rem`. The measurement and the named wrong
  remedy are in `Defects` so the next reader does not revert the breakpoint instead.

- 🔴 **A ROLLBACK TARGET IS A CLAIM WITH A SHELF LIFE, AND THE API POD'S HAD BEEN WRONG SINCE THE
  CUTOVER — IN THE DANGEROUS DIRECTION.** The deployment manifest said *"ROLLBACK IS THIS LINE ALONE"*
  and named **the pre-cutover PYTHON pod on the internal registry** — a DIFFERENT IMPLEMENTATION, not
  a previous version of the running one — while the pod had long been serving the Go image from the
  public registry. The same file's own node-affinity note records that the internal registry does not
  resolve on the burst node, so it might not even pull. **Somebody rolling back under pressure would
  have changed implementation while believing they were reverting a version.** The rule, applied in
  the same commit that moved the tag: **re-point the rollback target at the PREDECESSOR whenever you
  move `image:`** — otherwise the first person to need it is the one who discovers it is wrong. ⚠ It
  was found only because the bump forced someone to read the paragraph around the line being edited.
- 🔴 **VERIFY A DEPLOY BY A DISCRIMINATOR, NOT BY HEALTH — READINESS PROVES THE DEPLOY, NEVER THE
  CODE.** This arc already records a healthy pod serving a 13-commit-stale image. The bump above was
  closed instead on a behaviour **only the new image can produce**: a second snapshot fetch answering
  `304 not-modified` (P7's conditional sync, absent before `#90`), read on BOTH sides — the client's
  `already current` line and the pod's own audit line. **Ask what the new artefact can do that the old
  one provably cannot, then make the probe be that.** A 200, a ready endpoint and a matching image tag
  are all satisfied by an image that does nothing new.
- 🔴 **I RENDERED THREE ROUTE STATES AND GENERALISED TO THE ROUTE SET, AND CALLED IT "VERIFIED ON THE
  REAL SERVED HTML, NOT INFERRED".** The claim was *"`.page-main > .card` reaches exactly the two
  broken pages"*. It reaches **four** page states: the two I opened, plus `NavigatePage` (`/scope` and
  `/entry` naming nothing) and `/?q=…`, whose `searchResults` renders `.card.results` as a direct
  child of `<main>`. Every word about what I measured was true; the quantifier was not. 🔴 **The
  phrase "verified, not inferred" is what made it dangerous** — it advertises the absence of the exact
  gap it contained, and a subagent had to read the render sites from SOURCE to find it. **For a claim
  about a SET, enumerate the sites; rendering the ones you happened to open is a sample.**
- 🔴 **A CORRECT CONCLUSION CAN REST ON A WRONG MECHANISM, AND THE MECHANISM IS WHAT THE NEXT EDIT
  USES.** I wrote that `.scope-grid .card` "overrides for cards inside the grid" and so protects it
  from a `.page-main > .card` cap. It declares only `margin-block: 0` and has **equal** specificity
  (0,2,0), so it could never override a `max-width`. The grid is protected because `>` is a **child
  combinator** and grid cards are **grandchildren** — they never match. The conclusion held; anyone
  flattening the wrapper on the strength of my explanation would have capped the whole grid. **State
  the mechanism only when you have checked it, and pin the mechanism in the guard** — #131's guard
  asserts the NESTING, not just the declaration.
- 🔴 **AN ANONYMOUS PROBE OF AN AUTHENTICATED ROUTE MEASURES THE REFUSAL, NOT THE PAGE — AND THAT IS
  HOW THIS ARC RECORDED A FALSE ZERO FOR TWO ROUNDS.** The claim *"`GET /` carries no script, so the
  injection is not even uniform"* was measured against a **12-byte `401` body**. Authenticated, the
  same route carries **two** scripts. ⚠ The tell was available the whole time and nobody read it: the
  response was **12 bytes**. **Before comparing two pages, check both are pages.**
- 🔴 **`grep -c` ON A `/proc/<pid>/cmdline` IS A CLAIM ABOUT THE WRAPPER, NOT THE PROCESS.** A
  three-part identity check before killing a scratch server reported `cmd_match=no` for a process
  whose cmdline plainly contained the string — because `grep` here is a FUNCTION wrapping ugrep and
  `/proc` cmdline files are NUL-separated. The correct read is `tr '\0' ' ' < /proc/<pid>/cmdline`
  then a shell `case`. ⚠ **The failure was in the SAFE direction this time and that is luck, not
  design** — the same wrapper returning a false POSITIVE would have killed the wrong process.
- 🔴 **CDP WILL NOT ATTACH TO A `file:` URL, WHICH MAKES "RENDER IT LOCALLY AND SCREENSHOT IT" A
  LOOPBACK-SERVER TASK RATHER THAN A ONE-LINER.** `nav file://…` succeeds, `--wake` and `screenshot`
  both fail `cdp_attach_refused:file:`, and the failure is per-op rather than at navigation — so the
  page looks fine until the capture. Serve the directory on a free port instead. 🔴 **Pick the port
  with `ss -ltn` FIRST**: this document already records a handed-over fixed port being taken by
  another session, and 8103 was still occupied this session.
- 🔴 **A MEASUREMENT THAT SETTLES A LAYOUT ARGUMENT NEEDS THE SECOND POINT, AND THE SECOND POINT
  RETIRED THE REMEDY'S URGENCY RATHER THAN CONFIRMING IT.** `auto-fit` vs `auto-fill` was filed as a
  visible defect. At **2** cards `auto-fit` gives 1446px monsters and `auto-fill` gives 309px — the
  pathology is real. At **26** cards, which is what the real store has, the two are **identical** at
  307px because all 9 tracks are occupied and nothing collapses. **The fix is correct and latent; a
  one-point measurement would have shipped it as a fix for what is on screen.**
- 🔴 **A REMEDY WRITTEN FROM ONE PAGE'S SYMPTOM UNDER-SCOPES THE FIX.** The filed entry named the
  entry page's void. Looking at the surface showed `/scope` has the SAME defect and slightly worse —
  a 2908px card with content in the leftmost ~400px. **Both pages are the non-grid case; the entry
  page was simply the one somebody happened to open.**
- 🔴 **THE AMBIENT TOOLCHAIN IS NOT THE PINNED TOOLCHAIN, AND A REPO THAT SAYS "PINNED, NOT
  INHERITED" DOES NOT MAKE IT SO.** Bare `go` is **1.26.7** here; `nix develop -c go` is **1.25.14**.
  A subagent's honest report — naming which toolchain its result came from instead of saying "tests
  pass" — is the only reason this was noticed. **Ask which toolchain a green came from.** ⚠ And the
  standing "copy `.envrc` into the agent worktree" advice is inapplicable here: **this repo has no
  `.envrc` at all**, which a brief asserted and was wrong about.
- 🔴 **AGENTS SHARE ONE SCRATCHPAD DIRECTORY, AND GENERIC FILENAMES IN IT GET OVERWRITTEN MID-TASK.**
  Two agents running concurrently both wrote `msg.txt` and `pr.md` there; the second clobbered the
  first. No damage — both had already created their commits and PRs — but the hazard is now observed
  rather than predicted. **Name every scratch file per-agent.**
- ⚠ **A WAITER BUILT ON A TOOL'S EXIT CODE INVERTED ITS OWN VERDICT.** `until ! gh pr checks …`
  exits **8** while checks are pending, so the negation made the loop condition true immediately and
  it printed a confident "settled" beside a `pending` row. **Poll the output TEXT, not the status** —
  the same "read the content, not the exit code" rule this repo already carries, in a new shape.
- 🔴 **A LEAK-SCANNER CONTROL BUILT FROM A TEXTBOOK ADDRESS SCANS CLEAN.** An agent validating the
  scanner against its own changed file first planted a **tidy, low-numbered RFC1918 address of the
  kind documentation uses** and got **0** findings — that exact value sits in the scanner's own
  documentation allowlist. A realistic one produced **1**. The pair reported was `1 on the realistic
  control, 0 under test`. **A scanner allowlists its own canonical examples; build the negative
  control from realistic data.** ⚠ **This bullet is deliberately NOT instantiated** — this document
  already records the gate refusing a bullet that spelled an address in order to explain it, and the
  rule it broke is *describe the shape, never instantiate it*. The shape is the lesson.
- **Decision (operator, this session): the wide ultrawide layout is KEPT.** Looked at against the
  real store at 3004px — 26 scopes, 9 columns, 96.3%. Offered and declined implicitly by the choice:
  reverting the breakpoint, which would have undone the earlier operator decision while leaving the
  actual cause — the un-gridded pages — in place.
- **Decision (operator, this session): cap the CARD on `/scope` and `/entry`, leave `/` at 96%.**
  Offered and declined: a two-column layout inside a full-width card (uses the width, bigger diff),
  and capping only the inner content consistently (keeps the void).
- **Decision (operator, this session): ACCEPT the edge-injected script and correct the claim in
  code**, rather than disabling the CDN features or restoring a CSP that permits them. The visible
  consequence is accepted with it: the signed-in identity line renders as a literal placeholder to
  any reader without script.
- 🔴 **Decision (operator, this session), REVERSED WITHIN THE SESSION, AND BOTH HALVES ARE KEPT
  BECAUSE THE SECOND ONE IS THE ONE THAT BINDS.** First: *set the edge's Browser Cache TTL to
  "Respect Existing Headers" for this host*, rather than accepting the override or bypassing the
  cache. Then, before anything was done: **"skip the ttl changes"**. ❌ **The first decision was
  never acted on and must not be read as standing** — the edge is untouched and rank 22 is CLOSED BY
  DECISION. ⚠ **The generalisable bit is why this is written down at all:** the earlier line was
  already recorded in three places (a rank, a `Defects` entry and this bullet) by the time it was
  reversed, and a reversal that corrects only the most visible copy leaves the other two asserting a
  plan nobody intends. **When a decision flips, grep for every site that recorded it** — here the
  count was three, and the tell was that one of them was phrased as a closing condition, which is
  what a future session would have picked up and worked.
- ⚠ **VERIFYING A LAYOUT DID NOT REQUIRE THE OPERATOR'S SCREEN, AND DID NOT TAKE IT.** The whole
  pass — three pages, geometry probes, a two-point grid control and three screenshots — ran in
  BACKGROUND tabs with CDP captures. **Zero workspace switches and zero focus changes**, which is the
  stronger of the two claims that rule distinguishes.

- 🔴 **A `-run` FILTER THAT MATCHES NO TEST REPORTS `ok`, AND I READ THAT AS AN ANSWER FOR ONE
  STEP.** Checking whether the AST raw-node ban also killed my escaping mutant, I ran
  `go test -run 'Raw.*Ban|Ban.*Raw'`. No test in `internal/ui` matches that pattern — they are
  `TestNoRawNodeConstructorAppearsInTheUIPackage` and `TestTheRawNodeBanCanGoRED` — and the
  package answered a clean **`ok`**, which is indistinguishable from "the ban ran and passed".
  **The control is one command and it is now the habit: count `=== RUN` lines before believing
  any `-run`-scoped verdict.** Re-run with a validated filter (1 `=== RUN`), the ban catches it.
  ⚠ Same family as this repo's `-skip`-by-name and `ok`-floor lessons, but in the SELECTION rather
  than the reporting direction — and hit while deliberately validating an instrument.
- 🔴 **A MUTANT KILLED BY A DIFFERENT GUARD PROVES NOTHING ABOUT YOURS, AND THE RAW VIEW IS THE
  CASE WHERE BOTH FIRE.** `g.Raw(e.Raw)` in `rawBlock` is caught by the new behavioural escaping
  guard AND by `rawban_test.go`'s AST ban. Read together, the kill is unattributable. So the
  behavioural verdict was taken with `-run` scoped to that guard alone, and the ban's kill
  confirmed separately. ⚠ **AND IT REFUTED A CLAIM ALREADY IN THE TREE:**
  `internal/ui/README.md`'s Phase-D tail says *"there is no way to write the non-escaping
  mutant"* — true of the inline-code split, which builds only `g.Text` and an attribute-free
  `h.Code`; false of a view that emits ONE node. **A "cannot be mutated" claim is scoped to the
  code it was written about, and expires the moment a new call site exists.**
- 🔴 **TWO GUARDS I WROTE PASSED ON PRE-CHANGE CODE, AND SAYING SO IS THE POINT.** The raw view's
  narrowing and escaping tests both drive `?view=raw` — which pre-change is ignored, so both were
  measuring the RENDERED view. They are **invariant guards**, and the honest handling is to label
  them and mutation-test them on the new path, not to quietly count five green tests as five
  guards. ⚠ The general shape for a NEW feature: a test for a code path that does not exist yet
  either fails to COMPILE (not a meaningful red) or passes VACUOUSLY. Writing the test against
  the WIRE spelling — literals, not the constants the change introduces — is what makes the first
  three compile at base and fail for the right reason.
- 🔴 **A GENERATED FILE MERGED AS TEXT IS THE SEMANTIC-CONFLICT SHAPE, AND "IT CAME OUT IDENTICAL"
  IS A FACT ABOUT THE PAIR RATHER THAN ABOUT THE MECHANISM.** #133 and #134 both add rules to
  `tailwind.css` and both regenerate `app.css`. The textual merge of `app.css` was byte-compared
  against a real regeneration from the merged source: identical, because the additions land in
  disjoint blocks and Tailwind emits in source order. **Do not generalise it.** The rule that
  survives: regenerate build output on the merged tree, then compare — never merge it and move on.
- 🔴 **AND THE MERGED-TREE TEST EXPIRES WHEN THE BASE MOVES, WHICH IS EXACTLY WHAT MERGING EITHER
  PR DOES.** #133 × #134 were tested merged at `38bea8b`. The moment one lands, that measurement
  is about a tree that no longer exists. The rule already says it; it is written here because the
  situation — two open PRs on one package, each green on its own branch — makes it feel unnecessary.
- 🔴 **I READ A PIPELINE'S EXIT CODE AND GOT THE GREP'S.** `nix build` piped into a `grep` for
  `FAIL|error`, with `rc=$?` read afterwards, reports whether GREP matched — not whether the build
  succeeded — and with a reassuring pattern it prints `rc=0` for a failed build. Caught by
  re-running with nix's status captured directly, no pipe. **Read the content, and when you need
  the status, do not put a pipe between you and it.**
- 🔴 **A SCRIPTED SECTION-REPLACE MATCHED A HEADING NAME QUOTED INSIDE PROSE AND ATE FIFTEEN RANKED
  ITEMS.** Editing this very handoff delta, a `text.index("## How to verify")` hit the backticked
  occurrence of that heading name inside rank 9's BODY, so the replacement spliced a whole section
  into the middle of an item and destroyed ranks 10–24. The write gate caught it —
  `status=unforced`, one item with no `forcing:` field — the only reason it was not shipped. **Anchor a heading match to line start, and when a document's own prose quotes its
  structure, prefer rewriting the file whole over a scripted splice.** ⚠ The gate refused on a
  SYMPTOM two items away from the damage; a gate that only counted sections would have passed it.
- 🔴 **A UI CHANGE WAS LOOKED AT IN A REAL BROWSER WITH ZERO SCREEN RAISES, AND THE RECIPE IS
  REUSABLE.** Dump the handler's own bytes from a throwaway test, copy the generated stylesheet to
  the content-hashed path the HTML links, serve the directory on a **dynamically chosen free port**
  (`8103` is taken on this host — the doc's old fixed-port recipe collides), `browser open` a
  BACKGROUND tab, `screenshot --fullpage`, then `close` the tab and kill the server **by resolved
  PID after confirming `/proc/<pid>/cmdline`**. The throwaway test is deleted before the commit.
  ⚠ This is what turned the raw view's discriminating tokens from asserted into seen.
- 🔴 **A SWEEP RUN AT THE START OF A SESSION IS NOT THE SWEEP THAT MATTERS.** `gh pr list --state
  open` answered `[]` at orientation; twenty minutes later, before branching, it answered **#133** —
  a concurrent session's PR touching the same three files. The claim lock could not have shown it
  (the peer claimed a DIFFERENT slug, `cairn-ui-share-affordance`, for the same package). **Both
  moments are load-bearing, and the second one is where the sunk cost is highest.**
- 🔴 **CARRIED FORWARD FROM A `State now` THAT WAS ABOUT TO DROP THEM A SECOND TIME — TWO
  RETRACTED MECHANISMS ABOUT THE CARD CAP, BOTH STILL BINDING.** (a) ❌ *"`.page-main > .card`
  reaches exactly the two broken pages"* — it reaches **four**: `/scope`, `/entry`, `NavigatePage`
  and `/?q=`, whose `searchResults` renders `.card.results` as a direct child of `<main>`. Three
  route states were rendered and the claim generalised to the ROUTE SET. **For a claim about a SET,
  enumerate the render sites from SOURCE.** (b) ❌ *"`.scope-grid .card`
  overrides for cards inside the grid"* — it declares only `margin-block: 0` and has EQUAL
  specificity, so it could never override a `max-width`. The grid is safe because `>` is a CHILD
  COMBINATOR and grid cards are GRANDCHILDREN, which never match — so **anyone who later flattens
  the `.scope-grid` wrapper silently caps the whole grid**, which is why #131's guard pins the
  NESTING and not only the declaration. ⚠ Both sat under `State now`, which REPLACES; they survive
  here and in the `cairn/ui` index entry, and nowhere else in this doc.
- 🔴 **A GUARD THAT WALKS FROM THE REPO ROOT CANNOT TELL THIS REPO FROM A CHECKOUT OF THIS REPO
  INSIDE IT** — and agent worktrees now live under `.claude/worktrees/`, so this is the default
  situation rather than an exotic one. The tell is a FAIL naming paths that contain the repo's own
  name twice. ⚠ **The control that settles it is a fresh worktree of `origin/main`**, which has the
  same tracked content and none of the nesting; if that is green, the finding is about the clone
  and not about the tree. Same family as the leakscan `result`-symlink lesson, one level out.

- 🔴 **"THE FEATURE IS MISSING" AND "IT HAS NO ENTRY POINT" ARE THE SAME REPORT AND DIFFERENT DEFECTS,
  AND ONLY THE SECOND IS INVISIBLE TO EVERY GATE THIS REPO OWNS.** The share flow was deployed,
  authorised, route-registered and test-covered — and unusable, because nothing linked to it. Every
  guard asked *"does this page render correctly?"*; none asked *"can a reader GET here?"* Route ledger,
  class-rule guard, Chromium `uiaudit` and a 24-item browser-validation pass were green throughout, and
  that validation could not have caught it: it reached pages by **typing URLs**, the one access path the
  defect leaves working. **Reachability is not rendering.** Four method lessons from the same round,
  each measured: a `Href(X)` count of **0** is evidence only beside a non-zero sibling (`Href(RootPath)`
  = 3 was the control); **consolidating a duplicate is what found the second half** (`SharePage`'s own
  header copy was already wrong — plain-text wordmark, no way back — visible only once the copies sat
  side by side); a class added with no stylesheet rule ships **unstyled** with `app.css` byte-unchanged
  and the currency check green (`TestEveryRenderedClassHasARuleInTheStylesheet` caught it, and
  `checks.ui-stylesheet-is-current` was validated with a stale-`app.css` → rc 1 negative control before
  its green was believed, because a `nix build` printing nothing is the CACHED case); and **deployed ≠
  verified** — a content-hashed stylesheet URL plus `cmp` against `main`'s `app.css` is what proved the
  live bytes from outside the cluster, which the pod's own status cannot do. ⚠ Two of my probes were
  wrong and are kept rather than tidied: a `MATCH: NO` from comparing against the source's build-time
  PLACEHOLDER (`app.000000000000.css`), and an empty regex on a rule that IS present — **a mismatch
  from a pattern you wrote is a fact about the pattern first**. Full record: `cairn recall --ref ui`.
- 🔴 **`internal/ui` HAS FOUR PAGE FRAMES, TWO ARE NOW CONSOLIDATED, AND THE FOURTH MUST STAY SEPARATE.**
  `shell` frames the browse and (as of #133) the share pages; `SignInPage` builds its own BY DESIGN —
  the one PUBLIC page, no viewer/session/CSRF. Because arguing for consolidation is what makes routing
  it through `shell` the next plausible mistake, that boundary is pinned by
  `TestTheSignInPageOffersNoAuthenticatedNavigation` and **mutation-tested** (mutant confirmed to
  COMPILE, so it reached the guard rather than dying at the build). **Do not "finish the job".**
- ⚠ **A DEPLOY REPO'S UNTRACKED FILES BELONG TO OTHER SESSIONS.** That repo's `trunk` carried five;
  the bump staged **one explicit path**. `git add -A` there commits a sibling's WIP into a repo where
  commit means deploy.
- **Decision (operator, this session): merge #133 and deploy it**, without the offered audit ladder.
  Recorded because it touches shipped rendering, so the prose-PR round cap did not apply — skipped on
  instruction, not by rule.

## How to verify

```bash
# the share flow's ENTRY POINT — from outside the cluster. 🔴 NEVER write the deployed host into a
# repo file: `leakscan` gates hosts under the deployment's registrable domain, UNBOUNDED.
U=https://<the deployed UI host>
curl -s -o /dev/null -w '%{http_code}\n' "$U/share"      # 401 — registered and auth-gated, NOT 404
SERVED=$(curl -s "$U/sign-in" | grep -oE 'app\.[0-9a-f]+\.css' | head -1)
curl -s "$U/static/$SERVED" | grep -c 'nav-share'         # 1 — the rule is live
curl -s "$U/static/$SERVED" > /tmp/live.css
git show origin/main:internal/ui/app.css > /tmp/main.css
cmp /tmp/live.css /tmp/main.css && echo "the served bytes ARE main's"
curl -s "$U/sign-in" | grep -c 'href="/share"'            # 0 — the PUBLIC page stays clean
```
🔴 **THE AUTHENTICATED HEADER LINK IS NOT VERIFIED BY ANY OF THAT** — it all stops at the auth
boundary. Sign in and look at the header; that is rank 9 and it is the operator's.

🔴 **READ THE RUNNING IMAGE, NOT THE MANIFEST** — a bump is a claim about git:
```bash
export KUBECONFIG=<the deployment-manifest checkout>/<cluster>-kubeconfig
kubectl -n subsystem-store get pods -l app=cairn-ui \
  -o custom-columns='NAME:.metadata.name,READY:.status.containerStatuses[0].ready,IMAGE:.spec.containers[0].image'
kubectl -n subsystem-store logs <pod> --tail=40 | grep -E 'serving|sharing|sign-in'
```
🔴 **VERIFY A PUBLISHED IMAGE WITH A NEGATIVE CONTROL, each stream to its own file** — nix writes
cache warnings to stderr that a merged capture feeds to your parser:
```bash
nix-shell -p skopeo --run "skopeo inspect --no-creds docker://ghcr.io/zacxdev/cairn-ui:sha-<40hex>" \
  > /tmp/ok.json 2> /tmp/ok.err; echo "rc=$?"
nix-shell -p skopeo --run "skopeo inspect --no-creds docker://ghcr.io/zacxdev/cairn-ui:sha-$(printf '0%.0s' {1..40})" \
  >/dev/null 2>&1; echo "absent tag rc=$?   # non-zero, or the zero above is meaningless"
```
⚠ **`go test ./...` FROM THE BASE CLONE IS RED FOR A REASON THAT IS NOT THE TREE** while any
`.claude/worktrees/agent-*` exists — see `State now` and rank 23. Run it in a fresh worktree of
`origin/main`, as `nix develop -c` (bare `go` is 1.26.7; the pin is 1.25.14).

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

### SUPERSEDES "FOUR of the five auth controls are still LOOPBACK-only readings" — three of the four are now measured OFF-MESH
- as-of: 2026-09-26
- **Symptom + exact repro:** not a defect — the same coverage gap, re-measured. The block above it is
  **retired**: its `Next probe` prescribed exactly these readings and they have now been taken.
- **Observed (with values):** all at the public edge, holding a real session minted from the deployed
  surface's own credential. **The CSRF pair:** a `POST /sign-out` carrying a valid session cookie, a
  correct `Origin` and a WRONG `csrf` value → **403**; immediately after, `GET /` with the same jar →
  **200**, which is the positive control proving the 403 was the GATE refusing and not a dead session.
  **The Origin pair:** the same POST with **no** `Origin` header → **403**, and with
  `Origin: https://evil.example` → **403**, each followed by a **200** on `GET /` with the same jar.
  **The `__Host-` attributes**, read off a real edge `Set-Cookie` rather than a jar file:
  `__Host-cairn-session=…; Path=/; Expires=…; HttpOnly; Secure; SameSite=Lax` — no `Domain`.
  `via: measurement`
- **Ruled out:** that the earlier loopback readings were the only evidence. They are now corroborated
  off-mesh for three of four controls. `via: measurement`
- **Leading hypothesis:** the fourth control holds too, on the same structural argument — every gate
  derives from the REQUEST and none branches on network position.
- **Next probe:** the five-failure lockout is the ONE control still unmeasured off-mesh, and it was
  deliberately NOT fired. 🔴 **DO NOT FIRE IT UNTIL THE TRUSTED-PROXY ALLOWLIST IS CONFIRMED TO
  RESOLVE YOUR CLIENT ADDRESS.** An allowlist that does not match the real proxy buckets every caller
  under the proxy's own address, so one abuser — you — locks out everybody. Defaults are 5 failures /
  900 s, i.e. a **15-minute sign-in outage on a public surface**, and waiting it out extends it. The
  variable being SET is not the check; the tell is in the pod's log, not in any probe.

### The edge rewrites page CONTENT, not only injecting script — and the previous measurement of this was of a refusal, not a page
- as-of: 2026-09-26
- **Symptom + exact repro:** authenticated `GET /` at the edge, then count `<script>` elements and
  grep for `__cf_email__`.
- **Observed (with values):** anonymous `GET /sign-in` → **1** script (an inline CDN bot-detection
  injection). **Authenticated `GET /` → 2 scripts**: that same inline one PLUS a `src=`-loaded
  email-decoding script. And the edge **rewrites rendered content**: `signed in as <address>` is
  replaced by an anchor with `class="__cf_email__"`, a `data-cfemail` hex payload and the literal
  visible text `[email protected]`, pointing at a CDN endpoint **the application never emitted and no
  route ledger declares**. With script disabled the surface therefore displays a FALSE identity
  string. `via: measurement`
- **Ruled out:** ❌ **RETRACTED — the previously recorded "`GET /` carries none, so it is not even
  uniform".** That reading was taken ANONYMOUSLY, so what it measured was a **12-byte `401` body** —
  a refusal, not the entries page. Re-measured authenticated, it carries two. **An anonymous probe of
  an authenticated route measures the refusal, not the page.** `via: measurement`
- **Leading hypothesis:** two separate CDN features (bot detection and email obfuscation), the second
  of which is a content rewriter rather than an injector — so a test asserting the renderer's output
  bytes says nothing about what a reader sees.
- **Next probe:** none needed for the decision, which is taken (see `Defects`). If the accept is ever
  revisited, the discriminating reading is whether disabling email obfuscation alone removes the
  `src=` script while leaving the inline bot-detection one.
