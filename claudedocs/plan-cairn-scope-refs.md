# Plan: scope-level code sources, and an auditor that checks entries against them

This is a DESIGN, not a description of anything built. None of it exists yet.

**Where the citations point.** Every claim about today's behaviour carries a `file:line` read off
`origin/main` at **`0355c7a`** (PR #209 merged). Re-read before editing. A claim that was
MEASURED rather than read says so where it appears, and names who measured it: this plan's
author, or an audit round.

Examples are synthetic:
- scopes are `alpha-notes` and `beta-notes`;
- repositories are `example-org/example-repo` (and `example-org/example-mono` for a monorepo) on
  `github.com` or `git.example.com`;
- the one real repository an example may name is this public one.

Operator decisions are PARAPHRASED, never quoted (`AGENTS.md`).

**How proposed verbs are spelled.** They are written without the CLI prefix: "the proposed
`audit-refs` verb", "the proposed `sources` verb". `tests/test_no_scrubbed_identifiers.py`
refuses any backticked CLI-prefixed verb that no client registers yet, and it is right to: a plan
must not read as an instruction to run a command that does not exist.

**The plugin plan.** `claudedocs/plan-cairn-plugins.md` exists only on the unmerged branch
`origin/zach/plan-plugins` (PR #208), so it is cited by decision number.

**Revision history.**
- *Revision 1* (`71e2823`) recommended a separate journal outside the store.
- *Revision 2* (`9dc1e13`) recorded O4: the scope's `README.md` front matter. It applied round 0
  (D1–D5) and round 1 (F1–F8).
- *Revision 3* records **O5, which supersedes O4**: an append-only journal outside the store
  tree, on the arc registry's pattern.
  - The UI is the only writer.
  - Records are keyed by scope ID.
  - The pod mounts the journal read-only and serves one Go-only GET route, which the auditor
    reads.
  - Every piece of README machinery is deleted: the front-matter reader and splice,
    `SetScopeSources`, the Python-ignores test and the parity-world README.
  - Round 2's findings (R2-1 to R2-5) are applied.
- *Revision 4* (`e7a7238` → this) applies round 3 (`e7a7238`'s delta audit):
  - **Records are keyed by the NORMALISED SCOPE NAME, not by scope ID.** The UI and the pod resolve
    scope IDs in DIFFERENT ID spaces, so ID keying could never be read back (🔴1, decision 4). This
    departs from O5's keying detail, so it is flagged for the operator as Q17.
  - Tags are kept out of reachability (🟡1).
  - T9 lists every file the UI writes (🟡2).
  - The journal's three states are spelled out, and a broken journal answers 503 (🟡3).
  - The backup treats an absent journal as a pass (🟡4).
  - The four-places ledger is corrected (🟢1), and the nits are folded in.
- *Revision 5* (`52f600c` → this) applies round 4:
  - **one wire answer for a broken journal**: arcs' `store-unreachable` 503, mapped to exit 10 by
    the auditor;
  - T9 pin 1 covers every PACKAGE-LEVEL `os` mutator; revision 6 narrowed this from "every";
  - "Four" parts;
  - three wording nits.
- *Revision 6* (`9948883` → this) applies round 5, narrowing two sentences:
  - pin 1 is scoped to PACKAGE-LEVEL `os` functions, and methods are added to what it cannot see;
  - the exit-3 and exit-10 mechanisms are cited correctly.
- *Revision 7* (S1's branch) records the operator's answer to **Q17: key by the normalised scope
  NAME**. O5's "keyed by scope ID" is superseded by that answer, not by the agent.

  Removed decisions keep their numbers, marked REMOVED, so references stay stable.

## Goal and premise

No part of cairn currently knows which code a scope is about, so nothing can mechanically test
whether an entry's paths, commits or pull requests still hold upstream. The operator wants such a
test to run unattended. In the operator's terms, each scope names its **code source**: the
repositories and branch it describes. An auditor then compares every entry's checkable claims
against that remote and flags the ones that went stale. The declaration is edited in the browser.

There are two outcomes, and the second is worthless without the first.

1. **A code-source declaration per scope.** It is machine-readable, validated, editable only by
   whoever administers the scope, and has a change history.
2. **A deterministic auditor.** For every entry in a scope, it reports which checkable claims hold
   at the fetched commit, which do not, and which it could not check. It never folds those three
   together, and it never edits an entry.

### What would make this unnecessary

Drop the work, or the named half of it, if any of these holds:

- **The existing out-of-tree audit is enough.** The operator's private tooling already checks
  `RESOLVED <sha>` markers and `## Pointers` paths against LOCAL clones (STEP 1), and it infers
  scope-to-clone from directory names. If every clone is always on the auditing host and always
  fetched, a `git fetch` before that audit buys most of what this plan buys.
- **Nobody acts on the findings.** An audit nobody reads is a permanently red gate. B3 (a census)
  and Q4 exist so that the cheap half lands first and is judged.
- **Scopes rarely document a single repository.** For those, the declaration is optional. An
  undeclared scope exits 10, so it is never reported clean (decision 8).

### closing-condition

- **closing-condition:** `check`. Four mechanical parts, all required:
  1. Slices S1–S5 are MERGED on cairn `main`, verified by content, not by ancestry.
  2. **`tests/refaudit/e2e.sh` exits 0 on `main`.** It runs in the `go` CI job, which sets
     `CAIRN_GIT_TESTS_REQUIRED` (`.github/workflows/ci.yml:722`), and the step is NOT
     `continue-on-error`.
  3. **`tests/conformance/run_go.sh` exits 0 with the new `go_only` rows present**, and
     `python3 tests/conformance/suite.py run` still reports 0 failures. The oracle skips those rows
     by id, counted, with their reason.
  4. **The cross-binary seam test (🔴1) passes in the `go` job:**
     `TestAUIWrittenDeclarationIsWhatThePodServes` (S4). It builds BOTH halves the way their `main`s
     build them:
     - the UI's handler over a JOURNAL-backed `control` model, POSTing a declaration;
     - the pod's `api.New` over a TOKEN-FILE projection (`cmd/cairn-server/main.go:358`), serving
       it.

     It asserts that the GET returns the POSTed list. It is shown RED under the ID-keyed design of
     revision 3, through the mutant `ui-sources-keyed-by-control-id`. `e2e.sh`'s clause (a) cannot see
     this defect, because its writer chooses its own key. That is why this is a separate part.

  `e2e.sh` follows the repository's harness convention: it exits **2** ("could not vouch") when
  `git` or a built binary is missing, or when one of its own controls misbehaves. That is the
  SCRIPT's code. The proposed `audit-refs` verb exits 2 only for bad argv (decision 8).

  **What `e2e.sh` asserts.** Everything runs over SYNTHETIC worlds built at run time, and nothing
  touches the network:
  - bare repositories made with `git init --bare`;
  - a loopback GitHub stub;
  - the auditing host's map pointing `example.com` at `file://<tmp>` (decision 8);
  - a sources journal written by a test-only program through the ONE writer
    (`internal/codesrc`), standing in for the UI's POST. S4 covers the POST itself.

  | clause | what it asserts | negative control inside the clause |
  |---|---|---|
  | **(a) the declaration is served** | the pod, started with the journal mounted read-only, serves `alpha-notes`'s two sources on `GET /api/v1/sources/alpha-notes`. The proposed `sources` verb prints exactly those two canonical forms, in order | `beta-notes`, which has no record, prints `sources=undeclared`; a principal with no grant on `alpha-notes` gets the byte-identical answer it gets for a scope that does not exist |
  | **(b) clean world** | the proposed `audit-refs` verb, fetching over the real `file://` path, exits **0** and prints `checked=<N> stale=0 unchecked=0` with N equal to the planted count, and N > 0 | — |
  | **(c) stale world** | exactly one missing path, one symbol absent from its file and one PR answering 404 → exit **9**. The finding set EQUALS the planted set, by entry, claim and state | removing each planted defect drops exactly its own finding |
  | **(d) could not look** | exit **10**, with the reason named, for each of: a remote that does not exist; an undeclared scope; a declared scope with `checked=0`; one mapped source clean beside one unmapped source | the same scope with the remote present, one claim, and only mapped sources exits 0 |
  | **(e) the pod and UI never fetch** | `go list -deps` finds no `os/exec` and no `internal/refaudit` in `cmd/cairn-server` or `cmd/cairn-ui` | the same query on `cmd/cairn` finds both, so the count can move |
  | **(f) reachability, not presence, not tags** | a `RESOLVED` SHA reachable ONLY from `refs/pull/1/head` reports `exists-off-branch` (informational), and the exit is unaffected. Then that ref is DELETED on the remote while a TAG pointing at the same SHA is KEPT, and the run repeats. The SHA now reports `not-reachable` (could-not-look, exit 10). That holds even though the previous run left the object in the mirror, and even though the tag still contains it (🟡1) | the run with the pull refspec removed also reports `not-reachable` |

  **`--self-test`** applies one sabotage per clause on a scratch copy of the tree with its `.git`
  removed (the `tests/control_mutants.py` pattern). Each must be caught by its OWN clause's
  message. It prints **`sabotaged=6 caught=6`**. The sabotages:
  - **(a)** the pod serves the first source only;
  - **(b)** the denominator prints 0;
  - **(c)** the symbol check is dropped;
  - **(d)** an unmapped host counts as clean;
  - **(e)** a blank `os/exec` import goes into `cmd/cairn-server`;
  - **(f)** the reachability test is replaced by object presence (`cat-file -e`).

  ⚠ **NOT covered:**
  - stored findings (Q4) and a recall-header line (Q5). Both are open questions, not slices;
  - the PR check against the REAL GitHub API. That is a one-time operator check (S5).
  - S2 and S4 are covered by their own Go tests and part 4, not by `e2e.sh`. That is stated so
    nobody reads the six clauses as covering the UI write.

## STEP 1 — What audits exist today

**In this repository, none touch a remote.** cairn's own checks are about entry SHAPE:
- the `validate` scans (`internal/store/validate.go:196`, `:251`, `:455`);
- the open-actions scan (`:543`);
- the arc registry's `?check=1` orphan check (`tests/conformance/README.md`, "The orphan
  check (S5)").

**Out of tree: the operator's private hygiene tooling**, summarised by mechanism only:

| claim in an entry | how it is checked today | against what |
|---|---|---|
| a commit SHA (`RESOLVED <sha>:`, or a backticked hex run) | `git cat-file -e <sha>^{commit}` (object PRESENCE) | every LOCAL clone it can find |
| a path in `## Pointers` | `exists()` on the filesystem | the scope's local clone, then the others |
| a PR or issue ref | `gh pr view`, behind an opt-in flag | GitHub, as the operator's `gh` login |
| a symbol | **not checked** | — |

Its verdict is three-state, and it deletes nothing. It warns of three limits:
- it answers about the clone on disk, not the remote;
- a short SHA can match the wrong repository;
- **a SHA that exists but implements nothing still passes.**

That last limit carries into this plan unchanged (decision 7).

**The plugin plan (#208) describes no auditor.** Its plugins write per-session outputs of a closed
set of types (decision 9 there), its worker tokens "cannot read the store" (decision 2 there), and
its output renders as a labelled CLAIM (decision 11 there).

## STEP 2 — What exists today, read off the code (`0355c7a`)

### Per-entry refs: `refs:` is `<system>:<id>`

- `TaskRef` normalises the system half and keeps the id half byte-identical
  (`internal/store/entry.go:66-84`).
- `ParseTaskRef` refuses whitespace, a comma (because the inline-list writer would split it) and
  control characters (`:570-622`, `:594-608`).
- The URL registry is string formatting only (`internal/store/refurl.go:11-15`). Its built-in tier
  is `github` and `clickup` (`:81-120`). Its operator tier is `CAIRN_REF_BASE_<SYSTEM>`, because a
  self-hosted host may not be written into this repository (`:26-35`).

So `github:example-org/example-repo#428` is already a structured PR claim.

### Claims the entry grammar makes structurally

- **`RESOLVED <sha>:`** — 7 to 40 hex characters (`internal/store/openness.go:47-53`), lowercased
  (`:76-90`). The marker exists to make the claim checkable (`:41-42`).
- **`## Pointers`** — a declared heading (`internal/store/journal.go:16-20`). Its rows are
  `- path — description` (`internal/report/citationtoken_test.go:21-24`). No parser extracts the
  path.
- **Symbols** — backticked spans in prose, with no structure.

### Why the store tree is the wrong place for anything the UI edits (O5's reasons)

- **The pod serves a COPY, and re-seeding overwrites it wholesale.**
  - `server/README.md:333-334` says so: "This server does NOT serve the authoritative store — it
    serves a COPY, and nothing syncs that copy."
  - `seed.sh` pipes a tar of the operator's local store into the pod and extracts it over the
    destination (`server/seed.sh:313-316`).
  - The local store "stays authoritative" (`server/README.md:32-33`), and the persistent volume is
    "a second copy, not the only one" (`:1505-1507`).

  So a file the UI edits inside the pod's store would be silently reverted by the next seed. That
  sank revision 2's README storage (round 2's 🔴).
- **The UI's store mount is read-only by an earlier operator decision.** The UI got its own volume
  for the control journal and the session table, with the store mounted read-only. Putting the
  journal on the store's volume was declined because `seed.sh` overwrites it
  (`claudedocs/handoff-cairn-control-plane.md:502-505`). README storage would have reversed that
  and handed the internet-facing UI write access to the WHOLE store.
- **Revision 1's measurement still stands, and still rules out a reserved file.** This plan's
  author measured both loaders (Go `LoadIndex`, Python `load_index`) at `0355c7a`:
  - `_scope.md` → **1 MALFORMED** row;
  - `README.md` with the same front matter → **0** rows.

  That is what made README storage look free (O4). O5 rejects it for the two reasons above, not
  for this one.

### The arc registry: the pattern O5 adopts

- **One package.** The arc registry is an append-only JSONL journal OUTSIDE the store tree
  (`internal/arcs/arcs.go:1-2`). One package is its validator, reader and writer (`:4-8`).
- **Placement.** The pod refuses to start if the journal resolves inside the store root, through
  `arcs.ResolveJournalPath` (`cmd/cairn-server/main.go:381-394`). Inside the root, the journal
  would become a scope.
- **The write.** The journal is opened with `O_NOFOLLOW` (`internal/arcs/journal.go:178`). An
  exclusive `flock` covers the whole read-merge-append, then there is one `write(2)` under
  `O_APPEND`, then `fsync` (`:160-191`). Its comment names the limit: `flock` is advisory-only
  across hosts on a network filesystem (`:167`).
- **The read, in THREE states.**
  - **Off:** unset, which answers `registrations-unconfigured`.
  - **Empty:** a missing file is an empty snapshot flagged `Missing` (`:86-96`).
  - **Broken:** any other read failure is a `*JournalUnreadableError`, which the pod answers as
    **503** (`internal/api/server.go:930-935`, `:1424-1428`).

  Damaged lines are skipped and counted, and a torn tail is decided by the last byte (`:98-105`,
  `Snapshot.Damaged` at `:65`).
- **Provenance.** The pod stamps who pushed and when; every other field is the writer's own word
  (`arcs.go:15-19`).
- **The deployment facts arcs already carry:**
  - the journal is BACKED UP on both instances (`claudedocs/handoff-cairn-arcs-sessions.md:46-51`):
    - the personal instance's UI backup job FAILS if `arcs.jsonl` is absent;
    - the client instance's backup job PASSES on absence, logging `arcs: absent`, because nothing
      had registered there yet;
  - a second pod mounting a ReadWriteOnce, node-local volume must run on the SAME node;
  - on one storage driver, a claim-level `readOnly` breaks co-mounting, so read-only is enforced on
    the CONTAINER mount (`handoff-cairn-arcs-sessions.md:122-126`).
- **Go-only.** The arc routes are Go-only by decision. `internal/api/routes.go:67-71` names the
  FOUR places a Go-only head moves together:
  - that table;
  - the `go_only` rows of `tests/conformance/requests.json`;
  - the `go_only` rows of `tests/testlib/capability_ledger.py` (`:39-43`, `:112-128`);
  - `flake.nix`'s `want-go-only.txt`.

  A Go-only golden is a change detector (`tests/conformance/README.md`, "…and the mirror").
  ⚠ Revision 3 named a fifth, "the dual-run gate's go-only declaration for the head". **No such
  ledger exists** (round 3 🟢1). The dual-run gate discovers routes from the oracle, and its
  `go_only_params` is a list of query parameters. So a new Go-only head is simply INVISIBLE to
  dual-run, which is a blind spot and not a ledger to move.
- ⚠ **Every read head is served on GET AND HEAD.** The ledger expands each head over
  `safeReadMethods` (`routes.go:84`, `:96-102`). So "one GET route" (O5) and D5 ("no HEAD") are
  reconciled in decision 10.

### Scope identity: two authorities, two ID SPACES — inside ONE deployment (round 3 🔴1)

- The control model's `Scope` has an immutable `ID`, a mutable `DisplayName` and a `ProjectID`
  (`internal/control/model.go:228-243`).
- **There are two ID rules:**
  - **In a control journal**, scope IDs are RANDOM at creation (`internal/control/provision.go:113`,
    `NewID`). A rename keeps the ID (`scope-renamed`, `internal/control/journal.go:22`), and there
    is no scope-deletion event.
  - **In the token-file projection**, scope IDs are DERIVED from the folded directory name
    (`internal/control/tokenfile/source.go:580-586`, `DerivedID`).
- **Both rules are live in ONE deployment, on opposite sides of this feature.**
  - Only a journal-backed UI can edit, because the token file confers `admin` on nobody
    (`tokenfile/source.go:424-425`).
  - The pod authorises bearer tokens from the TOKEN-FILE projection
    (`api.New(*store, tokens, …)`, `cmd/cairn-server/main.go:358`). Its control journal is only a
    SESSION authority (`:410-423`).

  So a record keyed by the UI's scope ID could never be found by the pod's lookup. Revision 3's
  design would have answered `undeclared` for every scope, forever. **The one identifier both
  authorities and the store share is the folded DIRECTORY NAME** (`store.NormalizeRef`), which is
  what decision 4 now keys on.
- Authorisation stays keyed on each authority's own ID (`internal/control/resolve.go:89`). Keying
  the RECORD by name does not change who may read or write it.

### The browser surface: the share flow is the edit-flow precedent

- UI routes are a declared table (`internal/ui/routes.go:118-185`). `POST /share` and
  `POST /unshare` are scope-administration writes (`:147-149`).
- Both cross-site gates derive from the METHOD (`stateChanging`, `internal/ui/server.go:1323`):
  same-origin before auth, and a per-session CSRF token after it (`:1305-1306`).
- The share handler authorises with `id.Auth.Allows(scope, control.VerbAdmin)`
  (`internal/ui/sharehandlers.go:170`).
- **Every file the `cairn-ui` binary writes today** (round 3 🟡2). Measured by finding the
  file-writing `os` calls (`WriteFile`/`OpenFile`/`Create`/`CreateTemp`/`Rename`/`Mkdir`; T9's pin 1
  uses the full PACKAGE-LEVEL mutator set) in every
  in-repo package of `go list -deps ./cmd/cairn-ui`:
  - **the control journal**, which must resolve outside `-store` (`cmd/cairn-ui/main.go:246-266`),
    through `internal/control/filestore.go`;
  - **the session table** (`-session-file`), rewritten by temp file plus rename
    (`internal/identity/sessionstore.go:283-313`), plus its **`<session-file>.lock` sidecar**,
    created on first use (`:197`);
  - **the presence token file**, appended only by the `issuePresenceToken` admin path
    (`cmd/cairn-ui/presence.go:161-192`; `internal/presence/tokens.go:175-188`).

  `internal/write` and `internal/arcs` are in the graph but write nothing from it. The UI never
  calls `AppendBullet`/`ReplaceEntry`/`CreateEntry` or `arcs.Register`, and `internal/ui` itself
  contains **zero** direct file-writing `os` calls. Revision 3 said "the control journal only",
  which was false.
- **The UI's `internal/write` uses are pure helpers:**
  - `write.SessionComponent` at 2 sites. It is a regexp VARIABLE (`bell.go:45`,
    `sessionpage.go:97`);
  - `write.WithoutTrailers` at 1 site (`sessionpage.go:334`);
  - `write.Attribution` at 0 sites.

  Revision 3 printed 5/2/1, which counted comment mentions. These counts are non-comment code
  lines.

### The CLI: the one `git` caller, and the exit model the auditor reuses

- `internal/client/reposcope.go` is the CLI's one `git` call site (`:38-58`).
- **MEASURED by this plan's author (`go list -deps`, `0355c7a`):** `os/exec` is in `cmd/cairn`'s
  graph (1 of 204 packages) and in neither `cmd/cairn-server`'s (0 of 209) nor `cmd/cairn-ui`'s
  (0 of 232).
- **Go tests run `git` under a two-tier rule.**
  `internal/depspolicy/depspolicy_test.go:419-448` drives a real `git init`.
  - It SKIPS without `git`, because the nix check phase has none, and adding git there was
    declined (`:420-426`).
  - It FAILS when `CAIRN_GIT_TESTS_REQUIRED` is set, which the `go` CI job does (`ci.yml:722`).

  This plan reuses that rule and adds no `git` to any check phase.
- **The exit model.** `arcs --check` reuses doctor's `0/9/10` (`internal/client/exit.go:18`):
  `9` = a check MEASURED a problem, `10` = a check COULD NOT LOOK
  (`internal/doctor/render.go:46-51`). `2` is USAGE (`exit.go:36-39`).

## Decisions — who chose what

### Chosen by the OPERATOR (paraphrased; not re-litigated)

| # | what the operator chose | where it lands |
|---|---|---|
| O1 | Being able to audit the store's claims against upstream code mechanically, which no tool does today. | The goal. |
| O2 | "Scope-level refs" means each scope names the code it describes (repositories and branch), so an auditor can test entries' paths, symbols, commits and PR references against it. | Decisions 2, 6. |
| O3 | The declaration belongs to the scope and is edited in the browser. **Deviation history:** the first wording was a per-scope "manifest", with a reserved file such as `_scope.md` as the example. Revision 1 measured that file loading as a MALFORMED entry on both clients, and recommended a separate journal. | Decisions 1, 4, 5. |
| O4 | *(Superseded by O5.)* Store the sources in the scope's `README.md` front matter. It was chosen because README front matter is invisible to both loaders, as measured. Round 2 then showed that the pod's store is a copy `seed.sh` overwrites, and that the UI would need write access to the whole store. | — |
| O5 | **An append-only journal OUTSIDE the store tree, on the arc registry's pattern.** The UI is its ONLY writer. ~~Records are keyed by scope ID, not by name.~~ **SUPERSEDED by the operator's Q17 answer: records are keyed by the normalised scope NAME (`codesrc.Key`).** The store pod mounts it READ-ONLY and serves one Go-only GET route, which the CLI auditor reads. The reasons the operator accepted: README edits would be silently reverted by the next re-seed; README storage would reverse the decision to keep the UI's store mount read-only; and the journal gives a who/when change history for free, which an audit feature wants. ⚠ **The agent departs from ONE detail of this, the keying**: records are keyed by the normalised scope NAME, because the UI's and the pod's scope IDs come from different authorities and never match (round 3 🔴1, decision 4). That was for the operator to confirm (Q17), and the operator CONFIRMED it (Q17, answered). | Decisions 1, 4, 5, 10. |

### Chosen by the AGENT writing this plan (open to review)

1. **The storage contract under O5: a new package, `internal/codesrc`, that is the journal's ONE
   validator, reader and writer, as `internal/arcs` is for arcs.**
   - **Location and configuration.**
     - The env var is `CAIRN_SOURCE_JOURNAL`. There is **no flag**: a pre-feature binary handed an
       unknown flag refuses to start, but it ignores an unknown env var, so the env var is the
       rollback story.
     - It has no default. Unset means the pod answers `sources-unconfigured` and the UI renders
       that state on the page.
     - Both binaries refuse to start if the path resolves inside the store root. They reuse
       `arcs.ResolveJournalPath`'s resolution, but the refusal message must name the SOURCES
       journal and `CAIRN_SOURCE_JOURNAL`, never "arc journal". A test pins the message.
     - The file lives on the UI-owned volume, beside the control journal
       (`handoff-cairn-control-plane.md:502-505`).
   - **Records.** One JSON line each: `{schema, scope, sources:[…], set_by, set_at, revision}`.
     - `scope` is the key: `codesrc.Key(name)` (decision 4).
     - `set_by` is the signed-in principal and `set_at` is the UI's clock. Both are stamped by the
       UI, never taken from the form.
     - `revision` is a digest of `scope` plus `sources`.
     - The fold is LATEST-WINS per `scope`. The journal is the history.
   - **Writing (R1's F2, re-specified for O5).** `Journal.Set(scope, sources, ifRevision, by, now,
     interleave)` opens the file with `O_NOFOLLOW`, as arcs does (`internal/arcs/journal.go:178`),
     and runs entirely under ONE exclusive `flock`:
     1. take the lock;
     2. re-read through the locked descriptor;
     3. fold;
     4. compare the scope's CURRENT revision with `ifRevision`, inside the lock. A scope with no
        record has the fixed revision `none`;
     5. append one line;
     6. `fsync`;
     7. release.

     **Exactly one of two writes carrying the same revision lands.** The other gets
     `*StaleRevisionError` carrying the current revision. `interleave` runs inside the lock,
     between the compare and the append, in `internal/write`'s seam pattern
     (`internal/write/write.go:284-286`; `write_test.go:188-215`). A second submission of an
     UNCHANGED list with the now-stale revision is refused the same way. That is a harmless no-op
     the user sees as "already current", not a lost update.
   - **Reading: arcs' three states, exactly (round 3 🟡3).**
     - **Off:** the env var is unset. The answer is `sources-unconfigured`, HTTP 200.
     - **Broken:** the file exists but cannot be read (permissions, a directory at the path, an
       I/O error). That is `*JournalUnreadableError`. The pod answers EXACTLY as arcs do: it
       routes the error through `storeUnreachable` (`internal/api/server.go:930-935`,
       `:967-970`).
       - **The wire answer:** `503`, `X-Store-Status: store-unreachable`, `X-Store-Exit: 3`, and
         the error text as a plain-text body.
       - **No new wire token** (round 4 🟡1).
       - **The proposed `audit-refs` verb maps that 503 to the could-not-look reason
         `sources-unreadable`, exit 10, on any non-200 fetch failure.** Like every read verb, it
         never reads `X-Store-Exit` on a 503.
         - **Where a plain read's 3 actually comes from** (round 5 🟢1). On a non-200,
           `FetchReport` discards the headers and returns `*StoreUnreachable`
           (`internal/client/arcs.go:76-85`). `cli.go:661-676` maps that to
           `ExitUnreachableNoCache`, which is 3 (`exit.go:42`). `X-Store-Exit` is never read on a
           503: `printPodReport` runs only on a 200.
         - **The precedent the auditor follows for 10 is `arcsCheck`**. It returns
           `doctor.ExitUnmeasured` when `FetchReport` fails (`arcs.go:166-169`), under the comment
           "THE POD DID NOT ANSWER IS ALSO 10, NOT 3" (`:143-145`).
         - The proposed `sources` verb is a plain read, so it keeps the arcs behaviour and exits
           3.
     - **Empty:** the file is absent, so it is `Missing`. Every scope reads `sources=undeclared`,
       and the response carries `journal=absent`.

       ⚠ **A wrong path looks exactly like this**: nothing was ever written, and every scope is
       undeclared. Arcs accept the same ambiguity. Here it is visible on every response through
       `journal=absent`, and it fails safe, because an undeclared scope exits 10 and is never
       reported clean.

     Damaged lines are skipped and counted, and a torn tail is never applied. An UNKNOWN FIELD is
     ignored, not refused, so a record written by a newer build still folds in an older one.
   - **Rollback.**
     - A pre-feature pod or UI ignores `CAIRN_SOURCE_JOURNAL` and never opens the file.
     - The journal is a separate file, not an event in the control journal, so the control
       journal's refuse-whole rule (`internal/control/journal.go:219-226`) cannot be triggered by
       it.
     - The store tree is untouched.
     - Rolling back leaves an inert file; rolling forward folds it again.
   - **Deployment** (private repositories; named here, done there):
     - the UI mounts the volume read-write;
     - the pod mounts the SAME volume with the container-level `readOnly` (never claim-level;
       arcs' storage-driver lesson), so it must schedule on the UI's NODE, because the volume is
       ReadWriteOnce and node-local. Arcs already carry this constraint;
     - the backup jobs stage `sources.jsonl` beside `arcs.jsonl`, with the same restore-verify
       step. **An absent file is a PASS, logged as `sources: absent`** (round 3 🟡4), which is the
       client instance's arcs precedent. Nothing creates the file until the first POST, so a
       fail-on-absent rule would be red from deploy day. The alternative, the UI creating the file
       at startup, adds a write on a path the read-only state otherwise never takes. The cost:
       absence on a deployment that HAS been edited would also pass. It is visible as
       `sources: absent` in the backup log and as `journal=absent` on every GET.

2. **The source grammar: one string per source.**

   ```
   git:<host>/<repo-path>[//<subpath>]@<branch>
   ```

   - `<host>` is a DNS name, lowercased. No scheme, userinfo or port.
   - `<repo-path>` is ≥ 2 segments of `[A-Za-z0-9._-]+`. No segment starts with `-` or `.`, and a
     trailing `.git` is stripped. **Case is PRESERVED** and compared byte-identically, as for
     `TaskRef`'s id half.
   - `//<subpath>` is optional, ≥ 1 segment under the same rule, with no `..`.
   - `@<branch>` is REQUIRED. It must be a valid `git check-ref-format --branch` name, with no
     leading `-`, no `@` and no whitespace.
   - No comma and no control characters anywhere.
   - **As built (S1, after round 1):** the string must be valid UTF-8 (the JSON encoder would
     otherwise rewrite a bad byte and the reader would skip the writer's own record); "control"
     is `unicode.IsControl` (C0, DEL and C1, CSI included); Unicode FORMAT characters (category
     Cf: bidi overrides and isolates, zero-width characters, the BOM) are refused, because a
     source is a displayed string and these make two sources render alike; whitespace is
     `unicode.IsSpace`. The host ends at the FIRST `/`; an `@` before it is userinfo, and the
     branch starts after the FIRST `@` after it, so a second `@` is refused as an `@` in the
     branch. **`#` is NOT refused**: git accepts it in a branch (`issue#12`), and a pasted
     `… # comment` still cannot join a source, because it can only be attached by whitespace,
     which is refused. An over-cap refusal names the source's ORIGINAL line, duplicates
     included.
   - The canonical form is the normalised string. Duplicates are DEDUPED and order is KEPT, so
     the first source is the primary.
   - At most 8 sources (Q14).

   Synthetic examples: `git:github.com/example-org/example-repo@main`;
   `git:github.com/example-org/example-mono//services/widget@release/2.x`;
   `git:git.example.com/team/sub-group/example-repo@trunk`.

   **`codesrc.Parse` is the ONE parser.** Every caller uses it: the UI POST, the writer (which
   re-validates), the pod's GET (which re-validates what it folds), and the auditor.

   ⚠ Revision 2's whitespace rule was motivated by a measurement: `#` is not a front-matter
   comment. That measurement no longer applies, because nothing here is front matter. The rule
   stays for its own reason: a source is one token, and the textarea is split on newlines.

3. **REMOVED (O5).** Revision 2's "Go-only reading, Python provably ignores the README key". No
   store file carries sources, so neither client nor the oracle reads anything new. **The
   Python-ignores test and the parity-world README are deleted.** The only new served shape is a
   Go-only route (decision 10), which follows the arcs precedent.

4. **Who may edit, and the ONE key (round 2's 🟡4; round 3 🔴1).**
   - **Edit:** a principal holding `admin` on the scope. That is the share flow's verb
     (`sharehandlers.go:170`), and only the UI asks for it.
   - **Read:** a principal holding `read`. The GET answers uniformly for "not visible" and
     "absent".
   - ⚠ **A token-file deployment confers `admin` on nobody** (`tokenfile/source.go:424-425`), so
     nobody can edit there. Stated so nobody files it as a bug.
   - **The key.** `codesrc.Key(scopeName string) string` is `store.NormalizeRef` of the store
     DIRECTORY name. It is the ONE function from a scope to the journal key, called by the UI's GET
     and POST, the pod's GET handler, and nothing else. It needs no `control.Model`, so the two
     binaries' different authorities cannot make it disagree (STEP 2, "two ID spaces"). The
     proposed `sources` and `audit-refs` verbs send the name, and the pod folds it.
   - **The consequences, stated plainly:**
     - **a DIRECTORY rename ORPHANS the record.** The scope reads `undeclared` under its new
       directory name until an admin re-declares it, and the old record stays in the journal as
       history. ⚠ A control-journal `scope-renamed` event ON ITS OWN renames no directory. It
       detaches that scope's display name from its directory, which is a pre-existing property of
       the control plane, not something this feature adds. The key follows the directory, never
       the display name;
     - **a directory delete followed by a recreate under the same name RE-ATTACHES the record.**
       The new scope inherits the old declaration, shown with its original `set_by`/`set_at`, so
       the page says whose declaration it is.

     Both behave the same in every deployment, because the key is the directory name. On the pod,
     the token-file projection enumerates store directories. On the UI, the STORE INDEX enumerates
     them; the journal authority does not. Revision 3's per-deployment table is deleted with the
     ID keying. An orphaned record is not a fault; the UI shows nothing for it in v1 (Q12).
   - ⚠ **Revision 3 keyed by scope ID through `KeyFor(model, name)`.** That design is RETRACTED
     (round 3 🔴1). Each side resolved IDs in its own space: random in the UI's control journal,
     derived in the pod's token-file projection. The pod never found the UI's record. Closing
     condition part 4 is the test that would have shown it.

5. **The edit flow: the UI is the ONLY writer (D1, O5).**
   - **Routes.** `GET /scope/sources?scope=<s>` renders a form and `POST /scope/sources` writes.
     Both are rows in the UI's declared table.
     - Same-origin and CSRF come from the method (`server.go:1305`, `:1323`).
     - Authorisation is decision 4.
     - With the journal unconfigured, the page renders that state, as `ReadOnlyAuthority` does.
   - **The form.** ONE `<textarea>`, one source per line (Q6), plus a hidden `revision`.
     - The POST calls `codesrc.Journal.Set` with `codesrc.Key(scope)` and the form's revision.
     - A stale revision writes nothing and re-renders the CURRENT list.
     - An invalid line is refused, naming its line number and rule, with the text preserved.
     - An empty textarea writes a record with `sources: []`. That is an explicit
       "undeclared, by `<who>` at `<when>`", so clearing is history too.
   - **The scope page.** It gains a Sources block: each source as text, a link ONLY for the
     built-in `github.com` shape (`https://github.com/<repo-path>/tree/<branch>`) through
     `safeHref`, and the latest record's `set_by` and `set_at`.
   - **No CLI edit, no pod write** (D1). The pod opens the journal `O_RDONLY`, and its import
     graph never reaches `Journal.Set`. That is enforced by the caller ledger (T9).

6. **What the auditor checks DETERMINISTICALLY.** Every check is a fact about a fetched commit,
   and the finding quotes that commit.

   | claim, and where it comes from | check | states |
   |---|---|---|
   | **path** — the first backticked span of a `## Pointers` row, or its first token before ` — `, if path-shaped (contains `/` or a `.<ext>`, no scheme, no whitespace, no leading `-`) | **Normalise first (round 2's 🟡2):** strip one leading `./` and any trailing `/`. Then run `git ls-tree -z <commit> -- <path>` and branch on CONTENT: the path is present when a NUL-terminated record's path field EQUALS the normalised path. `ls-tree` exits 0 on a missing path (round-1 F8). A `tree` record (a directory) is present. Try repo-root-relative first, then subpath-relative (Q7). | `holds(root)` · `holds(subpath)` · `missing` · `unchecked: not path-shaped` |
   | **commit** — `RESOLVED <sha>:` (`openness.go:47-53`) | **Reachability, never object presence (round 2's 🟡3):** `git rev-parse --disambiguate`, then `git for-each-ref --contains <sha> refs/heads/ refs/pull/`. The pattern RESTRICTS the answer to the two namespaces this run fetched, so a TAG can never count (round 3 🟡1). `on-branch` if the declared branch's ref contains it; `exists-off-branch` if only another fetched ref does; `not-reachable` if none does. | `on-branch` · `exists-off-branch` · `not-reachable` · `ambiguous` |
   | **PR** — a `refs:` item `github:<owner>/<repo>#N` (`refurl.go:104-118`) | GitHub REST `GET /repos/<owner>/<repo>/pulls/N`, falling back to `/issues/N` on 404 | `open` · `merged` · `closed-unmerged` · `not-found` · `unchecked: rate-limited / no token` |
   | **ref outside the declaration** — a `github:` ref whose owner/repo is none of the scope's sources | string comparison | `outside-sources` |
   | **symbol** — ONLY the structural form `path#Symbol` in a `## Pointers` row (Q8) | the path normalisation above, then `git grep -w -F -e <Symbol> <commit> -- <path>` | `present` · `absent-from-file` · `file-missing` |

   **The CLOSED severity table (round 2's 🟡5).** Every state above appears exactly once, in one
   table in `internal/refaudit`, and a test asserts that the table's key set EQUALS the set of
   states the checks can emit.

   | class | states | counted in | effect on the exit |
   |---|---|---|---|
   | **holds** | `holds(root)`, `holds(subpath)`, `on-branch`, `present` | `checked` | none |
   | **informational** | `exists-off-branch`, `open`, `merged`, `closed-unmerged`, `outside-sources` | `checked` | none |
   | **stale** | `missing`, `absent-from-file`, `file-missing`, `not-found` | `checked` and `stale` | **9** |
   | **could not look** | `not-reachable`, `ambiguous`, `unchecked: rate-limited / no token`, `unchecked: host not mapped`, `unchecked: remote unreachable` | `unchecked` | **10**, unless something is stale |
   | **unchecked by design** | `unchecked: not path-shaped` | `unchecked` | none on its own |

   Plus four SCOPE-level could-not-look reasons, each exit 10: `sources=undeclared` (including
   `journal=absent`), `sources-unconfigured`, `sources-unreadable` (the auditor's name for the pod's 503 `store-unreachable`, decision 1), and
   `checked=0`.

   **The mixed case, spelled out.** Exit **9** if any claim anywhere is stale. Otherwise exit
   **10** if any claim or source could not be looked at. That includes a declared source on an
   unmapped host sitting beside a clean mapped one, because part of the declaration was never
   looked at. Otherwise exit **0**, and only when `checked > 0`.

   **Why a commit is never stale.** Absence from the fetched refs proves nothing about the
   remote. And the fetch PRUNES, so the answer depends on the remote's CURRENT refs, never on how
   old the mirror is. Two costs follow, stated rather than hidden:
   - **squash-and-delete:** on a host with no pull refs, a feature branch deleted after a squash
     merge leaves its `RESOLVED` SHA `not-reachable` (exit 10) forever. GitHub keeps
     `refs/pull/N/head`, so there the SHA stays `exists-off-branch`;
   - **force-push:** a rewritten branch makes the old SHAs `not-reachable`.

   Both are could-not-look, never stale. An operator who wants them quiet re-points the
   `RESOLVED` marker at the squash commit.

   **Unchecked by design, and counted so the denominator is honest:** prose backticks, SHAs in
   prose after the marker, URLs and operator-tier ref systems. (`path:line` is REMOVED, D4.)

7. **What needs JUDGEMENT stays out of the exit code:**
   - whether an existing file still says what the entry claims;
   - whether a commit implements the claim;
   - whether `OPEN:` items are still open;
   - what a closed-unmerged PR means;
   - whether a moved symbol is the same symbol.

   The auditor may print inputs for those judgements. It never renders a verdict on them.

8. **Where the auditor runs: the proposed `audit-refs` verb, on an operator host, with the
   operator's own credentials.** The pod and the UI never fetch (clause e).
   - **Reading.** It reads entries from the synced cache, as `recall` does. It reads sources from
     `GET /api/v1/sources/<scope>` with the client's existing token.
   - **Fetching (F1, and round 2's 🟡3).** For each source it updates a mirror at
     `$XDG_CACHE_HOME/cairn/mirrors/<sha256(canonical source)>.git` with
     `git fetch --prune --no-tags` IN FULL (no partial-clone filter), using these refspecs:
     - `+refs/heads/*:refs/heads/*`;
     - `+refs/pull/*/head:refs/pull/*/head`, which on a host with no pull refs fetches nothing.

     Reachability is computed only over `refs/heads/` and `refs/pull/` as that fetch left them
     (decision 6). An object surviving in the mirror from an earlier run never counts.

     **`--no-tags` AND the restricted `for-each-ref`: both, and each for its own reason (round 3
     🟡1).** Tags auto-follow by default, and `--prune` does not delete them. Round 3 measured
     this on git 2.55: a branch and a tag deleted on the remote left `refs/heads/feat` pruned and
     `refs/tags/v1` kept. An unrestricted `for-each-ref --contains` then reported the SHA reachable,
     which is a false clean exit 0.
     - `--no-tags` stops new tags arriving.
     - The namespace restriction ignores any tag already in a mirror that an older build created.
     - Every `git` call runs with `GIT_NO_LAZY_FETCH=1`.
     - Round 1 measured, on one squash-shaped world: 128 resolved with a full single-branch
       fetch, 128 with no lazy fetch, and **0** with lazy fetch on a `blob:none` mirror.
     - The mirror's directory is a DIGEST, never a path built from the declaration.
   - **Transport (F3: https only).** The auditing host holds a map, `CAIRN_AUDIT_HOSTS`, from host
     to base URL, defaulting to `github.com=https://github.com`.
     - `GIT_ALLOW_PROTOCOL` is the set of schemes in that map. `file` appears only when a host is
       mapped to a `file://` base, which is how `e2e.sh` reaches the real fetch path.
     - **SSH is not supported in v1.**
     - Every call sets `GIT_TERMINAL_PROMPT=0`, `GIT_CONFIG_NOSYSTEM=1` and
       `GIT_LITERAL_PATHSPECS=1` (F8), and passes `--end-of-options` or `--`.
     - Credentials come from the operator's credential helper. An unmapped host is never
       contacted.
   - **PR state.** It reads `GH_TOKEN` or `GITHUB_TOKEN`. The API base is `CAIRN_AUDIT_GITHUB_API`
     (default `https://api.github.com`), which `e2e.sh` points at its stub. No credential is
     printed, logged or sent to the pod.
   - **Output.** `sources=<…>`, then `checked=<N> stale=<S> unchecked=<U>`, then one line per
     finding. `--json` gives the same as data.
   - **Exit codes:** doctor's `0/9/10` per decision 6's table. No new code is minted. `2` stays
     USAGE.
   - **The proposed `sources` verb** (read-only, Go-only) prints the served canonical list,
     `sources=undeclared` or `sources-unconfigured`.

9. **The auditor is a LIBRARY first (`internal/refaudit`), so #208 can wrap it later.** It takes
   plain values and the `GitReader` and `PRReader` interfaces, and returns plain findings. The
   proposed `audit-refs` verb is its first caller. A later plugin calling the same library cannot
   disagree with it.

10. **The pod's ONE route: `GET /api/v1/sources/<scope>`, Go-only, on the arcs precedent (O5).**
    - **How it answers:**
      - authorised by `read` on the scope;
      - `sources-unconfigured` (200) when the env var is unset;
      - **503 with `X-Store-Status: store-unreachable` and `X-Store-Exit: 3`** when the journal is
        configured but cannot be read. That is the arcs answer, through `storeUnreachable`
        (`internal/api/server.go:930-935`, `:967-970`), and no new wire token. The auditor maps
        it to exit 10 (decision 1);
      - for a principal who cannot read the scope, a body byte-identical to an absent scope's;
      - a scope with no record returns `sources=undeclared`, plus `journal=absent` when the file
        does not exist;
      - otherwise the latest record's canonical sources, `set_by`, `set_at` and `revision`.

      All of these go through `codesrc.Key(<scope>)`.
    - **⚠ HEAD vs D5.** D5 removed a separately designed HEAD route. The route ledger nonetheless
      derives `HEAD` from every read head (`routes.go:84`, `:96-102`). **Recommend letting the
      ledger derive it:** no extra code, and the corpus's existing head-matches-get relation
      covers it. The alternative, a per-head GET-only exception, adds a second rule to a ledger
      that has one (Q16).
    - **The ledgers it moves.** These are the four places `routes.go:67-71` names, plus the
      route-declaration check:
      - `readHeads`;
      - `go_only` rows in `tests/conformance/requests.json` (authorised, unauthorised,
        refused-equals-absent), recorded by `run_go.sh record-go-only` with the journal path
        handed over the way `arc-journal=` is;
      - `capability_ledger` `go_only` rows;
      - `want-go-only.txt`;
      - `checks.go-server-declares-its-routes`.
    - **What the corpus cannot send, and what witnesses it instead.** `run_go.sh` boots ONE server
      with ONE journal path per run. So the configured-but-unreadable 503 and the unconfigured 200
      cannot both be rows beside the authorised ones. Each would need its own boot.

      Both are therefore witnessed by **literal-body Go tests in `internal/api`**, which are the
      contract witnesses for a Go-only route anyway (`tests/conformance/README.md`, "…and the
      mirror"). A second boot in the runner is a new mechanism, and this plan does not add it
      (Q18).
    - **The dual-run gate does NOT see this head.** It discovers routes from the oracle, which has
      no such route (STEP 2). That is named as a blind spot.
    - The oracle is never told about it.

11. **Recall gets no source line (Q5).** The proposed `sources` verb and the UI scope page answer
    "what does this scope document".

## Threat model

| threat | control |
|---|---|
| **T1. A hostile declaration aims the auditor's fetch at an attacker's host** | Only hosts in the auditing host's map are contacted, and the URL is built from the map's base. `GIT_ALLOW_PROTOCOL` holds only the map's schemes (https by default, no `ext::`, no ssh). Credential helpers are keyed by host. |
| **T2. Argument injection** (a leading `-`) | Refused at parse, plus `--end-of-options`/`--` at the one `GitReader` call site. |
| **T3. Pathspec magic, globs, odd spellings of a cited path** | `GIT_LITERAL_PATHSPECS=1`; the `./` and trailing-`/` normalisation; `ls-tree -z` with an exact path compare. |
| **T4. Mirror-path traversal** | The mirror directory is `sha256(canonical source)`. |
| **T5. Repointing a scope's audit** | Only `admin`, only through the UI, behind both method-derived gates. Every change is a journal line with `set_by` and `set_at`. |
| **T6. A lost update between two admins** | The revision compare inside the lock: exactly one of two same-revision writes lands (S1, S4). A resubmitted UNCHANGED list is refused the same way, which is a harmless no-op. |
| **T7. A re-seed silently reverting declarations** (round 2's 🔴) | The journal lives outside the store tree, on the UI's volume. `seed.sh` writes only the store copy (`seed.sh:313-316`). Pinned by the inside-store startup refusal. |
| **T8. A feature rollback breaks something** | An env var an old binary ignores, a separate file, no control-journal event, and an untouched store tree (decision 1). |
| **T9. The internet-facing UI's write reach** (round 2's 🟡1; round 3 🟡2, restated truthfully) | **The `cairn-ui` binary writes four files: the control journal, the session table (`-session-file`), the presence token file (admin path only), and, with S4, the sources journal.** It writes nothing in the store, whose mount STAYS read-only (`handoff-cairn-control-plane.md:502-505`). The session table also leaves a `<session-file>.lock` sidecar (`sessionstore.go:197`). Three pins, each failing on GROW or SHRINK: (1) **`internal/ui` contains zero calls to any PACKAGE-LEVEL `os` function that mutates the filesystem**: `WriteFile`, `OpenFile`, `Create`, `CreateTemp`, `Rename`, `Mkdir`, `MkdirAll`, `MkdirTemp`, `Link`, `Symlink`, `Remove`, `RemoveAll`, `Truncate`, `Chmod`, `Chown`, `Lchown`, `Chtimes` and `CopyFS`. The test parses `$(go env GOROOT)/src/os`, not `runtime.GOROOT()`, which can be empty under `-trimpath`. It classifies every exported package-level function as mutator or not, and fails on an unclassified one, so a toolchain bump that adds a package-level mutator goes red until it is classified. Measured 0 today, so this is an INVARIANT guard, labelled as one. An AST walk catches a raw `os.WriteFile` or `os.Remove` in a handler; (2) **`internal/ui` has exactly one call site of `codesrc.Journal.Set` and none of `AppendBullet`/`ReplaceEntry`/`CreateEntry`**; (3) **`internal/api` has zero call sites of `Journal.Set`**, so the pod cannot become a second writer. ⚠ **What these do NOT see:** **METHODS on `*os.File` and `*os.Root`**. That includes everything reached through `os.OpenRoot` (on go1.25, `*os.Root` has `WriteFile`, `Create`, `OpenFile`, `Mkdir`, `MkdirAll`, `Remove`, `RemoveAll`, `Rename`, `Link`, `Symlink`, `Chmod`, `Chown`, `Lchown` and `Chtimes`), so `os.OpenRoot(…)` followed by `r.WriteFile(…)` passes pin 1. A method call on an imported type cannot be resolved by an AST walk, nor by the repo's no-importer `go/types` precedent, which silently drops such calls (`internal/ui/membershipledger_test.go:36-43`). Pin 1 also does not see a write reached through ANOTHER package's function (the session store and control filestore are such packages, and are legitimately called); `cmd/cairn-ui` itself, which pin 1 does not walk; and the deployment's actual mounts. The pins bound the CODE in two packages, not the process. |
| **T10. The pod or UI spawns `git` or reaches a code host** | Clause (e). ⚠ It is structural on `exec` only, and cannot see a `net/http` CLIENT call. |
| **T11. Stored XSS through a source string** | gomponents `Text` only; the one href through `safeHref`; `Raw`/`Rawf` stay AST-banned. |
| **T12. Resource exhaustion on the auditing host** | ≤ 8 sources, a per-call timeout, a per-run PR-check cap, and a cap hit reported as could-not-look. Full mirrors are F1's price, and their size is unmeasured. |
| **T13. A finding read as a verdict on meaning, or a stale run read as current** | The closed severity table, judgement kept out of the exit code, and every finding quoting its fetched commit and time. |
| **T14. A committed fixture leaks a real repository** | Run-time synthetic worlds, `leakscan` in CI, no captured text. |
| **T15. A torn or hand-damaged journal** | Arcs' read rule: damaged lines are skipped and COUNTED, and the torn tail is never applied. **As built (S2), the GET reports the damage as a FACT, never as a count**: the body carries `damaged=yes` and a fixed sentence saying a declaration written only by a damaged line is not shown and an older one may be shown in its place; the counts (`Skipped`, torn tail) go to the pod's stderr. The reason is arcs' own: a damaged line cannot be attributed to a scope (it did not parse), so a count is over EVERY scope's lines — including scopes the caller cannot read — and putting it on the wire would leak activity in hidden scopes. A scope whose latest line was damaged falls back to its previous valid record. *(Revision 6 said "the GET reports the damage count"; superseded by the build for the reason above.)* |

## Slices

Each slice is mergeable alone, leaves `main` releasable, and has tests that run on that slice
alone.

| slice | what | ledgers it moves | mergeable alone because |
|---|---|---|---|
| **S1** | **`internal/codesrc`**: `Parse`/`Canonical` (decision 2); `Journal` with `Read`/`fold`/`Set` and the three read states (decision 1); `Key` (decision 4); `ResolveJournalPath` reuse with its own refusal message. | new package → `go` job `ok` floor; `tests/control_mutants.py` `PKGS` plus its pinned count. | A library nothing imports. |
| **S2** | **Pod**: `CAIRN_SOURCE_JOURNAL` (read-only open, inside-store refusal shown RED first); `GET /api/v1/sources/<scope>` (decision 10). | `internal/api/routes.go` `readHeads`; `requests.json` `go_only` rows plus `record-go-only` goldens; `capability_ledger`; `want-go-only.txt`; `checks.go-server-declares-its-routes`; T9's pin 3; README rows. | Unset means `sources-unconfigured`, and nothing writes the journal yet. |
| **S3** | **The proposed `sources` verb** (read-only, Go-only). | `internal/client` verb table → `-verbs`; `want-go-only-verbs.txt`; `capability_ledger` `go_only` row. | An additive read verb. Needs S2. |
| **S4** | **UI**: `CAIRN_SOURCE_JOURNAL` (inside-store refusal); the Sources block; `GET`/`POST /scope/sources` (decision 5); the `internal/ui` half of T9's ledger. | `internal/ui/routes.go` table; hand ledger and near-miss probes; the uiaudit world gains a seeded sources journal; mutant rows; `internal/ui/README.md` (including the deployment requirements of decision 1). | Unset means the page renders the unconfigured state. |
| **S5** | **`internal/refaudit` + the proposed `audit-refs` verb** (decisions 6–9); the import-graph test (clause e); `tests/refaudit/e2e.sh` with clauses (a)–(f) and `--self-test` (`sabotaged=6`); its `go`-job step. | new package → `ok` floor and `PKGS`; `internal/depspolicy` (the pod/UI ban); verb ledgers as in S3; `ci.yml`; README. Clause (a) needs S2 and S3. | A read-only verb that writes only its own mirror cache. |

**Slice ↔ closing condition ↔ self-test, checked against each other:**
- Part 3 of the closing condition (the conformance rows) lands in **S2**.
- `e2e.sh` and all six clauses land in **S5**, which comes after S2 and S3. `sabotaged=6` is
  pinned once, in S5.
- **Part 4 (the cross-binary seam test) lands in S4**, which comes after S2, since it needs the
  pod's GET.
- **S1 and S4 have no e2e clause.** Their witnesses are their own tests: S1's same-revision race
  and three read states, and S4's POST, gates, T9 pins and the part-4 seam test.

**Mutant rows** (indicative names; they join `tests/control_mutants.py` and move its pinned
count). Recounted for this revision: **31** (S1 13, S2 3, S4 4, S5 11).

- **S1 (13):**
  - `codesrc-accepts-a-non-dns-host`
  - `codesrc-branch-may-lead-with-a-dash`
  - `codesrc-whitespace-in-branch-accepted`
  - `codesrc-repo-path-case-folded`
  - `codesrc-subpath-dotdot-accepted`
  - `codesrc-duplicate-refused-not-deduped`
  - `codesrc-order-not-kept`
  - `codesrc-revision-compared-outside-the-lock`
  - `codesrc-stale-revision-accepted`
  - `codesrc-fold-earliest-wins`
  - `codesrc-damaged-line-refuses-whole-journal`
  - `codesrc-unknown-field-refused`
  - `codesrc-key-not-normalised`
- **S2 (3):**
  - `api-sources-get-distinguishes-absent-from-unreadable`
  - `api-sources-journal-inside-store-accepted`
  - `api-sources-unreadable-journal-answers-200`
- **S4 (4):**
  - `ui-sources-post-skips-the-admin-check`
  - `ui-sources-links-a-non-github-host`
  - `ui-sources-set-by-taken-from-the-form`
  - `ui-sources-keyed-by-control-id`: revision 3's design, which closing-condition part 4 must
    kill
- **S5 (11):**
  - `refaudit-tags-count-as-reachable`
  - `refaudit-off-branch-counted-stale`
  - `refaudit-not-reachable-counted-stale`
  - `refaudit-presence-counted-as-reachability`
  - `refaudit-ls-tree-exit-code-trusted`
  - `refaudit-directory-counted-missing`
  - `refaudit-literal-pathspecs-dropped`
  - `refaudit-undeclared-scope-exits-0`
  - `refaudit-checked-zero-exits-0`
  - `refaudit-unmapped-host-counted-clean`
  - `refaudit-mirror-dir-from-path-components`

Revision 2's `write-sources-*` and README-splice rows are deleted with the machinery. So are
`refaudit-closed-pr-escalates` and `refaudit-host-map-bypassed`: the first is subsumed by the
severity table's key-set test, and the second by clause (d)'s mixed case. The T9 ledgers are
grow/shrink tests, not battery rows. Git-dependent rows follow the two-tier rule: KILLED under
`CAIRN_GIT_TESTS_REQUIRED`, and reported skipped by name where `git` is absent, never reported as
killed.

### Test plan per slice (negative controls named)

**S1.**
- **Parse.** Every example maps to a LITERAL canonical string. Each refusal has a fixture valid in
  every respect but one, and the test asserts that refusal's OWN message. The refusals:
  - a scheme;
  - userinfo;
  - a port;
  - a one-segment repo path;
  - a `..` subpath;
  - a missing `@`;
  - a branch with a leading `-`;
  - a branch with a space;
  - a comma;
  - a control character;
  - nine sources.

  Fixtures are pairwise distinct and distinct from every constant the assertion names.
- **The same-revision race (F2).** Two `Set` calls carry the SAME revision. `interleave` holds the
  first between its compare and its append while the second starts, and the second blocks on the
  lock. EXACTLY ONE line is appended. The other call returns `*StaleRevisionError` carrying the new
  revision, and the fold shows the first call's list.
  - **Negative control:** the mutant `codesrc-revision-compared-outside-the-lock` makes both land.
    The test must go red on the appended-line COUNT (2 ≠ 1).
  - **As built (S1):** the mutant moves the `interleave` seam out of the lock WITH the compare (the
    seam marks the compare-to-append window), so call two CAN reach the seam before call one
    appends; call one waits for it with a 300ms deadline. ⚠ The kill still depends on that deadline:
    call two must arrive within it (round 1 measured a 1µs deadline turning the mutant SURVIVED 20 of
    20). Measured at 300ms: 10 of 10 runs red on the count, each in 0.08–0.22s. Too short a deadline
    fails LOUD (a SURVIVED row in the battery), never as a false green on the real code.
- **Fold.** Two records for one scope: the later wins, and reversing the fold goes red. A torn tail
  and a non-JSON line are skipped and counted. A record with an unknown field folds; that is an
  **invariant guard**, labelled, because no build writes one yet.
- **`Key`.**
  - `Alpha-Notes` and `alpha-notes` give one key, as a literal.
  - A renamed scope's new name reads `undeclared` and the old record survives in `Read`.
  - A recreated same-name scope reads the old record.
- **The three read states.** Each is pinned by a literal:
  - unset → off;
  - a DIRECTORY at the path → `*JournalUnreadableError`;
  - an absent file → `Missing`.
- **Two refusals:**
  - the inside-store refusal names the SOURCES journal, never the arc journal;
  - a symlink at the journal path is refused (`O_NOFOLLOW`).

**S2.**
- **RED first:** a journal inside the store root refuses startup with its own message. Two
  controls come up: a path outside the root, and the variable unset.
- **The read-only open.** The pod's journal descriptor is opened `O_RDONLY`. A test hands the pod a
  journal file with mode `0444` and a GET still answers. The negative control is the same test with
  `Set` reachable from `internal/api`; T9's ledger refuses it.
- **Authz and state matrix, with literal bodies** (the Go witnesses for what the corpus cannot
  send):
  - `read` → 200;
  - no grant → byte-identical to an absent scope's answer;
  - unconfigured → `sources-unconfigured`;
  - a directory at the journal path → **exactly** `503`, `X-Store-Status: store-unreachable`,
    `X-Store-Exit: 3`, `text/plain`, and a body that is the `*JournalUnreadableError` text,
    pinned literally. The mutant `api-sources-unreadable-journal-answers-200` turns that into a
    200 and must go red. **The client half**, an `internal/client` test: the proposed
    `audit-refs` verb, handed that exact response from a stub, exits **10** with
    `sources-unreadable`, NOT 3. The proposed `sources` verb exits 3;
  - an absent file → `sources=undeclared journal=absent`.
- **Corpus.** `run_go.sh` exits 0 with the new rows. `record-go-only` refuses if no 2xx is seen.
  `TestTheRouteLedgerMatchesTheConformanceCorpus` goes RED with the table row present and the
  corpus row absent (shown once).

**S3.**
- The verb prints the canonical list, `sources=undeclared` or `sources-unconfigured`, each as a
  literal.
- `tests/test_go_client_ledgers.py` goes red if `want-go-only-verbs.txt` omits it (shown once).

**S4.**
- **A valid POST** appends exactly one line, stamped with the signed-in principal and the UI's
  clock. A forged `set_by` form field is IGNORED, caught by the mutant
  `ui-sources-set-by-taken-from-the-form`.
- **A stale revision** appends nothing (line count before = after) and re-renders the CURRENT
  list.
- **An invalid line** is refused naming its line number, with the text preserved.
- **An empty textarea** appends `sources: []`.
- **Gates:**
  - no CSRF → 403;
  - cross-origin → refused (asserted);
  - a `write`-only principal → the share flow's refusal;
  - a token-file deployment → the refusal for every principal.
- **Links.** A `github.com` source renders exactly one `href`, equal to the literal
  `https://github.com/example-org/example-repo/tree/main`. A `git.example.com` source renders none.
- **T9's pins** go red, each shown once, when:
  - a raw `os.WriteFile` is added to an `internal/ui` handler (pin 1);
  - a call to `write.CreateEntry` is added (pin 2);
  - a second `Journal.Set` call site is added (pin 2).
- **The cross-binary seam (closing-condition part 4; round 3 🔴1).**
  `TestAUIWrittenDeclarationIsWhatThePodServes` lives in an external test package, so it can
  import both `internal/ui` and `internal/api`. It runs over one temp journal and one store
  directory `alpha-notes`:
  - **the UI half** is built over a control-JOURNAL model in which `alpha-notes` has a RANDOM ID,
    and holds an admin session. It POSTs two sources;
  - **the pod half** is built with `api.New` over a TOKEN-FILE projection, so `alpha-notes` has a
    DERIVED ID, the way `cmd/cairn-server/main.go:358` builds it. It GETs `alpha-notes`.

  It asserts the GET body carries exactly the two sources. **RED under
  `ui-sources-keyed-by-control-id`**: the GET answers `sources=undeclared`, because the two IDs
  differ. The test also asserts that precondition, so it cannot go green by the two fixtures
  happening to share an ID.

**S5.**
- **The e2e.** Clauses (a)–(f), and `--self-test` → `sabotaged=6 caught=6`.
- **Unit tests over the state table.** One synthetic world built in the test (git tests follow the
  two-tier rule):
  - a file;
  - a directory;
  - a file under a subpath;
  - a commit on `main`;
  - a commit only under `refs/pull/1/head`;
  - two commits sharing a 7-hex prefix, made by brute-forcing commit timestamps, with the run time
    named;
  - the loopback stub answering open, merged, closed and 404.

  Each planted claim maps to ONE literal state. **The severity table's key set EQUALS the emitted
  state set**, and adding a state without classifying it goes red.
- **Paths (round 2's 🟡2).** Each spelling maps to a literal:
  - `./cmd/x.go` → `holds(root)`;
  - `internal/` → `holds(root)`, a directory;
  - `:(top)x` and `*.go` are looked up literally;
  - a missing path → `missing`, even though `ls-tree` exits 0. The mutant
    `refaudit-ls-tree-exit-code-trusted` flips it.
- **Reachability (round 2's 🟡3).** After run 1 fetches a SHA, its only ref is deleted on the
  remote. Run 2 reports `not-reachable`. Under the mutant `refaudit-presence-counted-as-reachability`
  it reports `exists-off-branch`, because the object is still in the mirror, and the test goes red.
- **Tags (round 3 🟡1).** On the remote, a tag `v1` points at a SHA whose only branch is then
  deleted. The run reports `not-reachable`. Under `refaudit-tags-count-as-reachable` (dropping
  `--no-tags` and the namespace restriction together) it reports reachable, and the test goes red.
  A second case pre-plants `refs/tags/v1` in an existing mirror, with `--no-tags` kept, to prove
  the namespace restriction ALONE also holds.
- **Exit codes.**
  - Undeclared, `checked=0` and mixed mapped/unmapped each exit 10 — never 0 and never 2.
  - One stale claim beside one unmapped host exits 9.
- **The host map.** An unmapped host is never contacted (the stub counts 0). A mapped one is
  (> 0); that is the pair.
- **Once, by the operator, not in CI:** the proposed `audit-refs` verb over a scope citing a PR in
  this public repository, run with and without a token. Record the states.

## Round-0 dispositions (carried from revision 2)

| # | deletion | applied |
|---|---|---|
| D1 | the CLI `sources set` verb, the pod `PUT` and `If-Match`/412 | REMOVED. The UI is the only writer (also O5). The form's revision is F2's requirement, not an API precondition. |
| D2 | the S6 (stored findings) and S7 (recall header) slice rows | REMOVED. Q4 and Q5 stay. |
| D3 | B1, B2, B4 | REMOVED. B3 kept. |
| D4 | the `path:line` check | REMOVED. |
| D5 | `HEAD /api/v1/sources` | No separately designed HEAD route. The ledger's derived HEAD is Q16 (decision 10). Revision 2's extension of D5 to GET is RETRACTED: O5 restores the GET. |

## Round-2 findings → where each is fixed

| finding | fix |
|---|---|
| 🔴 the store is a re-seeded copy; README storage would make the UI a whole-store writer | O5: storage moved out of the store tree (decision 1, T7, T9). |
| 🟡1 T9's "one write function" premise is false | Measured the UI's actual `internal/write` calls (three pure helpers). T9 restated as "the UI writes only the two journals", pinned by two grow/shrink ledgers. *(That restatement was itself false; it is superseded by round 3 🟡2, below.)* |
| 🟡2 path normalisation, `-z`, directories | Decision 6's path row and S5's path tests. |
| 🟡3 presence vs reachability, mirror age | `for-each-ref --contains` over this run's refs, `fetch --prune`, clause (f), the stated squash-and-delete and force-push costs. "Harmlessly" is gone. |
| 🟡4 one resolver; ID-keying behaviour | `codesrc.Key` (decision 4). Revision 3's ID keying is retracted by round 3 🔴1, below. |
| 🟡5 a closed severity table | Decision 6's five-class table, the scope-level reasons, the mixed case, and the key-set test. |
| 🟢 BOM, CRLF, create semantics | **Moot, confirmed.** They were properties of the README splice, which is deleted. The journal is JSONL written only by `codesrc`. The textarea is split on `\n` with a trailing `\r` stripped per line, and any other whitespace in a line is a parse refusal. |
| nit: mutant count | Recounted: 28 (S1 13, S2 2, S4 3, S5 10) in revision 3; 31 after round 3. |

## Round-3 findings → where each is fixed

| finding | fix |
|---|---|
| 🔴1 the UI's and the pod's scope IDs come from different authorities, so the pod never finds the UI's record | Records are keyed by `codesrc.Key(name)`, the normalised directory name (decision 4). Rename orphans the record and recreate re-attaches it, stated plainly. The per-deployment table is deleted. A cross-binary seam test is closing-condition part 4, RED under the mutant `ui-sources-keyed-by-control-id`. The departure from O5's keying detail is Q17. |
| 🟡1 tags auto-follow and survive `--prune`, giving a false clean exit | `--no-tags`, and `for-each-ref --contains` restricted to `refs/heads/ refs/pull/` (decisions 6 and 8). Clause (f) gains a kept-tag case. New mutant `refaudit-tags-count-as-reachable`. A test proves the restriction alone holds against a pre-existing tag. |
| 🟡2 T9's "only the two journals" is false | Lists all four files the binary writes (STEP 2, T9). Pin 1 is widened to "zero direct file-writing `os` calls in `internal/ui`", which catches a raw `os.WriteFile`. What the pins cannot see is stated. The 5/2/1 counts are corrected to 2/1/0 (non-comment code lines). |
| 🟡3 no answer for a configured-but-unreadable or absent journal | Arcs' three states (decision 1). A broken journal answers arcs' 503 `store-unreachable` (round 4 corrected the token; see the round-4 table). An absent file means empty, with `journal=absent` on every response, and the wrong-path risk is stated (it fails safe, at exit 10). The 503 is witnessed by a literal-body `internal/api` test, because the one-boot corpus cannot send it beside the authorised rows (decision 10, Q18). |
| 🟡4 a fail-on-absent backup is red from deploy day | Absence is a PASS, logged `sources: absent`, following the client instance's arcs backup. The reason, and the cost, are stated (decision 1). |
| 🟢1 no dual-run ledger exists | The list is corrected to `routes.go:67-71`'s four places. The dual-run gate is named as BLIND to the head. |
| nits (round 3) | The refusal names the sources journal, pinned. The `internal/ui/README.md:654` citation is dropped. T6 notes that a resubmitted unchanged list is a harmless no-op. `Set` opens with `O_NOFOLLOW`, and a test refuses a symlink. |

## Round-4 findings → where each is fixed

| finding | fix |
|---|---|
| 🟡1 the broken-journal 503 had two contradictory spellings | **One wire answer: arcs' `storeUnreachable`.** That is `503`, `X-Store-Status: store-unreachable`, `X-Store-Exit: 3`, and the error text as the body. No new token. The proposed `audit-refs` verb maps it to `sources-unreadable`, exit 10, as `arcsCheck` does. The proposed `sources` verb gets the plain-read exit 3. *(Round 5 corrected which mechanism produces each code.)* The literal-body `internal/api` test pins the exact response, and an `internal/client` test pins the 10 (decisions 1, 6 and 10; S2 test plan). |
| 🟢1 T9 pin 1 was narrower than its sentence | Pin 1 now covers every PACKAGE-LEVEL `os` mutator, classified in the test, and failing on an unclassified one. *(Round 5 narrowed this sentence further; see below.)* |
| 🟢2 "three mechanical parts" with four listed | Fixed to "Four". A sweep finds no other three-parts mention. |
| *(round 5)* 🟡1 pin 1 claimed "every `os` mutator", but methods are invisible to it | The claim is narrowed to PACKAGE-LEVEL `os` functions, with `CopyFS` added. The set is read by parsing `$(go env GOROOT)/src/os`. "Methods on `*os.File`/`*os.Root`, and their reach via `os.OpenRoot`" now heads T9's "do NOT see" list, citing the no-importer limit (`membershipledger_test.go:36-43`). |
| *(round 5)* 🟢1 the wrong mechanism was cited for exit 3 | The 3 comes from `FetchReport` → `*StoreUnreachable` → `cli.go:661-676` → `ExitUnreachableNoCache`. `X-Store-Exit` is unread on a 503. The 10 follows the `arcsCheck` precedent (`arcs.go:143-145`, `:166-169`). |
| nits (round 4) | T9 and STEP 2 list the session store's `.lock` sidecar. "A rename orphans" now reads "a DIRECTORY rename", and says a control-journal `scope-renamed` alone detaches the display name from its directory. On the UI, the STORE INDEX enumerates directories, not the journal authority. |

## Open questions

### Answered by the operator

| Q | question | answer |
|---|---|---|
| Q1 | Where are sources stored? | First the README front matter (O4); now **an append-only journal outside the store, UI-written and read-only to the pod (O5)**. |
| Q17 | Key the journal by scope NAME rather than by scope ID, departing from O5's detail? | **Yes (paraphrased).** Records are keyed by the normalised scope name: `codesrc.Key` is `store.NormalizeRef` of the store directory name. A directory rename orphans the declaration, and an admin re-declares it. A delete followed by a recreate under the same name re-attaches the old declaration, and the page shows who declared it (`set_by`/`set_at`). O5's "keyed by scope ID" is SUPERSEDED by this answer. Recorded in S1. |

### Still open — each with the agent's recommendation

- **Q2. REMOVED (O5).** Nothing in the store changes, so the Go/Python reading question is gone.
- **Q3. Where the auditor runs.** **Recommend** the proposed `audit-refs` verb on an operator host,
  on a user timer, over `internal/refaudit`. A #208 plugin may wrap it later. Never the pod.
- **Q4. Are findings stored?** **Recommend print-only first.**
- **Q5. A source line in every recall?** **Recommend no.** Yes would mean the pod's renderer
  reading the journal, and a declared Go-vs-oracle divergence.
- **Q6. The UI input control (a FORK).** **Recommend one textarea, one source per line.**
- **Q7. Path base in a monorepo.** **Recommend** root-relative, then subpath-relative, reporting
  which held.
- **Q8. Symbols.** **Recommend the structural `path#Symbol` form only.** A heuristic over prose is
  the shape `openness.go:33-40` rejects.
- **Q9. The default host map.** **Recommend `github.com=https://github.com` only.**
- **Q10. Who may edit.** **Recommend `admin`.**
- **Q11. A `RESOLVED` SHA found only off-branch.** **Recommend informational.**
- **Q12. Orphaned records** (left by a rename). **Recommend showing nothing in v1.** The journal
  keeps them as history. A later admin view could list records whose name matches no scope
  directory.
- **Q13. A per-entry source override.** **Recommend deferring** until a measured case exists.
- **Q14. The ≤ 8 cap.** **Recommend keeping it.**
- **Q15. The deployment changes (rewritten for O5).** They are:
  - the pod mounting the UI's volume read-only, at the CONTAINER level, which co-schedules it on
    the UI's node (RWO);
  - `CAIRN_SOURCE_JOURNAL` on both pods;
  - the backup jobs staging `sources.jsonl` beside `arcs.jsonl`.

  **Recommend** shipping all three in the deploy that ships S2 and S4.
- **Q16 (new). The derived HEAD.** **Recommend letting the ledger derive `HEAD` for the new head**
  (decision 10). The other reading of D5 is a GET-only exception in a ledger with one rule.
- **Q17. ANSWERED — see "Answered by the operator".** The recommendation (name) was confirmed.
  The reasoning that led to it: it is the only identifier the UI (journal-backed authority) and
  the pod (token-file authority) share; the alternative is unifying the two authorities' ID
  spaces, a control-plane change far outside this feature.
- **Q18 (new, round 3 🟡3). A second corpus boot for the 503 and unconfigured rows.** **Recommend
  no.** They are witnessed by literal-body Go tests, which is the contract witness for a Go-only
  route anyway, and a second boot in the runner is a new mechanism.

## Recommended improvements beyond the ask (clearly recommendations)

- **B3. A census mode for the proposed `audit-refs` verb (`--census`).** Counts only, per scope:
  path-shaped Pointers rows, `RESOLVED` SHAs, `github:` refs and structural `path#Symbol` rows.
  It measures how much of the store the checks can reach, before Q4. It prints no entry text.

## What could not be measured

- **Any real store** (private, and no captured text comes here): how many entries carry
  checkable claims, and how many scopes document one repository. B3 measures that on the
  operator's host.
- **The private audit tooling's hit rates.** It was read by mechanism only.
- **The deployed volumes:**
  - whether the pod can co-mount the UI's volume read-only on each instance's storage driver
    (arcs measured one driver's claim-level failure; the other is unmeasured for this direction);
  - whether `flock` on that volume is node-local, as decision 1 assumes.
- **Full-mirror sizes and fetch times** for real repositories (T12). Round 1's numbers were on a
  synthetic world.
- **Whether `gitMinimal` on the client's wrapper fetches over https.**
- **GitHub's API today**: tokenless rate limits, and `/pulls/N` against `/issues/N` for a
  converted issue.
- **#208's final shape.** It is unmerged, and decision 9 is written so that does not matter.
- **Sizes and effort.** Not estimated.
