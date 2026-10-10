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
- **Operator decisions this session (recorded in the plan, revision 17 on #215):** O12 binary ships
  (coordinator's reading, recorded as reversible: only a closed list of KNOWN binary signatures ships
  byte-identical; everything else incl. NUL-separated and invalid-byte text is scanned); O14 90 days,
  ~20 GB per capturing host; O13 the plan's recommendations adopted as stated defaults (Q5: the
  client instance is NOT armed without an explicit decision).
- **S0 = #215 (`7e6d53e`): audit ladder CLEAN** (rounds 0+1, round 2 clean). Waiting on CI
  (`uiaudit` red = hub 502).
- **S1 = #216 (`zach/transcripts-s1` @ `d4ccf7f`, `internal/redact`): round 3 NOT CLEAN** (1🔴 3🟡).
  Round 2's four regressions are fixed and the adopted auditor tests pass (attack set 74/75,
  planted=72 caught=72 clean-damaged=0 at 8 seeds) — BUT a fresh blind 62-case set scored **30/62 at
  both 2ba3e5c and d4ccf7f**: the gains concentrated on the adopted test shapes. 🔴 single-file
  `grep -n` (`N:`), `grep` without `-n` (`path:`), context (`N-`) and `diff` `<` prefixes still leak
  (also PEM bodies). 🟡A new clean damage 4/74 → 36/74 on blind probes (letters-only `Password:
  hashed,`, i18n/validation strings via source-literal, `?key=getting-started`, minified JS);
  `identifierTail`'s `,` branch unreachable. 🟡B named-key misses (systemd `Environment=`,
  `os.environ[...] =`, CLI flags `--password=`/`-p<pw>`/`-a`, `IDENTIFIED BY`, kubeconfig
  `client-key-data`, …). 🟡C `audit_rate_test.go:44` checks the whole value + first half, not the
  docstring's "no 6-char window survives" — libpq pm-20 is 139/200 under the stated oracle, so the
  README/plan's "176–197/200" is overstated.
- **S2 = #217 (`27b812c`, `cmd/cairn-capture`, uploads NOTHING):** round 2 clean as a delta;
  re-check rides with #216's round 3.
- Claim held: `cairn-transcripts-s0-s2`.

## Next steps (ranked)
1. **Merge #215 (S0)** once CI's `go` job is green — stacked parent: merge WITHOUT
  `--delete-branch`, then retarget #216 to `main` (`gh pr edit 216 --base main`). forcing: user —
  operator asked for S0–S2.
2. **#216 redactor: an OPERATOR DECISION before another fix round.** Three rounds of shape-by-shape
  rules have each fixed the named cases while a blind set stays flat (30/62) and clean damage grows.
  Options to put to the operator: (a) keep going rule-by-rule, accepting a stated residual and
  measuring a fixed blind set each round; (b) change approach — redact by KEY CONTEXT + entropy
  (any value of a secret-named key/flag/env, plus high-entropy tokens near secret words) and accept
  more clean damage; (c) keep the shape rules but do NOT arm capture on any instance until a
  named blind-set floor (e.g. ≥90%) is met. Then fix round 4 (🔴 grep prefixes first), delta audit,
  merge after #215, then #217 (retarget each to `main`; re-run the merged tree when the base moves).
  forcing: security — the redactor is the only control between raw transcripts and the store (O1).
3. **S3** (upload + transcript store in cairn-ui) and **S11** (client read ledger) — S11 must land
   before transcripts are armed on any real instance. forcing: user — operator chose the build.

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
