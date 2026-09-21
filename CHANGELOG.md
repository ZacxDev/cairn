# Changelog

An **index** of user-facing contract changes: what a consumer who pinned this
flake gets that they did not get before, one line each, newest first.

🔴 **THIS FILE IS AN INDEX, NOT A SECOND COPY. LINK, DO NOT RESTATE.** Two
rounds of review found the opposite shape here and both were right. A change's
*migration* — what you must re-check — belongs in `README.md`, which
`AGENTS.md` names as the announcement surface and which a consumer actually
reads; its *record* — the drafts retracted, the gate that did not license it,
the precondition that closed first — belongs beside the thing it governs. When
`0587ace` evicted that record from `AGENTS.md` its message said
`tests/parity/README.md` residual 7 already held every clause, so "no second
copy of the record now exists to keep true". Restating either one here would
recreate exactly that.

🔴 **Entries are anchored to a PR and a commit sha, never to a date.** A date
says when somebody looked; a sha says which tree they looked at — and it is the
only anchor that answers the question a consumer actually has, which is *"does
this change sit between the revision I am pinned to and the one I am moving
to?"* `git log <your-rev>..<target-rev>` is the mechanical form of that
question.

⚠ **This file starts where the announcements did, not where the project did.**
Changes before the entry below are not reconstructed here and this file does not
claim to cover them; `git log` and the PR history are the record for those.

---

| change | anchor | what you may have to do | why it was taken |
|---|---|---|---|
| The build gained its **first third-party dependency** (`gomponents`, for the new `packages.cairn-ui`), so `go.mod` has a `require` block and `flake.nix` passes a real `vendorHash` where it passed `null` | [#55](https://github.com/ZacxDev/cairn/pull/55) → `91389ae` | Nothing, unless you audit what you build: the pin is no longer "no third-party code, checkable in one glance". **The serving path is still stdlib-only** — `internal/depspolicy` is the allowlist plus import ban that measures it, and no package the pod or the CLI links reaches the new module | `internal/depspolicy`'s package doc |
| `packages.default` and `apps.default` became the **Go** client — `nix run` and `nix profile install github:ZacxDev/cairn` now execute `cmd/cairn` | [#50](https://github.com/ZacxDev/cairn/pull/50) → `1a59e59` | [`README.md`](README.md), § *The default client is now the Go one* — the `-verbs`/`-exit-codes` widening, the loss of argparse's wording, and the opt-out in both consumption modes | [`tests/parity/README.md`](tests/parity/README.md) residual 7 |
