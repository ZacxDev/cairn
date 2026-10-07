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
- **Plan merged** (#194, `b4f349f`): five audit rounds, the fifth clean. Nothing built yet;
  each slice waits for the operator's go-ahead.
- **Shipped and deployed on BOTH instances at `sha-6edcb45`** (personal: deployment repo
  trunk `8d37487`; client: client infra repo trunk `1c6145b`, tag+digest pinned store
  `sha256:fdcf67b6…`, ui `sha256:3fe4386a…`; rollback both lines to `sha-64475d7`):
  - #192 raw entry view drops the front-matter provenance rows (`64475d7`).
  - #193 scope tabs (`?tab=entries|sessions|arcs` with counts), cross-scope `/session`
    page (uniform 404 for unseen/hidden ids, bullet anchors `b-<citation id>`),
    numbers-not-prose sessions/arcs views (`9fc8101`). Three audit rounds.
  - #195 browser sign-in REFUSES a narrowed credential (uniform 401, counts toward
    lockout); invite and share-candidate paths act as `membershipActor(id)` (a narrowed
    bearer could previously mint invitations into its owner's project); type-resolved
    membership ledger test (`6edcb45`). Battery measured in CI: `mutants=217 killed=215
    survived=2 misattributed=0`.
- **Verified live on the personal pod** (operator token over a port-forward, served HTML,
  not a browser): tabs "Entries 15 · Sessions 37 · Arcs 2", filter only on Entries, 37
  session links, session page 200 with trailer-free excerpt, its bullet link lands on an
  existing anchor, unknown session id 404, arc page links its member sessions. Client
  instance: pods on the new digests, API answers; its UI not viewed (operator not signed in).
- **Narrowed-credential refusal NOT exercised live** — neither instance has ever issued a
  narrowed credential (journals: 37 and 19 records, every `narrowed_scopes` null); covered
  by tests that are red on pre-fix `main`.
- **IN FLIGHT: ZacxDev/cairn#196** — the `go` job's `ok` floor `-lt 19` → `-lt 23`
  (re-measured 23 ok on `main`; gate replay: 23 passes, 22 refused).

## Next steps (ranked)
1. **#196 is MERGED** (`78fe99a`, all 8 checks green): the `go` job's `ok` floor is now `-lt 23`.
   Nothing to do; the rank is kept so later ranks keep their claim identities.
   forcing: gate — the floor stood 4 below the package count.
2. **S1 — the arcs-first page and `/arc` tabs (scopes · sessions)**, per the plan; no
   presence dependency. Repo cairn, `internal/ui`, `internal/report`. IN FLIGHT: branch
   `zach/arcs-first-page` (subagent building it; PR not yet opened). Claimed:
   `claim-work` slug `cairn-arcs-presence-2` — `claim-work --release cairn-arcs-presence-2`
   once it merges. Then: round 0 + round 1 audit, merge, deploy to both instances (personal:
   deployment repo trunk; client: client infra repo trunk, tag+digest pinned), verify live.
   forcing: user — the operator asked for an arcs-first page and said go.
3. **S2–S5 — presence, the host agent, badges, the bell**, in plan order. S2 deploys only
   after #195 has been live for the instance's effective session TTL (12 h default) and the
   journal still shows zero narrowed credentials — the plan's stated precondition.
   forcing: user — the operator asked for tmux identity and a bell button.

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

## How to verify
```bash
gh pr view 196 --repo ZacxDev/cairn --json state,statusCheckRollup
git fetch origin && git cat-file -e origin/main:claudedocs/plan-cairn-arcs-presence.md && echo plan-merged
git cat-file -e origin/main:internal/ui/sessionpage.go && echo session-page-merged
git grep -n 'func (a Authorization) Narrowed' origin/main -- internal/control/
```
