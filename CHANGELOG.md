# Changelog

User-facing contract changes: what a consumer who pinned this flake gets that
they did not get before.

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

**[#50](https://github.com/ZacxDev/cairn/pull/50) → `1a59e59`.** Announced in
[`README.md`](README.md); this is the record behind that announcement.

### What changed

`nix run github:ZacxDev/cairn` and `nix profile install github:ZacxDev/cairn`
now execute `cmd/cairn` (Go) rather than the Python script. A flake input that
reaches `cairn.packages.${system}.default` gets the Go client.

Two behaviours differ, and one of them widens the contract:

- 🔴 **`cairn -verbs` and `cairn -exit-codes` now answer** — exit `0` with a
  table on stdout, where the Python client exits `2` with argparse's `usage:`
  block on stderr for the same argv. This is a single-dash token *succeeding*
  where the default previously **refused**, which is the dangerous direction:
  a caller that relied on the refusal gets a success. Declared as residual 7 in
  [`tests/parity/README.md`](tests/parity/README.md). Whether these become
  documented public surface or move behind a gate is a **P8 decision that has
  not been taken**.
- **Argparse's exact wording is gone** from `--help` and from usage errors. The
  exit codes are identical and gated; the *text* of a usage failure is not, and
  the parity gate deliberately does not compare it. Anything parsing argparse's
  prose needs re-reading.

### The opt-out, in both consumption modes

Naming `cairn` opts out of all of the above. The CLI spelling is a `#fragment`
(`nix build github:ZacxDev/cairn#cairn`); the flake-input spelling is the
attribute `cairn.packages.${system}.cairn`, and `cairn.apps.${system}.cairn` for
a re-exported app.

⚠ **The flake-input spelling was missing from the first draft of the
announcement, and a round-0 audit caught it.** `README.md` calls flake-input the
primary consumption mode, so the consumers most affected by the flip had been
handed an opt-out they could not use. Both spellings are now pinned by
`checks.default-is-the-go-client`, which asserts `packages.default ==
packages.cairn-go`, `apps.default == getExe packages.default`, `apps.cairn ==
getExe packages.cairn`, and `packages.cairn != packages.default` — at evaluation
time, building neither client, so a compile failure cannot redden it and read as
"the default moved".

🔴 **`apps.cairn` was unpinned until #50's fix round, and the near-miss is the
part worth keeping.** Repointing it at the Go client left **all three** of the
guard's original assertions green while the announced escape hatch silently
became the Go client — a guard written for attribute A not covering sibling
attribute B, defeating a promise the same PR created. Closed by the fourth
assertion.

### Why it was taken, and on what authority

🔴 **On an operator decision, not on a green gate.** An earlier draft took the
flip on the gate alone and was **reverted**; that reading stays wrong however
green the gate gets. A differential gate says the two clients agree — it cannot
say that changing which one a stranger executes is a change anyone wants.

The decision followed the closure of parity residual 8
([#48](https://github.com/ZacxDev/cairn/pull/48) → `181053a`), which removed the
one **measured** blocker: every read verb refused at exit `11` on a
multi-instance host, so the `doctor` quickstart in `README.md` would have refused
there.

### What it did **not** do

- **Nothing was deleted.** The Python client, its `lib/`, and its packaging are
  untouched. `packages.cairn` still builds it.
- **It did not make "one renderer" true.** The Python renderer still ships until
  the oracle is deleted at P8. Until then the parity gate *is* the comparison,
  rather than the absence of one.
- **It did not retire the `lib/`-beside-the-script rule** in `AGENTS.md`. That
  rule governs the client that is still shipped and still the oracle; it stops
  governing anything on the day `packages.cairn` does.
