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
- **DEPLOYED to the personal instance at `sha-197dd0e`** (both pods; deployment-repo trunk
  `bbb6055`, operator go) — carries #213, #214 AND #220's S4. Pre-deploy backup (a manual run of the `cairn-ui-backup` CronJob, newest archive in
  `cairn-ui-daily/`, restored+verified). Observed after rollout: migrations
  `1` → `1,2`, `team_links` table present (was absent); manifest shortcuts Arcs `/arcs`, Search
  `/scopes?q=`, Team `/team` (was none); stylesheet `app.70d3b32558c1.css` → `app.f531650813b2.css`;
  cairn-ui logs "serving 48 route(s)", no error lines; store pod 0 panics, `cairn sync` live.
  🔴 NOT verified signed-in (the Team page, minting/redeeming a link) — operator's check.
- **ROLLBACK:** an older cairn-ui refuses a version-2 database — run
  `DELETE FROM schema_migrations WHERE version = 2;` BEFORE reverting the image (recipe on #214).

## Next steps (ranked)
1. **(new arc) Keep reusable-link tokens out of access logs BEFORE minting one** — `GET
   /join?invite=<token>` is written by the default nginx `access_log` on BOTH relay hops (the public
   gateway and the cluster-local gateway, both `http` blocks with no `access_log` directive),
   and Cloudflare sees the full URL too. Operator decision pending: app-side fragment
   (`/join#invite=…`, covers every hop incl. Cloudflare) vs nginx `access_log off` on `/join`.
   forcing: security — a logged reusable token is a standing enrolment capability.
2. **(new arc) Fold single-project invitations into team links** — a separate PR after #214 soaks.
   forcing: user — operator chose "fold later".
3. **(done) Deploy #214 + S4** — `sha-197dd0e`, trunk `bbb6055`. forcing: user — operator go.

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
- The cairn-ui Deployment still sets `CAIRN_SUPABASE_ISSUER`/`CAIRN_SUPABASE_JWKS_URL`; the pod warns
  they are deprecated aliases for `CAIRN_OIDC_*` — rename in the deployment repo.
- `internal/control/README.md` says no whole-battery run on the merged tree is recorded; CI's `go`
  job on `1d969b2` was that run (green) — replace the sentence with its `mutants=339 …` line.
- `cmd/cairn-ui`'s `aPortNothingIsListeningOn` can hand out the same port twice (measured 17 in
  200,000 pairs), which makes `TestATokenFileContentProblemLeavesTheBrowserServingAndTheAgentStopped`
  flake under load — keep the two picked ports distinct, or assert on WHAT answers.
- `internal/ui/render.go` (~:496) and `routes_test.go` (~:99) still say the surface has ONE script;
  S4 made it two.
