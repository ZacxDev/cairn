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
  `sabotaged=16 caught=16` (the measured count; the plan first said 14), `cairn-capture --self-test` prints `planted=P caught=P
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
  measurement, last score `leaks caught=157/190 clean damaged=18/175`; **O18** answers Q16 with
  option (a) — keep every client current (both write the read ledger from S11 on), callers read
  through the client; F2 not narrowed, no share measurement; Q16 is not an arming blocker.
- **S0–S3 MERGED:** S0 #215 `76ddc7e`; S1 #216 `7ca2e25` (`internal/redact`, rounds 4–7); S2 #217
  and S3 #224 through merge train 1 (#229, merge commit `b06f15c`, whose tree equals the CI-tested
  train tree). S3 = transcript upload + per-session store in cairn-ui, DISARMED: every upload is
  answered 503 `capture-disarmed` unless cairn-ui starts with `-arm-transcript-capture` (no env
  spelling, no default).
- **EVERY SLICE IS MERGED (S0–S11).** S3b #260 `1cbf06b` (host uploader, `cairn-capture --run`,
  `tests/plugins/e2e.sh`); S4 #261 `3629649` (`transcript.Visible` + `V`, the collapsed transcript
  on `/session`, `GET /session/transcript`); S11 #266 `8184e79` (the client read ledger, both
  clients; the pod folds it; F1 decided at view time); S7 #268 `c31ca3d` (`GET /my-sessions`);
  S8 #269 `59f0ef6` (JSON skeleton/records/tool routes + the Go-only `transcript` verb); S5 #270
  `8ba0327` (`internal/plugins`, the plugin API on the worker listener, `/plugins` and the toggle
  POSTs); S9 #272 `cebd193` (`plugins/` nested module, `cairn-summary`); S6 #273 `ad7306e` (outputs
  rendered, the owner's delete, the capture `withdraw` route, deletion FINAL for a root,
  `cairn-capture -allow-plaintext-host`); S10 #274 `b888b2d` (`cairn-clickup`, `ticket-scan`, the
  closing wiring). Each PR's batched 🟡s are in its final PR comment; the ones still open are
  under "Defects (batched)" below.
- **Closing condition, measured at `b888b2d`:** part 1 met (slices merged); part 2 —
  `tests/plugins/e2e.sh` `passed=22 failed=0 expected=22`, `--self-test` `sabotaged=16 caught=16`
  (the plan now states the measured 16: all fourteen clauses are wired, (a) and (g) carry two
  sabotages each); part 3 — `cairn-capture --self-test` `SUMMARY redaction: planted=79 caught=79
  clean-damaged=0`; part 4 — the plugins suite, 83 PASS / 7 ok in the `go` job; part 5 — WAIVED as
  a gate by O17, kept as a measurement, NOT re-measured (below).
- 🔴 **CAPTURE IS UNARMED EVERYWHERE, AND ARMING IS THE OPERATOR'S ACT.** Nothing code-side blocks
  it any more: O15's gate is waived (O17), Q16 is answered (O18), AP1–AP3 are closed (#258), S3b
  and S11 are built. A `cairn-ui` with no `-worker-addr`/`-transcript-dir` renders nothing new and
  serves no worker listener.
- **AP4, RECORDED at `b888b2d`** (a record step since O17, not a gate; `internal/redact` is
  byte-identical from `8ba0327` to `b888b2d`): the three marker-adjacency tests
  (`TestAMarkerDoesNotShieldASecretBesideIt`, `TestTheRedactorIsQuietOnItsOwnOutput`,
  `TestRoundSixHeldBackOracleIgnoresMarkers`) — 3 run, 3 pass under go1.25.14, so T1's declared
  residual describes this redactor (a low-entropy value glued directly AFTER a marker under a key,
  and a word-like value glued BEFORE one, still ship). **The held-back score was NOT re-measured:**
  it needs a fresh auditor-written set and none exists in any repository; the last score on
  record is `leaks caught=157/190 clean damaged=18/175` at the round-6 head, and `internal/redact`
  has changed since that commit. Neither line is a gate.
- **Needs deploy (coordinator):** the `cairn-ui` image at `b888b2d` carries S4–S10 with capture
  OFF. **No database migration** in `16f2a0a..b888b2d` (`internal/pgstore` is untouched). The
  image for `b888b2d` was still publishing when this was written — confirm the tag before pinning.
- Claims: `cairn-transcripts-arming` released with this doc.

## Arming — the ordered steps, and what each needs from the operator
The full runbook (literal manifests, unit files, commands) is kept OUT of this repository because
it names a deployment; this is its skeleton. Nothing here has been run.
1. **Deploy the image at `b888b2d`, no new flags.** Needs: nothing but the deploy.
2. **Host tooling.** The tooling repository installs neither `cairn-capture` nor the plugins: add
   `packages.cairn-capture` (and, later, `cairn-summary` / `cairn-clickup`) from this flake, move
   its cairn pin to `b888b2d` (O18 needs ≥ `8184e79` for the read ledger; the capture agent needs
   ≥ `ad7306e` for `-allow-plaintext-host`, withdrawal and the 410 handling), and add a user
   service + timer for `cairn-capture --run` that is inert until its token file exists. Where the
   host reaches the worker listener over plain http on a protected private network, the unit
   names that host exactly with `-allow-plaintext-host <alias>=<host>`. ONLY the default
   instance gets a `-worker`; the client instance is not armed (Q5).
3. **Pod, capture still disarmed:** a dedicated volume for `-transcript-dir` (never a directory
   holding any `cairn-ui` state — AP1 refuses it), `-worker-addr`, `-worker-tokens`,
   `-worker-owner`, `-transcript-retention 90d`, `-transcript-quota` (O14), a route to the worker
   port, and a memory limit raised from 256Mi (one worst-shape cap-sized record allocates
   ~253 MiB in the re-check walk — `internal/transcript/archive/arming_test.go`; 768Mi proposed).
   The `-worker-tokens` FILE MUST EXIST before these flags land (a missing file is exit 78).
   Needs: the storage class/size and the memory limit confirmed.
4. **🔴 OPERATOR OK REQUIRED — mint one capture token per (owner, host):**
   `cairn-ui -issue-worker-token capture -worker-owner <kind:id> -worker-host <host label>
   -worker-tokens <file>`, run where the control journal is (inside the pod), its output written
   straight into the host's 0600 token file and never to a terminal or a transcript. Needs: the
   owner principal confirmed, and the OK.
5. **First pass by hand, on one host, with `--dry-run` first.** 🔴 On a host that knows TWO
   instances the first `--run` makes holds FINAL even against a disarmed listener, and a session
   with `*` in `V` is held: on one measured host a sample of 81 sessions gave 63 held and 18
   shipping. Needs: the operator to look at the full dry-run counts and accept that share (O18
   accepted the residual in principle; this is its measured size on one host, one sample).
6. **The arming commit, last and alone: `-arm-transcript-capture`.** Needs: the operator's act.
7. **Verify** (deployed ≠ verified): the pod's mode line says ARMED with the minted row count; the
   unit's run line shows `requests` and `shipped` above 0; `/my-sessions` lists a session in a real
   browser; a second principal is refused. Then enable the timer; the second host later.
8. **Plugins, later and separately:** register `summary` (every toggle OFF by construction), mint
   its plugin token (same OK), choose provider/model/cap; `clickup` after that. Rollback order: a
   plugin token row must be deleted before its registry entry.

## Open for operator review (agent decisions recorded in the plan)
- **Arc toggles:** `off` covers a member snapshot (a veto cannot be lifted by re-registering the
  arc); `on` reads the arc's LIVE members, so per-arc summaries follow a growing arc. Residual: a
  writer of an `on` arc's home scope can enrol a session that touched that scope and whose every
  other scope is `allowed`/`on` (plan decision 10, T14).
- **Route-back hold (decision 16):** a routing-table edit that sends a session back to an instance
  that deleted it HOLDS it where it is; an unanswering destination defers the withdrawal. Cost: a
  prefix can stay on an instance the table no longer routes to until its owner deletes it there.
- **A pod re-check 422 becomes a `refused-by-recheck` placeholder**, not a permanent hold (O17's
  consequence; decision 6): one record is lost, the stream continues, nothing refused is stored.
- **`-allow-plaintext-host`** on `cairn-capture` and the plugins' worker client: an exact-host,
  flag-only opt-in to plain http, for a deployment whose host-to-pod path is a protected private
  network. It is an unverifiable assertion about a network — the operator owns it.
- **Every served transcript read records `*`** in the reading session's ledger (owner-only for
  that session); kept under O18.

## Defects (batched)
- **`tests/dualrun`'s self-test is nondeterministic for one mutant.** On #274's head `6b094c9` the
  `dualrun` job failed once, exit 2, `mutants=7 caught=6`: the `audit-identity-field-dropped`
  mutant also tripped the `process` comparison where it declares `['audit']` only. A re-run of the
  SAME commit passed `mutants=7 caught=7`; `main` passed the job at `ad7306e`. The process-stream
  comparison flakes; not caused by S10 and not fixed.
- **`plugins/clickup` boundary rule drops `<type>-<ID>-<slug>` branch names** (`fix-ALPHA-12`,
  `ALPHA-12-2nd-try`) with nothing logged, while `x--ALPHA-12` still matches
  (`plugins/clickup/match.go` `boundedLeft`/`boundedRight`). Decide: narrow the left rule, or
  state the cost.
- **Guards removable with the suite green (#274 round 2):** bidi set membership
  (`internal/plugins/manifest.go`), a 401/403 on the destination probe not recorded as fatal
  (`internal/capture/agent.go`), Unicode neighbours of a canonical ticket token, `_` on the left.
- **`plugins/clickup`:** with the plugin's `-store-ticket-titles` on, an ids-only pod and a
  nameless ticket, the model's rationale is still stored; a hung `gh` is paid once per PR; a
  409-without-position / 404 / no-endpoint probe answer defers a withdrawal forever (logged and
  counted each run, unbounded).
- **`/my-sessions` and the pending/plugin paths read every `meta.json` per request** (measured:
  2,000 sessions 31 ms / 13.9 MB; 10,000 sessions 221 ms / 69.6 MB); no owner index.
- **A refused viewer gets a 500, not the uniform miss, when a session's `meta.json` does not
  parse** (S4; reachable on the client listener too since S8).
- **The redact re-check walk allocates ~253 MiB for one cap-sized record of short `x: y` lines**
  (recorded in `arming_test.go`); concurrent unframed uploads have no in-flight limit.
- **`internal/capture` state:** a `Rechecked` entry is left behind when a lost placeholder ack is
  settled through the probe; `CleanStateTemps` globs every `.state-*` in the directory while the
  lock is per state file; `pending` changed JSON shape with `Schema` still 1.
- **Read ledger files are never pruned** (`$XDG_STATE_HOME/cairn/read-ledger`), and
  `TestTheStatusTokenIsTheFirstStderrLineEvenWithADeprecationWarning` is order-dependent under
  `go test -shuffle=on`.
- **Round-0 deletion candidates recorded, not taken:** in `plugins/clickup` the host-local redo
  marker, the `entry-ref` source, the saved model answer and the ticket-title path; the
  OpenAI-compatible provider spelling in `plugins/internal/llm`; paging on `/my-sessions`.

## Next steps (ranked)
1. **Deploy `b888b2d`** (capture OFF, no migration). forcing: user — the operator chose to ship capture now (O17).
2. **Arming, steps 2–7 above**, stopping for the operator at steps 4, 5 and 6. forcing: user.
3. **The first two defects above** (the dualrun flake; the ClickUp boundary rule) before plugin B
   is toggled on anywhere. forcing: quality.

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
