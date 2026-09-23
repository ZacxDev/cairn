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

⚠ **ONE HALF OF THAT SILENCE IS NOW BROKEN, AND ONLY ONE — SAY WHICH.** `Run` used to
DISCARD each refresh's error (`_ = c.refresh(…)`), so a failing authority was invisible
until a restart. `RefreshTriggers.OnRefresh` is called after every refresh with that
refresh's own result, nil on success, and `cmd/cairn-server` supplies one for the control
journal that prints a line on each TRANSITION — it broke, it recovered. That reports
**failure**, not **staleness**: a cache refreshing successfully against an authority
nobody has written to is fresh by this signal and could still be `degraded` by
`Staleness`. The deferred surface above is unchanged, and the reporter deliberately does
not call `Staleness()` — see its comment for why, and for the condition that joins them.

### The five design calls

| decision | what was chosen | why |
|---|---|---|
| how two concurrent publishers order their COMMITS | **a generation stamp — never the epoch** | `src.Model` runs outside the lock on purpose, and the deployed binary now has TWO independent triggers (the timer in `Run`, SIGHUP through `api.Server.SetTokens`), so their reads overlap. Without an order the slow one wins: the operator revokes a row, SIGHUP publishes and commits, the timer — which entered `src.Model` first holding the old table — commits on top, and the revoked credential authenticates again while the status says `fresh`. 🔴 The order **cannot** come from `Model.Epoch`: the epoch is an event COUNT, so a revocation makes it go DOWN (measured on the token-file adapter at two points, over a store root that stays readable: deleting one mapped row takes a two-row table 10 → 7 and a four-row table 17 → 13, pinned by `tokenfile.TestARevocationMakesTheEpochGoDOWN`), and an "ignore a lower epoch" guard would reject exactly the smaller, newer world. ⚠ An earlier draft cited "7 → 5 when a store root stopped enumerating" — right arithmetic, unreproducible scenario, since such a root now errors rather than projecting a smaller world, and a shrinking enumeration is not a revocation anyway. A discarded attempt is counted as `Superseded` — a third outcome, not a failure. `ApplyNow` takes its generation **after** `Append` rather than before, so a refresh whose read straddles the write cannot overwrite the one path that promises the revocation is in force on return — and the mirror clause, `mine >= c.committed`, keeps it from committing over a THIRD attempt that started later still (`TestAWriteDoesNotCommitOverAnAttemptThatSTARTEDAfterIt`, which forces that interleaving from inside the injected clock). |
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
  ("the token reload will swallow it") would make a second registration silently dead.
  ⚠ **No program has two registrations today.** `cmd/cairn-server` added one for the
  control-journal cache in P5 and it was removed in the same review: it bought at most one
  refresh interval on a command a human runs by hand, against the rule that program states
  beside its authority timer — *one trigger, one place*. The measurement is kept because it
  is what would be relied on if a second consumer is ever justified; it is not a
  description of the current wiring.
- **Anything about another replica, or about a copy already synced.** `ApplyNow`'s
  promise is about *this process's* cache. Revoking stops future syncs; it does not
  recall the files already on somebody's laptop.

## The mutation battery

```bash
python3 tests/control_mutants.py          # 141 mutants, over SEVEN packages
python3 tests/control_mutants.py --show    # print each edit without running it
```

**Measured on this tree: 141 mutants, 139 killed, 2 labelled EQUIVALENT at the code,
0 misattributed, 0 harness errors, 0 stale extra-killers, positive control GREEN.**

⚠ **AND THE RUN BEFORE THAT ONE REPORTED A HARNESS ERROR, WHICH IS WORTH KEEPING BECAUSE
IT IS THE INSTRUMENT CATCHING A CHANGE NOBODY WOULD HAVE LOOKED FOR.** Closing the `%p`
leak made `Issued.token` a `*string`, and `issued-renders-its-token` — a row written long
before, which puts the field back into the log line as `%s` — then failed `go vet` inside
`go test`: `format %s has arg i.token of wrong type *string`. The tree DID NOT BUILD, so
the row measured nothing rather than exercising its guard, and a mutant that dies at the
build is a false green about the test it names. The row now interpolates `Token()`, which
compiles and is the more plausible edit anyway. Two lessons, both already rules here: a
harness error is not a kill, and every row's anchor and TYPES are part of the tree it is
pinned against.

🔴 **THE THIRD EQUIVALENT LABEL WAS MEASURED FALSE AND IS NOW A KILL, WHICH IS WHY THE
SPLIT MOVED WITHOUT A ROW BEING ADDED.** `ui-share-write-authority-check-removed-in-the-
handler` was labelled EQUIVALENT on the reasoning that the write path checks `Allows` at
two sites, so removing either leaves the other answering 403. An audit round produced the
discriminator the label had missed: a request with NO verb field answers **403** unmutated
and **400** with the handler's site removed, because that check runs BEFORE the form is
validated. The row's retracted reason is kept beside it — an EQUIVALENT label is precisely
what stops anybody writing the test that kills the mutant, so a record of one being wrong
is worth more than a tidy row.

⚠ **RE-DERIVE THESE, DO NOT CARRY THEM FORWARD.** They were current at every commit from
`bcfaa19` to `8fb98d2` and went stale at `ca632e3`, a round that added ten mutants and
edited forty-four lines of this file without re-reading its own headline — so it said
`62 / 61 / 1` over a tree measuring `72 / 70 / 2`, and the file read as if the second
survivor did not exist while that survivor was the round's most important finding.
`python3 tests/control_mutants.py` prints the `SUMMARY` line these are copied from, and
the survivor paragraph below must name exactly the mutants that actually survived.

🔴 **IT RUNS OVER SEVEN PACKAGES NOW, BECAUSE THE GUARDS SPAN A SEAM.** The set, in `PKGS`
order: `internal/control`, `internal/control/tokenfile`, `internal/identity`,
`internal/api`, `cmd/cairn-server`, `internal/ui`, `cmd/cairn-ui`. `internal/control` is the model and its
predicate, `internal/control/tokenfile` is the projection, `internal/identity` is P4's
authenticator and its two new backends, `internal/api` is the server that authorises from
all of them, `cmd/cairn-server` is the program, and `internal/ui` is the BROWSER surface —
the second consumer of the same authority — and a mutant in one is killed by a guard in
another. A battery scoped to one package would have scored every one of those SURVIVED
while the suite that catches them was never run.

🔴 **`internal/ui` IS THE SIXTH, AND IT IS HERE BECAUSE THE ALTERNATIVE WAS MEASURED.** The
share flow arrived with a battery of its OWN — `tests/ui_share_mutants.py`, 234 lines, a
second copy of this harness — and **no gate ran it**: `grep -l ui_share_mutants` over the
whole tree returned the file and one README line, where `control_mutants` is a step in
`.github/workflows/ci.yml` and is pinned by `tests/test_control_mutant_count_is_pinned.py`.
Its own README table meanwhile presented that one
afternoon's reading as a standing property — a kill count in present tense, for a battery
nothing would ever run again. (This very paragraph hit the same sweep: quoting that count
verbatim made `test_control_mutant_count_is_pinned.py` read it as a live claim about THIS
battery and go red. The pin is indifferent to who wrote the number, which is the point.) It also mutated the LIVE working tree where this
harness mutates a `copytree`, so an interrupt left a mutated `internal/ui/` in the
checkout. Its eight rows are the `ui-` prefixed ones here, and the file is deleted. **A
second battery is a second thing to remember to run, and this repository's record is that
nobody does.**

⚠ **THAT LIST IS PINNED TO `PKGS` AS A WHOLE STRING, MEMBERSHIP AND ORDER, NOT AS A COUNT.**
`tests/test_control_mutant_count_is_pinned.py` derives it from the tuple and requires it
here and in `.github/workflows/ci.yml`. Until it did, swapping `./internal/api/` for
`./internal/report/` in `PKGS` — five entries either way — left all six pins GREEN while
this file still named `internal/api` by path and the CI comment still described it in prose
as "the server that authorises from all of them". The CI comment gained the paths in the
same change, because a prose description is not a set anything can compare. Reflow the list
freely; changing a NAME in it without moving `PKGS` is a red test.

🔴 **AND `cmd/cairn-server` IS THE FIFTH, BECAUSE THE TIMER THAT BOUNDS THE DIVERGENCE
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

🔴 **IT RUNS IN CI, IN THE `go` JOB, RATHER THAN BEING A NUMBER IN THIS FILE.** The one
timing figure here is a DELTA measured back to back on a single host and is not a current
runtime: **2m46s at 62 mutants over four packages, against 2m01s for the same battery at
61 mutants over three** — same host, same idle machine, which is what makes the ~45s the
fourth package costs a measurement rather than an impression. ⚠ The battery is 141 mutants
now, so neither number describes what a run takes today, and a run on a loaded box is
several times either. (It costs that much because a
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

The two survivors are `constant-time-compare-becomes-equality` and P4's
`the-proxy-secret-is-compared-with-equality` — replacing `subtle.ConstantTimeCompare` with
string equality, in `control.EqualHash` and in the trusted-header backend's shared-secret
check. String equality and a constant-time compare agree on every input, so no behavioural
test can distinguish them and none should be written to try; the property at stake is a
timing one, defended by the comment beside each call. They are listed here so a reader
finding them SURVIVED does not read that as "the comparison does not matter" — and there
are two rather than one because they are two different secrets at two different call
sites, not one guard counted twice.

🔴 **AND TWO MORE SURVIVED BRIEFLY, BECAUSE A PRODUCTION CHANGE MOVED THE OBSERVABLE OUT
FROM UNDER A GUARD THAT WAS NOT EDITED.** `append-validates-against-the-live-cache` and
`clone-is-shallow` both point at `TestARejectedBatchLeavesNeitherBytesNorState`, which read
the poisoned state through `FileStore.Model`. Deleting `Model`'s cache short-circuit —
correct, and unrelated to those guards — made `Model` re-read the journal, which the
rejected batch never touched, so the test answered correctly whether or not the leak
happened. Measured: **both scored SURVIVED on a fully green `go test ./...`**, and the
suite gave no other signal at all. The observable is now `lastKnownGood()`, the retained
model, which is what a poisoned cache would actually be served from the moment a reload
fails; both mutants die on it with the guard's own message.

⚠ **THE LESSON IS THE ONE THIS BATTERY EXISTS FOR, STATED IN THE DIRECTION THAT IS EASY TO
MISS.** Nothing edited the guard, and nothing edited its mutants. A change three
declarations away silently made the test read through a path where the defect is invisible
— which a green suite cannot distinguish from a guard that works. **When a read path is
changed, re-run the battery, not the suite.**

🔴 **FOUR TIMES IN ONE SLICE, WHICH MAKES IT A CLASS RATHER THAN AN INCIDENT — AND ONLY
THE FIRST TWO HAD A MUTANT TO REPORT THEM.** The two above were caught because a battery row
pointed at the affected test. The third was not: `provision_test.go`'s sibling
`TestARefusedProvisioningLeavesNeitherBytesNorState` read its state arm through
`FileStore.Model` too, and — being one layer up, with no row of its own — simply passed
while asserting nothing about state. It now reads `lastKnownGood()` and is listed as an
`extra_killers` on both rows.

🔴 **AND THAT SENTENCE WAS ITSELF THE DEFECT IT DESCRIBES, FOR SEVERAL ROUNDS.** `extra_killers`
was a field set on 23 rows (30 entries) and **read by nothing**: the verdict logic required only
`m.killer in failing` and tolerated any other failing test regardless. So "is listed as an
`extra_killers` on both rows" was offered here as the closure for a silently-emptied guard while
being no gate at all — coverage claimed, none provided, inside the battery built to refuse exactly
that. It is a real ledger now: every listed entry must be in that mutant's failing set, and a row
whose list has gone stale is a **red** battery (`stale-extras=N` in the SUMMARY line) rather than a
quieter kill. The remedy is per row and is a decision — either the guard moved out from under the
mutant's observable, or the row's list was aspirational — and the failure message says so rather
than inviting the entry to be deleted. **And the fourth is in a different package entirely:**
`internal/identity`'s `TestTheEnvironmentLedgersNameEveryVariableEachBackendReads` loops
every ledger variable set alone and asserts each reaches a refusal. Putting
`ErrSessionBackendWithoutAuthority` **before** the constructors moved that observable —
measured: with a nil session authority, **15 of 15** variables refused via the new
sentinel and **0** via their own ledger; with an authority supplied, **0** and **15**. The
loop now supplies one and asserts the refusal's **provenance** rather than its existence.
**The tell in all four is the same: a production change to a READ PATH, and a test nobody
edited.** Ask what a test's observable is, not whether it is green.

🔴 **THERE WAS BRIEFLY A SECOND SURVIVOR, AND ITS LABEL WAS FALSE.**
`the-write-ignores-a-newer-commit` — deleting the `mine >= c.committed` clause from
`Cache.write` — was labelled EQUIVALENT on the grounds that reaching it needed a window
"between two adjacent statements that no gate can open from outside". The statements are
not adjacent: `now := c.clock()` sits between `c.begin()` and `c.mu.Lock()`, and `clock`
is a caller-injected hook (`CacheOptions.Now`). Parking a whole refresh inside that hook —
the same technique `TestAConcurrentRefreshCannotUNDOApplyNow` uses on `Append`, one hook
over — kills it: serving epoch **23** against the written **24** with `superseded=1` at
HEAD, **24 / 24 / 0** with the mutant. `TestAWriteDoesNotCommitOverAnAttemptThatSTARTEDAfterIt`
is the killer and the label is gone. **A label that reads as coverage while providing none
is worse than no label** — it forecloses the test that would close the gap, and this one
was sitting inside the battery built to refuse exactly that shape.

### What P4's twenty-one rows found

🔴 **THREE EXISTING ROWS WENT STALE BECAUSE P4 MOVED THE LINES THEY NAME, AND THE BATTERY
IS WHAT SAID SO RATHER THAN A REVIEWER.** `read-set-uses-the-write-verb`,
`the-audit-identity-becomes-an-opaque-id` and `the-hot-path-refreshes` all matched text in
`internal/api/server.go` that the identity seam re-spelled — the first two because the
assignments now read the `identity.Identity` the authenticator returned, the third because
the authority call moved into `identity.MachineToken`. Each was reported as a **HARNESS
ERROR**, not as a SURVIVED row, which is the whole point of the occurrence-count assertion:
a drifted pattern that scored SURVIVED would have read as a coverage gap and sent the next
reader hunting a guard that is fine. `the-hot-path-refreshes` could not simply follow its
line, because `identity.TokenAuthority` deliberately offers only `Authenticate` and cannot
refresh; it now mutates the server's own call into the authenticator, where the cache is
still in scope.

🔴 **AND TWO NEW ROWS DID NOT COMPILE IN THEIR FIRST DRAFT — THE ONE OUTCOME THAT PROVES
NOTHING.** Removing `subtle.ConstantTimeCompare` left `crypto/subtle` unused; renaming
`envBool`'s `default:` arm to a case nothing matches left a function that returns nothing.
Both now keep every identifier referenced, which is the same lesson
`the-refresh-loop-has-no-triggers` records one section up.

⚠ **TWO MORE FIXTURES SAT EXACTLY ON THEIR OWN GUARD'S BOUNDARY, AND BOTH SURVIVED A
FULLY GREEN TEST.** `exp-is-not-required` survived because a zero `numericDate` renders as
1970, so the expiry COMPARISON refuses a token with no `exp` anyway and "it was refused"
could not tell the two apart — the test now reads the message and requires it to name the
absent claim. `a-failed-fetch-resets-the-reported-age` survived because the key-set fixture
used a FIXED clock, making `now` equal to the instant of the last successful fetch, so
moving `fetchedAt` on a failure changed nothing observable — the fixture now steps a minute
per reading. Both are the shape this file already records for
`effective-by-measured-from-now`, found twice more in one round.

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
| a store root that will not ENUMERATE | the whole projection is **refused** | it used to be swallowed, on the true-at-one-instant grounds that every read route answers 503 through such a root anyway. The projection is CACHED, so the empty world it produced was committed, `materializedAt` moved, the status said `fresh`, and it was served **through a root that was readable again** — measured as `200 scope-absent` for a scope that exists on disk. On the deployed shape the principals are bare rows, so `storeDirs` IS the whole enumeration and it is not one scope, it is all of them. Refusing keeps the last-known-good instead, which is what preserves the credential table the old argument was protecting. 🔴 **AND THE SAME MECHANISM PRESERVES A CREDENTIAL THE OPERATOR IS DELETING** — measured at both ends over one probe: at `fb7e788` a SIGHUP reload through a renamed-away root SUCCEEDS and the revoked row stops authenticating; at `ca632e3` the reload REFUSES and the revoked row still authenticates, across that reload and a second attempt inside the same outage, until the root is readable again. The window is the OUTAGE, not the 30s refresh interval, and SIGHUP is the only revocation path the pod has — `Source` has no write half, so `ApplyNow` returns `ErrAuthorityReadOnly` here. It is not silent: the refused-reload line names the fingerprints still serving and says the authority is unchanged. Declared at `tokenfile.Source.Model`. |
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

## The user-creation path — P5's first slice

`ProvisionUser` (`provision.go`) is the first thing in this repository that WRITES to a
journal-backed authority. One batch: `user-created`, `project-created`, `member-set` at
`RoleOwner`, and one `scope-created` per requested scope. `cairn-server -create-user` is
its only caller.

🔴 **IT CLOSES A DEFECT THAT WAS MEASURED, NOT ANTICIPATED.** P4 shipped three identity
backends and a durable `Store`, and nothing that could put a user into one: enumerated at
`229c142` over every non-test `.go` file under `cmd/` and `internal/`, `OpenFileStore` had
no caller but its own definition (positive control: the same sweep hits in
`filestore_test.go` and `cache_test.go`). So the only authority any binary wired was the
token-file projection, whose single synthetic user sits at provider `cairn-token-file`,
holds no membership, and is the subject of no grant. Measured live at that commit: a
trusted-header backend aimed at exactly that pair **authenticated** — `Identity.Valid()`
true, principal `user:usr_… (cairn-token-file:operator)` — with **zero** readable scopes.
Both session backends were inert in every deployment that could exist.

**Three rules the path carries, each with its own reason:**

1. **Membership is what confers authority, and the batch must contain it.** `Resolve`
   reads memberships and grants; `Project.OwnerUserID` is a record of who made the
   project and is not consulted. A batch with the user and the project and no `member-set`
   passes every structural check and resolves to an empty authorization — the exact state
   above. `membership-omitted-from-the-provisioning-batch` is the mutant.
2. **One user per (provider, subject) pair**, enforced in `apply` rather than at this
   path, because the rule has to hold for every writer. `UserByProviderSubject` returns
   the first match over a Go map, so a duplicated pair makes "who is this session" answer
   a different user id — with a different authorization — on different requests in one
   process, with nothing erroring.
3. **A scope display name must be free across the WHOLE journal, compared by its FOLDED
   form**, which is wider than `apply`'s within-a-project rule and is a guard at this path
   only. Authorization is keyed on ids; the READER is not — `VisibleScopes` hands out
   names and the store root's directories are narrowed by them, so two scope records
   sharing a display name resolve to one directory and each project's members read the
   other's entries.

   🔴 **AND "SHARING A DISPLAY NAME" MEANS `store.NormalizeRef`-EQUAL, NOT
   STRING-EQUAL — THE FIRST VERSION OF THIS GUARD COMPARED RAW AND WAS DEFEATED BY A
   CAPITAL LETTER.** That fold is what decides the directory: it is applied on both sides
   of `store.ScopeSet.Allows` and again on the write path, where `createEntry` folds the
   scope before resolving a filename. So `Quarry_Notes` and `quarry-notes` are ONE
   directory to every reader and every writer. Measured at `18df63d`: two `-create-user`
   runs in different projects, both accepted, both owners at `RoleOwner`, and the second
   read **and wrote** the first's entries — with no undo, because nothing emits
   `scope-renamed` and the journal is append-only. `scope-name-collision-by-FOLD-accepted`
   is the mutant, and it is a *second* row rather than a widening of
   `scope-name-collision-across-projects-accepted`: that one deletes the guard's operand,
   so a guard that is present and too NARROW survives it.

   ⚠ **WHAT IT DOES NOT SEE, AND ONE ITEM IS LIVE IN EVERY DEPLOYMENT.** It iterates the
   JOURNAL's scopes. The machine-token world's scopes are the **store root's
   subdirectories** (`tokenfile.Source.storeDirs`), and both worlds are narrowed against
   the one store root — so on first use, with an empty journal, this guard passes
   unconditionally while the store root may already hold every existing tenant's
   directory. `internal/control` holds no path and must not grow one;
   `cmd/cairn-server`'s `warnScopesThatAlreadyExistOnDisk` is where that is seen, and it
   **warns** rather than refuses because an existing directory is equally the hazard and
   the ordinary sequence (seed the store, then provision its owner) and nothing on disk
   distinguishes them. The other three blind spots — `scope-renamed`, `scope-moved`, a
   hand-edited journal — reach the same collision without passing through this function,
   and a fourth is the read of `s.Model(ctx)` happening **outside** the `flock` `Append`
   takes, so two concurrent `-create-user` runs can both pass it. It closes when a scope's
   bytes are addressed by id rather than by display name.

   🔴 **THERE WAS A SIXTH AND THIS ENUMERATION — WRITTEN UNDER THE HEADING THAT A GUARD'S
   DESCRIPTION HAS TO BE AS WIDE AS ITS BODY — OMITTED IT: THE REQUEST ITSELF.** The guard
   compared each requested name against `m.Scopes` and against nothing else, so two names
   inside ONE batch met only `apply`'s within-a-project rule, which is a **raw** `==`
   (`Model.ScopeByNameIn`). Measured at `8c06ea1`:
   `-create-user -project quarry -scopes "Quarry_Notes,quarry-notes"` was **accepted** —
   two `scope-created` events, two scope ids, one directory, append-only, no undo. It is
   closed: `checkScopeNamesAreFree` now folds within the request as well as against the
   journal, `TestTwoScopeNamesInONEREQUESTThatFoldAlikeAreRefused` is the guard and
   `within-ONE-request-a-FOLDED-duplicate-accepted` is the mutant. Its blast radius was
   bounded — both records land in one project under one owner — and it would have become
   an authorization defect the moment scope **sharing** lands, because a grant names a
   scope **id** and granting one of a folded pair hands over the other's bytes while every
   id-keyed check agrees the grant was honoured exactly.

   ⚠ **AND CLOSING IT LEAVES A RESIDUAL, DECLARED RATHER THAN NORMALISED AWAY.** The fold
   is in `checkScopeNamesAreFree`, not in `apply` — `Model.ScopeByNameIn` stays raw,
   deliberately: it is a RESOLVER as well as a collision check (folding it would make
   `ScopeByNameIn(p, "Quarry_Notes")` return the scope named `quarry-notes`, a second
   silent name-resolution mechanism inside the model), it would put the reader's fold into
   a model that must know nothing about the reader, and it would silently widen
   `scope-renamed` and `scope-moved`, which nothing emits and no test pins. So a
   `scope-created` appended by a writer that does not come through `ProvisionUser` is
   still checked raw. `TestApplyRefusesTwoScopesWithONENameInONEProject` asserts both
   halves of that — the raw refusal and the folded acceptance — so the residual cannot
   drift silently.

⚠ **AND `FileStore.Model` RE-READS THE JOURNAL ON EVERY CALL, BECAUSE THE WRITER IS A
DIFFERENT PROCESS.** It used to serve a process-local projection invalidated only by that
value's own appends — correct for a single owner and wrong for this shape, where
`kubectl exec … cairn-server -create-user` writes and the server reads. A pod reading
through the short-circuit would materialize once at startup and never see a provisioned
user: the journal correct, the command successful, the sign-in still refused.

🔴 **THE FIX WAS TO DELETE THE SHORT-CIRCUIT, NOT TO ROUTE AROUND IT.** The first version
added a one-line `ReloadingSource` wrapping the same `*FileStore` so that `Reload` could
satisfy `Source`; the pod took the wrapper and every other caller took `Model`. That is two
spellings of one read path with the unsafe one as the default, and it is the shape that
regenerates the same bug at the next call site. `ReloadingSource` is gone; `Model` **is**
`Reload`. The `cached`/`loaded` fields stay because `lastKnownGood` reads them, which is
what keeps an unreadable journal degrading to a STALE authority rather than an empty one
(`TestAnUnreadableJournalLeavesTheFileStoreServingLastKnownGood`).

⚠ **AND THE SHORT-CIRCUIT SAVED NOTHING, MEASURED RATHER THAN ARGUED.** Its only
beneficiary was a caller that owns the file and reads it more than once, and the only such
caller is `ProvisionUser`, which reads once and appends once. Counted with
`strace -e trace=openat` over a real `cairn-server -create-user`, at `e11c3a7` and again
after the deletion: **4 opens of the journal either way** — the `OpenFileStore` create,
the `Model` read, the `O_APPEND` write, and `Append`'s own re-read under the `flock`. The
branch was never taken, because the `FileStore` value is fresh when `ProvisionUser` reaches
it.

⚠ **AND THE POD'S CACHE HAS ONE TRIGGER: THE TIMER.** A SIGHUP channel was registered for
it and removed — see the trigger note above.

## The credential-issuing path — and the sentence it made false

`IssueCredential` (`credential_issue.go`) is the second thing in this repository that
WRITES to a journal-backed authority, and `cairn-server -issue-credential` is its only
caller. One event: `credential-issued`, carrying `HashToken(token)` and never the token.

🔴 **IT CLOSES A GAP THAT WAS MEASURED AT THE OTHER END OF THE TREE, AND THE OBSERVABLE WAS
A REFUSAL TO START.** P3 shipped `Authenticate`, `Credential`, `Narrow` and the
digest-collision rule; P5's first slice shipped `ProvisionUser`. Nothing wrote a
credential. So `cmd/cairn-ui -control-journal <file>` — whose startup guard asks the
sign-in precondition itself, "is there a live, attributable credential" — refused to start
against a journal `-create-user` had just written, and its own error text said so:
*"no tool in this repository writes a credential into a journal yet. A journal-backed
cairn-ui cannot be made sign-in-capable BY ANY TOOL IN THIS REPOSITORY."* The browser
surface's entire write half was unreachable by any path here. **That sentence is now false
and the refusal names the command instead**; the guard is unchanged, because
`-create-user` still mints a user and no credential.

**Four rules this path carries, each with its own reason:**

1. **The width is not chosen here.** `TokenEntropyBytes` is 32, which renders as 43
   base64url characters, because `authz.MinTokenChars` is 43 and the pod refuses to START
   on a shorter token-file row. A narrower mint would be a credential this repository's own
   programs reject as guessable, issued by the tool whose job is to produce a working one.
   🔴 **Nothing in the production graph makes those two numbers agree** — `internal/control`
   holds no configuration and must not import the token-file parser — so the agreement is a
   SEAM GUARD, `TestTheMintedWidthAgreesWithTheTokenFileFloor`, which imports both.
2. **The raw token leaves through `Issued.Token()` and nothing else.** The field is
   unexported, `Issued` implements `fmt.Formatter`, and the field is a POINTER. ⚠ That is
   a property of FORMATTING, not a confidentiality boundary: `Token()` still returns the
   secret (which is the point — the command emits it once), an encoder that skips
   unexported fields DROPS it rather than leaking it, and a debugger reads it anyway.
   `Issued`'s own comment enumerates the five things it does not cover.

   🔴 **THE FIRST VERSION OF THIS RULE WAS `Stringer`-ONLY AND WAS 14 OF 22 VERBS SHORT —
   MEASURED, NOT ARGUED.** It read "`String`/`GoString` redact, so `%v`, `%+v`, `%s`, `%q`
   and `%#v` cannot reach it", which is true, and is a list of the verbs `fmt` routes
   through those two interfaces. `fmt` REFLECTS the operand for every other verb, and a
   reflected struct prints its unexported field's value inside `%!d(string=…)`: over
   `Issued` and `*Issued` at `0fb61d4`, **`%d %b %o %O %c %U %e %E %f %F %g %G %t %p` all
   rendered the raw token**. `TestNoRenderingOfIssuedContainsTheToken` ranged over exactly
   the five verbs that already worked while its own docstring claimed "across every verb
   that can reach a struct" — a guard narrower than its description, which is the class
   this repository keeps closing. It now sweeps all 22 in four spellings (raw, hex, HEX,
   decimal bytes), over the value AND a pointer, with an unredacted twin as its positive
   control.

   🔴 **`Formatter` CLOSES 21 OF 22 AND THE POINTER CLOSES THE TWENTY-SECOND.** `fmt`
   consults a `Formatter` before `Stringer` and for every verb — except `%T` and `%p`,
   which it answers before any formatting interface. `%p` of a NON-pointer operand falls
   into `badVerb`, which sets `erroring` and then re-renders the operand as `%v`, and
   method dispatch returns early while `erroring` — so the struct is reflected and the
   field printed. Measured on this type: `Format` alone leaves **1 of 22** leaking; with
   `token *string` it is **0 of 22**, because `fmt` renders a pointer FIELD as an address
   and follows a pointer only at depth 0. ⚠ A `Format` method on the FIELD's type would
   have done nothing: `fmt` calls a value's formatting methods only when it can
   `Interface()` it, and a field reached through an unexported name cannot be — the same
   mechanism that produced the leak. ⚠ And `go vet` flags `fmt.Sprintf("%d", issued)` with
   a CONSTANT format string and says nothing about the same call with the format in a
   variable, which is why the type has to defend itself rather than rely on the linter.
3. **The principal is checked before the mint, and what it buys is the refusal's first
   line.** `apply` refuses the same batch under the `flock`, so as a correctness check this
   is redundant and is not claimed otherwise. `ErrNoSuchPrincipal` is a sentinel
   `cmd/cairn-server -issue-credential` BRANCHES on, printing "this control journal holds no
   user with id …" instead of relaying a sentence about a batch that would not replay.
   🔴 **TWO STRONGER JUSTIFICATIONS STOOD HERE AND ARE RETRACTED.** "Through `Append` a bad
   subject and a failed `write(2)` are one opaque `fmt.Errorf`" is FALSE — they are plainly
   different text (`… subject user usr_x does not exist` against `control journal append:
   <errno>`); what they were not is different by TYPE, which is what a caller needs to
   branch. And "a request that cannot succeed never causes a secret to EXIST" is true and
   WEAK: a token that never reached the journal authenticates to nothing. ⚠ A sentinel
   nothing branches on is not a guard — this one had exactly one `errors.Is` consumer
   tree-wide (its own test) until the command's branch landed.
4. **The token is emitted once, and `-token-out <path>` is the sink that is not
   world-readable.** The command used to print `… -issue-credential … > token` as its
   remedy; a shell redirection creates its file at the umask, so at the default 022 that is
   a **0644** file holding a bearer credential nothing here can revoke — beside a journal
   that is 0600 by construction. `-token-out` creates the file itself with `O_CREATE|O_EXCL`
   at 0600 (`O_EXCL` is what makes the mode a claim: `OpenFile`'s perm applies only to a
   file it CREATES, and it refuses to follow a symlink), it is opened BEFORE the mint so a
   bad path cannot leave a credential whose secret nobody saw, and `-token-out -` is stdout
   explicitly.

🔴 **AND THE DIGEST FIELD'S GUARD WAS WIDENED IN THE SAME CHANGE, BECAUSE A LENGTH CHECK
WAS NOT THE GUARD IT READ AS.** `Event.validate` required `len(e.TokenHash) == HashHexLen`
and its message said a short hash "is the shape a raw token takes" — true of the case it
caught and false of the case that matters: **`base64.RawURLEncoding` of 48 random bytes is
exactly 64 characters**, and 48 bytes is an ordinary width for a machine-minted token, so a
raw secret had a natural spelling that cleared the check and was persisted verbatim into
the append-only authority. It now requires a 64-character hex digest.

🔴 **AND THE FIRST WIDENING WENT TOO FAR IN THE OTHER DIRECTION — IT REQUIRED *LOWERCASE*,
AND THAT IS A RETRACTED CLAIM RATHER THAN A TIGHTENING WORTH KEEPING.** Case does not
discriminate the hazard: a raw base64url token is excluded by `-`, `_` and every letter
from `g` to `z`, never by case, so requiring lowercase bought **zero** additional coverage.
What it cost was a retroactive refusal *on replay* — `Event.validate` runs through
`Model.apply`, which fails a journal **whole**, so one hand-written uppercase digest loads
**zero** credentials and `FileStore.Reload` falls back to `lastKnownGood()`, empty on a cold
start. The population holding such a row is exactly the one an older `cairn-ui` refusal text
created by prescribing a hand-appended record, and `Get-FileHash` / `certutil -hashfile`
emit uppercase. So the check takes either case and **`Model.apply` lowercases what it
stores** — two reasons, both load-bearing: the duplicate-digest refusal is a string compare
(without it, one secret in two spellings is two accepted rows), and `EqualHash` is
byte-exact against a lowercase `HashToken`, so normalising turns a record that replayed
clean and authenticated **nobody** into one that works.
`TestAnUppercaseDigestReplaysAndTheCredentialItNamesAuthenticates` and
`TestOneSecretInTwoSpellingsIsStillRefusedAsADuplicate` are the regression tests, each
measured red against both halves separately; the mutants are
`digest-shape-check-refuses-UPPERCASE-hex` and
`credential-digest-not-normalised-on-replay`.

`TestA64CharacterRawTokenIsRefusedAsADigest` is the regression test for the length-only
guard — **red at `origin/main`'s `journal.go`, green at HEAD** — and the pre-existing table test
`TestTheJournalRefusesWhatItCannotEnforce` stays GREEN under that revert, which is the
measurement saying it could never see this case (its fixture is 26 characters).
`token-hash-checked-by-LENGTH-only` is the mutant, and it is a *second* row rather than a
widening of `raw-token-accepted-as-a-digest`: that one deletes the guard's operand, so a
guard that is present and too NARROW survives it.

🔴 **AND BOTH OF THOSE CORRECTIONS WERE STILL AN OUTAGE ON UPGRADE, WHICH IS WHAT THE
REPLAY EXEMPTION CLOSES.** The two fixes above narrowed what `validate` and `apply` accept,
and both checks run on REPLAY — so a journal an older build had already written met a newer
build's refusal and the whole file stopped loading. Measured at `0fb61d4`, each against a
journal that also held a perfectly good credential:

| the journal holds | credentials loaded at `0fb61d4` |
|---|---|
| a 64-character **non-hex** `token_hash` (what the pre-widening length-only check accepted) | **0** — `token_hash is not a 64-character hex digest` |
| one secret recorded in **two case spellings** (what the normalisation fix made visible) | **0** — `crd_dup1 carries the same token digest as crd_dup0` |

In both cases `FileStore.Reload` then serves `lastKnownGood()`, empty on a cold start: the
operator loses their entire control-plane authority on upgrade, and the only remedy is
hand-editing an append-only file. The second case was *introduced by the previous round's
own fix*, which is the tell that this is a shape rather than two accidents.

🔴 **SO REPLAY AND APPEND ARE SPLIT, AND THE EXEMPTION IS A TABLE RATHER THAN A CONDITION.**
`replayDroppable` (`journal.go`) maps an event KIND to the failures a replay may drop that
record for, and it has exactly one entry: `credential-issued`, for `ErrUnusableTokenDigest`
and `ErrDuplicateTokenDigest`. `Replay` looks the kind up; a kind with no entry cannot be
dropped whatever error it raises, so a revocation or a kind from a newer build still fails
the journal whole. **`Append` never consults it** — it validates a batch by calling `apply`
on a clone directly — so a writer trying to CREATE either record is refused exactly as
before.

🔴 **THE JUSTIFICATION IS DIRECTIONAL, AND THAT IS WHY IT DOES NOT CONTRADICT THE
`default:` ARM THREE PARAGRAPHS OF THIS FILE REST ON.** That arm refuses an unknown kind
whole *"because a dropped revocation is a grant that keeps working"* — reasoning about the
WIDENING direction. Dropping a `credential-issued` record NARROWS: one fewer credential.
And each of the two exempted failures is provably inert on its own terms — a `token_hash`
that is not lowercase-or-uppercase hex is byte-unequal to every value `HashToken` can emit,
so it authenticates nobody; a duplicate keeps working as the record it was FIRST written
as, and what is removed is the two-principals-one-digest ambiguity `Resolve` would otherwise
settle by map order. So the exemption is exactly `credential-issued`, exactly in the
narrowing direction, and never a revocation or an unknown kind.

🔴 **A DROPPED RECORD IS DATA ON THE MODEL, BECAUSE THIS PACKAGE HOLDS NO LOGGER.**
`Model.Dropped` carries a `DroppedRecord` per skipped line — position, kind, credential id
and the refusal's own text — and `cmd/cairn-server` (at pod startup and on
`-issue-credential`) and `cmd/cairn-ui` (at startup) render them on stderr. Without that the
leniency would be a silent narrowing of an authority, which is the one thing that would make
it worse than the outage it replaces. The renderer never echoes a `token_hash`: the refusal
texts are written not to, because the field may be holding a live secret.

⚠ **ONE INTERACTION IS ANSWERED WITH A MESSAGE RATHER THAN A SECOND EXEMPTION.** A journal
that hand-appends an unusable `credential-issued` record AND a `credential-revoked` for it
now has the issue dropped, so the revocation names a credential the model does not hold and
the file is refused whole — pointing at the line the operator got right. `dropHint` adds a
sentence naming the dropped issue and its position. Widening `replayDroppable` to cover that
revocation was considered and refused: it is the "never a revocation" rule being
reinterpreted by whoever hits the case next.

The guards: `TestAnUnusableDigestDropsOnlyItsOwnRecord` and
`TestOneSecretRecordedTwiceDropsTheLaterRecord` (regression coverage, both measured red at
`0fb61d4` — 0 credentials loaded); `TestOnlyCredentialIssuedMayBeDroppedAtReplay` (the
ledger over `AllEventKinds`) and `TestAKindWithNoDroppableEntryStillRefusesTheWholeJournal`
(its behavioural half — a structural assertion about a map type-checks past a `Replay` that
ignores it); `TestARevocationOfADroppedCredentialRefusesTheFileAndSaysWhy`; and
`TestAppendStillRefusesWhatReplayWouldDrop`, which is the claim that the write path did not
inherit any of it. The mutants are
`replay-refuses-the-whole-file-on-an-unusable-digest`,
`replay-refuses-the-whole-file-on-a-duplicate-digest`, `replay-may-drop-a-revocation`,
`replay-drops-every-failed-event`, `a-dropped-record-does-not-name-its-credential`,
`append-inherits-the-replay-exemption` and
`dropped-records-are-not-rendered-to-the-operator`.

⚠ **WHAT THIS PATH DOES NOT DO, ENUMERATED RATHER THAN GESTURED AT.** It does not GRANT —
a credential carries its principal's authority, computed at the moment it is asked, so one
issued to a principal that can reach nothing authenticates and sees nothing (which is why
`cairn-server -issue-credential` asks `Authenticate` with the token it just minted and says
what it reaches). It does not REVOKE: `EventCredentialRevoked` still has no writer in this
repository, so a rotation is "issue, then hand-append the revocation" — the same shape as
the gap this path closed, one event over. And it does not RE-ISSUE: there is no recovery
from a lost token, by construction, because the journal holds only the digest.

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
  ⚠ P5's first slice did NOT change this. It writes a journal for SESSIONS, beside the
  token-file projection rather than over it, and the machine-token backend still
  authorises from the projection — so the two authorities coexist and the migration of the
  existing credentials is still owed.

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
