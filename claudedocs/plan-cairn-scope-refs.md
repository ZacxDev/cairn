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

- **closing-condition:** `check`. Three mechanical parts, all required:
  1. Slices S1–S5 are MERGED on cairn `main`, verified by content, not by ancestry.
  2. **`tests/refaudit/e2e.sh` exits 0 on `main`.** It runs in the `go` CI job, which sets
     `CAIRN_GIT_TESTS_REQUIRED` (`.github/workflows/ci.yml:722`), and the step is NOT
     `continue-on-error`.
  3. **`tests/conformance/run_go.sh` exits 0 with the new `go_only` rows present**, and
     `python3 tests/conformance/suite.py run` still reports 0 failures. The oracle skips those rows
     by id, counted, with their reason.

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
  | **(f) reachability, not presence** | a `RESOLVED` SHA reachable ONLY from `refs/pull/1/head` reports `exists-off-branch` (informational), and the exit is unaffected. After that ref is DELETED on the remote and the run repeats, the SHA reports `not-reachable` (could-not-look, exit 10), even though the previous run left the object in the mirror | the run with the pull refspec removed also reports `not-reachable` |

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
  - S2 and S4 are covered by their own Go tests, not by `e2e.sh`. That is stated so nobody reads
    the six clauses as covering the UI write.

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
- **The write.** An exclusive `flock` covers the whole read-merge-append. There is one `write(2)`
  under `O_APPEND`, then `fsync` (`internal/arcs/journal.go:160-191`). Its comment names the limit:
  `flock` is advisory-only across hosts on a network filesystem (`:167`).
- **The read.** A missing file is an empty snapshot flagged `Missing` (`:86-96`). Damaged lines are
  skipped and counted, and a torn tail is decided by the last byte (`:98-105`, `Snapshot.Damaged`
  at `:65`). That tolerance is right for non-authority data.
- **Provenance.** The pod stamps who pushed and when; every other field is the writer's own word
  (`arcs.go:15-19`).
- **The deployment facts arcs already carry:**
  - the journal is BACKED UP on both instances: staged under `arcs/` by the personal instance's
    UI backup job, uploaded beside the archive by the client instance's backup job
    (`claudedocs/handoff-cairn-arcs-sessions.md:46-50`);
  - a second pod mounting a ReadWriteOnce, node-local volume must run on the SAME node;
  - on one storage driver, a claim-level `readOnly` breaks co-mounting, so read-only is enforced on
    the CONTAINER mount (`handoff-cairn-arcs-sessions.md:122-126`; `internal/ui/README.md:654`).
- **Go-only.** The arc routes are Go-only by decision (`internal/api/routes.go:67-77`). Four
  places move together:
  - that table;
  - the `go_only` rows of `tests/conformance/requests.json`;
  - the `go_only` rows of `tests/testlib/capability_ledger.py` (`:39-43`, `:112-128`);
  - `flake.nix`'s `want-go-only.txt`.

  A Go-only golden is a change detector (`tests/conformance/README.md`, "…and the mirror"). The
  dual-run gate DECLARES such a head go-only rather than sending it (same README, the `flagParam`
  paragraph).
- ⚠ **Every read head is served on GET AND HEAD.** The ledger expands each head over
  `safeReadMethods` (`routes.go:84`, `:96-102`). So "one GET route" (O5) and D5 ("no HEAD") are
  reconciled in decision 10.

### Scope identity: two authorities, two ID rules

- The control model's `Scope` has an immutable `ID`, a mutable `DisplayName` and a `ProjectID`
  (`internal/control/model.go:228-243`).
- **Journal-backed deployment.**
  - Scope IDs are RANDOM at creation (`internal/control/provision.go:113`, `NewID`).
  - A rename is a `scope-renamed` event that keeps the ID (`internal/control/journal.go:22`).
  - There is no scope-deletion event (none found by `grep` over `internal/control`).
- **Token-file deployment.** Scope IDs are DERIVED from the folded directory name
  (`internal/control/tokenfile/source.go:580-586`, `DerivedID`).
- **Name to ID.** `Model.ScopeByName` REFUSES a name that two projects share rather than picking
  one, because picking would be a cross-tenant read (`model.go:450-480`).
- **Authorisation is keyed on the ID** (`internal/control/resolve.go:89`).

### The browser surface: the share flow is the edit-flow precedent

- UI routes are a declared table (`internal/ui/routes.go:118-185`). `POST /share` and
  `POST /unshare` are scope-administration writes (`:147-149`).
- Both cross-site gates derive from the METHOD (`stateChanging`, `internal/ui/server.go:1323`):
  same-origin before auth, and a per-session CSRF token after it (`:1305-1306`).
- The share handler authorises with `id.Auth.Allows(scope, control.VerbAdmin)`
  (`internal/ui/sharehandlers.go:170`).
- **The UI's filesystem writes today are the control journal only.** It must resolve outside
  `-store` (`cmd/cairn-ui/main.go:246-266`).
- **The UI calls three `internal/write` functions, and all are pure string helpers:**
  `write.SessionComponent` (5 sites), `write.WithoutTrailers` (2) and `write.Attribution` (1).
  This was MEASURED by `grep` over `internal/ui/*.go`, excluding tests. ⚠ Revision 2's T9 premise,
  "the UI calls exactly one write function", was false (round 2's 🟡1). It is restated in T9.

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
| O5 | **An append-only journal OUTSIDE the store tree, on the arc registry's pattern.** The UI is its ONLY writer. Records are keyed by scope ID, not by name. The store pod mounts it READ-ONLY and serves one Go-only GET route, which the CLI auditor reads. The reasons the operator accepted: README edits would be silently reverted by the next re-seed; README storage would reverse the decision to keep the UI's store mount read-only; and the journal gives a who/when change history for free, which an audit feature wants. | Decisions 1, 4, 5, 10. |

### Chosen by the AGENT writing this plan (open to review)

1. **The storage contract under O5: a new package, `internal/codesrc`, that is the journal's ONE
   validator, reader and writer, as `internal/arcs` is for arcs.**
   - **Location and configuration.**
     - The env var is `CAIRN_SOURCE_JOURNAL`. There is **no flag**: a pre-feature binary handed an
       unknown flag refuses to start, but it ignores an unknown env var, so the env var is the
       rollback story.
     - It has no default. Unset means the pod answers `sources-unconfigured` and the UI renders
       that state on the page.
     - Both binaries refuse to start if the path resolves inside the store root, reusing
       `arcs.ResolveJournalPath`.
     - The file lives on the UI-owned volume, beside the control journal
       (`handoff-cairn-control-plane.md:502-505`).
   - **Records.** One JSON line each: `{schema, scope_id, scope_name_at_write, sources:[…],
     set_by, set_at, revision}`.
     - `set_by` is the signed-in principal and `set_at` is the UI's clock. Both are stamped by the
       UI, never taken from the form.
     - `scope_name_at_write` is for humans reading the journal. It is never used as a key.
     - `revision` is a digest of the record's `scope_id` plus `sources`.
     - The fold is LATEST-WINS per `scope_id`. The journal is the history.
   - **Writing (R1's F2, re-specified for O5).** `Journal.Set(scopeID, sources, ifRevision, by,
     now, interleave)` runs entirely under ONE exclusive `flock`:
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
     (`internal/write/write.go:284-286`; `write_test.go:188-215`).
   - **Reading.** `Journal.Read()` follows arcs exactly: a missing file is empty plus `Missing`;
     damaged lines are skipped and counted; a torn tail is never applied. An UNKNOWN FIELD in a
     record is ignored, not refused, so a record written by a newer build still folds in an older
     one.
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
       step, and fail if it is absent once configured.

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

4. **Who may edit, and the ONE resolver (round 2's 🟡4).**
   - **Edit:** a principal holding `admin` on the scope. That is the share flow's verb
     (`sharehandlers.go:170`), and only the UI asks for it.
   - **Read:** a principal holding `read`. The GET answers uniformly for "not visible" and
     "absent".
   - ⚠ **A token-file deployment confers `admin` on nobody** (`tokenfile/source.go:424-425`), so
     nobody can edit there. Stated so nobody files it as a bug.
   - **The resolver.** `codesrc.KeyFor(model control.Model, scopeName string) (control.ID, error)`
     is the ONE function from a scope name to the journal key. It is called by the UI's GET and
     POST, the pod's GET handler, and nothing else. The proposed `sources` and `audit-refs` verbs
     send a NAME and the pod resolves it, so the CLI never derives an ID.
     - It folds the name with `store.NormalizeRef`, the directory rule.
     - It resolves with `Model.ScopeByName`, which REFUSES a name two projects share
       (`model.go:450-480`). That refusal surfaces as a could-not-look reason, never as a guess.
   - **What ID keying means in each deployment, pinned by S1 tests in both:**

     | event | journal-backed deployment (IDs random, `NewID`) | token-file deployment (IDs derived from the name) |
     |---|---|---|
     | scope renamed | **sources follow**: the `scope-renamed` event keeps the ID | **sources are orphaned**: the new name derives a new ID, so the scope reads `undeclared` and the old record stays in the journal unreferenced |
     | scope recreated under the same name | **sources do NOT follow**: a fresh `NewID`, so the scope reads `undeclared` | **sources re-attach**: same name, same derived ID |

     An orphaned record is history, not a fault. The UI shows nothing for it in v1 (Q12).

5. **The edit flow: the UI is the ONLY writer (D1, O5).**
   - **Routes.** `GET /scope/sources?scope=<s>` renders a form and `POST /scope/sources` writes.
     Both are rows in the UI's declared table.
     - Same-origin and CSRF come from the method (`server.go:1305`, `:1323`).
     - Authorisation is decision 4.
     - With the journal unconfigured, the page renders that state, as `ReadOnlyAuthority` does.
   - **The form.** ONE `<textarea>`, one source per line (Q6), plus a hidden `revision`.
     - The POST calls `codesrc.Journal.Set` with the resolved ID and the form's revision.
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
   | **commit** — `RESOLVED <sha>:` (`openness.go:47-53`) | **Reachability, never object presence (round 2's 🟡3):** `git rev-parse --disambiguate`, then `git for-each-ref --contains <sha>` over the refs fetched IN THIS RUN. `on-branch` if the declared branch's ref contains it; `exists-off-branch` if only another fetched ref does; `not-reachable` if none does. | `on-branch` · `exists-off-branch` · `not-reachable` · `ambiguous` |
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

   Plus three SCOPE-level could-not-look reasons, each exit 10: `sources=undeclared`,
   `sources-unconfigured`, and `checked=0`.

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
     `git fetch --prune` IN FULL (no partial-clone filter), using these refspecs:
     - `+refs/heads/*:refs/heads/*`;
     - `+refs/pull/*/head:refs/pull/*/head`, which on a host with no pull refs fetches nothing.

     Reachability is computed only over the refs that fetch left (decision 6). An object
     surviving in the mirror from an earlier run never counts.
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
      - `sources-unconfigured` when the env var is unset;
      - for a principal who cannot read the scope, a body byte-identical to an absent scope's;
      - a scope with no record returns `sources=undeclared`;
      - otherwise the latest record's canonical sources, `set_by`, `set_at` and `revision`.
    - **⚠ HEAD vs D5.** D5 removed a separately designed HEAD route. The route ledger nonetheless
      derives `HEAD` from every read head (`routes.go:84`, `:96-102`). **Recommend letting the
      ledger derive it:** no extra code, and the corpus's existing head-matches-get relation
      covers it. The alternative, a per-head GET-only exception, adds a second rule to a ledger
      that has one (Q16).
    - **The ledgers it moves:**
      - `readHeads`;
      - `go_only` rows in `tests/conformance/requests.json` (authorised, unauthorised,
        unconfigured, refused-equals-absent), recorded by `run_go.sh record-go-only` with the
        journal path handed over the way `arc-journal=` is;
      - `capability_ledger` `go_only` rows;
      - `want-go-only.txt`;
      - `checks.go-server-declares-its-routes`;
      - the dual-run gate's go-only declaration for the head.
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
| **T6. A lost update between two admins** | The revision compare inside the lock: exactly one of two same-revision writes lands (S1, S4). |
| **T7. A re-seed silently reverting declarations** (round 2's 🔴) | The journal lives outside the store tree, on the UI's volume. `seed.sh` writes only the store copy (`seed.sh:313-316`). Pinned by the inside-store startup refusal. |
| **T8. A feature rollback breaks something** | An env var an old binary ignores, a separate file, no control-journal event, and an untouched store tree (decision 1). |
| **T9. The internet-facing UI's write reach** (round 2's 🟡1, restated) | **The UI writes only the control journal, which it already wrote, and the sources journal.** Its store mount STAYS read-only (`handoff-cairn-control-plane.md:502-505`). Two pins, both failing on GROW or SHRINK: (1) **a write-call ledger over `internal/ui`**: no call to any file-writing `internal/write` function (`AppendBullet`, `ReplaceEntry`, `CreateEntry`; an INVARIANT guard today, labelled as one), and exactly one call site of `codesrc.Journal.Set`; (2) **the same ledger over `internal/api`**: zero call sites of `Journal.Set`, so the pod cannot become a second writer. ⚠ These pin the CODE. The read-only store mount and the pod's container-level `readOnly` are deployment facts this repository cannot see. |
| **T10. The pod or UI spawns `git` or reaches a code host** | Clause (e). ⚠ It is structural on `exec` only, and cannot see a `net/http` CLIENT call. |
| **T11. Stored XSS through a source string** | gomponents `Text` only; the one href through `safeHref`; `Raw`/`Rawf` stay AST-banned. |
| **T12. Resource exhaustion on the auditing host** | ≤ 8 sources, a per-call timeout, a per-run PR-check cap, and a cap hit reported as could-not-look. Full mirrors are F1's price, and their size is unmeasured. |
| **T13. A finding read as a verdict on meaning, or a stale run read as current** | The closed severity table, judgement kept out of the exit code, and every finding quoting its fetched commit and time. |
| **T14. A committed fixture leaks a real repository** | Run-time synthetic worlds, `leakscan` in CI, no captured text. |
| **T15. A torn or hand-damaged journal** | Arcs' read rule: damaged lines are skipped and COUNTED, the torn tail is never applied, and the GET reports the damage count. A scope whose latest line was damaged falls back to its previous record, and the response says so. |

## Slices

Each slice is mergeable alone, leaves `main` releasable, and has tests that run on that slice
alone.

| slice | what | ledgers it moves | mergeable alone because |
|---|---|---|---|
| **S1** | **`internal/codesrc`**: `Parse`/`Canonical` (decision 2); `Journal` with `Read`/`fold`/`Set` (decision 1); `KeyFor` (decision 4); `ResolveJournalPath` reuse. | new package → `go` job `ok` floor; `tests/control_mutants.py` `PKGS` plus its pinned count. | A library nothing imports. |
| **S2** | **Pod**: `CAIRN_SOURCE_JOURNAL` (read-only open, inside-store refusal shown RED first); `GET /api/v1/sources/<scope>` (decision 10). | `internal/api/routes.go` `readHeads`; `requests.json` `go_only` rows plus `record-go-only` goldens; `capability_ledger`; `want-go-only.txt`; `checks.go-server-declares-its-routes`; dual-run go-only declaration; the `internal/api` half of T9's ledger; README rows. | Unset means `sources-unconfigured`, and nothing writes the journal yet. |
| **S3** | **The proposed `sources` verb** (read-only, Go-only). | `internal/client` verb table → `-verbs`; `want-go-only-verbs.txt`; `capability_ledger` `go_only` row. | An additive read verb. Needs S2. |
| **S4** | **UI**: `CAIRN_SOURCE_JOURNAL` (inside-store refusal); the Sources block; `GET`/`POST /scope/sources` (decision 5); the `internal/ui` half of T9's ledger. | `internal/ui/routes.go` table; hand ledger and near-miss probes; the uiaudit world gains a seeded sources journal; mutant rows; `internal/ui/README.md` (including the deployment requirements of decision 1). | Unset means the page renders the unconfigured state. |
| **S5** | **`internal/refaudit` + the proposed `audit-refs` verb** (decisions 6–9); the import-graph test (clause e); `tests/refaudit/e2e.sh` with clauses (a)–(f) and `--self-test` (`sabotaged=6`); its `go`-job step. | new package → `ok` floor and `PKGS`; `internal/depspolicy` (the pod/UI ban); verb ledgers as in S3; `ci.yml`; README. Clause (a) needs S2 and S3. | A read-only verb that writes only its own mirror cache. |

**Slice ↔ closing condition ↔ self-test, checked against each other:**
- Part 3 of the closing condition (the conformance rows) lands in **S2**.
- `e2e.sh` and all six clauses land in **S5**, which comes after S2 and S3. `sabotaged=6` is
  pinned once, in S5.
- **S1 and S4 have no e2e clause.** Their witnesses are their own tests: S1's same-revision race,
  and S4's POST, gates and T9 ledger.

**Mutant rows** (indicative names; they join `tests/control_mutants.py` and move its pinned
count). Recounted for this revision: **28**.

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
  - `codesrc-keyfor-picks-one-of-an-ambiguous-name`
- **S2 (2):**
  - `api-sources-get-distinguishes-absent-from-unreadable`
  - `api-sources-journal-inside-store-accepted`
- **S4 (3):**
  - `ui-sources-post-skips-the-admin-check`
  - `ui-sources-links-a-non-github-host`
  - `ui-sources-set-by-taken-from-the-form`
- **S5 (10):**
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
- **Fold.** Two records for one ID: the later wins, and reversing the fold goes red. A torn tail
  and a non-JSON line are skipped and counted. A record with an unknown field folds; that is an
  **invariant guard**, labelled, because no build writes one yet.
- **`KeyFor`, in both authorities.** Each is built with the existing fixtures: a journal-backed
  `Model` from events, and a token-file `Model` from the projection.
  - **The four rows of decision 4's table.** Rename and recreate in each deployment, with a
    literal expectation per cell.
  - **An ambiguous name** (two projects) is refused with `ScopeByName`'s own message.

**S2.**
- **RED first:** a journal inside the store root refuses startup with its own message. Two
  controls come up: a path outside the root, and the variable unset.
- **The read-only open.** The pod's journal descriptor is opened `O_RDONLY`. A test hands the pod a
  journal file with mode `0444` and a GET still answers. The negative control is the same test with
  `Set` reachable from `internal/api`; T9's ledger refuses it.
- **Authz matrix, with literal bodies.** `read` → 200. No grant → byte-identical to an absent
  scope's answer. Unconfigured → `sources-unconfigured`.
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
- **T9's `internal/ui` ledger** goes red when a call to `write.CreateEntry` is added, and when a
  second `Journal.Set` call site is added (each shown once).

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
| 🟡1 T9's "one write function" premise is false | Measured the UI's actual `internal/write` calls (three pure helpers). T9 restated as "the UI writes only the two journals", pinned by two grow/shrink ledgers. |
| 🟡2 path normalisation, `-z`, directories | Decision 6's path row and S5's path tests. |
| 🟡3 presence vs reachability, mirror age | `for-each-ref --contains` over this run's refs, `fetch --prune`, clause (f), the stated squash-and-delete and force-push costs. "Harmlessly" is gone. |
| 🟡4 one resolver; ID-keying behaviour | `codesrc.KeyFor` (decision 4), with the rename/recreate table for both deployments. |
| 🟡5 a closed severity table | Decision 6's five-class table, the scope-level reasons, the mixed case, and the key-set test. |
| 🟢 BOM, CRLF, create semantics | **Moot, confirmed.** They were properties of the README splice, which is deleted. The journal is JSONL written only by `codesrc`. The textarea is split on `\n` with a trailing `\r` stripped per line, and any other whitespace in a line is a parse refusal. |
| nit: mutant count | Recounted: 28 (S1 13, S2 2, S4 3, S5 10). |

## Open questions

### Answered by the operator

| Q | question | answer |
|---|---|---|
| Q1 | Where are sources stored? | First the README front matter (O4); now **an append-only journal outside the store, UI-written and read-only to the pod (O5)**. |

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
- **Q12. Orphaned records** (a token-file rename, a journal-deployment recreate). **Recommend
  showing nothing in v1.** The journal keeps them as history. A later "orphaned declarations" admin
  view can list records whose ID no longer resolves.
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
