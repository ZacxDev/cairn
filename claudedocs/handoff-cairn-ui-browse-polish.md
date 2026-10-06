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
- **PR #191 OPEN, not merged** — branch `zach/ui-browse-recency-filter`, head `458ef8a`,
  `mergeable=MERGEABLE`; CI was still running at handoff (only `leakscan` had finished, green).
  Built by a subagent in worktree `.claude/worktrees/agent-a76c7198a96027f59`.
- Operator decisions taken this session (AskUserQuestion): filter is CLIENT-SIDE JS (operator
  chose it over a no-JS `?q=` form); "updated" = entry FILE mtime; journal section displays as
  "History" (literal heading in `title=`), rows say "N history notes", entry page drops the
  count; drop entryWhat, the Aliases/Tags/Refs definition lines AND the root/scope legends.
- What the PR does (subagent's report; gates NOT re-run by the parent beyond `go vet` of
  `internal/ui` + `internal/report` in the worktree, rc 0): `report.FileMTime`/`report.NewerFirst`
  exported and shared by recall ordering and the UI; server-side relative time against
  `Config.Now`; `internal/ui/filter.js` served at a content-hashed `/static/filter.<hash>.js`;
  the zero-script claim replaced by an allowlist (`ui.AllowedScriptSources()`), guarded over
  rendered pages, uiaudit's walk, and a JS source ban (`innerHTML`, `fetch`, …); pinned-whole
  tests for RefsKeyDescription/TagsKeyDescription and battery row
  `ui-tags-key-description-loses-both-its-claims` deleted (battery now 200 mutants).
- Reported, unverified by the parent: go test 23 ok; run_go.sh 0 failures; leakscan clean;
  uiaudit 120 captures, 0 non-allowlisted scripts, 0 axe violations; 18/18 mutants killed.
  Full pytest NOT re-run after the subagent fixed the 3 mutant-count pins.
- Not deployed anywhere.
- The prior arc `cairn-arcs-sessions` is CLOSED; its two post-close items (client-instance UI
  after operator sign-in; client backup's `arcs:` upload branch on its first real run) still
  wait on external triggers — see that doc. Re-checked today: two client-routed scopes answer
  `no-arc-registered` (2 of 7 sampled).

## Next steps (ranked)
1. **Ask the operator** whether to also drop the scope page's `scopeWhat` explainer ("A scope
   is one directory under the store root…") and the entry page's collapsed "What am I looking
   at?" legend — both still render on #191 (seen in the subagent's screenshots).
   Repo cairn, `internal/ui/render.go`. forcing: user — the operator asked for the copy gone.
2. **Wait for #191's CI, then `/audit-pr 191`**, fix findings as one batch, re-run the full
   pytest suite. IN FLIGHT: ZacxDev/cairn#191. forcing: user — operator feedback being shipped.
3. **Merge, publish images, deploy the UI to both instances, verify signed-in** on the
   personal instance (operator's browser, background tab, never raised). forcing: user

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

## How to verify
```bash
gh pr view 191 --repo ZacxDev/cairn --json state,mergeStateStatus,statusCheckRollup
git fetch origin && git cat-file -e origin/main:internal/ui/filter.js && echo merged
# in the PR worktree / branch:
nix develop --command bash -c 'go vet ./... && go test -count=1 ./...'
python3 tests/leakscan.py --self-test && python3 tests/leakscan.py
```
