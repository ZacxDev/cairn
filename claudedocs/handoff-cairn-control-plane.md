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
- Branch `feat/control-plane-data-model` @ `5c02f2b`, pushed. **PR
  [#29](https://github.com/ZacxDev/cairn/pull/29) OPEN, CI IN FLIGHT at handoff time** —
  `leakscan` green, the other five jobs still running. 🔴 Nobody has read a green `go`,
  `tests`, `parity`, `dualrun` or `nix` job on this branch; check before merging.
  `mergeable=MERGEABLE` against `main`.
- Other open PR is still **another session's** #24 (`feat/client-multi-instance-routing`)
  — not this effort's, and see the merged-tree gotcha below.
- **DONE this session — P3a, the control plane's data model and its one predicate:**
  - `internal/control/` (10 files, ~3,900 lines with tests): `model.go` (users,
    projects, memberships, immutable scope ids, append-only grants, credentials),
    `verbset.go`, `journal.go` (11 event kinds, replay), `ids.go`, `resolve.go`
    (`Resolve`, `Authenticate`, `Narrow`), `filestore.go` (the `Store` seam +
    the file backend), `README.md`.
  - `tests/control_mutants.py` — 28 mutants, wired into the `go` CI job (~30s).
  - `.github/workflows/ci.yml` — the `go test` `ok` floor moves **10 → 11** (twelve
    packages carry tests now; a floor left at 10 stops failing for the FIRST deletion).
- **The four deferred decisions in `plan-cairn-control-plane.md` §"Deferred decisions"
  are SETTLED by the operator** (recorded with their reasons in
  `internal/control/README.md`): three verbs `read`/`write`/`admin`; a project is BOTH a
  grant subject and a credential principal; **no NULL project** (signup mints a solo
  one); the authority sits behind a `Store` interface with a file backend first.
- **NOT DONE — the rest of P3, and it is the bulk of the wiring.** `internal/control` is
  a LIBRARY: no route consults it, and `authz.TokenRecord` + the token file still
  authorise the pod. The four remaining pieces are listed in rank 1 below and in
  `internal/control/README.md` §"What is left of P3".
- **Deploy/verify status: unchanged — nothing deployed.** `packages.default` is still
  the Python client. The Go server has still never run against the real pod.
- Gates measured on `5c02f2b`: 1,860 python tests · go vet + `go test ./...` **12/12
  packages ok** · corpus **116 PASS / 0 failures / 4 oracle-specific skips** (Go) and
  **99 requests / 433 assertions / 0 failures / 0 skipped** (oracle) · leakscan **0
  findings / 259 files** with both controls PASS · control battery **28 mutants /
  27 killed / 1 EQUIVALENT / 0 misattributed**, positive control GREEN ·
  `nix build .#cairn-server-go` builds (its `go test ./...` runs in the derivation).
  🔴 `parity` and `dualrun` were NOT run locally — they need a live pod, and this change
  touches no serving path. CI is the only reading of them on this branch.
- Measured on the **MERGED tree** (this branch + `origin/feat/client-multi-instance-routing`):
  1,923 python tests · 12/12 Go packages · leakscan 0/267 · battery 28/27/1 ·
  `test_agent_instructions_weight.py` green with **1,031 B headroom**.

## Next steps (ranked)
1. **P3 — the REST of the control plane.** P3a (the model, the journal, `Resolve`,
   `Authenticate`, `Narrow`, the `Store` seam) is PR #29. Four pieces remain, in
   dependency order: **(a)** the materialized pod-side cache — refresh on change, on a
   timer and on `SIGHUP`, with the epoch and its age reported in `doctor` and the status
   surface; **(b)** wiring `control.Principal` into `internal/api` in place of
   `authz.TokenRecord`, keeping the conformance corpus green; **(c)** the migration from
   the token file, which is where the legacy **unrestricted** bare row has to become
   explicit grants against a model that has no unrestricted principal by construction;
   **(d)** immutable scope ids shipped in the snapshot, and the client renaming its local
   directory when it sees a known id at a new name. Repo `cairn`; `internal/control`,
   `internal/api`, `internal/snapshot`, `internal/client`.
   forcing: user — the operator specified per-scope sharing, projects and invites directly.
2. **P4 — identity as an interface, two backends.** Supabase JWT verified locally against
   cached JWKS, and trusted-header/forward-auth for a proxy-fronted instance.
   🔴 The trusted-header backend must **refuse to start** unless explicitly configured as
   proxy-fronted with a source check — otherwise anyone reaching the pod directly is
   anyone. Needs P3's principals to resolve to, which now exist (`control.Principal`).
   🔴 **AND IT INHERITS THE STDLIB-ONLY DECISION** — see the gotcha below; do not start
   writing a Postgres backend before settling it.
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
   every sync is O(store) per client. 🔴 Now that P3a exists this stops being a scale
   item: with many principals a mis-keyed cache cross-serves another tenant's tar, so key
   it on **principal + epoch** — and `control.Authorization` already carries `Epoch`.
   forcing: none
5. **The cutover decision** — `packages.default` → the Go client, and the deployed image
   → the Go server. Backed by P1's dual-run rather than a leap. 🔴 `AGENTS.md` says
   to diff the two images for what the agreement test cannot read before swapping;
   that instruction now has an instrument behind it.
   forcing: none
6. **P8 — retire the Python oracle.** Gated on 5 having held in real use. The retirement
   ledger in `tests/parity/README.md` lists what dies with it (`tests/parity/`,
   `tests/dualrun/` ≈2,900 lines, `internal/store/pyoserror.go`, the `dualrun` job).
   forcing: none

## Defects (batched)
Fix as one round; closing one buys room for one rank.
- 🔴 **`AGENTS.md` IS 584 B FROM ITS WORKING CEILING AND THE NEXT LINE ANYBODY ADDS
  TRIPS IT.** 36,216 B + `CLAUDE.md` 267 B = 36,483 B against a 36,800 B working budget
  (`MAX_BYTES` 37,700 − `MIN_HEADROOM_BYTES` 900). This session hit it TWICE while adding
  a five-line pointer and had to delete the pointer to land. The eviction that resolves it
  already has a closing condition: the ~15.9 KB server section relocates to
  `tests/conformance/README.md`, where it is largely duplicated, and `MAX_BYTES` is
  **lowered**. That is the one item on this list worth doing before any other work touches
  that file.
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

- 🔴 **TWO PRs THAT MERGE WITH ZERO TEXTUAL CONFLICT, BOTH INDIVIDUALLY GREEN, PRODUCED A
  RED MERGED TREE — AND NEITHER PR'S CI COULD SEE IT.** This branch and #24 both add lines
  to `AGENTS.md`. `git merge` was clean; `mergeable=MERGEABLE`; both branches green. The
  merged tree failed `test_the_session_instructions_keep_WORKING_HEADROOM` with **716 B of
  headroom against a required 900**. It was found only by BUILDING the merged tree in a
  throwaway worktree and running the gate there. Fixed on this side at `5c02f2b` (the
  pointer paragraph folded into the layout row) so merge ORDER does not decide whether
  `main` goes red; merged headroom is now 1,031 B. **The general rule this instantiates:
  a byte-budget gate over a shared file makes every concurrent PR a semantic conflict, and
  the conflict is invisible to both.** Build the merged tree.
- 🔴 **P3a's STORAGE ANSWER COLLIDES WITH THE `STDLIB ONLY` RULE, AND P4 OWNS THE
  DECISION.** `go.mod` has no `require` block and `flake.nix` passes `vendorHash = null`,
  which together make a dependency in the serving path a BUILD FAILURE rather than a
  silent addition. Every embedded SQL engine for Go is a third-party dependency, and **so
  is every Postgres driver**. So the file-backed journal is not a placeholder chosen for
  speed — it is the only durable authority the stated constraint permits today, and the
  plan's decision 1 ("authz state in Postgres/Supabase") **cannot be implemented without
  first deciding what happens to stdlib-only.** Recorded in `internal/control/README.md`;
  do not discover it halfway through P4.
- 🔴 **A `Model` STRUCT COPY IS NOT A COPY, AND THAT DEFECT SHIPPED IN P3a's FIRST DRAFT.**
  `control.Model` is six maps behind a struct header. `FileStore.Append` validated a batch
  by applying it to `current` — which shares every bucket with the live cache — so a batch
  rejected at its third event left the first two permanently applied to the served
  authority with **nothing in the journal recording them**: a model WIDER than the file it
  claims to project, and a restart silently "losing" grants that were never written.
  `Model.clone` is the fix, `TestARejectedBatchLeavesNeitherBytesNorState` is the guard
  (red at baseline by a one-word change), and the battery carries the same defect one
  nesting level down because `maps.Clone` is ONE LEVEL DEEP and the two membership indexes
  are maps of maps.
- 🔴 **A MUTATION BATTERY'S OWN ROWS ARE AS WRONG-ABLE AS THE CODE, AND THREE OF THIS
  ONE'S WERE.** `tests/control_mutants.py` found: a row naming a killer that **never
  runs** (the narrowing test calls `Narrow` directly and never reaches `copyIDs`, so the
  row read as coverage while providing none — inside the control built to refuse exactly
  that); and two mutants that **failed to apply at all** — one drifted pattern, one that
  left an import unused so the tree did not build. **A mutant that does not COMPILE dies
  at the build rather than at a guard, which is the one outcome that proves nothing.**
  Hence: attribution by WHICH TEST failed (a kill by another test is reported
  MISATTRIBUTED, not as a pass), an occurrence-COUNT assertion on every pattern, a
  "DID NOT BUILD" outcome distinct from a kill, and a positive control on an unedited copy.
- **The authz matrix carries a positive control because a uniformly-refusing matrix is
  self-consistent and measures nothing** — the same shape `tests/parity/README.md` records
  (72 PASS / 0 FAIL while the pod refused every request). The allow count is pinned from
  BOTH sides: **32 of 60 cells**, not 0 and not 60.
- **Decision (operator, this session): a project principal has NO authority over its own
  project's scopes.** A project is not a member of itself, so a service account for a
  project reaches that project's scopes only if somebody granted it. The wide alternative
  — "a project credential implicitly holds what the project owns" — is a rule that never
  appears in the grant log, and "who could see this, and when" is the whole reason the log
  exists. The same behaviour is available VISIBLY, via an explicit self-grant at project
  creation; that is a policy decision for whoever wires this up, recorded in
  `internal/control/README.md` because the default will otherwise read as an oversight to
  the first person who creates a CI credential and finds it cannot write.
- **`go test`'s `ok` floor is a LEDGER, not a smoke test** — it moved 10 → 11 with this
  change because twelve packages now carry tests. A floor left one below an OLD count does
  not fail late; it stops failing for the FIRST deletion, which is the only one anybody
  would notice.

## How to verify
```bash
cd /home/zach/workspace/cairn
python3 tests/leakscan.py && python3 tests/leakscan.py --self-test   # 0 findings / 259 files, controls PASS
uv run --python 3.12 --with pytest -- pytest tests -q -p no:randomly # 1860 passed
go vet ./... && go test ./...                                        # 12 packages ok
python3 tests/control_mutants.py                                     # 28 / 27 killed / 1 EQUIVALENT / 0 misattributed
python3 tests/control_mutants.py --show                              # every edit, without running it
python3 tests/conformance/suite.py run                               # oracle: failures=0 skipped=0
bash tests/conformance/run_go.sh                                     # Go: 116 PASS / 0 failures / 4 skips
python3 tests/parity/harness.py                                      # 90 cases / 91 passes / 0
python3 tests/parity/harness.py --break-pod                          # MUST exit 2 — could not vouch
python3 tests/dualrun/harness.py                                     # 361 / 1489 / 0
python3 tests/dualrun/harness.py --break-both                        # MUST exit 2 — could not vouch
python3 tests/dualrun/harness.py --store ~/.claude/analyze-service-index   # 612 / 2492 / 0; reads only the summary
```
🔴 The two `--break-*` runs are the point: a gate that cannot refuse has not been read.
🔴 The `--store` command prints **real scope names** per target — never paste its full
output into a transcript, a PR or a CI log. Read the `SUMMARY` line only.
🔴 **Before merging ANY branch alongside another open PR, build the MERGED tree and run
the gate there** — `git worktree add <tmp> -b tmp/merged <yours>` → `git merge
origin/<theirs>` → run `pytest tests -q`, `go test ./...` and
`tests/test_agent_instructions_weight.py` in it. A clean `git merge` and a green
`mergeable` are not evidence; this session measured a red merged tree behind both.
