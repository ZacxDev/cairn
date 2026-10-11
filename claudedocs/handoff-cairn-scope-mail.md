# Handoff: cairn-scope-mail

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
"Scope mail", named **memo**: any party with write on a scope can send a short message to it, and
agent sessions on that scope see it — at session start and periodically via a hook — as a fenced,
UNTRUSTED pointer-plus-preview block. Per `claudedocs/plan-cairn-scope-mail.md` (#221, `4413feb`).
- **closing-condition:** `check` — the plan's five-part closing condition (`claudedocs/plan-cairn-scope-mail.md`
  `### closing-condition`): S0–S5 and S7 merged by content; `tests/memo/e2e.sh` exits 0 in the
  `pgtest` job with `--self-test` printing `sabotaged=13 caught=13`; the pod's `-routes` output
  unchanged (an invariant guard); the tooling repo's hook test target exits 0 with the hook's
  `--self-test` printing `sabotaged=6 caught=6`; and `tests/memo/live-probe.sh` exits 0 against the
  DEPLOYED listener.

## State now
- **Plan MERGED** `4413feb` (#221). **Q16 ANSWERED by the operator:** KEEP decision 17's verb narrowing over Q15's
  per-host project principal, accepting the stricter rollback (recorded in the plan, `a7ee397`). Round-0 D2 kept:
  migration 3 stays registered in S1.
- **S0 MERGED** (cairn #230; tooling #2125, hook UNREGISTERED).
- **S1 MERGED: #235 squash `8b7ef0d`** (2026-10-11), `main`'s tree byte-identical to the audited head `a7ee397`.
  Landed: verb narrowing (`credential-verbs-narrowed`, `-issue-credential -narrow-verbs`), `cairn-server
  -revoke-credential`, `memo.MaySend/MayRead/MayRetract` + `CheckSend`, migration 3 + `pgstore.MemoStore`, Guard A on
  name+type (`internal/surface`), Guard C over pgstore's exported surface. Audit: round 0 (Q16 questioned → operator
  kept it), round 1 no 🔴 (2 🟡 + nits, batched below) — merged under the operator's "no 🔴 in the latest round" rule.
- ⚠ **NOT DEPLOYED.** The next cairn-ui deploy applies migration 3 (`pgstore.Open` migrates); from then a rollback
  below S1 needs the version-3 recipe on #235, and older binaries refuse a journal holding `credential-verbs-narrowed`
  (exit 78, measured on the base build). Nothing reads the memo tables until S2.
- Claims: all released.

## Next steps (ranked)
1. **S1** — DONE, #235 `8b7ef0d`. forcing: user — operator asked for scope mail.
2. **S2 → S3 → S5 (cairn) and S4 (tooling, last) and S7 (deployment)** per the plan's slice table. S2 first: the
   `-client-api-addr` listener; fix the S1 defects below in it. forcing: user — operator asked for scope mail.

## Defects (batched)
- **From #235 round 1 (fix in S2, before these paths are exposed):** two CORRECT-but-untested guards — `Retract`'s
  read check (`internal/pgstore/memos.go:266`; without it a caller who cannot read scope X distinguishes existing
  memo ids there via `not-permitted` vs `not-found`, T12) and scope folding on send (`memos.go:110`; without it
  `ALPHA-NOTES` gets its own quota, T3). The auditor proved each hazard with a throwaway test, red under the mutant.
  Nits, also untested: quota counting retracted rows (`:131`), `Retract`'s transaction timeout (`:274`), `List`'s
  200 cap (`:200`), `-narrow-verbs ","` refusal (`cmd/cairn-server/issuecredential.go:705`); `Narrowed()`'s doc at
  `internal/control/resolve.go:64` still says only a non-nil list narrows.
- **Rule in two places:** `MayRetract`'s doc and decision 10 say admin alone may retract, but `Retract` also
  requires read, so an admin-only grant (the share form allows one) gets `not-found`. Decide which is intended and
  make the predicate S5 draws its retract button from carry the whole rule.
- **Carried to S3:** restore the live probe's cache-sync step (it is what lets the
  omit-`SUBSYSTEM_STORE_URL`/`_TOKEN` mutants go red); "exits 2 when `CAIRN_UI_URL` is absent"
  means absent from the probe-input file; decide whether `os.UserHomeDir()`, `reposcope`'s
  `os.Environ()` and `envalias.OSDeprecations()` join the env-read ledger.
- **Carried to S4 (before the hook is registered):** a descendant that `setsid`s and keeps stdout open
  defeats the 2 s bound (measured 12.03 s) — close the pipes after `killpg`; relayed output is
  uncapped (64 MiB passed through) — cap it and treat overflow as `malformed`.
- (The S1/S2 Guard A carry-overs and the Low `Unsafe`/`\u`-escape items were FIXED in #235.)

## Gotchas / decisions / dead-ends
- **Go's `unicode.C` already includes unassigned code points** — "unassigned" computed as "in no
  table" admitted every one; `Unsafe` is "not in L, M, N, P, S or Z". via: measurement
- **The tooling repo's `scripts/leakscan.py` reports 13 findings already on its `main`**, none from
  this work. via: command

- **S1's first hand-run "advisory lock removed" mutant SURVIVED** — the 20-goroutine race window was narrower than
  connection setup; the test was fixed to hold connections first (`1cbfc16`). A concurrency guard needs its mutant
  run, not its design read. via: measurement
- **S1 stamps `created_at` AFTER taking the per-scope lock**, so one scope's rows commit in time order and the
  plan's "late committer" cursor test had to span two scopes; recorded in the plan's S1 corrections section. via: change
- **An older `cairn-server` refuses a journal holding `credential-verbs-narrowed` whole** (exit 78, "unknown event
  kind", nothing appended) — the fail-closed rollback premise, measured on the base build. via: measurement
- **The `-narrow-scopes` without `-narrow-verbs` refusal broke `tests/presence/e2e.sh`**; it now passes all three
  verbs. Any other script issuing narrowed credentials needs the same. via: command

- **Correction to this doc's previous `State now`:** it said the base moved under #235 after its CI ran (#233/#225).
  Wrong — #235 had already merged both (`dce97bf`) and its CI covered them; only #227 arrived later. Read the PR's
  own merge commits before claiming a base moved. via: command

## How to verify
```bash
git -C ~/workspace/cairn log --oneline -1 -- internal/memo internal/pgstore
nix develop ~/workspace/cairn -c go -C ~/workspace/cairn test ./internal/memo ./internal/control
grep -c cairn-memo-hook ~/.claude/settings.json   # 0 until S4 registers it
```
