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
- ✅ **VERDICT: ADDRESSED — THE ARC IS CLOSED.** Both clauses met: (1) PR #191 squash-merged as
  `45df720` (content-verified: `internal/ui/filter.js` on `origin/main`; all 8 CI checks green
  on the merged head `12f7fda`); (2) on the deployed personal UI the scope rows are
  newest-first with an `… ago` `<time>` each (measured from the pod's served HTML: 26 root
  cards, 83 scope rows, both sorted descending), and the operator reported, signed in, that
  typing into the filter drops the row count.
- Deployed on both instances, both pods `sha-45df720`, 0 restarts: personal via deployment repo
  `trunk` `7bd2b9a`; client via the client infra repo's `trunk` `aa72e83`, tag+digest pinned.
  Rollback on either: both image lines back to `sha-ea9cfa7…` (client digests `ba216e99…` /
  `1e239549…`).
- The prior arc `cairn-arcs-sessions` is CLOSED; its two post-close items (client-instance UI
  after operator sign-in; the client backup's `arcs:` upload on its first real run) still wait
  on external triggers — see that doc. The client UI check now covers this rollout's image too.

## Next steps (ranked)
1. **None for this arc.** The client-instance UI check lives in `handoff-cairn-arcs-sessions.md`
   rank 1 and now covers this image as well. forcing: user — tracked there, not here.

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
