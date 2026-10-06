# Handoff: cairn-arcs-sessions — 2026-10-05

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
Session and arc registration: a caller can resolve, for any scope it may read, **every
session that wrote there and every arc that touched it**, over the API, the Go CLI and the
browser surface — each answer stating its own coverage. Design:
`claudedocs/plan-cairn-arcs-sessions.md` (read it before any slice; it carries the measured
facts, the eight design decisions, the ledgers and the slice table).

- **closing-condition:** `check` — slices S0–S5 of the plan are MERGED on `main` (verified
  by content, not ancestry), AND `tests/arcs/e2e.sh` exits 0 on `main` — it boots
  `cairn-server` on a synthetic store, appends two trailered bullets from two sessions,
  registers one arc with the proposed `arc register` verb (S3), and asserts that the proposed
  `sessions` and `arcs` verbs, each run with `--scope alpha-notes`, list both sessions and the arc with their
  coverage lines, while a principal without that scope gets the absent answer (the negative
  control) — AND, against the DEPLOYED pod, the proposed `arcs` verb run with
  `--repo /home/zach/workspace/cairn` lists this arc (`cairn-arcs-sessions`), registered by `/handoff` itself (slice T1).
  ADDRESSED ⇒ arc CLOSED.
- closing-verdict: addressed

## State now
- ✅ **VERDICT: ADDRESSED — THE ARC IS CLOSED.** All three clauses measured:
  (1) S0–S5 merged (#182–#187); (2) `tests/arcs/e2e.sh` on `origin/main` `7528c74`: 32 PASS /
  0 FAIL, `--self-test` sabotaged=3 caught=3; (3) against the DEPLOYED pod,
  `cairn arcs --repo /home/zach/workspace/cairn` → `status=arcs-listed`, row
  `cairn/cairn-arcs-sessions · declared · status open · closing check · 1 member`, registered by
  this doc's own `/handoff` (`arc-register: registered home=cairn slug=cairn-arcs-sessions
  status=open`) — run from a WORKTREE, so `DeriveScope`'s worktree-stability held live.
  Extra: `arcs --check` against the deployed pod → `arcs-check-clean`, rc 0, 1 arc, no findings.
- ⚠ **THE INSTALLED CLIENT LACKS `arcs --check`**: T1 pinned the tooling repo's cairn at
  `4714652` (S4); `--check` landed in S5 (`7528c74`). The installed `cairn arcs --check` is a
  usage error (rc 2) until that pin is bumped again; the check above ran via
  `nix run github:ZacxDev/cairn/ea9cfa7 -- arcs …`. Not part of the closing condition.
- ✅ **THE JOURNAL MOUNT IS LIVE** (deployment repo `trunk` `a49259c`): PVC `cairn-arc-journal`
  (local-path RWO 128Mi) RW at `/var/lib/cairn-arcs` in `subsystem-store-api`, RO in `cairn-ui`,
  `CAIRN_ARC_JOURNAL=/var/lib/cairn-arcs/arcs.jsonl` in both; BOTH images bumped `ffa0eca` →
  `ea9cfa7` (the images were pre-arcs — a mount alone would have been inert). Both rolled out,
  0 restarts, same node; cairn-ui logs `arcs read-only from /var/lib/cairn-arcs/arcs.jsonl`.
  Rollback: both `image:` lines back to `ffa0eca` (named in each file's rollback comment).
- ✅ **BOTH HOSTS SWITCHED** (`scripts/ship.sh` in the tooling repo: both at `05180b6`, VERIFIED):
  the installed client lists `sessions`, `arcs`, `arc-show`, `arc-register`.
- ✅ **DEPLOYED POD ANSWERS, pre-registration:** `cairn arcs --repo /home/zach/workspace/cairn`
  → `status=no-arc-registered scope=cairn`, `attributed: 72 of 102 bullets`, `0 of 36 writing
  sessions` in an arc — the journal is configured (not `registrations-unconfigured`) and empty.
- ✅ **S0–S5 ALL MERGED on `main`**, each squash pinned with `--match-head-commit` to a head
  whose 8 CI jobs were green: S0 #182 `0572b35`, S1 #183 `21d8782`, S2 #184 `c862970`, S3 #185
  `dda0c3d`, S4 #186 `4714652`, S5 #187 `7528c74`. Plan #180 `107b389`.
- ✅ **CLAUSE 2 MEASURED on a clean detached checkout of `origin/main` at `7528c74`:**
  `tests/arcs/e2e.sh` rc 0, **32 `PASS` lines / 0 `FAIL`**, `SUMMARY e2e: passed=32 failed=0
  expected=32`; `--self-test` rc 0, `sabotaged=3 caught=3` (the positive control).
- ✅ **T1 MERGED in the private tooling repo** (its #2071, squash `05180b6`): a landed handoff
  runs `cairn arc-register --repo … --slug <topic> --from <payload>` after `status=written|pushed`,
  NON-BLOCKING (one `arc-register:` stderr line; an env-var opt-out documented in that repo's handoff skill), and
  bumps that repo's cairn pin `d7e1fec` → `4714652`. Status comes from a NEW `closing-verdict:`
  field under `## Goal` (this doc now carries one); absent ⇒ `unknown`. Merged over a RED gate by
  operator decision: its base was already red with the SAME 8 pytest failures + nodetests
  (collected 25524→25580, passed 25506→25562 — exactly its +56 new tests).
- ⚠ **ONE DEPARTURE FROM THE PLAN, in S5:** "a member session that wrote nowhere the principal can
  see" is a COVERAGE number, not exit 10 — members come from commit trailers and transcripts, so a
  member with no entry trailer is the normal state and a 10 would be permanent. One-line revert.
- ⚠ **Verb names as shipped:** `sessions`, `arcs` (`--check [--all-scopes]`), `arc-show`,
  `arc-register --from <json>` — hyphenated, not `arc register|show`, because the ledgers carry one
  read/write bit per verb.
- ⚠ **Mid-phase seams, each found by CI or a rebase, not by a slice's own gates:** the
  verb-citation guard read only the PYTHON parser (S0 widened it to `GO_ONLY_VERBS`); the authz
  battery's create row matched S3's new PUT check twice (re-anchored + a new row, 190); a textually
  clean S5 rebase failed `go build` on S4's rename `unconfiguredBody` →
  `RegistrationsUnconfiguredBody`. Battery now **201 mutants, 199 killed, 2 EQUIVALENT**.

- ⚠ **Deferred, NOT this arc** (recorded in the plan's Deferred section): recording READS
  (client session header + retention; the pod sees syncs, not recalls) and an authenticated
  append-time write log (Q6). Each would be a new arc with its own closing condition.

## Next steps (ranked)
1. ~~**Operator: the journal mount.**~~ **DONE** — deployment repo `trunk` `a49259c`.
   forcing: user — the operator chose a journal outside the store tree (Q2).
2. ~~**Operator: `home-manager switch` on each host.**~~ **DONE** — both at `05180b6`.
   forcing: user — the deploy step for the tooling repo is the operator's.
3. ~~**Close the arc.**~~ **DONE** — see State now; ADDRESSED.
   forcing: gate — clause 3 of the closing condition.

## Gotchas / decisions / dead-ends
- **The trailer's `<session>` is self-declared and only the APPEND route sets `<actor>`.**
  `put`/`create` write bytes verbatim (`internal/write/write.go:255-264`), so a derived edge
  is a claim the entry makes, not an authenticated fact. Every answer must say so.
- **Three bullets in four carry no trailer** (measured over this host's two caches with an
  approximate Python scan: 27.6% and 20.3% attributed; 8 of 41 scopes have none). Never
  render a session list without `attributed: K of N`.
- **Session ids are not one shape** (~3% non-uuid in the same scan). Compare byte-exact;
  never shape-check, lowercase or normalise.
- **A dot-prefixed DIRECTORY at the store root becomes a scope** for the token-file adapter
  (`internal/control/tokenfile/source.go:542-558` and `internal/store/load.go:185-248` skip
  only non-directories; only `snapshot.Build` skips dot names). Registration storage must be
  a regular file or live elsewhere — S3 must measure this RED before relying on it.
- **The pod never sees a scope read** — `recall`/`search` render the local cache and the
  client's only read request is the snapshot. A future reads phase cannot be pod-side
  observation; it records syncs.
- **Ruled out:** storing registrations in `internal/pgstore`. The pod is the surface that
  must serve them and it may not link a database driver (`internal/pgstore/pgstore.go:1-25`,
  `internal/depspolicy`). `via: measurement`
- **Ruled out:** shipping registrations inside the snapshot so the CLI could read them
  offline. The snapshot is byte-compared against the oracle by `tests/dualrun/`, so a Go-only
  member is a divergence by construction. `via: measurement`
- **Ruled out:** an orphan check inside `cairn doctor`. `doctor` is host- and
  credential-scoped (`internal/client/cli.go:80-84`) and three parity rows compare its stdout
  byte-for-byte with the oracle (`tests/parity/harness.py:680-687`); a Go-only check would
  red them or narrow them to exit-only. `via: measurement`
- **Ruled out:** treating "session id in a trailer but in no arc" as a finding. Roughly 700
  distinct writing sessions in one cache against arcs registered only from now on makes it a
  permanently-red check; it is reported as a coverage number instead. `via: measurement`
- **Ruled out:** keying arcs on the tooling's `repo_label`. It is the basename of the path
  handed in, so a worktree run would key the same arc under a different name; cairn's
  `DeriveScope` reads the git common dir and is worktree-stable
  (`internal/client/reposcope.go:60-75`). `via: measurement`
- **Ruled out:** adding new exit codes for the new verbs. Every outcome maps onto the existing
  printed contract, and a new constant would move two cross-client ledgers for nothing.
  `via: assumed` (a design choice; S2/S3 confirm it holds verb by verb)

- **SUPERSEDES the "regular file or live elsewhere" half of the dot-directory bullet above:**
  the operator chose ELSEWHERE (Q2). The journal lives outside the store tree and the pod
  refuses to start when `-arc-journal` resolves inside the store root. The adapter hazard
  itself is unchanged and still has to be measured RED in S3.
- **Ruled out:** a journal file at the store root (dot-prefixed or not), the plan's original
  no-deployment-change option. `via: doc` (operator decision Q2, recorded in the plan)
- **Ruled out:** compacting or deleting closed-arc registrations this phase. `via: doc`
  (operator decision Q7, recorded in the plan)
- **Ruled out:** an authenticated append-time write log in this phase; it is Deferred beside
  reads, and every result says trailers are self-reported. `via: doc` (operator decision Q6, recorded in the plan)

## How to verify
```bash
# clause 1: every slice by CONTENT on main (squash ≠ ancestry)
git fetch origin
for p in internal/touch internal/arcs internal/report/sessions.go internal/report/arcs.go \
         internal/report/arcscheck.go internal/ui/arcs.go tests/arcs/e2e.sh; do
  git cat-file -e origin/main:$p && echo "present $p"; done
# clause 2: the closing check and its positive control (≈ a few minutes; builds both binaries)
nix develop --command bash tests/arcs/e2e.sh              # want: passed=32 failed=0 expected=32
nix develop --command bash tests/arcs/e2e.sh --self-test  # want: sabotaged=3 caught=3
# clause 3 (after the journal mount + switch + a /handoff on this topic):
cairn arcs --repo /home/zach/workspace/cairn               # lists cairn-arcs-sessions
cairn arcs --repo /home/zach/workspace/cairn --check; echo rc=$?   # want 0
# the plan's load-bearing citations still hold (re-read before acting on any)
grep -n '^const attributionPattern\|^const attributionActorClass\|^const sessionClass' internal/write/revision.go
sed -n 542,558p internal/control/tokenfile/source.go   # storeDirs: no dot filter
go run ./cmd/cairn -verbs                      # includes the Go-only sessions/arcs/arc-show/arc-register
go run ./cmd/cairn -exit-codes                 # 13 rows; S5 left these UNCHANGED
# the gates a slice must keep green
go vet ./... && go test -count=1 ./...
tests/conformance/run_go.sh && python3 tests/conformance/suite.py run
python3 tests/leakscan.py --self-test && python3 tests/leakscan.py
```
