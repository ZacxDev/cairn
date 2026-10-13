# internal/plugins — the registry, toggles and plugin API's decisions (transcripts plan, S5 and S6)

Slices S5 and S6 of `claudedocs/plan-cairn-plugins.md` (decisions 9, 10, 11, 13, 14; operator decisions O4, O5, O7).
Plugins are OUT-OF-PROCESS programs (O4) speaking plain HTTP+JSON (O7, never MCP) to `cairn-ui`'s
WORKER listener with a `plugin` worker token. Nothing here runs a plugin, calls a model or holds a
plugin's own credential.

## What lives where

| file | what |
|---|---|
| `manifest.go` | the manifest, the CLOSED capability vocabulary (`pending:list`, `transcript:read`, `output:read:<type>`, `output:write:<type>`) and output-type set (`summary`, `ticket-edge`, `ticket-scan`, `ticket-suggestion`), `-plugin-registry` loading — any problem refuses the WHOLE file |
| `toggles.go` | the three levels, their values, `-plugin-journal` (the arc journal's write discipline), and `Effective` — decision 10's fold, the ONE spelling of it |
| `outputs.go` | the output envelope, each type's in-tree body schema (`DecodeBody` — ONE check, for the write path and for the renderer), `Stamp` (the pod's id, clock, plugin NAME and VERSION — never the body's) |
| `pending.go` | decision 13's stateless pending query |
| `service.go` | `Service`: one object the worker listener's plugin routes (`internal/worker/plugin.go`) and the browser's toggle forms (`internal/ui/plugins.go`) both decide from |

## The fold, and who may set each level

ON iff every scope in the session's WHOLE visibility set `V` is a scope the authority still holds AND
is `allowed` or `on`; AND every scope is `on` OR an arc that speaks for the session is `on`; AND no arc
that speaks for it is `off`; AND its owner has not opted out. An empty `V`, `*` and an unknown name are OFF.

Which arcs **speak for** a session — exactly what `Effective` over `ArcsFor` enforces:

- only an arc whose HOME scope is in the session's `V` (an arc homed elsewhere says nothing, `on` or `off`);
- for **`on`**: the arcs that list the session (or one of its child sessions) NOW — the arc registry
  as it reads at the moment of the decision;
- for **`off`**: those, AND every arc whose veto SNAPSHOTTED the session — the member ids the arc
  listed when an `admin` of its home set it `off` (`Record.Members`, written by `internal/ui`'s
  `handlePluginArc`; `State.ArcsVetoing`). Removing a session from an `off` arc lifts nothing.

🔴 **The residual, stated (an agent decision, recorded in the plan under decision 10 / T14, open to
operator review).** `on` is read live so that per-arc summaries stay continuous as an arc grows
(operator decision O5). Arc membership is whatever an arc `PUT` names, and any WRITER of the home
scope may `PUT` — so **a writer of an `on` arc's home scope, who is not an admin, can enrol a
session in the plugin by adding it to the arc, provided the session touched that home scope and
every other scope in its `V` is `allowed` or `on`.** That is what `allowed` consents to: an arc
turning the plugin on for sessions that touched the scope. A scope left `off` is never overridden,
and "touched the home" is itself a claim a writer of the home can make (a trailer naming the
session). A snapshot for `on` too was built first and withdrawn for the reason above.

The arc toggle POST is bound to what the admin saw: the form carries a digest of the member list its
page rendered, and a POST whose digest is not the CURRENT list's saves nothing (409) — an `on` from a
tab opened before a writer added a session does not land.

| level | values (default first) | set by |
|---|---|---|
| scope | `off`, `allowed`, `on` | `admin` on the scope (`control.Resolve`) |
| arc | `unset`, `on`, `off` | `admin` on the arc's HOME scope — not the right to `PUT` it; an `off` record snapshots the arc's members; the POST must carry the digest of the member list its page showed |
| session | `unset`, `off` — `on` is refused 400 and journals nothing | the transcript's OWNER arm (un-narrowed `(Kind, ID)`) |

`TestFoldCase01…09` and `TestASessionLevelOnIsRefusedAndJournalsNothing` are the ten cases, each red
under its own `plugins-*` row in `tests/control_mutants.py`.

## As built — the departures from the plan, each deliberate

- **Outputs are STORED in the session's own directory** (`outputs.jsonl`,
  `internal/transcript/archive/outputs.go`, decision 15's layout) — which makes every deletion
  (`rm -r` of the session: retention, the owner's delete, a withdrawal) the cascade. S6 renders them
  (`internal/ui/outputs.go`) and journals each deletion.
- **A session not stored is the same 403 as one the plugin is OFF for** (T10) — AND SO IS A DELETED
  ONE (S6). The plan's clause (l) read "the plugin's next transcript read is 404"; as built it is the
  ONE 403 body, byte-identical to a never-stored id's. A distinct 404 for "was stored, now deleted"
  would tell any plugin token which session ids ever held a transcript, sessions it was never ON for
  included — the existence oracle T10 closes — and it buys a plugin nothing: the session is gone from
  its pending list and every refusal already means "stop". The plan's clause is amended.
- **`plugin_version` is stamped from the registry too**, not only the name.
- **`prev_output` is the plugin's LAST-WRITTEN output** for the session, any type.
- **The transcript route IS S8's read model**: `transcript.ParseRecordsQuery` and `View.Page`, so
  `view=conversation` by default (decision 13's "reads through `view=conversation`"), `view=raw` on
  request, and ONE pager for agents and plugins. A stream the session does not hold is the one 403.
- **The manifest has no `toggle_key` and no `output_types`** (#270 round 0): toggles are keyed by the
  plugin's name, and the types it writes are derived from its `output:write:<type>` capabilities — one
  declaration, so nothing can disagree with it. A registry carrying either key is refused.
- **The stored body is the re-encoded VALIDATED value** (`CanonicalBody`), never the bytes sent — a
  duplicate key validated as its last value was stored as both.
- **The summary body is `{"text": …}`** (≤ 16 KiB) — the plan names the type, not its shape.
- **A ticket TITLE is stored only where the instance says so** (S10; Q8, adopted by O13: ids only by
  default). `TicketEdge` and `TicketSuggestion` carry an optional `title` (≤ 256 bytes, no control
  character); `Service.WriteOutput` refuses a body carrying one — `ErrTitleNotStored`, a 400 naming the
  flag, decided BEFORE the session is looked at so it says nothing about any session — unless
  `cairn-ui` was started with `-plugin-store-ticket-titles` (no default; refused without a registry).
  The plan spelled the flag for the pod and left the body's field unstated.
- **`ticket-scan` is a fourth output type** (S10): `{system, edges, suggestions, model_asked}` — that a
  ticket matcher EXAMINED a session through the envelope's watermark. Decision 13 advances a session only
  when its plugin writes an output, and a matcher that finds nothing had nothing to write; the reasoning
  is in `plugins/README.md`.
- **The registry and the journal are both-or-neither, and only with the worker listener**; a `plugin`
  token row is admitted only for a plugin the registry holds, at startup and on every re-read.

## What it cannot see

- **Cost:** deciding a session walks the WHOLE store for its write trailers (decision 3's trap forbids
  a narrower walk): ONE walk per pending query however many roots, one per transcript read, and none
  while no scope consents to the plugin (`State.AnyConsent`). ⚠ There is NO shortcut on the stored
  half of `V`, and there was one until S6: the walk was skipped when the fold already refused the
  stored half, under a comment calling the fold monotone. It is not — an arc counts only when its home
  is in `V`, so a scope the walk ADDS can be the home of an `on` arc — and a session whose arc home
  only the walk finds was answered OFF until an unrelated page view recorded the scope. A correct skip
  needs a second spelling of the fold ("OFF for every superset"); it was written, and deleted in
  favour of asking the one fold over the whole set. Unmeasured at scale; the S4 walk measurement is
  the nearest number.
- **One worker per plugin** (decision 13): two receive the same items.
- **A stolen plugin token** reads every transcript its plugin is ON for (T6) — and can fill the
  instance's transcript quota with outputs only up to the plugin's SHARE: one stored line is at most
  `archive.MaxOutputLine` (128 KiB after escaping), one plugin's outputs at most 1/20 of the quota and
  all plugins' at most 1/5, each refused 507 past it — so capture keeps at least 80% (T12).
- **An arc speaks only for sessions whose `V` holds its home** (#270 round 1; the plan's decision 10
  named no such limit): an unrelated scope's admin cannot turn a plugin ON or OFF by listing a session
  in their own arc. The `on` residual is stated above.
- **A rollback loses arc vetoes.** A build older than `Record.Members` refuses an `off` line carrying
  it (unknown key) and counts it `Skipped`, so that arc reads as its PREVIOUS value — possibly `on`.
  An `on` or `unset` record carries no snapshot and reads the same on either build.
- **The output caps count from a running total** (S6): stored output bytes are counted off the disk
  once, when the archive opens, then moved by each append and each deletion. ⚠ A file written into a
  session directory by anything but the archive is not in the totals until a restart.
- **The 507 says which limit** in the operator's log (S6): the plugin's own share, all plugins'
  share, or the INSTANCE quota. The 507 BODY does not — it carries the limit's byte count only.
- **One unreadable `outputs.jsonl` does not stop the store opening**: its bytes are counted whole, at
  the file's on-disk size, against NO plugin — they weigh on all plugins' shared 20% and on the quota.
- **A deleted session takes no output**: a deletion is final for its root (`archive.gone`), so no
  upload can re-create the session a plugin would attach to.
- **Consent is keyed by the folded scope NAME**, not a control id — the store's own name=directory rule.
  A scope deleted and recreated under the same name inherits its predecessor's toggle; `known` neutralises
  only a name the authority no longer holds at all.
- **The pending query walks the store at most ONCE per poll** (`transcript.Store.ForPlugins`), and
  NOT AT ALL while no scope consents to the plugin (`State.AnyConsent`) — #270 round 1 measured one
  walk per session (0.36 s for 50 sessions, 1.04 s for 200, every toggle off). A single transcript read
  (`Service.Open`) still walks for its one session, so a latency difference between "stored
  owner-only" and "absent" remains, unhidden by the uniform 403.
- **Journal damage is counted, not surfaced** (`State.Skipped`, `TornTail`): a damaged latest `off`
  falls back to the earlier value.
- **One undecidable session** (an unparsable `meta.json`) is left out of the pending list (OFF) and a
  line says so; every OTHER error — a store walk, a meta write — fails the whole query (500), so a
  broken store never reads as "nothing pending".
