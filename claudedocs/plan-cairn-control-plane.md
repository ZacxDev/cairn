# Plan: cairn grows a control plane — 2026-09-13

The next phase turns cairn from *a store with a static allowlist* into *a multi-user
product with a UI*. This doc records the decisions that were taken deliberately, what
each one costs, and the order that makes the risky part safe. It is a plan, not a
measurement: nothing here had been built or verified WHEN IT WAS WRITTEN, and every claim
about today's behaviour was read off the code at `3c316ff` and is cited so it can be
re-checked.

⚠ **THAT SENTENCE IS NO LONGER TRUE OF THE WHOLE DOCUMENT, AND LEAVING IT UNQUALIFIED WOULD
MAKE THIS PLAN READ AS A GREENFIELD ONE.** P0, P1 and P2 have shipped; `AGENTS.md` is the
authority on what is built, what is measured, and what the measurements do NOT cover — in
particular that the SERVER dual-run (P1's last step) is still outstanding even though P2's
client parity gate is green. Read status there, not here. Everything below is the DECISION
record and is unchanged.

## What exists today, measured at `3c316ff`

- **Authorization is static config.** A token file, one row per line: `<token>` (legacy —
  identity `legacy`, UNRESTRICTED, and forbidden to write) or
  `<token> <identity> <scope>,<scope>`. It resolves to one `TokenRecord{token, identity,
  scopes}` — *one* object on purpose, so "which token is this" and "what may it see"
  cannot be answered from two structures that disagree (`server.py:771`).
- **`scopes is None` is unrestricted and reachable only from a legacy row; `()` is its
  opposite.** The per-request default is `()`, so a route that forgets to set the field
  sees nothing rather than everything (`server.py:777`).
- **One predicate narrows every read channel**: `rc.visible_scope_set` is used by
  `load_index` (what is OPENED), `load_store` (the RESULT SHAPE) and `_snapshot`'s
  candidate list (`server.py:4131`). A refused scope answers exactly what a never-existed
  scope answers, so an error cannot enumerate the store.
- **Writes derive the actor from the token identity**, and a body-supplied `actor` key is
  accepted and discarded as hostile input. A legacy bare token may not write at all.
- **The pod and the client run the SAME renderer.** `server.py` imports
  `subsystem_recall` and returns `render_text`/`render_search` verbatim; the client runs
  that unmodified reader against its local replica. This is what makes byte-identity
  between pod output and local output expressible at all, and what
  `verify-byte-identity.sh` is built on.
- **There are no users, projects, groups, ACL objects or database.** The store is a flat
  filesystem, `<scope>/<entry>.md` at depth 2, and **a scope's identity is its directory
  name.**

🔴 **The shape of the whole phase follows from one fact:** authorization today is a
hand-edited secret plus `SIGHUP`. Invite a user, share a scope, move a scope under a
project — every one of those is *mutable, self-service, runtime* state. The UI is the
easy half. The control plane is the work.

## Decisions taken

| # | decision | cost accepted |
|---|---|---|
| 1 | **Authz state in Postgres (Supabase), materialized to a pod-side cache.** The hot path makes no network call. | A revocation is not effective until the cache refreshes — see D. |
| 2 | **Server AND client rewritten in Go.** One render package, shared by pod, CLI and UI. | The largest item here by far. Mitigated by 6, not by optimism. |
| 3 | **Identity is an interface with two backends from day one**: Supabase JWT (GitHub/Google) and trusted-header / forward-auth. | A second backend to test. Cheaper than retrofitting an abstraction around a vendor. |
| 4 | **A scope has an immutable ID; name and project binding are mutable metadata.** | A migration for existing stores, and an ID→name projection everywhere a name is shown. |
| 5 | **Self-serve public signup, with a config flag for invite-only.** Instances may front cairn with their own auth proxy. | Quotas, per-principal limits, abuse handling, account deletion/export. |
| 6 | **Dual-run: Python is the oracle.** The Go server proves itself against the Python one on the same store until the bytes match, then Python is retired. | Two implementations alive through the transition. |

**Why 2 is the risky one, stated plainly:** rewriting the server alone would break the
shared-renderer invariant above — two renderers, two languages, agreeing byte-for-byte
forever, with drift arriving as "a different order that reads as a stale cache", which is
exactly the silent failure the mtime/PAX work exists to prevent. Rewriting the *client*
too is what keeps the invariant instead of policing it. That is the reason for 2's scope,
and it is the reason the client rewrite is not optional.

## The design problems, in dependency order

### A. Data model

```
user        (id, provider, provider_subject, email, created_at)
project     (id, name, owner_user_id, created_at)
membership  (user_id, project_id, role)                      -- owner | admin | member
scope       (id PK immutable, display_name, project_id NULL, owner_user_id)
grant       (id, subject_kind, subject_id, object_kind, object_id, verbs,
             granted_by, granted_at, revoked_at NULL)        -- APPEND-ONLY
credential  (id, principal_kind, principal_id, token_hash, narrowed_scopes NULL,
             created_at, revoked_at NULL)
```

🔴 **A credential's authority is DERIVED from grants, never enumerated beside them.**
Today a token row lists its scopes; if a token kept listing scopes *while* grants also
existed, the two would drift, and that is the one-rule-one-place failure this repo has
already paid for elsewhere. So: a credential binds to a principal, its authority is
computed from that principal's grants, and `narrowed_scopes` may only **intersect** —
never widen. A credential that names a scope the principal has lost simply sees nothing.

🔴 **`grant` is append-only and `revoked_at` is a tombstone, not a delete.** "Who granted
whom what, when, and who revoked it" cannot be backfilled later, and without it
"validated access control" is not checkable after the fact. The materialized current view
is derived from this table; the table is the authority.

### B. Store layout under an immutable ID

Do **not** rename the on-disk directory to an opaque ID. Two things break: the local
replica stops being human-browsable (people `cat` these files, and that is a feature),
and every rendered header needs an ID→name lookup to say anything legible.

Instead: **the path stays the display name; the ID lives in per-scope metadata that the
snapshot ships.** On rename or move, a syncing client sees the same ID at a new name and
**renames its local directory** rather than re-downloading the scope. The ID is the
identity for authz and for sync reconciliation; the path is a projection the client
repairs. A rename does change rendered output (the digest names the scope), which is
correct and expected — not a byte-identity violation.

### C. Authorization evaluation

Preserve all four properties measured above, and pin them as a **relationship**, not per
component: a principal × scope × verb matrix, since two components each tested in
isolation can still be broken together. Specifically:
- one predicate, consulted by the opened-set narrowing, the result-shape narrowing and
  the snapshot candidate filter — as today, from one function;
- fail-closed default (the request's visible set starts empty, never unrestricted);
- refused is indistinguishable from absent, on every route including errors;
- a request resolves to exactly one `Principal`, computed once.

The Go rewrite must not introduce a second place that decides visibility. The UI is a
consumer of this, never a second implementation of it.

### D. The pod-side authz cache, and what it cannot see

Materialize each principal's authority from Postgres to local storage with an **epoch**.
Refresh on change notification, on a timer, and on `SIGHUP` (operators already have that
muscle memory). Reads then survive a Postgres or identity-provider outage, which is the
point — cairn's promise is that an offline "orient me" still answers.

🔴 **The honest limit: a revocation is not effective until the cache refreshes.** So
bound the staleness, and *report it* — the authz epoch and its age belong in `doctor` and
in the status surface, because a cache that silently serves a revoked grant is precisely
the instrument that cannot see the thing it is trusted for. Provide a synchronous
"revoke now" path that invalidates before returning, and make the UI say which one
happened.

**Test it by killing the dependency**, not by reasoning about it: stop Postgres, prove
reads still serve, prove a write refuses cleanly, prove the staleness is reported.

### E. Identity interface

```go
type Authenticator interface { Authenticate(*http.Request) (Principal, error) }
```

- **SupabaseJWT** — verify locally against cached JWKS. No network call on the hot path;
  an IdP outage must not stop reads for already-issued sessions.
- **TrustedHeader** — for an instance fronted by an OAuth proxy. 🔴 **This backend is a
  foot-gun and must be built as one that refuses to fire.** If the pod is reachable
  directly, anyone can set the header and become anyone. It must refuse to start without
  an explicit "this deployment is proxy-fronted" config *and* a check on the request
  source (shared secret or mTLS), and it must never be the default.
- **MachineToken** — hashed lookup in the materialized cache; the agent/CI path, which
  cannot do OAuth and is why the token path survives at all.

All three resolve to one `Principal`. Nothing downstream branches on which backend
produced it.

### F. Self-serve signup

Public signup brings work that is not optional once strangers are in: per-principal rate
limits (a limiter exists today and now needs a per-principal key), storage quotas,
abuse handling, and **account deletion and export**. The invite-only flag is a config
switch over the same code path, not a second mode — one rule, one place.

### G. The UI (PWA · Tailwind · gomponents · htmx)

Screens: sign-in · projects · project members · scopes in a project · entry view
(rendered digest) · search · **share dialog** · credentials (create, show once, revoke) ·
grant log · status/doctor.

🔴 **The share dialog must state that unsharing cannot recall a replica.** Every client
holds a full local copy; revoking access stops *future* syncs and does not delete the
copy already on someone's laptop. A sharing UI that omits this implies a guarantee the
system cannot make. Pin the wording in a test — and pin the **whole normalised string**,
because a guard on a few words is walkable by rewording.

On offline: cache rendered digests as whole pages in the service worker. Do **not**
promise offline writes — the write path has no cache to fall back on by design, and a
queued write is a write the caller believes landed.

### H. The conformance suite — the thing that makes the rewrite safe

Before any Go is written, extract the current contract into **HTTP-level golden fixtures**
generated from the Python server: every route, status, header and body, including the
refusals and the uniform-401 behaviour. Language-agnostic, runnable against either
implementation. This is the single highest-leverage item in the plan: it converts "we
ported it carefully" into a measurement.

Then, in order: the Go server passes the conformance suite → both servers run on the same
store and `verify-byte-identity.sh` compares them → the Go client's output is byte-identical
to the Python client's against the same cache → cut over → retire Python.

🔴 **A green conformance suite is a claim about the cases it encodes.** Name what it
cannot see: concurrency, cache staleness, anything depending on a real Postgres, and any
route added after the fixtures were generated. Fixtures are generated, not hand-written,
and regenerating them is part of changing the contract.

### I. CLI ⇄ UI ⇄ API parity, mechanically

One **capability ledger** defined in one place, with the CLI verb table, the HTTP route
table and the UI action set each asserted against it — failing when the set GROWS or
SHRINKS. That turns "the CLI covers every feature" from a promise into a gate. Today's
verbs: `sync · recall · search · ls-entries · validate · doctor · append · put · create`,
plus the new ones this phase implies (`whoami`, `share`, `projects`, `members`,
`credentials`).

## Sequence

| phase | what | size | why here |
|---|---|---|---|
| P0 | Conformance fixtures + capability ledger. No behaviour change. | M | De-risks everything after it. |
| P1 | Go server passes conformance against the **existing static token file**. Dual-run, byte-identity green. No new features. | L | One variable at a time: language, not model. |
| P2 | Go client byte-identical to the Python client; nix packaging swaps to the binary. Python client kept until parity is proven. | M | Closes the two-renderer window as early as possible. |
| P3 | Data model, immutable scope IDs, projects, grants, memberships, grant log, materialized cache with epoch + staleness reporting. Authz matrix tests. | L | The control plane proper. |
| P4 | Identity interface, Supabase (GitHub/Google), trusted-header backend, browser sessions. | M | Needs P3's principals to resolve to. |
| P5 | The PWA. | L | Needs P3 and P4 to have anything to show. |
| P6 | Self-serve signup, invite-only flag, quotas, per-principal limits, deletion/export. | M | Gating the public surface last, deliberately. |
| P7 | Snapshot ETag/304 keyed by **principal + epoch**. | S | Was a scale item; with many principals a mis-keyed cache cross-serves another tenant's tar, so it is now correctness. |
| P8 | Retire Python; delete the oracle once the gate has held. | S | Only after P1–P2 have been green across real use, not on the day they first pass. |

Sizes are relative (S/M/L), not estimates — no one has measured these.

## Risks, each with its control

| risk | control |
|---|---|
| The rewrite loses behaviour nobody wrote down. | P0's generated fixtures + dual-run byte-identity. Not code review. |
| Two renderers drift. | P2 deletes the second renderer. Until then the gate is the comparison, not discipline. |
| A revoked grant keeps working. | Bounded cache staleness, epoch reported in `doctor`, synchronous revoke path. |
| Trusted-header spoofing. | Backend refuses to start unfronted; source check required; never default. |
| Supabase outage stops reads. | JWKS cached, authz materialized; verified by killing the dependency, not by argument. |
| The UI implies privacy the system lacks. | The replica-honesty notice, pinned as a whole normalised string. |
| Public signup cost/abuse. | P6's quotas and per-principal limits, before signup opens. |

## Deliberately NOT in this phase

End-to-end per-scope encryption (it kills server-side search; client-side search over the
replica is viable later precisely because every client already holds the files),
real-time collaborative editing, federation between instances, and entry-level (sub-scope)
sharing — grants stop at the scope for now.

## Deferred decisions, to settle inside P3/P4

- Verb granularity on a grant: `read` / `append` / `write` / `admin`, or fewer.
- Whether a project can be granted a scope (project-as-subject) in v1, or only users.
- Whether credentials are per-user or per-project service accounts, or both.
- What a scope with `project_id NULL` means: personal, or implicitly a solo project.
- Whether the legacy bare-token row survives the rewrite or is dropped at P8.

**The first four were settled before P3a was written; the answers and their reasons are
the table in `internal/control/README.md` ("What the plan left open, and what was
settled"). The fifth is settled here, because P3b piece (b) would otherwise have settled
it de facto by shipping code.**

🔴 **THE LEGACY BARE-TOKEN ROW SURVIVES THE REWRITE.** `internal/control/tokenfile`
projects a bare row into a project principal holding `read` on the scopes project, so
the unrestricted credential keeps working through the new authorization predicate rather
than being dropped at the point the predicate arrived.

The reason is that dropping it is **not a code change**. The deployed pod's only
principals are bare rows — `server/README.md`'s rotation procedure shows the live table
as `token reload: LOADED 2 identities [<new>:legacy,<old>:legacy]` — so retiring the
shape means editing the secret to give every holder an explicit allowlist, and doing it
in step with a pod that is serving. That is a coordinated cutover against a live deployment, and this
phase's whole claim is that the mechanism underneath the served contract was replaced
while the contract did not move.

⚠ **It is SURVIVES-FOR-NOW, not survives-forever, and P8 inherits the question.** The
adapter reproduces the bare row's authority with a measured divergence (a scope
directory created out of band is invisible to it until the next refresh — see
`internal/control/README.md`), which is a cost that exists only while an unrestricted
principal does. P8 already owns retiring the Python oracle and a widening of the CLI
contract; retiring the bare row belongs with those, where a coordinated cutover is
already on the table, and it must not be done by reintroducing an unrestricted principal
in the model.
