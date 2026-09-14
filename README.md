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
| `cairn` | the client CLI |
| `lib/` | the reader: cache resolution, recall rendering, scope/ref resolution, doctor |
| `server/` | the pod: `server.py`, `Dockerfile`, `seed.sh`, `verify-byte-identity.sh` |
| `tests/` | the suites, plus `leakscan.py` |
| `flake.nix` | the packaged client, the server image, and the checks over both |

## Development

```bash
pytest tests/                         # the suite
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
