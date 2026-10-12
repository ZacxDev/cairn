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
- **Plans MERGED:** #208 `8c61b21` (read scopes via a CLIENT READ LEDGER, O11), #211 `a3e7de3`.
- **Operator decisions:** O12 binary ships (closed list of known signatures); O13 the plan's
  recommendations as defaults (Q5: the client instance is NOT armed without an explicit decision);
  O14 90 days, ~20 GB per capturing host; **O15** redact by key context + entropy, tool line
  prefixes normalised first, arming gated on a FRESH auditor-written held-back set scoring ≥90%
  leaks caught AND ≤15% clean damaged; **O16** heuristic redactor rounds ENDED after round 7, S1
  merged with capture UNARMED everywhere; **O17** the held-back gate (closing-condition part 5) is
  WAIVED as an arming precondition — ship capture without perfect redaction; part 5 stays a
  measurement, last score `leaks caught=157/190 clean damaged=18/175`.
- **S0–S3 MERGED:** S0 #215 `76ddc7e`; S1 #216 `7ca2e25` (`internal/redact`, rounds 4–7); S2 #217
  and S3 #224 through merge train 1 (#229, merge commit `b06f15c`, whose tree equals the CI-tested
  train tree). S3 = transcript upload + per-session store in cairn-ui, DISARMED: every upload is
  answered 503 `capture-disarmed` unless cairn-ui starts with `-arm-transcript-capture` (no env
  spelling, no default).
- 🔴 **CAPTURE IS UNARMED, AND ARMING IT WOULD CAPTURE NOTHING.** Still blocking: (1) **S3b** — S3
  (#224) built only the POD half; there is no host-side uploader (`internal/capture/sink.go` has
  no HTTP sink, nothing calls `capture/v1`) and `tests/plugins/e2e.sh` (clauses a, b, c, h) does
  not exist; built as the next PR in this lane; (2) S11 (client read ledger) is unbuilt. NOT blocking any
  more: O15's held-back gate — waived as an arming gate by O17, kept as a measurement (last score
  `leaks caught=157/190 clean damaged=18/175`, 82.6%, round-6 head); arming preconditions
  **AP1–AP3** — closed by #258 (transcript-dir ownership + state-path refusal; a 4 MiB cap on an
  assembled record, 413; meta-less sessions and idle frames swept; names/cursors re-checked; the
  owner re-asked per request); **AP4** — now a re-measure-and-record step on the commit being
  armed, not a gate. Arming itself stays the operator's act (`-arm-transcript-capture`).
- **Deployed:** the personal instance runs `sha-9292da2` (deployment trunk `faa94f1`) with NO worker
  flags set — cairn-ui logs "serving 49 route(s)" and starts no worker listener.
- Claims: all released.

## Next steps (ranked)
1. **(done) #216 redactor** — merged `7ca2e25` under O16; capture unarmed. forcing: security —
   the redactor is the only control between raw transcripts and the store (O1).
2. **S3b** (the agent upload + `tests/plugins/e2e.sh`, incl. the uploader's 413 handling for a
   record over the 4 MiB cap) next, then **S11** (client read ledger); then **S4–S10** per the
   plan. S3's POD half is done (#224). Arming needs S3b, S11 and the AP4 re-measure-and-record
   step (O15's gate waived by O17; AP1–AP3 closed by #258).
   forcing: user — operator chose the build.
3. **(done) #215 follow-through** — delete the now-unused `zach/transcripts-s0`, `-s1`, `-s2` and
   `-s3` branches (every PR based on them is merged). forcing: user — operator asked for S0–S2.

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

- **Heuristic redactor rounds did not converge on unseen data** — four fresh auditor-written
  held-back sets scored 154 → 155 → 157 of 190 across rounds 4–6 while each round's own fixes
  opened a new defect class (prose damage, identifier recovery hiding random tails, a budget test
  coupled to the live tree and GOROOT). Do not resume rule-by-rule rounds; O16 records why. via: measurement
- **A held-back set is spent once a fixer has seen it** — every round needs a NEW set written by an
  auditor the fixer never saw, kept out of the repo and PRs. via: measurement
- **`internal/redact` used to match its own `[redacted:…]` markers** (1,502 re-hits over seeds 1–40),
  which would have made the pod re-check refuse every upload. S3 cuts marker regions out of a span
  instead of dropping it; a value glued AFTER a marker (and a word-like value before one) still
  leaks, at base too — declared residuals pinned by `TestAMarkerDoesNotShieldASecretBesideIt`. via: measurement
- **The damage budget pins a FROZEN corpus** (`internal/redact/testdata/`); the live-tree and GOROOT
  sweeps are opt-in (`-redact.live-budget`) because the live form failed on unrelated doc edits and
  under Go 1.26. A whole-stdlib sweep still sees +112 lines the frozen corpus cannot. via: measurement
