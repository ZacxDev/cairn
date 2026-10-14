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
- **Transcript capture is ARMED on the personal instance, for BOTH hosts.** First host as before (its timer's
  incremental uploads are now observed). Second host: tooling gate changed to enable the timer there too (still
  fail-closed per host), quota raised for two hosts, its own capture token minted straight into place; full dry run
  171 sessions, 137 held (80%, the first host's accepted share was 75%), first armed pass shipped 34 with 0 refused;
  redaction spot-check on its stored bytes: token prefix 0, no bearer followed by a token-length value (content
  control: 14,870 `tool_use` lines); its timer's passes upload incrementally. Details: the deployment repo's capture
  handoff and `claudedocs/handoff-cairn-transcripts.md`.
- **All lanes DONE and merged:** transcripts+plugins (#258, #260, #261, #266, #268, #269, #270, #271, #272,
  #273, #274, #275, #276); scope refs S3–S5 (#250, #255, #259) with the source journal live on a DEDICATED
  volume (#264); audit-refs "could not check" (#267); scope-mail fixes and the `refused` status (#251, #257);
  ui-defects (#252, #253, #254, #256, #262, #263; mobile-pwa arc closed); invite fold option A (#265);
  tooling-misc; env-name retirement on both hosts and the store pod.
- **External memo senders:** the personal agent's bridge reads and sends memos (credential live, one read
  proven 200); the task board RELAYS memos to its agents with one read-only credential for 13 scopes — relay
  ON, but its token has made NO request yet (no agent with a task was running); the webhook receiver is
  LIVE for ALL THREE of its sources — see the next bullet.
- **Webhook receiver: ALL THREE sources LIVE, ClickUp PROVEN end to end.** The CDN zone's bot-fight mode was
  challenging every ClickUp delivery (403) and no skip rule can bypass it on the free plan, so it was turned
  OFF zone-wide (operator decision; read back `fight_mode: false`; rollback = `fight_mode: true`). The webhook
  was reactivated (`fail_count` 0); the first real delivery answered 200 within a minute, the receiver
  counted 5 received / 5 queued for the ClickUp route's scope, and one batch memo ("5 events", summaries and
  links only) landed in that scope after the 10-minute window — `memos_sent` 12 → 13, `fail_count` still 0.
- **New cairn-server operator commands, merged and used live:** `-create-scope` (#279, created the DM scope)
  and `-remove-member` (#280). Both host agent credentials were re-issued to include the new scope and their
  predecessors revoked.
- **Since then:** the first REAL DM digest landed in the DM scope; this workbench host is switched; the docs
  pass merged (#282) and the repo description/topics were updated.
- **New receiver source for the personal agent's conversations: LIVE and PROVEN end to end.** Image 0.3.0
  (deployment repo #1010, #1011, #1013, #1016 code; #1012 its own read-only archive identity, proven HEAD 200 /
  PUT 403; #1015 go-live). Every thread routes to a NEW scope `muse` (operator decision), count + title + link
  only. The first REAL batch landed: memo "6 new turns in … thread …" via the receiver credential, after the
  10-minute window. `/healthz` shows the poller's last-good time every minute (credential + network proven).
  Each merge followed an audit round with no 🔴; mutant batteries re-run independently each time.
- **Credentials for `muse`:** scope created in the operator's project; the receiver credential and BOTH host
  agent credentials re-issued with it, each proven (probe 201 / send+read+retract per host), predecessors
  revoked; deployed receiver token digest-checked. The client routing row merged and BOTH hosts were switched
  with the ship script on one sha (the laptop's unpushed one-line text-expander commit went through its own PR
  first, then the laptop rebased cleanly onto main).
- Not verified: the relay's first real request and an
  end-to-end relayed memo; a real-browser read of a stored transcript; a second signed-in principal being
  refused a transcript; the second host's hook/client beyond the ship script's report and a store-path match.

## Next steps (ranked)
1. DONE — webhook receiver go-live: all three sources live, ClickUp proven end to end (State now). Kept as a
   numbered row so ranks 2–8 keep their claim ids. forcing: user — operator asked to go all the way to live
2. DONE — new receiver source (the personal agent's conversations): live, first real memo delivered (State now).
   forcing: user — operator asked for it at handoff
3. DONE — transcript capture armed on the second host (State now).
   forcing: user — operator: arm it fully
4. Transcript backup: DESIGN, then ask before building — it must honour 90-day retention and owner deletion
   (the existing job stages through a small scratch volume and would outlive both) and fit a 20 GB store.
   forcing: user — operator: design, then ask
5. Tooling repo CI: REOPEN and merge the closed shard-1 fixture fix (branch `fix/tmux-reply-test-socket-path`
   — `/resume` reports it GONE locally; check the remote before assuming it was deleted; confirm it turns
   shard 1 green), and find why the go/node legs post "no verdict" while their logs pass.
   forcing: user — operator: reopen and merge
6. First dispatched task-board agent on a scoped task: confirm the relay's first listener request answers 200
   and one memo is delivered once, then retracted. forcing: user — operator chose the relay
7. Operator-only checks: signed-in Team flow, iPhone install checklist, signed-in memos tab, live opencode memo
   delivery, ring-notification middle-click, first source added in the browser (then raise the sources backup
   line floor to 1), a stored transcript in a real browser. forcing: user — the operator's own checks
8. Retire the invitation half once the deployed `invites` table has zero open rows (at most 7 days after the
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

- **Closing-condition verdict for the sweep: MET** — every ranked item was merged or recorded as waiting on
  the operator, and the final wave's image runs with the memo probe exiting 0. The ranked list above is the
  operator-unblocked tail, taken in a follow-on session; it does not reopen the merged work. via: command
- **Operator decisions at handoff (paraphrased):** receiver order Signal → Discord via the browser bridge →
  ClickUp, plus the personal agent's conversations as a new source; transcript backup is designed first and
  approved before building; the second host is armed fully; the closed tooling-repo fixture fix is reopened
  and merged. via: command
- **Receiver credentials are narrowed to their routes' scopes**, so every added route means re-issuing or
  widening the credential — batch the routes per source before issuing. via: code

- **Operator decisions at receiver go-live (paraphrased):** both active chat conversations feed ONE scope, a
  third low-traffic one stays unrouted, no excerpts; ClickUp = one list on the shared workspace, to a second
  scope; Discord = every received DM into one scope via a browser-fed source (build approved); one
  secret-scan baseline entry for the ClickUp webhook id (it is a route key, not a credential). via: command
- **A narrowed credential cannot be widened** — `cairn-server` has `-issue-credential` and
  `-revoke-credential` and nothing between, so each new routed scope is a replace-prove-swap-revoke cycle.
  via: code
- **The receiver pod could not read its own token**: a Secret volume mounts root-owned and at mode 0400 a
  non-root pod without `fsGroup` gets "Permission denied", which the receiver reports exactly like "no token".
  Two throwaway pods on the same Secret (sizes only) separated the cases; `fsGroup` fixed it. via: measurement
- **Time the 30 s credential wait from the issue line's own timestamp.** A probe sent 14 s after issuance
  answered 401 (one lockout strike); the same token answered 201 from a fresh pod at 73 s. via: measurement
- **`memo-retract --id` takes the bare number**, not the `m-<n>` that `memo-read` prints. via: command
- **Check what a "configure it" step can actually see before configuring it**: the Discord step was written as
  application setup, and one question to the operator showed the wanted input (all DMs) is outside anything an
  application receives. via: command

- **Operator decisions this session (paraphrased):** DMs to a NEW scope (`inbox`), so build `-create-scope`;
  remove the synthetic test member, so build `-remove-member`; receiver path mesh-only, not public; trim
  per-DM routes, the link field and the receiver-side group check; no-browser is quiet; accept that the
  watcher may mark DMs read; re-issue both host credentials to read the new scope; add the Cloudflare skip
  rule. via: command
- **A credential narrowed at issue never sees a scope created later** — `inbox` was invisible to both host
  credentials until they were re-issued with it. via: measurement
- **A proxied public webhook host silently blocks cloud senders** — ClickUp suspended its webhook at 100
  failures with ZERO requests reaching the private cluster gateway, while a residential curl through the same host
  answered 401 from the receiver. The zone already had the same skip rule for another service's API.
  via: measurement
- **Operator-run journal commands go in a one-off pod on the store image mounting the UI volume with
  `CAIRN_CONTROL_JOURNAL` set** — not `kubectl exec`: the store pod sets no journal env and the UI image
  has no `cairn-server`. via: command
- **The client honours `CAIRN_ROUTES=<file>`** — enough to read a new scope before a `home-manager switch`
  lands its routing row. via: command

- **A timing test can redden a docs-only PR's `nix` job**: `TestMemoScopesIsLinearOnUnterminatedBlocks`
  measured 25.6× growth for 8× input on a loaded runner (bound between 8× and 64×); re-running the same
  commit passed. Re-run before debugging the diff. via: measurement

- **A CDN skip rule does NOT bypass free-plan bot-fight mode — RETRACTS the earlier "fixed by a skip rule".**
  The rule skips the super-bot-fight phase, which only exists on paid plans; the firewall events name
  `botFight` as the source of every ClickUp 403, while the same rule demonstrably matched our probes.
  via: measurement
- **"A cloud sender is blocked" was too broad**: unsigned POSTs from a datacenter (Hetzner) IP with three
  user agents all reached the receiver (401, no `cf-mitigated`); bot-fight mode flags ClickUp's AWS
  `axios/0.24.0` traffic specifically. A datacenter curl passing proves nothing about a particular sender.
  via: measurement
- **Prove the non-curl Loki filter can see a line before trusting its zero** — a POST with UA `ClickUp` from
  the workbench appeared as 1 line in 15 min; real deliveries 0 in 48 h. The PROD gateway's nginx (the hop
  before the private cluster) showed the same 0, which is what placed the drop at the CDN edge. via: measurement
- **The three sops-held CDN tokens in the deployment repo are ONE DNS-only token** (zones read only; no
  rulesets, settings, analytics). The account token in the workbench's home is the one that reads rulesets
  and now zone analytics too; it still cannot read `bot_management`. The analytics GraphQL
  endpoint is `/client/v4/graphql`, and the free plan refuses the `clientASNDescription` field. via: command
- **The ClickUp webhook can be read without printing the token**: the `clickup` skill's `accounts.json`
  (`bot` account) → `GET /team/<id>/webhook`, filter by endpoint. `date_suspended` is epoch MILLISECONDS.
  via: command

- **Operator decision: the CDN zone's bot-fight mode is OFF, zone-wide** — chosen over DNS-only for the hook
  host and over a paid plan. Every proxied host on the zone lost it; each still authenticates its own callers.
  Turning it off needed a token with zone Bot Management EDIT (added by the operator). via: command
- **The receiver BATCHES**: deliveries show up as `events_queued` at once, the memo only after the per-scope
  10-minute window from the FIRST event. An empty scope read right after a 200 is not a failure. via: measurement

## How to verify
```bash
bash tests/memo/live-probe.sh ~/.config/cairn-memo-probe/probe-input           # SUMMARY … exit=0
gh pr list -R ZacxDev/cairn --state open                                       # none left from the sweep
cairn sources --scope cairn                                                    # status=sources-undeclared until a source is added
systemctl --user is-active cairn-capture.timer                                 # active on the armed host only
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
- 🟡 Webhook receiver: an UNREADABLE token file is reported exactly like an absent one (the read error is
  swallowed); a ClickUp webhook id in a route trips the deployment repo's secret scan, so every new ClickUp
  route needs its own baseline entry.
- 🟡 `-create-user`/`-set-member`/`-remove-member` runbooks in `server/README.md` say `kubectl exec`, which
  cannot reach the deployed journal (see Gotchas). `-create-user -scopes` applies no name rule (accepts
  `..`); unchecked whether anything can climb out of the store with it.
- 🟡 Small open audit notes: the scope-created writer ledger skips every non-function declaration in
  `journal.go`; `-remove-member` prints "nothing was written" even after an Append I/O error; open legacy
  invitations are not re-checked against the inviter's current authority until they lapse.
- 🟡 Discord DM watcher: whether opening a DM marks it read is unmeasured (accepted); a status containing
  parentheses in a DM label makes it read as not-a-DM; an older ClickUp mutant survives ~1 run in 7 (map
  order); the deployment repo's pre-push gate intermittently reports a guard that "could not run".
- 🟡 The Cloudflare account token file on the workbench is mode 644.
- 🟡 Docs-pass leftovers: a `flake.nix` comment (~1362–1372) says `cairn-ui` makes no outgoing TLS connection
  (false: the OIDC backend fetches keys); four `cairn-server` refusal messages still say "the path this pod
  reads its journal from"; the scopeuse linearity test is load-sensitive.
- 🟡 Agent-conversation receiver source, last audit round (#1016, no 🔴): one test file still carries an
  audit-round name; a one-field per-thread struct could be a plain map; after purging the BRIDGE profile the
  shared read policy is gone too, so re-provisioning the receiver's profile then refuses (rc 8) and the hint
  does not say why. All cosmetic or hint-level.
