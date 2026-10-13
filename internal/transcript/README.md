# internal/transcript

The classification table for session transcripts (Claude Code JSONL, opencode export documents),
the synthetic world it is tested against, and — since S4 — the ONE visibility predicate and the
collapsed reading model the browser surface renders. `claudedocs/plan-cairn-plugins.md` is the plan.
`internal/ui` is the importer; the capture agent imports only `scopeuse`.

## What is here

| file | what |
|---|---|
| `classify.go` | decision 17's ONE table: declared record/block/part types, their key sets, and the class each UNIT takes (`ClassifyClaude`, `ClassifyOpencodePart`) |
| `testdata/synthetic_world.json` | written by `tests/transcripts/gen.py` — regenerate and diff, never hand-edit |
| `classify_test.go` | the shape ledger (both directions), its comparator's own control, and decision 17's rules over literal records |
| `archive/` | S3's transcript STORE on `cairn-ui` (decision 15): per-session directories, CAS append, frames, ownership, the refusing re-check, quota, retention. Written only through `internal/worker`, whose upload routes are DISARMED unless `cairn-ui -arm-transcript-capture` (O16). Since S4 it derives the READ half of a session's visibility set itself, through `scopeuse`, over every record and text blob it stores |
| `visible.go` | S4: `Visible` (decision 4), `VisibilitySet` and the whole-store `WrittenScopes` walk (decision 3), and `Store.View` — the only way a stored session's content leaves this package |
| `read.go` | S4: the collapsed reading (decision 18) — turns of classified items, at most 50 turns and 1,000 items a page (ending at a turn boundary), child streams nested under the call that spawned them |
| `transcripttest/` | a fixture archive for tests in this package and `internal/ui`; nothing that ships imports it |

`tests/test_transcript_fixtures.py` refuses a stale world and any date outside the year 2000.

## The claims, and their scope

- **The table pins the GENERATOR, and the generator pins the key sets measured on ONE host** at
  Claude Code 2.1.289 and opencode 1.18.29 (plan R1–R3). A real session can carry a type neither
  has seen; it classifies `unknown` — shown and counted, never dropped.
- **Units, not records.** A `tool_result` block inside a `user` record is a tool result; text
  beside it is runtime; only a `text` block (or string content) of a `user` record with no tool
  result, no `toolUseResult`, and neither `isMeta` nor `isCompactSummary` is `human-text`.
- **An agent's prompt is not a user message.** A sidechain `user` record (every record of a
  subagent file) and an opencode CHILD session's `user` text are `agent-prompt`; `human-text` is
  only what a human typed into a ROOT session. `synthetic: true` opencode text is `runtime`.
- **`binary` is a display label.** Under operator decision O12 binary content ships; the class
  says where it sits (an `image` item, `toolUseResult.file.base64`, an opencode attachment or
  `file` part) so a renderer can collapse it.

## Visibility and reading (S4)

- **One predicate.** `Visible(viewer, meta, V)`: the OWNER (an un-narrowed credential whose
  `(Kind, ID)` uploaded the session), or a viewer who reads EVERY scope in a non-empty `V`. `*`, a
  name the viewer's authority does not hold under any verb, and an empty `V` are owner-only.
- **`V` only grows.** `W_trailer` (every scope in the STORE where a trailer names the root or one of
  its child sessions — never the viewer's readable set, which would make "every" vacuous), plus what
  the pod derived and what the agent declared. A scope with an entry the walk cannot scan counts as
  written.
- **`Store.View` returns nil for absent, refused and not-a-session alike**, the Store's archive is
  unexported (`NewStore`), and a `View` reads each stream only up to where it stood when the View was
  decided — so a caller cannot render the three differently, read a record without asking, or be
  served a record uploaded after the verdict.
- **The reading classifies UNITS.** A `tool_result` block inside a `user` record is a tool result;
  the `toolUseResult` copy is counted, not shown. An opencode stream is read at each unit's LAST
  stored version.
- **Read-time bounds only:** `TurnsPerPage` (50), `ChildItemCap` (200), `CollapsedBudget` (2 MiB of
  collapsed content a page; user messages and assistant text are never charged).

## Shapes deliberately NOT declared

- opencode `subtask` parts: counted twice in one database (R3), absent from the 120 most recent
  exports, so their keys are unmeasured. They classify `unknown`. The child-session link that WAS
  measured is the `task` tool's `state.metadata.sessionId` (45 of 45 child sessions over six
  roots) — classified `child-link`.
- opencode `compaction` parts carry only the part envelope here: R3 counted one, and its other
  keys were not measured.
- Claude Code `attachment` subtypes: the real set is wider (R1); every attachment is bookkeeping
  whatever its subtype, so the declared list governs the fixture's coverage only.

## Measured while building S0 (one host, opencode 1.18.29)

- **`opencode export` TRUNCATES when its stdout is a pipe**: eight of eight sessions measured
  stopped somewhere between 8 and 96 KiB (8, 64, 96 here; 16 in a reviewer's run — the boundary varies) with exit 0 and an unterminated JSON document, while the same
  exports written to a regular file were complete and parsed (0.9–18 MB). A reader must send the
  export to a FILE and refuse a document that does not parse — never trust the exit code.
- **The export carries no `time_updated`** on any part (the database column exists, the export
  omits it), so a part's version cannot be `(id, time_updated)` from the export; S2 versions a part
  by `(id, digest of its bytes)`.
- **`opencode session list --format json` lists ROOT sessions of the CURRENT project only** (157
  listed against 614 root sessions in the database across 17 projects). Child sessions are found
  through the root's `task` parts, and a capture agent must be told which project directories to
  enumerate.
- **`OPENCODE_SESSION_ID` is not set by opencode itself**: the 1.18.29 bundle carries no such
  string (positive control: `OPENCODE_DISABLE_AUTOUPDATE` is found in the same bundle by the same
  search). On the measured host it is exported by the operator's own `shell.env` plugin, which sets
  it from the hook's `sessionID` argument — the session running the tool, so for a subagent that is
  the CHILD session's id by that code, not measured live (an `opencode run` probe was refused by
  this agent's sandbox). A host without that plugin has no such variable.
