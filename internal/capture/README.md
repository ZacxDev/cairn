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
409, which stores nothing). If it answers 410, the session is HELD where it is: no withdrawal, nothing
more of it shipped, `HOLD <root>: its routing answer now names instance <x>, where this session's
transcript was DELETED…` logged once, counted `held`. Any other answer — or none — changes nothing
and the withdrawal proceeds. ⚠ The held prefix then sits on an instance the CURRENT table no longer
routes its scopes to: that is the stated cost of not deleting it everywhere, and removing it is the
operator's act (the owner's delete, on that instance).

⚠ **What it cannot do.** The withdrawal table's third row reads "never landed" off this agent's own
watermarks, which a kill can leave unsaved — but then the instance DOES hold the prefix and answers
`true`, so the case cannot be confused in the direction that matters. And `deleted` is per session,
not per instance: a session marked deleted is not sent to ANY instance afterwards.
