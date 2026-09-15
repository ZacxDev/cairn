# cairn — repository rules

<!-- The canonical agent-instruction file. `CLAUDE.md` is a one-line stub that
     imports this one (Claude Code does not read AGENTS.md natively); edit HERE,
     never there. -->

`cairn` is a small hosted store for per-subsystem engineering notes: a pod that serves
scoped, per-token-authorised entries, and a client that syncs a local cache and reads it.

## 🔴 THIS REPOSITORY IS PUBLIC, AND IT WAS EXTRACTED FROM A PRIVATE ONE

Every file here came out of a private monorepo whose comments narrated real incidents on
named hosts, and whose tests used real internal project names as fixture data. That content
was removed by hand. **The single most important property of this repo is that it stays
removed.**

**Never commit:**
- a hostname, host name, cluster name, or network name from any private deployment;
- a private IP (RFC1918 or CGNAT), or a real public IP belonging to someone's infrastructure;
- a real project, client, customer, repository or scope name — **fixtures must be synthetic**;
- a dated incident reference (`<a real date>: …`). Keep the mechanism, drop the particulars.
  Fixtures that genuinely need a date use an obviously-synthetic **year 2000** one, which is
  what `leakscan.py` allows and what makes its remedy unambiguous;
- captured text of any kind — anyone's messages, prompts, transcripts, or a model's summaries
  of them — however it arrives. A test needs the SHAPE; regenerate it synthetic.

**`tests/leakscan.py` enforces this and runs in CI on every commit.** Run it before you push:

```bash
python3 tests/leakscan.py            # scan the tree
python3 tests/leakscan.py --self-test # prove the gate is an instrument
```

🔴 **A clean run is not evidence until both its controls have been watched to work.** The
scanner therefore runs its own controls on *every* invocation and exits **2** — not 0 — if a
control misbehaves. Exit 2 means "could not vouch", never "passed". If you add a rule, add a
**realistic** negative control for it: a scanner that only recognises its own textbook
examples passes a real leak.

## Comments: keep the mechanism, drop the particulars

The comments here are unusually dense, and that is deliberate. Several guards exist because a
previous, plausible theory was measured **wrong**, and the retracted theory is written down so
nobody re-derives it. That is the most valuable thing in the codebase — keep writing them.

What survives extraction, and what does not:

- ✅ *"Printing a secret to stdout re-stages it in any transcript that captures the run."*
- ❌ *"…which forced a rotation on <date>."*
- ✅ *"`-e` follows the link and is false for a dangling symlink; without `-L` the write follows it."*
- ❌ any hostname, host name, client name, or cluster address.

State the **claim and its scope**, not the anecdote.

## Evidence rules

These are the house style, and they are why the guards here are worth trusting:

- **A test you have not watched FAIL proves nothing.** A regression test must be shown red on
  pre-change code; report the matrix. A guard pinning an invariant the bug never violated is an
  *invariant guard* — label it as one, do not count it as regression coverage.
- **Validate the instrument before reading its verdict.** A reassuring zero is
  indistinguishable from a harness wired to nothing. Feed it a case that MUST produce a
  non-zero count, watch the number move, and report the pair.
- **Read the content, not the exit code.** Count the runner's own result lines.
- **A comment is a claim too.** When you close a hazard, update the comment describing it as
  open.
- **One measurement is not a general claim.** If behaviour depends on a dimension, measure at
  ≥2 points and name them.

## Layout

| path | what |
|---|---|
| `cairn` | the PYTHON client CLI and the ORACLE — sync, recall, search, validate, ls-entries, doctor, append, put, create |
| `lib/` | the Python reader: cache resolution, recall rendering, scope/ref resolution, doctor |
| `server/` | the pod: `server.py`, `Dockerfile`, `seed.sh`, `verify-byte-identity.sh` |
| `cmd/cairn-server`, `internal/api` | the Go port of the server (P1), stdlib-only — see below |
| `cmd/cairn`, `internal/client`, `internal/doctor` | the Go port of the CLIENT (P2), over the SAME `internal/report` the pod uses |
| `internal/report`, `internal/store` | the ONE renderer and the store loader, shared by pod and CLI |
| `tests/` | the suites, plus `leakscan.py`, `conformance/` (P1's gate) and `parity/` (P2's) |
| `flake.nix` | both clients, the server image, the Go server, and the checks over all of them |

## 🔴 TWO SERVERS ARE ALIVE, AND `server/server.py` IS THE ORACLE

`cmd/cairn-server` is the Go port. It is **not deployed by anything**: it exists so the
conformance corpus can be replayed against both implementations on the same store and
the difference MEASURED. The sequence is fixed — the Go server passes the corpus, then
both run over one store and byte-identity is compared, then the client is ported, then
Python is retired.

🔴 **STEPS ONE AND THREE ARE DONE; STEP TWO IS STILL NOT, AND IT IS NOT IMPLIED BY THE
OTHER TWO.** The corpus is green for both server implementations (step 1) and the Go
client is byte-identical to the Python one over `tests/parity/` (step 3) — but **no
dual-run of the two SERVERS over one store has been made**, which is the comparison
`server/verify-byte-identity.sh` exists for. Step 3 landing out of order is deliberate:
it closes the two-renderer window, and it says nothing about the servers agreeing on a
live store. Do not declare a step done early, and do not switch the deployed image on
the strength of a green corpus plus a green parity gate — the sentence here once read
"while the corpus is partial", which a green corpus would have satisfied while leaving
every remaining step untouched.

🔴 **AND THE BYTE-IDENTITY GATE IS SCOPED TO THE *UNCOMPRESSED* TAR, BECAUSE GZIP
IDENTITY IS UNATTAINABLE — MEASURED, NOT ASSUMED.** `/api/v1/snapshot` ships
`tarfile.open(mode="w:gz")` output on the oracle and `compress/gzip` output in Go, and the
two cannot be made equal at any setting. Two independent reasons, so closing one does not
help:

- **the 10-byte gzip header.** Go's `compress/gzip` hardcodes the OS byte to `0xff`
  (unknown) with no API to change it; CPython writes `0x03` (Unix). At level 9 the XFL
  bytes agree (`02`) and the OS bytes still differ.
- **the DEFLATE stream itself**, which differs in LENGTH and not merely in content —
  512 bytes from Go against 511 from zlib at level 9 on one 20,480-byte tar. `compress/flate`
  and zlib make different match and block choices; that is a permitted freedom of the
  format, not a defect in either.

So the gate compares **the tar inside the gzip**, which IS achievable: after the header
fixes in `internal/snapshot/paxtar.go`, the Go writer's archive was byte-identical to
CPython's `PAX_FORMAT` output over a member list carrying a whole second, `.25`, `.5` on an
even second, `.5` on an odd one, `.75`, a non-ASCII name and a name over 100 bytes. The
corpus already avoids the compressed bytes for the same reason — `wire.py` drops
`Content-Length` on `/snapshot` and compares the **extracted tree** — so do not add a gate
on the gzip bytes and do not read this row as "nobody looked".

⚠ **The extracted-tree comparison is ALSO what hid four header divergences**: every POSIX
reader prefers a PAX extended record over the ustar field, so a wrong `mtime` field, a
missing `path` record, a reordered record set and a moved checksum all normalise away before
the comparison happens. Byte-diff the two archives when you change that writer; the tree
comparison structurally cannot see it.

**The corpus is the specification, and it is now GREEN for both implementations.**
Measured on this tree: the Go server answers **116 PASS, 0 failing cases, 0 failing
relations, 4 rows skipped** as oracle-specific; the oracle answers **0 failures, 0
skipped**. At P1a the split was 94 PASS and 22 failing cases — every one of them a
`/api/v1/recall/{scope}` or `/api/v1/search/{scope}` **rendering** case, which is what
P1b closed. 🔴 **A GREEN CORPUS IS THE START OF THE BYTE-IDENTITY QUESTION, NOT THE END
OF IT** — see the two paragraphs below, and note that the next step in the sequence is
running both servers over ONE store and comparing, which the corpus does not do.

```bash
go vet ./... && go test ./...            # the port's own guards
tests/conformance/run_go.sh              # the P1 gate: the corpus against the Go server
python3 tests/conformance/suite.py run   # …and against the oracle, which must stay 0 failures
```

⚠ **A GREEN `run_go.sh` AND A GREEN `go test` ARE DIFFERENT CLAIMS, AND NEITHER IMPLIES THE
OTHER.** The corpus measures the SERVED HTTP contract over the bodies `requests.json`
declares; `go test` measures the renderer against the oracle's bytes over shapes the corpus
never sends (see the reader fixture below) and the decoding differences the corpus is
structurally blind to. Read both.

🔴 **A REFUSAL THAT IS "THE SAME" ON BOTH SERVERS MAY BE THE SAME FOR THE WRONG REASON.**
A relation between two responses that both fail their own golden still PASSES — it
compares them to each other, not to the contract — and at P1a `refused-equals-absent`
and `head-matches-get` did exactly that for the two report routes, because
`501 not-implemented` is beautifully uniform. Two things now stand against it, and the
order matters because the first was **measured insufficient**:

- the runner prints the caveat on the line itself. That annotates a PASS; it does not
  withhold one, so it is a comment competing with a verdict.
- 🔴 **the deterministic half, added at P1b: a relation with NO non-5xx member is a
  FAILURE.** Every 5xx this server emits is uniform *by design* — it names no scope — so
  two of them compare equal for free. `_any_real_answer` refuses to vouch for that set.
  The bar is **one** real answer and the floor is **500, not 400**: a 4xx refusal IS an
  answer, and the uniformity of those refusals is itself part of the contract.
  Watched to work rather than reasoned about — a build whose report routes answer 500
  produces `FAIL relation refused-equals-absent recall (every member answered 5xx …)`
  for all four report members while the `snapshot`/`append`/`replace` pairs still PASS.

🔴 **A GREEN CORPUS IS NOT A GREEN PORT, AND THAT IS MEASURED RATHER THAN CAUTIONARY.**
Two defects shipped in the first Go commit with all four CI jobs green and the split
exactly as documented above: a body with an invalid UTF-8 byte answered `200 appended`
and wrote a permanent U+FFFD into a curated entry (the oracle refuses it 400), and a
guard that could not tell a surrogate PAIR from a lone surrogate 400'd every astral
character — which is what `cairn append` sends, because `json.dumps` defaults to
`ensure_ascii=True`. Both were found by reading the port against `server.py` function by
function, not by the suite; the suite builds its bodies from `requests.json` and no row
carries either shape. **When the corpus is green, the question left is "what does it not
send", and DECODING differences are the answer.**

🔴 **AND THE RENDERER'S OWN GATE IS A DIFFERENTIAL FIXTURE, BECAUSE THE CORPUS CANNOT
SEND MOST OF WHAT IT RENDERS.** `internal/report/testdata/reader_fixtures.json` holds the
**oracle's own rendered bytes** for 50 cases over a 122-entry synthetic world, generated by
`tests/reader_fixtures.py` and replayed by `internal/report`'s tests. It exists because no
corpus row carries an openness marker, a near-miss marker, a `tasks:` key, a duplicate
heading, a fenced region, a scope over the 100-line index page, an ambiguous ref, a bare
entry, a present-but-EMPTY section, an entry with no `## What it is`, an honoured
`sensitivity:`, an exact mtime TIE, a name-only search hit, a sub-threshold near miss, a
fuzzy/prefix/substring match, a joined compound term, a `--max-hits` truncation, a malformed
entry BESIDE readable ones, a `search-unreadable` scope, or an EMPTY allowlist — and every
one of those is a branch.

```bash
python3 tests/reader_fixtures.py generate     # re-record from the oracle's reader
python3 tests/reader_fixtures.py print <case> # read one case's expected bytes
```

Three properties keep it honest, each the same shape as the HTTP corpus':
`tests/test_reader_fixtures.py` **regenerates and diffs** (a stale fixture is a failure, not
a weaker comparison); `TestTheFixtureCoversTheSHAPESTheCorpusCannotSend` is a **ledger over
the rendered output**, keyed on strings only each branch can produce, so the set cannot
shrink; and two tables measure the functions no rendered case can reach properly —
`difflib.SequenceMatcher.ratio()` over 20 pairs including the autojunk boundary, and
CPython's `round(x, 3)` over 19 values including `.xx5` boundaries. ⚠ **The fixture is in
`flake.nix`'s `onlyGo` filter for the same reason `requests.json` is** — leave it out and
the sandbox tier's `go test` goes red naming it, which is the good direction and still worth
saying.

🔴 **THE MUTATION BATTERY: 56 mutants over two targets, 52 KILLED, 4 SURVIVED — and every
survivor is LABELLED EQUIVALENT AT THE CODE, with the reasoning, because three of them
corrected a comment that was wrong.**

**46 over the RENDERER**, in three rounds: round 1 killed 24 of 40 and its 13 survivors are
what built the fixture above; round 2 killed 40 of 46; round 3 killed 43 of 46 and is clean.
The three correct survivors: `sort.SliceStable` → `sort.Slice` (the comparator is a total
order); `1e-9*nsec` → `nsec/1e9` (**measured bit-identical at every realistic mtime
magnitude** — the ULP of the sum dwarfs the difference, and the old comment claimed the
hazard was reachable); and disabling `difflib`'s extension loops (**with an empty junk set
the DP has already found the longest contiguous run, so neither loop can advance** — the old
comment said two of the four "can run", which is two more than can).

**10 over `store.ScopeRevision` AND THE WARNING SINK**, the two surfaces P1b added with one
branch of coverage each: 9 killed by the guard's own test, 1 labelled equivalent. That round
also found a defect in the new code — `readGitText` stripped the WHOLE file where the oracle
reads `packed-refs` unstripped, which removed the last line's trailing whitespace and made
the per-field strip unreachable from a fixture whose matching row came last.

🔴 **THE RENDERER IS A LIBRARY, NOT A HANDLER, BECAUSE P2 IMPORTS IT.** `internal/report`
holds `Recall`, `Search`, `RecallReport.RenderText`, `SearchReport.RenderText` and
`ExitFor` — plain values in, plain values out, no `net/http` type in any signature, no
server config, and every error classifiable with `errors.As`/`errors.Is`
(`store.StoreMissingError`, `store.EntryUnreadableError`). `report.Reader` is the thin
`Renderer` the pod hands to `internal/api` and the ONLY type in the package that knows a
server exists. **That shape is no longer a promise about P2: `internal/client` is the second
consumer, and it calls `Recall`/`Search`/`RenderText`/`ExitFor` directly rather than through
`Reader`.** Pod and CLI run ONE renderer, which is what makes byte-identity a property
rather than a discipline — rewriting the server alone would have left two renderers
agreeing forever by review.

⚠ **TWO PLACES THE PORTED READER DELIBERATELY DIFFERS FROM THE ORACLE, each recorded
because neither is visible to the corpus:**

| where | the difference | why |
|---|---|---|
| `store.ScopeRevision` on a `.git/HEAD` that is not valid UTF-8 | ONE `400` audit line here, **two** (`200` then `400`) on the oracle | there the strict decode raises while the response's arguments are being evaluated, after the 200 line is already written. Reproducing a mid-response raise to duplicate a log line is a worse trade than naming it |
| `store.ScopeRevision` resolving a `ref:` | `filepath.Join` CLEANS, so a `ref:` naming `../…` cannot climb out of the git dir; the oracle's `git / ref` can | a NARROWING, in the safe direction. A HEAD pointing outside its own repo is not a revision worth reporting |

✅ **AND ONE ROW OF THAT TABLE IS CLOSED, BY ITS OWN STATED CONDITION.**
`report.ErrFocusSelectorUnported` refused a non-empty focus window, on the stated grounds
that "the store API never sends one" — **true of the pod and FALSE of the CLI**, which is
what P2 is. `cairn recall` with no `--scope` and the default `--mode` builds a window out of
the repo's newest handoff doc and passes it straight through, so the refusal was reachable
from the commonest invocation of the commonest verb and a Go client could not answer
`recall` at all. The condition recorded for deleting it was "`associate_paths` ported with a
red-at-baseline differential test"; `store.AssociatePaths` is that port, seven
`digest-focus-*` rows in `reader_fixtures.json` carry the ORACLE's own rendered basis for it
(a filename-tier hit, an alias-tier hit, a miss, an ambiguous ref, the mtime tie-break, the
`…` truncation and the sourceless arm), and two more — `digest-focus-count-beats-mtime` and
`digest-focus-ref-is-the-last-resort` — exist because a mutation sweep found the ranking's
PRIMARY key and its LAST resort unreachable from the other seven. The error and its guard
are deleted together, as promised, rather than left as a branch no caller can reach.

🔴 **AND ONE DIVERGENCE IS OPEN RATHER THAN DELIBERATE: AN INTEGER QUERY PARAMETER WIDER
THAN `int64`.** `_int_param` is `int(v)`, which is arbitrary precision; `intParam` is
`strconv.Atoi`, which is not. **Measured live on both servers over one world**, not derived
from reading:

```
GET /api/v1/recall/alpha-notes?page=999999999999999999999
  oracle 200: INDEX (from index) — no entries: page 999999999999999999999 is past the end …
  go     400: bad request: page must be an integer, got '999999999999999999999'
GET /api/v1/recall/alpha-notes?limit=999999999999999999999
  oracle 200: INDEX (from index) — ALL 2 entries in `alpha-notes/`, none omitted …
  go     400: bad request: limit must be an integer, got '999999999999999999999'
```

It is a P1a-era parsing difference that only became OBSERVABLE at P1b, because before the
renderer existed both answers were refusals. `tests/conformance/` cannot see it — the runner
builds its targets from `requests.json` and no row carries a 21-digit parameter.
⚠ **It is recorded here and NOT fixed in the same change as the renderer**, deliberately:
widening `intParam`'s range is a change to the validation ladder, and P1b's whole claim is
that the ladder did not move. **Closing condition:** a `requests.json` row carrying a
21-digit `?page=`, regenerated against the oracle, and `run_go.sh` exiting 0 with it present
— which forces whoever lands it to decide between matching `int()` and declaring the
difference in `wire.NORMALIZATIONS`. The check is mechanical; the decision is not, which is
why it is a separate change.

🔴 **THE GO SIDE CARRIES ITS OWN ROUTE LEDGER, BECAUSE THE SUITE CANNOT BUILD ONE FOR
IT.** `cases.declared_routes` reads the oracle's dispatch tables by AST and has no
equivalent for a compiled binary, so the blind spot — a route added after the fixtures
were generated — is closed on the Go side by `api.DeclaredRoutes()`, checked against
`tests/conformance/requests.json` by `TestTheRouteLedgerMatchesTheConformanceCorpus`,
against the wiring at construction, and against the ledger's own spelling by
`checks.go-server-declares-its-routes` (which reads it out of the RUNNING binary).
Adding a row to a dispatch table is adding a public, internet-reachable endpoint; all
four of those have to move together.

🔴 **THE GO TOOLCHAIN IS PINNED, NOT INHERITED** — the same discipline as the
interpreter, and for the same reason. `go.mod` says 1.25, `flake.nix` uses
`buildGo125Module` (nixpkgs' default `go` is **1.26** against the pinned lock), and CI
pins `go-version: "1.25"`. Move all three together or not at all. ⚠ `buildGoModule`
with the compiler in `nativeBuildInputs` is a NO-OP for the pin: it uses the `go` from
its own scope, so the build fetched 1.26 while the derivation advertised 1.25.

🔴 **STDLIB ONLY.** `go.mod` has no `require` block and `flake.nix` passes
`vendorHash = null`; together those make a new dependency in the serving path a build
FAILURE rather than a silent addition.

## 🔴 TWO CLIENTS ARE ALIVE, `cairn` IS THE ORACLE, AND `tests/parity/` IS THE GATE

```bash
python3 tests/parity/harness.py              # the P2 gate: both clients, one cache root
python3 tests/parity/harness.py --self-test   # prove the differ can go RED
python3 tests/parity/harness.py --break-pod   # prove the PRE-FLIGHT refuses to vouch
```

`cmd/cairn` is the Go client. It exists for ONE reason: rewriting the server alone left **two
renderers in two languages that must agree byte-for-byte forever**, with drift arriving as "a
different order that reads as a stale cache" — no error, no missing entry. `internal/report`
is the one renderer; the pod reaches it through `report.Reader` and the CLI calls
`Recall`/`Search`/`RenderText`/`ExitFor` directly. **One renderer, three consumers** (pod, CLI,
a future UI) is the deliverable, not a second client.

**Measured on this tree: 81 cases, 82 PASS, 0 failures, 0 dead normalizations** — all nine
verbs, every output-shaping flag, every documented exit code, `--help` in four spellings, and
`cache-mtime-parity` on top.

🔴 **THE FIRST FULL RUN OF THAT GATE REPORTED 72 PASS / 0 FAIL AND MEASURED NOTHING.**
`SUBSYSTEM_STORE_TRUSTED_PROXIES=127.0.0.1/32`, copied from the conformance runner where it is
correct, told the server the loopback peer was a PROXY — so every DIRECT request was refused
`401 status=no-client-ip`, no cache was ever written, and every report rendered
`store-unreachable`. **Two clients failing identically compare equal.** It was found by reading
the pod's audit log, not by the green. Three controls now stand against it and they are three
different claims, so read all three:

| control | proves | refuses with |
|---|---|---|
| `PREFLIGHT status=… declared-entries=…` | the POD answers a non-empty snapshot for this token | exit **2** — "could not vouch", not "failed" |
| `CONTENT-FLOOR live-banner=… rendered-digest=…` | the CLIENTS reached a live fetch and a rendered digest | exit 2 |
| `--self-test` → `sabotaged=4 caught=4` | the differ goes red on stdout, on stderr, on the exit code **AND** on `exit+stdout` | exit 2 |

`--break-pod` is the negative control on the first of those: it reconfigures the pod into
exactly the state above and the harness must answer 2, not 82 PASS. ⚠ **Four sabotage rows,
not one**, because a `compare="exit"` row is STRUCTURALLY blind to stdout and stderr — one
control over "something went red" would vouch for a differ that had lost most of its
comparisons. 🔴 **And one of them REPLACES the Go argv rather than appending to it**: both
clients handle `--help` before anything else, so no extra argument changes either answer and an
append-only mechanism had NO control over the `exit+stdout` mode at all.

🔴 **THE GATE FOUND THREE DEFECTS NO EXISTING TEST OR GOLDEN COULD SEE, and each is worth
knowing because each is a class rather than a typo:**

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
single most common invocation there is. Four rows now cover it, under a THIRD comparison mode
that asserts the exit code AND that both sides put something on stdout, because a client that
printed nothing and exited 0 would pass an exit-only row while telling the reader nothing.

A fifth came out of the Go unit battery: a truncated gzip stream and an HTML error page
surface as the SAME `io.ErrUnexpectedEOF` out of `tar.Next`, so classifying on the error VALUE
called `<html>nope</html>` a truncated tar where the oracle says `did not return an archive`.
`gzipLayerError` records WHICH LAYER failed at the point it is known, and `validateGzipLayer`
streams the compressed body to `io.Discard` **under a limit** — decompressing into memory to
inspect it would make a decompression bomb a MEMORY bomb before either ceiling is consulted,
because the ceilings read headers a truncated stream never reaches.

🔴 **SIX RESIDUAL DIFFERENCES ARE DECLARED IN `tests/parity/README.md` RATHER THAN NORMALISED
AWAY, and the one worth repeating here is the mtime.** The oracle's `tarfile` carries a PAX
mtime as a Python **float** and `os.utime` writes it back, losing sub-microsecond precision
Go's exact decimal parse keeps (measured: `…236263` vs `…236300`, 37 ns). It is **measured
unable to matter**: both collapse to the same `float64`, which is the value
`report.pyMtime` computes and the index order is decided on, because one ULP is 238 ns at the
current epoch — 119/238/238/477 ns at four magnitudes (2000, now, 2038, 2100). The gate asserts
the doubles are identical **and** that the raw delta stays under that bound, so a change that
widened it fails there rather than reordering an index. The other five: argparse's usage text
(exit code compared, text not), `urllib`-vs-`net/http` failure tails, a reader error's exit
route (3 by contract on Go, 1 by traceback on the oracle), a `SyntaxError` in a runtime-loaded
module (impossible in a binary), and `ReadStamp`'s missing "is not text" arm.

🔴 **THE GO CLIENT CARRIES TWO LEDGERS OF ITS OWN, BECAUSE THE PYTHON-SIDE GATES ARE BLIND TO A
COMPILED PROGRAM.** `capability_ledger.cli_verbs_from_parser` asks the PYTHON argparse parser
what subcommands it has; `tests/test_cairn_doctor.py`'s exit-code ledger walks the `cairn`
script's AST. Neither has an equivalent for a binary, so a Go-only verb, a Go client that
silently LOST one, or a Go exit code colliding with `doctor`'s 10 would each leave those gates
green. `cairn -verbs` and `cairn -exit-codes` print the tables the dispatcher and the exit
model are built from, and they are read out of the RUNNING binary in **two tiers**:
`tests/test_go_client_ledgers.py` (the `go` CI job, which REFUSES on a skip — the `tests` job
has no toolchain, and a skip nobody counts is a pass) and
`checks.go-client-declares-its-verbs` (the `nix` job). Same shape as
`api.DeclaredRoutes()`/`cairn-server -routes`, and for the same reason.

🔴 **THE EXIT-CODE MODEL IS A PRINTED CONTRACT AND THE `{0, 9}` OVERLAP IS DELIBERATE.** Read
outcomes `0`/`3`/`4`/`5`, writes `6`/`7`/`8`/`9`, usage `2` — a third bucket, not a read — and
`doctor`'s own `0`/`9`/`10`. Both clients declare identical values, asserted from BOTH
discovered operand sets. Do not renumber the 9 to make it unique: it is unambiguous at every
call site (`doctor` never creates an entry, `create` never runs diagnostics), and renumbering
changes a contract the command PRINTS to remove a collision that was never a defect.

⚠ **WHAT THE GATE STRUCTURALLY CANNOT SEE**, listed in `tests/parity/README.md` and worth one
line here: concurrency (both clients take the same `flock`, but nothing runs them at the same
instant), real network failures beyond a connect refusal, a narrowed credential (the SERVER's
narrowing is the corpus's claim), and the `doctor` states no healthy world reaches.

## 🔴 THE MUTATION BATTERY OVER P2: 55 MUTANTS, 52 KILLED, 3 LABELLED EQUIVALENT AT THE CODE

Round 1 killed 44 of 51, and its findings are why there was a round 2 — **every one of them was a
hole in a guard rather than a defect in the code**, which is the useful direction. Round 3 added
four mutants over the `--help` path that round 2 had no code to mutate, and it is CLEAN: the three
survivors below are labelled equivalent, which is not a finding, so the ladder ends there.

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

## Installing and building with nix

```bash
nix run   github:ZacxDev/cairn -- doctor    # the client, without installing it
nix build github:ZacxDev/cairn#cairn        # the client
nix build github:ZacxDev/cairn#server-image # the pod image, as a loadable tarball
```

Consumers pin this flake as an input; that is the supported way to get a `cairn`
whose version cannot disagree with the code in it, because **the version is the
git revision** and is never written down by hand.

🔴 **THERE ARE NOW TWO CLIENTS, AND EVERY CLAIM BELOW SAYS WHICH ONE IT IS
ABOUT.** `cairn` (`packages.cairn`, and still `packages.default`) is the Python
client and the ORACLE; `cmd/cairn` (`packages.cairn-go`) is the Go port. The Go
client is a SECOND artefact during P2, not a replacement: nothing in `apps` or
`packages.default` points at it, because swapping them changes what
`nix run github:…/cairn` executes for every existing consumer — a cutover, not a
build. **The Python client, its `lib/` and its packaging are not deleted here**;
the plan retires Python at P8, after the gate below has held over real use.

🔴 **`lib/` MUST STAY BESIDE THE *PYTHON* CLIENT SCRIPT, AND `packages.cairn` IS
BUILT THAT WAY ON PURPOSE.** `cairn` finds its modules with
`Path(__file__).resolve().parent / "lib"`. `.resolve()` follows symlinks, so
what matters is the directory holding the REAL file — the package therefore
installs the script and `lib/` together under `libexec` and puts a wrapper in
`bin/`. Do not "simplify" this by exporting `PYTHONPATH`: that makes the modules
reachable by a second mechanism which shadows the first, leaving the file's own
stated one dead and the next layout change silently broken.
`packages.cairn` fails its own install check if the client cannot import its
own `lib/`, so a broken client cannot be built at all — and that check is what
catches a missing `lib/`, because the imports are at module scope and `--help`
therefore dies too. `checks.client-resolves-its-lib` earns its place on the
other side: it runs a real subcommand to completion against a real cache root,
which is behaviour the install check does not exercise.

🔴 **NONE OF THAT PARAGRAPH APPLIES TO THE GO CLIENT, AND SAYING SO IS THE POINT
RATHER THAN LEAVING IT TO BE INFERRED.** A single binary has no sibling module
directory, no import path, and no install check about one — so the two hazards
the paragraph above exists for (a lost `lib/`, a second resolution mechanism
shadowing the stated one) cannot exist there. What `packages.cairn-go` has
instead is a `gitMinimal` on its wrapper's `PATH`, for exactly the reason
`packages.cairn` has one: `internal/client/reposcope.go` invokes `git` by BARE
NAME to derive a repo's scope, so a package that did not carry its own answer
would make `cairn recall` with no `--scope` fail differently on two machines.
⚠ **Do not read the `lib/` rule as retired.** It governs the client that is
still shipped and still the oracle; it stops governing anything on the day
`packages.cairn` does, which is P8.

⚠ **`checks.client-resolves-its-lib` runs in a nix sandbox, and a sandbox pins
dimensions.** Its HOME has no cache root, which is exactly why it did not
notice that `cairn doctor` crashed on any host that HAD one
(`AttributeError: 'NoneType' object has no attribute 'iterdir'`, zero stdout,
exit 1, whenever `CAIRN_MIRROR_ROOT` was unset — the default). Ask what your
sandbox cannot have before reading its green as coverage.

⚠ **AND `checks.go-client-declares-its-verbs` HAS THE SAME SHAPE OF BLINDNESS,
NAMED HERE BECAUSE IT IS NEW.** It has no store, no token, no network and no
HOME with a cache root, so it exercises the two LEDGERS and nothing about
reading or writing — the `AttributeError` above is exactly the class of defect it
cannot see. The parity gate is what measures behaviour, and it needs a running
pod that a nix sandbox is the wrong place for.

🔴 **THERE ARE TWO WAYS TO BUILD THE POD AND THEY MUST NOT DIVERGE.**
`server/Dockerfile` is what is deployed today; `packages.server-image` is the
reproducible alternative. The runtime contract — env, port, uid, entrypoint — is
written in both, so `tests/test_flake_image_matches_dockerfile.py` pins them
against each other and goes red when one moves alone. Change one, change the
other, in the same commit. The module set is deliberately *not* duplicated: the
Dockerfile enumerates its `COPY`s (kept honest by
`test_the_image_copies_every_module_it_needs`) while the flake copies all of
`lib/`, so there is nothing there for the two to disagree about.

🔴 **AND THE AGREEMENT TEST IS NARROWER THAN "THE TWO IMAGES ARE THE SAME" —
KNOW WHAT IT DOES NOT SEE.** It pins env, uid, port and entrypoint. It is
structurally blind to LAYER CONTENTS, and that blindness has already cost
something real: the first version of `packages.server-image` shipped the code
alone, so the image had **no `PATH` and no `sh`/`tar`/`find`/`cut`** while all
four pinned values agreed. It started, passed health checks and served — and
every documented operation against it failed, because `server/seed.sh` seeds
through `kubectl exec … -- tar -xf -` and `server/README.md`'s token
revocation is `kubectl exec … -- sh -c 'kill -HUP 1'`. **A pod that cannot be
seeded and whose leaked credential cannot be revoked**, behind four green
assertions. The image now carries busybox and declares `PATH`, and
`test_the_flake_image_declares_a_PATH_and_carries_the_operational_toolchain`
pins that — but the general lesson stands: **before swapping the deployed
image, diff the two for what the test cannot read.**

Known remaining differences, measured on the built images (26 layers,
209,252,641 bytes):

| | `server/Dockerfile` | `packages.server-image` |
|---|---|---|
| `/etc`, `/usr` | present | **absent** |
| `WorkingDir` | `/` | `/app` |
| shell / `tar` / `find` / `cut` | from `python:3.12-slim` | busybox 1.37.0 |
| `bash`, `apt-get` | **present** | absent |
| `wget`, `nc`, `httpd`, `telnetd` | **absent** | **present** (busybox applets) |
| size | smaller | larger |

🔴 **NEITHER IMAGE'S TOOL SURFACE IS A SUBSET OF THE OTHER'S, and the row that
matters is the `wget`/`nc`/`httpd`/`telnetd` one** — not the size row below it.
busybox ships 402 applets at `/bin` (with `/sbin` a SYMLINK to it, so one
directory, not two), which puts **four network servers** — `httpd`, `telnetd`,
`ftpd`, `tftpd` — and a set of network clients — `wget`, `nc`, `telnet`,
`ftpget`, `ftpput`, `tftp`, `nslookup`, `ping`, `traceroute`, `nbd-client`,
`udhcpc`, `ntpd`, `rdate`, and notably **`ssl_client`** — into a pod that mounts
a credential at `/run/secrets/subsystem-store/token`, none of which the deployed
image has. ⚠ Two earlier drafts of this sentence undercounted, each in the
direction of the previous fix ("two egress clients", then "an HTTP server, a
telnet server"); enumerate from `busybox --list` on the built image rather than
from this paragraph, because a reader told to "revisit the trade with the threat
model in front of you" needs the real set and `ssl_client` is the one that
matters for a token. Against that: the pod runs as uid 65532, no
applet is setuid, and the deployed image ships `bash`, `apt-get` and **8 setuid
binaries including `su` and `passwd`** — so neither is meaningfully "hardened"
relative to the other. **This is recorded rather than fixed, deliberately** —
trimming means `pkgs.busybox.override { extraConfig = "CONFIG_HTTPD n\n…"; }`,
which rebuilds busybox from source with no cache hit, and the applets are not
reachable without execution the attacker would already need. If this image is
ever actually deployed, revisit that trade **then**, with the threat model in
front of you; do not read this row as settled.

🔴 **THE INTERPRETER IS PINNED, NOT INHERITED.** `flake.nix` uses
`pkgs.python312` because `server/Dockerfile` is `python:3.12-slim` and CI pins
`python-version: "3.12"`. A bare `pkgs.python3` followed nixpkgs to 3.14 and
shipped an interpreter **nothing in this repo had ever run the suite under** —
and the suite already emits a 3.14 tar-extraction `DeprecationWarning`, so the
gap was behaviourally live. Move all three together or not at all.

## Naming

The project is **cairn**. Some identifiers still read `subsystem_store` / `SUBSYSTEM_STORE_*`
— these are **accepted aliases**, kept so existing deployments do not need a coordinated
cutover. New names should use `cairn` / `CAIRN_*`; do not mass-rename the aliases away without
a migration path for people already running this.
