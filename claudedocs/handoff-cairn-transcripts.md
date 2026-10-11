# Handoff: cairn-transcripts

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
Ship session transcripts (Claude Code + opencode) to the store, redacted on the host, visible in the
session page, plus an out-of-process plugin system (summaries; ClickUp matching) — per
`claudedocs/plan-cairn-plugins.md` (#208). Also landed: the agent-usage trace `plan-cairn-agent-view.md` (#211).
- **closing-condition:** `check` — the plan's closing condition: S0–S11 merged (verified by
  content), `tests/plugins/e2e.sh` exits 0 in the `go` job with `--self-test` printing
  `sabotaged=14 caught=14`, `cairn-capture --self-test` prints `planted=P caught=P
  clean-damaged=0`, and both example plugins' suites pass against fake LLM / fake ClickUp servers.

## State now
- **Plans MERGED:** #208 `8c61b21` (15 audit rounds; read scopes via a CLIENT READ LEDGER, O11),
  #211 `a3e7de3` (agent-view trace: no hook injects cairn; 661/665 recalls are piped through
  head/grep — the v1 recall tab is #213 in the UI arc).
- **Operator decisions (recorded in the plan, revision 17):** O12 binary ships (only a closed list
  of KNOWN binary signatures ships byte-identical); O14 90 days, ~20 GB per capturing host; O13 the
  plan's recommendations as stated defaults (Q5: the client instance is NOT armed without an
  explicit decision).
- **NEW operator decision O15 (2026-10-10, for #216; being written into the plan by round 4):**
  change approach — redact by KEY CONTEXT (any value of a secret-named key/flag/env var/config
  field) plus HIGH-ENTROPY tokens, accepting more clean damage; NORMALISE tool line prefixes (grep
  `path:N:`/`path:`/`N-`, diff `<`/`>`) before any rule runs; each round's held-back set is FRESH,
  written by an auditor the fixer never sees; and **capture is armed on NO instance until a fresh
  held-back set scores ≥90% leaks caught AND ≤15% clean lines damaged.**
- **S0 = #215 MERGED** as `76ddc7e` (16:43Z, branch kept); verified by content — all 10 files equal
  the branch head `7e6d53e` except one `ci.yml` mutant-count comment that came from `main` (#213).
  #216 was already retargeted to `main`.
- **S1 = #216 (`zach/transcripts-s1` @ `d4ccf7f`): fix round 4 IN FLIGHT** under O15, by a
  subagent in its own worktree (merging `main` in — the PR read CONFLICTING after #215). Round 3's
  findings it addresses: 🔴 grep/diff prefix leaks + PEM bodies; 🟡A clean damage 36/74; 🟡B named-key
  misses; 🟡C `audit_rate_test.go:44` vs its docstring, and the overstated "176–197/200".
- **S2 = #217 (`27b812c`):** stacked on #216; round 4 may change the API it calls.
- Claim held: `cairn-transcripts-s0-s2`.

## Next steps (ranked)
1. **#216 round 4 → then a delta audit whose auditor WRITES A FRESH held-back set** (never shown
   to the fixer) and scores it against O15's floor/ceiling; 🔴 grep prefixes first. Merge only
   on a clean round and green CI; then rebase/retarget #217 and re-run the merged tree.
   IN FLIGHT: ZacxDev/cairn#216. forcing: security — the redactor is the only control between raw
   transcripts and the store (O1).
2. **S3** (upload + transcript store in cairn-ui) and **S11** (client read ledger) — S11 must land
   before transcripts are armed on any real instance, and O15's gate must pass too.
   forcing: user — operator chose the build.
3. **#215 follow-through** — DONE (merged, verified by content); delete `zach/transcripts-s0` once
   no PR is based on it. forcing: user — operator asked for S0–S2.

## Gotchas / decisions / dead-ends
- **`opencode export` silently truncates at ~8–96 KiB when piped (exit 0)** but is complete to a
  file — capture writes to a file and refuses anything that doesn't parse. via: measurement
- **`OPENCODE_SESSION_ID` is not set by opencode**; it comes from the operator's own plugin; live
  value in a subagent unmeasured. via: measurement
- **The shell-command read-scope parser was abandoned after rounds 8–11 each found a new shell
  edge** (zsh glob qualifiers, here-strings, `--scope` spellings, `ls-entries` ignoring `--scope`);
  replaced by the client read ledger (O11). Do not reintroduce command-line parsing. via: change
- **A docs guard refuses any backticked `cairn <verb>` for an unregistered verb**
  (`tests/test_no_scrubbed_identifiers.py`) — spell proposed verbs without the prefix. via: command
- **Real-transcript dry run (counts only):** 204 of 269 Claude sessions would be HELD (`*` in V)
  until the read ledger (S11) exists. via: measurement
