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
nix run   github:ZacxDev/cairn -- doctor       # the default client — the GO one — uninstalled
nix build github:ZacxDev/cairn#cairn-go        # the Go client, by name
nix build github:ZacxDev/cairn#cairn           # the PYTHON client — no longer the default
nix build github:ZacxDev/cairn#server-image    # the pod image, as a loadable tarball
```

Consumers pin this flake as an input. The version **is** the git revision —
never written down by hand — so a built `cairn` cannot disagree with the code
in it.

### 🔴 The default client is now the Go one — what changed for you

`packages.default` and `apps.default` were the **Python** client. They are now the
**Go** one, so `nix run github:ZacxDev/cairn` and `nix profile install
github:ZacxDev/cairn` both execute `cmd/cairn` rather than the Python script.
`#cairn-go` is the same store path under its own name.

**Nothing was deleted.** `nix build github:ZacxDev/cairn#cairn` still builds the
Python client, which remains the **oracle** the Go one is measured against; it is
retired at P8, not here. Naming `#cairn` is the opt-out from everything below.

The two clients are diffed against each other by
[`tests/parity/`](tests/parity/README.md) over all nine verbs — **101 cases, 102
PASS, 0 failures** — so a `cairn recall` / `search` / `ls-entries` / `doctor` you
already run prints what it always printed. ⚠ Read that gate at its real width
before relying on it: **31 of the 101 rows compare only the exit code**, or the
exit code plus "both sides printed something", and never the output text. The
residual table says which, per row. **Two things do change:**

- **`cairn -verbs` and `cairn -exit-codes` now answer.** They exit **0** with a
  table on stdout; the Python client exits **2** with argparse's `usage:` block on
  stderr for the same argv. A single-dash token that the default *refused* before
  this change *succeeds* after it — the dangerous direction, which is why it is
  stated here rather than left to be discovered. They are the Go client's ledgers
  (`tests/parity/README.md` residual 7); whether they become documented public
  surface or move behind a gate is a P8 decision that has not been taken.
- **Argparse's exact wording is gone from `--help` and from usage errors.** The
  exit codes are identical and gated; the *text* of a usage failure is not, and
  the parity gate deliberately does not compare it. Anything parsing argparse's
  prose will need re-reading.

⚠ **And one thing that was expected to change and MEASURABLY DOES NOT.** A
`routes.json` entry naming an alias this host has no config for makes `recall`,
`search` and `validate` refuse at exit **11** — at one instance as well as many.
That became true of the *Go* client only recently (`tests/parity/README.md`
residual 8), so it was written down as belonging in this announcement. It does not:
measured here over one synthetic world with `{"alpha-notes": "nowhere"}`, **both**
clients exit 11 on all three verbs, because the port took the behaviour *from* the
oracle. The old default already refused, so the new default refusing is not a
change for anyone. `cairn routes --check` grades the table in both directions;
`sync`, `ls-entries` and `doctor` route nothing and are untouched.

**What licensed this was an operator decision, not a green gate.** An earlier draft
took the flip because the parity gate was green; it was reverted, and that reading
is still wrong. The decision followed the closure of the one *measured* blocker:
the Go client used to refuse every read verb at exit 11 on a host with more than
one instance configured, which would have made the `doctor` quickstart above refuse
there. It routes now.

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
| the scope→instance table, graded | `cairn routes` (`--check`) |

**One instance, or several.** The client is configured by
`~/.config/subsystem-store/env` — `SUBSYSTEM_STORE_URL` and
`SUBSYSTEM_STORE_TOKEN`, environment variables of the same name winning — and
that instance is called `personal`. A second instance is an **additive** file at
`~/.config/subsystem-store/instances/<alias>.env` with its own cache root
(`~/.cache/subsystem-store-<alias>`) and its own sync stamp; the default
instance's cache root does not move. Which instance a scope belongs to is
decided by a table **you** supply — `~/.config/subsystem-store/routes.json`, or
`$CAIRN_ROUTES`, a flat JSON object of `"scope": "alias"` — because this program
knows how to route and nothing about who routes where.

🔴 **With more than one instance, an unregistered scope refuses at exit 11,
naming the scope.** It never falls back to an instance: a write that lands in a
store nobody reads is found days later, if at all, and a refusal costs one error
message. `cairn routes --check` grades the table in both directions — a scope
with no entry, and an entry naming an alias that does not exist. A table entry
naming a scope that holds **no entries** prints as a ⚠ note and does *not* fail
the check: a snapshot ships entry files rather than directories, so a stale entry
and one pre-registered before its first write look identical from here, and
failing on it would break the very thing the table is for.

With **one** instance configured, every READ prints what it always did —
including when a table is already present, which is the normal state while you
are writing one. Two exceptions, at any instance count, and neither is an
accident:

- **the write verbs name their instance unconditionally.** `append`, `put` and
  `create` print `instance=personal` even on a one-store host, because "where did
  that bullet go" is a question about a durable record, asked later, by someone
  who no longer has the terminal. ⚠ If you parse `cairn: appended scope=… ref=…`
  positionally, that field is new — it sits between the status and `scope=`.
- **a table entry naming an alias this host has no config for still refuses**,
  because that entry says where the scope lives and this machine cannot reach it.

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
| `cmd/cairn`, `internal/client` | the Go port of the CLIENT, and **the default** — byte-identical to the Python one |
| `internal/report` | the ONE renderer, shared by the pod and the CLI |
| `tests/` | the suites, plus `leakscan.py`, the HTTP conformance corpus, the server dual-run gate and the client parity gate |
| `flake.nix` | both clients (`default` is the **Go** one, `#cairn` the Python one), the server image, the Go server, and the checks |

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
reaches — and the byte-identity comparison is a separate instrument.

That instrument is [`tests/dualrun/`](tests/dualrun/README.md): both servers over **one**
store, every route, every scope, every entry, three principals, and the snapshot's
**uncompressed** tar compared byte for byte (gzip identity between `compress/flate` and zlib
is measured unattainable, so the envelope is a declared difference). Measured **612 targets /
2,492 comparisons / 0 differences** on a real store and **361 / 1,489 / 0** on a generated
one. It found one divergence nothing else could see — the audit record's timestamp spelling —
and its own controls are a pre-flight that refuses to vouch, seven mutants of which seven are
killed, and a positive control that watches the comparison count move with the store.
`AGENTS.md` states the sequence and the reasons for it.

## …and why two CLIENTS are alive

Rewriting the server alone would have left **two renderers in two languages that must agree
byte-for-byte forever**, with drift arriving as "a different order that reads as a stale
cache" — no error, no missing entry. That premise entails deleting the *Python renderer*; it
does not on its own entail a full CLI port, which a Python CLI over a small Go renderer would
also have satisfied. What carries the full port is the next reason: **a single binary, one
language, and Python retirable at P8**. Both land on the same `internal/report`: one renderer,
three consumers (pod, CLI, a future UI), which makes byte-identity a property of there being one
implementation rather than a discipline two are held to — ⚠ **from P8, not from today**, because
the Python renderer still ships as `packages.cairn` until the oracle is deleted. The default
flipping to Go did not make it one renderer; it changed which one you get by default.

`cairn` stays the oracle, and `packages.cairn` still builds it. The gate is
[`tests/parity/`](tests/parity/README.md): both clients, one pod, one store, one cache root,
identical argv. **101 cases, 102 PASS, 0 failures** across all nine verbs, every output-shaping
flag, every documented exit code, `--help` in four spellings, argparse's option-versus-value
rules, and a two-instance host with a second pod. ⚠ **That is not 101 byte diffs, and the split
says which rows are load-bearing:** 70 rows diff stdout, stderr *and* the exit code; 23 compare
the exit code only; 8 compare the exit code plus "both sides put something on stdout" — so
**31 of the 101 never compare output text**, every one of them because argparse's wording is not
worth reproducing in Go. The residual table says which, per row.

⚠ **Every count in that paragraph is restated here from `AGENTS.md` and NOTHING ASSERTS ON
THEM** — the gate's own floor lives in `.github/workflows/ci.yml`, which refuses below 102
passes. They went stale while `AGENTS.md` and that floor moved, and this file still read
`90 / 91 / 59 / 31 of the 90`, so re-derive rather than trust:
`grep -c 'Case(' tests/parity/harness.py` for the case count, and the `compare=` distribution
beside it for the split.

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
