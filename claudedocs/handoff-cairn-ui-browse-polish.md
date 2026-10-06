# Handoff: cairn-ui-browse-polish — 2026-10-06

## Run this first — the index, one command
```bash
cairn recall --repo /home/zach/workspace/cairn
```
Terse pointers this doc does not carry, curated by past sessions and outliving it.
🔴 RECALL, NOT LIVE OBSERVATION — every line is a pointer to VERIFY, never a current
reading, and it may describe a gotcha already fixed. `scope-absent`/`scope-empty` means
nothing is recorded yet: ordinary, not an error, and not a clean bill of health.
Non-blocking: if it exits non-zero, print the stderr line and carry on.

## Goal
Apply the operator's browser-surface feedback: recency sort with relative timestamps on the
all-scopes and scope pages, a fuzzy filter over a scope's entries, less explanatory copy,
aliases/tags/refs as chips, and a clearer label for the journal section.
- **closing-condition:** `check` — PR #191 is MERGED on `main` (verified by content: `internal/ui/filter.js`
  present on `origin/main`), AND the deployed UI on the personal instance, signed in, shows a
  scope page whose rows are ordered newest-first with an `… ago` `<time>` per row and a filter
  box that narrows the row count when a query is typed.

## State now
- ⏳ **VERDICT: NOT ADDRESSED — one clause open: the filter NARROWING the row count when a
  query is typed, observed on the deployed UI.** Everything else in the closing condition is
  measured: PR #191 squash-merged as `45df720` (content-verified: `internal/ui/filter.js` on
  `origin/main`, `internal/` identical to the PR head `12f7fda`); the deployed personal pod's
  served HTML (operator token over a port-forward, not a browser) shows 26 root cards and 83
  scope rows each newest-first with `<time class="updated">` (`6m ago`, `22m ago`, `1h ago`…),
  the filter control rendered, exactly one script `/static/filter.0d69fda9244d.js` (served 200
  `text/javascript`, 3133 B, unauthenticated), "N history notes", the History heading with the
  literal heading as tooltip, alias/tag chips, an `updated` row, and NO legend/explainer on any
  of the three pages. The filter's typing behaviour is CI-only (uiaudit's real-browser test).
- **Audit:** round 0 → proceed (1 deletion candidate D1, kept: the copy-absence tests guard
  against an agent re-adding the copy); round 1 → CLEAN at `4eae012`, ladder ended. Two comment
  overclaims round 0 flagged were fixed in `12f7fda` (allowlist is a claim about the ORIGIN's
  bytes; five negative controls, not four). The PR body's stale legend sentence was corrected in
  a PR comment, not a silent edit.
- **Deployed, both instances, both pods `sha-45df720`, 0 restarts:** personal via deployment
  repo `trunk` `7bd2b9a` (rollback: both lines back to `sha-ea9cfa7…`); client via the client
  infra repo's `trunk` `aa72e83`, pinned by tag AND digest (store `sha256:1663000d…`, ui
  `sha256:62495665…`; rollback to `ea9cfa7` @ `ba216e99…`/`1e239549…`). APIs answer on both
  (`arcs --check` clean on personal; client `recall`/`arcs` answer from the client instance).
- The prior arc `cairn-arcs-sessions` is CLOSED; its two post-close items (client-instance UI
  after operator sign-in; the client backup's `arcs:` upload on its first real run) still wait
  on external triggers — see that doc.
- **mtimes are real on the deployed store** — the root page's times are minutes-to-hours old,
  not the seed time, so the "re-seed resets updated" caveat did not bite here.

## Next steps (ranked)
1. **Type a query into the scope page's filter on the deployed personal UI** (operator's
   browser, owned background tab, never raised) and watch "N of M entries" drop; that closes
   the arc. No browser profile was connected on this host this session (`whoami` →
   `connected: 0`). forcing: user — the operator asked for the filter.
2. **The client UI's signed-in check** is still blocked on the operator signing in there (the
   arcs-sessions doc's rank 1); this rollout changed its image too, so do both checks in one
   visit. forcing: user

## Gotchas / decisions / dead-ends
- **Ref pointers exist only per ENTRY** (`refs:`/`tasks:` front matter, `<system>:<id>`,
  resolved by `store.RefURL`); 4 of 350 entries in the personal cache carry any. A SCOPE has no
  repo/remote link anywhere — adding one needs new data and is a separate design. via: measurement
- **Relative time is rendered at page load** — no live ticking; the filter JS only reads
  `textContent`/data attributes and toggles `hidden`. Without JS every row shows.
- **mtime caveat**: a re-seed or a restore that does not preserve mtimes resets "updated".
- The base clone ran 8 commits behind origin/main at session start; editor diagnostics from it
  (undefined `hashAsset`, `PageView.Now`) were the STALE clone, not the branch. via: command
- The CSP is gone by operator decision (internal/ui/README.md), so the script needs no
  header change; served pages already carry Cloudflare-injected scripts downstream, which the
  allowlist guard (origin bytes) cannot see.

- **Verifying the UI without a browser:** port-forward `deploy/cairn-ui` and `curl -H @<file>`
  with `Authorization: Bearer <cairn-ui-operator.token>` (header from a 0600 file, never argv) —
  a header credential beats the cookie backend, and the response is the ORIGIN's bytes (no
  edge-injected scripts). It proves rendering and order, not script behaviour. via: command
- **The base-clone write guard judges a `git -C $VAR` it cannot resolve as the cwd's repo** and
  refuses; spell the worktree path literally. via: command
- **The client infra repo's pre-push gate needs pyyaml**; run the push inside
  `nix-shell -p 'python3.withPackages(p: [p.pyyaml])'` rather than `--no-verify`. via: command

## How to verify
```bash
gh pr view 191 --repo ZacxDev/cairn --json state,mergeStateStatus,statusCheckRollup
git fetch origin && git cat-file -e origin/main:internal/ui/filter.js && echo merged
# in the PR worktree / branch:
nix develop --command bash -c 'go vet ./... && go test -count=1 ./...'
python3 tests/leakscan.py --self-test && python3 tests/leakscan.py
```
