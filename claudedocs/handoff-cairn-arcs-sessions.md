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
  registers one arc with `cairn arc register`, and asserts `cairn sessions --scope
  alpha-notes` and `cairn arcs --scope alpha-notes` list both sessions and the arc with their
  coverage lines, while a principal without that scope gets the absent answer (the negative
  control) — AND, against the DEPLOYED pod, `cairn arcs --repo /home/zach/workspace/cairn`
  lists this arc (`cairn-arcs-sessions`), registered by `/handoff` itself (slice T1).
  ADDRESSED ⇒ arc CLOSED.

## State now
- 📄 **PLAN ONLY — NO FEATURE CODE EXISTS.** PR #180, branch `docs/plan-cairn-arcs-sessions`
  off `main` at `621b4e6`, carries the plan and this doc.
- ✅ **ALL SEVEN OPEN QUESTIONS ARE ANSWERED BY THE OPERATOR** and folded into the plan as
  Q1–Q7 under "Decisions taken"; the plan's "Open questions" section now reads none open.
  In one line each: home-scope visibility; journal at a separate path outside the store tree;
  `arcs --scope` lists `inferred` arcs beside `declared` ones; status `unknown` accepted and
  never rendered as `open`; `arcs --check` on doctor's 0/9/10; the authenticated append-time
  write log is a later phase; registrations kept forever, append-only, no compaction.
- ⚠ **Q2 NEEDS ONE DEPLOYMENT CHANGE THAT IS NOT IN THIS REPO** — the pod's data volume is
  mounted at the store root, so the journal needs its own mount (read-write in the pod,
  read-only in the UI) and `CAIRN_ARC_JOURNAL`. The plan spells it out; nothing in the private
  deployment repo was edited. Until it lands the routes answer `registrations-unconfigured`.
- ✅ Both leakscan commands exit 0 with the revised plan in the tree.

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
4. **S2 — the sessions surface**: `GET/HEAD sessions/<scope>`, `cairn sessions`, corpus
   rows, pod⇄CLI byte identity, authz pairs. Size M.
   forcing: user — the operator asked for session↔scope resolution.
5. **S3 — the arc registry**: append-only journal at `-arc-journal`/`CAIRN_ARC_JOURNAL`
   (no default), with a startup REFUSAL when the path resolves inside the store root, shown
   RED first; `PUT/GET arc/<home>/<slug>`; `GET arcs/<scope>` listing `declared` and
   `inferred` arcs with the label; home-scope visibility; `unknown` status never shown as
   `open`; `cairn arc …`/`cairn arcs`; the merge rule. Size L.
   forcing: user — the operator asked for arc registration.
6. **S4 — UI**: scope-page section and `/arc` page, both route ledgers, uiaudit; renders the
   `declared`/`inferred` label and visibility from S3's renderer, and reads the journal from
   its own read-only mount. Size M.
   forcing: user — the operator asked for UI integration.
7. **S5 — `arcs --check` on doctor's 0/9/10 with no new exit constant, and
   `tests/arcs/e2e.sh`**, the closing check. Size S.
   forcing: gate — the closing condition names this command.
8. **T1 — the private tooling repo's `/handoff --confirm` calls `cairn arc register`**,
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
sed -n 54p internal/write/revision.go          # attributionPattern
sed -n 542,558p internal/control/tokenfile/source.go   # storeDirs: no dot filter
go run ./cmd/cairn -verbs                      # today: 10 verbs, none Go-only
go run ./cmd/cairn -exit-codes                 # the codes the new verbs must reuse
# the leak gate, both controls
python3 tests/leakscan.py --self-test && python3 tests/leakscan.py
# after S5: the closing check
tests/arcs/e2e.sh; echo "rc=$?"                # must print rc=0
```
