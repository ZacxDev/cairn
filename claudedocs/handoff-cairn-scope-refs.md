# Handoff: cairn-scope-refs

## Run this first — the index, one command
```bash
cairn recall --repo "$(git rev-parse --show-toplevel)"
```
Terse pointers this doc does not carry, curated by past sessions and outliving it.
🔴 RECALL, NOT LIVE OBSERVATION — every line is a pointer to VERIFY, never a current
reading, and it may describe a gotcha already fixed. `scope-absent`/`scope-empty` means
nothing is recorded yet: ordinary, not an error, and not a clean bill of health.
Non-blocking: if it exits non-zero, print the stderr line and carry on.

## Goal
Let each scope declare the code it documents (`git:<host>/<repo-path>[//<subpath>]@<branch>`) so a
CLI auditor can deterministically check entries' paths, SHAs and PR refs against the remote — per
`claudedocs/plan-cairn-scope-refs.md` (#210).
- **closing-condition:** `check` — the plan's closing condition: S1–S5 merged (verified by content),
  `tests/refaudit/e2e.sh` exits 0 in a blocking `go` step with `--self-test` printing
  `sabotaged=6 caught=6`, `tests/conformance/run_go.sh` exits 0 with the sources rows and the oracle
  run stays at 0 failures, and the UI/pod seam test passes.

## State now
- **Plan MERGED** #210 `80daba4`. **S1 #218 and S2 #219 MERGED** through merge train 1 (#229, merge
  commit `b06f15c`). **S3 #250 MERGED** `b44d0af` (the `sources` verb). **S4 #255 MERGED** `231aea5`
  (the edit form, the Sources block, the UI/pod seam test — closing-condition part 4).
  **S5 #259** (`internal/refaudit`, `cairn audit-refs`, `tests/refaudit/e2e.sh` — part 2): merged in
  the commit that carries this doc, if this line is on `main`; verify by content.
- **Audit ladders:** #250 r0+r1 (r1 clean). #255 r0 (D1 read-gated page kept) + r1 (three 🟡 fixed,
  no 🔴). #259 r0 (requirements questioned, below) + r1 (two 🔴 fixed: an inherited `GIT_DIR` could
  redirect the `--prune` fetch into the caller's repo; a private repo's 404 read as a stale PR) + r2
  delta.
- **Deployed and CONFIGURED:** the personal instance runs pod and UI at `sha-168824c` (S3–S5), and
  the source journal is now configured on a DEDICATED volume (operator decision): the UI mounts it
  read-write and writes `sources.jsonl` there, the pod mounts the same claim read-only, and the pod
  does NOT mount the UI's state volume (read from the live pod spec: three claims, none of them the
  UI's; the UI's four as the positive control). Measured after the rollout, image digests unchanged
  and restarts 0 on both: the UI's startup line reads `code sources written to <path>`; `cairn
  sources --scope <s>` answers `status=sources-undeclared … journal=absent`, exit 0, on two scopes
  (the `sources-unconfigured` it replaced is the earlier record's reading, not re-measured here);
  the memo live probe still exits 0 (6/6). The UI backup job stages the volume as `sources/` and
  logs `sources: absent` at stage and again after restore-verify.
  ⚠ NOT verified: a WRITE. Nothing has been saved, so the UI creating the file (`0600`) and the pod
  then reading it across the two mounts is unexercised — the mount is read-write and the directory
  writable by the UI's uid, which are prerequisites, not the save.
- Claims: `scope-refs-s3-s5` released at the end of the run.

## Next steps (ranked)
1. **Operator: declare the first source in the browser** (an admin of the scope, the scope page's
   Sources block). Then verify the half nothing has exercised: `cairn sources --scope <s>` lists it,
   `cairn audit-refs --scope <s>` runs, and the next UI backup logs `sources: lines=1`, twice (stage
   and restore). Then set the backup job's source-line floor to 1 in the deployment repo, so a lost
   journal fails the job instead of passing as `absent`. A `503 store-unreachable` from the pod at that point means the pod cannot read the
   file the UI created. forcing: user — operator asked for scope refs end to end.
2. **Operator decisions the S5 audit raised** (each `forcing: audit`):
   - ✅ DECIDED AND BUILT — 🟡2 of #259 r1: a claim missing from the fetched sources while another
     declared source was never fetched is now `unchecked: not in the sources fetched` (exit 10), never
     stale; a stale verdict means every declared source was read. The plan's test-plan row records it.
   - Q3/Q7/Q8/Q9/Q11 shipped as the plan's recommendations (operator host; root-then-subpath paths;
     structural `path#Symbol` only; default host map `github.com`; off-branch informational) with no
     recorded operator answer. Confirm or redirect.
3. **The one-time operator check (plan S5):** `cairn audit-refs` over a scope citing a PR in this
   public repository, with and without `GH_TOKEN`; record the states.
4. **B3 (census mode)**, the plan's recommended cheap half, is NOT built.

## Defects (batched, open — merged under the no-🔴 rule)
- ✅ FIXED — 🟡 #259 r2: a Ctrl-C (SIGINT/SIGTERM/SIGHUP) during a `git` call now cancels its
  context — the same process-group kill the timeout uses — and is re-raised once `git` is reaped
  (`TestAnInterruptKillsTheStalledFetch`, red before the fix).
- 🟢 #259 r2: no test pins the process-group kill on its own (`WaitDelay` alone keeps the stall test
  green while leaving `git-remote-https` alive); and `gitEnv`'s comment says credential-helper config
  is kept, while config injected through `GIT_CONFIG_COUNT`/`GIT_CONFIG_PARAMETERS` is stripped (a
  private source configured that way fails loud, exit 10).
- 🟢 #259 r1: `outside-sources` counts as checked, so a scope citing only out-of-scope PRs exits 0
  with `checked=1` (plan-specified; recorded).

## Gotchas / decisions / dead-ends
- **The pod's store is a COPY that `seed.sh` overwrites** (`tar -xf` over `/data`) — anything the UI
  writes into the store tree is reverted on re-seed; keep UI-written state in its own journal.
  via: code
- **The sources journal's dedicated volume is node-local and ReadWriteOnce, shared by two pods** —
  safe only because both already mount the store's volume, whose node pin places them together, so
  the new claim is provisioned on that node whichever pod consumes it first (so no ordering should
  deadlock — derived from the node pins; one rollout observed, both pods up in under a minute). If the store's volume ever moves, both pods go Pending on a volume-affinity conflict:
  loud, not silent. Rollback of the deployment change DELETES the claim and the journal with it
  (prune, reclaim `Delete`) — restore `sources/` from the UI backup archive if declarations exist.
  via: command
- **Claim-level `readOnly` on the pod's mount co-mounted fine** on this deployment's hostpath-style
  provisioner: both pods Running with the claim mounted, the UI's `rw`, the pod's `ro` and refusing
  a write. One driver, one measurement; the plan's "never claim-level" warning (the reader stuck
  Pending beside the read-write holder) is about a different driver and still stands there.
  via: command
- **Arc journal: `/var/lib/cairn-arcs/arcs.jsonl` on PVC `cairn-arc-journal` (local-path RWO);
  the store pod writes, cairn-ui mounts it read-only** — both pods must share a node. The sources
  journal reverses the direction (UI writes, pod reads). via: command

- **`internal/codesrc` imports `internal/identity`**, so the Go client binary now links identity,
  control and netid for a five-line value check — no cycle, no import-ban hit, no `init()`; a small
  standalone helper package would avoid it. via: code
- **The sources-journal ≠ arc-journal refusal compares resolved PATHS** (symlinks are followed), so
  a hardlink, bind mount or second mount path to the same file gets past it; both journals are
  append-only and line-flocked, so the cost is cross-read noise, not corruption. via: code
- **`internal/client` must never import `internal/refaudit`**: `internal/ui` imports `internal/client`
  (the agent tab), so the auditor would be linked into the browser surface. `cmd/cairn` joins them via
  `client.SetAuditRunner`; `internal/depspolicy`'s `TestThePodAndTheBrowserSurfaceNeverReachTheAuditor`
  holds it. `os/exec` WAS already in `cmd/cairn-ui`'s graph (via `internal/client`) before S5 — the
  plan's clause (e) wording was false; the guard pins the UI's exec importer set to exactly
  `internal/client`. via: code
- **S4 departs from the plan on one detail:** `GET /scope/sources` is gated by READ (the page shows the
  declaration, the same as the scope-page block) and only the form and the POST need `admin`. via: code
