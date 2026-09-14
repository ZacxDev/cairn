# The HTTP conformance suite

This is the instrument that turns "we ported it carefully" into a measurement. It
records the store API's contract as **HTTP-level golden fixtures** generated from
the Python server, and replays the same declared requests against any
implementation — the oracle today, an unmodified Go binary at P1.

It speaks HTTP and nothing else. `cases.py` and `wire.py` import no part of
`server.py` and no part of the client; `oracle.py` is the one module that knows
the oracle is Python, and it knows only how to build the fixture store and start
a subprocess.

```bash
python3 tests/conformance/suite.py generate          # re-record every golden
python3 tests/conformance/suite.py run               # replay against the oracle
python3 tests/conformance/suite.py run --base-url http://127.0.0.1:8102 \
                                       --token-file /path/to/tokens
python3 tests/conformance/suite.py normalizations    # the declared licences to differ
python3 tests/conformance/suite.py build-store DIR   # materialise the world
```

`tests/test_conformance_suite.py` runs the whole thing under pytest, including
the negative controls.

## The pieces

| path | what |
|---|---|
| `world.json` | the fixture store: files, **mtimes**, the seed stamp, a scope's `.git/HEAD`, and the principals |
| `requests.json` | the declared request list — one row per case, plus the three relations |
| `golden/<case-id>.json` | one recorded response per case |
| `cases.py` | loads and VALIDATES the list; holds the route ledger |
| `wire.py` | issues a case, normalizes the answer, records or reads a golden |
| `oracle.py` | builds the world, mints the tokens, boots `server/server.py` |
| `suite.py` | the generator, the runner, the relational assertions, the leak guard |
| `mutate.py` | puts a deliberately wrong server behind the suite |

## Fixtures are GENERATED, never hand-written

A hand-edited golden asserts what somebody believed. Three things enforce that:

1. **Regeneration is one command** — `suite.py generate`.
2. **Every golden records `body.sha256`**, and `read_golden` refuses a file whose
   recorded lines do not hash to it. A partial hand edit is a loud error naming
   the regeneration command.
3. **A test regenerates the whole set and diffs it against what is committed**
   (`TestTheGoldensAreGenerated`). If the two disagree, the committed goldens are
   not what the generator produces, and the test says so.

Bodies are stored as **lines**, so a golden that moves moves one line in the
diff rather than rewriting a single escaped string.

### The store cannot be checked in

Git stores no mtime, and this contract depends on mtimes: `/api/v1/snapshot`
preserves them with sub-second precision because the reader orders its index
newest-first by entry mtime. So the store is **built from `world.json`** on every
run, and `alpha-notes/gadget-one.md` and `gadget-two.md` deliberately share a
whole second and differ only in the fraction — that is the one input shape a
normalized tar destroys.

### The tokens are minted per run

A real-looking 58-character credential committed to a public repository is a
finding whether or not it ever authenticated anything, and `tests/leakscan.py`
refuses one on sight. `world.json` declares **principals**; `oracle.py` mints a
fresh token per principal per run and writes the token file. The request list
names principals symbolically, and the runner maps a name back to whatever token
the server under test was configured with by reading the same token file the
server reads. No response in this contract contains a token.

## The server under test must be configured like this

The goldens do not apply to a server started any other way:

| env | value | why |
|---|---|---|
| `SUBSYSTEM_STORE_TRUSTED_PROXIES` | `127.0.0.1/32` | the suite connects over loopback, so loopback is the trusted proxy and `CF-Connecting-IP` is honoured |
| `SUBSYSTEM_STORE_MAX_FAILURES` | a large number | **see below** |
| `CAIRN_HOST` | `conformance-oracle` | keeps the real hostname out of a report body. Defence in depth only — the normalization is what makes a golden host-independent |

🔴 **`MAX_FAILURES` is the one that would silently destroy a run.** The lockout
answers the *same uniform 401* a bad token does — that is the design — so once it
trips, a correctly authorized request also answers 401 and matches no golden
except by accident. This corpus issues fifteen deliberate refusals from one
client address, and the six carrying a wrong-or-absent credential are the ones
the limiter counts — over the production default of five per minute. (A non-API
path and a missing client IP are deliberately *not* counted.) The raised ceiling
is kept honest by the **canary**: `put-no-precondition` is
authenticated, touches no store state, and is re-issued as the last request of
every run. If it stops matching its golden, either a lockout tripped or the run
mutated state a read depends on, and nothing after that point can be trusted.

## Three cases send literal request bytes

`raw_request` rows write the request line themselves over a socket and read the
response to EOF. Three reasons a row needs it, and each is a real case:

- a request line an HTTP client cannot express (`GET http://[ HTTP/1.1`, and
  `GET\r\n\r\n`);
- a response with **no status line and no headers** — the server really does
  answer HTTP/0.9 when `parse_request` never established a version, so the
  `unauthorized` body arrives with no framing at all. Recorded as its own shape;
  deliberately **not** a member of the uniform-401 set, because it is
  distinguishable from a bad token on the wire (it reveals nothing about the
  store, so this is recorded rather than called a defect);
- 🔴 **a body whose length was not dictated by `Content-Length`.**
  `raw-recall-read-to-eof` asks for `Connection: close` and reads to EOF, which
  is the only way this suite can observe a mis-framed report. Through
  `http.client` the header decides how many bytes are read, so comparing the two
  asks the client whether it agrees with itself — measured vacuous for 95 of 97
  cases before this row existed.

A raw line may need the run's own credential, which is minted per run:
`{{<name>}}` is substituted with that principal's token. The braces are not
cosmetic — `tests/leakscan.py` refused the first spelling of that placeholder
(`__PRINCIPAL_wide-reader__`: 25 characters of the credential charset after the
word `Bearer`), which is the gate working rather than a false positive.

## Reads before writes

A row's `phase` is `write` if the case **may change the store**, and `read`
otherwise. `cases.py` refuses a `read` row placed after the first `write` row,
because a successful write moves entry mtimes and every report body carries
`entry-files=N` and `newest=<mtime>`. Within the write phase the order is the
list's order, and each write case targets an entry no other write case changes.

## Every normalization is declared, per field, with its reason

Run `suite.py normalizations` for the authoritative table; there is deliberately
no "ignore headers" switch. Two properties keep the set honest:

- **A declared normalization that matches nothing is an error.** A row that keeps
  a normalization it no longer needs stops comparing that field, invisibly.
- **What is *not* normalized is written down too** (`wire.NOT_NORMALIZED`), so a
  reader can tell "measured deterministic" from "nobody looked".

The short version:

| field | treatment | why |
|---|---|---|
| `Date` | dropped, every case | required by HTTP/1.1, and it is the moment of the response |
| `Content-Length` on `/snapshot` | dropped | the length of a gzip stream is a property of the compressor build; a Go implementation must be free to differ |
| `Content-Length` on a **report** (`recall`/`search`, GET and HEAD) | dropped | 🔴 the value counts the bytes the server sent, and a report body names the machine and the store path — both variable in **length**, not merely in value. See the determinism note below: two runs on one host agree, so this was invisible until a second host label was measured. What replaces it: the body stays pinned line by line (no fact about the report's *content* is lost, only the redundant restatement of its length); `raw-recall-read-to-eof` measures a report's length independently of the header; and `head-matches-get` pins that a HEAD reports its GET's length. Kept verbatim on every other case |
| `ETag` on the append | masked after its SHAPE is checked | the revision hashes content that embeds the server's UTC date. Every other ETag hashes bytes the request sent or an untouched fixture file, and is pinned literally |
| `  store: <path>` | replaced | a temporary directory here, `/data` in the pod |
| `  host: <identity>` and the `scope-absent` sentence | replaced | the report names the machine on purpose; the value differs per host and embeds a prefix of `/etc/machine-id`, which must not be committed to a public repo |
| the appended bullet's leading `- YYYY-MM-DD: ` | replaced | stamped from the server's UTC clock. Only the leading date — the `[cairn: actor/session]` trailer stays pinned |

The snapshot is compared as its **extracted tree** — member paths, contents,
modes, and mtimes to sub-second precision, in archive order — plus `mtime_order`,
the member names sorted newest-first, which is the ordering the reader derives.
The raw archive bytes are not a golden: the gzip member header carries the
compression time.

## Three properties are RELATIONSHIPS between responses

A per-response golden cannot see any of them. Each case has its own file, and each
would keep passing if one of them grew a distinguishing header, because whoever
regenerated the goldens would have recorded the divergence.

- **A refused scope answers exactly what a never-existed scope answers**, for the
  same caller, in the same run — on `recall`, `search`, `snapshot`, `POST
  .../bullets` and `PUT`. The single licence is the scope NAME, which a report
  echoes; every other byte and every header must match.
- **The uniform 401**: a bad token, no token, a missing client IP, a duplicated
  one, a non-API path (even with a valid credential), an unhandled verb and an
  unparseable request target are byte-identical.
- **A HEAD reports the `Content-Length` its GET would have sent.** Neither value
  can live in a golden (see the table above), so the claim is made between the two
  answers in one run. The mutation it exists for is the naive one: no body, so
  report no length.

`generate` checks all three and **refuses to record** a corpus that violates any
of them — otherwise the first divergence would be baked in and the suite would
then defend it.

## Determinism is proven, on the dimensions that move

`generate` was run twice against a freshly-built world and the 98 goldens were
byte-identical, to each other and to what is committed. That check is permanent
(`TestTheGoldensAreGenerated`) — but on its own it is **structurally blind to
anything that depends on the machine**, because both runs share one.

So the dimensions were measured at a second point as well:

| dimension | second point | result |
|---|---|---|
| wall clock | the two runs, seconds apart | identical; the date normalization is separately measured against two different dates in `TestNormalizations` |
| host identity | `CAIRN_HOST` set to a longer label | **found a real defect** — 21 goldens moved, because `Content-Length` counts the un-normalized body. Fixed, re-measured identical, and pinned by `test_a_different_host_label_produces_the_same_goldens` |
| store path | `TMPDIR` set to a much longer directory | identical (same mechanism as the host label, same fix) |

## What this suite CANNOT see

Named rather than omitted, because a green run is a claim about the cases it
encodes:

- **A route added after the fixtures were generated.** This is the structural
  blind spot of anything that replays a recorded list. It is **closed for the
  Python oracle**: `cases.declared_routes` reads `API_ROUTES` and `WRITE_ROUTES`
  out of `server/server.py` by AST, and `validate_corpus` fails when the declared
  set grows *or* shrinks against what the list addresses. It is **not closed for
  a non-Python implementation** — there is no source for that function to read,
  so P1 has to add the equivalent ledger on the Go side. The AST reader is also
  blind to a route dispatched from anywhere other than those two module-level
  dict literals.
- **Concurrency.** Every case is one request at a time, in a declared order. The
  commutativity and idempotency of two writers appending to one entry, the entry
  lock, the audit lock, and the listen backlog are all invisible here;
  `tests/test_subsystem_store_api.py` is where they are tested.
- **Cache staleness.** The client's local cache, `cairn sync`, and the reader's
  behaviour against an extracted snapshot are the client's contract, not the
  server's.
- **Anything requiring a real datastore.** The world is a filesystem store built
  from a declaration. The control plane's Postgres, its authz cache, its epochs
  and its grant log do not exist yet and are not modelled.
- **The rate limiter and the lockout.** They are configured OUT of the way (see
  above) precisely so the rest of the corpus is reachable. What the suite does
  assert is that the lockout did not trip *during* the run — that is the canary.
- **The audit log.** One line per `/api/*` request on the server's stdout. It is a
  real contract and it is not an HTTP surface, so a suite that speaks HTTP cannot
  reach it for a server it did not start.
- **Connection REUSE, and everything that depends on it.** Every case opens its
  own connection and closes it. The body-draining that stops an unread entity
  body being parsed as the next request, the `Connection: close`-on-every-non-200
  rule's *effect*, and request smuggling generally need a second request on one
  socket, which this corpus never sends. The `Connection` header itself is
  recorded and pinned; what it causes is not.
- **Chunked transfer encoding, and every other framing this server refuses.** The
  runner always sends `Content-Length`.
- **Header order.** Recorded sorted. `_respond` emits a fixed order, RFC 9110
  gives it no meaning, and pinning it would fail a correct implementation.
- **The raw snapshot bytes**, for the reason above.
- **TLS, the gateway, and anything a proxy does.** The suite talks plain HTTP to a
  loopback socket.
- **SIGHUP token reload, startup refusals, and every exit-code path.** Those are
  process behaviour, not request/response behaviour.
- **A hostname shorter than four characters**, for the leak guard: a
  three-character host name is a substring of ordinary English, so the short case
  is left uncovered rather than wrongly covered.

## Validating the instrument

A reassuring zero is indistinguishable from a harness wired to nothing.

- **Negative control** — `mutate.py` copies `server/server.py`, applies one
  realistic mutation, and the suite must go red. Covered in
  `TestTheSuiteGoesRedOnARealMutation`: a status code moved, a header dropped,
  the snapshot's mtimes truncated to whole seconds, its members reordered, a bad
  query parameter silently defaulting. `TestTheRelationsCannotBeRegeneratedAway`
  covers the three mutations a fresh set of goldens would otherwise absorb: a
  refused scope that leaks the scope revision, a 401 that says why, and a HEAD
  that reports a length of zero.
- **Positive control** — `mutate.unmutated_server` performs the same copy with no
  edit and must pass, so a red is the mutation and not the harness. And a
  mutation pattern that matches nothing is an **error**, not a survived mutant.
- **The counts are reported, never inferred.** The runner's summary line names
  `requests=`, `assertions=` and `failures=`, and every case gets its own
  `PASS`/`FAIL` line to be counted. `0 failures` over 0 requests is the failure
  mode being guarded against.
