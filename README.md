# cairn

**Simple, scoped, sharable memory for AI agent swarms.**

A small hosted store that many agents, on many hosts, can read and write at
once without overwriting each other or seeing each other's scopes. Entries are
plain `<scope>/<entry>.md` files — terse pointers, gotchas, work history — so
what an agent wrote stays readable by a human in a text editor, and what a human
wrote is readable by an agent with no ingestion step.

It is a **store**, not a retrieval engine: no embeddings, no index build, no
ranking model. A scope is a directory, a memory is a bullet in a file, and
recall is that file rendered. If you want semantic search over a corpus, cairn
is the wrong tool. If you want a shared notebook several agents can be trusted
to write to concurrently, this is what it does.

## Why a swarm needs more than a shared file

Four properties you would otherwise have to build yourself on top of a shared
bucket, a git repo or an NFS mount:

- **Scoped.** A token carries a scope allowlist, and it gates every route. A
  scope outside it answers **exactly** what a scope that never existed answers,
  so an agent cannot enumerate the store by probing for errors. Give each agent
  — or each tenant, or each task — its own token and its own slice.
- **Attributed.** Every write carries the writing agent's session id, and it is
  **required**. The actor comes from the token; a body-supplied `actor` is
  accepted and discarded as hostile input. A bullet renders as
  `- YYYY-MM-DD: <text> [cairn: <identity>/<session>]`, so "which agent believed
  this, and when" survives into the memory itself.
- **Concurrency-safe, and safe to retry.** `PUT` requires `If-Match` (`428`
  without it; `*` refused), and `create` lands through a hard link, so `EEXIST`
  is decided by the kernel rather than by a check-then-write. Two agents racing a
  create cannot both win, and neither can silently clobber the other's edit —
  the loser gets exit `8` and re-derives. An **append** is recognised by content
  hash, so a re-POST after a timeout is idempotent and reports `duplicate`
  rather than writing twice: the failure mode a retrying agent actually has.
- **Honest about staleness.** Each host keeps a stamped read-through replica, so
  recall does not depend on the network — but a read always states which state
  produced it (`live` / `cached (stale <age>)` / `scope-empty`), and an
  unreachable store with no cache exits `3` rather than answering "empty". An
  agent that cannot tell "nothing is there" from "I could not look" will act on
  the difference.

## Quickstart — two agents, one store

```bash
# Each agent host: point at the pod and its own token.
mkdir -p ~/.config/subsystem-store
cat > ~/.config/subsystem-store/env <<'EOF'
SUBSYSTEM_STORE_URL=https://store.example.invalid
SUBSYSTEM_STORE_TOKEN=<this agent's token>
EOF

# Agent A records a finding, attributed to its own session.
cairn append --scope alpha-index --ref widget-cfg \
  --text 'the retry budget is per-connection, not per-request' \
  --session "$AGENT_SESSION_ID"
# → cairn: appended instance=personal scope=alpha-index ref=widget-cfg revision=<sha>
#   (`duplicate` instead of `appended` if that exact bullet was already there)

# Agent B, on another host, picks it up.
cairn sync                              # refresh the local replica
cairn recall --scope alpha-index        # the scope's digest, with A's bullet and its session id
cairn search 'retry budget'             # or find the hunk by text

# A third agent, whose token does not name alpha-index:
cairn recall --scope alpha-index        # `scope-empty`, exit 0 — indistinguishable
                                        # from a scope that was never created
```

`cairn doctor` answers pod reachability, credential, cache stamp, counts, scope
visibility and which store this host resolves — in one call, with `--json` for a
supervising process.

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
**Go** one, landed by **[#50](https://github.com/ZacxDev/cairn/pull/50)** — the anchor
to check a pin against, because "now" cannot tell you whether the flip sits between the
revision you are on and the one you are moving to. So `nix run github:ZacxDev/cairn`
and `nix profile install github:ZacxDev/cairn` both execute `cmd/cairn` rather than the
Python script.

Two behaviours change, both stated here rather than left to be discovered:
**`cairn -verbs` and `cairn -exit-codes` now answer** — exit `0` with a table on
stdout, where the Python client exits `2` with argparse's `usage:` for the same
argv, which is a single-dash token *succeeding* where the default previously
refused; and **argparse's exact wording is gone** from `--help` and from usage
errors, so anything parsing that prose needs re-reading. The exit codes
themselves are identical and gated.

**Nothing was deleted, and `cairn` is the opt-out — in BOTH consumption modes.** The
Python client is still built and is still the **oracle** the Go one is measured
against. From the CLI that is a `#fragment`:

```bash
nix build github:ZacxDev/cairn#cairn           # the Python client, explicitly
nix run   github:ZacxDev/cairn#cairn -- doctor
```

…but the consumer this flip actually lands on pins this flake as an **input** and
reaches an *attribute*, where a `#fragment` is not a spelling you can use:

```nix
{
  inputs.cairn.url = "github:ZacxDev/cairn";   # or pin a rev/ref

  outputs = { self, nixpkgs, cairn, ... }:
    let system = "x86_64-linux"; in {
      # `cairn.packages.${system}.default` is now the GO client.
      # Take the PYTHON client by name instead:
      packages.${system}.my-cairn = cairn.packages.${system}.cairn;
      # …and its runnable form, if you re-export an app:
      apps.${system}.my-cairn = cairn.apps.${system}.cairn;
    };
}
```

Both spellings are gated rather than promised: `checks.default-is-the-go-client` pins
the flip itself and both opt-out spellings, at evaluation time, building neither client.
⚠ It is described here in one sentence and in full beside the code it guards
(`flake.nix`), which is the only description worth trusting — this one is hand-written
and nothing asserts it. **The record of why the flip was taken on an operator decision
rather than on a green gate** lives in [`tests/parity/README.md`](tests/parity/README.md)
residual 7, and nowhere else on purpose.

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

### Exit codes, because the caller is usually a program

Read outcomes and write outcomes are **disjoint**, so a supervisor cannot read
silence as success. `0` content was served (live, cached, or a genuinely empty
scope) · `2` usage · `3` store unreachable and **no** cache, nothing was read at
all · `4` `sync` did not refresh though a usable cache survived · `5` the archive
was refused · `6` the store refused the write, change the request · `7` the write
did **not** happen and a retry is the right response · `8` precondition failed,
re-sync and re-apply · `9` `create` only, the entry already exists · `11` no
instance could be decided for this scope. `doctor` prints its own set on every
run, where `10` means *no problem measured, but at least one check could not
look* — which is not a clean bill of health.

**Writes never degrade.** There is no cache to answer from and no such thing as a
stale write, so an unreachable store is a refused write at `7`, never a queued or
local one.

### One instance, or several

The client is configured by `~/.config/subsystem-store/env` —
`SUBSYSTEM_STORE_URL` and `SUBSYSTEM_STORE_TOKEN`, environment variables of the
same name winning — and that instance is called `personal`. A second instance is
an **additive** file at `~/.config/subsystem-store/instances/<alias>.env` with its
own cache root and its own sync stamp; the default instance's cache root does not
move. Which instance a scope belongs to is decided by a table **you** supply —
`~/.config/subsystem-store/routes.json`, or `$CAIRN_ROUTES`, a flat JSON object of
`"scope": "alias"` — because this program knows how to route and nothing about who
routes where.

🔴 **With more than one instance, an unregistered scope refuses at exit 11, naming
the scope.** It never falls back: a write that lands in a store nobody reads is
found days later, if at all, and a refusal costs one error message.
`cairn routes --check` grades the table in both directions. With **one** instance
configured, every read prints what it always did. Two exceptions at any instance
count: the **write** verbs name their instance unconditionally — "where did that
bullet go" is a question about a durable record, asked later, by someone who no
longer has the terminal — and a table entry naming an alias this host has no config
for still refuses. ⚠ **If you parse `cairn: appended scope=… ref=…` positionally,
that field is new — it sits between the status and `scope=`.** The binding rules for
anyone editing a routing path are in [`lib/README.md`](lib/README.md).

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
hot-reloadable with `SIGHUP`, so onboarding an agent does not restart the pod. The
full contract, plus the operating runbook (seeding, byte-identity verification,
rotation, rate limiting), is [`server/README.md`](server/README.md).

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

## How the agreement is measured, and why two of everything is alive

`server/server.py` is the **oracle**; `cmd/cairn-server` is a stdlib-only Go port of
it, deployed by nothing. Rewriting the server alone would have left two renderers in
two languages that must agree byte-for-byte forever, with drift arriving as "a
different order that reads as a stale cache" — no error, no missing entry. So both
land on the same `internal/report`: one renderer, three consumers (pod, CLI, a future
UI). ⚠ That becomes a *property* only when the Python renderer is deleted at P8; until
then it is a discipline, and these three instruments are what enforce it:

| instrument | what it compares | read it |
|---|---|---|
| [`tests/conformance/`](tests/conformance/README.md) | the served HTTP contract, as golden fixtures generated from the oracle, replayed against either server | the corpus and its declared blind spots |
| [`tests/dualrun/`](tests/dualrun/README.md) | both servers over **one** store — every route, scope, entry and principal, and the snapshot's **uncompressed** tar byte for byte | why gzip identity is unattainable, and the PAX-header divergences a tree comparison cannot see |
| [`tests/parity/`](tests/parity/README.md) | both **clients**, one pod, one cache root, identical argv, across all nine verbs | the seven declared residuals, and the P8 retirement ledger |

⚠ **Counts are deliberately not quoted here.** Every one of them went stale in this
file at least once while the gates moved, and nothing asserted on them; the numbers
live beside the gates that produce them, and the CI floors live in
[`.github/workflows/ci.yml`](.github/workflows/ci.yml). Re-derive rather than trust —
`grep -c 'Case(' tests/parity/harness.py` for the parity case count, and the
`compare=` distribution beside it for how many rows compare output text rather than
only the exit code.

⚠ **And a green gate is not evidence until its controls have been watched to work.**
The parity gate's first full run reported 72 PASS / 0 FAIL while every request was
refused and no cache was ever written — two clients failing identically compare equal.
Both harnesses now refuse to vouch (exit **2**, which is not "failed") unless a
pre-flight succeeds, and both ship a `--self-test` that proves the differ can go red.

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
