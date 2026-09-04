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
- a dated incident reference (`2026-08-29: …`). Keep the mechanism, drop the particulars;
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

## Naming

The project is **cairn**. Some identifiers still read `subsystem_store` / `SUBSYSTEM_STORE_*`
— these are **accepted aliases**, kept so existing deployments do not need a coordinated
cutover. New names should use `cairn` / `CAIRN_*`; do not mass-rename the aliases away without
a migration path for people already running this.
