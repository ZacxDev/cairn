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
- **S1 DONE and LIVE** — #197 merged as `45ef3d9` (content-verified: `internal/` and
  `uiaudit/` on `main` identical to the PR head `25f8787`; all 8 CI checks green). Three
  audit rounds; round 3 clean (the auditor's property test over 247,940 time points found 0
  label/liveness mismatches, 5,774 under the mutant). `GET /arcs` lists arcs whose HOME scope
  is readable; live ⇔ status open OR ⌊(now − last update)/24h⌋ ≤ 14 — the same truncation
  the "Nd ago" label uses, so a row reading ≤14d is live whichever source won; last update =
  max(pod `registered_at`, newest member bullet in a readable scope, future bullet dates
  clamped to today); `?all=1` shows all; header "Arcs" link on every page. `/arc` has
  scopes · sessions tabs (unknown tab ⇒ default; every miss is the same 404 bytes). Battery
  226 rows (224 killed, 2 labelled equivalent) measured at `f238b91`; the re-anchored
  `ui-arcs-index-live-window-compared-as-an-instant` row measured alone at `25f8787`.
- **Deployed on BOTH instances at `sha-45ef3d9`, 0 restarts**: personal via deployment repo
  trunk `47f387f`; client via the client infra repo trunk `ebf1d70`, tag+digest pinned
  (store `sha256:f35f7061…`, ui `sha256:37fbdc9e…`). Rollback either: both image lines back
  to `sha-6edcb45` (client digests `fdcf67b6…` / `3fe4386a…`).
- **Verified live on the personal pod** (operator token over a port-forward, served HTML):
  `/arcs` 200 with 30 live arcs newest first ("registered 2h ago"…), "0 not live" (every
  registration is recent), `?all=1` 200, header link present on `/`, an arc's scopes tab
  links 2 scopes and its sessions tab links its member session, an unknown arc 404. Client
  instance: pods on the new digests, API answers; its UI not viewed (operator not signed in).
- `claim-work` slug `cairn-arcs-presence-2` RELEASED.

## Next steps (ranked)
1. **#196 is MERGED** (`78fe99a`): the `go` job's `ok` floor is `-lt 23`. Nothing to do; the
   rank is kept so later ranks keep their claim identities. forcing: gate — kept for numbering.
2. **S1 is MERGED and DEPLOYED** (#197, `45ef3d9`). Nothing to do; kept for numbering.
   forcing: user — done.
3. **S2 — presence store + agent API** (repo cairn, `internal/presence` new package, `cmd/cairn-ui`
   second listener, ledgers per the plan's S2 row, `go` job `ok` floor → measured count on the
   merged tree, 24 if nothing else moved), then S3 (tooling repo host agent), S4 (badges),
   S5 (bell). 🔴 S2 deploys only after #195 (live since the `sha-6edcb45` rollout) has been
   deployed for at least the instance's effective session TTL (12 h default) AND the journal
   re-check still shows zero narrowed credentials. Use `control.Authorization.Narrowed()`
   (#195) rather than the plan's decision-11 credential-row derivation, and update decision 11.
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

- **A ruling formula can be wrong while its stated goal is right** — the round-2 ruling for
  #197 said `utcDay(lastUpdated) >= utcDay(now) − 14`, which would hide a 14d23h registration
  reading "14d ago"; the implementer built the goal (`⌊Δ/24h⌋ ≤ 14`) and said so. Brief the
  GOAL plus the test cases, and treat the formula as a suggestion. via: measurement
- **zsh history modifiers bit a digest lookup**: `"…/$img:sha-…"` expands `:s` as a modifier
  (`bad substitution`); brace it, `${img}:sha-…`. via: command
- **A scheduled merge-on-green loop must check the publish run's head sha equals the merge
  commit** before reading digests — the publish workflow lists the newest run, which can be
  another push. via: command

## How to verify
```bash
git fetch origin && git cat-file -e origin/main:internal/ui/arcsindex.go && echo s1-merged
gh pr view 197 --repo ZacxDev/cairn --json state,mergeCommit --jq '"\(.state) \(.mergeCommit.oid)"'
# live: port-forward deploy/cairn-ui (personal) and GET /arcs, /arcs?all=1, /arc?home=…&slug=…&tab=sessions
# with an Authorization: Bearer header read from a 0600 file; expect 200s and an unknown arc → 404
```
