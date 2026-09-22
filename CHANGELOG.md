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
| `cairn-ui` gained a **share flow** — three new routes (`GET`/`POST /share`, `POST /unshare`) and a `-control-journal` flag that SWITCHES the authority from the token-file projection to a `control.FileStore` | [#64](https://github.com/ZacxDev/cairn/pull/64) → `7d7c9ea` | Nothing unless you run `cairn-ui`. If you do: without `-control-journal` the share pages serve a notice and every scope page answers 404, because the token-file projection confers `admin` on nobody; with one, the binary now **refuses to start** (exit 78) unless the journal holds a live, attributable credential — and **at that anchor** no tool in this repository wrote one, which is why the refusal's own text said so; `cairn-server -issue-credential` closed that afterwards and the refusal now names it | [`internal/ui/README.md`](internal/ui/README.md) § *Phase C* |
| The Python client **runs on interpreters older than the tar-filter backports** again — `install_snapshot` called `tar.extract(..., filter="data")` unconditionally, which is a `TypeError` before 3.12 / 3.11.4 / 3.10.12 / 3.9.17 / 3.8.17, so `sync` died with a traceback instead of one of the client's own exit codes | [#70](https://github.com/ZacxDev/cairn/pull/70) → `2f33e80` | Nothing if every host you run on is 3.12. If you **fetch this client by sha into an image** and run it there, a pin at or before `080bb2d` cannot sync on an older interpreter — move past this one | The floor was undeclared, and structurally invisible to every host that runs the client interactively; the fallback's reasoning — and why it is not the same call minus the guard — is at `HAS_TAR_FILTERS` and the `install_snapshot` extract site |
| The build gained its **first third-party dependency** — `maragu.dev/gomponents`, brought in by the new `packages.cairn-ui` — so `flake.nix` passes a real `vendorHash` where it passed `null` | [#55](https://github.com/ZacxDev/cairn/pull/55) → `91389ae` | Nothing, unless you audit what you build — then read what the old guarantee was and what replaced it: [`internal/depspolicy`](internal/depspolicy/depspolicy.go) | [`internal/depspolicy`](internal/depspolicy/depspolicy.go)'s package doc |
| `packages.default` and `apps.default` became the **Go** client — `nix run` and `nix profile install github:ZacxDev/cairn` now execute `cmd/cairn` | [#50](https://github.com/ZacxDev/cairn/pull/50) → `1a59e59` | [`README.md`](README.md), § *The default client is now the Go one* — the `-verbs`/`-exit-codes` widening, the loss of argparse's wording, and the opt-out in both consumption modes | [`tests/parity/README.md`](tests/parity/README.md) residual 7 |
