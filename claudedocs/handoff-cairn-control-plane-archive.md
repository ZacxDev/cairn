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

