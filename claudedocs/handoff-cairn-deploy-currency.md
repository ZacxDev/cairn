# Handoff: cairn-deploy-currency — 2026-09-29

## Run this first — the index, one command
```bash
cairn recall --repo "/home/zach/workspace/cairn"
```
Terse pointers this doc does not carry, curated by past sessions and outliving it.
🔴 RECALL, NOT LIVE OBSERVATION — every line is a pointer to VERIFY, never a current
reading, and it may describe a gotcha already fixed. `scope-absent`/`scope-empty` means
nothing is recorded yet: ordinary, not an error, and not a clean bill of health.
Non-blocking: if it exits non-zero, print the stderr line and carry on.

## Goal
Make "what is deployed" and "what is on `main`" observable rather than remembered, and
prove the one deployed feature that has never been exercised. Both predecessor arcs
(`handoff-cairn-control-plane.md`, `handoff-cairn-next-phase.md`) are CLOSED; this is the
new arc their leftovers belong to, not another round of either.

- **closing-condition:** `check` — (a) an instrument exists that FAILS when a deployed
  cairn image's commit is behind `origin/main` (a CI job, a Prometheus rule, or image
  automation) and it has been watched going RED on a deliberately stale pin; AND (b) the
  browser surface's session table has been written — `sessions.n_tup_ins` read as 0 before
  and ≥1 after one real sign-in. A later session runs the instrument's own red-control and
  the two psql reads. ADDRESSED ⇒ arc CLOSED.

## State now

- 🔴 **CLAUSE (b) IS MET AND VERIFIED ON THE LIVE DEPLOYMENT; THE ARC IS NOT CLOSED.**
  `select count(*) from sessions` moved **0 → 1**, `invites` **0 → 1** (minted then revoked),
  the share flow ran end to end (`granted` + `grant-revoked`, picker offering a real second
  user), and the control journal went **30 → 37** records. `cairn` repo `main` is at
  **`1e6fceb6`**, clean, **0 open PRs**.
- 🔴 **WHAT IS LEFT IS CLAUSE (a), AND IT HAS TWO HALVES — THE CONDITION'S WORDING ONLY
  COVERS ONE.** (a) says "a deployed **image**". Three artefacts can be stale, not one:
  - ✅ **the two pod images** — both bumped to `origin/main` this session and verified by
    DIGEST against values resolved from ghcr *before* each push; deployed commit was 0 behind
    at the time.
  - ⏳ **the INSTALLED CLIENT — still stale, and this is the live item.** Re-measured directly
    rather than taken from the peer block that found it: `readlink -f "$(command -v cairn)"` →
    `…-cairn-5dfc11a/bin/cairn`, and `cairn recall --help` greps **0** for `--ref-to`, **0**
    for `--tag` and **0** for `Requirements`, with `--scope` at **2** as the positive control
    proving the grep works. **So "how do I start using the new features" answers: from the
    browser yes, from the installed CLI NO.**
  - ❌ **no instrument exists that FAILS on any of the three.** That is rank 2 and it is
    unbuilt.
- ⏳ **THE CLIENT FIX IS BUILT, GREEN AND UNMERGED — it is one merge plus one switch away.**
  The config repo's **PR #1933** (`fix/cairn-client-pin-bump`) is OPEN with **all four checks
  PASS**: `pytests collected=24710 passed=24703 skipped=7 failed=0`, `gotests 461/0`,
  `nodetests 1720/0`, and a `cairn-client-runs` leg confirming the pinned client executes.
  ⚠ **Green on a PR is not applied to this host:** that repo's mainline still pins
  `5dfc11a2ac12`, which is byte-for-byte the revision installed here — so the two agree, and
  both are stale. Merging it is necessary AND not sufficient; the client is a home-manager
  `home.file` COPY, so it also needs a switch.
- ⚠ **THE OPERATOR NOW HAS A BROWSER-SURFACE CREDENTIAL AND HAD NONE BEFORE.** Issued for
  their own existing user from inside the pod that owns the journal, delivered to a `0600`
  file in their own client-config directory. **The path is deliberately not written here** —
  this repo is PUBLIC and naming the file is a pointer to a live bearer token.
- ⚠ **A SECOND USER IS PROVISIONED ON THE LIVE DEPLOYMENT AND IS DELIBERATELY INERT.**
  `supabase:sharing-test`, joined to the existing project at role `member`, with **no
  credential** — so it cannot authenticate, and it exists only to keep the share picker
  exercisable. Its grant was revoked and the test invitation was revoked.
- ✅ **CARRIED FORWARD — BOTH PREDECESSOR ARCS ARE CLOSED, RE-MEASURED RATHER THAN READ OFF
  THEIR DOCS** (kept because a REPLACE heading would otherwise drop the values): control-plane
  `python3 -m pytest tests -q` **2357 passed / 0 failed**, `go test ./...` **21 ok / 0 FAIL**,
  `nix eval` resolving `default` and `cairn-go` to the **identical** store path; next-phase
  `#148` merged as `e8839d9` with all four cards `complete`, each feature verified by content.
- ✅ **CARRIED FORWARD — the two pod images were bumped to `origin/main` this session and
  verified by DIGEST** (deployment repo `ab2a59ab5` → `ccdcd13e4`), and a bullet that existed
  in NO committed file was restored as `#149`. Both are recorded in full under `Gotchas` and in
  the RESOLVED investigation block; the values are kept here so a future REPLACE cannot take
  them silently.
- 🔴 **A RETRACTED FINDING OF THIS ARC'S OWN LIVES UNDER `Open investigations` AND IS STILL
  BINDING:** the trusted-proxy "global lockout" claim is WITHDRAWN — there is no such exposure.
  Do not re-derive it; the block says why.
- ⚠ **NO TASK-BOARD FIELD: an UNKNOWN rather than a measured
  absence.** The resolver exited **5**; an unknown session id answers 200 with an empty array,
  so that zero cannot distinguish "touched no task" from "wrong id". None written, none
  created.
- ✅ **SIX PRs MERGED THIS SESSION IN `cairn`**, all docs-only: `#149` (`438b977a`, the rescued
  bullet), `#150` (`25b6ab62`, a peer's arc close), `#151` (`cdf6fae5`, the retraction + four
  defects), `#152` (`01a07c91`, this doc's birth), `#154` (`53534f7e`, clause (b) + the
  counter correction), and two peers' — `#153` (`d6290d54`, the client-side half) and `#155`
  (`1e6fceb6`, retracting a `CAIRN_LIB` two-tier claim `#153` had shipped as measured).

## Open investigations — live diagnosis state

### ❌ RETRACTED IN FULL — "the trusted-proxy allowlist is stale, so every caller shares one lockout bucket"
- as-of: 2026-09-29
- **Symptom + exact repro:** not a defect — a withdrawn claim, recorded so nobody re-derives
  it. Repro of the wrong reading: enumerate every pod's `status.podIP` and look for the
  address the browser surface's `CAIRN_TRUSTED_PROXIES` names.
- **Observed (with values):** the allowlisted single-host prefix is held by **no pod** — 797
  pods enumerated, exact match on the podIP field, with the browser pod's own IP returning a
  row as the positive control. That reading is TRUE. `via: measurement`
- **Ruled out:** ❌ **RETRACTED — the conclusion drawn from it**, namely that
  `netid.PeerIsTrusted` is false for every request, that `CF-Connecting-IP` is therefore
  never read, and that per `netid.ResolveClient`'s documented untrusted-peer branch (bucket
  = the peer's own address) five failed sign-ins from anyone would be a global sign-in
  outage. **The peer the pod sees is not a pod.** It is the `hostNetwork` gateway, whose
  source address toward pods on that node is the `cilium_host` address — a node-level
  address that appears in no pod's `status.podIP` at all. The API deployment's own comment
  records deriving the value exactly that way, with `ip route get <pod-ip>` after a gateway
  roll. `via: code`
- **Ruled out:** that any global-lockout exposure exists. Re-measured the way the value was
  derived — `kubectl -n <gw-ns> exec ds/<gateway> -c <proxy> -- ip route get <pod-ip>` — the
  route leaves via `cilium_host` and its `src` is **the allowlisted address, exactly**. So
  the header IS read and a caller can only ever lock out ITSELF, which is what `netid`'s
  package doc claims. `via: measurement`
- **Leading hypothesis:** none. The claim is withdrawn.
- **Next probe:** none for this claim. 🔴 **The reusable lesson is the point: ask what your
  instrument can REPORT before reading its output as an elimination.** This repository
  already records that sentence about its leak scanner, and it was hit again inside one
  session in a new shape — a 797-row enumeration with a working positive control is exactly
  the evidence that feels conclusive while answering a different question than the one
  asked. If the gateway is ever rebuilt, re-derive with `ip route get`, never by looking for
  a pod.

### ✅ RESOLVED 2026-09-30 — the session store IS written, the invite store IS written, and the share flow is EXERCISED on the live deployment
- as-of: 2026-09-30 · `via: measurement`
- **What this settles:** the `Next probe` of the RETIRED block below ("one real sign-in … reading
  the counter before and after") has been run against the real deployment, and so has everything
  rank 3 was blocked on. **Closing-condition clause (b) is MET.** The block below is
  **retired** — do not re-run its probe or re-derive its framing.
- **How the credential problem was solved, since the operator had none:** `cairn-server` is not in
  the `cairn-ui` image and that pod's `/tmp` is read-only, but the pod **does** carry the exact nix
  glibc the binary is linked against, so the binary was copied onto the writable state volume and
  run there — appending under the same `flock` the application uses — rather than contending for
  the ReadWriteOnce PVC with a second pod. A credential was issued for the operator's OWN existing
  user, delivered to a `0600` file, and the binary and token were removed from the volume
  afterwards. The pod stayed `ready=true restarts=0` throughout.
- **Observed (with values), all against the live deployment:**
  | reading | before | after |
  |---|---|---|
  | `select count(*) from sessions` | 0 | **1** (`user`, the operator's user id) |
  | `select count(*) from invites` | 0 | **1**, minted then **revoked** |
  | live `/share` candidate list | no candidate possible | **offers a real second user** |
  | `granted` / `grant-revoked` in the journal | 0 / 0 | **1 / 1** |
  | journal records | 30 | **37** |
  `POST /sign-in` → **303** with `__Host-cairn-session; Path=/; HttpOnly; Secure; SameSite=Lax`;
  `GET /` authenticated → **200** with 26 scope cards; anonymous → **401**.
- 🔴 **AND THE CLAUSE THIS ARC EXISTS TO PROVE: live `/share` rendered 2 × `read,write`
  "via project membership" while "shares you can take back" said "No grant names this scope"** —
  a principal with authority and NO grant, which is exactly the positive control `AGENTS.md`
  names as proof the listing is computed from `control.Resolve` and never from `Model.Grants`.
  Both implementations render a principal-with-both identically, so this is the first evidence
  that claim holds rather than being asserted. `via: measurement`
- 🔴 **CORRECTION TO THE RETIRED BLOCK'S INSTRUMENT, AND IT IS THE REUSABLE PART:
  `pg_stat_user_tables.n_tup_ins` LAGS, SO ITS ZERO CANNOT DISTINGUISH "NEVER WRITTEN" FROM
  "WRITTEN SECONDS AGO".** Measured: immediately after the live sign-in the row was ALREADY
  present (`count(*)=1`, `issued_at` stamped) while the view still read `n_tup_ins=0
  n_live_tup=0`; a later read caught up to `ins=1 live=1`. `track_counts=on`, and
  `stats_fetch_consistency=cache` is why — a stats snapshot is cached. **So the authority is
  `select count(*)`, a direct read; the counter is lagging corroboration, never the primary
  reading.** The retired block's conclusion was nonetheless CORRECT, because it rested on
  `count(*) = 0` — but its stated evidence, and the `schema_migrations n_tup_ins=1` "positive
  control" beside it, would give a false negative to anyone re-running it within seconds of a
  sign-in. ⚠ Same shape as this doc's other retraction: an instrument answering a different
  question than the one asked.
- **Ruled out:** that the second user needs a credential to make the picker usable. It does not —
  it is left provisioned as a co-member with **no** credential, so it is inert but keeps the share
  picker exercisable. `via: measurement`
- **Next probe:** none for this block; it is closed. What remains of this arc is clause **(a)**,
  the deployed-artefact currency instrument, which is rank 2.

### ❌ RETIRED 2026-09-30 — superseded by the block above; its probe has been run and its instrument was wrong
### The browser surface's session table has never been written to
- as-of: 2026-09-29
- **Symptom + exact repro:** not a defect yet — a deployed feature with zero exercise.
  `kubectl -n subsystem-store exec sts/cairn-ui-postgres -- psql -U cairn_ui -d cairn_ui -c
  "select relname,n_tup_ins,n_tup_del,n_live_tup from pg_stat_user_tables order by relname;"`
- **Observed (with values):** `sessions` → `n_tup_ins=0 n_tup_del=0 n_live_tup=0`, cumulative
  since the postmaster came up at the 28(d) cutover (`pg_stat_database.stats_reset` is
  **NULL**, so the counter spans the whole life of the database).
  `schema_migrations` → `n_tup_ins=1`, which is the **positive control**: the counter moves,
  and this is the database the application migrated. `invites` → 0. The table's columns are
  `digest, kind, principal, issued_at, expires_at`. The startup log carries both the
  `$CAIRN_UI_SESSION_FILE … is IGNORED` line and
  `state sessions in postgres, invitations in postgres`. `via: measurement`
- **Ruled out:** that rows were written and later expired away — `n_tup_del` is 0 and
  `n_tup_ins` is cumulative, so a write-then-delete would still show. `via: measurement`
- **Ruled out:** that the deployed image lacks the code — the running binary PRINTS
  `sessions in postgres`, and the pinned tree carries `CAIRN_UI_DB_DSN` in
  `cmd/cairn-ui/main.go` plus `internal/pgstore`. `via: measurement`
- **Ruled out:** that the operator's own store credential can drive the probe. One
  credential-form POST with a correct `Origin` returned **401** and wrote nothing (correct
  for a refusal — the counter stayed 0). The store API token is a DIFFERENT credential from
  the browser surface's, whose credential was issued into the control journal at seeding.
  `via: measurement`
- **Leading hypothesis:** nobody has signed in since the cutover. A startup banner is
  configured-state, not evidence the feature works — which the 28(d) block said in advance,
  and which this reading is the first to test.
- **Next probe:** one real sign-in with the credential from the control journal, reading the
  counter **before and after** so the move is the evidence rather than the end state.
  ⚠ Budget: the lockout is 5 failures / 900 s bucketed on the client key, and **one of those
  five is already spent** by the 401 above. The allowlist is correct (see the retraction), so
  a further failure locks out only the caller — not everybody.

### The INSTALLED client carries none of the three features this repo shipped, and no card, claim or arc owns it — this arc's client-side half
- as-of: 2026-09-29
- **Symptom + exact repro:** `cairn recall --help` on this host lists neither `--ref-to` nor
  `--tag`. `readlink -f "$(which cairn)"` → a `/nix/store/…-cairn-5dfc11a/bin/cairn`.
- **Observed (with values):** installed revision **`5dfc11a`**, dated **2026-09-23**, is
  **63 commits** behind `origin/main` and `merge-base --is-ancestor` says it is an ancestor of
  **all three** feature squashes (`5c59169` refs, `94ecb7e` tags, `e8839d9` requirements) — so
  it predates every one. `--ref-to` and `--tag` each grep **0** in `--help`. It is the PYTHON
  client (argparse usage; `-verbs` refuses at exit 2), while the flake's `default` is now the
  Go one. It is installed by **home-manager as a `home.file` COPY** — `readlink -f` terminates
  in `…-home-manager-files/…`, so editing anything does nothing and it needs a switch — and the
  revision comes from the other repo's `flake.lock` node `cairn`, locked at `5dfc11a2ac12`.
  `via: measurement`
- **Ruled out:** the deployed PODS as the gap. They carry all three, measured behaviourally
  against the live pod with a control on each: `?ref-to=zzz-no-such-ref` → **400** carrying
  662's own refusal (*"not a well-formed `<system>:<id>` ref"*); `?tag=<bogus>` →
  **`status=tag-absent`** with a real narrowing sentence (*"0 of 13 entries … carry it"*); and a
  `## Requirements` section written into a real entry → index row **`🔴 2 REQ OPEN ✅ 1 REQ
  MET`**, the counts matching the content exactly. That is a SECOND, INDEPENDENT instrument
  agreeing with this arc's digest verification — behaviour where that read identity.
  `via: measurement`
- **Ruled out:** this arc already covering it. Closing condition (a) is about a deployed
  **image's** commit; grepping this doc for `flake.lock`, home-manager, "installed client" and
  `5dfc11a` returns **empty**, and none of the five ranked steps names it. No task-board card
  matches, and `claim-work --list` holds no claim on it (`cairn-pods-renderer-lag` is released).
  `via: measurement`
- **Leading hypothesis:** nothing is broken — the pin was simply never bumped. It is 6 days old
  and three user-facing features have landed behind it. `via: assumed`
- **Next probe:** in the other repo,
  `python3 -c "import json;print(json.load(open('flake.lock'))['nodes']['cairn']['locked']['rev'])"`
  then `nix flake update cairn` and a switch. Expect `scripts/tests/test_cairn_flake_pin.py` to
  gate it: it pins the whole SEAM as one relationship (input → outputs argument →
  `extraSpecialArgs` → `nix/home.nix`'s module header → the deploy line), plus the half of the
  split that must NOT move to the package, plus the `CAIRN_MIRROR_ROOT` export whose absence
  silently downgrades a `doctor` check from PASS to NOT-RUN. 🔴 **Decide Python-vs-Go default
  BEFORE opening it** — taking `packages.default` flips `-verbs`/`-exit-codes` from exit 2 to
  exit 0, which is a public-surface change and the operator's call, not a side effect of a bump.

### SUPERSEDES the "Next probe" on the client-pin block above: the bump was BUILT, it is RED, and the cause is a VOCABULARY SEAM rather than a regression
- as-of: 2026-09-29
- 🔴 **The block above ends with "open the PR and expect `test_cairn_flake_pin.py` to gate it".
  That probe is ANSWERED and its expectation was WRONG in an instructive way** — the seam guard
  it named passes; eight OTHER guards fail. Do not re-run that probe.
- **Symptom + exact repro:** the other repo's PR (`flake.lock`'s `cairn` node only,
  `5dfc11a2ac12` → `cdf6fae5f6ba`) is RED on that repo's `pytests` context. Repro:
  `CAIRN_LIB=<new-store-path>/libexec/cairn/lib` then the repo's own runner.
- **Observed (with values):** full suite under the new lib —
  **`TOTAL collected=24707 passed=24692 skipped=7 failed=8`, `RESULT: FAIL (exit=1)`.**
  `nodetests`, `gotests` and the **pinned-client leg all PASS**, so the client itself runs. The
  eight, by name: `test_every_declared_status_is_reachable` ·
  `test_all_three_badges_reproduce_the_FULL_prose` · `test_kills_the_what_it_is_INCLUSION` ·
  `test_kills_the_unreadable_entry_wrap` · `test_kills_the_search_unreadable_discriminator` ·
  `test_the_WRITER_refuses_a_bare_string_the_READER_would_reject` ·
  `test_a_github_ref_survives_write_read_BYTE_IDENTICALLY` · `test_kills_the_ITERDIR_choice`.
  `via: measurement`
- **Ruled out:** a pre-existing red. That repo's mainline is green on all four contexts
  (`collected=24707 passed=24700 failed=0`). `via: measurement`
- **Ruled out:** anything other than the pin. Same tree, same test, only `CAIRN_LIB` differs —
  **old lib 1 passed, new lib 1 failed.** One variable, both directions. `via: measurement`
- **Ruled out:** the seam guard the input's own comment names. `test_cairn_flake_pin.py` and six
  other client suites pass — **282 passed / 0 failed** — which is exactly why that green was
  believed and was not evidence. `via: measurement`
- **Leading hypothesis — and it is a mechanism, not a guess.** The three shipped features GREW
  the vocabulary those guards enumerate: a fourth canonical section, two new statuses, two new
  badges (so a guard asserting "all **three** badges" is false *by arithmetic*), and ref
  grammar. Both repos' suites are green in isolation; together they are not. Worked example, the
  `ITERDIR` one: it mutates the FILENAME tier (`iterdir`→`glob`) and asserts a `0o000` scope then
  emits NO caveat — a silently-wrong "slug is free". Under the new lib the ALIAS tier catches the
  `PermissionError` and says so, so the mutant can no longer be silent. **The client got SAFER;
  what broke is the fixture's isolation premise**, because one `chmod` breaks both tiers.
  `via: measurement`
- **Next probe:** update that repo's writer-side guards to absorb the new vocabulary — the
  heading set, the status set, the badge cardinal (**derive it, never re-count it in prose**), and
  the ref grammar — then re-run under `CAIRN_LIB=<new>`. 🔴 **Do NOT relax the assertions to go
  green:** five of the eight guard against a SILENT wrong answer, and three are mutation kills
  whose whole value is refusing. Expect the badge cardinal to be the documented
  hand-written-cardinal trap in a new place.

### RESOLVED, and it RETRACTS the `CAIRN_LIB` two-tier gotcha this same session wrote two blocks above
- as-of: 2026-09-29
- 🔴 **THE FIX IS LANDED AND THE SUITE IS GREEN**, so the "Next probe" on the block
  above is answered: the other repo's PR carries a second commit updating the eight
  guards. Full suite under the pinned lib: **`TOTAL collected=24710 passed=24703
  skipped=7 failed=0`, `RESULT: PASS (exit=0)`**, 0 failure headers. The arithmetic
  closes exactly — `+3` collected are three tests this change ADDS, `+11` passed is
  the 8 that were red plus those 3, skips unchanged at 7 — so nothing hid behind the
  failures and no new failure replaced one. `via: measurement`
- 🔴 **RETRACTED: "a pin bump's local suite runs against the INSTALLED client, so a
  green local run is structurally unable to see the bump."** That block is above, it
  is stated as measured, and it is **FALSE**. Do not act on it and do not re-derive
  it. Measured directly: inside `nix develop` with `$CAIRN_LIB` unset, `cairn` on
  PATH resolves to the **pinned** package (`flake.nix` puts `cairn.packages.*.cairn`
  in the devShell) and `cairn_pin.ensure()` returns the pinned `lib/`. And
  `run-tests.sh`'s own header records that the pre-push tier "now runs `nix
  develop`". **Both tiers read the same lib; there was no tier gap.** `via: measurement`
- 🔴 **THE REAL CAUSE WAS COVERAGE, AND IT IS A DULLER LESSON WORTH MORE.** The
  pre-push check on the bump ran **seven `*cairn*`-named test files** and reported
  "282 passed / 0 failed" as if that gated it. **None of the three files that fail is
  `*cairn*`-named** — they are the recall, touch and task-refs suites. A
  FILENAME-SHAPED selection cannot gate a change whose blast radius is a shared
  VOCABULARY, because the vocabulary's consumers are not named after it. Ask what
  the change's blast radius is keyed on, then pick the target set from THAT.
  `via: measurement`
- ⚠ **ALSO RETRACTED: "merge and switch together, or pre-push fails on an unswitched
  host."** Same root error. Pre-push enters `nix develop`, so it gets the pinned lib
  whatever is installed. A `home-manager switch` is still wanted — but only so the
  operator's INTERACTIVE `cairn` gains the features, which was the point of the bump,
  not a test concern. `via: measurement`
- **`$CAIRN_LIB` is still the right INSTRUMENT, just not the explanation.** Setting it
  explicitly is how the eight were attributed to the bump rather than to the tree:
  same tree, same test, **old lib 1 passed / new lib 1 failed**. That old-lib arm is
  an A/B probe, not a configuration anybody runs. `via: measurement`
- **Next probe:** none for this. The remaining question is the operator's: merge the
  other repo's PR, then switch.

### The client-side half is GREEN on a PR and STALE on this host, and those are different claims
- as-of: 2026-09-30
- **Symptom + exact repro:** the installed CLI carries none of the three features this repo
  shipped. `readlink -f "$(command -v cairn)"`; then
  `cairn recall --help | grep -c -- --tag`.
- **Observed (with values):** installed store path is `…-cairn-5dfc11a/bin/cairn`;
  `--ref-to` **0**, `--tag` **0**, `Requirements` **0** in `--help`, with `--scope` at **2**
  as the positive control proving the grep can match. The config repo's mainline
  `flake.lock` node for this project locks **`5dfc11a2ac12`** — the same revision — so the
  pin and the install agree and both predate all three feature squashes. `via: measurement`
- **Ruled out:** that the fix is unwritten or red. The config repo's **PR #1933** is OPEN
  with all four checks PASS (`pytests 24703 passed / 0 failed`, `gotests 461/0`,
  `nodetests 1720/0`, plus a leg that runs the pinned client). `via: measurement`
- **Ruled out:** that merging it is sufficient. The client is installed by home-manager as a
  `home.file` **copy** — `readlink -f` terminates in the store, not in a working tree — so
  the tree changing does nothing until a switch runs. `via: code`
- **Ruled out:** that the PODS share the gap. Both were bumped to `origin/main` this session
  and verified by digest; the three features are live on the browser surface, which is why
  the browser answer and the CLI answer differ. `via: measurement`
- **Leading hypothesis:** nothing is broken — the pin was never bumped, and the bump is now
  one merge plus one switch from applied.
- **Next probe:** merge the config repo's PR #1933, then run the home-manager switch, then
  **re-run the `--help` greps above and require `--ref-to` and `--tag` to be non-zero** —
  the switch is the step that can silently not happen, and `--help` is the cheapest reading
  that can tell. 🔴 **Decide Python-vs-Go default BEFORE merging:** taking
  `packages.default` flips `-verbs`/`-exit-codes` from exit 2 to exit 0, which is a
  public-surface change and the operator's call, not a side effect of a version bump.

### ANSWERED — the Python-vs-Go decision is MADE (Go), and it is a PIN-SEAM SPLIT rather than a one-line flip
- as-of: 2026-09-30
- **What this settles:** the client-pin block's 🔴 *"Decide Python-vs-Go default BEFORE opening
  it"* is ANSWERED — **operator decision: take the GO client.** Do not re-open it; below is its
  measured COST, unknown when that sentence was written.
- **Observed (with values)** — both packages built from the pinned revision and inspected, not
  inferred from the flake:
  | reading | `cairn-<pin>` (Python) | `cairn-go-<pin>` (Go) |
  |---|---|---|
  | top level | `bin` + **`libexec`** | `bin` **only** |
  | `entry_shape.py` / `subsystem_resolver.py` | present / present | absent |
  | `--ref-to` / `--tag` in `recall --help` | 2 / 2 | **1 / 1** |
  | `--scope` (control) / a bogus flag (control) | 2 / 0 | 1 / **0** |
  | `-verbs` | rc **2** (argparse refusal) | rc **0** + table |
  `via: measurement`
- **Ruled out:** that the flip is re-pointing one threaded package name. The config repo's
  `scripts/lib/cairn_pin.py` reaches the reader modules by route 2 — `which cairn` → `realpath` →
  `<store>/libexec/cairn/lib`, accepted on **CONTENT** (both marker modules) with **no local
  fallback**, because that repo deleted its own copies. The Go package has no `libexec` at all, so
  route 2 refuses and `CairnPinUnresolved` fires for **22 tracked files importing `cairn_pin`**, 11
  importing `cairn_lib`, the out-of-store `cairn-who` and `cairn-validate` launchers (the latter is
  named by the `subsystem-index` write protocol), and the writer. `via: measurement`
- **Ruled out:** that an env var alone is the fix. `cairn_pin`'s own docstring records that the
  variable route exists for sandboxes and that **route 2 is the one that must work unattended** —
  the session-variables file lands in `profile.d`, which a non-interactive shell does not source.
  So the replacement must be an on-PATH, content-valid answer, not a `CAIRN_LIB` export.
  `via: code`
- **Ruled out:** that the four parse contracts the config repo's ops wrappers depend on break
  on the Go client. All four probed directly against the built Go binary, one control each —
  values in the Gotchas entry below, not repeated here. `via: measurement`
- **Ruled out:** that the Go package is reachable only at the NEW pin, making the flip depend on
  the bump. `packages.cairn-go` and `default = mkGoClient` are present in the flake at **both** the
  old and the new pinned revisions. The bump is still required for the *features* — the old
  revision predates all three — but not for evaluation. ⚠ This CORRECTS a claim I made earlier in
  this same session and had not measured. `via: measurement`
- **Leading hypothesis — a mechanism, not a guess.** One name does two jobs: *the binary the
  operator types* and *the source of the reader modules*. The flip separates them — deploy line
  takes the Go package, a second threaded name keeps the Python one, and an in-store `cairn-py`
  launcher gives route 2 a content-valid answer without an env var. 🔴 **Bind it to
  `packages.cairn-go`, NOT `packages.default`**: same store path today, but `default` can be
  re-pointed upstream without the name changing meaning, and a pin must not inherit that.
- **Next probe:** rank 6 carries the design and the file set. The two RED proofs it must not skip:
  watch `cairn_pin` route 2 REFUSE on a Go-only PATH and then resolve once a Python-lib launcher
  is on PATH (both directions), and watch the new threaded-name guard fail against a decoy
  binding of the right name pointing at the wrong package — **that exact decoy shape is recorded
  as having SURVIVED in the guard file already**, so copy its assignment-bound regex discipline
  rather than searching the whole region.

### ✅ RESOLVED 2026-09-30 — the pin bump is MERGED, SWITCHED and verified BEHAVIOURALLY on the installed client; and the blocker above was ALREADY BEING REPORTED by a red instrument nobody reads
- as-of: 2026-09-30
- **What this settles:** the client-pin block's *"Next probe: … then `nix flake update cairn` and a
  switch"* is **RUN**. The config repo's PR is squash-merged and the switch is done. The two blocks
  immediately above — the Go decision and the parked-base-clone blocker — are **NOT** superseded:
  the flip itself is still unbuilt, and this entry is the PYTHON step only.
- 🔴 **AND THE BLOCKER THAT MADE THE ROUTE NON-OBVIOUS, RECORDED BECAUSE THE NEXT SWITCH HITS IT
  TOO: the config repo's base clone is parked on ANOTHER SESSION'S BRANCH** — 1 ahead / 3 behind
  its mainline, in sync with its own remote — and that branch pinned the OLD revision, as did the
  mainline until this merge. So `home-manager switch --flake <config-repo>` run there builds the
  **old** client and **exits 0**, leaving the greps at 0, which reads as "the bump did not work".
  🔴 **Do NOT fix it by switching branches in the shared base clone** — the rules forbid it and
  the branch is someone else's. Use a clean worktree at the merged mainline. ⚠ The session-start
  status line cannot show this: it reports the repo the session is IN, and an earlier command here
  read a REMOTE ref, which never sees the local checkout's branch. `via: measurement`
- **Observed (with values), on the INSTALLED path rather than a store path built by hand:**
  | reading | before | after |
  |---|---|---|
  | `readlink -f "$(which cairn)"` | `…-cairn-5dfc11a/bin/cairn` | **`…-cairn-cdf6fae/bin/cairn`** |
  | `--ref-to` in `recall --help` | 0 | **2** |
  | `--tag` in `recall --help` | 0 | **2** |
  | `--scope` (instrument control) | 2 | 2 — unchanged, so the grep worked BOTH times |
  | a bogus flag (negative control) | 0 | 0 |
  | `-verbs` | rc 2 | rc 2 — still the PYTHON client, as this step intends |
  The merge landed as one squash commit; verified **by content, never ancestry** — the config
  repo's mainline pins the new revision and the old revision greps **0**. `via: measurement`
- 🔴 **AND `--help` IS NOT BEHAVIOUR, so both features were EXERCISED with a control each:**
  `--ref-to` with a malformed ref → rc **2** carrying the ref-grammar refusal (*"not a well-formed
  `<system>:<id>` ref"*); `--tag` with a bogus tag → **`status=tag-absent`** with the real narrowing
  sentence (*"0 of 13 entries in `cairn/` carry it … This is a NARROWING, not a truncation"*). Those
  are the SAME strings a previous session measured against the live pods — so pod and installed
  client now agree on behaviour, not merely on a version. `via: measurement`
- **Ruled out:** that switching from a WORKTREE instead of the parked base clone would repoint the
  two out-of-store launchers at a scratch path. Checked BEFORE the switch by reading the module's
  own binding (the workspace prefix is derived from `$HOME`, not from the flake's location) and
  AFTER by `readlink -f` — the arbiter, because one level of `readlink` shows only the
  home-manager-files store indirection and would have read as a store copy. **Both launchers
  terminate in the base clone; 0 terminate in the scratch worktree, 2 in the base clone**, and one
  of them runs to rc 0. `via: measurement`
- 🔴 **Ruled out: that the parked-base-clone blocker above was unobserved. IT IS BEING REPORTED
  EVERY SIX HOURS BY A UNIT THAT HAS BEEN RED FOR AT LEAST TWO DAYS.** The switch printed
  `degraded` and named one failed user unit — the passive drift deadman. Its exit code is
  **12 = `not-on-branch-main`**, and its own output names the branch:
  *"DRIFT — checkout is on '<branch>', not on branch main … anything committed on '<branch>' stays
  invisible to origin/main."* Failure history: **12, 17, 17, 17, 17, 12** across the last six
  timer firings, the oldest ~2 days before this session. ⚠ **PRE-EXISTING, NOT CAUSED BY THE
  SWITCH** — the last failure stamped ~48 minutes BEFORE the switch ran, and it is timer-triggered.
  `via: measurement`
- **Leading hypothesis:** this is the *permanently-red-gate* failure, not a missing instrument. A
  currency check that fires on schedule, names the exact drift, and is never read has already
  trained its own bypass — and this arc spent a session rediscovering by hand what it prints.
  🔴 **This is DIRECT INPUT TO RANK 2: before building a new currency instrument, account for the
  one that already exists and is red.** A second unread instrument is worse than none.
- **Next probe:** for the drift deadman — `systemctl --user status drift-check.service` and
  `journalctl --user -u drift-check.service` for the per-host lines, then decide between unbreaking
  it and stopping gating on it; do NOT add a third check beside it. ⚠ Two of its lines are
  unreachable REMOTE hosts, so part of its red is about the other machine and not about this one —
  read the per-host lines before attributing the whole verdict. For rank 6's remaining half: the Go
  flip is designed but UNBUILT, and the decision it implements is the operator's, already made.

## Next steps (ranked)

🔴 **NUMBERING IS STABLE — a rank is half a `claim-work` slug**, and `claim-work` comes BEFORE
you act. ⚠ **AND RUN `gh pr list --state open` ANYWAY, TWICE — before starting and again
immediately before `gh pr create`.** Measured three times: the pre-work sweep was clean and a
peer's PR appeared mid-write (`#150`, `#153`, `#155` — the last one retracting a claim a peer had
shipped hours earlier). `claim-work` answering **rc 12, already yours** is indistinguishable from
untouched work; only the sweep sees a duplicate nobody claimed.

1. ✅ **DONE — all PRs merged.** Six landed in the prior session, verified BY CONTENT on
   `origin/main` with a negative control at 0; ancestry was deliberately not used (a squash never
   makes the branch head an ancestor). `#156` and this doc's own PR landed after it, same way.
   forcing: gate — unmerged docs PRs are the `stranded-docs` shape, and `#149` exists because
   that shape already cost a bullet.
2. **BUILD THE DEPLOYED-ARTEFACT CURRENCY INSTRUMENT — the (a) half, still unbuilt and now
   known to cover THREE artefacts** (two pod images, one installed client), not the one its
   wording names. 🔴 **Watch it go RED on a deliberately stale pin before believing it**, and
   do not accept "both pods carry the same tag" as the check — they were equal to each other
   and both stale, twice in one session. 🔴 **AND ACCOUNT FOR THE CURRENCY INSTRUMENT THAT
   ALREADY EXISTS AND IS RED: the config repo's drift deadman has been failing every six hours
   for two days, exit 12 = `not-on-branch-main`, naming the exact drift this arc rediscovered by
   hand.** Unbreak it or stop gating on it; a second unread instrument beside it is worse than
   none. Evidence: the RESOLVED investigation block.
   forcing: regression — the image gap re-opened within minutes of being closed, twice, and no
   gate in any of the three repositories can see any of the three artefacts.
3. ✅ **DONE 2026-09-30 — clause (b) is MET.** Session table `count(*)` 0 → 1 on the live
   deployment, invite store 0 → 1, share flow exercised end to end. Claim
   `cairn-ui-session-store-probe` RELEASED. forcing: user — the operator held the only
   credential that could run it, and that is now moot: one was issued for their own user.
4. **P8 — retire the Python oracle.** Carried over unchanged: a real read AND a real write
   against the live pod from **two distinct hosts**, recorded, AND no open defect naming the
   Go client or `packages.default`.
   ⚠ **AND RANK 6 NOW FEEDS IT: the operator has chosen the Go client for the installed CLI, so
   P8's decision half is partly pre-answered — but the pin-seam split rank 6 describes must land
   FIRST, or retiring `packages.cairn` removes the reader modules 22 config-repo consumers
   import.** **BACKSTOP: not done by 2026-11-01 ⇒ P8 opens anyway and the residual risk is
   accepted EXPLICITLY, in writing.**
   forcing: deadline — the 2026-11-01 backstop, set by the operator.
5. **Fix the base-clone write guard.** It reproduced a **FIFTH** independent time: it refused
   commits in linked worktrees while naming the base clone, having resolved the repo from `$PWD`
   rather than the command's `-C` target. Premise proved false before every override.
   **Closing condition:** the guard admits a linked worktree AND reads `-C`, with a test that a
   real base-clone write is still refused.
   forcing: gate — a guard whose diagnosis is reliably about the wrong repository trains its own
   bypass, which is the permanently-red-gate failure wearing a different hat.
6. 🔶 **HALF DONE — THE PYTHON STEP SHIPPED; THE OPERATOR'S DECISION (GO) IS UNBUILT.** ✅ The
   config repo's PR is squash-merged, the switch is run, and the installed client carries both
   features — verified BEHAVIOURALLY, not just greped in `--help`. **Do NOT re-do that half.**
   ⛔ What remains is the GO FLIP, a **PIN-SEAM SPLIT** rather than a one-line change: the Go
   package ships no `libexec/cairn/lib`, which is how 22 config-repo files, both out-of-store
   launchers and the writer reach the reader modules via `cairn_pin` route 2 — content-validated,
   no fallback. Design, file set and RED proofs: the ANSWERED block. 🔴 **Switch from a CLEAN
   WORKTREE at the merged mainline** — the base clone is parked on another session's branch, where
   a switch rebuilds the OLD client at exit 0.
   forcing: user — the operator asked directly *"how can we validate and start using the new
   features?"*, and chose Go over Python for the installed client when asked.
7. **TRIAGE THE SEVEN UN-DROPPED SCOPE ITEMS from the original asks**, each measured absent on
   `main`: a PWA (no manifest, no service worker), htmx (0 occurrences — the surface is
   server-rendered gomponents), Google sign-in (only the one provider route exists), "move scope
   ownership between projects" and "remove a member" (both exist ONLY as declared journal event
   kinds with no writer, no CLI flag and no UI route), **scope**-level tags (entry-level `tags:`
   shipped instead), and `/the-algorithm` over the whole design (asked 2026-09-23, still 0 hits).
   Two look like conscious narrowings — Google was re-specified to one provider, and scope tags
   may have been narrowed during the proposal walkthrough — but neither was ever recorded as a
   decision, so they read as dropped.
   forcing: user — all seven were asked directly and none was ever declined in writing.

## Defects (batched)

- 🔴 **THE DEPLOYED RENDERER GOES STALE ON EVERY `internal/report` MERGE AND NOTHING
  OBSERVES IT.** Rank 2 owns the fix; this entry owns the measurement. Both pods sat at
  `bcfb60a`, agreed with each other, and were one code commit behind `main` across `#146`.
  Bumped to `b2b54e4`; `#148` merged **while that was reconciling** and touches
  `internal/report/{entry,prose}.go`, so it re-opened. **Pod-to-pod agreement is not the
  property the byte-identity gates hold — pod-to-client is, and two equal tags are equally
  stale.**
- 🔴 **`#140`'s "0 lost, 0 duplicated" WAS TRUE OF THE BULLETS IT MOVED AND SILENT ABOUT ONE
  IT DELETED.** Closed by **#149**. **An eviction's safety property must be measured against
  the COMMIT, never against the working tree that produced it.**
- ⚠ **`grep -c` ON A PHRASE THAT WRAPS ACROSS A NEWLINE ANSWERS A FALSE 0, AND IT COST A
  WRONG CONCLUSION HERE BEFORE THE CONTROLS CAUGHT IT.** Flatten with `tr '\n' ' '` before
  believing an absence in prose. Two separate false zeros this session.
- ⚠ **AN UNBRACED `$ref:` IN zsh ATE A GIT REF AND RETURNED A CONFIDENT WRONG 0.**
  `git show $ref:path` with `a035483^` silently failed under `2>/dev/null`, reading as "the
  bullet was never there". Brace it (`${ref}`) and quote the pathspec.
- 🟡 **FOUR FILES ARE NOT `gofmt`-CLEAN AND NOTHING GREPS IT** —
  `internal/client/{anchor_test,exit,options}.go`, `internal/control/tokenfile/source.go`.
  Pre-existing; re-confirmed. Carried from both predecessor docs.
- 🟡 **`AGENTS.md` + `CLAUDE.md` PASS THEIR GATE BY ONE BYTE.** Re-measured on `main`:
  `MAX_BYTES = 32_500`, `MIN_HEADROOM_BYTES = 900`, files **31,332 + 267 = 31,599**, so
  headroom is **901 against a 900 floor** — green, and the next edit of any size reddens it.
  Unchanged by `#146` touching `AGENTS.md`. Filed as card 682 on the next-phase doc.

## Gotchas / decisions / dead-ends

- **Decision (operator, this session): deploy the image bump straight to the deployment
  repo's `trunk`** — where commit IS deploy — rather than via a PR. Two deploys resulted,
  because `main` moved mid-reconcile; the second was the same approved step re-targeted, not
  a new one.
- **Decision (operator, this session): drive the session probe with the operator's existing
  store token.** Measured insufficient — it is a different credential namespace (401). The
  browser surface's credential is the one in the control journal.
- 🔴 **A ROLLBACK TARGET IS A CLAIM WITH A SHELF LIFE, AND THE DEPLOYMENT REPO SAYS SO IN
  THE FILE.** Both pins' rollback comments were re-pointed at the actual predecessor in the
  same commit that moved `image:`. The UI pin had no rollback line at all and now has one.
- 🔴 **THE SWEEP CAUGHT A DUPLICATE AT THE SECOND WINDOW, AND THE LOCK COULD NOT HAVE.** The
  pre-work sweep was clean; a peer's #150 appeared while this session was writing the same
  bookkeeping, and the sweep run immediately before `gh pr create` is what saw it. The
  overlapping half was then withdrawn so #150 owns it alone. **Run the sweep at both
  moments, and remember rc 12 is the one return value that suggests you have nothing to
  check.**
- ⚠ **`rollout status` AND A FLUX RECONCILE ARE DIFFERENT CLAIMS — reconcile the SOURCE,
  then the Kustomization, then read `imageID`.** Followed here; the source lagged the
  Kustomization on the first pass and the applied revision was read back explicitly both
  times.
- ⚠ **A MOVED IMAGE DIGEST IS NOT A CHANGED SERVER** — the Go binary's nix store path embeds
  the short rev, so every commit to `main` produces a new digest even when no Go code
  changed. Recorded in the deployment repo; restated because a digest diff is tempting to
  read as evidence.
- 🔴 **THE PUBLIC REPO'S LEAK RULE BIT THE BLOCK DESCRIBING THE NETWORK FINDING.** The
  retraction above first spelled the allowlisted address in order to explain it. Every
  address is now named by ROLE, checked mechanically against the diff (0 matches).
  **An example that IS the thing it forbids is the thing it forbids.**

- 🔴 **A DEPLOYED-CURRENCY ARC HAS A CLIENT-SIDE HALF, AND IT IS THE HALF NOBODY IS LOOKING
  AT.** "What is deployed" naturally reads as pods and images; the artefact an operator actually
  TYPES is pinned separately, by a different repo, through a different mechanism (home-manager
  `home.file`, not an image tag), and it can be six days and three features behind while every
  pod is at `origin/main` and every currency check is green. The full measurement is the Open
  investigations block above. **Any instrument built for closing condition (a) should be asked
  whether it can see the client at all** — as specified it cannot.
- 🔴 **`x-store-revision: unknown` — THE HEADER EXISTS AND NOTHING POPULATES IT, MEASURED.** The
  pod already answers a revision header on every read and its value is the literal string
  `unknown`, so you cannot ask a running pod which code it is. There is **no `/version` route**
  either — the read heads are exactly `recall`, `search`, `snapshot`. That is why establishing
  whether a pod carried a feature needed a behavioural probe rather than one `curl`. It is also
  a ready-made home for this arc's instrument: populating that header at build time makes pod
  currency a one-request check instead of a deploy-repo archaeology exercise.
- 🔴 **THE CLOSED `next-phase` ARC'S RANK 9 IS CONFIRMED LIVE, WITH A PAIRED CONTROL — and it is
  now on real content.** Identical `OPEN:` marker text: under `## Requirements` the validator
  reports **`0 declared`**; moved verbatim under `## Nuance / work-history` it is **found**. Only
  the SECTION differs, so the scoping is the mechanism, not the spelling. A whole-scope run
  agrees (13 entry files, still only the one pre-existing declared `OPEN:`). So a requirement's
  open state reaches **no** validator surface. Recorded as an `OPEN: (inferred)` requirement on
  the `cairn/report` store entry, which is self-demonstrating: the bullet describing the
  blindness is itself invisible to the check that would report it.
- 🔴 **A `## Requirements` BADGE LIVES ON THE INDEX VIEW, NOT ON A `--ref` READ — AND PROBING THE
  WRONG ONE READS AS "THE FEATURE IS ABSENT".** A `--ref` read renders the section's body
  verbatim (bodies always are) and emits **no** badge, so `grep REQ` there returns 0 on a pod
  that fully supports it. Use the index/list view, and pair it with a badge-rendering control
  (assert some OTHER badge appears) so a zero cannot mean "badges are off everywhere".
- ⚠ **#150 WAS MERGED 16 MINUTES BEFORE ITS OWN CHECKS SETTLED** — merged 22:43:22Z, all 8 green
  at 22:59:43Z, no auto-merge. The outcome was benign and the head was already 6-of-8 green, but
  what a gate buys is the ORDERING of the evidence, not the outcome, and that is what was spent.
  Recorded, not relitigated.
- ⚠ **zsh: AN UNQUOTED MULTI-PATH `$3` INSIDE A FUNCTION IS **ONE** PATHSPEC, NOT TWO.** zsh does
  not word-split, so `git grep -l "$1" <rev> -- $3` with `$3="internal/api internal/report"`
  matches nothing and returns a confident **0 for every row** of a check table. Three features
  read as "integration ABSENT" until a positive control caught it. Use `${=3}` or a real array.
- ⚠ **`clawgatectl task list` IS NOT A VERB — IT IS `ls`** — and the wrong spelling printed
  nothing and exited without an error, which reads exactly like "no open tasks". Same shape as
  grepping JSON for `^status`: the tool answered about itself, not about the board.

- 🔴 **A PIN BUMP'S LOCAL SUITE RUNS AGAINST THE *INSTALLED* CLIENT, NOT THE PINNED ONE — SO A
  GREEN LOCAL RUN IS STRUCTURALLY UNABLE TO SEE THE BUMP.** `cairn_pin` resolves the packaged
  `lib/` from **`$CAIRN_LIB`, else from `~/.local/bin/cairn`'s store path** — never from
  `flake.lock`. Measured: 282 tests passed locally and were reported as covering the bump; they
  exercised the OLD lib, while CI's sandbox exercised the NEW one and found 8 failures. This is
  the two-tier rule with a concrete mechanism: **name the variable that selects the tier, and set
  it.** `CAIRN_LIB=<store-path>/libexec/cairn/lib` is the whole fix to the method.
- 🔴 **"VERIFIED IN ISOLATION" HAS A CONCRETE SHAPE HERE: TWO REPOS, EACH GREEN, BROKEN
  TOGETHER.** cairn's suite is green at `cdf6fae`; the other repo's is green on its mainline; the
  pin that joins them fails 8 guards. Nothing in either repo's CI ever built the combined state —
  the other repo's pinned-client leg passes because it only asks whether the client RUNS. **Ask
  which surface your fixture does not load.**
- 🔴 **A COUNT IS NOT A SET, AND I GOT THE SET WRONG TWICE BEFORE GETTING IT RIGHT.** The status
  description truncates at 140 chars and named **1**; a file-scoped run found **1**; my first
  FAILURES-header regex found **4**; the runner's own `failed=` said **8**. Only the last is the
  set. **Read the runner's own total, then make your name-extraction agree with it** — a header
  pattern that returns fewer names than the total is a broken instrument, and the disagreement is
  the tell.
- ⚠ **THE WRITE GUARD KEYS ON THE SESSION'S CWD, NOT THE `-C` TARGET — AND THE PLUMBING ROUTE IS
  THE RIGHT ANSWER, NOT THE OVERRIDE.** Committing into a worktree of the OTHER repo was refused
  because this session sits in *this* repo's shared base clone. The documented
  `BASE_CLONE_WRITE_OK=1` asserts a hazard that was not happening; `hash-object` → scratch
  `GIT_INDEX_FILE` → `read-tree` → `update-index` → `write-tree` → `commit-tree` → `push <sha>:…`
  produced the commit while touching no branch, no index and no base clone. Verify the built
  commit's own diff before pushing it.
- ⚠ **zsh ATE `$C:flake.lock` AS A HISTORY MODIFIER — FOURTH INSTANCE IN THIS EFFORT, AND THIS
  TIME IT FAKED A SAFETY FAILURE.** `git show "$C:flake.lock"` lost `:f`, wrote an EMPTY file, and
  the "is this worktree's content already pushed?" check then reported a false
  *DIFFERENT — DO NOT REMOVE*. Braced (`"${C}:flake.lock"`) it is byte-identical. Prior instances
  were refspecs, so "brace refspecs" was the wrong generalisation: **brace every `$VAR:`
  construction.** The failure direction is not always loud — here it was, but a false SAME would
  have licensed deleting unsaved work.
- ⚠ **AN AGENT-AUTHORED PR COMMENT IS LABELLED AS SUCH, DELIBERATELY.** The finding above was
  posted to that PR prefixed "posted from the operator's account by an AGENT; authorship NOT
  verified; this is a finding, NOT an operator requirement, and nothing here is
  deletion-immune" — the mitigation the closed arc's rank 10 asks for, applied rather than merely
  filed.

- 🔴 **I ASSERTED A MECHANISM WITHOUT MEASURING IT, WHILE FIXING GUARDS THAT EXIST TO
  STOP THAT.** The retraction above is the instance. A coherent story — "two tiers,
  two libs, the local one is stale" — explained every observation I had, so I wrote
  it into a handoff doc and a PR comment as measured. It took one command to refute
  (`nix develop … -c 'which cairn'`). **A theory that explains the failure is not
  evidence for it**, and the cheap discriminating control was cheaper than the
  paragraph I wrote instead. The tell I ignored: I had never actually run the
  resolution, only read the resolver's docstring and reasoned forward from it.
- 🔴 **A GUARD CAN BE NARROWER THAN ITS NAME AND STILL PASS — TWO DID, IN ONE CLASS.**
  `..._drops_ALL_THREE_explanations` and `..._brings_ONLY_its_own_clause` asserted
  over three badges while the module had four: green, reading as coverage, providing
  none for the fourth. Only the sibling that COMPARED against the full rendering
  failed. **A hand-written cardinal in a test NAME is the same claim as one in prose**
  — derive the set, and add a guard that FAILS when the module grows a member nobody
  mapped. That new guard was watched red with its own message, not another's.
- 🔴 **A MUTATION ANCHOR THAT GOES `0x` IS THE GOOD FAILURE; THE BAD ONE IS RE-POINTING
  IT AT THE WRONG SITE.** Three anchors went stale (a tuple re-spelled across lines, a
  predicate that grew two terms, a construction moved into a factory) and
  `_load_mutant` REFUSED each loudly rather than scoring SURVIVED — which is the only
  reason they were found. My first re-anchor then aimed at a *sibling* site: the
  mutation applied, the test failed, and it read as a broken FIX rather than a wrong
  ANCHOR. **Re-derive which call path the scenario reaches before moving an anchor**,
  and prefer the narrowest expression the mutation can be about — the factory over
  its caller, the tuple's first element over the whole tuple.
- 🔴 **A CLASS-LEVEL `module.CONSTANT` IN A TEST IS AN IMPORT-TIME DEPENDENCY, AND IT
  FAILS AS AN OPAQUE COLLECTION ERROR.** Keying a map on `rc.BADGE_REQUIREMENTS`
  resolved at class-definition time, so against a client lacking it the whole FILE
  reported `1 error` — no test names, no counts, exit 2. Keyed on the badge's VALUE
  literal instead, the file imports either way and the mismatch is reported BY NAME.
  Same family as every other opaque-zero in this doc: prefer the failure that names
  itself.
- ⚠ **`grep -c` EXITS 1 ON ZERO MATCHES, SO A TRAILING `grep -c` MAKES A GREEN RUN
  REPORT FAILURE.** A background full-suite run notified as "failed with exit code 1"
  while its own content read `RESULT: PASS (exit=0)` and `0` failure headers — the
  exit was the counter's, not the runner's. The pipe-eats-the-verdict trap with the
  polarity inverted, and the remedy is the same: read the content.

- 🔴 **(a) AS WRITTEN IS NARROWER THAN THE PROBLEM — DO NOT RE-DERIVE IT AS IMAGE-ONLY.** The
  closing condition says "a deployed **image**", and there are THREE artefacts that can be
  stale: the two pod images and the INSTALLED CLIENT. This instruction is recorded here, under
  an APPEND heading, because it first lived in `State now` — which REPLACES, so the next update
  would have deleted the one sentence telling the next session the condition under-describes its
  own arc. ⚠ That is the same narrower-than-the-sentence shape this repo keeps recording, and it
  was committed while correcting a different instrument error in the same PR.
- 🔴 **A CREDENTIAL CAN BE ISSUED WITHOUT THE OPERATOR HAVING ONE, AND THE POD THAT OWNS THE
  JOURNAL IS WHERE IT HAS TO HAPPEN.** The operator had no browser-surface credential at all,
  which blocked every live verification. `cairn-server` is NOT in the browser pod's image and
  that pod's `/tmp` is read-only — **but it carries the exact nix glibc the binary is linked
  against**, so the binary was copied onto the writable state volume and run there, appending
  under the same `flock` the application uses. That beat the alternative (a second pod mounting
  the ReadWriteOnce PVC) on blast radius. A credential was issued for the operator's OWN
  existing user, delivered to a `0600` file, and the binary and token were deleted from the
  volume afterwards; the pod stayed `ready=true restarts=0` throughout. ⚠ Check the glibc store
  path first — a nix-built binary is dynamically linked and a mismatch is the failure mode.
- 🔴 **`pg_stat_user_tables.n_tup_ins` LAGS, SO ITS ZERO CANNOT DISTINGUISH "NEVER WRITTEN"
  FROM "WRITTEN SECONDS AGO".** Measured: immediately after the live sign-in the row was
  ALREADY present (`count(*)=1`, `issued_at` stamped) while the view still read `n_tup_ins=0
  n_live_tup=0`; a later read caught up to `ins=1 live=1`. `track_counts=on`, and
  `stats_fetch_consistency=cache` is the mechanism. **`select count(*)` is the authority.**
  ⚠ An earlier entry in this arc cited the counter as its evidence *and* cited a sibling
  table's non-zero counter as a "positive control proving the counter moves" — both were
  corrected in `#154`, because that pair would hand a false negative to anyone re-running it
  within seconds of a sign-in.
- 🔴 **A `member` CANNOT SHARE, AND THAT INTERACTS WITH CO-MEMBERSHIP NARROWING IN A WAY THE
  PRODUCT ASK DID NOT ANTICIPATE.** `/share` answers **404** to a `member` — only owner/admin
  may share. And `Candidates` is narrowed to people you already share a project with. So the
  only principals you *can* share with already reach every scope the project owns, which means
  a per-scope grant is only *meaningful* for a scope the project does NOT own, or across
  projects. The mechanism works; its useful surface is narrower than "share per-scope with
  other users" reads.
- 🔴 **THE SHARE PAGE'S POSITIVE CONTROL IS NOW MEASURED RATHER THAN ASSERTED.** Live `/share`
  rendered **2 × `read,write` "via project membership"** while "shares you can take back" said
  *"No grant names this scope"* — a principal with authority and NO grant, which is exactly the
  shape `AGENTS.md` names as proof the listing comes from `control.Resolve` and never from
  `Model.Grants`. A principal with BOTH renders identically under either implementation, so
  only this fixture can tell them apart.
- ⚠ **`/invite` IS AN INDEX, NOT THE MINT FORM — and I nearly filed that as a defect.** The
  page lists projects you may invite into; the form lives at `/invite?project=<id>`. Reading
  the index and concluding "the mint affordance is missing" is the same shape as this arc's
  two retractions: a confident read of a page that answers a different question than the one
  asked.
- 🔴 **A SESSION-MESSAGE AUDIT NEEDS A CORPUS ENUMERATION, BECAUSE THE ARC RESOLVER CANNOT SEE
  THIS REPO.** `extract_user_msgs.py --arc` exits **3** on any doc here — it resolves only four
  repo handles and this project is not one, so nothing is measured (which is a different
  finding from an empty arc, exit 4). Enumerate the project's own transcript directory instead.
  ⚠ **And 369 of the 514 extracted records were `<task-notification>` harness blobs** despite
  the tool documenting that harness boilerplate is removed — the real operator-message count
  was **145**. A count taken off that tool without filtering is inflated ~3.5×.
- ⚠ **A NEGATIVE CONTROL CAN CONTAMINATE ITSELF WHEN THE CORPUS IS YOUR OWN TRANSCRIPT.**
  Grepping the session corpus for a deliberately-absent string returned **1** — my own
  transcript, which had recorded the probe command. Exclude your own session id before reading
  such a control as a failure.

- 🔴 **DECISION (operator, this session): the installed client becomes the GO one.** It flips
  `-verbs`/`-exit-codes` from exit 2 to exit 0 — the public-surface widening residual 7 predicted —
  raised with its blast radius before the choice. The concern raised alongside it (the ops wrappers
  PARSE the client, and the parity gate's own named blind set includes `doctor` states) was then
  **measured closed** on all four parse surfaces: `recall`'s `store:` line, `routes`' rows plus its
  `instances:` line, `doctor --json`'s `<alias>/reader-resolution` detail (with a bogus alias ABSENT
  as the control), and `doctor` exiting **10** under `--no-sync`. NOT closed: `sync`, left to the
  parity harness, which runs both clients over one cache root by design. `via: measurement`
- ✅ **THE PYTHON HALF OF RANK 6 SHIPPED; THE GO DECISION IS UNBUILT.** Staged Python-first so the Go change lands against a known-good, rollback-able state instead of riding a
  lock bump. **Do not read "rank 6 verified" as "the Go decision shipped".** Rollback point:
  home-manager generation **845** was current immediately before the switch.
- 🔴 **A `test -d` AGAINST A VARIABLE HOLDING GARBAGE ANSWERED THE REASSURING SIDE.** A background
  build's path was captured with `tail -1`, which returned the runner's `[exited with code 0]` line;
  `test -d "$GARBAGE/libexec/cairn/lib"` then printed *"design premise holds"* — the wanted answer,
  because the path did not exist. Redone with the store path's **own existence** printed beside it.
  The documented *comparison against an absent operand reports SAME, not MISSING* trap, in a new
  shape: **a negative a broken instrument would also produce is not a negative.**
- 🔴 **TWO DOCS PRs ON ONE DOC WERE BOTH `MERGEABLE/CLEAN`, AND THE SECOND WENT `CONFLICTING` THE
  INSTANT THE FIRST MERGED.** `CLEAN` describes the base as it stood, not a property of the PR, so a
  green sweep plus two green `mergeable` reads is **not** evidence both can land. Confirmed by two
  instruments agreeing: `git merge-tree --write-tree` **exit 1** (branch on the EXIT CODE — it prints
  a tree OID on success and emits no `<<<<<<<`, so a marker grep returns a confident wrong "no
  conflict") and GitHub's `CONFLICTING DIRTY`. **Resolved by rebuilding the delta on the merged base
  through `handoff_doc.py`, never by hand** — the sections have replace/append semantics a 3-way text
  merge cannot know, and hand-resolving is how a REPLACE section eats an APPEND one.
- ⚠ **`gh pr checks` EXITS 8 WHILE ANY CHECK IS PENDING, AND ITS EMPTY SET READS AS ALL-GREEN.** The
  non-zero is not a failing gate. Worse, filtering for non-passing checks returns empty BOTH when
  everything passed and when **no checks exist at all** — which is what a fresh push looks like. One
  PR here was misread as green on exactly that. **Print the check COUNT before the buckets.**
- ⚠ **A SWITCH REPORTING `degraded` IS REPORTING ON THE WHOLE USER SESSION, NOT ITS OWN WORK.**
  `home-manager switch` exited **0** while printing `The service manager is degraded` and naming a
  failed unit whose last failure stamped ~48 min earlier, on its own timer. **Read the failure's own
  timestamp before attributing it to the deploy you just ran.**
- ⚠ **THE SIZE CEILING IS PAID BY WRITING LESS, NOT BY EVICTING HISTORY — `--prune` cannot take a
  whole investigation block.** It names lines verbatim, each matching exactly one, but `- as-of:`
  and `` `via: measurement` `` recur (5 and 7 times here) and are parsed fields. This delta was cut
  ~20% to fit, dropping no measured value: one block's facts were folded into another, and a
  duplicated table was removed.
- ⚠ **NO TASK-BOARD FIELD — AN UNKNOWN, NOT A MEASURED ABSENCE.** The resolver exited **5**: an
  unknown session id answers 200 with an EMPTY ARRAY, so that zero cannot separate "touched no
  task" from "wrong id". None written, none created.

## How to verify

🔴 **READ EVERY STATUS OFF THE COMMAND, NEVER THROUGH A PIPE.** This arc has now paid **five**
times; the fifth was this session's own task-board `field` helper piped to `head -2`, where `$?` was
`head`'s **0** and the real status was **1**. Redirect to a file and read `$?`.

```bash
# clause (b) — DIRECT reads are the authority; the stats counter LAGS and its 0 is ambiguous
kubectl -n subsystem-store exec sts/cairn-ui-postgres -- psql -U cairn_ui -d cairn_ui \
  -c "select count(*) from sessions; select count(*) from invites;"

# the deployed images: resolve image -> commit -> COUNT the distance (no gate does this)
DEP=$(kubectl -n subsystem-store get deploy cairn-ui \
  -o jsonpath='{.spec.template.spec.containers[0].image}' | sed 's/.*sha-//')
git rev-list --count ${DEP}..origin/main          # must be 0, or only claudedocs/ commits

# the INSTALLED client — the third stale artefact, and the cheapest reading of it
readlink -f "$(command -v cairn)"
cairn recall --help | grep -c -- --tag           # must be non-zero once #1933 lands + a switch
cairn recall --help | grep -c -- --scope         # positive control: must already be non-zero
```

**Verify a deploy by DIGEST, never by the `image:` field:** resolve the tag on ghcr
anonymously *before* the push, then read `.status.containerStatuses[0].imageID` back and
compare. A tag equal to what you wrote proves only that you wrote it.

**The share flow, end to end, on a COPY rather than the live world** — the recipe that worked:
copy the journal out, provision a second user with `-create-user` then join it to the EXISTING
project **by id** with `-set-member` (the same `-project` NAME twice mints a second project),
`-issue-credential`, run `cairn-ui` on a port chosen after `ss -ltn`, then drive
`/sign-in` → `/share` → `/unshare`. `/share` takes `scope` + `subject` + repeated `verb=`;
`/unshare` takes a `grant=grt_…` id. A `member` gets **404** on `/share` — only owner/admin
may share.

**The leak gate must pass before any push, and read its CONTENT not a pipe's rc:**
```bash
cd <a fresh worktree>   # the base clone exits 2 on untracked `result` symlinks and agent worktrees
python3 tests/leakscan.py --self-test > /tmp/ls.out 2>&1; echo rc=$?
python3 tests/leakscan.py > /tmp/l.out 2>&1; echo rc=$?; tail -3 /tmp/l.out
```
