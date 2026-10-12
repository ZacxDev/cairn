The host-side capture agent behind `cmd/cairn-capture` (S2 and S3b of
`claudedocs/plan-cairn-plugins.md`, decisions 5, 15 and 16). It reads, redacts and routes every
session (S2), and uploads it over the capture/v1 contract `internal/worker` serves (S3b,
`HTTPSink`). The binary's modes: `--run` (one pass, for a user timer), `--dry-run` and
`--self-test`. stdlib-only, under `internal/depspolicy`'s import ban.

🔴 **The upload does not arm capture, and capture is UNARMED on every instance (plan O16).** O15's
held-back measurement (closing-condition part 5, `internal/redact`'s `redact-heldback`) FAILS —
157/190 leaks caught, under the 90% floor — and operator decision O17 WAIVED it as an arming
precondition: it is a recorded measurement, not a gate. A green `--self-test` is closing-condition
part 3, over a corpus the redactor's authors wrote; it measures neither, and arming stays the
operator's act.

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

## The upload (S3b): `HTTPSink`, and what each answer does

A watermark advances **only on a 200**. Per instance: `-worker [ALIAS=]URL` (https, or http to
loopback only) and `-capture-token-file [ALIAS=]FILE` (mode 0600; never argv, never the
environment, never printed); `-host` is the label the token was minted for.

| answer | what the agent does |
|---|---|
| 200 / 202 | stored / a staged non-final continuation frame |
| 409 `{"stored_to":…}` | **resume**: the position is the end of a record this write carries (a lost acknowledgement) → continue after it, every record once; any other position → `UnresumableError`, nothing guessed. An opencode stream — whose units opencode mutates in place, so the stored position may be one the current export cannot reproduce — keeps the PLAN of its one unacknowledged write (`pending`, saved before the request leaves); while it is pending no new write is sent: the instance is first ASKED where the stream is (a probe that stores nothing) and the plan is settled against the answer |
| 409 uniform | `ErrNotOwner`: another (owner, host) holds the root; logged every run |
| 413 | the record or blob is replaced by a **placeholder** naming its size and the bound, under the SAME `src` and cursor; the stream moves on and is never wedged. A record over `MaxRecordBytes` (= `archive.MaxAssembledBytes`) is replaced BEFORE it is sent. Counted as `omitted=` |
| 422 naming a record or the blob | EXACTLY that record or blob is replaced by a `refused-by-recheck` **placeholder** and the stream moves on. Nothing the pod refused is stored, and its bytes are sent once: the refusal is saved in the state (`rechecked`) BEFORE the placeholder leaves, so a later run — after a lost acknowledgement, or a full pod — sends the placeholder at once. Residual: a process killed between the 422 and that save sends them one more time. Counted as `recheck-omitted=`. ⚠ An agent decision under O17: a hold was built first, and the pod's re-check was measured refusing clean data, which would freeze a stream for good |
| 422 naming a request field | a name or cursor matched: that stream (or blob version) is **held for good** (`refused` in state) and never resent; the session's other streams continue |
| 3xx | an error — a redirect is NEVER followed (it would carry the token and the body) |
| 503 / 507 | `HeldError`: nothing advances, nothing is lost, the instance gets no further request this run, sessions bound for it are skipped UNREAD (`skipped=`), the next run retries |
| 401 / 403 | `FatalError`: no further request to that instance this run (each would count toward the worker's lockout), exit 1 |

**Cursors** carry no content: a Claude Code stream's is its byte offset; an opencode stream's is a hash
chain over unit keys and the digests of their REDACTED bytes; a blob's is a digest of its redacted bytes.

**Deviations, stated:** S2 named blobs by their path under the session directory, and the pod
refuses a `/` in a blob name — every blob answered 400. `WireBlobName` maps `tool-results/<base>`
to `<base>` and any other path to its `/`-as-`.` spelling (or a digest when that still does not fit).
A record that is not JSON ships as `{"cairn_capture":"not-json","parts":[…]}` — its UTF-8 runs as
strings, other bytes as hex — because the pod stores only JSON values; base64 of the whole line was
built first and MEASURED refused by the pod's entropy rule (422) on a line with nothing secret in it.

A user timer (decision 5's cadence; the unit is the operator's, not shipped here):

```ini
# ~/.config/systemd/user/cairn-capture.service
[Service]
Type=oneshot
ExecStart=%h/.nix-profile/bin/cairn-capture --run -host host-a -worker https://worker.example.invalid -capture-token-file %h/.config/cairn/capture-token
# ~/.config/systemd/user/cairn-capture.timer
[Timer]
OnStartupSec=60s
OnUnitActiveSec=60s
AccuracySec=1s
```

A run takes `<state>.lock` (`flock`); an overlapping run exits 0 having done nothing. The state is
saved only when it CHANGED: at once where a crash must not lose a decision (an opencode write's
plan, a re-check refusal), after a changed session at most every two seconds, and when the run
ends with unsaved changes — an idle run writes nothing. A write that fails part-way commits the
prefix that was acknowledged. The token file may be a symlink (a secret manager's); the 0600 check is on its target.

## Not built here

The withdraw route (S6): until it exists `Withdraw` fails, so a session whose routing moved or
turned held keeps its shipped prefix where it is and NOTHING more of it is sent, logged every run.
The ledger stream's upload (S11).
