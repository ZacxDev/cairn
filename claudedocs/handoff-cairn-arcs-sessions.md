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

## State now
- ✅ **S0–S3 MERGED on `main`** (#182, #183, #184, #185; read off `origin/main` at `dda0c3d`:
  `internal/touch`, `internal/arcs`, `internal/report/{sessions,arcs}.go`, routes `sessions/`,
  `arcs/`, `arc/`, verbs `sessions`, `arcs`, `arc-show`, `arc-register`).
- 🔄 **S4 (UI) is PR #186**, branch `feat/s4-arcs-ui`, OPEN.
- 🔄 **S5 is the PR from branch `feat/s5-arcs-check-e2e`**: `cairn arcs --check` (a FLAG on the
  existing verb, served as `GET arcs/<scope>?check=1[&all_scopes=1]` — a mode of the existing head,
  so NO verb or route ledger moved) and `tests/arcs/e2e.sh` (32 assertions over both real binaries;
  `--self-test` proves 3 sabotaged builds each turn their named assertion RED). Wired into the `go`
  CI job. Exit codes are doctor's 0/9/10 via `report.ArcsCheckExit`; `cairn -exit-codes` unchanged.
- ⚠ **ONE DEPARTURE FROM THE PLAN, in S5:** "a member session that wrote nowhere the principal can
  see" is printed as a COVERAGE number, not exit 10 — members come from commit trailers and
  transcripts, so a member with no entry trailer is the normal state and a 10 would be permanent.
- ⚠ **Q2 NEEDS ONE DEPLOYMENT CHANGE THAT IS NOT IN THIS REPO** — the journal's own mount and
  `CAIRN_ARC_JOURNAL`. Until it lands the deployed routes answer `registrations-unconfigured`, and
  `cairn arcs --check` against the deployed pod exits 10.

## Next steps (ranked)
1. ~~**Operator reviews the plan PR and answers open questions 1 and 2.**~~ **CLOSED** — all
   seven answered and folded into the plan.
   forcing: user — closed.
2. **S0 — declaration plumbing** (`go_only` corpus field and both validators,
   `GO_ONLY_VERBS` with a three-operand check, `go_only` capability rows), exercised on
   synthetic tables with no route added. Size S.
   forcing: gate — every later slice adds a Go-only route or verb, and today's ledgers
   REFUSE one (`cases.py:337-344` refuses a corpus route the oracle lacks;
   `test_the_go_client_declares_EXACTLY_the_pythons_verb_set` refuses a Go-only verb).
3. **S1 — `internal/touch` + `write.ParseAttributions`**: derivation, coverage, rendering,
   the strip⇄parse seam test, a 10× synthetic-store benchmark. Size M.
   forcing: user — the operator asked for session↔scope resolution.
4. **S2 — the sessions surface**: `GET/HEAD sessions/<scope>`, the proposed `sessions` verb, corpus
   rows, pod⇄CLI byte identity, authz pairs. Size M.
   forcing: user — the operator asked for session↔scope resolution.
5. **S3 — the arc registry**: append-only journal at `-arc-journal`/`CAIRN_ARC_JOURNAL`
   (no default), with a startup REFUSAL when the path resolves inside the store root, shown
   RED first; `PUT/GET arc/<home>/<slug>`; `GET arcs/<scope>` listing `declared` and
   `inferred` arcs with the label; home-scope visibility; `unknown` status never shown as
   `open`; the proposed `arc` and `arcs` verbs; the merge rule. Size L.
   forcing: user — the operator asked for arc registration.
6. **S4 — UI**: scope-page section and `/arc` page, both route ledgers, uiaudit; renders the
   `declared`/`inferred` label and visibility from S3's renderer, and reads the journal from
   its own read-only mount. Size M.
   forcing: user — the operator asked for UI integration.
7. **S5 — `arcs --check` on doctor's 0/9/10 with no new exit constant, and
   `tests/arcs/e2e.sh`**, the closing check. Size S.
   forcing: gate — the closing condition names this command.
8. **T1 — the private tooling repo's `/handoff --confirm` calls the proposed `arc register` verb**,
   non-blocking, sending `open`/`closed` when it can and omitting status otherwise; plus the
   pin bump. Size S; a different repo's PR.
   forcing: user — the operator decided registration is pushed by `/handoff`.
9. **The journal mount in the private deployment repo** (the Deployment change Q2 needs).
   The operator's change; it gates live use of S3/S4 and the deployed half of the closing
   condition, not their merge.
   forcing: user — the operator chose a journal outside the store tree.

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
# the plan's load-bearing citations still hold (re-read before acting on any)
sed -n 44p internal/write/revision.go          # attributionFormat
grep -n '^const attributionPattern\|^const attributionActorClass\|^const sessionClass' internal/write/revision.go  # the grammar (split into its two classes by S1)
sed -n 542,558p internal/control/tokenfile/source.go   # storeDirs: no dot filter
go run ./cmd/cairn -verbs                      # 14 verbs, four of them Go-only
go run ./cmd/cairn -exit-codes                 # 13 rows; S5 must leave these UNCHANGED
# the leak gate, both controls
python3 tests/leakscan.py --self-test && python3 tests/leakscan.py
# the closing check (S5) — first prove it can go red, then run it
tests/arcs/e2e.sh --self-test                  # must print: SUMMARY e2e-self-test: sabotaged=3 caught=3
tests/arcs/e2e.sh; echo "rc=$?"                # must print SUMMARY e2e: passed=32 failed=0 expected=32, rc=0
```
