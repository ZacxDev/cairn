# `internal/control` — the control plane's data model and authorization

This is **P3a** of `claudedocs/plan-cairn-control-plane.md` (the model, the durable
authority, and the one predicate that decides what a principal may see), **P3b piece
(a)** (the materialized cache, its epoch, and the staleness report) and **P3b piece (b)**
(the serving path authorises from here, and the token file became an adapter).

🔴 **IT IS NO LONGER A LIBRARY NOBODY CALLS — `internal/api` AUTHORISES FROM IT.** The
sentence here used to read "nothing is wired into `internal/api`, no route consults it,
and the token file is still what authorises the running server"; all three are now false.
`authz.LoadTokens` still parses the deployed secret and every guard in its ladder still
runs, but the table it produces is the INPUT to `internal/control/tokenfile`, which
projects it into a `Model`; `control.Authenticate` over the materialized cache is what
resolves a request.

**The served contract did not move, and that is measured rather than claimed.**
`tests/conformance/suite.py run` stays at **requests=99 / assertions=433 / 0 failures /
0 skipped** and `tests/conformance/run_go.sh` at **116 PASS / assertions=415 / 0 failures
/ 4 skipped**, identical to the numbers at `3c8707c` — the same claim P1b made about the
validation ladder. The corpus replays the identical requests against a server whose
authorization mechanism has been swapped underneath it, and nothing in the served
contract moved.

🔴 **THAT IS NOT WHY THE TOKEN FILE BECAME AN ADAPTER, AND THIS PARAGRAPH USED TO SAY IT
WAS.** It called the corpus "a differential gate over the migration rather than a
regression suite that happens to still pass" — but the corpus cannot see the half the
migration actually changes (see "The token file as a `Source`" below, and the measurement
in the next paragraph). The three reasons that do carry the adapter are recorded there;
what the corpus carries here is exactly the sentence above it and no more.

⚠ **AND THE CORPUS IS BLIND TO THE HALF THAT MATTERS MOST — MEASURED, NOT FEARED.**
Deleting the store-directory half of the adapter's scope enumeration outright leaves the
Go corpus at **116 PASS / 0 failures**, because every scope in the corpus's world is named
by a mapped row, so the union already covers it. The same edit turns **eight** Go guards
red. A green corpus across this change is necessary and is not sufficient; see the
battery and the tokenfile tests for what actually stands on it.

🔴 **THAT NUMBER READ `seven` AND HAS NEVER MEASURED MORE THAN SIX — INCLUDING AT THE
COMMIT THAT WROTE IT.** Re-measured at `cafeb87`, where the sentence was introduced: the
same edit turned **six** guards red there, so the count was an overcount from the start
rather than one that drifted. The guard a reader would expect in the set and would not
find is `TestTheProjectionIsAPureFunctionOfItsInputs`: its fixture gave the one record an
allowlist naming every directory in the store, so the union's store half contributed
nothing to it and deleting `storeDirs()` left it green — a fixture that reads as coverage
of the enumeration while measuring only one half of it. It now names two of the four
directories plus one scope with **no** directory, so five scopes are enumerated and each
half is separately load-bearing: dropping the store half yields three, dropping the
allowlist half yields four, and each is that test failing on its own precondition.
Measured on this tree, deleting the store half turns these eight red —
`TestALegacyRowReachesEveryScopeAndMayWriteNone`,
`TestAScopeCreatedAfterMaterializationIsInvisibleUntilTheNextRefresh`,
`TestAScopeCreatedOutOfBandReachesABareRowAfterARefresh`,
`TestTheAllowlistAsymmetrySurvivesTheProjection`,
`TestTheBinarysOwnTimerIsWhatClosesTheDivergence`,
`TestTheProjectionIsAPureFunctionOfItsInputs`,
`TestTheServedAuthorizationMatrixIsExactlyThis` and
`TestTwoDirectoriesThatFoldTogetherDoNotTakeTheAuthorityDown` — and deleting the
allowlist half turns four red, which is a different set and a different claim.

## What the plan left open, and what was settled

`claudedocs/plan-cairn-control-plane.md` §"Deferred decisions, to settle inside P3/P4"
named **five**. The operator settled the first four before any code was written:

| question | answer | why |
|---|---|---|
| verb granularity | **`read` / `write` / `admin`** — three, not four | the store enforces no distinction between appending a bullet and replacing an entry, so an `append` verb would be a word in the grant table that no call site can branch on. The journal is append-only, so a fourth verb later is cheap and a regretted one is not. |
| project as a grant subject / credential principal | **both** | otherwise "share with the team" is one grant per member that silently goes stale as membership changes — the drift the append-only table exists to prevent. `principal_kind` was already in the schema sketch, so it costs a branch in one function. |
| what `project_id NULL` means | **there is no NULL** — every scope is in a project, and signup auto-creates a solo one | a NULL is a branch at every authorization site. Collapsing it means `Resolve` has exactly one resolution path. Costs a migration that mints a project per existing scope owner. |
| where the authority lives | **behind a `Store` interface, file-backed first**; Postgres becomes a second backend at P4 | the hot path never touches the authority — it reads a materialized projection — so the matrix, the grants and the staleness report can all land and be gated with no Postgres in CI, where all four existing gates run today with no external dependency at all. |

🔴 **AND THE FIFTH IS SETTLED HERE, BECAUSE PIECE (b) WOULD OTHERWISE HAVE SETTLED IT BY
SHIPPING CODE.** *"Whether the legacy bare-token row survives the rewrite or is dropped
at P8."* **It survives.** `internal/control/tokenfile` projects a bare row into a project
principal holding `read` on the scopes project, so the unrestricted credential keeps
working through the new predicate.

The reason is that dropping it is not a code change. The deployed pod's only principals
ARE bare rows — `server/README.md`'s rotation procedure shows the live table as
`token reload: LOADED 2 identities [<new>:legacy,<old>:legacy]` — so retiring the shape
means editing the secret, giving every holder an allowlist and doing it in step with a
serving pod. That is a coordinated cutover against a live deployment, in the one change
whose claim is that the served contract did not move.

⚠ **It is survives-FOR-NOW, and P8 inherits the question**, which is recorded in the plan
doc beside the decision rather than only here. The divergence in "The divergence, measured
rather than reasoned about" below exists only while an unrestricted principal does, so it
is part of the price of this answer; and the retirement must not be done by reintroducing
an unrestricted principal in the model.

🔴 **AND ONE CONSTRAINT THOSE ANSWERS COLLIDE WITH, RECORDED BECAUSE P4 OWNS IT.**
`go.mod` has no `require` block and `flake.nix` passes `vendorHash = null`; together
those make a new dependency in the serving path a build **failure** rather than a silent
addition. Every embedded SQL engine for Go is a third-party dependency, and so is every
Postgres driver. So the file backend is not a placeholder chosen for speed — it is the
only durable authority the stated constraint permits today, and **P4 cannot add a
Postgres backend without deciding what happens to stdlib-only.** That is a decision, not
an oversight; it is written here so whoever reaches P4 meets it before writing code
rather than after.

## The one property everything is built to preserve

A principal's authority is **derived**, never enumerated beside the credential. A
credential binds to a principal; what that principal may see is computed from membership
and grants at the moment it is asked. The alternative — a credential row that also lists
its scopes, which is exactly what the token file does — is two structures holding one
fact, and they drift.

Authority arrives two ways, and **one function computes the union**:

1. **ownership** — a user is a member of a project, so they reach that project's own
   scopes at their role's verbs (`roleVerbs`);
2. **sharing** — a live grant names the principal, or a project they belong to, as its
   subject.

Those are different facts about the world and both must be consulted. One-rule-one-place
requires one *function*, not one *table* — `Resolve`, and nothing else in the codebase
may decide what a principal can see.

## The gate

`TestTheAuthorizationMatrixIsExactlyThis` is a **ledger over the whole relationship**,
not a sample: five principals × four scopes × three verbs, every cell written out, and
the test refuses to run if the model holds a principal or a scope the table does not
name — it fails when the set **grows** as well as when it shrinks.

The fixture is built so that each mechanism is separately observable. No two routes
produce the same row:

| mechanism | its only witness |
|---|---|
| ownership at three roles | alice / bob / carol in `atlas`, whose rows differ only in the admin column |
| a personal cross-project share | carol on `beacon-notes`, read only |
| a project as **subject** | `atlas` on `beacon-secrets` — every atlas member gains it without being named |
| a project as **object** | erin, who is a member of nothing, on all of `beacon` |
| a **union** of two routes | carol on `atlas-notes`: her role gives read+write, a personal grant gives admin, and only the union gives all three |
| a **revoked** grant | bob on `beacon-notes`, whose tombstone is the only reason his row differs from what the grant says |

🔴 **AND IT CARRIES A POSITIVE CONTROL, BECAUSE A MATRIX IN WHICH EVERY CELL REFUSES IS
PERFECTLY SELF-CONSISTENT AND MEASURES NOTHING.** This repository has already paid for
that shape once: `tests/parity/README.md` records the P2 harness reporting 72 PASS /
0 FAIL while the pod refused every request, because two clients failing identically
compare equal. So the allow count is pinned from both sides — **32 of 60 cells allow**,
not 0 and not 60 — and any fixture change moves that number and forces whoever made it
to say what they expected.

## The materialized cache — P3b piece (a)

`cache.go` is the pod-side projection: a `Model`, the **epoch** it was built from, and
the instant it was built. Reads come from it; `Source.Model` is called from `refresh`
and from nowhere else, so a dead or hung authority cannot stop an authorization
decision. That is the plan's §D promise — an offline "orient me" still answers —
and it is bought with one honest cost, stated in the code and repeated here:

🔴 **A REVOCATION IS NOT EFFECTIVE UNTIL THE CACHE REFRESHES.** No arrangement of this
design makes that false, because a cache that asked the authority whether it was stale
would be making exactly the call the outage is supposed to survive. So the lag is
**bounded** by the schedule, **reportABLE** by `Staleness`, and **bypassable** by
`ApplyNow`.

⚠ **"REPORTABLE" IS NOT "REPORTED", AND THIS SENTENCE SAID THE SECOND ONE.** `Cache.Staleness()` has **no caller outside the tests** — not
`cmd/cairn-server`, not `internal/doctor`, not any route — so the lag, and a `degraded` or
`stale` authority with it, is currently **bounded and silent**. The value is built,
rendered and pinned; nothing prints it. Why it is deferred, and the closing condition for
it, are one section down under *What piece (a) structurally cannot see*.

### The five design calls

| decision | what was chosen | why |
|---|---|---|
| how two concurrent publishers order their COMMITS | **a generation stamp — never the epoch** | `src.Model` runs outside the lock on purpose, and the deployed binary now has TWO independent triggers (the timer in `Run`, SIGHUP through `api.Server.SetTokens`), so their reads overlap. Without an order the slow one wins: the operator revokes a row, SIGHUP publishes and commits, the timer — which entered `src.Model` first holding the old table — commits on top, and the revoked credential authenticates again while the status says `fresh`. 🔴 The order **cannot** come from `Model.Epoch`: the epoch is an event COUNT, so a revocation makes it go DOWN (measured, 7 → 5), and an "ignore a lower epoch" guard would reject exactly the smaller, newer world. A discarded attempt is counted as `Superseded` — a third outcome, not a failure. `ApplyNow` takes its generation **after** `Append` rather than before, so a refresh whose read straddles the write cannot overwrite the one path that promises the revocation is in force on return. |
| what the bound does | **bounds the REPORT, not the reads** | refusing to serve past `MaxAge` converts an authority outage into a total read outage, at the moment the operator can least fix it. An exceeded bound is LOUD and still serving. The one thing it must never be is silent. |
| who installs the signal handler | **the caller does; the cache receives a channel** | `signal.Notify` is process-global state, and a library that calls it takes away the program's decision to have a handler at all — which on an ordinary process is the difference between a SIGHUP that reloads and one that terminates. `cmd/cairn-server` already owns that call for the token file. |
| what `Effect` is derived from | **the two EPOCHS, never the call site** | a deferred write that a concurrent refresh has already picked up IS in force. Labelling it by its code path would have the UI say "effective within 60s" about something that already happened. The invariant is structural — `EffectImmediate` exactly when `ServingEpoch >= WrittenEpoch` — so a caller can check the label against the numbers beside it. |
| where the staleness lives | **a VALUE with a `String()`, not a log line** | a log line is read by whoever happens to be tailing. A value can be rendered into a status surface, compared in a test, and asserted on. `TestTheStalenessRendersExactly` pins the **whole normalised line** for SEVEN states, because a guard on a few words is walkable by rewording. ⚠ The seventh exists because six of them read `superseded=0`, which pins the spelling and measures nothing; it forces the interleaving and reads the number. |

⚠ **`Staleness.LastError` IS A FIELD AND IS DELIBERATELY NOT IN `String()`.** The text
comes from the authority — an OS error naming a path, a journal parse failure quoting a
line — and un-authored text on an operator stream can forge a line boundary, which is
what `reloadSafe` in `cmd/cairn-server` exists for. The rendered line carries the
BOOLEAN; the caller that wants the text sanitises it for its own stream. A second
sanitiser here would be a second copy of a predicate that already exists.

### Four states, and why `unmaterialized` is not `stale`

`unmaterialized` · `fresh` · `degraded` · `stale`. The one worth naming: a cache whose
**first** refresh never succeeded has nothing to serve and authorises nobody — that is a
cold start, not an outage, and conflating it with `stale` would produce a cache that
authorises nobody while reporting itself healthy. `bound=none` renders instead of
`bound=0s` for the same reason: `Exceeded` is structurally false when no bound was
declared, and a reader must not be able to mistake that for the bound being met.

### The dependency kill

Measured rather than reasoned about, with a `Store` double that has a power switch —
the real `FileStore` cannot produce this failure, because once it has loaded its own
last-known-good keeps answering:

| claim | how it was measured |
|---|---|
| reads still serve | the authority is unplugged; `Authenticate(carolToken)` still returns carol's matrix row, at the last known-good epoch |
| the age GROWS | two named points, **60s and 300s** after the last good materialization, straddling a 2m bound: `degraded` then `stale` |
| a failed refresh does not reset the age | `MaterializedAt` is pinned across three failed attempts while `LastAttempt` moves — the two facts stay distinguishable |
| writes refuse CLEANLY | `Apply` and `ApplyNow` both return the authority's error with a **zero** `WriteResult`, the cache's epoch and `MaterializedAt` are unmoved, and the double counts **2** attempts — the refusal reaches the authority rather than being guessed |
| a HUNG authority is not a failing one | `Source.Model` blocks forever; a read completes against a 5s deadline. The lock is never held across the Source call, or the cache would reintroduce the outage it exists to survive |
| the loop survives the outage | the authority dies, two attempts fail, the authority returns, and the **loop** is what notices |

### What piece (a) structurally cannot see

- ✅ **The running server — CLOSED by piece (b), and this bullet is kept rather than
  deleted because a comment is a claim too.** It used to read "still nothing wired into
  `internal/api`; `authz.TokenRecord` is still what authorises a request". `internal/api`
  now holds a `control.Cache` over `tokenfile.Source`, `cmd/cairn-server` runs its refresh
  loop, and `TestTheHotPathDoesNotContactTheAuthority` in `internal/api` measures the
  no-network claim from both directions against a REAL server rather than a library
  double. ⚠ What is still not done is the SURFACE that prints `Staleness`: no `doctor`
  output and no startup banner carries the epoch. That is deliberate — the banner is a
  string `tests/dualrun/harness.py` compares between the two servers, so a Go-only field
  there moves a gate in the same change that most needs it. 🔴 **THIS IS THE ONE TRUE
  SENTENCE ABOUT REPORTING IN THIS FILE, AND TWO OTHERS USED TO CONTRADICT IT** (the
  revocation paragraph above, and the divergence section below) by writing **reported**
  where only **reportable** is earned. **CLOSING CONDITION,** so this is checkable rather
  than aspirational: a render — a `doctor` section, a status route, or a banner field
  declared in `wire.NORMALIZATIONS` — that a `tests/dualrun/` run exits 0 with. ⚠ And the
  cost of leaving it open GREW rather than stayed flat: a store root that will not
  enumerate is now a refusal, so `degraded` is a state a healthy-looking pod can sit in,
  and it is exactly the state an operator cannot see.
- **Two processes.** Every trigger is measured in one process. Two pods over one journal
  refresh independently and can serve different epochs at the same instant.
- **A real clock.** Every staleness assertion runs on an injected clock. A stepped or
  descheduled real clock is exactly what `Exceeded` is for and exactly what no test here
  exercises.
- **Whether the three triggers coexist.** Each is measured with the other two DISABLED,
  so `LastTrigger` identifies the mechanism. That they are independent is the `select`'s
  property, reasoned about rather than measured — with one exception that was worth
  measuring: `TestTwoNotifyChannelsBothReceiveOneSIGHUP`, because the plausible belief
  ("the token reload will swallow it") would make this trigger silently dead in the only
  program that has both.
- **Anything about another replica, or about a copy already synced.** `ApplyNow`'s
  promise is about *this process's* cache. Revoking stops future syncs; it does not
  recall the files already on somebody's laptop.

## The mutation battery

```bash
python3 tests/control_mutants.py          # 62 mutants, over FOUR packages
python3 tests/control_mutants.py --show    # print each edit without running it
```

**Measured on this tree: 62 mutants, 61 killed, 1 labelled EQUIVALENT at the code,
0 misattributed, 0 harness errors, positive control GREEN.**

🔴 **IT RUNS OVER FOUR PACKAGES NOW, BECAUSE THE GUARDS SPAN A SEAM.** `internal/control`
is the model and its predicate, `internal/control/tokenfile` is the projection, and
`internal/api` is the server that authorises from it — and a mutant in the projection is
killed by a guard in the server and vice versa. A battery scoped to one package would have
scored every one of those SURVIVED while the suite that catches them was never run.

🔴 **AND `cmd/cairn-server` IS THE FOURTH, BECAUSE THE TIMER THAT BOUNDS THE DIVERGENCE
LIVES ONLY THERE.** Measured before it was added: deleting the refresh goroutine from
`main` — and the two imports it alone needed — left `go build ./...` clean, `go vet ./...`
clean and all thirteen `internal/...` test packages green, with this battery not running
the package at all. The one mechanism bounding a divergence the README declares had no
gate of any kind. `the-refresh-loop-has-no-triggers` is that row, and it empties the
trigger set rather than deleting the block, because the deletion does not COMPILE and a
mutant that dies at the build proves nothing.

⚠ **ONE CACHE ROW EXISTS BECAUSE A FIXTURE SAT ON ITS OWN GUARD'S BOUNDARY.**
`effective-by-measured-from-now` replaces `materializedAt + bound` with `now + bound`.
The first draft of `TestAnOrdinaryRevokeIsDeferredAndSaysSo` materialized and then wrote
at the **same** pinned instant, which makes the two expressions identical — the mutant
would have SURVIVED a fully green test that appeared to assert the deadline. The fixture
now moves its clock 20s between the two, and the test says why.

🔴 **IT RUNS IN CI, IN THE `go` JOB, RATHER THAN BEING A NUMBER IN THIS FILE.** **2m46s
on one developer host, against 2m01s for the same battery at 61 mutants over three
packages** — both measured back to back on that host, which is what makes the ~45s the
fourth package costs a delta rather than an impression. (It costs that much because a
mutant in `internal/api` or `internal/control` forces `cmd/cairn-server` and its test
binary to rebuild. An earlier `~80s` here was measured on a different host and is
superseded rather than contradicted — the two were never comparable.) Four of the cache
rows are killed by a TIMEOUT rather than by an assertion (a dropped trigger and a stopped
loop have no observable except the refresh that never comes), and that is what the bulk
of it buys. The other half of the pair — the matrix's own 32/60 positive control — runs
on every CI run
and is a claim about the *matrix*; this is a claim about each individual *guard*, and a
battery nobody runs bit-rots into patterns that match nothing and score SURVIVED without
ever executing. The harness refuses on an occurrence count that is not exactly what the
row declares, so drift is a loud HARNESS ERROR rather than a quiet false finding.

Attribution is by **which test failed**, not by "something went red": a mutant killed by
the wrong guard proves the suite can fail and proves nothing about the guard it was built
to exercise. A kill by anything other than the row's named test is reported as
MISATTRIBUTED, which is a finding rather than a pass.

The one survivor is `constant-time-compare-becomes-equality` — replacing
`subtle.ConstantTimeCompare` with `==`. String equality and a constant-time compare agree
on every input, so no behavioural test can distinguish them and none should be written to
try; the property at stake is a timing one, defended by the comment beside the call. It
is listed here so a reader finding it SURVIVED does not read that as "the comparison does
not matter".

🔴 **TWO ROWS OF THE BATTERY WERE WRONG IN THEIR FIRST DRAFT, AND THE BATTERY IS WHAT
SAID SO.** Recorded because both are the shape this whole file is about:

- `copyids-flattens-nil` named the narrowing test as its killer. That test calls `Narrow`
  **directly** with literal slices and never reaches `copyIDs` at all — the path that
  does is `apply` storing a credential. A row whose named killer never runs is the
  "reads as coverage while providing none" shape, sitting inside the control built to
  refuse it. It is now attributed to the two authentication tests that actually see it.
- `empty-verb-grant-accepted` and `constant-time-compare-becomes-equality` each failed to
  apply at all — one pattern had drifted, one left an import unused so the tree did not
  build. A mutant that does not compile dies at the build rather than at a guard, which
  is the one outcome that proves nothing; both are now reported as HARNESS ERRORS rather
  than scored.

## One defect this package shipped with, and the guard that now stands on it

The first draft of `FileStore.Append` validated a batch by applying it to `current` — a
`Model` struct copy. A `Model` is six maps behind a struct header, so that "scratch copy"
**was the live cache**: a batch rejected at its third event left the first two
permanently applied to the authority the process was serving, with nothing in the journal
recording them. The served model would then be **wider** than the file it claims to
project, and a restart would silently "lose" grants that were never written.

`Model.clone` is the fix and `TestARejectedBatchLeavesNeitherBytesNorState` is the guard.
It is red at baseline by a one-word change (`current.clone()` → `current`), and the
battery carries that mutation plus the same defect one nesting level down
(`clone-is-shallow`), because `maps.Clone` is one level deep and the two membership
indexes are maps of maps.

## The token file as a `Source` — P3b piece (b)

`internal/control/tokenfile` is the adapter, and it is the answer to the question piece
(b) could not avoid: **wiring `Principal` into the API is not shippable on its own,
because with nothing else the server would have no principals at all.** The two answers
available were a one-time conversion of the deployed secret into a journal, and a
`Source` that reads the same file the pod already reads. Three reasons carry the
adapter:

1. **the deployed secret keeps working unchanged.** The live pod's only principals are
   rows in that file, so a conversion is a coordinated cutover against a running
   deployment; this is a code change.
2. **the rollback is one commit.** Nothing is written — `Source.Events` is read-only and
   no caller appends it to a `FileStore` — so reverting leaves no converted data to
   restore.
3. **the journal format is not settled.** `go.mod` has no `require` block, which blocks
   every Postgres driver and every embedded SQL engine, so P4 owes a decision about
   stdlib-only before there is a durable authority to convert *into*; and piece (d)
   changes what a scope IS, from a directory the adapter enumerates to a `scope-created`
   event. A conversion today writes a format the next phase is about to move.

🔴 **AND THE REASON THIS SECTION GAVE FIRST WAS FALSE.** It read: *"the adapter was
chosen for one reason above the others: it makes the conformance corpus a differential
gate over the migration."* `tests/conformance/world.json` holds four scopes
(`alpha-notes`, `beta-notes`, `hollow-set`, `rubble-heap`) and its `wide-reader`
principal's allowlist names **all four** — so no row in the corpus ever exercises
unrestricted-ness, and the corpus cannot distinguish "unrestricted" from "allowlisted
everything", which is exactly the property the adapter has to reproduce. Measured rather
than argued: deleting the store-directory half of the enumeration leaves `run_go.sh` at
**116 PASS / 0 failures**. What the corpus does gate is that the served contract did not
move for the principals it declares, which is worth having and is a narrower claim. What
gates the unrestricted half is `internal/api/authority_test.go`, whose `gamma-notes`
scope is reachable by the BARE row alone.

⚠ **The fix for this is NOT to add a scope to `world.json` here.** Widening the world
moves the corpus in the one change whose whole claim is that the corpus is exactly
unmoved; it is a FOLLOW-UP, and the number above is what says how much the corpus is
worth in the meantime. 🔴 **Its closing condition is mechanical, so it is checkable
rather than aspirational:** a `world.json` scope that **no mapped row names**, goldens
regenerated against the oracle, and `tests/conformance/run_go.sh` going **RED** when
`tokenfile.storeDirs` is deleted — the same edit that leaves it at 116 PASS today. The
red is the whole point; a green corpus over a widened world would have bought nothing.

What it synthesizes, and why each shape was picked:

| decision | what was chosen | why |
|---|---|---|
| where a row's identity lives | each distinct identity becomes a **project principal** named for it | `Principal.Display` is what the audit line's `identity=` field and every written bullet's ACTOR carry, and `displayOf` returns a project's `Name` verbatim. Making the row a USER instead would have rendered `provider:subject` or forced the identity into `User.Email`, which is a lie a UI would later repeat. A machine credential IS the service-account case the plan names. |
| where the scopes live | one project, `token-file scopes`, holding every scope; principals are **outside** it | membership would confer `roleVerbs` over those scopes, and membership authority never appears in the grant log. Every authority here is an explicit `granted` event instead — the same ruling this file records for a project's own scopes, one layer out. |
| what a MAPPED row gets | `read,write` on each scope it names | the store enforces no distinction between the write verbs, and the token file confers all of them over one allowlist. |
| what a BARE row gets | `read` on the **project**, not on a list of scopes | a project-as-object grant expands against `ScopesIn` at RESOLVE time, so a scope added to the model later is covered without a re-grant. It is the closest thing to "unrestricted" a model with no wildcard can express. |
| how the write refusal is asked | "does this principal hold `write` ANYWHERE" | the server's 403 is about the CREDENTIAL and names no scope, so it must not be a per-scope question — a per-scope 403 would tell an authenticated caller that a scope it cannot reach exists. A principal that may write somewhere and aims at a scope it may not gets the not-found answer instead. Both halves are in the served matrix. |
| a row with NO identity | the whole projection is **refused**, so the pod does not start | `Principal.Display` is what the audit line and every written bullet's ACTOR carry, so a principal with no name is a credential whose use cannot be attributed. ⚠ Unreachable from a token FILE — `ParseTokenRow` gives every row an identity — and reachable from a programmatic `TokenRecord`. It is LOUDER than the old behaviour, which was an empty `identity=` field and a bullet attributed to nobody. |
| where scope names come from | the UNION of the store's directories and the mapped rows' allowlists | neither alone is enough. Directories are the only record of what a bare row may read; allowlists are the only record of a scope with **no directory yet**, which is the first-entry create that is how the store gained every scope it has. |
| a store root that will not ENUMERATE | the whole projection is **refused** | it used to be swallowed, on the true-at-one-instant grounds that every read route answers 503 through such a root anyway. The projection is CACHED, so the empty world it produced was committed, `materializedAt` moved, the status said `fresh`, and it was served **through a root that was readable again** — measured as `200 scope-absent` for a scope that exists on disk. On the deployed shape the principals are bare rows, so `storeDirs` IS the whole enumeration and it is not one scope, it is all of them. Refusing keeps the last-known-good instead, which is what preserves the credential table the old argument was protecting. |
| a COLD start over such a root | `api.New` fails and `cmd/cairn-server` exits **78** | a cache with no last-known-good has nothing to keep, so the only two answers are "serve an enumeration known to be wrong" and "do not come up". Nothing served is lost, and that is checked rather than assumed: every read route answers 503 through that root, and a write cannot land either, because `internal/write` creates a scope directory with `os.Mkdir` and not `MkdirAll`, so it fails on the absent parent. The alternative moves the window to startup rather than removing it, which is exactly where an unmounted volume puts it. ⚠ A divergence from `server/server.py`, which starts and answers 503; declared at `tokenfile.Source.Model` with the same closing condition as the divergence below. |
| two rows sharing one IDENTITY | interchangeable only if they describe the **same authority**; otherwise **refused** | the dedupe is keyed on `authorityKey` — `IsLegacy()` plus the folded, sorted allowlist — which is derived from the same two facts `grantsFor` branches on. Keyed on the identity STRING alone, every row after the first contributed only a credential: a mapped row named `legacy` ahead of a real bare row gave that bare row `write` on one scope and took `read` away on another. ⚠ Unreachable from a token FILE (`authz.LoadTokens` guards 8 and 12) — which is a guard in a DIFFERENT package being the only thing between this and a wrong authority, and therefore a reason to gate it here rather than not to. |

### 🔴 The divergence, measured rather than reasoned about

**A scope directory created out of band is not visible to a bare (legacy) row until the
next refresh.** `store.Unrestricted()` is a sentinel evaluated per request, so today a
directory that appeared one millisecond ago is readable by the next request;
`Authorization.VisibleScopes` never returns that sentinel by construction, so the adapter
enumerates, and an enumeration is a claim about a moment.

Both answers are observed, at the library
(`TestAScopeCreatedAfterMaterializationIsInvisibleUntilTheNextRefresh`) and end-to-end
through the served surface (`TestAScopeCreatedOutOfBandReachesABareRowAfterARefresh`):
the same request against the same disk answers `scope-absent` at the old epoch and
`recalled` after a refresh.

It is **declared rather than closed**, and it is narrower than "a new scope":

- a MAPPED row is unaffected — its allowlist is in the file, so a scope it names is
  covered whether or not the directory exists;
- a scope created THROUGH this server (`PUT` with `If-None-Match: *`) is unaffected, because
  the creating row is mapped and the scope was in the model before the directory was;
- what is left is a directory created out of band — `server/seed.sh`, which seeds through
  `kubectl exec … tar -xf -`.

🔴 **AND THE MITIGATION IS THE TIMER, NOT AN OPERATOR RELOAD — THIS SECTION CLAIMED
OTHERWISE AND `server/README.md` REFUTES IT.** The bullet above ended "which is an
operation an operator already follows with a reload". The documented seeding procedure is
`build-push.sh` → `seed.sh --push` → `port-forward` → `verify-byte-identity.sh`, and it
contains no SIGHUP at all; the only documented SIGHUP is the token-**rotation**
procedure, which is a different operation with a different trigger. So nothing but the
schedule closes this window, which is why the schedule now has a gate of its own rather
than a comment: `cmd/cairn-server`'s `TestTheBinarysOwnTimerIsWhatClosesTheDivergence`
runs the binary, creates a directory behind its back, sends nothing, and requires the
read to start answering.

The window is **bounded** (`api.AuthorityRefreshInterval`, 30s, under
`api.AuthorityMaxAge`, 2m) and **NOT reported** — `Cache.Staleness` renders it as a value
and no deployed program calls that method, so the sentence here used to be wrong in the
reassuring direction. Bounded and silent is the honest pair; the closing condition for the
surface is in *What piece (a) structurally cannot see*. 🔴 **CLOSING CONDITION, stated so it is
checkable rather than aspirational:** it closes when scopes stop being discovered from the
filesystem at all — when a scope exists because a `scope-created` event says so, which is
what piece (d) and P4 build. At that point `tokenfile.storeDirs` has no reason to exist
and the enumeration is complete by construction. It does **not** close by widening the
adapter, and it must **not** be closed by reintroducing an unrestricted principal.

## What this package structurally cannot see

- **A second authority.** Everything above is measured against a `FileStore` or a test
  double or the token-file adapter. No Postgres, no identity provider, no JWT.
- **Concurrency across processes.** `Append` takes an exclusive `flock` and writes one
  `write(2)` under `O_APPEND`, which is what makes read-validate-write atomic — but
  nothing in the tests runs two processes at the same instant, so that is a reasoned
  property, not a measured one.
- ✅ **Staleness — CLOSED by P3b piece (a), and this bullet is kept rather than deleted
  because a comment is a claim too.** It used to read "nothing yet *reports* it".
  `Cache.Staleness()` is now a renderable value with the epoch, its age, the declared
  bound and whether the bound was exceeded, and `cache.go`'s section above is what it
  was replaced by. ⚠ What is **not** closed is the surface that PRINTS it: `internal/api`
  holds the cache and exposes it, and NOTHING calls `Staleness()` outside the tests — not
  `cmd/cairn-server`, not `internal/doctor`, not a route — so the value exists and no
  deployed program renders it. ⚠ **AND THAT WAS PIECE (b)'s WIRING, WHICH LANDED WITHOUT
  IT**: the sentence here read "nothing constructs a `control.Store` yet", which stopped
  being the obstacle the moment `api.Server` built a `Cache`. The obstacle is the one named
  under *What piece (a) structurally cannot see* — the banner is a dualrun-compared string
  — and the closing condition is stated there.
- ✅ **The token file — CLOSED by piece (b), and the bullet is kept because a comment is
  a claim too.** It used to read "migrating the existing static credentials into grants is
  not done". `internal/control/tokenfile` is that reconciliation, and the section above
  records what the legacy unrestricted row cost. ⚠ What is NOT done is writing those
  events to a durable journal: the projection is rebuilt on every refresh and nothing has
  appended it to a `FileStore`. `Source.Events` is exported so that conversion, when it
  happens, is the same function rather than a second description of the same world.

## One question the code answers narrowly, and the caller may want to answer wider

**A project principal has no authority over its own project's scopes.** A project is not
a member of itself, so a service account for `atlas` reaches `atlas-notes` only if
somebody granted it — `TestAProjectCredentialCarriesTheProjectsOwnGrantsAndNoMembersRoles`
pins that.

It is the narrow answer on purpose: the wide one — "a project credential implicitly holds
what the project owns" — is a rule that never appears in the grant log, and "who could see
this, and when" is the whole reason the log exists. The same behaviour is available
visibly, by emitting an explicit self-grant (`subject = project P`, `object = project P`)
when a project is created or when its first service account is issued. That is a **policy
decision for whoever wires this up**, not a library one, and it is recorded here because
the default will otherwise read as an oversight to the first person who creates a CI
credential and finds it cannot write.

## What is left of P3

1. ◐ the materialized cache — **the library half and the WIRING are done** (`cache.go`;
   `internal/api` holds one and `cmd/cairn-server` runs its refresh loop). What is left is
   the **surface**: no `doctor` output and no startup banner carries the epoch.
   🔴 Deliberately not done here: adding a field to `cmd/cairn-server`'s startup line
   would change a string `tests/dualrun/harness.py` compares between the two servers,
   and dualrun needs a live pod that this change was not in a position to run. Wire it
   with that gate in front of you, not without it. The same reasoning is why the authz
   epoch is **not** surfaced as a response header or a route: that is a CONTRACT change,
   and it would regenerate the conformance corpus in the one commit whose whole claim is
   that the corpus did not move. It is its own piece;
2. ✅ wiring `Principal` into `internal/api` in place of `authz.TokenRecord` — **done**,
   with the corpus at exactly its pre-change numbers, and `authz.Authorize`,
   `authz.ErrRejected` and `TokenRecord.VisibleScopes` DELETED rather than left beside the
   new path. A second authenticator over a second spelling of "what may this see" is the
   two-structures-one-fact shape the whole package exists to refuse, and it does not stop
   being that shape because one of the two is only reachable from a test. The three
   properties those tests pinned moved to `internal/control/tokenfile/source_test.go`,
   where the mechanism that now decides them lives;
3. ◐ the migration from the token file — **the projection is done** (the section above);
   what is left is WRITING it, so that the authority is a journal an operator can audit
   rather than a file re-read on every refresh;
4. immutable scope ids shipped in the snapshot, and the client renaming its local
   directory when it sees a known id at a new name.
