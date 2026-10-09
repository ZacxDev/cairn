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
- **S0 MERGED** — #204 as `d4a1dda`.
- **S1 OPERATOR-APPROVED TO MERGE + DEPLOY, waiting on CI** — #205 `zach/mobile-s1` head `b1c28ff`
  (audit rounds 0, 1 (4🟡 fixed), 2 CLEAN; measurements in the PR body). CI run `37885832874`:
  7 green, `go` still running at 05:31Z (the previous head's `go` job took ~78 min). Operator said
  proceed: merge once green, verify by content, bump both personal pods.
- **S2 IN FLIGHT** — implementer subagent on branch `zach/mobile-s2`, STACKED on
  `origin/zach/mobile-s1` (S2's clause (c) sabotages need S1's refusals). It does NOT open a PR;
  the parent rebases onto `main` after #205 squash-merges, then runs audit rounds and opens the PR.
  S3 has not landed, so S2 pins `sabotaged=6`.
- Claims HELD: `cairn-mobile-pwa-1` (merge+deploy S1), `cairn-mobile-pwa-2` (S2).
- NOT deployed: S1. The personal instance runs `sha-0d3a1fa` (both pods).

## Next steps (ranked)
1. **Merge #205 (S1) once CI is green at `b1c28ff`**, verify by content, confirm publish-image
   pushed both images for the merge sha, then bump BOTH personal pods (the store pod's
   `cairn-store-go` line and the UI pod's `cairn-ui` line) in the deployment repo — worktree off
   its trunk; trunk = deploy; one commit, rollback sha `0d3a1fa8f746dfd75edc198d1f98b0d86f1f4383`
   in the message, the same shape as the previous bump. Then check live; the operator opens a page
   on a phone. Release `cairn-mobile-pwa-1`.
   forcing: user — operator approved merge+deploy this session.
2. **S2** — IN FLIGHT on `zach/mobile-s2` (see State now). Rebase onto `main` after #205, round 0 +
   round 1 audits → delta rounds → PR → merge. Then S3, S4, S5, S6a, S6b per the plan.
   forcing: user — operator chose this scope (O10 and later answers).

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

## How to verify
```bash
git fetch origin && git log --oneline -3 origin/main
gh pr view 205 --repo ZacxDev/cairn --json state,headRefOid,mergeStateStatus
gh pr checks 205 --repo ZacxDev/cairn
# after S1 deploys: open the personal instance on a phone — tap targets ≥44px, no zoom on the search box
```
