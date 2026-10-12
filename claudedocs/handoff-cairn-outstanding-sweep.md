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
- Audit that produced this list: four earlier sessions' operator messages, each request checked against
  merged PRs, the deployment repo's pins and the arc handoffs. Scope-mail arc itself is CLOSED (#248).
- Deployed at the start of this sweep: `sha-f162b54` (both pods). `main` was `be6662e` (#247 JWKS backoff
  and #181 charts merged, NOT deployed).
- **Eight lanes dispatched as background subagents, all IN FLIGHT** (each in its own worktree, claims
  via `claim-work`, progress lines in the coordinator session's scratchpad `status-<lane>.md`):
  1. transcripts+plugins — AP1–AP3, S4, S11, S7, S8, S5, S6, S9, S10; prepares capture arming.
  2. scope-refs — S3 (`sources` verb), S4 (edit form), S5 (`audit-refs` + e2e); prepares the
     source-journal deployment change.
  3. scope-mail-fixes — `refused` memo status; #246 test row; `memo-send --help`; `run.sh:278`;
     closing-condition part 3 re-anchor; then in the tooling repo the hook's three #2148 🟡s + the
     fingerprint 🟢 + `refused` handling, and `cairn`/`handoff`/`resume` skills pointing at `cairn-memo`.
  4. ui-defects — invite fold into team links; port-picker race; "one script" comments; mobile S6b
     (blocking `uiaudit-touch`); stale CDPATH note; populated memos-tab walk capture.
  5. deploy-wave0 — deploy `main` (#247, #181, #248) + rename deployment env `CAIRN_SUPABASE_*` →
     `CAIRN_OIDC_*` per `internal/envalias` pairs; live probe after.
  6. tooling-misc (tooling repo) — a cairn repo handle for `find-session --arc`; the presence S3
     stop-signal deadlock.
  7. memo-agents — task-board agents receive memos; personal agent's bridge gets memo read + send.
  8. memo-webhooks — new receiver service (ClickUp, Discord, Signal → memos) in the deployment repo.
- Not verified by anyone yet: everything above (lanes still running when this was written).

## Next steps (ranked)
1. Collect lane 5's result (deploy-wave0); confirm running imageID == tag digest and probe exit 0.
   forcing: user — operator asked for merge+deploy per wave
2. Relay every BLOCKED-ON-OPERATOR item from lanes 1, 2, 7, 8 (credential issuance, migration
   deploys, ClickUp/Discord registrations) to the operator in ONE batch with exact commands.
   forcing: user — operator requires OK for credentials and migrations
3. Deploy wave 1: once lanes 1–4 have merged, deploy cairn `main` by the recorded recipe (backup
   jobs, 200-vs-404-control tag check, digest match, live probe); a migration in range stops for OK.
   forcing: user — operator asked for merge+deploy per wave
4. 🔴 Deploy order for `refused`: the tooling repo's hook that understands `refused` must be on both
   hosts BEFORE a cairn client emitting it reaches them — lane 3 decides and records the order.
   forcing: user — operator chose the new status
5. Arm transcript capture (lane 1's prepared flags + capture-token commands) after operator OK.
   forcing: user — operator: ship capture now, redaction quality accepted
6. Configure the scope-refs source journal on the deployment (lane 2's prepared diff).
   forcing: user — operator asked for scope refs
7. Issue the task-board / personal-agent / receiver credentials and register webhooks (lanes 7, 8).
   forcing: user — operator asked for all four external senders
8. Operator-only checks still open: signed-in Team flow (mint + redeem a team link), iPhone install
   checklist (gates client-instance install), signed-in memos tab, live opencode memo delivery,
   ring-notification middle-click. forcing: user — named as the operator's own checks

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

## How to verify
```bash
gh pr list -R ZacxDev/cairn --state all --limit 30 --json number,title,state,mergedAt
git -C <deployment-repo> log --oneline -10 origin/trunk -- <cairn manifests>   # deployed pins
bash tests/memo/live-probe.sh ~/.config/cairn-memo-probe/probe-input           # SUMMARY … exit=0
```
