# Plan: an arcs-first browser, and where a live session is running

This is a DESIGN, not a measurement of anything built. Nothing below exists yet. Every claim
about today's behaviour was read off the code at `origin/main` `64475d7` (cairn) or the operator
tooling repo (read-only; paths below are prefixed `tooling:`), and carries a `file:line` so it can be re-checked. Claims about
the browser surface's scope tabs and session page were read off **PR #193's branch
(`zach/ui-scope-tabs-sessions` at `b1d82ad`), which is OPEN and NOT merged** — they are marked
`(#193)` and must be re-read once it lands. Every number about real data names the instrument that
produced it and what that instrument cannot see. Fixtures and examples are synthetic: hosts
`host-a`/`host-b`, tmux target `notes:3`, session ids `s-0001`, year-2000 dates.

## Goal

Two operator ideas, carried as one plan because the second decorates the first:

1. **An arcs-first page.** The browser opens on the arcs that are LIVE — newest activity first,
   each with when it was last updated — and clicking one resolves it: its scopes, its sessions
   and the entries its sessions touched, each linking to the existing `/scope`, `/session` and
   `/entry` pages.
2. **Where is that session running?** For a session the VIEWER owns that is live in a tmux pane
   on one of the viewer's machines, show the host label, the tmux target (`<session>:<window>`)
   and the hotkey — and offer a button that rings the terminal bell in that pane, so the window
   is easy to find. Bell only: nothing in this design can type into a pane.

### Premise, and what would make this work unnecessary

The premise is that the operator resolves sessions **from the cairn browser surface** — a scope
or arc page names a session, and the next question is "which window is that". Two things would
make the presence half unnecessary, and the plan should be dropped rather than built if either
holds:

- **The operator already answers "which window" elsewhere.** The tooling repo's `scripts/session-manager`
  resolves a session id to host + tmux slot + hotkey today (measured below), and the tooling repo already
  runs a host→server snapshot push on a 2-minute user timer that POSTs that tool's JSON to another
  of the operator's services (`tooling:scripts/tmux-snapshot-push.sh`, unit at
  `tooling:nix/home.nix:4314-4355`, timer `:4515-4528`). If the session ids on a cairn page can
  simply be pasted into that tool, the cairn side adds a second copy of an answer that exists.
- **The session ids on cairn pages are not the ones in panes.** They are the same string space
  (the write trailer's `<session>` is the agent runtime's session id, and the agent ledger records
  the same id per pane — below), but a trailer's session is SELF-DECLARED by the writer
  (`internal/write/bullet_request.go:15-28`). If the operator's writers stopped declaring the
  runtime's real id, presence would join nothing.

The arcs-first half has no such escape: nothing today lists arcs across scopes.

### closing-condition

- **closing-condition:** `check` — slices S1, S2, S4 and S5 are MERGED on cairn `main` (verified by
  content, not ancestry) AND the tooling repo's host-agent slice S3 is merged in the tooling repo AND
  `tests/presence/e2e.sh` exits 0 on cairn `main`. That script boots `cairn-ui` with presence
  enabled on a synthetic store and journal, and asserts, as relationships:
  (a) `GET /arcs` lists a recent and an open arc newest first and omits a closed one older than 14
  days until `?all=1`;
  (b) an agent token bound to owner A on `host-a` pushes presence for `s-0001`, and A's session
  page shows `host-a · notes:3` while owner B's page for the same session is BYTE-IDENTICAL to the
  page with no presence at all (the negative control);
  (c) A's `POST /ring` enqueues and B's `POST /ring` for the same session gets the same answer as a
  session with no presence;
  (d) the `host-a` token claims the ring and a `host-b` token for the same owner claims nothing;
  (e) the reference ring executor, pointed at a PRIVATE tmux server (`tmux -L presence-e2e`, never
  the default socket), sets `window_bell_flag=1` on the target window and on no other, and the
  pane's captured contents and its program's stdin are unchanged (BEL is not printable).
  A `--self-test` sabotages (b) and (e) and must report them caught, exiting 2 if it cannot.

Post-close rollout (NOT part of the closing condition, listed so it is not mistaken for it): the
personal instance runs with presence enabled, each operator host runs the S3 timer, and a click on
the deployed session page's bell sets the bell flag on the right window — an operator judgement
over the window's status-line styling.

## What exists today, measured

### Arc registrations — what the journal records and how often

- **One record per registration, JSON per line, append-only, never compacted**
  (`internal/arcs/journal.go:17-24`). The fold keeps the LAST valid record per `(home, slug)`
  (`journal.go:106-137`); the browser re-reads the whole file on every request
  (`internal/ui/arcs.go:60-76` (#193)).
- **The record** is `arcs.Registration` (`internal/arcs/arcs.go:92-109`): home, slug, status
  (`open | closed | unknown`, `arcs.go:39-46`), closing kind, declared scopes, the two
  measured-leg booleans, commit counts, `reported_at`, members (`session`, `role`, `first_seen`,
  `carried`), `registered_by`, `registered_at`.
- **`registered_at` is on EVERY record that is ever applied; `reported_at` is NOT.** The pod stamps
  `registered_at` from its own clock (`journal.go:205`) and the read-side check refuses a record
  without a valid one (`arcs.go:358`). `reported_at` is the tooling's word, optional and accepted
  empty (`arcs.go:227-232`).
- **How often a record is appended: on every `/handoff` whose commit landed, in practice.**
  `Register` appends nothing when the merged state equals the previous one except for
  `registered_at` (`arcs.go:333-338`, `journal.go:206-208` → `arc-unchanged`). But the tooling repo's
  registrar sets `reported_at` to NOW on every call (`tooling:scripts/lib/handoff_register.py:151`),
  so every push differs and is appended. It is called only from the two success arms of a handoff
  write, after the commit is on the remote, non-blocking (`tooling:scripts/lib/handoff_doc.py`,
  `_register_arc`, ~`:8827-8857`). So a registration's age is "time since the last handoff", which
  for an arc mid-flight can be days.
- **Consequence for "last updated":** `registered_at` is the one registration instant that is
  present, pod-clocked and comparable across arcs. A tool that re-pushes identical content does not
  move it — correctly, since nothing changed.

### Bullet dates — the only time the store carries

- A write edge's `Date` is the bullet's own `- YYYY-MM-DD:` opener or `""`; it is "the ONLY time
  the bytes carry … There is no time of day and no write timestamp anywhere else"
  (`internal/touch/touch.go:46-49`). ~44% of the oracle's live corpus is undated
  (`internal/store/journal.go:90-93`).
- Per session per scope, `touch.Session.FirstDate/LastDate` are the min/max non-empty dates
  (`touch.go:73-76`), and every `BulletRef` keeps `EntryRef`, `CitationID` and `Date`
  (`touch.go:55-59`) — enough to list entries touched without a new parser.
- A bullet date is the pod's clock on an append and the WRITER'S WORD on a `put`/`create`
  (`touch.go:46-48`), so a future date is writable.

### The browser surface after #193 (OPEN — read off its branch)

- Routes are an exact-match map with operands in query parameters (`internal/ui/routes.go:117-212`
  (#193)): `GET /arc` (`:126`) and `GET /session` (`:132`) are `classContent`; there is no
  arcs-index row.
- The scope page has tabs `?tab=` ∈ {`""` entries, `sessions`, `arcs`} (`routes.go:355-372`,
  `render.go:254-297` (#193)); an unknown value renders the default tab, never a 400.
- `/arc` renders ONE arc: summary stats, declared scopes (narrowed), members with the readable
  scopes each wrote in (`internal/ui/arcs.go:141-181, 461-536` (#193)). Every miss is one 404 with
  `report.ArcUnregisteredBody` (`arcs.go:120-128` (#193)). It has no tabs and no entries list:
  `report.Arc` keeps only the scope NAMES per member (`internal/report/arcs.go:289-330` (#193)).
- `/session` aggregates one session across every readable scope; an id outside
  `write.SessionComponent` is refused before any read, and every miss is one byte-identical 404
  (`internal/ui/sessionpage.go:72-73, 83-116` (#193)); the walk is `report.SessionAcross`
  (`internal/report/sessionacross.go:73-122` (#193)).
- **Cost, measured** (`BenchmarkSessionPageAndScopeTabs`, `internal/ui/sessionpage_bench_test.go`
  (#193), run here with `-benchtime 10x` on the local toolchain, go 1.26 — NOT the pinned 1.25;
  absolute times are host-bound, the ratio is the claim): at 30 scopes × 100 entries the
  whole-store session page took ~249 ms/op against ~186 ms for the scope page's arcs tab and
  ~174 ms for its entries tab; at 10 × 30, ~25 ms against ~16 ms. An arcs-first page needs the
  same ONE whole-store walk the session page does — not one walk per arc.

### Route ledgers a new browser row moves (read off how `/arc` was added, #186 `4714652`)

That commit touched: `internal/ui/routes.go` (the `routes` row + path/query constants),
`internal/ui/routes_test.go` (the hand ledger `TestTheRouteLedgerMatchesTheDispatchTable`,
`bareGETAnswer`, `contentAuthority` — `:56-90, :401, :685` (#193)), `uiaudit/targets.go`
(`linkExpanded`, `:180` (#193)) and `uiaudit/boot.go` (fixture data), `internal/ui/README.md`,
and `tests/control_mutants.py` (mutation rows). It did NOT touch `flake.nix`: the `onlyGo` filter
(`flake.nix:351-455`) only has to move for a new EMBEDDED asset (as `filter.js` did, `:431-433`).
A browser row does not touch `api.DeclaredRoutes()` or `tests/conformance/requests.json` —
"THIS IS A DIFFERENT LEDGER FROM THE POD'S, AND NEITHER MOVES THE OTHER" (`routes.go:17-20`).

### Identity: who is the "owner" of a presence row

- A request resolves to `identity.Identity{Principal, Auth, Fingerprint}`
  (`internal/identity/identity.go:82-115`); `control.Principal{Kind, ID, Display, CredentialID}`
  (`internal/control/resolve.go:17-26`). `Display` is an email, `provider:subject` or a project
  name (`resolve.go:431-446`) and the email is MUTABLE (`internal/control/model.go:193-196`).
  **The stable owner key is `(Principal.Kind, Principal.ID)`.** `CredentialID` is set only on the
  bearer path, so whole-struct comparison would fail across paths.
- **Browser and bearer agree on that key ONLY on `cairn-ui` with a control journal.** The UI's
  chain is machine token → JWT → cookie (`internal/ui/auth.go:60-70`, `identity.go:269-287`) over
  ONE authority; a GitHub sign-in resolves `PrincipalFor(KindUser, user.ID)`
  (`internal/identity/supabase.go:240-265`), and a credential issued with
  `-principal-kind user` resolves to the same `(user, usr_…)`.
- **The pod's bearer path never sees a journal user.** `cmd/cairn-server` authenticates machine
  tokens against the token-file projection (`cmd/cairn-server/main.go:429`), where every row is a
  `project` principal (`internal/control/tokenfile/source.go:365, 376-387`). So a presence store in
  the pod could not bind to the browser's user — decision 1.
- **There is no capability narrower than a scope verb.** Verbs are a closed `{read, write, admin}`
  (`model.go:50-75`); `Credential.NarrowedScopes` only intersects scopes (`model.go:277-288`).
- 🔴 **FINDING (pre-existing, read not exercised): a narrowed credential gains its principal's FULL
  authority by signing in.** `handleSignIn` discards the narrowed authorization
  (`principal, _, err := s.credentials.Authenticate(presented)`, `internal/ui/session.go:221`);
  `openSession` stores only `Kind` and `Principal` (`session.go:279-283`); the cookie backend
  re-resolves with no narrowing (`Auth: control.Resolve(model, principal)`,
  `internal/identity/cookiesession.go:102`). So "mint a narrowed token for the host agent" would not
  bound its blast radius — decision 3 does not use the control plane's credentials at all, and S0
  reports this separately.

### Cross-site gates and rate limits on the browser surface

- `stateChanging` is every method but GET/HEAD/OPTIONS (`internal/ui/server.go:1273-1280`). For
  such a request the dispatcher requires same-origin BEFORE auth — `sameOrigin` refuses a missing
  `Origin` (`session.go:71-81`) — and a CSRF token (form `csrf` or `X-Cairn-CSRF`) AFTER auth
  (`server.go:1143-1261`, `session.go:143-153`). A new `POST` row inherits both by method.
- **No request-rate limiter exists**; only a failure lockout (`internal/netid/limiter.go:140-162`)
  used by the pod and by sign-in. A ring rate limit is new code.
- `cairn-ui` already holds mutable state: cookie sessions and invites, in a file or in
  `internal/pgstore` (`internal/pgstore/migrate.go:38-88`), optional on `-db-dsn`
  (`cmd/cairn-ui/main.go:266-269`).

### Instances

- Instances exist only in the CLIENT (`internal/client/instances.go:100-105`, `DefaultAlias =
  "personal"`); neither server knows which instance it is. The established way to enable a
  feature on one deployment only is a flag with NO default, off when unset, blank refused —
  `-arc-journal` in both binaries (`cmd/cairn-ui/main.go:256-265`).

### The tooling repo: how a session id resolves to a pane today (read-only; one local measurement)

- **Source of truth is the agent ledger**, `~/.cache/agent-ledger/*.json`, one record per pane with
  `{runtime, session_id, last_activity_ts, window_id, pane_id, tmux_pid, …}`
  (`tooling:scripts/lib/agent_ledger.py:20-21, 128-129`), written by a Claude hook
  (`tooling:scripts/claude-hooks/agent-ledger-hook.py:86-88`) and an opencode plugin
  (`tooling:scripts/opencode/plugin/ledger.js:118-120`). `runtime` is a closed set of three values,
  two of them `claude` and `opencode` (`agent_ledger.py:234`).
- **The join** (`tooling:scripts/session-manager:3084-3242`): live `(session, index)` → `window_id`
  → ledger record; `claude_session_id = led.session_id or task.claude_session` (`:3241-3242`).
  Label and hotkey come from the slot table (`resolve_label` `:2248-2279`, `hotkey_display`
  `:2282-2309`); a window outside it has no hotkey.
- **Two guards, not one.** The task-file guard pins a recorded `(session, window_index)` to the
  slot its live `window_id` now holds and drops mismatches (`filter_live_tasks`, `:2892-2898`) —
  added because `renumber-windows` shifts indexes (docstring `:78-94`); `index_tasks_by_window`
  drops a slot two files claim (`:2907-2961`). The ledger guard keys on `window_id` and drops a
  record whose `tmux_pid` is not the live server's (`generation_mismatch`), because window ids
  restart at `@0` with the server (`agent_ledger.py:59-71, 647-661`).
- **Stated failure modes:** `ledger.conflicts` — two live records claim one `window_id`, newest
  wins, still reported (`agent_ledger.py:682-711`; typically a pane that ran one runtime then
  another); `records_no_window` — an agent outside tmux, counted and never joined
  (`agent_ledger.py:624-631`); `not_live`, `generation_mismatch`, and `generation_unchecked` — a
  record KEPT when either pid is missing (`agent_ledger.py:653-660`); read states `error`,
  `no_sentinel`, `unmeasured`, `partial` (`session-manager:3652-3708, 4382`); remote rows come
  over ssh (`:3623-3629`) and lack task-file data; `list-panes` and `list-windows` are separate
  calls, so a renumber between them is unguarded (`:2998-3002`).
- **Cost, ONE local measurement on ONE host at ONE moment** (`session-manager scan --host <local>
  --no-ch --json`, no ssh): 0.196 s wall; 72 windows, 61 carrying a session id (all `claude`, 0
  `opencode`, 11 none), 62 with a hotkey; ledger 188 records → 63 live, 8 no-window, 37 not-live,
  80 generation-mismatch, 2 conflicts. Per host it spawns `list-panes`, `list-windows`, one batched
  `capture-pane`, a ledger `sh -c` and a repo probe; the remote host costs the same over ssh with
  12 s timeouts. Not measured: the second host, an opencode-heavy moment, a cold cache.
- **Opencode ids.** The tooling repo treats the id as opaque (`session-manager:3250`); the trailer grammar's
  `sessionClass` is `[A-Za-z0-9][A-Za-z0-9_.-]{0,63}` (`internal/write/revision.go:71`), which
  admits both a uuid and a `ses_`-prefixed id, and `SessionComponent` is built from it
  (`bullet_request.go:24-26`). Presence must still carry `runtime`: the id alone does not say
  which runtime's resume command applies, and one pane can hold both in sequence (the conflict
  above).

### Ringing a bell in a pane — verified vs inferred

VERIFIED on a PRIVATE detached tmux server (`tmux -L <literal probe socket> -f /dev/null`, tmux
3.7c, kernel 7.2.3; the operator's server was never addressed), two windows, window 1 not current:

- Writing the single byte `0x07` to window 1's `#{pane_tty}` (a `crw--w----` device owned by the
  user) set `window_bell_flag=1` on window 1 and left window 0 at 0, under `monitor-bell on`.
- With `bell-action current` the `alert-bell` hook did NOT fire for that non-current window; with
  `bell-action any` it did. **The tooling repo sets `bell-action current`** with an `alert-bell` sound hook
  (`tooling:.tmux.conf:190-193`), so a ring to a background window raises the FLAG but plays no
  sound under today's config. The tooling repo's status styling reads `#{window_bell_flag}`
  (`tooling:scripts/tmux-idle-update.sh:46,57`) — that styling is the visible "find me" signal.
- Bytes written to the pane tty arrive as pane OUTPUT, not input: `XYZ` written there appeared in
  `capture-pane`; nothing reached the program's stdin. The kernel's input-injection path is off
  here (`/proc/sys/dev/tty/legacy_tiocsti` = 0).

INFERRED, not verified: that alacritty raises an X urgency hint on a tmux-forwarded bell (the tooling repo
sets `bell.duration = 0`, `tooling:nix/programs/alacritty/default.nix:87-90`, and i3 only colours
an urgent workspace, `tooling:nix/i3/config.nix:541`); and that a ring behaves the same on the
second host's tmux version.

**`send-keys` exists in the tooling repo** (`tooling:scripts/tmux-reply-agent:15,197,308`,
`tooling:scripts/session-write:22-30,106`, `tooling:scripts/lib/tmux_text_policy.py:44,56`) — the ring
executor must share no code path with any of them (decision 9).

## Decisions already taken (by the operator; not re-litigated here)

| # | decision | cost accepted |
|---|---|---|
| O1 | **Live arcs** = status `open` OR last-updated within 14 days, newest first, show-all toggle. last-updated = max(registration time, newest attributed bullet by any member session in a scope the viewer can read). Visibility unchanged: listed only when the HOME scope is readable (Q1 of the arcs plan). | An arc whose members wrote only in scopes you cannot read looks staler to you than to someone who can; that is the narrowing working, and the row says what its time is made of. |
| O2 | Clicking an arc resolves it on the EXISTING `/arc` page, extended with tabs mirroring the scope page's `?tab=`: scopes · sessions · entries touched, linking to `/scope`, `/session`, `/entry`. | `/arc` grows a walk that keeps bullet refs, not just scope names. |
| O3 | **Presence** = session id → {host label, tmux target, hotkey/label, runtime, last-seen}, pushed by a host agent on each operator machine to the PERSONAL instance ONLY, readable ONLY by the identity that pushed it, never shared through scope grants, TTL ~3 min, never sent to any other instance. Shown on the session page, session rows and arc rows when the viewer owns it. | A shared arc's other readers never see where it is running, by design. |
| O4 | **Bell** = queued pull: a CSRF-protected, rate-limited POST enqueues a ring only for a session whose LIVE presence belongs to the viewer; the host agent (outbound-only) fetches pending rings every few seconds and rings the bell in that pane; nothing connects inbound to a host. | Ring latency is the poll interval. |
| O5 | **Deliverable this round is this doc**, as a PR. The host agent is its own slice, implemented in the tooling repo; this plan names the contract between the two repos. | — |

## Design decisions this plan takes (each with its evidence)

1. **`cairn-ui` owns presence and the ring queue; the pod does not.** The owner key must equal
   the browser viewer's `(Kind, ID)`, and only `cairn-ui` over a control journal resolves a bearer
   credential and a GitHub session to the same principal (measured above); the pod's bearer path
   yields token-file PROJECT principals. And the ring is ENQUEUED by the browser, so whichever
   process holds the queue must be writable by the UI — the UI's arc-journal mount is read-only
   (`cmd/cairn-ui/main.go:256-265`), and two processes appending one file across pods is exactly
   the cross-host `flock` gap `journal.go:160-168` declares. No pod route, no corpus row, no
   conformance change.
2. **Presence and rings are EPHEMERAL and held in memory in the UI process** (a map keyed by
   owner; rows expire at `last_push + TTL`; rings expire unclaimed after 60 s). A restart loses at
   most one push interval of presence and any unclaimed ring — both self-heal on the next push or
   click. Not pgstore and not a journal: nothing here is a record anyone needs tomorrow, and a
   durable copy of "which hosts the operator uses" is a liability with no reader. ⚠ **This assumes
   ONE UI replica** — with two, a ring enqueued on one is invisible to an agent polling the other.
   The flag's help text states it; Open question P2 covers multi-replica.
3. **The host agent does NOT hold a control-plane credential. It holds a PRESENCE TOKEN** — a
   separate bearer secret, stored server-side as a digest in a file (`-presence-tokens`, no
   default), each row binding `digest → (owner Kind, owner ID, host label)`. It is unknown to
   `identity.Backends`, so it authenticates NOTHING but the agent routes: every browser row, the
   pod, and `POST /sign-in` answer it exactly as they answer garbage. That makes "a stolen agent
   token can write presence for its own owner and host, and claim that host's rings — nothing
   else" true by CONSTRUCTION rather than by a narrowing the sign-in finding above shows can be
   undone. Minting resolves a human-supplied owner (an email or a token-file identity) to the
   stable `(Kind, ID)` ONCE, at mint time, and stores the id — never the mutable email.
4. **The agent routes live on a SECOND LISTENER with its own two-row ledger**, enabled by
   `-presence-agent-addr` (no default). The browser surface's gates are derived from the METHOD
   and may never be relaxed by a route class (AGENTS.md, "TWO CROSS-SITE GATES"; `routes.go:28-39`),
   and a non-browser POST carries no `Origin` and no cookie, so it cannot pass them — and must not
   be made to. A separate mux keeps `TestTheRouteLedgerMatchesTheDispatchTable`'s claim ("every
   served path is in this map") true for the browser listener, gives the agent API a ledger of its
   own, and lets that listener refuse cookies outright (no session, so no CSRF surface to defend).
   Ingress exposes it as a separate path or host; that is a deployment change in the private repo.
5. **The owner check is ONE predicate, and every miss is one answer.** `presence.For(viewer,
   session)` returns a row only when `row.owner == (viewer.Kind, viewer.ID)` and the row is
   unexpired. Every surface (badge, page, ring enqueue) asks it and nothing else. A session with
   presence owned by someone else, with expired presence, or with none renders the SAME bytes and
   answers `POST /ring` the SAME way — so user B can neither read A's host names nor learn that A
   has a live pane for a session both can read.
6. **Presence is keyed `(owner, host label, session)` and a push REPLACES that host's whole set.**
   Each push is the agent's full current list for its host (the arc registrar's whole-report
   shape), so a pane that closed disappears on the next push rather than at TTL. Two hosts never
   overwrite each other because the host label comes from the TOKEN row, never the body.
7. **What a presence row carries, and what it never does.** Carried: `session`, `runtime`
   (`claude | opencode | other`), `host` (from the token), `target` (`<session>:<window>`, a
   display string), `label`, `hotkey` (display string or empty), `last_activity` (RFC 3339 from the
   ledger), `pushed_at` (UI clock). **Never carried**: pane tty path, pane id, window id, tmux pid,
   cwd, repo path, pane contents, transcript text, preview bytes. The UI never needs them — the
   agent re-resolves the pane locally at ring time (decision 9) — and every field not sent is one a
   compromised UI cannot leak. Bounded: ≤ 256 rows per push, each string ≤ 128 bytes, the session
   validated by `write.SessionComponent`; unknown fields refused (`DisallowUnknownFields`, the
   `arcs.DecodePayload` rule, `arcs.go:168-184`).
8. **A ring carries a session id and nothing else.** `POST /ring` with form fields `session` and
   `csrf`. No byte, message, key or count travels: the executor's only action is one constant
   byte. Rate limit: at most ONE pending ring per `(owner, session)` and a 10 s cooldown after a
   claim; at most 16 pending per owner; a ring past its 60 s TTL is dropped unclaimed (a bell
   minutes late is a wrong signal). Over-limit answers the same redirect as success — the page is
   not an oracle for the queue — and the operator log records the refusal.
9. **The executor rings by RE-RESOLVING, never by trusting the server.** On claiming
   `{ring_id, session}` the tooling repo's agent: re-reads the agent ledger through `agent_ledger`'s own
   live filter (window still live, `tmux_pid` equals the live server's — the generation guard),
   and is STRICTER than that filter in one place: the filter KEEPS a record when either pid is
   missing (`generation_unchecked`, `tooling:scripts/lib/agent_ledger.py:653-660`), and the executor
   refuses it (`unresolved`) — an unchecked generation is exactly the window-id-reuse case; it
   requires EXACTLY ONE live record with that session id (zero or a conflict → report
   `unresolved`, ring nothing); asks tmux for that pane's `#{pane_tty}`; confirms it is a character
   device under `/dev/pts/` owned by the agent's uid; opens it `O_WRONLY|O_NOCTTY|O_NOFOLLOW` and
   writes the single byte `0x07`. It never calls `send-keys`, `paste-buffer`, `load-buffer`, any
   ioctl, or any tooling-repo module that does; it never opens the tty for reading. A stale presence row
   or a reused window id therefore cannot redirect a ring: the target is decided on the host at ring
   time by the same guard session-manager trusts.
10. **Arc "last updated" is computed in ONE whole-store walk, with the source named.** For each
    visible arc: `reg_at` = its latest `registered_at` (pod clock, always present —
    `arcs.go:358`; `reported_at` is shown in the tooltip, not used, because it is optional and on
    the tooling's clock); `bullet_at` = the newest bullet date by any member session over the
    caller's narrowed index, **clamped to today** (a writer-declared future date must not pin an arc
    to the top) and taken as that day's 00:00 UTC; `last_updated = max(reg_at, bullet_at)`. The row
    renders which won — "registered 3h ago" or "bullet today" (day precision, `dateAgo`'s rule,
    `arcs.go:554-563` (#193)) — so a day-precision date is never displayed as an hour.
11. **Live = `open` OR `last_updated` within 14 days; `unknown` is NOT `open`.** Q4 of the arcs
    plan holds: an `unknown` arc is live only by recency. Sorted by `last_updated` descending, ties
    by `(home, slug)`. `?all=1` shows every visible arc in the same order; the count of hidden
    arcs is printed, so the default view never reads as complete.
12. **The arcs-first page is a NEW row, `GET /arcs`, linked from the nav — `/` is NOT replaced.**
    `/` is the entries index with its search and tag filter (`routes.go:118, 298-323` (#193)), and
    its history includes a row deleted for rendering without consulting authority
    (`routes.go:70-78`). Making `/` redirect to `/arcs` is a one-line follow-up the operator can
    take after living with it (Open question P1).
13. **No new embedded asset, no script.** Tabs are server-rendered links (`?tab=` on `/arc`, the
    scope page's rule); the bell is a plain `<form method="post">` with the CSRF field. So
    `AllowedScriptSources`, the stylesheet digest rows and the `onlyGo` filter do not move.

## Contract between cairn and the tooling repo (S2 ⇄ S3)

```
POST {agent-base}/agent/v1/presence          Authorization: Bearer <presence token>
  body: {"schema": 1, "rows": [
          {"session": "s-0001", "runtime": "claude", "target": "notes:3",
           "label": "notes", "hotkey": "Alt+n", "last_activity": "2000-01-02T03:04:05Z"},
          {"session": "ses_example0000000000000000001", "runtime": "opencode",
           "target": "notes:4", "label": "notes", "hotkey": "", "last_activity": ""}]}
  200 X-Presence-Status: presence-replaced   body: "rows=2 refused=0"
  400 malformed (unknown field, oversize, bad session)   401 unknown/garbage token (uniform)

POST {agent-base}/agent/v1/rings/claim        Authorization: Bearer <presence token>
  body: {}            (POST, not GET: claiming CHANGES state)
  200 {"schema": 1, "rings": [{"ring_id": "r-0001", "session": "s-0001"}]}
      — rings for THIS token's (owner, host) only; each returned exactly once

POST {agent-base}/agent/v1/rings/report       Authorization: Bearer <presence token>
  body: {"schema": 1, "results": [{"ring_id": "r-0001", "outcome": "rang"}]}
      outcome ∈ rang | unresolved | ambiguous | not-a-tty
```

The report row lets the session page say "rang" / "pane not found on host-a" instead of
pretending. Cadence: presence every 60 s (the TTL of ~3 min tolerates two missed pushes), claim
every 5 s. Both from ONE systemd user service/timer pair per host (the tooling repo pattern:
`tooling:nix/home.nix:4314-4355, 4515-4528`), outbound HTTPS only, token read from a 0600 file and
sent from a curl config file or an in-process header, never argv (the
`tooling:scripts/tmux-snapshot-push.sh:55,95,252-263` rule).

## Threat model — presence and bell

| threat | control |
|---|---|
| **User B reads A's presence** (host labels, tmux targets) | Decision 5: one owner predicate; non-owner pages byte-identical to no-presence pages (e2e (b)). Presence is never in a grant, never in a scope, never on `/scope?tab=sessions` for anyone but its owner. |
| **User B rings A's pane** | `POST /ring` resolves the viewer's `(Kind, ID)` from the authenticated identity and enqueues only when `presence.For(viewer, session)` is non-nil; else the same answer as no presence (e2e (c)). The queue is keyed by owner; a claim returns only rings for the claiming token's `(owner, host)` (e2e (d)). |
| **Cross-site ring** (a page on another origin posts the form) | Both existing gates by METHOD: same-origin before auth, CSRF after (`server.go:1143-1261`). No new class. |
| **Replay / flooding** | A replayed form re-rings at most once per 10 s cooldown, one pending per `(owner, session)`, 16 per owner, 60 s TTL (decision 8). Ring ids are random and claim-once. The agent API's failed-token lockout reuses `netid.RateLimiter`. |
| **A stolen host-agent token** | Decision 3: it authenticates only the agent listener. Blast radius: write (replace) presence rows for ITS owner and ITS host label; claim rings queued for that `(owner, host)` (and so suppress them); report outcomes. It cannot read any scope, sign in, read presence back, enqueue a ring, or address another host's rings. Revocation = delete the digest row; the file is re-read on change. |
| **A stolen browser session of the owner** | Equivalent to the owner: can read the owner's presence and ring the owner's panes — bell only (below). No new power beyond what the session already had over the owner's store. |
| **Stale presence** (pane closed, window renumbered, tmux server restarted and `@N` reused) | Server: TTL ~3 min plus whole-host replace on every push (decision 6). Host: the executor re-resolves at ring time through the generation guard and refuses zero-or-many matches (decision 9). A stale row can at worst show an old target for ≤ 3 min; it can never aim a ring. |
| **What the bell can do** | Write ONE constant byte `0x07` to a pane tty the agent's uid owns. Writing the slave side is pane OUTPUT (verified: text written there is captured as output, not delivered as input); input injection via `TIOCSTI` is disabled on the measured kernel and the executor makes no ioctl at all. The ring payload carries no bytes, so there is nothing to inject even if the executor were wrong. Guard: a structural test that the executor module imports no tooling-repo module that reaches `send-keys`, and a behavioural one on a private tmux server that the pane's contents and the program's stdin are unchanged. |
| **The client instance receives presence** | Three walls: (1) the client instance's UI runs without `-presence-agent-addr` and `-presence-tokens`, so the agent routes do not exist there and every presence lookup is empty; (2) the host agent is configured with ONE base URL of its own and never reads the cairn client's `instances/*.env` or routes — it is not the cairn client; (3) at startup the agent REFUSES if its base URL's origin equals that of any NON-default instance file it can see (a deterministic check, not a convention). |
| **A writer forges a future bullet date to pin an arc** | Clamped to today (decision 10). |
| **Presence makes a session page "found" for someone who could not otherwise see it** | Presence decorates a page that `report.SessionAcross` already found; it never changes `Found()` (`sessionpage.go:106-113` (#193)), so the uniform 404 is untouched. |

## Slices

| slice | repo | what | ledgers it moves | mergeable alone because |
|---|---|---|---|---|
| S0 | cairn | **Report the sign-in widening as its own issue** (finding above): a narrowed credential presented to `POST /sign-in` yields a full-authority session. Not a presence prerequisite under decision 3 — listed because the plan found it. | — (an issue, then whatever fix the operator chooses) | Independent of everything below. |
| S1 | cairn | **Arcs-first page + `/arc` tabs, no presence.** `GET /arcs` (decisions 10–12); `/arc?home=&slug=&tab=` ∈ {`""` scopes, `sessions`, `entries`}; a structured-only extension of the arc report that keeps member `BulletRef`s (no `RenderText` change, so the pod's and CLI's bytes do not move — the `SessionAcross` arrangement). | `routes` + constants; `routes_test.go` hand ledger, `bareGETAnswer`, `contentAuthority`; `uiaudit/targets.go` (`/arcs` in `linkExpanded`) + `boot.go` fixtures (a recent, an open-old, a closed-old arc); `internal/ui/README.md`; `tests/control_mutants.py`. NOT `flake.nix`, NOT the corpus. | Read-only over the journal and store that exist. Depends on #193 merging (the tab code). |
| S2 | cairn | **Presence store + agent API + owner-only read.** `internal/presence` (stdlib-only: the owner predicate, TTL, whole-host replace, the ring queue's types); `cmd/cairn-ui` flags `-presence-agent-addr`, `-presence-tokens`, `-issue-presence-token`; the agent listener's own route ledger and its test; refusal to start with one of the two flags set and not the other. | New agent-API ledger + `DeclaredRoutes`-style test; `cmd/cairn-ui` flag tests (blank refused, `-arc-journal`'s policy); `internal/ui/README.md`; `depspolicy` UNCHANGED (stdlib-only package; asserted, not moved). | Off by default; no browser surface changes yet. |
| S3 | **tooling repo** | **Host agent.** One script + one systemd user service/timer per host: presence push every 60 s from session-manager's LOCAL scan (`--host <local> --no-ch --json`, never the ssh leg — each host reports itself), ring claim every 5 s, executor per decision 9, report. Token in a 0600 file. | the tooling repo's own test suite and nix module; no cairn ledger. | Talks only the contract above; cairn is complete without it, and it is inert until a token is minted. |
| S4 | cairn | **Presence badges.** `host · target · hotkey · runtime · seen Ns ago` on the session page, on session rows (scope tab, arc tabs) and a "live pane" badge on `/arcs` and `/arc` rows — all through the one predicate. | `internal/ui` render tests; uiaudit fixture (an owner and a non-owner viewer); README. No route row. | Read-only over S2's store. |
| S5 | cairn + tooling repo | **Bell.** `POST /ring` (browser ledger, `0` class — gates by method), the queue + rate limits (decision 8), the button on the session page and session rows only where S4 shows presence, a 303 back to the session page; the tooling repo's executor already exists from S3 and is switched on. `tests/presence/e2e.sh` (the closing check). | `routes` + `routes_test.go` (`POST /ring`); `contentAuthority` N/A (not content); `tests/control_mutants.py`; README. | Needs S2–S4; everything before it ships useful without it. |

Sizes are not estimated; nobody has measured these.

### Test plan per slice (each names its negative controls)

**S1.**
- Literal-expectation render tests over a synthetic journal: order by `last_updated`, the
  14-day boundary measured at 13 d 23 h and 14 d 1 h (not on the boundary), `open` kept at 400
  days, `unknown` at 20 days hidden, `?all=1` shows it, hidden count printed.
- **Negative controls:** an arc homed in an unreadable scope is absent from `/arcs` AND its
  members' bullets in readable scopes do not resurrect it; a member's bullet in an unreadable scope
  does NOT move `last_updated` (two viewers, one store, different times — a relationship); a
  future-dated bullet does not sort above today.
- `/arc` tabs: an unknown `tab` renders the default tab (the scope page's rule); every miss stays
  `ArcUnregisteredBody` with the same bytes across tabs.
- Mutation rows: drop the clamp; use `reported_at` instead of `registered_at`; count `unknown` as
  open; skip the visibility check in the bullet walk — each must be killed by a NAMED test.
- Cost: extend `BenchmarkSessionPageAndScopeTabs` with `/arcs` at both sizes; the claim is "≤ the
  session page's walk", stated as a ratio.

**S2.**
- Owner predicate as a relationship: owner A sees the row; B, the bare token-file row, a project
  principal with the same `ID` string but different `Kind`, and A after TTL see nothing — each
  case shown RED by deleting one clause of the predicate.
- Token isolation (the decision-3 claim): a presence token presented as `Authorization: Bearer` to
  every browser GET row, to `POST /sign-in`'s form and to the pod is refused exactly as a random
  token is (byte-compare the bodies). Positive control: a real control credential IS accepted on
  the same request.
- Whole-host replace: push {s1, s2} then {s2} → s1 gone; `host-b`'s push leaves `host-a`'s rows.
- Body bounds and unknown-field refusal, each with a just-under-the-bound positive control.
- Startup: `-presence-agent-addr` without `-presence-tokens` (and vice versa) refuses; blank values
  refuse; neither set → no listener (probe the port: connection refused).

**S3 (the tooling repo).**
- Executor against a PRIVATE tmux server (`tmux -L <literal test socket>`): ring → bell flag on the
  target window only; two ledger records for one session → `ambiguous`, nothing written; a record
  whose `tmux_pid` is a previous server's → `unresolved`; a record with NO `tmux_pid` (kept by the
  ledger filter as `generation_unchecked`) → `unresolved`; a pane tty replaced by a symlink or a
  regular file → `not-a-tty`.
- No-input property: the target pane runs `cat > <file>`; after a ring the file is empty and
  `capture-pane` is unchanged. Structural: the executor's import graph contains no module that
  calls `send-keys`/`paste-buffer` (an asserted ledger of its imports, failing on GROW).
- Push: rows from a fixture session-manager JSON; a remote row in the fixture is NOT pushed.
- Instance wall: a base URL equal to a fixture non-default instance's origin refuses to start.
- Token never in argv: assert the spawned command lines in a fake runner.

**S4.**
- Owner and non-owner render the same session page; the non-owner's bytes equal the no-presence
  bytes (the e2e (b) relation, also as a unit test). A badge with `last_activity` empty renders no
  age rather than "0s".
- Mutation: render presence without the owner check — the non-owner byte-compare must go red.

**S5.**
- `POST /ring` without `Origin`, with a foreign `Origin`, without the CSRF field → the existing
  gates' refusals (no new code path; asserted so a future class cannot bypass them).
- Owner A rings → one pending; repeat inside 10 s → no second; B rings A's session → same 303 as no
  presence, and the queue unchanged (read the queue in-process).
- TTL: an unclaimed ring is gone at 61 s; a claim at 59 s returns it once and never again.
- e2e as the closing check, with `--self-test` sabotaging the owner check and the executor.

## Open questions (each with a recommendation)

- **P1. Should `/` become the arcs page?** Recommend NO for S1: ship `GET /arcs` plus a nav link,
  and flip `/` only after the operator has used it — the root row's search and tag filter have
  their own consumers.
- **P2. More than one UI replica.** In-memory state (decision 2) silently splits across replicas.
  Recommend: keep one replica (it is a personal instance) and state it in the flag's help; if a
  second replica is ever wanted, move presence and rings to `internal/pgstore` behind the same
  `internal/presence` interface — the UI already owns that database.
- **P3. Mint presence tokens from the CLI or the browser?** A browser self-mint page would bind
  the owner by construction (the viewer mints for themselves) but puts a bearer secret in a page
  and needs a writable token store. Recommend CLI mint on `cairn-ui` for S2, resolving the owner
  from an email or token-file identity once, and revisit if a second person ever runs an agent.
- **P4. Make the bell audible for background windows?** Under the tooling repo's `bell-action current` a ring
  sets the flag but plays no sound (verified above). Recommend leaving tmux's config alone and
  relying on the flag's status styling for v1; if that is not findable enough, the tooling-repo change is
  `bell-action any` (a global behaviour change the operator should choose), not an executor feature.
- **P5. Presence for a session the owner cannot see any writes of.** The session page 404s for an
  id with no visible writes (`sessionpage.go:106-113` (#193)). Recommend NOT changing that in S4:
  presence decorates found pages only; a "my live sessions" list is a separate page if wanted.
- **P6. The other host's tmux.** Every bell measurement here is one host, one tmux version, a
  private server. Recommend S3's executor tests run on both hosts before S5 merges, and the report
  outcome (`rang`/`unresolved`) is what the page shows, so a host where it fails says so.
- **P7. The sign-in widening (S0).** Out of this plan's path by decision 3, but real. Recommend an
  issue now; whether a session should inherit its credential's narrowing is a control-plane
  decision with its own blast radius.

## What this plan could not measure

- Whether the deployed personal UI runs with a control journal and `-db-dsn` (the deployment
  manifests are private); decision 1's owner-key argument requires the journal there.
- The second host's session-manager scan and tmux bell behaviour; any opencode-heavy moment
  (the one scan held 0 opencode rows).
- Whether alacritty raises an urgency hint on a forwarded bell (inferred from config only).
- `/arcs` cost on the real store (only the synthetic benchmark, on a non-pinned toolchain).
- #193's final shape: every `(#193)` citation must be re-read after it merges.
