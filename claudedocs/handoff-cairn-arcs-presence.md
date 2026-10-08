# Handoff: cairn-arcs-presence — 2026-10-07

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
Build the operator's arcs-first page and owner-only tmux presence with a ring-the-bell
button, as designed in `claudedocs/plan-cairn-arcs-presence.md` (merged #194 — read it
before any slice: decisions, threat model, slices S1–S5, the deploy preconditions).
- **closing-condition:** `check` — slices S1–S5 of `claudedocs/plan-cairn-arcs-presence.md`
  are MERGED on `main` (verified by content, not ancestry), AND both runnable checks the
  plan's closing condition names exit 0: cairn's `tests/presence/e2e.sh` (queue, owner and
  host semantics up to "claimed by the right agent token", with its `--self-test` reddening
  every clause) and the tooling repo's S3 executor test (claim → BEL on a private tmux
  server). ADDRESSED ⇒ arc CLOSED.

## State now
- **S1 DONE and LIVE** — #197 merged as `45ef3d9`, deployed on BOTH instances at `sha-45ef3d9`
  (personal via deployment repo trunk `47f387f`; client via the client infra repo trunk
  `ebf1d70`, tag+digest pinned: store `sha256:f35f7061…`, ui `sha256:37fbdc9e…`). Rollback
  either: both image lines back to `sha-6edcb45` (client digests `fdcf67b6…` / `3fe4386a…`).
  Verified live on the personal pod over a port-forward (`/arcs`, `?all=1`, `/arc` tabs, 404).
- **S2 MERGED, NOT DEPLOYED** — #198 squash-merged as `123d771`,
  content-verified (`git diff 69b3e10 origin/main -- internal cmd tests .github` empty). CI at
  head `69b3e10`: all 8 checks green; `go` job `packages with passing tests: 24`,
  `SUMMARY mutants=257 killed=255 survived=2 misattributed=0` (the 2 are the labelled
  equivalents; all 31 `presence-*` rows killed). Audit ladder: round 0 (D1 kept, D2/D3
  deleted), round 1 (2🟡 3🟢, fixed in `69b3e10`), round 2 delta CLEAN ⇒ ladder ended. Claims
  blocks are PR comments on #198.
- **S2 deploy precondition — the TTL half is measured, the journal half is not.** #195's image
  (`sha-6edcb45`) commits: personal deployment repo `8d37487`; client
  infra repo `1c6145b` (read each commit time with `git log -1 --format=%cI <sha>`; the
  rollout itself lags by minutes and was not observed). Neither manifest sets
  `-session-ttl`/`CAIRN_UI_SESSION_TTL` ⇒ 12 h default ⇒ **earliest S2 deploy = the LATER of those
  two commit times + 12 h, plus margin for the rollout lag**. The journal re-check (no credential with non-null
  `narrowed_scopes`) has NOT been run.
- `claim-work` slug `cairn-arcs-presence-3` is still HELD (deploy outstanding).

## Next steps (ranked)
1. **#196 is MERGED** (`78fe99a`): nothing to do; kept for numbering. forcing: gate — kept for numbering.
2. **S1 is MERGED and DEPLOYED** (#197, `45ef3d9`). Nothing to do; kept for numbering.
   forcing: user — done.
3. **S2 deploy** (#198, `123d771`) — once the TTL half in State now has elapsed: re-read each instance's control
   journal for non-null `narrowed_scopes`, then bump both pods' images to `sha-123d771…` in the
   deployment repo (the store and UI Deployment manifests) and the
   client infra repo (tag+digest pinned), confirm the publish run's head sha is `123d771` before
   reading digests. S2 is inert with no presence flags set; enabling presence on the personal
   instance (flags, token file, `-presence-owner`) is a separate, deliberate manifest change.
   Then `claim-work --release cairn-arcs-presence-3`. Then S3 (tooling repo host agent), S4
   (badges), S5 (bell).
   forcing: user — the operator asked for tmux identity and a bell button, and for S2 to deploy only after the #195 TTL precondition.

## Gotchas / decisions / dead-ends
- **#195 adds `control.Authorization.Narrowed()`.** The plan's decision 11 derives the
  bearer narrowing bit UI-side from the credential row because no such accessor existed
  when it was written; S2 should use `Narrowed()` instead and update decision 11. via: code
- **The membership ledger test claims only what it covers**: direct, aliased, method-value
  and interface-helper uses. Five shapes still pass it (embedded struct, generic helper,
  package-level func literal, type assertion on an imported value, a locally declared
  interface) — listed in `internal/ui/README.md`; the behavioural tests guard the real
  call sites. The audit ladder stopped there after two scaffolding-only rounds. via: measurement
- **Two PRs adding mutant-battery rows conflict on the count** in `ci.yml` and
  `internal/control/README.md`; resolve by COUNTING `Mutant(` rows in the merged file, and
  read CI's `SUMMARY mutants=` line before trusting a README figure. via: command
- **Verifying the UI without a browser**: port-forward `deploy/cairn-ui` and `curl -H @<0600
  file>` carrying `Authorization: Bearer` from `~/.config/subsystem-store/cairn-ui-operator.token`;
  proves the origin's rendered bytes, not script behaviour. via: command
- **The base-clone write guard refuses `git -C $VAR` it cannot resolve, and a worktree path
  that does not exist yet** — spell paths literally and create a worktree with
  `worktree add -b <branch>` in one step. via: command

- **A ruling formula can be wrong while its stated goal is right** — the round-2 ruling for
  #197 said `utcDay(lastUpdated) >= utcDay(now) − 14`, which would hide a 14d23h registration
  reading "14d ago"; the implementer built the goal (`⌊Δ/24h⌋ ≤ 14`) and said so. Brief the
  GOAL plus the test cases, and treat the formula as a suggestion. via: measurement
- **zsh history modifiers bit a digest lookup**: `"…/$img:sha-…"` expands `:s` as a modifier
  (`bad substitution`); brace it, `${img}:sha-…`. via: command
- **A scheduled merge-on-green loop must check the publish run's head sha equals the merge
  commit** before reading digests — the publish workflow lists the newest run, which can be
  another push. via: command

- **S2 uses `control.Authorization.Narrowed()` (#195, `internal/control/resolve.go:78`)**, not the
  plan's decision-11 credential-row derivation; the S2 PR is briefed to rewrite decision 11 to
  describe what was built. A reviewer should check the edit landed. via: code

- **A token-file CONTENT error at startup no longer exits `cairn-ui`** (#198 round 1): the
  browser keeps serving, the agent listener stays OFF, and the only signal is a stderr WARNING
  plus hosts seeing connection refused. Flag misconfigurations and an unreadable file still exit
  78. When enabling presence, check the startup log, not just pod health. via: code
- **`audit-dispatch.py --emit-claims` only prints the block** — post it yourself as an ISSUE
  comment (`gh pr comment`); the next `--round` reads only those. via: command

## How to verify
```bash
git fetch origin && git cat-file -e origin/main:internal/ui/arcsindex.go && echo s1-merged
gh pr view 197 --repo ZacxDev/cairn --json state,mergeCommit --jq '"\(.state) \(.mergeCommit.oid)"'
# live: port-forward deploy/cairn-ui (personal) and GET /arcs, /arcs?all=1, /arc?home=…&slug=…&tab=sessions
# with an Authorization: Bearer header read from a 0600 file; expect 200s and an unknown arc → 404
```
## Defects (batched)
- `internal/presence/wire.go:24-30`, `internal/ui/README.md` presence section and #198's body
  say the worst-case legal push is 638,245 B; round 2 measured a legal 662,111 B push
  (`last_activity` may be a 128-byte RFC 3339 value with a long fraction). 1 MiB still admits
  it; fix the number (and `TestAWorstCaseLegalPushIsAccepted`'s fixture) in the S4 PR.
