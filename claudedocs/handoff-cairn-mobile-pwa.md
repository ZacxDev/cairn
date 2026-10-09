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
- **S0 MERGED** — #204 as `d4a1dda`: real CDP touch emulation at the mobile/tablet rungs
  (`setTouchEmulationEnabled`, explicitly disabled at non-touch rungs), a reachability refusal
  inside `refuseWalkRegressions` (coarse TRUE at every touch capture, FALSE at every other),
  report-only axe target-size / input-font / sub-24px-box measurements, and a journal-backed
  second uiaudit world (walks `/share` + `/invite`, never pushed). CI's chromium also matched.
- **S1 AUDITED, CI PENDING** — #205 `zach/mobile-s1` head `b1c28ff`: touch CSS under
  `@media (pointer: coarse)` in `internal/ui/tailwind.css` (app.css regenerated), 44px targets,
  16px inputs, header grid in DOM order, B1 whole-row link on scope-page rows only
  (`#entry-list > .entry-row`), long-name wrapping; input-font + target-size + header-order
  refusals. Rounds: 0, 1 (4🟡 fixed), 2 CLEAN. CI at `b1c28ff`: 6 green, `go` + `tests` pending
  at handoff. Measured (nixpkgs chromium 154): touch rungs boxes<24px 326→0, inputs<16px 5→0,
  overflow 11→0; desktop rungs byte-identical capture lines.
- NOT deployed: S1. The personal instance runs `sha-0d3a1fa` (sign-in return path, #202).
- No claim held for this effort.

## Next steps (ranked)
1. **Merge #205 (S1) once CI is green at `b1c28ff`**, verify by content, then bump BOTH personal
   pods to the merge sha in the deployment repo (worktree; trunk = deploy) and check live: the
   operator opens a page on a phone (taps, no input zoom). forcing: user — the operator asked for mobile-first.
2. **S2** (manifest, icon variants, `-app-name`/`-app-short-name`/`-app-icon-variant` flags,
   `pwaHead()` with no script tag, `pwa_check.sh` with clauses (a)(b-name,icon)(c), pins
   `sabotaged=6` or 7). Then **S3** (`no-store` only — may land any time BEFORE S4), **S4**
   (`pwa.js`, shortcuts incl. `/?q=`, screenshots via nix, iOS hint key; needs S2 AND S3),
   **S5** (standalone Back/Reload), **S6a** (pin CI chromium) → **S6b** (blocking touch job).
   Each slice: implementer subagent in a worktree → round 0 + round 1 → delta rounds → merge.
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

## How to verify
```bash
git fetch origin && git log --oneline -3 origin/main
gh pr view 205 --repo ZacxDev/cairn --json state,headRefOid,mergeStateStatus
gh pr checks 205 --repo ZacxDev/cairn
# after S1 deploys: open the personal instance on a phone — tap targets ≥44px, no zoom on the search box
```
