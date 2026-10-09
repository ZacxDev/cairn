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
- **S0 MERGED** #204 `d4a1dda`. **S1 MERGED + DEPLOYED** #205 `90d1344`: both personal pods on
  `sha-90d1344` (rollback `sha-0d3a1fa`), served stylesheet byte-identical to S1's `app.css`
  (pre-S1 one differs). NOT verified on a phone — the operator's check.
- **S2 MERGED** #206 as `173f08f` (content-identical to reviewed head `32305fb`). Audit: round 0
  (1 deletion candidate D1, deleted) → round 1 (2🟡 2🟢, all fixed in `c1fba06`) → round 2 CLEAN
  → CI's full battery caught 2 S2 casualties (stale invite pattern; a child waiter blind to exit
  pushing the package past `-timeout=2m`), fixed in `32305fb`, delta-audited CLEAN, CI 8/8 green,
  battery `killed=294 survived=2` (the two declared EQUIVALENT).
- **S2 NOT deployed.** It is inert until `-app-name` is set; arming the personal instance needs
  the operator's name + icon variant (`amber`/`teal`/`violet`/`slate`).
- `pwa_check.sh --self-test` pins `sabotaged=6 caught=6 plain-loop=3/3` (S3 not landed).
- No claims held.

## Next steps (ranked)
1. **S3** — `no-store` alone (decision 8) + `TestEveryNonPublicHTMLRowIsNoStore`; S2 has landed,
   so S3 wires clause (d) into `pwa_check.sh` and pins `sabotaged=7`. Then S4, S5, S6a, S6b.
   forcing: user — operator chose this scope (O10 and later answers).
2. **Arm + deploy S2 on the personal instance** once the operator picks an app name and icon
   variant (bump both pods to the S2 merge sha; add `-app-name`/`-app-icon-variant` args beside
   the existing `command`). forcing: user — awaiting the operator's choice.

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

## How to verify
```bash
git fetch origin && git log --oneline -3 origin/main
gh pr view 205 --repo ZacxDev/cairn --json state,headRefOid,mergeStateStatus
gh pr checks 205 --repo ZacxDev/cairn
# after S1 deploys: open the personal instance on a phone — tap targets ≥44px, no zoom on the search box
```
