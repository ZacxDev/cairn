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
- Branch `main` @ **`1a59e59`**, clean, **no open PRs**. Three PRs merged this session, each verified
  BY CONTENT: **#48 → `181053a`** (residual 8 closed), **#49 → `d8ce83b`** (the previous handoff),
  **#50 → `1a59e59`** (the flip).
- ✅ **THE CUTOVER ARC IS COMPLETE. `packages.default` AND `apps.default` ARE THE GO CLIENT.**
  Verified on `main` by **running it**, not by reading the diff: `nix run .# -- -verbs` → **rc 0**
  with a 134 B table; `nix run .#cairn -- -verbs` → **rc 2** with argparse's usage. That pair IS
  residual 7's contract widening, and it is now the shipped behaviour.
- ✅ **THE ANNOUNCED ESCAPE HATCH WORKS AND IS SPELLED FOR BOTH CONSUMPTION MODES.** `README.md`
  carries the CLI form (`#cairn`) **and** the flake-input form
  (`cairn.packages.${system}.cairn` / `cairn.apps.${system}.cairn`) — the second was missing and a
  round-0 audit caught it: `README.md` calls flake-input the PRIMARY mode, so the consumers most
  affected had been handed an opt-out they could not use. The announcement is anchored to **PR
  #50**, never to a date.
- ✅ **`checks.default-is-the-go-client` PINS THE WIRING AS A RELATIONSHIP**, and it is **named
  explicitly in `ci.yml`** — 🔴 because **CI never runs `nix flake check`**; the `nix` job builds
  each check by name, so a `checks.*` entry nobody names is a check nobody runs. It asserts
  `packages.default == packages.cairn-go`, `apps.default == getExe packages.default`,
  `apps.cairn == getExe packages.cairn`, `packages.cairn != packages.default`, and the resolved
  programs' base names. Build-free by design (0 `inputDrvs`), so a compile failure cannot redden it
  and read as "the default moved".
- 🔴 **THE ARC'S CLOSING CONDITION IS NOW 3 OF 4. ONLY THE PWA REMAINS.** Green: the authz matrix,
  identity through both backends, and `packages.default` the Go client. Unmet: **the PWA's share
  flow with its replica-honesty notice pinned by a test — nothing exists** (no web/pwa/ui directory
  in the tree).
- ✅ **THE PYTHON CLIENT IS UNCHANGED, STILL SHIPPED, AND STILL THE ORACLE.** `packages.cairn`,
  `apps.cairn` and `lib/` are untouched; `tests/parity/` is still the gate and byte-identity is
  still required both ways. **P8 retires Python; this was not P8.**
- **Claim `cairn-control-plane-1` is RELEASED.** The ranked list below is unclaimed.

## Next steps (ranked)
1. **P5 remainder — the PWA** (Tailwind + gomponents + htmx). Sign-in, projects, members, scopes,
   entry view, search, **share dialog**, credentials, grant log, status. 🔴 The share dialog must
   state that unsharing cannot recall a replica — **pin the whole normalised string**, because a
   guard on words is walkable by rewording. This is now the **only** unmet clause of the arc's
   closing condition and the largest thing left; it is entirely unstarted.
   forcing: user — "a fully featured UI (PWA tailwind + gomponents + htmx webapp)".
2. **The batched scaffolding defects.** ✅ **Issue #51 is DONE** — the reverted-draft flip history
   is evicted to `tests/parity/README.md` residual 7, which already carried it; `AGENTS.md` +
   `CLAUDE.md` are **31,082 B**, so the headroom before the warning band is **518 B**, not 147.
   The rest are test, harness or prose items; none changes what CI does.
   forcing: gate — filed BY attribution gates rather than fixed, so nothing else will surface them.
3. **P7 — conditional snapshot sync.** `/api/v1/snapshot` ships a full tar with no ETag/304. Key it
   on **principal + epoch** — `control.Authorization` already carries `Epoch`.
   forcing: none
4. **P8 — retire the Python oracle.** Now genuinely unblocked by the flip having landed, but gated
   on it **holding over real use** — which is a waiting period, not a task. The retirement ledger
   in `tests/parity/README.md` is a list of DECISIONS, not a delete script.
   forcing: none

## Defects (batched)
- 🔴 **THIS DOC IS OVER ITS 65,536 B GUIDELINE AND THREE CONSECUTIVE UPDATES HAVE EACH FLAGGED IT
  AND THEN GROWN IT.** No test reads the number, so nothing goes red; it is judgement about what the
  next session must read before it can act. The archive
  (`claudedocs/handoff-cairn-control-plane-archive.md`) is where answered material goes. 🔴 **Do NOT
  satisfy this by deleting a claim or narrowing a rule** — that is the failure the `AGENTS.md` budget
  already produced once, where the only reordering that fit deleted the word "BYPASS" from the row
  describing an auth-bypass surface. **Closing condition:** a prune PR that moves answered Gotchas
  blocks to the archive and brings this file under the guideline, or a written line from a named
  reader saying the ceiling is wrong.
- 🟡 **`apps.cairn` WAS UNPINNED UNTIL #50's FIX ROUND, AND THE NEAR-MISS IS THE RECORD WORTH
  KEEPING.** `README.md` promises "naming `#cairn` is the opt-out"; `nix run …#cairn` resolves
  `apps.cairn` first. Repointing it at the Go client left **all three** of the guard's original
  assertions green while the announced hatch silently became the Go client — the same half-flip class
  the guard exists for, one attribute over, defeating a promise the same PR created. Closed by a
  fourth assertion. **Kept as a defect entry because the lesson generalises: a guard written for
  attribute A does not cover sibling attribute B, and a promise made in prose creates a new thing to
  pin.**
- 🟡 **`checks.default-is-the-go-client` IS INSENSITIVE ON THE PYTHON SIDE.** `mkCairn`'s pname is
  already `cairn`, so a `meta.mainProgram` removed *there* leaves the base-name assertions green.
  Written into the flake comment rather than left to be discovered. **Closing condition:** a decision
  to close it or a written line saying why not.
- 🟡 **THREE FILED BY #48'S LADDER.** (a) `internal/client/readrouting_test.go:281` — the failure
  message says the predicate "must inspect EVERY state", but the assertion is `rows < 2 || nonOK == 0`
  and the fixture yields `PROBLEM=0`, so the `Problem` branch never executes. (b)
  `internal/doctor/render.go:99` — `Markers()` has one consumer and exports a rendering detail;
  separately, a state added to `markers` but not to `States` makes `doctorRow` silently skip those
  rows. (c) `lib/README.md` — the re-count recipe greps two literal phrases, so it is a SPELLED check
  that cannot see a reworded echo. **Closing condition:** one PR correcting all three.
- 🟡 **COUNTS QUOTED IN PROSE THAT NOTHING ASSERTS ON** — `README.md`'s 101/102/70/23/8,
  `lib/README.md`'s echo-site count, `tests/test_parity_harness.py`'s floor. Each now says so at its
  site. The repo owns the fix pattern (`tests/test_control_mutant_count_is_pinned.py` pins a README
  headline against its derived value); applying it is separate work. **Closing condition:** a decision
  to pin each or a written line saying why not.
- 🟡 **FOUR FILED BY #44'S LADDER** in `tests/test_publish_workflow.py` and
  `tests/test_control_mutant_count_is_pinned.py`, plus **three unpinned by construction** in
  `publish-image.yml` (`ci.yml:496`'s ARM count, step-level `if:` expressions outside the whole-body
  pins, and `log in to ghcr` being the one credential-handling `run:` step no test pins).
  **Closing condition:** one PR each, or a written line saying why not.
- 🟡 **THE DEPLOYMENT MANIFEST'S NODE-AFFINITY COMMENT IS STALE.** It keeps the pod off the off-LAN
  burst node *because the LAN registry does not resolve there*; the pod now pulls from ghcr, so that
  reason is void while the affinity may still be wanted (the PVC is ReadWriteOnce local-path).
  **A comment is a claim too. Closing condition:** the comment states the reason that is true, or the
  affinity goes.
- 🟡 **`tests/dualrun/` cannot see image drift, structurally.** It runs the TREE's `server.py`;
  nothing compares the Go server against the artefact actually serving. **Closing condition:** decide
  whether a deployed-artefact arm is worth owning, or write the line saying it is not.
- ✅ **CLOSED this session:** residual 8 in all four clauses; the `search --all-scopes` fan-out's
  measured-zero coverage; routed `validate`'s oracle divergence (an authorised oracle change); two
  unconditional-label mutants surviving on `defaultInstance` and `bannerFor`;
  `tests/routing_mutants.py` scoring a never-run suite as KILLED and exiting 1 on a missing toolchain;
  two CI floors with silent slack; and the flake wiring being pinned by prose only.
- Everything previously listed stands unchanged: #38's three residuals; `ScopeByNameIn`
  raw-vs-folded; P4 round 5's two prose defects; the degenerate-spelling limb; PR #15's six findings;
  the four deferred Go/oracle divergences; `server/seed.sh:110`'s `cd`; three files not `gofmt`-clean
  with nothing in CI grepping it; `-race` gated in one tier only; and P4 rounds 1 and 3's guards
  absent from the persistent battery.

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

- 🔴 **THE DUAL-RUN GATE HAS A ~1-MINUTE WINDOW EACH DAY IN WHICH IT STRUCTURALLY CANNOT VOUCH,
  AND IT GOES RED RATHER THAN QUIET.** The append route stamps the UTC date into the bullet and
  the ETag hashes the stamped content, so a run crossing midnight asks the two servers about
  **two different days**. The harness detects exactly that and exits **2** — "could not vouch" —
  which GitHub can only render as a failed job. Measured on #41: window `23:59:26Z → 00:00:40Z`,
  refusal printed at `00:00:00.75Z`, with `SELF-TEST mutants=7 caught=6` as a knock-on. The
  identical tree re-run at `00:09:48Z` was **all six green**. 🔴 **Do not read a midnight-UTC
  `dualrun` red as a regression, and do not "fix" it by loosening the gate** — the refusal is the
  design working. Diagnose it the same way: read the REASON; compare the job's wall time against
  its own baseline (74 s against 61–100 s); and check whether a SIBLING job on the same run was
  inflated (it was not — `parity` 58 s against 53–62 s). **Load inflates every job; a failed
  assertion inflates one.**
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
- 🔴 **A MISSING *INTERMEDIATE* `audit-claims` BLOCK DOES NOT REFUSE — IT SILENTLY WIDENS THE
  RANGE.** With #38's round-2 block never posted, `--round 3` anchored on **round 1's** tip and
  the "delta" would have spanned two rounds' fixes. It is announced **once, on stderr, and nowhere
  in the brief.** Check BOTH surfaces before concluding a block is absent — `issues/<n>/comments`
  AND `pulls/<n>/comments` (and `/reviews`): the script reads only the first, so a block posted as
  a REVIEW is invisible to it. The fix is to **post the missing block**, reconstructed from the
  fix commit's own DIFF — never from a handoff's prose about why the fix is correct, which is
  exactly the framing a blind round must not receive. ⚠ Related: `--emit-claims` **prints, it
  does not post**; the two halves fail independently, which is how the block went missing.
- 🔴 **A BLIND ROUND EARNS ITS COST EVEN AFTER A THOROUGH ONE.** #41's round 1 was given the diff,
  the environment and the failure CLASSES, but no findings and no conclusions, and told not to
  read the PR thread. It found two things no earlier pass had: a CA-bundle justification that was
  **false in both directions**, and an unguarded `Env` override that would have let a pod read its
  token from a compiled-in path. It also **refuted its own security hypothesis** by measurement
  and reported the refutation rather than the suspicion.
- 🔴 **ASK WHICH GUARD YOUR OWN CHANGE EMPTIES — it caught a real weakening in-round.** #41 added
  a second image to `flake.nix`, and the deployed image's guard used an **unscoped whole-file**
  `PATH` search — so deleting the Python image's override entirely still passed, because
  `PATH = serverPath` matched in the **Go** block. Red at `origin/main` → green on the branch →
  red again once scoped. A regression introduced and closed inside the same PR.
- 🔴 **A RANK SHUFFLE SILENTLY RE-POINTS EVERY LIVE CLAIM, BECAUSE THE RANK IS HALF THE SLUG.**
  `cairn-control-plane-1` was held with a subject describing the P5 slice after the cutover was
  promoted to rank 1, so `claim-work --slug-for <doc> 1` returned a ref naming different work, and
  `rc 12` ("already yours, carry on") would have let a session continue with the wrong item.
  **When you re-rank, re-subject the claims in the same breath.**
- 🔴 **A `docker run` IS NOT A READ OF THE IMAGE.** Inspecting the nix Python image through a
  container showed `/etc` present, contradicting `AGENTS.md`; the image's own layers carry **no
  `etc/`** — docker injects `mtab`, `resolv.conf`, `hostname` and `hosts` at runtime. The table
  was right and the container reading was the error. ⚠ The **Go** image genuinely does have
  `/etc/ssl/certs`, because `pkgs.cacert` is root-merged — not a contradiction.
- 🔴 **THE BUSYBOX NETWORK-SERVER COUNT WAS WRONG FOUR TIMES, EACH IN THE SAME DIRECTION, SO THE
  NUMBER IS GONE.** Drafts said two, four, then six. Enumerated from the pinned build, beyond
  those six there are `fakeidentd`, `udhcpd`, `lpd`, `dhcprelay`, `tcpsvd` and `udpsvd` —
  `tcpsvd`/`udpsvd` bind an arbitrary port and exec anything — with `ntpd`/`rdate`/`zcip` on a
  boundary no single number resolves. **The instruction stays, the count is deleted, and the
  record that four drafts were wrong is kept so nobody supplies a fifth.**
- 🔴 **THE LEAK GATE CAUGHT ME PUSHING A DENIED IDENTIFIER INTO THIS PUBLIC REPO — the same
  failure this document already recorded, reproduced by the session that had just read it.**
  `handoff_doc.py` owns the commit AND the push as one step, so "scan before pushing" does not
  exist in that flow: **scan the SCRATCH DELTA before handing it to the tool.** The way to do
  that, since `leakscan` has no single-file mode: drop the delta into the tree under a temp name,
  scan, then delete it — and **run a positive control first** (a file with a known denied
  identifier) to prove the scan reaches an untracked file. Measured: 1 hit on the control, 0 under
  test. Describe an external tool by its ROLE here, never by its name.
- ⚠ **A worktree created with `git worktree add -b <branch> origin/main` has upstream
  `origin/main`, so a bare `git push` there TARGETS MAIN.** Mine was refused only because
  `push.default=simple` requires the names to match — luck, not design. Push with an explicit
  refspec (`git push origin <branch>:<branch>`) and fix the upstream immediately.
- ⚠ **`xargs -0 command grep` FAILS** (`command` is a builtin xargs cannot exec) with 127 and
  empty output — indistinguishable from a clean zero. Use plain `grep` under `xargs`: it execs the
  binary, so this host's `.gitignore`-honouring `grep` FUNCTION never applies.
- ⚠ **TWO OPEN PRs BOTH EDITING THIS DOC CONFLICT WHILE BOTH REPORT `MERGEABLE`.** Measured again
  this session: #41 and the handoff PR both touched it, GitHub compared each against `main` where
  neither had landed, and `git merge-tree --write-tree` between the two exited **1**. Branch on
  the EXIT CODE — that command prints only a tree OID on success and emits no conflict markers.
- **Decision (operator): the cutover is SPLIT.** The Go server image ships; the
  `packages.default` flip is separate. ⚠ It waited on residual 8, which is now CLOSED — so the
  flip waits on the operator and on residual 7's contract widening, not on a capability.

- 🔴 **A DIFFERENTIAL HARNESS WHOSE ROUTE PATHS ARE GUESSED COMPARES REFUSALS AND REPORTS
  ZERO.** My first deployed-vs-Go comparison invented `/api/v1/entries?scope=…` and
  `/api/v1/recall?scope=…`; all 27 rows returned **404 `no such endpoint` from BOTH servers**
  and it printed `differing=0`. The real shapes are path-segment based
  (`/api/v1/recall/<scope>`, `/api/v1/search/<scope>?q=`, `/api/v1/entry/<scope>/<ref>`) and
  live in `tests/conformance/requests.json`; `cairn-server -routes` is the ledger. 🔴 **A
  non-5xx floor does NOT catch this** — the repo's rule that the floor is 500 rather than 400
  is about refusal UNIFORMITY, and a uniform 404 sails through it. The floor that caught it
  was a POSITIVE CONTROL (make the two stores differ; the count must move) plus an explicit
  `both-unrouted` counter. **Every route list is a claim that must be read out of the served
  contract, never composed from memory.**
  The corrected run reports `compared=39 differing=26 non5xx=39 both-200=27 both-unrouted=0`;
  the broken one reported `compared=27 differing=0`. Indistinguishable without the control —
  the third time this repo has recorded the shape, and the first time it was hit by the
  session that had just read about it.
- 🔴 **`nix build --print-out-paths` IS PLURAL, AND A MULTI-OUTPUT DERIVATION SILENTLY BREAKS
  `$GITHUB_OUTPUT`.** `nixpkgs#skopeo` prints its `man` output FIRST. Consuming the result as
  one path yields exit 127 and `Invalid format`; both errors name the binary, which reads like
  a missing tool rather than a two-line string. **Select the output explicitly, and prove the
  selection by running `--version` on what you resolved.**
- 🔴 **A TAG IN A REGISTRY IS NOT EVIDENCE OF A CI RUN.** `cairn-store` was anonymously
  pullable while every publish run had failed — a hand push and a green pipeline are
  indistinguishable from the registry side. **Read the workflow's run list, not the tag list.**
- 🔴 **`X-Forwarded-For` IS DELIBERATELY NEVER READ — I tested the wrong header first and got
  a clean 401/401 "agreement" that measured nothing.** The client is keyed on
  `CF-Connecting-IP` (`server.py`'s `client_ip`, via `sole_header`), because XFF is
  caller-supplied. Two servers refusing for the same wrong reason compare equal. **When a
  comparison's rows are all refusals, find the row that must be a 200 before believing it.**
- 🔴 **A `docker run` READS THE RUNTIME, NOT THE IMAGE — but an `export` of the container's
  filesystem reads the image.** Layer/tool inventories here were taken with
  `docker create` + `docker export` + `tar -tvf`, which is what makes the setuid counts (11
  vs 0) and the PATH inventories claims about the artefact rather than about docker.
- **Decision (operator, this session): PUBLISH AND SWAP.** The Go image gets published and the
  pod is pointed at it — accepting that the swap lands a new implementation AND the
  un-deployed oracle fixes in one rollout, because the only behavioural delta measured is the
  honesty-prose correction, and that correction is the point.
- **Decision (operator, this session): the busybox trade is ACCEPTED as a narrowing**, without
  an extra guard pinning the two images' applet sets equal. The measurement behind it is
  relational (identical sets), which is what avoids the counting trap that produced four wrong
  applet counts in earlier sessions. **Do not supply a fifth count.**
- **Dead end: a distroless Go image was not costed.** Busybox is load-bearing for both
  documented operations — `server/seed.sh` seeds through `tar -xf -`, and revocation is
  `sh -c 'kill -HUP 1'`. Removing it means replacing both procedures, which is a different
  piece of work from the cutover.
- ⚠ **The cluster could not be read from this host** — no context for it in this machine's
  kubeconfig, so every "deployed" claim here is about **the artefact the GitOps manifest pins
  and the registry serves**, not a live pod read. Name that scope when quoting these numbers.
- ⚠ **`claim-work` slug reminder, hit again:** the rank is half the slug, so re-ranking
  re-points live claims. This session holds `cairn-control-plane-1` with the cutover subject;
  ranks 1 and 2 below are both "the cutover", so re-subject before splitting them across two
  sessions.

- 🔴 **ONE DEFECT CAN MASK ANOTHER, AND THE SECOND IS ONLY OBSERVABLE ONCE THE FIRST IS FIXED.**
  Seven runs failed at the skopeo step, so nobody ever learned that the package could not be
  written either. Both were real, both blocked publishing, and no amount of staring at the
  seven red runs would have revealed the second. **When a gate has never once passed, expect
  the failure you can see to be hiding the next one — budget for a second round.**
- 🔴 **A MUTATION BATTERY THAT PARSES `FAILED` LINES SCORES EVERY MUTANT AS SURVIVED WHEN THE
  RUNNER NEVER RAN.** Measured on one tree: `python3 -m pytest` → `No module named pytest` →
  **14 mutants, 14 SURVIVED**; the same tree under an interpreter carrying pytest → **14
  KILLED**. Nothing in the output distinguished the two except the verdict. `publish_workflow_
  mutants.py` now reads the runner's own summary counts and refuses with **exit 2** when zero
  tests ran. ⚠ **`tests/control_mutants.py` has the SAME shape and has NOT been given that
  control** — run it under a pytest-carrying interpreter, or fix it.
- 🔴 **A GO IMAGE'S DIGEST MOVES ON EVERY COMMIT EVEN WHEN NO GO CODE CHANGED**, because the
  binary's store path embeds the short rev: `cairn-server-<rev>`. Measured across a prose-only
  PR — the Go digest moved, the Python one did **not** (`171ba281…` on both revs, because that
  image's content is rev-independent). **A moved digest is NOT evidence the server changed**,
  which is exactly what someone diffing digests to decide whether to redeploy would conclude.
- 🔴 **`textwrap.wrap` COLLAPSES RUNS OF WHITESPACE, so regenerating a whole-string pin with it
  silently rewrites the bytes the pin exists to assert.** Caught only because the test stayed
  red. Slice the string instead, and assert the round trip.
- 🔴 **A PREDICTION I STATED CONFIDENTLY WAS FALSIFIED BY THE FIRST RUN THAT TESTED IT:** "a
  ghcr package is PRIVATE on first publish, so the Go package's first run is EXPECTED to fail".
  It was created **public** and the proof passed. Four sites asserted it, one of them telling
  the reader to expect red. Retracted in #45 with the scope stated — one package, created by
  Actions, in a PUBLIC repo — and the refusal KEPT, because it is the only thing that would
  catch the case where that inheritance does not happen.
- **Decision (operator, this session): the busybox trade is a NARROWING, accepted.** Measured
  relationally to avoid the counting trap: the two nix images carry **identical** applet sets,
  so the cutover adds none; against the retired image it trades network applets for the loss of
  two interpreters and 11 setuid binaries.
- **Decision (operator, this session): registry move and implementation swap as ONE commit**,
  on the reasoning that they are entangled — the Go pod is published only to the public
  registry, so either half alone deploys nothing.
- ⚠ **A `Recreate` + `replicas: 1` rollout has a hard refusal window, and it was OBSERVED** —
  one 502 between the old and new text. Expected, brief, and worth telling anyone who watches
  a swap that a single non-200 mid-rollout is the design, not a fault.

- 🔴 **A PRUNED-TO ARCHIVE IS NOT A HANDOFF DOC, AND THE WRITE-BACK GUARD CANNOT KNOW THAT.**
  Moving blocks into `handoff-cairn-control-plane-archive.md` made the guard treat that
  filename as its own topic and demand a handoff for it. Checked rather than asserted before
  dismissing: the archive carries **zero** of `## Goal`, `## State now`, `## Next steps`,
  `## How to verify` or a closing-condition, and the prune is already described inside the real
  doc. **Dismiss is the right answer there** — a handoff written "for the archive topic" would
  mint a doc nobody wants. ⚠ The dismissal is per-session; a new session starts fresh.
- 🔴 **CHECKING A CLAIM BEFORE DISMISSING A GUARD IS WHERE THE DEFECT WAS.** The dismissal
  itself was correct, but verifying the reasoning turned up a **wrong count**: the prune note
  said three blocks moved and four had. It was written while drafting the delta — *before the
  act it describes*. **Derive a count AFTER doing the thing, from the thing**, and when
  correcting one, record the cause rather than only the number.
- ⚠ **A HANDOFF DOC CAN NEVER RECORD ITS OWN MERGE**, so a `State now` naming the branch sha
  is stale by exactly one commit the moment it lands. That is the mechanism, not rot — do not
  read it as the doc being behind, and do not open a PR solely to bump it.
- 🔴 **THE SAME TWO FACTS WERE CARRIED FORWARD BY HAND THREE UPDATES RUNNING, WHICH IS WHAT THE
  DURABLE-DROP WARNING IS ACTUALLY FOR.** `State now` REPLACES, so anything durable parked there
  must be re-typed every single update or it silently disappears — and the warning is a textual
  FLOOR, so a reworded carry-forward still trips it and a silent run still is not proof nothing
  was lost. Moved here instead, where the section APPENDS and the problem stops recurring:
  **all three audit ladders are CLOSED** — #38 over rounds 0–4 and #41 over rounds 0–2, both
  ending on the attribution gate rather than on a clean round; **#44 over rounds 0/1/2, stopped
  MECHANICALLY** (`audit-dispatch.py --round 3` → **rc 5**) after two consecutive payload-0
  rounds. And the **INERT-BACKENDS defect is CLOSED ON `main`**, not merely in a branch: #38
  landed as `2055bd2`. **The general rule: if you find yourself re-typing a fact to stop a
  REPLACE heading eating it, it belongs under an APPEND one.**

- ✅ **THE CUTOVER IS COMPLETE AND VERIFIED — CARRIED HERE BECAUSE `State now` REPLACES.** The pod
  runs the **Go server** from the public registry at an immutable `sha-<40-hex>` tag; one GitOps
  commit moved the REGISTRY and the IMPLEMENTATION together and the parsed manifest differs from
  its predecessor in `image` and nothing else. Verified **against the runtime symptom through the
  public endpoint with no cluster access**: four polls on the old text, one 502 (the `Recreate`
  window at `replicas: 1`, expected), then the corrected read-through-cache caveat. Negative
  controls first (bad token → 401, no token → 401), then recall 200, search 200, snapshot 200
  extracting 276 members, an absent scope 200 carrying `status=scope-absent`, `cairn sync` rc 0.
  **The publish workflow succeeds** after seven consecutive failures, both packages anonymously
  pullable. **This is the third update running that a durable fact had to be hand-carried out of
  `State now`; the doc's own rule is that such a fact belongs under an APPEND heading.**
- 🔴 **#48'S AUDIT LADDER: FOUR ROUNDS, ENDED ON A CLEAN ROUND, AND EVERY ROUND FOUND SOMETHING
  SIX GREEN CI CHECKS COULD NOT SEE.** Round 0 (requirements) found `searchEveryInstance`
  shipping with **measured-zero coverage** — `if true { return ExitOK, nil }` as its first
  statement **compiled** and left the suite at 51 PASS / 0 FAIL, byte-identical to baseline.
  Round 1 (blind, nine axes) found the merge blocker: routed `validate` disagreed with the oracle
  and **the oracle was the wrong side**. Round 2 found two unconditional-label mutants surviving
  on `defaultInstance` (the no-scope `cairn validate` path — the DEFAULT invocation) and
  `bannerFor`, plus five sentences a previous fix round had written. Round 3 was **clean**.
  ⚠ **The attribution gate was one round from firing** — round 3's fixes changed **0** payload
  lines — so the ladder ended on cleanliness with the mechanical stop right behind it.
- 🔴 **THE LADDER'S 100% PROSE BASE RATE BROKE, AND THAT IS THE SIGNAL THAT ENDED IT.** Every
  round until the last found a defect in the PREVIOUS round's prose — round 1's fixes created
  round 2's findings, round 2's created round 3's. Round 3's rewritten prose audited **true**,
  including its meta-claims about round 2's prose. **A base rate breaking is evidence; "I have had
  enough rounds" is not.**
- 🔴 **AN IDE DIAGNOSTIC SURFACE IS AN INSTRUMENT TOO, AND IT REPORTED A BRANCH THAT DOES NOT
  COMPILE WHILE CI WAS 6/6 GREEN — REPEATEDLY.** The editor reported `not enough arguments in
  call to StoreHostLine`, `undefined: RefuseUnportedMultiInstance` and a dozen `UndeclaredName`s,
  every time resolving some files from an agent's worktree and others from the base clone on a
  different commit. All false, every time. **The discriminating control is a BUILD in the tree you
  are asking about** — `go vet ./...` there, rc captured before any pipe. Read a diagnostic panel
  as a claim about the indexer's view, never about a tree.
- 🔴 **I DOUBTED A CORRECT AGENT REPORT BECAUSE MY OWN GREP WAS THE WRONG INSTRUMENT.** Checking
  "three multi-instance READ parity rows", I grepped the harness for row names matching
  `multi-instance` and found only the two pre-existing `routes-*` rows — which reads exactly like
  an unsupported claim. The rows are named `recall-routed-to-a-NON-DEFAULT-instance`,
  `recall-routed-to-the-DEFAULT-instance-is-still-labelled` and `ls-entries-walks-EVERY-instance`.
  **Validate the instrument before disbelieving the report**: a zero from a pattern you chose is a
  fact about the pattern.
- 🔴 **TWO SUITE COUNTS THAT DISAGREE ARE USUALLY TWO PACKAGE SETS, AND THIS LADDER PRODUCED THE
  SCARE TWICE.** An audit reported `74 PASS` where the next round measured `54`; both were right
  (`./internal/client ./internal/report ./internal/hostid` versus `./internal/client` alone).
  Later, `54` versus `911` — the same thing against `./...`. **Name the PACKAGE SET beside any Go
  count**, the way this repo already says to name the tree. The agent that flagged the
  discrepancy rather than papering over it is what made it one command to settle.
- **A MERGED-TREE GATE IS FREE WHEN THE MERGE-BASE EQUALS THE BASE TIP — SAY SO RATHER THAN
  BUILDING ONE.** Throughout #48, `git merge-base origin/main <branch>` returned `main`'s own tip,
  so the merged tree WAS the branch tree and the branch's green WAS the merged green. It stops
  being free the moment anything lands on `main`; re-run the check, never remember its answer.
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
- 🔴 **THE `/audit-pr` BRIEF'S "WHERE TO WORK" AND THIS REPO'S GOTCHA DISAGREE, AND THE REPO IS
  RIGHT — THE BRIEF SAYS IT CONFIDENTLY AND SAYS IT EVERY TIME.** `audit-dispatch.py` prints
  *"Dispatch with `isolation: "worktree"` — the flag worktrees the CWD's repo, and here that is
  the right one."* True about the REPO and wrong about the REF: the flag branches from the DEFAULT
  branch, so an agent sent at an unmerged PR gets a tree of `main` with none of it. Every one of
  the six dispatches across this PR got a hand-built `git worktree add` at the exact sha plus **a
  base check it could fail**. Zero mis-targeted agents.
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
- ⚠ **`leakscan` EXIT 2 IS "COULD NOT VOUCH", AND A LEFTOVER AGENT WORKTREE CAUSES IT.** A removed
  agent's worktree directory under `.claude/worktrees/` made the scanner exit 2 with
  `COULD NOT READ … Is a directory`. Not a leak and not a pass. Check the worktree is clean and
  its commits are on `origin` **before** removing it — then re-run for a real verdict.
- **Decision (operator, on #48): the oracle may be changed when a contract cannot include the
  behaviour.** `cmd_validate`'s negative count is the second such exception, after
  `seeded=UNREADABLE`. Both were chosen over the two alternatives — declaring a residual, and
  making the port bug-compatible — and the reasoning is recorded at the change site, not only
  here.
- **Decision (operator, on #48): residual 8 and the flip are TWO PRs.** Bundling them would put
  an irreversible-for-consumers contract change inside a PR whose stated subject is a narrowing
  being closed, where no reviewer is looking for it.
- ⚠ **The host-dependent red was RE-MEASURED, not assumed.**
  `tests/test_cairn_doctor.py::TestTheCliWiring::test_a_no_sync_run_still_reads_the_LOCAL_config`
  fails `IndexError` at `:876` because `~/.config/subsystem-store/instances/` holds one instance
  file (mtime unchanged across the whole session). Local: **1 failed, 1969 passed**; **CI's
  `tests` job passes**, which is the control saying it is the HOME and not the tree. The real
  defect is that the test inherits the operator's HOME rather than pinning one.
- ⚠ **THE CROSS-REPO HANDOFF SEARCH HAS ZERO REACH INTO THIS REPO**, so that half of `/resume`
  step 4 is inert here. Its scope line names four OTHER repositories and `in_scope_docs` equals
  `indexed_docs` — this repo is absent from the indexer's repo handles, so `--exclude-slug` parsed
  and matched nothing **because no doc from here is indexed at all**. A query about this arc
  returns unrelated hits from unrelated repositories, which must not be pasted into this PUBLIC
  repo. **`cairn recall --repo` is the only step-4 surface that reaches this work.**

- 🔴 **CI NEVER RUNS `nix flake check` — THE `nix` JOB BUILDS EACH CHECK BY NAME, SO A `checks.*`
  ENTRY NOBODY NAMES IS A CHECK NOBODY RUNS.** Verified on `main`: no flake-check step, eight
  explicit `nix build .#…` steps. All pre-existing checks ARE named, so there was no latent hole —
  but the convention is one omission away from producing one, and the omission reads as covered
  because the check exists in `flake.nix`. **Adding a `checks.*` entry means adding a CI step in the
  same commit.**
- 🔴 **A GUARD WRITTEN FOR ATTRIBUTE A DOES NOT COVER SIBLING ATTRIBUTE B — AND A PROMISE MADE IN
  PROSE CREATES A NEW THING TO PIN.** The flip's guard pinned `packages.default`/`apps.default` and
  left `apps.cairn` free, in the same PR whose `README.md` promised `#cairn` was the opt-out.
  Repointing `apps.cairn` kept all three original assertions green while the announced hatch became
  the Go client. **Ask of every new guard: what sibling does this not reach, and did this change make
  that sibling load-bearing?**
- 🔴 **A COMMON-MODE OPERAND DEFEATS AN EQUALITY GUARD SILENTLY.** The flip guard compared two
  `getExe`-derived paths — the *same expression* — so a wrong `meta.mainProgram` moved BOTH operands
  together, leaving the guard green while `nix run` would fail. Measured: `getExe` without
  `mainProgram` emits only a deprecation **warning** and returns a differently-named path. **An
  equality between two values derived the same way tests the derivation, not the wiring.** Closed
  with a base-name assertion.
- 🔴 **A GUARD'S NAMED HAZARD CAN BE UNREACHABLE, WHICH MAKES IT AN INVARIANT GUARD RATHER THAN A
  REGRESSION GUARD — AND THE LABEL MATTERS.** The flip guard's store-path floor was written as if it
  caught a missing attribute; a missing flake attribute is an **eval error**, not `""`. Worth keeping
  (two empty strings compare equal, which is the zero an equality gate must rule out) but relabelled,
  because a comment claiming regression coverage it does not provide is how the next reader stops
  looking.
- 🔴 **AN ANNOUNCEMENT IS ONLY AS GOOD AS THE CONSUMPTION MODE IT IS SPELLED FOR.** #50's first draft
  gave the opt-out only as a CLI fragment (`#cairn`) while `README.md`'s own second line says
  consumers **pin this flake as an input** — so the people most affected were handed an opt-out they
  could not paste. Found by round 0, not by any gate. **Ask who reads this and in what form**, and
  anchor the announcement to a **sha or PR number, never a date** (the leak gate refuses dates, and a
  sha says which tree rather than when somebody looked).
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
- **Decision (operator, this session): the oracle may be changed when a contract cannot include the
  behaviour.** `cmd_validate`'s negative entry count is the second such exception, after
  `seeded=UNREADABLE`. Chosen over declaring a residual and over making the port bug-compatible.
- **Decision (operator, this session): the flip ships, and it is a DECISION rather than a gate
  outcome.** Residual 8 closing removed the measured blocker; a green gate has never licensed this
  flip, and the branch that read it that way was reverted. Recorded at the change site and in
  `flake.nix`, not only here.
- **Decision (operator, this session): the default-wiring guard lands WITH the flip**, not as a
  follow-up — the flip is what makes its claim true, and filing it would have left the repo's most
  consequential wiring unpinned for the length of the follow-up.
- ⚠ **#50's ladder was round 0 + one fix round, by operator choice, and round 0 was the round that
  paid.** It found the announcement gap and the `apps.cairn` hole — neither visible to any gate, both
  in the half of the PR that exists for people outside this repo. **Round 0 is the only round that
  can ask whether the thing should exist, and it is only actionable while the merge decision is
  open.**

## How to verify
```bash
cd /home/zach/workspace/cairn
python3 tests/leakscan.py; echo "rc=$?"      # CAPTURE THE RC BEFORE ANY PIPE
uv run --python 3.12 --with pytest -- pytest tests -q -p no:randomly
go vet ./... && go test ./... && go test -race ./...
python3 -u tests/control_mutants.py
uv run --python 3.12 --with pytest -- python -u tests/publish_workflow_mutants.py
python3 tests/conformance/suite.py run       # oracle: 0 failures
bash tests/conformance/run_go.sh             # Go: 0 failures, 4 skips
python3 tests/dualrun/harness.py             # SUMMARY … differences=0
python3 tests/parity/harness.py --break-pod  # MUST exit 2 — could not vouch
```
🔴 **RUN THE MUTATION BATTERIES UNDER AN INTERPRETER THAT HAS `pytest`.** A bare `python3` has
none; the publish battery now refuses with **exit 2** rather than printing a false all-SURVIVED,
but only that one does — see the Gotcha.
🔴 **`dualrun` and `parity` exit 2 for "COULD NOT VOUCH", which is NOT "failed"**.
🔴 **VERIFY A DEPLOY BY THE RUNTIME SYMPTOM.** For this pod the discriminator is the rendered
caveat: the retired image says the store is *"PER-HOST and unreplicated"*, the current one says
it is *"read through a PER-HOST CACHE"*. A recall through the public endpoint tells you which
is serving without any cluster access at all.
🔴 **Verify a squash merge BY CONTENT, never by ancestry.**
🔴 **Reading CI: require SIX checks present AND all COMPLETED.**

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
