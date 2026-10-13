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
- **Deployed:** cairn `b888b2d` on both pods (wave 3), carrying every slice of the sweep. Memo live probe 6/6
  exit 0 against the ARMED pod. Tooling repo `63ee919c` shipped to both hosts (cairn client and `cairn-capture`
  at `b888b2d`).
- **Transcript capture is ARMED on the personal instance, for ONE host.** 213 sessions stored after the first
  armed pass (dry run on that host: 845 sessions, 634 held — accepted by the operator — 211 to ship); one
  incremental timer upload observed since. Pod memory peaked at 18Mi during the pass (limit 768Mi). Redaction
  spot-check on stored bytes: the capture token's prefix 0 times; no literal bearer token. Details and every
  rollback: `claudedocs/handoff-cairn-transcripts.md`. Plugins: none registered.
- **All lanes DONE and merged:** transcripts+plugins (#258, #260, #261, #266, #268, #269, #270, #271, #272,
  #273, #274, #275, #276); scope refs S3–S5 (#250, #255, #259) with the source journal live on a DEDICATED
  volume (#264); audit-refs "could not check" (#267); scope-mail fixes and the `refused` status (#251, #257);
  ui-defects (#252, #253, #254, #256, #262, #263; mobile-pwa arc closed); invite fold option A (#265);
  tooling-misc; env-name retirement on both hosts and the store pod.
- **External memo senders:** the personal agent's bridge reads and sends memos (credential live, one read
  proven 200); the task board RELAYS memos to its agents with one read-only credential for 13 scopes — relay
  ON, but its token has made NO request yet (no agent with a task was running); the webhook receiver is
  public and reads the chat pipeline, but sends NOTHING: no routes, no credential.
- Not verified: the relay's first real request and an end-to-end relayed memo; a real-browser read of a stored
  transcript; a second signed-in principal being refused a transcript; the second host's hook/client beyond
  the ship script's report and a store-path match.

## Next steps (ranked)
1. Webhook receiver go-live: the operator fills the route table (source id → scope) in the deployment repo's
   receiver handoff; then issue `webhooks@receiver` (send only) for exactly those scopes, wait a full 30 s
   refresh, prove with ONE request. ClickUp webhooks and the Discord app setup are the operator's.
   forcing: user — operator approved all three sources
2. Transcripts are NOT backed up: decide a backup design that honours retention and owner deletion (the
   existing job stages through a small scratch volume and would outlive both). forcing: user — operator
   decision requested at arming
3. Second host: `cairn-capture --dry-run` first, mint its capture token (operator OK), raise the quota, enable
   its timer. forcing: user — operator armed one host first
4. First dispatched task-board agent on a scoped task: confirm the relay's first listener request answers 200
   and one memo is delivered once, then retracted. forcing: user — operator chose the relay
5. Operator-only checks: signed-in Team flow, iPhone install checklist, signed-in memos tab, live opencode memo
   delivery, ring-notification middle-click, first source added in the browser (then raise the sources backup
   line floor to 1), a stored transcript in a real browser. forcing: user — the operator's own checks
6. Retire the invitation half once the deployed `invites` table has zero open rows (at most 7 days after the
   fold deployed): revoke route, redeem arm, the redirect, `invite.Store.Create`; a route-ledger change.
   forcing: user — follows from the operator's option A

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

- **Operator decisions after the lanes reported:** invite fold = A; relay design for task-board agents; sources
  journal on a dedicated volume; audit-refs reports "could not check" when any source was unread; retire the
  old env names on hosts and store pod; Q16 = keep clients current; arm one host, accept the held history,
  storage as proposed, no plugins yet; do not rotate the bridge token; do not make the touch job required.
  via: command
- **A proposed port goes stale between plan and deploy** — the receiver's planned relay port had been taken
  by another service by go-live; decode the LIVE gateway's listen sockets immediately before using one.
  via: measurement
- **A new volume or object must move the deployment repo's phase-1 census test in the SAME commit** — it is
  not in the pre-push gate (which passed), so trunk went red until the ledger line was added; run it inside
  the repo's dev shell (outside, a missing module makes it fail for a reason that measures nothing).
  via: measurement
- **A CI job that exits 2 is "could not vouch"** — re-run the same commit before debugging the diff: the S10
  failure was the dualrun flake above. via: measurement
- **Lanes stall silently**: three times a lane sat "waiting" for hours on a worker that had died (API limits
  or otherwise). The status file's last timestamp against the clock is the tell; unpushed commits in the
  lane's worktree are where the work is. via: measurement

## How to verify
```bash
gh pr list -R ZacxDev/cairn --state all --limit 30 --json number,title,state,mergedAt
git -C <deployment-repo> log --oneline -10 origin/trunk -- <cairn manifests>   # deployed pins
bash tests/memo/live-probe.sh ~/.config/cairn-memo-probe/probe-input           # SUMMARY … exit=0
```
## Defects (batched)
- 🟡 The tooling repo's sharded CI: shard 1 is red on every PR (five socket-path overflows in a test file under
  the shard temp dir; go/node legs post "no verdict" though they pass). A one-line fixture fix was opened and
  CLOSED unmerged by someone else; its branch still exists.
- 🟡 `tests/dualrun` self-test is flaky: `audit-identity-field-dropped` once also tripped the `process`
  comparison (mutants=7 caught=6, exit 2); the same commit passed on re-run.
- 🟡 The personal-agent bridge credential's first ~16 of 43 characters were printed into the coordinator's
  transcript. Operator chose NOT to rotate. Rotate if that transcript is ever shared.
- 🟡 Transcripts: ClickUp custom-id rule drops `fix-ALPHA-12`-style branch names silently; the JSON read routes
  answer 500 on an unparseable `meta.json`; "My sessions" reads every `meta.json` per request; one cap-sized
  record allocates ~253 MiB in the re-check walk; no fresh held-back redaction score exists (last 157/190).
- 🟡 Transcript agent decisions open for operator review: an arc toggled ON reads the arc's LIVE members (a
  writer of the home scope can enrol a session); every transcript read makes the reading session owner-only;
  a pod re-check refusal becomes a placeholder; plain http is allowed only for one named mesh host.
- 🟡 Webhook receiver: a batch is lost if its summary-only retry is refused; unusual-space normalisation runs
  after credential scrubbing; slow clients can hold every request slot; ClickUp secrets are not
  whitespace-trimmed; the public hostname does not require the CDN path.
- 🟡 Invite fold: only one of the two redeem paths is guarded with a link store wired.
- 🟡 Task-board relay: the token is read once at start (a fixed Secret needs a restart); delivery is once per
  agent, not per chat session; only one agent runtime receives memos.
- 🟡 The tooling repo's frozen Python fork of the client can no longer read the renamed env files (dead code;
  remove at P8). `cairn doctor` exits 9 on both hosts (`token-scopes`; one host also `frozen-mirror`) — not
  investigated.
- 🟡 A deployment-repo image-node test is red on its trunk from another session's workload.
- 🟡 Nothing in CI notices a self-test sabotage that stops applying.
