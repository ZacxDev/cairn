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
- *Revision 9* replaces revision 8's recognition rule, which was vacuous over zero invocations,
  never required every candidate token to be accounted for, and missed at least `.#cairn-go` and `#default` (and, on one reading,
  the bare flake ref). A line is now a candidate when ANY token contains `cairn` or
  `subsystem-recall`, and contributes only explicit scopes when (a) ≥ 1 invocation is parsed, (b)
  every candidate token is a recognised invocation's program word, and (c) each carries `--scope`;
  otherwise it adds `*` beside what it parsed.
- *Revision 10* exempts a recognised invocation's own ARGUMENTS from condition (b): the line is
  split into segments at control operators, and a cairn-containing word after the program word of
  a fully parsed `cairn` segment (no command substitution) is not a candidate. Revision 9 counted
  them, so `--scope cairn-notes`, a quoted `"cairn plugin"` search term or a `…/cairn/…` path added
  `*` to a scoped call (593 of 1,343 scoped lines on this host, heuristic count). The substring
  match is stated to be case-insensitive.
- *Revision 11* closes two fail-open paths in that exemption. Any segment word containing `(`,
  `)`, `$`, a backtick, `{`, `}` or a word-initial `=` — quoted or not — disqualifies the segment,
  covering zsh glob qualifiers and `=(…)` as well as bash substitutions; and `ls-entries` adds `*`
  whatever its flags, because both clients ignore its `--scope`. Redirections are stripped before
  splitting, zsh's `|&`/`&|`/`&!` are split on, and a repeated `--scope` is read last-wins.
- *Revision 12* closes the two holes revision 11's nits opened: a stripped redirection target or
  here-string that contains the name still counts as a candidate token (an input redirection can
  feed it to a shell), and `--scope` values are read as the UNION over every spelling (`--scope X`,
  `--scope=X`, Python prefix abbreviations), with an unclassifiable possible abbreviation adding
  `*` — replacing revision 11's last-wins, which a literal-only parser got wrong. `sync` is named
  as accepting and ignoring `--scope` while printing only banners.
- *Revision 13 is a DELETION-led rewrite on an operator decision (O11).* The shell-command-line
  parser that revisions 8–12 built for READ scopes is DELETED — candidate tokens, segments,
  redirection handling, the disqualifying-character rule, the argument exemption, conditions
  (a)–(c), the `--scope` spelling union and the `ls-entries` special case — with its fixtures and
  eight mutant rows. **Everything the revision 8–12 entries above say about that parser is
  superseded and retracted — and so is revision 7's rule "every `cairn` invocation without an
  explicit `--scope` adds `*`": under the ledger such a call adds the scope it actually read.** In its place: a READ LEDGER both `cairn` clients write about
  themselves when a session id is in their environment (decision 3a, slice S11), uploaded with the
  transcript, plus two coarse fail-closed fallbacks (F1: a cairn-naming session with an empty
  ledger; F2: any input naming the cache root). New clause (n), six new mutant rows; Q16 and T3
  rewritten.
- *Revision 14* fixes three ledger gaps from audit round 12. Verbs that print store-derived content
  without touching one resolvable scope — decision 17's `transcript` verbs, `doctor`, `routes` —
  now write a `*` record (revision 13 wrote none for them, so a `transcript records` read of
  another session added nothing to `V`). The pod's fold of the `ledger` stream, and its mutant
  row, move from S3 to S11, which adds the ledger. And S11 makes `tests/testlib/env_pin.py` clear
  `CLAUDE_CODE_SESSION_ID`, `OPENCODE_SESSION_ID` and `XDG_STATE_HOME`, without which test runs on
  agent-run hosts would write fixture scopes into the developer's real session ledger.
- *Revision 15* moves `arcs` and `arc-show` from the resolved-scope list to the `*` list — they print
  arcs and member writes homed in OTHER scopes (`internal/report/arcs.go:236-239, 453, 406/457`) — states
  the owner-only cost of every `*`-writing verb, and makes the `env_pin` control assert nothing
  appears under a sentinel `HOME` either.
- *Revision 16* corrects that control: the parity harness overrides `HOME` for every row, so the
  control searches each row's harness `HOME` (`work/home`) and the sentinel `XDG_STATE_HOME` for any
  `cairn/read-ledger` file, with a before-the-change positive control. It also corrects the
  `arc-show` citation to `MemberLine`'s "wrote in:" list and lists the `*`-writing content verbs in
  Q16 and the unmeasured-share note.
- *Revision 17 records three operator decisions and S0's measurements, and is a WITHHOLDING
  DELETION.* **O12** answers Q2: content no redactor can read — images, PDFs, other binary
  payloads, non-UTF-8 text — SHIPS, every byte. Decision 6a's withholding, its placeholder, its
  file-signature list and every test asserting a withheld value are DELETED; binary content is
  still CLASSIFIED (for collapsed display) and shipped byte-identical. **Everything revisions 3–4
  and decision 6a's earlier text say about withholding is superseded by this entry and by the
  rewritten decision 6a.** **O13** records the defaults the coordinator stated it would take on Q4,
  Q5, Q7, Q8, Q9, Q10, Q11, Q12 and Q15 (the plan's own recommendations) before the operator
  answered O12 and O14; the operator did not object. **O14** answers Q6: 90-day retention, a quota of about twice the steady state,
  re-measured after a month. S0 measured four facts about opencode that move decision 5 (R3): an
  export TRUNCATES when its stdout is a pipe, it carries no part `time_updated`, `session list`
  shows only the current project's root sessions, and `OPENCODE_SESSION_ID` is not set by opencode
  itself (decision 3a).
- *Revision 18 applies review rounds 0 and 1 of S0–S2.* **The coordinator's reading of O12 narrows
  "binary"** (below, and decision 6a): binary is a value whose bytes BEGIN WITH A KNOWN BINARY FILE
  SIGNATURE, or a base64/`data:` payload that decodes to one; everything else — NUL-separated text
  and mostly-UTF-8 text with stray invalid bytes included — is TEXT and is scanned. Revision 17's
  "no NUL in the first 8,000 bytes AND valid UTF-8" text rule is retracted with it. A text written
  by an AGENT to a subagent (a sidechain `user` record, an opencode child session's `user` text) is
  its own class, `agent-prompt`, never `human-text` (decision 17). Decision 18 anchors an opencode
  child at the `task` part that names it, not at a `subtask` part. Decision 6 now parses a `data:`
  prefix before its base64 rule.
- *Revision 19 records operator decision O15 (S1 review round 4).* Three rounds of per-format
  redaction rules each closed the cases they named while a fresh held-back set stayed flat and
  damage to clean text grew. **O15** changes the approach — key context plus entropy over
  prefix-normalised lines (decision 6) — and adds an **arming gate**: closing-condition part 5, and
  the S3 arming precondition. It also corrects a recall figure the plan stated (the libpq row of
  the rate test, test plan S1).
- *Revision 20 applies S1 review round 5 under O15, and scopes what O15's key-context rule does as
  BUILT.* "Any value attached to a secret-sounding name" was wider than the code: a named value is
  redacted unless its SHAPE is code, a placeholder or prose, a bare value is longer than 1 KiB, or —
  for a name whose secret word is a segment rather than its end — it is a word, a slug, a number or
  a URL (decision 6, T1). It also corrects the rate test's stated figure (one seed reported as if
  general; now a range over named seeds) and records a scorer defect that made the self-test's
  78/79 seed-dependent.
- *Revision 21 applies S1 review round 6 under O15, and RETRACTS one clause of revision 20's
  scoping.* "A bare value longer than 1 KiB is refused" is gone: round 5 took it to keep the rule
  linear and it shipped a 2 KiB hex key and a URL-encoded document that round 4 had caught; a named
  bare value of any length is now taken, judged by its first 64 bytes. The rest of the round
  replaces evidence that was too thin in both directions — a `.netrc` password line, a weak name's
  value, "code notation" read off spacing, an identifier counted by characters alone, a `.pgpass`
  row read off five colons — and it adds the thing rounds 4 and 5 lacked: **damage is now measured
  on text nobody wrote for the purpose** (this repository's tracked text, and the Go standard
  library's source) **and pinned as a budget**, so a round that widens it fails a test. Decision 6,
  T1 and the S1 test plan carry the measured numbers and the residuals.

## Goal and premise

The operator asked for two things, carried in one plan because the second consumes the first:

1. **Session content in the store.** Every byte of every Claude Code and opencode session — tool
   calls, tool outputs, subagents and the runtimes' own bookkeeping included — shipped from the host
   it ran on to cairn, redacted on that host before it leaves, and shown on the session page
   (`/session`) to the principals allowed to see it. ONE exception narrows "every byte", decided
   and stated where it lives: a HELD session stays on its host (decision 16) — held when its scopes
   route to two instances, when it carries `*` or an unroutable scope name while more than one
   instance is configured, or when a withdrawal of its earlier prefix was answered `false`. Content
   no redactor can read SHIPS unredacted (O12, decision 6a). *Revisions 3–16 counted withheld
   binary as a second exception; O12 deleted it.*
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
  1. Slices **S0–S11 are MERGED** on cairn `main`, verified by content (the named files exist with
     the named tests), not by ancestry.
  2. **`tests/plugins/e2e.sh` exits 0** in the `go` CI job, with **`--self-test` printing
     `SUMMARY e2e-self-test: sabotaged=14 caught=14`** (one sabotage per clause below), and the
     job's PASS floor set to the count measured when the script lands (the `tests/presence/e2e.sh`
     pattern, `.github/workflows/ci.yml:1045-1055`).
  3. **`cairn-capture --self-test` exits 0** in the `go` job and prints
     **`SUMMARY redaction: planted=P caught=P clean-damaged=0`** over the realistic synthetic corpus
     (decision 6), where P is the planted count declared by the corpus generator (asserted equal,
     not read off the run).
  4. **The two example plugins' own suites exit 0** in the `go` job against in-process fake LLM and
     fake ClickUp servers, with synthetic fixtures only.
  5. **The arming gate (O15) passes on a FRESH held-back set**: `redact-heldback <cases.jsonl>`
     (`internal/redact/cmd/redact-heldback`, built, not `go run`) exits 0 — at least 90% of leaks
     caught AND at most 15% of clean lines damaged, both controls having behaved — over a case set
     an AUDITOR wrote for the redactor commit being armed, that the fixer never saw, and that was
     not reused from an earlier round. The exit code is mechanical; the set's freshness is a named
     human judgement over named evidence: the PR comment recording the run names the set's author,
     the redactor commit, and the `leaks caught=X/Y clean damaged=A/B` line verbatim.

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
  | **(n) the client ledger counts (O11)** | a session that WROTE `alpha-notes` READS `beta-notes` only through `"$BIN" sessions --scope beta-notes`, where `BIN` holds the client's path — no `cairn` token in the command line, no header in the output — with the session id in its environment; the client writes a ledger record, the agent uploads it, and viewer Q (reads `alpha-notes` only) gets the no-transcript bytes while R (reads both) sees it | the pod ignores the uploaded ledger | S11 |

  The sabotages run on a scratch copy of the tree with its `.git` removed (the
  `tests/control_mutants.py` pattern), and each must be caught by ITS clause's own assertion. Each
  clause also has a Go-side mutant row (listed under Slices) where the guarded code is Go.

  ⚠ **What the closing condition does NOT require**, stated so nobody reads it in: (i) any real
  deployment having transcripts; (ii) the operator judging summary QUALITY or match PRECISION —
  both are human judgements over real data and are post-close rollout (below); (iii) a real LLM
  provider or a real ClickUp workspace ever being called by CI. Redaction RECALL on real data is not
  measurable by any check that may run in this public repository (R6).

### Rollout (NOT part of the closing condition)

- 🔴 **Nothing is armed before closing-condition part 5 holds for the redactor being armed (O15)** —
  not the personal instance, not one host. A rule change after the gate passed resets it: the next
  arming needs a NEW held-back set scored against the new commit.
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
- **Measured in S0 (same host, same version), and each one moves decision 5:**
  - 🔴 **An export TRUNCATES when its stdout is a PIPE.** Eight of eight recent sessions stopped
    somewhere between 8 and 96 KiB — the boundary VARIES (8, 64 and 96 KiB here, 16 KiB in a reviewer's run; not a closed set) — with
    **exit 0** and an unterminated document; the same exports written to a
    REGULAR FILE were complete and parsed (about 0.9 to 18 MB). The 28,051-byte export above was
    under the boundary, which is why it measured clean. A reader sends the export to a file and
    refuses a document that does not parse; the exit code proves nothing.
  - **No part carries `time_updated` in the export** (the database column exists; the export
    omits it), so the watermark above cannot be read from an export: a part's version is
    `(id, digest of its bytes)`.
  - **`opencode session list --format json` lists the CURRENT project's ROOT sessions only**
    (157 listed against 614 root sessions over 17 projects in the database), keyed
    `{id, title, updated, created, projectId, directory}`. A child is found through its root: a
    `task` tool part's `state.metadata.sessionId` named the child for 45 of 45 children across six
    roots, and the child's own export carries `info.parentID`. No `subtask` part appeared in the 120
    most recent exports.

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
  from text for THIS plan: storage is identical, but redaction cannot apply. The operator decided
  it SHIPS anyway (O12), so a secret inside an image, a PDF or non-UTF-8 text is an uncontrolled
  residual (T1). *Revisions 3–16 withheld it pending Q2; retracted.*
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
REAL recall is an operator-side measurement: Q4 (adopted, O13) is a host-local, never-committed audit
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
  the scope to `V` (decision 3); it never removes anything.
- **Other paths, with weaker signals:** the Go client's verbs that take `--scope`/`--repo`
  (`recall`, `search`, `sessions`, `arcs`, `arc-show`, `validate`, `ls-entries`, `arc-register`,
  `append`, `put`, `create`; `internal/client/cli.go:47-133`) visible as a command line with
  `--scope`/`--repo`/`--all-scopes`, or with NEITHER, in which case the scope is derived from the
  working directory's repository (`internal/client/reposcope.go:71, 89`). Only `report`'s recall,
  search and the line at `renderer.go:158` render a `subsystem-recall:` header; every other verb
  above is **header-less**, so in the TRANSCRIPT the command line is its only trace — which is why
  decision 3 does not read the transcript for it at all but the client's read ledger (decision 3a,
  O11) (an `append` also leaves a trailer, which `W_trailer` sees). *Revision 13 left "the command
  line is the only signal" standing here; with the ledger it was stale.* And a direct file read of the local cache by path. `--all-scopes` (`search` and
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
| O9 | "Full raw" means EVERY byte — bookkeeping records, duplicate tool-result copies and subagents included. Because agents will be the main readers, selective reading is designed on top: a skeleton, filters, ranges, collapsed bookkeeping and duplicates by default, bounded pages; and a UI that collapses subagents and tool calls, truncates long assistant responses behind a reveal, and NEVER truncates a user message. | No storage-time truncation or per-session cap (decision 15); read-time limits only (decisions 17, 18). One agent-side exception, stated where it lives and in the Goal: held sessions stay on their host (decision 16). Binary content ships (O12). |
| O10 | Sessions with no recorded write ARE shipped, visible to their OWNER only, and a "My sessions" page lists them. | Clause (g); S7. |
| O11 | Replace the shell-command-line parser for READ scopes with a read ledger the `cairn` client writes itself, plus a simple fail-closed fallback. | Both clients change (the Python oracle while it lives), and a new parity row; decisions 3 and 3a, S11. |
| O12 | Answers Q2: content no redactor can read — images, PDFs, other binary payloads — SHIPS, every byte. The redactor scans every text-decodable string (base64 that decodes to text included) and leaves binary content as it is. **The coordinator's reading (revision 18, reversible):** O12 meant images, PDFs and binary PAYLOADS, so "binary" is a value whose bytes begin with a KNOWN binary file signature (or a base64/`data:` payload decoding to one); NUL-separated text and mostly-UTF-8 text with stray invalid bytes are TEXT and are scanned. *Reason: under revision 17's rule a `/proc/<pid>/environ` dump or an `env -0` listing — NUL-separated, all secrets — classed as binary and shipped unscanned; the narrowing costs nothing O12 asked for.* | Decision 6a's withholding is DELETED; binary is still classified, for collapsed display (decisions 17, 18). T1 names the residual: a secret inside an image, a PDF or another signature-bearing payload ships unredacted. |
| O13 | The coordinator stated these recommended answers to the operator as the defaults it would take before the operator answered O12 and O14; the operator did not object. They cover Q4 (a host-local, never-committed, counts-only redaction audit), Q5 (the client instance is NOT armed without an explicit operator decision), Q7 (one capture owner per instance), Q8 (ClickUp ids only, titles behind a no-default flag), Q9 (keep account identifiers), Q10 (plugins on a user timer on one operator host), Q11 (an `AGENTS.md` row is paid for by evicting an equal amount of history to a README in the same change), Q12 (per-session directory, one replica) and Q15 (the proposed Go-only `transcript` verb). | Each question below is marked "recommendation adopted"; Q13, Q14 and Q16 stay open. |
| O14 | Answers Q6: retention 90 days; an instance quota of about twice the steady state — about 20 GB compressed per capturing host on the personal instance — re-measured after a month of real uploads. | Implemented in S3 (`-transcript-retention`, `-transcript-quota`); decision 15. |
| O15 | After S1's third review round, the redactor changes approach: redact by KEY CONTEXT (any value attached to a secret-sounding name — a key, a flag, an environment variable, a config field, SQL's `IDENTIFIED BY`) plus ENTROPY (long random-looking tokens), retiring or narrowing the per-format rules those two subsume and accepting more damage to clean text, within a ceiling. Tool line prefixes are normalised FIRST, so every rule sees a line's content, with redaction still applied to the original bytes. And an arming gate: capture is armed on NO instance until a FRESH held-back set — written by an auditor, never seen by the fixer, replaced every round — shows at least 90% of its leaks caught AND at most 15% of its clean lines damaged. | Decision 6 (S1: `internal/redact/{normalise,keyed,entropy}.go`); closing-condition part 5; the S3 arming precondition; Rollout. The gate is `internal/redact/heldback.go` + `cmd/redact-heldback`. *As built (revision 21), and no wider: "any value" means any value whose SHAPE is not code, a placeholder or prose, at any length (revision 20's refusal of a bare run over 1 KiB is retracted); a name whose secret word is a segment rather than its end — and the abbreviation `cred`/`creds` — takes only a value that itself looks like a credential. Decision 6 states the refusals and T1 their measured cost.* |

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
   - `L(s)` = **the client READ LEDGER (O11).** Both `cairn` clients — the Go `cmd/cairn` and,
     while it lives, the Python oracle — append one record per SERVED scope access to a local,
     append-only ledger whenever a session id is present in their environment, and the capture
     agent uploads the session's records beside its transcript (decision 3a). `L(s)` is the set of
     scopes those records name, `*` included.
   - **The fallback, fail closed and deliberately coarse** — two rules, both over the whole root
     session (every stream), neither parsing a command line:
     - **F1, empty ledger:** if ANY tool input in the session contains the substring `cairn` or
       `subsystem-recall` (case-insensitive) and the session's ledger holds ZERO records, `V` gets
       `*`. Over-matching (a session that only edits files under a `cairn` directory) costs
       owner-only, which fails safe.
     - **F2, cache-path reads:** if ANY tool input contains the substring `subsystem-store`
       (case-insensitive) — the client cache root's directory name
       (`internal/client/readstore.go:63-71`; instance caches are siblings,
       `internal/client/instances.go:122-131`) — `V` gets `*`, because a file read from the cache
       never passes through the client and so is never ledgered. A config path under
       `~/.config/subsystem-store/` over-matches too; that fails safe. ⚠ F2 does NOT see a local
       store copy at a path without that name — a `--cache <dir>` root (`internal/client/cli.go:339`)
       or a `CAIRN_MIRROR_ROOT` mirror (`cli.go:834`) read by file tools; T3 names it.
     A call is never matched to "its" ledger record: the rules ask only "is the ledger empty" and
     "does any input name the cache", which is what keeps the shell grammar out of the design.
   - *DELETED in revision 13 (O11), with the ledger in its place:* the shell-command-line parser
     revisions 8–12 built for `U_calls` — candidate-token detection, the segment splitter,
     redirection stripping and the redirection-target rule, the disqualifying-character rule, the
     argument exemption, conditions (a)–(c), the `--scope` spelling union, the cwd-derived and
     `--all-scopes` command rules, and the `ls-entries` special case (the ledger records what
     `ls-entries` actually walked). Every sentence revisions 8–12 wrote about them, here and in
     the history entries above, is retracted; five audit rounds each found a new shell form that
     parser got wrong, and a ledger the client writes about itself has no shell form to get wrong.
   - *DELETED in revision 7, with no replacement:* the exception under which a header in a "simple"
     `cairn recall|search` call's own result cancelled the `*`, the allowlist defining "simple", the
     PENDING set `P` that waited for late results, and its 24 h backstop (round 6 of the audit
     measured it admitting 0 of 597 no-`--scope` Claude Code calls and 5 of 24 opencode ones).
   - `D(s)` = scopes the capture agent declares at upload — the same `L ∪ R_header ∪ F` it computes
     for routing; it can only ADD to the pod's set.
   - `V(s) = W_trailer ∪ R_header ∪ L ∪ F ∪ D`, over the root and every child stream (decision 7),
     where `F` is `{*}` when F1 or F2 fires and empty otherwise.
   - **One function, two callers, the SAME answer.** `R_header` and `F` come from ONE package
     (`internal/transcript/scopeuse`), imported by `cmd/cairn-capture` (routing, decision 16) and by
     `cairn-ui` (which re-derives over the STORED records and ledger at upload, so an old or lying
     agent cannot shrink `V`). Neither resolves a working directory or parses a shell line.
   - **What can go wrong, and in which direction.** A forged trailer, a quoted header in prose, a
     spurious ledger record, or an over-matching F1/F2 only ADDS, hiding `s` from more people. The
     ledger is SELF-REPORTED by the host, like the trailer: a session (or anything on the host)
     that edits or deletes its ledger can only make `V` SMALLER — and an emptied ledger trips F1 if
     any input names the program. The ledger CLOSES the old parser's residual of invocations whose
     program name never appears in the command line (a variable, a script, a shell function) —
     but only for a LEDGER-WRITING client that SEES the session id: the client records the read
     however it was invoked, provided it is current and the id reached its environment. What
     remains is threat T3.
   - **Unknown names fail closed.** A scope name in `V` that the control model does not know
     (renamed, deleted, on another instance) and the sentinel `*` are unreadable by everyone but the
     owner.
   - **Cost.** `R_header`, `L`, `F` and `D` are computed once per upload and stored in
     `meta.json` (grow-only). `W_trailer` changes whenever anyone writes a trailer, so it is a
     whole-store walk at view time — unmeasured for this design. S4 benchmarks it beside
     `BenchmarkSessionPageAndScopeTabs`; if it costs more than the session page's own walk, S4 caches
     it per session keyed on a store-change signal, which S4 must identify and name (none is
     designated here).

3a. **The client read ledger (O11; this design is the agent's, under that decision).**
   - **When it is written.** Each client checks its environment for a session id —
     `CLAUDE_CODE_SESSION_ID` (MEASURED on this host: Claude Code sets it for tool commands, and in
     a SUBAGENT's tool commands it equals the ROOT session id, so subagent reads attribute to their
     root, matching decision 7) or `OPENCODE_SESSION_ID` — **measured in S0: opencode does NOT set
     it.** The 1.18.29 bundle carries no such string (the same search finds
     `OPENCODE_DISABLE_AUTOUPDATE` in it — the positive control). On the measured host it is
     exported by the operator's OWN `shell.env` plugin from the hook's `sessionID` argument — the
     session running the tool, so for a subagent the CHILD's id by that code (read, not run: a live
     `opencode run` probe was refused by the measuring agent's sandbox). A child id is mapped to
     its root by decision 7's recorded child ids, and a host without that plugin has no variable,
     which F1 covers. No id → no write.
   - **What a record holds.** One JSON line per scope ACCESSED by a served call: `{schema,
     session, verb, instance, scope, client, client_version, at}`, where `scope` is the RESOLVED
     scope the call actually touched — not the flag as typed — for these verbs: `recall`, `search`,
     `sessions`, `validate`, and the writes `append`, `put`, `create`,
     `arc-register` (so an unattributed `put`/`create` is covered too); `ls-entries` writes one
     record per scope it listed, per instance; `--all-scopes` and any widening the client cannot
     enumerate write `*`. **Verbs that print store-derived content WITHOUT touching one resolvable
     scope write `*`:** decision 17's `transcript skeleton|records|tool` (they print ANOTHER
     session's transcript, whose `V` may hold any scope), `doctor` (per-scope visibility and
     counts), `routes` (the scope→instance table), and **`arcs` and `arc-show`**, which print
     content homed in OTHER scopes: `arcs --scope beta-notes` lists inferred arcs as
     `<home>/<slug> · …` with homes elsewhere (`internal/report/arcs.go:236-239`), and `arc-show`'s
     declared-scopes line (`arcs.go:453`) and each member's "wrote in:" list (`MemberLine`,
     `arcs.go:406`, rendered at `:457`) name other scopes. *Revision 15 cited `:454`, which is
     `MemberWritesLine` — a count, not a scope list; corrected.* *Revision 14
     listed them as recording only the resolved scope; retracted.* One record per printed home or
     declared scope was NOT chosen: it needs the client to enumerate every scope its rendered output
     names, including summarised member lists, and a missed one fails open — `*` cannot. **The
     cost, stated:** any session that runs `doctor`, `routes`, `arcs`, `arc-show` or a `transcript`
     verb becomes owner-only (and held, with several instances configured). No hook in THIS
     repository runs `doctor` (`flake.nix` only names it in checks and usage); hooks in the
     operator's own tooling were not checked. `sync` writes nothing: it accepts and ignores
     `--scope` and prints only banners (`internal/client/verbs.go:69-70`), so no store content
     reaches the transcript through it. *Folding the read session's own `V(t)` into `V(s)` instead
     of `*` was not chosen: `V(t)` grows after the read (later trailers, later uploads), so `V(s)`
     would have to be re-derived whenever any session it read changes — a dependency graph across
     sessions, for a precision gain on a rare call.* *Revision 13 listed only scope-touching verbs,
     so a `transcript` read wrote NO record and, with F1 off, `V(s)` missed the content it printed;
     retracted.* No content, no arguments, no query text — the ledger holds scope names only, so it
     needs no redaction.
   - **Where.** `$XDG_STATE_HOME/cairn/read-ledger/<session>.jsonl` (default
     `~/.local/state/cairn/…`), directory 0700, file 0600, one `O_APPEND` write per record. No new
     environment variable: tests point `HOME`/`XDG_STATE_HOME` at a scratch tree, so the env-alias
     ledger does not move.
   - **Invisible to the client's contract.** The write is best-effort: any error is swallowed —
     nothing on stdout or stderr, no change to the exit code, no added latency beyond one local
     append. That is what keeps `tests/parity/` (which diffs stdout, stderr and exit) unchanged; a
     new parity row runs both clients with a session id set and compares their LEDGER FILES
     (timestamps normalised), so the two clients cannot record different scopes. **The existing rows
     must run with no session id, and today they would NOT on an agent-run host:**
     `tests/testlib/env_pin.py:100-106` clears only the `SUBSYSTEM_STORE_`/`CAIRN_` prefixes and the
     host labels, so `CLAUDE_CODE_SESSION_ID`, `OPENCODE_SESSION_ID` and `XDG_STATE_HOME` leak into
     every client subprocess — local and CI tiers would diverge, and a test that leaves `HOME`
     unchanged would write FIXTURE scopes into the developer's REAL session ledger, making that
     session owner-only or held. S11 adds all three to `env_pin`'s cleared set in the same change
     that adds the ledger; until then the risk is named here rather than incurred. *Revision 13
     said the existing rows "run with no session id"; that was false on agent-run hosts.* The
     Python half joins the P8 retirement ledger.
   - **Upload.** The capture agent reads `<session>.jsonl` for the root and every child id and
     ships the records as a `ledger` stream of the root (decision 15's CAS and caps); the pod folds
     them into `L(s)`.

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
     streams; every persisted `tool-results/*` file as a BLOB of its stream (text redacted, binary
     byte-identical — decision 6a, O12). opencode through
     `opencode export <id>` as a subprocess by bare name (the `gitMinimal`-on-`PATH` precedent for
     `git`) with its stdout sent to a REGULAR FILE — a pipe truncates it at exit 0 (R3, measured in
     S0) — refusing a document that does not parse, and diffing parts by `(id, digest of the part's
     bytes)` against local state, because the export carries no `time_updated`. Root sessions come
     from `opencode session list --format json` run in each configured project directory (it lists
     only the current project's roots); children from the roots' `task` parts. It never opens the
     SQLite file, so the `account`/`credential` tables are structurally out of reach. *Revision 1
     excluded persisted binaries; revisions 3–16 withheld them pending Q2; O12 ships them.*
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
   - 🔴 *The approach since O15 (revision 19):* tool line prefixes (Read's numbered copy, grep's
     `path:N:`/`path-N-`/`N:`/`N-`/`path:`, a diff's `<`/`>`/`+`/`-`; since revision 20 also
     `path:N:C:`, docker compose's `svc | `, `kubectl logs --prefix`, a log timestamp and `git
     blame`, each recognised by STRUCTURE — a bare `path:` must look like a path, because `fix:`
     and `TODO:` are the same bytes in prose) are set aside FIRST — every
     prefix-stripped reading of the text is a view, the original among them, line-anchored rules run
     over every view, and matches map back to the original bytes. Then two general rules carry the
     table: KEY CONTEXT (a value attached to a name the one secret-name predicate accepts, in any
     notation — assignment, YAML, quoted keys, call arguments, flags, SQL, XML — *unless* the value's
     shape is code, a placeholder or prose, or the name is WEAK — its secret word a segment, not the
     end: `DB_PASSWORD_PROD`, or the abbreviation `cred`/`creds` — and the value is not itself
     credential-shaped: a word, an identifier or slug, a number, a timestamp, an address, a URL, a
     type or a regex. Since revision 21 a bare value of ANY length is taken, judged by its first
     64 bytes; a digits-only value of four or more digits is taken under a strong name outside
     code notation; and what reads as code is decided by evidence about the LINE — an ALL_CAPS
     name glued to `=` is an environment assignment whatever follows the value, a name that starts
     its line before a spaced `=` and a value that ends it is the INI layout, and a dereference is
     read only over an operand that looks like an identifier) and ENTROPY (a long
     run of the base64 alphabet with all three character classes that is not wordy, not an
     identifier/path/digest/transcript ID, and not a binary payload's encoding). The per-format
     dotenv, source-literal, libpq, `docker -e` and `.npmrc` rules are RETIRED into key context; the
     query rule is narrowed to names that carry a credential without saying so; short CLI flags
     are a closed per-tool list. Vendor formats, positional files, PEM blocks and the YAML/JSON
     structure stay — and since revision 21 the two positional files are read only with their
     file's STRUCTURE: a `.pgpass` row needs a host-like, a port-like, a database and a user field
     (five colon fields with a number second is also `path:N:C:` in front of a comment), and a
     `.netrc` password that is a plain word needs a `machine`/`default` record in its block, a
     netrc file named by the line's prefix or the lines above, or — the lone line — no prefix but
     a line number and a word that is not an attribute of a password. `internal/redact/README.md`
     is the inventory and the measured costs.
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
     **A `data:` URL is parsed first:** the `data:<media type>[;base64],` prefix disqualifies the
     whole string from the base64 alphabet, so a WHOLE-string `data:…;base64,<payload>` value has
     its payload decoded and scanned exactly as above (`data:text/plain;base64,…` included); a match
     replaces the whole URL. *Revision 17 swept the `data:` text out of decision 6 with the
     withholding, leaving no mechanism for `data:text/…` payloads; restored (S1 implements it).*
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
     caught=P" plus `clean-damaged=0` on a clean corpus of UUIDs, commit SHAs, digests, image
     payloads and thinking signatures (a redactor that eats every hash makes transcripts
     unreadable; that is a failure too). *Since O15 a random base64 value in prose is a PLANT (the
     entropy rule's), not clean filler.*
     A textbook example key is NOT in the corpus.

6a. **Blobs and binary content: text is redacted, binary SHIPS byte-identical (O12).**
   *Revisions 3–16 WITHHELD unredactable binary pending Q2 — a placeholder naming type, size and a
   keyed digest, a closed file-signature list deciding what counted, and tests asserting each
   carrier withheld. O12 answered Q2 "ship", so all of that is DELETED, not narrowed.*
   - **The binary rule (revision 18, the coordinator's reading of O12):** bytes are BINARY when they
     BEGIN WITH A KNOWN BINARY FILE SIGNATURE — PNG, JPEG, GIF, WebP, PDF, ZIP, gzip, bzip2, xz,
     zstd, 7z, ELF; a closed, tested list — or are a base64/`data:` payload that decodes to such
     bytes. **Everything else is TEXT and is scanned**, including NUL-separated text (a
     `/proc/<pid>/environ` dump, `env -0`, `find -print0`: split on NUL and each segment scanned) and
     mostly-UTF-8 text with stray invalid bytes (scanned with the invalid bytes preserved; only
     matched spans are replaced, so every other byte stays identical). A UTF-16 text with a byte
     order mark is decoded, scanned and re-encoded. *Revision 17's rule — no NUL in the first 8,000
     bytes AND valid UTF-8 — is retracted: it shipped an environ dump unscanned.*
   - **Text blobs.** A blob that parses as JSON (or JSON Lines) is redacted by decision 6's
     DECODED-string traversal, exactly like a record, so a JSON-escaped secret inside a blob is
     caught; if nothing matches, the ORIGINAL bytes are stored; otherwise a JSON DOCUMENT is
     redacted IN PLACE (revision 21: only the string tokens that changed are rewritten — one hit
     used to re-serialise the whole document, 1,186 changed lines for four hits in this
     repository's one large fixture) and a JSON Lines record with a hit is re-encoded compact.
     Any other text blob is redacted as ONE string with the same table, keyed tags and the
     line-based `Secret` rule, and stored as the redacted bytes — byte-identical to its source
     exactly when nothing matched. The pod re-checks both kinds like a record (decision 6).
   - **Binary blobs and binary values ship as they are.** A blob that is binary by the rule above
     is shipped byte-identical. Inside a record, a string that is base64 or a `data:` URL whose
     payload is not text (an inline `image` block, its `toolUseResult.file.base64` duplicate, an
     opencode `state.attachments[].url`) is left untouched by every rule. A base64 or `data:` value
     whose payload DOES decode to text is scanned decoded (decision 6's base64 rule).
   - **The decoder:** Go's `encoding/base64`, trying `StdEncoding`, `RawStdEncoding` (unpadded),
     `URLEncoding` and `RawURLEncoding` in that order, after removing ASCII whitespace from the
     string — so padded, unpadded, URL-safe and line-wrapped encodings of a whole string are all
     decoded. Thinking `signature` values (≈5% of bytes, R1) and 64-hex digests are base64-alphabet
     strings that decode to arbitrary bytes; they carry no file signature, so they are scanned
     decoded — and no rule matches random bytes, so they ship untouched (`clean-damaged=0`).
   - **Residuals, stated rather than closed:** a secret inside an image, a PDF, an archive or any
     other signature-bearing payload ships unredacted (T1) — a PNG carrying a token in a text chunk
     included; text in an encoding other than UTF-8 or BOM-marked UTF-16 is scanned byte-wise and a
     rule matches it only where its bytes coincide; an encoded secret EMBEDDED in a longer string is
     not decoded (decision 6).
   - **What the reader still sees.** Binary content is CLASSIFIED (decision 17's `binary` class, by
     position) so the page and the skeleton can collapse it to one line naming its type and size
     (decision 18). That is display only; nothing is withheld.

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
    "Every byte" here excludes exactly one thing, decided elsewhere: sessions held on their host by
    decision 16 (for any of the reasons the Goal lists). Binary content is stored like any other
    byte (O12).
    - `<dir>/<root-session>/meta.json` (owner, host, runtime, the grow-only `R_header`/`L`/`F`/`D`
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
      the agent HOLDS and retries — bytes are delayed, never dropped. **O14 sets the values for the
      personal instance:** 90 days, and a quota of about twice the steady state (about 20 GB
      compressed per capturing host), re-measured after a month of real uploads.
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

17. **Selective reading for agents (O9): ONE read model, served as JSON on `cairn-ui` and as a proposed
    Go-only `transcript` verb.**
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
        `toolUseResult.file.base64` value inside the duplicate field — shipped, collapsed, O12);
        `unknown`.
      - **Text an AGENT wrote to a subagent is `agent-prompt`, never `human-text`:** a `text`
        block or string content of a sidechain `user` record (`isSidechain: true`, `agentId` set —
        every record of a subagent file), and an opencode CHILD session's `user` text. Only what a
        human typed into a ROOT session is a user message. *Revision 17 classed both as
        `human-text`; retracted.*
      - **opencode** — each PART: `text` of a ROOT session's `user` message → `human-text` (of a
        CHILD session's → `agent-prompt`); a `text` part with `synthetic: true` → `runtime`; `text` of an
        `assistant` message → `assistant-text`; `reasoning` → `thinking`; `tool` → `tool-call` +
        `tool-result` (its `state.input` / `state.output`) + one `binary` unit per
        `state.attachments[]` entry (shipped, collapsed, O12); a `file` part → `binary`;
        `step-start`/`step-finish`/`patch` → `bookkeeping`; `compaction` → `runtime`; a `task` tool
        part's `state.metadata.sessionId` → a child-stream link (measured, R3); anything else —
        `subtask` included, whose keys were never measured — → `unknown`.
      - The `binary` class is a DISPLAY label, by POSITION. Nothing is withheld (O12); a binary
        value in an undeclared position ships and renders in its position's class.
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
    - **The CLI:** the proposed `transcript` verb (`transcript skeleton|records|tool`) mirrors the
      three routes. *It is PROPOSED, not registered, and is spelled throughout this plan without the
      `cairn` prefix on purpose: `tests/test_no_scrubbed_identifiers.py` refuses any backticked
      `cairn <verb>` citation of a verb no shipped client registers, and that guard should keep
      meaning "this exists".* They talk to
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
      summarised by `agentType` and `description`; opencode children likewise at the `task` tool
      part whose `state.metadata.sessionId` names them (measured, R3). *Revision 17 left them
      anchored at a `subtask` part, whose keys were never measured; corrected.*
    - **Bookkeeping records and duplicate FIELDS** collapse into ONE `<details>` per turn ("N
      bookkeeping records, K duplicate tool-result copies") with a link to `view=raw`.
    - **Binary content** (decision 17's `binary` class) renders collapsed, as one line naming its
      type and size, with the bytes in the raw view. *Revisions 3–16 rendered a "withheld"
      placeholder here; O12 retracted it.*
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
                                                         # text, or binary bytes as they are (6a,
                                                         # O12); same CAS, frames, 409/422/507 rules
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
| **T1. A secret survives redaction and is stored** | The residual the operator accepted by choosing every byte (O1, O9). Controls: host-side redaction on decoded strings with a keyed tag (decision 6), the pod's refusing re-check (clause c), the realistic corpus (closing condition 3), per-host denylist, retention, per-session deletion. **What is NOT controlled:** secrets that are neither NAMED nor RANDOM-LOOKING (typed passwords in prose, an unnamed all-lower-case, hex or short token — O15's two general rules cannot see them; a novel token format that is long and mixed-case IS caught by the entropy rule); an UNNAMED password-manager password with symbols (the symbols split it into base64-alphabet runs under 20 characters: 0–1/200 caught at 16–32 characters, measured revision 20 — a symbol-token rule that caught 175/200 at 24 was measured damaging 275 tokens of this repository's own text and not adopted); a NAMED value the key-context rule refuses by design, each MEASURED (revision 21; `internal/redact/README.md` names the test that pins each): one whose shape is code, a placeholder or prose (symbol-bearing generated passwords 193–200/200 over seeds 4–8; every symbol-free generator 200/200); a WEAK name's value that is itself identifier- or slug-shaped, e.g. `DB_PASSWORD_PROD=tiger_2024` (0/200, pinned; random values under weak names 199–200/200); a hex constant of at most 8 digits, or a signed or decimal number, under any name; a digits-only value in code notation or under four digits; a letters-only value after a spaced `=`, and a bracket-led value the line does not close there; a symbol-led password in an indented `key: value,` whose operand does not look random (194–200/200); a `.netrc` password that is a plain word AND is alone AND is an attribute word, sits behind a quote, diff, compose or non-netrc path prefix, or follows a capitalised keyword (plain-word passwords with netrc structure or file context: 340/340 over 17 layouts); a `.pgpass` row whose database or user starts with a digit. *Revision 20 also listed "a bare value over 1 KiB" here; that refusal is retracted — such a value is taken (90/90 at 1 KiB to 64 KiB).* **And what redaction costs clean text, now measured on text nobody wrote for the purpose and pinned as a budget** (`internal/redact/budget_test.go`): the Go standard library's source, 3,027,865 lines — 10,285 changed at round 4, 11,182 at round 5, 4,790 at round 6, nearly all of them the library's own test keys, certificates and vectors; this repository's tracked text, 300,774 lines — 1,422, 1,419, 224, of which 19 are in files that are not tests or fixtures and are enumerated. Stated clean-text costs, each pinned: a line that is exactly `password <non-attribute word>`, bare or behind a line number; `grep -n` output over a line that is itself three colon-separated words; a weak name over a value that does look random (a key id after a `…_KID` name; pinned in `TestRoundSixWeakNamesNeedACredentialShapedValue`); **a secret inside an image, a PDF, an archive or any other payload that begins with a known binary file signature (a PNG carrying a token in a text chunk included) — that content SHIPS UNREDACTED by operator decision (O12, decision 6a, the coordinator's reading), because no text rule can read it**; text in an encoding other than UTF-8 or BOM-marked UTF-16 (scanned byte-wise only); an encoded secret embedded in a longer string (not decoded — caught as a token only when long and random-looking), a flow-style YAML `Secret` (decision 6), and anything stored BEFORE a rule existed — a rule added later does not rewrite stored records (B3 proposes a re-scan). |
| **T2. Confidential but non-secret content** (client business detail, personal data in a tool output) | Redaction does not address it at all; VISIBILITY is the only control (decision 4). Stated, so nobody believes the redactor covers it. |
| **T3. Under-counted `V` widens visibility** | `V` is writes over the whole store, plus the client read ledger (decision 3a), plus rendered headers anywhere in the content, plus declarations, re-derived on the pod, grow-only (decision 3); a header naming no scope and a ledger `*` add `*`; F1 adds `*` when any tool input names `cairn`/`subsystem-recall` and the ledger is empty, F2 when any input names the cache root `subsystem-store`; nothing cancels a `*`, no caller resolves a working directory or parses a shell line; `*` and unknown names fail closed; empty `V` is owner-only (clause g); a session touching two instances is held and an already-shipped prefix withdrawn (decision 16, clauses h and m). **The ledger is SELF-REPORTED by the host**, like the write trailer: a session or anything else on the host can edit or delete it, and that can only make `V` SMALLER — an emptied ledger trips F1 if any input names the program. **The residual:** (1) a session MIXING a ledger-writing client with one that writes no record — a pre-ledger client (an older pinned revision), or a call whose environment lost the session id (`env -i`, some `sudo` setups) — has a non-empty ledger, so F1 does not fire and the unrecorded call's scope is missing; (2) a file-tool read of a local store copy whose path does not contain `subsystem-store` — a `--cache <dir>` root (`internal/client/cli.go:339`), a `CAIRN_MIRROR_ROOT` mirror (`cli.go:834`), or a moved cache root; (3) a store or transcript read that bypasses the client entirely (a direct HTTP call to the pod, or to `cairn-ui`'s `/transcript/…` routes — decision 17's CLI verbs write `*`, a raw HTTP call writes nothing). *Revisions 8–12 listed, as the residual, invocations whose program name never appears in the command line (a variable, a script, a shell function); the ledger records the read however it was invoked, so that residual is CLOSED for a current, ledger-writing client that sees the session id (residual (1) is what is left when either fails) — retracted with the parser.* |
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
| **S0** | **Fixtures and shape ledgers.** `tests/transcripts/gen.py` emits synthetic sessions in BOTH formats from the measured key sets (R1–R3): main stream, two subagents, an opencode child, a compaction boundary, a `pr-link`, persisted tool output, mutated opencode parts, bookkeeping records and duplicate fields, a `user` record carrying a large `tool_result` block, an inline image block AND its `toolUseResult.file.base64` duplicate, an opencode `tool` part with a `data:` URL in `state.attachments[].url`, a binary (PDF and JPEG), a UTF-8 text, a UTF-16 text and a JSON text tool-result blob, thinking `signature` values and 64-hex digests (which must survive untouched), and every read path of R7 (a rendered recall in a tool result; one in a hook attachment with no command line; a store-wide search rendered `scope=(all scopes)` in a hook attachment; a bare header-less `ls-entries`; an explicit `--scope` read; a `--repo` read; a `cd … && cairn recall` chain). A shape test pins the generator's record types, block types and field names against `internal/transcript`'s classification table. The generator also emits a synthetic read-ledger file per session (decision 3a), including an EMPTY one beside cairn-naming inputs (F1). And S0 MEASURES, on a host with opencode, whether `OPENCODE_SESSION_ID` reaches tool commands and which session id it carries, recording the answer for S11 — **done: decision 3a and `internal/transcript/README.md`.** The generator writes ONE file, `internal/transcript/testdata/synthetic_world.json`, which Go tests materialise, so `onlyGo` needs one row; the table is `internal/transcript/classify.go`. | `tests/`; `onlyGo` if Go tests read the fixtures; README. | Test-only. |
| **S1** | **`internal/redact`** — its own rule table (decision 6), decoded-string traversal, text-blob redaction and the text/binary sniff (decision 6a), keyed tags, structural Secret rule, denylist loader; the corpus generator and the `planted/caught/clean-damaged` report; the behavioural containment test against leakscan's controls. | new package; `ok` floor; README. | Library only. |
| **S2** | **`cmd/cairn-capture`** and **`internal/transcript/scopeuse`** — readers (JSONL by offset, subagents, blobs — text redacted, binary byte-identical; `opencode export` to a file, diffing by part digest), `V` derivation (the `*` mappings of decision 3), watermark state, routing (decision 16), `--dry-run`, `--self-test`, `-verbs`. No upload yet. | `cmd/cairn-capture`; new packages; `./internal/transcript/scopeuse/` joins `control_mutants.py` `PKGS` (+ pinned count); `depspolicy.LinkedBinaryRoots` + its test; `flake.nix` `packages.cairn-capture` + a ledger check; `ok` floor; closing-condition part 3 step in `ci.yml`. | Inert: it sends nothing. |
| **S3** | **Transcript store + capture API.** `internal/transcript` (directory layout, CAS append, frames, ownership, quota, retention sweeper, deletion, pod-side `V` re-derivation), worker listener + ledger, `capture` token kind and `cairn-ui -issue-worker-token capture`, refusing pod re-check. Agent gains upload. `tests/plugins/e2e.sh` created with clauses (a), (b), (c), (h). 🔴 **Arming precondition (O15):** S3 may MERGE inert, but capture is armed on NO instance until closing-condition part 5 holds for the redactor commit being armed (≥ 90% of a fresh held-back set's leaks caught, ≤ 15% of its clean lines damaged); S11 is a second, independent precondition. | new package → `ok` floor, `control_mutants.py` `PKGS` (+ pinned count through `ci.yml` and `internal/control/README.md`); `cmd/cairn-ui` flags (`-worker-addr`, `-worker-tokens`, `-transcript-dir`, `-transcript-retention`, `-transcript-quota`) and tests; `ci.yml` e2e step. | Inert unless `-worker-addr` AND `-transcript-dir` are set. |
| **S4** | **Visibility + the session page shows content (O3).** `transcript.Visible` (decision 4) with `V` (decision 3); the collapsed transcript section on `/session` (decision 18); the owner arm finding O10 sessions; `GET /session/transcript` raw-record view. e2e clauses (d), (e), (f), (g — the visibility half). Benchmark of the whole-store `W_trailer` walk. | UI rows → hand ledger, `contentAuthority`, uiaudit targets + a synthetic transcript in the uiaudit world; mutant rows. | Read-only over S3; renders nothing when no transcript exists. |
| **S5** | **Plugin registry, toggles, plugin API.** `internal/plugins` (manifest validation, closed capability and output-type vocabularies, plugin token kind, the narrowing-only toggle fold + `-plugin-journal`, the stateless pending query); `/plugins` page + toggle POSTs on scope, arc and session pages. e2e clauses (i), (j). | new package → `ok` floor, `PKGS`; UI rows; `cmd/cairn-ui` flags (`-plugin-registry`, `-plugin-journal`); mutant rows; README. | Inert with no registry; every toggle OFF by construction. |
| **S6** | **Outputs: storage, rendering, deletion cascade.** Outputs stored per session; rendered labelled-derived (decision 11) on the session page and as a one-line summary on session rows; owner "delete transcript" POST cascading to outputs and edges; the capture `withdraw` route running the same cascade, and the agent's withdrawal on a routing change (decision 16). e2e clauses (k), (l), (m). | UI rows; worker-listener ledger (`withdraw`); `cmd/cairn-capture`; mutant rows; README. | Nothing renders until a plugin writes; the agent withdraws nothing until a routing answer changes. |
| **S7** | **"My sessions" (O10).** `GET /my-sessions`: the viewer's OWN sessions with a transcript (owner arm only), newest first, including owner-only ones, each linking to `/session`. e2e clause (g — the listing half). | UI row → ledgers, uiaudit; mutant row; README. | Read-only over S3/S4. |
| **S8** | **Agent read API and CLI (decision 17).** The classification table's skeleton, records and tool routes on `cairn-ui`; the proposed Go-only `transcript` verb (`transcript skeleton|records|tool`) over `CAIRN_UI_URL`. | UI rows; `internal/client/cli.go` verbs; `capability_ledger` `go_only`; `want-go-only-verbs.txt`; `tests/test_go_client_ledgers.py`; mutant rows. | Read-only; the pod and parity corpus untouched. |
| **S9** | **Example plugin A — summaries.** `plugins/summary` in a NESTED stdlib-only module (provider HTTP API over `net/http`, no SDK), host-side user timer, incremental per decision 13, its own spend cap, a fake provider in tests. Reads through `view=conversation` by default. | `depspolicy.DeclaredNestedModules`; `flake.nix` package; `ci.yml` step for its suite. | A separate binary; nothing runs until registered and toggled. |
| **S10** | **Example plugin B — ClickUp.** `plugins/clickup` in the same nested module: ticket list fetch (read-only token, 429-aware), deterministic matchers (decision 14) over transcript records (`gitBranch`, `pr-link`, URLs), commit messages and trailers from a host-local repo list, PR bodies via the host's own GitHub CLI, entry refs; LLM suggestions using plugin A's summaries when present (`output:read:summary` — the cross-plugin test of the abstraction). Synthetic ClickUp fixtures only. Then closing wiring: `sabotaged=14 caught=14`, measured floors, the `AGENTS.md` row with an equal eviction (Q11). | same nested module; flake package; `ci.yml`; `AGENTS.md`; READMEs. | Separate binary; inert until registered and toggled. |
| **S11** | **The client read ledger (O11, decision 3a)** in BOTH clients: the Go `internal/client` writes a record per scope a served call touched when `CLAUDE_CODE_SESSION_ID` or `OPENCODE_SESSION_ID` is set; the Python oracle does the same; a parity row compares the two clients' ledger files; `cmd/cairn-capture` uploads the `ledger` stream; the POD's fold of that stream into `meta.json` and `V` (moved here from S3: S11 adds the ledger, so it owns both ends); `tests/testlib/env_pin.py` clears `CLAUDE_CODE_SESSION_ID`, `OPENCODE_SESSION_ID` and `XDG_STATE_HOME`; e2e clause (n). **Must land before S3 is armed on a real instance** — without it every session that runs `cairn` is owner-only by F1. | `internal/client` (+ joins `control_mutants.py` `PKGS`, moving the pinned count and its enumerations); `internal/transcript` (the pod-side ledger fold); `tests/testlib/env_pin.py` (three cleared variables); the Python `cairn`/`lib/`; `tests/parity/` (a new row, and its README's P8 retirement ledger); `cmd/cairn-capture`; `ci.yml` e2e floor. NOT `internal/api` or `cmd/cairn-server`: the store POD (`cmd/cairn-server`) is untouched — the ledger fold lives in `cairn-ui`'s `internal/transcript`. | Inert without a session id in the environment; with one, the write is invisible to stdout, stderr and the exit code. |

**Mutant rows** (indicative names). The pinned count starts at **296**; the **51** rows below would
take it to **347** if every one lands as named (revision 7 deleted six and revision 13 eight, each
listed where it was; revision 13 added six for the ledger) — the pinned number is whatever the battery declares
at each merge, never this sum. S0, S1, S9 and S10 add no row to the authz battery (S1's guards are
measured by the redaction corpus; S9/S10 by their own suites).

- **S2 (4, `scopeuse`):** `scopeuse-all-scopes-header-names-a-scope`,
  `scopeuse-scopeless-header-dropped` (the `renderer.go:158` form dropped instead of `*`),
  `scopeuse-fallback-empty-ledger-trusted` (F1: a session with a cairn-naming input and an EMPTY
  ledger gets no `*`), `scopeuse-cache-path-read-ignored` (F2: an input naming `subsystem-store`
  adds no `*`). *DELETED in revision 13 with the parser (O11, decision 3):*
  `scopeuse-cwd-derived-command-dropped`, `scopeuse-unrecognised-invocation-adds-nothing`,
  `scopeuse-unaccounted-candidate-token-ignored`, `scopeuse-argument-counted-as-candidate`,
  `scopeuse-disqualifying-character-ignored`, `scopeuse-ls-entries-scope-honoured`,
  `scopeuse-scope-spelling-missed`, `scopeuse-stripped-target-not-candidate`. *DELETED in
  revision 7 with the machinery they guarded:*
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
- **S11 (4, `./internal/client/` joins `PKGS`):** `transcript-pod-ignores-ledger` (n) (the pod's fold of an uploaded `ledger` stream into `meta.json` is skipped — moved here from S3 in revision 14, because S3 has no ledger fold and the battery runs `go test`, not the e2e, so the row was unkillable there), `client-ledger-write-skipped` (a served call with a session id in its environment writes no record), `client-ledger-records-requested-not-resolved-scope` (the record names the flag as typed instead of the scope the call resolved and touched), `client-ledger-write-visible-in-output` (a ledger write error reaches stderr or the exit code).

**Clause ↔ row ledger.** (a) cas · (b) none — its sabotage is in the AGENT's redaction call path,
which `PKGS` does not cover; it is measured by the e2e and the corpus · (c) both re-check rows · (d)
section · (e) viewer scopes · (f) ignores reads · (g) empty set · (h) none — guarded in
`cmd/cairn-capture`, not in `PKGS`; its Go-side control is S2's routing test · (i) default on · (j)
read ignores toggle · (k) output without predicate · (l) delete keeps outputs · (m) withdraw keeps
directory · (n) pod ignores ledger (S11).

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
- Binary content SHIPS (O12, decision 6a), one assertion per measured carrier, each BYTE-IDENTICAL
  after redaction: an inline `image` block's payload; its `toolUseResult.file.base64` duplicate;
  an opencode `state.attachments[].url` `data:` URL; a PNG that carries a token in a text chunk
  (the residual, pinned AS one). TEXT that revision 17 called binary is now scanned: a
  NUL-separated environ dump with a planted value is redacted segment by segment, a mostly-UTF-8
  blob with stray invalid bytes has only the matched span replaced (every other byte identical),
  and a BOM-marked UTF-16 dotenv blob is redacted. Around them the text rules still bite: a planted text secret in the
  same record as an image payload is caught, and a base64 value (or a `data:text/…` URL) whose
  payload DECODES TO TEXT carrying a planted secret is caught decoded. Controls: (1) dropping the
  signature check makes the token-carrying PNG's bytes scanned and altered — red on byte identity;
  (2) thinking `signature` values and 64-hex digests survive untouched
  (`clean-damaged=0`). Decoder coverage: a whole-string base64 TEXT secret encoded padded,
  unpadded, URL-safe, and line-wrapped at 76 columns is caught in all four forms (control: a
  decoder trying only `StdEncoding` without whitespace removal misses one of the four). Residual
  pinned AS a residual: the same encoded secret embedded after a line of other text in one string
  is NOT decoded — the test asserts that, so a later in-string decoder is a deliberate change that
  turns it red. *Revisions 3–16 asserted each binary carrier WITHHELD; O12 retracted that.*
- Keyed tag: two occurrences of one secret get one tag under one host key; two keys give two tags
  for the same secret; the tag is NOT the unkeyed digest prefix (control: computing
  `sha256(secret)[:8]` does not equal it).
- Containment: every realistic `credential` control string from leakscan's self-test is redacted —
  asserted as "the credential run inside leakscan's own match is gone", because redacting only
  the word `Bearer` breaks leakscan's match while leaving the token. Control: deleting the
  private-key rule makes this test red. *The plan named the GitHub-token rule as the control;
  S1 measured that deletion GREEN — leakscan's GitHub control is a `GITHUB_TOKEN=` line, which the
  dotenv rule also catches. S1's first build then used the authorization rule; the review round's
  short-bearer rule covers that control too, so it went green as well. The private-key rule is the
  only one reading leakscan's OPENSSH header control.*
- Review rounds 0–1 (S1): copy-prefixed lines (Read's numbered copy, `grep -n`, diffs); ONE
  `SecretKey` predicate, case- and style-insensitive; duplicate JSON keys and object KEYS scanned;
  NUL-separated, invalid-byte and BOM-UTF-16 text scanned (the narrowed binary rule); new shapes
  (PGP blocks, `redis://:pw@`, Slack `xapp-` and webhooks, `whsec_`, libpq `password=`,
  `docker -e`, `curl -u`, `X-Auth-Token`, `Authorization` in a JSON headers map, bearer ≥ 8, `.npmrc`
  `_authToken`, k8s env pairs); a code-shaped clean corpus at `clean-damaged=0`; a bounded
  private-key match; every `SelfTest` exit-2 branch tested; `Score` asserting each plant's own rule;
  and a fresh run-time attack set reported per class. The denylist glob's claim is NARROWED to the
  structured copies it covers — a Read `tool_result` copy and a `cat` are not joined to their path.
- Review round 2 (S1) measured round 1 making four things WORSE, and round 3 fixed each against
  the auditor's own tests, adopted as permanent regression tests: a private-key body read through a
  copy prefix (3/3 lines shipped → 0/3), the code filter refusing real passwords (13–78/200
  symbol-bearing and 0/200 dotted → 176–200/200 and 200/200, the clean-damage gain kept at 0/75 —
  *⚠ the 176 is RETRACTED (round 4): the test's body used a weaker oracle than its doc stated, and
  under the stated one, no 6-character window surviving, round 3's code measured 139/200 for the
  libpq string*),
  the END rule losing `PGPASSWORD`/`SECRET_KEY_BASE`/`apiKeyValue` (now glued words, a closed
  suffix set and a closed `<VENDOR>_KEY` list), and ASCII file magics letting text skip scanning
  (now a non-text byte in the first 1 KiB is required). It also closed pre-existing gaps: URL
  query and connection-string credentials, JWK private members, YAML block scalars, `.pgpass`,
  `.netrc`, source literals, `docker login -p`, value-first k8s env entries, BOM-less UTF-16. The
  corpus plants 72.
- Review round 4 (S1, O15): regression tests for every tool prefix round 3 found hiding a line
  (`N:`, `path:`, `N-`, `path-N-`, `<`, `>`), each in front of a value only a line-anchored rule can
  catch, and for PEM bodies behind each; for the named-key notations round 3 found unread
  (systemd `Environment=`, `os.environ[...] =`, `--password=`/`--password v`, `mysql -p<pw>`,
  `redis-cli -a`, `IDENTIFIED BY`, kubeconfig, XML, .NET); and for round 3's damaged clean probes —
  each shown RED on the pre-O15 code. The rate test now implements the oracle its doc states
  (symbol-bearing 195–199/200 in every shape, libpq included); the entropy rule's recall and the
  clean shapes it must spare are pinned (`TestTheEntropyRuleThresholds`). The corpus plants 79.
  The arming gate's own tests drive both bounds at their edges and every exit-2 branch.
  *Correction (revision 20): "195–199/200" held at seed 4 only; over seeds 4–8 round 4's code
  measured 192–200, and the README now states the range with the seeds it was measured at.*
- Review round 5 (S1, O15; `round5_test.go`, each shown RED on round 4's head unless labelled an
  invariant guard): the key-context finder in LINEAR time (a 4N/N timing ratio under 8, measured
  13.1–15.6 at round 4; plus a clock-free operation count); a stripped prefix-shaped word (`fix:`,
  `12:`, `> `) no longer exposing prose to the `.netrc` rule, whose own-line form now needs netrc
  structure or a non-word value; values that START with `*`, `&` or a printf verb outside code
  notation (200/200 each, from 0–42/200); `path:N:C:`, `N:C:`, docker compose, `kubectl
  --prefix`, ISO and syslog timestamps and `git blame` as prefixes, with a PEM block's SHORT final
  body line now bounded by its END line (it leaked with no prefix at all); a secret word as a
  SEGMENT of the name (`DB_PASSWORD_PROD`) and `creds`/`cred`/`privkey` (200/200, from 0/200);
  `mysql -p'pw'`/`-p"pw"`; an injective view-dedupe key. The self-test's 78/79 on about one seed in
  six was the SCORER, not the redactor (a plant's `&` was JSON-escaped in its record, so its rule was
  never credited); with that and two rule fixes, 0 of 400 seeds fail.
- Review round 6 (S1, O15; `round6_test.go`, each shown RED on round 5's head unless labelled an
  invariant guard or a cost pin; `budget_test.go`). **Every heuristic is measured in both
  directions** — recall on generated secrets, damage on neutral text — and the damage is PINNED:
  every line of this repository's non-fixture files that the redactor changes is enumerated (a
  new one fails), fixture files and a sample of the Go standard library's source are held to
  ceilings, and the pgpass rule to a ceiling over this repository's lines behind grep prefixes.
  The cases: a named bare value at 1 KiB, 2 KiB and 64 KiB (90/90, from 18/90) with the finder
  still linear (two operation counts and two timing ratios); `.netrc` plain-word passwords from
  structure or file context (340/340 over 17 layouts, from 80/340) against prose (0/35 damaged,
  from 7/35) with the residuals pinned in both directions; a weak name's value held to a
  credential shape (0/33 clean probes damaged, from 21/33; random values 199–200/200; the
  identifier-shaped cost pinned at 0/200); identifiers the entropy rule had begun to redact
  (the rule's lines over the standard library 4,826 → 5,687 → 4,617, its recall table unchanged);
  a JSON document redacted in place; symbol-led values in the INI layout and before trailing
  punctuation (200/200 in seven of eight layouts, 194–200 in the eighth, from 0–43/200); a
  redactor that only corrupts the encoding scoring nothing (256 credits over 60 seeds, now 0,
  and run by the self-test as a third control); the arming gate's oracle not counting a
  redaction marker as surviving secret (a known-answer test); `.pgpass` structure (6 of 22,887
  neutral lines behind `path:N:C:`, now 0); a digits-only value under a strong name (0/200 at
  rounds 4 and 5, 200/200). 61 named mutants, each killed by its own guard's message; the
  harness's no-op control survives, as it must.

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
  replaced; a BINARY one is byte-identical to its source (O12, decision 6a). *Revision 2 asserted
  every blob byte-identical to its source, which contradicted redaction; retracted. Revisions 3–16
  made a binary blob a withheld placeholder; O12 retracted that.*
- opencode: from two recorded synthetic exports where 3 parts changed and 1 was added, exactly 4
  upserts are produced; control: diffing by id alone produces 1. A truncated export (a document
  cut at a 64 KiB boundary, exit 0 — the measured pipe failure) is refused and uploads nothing;
  control: a reader that trusts the exit code ships a partial session.
- **The SQLite file is never opened:** a test runs the reader with `HOME` pointing at a tree where
  the database path is a FIFO; the reader completes (it only calls `opencode export`, here a stub).
- `scopeuse`, with literal expectations, IDENTICAL for the pod and the agent. Headers: the
  hook-attachment recall with no command line → `beta-notes` (control: a reader of tool INPUTS
  only misses it); the hook-attachment search rendered `scope=(all scopes)` → `*` (mutant
  `scopeuse-all-scopes-header-names-a-scope`); a `renderer.go:158`-form header → `*` (mutant
  `scopeuse-scopeless-header-dropped`). Ledger: a session whose ledger holds `{alpha-notes,
  beta-notes}` → both, with no `*` even though its command lines are bare `cairn recall` calls in
  chains, redirections and wrappers (the ledger, not the line, is the read record). F1: a session
  with a `cairn ls-entries` input and an EMPTY ledger → `*` (mutant
  `scopeuse-fallback-empty-ledger-trusted`); the same session with one ledger record → that
  record's scope and no `*` (control: F1 is about emptiness, not per call); a session with no
  cairn-naming input and an empty ledger → nothing (F1 does not fire on an unrelated session);
  `CAIRN recall` with an empty ledger → `*` (case-insensitive). F2: a `Read` tool input on a path
  under `~/.cache/subsystem-store/` → `*` (mutant `scopeuse-cache-path-read-ignored`); and the
  agent's output for every fixture equals the pod's. *DELETED in revision 13 with the parser
  (O11):* every command-line fixture of revisions 8–12 — the recognition shapes and flake forms,
  the mixed line, the argument exemption and its limits, the zsh and expansion forms, `ls-entries
  --scope`, the `--scope` spelling union, the redirection-target and redirection-stripping cases,
  and the "not a blanket `*`" control. *DELETED in revision 7:* the simple-call, chained-call,
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
Closing wiring: `--self-test` prints `sabotaged=14 caught=14`; each sabotage fails ITS clause's
message; the `ok` floor, mutant count and e2e floor equal the counts measured on the merged tree;
`AGENTS.md` stays under its working budget.

**S11.** For each resolved-scope verb (`recall`, `search`, `sessions`, `validate`,
`ls-entries`, `append`, `put`, `create`, `arc-register`) with `CLAUDE_CODE_SESSION_ID` set and
`XDG_STATE_HOME` on a scratch tree: exactly the records for the scopes the call RESOLVED and
touched — a bare `recall` in a repository whose derived scope is `alpha-notes` records
`alpha-notes` (mutant `client-ledger-records-requested-not-resolved-scope`: it records the empty
flag value instead); `ls-entries` records every scope it listed; `search --all-scopes` records `*`;
a refused call (bad flag, unroutable scope) records nothing. With no session id: no file is
created (control: the mutant `client-ledger-write-skipped` writes nothing WITH an id). With the
ledger directory read-only: stdout, stderr and the exit code are byte-identical to a run with no
session id (mutant `client-ledger-write-visible-in-output`). Parity: the new row runs both clients
on one cache root with one session id and compares their ledger files (timestamps normalised) —
red if the Python oracle records a different scope; the existing rows are unchanged ONCE
`env_pin` clears the session-id variables (control: with `CLAUDE_CODE_SESSION_ID` and a sentinel
`XDG_STATE_HOME` exported in the harness's own environment, run the existing rows and search for
any `cairn/read-ledger` file under BOTH the sentinel `XDG_STATE_HOME` and each row's harness
`HOME` (`work/home`; the harness overrides `HOME` for every row, `tests/parity/harness.py:1210,
1382-1383`, applied last by `env_pin.py:147-165`, so an exported sentinel `HOME` would never
reach the clients). Before the `env_pin` change at least one file appears — the positive control;
after it, NONE under either root. A partial change that cleared `XDG_STATE_HOME` but not the
session id writes under `work/home/.local/state/cairn/read-ledger/` and is caught there. *Revision
15's sentinel-`HOME` wording could never go red; retracted.*) **Content verbs:**
`transcript skeleton`, `transcript records` and `transcript tool` (against a stubbed `cairn-ui`),
`doctor`, `routes`, `arcs --scope beta-notes` and `arc-show --scope beta-notes --slug x` each
write exactly one `*` record, even when their output names only `beta-notes`; `sync` writes none (control: a client
that records only scope-touching verbs writes nothing for `transcript records`). **Pod fold** (a
Go test in `internal/transcript`): an uploaded `ledger` stream naming only `beta-notes`, for a
session whose transcript carries no header and no trailer, puts `beta-notes` in `meta.json` and in
`V` (mutant `transcript-pod-ignores-ledger`). `OPENCODE_SESSION_ID`: S0 measured that opencode
does NOT set it — the operator's own `shell.env` plugin does, with the CHILD's id inside a subagent
by that plugin's code (decision 3a); S11 maps a child id to its root and pins that. e2e clause (n).

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

Each has a recommendation. **Answered (revision 17):** Q2 by O12 (binary ships); Q6 by O14;
Q4, Q5, Q7, Q8, Q9, Q10, Q11, Q12 and Q15 by O13 (recommendation adopted). **Still open: Q13, Q14 and
Q16.** S0 and S1 depend on none of them; S11 depends on none either. **Q14 and Q16 shape S2**:
hold-back, withdrawal and re-shipping (Q14), and the pod-side `*` rule, which lives in S2's
`scopeuse` package with its fallback mutants (F1, F2) and first RUNS on the pod in S3 (Q16). S2
builds the RECOMMENDED answers, and because S2 uploads nothing, a different answer changes S2's code
before any byte has left a host. No question blocks merging S0–S2; Q14 and Q16 must be answered
before S3 ships to a real instance. *Revision 3 said Q16 shapes S3; the rule's code and mutant are
in S2. Revisions 3–16 listed Q2 as shaping S2; O12 answered it.*

- **Q1. REMOVED (O9)** — subagent transcripts are shipped.
- **Q2. ANSWERED by O12: content no redactor can read SHIPS.** Images, PDFs and other binary
  payloads — bytes that begin with a known binary file signature, by the coordinator's reading of
  O12 (revision 18, reversible) — leave the host byte-identical; everything else is scanned as text,
  NUL-separated and invalid-byte-bearing text included, and BOM-marked UTF-16 is transcoded,
  scanned and re-encoded. The cost is T1's binary row, an uncontrolled residual. *Revision 17
  shipped NUL-bearing and non-UTF-8 text unscanned too; the narrowing retracts that.* *Revisions 3–16 recommended WITHHOLDING (a placeholder naming type,
  size and a keyed digest); that recommendation and the as-built withholding are retracted.*
- **Q3. WITHDRAWN (D5)** — the rule table is not shared with leakscan.
- **Q4. Real-data redaction audit. Recommendation adopted (O13):** a host-local script, never
  committed, that runs the rule table over the host's own transcripts and prints only per-rule
  COUNTS, run once before arming capture and after every rule change.
- **Q5. The client instance. Recommendation adopted (O13): the client instance is NOT armed
  without an explicit operator decision.** It holds client principals and client-confidential
  content, and plugins there send transcripts to an LLM provider. Do not arm capture there until
  the personal instance has run for a while, and require an explicit decision before any LLM
  plugin's scope toggle is set to `allowed` there.
- **Q6. Retention and quota. ANSWERED by O14** — the recommendation, as decided: 90 days and an
  instance quota of ≈2× the steady state — ≈20 GB compressed per capturing host on the personal
  instance — re-measured after a month of real uploads. The basis: every byte (O9), measured on one
  host, is ≈2.8 GB of JSONL per week (subagents included; persisted tool results and opencode not),
  ≈0.8 GB/week at the sampled 3.5×.
- **Q7. One capture owner per instance (the presence single-owner wall)? Recommendation adopted
  (O13): yes for the personal instance** — it also removes the session-id squatting case (decision 15, T5); the client
  instance's answer depends on whether anyone else will run capture.
- **Q8. Store ClickUp ticket titles? Recommendation adopted (O13): no by default** (ids only); a
  no-default flag allows titles per instance.
- **Q9. Account identifiers in records** (`ownerAccountUuid`, `accountUuid`, `credential_org`
  attachments). Not credentials, so redaction leaves them. Under O9 (every byte) they ship.
  **Recommendation adopted (O13):** keep them; whether the per-host denylist should carry them is
  the host operator's choice.
- **Q10. Where the example plugins run. Recommendation adopted (O13):** a user timer on one
  operator host for v1
  (the LLM key and ClickUp token stay there); a pod is the same binary later.
- **Q11. `AGENTS.md`.** It has 25 bytes of headroom. The new packages need one layout row
  (≈150 bytes). **Recommendation adopted (O13):** the PR that adds the row evicts an equal amount of
  history to a README, as the weight test's playbook prescribes — never by narrowing a rule.
- **Q12. Storage backend and replicas. Recommendation adopted (O13):** the per-session directory
  (decision 15) and one replica; if a second replica is ever needed, move transcripts to object storage or pgstore
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
  With the read ledger (decision 3a) a bare or `--repo` read records the scope it actually read,
  so it re-ships too; the session is HELD instead only when F1 or F2 adds `*` (Q16). *Revision 7
  called this re-ship "the common case" and revision 8 said a bare read always held; both are
  superseded by the ledger.* A session with empty `V` goes to the default instance, owner-only. This NARROWS "ship
  every session" only for held sessions, which exist only on their host. **Recommend** hold +
  withdraw + re-ship as written. Alternatives: (a) keep the prefix
  where it is and hold only the rest — the prefix stays visible under its own `V` there but is an
  incomplete transcript presented as the session; (b) ship the whole session to the instance holding
  its WRITES with the other instance's scopes still in `V` (owner-only there) — keeps the bytes but
  moves client-read content into the personal store, which O8 rules out; (c) delay all shipping until
  a session is idle — no withdrawal needed, but no continuous summaries.
- **Q15. Agent reads through `cairn` or a separate binary? Recommendation adopted (O13):** the
  proposed Go-only `transcript` verbs (decision 17): agents already run `cairn`, and the Go-only mechanism exists. The alternative
  is a `cairn-transcript` reader binary that leaves `cmd/cairn`'s ledgers untouched.
- **Q16. What still makes a session owner-only (and, with several instances, HELD).** Under O11
  the read ledger (decision 3a) records the scope a bare or `--repo` `cairn` call actually read,
  so **the bare-read cost is gone when the client is current and the session id reaches it.**
  *Revisions 7–12 charged every bare or `--repo` call `*` (round 6 of the audit counted 597 such
  Claude Code calls and 24 opencode calls on this host); that cost and every earlier wording of
  this question are retracted with the parser.* What remains: (1) F1 — a session that names
  `cairn` but whose ledger is EMPTY (a pre-ledger client, an opencode build that does not set
  `OPENCODE_SESSION_ID`, a call run with the environment cleared); (2) F2 — any input naming the
  cache root `subsystem-store`, including harmless mentions of the config directory; (3) a ledger
  `*` from `--all-scopes`, or from a content verb that writes `*` — `doctor`, `routes`, `arcs`,
  `arc-show`, or a `transcript` verb (decision 3a). **Options:** (a) **keep every client current** (both clients write
  the ledger from S11 on) and have callers read through the client rather than the cache files —
  **recommended**; (b) narrow F2 to `Read`-style FILE tool inputs under the cache root instead of
  any input — a later, separately measured change; (c) accept the remaining owner-only/held
  sessions and measure their share on the personal instance. Explicit `--scope` on callers is no
  longer needed for visibility.

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
- **The SHARE of real sessions F1, F2 or a `*`-writing verb (`--all-scopes`, `doctor`, `routes`,
  `arcs`, `arc-show`, `transcript`) makes owner-only or held** under the ledger (Q16):
  unmeasured — no ledger exists yet. Also unmeasured: which id `OPENCODE_SESSION_ID` carries
  inside a subagent, observed live (S0 read it from the operator plugin's code — the child's — and
  measured that opencode itself never sets it, decision 3a); whether
  `CLAUDE_CODE_SESSION_ID` reaches commands run under `sudo`, `env -i` or detached services; and
  how often sessions mix a ledger-writing client with a pre-ledger one (T3's residual (1)).
  `CLAUDE_CODE_SESSION_ID` itself was measured once, on this host, in a subagent's tool command,
  where it equalled the root session id.
- **What `opencode export --sanitize` removes**, and whether `opencode export` is safe to run
  against a database a live opencode process is writing (one export, one idle session).
- **Redaction recall on real transcripts** (R6) — deferred to Q4's host-local count.
- **The whole-store `W_trailer` walk's cost per view** — benchmarked in S4, not here.
- **The weekly rate of persisted tool results and of opencode growth.**
- **LLM cost per summary** and summary quality; **ClickUp API behaviour** (no request was made;
  rate limits are from the documentation).
- **The compression ratio at scale** (3.5× on a 30-file sample).
- **Sizes and effort.** Not estimated; nobody has measured these slices.
