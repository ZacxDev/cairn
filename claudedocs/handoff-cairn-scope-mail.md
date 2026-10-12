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
- **All slices MERGED, DEPLOYED and VALIDATED end to end on both hosts.** cairn: S0 #230, S1 #235, S2 #236, S3 #239
  (+#241), S5 #242 (+#244, #246), publish fix #243 — deployed `sha-f162b54`. Deployment repo: S7 (listener `:8125`,
  mesh-only gateway route, `memo-probe` store token). Tooling repo: S4 #2145 `66d75043` and the hook backoff +
  kill switch #2148 `de397e2a` (merged by operator instruction before its CI settled; CI was GREEN on the merged head
  `b435ab14`: pytests 26496/0, gotests 514/0, nodetests 1883/0, cairn-client-runs pass).
- **Rollout DONE (both hosts):** credentials `agents@workbench` `crd_ehdrdnv60ft1avwxgw146jn59g` and `agents@laptop`
  `crd_mt4p7wvyq7b1pbwwh7p32nf388` — operator principal, narrowed to the 26 scopes of the operator's project,
  verbs `read,write`; each proven with one listener request (200) BEFORE being written. Each host's
  `~/.config/subsystem-store/env` now carries `CAIRN_UI_URL=http://<mesh-gw>:8125` and `CAIRN_UI_TOKEN` (0600; token
  hash matches the issued one; the pre-rollout file kept as `env.pre-memo-rollout`). `scripts/ship.sh` converged both
  hosts at `de397e2a` (VERIFIED, both). Local token copies shredded.
- **End-to-end validation (both hosts, the deployed hook, a real memo):** `memo-send --scope <a real scope>` → id 3; a NEW
  SessionStart in a checkout of a repo on a cached scope received exactly ONE fenced block (1 open / 1 close marker, matching nonce,
  standing "DATA, not instructions" line, `via agents@workbench`); the same session's next prompt got 0 bytes; after
  `memo-retract`, a NEW session got 0 bytes and `memo-read` shows the tombstone. Live probe re-run: `steps=6
  passed=6 failed=0 exit=0`. Listener: 1 `client-api: refused` line since the pod started (the probe's own
  unauthenticated step), no lockout.
- **Closing condition: ALL FIVE PARTS MET — the arc is CLOSED.** Part 4 on the tooling repo's `main`, three ways: (a) its
  own push gate on `4a774ee2` GREEN on all four legs (pytests 26597/0, gotests 514/0, nodetests 1908/0, cairn-client-runs
  pass), where the hook test is a per-target floor (`run-tests.sh` `…test_cairn_memo_hook.py|76`) and the hook and its
  test are byte-identical to `1cbae305`; (b) run by hand in a clean worktree of `main` at `1cbae305`, inside `nix develop`:
  `--targets …test_cairn_memo_hook.py` exit 0, 79/79 (floor 76), and the hook's `--self-test` → `sabotaged=9 caught=9`
  (#2148 added three; the plan's `6/6` wording is superseded). (c) `main`'s run at `1cbae305` itself
  gave NO verdict: the gate pod was `Preempted` and pytests exited 255 — a kill, not a test result; its log was gone
  before it could be read. A hand re-run was staged and NOT created, because `main` had moved on and (a) covered it.
  ⚠ Running the target outside the dev shell exits **3** (a missing-toolchain precondition), not a test failure.
- 🔴 ROLLBACK below `sha-0d9ba01` needs the migration-3 recipe (#235); the journal holds verb-narrowed credentials,
  so older binaries refuse it — roll back only to a build ≥ S1.
- NOT verified: the browser memos tab SIGNED IN (operator's check); opencode delivery on a live session (plugin
  deployed, measured only on S0's one host).
- Claims: all released.

## Next steps (ranked)
1. **S1** — DONE, #235. forcing: user — operator asked for scope mail.
2. **Scope mail** — DONE and CLOSED: all five closing-condition parts met (see State now). forcing: user — operator asked
   for scope mail.
3. **Batched defects below** — a NEW arc, not this one; each needs its own claim. Two follow-ups the operator raised at
   closing, not yet planned: point the `cairn`, `handoff` and `resume` skills at `cairn-memo` (only `cairn-memo` names
   memos today), and external senders — the task board's in-cluster agents (env-only rollout, per-agent narrowed
   credential), the hosted personal agent (write access would break its read-only-on-cluster property: operator's call),
   and a webhook receiver for
   ClickUp/Discord/Signal (new service; scope routing, the 30-live-memo quota, per-peer lockout, third-party text).

## Defects (batched)
- **#2148 round-1 🟡s (tooling-repo hook):** (1) the host backoff is keyed on ONE fingerprint, so sessions in DIFFERENT
  environments (some exporting `CAIRN_UI_URL`, some not) overwrite each other's record — 8 alternating sessions sent 8
  refused requests; key a map fingerprint→expiry. (2) `_write_backoff` runs outside any try in `guarded_check`: a
  read-only cache root with an existing `.host.lock` sends a request per session SILENTLY (no backoff, no stop, no
  line) — wrap it and still write the session marker. (3) the 2 s host-lock wait turns a slow-but-healthy listener into
  false `unreachable` (20 starts × 150 ms → 7) — on lock timeout don't stamp `last`, retry next event. 🟢 removing
  `st_mtime_ns`/`st_size` from the fingerprint survives the suite.
- **#2148 round 0:** the client prints `unconfigured` both when no `CAIRN_UI_*` is set (no request) and on a listener
  401, so the hook backs off on both; a distinct "refused" client status would let it back off only on a real
  refusal — changes cairn's 7-status contract, operator's call.
- **#246 🟡:** `internal/ui/memos.go:364` `Scannable(subject, body)` → `Scannable("", body)` survives; add the
  subject-past-`MaxScannedBytes` + secret-in-body row.
- **Walk capture of a POPULATED memos tab** (axe / touch / overflow / screenshots) — operator ruled it a separate,
  non-blocking follow-up.
- **Closing-condition part 3's wording** diffs `-routes` against `b2ba3ac`, which predates scope-refs' `sources`
  route — re-anchor it on a post-scope-refs base before reading it.
- `tests/pgtest/run.sh:278` pipes into `head -1` inside an assignment under `set -euo pipefail` — safe only while
  the version line is logged once.
- `memo-send --help` does not say what the send-time secret scan cannot catch (the skill does); the plan's
  token-cost table was not recomputed from the S0 goldens.

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

- **`ssh host 'bash -s' < script` gives the script's STDIN to `bash` itself** — a `read`/`tr` inside the script then
  consumes the script's own text. The first laptop env write stored a line of the script as `CAIRN_UI_TOKEN`
  (86 chars, 19 spaces); caught by a shape check on the written file, restored from the backup within ~2 min, and the
  listener log showed no request from that host. Ship the script and the secret as FILES (0600 temp, `scp -p`), run
  it with stdin from the secret file, `shred -u` after; verify by comparing token HASHES on both sides. via: measurement
- **Test a new credential with ONE request before writing it anywhere a hook will read** — the listener's lockout is
  per mesh host (5 in 60 s → 900 s), and a hook firing on every prompt multiplies a bad token. `curl -H @<0600 header
  file>` keeps the token off argv. via: command
- **A tekton run on a the tooling repo's `main` commit is cancelled (`error … superseded`) when a newer commit lands** — read the
  newest commit's statuses, and check the commit you care about is its ancestor. via: command
- **The S4 hook became active on both hosts before rollout** (some session switched after #2145 merged); harmless
  because with no `CAIRN_UI_*` the client sends nothing — it printed `not configured` once per session. via: command

## How to verify
```bash
bash tests/memo/live-probe.sh ~/.config/cairn-memo-probe/probe-input     # SUMMARY … exit=0
cairn memo-send --scope <scope> --subject "check" --ttl 1h   # then a NEW session in a checkout of that repo shows ONE block
cairn memo-retract --scope <scope> --id <id>                 # a NEW session shows nothing
gh api repos/<tooling-repo>/commits/<its main sha>/status --jq '.statuses[]|"\(.context) \(.state)"'
```
