The host-side capture agent behind `cmd/cairn-capture` (S2 of
`claudedocs/plan-cairn-plugins.md`, decisions 5 and 16). **It uploads nothing**: its only sink
is a local spool, and S3 adds the upload. stdlib-only, under `internal/depspolicy`'s import ban.

## What one run does, per ROOT session

1. **Reads what is new.** Claude Code: every stream's complete lines after its byte offset (the
   main JSONL, each `subagents/agent-<id>.jsonl` as `subagent:<id>`), and every other file under
   the session directory as a blob. opencode: `opencode export <id>` for each root that
   `opencode session list --format json` names in each `-opencode-project` directory, and each
   child its `task` parts name, recursively; a unit (session info, message info, part) is new when
   the digest of its bytes changed.
2. **Derives `V`'s read half** with `internal/transcript/scopeuse` over the new records, the
   persisted evidence of earlier runs, and the client read ledger — grow-only.
3. **Routes** (`Decide`, decision 16) over `V` *including* the records about to ship, so a record
   that reads another instance's scope never reaches the first one.
4. **Acts**: ship to the routed instance; on a move, withdraw from the old one and re-ship the
   whole session from offset 0 (only up to what was derived); on a hold, withdraw any shipped
   prefix and stop for good.
5. **Redacts** every record and blob (`internal/redact`) and writes the spool; a watermark
   advances only after its write succeeds.

## Measured facts it is built on (one host, opencode 1.18.29)

- `opencode export` **truncates at exit 0 when stdout is a pipe** (8 of 8 sessions), so stdout is
  a regular temp file and a document that does not parse ships nothing.
- The export carries **no part `time_updated`**, so a part's version is a digest of its bytes.
- `session list` names the **current project's root sessions only**.

## Deviations from the plan, and why

- **One state file, not one per instance.** A session's watermarks travel with it when it moves
  (they are reset on a move anyway), and "which instance holds the prefix" is per session; one
  0600 file holds both. The plan's per-instance wording described the same information.
- **The opencode watermark is `(unit key, digest)`**, not `(part id, time_updated)`: measured above.
- **Session info and message info are shipped as records too**, beside the parts, so "every byte"
  of an export is shipped — a moved `time.updated` on the session is an upsert of `info`.

## Not built here

The upload, CAS, frames and the pod-side re-check (S3); the pod's `withdraw` route and its
cascade (S6); the ledger stream's upload (S11).
