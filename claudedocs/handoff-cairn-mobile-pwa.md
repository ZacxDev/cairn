# Handoff: cairn-mobile-pwa — 2026-10-09

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
Make cairn's browser surface mobile-first and installable (network-only, NO service worker),
per `claudedocs/plan-cairn-mobile-pwa.md` (merged #203 — read it before any slice: operator
decisions O1–O13 paraphrased, slices S0–S6b, the iPhone checklist, closing-condition clauses).
- **closing-condition:** `check` — the plan's closing condition: `uiaudit/pwa_check.sh` exits 0
  on cairn `main` with `--self-test` reporting `sabotaged=9 caught=9` (clauses (a)–(e), wired
  by S2/S3/S4 per the plan's per-slice table), AND slices S0–S5 merged (verified by content,
  not ancestry), AND S6b's blocking `uiaudit-touch` job green. The iPhone checklist is NOT part
  of it (it gates announcing install to the client instance only). ADDRESSED ⇒ arc CLOSED.

## State now
- **S0–S4 MERGED.** S1 #205 `90d1344`, S2 #206 `173f08f`, S3 #209 `0355c7a`, test fix #207
  `86b5b93`, **S4 #220 `f3623d4`** (merged on a fully green run at `480fbcf`; verified by content,
  27 files equal the tested head). The Team shortcut now targets `/team` (repointed inside #214's
  merge, `197dd0e`).
- **Deployed on the personal instance:** both pods `sha-23a7f21` — S0–S3 only; **S4 is NOT
  deployed.** Still ARMED (`CAIRN_UI_APP_NAME=cairn`, short name `cairn`, icon variant `teal`).
  NOT verified on a phone (operator's check).
- `uiaudit/pwa_check.sh --self-test` on the #214+#220 merged tree: `sabotaged=9 caught=9
  plain-loop=3/3` (a subagent's local run, before the squash).
- Claim `cairn-mobile-s4` RELEASED.

## Next steps (ranked)
1. **S5** (standalone Back/Reload), **S6a** (pin CI chromium) → **S6b** (blocking `uiaudit-touch`
   job), per the plan — the closing condition still needs S5 merged and S6b green.
   forcing: user — operator chose this scope (O9–O11).
2. **Deploy S4 to the personal instance** — needs the operator's go; it would ride with #214's
   deploy (migration 2). forcing: user — awaiting operator.
3. **(done) Merge #220** — merged `f3623d4`. forcing: user — operator asked for S4.

## Gotchas / decisions / dead-ends
- **Operator decisions this session (paraphrased; never quote verbatim — AGENTS.md forbids
  captured text):** network-only installable; both instances, per-instance name + icon
  variant; app shortcuts + install button (update banner dropped with the worker); all four
  mobile areas; iOS sign-in checklist gates the CLIENT instance only; S0+S1 first; distinct
  icon, generated screenshots, remembered iOS hint (`localStorage`), S5 all IN; pin chromium
  then make the touch job blocking; DROP the service worker for v1; touch tap-size counts stay
  REPORT-ONLY (no hard 24/44px floor — a 22px bell is caught only by review). via: user
- **`args:` on the cairn-ui container replaces the image Cmd** (no Entrypoint) — the deployment
  needs `command: ["cairn-ui"]` beside any args; it already carries it. via: measurement
- **Headless chromium reports `hover: none` at every width**, so `hover:` rules are unmeasured;
  and at the mobile rung chromium WIDENS the page to fit rather than reporting overflow — the
  harness now records widen-to-fit as overflow (S1). via: measurement
- **Every audit round on this effort found something the previous round's fix introduced**
  (summary scope, journal-world totals, an ordering hole, an empty-side guard) — budget 2–4
  delta rounds per slice, and re-run the static mutant-row sweep after any CSS/markup edit (a
  `p`→`div` change once blinded a `tests/control_mutants.py` row that only CI's full battery saw). via: measurement
- **Parallel subagents must use per-agent scratch dirs** — two once cross-wrote PR bodies via a
  shared `pr-body.md`. via: measurement

- **The deployment repo's base clone can sit far behind its trunk** — read the current image line
  off the remote ref (`git grep <pattern> origin/<trunk> -- <path>`), never its working tree. via: command

- **Run the FULL mutant battery (or every row whose `path` a slice touched) before calling a
  slice green** — per-row `--only` runs of a slice's NEW rows missed two rows the slice broke
  (a pattern on an edited line; a timing budget). CI's `go` job is where the full battery runs. via: measurement
- **The `cmd/cairn-ui` battery rows that make every child refuse sit at ~86–87 s of the
  package's 120 s `-timeout`** — two waiters still poll after child exit
  (`database_test.go` ~:318, 60 s; `main_test.go` ~:817, 20 s). Moving them onto `presenceChild`
  (whose `waitFor` now fails on exit) restores the margin. via: measurement
- **`pwa_check.sh` breaks under an exported `CDPATH`** (`$(cd … && pwd)` prints the path twice →
  a misleading "missing built cairn-ui"); pre-existing pattern, unset `CDPATH` to run it. via: measurement

- **`opencode export`, chromium and the uiaudit walk all misbehave on a loaded host** — the S4
  screenshot world is UNARMED (armed headless chromium fires `beforeinstallprompt` itself → racy
  captures), scrollbars hidden, a flake-local `fonts.conf` (nixpkgs' rendered DejaVu Math). Run
  `pwa_check.sh` with `PWA_CHECK_PORT=<free>` and `LD_LIBRARY_PATH` unset; a long `TMPDIR` overflows
  chromium's SingletonSocket. via: measurement
- **The audit hub's push endpoint answered 502 (intermediary) during this arc's S4 window**, making every PR's
  non-blocking `uiaudit` job red at `verify-push`; `main`'s earlier run passed. Not caused by any
  PR. via: command

- **S3 put `no-store` on EVERY HTML page — public ones included, from one writer**; the plan's
  decision 8 and S3 test plan carry a SUPERSEDED-AT-BUILD note saying so. via: change

## How to verify
```bash
git fetch origin && git log --oneline -3 origin/main
gh pr view 205 --repo ZacxDev/cairn --json state,headRefOid,mergeStateStatus
gh pr checks 205 --repo ZacxDev/cairn
# after S1 deploys: open the personal instance on a phone — tap targets ≥44px, no zoom on the search box
```
