# The P2 parity gate

```bash
python3 tests/parity/harness.py              # the gate
python3 tests/parity/harness.py --self-test  # prove the differ can go RED
python3 tests/parity/harness.py --only recall-digest --keep   # one row, world kept
```

🔴 **A GREEN RUN IS NOT EVIDENCE UNTIL BOTH CONTROLS HAVE BEEN WATCHED TO WORK, and this harness
is the reason that rule exists.** Its first full run reported **72 PASS, 0 FAIL** while every
single request was refused `401 status=no-client-ip`, no cache was ever written, and every report
rendered `store-unreachable` — because `SUBSYSTEM_STORE_TRUSTED_PROXIES` had been copied from the
conformance runner, where `127.0.0.1/32` is correct, into a harness whose clients connect
*directly*. Two clients failing identically compare equal. Three controls now stand against that,
and they are three different claims:

| control | what it proves | how it reads |
|---|---|---|
| `PREFLIGHT status=… declared-entries=…` | the POD answers a snapshot for this token, non-empty | exit **2** ("could not vouch"), not 1, if it does not |
| `CONTENT-FLOOR live-banner=… rendered-digest=…` | the CLIENTS got as far as a live fetch and a rendered digest | exit 2 if either is false |
| `--self-test` → `SELF-TEST sabotaged=3 caught=3` | the differ can go RED on stdout, on stderr **and** on the exit code | exit 2 if any sabotaged row is not reported |

The three sabotage rows are separate on purpose: `compare="exit"` rows are **structurally blind**
to stdout and stderr, so one control over "something went red" would vouch for a differ that had
lost two of its three comparisons.

## What is compared

Both clients run against **one** pod, **one** store and **one** cache root, with identical argv
and identical environment, and the harness diffs **stdout, stderr and the exit code**. Nothing is
recorded: the oracle runs live, so the two cannot agree with a snapshot of an older Python client
and have it called parity.

The store is restored from a pristine copy **before every client run**, mtimes included — three
write verbs change it, the two clients run in sequence, and without that `create-ok` had the
oracle create the entry and the Go client legitimately answer `already-exists`. A row that needs
an earlier write declares `setup` instead of relying on row order.

`cache-mtime-parity` is a separate, structural claim: each client syncs into its **own** root and
the trees are compared as the *reader* sees them (`float(sec) + 1e-9*nsec`, which is what
`report.pyMtime` computes). The rendered order is what *caught* a dropped mtime; this is what
*pins* it, because a rendered order can agree by accident of three files landing in the right
sequence while every timestamp is wrong.

## Declared differences — the residuals, named rather than normalised away

| # | difference | why it is not closed |
|---|---|---|
| 1 | **argparse's usage text.** Unknown flag, missing required flag, unknown subcommand, non-integer `--limit`, `doctor --scope`, bare invocation: the oracle prints argparse's `usage:` block and its wording; the Go client prints its own sentence. | Reproducing argparse's layout, its prefix abbreviation (`--sc` → `--scope`) and its exact phrasing in Go is a second implementation of a library nobody reads twice. **The exit code is 2 on both** and that is the half a caller branches on, so those rows are `compare="exit"` — declared per row, never applied silently. |
| 2 | **`urllib` vs `net/http` failure text.** `<host> unreachable: <reason>` has a `urllib` tail on one side and a Go tail on the other; the oracle distinguishes a DNS/connect failure (`URLError.reason`) from a socket timeout (a bare `OSError`) where Go returns one `*url.Error` for both. | The host and the word `unreachable` — what a reader greps and what a human needs — are identical. Matching the tails would mean transcribing two libraries' error strings, which change with their versions. Rows that reach these are `compare="exit"`. |
| 3 | **Sub-microsecond mtime.** The oracle's `tarfile` carries a PAX mtime as a Python **float** and `os.utime` writes it back, losing precision Go's exact decimal parse keeps: measured `…236263` (oracle) vs `…236300` (Go) on the seed stamp, 37 ns. | **Measured unable to matter.** Both collapse to the same `float64`, which is the value the index order is decided on: one ULP is 238 ns at the current epoch — measured at four magnitudes (year 2000 → 119 ns, now → 238, 2038 → 238, 2100 → 477). `cache-mtime-parity` asserts the doubles are identical **and** that the raw delta stays under that ULP, so a future change that widened it fails here rather than reordering an index. |
| 4 | **A reader error's exit route.** A missing store root or an unreadable entry exits **3** on the Go client, naming the error. The oracle raises out of its subcommand and prints a traceback at exit **1**. | The Go behaviour is the CONTRACT the reader documents (3 is "the store is broken"); the oracle's 1 is an uncaught exception. Reproducing a traceback would be reproducing a defect. No row reaches it — the world is always readable — so it is declared, not measured. |
| 5 | **A `SyntaxError` in the reader's own modules.** On the oracle a present-but-unparseable `lib/cairn_doctor.py` takes every verb down at exit 1; the Go client has no such failure mode. | It is a property of loading Python at runtime and cannot exist in a single binary. The oracle's own comment says widening its `except ImportError` is *not* the obvious fix. |
| 6 | **`ReadStamp` has no "is not text" arm.** The oracle distinguishes an unreadable stamp from one that is not valid UTF-8, because `read_text` raises; Go's `ReadFile` returns bytes and cannot fail on encoding. | The stamp is written by this program and is ASCII, so the arm is unreachable in practice. Re-validating the bytes to manufacture the distinction would be inventing a check the oracle only has by accident of its API. |

## What the gate structurally cannot see

- **Concurrency.** Both clients take the same `flock` around the cache swap, which is why they can
  share a root at all; nothing here runs them at the same instant.
- **Real network failures.** An unreachable pod is a connect refusal to a closed port. A DNS
  failure, a mid-transfer reset and a half-open socket are not built, and see difference 2.
- **A narrowed credential.** The token's allowlist names every scope the world holds. The
  *server's* narrowing is the conformance corpus's claim, not this one's — but note that a refused
  scope and an absent one are byte-identical by design, so a client cannot tell them apart either
  way.
- **The `doctor` states no world reaches.** An unreadable local root (`token-scopes` UNMEASURED
  with `unread`) needs a mode-000 directory, which a root-run CI job would not honour.
- **Anything after the pod answers 5xx from a real fault.** The world is healthy; `503` is only
  reached through a refused scope, which this token does not have.
