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
- 🔴 **`STORE_IS_PER_HOST` IS BYTE-MIRRORED INTO 25 TRACKED FILES, NOT FOUR
  PLACES.** Enumerated over `git ls-files` at `38b358d`: **25 files, 92
  occurrences** — `internal/hostid/hostid.go`,
  `internal/report/testdata/reader_fixtures.json` (64 of them), **20 of the 98**
  `tests/conformance/golden/*.json` (24), `lib/entry_shape.py`,
  `server/README.md` and `tests/test_subsystem_recall.py`. Two are
  regenerate-and-diff gates; `server/README.md` is prose no gate covers. That is
  why the multi-instance caveat is a clause `entry_shape.store_caveat` ADDS at
  render time rather than an edit to the constant. ⚠ **The earlier wording here
  was "a four-place change", wrong by 2× in the direction that makes the edit
  look cheap** — it counted mirror KINDS and read as a count of SITES. Re-derive
  rather than quoting this number; it moves whenever a golden is regenerated:

  ```bash
  git ls-files | xargs grep -c 'PER-HOST CACHE' 2>/dev/null | grep -v ':0$' | wc -l
  ```
- **A read is labelled only when there is more than one instance; a WRITE names
  its instance always.** "Where did that bullet go" is a question about a durable
  record, asked later, by someone who no longer has the terminal.
- **Two harnesses carry the claims a suite cannot make, and they are RUNNABLE rather than
  written down.** `tests/unchanged_output_capture.py` runs six read shapes against TWO TREES —
  `origin/main` and this one — in BOTH configurations (no table, and a table present on a
  one-instance host) and diffs the bytes, with a one-character perturbation of the caveat as its
  positive control; it exits **2** if that control does not move. `tests/routing_mutants.py`
  breaks each routing guard on purpose and requires each mutant to die **to its own test's
  name** (`kills`), with a deliberately-fatal row as its positive control and a `PYTHONDONTWRITE
  BYTECODE=1` + `__pycache__` sweep so a same-length edit cannot be scored SURVIVED without
  having run. ⚠ The capture's first version ran only the no-table configuration — the one state
  in which the old predicate was `False` by construction — so it measured green over the defect
  above. Re-run it with `--base 58b4971` to watch it report the 77 diff lines it could not see.
- ⚠ **THE GO CLIENT CARRIES THE VERB AND THE CODE, NOT THE ROUTING.** `cmd/cairn`
  declares `routes` and `EXIT_UNROUTED = 11` and implements both — the ledgers
  and the parity gate would otherwise be green over a client that had silently
  lost a verb and a code. What it does **not** do is consult the table on
  `recall`/`search`/`append`/`put`/`create`: that needs the caveat's
  multi-instance clause inside `internal/report`, which is the renderer the POD
  shares and which has no instance context at all. Declared difference 8 in
  `tests/parity/README.md` carries the closing condition. Until it closes, a
  multi-instance host must run the Python client.

