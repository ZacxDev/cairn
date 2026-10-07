# Plan: an arcs-first browser, and where a live session is running

This is a DESIGN, not a measurement of anything built. Nothing below exists yet. Every claim
about today's behaviour was read off the code at `origin/main` `64475d7` (cairn) or the operator
tooling repo (read-only; its paths are prefixed `tooling:`), and carries a `file:line` so it can
be re-checked. Claims about the browser surface's scope tabs and session page were read off
**PR #193's branch (`zach/ui-scope-tabs-sessions` at `b1d82ad`), which is OPEN and NOT merged** —
they are marked `(#193)` and must be re-read once it lands. Every number about real data names the
instrument that produced it and what that instrument cannot see. Examples are synthetic: hosts
`host-a`/`host-b`, tmux target `notes:3`, session ids `s-0001`, year-2000 dates.

**Revision 2** applied the PR's round-0 and round-1 audit rulings: the `/arc` entries tab, the
ring report route, the ring cooldown/cap and the client-side instance wall are gone; rings are
claimed by a long-running service; two hosts presenting one session have a deterministic target;
presence fails closed under credential narrowing; and the closing condition names one runnable
check per repo. **Revision 3** applies round 2: extending the existing push unit was measured
impossible, so presence gets its OWN unit on EACH host, scanning local windows only, with a token
bound to ONE host label; the single-owner wall and the queue's owner filter each get their own
reachable control; narrowing is closed without a stored session bit (revision 4: the companion
sign-in change is a hard prerequisite, so S2 needs no schema change); the CI `ok` floor is stated as a
measurement; and decision 8 lists the carried fields per location.

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
     (d) A's `host-a` claim token claims the ring exactly once; A's `host-b` claim token and a
     just-revoked claim token each claim nothing; and — the control for decision 15's
     SINGLE-OWNER WALL, not for the queue — a token row for owner B in the token file refuses the
     listener at startup, while the same row added to the file AFTER startup is refused as that
     ROW only (logged; A's rows keep working) and B's token then gets 401. The queue's own
     owner-keying cannot be reached end to end once the wall exists (no second owner can
     authenticate), so its control is the S2 UNIT test that constructs the queue directly with two
     owners;
     (e) with `s-0001` presented by both `host-a` and `host-b`, the ring goes to the host whose row
     has the newest `last_activity`, and on a tie to the byte-wise smaller host label;
     (f) narrowing, in two halves: a narrowed BEARER credential's session page shows no presence
     (the per-request derived bit, decision 11); and — the END-TO-END REPEAT of S2's Go
     prerequisite test, observed from outside the process — a narrowed credential presented to
     `POST /sign-in` is refused, the response sets no session cookie, and a follow-up request
     carrying any cookie from that response is unauthenticated.
     `--self-test` sabotages EVERY clause (a)–(f) in turn and must report each caught (exit 2 if
     any sabotage is not caught). Each sabotage is a mutant of the code path that clause pins, on
     a scratch copy of the tree (the `tests/control_mutants.py` pattern), chosen so the clause's
     OWN assertion is the one that fails: (a) drop the 14-day filter; (b) render presence without
     the owner predicate; (c) enqueue without the predicate; (d) skip the per-row owner check in
     the token-file reader (B's added row then authenticates); (e) pick the OLDEST
     `last_activity`; (f) two sabotages, one per half — ignore the bearer `Narrowed` bit, and
     revert the sign-in refusal of narrowed credentials (on a tree without the companion change,
     (f)'s second half is already red). A sabotage that leaves its clause green is a
     clause that cannot fail, and the script reports it as such.
  2. **tooling repo: the S3 executor test**, in that repo's own suite (which tier has `tmux` is
     not measured here — the test must exit 2 rather than skip where it is absent, so a tier
     without it goes red instead of vacuously green). Against a PRIVATE tmux server
     (`tmux -L <literal test socket>`, never the default), a claimed ring sets `window_bell_flag=1`
     on the target window and on no other, and the pane's captured contents and its program's
     stdin are unchanged; its own `--self-test` sabotages the target resolution and the no-input
     property and shows each go red.

Post-close rollout (NOT part of the closing condition): the personal instance runs with presence
enabled, each operator host runs its own S3 presence unit and its own ring-claim service, and a
click on the deployed session page's bell
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
  non-blocking (`tooling:scripts/lib/handoff_doc.py`: `_register_arc` defined at `:8832`, called
  at `:8803` and `:8828`). So a
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
moving it is silently absorbed. **It is already stale on `main`:** `go test ./...` on this tree
(`64475d7` plus this doc; local go 1.26) prints **23** `^ok` lines against the floor's 19, so four
packages can vanish today with the gate green. That is a pre-existing defect, being handled outside
this plan; this plan only requires that whoever adds `internal/presence` sets the floor to the
`ok` count MEASURED on the merged tree (24 if nothing else moved — re-measure). And second, `tests/control_mutants.py`'s `PKGS` (`:88`) is pinned by
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
  sign-in refuse narrowed credentials. **S2 hard-depends on that change's BEHAVIOUR** — a narrowed
  credential's sign-in is refused and no session record is created — pinned by S2's own Go
  prerequisite test (decision 11); it does not depend on how the change implements it.

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
  80 generation-mismatch, 2 conflicts. **Re-measured for revision 3, three runs, same host:**
  `scan --host <this host's own label> --no-ch --json` took 98–115 ms wall (exit 0 each), one
  host in the document, 73 windows, 62 with a session id — and all 62 carry a ledger record with a
  non-empty `tmux_pid`, so decision 9's filter would have dropped none at that moment. Not
  measured: the second host, an opencode-heavy moment.
- **The local host's label is resolved, never guessed:** `local_host_label` delegates to a shared
  rule and RAISES rather than defaulting, because `hostname` is the same on both machines
  (`session-manager:3773-3790`). The per-host presence unit uses it, so a host can never push under
  the other's label.
- **Opencode ids.** The tooling repo treats the id as opaque (`session-manager:3250`); the trailer
  grammar's `sessionClass` is `[A-Za-z0-9][A-Za-z0-9_.-]{0,63}` (`internal/write/revision.go:71`),
  which admits both a uuid and a `ses_`-prefixed id (`bullet_request.go:24-26`). Presence still
  carries `runtime`: one pane can hold both runtimes in sequence (the conflict above).

### The existing host→server push unit (what S3 models on, and does NOT extend)

- `tooling:scripts/tmux-snapshot-push.sh` is a DELIBERATE DUMB PIPE: it posts `session-manager
  --json --pane-preview` VERBATIM (`:15-22, :157`) and says not to reshape the payload there.
- **Its collected document does not outlive the run:** it is written into a per-run `mktemp -d`
  directory that `trap cleanup EXIT INT TERM` deletes (`:109-113`), so a second `ExecStart=` could
  never read it.
- It runs on ONE host only and covers both: the collector ssh'es to the other host, so the unit
  is gated to one host as a correctness requirement — two hosts each pushing a two-host document
  would fight over every row (`tooling:nix/home.nix:270-274`, comment `:4524-4527`).
- `Type = "oneshot"`, `TimeoutStartSec = 150`, distinct non-zero exit codes as its only alarm,
  deliberately no `OnFailure` (`home.nix:4318-4356`; codes at `tmux-snapshot-push.sh:28-38`);
  timer `OnStartupSec = 1min`, `OnUnitActiveSec = 2min`, **no `AccuracySec`** (`home.nix:4519-4522`)
  — so systemd's default 1-minute accuracy applies and ticks land 2–3 minutes apart.
- Token from a 0600 file, sent from a curl config file, never argv
  (`tmux-snapshot-push.sh:55, 95, 115, 252-263`) — the one part S3 copies.

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
   - a **push token** bound to `(owner, ONE host label)`: may only REPLACE that host's presence (it
     lives with that host's presence unit — S3);
   - a **claim token** bound to `(owner, ONE host label)`: may only claim that host's rings (it
     lives with that host's ring-claim service).

   Both kinds sit on the same host under the same uid, so the split does not protect one from a
   compromise of the other; what it buys is that each PROCESS holds only the capability it uses —
   a leaked environment of the push unit cannot suppress rings, and the claim service cannot make
   badges lie.

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
6. **Presence is keyed `(owner, host label, session)`; a push REPLACES its host's whole set.** A
   closed pane disappears on the next push rather than at TTL. The host comes from the TOKEN row;
   the body's `host` must equal it (a mismatch is a 400, which catches a token file copied to the
   wrong machine), and a push can never touch another host's rows.
7. **Two hosts presenting one session: the TARGET is the row with the newest `last_activity`;
   ties — including two empty values — go to the byte-wise smaller host label.** An empty
   `last_activity` sorts oldest. The badge shows the target row (and "also on host-b" when
   another live row exists); a ring is queued for the target's host at enqueue time.
8. **What presence carries, PER LOCATION, and what it never does.**
   - **On the wire** (the contract below), exactly: top level `schema`, `host`, `rows`; per row
     `session`, `runtime` (`claude | opencode | other`), `target` (`<session>:<window>`, display
     only), `label`, `hotkey` (display or empty), `last_activity` (RFC 3339 from the ledger
     record, or empty). Nothing else — S3's key-set test asserts these two sets.
   - **Stored in the UI** (never sent by the host): the wire row, plus `owner` and `host` (both
     from the token row), `pushed_at` (UI clock) and the expiry derived from it.
   - **Never carried, anywhere**, and refused as unknown fields: `pane_preview`, pane tty path,
     pane id, window id, tmux pid, cwd, repo path, pane contents, transcript text. The agent re-resolves the pane locally at ring time
   (decision 9), so the UI never needs them. Bounded: ≤ 256 rows per push, each string ≤ 128
   bytes, sessions validated by `write.SessionComponent`, `DisallowUnknownFields` (the
   `arcs.DecodePayload` rule, `arcs.go:168-184`).
9. **The pushed rows and the executor use ONE join, so every offered bell can resolve.** A row is
   pushed only if (i) it is a LOCAL row — the scan is `--host <this host's own label>`, so there
   is no ssh leg and no other host's ledger — and the local ledger read is `ok`, i.e. the sentinel
   carried the live server pid;
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
11. **Presence and rings FAIL CLOSED under credential narrowing — by making a narrowed SESSION
    impossible rather than by storing a bit on every session.**
    - **Sessions carry NO narrowing bit.** Every session-minting door is accounted for. `Narrow`
      has exactly one call site, `control.Authenticate`, the bearer-credential path
      (`internal/control/resolve.go:314`). Both provider doors open a session from a provider
      principal resolved with plain `control.Resolve`, no narrowing (`internal/ui/oauth.go:700,
      749`; `internal/identity/supabase.go:254-266`). The one door that can mint a session from a
      narrowed credential is `POST /sign-in` (`internal/ui/session.go:221, 236`), and the
      **companion change makes it REFUSE a narrowed credential**. After that change, every newly
      minted session is un-narrowed by construction.
    - **Therefore the companion change is a HARD PREREQUISITE of S2**: merged AND deployed before
      S2 deploys. S2 does not trust that it happened; it proves it with a **Go test S2 owns** (the
      "Prerequisite control" under S2's test plan): a narrowed credential presented to
      `POST /sign-in` is refused, and the session store — read in-process by the test — holds no
      new record. On a tree without the companion change that test is red, so S2 cannot merge
      without it silently. (`tests/presence/e2e.sh` does not exist until S5; its clause (f) is
      S5's END-TO-END REPEAT of this guard, observed from outside the process, not the guard
      itself.)
    - **Sessions minted BEFORE the companion change deployed** are the residual case, and it is
      closed by a measured fact plus a deploy precondition. Measured: the control journals on both
      deployed instances held zero narrowed credentials (37 and 19 records, every
      `narrowed_scopes` null), and a session lives `DefaultSessionTTL = 12 * time.Hour`
      (`internal/identity/session.go:145`) **unless the deployment overrides it** with
      `-session-ttl` / `CAIRN_UI_SESSION_TTL` (`cmd/cairn-ui/main.go:219`). **S2 deploy
      precondition:** S2 deploys no sooner than the target instance's EFFECTIVE session TTL (12 h
      by default — read the manifest) after the companion change's deploy, AND the deployer
      re-checks that the instance's journal holds no credential with non-null `narrowed_scopes`
      minted before that deploy. The journal check covers the case regardless of the TTL: with no
      narrowed credential ever issued, no session can have been minted from one. Together these
      leave no narrowed session alive when presence switches on.
    - **The BEARER path: derived, not carried — and the shared signature is NOT widened.**
      `control.Authenticate` returns `(Principal, Authorization, error)` (`resolve.go:287`), `Narrow`
      returns a plain `Authorization` (`resolve.go:399-419`), and `identity.TokenAuthority` has the
      same signature (`internal/identity/machinetoken.go:28-30`, called at `:78`) and is shared
      with the pod (`internal/api/server.go:357`, `internal/control/cache.go:249`). Widening it
      would move the pod for a UI-only feature. Instead the presence predicate derives the bit
      UI-side from the model it already holds. Authentication stamps the matched credential's id
      on the principal (`p.CredentialID = matched.ID`, `resolve.go:313`), and the model keys
      credentials by that id (`Credentials map[ID]Credential`, `internal/control/model.go:320`;
      the row carries `NarrowedScopes`, `:285`). `cairn-ui`'s authority is a `control.Cache`
      whose `Model` method is already handed to other UI code (`cmd/cairn-ui/main.go:308, 342`;
      `cache.go:239`). So: `narrowed = CredentialID != "" && (row missing ||
      row.NarrowedScopes != nil)` — a row that vanished between authentication and the check
      reads as narrowed, i.e. fail closed. A cookie identity has an empty `CredentialID`
      (`resolve.go:22-25`) and is un-narrowed by the prerequisite above. Computed per request,
      never stored; `presence.For` returns nothing for a narrowed identity, so a narrowed bearer
      credential sees no presence and cannot ring. **Ledger impact:** none in
      `internal/identity`, `internal/control` or `internal/api` — no `Identity` field, no signature
      change, the pod is untouched; the derivation lives in `internal/presence`, which is already
      in `tests/control_mutants.py`'s `PKGS` (S2).
    - **What this buys: S2 is genuinely rollback-safe.** No session-store field, no pgstore
      migration, no schema version — rolling the UI image back is an ordinary rollback.
      (`internal/pgstore/migrate.go:166-173` refuses an unknown schema version, so a migration
      would have made the rollback an outage even with presence off.)
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
    token row for a different owner — at startup by refusing to start; for a row that appears in
    the re-read token file AFTER startup, by refusing THAT ROW only (logged, naming the row's
    digest prefix, never the token), so one bad edit cannot take the owner's own hosts offline.
    Presence on a deployment then requires a deliberate,
    reviewable line naming the operator as that instance's sole presence owner; a copied manifest
    without it does not start the listener. (The earlier "agent refuses an origin matching a
    non-default instance file" wall is dropped, ruling D5.)

### Q3: S3 does NOT extend the existing push unit — measured impossible; each host gets its own

Revision 2 planned a second `ExecStart=` on the existing push unit reading the same collected
document. Measured against that script, it cannot work, and the review ruling reversed it:

- the document lives in a per-run `mktemp -d` directory that `trap cleanup EXIT` deletes before any
  later `ExecStart=` runs (`tooling:scripts/tmux-snapshot-push.sh:109-113`);
- it would couple presence to an unrelated push destination — a failed snapshot POST (exits 4/5,
  `:28-38`) would stop the presence step — and to that unit's shared `TimeoutStartSec = 150`
  budget, most of which is the ssh collector's (`home.nix:4318-4356`);
- that unit runs on one host and collects the other over ssh, so it would also have forced a
  token bound to a SET of host labels and bent O3's "a host-side agent on each operator machine".

**So S3 is a NEW pair of user units on EACH host, sharing nothing with the snapshot unit:**

- **presence push** — a oneshot service + timer per host: `session-manager scan --host <this
  host's own label, from local_host_label> --no-ch --json` (LOCAL windows only, no ssh; measured
  98–115 ms on one host), decision 9's filter, decision 8's wire body, POST with that host's push
  token (bound to ONE host label). Timer `OnStartupSec = 30s` + `OnUnitActiveSec = 60s` +
  `AccuracySec = 1s`. `OnUnitActiveSec` alone has NO first trigger — it counts from the service's
  last activation, which never happens — so the timer would never fire; `OnStartupSec` supplies
  the first elapse, mirroring the existing timer's pair (`home.nix:4520-4521`). Without
  `AccuracySec` systemd's default 1-minute accuracy would stretch a 60 s period towards 2 minutes,
  and the existing timer shows that default is what this repo's timers get (`home.nix:4519-4522`).
  At 60 s ± 1 s a ~3-minute TTL tolerates two missed pushes.
- **ring claim** — a long-running user service per host (below).

This restores O3 as the operator chose it: each machine reports itself, with its own token.

**Ring claiming is NOT a timer**: a long-running user service per host (`Restart = "always"`,
a ~5 s claim loop, outbound only), the shape the tooling repo already uses for its always-on
monitors (`tooling:nix/home.nix:2747`). A oneshot timer at a 5-second period would spend its life
in unit start-up and, with default accuracy, would not run every 5 seconds anyway.

## Contract between cairn and the tooling repo (S2 ⇄ S3)

```
POST {agent-base}/agent/v1/presence           Authorization: Bearer <push token>
  body: {"schema": 1, "host": "host-a",
         "rows": [{"session": "s-0001", "runtime": "claude", "target": "notes:3",
                   "label": "notes", "hotkey": "Alt+n",
                   "last_activity": "2000-01-02T03:04:05Z"}]}
  — that host's whole set is REPLACED (an empty `rows` clears it); `host` must equal the token's
    host label, else 400 and nothing written; no other host is ever touched
  200 X-Presence-Status: presence-replaced    body: "rows=1"
  400 malformed (unknown field, oversize, bad session, host ≠ token's host)   401 uniform

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
| **User B rings A's pane** | `POST /ring` enqueues only when `presence.For(viewer, session)` is non-nil; otherwise the same answer as no presence (e2e (c)). Two independent guards, each with its own control: the single-owner wall refuses any token row for B, so B cannot authenticate to the agent listener at all (e2e (d)); and behind it the queue is keyed by owner, so a claim returns only rings for the claiming token's `(owner, host)` — controlled by an S2 UNIT test that builds the queue with two owners, because the wall makes that filter unreachable end to end. |
| **Cross-site ring** | Both existing gates by METHOD (`server.go:1143-1261`). No new class. |
| **Replay / flooding** | At most ONE pending ring per `(owner, session)` (a repeat while pending is a no-op with the same answer) and a 60 s TTL (ruling D3). Ring ids are random and claim-once. Failed agent tokens hit the `netid.RateLimiter` lockout, keyed on the real client (decision 4). |
| **A stolen push token** | Can replace presence rows for its owner on its ONE host — badges can be made to LIE for ≤ TTL. It cannot aim a ring (the executor decides the pane, decision 9), read anything, sign in, or enqueue. |
| **A stolen claim token** | Can claim — and so SUPPRESS — rings queued for its `(owner, host)`. Nothing else. |
| **Revocation** | Delete the digest row; the token file is re-read per agent request, so the next request is 401 (S2 test). A row for another owner appearing in that re-read is refused as a row, logged (decision 15). |
| **A narrowed credential or a session minted from one** | A narrowed bearer credential sees no presence and cannot ring (bit derived per request from the credential row, decision 11; e2e (f)). A session cannot be minted from one once the companion change — S2's hard prerequisite, pinned by S2's Go prerequisite test and repeated end to end by S5's e2e (f) — is deployed. Sessions minted before it are closed by the deploy precondition: zero narrowed credentials measured on either journal (the check that holds whatever the TTL), and S2 deploying no sooner than the instance's effective session TTL later (12 h by default; `-session-ttl` / `CAIRN_UI_SESSION_TTL` can change it). |
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
| S2 | cairn | **Presence store + agent API.** New package `internal/presence` (stdlib-only: the owner predicate, target selection, TTL, whole-host replace, the ring queue, token-file parsing); `cmd/cairn-ui` flags `-presence-agent-addr`, `-presence-tokens`, `-presence-owner`, `-issue-presence-token`; the agent listener's own ledger + test; the reachable-bind refusal on its bind; the bearer narrowing bit, DERIVED in `internal/presence` from `control.Cache.Model()`'s credential row keyed by `Principal.CredentialID` (missing row ⇒ narrowed) — NOT a new `identity.Identity` field and NOT a wider `TokenAuthority` signature, so `internal/identity`, `internal/control`, `internal/api` and the pod do not move (decision 11). **HARD PREREQUISITE: the companion sign-in change merged and deployed, pinned by S2's Go prerequisite test; S2 deploys no sooner than the instance's effective session TTL after it (12 h by default, `cmd/cairn-ui/main.go:219`), with no pre-existing narrowed credential on the target journal (decision 11's deploy precondition).** No session-store or pgstore schema change. | **`ci.yml:831` `ok` floor set to the `ok` count MEASURED on the merged tree** — 24 if nothing else moved (23 measured on `64475d7` + `internal/presence`); it is a `<` floor and silent if forgotten, and it is already 4 stale on `main` (a separate defect, handled outside this plan); **`./internal/presence/` added to `tests/control_mutants.py` `PKGS`** (recommended — the owner predicate IS an authz seam), which `tests/test_control_mutant_count_is_pinned.py` then forces through `ci.yml`'s step name/comments and the README enumerations; `cmd/cairn-ui` flag tests; `internal/ui/README.md`; `depspolicy` unchanged (asserted). | No browser change yet, and rollback-safe: it adds no schema version, so rolling the UI image back is an ordinary rollback. |
| S3 | tooling repo | **Host side, on EACH host, sharing nothing with the existing snapshot unit (Q3).** (i) A presence push: oneshot service + timer (`OnStartupSec = 30s` for the first elapse, `OnUnitActiveSec = 60s`, `AccuracySec = 1s`), local scan via `local_host_label`, decision 9's filter, decision 8's wire body, that host's push token; (ii) a long-running ring-claim service (`Restart = "always"`, ~5 s loop) with the executor (decision 9) and that host's claim token. Tokens in 0600 files, never argv. | The tooling repo's own suite and nix module; no cairn ledger. | Inert until tokens are minted. |
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
  `ID` string but a different `Kind`, A after TTL, and A through a narrowed BEARER credential see
  nothing — each shown RED by deleting one clause.
- **Prerequisite control (owned by S2):** a narrowed credential presented to `POST /sign-in` is
  refused and the session store holds no new record — red on any tree lacking the companion
  change, so S2 cannot merge ahead of it. And a provider (OAuth) sign-in yields an un-narrowed
  session: the positive control that the refusal is specific to narrowing, not to sign-in.
- **Derived bearer bit:** over one model, an un-narrowed bearer credential of A sees A's presence
  (positive control); a narrowed credential of A sees none; a principal whose `CredentialID`
  names a row absent from the model sees none (fail closed). Shown RED by reading the bit from
  `Authorization` emptiness instead of the credential row, and by treating a missing row as
  un-narrowed.
- Target selection: two hosts, newest `last_activity` wins; equal and both-empty → smaller label.
- Token isolation (decision 3): push and claim tokens presented to every browser GET row, to
  `POST /sign-in` and to the pod are refused exactly as a random token is (byte-compare); a real
  control credential IS accepted on the same request (positive control). A push token cannot
  claim; a claim token cannot push.
- **Queue owner-keying, at UNIT level** (the wall makes it unreachable end to end): construct
  the queue directly with rings for owners A and B on the same host label; a claim as `(B,
  host-a)` returns only B's, `(A, host-a)` only A's. Shown RED by dropping the owner from the
  claim filter. A's `host-b` claim token gets none of A's `host-a` rings (end to end).
- **The wall's own control:** a token row for B present at startup → the listener refuses to
  start; the same row appended to the file after startup → that row is refused and logged, A's
  push and claim still succeed, B's token gets 401. Shown RED by skipping the per-row owner check.
- **Revocation:** remove A's row from the token file → A's next request is 401 with no restart.
- Whole-host replace: push `host-a` with `[s1, s2]` then `[s2]` → s1 gone; `host-b`'s rows are
  untouched by either. A push whose `host` differs from the token's → 400, nothing written.
- Body bounds and unknown-field refusal (each never-carried field, `pane_preview` first), each
  with a just-under-the-bound positive control.
- Startup: any of `-presence-agent-addr`, `-presence-tokens`, `-presence-owner` without the
  others refuses; blank values refuse; a token row for another owner refuses; none set → no
  listener (connection refused). A reachable agent bind with no trusted-proxy allowlist refuses
  (mirroring `main.go:474-509`).

**S3 (tooling repo).**
- **Push body:** from a fixture `session-manager` document, the pushed JSON's top-level key set
  is EXACTLY `{schema, host, rows}` and every row's is EXACTLY `{session, runtime, target, label,
  hotkey, last_activity}` — the CONTRACT's wire sets (decision 8), asserted as sets so growth or
  shrinkage is red; `pane_preview` absent; `host` equals `local_host_label()`. Rows dropped for: a
  ledger read that is not `ok`; a row with no ledger record; a record with no `tmux_pid`; an id
  from the task-file fallback; two live records with one id. A fixture carrying a second host's
  rows pushes none of them.
- **Host label:** when `local_host_label` raises, the push exits non-zero and sends nothing.
- **Timer actually fires:** a nix-evaluation test that the timer declares a first trigger
  (`OnStartupSec` or `OnActiveSec`) beside `OnUnitActiveSec` — `OnUnitActiveSec` alone never
  elapses. Plus a **deploy-time check** (it needs a live user manager, so it cannot run in CI):
  after install, `systemctl --user list-timers` on each host lists the presence timer with a
  non-empty NEXT, and within ~1 min the service has run once.
- **Executor** against a private tmux server (`tmux -L <literal test socket>`): ring → bell flag on
  the target window only; ambiguous / unresolved / generation-unchecked / not-a-tty → nothing
  written, outcome logged.
- **No input:** the target pane runs `cat > <file>`; after a ring the file is empty and
  `capture-pane` is unchanged. Structural: the executor's import graph is an asserted ledger with
  no module reaching `send-keys`/`paste-buffer`, failing on GROW.
- Token never in argv (assert spawned command lines). On a 401 the claim service exits with a
  code listed in `RestartPreventExitStatus=` — `Restart=always` alone would restart it forever and
  it would never appear in the failed-unit list — so a revoked token surfaces as a failed unit.
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
