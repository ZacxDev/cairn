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
yours. ⚠ This line once claimed the scanner enforced all five while it enforced three; the
record of that, and why neither rule is a heuristic, is `tests/leakscan.py`'s own docstring.
Run it before you push:

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
every session pays both before doing anything, and the prose rule above did not stop it quadrupling
in one session (the measured curve is in the test).
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
| `cmd/cairn-ui`, `internal/ui` | P-A/B/C: the BROWSER surface — pages, gomponents, COOKIE SESSIONS, the SHARE FLOW, deployed by nothing; 📄 its own README |
| `internal/depspolicy` | the ALLOWLIST and import BAN that replaced `vendorHash = null` |
| `internal/control` | P3: the ONE authz predicate the pod now authorises from, and `tokenfile/` (the token file, projected); 📄 its own README |
| `internal/identity` | P4: the ONE `Authenticator` (🔴 one backend BYPASSES auth on a DIRECTLY-reached pod; never default, refuses to start); 📄 its own README |
| `tests/` | the suites, `leakscan.py`, `conformance/`+`dualrun/` (P1's gates), `parity/` (P2's) |
| `flake.nix` | both clients (**`default` is the GO one; `#cairn` is the Python one**), BOTH pod images, the UI, and the checks |

## 🔴 TWO SERVERS ARE ALIVE, AND `server/server.py` IS THE ORACLE

`cmd/cairn-server` is the Go port. It is **not deployed by anything**: it exists so the
conformance corpus can be replayed against both implementations on the same store and
the difference MEASURED.

📄 **P1'S HISTORY LIVES IN `tests/conformance/README.md` AND `tests/dualrun/README.md`,
NOT HERE**: the measured corpus splits, the reader fixture's case list and its ancillary
tables, the mutation batteries, the two reader divergences' reasoning, the closed
`ErrFocusSelectorUnported` row, the `?page=<21 digits>` worked example, the gzip/DEFLATE
measurements and the PAX-header story. Records of rounds, read on demand. What is below
is what binds the next edit.

✅ **ALL THREE STEPS ARE DONE, AND STEP TWO IS `tests/dualrun/`** — both servers over ONE
store; every route, scope, entry and principal compared, uncompressed tar included, 0
differences. 🔴 Do not declare a step done early; a sentence about the CORPUS is satisfied
by a green corpus while every step stands untouched. ⚠ It licenses the image cutover's
PRECONDITION, not the cutover — **diff the two images** for what the agreement test
cannot read; this gate reads neither image.

🔴 **AND THE BYTE-IDENTITY GATE IS SCOPED TO THE *UNCOMPRESSED* TAR, BECAUSE GZIP IDENTITY
IS UNATTAINABLE — MEASURED, NOT ASSUMED.** Two independent reasons, so closing one does
not help. **Do not add a gate on the gzip bytes, and do not read this row as "nobody
looked"** — `wire.py` drops `Content-Length` on `/snapshot` and compares the extracted
tree for the same reason. ⚠ **But an extracted-tree comparison is ALSO what HID four PAX
header divergences**, because every POSIX reader prefers an extended record over the
ustar field it shadows. **Byte-diff the two archives when you change
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

🔴 **A REFUSAL THAT IS "THE SAME" ON BOTH SERVERS MAY BE THE SAME FOR THE WRONG REASON** —
two responses that both fail their own golden still satisfy a relation BETWEEN them. So **a
relation with NO non-5xx member is a FAILURE**: the bar is **one** real answer and the floor
is **500, not 400**, because a 4xx refusal IS an answer and the uniformity of those refusals
is itself part of the contract, where every 5xx this server emits is uniform *by design*. A
printed caveat was tried first and **measured insufficient** — it annotates a PASS rather
than withholding one.

🔴 **A GREEN CORPUS IS NOT A GREEN PORT, AND THAT IS MEASURED RATHER THAN CAUTIONARY** — two
defects shipped in the first Go commit with all four CI jobs green, both DECODING
differences, both found by reading the port against `server.py` function by function rather
than by the suite. **When the corpus is green, the question left is "what does it not
send", and DECODING differences are the answer.**

🔴 **AND THE RENDERER'S OWN GATE IS A DIFFERENTIAL FIXTURE, BECAUSE THE CORPUS CANNOT SEND
MOST OF WHAT IT RENDERS.** `internal/report/testdata/reader_fixtures.json` holds the
**oracle's own rendered bytes** over a synthetic world, generated by
`tests/reader_fixtures.py` and replayed by `internal/report`'s tests. Three rules bind
anyone touching it: **regenerate and diff** rather than hand-edit (a stale fixture is a
failure, not a weaker comparison); the coverage ledger is keyed on strings only each branch
can produce, so **the covered set cannot shrink**; and it stays in `flake.nix`'s `onlyGo`
filter for the same reason `requests.json` is.

🔴 **THE RENDERER IS A LIBRARY, NOT A HANDLER, BECAUSE P2 IMPORTS IT.** `internal/report`
holds `Recall`, `Search`, `RecallReport.RenderText`, `SearchReport.RenderText` and
`ExitFor` — plain values in, plain values out, no `net/http` type in any signature, no
server config (the instance is a plain string; `""` is the pod's), and every error
classifiable with `errors.As`/`errors.Is`
(`store.StoreMissingError`, `store.EntryUnreadableError`). `report.Reader` is the thin
`Renderer` the pod hands to `internal/api` and the ONLY type in the package that knows a
server exists. **That shape is no longer a promise about P2: `internal/client` is the second
consumer, and it calls `Recall`/`Search`/`RenderText`/`ExitFor` directly rather than through
`Reader`.** Pod and CLI run ONE renderer, which is what makes byte-identity a property
rather than a discipline.

⚠ **TWO PLACES THE PORTED READER DELIBERATELY DIFFERS FROM THE ORACLE**, both in
`store.ScopeRevision`, both invisible to the corpus, neither a defect, and named here
because a reader comparing two audit streams has to know they are there: a `.git/HEAD`
that is not valid UTF-8 logs **one** `400` audit line here against **two** (`200` then
`400`) on the oracle; and a `ref:` naming `../…` cannot climb out of the git dir here,
where the oracle's `git / ref` can — a NARROWING, in the safe direction. Why each was
accepted: `tests/conformance/README.md`.

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
equivalent for a compiled binary, so that blind spot is closed on the Go side by
`api.DeclaredRoutes()`, checked against `tests/conformance/requests.json` by
`TestTheRouteLedgerMatchesTheConformanceCorpus`, against the wiring at construction, and
against the ledger's own spelling by `checks.go-server-declares-its-routes` (which reads it
out of the RUNNING binary). Adding a row to a dispatch table is adding a public,
internet-reachable endpoint; all four of those have to move together.

🔴 **THE GO TOOLCHAIN IS PINNED, NOT INHERITED** — the same discipline as the
interpreter, and for the same reason. `go.mod` says 1.25, `flake.nix` uses
`buildGo125Module` (nixpkgs' default `go` is **1.26** against the pinned lock), and CI
pins `go-version: "1.25"`. Move all three together or not at all. ⚠ `buildGoModule`
with the compiler in `nativeBuildInputs` is a NO-OP for the pin: it uses the `go` from
its own scope, so the build fetched 1.26 while the derivation advertised 1.25.

🔴 **THE SERVING PATH IS STILL STDLIB-ONLY — the import BAN measures it, not
`vendorHash = null`, which is gone. See the dependency section below.**

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
order that reads as a stale cache" — no error, no missing entry. That entails **deleting the
PYTHON RENDERER**; it does **not** entail a full CLI port. What carries the port is the reason to
state instead: **a single binary, one language, and Python RETIRABLE at P8**. ⚠ And **"one
renderer" is a P8 property, not a P2 one, AND THE DEFAULT FLIP DID NOT MAKE IT ONE** — the Python
renderer still ships as `packages.cairn` until the oracle is deleted, so until then this gate IS
the comparison rather than the absence of one.

**Measured on this tree: 101 cases, 102 PASS, 0 failures, 0 dead normalizations** — every
verb, every output-shaping flag, every documented exit code, `--help` in four spellings, the
argument-shape rules, a TWO-INSTANCE `routes --check`, a routed `put` and four routed READS
against a second pod, and `cache-mtime-parity` on top. 🔴 **THAT IS NOT 101 BYTE DIFFS: 70 rows
compare stdout, stderr AND the exit code; 23 compare the exit code ONLY; 8 compare the exit code
plus "both sides put something on stdout" — so 31 of 101 never compare output text.** Declared
per row and in the residual table; it is the HEADLINE that reads wider than the gate, so know
which rows are load-bearing before trusting one.

🔴 **TWO CLIENTS FAILING IDENTICALLY COMPARE EQUAL** — its first full run reported 72 PASS / 0
FAIL against a pod that refused every request. Three controls stand against that, they are three
different claims, and each refuses with exit **2** ("could not vouch", never "failed"):
`PREFLIGHT` (the POD answers a non-empty snapshot for this token), `CONTENT-FLOOR` (the CLIENTS
reached a live fetch and a rendered digest), and `--self-test` → `sabotaged=4 caught=4` (the differ
goes red on stdout, on stderr, on the exit code **and** on `exit+stdout`). `--break-pod` is the
negative control on the first. Read all three; a green without them is a green about nothing.

📄 **READ ON DEMAND RATHER THAN HERE, all in `tests/parity/README.md`:** the worked example behind
that green, what the gate FOUND (ten divergences in seven findings), the MUTATION BATTERY over P2,
and the **P8 RETIREMENT LEDGER** — every file, guard and row that exists only while the Python
oracle does. Records of rounds, not decision input before acting. This file states the retirement
condition for the `lib/` rule and for nothing else; the ledger is a list of DECISIONS, not a delete
script.

🔴 **SEVEN RESIDUALS ARE DECLARED IN `tests/parity/README.md` RATHER THAN NORMALISED AWAY, AND
THE RULING IS THE SAME EVERY TIME: declare it, never mirror it into the oracle.** Two are traps:

- **the sub-microsecond mtime** is **measured unable to matter** — both sides collapse to the same
  `float64`, which is what the index order is decided on, and `cache-mtime-parity` pins the doubles
  equal AND the raw delta under one ULP. Do not "fix" it, and do not read it as unmeasured.
- **`cairn -verbs` / `cairn -exit-codes`** exit **0** with a table on stdout on the Go client and
  **2** with argparse's `usage:` on the oracle — the Go client *succeeding* where the oracle
  refuses, which is the dangerous direction. 🔴 **THAT WIDENING HAS HAPPENED — the default flipped,
  so `nix run github:…/cairn -verbs`, refused at exit 2 before, answers 0 for anyone not naming
  `#cairn`.** Announced in `README.md`. P8 still owns the DECISION (public surface, or gated),
  never the moment.

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

⚠ **THE GATE HAS A NAMED BLIND SET — concurrency, real network failures, a narrowed
credential, unreached `doctor` states, and multi-instance worlds beyond two.** It is enumerated
in `tests/parity/README.md` and NOT transcribed here: a copy of it went stale the first time the
list grew.

## 🔴 A THIRD-PARTY DEPENDENCY EXISTS NOW, AND `vendorHash = null` IS GONE

The refusal is `internal/depspolicy`: an allowlist failing on GROW *or* SHRINK, plus an
import ban over `cmd/cairn`/`cmd/cairn-server`'s graph. 🔴 **THE BAN KEEPS IT OUT OF THE
POD; THE ALLOWLIST CANNOT** — one entry is satisfied by a tree where `internal/api` imports
it on every route. A new module moves BOTH, and only `internal/ui` may import one.
🔴 **STILL A BUILD FAILURE THROUGH NIX** (all three Go derivations run these tests in
`doCheck`) **— but a build refusal cannot be deleted and this one can**; only the `go`
job's `ok` floor notices, and only per PACKAGE. 🔴 **gomponents does NOT neutralise a URL
scheme**: hrefs go through `safeHref`, `Raw`/`Rawf` are AST-banned.
📄 `internal/depspolicy`'s package doc (the claim, ONCE); `internal/ui/README.md`.

## 🔴 THE BROWSER SURFACE: THE CLAIMS THAT BIND A NEXT EDIT

🔴 **THE COOKIE BACKEND IS THIRD OF FOUR IN `identity.Backends`: every HEADER-borne
credential is tried before the AMBIENT one**, so a bearer token beats a stale cookie.

🔴 **TWO CROSS-SITE GATES, BOTH DERIVED FROM THE METHOD (`stateChanging`) AND NEVER FROM A
ROUTE CLASS**: same-origin **before** auth, so it covers the PUBLIC sign-in row; then a
per-session CSRF token **after** auth, so the token gate is reachable rather than shadowed.
A class can only make a route LESS protected, so no gate may be derived from one.

🔴 **THE SHARE FLOW ANSWERS "WHO HAS ACCESS TO THIS" FROM `control.Resolve`, NEVER FROM
`Model.Grants`** — authority arrives two ways, so a grant-row listing under-reports every
project MEMBER. 🔴 **`ReplicaHonesty` is pinned as a WHOLE NORMALISED STRING.** Both bind the
next edit; everything else about the flow — `Revocable`, `-control-journal`, why the
token-file deployment can show nothing — is in the README below, which is free to read.

📄 Everything else is re-derivable and lives where it is free to read: `internal/ui/README.md`
(the storage decision, the gates' RED proofs, phase C's decisions and mutation table, what
they still cannot see) and `internal/identity/session.go` (the two operator requirements, the
four options, the store's mechanics).

## 🔴 SEVERAL INSTANCES: AN UNROUTED SCOPE REFUSES

Binding rules: **`lib/README.md`**. Read it before editing any routing path.

## Installing and building with nix

```bash
nix run   github:ZacxDev/cairn -- doctor       # the DEFAULT client — the GO one — uninstalled
nix build github:ZacxDev/cairn#cairn           # the PYTHON client and the oracle — NOT the default
nix build github:ZacxDev/cairn#cairn-go        # the Go client by name — same store path as default
nix build github:ZacxDev/cairn#server-image    # the PYTHON pod image, as a loadable tarball
nix build github:ZacxDev/cairn#server-image-go # the GO pod image — published, deployed by nothing
```

Consumers pin this flake as an input; that is the supported way to get a `cairn`
whose version cannot disagree with the code in it, because **the version is the
git revision** and is never written down by hand.

🔴 **EVERY CLAIM BELOW SAYS WHICH CLIENT IT IS ABOUT.** `cairn` (`packages.cairn`) is
the Python client and the ORACLE; `cmd/cairn` (`packages.cairn-go`, and now
`packages.default`/`apps.default` too) is the Go port. **The Python client, its `lib/`
and its packaging are not deleted here** — P8 retires Python, and the parity gate stays
the comparison until it does. 🔴 **THE DEFAULT FLIPPED ON AN OPERATOR DECISION, NOT ON A
GREEN GATE** — the record is `tests/parity/README.md` residual 7.

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
directory, so neither hazard the paragraph above exists for can arise there.
What `packages.cairn-go` has instead is a `gitMinimal` on its wrapper's `PATH`,
for exactly the reason `packages.cairn` has one:
`internal/client/reposcope.go` invokes `git` by BARE NAME to derive a repo's
scope, so a package that did not carry its own answer would make `cairn recall`
with no `--scope` fail differently on two machines.
⚠ **Do not read the `lib/` rule as retired.** It governs the client that is
still shipped and still the oracle; it stops governing anything on the day
`packages.cairn` does, which is P8.

⚠ **A NIX SANDBOX PINS DIMENSIONS — ASK WHAT YOURS CANNOT HAVE BEFORE READING ITS
GREEN AS COVERAGE.** `checks.client-resolves-its-lib` has no cache root in its
HOME and therefore missed a crash on every host that HAD one; **every
`*-declares-its-*` check** has no store, token, network or cache-root HOME
either, so each exercises a LEDGER and nothing about behaviour. The worked
example, and why the parity harness is what measures behaviour instead, are in
`tests/parity/README.md`.

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
KNOW WHAT IT DOES NOT SEE.** It pins env, uid, port and entrypoint, and it is
structurally blind to LAYER CONTENTS. That blindness has already shipped an
image that started, passed health checks and served while every documented
operation against it failed — the worked example is beside the guard that closed
it, in `tests/test_flake_go_image_runtime_contract.py`. So: **before swapping
the deployed image, diff the two for what the test cannot read.**

📄 **THE MEASURED DIFFERENCES ARE IN `server/README.md`, NOT HERE** — the layer
table, the busybox applet surface (network **servers** and **clients**, including
`ssl_client`, beside a mounted credential), the setuid counts, and why that trade
is RECORDED RATHER THAN FIXED. 🔴 **THIS
SENTENCE CARRIES NO COUNT AND THE DELETION IS THE POINT — four drafts gave one
and all four undercounted. The set is whatever `busybox --list` on the built
image says; read it there, and do not supply a fifth number here.** Neither
image's tool surface is a subset of the other's, and nothing about it is settled.

🔴 **AND THERE IS A THIRD IMAGE: `packages.server-image-go`, WHICH IS A THIRD
BUILD AND NOT A THIRD STATEMENT OF THE CONTRACT.** It wraps `cmd/cairn-server`
and DERIVES uid, port, exposed port and env from the same
`serverUid`/`serverPort`/`serverEnv` bindings the pin above reads, minus a named
`serverEnvPythonOnly` set. A COPY is the hazard; a subtraction is what makes a
variable added for one pod reach both.
`tests/test_flake_go_image_runtime_contract.py` pins that it stays derived, that
it carries busybox and a `PATH`, and that its CA bundle is NAMED rather than
merely present. 🔴 **IT IS NOW PUBLISHED AND STILL DEPLOYED BY NOTHING, AND THOSE
ARE TWO CLAIMS.** `.github/workflows/publish-image.yml` pushes BOTH pods, to two
ghcr packages (`cairn-store`, `cairn-store-go`) under one `sha-<40-hex>` scheme;
the cutover of the DEPLOYED image remains a separate decision. ⚠ Both are
PUBLIC and anonymously pullable, verified against a negative control — including
the retracted private-on-first-publish prediction, recorded in
`server/README.md`.

🔴 **THE INTERPRETER IS PINNED, NOT INHERITED.** `flake.nix` uses
`pkgs.python312` because `server/Dockerfile` is `python:3.12-slim` and CI pins
`python-version: "3.12"`. A bare `pkgs.python3` followed nixpkgs to 3.14 and
shipped an interpreter **nothing in this repo had ever run the suite under**.
Move all three together or not at all.

## Naming

The project is **cairn**. Some identifiers still read `subsystem_store` / `SUBSYSTEM_STORE_*`
— these are **accepted aliases**, kept so existing deployments do not need a coordinated
cutover. New names should use `cairn` / `CAIRN_*`; do not mass-rename the aliases away without
a migration path for people already running this.
