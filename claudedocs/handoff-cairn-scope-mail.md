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
- **Plan MERGED** `4413feb` (#221, 6 audit rounds; ladder stopped on the operator's "merge once the
  latest round has no 🔴" rule). Operator decisions: O1 per-session read state; O2 senders need
  WRITE; O3 pointer + preview, silent when empty; O6 Postgres in cairn-ui (migration 3); O7 no ack
  in v1; O8 sender or scope admin retracts; **O9** agents use a per-host NARROWED JOURNAL credential
  (O5's token-file rows were REVERSED: the deployed UI deliberately holds no store token file, and
  that file's single row is shared by every client); D2–D6 cut (urgent bell, ack, per-host cursor,
  per-sender quota, a Go copy of leakscan's patterns). Decision 17 (coordinator default, reversible):
  verb narrowing — agent credentials carry read+write, never admin.
- **S0 MERGED:** cairn #230 (`internal/memo` trust-boundary renderer, via merge train 2 → `9292da2`)
  and tooling #2125 (the hook, `8b43e7f`, UNREGISTERED — `~/.claude/settings.json` does not
  reference it). Stored text sits behind a `func() *stored` carrier: a plain pointer leaked under
  `%s`/`%q` (344/440), the func carrier 0/440, and an independent audit measured 0 leaks over
  25,631 fmt/encoder/template/slog/panic cases.
- **opencode can show text to its model only through a tool-call plugin** (`tool.execute.after`
  appending to output; 1 canary run vs 1 control, one host, opencode 1.18.29).
- Nothing is deployed for memos beyond the library; no route, no verb.

## Next steps (ranked)
1. **S1** — migration 3, the memo store, send/read authorization, the per-scope quota, the send-time
   secret scan, AND the round-6 + S0-audit carry-overs below. forcing: user — operator asked for scope mail.
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

## How to verify
```bash
git -C ~/workspace/cairn log --oneline -1 -- internal/memo
nix develop ~/workspace/cairn -c go -C ~/workspace/cairn test ./internal/memo
grep -c cairn-memo-hook ~/.claude/settings.json   # 0 until S4 registers it
```
