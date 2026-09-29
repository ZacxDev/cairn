# Handoff: cairn-deploy-currency — 2026-09-29

## Run this first — the index, one command
```bash
cairn recall --repo "/home/zach/workspace/cairn"
```
Terse pointers this doc does not carry, curated by past sessions and outliving it.
🔴 RECALL, NOT LIVE OBSERVATION — every line is a pointer to VERIFY, never a current
reading, and it may describe a gotcha already fixed. `scope-absent`/`scope-empty` means
nothing is recorded yet: ordinary, not an error, and not a clean bill of health.
Non-blocking: if it exits non-zero, print the stderr line and carry on.

## Goal
Make "what is deployed" and "what is on `main`" observable rather than remembered, and
prove the one deployed feature that has never been exercised. Both predecessor arcs
(`handoff-cairn-control-plane.md`, `handoff-cairn-next-phase.md`) are CLOSED; this is the
new arc their leftovers belong to, not another round of either.

- **closing-condition:** `check` — (a) an instrument exists that FAILS when a deployed
  cairn image's commit is behind `origin/main` (a CI job, a Prometheus rule, or image
  automation) and it has been watched going RED on a deliberately stale pin; AND (b) the
  browser surface's session table has been written — `sessions.n_tup_ins` read as 0 before
  and ≥1 after one real sign-in. A later session runs the instrument's own red-control and
  the two psql reads. ADDRESSED ⇒ arc CLOSED.

## State now

- Branch: `main`, clean, at `e8839d9`. No uncommitted work anywhere: 54 registered
  worktrees swept, instrument positive-controlled, **2 dirty and neither holding anything
  unsaved** (one byte-identical to `main`, one rescued — see below).
- ⚠ **NO TASK-BOARD FIELD — AN UNKNOWN, NOT A MEASURED ABSENCE.** The resolver exited
  **5**; an unknown session id answers 200 with an EMPTY ARRAY, so that zero cannot
  distinguish "touched no task" from "wrong id". None written, and none created.
- ✅ **BOTH PREDECESSOR ARCS ARE CLOSED, RE-MEASURED RATHER THAN READ OFF THEIR DOCS.**
  - control-plane: `python3 -m pytest tests -q` **2357 passed / 0 failed** (528.8 s),
    `go test ./...` **21 ok / 0 FAIL**, `nix eval` resolving `default` and `cairn-go` to the
    **identical** store path, and both named tests present
    (`internal/control/matrix_test.go:85`, `internal/ui/sharing_test.go:416`). ⚠ The doc's
    counts were 2303 and 20; both grew with `#146`.
  - next-phase: `#148` MERGED as `e8839d9`; all four cards `complete`; each feature verified
    **by content** on mainline with a nonexistent marker returning **0** as the negative
    control. Recorded by a peer session's **PR #150**.
- ✅ **BOTH DEPLOYED PODS ARE NOW AT `origin/main` — 0 COMMITS BEHIND, AND IT TOOK TWO
  DEPLOYS.** Deployment repo `ab2a59ab5` (→ `b2b54e4`) then `ccdcd13e4` (→ `e8839d9`).
  Verified by DIGEST against values resolved from ghcr **before** each push:
  `cairn-ui sha256:518824b9…c939`, `cairn-store-go sha256:8a83e9cc…ac0c`, both pods
  `ready=true restarts=0`. Runtime symptom exercised, not just the rollout: `GET /` → **401**
  to a non-browser and **303 → /sign-in** to a browser `Accept`; `/sign-in` → **200**
  carrying both a `name="token"` field and a `sign-in/github` action, so the provider is
  ARMED; and the store API served a real live fetch (322 entries) on the new image.
- 🔴 **ONE OF THIS SESSION'S OWN FINDINGS IS RETRACTED, AND THE RETRACTION IS THE MOST
  VALUABLE THING HERE.** I reported that the browser surface's trusted-proxy allowlist was
  stale and that every public caller therefore shared one lockout bucket — a 15-minute
  global sign-in outage available to anyone. **There is no such exposure.** Full block under
  `Open investigations`; do not re-derive it.
- ✅ **A BULLET THAT EXISTED IN NO COMMITTED FILE IS RESTORED — PR #149.** `#140`
  (`a035483`) deleted an operator-decision bullet from the control-plane doc while the
  archive half of that move sat unstaged in another session's scratchpad worktree.
- ⏳ **THREE PRs OPEN, none merged by this session:** **#149** (the rescue, +17/−0), **#150**
  (a peer's arc-closing bookkeeping, +125/−59), **#151** (mine, +76/−0 — the retraction and
  four defects, deliberately narrowed to zero overlap with #150 so merge order is free).

## Open investigations — live diagnosis state

### ❌ RETRACTED IN FULL — "the trusted-proxy allowlist is stale, so every caller shares one lockout bucket"
- as-of: 2026-09-29
- **Symptom + exact repro:** not a defect — a withdrawn claim, recorded so nobody re-derives
  it. Repro of the wrong reading: enumerate every pod's `status.podIP` and look for the
  address the browser surface's `CAIRN_TRUSTED_PROXIES` names.
- **Observed (with values):** the allowlisted single-host prefix is held by **no pod** — 797
  pods enumerated, exact match on the podIP field, with the browser pod's own IP returning a
  row as the positive control. That reading is TRUE. `via: measurement`
- **Ruled out:** ❌ **RETRACTED — the conclusion drawn from it**, namely that
  `netid.PeerIsTrusted` is false for every request, that `CF-Connecting-IP` is therefore
  never read, and that per `netid.ResolveClient`'s documented untrusted-peer branch (bucket
  = the peer's own address) five failed sign-ins from anyone would be a global sign-in
  outage. **The peer the pod sees is not a pod.** It is the `hostNetwork` gateway, whose
  source address toward pods on that node is the `cilium_host` address — a node-level
  address that appears in no pod's `status.podIP` at all. The API deployment's own comment
  records deriving the value exactly that way, with `ip route get <pod-ip>` after a gateway
  roll. `via: code`
- **Ruled out:** that any global-lockout exposure exists. Re-measured the way the value was
  derived — `kubectl -n <gw-ns> exec ds/<gateway> -c <proxy> -- ip route get <pod-ip>` — the
  route leaves via `cilium_host` and its `src` is **the allowlisted address, exactly**. So
  the header IS read and a caller can only ever lock out ITSELF, which is what `netid`'s
  package doc claims. `via: measurement`
- **Leading hypothesis:** none. The claim is withdrawn.
- **Next probe:** none for this claim. 🔴 **The reusable lesson is the point: ask what your
  instrument can REPORT before reading its output as an elimination.** This repository
  already records that sentence about its leak scanner, and it was hit again inside one
  session in a new shape — a 797-row enumeration with a working positive control is exactly
  the evidence that feels conclusive while answering a different question than the one
  asked. If the gateway is ever rebuilt, re-derive with `ip route get`, never by looking for
  a pod.

### The browser surface's session table has never been written to
- as-of: 2026-09-29
- **Symptom + exact repro:** not a defect yet — a deployed feature with zero exercise.
  `kubectl -n subsystem-store exec sts/cairn-ui-postgres -- psql -U cairn_ui -d cairn_ui -c
  "select relname,n_tup_ins,n_tup_del,n_live_tup from pg_stat_user_tables order by relname;"`
- **Observed (with values):** `sessions` → `n_tup_ins=0 n_tup_del=0 n_live_tup=0`, cumulative
  since the postmaster came up at the 28(d) cutover (`pg_stat_database.stats_reset` is
  **NULL**, so the counter spans the whole life of the database).
  `schema_migrations` → `n_tup_ins=1`, which is the **positive control**: the counter moves,
  and this is the database the application migrated. `invites` → 0. The table's columns are
  `digest, kind, principal, issued_at, expires_at`. The startup log carries both the
  `$CAIRN_UI_SESSION_FILE … is IGNORED` line and
  `state sessions in postgres, invitations in postgres`. `via: measurement`
- **Ruled out:** that rows were written and later expired away — `n_tup_del` is 0 and
  `n_tup_ins` is cumulative, so a write-then-delete would still show. `via: measurement`
- **Ruled out:** that the deployed image lacks the code — the running binary PRINTS
  `sessions in postgres`, and the pinned tree carries `CAIRN_UI_DB_DSN` in
  `cmd/cairn-ui/main.go` plus `internal/pgstore`. `via: measurement`
- **Ruled out:** that the operator's own store credential can drive the probe. One
  credential-form POST with a correct `Origin` returned **401** and wrote nothing (correct
  for a refusal — the counter stayed 0). The store API token is a DIFFERENT credential from
  the browser surface's, whose credential was issued into the control journal at seeding.
  `via: measurement`
- **Leading hypothesis:** nobody has signed in since the cutover. A startup banner is
  configured-state, not evidence the feature works — which the 28(d) block said in advance,
  and which this reading is the first to test.
- **Next probe:** one real sign-in with the credential from the control journal, reading the
  counter **before and after** so the move is the evidence rather than the end state.
  ⚠ Budget: the lockout is 5 failures / 900 s bucketed on the client key, and **one of those
  five is already spent** by the 401 above. The allowlist is correct (see the retraction), so
  a further failure locks out only the caller — not everybody.

### The INSTALLED client carries none of the three features this repo shipped, and no card, claim or arc owns it — this arc's client-side half
- as-of: 2026-09-29
- **Symptom + exact repro:** `cairn recall --help` on this host lists neither `--ref-to` nor
  `--tag`. `readlink -f "$(which cairn)"` → a `/nix/store/…-cairn-5dfc11a/bin/cairn`.
- **Observed (with values):** installed revision **`5dfc11a`**, dated **2026-09-23**, is
  **63 commits** behind `origin/main` and `merge-base --is-ancestor` says it is an ancestor of
  **all three** feature squashes (`5c59169` refs, `94ecb7e` tags, `e8839d9` requirements) — so
  it predates every one. `--ref-to` and `--tag` each grep **0** in `--help`. It is the PYTHON
  client (argparse usage; `-verbs` refuses at exit 2), while the flake's `default` is now the
  Go one. It is installed by **home-manager as a `home.file` COPY** — `readlink -f` terminates
  in `…-home-manager-files/…`, so editing anything does nothing and it needs a switch — and the
  revision comes from the other repo's `flake.lock` node `cairn`, locked at `5dfc11a2ac12`.
  `via: measurement`
- **Ruled out:** the deployed PODS as the gap. They carry all three, measured behaviourally
  against the live pod with a control on each: `?ref-to=zzz-no-such-ref` → **400** carrying
  662's own refusal (*"not a well-formed `<system>:<id>` ref"*); `?tag=<bogus>` →
  **`status=tag-absent`** with a real narrowing sentence (*"0 of 13 entries … carry it"*); and a
  `## Requirements` section written into a real entry → index row **`🔴 2 REQ OPEN ✅ 1 REQ
  MET`**, the counts matching the content exactly. That is a SECOND, INDEPENDENT instrument
  agreeing with this arc's digest verification — behaviour where that read identity.
  `via: measurement`
- **Ruled out:** this arc already covering it. Closing condition (a) is about a deployed
  **image's** commit; grepping this doc for `flake.lock`, home-manager, "installed client" and
  `5dfc11a` returns **empty**, and none of the five ranked steps names it. No task-board card
  matches, and `claim-work --list` holds no claim on it (`cairn-pods-renderer-lag` is released).
  `via: measurement`
- **Leading hypothesis:** nothing is broken — the pin was simply never bumped. It is 6 days old
  and three user-facing features have landed behind it. `via: assumed`
- **Next probe:** in the other repo,
  `python3 -c "import json;print(json.load(open('flake.lock'))['nodes']['cairn']['locked']['rev'])"`
  then `nix flake update cairn` and a switch. Expect `scripts/tests/test_cairn_flake_pin.py` to
  gate it: it pins the whole SEAM as one relationship (input → outputs argument →
  `extraSpecialArgs` → `nix/home.nix`'s module header → the deploy line), plus the half of the
  split that must NOT move to the package, plus the `CAIRN_MIRROR_ROOT` export whose absence
  silently downgrades a `doctor` check from PASS to NOT-RUN. 🔴 **Decide Python-vs-Go default
  BEFORE opening it** — taking `packages.default` flips `-verbs`/`-exit-codes` from exit 2 to
  exit 0, which is a public-surface change and the operator's call, not a side effect of a bump.

## Next steps (ranked)

🔴 **NUMBERING IS STABLE — a rank is half a `claim-work` slug**, and `claim-work` comes
BEFORE you act. ⚠ **And run `gh pr list --state open` ANYWAY: a claim answers "may I", never
"is it done".** Measured this session — rank 4 of the next-phase doc returned **rc 12,
already yours**, while 21 files of finished, twice-audited work sat in an open PR.

1. **Merge the three open PRs — #149, #150, #151 — in the cairn repo.** All three are
   docs-only. #151 is `+76/−0` and cannot textually conflict with #150's `State now`
   rewrite; #149 touches only the archive. Offer round 0 of the audit ladder on #149 and
   #151 and skip the nine correctness axes: no code paths are touched.
   forcing: gate — three unmerged docs PRs are the `stranded-docs` shape one `checkout` from
   loss, and #149 exists precisely because that shape already cost a bullet.
2. **Build the deployed-artefact currency instrument** — the (a) half of this arc's closing
   condition. Files: a new CI job or Prometheus rule; the deployment repo's
   the deployment repo's `subsystem-store` app directory carries the two `image:` pins it must
   read (`deployment.yaml` for the API pod, `ui-deployment.yaml` for the browser surface). 🔴 **Watch it go RED on a deliberately stale pin before believing it** — and
   note that "both pods carry the same tag" is NOT the check: they were equal to each other
   and both stale, twice, which is exactly how this went unseen.
   forcing: regression — the gap re-opened within minutes of being closed, twice in one
   session, and no gate in either repository can see it.
3. **Prove the session table is written** — the (b) half. Needs the browser surface's own
   credential, which is the operator's. Claim `cairn-ui-session-store-probe` is HELD and
   deliberately not released.
   forcing: user — the operator holds the only credential that can run it.
4. **P8 — retire the Python oracle.** Carried over from the control-plane arc unchanged:
   closing condition is a real read AND a real write against the live pod from **two distinct
   hosts**, recorded, AND no open defect naming the Go client or `packages.default`.
   **BACKSTOP: not done by 2026-11-01 ⇒ P8 opens anyway and the residual risk is accepted
   EXPLICITLY, in writing.**
   forcing: deadline — the 2026-11-01 backstop, set by the operator.
5. **Fix the base-clone write guard** — carried over as the control-plane doc's rank 34, and
   it reproduced a **fourth** independent time this session: it refused a commit in a LINKED
   WORKTREE while naming the base clone, having resolved the repo from `$PWD` rather than
   from the command's `-C` target. The premise was proved false on every axis before each
   override (`--show-toplevel`, a 63-byte `gitdir:` `.git` FILE, a per-worktree git dir, a
   feature branch, the base clone still on `main`). **Closing condition:** the guard admits a
   linked worktree AND reads `-C`, with a test that a real base-clone write is still refused.
   forcing: gate — a guard whose diagnosis is reliably about the wrong repository trains its
   own bypass, which is the permanently-red-gate failure wearing a different hat.

## Defects (batched)

- 🔴 **THE DEPLOYED RENDERER GOES STALE ON EVERY `internal/report` MERGE AND NOTHING
  OBSERVES IT.** Rank 2 owns the fix; this entry owns the measurement. Both pods sat at
  `bcfb60a`, agreed with each other, and were one code commit behind `main` across `#146`.
  Bumped to `b2b54e4`; `#148` merged **while that was reconciling** and touches
  `internal/report/{entry,prose}.go`, so it re-opened. **Pod-to-pod agreement is not the
  property the byte-identity gates hold — pod-to-client is, and two equal tags are equally
  stale.**
- 🔴 **`#140`'s "0 lost, 0 duplicated" WAS TRUE OF THE BULLETS IT MOVED AND SILENT ABOUT ONE
  IT DELETED.** Closed by **#149**. **An eviction's safety property must be measured against
  the COMMIT, never against the working tree that produced it.**
- ⚠ **`grep -c` ON A PHRASE THAT WRAPS ACROSS A NEWLINE ANSWERS A FALSE 0, AND IT COST A
  WRONG CONCLUSION HERE BEFORE THE CONTROLS CAUGHT IT.** Flatten with `tr '\n' ' '` before
  believing an absence in prose. Two separate false zeros this session.
- ⚠ **AN UNBRACED `$ref:` IN zsh ATE A GIT REF AND RETURNED A CONFIDENT WRONG 0.**
  `git show $ref:path` with `a035483^` silently failed under `2>/dev/null`, reading as "the
  bullet was never there". Brace it (`${ref}`) and quote the pathspec.
- 🟡 **FOUR FILES ARE NOT `gofmt`-CLEAN AND NOTHING GREPS IT** —
  `internal/client/{anchor_test,exit,options}.go`, `internal/control/tokenfile/source.go`.
  Pre-existing; re-confirmed. Carried from both predecessor docs.
- 🟡 **`AGENTS.md` + `CLAUDE.md` PASS THEIR GATE BY ONE BYTE.** Re-measured on `main`:
  `MAX_BYTES = 32_500`, `MIN_HEADROOM_BYTES = 900`, files **31,332 + 267 = 31,599**, so
  headroom is **901 against a 900 floor** — green, and the next edit of any size reddens it.
  Unchanged by `#146` touching `AGENTS.md`. Filed as card 682 on the next-phase doc.

## Gotchas / decisions / dead-ends

- **Decision (operator, this session): deploy the image bump straight to the deployment
  repo's `trunk`** — where commit IS deploy — rather than via a PR. Two deploys resulted,
  because `main` moved mid-reconcile; the second was the same approved step re-targeted, not
  a new one.
- **Decision (operator, this session): drive the session probe with the operator's existing
  store token.** Measured insufficient — it is a different credential namespace (401). The
  browser surface's credential is the one in the control journal.
- 🔴 **A ROLLBACK TARGET IS A CLAIM WITH A SHELF LIFE, AND THE DEPLOYMENT REPO SAYS SO IN
  THE FILE.** Both pins' rollback comments were re-pointed at the actual predecessor in the
  same commit that moved `image:`. The UI pin had no rollback line at all and now has one.
- 🔴 **THE SWEEP CAUGHT A DUPLICATE AT THE SECOND WINDOW, AND THE LOCK COULD NOT HAVE.** The
  pre-work sweep was clean; a peer's #150 appeared while this session was writing the same
  bookkeeping, and the sweep run immediately before `gh pr create` is what saw it. The
  overlapping half was then withdrawn so #150 owns it alone. **Run the sweep at both
  moments, and remember rc 12 is the one return value that suggests you have nothing to
  check.**
- ⚠ **`rollout status` AND A FLUX RECONCILE ARE DIFFERENT CLAIMS — reconcile the SOURCE,
  then the Kustomization, then read `imageID`.** Followed here; the source lagged the
  Kustomization on the first pass and the applied revision was read back explicitly both
  times.
- ⚠ **A MOVED IMAGE DIGEST IS NOT A CHANGED SERVER** — the Go binary's nix store path embeds
  the short rev, so every commit to `main` produces a new digest even when no Go code
  changed. Recorded in the deployment repo; restated because a digest diff is tempting to
  read as evidence.
- 🔴 **THE PUBLIC REPO'S LEAK RULE BIT THE BLOCK DESCRIBING THE NETWORK FINDING.** The
  retraction above first spelled the allowlisted address in order to explain it. Every
  address is now named by ROLE, checked mechanically against the diff (0 matches).
  **An example that IS the thing it forbids is the thing it forbids.**

- 🔴 **A DEPLOYED-CURRENCY ARC HAS A CLIENT-SIDE HALF, AND IT IS THE HALF NOBODY IS LOOKING
  AT.** "What is deployed" naturally reads as pods and images; the artefact an operator actually
  TYPES is pinned separately, by a different repo, through a different mechanism (home-manager
  `home.file`, not an image tag), and it can be six days and three features behind while every
  pod is at `origin/main` and every currency check is green. The full measurement is the Open
  investigations block above. **Any instrument built for closing condition (a) should be asked
  whether it can see the client at all** — as specified it cannot.
- 🔴 **`x-store-revision: unknown` — THE HEADER EXISTS AND NOTHING POPULATES IT, MEASURED.** The
  pod already answers a revision header on every read and its value is the literal string
  `unknown`, so you cannot ask a running pod which code it is. There is **no `/version` route**
  either — the read heads are exactly `recall`, `search`, `snapshot`. That is why establishing
  whether a pod carried a feature needed a behavioural probe rather than one `curl`. It is also
  a ready-made home for this arc's instrument: populating that header at build time makes pod
  currency a one-request check instead of a deploy-repo archaeology exercise.
- 🔴 **THE CLOSED `next-phase` ARC'S RANK 9 IS CONFIRMED LIVE, WITH A PAIRED CONTROL — and it is
  now on real content.** Identical `OPEN:` marker text: under `## Requirements` the validator
  reports **`0 declared`**; moved verbatim under `## Nuance / work-history` it is **found**. Only
  the SECTION differs, so the scoping is the mechanism, not the spelling. A whole-scope run
  agrees (13 entry files, still only the one pre-existing declared `OPEN:`). So a requirement's
  open state reaches **no** validator surface. Recorded as an `OPEN: (inferred)` requirement on
  the `cairn/report` store entry, which is self-demonstrating: the bullet describing the
  blindness is itself invisible to the check that would report it.
- 🔴 **A `## Requirements` BADGE LIVES ON THE INDEX VIEW, NOT ON A `--ref` READ — AND PROBING THE
  WRONG ONE READS AS "THE FEATURE IS ABSENT".** A `--ref` read renders the section's body
  verbatim (bodies always are) and emits **no** badge, so `grep REQ` there returns 0 on a pod
  that fully supports it. Use the index/list view, and pair it with a badge-rendering control
  (assert some OTHER badge appears) so a zero cannot mean "badges are off everywhere".
- ⚠ **#150 WAS MERGED 16 MINUTES BEFORE ITS OWN CHECKS SETTLED** — merged 22:43:22Z, all 8 green
  at 22:59:43Z, no auto-merge. The outcome was benign and the head was already 6-of-8 green, but
  what a gate buys is the ORDERING of the evidence, not the outcome, and that is what was spent.
  Recorded, not relitigated.
- ⚠ **zsh: AN UNQUOTED MULTI-PATH `$3` INSIDE A FUNCTION IS **ONE** PATHSPEC, NOT TWO.** zsh does
  not word-split, so `git grep -l "$1" <rev> -- $3` with `$3="internal/api internal/report"`
  matches nothing and returns a confident **0 for every row** of a check table. Three features
  read as "integration ABSENT" until a positive control caught it. Use `${=3}` or a real array.
- ⚠ **`clawgatectl task list` IS NOT A VERB — IT IS `ls`** — and the wrong spelling printed
  nothing and exited without an error, which reads exactly like "no open tasks". Same shape as
  grepping JSON for `^status`: the tool answered about itself, not about the board.

## How to verify

🔴 **READ EVERY STATUS OFF THE COMMAND, NEVER THROUGH A PIPE** — a `| tail` owns the exit
status, which this arc's predecessor paid for four times.

```bash
# the two closed arcs, re-measurable
nix develop -c bash -c "cd /home/zach/workspace/cairn && python3 -m pytest tests -q -p no:randomly"   # 2357 passed
nix develop -c bash -c "go test ./... > /tmp/t.out 2>&1; echo rc=\$?; grep -c '^ok' /tmp/t.out"        # 21 ok
nix eval --raw .#packages.x86_64-linux.default.outPath   # == .#packages.x86_64-linux.cairn-go.outPath

# deployed currency — the (a) half of the closing condition, by hand until rank 2 lands
DEP=$(kubectl -n subsystem-store get deploy cairn-ui \
  -o jsonpath='{.spec.template.spec.containers[0].image}' | sed 's/.*sha-//')
git -C /home/zach/workspace/cairn rev-list --count ${DEP}..origin/main    # must be 0

# the (b) half — read the counter BEFORE and AFTER one sign-in; the MOVE is the evidence
kubectl -n subsystem-store exec sts/cairn-ui-postgres -- psql -U cairn_ui -d cairn_ui \
  -c "select relname,n_tup_ins,n_live_tup from pg_stat_user_tables order by relname;"
```

**Verify a deploy by DIGEST, never by the `image:` field:** resolve the tag on ghcr
anonymously *before* the push, then read `.status.containerStatuses[0].imageID` back and
compare. A tag equal to what you wrote proves only that you wrote it.

**The leak gate must pass before any push, and read its CONTENT not a pipe's rc:**
```bash
cd <a fresh worktree>   # the base clone exits 2 on untracked `result` symlinks and agent worktrees
python3 tests/leakscan.py --self-test > /tmp/ls.out 2>&1; echo rc=$?
python3 tests/leakscan.py > /tmp/l.out 2>&1; echo rc=$?; tail -3 /tmp/l.out
```
