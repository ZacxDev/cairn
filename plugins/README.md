# plugins — the example plugins (transcripts plan S9, S10)

Out-of-process programs (operator decision O4) that read session transcripts through `cairn-ui`'s
plugin API and write back DERIVED records, which the browser surface renders as labelled claims
(decision 11). Plain HTTP+JSON, never MCP (O7). The API's server side is `internal/plugins` and
`internal/worker/plugin.go` in the parent module; its README is the contract's other half.

| path | what |
|---|---|
| `go.mod` | a NESTED module, **stdlib-only** — `internal/depspolicy` declares it `StdlibOnly` and refuses any `require`, the parent module's included |
| `internal/pluginapi` | the plugin API client: pending, transcript pages, outputs, the one write |
| `internal/llm` | the model provider: ONE request shape, spelled for the Anthropic Messages API and an OpenAI-compatible chat-completions API, over `net/http` — no SDK |
| `internal/secretfile` | the only way a credential is taken: a file, mode 0600, never argv or the environment |
| `summary/` | example plugin A — continuous session summaries (S9); `manifest.json` is what the operator registers |

## Plugin A: `summary` (S9)

A ONE-SHOT run for a host-side user timer (say every 15 minutes):

```
cairn-summary -worker-url https://<worker listener> -token-file ~/.config/cairn-summary/token \
              -provider anthropic -model claude-opus-5-5 -key-file ~/.config/cairn-summary/key
```

1. `GET /plugin/v1/pending` — the sessions this plugin is ON for that grew past its last output
   (decision 13; the pod holds the watermark, this host holds no progress state).
2. Per session: its previous summary (`GET /plugin/v1/outputs?type=summary`, the id the pending item
   names) and ONLY the main stream's records after that summary's watermark, read with
   `view=conversation` (bookkeeping left out as placeholders).
3. The records are rendered as labelled lines — `[human]`, `[assistant]`, `[tool call Bash]`,
   `[tool result]`, and `[RUNTIME-GENERATED …]` for the runtime's compaction summary and every `system`
   record — tool inputs/results clipped to 2 KiB, other text to 32 KiB, thinking left out.
4. Requests of at most `-chunk-bytes` of records each, every one carrying the previous summary: a
   rolling chain whose last answer is the new summary.
5. `POST /plugin/v1/outputs` — `{"text": …}` (≤ 16 KiB) with the watermark: main at the last record
   summarised, and — once main was read to its end — every stream the pending item listed.

Exit codes: 0 the run finished (a capped run included), 1 a session failed, 2 usage/configuration.
The last stdout line is `SUMMARY summary-run: sessions=… written=… requests=… failed=… capped=… skipped=…`.

Both URLs must be `https`, or `http` to a loopback address (the rule `cairn-capture` applies), and
neither client follows a redirect — Go re-sends `x-api-key` to a redirect target on any host and the
bearer token to the same host on another port, so a 3xx is an error (`internal/endpoint`).
`-effort` (Anthropic's `output_config.effort`) is sent only when set: models that do not take it
answer 400.

### The spend cap

`-max-requests-per-day N` (default 50) over a rolling 24 hours, counted in `-spend-ledger`
(`$XDG_STATE_HOME/cairn-summary/spend.log`) under `flock`, the line written BEFORE the request. A
request is bounded on both sides (`-chunk-bytes`, `-max-output-tokens`), so the request cap is a cost
cap. `0` sends nothing — the S9 test's assertion. The other ceiling that holds is a spending limit set
at the provider (decision 12: the pod never sees the key, so it cannot cap anything).

### Backoff, and a summary whose write was refused

`-state-file` (default `sessions.json` beside the spend ledger, mode 0600) holds what the stateless
pending query cannot know. A session that fails DETERMINISTICALLY — a provider 4xx other than 429, a
refusal or truncated answer, an empty answer, a write the pod refused (507 quota, 400, 403) — is
skipped for 15 minutes, doubling per failure to 24 hours; a success clears it and so does the session
growing. The pod lists oldest-first, so the run asks for the pod's maximum (100) and works on the first
`-limit` sessions NOT in backoff: failing sessions cannot hide healthy ones behind them. A summary whose
write was refused is kept there and re-POSTed on the next attempt without calling the provider again;
it is dropped when written, when the pod's last output for the session changes, or when the pod stops
listing the session (deleted, toggled off).

### What it cannot see, stated

- **Subagent streams are not read.** Their work reaches the summary only as the spawning call's
  result in main; the watermark still covers them so the session stops being pending.
- **One worker per plugin** (decision 13): two hosts running it duplicate the work, and each has its
  own ledger, so each gets the whole cap.
- **A crashed run repeats its last session's requests** — a chain's progress is written when it ends,
  is capped, or a request fails (an error, an empty answer, the run's own deadline), but a KILLED
  process writes nothing, and the ledger has already counted them.
- **A non-deterministic failure is retried every run with no backoff**: a provider 5xx or 429, an
  unreachable endpoint, the run's own deadline. One request per such session per run; the cap bounds it.
- **More than 100 sessions in backoff at once hide the rest** — the pod's pending page is 100.
- **A cached summary is transcript-derived text at rest on this host** until its write lands or the
  pod stops listing the session.
- **The host clock decides both windows**: stepped FORWARD it ends a backoff and the spend ledger's 24
  hours early (more spend than the cap intends); stepped back it lengthens both.
- **opencode text parts are not attributed**: the role is on the message's `info` record, which the
  conversation view leaves out, so they render as `[text — opencode, speaker not recorded…]`.
- **Prompt injection** (T8): the system prompt says the transcript is data; what holds is downstream
  — an output is a labelled, derived claim, rendered as text only, and this program writes nothing
  else.
- **The fakes prove the PLUGIN, not the contract**: they are written from the same spelling as the
  client. What runs this binary against the REAL worker listener — registry parsed from the committed
  `manifest.json`, a real journal row, a real archive — is the parent module's
  `internal/worker` `TestTheSummaryPluginAgainstTheRealListener`; a real provider is never called by CI.
