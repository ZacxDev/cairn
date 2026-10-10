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
- **#213 MERGED** as `23a7f21` (hub, `/scopes` with `/?q=`/`/?tag=` 303 redirects, `/sessions`,
  scope/entry polish, `?tab=agent` byte-equal to `cairn recall` with a derived `head -60` mark).
  Audit ladder: round 0 (D1 drop hub arcs/sessions counts; D2 derive the banner count) → round 1
  CLEAN → round 2 CLEAN. **NOT deployed** — the operator has not approved deploying it.
- **#214 (`zach/team-link` @ `6faa6a5`) — audit round 3 RUNNING** (delta `015bb9d..6faa6a5`: the
  per-viewer Revoke form + project-name hiding fix). Operator decisions: the FORMS move onto `/team`
  (`/share`, `/invite` GET → 303); "reader" stays a read-only GRANT but project-wide grants are
  listed + revocable on `/team`; reuse = UNLIMITED until expiry/revoke; fold single-project invites
  into team links LATER. Rebased onto #213; battery 334 rows; pgtest 151/151. Rollback recipe is a
  PR comment (`DELETE FROM schema_migrations WHERE version = 2`).
- Claim held: `cairn-team-link`.

## Next steps (ranked)
1. **#214: read round 3; if clean, merge once CI is green** (the `uiaudit` hub-502 red is
   non-blocking), verify by content, release `cairn-team-link`. If not clean, one more fix round.
   forcing: user — operator asked for the Team page this session.
2. **Deploy #213 (+ #214 once merged) to the personal instance** — bump both pods in the deployment
   repo; #214 runs pgstore migration 2, so post-deploy rollback needs the recipe. forcing: user —
   awaiting the operator's go (asked, not yet answered).
3. **Fold single-project invitations into team links** — a separate PR after #214 soaks.
   forcing: user — operator chose "fold later".

## Gotchas / decisions / dead-ends
- **A reusable team-link token travels in `GET /join?invite=…` and lands in gateway access logs by
  default** — strip the query string from access logs for `/join` (deployment repo) before arming
  reusable links. via: code
- **#212 (`feat/instance-name-and-display-name`, another session's) touches the same UI files** —
  whichever merges after the other must rebase and re-run the merged tree. via: command
