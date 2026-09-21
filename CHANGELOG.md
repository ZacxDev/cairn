# Changelog

User-facing contract changes: what a consumer who pinned this flake gets that
they did not get before, and what they have to do about it.

🔴 **THIS FILE CARRIES THE CHANGE AND THE MIGRATION, NEVER THE RECORD OF WHY.**
The reasoning behind a decision — the drafts that were retracted, the gate that
did not license it, the precondition that closed first — lives in one place
beside the thing it governs, and duplicating it here would make two files each
claim to be the record. That is not hypothetical: `0587ace` evicted exactly this
narrative from `AGENTS.md` *because* `tests/parity/README.md` residual 7 already
held every clause, and its message says "no second copy of the record now exists
to keep true". Keep it that way. **Link, do not restate.**

🔴 **Entries are anchored to a PR and a commit sha, never to a date.** A date
says when somebody looked; a sha says which tree they looked at, which is the
thing a reader can check out and re-measure — and it is the only anchor that
answers the question a consumer actually has, which is *"does this change sit
between the revision I am pinned to and the one I am moving to?"* `git log
<your-rev>..<target-rev>` is the mechanical form of that question.

⚠ **This file starts where the announcements did, not where the project did.**
Changes before the entry below are not reconstructed here and this file does not
claim to cover them; `git log` and the PR history are the record for those.

---

## `packages.default` and `apps.default` became the Go client

**[#50](https://github.com/ZacxDev/cairn/pull/50) → `1a59e59`.**

### What changed

`nix run github:ZacxDev/cairn` and `nix profile install github:ZacxDev/cairn`
now execute `cmd/cairn` (Go) rather than the Python script. A flake input that
reaches `cairn.packages.${system}.default` gets the Go client.

### What you may have to do

- 🔴 **`cairn -verbs` and `cairn -exit-codes` now answer** — exit `0` with a
  table on stdout, where the Python client exits `2` with argparse's `usage:`
  block on stderr for the same argv. This is a single-dash token *succeeding*
  where the default previously **refused**, which is the dangerous direction: a
  caller that relied on the refusal gets a success. Check any wrapper that
  treats a non-zero exit as "unknown flag".
- **Argparse's exact wording is gone** from `--help` and from usage errors. The
  exit codes are identical and gated; the *text* of a usage failure is not.
  Anything parsing argparse's prose needs re-reading.
- **Nothing else moves.** Read and write exit codes, output text for every
  verb, and every documented flag are compared between the two clients by
  [`tests/parity/`](tests/parity/README.md) — read its residual table for the
  rows that compare only the exit code.

### The opt-out, in both consumption modes

Naming `cairn` opts out of all of the above. Nothing was deleted: the Python
client is still built and is still the oracle.

```bash
nix build github:ZacxDev/cairn#cairn           # the CLI spelling
nix run   github:ZacxDev/cairn#cairn -- doctor
```

```nix
# the flake-input spelling — a `#fragment` is not available here
packages.${system}.my-cairn = cairn.packages.${system}.cairn;
apps.${system}.my-cairn     = cairn.apps.${system}.cairn;
```

Both spellings are pinned by `checks.default-is-the-go-client`; the assertions
and their reasoning are in `flake.nix`, beside the wiring they guard.

📄 **Why this was taken, and on what authority:**
[`tests/parity/README.md`](tests/parity/README.md) residual 7. It is the record;
this entry is the migration.
