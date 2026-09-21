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

- **Scoped.** A token carries a scope allowlist, and it gates every route except
  `/healthz`. A scope outside it answers **exactly** what a scope that never
  existed answers, so an agent cannot enumerate the store by probing for errors.
  Give each agent, or each task, its own token and its own slice. ⚠ **One thing
  is still shared, and it is the reason this is not tenant isolation:**
  `X-Store-Snapshot` and the freshness line that opens every body carry a
  **store-wide** `entry-files=` count, over scopes the caller cannot name. Two
  parties on one pod can each watch the other's count move. Stated in
  `server/server.py`, and repeated here rather than left to be found.
- **Attributed — a record of who wrote what, not a security boundary.**
  `cairn append` requires `--session` (no default, no env fallback) and renders
  `- YYYY-MM-DD: <text> [cairn: <identity>/<session>]`, so a swarm's memory
  carries which agent believed a thing and on which run. Read at its real width:
  - the **`<identity>`** is taken from the token on `POST /bullets`, and a
    body-supplied `actor` is never read — so on that route it cannot be forged;
  - the **`<session>`** is supplied by the caller. It is **correlation data,
    not an identity claim**: the server validates its shape and never its
    ownership, so one agent can name another's session — and a session id is
    printed in the recall of the entry it wrote to, readable by anyone who can
    read the scope (`--ref <entry>`, or `--limit <n>` for the whole scope; the
    default digest prints one body out of N, so it is **not** the way to
    enumerate them), so they are not secrets either;
  - `put` and `create` write your bytes **verbatim**, trailer included, and the
    server does not check it. Enforcing that was considered and declined
    (`server/server.py`).

  🔴 **And there is no append-only credential to close the gap with.** The verb
  set is closed at three — `read`, `write`, `admin` — with `append` deliberately
  absent, because all three write verbs mutate a file through one path and a
  fourth verb would be spelled rather than structural (`internal/control`). Any
  token that can append can also `PUT`. **So treat a trailer as a cooperative
  record for debugging and recall, never as evidence of authorship.**
- **Concurrency-safe, and safe to retry.** `PUT` requires `If-Match` (`428`
  without it; `*` refused), and `create` lands through a hard link, so `EEXIST`
  is decided by the kernel rather than by a check-then-write. Two agents racing a
  create cannot both win — the loser gets exit **`9`** (*it already exists, do
  not retry*), which is deliberately **not** the `8` a lost `put` update gets
  (*re-sync, re-derive, re-apply*): the server answers the same `412` for both
  and the remedies are opposites, so conflating them retries forever. An
  **append** is recognised by content hash, so a re-POST after a timeout is
  idempotent and reports `duplicate` rather than writing twice — the failure
  mode a retrying agent actually has.
- **Honest about staleness.** Each host keeps a stamped read-through replica, so
  recall does not depend on the network. A read names **which of four states**
  produced it — `live`, `cached` (with age and revision), `scope-empty`, or
  🔴 **`store-unreachable, no cache`, which exits `3` and must never be read as
  the third**: `scope-empty` and an unreachable store both "print no entries"
  and one of them is a lie. Orthogonally it names the **scope's status**, so a
  `cached` read can still report `scope-absent` (no such scope here — also what
  a scope your token cannot see looks like) or `scope-unreadable`. An agent that
  cannot tell "nothing is there" from "I could not look" will act on the
  difference.

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
cairn recall --scope alpha-index        # `scope-absent`, exit 0 — byte-identical
                                        # to a scope that was never created
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
nix build github:ZacxDev/cairn#cairn-ui        # the BROWSER surface — phase A, deployed by nothing
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
themselves are identical and gated. ⚠ **Whether `-verbs`/`-exit-codes` become
documented public surface or move behind a gate is a P8 decision that has not
been taken** — they are the Go client's internal ledgers, and today they answer.

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
residual 7, and nowhere else on purpose. [`CHANGELOG.md`](CHANGELOG.md) indexes this
and every later contract change by PR and sha, so a consumer can tell whether one sits
between the revision they are pinned to and the one they are moving to.

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
scope) · `2` usage · `3` **nothing was read** — the store was unreachable with no
cache, *or* the scope's entries are there and unreadable, which is a content
defect `cairn validate` diagnoses rather than an outage to retry · `4` `sync` did
not refresh though a usable cache survived · `5` the archive was refused · `6` the
store refused the write, change the request · `7` the write did **not** happen and
a retry is the right response · `8` precondition failed, re-sync and re-apply ·
`9` `create` only, the entry already exists — **do not** retry, and do not
conflate it with `8` · `11` no instance could be decided for this scope. `doctor`
prints its own set on every run, where `10` means *no problem measured, but at
least one check could not look* — which is not a clean bill of health.

**Writes never degrade.** There is no cache to answer from and no such thing as a
stale write, so an unreachable store is a refused write at `7`, never a queued or
local one.

### One instance, or several

The client is configured by `~/.config/subsystem-store/env` —
`SUBSYSTEM_STORE_URL` and `SUBSYSTEM_STORE_TOKEN`, environment variables of the
same name winning — and that instance is called `personal`. A second instance is
an **additive** file at `~/.config/subsystem-store/instances/<alias>.env` with its
own cache root (`~/.cache/subsystem-store-<alias>`) and its own sync stamp; the
default instance's cache root does not move. Which instance a scope belongs to is
decided by a table **you** supply —
`~/.config/subsystem-store/routes.json`, or `$CAIRN_ROUTES`, a flat JSON object of
`"scope": "alias"` — because this program knows how to route and nothing about who
routes where.

🔴 **With more than one instance, an unregistered scope refuses at exit 11, naming
the scope.** It never falls back: a write that lands in a store nobody reads is
found days later, if at all, and a refusal costs one error message.
`cairn routes --check` grades the table in both directions — a scope with no
entry, and an entry naming an alias that does not exist. ⚠ **It does not fail on
a table entry naming a scope that holds no entries**: that prints as a note,
because a snapshot ships entry files rather than directories, so a stale entry
and one pre-registered before its first write look identical from here. Do not
gate CI on `--check` expecting it to catch that case. With **one** instance
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

## The browser surface — `cairn-ui`

There is one, and it is **phase A**: a single read-only page listing the scopes
your credential can see and the entries in them. `GET /` is the page and is
authenticated; `/healthz` is the one unauthenticated route. There are no write
routes, no sign-in flow, and **nothing deploys it** — it is built and run by hand.

```bash
nix build github:ZacxDev/cairn#cairn-ui
./result/bin/cairn-ui -store <store root> -token-file <token file> -port 8103
```

⚠ **`-token-file` is not optional off-cluster.** It defaults to the pod's secret
mount, so on a machine that has none the binary exits **78** without serving, and
exporting `SUBSYSTEM_STORE_TOKEN` does not help — the flag always carries a
default, so the env fallback is never reached. Single-dash flags: this binary uses
Go's stdlib `flag`, not the client's `--long` style, and `-h` lists all four
(`-store`, `-host`, `-port`, `-token-file`; each default is env-resolved, so what
`-h` prints depends on your environment). It reads the store **from disk** rather
than over HTTP, and authenticates with the same machine token file as the pod.

🔴 **Two backends are absent, for two different reasons, and conflating them is the
misreading to avoid.** Supabase is simply *not wired yet* and returns with the
phase that builds a sign-in flow. The **trusted-header** backend is refused on
principle and is not coming back here: a browser surface exists to be publicly
reachable, and that backend lets anyone who can open a socket to it *be* any user
at full authority. `AuthBackends` takes one parameter so neither can be passed;
`TestTheUIChainHasNoTrustedHeaderMember` pins the exclusion.

It is the only package here that links a third-party module (`gomponents`, for
HTML), and **the serving path is still stdlib-only** — no package the pod or the
CLI links reaches it. What replaced `vendorHash = null`, and how that is measured
rather than asked for, is stated once in `internal/depspolicy`'s package doc; how
this page escapes entry text is stated once in
[`internal/ui/README.md`](internal/ui/README.md). Both are pointers on purpose:
those claims have one home each and a correction belongs there.

## Layout

| path | what |
|---|---|
| `cairn` | the Python client CLI, and the ORACLE the Go one is measured against |
| `lib/` | the Python reader: cache resolution, recall rendering, scope/ref resolution, doctor |
| `server/` | the pod: `server.py`, `Dockerfile`, `seed.sh`, `verify-byte-identity.sh` |
| `cmd/cairn-server`, `internal/api` | the Go port of the server — passes the corpus, not deployed |
| `cmd/cairn`, `internal/client` | the Go port of the CLIENT, and **the default** — diffed against the Python one by `tests/parity/`, which declares both its residuals and the rows that compare only the exit code |
| `internal/report` | the ONE renderer, shared by the pod and the CLI |
| `cmd/cairn-ui`, `internal/ui` | the BROWSER surface — one page, gomponents, deployed by nothing |
| `internal/depspolicy` | the allowlist and import ban that replaced `vendorHash = null` — the serving path is still stdlib-only, and this is what measures it |
| `tests/` | the suites, plus `leakscan.py`, the HTTP conformance corpus, the server dual-run gate and the client parity gate |
| `flake.nix` | both clients (`default` is the **Go** one, `#cairn` the Python one), the server image, the Go server, and the checks |

## How the agreement is measured, and why two of everything is alive

`server/server.py` is the **oracle**; `cmd/cairn-server` is a stdlib-only Go port of
it, deployed by nothing. Rewriting the server alone would have left two renderers in
two languages that must agree byte-for-byte forever, with drift arriving as "a
different order that reads as a stale cache" — no error, no missing entry. So both
land on the same `internal/report`: one renderer, two consumers — the pod and the CLI.
⚠ **The browser surface is not one of them.** `internal/ui` imports `internal/report`
nowhere; it reads `internal/store` and renders HTML through its own code, so the page
is a *second* renderer producing a different medium, and nothing compares the two. That
was a forecast ("a future UI") until `cairn-ui` shipped; it is now a measured exception,
and whether the page should ever route through `internal/report` is open. ⚠ And
one-renderer becomes a *property* only when the Python renderer is deleted at P8; until
then it is a discipline, and these three instruments are what enforce it:

| instrument | what it compares | read it |
|---|---|---|
| [`tests/conformance/`](tests/conformance/README.md) | the served HTTP contract, as golden fixtures generated from the oracle, replayed against either server | the corpus and its declared blind spots |
| [`tests/dualrun/`](tests/dualrun/README.md) | both servers over **one** store — every route, scope, entry and principal, and the snapshot's **uncompressed** tar byte for byte | why gzip identity is unattainable, and the PAX-header divergences a tree comparison cannot see |
| [`tests/parity/`](tests/parity/README.md) | both **clients**, one pod, one cache root, identical argv, across every verb in the table above | the declared residuals, the P8 retirement ledger, and **which rows compare only the exit code** |

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
