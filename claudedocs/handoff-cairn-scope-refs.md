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
- **Plan MERGED** #210 `80daba4`. **S1 #218 and S2 #219 MERGED** through merge train 1 (#229, merge
  commit `b06f15c`, tree equal to the CI-tested train tree). #219 was CLOSED as landed (its stacked
  base made GitHub refuse to mark it merged; its head `108a292` is an ancestor of `main`).
- The stranded round-1 fix round was recovered and finished (S1 `d0a24fb`, S2 `af77d81`); its single
  final audit found no 🔴. Merge-prep re-pointed a mutant row whose anchor the fix had deleted.
- **Deployed:** personal instance at `sha-9292da2`. `GET /api/v1/sources/<scope>` answers 401 without
  a credential and 200 `status=sources-unconfigured` with one — the pod has no source journal
  configured, so nothing is served yet.
- Claims: all released.

## Next steps (ranked)
1. **(done) #218/#219** — merged via train 1. forcing: user — operator asked for scope refs S1–S2.
2. **S3** (CLI `sources` verb), **S4** (UI edit form + the seam test; the UI is the journal's only
   writer), **S5** (`audit-refs` verb + `tests/refaudit/e2e.sh`). Then configure a source journal on
   the deployment. forcing: user — operator chose this scope.

## Gotchas / decisions / dead-ends
- **The pod's store is a COPY that `seed.sh` overwrites** (`tar -xf` over `/data`) — anything the UI
  writes into the store tree is reverted on re-seed; keep UI-written state in its own journal.
  via: code
- **Arc journal: `/var/lib/cairn-arcs/arcs.jsonl` on PVC `cairn-arc-journal` (local-path RWO);
  the store pod writes, cairn-ui mounts it read-only** — both pods must share a node. The sources
  journal reverses the direction (UI writes, pod reads). via: command

- **`internal/codesrc` imports `internal/identity`**, so the Go client binary now links identity,
  control and netid for a five-line value check — no cycle, no import-ban hit, no `init()`; a small
  standalone helper package would avoid it. via: code
- **The sources-journal ≠ arc-journal refusal compares resolved PATHS** (symlinks are followed), so
  a hardlink, bind mount or second mount path to the same file gets past it; both journals are
  append-only and line-flocked, so the cost is cross-read noise, not corruption. via: code
