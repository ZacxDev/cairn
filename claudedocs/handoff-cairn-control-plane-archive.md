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

## Gotchas moved out of the handoff when it was pruned at `1659663`

🔴 **VERBATIM, AND MOVED RATHER THAN SUMMARISED.** The handoff had reached 115,483 B against its own 65,536 B guideline, with eight consecutive updates flagging the ceiling and then raising it. These bullets are records of arcs that have CLOSED — P1's two servers, P2's two clients, P3/P4's control plane and identity, the image cutover, the default flip, the positioning pass — and duplicate instances of tripwires the handoff still carries once each. **Nothing here was edited on the way in.** A claim that reads as settled history here is still a claim: if one of these contradicts the code, the code moved and this is the record of what was believed.

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
- **zsh ate `$var:` twice this session** (`$T:tests/…` → `:t`, `$s:AGENTS.md` → `:A`),
  each time returning a confident *wrong* value — a false "0 references", an empty file
  that "parsed OK". **Brace it: `${var}:path`.**
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

- 🔴 **CHECKING A CLAIM BEFORE DISMISSING A GUARD IS WHERE THE DEFECT WAS.** The dismissal
  itself was correct, but verifying the reasoning turned up a **wrong count**: the prune note
  said three blocks moved and four had. It was written while drafting the delta — *before the
  act it describes*. **Derive a count AFTER doing the thing, from the thing**, and when
  correcting one, record the cause rather than only the number.
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
- 🔴 **THE `/audit-pr` BRIEF'S "WHERE TO WORK" AND THIS REPO'S GOTCHA DISAGREE, AND THE REPO IS
  RIGHT — THE BRIEF SAYS IT CONFIDENTLY AND SAYS IT EVERY TIME.** `audit-dispatch.py` prints
  *"Dispatch with `isolation: "worktree"` — the flag worktrees the CWD's repo, and here that is
  the right one."* True about the REPO and wrong about the REF: the flag branches from the DEFAULT
  branch, so an agent sent at an unmerged PR gets a tree of `main` with none of it. Every one of
  the six dispatches across this PR got a hand-built `git worktree add` at the exact sha plus **a
  base check it could fail**. Zero mis-targeted agents.
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

- 🔴 **A GREEN SIX-CHECK CI CERTIFIED FOURTEEN FALSE SENTENCES ACROSS TWO PRs, AND THAT IS THE
  MEASUREMENT, NOT THE COMPLAINT.** #54 ran six audit rounds and #57 three; between them the ladders
  found **fourteen prose defects**, and CI was `leakscan tests go nix parity dualrun` **6/6 green on
  every single one**. The reason is structural and worth stating once: **no test reads the root
  `README.md`, nothing references `CHANGELOG.md`, and no check in `flake.nix` touches either file.**
  When the payload is prose the only instrument is a reader. Do not read a green run on a docs PR as
  coverage of anything but the code it did not change.
- 🔴 **THREE OF THOSE WERE SECURITY CLAIMS IN A PUBLIC README, ALL FALSE IN THE REASSURING
  DIRECTION.** (a) *"attribution … REQUIRED on every write"* — false for `put`/`create`, and it is
  the exact claim `server/server.py:2536` names the README as the place NOT to make: *"what is NOT
  acceptable is CLAIMING otherwise, which is why the claim is scoped to POST in the README"*.
  (b) an **unmitigated-XSS** claim about code that mitigates it — `internal/ui/render.go:126`
  `safeHref` allowlists `{"http://", "https://"}`, so `javascript:` is refused; the README stated
  the hazard and stopped before the mitigation. (c) *"the env fallback is never reached"* —
  contradicted by the binary's own refusal, which NAMES the variable: *"pass --token-file, or set
  $SUBSYSTEM_STORE_TOKEN"*. **Before writing a security sentence about this repo, find the code
  comment that already governs it; three of these had one, forty lines away.**
- 🔴 **I RESTATED A DELIBERATELY-CONSOLIDATED CLAIM TWICE, BOTH TIMES ONE COMMIT AFTER THE
  CONSOLIDATION.** First: a `CHANGELOG.md` recreating the record `0587ace` had just moved to
  `tests/parity/README.md` residual 7, whose commit message says *"no second copy of the record now
  exists to keep true"*. Second: a README paragraph restating what
  `internal/depspolicy/depspolicy.go:3` declares canonical — *"EVERY OTHER SITE POINTS HERE RATHER
  THAN RESTATING IT … six spellings of one claim"* — making a seventh and eighth. **And the
  predicted staleness had already fired inside the same diff**: it said `go.mod` "no longer has an
  EMPTY `require` block" where `go.mod:1` says it had **NO** block, the distinction being what made
  `vendorHash = null` a refusal. **When a doc says it is the canonical site, link. A corrected
  restatement is still a restatement.**
- 🔴 **A NEGATIVE IS THE CLAIM YOU CANNOT CONFIRM BY READING, AND THE SAME ERROR RAN TWO ROUNDS
  DEEP.** #57 round 1 found a documented command that exits **78** — written from `cairn-ui -h`
  output, never run. Round 1's FIX then asserted two negatives — *"not optional off-cluster"*,
  *"the env fallback is never reached"* — derived by reading `authz.LoadTokens`' switch instead of
  executing it. Both measured false: `SUBSYSTEM_STORE_TOKEN_FILE` alone serves, and `-token-file=`
  plus `SUBSYSTEM_STORE_TOKEN` serves. **`-h` listing a flag and an invocation working are different
  claims, and only one is what a reader copies.**
- 🔴 **A UNIFORM FAILURE ACROSS A PROBE IS INDISTINGUISHABLE FROM A FINDING UNTIL YOU READ THE LOG.**
  Measured twice in one session: four `curl`s all returning `000` (the server never started — an
  18-char synthetic token under the 43-char floor) and three `cairn recall` probes all returning
  `rc=3` (the fixture put scopes under `cache/store/<scope>`; the real layout is `cache/<scope>` with
  a `.sync-stamp`). Both looked like results. **Build a positive control into the probe itself** —
  the run that finally worked showed `scope-absent` rc 0 / `scope-empty` rc 0 / `scope-unreadable`
  rc 3, three distinct answers, which is what proves the probe can discriminate at all.
- 🔴 **THE BASE MOVED UNDER BOTH PRs AFTER CI WENT GREEN, AND DISJOINT FILES WERE NOT SAFETY.** #55
  landed mid-run on #54 and #56 mid-run on #57. Neither touched either PR's files and `merge-tree`
  exited 0 both times — but #55 added `cmd/cairn-ui`, `internal/ui` and `internal/depspolicy`, which
  made #54's Layout table incomplete, and made a pre-existing `README.md` forecast ("one renderer,
  three consumers (pod, CLI, **a future UI**)") read as a description of shipped code. **Measured
  with a positive control: `internal/ui` imports `internal/report` NOWHERE** — the page is a SECOND
  renderer in a different medium, and nothing compares the two. Both PRs were re-merged onto the new
  base and CI re-run before merging.
- 🔴 **`gh repo edit` PRINTS NOTHING ON SUCCESS, AND A FALSE CLAIM CAN GO LIVE IN THE REPO
  DESCRIPTION.** The description set early in the session carried *"attribution on every write"* and
  later *"unforgeable per-agent-session"* — both false, both fixed only because an audit round caught
  the README copy and the sweep reached the description. **The description is a public claim with no
  gate, no history and no reviewer. Read it back after every edit, and sweep it when retracting a
  claim from the README.**
- **An unquoted heredoc ran the backticks in a PR comment as command substitution** and silently
  deleted the values from a paragraph reporting a failed probe, leaving *"returned ."*. Caught on
  read-back; repaired, with the repair noted in the comment rather than made quietly. **Use
  `<<'BODY'` for any `gh pr comment` body, and read back what you posted.**
- **Decision (operator, this session): #57's ladder was STOPPED at round 2, not run to a clean
  round.** Round 2 produced a finding that needed fixing, so the findings-keyed rule would have run a
  round 3. It did not. **The round-2 fix commit `73cfb7d` is therefore prose no audit round has
  read** — three invocation forms and four flag→env mappings, each individually measured but never
  adversarially re-read. Stated on the PR so it reads as *unreviewed*, not *reviewed-clean*; those
  are indistinguishable in a merged history.

- 🔴 **CARRIED FORWARD FROM A `State now` THAT THIS UPDATE REPLACES, BECAUSE IT IS A
  DURABLE DECISION AND NOT STATUS: MCP IS HELD.** No MCP server; agents integrate via the
  CLI and the HTTP API. 🔴 **Do not cite the old blocker when revisiting it** — "a
  third-party dependency is a BUILD FAILURE under `vendorHash = null`" stopped being true
  at #55. The hold rests on the operator's call alone. Also in this host's memory as
  `mcp-server-on-hold`. It sat under a REPLACE heading and would have been deleted by this
  very update; moved here, where the bucket appends.
- 🔴 **EVERY LADDER THIS SESSION FOUND REAL DEFECTS IN MY OWN WORK, AND THE DOMINANT SHAPE WAS
  A GUARD REPRODUCING THE DEFECT IT CLOSED.** #63's consolidation missed two of seven sites and
  the defect it was named for was still live at both — **80 failed / 20 passed** when measured.
  #66's echo ledger discovered sites using **the same two literal phrases as the recipe it
  replaced**, so it was blind to a site phrased differently from birth; two such sites were
  already in the tree, one of whose docstrings reads *"this file is the site that sweep missed"*.
- 🔴 **A COUNT IN PROSE WENT WRONG FOUR TIMES IN ONE PR, ONCE WHILE FIXING THE FINDING THAT IT
  WAS WRONG.** "all three" → "FIVE" → six → nine, the last written without measuring. "Site"
  means a file, a call site, or a client-vs-server distinction depending on the reader, so no
  number was checkable. **The total is now absent from prose and the SET is an assertion.**
  Do not re-add a count.
- 🔴 **AN AUDIT CAN BE RIGHT ABOUT THE GAP AND WRONG ABOUT THE FIX.** #63 round 2 proposed a
  one-keyword fix for a host-label divergence; applied and measured, it **still failed** — the
  comparison is child-versus-in-process, so pinning one side swaps one divergence for another.
  Re-verify an audit's proposed fix rather than shipping on the reviewer's authority.
- 🔴 **A PROSE RATIONALE CAN BE FALSIFIED BY `grep` ON ITS OWN FILE.** #66 argued the capture
  harness must be kept because P8 will need it; every invocation in it is `sys.executable` over
  `cairn` and `server/server.py` — exactly what P8 deletes. P8 is its RETIREMENT condition.
- **`$?` AFTER A PIPE bit this session twice**, once while measuring the very gate whose rc
  mattered — `tests/parity/harness.py` read as rc 0 when the true rc was **2**, "could not
  vouch". Capture the rc into a variable before any pipe.
- **`isolation: "worktree"` was avoided for EVERY dispatch, deliberately** — on this repo it
  branches from the DEFAULT branch, so an agent sent to an unmerged branch gets a tree of
  `main` with the change absent. Every audit agent got a hand-built detached worktree at the PR
  head **plus a base check it could fail**. Zero mis-targeted agents across seven dispatches.

## Defect entries closed and moved out of the handoff at the `#75` update

⚠ **VERBATIM, AND MOVED RATHER THAN SUMMARISED.** Each of these had its closing
condition met by a named merge. They are kept because a closed entry still records what
was believed and what closed it — if one contradicts the code, the code moved and this is
the record. The durable LESSONS from the first of them live under `Gotchas` in the
handoff, not here, because that section appends and this file is read on demand.

- ✅ **CLOSED BY `f8a257e` — the byte-guideline entry.** It asked for a prune PR moving answered
  `Gotchas` blocks to the archive and bringing this file under 65,536 B; `f8a257e` did that by
  MOVING 125 of 174 bullets verbatim. 🔴 **IT STAYED 🔴-MARKED AND LIVE-LOOKING FOR TWO UPDATES
  AFTER ITS CONDITION WAS MET, ASSERTING `99,059 B` ABOUT A FILE THAT `f8a257e` HAD ALREADY CUT
  TO 52,228 — off by 46,831 B, at the top of the section ranked work is drawn from.** ⚠ The
  figure quoted is the MERGED size at a named commit, deliberately: the draft before this one
  quoted the size of its own parent, which every further edit falsified — the previous version of
  this entry carried a warning saying exactly that, the warning was deleted with the entry, and
  the failure it named was re-committed within the same round. **A self-referential byte count is
  stale on arrival; quote a commit's.** The mechanism is worth more than
  the entry: this section REPLACES, so a bullet survives every update that does not retype it,
  and the prune deliberately did not touch `Defects`. **A closing condition met by a PR closes
  NOTHING until somebody edits this section**, and the update that measures the file is the one
  holding the evidence — so close it THERE, in the same change, or it reads as open forever.
  ⚠ **The guideline itself has NO author of record and NO instrument.** Measured: no test and no
  nix check reads `claudedocs/handoff-*.md` at all, and `AGENTS.md`'s budget —
  `tests/test_agent_instructions_weight.py` — covers `AGENTS.md`/`CLAUDE.md` and has no analogue
  here. ⚠ **But "65,536 appears nowhere else in the tree" was FALSE at the spelling a reader
  greps**: `65536` is in three other files as a socket buffer size, and only the comma'd form is
  doc-exclusive. The conclusion survives the correction; the supporting measurement did not, in
  the same round that filed "grep for the number before batching a count as prose". Report the
  guideline as a self-imposed target, never as a gate.
- ✅ **CLOSED BY #48's SESSION** (relabelled — this line previously read "this session" and now names
  which): residual 8 in all four clauses; the `search --all-scopes` fan-out's measured-zero coverage;
  routed `validate`'s oracle divergence; two unconditional-label mutants on `defaultInstance` and
  `bannerFor`; `tests/routing_mutants.py` scoring a never-run suite as KILLED; two CI floors with
  silent slack; the flake wiring pinned by prose only.
- ✅ **CLOSED BY #53:** issue #51, the `AGENTS.md` reverted-draft flip history.

## Gotchas moved out of the handoff at the SECOND prune

🔴 **VERBATIM, AND MOVED RATHER THAN SUMMARISED — the same rule as the first prune above, applied seven days later.** The section had gone from 49 survivors to **147 bullets and 84,556 B**, 62% of a 136,100 B document, while every other section combined would have fitted inside the 65,536 B guideline on its own. **70 bullets moved, nothing was edited on the way in, and nothing was shortened.** What moved is a record of a round, a PR or an arc that has CLOSED — P1's seed-stamp decision, the #38/#41 attribution ladders, #64's six-round share-flow ladder, the rename arc, P7's ETag, the #85 sweep, the #96/#97 doc races, #101/#102's fact-ledger arc, and the deploy decisions the deploy discharged — plus every duplicate instance of a tripwire the handoff still carries once each. A claim that reads as settled history here is still a claim: if one of these contradicts the code, the code moved and this is the record of what was believed.

**The header paragraph the second prune replaced, verbatim, because it is the first prune's own record of itself:**

> 🔴 **THIS SECTION WAS PRUNED, AND PRUNED MEANS *MOVED*: THE PRUNE DELETED AND SHORTENED NOTHING.** ⚠ One of the 49 survivors has since been superseded by a fuller instance in this same section and removed, so a re-run of the prune's own verification now reconciles to 173 rather than 174 — the CLAIM survived, the bullet did not, and those are different statements.** It held 174 bullets and 89,588 B — 78% of a document read first thing every session. 125 of them are now in `claudedocs/handoff-cairn-control-plane-archive.md`, **verbatim**, under a heading that says which arc they came from. What stayed is what binds a NEXT edit: one instance of each general tripwire, the standing operator decisions, and the current arc's record. What moved is a record of a round or an arc that has CLOSED, plus every duplicate instance of a tripwire kept here — `isolation: "worktree"` alone was recorded six times, `$?`-after-a-pipe three, MERGEABLE-but-conflicting four.
> 
> ⚠ **A DUPLICATE IS NOT A REDUNDANCY WHEN THE SECOND ONE RECORDS THAT THE LESSON WAS READ AND THEN HIT ANYWAY** — that is why the instance kept is usually the LATEST, which carries the re-occurrence, rather than the first, which carries only the discovery.

- **Decision (operator, this session):** an unencodable seed stamp resolves to
  `seeded=UNREADABLE` on **both** sides — an authorised exception to P1's "do not change
  the oracle", because a contract cannot include "sometimes truncate the response
  mid-stream". Regenerating all 98 goldens after it moved **none**.
- 🔴 **THE MUTATION BATTERY'S OWN ROWS ARE AS WRONG-ABLE AS THE CODE, AND FOUR WERE.** Across
  three rounds: a row naming a killer that NEVER RUNS (the named test reached the function by
  a different path); two patterns that failed to apply so the mutant died at the BUILD rather
  than at a guard; and an `EQUIVALENT` label whose stated reason — "a window between two
  adjacent statements no gate can open from outside" — was FALSE, because a caller-injected
  `clock()` sat between them. That last one is the worst shape: a label that reads as
  coverage while providing none, forecloses the test that would close the gap, and sits
  inside the battery built to refuse exactly that.
- 🔴 **`$?` AFTER A PIPE IS THE LAST COMMAND'S STATUS.** `python3 tests/leakscan.py | tail`
  returns `tail`'s 0 for a scanner that exited **2**. Combined with the worktree defect
  above — failure on stderr, stdout ending in a wall of `PASS` — a piped read looks clean.
  Capture the rc before any pipe, and read stderr.
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
- 🔴 **VERIFY A SQUASH MERGE BY CONTENT, NEVER BY ANCESTRY — carried here from `State now`,
  which is a REPLACE section, so it would otherwise have been deleted by this very update.**
  `git merge-base --is-ancestor <branch-head> origin/main` returns **false** after every
  squash merge, forever, and reads as "not merged — redo the work". Measured on #35:
  ancestry false, while `git diff e708cba origin/main -- internal/identity/` was **empty** and
  `main` carried the new symbol. ⚠ **A WHOLE-TREE diff is NOT that check** — it showed 4 files
  / 930 insertions, which were another PR's files the branch never had. Diff the PAYLOAD
  PATHS, and separately confirm the merge commit exists.
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
- ⚠ **A worktree created with `git worktree add -b <branch> origin/main` has upstream
  `origin/main`, so a bare `git push` there TARGETS MAIN.** Mine was refused only because
  `push.default=simple` requires the names to match — luck, not design. Push with an explicit
  refspec (`git push origin <branch>:<branch>`) and fix the upstream immediately.
- ⚠ **`xargs -0 command grep` FAILS** (`command` is a builtin xargs cannot exec) with 127 and
  empty output — indistinguishable from a clean zero. Use plain `grep` under `xargs`: it execs the
  binary, so this host's `.gitignore`-honouring `grep` FUNCTION never applies.
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
- 🔴 **AND THEN IT CAUGHT THE *NEXT* HANDOFF DELTA, AFTER THE PUSH — THIRD EVENT OF THE SAME
  CLASS, WITH THE BULLET DIRECTLY ABOVE ALREADY WRITTEN.** ⚠ **Not the same identifier**, and
  the distinction matters because it is what rules out "one remedy already covers this": the
  first was an external tool's name, the second a repository name, both `denied-identifier`.
  🔴 **The failure is a SEQUENCING one: `handoff_doc.py --confirm --push` commits and pushes in
  ONE call, so there is no moment between them to scan in.** Scanning "the pushed tree" is
  scanning after the mistake is public.
- 🔴 **AND THE REMEDY I FIRST WROTE FOR THAT WAS A THIRD PHRASING OF ADVICE THAT HAD ALREADY
  FAILED TWICE — RECORDED BECAUSE THE REACH FOR A BETTER WORDING IS THE ERROR, NOT THE WORDING.**
  It said "scan the SCRATCH DELTA, before the tool is invoked at all". The bullet above it said
  the same thing in different words, and the leak happened anyway. **There is no mechanism:
  `~/.claude/skills/handoff/` contains no reference to `leakscan` at all, so nothing runs it and
  nothing refuses on it.** A guard spelled as a sentence is walkable by forgetting, and three
  events is enough evidence that it is being forgotten. **The deterministic fix is one line in
  `handoff_doc.py`: run `leakscan` on the delta and refuse the write on rc≠0** — filed as ranked
  work, in the repo that owns that script rather than this one. Until it exists, the honest
  statement is that this hazard is UNGUARDED, not that it is handled.
- ⚠ **`leakscan` EXIT 2 IS "COULD NOT VOUCH", AND A LEFTOVER AGENT WORKTREE CAUSES IT.** A removed
  agent's worktree directory under `.claude/worktrees/` made the scanner exit 2 with
  `COULD NOT READ … Is a directory`. Not a leak and not a pass. Check the worktree is clean and
  its commits are on `origin` **before** removing it — then re-run for a real verdict.
- 🔴 **MY OWN GREP WAS THE WRONG INSTRUMENT THREE TIMES IN ONE SESSION, AND THE THIRD ONE ALMOST
  REOPENED A CLOSED FINDING.** (a) Grepping parity row names for `multi-instance` found none and read
  as an unsupported claim — the rows are named `recall-routed-…`. (b) A crude comment filter reported
  7 "non-comment" lines in an all-docstring diff. (c) Grepping for a retracted sentence counted the
  **quoted retraction** — the repo's house style of recording the old wording so nobody re-derives it
  — as a live claim. **A zero, or a hit, from a pattern you chose is a fact about the pattern. Read
  the match before believing the count.**
- 🔴 **FOUR NEW DEFECT ENTRIES, PARKED HERE BECAUSE THE `Defects` HEADING REPLACES AND WOULD
  HAVE DELETED THIRTEEN.** Move them under ranked item 6.
  **(a)** `tests/unchanged_output_capture.py` is run by NO CI job and no nix check — measured,
  zero references in `.github/workflows/` and `flake.nix`. #66 decided it stays MANUAL with a
  written trigger; a blanket job would be red by construction, since it is a base-vs-head
  differential. **(b)** The canonical handoff is ~99 KB against its own 65 KB guideline and
  does not record #60, #63 or #66; its prune PR is blocked behind #62 and #65. **(c)** Two
  entries #66 deliberately did not answer because #64 owns their files — the
  `checks.default-is-the-go-client` Python-side insensitivity (`flake.nix`) and 2(d), whether
  `cairn-ui` should render through `internal/report` (`internal/ui/README.md`); 2(d) is likely
  answered by #64 itself. **(d)** Raw environment copies remain in five NON-consumer files
  (`tests/test_subsystem_store_api.py`, `tests/routing_mutants.py`, `tests/parity/world.py`,
  `tests/dualrun/harness.py`, `tests/conformance/oracle.py`); most build a SERVER environment,
  which is a different predicate, and none was audited. Stated in `env_pin.py`'s docstring.
- 🔴 **A GUARD I WIDENED TO ACCOMMODATE A NEW FEATURE STOPPED GUARDING, AND ONLY A MUTANT
  FOUND IT.** `TestEveryContentRouteConsultsTheAuthority` is a regression test for a shipped
  defect (a page rendered without consulting the authority). Adding a second authority, the
  natural edit was `source.calls == 0` → `source.calls + sharing.reads == 0` — and **a sum is
  satisfiable by the WRONG authority**: re-applying the original defect with a call to the
  other one PASSES. Fixed with a per-route expectation where a content route missing from
  the map FAILS. 🔴 **THE GENERAL SHAPE: when you add a second way to do the thing a guard
  watches, the guard is part of the change, and widening it is the failure mode.**
- 🔴 **AN `EQUIVALENT` MUTATION LABEL IS A CLAIM ABOUT *EVERY* OBSERVABLE, AND ONE MADE HERE
  WAS FALSE.** Two authority checks looked redundant ("removing either leaves the other
  answering 403"). The discriminator missed: a request with **no verb field** answers 403
  unmutated and **400** with the handler's check removed, because that check runs BEFORE form
  validation — so an unauthorised caller learns their input was malformed. **An EQUIVALENT
  label is precisely what stops anybody writing the test that kills the mutant**, which is
  why the retracted reason is kept beside the row rather than tidied away.
- 🔴 **A GUARD'S OWN PRESCRIBED REMEDY PRODUCED THE STATE IT REFUSES.** The control-journal
  refusal said "seed it with `cairn-server -create-user`" — which mints a user and **no
  credential**. A journal seeded exactly as instructed had 1 user, 0 credentials, and the
  surface came up announcing `sharing writable` while refusing every sign-in. **Third
  spelling of that guard: a byte count (walked around by a one-byte journal), a user count
  (walked around by the remedy), then the sign-in precondition itself.** 🔴 **A PROXY CAN
  ALWAYS BE WALKED AROUND — ask the question the code asks.**
- 🔴 **AN OPERATOR-FACING REMEDY CAN INVITE A SECRET INTO A DURABLE STORE.** "Hand-append a
  `credential-issued` record" named neither the shape nor that `token_hash` is the SHA-256
  digest. `Event.validate` checked that field's **LENGTH ONLY** at the time — under a message
  reading "this journal is not a place a credential may ever land" — so a 64-character token
  was accepted and persisted, and sign-in then failed on a digest mismatch with no signal why.
  **When you tell somebody to hand-write a record, name the field that holds a digest.**
  ✅ The check now requires hex, in either case, and `Model.apply` lowercases what it stores;
  the tense here is past deliberately, because the lesson is the remedy's shape rather than
  the guard's state.
- 🔴 **A COMMENT THAT WOULD HAVE INSTRUCTED THE NEXT MAINTAINER TO UNDO THE GATE IT
  EXPLAINS.** A commit moved the `go` job's `ok` floor to `-lt 18` and left three claims at
  17 beside it, including in bold *"So: `ok < 17` refuses. 17 passes (today's tree)."*
  Somebody whose deletion failed the gate reads that, concludes it is misconfigured and sets
  it back — restoring the off-by-one the block exists to record. **Move every number in a
  gate's explanation with its condition, in one commit; the explanation is the thing people
  act on.**
- 🔴 **FIVE ROUNDS, AND THE FIX ROUND'S OWN PROSE WAS THE NEXT FINDING EVERY SINGLE TIME.**
  Measured across the ladder: rounds 2–5 each found the previous round's sentences wrong —
  four unswept retractions, a completeness claim ("the last copy standing") that was itself
  false, a cost conclusion that **contradicted its own numbers**, and a per-round payload
  figure off by one in the very commit message arguing the ladder had converged. **Budget
  for it; the correction is cheap and the belief is not.**
- ⚠ **A COST MODEL RETIRED ON EVIDENCE THAT CONFIRMED IT.** A round replaced
  `O(P × G × log G)` with four timings and concluded the cost was "worse than the expression
  implies … quadratic and not the log-linear the old expression reads as". `P × G × log G`
  **is** quadratic when both parameters grow together; the model predicted 4.49× against
  4.47× measured. The conclusion came from reading a two-parameter expression as if one were
  fixed. `BenchmarkAudience` now makes the table re-derivable, which is what the round should
  have committed in the first place.
- **Decision (operator, this session): land both halves of the share flow as-is**, rather
  than dropping the unreachable write half. Round 0 argued the write half is unattributed and
  unreachable; a probe showed the READ half is equally journal-gated, so dropping it buys a
  smaller diff and no reachability.
- **Decision (operator, this session): keep `Candidates`' co-membership narrowing** — you can
  only share with people you already share a project with — with the invite flow filed as P6.
- 🔴 **THE SHARE FLOW'S SIX-ROUND AUDIT LADDER, RECORDED HERE RATHER THAN IN `State now`
  BECAUSE THAT HEADING REPLACES AND THIS IS THE DURABLE HALF.** #64 carried round 0
  (requirements/deletion) plus rounds 1–5, each dispatched BLIND against a hand-built
  worktree at the PR head — `isolation: "worktree"` branches from the DEFAULT branch here and
  would have handed every auditor a tree of `main` without the package under audit. What it
  found, in order: **round 0** a 234-line mutation battery **no gate ran**; **round 1** a
  regression guard the PR had **widened until it stopped guarding** (re-applying the original
  defect PASSED it) and an `EQUIVALENT` label **measured false**; **round 2** four retractions
  written at one site each by the commit whose own message said a retraction is a tree-wide
  sweep; **round 3** the control-journal guard's **own prescribed remedy producing the state
  it refuses**; **round 4** a CI comment that would have instructed the next maintainer to
  undo the floor it had just moved; **round 5** a remedy that **invited a secret into the
  authority journal**. 🔴 **THE STOP WAS ARGUED, NOT TRIGGERED.** Executable payload per
  round — `--remerge-diff --not origin/main`, comments stripped — was **15 → 22 → 31 → 4 → 7**
  against **722** in the PR proper. The attribution gate's two-consecutive-zeroes never fired
  and this was not an all-prose PR, so neither mechanism ended it; it was stopped on the
  stated grounds that the ladder had left the PR, with the auditor concurring unprompted.
  **Every round's findings are on the PR as comments, which is the record.**
- 🔴 **A DOC PR I WROTE PREDICTED ITS OWN STALENESS AND I MERGED THE FEATURE FIRST ANYWAY —
  ON PURPOSE, AND THE ORDER IS THE POINT.** #71 said "still 3 of 4 on `main`", which merging
  #64 made false within the minute. Writing the handoff first and merging second would have
  shipped a doc that was wrong on arrival; merging first and re-deriving the delta costs one
  rebase and produces a doc whose verdict was MEASURED rather than predicted. **When a doc's
  claim is about a state your next action changes, take the action first.**
- **Decision (operator, this session): the credential gap is FILED, not built.** A
  credential-issuing command carries its own decisions — token generation, display-once,
  revocation UX — and the one hazard worth naming is that it is the single path where a
  secret could reach the journal. It is ranked work rather than a bolt-on to the share flow.
- ✅ **THE CONTROL-PLANE ARC CLOSED AT `7d7c9ea`, AND THE VERDICT IS RECORDED HERE BECAUSE
  `State now` REPLACES AND THIS IS THE DURABLE HALF.** The condition — frozen at round 1, four
  clauses, naming the three commands a later session runs — was measured **on `main`** rather
  than inferred from the PR's six green checks: authz matrix PASS · share flow serving a scope
  granted from one user to another with its replica-honesty notice pinned PASS · identity
  through both backends PASS · `packages.default` the Go client PASS · `pytest tests -q` 1997
  passed / 0 failed · `go test ./...` 18 ok. **ADDRESSED ⇒ ARC CLOSED.** 🔴 **THE "ON `main`"
  WORDING IS THE WHOLE POINT AND IT COST ONE EXTRA STEP TO HONOUR**: green CI is a claim about
  a branch, the clause asked about `main`, and they agreed here only because they were both
  measured. A session that reported "addressed" off the PR's rollup would have been asserting.
- 🔴 **MY OWN MEASUREMENT OF THAT COUNT WAS WRONG FIRST, AND IT WOULD HAVE HAD ME "CORRECT"
  NINE TO NINE.** `go run ./cmd/cairn -verbs | tail -n +2 | grep -c .` answered **9** — the
  `tail -n +2` assumed a header row that the command does not print, and silently dropped
  `append`. Reading the RAW output answered ten, and argparse agreed. **Parsing a tool's
  output makes its FORMAT a dependency you did not pin**, and the failure here was a
  confident number that matched the stale prose.
- 🔴 **DELETING DEAD CODE REPRODUCED THE FINDING INSIDE ITS OWN FIX.** #44's finding was that
  `normalise_shell` was dead *and* that a docstring still pointed at it. Deleting the function
  left TWO docstrings pointing at it — the same defect, freshly made, in the commit closing it.
  **After removing a symbol, grep its NAME, not just its call sites.**
- 🔴 **A GUARD'S MESSAGE CLAIMED EVERY STATE WHILE ITS BODY COUNTED ROWS, AND THE STATE IT
  MISSED WAS THE ONE THAT BREAKS THE NAIVE SPELLING.** `internal/client`'s `doctor` fixture
  renders `OK`, `UNMEASURED` and `NOT-OBSERVABLE` — never `PROBLEM`, whose marker `🔴` is a
  SINGLE RUNE where the other three are two. Closed with two guards rather than one: the
  fixture's states are a ledger that fails if it SHRINKS, and a separate unit case drives
  `doctor.ParseRow` over every state. ⚠ **`ParseRow` LANDED ON `main` AT `93d0f03`**, replacing the
  exported `Markers()` table; this bullet carried a "pending #74" label for exactly one merge,
  which is the point — a durable bullet naming a symbol one unmerged PR away greps to zero hits
  with nothing to tell a reader whether it was renamed or reverted. **When a fixture cannot reach a case, the honest fix is
  a second guard, not a wider sentence.**
- 🔴 **A PR BODY IS A CLAIM, AND THE CLAIM WAS ABOUT A DESIGN THAT NO LONGER SHIPPED.** #1854's
  body carried a watched end-to-end run, a red/green matrix and a mutation table — all taken
  against its FIRST shape, whose differential attribution machinery its third commit **deleted**
  in response to an operator decision. An audit-driven redesign resets the verification gate, so
  the evidence in the body was about code that had been removed. **Re-watch after a redesign;
  the body's confidence is the strongest reason to, not a reason not to.**
- 🔴 **GATE ON THE MERGED TREE, NOT THE PR BRANCH — AND THE TELL IS THAT THE BASE MOVED, NOT
  THAT ANYTHING OVERLAPPED.** #1854's four tekton checks were green on its own branch while
  `main` had advanced two commits. `git merge-tree --write-tree` exited **0** (branch on the
  EXIT CODE — it prints a tree OID and emits no conflict markers, so a marker grep finds
  nothing either way), and the merged tree then ran **575 passed / 0 failed**. Green on the
  branch was a claim about a tree nobody was going to ship.
- ⚠ **RULE (m) FIRES BEFORE RULE (o), WHICH COSTS A ROUND WHEN PROBING THE LEAK GATE.** A probe
  delta whose `## Goal` carried a prose closing condition rather than the
  `closing-condition:` key exited **11** `undefined-done` and never reached the scanner.
  Harmless and loud — but a probe of a LATER gate has to satisfy every earlier one first.
- 🔴 **RETRACTING A TRUE CLAIM IS THE SAME DEFECT AS LEAVING A FALSE ONE, POINTED THE OTHER WAY —
  AND #85 DID IT TWICE.** Once claiming `flake.nix` *"never said"* the Go image was undeployed
  (it did, at the `server-image-go` block, a site the same commit had missed while editing that
  file); once retracting *"neither had anything watching its condition"*, which was true.
- 🔴 **A CORRECTION APPLIED AT SOME OF ITS SITES READS COMPLETE AT WHICHEVER ONE YOU LAND ON.**
  The dominant failure across #85's four audit rounds. Measured: the publish-ordering rationale
  had **SIX** sites; round 1 fixed three and reported it done; round 2 found the other three —
  one of them the copy a *"See the 🔴 at the top of this file"* pointer routes readers to, in a
  file round 1 had edited four lines below that pointer.
- 🔴 **A GUARD'S STATED PREMISE CAN DIE WHILE ITS ASSERTION STAYS CORRECT — ONLY THE PROSE TELLS
  YOU.** `test_flake_go_image_runtime_contract.py`'s *"Nothing has ever published
  `server-image-go`"*, and `test_publish_workflow.py`'s ordering rationale, whose **both** legs
  are spent. The first guard's line is still worth keeping; the second now enforces the opposite
  of what it argues for (rank 10).
- 🔴 **A NOTICE THAT LICENSES WORK IS ITSELF WORK: RETIRE IT IN THE COMMIT THAT DOES THE WORK.**
  #81 recorded that the tree was deliberately left inconsistent pending an operator decision;
  #85 swept it on that decision and left the notice standing — while `AGENTS.md`, paid by every
  session, points at that exact block.
- ⚠ **A PROSE-PAYLOAD LADDER CANNOT FIRE THE ATTRIBUTION GATE** — the `.md` IS the payload, so
  every round scores non-zero by construction. Use the ladder-authored pre-image share instead,
  and **record the measurement**: #81's ladder stopped at **16/16 = 1.00** (threshold 2/3);
  #85's round 2 measured **8/28 = 0.29**, so that ladder had NOT left the PR and continuing was
  the correct call rather than a judgement.
- 🔴 **AND THE SAME MERGE LEFT A TREE THAT DID NOT COMPILE, FROM FILES THAT NEVER CONFLICTED.**
  *(Also re-derived from #80.)* The rename replaced an env helper; the new command's call site —
  added on the other branch, in a hunk the rename never touched — still called the old name.
  **Disjoint files are not safety: one side widened how something is read, the other added a
  caller.** The mutation battery said so first, by REFUSING TO VOUCH: *"the unedited copy is not
  green, so every mutant below would score KILLED for a reason that has nothing to do with its
  guard."* A non-compiling tree would otherwise have reported a perfect score.
- 🔴 **MY PROBE REVOKED THE SESSION IT WAS ABOUT TO TEST, AND I FILED THE 401 AS AN INSTRUMENT
  QUIRK — THE RETRACTED DIAGNOSIS IS KEPT HERE BECAUSE IT WOULD HAVE TAUGHT THE NEXT READER TO
  PAPER OVER A REAL REVOCATION.** ❌ **RETRACTED:** *"the cookie is `__Host-…; Secure` and curl
  will not send a `Secure` cookie over `http://`, so a post-sign-in 401 is the instrument, not
  the server; force the header."* **Measured false in both directions.** curl 8.21.0 treats
  loopback as a secure context exactly as a browser does: sign in, `GET /` with the jar → **200**.
  The positive control that the Secure rule *can* withhold (a `secure` cookie for a non-loopback
  host over `http://`) was watched to fire, so the instrument was working.
  ✅ **THE REAL MECHANISM IS A SECURITY GUARD DOING ITS JOB.** `internal/ui/session.go` revokes
  the presented session before minting the new one — session-fixation defence, and the code says
  so. My chain re-ran `POST /sign-in` mid-probe with `-b <jar>` to read a `Location`, and threw
  the replacement cookie away with `-c /dev/null`; that call **revoked the jar's own session**,
  so the next `GET /` was a correct 401. Isolated: a second sign-in **without** presenting the
  cookie leaves the first session at 200; **presenting** it takes the first session to 401.
  🔴 **THE LESSON IS THE CONTROL I DID NOT RUN.** A 401 is the observable the most mechanisms
  share, and I picked the one I already suspected. The forced-header "control" changed **two**
  variables — it also re-signed-in, so it minted a fresh session; it could never have
  discriminated. **Never re-run an authenticating request inside a chain you are measuring**, and
  when an auth probe fails, suspect your own previous request before the server.
- 🔴 **THE TEN BULLETS BELOW ARE RE-DERIVED FROM #96, WHICH LOST THE DOC RACE THREE TIMES.**
  They are its session's own records, moved into `Gotchas` (which APPENDS) rather than into
  `State now` (which REPLACES) because every one of them is a record of a round that has
  CLOSED, and that is what this document's own convention says to do with those. ⚠ **Six of
  #96's bullets were deliberately NOT carried and the reason is per-bullet, not editorial:**
  its `main` sha, its rank-9 status and its `leakscan` instance-naming are all falsified by
  the work above; its `SIX OF ELEVEN` event-kind count is superseded by the FOUR-of-eleven
  entry with a selection rule under `Defects`; its `leakscan` directory entry is superseded by
  the widened class there; and its *"the cheap control settled it"* bullet is **retracted**
  above, so re-landing it would reinstate a conclusion this file now measures as wrong.
- ✅ **THE RENAME ARC (rank 7) IS DONE AND VERIFIED ON `main`, CLAUSE BY CLAUSE.**
  `56cc56e`. Measured live, not recalled: **11** alias pairs in `internal/envalias`; every
  configuration read goes through the resolver (the only two raw reads are deliberate —
  `cairn-ui`'s control journal, which must SEE whitespace to refuse it, and
  `lib/host_identity.py`'s host-label names, which have no aliases); the warning is
  *"$X is a deprecated alias for $Y. Where both are set in the environment, $Y is the one that
  is read."*; and `RemovalAnchor` = *"the Python client (packages.cairn) is retired"* — the
  milestone the operator approved in place of a version, since this repo's version IS the git
  revision (`flake.nix`: `self.shortRev`) and `leakscan` refuses a dated one.
- 🔴 **P7'S KEY WAS REPLACED BEFORE IT WAS BUILT, AND THAT IS THE SESSION'S BEST RESULT.** Rank
  3 specified *"key it on principal + epoch"*. Measured: all fourteen journal event kinds are
  AUTHORIZATION events and `internal/write/write.go` makes no `control.` call, so an append
  moves no epoch — that key answers **304 to a client missing new bullets**. The validator is a
  digest of the **UNCOMPRESSED** tar instead: it covers content, the visible set and `?scope=`
  at once, and it is the only form both servers can agree on, because gzip identity between
  them is recorded as unattainable while `tests/dualrun/` compares the uncompressed tar
  byte-for-byte. **Independently re-verified after merge:** mutating the validator to a
  content-independent value makes `TestAnAppendMovesTheETag` fail with its own message.
- ⚠ **ONE DEBT, FILED RATHER THAN CARRIED IN A REPORT.** P7's two binding claims are not in
  `AGENTS.md` — it has **73 bytes** free and its own rule forbids paying by deleting a claim.
  It is `## Defects (batched)` entry with a mechanical closing condition; it is not an open end.
- 🔴 **AND THE GATE BUILT THIS SESSION REFUSED THIS VERY DOC, CORRECTLY.** The first draft of the
  bullet above spelled the task board's real name — the SAME denied identifier scrubbed off `main`
  this morning as #68, re-introduced by the sentence explaining that no task resolved. Rule (o)
  exited 13 on the delta, named the file and line, and nothing was written or pushed. **Not a
  pre-existing red and not an override case:** the remedy was the scratch file. The remedy for the
  NAME is the one `AGENTS.md` already prescribes — keep the mechanism, drop the particular.
- 🔴 **A MUTANT THAT DOES NOT COMPILE DIES AT THE BUILD AND PROVES NOTHING — HIT LIVE WHILE
  VERIFYING P7.** Replacing the ETag's digest with a constant left `hex` and `sha256Sum`
  unused, so `go test` reported `[build failed]` and the guard never ran. Rebuilt so the mutant
  still USES both symbols while being content-independent, it reached the guard and died with
  the guard's own message. **Mutate the narrowest expression that can be wrong**, and check the
  mutant compiles before reading its verdict.
- 🔴 **RE-DERIVED FROM #97**, which lost the doc race to #98 and then to #96. Three of its
  bullets were deliberately not carried — its `SIX OF ELEVEN` event-kind count, its
  `leakscan`-exits-2-on-a-directory entry, and its *"the cheap control settled it"* bullet —
  each superseded or retracted above rather than merely reworded.
- 🔴 **AN EMPTY RESULT COULD NOT DISTINGUISH TWO CAUSES OF A `leakscan` EXIT 2, AND THE CHEAP
  CONTROL SETTLED IT IN ONE COMMAND.** The base clone exited 2 with untracked `result` symlinks
  AND a live agent worktree both present, either a plausible culprit. A fresh worktree of
  `origin/main` — same tree, neither artefact — scanned **rc 0**, and the scanner's single
  `COULD NOT READ` line named the worktree directory. **The rival mechanism was named before
  concluding, and the discriminating control cost less than reasoning about it would have.**
- 🔴 **THE LEAK GATE BUILT THIS MORNING REFUSED THREE OF THIS SESSION'S OWN DELTAS, AND THE
  THIRD WAS A DIFFERENT RULE FROM THE FIRST TWO.** Two `denied-identifier` (a repo name in a
  `$`-variable spelling, which is why it did not LOOK like a name; and this repo's own
  synthetic canary, pasted in while documenting how to probe the gate) and one
  `dated-incident` (a real date in prose). ⚠ **A SYNTHETIC VALUE IS STILL A DENIED
  IDENTIFIER** — the canary exists to be refused, so quoting it in a committed doc is a real
  violation, not an exemption. The remedy every time was to describe by ROLE.
- 🔴 **TWO REASONS FOR THAT ORDER WERE FALSE AND ARE RETRACTED IN THE CODE RATHER THAN
  SWAPPED.** "A lockout checked after the token is one a valid credential walks through" —
  false, measured. "An attacker must not get the failure record wiped" — false for THIS
  limiter: `netid.RateLimiter.RecordSuccess` is a deliberate no-op whose own comment explains
  that a success-resets-counter design is the defect. `internal/api` states the first reason
  for ITSELF; **a reason does not transfer between packages unexamined**, and examining it is
  what produced the retraction.
- 🔴 **A BUCKET-SEPARATION TEST NEEDS A THRESHOLD ABOVE ONE, AND AT ONE IT FAILS AGAINST
  CORRECT CODE.** `RecordFailure` reports the lockout when the count REACHES the threshold, so
  at 1 every client trips on its own first failure — making "the second client's first failure
  must not trip" unsatisfiable. The first draft asserted exactly that and went red against a
  correct implementation.
- 🔴 **AN ANCHOR INSIDE A STEP'S BODY IS NOT A BOUNDARY.** Inserting that leg anchored on a
  comment line that occurs INSIDE the Go proof step, so the insert landed mid-step and a
  follow-up rewrite truncated its tail. The whole-text pin caught it with a diff naming
  exactly the missing lines; review would not have. Restored from `origin/main` and appended
  at EOF, where the last step genuinely ends.
- ⚠ **A COUNT IN A DOCSTRING BESIDE THE DICT IT COUNTS WILL DRIFT** — "Four push steps", "the
  four CONTROL steps", "both pods", "Two repositories": ten stale phrasings swept in one pass.
  The docstrings now carry NO number, because the dict IS the count.
- **Decision (operator, this session): the IngressRoute lands in the SAME PR as the workload**,
  accepting that the public hostname answers refusals until the journal is seeded. The
  alternative — cluster-internal first, route as a follow-up — was offered twice and declined.
- **Decision (operator, this session): the in-cluster seeding is done by the ASSISTANT next
  session, with an explicit go-ahead first**, rather than handed to the operator as a runbook.
- 🔴 **"BOTH CI TIERS SIMULATED GREEN" WAS A SIMULATION THAT COULD NOT REACH THE BRANCH IT
  CLAIMED TO TEST.** The refuse-on-skip path only executes when a toolchain is ABSENT; the host
  has `go` and `nix`, so `toolchain_missing` returned `None` and the `require` set was never
  consulted. `CAIRN_FACTS_REQUIRE=go`, `=nix` and `=all` all printed an identical `20 passed`.
  **Ask what a green simulation structurally cannot execute** — here, the only branch that
  mattered.
- 🔴 **I REPORTED `completed=6/6` AND NEVER READ THE CONCLUSIONS. THREE OF THE SIX HAD FAILED.**
  `completed` and `success` are different fields on the same object, and the easier one to assert
  is the one that says nothing. This happened while assembling an audit brief, in the session
  that had been quoting *read the content, never the exit code* all day. **Read
  `[.statusCheckRollup[]|select(.conclusion=="FAILURE")]|length`, not a completion count.**
- ⚠ **AND THE CI BREAKS WERE STRUCTURAL, NOT FLAKY:** the new step sat at `ci.yml:380` while the
  job's `pip install pytest` was at `:531`; the `nix` job installs pytest NOWHERE; and 20 added
  tests took the `tests` job's drift guard from gap 97 to 117 against its `MAX_GAP = 100`. **A new
  pytest step in the `go` or `nix` job must be placed after that job's own interpreter setup, and
  adding tests in bulk must move `FLOOR`.**
- 🔴 **A SPELLED READ-ONLY GUARD WALKED PAST EVERY DESTRUCTIVE COMMAND PUT THROUGH IT.**
  `{tok.lstrip("-") for tok in cmd} & {"rm","write","commit",…}` accepts `git clean -fdx`, a hard
  repo reset, and any `sh -c "…"` one-liner, because a shell string tokenises as ONE token.
  Labelling it "a spelled check" in its own docstring was NOT enough — **it had no negative
  control, and neither did its sibling**, in a PR whose other three controls all did.
- ⚠ **A GUARD WHOSE BODY IS A WORD-COUNT WHILE ITS DOCSTRING CLAIMS A RELATIONSHIP.** #101's
  `test_every_unanswerable_fact_names_who_CAN_answer_it` asserted `len(reason) > 80` and that one
  of four words appeared. A reason saying the OPPOSITE — *"nobody knows who could"* — passes both.
  Same shape as the guards-narrower-than-their-docstring family already recorded here.
- ⚠ **THE SALVAGE IS THE SHAPE TO COPY, NOT THE MECHANISM.** #102 does the one part with a
  measured incident behind it, in one file: delete the derivable claim, name the command once,
  add no guard. Measured `+8 lines, -4 prose words, derivable claims asserted 3 -> 0` — the line
  count ROSE because a fenced command costs lines, and the number that matters is the assertion
  count. **Report both rather than the flattering one.**
- ⚠ **`flake.nix` HAD ALREADY DECLINED #101'S SHAPE, IN WRITING** — *"a check that ran the binary
  to print that list and diffed it against a third hand-written copy would restate one claim down
  a longer path."* The repo's existing prose contained the refutation of the mechanism before it
  was built. **Grep the tree for a rejected-alternative note before building a gate.**
- ⚠ **A THROWAWAY REPO BUILT TO TEST A GUARD STILL TRIPS THE never-commit-to-main HOOK**, and the
  hook resolves `-C` from the COMMAND TEXT: a shell variable it cannot expand, or a directory the
  command is about to CREATE, both make it judge the CALLER's repo instead. `git init -b work` and
  create the directory in an EARLIER tool call.
- ⚠ **A `no-change` (exit 5) SHORT-CIRCUITS BEFORE THE LEAK GATE.** Re-testing the gate with a delta
  already merged measures the no-change path, not the guard — a green there is about nothing. Use a
  fresh delta for every gate probe.

## Open-investigation blocks closed and moved out at the SECOND prune

🔴 **VERBATIM.** Six blocks: the two host-HOME doctor-test blocks and the ✅ CLOSED block that closed them (kept together, because the closure's whole value is that it quotes what the two live-reading blocks said), the `packages.default` flip's mechanical blocker, the live-JWKS-fetch coverage gap, and the publish gate. Every one is resolved or superseded on `main`. The two that stay in the handoff are the ones still open: the four loopback-only auth controls, and the byte-identity test that failed once and has not reproduced.

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

### CLOSED — a live JWKS fetch has now been exercised against a real issuer
- as-of: 2026-09-18 · **CLOSED 2026-09-25, and the `Next probe:` line below is DISCHARGED — do
  not run it.** The probe it asked for was *"a pod, a network and a real issuer"*, and the deploy
  recorded in `State now` is exactly that: the browser surface runs in-cluster against a live
  self-hosted GoTrue, reached over TLS through the public edge. **A completed HANDSHAKE, which is
  what this block said no measurement had ever produced:** the pod's startup line carries **no**
  `key set could not be fetched` warning, and that warning is emitted on any fetch failure — so
  its absence is the positive evidence, not silence. The failure arm was ALSO observed, which is
  what makes the success arm a measurement rather than an assumption: the same binary run against
  a 502 printed the warning, withheld the provider button and answered 503 on those routes.
  ⚠ **SCOPE: this closes the HANDSHAKE question, NOT the `SSL_CERT_FILE` one.** The root-count
  observations below stand unchanged and the variable is still inert in both directions; nothing
  here re-opens or re-measures that. ⚠ And the handshake was made by the **UI** image, not the
  pod image the root counts were taken in — same CA-bundle mechanism, different artefact, so read
  it as evidence about the mechanism rather than about that image.
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

### STILL LIVE, re-measured at `e15e331`: the host-HOME-dependent doctor test
- as-of: 2026-09-21
- **Symptom + exact repro:** `uv run --python 3.12 --with pytest -- pytest tests -q -p no:randomly`
  → **`1 failed, 1969 passed`**, the failure being
  `tests/test_cairn_doctor.py::TestTheCliWiring::test_a_no_sync_run_still_reads_the_LOCAL_config`.
- **Observed (with values):** measured TWICE this session, on `main` at `0587ace` and again on the
  `docs/positioning-agent-swarm-memory` branch — **identical count both times, 1 failed / 1969
  passed**, so the branch introduced nothing. `stat ~/.config/subsystem-store/instances` →
  mtime **2026-09-17 22:00:43**, one `.env` inside. `via: measurement`
- **Ruled out:** that either of this session's PRs caused it — the same failure and the same pass
  count on plain `main` before any branch existed. `via: measurement`
- **Leading hypothesis:** unchanged from the 2026-09-18 block above — a sibling session's write to
  `~/.config/subsystem-store/instances` makes `cmd_doctor` emit per-instance check names, and the
  test's `"token"` lookup finds none. CI is unaffected (fresh checkout, clean HOME).
- **Next probe:** none needed for either merged PR. The real defect is that the test inherits the
  operator's HOME instead of pinning one; that is the fix when somebody wants it.

### ✅ CLOSED — the host-HOME-dependent doctor test was fixed two merges before this doc still called it LIVE
- as-of: 2026-09-23
- **Symptom + exact repro:** the block above — *"STILL LIVE, re-measured at `e15e331`"* — records
  `uv run --python 3.12 --with pytest -- pytest tests -q -p no:randomly` returning
  **`1 failed, 1969 passed`**, the failure being
  `tests/test_cairn_doctor.py::TestTheCliWiring::test_a_no_sync_run_still_reads_the_LOCAL_config`,
  and its `Next probe` says *"the real defect is that the test inherits the operator's HOME instead
  of pinning one; that is the fix when somebody wants it."*
- **Observed (with values):** on `feat/ui-image` at `a88f60b` the full suite is
  **2059 passed, 0 failed** in 525.86 s — that test among them, no failure, no skip.
  🔴 **AND THE CONDITION THE BLOCK BLAMES IS STILL PRESENT, WHICH IS WHAT MAKES THIS A CLOSURE
  RATHER THAN A DISAPPEARANCE**: `stat ~/.config/subsystem-store/instances` still reads mtime
  **2026-09-17 22:00:43** with one `.env` inside — byte-for-byte the state the two earlier
  blocks both name as the cause. Same host, same HOME, same config, test green.
  `via: measurement`
- **Ruled out:** that the symptom merely failed to reproduce. The fix is identifiable and dated:
  `git log e15e331..origin/main -- tests/test_cairn_doctor.py lib/ cairn` names **`c47636b`**,
  *"Pin the host configuration a doctor test inherited, and close three ladder-filed defects"*
  (#60) — which is the `Next probe`'s own prescription, already landed. `via: measurement`
- **Ruled out:** that this doc was merely behind by one merge. It is behind by **two** — `c47636b`
  (#60) predates both `93d0f03` (#74) and `a88f60b` (#76) — and the LATER of the two blocks,
  which re-measured it as LIVE, was written **after** the fix existed, which is the part worth recording.
  `via: measurement` — the ordering was read off
  `git log --oneline e15e331..origin/main -- tests/test_cairn_doctor.py lib/ cairn`, which lists
  `c47636b` below both later merges rather than inferred from the version numbers.
- **Leading hypothesis:** none needed; closed.
- **Next probe:** none. 🔴 **THE DURABLE LESSON IS ABOUT THIS DOCUMENT, NOT ABOUT THE TEST.** An
  `Open investigations` block is APPEND-ONLY, so a block saying `STILL LIVE` survives every update
  that does not retype it — including the update that lands its fix. Two sessions re-measured this
  one as live; neither ran `git log <the-as-of-ref>..origin/main -- <the paths the block names>`,
  which is one command and is what closed it. **Before re-measuring any `STILL LIVE` block, ask
  what landed since its `as-of` ref.**
