# Handoff: cairn-control-plane — 2026-09-15

## Run this first — the index, one command
```bash
cairn recall --repo /home/zach/workspace/cairn
```
Terse pointers this doc does not carry, curated by past sessions and outliving it.
🔴 RECALL, NOT LIVE OBSERVATION — every line is a pointer to VERIFY, never a current
reading, and it may describe a gotcha already fixed. `scope-absent`/`scope-empty` means
nothing is recorded yet: ordinary, not an error, and not a clean bill of health.
Non-blocking: if it exits non-zero, print the stderr line and carry on.

## Goal
Turn cairn from a store with a static allowlist into a multi-user product: per-scope
sharing with users and projects, project membership and invites, identity via Supabase
(GitHub/Google) or a fronting auth proxy, and a PWA — on a foundation where **one
renderer** serves pod, CLI and UI. Plan: `claudedocs/plan-cairn-control-plane.md` (#16).

- **closing-condition:** `check` — on `main`: a green principal × scope × verb authz
  matrix over projects+grants; the PWA's share flow serving a scope granted from one
  user to another, with its replica-honesty notice pinned by a test; identity resolving
  through both backends; and `packages.default` the Go client. A later session runs
  `pytest tests -q`, `go test ./...` and reads `flake.nix`. ADDRESSED ⇒ arc CLOSED.

## State now
- Branch `main` @ `0497140`, clean, up to date with origin. **Twelve PRs merged this
  session** (#14–#23, #25–#27). Only open PR is **another session's** #24
  (`feat/client-multi-instance-routing`) — not this effort's.
- **DONE — the foundation (plan phases P0–P2), all merged:**
  - **P0** — `tests/conformance/` HTTP corpus, 98 generated goldens, 8 routes (#19);
    `tests/testlib/capability_ledger.py` + `tests/test_capability_ledger.py`, CLI↔route
    parity as a gate failing on GROW *and* SHRINK (#18).
  - **P1** — the Go store API: `cmd/cairn-server/`, `internal/{api,authz,store,snapshot,
    write,netid,pytext,report}` (#21, #22). Corpus green for **both** servers.
  - **P1c** — `tests/dualrun/`: two servers, one store, every route (#26).
  - **P2** — the Go client `cmd/cairn/` + `internal/client/`, one renderer three
    consumers, with `tests/parity/` (90 cases) (#23).
  - **Unplanned but urgent** — a live leak in this PUBLIC repo: real project/node names
    and dated incident refs across 23 tracked files. Scrubbed, and the two ungated
    `Never commit` bullets now have a mechanical gate (#27).
- **NOT STARTED — everything the operator asked for directly.** P3 (data model,
  projects, grants, memberships), P4 (identity/Supabase), P5 (the PWA), P6 (signup,
  quotas, deletion/export). Measured: `supabase`, `project_id`, `oauth`, `PWA`,
  `gomponents`, `htmx` each appear in **exactly one tracked file — the plan doc.**
- **Deploy/verify status:** nothing deployed. `packages.default` is still the **Python**
  client; the Go client and server are built but **not shipped**. The Go server has
  never run against the real pod — only against copies of the store.
- Gates on `main`: 1,860 tests · corpus 116 PASS/0 (Go, 4 `oracle_only` skips) and
  121/0 (oracle) · dualrun 361/1,489/0 generated, **612 targets / 2,492 comparisons /
  0 differences** against the operator's real store · parity 90 cases/91 passes/0 ·
  leakscan 0 findings/244 files. Six CI jobs.

## Next steps (ranked)
1. **P3 — the data model and authorization.** `users`, `projects`, `memberships`,
   immutable `scope` ids, append-only `grants`, credentials **derived** from grants
   (never enumerated beside them), and a pod-side materialized cache with an epoch and
   reported staleness. Authz matrix tests (principal × scope × verb) as a relationship,
   not per-component. This is the gate on every item below it and on everything the
   operator asked for. Repo `cairn`; new `internal/authz` surface + a store migration.
   forcing: user — the operator specified per-scope sharing, projects and invites directly.
2. **P4 — identity as an interface, two backends.** Supabase JWT verified locally
   against cached JWKS, and trusted-header/forward-auth for a proxy-fronted instance.
   🔴 The trusted-header backend must **refuse to start** unless explicitly configured as
   proxy-fronted with a source check — otherwise anyone reaching the pod directly is
   anyone. Needs P3's principals to resolve to.
   forcing: user — "identity via supabase (github and google)", plus a named second
   instance that will front it with an oauth proxy.
3. **P5 — the PWA** (Tailwind + gomponents + htmx). Sign-in, projects, members, scopes,
   entry view, search, **share dialog**, credentials, grant log, status. 🔴 The share
   dialog must state that unsharing cannot recall a replica — every client holds a full
   local copy, so revoking stops future syncs and deletes nothing already on disk
   elsewhere. Pin the whole normalised string, not keywords.
   forcing: user — "a fully featured UI (PWA tailwind + gomponents + htmx webapp)".
4. **P7 — conditional snapshot sync.** `GET /api/v1/snapshot` ships a full tar with no
   ETag/304 — verified at `0497140`: `_snapshot` contains zero ETag/304 handling — so
   every sync is O(store) per client. Inherited as the only live rank from
   `claudedocs/handoff-agents-migration.md`. 🔴 Once P3 lands this stops being a scale
   item: with many principals a mis-keyed cache cross-serves another tenant's tar, so key
   it on **principal + epoch**.
   forcing: none
5. **The cutover decision** — `packages.default` → the Go client, and the deployed image
   → the Go server. Now backed by P1's dual-run rather than a leap. 🔴 `AGENTS.md` says
   to diff the two images for what the agreement test cannot read before swapping;
   that instruction now has an instrument behind it.
   forcing: none
6. **P8 — retire the Python oracle.** Gated on 5 having held in real use. The retirement
   ledger in `tests/parity/README.md` lists what dies with it (`tests/parity/`,
   `tests/dualrun/` ≈2,900 lines, `internal/store/pyoserror.go`, the `dualrun` job).
   forcing: none

## Defects (batched)
Fix as one round; closing one buys room for one rank.
- **PR #15's six findings, still open on `main`** (recorded at
  https://github.com/ZacxDev/cairn/pull/15 when the operator chose to merge with them
  open; each re-checked present at `0497140`): `lib/cairn_doctor.py` claims "2,016
  bytes" for a value that is HOME-length dependent (measured 1,812/2,100/2,604) and
  claims a point "would INVERT" where it exits 1, not 120; `cairn:337` cites
  `server.py:422` for `sole_header`, which is at `server/server.py:1409`;
  `claudedocs/handoff-agents-migration.md` still asserts the observable set "is
  0/1/2/9/10" where a later commit of that same PR made it non-exhaustive; the ledger's
  vacuity-control prose names the wrong guard.
- **Go/oracle divergences deferred with closing conditions in code**: `NaN`/`Infinity`
  (needs a second JSON tokenizer), the `text`-field surrogate message (needs a
  surrogate-preserving decoder), the `actor`-key 400-vs-200 residual, and `?page=<21
  digits>` (Python `int` is arbitrary precision, `strconv.Atoi` is not — oracle 200, Go
  400; recorded in `AGENTS.md`).
- **`server/seed.sh:110`** has the `( cd "$1" && … )` shape that made
  `tests/conformance/run_go.sh` unrunnable — a bare `cd <relative>` **prints** the
  directory when it resolves through `CDPATH`. The other scripts use `CDPATH= cd --`.
- **`AGENTS.md`'s server section** is ~15.9 KB of 35.8 KB and largely duplicated in
  `tests/conformance/README.md`. Closing condition already written: the section
  relocates and `MAX_BYTES` in `tests/test_agent_instructions_weight.py` is *lowered*.

## Gotchas / decisions / dead-ends
- 🔴 **A green corpus is not a green port.** The conformance split was 94/22/0/4 *before*
  two criticals were fixed and *before* the token fail-open was found. Three defects were
  found outside it: a Go loader that accepted a non-UTF-8 token file as an unrestricted
  legacy credential and served the whole store via `/snapshot`; an installer that dropped
  every member's mtime; `--text -h` exiting 0 while writing nothing. The corpus replays
  declared cases against a **correctly configured** server — it cannot vary the token
  file's bytes.
- 🔴 **A differential gate can pass because BOTH sides are broken.** The parity harness
  reported **72 PASS / 0 FAIL while measuring nothing** — a trusted-proxies env var
  copied from the conformance runner made the pod refuse every request, and two clients
  failing identically compare equal. Hence `--break-pod` / `--break-both` (rc 2 = could
  not vouch), a pre-flight, a content floor and a self-test on both harnesses. **Run the
  control, not just the gate.**
- 🔴 **`AGENTS.md` claimed `tests/leakscan.py` enforced five bullets; it gates three.**
  That false sentence is why nobody looked, and a live leak sat behind it through four
  green gates, 1,860 tests and eleven merged PRs. Now: the claim says what each bullet is
  covered by, and the two ungated ones have a mechanical rule.
- 🔴 **A plaintext denylist in a public repo republishes what it removes.** The gate holds
  **SHA-256 digests**, matched over `-`/`_` prefixes. Nobody can audit the set from inside
  the repo — stated in the constant's header, because a set auditable from here is
  readable from here. The one remaining plaintext real-name site is the
  `reachable-hostname` regex, and it **must** stay plaintext: it matches an *unbounded*
  set (any host under those domains), and digesting would convert "any host in this
  domain" into "these exact hosts" and stop catching the new one.
- 🔴 **Scrubbing `HEAD` does not unpublish history.** The names remain in git history on a
  public remote. The operator considered and **declined** a history rewrite.
- **`server/verify-byte-identity.sh` is NOT prior art for `tests/dualrun/`.** It compares
  the pod against the local CLI — one reader over **two** stores; the dual-run is two
  servers over **one**. Most of its machinery exists *because the stores can differ*;
  borrowing it imports four licences to differ that the dual-run must not grant.
- **Byte-identity means the UNCOMPRESSED tar.** gzip identity is unattainable between Go's
  `compress/flate` and zlib (Go hardcodes the OS byte, no API).
- **`tests/parity/harness.py` and `tests/dualrun/harness.py` collide on the module name
  `harness`.** In the reverse import order the ledger measures the *wrong gate while
  passing*. Both are run as scripts (`python3`, not `bash`).
- **Never run two `uv run --with pytest` invocations concurrently** — one disposes of the
  other's ephemeral venv and produces a bogus `FileNotFoundError` red.
- **zsh ate `$var:` twice this session** (`$T:tests/…` → `:t`, `$s:AGENTS.md` → `:A`),
  each time returning a confident *wrong* value — a false "0 references", an empty file
  that "parsed OK". **Brace it: `${var}:path`.**
- **The flake's source filter is git-based**: an untracked new `.go` file compiles under
  `go build` and fails `nix build`. Stage new files before reading either tier as green.
- **`AGENTS.md` + `CLAUDE.md` are gated** to 37,700 B with a 900 B minimum headroom
  (`tests/test_agent_instructions_weight.py`); currently 36,354 B. Put narrative in a
  harness README behind a pointer — that is the pattern the gate's own playbook prints.
- **Decision (operator, this session):** an unencodable seed stamp resolves to
  `seeded=UNREADABLE` on **both** sides — an authorised exception to P1's "do not change
  the oracle", because a contract cannot include "sometimes truncate the response
  mid-stream". Regenerating all 98 goldens after it moved **none**.
- **Dead end:** the narrow "audit/process arms only" shape for the dual-run was costed and
  rejected — it saves ~23 of 1,799 lines and forces out the tar arm, the one the corpus's
  own README flags 🔴 with four measured divergences.

- 🔴 **THE `dated-incident` RULE FIRES ON VERIFICATION STAMPS, NOT ONLY ON INCIDENT
  NARRATION — and it caught the first draft of THIS doc.** `leakscan` refused
  `verified <a real date>:` twice, on the PR immediately after the rule shipped. The fix
  is not an exemption: **pin the claim to a COMMIT SHA instead of a day.** A date says
  when somebody looked; a sha says which tree they looked at, which is the thing a later
  reader can actually check out and re-measure. Prefer `verified at <sha>` everywhere in
  this repo's prose — it satisfies the gate and is strictly more useful.
  🔴 **Then it fired a SECOND time — on this very bullet, for quoting the date it was
  warning about.** Document the shape with the placeholder `<a real date>`, the way
  `AGENTS.md` does; an example that instantiates the thing it forbids is the thing it
  forbids. ⚠ And note what this exposed: `Gotchas` is an APPEND section, so
  `handoff_doc.py` — which owns every write to this file — **structurally cannot correct
  a line inside it**; appending leaves the bad line in place and the gate keeps refusing.
  That correction had to be a direct edit, and that is the one case where it is right.

## How to verify
```bash
cd /home/zach/workspace/cairn
python3 tests/leakscan.py && python3 tests/leakscan.py --self-test   # 0 findings / 244 files, controls PASS
uv run --python 3.12 --with pytest -- pytest tests -q -p no:randomly # 1860 passed
go vet ./... && go test ./...                                        # 11 packages ok
python3 tests/conformance/suite.py run                               # oracle: failures=0 skipped=0
bash tests/conformance/run_go.sh                                     # Go: 116 PASS / 0 failures / 4 skips
python3 tests/parity/harness.py                                      # 90 cases / 91 passes / 0
python3 tests/parity/harness.py --break-pod                          # MUST exit 2 — could not vouch
python3 tests/dualrun/harness.py                                     # 361 / 1489 / 0
python3 tests/dualrun/harness.py --break-both                        # MUST exit 2 — could not vouch
python3 tests/dualrun/harness.py --store ~/.claude/analyze-service-index   # 612 / 2492 / 0; reads only the summary
```
🔴 The two `--break-*` runs are the point: a gate that cannot refuse has not been read.
🔴 The last command prints **real scope names** per target — never paste its full output
into a transcript, a PR or a CI log. Read the `SUMMARY` line only.
