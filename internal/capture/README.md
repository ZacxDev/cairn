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
   `opencode export <id>` for each root `opencode session list --format json -n <SessionListCap>`
   names in each `-opencode-project` directory — and, under `-opencode-discover`, in every project
   `opencode debug scrap` names — and each child its `task` parts name, recursively; a unit (session
   info, message info, part) is new when the digest of its bytes changed. Each root is processed
   once per pass, however many directories list it (`session list` answers a PROJECT). A root whose
   listed `updated` is unchanged since a pass that exported and shipped it completely, an export
   that began at least `OpencodeSettle` (60 min) after that `updated` and less than
   `OpencodeMaxExportAge` (24 h) ago, is NOT exported (`Agent.exportSettled`): its ledgers are
   still folded, it is still routed, and its ledger still ships. The run line counts these as
   `export-skipped=`.
2. **Derives `V`'s read half** with `internal/transcript/scopeuse` over the new records, the
   persisted evidence of earlier runs, and the client read ledgers of the root and every child —
   a UNION with the stored `V`, so a deleted ledger cannot shrink it.
3. **Routes** (`Decide`, decision 16) over `V` *including* the records about to ship. A scope the
   routing table explicitly sends to an unconfigured alias is HELD at any instance count.
4. **Acts**: ship to the routed instance — the client read ledger FIRST (S11, decision 3a): the
   root's `<session>.jsonl` and every opencode child's, new complete lines only, as the root's
   `ledger` stream through the opencode path's saved-plan resume, a line that is not JSON (or a
   ledger that exists and cannot be read) shipped as a record naming `*` with its bytes dropped. The
   pod folds it into `V` itself (`internal/transcript/archive`'s `foldLedger`), and it goes before the
   transcript because the pod's F1 asks "is the ledger empty"; on a move, withdraw from the old one and re-ship the
   whole session from offset 0 (only up to what was derived); on a hold, withdraw any shipped
   prefix and stop for good.
5. **Redacts** every record and blob (`internal/redact`) and hands the exact bytes to the Sink; a
   watermark advances only after its write succeeds. One session's failure is refused and logged;
   the run continues; whether the pass then exits 3 is the exit-status section below.

## Measured facts it is built on (one host, opencode 1.18.29)

- `opencode export` **truncates at exit 0 when stdout is a pipe** (somewhere in 8–96 KiB; the
  boundary varies), so stdout is a regular temp file and a document that does not parse ships
  nothing.
- The export carries **no part `time_updated`**, so a part's version is a digest of its bytes.
- `session list` names the **current project's root sessions only**.
- `session list` answers **at most 100 rows unless `-n` is given**, exit 0, nothing on stderr: 100
  of a project's 202 roots with no `-n`, 50 at `-n 50`, all 202 at `-n 100000`. So every list is
  asked with `-n SessionListCap` (1000000), and a list that comes back with that many rows is
  REFUSED (`ErrSessionListAtCap`, the project's `refused opencode project` line) — at the cap,
  complete and cut short look the same.
- `debug scrap` prints every project opencode knows as `[{"id", "worktree", "sandboxes", …}]`;
  the `global` project's worktree is `/`, and `session list` run there lists the global project's
  sessions. `session list` runs IN a directory but filters by PROJECT, not directory: a base
  checkout and a worktree of it answered identical id sets, and a project whose worktree was gone
  answered all 10 of its sessions from either of its 2 existing `sandboxes`. So
  `-opencode-discover` lists a project whose worktree is gone from its first existing sandbox, and
  skips only a project with neither; it says both counts on stderr every pass
  (`opencode discovery: projects=N via-sandbox=S skipped-missing-worktree=M enumerated=K`).
- One `opencode export` costs **~0.65 s whatever its size**, so exporting every root and child on
  every 60-second pass costs minutes of CPU per pass on a host with hundreds of sessions — the
  reason for the skip above. A root's listed `updated` does NOT move for every change its export
  carries; two shapes were measured: **a child updated after its root** (2 of 459 children, by at
  most 39.8 s), and **a root's own final assistant message whose `time.completed` lands after the
  root's listed `updated`** (13 of 120 recent roots, by at most 135.4 s — a message completing does
  not bump `updated`). The skip therefore needs an export that began `OpencodeSettle` (60 min)
  after `updated`, not merely an unchanged `updated`.
  ⚠ A change later than that window with `updated` unmoved is beyond what was measured and not
  ruled out; the **24 h backstop** (`OpencodeMaxExportAge`) re-exports every root at least daily,
  so such a change is DELAYED by at most a day, never lost.

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

### `--run`'s exit status

🔴 **This is the ONE statement of cairn-capture's exit codes**; `main.go`, `Recurs`'s doc and the
CHANGELOG point here. It is cairn-capture's own table, not the client's (`cairn -exit-codes`, where 3
means something else).

| exit | means |
|---|---|
| 0 | the pass completed with no refusal that RECURS — whatever `refused=` says. A network error, a 5xx, a 400, an unanswered withdrawal, an unlistable opencode project, an incomplete export and a 503/507 are counted in `refused=` and retried next run; a 422 hold and a rewritten stream are marked once (below); a 410 is `deleted=`, a routing hold `held=` |
| 3 | the pass **completed** — every other session processed, the state saved — but refused at least one session for a reason that recurs on every run with nothing changed on either side (`capture.Recurs`): an **unresumable position** or **another uploader** (table below) — or **`-opencode-discover` was refused**: `opencode debug scrap` failed (a missing binary, a non-zero exit, or an answer that is not a list of projects with absolute worktrees and sandboxes — never read as "no projects"). That refuses only the DISCOVERED projects: Claude Code and every explicit `-opencode-project` directory are still captured, and a `refused opencode discovery` line says so at the start. One last stderr line per cause names the count and that cause's remedy |
| 1 | the state cannot be read or saved, a session tree cannot be listed, or an instance refused the capture token (401/403); wins over 3 |
| 2 | usage, and `--self-test`'s "could not vouch" |

`--dry-run` reads no state and talks to no instance, so no session refusal here can arise in it; a
refused `-opencode-discover` is the one 3 it shares — reported the same way, after everything else
it could read was printed.

| cause | the line to look for | exit | remedy |
|---|---|---|---|
| a stored position the state cannot resume from (`UnresumableError`) | `refused <session>: the instance holds this stream at "<pos>", which is not the start or the end of any record this run carries; …` | 3, every run | **On a host routed to ONE instance:** once the cause is understood (say, the instance's store was restored behind this host), with no run in progress, remove that session's entry (keyed by its id, under `sessions`) from the state file; the next run reads it again from the start. The entry also remembers which records the pod's re-check refused, so their bytes may be sent once more (the pod refuses them again). **On a host routed to SEVERAL instances: do not remove the whole entry** — it also records which instance holds the session's earlier upload (`instance`, the withdrawal obligation of decision 16) and any final hold (`held`), and removing it can strand or duplicate that upload. No narrower remedy has been measured; resolve it with the instance's operator |
| another (owner, host) holds the root (`ErrNotOwner`) | `refused <session>: the instance answered that this session is held by another uploader (409); …` — or, on a move, a `refused <session>: its routing answer now names instance …` line ending in that text | 3, every run | resolved **on the instance** (the other uploader's copy of that root — a token reissued under another host label, say). Clearing this host's state does **not** resolve it. **On a host routed to SEVERAL instances, do not remove the whole entry** either: when the refusal came from a move's destination probe, clearing empties `instance`, the next run ships straight to the destination and is refused again, and the copy on the instance the session left is never withdrawn — stranded |
| `-opencode-discover`'s `opencode debug scrap` failed or answered something unreadable | `refused opencode discovery: …` first, and `opencode discovery was refused …` last | 3, every run until it answers | make `-opencode-bin` run and `opencode debug scrap` print a JSON list of projects (an opencode upgrade that changed its shape needs a cairn-capture that reads it); nothing in the state is involved |
| a 422 naming a request field (the pod's redaction re-check matched a name or cursor) | `HOLD <session>/<stream>: … — held on this host for good and never resent` (no `refused` line) | 0 | **never clear the entry**: that forgets the hold and re-sends bytes the re-check refused. Marked once, so it would fail only one run anyway |
| a rewritten Claude Code stream | `refused <session>/<stream>: …` | 0 | marked once and skipped after; clearing would re-ship the rewritten file from its start |

⚠ A session reports only its FIRST failure, so its `refused` line may name a transient error while
another of its streams is wedged; the exit still counts the wedge (the classification sees every
failed write); once the transient cause is gone, a later run's line can name it.

A run takes `<state>.lock` (`flock`); an overlapping run exits 0 having done nothing. The state is
saved only when it CHANGED: at once where a crash must not lose a decision (an opencode write's
plan, a re-check refusal, and — since S6 — WHERE a session is bound: its instance, or its hold,
saved before the first upload leaves), after a changed session at most every two seconds, and when
the run ends with unsaved changes — an idle run writes nothing. A write that fails part-way commits the
prefix that was acknowledged. The token file may be a symlink (a secret manager's); the 0600 check is on its target.

## Plain `http` to a named host — `-allow-plaintext-host` (an operator opt-in)

A worker URL must be `https`, or `http` to a loopback address: the capture token rides in a header
and the bodies are transcripts. Some deployments reach `cairn-ui`'s host-side listeners over a
PRIVATE ENCRYPTED MESH, addressed as plain `http` — there the mesh IS the transport protection, and
there is no certificate for TLS to check — so the real worker URL was refused (exit 2).

`cairn-capture --run … -allow-plaintext-host [ALIAS=]HOST` (repeatable) accepts plain `http` for
that instance's `-worker` when its URL's host is EXACTLY `HOST`.

- **Exact, never a pattern**: no suffix, prefix, wildcard, case fold or port
  (`TestPlainHTTPToANamedHostIsAnExplicitExactOptIn`; mutant
  `capture-plaintext-host-matched-by-suffix`). Per instance: an opt-in for one alias does not admit
  another's URL.
- **A flag only** — no default and no environment spelling, so a copied environment cannot switch
  it on.
- **An opt-in that admits nothing is a WARNING line, not a refusal**: an alias with no `-worker`,
  or a worker that is `https` or loopback by now. Refusing it (built first) would stop capture on
  the day a worker moves to `https` with the flag still in the unit file; the plugins' client
  (`plugins/internal/endpoint`) ignores an unused entry too. A value that is not a bare host is a
  usage error (exit 2), and a host that is simply not the worker's leaves the URL refused as before.
- **Said on every run**, per instance, on stderr: `⚠ instance <alias>: the capture token and every
  upload go to <url> over PLAIN HTTP (-allow-plaintext-host); the network between is the only
  transport protection`.
- **Unchanged by it**: a redirect is never followed (`TestTheSinkNeverFollowsARedirect` — the sink
  knows nothing of the opt-in, so the token still goes nowhere but the named host), and the
  REDACTION-OFF test build still sends to a loopback address only.
- **No proxy from the environment, ever.** Go's default transport reads `HTTP_PROXY`, and for a
  plain-`http` URL the whole request — token and transcript — goes to that proxy in clear. The
  sink's client has no proxy, for `https` too (`TestNoEnvironmentProxyEverCarriesTheCaptureToken`,
  measured in a child process because Go reads those variables once; mutant
  `capture-sink-uses-the-environment-proxy`).

⚠ **What nothing here can check:** that the path to the named host really is protected. The flag is
the operator's assertion about a network; on an unprotected one the token and every transcript byte
cross it in clear.

## The withdrawal (S6, decision 16, clause m)

When a run's routing answer is no longer the instance holding a session's prefix — its scopes now
route to another instance, or to two (held) — the agent FIRST asks the holding instance to forget it:
`POST /capture/v1/sessions/{root}/withdraw` with that instance's capture token (`HTTPSink.Withdraw`;
`internal/worker`'s `WithdrawPattern`). Nothing of the session is shipped anywhere until that
instance has answered.

| the instance answers | the agent |
|---|---|
| `{"deleted": true}` — it held the root for this host's (owner, host) and deleted it (transcript, plugin outputs, ticket edges), or already had | resets the session's watermarks, binds it nowhere, then acts on the new answer: re-ships it WHOLE from offset 0 to the other instance, or holds it |
| `{"deleted": false}`, and this agent has an ACKNOWLEDGED upload of the session on record | holds the session FOR GOOD, loudly (`HOLD … PREFIX NOT WITHDRAWN: not held by this owner and host`): the token was reissued under another host label, say. It is never shipped anywhere else — the prefix's presence elsewhere is surfaced, not duplicated — and what the instance holds must be removed there by hand |
| `{"deleted": false}`, and no upload of it was ever acknowledged | treats the prefix as never having landed (the instance was not taking uploads) and acts on the new answer |
| no answer (a network error, a 5xx, a 404 from an instance older than the route) | leaves the session bound where it was, ships nothing of it, counts it `withdrawal-pending` on the run's summary line, and asks again next run |
| 401 / 403 | the same, and the run fails loudly (the token is refused) |

🔴 **WHERE A SESSION IS BOUND IS SAVED BEFORE ITS FIRST UPLOAD LEAVES.** It used to be saved with the
session's other changes, up to two seconds later: a run killed in that window had shipped a prefix
under a state that still said "nowhere", so a later routing change found no instance to withdraw
from (S3b's audit). ⚠ The cost is one state save per session the FIRST time it is bound — a first run
over a large backlog saves once per session (`TestAnIdleRunSavesNothing` pins the count; an idle run
still saves nothing).

🔴 **A WITHDRAWAL IS SENT TO AN INSTANCE THAT IS NOT TAKING UPLOADS.** A 503 (disarmed) or a 507
(quota full) stops an instance for UPLOADS for the rest of the run, and a session bound for it used
to be skipped before it was read — so a read that changed its routing answer was never derived and
its prefix never withdrawn, for as long as the instance stayed stopped. Now a session WITH a shipped
prefix is still derived and routed (its new bytes only), and the withdrawal — which stores nothing
and is not behind the arming gate — goes out; a session with nothing shipped is still skipped
unread. A refused token (401/403) still ends every request to that instance.

## A session deleted on its instance (410)

An upload answered **410 `{"refused":"deleted"}`** means the session's transcript was deleted on that
instance — by its owner, by this agent's withdrawal, or by retention — and that is FINAL there. The
agent marks the session (`deleted` in the state, saved at once), logs `DELETED <root>` ONCE, counts it
`deleted` on that run's summary line, and never reads or sends it again. Without that mark the
session was refused every run, and a stream or blob sent from its start was accepted, re-creating
on the instance the session its owner had deleted.

🔴 **The destination is asked BEFORE the current holder is told to forget** (#273's audit F1, closed
in S10). A routing-TABLE edit can send a session back to an instance it was once withdrawn from, where
its deletion is final. Withdrawing first removed the prefix from the instance that held it, and the
first upload to the other then answered 410 and marked the session `deleted` — captured nowhere, over
an edit to a table. So on a move the agent first sends the destination a position probe (the CAS's own
409, which stores nothing), and ONLY a definite "not deleted here" lets the withdrawal go ahead:

| the destination answers | the agent |
|---|---|
| a position (the 409) | withdraws from the holder and moves the session, as before |
| 410 | HOLDS the session where it is, for good: no withdrawal, nothing more of it shipped, `HOLD <root>: its routing answer now names instance <x>, where this session's transcript was DELETED…` logged once, counted `held` |
| nothing — unreachable, a 5xx, disarmed, a refused token | DEFERS: nothing is withdrawn and nothing of the session is sent this run; counted `withdrawal-pending`; asked again next run |

The third row is #274's audit: the first version withdrew on no answer, so a destination that was
merely down when the table was edited — and would have said 410 — still ended the capture everywhere.

⚠ **The trade, an AGENT decision open to operator review.** While the destination cannot be asked —
and for good once it answers 410 — the prefix stays on an instance the CURRENT table no longer routes
its scopes to, and nothing new of the session is captured. The alternative (withdraw at once, as
decision 16 reads) keeps the routing rule strict and loses the session's capture whenever the
destination turns out to have deleted it. Removing a held prefix is the operator's act: the owner's
delete, on that instance.

⚠ **What it cannot do.** The withdrawal table's third row reads "never landed" off this agent's own
watermarks, which a kill can leave unsaved — but then the instance DOES hold the prefix and answers
`true`, so the case cannot be confused in the direction that matters. And `deleted` is per session,
not per instance: a session marked deleted is not sent to ANY instance afterwards.
