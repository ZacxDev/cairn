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
  READ for sender AND scope admin; memos tab ON at deploy; the populated-tab click-path browser test (#244) satisfies
  "before S4", and the WALK capture of a populated tab is a separate non-blocking follow-up; S7 live in two pushes;
  a DEDICATED probe identity. **Q20** (empty memo accepted) is OPEN.
- **MERGED (cairn):** S0 #230, S1 #235, S2 #236, S3 #239, S3 follow-ups #241, S5 #242, publish fix #243, S5
  follow-ups #244 `e18efd1`, secret-first send check #246 `f162b54`. Each audited round 0 + 1, no 🔴.
- **DEPLOYED `sha-f162b54`** to both pods (deployment trunk `186b730`); #244 was held until #246 merged so its
  credential-echo case was never live. Running imageIDs = tag digests, 0 restarts.
- **S7 DONE (deployment):** cairn-ui `-client-api-addr=0.0.0.0:8125` + Service port (`4cf9ac3`), a MESH-ONLY gateway
  block `listen <mesh-gw>:8125` mirroring presence's 8105 (`11f9e1a`), and a store-pod token row `memo-probe` scoped
  to `cairn-memo-probe` only (`e27c77f`). Probe identity: synthetic user (provider `memo-probe`, cannot sign in),
  scope `cairn-memo-probe`, credential `crd_by7z77tk0yn6qrq64b5rbth850` (read,write). `SHOW server_version` 18.6.
- ✅ **Closing-condition part 5 MET:** `tests/memo/live-probe.sh` against the deployed listener over the mesh →
  `SUMMARY live-probe: steps=6 passed=6 failed=0 exit=0` (recorded as a comment on deployment commit `11f9e1a`),
  re-run green after the `sha-f162b54` deploy. Probe input (0600, two live tokens) is in
  `~/.config/cairn-memo-probe/probe-input` on the operator's host.
- 🔴 NOT verified signed-in: the browser memos tab — the OPERATOR's check.
- 🔴 ROLLBACK below `sha-0d9ba01` needs the migration-3 recipe (#235); the journal now holds verb-narrowed
  credentials, so older binaries refuse it — roll back only to a build ≥ S1.
- **IN FLIGHT:** S4 in the tooling repo (branch `zach/cairn-memo-s4`, claim `cairn-scope-mail-s4`): hook defects
  (setsid pipes, output cap), registration, `cairn-memo` skill, opencode surfacing, cairn flake bump, rollout runbook.

## Next steps (ranked)
1. **S1** — DONE, #235. forcing: user — operator asked for scope mail.
2. **S4** (tooling) — IN FLIGHT; audit + merge, then the PER-HOST ROLLOUT (operator: issue `agents@<host>` narrowed
   read,write, write `CAIRN_UI_URL`/`CAIRN_UI_TOKEN` 0600), then closing-condition part 4 on the tooling repo's main.
   S5 and S7 are DONE. forcing: user — operator asked for scope mail.

## Defects (batched)
- **Carried to S4 (being fixed in the S4 PR):** a descendant that `setsid`s and keeps stdout open defeats the 2 s bound
  (measured 12.03 s); relayed output uncapped (64 MiB passed through).
- **#246 round-1 🟡:** `internal/ui/memos.go:364`'s `memo.Scannable(subject, body)` → `Scannable("", body)` survives the
  suite; a crafted POST with a >16 KiB subject and a secret in the body would then be echoed. Add the row.
- **Walk capture of a POPULATED memos tab** (axe / touch / overflow / screenshots) — operator ruled it a separate,
  non-blocking follow-up; needs a database in the walk world, which moves every existing walk measurement.
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

- **The memo listener's failed-credential lockout is PER MESH HOST** (the gateway sets `CF-Connecting-IP` to the mesh
  peer): five bad tokens from one agent host lock that host out of memos for 15 min, and every unauthenticated 401
  probe from a host counts. via: code
- **Probe scopes need THREE things in this deployment:** a `scope-created` journal record (only `cairn-server
  -create-user -scopes` writes one), a store directory holding ≥1 file, and a store-pod token row allowing it — the
  client's `memo-check` answers `no-scope` from the local cache before it ever asks the listener. via: command
- **`sops edit` with a scripted EDITOR:** copy the existing rows' indent; a wrong indent is refused by sops as invalid
  YAML and the file is left untouched (it re-runs the editor once). via: measurement
- **zsh ate a git refspec:** `"$C1:refs/heads/trunk"` expanded `:r` as a history modifier — brace it, `"${C1}:…"`.
  via: command
- **A subagent stuck polling CI re-emits its completion notice indefinitely** — read the PR state directly and
  `TaskStop` it. via: command

## How to verify
```bash
bash tests/memo/live-probe.sh ~/.config/cairn-memo-probe/probe-input   # SUMMARY … exit=0
KUBECONFIG=<deployment kubeconfig> kubectl -n subsystem-store logs deploy/cairn-ui --tail=1   # "client API on [::]:8125"
gh pr list --repo ZacxDev/cairn --state open
```
