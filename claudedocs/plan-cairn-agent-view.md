# Plan: "what the agent sees" — auditing cairn's agent-facing surface from the UI

This is a RESEARCH NOTE and a ranked PROPOSAL. Nothing in section 4 is built.

**Where the citations point.** cairn claims carry a `file:line` read off `origin/main` at
**`0355c7a`**. Claims about the operator's agent tooling cite paths in the public tooling repo
(written `<tooling>/<path>`); the deployed copies under `~/.claude/` and `~/.config/opencode/` were read
to confirm what is actually WIRED, which is not always what the repo contains.

**What the numbers are.** Every count below was produced on ONE operator host, from that host's
local Claude Code transcripts (`~/.claude/projects/**/*.jsonl`: 7,661 files, of which 6,794 are
subagent sidechains; roughly five weeks retained) and its opencode database (1,070 sessions). The
instruments counted command lines, flags and byte lengths; they never copied a prompt, a message,
a scope name or entry text, and nothing here quotes one. Examples use synthetic scopes
(`alpha-notes`). Operator decisions are paraphrased.

**Decision legend.** **[OPERATOR]** = chosen by the operator, not re-litigated here.
**[AGENT]** = this note's recommendation, open to review.

## Headline findings

1. 🔴 **NO HOOK INJECTS CAIRN CONTENT — IN EITHER RUNTIME.** Every cairn read an agent makes is a
   tool call the agent itself issued, mostly because a skill told it to. The deployed Claude Code
   wiring (`~/.claude/settings.json` → SessionStart, UserPromptSubmit, PreToolUse, PostToolUse,
   Stop, SubagentStop) and every opencode plugin were searched; none calls `cairn`. Over the whole
   transcript corpus, **0** attachment or system records carry the renderer's
   `subsystem-recall:` header, against at least **424** tool results that do (positive control; lone-command results only). A
   "preview of the SessionStart injection" therefore previews something that does not exist; see
   4.4 for what to do instead.
2. 🔴 **THE AGENT ALMOST NEVER READS THE WHOLE RECALL.** Of **665** tool calls whose command was a
   lone recall, **661** piped it through a filter (`head` 346, `grep` 129, `tail` 119, `sed` 65),
   and **no skill tells it to** (searched: no skill pipes a recall). The modal cap is `head -60`
   (205 of 509 capped calls; `-80` next at 85). Measured on one large scope read the way `/resume`
   reads it (`--repo`), the full digest is **51,531 B / 367 lines**, `head -60` keeps **6,122 B (12%)**, and only
   **3 of the 57** citation tokens the full digest prints survive the cut. Across the 29 scopes
   this host routes, **15** have digests longer than 60 lines.
3. 🔴 **THE v1 EXACT-RECALL TAB WILL NOT SHOW WHAT `/resume` SHOWS, IN THREE NAMED PLACES.** The
   dominant read is `recall --repo <path>` (1,355 invocations, against 613 `--scope`). With
   `--repo` and the default mode the client builds a FOCUS WINDOW from that repository's newest
   local handoff doc and features the entry it names
   (`internal/client/verbs.go:389-401`); with `--scope` it never does, so the featured entry is
   the newest file. In the agent's own recall outputs the handoff-resolved pick appears **150**
   times against **12** most-recent fallbacks. The other two deltas are the client-only state
   banner (`verbs.go:440`, stdout) and the `host:` line, which names the RENDERING host
   (`internal/report/text.go:310`, `verbs.go:30-35`) — the pod's in the UI, the agent's host on the
   CLI.
4. **Reads are invisible to the store by construction.** Every client syncs the WHOLE store
   (`verbs.go:318-321`), so the pod's audit line (`internal/api/server.go:1135-1165`) records
   `path=/snapshot` and a token fingerprint — never which scope was then read, or by which
   session. A server-side audit of "who read alpha-notes" has no data source today.
5. **A structural usefulness signal already exists and is barely exercised.** The `[cb:xxxxxxxx]`
   citation token (`internal/write/revision.go:99`; rendered at `internal/report/text.go:650`)
   appears in 52 tool results since it shipped, printing **198** distinct tokens across 31
   transcript files; **18** (9%) reappear later in the same file — 12 in the agent's prose, 9 in a
   non-cairn tool input, 4 in a later cairn command (sets overlap). The small denominator is
   finding 2: the cut removes most tokens before the agent sees them.
6. **The skills document a flag the default client refuses.** `<tooling>/claude/skills/resume/SKILL.md`
   step 4 and `resume/reference/cairn-recall.md` list `recall --search`, `-C`, `--all-scopes` and
   `--max-hits`; the Go client's `recall` accepts none of them (`cairn recall --help`). Of 15 lone
   `recall --search` calls, **11** came back matching a usage/flag-error pattern.

## 1. The trace — every path between an agent and cairn

### 1.1 Hooks and always-on context: none carry cairn content

| surface | wired where | calls cairn? | how checked |
|---|---|---|---|
| Claude Code hooks (8 events, ~25 commands) | `~/.claude/settings.json` → `~/.claude/hooks/*` (home-manager links into `<tooling>/scripts/claude-hooks/`) | **no** — the five files that mention cairn do so in comments | symlink-following `find -L … \| xargs grep`, with a positive control on a term the files must contain |
| opencode plugins (6) | `~/.config/opencode/plugin/*.js` | **no** | same; the first pass used `find -type f`, which skips symlinks, and returned a vacuous zero — corrected |
| global instructions | `~/.claude/CLAUDE.md`, `RULES.md`, `~/.config/opencode/AGENTS.md` | **no** cairn instruction | grep |
| skill LISTING (names + one-line descriptions) | injected by the runtime every session | names four cairn skills; carries no store content | — |

⚠ A third-party session hook binary is wired at SessionStart; its strings contain no `cairn`. That
is a weaker check than reading source, and is named as such.

### 1.2 Skills — the instruction surface that DRIVES the reads

All under `<tooling>/claude/skills/` (deployed read-only to `~/.claude/skills/` and
`~/.config/opencode/skills/`). Invocation counts are Skill-tool calls in the transcripts (Claude
Code, main thread unless stated) and opencode `skill` tool calls.

| skill | what it tells the agent to run | mandatory? | Claude calls | opencode calls |
|---|---|---|---|---|
| `resume` (+ `reference/cairn-recall.md`) | `cairn-ops/read.sh recall --repo <path>` at step 4 | 🔴 **UNCONDITIONAL** ("run both now") | 311 | 13 |
| `handoff` | the same `read.sh recall --repo`, then writes via `subsystem-index` | yes | 528 | 19 |
| `subsystem-index` (47.6 KB body) | `cairn append` / `create` / `put`; `cairn recall`, `sync`, `search`, `validate` around them | the write protocol | 487 | 67 |
| `analyze-service` | `cairn sync`; writes via `create` | on recon | — | 6 |
| two ops skills (an approval router's, `obs-read`) | `read.sh search '<term>' --scope … --if-available` as a best-effort preflight | best-effort | — | — |
| `prune-index` | `read.sh recall --ref`, `cairn put`, `hygiene.sh prune` | on demand | — | — |
| `cairn`, `cairn-read`, `cairn-write`, `cairn-hygiene` | doors/routers; `cairn doctor`, `health.sh instances` | by name | 14 / 2 / 7 / 0 | 2 / 0 / 1 / 0 |

### 1.3 CLI verbs and wrappers, as invoked

`cairn -verbs` lists 14 verbs: reads `arc-show arcs doctor ls-entries recall routes search sessions
sync validate`; writes `append arc-register create put`. The operator's tooling reaches them two
ways: bare (`cairn <verb>`, through a PATH wrapper — 1.4) and via `<tooling>/scripts/cairn-ops/`
(`read.sh`, `write.sh`, `health.sh`, `hygiene.sh`), which add a few refusals and exec the client.

Invocations counted by command-position regex (a verb counts once per command that names it;
command lines only, no content):

| verb | Claude bare | Claude via cairn-ops | Claude main / subagent | sessions (files) | opencode |
|---|---|---|---|---|---|
| `recall` | 1,331 | 539 | 1,742 / 128 | 569 | 45 + 6 |
| `append` | 1,320 | 415 | 1,707 / 28 | 530 | 74 + 6 |
| `sync` | 1,180 | 211 | 1,312 / 85 | 487 | 48 + 3 |
| `put` | 252 | 84 | 323 / 13 | 198 | 39 |
| `create` | 230 | 42 | 258 / 14 | 124 | 29 + 3 |
| `search` | 134 | 9 | 119 / 24 | 67 | 19 |
| `validate` | 129 | 91 | 157 / 63 | 95 | 3 + 14 |
| `ls-entries` | 108 | — | 56 / 52 | 49 | 4 |
| `doctor` | 75 | — | 46 / 30 | 40 | 2 |
| `routes` / `arcs` / `sessions` / `arc-register` | 72 / 29 / 6 / 6 | — | — | — | — |

Recall flags: `--repo` 1,355 · `--ref` 939 · `--scope` 613 · `--list` 374 · `--limit` 69 ·
`--search` 62 (refused, finding 6). **Drill-down:** of 569 transcript files with a recall, **268**
later ran `recall --ref` — the digest → entry path the skill describes is used about half the time.

⚠ The regex is a floor and has known noise (a handful of `cairn <word>` hits from prose inside
`echo`). It was validated by its own positive controls (non-zero counts on every documented verb)
and by agreement between two independent passes: `recall` main+subagent = 1,870 = bare + wrapped.

### 1.4 The PATH wrapper — a second, host-local instrument

`<tooling>/scripts/cairn-receipt.sh` (deployed by `<tooling>/nix/home.nix`) is the `cairn` on PATH: it runs
the real client with inherited fds and then appends ONE telemetry row per invocation to the host's
activity spool — `verb`, `--scope`/`--repo` (charset-stripped), exit code, duration, and the
runtime's session id. 🔴 **By its own contract it records NO output size, entry count or bullets
printed** (it may not read the client's stdout). It is the only per-read record that exists, and it
lives on the host, outside cairn.

### 1.5 Paths that are NOT agent reads (out of scope here)

Host presence agents and the ring notifier (`<tooling>/nix/cairn-presence.nix`,
`<tooling>/scripts/cairn-presence-push`, `cairn-ring-*`) talk to the UI's agent listener; `cairn-who`
and `cairn-validate` are operator tools. None renders store content into an agent's context.

## 2. What the agent actually sees

### 2.1 Shape of a recall answer (stdout, in order)

1. **State banner** — client-only, one of `cairn: live — fetched from … just now — N entries,
   snapshot …`, `cairn: live — already current at … — not modified` (a 304), `⚠ cairn: cached —
   … SERVED FROM CACHE, <age>, revision …`, or `🔴 … store-unreachable, no cache` (stderr)
   (`internal/client/state.go:139-183`, `verbs.go:440`).
2. **Header** `subsystem-recall: status=<…> scope=<resolved scope>`, the `host:` line, and a
   `caveat:` line saying this is recall, not observation (`internal/report/text.go:306-313`).
3. **Index** — one row per entry (ref, `N nuance`, `sensitivity=`, badges `🔴 N OPEN`,
   `🔴 N NEAR-MISS`, `⚠ N UNVERIFIABLE`, `🔴 NO <heading>`), newest first, 100 per page.
4. **One featured entry in full**, with the basis of the pick printed; every nuance bullet carries
   ` [cb:xxxxxxxx]`.
5. A `MALFORMED` block when entry files could not be indexed.

### 2.2 Size

Full digests, rendered locally from the synced cache for all 29 scopes this host routes (numbers
only, `--scope`): median **11.8 KB / 64 lines**; largest **54.6 KB (~13.7 K tokens at 4 B/token)**;
longest **219 lines**; smallest 4.0 KB. `--list` (index only): 2.2–9.3 KB. The `--repo` read of
headline 2 (51.5 KB / 367 lines) is longer than any `--scope` digest because its featured entry
differs — finding 3 again.

What reached the agent (byte length of the tool result, i.e. AFTER its own filter), Claude Code:

| mode | n | p50 | p90 | max |
|---|---|---|---|---|
| `recall --ref` | 318 | 5,491 B | 16,674 B | 29,224 B |
| `recall` (digest) | 286 | 6,855 B | 14,964 B | 28,840 B |
| `recall --list` | 46 | 2,175 B | 4,912 B | 6,272 B |
| `search` | 35 | 2,095 B | 7,434 B | 12,019 B |
| `append` (the write's echo) | 81 | 464 B | 1,978 B | 9,940 B |

opencode, same instrument: `recall` p50 2,499 B (n=37), `append` p50 667 B (n=61).
Over all lone recalls the agent received **4.66 MB** (~1.16 M tokens) across the window.

### 2.3 Truncation — the agent's, not cairn's

cairn never truncates silently: the index states `none omitted` or `entries 1–100 of N`, and
`--limit` prints a loud notice. The truncation is downstream: 661 of 665 lone recalls were piped,
with `head -N` caps p50 = 60. A `| tail`, `| grep` or `| sed` also drops the banner and header —
the `subsystem-recall:` header survives in **404 of 665** recall results (61%), the state banner is
absent from **151**. So in roughly a third of reads the agent cannot see whether its answer was live
or cached, nor which scope was resolved.

### 2.4 Staleness and caching

- The cache is per host, per instance (`~/.cache/subsystem-store` by default,
  `internal/client/readstore.go:63-71`); every `recall`/`search` syncs the whole store first, with
  a conditional GET (`state.go:130-160`), unless `--no-sync`.
- Banner states seen in recall results: **live-fetched 291 · live-304 103 · `--no-sync` 13 ·
  no-cache 1 · SERVED FROM CACHE 0**. The zero is a floor for the 151 results whose banner was
  filtered away; the pattern is the spelling in both clients (`state.go:182`, `cairn:1132`).
- `sync` is also run explicitly — 1,180 bare `cairn sync` calls — though every `recall`/`search`
  already syncs; how many of those were redundant was not measured.

### 2.5 Writes — what comes back

`append` (1,735 Claude calls) is the dominant write; its echo is small (p50 464 B). The writer's
session is SELF-DECLARED (`--session`, `internal/client/cli.go:120`), and only `append` stamps a
trailer, so a session that wrote through `put`/`create` is invisible to the session page
(already recorded in PR #208's plan, step 2).

## 3. Gaps that make the agent's view hard to audit today

| # | gap | consequence | who must change |
|---|---|---|---|
| G1 | No record of what a session READ: the pod sees whole-store snapshots only; the receipt row is host-local and carries no output facts | "what did cairn tell that session" is answerable only from the host's transcript | cairn (read ledger, #208 decision 3a) + capture (#208) |
| G2 | The agent's view ≠ the store's answer: self-applied `head`/`tail`/`grep` | a preview of the full answer overstates what the agent saw by up to ~8× on large scopes | the tooling repo (skills) and/or cairn (a budget flag, 4.3) |
| G3 | The featured pick depends on a LOCAL repo's newest handoff doc | the pod cannot reproduce the `--repo` digest; v1's tab shows the `--scope` digest | cairn (label it) — the window cannot move to the pod without the doc |
| G4 | Client-only lines (banner, `host:`) | "byte-for-byte" holds for the renderer's body, not the whole stdout | cairn (state it in the v1 tab) |
| G5 | Skills document flags the default (Go) client refuses (`recall --search`, `-C`, `--all-scopes`, `--max-hits`) | 11 of 15 such calls failed | the tooling repo (skills) |
| G6 | No hook composes context, so "what an agent sees at session start" is whatever the skill mandated, filtered by the agent | there is no single composed artifact to preview | the tooling repo (only if a hook is wanted, 4.4) |
| G7 | Usefulness has one structural signal (`[cb:]`) and it is mostly truncated away before it can be cited | "recalled but never cited" is unmeasurable per entry today | G1 + G2 first |

## 4. Proposal — what comes AFTER the v1 exact-recall tab, ranked

**[OPERATOR] v1 is chosen and in progress elsewhere:** an exact-recall tab on each scope page,
byte-for-byte `cairn recall --scope X` from the same renderer, with size and a token estimate.
Everything below builds on it. Rank is by value per cost; each item names its data source, cost,
authority rule, and whether cairn, the tooling repo or both must change.

### 4.1 [AGENT, rank 1] Make v1 honest about the three deltas, and show the agent's cut line

- **What.** On the v1 tab: (a) a line stating the tab is the `--scope` digest and that `/resume`'s
  `--repo` digest may feature a different entry (finding 3), naming the rule rather than guessing
  the entry; (b) the client-only lines rendered as a labelled placeholder, not faked; (c) a marker
  at line 60 (the measured modal cap, with 40/80 selectable) showing bytes, token estimate and
  `[cb:]` tokens ABOVE vs BELOW the cut.
- **Data source.** The renderer's own output; the cut lines are constants from this note's
  measurement, stated as such.
- **Cost.** Small — presentation over text v1 already has. One guard: the cut marker must not
  alter the rendered bytes (assert the body still equals `RenderText` output with the marker
  stripped).
- **Authz.** Same as the scope page (`classContent`, the viewer's narrowed set). No new data.
- **Change.** cairn only.

### 4.2 [AGENT, rank 2] A per-scope "agent budget" column on the browse index

- **What.** For every scope the viewer reads: digest bytes, lines, ~tokens, `--list` bytes, and a
  flag when the digest exceeds the cut line or a configured token budget. The operator sees at a
  glance which scopes are too big to be read whole by an agent (15 of 29 today).
- **Data source.** `report.Recall` + `RenderText` per scope on the pod — the same call v1 makes.
- **Cost.** Small–medium: one render per readable scope per page load. Unmeasured on the pod at
  the current store size; cache keyed on the snapshot revision if it is slow. Measure before
  shipping.
- **Authz.** Viewer's narrowed set only; a count about a scope the viewer cannot read is itself an
  answer about that scope, so it must not appear.
- **Change.** cairn only.

### 4.3 [AGENT, rank 3] Close G2 at the source so the preview and the agent agree

Not a UI item, but it decides whether every preview above is accurate. Two options; recommend (a):

- **(a) cairn: a deterministic budget on recall** — e.g. `--max-bytes N` that stops at an
  ENTRY/BULLET boundary and prints a loud `truncated: K of M bullets, re-run with --ref` notice
  (the house style `--limit` already follows). The skill then passes the budget instead of
  `| head`, the header and banner can no longer be filtered away, and 4.1's cut line becomes the
  real one. Needs parity rows in `tests/parity/` and the P8 ledger, since the Python oracle has no
  such flag.
- **(b) tooling repo only:** skill guidance "never pipe recall; use `--list` then `--ref`". Prose, so
  weaker (`RULES.md`: deterministic over prose) — and agents already pipe without being told to.
- **Also in the tooling repo:** fix G5 (remove or re-route the `recall --search` / `-C` / `--all-scopes` /
  `--max-hits` documentation to `cairn search`).

### 4.4 [AGENT, rank 4] Hook-injection preview — only if a hook is ever added, and then composed IN cairn

- **Finding first:** there is nothing to preview (headline 1). Do not build a preview of a
  hypothetical hook.
- **If** the operator wants session-start injection (replacing `/resume`'s mandate with a hook),
  recommend the composition live in cairn as ONE function — e.g. a `cairn context --repo <path>
  --budget N` verb, which the hook calls and the UI renders through the same code — so the
  preview is byte-identical by construction rather than by a second implementation in the tooling repo that
  the UI would have to mirror. Cost: medium (a new verb moves `-verbs`, the Go ledgers, parity and
  the P8 ledger together). Authz: identical to recall. Change: cairn (verb) + the tooling repo (hook wiring).
  **This is a Fork for the operator, not a recommendation to add a hook.**

### 4.5 [AGENT, rank 5] Per-session "what cairn returned" — after PR #208's capture lands

- **What.** On `/session`, a section listing each cairn READ the session made: verb, resolved scope,
  state (live/304/cached), bytes received vs bytes the store would have answered, the `[cb:]`
  tokens received, and the featured pick's basis — metadata, with the body behind a click.
- **Data source.** Two, and they disagree on purpose: (1) captured transcripts (#208), detecting the
  `subsystem-recall:` header and `[cb:]` tokens in tool results (#208 R7) — shows what the agent
  RECEIVED after its filter; (2) the client read ledger (#208 decision 3a) — shows what the client
  RENDERED. The difference between them IS finding 2, per session.
- **Cost.** Medium on top of #208; none of it is buildable before #208's capture slices.
- **Authz.** #208's intersection rule (decision 4) for the transcript; additionally, a read of a
  scope the viewer cannot read must render byte-identically to no read at all (no existence
  oracle, the session page's existing rule).
- **Change.** cairn (render) + the tooling repo (the capture shipper #208 already plans; the receipt wrapper
  could forward its row, but it carries no output facts by contract).

### 4.6 [AGENT, rank 6] Usefulness: "printed but never cited", per entry and per bullet

- **What.** For each entry: times its bullets reached an agent, times a token reappeared in that
  session's prose or a non-cairn tool input, last cited. Entries printed often and never cited are
  pruning candidates for `cairn-hygiene`/`prune-index`.
- **Data source.** 4.5's data. Baseline today: 18 of 198 tokens (9%).
- **Caveats, stated on the page.** `/resume` MANDATES echoing what it recalled, so citation in the
  report measures compliance as well as use (a ref-reappearance proxy saturated at 99.3% for that
  reason, per the operator tooling's own measurement); and a token cut by `head` cannot be cited,
  so until 4.3 lands the denominator must be tokens RECEIVED, not tokens rendered.
- **Cost.** Medium; a #208 plugin is the natural home (derived records over captured sessions).
- **Authz.** Per-scope counts only for scopes the viewer reads; the session list behind a count
  follows 4.5's rule.
- **Change.** cairn (plugin + render).

### 4.7 [AGENT, rank 7] Audit view: who read a scope

- **What.** Per scope: sessions/hosts/tokens that read it over a window, with verb and state.
- **Data source.** Only the read ledger (#208 decision 3a) can say it; the pod's audit log cannot
  (headline 4), and the receipt row is host-local.
- **Authz.** Owner/admin of the scope only — "who reads my notes" is more sensitive than the notes'
  existence.
- **Cost.** Medium; low priority until there is more than one reader.
- **Change.** cairn.

### Not recommended

- **A view of the SKILL instructions** (what the tooling repo tells the agent). It is the tooling repo's content, already
  readable where it lives, and mirroring it into cairn creates a copy that drifts.
- **Server-side reproduction of the `--repo` focus window.** It needs the repo's newest local
  handoff doc on the pod; shipping that doc to make a preview match is the wrong direction.

## 5. What could not be measured

- **The full answer behind each historical read.** Transcripts hold what the agent received after
  its own filter; the store's unfiltered answer at that moment is not recorded anywhere.
- **Whether a recalled bullet changed what a session did.** Citation reappearance is the structural
  proxy, and it is confounded by the `/resume` echo mandate (4.6).
- **Other hosts.** One host's transcripts and caches; the other host's usage shape is unmeasured.
- **The receipt telemetry stream** (1.4) was not queried; it would give a second, independent
  invocation count with exit codes and durations, but no output facts.
- **Pod-side cost of 4.2** at the current store size.
- **opencode subagents** are not separable in its database the way Claude Code sidechains are.
- **A third-party SessionStart hook binary** was checked by `strings` only.
- **Transcript retention** bounds the window to roughly five weeks; older behaviour is not
  represented, and the citation-token numbers cover only the days since it shipped.

## Method appendix

Three throwaway scripts (not committed): a JSONL walker that matched cairn invocations in Bash
`tool_use` inputs by a command-position regex, joined each to its `tool_result` by id for byte
length, and recorded flags, actor (sidechain path), pipe filter and banner/header presence; an
opencode walker over `part` rows of type `tool`; and a renderer pass that ran `cairn recall
--no-sync --scope <s>` (and `--list`) for each routed scope, recording only byte and line counts.
Each reported zero has its paired non-zero beside it: hook records 0 vs tool results 424;
SERVED-FROM-CACHE 0 vs live 394.
