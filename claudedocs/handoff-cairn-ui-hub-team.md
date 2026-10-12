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
- 🔴 **ARC CLOSED** (closing condition met earlier: #213 and #214 merged). Below are follow-ons.
- **Team-link tokens now travel in the URL FRAGMENT** (#232, through merge train 2 → `9292da2`):
  minted links are `/join#invite=<token>`; `join.js` (the third allowlisted script, with its own
  spelling guard) reads the fragment, clears it with `history.replaceState`, fills the accept form
  and clears it again on a back/forward-cache restore; the user's click POSTs it. A REUSABLE token
  arriving as `?invite=` is refused; single-use `?invite=` links still redeem (transition).
- 🔴 **What actually protects a reusable token that was ever logged is REVOCATION BY ITS MINTER** (or
  expiry). The query/fragment mark is client-reported, so `?`→`#` or posting it in the `invite`
  field redeems it. At deploy `team_links` held **0 rows**, so nothing needed revoking.
- **Deployed:** personal instance at `sha-9292da2` (deployment trunk `faa94f1`), validated
  unauthenticated: `/join` serves `join.<hash>.js` (`text/javascript`, immutable) plus a
  `<noscript>` notice, a wrong hash is refused, migrations still `1,2`. 🔴 NOT verified signed-in —
  minting a link, redeeming it in a second browser, and the Team page are the operator's checks.
  ⚠ SUPERSEDED: the instance now runs `sha-f162b54` with the memos migration (version 3) applied —
  see `handoff-cairn-scope-mail.md`. #252/#253/#254 are merged, not deployed.
- **ROLLBACK (still binding):** an older cairn-ui refuses a database from a newer build. The database
  is now at version 3, so reverting below `sha-0d9ba01` needs the migration-3 recipe (#235) FIRST,
  and then, to go below #214, `DELETE FROM schema_migrations WHERE version = 2;` (recipe on #214).
- Claims: all released.

## Next steps (ranked)
1. **(done) Keep reusable-link tokens out of access logs** — fragment links, #232, deployed.
   forcing: security — a logged reusable token is a standing enrolment capability.
2. **Fold single-project invitations into team links** — a separate PR now that #214 has soaked.
   forcing: user — operator chose "fold later". 🔴 **BLOCKED ON AN OPERATOR RULING, not built:** a
   single-project invitation and a one-project single-use team link differ in three ways, and
   "fold" picks a side on each — (i) invitations may confer `owner` (an owner may invite an owner,
   `control.Role.CanConfer`); team links refuse `owner` by design; (ii) an invitation is listed to and
   revocable by EVERY manager of its project (`mayManage`); a team link only by its minter;
   (iii) open `invites` rows MAY exist in the version-1 table (not counted). **Recommended (no migration):** the
   Team page keeps ONE mint form (the team-link form, `?project=` preselecting that project);
   `POST /invite` stops minting and 303s to it; outstanding invitations stay listed, revocable and
   redeemable until they expire; a project owner can no longer confer `owner` self-service (no CLI mints
   invitations; the only other path is the operator's `cairn-server set-member`). Alternatives: widen team links to
   carry (i)+(ii) for one-project single-use links (an authz-model change), or a NEW migration (version 4 —
   3 is the memos tables) moving open invitations into `team_links` (stop before deploy). The older invite branches
   (`feat/ui-invite-flow`, `p6/invite-*`, `fix/rank-33-…`) are squash-merged via #139/#144, predate
   team links, and neither implement the fold nor conflict with it.
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

- **`join.js` fills the form but never auto-submits** — a mail scanner that runs script would
  otherwise start a sign-in for every link it sees. via: change
- **Every GET route needs a uiaudit walk class** (`plainGET` / `linkExpanded` / `notADocument` in
  `uiaudit/targets.go`): #232 added `join.js`'s route without one and `TestPWAClauses/e_storage`
  plus `TestTheREALLedgerIsFullyACCOUNTEDFor` went red until it joined `notADocument`. via: measurement
## Defects (batched)
- ✅ **CLOSED by #252** (`429e953`, test-only): `aPortNothingIsListeningOn` now HOLDS a bound,
  non-listening socket for the whole test instead of closing it (old shape: 38 duplicates in 200,000
  pairs; held: 0). 🟡 left: the agent address uses `127.0.0.2`, unconfigured by default on macOS —
  moot while `cmd/cairn{,-ui}` do not build for macOS (`st.Mtim`, `internal/report/entry.go`).
  The same close-then-reuse shape remains in `cmd/cairn-server/main_test.go` `freePort` and
  `uiaudit/payload_test.go` — not fixed.
- ✅ **CLOSED by #253** (`c8aab6b`): every "the surface has ONE script" claim now names three
  (`filter.js`, `pwa.js`, `join.js`); per-page "one script" claims, which are true, stay.
- The deployed cairn-ui still sets `CAIRN_SUPABASE_ISSUER`/`CAIRN_SUPABASE_JWKS_URL`, which it warns
  are deprecated aliases for `CAIRN_OIDC_*` — rename them in the deployment repo.
