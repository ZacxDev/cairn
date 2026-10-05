# Plan: cairn learns who touched a scope — sessions and arcs

This is a DESIGN, not a measurement of anything built. Nothing below exists yet. Every claim
about today's behaviour was read off the code at `621b4e6` and carries a `file:line` so it can
be re-checked; every number about real data says which instrument produced it and what that
instrument cannot see. The arc handoff that tracks execution is
`claudedocs/handoff-cairn-arcs-sessions.md`.

## Goal

A caller holding a credential can ask, for any scope it may read: **which sessions wrote here,
and which arcs touched it** — over the API, the Go CLI and the browser surface — and every
answer states its own COVERAGE, so an empty list can never read as "nobody touched this".

- A **session** is an opaque agent-session id, the `<session>` half of the write trailer.
- An **arc** is one effort tracked by one handoff doc in one repo — the operator tooling's
  unit (the set of sessions that worked `claudedocs/handoff-<topic>.md`).
- **Touched**, in this phase, means **wrote an attributed bullet that still survives in the
  entry**. Reads are a later phase (see Deferred).

## What exists today, measured at `621b4e6`

### The write trailer — what it is, who writes it, and what it proves

- **Format.** `attributionFormat = " [cairn: %s/%s]"` (`internal/write/revision.go:44`), a
  SUFFIX on purpose: the bullet grammar is prefix-anchored, so a suffix leaves `OPEN:` and the
  date reader untouched (`revision.go:35-43`).
- **Grammar.** `attributionPattern` (`revision.go:54`):
  `\[cairn: [a-z0-9][a-z0-9-]{0,31}/[A-Za-z0-9][A-Za-z0-9_.-]{0,63}\]`. It is a fragment
  compiled only into `bulletTrailersRe`, which matches the whole run of machine trailers
  anchored at the END of a bullet (`revision.go:81-160`); `stripBulletTrailers` is the one
  reader of "what a trailer is" in Go, with one Python twin (`revision.go:132-160`).
- **Producer.** `RenderBullet(text, actor, session, today)` (`revision.go:240-243`), called
  ONLY from `write.AppendBullet` (`internal/write/write.go:150`), reached ONLY from the pod's
  `POST …/entry/<scope>/<ref>/bullets` handler (`internal/api/server.go:1472-1494`).
- **`<actor>` is the authenticated principal's `Display`**, not its id:
  `rq.identity = who.Principal.Display` (`server.go:773`), passed as the actor at
  `server.go:1492-1494`. For a token-file principal that is the row's identity column; for a
  control-plane user it would be the email (`internal/control/resolve.go:431-446`) — which
  `attributionPattern` cannot match. Today only the token file authenticates the pod
  (`cmd/cairn-server/main.go:214-222` has no database flag), so every live actor is a
  token-file identity.
- **`<session>` is SELF-DECLARED by the writer.** It is a required body field validated only
  by `SessionComponent = ^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$`
  (`internal/write/bullet_request.go:15-28, 249-253`); the CLI requires `--session` with no
  default and no env fallback (`internal/client/cli.go:112-116`). Nothing authenticates it.
- **What a trailer does NOT prove.** `ReplaceEntry` and `CreateEntry` write the caller's bytes
  verbatim, so a forged `[cairn: someone-else/…]` lands as sent — a declared limit, not an
  oversight (`write.go:255-264`, `revision.go:235-238`). And a re-POST that dedupes returns
  `duplicate` WITHOUT writing a new trailer (`write.go:144-149`), so a session that re-asserts
  an existing bullet leaves no edge.
- **The trailer is already load-bearing elsewhere**: it is inside `Lines[0]` and therefore
  inside the citation id (`internal/store/journal.go:246-247`).

### How much of a real store carries a trailer — MEASURED, with its instrument named

Instrument: a throwaway Python scan (not committed) over this host's two local caches, using
a bullet-opening-line regex `^[-*][ \t]` and a `## Nuance` heading match — an APPROXIMATION of
`store.ParseJournalBullets`, not the parser itself. It counted trailers by
`attributionPattern` anywhere in the line, so it OVER-counts prose that quotes a trailer
(the end-anchored rule below would not).

| cache | scopes | scopes with ≥1 trailer | bullet lines | trailered | distinct sessions | session shape |
|---|---|---|---|---|---|---|
| A | 27 | 22 | 5,204 | 1,435 (27.6%) | 695 | 1,402 uuid · 33 other |
| B | 14 | 11 | 2,135 | 433 (20.3%) | 233 | 428 uuid · 5 other |

What it established, at the scope it carries:
- **Three bullets in four have NO trailer.** The back-fill is free but partial; any answer that
  lists sessions without the attributed-of-total fraction reads as complete and is not.
- **8 of 41 scopes have no trailered bullet at all** — "no attributed writes" is a COMMON
  state, not a corner, and must have its own status distinct from `scope-empty`.
- **Every trailered bullet sat under `## Nuance`** (1,435/1,435 and 433/433), consistent with
  `AppendBullet` writing only there.
- **Session ids are NOT one shape.** ~3% are not uuids: a `ses_`-prefixed 30-character form
  (another agent runtime), digit-and-letter stamps, and short words. The operator tooling
  already learned this and treats ids as opaque strings, never shape-checked. This design does
  the same.
- **Two lines carried a trailer-LIKE token that the grammar rejects** — placeholder spellings
  quoted in prose. And one line carried two grammatical trailers, a quotation. Both are why
  the derivation reads only the END-ANCHORED run.

### The pod, its routes, its authz

- Route tables are the ledger: `readHeads = {recall, search, snapshot}` over `GET`/`HEAD`,
  `writeKeys = {POST entry, PUT entry}` (`internal/api/routes.go:62-75`); one arity per head
  (`routes.go:27-31`); `DeclaredRoutes()` derives from them (`routes.go:89-101`).
- Every read narrows through `rq.visible = who.Auth.VisibleScopes(control.VerbRead)` and every
  write through `rq.writable` (`server.go:774-775`), computed once from
  `control.Authorization.Allows` — THE predicate (`internal/control/resolve.go:60-71`).
  Refused is indistinguishable from absent on every route.
- A principal with the write verb nowhere is refused writes with a credential-level 403
  before any scope is named (`server.go:606-646`).
- The pod is stdlib-only by an import ban over `cmd/cairn`/`cmd/cairn-server`'s graph
  (`internal/depspolicy`); `internal/pgstore` holds ONLY the UI's sessions and invites and is
  deliberately NOT the control journal, because a driver cannot enter the pod's import closure
  (`internal/pgstore/pgstore.go:1-25`).

### The store root, and a hazard for any new on-disk state

- `snapshot.Build` skips every dot-prefixed name at the root (`internal/snapshot/snapshot.go:390-394`).
- But **`tokenfile.Source.storeDirs` (`internal/control/tokenfile/source.go:542-558`) and
  `store.LoadStore`'s root loop (`internal/store/load.go:185-248`) skip only NON-directories —
  neither filters a dot-prefixed DIRECTORY.** A `.something/` directory at the store root
  would therefore be enumerated as a SCOPE by the token-file adapter — and the legacy bare row
  reaches every enumerated scope. That is why the operator put registration storage OUTSIDE
  the store tree (Q2), and why the pod enforces it at startup.
- In the reference deployment the store root IS the volume root, the pod is the single writer
  (one replica, read-write-once claim), and the UI mounts the same claim READ-ONLY at the same
  root (measured in the private deployment manifests; no names here).

### The client

- `recall`/`search` render the LOCAL cache; the client's only read request is
  `/api/v1/snapshot` (`tests/testlib/capability_ledger.py:16-22`). **The pod never sees a
  scope being read** — it sees a sync. That fact decides the Deferred section.
- Exit codes are a printed contract: `0`, `2`, `3`, `4`, `5`, writes `6`–`9`, `11` unrouted,
  doctor `0/9/10` (`internal/client/exit.go:7-83`).
- `doctor` asks about THIS HOST and THIS CREDENTIAL, not a scope (`cli.go:80-84`), and the
  parity harness compares its stdout byte-for-byte across both clients
  (`tests/parity/harness.py:680-691`).

### The operator tooling's arc model (the private tooling repo; read, not modified)

`handoff_arc.resolve_arc` returns an `ArcReport`: `doc`, `repo` (a LABEL — the checkout
path's basename), `members[]` of `(session_id, role ∈ {originated, earliest-stamped, wrote,
resumed}, first_seen, commits[])`, `total_commits`, `unstamped_commits`, `readers_measured`,
`unmeasured_notes[]`. Writers come from `Claude-Session-Id:` commit trailers (scanned over the
whole body, column 0); readers from transcript kickoff lines, and the reader leg can be
UNMEASURED on a given run. The handoff writer parses `closing-condition:` kinds
`{check, judgement}`. That is everything the registration payload can carry — and the pod can
never see a transcript.

## Decisions taken (by the operator, not re-litigated here)

| # | decision | cost accepted |
|---|---|---|
| 1 | **Hybrid source of truth.** Session→scope write edges are DERIVED from the trailers already in the store (free back-fill). Arcs, arc→scope and arc→session are REGISTERED by the operator tooling's `/handoff`. | Derived edges inherit every limit of the trailer listed above; registrations are only as fresh as the last `/handoff` run. |
| 2 | **Touched = writes this phase; reads later.** | An arc whose sessions only READ a scope is invisible to it until the reads phase. |
| 3 | **Go-only, declared.** New routes in `cmd/cairn-server`/`internal/api`, new verbs in `cmd/cairn`/`internal/client`. The Python oracle is not extended. | The conformance corpus, the parity harness and the dual-run cannot compare these surfaces against a second implementation. Coverage comes from Go tests with literal expectations plus implementation-neutral relations. |
| 4 | **Deliverable is this plan plus a handoff, as a PR.** No task-board cards. | — |

**The seven questions this plan first left open were answered by the operator on review of
the PR.** They are decisions now, numbered as the questions were:

| # | decision | cost accepted |
|---|---|---|
| Q1 | **An arc is visible iff its HOME scope is readable.** Its other scopes are listed narrowed. | An arc homed in a scope you cannot read is not listed under one you can; the coverage line says so. |
| Q2 | **The journal lives at a SEPARATE path, `-arc-journal` / `CAIRN_ARC_JOURNAL`, OUTSIDE the store tree.** The pod refuses to start if the path resolves inside the store root. | One Deployment change in the reference deployment (below): today the store root IS the volume root, so the journal needs its own mount. |
| Q3 | **`arcs --scope X` lists arcs whose member session wrote to X without declaring it, labelled `inferred`**, beside `declared` ones. | `inferred` is a join over self-reported trailers and inherits their limits; the label is what keeps it from reading as a registration. |
| Q4 | **Status is `open` \| `closed` \| `unknown`.** `/handoff` sends `open`/`closed` when it can; cairn accepts `unknown` and renders it as `unknown`, never as `open`. | Arcs registered by tooling that cannot compute a verdict stay `unknown` indefinitely. |
| Q5 | **`arcs --check` reuses doctor's `0`/`9`/`10` exit codes** at its own call site. No new constants. | The `{0, 9}` overlap gains a third call site; it stays unambiguous because each call site knows its verb (`internal/client/exit.go:19-24`). |
| Q6 | **An authenticated append-time write log is a LATER phase**, listed in Deferred beside reads. Every result states that trailers are self-reported. | Derived edges remain forgeable through `put`/`create` and the session id remains self-declared for this whole phase. |
| Q7 | **Registrations, closed arcs included, are kept forever: append-only, no compaction this phase.** | The journal grows by one event per `/handoff` run; its size is unbounded until a later phase. |

**The one deployment change Q2 needs (not made here; the deployment repo is private).** The
pod's Deployment mounts its data volume at the store root, so no path on it is outside the
store tree. It needs a SECOND volume — or a re-rooted store on a subdirectory — mounted
read-write in the pod at the journal's directory, with `CAIRN_ARC_JOURNAL` set to a file in
it; and, for S4, the same volume mounted READ-ONLY in the UI's Deployment with the same
variable. Until that lands, the routes answer `registrations-unconfigured`, which is the
designed off state rather than a failure.

## Design decisions this plan takes (each with its evidence; 4, 5 and 6 are now fixed by Q1–Q3)

1. **The arc key is `(home scope, slug)`, where the home scope is cairn's OWN repo→scope
   derivation (`client.ScopeForRepo` → `DeriveScope`, `internal/client/reposcope.go:60-102`),
   never the tooling's `repo_label`.** `DeriveScope` is worktree-stable (it reads the git
   COMMON dir); `repo_label` is the basename of whatever path it was handed, so a run from a
   worktree would key the same arc under the worktree's directory name and split one arc in
   two. Computing it in the proposed `arc register` verb (S3; `--repo <path>`) makes "which scope is this repo"
   one rule in one place — the same one `cairn recall --repo` uses.
2. **Write edges are derived at READ time from the bytes the reader already loads — no index,
   no cache.** The cost is one parse of the scope, the same order as a `recall`; at the
   measured size (≈5k bullet lines in the larger cache) there is nothing to amortise, and an
   index is a second copy of the store that can go stale. Consistency is by construction:
   the edges ARE the current bytes. An incremental index is deferred until a benchmark says
   otherwise (slice S1 carries the benchmark).
3. **Derivation reads only the END-ANCHORED trailer run of a bullet's collapsed text, using
   the same pattern `stripBulletTrailers` uses** — exported from `internal/write` as a parser
   (`ParseAttributions`) so "what is a trailer" stays one definition. A trailer quoted
   mid-prose, a placeholder, or a malformed one is not an edge. The seam test pins that every
   trailer the strip removes is one the parser returns, and vice versa.
4. **Registrations live in an append-only JSONL journal OUTSIDE the store tree (Q2), written
   only by the pod, read by the pod and the UI, never compacted (Q7).** Not pgstore: the pod
   cannot link a driver (`pgstore.go:1-25`, `internal/depspolicy`), and the pod is the surface
   that must serve them. Not inside the snapshot: the snapshot is byte-compared against the
   oracle by `tests/dualrun/`, so a Go-only member would be a divergence. Mechanically it
   follows `control.FileStore` (lock, append, re-read, keep last-known-good on a bad read). The
   path is `-arc-journal` / `CAIRN_ARC_JOURNAL` with **no default** — unset means the arc
   routes answer `registrations-unconfigured`, and no image or Dockerfile changes. 🔴 **The pod
   REFUSES TO START when the path, after symlink resolution, is inside the store root** — the
   dot-directory hazard above means a journal (or its directory) under the root could become a
   scope, so "outside the tree" is enforced, not requested. The variable is a new name with no
   old spelling, so `internal/envalias` gains no pair.
5. **Arc→scope edges have TWO provenances, both reported, never merged silently**:
   `declared` (in the registration; the home scope is always declared) and `inferred`
   (computed at read time: a scope where a registered member session wrote an attributed
   bullet). The second is needed to answer the question asked — an arc whose session wrote to
   scope X touched X whether or not anyone declared it, and Q3 makes it part of `arcs --scope`.
   Registration remains the authority for membership; `inferred` is a join over SELF-REPORTED
   trailers, labelled as one on every surface (API body, CLI, UI).
6. **An arc is visible to a principal iff it may READ the arc's HOME scope**; its other scopes
   are listed narrowed through the same predicate. A rule of "visible when any of its scopes is
   readable" would print the home scope's NAME — a scope the reader may not see — which breaks
   refused-equals-absent for scope names. The cost: an arc homed in a scope you cannot read is
   not listed under a scope you can, and the coverage line says so. (Fixed by Q1.)
7. **Registration replaces state per key, but an UNMEASURED leg never erases a measured one.**
   The tooling pushes the whole current report on every `/handoff`; a push with
   `readers_measured: false` keeps the previously registered reader members rather than
   dropping them, because one host's unwalked transcript corpus is not evidence that another
   host's readers vanished.
8. **No new exit codes.** Every new verb maps onto the existing contract, so
   `ExitCodes()` and `test_the_two_clients_declare_the_SAME_exit_code_values` do not move.

## Data model

```
WriteEdge   (derived, never stored)
  scope, entry_ref, citation_id      -- citation id = the bullet's existing [cb:…] id
  actor                              -- as written in the trailer
  session                            -- opaque string, compared byte-exact, never normalised
  date                               -- the bullet's own `- YYYY-MM-DD:` opener, if present

Arc         (registered)  key = (home_scope, slug)
  status            open | closed | unknown   -- absent in the payload ⇒ unknown; rendered
                                              --   as `unknown`, never defaulted to open (Q4)
  closing_kind      check | judgement | none
  declared_scopes   []scope          -- ⊇ {home_scope}
  writers_measured, readers_measured bool
  commits_total, commits_unstamped   int     -- the tooling's own coverage, as counts
  reported_at       RFC 3339, set by the tooling
  registered_by     principal Display (from the credential, never the body)
  registered_at     pod clock

ArcMember   (registered)  (arc key, session)
  role              originated | earliest-stamped | wrote | resumed
  first_seen        RFC 3339 or empty
```

**Never stored**: the doc body, commit subjects, commit SHAs, the tooling's free-text
`unmeasured_notes` (only their counts survive, as the measured booleans and commit counts),
any transcript text, any filesystem path. The slug and home scope are the only names an arc
carries, and they are the names a reader of the home scope can already see.

Many-to-many falls out: one arc declares several scopes and is joined to more via its
sessions; one session id may be a member of several arcs.

## Flows

**Derivation (read time, every surface).** A new stdlib-only package `internal/touch` (named
for the verb this phase defines) takes a `store.Index` already narrowed by the caller's
visible set, walks each entry's bullets with `store.ParseJournalBullets`, reads
`write.ParseAttributions` over each bullet's text, and returns `[]WriteEdge` plus a `Coverage`
value. It never opens the store itself, so it cannot see a scope the caller could not
(`internal/ui`'s `TagMatches` follows the same rule for the same reason).

**Registration (push, tooling side).** After `/handoff --confirm` lands the doc, the tooling
calls the proposed `arc register` verb (S3) with `--repo <path> --slug <topic> --from <json-file>`, non-blocking (a
failed registration prints its stderr line and the handoff still succeeds). The client
derives the home scope, then `PUT /api/v1/arc/<home>/<slug>`. Payload (synthetic):

```json
{"schema": 1, "status": "open", "closing_kind": "check",
 "declared_scopes": ["alpha-notes", "beta-notes"],
 "writers_measured": true, "readers_measured": false,
 "commits_total": 7, "commits_unstamped": 2,
 "reported_at": "2000-01-02T03:04:05Z",
 "members": [{"session": "s-0001", "role": "originated", "first_seen": "2000-01-01T00:00:00Z"},
             {"session": "ses_example0000000000000000001", "role": "wrote", "first_seen": ""}]}
```

A member id outside `write.SessionComponent` can never equal a trailer's session, so it is
not stored; the response counts it (`unjoinable=N`) so the tooling can report it rather than
lose it silently.

## API routes (Go only)

New heads, each mirroring `recall/<scope>`'s shape — one arity per head, operands as path
components the existing `checkPathComponents` already vets:

| route | arity | answers |
|---|---|---|
| `GET`/`HEAD sessions/<scope>` | 2 | sessions with attributed writes in the scope, per session: actor, bullet count, first/last date; plus coverage |
| `GET`/`HEAD arcs/<scope>` | 2 | arcs touching the scope, each with provenance (`declared`/`inferred`); plus coverage |
| `GET`/`HEAD arc/<home>/<slug>` | 3 | one arc: metadata, members (each with the narrowed scopes it wrote), scopes by provenance; plus coverage |
| `PUT arc/<home>/<slug>` | 3 | register/replace; `X-Store-Status: arc-registered` or `arc-unchanged` |

Bodies are rendered text, like `recall`, with `?format=json` deferred until a consumer needs
it. Refusals reuse the existing vocabulary: an unreadable scope and an absent one answer the
same `404`; an arc whose home scope is unreadable answers exactly what an unregistered key
answers; a malformed payload is `400`; a `PUT` naming any declared scope the principal may
not WRITE is the same `404` a nonexistent scope gets, and nothing is written; a principal
with write nowhere gets the existing credential-level `403`.

## CLI verbs (Go only)

| verb | flags | source | exit codes |
|---|---|---|---|
| `sessions` | `--scope`/`--repo`, `--no-sync` | LOCAL cache, via `internal/touch` — the same function the pod runs, so pod and CLI bytes are equal by construction (the `recall` arrangement) | `0` (incl. no attributed writes), `3` unreachable with no cache |
| `arcs` | `--scope`/`--repo` | the POD (`arcs/<scope>`), body printed verbatim — registrations are not in the cache | `0` (incl. none registered), `3` unreachable |
| `arc show` | `--repo`, `--slug` | the pod (`arc/<home>/<slug>`) | `0` (incl. `arc-unregistered`), `3` |
| `arc register` | `--repo`, `--slug`, `--from` | `PUT arc/…` | `0`, `6` refused, `7` did not happen, `2` usage |
| `arcs --check` | (none) | the pod: orphan findings over every arc the principal can see | doctor's legend, `0` no finding / `9` a finding measured / `10` could not look, at its own call site — no new constant (Q5) |

`arc show`/`arc register` as one `arc` verb with a sub-action, or two verbs, is a naming
detail settled in S3 against `cli.go`'s parser; either way `-verbs` prints them and the
ledgers below move.

**Why the orphan check is NOT in `doctor`.** Two reasons, both measured: `doctor` is scoped
to the host and the credential and takes no scope (`cli.go:80-84`), and its stdout is
byte-compared against the oracle by three parity rows (`harness.py:680-687`) — a Go-only
eighth check would turn those red or force them down to exit-only, losing the comparison.
**And "session ids never in any arc" is a COVERAGE NUMBER, not a finding**: with ~700
distinct writing sessions in one cache and arcs registered only from now on, that count is
large forever, and a check that is permanently red is worse than none. Findings are: an arc
with no readable declared scope; a declared scope that no longer exists; a member session that
wrote nowhere the principal can see (`10`, unmeasured, not `9`: absence of edges cannot
separate "wrote elsewhere" from "never wrote").

## UI (`internal/ui`, read-only)

- **Scope page section** on `GET /scope?id=…`: "Sessions" and "Arcs" lists with the coverage
  lines verbatim, rendered from `internal/touch` over the already-narrowed `Visible` result and
  from the journal (read-only mount, re-read on change).
- **Arc page** `GET /arc?home=<scope id>&slug=<slug>` — a fixed path with query operands, for
  the reason `routes.go:110-128` gives for `/scope` and `/entry`; the slug is MATCHED against
  the registered set, never resolved. `classContent`; its authority is declared in
  `routes_test.go`'s `contentAuthority`.
- No state-changing row, so neither cross-site gate gains a case; both still apply by method.
  Session ids are TEXT, never links (a resume command is the tooling's rendering concern).
  Every href goes through `safeHref` (`render.go:2321`); `Raw`/`Rawf` remain banned.
- No registration from the browser (YAGNI; the UI's mount is read-only anyway).

## Authorization

One predicate, no parallel check: every read uses `rq.visible`; registration uses
`rq.writable`; the UI uses `Source.Visible`. In `internal/touch` the narrowing is structural
(it receives an already-narrowed index). Arc visibility is "home scope ∈ visible"; every
scope list an arc renders (declared, inferred, per-member) is intersected with the visible
set before rendering. The legacy bare row may read arcs and sessions and may not register,
because it may write nowhere — the existing rule, unchanged.

## Storage per deployment shape

| shape | write edges | registrations |
|---|---|---|
| token-file pod (deployed) | derived from the store at read time | journal file via `-arc-journal`, OUTSIDE the store tree (startup refuses otherwise); unset ⇒ `registrations-unconfigured`; needs its own mount (the Deployment change under "Decisions taken") |
| control-journal UI | derived from its read-only store mount | the same journal file, on its own READ-ONLY mount |
| pgstore UI | unchanged — pgstore holds sessions/invites only | NOT pgstore: the pod could not serve what only the UI can read |
| client cache | `sessions` derives locally | not cached; `arcs`/`arc` go to the pod |

## The coverage / honesty contract

Every answer carries these lines, always printed in full (a zero is printed, never omitted):

- `coverage: writes measured from entry trailers · reads NOT recorded (not collected in this phase)`
- `attributed: K of N bullets carry a write trailer (N−K have none — their writers are NOT listed)`
- `attribution: trailers are self-reported — actor as written in the entry (only appended bullets had it set by the pod); session ids are declared by the writer` (Q6)
- one status token, each a distinct mechanism, sharing no phrase with another:
  `scope-absent` · `scope-empty` · `no-attributed-writes` (entries exist, zero trailers) ·
  `registrations-unconfigured` · `no-arc-registered` (journal read, nothing for this scope) ·
  `arc-unregistered` · a populated list
- arcs answers add: `M of S writing sessions belong to a registered arc`,
  `arcs are listed only when their home scope is readable to you` (Q1), and a per-arc
  provenance label, `declared` or `inferred` — the latter glossed once per answer as
  `inferred: a member session wrote here; the arc did not declare this scope` (Q3)
- every arc's status is printed as registered: `open`, `closed` or `unknown` — an `unknown`
  is never rendered as, sorted with, or counted as `open` (Q4)
- arc answers add the tooling's own coverage: `commits_unstamped of commits_total commits
  carry no session id`, `readers: measured | NOT measured`, and the registration's age.

Pinned as WHOLE normalised strings (a guard on words is walkable by rewording).

## Declaration ledgers — every one that must move together

**API (one slice moves all of them):**
1. `internal/api/routes.go` `readHeads`/`writeKeys` → `DeclaredRoutes()`.
2. `tests/conformance/requests.json`: new rows with a new field **`go_only: true` +
   `go_only_why`** (mirror of `oracle_only`, `tests/conformance/cases.py:66-76,223-238`).
3. `tests/conformance/cases.py`: `validate_corpus`'s stale-route check (`cases.py:337-344`)
   excludes `go_only` rows, and a NEW inverse guard fails if a `go_only` route appears in the
   oracle's `declared_routes` (the oracle grew it; the mark is now a lie). `suite.py run`
   against the oracle skips them by id, reported; against Go they run.
4. Golden provenance: `go_only` goldens are recorded from `cmd/cairn-server` and stamped
   `recorded-from: cmd/cairn-server`. That makes them a CHANGE DETECTOR, not a contract
   witness — the house rule says never derive an expectation from the implementation. The
   contract witnesses are the Go tests with literal expected bodies, and the corpus's
   implementation-neutral relations (refused-equals-absent, head-matches-get, uniform-401),
   which need no golden and which every new read head joins.
5. `internal/api/api_test.go` `corpusRoutes` (`api_test.go:61-100`) counts `go_only` rows;
   `TestTheRouteLedgerMatchesTheConformanceCorpus` (`:113`) then holds unchanged;
   `TestEveryDeclaredRouteIsActuallyDISPATCHED`'s `targets` map (`:150`) gains each head.
6. `flake.nix` `checks.go-server-declares-its-routes` `want.txt` (`flake.nix:1489-1498`).
7. `tests/testlib/capability_ledger.py` `LEDGER`: rows gain a `go_only` surface asserted
   against `cairn -verbs` / `cairn-server -routes` instead of the oracle's tables.
8. `tests/conformance/README.md` "what this suite cannot see" and the P8 retirement ledger in
   `tests/parity/README.md` (the `go_only` machinery dissolves when the oracle does).

**CLI:**
1. `internal/client/cli.go` `Verbs()` and `requiredFlags`.
2. `flake.nix` `checks.go-client-declares-its-verbs` `want-verbs.txt` (`flake.nix:1409-1420`).
3. `tests/test_go_client_ledgers.py`: `test_the_go_client_declares_EXACTLY_the_pythons_verb_set`
   (`:188`) gains a declared `GO_ONLY_VERBS`, checked from three operands the way
   `GO_ONLY_LEDGER_FLAGS` is (`:82, :108-180`) — the tuple, the dispatcher read as source, and
   both binaries' behaviour — so shrinking the tuple cannot shrink what is measured.
4. `tests/parity/README.md`: a residual row for the Go-only verbs (no oracle twin, no parity
   row, by decision 3), and a P8 ledger row.
5. Exit codes: no constant added, so `ExitCodes()` and the two exit-code ledgers are
   UNCHANGED — a design constraint asserted, not a ledger moved.
6. **Named blind spot:** `tests/test_parity_harness.py::test_every_CLI_verb_appears_in_at_least_one_case`
   reads the PYTHON parser, so it cannot see a Go-only verb at all.
7. `tests/test_no_scrubbed_identifiers.py::test_the_malformed_remedy_names_a_verb_THIS_PACKAGE_REGISTERS`
   refuses every backticked "cairn" + verb citation in any tracked file whose verb the PYTHON
   parser does not register — it reads only that parser, so S0 widens it to also accept the
   declared `GO_ONLY_VERBS`. Until the slice that REGISTERS a verb has merged, docs cite it
   only as a proposal ("the proposed `arcs` verb"), never in that runnable shape — this plan
   and its handoff included, which is how this PR's first CI run went red.

**UI:** `internal/ui/routes.go` `routes`; `routes_test.go`'s hand ledger and
`contentAuthority`; `uiaudit/targets.go` (`linkExpanded`, since the scope page will link arc
pages); `internal/ui/README.md`.

**Imports:** `internal/touch` and the journal reader are stdlib-only and inside
`depspolicy.LinkedBinaryRoots`' closure; no module is added, so the allowlist does not move.

## Test strategy

- **What can be shown RED first.** This phase adds behaviour rather than fixing a bug, so
  most guards are new-behaviour tests; each new guard is mutation-tested (break the code, watch
  THAT guard fail with ITS message). Three are genuine regression-shaped guards and must be
  shown red on pre-change code:
  1. **The journal-inside-the-store hazard (Q2)** — first, MEASURE the premise: a dot-prefixed
     directory at the store root IS enumerated as a scope by the token-file adapter (expected
     at `621b4e6` from reading `source.go:542-558`; S3 must observe it, not cite it). Then
     the guard: `cairn-server -arc-journal <store-root>/.arcs/journal.jsonl` must REFUSE TO
     START, naming the reason — and so must a path that reaches the store root through a
     symlink, and the store root itself. RED before the startup check exists (the pod starts
     and the journal's directory becomes a scope), GREEN after; the control is a path outside
     the root, which must start. The refusal is a design choice, not a fix to the adapter, so
     the adapter's dot-directory behaviour stays as it is and stays recorded.
  2. **The ledger seams** — adding a head to `readHeads` with no corpus row must turn
     `TestTheRouteLedgerMatchesTheConformanceCorpus` red; adding a Go verb with no
     `GO_ONLY_VERBS` entry must turn the ledger test red. Watched red, then made green.
  3. **Strip ⇄ parse agreement** — `ParseAttributions` and `stripBulletTrailers` over one
     generated corpus (trailer at end, after `[cb:…]`, mid-prose, placeholder, two in a run,
     CRLF, wrapped bullet): a parser that accepts what the strip leaves, or misses what it
     strips, fails.
- **Positive controls.** Every "zero sessions" assertion is paired with a fixture that must
  produce a non-zero count; every refused-equals-absent pair includes a principal for whom the
  same request is a real answer (the corpus's existing 500-floor rule).
- **Byte identity, pod ⇄ CLI.** The proposed `sessions` verb (S2), run with `--scope X` over a synced cache, equals the
  pod's `sessions/X` body over the same store — one test, both renderings.
- **Authz matrix.** principal × scope × route, as a relationship, including: arc homed in an
  unreadable scope; inferred scope unreadable; registering against an unwritable declared
  scope; the bare row.
- **End-to-end** `tests/arcs/e2e.sh` (S5): boots `cairn-server` on a synthetic store, appends
  two trailered bullets with two sessions via `cairn append`, registers one arc via
  the proposed `arc register` verb, then asserts the proposed `sessions` and `arcs` verbs list both, with the
  coverage lines, and that a reader without the scope gets the absent answer. It is the
  closing check.
- **Which gate covers what:** `go test ./...` — derivation, journal, routes, authz, CLI,
  UI; `tests/conformance/run_go.sh` — the served contract of the new heads and their
  relations; `python3 tests/conformance/suite.py run` — must stay 0 failures with the new
  rows reported skipped; parity harness — UNCHANGED (blind by decision 3, named); dual-run —
  UNCHANGED (hand-listed requests; blind to the new heads, named); `uiaudit` — the two new
  GET surfaces in a real browser; `tests/leakscan.py` — every fixture synthetic.

## Sequence — each slice independently mergeable

| slice | what | size | mergeable alone because |
|---|---|---|---|
| S0 | **Declaration plumbing**: `go_only` corpus field + both validators, `GO_ONLY_VERBS` with its three-operand check, `go_only` capability rows — exercised on synthetic tables, no route added. | S | No surface moves; the controls prove the ledgers can go red. |
| S1 | **`internal/touch` + `write.ParseAttributions`**: derivation, `Coverage`, rendering, strip⇄parse seam test, a 10× synthetic-store benchmark. | M | Library only; nothing calls it yet. |
| S2 | **Sessions surface**: `GET/HEAD sessions/<scope>`, the proposed `sessions` verb, corpus rows, pod⇄CLI byte identity, authz pairs. | M | Needs no registrations; answers the "which sessions" half alone. |
| S3 | **Arc registry**: append-only, never-compacted journal at `-arc-journal` / `CAIRN_ARC_JOURNAL`, no default, and a startup REFUSAL when the path resolves inside the store root (shown RED first); `PUT/GET arc/…`, `GET arcs/<scope>` listing `declared` AND `inferred` arcs with the label; home-scope visibility (Q1); status `unknown` accepted and never rendered as `open` (Q4); the proposed `arc` and `arcs` verbs; merge rule 7. | L | Off by default (no journal ⇒ `registrations-unconfigured`). Live use also needs the Deployment change under "Decisions taken", which is the operator's, in the private deployment repo. |
| S4 | **UI**: scope-page section and `/arc` page, ledgers, uiaudit — rendering the `declared`/`inferred` label and the home-scope visibility rule verbatim from S3's renderer (no second visibility check), and `unknown` status as `unknown`. Reads the journal from its own READ-ONLY mount. | M | Read-only over S2/S3. |
| S5 | **`arcs --check`**, exiting on doctor's `0`/`9`/`10` legend with NO new constant — so `cairn -exit-codes` and both exit-code ledgers stay byte-unchanged, which the slice asserts — **+ `tests/arcs/e2e.sh`**. | S | The closing check; reads only. |
| T1 | **Tooling side** (private tooling repo, not cairn): `/handoff --confirm` calls the proposed `arc register` verb non-blocking; the pin bump that brings the verb. | S | A separate repo's PR; cairn is complete without it. |

Sizes are relative, not estimates — nobody has measured these.

## Risks, each with its control

| risk | control |
|---|---|
| A trailer is forged through `put`/`create`, or a session id is made up. | Not closable from bytes. Stated in the coverage line on every answer; Deferred names the append-time ledger that would close it. |
| A list without its denominator reads as complete (≈75% of bullets are unattributed, measured). | `attributed: K of N` printed always, pinned as a whole string. |
| Registration storage becomes a scope (dot-directory hazard). | The journal lives outside the store tree (Q2) and the pod refuses to start otherwise; S3's RED-first guard. |
| An `inferred` arc reads as a registration. | The `inferred` label on every surface, glossed in the answer; pinned as a whole string. |
| An `unknown` status reads as `open`. | Rendered literally; a test feeds a payload with no status and asserts the word `unknown` and the absence of `open`. |
| The journal grows without bound (Q7). | Accepted this phase; one event per `/handoff` run. Its size is reportable by `arcs --check`, and compaction is listed in Deferred. |
| A Go-only surface drifts with no second implementation to compare. | Literal-expectation Go tests, implementation-neutral relations, pod⇄CLI byte identity, the e2e check. |
| `go_only` goldens recorded from Go read as contract. | Stamped `recorded-from`, documented as change detectors; contract assertions live in Go tests. |
| An arc name leaks a client or a scope name. | Visibility via the home scope; all scope lists narrowed; synthetic fixtures; leakscan. |
| One host's push erases another host's reader members. | Merge rule 7, tested with a measured-then-unmeasured push sequence. |
| Derivation is slow on a big store. | S1 benchmark; index only if it fails. |
| The tooling sends ids cairn cannot join. | `unjoinable=N` in the response, reported by the tooling. |
| A permanently-red orphan check. | Unarced sessions are coverage, not findings. |

## Deferred

- **Reads (the next phase).** The pod structurally cannot observe a scope READ: `recall` and
  `search` render the local cache and the client's only read request is the snapshot. So
  "record reads at the pod" would record SYNCS, not reads of a scope. A reads phase therefore
  needs the CLIENT to report reads (a `X-Cairn-Session` header on the snapshot fetch, or a
  separate push), and must answer: retention (how long a read edge lives; reads vastly
  outnumber writes), privacy (a read log of who looked at what is a new surveillance surface
  the write trailer never was), opt-out, and whether a sync counts as a read of every scope it
  shipped. The seam left for it: `WriteEdge` becomes `Edge{Kind}` with `Kind` currently only
  `write`, and the coverage line's `reads NOT recorded` is a value, not a literal, so it can
  become `reads measured`.
- **An authenticated append-time write log — a LATER phase, beside reads (Q6).** Recording
  `(actor, session, scope, entry, citation id)` when `AppendBullet` writes would make edges
  authenticated rather than self-reported and give history across rewrites — and is the
  natural place the reads phase would write too. Until it exists, every result states that
  trailers are self-reported. Its open questions: whether the session id should then be bound
  to the credential, and how its edges reconcile with trailer-derived ones over the same bullet.
- **Compaction and deregistration** of arcs. Q7 keeps every registration, closed arcs
  included, forever and append-only this phase; a `DELETE` or compaction is a later decision.
- **`?format=json`** on the new routes, until a consumer needs it.

## Open questions for the operator

None open. All seven were answered on review and are recorded as Q1–Q7 under "Decisions
taken".
