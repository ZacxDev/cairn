# The P1 dual-run gate — two servers, one store, every route

```bash
python3 tests/dualrun/harness.py                       # the generated store (mode 2)
python3 tests/dualrun/harness.py --store ~/some/store  # the operator's own (mode 1)
python3 tests/dualrun/harness.py --self-test           # seven mutants, each on its own arm
python3 tests/dualrun/harness.py --break-both          # the PRE-FLIGHT's control (rc 2)
python3 tests/dualrun/harness.py --positive-control    # the count MOVES with the store
python3 tests/dualrun/harness.py --only entry:wide-writer:alpha-index/widget-cfg --keep
```

This is **step two of the three-step sequence** `AGENTS.md` fixes for the server port: the Go
server passes the conformance corpus (step 1), **both servers run over one store and
byte-identity is compared** (step 2, this), the client is ported (step 3), then Python is
retired. Steps 1 and 3 were green while this was outstanding, and neither implies it.

**It is also the instrument behind an instruction `AGENTS.md` already gives.** That file
tells a reader to diff the two pod images for what the agreement test cannot read *before
swapping the deployed one*. Until this gate existed there was nothing behind "and the two
servers agree" except a green corpus over a declared world.

## Measured on this tree

| | mode 1 — the operator's own store | mode 2 — the generated store |
|---|---|---|
| scopes / entry files | 18 / 161 | 9 / 123 |
| indexed entries | 148 | 119 |
| targets | **612** | **361** |
| comparisons | **2,492** | **1,489** |
| **differences** | **0** | **0** |
| audit lines compared | 634 | 375 |
| process-stream lines compared | 2 | 20 |
| wall time | ~100 s | ~5 s |

🔴 **The pair, never the zero alone.** `differences=0` over `comparisons=0` is what a
harness wired to nothing prints, so the count is reported beside it and the controls below
are what make the count mean something.

**The uncompressed tar is byte-identical on a real-shaped store**: over 161 real entry files
with real sub-second mtimes, a scope that is a git repo and one that is not, and three
principals' differently-narrowed candidate sets, `/api/v1/snapshot`'s tar compared equal
byte for byte on every request. That was the open question this gate was built to answer.

## 🔴 Byte-identity here means the *uncompressed* tar

Scoped deliberately, and the scoping is **measured rather than chosen**. The measurement was
relocated here from `AGENTS.md` at `752415d` — it is a record of what was measured, and
`AGENTS.md` is paid for by every session before it has been told anything, so it keeps the
imperative ("the gate is scoped to the uncompressed tar; do not add a gate on the gzip
bytes") and this file keeps the evidence.

`/api/v1/snapshot` ships `tarfile.open(mode="w:gz")` output on the oracle and
`compress/gzip` output in Go, and the two cannot be made equal at any setting. Two
independent reasons, so closing one does not help:

- **the 10-byte gzip header.** Go's `compress/gzip` hardcodes the OS byte to `0xff`
  (unknown) with no API to change it; CPython writes `0x03` (Unix). At level 9 the XFL
  bytes agree (`02`) and the OS bytes still differ.
- **the DEFLATE stream itself**, which differs in LENGTH and not merely in content —
  512 bytes from Go against 511 from zlib at level 9 on one 20,480-byte tar.
  `compress/flate` and zlib make different match and block choices; that is a permitted
  freedom of the format, not a defect in either.

The tar *inside* the gzip IS achievable, and was achieved: after the header fixes in
`internal/snapshot/paxtar.go`, the Go writer's archive was byte-identical to CPython's
`PAX_FORMAT` output over a member list carrying a whole second, `.25`, `.5` on an even
second, `.5` on an odd one, `.75`, a non-ASCII name and a name over 100 bytes.

So the gzip envelope — and `Content-Length` on that one route, which counts the gzip bytes —
is a declared difference, and **the tar inside it is compared byte for byte, in archive
order, including every PAX extended record**. Not as an extracted tree: that comparison is
what hid four header divergences (`tests/conformance/README.md` records them), because every
POSIX reader prefers an extended record over the ustar field it shadows. The member-level
walk in this harness runs only to *diagnose* a byte difference, never to decide one.

⚠ **Do not read "byte-identical" as including the gzip envelope.** It does not, it cannot,
and a future reader asking why should find this paragraph before they go looking for a bug.

## What is compared

Six comparisons — the **arms** — and the self-test attributes every mutant kill to one of
them:

| arm | what it compares |
|---|---|
| `status` | the status code **and the reason phrase** |
| `headers` | the whole header set, name spellings included, order not asserted (RFC 9110 gives it no meaning) |
| `body` | the response body, byte for byte |
| `tar` | `/api/v1/snapshot`'s **uncompressed tar**, byte for byte |
| `audit` | the audit stream, line for line, in order — **no other gate in this repository reads it** |
| `process` | everything else the two servers wrote: the startup banner, the legacy-mode warning, and the reader's own `*-unreachable` warning line |

Plus one relational arm that is not a comparison of two answers to one question: **the
ref-set arm**, which reads each scope's index from *both* servers and compares the ref
lists before the per-entry sweep runs. Taking the list from one side only would make a ref
that one server indexes and the other does not invisible — the sweep would never ask.

### The target matrix

All **8 dispatch entries** are covered: `GET`/`HEAD` × `recall`/`search`/`snapshot`, plus
`POST entry` and `PUT entry` — `HEAD` reaches all three read routes and `PUT entry` is one
route carrying two operations keyed on the precondition header.

- **per scope** (18 targets, plus a 19th on any scope that indexes an entry): the digest, `mode=list`, `mode=full` at a tight and a
  generous `limit`, `limit=1`, `page=2`, a page past the end, an invalid `mode`, `HEAD`, a
  search hit, a search miss, a tuned search (`threshold` + `max_hits` + `context` at once),
  a **stricter** threshold, `max_hits=1`, `context=0`, an explicit `all_scopes=0`, a search
  `HEAD`, a `?scope=`-narrowed snapshot, and — added after enumeration — **a search whose
  term is a ref the scope really indexes**, which is the only search target guaranteed to
  HIT on a store nobody wrote the fixtures for. 🔴 **Every parameter appears at two or more
  distinct values, and that is a guard rather than a habit** — a mutation sweep moved
  `threshold=0.3` to `0.6` (the *default*, so the parameter stops changing anything) and
  survived a check whose sentence was only about the name appearing. One value compares the
  two implementations' **parsing** and nothing about the parameter's **effect**, which is
  the half a shared renderer would not have given you;
- **per scope × principal**: the narrowed reader and the legacy bare row on *every* scope,
  so the narrowing is compared on scopes inside **and** outside its allowlist;
- **per entry**: one `?ref=` render for every indexed ref, for the wide principal and for
  the narrowed one over its own scopes. This is what `server/verify-byte-identity.sh` does,
  and it is what reaches `resolve_ref_tiered`;
- **store-level**: the whole-store snapshot for each of the three principals, `HEAD`, a
  `?scope=` naming nothing, an unsafe `?scope=`, `all_scopes=1` wide and narrowed, the
  absent scope on both report routes, every rung of both validation ladders, a repeated
  query parameter (last wins), an unsafe path component, no-route, a non-API path with a
  valid credential, `/healthz`, an unhandled verb, a write verb on a read-only route, a bad
  token, no token, and a malformed `Authorization` header;
- **write phase** (19 targets, store restored around every request): an append that lands,
  an append whose body supplies a hostile `actor` (accepted and discarded — the *rendered
  trailer* must name the authenticated identity, which is a body claim), an escaped astral
  surrogate pair, an over-cap bullet, a non-JSON body, an empty body, an unknown ref, a
  legacy row (forbidden to write), the **refused/absent scope pair from both principals'
  sides**, and every precondition branch of the PUT: none, a stale `If-Match`, `If-Match: *`,
  both headers, an `If-None-Match` list, a create over a ref that exists — and, the two that
  make it a write phase rather than a refusal phase, **a create that lands at 201** and **a
  replace whose `If-Match` each server derives from the entry's own current revision**.
  🔴 Three write statuses — `appended`, `replaced`, `created` — must be reached on **both**
  servers or the run refuses to vouch; see "what this gate found".

### Three principals, one of them narrower than the store

`wide-writer` (every scope), `narrow-reader` (two scopes of nine in mode 2; the store's
first two in mode 1), and a **legacy bare row** — unrestricted on reads, forbidden to write.
A gate that only ever sent an unrestricted credential would compare the happy path and
nothing about the narrowing, and the narrowing is where the most recent real defect lived.
In mode 1 the narrowed allowlist is **derived from the store** and forced to be a proper
subset, because a fixed list would name scopes a real store does not hold and
`narrow-reader` would then see nothing — every narrowing target comparing two identical
refusals, which is the vacuous-green shape inside the arm added to avoid it.

## Two modes, and they are not substitutes

**Mode 1 — `--store <path>`.** Answers "are the two servers identical on *my* data". The
store is **copied** (mtimes verified to the nanosecond) and the copy is what is served, so
the write routes cannot mutate the operator's notes; the copy is restored around every write
request. ⚠ What the copy costs: it is made by this process as this user, so a file the
operator cannot read is a loud copy failure rather than a served `store-unreachable`, and
anything the filesystem will not reproduce (an exotic mode, an ACL, a hardlink's identity)
is not part of what is compared. Both servers read the *same* copy, so no difference can
make the comparison unfair — only narrower than the real disk.

🔴 **Mode 1's output names the operator's scopes, and this repository is public.** The path
is never printed; the per-target lines carry scope names because a verdict nobody can
attribute is not a verdict. Mode 1's output belongs nowhere near a commit, a CI log or a
pull request. Mode 2's output is the publishable one, which is why CI runs mode 2.

**Mode 2 — the generated store.** Deterministic from a seed, so a failure reproduces.

### What mode 2 covers that the conformance corpus does not

The corpus replays 98 declared cases against a world of 2 real scopes and 6 entries. Mode 2
builds 9 scopes and 123 entry files, and every one of these is served here and nowhere else:

- a scope **over** the reader's 100-line index page, so the paginated header, the
  `N more entries` notice, the last page and a page past the end are all served. ⚠ This is a
  widening over `server/verify-byte-identity.sh`, which *refuses* a paginated index —
  correctly, because it compares two **stores** and page membership is mtime-derived. Here
  both servers read one store, so the page is a fact about the store and is comparable;
- a scope that **is** a git repo and a scope that is **not**, so `X-Store-Revision` — the one
  value in a report response read off the filesystem rather than through the narrowed index —
  is compared in both of its shapes;
- entries sharing a whole second with differing fractions, **and** a pair tied to the
  nanosecond, so the index order's tie-break and the featured pick's are both live;
- a **non-ASCII** member name and a member name **over 100 bytes** — the only two shapes that
  make CPython's PAX writer emit a `path` extended record at all, which is where four header
  divergences hid;
- an **ambiguous bare ref** (`plum.md` beside `plum.process.md`), which is the per-entry
  arm's own refusal path and the only thing that makes the resolver mutant reachable;
- malformed entries **beside** readable ones, a scope holding files and nothing indexable, an
  **empty** scope directory, a one-entry scope (whose index has exactly one possible order,
  so it is the control that separates "the orders agree" from "there was one order"),
  openness markers, `tasks:`, an honoured `sensitivity:`, a duplicate heading, a fenced
  region, a present-but-empty section, a bare entry, and non-ASCII body content.

### What mode 2 **cannot** stand in for

The operator's real store is the only thing with the real store's **shape**: its scope count,
its entry-size distribution, its mtime spread, its actual `.git` contents, and the one-off
malformations nobody would think to write down. A green in mode 2 is a claim about a world
this repository imagined — which is the same limitation the corpus has, one size up. Mode 1
is the only mode that answers the question about real data, and it is the operator's to run.
Neither mode substitutes for the other, and CI can only ever run the synthetic one.

## Declared differences — each a licence, each required to fire

There is deliberately no "ignore headers" switch, and **a declared licence that matched
nothing is a failure**: either it is dead, or the difference it named has been *closed* and
should be deleted, or a pattern stopped matching and a real difference is now hidden behind
it. All five fired on both modes.

| # | licence | where | why |
|---|---|---|---|
| 1 | `date-header` | every response | `Date` is required by HTTP/1.1 and is the moment of the response |
| 2 | `snapshot-gzip-envelope` | `/api/v1/snapshot`'s gzip framing and `Content-Length` | measured unattainable — see the scoping section above. The tar inside is compared instead |
| 3 | `json-decoder-diagnostic` | the append route's `body must be JSON (…)` tail | CPython's `json` diagnostic on one side, `encoding/json`'s on the other. The corpus rules the same way and marks its row `oracle_only`; here the row is still compared up to the open parenthesis, so the status, the headers and the message a caller greps are all pinned |
| 4 | `audit-timestamp-instant` | the audit stream's `ts=` **value** | two servers stamp two instants. Every **digit** is replaced by `0` rather than the field being dropped, so the **spelling** is still compared — which is the point; see the finding below |
| 5 | `listen-port` | the startup banner's `listening on <host>:<port>` | two servers cannot share one port. Everything else on that line is compared literally |

🔴 **`Content-Length` is compared literally on every other route, which is STRICTER than the
conformance corpus.** That corpus drops it on reports too, because its goldens must survive
being replayed on another host, where the body names a different machine and a different
store path and is therefore a different length. Both servers here read one store on one host,
so a report's length is a fact they must agree about — including on every `HEAD`, which makes
`head-matches-get` a byte claim rather than a relation.

## What this gate found

**One divergence in the code under test, and it was invisible to every existing gate — plus
three defects in the gate itself, every one found by asking the instrument a question about
itself rather than by any run going red.**

🔴 **The audit record's timestamp.** The oracle writes
`ts=2026-01-02T03:04:05+00:00` (`datetime.now(timezone.utc).isoformat(timespec="seconds")`);
the Go server wrote `ts=2026-01-02T03:04:05Z` (`time.RFC3339`, whose `Z07:00` collapses UTC
to a bare `Z`). **Every byte on the wire agreed**, so `go test`, the conformance corpus and
the parity gate were all green — the corpus names the audit log under what it cannot reach,
and it is right: a suite that speaks HTTP cannot read the stdout of a server it did not
start. This harness starts both.

Both spellings are valid RFC 3339 and no correct parser can tell them apart. That is not the
standard the stream is held to: the audit log is **grepped**, and the documented
token-rotation procedure is "read the fingerprints printed at startup, then grep this stream
for the one that should have stopped appearing" — so a cutover that silently re-spelled the
timestamp would change the shape of every line an operator's saved query matches.

**The Go side is the one that moved**, because the oracle is the contract and the deployed
pod emits `+00:00`. The layout is now a named constant carrying the reason, and
`api.TestTheAuditRecordIsTheORACLESSPELLINGFieldForField` pins the **whole normalised
record** against a fixed clock — not a set of `Contains` checks, because a guard on a few
fields is walkable by re-spelling the others. **Red at `38b358d`**: one character short,
`ts=2000-01-05T00:00:00Z` against `ts=2000-01-05T00:00:00+00:00`. Green at HEAD.

### …and three defects in the harness itself

🔴 **`tests/parity/harness.py` and `tests/dualrun/harness.py` collide on the module name
`harness`, and that broke the parity ledger.** Both test files put their own directory on
`sys.path` and `import harness`; `sys.modules` holds exactly one module by that name, so
whichever pytest collected first won and the other silently got the wrong module. Measured:
the full suite went **2 failed, 6 errors** in `tests/test_parity_harness.py`, with
`AttributeError`s about attributes the dual-run harness does not have.

⚠ **The failure mode that matters is the one that did not happen.** Under `-p no:randomly`
the dual-run file is collected first, so the *parity* ledger was the one that broke — loudly.
Had the order been the other way round, this gate's ledger would have measured the parity
harness, and some of its guards would have **passed anyway**, because a missing attribute is
an error while a present one is not proof it came from the right module. A module-name
collision is not a naming nit; it is a silent substitution of the thing under test.

`tests/test_dualrun_harness.py` now loads all three modules from explicit file paths under
names that cannot collide, leaves `sys.modules["harness"]` for the parity ledger to claim,
and carries `test_this_file_imported_the_DUALRUN_harness_and_not_the_PARITY_one` — which
asserts the file on disk for all three modules **and** that `import harness` still resolves
to the parity one. Red under a bare import in **both** collection orders; green in both after.

🔴 **THE WRITE PHASE COMPARED ONLY REFUSALS, AND EVERY TARGET PASSED.** The PUT targets first
sent an empty body, so `create-new` answered **422 entry-shape** on both servers: no 201, no
200 `replaced`, no ETag over content the gate had put there. Two servers agreeing on a 422 is
a real comparison of the refusal and says nothing about the write. And
`append-refused-scope` pointed at a scope the **wide** principal can see, so it answered
`200 appended` — a row named for a refusal that compared a successful write. Both were found
by printing every write target's status side by side and reading the column, not by any run
going red. Closed by: conformant bodies, a `derive_if_match` target that reads the current
revision off a stale `If-Match`'s 412, the refused/absent pair sent by the **narrowed**
principal, a `writes-landed` runtime floor naming all three success statuses, and two
structural guards in the ledger.

### …and the target-id collision

The per-entry sweep's target ids did not carry the principal, so the narrowed principal's
three entry targets shared an id with three of the wide one's. The headline difference count
is `len(set(failures))`, so a mutant that made **all 316** targets differ was reported as
**313** — an id collision under-reports the number the whole gate is read through, and makes
`--only` ambiguous about which principal it selected. Found by asking why a mutant's count
was three short of the target count. The ids now carry the principal and `run_once`
**refuses** a duplicate id rather than warning about one.

## Validating the instrument

🔴 **Two servers failing identically compare equal, and this gate's entire output is an
equality.** That is not hypothetical here: the P2 parity harness's first full run reported
**72 PASS / 0 FAIL** while a trusted-proxies value made the pod refuse every request and both
clients rendered `store-unreachable`. Four controls stand against that, they are four
different claims, and each refuses with exit **2** — "could not vouch", never "failed".

### 1. The pre-flight — did both servers answer something substantive?

Measured on **each server separately**, because an equality cannot see a shared failure:

- a **rendered digest** — a 200 whose body carries the report's own `subsystem-recall:
  status=` line, so the renderer ran rather than a refusal being formatted;
- a **non-empty snapshot** — a 200 whose `X-Store-Entries` is at least 1 **and** whose gzip
  body extracts to a tar with at least one member, because a header can count what an
  archive does not carry;
- **all three WRITE successes** — `appended`, `replaced`, `created` — per server, checked over
  the run. Named rather than counted, so a success whose meaning changed cannot satisfy the
  floor, and three rather than one because `appended` alone leaves **both** halves of
  `PUT entry` compared at their refusals only;
- a **search HIT**, per server, checked over the run rather than up front. `HIT_TERM` is a
  word the *generated* world spells and a real store probably does not, so without this a
  mode-1 run could compare the sentence a **miss** produces on every search target — a
  correct comparison, and nothing about the hit path, the hunk ranking, the rung, the context
  window or the truncation notice. `search-by-ref` is what makes a hit reachable on any
  store; this is what proves one happened.

Watched working: `--break-both` puts the loopback back into `SUBSYSTEM_STORE_TRUSTED_PROXIES`
— the exact P2 incident — and both servers then refuse every direct request
`401 status=no-client-ip`. Every target would compare equal. Measured output:

```
PREFLIGHT oracle digest-status=401 rendered-digest=False digest-bytes=13 snapshot-status=401 declared-entries=-1 tar-members=-1
PREFLIGHT go     digest-status=401 rendered-digest=False digest-bytes=13 snapshot-status=401 declared-entries=-1 tar-members=-1
REFUSING TO VOUCH: … every target below would compare two FAILURES to each other …
rc=2
```

Two further refusals of the same kind: a store copy whose **mtimes** did not survive (mode 1
verifies every file to the nanosecond, because a rounded copy would make the gate measure a
world that does not exist while reporting it as the operator's), and a run that **straddles
UTC midnight** — the append route stamps the date into the bullet and the ETag hashes the
stamped content, so the two servers would have been asked about two different days. That is a
reason to refuse, not a licence to normalise the date away: normalising it would stop
comparing a field that is part of the contract on every other run.

### 2. The negative controls — seven mutants, `--self-test`

`mutants.py` boots the **oracle** from a mutated copy. The mutations are realistic edits, not
textbook ones, and the sweep makes **three** assertions per mutant rather than "something
went red":

1. the comparison the mutant was **written for** is among the failing ones — the arm is wired
   to something;
2. the failing comparisons **equal** the set the mutation declares — nothing else moved, so
   the kill is attributable;
3. where `only_target_arm` is named, the failing target arms equal exactly that — which is
   the claim that makes an arm **load-bearing** rather than merely live.

| mutant | edit | arm | caught | evidence |
|---|---|---|---|---|
| `status-code-moved` | a write verb on a read-only route answers 403, not 405 | `status` | ✅ | 2 targets, only the `status` comparison |
| `response-header-dropped` | `Cache-Control: no-store` gone from every response | `headers` | ✅ | **361 of 361** targets, only the `headers` comparison |
| `tar-mtime-truncated` | the snapshot's member mtimes lose their fraction | `tar` | ✅ | 13 targets, only the `tar` comparison |
| `tar-members-reordered` | the same members in the opposite order | `tar` | ✅ | 10 targets, only the `tar` comparison |
| `audit-identity-field-dropped` | the audit line loses `identity=` | `audit` | ✅ | nothing on the wire changes; only the `audit` arm |
| `startup-banner-field-dropped` | the banner loses `trusted-proxies=` | `process` | ✅ | only the `process` arm |
| `resolver-tier-keyed-on-ref` | the filename tier matches `e.ref`, not `e.slug` | `body`, on the `entry` arm **alone** | ✅ | `entry:wide-writer:theta-ambiguous/plum` and nothing else |
| *(control)* `unmutated_server` | the same copy mechanics, no edit | — | PASSES | 361 targets, 1,489 comparisons |

**Nothing survived.** Three of the seven exist because the first four could not reach their
arms: the audit stream, the process stream, and the per-entry sweep. `startup-banner-field-
dropped` was added *because* `test_every_ARM_has_a_negative_control` asked the question — the
`process` arm was live and nobody had watched it go red.

🔴 **`tar-mtime-truncated` and `tar-members-reordered` are the two that would each pass an
extracted-tree comparison.** The reorder changes no member name, mode, mtime or content; the
truncation changes no name and no byte of content. Both are caught only because the verdict is
the **archive's own bytes**.

⚠ **Two things the sweep corrected about itself, recorded because a later round should not
re-derive them:**

- **The first attribution scheme read the failing TARGET's name and then added `status` and
  `headers` to every attribution unconditionally** — so the two mutants written for those
  arms were "caught by the right arm" whatever had actually gone red. That is
  reads-as-coverage-while-providing-none, inside the control that exists to stop it.
  Attribution is now by which *comparison* failed.
- **`resolver-tier-keyed-on-ref` was declared as `{"body"}` and that was wrong.** Re-resolving
  a ref changes the **answer**, so `X-Store-Status`, `X-Store-Exit` and `Content-Length` move
  with the body and the audit record's `status=` field moves with them: three comparisons, one
  cause. Declaring `{"body"}` would have been a guard demanding something untrue. The
  isolating claim was never the comparison set — it is `only_target_arm`.

⚠ **`--self-test` refuses `--store`.** The mutants need the shapes they were written for — an
ambiguous bare ref, entries inside one whole second, a non-ASCII member name — and a real
store is not guaranteed to hold any of them. A mutant that is unreachable in the world it is
run against is scored SURVIVED for a reason that has nothing to do with the gate.
`resolver-tier-keyed-on-ref` is the sharp case: for every entry whose slug and ref are the
same string the edit is a **no-op**, so without `theta-ambiguous` it would survive a perfect
gate.

### 2b. …and the gate is SYMMETRIC, measured rather than reasoned

🔴 **Every one of the seven mutants above edits the ORACLE.** That the comparison is
symmetric follows from its shape — but "follows from its shape" is reasoning, and the whole
point of a negative control is not to accept that. So two mutants were applied to the **Go**
server, rebuilt, and the gate required to go red:

| Go-side mutant | edit | result |
|---|---|---|
| `G1-go-report-body-loses-its-trailing-newline` | `serveReport` drops the body's final `\n` | **red**: 305 of 361 targets, failing comparisons `body, headers` (`Content-Length` moves with the body) |
| `G2-go-snapshot-mtime-truncated` | the **mirror** of the oracle's tar mutant — `MTime` truncated to whole seconds in `internal/snapshot` | **red**: 10 targets, failing comparison `tar` alone |

G2 is the one worth having: it proves the `tar` arm is sensitive in **both** directions, so a
future regression in the Go writer — the side that is going to be deployed — cannot pass a
gate that only ever watched the oracle move. Control after restoring and rebuilding:
`SUMMARY targets=361 comparisons=1489 differences=0`.

### 2c. A second battery, over this gate's OWN guards — 26 mutants in six rounds

`tests/test_dualrun_harness.py` is a ledger over the gate's declarations, and a ledger is a
claim too. Six rounds, under `PYTHONDONTWRITEBYTECODE=1` throughout — a same-length edit
landing in the same whole second as the last import is invisible to CPython's mtime+size
bytecode cache, and the mutant would be scored SURVIVED without ever executing.

**Round 1 — 14 mutants, 14 killed, each with its own guard's message.** A route target
deleted, a query parameter dropped, the `process` arm's mutant removed, a mutation pattern
made not to match, the isolated-arm claim dropped, the page-cap copy pushed under the real
cap, a real year in the generated world, every scope made a git repo, a licence's reason
gutted, `0xff` removed from the gzip licence, a narrower row's reason gutted, an arm left
with no target, a mutation's `expect` set made to exclude its own arm, and the target ids
made to collide.

🔴 **Round 1 killing everything is a warning, not a result** — the house rule is that a
first-pass sweep which kills everything usually means the mutants were too easy. **Round 2
aimed at where each guard might be narrower than its sentence, and all five survived.**
Three were real gaps and are now closed; two are labelled limits.

| round-2 mutant | survived round 1's guards because | now |
|---|---|---|
| `threshold=0.3` → `0.6` (**the default**) | the guard's sentence was only "the name appears in some query string" | **closed at the unit level**: every discovered parameter must appear at **two or more distinct values**. Four search targets were added to satisfy it, which is a real coverage widening the sweep bought |
| `threshold=0.3` → `threshold=` (empty) | same | **closed**: an empty value is refused |
| `recall-digest` addressed at `/api/v1/recall` (**wrong arity**) | `GET /api/v1/recall` addresses `GET recall` exactly as `GET /api/v1/recall/<scope>` does. The route ledger counts what is ADDRESSED; only one of the two reaches a handler | **closed at RUNTIME**: a target both servers answer `X-Store-Status: no-route` to, and that is not the declared no-route probe, now REFUSES TO VOUCH (rc 2). ⚠ Keyed on the header and not the bare 404 — the first draft used the status code and flagged `append-unknown-ref` and `append-narrow-forbidden`, which answer a **deliberate** 404 from a dispatched handler because refused must be indistinguishable from absent. An empty result cannot distinguish two mechanisms; the header is the upstream signal they disagree about |
| a mutation's `new` made a semantic **no-op** while its `kind` still claims the `tar` arm | no unit guard can evaluate a Python string's runtime effect | **LABELLED, not closed**: `--self-test` is what catches it, and it does — the mutant is scored `SURVIVED` and the run exits 2. Watched to work. This is the honest two-tier split: the unit file pins the SHAPE of the declaration, the self-test pins that the declaration is TRUE |
| a real-looking project name in the generated world (`acme-payments-prod`) | the guard checks **dates**, not names, and only inside `*.md` | **LABELLED, not closed**, and this matches the repository's existing ruling rather than being an oversight. `tests/leakscan.py`'s own docstring records why it does not police project names: an earlier version that chased them produced **480 findings of which 4 mattered**, and a gate firing 476 times for nothing is a gate someone turns off. `AGENTS.md` requires synthetic fixtures; enforcement there is review, deliberately |

**Round 3 — 2 mutants, both killed at runtime**, which is what closed the two gaps above:
the wrong-arity target by the stray-no-route floor (`rc=2`, naming nine targets), and the
no-op mutation by `--self-test` (`SELF-TEST SURVIVED tar-members-reordered`, `rc=2`).

**Round 4 — 1 mutant, the module-name collision.** Reverting the ledger's explicit
file-path imports to a bare `import harness` is red in **both** collection orders, which is
the shape that matters: the collision is silent in exactly one of them.

**Round 5 — 3 mutants over the write phase, each killed at BOTH tiers.** PUT bodies back to
`b""` → the structural guard fails on "sends no body at all" *and* the runtime
`writes-landed` floor exits 2; `derive_if_match` dropped → same pair; the refused-scope row
sent as the **wide** principal → the pair guard fails. Two tiers rather than one because a
structural check type-checks past a wrong argument and a runtime floor cannot say *which*
declaration was wrong.

**Round 6 — 1 mutant, over `mutants.py`'s own copy mechanics.** `shutil.rmtree` refuses a
symbolic link and under `ignore_errors=True` refuses **silently**, so reusing one `dest` for a
`server` mutation (which leaves `lib` a symlink) and a `lib/` mutation (which needs a real
copy) breaks. ⚠ **Measured, and the hazard is a CRASH rather than a false SURVIVED** — the
first comment claimed the worse one: `copytree` raises `FileExistsError` on the surviving
link, which is loud and could not produce a wrong verdict. Unreachable today (every mutation
gets its own `dest`); fixed anyway, and the fix watched to apply the edit to the **copy** with
the real `lib/` verifiably untouched.

⚠ **One correction the sweep forced on its own attribution scheme**, recorded so a later
round does not re-derive it: the first version read the failing TARGET's name and then added
`status` and `headers` to every attribution unconditionally, so the two mutants written for
those arms were "caught by the right arm" whatever had gone red. Attribution is now by which
*comparison* failed, and there are three assertions rather than one.

### 3. The positive control — the count moves with the store

`comparisons=1304` proves nothing on its own; a harness enumerating a hardcoded list would
print the same number over an empty store. `--positive-control` runs the gate over two stores
of different size and requires both counts to grow:

```
POSITIVE-CONTROL scale=1 targets=361 comparisons=1489
POSITIVE-CONTROL scale=2 targets=462 comparisons=1894
POSITIVE-CONTROL OK
```

It is a mode-2 control and it **refuses** `--store`: mode 1 is pointed at one real store and
has no second size to compare against.

### 4. The zero refusals, and the ones that are about the MATRIX rather than the servers

Each of these refuses (rc 2) or fails rather than passing through:

- a store with **no scope directory** — nothing would be compared, and that zero is the
  failure rather than the all-clear, which is the rule `verify-byte-identity.sh` states;
- a `--only` that selected nothing;
- a store in which **no scope indexes a single entry**, so no write target could be addressed
  and the two write routes would not be compared at all. A `FAIL`, not a silent narrowing;
- **two targets sharing an id** — the headline count is `len(set(failures))`, so a collision
  under-reports it. Measured, and found the hard way; see "what this gate found";
- **a target both servers answer `no-route` to** that is not the declared no-route probe —
  the route it names looks covered while only a refusal was compared;
- **the no-route probe itself not producing a 404** — without that, the floor above cannot
  tell a stray 404 from a run in which no 404 is reachable at all. A floor whose positive
  control is not checked is the reassuring zero one level up.

## What this gate structurally CANNOT see

Named rather than omitted, because a green is a claim about what was asked.

- **The gzip envelope.** Deliberate and measured; see the scoping section. Also `Content-Length`
  on `/api/v1/snapshot` alone.
- **Concurrency.** Every target is one request at a time, in a declared order, and the two
  servers are asked in sequence. The entry lock, the audit lock, two writers appending to one
  entry, and the listen backlog are all invisible here. `tests/test_subsystem_store_api.py` is
  where those are tested, for the oracle.
- **The rate limiter and the lockout.** Configured out of the way (`MAX_FAILURES` raised),
  exactly as the corpus does, so the rest of the sweep is reachable at all. What would happen
  when a lockout trips on each implementation is not compared.
- **TLS, the gateway, and anything a proxy does.** Plain HTTP to a loopback socket, and the
  trusted-proxy set deliberately names an address neither server can see the harness at.
- **Anything requiring the real pod's environment.** Kubernetes, the secret mount, `SIGHUP`
  token reload, startup refusals and every exit-code path are process and deployment
  behaviour rather than request/response behaviour. `--break-both` is the one environmental
  variable this harness moves, and it moves it as a control.
- **Connection REUSE.** Every target opens its own connection and closes it, because a
  non-200 closes on both servers and a keep-alive sweep would interleave two connection
  lifecycles into the comparison. The body-draining rule's *effect* and request smuggling
  generally need a second request on one socket.
- **Chunked transfer encoding and every other framing both servers refuse.** The runner always
  sends `Content-Length`. `internal/api`'s `readBody` comment is where that decision lives.
- **Header ORDER.** Recorded sorted; RFC 9110 gives it no meaning and pinning it would fail a
  correct implementation. The name *spellings* are compared literally.
- **A request line an HTTP client cannot express.** The three `raw_request` rows are the
  corpus's; this harness speaks `http.client` only.
- **An unreadable file, a mode-000 scope, or a dangling symlink.** Mode 1 copies as the
  invoking user, so such a file is a copy failure rather than a served answer, and mode 2
  generates none. The `store-unreachable` arm of both servers is therefore compared only on
  the paths a readable store can reach.
- **The oracle's own correctness.** This is a differential gate. Two implementations that are
  wrong in the same way compare equal, which is what the conformance corpus — a comparison
  against *recorded* behaviour — is for. Read both.
- **A real store other than the one it is pointed at.** Mode 1 is one operator, one machine,
  one moment.

## Files

| path | what |
|---|---|
| `harness.py` | the gate: the target matrix, the six arms, the pre-flight, the controls |
| `genstore.py` | mode 2's seeded, deterministic store generator |
| `mutants.py` | the six negative-control mutations, and the unmutated positive control |
| `../test_dualrun_harness.py` | the pytest ledger over this directory — fifteen guards that the arms, the licences, the mutants, the route coverage and the parameter coverage cannot shrink unnoticed. It does NOT run the gate: that needs a Go toolchain and half a minute, and its verdict is read in its own CI job, exactly as the corpus's and the parity harness's are |
