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

## What this gate found — ten divergences in seven findings

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

## Declared differences — the residuals, named rather than normalised away

⚠ **ROW 8 IS GONE AND ITS NUMBER IS NOT REUSED.** It declared that the Go client routed WRITES and refused READS at exit 11; the read verbs route now, so the difference it declared does not exist and the row was deleted with the guard that asserted it (`RefuseUnportedMultiInstance`). What replaced it is coverage rather than prose: `recall-routed-to-a-NON-DEFAULT-instance`, `recall-routed-to-the-DEFAULT-instance-is-still-labelled` and `ls-entries-walks-EVERY-instance` compare stdout, stderr and the exit code on a two-instance host, and the reader fixture carries four instance-bearing rows. **The numbering is left with a hole on purpose** — this file's rows are referred to by number from `AGENTS.md`, `lib/README.md`, `flake.nix` and the handoff docs, and renumbering would silently re-point every one of those at a different difference.

| # | difference | why it is not closed |
|---|---|---|
| 1 | **argparse's usage and help text.** Unknown flag, missing required flag, unknown subcommand, non-integer `--limit`, `doctor --scope`, bare invocation — and `--help` / `-h` / `<verb> --help`: the oracle prints argparse's `usage:` block and its wording; the Go client prints its own. | Reproducing argparse's layout, its prefix abbreviation (`--sc` → `--scope`) and its exact phrasing in Go is a second implementation of a library nobody reads twice. **The exit code matches on all of them** — 2 for the refusals, **0 for the help rows** — and that is the half a caller branches on. Refusal rows are `compare="exit"`; help rows are `compare="exit+stdout"`, which also asserts BOTH sides put something on stdout, because a client that printed nothing and exited 0 would pass an exit-only row while telling the reader nothing. 🔴 Before those four rows existed the Go client exited **2 with an empty stdout** on all of them — on the single most common invocation there is. |
| 2 | **`urllib` vs `net/http` failure text.** `<host> unreachable: <reason>` has a `urllib` tail on one side and a Go tail on the other; the oracle distinguishes a DNS/connect failure (`URLError.reason`) from a socket timeout (a bare `OSError`) where Go returns one `*url.Error` for both. | The host and the word `unreachable` — what a reader greps and what a human needs — are identical. Matching the tails would mean transcribing two libraries' error strings, which change with their versions. Rows that reach these are `compare="exit"`. |
| 3 | **Sub-microsecond mtime.** The oracle's `tarfile` carries a PAX mtime as a Python **float** and `os.utime` writes it back, losing precision Go's exact decimal parse keeps: measured `…236263` (oracle) vs `…236300` (Go) on the seed stamp, 37 ns. | **Measured unable to matter.** Both collapse to the same `float64`, which is the value the index order is decided on: one ULP is 238 ns at the current epoch — measured at four magnitudes (year 2000 → 119 ns, now → 238, 2038 → 238, 2100 → 477). `cache-mtime-parity` asserts the doubles are identical **and** that the raw delta stays under that ULP, so a future change that widened it fails here rather than reordering an index. |
| 4 | **A reader error's exit route.** A missing store root or an unreadable entry exits **3** on the Go client, naming the error. The oracle raises out of its subcommand and prints a traceback at exit **1**. | The Go behaviour is the CONTRACT the reader documents (3 is "the store is broken"); the oracle's 1 is an uncaught exception. Reproducing a traceback would be reproducing a defect. No row reaches it — the world is always readable — so it is declared, not measured. |
| 5 | **A `SyntaxError` in the reader's own modules.** On the oracle a present-but-unparseable `lib/cairn_doctor.py` takes every verb down at exit 1; the Go client has no such failure mode. | It is a property of loading Python at runtime and cannot exist in a single binary. The oracle's own comment says widening its `except ImportError` is *not* the obvious fix. |
| 6 | **`ReadStamp` has no "is not text" arm.** The oracle distinguishes an unreadable stamp from one that is not valid UTF-8, because `read_text` raises; Go's `ReadFile` returns bytes and cannot fail on encoding. | The stamp is written by this program and is ASCII, so the arm is unreachable in practice. Re-validating the bytes to manufacture the distinction would be inventing a check the oracle only has by accident of its API. |
| 7 | **The Go client's own ledger flags, `-verbs` and `-exit-codes`.** Measured on both binaries: each exits **0** with its table on **stdout** on the Go client, and **2** with argparse's `usage:` block on **stderr** on the oracle. This is the same family as the four `--help`/argument-shape divergences above — the Go client *succeeding* where the oracle refuses — and no row covers it, because the flags were added **for** the gate's sibling ledger (`tests/test_go_client_ledgers.py`) and the gate is therefore structurally blind to them. | **Not closable while both clients ship, and mirroring it into the oracle is the mistake this repo already paid for and deleted.** The Python-side ledgers read the argparse parser (`testlib.capability_ledger`) and the `cairn` script's AST directly, so a printed table on the oracle would have no reader — which is residual finding 3 above (`CAIRN_CACHE_ROOT`) exactly: a second mechanism reaching one value leaves the first silently dead. What IS gated is that the set cannot move unnoticed: `test_the_GO_ONLY_ledger_flags_are_exactly_the_declared_set` compares THREE operands, two of them discovered — the `switch argv[0]` dispatch in `cmd/cairn/main.go` read as source, its own declared tuple, and what both binaries actually do when handed each probe — and fails if a third Go-only flag appears, if a declared one stops diverging, or if the dispatch moves out of the file the discovery greps. ⚠ Its first cut built the probe set out of the declaration, so shrinking the tuple shrank what was measured and the mutant SURVIVED; that is why the probes come from the source. 🔴 **THE WIDENING BELONGS TO THE DEFAULT CUTOVER, NOT TO P8 — THIS ROW USED TO SAY OTHERWISE AND THE CORRECTION STANDS EVEN THOUGH THE CUTOVER DOES NOT.** The sentence read "the CLI contract *widens* at that moment [when the oracle is deleted]". That is the wrong moment: it widens when `packages.default`/`apps.default` become the Go client, because `nix run github:…/cairn -verbs` — refused at exit 2 today — answers 0 on stdout from that instant, for every consumer who does not name `#cairn`. ⚠ **The flip is NOT in the tree, AND THE REASON IT WAS HELD IS NOW CLOSED.** A branch took it and it was reverted on the operator's decision, for residual 8's reason rather than this one: the Go client refused every READ verb at exit 11 on a host with more than one instance configured, so the flip would have made the documented `nix run … -- doctor` quickstart refuse there. **Residual 8 is closed** — the read verbs route, the caveat is instance-aware, three multi-instance READ rows compare bytes, and the guard is deleted — so what remains before the flip is the DECISION and this row's widening, not a missing capability. 🔴 That is a narrower statement than "the flip is licensed": the gate being green has never licensed it, and the reverted branch is the measurement of that. So this is a PENDING widening, and it is owned by whoever lands the flip — including the announcement, since it changes a contract for consumers who never asked for a new client. What P8 owns is unchanged and is a DECISION, not a deletion: either document `-verbs`/`-exit-codes` as public surface in `README.md`'s verb table, or move them behind an undocumented gate the ledger tests still reach. **Closing condition, owned by P8:** the day the oracle is deleted there is nothing left to diverge from, so this row and that guard are deleted with it — together with that decision. |

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

- **Concurrency.** Both clients take the same `flock` around the cache swap, which is why they can
  share a root at all; nothing here runs them at the same instant.
- **Real network failures.** An unreachable pod is a connect refusal to a closed port. A DNS
  failure, a mid-transfer reset and a half-open socket are not built, and see difference 2.
- **A narrowed credential.** The token's allowlist names every scope the world holds. The
  *server's* narrowing is the conformance corpus's claim, not this one's — but note that a refused
  scope and an absent one are byte-identical by design, so a client cannot tell them apart either
  way.
- **A multi-instance host beyond the three READ rows and the two `routes` rows.** The world stands up exactly TWO instances against TWO pods; three instances, an instance whose config is present but empty, and a fan-out where one pod is down are not built. The per-verb behaviour there is `internal/client`'s own tests' claim, not this one's.
- **The `doctor` states no world reaches.** An unreadable local root (`token-scopes` UNMEASURED
  with `unread`) needs a mode-000 directory, which a root-run CI job would not honour.
- **Anything after the pod answers 5xx from a real fault.** The world is healthy; `503` is only
  reached through a refused scope, which this token does not have.

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
| `packages.cairn`, `checks.client-resolves-its-lib`, and the `lib/` rule in `AGENTS.md` | the rule governs the client that is still shipped and still the oracle | ⚠ **TWO CONDITIONS, AND THIS ROW USED TO COLLAPSE THEM INTO ONE.** `packages.default`/`apps.default` point at the Python client and pointing them at Go is the DEFAULT CUTOVER, which is not P8 and never was blocked on the oracle's existence. It WAS blocked on residual 8; that row is closed and deleted, so the cutover is now a DECISION (residual 7's contract widening, and an announcement) rather than a missing capability. `packages.cairn` being GONE is P8's condition and is the one that retires the `lib/` rule. Both, in that order: **`packages.default` is the Go client**, then **`packages.cairn` is gone** |
| residuals 3, 4, 5 and 6 | sub-microsecond mtime, the traceback exit route, a `SyntaxError` in a runtime-loaded module, `ReadStamp`'s missing arm — all four are properties of the ORACLE's implementation | they are deleted with the table; residual 3's `cache-mtime-parity` assertion is the one to think about, because the ULP bound it pins is a real property of the reader and is worth keeping as a Go-only guard |

⚠ **This ledger is a list of things to DECIDE about, not a delete script.** Three rows above
(`pyoserror.go`, the narrower rows, the mtime bound) carry a measurement that outlives the
oracle; deleting them because the oracle went away would lose it.
