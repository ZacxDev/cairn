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
- Branch `main` @ `0d9d3fa`, clean. The base clone sits on `docs/handoff-p4-ceiling` (PR #37,
  `claudedocs/` only — no interaction with the byte gate).
- 🔴 **RANK 1 (P4, identity) IS STILL PR #35 @ `7ac810e`, OPEN AND NOT MERGED.** Claim
  `cairn-control-plane-1` is HELD (rc 12, this session).
- **What #35 contains:** `internal/identity` — an `Authenticator` interface over three
  backends (`MachineToken`, a verbatim move of the old token path; `SupabaseJWT`,
  JWKS-only; `TrustedHeader`, for a proxy-fronted instance), all resolving to one
  `control.Principal` with authentication and authorization from the same match.
- 🔴 **DECISION (operator, carried forward): the Supabase project uses ASYMMETRIC keys, so
  the legacy HS256 path is DELETED.** Retired: `AlgHS256`, `keyOct`, `staticSecret`,
  `resolverPair`, `MinHS256SecretBytes`, `ErrSupabaseWeakSecret`, both `_JWT_SECRET*`
  env vars, `SupabaseConfig.Secret`, the HMAC signer, the weak-secret construction rung.
  ⚠ **The HS256 *refusal* is KEPT and is now unconditional** — deleting support without
  the check is how algorithm-confusion returns. Round 1 re-measured the guard: deleting
  the `symmetricAlg` call site turns **6/6** subtests red, each naming
  `ErrTokenAlgSymmetric`, and **no arm reports algorithm confusion** — confirming the
  guard's honest scope is "it NAMES the refusal", not "it is the only refusal".
- 🔴 **DECISION (operator, carried forward): `MAX_BYTES` raised 31,850 → 32,500**, base
  named (30,999 B measured merged content) — the Gotcha below carries the reasoning, and
  the boundary set has now been re-watched firing on the merged tree.
- ✅ **CI IS CONFIRMED GREEN, AND THE READING CARRIES ITS OWN PROOF.** All **six** checks
  present AND `COMPLETED`/`SUCCESS` — `leakscan`, `tests`, `go`, `parity`, `dualrun`, `nix` —
  on run `35163448166`, whose `headSha` was read back and equals `7ac810e`. The previous
  head's RED `tests` (`assert 851 >= 900` at `1dd2815`) is **resolved**, not merely rerun.
- ✅ **THE MERGED TREE WAS BUILT AND MEASURED, because `7ac810e` does NOT contain
  `origin/main` (`0d9d3fa`).** In a throwaway worktree, `main` merged with the PR:
  `AGENTS.md`+`CLAUDE.md` = **30,999 B** (`wc -c`) ⇒ headroom **1,501** against
  `MIN_HEADROOM 900`; `pytest tests` **1938 passed / 0 failed**; `go vet` clean;
  `go test ./...` **15 `ok`** counted from the result lines; `leakscan` **rc 0** (captured
  before any pipe, stderr read separately) over **293 files**; `--self-test` rc 0.
- ✅ **ROUND 1 OF THE AUDIT LADDER IS DONE — dispatched BLIND, five findings, NO 🔴.**
  Three 🟡: a ledger guard that restates the constants it says it *discovers* (so it cannot
  fail when the set GROWS, proven by adding a real setting and watching `go test ./...` stay
  green); `anySet`'s `TrimSpace` silently disabling a backend whose value is whitespace; and
  `internal/identity/README.md:366` claiming `KeySet` "is exercised under `-race`" when
  **nothing in the repo runs `-race`**. Two 🟢: an empty secret *file* mis-blamed on the wrong
  setting, and `JWKSOptions.Interval`'s doc contradicting its constructor. All five
  re-verified at source by the orchestrator rather than accepted.
- 🔴 **DECISION (operator, this session), on the two findings that had a choice:** add
  **`-race` to the `go` CI job** (measured free today: rc 0, 15 ok, zero races) rather than
  deleting the README claim; and **refuse a whitespace-only ledger value** with a sentinel
  naming the variable, rather than making `anySet` presence-keyed.
- 🔴 **THE FIX ROUND IS IN FLIGHT** in a worktree on `feat/identity-interface` at `7ac810e`;
  at the time of writing it had pushed nothing. **ROUND 2 (the delta re-audit against
  `7ac810e`) IS STILL OWED** — round 1 was not clean, so the ladder continues.
- **Deploy/verify status: unchanged — nothing deployed.** `flake.nix` default is still the
  Python client; the Go server has never run against the real pod; **neither new identity
  backend has ever run anywhere**, and the Defect below says why that is structural.

## Next steps (ranked)
1. **P4 — identity. 🔴 IN FLIGHT as `ZacxDev/cairn#35` @ `7ac810e`.** CI and the merged tree
   are both confirmed green; round 1 is done. What remains: land the five fixes, **re-run the
   full gate set on the MERGED tree again** (the base moved once already — a pre-fix green is
   a measurement of a different tree), then **round 2 as a delta re-audit against `7ac810e`**,
   framed as *what was claimed fixed* and never *why it is correct*. The ladder ends on the
   first round that finds nothing — not on a "safe to merge" verdict, which round 1 already
   returned while reporting five real defects.
   forcing: user — "identity via supabase (github and google)", plus a named second
   instance that will front it with an oauth proxy.
2. **P5 — the PWA** (Tailwind + gomponents + htmx). Sign-in, projects, members, scopes,
   entry view, search, **share dialog**, credentials, grant log, status. 🔴 The share
   dialog must state that unsharing cannot recall a replica. Pin the whole normalised
   string, not keywords. 🔴 **P5 IS ALSO WHAT MAKES P4 REACHABLE** — see the Defect:
   both new backends are inert until something creates a `control.User`.
   forcing: user — "a fully featured UI (PWA tailwind + gomponents + htmx webapp)".
3. **P7 — conditional snapshot sync.** `/api/v1/snapshot` ships a full tar with no
   ETag/304. 🔴 With P3 landed this is correctness, not scale: with many principals a
   mis-keyed cache cross-serves another tenant's tar, so key it on **principal + epoch** —
   `control.Authorization` already carries `Epoch`.
   forcing: none
4. **The cutover, and P3(d) immediately after it.** `packages.default` → the Go client and
   the deployed image → the Go server; 🔴 diff the two images first. **Then P3(d)** —
   immutable scope ids and client-side rename reconciliation — deferred here by operator
   decision.
   forcing: none
5. **P8 — retire the Python oracle.** Gated on 4 having held in real use.
   forcing: none

## Defects (batched)
Fix as one round; closing one buys room for one rank.
- 🔴 **BOTH NEW IDENTITY BACKENDS ARE STRUCTURALLY INERT IN EVERY DEPLOYMENT THAT CAN
  EXIST TODAY.** Verified from the code, all three legs: no binary constructs a
  `control.FileStore`, so `tokenfile.Source` is the only authority; it synthesizes exactly
  one user (`Provider = "cairn-token-file"`, `Subject = "operator"`), while `SupabaseJWT`
  defaults to provider `"supabase"` — so `UserByProviderSubject` can **never** match; and
  `tokenfile` emits **zero** `EventMemberSet` with all three grant sites using
  `SubjectKind: KindProject`, so even a `TrustedHeader` pointed at `cairn-token-file`
  authenticates someone who resolves to an **empty `Authorization`**. The trap: an operator
  follows the README, gets a pod that fetches JWKS, starts clean, passes its health check
  — and refuses **every** sign-in. **Closing condition:** a user-creation path (P5/P6) over
  a journal-backed `control.Store`. 🔴 Do NOT close it by creating users on the fly; that
  is self-serve signup (P6) and would be a silent authorization decision.
- 🔴 **AN IN-REPO AGENT WORKTREE MAKES LOCAL `leakscan` UNABLE TO VOUCH — exit 2, not a
  finding and not a pass.** With a subagent worktree at `.claude/worktrees/agent-*/`,
  `tests/leakscan.py` exits **2** with `COULD NOT READ …: [Errno 21] Is a directory` on
  **stderr**, having printed every control PASS on stdout. Mechanism: it enumerates with
  `git ls-files --cached --others` (deliberately — an unstaged file must not hide),
  `.claude/` is untracked and NOT gitignored, and git reports a linked worktree as one
  DIRECTORY entry. Control: 276 enumerated paths with `.claude/`, 275 without. ⚠ CI is
  unaffected (fresh checkout). **Closing condition:** `leakscan` skips a directory entry
  *and names it in its summary*, plus a negative control proving the skip is reported.
  Gitignoring `.claude/` is the wrong fix — it would hide project config that belongs in
  the scan.
- **Audit round-3 residue on #31** (scaffolding, no payload consequence): the README's
  "current at every commit from X to Y" range is charitable rather than exact; the cache
  test's re-entrancy precondition is stated in the battery harness but not beside
  `clock()`; the deleted EQUIVALENT label carried a second wrong claim (severity, not only
  reachability) that the retraction does not name.
- **`retiredEnv` is a behaviour change nobody asked for**: a stale
  `CAIRN_SUPABASE_JWT_SECRET` now exits 78. Introduced because dropping a name from
  `supabaseEnv` **inverts** the partial-configuration rule (`anySet` only counts names in
  a ledger, so the variable would be silently discarded by a healthy pod). Judged correct
  and kept; recorded because it is new refusal surface.
- **Three files are not `gofmt`-clean** on `main` (`internal/client/exit.go`,
  `internal/client/options.go`, `internal/doctor/doctor_test.go`) — **nothing in CI greps
  `gofmt`**.
- **PR #15's six findings, still open on `main`**; **Go/oracle divergences deferred with
  closing conditions in code** (`NaN`/`Infinity`, the surrogate message, the `actor`-key
  residual, `?page=<21 digits>`); **`server/seed.sh:110`**'s `( cd "$1" && … )` shape.
- ✅ **CLOSED, THEN PARTLY RE-OPENED: the `AGENTS.md` byte-budget defect.** #32 satisfied
  the written closing condition exactly — the P1 server history relocated (5,960 B) and
  `MAX_BYTES` **lowered** 37,700 → 31,850. ⚠ **It then failed within hours**, and the
  record is kept whole rather than replaced because the failure is the useful half: two
  concurrent one-row additions (84 B + 166 B) consumed the 202 B margin and reddened the
  merged tree, so #35 raised the ceiling to **32,500** with the base named. The defect is
  closed in the sense that the eviction was done and is not to be re-litigated; what
  remains open is the **structural** half — `Installing and building with nix` (8,321 B)
  and the server section's remains (9,071 B) are still evictable, and this file's design
  is that history leaves rather than the ceiling rising. **Closing condition for the
  remainder:** the next contributor who needs bytes evicts one of those two rather than
  moving the number a third time.

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
  `python3 ~/workspace/devrc/scripts/audit-dispatch.py 35 --round 2` and **read its stderr**
  for the "newest claims block says round=N" line before dispatching.
