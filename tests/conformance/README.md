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
| `CAIRN_TRUSTED_PROXIES` | `127.0.0.1/32` | the suite connects over loopback, so loopback is the trusted proxy and `CF-Connecting-IP` is honoured |
| `CAIRN_MAX_FAILURES` | a large number | **see below** |
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

## Four rows are asserted against the ORACLE ONLY

🔴 **Some recorded behaviour is CPython or `http.server` ARTIFACT, not designed
contract**, and the corpus has to be able to say which. A row marked `oracle_only`
is asserted against the Python server and **skipped — by id, with its reason, counted
in the summary — for any other implementation**. `cases.py` refuses a mark with no
reason, and refuses a reason with no mark.

⚠ **IT IS THE LAST RESORT AND NOT THE FIRST.** A response differing in ONE FIELD
belongs in `wire.NORMALIZATIONS`, which keeps every other byte pinned for both
implementations. This mark stops the whole case being compared, so whatever the row was
covering has to be covered somewhere else and the reason must say where.

| row | the artifact | where the contract half is covered |
|---|---|---|
| `raw-malformed-request-line` | HTTP/0.9: `parse_request` never established a version, so `send_response_only` suppresses the status line and every header | nowhere else — the property is "it does not crash", and a framed 400 from a port satisfies it |
| `raw-malformed-absolute-target` | the request line never reaches a handler in a server whose framework parses it first; Go's `net/http` answers its own 400 in `readRequest` | **nowhere** — the answer comes from the framework's parser before any port code runs, so there is nothing to test; the security property holds because a framed 400 names no scope |
| `post-bullets-not-json` | the body quotes CPython's `json` diagnostic (`Expecting property name enclosed in double quotes: line 1 column 2 (char 1)`) | `write.TestDecodeBulletBodyRefusesRatherThanCrashing` and `api.TestAMalformedBodyIsAnsweredAndNotDropped` pin the 400, the `X-Store-Status`, the message prefix, and that the connection survives |
| `post-bullets-deeply-nested-json` | the same, plus a defect (`RecursionError` out of `json.loads`) that a parser returning an error instead of unwinding the stack cannot reproduce | the same two Go tests |

Both raw rows were ALREADY excluded from the uniform-401 relation for the first row's
reason ("it reveals nothing about the store, so this is recorded rather than called a
defect"); the second now carries the same reasoning. When a member of a relation is
skipped the runner says so on its own line and names how many members are left, and a
relation left with NO members is a **failure** rather than a pass.

```bash
python3 tests/conformance/suite.py run --base-url … --token-file …                     # skips them, loudly
python3 tests/conformance/suite.py run --base-url … --token-file … --oracle-specific assert
```

The second form is for a `--base-url` pointing at a hand-started **oracle**. Omitting
`--base-url` boots the oracle and always asserts.

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

### 🔴 …AND A RELATION WITH NO NON-5xx MEMBER IS A FAILURE, NOT A PASS

A between-responses claim is satisfied by two answers that are identically WRONG. That
is not a defect in the claim — "these two are the same" really is what it asserts — but
it is how one gets BELIEVED: measured at P1a, `refused-equals-absent` and
`head-matches-get` both reported PASS for the two report routes while **all four members
answered `501 not-implemented`**, because a not-implemented answer is beautifully
uniform.

P1a's remedy was a caveat printed on the line. That annotates a PASS; it does not
withhold one, so a reader still had a green verdict arguing with a prose footnote.
`refused-equals-absent` and `head-matches-get` now **refuse to vouch** for a member set
in which nothing answered below 500 — `suite._any_real_answer` is the predicate, and both
relations consult it before comparing anything:

- **one** real answer is enough — the relation's job is comparing them, not grading them;
- the floor is **500 and not 400**, because a 4xx refusal IS an answer and the uniformity
  of those refusals is part of the contract. Only a 5xx says the server did not answer
  the question, and every 5xx here is uniform *by design* (it names no scope), which is
  exactly why two of them compare equal for free;
- the caveat stays, for the case it really covers: two members can fail their goldens
  while still being real 200s whose SAMENESS is a genuine measurement.

Watched to work rather than reasoned about, in both tiers:

| control | result |
|---|---|
| a Go build whose report routes answer 500, replayed through `run_go.sh` | `FAIL relation refused-equals-absent recall / search` and `FAIL relation head-matches-get recall / search` — while `snapshot`, `append` and `replace` still PASS, so the guard is not blanket-failing |
| `TestARelationCannotBeSatisfiedByTwoSERVERERRORS` | every pair fails at 500, 501 and 503; 200, 404 and 428 all pass; a MIXED 503/200 pair is still COMPARED and fails on the comparison, not on this guard |

The head-pair control feeds **matching** lengths on purpose: the old claim is satisfied
and the relation must refuse anyway. Mismatched lengths would go red for the other
reason and prove nothing about this guard.

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
  set grows *or* shrinks against what the list addresses. There is no source for
  that function to read in a compiled binary, so the Go side carries its own
  ledger instead — `api.DeclaredRoutes()`, checked against `requests.json`, against
  the wiring at construction, and against its own spelling by
  `checks.go-server-declares-its-routes`, which reads it out of the RUNNING binary.
  (This paragraph said "P1 has to add the equivalent ledger"; P1a added it, and a
  future tense left on a finished thing reads as an open gap.) The AST reader is
  still blind to a route dispatched from anywhere other than those two
  module-level dict literals.
- 🔴 **MOST OF WHAT THE REPORT RENDERER DOES.** The corpus sends the bodies
  `requests.json` declares, and none of them carries an openness marker, a
  near-miss marker, a `tasks:` key, a duplicate heading, a fenced region, a scope
  over the 100-line index page, an ambiguous ref, a bare entry, a
  present-but-EMPTY section, an entry with no `## What it is`, an honoured
  `sensitivity:`, an EXACT mtime tie, a name-only search hit, a sub-threshold near
  miss, a fuzzy/prefix/substring match, a joined compound term, a `--max-hits`
  truncation, a malformed entry BESIDE readable ones, a `search-unreadable`
  scope, or an EMPTY allowlist. Every one is a branch, and **measured**: with only
  the corpus-driven tests, 13 of 40 mutations to the Go renderer SURVIVED.
  `internal/report/testdata/reader_fixtures.json` is where that is covered — the
  oracle's own rendered bytes over 50 such cases, generated by
  `tests/reader_fixtures.py`. It is a sibling instrument, not part of this corpus:
  it compares one function's output and knows nothing about HTTP.
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
- 🔴 **EVERY USTAR HEADER FIELD A PAX EXTENDED RECORD OVERRIDES.** The snapshot is
  compared as its **extracted tree**, and every POSIX reader prefers an extended
  record over the ustar field it shadows — so the comparison normalises the header
  away before it happens. Measured as four real divergences against CPython's own
  writer, all invisible here and to every reader: the ustar `mtime` field is
  `round(val)` there (half-to-**even**) and was `int64()` truncation in the Go
  writer, which differs at `.75` always and at `.5` on an odd second; the extended
  records are emitted in dict-insertion order (`path` before `mtime`) and were
  emitted `mtime`-first; a **non-ASCII member name** gets a `path` record at any
  length there (the ASCII test runs before the length test) and got one only above
  100 bytes; and the header **checksum** moves with the first of those. 193 bytes
  across 7 header blocks, behind a green extracted-tree comparison.
  **Byte-diff the two archives when you touch `internal/snapshot/paxtar.go`** —
  after the fix they are byte-identical over a member list carrying all four
  classes. `tests/dualrun/README.md` records why the byte-identity gate is scoped
  to the *uncompressed* tar (gzip identity is unattainable, measured) and names
  the member list that writer was proved byte-identical over.
- **A `seeded=` value that is not printable ASCII.** `world.json` declares an ASCII
  seed stamp, so no case reaches the three accidents a non-ASCII one produced on the
  oracle — one of which **truncated the response after the status line**. Fixed in
  the oracle and matched in Go; covered by
  `TestSnapshotStamp::test_a_stamp_that_cannot_GO_IN_A_HEADER_is_UNREADABLE` and by
  `snapshot.TestFreshnessNamesEveryFailureState` instead. Regenerating all 98
  goldens after that fix moved **none** of them, which is the same statement from the
  other side.
- **TLS, the gateway, and anything a proxy does.** The suite talks plain HTTP to a
  loopback socket.
- **SIGHUP token reload, startup refusals, and every exit-code path.** Those are
  process behaviour, not request/response behaviour.
- **A REQUEST BODY THAT IS NOT VALID UTF-8, AND AN ESCAPED NON-BMP CHARACTER.** Both
  measured as real divergences against a Go port while all four CI jobs were green, so
  this entry is evidence rather than caution. The runner builds every body from
  `requests.json`, and no row carries either shape:
  - a body with a byte that is not valid UTF-8 — the oracle refuses it with a 400
    (`body.decode("utf-8")` is strict and runs BEFORE `json.loads`), while a decoder
    that replaces the byte answers `200 appended` and writes a permanent U+FFFD into a
    curated entry;
  - a `\uD83D\uDE00`-style surrogate PAIR — which is what `cairn append` puts on the
    wire for any astral character, because `json.dumps` defaults to
    `ensure_ascii=True`. A guard that cannot tell a pair from a lone surrogate 400s
    every emoji the shipped client sends.
  Both now have Go-side regression coverage with a red-at-baseline matrix. 🔴 THE
  LESSON GENERALISES: the corpus pins the bytes it was told to send, so a decoding
  difference between two implementations is exactly the class it is blind to. A port's
  own tests own that half.
- **A hostname shorter than four characters**, for the leak guard: a
  three-character host name is a substring of ordinary English, so the short case
  is left uncovered rather than wrongly covered.
- 🔴 **AN ENVIRONMENT VARIABLE THAT IS PRESENT BUT EMPTY**, which is the AUTHORISED
  exception below. Two independent reasons, so closing one would not help: no key in
  `requests.json` carries an environment at all — the corpus describes REQUESTS and the
  rule decides STARTUP — and the three variables it is about never reach the server as
  variables anyway, because `oracle.py` passes `--store`, `--host` and `--port` as FLAGS,
  which override the defaults under test. `ORACLE_ENV`'s three entries are all non-empty.
  So a green corpus is not evidence about that rule and never will be unless somebody
  adds such a case. Its guards are named in the section below, one per implementation.

## 🔴 An AUTHORISED exception to "do not change the oracle" — a present-but-empty value is ABSENT

**Decision (operator, this session, on PR #69): `env_aliases.value` treats a variable
that is PRESENT BUT EMPTY as ABSENT, in the oracle as well as in the client.** The
standing P1 rule is that the oracle is the golden source and is never edited to make the
port agree; the standing exception is a defect a contract cannot contain, granted by the
operator, per site, in writing. The precedent is `seeded=UNREADABLE` on both servers —
authorised "because a contract cannot include 'sometimes truncate the response
mid-stream'"; `tests/parity/README.md` records the second, `cmd_validate`'s negative
count. **Here: a contract cannot include "the same blank value means two different
things in two implementations of one server".**

**It NARROWS the oracle toward what Go already did.** Every Go call site tested `!= ""`
before this PR existed (`envOr`, `envInt`, `netid.LimiterSettings`, `authz.LoadTokens`),
and so did the Python client's `load_config`. The oracle's `main()` was the outlier:

| a blank `CAIRN_PORT` / `CAIRN_STORE_ROOT` | oracle, before | Go, before and after | oracle, now |
|---|---|---|---|
| `--store` | `""` — serves a store root nothing named | falls through to the deprecated name, else `/data` | same as Go |
| `--port` | `ValueError: invalid literal for int() with base 10: ''` | falls through, else 8102 | same as Go |

So it removes a divergence rather than creating one, which is why "authorise and declare"
was chosen over reverting it on the oracle.

⚠ **THE EXCEPTION IS THAT RULE AND NOTHING ELSE.** It does not license editing the oracle
anywhere else, and both spellings of the resolver carry the same statement in a comment.

🔴 **"BLANK" MEANS WHITESPACE-ONLY TOO, AND THAT IS A WIDENING OF THIS EXCEPTION RATHER
THAN A RESTATEMENT OF IT — DECLARED HERE FOR THE SAME REASON THE ORIGINAL WAS.** The rule
first shipped with blankness spelled INLINE at each of two sites, and the two disagreed:
`deprecations` tested `.strip()`, the resolver returned the OLD name's value raw. So
`SUBSYSTEM_STORE_ROOT="  "` resolved to `"  "` — a pod would have taken a whitespace store
root — *and* warned about nothing, contradicting this file's own "a blank value changes no
resolution". Both implementations had the identical defect, so `tests/parity/` compared
them equal and could not see it. It is closed by one named predicate per language
(`blank` / `_blank`) read by both halves, and the half that MOVED is resolution:

| a whitespace-only `SUBSYSTEM_STORE_ROOT` | oracle at `f74657d` | oracle+Go, before | oracle+Go, now |
|---|---|---|---|
| `--store` | `"  "` | `"  "` | falls through to `/data` |
| a deprecation warning | n/a | none | none |

The alternative — warn on any non-empty old value, whitespace included — was rejected
because it keeps a resolved value no operator can have meant. **The cost is the same one
the section below already records, one step wider:** a manifest that sets a store root to
whitespace now relocates writes quietly rather than serving a nonsense path. Watched RED
at `78679b9` on the `"  "` and `"\t"` rows in both languages, with the `""` row green
throughout as the control that the predicate was not simply inverted.

🔴 **THE COST, WHICH IS REAL AND WAS TAKEN DELIBERATELY.** A blank store root now resolves
to a default instead of failing, so a manifest bug that BLANKS it relocates writes quietly
rather than loudly. That is the trade: the old oracle's `""` and `ValueError` were at
least loud on the pod's own startup line. Measured while proving the guards below can go
red — with the rule removed on the Go side the server came up and printed `store=` with an
empty value rather than refusing, so an empty store root is not loud on either
implementation today. Nothing in either program treats "blank" as an operator error.

⚠ **AND ONE INTERACTION THAT USED TO BE HERE IS GONE, WHICH IS WORTH MORE THAN THE
INTERACTION WAS.** This paragraph read: both pod images bake the deprecated spelling on
purpose, so in a container `CAIRN_STORE_ROOT=""` falls through not to the code default but
to the image's `SUBSYSTEM_STORE_ROOT=/data`. **Neither image sets any store variable any
more** (`README.md`, § *The environment variables are now `CAIRN_*`*; `flake.nix`'s
`serverEnv`; `server/Dockerfile`'s `ENV`), so a blank resolves to the code default in a
container exactly as it does anywhere else — and the values are the same `/data` and `8102`
the image used to state, pinned against both implementations by
`tests/test_flake_image_matches_dockerfile.py`. One fewer place where the rule means
something different depending on where the process runs.

🔴 **THE CORPUS IS BLIND TO THIS, AND THAT IS MEASURED RATHER THAN ASSUMED** — see the
last bullet of *What this suite CANNOT see*: no `requests.json` key carries an
environment, and `oracle.py` passes `--store`/`--host`/`--port` as flags, which override
the very defaults the rule decides. **A green corpus is not evidence about this rule.**

🔴 **WHAT COVERS IT, ON BOTH SIDES, BECAUSE THE CORPUS CANNOT.** Both servers print
`listening on <host>:<port> store=<root> …`, so both guards read the RESOLVED values out
of a running process rather than out of the resolver — a unit test on `value_or` stays
green while `main()` stops calling it, which is the seam nobody owns. Each boots its
server with the three current names present-but-EMPTY and the deprecated spellings
carrying the real values, and passes no `--store`/`--host`/`--port` flag, since a flag
would override the thing under test.

| arm | guard | label | watched RED at |
|---|---|---|---|
| oracle | `tests/test_env_aliases.py::TestABlankOLDNameOnTheORACLE` | **regression coverage** — the only arm here that attributes | the real pre-change tree, `git show f74657d:server/server.py`, one half at a time and with the base-era `SUBSYSTEM_STORE_TRUSTED_PROXIES` supplied so nothing dies for a neighbour's reason: blank `SUBSYSTEM_STORE_ROOT` → base prints `store=` empty against the expected `store=/data`; blank `SUBSYSTEM_STORE_PORT` → `ValueError: invalid literal for int() with base 10: ''` while the parser is being BUILT, which a `--port` flag does not rescue |
| oracle | `tests/test_env_aliases.py::TestABlankValueIsTreatedAsAbsentByTheORACLE` | **INVARIANT GUARD** — relabelled; see below | — |
| Go | `cmd/cairn-server::TestABlankEnvironmentValueIsTreatedAsABSENT` | **INVARIANT GUARD** — Go never had the other behaviour, so it is not evidence that anything was fixed | `envOr` made to prefer a present-empty value → `listening on :<port> store=` ; `envInt` likewise → `CAIRN_PORT must be a number, got ""`. Two separate mutants, each killed by this guard's own message |

🔴 **THE SECOND ROW WAS CLAIMED AS REGRESSION COVERAGE AND WAS NOT, AND THE CORRECTION IS
WORTH MORE THAN THE ROW.** It blanks the CURRENT names and supplies the deprecated ones —
but at `f74657d` the oracle has no `CAIRN_*` handling at all, so blanking `CAIRN_STORE_ROOT`
there exercises nothing. Measured against the real base tree with the base-era trusted-proxy
spelling supplied, it prints **exactly the string it asserts** — green. Its red appeared only
because the fixture named `CAIRN_TRUSTED_PROXIES`, which base does not know, so the base
oracle refused with `no trusted proxies` **before `main()` ever evaluated a store root or a
port** — a mutant dying for the wrong reason, under a message that said "a present-but-empty
value was NOT treated as absent". The earlier "RED ON THE PRE-CHANGE ORACLE" claim was
measured against MUTANTS of HEAD, not against `f74657d`, and the two are not the same claim.
The row stays, relabelled, because it still pins the deprecation window's own behaviour and
because it is the control that stops a `/data`-hardcoding mutant surviving the first row.

Every fixture value is one no constant under test can equal — a temporary directory
against `/data`, a kernel-assigned port asserted unequal to 8102, `127.0.0.1` against a
`0.0.0.0` default — and each arm runs a SECOND world and asserts the printed values MOVE.
A fixture whose only possible output is the default's own value cannot see a mutant that
hardcodes the default, and would survive a fully green suite.

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

## The mutation battery over P1 — 56 mutants, 52 killed, 4 labelled equivalent

Moved out of `AGENTS.md`, which every session pays for before it has been told
anything: this is a record of rounds, not decision input before acting, and a
survivor's authority is the label beside the code it labels. The four survivors are
labelled EQUIVALENT at the code, with the reasoning, because three of them corrected
a comment that was wrong.

🔴 **THE MUTATION BATTERY: 56 mutants over two targets, 52 KILLED, 4 SURVIVED — and every
survivor is LABELLED EQUIVALENT AT THE CODE, with the reasoning, because three of them
corrected a comment that was wrong.**

**46 over the RENDERER**, in three rounds: round 1 killed 24 of 40 and its 13 survivors are
what built the fixture above; round 2 killed 40 of 46; round 3 killed 43 of 46 and is clean.
The three correct survivors: `sort.SliceStable` → `sort.Slice` (the comparator is a total
order); `1e-9*nsec` → `nsec/1e9` (**measured bit-identical at every realistic mtime
magnitude** — the ULP of the sum dwarfs the difference, and the old comment claimed the
hazard was reachable); and disabling `difflib`'s extension loops (**with an empty junk set
the DP has already found the longest contiguous run, so neither loop can advance** — the old
comment said two of the four "can run", which is two more than can).

**10 over `store.ScopeRevision` AND THE WARNING SINK**, the two surfaces P1b added with one
branch of coverage each: 9 killed by the guard's own test, 1 labelled equivalent. That round
also found a defect in the new code — `readGitText` stripped the WHOLE file where the oracle
reads `packed-refs` unstripped, which removed the last line's trailing whitespace and made
the per-field strip unreachable from a fixture whose matching row came last.

## Also relocated from `AGENTS.md`: the rest of P1's measured record

Same reason as the battery above. `CLAUDE.md` imports `AGENTS.md`, so every session in this
repository pays for both before it has been told what the work is, and
`tests/test_agent_instructions_weight.py`'s eviction playbook names this file as P1's
destination. What stayed in `AGENTS.md` is what binds the next edit — the byte-identity
gate's scope, "a green corpus is not a green port", the relation-with-no-real-answer rule,
the route ledger, the pinned Go toolchain, stdlib-only, the two divergences a reader has to
know about, and the open one's closing condition. What is below is the evidence for those,
which is read on demand. Relocated at `752415d`.

### The corpus split, measured at P1a and again at P1b

Measured on the tree at `752415d`: the Go server answers **116 PASS, 0 failing cases, 0
failing relations, 4 rows skipped** as oracle-specific (the four in "Four rows are asserted
against the ORACLE ONLY" above); the oracle answers **0 failures, 0 skipped**. At P1a the
split was **94 PASS and 22 failing cases** — every one of them a `/api/v1/recall/{scope}` or
`/api/v1/search/{scope}` **rendering** case, which is what P1b closed.

The later number is only meaningful beside the earlier one: 94 was a port with no renderer
in it, and 116 is the same corpus against one that has it.

### The renderer's differential fixture — 50 cases over a 122-entry world

`internal/report/testdata/reader_fixtures.json` holds the **oracle's own rendered bytes** for
**50 cases over a 122-entry synthetic world**, generated by `tests/reader_fixtures.py` and
replayed by `internal/report`'s tests.

```bash
python3 tests/reader_fixtures.py generate     # re-record from the oracle's reader
python3 tests/reader_fixtures.py print <case> # read one case's expected bytes
```

It exists because no row of this corpus carries any of the branches enumerated under "MOST
OF WHAT THE REPORT RENDERER DOES" above — and that is measured, not asserted: with only the
corpus-driven tests, **13 of 40** mutations to the Go renderer survived.

Three properties keep it honest, each the same shape as this corpus':

- `tests/test_reader_fixtures.py` **regenerates and diffs**, so a stale fixture is a failure
  rather than a weaker comparison;
- `TestTheFixtureCoversTheSHAPESTheCorpusCannotSend` is a **ledger over the rendered
  output**, keyed on strings only each branch can produce, so the covered set cannot shrink;
- two tables measure the functions no rendered case can reach properly —
  `difflib.SequenceMatcher.ratio()` over **20 pairs** including the autojunk boundary, and
  CPython's `round(x, 3)` over **19 values** including the `.xx5` boundaries.

⚠ The fixture is in `flake.nix`'s `onlyGo` filter for the same reason `requests.json` is —
leave it out and the sandbox tier's `go test` goes red naming it, which is the good
direction and still worth saying.

### Why the two deliberate reader divergences were accepted

`AGENTS.md` names both, because a reader comparing two audit streams has to know they are
there. The reasoning is here.

| where | the difference | why |
|---|---|---|
| `store.ScopeRevision` on a `.git/HEAD` that is not valid UTF-8 | ONE `400` audit line in Go, **two** (`200` then `400`) on the oracle | there the strict decode raises while the response's arguments are being evaluated, after the 200 line is already written. Reproducing a mid-response raise to duplicate a log line is a worse trade than naming it |
| `store.ScopeRevision` resolving a `ref:` | `filepath.Join` CLEANS, so a `ref:` naming `../…` cannot climb out of the git dir; the oracle's `git / ref` can | a NARROWING, in the safe direction. A HEAD pointing outside its own repo is not a revision worth reporting |

### …and a third row of that table, CLOSED by its own stated condition

`report.ErrFocusSelectorUnported` refused a non-empty focus window, on the stated grounds
that "the store API never sends one" — **true of the pod and FALSE of the CLI**, which is
what P2 is. `cairn recall` with no `--scope` and the default `--mode` builds a window out of
the repo's newest handoff doc and passes it straight through, so the refusal was reachable
from the commonest invocation of the commonest verb and a Go client could not answer
`recall` at all.

The condition recorded for deleting it was "`associate_paths` ported with a red-at-baseline
differential test". `store.AssociatePaths` is that port; seven `digest-focus-*` rows in
`reader_fixtures.json` carry the ORACLE's own rendered basis for it (a filename-tier hit, an
alias-tier hit, a miss, an ambiguous ref, the mtime tie-break, the `…` truncation and the
sourceless arm), and two more — `digest-focus-count-beats-mtime` and
`digest-focus-ref-is-the-last-resort` — exist because a mutation sweep found the ranking's
PRIMARY key and its LAST resort unreachable from the other seven. The error and its guard
were deleted together, as promised, rather than left as a branch no caller can reach.

### The one OPEN divergence, worked: an integer query parameter wider than `int64`

`AGENTS.md` carries the divergence and its closing condition, because an open item somebody
has to satisfy is decision input before acting. This is the evidence under it.

`_int_param` is `int(v)`, which is arbitrary precision; `intParam` is `strconv.Atoi`, which
is not. **Measured live on both servers over one world**, not derived from reading:

```
GET /api/v1/recall/alpha-notes?page=999999999999999999999
  oracle 200: INDEX (from index) — no entries: page 999999999999999999999 is past the end …
  go     400: bad request: page must be an integer, got '999999999999999999999'
GET /api/v1/recall/alpha-notes?limit=999999999999999999999
  oracle 200: INDEX (from index) — ALL 2 entries in `alpha-notes/`, none omitted …
  go     400: bad request: limit must be an integer, got '999999999999999999999'
```

It is a P1a-era parsing difference that only became OBSERVABLE at P1b, because before the
renderer existed both answers were refusals — which is the general shape worth keeping: **a
difference between two validation ladders is invisible for as long as everything past the
ladder is a refusal.** This corpus cannot see it either; the runner builds its targets from
`requests.json` and no row carries a 21-digit parameter.

⚠ It was recorded and NOT fixed in the same change as the renderer, deliberately: widening
`intParam`'s range is a change to the validation ladder, and P1b's whole claim was that the
ladder did not move.

