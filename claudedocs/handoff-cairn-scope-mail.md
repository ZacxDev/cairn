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
- **Plan MERGED** `4413feb` (#221). Operator rulings since: **Q16** keep verb narrowing; **Q19** retraction requires
  READ for sender AND scope admin; the memos tab ships ON at deploy (no flag); populated-tab browser fixtures are a
  follow-up PR before S4. **Q20** (empty memo accepted) is OPEN, behaviour unchanged.
- **MERGED:** S0 #230; S1 #235 `8b7ef0d`; S2 #236 `862d3cb`; S3 #239 `0d9ba01`; S3 follow-ups #241 `a46b447`;
  **S5 #242 `74f826f`**; publish-workflow fix #243 `bb3ce63`. Each: round 0 + round 1, no 🔴.
- **DEPLOYED `sha-bb3ce63`** to both pods (deployment trunk `5c58ba1`). Verified: running imageIDs equal the tag
  digests, 0 restarts; cairn-ui logs `serving 51 route(s)`, `memos in postgres`, `client API off`; migrations 1,2,3;
  `/sign-in` 200, `/` 401, an anonymous `POST /memo` without Origin 403; the store answered an authenticated sync.
  Pre-deploy backup `cairn-ui-backup-pre-s5b` restored+verified.
- 🔴 **NOT verified signed-in:** the memos tab, sending and retracting from the browser — the OPERATOR's check
  (open a scope → Memos; send one; retract it; confirm the tombstone line).
- 🔴 **ROLLBACK below `sha-0d9ba01` needs the migration-3 recipe (#235);** once any credential is narrowed by verbs,
  the journal recipe too.
- **IN FLIGHT:** S5 follow-ups (`zach/scope-mail-s5-followups`, claim `cairn-scope-mail-s5-followups`): a
  database-backed uiaudit world driving the populated tab in a real browser, the adapter retract/tombstone pgtest,
  the form length limits, the memowire listener-first rule, and the round-0 deletion candidates.

## Next steps (ranked)
1. **S1** — DONE, #235 `8b7ef0d`. forcing: user — operator asked for scope mail.
2. **S7** (deployment: `-client-api-addr` container port, Service port, ingress route, trusted-proxy allowlist for
   that bind, a designated PROBE scope + a probe credential narrowed to read,write, `SHOW server_version` ≥ 17, the
   deploy-order check), then run `tests/memo/live-probe.sh` against it and record its summary line; then **S4**
   (tooling hook registration, LAST). S5 is DONE (#242). forcing: user — operator asked for scope mail.
3. **S5 follow-ups** — IN FLIGHT (see State now). forcing: user — operator chose "follow-up PR before S4".

## Defects (batched)
- **Being fixed in S5 follow-ups:** `cmd/cairn-ui/memotab.go`'s `IncludeRetracted: true` and `Retract` pass-through
  have no real-store test (both mutants survive); the send form's `maxlength` counts UTF-16 units where the server
  counts UTF-8 bytes; `internal/memowire` does not state that an added `memo.Rendered` field needs the listener
  deployed before clients update.
- **Carried to S4 (before the hook is registered):** a descendant that `setsid`s and keeps stdout open
  defeats the 2 s bound (measured 12.03 s) — close the pipes after `killpg`; relayed output is
  uncapped (64 MiB passed through) — cap it and treat overflow as `malformed`.
- **Closing-condition part 3's wording** diffs `-routes` against `b2ba3ac`, which predates scope-refs' `sources`
  route — re-anchor it on a post-scope-refs base before reading it.
- `tests/pgtest/run.sh:278` pipes into `head -1` inside an assignment under `set -euo pipefail` — safe only while
  the version line is logged once.

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

- **`echo "$out" | head -N` under `pipefail` fails once the writer's output passes the 64 KiB pipe buffer** — the
  publish workflow's anonymous-pull proof did exactly that when the image's tag list grew, and because it is the
  FIRST proof step the later legs never ran: two pod images pushed, the UI image not. A failed publish can be a
  PARTIAL publish — check each image's tag (200 vs a 404 control) before deploying. Fixed in #243. via: measurement
- **Merging while GitHub reports `mergeStateStatus: UNKNOWN`** (the base had just moved) went through; the merged
  `main` was then CI-green, but that was checked AFTER — re-read the state until it settles before merging.
  via: command

## How to verify
```bash
KUBECONFIG=<deployment kubeconfig> kubectl -n subsystem-store get deploy -o wide
KUBECONFIG=<deployment kubeconfig> kubectl -n subsystem-store logs deploy/cairn-ui --tail=1   # "memos in postgres", "client API off" until S7
gh pr list --repo ZacxDev/cairn --state open
```
