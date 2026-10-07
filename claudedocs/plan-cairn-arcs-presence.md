# Plan: an arcs-first browser, and where a live session is running

This is a DESIGN, not a measurement of anything built. Nothing below exists yet. Every claim
about today's behaviour was read off the code at `origin/main` `64475d7` (cairn) or the operator
tooling repo (read-only; its paths are prefixed `tooling:`), and carries a `file:line` so it can
be re-checked. Claims about the browser surface's scope tabs and session page were read off
**PR #193's branch (`zach/ui-scope-tabs-sessions` at `b1d82ad`), which is OPEN and NOT merged** —
they are marked `(#193)` and must be re-read once it lands. Every number about real data names the
instrument that produced it and what that instrument cannot see. Examples are synthetic: hosts
`host-a`/`host-b`, tmux target `notes:3`, session ids `s-0001`, year-2000 dates.

**Revision 2** applies the PR's round-0 and round-1 audit rulings: the `/arc` entries tab, the
ring report route, the ring cooldown/cap and the client-side instance wall are gone; the presence
push extends the existing host→server push unit; rings are claimed by a long-running service; two
hosts presenting one session have a deterministic target; presence fails closed under credential
narrowing; and the closing condition names one runnable check per repo.

## Goal

Two operator ideas, carried as one plan because the second decorates the first:

1. **An arcs-first page.** The browser opens on the arcs that are LIVE — newest activity first,
   each with when it was last updated — and clicking one resolves it on `/arc`: its scopes and its
   sessions, linking to the existing `/scope` and `/session` pages.
2. **Where is that session running?** For a session the VIEWER owns that is live in a tmux pane
   on one of the viewer's machines, show the host label, the tmux target (`<session>:<window>`)
   and the hotkey — and offer a button that rings the terminal bell in that pane, so the window
   is easy to find. Bell only: nothing in this design can type into a pane.

### Premise, and what would make this work unnecessary

The premise is that the operator resolves sessions **from the cairn browser surface** — a scope
or arc page names a session, and the next question is "which window is that". Two things would
make the presence half unnecessary, and it should be dropped rather than built if either holds:

- **The operator already answers "which window" elsewhere.** The tooling repo's
  `scripts/session-manager` resolves a session id to host + tmux slot + hotkey today (measured
  below), and the tooling repo already runs a host→server push of that tool's JSON on a 2-minute
  user timer to another of the operator's services (`tooling:scripts/tmux-snapshot-push.sh`, unit
  `tooling:nix/home.nix:4314-4355`, timer `:4515-4528`). If pasting a session id into that tool
  is good enough, the cairn side is a second copy of an answer that exists.
- **The session ids on cairn pages are not the ones in panes.** They share one string space (the
  write trailer's `<session>` is the runtime's session id; the agent ledger records the same id
  per pane), but a trailer's session is SELF-DECLARED by the writer
  (`internal/write/bullet_request.go:15-28`). If writers stopped declaring the real id, presence
  would join nothing.

The arcs-first half has no such escape: nothing today lists arcs across scopes.

### closing-condition

- **closing-condition:** `check` — cairn slices S1, S2, S4 and S5 are MERGED on cairn `main`
  (verified by content, not ancestry), the tooling-repo slice S3 is merged there, AND BOTH
  runnable checks below exit 0 on their repos' main branches. Each exits **2** — "could not
  vouch", never a skip and never 0 — when its prerequisite is missing (a Go toolchain / built
  `cairn-ui` for the first; `tmux` or the executor for the second).
  1. **cairn: `tests/presence/e2e.sh`**, run in the `go` CI job beside `tests/arcs/e2e.sh`
     (`.github/workflows/ci.yml:1008-1010`). It boots `cairn-ui` with presence enabled on a
     synthetic store and journal and proves the QUEUE, OWNER and HOST semantics up to "the ring
     was claimed by the right agent token" — there is no executor in cairn. As relationships:
     (a) `GET /arcs` lists a recent and an open arc newest first and omits a closed arc older than
     14 days until `?all=1`;
     (b) owner A's push token pushes presence for `s-0001` on `host-a`; A's session page shows
     `host-a · notes:3`, while owner B's page for the same session is BYTE-IDENTICAL to the page
     with no presence at all;
     (c) A's `POST /ring` enqueues; B's `POST /ring` for the same session gets the same answer as a
     session with no presence and leaves the queue unchanged;
     (d) A's `host-a` claim token claims the ring exactly once; A's `host-b` claim token, owner B's
     claim token for `host-a`, and a just-revoked claim token each claim nothing;
     (e) with `s-0001` presented by both `host-a` and `host-b`, the ring goes to the host whose row
     has the newest `last_activity`, and on a tie to the byte-wise smaller host label;
     (f) a narrowed credential, and a session minted from one, see no presence and cannot ring.
     `--self-test` sabotages EVERY clause (a)–(f) in turn and must report each caught (exit 2 if
     any sabotage is not caught).
  2. **tooling repo: the S3 executor test**, in that repo's own suite (which tier has `tmux` is
     not measured here — the test must exit 2 rather than skip where it is absent, so a tier
     without it goes red instead of vacuously green). Against a PRIVATE tmux server
     (`tmux -L <literal test socket>`, never the default), a claimed ring sets `window_bell_flag=1`
     on the target window and on no other, and the pane's captured contents and its program's
     stdin are unchanged; its own `--self-test` sabotages the target resolution and the no-input
     property and shows each go red.

Post-close rollout (NOT part of the closing condition): the personal instance runs with presence
enabled, each operator host runs the S3 services, and a click on the deployed session page's bell
lights the right window — an operator judgement over the window's status-line styling.

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
  `registered_at` (`arcs.go:333-338`, `journal.go:206-208` → `arc-unchanged`). But the tooling
  repo's registrar sets `reported_at` to NOW on every call
  (`tooling:scripts/lib/handoff_register.py:151`), so every push differs and is appended. It is
  called only from the two success arms of a handoff write, after the commit is on the remote,
  non-blocking (`tooling:scripts/lib/handoff_doc.py`, `_register_arc`, ~`:8827-8857`). So a
  registration's age is "time since the last handoff", which for an arc mid-flight can be days.

### Bullet dates — the only time the store carries

- A write edge's `Date` is the bullet's own `- YYYY-MM-DD:` opener or `""`; it is "the ONLY time
  the bytes carry … There is no time of day and no write timestamp anywhere else"
  (`internal/touch/touch.go:46-49`). ~44% of the oracle's live corpus is undated
  (`internal/store/journal.go:90-93`).
- Per session per scope, `touch.Session.FirstDate/LastDate` are the min/max non-empty dates
  (`touch.go:73-76`) — enough for "newest member bullet" without a new parser or a bullet list.
- A bullet date is the pod's clock on an append and the WRITER'S WORD on a `put`/`create`
  (`touch.go:46-48`), so a future date is writable — and so is a trailer naming any session
  (`bullet_request.go:15-28`).

### The browser surface after #193 (OPEN — read off its branch)

- Routes are an exact-match map with operands in query parameters (`internal/ui/routes.go:117-212`
  (#193)): `GET /arc` (`:126`) and `GET /session` (`:132`) are `classContent`; there is no
  arcs-index row.
- The scope page has tabs `?tab=` ∈ {`""` entries, `sessions`, `arcs`} (`routes.go:355-372`,
  `render.go:254-297` (#193)); an unknown value renders the default tab, never a 400.
- `/arc` renders ONE arc: summary stats, declared scopes (narrowed), members with the readable
  scopes each wrote in (`internal/ui/arcs.go:141-181, 461-536` (#193)). Every miss is one 404 with
  `report.ArcUnregisteredBody` (`arcs.go:120-128` (#193)). It has no tabs today.
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

### What adding `/arc` actually touched (#186, `4714652`, from `git show --stat`)

`.github/workflows/ci.yml` (the `control_mutants.py` count in the step name and comments,
190 → 200), `cmd/cairn-ui/main.go` + `cmd/cairn-ui/arcjournal_test.go` (the read-only
`-arc-journal` flag and its inside-the-store refusal), `internal/control/README.md`,
`internal/report/arcs.go` + `internal/report/sessions.go` (line methods exported for the page),
`internal/ui/arcs.go` + `arcs_test.go`, `internal/ui/render.go`, `internal/ui/routes.go` (the row
and constants), `internal/ui/routes_test.go` (the hand ledger, `bareGETAnswer`,
`contentAuthority` — `:56-90, :401, :685` (#193)), `internal/ui/server.go`,
`internal/ui/README.md`, `tests/control_mutants.py` (ten rows), `uiaudit/boot.go` (fixture arcs)
and `uiaudit/targets.go` (`linkExpanded`, `:180` (#193)). It did NOT touch `flake.nix`: the
`onlyGo` filter (`flake.nix:351-455`) moves only for a new EMBEDDED asset (as `filter.js` did,
`:431-433`). A browser row touches neither `api.DeclaredRoutes()` nor
`tests/conformance/requests.json` (`routes.go:17-20`).

Two CI floors are relevant to any new tested package: the `go` job's per-package `ok` floor is a
`<` comparison (`if [ "$ok" -lt 19 ]`, `.github/workflows/ci.yml:831`) — a package added without
moving it is silently absorbed; and `tests/control_mutants.py`'s `PKGS` (`:88`) is pinned by
`tests/test_control_mutant_count_is_pinned.py`, which forces the counts and package enumerations
spelled in `ci.yml` and the READMEs to follow any edit.

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
  (`internal/identity/supabase.go:240-265`).
- **The pod's bearer path never sees a journal user.** `cmd/cairn-server` authenticates machine
  tokens against the token-file projection (`cmd/cairn-server/main.go:429`), where every row is a
  `project` principal (`internal/control/tokenfile/source.go:365, 376-387`).
- **There is no capability narrower than a scope verb.** Verbs are a closed `{read, write, admin}`
  (`model.go:50-75`); `Credential.NarrowedScopes` only intersects scopes (`model.go:277-288`).
- 🔴 **FINDING (pre-existing, read not exercised): a narrowed credential gains its principal's FULL
  authority by signing in.** `handleSignIn` discards the narrowed authorization
  (`principal, _, err := s.credentials.Authenticate(presented)`, `internal/ui/session.go:221`);
  `openSession` stores only `Kind` and `Principal` (`session.go:279-283`); the cookie backend
  re-resolves with no narrowing (`Auth: control.Resolve(model, principal)`,
  `internal/identity/cookiesession.go:102`). A companion change (separate PR) makes browser
  sign-in refuse narrowed credentials; this plan references it and does not depend on its details
  (decision 11).

### Cross-site gates, client identity and rate limits on the browser surface

- `stateChanging` is every method but GET/HEAD/OPTIONS (`internal/ui/server.go:1273-1280`). For
  such a request the dispatcher requires same-origin BEFORE auth — `sameOrigin` refuses a missing
  `Origin` (`session.go:71-81`) — and a CSRF token AFTER auth (`server.go:1143-1261`,
  `session.go:143-153`). A new `POST` row inherits both by method.
- **No request-rate limiter exists**; only a failure lockout (`internal/netid/limiter.go:140-162`)
  used by the pod and by sign-in.
- **The lockout keys on a client identity that is only real behind an allowlist.**
  `cmd/cairn-ui/main.go:474-509` refuses to start when the bind is reachable and no trusted-proxy
  allowlist is set, because the client-IP header is forgeable by every peer outside it.

### Instances

- Instances exist only in the CLIENT (`internal/client/instances.go:100-105`, `DefaultAlias =
  "personal"`); neither server knows which instance it is. The established way to enable a
  feature on one deployment only is a flag with NO default, off when unset, blank refused —
  `-arc-journal` in both binaries (`cmd/cairn-ui/main.go:256-265`). That is a MANIFEST
  convention, not a structural property: nothing stops the same flag being set on a second
  deployment.

### The tooling repo: how a session id resolves to a pane today (read-only; one local measurement)

- **Source of truth is the agent ledger**, `~/.cache/agent-ledger/*.json`, one record per pane with
  `{runtime, session_id, last_activity_ts, window_id, pane_id, tmux_pid, …}`
  (`tooling:scripts/lib/agent_ledger.py:20-21, 128-129`), written by a Claude hook
  (`tooling:scripts/claude-hooks/agent-ledger-hook.py:86-88`) and an opencode plugin
  (`tooling:scripts/opencode/plugin/ledger.js:118-120`). `runtime` is a closed set of three values,
  two of them `claude` and `opencode` (`agent_ledger.py:234`). The ledger read prints a sentinel
  line carrying the live tmux server's pid (`agent_ledger.py:535-544`).
- **The join** (`tooling:scripts/session-manager:3084-3251`): live `(session, index)` →
  `window_id` → ledger record; `claude_session_id = led.session_id or task.claude_session`
  (`:3241-3242`) — so a row's id can come from an untrusted task file when no ledger record
  exists; each row also carries the whole ledger record as `"ledger"` (`:3251`) and a
  `pane_preview` when asked (`:3236`). Label and hotkey come from the slot table
  (`resolve_label` `:2248-2279`, `hotkey_display` `:2282-2309`).
- **Two guards, not one.** The task-file guard pins a recorded `(session, window_index)` to the
  slot its live `window_id` now holds (`filter_live_tasks`, `:2892-2898`; `renumber-windows`
  shifts indexes, docstring `:78-94`); `index_tasks_by_window` drops a slot two files claim
  (`:2907-2961`). The ledger guard keys on `window_id` and drops a record whose `tmux_pid` is not
  the live server's (`generation_mismatch`), because window ids restart at `@0` with the server
  (`agent_ledger.py:59-71, 647-661`) — but it KEEPS a record when either pid is missing
  (`generation_unchecked`, `:653-660`).
- **Stated failure modes:** `ledger.conflicts` (two live records claim one `window_id`; newest
  wins, still reported, `agent_ledger.py:682-711`); `records_no_window` (`:624-631`); `not_live`,
  `generation_mismatch`, `generation_unchecked`; read states `error`, `no_sentinel`,
  `unmeasured`, `partial` (`session-manager:3652-3708, 4382`); `list-panes` and `list-windows`
  are separate calls, so a renumber between them is unguarded (`:2998-3002`).
- **Cost, ONE local measurement on ONE host at ONE moment** (`scan --host <local> --no-ch
  --json`, no ssh): 0.196 s wall; 72 windows, 61 carrying a session id (all `claude`, 0 `opencode`,
  11 none), 62 with a hotkey; ledger 188 records → 63 live, 8 no-window, 37 not-live,
  80 generation-mismatch, 2 conflicts. Not measured: the second host, an opencode-heavy moment.
- **Opencode ids.** The tooling repo treats the id as opaque (`session-manager:3250`); the trailer
  grammar's `sessionClass` is `[A-Za-z0-9][A-Za-z0-9_.-]{0,63}` (`internal/write/revision.go:71`),
  which admits both a uuid and a `ses_`-prefixed id (`bullet_request.go:24-26`). Presence still
  carries `runtime`: one pane can hold both runtimes in sequence (the conflict above).

### The existing host→server push unit (what S3 extends)

- `tooling:scripts/tmux-snapshot-push.sh` is a DELIBERATE DUMB PIPE: it posts `session-manager
  --json --pane-preview` VERBATIM (`:15-22, :157`) and says not to reshape the payload there.
- It runs on ONE host only and covers both: the collector ssh'es to the other host, so the unit
  is gated to one host as a correctness requirement — two hosts each pushing a two-host document
  would fight over every row (`tooling:nix/home.nix:270-274`, comment `:4524-4527`).
- `Type = "oneshot"`, `TimeoutStartSec = 150`, distinct non-zero exit codes as its only alarm,
  deliberately no `OnFailure` (`home.nix:4318-4356`; codes at `tmux-snapshot-push.sh:28-38`);
  timer `OnStartupSec = 1min`, `OnUnitActiveSec = 2min`, **no `AccuracySec`** (`home.nix:4519-4522`)
  — so systemd's default 1-minute accuracy applies and ticks land 2–3 minutes apart.
- Token from a 0600 file, sent from a curl config file, never argv
  (`tmux-snapshot-push.sh:55, 95, 115, 252-263`).

### Ringing a bell in a pane — verified vs inferred

VERIFIED on a PRIVATE detached tmux server (`tmux -L <literal probe socket> -f /dev/null`, tmux
3.7c, kernel 7.2.3; the operator's server was never addressed), two windows, window 1 not current:

- Writing the single byte `0x07` to window 1's `#{pane_tty}` (a `crw--w----` device owned by the
  user) set `window_bell_flag=1` on window 1 and left window 0 at 0, under `monitor-bell on`.
- With `bell-action current` the `alert-bell` hook did NOT fire for that non-current window; with
  `bell-action any` it did. **The tooling repo sets `bell-action current`** with an `alert-bell`
  sound hook (`tooling:.tmux.conf:190-193`), so a ring to a background window raises the FLAG but
  plays no sound. The tooling repo's status styling reads `#{window_bell_flag}`
  (`tooling:scripts/tmux-idle-update.sh:46,57`) — that styling is the visible "find me" signal.
- Bytes written to the pane tty arrive as pane OUTPUT, not input: `XYZ` written there appeared in
  `capture-pane`. The kernel's input-injection path is off here
  (`/proc/sys/dev/tty/legacy_tiocsti` = 0).

INFERRED, not verified: that the terminal raises an urgency hint on a forwarded bell (its config
sets `bell.duration = 0`, `tooling:nix/programs/alacritty/default.nix:87-90`; the window manager
only colours an urgent workspace, `tooling:nix/i3/config.nix:541`); and that a ring behaves the
same on the second host's tmux version.

**`send-keys` exists in the tooling repo** (`tooling:scripts/tmux-reply-agent:15,197,308`,
`tooling:scripts/session-write:22-30,106`, `tooling:scripts/lib/tmux_text_policy.py:44,56`) — the
ring executor must share no code path with any of them (decision 9).

## Decisions — who chose what

### Chosen by the OPERATOR (not re-litigated)

Quoted as relayed in this plan's brief and in the review rulings.

| # | the operator's choice | cost accepted |
|---|---|---|
| O1 | **Live arcs** — "status open OR last-updated within 14 days, newest first, with a show-all toggle". **last-updated** — the option the operator picked: "last update (newest handoff, or newest bullet a member wrote)", i.e. max(newest registration time, newest attributed bullet by any member session in a scope the viewer can read). Visibility unchanged: "an arc is listed only when its HOME scope is readable". | Self-declared attribution: anyone who can write a readable scope can keep an arc live by appending a bullet whose trailer names a member session (`bullet_request.go:15-28`); the future-date clamp (decision 10) blocks only future dates. Accepted, because visibility is unchanged — it reorders what a reader could already see. |
| O2 | "Clicking an arc resolves it: extend the existing `/arc` page (likely tabs mirroring the scope page's `?tab=`)", linking to `/scope` and `/session`. Review ruling D1: tabs are **scopes · sessions** only; no entries tab. | — |
| O3 | **Presence** — "session id → {host label, tmux target, hotkey/label, runtime, last-seen}, pushed … to the PERSONAL instance ONLY, readable ONLY by the identity that pushed it (owner-only, never shared via scope grants), short TTL (~3 min), never sent to any other instance". | A shared arc's other readers never see where it is running. |
| O4 | **Bell** — "queued pull: … a CSRF-protected, rate-limited POST that enqueues a ring only for a session whose live presence belongs to the viewer; the host agent (outbound-only) fetches pending rings every few seconds … nothing connects inbound to the hosts". Review ruling D3: the rate limit is one pending ring per (owner, session) plus a 60 s TTL. | Ring latency is the claim interval. |
| O5 | "Deliverable this round = the plan doc only"; the host agent is its own slice in the tooling repo. | — |

### Chosen by the AGENT writing this plan (open to review)

Every decision below is the plan author's, with its evidence; any of them can be overruled.

1. **`cairn-ui` owns presence and the ring queue; the pod does not.** Only `cairn-ui` over a
   control journal resolves a bearer credential and a GitHub session to one `(Kind, ID)`; the pod's
   bearer path yields token-file PROJECT principals. The ring is enqueued by the browser, so the
   queue's holder must be writable by the UI, whose arc-journal mount is read-only
   (`cmd/cairn-ui/main.go:256-265`); two pods appending one file is the cross-host `flock` gap
   `journal.go:160-168` declares. No pod route, no corpus row.
2. **Presence and rings are EPHEMERAL, in memory in the UI process** (rows expire at
   `pushed_at + TTL`; a ring expires unclaimed after 60 s). A restart loses at most one push
   interval of presence and any pending ring; both self-heal. Not pgstore: nothing here is needed
   tomorrow, and a durable record of which hosts the operator uses is a liability with no reader.
   ⚠ **Assumes ONE UI replica** (Open question P2).
3. **The host side holds PRESENCE TOKENS, never a control-plane credential**, in two kinds —
   least privilege per process:
   - a **push token** bound to `(owner, set of host labels)`: may only REPLACE presence for those
     hosts (it lives with the extended push unit, which reports both hosts — Q3 below);
   - a **claim token** bound to `(owner, one host label)`: may only claim that host's rings (it
     lives with that host's ring service).

   Both are stored server-side as digests in one file (`-presence-tokens`, no default), are
   unknown to `identity.Backends`, and so authenticate NOTHING but the agent routes: every browser
   row, `POST /sign-in` and the pod answer them exactly as they answer garbage. **Revocation:**
   delete the row; the UI re-reads the token file on every agent request (a few rows — no cache
   to go stale), so the next request from that token is refused. Minting resolves a human-supplied
   owner (an email or token-file identity) to the stable `(Kind, ID)` ONCE and stores the id.
4. **The agent routes live on a SECOND LISTENER with its own ledger** (`-presence-agent-addr`, no
   default). The browser surface's gates are derived from the METHOD and may never be relaxed by
   a route class (AGENTS.md "TWO CROSS-SITE GATES"; `routes.go:28-39`); a non-browser POST carries
   no `Origin` and no cookie, so it cannot pass them and must not be made to. The agent listener
   accepts no cookies at all. **It applies `cairn-ui`'s reachable-bind / trusted-proxy refusal to
   ITS OWN bind address** (`cmd/cairn-ui/main.go:474-509`), so the failed-token lockout
   (`netid.RateLimiter`) keys on the real client rather than on the proxy — a separate bind is a
   separate reachability question and must not inherit the browser listener's verdict.
5. **The owner check is ONE predicate and every miss is one answer.** `presence.For(viewer,
   session)` returns the TARGET row only when the viewer is un-narrowed (decision 11), the row's
   owner equals `(viewer.Kind, viewer.ID)`, and the row is unexpired. Every surface — badge, page,
   ring enqueue — asks it and nothing else. Another owner's presence, expired presence and no
   presence render the SAME bytes and answer `POST /ring` the SAME way.
6. **Presence is keyed `(owner, host label, session)`; a push REPLACES each named host's whole
   set.** A closed pane disappears on the next push rather than at TTL. The host labels a push may
   name come from the TOKEN row, never the body.
7. **Two hosts presenting one session: the TARGET is the row with the newest `last_activity`;
   ties — including two empty values — go to the byte-wise smaller host label.** An empty
   `last_activity` sorts oldest. The badge shows the target row (and "also on host-b" when
   another live row exists); a ring is queued for the target's host at enqueue time.
8. **What a presence row carries, and what it never does.** Carried: `session`, `runtime`
   (`claude | opencode | other`), `host` (validated against the token), `target`
   (`<session>:<window>`, display only), `label`, `hotkey` (display or empty), `last_activity`
   (RFC 3339 from the ledger record), `pushed_at` (UI clock). **Never carried**, and refused as
   unknown fields: `pane_preview`, pane tty path, pane id, window id, tmux pid, cwd, repo path,
   pane contents, transcript text. The agent re-resolves the pane locally at ring time
   (decision 9), so the UI never needs them. Bounded: ≤ 256 rows per push, each string ≤ 128
   bytes, sessions validated by `write.SessionComponent`, `DisallowUnknownFields` (the
   `arcs.DecodePayload` rule, `arcs.go:168-184`).
9. **The pushed rows and the executor use ONE join, so every offered bell can resolve.** A row is
   pushed only if (i) its host's ledger read is `ok` — the sentinel carried the live server pid;
   (ii) the row has a ledger record (`row.ledger`, `session-manager:3251`) whose `tmux_pid` is
   non-empty — so the generation was CHECKED, not `generation_unchecked`; (iii) its session id is
   `ledger.session_id`, never the task-file fallback (`:3241-3242`); (iv) no other live record
   on that host carries the same session id. At ring time the executor re-runs exactly that
   predicate on its own host (re-read the ledger, require the live pid, require exactly one
   matching record), asks tmux for that pane's `#{pane_tty}`, confirms it is a character device
   under `/dev/pts/` owned by its uid, opens it `O_WRONLY|O_NOCTTY|O_NOFOLLOW` and writes the
   single byte `0x07`. It never calls `send-keys`, `paste-buffer`, `load-buffer`, any ioctl, or any
   tooling-repo module that does; it never opens the tty for reading. Outcomes (`rang`,
   `unresolved`, `ambiguous`, `not-a-tty`) go to the service's own log only (ruling D4) — nothing
   reports back to cairn. A stale row or a reused window id cannot aim a ring: the target is
   decided on the host, at ring time, by the stricter predicate.
10. **Arc "last updated" (O1) is computed in ONE whole-store walk, with the source named.**
    `reg_at` = the latest `registered_at` (pod clock, always present, `arcs.go:358`; `reported_at`
    is optional and on the tooling's clock, so it is shown in the tooltip, not used);
    `bullet_at` = the newest `touch.Session.LastDate` of any member over the caller's narrowed
    index, **clamped to today** and taken as that day's 00:00 UTC; `last_updated =
    max(reg_at, bullet_at)`. The row says which won — "registered 3h ago" or "bullet today" (day
    precision, `dateAgo`'s rule, `arcs.go:554-563` (#193)).
11. **Presence and rings FAIL CLOSED under credential narrowing.** `identity.Identity` gains a
    `Narrowed` bit: set on the bearer path when the presented credential has `NarrowedScopes`,
    and on the cookie path from the session record, which records at mint whether the credential
    it was minted from was narrowed. `presence.For` returns nothing for a narrowed identity, so a
    narrowed credential — or a session minted from one, whether or not the companion sign-in
    change has landed — sees no presence and cannot ring. If the companion change lands first and
    refuses narrowed sign-in, the session half of the bit is always false for new sessions; the
    S2 test pins the relation either way (it accepts a refused sign-in OR a session without
    presence, and fails only on presence shown).
12. **Live = `open` OR `last_updated` within 14 days; `unknown` is NOT `open`** (Q4 of the arcs
    plan). Newest first, ties by `(home, slug)`; `?all=1` shows every visible arc; the hidden count
    is printed.
13. **The arcs-first page is a NEW row, `GET /arcs`, linked from the nav — `/` is NOT replaced**
    (Open question P1). `/arc` gains `?tab=` ∈ {`""` scopes, `sessions`} over the data it already
    renders — no report change, no new walk on `/arc`.
14. **No new embedded asset, no script.** Tabs are server-rendered links; the bell is a plain
    `<form method="post">` with the CSRF field. `AllowedScriptSources`, the stylesheet rows and
    the `onlyGo` filter do not move.
15. **The personal-instance wall is made STRUCTURAL, not left to the manifest.** Today the only
    wall is that the client instance's manifest does not set `-presence-agent-addr` /
    `-presence-tokens` — a deployment convention, not code. Recommended: the agent listener
    refuses to start unless `-presence-owner <kind>:<id>` names exactly ONE owner, and refuses any
    token row for a different owner. Presence on a deployment then requires a deliberate,
    reviewable line naming the operator as that instance's sole presence owner; a copied manifest
    without it does not start the listener. (The earlier "agent refuses an origin matching a
    non-default instance file" wall is dropped, ruling D5.)

### Q3: S3 EXTENDS the existing push unit — and the two places that bends a design input

Extending is possible and is the plan. The unit already collects BOTH hosts once every 2 minutes;
a second `ExecStart=` in the same oneshot service runs a separate presence step that reads the SAME
collected document (no second scan, no second timer), applies decision 9's predicate, strips it to
decision 8's key set and POSTs it with the push token. It is a separate step, not an edit to the
dumb pipe, because that script's own rule is that it never reshapes its payload
(`tmux-snapshot-push.sh:15-22`). Its failure gets its own exit code so the existing alarm path
(`systemctl --user --failed`) distinguishes it; a failed snapshot leg stops it (oneshot
`ExecStart=` lines run in order), and presence then ages out at TTL — the honest outcome.

What it bends, stated rather than hidden:
- **"Pushed by a host agent on each operator machine" (O3) becomes "pushed for each machine by
  the one collecting host".** The remote host's rows arrive via the collector's ssh leg, which is
  pre-existing operator infrastructure, not a cairn connection. Hence the push token is bound to a
  SET of host labels (decision 3). Ring execution still runs ON each host (it must write a local
  pty), from that host's own claim service.
- **TTL ~3 min against a 2-minute tick with 1-minute default accuracy.** Ticks land 2–3 minutes
  apart (no `AccuracySec`, `home.nix:4519-4522`), so a 3-minute TTL tolerates zero missed pushes
  and can flicker. Recommended: set `AccuracySec = 5s` on the extended timer (harmless to the
  snapshot leg). If the operator prefers not to touch that timer, raise the TTL to 5 minutes.

**Ring claiming is NOT a timer**: a long-running user service per host (`Restart = "always"`,
a ~5 s claim loop, outbound only), the shape the tooling repo already uses for its always-on
monitors (`tooling:nix/home.nix:2747`). A oneshot timer at a 5-second period would spend its life
in unit start-up and, with default accuracy, would not run every 5 seconds anyway.

## Contract between cairn and the tooling repo (S2 ⇄ S3)

```
POST {agent-base}/agent/v1/presence           Authorization: Bearer <push token>
  body: {"schema": 1, "hosts": {
           "host-a": [{"session": "s-0001", "runtime": "claude", "target": "notes:3",
                       "label": "notes", "hotkey": "Alt+n",
                       "last_activity": "2000-01-02T03:04:05Z"}],
           "host-b": []}}
  — each named host's set is REPLACED; a host not named is untouched; a host the token does not
    cover → 400, nothing written
  200 X-Presence-Status: presence-replaced    body: "hosts=2 rows=1"
  400 malformed (unknown field, oversize, bad session, uncovered host)   401 uniform

POST {agent-base}/agent/v1/rings/claim        Authorization: Bearer <claim token>
  body: {}            (POST, not GET: claiming CHANGES state)
  200 {"schema": 1, "rings": [{"ring_id": "r-0001", "session": "s-0001"}]}
      — rings for THIS token's (owner, host) only; each returned exactly once
```

Two routes. There is no report route (ruling D4): outcomes are logged on the host.

## Threat model — presence and bell

| threat | control |
|---|---|
| **User B reads A's presence** (host labels, targets) | Decision 5's one predicate; B's pages byte-identical to no-presence pages (e2e (b)). Presence is never in a grant or a scope listing. |
| **User B rings A's pane** | `POST /ring` enqueues only when `presence.For(viewer, session)` is non-nil; otherwise the same answer as no presence (e2e (c)). The queue is keyed by owner; a claim returns only rings for the claiming token's `(owner, host)` — B's claim token for the same host label gets none of A's (e2e (d)). |
| **Cross-site ring** | Both existing gates by METHOD (`server.go:1143-1261`). No new class. |
| **Replay / flooding** | At most ONE pending ring per `(owner, session)` (a repeat while pending is a no-op with the same answer) and a 60 s TTL (ruling D3). Ring ids are random and claim-once. Failed agent tokens hit the `netid.RateLimiter` lockout, keyed on the real client (decision 4). |
| **A stolen push token** | Can replace presence rows for its owner on its host set — badges can be made to LIE for ≤ TTL. It cannot aim a ring (the executor decides the pane, decision 9), read anything, sign in, or enqueue. |
| **A stolen claim token** | Can claim — and so SUPPRESS — rings queued for its `(owner, host)`. Nothing else. |
| **Revocation** | Delete the digest row; the token file is re-read per agent request, so the next request is 401 (S2 test). |
| **A narrowed credential or a session minted from one** | Sees no presence and cannot ring (decision 11; e2e (f)). |
| **Stale presence** (pane closed, window renumbered, server restarted and `@N` reused) | Server: TTL and whole-host replace. Host: only generation-CHECKED, ledger-backed, unique rows are pushed, and the executor re-runs that predicate at ring time (decision 9). A stale row can show an old target for ≤ TTL; it can never aim a ring. |
| **Two hosts present one session** | Deterministic target (decision 7); e2e (e). |
| **What the bell can do** | Write ONE constant byte `0x07` to a pane tty the executor's uid owns. Writing the slave side is pane OUTPUT (verified above); the executor makes no ioctl; the ring payload carries no bytes, so there is nothing to inject even if the executor were wrong. |
| **Presence reaches another instance** | Server side: no listener unless `-presence-agent-addr`, `-presence-tokens` AND a single `-presence-owner` are set (decision 15). Host side: the push and claim steps are configured with ONE base URL each; they are not the cairn client and read none of its instance or route files. Both are configuration, and the recommendation in decision 15 is what turns the server half into a deliberate act. |
| **Writer keeps an arc live** | Accepted (O1's cost column); the clamp blocks future dates only. |
| **Presence makes a session page "found" for someone who could not see it** | Presence decorates a page `report.SessionAcross` already found; it never changes `Found()` (`sessionpage.go:106-113` (#193)). |

## Slices

| slice | repo | what | ledgers it moves | mergeable alone because |
|---|---|---|---|---|
| S0 | cairn | **The sign-in widening** (finding above) — owned by the companion change, not by this plan. Listed so it is not lost. | — | Independent. |
| S1 | cairn | **Arcs-first page + `/arc` tabs, no presence.** `GET /arcs` (decisions 10, 12, 13); `/arc?…&tab=` ∈ {scopes, sessions}. | `routes` + constants; `routes_test.go` hand ledger, `bareGETAnswer`, `contentAuthority`; `uiaudit/targets.go` (`/arcs` in `linkExpanded`) + `boot.go` fixtures (a recent, an open-old, a closed-old arc); `internal/ui/README.md`; `tests/control_mutants.py` rows (and the pinned count it forces into `ci.yml`). NOT `flake.nix`, NOT the corpus, NOT the `ok` floor (no new package). | Read-only over the journal and store that exist. Depends on #193 merging. |
| S2 | cairn | **Presence store + agent API.** New package `internal/presence` (stdlib-only: the owner predicate, target selection, TTL, whole-host replace, the ring queue, token-file parsing); `cmd/cairn-ui` flags `-presence-agent-addr`, `-presence-tokens`, `-presence-owner`, `-issue-presence-token`; the agent listener's own ledger + test; the reachable-bind refusal on its bind; `Identity.Narrowed` and the session record's narrowed-at-mint field. | **`ci.yml:831` `ok` floor `19` → `20`** for `internal/presence` (measure on the merged tree — it is a `<` floor and silent if forgotten); **`./internal/presence/` added to `tests/control_mutants.py` `PKGS`** (recommended — the owner predicate IS an authz seam), which `tests/test_control_mutant_count_is_pinned.py` then forces through `ci.yml`'s step name/comments and the README enumerations; `cmd/cairn-ui` flag tests; `internal/ui/README.md`; `depspolicy` unchanged (asserted). | Off by default; no browser change yet. |
| S3 | tooling repo | **Host side.** (i) A presence step as a second `ExecStart=` on the existing push unit, `AccuracySec = 5s` on its timer (Q3); (ii) a long-running per-host claim service (`Restart = "always"`, ~5 s loop) with the executor (decision 9). Tokens in 0600 files, never argv. | The tooling repo's own suite and nix module; no cairn ledger. | Inert until tokens are minted. |
| S4 | cairn | **Presence badges** — `host · target · hotkey · runtime · seen Ns ago` (+ "also on …") on the session page and session rows; a "live pane" badge on `/arcs` and `/arc` rows — all through the one predicate. | `internal/ui` render tests; uiaudit fixture (owner + non-owner); README. No row. | Read-only over S2. |
| S5 | cairn | **Bell.** `POST /ring` (browser ledger, class `0` — gates by method), queue semantics (O4/D3, decision 7), the button where S4 shows presence, a 303 back to the session page; `tests/presence/e2e.sh` with its `--self-test`, wired into the `go` job. | `routes` + `routes_test.go` (`POST /ring`); `tests/control_mutants.py`; `ci.yml` (the e2e step); README. | Needs S2–S4. |

Sizes are not estimated; nobody has measured these.

### Test plan per slice (negative controls named)

**S1.**
- Literal-expectation render tests: order by `last_updated`; the 14-day boundary measured at
  13 d 23 h and 14 d 1 h; `open` kept at 400 days; `unknown` at 20 days hidden; `?all=1` shows it;
  the hidden count printed.
- **Negative controls:** an arc homed in an unreadable scope is absent from `/arcs` even when its
  members wrote in readable scopes; a member's bullet in an unreadable scope does NOT move
  `last_updated` (two viewers, one store — a relationship); a future-dated bullet does not sort
  above today. Positive control for the attribution cost: a bullet naming a member session,
  written by a non-member, DOES move it (pins O1's accepted cost so nobody "fixes" it silently).
- `/arc` tabs: unknown `tab` → default tab; every miss stays `ArcUnregisteredBody`, same bytes on
  every tab.
- Mutants (each killed by a NAMED test): drop the clamp; use `reported_at`; count `unknown` as
  open; skip the visibility check in the member walk.
- Cost: extend `BenchmarkSessionPageAndScopeTabs` with `/arcs` at both sizes; claim "≤ the session
  page", stated as a ratio.

**S2.**
- Owner predicate as a relationship: owner A sees the row; B, a project principal with the same
  `ID` string but a different `Kind`, A after TTL, and A narrowed (bearer AND session-minted) see
  nothing — each shown RED by deleting one clause.
- Target selection: two hosts, newest `last_activity` wins; equal and both-empty → smaller label.
- Token isolation (decision 3): push and claim tokens presented to every browser GET row, to
  `POST /sign-in` and to the pod are refused exactly as a random token is (byte-compare); a real
  control credential IS accepted on the same request (positive control). A push token cannot
  claim; a claim token cannot push.
- Claim isolation: B's claim token for `host-a` and A's `host-b` claim token get none of A's
  `host-a` rings. **Revocation:** remove A's row from the token file → A's next request is 401
  with no restart.
- Whole-host replace: push `{host-a: [s1, s2]}` then `{host-a: [s2]}` → s1 gone; a push naming
  only `host-b` leaves `host-a`'s rows. A push naming a host outside the token's set → 400,
  nothing written.
- Body bounds and unknown-field refusal (each never-carried field, `pane_preview` first), each
  with a just-under-the-bound positive control.
- Startup: any of `-presence-agent-addr`, `-presence-tokens`, `-presence-owner` without the
  others refuses; blank values refuse; a token row for another owner refuses; none set → no
  listener (connection refused). A reachable agent bind with no trusted-proxy allowlist refuses
  (mirroring `main.go:474-509`).

**S3 (tooling repo).**
- **Push body:** from a fixture `session-manager` document, the pushed JSON's key set is EXACTLY
  decision 8's (asserted as a set — grows or shrinks → red), `pane_preview` absent; rows dropped
  for: a host whose ledger read is not `ok`; a row with no ledger record; a record with no
  `tmux_pid`; an id from the task-file fallback; two live records with one id.
- **Executor** against a private tmux server (`tmux -L <literal test socket>`): ring → bell flag on
  the target window only; ambiguous / unresolved / generation-unchecked / not-a-tty → nothing
  written, outcome logged.
- **No input:** the target pane runs `cat > <file>`; after a ring the file is empty and
  `capture-pane` is unchanged. Structural: the executor's import graph is an asserted ledger with
  no module reaching `send-keys`/`paste-buffer`, failing on GROW.
- Token never in argv (assert spawned command lines); the claim service exits non-zero on a 401
  so `Restart=always` plus the failed-unit list surface a revoked token.
- Missing `tmux` → exit 2, never skip.

**S4.** Owner and non-owner render the same session page; the non-owner's bytes equal the
no-presence bytes; a narrowed owner's bytes equal them too. Mutant: render without the predicate →
red.

**S5.** `POST /ring` without `Origin`, with a foreign one, without CSRF → the existing refusals
(asserted so no future class bypasses them). A ring while one is pending → no second ring, same
answer. TTL: unclaimed ring gone at 61 s; claimed at 59 s exactly once. The e2e and every clause's
sabotage under `--self-test`.

## Open questions (each with a recommendation)

- **P1. Should `/` become the arcs page?** Recommend NO for S1: ship `GET /arcs` plus a nav link,
  flip `/` later if wanted — the root's search and tag filter have their own consumers.
- **P2. More than one UI replica.** In-memory state splits across replicas. Recommend one replica
  (it is a personal instance), stated in the flag help; if that changes, move presence and rings
  into `internal/pgstore` behind the same `internal/presence` interface.
- **P3. Mint presence tokens from the CLI or the browser?** Recommend CLI mint on `cairn-ui` for
  S2 (owner resolved once, from an email or token-file identity); a browser self-mint page would
  put a bearer secret in HTML and need a writable token store.
- **P4. Make the bell audible for background windows?** Under `bell-action current` a ring sets the
  flag but plays no sound (verified). Recommend relying on the flag's styling for v1; the audible
  alternative is `bell-action any`, a global behaviour change for the operator to choose.
- **P5. Presence for a session the owner cannot see any writes of.** Recommend NOT changing the
  session page's uniform 404 (`sessionpage.go:106-113` (#193)); a "my live sessions" list is a
  separate page if wanted.
- **P6. The other host's tmux.** Every bell measurement is one host, one tmux version, a private
  server. Recommend the S3 executor test runs on both hosts before S5 merges.
- **P7. The single-owner wall (decision 15).** It makes presence one-owner per deployment.
  Recommend accepting that for a personal instance; a second owner would be a deliberate change to
  that flag, not a configuration slip.

## What this plan could not measure

- Whether the deployed personal UI runs with a control journal (the manifests are private);
  decision 1's owner-key argument requires it there.
- The second host's scan and tmux bell behaviour; any opencode-heavy moment (the one scan held 0
  opencode rows).
- Whether the terminal raises an urgency hint on a forwarded bell (config only).
- `/arcs` cost on the real store (synthetic benchmark only, non-pinned toolchain).
- Which tooling-repo test tier has `tmux`.
- #193's final shape: every `(#193)` citation must be re-read after it merges.
