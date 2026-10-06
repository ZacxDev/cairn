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
- ✅ **VERDICT: ADDRESSED — THE ARC IS CLOSED.** All three clauses measured: (1) S0–S5 merged
  (#182 `0572b35`, #183 `21d8782`, #184 `c862970`, #185 `dda0c3d`, #186 `4714652`, #187
  `7528c74`; plan #180 `107b389`); (2) CLAUSE 2 MEASURED on a clean detached checkout of
  `origin/main` at `7528c74`: `tests/arcs/e2e.sh` 32 PASS / 0 FAIL, `--self-test` sabotaged=3
  caught=3; (3) the deployed personal pod lists `cairn/cairn-arcs-sessions`, registered by this
  doc's own `/handoff` from a worktree. Everything below is post-close rollout, not part of the
  closing condition.
- ✅ **BOTH INSTANCES RUN THE PHASE.** Personal instance: deployment repo `trunk` `a49259c` (journal
  volume, both images `ffa0eca` → `ea9cfa7`). CLIENT instance: the client infra repo's `trunk`
  `e2748ec` — same change on `linstor-nvme`, images pinned by tag AND digest (store
  `sha256:ba216e99…`, ui `sha256:1e239549…`); both pods running those digests, 0 restarts.
  Live: `cairn arcs --scope <a client-routed scope>` went **HTTP 404 → `no-arc-registered`**,
  `--check` rc 0; the pod's audit lines show both at `result=200`. Rollback on either: both
  image lines back to `ffa0eca`; the journal volume is inert to that image.
- ✅ **THE JOURNAL IS BACKED UP ON BOTH.** Personal: `cairn-ui-backup` stages it under `arcs/`
  (deployment repo `99f43c4`); a manual run logged `arcs: lines=15`, `verify: files=5 missing=0
  differs=0`, `OK … restored and verified`, and FAILS if `arcs.jsonl` is absent. Client:
  `cairn-backup` step 7b uploads `arcs-<stamp>.jsonl` beside the archive with the same
  stat/download/sha256 round trip; a manual run passed (144 entries restore-checked) with
  `arcs: absent` — nothing has registered there yet, so **the upload branch has not run live**.
- ✅ **THE INSTALLED CLIENT HAS `arcs --check`** — the tooling repo's pin bumped again to
  `ea9cfa7` (its #2072, shipped to both hosts): `cairn arcs --repo /home/zach/workspace/cairn
  --check` → `arcs-check-clean`, rc 0, `closed 1`. Retires the "installed client lacks
  `--check`" caveat.
- ✅ **UI VERIFIED SIGNED-IN on the personal instance** (operator's browser, an owned background
  tab, never raised): scope page renders "Sessions that wrote here" (`scanned: 14 of 14`,
  `attributed: 72 of 102`, 36 sessions) and "Arcs that touched this scope" linking the arc; the
  arc page renders `arc-found`, status closed, closing check, 1 member.
- ⏳ **UI NOT YET VERIFIED on the client instance** — neither browser profile is signed in to its
  credential form (both pass the SSO proxy, then stop at cairn's own sign-in).
- ⚠ **Registrations are already arriving from other sessions**: the personal journal held 15
  records at the backup run, including an `inferred` arc on a second scope.

## Next steps (ranked)
1. **Verify the UI on the client instance** once the operator signs in there: a scope page's
   two cards (sessions listed; arcs `no-arc-registered` until a client-routed `/handoff`).
   forcing: user — the operator asked for production validation on both instances.
2. **Confirm the client backup's upload branch on its first real run**: after the first arc
   registers on the client instance, the next `cairn-backup` log must show `arcs: verified
   <prefix>/arcs-<stamp>.jsonl` and `LAST_SUCCESS` must carry `arcs=<n> lines…`.
   forcing: user — the operator asked for the journal to be included in the backup job.

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

- 🔴 **On `linstor` a claim-level `readOnly: true` BREAKS co-mounting**: the CSI driver mounts the
  block device `-o ro`, which fails beside the rw holder on the same node (the client infra repo
  records a job stuck Pending 24 min). The personal-instance pattern (claim-level readOnly on the UI) would
  have walked straight into it; the client rollout enforces read-only on the CONTAINER mount only.
  via: doc
- **The stored credential files under `~/.config/subsystem-store/` are NOT valid browser
  sign-in credentials** — the UI refused both (`That credential was not accepted`); stop after
  two (lockout 5/60s). Signed-in UI checks go through the operator's own browser.
- **The DR restore verifier stays on the older Go image on purpose**: its test pins the image
  FAMILY (`cairn-store-go`) and declines intra-Go revision skew by design.
- ⚠ **zsh does not word-split** — `set -- $K` on a two-word value silently skipped a `flux
  reconcile` and the "rolled out" read that followed was the OLD Deployment. Use literal names.

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
