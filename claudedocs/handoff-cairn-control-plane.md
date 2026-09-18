# Handoff: cairn-control-plane — 2026-09-15

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
- Branch `main` @ **`229c142`**. ✅ **P4 (identity) MERGED** as `2665ebb` after a five-round
  audit ladder plus a design pass; the handoff for it merged as `229c142` (#37).
- 🔴 **RANK 2 (P5 slice 1) IS IN FLIGHT AS `ZacxDev/cairn#38` @ `96be4ec`, NOT MERGED.**
  Claim `cairn-control-plane-1` is **HELD** (rank 1 is now the cutover — see the ⚠ below).
- **What #38 ships:** `control.ProvisionUser` (one batch: `user-created`, `project-created`,
  `member-set`@`RoleOwner`, one `scope-created` per scope), `cairn-server -create-user` (an
  operator flag mode that exits — **no new HTTP route**), `$CAIRN_CONTROL_JOURNAL` plus a
  cache/refresh timer for the pod's read path, `identity.FromEnvironment(env, authority,
  sessions)` refusing in **both** directions, and `(provider, subject)` + folded scope-name
  uniqueness. It closes the 🔴 that both identity backends were inert.
- 🔴 **AUDIT LADDER ON #38: rounds 0, 1 and 2 are DONE; ROUND 3 IS OWED.** Round 0 →
  requirement questioned + 3 deletions taken. Round 1 (blind) → **1🔴** 3🟡 1🟢. Round 2 →
  4🟡. Every round found real defects, so the ladder continues. Heads in order:
  `e11c3a7` → `18df63d` → `8c06ea1` → `96be4ec`.
- ⚠ **OPERATOR DECISION this session: THE CUTOVER IS PROMOTED ABOVE THE REST OF P5.**
  Round 0 measured that **nothing in #38 runs**: the live pod is the Python image
  `subsystem-store-api:0.8.1` with four `SUBSYSTEM_STORE_*` env vars and **zero** `CAIRN_*`.
  Every hour of P5 buys nothing a user can touch until the cutover lands.
- 🔴 **GATES WERE MID-RUN AT HANDOFF — do not quote them as green.** Confirmed on the merged
  tree `d6aac74` (`origin/main` + `96be4ec`): `go vet` rc 0 · `go test` **15 ok / 0 FAIL** ·
  `go test -race` **15 ok / 0 DATA RACE**. `pytest`, `leakscan`, conformance and the battery
  had **not finished**. The fix agent's own run on `96be4ec` reported battery
  `110/108/2, misattributed=0, harness-errors=0, stale-extras=0`, conformance `99/415/0/4`,
  leakscan rc 0 / 298 files — **its numbers, not mine.** Re-run before believing them.
- **Deploy/verify status: STILL NOTHING DEPLOYED, but the BUILD half of rank 1 has landed on a
  branch.** `packages.default`/`apps.default` are now the **Go client**, and
  `packages.server-image-go` exists (there was no Go server image at all before). The Go server
  has still never run against the real pod, and `publish-image.yml` still publishes
  `packages.server-image`, the **Python** pod. See rank 1.

## Next steps (ranked)
1. **THE CUTOVER — its BUILD half is IN FLIGHT on `feat/go-server-image-and-default-cutover`;
   the DEPLOY half is untouched. Then P3(d).** What landed on that branch: `packages.default`
   and `apps.default` → the Go client, and a new `packages.server-image-go` wrapping
   `cmd/cairn-server` (**there was no Go server image before it**). What did NOT: nothing was
   deployed, nothing was pushed to a registry, and `publish-image.yml` still publishes the
   Python `packages.server-image`.
   ⚠ **THE APPLET COUNT IN THIS ITEM WAS WRONG AND IS CORRECTED — it said FOUR network
   servers.** Enumerated from `busybox --list` on the built image there are **six**: `httpd`,
   `telnetd`, `ftpd`, `tftpd`, **`dnsd`** and **`inetd`**, plus `ssl_client` and a set of
   network clients, in a pod that mounts a credential at
   `/run/secrets/subsystem-store/token`; the deployed image instead ships `bash`, `apt-get`
   and 8 setuid binaries. That is the fourth draft of this count and the third correction —
   the measured record now lives in `server/README.md`, and the standing instruction is to
   enumerate from the built image rather than from any paragraph. Neither image's tool surface
   is a subset of the other's and nothing about that trade is settled.
   🔴 **WHAT REMAINS AT RANK 1: the DEPLOY decision.** Diff the images with the threat model in
   front of you, decide whether the pod runs the Go server, and decide whether
   `publish-image.yml` publishes it. Moving `packages.default` already changed what
   `nix run github:…/cairn` executes for **every existing consumer**, and it WIDENED the CLI
   contract (`-verbs`/`-exit-codes` answer 0 where the oracle refused at 2) — declared, not a
   defect. Then **P3(d)** (immutable scope ids + client-side rename reconciliation), which is
   also the closing condition for two residuals #38 declared.
   forcing: user — operator promoted this above the rest of P5 this session, on the measurement
   that nothing in P5 is reachable until it lands.
2. **P5 slice 1 — 🔴 IN FLIGHT as `ZacxDev/cairn#38` @ `96be4ec`.** Owed: **round 3** as a
   delta re-audit of `8c06ea1..96be4ec`, a full merged-tree gate run, then merge. Files:
   `internal/control/{provision,cache,journal,model}.go`, `cmd/cairn-server/createuser.go`,
   `internal/identity/config.go`, `tests/control_mutants.py`.
   forcing: user — "identity via supabase (github and google)", plus a named second instance
   fronted by an oauth proxy; this slice is what makes either reachable.
3. **P5 remainder — the PWA** (Tailwind + gomponents + htmx). Sign-in, projects, members,
   scopes, entry view, search, **share dialog**, credentials, grant log, status. 🔴 The share
   dialog must state that unsharing cannot recall a replica — pin the whole normalised string,
   not keywords. ⚠ **The carve of P5 into "enabling work first" was the ORCHESTRATOR's, not
   the operator's** — round 0 measured 12 of 17 requirements unattributed, with the shaping
   ones tracing to a dispatch brief. The operator's ask on record is the full PWA.
   forcing: user — "a fully featured UI (PWA tailwind + gomponents + htmx webapp)".
4. **P7 — conditional snapshot sync.** `/api/v1/snapshot` ships a full tar with no ETag/304.
   🔴 With P3 landed this is correctness, not scale: with many principals a mis-keyed cache
   cross-serves another tenant's tar, so key it on **principal + epoch** —
   `control.Authorization` already carries `Epoch`.
   forcing: none
5. **P8 — retire the Python oracle.** Gated on 1 having held in real use.
   forcing: none

## Defects (batched)
Fix as one round; closing one buys room for one rank.
- 🔴 **THE INERT-BACKENDS DEFECT IS CLOSED *IN #38*, NOT ON `main`.** Until #38 merges,
  `main` still has no binary constructing a journal-backed `control.Store`, so both session
  backends still authenticate nobody. **Closing condition (unchanged, now satisfiable):** the
  user-creation path lands AND `internal/identity` authenticates a Supabase or trusted-header
  session end-to-end asserting a **NON-EMPTY `Authorization`**. #38 carries exactly that test.
- 🟡 **#38 declares three residuals, each with a closing condition in the code:** the
  store-root collision hazard (only its *signal* is closed — a warning, not a refusal, because
  a directory carries no owner); the `ProvisionUser` model read **outside the `flock`**, so two
  concurrent `-create-user` runs race; and `Cache.Staleness()` still rendered nowhere (six
  comments in four packages state as load-bearing that it has no non-test caller).
- 🟡 **`Model.ScopeByNameIn` compares scope names RAW while the reader folds.** Deliberate and
  documented in #38: folding it would make the model a second, silent name-resolution
  mechanism and would widen `EventScopeRenamed`/`EventScopeMoved`, which nothing emits and no
  test pins. Pinned by a *folded-duplicate-accepted* arm so it cannot drift silently.
  **Closing condition:** P3(d) (scope bytes addressed by id).
- 🟢 **Two prose defects from P4's round 5, still open on `main`:** five present-tense
  cross-references naming identifiers that do not exist (`arms`, `settingsProbed`,
  `refuseBlankSettings`, `anySet` — each occurs in exactly ONE file with ZERO declarations,
  against a positive control); and `internal/identity/README.md`'s "Nothing here restates a
  count either" three paragraphs below its own "seven measured instances over six settings".
  **Closing condition:** a PR making every identifier named in `internal/identity/` and
  `tests/control_mutants.py` prose resolve to a real declaration, proven by a sweep with a
  positive control.
- 🟢 **A degenerate spelling left OPEN and named:** a rune graphic by Unicode category that
  still renders blank (U+2800, U+3164, U+115F, U+FFA0, a lone combining mark). Measured on
  `main`: a proxy secret of 32× U+2800 builds as a live 96-byte shared secret. Closing it needs
  a rendered-width judgement the package has no source for — a **new limb**, not a wider one.
- **PR #15's six findings, still open on `main`**: `lib/cairn_doctor.py` claims "2,016 bytes"
  for a HOME-length-dependent value; `cairn:337` cites `server.py:422` for `sole_header`, which
  is at `server/server.py:1409`; and the ledger's vacuity-control prose names the wrong guard.
- **Go/oracle divergences deferred with closing conditions in code**: `NaN`/`Infinity`, the
  `text`-field surrogate message, the `actor`-key 400-vs-200 residual, `?page=<21 digits>`.
- **`server/seed.sh:110`**'s `( cd "$1" && … )` shape — a bare `cd <relative>` **prints** the
  directory when it resolves through `CDPATH`. The other scripts use `CDPATH= cd --`.
- **Three files are not `gofmt`-clean on `main`** (`internal/client/exit.go`,
  `internal/client/options.go`, `internal/doctor/doctor_test.go`) — alignment only, and
  **nothing in CI greps `gofmt`**.
- **`-race` is gated in ONE TIER ONLY.** The `go` CI job runs it; `flake.nix`'s two
  `checkPhase`s still run `go test ./...` plain. ⚠ **That gate has now EARNED itself twice** —
  it caught a genuine data race in #38 (`refreshInterval` vs a leaked refresh goroutine).
  **Closing condition:** `-race` in both flake check phases, or a written line saying the CI
  tier is the only one and why.
- **The guards added by P4 ladder rounds 1 and 3 are NOT in the persistent battery** — proven
  by hand-run isolated mutations only, so nothing re-proves them later.
- ⚠ **`AGENTS.md` byte budget: `MAX_BYTES` 32,500, merged headroom 1,501 B.** History:
  #32 closed the original defect exactly as written (server history relocated, ceiling
  **lowered** 37,700 → 31,850, 202 B margin sized to keep the same design margin against a
  smaller file) — and it **failed within hours**, because the margin was derived for ONE edit
  and two concurrent one-row additions (84 B + 166 B) consumed it. Hence 32,500 = 30,999
  measured merged + 900 `MIN_HEADROOM` + 601 usable. **Closing condition for the remainder:**
  the next contributor who needs bytes evicts `Installing and building with nix` (8,321 B) or
  the server section's remains (9,071 B) rather than moving the number a third time.

## Gotchas / decisions / dead-ends
- 🔴 **A green corpus is not a green port.** The conformance split was 94/22/0/4 *before*
  two criticals were fixed and *before* the token fail-open was found. Three defects were
  found outside it: a Go loader that accepted a non-UTF-8 token file as an unrestricted
  legacy credential and served the whole store via `/snapshot`; an installer that dropped
  every member's mtime; `--text -h` exiting 0 while writing nothing. The corpus replays
  declared cases against a **correctly configured** server — it cannot vary the token
  file's bytes.
- 🔴 **A differential gate can pass because BOTH sides are broken.** The parity harness
  reported **72 PASS / 0 FAIL while measuring nothing** — a trusted-proxies env var
  copied from the conformance runner made the pod refuse every request, and two clients
  failing identically compare equal. Hence `--break-pod` / `--break-both` (rc 2 = could
  not vouch), a pre-flight, a content floor and a self-test on both harnesses. **Run the
  control, not just the gate.**
- 🔴 **`AGENTS.md` claimed `tests/leakscan.py` enforced five bullets; it gates three.**
  That false sentence is why nobody looked, and a live leak sat behind it through four
  green gates, 1,860 tests and eleven merged PRs. Now: the claim says what each bullet is
  covered by, and the two ungated ones have a mechanical rule.
- 🔴 **A plaintext denylist in a public repo republishes what it removes.** The gate holds
  **SHA-256 digests**, matched over `-`/`_` prefixes. Nobody can audit the set from inside
  the repo — stated in the constant's header, because a set auditable from here is
  readable from here. The one remaining plaintext real-name site is the
  `reachable-hostname` regex, and it **must** stay plaintext: it matches an *unbounded*
  set (any host under those domains), and digesting would convert "any host in this
  domain" into "these exact hosts" and stop catching the new one.
- 🔴 **Scrubbing `HEAD` does not unpublish history.** The names remain in git history on a
  public remote. The operator considered and **declined** a history rewrite.
- **`server/verify-byte-identity.sh` is NOT prior art for `tests/dualrun/`.** It compares
  the pod against the local CLI — one reader over **two** stores; the dual-run is two
  servers over **one**. Most of its machinery exists *because the stores can differ*;
  borrowing it imports four licences to differ that the dual-run must not grant.
- **Byte-identity means the UNCOMPRESSED tar.** gzip identity is unattainable between Go's
  `compress/flate` and zlib (Go hardcodes the OS byte, no API).
- **`tests/parity/harness.py` and `tests/dualrun/harness.py` collide on the module name
  `harness`.** In the reverse import order the ledger measures the *wrong gate while
  passing*. Both are run as scripts (`python3`, not `bash`).
- **Never run two `uv run --with pytest` invocations concurrently** — one disposes of the
  other's ephemeral venv and produces a bogus `FileNotFoundError` red.
- **zsh ate `$var:` twice this session** (`$T:tests/…` → `:t`, `$s:AGENTS.md` → `:A`),
  each time returning a confident *wrong* value — a false "0 references", an empty file
  that "parsed OK". **Brace it: `${var}:path`.**
- **The flake's source filter is git-based**: an untracked new `.go` file compiles under
  `go build` and fails `nix build`. Stage new files before reading either tier as green.
- **`AGENTS.md` + `CLAUDE.md` are gated** to 37,700 B with a 900 B minimum headroom
  (`tests/test_agent_instructions_weight.py`); currently 36,354 B. Put narrative in a
  harness README behind a pointer — that is the pattern the gate's own playbook prints.
- **Decision (operator, this session):** an unencodable seed stamp resolves to
  `seeded=UNREADABLE` on **both** sides — an authorised exception to P1's "do not change
  the oracle", because a contract cannot include "sometimes truncate the response
  mid-stream". Regenerating all 98 goldens after it moved **none**.
- **Dead end:** the narrow "audit/process arms only" shape for the dual-run was costed and
  rejected — it saves ~23 of 1,799 lines and forces out the tar arm, the one the corpus's
  own README flags 🔴 with four measured divergences.

- 🔴 **THE `dated-incident` RULE FIRES ON VERIFICATION STAMPS, NOT ONLY ON INCIDENT
  NARRATION — and it caught the first draft of THIS doc.** `leakscan` refused
  `verified <a real date>:` twice, on the PR immediately after the rule shipped. The fix
  is not an exemption: **pin the claim to a COMMIT SHA instead of a day.** A date says
  when somebody looked; a sha says which tree they looked at, which is the thing a later
  reader can actually check out and re-measure. Prefer `verified at <sha>` everywhere in
  this repo's prose — it satisfies the gate and is strictly more useful.
  🔴 **Then it fired a SECOND time — on this very bullet, for quoting the date it was
  warning about.** Document the shape with the placeholder `<a real date>`, the way
  `AGENTS.md` does; an example that instantiates the thing it forbids is the thing it
  forbids. ⚠ And note what this exposed: `Gotchas` is an APPEND section, so
  `handoff_doc.py` — which owns every write to this file — **structurally cannot correct
  a line inside it**; appending leaves the bad line in place and the gate keeps refusing.
  That correction had to be a direct edit, and that is the one case where it is right.

- 🔴 **TWO PRs THAT MERGE WITH ZERO TEXTUAL CONFLICT, BOTH INDIVIDUALLY GREEN, PRODUCED A
  RED MERGED TREE — AND NEITHER PR'S CI COULD SEE IT.** This branch and #24 both add lines
  to `AGENTS.md`. `git merge` was clean; `mergeable=MERGEABLE`; both branches green. The
  merged tree failed `test_the_session_instructions_keep_WORKING_HEADROOM` with **716 B of
  headroom against a required 900**. It was found only by BUILDING the merged tree in a
  throwaway worktree and running the gate there. Fixed on this side at `5c02f2b` (the
  pointer paragraph folded into the layout row) so merge ORDER does not decide whether
  `main` goes red; merged headroom is now 1,031 B. **The general rule this instantiates:
  a byte-budget gate over a shared file makes every concurrent PR a semantic conflict, and
  the conflict is invisible to both.** Build the merged tree.
- 🔴 **P3a's STORAGE ANSWER COLLIDES WITH THE `STDLIB ONLY` RULE, AND P4 OWNS THE
  DECISION.** `go.mod` has no `require` block and `flake.nix` passes `vendorHash = null`,
  which together make a dependency in the serving path a BUILD FAILURE rather than a
  silent addition. Every embedded SQL engine for Go is a third-party dependency, and **so
  is every Postgres driver**. So the file-backed journal is not a placeholder chosen for
  speed — it is the only durable authority the stated constraint permits today, and the
  plan's decision 1 ("authz state in Postgres/Supabase") **cannot be implemented without
  first deciding what happens to stdlib-only.** Recorded in `internal/control/README.md`;
  do not discover it halfway through P4.
- 🔴 **A `Model` STRUCT COPY IS NOT A COPY, AND THAT DEFECT SHIPPED IN P3a's FIRST DRAFT.**
  `control.Model` is six maps behind a struct header. `FileStore.Append` validated a batch
  by applying it to `current` — which shares every bucket with the live cache — so a batch
  rejected at its third event left the first two permanently applied to the served
  authority with **nothing in the journal recording them**: a model WIDER than the file it
  claims to project, and a restart silently "losing" grants that were never written.
  `Model.clone` is the fix, `TestARejectedBatchLeavesNeitherBytesNorState` is the guard
  (red at baseline by a one-word change), and the battery carries the same defect one
  nesting level down because `maps.Clone` is ONE LEVEL DEEP and the two membership indexes
  are maps of maps.
- 🔴 **A MUTATION BATTERY'S OWN ROWS ARE AS WRONG-ABLE AS THE CODE, AND THREE OF THIS
  ONE'S WERE.** `tests/control_mutants.py` found: a row naming a killer that **never
  runs** (the narrowing test calls `Narrow` directly and never reaches `copyIDs`, so the
  row read as coverage while providing none — inside the control built to refuse exactly
  that); and two mutants that **failed to apply at all** — one drifted pattern, one that
  left an import unused so the tree did not build. **A mutant that does not COMPILE dies
  at the build rather than at a guard, which is the one outcome that proves nothing.**
  Hence: attribution by WHICH TEST failed (a kill by another test is reported
  MISATTRIBUTED, not as a pass), an occurrence-COUNT assertion on every pattern, a
  "DID NOT BUILD" outcome distinct from a kill, and a positive control on an unedited copy.
- **The authz matrix carries a positive control because a uniformly-refusing matrix is
  self-consistent and measures nothing** — the same shape `tests/parity/README.md` records
  (72 PASS / 0 FAIL while the pod refused every request). The allow count is pinned from
  BOTH sides: **32 of 60 cells**, not 0 and not 60.
- **Decision (operator, this session): a project principal has NO authority over its own
  project's scopes.** A project is not a member of itself, so a service account for a
  project reaches that project's scopes only if somebody granted it. The wide alternative
  — "a project credential implicitly holds what the project owns" — is a rule that never
  appears in the grant log, and "who could see this, and when" is the whole reason the log
  exists. The same behaviour is available VISIBLY, via an explicit self-grant at project
  creation; that is a policy decision for whoever wires this up, recorded in
  `internal/control/README.md` because the default will otherwise read as an oversight to
  the first person who creates a CI credential and finds it cannot write.
- **`go test`'s `ok` floor is a LEDGER, not a smoke test** — it moved 10 → 11 with this
  change because twelve packages now carry tests. A floor left one below an OLD count does
  not fail late; it stops failing for the FIRST deletion, which is the only one anybody
  would notice.

- 🔴 **A STACKED PR PLUS A SQUASH MERGE PRODUCES A CONFLICT THAT LOOKS LIKE A REBASE
  PROBLEM AND IS NOT.** #30 was stacked on #29. Squash-merging #29 created a NEW commit, so
  #30's branch still carried #29's originals as non-ancestors of `main`; retargeting #30 to
  `main` turned it **CONFLICTING**, 19 files / 5,826 additions, re-proposing all of P3a onto
  a `main` that already had it. The fix is `git rebase --onto main <parent-tip>`, which
  drops exactly the squashed commits. 🔴 **And the base MOVED, so the whole gate set has to
  be re-run before force-pushing** — a pre-rebase green is a measurement of a different
  tree.
- 🔴 **`isolation: "worktree"` BRANCHES FROM THE DEFAULT BRANCH, NOT YOUR CHECKED-OUT ONE.**
  Measured twice: an agent dispatched to build on an unmerged branch got a worktree of
  `main` and correctly refused rather than recreating the missing package. **Always give a
  dispatched agent a base check it can fail** ("`ls <path>`; if missing, STOP"), and the
  recovery command.
- 🔴 **AN `until`-LOOP CI WATCHER THAT COUNTS *INCOMPLETE* CHECKS REPORTS GREEN ON AN EMPTY
  ROLLUP.** Zero incomplete checks is also what "no checks exist yet" looks like — the state
  right after a push, while GitHub clears the rollup. It returned `checks: []`,
  `mergeable: UNKNOWN`, exit 0. **Require a minimum check COUNT as well as completion, and
  print the count**, so the reading carries its own proof it measured something.
- 🔴 **`pathlib.read_text()` COUNTS CHARACTERS; THE BYTE BUDGET IS BYTES.** `AGENTS.md`
  carries 153 non-ASCII characters (🔴 ⚠ —) costing 343 bytes, so a section table built with
  `read_text()` under-reports by ~1%. An audit diagnosed the resulting mismatch as a STALE
  MEASUREMENT (two different commits) when it was one read in the wrong unit — a wrong
  diagnosis outlives a wrong number. Use `read_bytes()` or `wc -c`.
- 🔴 **THE MUTATION BATTERY'S OWN ROWS ARE AS WRONG-ABLE AS THE CODE, AND FOUR WERE.** Across
  three rounds: a row naming a killer that NEVER RUNS (the named test reached the function by
  a different path); two patterns that failed to apply so the mutant died at the BUILD rather
  than at a guard; and an `EQUIVALENT` label whose stated reason — "a window between two
  adjacent statements no gate can open from outside" — was FALSE, because a caller-injected
  `clock()` sat between them. That last one is the worst shape: a label that reads as
  coverage while providing none, forecloses the test that would close the gap, and sits
  inside the battery built to refuse exactly that.
- 🔴 **A NUMBER QUOTED IN PROSE AND DERIVABLE FROM CODE HAS NO GATE, AND PROSE REMEDIES DO
  NOT WORK.** The battery's headline went stale TWICE while the file carried a
  `⚠ RE-DERIVE THESE` instruction. `tests/test_control_mutant_count_is_pinned.py` now pins
  it at three README anchors AND the CI step **name** (the string a reader sees in the
  Actions UI, which carried a stale number for a whole round after the comment above it was
  fixed). ⚠ It pins the COUNT only — the kill/survivor split is the outcome of a RUN.
- 🔴 **A PROSE GUARD MUST NOT RED ON CORRECT PROSE, AND TWO DRAFTS OF THAT PIN DID.** Draft 1
  matched every `N mutants` including the README's legitimate HISTORICAL timing comparison;
  draft 2 exempted `at N mutants` with a literal space, and the README wraps that exact
  phrase across a line break. A guard that reds on a sentence nobody should change trains
  its reader to edit the sentence. Both drafts are recorded in the file.
- 🔴 **THE ORCHESTRATOR'S OWN BRIEF IS AN UNATTRIBUTED REQUIREMENT, AND ROUND 0 IS WHERE
  THAT SURFACES.** Measured on #31: **7 of 12 requirements unattributed**, and the adapter
  design — which shaped the whole PR — traced to a dispatch brief, not to the operator, whose
  actual ask names "(b) wiring" and "(c) the migration" as two SEPARATE pieces. The verdict
  was still keep the adapter, but for three different reasons than the one recorded, and the
  recorded one was measurably FALSE (the conformance corpus cannot gate unrestricted-ness:
  its world has four scopes and `wide-reader`'s allowlist names all four).
- 🔴 **A DECLARED DIVERGENCE'S MITIGATION IS A CLAIM TOO.** #31 declared an out-of-band-scope
  window as "bounded and REPORTED" — and `Cache.Staleness()` had **no non-test caller
  anywhere in the tree**. Bounded: true. Reported: false, at nine sites including the README,
  which contradicted itself within one file.
- **A fix round's likeliest next finding is a sentence it wrote to explain itself.** Round 1's
  fixes created three of round 2's seven findings; round 2's created two of round 3's five.
  Every one was a number or a claim written while fixing something else.
- 🔴 **`grep -c` EXITS 1 WHEN THE COUNT IS ZERO**, so a verification that ends on it inverts
  its own status — a clean result reads as failure and a dirty one as success. Put the
  count-to-zero check anywhere but last, or branch on the number.
- **Own what you spawn, and the next round is who finds out.** This session leaked a
  `cairn-server.test` that held a loopback port for **13h47m** (found by a later audit, whose
  cwd filter correctly said "not mine") and **566 MB** of battery temp trees under `/tmp`.
  Both cleared. Resolve a PID and check `/proc/<pid>/cwd` before killing — and note the
  battery's `TemporaryDirectory` does NOT survive a crashed run.

- 🔴 **THE STDLIB-ONLY QUESTION IS NARROWER THAN THE HANDOFF PREVIOUSLY IMPLIED, AND THE
  DISTINCTION UNBLOCKED P4.** The previous update said "P4 inherits the stdlib-only
  decision", which reads as *P4 is blocked*. It is not. What is blocked is replacing
  `tokenfile.Source` with a **Postgres-backed `control.Store`** — the plan's decision 1.
  **Identity is a different seam**: verifying a Supabase JWT needs no driver, and every
  primitive it does need is in the standard library (checked before dispatching, not
  assumed). The general shape: *"X inherits decision D"* is worth splitting into which
  PART of X needs D, because the unsplit sentence stops work that could proceed.
- 🔴 **`handoff_doc.py` RUNS GIT FROM INSIDE PYTHON, SO NO PreToolUse HOOK SEES ITS
  COMMIT.** This repo forbids committing to `main`; the `bash-guard` hook that enforces
  that for a shell `git commit` is structurally blind here. **Check
  `git branch --show-current` yourself before `--confirm --push`** — the tool will
  cheerfully commit the handoff onto whatever branch the checkout is standing on, and on
  this repo that would be a rule violation with no warning.
- ⚠ **An arc's closing condition can be PARTLY satisfied, and saying which clause is what
  makes that useful.** Clause 1 (the authz matrix) is green on `main`; three clauses
  remain. Reporting "not addressed" alone would have hidden that a third of the condition
  is now permanently met, and reporting "addressed" would have been false.

- 🔴 **A BYTE-BUDGET GATE OVER A SHARED FILE MAKES EVERY CONCURRENT PR A SEMANTIC
  CONFLICT, AND THE MARGIN WAS SIZED IN THE WRONG UNIT.** Two branches each added exactly
  one layout row within hours — 84 B and 166 B — each green alone, **merge 49 B over**.
  The only reordering that fit deleted the word "BYPASS" from the one row describing an
  auth-bypass surface: the gate degrading the content it exists to protect. The 202 B
  margin was derived for ONE edit ("a bit over one large paragraph"); a repo with
  concurrent PRs does not present one edit. `MAX_BYTES` → **32,500** = 30,999 measured
  merged + 900 `MIN_HEADROOM` + **601 usable** (~four index rows at the observed ~150 B).
  🔴 **The raised gate was watched still firing** — +601 green, +602 headroom red, +1501
  headroom red, +1502 both red. ⚠ **If you land here again the answer is probably NOT a
  third number**: `Installing and building with nix` (8,321 B) and the remains of the
  server section (9,071 B) are still evictable, and this file's design is that history
  leaves rather than the ceiling rising.
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
- 🔴 **AN ABSENT OPERAND REPORTS SAME, NOT MISSING** — my own check hit this: a
  `grep -rn <pattern> <dir>` over a directory that did not exist returned nothing, and I
  nearly read that as "the symbols were cleanly deleted". Prove the operand exists first.
- 🔴 **I ASSERTED A SECURITY CLAIM THAT WAS FALSE, AND THE AGENT REFUTED IT BY
  MEASUREMENT.** I briefed that deleting HS256 support *and* its check would reintroduce
  algorithm-confusion. It probed the actual refusals under the mutant: both paths still
  fail closed (via `accepts` and `algKeyType`). Reproduced — with the named guard removed
  the forgery is still **refused**, just anonymously. My claim was true of a *careless*
  deletion leaving `AlgHS256` in the key-type table, not of this one. **The guard is kept
  for the reason that IS measurable: it NAMES the refusal**, so a future one-line
  re-addition of HS256 support cannot silently re-open the hole, and the test asserts the
  *sentinel* rather than "an error" — which is why removing it goes red on behaviour that
  is still safe.
- **Round 0 earns its place on a big PR.** On #35 it produced a real deletion (a whole
  attack class retired rather than audited), corrected my attribution (both new backends
  are the OPERATOR's ask, cited from this doc — not an orchestrator requirement), found
  three of its own deletion candidates load-bearing on inspection, and **declined to fetch
  the PR branch** because that writes refs into a git dir shared with the caller's
  checkout. Its ledger: `requirements: 25 (unattributed: 11) · deletion candidates: 3`.
- **Deleting a symbol can INVERT a ledger.** Dropping a name from `supabaseEnv` does not
  merely stop reading it: `anySet` counts only names in the ledger, so the variable becomes
  **silently discarded by a healthy pod** — the opposite of the partial-configuration
  rule's intent. A "retired" ledger that refuses is the fix.

- 🔴 **A RAISED CEILING MUST BE WATCHED FIRING AT THE TREE YOU WILL ACTUALLY MERGE, NOT THE
  BRANCH.** The `MAX_BYTES` 32,500 boundary set was originally measured on the branch. Re-run
  on the **merged** tree it reproduces exactly — `+601` green, `+602` headroom red, `+1501`
  headroom red, `+1502` both red — which is what makes the merged green *earned* rather than
  asserted. The general shape: a gate's negative control is a claim about the tree it ran on,
  and the tree that decides the merge is a different one.
- 🔴 **TWO TEST COUNTS THAT DISAGREE ARE USUALLY TWO TREES, NOT A DEFECT — AND THE CI FLOOR
  MOVED WITH THEM.** An auditor reported `1927 collected` at the PR head; the orchestrator
  measured `1938` on the merged tree. Neither is wrong: the PR adds **0** Python test
  functions and `main` added **11** since `83c6ba4`. ⚠ The `FLOOR` constant differs the same
  way — **1,873 at the PR head, 1,888 on `origin/main`** — and the PR does not touch that
  line, so the merged tree inherits `main`'s 1,888 and clears it at 1,938. **Name the tree
  beside any suite count in this repo**, or the next reader reads a discrepancy as a
  regression.
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
- ⚠ **THE AUDIT BRIEF AND THIS REPO'S OWN GOTCHA DISAGREE ABOUT `isolation: "worktree"`, AND
  THE REPO IS RIGHT.** `audit-dispatch.py`'s WHERE TO WORK section says to dispatch with that
  flag because the PR lives in the cwd's repo — true, but incomplete: the flag branches from
  the DEFAULT branch, so for an UNMERGED branch it hands back a tree of `main`. Both audit and
  fix agents were given a hand-built detached/branch worktree at the PR head plus **a base
  check they could fail** (`rev-parse HEAD` must equal the head sha; `ls` the package). Do
  this for every dispatch against an open PR here.
- 🔴 **THE REPO'S OWN NAMED FAILURE SHAPE TURNED UP INSIDE THE PACKAGE BUILT TO REFUSE IT.**
  `TestTheEnvironmentLedgersNameEveryVariableEachBackendReads` carries the comment *"Every
  `Env*` constant this package exports, discovered rather than restated. Restating them would
  be the second spelling this test exists to refuse"* — directly above a hand-written literal
  restating all fifteen — and a 🔴 docstring claiming it *"fails when the set GROWS"*. It
  cannot: a constant absent from **both** ledgers is in neither side of the comparison. This
  is "a guard's DESCRIPTION claims coverage while the body inspects one side", and the place
  it hid was the guard written to close exactly that class one level up. **Reading as coverage
  while providing none is worse than none — it stops anyone looking.**
- **A blind round 1 is worth its cost even on a PR whose CI is fully green.** All six checks,
  a green corpus over 415 assertions, a 96-mutant battery at 94 killed and a clean merged tree
  did not surface any of the five findings — three of which are claims a future reader would
  rely on and be wrong. Green gates and honest prose are independent properties.

- 🔴 **THE LADDER'S DOMINANT FAILURE MODE, MEASURED OVER FIVE ROUNDS: each round's findings
  were against the PREVIOUS round's FIX, and usually against its PROSE rather than its
  code.** Round 2's five findings were all against round 1's fix; round 3's F2/F3 against
  round 2's; round 4's against round 3's. **Budget for it, and read every sentence a fix
  round writes against what the code now does.** The corollary that kept paying: when a fix
  round re-read its own new comment, it found real defects — one such read discovered a
  FIFTH instance of the hazard that no audit had found.
- 🔴 **A GUARD CAN BE SPELLED RATHER THAN STRUCTURAL, AND THIS CLASS PRODUCED SEVEN
  INSTANCES IN ONE FILE.** The tell each time: the package refused ONE spelling of a hazard
  and accepted ANOTHER spelling of the identical hazard, while the refusal it *did* have
  named the hazard exactly. Measured, in order: `treu` refused / `"  "` accepted (mTLS
  silently off); `"  "` refused / `","` accepted (peer allowlist silently empty); `"  "`
  refused / 32× U+200B accepted (a live shared secret made of zero-width characters). **Ask
  of every guard: can it pass while the hazard exists in a DIFFERENT SPELLING?**
- 🔴 **FOUR ROUNDS OF INCREMENTS DID NOT CONVERGE; ONE DESIGN PASS DID — AND THE SIGNAL TO
  SWITCH WAS COUNTABLE.** *"is this setting set?"* had grown **six different answers** in one
  file, and which one a setting got was decided by which reader it happened to be plumbed
  through. The organising principle the comments claimed ("does this setting's zero turn a
  check off") **existed nowhere in the data**. The fix: each ledger entry DECLARES its blank
  policy, one predicate, one resolver, and a gate that makes an undeclared policy a RED test
  with an actionable message. ⚠ **What it closes and what it does not:** a setting can no
  longer get a permissive answer by OMISSION, by a new reader path, or by a new SPELLING for
  a list (those are derived from the declaration). It still depends on a human choosing
  `refuseBlank` vs `defaultsTo` correctly — measured to cost **six coordinated edits across
  two files**, each a sentence stating the claim. That residual is irreducible: nothing
  mechanical can decide whether a setting's unset value is permissive.
- 🔴 **A TEST CAN PIN THE HAZARD OPEN, AND A GREEN SUITE THEN CERTIFIES THE DEFECT.** Round
  2's guard asserted that blank `PEERS`, `REQUIRE_CLIENT_CERT` and `REQUIRE_ROLE` were
  ACCEPTED — all three permissive-zero. Fixing round 3's finding therefore required
  CHANGING those arms, which looks exactly like weakening a test. The only thing that
  separated the two readings was an independent probe of what the earlier round's substance
  actually was. **When a fix must change an existing assertion, prove the earlier round's
  real claim still holds rather than arguing from the diff.**
- 🔴 **A MUTANT THAT FAILS TO APPLY REPORTS A FALSE `SURVIVED`, AND IT HAPPENED TWICE HERE —
  ONCE ON THE MUTATION PROVING THE GATE.** Both were caught only by asserting the anchor's
  occurrence count BEFORE editing. **Assert the anchor count every time**; a "survived" you
  did not watch apply is not evidence.
- 🔴 **`$?` AFTER A PIPE COST A FALSE `vet rc=1` IN THIS SESSION** — the status was `grep`'s
  "no lines matched", i.e. vet printing nothing, which is the healthy case. Re-measured with
  the rc captured before any pipe: rc 0, and a negative control (a deliberate `Printf` verb
  mismatch) confirmed vet can still go red in that tree. **A reassuring number and a broken
  harness are indistinguishable until you validate the instrument.**
- 🔴 **GREP MATCHED `auth=fail` INSIDE THE SERVER'S OWN AUDIT LOG** when I searched
  conformance output for `PASS|FAIL` case-insensitively, returning a screenful of false
  matches and no summary. The real line was `SUMMARY requests=99 assertions=415 failures=0
  skipped=4`. **Parsing a tool's output makes its FORMAT a dependency you did not pin.**
- 🔴 **TWO PRs REPORTING `mergeable=MERGEABLE` CAN STILL CONFLICT WITH EACH OTHER.** #35 and
  #37 both edit `claudedocs/handoff-cairn-control-plane.md`; GitHub compared each against
  CURRENT `main`, where neither had landed, so both read CLEAN.
  `git merge-tree --write-tree` exited **1**. 🔴 Branch on the EXIT CODE — that command
  prints only a tree OID on success and emits NO conflict markers, so a marker grep finds
  nothing whether or not a conflict exists.
- 🔴 **I PUSHED A PRIVATE REPO NAME INTO A PUBLIC REPO AND THE LEAK GATE CAUGHT IT AFTER THE
  PUSH.** `handoff_doc.py` owns the commit AND the push as one step, so running `leakscan`
  "before pushing" is not possible in that flow — it fired on the commit that had already
  landed (`rc 1`, `denied-identifier`). **Scan the SCRATCH DELTA before handing it to the
  tool.** Fixed in a follow-up commit; the gate did its job, my ordering did not.
- **`isolation: "worktree"` was avoided for EVERY dispatch this session, deliberately.** On
  this repo that flag branches from the DEFAULT branch, so an agent sent to an unmerged
  branch gets a tree of `main` with the package under audit missing. Every audit and fix
  agent got a hand-built detached/branch worktree at the exact head **plus a base check it
  could fail** (`rev-parse HEAD` must equal the sha; `ls` the package). Zero mis-targeted
  agents across eight dispatches.
- **A blind round 1 earns its place even on a fully green PR.** Six CI checks, a green
  corpus over 415 assertions, a 96-mutant battery and a clean merged tree surfaced none of
  round 1's five findings — three of which were claims a future reader would rely on and be
  wrong. **Green gates and honest prose are independent properties.**
- **`pgrep -f` matched my own shell** while sweeping for leaked processes; the single "hit"
  was the sweeping command's own `zsh -c` line quoting the pattern. Read as a leak it sends
  you hunting a process that does not exist; read as a target for `kill` it kills the shell.
  **Resolve PIDs, skip `$$`, confirm via `/proc/<pid>/cmdline` and `/proc/<pid>/cwd`.**
- **Base-clone drift happens on FEATURE branches too.** The local `feat/identity-interface`
  was a stale leftover four commits behind `origin`, checked out in no worktree. A
  `worktree add` on it silently produces a tree missing those commits and every gate run
  there measures the wrong thing. `git merge --ff-only` is the safe sync precisely because
  it cannot conflict or autostash — it advances or REFUSES.

- 🔴 **THE MUTATION BATTERY'S STDOUT IS BLOCK-BUFFERED TO A FILE, SO ITS LOG LOOKS FROZEN FOR
  ~12 MINUTES — AND THAT IS WHAT TWO STALLED AGENTS WERE LOOKING AT.** Both sat "waiting on the
  battery"; in one case the process had already died and the orchestrator had to take over.
  **Run it as `python3 -u tests/control_mutants.py`**, or track progress by counting its temp
  work dirs. 🔴 **And never infer a backgrounded run is alive from a quiet log — poll for a
  TERMINAL line, or check the PID.**
- 🔴 **A SIBLING SESSION CHANGED THIS HOST'S `HOME` MID-SESSION, AND TWO CORRECT READINGS OF
  "THE SAME TREE" THEN DISAGREED.** My green and an auditor's red were both accurate when
  taken. The lesson is not that either was wrong: **a shared host's HOME state is a dimension
  the measurement did not name.** When two runs of one tree disagree, `stat` the state the test
  reads before re-diagnosing the code.
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
- 🔴 **`extra_killers` WAS DECLARED ON 23 ROWS / 30 ENTRIES AND READ BY NOTHING — inside the
  battery built to refuse exactly that.** The README offered it as the closure for one of the
  silently-emptied guards, so it read as coverage and provided none. Made real (a stale entry
  now exits 1, with a `stale-extras=N` field in the SUMMARY). It exposed **exactly one** false
  row, `hot-path-reads-the-authority`, whose listed killer `unplug()`s the source so every call
  the mutant inserts takes the error branch — **structurally incapable** of seeing it, and
  contradicted by the adjacent row's own `why`. The entry was removed **and the removal
  recorded in ten lines**, not tidied away.
- 🔴 **A GUARD'S PREDICATE MUST MATCH THE THING IT PROTECTS, AND "IS A DIRECTORY" HAS TWO
  ANSWERS.** `DirEntry.IsDir()` does not follow symlinks; `os.Stat().IsDir()` does. The reader
  used the latter, the new warning the former — measured `[tenant-a-notes tenant-c-notes]` vs
  `[tenant-c-notes]`, so a symlinked scope directory got no warning at all. Same family as the
  scope-name 🔴: `NormalizeRef("Quarry_Notes") == NormalizeRef("quarry-notes")`, so a raw `==`
  guard let two `RoleOwner` tenants share one directory for read AND write, append-only, no
  undo.
- 🔴 **VERIFY A SQUASH MERGE BY CONTENT, NEVER BY ANCESTRY — carried here from `State now`,
  which is a REPLACE section, so it would otherwise have been deleted by this very update.**
  `git merge-base --is-ancestor <branch-head> origin/main` returns **false** after every
  squash merge, forever, and reads as "not merged — redo the work". Measured on #35:
  ancestry false, while `git diff e708cba origin/main -- internal/identity/` was **empty** and
  `main` carried the new symbol. ⚠ **A WHOLE-TREE diff is NOT that check** — it showed 4 files
  / 930 insertions, which were another PR's files the branch never had. Diff the PAYLOAD
  PATHS, and separately confirm the merge commit exists.
- 🔴 **MY OWN PARSING OF TOOL OUTPUT WAS THE ERROR THREE TIMES THIS SESSION.** `grep -iE
  'PASS|FAIL'` over conformance output matched **`auth=fail`** in the server's audit log
  (screenful of false matches, no summary); `$?` after a pipe gave grep's status and produced a
  false `vet rc=1` when vet was clean; and counting battery verdicts with `[a-z0-9-]` missed
  seven mutant names containing UPPERCASE. **Cross-check with a second tool that fails
  differently** (`awk` on field 2 settled the last one against the SUMMARY line).
- **An agent that hits its session limit mid-round may have PUSHED — verify by git, then
  resume it for the REPORT.** Measured: the agent died saying only "Pushed"; `origin` did carry
  its commit and its worktree was clean. `SendMessage` to its id resumed it with context intact
  and it delivered the full report. **Verify state first and tell it what you verified**, so the
  restored budget is not spent re-deriving.
- ⚠ **`isolation: "worktree"` WAS AVOIDED FOR EVERY DISPATCH, DELIBERATELY.** On this repo that
  flag branches from the DEFAULT branch, so an agent sent to an unmerged branch gets a tree of
  `main` with the package under audit missing. Every agent got a hand-built detached/branch
  worktree at the exact head **plus a base check it could fail** (`rev-parse HEAD` must equal
  the sha; `ls` the package). Zero mis-targeted agents across eleven dispatches.
- **Round 0 earns its place, and its ordering finding was worth more than its deletions.**
  On #38 it measured that nothing in the PR runs — the live pod is Python `0.8.1` with zero
  `CAIRN_*` — and escalated the ordering question that moved the cutover to rank 1. It also
  found the requirement it was asked to satisfy guarded the WRONG DIRECTION: an armed session
  backend with **no** journal silently fell back to the token-file projection, which is the
  state the original defect measured. Both directions now refuse.

## How to verify
```bash
cd /home/zach/workspace/cairn
python3 tests/leakscan.py; echo "rc=$?"      # CAPTURE THE RC BEFORE ANY PIPE
python3 tests/leakscan.py --self-test; echo "rc=$?"
uv run --python 3.12 --with pytest -- pytest tests -q -p no:randomly
go vet ./... && go test ./...
python3 tests/control_mutants.py             # positive control GREEN, misattributed=0
python3 tests/conformance/suite.py run       # oracle: 99 requests / 433 assertions / 0 / 0
bash tests/conformance/run_go.sh             # Go: 116 PASS / 0 failures / 4 skips
python3 tests/parity/harness.py --break-pod  # MUST exit 2 — could not vouch
python3 tests/dualrun/harness.py --break-both # MUST exit 2 — could not vouch
```
🔴 `parity` and `dualrun` need a LIVE POD; CI runs both. Say so rather than implying you
ran them.
🔴 **`leakscan` exits 2 if an agent worktree exists under `.claude/worktrees/`** — remove
them first, or its clean run is unearned.
🔴 **Before merging alongside another open PR, BUILD THE MERGED TREE and run the gate
there.** Measured twice this session, both on `AGENTS.md`'s byte budget: a clean
`git merge` and a green `mergeable` are not evidence.
🔴 **Reading CI: require SIX checks present AND all COMPLETED** before believing a
verdict — an empty rollup satisfies "nothing incomplete".
## Open investigations — live diagnosis state

### PR #35's `tests` job was RED on the previous head; the fix is pushed but unconfirmed
- as-of: 2026-09-16
- **Symptom + exact repro:** `gh run view <run> --repo ZacxDev/cairn --log-failed` on the
  run for head `1dd2815` ends `assert 851 >= 900` → `1 failed, 1937 passed`. The failing
  test is `tests/test_agent_instructions_weight.py::test_the_session_instructions_keep_WORKING_HEADROOM`.
- **Observed (with values):** the branch alone measured `AGENTS.md`+`CLAUDE.md` = **30,915 B,
  headroom 935** — green. The **merged** tree measured **30,999 B, headroom 851** — red by
  49. `git diff 83c6ba4 origin/main -- AGENTS.md` shows `main` gained one 84-byte row
  (`| \`python3\` on \`PATH\` | ... |`, from #36); our branch gained one 166-byte layout row.
  Each green alone; the merge is 49 B over.
- **Ruled out:** a defect in either row — both are single, minimal, correct index rows.
  `via: measurement` (diffed each side's AGENTS.md against the common base `83c6ba4`).
- **Ruled out:** trimming our row to fit. Three candidates measured; the best that fits
  (108 B) **drops the word "BYPASS" from the one row describing an auth-bypass surface**
  and leaves 9 B of slack. `via: measurement`
- **Leading hypothesis:** resolved, not hypothetical — the 202 B margin was derived for
  ONE edit and the unit is CONCURRENT edits. `MAX_BYTES` raised to 32,500 in `7ac810e`.
- **Next probe:** `gh pr view 35 --repo ZacxDev/cairn --json statusCheckRollup` — require
  **six** checks present AND all COMPLETED before reading the verdict (see the watcher
  Gotcha below), then confirm `tests` is SUCCESS on `7ac810e`.

### ✅ RESOLVED — PR #35's `tests` job, and the merged-tree byte budget
- as-of: 2026-09-16
- **Resolution:** both halves are closed by measurement, and this supersedes the block above
  titled "PR #35's `tests` job was RED on the previous head; the fix is pushed but unconfirmed".
- **Observed (with values):** six checks present and all `COMPLETED`/`SUCCESS` on run
  `35163448166`; that run's `headSha` read back as `7ac810e8a49bcca6b9b4871cddc3a7abb7b1cb51`,
  so the rollup is about the tree we care about. On the locally-built merged tree:
  30,999 B, headroom 1,501, 1938 pytest passes, 15 Go `ok` lines, leakscan rc 0 over 293 files.
- **Ruled out:** that the raised ceiling is a gate that no longer fires. Re-measured the
  negative control **on the merged tree** rather than the branch: `+601` green · `+602`
  headroom RED · `+1501` headroom RED · `+1502` both RED. `via: measurement`
- **Ruled out:** that the auditor's `1927 collected` contradicted the orchestrator's `1938`.
  The PR adds **zero** Python test functions; `main` added **11** since the merge base
  `83c6ba4`; 1927 + 11 = 1938. Both readings are correct and describe different trees.
  `via: measurement`
- **Next probe:** none for this block — it is closed. The live question has moved to the fix
  round and round 2, below.

### The round-1 fix round, and the round-2 delta that must follow it
- as-of: 2026-09-16
- **Symptom + exact repro:** not a defect — an unfinished ladder. Round 1 returned five
  findings, so by the stop rule another round is owed; a ladder that stops on findings has
  shipped the finding.
- **Observed (with values):** round 1 ledger — five findings (3 🟡, 2 🟢), zero 🔴, verdict
  "safe to merge" **which is explicitly not the stop signal**. Each finding re-verified at
  source by the orchestrator: the `config_test.go` comment claiming "discovered rather than
  restated" sits directly above a hand-written 15-element literal; `anySet` is
  `strings.TrimSpace(env[name]) != ""`; `jwks.go:89` says "Zero disables the timer" while
  `:144` maps zero to `DefaultJWKSInterval`. Tree-wide enumeration for `-race`
  (`find … -print0 | xargs -0 grep`, **not** the `.gitignore`-blind `grep -r`) returned five
  hits, **none of them a runner**.
- **Ruled out:** that the fix agent could safely use `isolation: "worktree"`. On this repo
  that flag branches from the DEFAULT branch, so it would hand back a tree of `main` with no
  `internal/identity` at all. The worktree was built by hand at the PR head instead.
  `via: code` — the flag's documented behaviour, already measured twice on this repo.
- **Leading hypothesis:** round 2's likeliest finding is a **sentence the fix round wrote to
  explain itself**, not code — the shape this ladder has hit repeatedly. The named trap here
  is the `-race` sentence: `internal/control/cache_test.go:81` and `:460` already record that
  `-race` is structurally blind to, and green on, a real defect in this tree, so a fix-round
  sentence implying `-race` covers concurrency correctness would be a fresh false claim
  inside the section whose job is honest inventory.
- **Next probe:** once the fix round pushes, re-run the full gate set on a freshly-built
  MERGED tree, then dispatch round 2 with
  the `/audit-pr` skill's own brief assembler (`audit-dispatch.py 35 --round 2`, whose path
  this doc deliberately does not name) and **read its stderr**
  for the "newest claims block says round=N" line before dispatching.

### ✅ RESOLVED — the P4 audit ladder, five rounds plus a design pass
- as-of: 2026-09-17
- **Resolution:** closed. This supersedes the blocks above about PR #35's CI and the
  round-1 fix round. P4 merged as `2665ebb`.
- **Observed (with values):** round 1 → 3🟡 2🟢 · round 2 → 2🟡 3🟢, **every finding against
  round 1's own fix prose** · round 3 → 3🟡, including a **security-relevant regression
  caused by round 2's fix** · round 4 → 3🟡 2🟢 plus the verdict that the code wanted a
  design pass · round 5 → **3🟢, nothing requiring a code change**. Payload per round:
  81 → 99 → 173 → 1,089, cumulative 1,442, never zero, so the attribution gate never fired.
- **Ruled out:** that the ladder had left the PR and was auditing its own scaffolding. The
  payload count was non-zero every round, measured with
  `git log --numstat --format= --remerge-diff <audited>..HEAD --not origin/main`, rc 0,
  stderr silent, range non-empty each time. `via: measurement`
- **Ruled out:** that incremental fixes were converging. Four consecutive rounds each found
  one MORE instance of the same class, each in a place the previous round's prose said was
  covered — 7 instances over 6 settings. That is what bought the design pass. `via: measurement`
- **Next probe:** none — closed. The residuals are in Defects with closing conditions.

### PR #38 round 3 is owed, and the merged-tree gates were unfinished at handoff
- as-of: 2026-09-18
- **Symptom + exact repro:** not a defect — an unfinished ladder plus an unfinished
  measurement. Rounds 1 and 2 each found real defects, so by the stop rule round 3 is owed.
- **Observed (with values):** merged tree `d6aac74` (`origin/main` + `96be4ec`) confirmed
  `go vet` rc 0, `go test` 15 ok / 0 FAIL, `go test -race` 15 ok / 0 DATA RACE. `pytest`,
  `leakscan`, conformance and the battery did **not finish** before handoff. CI on `96be4ec`
  read `UNSTABLE` (still running), not failing.
- **Ruled out:** that the ladder had left the PR and was auditing its own scaffolding. Payload
  per round was non-zero every time — round 1 fixes **291 lines**, measured with
  `git log --numstat --format= --remerge-diff <audited>..HEAD --not origin/main`, rc 0, stderr
  silent, range non-empty. `via: measurement`
- **Next probe:** rebuild the merged tree and run the full gate set with **`python3 -u`** on
  the battery (see the Gotcha — its stdout is block-buffered), then
  the `/audit-pr` skill's own brief assembler (`audit-dispatch.py 38 --round 3`, whose path
  this doc deliberately does not name) and dispatch it against a detached worktree at the PR
  head, never `isolation: "worktree"`.

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
