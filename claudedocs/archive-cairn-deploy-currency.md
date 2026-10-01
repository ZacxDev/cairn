# Archive: cairn-deploy-currency — closed investigation bodies

🔴 **READ ON DEMAND, THEREFORE FREE.** `handoff-cairn-deploy-currency.md` is read first
every session and pays for every byte it carries; it hit its enforced 65,536 B ceiling with
**10 bytes** to spare. These are the bodies of blocks already RESOLVED / RETIRED /
RETRACTED / superseded, evicted **verbatim** with `handoff_doc.py --prune`.

🔴 **EVICTED IS NOT DELETED, AND THE RETRACTIONS ARE THE REASON.** Three of the blocks below
exist to stop a future session re-deriving a theory that was MEASURED WRONG:

- the trusted-proxy **global-lockout exposure that does not exist** — the allowlisted address
  is the gateway's `cilium_host` source address and appears in no pod's `status.podIP`, so a
  797-row pod enumeration with a working positive control answered a different question than
  the one asked;
- the **`CAIRN_LIB` two-tier split**, asserted as measured and then measured FALSE — both
  tiers read the same lib, and the real cause was a filename-shaped test selection;
- **`pg_stat_user_tables.n_tup_ins` LAGS**, so its zero cannot distinguish "never written"
  from "written seconds ago" — `select count(*)` is the authority.

⚠ **EVERY BODY BELOW IS CLOSED.** Nothing here is a live diagnosis; each keeps its own
`as-of:` stamp. In the handoff, each of these blocks still carries its HEADING and stamp as a
pointer to this file — that is the shape a `--prune` leaves, and it is deliberate.

**How to add to it, precisely — because getting this wrong cost three rounds:**
`--prune` names lines verbatim, each matching **exactly one** line, and it combines with
`--update` so one run can add findings and evict closed ones. A prune naming a `### ` heading
must name **every** line of its block (`[partial block]`), and a whole block therefore cannot
be named, because `- as-of: <date>` and a bare `` `via: measurement` `` recur across blocks by
design (`[ambiguous]`). **So prune the BODY's unique lines and leave the heading as the
pointer** — which is what "move the text to the archive leaving a pointer" means in practice.

---

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


### 🔴 OPEN — this doc is at its enforced ceiling and `--prune` STRUCTURALLY cannot shrink it
- as-of: 2026-09-30
- **Symptom + repro:** any update → `status=size-ratchet`, exit **14**, nothing written. **65,526 B
  against an enforced 65,536 B ceiling — 10 B.** `handoff-audit.py`: **5.3x target**, **18,496 B
  evictable** (15,617 B in 4 resolved blocks).
- 🔴 **Observed — the blocker is the AMBIGUITY rule, not the `as-of:` one.** `LOAD_BEARING_FIELDS`
  *exempts* `as-of:` when a whole block is named, precisely so the largest eviction is takeable — so
  that rule is not it. But every named line must match **exactly one** line, and `- as-of: <date>` /
  `` `via: measurement` `` recur across blocks *by design* (*"Append-verbatim makes duplicates
  ordinary"*). Measured: `- as-of: <a shared date>` **5 matches**, `` `via: measurement` `` **9**,
  `|---|---|---|` **3** → `status=prune-refused`, exit 15. Heading-only is refused too
  (`[partial block]`). No spelling works. `via: measurement`
- **Ruled out:** pruning one block at a time so duplicates become unique — removing one takes
  that stamp from 5 matches to 4, and it reaches 1 only after the other four are gone, the blocked
  step. `via: measurement`
- 🔴 **RETRACTED TWICE, BOTH MINE, IN ONE SESSION.** I claimed this limit from the duplicate lines
  without reading the tool; then read `LOAD_BEARING_FIELDS` and retracted the CONCLUSION too; the
  third step finally ran the command. **Conclusion stands; the first mechanism AND the retraction of
  the conclusion were both wrong.** The lesson is the sequence. `via: code`
- **Leading hypothesis:** structural — `Open investigations` only grows, and its exit rule is
  defeated by its own append-verbatim duplicates.
- **Next probe — operator's call, not mechanical:** (1) `--override-size-ratchet "<reason>"`, whose
  contract requires the reason to state whether an operator approved; or (2) split the arc — clause
  (a) is open, so that is a scope decision. ⚠ `State now` was compacted this round, which buys ONE
  small update, not a fix. Do not spend it on prose.


### ✅ RESOLVED 2026-09-30 — the ceiling is cleared by EVICTION, and this RETRACTS my own "`--prune` cannot shrink it" from one round ago
- as-of: 2026-09-30
- 🔴 **RETRACTED: "`--prune` STRUCTURALLY cannot shrink this doc."** I wrote that here one round
  ago as measured. **It is FALSE, and the doc is ~18.8 KB smaller because it is.** What I measured
  was true; the conclusion I drew from it was too wide.
- **What is actually true, stated so nobody has to re-derive it:**
  - a prune naming a `### ` heading must name **every** line of its block, else `[partial block]`;
  - every named line must match **exactly one** line, else `[ambiguous]` — and `- as-of: <date>`
    plus a bare `` `via: measurement` `` recur across blocks *by design*
    (*"Append-verbatim makes duplicates ordinary"*);
  - therefore a **whole block** genuinely cannot be named — that half was right;
  - **but the BODY's unique lines can**, leaving the heading and its stamp as a pointer. Measured:
    a 28-line body-only prune returned `status=proposed` on the first try.
  `via: measurement`
- **Observed (with values):** 7 closed blocks evicted **verbatim** to
  `claudedocs/archive-cairn-deploy-currency.md` (22,127 B, 7 blocks), 223 lines pruned,
  **~18.8 KB** freed against a doc that had **10 B** of headroom. Each evicted block keeps its
  HEADING and `as-of:` stamp in place as the pointer — that is the shape a prune leaves, and it is
  deliberate rather than residue. `via: measurement`
- 🔴 **Ruled out:** that I found this myself. **A PEER SESSION had already shipped the exit path
  and I nearly landed a doc entry contradicting it.** The config repo's PR #1926 —
  *"the size-ratchet refusal denied the exit rule (q) had just shipped"* — says in as many words
  that the route is *"move the text to the archive leaving a pointer, then `--prune` the lines
  out"* and that **`--prune` combines with `--update`**. I found it only because a `git diff`
  against a moved mainline listed a file I had not written. **The sweep is what saved this, not my
  reasoning.** `via: doc`
- 🔴 **THE REUSABLE PART, AND IT IS ABOUT ME RATHER THAN THE TOOL — THREE ROUNDS ON ONE CLAIM:**
  (1) asserted the limit from duplicate lines without reading the tool; (2) read
  `LOAD_BEARING_FIELDS`, over-corrected, retracted the *conclusion*; (3) ran the command, restored
  the conclusion in a narrower form; (4) a peer's PR showed the conclusion was still too wide and
  the exit existed. **Every step was reasoning where a command was available, and the command was
  one line each time.** The rule this repo already carries — *a theory that explains the
  observation is not evidence for it* — was in scope the whole way.
- **Leading hypothesis:** none. Closed.
- **Next probe:** none for the ceiling. ⚠ **The doc will fill again** — `Open investigations`
  only grows. The route is above; the archive is the destination; do not reach for
  `--override-size-ratchet`, which ships an over-ceiling doc rather than fixing one.


### ✅ RESOLVED 2026-09-30 — the Go flip shipped to both hosts, and the two instruments that failed on the way are rank 2's real subject
- as-of: 2026-09-30
- **What this settles:** rank 6 in full. The ANSWERED block's design is BUILT and DEPLOYED; its
  "Next probe" is spent. Do not re-derive the pin-seam split — it is in `#1939`.
- **Observed (with values), identical on BOTH hosts:** `cairn` → `…-cairn-go-cdf6fae/bin/cairn`,
  `cairn-py` → `…-cairn-cdf6fae/bin/cairn`; `--ref-to`/`--tag` **1/1**, `--scope` **1**, bogus
  flag **0**; `-verbs` **rc 0**; `cairn_pin` → `…-cairn-cdf6fae/libexec/cairn/lib`;
  `cairn-validate` **rc 0**, `cairn-who` **rc 0**. `ship.sh` rc 0, 2 hosts compared at
  `aa01eb77`. `via: measurement`
- 🔴 **Ruled out: that a local green build says anything about CI.** The first `#1939` run went
  RED because I put the Go client in `gateTools`, which backs the devShell AND `checks.pytests`
  — and the config repo's CI pod **cannot sandbox a nix build**: PodSecurity `baseline` blocks the fixes,
  nix silently FALLS BACK to unsandboxed, and `nix config show` still reports `sandbox = true`
  (**the tell is that `/build` does not exist**). `mkGoClient` runs `go vet ./... && go test
  ./...` over 21 packages, which does not pass impure → `cairn-client-runs` FAILED and `pytests`
  reported `BROKEN GATE … before a verdict`. The same derivation builds green here, sandbox on,
  **21 ok / 0 FAIL**. Fix: the Go build is out of that repo's critical path; the hosts still install
  the UNMODIFIED package. `via: measurement`
- 🔴 **Ruled out: that `NO CAPACITY` is a verdict on the diff.** The merged-tree re-gate posted
  `NO CAPACITY: <leg> — the gate never started (queued past its deadline)` on all four legs.
  Timeline: `pending` **17:25:4x** → `error` **18:25:5x**, exactly **60 minutes**, never started.
  Queue was then drained (**5 Running / 1 Pending** against 457 Completed + 57 Error — split by
  phase, because terminal pods are not pressure), so a close/reopen re-trigger (the live CEL
  filter accepts `reopened`) came back **4/4 green: collected=24741 passed=24734 failed=0**,
  which is +4/+4 on the tests `#1926` added. `via: measurement`
- 🔴 **Ruled out: that the drift deadman can be trusted as-is — TWO false readings in one day.**
  (i) exit **12 = `not-on-branch-main`**: TRUE, unread for two days, and it cleared on its own
  when a peer moved the base clone off their branch. (ii) *"did not answer (unreachable)"* for
  the laptop, while a direct `ssh` answered **instantly** and `ship.sh` converged it **twice**.
  **A gate that is red when right and red when wrong trains its own bypass** — which is exactly
  what rank 2 must not add a third of. `via: measurement`
- 🔴 **Ruled out: my own "full local suite" as a gate. It is a DIFFERENT POPULATION and I
  contaminated it.** `pytest scripts/tests` collected **15,904**; CI's pytests leg collected
  **24,741**. And its single failure —
  `test_gate_exit_truthfulness.py::test_a_green_real_run_says_pass_with_exit_zero`, a test my
  branch does not touch (`git diff --name-only origin/main` → 0) — **did not reproduce**: a
  clean re-run is **13 passed / 0 failed**. I had killed pytest PIDs in that worktree while a
  test that spawns a REAL nested run was in flight. A control that shares the step you doubt.
  `via: measurement`
- **Leading hypothesis:** none. Closed.
- **Next probe:** none for rank 6. For rank 2, the probe is the drift deadman's own per-host
  lines (`journalctl --user -u drift-check.service`) BEFORE designing anything new.



### ✅ RESOLVED 2026-09-30 — the deadman is GREEN, both "false readings" were TRUE, and clause (a)'s detector is ruled out by /the-algorithm
- as-of: 2026-09-30
- **What this settles:** rank 2 in full, in the form the prerequisite asked for — *read the
  per-host lines BEFORE designing anything*. Reading them is what killed the design.
- 🔴 **Ruled out: that the deadman was broken. IT HAD ZERO INSTRUMENT DEFECTS.** All four red arms
  were true findings against a backlog nobody had cleared. Before → after, read off the
  instrument's own lines rather than inferred:
  | arm | before | after |
  |---|---|---|
  | rc17 built source | `<laptop>` built-source subtree `NOT current: 5 behind` | `CURRENT (0 behind / 0 ahead)` |
  | rc22 skill tiers | both hosts disagree with the ledger | both `matches the ledger (16 …)` |
  | rc10 behind | both hosts `BEHIND by 1` | `ship.sh` rc 0, 2 hosts at `5fdafe00` |
  | rc15 settings keys | `model skipDangerousModePermissionPrompt` | key sets `AGREE` |
  | `[srcrepo]` | `same=1 differing=1` | `same=2 differing=0` |
  rc17 cleared **on its own** — re-measured at the moment of acting, the subtree distance was
  **0, not the 5** a 5-hour-old reading said; a peer had already pulled it. rc22 took
  `sync-skill-tiers.py --apply` on BOTH hosts (13 → 16 overrides), verified to SURVIVE the
  subsequent switch. `via: measurement`
- 🔴 **Ruled out: that the "unreachable" reading was false.** The probe names **ONE ADDRESS** and
  the **very next line** reports a successful fallback to the host's second address, after which
  the entire remote block is real `<laptop>` data. Positive control: the primary address genuinely
  does not answer `:22`; the second does. **It was a misread of a per-address diagnostic as a
  per-host verdict**, and the instrument printed its own correction immediately. The rc12
  `not-on-branch-main` reading was already conceded TRUE. `via: measurement`
- 🔴 **RETRACTED, MINE, THIS SESSION: "0 successes in 76 firings, never green in 19 days."** I took
  that from `grep -c "Deactivated successfully"` — a pattern I never positive-controlled, on a run
  systemd had just reported `Result=success`. The real marker is `Finished Passive drift deadman`.
  True counts: **78 failures / 4 successes** over a Sep 11–30 journal, last green **Sep 16 — 14
  days, not 19, and not "never"**. The finding survives (a gate red for two weeks, toasting every
  6 hours); the numbers did not. **A reassuring zero from an unvalidated pattern is the same trap
  this doc already records twice.** `via: measurement`
- 🔴 **Ruled out: that clause (a)'s detector is worth building.** Measured, all three artefacts:
  | artefact | pinned at | behind `origin/main` | **code-bearing** behind |
  |---|---|---|---|
  | `cairn-ui` pod | `e8839d9` | 12 | **0** |
  | `subsystem-store-api` pod (Go) | `e8839d9` | 12 | **0** |
  | installed client (= the config repo's `flake.lock`) | `cdf6fae` | 8 | **0** |
  Every one of those commits touches only `claudedocs/`/`*.md`. So a naive `distance == 0`
  instrument is **RED today for zero real drift** — the false positive that trains its own bypass
  — and a correctly path-scoped one is green, needing a deliberately stale pin to go red.
  `via: measurement`
- 🔴 **Ruled out: `x-store-revision` as "a ready-made home" for the instrument. THE GOTCHA BELOW
  SAYING SO IS WRONG.** `X-Store-Revision` is the **scope's own git HEAD**, read off
  `<scope>/.git/HEAD` — it is what lets a report be quoted as `scope@sha`. `unknown` is
  **load-bearing**, not absence: a refused scope MUST answer it, indistinguishable from an absent
  one, with its own positive control. And the health route deliberately reveals no version
  *because it is unauthenticated*; the `Server` banner "must carry no version". Populating it with
  a build revision would hijack pinned semantics and reverse a deliberate disclosure decision.
  `via: code`
- 🔴 **Ruled out: that allowlisting `model` was the cheap path to green.** TWO independent written
  rulings say it is deliberately non-exempt: `drift-check.sh`'s *"WHAT IS NOT ON THIS LIST, ON
  PURPOSE: … `model` — behaviour, not preference"*, and the suite uses `model` as this
  subsystem's **primary negative control** (`test_settings_key_set_divergence_is_rc15`), whose
  docstring warns that using allowlisted keys there "would have turned the subsystem's primary
  negative control green while looking like a test that still fires". Aligning the hosts cost
  **zero code and zero test change**. **My recommendation was an unmeasured analogy to
  `effortLevel`/`voice`/`theme`; the codebase had already argued the opposite.** `via: code`
- **Leading hypothesis:** none. Closed.
- **Next probe:** none for rank 2. For rank 8, the Flux obstacle is the tag ordering — see it.


### ✅ RESOLVED 2026-10-01 — round 0 audited PR #163 and the deletion argument SURVIVED; six findings were mine and are fixed here
- as-of: 2026-10-01
- **What this settles:** whether clause (a)'s deletion argument rests on a sound predicate.
  It does, and more strongly than the doc claimed.
- 🔴 **The predicate HOLDS, and the audit tested it harder than I did.** I re-ran my own
  count; the auditor **enumerated the actual file set** in both ranges and found it entirely
  `claudedocs/*.md` — four files for the pods' range, three for the client's. **No commit in
  either range touches `flake.nix`, `flake.lock`, `go.mod`, `go.sum`, `.github/workflows/**`,
  `server/Dockerfile`, `internal/**`, `cmd/**` or any `Makefile`.** It also checked the one
  way a `*.md` could be code-bearing — a `//go:embed` of markdown — and there is none.
  So "0 code-bearing" is true under any predicate stricter than mine. `via: measurement`
- 🔴 **Ruled out: that `0 code-bearing` means `same image`. IT DOES NOT, AND THE DOC HELD BOTH
  HALVES WITHOUT JOINING THEM.** `ba78dbb..03f912e` diffs **only** this handoff doc, and the
  `cairn-ui` digests differ (`ff292fab…` → `40ac9382…`). The mechanism was already recorded
  here ("the Go binary's nix store path embeds the short rev"). Measured independently twice.
  **Consequence for rank 8, which is why it matters:** enrolling the pins as rank 8 was first
  worded would have produced **12 pod rollouts for 0 code change** in exactly the window used
  to argue no drift exists. `via: measurement`
- 🔴 **Ruled out: that rank 8 needs a new `main-<ts>` tag. `publish-image.yml:78` ALREADY
  MINTS AN ORDERABLE IMMUTABLE TAG** — "plus a bare semver on a version tag push" — which
  fires per RELEASE rather than per commit, a strictly smaller blast radius. A whole cairn-side
  workflow change was designed and is now unnecessary; rank 8 is rewritten to it. `via: code`
- 🔴 **MY OWN REGRESSION, caught by the audit: the "fixed" verify recipe CANNOT RUN.** I
  removed `| sed 's/.*sha-//'` — the very thing that made the old recipe work — while claiming
  to fix two bugs in that block. Measured: `rc=128`, empty stdout,
  `fatal: ambiguous argument 'sha-e8839d91…..origin/main'`. Under the `2>/dev/null` the block
  itself warns about, the capture is empty and the next comment reads `0 = functionally
  current`. **A fix round's own output is the likeliest next defect**, and this one broke the
  instrument the whole arc is about. Fixed in `## How to verify`. `via: measurement`
- 🔴 **Ruled out: "ZERO INSTRUMENT DEFECTS" as stated. IT IS AN OVERCORRECTION.** The doc's own
  remedy is *"Read the line AFTER the alarming one"* — a workaround imposed on every future
  reader for a diagnostic whose lede is reliably misread. Converting an instrument defect into
  a reader defect removes the last case for touching the instrument, and that is a judgement.
  The accurate claim: **no defect in what it MEASURES; a real defect in how its lede READS.**
  `via: code`
- **Ruled out:** that the eviction lost content — but **my denominator was wrong.** Verified
  against the commits: handoff removed **50** non-blank lines, archive added **59**, and **0**
  removed lines are absent from the archive at HEAD. The 59 was the archive's *added* count,
  not the evicted set; the 9-line gap is the heading, the `as-of:` stamp and the residue, which
  stayed in the handoff too. The archive copy is a **superset** — the safe direction — so the
  property holds and the number I quoted was not reproducible. This arc's `#140`/`#149` history
  is *about* eviction counts. `via: measurement`
- **Leading hypothesis:** none. Closed.
- **Next probe:** watch one *scheduled* drift-check firing go green, which no reading here
  covers. For rank 8, the semver path — not a new tag.


