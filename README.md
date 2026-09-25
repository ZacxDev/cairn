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
  and one of them is a lie. ⚠ **Exit `3` is therefore wider than that one
  state.** A read whose cache exists but cannot be fully read — a scope
  directory or an entry file at mode `000` — exits `3` with `index entry
  unreadable: under <root> (…) — the store was not fully read`. 🔴 **The exit
  code, not the output, is what says whether the report is complete — and that
  is the ONLY half of this you may rely on.** An earlier wording added "and
  prints **nothing on stdout**", which is false in general: see the
  `0 bytes`/`partial` note under the table below.
  🔴 **WHAT YOU ACTUALLY SEE DEPENDS ON THE VERB, AND THIS PARAGRAPH USED TO BE
  WRONG FOR TWO OF THE THREE.** It said such a read "prints `⚠ cairn: cached —
  …` and then exits `3`", which sends a caller hunting for a banner that is
  never emitted. Measured on BOTH clients over a mode-`000` scope directory
  **and** a mode-`000` entry file — twelve runs, the two clients agreeing byte
  for byte in every one, each run naming ONE scope with `--scope`:

  | verb | exit | stdout | the `cached` banner |
  |---|---|---|---|
  | `recall` | `3` | **0 bytes** | **not printed, on either stream** |
  | `search` | `3` | **0 bytes** | **not printed, on either stream** |
  | `validate` | `3` | 0 bytes **for the single scope those runs named** — see below | on **stderr** |

  🔴 **THE `validate` STDOUT CELL WAS WRONG AS A GENERAL CLAIM, AND THIS PR'S OWN
  TESTS ASSERT THE OPPOSITE IN THE SAME COMMIT.** `cmd_validate` prints its
  per-scope line INSIDE the loop, so every scope processed before the unreadable
  one has already emitted output by the time the raise happens. `0 bytes` is true
  only when the failing scope is the FIRST one validated — which is the world all
  twelve runs above used, because each named a single `--scope`. **Re-measured
  with no `--scope` at all**, one cache holding a readable `cairn` (9 entries)
  plus an unreadable scope, `validate --no-sync`, both clients byte-identical:
  unreadable scope sorting **after** → exit `3` with **2,030 bytes** of stdout,
  first line `cairn: cairn: 9 of 9 entry file(s) parse, 0 malformed`; unreadable
  scope sorting **before** → exit `3` with **0 bytes**. The tree already said so
  — `tests/test_cairn_cli.py::TestAScopeThatVANISHESMidRunIsNotServedAsZeroOfZero`
  asserts `"cairn: aaa: 1 of 1 entry file(s) parse, 0 malformed" in stdout` at
  exit 3, and `internal/client/validate_test.go` likewise. **So a supervisor told
  "3 ⇒ 0 bytes" either stops reading stdout or reads a non-empty stdout at 3 as
  corruption; the honest rule is that at exit `3` stdout is a PARTIAL report whose
  length depends on how far the walk got, and the code is what says it is
  partial.** ⚠ One shape neither measurement covered: `search --all-scopes` over a
  partially-unreadable store — that flag refuses `--cache`, so it was not built
  here. **Unmeasured, and not asserted either way.**

  The banner column's cause is ordering rather than policy — the reads print
  their banner *after* the reader returns, so the raise pre-empts it, while
  `validate` prints its banner before it loads anything. **For a read, the ABSENCE
  of the banner is the signal**: a `cached` banner is not a promise of `0`, and no banner at
  all is not a promise that nothing was attempted. Where a banner IS printed it
  says which of the four states the *sync* reached.
  Orthogonally a read names the **scope's status**, so a
  `cached` read can still report `scope-absent` (no such scope here — also what
  a scope your token cannot see looks like) or `scope-unreadable`. An agent that
  cannot tell "nothing is there" from "I could not look" will act on the
  difference.

## Quickstart — two agents, one store

```bash
# Each agent host: point at the pod and its own token.
mkdir -p ~/.config/subsystem-store
cat > ~/.config/subsystem-store/env <<'EOF'
CAIRN_URL=https://store.example.invalid
CAIRN_TOKEN=<this agent's token>
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
nix build github:ZacxDev/cairn#server-image    # the PYTHON pod image — built, published, NOT deployed
nix build github:ZacxDev/cairn#server-image-go # the GO pod image — this is what the cluster runs
nix build github:ZacxDev/cairn#cairn-ui        # the BROWSER surface — published, and DEPLOYED
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

### 🔴 The environment variables are now `CAIRN_*`

Every `SUBSYSTEM_STORE_*` variable has a `CAIRN_*` name. **Both work.** Within one
source the new name wins: the old one is read only when the new one is unset or blank
*there*. An old name that is *present* — including when the new one shadows it — prints
one line per process on stderr naming its replacement. Nothing breaks on the day you
upgrade, and nothing silently half-migrates.

| set this | instead of | what it is |
|---|---|---|
| `CAIRN_URL` | `SUBSYSTEM_STORE_URL` | client: the pod's base URL |
| `CAIRN_TOKEN` | `SUBSYSTEM_STORE_TOKEN` | client: the bearer token · pod: the token-SET fallback when no token file is readable |
| `CAIRN_CONFIG` | `SUBSYSTEM_STORE_CONFIG` | client: where the config file lives |
| `CAIRN_TOKEN_FILE` | `SUBSYSTEM_STORE_TOKEN_FILE` | pod and `cairn-ui`: the token file |
| **`CAIRN_STORE_ROOT`** | `SUBSYSTEM_STORE_ROOT` | pod and `cairn-ui`: the store root |
| **`CAIRN_LISTEN_HOST`** | `SUBSYSTEM_STORE_HOST` | pod: the listen address |
| `CAIRN_PORT` | `SUBSYSTEM_STORE_PORT` | pod: the listen port |
| `CAIRN_TRUSTED_PROXIES` | `SUBSYSTEM_STORE_TRUSTED_PROXIES` | pod: the peer allowlist that makes `CF-Connecting-IP` readable |
| `CAIRN_MAX_FAILURES` | `SUBSYSTEM_STORE_MAX_FAILURES` | pod: failed auths before a lockout |
| `CAIRN_FAILURE_WINDOW_S` | `SUBSYSTEM_STORE_FAILURE_WINDOW_S` | pod: the window they must fall inside |
| `CAIRN_LOCKOUT_S` | `SUBSYSTEM_STORE_LOCKOUT_S` | pod: the lockout duration |

⚠ **Two rows are not the mechanical prefix swap, and copying the pattern instead of the
table will break a pod.** `CAIRN_HOST` was already taken — it is the human-readable
machine *label* that appears in rendered output — so the pod's listen address is
`CAIRN_LISTEN_HOST`. And `CAIRN_ROOT` would read as a sibling of the client-side
`CAIRN_MIRROR_ROOT` when it is the *pod's* store root and not a client-side name at all,
so the pod's store root is `CAIRN_STORE_ROOT`.

**The config file's KEYS count too.** `~/.config/subsystem-store/env` and
`instances/<alias>.env` accept either spelling with the same precedence, and a deprecated
key there warns with a line that names *the file* rather than a `$VAR`, because that is
where you have to go to change it.

🔴 **BUT NEW-BEATS-OLD IS A RULE WITHIN ONE SOURCE, AND THE SOURCES COMPOSE THE OTHER WAY
ROUND. READ THIS BEFORE MIGRATING A CONFIG FILE.** For the **default** instance the whole
environment is consulted first, and only if it yields nothing is the file read — so an
**old name exported beats a new name in the file**:

```
~/.config/subsystem-store/env:   CAIRN_URL=https://new.example.invalid
environment:                     SUBSYSTEM_STORE_URL=https://old.example.invalid
→ the client uses old.example.invalid
```

Migrating the file alone therefore changes nothing while the old variable is still
exported, and the deprecation line — which describes only its own source — will not tell
you so. `unset SUBSYSTEM_STORE_URL` (and `SUBSYSTEM_STORE_TOKEN`) in the same change, or
export the new names too. ⚠ For a **non-default** instance the environment is not
consulted at all, so there the file is the only thing that decides.

🔴 **The pod images set NO store variable, in either spelling — so in a container there
is no image default for your `env:` to argue with.** `server/Dockerfile` and
`packages.server-image`/`server-image-go` used to bake `SUBSYSTEM_STORE_ROOT=/data`,
`SUBSYSTEM_STORE_PORT=8102` and
`SUBSYSTEM_STORE_TOKEN_FILE=/run/secrets/subsystem-store/token`. Every one of those
values was identical to the default the server already falls back to, so they configured
nothing — while tripping the deprecation sweep at every pod start with three lines no
manifest could clear, because the sweep reads the *whole* process environment and an
image `ENV` is part of it. They are gone. What this means for you:

- **Either spelling works in a container, and neither is shadowed.** Set `CAIRN_STORE_ROOT`
  or `SUBSYSTEM_STORE_ROOT` in your Deployment; whichever you set is what the pod reads.
  Set neither and it resolves `/data`, port `8102`, token
  `/run/secrets/subsystem-store/token` — the same three values the image used to state.
- **Migrating your Deployment to `CAIRN_*` now silences the warnings.** It did not before:
  the image's own `ENV` kept emitting them regardless of what your manifest said.
- ⚠ **`docker inspect` no longer documents the store root, port or token path.** That is
  the accepted cost, and **the startup line replaces two of the three, not all three.**
  Both pods print `listening on <host>:<port> store=<root> token-ids=… …`, so the port and
  the store root are readable from the log of a running container. `token-ids=` is the
  credential FINGERPRINTS, not the file they came from — the token PATH appears in no
  startup line on either implementation. Where it is observable: the Go server's
  `cairn-server -h`, which prints `(default "/run/secrets/subsystem-store/token")`; the
  oracle's `--help` does not print its default at all. Both also emit the path to stderr
  in one case only — `token file <path> absent; falling back to $CAIRN_TOKEN` — which is a
  failure notice, not documentation. What keeps the three from drifting is
  `tests/test_flake_image_matches_dockerfile.py`, which pins them against both
  implementations' code defaults.

**When the old names stop being read:** when the Python client (`packages.cairn`) is
retired, which is this arc's P8 milestone. Not a date — there is no semver here to hang
one on (`flake.nix` sets `version = self.shortRev`), and a milestone is something you can
check.

## The client — `cairn`

| you want | run |
|---|---|
| refresh the local cache from the pod | `cairn sync` |
| a scope's digest | `cairn recall --scope X` (`--ref R`, `--list`, `--limit N`, `--page N`) |
| find a hunk by text | `cairn search 'query'` (`--all-scopes`) |
| what the cache actually holds — the ENTRY files, never a scope's `README.md` | `cairn ls-entries` |
| the post-write check: parse, dropped lines, marker reachability | `cairn validate` |
| one call of diagnostics | `cairn doctor` (`--json`, `--no-sync`) |
| append one dated, attributed bullet | `cairn append --scope S --ref R --text '…' --session ID` |
| replace a whole entry behind `If-Match` | `cairn put --scope S --ref R --file F` |
| create a new entry (refuses to overwrite) | `cairn create --scope S --ref R --file F` |
| the scope→instance table, graded | `cairn routes` (`--check`) |

### Exit codes, because the caller is usually a program

Read outcomes and write outcomes are **disjoint**, so a supervisor cannot read
silence as success. `0` content was served (live, cached, or a genuinely empty
scope) · `2` usage · `3` **nothing was read** — the store was unreachable with no
cache, *or* the cache is there and could not be fully read (a mode-`000` scope
directory or entry file), which is a **permissions problem on this host's cache
rather than an outage to retry**: fix the mode, or `cairn sync` to replace the
cache. ⚠ **This clause used to say the state is "a content defect `cairn
validate` diagnoses", and that remedy is a dead end** — measured on both
clients, `cairn validate` over exactly that state exits `3` itself with **0
bytes on stdout**, so it diagnoses nothing the failing read did not already say.
The one thing it adds is the PATH, which every verb already names in the same
sentence. · `4` `sync` did
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

### A sync that is already current does not re-download

`GET /api/v1/snapshot` carries an **`ETag`** — `"sha256:<64 hex>"` over the
*uncompressed* tar — and both clients store it beside the cache in `.sync-etag`
and offer it back as `If-None-Match` on the next sync. A pod that has nothing new
answers **`304`** with no body, and the client keeps the cache it already has:

```
$ cairn sync            # first run
cairn: live — fetched from https://store.example.invalid just now — 7 entries, snapshot seeded=…
$ cairn sync            # nothing changed since
cairn: live — already current at https://store.example.invalid — not modified, snapshot seeded=…
```

Read that line as its own thing. It is still the **`live`** state — the pod was
reached and answered — and it is neither `fetched … just now` (bytes arrived) nor
`⚠ … SERVED FROM CACHE` (the pod could **not** be reached). Exit code, cache and
subsequent reads are unchanged.

⚠ **What it buys, exactly:** the client's download and its extraction. The server
still walks the store and builds the archive to compute the digest, so this is
bandwidth and client CPU, not pod CPU. Nothing is written on a `304`, so a later
`cached` banner still dates the last **download** — it under-states freshness
rather than over-stating it.

You need nothing for this. An older client against a new pod simply sends no
validator; a new client against an older pod stores none.

### One instance, or several

The client is configured by `~/.config/subsystem-store/env` —
`CAIRN_URL` and `CAIRN_TOKEN`, environment variables of the same name winning
(and the old `SUBSYSTEM_STORE_*` spellings still accepted in both places, see
the section *The environment variables are now `CAIRN_*`* above) —
and that instance is called `personal`. A second instance is
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

There is one. It carries the entries page, a browser sign-in with server-side revocable
sessions, the share flow, and a GitHub sign-in through the operator's GoTrue.

⚠ **THE ROUTE COUNT AND THE PHASE COUNT ARE DELIBERATELY NOT WRITTEN HERE, AND THE DELETION IS
THE FIX.** This paragraph said "**three phases**" and "**Seven routes**" and then enumerated
them — and stayed at seven through the change that added three (`POST /sign-in/github`,
`GET /sign-in/github/callback`, `GET /static/app.css`). `cmd/cairn-ui`'s own package comment
identifies this exact hazard, fixed it there, and left the literal seven standing in the file
the fix was about. A number in prose beside a ledger the program DERIVES is a second spelling
that only ever goes stale, so: **`cairn-ui` prints its own count at startup, and
`ui.DeclaredRoutes()` is the list.** What is worth stating instead is the SHAPE, which does not
move when a row is added: `GET /` is the entries page; the sign-in rows and the stylesheet
answer without a session, because they are how you get one; the share rows and `POST /sign-out`
are behind it; and `/healthz` is answered before the chain and is outside the ledger entirely.

🔴 **`cairn-ui` is a SINGLE-REPLICA surface, for TWO reasons and not one.** The
session table is the loud one: each replica holds its own sessions, so a second
replica without shared storage signs users out on a random fraction of requests.
The quieter one is the **control-plane cache** — a share recorded on replica A is
not served by B until B's cache refreshes, which is what the replica-honesty notice
on every share page exists to say. Putting only the session file on shared storage
buys two replicas that keep people signed in and silently disagree about who can
see what. Multi-replica is a later arc, not a configuration.

⚠ **AND THE CLAIM THAT NOTHING DEPLOYED IT IS RETRACTED.** This paragraph read *"And **nothing
deploys it**: `packages.ui-image` builds an image, but nothing publishes that image and there is
no manifest in this repository — it is built and run by hand."* All three clauses are false.
`.github/workflows/publish-image.yml` pushes the UI image to its own ghcr package and then
proves it pullable with no credentials; a manifest deploys it from the operator's GitOps
repository; and the surface is live on a public hostname. "No manifest **in this repository**"
was never evidence about what is running — that is the inference this sweep exists to kill, and
it stood in four places at once.

```bash
nix build github:ZacxDev/cairn#cairn-ui
./result/bin/cairn-ui -store <store root> -token-file <token file> -port 8103
```

⚠ **A credential is required, and off-cluster you must say where it is.**
`-token-file` defaults to the pod's secret mount
(`/run/secrets/subsystem-store/token`), so on a machine without one the binary
exits **78** and serves nothing. Three ways to supply it, all measured:
`-token-file <path>`; `CAIRN_TOKEN_FILE=<path>` with no flag; or
`-token-file=` (explicitly empty) plus `CAIRN_TOKEN=<row>`, which is the
env fallback the binary's own refusal names. Single-dash flags: this uses Go's
stdlib `flag`, not the client's `--long` style. `-h` lists seven — `-store`
(`CAIRN_STORE_ROOT`), `-host` (`CAIRN_UI_HOST`), `-port` (`CAIRN_UI_PORT`),
`-token-file` (`CAIRN_TOKEN_FILE`), `-session-file`
(`CAIRN_UI_SESSION_FILE`), `-session-ttl` (`CAIRN_UI_SESSION_TTL`) and
`-control-journal` (`CAIRN_UI_CONTROL_JOURNAL`) — and every default is env-resolved,
so what `-h` prints depends on your environment. It reads the store **from disk**
rather than over HTTP, and authenticates against the same token file as the pod.

🔴 **"Env-resolved" does NOT mean "the variable and the flag are interchangeable", and
`CAIRN_UI_CONTROL_JOURNAL` is where that matters.** An unset variable and an explicitly
EMPTY one both mean "no control journal" — the shape a manifest that emits every variable
with an empty default produces. A value that reduces to **nothing but whitespace** is
refused at **78**, naming the variable, rather than read as unset: read as unset this
surface would come up on the token-file projection, which confers `admin` on nobody, so
every scope page answers 404 and no share can be recorded while `/healthz` still answers
200. That is the same ruling the pod makes for its own `CAIRN_CONTROL_JOURNAL`. The **flag**
path refuses the same value for a different reason — `-control-journal '   '` has always
failed its `stat` — so the two arrival paths agree for every value that does **not name an
existing file**, which they did not between `1659663` and `68cf955`.

⚠ **THEY DO NOT AGREE ON A VALUE THAT DOES, AND THE FLAG IS THE LENIENT SIDE — MEASURED,
NOT INFERRED.** With a control journal in the working directory whose filename is literally
three spaces, `-control-journal '   '` **serves**, announcing
`sharing writable (control journal    )`, while `CAIRN_UI_CONTROL_JOURNAL='   '` exits
**78** naming the variable. The flag never meets the blank policy at all — it meets
`openAuthority`'s `stat`, which is a question about the filesystem and not about the
spelling — so "the two paths refuse the same set" is true of every value anyone would
type and false in general. **It is left open, and the reason is a judgement rather than an
argument from the mechanism**: the fix is available and obvious — run the same blank policy
over the flag's value too — and it was not taken because the direction is safe (the flag is
the LENIENT side, so nothing is refused that should be served), because naming a file with
nothing but whitespace is not a thing an operator does, and because a check on the flag
would refuse a path the filesystem resolves. Nothing gates it; the agreement test's own
docstring says which values it covers.

The other six variables above are resolved by
`internal/envalias`, which reads a whitespace-only value as **absent** and silently takes
the code default; `tests/conformance/README.md` measures what each one does with one.

🔴 **Two backends are absent, for two different reasons, and conflating them is the
misreading to avoid.** Supabase is simply *not wired yet* and returns with the
phase that builds a sign-in flow. The **trusted-header** backend is refused on
principle and is not coming back here: a browser surface exists to be publicly
reachable, and that backend lets anyone who can open a socket to it *be* any user
at full authority. `AuthBackends` takes one parameter so neither can be passed;
`TestTheUIChainHasNoTrustedHeaderMember` pins the exclusion.

### Sharing a scope with somebody

`GET /share` lists the scopes your credential may administer; `GET /share?scope=<id>`
is one scope's page. It answers three things: **who has access to this**, **which of
those you can take back**, and a form to share it with somebody.

🔴 **"Who has access to this" is computed from the authority, not from the list
of shares.** Access arrives two ways — a share, or membership of the project that owns
the scope — so a page that listed only shares would under-report every project
member, and in the direction that tells you your notes are more private than they
are. The two lists are shown separately because only shares can be revoked:
somebody who reaches a scope through project membership keeps it after every share
is taken back, and the page says so.

⚠ **You can only share with people and projects you already share a project with.**
That is deliberate — a picker listing every user would turn admin on one scope into
a directory of everyone in the deployment — and it means reaching anybody else needs
an invite, which does not exist yet.

🔴 **THE SHARE FLOW NEEDS `-control-journal <path>`, AND NOT ONLY TO WRITE.** Without one
the authority is the token file, which has no shares to record and **confers `admin` on
nobody** — so no scope is administrable, `GET /share` renders "No scope is administrable
by this credential", and a scope page answers **404 to every caller**. The page says on
every load that this deployment cannot record a share, so the cause is visible rather
than discovered at a click; but read the limit at its real width — without a journal the
share flow has nothing to show, not merely nothing to change.

⚠ An earlier draft of this paragraph claimed "the share pages still answer *who can see
this*" without one. That is **false**, and it was measured false rather than argued: with
a token-file authority, 0 scopes are administrable across every principal in the
projection and the scope page is a 404. The retraction is kept because the sentence was
plausible enough to survive writing it.

Every share page carries a notice about what this surface can and cannot promise: it
is one replica's answer from a cached copy of the authority, another reader gains or
loses access when their own cache refreshes rather than the instant you click, and
revoking stops future syncs without recalling entries already copied onto somebody's
machine. That notice is pinned **whole** by a test, so it cannot be quietly reworded
into a stronger promise.

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
| `server/` | the ORACLE pod: `server.py`, `Dockerfile`, `seed.sh`, `verify-byte-identity.sh` — no longer what runs |
| `cmd/cairn-server`, `internal/api` | the Go port of the server — passes the corpus, and **the deployed pod** |
| `cmd/cairn`, `internal/client` | the Go port of the CLIENT, and **the default** — diffed against the Python one by `tests/parity/`, which declares both its residuals and the rows that compare only the exit code |
| `internal/report` | the ONE renderer, shared by the pod and the CLI |
| `cmd/cairn-ui`, `internal/ui` | the BROWSER surface — pages, sign-in (credential form **and** GitHub through the operator's GoTrue), the share flow, gomponents; published, and **DEPLOYED** |
| `internal/depspolicy` | the allowlist and import ban that replaced `vendorHash = null` — the serving path is still stdlib-only, and this is what measures it |
| `tests/` | the suites, plus `leakscan.py`, the HTTP conformance corpus, the server dual-run gate and the client parity gate |
| `flake.nix` | both clients (`default` is the **Go** one, `#cairn` the Python one), the server image, the Go server, and the checks |

## How the agreement is measured, and why two of everything is alive

`server/server.py` is the **oracle**; `cmd/cairn-server` is a stdlib-only Go port of
it, **and it is what the cluster runs** — this line read "deployed by nothing" until
the cutover. Rewriting the server alone would have left two renderers in
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

The project is **cairn**. Identifiers that are not environment variables still read
`subsystem_store` — the `lib/` module names, `~/.config/subsystem-store/`,
`/run/secrets/subsystem-store/`, the `subsystem-recall` CLI alias. Those are accepted
aliases, kept so existing deployments don't need a coordinated cutover. The
environment variables have been renamed; see the migration note below.
