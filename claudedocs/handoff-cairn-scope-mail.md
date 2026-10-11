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
- **Plan MERGED** `4413feb` (#221). Operator decisions O1–O13 and D2–D6 cuts are recorded in the plan; O9 (per-host
  narrowed journal credential) and decision 17 (agent credentials carry read+write, never admin) bind S1–S4.
- **S0 MERGED** (cairn #230 via train 2 → `9292da2`; tooling #2125, hook UNREGISTERED).
- **S1 IN FLIGHT: ZacxDev/cairn#235** (`zach/scope-mail-s1`, head `92673ed`, NOT merged). Built: verb narrowing
  (`control.Narrow` verb arg, `credential-verbs-narrowed` event, `-issue-credential -narrow-verbs`), `cairn-server
  -revoke-credential`, `memo.MaySend/MayRead/MayRetract` + `CheckSend` (confident-secret scan pinned as a set),
  migration 3 + `pgstore.MemoStore`, Guard A keyed on name+printed type (`internal/surface`), U+3164-class
  Default_Ignorable in `Unsafe`, `\u` escapes in test sources. All 26 CI checks green on `92673ed`; pgtest 209/209;
  20/20 hand-run pgtest mutants killed; `control_mutants.py` 399/401 (2 pre-labelled equivalent). Self-reported by
  the implementing agent — CI green was re-read by the coordinator; the mutation matrix was NOT re-run by it.
- 🔴 **The base MOVED under #235 after its CI ran:** #233 (mutant batteries sharded into their own job,
  `tests/testlib/mutant_tree.py`) and #225 (CI tiered by changed paths) merged at `0276a51`/`50c56f1`. #235 adds
  mutant rows and edits `ci.yml` enumerations, so it needs `main` merged in and CI re-run before merging — a clean
  textual merge is not enough (semantic conflict over the mutant rows/ledgers).
- **Audit:** round 0 (requirements & deletion) dispatched on `92673ed`; checklist rounds not yet run. Merge rule:
  once the latest round has no 🔴.
- Nothing deployed for memos; no route, no verb. Claim `cairn-scope-mail-1` HELD by this session.

## Next steps (ranked)
1. **S1** — IN FLIGHT: ZacxDev/cairn#235. Finish round 0, merge `main` into the branch (post-#233/#225) and re-run
   CI, then checklist audit rounds until one has no 🔴; then merge and `claim-work --release cairn-scope-mail-1`.
   forcing: user — operator asked for scope mail.
2. **S2 → S3 → S5 (cairn) and S4 (tooling, last) and S7 (deployment)** per the plan's slice table.
   forcing: user — operator asked for scope mail.

## Defects (batched)
- **Carried to S1/S2 (from #221 round 6 and the S0 audit):** Guard A keys its ledger on NAMES, so a
  changed signature on an existing name can return stored text — key it on name + printed type;
  embedded fields from another package (e.g. `json.RawMessage` in `Rendered`) are invisible to
  Guard A and the emitter check — record qualified names and check every field of `Rendered`.
- **Carried to S3:** restore the live probe's cache-sync step (it is what lets the
  omit-`SUBSYSTEM_STORE_URL`/`_TOKEN` mutants go red); "exits 2 when `CAIRN_UI_URL` is absent"
  means absent from the probe-input file; decide whether `os.UserHomeDir()`, `reposcope`'s
  `os.Environ()` and `envalias.OSDeprecations()` join the env-read ledger.
- **Carried to S4 (before the hook is registered):** a descendant that `setsid`s and keeps stdout open
  defeats the 2 s bound (measured 12.03 s) — close the pipes after `killpg`; relayed output is
  uncapped (64 MiB passed through) — cap it and treat overflow as `malformed`.
- **Low:** Default_Ignorable letters/marks (e.g. U+3164) pass `Unsafe` — add them or state a residual;
  `tests/memo/hostile.py` and `render_test.go` carry raw invisible/bidi characters — write them as
  `\u` escapes.

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

## How to verify
```bash
gh pr view 235 --repo ZacxDev/cairn --json state,headRefOid,mergeStateStatus
gh pr checks 235 --repo ZacxDev/cairn
git -C ~/workspace/cairn log --oneline -1 -- internal/memo internal/pgstore
grep -c cairn-memo-hook ~/.claude/settings.json   # 0 until S4 registers it
```
