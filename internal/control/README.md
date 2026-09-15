# `internal/control` — the control plane's data model and authorization

This is **P3a** of `claudedocs/plan-cairn-control-plane.md` (the model, the durable
authority, and the one predicate that decides what a principal may see) plus **P3b piece
(a)** (the materialized cache, its epoch, and the staleness report). It is a **library
only**. Nothing here is wired into `internal/api`, no route consults it, and the token
file is still what authorises the running server. Replacing the token path is a
behaviour change; this is not one, and keeping those in separate commits is what lets
the conformance corpus stay green across this — measured at each step, not asserted:
`tests/conformance/suite.py run` stays at **0 failures / 0 skipped** and
`tests/conformance/run_go.sh` at **116 PASS / 0 failures / 4 skipped** across P3b(a).

## What the plan left open, and what was settled

`claudedocs/plan-cairn-control-plane.md` §"Deferred decisions, to settle inside P3/P4"
named four. The operator settled all four before any code was written:

| question | answer | why |
|---|---|---|
| verb granularity | **`read` / `write` / `admin`** — three, not four | the store enforces no distinction between appending a bullet and replacing an entry, so an `append` verb would be a word in the grant table that no call site can branch on. The journal is append-only, so a fourth verb later is cheap and a regretted one is not. |
| project as a grant subject / credential principal | **both** | otherwise "share with the team" is one grant per member that silently goes stale as membership changes — the drift the append-only table exists to prevent. `principal_kind` was already in the schema sketch, so it costs a branch in one function. |
| what `project_id NULL` means | **there is no NULL** — every scope is in a project, and signup auto-creates a solo one | a NULL is a branch at every authorization site. Collapsing it means `Resolve` has exactly one resolution path. Costs a migration that mints a project per existing scope owner. |
| where the authority lives | **behind a `Store` interface, file-backed first**; Postgres becomes a second backend at P4 | the hot path never touches the authority — it reads a materialized projection — so the matrix, the grants and the staleness report can all land and be gated with no Postgres in CI, where all four existing gates run today with no external dependency at all. |

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
**bounded** by the schedule, **reported** by `Staleness`, and **bypassable** by
`ApplyNow`.

### The four design calls

| decision | what was chosen | why |
|---|---|---|
| what the bound does | **bounds the REPORT, not the reads** | refusing to serve past `MaxAge` converts an authority outage into a total read outage, at the moment the operator can least fix it. An exceeded bound is LOUD and still serving. The one thing it must never be is silent. |
| who installs the signal handler | **the caller does; the cache receives a channel** | `signal.Notify` is process-global state, and a library that calls it takes away the program's decision to have a handler at all — which on an ordinary process is the difference between a SIGHUP that reloads and one that terminates. `cmd/cairn-server` already owns that call for the token file. |
| what `Effect` is derived from | **the two EPOCHS, never the call site** | a deferred write that a concurrent refresh has already picked up IS in force. Labelling it by its code path would have the UI say "effective within 60s" about something that already happened. The invariant is structural — `EffectImmediate` exactly when `ServingEpoch >= WrittenEpoch` — so a caller can check the label against the numbers beside it. |
| where the staleness lives | **a VALUE with a `String()`, not a log line** | a log line is read by whoever happens to be tailing. A value can be rendered into a status surface, compared in a test, and asserted on. `TestTheStalenessRendersExactly` pins the **whole normalised line** for six states, because a guard on a few words is walkable by rewording. |

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

- **The running server.** Still nothing wired into `internal/api`; `authz.TokenRecord`
  is still what authorises a request. Piece (b).
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
python3 tests/control_mutants.py          # 46 mutants
python3 tests/control_mutants.py --show    # print each edit without running it
```

**Measured on this tree: 46 mutants, 45 killed, 1 labelled EQUIVALENT at the code,
0 misattributed, 0 harness errors, positive control GREEN.**

⚠ **ONE CACHE ROW EXISTS BECAUSE A FIXTURE SAT ON ITS OWN GUARD'S BOUNDARY.**
`effective-by-measured-from-now` replaces `materializedAt + bound` with `now + bound`.
The first draft of `TestAnOrdinaryRevokeIsDeferredAndSaysSo` materialized and then wrote
at the **same** pinned instant, which makes the two expressions identical — the mutant
would have SURVIVED a fully green test that appeared to assert the deadline. The fixture
now moves its clock 20s between the two, and the test says why.

🔴 **IT RUNS IN CI, IN THE `go` JOB, RATHER THAN BEING A NUMBER IN THIS FILE.** ~80s on
one developer host, up from ~30s at 28 mutants: four of the cache rows are killed by a
TIMEOUT rather than by an assertion (a dropped trigger and a stopped loop have no
observable except the refresh that never comes), and that is what the extra minute buys. The
other half of the pair — the matrix's own 32/60 positive control — runs on every CI run
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

## What this package structurally cannot see

- **Anything about the running server.** No route consults it yet. A green suite here
  says nothing about what the pod authorises.
- **Concurrency across processes.** `Append` takes an exclusive `flock` and writes one
  `write(2)` under `O_APPEND`, which is what makes read-validate-write atomic — but
  nothing in the tests runs two processes at the same instant, so that is a reasoned
  property, not a measured one.
- ✅ **Staleness — CLOSED by P3b piece (a), and this bullet is kept rather than deleted
  because a comment is a claim too.** It used to read "nothing yet *reports* it".
  `Cache.Staleness()` is now a renderable value with the epoch, its age, the declared
  bound and whether the bound was exceeded, and `cache.go`'s section above is what it
  was replaced by. ⚠ What is **not** closed is the surface that PRINTS it: nothing in
  `cmd/cairn-server` or `internal/doctor` constructs a `control.Store` yet, so the value
  exists and no deployed program renders it. That is piece (b)'s wiring, and calling
  this row done would be declaring a step early.
- **The token file.** Migrating the existing static credentials into grants is not done,
  and it is the change that has to reconcile the legacy bare token — which is
  unrestricted — with a model that has **no** unrestricted principal by construction.

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

1. ◐ the materialized cache — **the library half is done** (`cache.go`: refresh on
   change, on a timer and on `SIGHUP`, the staleness value, and the synchronous revoke
   path). What is left is the **surface**: nothing constructs a `control.Store` in a
   deployed program, so no `doctor` output and no startup banner carries the epoch yet.
   🔴 Deliberately not done here: adding a field to `cmd/cairn-server`'s startup line
   would change a string `tests/dualrun/harness.py` compares between the two servers,
   and dualrun needs a live pod that this change was not in a position to run. Wire it
   with that gate in front of you, not without it;
2. wiring `Principal` into `internal/api` in place of `authz.TokenRecord`, keeping the
   conformance corpus green;
3. the migration from the token file, which is where the legacy unrestricted row has to
   become explicit grants;
4. immutable scope ids shipped in the snapshot, and the client renaming its local
   directory when it sees a known id at a new name.
