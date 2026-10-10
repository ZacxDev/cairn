# Plan: scope-level code sources, and an auditor that checks entries against them

This is a DESIGN, not a description of anything built. None of it exists yet.

**Where the citations point.** Every claim about today's behaviour carries a `file:line` read off
`origin/main` at **`0355c7a`** (PR #209 merged). Re-read before editing. Two claims were MEASURED
rather than read, and say so where they appear: the loader's answer to a reserved `.md` name
(both clients), and which binaries' import graphs contain `os/exec`.

Examples are synthetic: scopes are `alpha-notes` and `beta-notes`, repositories are
`example-org/example-repo` (and `example-org/example-mono` for a monorepo) on `github.com` or on
`git.example.com`, and the one real repository an example may name is this public one. Operator
decisions are PARAPHRASED, never quoted (`AGENTS.md`).

**Overlap with the plugin plan.** `claudedocs/plan-cairn-plugins.md` exists only on the unmerged
branch `origin/zach/plan-plugins` (PR #208). It is cited by decision number, not line, because it
can still move. Where this plan depends on it, it says so. Where the two could share code, the
decision is made here so neither plan waits for the other.

## Goal and premise

The operator wants to be able to audit cairn automatically against remote refs. Today there is no
deterministic way to do it. **Nothing in cairn records which code a scope documents**, so an
auditor has nothing to check an entry's claims against.

The operator decided what "scope-level refs" means. Each scope declares a **code source**: the
repository or repositories it documents, and the branch. An auditor can then check an entry's
claimed paths, symbols, commit SHAs and PR refs against that remote and flag the stale ones. The
declaration is stored per scope and can be edited in the browser.

There are two outcomes. The second is worthless without the first.

1. **A code-source declaration per scope.** It is machine-readable, validated, editable by
   whoever administers the scope, and readable by the CLI, the browser and the auditor.
2. **A deterministic auditor.** For every entry in a scope it reports which checkable claims hold
   at the declared source's current commit, which do not, and which it could not check. It says
   which of those three each claim is, never folds them together, and never edits an entry.

### What would make this unnecessary

Drop the work, or the named half of it, if any of these holds:

- **The existing out-of-tree audit is enough.** The operator's private tooling already checks
  `RESOLVED <sha>` markers and `## Pointers` paths against LOCAL clones (see "What audits exist
  today"). It infers the scope-to-clone mapping from directory names. If every scope's clone is
  always on the auditing host and always fetched, the declaration buys only the remote check, and
  `git fetch` before the existing audit buys most of that.
- **Nobody acts on the findings.** An audit that prints forty findings a week, which nobody
  reads, is a permanently red gate. Nothing here measures whether stale refs cause wrong
  decisions. The operator's ask is the only evidence of demand. Q4 (whether findings are stored)
  exists so the cheap half can land first and be judged.
- **Scopes rarely document a single repository.** If most scopes are cross-cutting (an org
  process, a client relationship), a per-scope source declares nothing useful for them. The
  declaration is OPTIONAL per scope for that reason. A scope with none reports
  `sources=undeclared` and is never counted as clean.

### closing-condition

- **closing-condition:** `check`. Three mechanical parts, all required:
  1. Slices S1–S5 are MERGED on cairn `main`, verified by content (the files and routes exist on
     `origin/main`), not by ancestry.
  2. **`tests/refaudit/e2e.sh` exits 0 on `main`**, run by a new step in the `go` CI job that is
     NOT `continue-on-error`.
  3. **`tests/conformance/run_go.sh` exits 0 with the new `go_only` rows present**, and
     `python3 tests/conformance/suite.py run` still reports 0 failures (the oracle skips those
     rows by id, with their reason, and counts them).

  `e2e.sh` exits **2** ("could not vouch", never a skip and never 0) when `git` or a built
  `cairn`/`cairn-server` is missing, or when any of its own controls misbehaves.

  **What `e2e.sh` asserts, all against a SYNTHETIC world built at run time** (bare git
  repositories made with `git init --bare` in a temp dir; entries in `alpha-notes` and
  `beta-notes`; no network):

  | clause | what it asserts | negative control inside the clause |
  |---|---|---|
  | **(a) declare** | `cairn sources set alpha-notes <two sources>` exits 0; `cairn sources alpha-notes` prints exactly those two canonical forms, in order; a reader with no grant on `alpha-notes` gets the same answer for it as for a scope that does not exist | a principal holding `write` but not `admin` on the scope is refused (exit 6), and the registry gains no line |
  | **(b) clean world** | `cairn audit-refs alpha-notes --mirror-root <dir>` over a world where every claim holds exits **0** and prints `checked=<N> stale=0 unchecked=0` with N equal to the planted claim count | — |
  | **(c) stale world** | the same command over a world with exactly one missing path, one unreachable SHA, one SHA present only on another branch, and one closed PR exits **9**, and the finding set EQUALS the planted set (by entry, claim and state) | each planted defect, removed one at a time, drops exactly its own finding |
  | **(d) could not look** | a declared source whose mirror is absent exits **10** with that source named, never 0 | the same run with the mirror present exits 0 |
  | **(e) the pod never fetches** | the pod's and the UI's import graphs contain no `os/exec` and no `internal/refaudit` (`go list -deps`) | the CLI's graph DOES contain both (the count can move) |

  `--self-test` applies one sabotage per clause on a scratch copy of the tree with its `.git`
  removed (the `tests/control_mutants.py` pattern). Each must be caught by its own clause's
  message. It prints `sabotaged=5 caught=5`.

  ⚠ **What the closing condition does NOT require: S6 (stored findings) and S7 (a recall
  header).** Both wait on operator answers (Q4, Q5). Both are additive, so the condition does not
  wait for them.

  ⚠ **And it does not cover the GitHub PR-state check against the REAL API.** Clause (c)'s
  closed PR comes from a stub HTTP server the test starts, because CI must not depend on
  github.com. The real API's behaviour is a device-style check the operator runs once (S5's test
  plan). That is stated, not hidden.

## STEP 1 — What audits exist today

**In this repository: none that touch a remote.** cairn's own checks are about entry SHAPE:
- `validate` reports parse refusals, dropped lines and marker reachability
  (`internal/store/validate.go:196`, `:251`, `:455`);
- the open-actions scan lists `OPEN:` bullets (`validate.go:543`);
- the arc registry's `?check=1` mode reports orphans (`tests/conformance/README.md`, "The orphan
  check (S5)").

None of them opens a socket or runs `git`.

**Out of tree: the operator's private hygiene tooling.** It is summarised here by mechanism only,
because that tooling is not public:

| claim in an entry | how it is checked today | against what |
|---|---|---|
| a commit SHA (the `RESOLVED <sha>:` marker, or a backticked hex run) | `git cat-file -e <sha>^{commit}` | every local clone it can find, the scope's own first |
| a path in `## Pointers` | `exists()` on the filesystem | the scope's local clone, then the others |
| a PR or issue ref | `gh pr view`, behind an opt-in flag | GitHub, with the operator's own `gh` login |
| a symbol | **not checked** | — |

- The verdict is three-state: a target exists, could not be tested, or has no home. Nothing is
  deleted automatically.
- The scope-to-repo mapping is **inferred from directory names**: a clone at
  `<workspace>/<scope>` with a `.git`, exact name match, or the scope is "could not test".
- Its own documentation says the check answers about the clone on disk, not the remote, and
  leaves the `git fetch` to a person.
- It warns that a 7-character SHA can match a commit in the wrong repository.
- It warns that a SHA which exists but implements nothing still passes. **An existence check is
  not a relevance check.** That limit carries into this design unchanged (see "What needs
  judgement").

**The plugin plan (#208) describes no auditor.** Its plugins read session transcripts and write
per-session outputs of a closed set of types (`summary`, `ticket-edge`, `ticket-suggestion`;
decision 9). A worker token "cannot read the store" (decision 2). Plugin output is rendered as a
labelled CLAIM (decision 11). Its ClickUp plugin reads `git log` from local repo paths on the
operator's host and PR bodies through the host's `gh`, with "nothing new on the pod" (Q13, S10).
So an auditor-as-plugin would need a new capability (read a scope's entries), a new output type
keyed per scope rather than per session, and #208 to have landed. Decision 9 below therefore does
not wait for it.

## STEP 2 — What exists today, read off the code (`0355c7a`)

### Per-entry refs: `refs:` is `<system>:<id>`, and the id half is opaque

- `TaskRef` is one `<system>:<id>` ref (`internal/store/entry.go:66-84`). The system half is
  normalised and the id half kept byte-identical (`:76-81`).
- `Entry.Tasks` holds an entry's `refs:` in file order, deduped, never normalised
  (`entry.go:129-133`). `tasks:`/`task:` are permanently accepted aliases (`:460-485`).
- `ParseTaskRef` splits on the FIRST colon (`entry.go:570-622`). It refuses whitespace (`:589`),
  a comma — because the inline-list writer would split it (`:594-608`) — and control characters
  (`:612-616`).
- **The URL registry is string formatting, never enrichment** (`internal/store/refurl.go:11-15`).
  It has two tiers:
  - BUILT-IN: `github` and `clickup` only (`:81-120`). `github:owner/repo#N` becomes
    `/owner/repo/issues/N`, which GitHub redirects for a PR (`:99-103`).
  - OPERATOR: `CAIRN_REF_BASE_<SYSTEM>` (`:42-53`, `:208-224`). The second tier exists because a
    self-hosted host may not be written into this public repository, and `leakscan` gates that
    (`:26-35`).
- `EntryReferences` is the ONE "does this entry carry this ref" predicate (`refurl.go:146-153`).

**What this means here:** `github:example-org/example-repo#428` is already a structured,
checkable PR claim. Its owner/repo half can be compared against a scope's declared sources. No
other system's refs have a shape the auditor can check.

### Claims the entry grammar already makes structurally

- **`RESOLVED <sha>:`** — `journalOpenness` (`internal/store/openness.go:47-53`) takes a 7–40
  character hex SHA; `BulletOpenness` lowercases it (`:76-90`). It is parsed in both clients
  because the open-actions and reachability scans read it. The comment states its purpose: the
  SHA makes the claim checkable with `git cat-file -e` (`openness.go:41-42`).
- **`## Pointers`** — one of three declared headings (`internal/store/journal.go:16-20`). Its rows
  are `- path — description`, not dated prose (`internal/report/citationtoken_test.go:21-24`).
  **No parser extracts the path.** It is prose with a convention.
- **Symbols** — backticked spans in prose. There is no structure that says "this backticked span
  is a symbol in that repository".

### The front-matter grammar: strings and lists of strings, nothing else

- `FrontMatter` values are a string or a `[]string` (`internal/store/frontmatter.go:18-22`).
  Accepted forms are `key: value`, an inline `[a, b]` list and a block list (`:54-58`). So a
  source declaration cannot be a nested map. Each source must be one string.
- **MEASURED (Go, `ParseFrontMatter`, at `0355c7a`):**
  - a block list of two source strings parses as a two-element list;
  - the inline form `[a, b]` parses the same;
  - **a trailing `# comment` is NOT a comment.** It becomes part of the value
    (`"…@main # trailing"`). So the source grammar must REFUSE whitespace. Otherwise a comment
    silently becomes part of a branch name.
- **`repo:` is already taken.** It is the older spelling of `scope:` in entry front matter
  (`entry.go:119-121`, `:215-218`), so a key named `repo` would mean two things. This plan's
  key and type names avoid it.

### A scope directory: entries, plus exactly one non-entry `README.md`

- `LoadIndex` reads `<root>/<scope>/*.md` (`internal/store/load.go:133-176`), filtered through
  `IsEntryFileName` (`:374-377`). That predicate excludes exactly `README.md`, the scope's
  policy sheet (`:306-319`). `README.md` is nobody's output: no code in the tree writes one.
- `/snapshot` ships every `*.md` that does not start with a dot, README included
  (`internal/snapshot/snapshot.go:442`; the oracle's `server/server.py:4636`). It ships
  nothing else.
- **MEASURED on BOTH loaders (Go `LoadIndex` and Python `load_index`) at `0355c7a`.** The world
  was one valid entry, a `_scope.md` carrying only `sources:` front matter, and a `README.md`
  carrying the same front matter:
  - `_scope.md` → **1 MALFORMED** row, `missing or empty \`service:\``, in both;
  - `README.md` → not indexed, 0 rows, in both.

  This is the positive/negative pair: the instrument CAN report a malformed reserved file, and
  does not for the README. **Consequence: any new reserved `*.md` name in a scope directory
  shows up as a broken entry on every client that predates it.** Consumers pin this flake, so
  old clients exist and cannot be upgraded on cairn's schedule.
- `ScopeRevision` (`internal/store/revision.go:17-48`) reads `<store>/<scope>/.git/HEAD`. That
  is the STORE's own revision of a scope, not the code a scope documents. It answers `unknown`
  for every served scope today (`:24-28`). **The two must never be conflated.** This plan
  reserves "source" for the documented code, and leaves "revision" meaning the store's own.

### The control journal refuses unknown events WHOLE

- `Event.validate`'s `default:` arm refuses an unknown event kind
  (`internal/control/journal.go:219-226`). `Replay` turns that into a refusal of the WHOLE
  journal (`:640-667`). The reason is authority safety: a dropped revocation is a grant that keeps
  working.
- The `Scope` record is `ID`, `DisplayName`, `ProjectID`, `CreatedAt` (`internal/control/model.go:238-243`).
- **Consequence:** if a code-source event were added to the control journal, then a ROLLBACK of
  the pod or UI to a build that predates it would refuse the authority at startup. An
  unauthorised-everything outage, caused by a feature that has nothing to do with
  authorisation. This is the decisive fact against storing sources there (decision 1).

### The arc registry: the precedent for scope-level, non-entry, non-authority data

- The arc registry is an append-only JSONL journal OUTSIDE the store tree
  (`internal/arcs/arcs.go:1-2`). The pod refuses to start if the journal resolves inside the
  store root, through `arcs.ResolveJournalPath` (`cmd/cairn-server/main.go:381-394`). Inside the
  root it would become a scope.
- It is written under an exclusive `flock`, with one `O_APPEND` write and an `fsync` before
  success (`internal/arcs/journal.go:160-191`). Its fold TOLERATES damaged lines and counts them
  (`Snapshot.Damaged`, `:65`). That is the right failure direction for non-authority data.
- The pod stamps who pushed and when; every other field is the tooling's own word
  (`arcs.go:15-19`). That is the provenance shape a source declaration and an audit finding
  both need.
- The arc routes are **Go-only by decision** (`internal/api/routes.go:67-77`). The four places a
  Go-only head moves together are:
  - that table;
  - the `go_only` rows in `tests/conformance/requests.json`;
  - `tests/testlib/capability_ledger.py`'s `go_only` rows (`:39-43`, `:112-128`);
  - `flake.nix`'s `want-go-only.txt`.

  A Go-only golden is a CHANGE DETECTOR, not a contract witness (`tests/conformance/README.md`,
  "…and the mirror").
- Go-only CLI verbs are legal **iff declared** (`tests/test_go_client_ledgers.py:16-19`).

### The browser surface: sharing is the edit-flow precedent

- UI routes are a declared table (`internal/ui/routes.go:118-185`). `POST /share` and
  `POST /unshare` are the existing scope-administration writes (`:147-149`).
- Both cross-site gates derive from the METHOD (`stateChanging`, `internal/ui/server.go:1323`):
  - same-origin, before auth;
  - a per-session CSRF token, after auth (`:1305-1306`).

  So a new `POST` row is covered by construction.
- The share handler authorises with `id.Auth.Allows(scope, control.VerbAdmin)`
  (`internal/ui/sharehandlers.go:170`).
- `cairn-ui` READS the store and never writes it. It writes only the control journal, which must
  resolve outside `-store` (`cmd/cairn-ui/main.go:246-266`). The arc journal is mounted
  READ-ONLY in the UI (`internal/ui/README.md`, "The binary: the pod's flag…").

### The CLI: the only binary that spawns `git`, and an exit model the auditor can reuse

- `internal/client/reposcope.go` is the ONE `git` call site (`:38-58`). It runs with
  `GIT_OPTIONAL_LOCKS=0`. It derives a scope from a LOCAL checkout's common-dir basename
  (`:60-103`). That is the reverse direction (checkout → scope), local only, and by name.
- `packages.cairn-go` puts `gitMinimal` on its wrapper's `PATH` for that call
  (`flake.nix:610-616`).
- **MEASURED (`go list -deps`, at `0355c7a`):** `os/exec` appears in `cmd/cairn`'s import graph
  (1 of 204 packages), and in neither `cmd/cairn-server`'s (0 of 209) nor `cmd/cairn-ui`'s (0 of
  232). That pair is clause (e)'s baseline. The pod and the UI cannot spawn a process today.
- The exit model is a printed contract (`internal/client/exit.go:3-67`). `arcs --check` reuses
  doctor's `0/9/10` (`exit.go:18`): `9` means a check MEASURED a problem, `10` means a check
  COULD NOT LOOK (`internal/doctor/render.go:46-51`). That is exactly the auditor's three-way
  answer.
- **No Go test runs `git` today.** The only `"git"` strings in `_test.go` files are a fixture
  name and the dependency policy's own module walk. So S1 makes the first, and the nix check
  inputs need `gitMinimal`.

### The recall header

`RecallReport.RenderText` writes the status line, `store:`, the host line and any extra header
lines, then `caveat:` (`internal/report/text.go:306-313`). A new conditional line moves no
existing golden. But it is a RENDERER change, and the renderer is shared by the pod and the CLI
and diffed against the Python oracle's bytes by the reader fixture and the parity gate (see S7).

## Decisions — who chose what

### Chosen by the OPERATOR (paraphrased; not re-litigated)

| # | what the operator chose | where it lands |
|---|---|---|
| O1 | Automatic auditing of cairn against remote refs is wanted, and today there is no deterministic way to do it. | The goal. |
| O2 | "Scope-level refs" means a **code source per scope**: the repo(s) and branch each scope documents, so an auditor can check entries' paths, symbols, SHAs and PR refs against that remote and flag stale ones. | Decisions 2 and 6. |
| O3 | The declaration is **stored per scope** and **editable in the UI**. | Decisions 1 and 4. |

### Chosen by the AGENT writing this plan (open to review)

1. **AGENT RECOMMENDATION — store sources in a NEW append-only registry journal OUTSIDE the
   store tree, on the arc registry's discipline. Not a file in the scope directory, and not the
   control journal.** Flag `-source-journal`, env `CAIRN_SOURCE_JOURNAL`, no default. Unset means
   the routes answer `sources-unconfigured` and the UI says so on the page. Startup is refused if
   the path resolves inside the store root (reusing `arcs.ResolveJournalPath`). Records are
   `{scope, sources:[…], set_by, set_at, revision}`. The fold is latest-wins per scope, and the
   history is the journal. Records are keyed by scope NAME, as arcs are (see Q12 on renames).

   The five candidates were weighed against what was measured above:

   | where | old clients | rollback | UI edit | history / provenance | verdict |
   |---|---|---|---|---|---|
   | **`_scope.md` (new reserved `.md`)** | **MEASURED: one MALFORMED row on every pre-feature client, both languages** | fine | the UI must WRITE the store (it never has) | none (overwrite) | **rejected** |
   | **`README.md` front matter** | MEASURED: invisible to both loaders, shipped by `/snapshot` | fine | the UI must write the store, splicing front matter into an operator's prose file | none | runner-up (Q1) |
   | **a dot-file in the scope dir** | ignored | fine | the UI must write the store | none | **rejected**: `/snapshot` does not ship dot-files, so caches would never see it, and changing that touches both servers' byte-identity gate |
   | **control journal event** | n/a | **an old build refuses the WHOLE authority** (`journal.go:219-226`) | UI already writes it | native | **rejected**: a feature rollback becomes an auth outage |
   | **a new registry journal (recommended)** | n/a: Go-only reads | an old build ignores an env var it does not know | the UI writes ONE file (not the store), like the control journal | native: `set_by`/`set_at` are pod/UI-stamped | **recommended** |

   **Why "in the snapshot, readable offline" did not decide it.** It was the README option's real
   advantage, and it buys nothing here: auditing against a REMOTE is online by definition. A
   client that can fetch the remote can fetch the declaration.

   ⚠ **The env var, not only the flag, is the rollback story.** A pre-feature binary handed an
   unknown FLAG refuses to start. Handed an unknown ENV var, it ignores it. So the deployment sets
   `CAIRN_SOURCE_JOURNAL`, and the flag exists for parity with `-arc-journal`.

2. **AGENT RECOMMENDATION — the source grammar is one string per source:**

   ```
   git:<host>/<repo-path>[//<subpath>]@<branch>
   ```

   - `<host>` is a DNS name, lowercased. No scheme, no userinfo, no port in v1.
   - `<repo-path>` is ≥ 2 `/`-separated segments. Each segment is `[A-Za-z0-9._-]+`, does not
     start with `-` or `.`, and a trailing `.git` is stripped. Case is PRESERVED and compared
     byte-identically. GitHub folds case and other hosts do not, so folding would make two
     repositories equal on some hosts. This is the same asymmetry `TaskRef`'s id half keeps.
   - `//<subpath>` (optional) names a monorepo directory. `//` cannot occur inside a normalised
     repo path, so the boundary is unambiguous even when an owner has nested groups. The subpath
     is ≥ 1 segment under the same character rule, with no `..` segment.
   - `@<branch>` is REQUIRED. There is no default-branch inference, because inference needs the
     network and would make a declaration mean different things on different days. A branch is
     a valid `git check-ref-format --branch` name, does not start with `-`, contains no `@`, and
     contains no whitespace (measured above: a `# comment` would otherwise be absorbed).
   - **No comma anywhere**, for `ParseTaskRef`'s reason (`entry.go:594-603`). No control
     characters.
   - **Canonical form** is the normalised string. Two declarations equal after normalisation are
     one source; duplicates are DEDUPED, not refused (the `aliases:` rule). ORDER IS KEPT: the
     first source is the scope's primary.
   - **≤ 8 sources per scope.** A cap whose violation is refused with the number. It bounds the
     auditor's fetches. The number is a guess, so it is stated as one (Q-list).

   Examples, all synthetic: `git:github.com/example-org/example-repo@main`;
   `git:github.com/example-org/example-mono//services/widget@release/2.x`;
   `git:git.example.com/team/sub-group/example-repo@trunk`.

   **One parser: `internal/codesrc.Parse`.** It is used by the pod's `PUT`, the UI's `POST`, the
   CLI's `sources set`, and the auditor when it reads a declaration back. A refusal names the
   rule broken and the fix, in the `ParseTaskRef` style.

3. **AGENT RECOMMENDATION — Go-only, by the arcs/sessions decision 3 precedent. The Python
   client and the oracle never read sources.** That removes the "two parsers pinned against each
   other" problem: there is ONE parser. Python is retirable at P8 (`AGENTS.md`), and a second
   parser would be a P8 deletion written today.

   This holds only while S7 (a recall header) is not built. If the operator wants the source in
   EVERY recall from BOTH clients (Q5), then `lib/` gains a twin parser and a shared fixture
   file, pinned the way `tests/test_env_aliases.py` pins `internal/envalias` against
   `lib/env_aliases.py`. That cost is S7's, and it is named there.

4. **AGENT RECOMMENDATION — who may set sources: a principal holding `admin` on the scope; who
   may read them: anyone holding `read`.**
   - Changing what code a scope claims to document redirects every future audit of it. That is
     the same class of act as sharing, so it gets the share flow's verb
     (`sharehandlers.go:170`).
   - A `write` holder can add entries but cannot repoint the audit.
   - READ follows scope visibility. A non-reader gets the uniform refusal, identical for an
     unreadable scope and an absent one (the existing refused-equals-absent relation, which every
     Go-only read head joins).
   - ⚠ **A token-file deployment has no `admin` grants**: its projection confers `admin` on
     nobody (`internal/control/tokenfile/source.go:424-425`). So in that deployment sources
     are settable by NOBODY through the API or the UI. Seeding the journal by hand is the
     supported route, the same answer the token-file deployment gives for sharing. Stated so
     nobody files it as a bug.

5. **AGENT RECOMMENDATION — the edit flow:**
   - `GET /scope/sources?scope=<s>` renders the form;
   - `POST /scope/sources` sets the sources;
   - both are rows in the UI's declared table;
   - same-origin and CSRF come from the method, so no handler opts in (`server.go:1305`, `:1323`);
   - authorisation is decision 4;
   - the read-only and unconfigured states are rendered on the page, as `ReadOnlyAuthority` is.

   **The input control is ONE `<textarea>`, one source per line** (a FORK, Q6). It carries a
   hidden `revision`, the registry's current record digest. A POST whose revision is stale is
   refused with the CURRENT list rendered, so two admins editing at once lose nothing silently
   (the `If-Match` idea, in a form). A refusal re-renders the textarea with the operator's text
   and names the failing LINE NUMBER and the rule.

   The scope page gains a **Sources** block:
   - each source as text;
   - a link ONLY for the built-in `github.com` shape (`https://github.com/<repo-path>/tree/<branch>`),
     through `safeHref`;
   - the registry's `set_by` and `set_at`.

   Every other host renders as inert text, the same answer `refurl.go`'s operator tier gives
   when no base is configured (`:36-40`).

6. **AGENT RECOMMENDATION — what the auditor checks DETERMINISTICALLY.** Each check is a fact
   about a named commit, never about "the branch", because a branch moves. The auditor first
   resolves each source's `@<branch>` to a 40-hex commit and quotes that commit in every finding.

   | claim, and where it comes from | check | states it can report |
   |---|---|---|
   | **path** — the first backticked span of a `## Pointers` row, or its first token before ` — `, if it is path-shaped (contains `/` or a `.<ext>`, no scheme, no whitespace, does not start with `-`) | `git ls-tree <commit> -- <path>`, which reads TREES only, so a partial clone needs no blob. Tried repo-root-relative first, then subpath-relative (Q7) | `holds(root)` · `holds(subpath)` · `missing` · `unchecked: not path-shaped` |
   | **path:line** — the same, with a `:<N>` suffix | the path check, plus the blob's line count ≥ N | `holds` · `missing` · `line-beyond-eof` |
   | **commit** — `RESOLVED <sha>:` (`openness.go:47-53`) | `git rev-parse --disambiguate`, then `git merge-base --is-ancestor <sha> <commit>` | `on-branch` · `exists-off-branch` · `absent` · `ambiguous` (a short SHA matching > 1 object) |
   | **PR** — a `refs:` item `github:<owner>/<repo>#N` (`refurl.go:104-118`) | GitHub REST `GET /repos/<owner>/<repo>/pulls/N` (falling back to `/issues/N` on 404), stdlib `net/http` | `open` · `merged` · `closed-unmerged` · `not-found` · `unchecked: rate-limited / no token` |
   | **ref outside the manifest** — a `github:` ref whose owner/repo is none of the scope's declared sources | string comparison only | `outside-sources` (informational) |
   | **symbol** — ONLY in the STRUCTURAL form `path#Symbol` in a `## Pointers` row (Q8) | `git grep -w -F -e <Symbol> <commit> -- <path>` | `present` · `absent-from-file` · `file-missing` |

   **Severity is decided by the state, in one table in one package, never at a call site:**
   - **Stale** (exit 9): `missing`, `line-beyond-eof`, `absent` (a commit), `absent-from-file`,
     `file-missing`, `not-found`.
   - **Informational**, printed but never escalating the exit: `exists-off-branch`,
     `closed-unmerged`, `merged`, `open`, `outside-sources`, `holds(subpath)`.
     - `exists-off-branch` is the squash-merge shape. A `RESOLVED` SHA from a feature branch is
       NEVER an ancestor of the branch after a squash merge, so calling it stale would be wrong
       every time.
     - Whether a closed PR makes an entry stale is judgement.
   - **Could not look** (exit 10 when nothing was stale): a mirror is unreachable, `ambiguous`,
     or the PR check was rate-limited or had no token.

   **What is NOT checked, deliberately:**
   - backticked spans in free prose, which carry no structure saying "symbol";
   - `RESOLVED` SHAs in the PROSE after the marker;
   - URLs;
   - anything under an operator-tier ref system.

   They are counted as `unchecked`, so the denominator is honest. They are never silently
   dropped.

7. **What needs JUDGEMENT, and therefore stays out of the exit code:**
   - whether a file that still exists still says what the entry claims;
   - whether a `RESOLVED` commit actually implements the claim;
   - whether an `OPEN:` item is still open;
   - whether a closed-unmerged PR means the work was abandoned or moved;
   - whether a symbol that moved files is the same symbol.

   The auditor can PRINT inputs for those judgements, for example "the cited file changed in
   N commits since the entry's newest dated bullet". **It never renders a verdict on them.** That
   division is the "prefer deterministic" rule taken literally: a heuristic that reads as a
   verdict is worse than no verdict.

8. **AGENT RECOMMENDATION — where the auditor runs: a CLI verb, `cairn audit-refs <scope>`, on
   an operator host, with the operator's own credentials. The pod and the UI never fetch,
   never hold remote credentials, and cannot spawn a process** (clause (e) pins the import graph).
   - **Reading.** It reads entries the way `recall` does: live from the pod, or from the synced
     cache. It reads the declaration from `GET /api/v1/sources/<scope>`.
   - **Fetching.** It fetches each source into a mirror under
     `$XDG_CACHE_HOME/cairn/mirrors/<sha256(canonical source)>.git`, as
     `git fetch --filter=blob:none` of one branch. The directory name is a DIGEST, never a path
     built from host/owner/repo, so a hostile declaration cannot traverse.
   - **Credentials.** `git` uses whatever credential helper or SSH agent the operator already
     has. The PR check reads `GH_TOKEN`/`GITHUB_TOKEN` from the environment, the `gh`
     convention. With neither, it is `unchecked` (exit 10 if nothing else was stale), never
     silently skipped. **No credential is ever printed, logged, or sent to the pod.**
   - **`--mirror-root <dir>`** makes the auditor read existing local repositories and fetch
     nothing. That is the test seam (clauses (b)–(d)). It is also what a CI job inside a
     documented repository would use against its own checkout (B1).
   - **`--json`** emits the findings as data, for S6 or a plugin.
   - **Exit codes** reuse doctor's `0/9/10`, as `arcs --check` does. No new code is minted, so
     neither exit ledger moves.

9. **AGENT RECOMMENDATION — the auditor is a LIBRARY first (`internal/refaudit`), so #208 can
   wrap it later, and nothing here waits for #208.** `internal/refaudit` takes plain values (the
   entries, the declaration, a `GitReader` interface, a `PRReader` interface) and returns plain
   findings, in the `internal/report` library shape.
   - The CLI verb is its first caller.
   - If #208 lands and grows a "read a scope's entries" capability plus a scope-keyed output
     type, a plugin can call the same library on a timer. **The verdicts then cannot differ
     between the two callers**, because there is one implementation.
   - Making the auditor a plugin FIRST would put this feature behind #208's S3–S6 (transcript
     store, worker listener, registry, outputs). None of those is needed to check a path.

10. **AGENT RECOMMENDATION — findings are PRINTED in v1 and stored only in S6, as CLAIMS with
    provenance, never as edits to entries.**
    - Every finding carries:
      - `source`, the canonical declaration;
      - `commit`, 40-hex;
      - `entry`, the `<scope>/<ref>`;
      - `claim`, the extracted text, which is what the entry already says, not captured text;
      - `check`, the rule id;
      - `state`;
      - `checked_at`;
      - `checker`, the cairn version, which is the git revision.
    - S6 stores a run with `PUT /api/v1/ref-audit/<scope>` (Go-only) into a THIRD journal.
      `audited_by`/`audited_at` are stamped by the pod, on the arc registry's provenance split
      (`arcs.go:15-19`). The UI renders the latest run per entry inside a labelled "derived"
      box, the way #208's decision 11 renders plugin output.
    - **The auditor never writes a bullet.** A machine claim written into an entry is
      indistinguishable from a human one by the next reader, which is exactly the class #208
      refuses for plugins (T8 there: "no write path except outputs").

11. **Recall gets NO source line in v1 (S7, Q5).** `cairn sources <scope>` and the UI's scope
    page answer "what does this scope document". A recall-header line is a renderer change
    diffed against the Python oracle, so it either drags in a Python parser (decision 3) or
    becomes a declared parity residual. Neither is worth paying before anyone has asked for the
    line in recall specifically.

## Threat model

| threat | control |
|---|---|
| **T1. A hostile declaration aims the auditor's fetch at an attacker's host, harvesting the operator's git credentials** | The auditing HOST holds an allowlist: `CAIRN_AUDIT_HOSTS`, default `github.com` only (Q9). A source whose host is not on it is `unchecked: host not allowed` and is never contacted. `git` runs with `GIT_ALLOW_PROTOCOL=https` (refusing `ext::`, `file::` and `ssh` options), `GIT_TERMINAL_PROMPT=0`, `GIT_CONFIG_NOSYSTEM=1`, and the URL built from the canonical form as `https://<host>/<repo-path>.git`. Credential helpers are keyed by host, so an allowlisted host gets only its own credential. |
| **T2. Argument injection** (a branch, path or symbol starting with `-`) | Refused at parse (decision 2), and passed after `--end-of-options` or `--` at every `git` call site. One call site: `refaudit`'s `GitReader` implementation, the `runGit` shape. |
| **T3. Mirror-path traversal** (`..` or `/` from a declaration reaching the filesystem) | Mirror directories are named by `sha256(canonical source)`. No declaration byte reaches a path. |
| **T4. Repointing a scope's audit** | Setting needs `admin` (decision 4). The UI POST passes both method-derived gates. Every change is a journal line with `set_by`/`set_at`, so "who repointed this, and when" is answerable. |
| **T5. Disclosure of private repository names** | Sources are readable only by scope readers, with a uniform refusal otherwise. A scope reader can already read entries that name the same repository, so this widens nothing for them. Stated rather than assumed: the declaration DOES name a repo that a reader might not otherwise have seen spelled out. |
| **T6. A feature rollback takes authorisation down** | Not in the control journal (decision 1; `journal.go:219-226` measured the reason). The registry is a separate, damage-tolerant file, configured by an env var an old binary ignores. |
| **T7. A finding mistaken for a verdict on meaning** | Severity is a closed state table (decision 6). Judgement-class questions never move the exit code (decision 7). The output header says `existence checks only — a holding claim may still be wrong`. |
| **T8. A stale run read as current** | Every finding quotes its resolved `commit` and `checked_at`. S6's UI box shows the age. |
| **T9. The pod or UI gains network egress to code hosts** | Clause (e): no `os/exec` and no `internal/refaudit` in either binary's import closure, enforced by a test in `internal/depspolicy` beside the existing import ban. ⚠ That is a STRUCTURAL check on `exec`. It cannot see an `net/http` client call added to the pod, because the pod already imports `net/http` to serve. Named, not hidden. |
| **T10. Stored XSS through a source string** | Text is rendered through gomponents `Text` only. The only href is the built-in GitHub shape, through `safeHref`. `Raw`/`Rawf` stay AST-banned (`AGENTS.md`). |
| **T11. Resource exhaustion on the auditing host** | ≤ 8 sources per scope, blob-less fetch, one branch, a per-git-call timeout, and a per-run PR-check cap. A run that hits a cap reports `unchecked`, never clean. |
| **T12. A committed fixture leaks a real repository** | The e2e world and every Go test build their repositories at run time with synthetic names. `leakscan` runs in CI. No captured text (`AGENTS.md`). |

## Slices

Each slice is mergeable alone, leaves `main` releasable, and has tests that run on that slice
alone.

| slice | what | ledgers it moves | mergeable alone because |
|---|---|---|---|
| **S1** | **`internal/codesrc`**: `Parse`/`Canonical` (decision 2) and the registry journal (`Read`/`fold`/`Set`, on `internal/arcs/journal.go`'s flock + `O_APPEND` + `fsync` discipline; damage-tolerant; latest-wins; `revision` = digest of the folded record). Library only. Adds `gitMinimal` to the Go derivations' check inputs, ready for S5. | new package → `go` job `ok` floor; `tests/control_mutants.py` `PKGS` plus its pinned count; `onlyGo` if fixtures are files. | Nothing imports it. |
| **S2** | **Pod wiring**: `-source-journal`/`CAIRN_SOURCE_JOURNAL`; the inside-store refusal (shown RED first); `GET`/`HEAD /api/v1/sources/<scope>` and `PUT /api/v1/sources/<scope>` (admin, optional `If-Match` → 412); `sources-unconfigured` when unset. | `internal/api/routes.go` `readHeads`/`writeKeys` (`DeclaredRoutes` derives); `requests.json` `go_only` rows + `record-go-only` goldens; `capability_ledger` `go_only` rows; `want-go-only.txt`; `checks.go-server-declares-its-routes`; README rows. | Inert unless the journal is configured. |
| **S3** | **CLI verbs**: `cairn sources <scope>` (read) and `cairn sources set <scope> <source>…` (write codes 6/7/8). | `internal/client` verb table → `cairn -verbs`; `want-go-only-verbs.txt`; `capability_ledger`; `tests/test_go_client_ledgers.py` sees them as declared Go-only. | Additive verbs; no exit code minted. |
| **S4** | **UI**: the Sources block on the scope page; `GET`/`POST /scope/sources` (decision 5); `cairn-ui -source-journal`/`CAIRN_SOURCE_JOURNAL` (refused inside `-store`); read-only and unconfigured states on the page. | `internal/ui/routes.go` table; hand ledger and near-miss probes; the uiaudit world gains one declared scope; mutant rows; `internal/ui/README.md`. | Renders nothing when unconfigured. |
| **S5** | **`internal/refaudit` + `cairn audit-refs`** (decisions 6–9): `GitReader` (the one `git` call site, `--end-of-options`, the env hardening of T1), `PRReader` (stdlib `net/http`), the state table, `--mirror-root`, `--json`, `0/9/10`; the import-graph test (clause e); `tests/refaudit/e2e.sh` with clauses (a)–(e) and `--self-test`; its CI step. | new package → `ok` floor, `PKGS`; `internal/depspolicy` (the pod/UI `os/exec` ban test); verb ledgers as S3; `ci.yml` step; README. Needs S2 + S3 for clause (a). | A read-only verb that writes nothing anywhere. |
| **S6** *(after Q4)* | **Stored findings**: `PUT /api/v1/ref-audit/<scope>` into `-ref-audit-journal`; `audit-refs --record`; the UI's derived box per entry. | as S2 for the route; as S4 for the UI. | Additive; the box renders nothing with no runs. |
| **S7** *(after Q5)* | **Recall header** `  sources: <canonical>[, …]` when a scope declares any. | `internal/report/text.go`; `lib/` twin parser + recall line; `tests/reader_fixtures.py` regenerate-and-diff; parity world gains a declaration; conformance goldens for one declared scope. | Absent when nothing is declared, so no existing golden moves. |

**Mutant rows** (indicative names; they join `tests/control_mutants.py` and move its pinned
count):

- **S1:**
  - `codesrc-accepts-a-non-dns-host`
  - `codesrc-branch-may-lead-with-a-dash`
  - `codesrc-whitespace-in-branch-accepted` (the `# comment` absorption)
  - `codesrc-repo-path-case-folded`
  - `codesrc-subpath-dotdot-accepted`
  - `codesrc-duplicate-refused-not-deduped`
  - `codesrc-order-not-kept`
  - `codesrc-fold-earliest-wins`
  - `codesrc-damaged-line-refuses-whole-journal`
- **S2:**
  - `api-sources-put-needs-only-write`
  - `api-sources-get-distinguishes-absent-from-unreadable`
  - `api-sources-if-match-ignored`
  - `api-sources-journal-inside-store-accepted`
- **S4:**
  - `ui-sources-post-skips-admin-check`
  - `ui-sources-stale-revision-overwrites`
  - `ui-sources-links-a-non-github-host`
- **S5:**
  - `refaudit-off-branch-counted-stale`
  - `refaudit-existence-counted-as-ancestry`
  - `refaudit-unreachable-mirror-reports-clean`
  - `refaudit-closed-pr-escalates`
  - `refaudit-unchecked-dropped-from-denominator`
  - `refaudit-host-allowlist-bypassed`
  - `refaudit-mirror-dir-from-path-components`

### Test plan per slice (negative controls named)

**S1.**
- **Parse.** Every example above maps to a LITERAL canonical string, pinned, never derived from
  `Canonical`. Each refusal has a fixture that hits only that refusal:
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
- **Reachability of each refusal** (the unreachable-guard rule). Each fixture is valid in every
  respect but the one it tests, so no earlier check can win. The test asserts that refusal's OWN
  message.
- **Case.** `git:github.com/Example-Org/Repo@main` and `…/example-org/repo@main` are two sources.
  Fixtures are pairwise distinct and distinct from any constant the assertion names.
- **Fold.**
  - Two sets for one scope: the later wins. Reversing the fold must go red (positive control).
  - A torn tail and a non-JSON line are SKIPPED and counted (`Damaged()`), never refuse the file.
  - A record carrying an unknown field loads. That is the rollback property. It is an
    **invariant guard**, labelled as one, because no build has yet written such a field.
- **Concurrency.** Two `Set`s racing on one file both land, serialised by the lock. This is
  measured with the arc journal's existing interleave seam, not with wall-clock sleeps.

**S2.**
- **RED first:** a journal path inside the store root refuses startup with its own message. Two
  controls come up: a path outside the root, and an unset path (`sources-unconfigured`).
- **Authz matrix**, literal bodies:
  - `admin` → 200;
  - `write` → the uniform refusal, with no journal line added. The negative control is the line
    COUNT before and after.
  - `read` → GET 200, PUT refused;
  - no grant → GET byte-identical to an absent scope's.
- **`If-Match`.** A stale revision → 412 carrying the current one; a matching one → 200.
- **Corpus.** `go_only` rows for GET, HEAD and PUT. `record-go-only` refuses if no 2xx is
  observed. `run_go.sh` exits 0, and the oracle run reports them as skipped by id.
  `TestTheRouteLedgerMatchesTheConformanceCorpus` goes RED with the table row present and the
  corpus row absent (shown once).

**S3.**
- `cairn sources alpha-notes` prints the canonical list.
- `sources set` with an invalid source exits **6** and prints the parser's own message; with the
  pod down it exits **7**.
- `cairn -verbs` lists both verbs. `tests/test_go_client_ledgers.py` goes RED if
  `want-go-only-verbs.txt` omits one (shown once).

**S4.**
- **The form.** The page renders the current list and revision.
- **POST outcomes:**
  - a valid POST lands one journal line stamped with the signed-in principal;
  - a POST with a stale revision lands nothing and re-renders the CURRENT list;
  - an invalid line is refused naming its line number, and the operator's text is preserved.
- **Gates:**
  - a POST without CSRF → 403, and without same-origin → refused. Both come for free from
    `stateChanging`, and the test asserts that rather than assuming it;
  - a `write`-only principal → the share flow's refusal.
- **Links.**
  - a `github.com` source renders exactly one `href` equal to the literal
    `https://github.com/example-org/example-repo/tree/main`;
  - a `git.example.com` source renders NO `href`. That is the negative control that the link is
    not built for every host.
- **uiaudit** captures the scope page with a declared source at the existing widths.

**S5.**
- **The state table.** One synthetic repository world built in the test:
  - a file;
  - a file under a subpath;
  - a commit on `main`;
  - a commit on a side branch only (the squash shape);
  - two commits sharing a 7-hex prefix, made by brute-forcing commit timestamps in the test, with
    the run time named;
  - a stub GitHub server answering open, merged, closed and 404.

  Each planted claim maps to exactly one LITERAL state.
- **The matrix.** Every state above is RED at `0355c7a`, because the verb does not exist, and
  green at head. The interesting reds are the MUTANTS:
  - `refaudit-off-branch-counted-stale` must flip the squash fixture to exit 9;
  - `refaudit-unreachable-mirror-reports-clean` must flip clause (d) from 10 to 0.
- **Positive control for every zero.** `stale=0` is printed beside `checked=N`, with N equal to
  the planted count. The clean world must report N > 0, or the run refuses with exit 2 ("could
  not vouch"). A wrongly-wired extractor therefore cannot pass as clean.
- **Hardening.**
  - a declaration naming a host off the allowlist is never contacted (the stub server counts
    connections: 0), with the allowlisted host's count (> 0) as the pair;
  - a branch beginning with `-` never reaches `git`, because the parser refuses it. A mutant that
    deletes the parse refusal must then be caught by `--end-of-options`, measured by a `git`
    shim that records argv.
- **Import graph** (clause e). The test asserts `os/exec` and `internal/refaudit` are absent from
  `cmd/cairn-server` and `cmd/cairn-ui` and PRESENT in `cmd/cairn`. That is the pair. Adding a
  blank import of `os/exec` to `cmd/cairn-server` goes RED (shown once).
- **Against the real GitHub API, once, by the operator, not in CI:**
  `GH_TOKEN=… cairn audit-refs <a scope citing a PR in this public repository>`. Record the
  states, and record whether the unauthenticated run reports `unchecked: rate-limited` rather
  than a state.

**S6/S7.** These are planned after Q4/Q5. Each would carry the same shape:
- **S6:** a literal-body Go test per route, `go_only` corpus rows, and a uiaudit capture of the
  derived box;
- **S7:** `tests/reader_fixtures.py` regenerated and DIFFED, a parity row carrying a declared
  scope, and a declared-absent control proving no existing golden moved.

## Open questions

### For the operator — each with the agent's recommendation

- **Q1. Where sources are stored.** **Recommend the separate registry journal** (decision 1).
  The runner-up is `README.md` front matter. It is invisible to old clients (measured) and it
  travels in the snapshot, but the UI would become a store writer, and an operator's prose file
  would carry machine data with no history. `_scope.md` is rejected on a measurement, and the
  control journal on its refuse-whole rule.
- **Q2. Go-only?** **Recommend yes** (decision 3). Answering no means a Python twin parser and a
  pinned fixture now, for a client P8 retires.
- **Q3. Where the auditor runs.** **Recommend the CLI verb on an operator host, on a user timer,**
  wrapping the `internal/refaudit` library (decisions 8–9).
  - Later, if wanted, a #208 plugin around the same library.
  - Later, if wanted, a CI job inside each documented repository (B1).
  - Not the pod, ever.
- **Q4. Are findings stored?** **Recommend print-only first (S5), then S6 if the printed findings
  turn out to be read.** S6's closing evidence would be the operator's own call after a few weeks
  of S5 runs.
- **Q5. A source line in every recall?** **Recommend no for v1** (decision 11). The scope page and
  `cairn sources` answer it.
- **Q6. The UI input control (a FORK).** **Recommend one textarea, one source per line**, with
  line-numbered refusals. The alternative is repeated single-line rows with add/remove. That
  needs script (a second allowlisted script) or a round-trip per row.
- **Q7. Path base in a monorepo.** **Recommend: try repo-root-relative, then subpath-relative,
  and report which held** (`holds(root)` / `holds(subpath)`). The alternative, subpath-only, is
  stricter and will flag every root-relative pointer an operator already wrote.
- **Q8. Symbols.** **Recommend the STRUCTURAL form only (`path#Symbol` in a `## Pointers` row),
  and leave prose backticks unchecked.** A heuristic over backticks is the prose-detector shape
  `openness.go:33-40` explains rejecting. This is a deterministic-versus-heuristic choice, so it
  is the operator's. The heuristic would find more claims and be wrong about some of them.
- **Q9. The host allowlist's default.** **Recommend `github.com` only.** A self-hosted forge is
  added per auditing host through `CAIRN_AUDIT_HOSTS`, never in this repository, for
  `refurl.go`'s reason (`:26-35`).
- **Q10. Who may set sources.** **Recommend `admin`** (decision 4). `write` would let any
  contributor repoint the audit.
- **Q11. An `exists-off-branch` commit.** **Recommend informational, never stale.** It is the
  squash-merge shape, so calling it stale is wrong by construction.
- **Q12. Scope renames.** The registry is keyed by name, as arcs are. **Recommend** that the
  journal-backed deployment's rename path ALSO appends a re-keyed record here (one line, same
  writer), rather than keying on the control plane's scope ID. Keying on the ID would make the
  token-file deployment, which has no IDs, unable to hold sources at all.
- **Q13. An entry that documents a different repository than its scope.** **Recommend deferring
  an entry-level `sources:` override** until a measured case exists. Today the auditor tries
  every declared source and reports `holds` in whichever matched.
- **Q14. The ≤ 8 cap.** A guess. **Recommend keeping it** until a real scope needs more. Raising
  it is a one-constant change.

## Recommended improvements beyond the ask (clearly recommendations)

- **B1. A pre-merge check inside a documented repository.** `cairn audit-refs --mirror-root .
  --source <this repo's canonical form>` in that repository's CI, against the PR's merge commit.
  It answers "this PR deletes a file N cairn entries cite" BEFORE the merge, which is the moment
  the information is cheapest to act on. It needs a cairn READ credential in that CI, which is a
  new place a credential lives, so it is the operator's call per repository.
- **B2. `cairn sources suggest`.** For a local checkout, print the canonical source derived from
  its `origin` and current branch, by reusing `reposcope.go`'s `git` call site. This turns "type
  the grammar by hand" into "confirm a suggestion". It never sets anything by itself.
- **B3. A census mode, `audit-refs --census`.** Counts only: path-shaped Pointers rows,
  `RESOLVED` SHAs, `github:` refs, and structural `path#Symbol` rows per scope. It would MEASURE
  how much of the store the deterministic checks can reach, before anyone decides S6 is worth
  building. It prints no entry text.
- **B4. Retire the out-of-tree name-inference.** Once sources exist, the operator's private
  audit can read `cairn sources <scope>` instead of guessing a clone from a directory name. That
  is one rule in one place, and the inference is the predicate this would replace.

## What could not be measured

- **Any real store.** Both real stores are private, and `AGENTS.md` forbids captured text here.
  So there is no measurement of:
  - how many entries carry path-shaped Pointers rows, `RESOLVED` SHAs or `github:` refs;
  - how many scopes document a single repository;
  - therefore, what fraction of claims the deterministic checks can reach.

  B3 exists to measure that on the operator's host.
- **The private audit tooling's actual hit rates.** It was read by mechanism only.
- **`flock` between the pod and the UI across the deployed volume.** Both would append to the
  registry. `flock` is advisory-only across hosts on some network filesystems; the arc journal's
  comment names the same limit (`internal/arcs/journal.go:160-168`). Whether the two processes
  share a node in the deployment is not in this repository.
- **Whether `gitMinimal` on the client's wrapper can fetch over `https`.** Today it only runs two
  `rev-parse`s. S5 must measure a real fetch from the wrapped binary before relying on it.
- **Partial-clone behaviour.**
  - whether `ls-tree` on a `blob:none` mirror ever triggers a lazy fetch;
  - the cost of `git grep` for the symbol check, which DOES need blobs.
- **GitHub's API behaviour today**: unauthenticated rate limits, and `/pulls/N` versus
  `/issues/N` for a converted issue. Both are S5's one-time operator check.
- **#208's final shape.** Unmerged; decision 9 is written so that it does not matter.
- **Sizes and effort.** Not estimated.
