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
- **ROLLBACK (still binding):** an older cairn-ui refuses a version-2 database — run
  `DELETE FROM schema_migrations WHERE version = 2;` BEFORE reverting the image below #214 (recipe on #214).
- Claims: all released.

## Next steps (ranked)
1. **(done) Keep reusable-link tokens out of access logs** — fragment links, #232, deployed.
   forcing: security — a logged reusable token is a standing enrolment capability.
2. **Fold single-project invitations into team links** — a separate PR now that #214 has soaked.
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

- **`join.js` fills the form but never auto-submits** — a mail scanner that runs script would
  otherwise start a sign-in for every link it sees. via: change
- **Every GET route needs a uiaudit walk class** (`plainGET` / `linkExpanded` / `notADocument` in
  `uiaudit/targets.go`): #232 added `join.js`'s route without one and `TestPWAClauses/e_storage`
  plus `TestTheREALLedgerIsFullyACCOUNTEDFor` went red until it joined `notADocument`. via: measurement
## Defects (batched)
- `cmd/cairn-ui`'s `aPortNothingIsListeningOn` can hand out the same port twice (measured 17 in
  200,000 pairs), which makes `TestATokenFileContentProblemLeavesTheBrowserServingAndTheAgentStopped`
  flake under load — keep the two picked ports distinct, or assert on WHAT answers.
- `internal/ui/render.go` (~:496) and `routes_test.go` (~:99) still say the surface has ONE script;
  there are now three (`filter.js`, `pwa.js`, `join.js`).
- The deployed cairn-ui still sets `CAIRN_SUPABASE_ISSUER`/`CAIRN_SUPABASE_JWKS_URL`, which it warns
  are deprecated aliases for `CAIRN_OIDC_*` — rename them in the deployment repo.
