# Handoff: cairn-ui-hub-team

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
Operator's browser-surface rework: `/` as a hub (Arcs/Scopes/Sessions/Team cards), the scope list
on `/scopes`, a `/sessions` list, scope/entry page polish, a "What an agent sees" recall tab, and ONE
Team page carrying sharing + invitations + multi-target team links (TTL, reuse, revoke, audit).
- **closing-condition:** `check` — PRs #213 and #214 are MERGED on `main` (verified by content:
  `internal/ui/hub.go` and `internal/ui/team.go` present on `origin/main`).

## State now
- 🔴 **ARC CLOSED (2026-10-10): the closing condition is MET** — #213 (`23a7f21`) and #214
  (`197dd0e`) are MERGED; `internal/ui/hub.go` and `internal/ui/team.go` are on `origin/main`.
  Anything below is a NEW arc, not another round of this one.
- **#213 DEPLOYED** to the personal instance: both pods `sha-23a7f21` (deployment-repo trunk
  `858890362`); rollout + new stylesheet hash + `cairn sync` observed. 🔴 NOT verified signed-in
  (every page but `/sign-in` and the manifest is 401 without a session) — operator's check.
- **#214 MERGED, NOT DEPLOYED.** It absorbed `main` twice (after #215, then after #220 → merge
  `1d969b2`: Team section renamed Phase T, `app.css` + the three install screenshots regenerated,
  mutant count re-derived to 339, and the S4 Team shortcut repointed `/share` → `/team` because
  #214's own ledger refused the old target). That merge commit got its own delta audit (CLEAN) and
  a fully green CI run before the squash; verified by content (60 files equal the audited head).
- Claim `cairn-team-link` RELEASED.

## Next steps (ranked)
1. **(new arc) Deploy #214 (+ #220's S4) to the personal instance** — needs the operator's go: it
   runs pgstore migration 2 (rollback recipe on the PR), and the #213 approval does not cover it.
   Strip the `/join` query string from gateway access logs before arming reusable links.
   forcing: user — awaiting operator.
2. **(new arc) Fold single-project invitations into team links** — a separate PR after #214 soaks.
   forcing: user — operator chose "fold later".
3. **(done) #214 merge** — merged `197dd0e`. forcing: user — operator asked for the Team page.

## Gotchas / decisions / dead-ends
- **A reusable team-link token travels in `GET /join?invite=…` and lands in gateway access logs by
  default** — strip the query string from access logs for `/join` (deployment repo) before arming
  reusable links. via: code
- **#212 (`feat/instance-name-and-display-name`, another session's) touches the same UI files** —
  whichever merges after the other must rebase and re-run the merged tree. via: command

- **The base-clone write guard cannot resolve `git -C $VAR`, nor a worktree path that does not
  exist yet** — it refuses both. Spell paths literally, and create the worktree in its own
  command before writing into it. via: command
- **The UI pods answer 401 to every unauthenticated page**, so an unauthenticated probe can prove
  a deploy (stylesheet hash on `/sign-in`) but never a feature. via: command

- **A merge that resolves a ledger conflict is a code change** — the #220→#214 merge had to edit
  `appShortcuts`, regenerate CSS and screenshots and re-derive counts; it was audited as its own
  delta rather than on the two parents' clean ladders. via: change
- **Two control-mutant battery runs on one loaded host disagree** — a row labelled EQUIVALENT was
  scored killed in a full run and survived 3/3 alone; read CI's `go` job, not a contended local
  run. via: measurement
## Defects (batched)
- `internal/control/README.md` says no whole-battery run on the merged tree is recorded; CI's `go`
  job on `1d969b2` was that run (green) — replace the sentence with its `mutants=339 …` line.
- `cmd/cairn-ui`'s `aPortNothingIsListeningOn` can hand out the same port twice (measured 17 in
  200,000 pairs), which makes `TestATokenFileContentProblemLeavesTheBrowserServingAndTheAgentStopped`
  flake under load — keep the two picked ports distinct, or assert on WHAT answers.
- `internal/ui/render.go` (~:496) and `routes_test.go` (~:99) still say the surface has ONE script;
  S4 made it two.
