# cairn — repository rules

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
| `tests/` | the suites, plus `leakscan.py` |
| `flake.nix` | the packaged client, the server image, and the checks over both |

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
`checks.client-resolves-its-lib` runs a real subcommand from the store path to
catch exactly that, and `packages.cairn` fails its own install check too, so a
broken client cannot even be built.

🔴 **THERE ARE TWO WAYS TO BUILD THE POD AND THEY MUST NOT DIVERGE.**
`server/Dockerfile` is what is deployed today; `packages.server-image` is the
reproducible alternative. The runtime contract — env, port, uid, entrypoint — is
written in both, so `tests/test_flake_image_matches_dockerfile.py` pins them
against each other and goes red when one moves alone. Change one, change the
other, in the same commit. The module set is deliberately *not* duplicated: the
Dockerfile enumerates its `COPY`s (kept honest by
`test_the_image_copies_every_module_it_needs`) while the flake copies all of
`lib/`, so there is nothing there for the two to disagree about.

## Naming

The project is **cairn**. Some identifiers still read `subsystem_store` / `SUBSYSTEM_STORE_*`
— these are **accepted aliases**, kept so existing deployments do not need a coordinated
cutover. New names should use `cairn` / `CAIRN_*`; do not mass-rename the aliases away without
a migration path for people already running this.
