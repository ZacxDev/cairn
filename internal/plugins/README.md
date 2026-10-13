# internal/plugins — the registry, toggles and plugin API's decisions (transcripts plan, S5)

Slice S5 of `claudedocs/plan-cairn-plugins.md` (decisions 9, 10, 13; operator decisions O4, O5, O7).
Plugins are OUT-OF-PROCESS programs (O4) speaking plain HTTP+JSON (O7, never MCP) to `cairn-ui`'s
WORKER listener with a `plugin` worker token. Nothing here runs a plugin, calls a model or holds a
plugin's own credential.

## What lives where

| file | what |
|---|---|
| `manifest.go` | the manifest, the CLOSED capability vocabulary (`pending:list`, `transcript:read`, `output:read:<type>`, `output:write:<type>`) and output-type set (`summary`, `ticket-edge`, `ticket-suggestion`), `-plugin-registry` loading — any problem refuses the WHOLE file |
| `toggles.go` | the three levels, their values, `-plugin-journal` (the arc journal's write discipline), and `Effective` — decision 10's fold |
| `outputs.go` | the output envelope, each type's in-tree body schema, `Stamp` (the pod's id, clock, plugin NAME and VERSION — never the body's) |
| `pending.go` | decision 13's stateless pending query |
| `service.go` | `Service`: one object the worker listener's plugin routes (`internal/worker/plugin.go`) and the browser's toggle forms (`internal/ui/plugins.go`) both decide from |

## The fold, and who may set each level

ON iff every scope in the session's WHOLE visibility set `V` is a scope the authority still holds AND
is `allowed` or `on`; AND every scope is `on` OR an arc listing the session is `on`; AND no arc
listing it is `off`; AND its owner has not opted out. An empty `V`, `*` and an unknown name are OFF.

| level | values (default first) | set by |
|---|---|---|
| scope | `off`, `allowed`, `on` | `admin` on the scope (`control.Resolve`) |
| arc | `unset`, `on`, `off` | `admin` on the arc's HOME scope — not the right to `PUT` it |
| session | `unset`, `off` — `on` is refused 400 and journals nothing | the transcript's OWNER arm (un-narrowed `(Kind, ID)`) |

`TestFoldCase01…09` and `TestASessionLevelOnIsRefusedAndJournalsNothing` are the ten cases, each red
under its own `plugins-*` row in `tests/control_mutants.py`.

## As built — the departures from the plan, each deliberate

- **Outputs are STORED here, minimally.** The plan gives storage to S6, but the write route and the
  pending watermark mean nothing without it, so `outputs.jsonl` lands in the session's own directory
  (`internal/transcript/archive/outputs.go`, decision 15's layout) — which makes every existing
  deletion (`rm -r` of the session) the cascade. S6 renders outputs and owns the cascade's journal fact.
- **A session not stored is the same 403 as one the plugin is OFF for** (T10). Clause (l)'s "the next
  transcript read is 404" after a deletion is S6's to reconcile with that.
- **`plugin_version` is stamped from the registry too**, not only the name.
- **`prev_output` is the plugin's LAST-WRITTEN output** for the session, any type.
- **The transcript route IS S8's read model**: `transcript.ParseRecordsQuery` and `View.Page`, so
  `view=conversation` by default (decision 13's "reads through `view=conversation`"), `view=raw` on
  request, and ONE pager for agents and plugins. A stream the session does not hold is the one 403.
- **The summary body is `{"text": …}`** (≤ 16 KiB) — the plan names the type, not its shape.
- **The registry and the journal are both-or-neither, and only with the worker listener**; a `plugin`
  token row is admitted only for a plugin the registry holds, at startup and on every re-read.

## What it cannot see

- **Cost:** the pending query decides every stored root, and deciding one walks the WHOLE store for
  its write trailers (decision 3's trap forbids a narrower walk) unless the stored half of `V` already
  refuses. Unmeasured at scale; the S4 walk measurement is the nearest number.
- **One worker per plugin** (decision 13): two receive the same items.
- **A stolen plugin token** reads every transcript its plugin is ON for (T6) — and can fill the
  instance's transcript QUOTA with outputs (each ≤ 64 KiB, no per-plugin cap), after which capture
  uploads answer 507 (T12). Not capped here.
- **An arc is anybody's** (audit round 1): members are arbitrary session ids, so a user who can see a
  session's id can list it in an arc homed in their own scope and turn the plugin ON wherever its scopes
  are only `allowed`, or OFF anywhere. `allowed` means "run when any arc asks". An arc still cannot run
  a plugin on a session any of whose scopes is `off`. Narrowing arc authority is open for review.
- **Consent is keyed by the folded scope NAME**, not a control id — the store's own name=directory rule.
  A scope deleted and recreated under the same name inherits its predecessor's toggle; `known` neutralises
  only a name the authority no longer holds at all.
- **Owner-only sessions are walked on every pending poll** (their stored `V` is empty, so no shortcut
  applies): O(owner-only roots × store) per poll, and a latency difference between "stored owner-only"
  and "absent" that the uniform 403 does not hide.
- **Journal damage is counted, not surfaced** (`State.Skipped`, `TornTail`): a damaged latest `off`
  falls back to the earlier value.
- **One undecidable session** (an unparsable `meta.json`) is left out of the pending list (OFF) and a
  line says so; every OTHER error — a store walk, a meta write — fails the whole query (500), so a
  broken store never reads as "nothing pending".
