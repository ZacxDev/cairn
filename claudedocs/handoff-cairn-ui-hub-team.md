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
- **#213 MERGED** as `23a7f21` and **DEPLOYED** to the personal instance (2026-10-10, operator go):
  both pods `cairn-store-go` + `cairn-ui` bumped `sha-0355c7a` → `sha-23a7f21` in the deployment
  repo (trunk `858890362`). Both ghcr manifests 200 before the bump (control tag 404). Rollout:
  both Deployments 1/1 on the new image, 0 restarts, 0 error/panic lines in 10 min of logs; the
  `/sign-in` stylesheet moved `app.d3f3e82bfc13.css` → `app.70d3b32558c1.css` (new UI code is
  serving); `cairn sync` against the store pod answered. 🔴 **NOT verified signed-in**: every
  page but `/sign-in` and the manifest is 401 without a session, so the hub, `/scopes`,
  `/sessions`, the `/?q=` 303 and `?tab=agent` are unobserved live — operator's check.
- **#214 (`zach/team-link`) audit round 3 CLEAN** (0🔴 0🟡 0🟢, PR comment 16:48Z) — the ladder is
  over. Its CI run predated #215's merge, so `origin/main` (`76ddc7e`) was merged in → head
  `ec2f0e8` (clean merge; the one shared file, `ci.yml`, carries both #215's `ok < 25` floor and
  #214's 334-mutant count). CI re-running on that tree. NOT deployed; deploying runs pgstore
  migration 2 (rollback recipe on the PR).
- ⚠ A stale agent worktree (`.claude/worktrees/agent-abcb090ebcfc6b8d2`, clean, at `6faa6a5`)
  still checks out `zach/team-link`, so the branch cannot be checked out elsewhere — work detached.
- Claim held: `cairn-team-link`.

## Next steps (ranked)
1. **#214: merge once CI's `go` job is green at `ec2f0e8`** (the `uiaudit` hub-502 red is
   non-blocking), verify by content (`internal/ui/team.go` on `origin/main`), release
   `cairn-team-link`. forcing: user — operator asked for the Team page.
2. **Deploy #214 to the personal instance** — needs the operator's go (it runs migration 2); the
   #213 deploy approval does not cover it. forcing: user — awaiting operator.
3. **Fold single-project invitations into team links** — a separate PR after #214 soaks.
   forcing: user — operator chose "fold later".

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
