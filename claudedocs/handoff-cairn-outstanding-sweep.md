# Handoff: cairn-outstanding-sweep

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
Close out everything left open across the five linked cairn arcs (scope-mail, scope-refs, ui-hub-team,
mobile-pwa, transcripts): the unbuilt work the operator asked for and the batched known defects, run
as parallel lanes. This is the COORDINATOR doc; each arc's own handoff stays the source of truth for
its arc and is updated by the lane that owns it.
- **closing-condition:** `check` — every item in Next steps 1–8 is either MERGED (verified by content
  on its repo's main branch) or recorded here as BLOCKED-ON-OPERATOR with the exact ask; and the final
  deploy wave's image is running with `tests/memo/live-probe.sh` exiting 0 against it.

## State now
- **Deployed:** cairn `168824c` on both pods (wave 1; wave 0 was `be6662e` + the `CAIRN_OIDC_*` env rename).
  Verified by the deploy lane: imageID == tag digest, 0 restarts, live probe 6/6 exit 0, new features inert
  ("code sources unconfigured", "transcript capture DISARMED"). No migration in either wave.
- **Hosts:** tooling repo `e7e1fcdf` shipped to both hosts (hook understands `refused`, self-test 13/13;
  client pin at `ec0a67d`). Verified on the workbench only; the laptop rests on the ship script's report.
- **Lanes DONE:** scope-refs S3–S5 (#250, #255, #259); scope-mail fixes + `refused` (#251, #257; tooling
  #2157, #2165); ui-defects (#252, #253, #254, #256, #262, #263 — and the mobile-pwa arc's closing condition is
  now MET); tooling-misc (cairn repo handle #2154, presence stop-signal deadlock #2159); webhook receiver
  (deployment repo #990, running, INERT: no credential, no routes); personal-agent bridge memo read+send
  (deployment #989, tooling #2155) — and its credential is ISSUED: `crd_wycapge1f2htcaw8654t13fndm`, read+write
  on three scopes, proven 200, stored sops-encrypted, bridge logs `memos=enabled`, one read through the bridge
  answered 200 from the bridge's OWN peer bucket.
- **Lane IN FLIGHT, twice cut off by API limits:** transcripts+plugins. MERGED: AP1–AP3 + waiver record (#258),
  S3b capture uploader (#260). S4 = PR #261 OPEN at `252c8f6`: round 3 had no 🔴 but CI `go` was RED (an
  allocation budget, 103.8 > 100 MiB at the cap) plus two findings (Unscanned persisted; page-wide item cap) —
  a fix round was running in worktree `cairn-transcripts-s4` when the limit hit. S11 was being developed in
  `cairn-transcripts-s11` (not yet a PR). S7, S8, S5, S6, S9, S10 and arming preparation not started.
- **Operator decisions taken after the lanes reported:** invite fold = option A (team-link form only; invite
  minting redirects there; existing invites keep working; no migration); task-board agents get memos by the
  task board RELAYING with one read-only credential; scope-refs source journal on a DEDICATED volume (not the
  shared UI state volume); approved: bridge credential (done), receiver credential + read-only Signal DB
  login, public relay port 8126 on both gateways. NOT approved: making `uiaudit-touch` a required check.
- Not verified: laptop hook/client after the ship; a real session receiving a memo after the ship (probe scope
  only); anything about #261's fix round.

## Next steps (ranked)
1. Transcripts lane: re-check #261 (CI `go` alloc budget + the two round-3 findings), finish the fix round in
   `cairn-transcripts-s4`, re-audit the delta, merge on no 🔴; then S11 → S7 → S8 → S5 → S6 → S9 → S10; then
   prepare arming and STOP before capture-token issuance. forcing: user — operator: ship capture now
2. Invite fold, option A (cairn `internal/ui`): one mint form on the Team page with `?project=` preselect;
   `POST /invite` redirects; existing invites listed/revocable/redeemable until expiry. forcing: user —
   operator chose A
3. Task-board relay (task-board repo + deployment repo): the board holds one read-only `agents@…` credential
   and passes memos to its agents; credential issuance needs the scopes agents work in. forcing: user —
   operator chose relay
4. Scope-refs source journal on a dedicated volume: rework the prepared manifest diff (it used the shared UI
   volume), add backup wiring, deploy with a UI image ≥ `231aea5` (already running). forcing: user —
   operator chose dedicated volume
5. Webhook receiver go-live: needs the operator's ROUTES first (which source feeds which scope) — the
   credential's scopes follow from them; then issue `webhooks@receiver` (send only), the read-only Signal DB
   role, and open relay port 8126 per the receiver's README (restarts every public relay once; firewall step on
   the production node first). ClickUp/Discord registration stays with the operator. forcing: user —
   operator approved
6. Operator-only checks: signed-in Team flow, iPhone install checklist, signed-in memos tab, live opencode
   memo delivery, ring-notification middle-click. forcing: user — the operator's own checks

## Gotchas / decisions / dead-ends
- **Operator decisions this sweep (paraphrased):** the redactor's 90% held-back gate is WAIVED as an
  arming precondition (last 157/190) — AP1–AP3 still close by their own checks, AP4 is measured and
  recorded only; add a distinct `refused` memo-check status (7 → 8, both clients/ledgers move);
  build all four external senders, INCLUDING personal-agent send (accepted: it removes that agent's
  read-only-on-cluster property); merge on a 🔴-free audit round and deploy per wave, but credential
  issuance and migration deploys stop for the operator. via: command
- **`find-session.py --arc handoff-cairn-*.md` exits 5** (no repo handle holds cairn); pointing an
  unused handle at the cairn checkout for one run resolved it. Lane 6 adds a real handle. via: command
- **A preempted gate run is not a verdict:** the tooling repo's main run on `1cbae305` was
  `Preempted` (pytests exit 255) and its pod log was gone within minutes; the next main head's own
  push gate was the verdict. A hand re-run guarded by "main still at the same sha" correctly aborted.
  via: measurement
- **`git cat-file -e $r:path` in zsh** expands `:p` as a history modifier → "Not a valid object
  name"; brace it `${r}:path`. via: command
- **Status files live in the coordinator session's scratchpad** — a new session cannot read them;
  read each lane's PRs and its arc handoff instead. via: assumed

- **A freshly issued credential answers 401 for up to 30 s** — the UI re-reads the journal on a 30 s timer
  (`refreshInterval`, `cmd/cairn-ui/main.go`). Wait a full interval before the one proving request; an early
  try costs a strike toward the per-peer lockout. via: measurement
- **`kubectl logs deploy/<x>` right after a rollout can read the OLD pod** — the bridge "still said disabled";
  the new pod said enabled. Read the pod by name. via: command
- **API limits kill a lane AND its children; children's results then arrive at the coordinator, not the
  lane.** A resumed lane reports "waiting" on workers that no longer exist — check its worktree for unpushed
  commits and forward any orphaned audit result. via: measurement
- **Never read a Secret with a bare `-o jsonpath='{.data}'`**, even truncated — base64 of the first bytes is
  the first bytes. Select the key and pipe to a file, or count it. via: command

## How to verify
```bash
gh pr list -R ZacxDev/cairn --state all --limit 30 --json number,title,state,mergedAt
git -C <deployment-repo> log --oneline -10 origin/trunk -- <cairn manifests>   # deployed pins
bash tests/memo/live-probe.sh ~/.config/cairn-memo-probe/probe-input           # SUMMARY … exit=0
```
## Defects (batched)
- 🟡 The store pod still uses four `SUBSYSTEM_STORE_*` env names (deprecation warnings at start); rename after
  checking what outside the pod reads them (backup/exec scripts).
- 🟡 The bridge credential's first ~16 of 43 characters were printed into the coordinator's transcript by a
  careless secret read (`jsonpath={.data}` piped to `cut`). Rotate if transcripts are captured or shared:
  issue new → secret → restart → `cairn-server -revoke-credential -credential crd_wycapge1f2htcaw8654t13fndm`.
- 🟡 Receiver (deployment #990): a batch is lost if the summary-only retry is itself refused; unusual-space
  normalisation runs after credential scrubbing; 16 slow clients can hold every request slot once public.
- 🟡 scope-refs S5: Ctrl-C leaves a stalled `git fetch` running; a file missing from every FETCHED source is
  reported stale even when another source could not be fetched (audit recommends "could not check").
- 🟡 tooling: stale "five handles" comments; a resume scope-warning test passes for the wrong reason;
  `cmd/cairn-server/main_test.go` `freePort` and `uiaudit/payload_test.go` still close-then-reuse a port.
- 🟡 Nothing in CI notices a self-test sabotage that stops applying (that is how `pwa_check --self-test`
  broke unseen).
