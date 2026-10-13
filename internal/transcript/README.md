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
| `archive/` | S3's transcript STORE on `cairn-ui` (decision 15): per-session directories, CAS append, frames, ownership, the refusing re-check, quota, retention, plugin outputs (`outputs.jsonl`, capped by running totals) and — since S6 — the ONE deletion cascade (`deleteLocked`: retention, the owner's delete and `Withdraw` all end there, each journaled). Written only through `internal/worker`, whose upload routes are DISARMED unless `cairn-ui -arm-transcript-capture` (O16). Since S4 it derives the READ half of a session's visibility set itself, through `scopeuse`, over every record and text blob it stores |
| `visible.go` | S4: `Visible` (decision 4), `VisibilitySet` and the whole-store `WrittenScopes` walk (decision 3), and `Store.View` — the only way a stored session's content leaves this package |
| `plugin.go` | S5/S6: the PLUGIN's way out (`Store.ForPlugin`, `Store.ForPlugins` — `Visible` REPLACED by the caller's toggle `Gate`, never skipped; the store walk is skipped only where `Gate.Settled` says it cannot change the answer), `Store.ViewsWithOutputs` (the viewers' views of many sessions that hold a plugin output, one walk at most — what a session row's derived line is read through), `View.Outputs`, and `Store.Delete` (the OWNER arm's deletion, which cascades) |
| `mine.go` | S7: `Store.Mine` — the viewer's OWN stored sessions for "My sessions", newest upload first: the owner arm alone, metadata only |
| `read.go` | S4: the collapsed reading (decision 18) — turns of classified items, at most 50 turns and 1,000 items a page (ending at a turn boundary), child streams nested under the call that spawned them |
| `agent.go` | S8: the agent read API (decision 17) — `View.Skeleton`, `View.Page` (two views, filters, ranges, a byte bound), `View.Tool`, and `Store.Answer`, the ONE entry point both listeners serve it through. Its spellings are `internal/transcriptwire`'s |
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
- **"My sessions" lists ownership, never visibility (S7).** `Store.Mine` keeps a stored session iff
  the owner arm holds — the `(Kind, ID)`-and-un-narrowed check `Visible` uses — and reads no `V`, so a
  reader who can open somebody else's session never sees it listed as theirs, and a narrowed
  credential lists nothing. It returns metadata only; content still leaves through `View`.
- **Read-time bounds only:** `TurnsPerPage` (50), `ChildItemCap` (200), `CollapsedBudget` (2 MiB of
  collapsed content a page; user messages and assistant text are never charged).

## The agent read API (S8)

- **One entry point, two listeners.** `Store.Answer(viewer, route, query)` is what `internal/ui`
  (`/transcript/{skeleton,records,tool}`) and `internal/clientapi` (`/client/v1/transcript/…`, what
  `CAIRN_UI_URL` names and the `transcript` verb asks) both serve. It asks `Store.View` first; every
  miss after that — no transcript, a refused viewer, a stream, record or call not there — is ONE body,
  `transcriptwire.UnseenBody`, which is the client listener's own `not-found`. A malformed parameter
  is a 400 naming it, decided from the query alone before the predicate.
- **The views.** `conversation` (the DEFAULT) leaves each bookkeeping record out as a placeholder
  naming its class, type and size, and strips the duplicate `toolUseResult` FIELD, naming its size;
  every other member keeps its VALUE's stored bytes and its order (its key is re-encoded), and a key
  present twice is removed twice. `raw` is the stored bytes, byte-exact — the
  page is encoded by hand, because `encoding/json` compacts a raw message and escapes `<`, `>`, `&`
  in it. An unrecognised view is REFUSED, never defaulted. `ParseView` and `View.Page` are what the
  plugin route (S5) and the summary plugin (S9, `view=conversation`) are meant to read through.
- **The bounds.** A page is at most `max_bytes` (64 KiB default, 1 MiB ceiling) of encoded records
  and always carries its first record WHOLE, however large, with `next_seq` naming the next — never
  split. A tool answer carries one byte range (64 KiB default, 1 MiB at most) of its result and of
  its duplicate, moved inward to character boundaries and reported. Its call's INPUT is carried
  whole (bounded by its record, 4 MiB), never cut to the range.
- **The skeleton carries no content beyond tool names** — and what the spawning call said a
  subagent was for (`agent_type`, `description`), which the plan names. ⚠ It is NOT bounded: it reads
  every stream once and lists every call, so a session of tens of thousands of calls is a
  proportionally large answer (the client bounds what it accepts at 16 MiB).
- ⚠ **Work is not bounded, the response is** — a page scans its stream from record 1, as the
  collapsed reading does, but DECODES only from `from_seq` on unless a Claude `tool` filter needs
  every earlier call's name (measured: the last record of a 20,000-record stream, 274 ms and 920,110
  allocations decoding everything, 128 ms and 60,144 skipping; `page_decode_test.go`).

## Outputs, deletion and withdrawal (S6)

- **An output leaves this package only on a `View`** (`View.Outputs`), so it is shown to exactly the
  principals `Visible` admits to the transcript it was derived from (clause k). There are two
  constructors a viewer has — `Store.View` and `Store.ViewsWithOutputs` — and each ends in `Visible`
  over the same set: the stored half, a whole-store walk's finds (folded into the grow-only
  `meta.Written`) and that walk's unscanned scopes. `ViewsWithOutputs` exists because a LIST of
  sessions asked per row would walk the store once per row.
- **ONE deletion cascade** (`archive.deleteLocked`). Retention (`Sweep`), the owner's delete
  (`Store.Delete`, the owner arm, asked again under the root's lock) and a capture agent's withdrawal
  (`archive.Withdraw`) all end in it: the fact is journaled first (root, the uploader's owner and
  host, the reason — `retention`, `owner-delete`, `withdrawn` — and the clock; never content), the
  session's output bytes come out of the cap totals, any continuation frames staged for the root are
  dropped (they live outside the directory), and the directory is removed. Every plugin output and
  ticket edge is a line of `outputs.jsonl` INSIDE that directory, so there is no second place a
  cascade could forget.
- **`archive.Withdraw(who, root)`** (decision 16): `true` when `who` — a capture token's own (owner,
  host) — holds the root (it is deleted) or holds nothing under it and the journal records a deletion
  of that root as uploaded by the same (owner, host); `false`, deleting nothing, for another owner's
  root, the same owner's under another host label, a root never stored and an id that is no session
  id. The ownership test is the upload path's own `owned`. ⚠ The second `true` arm reads ANY
  deletion of the caller's own root, not only a withdrawal — a stated widening of the plan's
  "already withdrew it": a prefix retention or the owner's delete removed is as gone as a withdrawn
  one, and `false` there would hold the session on its host for good. A root somebody else holds NOW
  is `false` even for a caller who once withdrew that id.
- **The output caps count from a running total.** Stored output bytes are counted once, by
  `archive.Open`; an append adds its line and a deletion subtracts that session's lines, each under
  the session's lock. As first built (S5) every write that would cross a cap re-read every session's
  `outputs.jsonl` before refusing. A `QuotaError` says which limit it hit (`Share`).
- **A plugin's walk is skipped only where the caller says the answer is settled** (`Gate.Settled`).
  `Gate.On` saying no over the stored half is not that: `internal/plugins/README.md` has why.

## Shapes deliberately NOT declared

- opencode `subtask` parts: counted twice in one database (R3), absent from the 120 most recent
  exports, so their keys are unmeasured. They classify `unknown`. The child-session link that WAS
  measured is the `task` tool's `state.metadata.sessionId` (45 of 45 child sessions over six
  roots) — classified `child-link`.
- opencode `compaction` parts carry only the part envelope here: R3 counted one, and its other
  keys were not measured.
- Claude Code `attachment` subtypes: the real set is wider (R1); every attachment is bookkeeping
  whatever its subtype — EXCEPT a `queued_command` with `commandMode: "prompt"` (not `isMeta`):
  a message a human typed mid-turn, which is human text (an agent's prompt in a sidechain) in every
  view, the skeleton's turn index and the browser surface, never cut. A queued task notification
  stays bookkeeping. So the declared list governs the fixture's coverage only.

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
