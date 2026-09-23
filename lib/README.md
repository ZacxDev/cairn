# `lib/` — the reader, and where a scope LIVES

The Python client's library half: cache resolution, recall and search rendering, scope and ref
resolution, `doctor`, and the multi-instance routing this file exists for.

🔴 **THIS FILE IS THE EVICTION TARGET FOR ROUTING, AND THAT IS WHY IT EXISTS.** `AGENTS.md` is
loaded by every session in this repository before any work starts — it is measured and ceilinged
by `tests/test_agent_instructions_weight.py` — so a rule that binds only the people editing
`lib/cairn_instances.py`, `cairn`'s routing helpers and `internal/client/instances.go` is paid by
everyone and read by almost nobody. It lives here, where it costs nothing until somebody opens it,
and `AGENTS.md` carries one line pointing at it. **Moved, not deleted: every claim below was in
`AGENTS.md` and is reproduced verbatim.**

## 🔴 THE CLIENT CAN BE POINTED AT SEVERAL INSTANCES, AND AN UNROUTED SCOPE REFUSES

`lib/cairn_instances.py` owns three questions: what is configured, which
instance a scope belongs to, and whether the two agree. The rules that bind
anyone editing that path:

- **The routing table is an INPUT, never a constant here.** A scope→alias map
  lives in the operator's own configuration (`~/.config/subsystem-store/routes.json`
  or `$CAIRN_ROUTES`). This repository is public: shipping a table would publish
  somebody's taxonomy. Aliases only — never a hostname — in anything this repo
  reads, prints or stores.
- **An unregistered scope REFUSES (exit 11) and names the scope — once there is
  more than one instance to choose between.** No fallback to the default
  instance, the first instance, or the one that answers. The failure a default
  recreates is a write landing in a store nobody reads, which is found days
  later by accident if at all.
- 🔴 **"IS THIS HOST ROUTING?" IS TWO QUESTIONS, AND COLLAPSING THEM INTO ONE
  BOOLEAN IS A MEASURED DEFECT.** `Routing.multi_instance` (`len(instances) > 1`)
  decides what is LABELLED — the banner label, the write label, `doctor`'s
  per-instance rows, `ls-entries`' prefix, the `--all-scopes` fan-out and the
  caveat's extra clause all ask it, so they cannot disagree. `Routing.alias_for`
  decides WHERE a scope lives, and it consults the TABLE first, whatever the
  instance count is. Three rows share one configuration — **one instance with a
  table present**, which is what an operator has the moment they write their
  first table — and two of them need opposite answers:

  | table state for this scope | `routes is not None or len>1` | `len>1` alone | correct |
  |---|---|---|---|
  | no table at all | sole instance | sole instance | sole instance |
  | a table, scope ABSENT from it | **REFUSE** | sole instance | sole instance |
  | a table entry naming an UNCONFIGURED alias | REFUSE | **sole instance** | REFUSE |

  Column 2 was shipped: a table's presence switched labelling on, so every
  recall on a one-instance host asserted *"with more than one instance
  configured…"* — false on that host, on every call — and every scope not yet in
  the table refused at exit 11. Column 3 is the obvious fix and it opens a
  **silent misroute**: a write landing in a store nobody decided on. Do not
  re-collapse these; the table above is reproduced at `Routing.alias_for` and
  guarded by `TestOneInstanceWithATablePresent`.
- **One rule, one place: `Routing.check` asks `Routing.alias_for`.** The grader's
  "the table names an alias no instance provides" finding, and its "a write to
  it will REFUSE" finding, are both predictions about what the resolver does —
  so they ask it rather than re-deriving from the same two inputs. That is also
  what keeps the grader honest on a one-instance host, where an unnamed scope
  does *not* refuse.
- **The default instance's cache root does not move.** `cache_root_for` returns
  `DEFAULT_CACHE_ROOT` unchanged for `personal` and a SIBLING directory for every
  other alias. A child directory would look exactly like a SCOPE to every reader
  that enumerates `<root>/<dir>`.
- 🔴 **`STORE_IS_PER_HOST` IS BYTE-MIRRORED INTO TENS OF TRACKED FILES, NOT FOUR
  PLACES.** The kinds are what matter and they are stable —
  `internal/hostid/hostid.go`,
  `internal/report/testdata/reader_fixtures.json` (the bulk of them), **20 of the
  98** `tests/conformance/golden/*.json` (24), `lib/entry_shape.py`,
  `server/README.md` and `tests/test_subsystem_recall.py`. Two are
  regenerate-and-diff gates; `server/README.md` is prose no gate covers. That is
  why the multi-instance caveat is a clause `entry_shape.store_caveat` ADDS at
  render time rather than an edit to the constant. ⚠ **The earliest wording here
  was "a four-place change", wrong by 2× in the direction that makes the edit
  look cheap** — it counted mirror KINDS and read as a count of SITES.

  🔴 **AND THE TOTALS THAT REPLACED IT WENT STALE TOO, WHICH IS WHY NO TOTAL IS
  QUOTED HERE ANY MORE.** This bullet carried "25 files, 92 occurrences"; measured
  later, the tree held **29 and 105**. The prose even named the wrong cause — it
  said the number moves when a golden is regenerated, and the golden figures above
  are still exact to the file; what moved was the **reader fixture**. A count that
  drifts and explains its own drift wrongly is worse than no count, because it
  reads as maintained. Re-derive when you need a total, and do not write the answer
  down:

  ```bash
  python3 - <<'PY'
  import pathlib, subprocess, sys
  sys.path.insert(0, "lib")
  from entry_shape import STORE_IS_PER_HOST          # the needle, never a literal
  files = subprocess.run(["git", "ls-files"], capture_output=True,
                         text=True, check=True).stdout.split()
  hits = {f: pathlib.Path(f).read_text(errors="replace").count(STORE_IS_PER_HOST)
          for f in files if pathlib.Path(f).is_file()}
  hits = {f: n for f, n in hits.items() if n}
  print(len(hits), "files,", sum(hits.values()), "occurrences")
  PY
  ```

  🔴 **THE NEEDLE IS IMPORTED, NOT SPELLED, AND THAT IS THE WHOLE POINT.** The
  recipe this replaces grepped the 14-character fragment `PER-HOST CACHE` out of a
  ~130-character constant. Reword the constant anywhere outside that fragment and
  the count does not move; reword the fragment itself and the count collapses
  toward zero — which reads as *few mirrors, cheap edit*, the exact direction this
  bullet already records being wrong in once. Importing the constant means the
  needle changes when the mirrored text does.

  ⚠ **THE IMPORTED VERSION ALSO PRINTS ZERO WHEN YOU REWORD THE CONSTANT, AND
  THERE ZERO IS THE ANSWER YOU WANT** — measured: rewording one word of
  `STORE_IS_PER_HOST` takes the count from 22/96 to **0/0**, because every mirror
  still holds the old text. That is the tree telling you what the edit costs. The
  fragment grep answered the same edit by drifting quietly from 92 to 105.

  ⚠ **ITS LIMIT, SO NOBODY READS IT AS EXHAUSTIVE:** it is an exact-substring
  count, so it undercounts any site that WRAPS the constant across lines or escapes
  it. That is a narrower blindness than the fragment grep's, not the absence of
  one.
- **A read is labelled only when there is more than one instance; a WRITE names
  its instance always.** "Where did that bullet go" is a question about a durable
  record, asked later, by someone who no longer has the terminal.
- **Two harnesses carry the claims a suite cannot make, and they are RUNNABLE rather than
  written down.** `tests/unchanged_output_capture.py` runs six READ shapes and three WRITE
  shapes against TWO TREES — `origin/main` and this one — in BOTH configurations (no table, and
  a table present on a one-instance host) and diffs the bytes. `tests/routing_mutants.py` breaks
  each routing guard on purpose and requires each mutant to die **to its own test's name**
  (`kills`), with a deliberately-fatal row as its positive control and a
  `PYTHONDONTWRITEBYTECODE=1` + `__pycache__` sweep so a same-length edit cannot be scored
  SURVIVED without having run.
  🔴 **AND IT REFUSES (exit 2) WHEN A RUN COLLECTED ZERO TESTS, which it did not until the
  round-2 audit read the variable that was measuring it.** `run_suites` computed `collected`
  and `main` never looked, so a shell without pytest produced no `FAILED` lines,
  `failing_tests` returned `[]`, and the `positive-control` row — which declares no `kills` —
  was scored `KILLED … by []` at **rc 0**: a fully green battery over nothing. Measured on a
  pytest-less interpreter: `KILLED positive-control by []` / `killed=1` / rc 0 before, and
  `REFUSING TO VOUCH … ran ZERO tests` / rc 2 after. The Go arm counts its own result lines for
  the same reason.
  🔴 **AND THE PARENTHESIS THAT USED TO END THAT SENTENCE IS RETRACTED: "there it reported
  `KILLED-BY-THE-WRONG-TEST`, which blames a guard for a missing runner".** Measured with `go`
  absent, both before and after the count was added: `subprocess.run(["go", …])` raises
  `FileNotFoundError`, so the battery never reaches the count, never reaches a verdict, and
  printed a traceback at **exit 1** — which is also its "a mutant SURVIVED" code, leaving a
  missing toolchain indistinguishable from a finding. The count was the right fix for the
  case it covers (a runner that IS present and reports nothing) and was never the fix for
  this one. It is closed separately: `_run` raises `ToolchainMissing` and `main` refuses at
  **exit 2**, the same "could not vouch" the zero-collected case makes. ⚠ Nothing in
  `.github/` or `flake.nix` runs this battery, so neither state was ever a CI hole.
  ⚠ **THE CAPTURE HAS BEEN BLIND TWICE, IN THE SAME SHAPE, AND BOTH ARE WORTH KNOWING.** Its
  first version ran only the no-table configuration — the one state in which the old predicate
  was `False` by construction — so it measured green over the defect above; re-run it with
  `--base 2301876` — this branch's first commit — to watch it report the 77 diff lines it
  could not see. ⚠ A SHA in prose is rebase-fragile: that reference has been rewritten
  twice by rebases onto a moving `main`, so if it does not resolve, use the commit whose
  subject is "route a scope to an INSTANCE, and refuse when nobody said which". Its second version ran
  only READS, so it could not see that `append`/`put`/`create` print `instance=personal`
  unconditionally — which is the one place a one-instance host's bytes really did move, and
  `README.md` was promising they had not.
  🔴 **SO IT CARRIES ONE POSITIVE CONTROL PER HALF, AND REFUSES (exit 2) IF EITHER FAILS TO
  MOVE.** A control licenses a conclusion about the dimension it was built to test and not a
  neighbouring one: the caveat perturbation moves every read and NO write, because a write
  prints no caveat. The write half's control is a one-character edit to `DEFAULT_ALIAS`, which
  is the value the `instance=` field carries. A write shape that exits non-zero also refuses to
  vouch — a refused write never reaches the `instance=` line, so it would contribute a
  reassuring "identical" while measuring nothing.
- ⚠ **EVERY VERB OF THE GO CLIENT ROUTES, AND THIS BULLET HAS BEEN WRONG TWICE —
  SO IT STATES THE MECHANISM RATHER THAN A STATUS.** It first read "what it does
  **not** do is consult the table on `recall`/`search`/`append`/`put`/`create`",
  which was false of the three write verbs on the commit that introduced it; it
  then read that the read verbs refused a multi-instance host outright, which
  stopped being true when declared difference 8 closed. What is true:
  - `cmd/cairn` declares `routes` and `EXIT_UNROUTED = 11` and implements both —
    the ledgers and the parity gate would otherwise be green over a client that
    had silently lost a verb and a code.
  - `append`, `put` and `create` consult the table and **always print the
    alias**: `writeInstance` calls `Routing.AliasFor`, loads the ROUTED
    instance's credentials, syncs the ROUTED instance's cache (which is what
    `put` derives its `If-Match` from) and prints `instance=<alias>` at one
    instance and at many. A durable record is read later, by someone who no
    longer has the terminal.
  - `recall`/`search` and `validate` consult it too, and `sync`, `ls-entries`
    and `doctor` route NOTHING so they walk EVERY configured instance. ⚠ Not
    because none of them takes a scope — only `doctor` has no `--scope`;
    `sync` and `ls-entries` declare one and pass `scope=""` regardless. All six
    print the alias — in the banner, in the caveat's multi-instance clause, as
    `ls-entries`' `[alias] ` line prefix, and as `doctor`'s `<alias>/<check>`
    **row names**, which is the one of the four a machine consumer of
    `doctor --json` parses — **only when more than one instance is configured**.
    That emptiness is the compatibility guarantee: a one-instance host's bytes
    are unchanged, which the reader fixture measures (regenerating with the
    clause added 225 lines and changed none).
  - 🔴 **AND THAT GUARANTEE IS ABOUT *LABELLING*, NOT ABOUT *ROUTING* — the
    sentence above is echoed at SEVERAL sites, and the number is deliberately
    not written here — `tests/test_narrowing_echo_sites.py` holds the set.** They
    are: here, `internal/client/instances.go`, `tests/parity/README.md`, `cairn`'s
    `_instance_for` docstring, `internal/client/routes.go`'s `readInstance`,
    `internal/client/state.go`'s `BannerNamed`, `tests/test_cairn_instances.py`,
    and `tests/unchanged_output_capture.py`, the harness that measures the claim.
    Every one of them reads wider than it is, and every one carries this
    narrowing. ✅ **THE SET IS NOW ASSERTED, AND THE RE-COUNT RECIPE THAT STOOD
    HERE IS DELETED RATHER THAN CORRECTED** — it matched two literal phrases, so
    it could not see a REWORDED echo, which is what #48's ladder filed against it.
    The ledger fails on GROW *or* SHRINK, and matches over whitespace-NORMALISED
    text because this file states the claim across a line break and the old
    line-based recipe therefore missed the very file it was written in. Do not
    re-add a recipe here: a second, weaker instrument beside the assertion is how
    the count went stale the first time.
    ⚠ **AND THE TOTAL IS DELIBERATELY ABSENT, BECAUSE FOUR DRAFTS OF IT WERE
    WRONG** — "all three", then "FIVE", then six, then seven. The ledger's GROW arm
    caught one of those **in the same PR that added the ledger**; two audits caught
    two more, both sites phrased differently from birth and therefore invisible to
    the first version's spellings. A list a guard asserts cannot drift; a total in
    prose always has.
    The
    scope-taking reads did not consult the table at all before;
    they do now, and `alias_for` row 3 REFUSES at ONE instance as well as at
    many. So exactly one single-instance case moved: a host whose table routes
    the scope to an alias it has no config for — a stale or typo'd entry, the
    case `routes --check` exists to find. Measured over one world with the
    packaged Go client and `routes.json = {"alpha-notes": "nowhere"}`:
    `recall`, `search` and `validate` each answered **exit 0**, off the default
    instance's cache, at `d8b858a`, and each answers **exit 11, refusing**, at
    HEAD. HEAD is the right
    answer — the old one read a store the table said was elsewhere — but it is
    a behaviour change on a one-instance host. 🔴 **This used to add "and it
    belongs in whatever announcement the `packages.default` flip carries", and
    that clause is RETRACTED by measurement**: it is a change to the GO client,
    not to the DEFAULT — the default was the Python oracle, this routing was
    ported FROM the oracle, and both packaged clients now exit 11 on all three
    verbs over the same world, so the flip changes nothing here. `README.md`'s
    announcement **drops** it — this bullet once said it *recorded* it, and the
    draft paragraph was cut: a consumer cannot act on a non-change. The record
    is here, on `internal/client/instances.go`'s header and in
    `tests/parity/README.md`. `sync`, `ls-entries` and
    `doctor` route NOTHING, so none of it reaches them.
  - 🔴 **AND THAT LAST CLAUSE USED TO READ "take no scope, so none of it
    reaches them", WHICH `--help` FALSIFIES FOR TWO OF THE THREE.** `cairn
    sync` and `cairn ls-entries` both declare `--scope`; only `doctor` has
    none. What makes them untouched is that they pass `scope=""` and fan out
    over every instance, not that there is no scope to route. Re-measured over
    the same `{"alpha-notes": "nowhere"}` world: `sync --scope alpha-notes`
    exits 4 and `ls-entries --scope alpha-notes` exits 0 on BOTH clients,
    byte-identical to the unscoped runs — the CONCLUSION stands and only the
    reason moved. The reason is load-bearing: a later change that routed
    `ls-entries --scope` through `alias_for` would falsify the conclusion while
    the old reason still read as covering it.
  - 🔴 `routes` reports rather than routes, and it must keep working before any
    table is right: it is the verb an operator runs *while standing up* a second
    instance.
- 🔴 **`routes --check` MUST THREAD THE ALIAS INTO THE STATE RESOLVER, NOT JUST THE
  CACHE PATH.** `resolve_state`/`ResolveState` FETCHES as well as unpacks, so a
  caller that hands it instance `N`'s cache root while it loads the DEFAULT
  instance's credentials fetches `personal` N times and unpacks it over every
  other instance's cache — the exact damage the sibling-directory layout exists to
  prevent, arriving through the code path instead of through `--cache`. The
  grader then reads one store's scope set and reports the others' scopes as
  missing. Measured on a two-instance host before the fix: `cairn[secondary]`
  banner naming `personal`'s URL, `secondary`'s cache holding `personal`'s
  scopes, and a false "exists on no configured instance" at exit 11.
- 🔴 **DIRECTION TWO OF `Routing.check` IS A NOTE, NOT A VERDICT.** It subtracts the
  cache's DIRECTORY LISTING from the table's keys, and a snapshot ships entry
  FILES — so a scope holding no entries is missing from that set whether it is
  stale, pre-registered before its first write, or pruned back to empty. Grading
  it at exit 11 made the check refuse exactly the two-way-pinned registry it
  exists to be, and the remedy its wording implied — delete the line — makes the
  next write to that scope REFUSE. The discriminator exists and is simply not in
  the snapshot (the server answers `scope-empty` vs `scope-absent` on
  `GET /api/v1/recall/<scope>`); promoting it back is gated on probing the ROUTED
  instance per table entry, identically in both clients, with a parity row.
- 🔴 **NAME RULES ARE SEPARATE FROM TYPE RULES IN `instances/`.** A file whose name
  begins with **`.#`** is skipped rather than refused. Emacs' lock file for
  `secondary.env` is `.#secondary.env` — it ends in `.env`, its stem is not a
  usable alias, and it is a dangling symlink — so it reached the hard
  `RoutingConfigError` and took **every** verb on that host to exit 11 while one
  buffer was open. The refusal's stated intent ("a file the operator wrote and
  would otherwise get no message about") is untouched: any other file that
  cannot be an alias is still an error.
  🔴 **`.#`, AND THE FIRST CUT WAS `.` — WHICH WAS WIDER THAN ITS OWN STATED
  RATIONALE AND OPENED THE HOLE THE REFUSAL EXISTS FOR.** `instances/.env` is a
  dotted name, so it was skipped; its stem is the EMPTY string, so it is exactly
  the file the refusal would otherwise name. Measured with a complete, valid
  `instances/.env` on both clients: **before, `instances: personal` at exit 0
  with no message at all; after, exit 11 naming the file.** Parity held in both
  directions, so no gate could see it — the two clients agreed on being wrong.
  `.#` is the only in-suffix name a TOOL writes: a vim swapfile is
  `.secondary.env.swp` and an Emacs autosave is `#secondary.env#`, and both fail
  the `.env` suffix test before the name test is reached. So every other dotted
  name is one a human could have chosen, and stays an error. ⚠ **The `internal/snapshot`
  and `doctor` scanners skip EVERY dotfile, and that asymmetry is correct rather
  than drift**: there is no operator-wrote-this refusal on those paths to leave a
  dotted name silently unread, so a wide skip costs nothing there and costs the
  whole refusal here. Do not "unify" them. ⚠ Both arms are
  pinned in both languages — the skip arm alone passes while the hole is open —
  by `test_an_EDITOR_LOCK_FILE_does_not_take_every_verb_to_exit_11` /
  `test_a_DOTTED_file_that_is_not_an_editor_lock_is_STILL_an_ERROR` and
  `TestAnEDITORLockFileDoesNotTakeEveryVerbToExit11` /
  `TestADottedFileThatIsNotAnEditorLockIsStillAnError`, with a mutant per
  direction per client in `tests/routing_mutants.py`.

