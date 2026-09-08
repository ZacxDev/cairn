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
directory, not two), which puts an HTTP server, a telnet server and several
network clients — `wget`, `nc`, `ftpget`, `ftpput`, `tftp`, `nslookup` — into a
pod that mounts a credential at `/run/secrets/subsystem-store/token`, none of
which the deployed image has. Against that: the pod runs as uid 65532, no
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
