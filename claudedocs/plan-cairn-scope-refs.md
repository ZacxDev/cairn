# Plan: scope-level code sources, and an auditor that checks entries against them

This is a DESIGN, not a description of anything built. None of it exists yet.

**Where the citations point.** Every claim about today's behaviour carries a `file:line` read off
`origin/main` at **`0355c7a`** (PR #209 merged). Re-read before editing. Claims that were
MEASURED rather than read say so where they appear, and name who measured them: this plan's
author, or audit round 1.

Examples are synthetic: scopes are `alpha-notes` and `beta-notes`; repositories are
`example-org/example-repo` (and `example-org/example-mono` for a monorepo) on `github.com` or
`git.example.com`; the one real repository an example may name is this public one. Operator
decisions are PARAPHRASED, never quoted (`AGENTS.md`).

**How proposed verbs are spelled.** They are written without the CLI prefix, as "the proposed
`audit-refs` verb" and "the proposed `sources` verb".
`tests/test_no_scrubbed_identifiers.py` refuses any backticked CLI-prefixed verb that no client
registers yet, and that refusal is correct. A plan must not read as an instruction to run a
command that does not exist.

**The plugin plan.** `claudedocs/plan-cairn-plugins.md` exists only on the unmerged branch
`origin/zach/plan-plugins` (PR #208). It is cited by decision number, not line, because it can
still move.

**Revision history.**
- *Revision 1* (`71e2823`) was the first draft. It recommended a separate registry journal
  outside the store.
- *Revision 2* records the operator's storage decision (O4: the scope's `README.md` front matter)
  and reworks everything downstream of it. It applies the round-0 deletions (D1–D5) and the
  round-1 findings (F1–F8):
  - **No pod change at all.** The pod gets no route, and the conformance corpus and route ledger
    do not move.
  - **The UI is the only editor**, writing through `internal/write` under the entry lock, with a
    revision compare.
  - **The auditor fetches every head plus GitHub's `refs/pull/*/head`, in full**, with lazy fetch
    disabled. A commit absent from that ref set is could-not-look, never stale.
  - **The e2e drives a real `file://` fetch.**
  - **Exit 10 for an undeclared scope and for `checked=0`.**
  - **The git-dependent tests reuse `CAIRN_GIT_TESTS_REQUIRED`.**

  Removed decisions keep their numbers, marked REMOVED, so references stay stable.

## Goal and premise

Today nothing in cairn records which code a scope describes. So no tool can mechanically ask
whether an entry's paths, commits or pull requests still hold upstream, and the operator wants
that check to run on its own. The operator's framing is a **code source per scope**: the
repository or repositories, and the branch, that a scope documents. An auditor can then compare
each entry's checkable claims with that remote and flag the stale ones. The declaration is kept
with the scope and edited in the browser.

There are two outcomes, and the second is worthless without the first.

1. **A code-source declaration per scope.** It is machine-readable, validated, editable by
   whoever administers the scope, and readable by the CLI, the browser and the auditor.
2. **A deterministic auditor.** For every entry in a scope it reports which checkable claims
   hold at the declared source's fetched commit, which do not, and which it could not check. It
   keeps those three apart and never edits an entry.

### What would make this unnecessary

Drop the work, or the named half of it, if any of these holds:

- **The existing out-of-tree audit is enough.** The operator's private tooling already checks
  `RESOLVED <sha>` markers and `## Pointers` paths against LOCAL clones (STEP 1). It infers the
  scope-to-clone mapping from directory names. If every scope's clone is always on the auditing
  host and always fetched, the declaration buys only the remote check, and a `git fetch` before
  the existing audit buys most of that.
- **Nobody acts on the findings.** An audit that prints forty findings a week which nobody reads
  is a permanently red gate. Nothing here measures whether stale refs cause wrong decisions. B3
  (census) and Q4 exist so the cheap half lands first and is judged.
- **Scopes rarely document a single repository.** A cross-cutting scope (an org process, a client
  relationship) declares nothing useful. So the declaration is OPTIONAL, and a scope without one
  answers `sources=undeclared`, which exits 10 and is never clean (decision 6).

### closing-condition

- **closing-condition:** `check`. Three mechanical parts, all required:
  1. Slices S1–S5 are MERGED on cairn `main`, verified by content (the files exist on
     `origin/main`), not by ancestry.
  2. **`tests/refaudit/e2e.sh` exits 0 on `main`.** It runs in the `go` CI job, which sets
     `CAIRN_GIT_TESTS_REQUIRED` (`.github/workflows/ci.yml:722`), and the step is NOT
     `continue-on-error`.
  3. **`python3 tests/parity/harness.py` exits 0 with S2's declared `README.md` in its world.**
     That is the measurement that neither client's existing output moves when a scope declares
     sources (decision 3).

  `e2e.sh` follows the repository's harness convention: it exits **2** ("could not vouch") when
  `git` or a built binary is missing, or when one of its own controls misbehaves. That is the
  SCRIPT's code. The proposed `audit-refs` verb itself never exits 2 except for bad argv
  (decision 8).

  **What `e2e.sh` asserts.** Every world is SYNTHETIC and built at run time:
  - bare repositories made with `git init --bare`;
  - a GitHub stub served on loopback;
  - the auditing host's map pointing `example.com` at `file://<tmp>` (decision 8).

  Nothing touches the network.

  | clause | what it asserts | negative control inside the clause |
  |---|---|---|
  | **(a) the declaration travels** | a store whose `alpha-notes/README.md` declares two sources is synced by the Go client; the proposed `sources` verb prints exactly those two canonical forms, in order, from the CACHE | `beta-notes`, with no `sources:` key, prints `sources=undeclared` |
  | **(b) clean world** | the proposed `audit-refs` verb over a world where every claim holds, fetched through the real `file://` path, exits **0** and prints `checked=<N> stale=0 unchecked=0`, with N equal to the planted claim count and N > 0 | — |
  | **(c) stale world** | the same verb over a world with exactly one missing path, one symbol absent from its file and one PR that answers 404 exits **9**. The finding set EQUALS the planted set (by entry, claim and state) | each planted defect, removed in turn, drops exactly its own finding |
  | **(d) could not look** | exit **10**, with the reason named, in each of: a declared source whose remote does not exist; an undeclared scope; a declared scope with zero checkable claims (`checked=0`) | the same scope with the remote present and one claim exits 0 |
  | **(e) the pod and UI never fetch** | `go list -deps` finds no `os/exec` and no `internal/refaudit` in `cmd/cairn-server` or `cmd/cairn-ui` | the same query on `cmd/cairn` finds both, so the count can move |
  | **(f) the squash shape, over a real fetch (F1)** | a `RESOLVED` SHA reachable ONLY from `refs/pull/1/head` on the `file://` remote reports `exists-off-branch` (informational), and the run's exit is unaffected | with the `refs/pull/*/head` refspec removed, the same SHA reports `not-in-fetched-refs` and the run exits **10** |

  `--self-test` applies one sabotage per clause on a scratch copy of the tree with its `.git`
  removed (the `tests/control_mutants.py` pattern). Each must be caught by its OWN clause's
  message:
  - **(a)** make the reader return the README's first source only;
  - **(b)** drop the denominator, so `checked=` prints 0;
  - **(c)** drop the symbol check;
  - **(d)** map "remote missing" to clean;
  - **(e)** add a blank `os/exec` import to `cmd/cairn-server`;
  - **(f)** drop the pull refspec.

  The run prints **`sabotaged=6 caught=6`**.

  ⚠ **What the closing condition does NOT cover.** Stored findings (Q4) and a recall-header line
  (Q5) are open questions, not slices. And the GitHub PR-state check against the REAL API is a
  one-time operator check (S5's test plan): CI must not depend on github.com, so clause (c)'s 404
  comes from the loopback stub.

## STEP 1 — What audits exist today

**In this repository: none that touch a remote.** cairn's own checks are about entry SHAPE:
- `validate`'s parse, dropped-line and marker-reachability scans
  (`internal/store/validate.go:196`, `:251`, `:455`);
- the open-actions scan (`:543`);
- the arc registry's `?check=1` orphan check (`tests/conformance/README.md`, "The orphan
  check (S5)").

None opens a socket or runs `git`.

**Out of tree: the operator's private hygiene tooling**, summarised by mechanism only:

| claim in an entry | how it is checked today | against what |
|---|---|---|
| a commit SHA (`RESOLVED <sha>:`, or a backticked hex run) | `git cat-file -e <sha>^{commit}` | every LOCAL clone it can find, the scope's own first |
| a path in `## Pointers` | `exists()` on the filesystem | the scope's local clone, then the others |
| a PR or issue ref | `gh pr view`, behind an opt-in flag | GitHub, with the operator's own `gh` login |
| a symbol | **not checked** | — |

That tooling's verdict is three-state (a target exists / could not test / no home). It deletes
nothing. It infers the scope-to-repo mapping from directory names. Its own documentation warns of
three limits:
- it answers about the clone on disk, not the remote;
- a 7-character SHA can match the wrong repository;
- **a SHA that exists but implements nothing still passes.**

That last limit carries into this design unchanged (decision 7).

**The plugin plan (#208) describes no auditor.** Its plugins read session transcripts and write
per-session outputs of a closed set of types (decision 9 there). A worker token "cannot read the
store" (decision 2 there). Output renders as a labelled CLAIM (decision 11 there). Decision 9
below therefore does not wait for #208.

## STEP 2 — What exists today, read off the code (`0355c7a`)

### Per-entry refs: `refs:` is `<system>:<id>`, and the id half is opaque

- `TaskRef` (`internal/store/entry.go:66-84`) normalises the system half and keeps the id half
  byte-identical.
- `Entry.Tasks` is the entry's `refs:` in file order, deduped (`:129-133`). `tasks:` and `task:`
  are accepted aliases (`:460-485`).
- `ParseTaskRef` splits on the FIRST colon. It refuses whitespace, a comma and control characters
  (`entry.go:570-622`). The comma is refused because the inline-list writer would split it
  (`:594-608`).
- The URL registry is string formatting only (`internal/store/refurl.go:11-15`), with a BUILT-IN
  tier of `github` and `clickup` (`:81-120`) and an OPERATOR tier through `CAIRN_REF_BASE_<SYSTEM>`
  (`:42-53`, `:208-224`). Self-hosted hosts may not be written into this repository, and
  `leakscan` gates it (`:26-35`).
- **So `github:example-org/example-repo#428` is already a structured PR claim.** No other
  system's refs have a shape the auditor can check.

### Claims the entry grammar makes structurally

- **`RESOLVED <sha>:`** — `journalOpenness` (`internal/store/openness.go:47-53`) takes 7–40 hex
  characters. `BulletOpenness` lowercases them (`:76-90`). The comment says the SHA exists to make
  the claim checkable (`:41-42`).
- **`## Pointers`** — a declared heading (`internal/store/journal.go:16-20`). Its rows are
  `- path — description` (`internal/report/citationtoken_test.go:21-24`). **No parser extracts
  the path.**
- **Symbols** — backticked spans in prose, with no structure saying "symbol in that repository".

### The scope directory: entries, plus exactly one non-entry `README.md`

- `LoadIndex` reads `<root>/<scope>/*.md` (`internal/store/load.go:133-176`) through
  `IsEntryFileName` (`:374-377`). That predicate excludes exactly `README.md`, the scope's policy
  sheet (`:306-319`). The Python twin is `subsystem_resolver.SCOPE_POLICY_SHEET`
  (`lib/subsystem_resolver.py:3315`, `:3381`).
- **No code in the tree writes a `README.md`.** Operators author it.
- `/snapshot` ships every `*.md` not starting with a dot, README included
  (`internal/snapshot/snapshot.go:442`; the oracle's `server/server.py:4636`). Every client cache
  therefore holds each visible scope's README (`load.go:306-309`). The snapshot is already
  narrowed to the caller's visible scopes, so a README reaches exactly the scope's readers.
- **MEASURED by this plan's author, on BOTH loaders (Go `LoadIndex`, Python `load_index`) at
  `0355c7a`.** The world held one valid entry, a `_scope.md` carrying only `sources:` front
  matter, and a `README.md` carrying the same front matter:
  - `_scope.md` → **1 MALFORMED** row in both;
  - `README.md` → **0 rows** in both.

  This is the pair: the instrument can report a reserved file as malformed, and does not for the
  README.
- `ScopeRevision` (`internal/store/revision.go:17-48`) is the STORE's own git revision of a
  scope. It answers `unknown` today (`:24-28`). "Source" in this plan always means the documented
  code, and never this.

### The front-matter grammar: strings and lists of strings, and `#` is data

- Values are a string or a `[]string` (`internal/store/frontmatter.go:18-22`). The accepted forms
  are `key: value`, an inline `[a, b]` list and a block list (`:54-58`). Unknown keys are
  PRESERVED for callers (`:56-57`). So each source must be one string.
- **MEASURED by this plan's author (Go `ParseFrontMatter`, `0355c7a`):**
  - a two-item block list parses as two items;
  - the inline form parses the same;
  - **a trailing `# comment` is NOT a comment.** It becomes part of the value
    (`"…@main # trailing"`).

  The source grammar must therefore REFUSE whitespace, or a comment silently becomes part of a
  branch name.
- **`repo:` is taken.** It is the older spelling of `scope:` in entry front matter
  (`entry.go:119-121`, `:215-218`). This plan's key is `sources:`, never `repo:`.
- The front-matter pattern is anchored at the top of the file (`frontmatter.go:10-15`).

### The write path: one lock, one revision, one seam

- `ReplaceEntry` takes the per-entry lock: an exclusive `flock` on a separate `.<name>.lock`
  file, which has a leading dot and no `.md` suffix (`internal/write/atomic.go:9-37`). Under that
  lock it reads the file, compares `EntryRevision` against `If-Match`, and only then writes
  through an atomic replace (`internal/write/write.go:239-291`; `atomic.go:80`). The comment
  explains why: a check outside the lock is decorative (`write.go:245-248`).
- **The interleave seam exists in this package.** `AppendBullet` and `ReplaceEntry` take an
  `interleave func()` that runs inside the lock, after the read and before the write
  (`write.go:108`, `:266`, `:284-286`). `write_test.go:188-215` uses it to FORCE two writers into
  the window a missing lock would leave open, rather than hoping a wall-clock race overlaps them.
  ⚠ Revision 1 cited the arc registry's concurrency test as this seam. That was wrong
  (round-1 F2): `internal/arcs/arcs_test.go:241-251` is a 32-goroutine contention test.
- `validateEntryBytes` answers "may these bytes land as an ENTRY". Its callers are pinned at
  exactly `{CreateEntry, ReplaceEntry}` in both directions
  (`TestTheWriteTimeValidatorHasExactlyTheDeclaredCallers`, `write.go:366-371`). A README is not
  an entry, so a README write must NOT become a third caller.
- **The pod's `PUT /api/v1/entry` cannot reach a README.** Writes resolve their target through
  the index (`entry.go:142-145`), and the index excludes `README.md`. So the proposed `sources`
  edit has no natural CLI route through the existing `put` verb. Per D1, none is added.

### The control journal refuses unknown events WHOLE

`Event.validate`'s `default:` arm refuses an unknown event kind
(`internal/control/journal.go:219-226`), and `Replay` refuses the whole journal over it
(`:640-667`). Revision 1 used this to rule out the control journal. O4 makes it moot. It stays
here because it is why the answer is not "put it in the control plane" (Q1).

### The browser surface: sharing is the edit-flow precedent

- UI routes are a declared table (`internal/ui/routes.go:118-185`). `POST /share` and
  `POST /unshare` are the existing scope-administration writes (`:147-149`).
- Both cross-site gates derive from the METHOD (`stateChanging`, `internal/ui/server.go:1323`):
  same-origin before auth, and a per-session CSRF token after it (`:1305-1306`).
- The share handler authorises with `id.Auth.Allows(scope, control.VerbAdmin)`
  (`internal/ui/sharehandlers.go:170`).
- **`cairn-ui` reads the store today and never writes it.** Its only write target is the control
  journal, which must resolve OUTSIDE `-store` (`cmd/cairn-ui/main.go:246-266`). S4 makes the UI a
  store writer for exactly one file per scope. That is a deployment change (the store volume must
  be mounted read-write in the UI's pod) and a blast-radius change (T9).

### The CLI: the one `git` caller, and the exit model the auditor reuses

- `internal/client/reposcope.go` is the CLI's one `git` call site, run with
  `GIT_OPTIONAL_LOCKS=0` (`:38-58`). It derives a scope from a LOCAL checkout's name (`:60-103`):
  checkout → scope, the reverse of this feature.
- **MEASURED by this plan's author (`go list -deps`, `0355c7a`):** `os/exec` is in `cmd/cairn`'s
  import graph (1 of 204 packages) and in neither `cmd/cairn-server`'s (0 of 209) nor
  `cmd/cairn-ui`'s (0 of 232). That pair is clause (e)'s baseline.
- **Go tests DO run `git` today, under a two-tier rule** (round-1 F5; revision 1 wrongly said
  none did). `TestAnUNTRACKEDNestedModuleIsIGNOREDButATRACKEDOneIsNOT`
  (`internal/depspolicy/depspolicy_test.go:419-448`) drives a real `git init`.
  - It SKIPS when `git` is absent, because the nix derivations' check phase has no `git`. Adding
    one "for a single test" was explicitly declined (`:420-426`).
  - It FAILS when `CAIRN_GIT_TESTS_REQUIRED` is set. The `go` CI job sets it
    (`.github/workflows/ci.yml:722`), so the tier that can measure it cannot skip it.

  This plan reuses that rule and adds no `gitMinimal` to any check phase.
- The exit model is a printed contract (`internal/client/exit.go:3-67`). `arcs --check` reuses
  doctor's `0/9/10` (`exit.go:18`): `9` = a check MEASURED a problem, `10` = a check COULD NOT
  LOOK (`internal/doctor/render.go:46-51`). `2` is USAGE, the caller's argv (`exit.go:36-39`).

### The recall header

`RecallReport.RenderText` writes status, `store:`, host, extra header lines, then `caveat:`
(`internal/report/text.go:306-313`). It is the shared renderer, diffed against the Python oracle
by the reader fixture and the parity gate. A source line there is Q5.

## Decisions — who chose what

### Chosen by the OPERATOR (paraphrased; not re-litigated)

| # | what the operator chose | where it lands |
|---|---|---|
| O1 | Being able to audit the store's claims against upstream code mechanically, which no tool does today. | The goal. |
| O2 | "Scope-level refs" means each scope names the code it describes (repositories and branch), so an auditor can test entries' paths, symbols, commits and PR references against it. | Decisions 2, 6. |
| O3 | The declaration belongs to the scope and is edited in the browser. **Deviation history, kept visible:** the operator's first wording was a per-scope "manifest", with a reserved file such as `_scope.md` as the example. Revision 1 measured that a reserved `_scope.md` loads as a MALFORMED entry on both clients and recommended a separate journal instead. The operator then chose O4. | Decisions 1, 4, 5. |
| O4 | **The sources live in the scope's existing `README.md`, as a `sources:` key in its front matter.** Not a separate journal and not `_scope.md`. Revision 1's measurement supports this: README front matter is invisible to both existing loaders. | Decision 1 and everything downstream. |

### Chosen by the AGENT writing this plan (open to review)

1. **The storage contract under O4.**
   - The declaration is the `sources:` key of the front matter at the top of
     `<store>/<scope>/README.md`, written as a BLOCK list (one source per `  - ` line).
   - No `README.md`, or one with no front matter, or front matter without `sources:`, all mean
     **undeclared**.
   - A `sources:` key present but invalid is **declared-invalid**: the proposed `sources` verb and
     the UI name the failing item and rule, and the auditor exits 10 naming it. It never makes the
     README, the scope or any entry unreadable. A README is not an entry, so nothing that loads
     entries ever parses this key.
   - **Consequences of O4:**
     - **No pod change.** The README already travels in `/snapshot`, already narrowed to the
       scope's readers, so the CLI reads the declaration from its synced cache. No route, no
       `go_only` corpus row, no route-ledger move (D5, extended in decision 10).
     - **History is the store's, not the feature's.** An overwrite keeps no prior version. If the
       store directory is a git repository, that repository is the history. The per-request
       actor is recorded in the UI's audit line, not in the file (decision 5).
     - **Renames carry it.** The declaration moves with the scope directory, so a scope rename
       needs nothing from this feature (round-1 F4). That replaces revision 1's Q12, which wrongly
       said the token-file projection has no scope IDs. It does
       (`internal/control/tokenfile/source.go:584-586`, `internal/control/resolve.go:89`).

   **Rollback safety, re-argued for O4:**
   - every pre-feature loader ignores `README.md` (measured, both languages);
   - every pre-feature snapshot ships it unchanged;
   - no pre-feature binary renders or parses a README.

   So a README carrying `sources:` is inert to every older build. Rolling back after edits leaves
   the key in place and harmless, and rolling forward reads it again. There is no journal, flag
   or event kind for an older build to refuse. The one thing a rollback does NOT undo is the UI
   pod's read-write store mount. A pre-feature UI never writes the store, so that mount is unused,
   not dangerous.

2. **The source grammar: one string per source.**

   ```
   git:<host>/<repo-path>[//<subpath>]@<branch>
   ```

   - `<host>` is a DNS name, lowercased. No scheme, userinfo or port.
   - `<repo-path>` is ≥ 2 `/`-separated segments. Each segment is `[A-Za-z0-9._-]+`, does not
     start with `-` or `.`, and a trailing `.git` is stripped. **Case is PRESERVED** and compared
     byte-identically, as with `TaskRef`'s id half: GitHub folds case and other hosts do not.
   - `//<subpath>` (optional) names a monorepo directory. `//` cannot occur inside a normalised
     repo path, so the boundary is unambiguous with nested groups. It is ≥ 1 segment under the
     same rule, with no `..`.
   - `@<branch>` is REQUIRED, because inferring a default branch would need the network. It must
     be a valid `git check-ref-format --branch` name, with no leading `-`, no `@` and **no
     whitespace**: the measured `# comment` absorption makes this rule load-bearing.
   - **No comma**, for `ParseTaskRef`'s reason (`entry.go:594-603`). No control characters.
   - The canonical form is the normalised string. Duplicates are DEDUPED, not refused. **Order is
     kept**, and the first source is the primary.
   - **≤ 8 sources per scope.** A refusal names the number. The number is a guess (Q14).

   Synthetic examples: `git:github.com/example-org/example-repo@main`;
   `git:github.com/example-org/example-mono//services/widget@release/2.x`;
   `git:git.example.com/team/sub-group/example-repo@trunk`.

   **One parser, `internal/codesrc.Parse`, used by every caller:**
   - the UI's POST;
   - the proposed `sources` verb;
   - the auditor;
   - S2's README splice, which validates the list it is about to write.

3. **Go-only READING, with the Python side PROVABLY ignoring the key. No Python twin parser.**
   - The two clients and the oracle all read the store, so the parser question returns under O4.
     The answer is that **no Python code path reads a README's front matter at all**:
     - `load_index` skips the file by `is_entry_filename` (`lib/subsystem_resolver.py:3381`);
     - the oracle only ships its bytes in `/snapshot`;
     - nothing in `lib/` renders it.
   - The proof is behavioural, not this sentence. S2 adds two checks, and both must be watched
     red under a sabotage:
     1. a Python test asserting that `load_index` over a scope whose README carries a valid
        `sources:` list, an INVALID one, and none returns the identical index and zero malformed
        rows. **Sabotage:** route the README through the entry path; it must go red with one
        malformed row;
     2. the parity world gains `alpha-notes/README.md` with a `sources:` key, and
        `tests/parity/harness.py` stays at 0 failures (closing-condition part 3). **Sabotage:**
        make the Go `recall` print a `sources:` line; it must go red on that row's stdout diff.
   - A Python twin becomes necessary only if Q5 puts the line in recall. That cost is named there.

4. **Who may edit: a principal holding `admin` on the scope, exactly the share flow's verb**
   (`sharehandlers.go:170`). Repointing what code a scope documents redirects every future audit
   of it.
   - **Readers see the declaration because they see the README.** The snapshot is already
     narrowed to the caller's scopes, so no second visibility rule exists to drift.
   - ⚠ A token-file deployment confers `admin` on nobody (`tokenfile/source.go:424-425`), so there
     the UI can edit nothing. Hand-editing the README on the store host is the existing route, and
     it stays one. This is stated so nobody files it as a bug.

5. **The edit flow: the UI is the ONLY editor (D1).**
   - **Routes.** `GET /scope/sources?scope=<s>` renders a form, and `POST /scope/sources` writes.
     Both are rows in the UI's declared table. Same-origin and CSRF come from the method
     (`server.go:1305`, `:1323`), and authorisation is decision 4. A deployment without the
     read-write mount renders the read-only state on the page, as `ReadOnlyAuthority` does.
   - **The write path.** The POST calls a new `internal/write.SetScopeSources(path, sources,
     ifRevision, interleave)`. It is `ReplaceEntry`'s discipline, applied to a file that is not an
     entry:
     1. take the SAME per-file lock (`atomic.go:26-37`), so `.README.md.lock` is a dot-file with
        no `.md` suffix, invisible to the loader and to `/snapshot`;
     2. read the README under the lock;
     3. compare `EntryRevision` of the current bytes against the form's hidden `revision`, inside
        the lock. A missing file has the fixed revision `absent`;
     4. splice;
     5. replace atomically (`atomic.go:80`);
     6. return the new revision.
     - It does **not** call `validateEntryBytes` (that caller pin stays at two). It validates
       with `codesrc.Parse` instead.
     - It carries the `interleave` seam at the point `ReplaceEntry` does, for the test in S2.
   - **The splice is defined on bytes, and it is the riskiest code here:**
     - with front matter present, it replaces only the `sources:` key's lines, or appends the key
       at the end of the block. Every other byte, other keys and the whole prose body included, is
       unchanged;
     - with no front matter, it prepends `---\nsources:\n  - …\n---\n`;
     - with an empty list, it removes the key, and removes the block too if no key remains. So
       adding then removing sources returns the ORIGINAL bytes;
     - after splicing it re-parses the result. If the parsed `sources:` is not exactly the new
       list, or any other key changed, it refuses and writes nothing.
   - **The form.** ONE `<textarea>`, one source per line (Q6), plus the hidden revision. A stale
     revision is refused, writes nothing, and re-renders the CURRENT list. An invalid line is
     refused naming its line number and rule, with the operator's text preserved. On success the
     UI logs one audit line naming the principal, the scope and the new revision. That line is
     the "who changed it" record O4 gives up in the file.
   - **The scope page.** It gains a Sources block: each source as text, a link ONLY for the
     built-in `github.com` shape (`https://github.com/<repo-path>/tree/<branch>`) through
     `safeHref`, and plain text for every other host (`refurl.go:36-40`'s rule).
   - **No CLI edit.** A README is unreachable through `put`, because writes resolve through the
     index. No new write verb is added (D1). Hand-editing on the store host remains possible.

6. **What the auditor checks DETERMINISTICALLY.** Every check is a fact about a named, fetched
   commit, quoted in the finding.

   | claim, and where it comes from | check | states |
   |---|---|---|
   | **path** — the first backticked span of a `## Pointers` row, or its first token before ` — `, if path-shaped (contains `/` or a `.<ext>`, no scheme, no whitespace, no leading `-`) | `git ls-tree <commit> -- <path>`, branching on CONTENT: present only if the output is non-empty AND its path field equals the requested path. `ls-tree` exits 0 on a missing path (round-1 F8). Repo-root-relative first, then subpath-relative (Q7). | `holds(root)` · `holds(subpath)` · `missing` · `unchecked: not path-shaped` |
   | **commit** — `RESOLVED <sha>:` (`openness.go:47-53`) | `git rev-parse --disambiguate`, then `git merge-base --is-ancestor <sha> <branch-commit>` | `on-branch` · `exists-off-branch` · `not-in-fetched-refs` · `ambiguous` |
   | **PR** — a `refs:` item `github:<owner>/<repo>#N` (`refurl.go:104-118`) | GitHub REST `GET /repos/<owner>/<repo>/pulls/N`, falling back to `/issues/N` on 404 (stdlib `net/http`) | `open` · `merged` · `closed-unmerged` · `not-found` · `unchecked: rate-limited / no token` |
   | **ref outside the declaration** — a `github:` ref whose owner/repo is none of the scope's sources | string comparison | `outside-sources` |
   | **symbol** — ONLY the structural form `path#Symbol` in a `## Pointers` row (Q8) | `git grep -w -F -e <Symbol> <commit> -- <path>` | `present` · `absent-from-file` · `file-missing` |

   **Severity is ONE closed table in `internal/refaudit`, never decided at a call site:**
   - **Stale (exit 9):** `missing`, `absent-from-file`, `file-missing`, `not-found`.
   - **Informational (never moves the exit):** `on-branch`, `exists-off-branch` (the squash-merge
     shape: a feature SHA is never an ancestor after a squash), `holds(subpath)`, `open`,
     `merged`, `closed-unmerged`, `outside-sources`.
   - **Could not look (exit 10 when nothing is stale):**
     - `not-in-fetched-refs`: absence from a PARTIAL ref set proves nothing about the remote
       (round-1 F1), so **a commit check never yields stale**;
     - `ambiguous`;
     - an unreachable remote;
     - `unchecked: rate-limited / no token`;
     - an undeclared or declared-invalid scope;
     - `checked=0`.

   **Unchecked by design, and counted so the denominator is honest:**
   - prose backticks;
   - SHAs in prose after the marker;
   - URLs;
   - operator-tier ref systems.

   (`path:line` is REMOVED, D4.)

7. **What needs JUDGEMENT stays out of the exit code:**
   - whether an existing file still says what the entry claims;
   - whether a `RESOLVED` commit implements the claim;
   - whether `OPEN:` items are still open;
   - what a closed-unmerged PR means;
   - whether a moved symbol is the same symbol.

   The auditor may PRINT inputs for those judgements. It never renders a verdict on them.

8. **Where the auditor runs: the proposed `audit-refs` verb on an operator host, with the
   operator's credentials.** The pod and the UI never fetch, never hold remote credentials and
   cannot spawn a process (clause e).
   - **Reading.** It reads the scope's entries and its README from the synced cache, the same
     cache `recall` reads.
   - **Fetching (F1).** Each source is fetched IN FULL (no partial-clone filter) into
     `$XDG_CACHE_HOME/cairn/mirrors/<sha256(canonical source)>.git` with the refspecs
     `+refs/heads/*:refs/heads/*` and `+refs/pull/*/head:refs/pull/*/head`. The second matches
     nothing on hosts without pull refs, harmlessly.
     - Every `git` call runs with `GIT_NO_LAZY_FETCH=1`, so no check can silently trigger a
       network fetch.
     - Round 1 measured why both choices matter, on one squash-shaped world: 128 SHAs resolved
       with a full single-branch fetch, 128 with `GIT_NO_LAZY_FETCH=1`, and **0** with lazy fetch
       on a `blob:none` mirror. Feature-branch SHAs need the extra refs, and a blob-less mirror
       makes the answer depend on fetch-time side effects.
     - The mirror directory is a DIGEST, never a path built from the declaration.
   - **Transport (F3: https only, stated once).** The auditing host holds a map,
     `CAIRN_AUDIT_HOSTS`, from host to base URL. The default is
     `github.com=https://github.com`. The fetch URL is `<base>/<repo-path>.git`.
     - `GIT_ALLOW_PROTOCOL` is the set of schemes appearing in that map: `https` by default.
       `file` is allowed only when the operator (or `e2e.sh`) maps a host to a `file://` base,
       which is how clauses (b)–(d) and (f) exercise the REAL fetch path.
     - **SSH is not supported in v1.** Revision 1 said both https and ssh in different places; it
       is https only.
     - `GIT_TERMINAL_PROMPT=0`, `GIT_CONFIG_NOSYSTEM=1`.
     - `GIT_LITERAL_PATHSPECS=1`: round 1 measured `:(top)` and globs being interpreted
       otherwise (F8).
     - `--end-of-options` or `--` at every call.
     - Credentials come from the operator's existing git credential helper. A host absent from
       the map is never contacted (`unchecked: host not mapped`).
   - **PR state.** It reads `GH_TOKEN`/`GITHUB_TOKEN`. With neither, the PR check is `unchecked`.
     The API base is `CAIRN_AUDIT_GITHUB_API` (default `https://api.github.com`), which is how
     `e2e.sh` points it at its loopback stub. **No credential is printed, logged or sent to the
     pod.**
   - **Output.** It prints `sources=<…>`, then `checked=<N> stale=<S> unchecked=<U>`, then one
     line per finding. `--json` emits the same as data.
   - **Exit codes (F6)** are doctor's `0/9/10`, as `arcs --check` uses them, with no new code
     minted:
     - **0** only when N > 0 and nothing is stale or could-not-look;
     - **9** when anything is stale;
     - **10** otherwise, **including an undeclared scope and `checked=0`**. A reassuring zero is
       never a pass.
     - `2` stays USAGE (bad argv) and is never used for "could not vouch".
   - **The proposed `sources` verb** (read-only, Go-only) prints the canonical list from the
     cache, or `sources=undeclared`, or the invalid item and its rule.

9. **The auditor is a LIBRARY first (`internal/refaudit`), so #208 can wrap it later.**
   - It takes plain values and a `GitReader`/`PRReader`, and returns plain findings, in the
     `internal/report` shape.
   - The proposed `audit-refs` verb is its first caller.
   - A future #208 plugin calling the same library cannot disagree with the CLI's verdicts.

10. **REMOVED (D5, extended).** Revision 1 proposed `GET`/`HEAD` (and, with D1, `PUT`)
    `/api/v1/sources/<scope>`. D5 removed `HEAD`. Under O4 the README already travels in
    `/snapshot`, so `GET` has no reader left either, and it is removed too. **The pod is
    untouched.** If the operator wants a live, uncached read, that is a later Go-only head on the
    arcs precedent.

11. **Recall gets no source line (Q5).** `sources` and the UI scope page answer "what does this
    scope document".

## Threat model

| threat | control |
|---|---|
| **T1. A hostile declaration aims the auditor's fetch at an attacker's host, harvesting credentials** | Only hosts in the auditing host's `CAIRN_AUDIT_HOSTS` map are contacted. The URL is built from the map's base, never from the declaration's scheme (it has none). `GIT_ALLOW_PROTOCOL` is limited to the map's schemes (https by default; no `ext::`, no ssh). Credential helpers are keyed by host. |
| **T2. Argument injection** (a branch, path or symbol starting with `-`) | Refused at parse (decision 2), plus `--end-of-options`/`--` at the one `GitReader` call site. |
| **T3. Pathspec magic or globs in a cited path** | `GIT_LITERAL_PATHSPECS=1` (F8), plus the content-based presence check. |
| **T4. Mirror-path traversal** | The mirror directory name is `sha256(canonical source)`. |
| **T5. Repointing a scope's audit** | Only `admin`, only through the UI, behind both method-derived gates. A UI audit line per change. |
| **T6. A README splice destroys an operator's prose** | Byte-level splice of one key, a post-splice re-parse that refuses on ANY other change, the atomic replace, and the add-then-remove round trip pinned to the original bytes (S2). |
| **T7. Lost update between two editors** | The revision compare inside the lock. Exactly one of two same-revision POSTs lands, and the other is refused with the current list (S2, F2). |
| **T8. A feature rollback breaks reading** | Inert to every older build (decision 1, measured on both loaders). |
| **T9. The UI gains write access to the STORE** | A new blast radius, accepted as O4's cost and named, not hidden: a compromised UI process could now write ENTRY files too, because a volume cannot be mounted read-write for one filename per directory. Control: a CALLER LEDGER test pinning that `internal/ui` calls exactly one `internal/write` function, `SetScopeSources`, failing on GROW or SHRINK (S4). ⚠ That pins the CODE, not the process's capability. |
| **T10. The pod or UI spawns `git` or reaches a code host** | Clause (e): no `os/exec` and no `internal/refaudit` in either import closure. ⚠ A structural check on `exec`; it cannot see a `net/http` CLIENT call, because both already import `net/http` to serve. |
| **T11. Stored XSS through a source string** | gomponents `Text` only; the one href (the built-in GitHub shape) goes through `safeHref`; `Raw`/`Rawf` stay AST-banned. |
| **T12. Resource exhaustion on the auditing host** | ≤ 8 sources, a per-call timeout and a per-run PR-check cap. Full mirrors are the price of F1; their size is unmeasured. A cap hit reports could-not-look, never clean. |
| **T13. A finding read as a verdict on meaning, or a stale run read as current** | The closed state table, judgement kept out of the exit code, and every finding quoting its fetched commit and time. |
| **T14. A committed fixture leaks a real repository** | Worlds are built at run time with synthetic names; `leakscan` in CI; no captured text. |

## Slices

Each slice is mergeable alone, leaves `main` releasable, and has tests that run on that slice
alone. **No slice touches `cmd/cairn-server` or `internal/api`**, so `tests/conformance/`, the
route ledger and `tests/dualrun/` do not move.

| slice | what | ledgers it moves | mergeable alone because |
|---|---|---|---|
| **S1** | **`internal/codesrc`**: `Parse`/`Canonical` (decision 2) and the pure README functions: `FromREADME(bytes) → (sources, state)` and `Splice(bytes, sources) → bytes`, with its round-trip and re-parse guarantees. | new package → `go` job `ok` floor; `tests/control_mutants.py` `PKGS` and its pinned count. | A library nothing imports. |
| **S2** | **`internal/write.SetScopeSources`** (decision 5's write path, with the `interleave` seam), the **Python-ignores proof** (decision 3, check 1), and the **parity world's declared README** (check 2). | `internal/write` tests; a new `tests/test_readme_sources_are_invisible_to_python.py`; `tests/parity/world.py`; mutant rows. | Nothing calls the new function yet. The parity change adds a file both clients already ignore. |
| **S3** | **The proposed `sources` verb** (read-only, Go-only). | `internal/client` verb table → `-verbs`; `flake.nix` `want-go-only-verbs.txt`; `tests/testlib/capability_ledger.py` `go_only` row. | An additive read verb. |
| **S4** | **UI**: the Sources block; `GET`/`POST /scope/sources`; the read-only state; the `internal/ui` → `internal/write` caller ledger (T9). | `internal/ui/routes.go` table; hand ledger and near-miss probes; the uiaudit world gains one declared README; mutant rows; `internal/ui/README.md` (including the read-write mount requirement). | Without the read-write mount the page renders read-only. |
| **S5** | **`internal/refaudit` + the proposed `audit-refs` verb** (decisions 6–9); the import-graph test (clause e); `tests/refaudit/e2e.sh` with clauses (a)–(f) and `--self-test` (`sabotaged=6`); its step in the `go` job. | new package → `ok` floor and `PKGS`; `internal/depspolicy` (the pod/UI ban); verb ledgers as in S3; `ci.yml`; README. Clause (a) needs S3. | A read-only verb that writes only its own mirror cache. |

**Slice ↔ closing condition ↔ self-test, checked against each other:**
- **S5 creates `e2e.sh`, and all six clauses with it.** Clause (a) needs S3's verb, and clauses
  (b)–(f) need S5 itself. So S5 lands after S3, and `sabotaged=6` is pinned once, in S5.
- **S1, S2 and S4 have their own Go and Python tests and no clause.** Their behaviour is
  witnessed by those tests and by closing-condition part 3 (S2's parity row), not by `e2e.sh`.
  This is stated so nobody reads the e2e's six clauses as covering the UI write.

**Mutant rows** (indicative names; they join `tests/control_mutants.py` and move its pinned count):

- **S1:**
  - `codesrc-accepts-a-non-dns-host`
  - `codesrc-branch-may-lead-with-a-dash`
  - `codesrc-whitespace-in-branch-accepted` (the `# comment` absorption)
  - `codesrc-repo-path-case-folded`
  - `codesrc-subpath-dotdot-accepted`
  - `codesrc-duplicate-refused-not-deduped`
  - `codesrc-order-not-kept`
  - `codesrc-splice-changes-a-body-byte`
  - `codesrc-splice-drops-another-key`
  - `codesrc-empty-list-leaves-the-key`
- **S2:**
  - `write-sources-revision-checked-outside-the-lock`
  - `write-sources-stale-revision-accepted`
  - `write-sources-skips-the-reparse-check`
  - `write-sources-uses-a-different-lock-file`
- **S4:**
  - `ui-sources-post-skips-the-admin-check`
  - `ui-sources-links-a-non-github-host`
  - `ui-calls-a-second-write-function`
- **S5:**
  - `refaudit-off-branch-counted-stale`
  - `refaudit-not-in-fetched-refs-counted-stale`
  - `refaudit-ls-tree-exit-code-trusted`
  - `refaudit-literal-pathspecs-dropped`
  - `refaudit-undeclared-scope-exits-0`
  - `refaudit-checked-zero-exits-0`
  - `refaudit-unreachable-remote-reports-clean`
  - `refaudit-closed-pr-escalates`
  - `refaudit-host-map-bypassed`
  - `refaudit-mirror-dir-from-path-components`

Removed with the slices they belonged to:
- revision 1's `api-sources-*` rows (D1, D5);
- the journal-fold rows (O4);
- `refaudit-existence-counted-as-ancestry`. With commit absence now could-not-look, that
  mutant's only effect is caught by `refaudit-not-in-fetched-refs-counted-stale`.

Any git-dependent mutant row follows the two-tier rule. Under `CAIRN_GIT_TESTS_REQUIRED` it must
be KILLED. Where `git` is absent, its test skips, and the battery reports that row as skipped by
name, never as killed.

### Test plan per slice (negative controls named)

**S1.**
- **Parse.** Every example maps to a LITERAL canonical string, never derived from `Canonical`.
  Each refusal has a fixture valid in every respect but one, so no earlier check can win, and the
  test asserts that refusal's OWN message. The refusals are:
  - a scheme;
  - userinfo;
  - a port;
  - a one-segment repo path;
  - a `..` subpath;
  - a missing `@`;
  - a branch with a leading `-`;
  - a branch with a space (the measured `# comment` shape);
  - a comma;
  - a control character;
  - nine sources.
- **Case.** `…/Example-Org/Repo@main` and `…/example-org/repo@main` are two sources. Fixtures are
  pairwise distinct and distinct from any constant the assertion names.
- **`FromREADME`.** Four states, each with a literal: absent file, no front matter, front matter
  without `sources:` (all three undeclared); valid; invalid. A README whose prose BODY contains a
  `sources:`-looking line reports undeclared. Front matter is top-anchored, and this is the
  control that the reader does not scan the body.
- **`Splice`.** Over README fixtures with:
  - other front-matter keys;
  - no front matter;
  - a prose body with `---` horizontal rules after the block;
  - CRLF line endings;
  - no trailing newline.

  It asserts that:
  - only the `sources:` lines differ (a byte diff, not a re-parse);
  - add-then-remove yields the original bytes;
  - a body byte changed by a sabotaged splice is caught by the re-parse refusal, shown once.

**S2.**
- **F2, re-specified for one editor.** Two `SetScopeSources` calls carry the SAME revision. The
  `interleave` seam holds the first inside the lock, after its read and before its write, while
  the second starts (`write_test.go:188-215`'s pattern). Exactly ONE lands. The other returns
  `PreconditionFailedError` carrying the new revision, and the file holds the first call's list.
  - **Negative control:** move the revision compare outside the lock (the mutant
    `write-sources-revision-checked-outside-the-lock`). Both calls then "succeed" and one list is
    lost. The test must go red on that.
- **The other outcomes:**
  - an absent README with revision `absent` creates one;
  - a stale revision writes nothing, asserted by a byte compare;
  - the lock file is `.README.md.lock`, and the loader and `/snapshot` both ignore it, asserted
    by a `LoadIndex` and a `snapshot.Build` member list.
- **The validator pin is unchanged.** `TestTheWriteTimeValidatorHasExactlyTheDeclaredCallers`
  still names exactly two callers. This is an **invariant guard**, labelled as one.
- **Python ignores it.** Decision 3's check 1, with its sabotage watched red.
- **Parity.** Decision 3's check 2: `harness.py` at 0 failures with the declared README in the
  world, and the sabotage (Go `recall` printing a `sources:` line) watched red.

**S3.**
- The verb prints the canonical list, `sources=undeclared`, or the invalid item, each pinned to a
  literal.
- `-verbs` lists it.
- `tests/test_go_client_ledgers.py` goes red if `want-go-only-verbs.txt` omits it (shown once).

**S4.**
- **A valid POST** lands, re-parses to the submitted list, and logs one audit line naming the
  principal.
- **A stale revision** writes nothing and re-renders the CURRENT list.
- **An invalid line** is refused naming its line number, with the text preserved.
- **Gates:** no CSRF → 403; cross-origin → refused (asserted, not assumed from `stateChanging`);
  a `write`-only principal → the share flow's refusal.
- **Links.** A `github.com` source renders exactly one `href`, equal to the literal
  `https://github.com/example-org/example-repo/tree/main`. A `git.example.com` source renders
  none. That is the control that the link is not built for every host.
- **The caller ledger (T9)** goes red when a second `internal/write` call is added to
  `internal/ui` (shown once).
- **uiaudit** captures the scope page and the form at the existing widths.

**S5.**
- **The e2e.** Clauses (a)–(f), and `--self-test` → `sabotaged=6 caught=6`.
- **Unit tests over the state table.** One synthetic repository world built in the test (git
  tests follow the two-tier rule):
  - a file;
  - a file under a subpath;
  - a commit on `main`;
  - a commit only under `refs/pull/1/head`;
  - two commits sharing a 7-hex prefix, made by brute-forcing commit timestamps, with the run
    time named;
  - the loopback GitHub stub answering open, merged, closed and 404.

  Each planted claim maps to ONE literal state.
- **F8.**
  - A missing path makes `ls-tree` exit 0 with empty output. The test asserts `missing`, and the
    mutant `refaudit-ls-tree-exit-code-trusted` turns it into `holds`.
  - A cited path spelled `:(top)x` or `*.go` is looked up LITERALLY.
- **F6.** An undeclared scope and a `checked=0` scope each exit 10, never 0 and never 2.
- **The host map.**
  - An unmapped host is never contacted: the stub counts connections, 0.
  - A mapped one is contacted: > 0, so the pair is reported.
  - A `file://` base works only when mapped. **Negative control:** remove the mapping, and the
    same declaration is `unchecked: host not mapped`.
- **Against the real GitHub API, once, by the operator, not in CI:** run the proposed
  `audit-refs` verb over a scope citing a PR in this public repository, with and without a
  token. Record the states, and record whether the tokenless run reports
  `unchecked: rate-limited` rather than a state.

## Round-0 dispositions (accepted)

| # | deletion | applied |
|---|---|---|
| D1 | the CLI `sources set` verb, the pod `PUT` and API `If-Match`/412 | REMOVED. The UI is the only editor. The UI's own revision compare stays: it is F2's requirement, not an API precondition. A CLI edit through the existing `put` is NOT natural, because `put` resolves through the index, which excludes `README.md`. No write verb is added. |
| D2 | the S6 (stored findings) and S7 (recall header) slice rows | REMOVED. Q4 and Q5 stay as questions. |
| D3 | B1, B2, B4 | REMOVED. B3 kept. |
| D4 | the `path:line` check | REMOVED from decision 6, its state table and the stale world. |
| D5 | `HEAD /api/v1/sources` | REMOVED, and extended to `GET` under O4 (decision 10). The pod is untouched. |

## Open questions

### Answered by the operator

| Q | question | answer |
|---|---|---|
| Q1 | Where are sources stored? | **The scope's `README.md` front matter → O4.** Revision 1 recommended a separate journal and ranked README front matter as the runner-up. |

### Still open — each with the agent's recommendation

- **Q2. Go-only reading?** **Recommend yes, with the Python side PROVEN to ignore the key**
  (decision 3). A twin parser becomes necessary only with Q5.
- **Q3. Where the auditor runs.** **Recommend** the proposed `audit-refs` verb on an operator
  host, on a user timer, over the `internal/refaudit` library. Later, if wanted, a #208 plugin
  around the same library. Never the pod.
- **Q4. Are findings stored?** **Recommend print-only first.** The decision is the operator's,
  after a few weeks of printed runs.
- **Q5. A source line in every recall?** **Recommend no.** Yes means a Python twin parser pinned
  by a shared fixture (the `tests/test_env_aliases.py` shape), a regenerated reader fixture and a
  parity row.
- **Q6. The UI input control (a FORK).** **Recommend one textarea, one source per line.** The
  alternative, repeated rows with add/remove, needs a second allowlisted script or a round-trip
  per row.
- **Q7. Path base in a monorepo.** **Recommend** root-relative, then subpath-relative, reporting
  which held. Subpath-only would flag every root-relative pointer already written.
- **Q8. Symbols.** **Recommend the structural `path#Symbol` form only.** A heuristic over prose
  backticks is the prose-detector shape `openness.go:33-40` rejects. This is a
  deterministic-versus-heuristic choice, so it is the operator's.
- **Q9. The default host map.** **Recommend `github.com=https://github.com` only.** Self-hosted
  forges are added per auditing host, never in this repository (`refurl.go:26-35`).
- **Q10. Who may edit.** **Recommend `admin`** (decision 4).
- **Q11. A `RESOLVED` SHA found only off-branch.** **Recommend informational** (the squash shape).
- **Q12. REMOVED (round-1 F4).** Renames: the README moves with its directory.
- **Q13. An entry documenting a different repository than its scope.** **Recommend deferring** a
  per-entry override until a measured case exists.
- **Q14. The ≤ 8 cap.** A guess. **Recommend keeping it** until a real scope needs more.
- **Q15 (new, O4). The read-write store mount for the UI.** It is a deployment change in the
  private deployment repository, and T9's blast radius. **Recommend** making it in the same
  deploy that ships S4. Until then the UI renders the read-only state, which is correct.

## Recommended improvements beyond the ask (clearly recommendations)

- **B3. A census mode for the proposed `audit-refs` verb (`--census`).** Counts only, per scope:
  path-shaped Pointers rows, `RESOLVED` SHAs, `github:` refs and structural `path#Symbol` rows.
  It would MEASURE how much of the store the deterministic checks can reach before anyone decides
  Q4. It prints no entry text.

## What could not be measured

- **Any real store** (private, and no captured text may come here):
  - how many entries carry checkable claims;
  - how many scopes document one repository;
  - **whether any real scope README already has front matter, or a leading `---` horizontal rule
    the anchored parser would misread as front matter.**

  S1's splice tests cover the shapes; whether real READMEs hit them is B3's or the operator's to
  look at.
- **The private audit tooling's hit rates.** It was read by mechanism only.
- **The deployed UI pod's volume mount**, and whether the UI and the pod share a node. The pod
  never writes READMEs, so the lock only contends between UI requests, but that is the code's
  claim, not a measurement of the deployment.
- **Full-mirror sizes and fetch times** for real repositories (T12). Round 1's measurements were
  on a synthetic world.
- **Whether `gitMinimal` on the client's wrapper can fetch over https.** Today it runs two
  `rev-parse`s.
- **GitHub's API today**: tokenless rate limits, and `/pulls/N` against `/issues/N` for a
  converted issue.
- **#208's final shape.** It is unmerged, and decision 9 is written so that does not matter.
- **Sizes and effort.** Not estimated.
