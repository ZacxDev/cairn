The host-side capture agent behind `cmd/cairn-capture` (S2 of
`claudedocs/plan-cairn-plugins.md`, decisions 5 and 16). **It sends and stores nothing**: the agent
hands redacted bytes to a `Sink`, S2 ships no Sink of its own (a local spool stood in and was
removed on review, D1), and the binary offers `--dry-run` and `--self-test` only. S3 adds the
upload. stdlib-only, under `internal/depspolicy`'s import ban.

🔴 **The upload does not arm capture, and capture is UNARMED on every instance (plan O16).** The
arming gate is O15's held-back measurement (closing-condition part 5, `internal/redact`'s
`redact-heldback`), and it currently FAILS: 157/190 leaks caught, under the 90% floor. A green
`--self-test` is closing-condition part 3, over a corpus the redactor's authors wrote; it is not
that gate and does not license arming.

## What one run does, per ROOT session

1. **Reads what is new.** Claude Code: every stream's complete lines after its byte offset (the
   main JSONL, each `subagents/agent-<id>.jsonl` as `subagent:<id>`), and every other file under
   the session directory as a blob — stat first, read only when size or mtime moved. opencode:
   `opencode export <id>` for each root `opencode session list --format json` names in each
   `-opencode-project` directory, and each child its `task` parts name, recursively; a unit (session
   info, message info, part) is new when the digest of its bytes changed.
2. **Derives `V`'s read half** with `internal/transcript/scopeuse` over the new records, the
   persisted evidence of earlier runs, and the client read ledgers of the root and every child —
   a UNION with the stored `V`, so a deleted ledger cannot shrink it.
3. **Routes** (`Decide`, decision 16) over `V` *including* the records about to ship. A scope the
   routing table explicitly sends to an unconfigured alias is HELD at any instance count.
4. **Acts**: ship to the routed instance; on a move, withdraw from the old one and re-ship the
   whole session from offset 0 (only up to what was derived); on a hold, withdraw any shipped
   prefix and stop for good.
5. **Redacts** every record and blob (`internal/redact`) and hands the exact bytes to the Sink; a
   watermark advances only after its write succeeds. One session's failure is refused and logged;
   the run continues.

## Measured facts it is built on (one host, opencode 1.18.29)

- `opencode export` **truncates at exit 0 when stdout is a pipe** (somewhere in 8–96 KiB; the
  boundary varies), so stdout is a regular temp file and a document that does not parse ships
  nothing.
- The export carries **no part `time_updated`**, so a part's version is a digest of its bytes.
- `session list` names the **current project's root sessions only**.

## Deviations from the plan, and why

- **One state file, not one per instance.** A session's watermarks travel with it when it moves
  (they are reset on a move anyway), and "which instance holds the prefix" is per session.
- **The opencode watermark is `(unit key, digest)`**, not `(part id, time_updated)`: measured above.
- **Session info and message info are shipped as records too**, beside the parts.
- **A Claude Code stream resumes only when its 4 KiB head fingerprint matches AND the byte before
  the watermark is still a newline** — the second check sees a rewrite past the head.

## Not built here

The upload, CAS, frames and the pod-side re-check (S3); the pod's `withdraw` route and its
cascade (S6); the ledger stream's upload (S11).
