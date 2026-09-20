# Handoff archive: cairn-control-plane — closed investigation blocks

Blocks moved out of `claudedocs/handoff-cairn-control-plane.md` once they were resolved or
superseded. They are kept VERBATIM rather than summarised: the value of a closed block is the
measured values and the eliminations, which is exactly what a summary drops. Nothing here is
live — every block below was closed at the time it was moved, and the live ones stay in the
handoff. This file is read on demand and therefore costs nothing per session.

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


## Moved from the handoff when the cutover completed

Four blocks whose question is ANSWERED — the deploy decision (and the block it
superseded), the publish gate, and the pod-vs-tree divergence the cutover closed.
Kept verbatim: a closed block's value is its measured values and its eliminations.

### 🔴 RANK 1'S REMAINDER: the DEPLOY decision, now dischargeable for the first time
- as-of: 2026-09-18
- **Symptom + exact repro:** not a defect — the half of the cutover that was never startable.
  `AGENTS.md` states the precondition: *"before swapping the deployed image, diff the two for
  what the test cannot read"*, and says the busybox trade is **recorded rather than settled**,
  to be revisited *"with the threat model in front of you"* if the image is ever deployed.
- **Observed (with values):** the diff between the two **Python** builds is done and is in this
  doc. The diff that matters for the cutover — the deployed Python image against
  `packages.server-image-go` — has never been done, because until `bb87cbd` **no Go image
  existed**. That is the sharp version: the precondition was **undischargeable**, not merely
  undischarged. Still open beside it: `publish-image.yml` publishes the Python image, and
  nothing publishes the Go one, so CI now builds an artefact nobody consumes.
  ⚠ **SUPERSEDED — both halves of that last sentence are now wrong.** `publish-image.yml` has
  in fact published NOTHING, ever (7 runs, 7 failures), and a Go publish path is in flight.
  See the CLOSED block and the publish-gate block appended below.
- **Ruled out:** that the Go image cannot be operated. Both documented procedures were exercised
  end to end against a loaded container — `server/seed.sh`'s `tar` push plus its containment
  guard, and `server/README.md`'s `kill -HUP 1` revocation, which reached `token reload: LOADED`.
  PID 1 is the server binary. `via: measurement`
- **Ruled out:** that the port loses SIGHUP reload. `cmd/cairn-server/main.go` calls
  `signal.Notify(signals, syscall.SIGHUP)` and the startup line advertises `reload=SIGHUP`.
  `via: code`
- **Next probe:** 🔴 **RETIRED — DO NOT RUN. This block is CLOSED.** The judgement it asked for
  was made: the diff is done, the busybox call is made, and the operator chose publish AND
  swap. A later reader following this line would re-derive a decision that already has an
  answer. The enumeration instruction it carried survives in the CLOSED block below, which is
  where it now belongs.

### CLOSED — "RANK 1'S REMAINDER: the DEPLOY decision" is discharged; do not re-run its probes
- as-of: 2026-09-19
- **Symptom + exact repro:** not a defect. That block asked for a diff of the deployed image
  against `packages.server-image-go`, and for the busybox call. **Both are now done and the
  operator has decided.** Its "Next probe" line is RETIRED — do not treat it as open work.
- **Observed (with values):** compared as containers at `3c4a1c6`. Deployed (Debian-slim
  base): **128 MB, CPython 3.12 + Perl, 11 setuid/setgid binaries** (`su`, `passwd`, `mount`,
  `umount`, `chsh`, `chfn`, `gpasswd`, `newgrp`, `chage`, `expiry`, `unix_chkpwd`), 360 PATH
  executables, **no** `nc`/`wget`/`curl`. `server-image-go`: **50 MB, ZERO interpreters, ZERO
  setuid**, busybox 1.37.0. Both run as uid 65532 on 8102 and refuse an unconfigured start
  with **exit 78** and the SAME `subsystem-store-api: token file unreadable: …` prefix.
- **Ruled out:** that the cutover widens the busybox surface. `busybox --list` on BOTH nix
  images: **402 applets each, zero difference in either direction** — the Python image CI
  already publishes carries the identical set. `via: measurement`
- **Ruled out:** that the Go image cannot be operated — re-verified at `3c4a1c6` rather than
  taken from the earlier note. `seed.sh`'s `tar -xf -` push landed 9 scopes; PID 1 is the
  server binary; `kill -HUP 1` revocation moved the old token **200 → 401** and the new one
  **401 → 200**; a malformed token file is REFUSED with `NOTHING CHANGED: still serving the 3
  previously loaded identities`. `via: measurement`
- **Ruled out:** that the trusted-proxy path is untested. It was, and the first attempt used
  the WRONG HEADER: `X-Forwarded-For` is deliberately never read (caller-supplied); the server
  keys on **`CF-Connecting-IP`**. With it: single header → **200 on both**, byte-identical
  audit lines; duplicate, malformed and empty → **401 on both**, identical bodies,
  `status=no-client-ip`. `via: measurement`
- **Leading hypothesis:** none — closed. The busybox trade is a **narrowing on both axes**: an
  interpreter subsumes the busybox network set, and `su`/`mount`/`passwd` disappear. Busybox
  stays load-bearing (seeding needs `tar`, revocation needs `sh -c 'kill -HUP 1'`), which is
  why a distroless variant was not pursued.
- **Next probe:** none. Work moved to ranks 1 and 2.

### 🔴 THE POD DOES NOT SERVE THE TREE, AND NO GATE IN THIS REPO CAN SEE IT
- as-of: 2026-09-19
- **Symptom + exact repro:** every byte-identity claim here compares Go against the TREE's
  `server.py`. The pod serves an image built from an OLDER tree, so "byte-identical to the
  oracle" and "byte-identical to production" are different claims and only the first was
  measured. Repro: pull the tag the GitOps manifest pins, extract `/app`, diff against the
  tree.
- **Observed (with values):** deployed `server.py` is **5,421 lines** against the tree's
  **5,473** (+52/−5 over 4 hunks); **all five** `lib/` modules differ, and three tree modules
  (`cairn_doctor.py`, `cairn_instances.py`, `timeouts.py`) are absent from the image. Run as
  containers over one generated world, 39 rows: **13 identical** (every refusal — 401/404/400
  — plus `/healthz`); **2** `/api/v1/snapshot` rows differing in the gzip envelope ONLY, with
  the **uncompressed tar byte-identical at 256,000 B** and the extracted trees identical;
  **24 differing, every one of them solely in the replication-honesty prose.**
  Today's `tests/dualrun/harness.py` on the same world: `targets=361 comparisons=1489
  differences=0`.
- **Ruled out:** that the 24 rows are a Go-vs-Python divergence. The only behavioural change
  in the 52-line `server.py` delta is the `_header_safe` / `seeded=UNREADABLE` handling — the
  operator-authorised oracle exception already recorded in the Gotchas. The rest of the delta
  is extraction scrub. `via: measurement`
- **Ruled out:** that the `host:` line difference was real. It embeds the container hostname;
  pinned identical on both, the line still differs — because it carries the caveat
  parenthetical. The hostname itself was a dimension the first measurement did not name.
  `via: measurement`
- **Leading hypothesis:** the drift is entirely image staleness, and the cutover CORRECTS a
  false claim rather than risking one. Transitive: Go == tree (dualrun, same world), tree !=
  deployed, therefore Go != deployed.
- **Next probe:** none needed for the decision. If a deployed-artefact arm is ever wanted, it
  needs a POSITIVE CONTROL — see the Gotcha below on how the first draft failed.

### The publish gate has never run to completion, and a hand-pushed tag hid it
- as-of: 2026-09-19
- **Symptom + exact repro:** `gh run list --workflow=publish-image.yml` → **7 runs, 7
  failures**, oldest to newest, while `ghcr.io/<owner>/cairn-store` holds a pullable tag.
- **Observed (with values):** every run dies in `pin skopeo to the flake's nixpkgs` with
  `line 4: /nix/store/…-skopeo-1.24.0-man` / `/nix/store/…-skopeo-1.24.0/bin/skopeo: No such
  file or directory` / `##[error]Process completed with exit code 127`, plus
  `##[error]Unable to process file command 'output' successfully` /
  `Invalid format '…/bin/skopeo'`. Reproduced locally: `--print-out-paths` emits the `-man`
  output first, then the real one.
- **Ruled out:** that the workflow published the existing tag. No run exists for `5d048dd`
  (the workflow landed later, in `0d9d3fa`), and every run that did exist failed **before**
  the push step. `via: measurement`
- **Leading hypothesis:** the tag was pushed by hand — its `Env` matches the NIX image, not
  the Dockerfile image the pod runs.
- **Next probe:** land rank 1, then `gh run list --workflow=publish-image.yml` must show a run
  that REACHES the push step. A tag appearing is not that.

