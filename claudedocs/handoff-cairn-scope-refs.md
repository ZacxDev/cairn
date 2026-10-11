# Handoff: cairn-scope-refs

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
Let each scope declare the code it documents (`git:<host>/<repo-path>[//<subpath>]@<branch>`) so a
CLI auditor can deterministically check entries' paths, SHAs and PR refs against the remote — per
`claudedocs/plan-cairn-scope-refs.md` (#210).
- **closing-condition:** `check` — the plan's closing condition: S1–S5 merged (verified by content),
  `tests/refaudit/e2e.sh` exits 0 in a blocking `go` step with `--self-test` printing
  `sabotaged=6 caught=6`, `tests/conformance/run_go.sh` exits 0 with the sources rows and the oracle
  run stays at 0 failures, and the UI/pod seam test passes.

## State now
- **Plan MERGED** #210 `80daba4` (6 audit rounds). Storage went manifest → `_scope.md` (measured
  MALFORMED in both loaders) → README front matter (rejected: `seed.sh` overwrites the pod's store
  copy, and the UI would need store write access) → an **append-only journal, arcs pattern (O5)**,
  UI the only writer, pod read-only. **Keyed by normalised scope NAME (Q17, operator-confirmed)**
  because the UI and the pod live in different ID spaces.
- **S1 = #218 (`zach/scope-refs-s1` @ `eeae65d`, `internal/codesrc`) and S2 = #219
  (`zach/scope-refs-s2` @ `a43f0a8`, read-only `GET /api/v1/sources/<scope>`): rounds 0+1 done, FIX
  ROUND RUNNING** — invalid UTF-8 and C1/format characters refused, `#` refusal dropped, one env
  reader (`codesrc.FromEnv`), on-wire `damaged=yes` dropped, sources-path == arc-path refused.
- Claim held: `cairn-scope-refs-s1-s2`.

## Next steps (ranked)
1. **#218/#219: post round-1 claims with the fix heads, run a delta round 2; when clean, merge #218
   (WITHOUT `--delete-branch`), retarget #219 to `main`, re-test the merged tree, merge #219.**
   forcing: user — operator asked for scope refs S1–S2.
2. **S3** (CLI `sources` verb), **S4** (UI edit form + the seam test), **S5** (`audit-refs` verb +
   `tests/refaudit/e2e.sh`). forcing: user — operator chose this scope.

## Gotchas / decisions / dead-ends
- **The pod's store is a COPY that `seed.sh` overwrites** (`tar -xf` over `/data`) — anything the UI
  writes into the store tree is reverted on re-seed; keep UI-written state in its own journal.
  via: code
- **Arc journal: `/var/lib/cairn-arcs/arcs.jsonl` on PVC `cairn-arc-journal` (local-path RWO);
  the store pod writes, cairn-ui mounts it read-only** — both pods must share a node. The sources
  journal reverses the direction (UI writes, pod reads). via: command
