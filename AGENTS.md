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
| `cairn` | the client CLI — sync, recall, search, ls-entries, doctor, append, put, create |
| `lib/` | the reader: cache resolution, recall rendering, scope/ref resolution, doctor |
| `server/` | the pod: `server.py`, `Dockerfile`, `seed.sh`, `verify-byte-identity.sh` |
| `cmd/`, `internal/`, `go.mod` | the Go port of the server (P1), stdlib-only — see below |
| `tests/` | the suites, plus `leakscan.py` and `conformance/` |
| `flake.nix` | the packaged client, the server image, the Go server, and the checks over all three |

## 🔴 TWO SERVERS ARE ALIVE, AND `server/server.py` IS THE ORACLE

`cmd/cairn-server` is the Go port. It is **not deployed by anything**: it exists so the
conformance corpus can be replayed against both implementations on the same store and
the difference MEASURED. The sequence is fixed — the Go server passes the corpus, then
both run over one store and byte-identity is compared, then the client is ported, then
Python is retired. **Step one is done and step two is not**: the corpus is green for both
implementations, and no dual-run comparison has been made. Do not declare a step done
early, and do not switch the deployed image on the strength of a green corpus — the
sentence here used to read "while the corpus is partial", which a green corpus would have
satisfied while leaving every remaining step untouched.

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

🔴 **THE MUTATION BATTERY OVER THIS PORT: 46 mutants, 43 KILLED, 3 SURVIVED — and all three
survivors are LABELLED EQUIVALENT AT THE CODE, with the reasoning, because two of them
corrected a comment that was wrong.** Round 1 killed 24 of 40 and the 13 survivors are what
built the fixture above; round 2 killed 40 of 46; round 3 is clean. The three that survive
correctly: `sort.SliceStable` → `sort.Slice` (the comparator is a total order);
`1e-9*nsec` → `nsec/1e9` (**measured bit-identical at every realistic mtime magnitude** —
the ULP of the sum dwarfs the difference, and the old comment claimed the hazard was
reachable); and disabling `difflib`'s extension loops (**with an empty junk set the DP has
already found the longest contiguous run, so neither loop can advance** — the old comment
said two of the four "can run", which is two more than can).

🔴 **THE RENDERER IS A LIBRARY, NOT A HANDLER, BECAUSE P2 IMPORTS IT.** `internal/report`
holds `Recall`, `Search`, `RecallReport.RenderText`, `SearchReport.RenderText` and
`ExitFor` — plain values in, plain values out, no `net/http` type in any signature, no
server config, and every error classifiable with `errors.As`/`errors.Is`
(`store.StoreMissingError`, `store.EntryUnreadableError`,
`report.ErrFocusSelectorUnported`). `report.Reader` is the thin `Renderer` the pod hands
to `internal/api` and the ONLY type in the package that knows a server exists. That shape
is the whole point of P2: **pod and CLI run one renderer**, which is what makes
byte-identity a property rather than a discipline — rewriting the server alone would leave
two renderers agreeing forever by review.

⚠ **THREE PLACES THE PORTED READER DELIBERATELY DIFFERS FROM THE ORACLE, each recorded
because none is visible to the corpus:**

| where | the difference | why |
|---|---|---|
| `report.ErrFocusSelectorUnported` | a non-empty focus path window is REFUSED, not served | the oracle's featured pick has two selectors and only the most-recent fallback is ported. A window that silently fell back would print a basis claiming a resolved pick — a wrong claim, silently. Closing condition: `associate_paths` ported with a red-at-baseline differential test, then the guard is deleted with it |
| `store.ScopeRevision` on a `.git/HEAD` that is not valid UTF-8 | ONE `400` audit line here, **two** (`200` then `400`) on the oracle | there the strict decode raises while the response's arguments are being evaluated, after the 200 line is already written. Reproducing a mid-response raise to duplicate a log line is a worse trade than naming it |
| `store.ScopeRevision` resolving a `ref:` | `filepath.Join` CLEANS, so a `ref:` naming `../…` cannot climb out of the git dir; the oracle's `git / ref` can | a NARROWING, in the safe direction. A HEAD pointing outside its own repo is not a revision worth reporting |

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

## Installing and building with nix

```bash
nix run   github:ZacxDev/cairn -- doctor    # the client, without installing it
nix build github:ZacxDev/cairn#cairn        # the client
nix build github:ZacxDev/cairn#server-image # the pod image, as a loadable tarball
```

Consumers pin this flake as an input; that is the supported way to get a `cairn`
whose version cannot disagree with the code in it, because **the version is the
git revision** and is never written down by hand.

🔴 **`lib/` MUST STAY BESIDE THE CLIENT SCRIPT, AND THE PACKAGE IS BUILT THAT
WAY ON PURPOSE.** `cairn` finds its modules with
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

⚠ **`checks.client-resolves-its-lib` runs in a nix sandbox, and a sandbox pins
dimensions.** Its HOME has no cache root, which is exactly why it did not
notice that `cairn doctor` crashed on any host that HAD one
(`AttributeError: 'NoneType' object has no attribute 'iterdir'`, zero stdout,
exit 1, whenever `CAIRN_MIRROR_ROOT` was unset — the default). Ask what your
sandbox cannot have before reading its green as coverage.

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
