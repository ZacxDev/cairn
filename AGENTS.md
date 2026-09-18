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

🔴 **`tests/leakscan.py` runs in CI on every commit AND IT DOES NOT COVER ALL FIVE — know
which.** Credentials, private IPs and dated incident references are gated in GENERAL.
Hostnames and project/scope names are gated only for a CLOSED SET of digests
(`denied-identifier`): the names a scrub already removed cannot come back, a NEW one is
invisible until you add it. Captured text is gated by NOTHING and cannot be; that bullet is
yours. ⚠ This line used to read "enforces this" while the scanner's own docstring said it
deliberately policed neither names nor dates — two bullets with no gate, and the most-read
file in the repo claiming otherwise. Run it before you push:

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

🔴 **AND THIS FILE HAS A BYTE BUDGET, ENFORCED RATHER THAN REQUESTED.** `CLAUDE.md` imports it, so
every session pays both before doing anything — and it went 10,617 → 41,598 B across four merges in
one session, which is what the prose rule above achieves on its own.
`tests/test_agent_instructions_weight.py` owns the ceiling and the working margin and prints WHAT to
evict and WHERE on failure. Never satisfy it by deleting a claim or narrowing a rule: history goes
to `tests/parity/README.md` or `tests/conformance/README.md`, which are read on demand and therefore
free.

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
| `internal/control` | P3: the ONE authz predicate the pod now authorises from, and `tokenfile/` (the token file, projected); 📄 its own README |
| `internal/identity` | P4: the ONE `Authenticator` (🔴 one backend BYPASSES auth on a DIRECTLY-reached pod; never default, refuses to start); 📄 its own README |
| `tests/` | the suites, `leakscan.py`, `conformance/`+`dualrun/` (P1's gates), `parity/` (P2's) |
| `flake.nix` | both clients (**default = the GO one**), BOTH pod images, and the checks over them |

## 🔴 TWO SERVERS ARE ALIVE, AND `server/server.py` IS THE ORACLE

`cmd/cairn-server` is the Go port. It is **not deployed by anything**: it exists so the
conformance corpus can be replayed against both implementations on the same store and
the difference MEASURED. The sequence is fixed — the Go server passes the corpus, then
both run over one store and byte-identity is compared, then the client is ported, then
Python is retired.

📄 **P1'S HISTORY LIVES IN `tests/conformance/README.md` AND `tests/dualrun/README.md`,
NOT HERE**: the measured corpus splits, the reader fixture's case list and its ancillary
tables, the mutation batteries, the two deliberate reader divergences and their
reasoning, the closed `ErrFocusSelectorUnported` row, the `?page=<21 digits>` worked
example, the gzip/DEFLATE measurements and the PAX-header story. Records of rounds, read
on demand. What is below is what binds the next edit.

✅ **ALL THREE STEPS ARE DONE, AND STEP TWO IS `tests/dualrun/`** — both servers over ONE
store; every route, scope, entry and principal compared, uncompressed tar included, 0
differences. 🔴 Do not declare a step done early: this sentence once read "while the
corpus is partial", which a green corpus would have satisfied while leaving every step
untouched. ⚠ It licenses the image cutover's PRECONDITION, not the cutover — **diff the
two images** for what the agreement test cannot read; this gate reads neither image.

🔴 **AND THE BYTE-IDENTITY GATE IS SCOPED TO THE *UNCOMPRESSED* TAR, BECAUSE GZIP IDENTITY
IS UNATTAINABLE — MEASURED, NOT ASSUMED.** Two independent reasons (the gzip header's OS
byte, and the DEFLATE stream's LENGTH), so closing one does not help. **Do not add a gate
on the gzip bytes, and do not read this row as "nobody looked"** — `wire.py` drops
`Content-Length` on `/snapshot` and compares the extracted tree for the same reason.
⚠ **But that extracted-tree comparison is ALSO what HID four PAX header divergences**:
every POSIX reader prefers an extended record over the ustar field it shadows, so a wrong
`mtime`, a missing `path` record, a reordered record set and a moved checksum all
normalise away before the comparison happens. **Byte-diff the two archives when you change
`internal/snapshot/paxtar.go`** — the tree comparison structurally cannot see it.

**The corpus is the specification, and it is GREEN for both implementations** — the Go
server with four rows skipped as oracle-specific, the oracle with none skipped and no
failures. 🔴 **A GREEN CORPUS IS THE START OF THE BYTE-IDENTITY QUESTION, NOT THE END OF
IT** — it replays a declared list against a declared world; `tests/dualrun/` is what
compares the two servers over a store nobody wrote a fixture for.

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

🔴 **A REFUSAL THAT IS "THE SAME" ON BOTH SERVERS MAY BE THE SAME FOR THE WRONG REASON.** A
relation between two responses that both fail their own golden still PASSES — it compares
them to each other, not to the contract. So **a relation with NO non-5xx member is a
FAILURE**: the bar is **one** real answer and the floor is **500, not 400**, because a 4xx
refusal IS an answer and the uniformity of those refusals is itself part of the contract,
where every 5xx this server emits is uniform *by design* and two of them compare equal for
free. A printed caveat was tried first and **measured insufficient** — it annotates a PASS
rather than withholding one.

🔴 **A GREEN CORPUS IS NOT A GREEN PORT, AND THAT IS MEASURED RATHER THAN CAUTIONARY.** Two
defects shipped in the first Go commit with all four CI jobs green and the corpus split
exactly as documented — both DECODING differences, both found by reading the port against
`server.py` function by function rather than by the suite, which builds its bodies from
`requests.json` and carries neither shape. **When the corpus is green, the question left is
"what does it not send", and DECODING differences are the answer.**

🔴 **AND THE RENDERER'S OWN GATE IS A DIFFERENTIAL FIXTURE, BECAUSE THE CORPUS CANNOT SEND
MOST OF WHAT IT RENDERS.** `internal/report/testdata/reader_fixtures.json` holds the
**oracle's own rendered bytes** over a synthetic world, generated by
`tests/reader_fixtures.py` and replayed by `internal/report`'s tests, for the openness and
near-miss markers, the `tasks:` key, the ambiguous ref, the malformed entry BESIDE readable
ones, the EMPTY allowlist and every other branch no corpus row carries. Three rules bind
anyone touching it: **regenerate and diff** rather than hand-edit (a stale fixture is a
failure, not a weaker comparison); the coverage ledger is keyed on strings only each branch
can produce, so **the covered set cannot shrink**; and it stays in `flake.nix`'s `onlyGo`
filter for the same reason `requests.json` is.

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

⚠ **TWO PLACES THE PORTED READER DELIBERATELY DIFFERS FROM THE ORACLE**, both in
`store.ScopeRevision`, both invisible to the corpus, and neither a defect: a `.git/HEAD`
that is not valid UTF-8 logs **one** `400` audit line here against **two** (`200` then
`400`) on the oracle; and a `ref:` naming `../…` cannot climb out of the git dir here,
where the oracle's `git / ref` can — a NARROWING, in the safe direction.

🔴 **AND ONE DIVERGENCE IS OPEN RATHER THAN DELIBERATE: AN INTEGER QUERY PARAMETER WIDER
THAN `int64`.** `_int_param` is `int(v)`, which is arbitrary precision; `intParam` is
`strconv.Atoi`, which is not — so a 21-digit `?page=` or `?limit=` is answered **200 by the
oracle and 400 by Go**, measured live on both servers over one world. `tests/conformance/`
cannot see it: the runner builds its targets from `requests.json` and no row carries that
shape. ⚠ It is NOT fixed in the same change as the renderer, deliberately. **Closing
condition:** a `requests.json` row carrying a 21-digit `?page=`, regenerated against the
oracle, and `run_go.sh` exiting 0 with it present — which forces whoever lands it to decide
between matching `int()` and declaring the difference in `wire.NORMALIZATIONS`. The check is
mechanical; the decision is not, which is why it is a separate change.

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

`cmd/cairn` is the Go client; `internal/report` is the one renderer, reached through
`report.Reader` by the pod and by direct `Recall`/`Search`/`RenderText`/`ExitFor` calls by the CLI.

🔴 **THE PREMISE, AT THE SCOPE IT ACTUALLY CARRIES.** Rewriting the server alone left **two
renderers in two languages that must agree byte-for-byte forever**, drift arriving as "a different
order that reads as a stale cache" — no error, no missing entry. That is true, and it entails
**deleting the PYTHON RENDERER**; it does **not** entail a full CLI port, which a Python CLI over a
small Go renderer would also have satisfied. What carries the port is the reason to state instead:
**a single binary, one language, and Python RETIRABLE at P8**. ⚠ And **"one renderer" is a P8
property, not a P2 one** — the Python renderer still ships as `packages.cairn` after the cutover,
so until P8 the gate below IS the comparison rather than the absence of one.

**Measured on this tree: 97 cases, 98 PASS, 0 failures, 0 dead normalizations** — all nine verbs,
every output-shaping flag, every documented exit code, `--help` in four spellings, the
argument-shape rules below, a TWO-INSTANCE `routes --check` and a routed `put` against a second
pod, and `cache-mtime-parity` on top. 🔴 **THAT IS NOT 97 BYTE DIFFS: 66 rows compare stdout,
stderr AND the exit code; 23 compare the exit code ONLY; 8 compare the exit code plus "both sides
put something on stdout" — so 31 of 97 never compare output text.** Declared per row and in the
residual table; it is the HEADLINE that reads wider than the gate, so know which rows are
load-bearing before trusting one.

🔴 **ITS FIRST FULL RUN REPORTED 72 PASS / 0 FAIL AND MEASURED NOTHING** — a
`SUBSYSTEM_STORE_TRUSTED_PROXIES` value copied from the conformance runner made the pod refuse every
direct request, so both clients rendered `store-unreachable` and **two clients failing identically
compare equal**. Three controls now stand against that, they are three different claims, and each
refuses with exit **2** ("could not vouch", never "failed"): `PREFLIGHT` (the POD answers a
non-empty snapshot for this token), `CONTENT-FLOOR` (the CLIENTS reached a live fetch and a rendered
digest), and `--self-test` → `sabotaged=4 caught=4` (the differ goes red on stdout, on stderr, on
the exit code **and** on `exit+stdout`). `--break-pod` is the negative control on the first. Read
all three; a green without them is a green about nothing.

📄 **READ ON DEMAND RATHER THAN HERE, all in `tests/parity/README.md`:** what the gate FOUND
(ten divergences in seven findings), the MUTATION BATTERY over P2 (61 mutants, 58 killed, 3 labelled
EQUIVALENT at the code), and the **P8 RETIREMENT LEDGER** — every file, guard and row that exists
only while the Python oracle does. Those are records of rounds, not decision input before acting,
and they were moved out of this file when it had quadrupled in one session; a survivor's authority
is the label beside the code it labels. This file states the retirement condition for the `lib/`
rule and for nothing else, which is what the ledger is for — and the ledger is a list of DECISIONS,
not a delete script.

🔴 **SEVEN RESIDUALS ARE DECLARED IN `tests/parity/README.md` RATHER THAN NORMALISED AWAY, AND
THE RULING IS THE SAME EVERY TIME: declare it, never mirror it into the oracle.** Two are traps:

- **the sub-microsecond mtime** is **measured unable to matter** — both sides collapse to the same
  `float64`, which is what the index order is decided on, and `cache-mtime-parity` pins the doubles
  equal AND the raw delta under one ULP. Do not "fix" it, and do not read it as unmeasured.
- **`cairn -verbs` / `cairn -exit-codes`** exit **0** with a table on stdout on the Go client and
  **2** with argparse's `usage:` on the oracle — the Go client *succeeding* where the oracle
  refuses, which is the dangerous direction. Declared rather than closed because a printed table on
  the oracle would be a second mechanism reaching a value the Python ledgers already read from the
  parser and the AST. 🔴 **THE CUTOVER HAS LANDED, SO THIS IS NOW A LIVE WIDENING OF THE CLI
  CONTRACT, NOT A PENDING ONE**: `nix run github:…/cairn -verbs` used to be REFUSED and now answers
  0. Intended and declared; `#cairn` is the unchanged path. P8 owns the deletion, not this.

The other five: argparse's usage text (exit code compared, text not), `urllib`-vs-`net/http` failure
tails, a reader error's exit route (3 by contract on Go, 1 by traceback on the oracle), and two
oracle-only failure modes a single binary cannot have.

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

## 🔴 SEVERAL INSTANCES: AN UNROUTED SCOPE REFUSES

Binding rules: **`lib/README.md`**. Read it before editing any routing path.

## Installing and building with nix

```bash
nix run   github:ZacxDev/cairn -- doctor       # the DEFAULT client — the GO one — uninstalled
nix build github:ZacxDev/cairn#cairn           # the PYTHON client, still the oracle
nix build github:ZacxDev/cairn#cairn-go        # the Go client, by name
nix build github:ZacxDev/cairn#server-image    # the PYTHON pod image, as a loadable tarball
nix build github:ZacxDev/cairn#server-image-go # the GO pod image — published by nothing
```

Consumers pin this flake as an input; that is the supported way to get a `cairn`
whose version cannot disagree with the code in it, because **the version is the
git revision** and is never written down by hand.

🔴 **THERE ARE NOW TWO CLIENTS, AND EVERY CLAIM BELOW SAYS WHICH ONE IT IS
ABOUT.** `cmd/cairn` (`packages.cairn-go`) is the Go port and it is **`packages.default`
and `apps.default`** — the cutover has landed, so `nix run github:…/cairn` executes the
Go client for every consumer who does not name one. `cairn` (`packages.cairn`,
`apps.cairn`) is the Python client and STILL THE ORACLE: neither it, its `lib/`, nor its
packaging is deleted here, and P8 is what retires them. ⚠ **The cutover WIDENED the CLI
contract** — see the `-verbs` residual above; that is declared, not a defect.

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

🔴 **THERE ARE TWO WAYS TO BUILD THE *PYTHON* POD AND THEY MUST NOT DIVERGE.**
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

📄 **THE MEASURED DIFFERENCES ARE IN `server/README.md`, NOT HERE** — the layer
table, the busybox applet enumeration (402 applets, **six** network servers and
`ssl_client` beside a mounted credential), the setuid counts, and why that trade
is RECORDED RATHER THAN FIXED. **THREE successive drafts of that count were
wrong, each in the direction of the previous fix, and the third was wrong while
telling the reader to enumerate**: `busybox --list` on the built image, never a
paragraph. Neither image's tool surface is a subset of the other's, and nothing
about it is settled.

🔴 **AND THERE IS A THIRD IMAGE: `packages.server-image-go`, WHICH IS A THIRD
BUILD AND NOT A THIRD STATEMENT OF THE CONTRACT.** It wraps `cmd/cairn-server`
and derives uid, port, exposed port and env from the same
`serverUid`/`serverPort`/`serverEnv` bindings the pin above reads, minus a named
`serverEnvPythonOnly` set — CPython's two knobs and a `HOME` that no package in
the Go server's import closure reads. A COPY is the hazard; a subtraction is what
makes a variable added for one pod reach both.
`tests/test_flake_go_image_runtime_contract.py` pins that it stays derived, that
it carries busybox and a `PATH`, and that its CA bundle is NAMED rather than
merely present. 🔴 **NOTHING PUBLISHES OR DEPLOYS IT.**
`.github/workflows/publish-image.yml` publishes `packages.server-image` — the
PYTHON pod — and the cutover of the DEPLOYED image is a separate decision.

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
