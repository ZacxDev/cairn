# The P2 parity gate

```bash
python3 tests/parity/harness.py              # the gate
python3 tests/parity/harness.py --self-test  # prove the differ can go RED
python3 tests/parity/harness.py --break-pod  # prove the PRE-FLIGHT refuses to vouch (rc 2)
python3 tests/parity/harness.py --only recall-digest --keep   # one row, world kept
```

🔴 **A GREEN RUN IS NOT EVIDENCE UNTIL BOTH CONTROLS HAVE BEEN WATCHED TO WORK, and this harness
is the reason that rule exists.** Its first full run reported **72 PASS, 0 FAIL** while every
single request was refused `401 status=no-client-ip`, no cache was ever written, and every report
rendered `store-unreachable` — because `SUBSYSTEM_STORE_TRUSTED_PROXIES` had been copied from the
conformance runner, where `127.0.0.1/32` is correct, into a harness whose clients connect
*directly*. Two clients failing identically compare equal. ⚠ **It was found by reading the pod's
AUDIT LOG, not by the green** — nothing in the run's own output disagreed with a working gate,
which is the whole reason the controls below had to be built rather than reasoned about. Three now
stand against it, and they are three different claims (the third is itself four mutants):

| control | what it proves | how it reads |
|---|---|---|
| `PREFLIGHT status=… declared-entries=…` | the POD answers a snapshot for this token, non-empty | exit **2** ("could not vouch"), not 1, if it does not |
| `CONTENT-FLOOR live-banner=… rendered-digest=…` | the CLIENTS got as far as a live fetch and a rendered digest | exit 2 if either is false |
| `--self-test` → `SELF-TEST sabotaged=4 caught=4` | the differ can go RED on stdout, on stderr, on the exit code **and** on `exit+stdout` | exit 2 if any sabotaged row is not reported |

The four sabotage rows are separate on purpose: `compare="exit"` rows are **structurally blind** to
stdout and stderr, so one control over "something went red" would vouch for a differ that had lost
most of its comparisons. 🔴 **And one of them is a `replace`, not an `append`** — both clients
handle `--help` before anything else, so no extra argument changes either answer and an
append-only mechanism had **no control at all** over the `exit+stdout` mode's two assertions.

## What is compared

Both clients run against **one** pod, **one** store and **one** cache root, with identical argv
and identical environment, and the harness diffs **stdout, stderr and the exit code**. Nothing is
recorded: the oracle runs live, so the two cannot agree with a snapshot of an older Python client
and have it called parity.

The store is restored from a pristine copy **before every client run**, mtimes included — three
write verbs change it, the two clients run in sequence, and without that `create-ok` had the
oracle create the entry and the Go client legitimately answer `already-exists`. A row that needs
an earlier write declares `setup` instead of relying on row order.

`cache-mtime-parity` is a separate, structural claim: each client syncs into its **own** root and
the trees are compared as the *reader* sees them (`float(sec) + 1e-9*nsec`, which is what
`report.pyMtime` computes). The rendered order is what *caught* a dropped mtime; this is what
*pins* it, because a rendered order can agree by accident of three files landing in the right
sequence while every timestamp is wrong.

`orphan-reap-parity` is the second structural claim, and it exists because the thing it measures
**is not printed anywhere**. `ReapOrphans` / `_reap_orphans` runs at the top of every snapshot
install, removes stale `<cache>.new-*` / `<cache>.old-*` trees, and returns a count no caller
renders — so a client that reaped NOTHING compared equal, on every row, to one that reaped
everything. Each client is given its own root with two year-2000 staging trees seeded beside it
and the filesystem is read afterwards. 🔴 **Its own positive control is read rather than assumed:
if the ORACLE did not reap both, the check FAILS rather than reporting agreement** — a fixture
that never reached the mechanism would otherwise make "both clients agree" a fact about the
fixture. The metacharacter has to sit in the cache's PARENT for it to be about the class at all;
see `WORLD_METACHARACTER_SUFFIX`.

`nonregular-path-parity` is the third structural claim, and it exists because **the world cannot
represent the input**. `world.build_store` writes files that the pod tars and each client unpacks;
neither a fifo nor a device node survives that pipe — the snapshot walker refuses them,
`install_snapshot` replaces the cache root wholesale, and `tarfile`'s `filter="data"` would drop
the member even if one arrived. So no `Case` row can present a non-regular path, and a whole
hazard class was invisible to a gate that otherwise compares every verb: `validate`'s two
advisories opened every candidate unconditionally while the loader beside them had refused
`other` / `link-to-other` before `open()` since a fifo was measured wedging a request thread for
25 s. Both clients **wedged forever** on a cache holding one, where the commit before the
advisories exited 5. The check syncs a real cache, seeds a fifo named `*.md` into it, and runs
both clients' `validate --no-sync` under a 30 s timeout. 🔴 **The timeout is part of the
assertion, and the positive control is read rather than assumed:** the fifo must land as a
MALFORMED entry at exit 5 — the loader's own refusal — or the row FAILS, because two clients that
both skipped the scope, or both crashed the same way, compare equal. ⚠ A character device is the
same table arm and is deliberately NOT seeded here: its failure is an OOM whose blast radius is
the harness's own box, where a fifo's is a hang a timeout bounds exactly. `link-to-other` is
covered by unit tests in both clients; what this check adds is the CROSS-CLIENT claim.

## What this gate found — twelve divergences in nine findings

🔴 **Relocated here from `AGENTS.md`, which is loaded into every session in this repository
and was 41.6 KB when this moved.** None of the below is decision input before acting; it is
the evidence that the gate measures rather than reassures, and it costs nothing until this
file is opened. `AGENTS.md` keeps the pointer and the rulings.

**Three the gate found that no existing test or golden could see**, each a class rather than
a typo:

1. **`create` answers `201`.** `urllib`'s `HTTPErrorProcessor` raises only OUTSIDE 200–299, so
   a `resp.StatusCode != 200` test reported a SUCCESSFUL create as `unrecognised HTTP 201 …
   treating the write as NOT LANDED` at exit 6 — whose documented remedy is to change a request
   that already landed. No conformance golden the client replays carries that status, and every
   write test written against `append` passes on a 200.
2. **The installer dropped every member's MTIME.** The reader orders its index by entry mtime,
   so the cache was ordered by TAR ORDER: a different listing with a different featured entry.
   Precisely the silent reordering this phase exists to prevent.
3. **A `CAIRN_CACHE_ROOT` override the port invented.** `doctor` resolves the READER's store
   through that function and NOT through `--cache`, so the two clients reported different
   `reader-resolution` roots on every doctor row. Deleted rather than mirrored into Python: a
   second mechanism reaching one value is the shape that leaves the first silently dead.

**A fourth was found by asking what the gate does NOT send: `--help`.** Every other row asserts
something a caller asked the tool to DO; `--help` is how a human finds out what it can do, and
there was no row for it. The Go client exited **2 with an empty stdout** for `--help`, `-h`,
`recall --help` and `doctor --help` where the oracle exits **0** with its help text — on the
single most common invocation there is. Four rows now cover it, under the `exit+stdout`
comparison mode.

**Following that one step further found four more, all in the same dangerous direction — the
Go client SUCCEEDING where the oracle refuses**: `append --text -h` exited 0 printing help
(oracle: 2), so a caller scripting `--text "$MSG"` whose message began with `-` would have read
exit 0 as "the bullet landed"; `recall --limit -h` the same; `recall --scope -weird` exited 3
having taken `-weird` as a scope; and `--help` after an unknown flag needs the refusal DEFERRED
in two separate loops, so a fix applied to one leaves the other wrong. Those four are one
finding with four rows — the rulings and the rows are in **Argument-shape rows** below.

**A sixth came out of the Go unit battery**: a truncated gzip stream and an HTML error page
surface as the SAME `io.ErrUnexpectedEOF` out of `tar.Next`, so classifying on the error VALUE
called `<html>nope</html>` a truncated tar where the oracle says `did not return an archive`.
`gzipLayerError` records WHICH LAYER failed at the point it is known, and `validateGzipLayer`
streams the compressed body to `io.Discard` **under a limit** — decompressing into memory to
inspect it would make a decompression bomb a MEMORY bomb before either ceiling is consulted,
because the ceilings read headers a truncated stream never reaches.

**A seventh came out of asking what region the gate does not COVER rather than what it does not
send, and it is the sharpest of the seven** — the two `<MULTICFG>` rows were both
`routes --check`, i.e. the GRADER, so the routed **WRITE** path this phase exists to ship had
**zero cross-client byte comparison at a non-default alias**. A live divergence sat in that gap.
On a two-instance host with `routes.json = {"myscope":"secondary"}` and
`instances/secondary.env` present but missing `SUBSYSTEM_STORE_TOKEN`,
`cairn put --scope myscope --ref x --file f` gave:

```
oracle  rc 7  🔴 cairn: refusing to PUT — could not refresh the cache … (config incomplete: …). … Pass --if-match explicitly if you already hold it.
Go      rc 7  🔴 cairn: the write did NOT happen — config incomplete: … Re-run when the store is reachable.
```

**The exit codes agree and the bytes do not**, which is why no gate could see it: the oracle's
`cmd_put` loads the credentials *after* `resolve_state`, so a missing one surfaces as a non-live
STATE and `put` refuses in its own words; the port loaded them eagerly in `writeInstance`, so the
error escaped to `cli.go`'s write-unreachable arm and refused in ITS words. ⚠ **Not "Go is
eager"** — `append` is byte-identical on the same input, because the oracle loads them eagerly
*there* too. The divergence was one verb wide. `writeRoute` is the split that closes it (the route
without the credentials, for the one verb that needs them late), pinned by
`TestAPutLoadsTheROUTEDCredentialsLAZILY`, whose killing mutant restores the eager load and is
invisible to any exit-code comparison. The region itself is now covered by
`put-routed-to-a-NON-DEFAULT-instance`, a full stdout/stderr/exit row over a scope that exists on
the SECOND pod only — so a client that reached the default instance for the alias, the
credentials, the cache or the `If-Match` hits a pod whose token does not carry that scope at all.
⚠ **THE DIVERGENCE PRE-DATES THE ROUND-2 AUDIT IN A DIFFERENT SHAPE, MEASURED AT THE COMMIT
BEFORE THE `routes --check` FIX** — and the earlier shape is the more interesting one, because
the port printed the ORACLE's sentence and still said something else:

```
oracle       🔴 … could not refresh the cache … (config incomplete: SUBSYSTEM_STORE_TOKEN not set (looked in …/instances/secondary.env …)). …
go @ada0157  🔴 … could not refresh the cache … (http://127.0.0.1:1 unreachable: dial tcp …: connect: connection refused). …
```

That is the *default* instance's URL inside the *routed* instance's refusal — the misroute, not
the eager load — so the two fixes reshaped one region rather than one being the cause of the
other. **What this round closed is the byte difference and the coverage hole**; the region was
never byte-identical at any commit on this branch until now.

**An eighth arrived with the `SUBSYSTEM_STORE_*` → `CAIRN_*` rename, and it is the one that
best justifies the gate's existence** — because every other instrument in the tree stayed green
while it was live. The rename put a resolver behind each environment read. `internal/client` had
**two** spellings of *"where is the config file"*: `transport.go`'s `DefaultConfigPath` and
`instances.go`'s `ConfigPath`. They agreed — both read the variable, both fell back to
`~/.config/subsystem-store/env` — until the alias ledger landed in ONE of them, at which point
the credential loader honoured `$SUBSYSTEM_STORE_CONFIG` and the ROUTING layer did not:

```
oracle  🔴 cairn: … SUBSYSTEM_STORE_TOKEN in <WORLD>/multi-config/instances/secondary.env is a deprecated alias …   (two instances, routed)
go      🔴 cairn: REFUSING — the routing table … routes scope `gamma-notes` to instance `secondary`, which is not configured on this host (configured: personal)
```

Seven rows red, one root cause — `routes --check` twice, three routed READS, a routed `put`, and
`recall-routed-to-the-DEFAULT-instance-is-still-labelled`, which lost its `[personal]` label
because the instance COUNT came back as one. 🔴 **NONE OF IT LOOKED LIKE AN ENVIRONMENT BUG**, and
that is the transferable part: the visible symptom was a routing refusal and a missing label, so a
reader debugging it from the message would have gone to `routes.go`. `go vet`, `go test ./...`,
the conformance corpus and `tests/dualrun/` were all green throughout.

The instructive half is **why the second copy survived the change at all**: `instances.go`
resolves through an injectable `func(string) string` rather than a `map[string]string`, and the
first cut of `internal/envalias` offered only the map form — so the call site that could not use
the resolver silently kept a plain single-name lookup. The remedy is the one the rules name, not
a second patch: `envalias.ValueFrom` is now where the precedence rule lives and every other entry
point delegates to it, `envalias.Resolving` wraps a getter for a call site that passes one
around, and `DefaultConfigPath` is one line delegating to `ConfigPath`. ⚠ **The guard covers BOTH
arms of `lookup` deliberately** — every unit test in the package injects a getter and no real run
does, so wrapping only the process-environment arm would have left the guard asserting the case
that was never broken.

## 🔴 An AUTHORISED exception to "do not change the oracle" — `cmd_validate`'s count

**Decision (operator, on PR #48): `cairn`'s `cmd_validate` globs the ROUTED instance's cache
root rather than the default one.** The standing P1 rule is that the oracle is the golden
source and is never edited to make the port agree; the standing exception is a defect a
contract cannot contain, granted by the operator, per site, in writing. The precedent is
`seeded=UNREADABLE` on both servers — authorised "because a contract cannot include 'sometimes
truncate the response mid-stream'". **Here: a contract cannot include "sometimes print a
NEGATIVE count".**

The one-token change is `(args.cache / scope)` → `(cache / scope)`. `_instance_for`,
`cache.iterdir()` and `load_index(cache, …)` in the same function already read the routed
instance; that single expression read the DEFAULT one, so on a host where `--scope` routes
elsewhere the numerator and denominator came from two different stores. Measured oracle against
the Go client — row 1 is the parity world's `gamma-notes` as it stands, row 2 the same world with
a malformed file added to the ROUTED store:

| routed store | oracle, before | Go client | exit |
|---|---|---|---|
| one readable entry | `gamma-notes: 0 of 0 entry file(s) parse, 0 malformed` | `1 of 1 … 0 malformed` | 0 both |
| one readable, one malformed | `gamma-notes: -1 of 0 entry file(s) parse, 1 malformed` | `1 of 2 … 1 malformed` | 5 both |

⚠ **THE EXCEPTION IS THAT EXPRESSION AND NOTHING ELSE.** It does not license editing the
oracle anywhere else in this port, and the site carries the same statement in a comment.

🔴 **WHAT NOW COVERS IT, AND WHY AN EXIT-ONLY ROW WOULD NOT.** `validate-routed-to-a-NON-DEFAULT-instance`
compares stdout, stderr **and** the exit code — both clients exit 0 on the clean store and 5 on
the malformed one, so the code is identical in both rows above and the count lives on stdout or
nowhere. Matrix: the row is **RED at `d57f46b`** (`-cairn: gamma-notes: 0 of 0 …` /
`+cairn: gamma-notes: 1 of 1 …`) and **GREEN with the fix**.

⚠ **NO GOLDEN MOVED, AND THAT WAS CHECKED RATHER THAN ASSUMED.** `cmd_validate` is CLIENT code:
the conformance corpus drives `server/server.py`, and `internal/report/testdata/reader_fixtures.json`
is generated by `tests/reader_fixtures.py` from `lib/`'s renderer, neither of which this function
is on. Proved by regenerating and diffing rather than by reasoning:
`python3 tests/reader_fixtures.py generate` left `reader_fixtures.json` byte-identical (same
md5, empty `git status`), and `python3 tests/conformance/suite.py run` stayed at
`requests=99 assertions=433 failures=0 skipped=0`.

⚠ **AND THE GUARD THE CHANGE COULD HAVE EMPTIED IS NAMED:**
`tests/test_cairn_cli.py::test_validate_REPORTS_WHAT_IT_CHECKED_and_the_count_MOVES`, which
pins the count and pins that it MOVES. It passes an explicit `--cache` on a one-instance host,
where `args.cache` and the routed `cache` are the *same object* — so the change is a no-op
there and the guard is not emptied. That is also exactly why it never saw the defect: the
dimension it fixes is the one the defect lives on.

## Finding 9 — the shared cache root was wiped ONCE PER CASE, not once per client

🔴 **THE SAME DEFECT `once()` ALREADY CARRIED A COMMENT ABOUT, ONE ROOT OVER, AND ONLY THE
CONDITIONAL SYNC MADE IT OBSERVABLE.** `case.wipe_cache` removed `<work>/cache` at the top of
the case loop — **outside** both `once()` calls — so the ORACLE ran against an empty cache and
the Go client ran against the one the oracle had just installed. That is verbatim the shape
the derived-root wipe was fixed for, and it was harmless for the same accidental reason the
comment there names: a sync was UNCONDITIONAL, so `install_snapshot` replaced the root
wholesale and the starting state could not reach the output.

`GET /api/v1/snapshot` gaining an `ETag` removed the accident. The second client to run now
presents the validator the first one stored and is answered `304`, so four rows compared a
client that DOWNLOADED against a client that was told nothing had changed:

```
FAIL sync-live
    -cairn: live — fetched from http://…:39315 just now — 7 entries, snapshot seeded=…
    +cairn: live — already current at http://…:39315 — not modified, snapshot seeded=…
```

`sync-live`, `ls-entries`, `recall-digest` and `validate-all-scopes` all failed that way, and
`cache-mtime-parity` failed separately on `.sync-etag` — a file each client writes at sync
time, so its mtime is a clock reading, which is exactly why `.sync-stamp` was already
excluded. **Neither failure was a difference between the two clients.** Both were the harness
putting them in different situations and then comparing the answers.

The fix is structural rather than per-row: the wipe moved **inside** `once()`, which is what
`wipe_cache` always meant, and `.sync-etag` joined `.sync-stamp` in the mtime exclusion — its
CONTENT is not excluded, because the two clients store the same bytes and a disagreement
there would surface as a differing file set or as a parity failure on the next sync.

⚠ **AND ONE ROW'S SUBJECT CHANGED, WHICH IS RECORDED RATHER THAN QUIETLY ABSORBED.**
`sync-again`'s `why` said it "exercises the retire-and-rename swap rather than the create
path". A second sync over an UNCHANGED store no longer downloads anything, so no swap
happens — on either client, which is what the row still measures byte for byte. The retire
branch is covered where it can be made deterministic instead:
`internal/client`'s `TestTheValidatorIsInstalledWithTheContentItDescribes` installs into one
cache twice, and `tests/test_cairn_cli.py`'s `test_a_second_sync_after_a_CHANGE_downloads_again`
drives the oracle through it end to end.

## Declared differences — the residuals, named rather than normalised away

⚠ **ROW 8 IS GONE AND ITS NUMBER IS NOT REUSED.** It declared that the Go client routed WRITES and refused READS at exit 11; the read verbs route now, so the difference it declared does not exist and the row was deleted with the guard that asserted it (`RefuseUnportedMultiInstance`). What replaced it is coverage rather than prose: `recall-routed-to-a-NON-DEFAULT-instance`, `recall-routed-to-the-DEFAULT-instance-is-still-labelled` and `ls-entries-walks-EVERY-instance` compare stdout, stderr and the exit code on a two-instance host, and the reader fixture carries four instance-bearing rows. **The numbering is left with a hole on purpose** — this file's rows are referred to by number from `AGENTS.md`, `lib/README.md`, `flake.nix` and the handoff docs, and renumbering would silently re-point every one of those at a different difference.

🔴 **AND CLOSING IT MOVED ONE SINGLE-INSTANCE CASE, WHICH THE "a one-instance host's bytes are unchanged" HEADLINE DOES NOT SAY.** That sentence is true of **labelling** and was read as covering **routing**. The scope-taking reads (`recall`/`search`/`validate`) did not consult the table at all before; they do now, and `AliasFor` **row 3** — a table routing a scope to an alias this host has no config for, i.e. a stale or typo'd entry, the case `routes --check` exists to find — refuses at ONE instance as well as at many. Measured over one world with the packaged Go client and `routes.json = {"alpha-notes": "nowhere"}`: `recall`, `search` and `validate` each answered **exit 0 at `d8b858a`**, off the default instance's cache, and each answers **exit 11, refusing, at HEAD**. HEAD is the correct answer — the old one read a store the table said was elsewhere — but it is a behaviour change on a one-instance host, so this said **it belongs in whatever announcement the `packages.default` flip carries**. 🔴 **THAT CLAUSE IS RETRACTED, AND THE RETRACTION IS A MEASUREMENT.** It is a change to the **GO client**, not to the **DEFAULT**: the default was the Python oracle, and the port took this behaviour *from* the oracle, so the oracle already refused. Re-measured on the flip's branch over one synthetic world with `{"alpha-notes": "nowhere"}`, both packaged clients answer **exit 11 on all three verbs** — `recall`, `search`, `validate`, six runs, six 11s. A consumer moving from the old default to the new one therefore sees **no change here**, which is the opposite of what this sentence told the flip's author to announce. 🔴 **AND `README.md`'s ANNOUNCEMENT DROPS IT — THE DELETION IS A RULING, NOT AN OVERSIGHT, AND THIS SENTENCE ONCE SAID THE OPPOSITE.** It used to read "`README.md`'s announcement records it as a non-change rather than dropping it". A draft of that section did, in its longest paragraph; the paragraph was cut, because a consumer-facing *"what changed for you"* section cannot hand a consumer an action for a change that measurably is not one. **The record is not lost, and dropping it from the announcement is not retracting it** — it is this paragraph, `internal/client/instances.go`'s header and `lib/README.md`'s bullet, which is where a contributor who might re-derive the wrong expectation reads. Had the paragraph been deleted without this sentence moving, the tree would have carried a claim the tree itself falsifies. `sync`, `ls-entries` and `doctor` route NOTHING and are untouched. The same clause is now on `internal/client/instances.go`'s header and `lib/README.md`'s bullet; `readInstance`'s own comment already said `AliasFor` "may already have refused".

🔴 **AND THAT LAST CLAUSE USED TO GIVE A REASON `--help` FALSIFIES: "take no scope".** `cairn sync` and `cairn ls-entries` both DECLARE `--scope` — only `doctor` has none. What makes the three untouched is that they pass `scope=""` and fan out over every configured instance (`Sync`, `LsEntries`, and `cmd_sync`'s measured reason the cache may never be narrowed), not that there is no scope to route. Re-measured over the same `{"alpha-notes": "nowhere"}` world at this head: `sync --scope alpha-notes` exits **4** and `ls-entries --scope alpha-notes` exits **0** on BOTH clients, byte-identical to the unscoped runs — so the CONCLUSION is unchanged and only its reason moved. 🔴 The reason is load-bearing rather than pedantic: this paragraph is what the `packages.default` flip's announcement is written from, and a later change that routed `ls-entries --scope` through `AliasFor` would falsify the conclusion while the old reason still read as covering it. The correction was applied at all five sites that carried the clause (`cairn`, `internal/client/instances.go`, `internal/client/verbs.go`, `lib/README.md`, here).

🔴 **A CLOSING CONDITION IN THIS TABLE THAT IS A *COMMAND* MUST SAY WHAT ITS ZERO-SELECTION RUN
LOOKS LIKE, BECAUSE `$?` CANNOT TELL YOU.** Measured at `38f2b1f`: `go test <pkg> -run <NameThatMatchesNothing> -count=1 -v`
prints `testing: warning: no tests to run`, `PASS`, `ok … [no tests to run]` and **exits 0**, so an
operator checking the exit code alone reads an unwritten test as a met condition. (`python3 -m pytest … -k <filterThatMatchesNothing>`
does not share it — it exits **5**.) So every command-shaped condition below states the
zero-selection state explicitly and names a check that is not the exit code. Rows whose condition
is an EVENT rather than a command — row 7's *"the day the oracle is deleted"* — have nothing to
state here; rows 1–6 declare no closing condition at all.

| # | difference | why it is not closed |
|---|---|---|
| 1 | **argparse's usage and help text.** Unknown flag, missing required flag, unknown subcommand, non-integer `--limit`, `doctor --scope`, bare invocation — and `--help` / `-h` / `<verb> --help`: the oracle prints argparse's `usage:` block and its wording; the Go client prints its own. | Reproducing argparse's layout, its prefix abbreviation (`--sc` → `--scope`) and its exact phrasing in Go is a second implementation of a library nobody reads twice. **The exit code matches on all of them** — 2 for the refusals, **0 for the help rows** — and that is the half a caller branches on. Refusal rows are `compare="exit"`; help rows are `compare="exit+stdout"`, which also asserts BOTH sides put something on stdout, because a client that printed nothing and exited 0 would pass an exit-only row while telling the reader nothing. 🔴 Before those four rows existed the Go client exited **2 with an empty stdout** on all of them — on the single most common invocation there is. |
| 2 | **`urllib` vs `net/http` failure text.** `<host> unreachable: <reason>` has a `urllib` tail on one side and a Go tail on the other; the oracle distinguishes a DNS/connect failure (`URLError.reason`) from a socket timeout (a bare `OSError`) where Go returns one `*url.Error` for both. | The host and the word `unreachable` — what a reader greps and what a human needs — are identical. Matching the tails would mean transcribing two libraries' error strings, which change with their versions. Rows that reach these are `compare="exit"`. |
| 3 | **Sub-microsecond mtime.** The oracle's `tarfile` carries a PAX mtime as a Python **float** and `os.utime` writes it back, losing precision Go's exact decimal parse keeps: measured `…236263` (oracle) vs `…236300` (Go) on the seed stamp, 37 ns. | **Measured unable to matter.** Both collapse to the same `float64`, which is the value the index order is decided on: one ULP is 238 ns at the current epoch — measured at four magnitudes (year 2000 → 119 ns, now → 238, 2038 → 238, 2100 → 477). `cache-mtime-parity` asserts the doubles are identical **and** that the raw delta stays under that ULP, so a future change that widened it fails here rather than reordering an index. |
| 4 | **A reader error's exit route — NARROWED THREE TIMES, because the unreadable-ENTRY half (#111), the unreadable-SCOPE-DIRECTORY half (#119) and now the searchable-but-unreadable-cache-ROOT half are all CLOSED AND MEASURED.** 🔴 **THE ROW HAS NOW BEEN WRONG ABOUT ITS OWN SIZE TWICE, AND THE SECOND TIME THE ERROR WAS IN HOW IT ENUMERATED — so what follows enumerates by (DEPTH × MODE), never by depth alone, because "depth" is the axis that hid a live case both times.** First it said the cache root was all that remained while the scope DIRECTORY was still open. Then — this is the second — it said *"Three mode-000 depths exist under a cache; ONE is still a divergence … 1 vs 3 in all four. (END OF SET.)"*, and that was false: the cache-**ROOT** depth is **two** cases separated by the `x` bit, and only one of them is `resolve_state`'s. Re-measured on both clients at `8ddbb6f`, `--no-sync`, one cache holding one readable scope, `chmod` on the named path: ▸ **cache ROOT without `x`** (`0000`, `0444`) — the oracle raises an uncaught `PermissionError` out of `resolve_state`'s `(cache / SYNC_STAMP).exists()` at exit **1** (traceback) while Go's `ResolveState` turns it into `store-unreachable, no cache` at exit **3**. **STILL A DIVERGENCE**, and wider than this row used to say: 1 vs 3 for `recall`, `search` **and** `validate` — six runs over two modes, and `search` was never named here. ▸ **cache ROOT with `x` and without `r`** (`0111`) — the stamp `stat` **succeeds**, so the state banner prints and execution reaches `cmd_validate`'s `held` / `Validate`'s own root walk. `recall` and `search` were ALREADY byte-identical at **3** here, reaching that walk through `load_store`/`LoadStore`; `validate` alone diverged — **1** with a traceback on the oracle against **3** carrying Go's raw `open <cache>: permission denied` instead of the reader's sentence. **NOW CLOSED**, by the third wrap (`scope_dirs_or_unreadable` / `ScopeDirsOrUnreadable`), gated by `validate-unreadable-cache-root` with a FIFTH content-floor sentinel. ▸ **scope DIRECTORY at `000`** and ▸ **entry FILE at `000`** — each answers **3** with the identical `index entry unreadable: under <root> (PermissionError: [Errno 13] …)` sentence on both clients. (END OF SET, enumerated on the axis that was wrong before.) 🔴 **WHAT THE OLD WORDING WOULD HAVE COST, STATED PLAINLY BECAUSE IT IS THE WHOLE LESSON:** this row named the remedy as *"teaching `resolve_state` that an unreadable stamp is 'no cache'"*. That remedy is right for the no-`x` case and does not touch `held` — so **executing it and watching the mode-000 row go green would have left the `0111` case live**, a declaration licensing its own blind spot. "1 vs 3 in all four" could not have found it either: at `0111` two of the three verbs were never divergent, so the row's own numbers describe a world it had not measured. | **The entry half is no longer declared — it is gated.** An unreadable `*.md` inside a cached scope exited 1 with a TRACEBACK on the oracle and 3 on the Go client, and the two clients' `index entry unreadable` sentences differed in their OS-error tail besides. Both are fixed together: the oracle's `main()` grew the reader-error rung `internal/client/cli.go` always had, `cmd_validate`/`Validate` now read through `load_store`/`LoadStore` (the function that owns the fail-closed wrap) instead of `load_index`/`LoadIndex`, and Go's two `index entry unreadable` sentences render their cause through `store.PyOSError`. `validate-unreadable-entry` and `recall-unreadable-entry` compare stdout, stderr AND the code over a cached entry this harness chmods to `000` for the measured run — the mode CANNOT be committed, since git does not preserve it and CI would restore the file readable. 🔴 **AND SO IS THE SCOPE-DIRECTORY HALF, WHICH THIS ROW DID NOT KNOW IT WAS CARRYING (#119). For one round this row said the only remaining case was a mode-000 cache ROOT, and that was false: a mode-000 scope DIRECTORY sat between the two, reachable by every read, and it was the WORST of the three.** `lib/subsystem_resolver.entry_files_in` walked the scope directory with `pathlib.Path.glob`, which SUPPRESSES the `OSError` its own scan raises and yields nothing, where Go's `mdNamesIn` uses `os.ReadDir` and propagates it. Measured on both clients with a readable control either side: `recall` and `validate` each answered **0** on the oracle with `status=scope-empty` and *"NOTHING RECORDED YET — `<scope>/` exists but holds no entries. … Not an error."* against **3** with the named sentence on the Go client — a false claim of ABSENCE at a SUCCESS code, which is strictly worse than the entry half's 1-with-a-traceback. The fix is one walk (`iterdir()`), reusing the `EntryUnreadableError` / `load_store` wrap that already existed rather than adding a second error path; a genuinely EMPTY directory still answers `scope-empty` at 0, separated by MECHANISM (`iterdir()` returns `[]` vs raises) and pinned on both sides. `validate-unreadable-scope-dir` and `recall-unreadable-scope-dir` gate it, with a FOURTH content-floor sentinel keyed on the row's own field — the two families print the IDENTICAL sentence, so a sentinel reading the sentence alone would let a run that had stopped chmodding directories vouch for a floor it never reached. 🔴 **AND SO IS THE CACHE-ROOT-AT-`0111` HALF, WHICH THIS ROW SPENT A ROUND DECLARING CLOSED — the THIRD unwrapped read of the store, and the one the row's own "(END OF SET.)" asserted did not exist.** `load_store`/`LoadStore` wraps the index walk and `entry_files_or_unreadable`/`EntryFilesOrUnreadable` wraps `validate`'s per-scope denominator; the read that enumerates the cache ROOT — `cmd_validate`'s `held`, which decides WHICH scopes are validated at all — was a bare `cache.iterdir()` on the oracle and a bare `os.ReadDir(cache)` returning its raw `*os.PathError` on the Go client, and had been since before this branch. **PRE-EXISTING, and what made it a finding is the DECLARATION rather than the line**: row 4 attributed the whole cache-root depth to `resolve_state` and named a remedy that cannot reach `held`. The fix is one new wrap per client over the ROOT walk, spelling the sentence through the SAME writer the other two use (`_store_unreadable` / `StoreUnreadable`, now three call sites for one set of bytes) — verified not by re-reading the words but by comparison: `validate`'s last stderr line at this mode is now BYTE-IDENTICAL to the line `recall` already printed, asserted in `tests/test_cairn_cli.py::TestASearchableButUnreadableCacheROOTExitsThreeAndNeverTracebacks::test_the_sentence_is_the_one_recall_ALREADY_printed`. `validate-unreadable-cache-root` gates the cross-client bytes, with a FIFTH content-floor sentinel keyed on its own field — all THREE mode families print the same sentence, so a run that had stopped chmodding the ROOT would still set the other two and vouch for a floor it never reached. ⚠ **ONE parity row, not three, and the absence is the argument**: `recall` and `search` already agreed at `8ddbb6f`, so rows on them would be INVARIANT guards wearing a regression row's name. The two `validate` ARGV shapes are covered per client instead, because what differs between them is a branch below `held`, not the bytes two clients print. ⚠ **And one seam is DECLARED rather than closed, deliberately**: the oracle's per-child `is_dir()` RAISES outside `pathlib._IGNORED_ERRNOS` while `ScopeDirsOrUnreadable`'s per-child `os.Stat` skips on ANY error. That asymmetry pre-dates this change and NO MODE REACHES IT — a child cannot be `stat`ed at all without `x` on the root, and without `x` the run has already diverged at the stamp check — so aligning it would add a guard nothing can make fail. 🔴 **What is left is ONE condition, at the cache root WITHOUT `x`, and it is a DIFFERENT one from the case just closed rather than a smaller version of it; nor is it closed by catching `OSError` at the oracle's CLI boundary**: Go answers that case with a BANNER and no error line, so an errno line at exit 3 would trade an exit-code divergence for a text one. Closing it means teaching `resolve_state` that an unreadable stamp is "no cache", which is its own change with its own reasoning. ⚠ **That remedy is now known NOT to cover the `0111` case** — it was written as though it covered the whole depth, and the left-hand cell records what that would have cost. 🔴 **THE CLOSING CONDITION WAS NARROWED TO THREE VERBS AND COULD THEREFORE GO GREEN WHILE THE DIVERGENCE REMAINED — IT IS WIDENED TO THE VERB AXIS.** It named `recall`, `search` AND `validate`, which was the set somebody had measured, offered as the set that exists. RE-MEASURED at `24eb508` over an isolated `HOME`/`XDG_CONFIG_HOME`, one instance, a one-route table, cache root chmodded per run, every verb `--no-sync`: at **`0444` and `0000`** the oracle exits **1** with a traceback and the Go client **3** with the banner for `recall`, `search`, `validate` **and `ls-entries`** — and `routes --check` is **1** against **11**. So the real set is **five verbs, not three**: every verb that resolves state, because the raise is `resolve_state`'s stamp `stat` and is upstream of all of them. A row pinning three of five is a row that can be satisfied twice over while two verbs still diverge, which is the same shape as the "(END OF SET.)" sentences this row has already been wrong about twice. **CLOSING CONDITION** — a merged PR after which, at cache-root modes `0444` **and** `0000`, both clients answer **identically for every verb that reads state**: `recall`, `search`, `validate`, `ls-entries` and `routes --check`. 🔴 **DO NOT re-narrow this to the verbs a row happens to cover** — the verb list is derived from the client's own subcommand table, and `tests/test_store_read_sites.py` is what enumerates the reads behind it. Mechanically: a parity row per verb whose id ends `-unreadable-cache-root-no-exec` for each of the five, `python3 tests/parity/harness.py \| grep -c -- '-unreadable-cache-root-no-exec'` is **5** (a count, not a `-q`, because a single PASS cannot show four missing rows) with all five reported `^PASS`, AND the run's own `SUMMARY … failures=0` line holds. ⚠ **`routes --check` MAY NOT BE EXPRESSIBLE AS A PARITY ROW AND THAT IS MEASURED, NOT ASSUMED**: the harness chmods BEFORE the run and `routes --check` refuses any instance that is not `STATE_LIVE`, which `--no-sync` can never be — so if it cannot be rowed, the condition is met by a per-client test naming that verb and this sentence must say so rather than dropping the verb. |
| 5 | **A `SyntaxError` in the reader's own modules.** On the oracle a present-but-unparseable `lib/cairn_doctor.py` takes every verb down at exit 1; the Go client has no such failure mode. | It is a property of loading Python at runtime and cannot exist in a single binary. The oracle's own comment says widening its `except ImportError` is *not* the obvious fix. |
| 6 | **`ReadStamp` has no "is not text" arm.** The oracle distinguishes an unreadable stamp from one that is not valid UTF-8, because `read_text` raises; Go's `ReadFile` returns bytes and cannot fail on encoding. | The stamp is written by this program and is ASCII, so the arm is unreachable in practice. Re-validating the bytes to manufacture the distinction would be inventing a check the oracle only has by accident of its API. |
| 7 | **The Go client's own ledger flags, `-verbs` and `-exit-codes`.** Measured on both binaries: each exits **0** with its table on **stdout** on the Go client, and **2** with argparse's `usage:` block on **stderr** on the oracle. This is the same family as the four `--help`/argument-shape divergences above — the Go client *succeeding* where the oracle refuses — and no row covers it, because the flags were added **for** the gate's sibling ledger (`tests/test_go_client_ledgers.py`) and the gate is therefore structurally blind to them. | **Not closable while both clients ship, and mirroring it into the oracle is the mistake this repo already paid for and deleted.** The Python-side ledgers read the argparse parser (`testlib.capability_ledger`) and the `cairn` script's AST directly, so a printed table on the oracle would have no reader — which is residual finding 3 above (`CAIRN_CACHE_ROOT`) exactly: a second mechanism reaching one value leaves the first silently dead. What IS gated is that the set cannot move unnoticed: `test_the_GO_ONLY_ledger_flags_are_exactly_the_declared_set` compares THREE operands, two of them discovered — the `switch argv[0]` dispatch in `cmd/cairn/main.go` read as source, its own declared tuple, and what both binaries actually do when handed each probe — and fails if a third Go-only flag appears, if a declared one stops diverging, or if the dispatch moves out of the file the discovery greps. ⚠ Its first cut built the probe set out of the declaration, so shrinking the tuple shrank what was measured and the mutant SURVIVED; that is why the probes come from the source. 🔴 **THE WIDENING BELONGED TO THE DEFAULT CUTOVER, NOT TO P8 — AND IT HAS NOW HAPPENED.** This row once said P8 ("the CLI contract *widens* at that moment [when the oracle is deleted]"), which was the wrong moment. `packages.default`/`apps.default` are the Go client as of the flip, so `nix run github:…/cairn -verbs` — refused at exit 2 before it — answers 0 on stdout from that commit, for every consumer who does not name `#cairn`. **Measured at BOTH points on the flip's own branch, because one point is not a general claim:** `nix run .# -- -verbs` → **rc 0**, 134 B on stdout, 0 B on stderr; `nix run .#cairn -- -verbs` → **rc 2**, 0 B on stdout, argparse's `usage:` on stderr. `-exit-codes` has the same shape (rc 0 / 353 B against rc 2 / 0 B). The announcement it was owed is `README.md`'s *"The default client is now the Go one"* section. ⚠ **WHAT LICENSED THE FLIP WAS AN OPERATOR DECISION, AND THAT PHRASING OUTLIVES THE FLIP.** A branch took it because the gate was green and was REVERTED; the gate being green never licensed it and does not now — this row is the record so nobody re-derives the licence from a green run. The decision was taken after residual 8 closed, which removed the flip's one MEASURED blocker: every READ verb refused at exit 11 on a multi-instance host, so the documented `nix run … -- doctor` quickstart would have refused there. That closure is a PRECONDITION, not the licence. What P8 owns is unchanged and is a DECISION, not a deletion: either document `-verbs`/`-exit-codes` as public surface in `README.md`'s verb table, or move them behind an undocumented gate the ledger tests still reach. **Closing condition, owned by P8:** the day the oracle is deleted there is nothing left to diverge from, so this row and that guard are deleted with it — together with that decision. |
| 9 | **THE ANCHOR-INSIDE-THE-PATTERN CLASS IS CLOSED, AND WHAT THIS ROW NOW DECLARES IS THE TWO NARROW DIVERGENCES THE FIX CHOSE.** ⚠ **Its predecessor claim is RETIRED, kept here as the record rather than deleted, because three separate comments in the tree pointed at it.** It read *"`put` derives its revision by globbing a pattern built out of the CACHE ROOT"* and declared the class open with two live sites in `verbs.go`; measured end to end against one pod with both real binaries, a cache root named `cache[bad` took the Go client to `cairn: cannot derive a revision — 0 cached file(s) match alpha-notes/widget-cfg`, **exit 2**, where the oracle answered `replaced` at **exit 0** off a derived `If-Match`. That is fixed, together with `Focus` (a repo path — the SEVERE one, a false claim of absence on the default `recall` path) and `ReapOrphans` (a cache parent — a leak nothing prints). One rule, one place: `internal/client/anchor.go`. **What remains is a deliberate, measured consequence of fixing it.** The oracle interpolates OPERATOR DATA into its own patterns at two of those sites — `cache.glob(f"{scope}/{ref}.md")` in `cmd_put`, and `cache.parent.glob(f"{prefix}*")` in `_reap_orphans`, where `prefix` carries the cache's own basename — and the Go client no longer does: it matches an exact name plus a `<ref>.*.md` family, and a literal prefix. So `put --ref 'wid*'` is a WILDCARD on the oracle and a literal on the Go client, and a BALANCED `[...]` in a cache's basename is a character class to `fnmatch` and to `filepath.Match` alike — measured, they agree there — but neither to `strings.HasPrefix`. | **DECLARED, NOT CLOSED, AND THE DIRECTION IS WHY.** Both remaining differences are the Go client REFUSING or DECLINING where the oracle acts on an interpretation nobody asked for: a `--ref` with a `*` in it can match exactly one file on the oracle and derive a precondition from `widget-cfg.md` while the `PUT` that follows addresses an entry literally named `wid*` — a precondition taken from bytes the request is not addressing, which is the one hazard `put`'s whole revision block exists to prevent; and a cache named `wid[ge]t` makes the oracle skip its own staging trees while offering to reap `widgt.old-…`, which belongs to a different cache. Mirroring either into the oracle is the mistake this table exists to refuse. 🔴 **NEITHER IS REACHABLE FROM THE CORPUS, AND THAT IS STATED RATHER THAN LEFT TO BE DISCOVERED:** no row passes a metacharacter `--ref`, and every cache BASENAME the harness builds is spelled out of `[a-z-]` — the metacharacter it now seeds is in the world ROOT, which is the anchor half and the half that was actually broken. **CLOSING CONDITION:** P8 — **and it is now ON P8's own ledger**, in the retirement table below. 🔴 It was not, for one round: a row closing on a checklist that does not list it is an object nobody can close, which is the failure this file names elsewhere and committed here. ⚠ **AND THE PREDECESSOR'S OWN MECHANICAL CHECK DID NOT FIRE WHEN THE ROW WAS RETIRED.** It pinned `go test ./internal/client/ -run PutDerivesARevisionUnderAMetacharacterCacheRoot -count=1 -v` and spent a paragraph warning that a zero-selection `-run` exits 0; the test that retired it was written as `TestPutDerivesARevisionUnderACacheRootCarryingAGlobMetacharacter`, which that filter **does not select** — measured at `6696ad1`: `testing: warning: no tests to run` / `PASS` / `ok … [no tests to run]`, exit **0**, i.e. exactly the unmet state the warning describes, over a row retired on it. The evidence behind the retirement was never in doubt; the bookkeeping was. ⚠ **AND NEITHER WAS THE SHA, FOR ONE ROUND.** This sentence and its twin in `internal/client/anchor_test.go` both cited `6696a17`, which resolves to nothing — `git rev-parse --verify 6696a17` is `fatal: Needed a single revision`, so an auditor re-deriving the bookkeeping argument this row rests on got an error instead. The commit is `6696ad17ea094be1f49a666eab77c7f8b0008381`, short `6696ad1`; both sites are corrected and each corrected sha was checked with `git cat-file -e <sha>^{commit}`, with `deadbee` as the control proving that check can fail. Closed by RENAMING the test to the pinned name rather than by restating the condition, so the prior round's check still means something: `--- PASS: TestPutDerivesARevisionUnderAMetacharacterCacheRoot` is what that command now prints. Until P8, a row that made either divergence reachable would have to decide between matching `fnmatch` and declaring the difference — and the decision is the expensive half, which is why it is not taken here. |
| 10 | **A repo that is SEARCHABLE but not READABLE (mode `--x`) resolves its handoff doc on the Go client and reads as ABSENT on the oracle.** 🔴 **This was an UNDECLARED divergence for two rounds, hidden behind a CPython internal that does not exist.** `internal/client/focus.go` and `internal/client/anchor_test.go` both said the case *"resolves fine on the oracle, whose `_PreciseSelector` asks `is_dir()` rather than scandir'ing the parent"*; measured with `git log -S_PreciseSelector` plus a per-commit `git show <c>:<file> | grep -c`, the sentence entered in `anchor.go` at `7348820`, was COPIED into `anchor_test.go` at `6696ad1`, and the round-1 audit (`b91c4ed`) deleted the `anchor.go` copy while writing a THIRD into `focus.go` — the false claim MOVED, not removed, twice. MEASURED against the pinned interpreter (`flake.nix` → `python312`, **3.12.14**), positive control first: `_make_selector` and `_WildcardSelector` are both present in `pathlib.py`, so the grep can see what is there — and **`_PreciseSelector` is absent from it**. 🔴 **ROUND 3 THEN REPLACED THAT WITH A SECOND WRONG MECHANISM, AND IT IS RETRACTED HERE.** This row read *"at mode `0111` `os.listdir` raises `PermissionError` … and `focus_window`'s own `except OSError` turns that into an ordinary empty answer."* **It does not: that arm never executes in this scenario.** Measured with an `os.scandir`/`os.listdir` spy on the same pinned 3.12.14, not read — at `0111`, `os.listdir` is called **0** times and `Path(repo).glob("claudedocs/handoff-*.md")` returns `[]` **without raising**; control, the same fixture readable, returns the doc. So an auditor asking whether `focus_window`'s `except OSError` is dead code must not keep it on this row's authority, and **adjusting that arm cannot close this row** — what would is the oracle-side change and the paired test row named in the right-hand column. 🔴 **EVERY WORDING OF THIS ROW THAT NAMED A MECHANISM HAS BEEN WRONG, SO THE CLAIM IT RESTS ON IS NOW BEHAVIOURAL AND NOTHING ELSE: for a repo at mode `0111` the Go client returns the doc and the oracle returns an empty window. It is CPython's globbing that produces the empty side.** That is version-independent; a selector name is not. If a mechanism detail is ever wanted here, measure it with a spy, pin the interpreter version beside it, and say it was measured. End to end on one synthetic fixture, same repo, mode flipped between the two reads: readable → both sides `source='claudedocs/handoff-demo.md'`; at `0111` → Go `Source="claudedocs/handoff-demo.md"`, oracle `FocusWindow(paths=(), source=None)`, i.e. *"most-recent fallback … (no handoff doc to read a path window from)"*. **That is the same false-claim-of-absence class `Focus` was fixed to close, with the clients swapped.** | **DECLARED, NOT CLOSED — and the Go side is deliberately NOT changed to match.** Answering the doc is the NON-NARROWING direction and is what `filepath.Glob` already did here before the fix; making `Focus` enumerate `<repo>` to reach parity would reintroduce the empty-result-reported-as-an-answer shape on a case that works today. Closing it therefore means moving the ORACLE — `focus_window` pre-`stat`ing the pattern's directory prefix instead of `scandir`ing the repo — which is a change to the reference implementation and is not taken here. 🔴 **THE GATE CANNOT SEE IT, AND THE REASON IS STRUCTURAL:** `tests/parity/` contains **no `chmod` call at all** (`grep -n 'chmod\|0o111\|S_IX' tests/parity/*.py` → no match), so every world it builds carries default permissions and no row can construct a `--x` repo; seeding one would also have to restore the mode before teardown. Declared here rather than left to be discovered. **CLOSING CONDITION (runnable, and UNMET today):** the oracle grows the paired row `tests/test_subsystem_recall.py::TestFocusWindow::test_a_repo_that_is_SEARCHABLE_but_not_READABLE_still_resolves` and `python3 -m pytest tests/test_subsystem_recall.py -p no:randomly -q -k a_repo_that_is_SEARCHABLE_but_not_READABLE` reports **`1 passed`**. 🔴 **A ZERO-SELECTION RUN IS NOT THE MET STATE, AND IT IS THE STATE TODAY** — measured at this head, that `-k` prints `405 deselected in 0.48s` and exits **5**; read the PASSED COUNT on the summary line, never `$?`, and never the absence of a failure. The Go half already exists and stays: `go test ./internal/client/ -run TestFocusDoesNotREADADirectoryTheGlobOnlyDESCENDSTHROUGH -count=1 -v` must print a `--- PASS:` line, and per the preamble above a zero-selection `-run` prints `testing: warning: no tests to run` / `ok … [no tests to run]` at exit **0**, which is likewise not the met state. ⚠ The Go row skips under EUID 0, where root bypasses the missing read bit and the state cannot be constructed at all; the paired oracle row would have to as well, and a skip is not a pass either. |

## Argument-shape rows

🔴 **Following the `--help` finding one step further found four more divergences, all in the
dangerous direction — the Go client SUCCEEDING where the oracle refuses.** If `-h` is special, what
happens when it is a VALUE? The rule is argparse's, and both halves are measured:

| shape | ruling | row |
|---|---|---|
| `--text -h`, `--limit -h`, `--scope -weird`, `--scope --repo .` | **exit 2** — a token beginning with `-` is an OPTION, not a value | `argv-help-in-a-VALUE-position`, `argv-option-shaped-value`, `argv-flag-as-a-value` |
| `--limit -1` | a **value** — argparse's `_negative_number_matcher`, so it reaches the reader's own option ladder and is refused there with the READER's message (full text compared) | `argv-negative-number-IS-a-value` |
| `search -- -h` | `--` ends the flags: the query is the literal `-h` (full text compared) | `argv-terminator-passes-a-dash-token` |
| `search --scope S -h` | **help** — a standalone `-h` anywhere is help, which is the opposite ruling from the value position, and the pair is what makes the rule observable | `argv-h-in-a-POSITIONAL-position` |
| `--bogus --help` in either order, and `--bogus-global --help` | **help wins** — argparse COLLECTS unrecognised arguments and reports them after parsing, while `-h` fires the moment it is consumed. Needs the refusal DEFERRED in two separate loops; a fix applied to one leaves the other wrong | three `argv-help-beats-an-unknown-*` rows |

`append --text=-h` is how a caller passes a literal `-h` as a value, on **both** clients.

## What the gate structurally cannot see

🔴 **A DEFECT THAT NEEDS THE WORLD TO *CHANGE MID-RUN* — AND THIS ONE WAS A LIVE DIVERGENCE FOR A
ROUND, NOT A HYPOTHETICAL.** Every row here runs two binaries over a **fixed tree**, so a
condition that exists only BETWEEN two reads inside one process cannot be a row at all. `cairn
validate` / `Validate` read each scope directory **twice** — once through `load_store`/`LoadStore`
for the index, once more for the printed line's DENOMINATOR — and #119 made that second read able
to fail (`glob` → `iterdir()`) without wrapping it. Measured at `e162746` over one cache holding
two scopes, the second removed after the first scope's line was printed: **the oracle died with a
`FileNotFoundError` traceback at exit 1 and the Go client printed `cairn: <scope>: 0 of 0 entry
file(s) parse, 0 malformed` at exit 0** — a reintroduced traceback on one side, a confident zero
over an unread directory on the other, and this gate green throughout. No *static* world reaches
that read — but 🔴 **the REASON this paragraph gave for that was FALSE, and it is corrected here
rather than restated.** It said *"`held` and the loader both resolve `<cache>/<scope>` from the same
parent listing and both test `is_dir()`, so any mode or absence that stops the second walk has
already stopped the first."* **They do not both test `is_dir()` on the Go side.** `Validate`'s held
loop skips a child on **any** `os.Stat` error — behaviour now carried verbatim into
`store.ScopeDirsOrUnreadable` — while `LoadIndex` `continue`s only for the four errnos in
`pathlib._IGNORED_ERRNOS` and **returns** every other stat error, a distinction its own comment
argues at length. So the two predicates genuinely disagree, and the old sentence asserted an
equivalence this tree contradicts. **What actually holds — and it is sufficient — is an ORDERING,
not an equivalence:** `LoadIndex` walks `<cache>/<scope>` INSIDE `LoadStore`, which owns the wrap,
*before* the denominator's second walk of that same directory is reached, so any static condition
that stops the second has already failed the wrapped first one closed. And where the two predicates
DO disagree, `held` is the side that SKIPS — so the scope is never iterated and the denominator is
never reached at all. Round 2 reached neither denominator across **12 adversarial static worlds**;
that measurement stands, and only its stated reason has moved. ⚠ **The conclusion is deliberately
not restated more strongly than it was measured**: "no world we could build reaches it" is the
claim, not "no world exists" — which is why the two per-client guards below exist at all.
**The general form — ask whether the defect needs a STATE TRANSITION rather than a state —
is what this paragraph is for.** What covers it instead is one guard per client, each staging the
transition deterministically through the stdout writer the verb already takes, so the removal is
performed BY the client mid-iteration with no timing window:
`internal/client/validate_test.go::TestAVanishedScopeDirectoryIsNotCountedAsZeroEntries` and
`tests/test_cairn_cli.py::TestAScopeThatVANISHESMidRunIsNotServedAsZeroOfZero`, both RED at
`e162746`, both pinning the identical `index entry unreadable: under <cache> (FileNotFoundError:
…)` sentence at exit 3. ⚠ Two guards rather than one shared harness: the device has to live inside
each client's own process, which is precisely why this gate cannot own it.

🔴 **AND THE FIRST CLOSED ENTRY IS A DIFFERENT DIMENSION OF THE SAME LESSON: A BYTE-IDENTITY
GATE IS BLIND TO EVERY DEFECT BOTH CLIENTS COMMIT IDENTICALLY, AND THE CORPUS IS WHAT DECIDES
WHICH THOSE ARE.** `world.py` seeded **no `README.md` in any scope**. A scope's `README.md` is its
policy sheet and not an entry — both loaders skip it, and `/snapshot` ships it, so every real
cache has them — but the rule was open-coded at four production sites and wrong at two. `cairn
ls-entries`, the verb the top-level `README.md` describes as *"what the cache actually holds"*,
listed every scope's sheet as an entry: **12 of them on a populated cache**, on BOTH clients, so
every row here compared equal and this gate was green over the miscount for its whole existence.
Nothing about the differ was broken; it was never handed the discriminating input. The world now
seeds two sheets, two lookalikes (`readme.md`, `README-old.md`) that ARE entries and must be
listed, and keeps `rubble-heap` README-free so "exclude `README.md`" is distinguishable from
"drop one file per scope". **The general form — ask what your corpus does NOT contain before
reading a green byte-diff as coverage — is why this paragraph stays after the hole closed.**

🔴 **AND THE SECOND CLOSED ONE IS THE SAME MECHANISM IN A DIFFERENT DIMENSION, WHICH IS WHY IT
GETS A PARAGRAPH RATHER THAN A LINE: EVERY PATH THIS WORLD BUILT WAS SPELLED OUT OF
`[A-Za-z0-9_-]`.** The Go client built `filepath.Glob` patterns by joining an ANCHOR — a cache
root, a repo path, a cache's own basename — onto a pattern, and `filepath.Match` interprets `*`,
`?`, `[` and `\` wherever they occur; an unterminated `[` is `ErrBadPattern`, and every one of
those call sites discarded the error, so the defect arrived as an EMPTY RESULT. The oracle's
`Path(anchor).glob(pattern)` has never had it. **Four sites, one class, and this gate could not
see any of them** — not because the differ was weak, but because the corpus could not produce the
character. One was found by reading (`ls-entries`); the other three were declared as residual 9
and are fixed. The world root now ends in `WORLD_METACHARACTER_SUFFIX`, so the cache roots, the
repo's parent and the store all carry a `[`; with that one constant in place and the code
unfixed, **three rows and one structural check go red** —
`recall-focus-resolved-through-an-explicit-repo-PATH`, `put-derives-if-match`,
`put-routed-to-a-NON-DEFAULT-instance` and `orphan-reap-parity`. ⚠ **`*` AND `?` ARE STILL NOT
SEEDED, AND THE REASON IS THAT THEY NEED A SECOND DIRECTORY:** they do not error, they match the
WRONG thing, so a fixture has to contain the sibling that gets matched instead. `[` is the shape
that reaches `ErrBadPattern`, which is what every affected site turned into "nothing is here".

- **A scope name that is a prefix of another AND is followed there by a byte below `/` — and this
  one is not hypothetical, the two clients ALREADY DIVERGE on it.** 🔴 **THE CONDITION IS NOT
  "a prefix", AND THIS BULLET SAID IT WAS.** The mechanism is BYTE-WISE vs COMPONENT-WISE: the Go
  client sorts the joined `scope/name` byte by byte (`sort.Strings`), while the oracle sorts
  `Path` objects, whose `__lt__` compares `_parts_normcase` — a tuple, component by component
  (CPython 3.12.14). Where scope `S` is a proper prefix of scope `T`, the oracle always puts all
  of `S`'s files first (`S` < `T` as strings); the Go client compares `/` (0x2f) against `T`'s
  first byte PAST the prefix, so it **agrees** when that byte sorts above `/` and **diverges**
  when it sorts below. MEASURED end to end on both clients over a two-scope cache:

  | scope pair | byte after the prefix | verdict |
  |---|---|---|
  | `a` / `a0` | `0` (0x30) | agree |
  | `a` / `aZ` | `Z` (0x5a) | agree |
  | `a` / `a_b` | `_` (0x5f) | agree |
  | `a` / `a+b` | `+` (0x2b) | **diverge** — oracle `a/y.md` first, Go `a+b/x.md` first |
  | `a` / `a-b` | `-` (0x2d) | **diverge** |
  | `a` / `a.b` | `.` (0x2e) | **diverge** |

  A pair that is NOT in a prefix relation cannot diverge at all: the first differing byte then
  lies inside both scope names, where the two comparisons agree. 🔴 **So the old sentence was
  not merely imprecise — it was a trap: somebody widening the corpus by it would seed `a`/`a0`,
  get a GREEN, and leave the gate exactly as blind**, which is the failure this whole section
  exists to warn about. Both sorts long predate the row that found this. No scope in `world.py`
  is a prefix of another (`alpha-notes`, `beta-notes`, `rubble-heap`, `hollow-set`, and the
  empty-scope names), so the gate has never been handed the discriminating input and every
  `ls-entries` row compares equal — the SAME mechanism as the closed README entry above, in a
  corpus dimension nobody had asked about. Closing it needs a corpus pair **whose second scope
  name continues with a byte below `/`** AND a decided direction, because agreeing means changing
  one client's stdout.

  ⚠ **The remedy, if the decision goes the oracle's way, is one deleted line — MEASURED, not
  designed.** Since `LsEntries` walks the cache root instead of globbing it, `os.ReadDir` returns
  the scope directories sorted and `store.EntryFileNames` returns the names sorted, so the lines
  are already in component-wise order; the Go client's final `sort.Strings` is the only thing
  re-ordering them byte-wise. Deleting it and re-running both clients over `a`/`a-b`, `a`/`a.b`,
  `a`/`a0` and an ordinary six-entry cache gave a byte-identical listing in all four. Recorded,
  **not applied** — it is still a change to a verb's stdout and this bullet stays open until
  somebody decides the direction and adds the corpus pair.
- **Concurrency.** Both clients take the same `flock` around the cache swap, which is why they can
  share a root at all; nothing here runs them at the same instant.
- **A CONDITIONAL sync whose validator was minted by the OTHER implementation.** Every row wipes
  the shared root per client (finding 9), so each client only ever presents a tag it stored
  itself. That the two mint the SAME tag for one store is `tests/dualrun/`'s claim — the `ETag`
  header is compared literally on every snapshot target there, and `snapshot-conditional-derived`
  round-trips it — not this gate's. What this gate does measure is that both RENDER a `304`
  identically, which is the half a human reads.
- **Real network failures.** An unreachable pod is a connect refusal to a closed port. A DNS
  failure, a mid-transfer reset and a half-open socket are not built, and see difference 2.
- **A narrowed credential.** The token's allowlist names every scope the world holds. The
  *server's* narrowing is the conformance corpus's claim, not this one's — but note that a refused
  scope and an absent one are byte-identical by design, so a client cannot tell them apart either
  way.
- **A multi-instance host beyond the three READ rows and the two `routes` rows.** The world stands
  up exactly TWO instances against TWO pods; three instances, an instance whose config is present
  but empty, and a fan-out where one pod is down are not built.

  🔴 **THIS BULLET USED TO END *"The per-verb behaviour there is `internal/client`'s own tests'
  claim, not this one's"*, AND THAT SENTENCE WAS FALSE ON THE COMMIT THAT ADDED IT.** Measured at
  `0c187d76`: `searchEveryInstance` — the whole `search --all-scopes` fan-out — had **zero**
  behavioural coverage. Inserting `if true { return ExitOK, nil }` as its first statement, a
  fan-out that searches nothing and reports success, **compiled** (`go build ./...` rc 0) and left
  `go test -count=1 -v ./internal/client` at **51 PASS / 0 FAIL**, byte-identical to the unmutated
  run. The only occurrence of `--all-scopes` anywhere under `internal/client`'s tests was an
  argv-rejection row (`recall --all-scopes` is not a flag `recall` takes), which never reaches the
  fan-out at all. A sentence that READS as coverage while providing none is worse than no sentence,
  because it stops anyone looking — so the retraction is recorded here rather than quietly
  rewritten. What is true now, gap by gap:

  | gap | covered by |
  |---|---|
  | the fan-out over several instances — every one searched, each section labelled, its own cache read | `TestAllScopesFANSOUTToEveryInstanceAndLABELSEachSection`, `TestAllScopesDoesNOTRequireThisReposScopeToBeRegistered`, `TestAnUNREADInstanceMakesTheFanOutLOUDAndNonZero`, plus mutant `go-a-search-fan-out-reads-ONE-instance` |
  | a fan-out where one pod is **DOWN** | **nothing.** The three rows above run OFFLINE: an instance is unread because it has no cache under `--no-sync`. That reaches the SAME branch (`state.ExitHint != 0`) a dead pod would, which is why the PARTIAL banner is measured — but a branch reached by another cause is not the cause |
  | an instance whose config is present but **empty** | `TestAPutLoadsTheROUTEDCredentialsLAZILY` — for `put` only. **No read verb has a row** |
  | **three** instances | **nothing.** Every test in this repository, both languages, stands up exactly two |

  ⚠ The three rows above are `internal/client`'s claim and NOT this gate's, which is what the
  retracted sentence was reaching for. The difference is that the pointer is now checkable: the
  test names are in it, and `tests/routing_mutants.py` carries a mutant that dies by name.
- **The `doctor` states no world reaches.** An unreadable local root (`token-scopes` UNMEASURED
  with `unread`) needs a mode-000 directory, which a root-run CI job would not honour.
- **Anything after the pod answers 5xx from a real fault.** The world is healthy; `503` is only
  reached through a refused scope, which this token does not have.

### …and what the NIX CHECKS cannot see, which is why this gate exists at all

Relocated from `AGENTS.md`, which keeps the imperative — *ask what your sandbox cannot
have before reading its green as coverage* — and hands the evidence here.

`checks.client-resolves-its-lib` runs in a nix sandbox whose HOME has no cache root. That
pinned dimension is exactly why it did not notice the `_visibility_check` defect that took
`cairn doctor` to exit 1 with zero stdout on any host that HAD one — a crash recorded beside
the code it lived in (`lib/cairn_doctor.py`, `internal/doctor/collect.go` and both of their
tests), and invisible to a check whose world cannot contain the trigger.

**Every `*-declares-its-*` check has the same shape of blindness**: no store, no token, no
network and no cache-root HOME, so each exercises a LEDGER and nothing about behaviour. This
harness is what measures behaviour instead, and it needs a running pod that a nix sandbox is
the wrong place for.

## The mutation battery over P2 — 61 mutants, 58 killed, 3 labelled equivalent at the code

🔴 **Also relocated here from `AGENTS.md`, for the same reason: this is a round-by-round record,
not a rule.** The three survivors' authority is the label **in the code**, beside the thing
labelled; this is the narrative and the harness lessons, which is what a later round needs and
what a session start does not.

Round 1 killed 44 of 51, and its findings are why there was a round 2 — **every one of them was a
hole in a guard rather than a defect in the code**, which is the useful direction. Rounds 3 and 4
added ten mutants over the `--help` and argument-shape paths that earlier rounds had no code to
mutate, and round 4 is CLEAN: the three survivors below are labelled equivalent, which is not a
finding, so the ladder ends there.

- **a `-run` filter that excluded the killing test.** `-run EMPTYDetail` matches nothing against
  `TestACheckWithAnEmptyDetailIsREFUSEDAtConstruction`, so deleting `Check`'s empty-detail
  refusal was scored SURVIVED while the guard was live. The harness was wrong, not the code.
- **an unreachable assertion inside a live guard.** A mutant removing the per-entry path dedup
  survived, because the INPUT dedup's assertion ran first and short-circuited the `PathCount`
  one. Isolating the mutation needed a path that names one entry TWICE — and the obvious fixture
  (`apps/widget-cfg/widget-cfg.yaml`) does NOT work, because `PathRefs` collapses the identical
  `("widget-cfg", "widget-cfg")` pair the directory and the stem both produce. It takes
  `apps/widget-cfg/Widget_Config.yaml`: the filename tier from the directory, the ALIAS tier
  from the stem.
- **an empty stamp read as a stamp.** A zero-byte `.sync-stamp` reported STAMPED with no
  fields, which makes `doctor` grade `reader-resolution` OK and `cache-stamp` OK with
  `(the stamp is empty)` — a store that cannot date itself reporting a clean bill of health.
- **a refusal message that mangled itself.** Backticks inside a double-quoted `echo` in
  `checks.go-client-declares-its-verbs` are a command substitution: the failure printed
  `writes: command not found` and lost the word it was about, on the one path nobody reads until
  something is already broken. Found by running that check's OWN negative control.
- **the ranking's primary key and last resort, unreachable.** Every `digest-focus-*` row
  resolved exactly ONE entry, so a comparator sorted ascending — or with the count key deleted
  entirely — survived. Two fixtures now make both observable.

The three survivors are labelled EQUIVALENT at the code with the reasoning, not left for a
sweep to re-derive: `report`'s `byRef` filter (`Matched` can only hold entries of the scope
`read` already covers, and a failed read aborts the report rather than producing a partial one),
`sort.SliceStable` over that total order (refs are unique within a scope, so stability is
unobservable — the same label `ListingOrder` carries), and `doctor`'s `MirrorRoot != ""`
condition in the visibility check (an empty path reaches `os.ReadDir("")`, fails ENOENT, and the
`absent` re-check stats `""` to the same answer — so it degrades to the benign branch **by
accident of a libc detail**, which is not a property to bet the NOT-OBSERVABLE-versus-absent
distinction on; the `frozen-mirror` check branches on the same condition and is NOT equivalent).

## The P8 retirement ledger — everything that exists only while the oracle does

🔴 **P8 is "retire Python; delete the oracle once the gate has held", and without this list it
starts by rediscovering what the oracle's existence paid for.** `AGENTS.md` named a retirement
condition for the `lib/` rule and for nothing else. These are the rest, each with the
mechanical check that it is genuinely dead rather than merely unused:

| what | why it exists only while the oracle does | how P8 knows it is safe to delete |
|---|---|---|
| **this whole directory** — `harness.py`, `world.py`, `hostile.py`, this README | it compares two clients. With one client there is no comparison to make | no `cairn` Python script in the tree; the `parity` CI job deleted in the same commit, not left permanently red |
| **`tests/dualrun/`** — `harness.py`, `genstore.py`, `mutants.py`, its README — plus `tests/test_dualrun_harness.py` and the `dualrun` CI job. ~2,900 lines | it runs BOTH SERVERS over one store and compares them byte for byte. `server/server.py` is one of the two arms, so with the oracle deleted there is no second arm and the harness cannot be run at all — not "is not worth running", *cannot* | mechanically: `server/server.py` absent from the tree **and** `grep -rn 'dualrun' .github/workflows/` empty **and** `tests/test_dualrun_harness.py` gone, all in the same commit — a harness whose arm is deleted is the permanently-red gate this repo refuses, and 17 collected tests silently erroring is worse than 17 deleted ones. ⚠ **ONE finding outlives it and does NOT belong here**: the audit record's `ts=` spelling (`+00:00`, not `Z`) is a claim about the served contract, not about the oracle, and its guard is `api.TestTheAuditRecordIsTheORACLESSPELLINGFieldForField` in `internal/api`'s own tests — already a Go-side test in a different runner, so it survives this deletion untouched. Do not fold it into the CI-job removal. Also re-home, do not delete, the two things the harness taught that are properties of the SERVER: that byte-identity is scoped to the *uncompressed* tar, and the mode-2 generated store — `genstore.py` is the only synthetic-world generator either server has |
| `internal/store/pyoserror.go` | it reproduces CPython's `str(OSError)` spelling (`[Errno 13] Permission denied: '…'`) so the Go reader's errors are byte-identical to the oracle's. Its own header says the parity gate is what compares those sentences | ⚠ **not a grep**: it is live code reached through `pyOSError` wrappers in `internal/client` and `internal/doctor`, so it will still have callers on the day the oracle dies. The question is whether the SPELLING is still owed to anyone — answer it by deleting the parity rows that pin it and seeing what else goes red. Then decide: keep the CPython spelling as the documented error contract, or re-record the goldens against Go's own |
| `tests/test_go_client_ledgers.py`'s two cross-client tests | `test_the_go_client_declares_EXACTLY_the_pythons_verb_set` and `test_the_two_clients_declare_the_SAME_exit_code_values` compute an equality against the PYTHON side | the file's own docstring already says so: "the day Python is retired this test is what has to be deleted deliberately rather than quietly stopping to hold". ⚠ THREE of the file's four tests are cross-client and go; the Go-ONLY one — `test_the_go_clients_exit_codes_keep_the_shared_set_at_0_and_9`, which carries both the `{0,9}` intersection and the "no doctor code is 1 or 2" check — STAYS |
| residual row 7, and `test_the_GO_ONLY_ledger_flags_are_exactly_the_declared_set` | both are statements about a divergence from the oracle | see row 7's closing condition, which is a DECISION and not only a deletion |
| the 31 narrower rows | 23 rows compare the exit code only and 8 compare exit plus a non-empty stdout, every one of them because argparse's text is not worth reproducing in Go (residuals 1 and 2) | with no argparse there is nothing to be narrow about: those rows either widen to a full byte diff against the Go client's own recorded output, or go away with the harness |
| `testlib.capability_ledger.cli_verbs_from_parser`, `testlib.cairn_source` | they read the Python client's parser and AST | `api.DeclaredRoutes()` / `cairn -verbs` / `cairn -exit-codes` are the replacements and already exist; the ledgers they feed must be re-pointed at those, NOT deleted |
| `packages.cairn`, `checks.client-resolves-its-lib`, and the `lib/` rule in `AGENTS.md` | the rule governs the client that is still shipped and still the oracle | ⚠ **TWO CONDITIONS, AND THIS ROW USED TO COLLAPSE THEM INTO ONE. THE FIRST IS NOW SATISFIED AND THE SECOND IS NOT, WHICH IS WHY THEY HAD TO BE SEPARATE.** ✅ `packages.default`/`apps.default` point at the **Go** client — the DEFAULT CUTOVER, taken on an operator decision after residual 8 closed, carrying residual 7's widening and `README.md`'s announcement. It was never blocked on the oracle's existence, and it did **not** retire anything in this row. ⬜ `packages.cairn` being GONE is P8's condition and is the one that retires the `lib/` rule — so until then `packages.cairn`, `checks.client-resolves-its-lib` and the `lib/` rule in `AGENTS.md` all still govern, unchanged by the flip. Both, in that order: ✅ **`packages.default` is the Go client**, then ⬜ **`packages.cairn` is gone** |
| residuals 3, 4, 5 and 6 | sub-microsecond mtime, a RAW `OSError`'s exit route (residual 4 — #111 closed its unreadable-ENTRY half and #119 its unreadable-SCOPE-DIRECTORY half, leaving only the mode-000 cache ROOT), a `SyntaxError` in a runtime-loaded module, `ReadStamp`'s missing arm — all four are properties of the ORACLE's implementation | they are deleted with the table; residual 3's `cache-mtime-parity` assertion is the one to think about, because the ULP bound it pins is a real property of the reader and is worth keeping as a Go-only guard. ⚠ Residual 4 no longer names "the traceback exit route", and the rewording is load-bearing: FOUR rows now gate what it used to declare — `validate-unreadable-entry` / `recall-unreadable-entry` (the entry file) and `validate-unreadable-scope-dir` / `recall-unreadable-scope-dir` (the scope directory) — so all four are NOT deleted with the table; they pin conditions the surviving client still has. 🔴 **The CONTENT-FLOOR sentinels go with them, and both of the two that cover these rows are keyed on a `Case` FIELD rather than on the sentence**, because the two families print the identical `index entry unreadable` line — a future prune that keeps the rows and drops the sentinels would leave a gate that passes when the `chmod` stops happening |
| **residual 9**, and the two declared-divergence paragraphs that name it | `put --ref 'wid*'` is a WILDCARD on the oracle and a literal here; a cache basename carrying `*`, `?` or a balanced `[…]` is a pattern to `fnmatch` and a literal to `strings.HasPrefix`. Both are statements about what the ORACLE interpolates into its own glob patterns, so neither can outlive it | 🔴 **THE ROW DECLARES P8 AS ITS CLOSING CONDITION AND WAS NOT ON THIS LIST UNTIL NOW — AN OBJECT CLOSING ON A LEDGER THAT DID NOT LIST IT**, which is the shape this whole table exists to refuse. Mechanically, the same as the rows above: `packages.cairn` gone, and row 9 deleted in the SAME commit together with the two paragraphs that cite it by number — `internal/client/verbs.go`'s `⚠ opts.Ref IS NOW LITERAL TOO` block and `internal/client/snapshot.go`'s `⚠ AND THE PREFIX IS NOW LITERAL` block. ⚠ **DELETE THE DECLARATIONS, NOT THE BEHAVIOUR.** The literal `--ref` and the literal prefix are the safe side and stay; with no oracle they stop being divergences and become simply what the client does. ⚠ One measurement in `snapshot.go` is worth re-homing rather than deleting — that `fnmatch` and `filepath.Match` agree on a balanced class and both disagree with `strings.HasPrefix`, while `*`/`?` make the oracle over-reap a sibling cache — because it is a property of the two matchers, not of the oracle |

⚠ **This ledger is a list of things to DECIDE about, not a delete script.** Three rows above
(`pyoserror.go`, the narrower rows, the mtime bound) carry a measurement that outlives the
oracle; deleting them because the oracle went away would lose it.
