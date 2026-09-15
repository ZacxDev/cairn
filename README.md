# cairn

**Simple, scoped, sharable, scalable agent-swarm memory.** A small hosted store
for per-subsystem engineering notes — a pod serving scoped, per-token-authorised
entries, and a client that syncs a read-through cache and reads (or writes) it.

Entries are plain `<scope>/<entry>.md` files: terse pointers, dated gotchas,
work history. The pod is one stdlib-Python HTTP layer over them; the client
keeps a stamped replica on each host and runs an unmodified local reader
against it, so an offline "orient me" still answers — and never lies about
which state produced what it printed.

## Installing and building with nix

```bash
nix run   github:ZacxDev/cairn -- doctor     # the client, without installing it
nix build github:ZacxDev/cairn#cairn        # the client
nix build github:ZacxDev/cairn#server-image # the pod image, as a loadable tarball
```

Consumers pin this flake as an input. The version **is** the git revision —
never written down by hand — so a built `cairn` cannot disagree with the code
in it.

## The client — `cairn`

| you want | run |
|---|---|
| refresh the local cache from the pod | `cairn sync` |
| a scope's digest | `cairn recall --scope X` (`--ref R`, `--list`, `--limit N`, `--page N`) |
| find a hunk by text | `cairn search 'query'` (`--all-scopes`) |
| what the cache actually holds | `cairn ls-entries` |
| parse-check the cached entries | `cairn validate` |
| one call of diagnostics | `cairn doctor` (`--json`, `--no-sync`) |
| append one dated, attributed bullet | `cairn append --scope S --ref R --text '…' --session ID` |
| replace a whole entry behind `If-Match` | `cairn put --scope S --ref R --file F` |
| create a new entry (refuses to overwrite) | `cairn create --scope S --ref R --file F` |

**Reads never lie about why nothing was printed.** A report states which of
`live` / `cached (stale <age>)` / `scope-empty` produced it — all exit 0 —
while `store-unreachable, no cache` exits 3. `sync` exits 4 when it could not
refresh though a cache survived, and a refused archive exits 5. **Writes never
degrade**: there is no cache to answer from and no stale write, so an
unreachable store is a refused write at exit 7, never a queued or local one
(6 refused · 8 precondition-failed · 9 already-exists — disjoint from every
read code, so a caller cannot read silence as success).

## The server

| route | what |
|---|---|
| `GET /healthz` | unauthenticated; body is exactly `ok\n` |
| `GET /api/v1/recall/{scope}` | rendered digest (`?mode=&ref=&limit=&page=`) |
| `GET /api/v1/search/{scope}?q=…` | search (`?threshold=&max_hits=&context=&all_scopes=`) |
| `GET /api/v1/snapshot[?scope=]` | gzipped tar of the entry files — the sync payload |
| `POST /api/v1/entry/{scope}/{ref}/bullets` | append ONE attributed bullet (the actor comes from the token, never the body) |
| `PUT /api/v1/entry/{scope}/{ref}` | whole-file replace via `If-Match`, or create via `If-None-Match: *` |

Auth is a token **set** — one row per line, `<token>` for legacy or
`<token> <identity> <scope>,<scope>` for a mapped row — rotated by overlap and
hot-reloadable with `SIGHUP`. A token's scope allowlist gates every route, and
a scope outside it answers exactly what a scope that never existed answers, so
an error cannot enumerate the store. The full contract, plus the operating
runbook (seeding, byte-identity verification, rotation, rate limiting), is
[`server/README.md`](server/README.md).

## Layout

| path | what |
|---|---|
| `cairn` | the Python client CLI, and the ORACLE the Go one is measured against |
| `lib/` | the Python reader: cache resolution, recall rendering, scope/ref resolution, doctor |
| `server/` | the pod: `server.py`, `Dockerfile`, `seed.sh`, `verify-byte-identity.sh` |
| `cmd/cairn-server`, `internal/api` | the Go port of the server — passes the corpus, not deployed |
| `cmd/cairn`, `internal/client` | the Go port of the CLIENT — byte-identical to the Python one |
| `internal/report` | the ONE renderer, shared by the pod and the CLI |
| `tests/` | the suites, plus `leakscan.py`, the HTTP conformance corpus and the client parity gate |
| `flake.nix` | both clients, the server image, the Go server, and the checks |

## The Go port, and why two servers are alive

`server/server.py` is the **oracle**. `cmd/cairn-server` is a stdlib-only Go port of it,
and it is not deployed by anything yet: it exists so the store API's contract can be
replayed against both implementations on one store and the difference measured rather
than reviewed. The contract is recorded as HTTP-level golden fixtures generated from the
oracle — 98 cases, 99 requests, 433 assertions — in
[`tests/conformance/`](tests/conformance/README.md).

The Go server now answers **every route**, report rendering included: 116 PASS, 0 failing
cases, 0 failing relations, and 4 rows skipped as CPython artifacts. The oracle stays at 0
failures and 0 skipped. Both sides are in CI, as a floor on passes and a ceiling of zero on
failures of every kind.

⚠ **A green corpus is not a finished port, and the numbers above are not the byte-identity
gate.** The corpus pins the bytes it was told to send, so `internal/report` carries its own
differential fixture — the oracle's rendered bytes over 50 report shapes no corpus row
reaches — and the next step in the sequence is running both servers over ONE store and
comparing. `AGENTS.md` states that order and the reasons for it.

## …and why two CLIENTS are alive

Rewriting the server alone would have left **two renderers in two languages that must agree
byte-for-byte forever**, with drift arriving as "a different order that reads as a stale
cache" — no error, no missing entry. That premise entails deleting the *Python renderer*; it
does not on its own entail a full CLI port, which a Python CLI over a small Go renderer would
also have satisfied. What carries the full port is the next reason: **a single binary, one
language, and Python retirable at P8**. Both land on the same `internal/report`: one renderer,
three consumers (pod, CLI, a future UI), which makes byte-identity a property of there being one
implementation rather than a discipline two are held to — ⚠ **from P8, not from today**, because
the Python renderer ships as `packages.default` until the oracle is deleted.

`cairn` stays the oracle and `packages.default` still builds it. The gate is
[`tests/parity/`](tests/parity/README.md): both clients, one pod, one store, one cache root,
identical argv. **90 cases, 91 PASS, 0 failures** across all nine verbs, every output-shaping
flag, every documented exit code, `--help` in four spellings, and argparse's option-versus-value
rules. ⚠ **That is not 90 byte diffs, and the split says which rows are load-bearing:** 59 rows
diff stdout, stderr *and* the exit code; 23 compare the exit code only; 8 compare the exit code
plus "both sides put something on stdout" — so **31 of the 90 never compare output text**, every
one of them because argparse's wording is not worth reproducing in Go. The residual table says
which, per row.

⚠ **A green gate is not evidence until its controls have been watched to work.** This one's
first full run reported 72 PASS / 0 FAIL while every request was refused and no cache was ever
written — two clients failing identically compare equal. It now refuses to vouch unless a
pre-flight fetch succeeds and some row renders a real digest, and `--self-test` proves the
differ can go red on stdout, on stderr and on the exit code separately.

## Development

```bash
pytest tests/                         # the Python suite
go vet ./... && go test ./...         # the Go port's own guards
tests/conformance/run_go.sh           # the corpus against the Go server
python3 tests/conformance/suite.py run  # …and against the oracle: must stay at 0 failures
python3 tests/parity/harness.py       # both CLIENTS over one cache root: the P2 gate
python3 tests/parity/harness.py --self-test  # prove the differ can go RED
python3 tests/reader_fixtures.py generate  # re-record the renderer's bytes FROM the oracle
python3 tests/leakscan.py             # the leak gate — runs in CI on every commit
python3 tests/leakscan.py --self-test # prove the gate is an instrument
```

This repository is public and was extracted from a private one: never commit
hostnames, private IPs, real project/scope names, dated incident narration, or
captured text — fixtures are synthetic. `tests/leakscan.py` runs in CI on every
commit. The full rules agents work under live in [`AGENTS.md`](AGENTS.md).

## Naming

The project is **cairn**. Some identifiers still read `subsystem_store` /
`SUBSYSTEM_STORE_*` — accepted aliases, kept so existing deployments don't
need a coordinated cutover. New names use `cairn` / `CAIRN_*`.
