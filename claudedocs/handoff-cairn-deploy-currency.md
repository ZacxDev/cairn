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

- ✅ **RANK 6 IS CLOSED: THE GO CLIENT IS LIVE AND VERIFIED ON BOTH HOSTS.** Operator decision
  (Go), `#1933` + `#1939` merged, `ship.sh` rc 0 with **2 hosts compared, both at `aa01eb77`**.
  Verified on the CONSUMER rather than off the deploy's verdict — identical readings on both
  machines: `cairn` → `…-cairn-go-cdf6fae/bin/cairn`, `cairn-py` → `…-cairn-cdf6fae/bin/cairn`
  (**byte-identical store paths across hosts**), `--ref-to`/`--tag` **1/1** with `--scope` 1 and
  a bogus flag **0** as controls, `-verbs` **rc 0** (was 2), `cairn_pin` route 2 resolving via
  `cairn-py`, and `cairn-validate`/`cairn-who` both **rc 0**.
- 🔴 **CLAUSE (a) IS NOW THE ONLY OPEN ITEM IN THIS ARC** — an instrument that FAILS when a
  deployed artefact is behind. Clause (b) was met earlier. Rank 2 owns it, and rank 2 has grown
  a prerequisite: the currency instrument that already exists gave **two false readings in one
  day**; see the RESOLVED block.
- ✅ **CAIRN HAS 0 OPEN PRs OF MINE.** Merged this session: `#156` (`517efb54`), `#157`
  (`94dc2594`), `#158` (`245b568b`, the ceiling), `#159` (`ba78dbb4`, the P8 constraint).
  ⚠ **`#160` is a PEER's** (`close-the-tag-vocabulary-at-the-write-path`) — opened while this ran,
  not mine, untouched.
- ✅ **THE DOC'S OWN CEILING IS FIXED BY EVICTION, NOT BY AN OVERRIDE.** 65,526 B → 51,374 B
  against 65,536; headroom **10 B → 14,162 B**. Seven closed bodies live verbatim in
  `claudedocs/archive-cairn-deploy-currency.md` (0 of 222 pruned lines absent from it).
  `--override-size-ratchet` was NOT used.
- ⚠ **THE OPERATOR HAS A BROWSER-SURFACE CREDENTIAL AND HAD NONE BEFORE** — their own user,
  issued from inside the pod that owns the journal, in a `0600` file in their client-config
  directory. **The path is deliberately not written here:** this repo is PUBLIC.
- ⚠ **A SECOND USER IS PROVISIONED LIVE AND DELIBERATELY INERT** — `member` on the existing
  project with **no credential**; it only keeps the share picker exercisable. Grant and test
  invitation revoked.
- ✅ **CARRIED FORWARD — a REPLACE heading would drop these values.** Predecessor arcs closed:
  `pytest tests -q` **2357 passed / 0 failed**, `go test ./...` **21 ok / 0 FAIL**, `nix eval`
  resolving `default` and `cairn-go` to the **identical** store path; `#148` merged as `e8839d9`.
  Pod images DIGEST-verified (`ab2a59ab5` → `ccdcd13e4`); a bullet in NO committed file restored
  as `#149`.
- ⚠ **NO TASK-BOARD FIELD: AN UNKNOWN, NOT A MEASURED ABSENCE.** The resolver reported
  **0 tasks**; an unknown session id answers 200 with an EMPTY ARRAY, so that zero cannot
  separate "touched no task" from "wrong id". None written, none created.

## Open investigations — live diagnosis state

### ❌ RETRACTED IN FULL — "the trusted-proxy allowlist is stale, so every caller shares one lockout bucket"
- as-of: 2026-09-29

### ✅ RESOLVED 2026-09-30 — the session store IS written, the invite store IS written, and the share flow is EXERCISED on the live deployment
- as-of: 2026-09-30 · `via: measurement`
  | reading | before | after |
  |---|---|---|
  n_live_tup=0`; a later read caught up to `ins=1 live=1`. `track_counts=on`, and

### ❌ RETIRED 2026-09-30 — superseded by the block above; its probe has been run and its instrument was wrong
### The browser surface's session table has never been written to
- as-of: 2026-09-29
  `via: measurement`

### The INSTALLED client carries none of the three features this repo shipped, and no card, claim or arc owns it — this arc's client-side half
- as-of: 2026-09-29
  `via: measurement`
  `via: measurement`
  `via: measurement`

### SUPERSEDES the "Next probe" on the client-pin block above: the bump was BUILT, it is RED, and the cause is a VOCABULARY SEAM rather than a regression
- as-of: 2026-09-29
  `via: measurement`
  `via: measurement`

### RESOLVED, and it RETRACTS the `CAIRN_LIB` two-tier gotcha this same session wrote two blocks above
- as-of: 2026-09-29
  `via: measurement`

### The client-side half is GREEN on a PR and STALE on this host, and those are different claims
- as-of: 2026-09-30

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
2. **BUILD THE DEPLOYED-ARTEFACT CURRENCY INSTRUMENT — the (a) half, still unbuilt and the
   ONLY thing left in this arc.** It covers THREE artefacts (two pod images, one installed
   client), not the one its wording names. 🔴 **Watch it go RED on a deliberately stale pin
   before believing it**, and do not accept "both pods carry the same tag" — they were equal to
   each other and both stale, twice.
   🔴 **AND THE CURRENCY INSTRUMENT THAT ALREADY EXISTS GAVE TWO FALSE READINGS IN ONE DAY —
   THAT IS THE STRONGEST ARGUMENT AGAINST BUILDING A SECOND ONE BESIDE IT.** The config repo's
   drift deadman: (i) exit **12 = `not-on-branch-main`**, TRUE and unread for two days — and it
   has since cleared on its own because a peer moved the base clone; (ii) *"zach@<laptop> did
   not answer (unreachable)"* while a direct `ssh` answered **instantly** and `ship.sh`
   converged that host twice. A gate that is red when right and red when wrong teaches its own
   bypass. **Unbreak it or stop gating on it; do not add a third check.**
   forcing: regression — the image gap re-opened within minutes of being closed, twice, and no
   gate in any of the three repositories can see any of the three artefacts.
3. ✅ **DONE 2026-09-30 — clause (b) is MET.** Session table `count(*)` 0 → 1 on the live
   deployment, invite store 0 → 1, share flow exercised end to end. Claim
   `cairn-ui-session-store-probe` RELEASED. forcing: user — the operator held the only
   credential that could run it, and that is now moot: one was issued for their own user.
4. **P8 — retire the Python oracle.** Carried over: a real read AND a real write against the
   live pod from **two distinct hosts**, recorded, AND no open defect naming the Go client or
   `packages.default`.
   🔴 **AND P8 NOW HAS A HARD CROSS-REPO PRECONDITION IT DID NOT HAVE, CREATED BY RANK 6 —
   `packages.cairn` CANNOT BE DELETED UNTIL THE CONFIG REPO STOPS IMPORTING THE READER
   MODULES.** The Go flip makes that dependency EXPLICIT rather than incidental: the config repo
   now threads `packages.cairn` under its own name purely for `libexec/cairn/lib`, deploys it as
   a second on-PATH launcher, and points three `CAIRN_LIB=` units at it. **22 files import that
   resolver, with no fallback by construction** — the copies were deleted when it consolidated
   onto the pin — so deleting `packages.cairn` refuses them all at import, including the writer
   and the launcher the `subsystem-index` write protocol names. ⚠ **The 2026-11-01 BACKSTOP is
   what makes this urgent rather than academic: a date-triggered P8 that fires into that state
   breaks the config repo with no warning.** So the ORDER is: the config repo stops importing
   the reader modules → then `packages.cairn` goes. Never the reverse, and the backstop does not
   change the order — it only decides when the question is forced.
   **BACKSTOP: not done by 2026-11-01 ⇒ P8 opens anyway and the residual risk is accepted
   EXPLICITLY, in writing.**
   forcing: deadline — the 2026-11-01 backstop, set by the operator.
5. **Fix the base-clone write guard.** It reproduced a **SEVENTH** and **EIGHTH** time this
   session — once naming the WRONG REPOSITORY (it reported the cairn base clone for a config-repo
   worktree whose `git-common-dir` is that repo's), and once on a `checkout` in a config-repo worktree: it refused
   commits in linked worktrees while naming the base clone, having resolved the repo from `$PWD`
   rather than the command's `-C` target. Premise proved false before every override.
   **Closing condition:** the guard admits a linked worktree AND reads `-C`, with a test that a
   real base-clone write is still refused.
   forcing: gate — a guard whose diagnosis is reliably about the wrong repository trains its own
   bypass, which is the permanently-red-gate failure wearing a different hat.
6. ✅ **DONE 2026-09-30 — THE GO CLIENT IS LIVE AND VERIFIED ON BOTH HOSTS.** Decision (Go)
   made by the operator, `#1933` (pin bump) and `#1939` (the pin-seam split) both merged,
   `ship.sh` converged **2 hosts compared, both at `aa01eb77`**. Verified on the CONSUMER, not
   off the deploy's own verdict: `readlink -f ~/.local/bin/cairn` → `…-cairn-go-cdf6fae/bin/cairn`
   and `cairn-py` → `…-cairn-cdf6fae/bin/cairn`, **byte-identical store paths on both machines**;
   `--ref-to`/`--tag` 1/1 with `--scope` 1 and a bogus flag **0** as controls; `-verbs` **rc 0**
   (was 2) — the visible sign of the flip; `cairn_pin` route 2 resolving through `cairn-py`; and
   `cairn-validate`/`cairn-who` both **rc 0**, which was the actual risk of the split. Claim
   `cairn-deploy-currency-6` RELEASED.
   forcing: user — the operator asked *"how can we validate and start using the new features?"*,
   chose Go when asked, and asked for the laptop shipped.
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

- 🔴 **A CLOSED-BLOCK ARCHIVE EXISTS: `claudedocs/archive-cairn-deploy-currency.md`.** Seven block
  bodies, verbatim, ~22 KB, read on demand. It carries the three retractions whose whole value is
  stopping a re-derivation (the trusted-proxy exposure that does not exist, the `CAIRN_LIB`
  two-tier split measured false, `n_tup_ins` lagging). **Evict to it, never summarise into it.**
  Its header states the exact prune mechanics, because getting them wrong cost three rounds here.
- ⚠ **A PRUNED BLOCK LOOKS LIKE A STUB AND THAT IS CORRECT.** Heading plus `as-of:` and nothing
  else means "the body is in the archive". It is not damage to tidy, and re-adding prose under it
  spends the headroom the eviction just bought.

- ✅ **RANK 6 CLOSED, AND THE STAGING WAS THE POINT.** Python-first (pin bump, verified), then
  Go (pin-seam split, verified) — each independently green, with home-manager generation **845**
  as the rollback point for the first and a clean second switch on top. Bundling them would have
  put a public-surface change and a lock bump in one irreversible step.
- 🔴 **THREE BLAST-RADIUS KEYS, AND I FOUND THEM ONE RED AT A TIME.** Round 1 keyed the test
  target set on the threaded package name → **15 files**. Round 2 added `gateTools`/
  `REQUIRED_TOOLS` → **39**, of which I had missed **24**. Round 3 was CI naming
  `test_the_cairn_home_nix_entries_stay_ADJACENT` in `test_peer_host.py` — a file with no "cairn"
  in its name, keyed on the deploy-line TEXT. **Adding a `home.file` entry has a blast radius
  keyed on POSITION, which no name-based selection can find.** Ask what the change's radius is
  keyed on, then pick the set from THAT — and accept that for a positional change the answer is
  "the full suite", i.e. CI.
- 🔴 **TWO GUARDS FIRED AND BOTH WERE RIGHT; NEITHER WAS RELAXED.** The adjacency pin exists
  because `nix/home.nix`'s `cairn-validate` comment refers to neighbours POSITIONALLY ("the pair
  above", "Read all three lines together") — so `cairn-py` moved BELOW the trio rather than the
  assertion moving. The runtime-shebang ban is about a CLASS: "my fixture is never executed" is
  not an exemption, so the fixture uses `testlib.mockbin.write_exec`.
- 🔴 **I DELETED MY OWN NEW ASSERTION RATHER THAN SHIP A VACUOUS ONE.** The two-client
  byte-identity `validate` check was real when written (`cmp` rc 0, mutation-killed with its own
  message). Once the Go client left that check, both names resolved to the SAME package — it
  would have diffed a binary against itself and passed unconditionally. A green that reads like a
  parity gate and asserts nothing is worse than its absence.
- ⚠ **VERIFYING A BUILT COMMIT'S OWN DIFF BEFORE PUSHING CAUGHT A SILENT 6-FILE LOSS.** The
  plumbing route parents on a sha you name; the worktree's local HEAD never advanced after a
  `push <sha>:<branch>`, so `-p $(rev-parse HEAD)` pointed at the MERGE BASE and the commit
  contained only the newest file. `git diff --stat <parent> <built>` is the step that found it.
- ⚠ **AN EMPTY OVERLAP IS ONLY MEANINGFUL IF BOTH OPERANDS ARE NON-EMPTY.** Deciding whether to
  re-gate after main moved, I printed a header and never `cat`ed the file — `comm -12` then
  reported no overlap because one side was empty. Print both COUNTS first (theirs 4, mine 7) and
  only then read the intersection.
- ⚠ **THE PIPE-EATS-THE-STATUS TRAP LANDED FOUR MORE TIMES THIS SESSION**, in both polarities:
  `rc=$?` after `| tail` reported a *nix build failure* as success, and a trailing
  `grep -c '^FAILED'` reported three *green* runs as "failed with exit code 1". Read the runner's
  own result line; take an exit code off the command itself.

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
