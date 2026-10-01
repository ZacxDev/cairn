# Handoff: cairn-tag-vocabulary — 2026-10-01

## Run this first — the index, one command
```bash
<tooling>/scripts/cairn-ops/read.sh recall --repo "/home/zach/workspace/cairn"
```
`<tooling>` is the private tooling checkout; its path is already exported in the shell. It is
not named here because this repo is PUBLIC and its name is a denied identifier.
Terse pointers this doc does not carry, curated by past sessions and outliving it.
🔴 RECALL, NOT LIVE OBSERVATION — every line is a pointer to VERIFY, never a current
reading, and it may describe a gotcha already fixed. `scope-absent`/`scope-empty` means
nothing is recorded yet: ordinary, not an error, and not a clean bill of health.
Non-blocking: if it exits non-zero, print the stderr line and carry on.

## Goal
Put the three shipped entry features (`tags:`, `refs:`, `## Requirements`) into actual
use across the store, and close the tag vocabulary so a typo cannot land silently. The
predecessor arc `handoff-cairn-next-phase.md` shipped the features; this arc is about
them being USED and ENFORCED.

- **closing-condition:** `check` — every entry on its resolved instance carries a tag
  from the closed set, the pod REFUSES an off-vocabulary tag at the write path (422,
  `X-Store-Status: entry-shape`) with a valid tag still accepted as the control, and both
  halves are verified against the DEPLOYED pod rather than against `main`.
  **MET 2026-10-01** — see "How to verify" for the exact probes and their outputs.

## State now
- Branch: `main`. Five PRs merged and verified **by content** on `origin/main` (a squash
  never makes the branch head an ancestor, so ancestry is the wrong test):
  - **#164** — two private identifiers plus a private entry filename scrubbed out of this
    PUBLIC repo, and their digests added to `tests/leakscan.py` so recurrence is caught.
  - **#160** — the `tags:` vocabulary CLOSED at the WRITE path (`internal/write`).
  - **#162** — the rendered link badge and body label both read `refs`; a `## Requirements`
    bullet's provenance renders once (badge) instead of twice.
  - **#165**, **#167** — `.claude/hooks/base-clone-write-guard.py` now judges the write
    TARGET rather than the session cwd.
- **Store: 303 entries tagged** across both instances — `infra` 136 / `product` 99 /
  `tooling` 68. Zero off-vocabulary tags anywhere.
- **Deployed and verified**: the deployment repo's pod and UI pins moved
  `e8839d9 -> ffa0eca` in one commit; the pod has rolled and both features are live at the
  edge. ⚠ The **UI** rollout is NOT independently verified — see Open investigations.
- A new store entry `cairn/tag-vocabulary` holds the closed vocabulary AND the per-scope
  table. The table CANNOT live in this repo: the scope names are denied identifiers here.

## Open investigations — live diagnosis state

### The UI pod's rollout was never independently confirmed
- as-of: 2026-10-01
- **Symptom + exact repro:** the pod and UI pins moved in one commit and the POD is
  confirmed rolled; nothing confirms the UI container is serving the new image.
- **Observed (with values):** pod-rendered `GET /api/v1/recall/cairn` with a valid bearer
  token returned **200**, body 7422 B, containing `🔗 N ref` **×4** and `🔗 N task` **×0**.
  That is the pod. The UI's equivalent signal is behind a cookie session.
- **Ruled out:** "the CSS fingerprint will tell us" — `/static/app.<hash>.css` is a build
  fingerprint but the change touched no CSS, so it cannot move. `via: measurement`
  (fetched `/sign-in` unauthenticated, hash `app.e9b33ecf4a5b.css`).
- **Ruled out:** "use the browser bridge" — the extension reports `connected: 0`, so no
  authenticated page can be read from here. `via: command`
  (`scripts/browser-bridge/browser whoami`).
- **Leading hypothesis:** it rolled. Both pins are in one commit, applied by one
  reconcile, and the pod demonstrably moved — but that is inference, not observation.
- **Next probe:** in a signed-in browser, open the browse page filtered by a tag, click any
  entry, and read the Refs panel heading. `refs` ⇒ rolled; `tasks` ⇒ pod and UI are out of
  step and the UI deployment needs looking at.

### A hook that fails to START is asserted to be an ALLOW, and nobody has measured it
- as-of: 2026-10-01
- **Symptom + exact repro:** `.claude/hooks/base-clone-write-guard.py`'s own docstring says
  every unexpected condition exits 0 and says nothing, i.e. a hook that cannot start lets
  the command run. The entire fail-open/fail-closed design rests on this.
- **Observed (with values):** an independent audit measured that **exit 1 produces no
  `deny` on stdout**, so a crashing hook does not block. It had no read of the harness's
  handling of a hook that never starts at all (missing interpreter, syntax error, chmod).
- **Ruled out:** nothing yet — this is unprobed rather than narrowed. `via: assumed`
  (the claim is the file's own, carried forward across three PRs without measurement).
- **Leading hypothesis:** a hook that fails to start is indeed an ALLOW, which means the
  guard's protection is only as good as the file being loadable.
- **Next probe:** in a THROWAWAY project dir (never this repo, and never while agents are
  working against the live hook), register a hook that exits non-zero before doing
  anything, and a second that is not executable, then attempt a write the guard would
  refuse. Record which of the two, if either, still blocks.

## Next steps (ranked)
1. **Make `?q=` and `?tag=` compose in the browse surface.** The operator explicitly chose
   "close it — make them compose" over documenting the limitation, and it was queued behind
   the PR traffic and never dispatched. Repo: this one; files `internal/ui/server.go`
   (`handlePage`), plus tests. Today searching drops an active tag filter and vice versa.
   forcing: user — the operator selected this option and it was never delivered.
2. **Close the three declared guard fail-opens.** All three are recorded with closing
   conditions in `.claude/hooks/base-clone-write-guard.py`'s docstring and all three were
   proven end to end by an audit: (a) `_shell_lines`' heredoc opener regex runs on the raw
   line, so ordinary text containing `<<WORD` — a quoted string, or even a `#` comment —
   silently drops every later line; (b) the program-name walk misses `if git commit`,
   `while`, `command`, `nohup`, `timeout`, `eval`, `stdbuf`, `exec`, `sudo`, `xargs`;
   (c) the `_REFUSED` complement is described as "reads" while `clean -fd`, `rm`, `mv`,
   `worktree remove` and `branch -D` all write shared state.
   forcing: security — each is a measured way to land a commit on the wrong branch in a
   shared clone, which is the single failure this guard exists to prevent.
3. **Confirm the UI pod rolled** (Open investigations block 1). One click path in a
   signed-in browser; everything else about the deploy is already verified.
   forcing: user — the operator asked for this deploy to be validated, and this is the one
   half that could not be closed from here.
4. **Teach the entry template to emit a `tags:` line.**
   `scripts/lib/subsystem_touch.py --template` emits none, so every entry born through the
   handoff flow starts untagged and invisible to `--tag`. That silently re-opens the gap
   this arc just closed, one entry at a time. Repo: the private tooling repo.
   forcing: regression — new entries reintroduce the untagged state this arc eliminated.

## Defects (batched)
- `.claude/hooks/base-clone-write-guard.py:702` still says the existence check lives
  "ahead of the probe budget" — #167 deleted that budget, so the sentence describes a
  mechanism that no longer exists.
- Same file: `_abs_path`'s claim that a second existence check "could never change a
  verdict" was measured FALSE when a NUL byte crashed the hook. #167 fixed the crash,
  which plausibly restores the claim — but it has not been re-measured since.
- The base clone of this repo carries a STAGED modification to the hook whose content is
  byte-identical to `origin/main`, while `HEAD` sits 4 commits behind. Harmless (the
  running hook is the fixed one) but it makes `git status` misleading. Clearing it is
  `restore --staged --worktree` on that path, then an ff-only merge.

## Gotchas / decisions / dead-ends
- 🔴 **The per-scope tag table cannot live in this repo.** `tests/leakscan.py` denies the
  scope names as `denied-identifier`s and this repo is PUBLIC. The RULE (exact match,
  never a prefix — with a counterexample) ships here with a SYNTHETIC table; the real rows
  live in the store entry `cairn/tag-vocabulary`. An attempt to commit them was refused by
  the leak gate with 9 findings, which is the gate working.
- 🔴 **A write-time gate, never a parse-time one.** A refusal inside the reader's front
  matter parser makes an entry MALFORMED — out of the index, out of `--ref`/`--search`,
  AND unwritable, because the write route resolves through the index. That would have
  bricked every entry carrying an off-vocabulary tag. Verified against the code, not
  assumed.
- ⚠ **A fourth vocabulary term was tried and removed.** `client-work` answers *whose* work
  where the other three answer *what kind*; mixing axes in one closed set under a
  one-tag-per-entry rule makes both unassertable, and the scope name already carries
  whose. 65 entries moved to `product`.
- ⚠ **`--tag` is scalar and the vocabulary is CLOSED, but a tag query is NOT checked
  against it** — a typo in the QUERY still returns a clean zero. Read the denominator the
  reader prints (`N of M entries in <scope> carry it`); a bare zero proves nothing.
- 🔴 **Variables in a `git -C` argument are refused by the write guard, by design.** #167
  deleted the shell-variable resolver because it opened four fail-opens. Pass `-C` a
  LITERAL absolute path; a `$VAR` is unresolvable and falls back to refusing. This bit the
  authoring session twice in one hour.
- ⚠ **An image pin bump deploys every commit merged since.** The one landed here carried
  19 commits (PRs #149–#167), not the two it is named for; the commit message enumerates
  them. Say what a bump carries before pushing it.
- ⚠ The client instance's migration to the Go image is owned by a DIFFERENT session. Do
  not touch that cluster's manifests from this arc.

## How to verify
Both halves of the closing condition, each with the control that makes it mean something.

**The write gate — negative then positive** (the positive control is not optional: without
it, a refusal is indistinguishable from a broken write path):
```bash
# take any entry, set an off-vocabulary tag, and try to replace it
<tooling>/scripts/cairn-ops/write.sh put --scope cairn --ref tag-vocabulary --file <bad copy> --no-verify
#   => 🔴 the store REFUSED the write [entry-shape] — ... is not one of infra|product|tooling
<tooling>/scripts/cairn-ops/write.sh put --scope cairn --ref tag-vocabulary --file <good copy> --no-verify
#   => cairn: replaced instance=... revision=...
```

**The renderer, read from the POD rather than the local client** (the installed CLI renders
locally, so it is NOT evidence about what is deployed):
```bash
set -a; . /home/zach/.config/subsystem-store/env; set +a
curl -s -H "Authorization: Bearer ${CAIRN_TOKEN}" "${CAIRN_URL}/api/v1/recall/cairn" \
  | grep -c '🔗 [0-9]* ref'     # => 4
#   and the retired word must be gone:  grep -c '🔗 [0-9]* task'  => 0
```

**Tag coverage across the fleet** — read each scope on its RESOLVED instance; the two
caches replicate 12 scopes, so a naive walk double-counts:
```bash
<tooling>/scripts/cairn-ops/health.sh instances --scope <scope>   # which cache is authoritative
# expected totals: infra 136 / product 99 / tooling 68, and no fourth term
```
