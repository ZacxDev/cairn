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
- Branch `main` @ `57daa58`, clean, up to date with origin. **No open PRs.** Four merged
  this session: **#29** (P3a), **#30** (P3b-a), **#31** (P3b-b), **#32** (the AGENTS.md
  eviction). The shared-queue claim `cairn-control-plane-1` is **RELEASED** — rank 1 is
  done and the queue is empty for this doc.
- ✅ **RANK 1 (P3 — the data model and authorization) IS DONE, and the arc's FIRST
  closing-condition clause is SATISFIED on `main`**: `TestTheAuthorizationMatrixIsExactlyThis`
  (the model, 5 principals × 4 scopes × 3 verbs, 32/60 allow) and
  `TestTheServedAuthorizationMatrixIsExactlyThis` (the SERVED seam, 3 × 4 × 5 over real
  HTTP, 24/60 allow) both green. The other clauses — the PWA share flow, identity through
  both backends, `packages.default` the Go client — are untouched, so **the arc is OPEN**.
- **What landed, by piece:**
  - **P3a** (#29 `bcfaa19`) — `internal/control`: the model, an append-only event journal,
    and `Resolve`, the ONE predicate. Library only.
  - **P3b-a** (#30 `3c8707c`) — `internal/control/cache.go`: the materialized cache, its
    epoch, `Staleness`, and a synchronous revoke that reports which effect it had.
  - **P3b-b** (#31 `752415d`) — `internal/api` authorises from `internal/control`;
    `internal/control/tokenfile` projects the token file into a `control.Source`.
    🔴 **Piece (c), the migration, is ABSORBED into this** — the adapter IS the migration,
    which is why the conformance corpus could stay byte-for-byte unmoved and mean
    something.
  - **The AGENTS.md eviction** (#32 `57daa58`) — the server section relocated to
    `tests/conformance/README.md` + `tests/dualrun/README.md`; the defect's own closing
    condition (`MAX_BYTES` *lowered*) satisfied.
- 🔴 **DECISION (operator, this session): PIECE (d) IS DEFERRED UNTIL AFTER THE CUTOVER**,
  and it is folded into that rank rather than standing as its own. Reason: the snapshot tar
  ships exactly `<scope>/*.md` at depth 2 with dotfiles skipped (verified in
  `internal/snapshot/snapshot.go` and the oracle), so there is no free place to put a scope
  id — every route costs either a new public endpoint on BOTH servers, a licensed
  difference in a gate whose whole value is having none, or a change to the byte-identity
  tar. All three mean teaching the Python oracle a trick P8 deletes. After the cutover
  there is ONE implementation and none of that applies. The cost accepted: a renamed scope
  re-downloads, which is correct and merely wasteful.
- 🔴 **DECISION (operator, this session): the legacy bare-token row SURVIVES the rewrite**
  — the plan's FIFTH deferred decision, which #31 was settling silently until a round-0
  audit caught it. Recorded in the plan doc and `internal/control/README.md` with its
  reason: the deployed pod's only principals are bare rows, so dropping it now is a
  coordinated cutover against a live deployment. P8 inherits the retirement question.
- 🔴 **DECISION (operator, this session): a cold start over an unreadable store root
  REFUSES** (exit 78, naming the volume) rather than coming up and serving 503s as the
  oracle does. Declared as a divergence with a closing condition.
- **Deploy/verify status: unchanged — nothing deployed.** `flake.nix:446` is still
  `default = mkCairn pkgs`, the Python client. The Go server has still never run against
  the real pod.
- **Gates on `main` @ `57daa58`:** 1,927 python tests · `go vet` + `go test ./...` **14
  packages ok** · control battery **72 mutants / 71 killed / 1 EQUIVALENT / 0
  misattributed / 0 harness-errors**, positive control GREEN · corpus **116 PASS / 0
  failures / 4 oracle-specific skips** (Go) and **99 requests / 433 assertions / 0
  failures / 0 skipped** (oracle) · leakscan **0 findings / 274 files**, both controls PASS
  · `AGENTS.md`+`CLAUDE.md` **30,748 B against a 30,950 B working budget — 202 B margin**.

## Next steps (ranked)
1. **P4 — identity as an interface, two backends.** Supabase JWT verified locally against
   cached JWKS, and trusted-header/forward-auth for a proxy-fronted instance.
   🔴 The trusted-header backend must **refuse to start** unless explicitly configured as
   proxy-fronted with a source check — otherwise anyone reaching the pod directly is
   anyone. `control.Principal` now exists for it to resolve to.
   🔴 **AND IT INHERITS THE STDLIB-ONLY DECISION.** `go.mod` has no `require` block and
   `flake.nix` passes `vendorHash = null`, so every Postgres driver and every embedded SQL
   engine is a BUILD FAILURE. The plan's decision 1 (authz state in Postgres) cannot be
   implemented without first deciding what happens to stdlib-only. Settle that BEFORE
   writing a second `control.Store` backend, not halfway through.
   forcing: user — "identity via supabase (github and google)", plus a named second
   instance that will front it with an oauth proxy.
2. **P5 — the PWA** (Tailwind + gomponents + htmx). Sign-in, projects, members, scopes,
   entry view, search, **share dialog**, credentials, grant log, status. 🔴 The share
   dialog must state that unsharing cannot recall a replica — every client holds a full
   local copy, so revoking stops future syncs and deletes nothing already on disk
   elsewhere. Pin the whole normalised string, not keywords.
   forcing: user — "a fully featured UI (PWA tailwind + gomponents + htmx webapp)".
3. **P7 — conditional snapshot sync.** `GET /api/v1/snapshot` ships a full tar with no
   ETag/304, so every sync is O(store) per client. 🔴 With P3 landed this is correctness,
   not scale: with many principals a mis-keyed cache cross-serves another tenant's tar, so
   key it on **principal + epoch** — and `control.Authorization` already carries `Epoch`.
   forcing: none
4. **The cutover, and P3(d) immediately after it.** `packages.default` → the Go client and
   the deployed image → the Go server; 🔴 `AGENTS.md` says to diff the two images for what
   the agreement test cannot read first, and that instruction now has an instrument behind
   it. **Then P3(d)** — immutable scope ids shipped to the client and client-side rename
   reconciliation — which is deferred to here by operator decision because before the
   cutover it costs a new endpoint on a component being retired (see `State now`).
   forcing: none
5. **P8 — retire the Python oracle.** Gated on 4 having held in real use. The retirement
   ledger in `tests/parity/README.md` lists what dies with it; the legacy-bare-row
   retirement question is now explicitly P8's.
   forcing: none

## Defects (batched)
Fix as one round; closing one buys room for one rank.
- ✅ **CLOSED this session: the `AGENTS.md` byte-budget defect** (#32). The server section
  relocated and `MAX_BYTES` was **lowered** 37,700 → 31,850, which was the closing
  condition as written. ⚠ Margin is 202 B, not comfort: the ceiling was lowered to keep the
  SAME design margin against a smaller file, deliberately, so the next addition still pays
  ceiling.
- **Audit round-3 residue on #31, filed rather than fixed** (all scaffolding, no payload
  consequence): `internal/control/README.md`'s "current at every commit from X to Y" range
  is charitable rather than exact; the new cache test's re-entrancy precondition is stated
  in the battery harness but not beside `clock()`; and the deleted EQUIVALENT label carried
  a second wrong claim (severity, not only reachability) that the retraction does not name.
- **`claudedocs/handoff-cairn-control-plane.md`'s own verify block was stale** for most of
  this session — scoped to `5c02f2b`, quoting `28 / 27 killed` and `1860 passed`. Fixed in
  this update; the general shape is that a verify block pinned to a sha rots silently.
- **Three files are not `gofmt`-clean** on `main` (`internal/client/exit.go`,
  `internal/client/options.go`, `internal/doctor/doctor_test.go`) — alignment only, arrived
  with the #24 merge, and **nothing in CI greps `gofmt`**.
- **PR #15's six findings, still open on `main`** (recorded at
  https://github.com/ZacxDev/cairn/pull/15 when the operator chose to merge with them
  open): `lib/cairn_doctor.py` claims "2,016 bytes" for a HOME-length-dependent value;
  `cairn:337` cites `server.py:422` for `sole_header`, which is at `server/server.py:1409`;
  and the ledger's vacuity-control prose names the wrong guard.
- **Go/oracle divergences deferred with closing conditions in code**: `NaN`/`Infinity`,
  the `text`-field surrogate message, the `actor`-key 400-vs-200 residual, and
  `?page=<21 digits>`.
- **`server/seed.sh:110`** has the `( cd "$1" && … )` shape that made
  `tests/conformance/run_go.sh` unrunnable — a bare `cd <relative>` **prints** the
  directory when it resolves through `CDPATH`. The other scripts use `CDPATH= cd --`.

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

## How to verify
```bash
cd /home/zach/workspace/cairn
python3 tests/leakscan.py && python3 tests/leakscan.py --self-test   # 0 findings / 274 files, controls PASS
uv run --python 3.12 --with pytest -- pytest tests -q -p no:randomly # 1927 passed
go vet ./... && go test ./...                                        # 14 packages ok
python3 tests/control_mutants.py                                     # 72 / 71 killed / 1 EQUIVALENT / 0 misattributed
python3 tests/control_mutants.py --show                              # every mutation, without running it
python3 tests/conformance/suite.py run                               # oracle: 99 requests / 433 assertions / 0 / 0
bash tests/conformance/run_go.sh                                     # Go: 116 PASS / 0 failures / 4 skips
python3 tests/parity/harness.py                                      # 97 cases / 98 passes / 0
python3 tests/parity/harness.py --break-pod                          # MUST exit 2 — could not vouch
python3 tests/dualrun/harness.py                                     # 361 / 1489 / 0
python3 tests/dualrun/harness.py --break-both                        # MUST exit 2 — could not vouch
```
🔴 The two `--break-*` runs are the point: a gate that cannot refuse has not been read.
🔴 `parity` and `dualrun` need a LIVE POD; CI runs both. Say so rather than implying you ran
them.
🔴 **Before merging alongside another open PR, BUILD THE MERGED TREE and run the gate
there** — `git worktree add <tmp> -b tmp/merged <yours>` → `git merge origin/<theirs>` →
run `pytest tests -q`, `go test ./...` and `tests/test_agent_instructions_weight.py` in it.
A clean `git merge` and a green `mergeable` are not evidence; this session measured a RED
merged tree behind both.
