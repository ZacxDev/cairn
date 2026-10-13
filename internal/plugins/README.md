# internal/plugins — the registry, toggles and plugin API's decisions (transcripts plan, S5 and S6)

Slices S5 and S6 of `claudedocs/plan-cairn-plugins.md` (decisions 9, 10, 11, 13, 14; operator decisions O4, O5, O7).
Plugins are OUT-OF-PROCESS programs (O4) speaking plain HTTP+JSON (O7, never MCP) to `cairn-ui`'s
WORKER listener with a `plugin` worker token. Nothing here runs a plugin, calls a model or holds a
plugin's own credential.

## What lives where

| file | what |
|---|---|
| `manifest.go` | the manifest, the CLOSED capability vocabulary (`pending:list`, `transcript:read`, `output:read:<type>`, `output:write:<type>`) and output-type set (`summary`, `ticket-edge`, `ticket-scan`, `ticket-suggestion`), `-plugin-registry` loading — any problem refuses the WHOLE file |
| `toggles.go` | the three levels, their values, `-plugin-journal` (the arc journal's write discipline), `Effective` — decision 10's fold — and `Settled`, the one condition under which a caller may stop computing a session's `V` |
| `outputs.go` | the output envelope, each type's in-tree body schema (`DecodeBody` — ONE check, for the write path and for the renderer), `Stamp` (the pod's id, clock, plugin NAME and VERSION — never the body's) |
| `pending.go` | decision 13's stateless pending query |
| `service.go` | `Service`: one object the worker listener's plugin routes (`internal/worker/plugin.go`) and the browser's toggle forms (`internal/ui/plugins.go`) both decide from |

## The fold, and who may set each level

ON iff every scope in the session's WHOLE visibility set `V` is a scope the authority still holds AND
is `allowed` or `on`; AND every scope is `on` OR an arc that COVERS the session is `on`; AND no arc
that covers it is `off`; AND its owner has not opted out. An empty `V`, `*` and an unknown name are OFF.

An arc **covers** a session when BOTH hold — and this sentence is exactly what `Effective` and
`State.ArcsCovering` enforce:

1. the session (or one of its child sessions) is in the **member snapshot** the arc's latest toggle
   record carries — the session ids the arc listed AT THE MOMENT an `admin` of the arc's home set that
   value (`Record.Members`, written by `internal/ui`'s `handlePluginArc` from the registration the
   authority had just answered with); and
2. the session's `V` holds the arc's HOME scope.

So a member a writer of the home ADDS after the admin's act is not covered, and a member a writer
REMOVES stays covered (an `off` veto cannot be lifted by re-registering the arc): the arc registry as
it reads now is not an input to any plugin decision, and `Service` is not handed the arc journal at
all. Covering a member added since takes an admin saving the toggle again; the arc page lists which
members a toggle covers and which it does not. A record with no snapshot covers nobody.

| level | values (default first) | set by |
|---|---|---|
| scope | `off`, `allowed`, `on` | `admin` on the scope (`control.Resolve`) |
| arc | `unset`, `on`, `off` | `admin` on the arc's HOME scope — not the right to `PUT` it; the record snapshots the arc's members |
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
- **`ticket-scan` is a fourth output type** (S10): `{system, edges, suggestions, model_asked}` — that a
  ticket matcher EXAMINED a session through the envelope's watermark. Decision 13 advances a session only
  when its plugin writes an output, and a matcher that finds nothing had nothing to write; the reasoning
  is in `plugins/README.md`.
- **The registry and the journal are both-or-neither, and only with the worker listener**; a `plugin`
  token row is admitted only for a plugin the registry holds, at startup and on every re-read.

## What it cannot see

- **Cost:** the pending query decides every stored root, and deciding one walks the WHOLE store for
  its write trailers (decision 3's trap forbids a narrower walk) unless `Settled` says the STORED half
  of `V` is OFF for every superset too. ⚠ "The stored half is OFF" alone was the skip until S6, under a
  comment calling the fold monotone, and it is not: an arc counts only when its home is in `V`, so a
  scope the walk ADDS can be the home of an ON arc — a session whose arc home only the walk finds was
  answered OFF until an unrelated page view recorded the scope. `Settled` is OFF-for-good only: an
  unconsented, unknown or `*` scope already in the set; the owner's opt-out; an `off` arc covering the
  session whose home is already in the set; or a scope merely `allowed` with NO `on` arc covering the
  session at all. `TestSettledIsOffForEverySuperset` checks it against `Effective` by exhaustion.
  Unmeasured at scale; the S4 walk measurement is the nearest number.
- **One worker per plugin** (decision 13): two receive the same items.
- **A stolen plugin token** reads every transcript its plugin is ON for (T6) — and can fill the
  instance's transcript quota with outputs only up to the plugin's SHARE: one stored line is at most
  `archive.MaxOutputLine` (128 KiB after escaping), one plugin's outputs at most 1/20 of the quota and
  all plugins' at most 1/5, each refused 507 past it — so capture keeps at least 80% (T12).
- **An arc speaks only for sessions whose `V` holds its home** (#270 round 1; the plan's decision 10
  named no such limit): an unrelated scope's admin cannot turn a plugin ON or OFF by listing a session
  in their own arc. And only for the members it listed when an admin set it (S6) — see "covers" above.
  ⚠ What that leaves: the snapshot is whatever the arc listed at that moment, so an admin who sets a
  toggle without reading the member list consents to whatever a writer had already put there. The arc
  page shows the list beside the form; nothing forces it to be read.
- **A rollback loses arc toggles.** A build older than `Record.Members` refuses a line carrying it
  (unknown key) and counts it `Skipped`: an arc `on` reads `unset` (fail closed), and an arc `off`
  veto is lost until the toggle is saved again.
- **The output caps count from a running total** (S6): stored output bytes are counted off the disk
  once, when the archive opens, then moved by each append and each deletion. ⚠ A file written into a
  session directory by anything but the archive is not in the totals until a restart.
- **The 507 says which limit** in the operator's log (S6): the plugin's own share, all plugins'
  share, or the INSTANCE quota. The 507 BODY does not — it carries the limit's byte count only.
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
