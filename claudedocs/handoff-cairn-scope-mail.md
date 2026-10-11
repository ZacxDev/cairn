# Handoff: cairn-scope-mail

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
"Scope mail", named **memo**: any party with write on a scope can send a short message to it, and
agent sessions on that scope see it — at session start and periodically via a hook — as a fenced,
UNTRUSTED pointer-plus-preview block. Per `claudedocs/plan-cairn-scope-mail.md` (#221, `4413feb`).
- **closing-condition:** `check` — the plan's five-part closing condition (`claudedocs/plan-cairn-scope-mail.md`
  `### closing-condition`): S0–S5 and S7 merged by content; `tests/memo/e2e.sh` exits 0 in the
  `pgtest` job with `--self-test` printing `sabotaged=13 caught=13`; the pod's `-routes` output
  unchanged (an invariant guard); the tooling repo's hook test target exits 0 with the hook's
  `--self-test` printing `sabotaged=6 caught=6`; and `tests/memo/live-probe.sh` exits 0 against the
  DEPLOYED listener.

## State now
- **Plan MERGED** `4413feb` (#221). Operator rulings since: **Q16** keep decision 17's verb narrowing; **Q19**
  retraction requires READ for sender AND scope admin (narrows O8; recorded in the plan by #239). **Q20** (empty memo
  accepted) is OPEN, behaviour unchanged.
- **MERGED:** S0 #230; **S1 #235 `8b7ef0d`**; **S2 #236 `862d3cb`**; **S3 #239 `0d9ba01`**. Each: round 0 + round 1,
  no 🔴, merged under the operator's "merge if the latest round has no high-severity finding" rule.
- **DEPLOYED `sha-0d9ba01`** to both pods (deployment trunk `fd6aa85`), 2026-10-11. Verified: running imageIDs equal
  the tag digests, 0 restarts; `schema_migrations` = 1,2,3 (memos/memo_events empty); cairn-ui logs `client API off
  (no -client-api-addr)`; `/sign-in` 200, `/` 401 unauthenticated; store answered the client's authenticated sync
  (304) and refuses an unauthenticated snapshot (401). Pre-deploy backups `cairn-ui-backup-pre-memo` and
  `subsystem-store-backup-pre-memo` restored+verified. NOT verified: any memo path (listener off until S7).
- 🔴 **ROLLBACK below `sha-0d9ba01` needs the migration-3 recipe (#235);** once any credential is narrowed by verbs,
  the journal recipe too.
- **IN FLIGHT:** S5 (`zach/scope-mail-s5`, the scope page's memos tab) and S3 follow-ups
  (`zach/scope-mail-s3-followups`: #239's round-1 🟡s + the e2e self-test CI time), both by subagents.

## Next steps (ranked)
1. **S1** — DONE, #235 `8b7ef0d`. forcing: user — operator asked for scope mail.
2. **S5** (cairn UI tab) — IN FLIGHT; then **S7** (deployment: listener port/Service/ingress, trusted proxies,
   probe scope + credential, deploy-order check), then **S4** (tooling hook registration, LAST).
   forcing: user — operator asked for scope mail.
3. **S3 follow-ups** — IN FLIGHT: the Defects (batched) list below plus round-0 D1 (e2e `--self-test` made `pgtest`
   CI's critical path: 1m07s → 16m36s). forcing: user — operator asked to fix audit findings before S7's live probe.

## Defects (batched)
- **From #239 round 1 (being fixed in S3 follow-ups):** cursor margin / `after` / `memo-read` paging have no failing
  test (`internal/client/memocursor.go:90-96`, `memo.go:414-451`); `memo-check`'s own instance routing untested
  (`memo.go:228-238`); strict response decoding couples client and listener releases (`memo.go:108-119`); a failed
  live probe leaves its memo live for an hour (`tests/memo/live-probe.sh`); `--ttl` under 1 s silently means 3 days;
  cursor saved before the block prints; `ScopeForRepo` errors swallowed as `no-scope`.
- **Carried to S4 (before the hook is registered):** a descendant that `setsid`s and keeps stdout open
  defeats the 2 s bound (measured 12.03 s) — close the pipes after `killpg`; relayed output is
  uncapped (64 MiB passed through) — cap it and treat overflow as `malformed`.
- **Closing-condition part 3's wording** diffs `-routes` against `b2ba3ac`, which predates scope-refs' `sources`
  route — re-anchor it on a post-scope-refs base before reading it.

## Gotchas / decisions / dead-ends
- **Go's `unicode.C` already includes unassigned code points** — "unassigned" computed as "in no
  table" admitted every one; `Unsafe` is "not in L, M, N, P, S or Z". via: measurement
- **The tooling repo's `scripts/leakscan.py` reports 13 findings already on its `main`**, none from
  this work. via: command

- **S1's first hand-run "advisory lock removed" mutant SURVIVED** — the 20-goroutine race window was narrower than
  connection setup; the test was fixed to hold connections first (`1cbfc16`). A concurrency guard needs its mutant
  run, not its design read. via: measurement
- **S1 stamps `created_at` AFTER taking the per-scope lock**, so one scope's rows commit in time order and the
  plan's "late committer" cursor test had to span two scopes; recorded in the plan's S1 corrections section. via: change
- **An older `cairn-server` refuses a journal holding `credential-verbs-narrowed` whole** (exit 78, "unknown event
  kind", nothing appended) — the fail-closed rollback premise, measured on the base build. via: measurement
- **The `-narrow-scopes` without `-narrow-verbs` refusal broke `tests/presence/e2e.sh`**; it now passes all three
  verbs. Any other script issuing narrowed credentials needs the same. via: command

- **Correction to this doc's previous `State now`:** it said the base moved under #235 after its CI ran (#233/#225).
  Wrong — #235 had already merged both (`dce97bf`) and its CI covered them; only #227 arrived later. Read the PR's
  own merge commits before claiming a base moved. via: command

- **Deploy recipe that worked (this arc):** `kubectl create job --from=cronjob/cairn-ui-backup <name>` and the same
  for `subsystem-store-backup`, read the `OK: … restored and verified` line; ghcr anonymous manifest GET 200 for the
  tag AND 404 for an all-zero control tag; bump both pins in a deployment-repo worktree off its trunk, push to
  trunk; `flux reconcile kustomization subsystem-store -n flux-system --with-source`; compare running `imageID` to
  the tag's `docker-content-digest`. Cluster access: the deployment repo's own kubeconfig. via: command
- **The base-clone write guard refuses `git -C $VAR …`** for ANY repo (it cannot resolve the variable) — spell the
  worktree path literally. via: command
- **S3's e2e self-test reruns the whole e2e per sabotage**, ~14.5 min per PR. via: measurement

## How to verify
```bash
KUBECONFIG=<deployment kubeconfig> kubectl -n subsystem-store get deploy -o wide
KUBECONFIG=<deployment kubeconfig> kubectl -n subsystem-store logs deploy/cairn-ui --tail=1   # "client API off" until S7
gh pr list --repo ZacxDev/cairn --state open
```
