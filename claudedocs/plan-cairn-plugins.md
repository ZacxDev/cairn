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

**Revision history.**
- *Revision 2* records the operator's answers O8–O10 and corrects round 0's attribution of O3, O5
  and the host-side redaction (all three were the operator's, from the original ask and the option
  chosen). It applies audit round 1 and dispositions round 0's deletion candidates D1–D6 (table
  under "Audit dispositions"). The visibility set `W(s)` (writes only) became **`V(s)`, writes AND
  reads** (O8); "full raw" became **every byte, stored untruncated** (O9), with an agent-facing
  selective-read layer and a collapsing UI on top; sessions with no recorded write are **shipped,
  owner-only, and listed on a new "My sessions" page** (O10). Removed: the per-record 1 MiB
  truncation, the per-session cap, the instance-level and session-ON toggles, `-plugin-admin`, the
  pod-side budget window, work leases, and the shared rule file. Removed decisions and questions
  keep their numbers, marked REMOVED, so references stay stable.
- *Revision 3* applies audit round 2 to revision 2's own text. Persisted tool-result BLOBS get a
  redaction rule, a pod re-check and planted secrets (text), and UNREDACTABLE binary content is
  WITHHELD pending Q2 (decision 6a). A rendered header naming no scope — `scope=(all scopes)` or no
  `scope=` field — maps to `*`. Classification moved from record TYPE to BLOCK and FIELD level, and a
  "user message" is now defined as a human `text` block (decision 17). Clause (b)'s redaction-off run
  no longer touches the pod. Clause (h), decision 16 and S2's routing test now share ONE fixture, and
  a new clause **(m)** covers WITHDRAWING an already-shipped prefix when routing changes. Retracted:
  "the pod is authoritative" for `--repo`/cwd-derived commands (the pod adds `*` for them), "a
  mis-parsed command only ADDS a scope", revision 2's reason for the session opt-out (re-grounded),
  and a `--json` read path that does not exist for recall or search.
- *Revision 4* applies audit round 3. Binary content is withheld BY CONTENT (a `data:` URL, or
  base64 whose decoded bytes carry a known file signature), which catches two measured carriers
  revision 3 would have shipped — `toolUseResult.file.base64` and opencode `state.attachments[].url`.
  A session whose routing answer moves to a single OTHER instance is withdrawn and RE-SHIPPED from
  offset 0, not held. A cwd-derived recall or search is paired with its own result, whose header
  carries the resolved scope, so only header-less verbs add `*`. The withdraw answer distinguishes
  `deleted: true` (the caller's own root) from `false`. The text/binary rule is stated as this
  plan's own (leakscan's NUL sniff plus a UTF-8 test), JSON text blobs get decoded-string traversal,
  and the YAML `Secret` rule is line-based (no stdlib YAML parser). Retracted: "the one place the
  plan stores less than every byte", "one definition of binary governs both", the uniform withdraw
  answer, and `*` for every cwd-derived command.
- *Revision 5* applies audit round 4, four of whose findings were in mechanisms revision 4 added,
  by choosing the narrowest rule that is obviously safe and stating residuals. Revision 4's
  per-call header pairing is RETRACTED: a header cancels a cwd-derived `*` only for a single simple
  `cairn recall|search` call and only from its OWN result block, never from a hook attachment. A
  result that has not arrived is PENDING (owner-only and paused), not header-less. A repeated
  withdrawal by the same owner and host answers `true` (idempotent across a lost acknowledgement).
  The base64 decoder is named (four encodings, whitespace removed); embedded binary payloads and
  embedded encoded secrets are declared residuals rather than closed with a new matcher; the
  decoded-text floor drops from 64 to 16 characters; flow-style YAML `Secret`s are a declared
  residual; the Goal and O9 list every hold reason.
- *Revision 6* applies audit round 5's two definition changes in decision 3. SIMPLE becomes an
  ALLOWLIST (no shell metacharacter; words exactly `cairn` + `recall|search` + arguments) instead of
  a list of forbidden operators. A result has ARRIVED when it is TERMINAL — opencode `completed` OR
  `error` (an error is header-less, so `*`) — and the 24 h backstop counts from the CALL's own
  timestamp, not session idleness. Retracted: revision 5's example-based "simple", its "1,260 of
  41,117 not yet completed" evidence (1,255 were finished errors, 5 were running), and its
  idleness-keyed backstop.
- *Revision 7 is a DELETION.* Round 6 of the audit measured that the header-cancels-`*` exception
  admitted 0 of 597 no-`--scope` Claude Code calls and 5 of 24 opencode ones on this host, so the
  exception, the "simple" allowlist, the pending set, the 24 h backstop, the agent-side
  working-directory resolution for routing, their tests and six mutant rows are DELETED, not
  refined. The rule is now: every `cairn` invocation without an explicit `--scope` adds `*`, on the
  pod and the agent, unconditionally; output headers only ADD scopes. **Everything revisions 4–6
  say above about pairing, "simple", pending results or the backstop is superseded by this entry
  and by decision 3.** Q16 is rewritten with the measured cost and the remedies.
- *Revision 8* adds the one rule revision 7 lacked: how `scopeuse` RECOGNISES an invocation. Any
  command line naming the program (a `cairn`/`subsystem-recall` token, a `…/cairn` path, a `#cairn`
  flake ref) that cannot be decomposed into invocations each carrying an explicit `--scope` adds
  `*`. Revision 7's "a mis-parsed command ADDS a scope … for every remaining path" is retracted, the
  `*` rule is scoped to VISIBLE invocations, and T3 names unseen program names as the residual.

## Goal and premise

The operator asked for two things, carried in one plan because the second consumes the first:

1. **Session content in the store.** Every byte of every Claude Code and opencode session — tool
   calls, tool outputs, subagents and the runtimes' own bookkeeping included — shipped from the host
   it ran on to cairn, redacted on that host before it leaves, and shown on the session page
   (`/session`) to the principals allowed to see it. Two exceptions narrow "every byte", each
   decided and stated where it lives: content no redactor can read is withheld pending Q2
   (decision 6a), and a HELD session stays on its host (decision 16) — held when its scopes route
   to two instances, when it carries `*` or an unroutable scope name while more than one instance is
   configured, or when a withdrawal of its earlier prefix was answered `false`.
   Agents are expected to be the main readers, so
   storage keeps every byte while READING is selective: a skeleton, filters, ranges and bounded pages
   (decision 17), and a UI that collapses tool calls and subagents (decision 18).
2. **A plugin system.** Out-of-process workers that read what (1) stored, through a
   capability-scoped API, and write back DERIVED records the browser surface renders. Two example
   plugins prove the abstraction: continuous session **summaries**, and **ClickUp ticket
   matching** (deterministic ID matches plus LLM suggestions).

The premise is that a session page today shows WHERE a session wrote (bullets, scopes, arcs,
`internal/ui/sessionpage.go:206-260`) but not WHAT it did, and that the answer to "what did that
session do" currently lives only on the host that ran it.

### What would make this unnecessary

Drop the work, or the named half, if any of these holds:

- **The host is always at hand.** If every reader of a session is also someone with shell access to
  the host that ran it, the operator's own tooling already answers "what did that session do"
  locally, and shipping transcripts buys a second, remote copy of an answer that exists — at the
  cost of a new, large, sensitive dataset (≈2.8 GB of raw Claude Code JSONL per week on the one
  host measured, subagents included, R1).
- **Summaries are enough.** If nobody ever reads the raw content, the plugin half could summarise
  ON THE HOST and ship only derived text. The operator chose every byte (O1, O9), so this is
  recorded, not argued.
- **The ClickUp half is a lookup, not a store.** If explicit ticket IDs in branch names and commit
  messages are the only matches anyone trusts, a host-side script printing them answers the
  question without a plugin API. The deterministic half of plugin B is cheap either way; the API is
  justified by the LLM half and by summaries.

### closing-condition

- **closing-condition:** `check`. Four mechanical parts, all required:
  1. Slices **S0–S10 are MERGED** on cairn `main`, verified by content (the named files exist with
     the named tests), not by ancestry.
  2. **`tests/plugins/e2e.sh` exits 0** in the `go` CI job, with **`--self-test` printing
     `SUMMARY e2e-self-test: sabotaged=13 caught=13`** (one sabotage per clause below), and the
     job's PASS floor set to the count measured when the script lands (the `tests/presence/e2e.sh`
     pattern, `.github/workflows/ci.yml:1045-1055`).
  3. **`cairn-capture --self-test` exits 0** in the `go` job and prints
     **`SUMMARY redaction: planted=P caught=P clean-damaged=0`** over the realistic synthetic corpus
     (decision 6), where P is the planted count declared by the corpus generator (asserted equal,
     not read off the run).
  4. **The two example plugins' own suites exit 0** in the `go` job against in-process fake LLM and
     fake ClickUp servers, with synthetic fixtures only.

  Each script exits **2** — "could not vouch", never a skip and never 0 — when a prerequisite is
  missing (a Go toolchain, a built `cairn-ui` or `cairn-capture`; the opencode leg uses a recorded
  synthetic export, so a missing `opencode` binary does not skip it) or any of its own controls
  misbehaves.

  | clause | relationship it asserts | the sabotage that must turn it red | slice that wires it |
  |---|---|---|---|
  | **(a) resume** | an upload interrupted mid-stream and retried lands every record exactly once; a stale `from` gets 409 naming the stored position | accept any `from` (no compare-and-swap) | S3 |
  | **(b) nothing unredacted leaves the host** | fixture sessions carrying P runtime-generated secrets — in records AND in a persisted TEXT tool-result blob — are captured; a recording relay between agent and pod REASSEMBLES continuation frames and then sees **0** of them in the agent's OUTGOING request bodies. The positive control is a SEPARATE run with redaction disabled (a test-only build tag) in which the relay answers 200 ITSELF and forwards nothing, so no pod refusal can stall the stream: it sees **P**. Reported as the pair. Measured BEFORE the pod, so the pod's re-check (c) cannot mask it | skip redaction for `tool_result` content | S3 |
  | **(c) the pod re-checks** | hand-built requests (bypassing the agent) carrying a value the rule table matches — one as a record, one as a text blob — are each refused 422 and NOTHING is stored; the same requests with clean values are stored (positive control) | drop the pod-side scan | S3 |
  | **(d) visibility, owner** | the uploading owner sees the transcript section; a principal with no read on any scope in `V` gets the same bytes as a session that has no transcript | render without the predicate | S4 |
  | **(e) visibility, every WRITTEN scope** | over one store, viewer R reads `alpha-notes` AND `beta-notes` (both written) and sees it; viewer P reads only `alpha-notes` and gets the no-transcript bytes, although P's session PAGE is found | compute the written set over the VIEWER's readable scopes instead of the whole store (decision 3's trap) | S4 |
  | **(f) visibility, every READ scope (O8)** | the session WROTE only `alpha-notes` and READ `beta-notes` (a rendered recall of it is in a tool result); viewer Q reads `alpha-notes` only and gets the no-transcript bytes; R sees it | drop the read half from `V` | S4 |
  | **(g) empty set is owner-only (O10)** | a session with no recorded write or read is stored, visible to its owner, listed on the owner's "My sessions" page, and absent for everyone else | treat "every scope of the empty set" as true | S4 |
  | **(h) routing honours reads (O8)** | THE routing fixture (shared with decision 16 and S2's routing test): a routing table sending `alpha-notes` to the personal instance and `beta-notes` to the client instance, and a session that WROTE `alpha-notes` and READ `beta-notes` in its FIRST turn. It is uploaded to NEITHER instance (the relay sees 0 requests for it) and is held, logged | route on written scopes only | S3 |
  | **(i) toggles default off, and only narrow** | a plugin token's pending list is empty until a toggle is set; after `alpha-notes` is set ON it lists exactly the sessions whose every scope in `V` is ON | default a missing scope toggle to ON | S5 |
  | **(j) capability scoping** | the summary plugin's token is refused (403, uniform) on a transcript of a session where it is OFF, and on writing a `ticket-edge` output | drop the per-session toggle check on `transcript:read` | S5 |
  | **(k) outputs inherit visibility, and say what they are** | a summary is shown to exactly the principals clauses (d)–(g) admit, and its render carries plugin name, version, model and watermark | render outputs without the source predicate | S6 |
  | **(l) deletion cascades** | the owner deletes a session's transcript; its directory, its plugin outputs and its edges are gone; the plugin's next transcript read is 404 | delete the transcript but keep outputs | S6 |
  | **(m) a shipped prefix is withdrawn when routing changes** | the same two instances; a session WROTE `alpha-notes` and shipped its prefix to the personal instance, then READ `beta-notes`. On the agent's next run no record from the read onward reaches either instance, the agent sends a withdrawal, the personal instance answers `{"deleted": true}`, and its directory, outputs and edges for the session are gone (the deletion cascade of (l)) | skip the withdrawal (stop shipping only) | S6 |

  The sabotages run on a scratch copy of the tree with its `.git` removed (the
  `tests/control_mutants.py` pattern), and each must be caught by ITS clause's own assertion. Each
  clause also has a Go-side mutant row (listed under Slices) where the guarded code is Go.

  ⚠ **What the closing condition does NOT require**, stated so nobody reads it in: (i) any real
  deployment having transcripts; (ii) the operator judging summary QUALITY or match PRECISION —
  both are human judgements over real data and are post-close rollout (below); (iii) a real LLM
  provider or a real ClickUp workspace ever being called by CI. Redaction RECALL on real data is not
  measurable by any check that may run in this public repository (R6).

### Rollout (NOT part of the closing condition)

- **The personal instance first**, with capture armed on one host, retention set, plugins
  registered but every toggle OFF (decision 10 — an agent decision, not an operator one). The
  operator turns on summaries for one scope, reads a week of output, and judges it. The same for
  plugin B against one ClickUp list.
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
  **6.5 GB** (max 31 MB) — **subagents are ≈70% of all JSONL bytes**. Beside each, an
  `agent-<id>.json` metadata file with keys `agentType`, `description`, `spawnDepth`, `toolUseId`,
  and on some `parentAgentId`, `spawnedWithWorktree`, `worktreeBranch`, `worktreePath`,
  `requestShape`, `requestNonInteractive` (key sets over 2,000 files).
- `<project>/<session-uuid>/tool-results/*` — tool output too large to keep inline, referenced from
  a record's `toolUseResult.persistedOutputPath`: 2,687 `.txt`, plus a few `.pdf`, `.html`, `.jpg`;
  299 MB in all.
- `<project>/memory/*.md` — NOT transcript content; out of scope.
- **Per ROOT session, EVERY byte** (its own JSONL + all its subagent files + its persisted tool
  results): 866 root sessions, **p50 6.6 MB, p90 26.5 MB, p99 72.1 MB, max 154.3 MB**.
- **Growth, every JSONL byte (top-level AND subagent files):** files modified in the last 7 days
  total **2.80 GB** (2,292 files, 226 of them top-level); last 30 days **8.97 GB** (7,425 files).
  Persisted tool results are NOT in those figures (299 MB in total, no rate measured); opencode is
  not either (R3). **gzip -6 on a 30-file sample: 3.5×** (57.8 → 16.6 MB).

**Records.** One JSON object per line; 0 unparseable lines in the 300 newest files. Top-level
`type` values seen (counts over 300 files): `assistant`, `attachment`, `user`, `queue-operation`,
`last-prompt`, `permission-mode`, `mode`, `pr-link`, `ai-title`, `system`, `atis-latch`,
`bridge-session`, `history-suppression`, `frame-link`, `cost-state`, `agent-name`,
`artifact-autoreact-ledger`, `artifact-comment-monitor`, `continued-in`. **The set is open**: it is
the runtime's and grows across versions (whether it is a documented contract is UNVERIFIED — no
primary source was found either way).

- **Envelope on conversation records** (`user`, `assistant`, `attachment`, `system`): `uuid`,
  `parentUuid` (string, or null at a root), `timestamp` (string), `sessionId`, `isSidechain`
  (bool), `cwd`, `gitBranch`, `version`, `entrypoint` (`cli` | `sdk-cli`), `userType`, and on some
  `agentId`, `slug`, `promptId`, `requestId`.
- **`assistant.message`**: `id`, `model`, `role`, `content`, `stop_reason`, `usage`, and more.
  Content blocks: `text {text}`, `thinking {thinking, signature}`, `tool_use {id, name, input,
  caller}`.
- **`user.message.content`**: a string, or blocks `text` and `tool_result {tool_use_id, content,
  is_error?}` whose `content` is a string (31,303) or a list of `text` / `image` /
  `tool_reference` items (1,736; 68 `image` items — inline base64). **Tool output size:** p50 449 B,
  p90 4.1 KB, p99 23 KB, max 656 KB (JSON-encoded).
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
  conversation a reader means by "the transcript"**. All of it is STORED (O9); decisions 17 and 18
  are how a reader avoids paying for the rest.

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
  append observation, not separately measured). Decision 18 renders that record as
  runtime-generated, NOT as a user message, and plugin A must not mistake it for a human turn.
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
  `part.data` averages 3.1 KB, max 2.8 MB, total 458 MB (all time; no rate measured).
- **Parts are MUTATED, not appended**: 135,976 of 145,685 parts have `time_updated >
  time_created` (streaming fills them in), and the `event` log carries `message.removed.1` (262).
  So an opencode watermark is **(part id, `time_updated`)**, and an upload is an UPSERT by part id —
  the Claude Code byte-offset model does not apply.
- **`opencode export <sessionID>`** prints one JSON document `{info: {id, slug, projectID,
  directory, path, title, agent, model, version, summary, cost, tokens, time}, messages: [{info,
  parts: [...]}]}` — the same message and part objects, without opening the database ourselves.
  Measured on ONE 21-part session: 0.8 s, 28,051 bytes, exit 0. **`--sanitize`** ("redact sensitive
  transcript and file data", per its `--help`) produced 17,264 bytes for the same session — WHAT it
  removes was not measured, and O1/O9 (every byte) rule it out as the redaction step anyway.

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
  password typed into a prompt; a credential inside an IMAGE or PDF (inline `image` blocks, R1, and
  persisted binary tool results); and any of these JSON-escaped (`\n`, `\u…`) inside a string
  value, where a raw-byte regex sees different bytes.
- **No scanner recognises unshaped secrets.** Entropy heuristics trade false positives (commit
  SHAs, digests, UUIDs, base64 payloads — all common in transcripts) for recall. Public secret
  scanners publish large pattern sets [S]; none can be the floor of a guarantee, which is why the
  residual is stated (threat T1), not designed away.
- **No text scanner reads an image.** That is the one way binary content is genuinely different
  from text for THIS plan: storage would be identical, but redaction — a hard requirement — cannot
  apply, so decision 6a WITHHOLDS it until Q2 is answered.
- **A persisted tool result is a FILE, not a JSON string** (R1: `tool-results/*.txt`, `.html`,
  `.pdf`, `.jpg`). A redactor that walks decoded JSON string values never sees it; decision 6a gives
  text blobs their own rule.
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
  configured list, get one task. Confirm against the primary API reference at S10.

### R6. What a public repository cannot measure about redaction

Recall on REAL transcripts — "of the secrets actually present, how many were caught" — needs a
ground truth that only reading real content provides, and this repository may not carry or quote
real content (`AGENTS.md`). So redaction is measured on a synthetic corpus (decision 6) and its
REAL recall is an operator-side measurement: Q4 recommends a host-local, never-committed audit
script that prints only counts.

### R7. How cairn content ENTERS a transcript — the read half of `V` (O8)

- **Both rendered read answers carry a machine-readable header line starting
  `subsystem-recall:`.** Recall renders `subsystem-recall: status=<…> scope=<scope>`
  (`internal/report/text.go:306-310`); search renders the same prefix plus `query=…`
  (`internal/report/searchtext.go:20-28`); both then print the store host line with the instance
  label (`hostid.StoreHostLine`, `text.go:310`). Pod and CLI share this ONE renderer (`AGENTS.md`),
  so the header is the same wherever the answer was produced.
- 🔴 **The header does NOT always name a scope.** A store-wide search sets the scope field to the
  literal `(all scopes)` (`internal/report/search.go:720-721`), and a third header form,
  `subsystem-recall: <status>: all N entry file…` (`internal/report/renderer.go:158`), carries no
  `scope=` field at all. *Revision 2 said "a header naming the scope"; that was false for both.*
  Decision 3 therefore maps EVERY `subsystem-recall:` line whose `scope=` value is absent or is not
  a valid scope name to the sentinel `*`.
- That makes the header a CONTENT signal: it is found wherever the answer landed — a shell tool's
  stdout, a `toolUseResult` copy, an opencode tool `state.output`, or a hook's injected context in an
  `attachment` record — whether or not the transcript also shows the command that produced it.
- **The header carries the RESOLVED scope.** `RecallReport.RenderText` prints `r.Scope`
  (`text.go:308`), the scope the client resolved — so a bare or `--repo` recall or search still
  names its real scope in its own output, however the command line spelled it. That header ADDS
  the scope to `V`; it does not cancel the `*` the cwd-derived call itself adds (decision 3).
- **Other paths, with weaker signals:** the Go client's verbs that take `--scope`/`--repo`
  (`recall`, `search`, `sessions`, `arcs`, `arc-show`, `validate`, `ls-entries`, `arc-register`,
  `append`, `put`, `create`; `internal/client/cli.go:47-133`) visible as a command line with
  `--scope`/`--repo`/`--all-scopes`, or with NEITHER, in which case the scope is derived from the
  working directory's repository (`internal/client/reposcope.go:71, 89`). Only `report`'s recall,
  search and the line at `renderer.go:158` render a `subsystem-recall:` header; every other verb
  above is **header-less**, so for it the command line is the only signal (an `append` also leaves
  a trailer, which `W_trailer` sees). And a direct file read of the local cache by path. `--all-scopes` (`search` and
  `arcs`, `cli.go:76, 97`) reads every scope the credential can reach. *Revision 2 listed a `--json`
  read; recall and search have no `--json` flag (only `doctor` does, `cli.go:114`), so that path is
  deleted.*
- None of this was measured on real transcripts (that would mean reading content). S0's fixtures
  carry each path synthetically.

## STEP 2 — What exists today, read off the code (`3046e2a`)

### A session is not a record

- **There is no session record anywhere.** A session id is an opaque string bounded by
  `write.SessionComponent` (`internal/write/bullet_request.go:26`), compiled from `sessionClass` =
  `[A-Za-z0-9][A-Za-z0-9_.-]{0,63}` (`internal/write/revision.go:71`). Both runtimes' ids fit: a
  36-character uuid and a 30-character `ses_…` id.
- It appears in three places only: the bullet trailer ` [cairn: <actor>/<session>]`
  (`revision.go:44`), stamped only by `AppendBullet` (`internal/write/write.go:108`); arc members
  `Member{Session, Role, FirstSeen, Carried}` (`internal/arcs/arcs.go:74-85`); and in-memory
  presence rows (`internal/presence/presence.go:83-93`).
- **The session is SELF-DECLARED** by the writer: `cairn append` requires `--session` with no
  default (`internal/client/cli.go:120, 139-143`); nothing authenticates it.
- **`put`/`create` stamp no trailer** (`internal/touch/touch.go:20-26`), so a session that wrote
  only through them is invisible to `touch`.
- **Reads leave no trace in the store at all** — nothing records which session recalled what. The
  read half of `V` can only come from the transcript (R7).
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
- **Cost.** The arcs/presence plan measured the whole-store session page at ~249 ms/op at 30
  scopes × 100 entries on a synthetic store (`claudedocs/plan-cairn-arcs-presence.md`, "Cost,
  measured"; local go 1.26, not the pinned 1.25). Decision 3 walks EVERY scope, not the viewer's —
  at least as much as that page for the widest viewer and more for a narrow one. **Unmeasured** for
  this design; S4 benchmarks it (decision 3).

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
- 🔴 **A session with no write is NOT FOUND by `/session` today** — `Found()` needs a write or an
  arc membership (`sessionpage.go:106-113`). O10's owner-only sessions therefore need their own way
  in: the "My sessions" page (S7), and an owner arm on `/session` (decision 4).

### Authority — `internal/control`

- `control.Resolve(m Model, p Principal) Authorization` (`internal/control/resolve.go:187`) returns
  per-scope verb sets; `Allows(scope, verb)` is the predicate (`:89`), `NamedScopes(verb)` (`:157`)
  maps ids to names. Verbs are a closed `{read, write, admin}` (`model.go:50-67`).
- Authority arrives two ways — project membership via `roleVerbs` and grants
  (`resolve.go:199-231`) — so **a `Model.Grants` listing under-reports every member**
  (`AGENTS.md`; `internal/ui/sharing.go:15-24`; guard
  `TestTheAudienceIsComputedFromResolveNotFromGrantRows`, `internal/ui/sharing_test.go:354`).
- Principal kinds are only `user` and `project` (`model.go:150-161`); the stable key is
  `(Kind, ID)` — `Display` is mutable (`resolve.go:17-26`). There is no host or agent principal.
- `Authorization.Narrowed()` (`resolve.go:78`) is true for a narrowed bearer credential; `Narrow`
  only intersects scopes (`resolve.go:419-441`), so `Allows` on a narrowed authorization is already
  correct for a scope check.
- **Journal credentials: revocation is APPLIED but has NO WRITER.** Replay applies
  `EventCredentialRevoked` (validated at `internal/control/journal.go:217`, applied at `:435-445`,
  setting `RevokedAt`); nothing in the tree emits one (`internal/control/credential_issue.go:94-96`).
  *Revision 1 of this plan said there was "no revocation path"; that was wrong — a hand-appended
  event works, a tool does not exist.*

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
  (`cmd/cairn-ui/main.go:284, 446-467`). An unknown schema version refuses
  (`migrate.go:166-173`), so a migration makes a UI rollback an outage.

### The pod, the UI and the gates a change passes through

- **Two binaries.** `cmd/cairn-server` (the pod, `internal/api`) and `cmd/cairn-ui` (browser,
  presence) are separate processes (`cmd/cairn-ui/main.go:26-30`). Storage, predicate, registry and
  rendering live in `cairn-ui` and new binaries: **the pod does not move**, so `api.DeclaredRoutes()`
  (`internal/api/routes.go:93`), `tests/conformance/requests.json` and `tests/dualrun/` are
  untouched. ⚠ **Decision 17 adds Go-only READ verbs to `cmd/cairn`**, which moves the Go client's
  verb ledgers (below) but not the parity corpus: Go-only verbs are declared, not compared, as
  `sessions` and `arcs` already are (`internal/client/cli.go:39-46`).
- **UI routes** are an exact-match table (`internal/ui/routes.go:117-235`); a row moves the hand
  ledger `TestTheRouteLedgerMatchesTheDispatchTable` (`routes_test.go:59`), `contentAuthority`
  (`:800-822`), `TestEveryServedPathComesFromTheLedger` (`:537`) and uiaudit's derived target maps
  (`uiaudit/targets.go:180, 228, 275, 334`).
- **Two cross-site gates by METHOD** (`internal/ui/server.go:1244-1310`, `stateChanging` `:1323`):
  same-origin before auth, CSRF after. A non-browser worker cannot pass them and must not be made
  to — the presence precedent's reason for a second listener. A bearer GET passes neither gate's
  scope (neither applies to GET), so agent READS can use the browser listener (decision 17).
- **XSS:** every rendered value is a `g.Text` or attribute; `Raw`/`Rawf` are AST-banned
  (`AGENTS.md`; `internal/ui/README.md` "The raw-node ban"). LLM output is attacker-influenced
  text and is rendered the same way — never as Markdown-to-HTML.
- **Script policy:** exactly one allowlisted script, `filter.js` (`internal/ui/script.go:66`).
  Decision 18's collapsing and "read more" use `<details>`/`<summary>`, so it adds none.
- **No Content-Security-Policy** by operator decision (`internal/ui/server.go:1673`).

### Dependencies — `internal/depspolicy`

- `DeclaredModules` is exactly `github.com/lib/pq` and `maragu.dev/gomponents`
  (`internal/depspolicy/depspolicy.go:204-231`), failing on GROW or SHRINK
  (`depspolicy_test.go:42`).
- **The import ban** covers `LinkedBinaryRoots` = `cmd/cairn`, `cmd/cairn-server`
  (`depspolicy.go:443-446`); `cmd/cairn-ui` is NOT banned (it is the positive control, `:450`).
- 🔴 **The ban's graph holds NON-TEST imports only** (`depspolicy.go:147-154`; `ImportGraph`,
  `:628`): a `_test.go` file importing a third-party module never reaches it, by design. A negative
  control for the ban must therefore use a NON-test `.go` file (S2).
- Nested modules (`uiaudit`) are a separate allowlist (`DeclaredNestedModules`, `:256-262`).
- 🔴 **Reading opencode's SQLite in Go needs a third-party driver** — the stdlib has none. Decision 5
  avoids it by reading `opencode export`'s JSON (R3), which keeps the capture binary stdlib-only and
  lets it join the ban.

### Governed ledgers a new package, row, verb or binary moves

| ledger | where | moves when |
|---|---|---|
| `go` job per-package `ok` floor | `.github/workflows/ci.yml:836` (`-lt 24`) | every new tested package; set to the count MEASURED on the merged tree |
| control mutant battery, **296** rows | `tests/control_mutants.py` `PKGS` (`:88-130`), pinned by `tests/test_control_mutant_count_is_pinned.py` into `ci.yml:865`, `:848-851` and `internal/control/README.md:261, 265, 417` | a new authz package joins `PKGS`; every new row |
| UI route ledgers | `routes.go`, `routes_test.go:59, 537, 800-822`, `uiaudit/targets.go` | every new browser row (S4–S8) |
| Go client verb ledgers | `internal/client/cli.go:47-154`; `capability_ledger.LEDGER` `go_only`; `flake.nix` `want-go-only-verbs.txt`; `tests/test_go_client_ledgers.py` | decision 17's `transcript` verbs (S8) |
| env ledger | `internal/envalias/envalias.go:164`, twin `lib/env_aliases.py`, `tests/test_env_aliases.py` | only if a new variable needs an alias — decision 17's `CAIRN_UI_URL` is a new name with NO old alias, so the ledger does not move (asserted) |
| depspolicy | `LinkedBinaryRoots` `depspolicy.go:443-446`; `DeclaredNestedModules` `:256-262` | `cmd/cairn-capture` joins the ban (S2); the plugins nested module (S9) |
| `onlyGo` | `flake.nix:351-467` | any embedded non-Go file or named Go-test fixture |
| `goVendorHash` | `flake.nix:505` | only if a module is added — none is planned |
| flake packages and checks | `flake.nix` | `packages.cairn-capture` (S2) with a `-verbs`-style ledger check |
| e2e PASS floors | `ci.yml:1015-1025` (arcs), `:1045-1055` (presence) | `tests/plugins/e2e.sh` adds its own |
| `AGENTS.md` weight | `tests/test_agent_instructions_weight.py:133, 154` | **25 bytes of headroom today** (31,308 + 267 against a 31,600 working budget) |

## Decisions — who chose what

### Chosen by the OPERATOR (paraphrased; not re-litigated)

| # | the operator's choice | cost accepted / where it lands |
|---|---|---|
| O1 | Ship the full raw transcript, tool outputs included, for BOTH Claude Code and opencode — both are mandatory. Redacted on the host before upload — that host-side redaction was part of the option the operator chose, over a redacted conversation-only subset. Sharpened by O9. | Redaction is a hard requirement; its residual is threat T1, stated rather than argued away. |
| O2 | A session's content is visible to its OWNER, and to principals who can read EVERY scope the session touched — computed through `control.Resolve`, never `Model.Grants`. A session that touched a scope you cannot read is hidden from you. Plugin output derived from it follows the same rule. Sharpened by O8. | Stricter than today's session page (a union); decision 4. |
| O3 | The session page SHOWS the content when you view a session — the operator's own words in the original ask. | A transcript renderer in `internal/ui` (S4, decision 18). |
| O4 | Plugins are OUT-OF-PROCESS workers — on a host or in their own pod — calling a capability-scoped plugin API with plugin tokens. The pod (here: `cairn-ui`) holds an in-tree registry of manifests, toggles and outputs, and renders. The serving path stays stdlib-only; plugin credentials (LLM keys, ClickUp tokens) never enter it. | A new listener (decision 8); example plugins are separate binaries (S9, S10). |
| O5 | Example plugin A: continuous session summaries, toggleable per scope and per arc — the operator's own words in the original ask. | Toggle levels are scope and arc (decision 10). |
| O6 | Example plugin B: match ClickUp tickets to sessions — (a) deterministically from explicit ids/URLs in branch names, commit messages and trailers, PR bodies and transcripts; (b) LLM semantic suggestions, shown AS suggestions with a confidence. No write-back to ClickUp. | Write-back is future work (B5). |
| O7 | An MCP server is on hold; plugins are not to be designed as MCP servers. | The plugin API is plain HTTP+JSON. |
| O8 | The visibility set is the scopes the session WROTE to PLUS the scopes it READ through cairn — derived from its read calls in the transcript, plus declared scopes. The read scopes also feed instance routing, so a session that read the client instance cannot ship to the personal one. | `V(s)` (decision 3), clauses (f) and (h). A session that recalled many scopes becomes visible to fewer people. |
| O9 | "Full raw" means EVERY byte — bookkeeping records, duplicate tool-result copies and subagents included. Because agents will be the main readers, selective reading is designed on top: a skeleton, filters, ranges, collapsed bookkeeping and duplicates by default, bounded pages; and a UI that collapses subagents and tool calls, truncates long assistant responses behind a reveal, and NEVER truncates a user message. | No storage-time truncation or per-session cap (decision 15); read-time limits only (decisions 17, 18). Two agent-side exceptions, both stated where they live and in the Goal: unredactable binary content is withheld pending Q2 (decision 6a), and held sessions stay on their host (decision 16). |
| O10 | Sessions with no recorded write ARE shipped, visible to their OWNER only, and a "My sessions" page lists them. | Clause (g); S7. |

### Chosen by the AGENT writing this plan (open to review)

1. **`cairn-ui` holds transcripts, the registry, toggles and outputs; the pod does not.** Only
   `cairn-ui` over a control journal resolves a GitHub session and a bearer credential to one
   `(Kind, ID)` (presence plan, decision 1), the session page lives there, and keeping the pod
   untouched keeps the conformance and dualrun gates — and the Python oracle — out of this work.
   ⚠ Assumes ONE `cairn-ui` replica, as presence does (Q12).

2. **Machine credentials are WORKER TOKENS in their own file, not journal credentials.** The reasons
   (*revision 2 replaces revision 1's, which rested on a wrong premise — see STEP 2*):
   - **A plugin is not a principal.** Journal credentials authenticate a `user` or `project`
     principal and carry THAT principal's whole authority through `control.Resolve`
     (`resolve.go:307-335`). A plugin's authority is a per-session toggle state, not a scope verb
     set; minting it a principal would hand it every scope its project can read, and every browser
     row would accept it.
   - **A capture token's authority is narrower than any verb**: append to its owner's transcripts
     from ONE host. The verb set has nothing that small (`model.go:50-67`), and narrowing only
     intersects scopes.
   - **Revocation by deleting a row, re-read per request**, is the presence precedent and needs no
     tool; journal revocation is applied on replay but has no writer.
   Two kinds, `-worker-tokens <file>` with no default:
   - **`capture <kind>:<id> <host> <digest>`** — bound to ONE owner and ONE host label. May only
     append to transcripts it owns, from its host, and WITHDRAW (delete) a session it uploaded
     (decision 16). Cannot read anything.
   - **`plugin <name> <digest>`** — bound to ONE registered plugin name. Its capabilities are the
     plugin's manifest's, as admitted by the registry (decision 9). Cannot sign in, cannot read the
     store, cannot read a transcript unless the plugin is ON for that session.
   Neither is known to `identity.Backends`, so every browser row and the pod answer them as they
   answer garbage (the presence precedent, and the same byte-compare test).

3. **The visibility set `V(s)` is every scope the session WROTE or READ, computed so that it only
   GROWS (O8).**
   - `W_trailer(s)` = every scope in which `touch.Writes` finds an edge naming `s` — walked over
     every scope in the store, NOT the viewer's readable set (STEP 2's trap; clause (e)).
   - `R_header(s)` = every scope named by a rendered read header (R7) found in ANY string of ANY
     stored record of the session — tool results, structured duplicates, attachments, hook
     context, opencode tool outputs. A content signal; clause (f).
   - `R_header` maps a `subsystem-recall:` line whose `scope=` is absent or not a valid scope name
     (`(all scopes)`, the `renderer.go:158` form) to the sentinel **`*`** — "unknown scope" (R7).
   - `U_calls(s)` = scopes named by `cairn` read and write commands visible in tool inputs with an
     explicit `--scope X`, and file reads under a cache root. A command with `--all-scopes` adds
     `*`. **Every VISIBLE `cairn` invocation whose scope does NOT come from an explicit `--scope` —
     a `--repo P`, or no scope flag at all (scope derived from the working directory's repository)
     — is cwd-derived and adds `*`, unconditionally, on the pod AND on the agent.** Nothing cancels
     that `*`: not a header in the call's output, not a hook attachment, not a later record.
     Headers in output still ADD the scopes they name (`R_header`); they never REMOVE anything. A
     read run by a HOOK has no visible command line, so it adds only its header's scope.
   - **How an invocation is RECOGNISED, failing closed.** A command line is a CANDIDATE when it
     contains any of: a `cairn` or `subsystem-recall` program token, a path ending in `/cairn`, or
     a flake reference ending in `#cairn`. A candidate line contributes explicit scopes ONLY when
     the parser decomposes it into `cairn` invocations that EACH carry an explicit `--scope`;
     otherwise it adds **`*`**. So the shapes the parser does not take apart — a full or relative
     path to the binary, `nix run <flake>#cairn -- …`, an environment prefix (`VAR=x cairn …`), a
     wrapper (`bash -c '…'`, `xargs cairn …`, `env`, `timeout`, `sudo`), the `subsystem-recall`
     alias — add `*` whenever any invocation in them lacks a decomposable `--scope`. *Revision 7
     never said how a call is recognised, so such a line added NOTHING — no scope and no `*` —
     leaving `V` too small; that gap is closed here, and nothing else changes.*
   - *DELETED in revision 7, with no replacement:* the exception under which a header in a "simple"
     `cairn recall|search` call's own result cancelled the `*`, the allowlist defining "simple", the
     PENDING set `P` that waited for late results, and its 24 h backstop. Revisions 4–6 refined that
     exception three times; round 6 of the audit then MEASURED what it admitted on this host among
     calls with no `--scope`: Claude Code **0 of 597** (every one contains `>` — `2>&1`,
     `2>/dev/null` — and 587 also `|`), opencode **5 of 24** (19 rejected by a quote). A mechanism
     admitting essentially no real traffic protects nothing and costs three moving parts, so it is
     removed rather than refined again. Its earlier forms — revision 4's per-call header pairing,
     revision 5's example-based "simple", revisions 5–6's pending set — are retracted with it.
   - `D(s)` = scopes the capture agent declares at upload — under this rule only scopes it read off
     explicit `--scope` arguments and headers, i.e. the same parser's output.
   - `V(s) = W_trailer ∪ R_header ∪ U_calls ∪ D`, over the root and every child stream (decision 7).
   - **One parser, two callers, the SAME answer.** `R_header` and `U_calls` come from ONE package
     (`internal/transcript/scopeuse`), imported by `cmd/cairn-capture` (routing, decision 16) and by
     `cairn-ui` (which re-derives over the STORED records at upload, so an old or lying agent cannot
     shrink `V`). Neither caller resolves a working directory: the pod cannot (`DeriveScope` needs
     the repository's git common dir and `ScopeForRepo` runs `git`,
     `internal/client/reposcope.go:71, 89`), and the agent no longer does. *Revisions 3–6 had the
     agent resolve cwd-derived commands with `DeriveScope` for ROUTING; that path is DELETED — a
     `cd ../other && cairn recall`, a stale record `cwd`, or an opencode `bash` call's own `workdir`
     argument could each make it resolve the wrong scope, and with the rule above there is nothing
     left for it to do.* Routing uses only explicit `--scope` and output headers; anything else is
     `*`.
   - ⚠ **The cost, measured:** on BOTH runtimes, every session that runs a bare or `--repo` `cairn`
     command (597 such Claude Code calls and 24 opencode calls on this host, Q16) becomes owner-only, and with more than one instance configured it is HELD on its
     host (decision 16). Q16 gives the operator the remedies.
   - **What can go wrong, and in which direction.** A forged trailer or a quoted header in prose
     ADDS a scope, hiding `s` from more people; a header can no longer remove anything, so a
     `cat`-ed header or a hook attachment only adds. For a command line that names the program in
     any form the recognition rule lists, a line the parser cannot decompose adds `*`, so an
     unrecognised SHAPE fails closed. *Revision 7 claimed "a mis-parsed command ADDS a scope … for
     every remaining path"; with no recognition rule, an unrecognised visible call added nothing,
     so that claim was false and is retracted — the true claim is the fail-closed rule above.* The
     unsafe residual is a read or write NO signal sees: an invocation whose program name never
     appears in the command line (run through a variable such as `"$BIN" put …`, an alias the rule
     does not list, a shell function, or a script), a `put`/`create` from such a script, a write to the
     other instance. That residual is threat T3.
   - **Unknown names fail closed.** A scope name in `V` that the control model does not know
     (renamed, deleted, on another instance) and the sentinel `*` are unreadable by everyone but the
     owner.
   - **Cost.** `R_header`, `U_calls` and `D` are computed once per upload and stored in
     `meta.json` (grow-only). `W_trailer` changes whenever anyone writes a trailer, so it is a
     whole-store walk at view time — unmeasured for this design. S4 benchmarks it beside
     `BenchmarkSessionPageAndScopeTabs`; if it costs more than the session page's own walk, S4 caches
     it per session keyed on a store-change signal, which S4 must identify and name (none is
     designated here).

4. **ONE predicate: `transcript.Visible(viewer, meta, V) bool`**, asked by every surface — the
   transcript section, the agent read API (decision 17), the raw-record view, plugin output
   rendering, edge rendering and "My sessions". True iff EITHER
   - the **owner arm**: the viewer is UN-narrowed AND its `(Kind, ID)` equals the owner's; OR
   - the **scope arm**: `V` is non-empty, contains no unknown name or `*`, and the viewer's
     authorization (`control.Resolve`, narrowed if the credential is) `Allows(scope, read)` for
     EVERY scope in `V`.
   An empty `V` is owner-only (clause (g)). A narrowed credential of the owner gets only the scope
   arm — so a narrowed agent credential can read a session whose every scope it was narrowed to, and
   never an owner-only one. Every miss renders the SAME bytes as "this session has no transcript",
   and nothing says which scope hid it. *Revision 1 refused every narrowed credential outright;
   `Narrow` already intersects scopes, so only the owner arm needed the un-narrowed requirement.*
   On `/session`, the owner arm also FINDS a page that `report.SessionAcross` would not (an O10
   session with no write): the page then shows the transcript section alone.

5. **The capture agent is a NEW stdlib-only binary, `cmd/cairn-capture`, added to the import ban's
   `LinkedBinaryRoots`** — not a verb of `cmd/cairn`.
   - *Why not a `cairn` verb:* `cmd/cairn` is `packages.default`, installed wherever cairn is read;
     a verb that reads every transcript on the machine widens what that binary is for, and it would
     need the capture token where today it holds only a reader's credential. A separate binary is
     least privilege. (Reading stored transcripts IS a `cairn` verb — decision 17.)
   - *Readers (every byte, O9):* Claude Code JSONL by byte offset, complete lines only (a partial
     last line waits for the next run); every subagent file and its `agent-<id>.json` as child
     streams; every persisted `tool-results/*` file as a BLOB of its stream (redacted, or withheld
     if it cannot be — decision 6a). opencode through
     `opencode export <id>` as a subprocess by bare name (the `gitMinimal`-on-`PATH` precedent for
     `git`), diffing parts by `(id, time_updated)` against local state; it never opens the SQLite
     file, so the `account`/`credential` tables are structurally out of reach. *Revision 1 excluded
     persisted binaries and pre-decided Q2; that is withdrawn. What remains open is ONLY whether an
     unredactable binary may leave the host (Q2); until it is answered, decision 6a withholds it.*
   - *State:* a local 0600 watermark file per instance — `{stream → offset | part-version set}` —
     advanced only after the pod acknowledges.
   - *Cadence:* a user timer (`OnStartupSec` + `OnUnitActiveSec = 60s` + `AccuracySec = 1s`, the
     presence plan's measured lesson that `OnUnitActiveSec` alone never fires). Idle sessions cost
     one `stat` per file.
   - *Modes:* `--dry-run` prints per-session record and redaction COUNTS and the computed `V`, never
     content; `--self-test` runs the redaction corpus (closing condition part 3); `-verbs` prints its
     own ledger for a nix check.

6. **Redaction: a rule table applied on the host, RE-CHECKED (refusing) on the pod, measured on a
   realistic synthetic corpus.**
   - *Where it runs:* on decoded JSON STRING VALUES, recursively, then re-encoded — never on raw
     bytes (an escaped secret is different bytes; R4). A match is replaced by
     `[redacted:<rule>:<tag>]` where `<tag>` is the first 8 hex of **HMAC-SHA256 keyed by a per-host
     secret** (a 0600 file the agent creates on first run). Two occurrences of one secret on one host
     stay recognisably the same; a reader without the host key cannot confirm a guessed low-entropy
     secret by hashing it. *Revision 1 used an unsalted digest prefix, which allowed exactly that
     confirmation.*
   - *The rules:* `internal/redact`, stdlib `regexp` (RE2), its OWN table — seeded from leakscan's
     `credential` alternatives and extended for R4's shapes: provider API-key prefixes, JWTs,
     URL/DSN userinfo passwords, dotenv `KEY=value` where KEY names a secret, PEM blocks, and a
     STRUCTURAL `Secret` rule: in a text that carries a `kind: Secret` line (YAML) or a
     `"kind": "Secret"` member (JSON), every value nested under a `data` or `stringData` key. The YAML
     half is LINE-based (indentation under the key), because the Go standard library has no YAML
     parser and `cmd/cairn-capture` is under the import ban; the JSON half uses `encoding/json`.
     *Revision 3 said a blob that "parses as a YAML … document" gets the structural rules; nothing
     in a stdlib-only binary can parse YAML, so that wording is retracted.* **Residual:** the
     line-based rule reads BLOCK style only; a flow-style mapping (`stringData: {password: …}` on one
     line) is not recognised by it, and its values are caught only if another rule matches them.
     Plus a **per-host denylist** of literal strings and file-path globs in a local 0600 file —
     never in the repo. **Not shared with `tests/leakscan.py`** (D5): different predicates (a public
     tree vs a session), two regex engines. One BEHAVIOURAL containment test pins the relation:
     every realistic `credential` control string in leakscan's self-test is also caught by
     `internal/redact` (fails if a leakscan control is added that redact misses).
   - *Base64 that hides text:* a WHOLE string of ≥ 16 base64-alphabet characters (after removing
     ASCII whitespace — see 6a's decoder) that DECODES to text (6a's text rule) is scanned decoded as
     well; a match replaces the whole encoded value. The floor buys no precision — the decoded scan
     redacts only on a rule match — so it is set low: 16 characters is 12 decoded bytes, and the only
     values shorter than that which a rule recognises are tiny dotenv lines (e.g. `TOKEN=abcd`, 10
     characters, base64'd unpadded is 14; a 4-character value alone is 6), which pass this decoded
     scan. *Revision 6 said a 4-character value encodes to 14 characters; it is 6 — corrected.* **Residual:** an encoded secret EMBEDDED in a longer
     string (not the whole value) is not decoded. *Revision 4 set the floor at 64 characters, which a
     base64-encoded 40-character token (56 characters) slipped under, and claimed that without the
     rule such a secret "passes every rule" as if the rule closed the case; both are retracted.*
   - *The pod re-checks, and REFUSES* (D4, kept): every received record's decoded strings AND every
     received text blob are scanned with the same table; a match is refused 422 (record index or
     blob name given, value not) and nothing in that request is stored. Kept as a refusal, not count-and-log, because storing a string the table already
     recognises as a secret is the harm the table exists to prevent, and the check costs one scan.
     ⚠ It catches a bypassed or OUT-OF-DATE agent (a host whose table is older than the pod's then
     holds that stream and logs, rather than losing it), NOT a miss: the same table cannot catch
     what it does not recognise. Clause (b) is measured on the agent's outgoing bytes precisely so
     this refusal cannot mask it.
   - *Measurement (closing condition part 3):* a generator emits synthetic transcripts in BOTH
     formats with **P** secrets planted in realistic positions — a shell tool's stdout of a dotenv
     file, `toolUseResult.originalFile`, an edit's `oldString`, a JSON-escaped value, a Kubernetes
     `Secret` manifest, an opencode tool `state.output`, a DSN in a command line, a prompt string, a
     bookkeeping `queue-operation` content string, a subagent's tool output, and a persisted TEXT
     tool-result BLOB (a dotenv dump and a `Secret` manifest as `.txt` files). 🔴 **The planted values
     are GENERATED AT RUN TIME from a seeded RNG**, never committed: a committed credential-shaped
     fixture is itself a `leakscan` finding (and should be). The report is the pair "planted=P
     caught=P" plus `clean-damaged=0` on a clean corpus of UUIDs, commit SHAs, digests and base64
     payloads (a redactor that eats every hash makes transcripts unreadable; that is a failure too).
     A textbook example key is NOT in the corpus.

6a. **Blobs and binary content: text is redacted, unredactable binary is WITHHELD until Q2 —
   decided BY CONTENT, not by field name.**
   - **The text rule:** bytes are TEXT when there is no NUL byte in the first 8,000 bytes AND the
     WHOLE value is valid UTF-8. The NUL half is leakscan's (git's) sniff
     (`tests/leakscan.py:368, 467-471`); **the UTF-8 half is this plan's own** — leakscan has no
     UTF-8 test. *Revision 3 said "one definition of binary governs both"; that was false and is
     retracted.* Consequence, stated: a UTF-16 or Latin-1 TEXT file is "binary" here and is
     WITHHELD, not redacted — Q2 names it.
   - **Text blobs.** A blob that parses as JSON (or JSON Lines) is redacted by decision 6's
     DECODED-string traversal, exactly like a record, so a JSON-escaped secret inside a blob is
     caught; if nothing matches, the ORIGINAL bytes are stored, otherwise the re-encoded document.
     *Revision 3 redacted every text blob as one raw string, which re-opened decision 6's
     JSON-escape gap for blobs; retracted.* Any other text blob is redacted as ONE string with the
     same table, keyed tags and the line-based `Secret` rule, and stored as the redacted bytes —
     byte-identical to its source exactly when nothing matched. The pod re-checks both kinds like a
     record (decision 6).
   - **Binary content is recognised BY CONTENT wherever it sits**, during the same traversal that
     redacts strings — not by a list of fields: a decoded string is withheld when the WHOLE string
     is a `data:` URL whose payload is not text, or the WHOLE string is a ≥ 64-character base64
     value whose decoded bytes BEGIN WITH A KNOWN FILE SIGNATURE (PNG, JPEG, GIF, WebP, PDF, ZIP — a
     closed, tested list). **The decoder:** Go's `encoding/base64`, trying `StdEncoding`,
     `RawStdEncoding` (unpadded), `URLEncoding` and `RawURLEncoding` in that order, after removing
     ASCII whitespace from the string — so padded, unpadded, URL-safe and line-wrapped encodings of
     a whole string are all decoded. "Decodes to non-text" alone is deliberately NOT the rule:
     thinking `signature` values (≈5% of bytes, R1) and 64-hex digests are base64-alphabet strings
     that decode to arbitrary bytes, and withholding them would destroy content that is neither an
     image nor a secret.
   - **Residuals, stated rather than closed** (each ships TEXT-SCANNED ONLY, i.e. redacted by the
     text rules but not withheld): (1) a binary payload in an unlisted format, base64-encoded with
     no `data:` prefix; (2) a listed-format payload EMBEDDED in a longer string rather than being the
     whole string — e.g. a shell tool's `base64` output preceded by other text, HTML carrying an
     inline `data:image/…` URL. The round-4 audit measured, on the one host, 254 whole-string hits
     (the 127 inline image payloads' and 127 `file.base64` duplicates' — its positive control), 3
     embedded signature-prefixed base64 runs of 200+ characters, and 6 embedded `data:image` /
     `data:application` URLs. Scanning inside strings for signature-prefixed runs was considered and
     NOT adopted: it is a second, fuzzier matcher over every string, for 9 measured instances; Q2
     names the residual so the operator can ask for it.
     The carriers measured on the one host (R1, R3), all caught by the whole-string rule rather than
     by name:
     - an inline `image` block's base64 payload inside a `user` record (68 blocks in the sample);
     - **its DUPLICATE**, `toolUseResult.file.base64` on the same tool result (127 records in the
       300 newest top-level files, in 26 of them);
     - opencode `tool` parts' `state.attachments[].url` (107 attachments measured, 107 of them
       `data:` URLs);
     - a non-text persisted tool-result blob (`.pdf`, `.jpg`, …).
     *Revision 3 named only the first and the last; the middle two would have been text-redacted
     and SHIPPED — silently answering Q2 "ship" for the commonest image path. Retracted.*
   - **What WITHHELD means.** Until the operator answers Q2, the agent sends, in place of the value
     (or the blob), `{"withheld": "unredactable-binary", "media_type": …, "bytes": N, "tag": <keyed
     digest>}`, and the page and the skeleton say "withheld: binary, N bytes". ⚠ **This narrows
     O9's "every byte"**, and it is ONE of TWO places the plan does — the other is decision 16's
     hold, under which a held session is stored on no instance. *Revision 3
     called this "the one place the plan stores less than every byte"; retracted.* Withholding
     exists only because two operator requirements (every byte, redacted before upload) cannot both
     hold for content no redactor can read. If Q2's answer is "ship", the placeholder is removed for
     the media types Q2 names and nothing else moves.

7. **Sidechains, subagents and child sessions are CHILD STREAMS of one root session.**
   - Claude Code: a subagent file shares the parent's `sessionId` (R2), so it is stream
     `subagent:<agentId>` of that session, carrying its metadata file's keys (the subagent tree is
     rebuilt from `parentAgentId` and `spawnDepth`).
   - opencode: a child session has its own id and a `parent_id` (R3); it is uploaded as stream
     `child:<ses_id>` of its ROOT, and its own id is recorded so trailers naming the child count
     toward the root's `V`.
   - **Visibility is per ROOT**: `V(root)` is the union over every stream, and the whole tree is
     visible or none of it — a child is never visible where its root is not.

8. **The capture and plugin APIs live on ONE NEW listener, `-worker-addr`, with its own route
   ledger** — not on the presence listener (which keeps its single-owner wall and its two routes
   unchanged; nothing presence-shaped migrates). It applies `cairn-ui`'s reachable-bind /
   trusted-proxy refusal to its own bind and the `netid` failed-token lockout, exactly as the
   presence listener does (`cmd/cairn-ui/main.go:537-563`; `internal/presence/agent.go:189-203`).

9. **One plugin contract, used by BOTH examples** (so the first two plugins test the abstraction):
   - **Manifest** (committed beside each plugin; registered on the pod by the operator through
     `-plugin-registry <file>`, no default — registering a plugin IS the instance-level decision):
     `schema`, `name`, `version`, `description`, `capabilities` (a closed in-tree vocabulary:
     `pending:list`, `transcript:read`, `output:read:<type>`, `output:write:<type>`), `toggle_key`
     (the plugin name), `output_types` (each a member of a CLOSED in-tree set: `summary`,
     `ticket-edge`, `ticket-suggestion`), `calls_model` (bool, rendered on `/plugins`).
   - **Output envelope**, pod-validated: `{plugin, plugin_version, model, session, input_watermark:
     {stream → seq}, type, body}`; the pod stamps `produced_at` and the token's plugin name (a plugin
     cannot claim to be another). Bodies are validated against the in-tree schema for their type
     with `DisallowUnknownFields` and size bounds.
   - **Why the output TYPES are in-tree and closed:** the renderer must know each type to render it
     safely; a plugin that needs a new type needs a reviewed cairn change, while a new plugin
     producing existing types needs only a registry entry.
   - A capability not in the manifest, or an output type not in the plugin's `output_types`, is
     refused 403 with one uniform body — clause (j).

10. **Toggles: default OFF, set per SCOPE and per ARC (O5), and every finer level can only
    NARROW.** *Revision 2 replaces revision 1's fold, under which an arc or session ON overrode a
    scope OFF — and arc membership is whatever an arc `PUT` names, with no admin needed.*
    - **Scope toggle** — three states, set by a principal with `admin` on that scope
      (`control.Resolve`): `off` (default, and the value of every unknown name and of `*`),
      `allowed` (the scope's admins consent; nothing runs because of it alone), `on` (consent AND
      run).
    - **Arc toggle** — `unset` (default), `on`, `off`, set by `admin` on the arc's HOME scope.
    - **Session opt-out** — the owner may turn a plugin OFF for one session. OFF only: a
      session-level ON is refused 400, so it can never enable anything. *Kept because scope consent
      is per SCOPE: when a session's every scope is ON, its owner has no other way to keep ONE
      session — say one that handled something sensitive — away from a plugin that sends content to
      a third party (T7).* *Revision 2's reason — that an owner-only session "has no scope at which to
      say no" — is retracted: under this fold an empty `V`, `*` and unknown names already resolve
      OFF, so the opt-out does nothing for owner-only sessions.*
    - **Effective value for (plugin, session):** ON iff (1) EVERY scope in `V(root)` is `allowed`
      or `on` — the consent AND, applied regardless of every other level; AND (2) EITHER every scope
      in `V` is `on`, OR some arc listing the session is `on`; AND (3) no arc listing the session is
      `off`; AND (4) the owner has not opted out. An empty `V` resolves OFF. So an arc can enable a
      plugin only inside scopes whose admins already consented, and an arc `PUT` that names a
      session cannot widen anything.
    - **REMOVED:** the instance level and `-plugin-admin` (D3) — registering the plugin is the
      instance-level act, and O5 names scope and arc.
    - **Journal:** `-plugin-journal <file>`, the arc journal's write discipline (flock, one
      `O_APPEND` write, fsync, refused inside the store root), records
      `{plugin, level, target, value, by: (Kind, ID), at: pod clock}`. The fold is last-write-wins
      per `(plugin, level, target)`; history is the journal.

11. **Plugin output is a CLAIM and is rendered as one.** Every output renders inside a labelled
    "derived" container stating plugin name and version, model, `produced_at`, and the watermark
    ("from turns 1–N of M; M−N newer turns not yet summarised" when the transcript has grown). Text
    is `g.Text` only — no Markdown, no links built from model output except a ClickUp URL built by
    the existing `refurl` template from a VALIDATED canonical id (decision 14). A suggestion renders
    with its confidence and the word "suggested"; an edge renders its signal ("branch name", "PR
    body", …).

12. **REMOVED (D1) — the pod-side budget window.** The pod never sees the LLM key, so a pod-side cap
    was cooperative only and could not stop a plugin that ignored it. The ceilings that actually
    hold are the plugin's OWN cap (S9's test: cap 0 → the fake provider receives 0 requests) and a
    spending limit set at the provider. Threat T7 says so.

13. **Continuous = incremental, by a STATELESS query.** *Revision 1's leases and acks are REMOVED
    (D2).* `GET /plugin/v1/pending?limit=N` returns sessions where the plugin is ON (decision 10),
    the transcript's newest `seq` exceeds the plugin's latest output watermark for that session, and
    the session has been idle ≥ 10 minutes OR grown by ≥ 200 records since that watermark (defaults,
    registry-configurable). Each item carries the previous output's id and watermark, so the plugin
    summarises only the new records plus its own prior summary; writing the new output IS what
    advances the watermark. ⚠ It assumes ONE worker per plugin: two would receive the same items and
    duplicate work (harmless, costly). There is no "session ended" signal in either runtime's
    storage (R1–R3), so idleness is the trigger.

14. **ClickUp matches are EDGES with PROVENANCE; suggestions are a different type.**
    - `ticket-edge {system: "clickup", ticket_id, custom_id?, signal, locator}` where `signal` ∈
      `{branch-name, commit-message, commit-trailer, pr-body, pr-link, transcript-url, entry-ref}`
      and `locator` names WHERE (`{stream, seq}` for a transcript record; a commit SHA; a PR number;
      an entry ref). Deterministic signals only: a ClickUp task URL in either form (R5), an
      explicit `clickup:<id>` ref (`internal/store/entry.go:486, 570`) on an entry the session
      wrote, or a CUSTOM id that is a member of the workspace's own closed list fetched by the
      plugin. A bare token that merely LOOKS like an id is never matched.
    - **The link is always the canonical id.** The plugin resolves a custom id (or a
      `/t/<workspace>/<custom>` URL) to the task's canonical id from the list it fetched, and stores
      that as `ticket_id`; the page links it through the existing `clickup:<id>` template
      (`refurl.go:82-92`, which refuses `/`), and shows `custom_id` as text. A custom id the list
      does not resolve produces no edge.
    - `ticket-suggestion {ticket_id, confidence: 0.0–1.0, rationale (≤ 280 chars)}` — LLM-produced,
      rendered as a suggestion, never promoted to an edge by the plugin.
    - **What ClickUp material is stored:** the ticket id (and custom id) only by default; the ticket
      TITLE only when the instance sets a no-default `-plugin-store-ticket-titles` (Q8).
      Descriptions, comments and assignees are never stored. Edges and suggestions are visible
      exactly when their session is (decision 4).

15. **Storage: every byte that is SHIPPED, in a per-session DIRECTORY under `-transcript-dir`**, no
    default, refused inside the store root (the `internal/arcs/path.go:30-90` rule), 0700/0600.
    "Every byte" here excludes exactly two things, both decided elsewhere: unredactable binary
    content, withheld pending Q2 (decision 6a), and sessions held on their host by decision 16 (for
    any of the reasons the Goal lists).
    - `<dir>/<root-session>/meta.json` (owner, host, runtime, the grow-only `R_header`/`U_calls`/`D`
      sets, child ids, created); `stream-<name>.jsonl.gz` segments, append-only, each record
      `{seq, src, rec}` where `src` is the source offset or part version and `rec` the redacted raw
      record; `blobs/` for persisted tool-result files; `outputs.jsonl` for plugin outputs. The
      server assigns `seq` per stream, so ONE watermark shape serves both runtimes.
    - **Nothing is truncated at storage** (O9). *Revision 1's per-record 1 MiB truncation and
      per-session cap are REMOVED*: the measured p99 root session is 72 MB and the max 154 MB, so a
      per-session cap would have cut exactly the large sessions. Size limits exist only at READ time
      (decisions 17, 18). A request body is bounded; a record larger than one request is sent as
      numbered continuation frames and reassembled before it is stored.
    - **Ownership of a session id:** the first accepted upload of a root id fixes `(owner, host)`.
      An upload of the same id by any other owner or host is refused 409 with ONE uniform body that
      names neither, and nothing is written. ⚠ A capture-token holder who learns another owner's
      session id (ids are on session pages) could SQUAT it first; the single capture owner per
      instance (Q7) removes that case.
    - **Deletion is `rm -r` of one directory** (plus the journaled fact of the deletion, no
      content) — the reason it is not one shared append-only journal.
    - **The disk is protected by an instance QUOTA and REQUIRED retention**, not by refusing large
      sessions: `-transcript-retention` has no default and is required when armed (nobody gets "keep
      forever" by omission); `-transcript-quota` refuses NEW uploads (507, named) when reached, and
      the agent HOLDS and retries — bytes are delayed, never dropped.
    - *Why not pgstore:* `-db-dsn` is optional on `cairn-ui`, the volume is GB/week (R1), and a
      schema migration turns a UI rollback into an outage (`migrate.go:166-173`). Q12 keeps the
      option open.

16. **Where a transcript is sent: the instance that routes EVERY scope in its `V` — writes AND
    reads (O8).** The capture agent computes `V` with the same `scopeuse` parser (decision 3) and
    resolves instances with `internal/client`'s routing (stdlib, already Go). The rule is evaluated
    on EVERY run, over `V` as it stands at that run (revision 6's pause while a pending set was
    non-empty is DELETED with that set, decision 3):
    - every scope routes to instance A → shipped to A only (so a session that only READ
      `beta-notes`, routed to the client instance, ships to the client instance — that is not a
      hold);
    - scopes route to two instances → **held**, logged. THE fixture (clause (h), S2's routing test):
      wrote `alpha-notes` (personal) and read `beta-notes` (client) in its first turn → held, 0
      requests to either;
    - `V` holds `*` or an unroutable name with more than one instance configured → held;
    - `V` empty (O10) → the DEFAULT instance, where decision 4 makes it owner-only.
    - 🔴 **Capture is incremental (60 s), so a prefix may ALREADY be on an instance when the rule
      changes its answer.** When a run computes an answer other than the instance holding the
      prefix, the agent: (1) stops shipping that session there; (2) sends
      `POST /capture/v1/sessions/{root}/withdraw` to the holding instance with that session's capture
      token, which runs the owner-deletion cascade (directory, outputs, edges — S6) and journals the
      withdrawal (who, when, no content); (3) acts on the NEW answer:
      - **a single other instance B** → re-ships the WHOLE session to B **from offset 0** (every
        stream, every blob), with fresh watermarks. Example: run 1 sees `V` empty and ships to the
        default (personal) instance; run 2 sees `V = {beta-notes}` (client) → withdraw from personal,
        re-ship to client from the start. *Revision 3 held such a session forever, which
        contradicted its own "a session that only READ `beta-notes` ships to the client instance";
        retracted.*
      - **held** (two instances, or `*` with several configured) → kept on the host only. Because `V`
        only grows, a held session stays held.
      The withdrawal answer is `{"deleted": true|false}` (contracts): `true` when the caller's own
      `(owner, host)` holds that root now (it is deleted) OR already withdrew it (the withdrawal is
      journaled with who and when, so a repeat is recognisable). That makes the call idempotent: a
      lost acknowledgement followed by a retry answers `true` again. *Revision 4 answered `false` to
      the retry — the root was gone — and treated `false` as final, so a timed-out response turned a
      successful withdrawal into a permanent hold; retracted.* The agent retries a FAILED request;
      a `false` answer is final and is logged loudly ("prefix not withdrawn: not held by this owner and host" — e.g. the
      token was reissued under another host label), and the session stays held rather than being
      re-shipped, so the prefix's presence elsewhere is surfaced instead of duplicated. Clause (m).
    - ⚠ **Re-shipping from offset 0 needs the source.** If the runtime has already deleted part of a
      session's files on the host, the re-shipped copy is what remains, and the agent logs the
      shortfall; it never re-sends bytes it no longer has.
    - **What that leaves, stated:** between the prefix shipping and the withdrawal, the prefix sat on
      the personal instance — but it was shipped BEFORE the read, so it held no `beta-notes` content
      that cairn could see, and it was visible under its OWN `V` then. Plugin outputs already made
      from it are deleted by the cascade; content already SENT to an LLM provider by a plugin cannot
      be recalled (T7). And a session's prompts and code about client work that never touched cairn
      are not a cairn read, so they never move `V`: such a session goes to the default instance,
      owner-only (T11).
    *Revision 2 said such sessions are "never shipped"; under 60 s incremental capture that was
    false, and the withdrawal is what makes the rule hold after the fact.* Q14 asks the operator to
    choose between this and the alternatives.

17. **Selective reading for agents (O9): ONE read model, served as JSON on `cairn-ui` and as Go-only
    `cairn transcript` verbs.**
    - **One classification table** in `internal/transcript` classifies UNITS, not records — because
      a record is not one kind of thing: a `tool_result` block (up to 656 KB, R1) lives INSIDE a
      `user` record, and the duplicate `toolUseResult` is a FIELD of that record, not a record.
      *Revision 2 classified by record type; under that, "user messages are shown in full"
      rendered every tool result in full, and "omit duplicate records" had no record to omit. Both
      are retracted.* The units and their classes:
      - **Claude Code** — each content BLOCK of a `user`/`assistant` record; the `toolUseResult`
        FIELD; every other record type as a whole. Classes: `human-text` (a `text` block, or string
        content, of a `user` record that has no `tool_result` block, no `toolUseResult`, and is not
        `isMeta` or `isCompactSummary`); `assistant-text`; `thinking`; `tool-call` (`tool_use`);
        `tool-result` (`tool_result` block); `duplicate` (the `toolUseResult` field); `runtime`
        (`isCompactSummary`, `isMeta` content, `system` records such as `compact_boundary` and
        `away_summary`); `bookkeeping` (the record types of R1 that are neither — `attachment`,
        `queue-operation`, `mode`, …); `binary` (an inline `image` block, and the
        `toolUseResult.file.base64` value inside the duplicate field — both withheld, decision 6a);
        `unknown`.
      - **opencode** — each PART: `text` of a `user` message → `human-text`; `text` of an
        `assistant` message → `assistant-text`; `reasoning` → `thinking`; `tool` → `tool-call` +
        `tool-result` (its `state.input` / `state.output`) + one `binary` unit per
        `state.attachments[]` entry (withheld, decision 6a); `step-start`/`step-finish`/`patch` →
        `bookkeeping`; `compaction` → `runtime`; `subtask` → a child-stream link; anything else →
        `unknown`.
      - The `binary` class is a DISPLAY label; WHAT is withheld is decided by 6a's content rule, so
        a binary value in an unlisted position is still withheld (and classed by its position).
      - **A "user message" is a `human-text` unit and nothing else.** Unknown units are SHOWN in raw
        views and COUNTED in the skeleton, never silently dropped; S0's shape ledger pins the table
        against the fixture's record types, block types and field names.
    - **Skeleton** — `GET /transcript/skeleton?session=` → the stream tree (root, subagents with
      `agentType`/`description`/`spawnDepth`, opencode children), per stream: record counts and
      bytes by class, compaction boundaries, and a TURN INDEX `[{turn, seq_from, seq_to,
      user_bytes, assistant_bytes, tool_calls: [{seq, tool, input_bytes, result_bytes, is_error,
      child_stream?}]}]`. No content beyond tool NAMES.
    - **Records** — `GET /transcript/records?session=&stream=&from_seq=&to_seq=&class=&role=
      &tool=&view=conversation|raw&max_bytes=` → a page bounded by `max_bytes` (default 64 KiB,
      ceiling 1 MiB) with `next_seq`. `view=conversation` (the DEFAULT) drops `bookkeeping`
      records, strips the `duplicate` FIELD from the records that carry it, and leaves a one-line
      placeholder naming the class and size of each thing removed; `view=raw` returns the stored
      records byte-exact.
    - **One tool call** — `GET /transcript/tool?session=&stream=&seq=&range=<start>-<end>` → the
      call, its result and its duplicate, with a byte range for long outputs.
    - **Every route asks decision 4's predicate first** and answers every miss with one 404 body.
      They are GET rows, so bearer reads pass both cross-site gates' scope by method; a narrowed
      agent credential reads exactly the sessions its narrowing covers (decision 4).
    - **The CLI:** `cairn transcript skeleton|records|tool` mirror the three routes. They talk to
      `cairn-ui`, not the pod, through ONE new variable, `CAIRN_UI_URL`, whose value is set per
      instance in each instance's env file the way `CAIRN_URL` is (`internal/client/transport.go:128-162`;
      no old alias, so the env ledger does not move). They are **Go-only verbs** — declared in `capability_ledger` `go_only`
      and `want-go-only-verbs.txt`, as `sessions` and `arcs` are — so the parity corpus compares
      nothing new. Q15 asks whether the operator prefers a separate reader binary instead.

18. **The UI renders a COLLAPSED reading of every byte (O3, O9) with no new script.**
    - **User messages — `human-text` units (decision 17) — are ALWAYS shown in full**, never
      truncated, whatever their length. A `tool_result` block inside the same `user` record is NOT a
      user message and renders as a collapsed tool result (below); the runtime's compaction summary
      and `isMeta` content are `runtime` units and render as labelled, collapsed blocks.
    - **Assistant text** longer than 1,200 characters or 15 lines renders its head, then
      `<details><summary>read more (N more characters)</summary>…the rest…</details>`. `<details>`
      needs no script, so `AllowedScriptSources` keeps its one entry.
    - **Tool calls** render collapsed: `<details><summary>Bash · exit 1 · 3.2 KB</summary>` with the
      input and the result inside; a result longer than 64 KiB shows its first 64 KiB and links to
      the raw-record view for the rest (a DISPLAY limit; storage is untouched).
    - **Subagents** render as a nested, collapsed `<details>` at the tool call that spawned them,
      summarised by `agentType` and `description`; opencode children likewise at their `subtask`
      part.
    - **Bookkeeping records and duplicate FIELDS** collapse into ONE `<details>` per turn ("N
      bookkeeping records, K duplicate tool-result copies") with a link to `view=raw`.
    - **Withheld binary content** (decision 6a) renders as one line naming its type and size.
    - **Pages** are bounded by turns (`?turn=` paging, 50 turns per page) so a 154 MB session never
      renders in one response.

## The contracts

```
# capture — worker listener, capture token (owner, host)
POST {worker}/capture/v1/sessions/{root}/streams/{stream}/records
  body: {"schema":1, "runtime":"claude|opencode", "host":"host-a",
         "from":"<offset or version cursor>", "to":"<…>",
         "declared_scopes":["alpha-notes"],          # grows only
         "records":[{"src":"<offset|part-id@ms>", "rec":{…redacted raw record…}}],
         "frames":{"index":i, "count":n}?}          # only for a record split across requests
  200 {"stored_to":"<…>", "seq_to":N}
  409 {"stored_to":"<…>"}       # `from` ≠ stored position — the agent resumes from there
  409 uniform                    # root id owned by another (owner, host) — names neither
  422 a record matched the rule table (index named, value not); nothing stored
  507 instance quota reached (named)     400 malformed / host ≠ token's host / unknown field
  401 uniform
POST {worker}/capture/v1/sessions/{root}/blobs/{name}   # a persisted tool-result file: redacted
                                                         # text, or the withheld placeholder (6a);
                                                         # same CAS, frames, 409/422/507 rules
POST {worker}/capture/v1/sessions/{root}/withdraw       # decision 16: runs the deletion cascade
  200 {"deleted": true}       # this token's own (owner, host) held the root (now deleted) OR
                              # already withdrew it (journaled) — idempotent across a lost ack
  200 {"deleted": false}      # held by another owner, by the same owner on another host, or by
                              # nobody and never withdrawn by this caller — ONE answer for all
                              # three, so no oracle (a `true` reveals only the caller's own root)
  # Revision 3 answered a uniform {"withdrawn": true} in every case, which made the agent's "retry
  # until acknowledged" unable to tell a deletion from a no-op; retracted.

# plugin — worker listener, plugin token (name)
GET  {worker}/plugin/v1/pending?limit=10
  200 {"items":[{"session":"s-0001", "streams":{"main":{"seq_to":N}},
        "prev_output":{"id":"o-…","watermark":{"main":M}}|null}]}
GET  {worker}/plugin/v1/transcript?session=s-0001&stream=main&from_seq=M&max_bytes=…&view=…
  200 {"records":[{"seq":…, "rec":{…}}], "next_seq":…}  # only when ON for this session
GET  {worker}/plugin/v1/outputs?session=s-0001&type=summary   # needs output:read:summary
POST {worker}/plugin/v1/outputs          {envelope, decision 9}  → 201 {"id":"o-…"}
  403 uniform for every capability, toggle or type refusal     401 uniform

# agent reads — BROWSER listener, the reader's own credential, decision 4's predicate
GET /transcript/skeleton  ·  GET /transcript/records  ·  GET /transcript/tool   (decision 17)
```

## Threat model

| threat | control |
|---|---|
| **T1. A secret survives redaction and is stored** | The residual the operator accepted by choosing every byte (O1, O9). Controls: host-side redaction on decoded strings with a keyed tag (decision 6), the pod's refusing re-check (clause c), the realistic corpus (closing condition 3), per-host denylist, retention, per-session deletion. **What is NOT controlled:** unshaped secrets (typed passwords, novel token formats), secrets inside images/PDFs (withheld rather than stored until Q2 is answered, decision 6a — if Q2 says "ship", this becomes an uncontrolled residual) EXCEPT binary payloads embedded in longer strings or in unlisted formats, which ship text-scanned only (6a's residuals), an encoded secret embedded in a longer string (not decoded), a flow-style YAML `Secret` (decision 6), and anything stored BEFORE a rule existed — a rule added later does not rewrite stored records (B3 proposes a re-scan). |
| **T2. Confidential but non-secret content** (client business detail, personal data in a tool output) | Redaction does not address it at all; VISIBILITY is the only control (decision 4). Stated, so nobody believes the redactor covers it. |
| **T3. Under-counted `V` widens visibility** | `V` is writes over the whole store plus reads from rendered headers anywhere in the content plus command-line scopes plus declarations, re-derived on the pod, grow-only (decision 3); a header naming no scope, `--all-scopes`, and EVERY VISIBLE cwd-derived `cairn` invocation (no explicit `--scope`) add `*`, unconditionally, on the pod and on the agent, as does any command line naming the program (a `cairn`/`subsystem-recall` token, a `…/cairn` path, a `#cairn` flake ref) that the parser cannot decompose into invocations each with an explicit `--scope`; a hook-run read adds only its header's scope — nothing cancels a `*`, and no caller resolves a working directory, so a `cd`, a stale record `cwd` or an opencode `bash` call's `workdir` argument cannot misroute or mis-scope a call; `*` and unknown names fail closed; empty `V` is owner-only (clause g); a session touching two instances is held and an already-shipped prefix withdrawn (decision 16, clauses h and m). **The residual:** a read or write NO signal sees — an invocation whose program name never appears in the command line (run through a variable such as `"$BIN" put …`, an unlisted alias, a shell function, or a script), or any `cairn` command run by a script whose command line the transcript does not show, when it is a header-less verb (`sessions`, `arcs`, `arc-show`, `ls-entries`) or a `put`/`create`. *Revisions 3–6 listed a second residual — a directory change making the AGENT's routing resolve the wrong scope; that resolution is deleted (decision 3), so the residual is gone with it.* |
| **T4. Viewer-set computation makes the predicate vacuous** | Clause (e), and a mutant row (`transcript-written-set-from-viewer-scopes`). |
| **T5. A stolen capture token** | Can APPEND to its owner's transcripts from its one host — inject fake records into the owner's own sessions — can SQUAT a not-yet-uploaded session id (decision 15), and can WITHDRAW (delete) its owner's sessions uploaded from that host (decision 16). Cannot read, or touch another owner's or host's existing sessions. Revoke by deleting the row (re-read per request). |
| **T6. A stolen plugin token** | Can read every transcript where that plugin is ON, and write outputs of its declared types. The largest single exposure the design creates; it is why toggles default OFF, require every scope's consent (decision 10), and why a plugin token is per plugin. |
| **T7. The LLM plugin sends transcripts to a third party** | By design: plugin A and B's LLM half send redacted records to a model provider. That egress is outside cairn once read; toggles are the consent, Q5 the gate for the client instance, the provider's data terms the residual. Spend is capped by the plugin itself and at the provider, NOT by the pod (decision 12, REMOVED). |
| **T8. Prompt injection through transcript content** | A transcript can contain text that instructs the summariser. Its blast radius is the plugin's OUTPUT: a misleading summary or a false suggestion. Controls: outputs are labelled derived (decision 11), suggestions never become edges, rendering is `g.Text` only, plugins have no write path except outputs. |
| **T9. XSS through raw records or model output** | Every rendered value through `g.Text`; the raw-node ban holds; a URL only through `safeHref` and the `refurl` template. Fixture: a record whose strings are hostile HTML, asserted escaped (the `TestHostileEntryTextIsEscaped` pattern). |
| **T10. Existence oracle** | The transcript section, the agent read routes, the raw view and output renders answer identically for absent, hidden and deleted; a plugin gets one 403 body for every refusal; the ownership collision 409 names nobody. |
| **T11. Cross-instance leakage** | Decision 16 on the host (reads included, O8), including withdrawal of a prefix shipped before the routing answer changed (clause m); per-instance worker tokens; the client instance arms nothing until Q5. **Residual:** a prefix sits on the first instance until the next agent run withdraws it (it predates the read, so it holds no content cairn saw from the other instance); client-related prompts and code that never touched cairn do not move `V` and land on the default instance, owner-only; content a plugin already sent to an LLM provider cannot be recalled. |
| **T12. Storage exhaustion** | Required retention, an instance quota that delays rather than drops (decision 15). |
| **T13. Committed fixtures leak** | Synthetic generator only; planted secrets generated at run time (decision 6); `leakscan` in CI; no captured text, ever (`AGENTS.md`). |
| **T14. Toggle tampering, including by arc membership** | Toggles are browser POSTs behind both gates, authorised per level (decision 10), journaled with `(Kind, ID)` and pod time; an arc can only enable inside scopes whose admins already consented, so naming a session in an arc `PUT` widens nothing. |
| **T15. Guess-confirmation of a redacted value** | The tag is keyed by a per-host secret (decision 6), so hashing a guess confirms nothing without that key. |

## Slices

Each slice is mergeable alone, leaves `main` releasable, and has tests that run on that slice
alone. None touches `internal/api` or `cmd/cairn-server`; only S8 touches `cmd/cairn` /
`internal/client` (Go-only verbs) — each slice's test plan re-asserts its own footprint.

| slice | what | ledgers it moves | mergeable alone because |
|---|---|---|---|
| **S0** | **Fixtures and shape ledgers.** `tests/transcripts/gen.py` emits synthetic sessions in BOTH formats from the measured key sets (R1–R3): main stream, two subagents, an opencode child, a compaction boundary, a `pr-link`, persisted tool output, mutated opencode parts, bookkeeping records and duplicate fields, a `user` record carrying a large `tool_result` block, an inline image block AND its `toolUseResult.file.base64` duplicate, an opencode `tool` part with a `data:` URL in `state.attachments[].url`, a binary, a UTF-8 text, a UTF-16 text and a JSON text tool-result blob, thinking `signature` values and 64-hex digests (which must survive untouched), and every read path of R7 (a rendered recall in a tool result; one in a hook attachment with no command line; a store-wide search rendered `scope=(all scopes)` in a hook attachment; a bare header-less `ls-entries`; an explicit `--scope` read; a `--repo` read; a `cd … && cairn recall` chain). A shape test pins the generator's record types, block types and field names against `internal/transcript`'s classification table. | `tests/`; `onlyGo` if Go tests read the fixtures; README. | Test-only. |
| **S1** | **`internal/redact`** — its own rule table (decision 6), decoded-string traversal, text-blob redaction and the text/binary sniff (decision 6a), keyed tags, structural Secret rule, denylist loader; the corpus generator and the `planted/caught/clean-damaged` report; the behavioural containment test against leakscan's controls. | new package; `ok` floor; README. | Library only. |
| **S2** | **`cmd/cairn-capture`** and **`internal/transcript/scopeuse`** — readers (JSONL by offset, subagents, blobs with binary withholding; `opencode export` diffing), `V` derivation (the `*` mappings of decision 3), watermark state, routing (decision 16), `--dry-run`, `--self-test`, `-verbs`. No upload yet. | `cmd/cairn-capture`; new packages; `./internal/transcript/scopeuse/` joins `control_mutants.py` `PKGS` (+ pinned count); `depspolicy.LinkedBinaryRoots` + its test; `flake.nix` `packages.cairn-capture` + a ledger check; `ok` floor; closing-condition part 3 step in `ci.yml`. | Inert: it sends nothing. |
| **S3** | **Transcript store + capture API.** `internal/transcript` (directory layout, CAS append, frames, ownership, quota, retention sweeper, deletion, pod-side `V` re-derivation), worker listener + ledger, `capture` token kind and `cairn-ui -issue-worker-token capture`, refusing pod re-check. Agent gains upload. `tests/plugins/e2e.sh` created with clauses (a), (b), (c), (h). | new package → `ok` floor, `control_mutants.py` `PKGS` (+ pinned count through `ci.yml` and `internal/control/README.md`); `cmd/cairn-ui` flags (`-worker-addr`, `-worker-tokens`, `-transcript-dir`, `-transcript-retention`, `-transcript-quota`) and tests; `ci.yml` e2e step. | Inert unless `-worker-addr` AND `-transcript-dir` are set. |
| **S4** | **Visibility + the session page shows content (O3).** `transcript.Visible` (decision 4) with `V` (decision 3); the collapsed transcript section on `/session` (decision 18); the owner arm finding O10 sessions; `GET /session/transcript` raw-record view. e2e clauses (d), (e), (f), (g — the visibility half). Benchmark of the whole-store `W_trailer` walk. | UI rows → hand ledger, `contentAuthority`, uiaudit targets + a synthetic transcript in the uiaudit world; mutant rows. | Read-only over S3; renders nothing when no transcript exists. |
| **S5** | **Plugin registry, toggles, plugin API.** `internal/plugins` (manifest validation, closed capability and output-type vocabularies, plugin token kind, the narrowing-only toggle fold + `-plugin-journal`, the stateless pending query); `/plugins` page + toggle POSTs on scope, arc and session pages. e2e clauses (i), (j). | new package → `ok` floor, `PKGS`; UI rows; `cmd/cairn-ui` flags (`-plugin-registry`, `-plugin-journal`); mutant rows; README. | Inert with no registry; every toggle OFF by construction. |
| **S6** | **Outputs: storage, rendering, deletion cascade.** Outputs stored per session; rendered labelled-derived (decision 11) on the session page and as a one-line summary on session rows; owner "delete transcript" POST cascading to outputs and edges; the capture `withdraw` route running the same cascade, and the agent's withdrawal on a routing change (decision 16). e2e clauses (k), (l), (m). | UI rows; worker-listener ledger (`withdraw`); `cmd/cairn-capture`; mutant rows; README. | Nothing renders until a plugin writes; the agent withdraws nothing until a routing answer changes. |
| **S7** | **"My sessions" (O10).** `GET /my-sessions`: the viewer's OWN sessions with a transcript (owner arm only), newest first, including owner-only ones, each linking to `/session`. e2e clause (g — the listing half). | UI row → ledgers, uiaudit; mutant row; README. | Read-only over S3/S4. |
| **S8** | **Agent read API and CLI (decision 17).** The classification table's skeleton, records and tool routes on `cairn-ui`; `cairn transcript skeleton|records|tool` Go-only verbs over `CAIRN_UI_URL`. | UI rows; `internal/client/cli.go` verbs; `capability_ledger` `go_only`; `want-go-only-verbs.txt`; `tests/test_go_client_ledgers.py`; mutant rows. | Read-only; the pod and parity corpus untouched. |
| **S9** | **Example plugin A — summaries.** `plugins/summary` in a NESTED stdlib-only module (provider HTTP API over `net/http`, no SDK), host-side user timer, incremental per decision 13, its own spend cap, a fake provider in tests. Reads through `view=conversation` by default. | `depspolicy.DeclaredNestedModules`; `flake.nix` package; `ci.yml` step for its suite. | A separate binary; nothing runs until registered and toggled. |
| **S10** | **Example plugin B — ClickUp.** `plugins/clickup` in the same nested module: ticket list fetch (read-only token, 429-aware), deterministic matchers (decision 14) over transcript records (`gitBranch`, `pr-link`, URLs), commit messages and trailers from a host-local repo list, PR bodies via the host's own GitHub CLI, entry refs; LLM suggestions using plugin A's summaries when present (`output:read:summary` — the cross-plugin test of the abstraction). Synthetic ClickUp fixtures only. Then closing wiring: `sabotaged=13 caught=13`, measured floors, the `AGENTS.md` row with an equal eviction (Q11). | same nested module; flake package; `ci.yml`; `AGENTS.md`; READMEs. | Separate binary; inert until registered and toggled. |

**Mutant rows** (indicative names). The pinned count starts at **296**; the **47** rows below would
take it to **343** if every one lands as named (revision 7 deleted six, listed where they were;
revision 8 added one) — the pinned number is whatever the battery declares
at each merge, never this sum. S0, S1, S9 and S10 add no row to the authz battery (S1's guards are
measured by the redaction corpus; S9/S10 by their own suites).

- **S2 (4, `scopeuse`):** `scopeuse-all-scopes-header-names-a-scope`,
  `scopeuse-scopeless-header-dropped` (the `renderer.go:158` form dropped instead of `*`),
  `scopeuse-cwd-derived-command-dropped` (a cwd-derived invocation — no explicit `--scope` — adds
  nothing instead of `*`), `scopeuse-unrecognised-invocation-adds-nothing` (a candidate line the
  parser cannot decompose adds nothing instead of `*`). *DELETED in revision 7 with the machinery they guarded (decision 3):*
  `scopeuse-paired-header-ignored`, `scopeuse-chained-call-header-suppresses-star`,
  `scopeuse-hook-attachment-counts-as-result`, `scopeuse-missing-result-becomes-star`,
  `scopeuse-chained-command-resolved`.
- **S3 (8):** `transcript-cas-ignores-from-offset` (a), `transcript-capture-token-host-unchecked`,
  `transcript-capture-token-reads`, `transcript-pod-recheck-skipped` (c),
  `transcript-pod-recheck-skips-blobs` (c), `transcript-root-owner-not-fixed`,
  `transcript-pod-skips-scope-rederivation`, `worker-token-accepted-by-browser-row`
- **S4 (11):** `transcript-section-without-predicate` (d),
  `transcript-written-set-from-viewer-scopes` (e), `transcript-visibility-set-ignores-reads` (f),
  `transcript-empty-set-visible` (g), `transcript-owner-by-display-not-id`,
  `transcript-narrowed-owner-arm`, `transcript-any-scope-not-every-scope`,
  `transcript-child-visible-without-root`, `transcript-unknown-scope-name-readable`,
  `transcript-star-sentinel-readable`, `transcript-render-classifies-by-record-type`.
  *DELETED in revision 7:* `transcript-pending-command-visible` (the pending set is gone).
- **S5 (15).** The toggle fold, ONE row per S5 fold case: `plugins-toggle-default-on` (i; case 1),
  `plugins-scope-on-ignored` (2), `plugins-arc-overrides-scope-off` (3), `plugins-arc-on-ignored` (4),
  `plugins-scope-toggle-any-not-every` (5), `plugins-arc-off-ignored` (6),
  `plugins-session-opt-out-ignored` (7), `plugins-unknown-scope-toggle-falls-back` (8),
  `plugins-empty-set-vacuously-on` (9), `plugins-session-level-accepts-on` (10). The rest:
  `plugins-transcript-read-ignores-toggle` (j), `plugins-output-type-unchecked`,
  `plugins-capability-unchecked`, `plugins-toggle-unauthorised-level`,
  `plugins-plugin-name-from-body-not-token`
- **S6 (6):** `plugins-output-rendered-without-predicate` (k), `plugins-delete-keeps-outputs` (l),
  `transcript-withdraw-keeps-directory` (m), `transcript-withdraw-deletes-another-owners-root`,
  `transcript-withdraw-host-unchecked`, `transcript-withdraw-repeat-answers-false`
- **S7 (1):** `my-sessions-lists-another-owner`
- **S8 (2):** `transcript-read-api-without-predicate`, `transcript-raw-view-without-predicate`

**Clause ↔ row ledger.** (a) cas · (b) none — its sabotage is in the AGENT's redaction call path,
which `PKGS` does not cover; it is measured by the e2e and the corpus · (c) both re-check rows · (d)
section · (e) viewer scopes · (f) ignores reads · (g) empty set · (h) none — guarded in
`cmd/cairn-capture`, not in `PKGS`; its Go-side control is S2's routing test · (i) default on · (j)
read ignores toggle · (k) output without predicate · (l) delete keeps outputs · (m) withdraw keeps
directory.

### Test plan per slice (negative controls named)

**S0.** The generator's output parses with 0 errors in both readers' unit tests; the shape test
fails when the generator emits a record type, block type or field the classification table does not
declare (control: add one) and when the table declares one the generator never emits (control:
delete one).
`leakscan` stays clean over the fixtures (they contain no credential shapes — S1 plants those at
run time).

**S1.**
- `planted=P caught=P` over the corpus; **negative control:** with the dotenv rule removed, caught
  < P and the report names the rule; **positive control for the structural rule:** a `Secret`
  manifest's `data` values are replaced while its `metadata.name` survives.
- `clean-damaged=0` over UUIDs, 40-hex SHAs, sha256 digests, base64 image payloads; control: a
  deliberately greedy rule (`[A-Za-z0-9]{32,}`) makes `clean-damaged` > 0, proving the counter
  can move.
- JSON-escaped secret: a token whose hyphen is written as a six-character JSON unicode escape
  (backslash, `u`, `002d`) is caught after decoding; control: scanning the raw bytes instead misses
  it (the reason decision 6 decodes first). *Revisions 1 and 2 printed this escape as a bare
  hyphen, which described no test at all.*
- Text blobs (decision 6a): a planted dotenv value in a `.txt` blob is replaced and the blob is
  otherwise byte-identical; a planted YAML `Secret` manifest in a `.txt` blob has its `data`
  values replaced by the line-based rule; a JSON blob whose planted secret is written with a
  unicode escape is caught by decoded traversal (control: redacting it as one raw string misses
  it); a re-encoded JSON blob keeps every number byte-exact (`json.Decoder.UseNumber`, so a
  20-digit id is not rounded through a float); a base64-encoded text secret outside any `Secret`
  manifest is caught decoded, **including a 40-character token whose encoding is 56 characters**
  (control: with the floor back at 64 that plant is missed — the reason the floor is 16).
- Binary BY CONTENT (decision 6a), one assertion per measured carrier, each WITHHELD with a
  placeholder carrying type, size and keyed tag: an inline `image` block's payload; **its
  `toolUseResult.file.base64` duplicate**; **an opencode `state.attachments[].url` `data:` URL**; a
  blob with a NUL byte in its first 8,000 bytes; a UTF-16 text blob (non-UTF-8 → withheld, as Q2
  states). Controls: (1) a field-name list that omits `file.base64` ships the duplicate — red; (2)
  a sniff that reads only the file extension classifies a NUL-carrying `.txt` as text — red; (3)
  thinking `signature` values and 64-hex digests are NOT withheld (`clean-damaged=0`), proving the
  rule keys on a file signature and not on "decodes to non-text". Decoder coverage: a whole-string
  PNG payload encoded padded, unpadded, URL-safe, and line-wrapped at 76 columns is withheld in all
  four forms (control: a decoder trying only `StdEncoding` without whitespace removal withholds one
  of the four). Residual pinned AS a residual: the same PNG payload embedded after a line of other
  text in one string is NOT withheld — the test asserts that, so a later in-string scanner is a
  deliberate change that turns it red, not an accident.
- Keyed tag: two occurrences of one secret get one tag under one host key; two keys give two tags
  for the same secret; the tag is NOT the unkeyed digest prefix (control: computing
  `sha256(secret)[:8]` does not equal it).
- Containment: every realistic `credential` control string from leakscan's self-test is redacted;
  control: deleting the GitHub-token rule from `internal/redact` makes this test red.

**S2.**
- Offset reader: a file grown mid-line ships only complete lines; the next run ships the rest
  exactly once. Control: a reader that ships the partial line produces a record that fails to
  parse (asserted red).
- A file whose first 4 KiB changed (a rewrite, not an append) is NOT resumed: it is refused
  locally and logged — the agent never uploads from an offset into different bytes.
- Every byte: the uploaded-record count equals the fixture's line count for every stream,
  bookkeeping and duplicate fields included (control: an agent filtering `attachment` records makes
  the count test red). A persisted TEXT tool-result file with no planted value becomes a blob
  byte-identical to the source; one with a planted value becomes the source with exactly that span
  replaced; a BINARY one becomes the withheld placeholder (decision 6a). *Revision 2 asserted every
  blob byte-identical to its source, which contradicted redaction; retracted.*
- opencode: from two recorded synthetic exports where 3 parts changed `time_updated` and 1 was
  added, exactly 4 upserts are produced; control: diffing by id alone produces 1.
- **The SQLite file is never opened:** a test runs the reader with `HOME` pointing at a tree where
  the database path is a FIFO; the reader completes (it only calls `opencode export`, here a stub).
- `scopeuse`, with literal expectations per S0 read path, IDENTICAL for the pod and the agent: an
  explicit `--scope beta-notes` read → `beta-notes`; the hook-attachment recall with no command line
  → `beta-notes` (control: a parser that reads tool INPUTS only misses it); the hook-attachment
  search rendered `scope=(all scopes)` → `*` (mutant `scopeuse-all-scopes-header-names-a-scope`); a
  `renderer.go:158`-form header → `*` (mutant `scopeuse-scopeless-header-dropped`); a bare `cairn
  recall` whose result carries `scope=alpha-notes` → `alpha-notes` AND `*` (the header adds, the
  cwd-derived call still adds `*`); a `--repo` `ls-entries`, a bare `ls-entries`, and an opencode
  `bash` call running a bare `cairn recall` with its own `workdir` argument → `*` each (mutant
  `scopeuse-cwd-derived-command-dropped`); the recognition rule's shapes, each WITHOUT a
  decomposable `--scope` → `*`: `/opt/tools/cairn ls-entries --repo ../beta-repo`, `nix run
  <flake>#cairn -- ls-entries --repo ../beta-repo`, `VAR=x cairn recall`, `bash -c 'cairn
  recall'`, `xargs cairn ls-entries`, `timeout 30 cairn recall`, and `subsystem-recall` with no
  `--scope` (mutant `scopeuse-unrecognised-invocation-adds-nothing`); a positive control that the
  rule is not a blanket `*`: `cairn recall --scope alpha-notes && cairn search --scope beta-notes
  x` → exactly `{alpha-notes, beta-notes}`, no `*`; and the agent's output for every fixture equals the
  pod's (control: an agent that resolves a working directory gives a scope where the pod gives
  `*`). *DELETED in revision 7 with decision 3's exception:* the simple-call, chained-call,
  newline/`&`, hook-attachment-as-result, pending-result, error-part and 24 h-backstop cases, and
  the agent-side `DeriveScope` case.
- Routing — THE fixture of clause (h) and decision 16: a table sending `alpha-notes` to the personal
  instance and `beta-notes` to the client; wrote `alpha-notes` and read `beta-notes` in the first
  turn → held, logged, nothing queued for either. Also: read `beta-notes` only → queued for the
  client instance; empty `V` → the default instance; control: routing on writes only queues the
  fixture for the personal instance.
- Re-ship (decision 16): run 1 with `V` empty queues the session for the default (personal)
  instance; run 2 after a `beta-notes` read queues a withdrawal for the personal instance AND every
  record and blob of the session for the client instance from offset 0 (asserted: the client queue's
  first record is the session's first line); control: an agent that holds instead queues nothing
  for the client instance.
- Withdrawal trigger (decision 16, the agent half of clause m): a session whose first run queued
  `alpha-notes` turns for the personal instance and whose second run adds a `beta-notes` read →
  the second run queues NO records and queues a withdrawal for the personal instance; control: an
  agent that only stops shipping queues no withdrawal.
- `--dry-run` output contains no string value of any record (planted sentinel in every string
  field, grep the output: 0; positive control: the sentinel IS in the fixture).
- `depspolicy`: the ban's walk now includes `cmd/cairn-capture`. **Negative control: a NON-test
  `.go` file** in `cmd/cairn-capture` of a scratch copy importing `maragu.dev/gomponents` makes the
  ban red; a `_test.go` importer would stay green by design (`depspolicy.go:147-154`), so it is not
  the control.

**S3.**
- CAS (clause a): two concurrent uploads from one `from` → exactly one 200, one 409 naming the
  stored position.
- Frames: a 3 MB record sent as three frames is stored as ONE record byte-identical to the source;
  a missing middle frame stores nothing.
- Ownership: owner B's first upload of `s-0001` after owner A's → 409 with the uniform body, and
  A's directory is unchanged; the same (owner, host) continuing → 200.
- Pod re-check (clause c) on a record AND on a text blob, each with its clean positive control;
  quota (507 names it; the agent holds and a later retry after retention
  frees space succeeds), retention sweep at retention − 1 s and + 1 s.
- Pod-side `V`: an agent that declares nothing and whose records carry a rendered `beta-notes`
  recall gets `beta-notes` in `meta.json` anyway (control: the mutant that trusts declarations only).
- Token isolation: capture tokens presented to every browser row, `POST /sign-in` and the pod are
  refused as a random token is (byte-compare); a capture token for `host-a` posting `host: host-b`
  → 400 and nothing written; revocation by deleting the row → next request 401, no restart.
- Startup: `-worker-addr` without `-worker-tokens`, or with `-transcript-dir` inside the store root,
  or without `-transcript-retention`, refuses; blanks refuse; a reachable bind with no trusted-proxy
  allowlist refuses.
- Clause (b) end to end, as TWO runs. (1) The real agent captures the S0 world with S1's planted
  secrets (records and a text blob) through a recording relay into a real `cairn-ui`; the relay
  REASSEMBLES continuation frames per record and per blob, decodes, and finds **0** planted values.
  (2) The redaction-off test build captures the same world through the relay with NO pod behind it:
  the relay answers every request 200 itself, so no 422 can make the agent hold a stream and stop
  sending; after reassembly it finds **P**. Report the pair; a count other than P in run (2) exits 2
  ("could not vouch"), because it means the instrument, not the redactor, is broken. *Revision 2
  ran the positive control against the real pod, whose 422 held the stream, and counted frames
  unassembled — so it could never reach P.*

**S4.**
- Relationships over ONE store and journal (clauses d–g): owner sees; R (reads every scope in `V`)
  sees; P (reads one written scope) gets the no-transcript bytes while P's session page is still
  found; Q (reads the written scope but not the READ scope) gets them too; a narrowed credential
  of the owner sees a session whose `V` it covers and NOT an owner-only one; an empty-`V` session is
  owner-only and its `/session` page is found for the owner alone; a scope renamed after the write is
  treated as unreadable for non-owners (control: renaming it back restores R's view); `*` hides from
  everyone but the owner.
- The trap's own control: a sabotaged predicate computing `W_trailer` from the viewer's scopes makes
  P see the transcript (clause e red).
- Byte identity: hidden, absent and deleted render the same section bytes.
- *DELETED in revision 7:* the pending-set visibility case (the set is gone, decision 3).
- Rendering (decision 18): a 20,000-character user message renders in full (asserted byte count);
  a 5,000-character assistant message renders 1,200 + a `<details>` holding the rest; a tool call
  renders inside `<details>`; a subagent nests at its spawning call; bookkeeping collapses to one
  `<details>` per turn; the compaction summary is labelled runtime-generated; `AllowedScriptSources`
  still has ONE entry. Control: a renderer that truncates user text fails the byte count.
- **Block-level classification (decision 17):** ONE `user` record carrying a 600 KB `tool_result`
  block renders that block COLLAPSED in a `<details>` showing its first 64 KiB with a raw-view link
  — not in full — while a separate `user` record with a 20,000-character `human-text` block renders
  in full; the `toolUseResult` field of the first record is counted under "duplicate tool-result
  copies", not rendered. Control: the mutant `transcript-render-classifies-by-record-type` renders
  the 600 KB block in full (asserted by byte count).
- XSS: a record whose every string is `<script>…</script>` renders escaped; the raw-node ban still
  passes.
- Cost: the benchmark reports the whole-store `W_trailer` walk against the session page's own walk
  at both of the existing benchmark's sizes, as a ratio.
- uiaudit: the session page with a transcript at all five widths, overflow 0, the touch refusals
  from the mobile plan unchanged.

**S5.**
- Toggle fold, ten cases with literal expectations, each red under its OWN mutant (named in the
  mutant list by case number): (1) nothing set → OFF; (2) every scope in `V` `on` → ON; (3) **arc
  `on` + one scope `off` → OFF** (the round-1 finding's own case); (4) arc `on` + every scope
  `allowed` → ON; (5) arc `on` + one scope `allowed` and one `off` → OFF; (6) every scope `on` + an
  arc `off` → OFF; (7) every scope `on` + owner opt-out → OFF; (8) an unknown scope name or `*` in
  `V` → OFF; (9) **empty `V` → OFF** (never vacuously ON); (10) a session-level ON is refused 400
  and changes nothing. The ON cases (2, 4) are what keep a blanket-OFF implementation from passing
  the OFF cases.
- Authorisation per level: a scope `write`-only principal cannot toggle the scope (403); its admin
  can; a principal who can PUT an arc but is not admin of its home scope cannot toggle the arc; a
  session's non-owner cannot opt it out.
- Journal: every accepted toggle appends exactly one record with `(Kind, ID)` and pod time; a
  refused one appends none.
- Capabilities (clause j): each refusal returns the same 403 bytes; the plugin name in an output
  envelope is taken from the token, and a body naming another plugin is overwritten (control: the
  mutant that trusts the body).
- Pending query at both trigger boundaries (9 min 59 s vs 10 min; 199 vs 200 records); writing an
  output removes the session from the next query (control: an output with an OLDER watermark does
  not).

**S6.** Output visibility equals transcript visibility for every viewer in S4's matrix (clause k,
asserted as equal SETS of viewers); a summary renders plugin name, version, model, watermark and
"N newer turns not yet summarised" when the transcript grew; deletion cascade (clause l) and its
journal fact; withdrawal (clause m): a capture token's `withdraw` of its own (owner, host)'s root
answers `{"deleted": true}` and runs the same cascade; the same call for ANOTHER owner's root, for
the SAME owner's root uploaded from ANOTHER host label, and for a root that never existed each
answer the identical `{"deleted": false}` and delete nothing (mutants
`transcript-withdraw-deletes-another-owners-root` and `transcript-withdraw-host-unchecked`, the
latter on the precedent of `transcript-capture-token-host-unchecked`); **withdraw-twice**: the
owner's own withdraw repeated after the first succeeded (the lost-acknowledgement case) answers
`{"deleted": true}` again and changes nothing (mutant `transcript-withdraw-repeat-answers-false`);
a ClickUp URL renders only through the `refurl` template from a validated canonical
id (control: a `javascript:` id is refused by validation and renders as text).

**S7.** The owner's page lists their owner-only and shared sessions; another principal who can read
every scope of the owner's shared session does NOT see it listed on THEIR "My sessions" (it is not
theirs) but can open it; control: the mutant that lists by visibility instead of ownership.

**S8.** Each route returns only what decision 4 admits (the S4 matrix replayed over the JSON
routes); `view=conversation` drops bookkeeping records and strips the duplicate `toolUseResult` field, naming each in a placeholder;
`view=raw` returns the stored bytes exactly (control: byte-compare against the stored segment);
`max_bytes` bounds every page, including a page whose first record alone exceeds it (it is returned
alone, with `next_seq`, never split silently); the CLI verbs appear in `cairn -verbs` and the
Go-only ledgers; `internal/api`, `tests/conformance/requests.json` and the parity corpus have no
diff.

**S9.** Against a fake provider (`httptest`): the first run summarises records 1–N; a second run
after M new records sends only records N+1–N+M plus the prior summary (asserted on the fake's
received bodies); a compaction summary record is labelled runtime-generated in the prompt; with
its own cap at 0 the fake receives 0 requests, and with the cap high ≥ 1 (the positive control).

**S10.** Deterministic matchers, each with literal fixtures: a branch `feature/clk0000a1-x`
matches only when `clk0000a1` is a fetched ticket id OR appears as a ClickUp URL; a commit message
with `https://app.clickup.com/t/clk0000a1` → edge `commit-message`; a custom id `ALPHA-12` matches
only when it is in the fetched custom-id list, and the edge stores the CANONICAL id it resolves to
(control: the same string with an empty list → no edge); a `/t/<workspace>/ALPHA-12` URL resolves
the same way; a `pr-link` record plus a PR body naming a ticket → edge `pr-body`; an entry the
session wrote with `refs: [clickup:clk0000a1]` → edge `entry-ref`. Every edge carries a locator
that resolves. LLM suggestions come from the fake provider with a fixed confidence and are never
written as edges (control: the fake returns a suggestion at confidence 1.0 — still a suggestion).
429 handling: the fake answers 429 with a reset header and the plugin waits, asserted by the fake's
request timestamps. **All ClickUp fixtures are synthetic** (ids `clk0000a1…`, list `list-0001`).
Closing wiring: `--self-test` prints `sabotaged=13 caught=13`; each sabotage fails ITS clause's
message; the `ok` floor, mutant count and e2e floor equal the counts measured on the merged tree;
`AGENTS.md` stays under its working budget.

## Audit dispositions (round 0 deletion candidates)

| # | candidate | disposition |
|---|---|---|
| D1 | the pod-side budget window | **DELETED** — cooperative only; the plugin's own cap and the provider's limit are the ceilings that hold (decision 12). |
| D2 | work leases and acks | **DELETED** — a stateless "sessions past my watermark" query serves one worker per plugin; the one-worker assumption is stated (decision 13). |
| D3 | instance- and session-level toggles, `-plugin-admin` | **Instance level and `-plugin-admin` DELETED** (registering a plugin is the instance act; O5 names scope and arc). **Session level KEPT as an owner opt-out that can only narrow**, because scope consent is per scope and the owner of a session in ON scopes has no other way to keep that ONE session from a third-party-egress plugin (decision 10). *Revision 2's reason — owner-only sessions have no scope to say no at — was false (they already resolve OFF) and is retracted.* |
| D4 | drop the pod re-check, or make it count-and-log | **KEPT, as a refusal.** Storing a string the table recognises as a secret is the harm; the masking of clause (b) is removed by measuring (b) on the agent's outgoing bytes instead (decision 6). |
| D5 | do not share a rule file with `tests/leakscan.py` | **DELETED the sharing (Q3 withdrawn).** Separate tables; one behavioural containment test (decision 6). |
| D6 | (owner-only / "my sessions" disposition) | **SUPERSEDED by O10** — shipped, owner-only, listed on "My sessions" (S7). |

## Open questions for the operator

Each has a recommendation. S0 and S1 depend on none of them. **Q2, Q14 and Q16 all shape S2**:
binary withholding (Q2), hold-back, withdrawal and re-shipping (Q14), and the pod-side `*` rule,
which lives in S2's `scopeuse` package with its mutant `scopeuse-cwd-derived-command-dropped` and
first RUNS on the pod in S3 (Q16). S2 builds the RECOMMENDED answers, and because S2 uploads
nothing, a different answer changes S2's code before any byte has left a host. No question blocks
merging S0–S2; Q2, Q14 and Q16 must be answered before S3 ships to a real instance. *Revision 3
said Q16 shapes S3; the rule's code and mutant are in S2.*

- **Q1. REMOVED (O9)** — subagent transcripts are shipped.
- **Q2. May content no redactor can read leave the host?** Storage is not the difference — it would
  be stored the same way. The difference is that redaction, a hard requirement (O1), cannot read
  it. **The withheld set, exactly (decision 6a):** (i) images and other recognised binary payloads,
  wherever they sit — inline `image` blocks (68 in the sample), their duplicate
  `toolUseResult.file.base64` (127 records), opencode `state.attachments[].url` `data:` URLs (107),
  and persisted binary tool results (`.pdf`, `.jpg`, …); and (ii) **TEXT that is not UTF-8** — a
  UTF-16 or Latin-1 file is "binary" by 6a's rule although a redactor could read it once decoded.
  **NOT in the withheld set, and shipped text-scanned only (6a's residuals):** a binary payload in
  an unlisted format base64-encoded with no `data:` prefix, and a listed-format payload EMBEDDED in
  a longer string (3 embedded base64 runs and 6 embedded `data:` URLs measured on one host). A
  "withhold" answer covers them only if the operator also asks for in-string scanning.
  Two operator requirements collide here: every byte (O9) and redacted before upload (O1). **As
  built until answered: WITHHELD** — a placeholder naming type, size and keyed digest is shipped
  instead. **Recommend keeping that** (redaction is the requirement whose failure cannot be
  undone). The alternative is shipping class (i) — images, PDFs, archives, by recognised file
  signature only — marked "not redacted", which turns T1's binary row into an uncontrolled
  residual. ⚠ A "ship" answer must NOT reach class (ii): non-UTF-8 text is redactable after
  transcoding, so the right change there is transcode-then-redact (a later change), never shipping
  it unredacted. *Revision 2 recommended shipping; revision 3 reverses that recommendation because
  revision 2's own decision 6 made redaction a hard requirement and gave no way to meet it for
  binary content.*
- **Q3. WITHDRAWN (D5)** — the rule table is not shared with leakscan.
- **Q4. Real-data redaction audit.** **Recommend** a host-local script, never committed, that runs
  the rule table over the host's own transcripts and prints only per-rule COUNTS, run once before
  arming capture and after every rule change.
- **Q5. The client instance.** It holds client principals and client-confidential content, and
  plugins there send transcripts to an LLM provider. **Recommend:** do not arm capture there until
  the personal instance has run for a while, and require an explicit decision before any LLM
  plugin's scope toggle is set to `allowed` there.
- **Q6. Retention and quota.** Every byte (O9), measured on one host: ≈2.8 GB of JSONL per week
  (subagents included; persisted tool results and opencode not), ≈0.8 GB/week at the sampled 3.5×.
  **Recommend** 90 days and an instance quota of ≈2× the steady state — ≈20 GB compressed per
  capturing host on the personal instance — re-measured after a month of real uploads.
- **Q7. One capture owner per instance (the presence single-owner wall)?** **Recommend yes for the
  personal instance** — it also removes the session-id squatting case (decision 15, T5); the client
  instance's answer depends on whether anyone else will run capture.
- **Q8. Store ClickUp ticket titles?** **Recommend no by default** (ids only); a no-default flag
  allows titles per instance.
- **Q9. Account identifiers in records** (`ownerAccountUuid`, `accountUuid`, `credential_org`
  attachments). Not credentials, so redaction leaves them. Under O9 (every byte) they ship.
  **Recommend** keeping them, and asking only whether the per-host denylist should carry them.
- **Q10. Where the example plugins run.** **Recommend** a user timer on one operator host for v1
  (the LLM key and ClickUp token stay there); a pod is the same binary later.
- **Q11. `AGENTS.md`.** It has 25 bytes of headroom. The new packages need one layout row
  (≈150 bytes). **Recommend** the S10 PR evicts an equal amount of history to a README, as the weight
  test's playbook prescribes — never by narrowing a rule.
- **Q12. Storage backend and replicas.** **Recommend** the per-session directory (decision 15) and
  one replica; if a second replica is ever needed, move transcripts to object storage or pgstore
  behind the same `internal/transcript` interface.
- **Q13. Commit-message and PR-body signals need host access to repositories.** **Recommend** a
  host-local list of repository paths in plugin B's config, read with `git log` and the host's
  existing GitHub CLI login — nothing new on the pod.
- **Q14. What happens when a session's scopes span two instances (decision 16).** What would
  actually be built: capture ships every 60 s, so a session is shipped to the instance its `V`
  routes to SO FAR; when a later run finds `V` spanning two instances (or holding `*` with more than
  one instance configured), the agent stops shipping it, holds the rest on the host, and WITHDRAWS
  the already-shipped prefix (the deletion cascade on that instance). When a later run finds `V`
  routing to a SINGLE instance other than the one holding the prefix — e.g. run 1 with `V` empty
  (shipped to the default, personal, instance) and run 2 with `V = {beta-notes}` (client) — the
  agent withdraws the prefix and RE-SHIPS the whole session to the new instance from offset 0. ⚠
  That re-ship happens only when the `beta-notes` read carried an explicit `--scope`: a BARE or
  `--repo` read adds `*` as well (decision 3), so with more than one instance configured the
  session is HELD, not re-shipped — the usual outcome today, unless Q16(a) is adopted.
  *Revision 7 still called this re-ship "the common case"; retracted.* A session with empty `V` goes to the default instance, owner-only. This NARROWS "ship
  every session" only for held sessions, which exist only on their host. **Recommend** hold +
  withdraw + re-ship as written. Alternatives: (a) keep the prefix
  where it is and hold only the rest — the prefix stays visible under its own `V` there but is an
  incomplete transcript presented as the session; (b) ship the whole session to the instance holding
  its WRITES with the other instance's scopes still in `V` (owner-only there) — keeps the bytes but
  moves client-read content into the personal store, which O8 rules out; (c) delay all shipping until
  a session is idle — no withdrawal needed, but no continuous summaries.
- **Q15. Agent reads through `cairn` or a separate binary?** **Recommend** Go-only `cairn transcript`
  verbs (decision 17): agents already run `cairn`, and the Go-only mechanism exists. The alternative
  is a `cairn-transcript` reader binary that leaves `cmd/cairn`'s ledgers untouched.
- **Q16. Every `cairn` call without an explicit `--scope` makes its session owner-only — and,
  with several instances configured, HELD on its host.** That is decision 3's rule, with no
  exception: any invocation whose scope comes from the working directory or `--repo` adds `*`.
  **The cost, measured** (round 6 of the audit, this host): among `cairn` calls with no `--scope`,
  Claude Code had 597 and opencode 24 — and under the unconditional rule EVERY session, on BOTH
  runtimes, that runs a bare or `--repo` `cairn` command becomes owner-only, and with more than one instance configured it is
  held on its host and never shipped (decision 16). *Revisions 4–6 tried to exempt a "simple"
  recall/search whose own output named its scope; the exemption admitted 0 of the 597 Claude Code
  calls and 5 of the 24 opencode ones, and is deleted (decision 3). Every earlier wording of this
  question is retracted with it.* **Options:**
  - **(a) Pass `--scope` explicitly** in the hooks, skills and agent instructions that invoke
    `cairn`, so the scope is in the command line and nothing is cwd-derived. No cairn change; the
    cost moves to the callers' configuration. **Recommended.**
  - **(b) FUTURE, not designed here, and needing its own audit:** the client prints its resolved
    scope on every verb, and the pod admits a header from a call's OWN result only when that call
    contains exactly one `cairn` invocation. It is the shape revisions 4–6 kept refining; it would
    come back only as its own change with its own measurement of what it admits.
  - **(c) Accept owner-only / held** for those sessions, and measure the share on the personal
    instance.

## Recommended improvements beyond the ask (clearly recommendations)

- **B1. MOVED INTO SCOPE (O10)** — "My sessions" is S7.
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
- **The SHARE of real sessions `*` makes owner-only or held.** Round 6 of the audit counted
  `cairn` CALLS without `--scope` on this host (597 Claude Code, 24 opencode — Q16), not sessions;
  the per-session share, the second host, and hook-injected reads were not measured.
- **What `opencode export --sanitize` removes**, and whether `opencode export` is safe to run
  against a database a live opencode process is writing (one export, one idle session).
- **Redaction recall on real transcripts** (R6) — deferred to Q4's host-local count.
- **The whole-store `W_trailer` walk's cost per view** — benchmarked in S4, not here.
- **The weekly rate of persisted tool results and of opencode growth.**
- **LLM cost per summary** and summary quality; **ClickUp API behaviour** (no request was made;
  rate limits are from the documentation).
- **The compression ratio at scale** (3.5× on a 30-file sample).
- **Sizes and effort.** Not estimated; nobody has measured these slices.
