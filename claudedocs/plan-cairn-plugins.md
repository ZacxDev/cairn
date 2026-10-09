# Plan: session transcripts in the store, and out-of-process plugins over them

This is a DESIGN, not a description of anything built. None of it exists yet.

**Where the citations point.** Every claim about today's behaviour carries a `file:line` read off
`origin/main` at **`3046e2a`** (mobile plan S2 merged). Re-read before editing: the lines move.

**What was measured, and where.** The transcript formats were measured on **ONE host**, read-only,
at the time of writing: Claude Code **2.1.289** (`claude --version`) under `~/.claude/projects/`,
and opencode **1.18.29** (`opencode --version`) in `~/.local/share/opencode/opencode-stable.db`.
The instruments printed only STRUCTURE — record types, key sets, value TYPES, closed-vocabulary
enum values, counts and byte sizes — never a message, prompt, tool output or title. The scripts
lived in a per-agent scratch directory and are not committed. The second host, and any other
version of either runtime, were not measured.

**Examples are synthetic.** Sessions are `s-0001`, hosts `host-a`/`host-b`, scopes `alpha-notes` /
`beta-notes`, the two deployments "the personal instance" and "the client instance", ClickUp tickets
`clk0000a1`, dates are year-2000. Operator decisions are PARAPHRASED, never quoted
(`AGENTS.md`). No transcript content appears anywhere in this document: formats are described from
field names and types alone.

## Goal and premise

The operator asked for two things, carried in one plan because the second consumes the first:

1. **Session content in the store.** The full raw transcript of every Claude Code and opencode
   session — tool calls and tool outputs included — shipped from the host it ran on to cairn,
   redacted on that host before it leaves, and shown on the session page (`/session`) to the
   principals allowed to see it.
2. **A plugin system.** Out-of-process workers that read what (1) stored, through a
   capability-scoped API, and write back DERIVED records the browser surface renders. Two example
   plugins prove the abstraction: continuous session **summaries**, and **ClickUp ticket
   matching** (deterministic ID matches plus LLM suggestions).

The premise is that a session page today shows WHERE a session wrote (bullets, scopes, arcs,
`internal/ui/sessionpage.go:206-260`) but not WHAT it did, and that the answer to "what did that
session do" currently lives only on the host that ran it.

### What would make this unnecessary

Drop the work, or the named half, if any of these holds:

- **The host is always at hand.** If every reader of a session page is also someone with shell
  access to the host that ran it, the operator's own tooling already answers "what did that session
  do" locally, and shipping transcripts buys only a second, remote copy of an answer that exists —
  at the cost of a new, large, sensitive dataset (≈2.8 GB of raw Claude Code JSONL per week on the
  one host measured, R1).
- **Summaries are enough.** If nobody ever reads the raw content on the page, the plugin half could
  summarise ON THE HOST and ship only the derived text. The operator chose full raw content (O1),
  so this is recorded, not argued.
- **The ClickUp half is a lookup, not a store.** If explicit ticket IDs in branch names and commit
  messages are the only matches anyone trusts, a host-side script printing them answers the question
  without a plugin API. The deterministic half of plugin B is cheap either way; the API is justified
  by the LLM half and by summaries.

### closing-condition

- **closing-condition:** `check`. Four mechanical parts, all required:
  1. Slices **S0–S9 are MERGED** on cairn `main`, verified by content (the named files exist with the
     named tests), not by ancestry.
  2. **`tests/plugins/e2e.sh` exits 0** in the `go` CI job, with **`--self-test` printing
     `SUMMARY e2e-self-test: sabotaged=N caught=N`** where N is the clause count in the table below
     (**11** once every slice has landed), and the job's PASS floor set to the count measured when
     the script lands (the `tests/presence/e2e.sh` pattern, `.github/workflows/ci.yml:1045-1055`).
  3. **`cairn-capture --self-test` exits 0** in the `go` job and prints
     **`SUMMARY redaction: planted=P caught=P clean-damaged=0`** over the realistic synthetic corpus
     (decision 6), where P is the planted count declared by the corpus generator (asserted equal,
     not read off the run).
  4. **The two example plugins' own suites exit 0** in the `go` job against in-process fake LLM and
     fake ClickUp servers, with synthetic fixtures only.

  Each script exits **2** — "could not vouch", never a skip and never 0 — when a prerequisite is
  missing (a Go toolchain, a built `cairn-ui` or `cairn-capture`, `opencode` absent for the
  opencode-reader leg: that leg uses a recorded synthetic export, see S2) or any of its own
  controls misbehaves.

  | clause | relationship it asserts | the sabotage that must turn it red | slice that wires it |
  |---|---|---|---|
  | **(a) resume** | an upload interrupted mid-stream and retried lands every record exactly once; a stale `from_offset` gets 409 naming the stored offset | accept any `from_offset` (no compare-and-swap) | S3 |
  | **(b) nothing unredacted reaches the pod** | fixture sessions carrying P runtime-generated secrets are captured and uploaded; a byte scan of the pod's transcript directory finds **0** of them; the SAME run with the agent's redaction disabled finds **P** (the positive control, reported as the pair) | skip redaction for `tool_result` content | S3 |
  | **(c) the pod re-checks** | a record that still matches the shared rule table is refused 422 and NOT stored | drop the pod-side scan | S3 |
  | **(d) visibility, owner** | the uploading owner sees the transcript section; a principal with no read on any written scope gets the same bytes as a session that has no transcript | render without the predicate | S4 |
  | **(e) visibility, every-scope** | over one store, viewer R reads `alpha-notes` AND `beta-notes` and sees it; viewer P reads only `alpha-notes` and gets the no-transcript bytes, although P's session PAGE is found | compute the written set over the VIEWER's readable scopes instead of the whole store (decision 3's trap) | S4 |
  | **(f) fail closed on an empty set** | a session with no attributed write and no declared scope is visible to its owner ONLY | treat "every scope of the empty set" as true | S4 |
  | **(g) toggles default off** | a plugin token receives 0 work items until a toggle is set; after `alpha-notes` ON it receives exactly the sessions whose every written scope is ON | default a missing toggle to ON | S5 |
  | **(h) capability scoping** | the summary plugin's token is refused (403, uniform) on a transcript of a session where it is OFF, and on writing a `ticket-edge` output | drop the per-session toggle check on `transcript:read` | S5 |
  | **(i) outputs inherit visibility, and say what they are** | a summary is shown to exactly the principals clause (e) admits, and its render carries plugin name, version, model and watermark | render outputs without the source predicate | S6 |
  | **(j) deletion cascades** | the owner deletes a session's transcript; its directory, its plugin outputs and its edges are gone; the plugin's next transcript read is 404 | delete the transcript but keep outputs | S6 |
  | **(k) budget** | a plugin whose reported usage reached its cap gets 0 work items until the window rolls | ignore the cap | S5 |

  The sabotages run on a scratch copy of the tree with its `.git` removed (the
  `tests/control_mutants.py` pattern), and each must be caught by ITS clause's own assertion.

  ⚠ **What the closing condition does NOT require**, stated so nobody reads it in: (i) any real
  deployment having transcripts; (ii) the operator judging summary QUALITY or match PRECISION —
  both are human judgements over real data and are post-close rollout (below); (iii) a real LLM
  provider or a real ClickUp workspace ever being called by CI. Redaction RECALL on real data is not
  measurable by any check that may run in this public repository (R6).

### Rollout (NOT part of the closing condition)

- **The personal instance first**, with capture armed on one host, retention set, plugins
  registered but every toggle OFF (O-default). The operator turns on summaries for one scope, reads a
  week of output, and judges it. The same for plugin B against one ClickUp list.
- **The client instance only after a separate operator decision** (Q5): it holds client principals
  and client-confidential content, and the LLM plugin there sends transcripts to a third party.

## STEP 1 — Research: what the two runtimes actually store

Tags: **[M]** measured here (one host, versions above); **[V]** a primary source fetched and read;
**[S]** a secondary source or search snippet, treat as a hypothesis.

### R1. Claude Code — `~/.claude/projects/<project>/<session>.jsonl` [M]

**Layout** (paths normalised; counts on one host):
- `<project>/<session-uuid>.jsonl` — the session's own records. **880** files, **2.7 GB**; per file
  p50 3.3 MB, p90 5.4 MB, p99 9.6 MB, max 13.5 MB.
- `<project>/<session-uuid>/subagents/agent-<id>.jsonl` — one per subagent. **6,755** files,
  **6.5 GB** — **subagents are ≈70% of all transcript bytes**. Beside each, an `agent-<id>.json`
  metadata file with keys `agentType`, `description`, `spawnDepth`, `toolUseId`, and on some
  `parentAgentId`, `spawnedWithWorktree`, `worktreeBranch`, `worktreePath`, `requestShape`,
  `requestNonInteractive` (key sets over 2,000 files).
- `<project>/<session-uuid>/tool-results/*.txt` (and a few `.pdf`, `.html`, `.jpg`) — tool output
  too large to keep inline, referenced from a record's `toolUseResult.persistedOutputPath`. 2,716
  files, 299 MB.
- `<project>/memory/*.md` — NOT transcript content; out of scope.
- **Growth:** files modified in the last 7 days total **2.80 GB** (226 top-level sessions); last 30
  days **8.97 GB** (864). **gzip -6 on a 30-file sample: 3.5×** (57.8 → 16.6 MB).

**Records.** One JSON object per line; 0 unparseable lines in the 300 newest files. Top-level
`type` values seen (counts over 300 files): `assistant`, `attachment`, `user`, `queue-operation`,
`last-prompt`, `permission-mode`, `mode`, `pr-link`, `ai-title`, `system`, `atis-latch`,
`bridge-session`, `history-suppression`, `frame-link`, `cost-state`, `agent-name`,
`artifact-autoreact-ledger`, `artifact-comment-monitor`, `continued-in`. **The set is open**: it is
the runtime's, not documented as a contract anywhere this plan found, and it grows across versions
(UNVERIFIED that it is undocumented — no primary source was found either way).

- **Envelope on conversation records** (`user`, `assistant`, `attachment`, `system`): `uuid`,
  `parentUuid` (string, or null at a root), `timestamp` (string), `sessionId`, `isSidechain`
  (bool), `cwd`, `gitBranch`, `version`, `entrypoint` (`cli` | `sdk-cli`), `userType`, and on some
  `agentId`, `slug`, `promptId`, `requestId`.
- **`assistant.message`**: `id`, `model`, `role`, `content`, `stop_reason`, `usage`, and more.
  Content blocks: `text {text}`, `thinking {thinking, signature}`, `tool_use {id, name, input,
  caller}`.
- **`user.message.content`**: a string, or blocks `text` and `tool_result {tool_use_id, content,
  is_error?}` whose `content` is a string (31,303) or a list of `text` / `image` /
  `tool_reference` items (1,736). **Tool output size:** p50 449 B, p90 4.1 KB, p99 23 KB, max
  656 KB (JSON-encoded).
- **`user.toolUseResult`** — a SECOND, structured copy of the tool's result, keyed per tool
  (`stdout`/`stderr`/`interrupted` for shell; `filePath`/`oldString`/`newString`/`originalFile`/
  `structuredPatch` for edits; `agentId`/`prompt`/`outputFile` for a subagent launch; …).
  `originalFile` carries a WHOLE file's contents.
- **Metadata-only records** carrying free text: `ai-title {aiTitle}`, `last-prompt {lastPrompt}`,
  `queue-operation {operation, content?}`. Records carrying account identifiers:
  `bridge-session {ownerAccountUuid, ownerOrganizationUuid}`,
  `artifact-autoreact-ledger {accountUuid}`, and an `attachment` subtype `credential_org`.
- **`pr-link {prNumber, prUrl, prRepository, timestamp}`** — 7,575 records: a DETERMINISTIC
  session→PR link the runtime already writes (plugin B uses it).
- **Byte share by component, 30-file sample:** `attachment` records 30.5%; the duplicate
  `toolUseResult` 16.4%; `tool_result.content` 13.6%; thinking `signature` 5.3%; `tool_use.input`
  4.2%; assistant `text` 2.0%; thinking text 0.1%. So **less than a fifth of the bytes are the
  conversation a reader means by "the transcript"**; the rest is harness bookkeeping and duplicates.
  Full raw content (O1) ships all of it; Q2 asks whether the duplicate and bookkeeping halves are
  worth their storage.

### R2. Claude Code — growth, sidechains, compaction [M]

- **Append-only, at one observation.** The newest top-level file grew 2,383,063 → 2,389,119 bytes
  over 20 s with its first 4 KiB unchanged. One file, one interval: consistent with append-only, not
  proof of it. **`timestamp` is NOT monotonic in file order** (1,724 regressions over the 50 newest
  files) — so a watermark must be a BYTE OFFSET, never a time.
- **Subagents are separate FILES, not interleaved records.** All 300 sampled subagent files had
  `isSidechain: true`, an `agentId`, and `sessionId` EQUAL to the parent session's id; 0 of 138,030
  records in 100 top-level files were sidechain records. So a subagent's trailer writes (if any)
  name the PARENT session id, and its transcript is a child stream of that session.
- **Compaction does not truncate the file.** It appears as a `system` record with subtype
  `compact_boundary` and `compactMetadata {trigger, preTokens, postTokens, durationMs,
  preservedSegment, preservedMessages, cumulativeDroppedTokens, …}` plus a `user` record with
  `isCompactSummary: true` and `isVisibleInTranscriptOnly: true` whose content is the RUNTIME's own
  summary. 10 boundaries in 300 files. The pre-compaction records stay above it (inferred from the
  append observation, not separately measured). A renderer must label the runtime's summary as
  runtime-generated, and plugin A must not mistake it for a human turn.
- Other `system` subtypes: `stop_hook_summary`, `turn_duration`, `away_summary`, `informational`,
  `agents_killed`, `local_command`, `scheduled_task_fire`.

### R3. opencode — `opencode-stable.db` (SQLite) [M]

**Schema** (read with `sqlite_master`, read-only URI; counts on one host): `session` (1,066 rows),
`message` (33,607), `part` (145,685), `event` (516,890), `todo`, `project`, `project_directory`,
`workspace`, `permission`, `session_share`, `session_input`, `session_message`,
`session_context_epoch`, `event_sequence`, migrations — **and `account`, `control_account` and
`credential` tables holding access/refresh tokens and credential values (0 rows here, but the
columns exist)**. A capture agent must never read those tables; decision 5 does not open the
database at all.

- **`session`**: `id` (`ses_` + 26 characters, 30 total, all 1,066), `parent_id` (non-null on
  **458** — opencode child sessions are SEPARATE session ids), `project_id`, `directory`, `title`,
  `version`, token and cost counters, `time_created`, `time_updated`, `time_compacting` (0 non-null),
  `time_archived`.
- **`message.data`** (JSON): `role` (`assistant` | `user`), `agent` / `mode` (a small closed set),
  `modelID`, `providerID`, `parentID`, `path`, `time`, `tokens`, `cost`, `finish`, `error?`,
  `summary?`.
- **`part.data`** (JSON) `type`: `tool` (40,759), `step-start`, `step-finish`, `reasoning`, `text`,
  `patch`, `file`, `subtask` (2), `compaction` (1). A `tool` part: `callID`, `tool`, `state
  {status: completed|error|running, input, output, metadata, title, time, error?, attachments?}`.
  `part.data` averages 3.1 KB, max 2.8 MB, total 458 MB.
- **Parts are MUTATED, not appended**: 135,976 of 145,685 parts have `time_updated >
  time_created` (streaming fills them in), and the `event` log carries `message.removed.1` (262).
  So an opencode watermark is **(part id, `time_updated`)**, and an upload is an UPSERT by part id —
  the Claude Code byte-offset model does not apply.
- **`opencode export <sessionID>`** prints one JSON document `{info: {id, slug, projectID,
  directory, path, title, agent, model, version, summary, cost, tokens, time}, messages: [{info,
  parts: [...]}]}` — the same message and part objects, without opening the database ourselves.
  Measured on ONE 21-part session: 0.8 s, 28,051 bytes, exit 0. **`--sanitize`** ("redact sensitive
  transcript and file data", per its `--help`) produced 17,264 bytes for the same session — WHAT it
  removes was not measured, and O1 (full raw) rules it out as the redaction step anyway.

### R4. Redaction — what a pattern scanner can and cannot do

- **`tests/leakscan.py`'s `credential` rule** (`tests/leakscan.py:274-294`) recognises PEM private
  keys and certificates, AWS access-key ids, GitHub tokens, Slack tokens, age secret keys, and an
  `authorization`/`bearer` keyword followed by ≥ 20 token characters. It was built to keep a PUBLIC
  SOURCE TREE clean, and its other rules (private IPs, the deployment's domains, a closed digest set
  of identifiers, dated incidents) are about this repository, not about secrets in a session.
- **What a transcript carries that that rule cannot see** (shapes, from R1/R3; none measured as
  present — measuring that would mean reading content): a dotenv file printed by a shell tool
  (`KEY=value` with no recognisable prefix); a Kubernetes `Secret`'s base64 `data:` values; a
  database DSN with a password in its userinfo; an API key of a provider the rule does not list
  (LLM providers, ClickUp's personal tokens [S]); a JWT outside an `Authorization` header; a
  password typed into a prompt; a credential inside an IMAGE or PDF tool result; and any of these
  JSON-escaped (`\n`, `\u…`) inside a string value, where a raw-byte regex sees different bytes.
- **No scanner recognises unshaped secrets.** Entropy heuristics trade false positives (commit
  SHAs, digests, UUIDs, base64 payloads — all common in transcripts) for recall. Public secret
  scanners publish large pattern sets [S]; none can be the floor of a guarantee, which is why the
  residual is stated (threat T1), not designed away.
- **A scanner that only recognises its own textbook examples passes a real leak** (`AGENTS.md`;
  `tests/leakscan.py` runs its controls on every invocation, `:837-875`). The corpus in decision 6
  is built for that reason from RUNTIME-GENERATED values in realistic positions.

### R5. ClickUp

- **Rate limit:** per token, **100 requests/minute** on Free Forever, Unlimited and Business;
  1,000 on Business Plus; 10,000 on Enterprise; over the limit answers **429** with headers naming
  the reset [developer.clickup.com/docs/rate-limits] [V].
- **Task ids and URLs:** the task permalink is `https://app.clickup.com/t/<task-id>`; cairn already
  renders `clickup:<id>` refs that way and REFUSES an id containing `/`
  (`internal/store/refurl.go:82-92`). Workspaces with **custom task ids** (`PREFIX-123`) address a
  task as `/t/<workspace-id>/<custom-id>` and the API takes `custom_task_ids=true&team_id=<id>`
  [S, third-party docs quoting the API reference]. ClickUp's own task-id charset and length were
  NOT found in a primary source; the example in `internal/store/entry.go:578` is 9 lowercase
  alphanumerics. **Decision 14 therefore never matches a bare ID shape against free text** — only a
  URL, an explicit `clickup:` ref, or a custom id drawn from the workspace's own CLOSED list.
- **No write-back** is in scope (O6); the read API surface plugin B needs is: list tasks in a
  configured list, get one task. Confirm against the primary API reference at S8.

### R6. What a public repository cannot measure about redaction

Recall on REAL transcripts — "of the secrets actually present, how many were caught" — needs a
ground truth that only reading real content provides, and this repository may not carry or quote
real content (`AGENTS.md`). So redaction is measured on a synthetic corpus (decision 6) and its
REAL recall is an operator-side measurement: Q4 recommends a host-local, never-committed audit
script that prints only counts.

## STEP 2 — What exists today, read off the code (`3046e2a`)

### A session is not a record

- **There is no session record anywhere.** A session id is an opaque string bounded by
  `write.SessionComponent` = `[A-Za-z0-9][A-Za-z0-9_.-]{0,63}` (`internal/write/revision.go:71`;
  `internal/write/bullet_request.go:15-31`). Both runtimes' ids fit: a 36-character uuid and a
  30-character `ses_…` id.
- It appears in three places only: the bullet trailer ` [cairn: <actor>/<session>]`
  (`revision.go:44`), stamped only by `AppendBullet` (`internal/write/write.go:108`); arc members
  `Member{Session, Role, FirstSeen, Carried}` (`internal/arcs/arcs.go:74-85`); and in-memory
  presence rows (`internal/presence/presence.go:83-93`).
- **The session is SELF-DECLARED** by the writer: `cairn append` requires `--session` with no
  default (`internal/client/cli.go:120, 139-143`); nothing authenticates it.
- **`put`/`create` stamp no trailer** (`internal/touch/touch.go:20-26`), so a session that wrote
  only through them is invisible to `touch`.
- **Nothing records a session's host, runtime, owner, branch or start time.** No code knows about
  sidechains or subagents (grep over `*.go`).

### Where a session wrote — `internal/touch` and `report.SessionAcross`

- `touch.Writes(storeRoot, ix, scope)` derives write edges LIVE from stored bytes, no index
  (`internal/touch/touch.go:124-168`); records `WriteEdge{Scope, EntryRef, CitationID, Date, Actor,
  Session}` (`:40-52`).
- `report.SessionAcross(root, session, visible, snap)` calls it for every scope in `visible` and
  keeps the ones naming the session (`internal/report/sessionacross.go:73-97`).
- 🔴 **The session page passes the VIEWER's readable set as `visible`**
  (`internal/ui/sessionpage.go:53, 63`). That is right for "which of its writes may I see", and
  **exactly wrong for "which scopes did it write to"**: over a viewer's readable set, every scope
  found is readable by construction, so an every-scope check computed that way is vacuously true.
  Decision 3 computes the written set over the WHOLE store; clause (e) is the control.

### The session page and its visibility rule today

- `GET /session` is `classContent` (`internal/ui/routes.go:133-138`), handled by
  `handleSessionPage` (`internal/ui/sessionpage.go:83-119`): visible scopes → empty id renders the
  navigation page → grammar bound 404 → `Session` → not found 404 / 503 → presence decorates.
- **Visibility today is a UNION**: the page is found when the session wrote in ANY scope the viewer
  reads, or is a member of an arc homed in one (`sessionpage.go:23-41`;
  `internal/report/sessionacross.go:109`). There is no owner concept.
- **No existence oracle:** unseen, never-written and ungrammatical ids answer the same 404 bytes
  (`sessionpage.go:72-73, 97-113`).
- 🔴 **O2's rule is STRICTER (an intersection)**, so the transcript section is a SECOND predicate
  inside a page the first one found: a viewer can see the page and not the transcript, and that
  must be byte-identical to a session that has no transcript (decision 4).

### Authority — `internal/control`

- `control.Resolve(m Model, p Principal) Authorization` (`internal/control/resolve.go:187`) returns
  per-scope verb sets; `Allows(scope, verb)` is the predicate (`:89`), `NamedScopes(verb)` (`:157`)
  maps ids to names. Verbs are a closed `{read, write, admin}` (`model.go:50-67`).
- Authority arrives two ways — project membership via `roleVerbs` and grants
  (`resolve.go:199-231`) — so **a `Model.Grants` listing under-reports every member**
  (`AGENTS.md`; `internal/ui/sharing.go:15-24`; guard `TestTheAudienceIsComputedFromResolveNotFromGrantRows`,
  `internal/ui/sharing_test.go:341`).
- Principal kinds are only `user` and `project` (`model.go:150-161`); the stable key is
  `(Kind, ID)` — `Display` is mutable (`resolve.go:17-26`). There is no host or agent principal.
- `Authorization.Narrowed()` (`resolve.go:78`) is true for a narrowed bearer credential.
- 🔴 **Journal credentials have NO revocation path**: `EventCredentialRevoked` has no writer
  (`internal/control/credential_issue.go:94-96`). Any new machine credential this plan adds must
  therefore NOT be a journal credential (decision 2).

### The precedent: presence's per-host tokens and second listener

- `internal/presence` (`cmd/cairn-ui`-only, `presence.go:23-30`) keeps rows in memory, keyed by a
  `(Kind, ID)` owner and a host label (`presence.go:48-59, 122-160`).
- **Tokens:** a file of rows `<push|claim> <kind>:<id> <host> <sha256-hex>`, digests only
  (`tokens.go:30-48, 60-86`), minted by `cairn-ui -issue-presence-token` and printed once
  (`cmd/cairn-ui/presence.go:161-199`), **re-read on every agent request so deleting a row
  revokes** (`agent.go:125-157`; unreadable file fails closed, `:129-134`), single-owner wall
  (`tokens.go:134-161`).
- **A second listener** (`-presence-agent-addr`, `cmd/cairn-ui/main.go:270-282`) serves exactly
  two routes from its own ledger `AgentRoutes()` (`internal/presence/agent.go:20-50`), outside the
  browser's same-origin and CSRF gates by design, with a failed-token lockout (`agent.go:189-203`).
- **The wire is strict:** exact key sets, `DisallowUnknownFields`, bounded counts and string sizes,
  control characters refused (`internal/presence/wire.go:20-47, 88-180`); transcript text is on its
  never-carried list (`claudedocs/plan-cairn-arcs-presence.md`, decision 8).

### Journals, and where new durable state may live

- **The arc journal** is the model for a durable append-only file outside the store tree: one JSON
  per line, `flock` + one `O_APPEND|O_NOFOLLOW` write + `fsync`, 0600, torn-tail sealing
  (`internal/arcs/journal.go:17-24, 148, 156-226`); its path has no default and is REFUSED inside
  the store root (`internal/arcs/path.go:12-16, 30-90`). No size cap, no retention.
- **`internal/pgstore`** holds only `cairn-ui`'s sessions and invites (`internal/pgstore/migrate.go:38-88`)
  and is OPTIONAL: `-db-dsn` unset means file sessions and no invites
  (`cmd/cairn-ui/main.go:283-287, 446-467`). An unknown schema version refuses
  (`migrate.go:166-173`), so a migration makes a UI rollback an outage.

### The pod, the UI and the gates a change passes through

- **Two binaries.** `cmd/cairn-server` (the pod, `internal/api`) and `cmd/cairn-ui` (browser,
  presence) are separate processes (`cmd/cairn-ui/main.go:26-30`). Everything in this plan lives in
  `cairn-ui` and new binaries: **the pod does not move**, so `api.DeclaredRoutes()`
  (`internal/api/routes.go:93`), `tests/conformance/requests.json`, `tests/dualrun/` and the parity
  gate are untouched. That is a claim each slice re-asserts (no diff under `internal/api`,
  `cmd/cairn-server`, `cmd/cairn`, `internal/client`).
- **UI routes** are an exact-match table (`internal/ui/routes.go:117-235`); a row moves the hand
  ledger `TestTheRouteLedgerMatchesTheDispatchTable` (`routes_test.go:59`), `contentAuthority`
  (`:800-822`), `TestEveryServedPathComesFromTheLedger` (`:537`) and uiaudit's derived target maps
  (`uiaudit/targets.go:180, 228, 275, 334`).
- **Two cross-site gates by METHOD** (`internal/ui/server.go:1244-1310`, `stateChanging` `:1323`):
  same-origin before auth, CSRF after. A non-browser worker cannot pass them and must not be made
  to — the presence precedent's reason for a second listener.
- **XSS:** every rendered value is a `g.Text` or attribute; `Raw`/`Rawf` are AST-banned
  (`AGENTS.md`; `internal/ui/README.md` "The raw-node ban"). LLM output is attacker-influenced
  text and is rendered the same way — never as Markdown-to-HTML.
- **No Content-Security-Policy** by operator decision (`internal/ui/server.go:1673`).

### Dependencies — `internal/depspolicy`

- `DeclaredModules` is exactly `github.com/lib/pq` and `maragu.dev/gomponents`
  (`internal/depspolicy/depspolicy.go:204-231`), failing on GROW or SHRINK
  (`depspolicy_test.go:42`).
- **The import ban** covers `LinkedBinaryRoots` = `cmd/cairn`, `cmd/cairn-server`
  (`depspolicy.go:443-446`); `cmd/cairn-ui` is NOT banned (it is the positive control, `:450`).
- Nested modules (`uiaudit`) are a separate allowlist (`DeclaredNestedModules`, `:256-262`).
- 🔴 **Reading opencode's SQLite in Go needs a third-party driver** — the stdlib has none. Decision 5
  avoids it by reading `opencode export`'s JSON (R3), which keeps the capture binary stdlib-only and
  lets it join the ban.

### Governed ledgers a new package, row or binary moves

| ledger | where | moves when |
|---|---|---|
| `go` job per-package `ok` floor | `.github/workflows/ci.yml:836` (`-lt 24`) | every new tested package; set to the count MEASURED on the merged tree |
| control mutant battery, **296** rows | `tests/control_mutants.py` `PKGS` (`:88-130`), pinned by `tests/test_control_mutant_count_is_pinned.py` into `ci.yml:865`, `:848-851` and `internal/control/README.md:261, 265, 417` | a new authz package joins `PKGS`; every new row |
| UI route ledgers | `routes.go`, `routes_test.go:59, 537, 800-822`, `uiaudit/targets.go` | every new browser row (S4, S5, S6) |
| depspolicy | `LinkedBinaryRoots` `depspolicy.go:443-446`; `DeclaredNestedModules` `:256-262` | `cmd/cairn-capture` joins the ban (S2); the plugins nested module (S7) |
| `onlyGo` | `flake.nix:351-467` | any embedded non-Go file or named Go-test fixture |
| `goVendorHash` | `flake.nix:505` | only if a module is added — none is planned |
| flake packages and checks | `flake.nix` | `packages.cairn-capture` (S2) with a `-verbs`-style ledger check |
| e2e PASS floors | `ci.yml:1015-1025` (arcs), `:1045-1055` (presence) | `tests/plugins/e2e.sh` adds its own |
| `AGENTS.md` weight | `tests/test_agent_instructions_weight.py:133, 154` | **25 bytes of headroom today** (31,308 + 267 against a 31,600 working budget) |

## Decisions — who chose what

### Chosen by the OPERATOR (paraphrased; not re-litigated)

| # | the operator's choice | cost accepted / where it lands |
|---|---|---|
| O1 | Ship the FULL RAW transcript, tool outputs included, for BOTH Claude Code and opencode — both are mandatory. Redaction still happens on the host before upload; the operator preferred full content over a redacted conversation-only subset. | Every byte class in R1/R3 is in scope, including whole-file copies and harness metadata. Redaction is a hard requirement; its residual is threat T1, stated rather than argued away. |
| O2 | A session's content is visible to its OWNER, and to principals who can read EVERY scope the session wrote to — computed through `control.Resolve`, never `Model.Grants`. A session that touched a scope you cannot read is hidden from you. Summaries and other plugin output derived from it follow the same rule. | Stricter than today's session page (a union); decision 4. |
| O3 | The session page SHOWS the content when you view a session. | A transcript renderer in `internal/ui` (S4). |
| O4 | Plugins are OUT-OF-PROCESS workers — on a host or in their own pod — calling a capability-scoped plugin API with plugin tokens. The pod (here: `cairn-ui`) holds an in-tree registry of manifests, toggles and outputs, and renders. The serving path stays stdlib-only; plugin credentials (LLM keys, ClickUp tokens) never enter it. | The plugin API is a new listener (decision 8); example plugins are separate binaries (S7, S8). |
| O5 | Example plugin A: continuous session summaries, toggleable per scope and per arc. | Toggle hierarchy (decision 10). |
| O6 | Example plugin B: match ClickUp tickets to sessions — (a) deterministically from explicit ids/URLs in branch names, commit messages and trailers, PR bodies and transcripts; (b) LLM semantic suggestions, shown AS suggestions with a confidence. No write-back to ClickUp. | Write-back is future work (B-list). |
| O7 | An MCP server is on hold; plugins are not to be designed as MCP servers. | The plugin API is plain HTTP+JSON. |

### Chosen by the AGENT writing this plan (open to review)

1. **`cairn-ui` holds transcripts, the registry, toggles and outputs; the pod does not.** Only
   `cairn-ui` over a control journal resolves a GitHub session and a bearer credential to one
   `(Kind, ID)` (presence plan, decision 1), the session page lives there, and keeping the pod
   untouched keeps the conformance, dualrun and parity gates — and the Python oracle — out of this
   work entirely. ⚠ Assumes ONE `cairn-ui` replica, as presence does (Q12).

2. **Machine credentials are WORKER TOKENS in their own file, never journal credentials.** Journal
   credentials cannot be revoked today (STEP 2); the presence token file can (delete the row, re-read
   per request). Two new kinds, same file format family, `-worker-tokens <file>` with no default:
   - **`capture <kind>:<id> <host> <digest>`** — bound to ONE owner and ONE host label. May only
     append to transcripts it owns, from its host. Cannot read anything.
   - **`plugin <name> <digest>`** — bound to ONE registered plugin name. Its capabilities are the
     plugin's manifest's, as admitted by the registry (decision 9). Cannot sign in, cannot read the
     store, cannot read a transcript unless the plugin is ON for that session.
   Neither is known to `identity.Backends`, so every browser row and the pod answer them as they
   answer garbage (the presence precedent, and the same byte-compare test).

3. **"The scopes a session wrote to" is computed over the WHOLE STORE, unioned with a declared set,
   and only ever GROWS.**
   - `W_trailer(s)` = every scope in which `touch.Writes` finds an edge naming `s` — walked over
     every scope in the store, NOT the viewer's readable set (STEP 2's trap; clause (e)).
   - `W_declared(s)` = scope names the capture agent declares at upload (decision 5: parsed from
     the session's own `cairn append|put|create --scope` tool calls — a heuristic, labelled as one),
     stored with the transcript, never shrunk by a later upload.
   - `W(s) = W_trailer ∪ W_declared`, over the session and every child stream (subagents, opencode
     child sessions — decision 7).
   - **Every error in this set is in the safe direction except one.** A forged trailer naming `s`
     ADDS a scope, which can only hide `s` from more people. The unsafe direction is an
     UNDER-count: a write `touch` cannot see (`put`/`create`, a deduped re-POST, a write to the
     OTHER instance) that the declaration heuristic also missed. That residual is threat T3.
   - A scope NAME in `W(s)` that the control model does not know (renamed, deleted, or on another
     instance) is treated as **unreadable by everyone but the owner**.

4. **ONE predicate: `transcript.Visible(viewer, meta, W) bool`**, asked by every surface — the
   transcript section, the transcript fetch route, plugin output rendering, edge rendering, and any
   future list. True iff the viewer is un-narrowed AND (viewer's `(Kind, ID)` == the owner's, OR
   (`W` is non-empty AND `control.Resolve`'s authorization `Allows(scope, read)` for EVERY scope in
   `W`)). **An empty `W` is owner-only** (clause (f)); a narrowed credential sees nothing (the
   presence ruling, `Authorization.Narrowed()`). Every miss renders the SAME bytes as "this session
   has no transcript", and the section never says which scope hid it.

5. **The capture agent is a NEW stdlib-only binary, `cmd/cairn-capture`, added to the import ban's
   `LinkedBinaryRoots`** — not a verb of `cmd/cairn`.
   - *Why not a `cairn` verb:* `cmd/cairn` is `packages.default`, installed wherever cairn is read;
     a verb that reads every transcript on the machine widens what that binary is for, and its
     verb ledger is a parity-gated contract (`AGENTS.md`). A separate binary is least privilege and
     moves no parity row.
   - *Readers:* Claude Code JSONL by byte offset, complete lines only (a partial last line waits for
     the next run); subagent files and their `agent-<id>.json` as child streams; persisted
     `tool-results/*.txt` files referenced by a record (text only — Q2). opencode through
     `opencode export <id>` as a subprocess by bare name (the `gitMinimal`-on-`PATH` precedent for
     `git`), diffing parts by `(id, time_updated)` against local state; it never opens the SQLite
     file, so the `account`/`credential` tables are structurally out of reach.
   - *State:* a local 0600 watermark file per instance — `{stream → offset | part-version set}` —
     advanced only after the pod acknowledges.
   - *Cadence:* a user timer (`OnStartupSec` + `OnUnitActiveSec = 60s` + `AccuracySec = 1s`, the
     presence plan's measured lesson that `OnUnitActiveSec` alone never fires). Idle sessions cost
     one `stat` per file.
   - *Modes:* `--dry-run` prints per-session record and redaction COUNTS, never content;
     `--self-test` runs the redaction corpus (clause 3 of the closing condition); `-verbs` prints
     its own ledger for a nix check.

6. **Redaction: ONE rule table, applied on the host, RE-CHECKED on the pod, measured on a realistic
   synthetic corpus.**
   - *Where it runs:* on decoded JSON STRING VALUES, recursively, then re-encoded — never on raw
     bytes (an escaped secret is different bytes; R4). A match is replaced by
     `[redacted:<rule>:<8-hex digest prefix>]`, so two occurrences of one secret stay recognisably
     the same without being recoverable.
   - *The rules:* `internal/redact`, stdlib `regexp` (RE2), seeded from leakscan's `credential`
     alternatives and extended for R4's shapes: provider API-key prefixes, JWTs, URL/DSN userinfo
     passwords, dotenv `KEY=value` where KEY names a secret, PEM blocks, and STRUCTURAL rules (in a
     document whose `kind` is `Secret`, every `data`/`stringData` value). Plus a **per-host
     denylist** of literal strings and file-path globs in a local 0600 file — never in the repo.
   - *One rule, one place (recommendation, Q3):* move the credential alternatives into one data file
     both `tests/leakscan.py` and `internal/redact` load, with a test that every leakscan control
     still behaves and that the redact table ⊇ leakscan's. Python `re` and Go RE2 differ; the shared
     subset must compile identically in both, asserted.
   - *The pod re-checks* every received record against the same table and refuses (422, nothing
     stored) a match — clause (c). ⚠ It catches a bypassed or outdated agent, NOT a miss: the same
     table cannot catch what it does not recognise.
   - *Measurement (closing condition part 3):* a generator emits synthetic transcripts in BOTH
     formats with **P** secrets planted in realistic positions — a shell tool's stdout of a dotenv
     file, `toolUseResult.originalFile`, an edit's `oldString`, a JSON-escaped value, a Kubernetes
     `Secret` manifest, an opencode tool `state.output`, a DSN in a command line, a prompt string.
     🔴 **The planted values are GENERATED AT RUN TIME from a seeded RNG**, never committed: a
     committed credential-shaped fixture is itself a `leakscan` finding (and should be). The report
     is the pair "planted=P caught=P" plus `clean-damaged=0` on a clean corpus of UUIDs, commit
     SHAs, digests and base64 payloads (a redactor that eats every hash makes transcripts
     unreadable; that is a failure too). A textbook example key is NOT in the corpus.

7. **Sidechains, subagents and child sessions are CHILD STREAMS of one root session.**
   - Claude Code: a subagent file shares the parent's `sessionId` (R2), so it is stream
     `subagent:<agentId>` of that session, carrying its metadata file's keys.
   - opencode: a child session has its own id and a `parent_id` (R3); it is uploaded as stream
     `child:<ses_id>` of its ROOT, and its own id is recorded so trailers naming the child count
     toward the root's `W`.
   - **Visibility is per ROOT**: `W(root)` is the union over every stream, and the whole tree is
     visible or none of it — a child is never visible where its root is not.

8. **The capture and plugin APIs live on ONE NEW listener, `-worker-addr`, with its own route
   ledger** — not on the presence listener (which keeps its single-owner wall and its two routes
   unchanged; nothing presence-shaped migrates). It applies `cairn-ui`'s reachable-bind /
   trusted-proxy refusal to its own bind and the `netid` failed-token lockout, exactly as the
   presence listener does (`cmd/cairn-ui/main.go:537-563`; `internal/presence/agent.go:189-203`).

9. **One plugin contract, used by BOTH examples** (so the first two plugins test the abstraction):
   - **Manifest** (committed beside each plugin; registered on the pod by the operator through
     `-plugin-registry <file>`, no default): `schema`, `name`, `version`, `description`,
     `capabilities` (a closed in-tree vocabulary: `work:lease`, `transcript:read`,
     `output:read:<type>`, `output:write:<type>`), `toggle_key` (the plugin name), `output_types`
     (each a member of a CLOSED in-tree set: `summary`, `ticket-edge`, `ticket-suggestion`),
     `calls_model` (bool), `budget` (unit and cap, decision 12).
   - **Output envelope**, pod-validated: `{plugin, plugin_version, model, session, stream_set,
     input_watermark: {stream → seq}, type, body, usage}`; the pod stamps `produced_at` and the
     token's plugin name (a plugin cannot claim to be another). Bodies are validated against the
     in-tree schema for their type with `DisallowUnknownFields` and size bounds.
   - **Why the output TYPES are in-tree and closed:** the renderer must know each type to render it
     safely; a plugin that needs a new type needs a reviewed cairn change, while a new plugin
     producing existing types needs only a registry entry.
   - A capability not in the manifest, or an output type not in the plugin's `output_types`, is
     refused 403 with one uniform body — clause (h).

10. **Toggles default OFF and inherit instance → scope → arc → session; every change is journaled.**
    - **Effective value for (plugin, session):** the session's explicit value if set; else, if any
      arc listing the session has an explicit value, the AND of those (any OFF wins); else the AND,
      over every scope in `W(root)`, of that scope's explicit value or the instance value; an empty
      `W` resolves OFF. "Every scope" mirrors O2: an LLM plugin reads a session only where every
      scope it touched consented.
    - **Who may set:** instance — the principals named by a no-default `-plugin-admin` flag; scope —
      `admin` on that scope via `control.Resolve`; arc — `admin` on the arc's HOME scope; session —
      its owner. Every set is a CSRF-gated browser POST (both gates by method).
    - **Journal:** `-plugin-journal <file>`, the arc journal's write discipline (flock, one
      `O_APPEND` write, fsync, refused inside the store root), records
      `{plugin, level, target, value, by: (Kind, ID), at: pod clock}`. The fold is last-write-wins
      per `(plugin, level, target)`; history is the journal.

11. **Plugin output is a CLAIM and is rendered as one.** Every output renders inside a labelled
    "derived" container stating plugin name and version, model, `produced_at`, and the watermark
    ("from turns 1–N of M; M−N newer turns not yet summarised" when the transcript has grown). Text
    is `g.Text` only — no Markdown, no links built from model output except a ClickUp URL built by
    the existing `refurl` template from a VALIDATED id. A suggestion renders with its confidence and
    the word "suggested"; an edge renders its signal ("branch name", "PR body", …).

12. **Budgets are DECLARED on the pod, ENFORCED cooperatively, and capped at the provider.** The
    registry carries a per-plugin cap per rolling day in the plugin's declared unit; each output
    reports `usage`; the pod stops leasing work once the window's sum reaches the cap (clause (k)).
    ⚠ The pod never sees the LLM key, so it cannot stop a plugin that lies or ignores the lease —
    the real ceiling is the plugin's own cap plus a spending limit set at the provider. Stated in
    the manifest docs and threat T7.

13. **Continuous = incremental, on a lease.** `POST /plugin/v1/work` returns up to N sessions where
    the plugin is ON and the transcript's newest `seq` exceeds the plugin's last output watermark
    for that session, AND the session has been idle ≥ 10 minutes OR grown by ≥ 200 records since
    that watermark (defaults, registry-configurable). Each item carries the previous output's id
    and watermark so the plugin summarises only the new records plus its own prior summary. A lease
    expires after 15 minutes; an unacked lease returns to the pool. There is no "session ended"
    signal in either runtime's storage (R1–R3), so idleness is the trigger.

14. **ClickUp matches are EDGES with PROVENANCE; suggestions are a different type.**
    - `ticket-edge {system: "clickup", ticket_id, ticket_url, signal, locator}` where `signal` ∈
      `{branch-name, commit-message, commit-trailer, pr-body, pr-link, transcript-url, entry-ref}`
      and `locator` names WHERE (`{stream, seq}` for a transcript record; a commit SHA; a PR number;
      an entry ref). Deterministic signals only: a ClickUp task URL in either form (R5), an
      explicit `clickup:<id>` ref (`internal/store/entry.go:486, 570`) on an entry the session
      wrote, or a CUSTOM id that is a member of the workspace's own closed list fetched by the
      plugin. A bare token that merely LOOKS like an id is never matched.
    - `ticket-suggestion {ticket_id, ticket_url, confidence: 0.0–1.0, rationale (≤ 280 chars)}` —
      LLM-produced, rendered as a suggestion, never promoted to an edge by the plugin.
    - **What ClickUp material is stored:** the ticket id and URL only by default; the ticket TITLE
      only when the instance sets a no-default `-plugin-store-ticket-titles` (Q8). Descriptions,
      comments and assignees are never stored. Edges and suggestions are visible exactly when their
      session is (decision 4).

15. **Storage: a per-session DIRECTORY under `-transcript-dir`**, no default, refused inside the
    store root (the `internal/arcs/path.go:30-90` rule), 0700/0600.
    - `<dir>/<root-session>/meta.json` (owner, host, runtime, `W_declared`, child ids, created);
      `stream-<name>.jsonl.gz` segments, append-only, each record `{seq, src, rec}` where `src` is
      the source offset or part version and `rec` the redacted raw record; `outputs.jsonl` for
      plugin outputs. The server assigns `seq` per stream, so ONE watermark shape serves both runtimes.
    - **Deletion is `rm -r` of one directory** (plus the journaled fact of the deletion, no
      content) — the reason it is not one shared append-only journal, where deleting one session
      means rewriting everyone's.
    - **Caps:** per-record (1 MiB after redaction: measured tool output max 656 KB, opencode part max
      2.8 MB — an over-cap record is TRUNCATED with an explicit `[truncated N bytes]` marker and
      counted, never silently dropped), per-session and per-instance totals (refusing 413 with the
      cap named), and **retention** (`-transcript-retention`, REQUIRED when the feature is armed —
      no default, so nobody gets "keep forever" by omission).
    - *Why not pgstore:* `-db-dsn` is optional on `cairn-ui`, the volume is GB/week (R1), and a
      schema migration turns a UI rollback into an outage (`migrate.go:166-173`). Q12 keeps the
      option open.

16. **Where a transcript is sent: the instance that routes EVERY scope in its declared `W`.** The
    capture agent resolves instances with `internal/client`'s routing (stdlib, already Go). A
    session whose declared scopes route to two instances is held locally and NOT shipped (logged);
    a session with no declared write goes to the default instance only, where decision 4 makes it
    owner-only. Q3 asks the operator to confirm.

## The contracts

```
# capture — worker listener, capture token (owner, host)
POST {worker}/capture/v1/sessions/{root}/streams/{stream}/records
  body: {"schema":1, "runtime":"claude|opencode", "host":"host-a",
         "from":"<offset or version cursor>", "to":"<…>",
         "declared_scopes":["alpha-notes"],          # grows only
         "records":[{"src":"<offset|part-id@ms>", "rec":{…redacted raw record…}}]}
  200 {"stored_to":"<…>", "seq_to":N}
  409 {"stored_to":"<…>"}       # `from` ≠ stored position — the agent resumes from there
  413 cap reached (named)       422 a record matched the rule table (index named, value not)
  400 malformed / host ≠ token's host / unknown field     401 uniform

# plugin — worker listener, plugin token (name)
POST {worker}/plugin/v1/work            {"max":10}
  200 {"lease":"l-…", "items":[{"session":"s-0001", "streams":{"main":{"seq_to":N}},
        "prev_output":{"id":"o-…","watermark":{"main":M}}|null}]}
GET  {worker}/plugin/v1/transcript?session=s-0001&stream=main&from_seq=M&limit=500
  200 {"records":[{"seq":…, "rec":{…}}], "seq_to":…}    # only when ON for this session
GET  {worker}/plugin/v1/outputs?session=s-0001&type=summary   # needs output:read:summary
POST {worker}/plugin/v1/outputs          {envelope, decision 9}  → 201 {"id":"o-…"}
POST {worker}/plugin/v1/work/{lease}/ack
  403 uniform for every capability, toggle or type refusal     401 uniform
```

## Threat model

| threat | control |
|---|---|
| **T1. A secret survives redaction and is stored** | The residual the operator accepted by choosing full content (O1). Controls: host-side redaction on decoded strings (decision 6), pod re-check (clause c), the realistic corpus (closing condition 3), per-host denylist, retention, per-session deletion. **What is NOT controlled:** unshaped secrets (typed passwords, novel token formats), secrets inside images/PDFs (Q2 recommends not shipping binaries), and anything leaked BEFORE a rule existed — a rule added later does not rewrite stored records (B3 proposes a re-scan). |
| **T2. Confidential but non-secret content** (client business detail, personal data in a tool output) | Redaction does not address it at all; VISIBILITY is the only control (decision 4). Stated, so nobody believes the redactor covers it. |
| **T3. Under-counted `W` widens visibility** | `W` is computed over the whole store plus the declared set and only grows (decision 3); empty `W` is owner-only (clause f); unknown scope names are unreadable; a session writing to two instances is not shipped (decision 16). The residual: a write invisible to BOTH `touch` and the declaration heuristic (e.g. a `put` issued by a script the session ran). |
| **T4. Viewer-set computation makes the predicate vacuous** | Clause (e), and a mutant row (`transcript-written-set-from-viewer-scopes`). |
| **T5. A stolen capture token** | Can APPEND to its owner's transcripts from its one host — inject fake records into the owner's own sessions (visible to whoever may see those sessions). Cannot read, delete, or touch another owner's or host's sessions. Revoke by deleting the row (re-read per request). |
| **T6. A stolen plugin token** | Can read every transcript where that plugin is ON, and write outputs of its declared types. That is the largest single exposure the design creates; it is why toggles default OFF, require every written scope's consent (decision 10), and why a plugin token is per plugin. |
| **T7. The LLM plugin sends transcripts to a third party** | By design: plugin A and B's LLM half send redacted raw records to a model provider. That egress is outside cairn's control once leased; toggles are the consent, the instance flag (Q5) the gate for the client instance, and the provider's own data terms the residual. Budget enforcement is cooperative (decision 12). |
| **T8. Prompt injection through transcript content** | A transcript can contain text that instructs the summariser. Its blast radius is the plugin's OUTPUT: a misleading summary or a false suggestion. Controls: outputs are labelled derived (decision 11), suggestions never become edges, rendering is `g.Text` only, plugins have no write path except outputs. |
| **T9. XSS through raw records or model output** | Every rendered value through `g.Text`; the raw-node ban holds; a URL only through `safeHref` and the `refurl` template. Fixture: a record whose strings are hostile HTML, asserted escaped (the `TestHostileEntryTextIsEscaped` pattern). |
| **T10. Existence oracle** | The transcript section, the fetch route and output renders answer "no transcript visible to you" with identical bytes for absent, hidden and deleted; a plugin gets one 403 body for every refusal. |
| **T11. Cross-instance leakage** | Decision 16 on the host; per-instance worker tokens; the client instance arms nothing until Q5. |
| **T12. Storage exhaustion** | Per-record, per-session and per-instance caps, required retention (decision 15), 413 named. |
| **T13. Committed fixtures leak** | Synthetic generator only; planted secrets generated at run time (decision 6); `leakscan` in CI; no captured text, ever (`AGENTS.md`). |
| **T14. Toggle tampering** | Toggles are browser POSTs behind both gates, authorised per level (decision 10), journaled with `(Kind, ID)` and pod time. |

## Slices

Each slice is mergeable alone, leaves `main` releasable, and has tests that run on that slice
alone. None touches `internal/api`, `cmd/cairn-server`, `cmd/cairn`, `internal/client` (except a
read-only import of its routing in S2) or the Python oracle — each slice's test plan re-asserts it.

| slice | what | ledgers it moves | mergeable alone because |
|---|---|---|---|
| **S0** | **Fixtures and shape ledgers.** `tests/transcripts/gen.py` emits synthetic sessions in BOTH formats from the measured key sets (R1–R3): main stream, two subagents, an opencode child, a compaction boundary, a `pr-link`, persisted tool output, mutated opencode parts. A shape test pins the generator's record-type set against the readers' declared set. | `tests/`; `onlyGo` if Go tests read the fixtures; README. | Test-only. |
| **S1** | **`internal/redact`** — the rule table (decision 6), decoded-string traversal, digest-tagged replacement, structural Secret rule, denylist loader; the corpus generator and the `planted/caught/clean-damaged` report. Recommended: the shared rule file with `leakscan` (Q3). | new package; `ok` floor; maybe `tests/leakscan.py` + its self-test; README. | Library only. |
| **S2** | **`cmd/cairn-capture`** — readers (JSONL by offset, subagents, persisted text; `opencode export` diffing), watermark state, routing (decision 16), `--dry-run`, `--self-test`, `-verbs`. No upload yet: `--dry-run` is the only output. | `cmd/cairn-capture`; `depspolicy.LinkedBinaryRoots` + its test; `flake.nix` `packages.cairn-capture` + a ledger check; `ok` floor; closing-condition part 3 step in `ci.yml`. | Inert: it sends nothing. |
| **S3** | **Transcript store + capture API.** `internal/transcript` (directory layout, CAS append, caps, retention sweeper, deletion), worker listener + ledger, `capture` token kind and `cairn-ui -issue-worker-token capture`, pod re-check. Agent gains upload. `tests/plugins/e2e.sh` created with clauses (a)–(c). | new package → `ok` floor, `control_mutants.py` `PKGS` (+ pinned count through `ci.yml` and `internal/control/README.md`); `cmd/cairn-ui` flags (`-worker-addr`, `-worker-tokens`, `-transcript-dir`, `-transcript-retention`, caps) and tests; `ci.yml` e2e step. | Inert unless `-worker-addr` AND `-transcript-dir` are set. |
| **S4** | **Visibility + the session page shows content (O3).** `transcript.Visible` (decision 4) with `W` (decision 3); a transcript section on `/session` (turn list, tool calls collapsed by `<details>`, child streams nested, compaction marked as runtime-generated, paginated by `?tseq=` — no script); `GET /session/transcript` raw-record view for one stream window. e2e clauses (d)–(f). | UI rows → hand ledger, `contentAuthority`, uiaudit targets + a synthetic transcript in the uiaudit world; mutant rows. | Read-only over S3; renders nothing when no transcript exists. |
| **S5** | **Plugin registry, toggles, plugin API.** `internal/plugins` (manifest validation, closed capability and output-type vocabularies, plugin token kind, toggle fold + `-plugin-journal`, leases, budget window); `/plugins` admin page + toggle POSTs on scope, arc and session pages. e2e clauses (g), (h), (k). | new package → `ok` floor, `PKGS`; UI rows; `cmd/cairn-ui` flags (`-plugin-registry`, `-plugin-journal`, `-plugin-admin`); mutant rows; README. | Inert with no registry; every toggle OFF by construction. |
| **S6** | **Outputs: storage, rendering, deletion cascade.** Outputs stored per session; rendered labelled-derived (decision 11) on the session page and as a one-line summary on session rows; owner "delete transcript" POST cascading to outputs and edges. e2e clauses (i), (j). | UI rows; mutant rows; README. | Nothing renders until a plugin writes. |
| **S7** | **Example plugin A — summaries.** `plugins/summary` in a NESTED stdlib-only module (provider HTTP API over `net/http`, no SDK), host-side user timer, incremental per decision 13, its own budget cap, a fake provider in tests. | `depspolicy.DeclaredNestedModules`; `flake.nix` package; `ci.yml` step for its suite. | A separate binary; nothing runs until registered and toggled. |
| **S8** | **Example plugin B — ClickUp.** `plugins/clickup` in the same nested module: ticket list fetch (read-only token, 429-aware), deterministic matchers (decision 14) over transcript records (`gitBranch`, `pr-link`, URLs), commit messages and trailers from a host-local repo list, PR bodies via the host's own GitHub CLI, entry refs; LLM suggestions using plugin A's summaries when present (`output:read:summary` — the abstraction's cross-plugin test). Synthetic ClickUp fixtures only. | same nested module; flake package; `ci.yml`. | Separate binary; inert until registered and toggled. |
| **S9** | **Closing wiring.** `--self-test` reaches `sabotaged=11 caught=11`; floors set to measured counts; `AGENTS.md` layout row with an equal eviction (Q11). | `ci.yml`; `AGENTS.md`; READMEs. | Docs/CI only. |

**Mutant rows** (indicative names; the pinned count starts at **296**). S0, S1, S2, S7 and S8 add no
row to the authz battery (S1's and S2's guards are measured by the redaction corpus; S7/S8 by their
own suites).

- **S3:** `transcript-cas-ignores-from-offset`, `transcript-capture-token-host-unchecked`,
  `transcript-capture-token-reads`, `transcript-pod-recheck-skipped`, `transcript-cap-ignored`,
  `worker-token-accepted-by-browser-row`
- **S4:** `transcript-written-set-from-viewer-scopes`, `transcript-empty-set-visible`,
  `transcript-owner-by-display-not-id`, `transcript-narrowed-viewer-sees`,
  `transcript-any-scope-not-every-scope`, `transcript-child-visible-without-root`,
  `transcript-unknown-scope-name-readable`
- **S5:** `plugins-toggle-default-on`, `plugins-scope-toggle-any-not-every`,
  `plugins-transcript-read-ignores-toggle`, `plugins-output-type-unchecked`,
  `plugins-capability-unchecked`, `plugins-budget-ignored`, `plugins-toggle-unauthorised-level`,
  `plugins-plugin-name-from-body-not-token`
- **S6:** `plugins-output-rendered-without-predicate`, `plugins-delete-keeps-outputs`

### Test plan per slice (negative controls named)

**S0.** The generator's output parses with 0 errors in both readers' unit tests; the shape test
fails when the generator emits a record type the readers do not declare (control: add one) and when
the readers declare one the generator never emits (control: delete one). `leakscan` stays clean
over the fixtures (they contain no credential shapes — S1 plants those at run time).

**S1.**
- `planted=P caught=P` over the corpus; **negative control:** with the dotenv rule removed, caught
  < P and the report names the rule; **positive control for the structural rule:** a `Secret`
  manifest's `data` values are replaced while its `metadata.name` survives.
- `clean-damaged=0` over UUIDs, 40-hex SHAs, sha256 digests, base64 image payloads; control: a
  deliberately greedy rule (`[A-Za-z0-9]{32,}`) makes `clean-damaged` > 0, proving the counter
  can move.
- JSON-escaped secret: a value with `-` in the token is caught after decoding; control:
  scanning the raw bytes instead misses it (the reason decision 6 decodes first).
- Replacement is deterministic and digest-tagged: two occurrences of one secret get one tag; two
  secrets get two.
- If Q3 lands as recommended: every `leakscan` control behaves identically after the move, and the
  redact table ⊇ leakscan's credential set (fails on SHRINK).

**S2.**
- Offset reader: a file grown mid-line ships only complete lines; the next run ships the rest
  exactly once. Control: a reader that ships the partial line produces a record that fails to
  parse (asserted red).
- A file whose first 4 KiB changed (a rewrite, not an append) is NOT resumed: it is refused
  locally and logged — the agent never uploads from an offset into different bytes.
- opencode: from two recorded synthetic exports where 3 parts changed `time_updated` and 1 was
  added, exactly 4 upserts are produced; control: diffing by id alone produces 1.
- **The SQLite file is never opened:** a test runs the reader with `HOME` pointing at a tree where
  the database path is a FIFO; the reader completes (it only calls `opencode export`, here a stub).
- Routing: declared scopes on two instances → held, logged, nothing queued.
- `--dry-run` output contains no string value of any record (asserted by planting a sentinel in
  every string field and grepping the output for it: 0; positive control: the sentinel IS in the
  fixture).
- `depspolicy`: the ban's walk now includes `cmd/cairn-capture`; control: a test-only file in a
  scratch copy importing `maragu.dev/gomponents` from it makes the ban red.

**S3.**
- CAS (clause a): two concurrent uploads from one `from` → exactly one 200, one 409 naming the
  stored position.
- Pod re-check (clause c), caps (413 names the cap; a just-under-cap record is accepted), per-record
  truncation marker, retention sweep at the boundary measured at retention − 1 s and + 1 s.
- Token isolation: capture tokens presented to every browser row, `POST /sign-in` and the pod are
  refused as a random token is (byte-compare); a capture token for `host-a` posting `host: host-b`
  → 400 and nothing written; revocation by deleting the row → next request 401, no restart.
- Startup: `-worker-addr` without `-worker-tokens`, or with `-transcript-dir` inside the store root,
  or without `-transcript-retention`, refuses; blanks refuse; a reachable bind with no trusted-proxy
  allowlist refuses.
- Clause (b) end to end: the e2e captures the S0 world with S1's planted secrets through the real
  agent into a real `cairn-ui`, then byte-scans `-transcript-dir` (decompressed) for each planted
  value: **0**; with `--no-redact` (a test-only build tag, absent from the shipped binary) the same
  scan finds **P**. Report the pair.

**S4.**
- Relationships over ONE store and journal (clauses d–f): owner sees; R (reads both written scopes)
  sees; P (reads one) gets the no-transcript bytes while P's session page is still found; a narrowed
  bearer credential of the owner sees nothing; empty-`W` session is owner-only; a scope renamed after
  the write is treated as unreadable for non-owners (control: renaming it back restores R's view).
- The trap's own control: a sabotaged predicate computing `W` from the viewer's scopes makes P see
  the transcript (clause e red).
- Byte identity: hidden, absent and deleted render the same section bytes.
- XSS: a record whose every string is `<script>…</script>` renders escaped; the raw-node ban still
  passes.
- uiaudit: the session page with a transcript at all five widths, overflow 0, the touch refusals
  from the mobile plan unchanged.

**S5.**
- Toggle fold, with literal expectations: no settings → OFF; instance ON, scope unset → ON; scope
  `beta-notes` OFF → a session that wrote both is OFF; an arc OFF overrides scope ON; a session ON
  overrides an arc OFF; empty `W` → OFF. Each shown red by its own mutant.
- Authorisation per level: a scope `write`-only principal cannot toggle the scope (403); its admin
  can; a session's non-owner cannot toggle the session.
- Journal: every accepted toggle appends exactly one record with `(Kind, ID)` and pod time; a refused
  one appends none.
- Capabilities (clause h): each refusal returns the same 403 bytes; the plugin name in an output
  envelope is taken from the token, and a body naming another plugin is overwritten (control: the
  mutant that trusts the body).
- Leases and the idle/growth trigger at both boundaries (9 min 59 s vs 10 min; 199 vs 200
  records); budget window (clause k).

**S6.** Output visibility equals transcript visibility for every viewer in S4's matrix (clause i,
asserted as equal SETS of viewers); a summary renders plugin name, version, model, watermark and
"N newer turns not yet summarised" when the transcript grew; deletion cascade (clause j) and its
journal fact; a ClickUp URL renders only through the `refurl` template from a validated id
(control: a `javascript:` id is refused by validation and renders as text).

**S7.** Against a fake provider (`httptest`): the first run summarises records 1–N; a second run
after M new records sends only records N+1–N+M plus the prior summary (asserted on the fake's
received bodies); a compaction summary record is labelled runtime-generated in the prompt; over
budget the plugin stops before calling the provider (control: with the cap at 0, the fake receives
0 requests — and with the cap high, ≥ 1, the positive control).

**S8.** Deterministic matchers, each with literal fixtures: a branch `feature/clk0000a1-x`
matches only when `clk0000a1` is a fetched ticket id OR appears as a ClickUp URL; a commit
message with `https://app.clickup.com/t/clk0000a1` → edge `commit-message`; a custom id `ALPHA-12`
matches only when `ALPHA-12` is in the fetched custom-id list (control: the same string with an
empty list → no edge); a `pr-link` record plus a PR body naming a ticket → edge `pr-body`; an
entry the session wrote with `refs: [clickup:clk0000a1]` → edge `entry-ref`. Every edge carries a
locator that resolves. LLM suggestions come from the fake provider with a fixed confidence and are
never written as edges (control: the fake returns a suggestion at confidence 1.0 — still a
suggestion). 429 handling: the fake answers 429 with a reset header and the plugin waits, asserted
by the fake's request timestamps. **All ClickUp fixtures are synthetic** (ids `clk0000a1…`, list
`list-0001`).

**S9.** `--self-test` prints `sabotaged=11 caught=11`; each sabotage fails ITS clause's message
(asserted by grepping the failing clause name); the `ok` floor, mutant count and e2e floor equal the
counts measured on the merged tree; `AGENTS.md` stays under its working budget.

## Open questions for the operator

Each has a recommendation; none blocks S0–S2.

- **Q1. Ship subagent transcripts?** They are ≈70% of the bytes (R1). **Recommend yes** — O1 says
  full raw, and a subagent is often where the tool work happened — with their own per-session cap.
- **Q2. Ship the duplicate and bookkeeping bytes, and persisted binaries?** Measured: `attachment`
  records ≈30% and the duplicate `toolUseResult` ≈16% of bytes; thinking signatures ≈5%.
  **Recommend:** ship them (O1) but let the renderer hide bookkeeping by default; do NOT ship
  persisted binaries (PDF, images) — the redactor cannot read them (T1). Text tool-results: yes.
- **Q3. One rule file shared with `leakscan`?** **Recommend yes** (one rule, one place), landed in
  S1 with leakscan's own controls re-run. The alternative is a second table pinned ⊇ the first.
- **Q4. Real-data redaction audit.** **Recommend** a host-local script, never committed, that runs
  the rule table over the host's own transcripts and prints only per-rule COUNTS, run once before
  arming capture and after every rule change.
- **Q5. The client instance.** It holds client principals and client-confidential content, and
  plugins there send transcripts to an LLM provider. **Recommend:** do not arm capture there until
  the personal instance has run for a while, and require an explicit instance-level decision before
  any LLM plugin is toggled on there.
- **Q6. Retention and caps.** **Recommend** 90 days, 64 MiB per session (compressed), and an
  instance cap sized from the measured growth (≈2.8 GB/week raw, ≈0.8 GB/week at the measured 3.5×
  — one host, Claude Code only).
- **Q7. One capture owner per instance (the presence single-owner wall)?** **Recommend yes for the
  personal instance**; the client instance's answer depends on whether anyone else will run capture.
- **Q8. Store ClickUp ticket titles?** **Recommend no by default** (id + URL only); a no-default
  flag allows titles per instance.
- **Q9. Account identifiers in records** (`ownerAccountUuid`, `accountUuid`, `credential_org`
  attachments). They are not credentials, so redaction does not remove them. **Recommend dropping
  those fields on the host** as a named, tested exception to O1 — or keep them, if full raw means
  literally every field.
- **Q10. Where the example plugins run.** **Recommend** a user timer on one operator host for v1
  (the LLM key and ClickUp token stay there); a pod is the same binary later.
- **Q11. `AGENTS.md`.** It has 25 bytes of headroom. The new packages need one layout row
  (≈150 bytes). **Recommend** the S9 PR evicts an equal amount of history to a README, as the weight
  test's playbook prescribes — never by narrowing a rule.
- **Q12. Storage backend and replicas.** **Recommend** the per-session directory (decision 15) and
  one replica; if a second replica is ever needed, move transcripts to object storage or pgstore
  behind the same `internal/transcript` interface.
- **Q13. Commit-message and PR-body signals need host access to repositories.** **Recommend** a
  host-local list of repository paths in plugin B's config, read with `git log` and the host's
  existing GitHub CLI login — nothing new on the pod.

## Recommended improvements beyond the ask (clearly recommendations)

- **B1. A "my sessions" page for the owner**, listing sessions with a transcript newest first —
  the owner currently reaches a session only through a scope or arc that names it, and an empty-`W`
  session is reachable by nobody else.
- **B2. Session-level metadata from the transcript.** `gitBranch`, `cwd` (repository name only),
  runtime, model and first/last record time are in every Claude Code record and in opencode's
  `session` row; showing them answers "where and when" without opening the transcript. They are
  subject to the same predicate.
- **B3. Re-scan on rule change.** A new redaction rule does not reach stored records. A pod-side
  sweep (`cairn-ui -rescan-transcripts`) applying the current table to stored records, journaling
  counts only, closes that gap.
- **B4. Confirm/dismiss for suggestions, inside cairn.** A suggestion promoted by a human becomes a
  `ticket-edge` with `signal: confirmed-by <(Kind, ID)>` — still no write-back to ClickUp.
- **B5. ClickUp write-back** (a comment linking the session) — out of scope (O6); future work.
- **B6. A transcript search** across sessions the viewer may see — out of scope; it multiplies T1's
  exposure and needs its own decision.

## What could not be measured

- **The second host**, any other runtime version, and any opencode-heavy host: every number in
  R1–R3 is one host at one moment.
- **Whether Claude Code ever rewrites a session file** rather than appending: one file, one 20 s
  interval. S2's rewrite refusal is the guard either way.
- **Whether compaction preserves the pre-compaction records** in every case: inferred from the
  append observation and the boundary records' presence, not traced record by record.
- **Whether either transcript format is a documented, stable contract.** No primary source found
  either way; the shape ledger (S0) is how drift becomes a red test rather than a silent drop.
- **What `opencode export --sanitize` removes**, and whether `opencode export` is safe to run
  against a database a live opencode process is writing (one export, one idle session).
- **Redaction recall on real transcripts** (R6) — deferred to Q4's host-local count.
- **LLM cost per summary** and summary quality; **ClickUp API behaviour** (no request was made;
  rate limits are from the documentation).
- **The compression ratio at scale** (3.5× on a 30-file sample) and the opencode share of growth.
- **Sizes and effort.** Not estimated; nobody has measured these slices.
