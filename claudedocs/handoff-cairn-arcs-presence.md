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
- **ARC CLOSED — the closing condition is ADDRESSED.** S1 `45ef3d9` (#197), S2 `123d771`
  (#198), S4 `2a73fd6` (#200), S5 `cf1c2c9` (#201) on cairn `main`; S3 merged in the tooling
  repo as `aae6405`; all content-verified. Both runnable checks exit 0 on their `main`s:
  `tests/presence/e2e.sh` at `cf1c2c9` → `passed=19 failed=0 expected=19`, `--self-test` →
  `sabotaged=7 caught=7`; the tooling repo's `test_cairn_ring.py` at `aae6405` → 45 passed,
  `cairn-ring-claim --self-test` → `SELF-TEST: PASS` (both sabotages CAUGHT).
- **POST-CLOSE ROLLOUT DONE except the operator's click.** Personal instance only:
  - both pods on `sha-cf1c2c9` (S4 badges + S5 bell); presence ENABLED via `cairn-ui` args
    `-presence-agent-addr=0.0.0.0:8105`, `-presence-tokens=<state PVC>/presence-tokens`,
    `-presence-owner=user:<operator's GitHub-backed id>`; startup line reads
    `presence agent on [::]:8105 (sole owner …, 4 token row(s)`;
  - the agent listener is reachable from the operator's two hosts over the private mesh only,
    through the deployment's mesh gateway (no public hostname), which SETS `CF-Connecting-IP` to the
    mesh peer — the server logs the real host address on a refusal;
  - 4 tokens (push + claim × 2 hosts) minted from the pod straight into 0600 files on each
    host; each host's units (`cairn-presence-push.timer`, `cairn-ring-claim.service`) are
    active; pushes acknowledged by the server on both hosts (`server=rows=N`); claim services
    polling every 5 s;
  - MEASURED via the operator bearer credential: this arc's own session page renders the
    badge `<host> · <session>:<window> · <hotkey> · claude · seen 38s ago`; `/arcs` shows 13
    `live pane` badges. No bell on that page (bearer request, no cookie) — expected.
- **NOT verified:** a real bell click from a signed-in browser lighting the right window — the
  operator's judgement (plan: "Post-close rollout").
- The client instance is NOT bumped past `sha-123d771` (presence is personal-only; nothing
  there needs S4/S5).

## Next steps (ranked)
1. **#196 is MERGED** (`78fe99a`): nothing to do; kept for numbering. forcing: gate — kept for numbering.
2. **S1 is MERGED and DEPLOYED** (#197, `45ef3d9`). Nothing to do; kept for numbering.
   forcing: user — done.
3. **Rollout done; one judgement left**: the operator clicks the bell on a signed-in session page
   and judges the window's status-line styling. forcing: user — the operator asked for a bell button.

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

- **A mutant row is a STRING match against source, so a markup change can blind it silently.**
  #201's `p`→`div` fix left S4's `ui-presence-session-page-badge-dropped` matching 0 times; only
  CI's FULL battery reported it (`harness-errors=1`) — every `--only` run of the new rows was
  green. After any edit to a file a mutant row names, sweep all rows for exactly-one matches.
  via: command
- **Two subagents sharing one scratchpad filename (`pr-body.md`) cross-wrote PR bodies** — one
  PR briefly carried the other's body. Give every agent its own scratch subdirectory. via: measurement
- **`gh pr update-branch` is the cheap way to put a PR's CI on the merged tree** after a sibling
  lands (it was how #200 picked up #199's race fix). via: command
- **The client infra repo's pre-push gate needs pyyaml**: push from
  `nix-shell -p 'python3.withPackages (p: [p.pyyaml])'`; never `--no-verify` there. via: command

- **`args:` on the `cairn-ui` container REPLACES the image's `Cmd`** — the image has no
  `Entrypoint` and its `Cmd` is a per-build store path, so args alone exec'd the first flag
  (exit 128, CrashLoopBackOff, ~8 min of UI downtime under `Recreate`). The manifest now carries
  `command: ["cairn-ui"]` (resolves via the image PATH, survives bumps) with a comment. Read the
  image config (`Entrypoint`/`Cmd`) BEFORE adding args to any of these images. via: measurement
- **The agent listener identifies clients ONLY by `CF-Connecting-IP` from a trusted proxy** — a
  route that does not pass through Cloudflare must SET that header itself, or every agent
  request is "no client identity" → 401. via: code
- **The tooling repo's home-manager generation was unbuildable for both hosts after a
  `flake.lock` refresh** (nixpkgs' anyio test suite fails; not cached); fixed by a
  `doCheck = false` overlay with a removal recipe in its comment. A switch failure there
  blocks any host-side rollout — build the generation before planning one. via: measurement

## How to verify
```bash
git fetch origin && git cat-file -e origin/main:internal/ui/arcsindex.go && echo s1-merged
gh pr view 197 --repo ZacxDev/cairn --json state,mergeCommit --jq '"\(.state) \(.mergeCommit.oid)"'
# live: port-forward deploy/cairn-ui (personal) and GET /arcs, /arcs?all=1, /arc?home=…&slug=…&tab=sessions
# with an Authorization: Bearer header read from a 0600 file; expect 200s and an unknown arc → 404
```
## Defects (batched)
- ~~worst-case push size stated wrongly~~ FIXED in #200.
- Tooling repo `scripts/cairn-ring-claim:~193`: the SIGTERM handler calls `Event.set()`, which can
  deadlock if the signal lands while the main thread holds the Event's internal lock (reproduced
  after 81 rapid signals in a tight loop; production odds ~1e-6 per stop; cost: a 90 s stop then
  SIGKILL). Fix: a lock-free handler (`signal.set_wakeup_fd`/self-pipe, or raise from the handler).
