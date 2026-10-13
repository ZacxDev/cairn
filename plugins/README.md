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
| `internal/endpoint` | the two rules every client that carries a credential shares: no redirect is followed, no cleartext off the host |
| `internal/hoststate` | what a plugin remembers on its host: a 0600 JSON file written whole, and the per-session failure BACKOFF |
| `internal/spend` | the plugins' OWN spend cap: a request ledger under `flock`, one file per program |
| `internal/transcripttext` | transcript records rendered as LABELLED lines — what a model reads instead of raw records |
| `summary/` | example plugin A — continuous session summaries (S9); `manifest.json` is what the operator registers |
| `clickup/` | example plugin B — ClickUp ticket edges and suggestions (S10); reads ClickUp, never writes to it |

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

Both URLs must be `https`, or `http` to a loopback address (the rule `cairn-capture` applies).
`-allow-plaintext-host <host>` (repeatable, exact match of the URL's host, no default, no environment
spelling) admits a plain-http `-worker-url` to that host — for a listener on a private ENCRYPTED mesh
address — and the run says so on stderr; it never applies to `-provider-url`. Neither client follows
a redirect, exception or not — Go re-sends `x-api-key` to a redirect target on any host and the
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

## Plugin B: `clickup` (S10)

Matches ClickUp tickets to sessions (operator decision O6). A ONE-SHOT run for the same kind of timer:

```
cairn-clickup -worker-url https://<worker listener> -token-file ~/.config/cairn-clickup/token \
              -clickup-token-file ~/.config/cairn-clickup/clickup-token -clickup-list <list id> \
              [-clickup-team <workspace id>] [-repo ~/src/alpha]… [-gh-bin gh] [-store-root <cairn cache root>]… \
              [-key-file ~/.config/cairn-clickup/key -provider anthropic -model claude-opus-5-5]
```

It writes THREE output types, and they are three different claims (decision 14):

| type | what it claims | made by |
|---|---|---|
| `ticket-edge` | the session NAMES this ticket: `{system, ticket_id, custom_id?, signal, locator}` — the signal says how, the locator where | a deterministic matcher; `model` reads `none (deterministic match)` |
| `ticket-suggestion` | a model GUESSES this ticket: `{ticket_id, confidence, rationale}` | the provider; never promoted to an edge, at any confidence |
| `ticket-scan` | this pass EXAMINED the session through its watermark: `{system, edges, suggestions, model_asked}` | every completed pass, written last |

### What counts as naming a ticket

ClickUp's id charset was not found in a primary source (R5), so nothing matches because it LOOKS like
an id. A ticket is named only by:

- a task URL — `https://app.clickup.com/t/<id>` (the id is the canonical id; it needs no list), or
  `https://app.clickup.com/t/<workspace>/<custom-id>`, resolved through the fetched list (and, with
  `-clickup-team`, only for that workspace);
- a CUSTOM id that is a member of the fetched list, standing as a whole token (`ALPHA-12` is not in
  `ALPHA-123`);
- in a STRUCTURED field only (a branch name, a commit trailer's value, a PR's head branch): a whole
  token that is a fetched canonical id, or one the same session named by URL;
- an explicit `clickup:<id>` ref in the front matter of an entry the session wrote.

The edge always stores the CANONICAL id; a custom id the list does not resolve is no edge.

| signal | read from | locator |
|---|---|---|
| `transcript-url` | a URL in any string of any record, any stream (`view=raw`) | `{stream, seq}` |
| `branch-name` | a record's `gitBranch` | the first record that carried the branch |
| `commit-message`, `commit-trailer` | `git log` in each `-repo`, for commits whose TRAILER block names the session id (`Claude-Session-Id: <id>`, any key) | `{commit}` (the full SHA) |
| `pr-body` | the body of a PR a `pr-link` record names, through `-gh-bin` | `{pr}` |
| `pr-link` | that PR's TITLE and head branch | `{pr}` |
| `entry-ref` | `refs:` (a list; a scalar `refs:` is an entry the store refuses) of a cached entry (`-store-root`) carrying the session's `[cairn: <actor>/<session>]` attribution | `{entry}` (`<scope>/<entry>`) |

One edge per (ticket, signal) per session: an edge the session already holds is never written again.

### Suggestions

With `-key-file`, ONE request per pass: the session's latest stored `summary` (plugin A's output, read
through `output:read:summary` — the cross-plugin read) or, when none is stored, the session's opening
records as labelled lines (first pass only), plus the candidate tickets — id and NAME, most recently
updated first, at most `-max-candidates`, leaving out every ticket the session already has an edge or
a suggestion for. The answer must be a JSON array; a pick outside the candidate list, without a
confidence in [0, 1], or below `-min-confidence` is dropped, and the best `-max-suggestions` are
written. A pass with nothing new to describe (the same summary as last pass) asks nothing. The spend
cap is plugin A's (`-max-requests-per-day`, its own ledger file).

### ClickUp, read-only

`GET /api/v2/list/<id>/task` (closed tasks and subtasks included, 100 a page) and nothing else — the
client has no other request, and the suite asserts the fake saw only `GET`s. A 429 is waited out
until the `X-RateLimit-Reset` it names (at most `-clickup-max-wait`, 4 attempts). The ticket list is
fetched BEFORE any session and only when something is pending; if the fetch fails nothing is written.
What is kept of a task: id, custom id, name. The name stays in memory unless the operator passes
`-store-ticket-titles`, which sends it as each edge's and suggestion's `title` (Q8: the default
stores ticket ids only) — and that works only against an instance started with
`-plugin-store-ticket-titles`; any other refuses the titled write with a 400.

The ClickUp token follows the module's rules for every credential (`internal/endpoint`,
`internal/secretfile`): a 0600 file, one header, no redirect followed (a 3xx is an error — Go would
re-send `Authorization` to its target), `https` or loopback only, and scrubbed from any error text.
`-allow-plaintext-host` is plugin A's flag and applies to `-worker-url` alone — never to ClickUp or the
provider, which are on the public internet.

### The state file: an unfinished pass is finished, a refused one backs off

`-state-file` (default `sessions.json` beside the spend ledger, mode 0600; `internal/hoststate`, shared
with plugin A) holds two things per session.

**The redo marker.** Every output of a pass carries the pass's watermark, so the FIRST one written
already takes the session off the pod's pending list. A pass that then stops — a refused write, a 500,
the run's deadline, a killed process — would leave a session that never grows again without its
remaining edges for good. So the pending item is saved before the pass's first write and cleared when
its scan is written; a session carrying the marker is worked on again whether or not the pod lists it,
from record 1 (an edge it already holds is not written twice). The marker also brings back a pass that
stopped at the 100-edge cap, and one whose host source (a repository, the GitHub CLI) could not be
read.

**The backoff.** A pass the pod REFUSED (507 quota, 400, the 403) or whose host source failed is
skipped for 15 minutes doubling to 24 hours; a success, or the session growing, clears it. A 5xx, an
unreachable pod and the run's own deadline are transient: retried by the next run, no backoff. A
session the pod no longer lists is given up after 6 failed attempts at one size (a line says so), and
at once when the pod answers its 403 for it (deleted, or toggled off). The run asks the pod for its
maximum page (100) and WORKS ON `-limit` sessions not in backoff, unfinished passes first.

Exit codes: 0 the run finished, 1 a session, a source or the ticket fetch failed, 2 usage. The last
stdout line is `SUMMARY clickup-run: sessions=… edges=… suggestions=… requests=… failed=… capped=… skipped=…`.

### As built — departures from the plan, each deliberate

- **`ticket-scan` is a FOURTH output type** (the plan's closed set had three). Decision 13 advances a
  session only when the plugin WRITES an output, and a matcher usually has nothing to say: without a
  type that records "examined, found nothing", every unmatched session stays at the head of the
  pending list forever and a page of them starves the rest. It also marks a pass as finished, which is
  how an interrupted pass is detected and redone from record 1.
- **Which commits are a session's** — the plan names a repository list, not the join. Only a trailer
  naming the session id is used; a branch or a time window would be a guess.
- **The `pr-link` signal** is the linked PR's title and head branch; the plan names the signal, not
  its source.
- **Ticket NAMES are sent to the provider** as candidates whenever a model is asked. Q8 governs what
  the POD stores (ids only by default); run without `-key-file` if names must not leave the host.
- **`get one task` (R5) is not called**: a `/t/<id>` URL already names the canonical id.

### What it cannot see, stated

- **A session that stamps no session trailer has no commit signal**, and an entry it wrote only with
  `put`/`create` (no attribution) has no `entry-ref`.
- **Host sources are read when a session is PENDING.** A commit made, or a PR body edited, after the
  session's last finished pass is seen only if the session grows again. (A source that FAILED is
  retried: the state file's redo marker.)
- **A session that LISTED tickets is linked to every one it listed** — a URL in a tool result is a
  URL. At most 100 new edges a pass; the next run writes the rest.
- **The redo marker is on ONE host.** A pass interrupted on a host whose state file is then lost is
  finished only if the session grows (its last output is not a scan, so it is re-read from record 1).
- **A summary written WHILE a pass runs is not asked about**: "new" is judged by the pod's
  whole-second stamp against this plugin's previous scan, so a summary stamped at or before that scan
  waits for the summary after it.
- **A URL split across two strings, or cut short by a tool, is no URL**: `…/t/abc...` is refused
  rather than read as ticket `abc`.
- **The entry reader is a narrow second reader**, not the store's parser: it reads the attribution
  trailer and the `refs:` key and nothing else; a file it misreads costs an edge.
- **Suggestions are best-effort**: a capped, failed or unconfigured model leaves the pass complete
  with `model_asked: false`, and that session is not asked again until its summary changes.
- **A suggestion whose write the pod refused is not kept**: after the backoff the model is asked — and
  paid — again. Plugin A caches its unwritten summary; this plugin does not cache.
- **More than 100 sessions in backoff at once hide the rest** — the pod's pending page is 100.
- **The fakes prove the PLUGIN, not the contract** — the real listener is the parent module's
  `internal/worker` `TestTheClickupPluginAgainstTheRealListener`, which also stores a summary through
  plugin A's registry entry and has plugin B read it. A real ClickUp workspace and a real provider are
  never called by CI.
