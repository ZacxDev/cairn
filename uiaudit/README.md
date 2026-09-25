# `uiaudit` — the browser-surface audit harness

Boots `cairn-ui` hermetically, drives a real Chromium over every `GET` row the route ledger
declares, captures axe + screenshots + console/network + layout at two widths, pushes the
result to an upstream audit hub and reads the deterministic diff back in the same CI job.

```bash
uiaudit/run.sh                    # the whole walk; needs chromium on PATH
cd uiaudit && go test ./...       # the guards, incl. the positive control (REFUSES without chromium)
cd uiaudit && go run ./spike -base http://127.0.0.1:18771 -token <tok> -axe vendor-js/axe.min.js
```

## Why it exists — four things, and nothing else

cairn has six CI jobs and eleven nix checks: a collected-test floor, an `ok` floor over 19 Go
packages under `-race`, 150 control mutants, 57 routing mutants, a conformance corpus, a CLI
parity harness, a dual-run. **None of them has ever rendered a page in a browser.** This
harness deliberately asserts nothing they cover — no renderer assertions, no authz logic, no
HTML string scanning. What it measures:

1. **The cookie flags as a BROWSER honours them.** `internal/identity/session.go` chooses
   `__Host-`, `Secure`, `HttpOnly`, `SameSite=Lax`, and its own comment flags the
   `Secure`-over-plaintext-loopback half as *"a claim about browsers and no test here has
   measured it"*. A header assertion cannot close that. A jar read after a real navigation
   can — see the spikes below.
2. **axe-core over the real rendered DOM.** There was no accessibility check of any kind in
   cairn before this. There cannot be one without a browser: axe's violations are functions
   of computed style and layout, not of the HTML string.
3. **Layout at 390px and 1440px.** This surface had never been rendered at any width. ⚠ The two
   values are **not chosen here and not arbitrary**: they are the two the upstream hub's own native
   crawl uses, so a pushed run is diffed against pages captured at the same widths. `Viewport`'s
   doc in `targets.go` says so at the definition. The COUNT satisfies this repository's
   two-points rule; the VALUES come from the consumer.
4. ⚠ **RETRACTED — and kept rather than deleted.** This claimed the harness is the only thing
   that can distinguish a deployment whose session volume vanished after start (readiness passes,
   every login fails). **It is false of this harness**, and the blind set below already said so:
   the walk only ever boots a *fresh hermetic pod over a temp directory it created*, so the broken
   world is one it cannot construct. Both statements were right; together they retract the claim.
   What is true is that a **smoke probe signing in against a real deployment** would distinguish
   them — `/healthz` answers before the authentication chain runs and a sign-in does not — and
   **nothing here runs one**. The gap is real and unclosed; naming this program as its closer was
   the error.

## The two spikes — both PASS, with the conditions they were measured under

Both claims were inferred in every codebase involved and measured in none. `spike/main.go`
re-runs them.

**Chromium 153.0.8010.52, `headless=new`, `--no-sandbox`. `cairn-ui` bound to loopback,
`-control-journal` unset, token-file authority, the `tests/reader_fixtures.py` store.**

### Spike 1 — a `__Host-` `Secure` cookie over plaintext loopback: **PASS**

Measured at **two** origins, because the claim in `session.go` names `localhost` while the
harness binds `127.0.0.1` and Chromium's potentially-trustworthy-origin rules are stated per
host:

| origin | jar after sign-in | second navigation |
|---|---|---|
| `http://127.0.0.1:18771` | `__Host-cairn-session` `secure=true httpOnly=true sameSite=Lax path=/ domain=127.0.0.1` | `/` renders authenticated content |
| `http://localhost:18771` | same, `domain=localhost` | same |

The discriminator is deliberately the **second request**, not the `Set-Cookie` header: a
browser that parsed the header and then dropped the cookie for failing the `Secure`
requirement looks identical at the header and differs only in what it sends back. So the
measured claim is **stored AND re-sent**, which is what the harness needs.

### Spike 2 — axe-core injected over CDP against `default-src 'none'`: **PASS**

The page's CSP, read off the wire rather than assumed:

```
Content-Security-Policy: default-src 'none'; style-src 'unsafe-inline'; base-uri 'none'; form-action 'self'
```

No `script-src` at all, so `default-src 'none'` governs scripts — the strictest case. A
`<script>` tag is blocked. `Runtime.evaluate` is debugger-privileged and bypassed it:
`window.axe` became an object, and `axe.run()` returned `{"violations":1,
"ids":["color-contrast"],"testEngine":"4.12.1"}` on `/`.

**What that result reaches, stated so it cannot go stale:** the measurement was taken at the
strictest point on the axis that matters — a policy granting scripts *no* source whatsoever — so
it carries to **any** policy that does not grant scripts a source, whatever else that policy
says. Re-run the spike only if `internal/ui`'s `ContentSecurityPolicy` ever starts permitting
scripts; a policy that tightens elsewhere, or that drops a clause, cannot invalidate it. That is
a claim about where this measurement sits on a dimension, not a cross-reference to anybody's
current value — which is the distinction the stale line further down was the counter-example to.

⚠ A side observation worth recording: `fetch(location.href)` from inside the page FAILED, and
that is the CSP working (`connect-src` falls back to `default-src 'none'`). The spike reads the
CSP with `curl` for that reason. A reader who saw only the page-side read would conclude the
header was absent.

## How the walk derives from the route ledger

`targets.go` imports `ui.DeclaredRouteLedger()` **at compile time**. A nested module under
`github.com/ZacxDev/cairn/` may import `internal/…` — Go's internal rule is about the import
path's prefix, not the module boundary — so a row added to `internal/ui/routes.go` reaches
this program with nobody editing it, and a row removed stops appearing.

Per row:

- **Not `GET`** → skipped and **listed**, never dropped. A state-changing row is reached by
  CLICKING a form on a captured page (that is how the sign-in POST is exercised); navigating
  to it directly would be a request with no `Origin`, which `sameOrigin` refuses.
- **`public` class** → captured signed-OUT. Derived from the CLASS, never from the path: a
  guard looking for `/sign-` would be a guard on a word a new row can be spelled around.
- **Anything else** → captured signed-IN.
- **In `notADocument`** → skipped, **with the reason it is not a document**, and counted.
- **A `GET` row in none of the three sets** → the walk **REFUSES**, naming the row and all
  three remedies. That is what stops a new route being absorbed silently.

`LedgerAccounting` requires `captured rows + skipped rows == len(ledger)` and is printed every
run: `ledger has 7 row(s); 3 target(s) derived, 4 row(s) skipped`.

### The third class, and the merged-tree break it closes

✅ **The accounting caught a break on a tree neither PR's CI could see.** The auth change adds
three `GET` rows. Both changes are green on their own branches and **touch zero files in
common**, so `git merge-tree` exits 0 and every per-branch gate stays green — and the merged
tree is red, because this walk refuses a `GET` row nobody classified. That is the disjoint-file
merge break: one side widened the route ledger, the other added a consumer of it.

**Neither new row may be captured, and that is the point of a third class rather than two more
`plainGET` entries:**

| row | why it is `notADocument` |
|---|---|
| `GET /sign-in/github/callback` | reachable only with a provider `?code=` **and** a live single-use flight cookie. Navigated bare it renders a refusal — and capturing a refusal is byte-for-byte the false green above |
| `GET /static/app.css` | a `text/css` response, not a document. axe, the layout smells and the digest are all meaningless on a stylesheet, and its screenshot is noise in an already-advisory pixel diff |

`notADocument` maps path → **reason**, not path → `bool`: a skip with no reason cannot be told
from a row somebody gave up on. The walk prints both kinds of skip in distinguishable sentences
(`not GET:` vs `not a document:`), because "a browser must not navigate this" and "a browser
cannot usefully render this" are different facts. Measured output on the merged ledger —
**10 rows → 3 targets + 7 skips**:

```
skip GET /sign-in/github/callback public (not a document: reachable only with a provider ?code= AND
  a live single-use flight cookie, so navigated bare it renders a refusal — capturing that would
  measure an error page and count it as a page)
skip GET /static/app.css public (not a document: a text/css response and not a document — axe, the
  layout smells and the a11y digest are all meaningless on a stylesheet, and its screenshot is
  noise in the pixel diff; checked over plain HTTP instead)
skip POST /share (not GET: reached by submitting a form, never navigated)
skip POST /sign-in public (not GET: reached by submitting a form, never navigated)
skip POST /sign-in/github public (not GET: reached by submitting a form, never navigated)
skip POST /sign-out (not GET: reached by submitting a form, never navigated)
skip POST /unshare (not GET: reached by submitting a form, never navigated)
```

**The accounting guard was mutation-tested, because adding a class means editing the thing that
catches an unclassified row.** Harness validated first (a `-run` filter selecting nothing scores
every mutant SURVIVED — measured earlier in this module). **5 mutants, 5 KILLED, each by the test
that NAMES its property:**

| mutant | killed by |
|---|---|
| the unknown-row refusal becomes a silent skip (`notADocument` as a default) | `TestTheUNKNOWNRowREFUSALSURVIVESTheThirdClass` |
| the not-a-document skip loses its REASON | `TestTheMERGEDLedgerIsFullyACCOUNTEDFor` |
| the not-a-document row is CAPTURED instead of skipped | both of the above |
| the two-class conflict check removed | `TestAPathClaimedByTwoClassesIsREFUSED` |
| `LedgerAccounting` stops comparing | `TestLedgerAccountingCatchesAnUnaccountedRow` |

### One non-browser assertion, on the stylesheet row

`StylesheetCheck` asserts the route answers **200**, media type **`text/css`**, **non-empty
body** — over plain HTTP, no browser. It is the one thing about that row a walk can usefully
check, and it matters because the route is a **blocking subresource of every page**: a 404 there
makes every page render unstyled and nothing else in this harness looks at that.

**Gated on the ledger**, so it is a no-op until the row exists. Six cases, and the gate's own
control is the first: the server in that case would fail *every* assertion, so a check that ran
anyway could not pass.

| case | result |
|---|---|
| no stylesheet row in the ledger, server deliberately broken | **SKIPPED** — proves the gate held |
| `200 text/css` + body | pass |
| `200 text/css; charset=utf-8` + body | **pass** — a whole-string content-type comparison would refuse this correct response |
| `404` | red: *answered 404, not 200* |
| `200 text/html` | red: *not text/css* |
| `200 text/css`, empty body | red: *EMPTY body* |

⚠ **The two `notADocument` keys and `StylesheetPath` are string literals**, not
`ui.OAuthCallbackPath` / `ui.StylesheetPath`, for one reason: those constants do not exist on
this branch's base. A literal is the second spelling of a route that `routes.go` warns about.
**Closing condition:** once the auth change merges, replace the literals with the constants and
assert the ledger contains them — a compile-time claim the moment the constants exist, and not
expressible before. `mergedLedger` in `targets_test.go` should be **deleted** at the same time,
not updated: it is a transcription, and the real ledger supersedes it.

### Measured LIVE on the merged tree, not only through the fixture

An integration branch off current `main` with the auth change and this one merged (**zero files
in common; `git merge-tree` exits 0**) was built and run in full:

| | result |
|---|---|
| root module | `go vet` clean, **19 `ok`**, 0 FAIL — the floor, unmoved |
| `uiaudit` module | **21 top-level / 23 subtests / 44 total, 0 FAIL** |
| the walk itself | exit **0**; `ledger has 10 row(s); 3 target(s) derived, 7 row(s) skipped` |
| `/static/app.css` | `answers 200 text/css with a non-empty body` |
| sign-in | `__Host-cairn-session secure=true httpOnly=true sameSite=Lax path=/` |
| `/sign-in` tap targets | **2 → 3** — the new OAuth button, caught with no change to the walk |

🔴 **And the walk found a defect in ITSELF on that tree, which is why this section is not just a
green tick.** See *"The walk could not sign in"* above: the merged sign-in page grew a second
form, the OAuth button precedes the token form in document order, and `chromedp.ByQuery` takes
the first match — so the walk started a provider flight and its success check (the *absence* of
`id="token"`) read the resulting page as a pass. Both halves are fixed: the selector names the
form by `ui.SignInPath`, and the verdict is now the cookie rather than the absence of a word.

⚠ **What the walk has still not seen live, stated as a moving target rather than a clause:**
the stylesheet as a real blocking subresource **against the policy as it stands at merge time**.
The merged-tree walk above did fetch it successfully on every page — but against `internal/ui`'s
`ContentSecurityPolicy` *as it was on the auth branch's head that day*, and that constant has
already changed since (a clause was removed after a round-0 audit found it permitted something
the code forbids) and may change again before merge. **Read the constant, never a copy of its
value.**

Also unseen live: an undeclared path answering **404** rather than 401 — this walk never probes
one. `GET /` **303**ing to `/sign-in` for `Accept: text/html` is covered by the redirect guard
below rather than by a live capture, because a signed-in walk does not trigger it.

🔴 **THIS LINE WAS STALE TWICE OVER, AND BOTH WAYS ARE WORTH RECORDING.** It named a specific
clause of that policy, and the clause was deleted — a cross-reference to one *part* of a value
somebody else owns, which is precisely the thing that rots. And it claimed the stylesheet
subresource had not been seen live when the merged-tree run above had already seen it, so the
sentence was pessimistic about this harness at the same time as it was wrong about the policy.
The second stale copy in this PR after the structural-zero line, and the same lesson both times:
**a claim about someone else's value belongs as a pointer to the thing that holds it.** The
spike evidence at the top of this file is deliberately NOT rewritten to match — it quotes the
policy as measured, which is what makes it evidence rather than a claim.

✅ **That 303 WAS a hazard for the document-status gate, and it is now CLOSED.** A redirect
lands on a 2xx, so every other check here passes on it — and the bytes measured would be filed
under this target's push identity, which is what the hub matches its P2 diff on, so one page's
violations would be attributed to another forever with nothing reporting an error.
`CaptureTarget` now compares the LANDED path against the navigated one and refuses a mismatch.
Driven at **303** (the real case), **302** and **307**, each asserting the redirect guard's own
error string so it cannot pass because the status gate or axe failed instead — plus a
**same-path positive control**, because a guard that refused every navigation would satisfy all
three redirect cases and make the harness useless.

⚠ The comparison is on the PATH only: a server may legitimately normalise a query string, and a
guard that fired on the honest tree gets deleted. So a redirect that keeps the path and drops the
query is **not** caught — a lesser fault (same route, different arguments), named here rather
than left to be discovered.

### A row whose scope comes from a query parameter

**It is never guessed. The walk reads the values the surface PUBLISHES.**

`GET /share` takes its scope in `?scope=`, and the parameter is a **`control.ID`** — a
`crypto/rand` value, unguessable by construction and deliberately so, because a
404-for-unknown beside a 403-for-somebody-else's would make the page an existence oracle over
every scope in the deployment. So `GET /share` with no parameter renders the **index**, the
index renders one link per administrable scope, and `ExpandLinks` turns those hrefs into
further targets. A deployment with more administrable scopes is covered automatically.

`ExpandLinks` accepts only a **relative** href whose **path equals the page it came from** and
which carries a query. Everything else is DECLINED and logged — `internal/ui/render.go`'s
`safeHref` allowlists schemes, so an entry's `ref` can legitimately render an external link,
and a harness that followed one would push somebody else's site through the hub.

⚠ **On the token-file deployment the index publishes nothing, and that is correct.**
`tokenfile.Source` grants no `admin` verb, so the page renders *"No scope is administrable by
this credential"* — an authority answer, not an empty store. The walk captures the index and
says so. Reaching a per-scope share page needs a journal-backed world; that is declared gap 1
below.

## What the first draft got wrong — the defect this harness shipped and then caught

**This is the one regression guard in the module; everything else here is an invariant guard
and is labelled as one.**

The first draft expanded the share row into one target per scope **NAME** off the store
(`/share?scope=alpha-notes`). Every one of those targets **404'd**, because the parameter is a
`control.ID`. Nothing downstream noticed:

- axe ran happily on the error page and reported five violations — `document-title`,
  `html-has-lang`, `landmark-one-main`, `page-has-heading-one`, `region`
- the layout script returned real numbers, including `missing_viewport_meta=true`
- the screenshot was a valid PNG
- the run exited **0**, printing **"62 axe violations across 6 rule(s)"**

Not one of those is a fact about cairn. **Every collector worked; none could tell it was the
wrong document.** After the fix the same walk reports **2 violations across 1 rule**.

| | first draft | HEAD |
|---|---|---|
| pages captured | 16 (12 of them 404s) | 6, all `200` |
| axe violations | 62 across 6 rules | 2 across 1 rule (`color-contrast`) |
| `missing_viewport_meta` | 12 pages | 0 pages |
| exit code | 0 | 0 |

Two guards close it, at two levels:

- `TestAGuessedQueryParameterIsNotHowAPageIsReached` pins the **design** — no target may
  carry a synthesised query parameter. Red on the first draft.
- `TestAPageThatANSWEREDAnErrorIsREFUSEDRatherThanMeasured` pins the **symptom** — a document
  whose own response was not 2xx is a refusal, before anything is measured. Red on the first
  draft, and driven at 404, 401 **and 500** so it is not a 4xx-shaped guard. Its refusal
  message is asserted, so it cannot pass because axe failed to inject instead.
  `TestAHealthyDocumentIsNOTRefused` is the positive control on it: a gate that refused
  everything would satisfy all three cases above.

## Positive controls — the pair for every captured signal

`control_test.go` serves a page built to make every collector non-zero, and REFUSES rather
than skips when chromium is absent (a skipped instrument validation is a green that means
nothing). Measured, both halves in the same run:

| signal | positive control | `cairn-ui` walk (6 pages) | reading |
|---|---|---|---|
| axe violations | **5** | **2** (`color-contrast`, on `/` at both widths) | real |
| tap targets under 44px | **1** | **8** | real |
| text under 12px | **2** | **0** | real zero |
| images with no dimensions | **1** | **0** | **structural** — no `<img>` renders anywhere; an existing XSS guard asserts `"<img"` cannot |
| horizontal overflow | **true** | **false** on every page | real |
| missing `<meta viewport>` | **true** | **false** on every page | real — gomponents' `HTML5` supplies it, and nothing pinned that before |
| console events | **2** | **0** | **structural** — inline stylesheet, no script, nothing to observe |
| network events (page subresources) | **2** | **0** | **structural** — there are none |
| a11y digest entries | **1** | **10** on `/`, 3 on `/sign-in`, 4 on `/share` | real |
| screenshot | **31137 bytes**, PNG magic checked | 6 PNGs | real |

**The two structural zeros are reported as structural in the walk log itself**, not as passes.

⚠ **One claim in an earlier draft of that log line was FALSE and is corrected.** It printed
`console=N network=M — STRUCTURAL ZERO` over both numbers. The console half holds. The network
half did not: the same run had a non-zero network count, because **Chromium requests
`/favicon.ico` on its own initiative and no ledger row carries it**, so the dispatcher's
uniform refusal answers it — a first-party `401` in any developer's network panel, caused by
the surface having no favicon row.

That refusal is now counted **separately and at walk level**, for two measured reasons:

- its page attribution is arbitrary (it lands on whichever page was loading when the browser
  asked), and the hub matches its P2 diff on `url`+`viewport`, so attributed to a page it
  would manufacture a network delta that flaps forever;
- **whether Chromium asks at all is run-dependent** — measured non-zero on one walk and zero
  on a later walk over the same tree.

So the claim the count supports is *"the surface refuses it when asked"*, never *"every visit
produces one"*.

## Declared residuals

### 1. The a11y digest anchors almost nothing — with the exact diff, unapplied

the hub's `dropContradicted` gate indexes only **concrete anchors** (`#id`, `[name=…]`, via
`report.ConcreteKeys`). Measured on the digests this walk captured:

| page | digest selectors | concrete anchors |
|---|---|---|
| `/` | `button` | **0** |
| `/share` | `button` | **0** |
| `/sign-in` | `input#token` (interactive **and** form control) | **1** |

**One anchor on the whole surface.** The digest is captured, validated and pushed, and the
deterministic grounding gate can refute claims about exactly the sign-in token field. That is
the same measured failure elsewhere (4 claims in → 4 survived), and it is **not fixable from
this module**: `internal/ui/render.go` is owned by other work in flight and this change does
not touch it.

The fix is ids on the four buttons and the two link sites. The diff, for whoever is rewriting
that markup:

```diff
--- a/internal/ui/render.go
+++ b/internal/ui/render.go
@@ signOutForm
-		h.Button(h.Type("submit"), g.Text("Sign out")),
+		h.Button(h.ID("sign-out"), h.Type("submit"), g.Text("Sign out")),
@@ SignInPage
-					h.Button(h.Type("submit"), g.Text("Sign in")),
+					h.Button(h.ID("sign-in"), h.Type("submit"), g.Text("Sign in")),
@@ revocableItem
-			h.Button(h.Type("submit"), g.Text("Revoke")),
+			h.Button(h.ID("revoke-"+string(scopeID)), h.Type("submit"), g.Text("Revoke")),
@@ shareForm
-		h.Button(h.Type("submit"), g.Text("Share")),
+		h.Button(h.ID("share-submit"), h.Type("submit"), g.Text("Share")),
@@ scope index link
-			return h.Li(h.A(h.Href(href), g.Text(s.Name)))
+			return h.Li(h.A(h.ID("scope-"+string(s.ID)), h.Href(href), g.Text(s.Name)))
@@ taskItem
-	return h.Li(h.Class("task"), h.A(h.Href(href), g.Text(ref)))
+	return h.Li(h.Class("task"), h.A(h.ID("task-"+ref), h.Href(href), g.Text(ref)))
```

⚠ Two of those six interpolate a value into an `id` attribute. gomponents escapes an attribute
value, so that is not an injection — but `scopeID` and `ref` are not guaranteed to be valid
HTML id tokens, and a duplicate id would make `ConcreteKeys` see one anchor where there are
two. Whoever applies it owns that decision; this harness only measures the consequence.

### 2. A per-scope share page is never reached

Needs a journal-backed world (`-control-journal` pointing at a real journal with an `admin`
grant). The token-file deployment the walk boots grants no `admin` verb, so the share index
correctly publishes nothing. Closing condition: a fixture journal in `boot.go` that mints one
admin grant over one synthetic scope, after which `ExpandLinks` reaches the page with no change
to the derivation.

### 3. ✅ The wire leg is EXERCISED — three real pushes, and what the service actually accepted

**This section previously said the leg was unexercised. It is not, and the distinction the old
wording asked for can now be drawn.** Three pushes to a real plugin target, all `200`, all
`status: done`, walk exit 0 each time. Endpoint and credentials are secrets; neither appears here.

**What the service ACCEPTED — measured by reading the ingested run back, not inferred from the
schema:**

| claim | how it was measured |
|---|---|
| all six page rows stored | `pages` array = **6**, `{mobile: 3, desktop: 3}` |
| the 390px capture is not collapsed | `width=390` on three rows, `width=1440` on three — the service derives width from the viewport NAME |
| `environment: "lab"` accepted | echoed back as `environment: lab` |
| **the a11y detail took the STRUCTURED path** | stored as a JSON **object** with top-level `"id": "color-contrast"` — not the `{"detail": "…"}` wrapping, so `new_a11y_rules` keys on it |
| the a11y digest passed the real validator | `a11y_digest_key` set on **all six** rows — a malformed digest 400s the whole push, so the verbatim vendoring is confirmed against the live validator rather than against my reading of it |
| raw layout counts, findings derived server-side | I send **no** `layout` finding; the service stored one it computed: `{"smell": "small-tap-targets", "count": 1, "examples": ["button (65x21)"], "note": "…"}` |
| **the mobile gating works, discriminatingly** | that layout finding appears on the **mobile** rows and is **absent from desktop**, despite both carrying the same `small_tap_targets: 1`. Had this harness computed the finding itself it would have emitted it on desktop too and been wrong — the raw-counts-only design is validated by a discriminating observation, not by the absence of an error |
| no `perf` findings | none stored; the block is omitted and `lab` would suppress them anyway |

**And what the second push measured, which CORRECTS a prediction this PR made.** The diff came back
`pages +0/-0, 0 changed, 0 size-changed`, **`new_a11y_rules (0)`**, all deltas `+0`.

- The predicted "first diff flags every axe rule new" transient **did not happen**, and the reason
  is structural: that transient applies to a target whose BASELINE predates the structured-detail
  fix, so its ids appear for the first time in the diff. This target's very first push already
  carried structured ids, so there was never an id-less baseline. A brand-new target skips it.
- So the promotion candidate's precondition — *"after two baseline runs"* — is **satisfied and
  measured**, not pending. Two runs exist and the diff is clean. What remains before promoting
  `new_a11y_rules` to blocking is a **decision**, not a measurement.
- `0 changed / 0 size-changed` across two independent runs also means the pixel diff is stable for
  this hermetic world. ⚠ That does **not** argue for gating on it: the 684-regressions-over-565-pushes
  figure is about real producers against changing sites, and this is a fixed fixture with a pinned
  clock. Stability here is evidence about the fixture, not about the metric.

**What is still NOT exercised, stated so the upgrade does not read wider than it is:**

- the push from **CI** rather than from this host — the secrets exist, but no CI run has carried
  them yet;
- a push **large enough to approach any cap** (body 64 MiB, per-file 16 MiB, ≤200 pages). The real
  body is ~787 KB and 24 parts, so every cap is exercised only by the offline refusals below;
- every **refusal** path. The server accepted all three pushes, so none of the 400s the offline
  tests assert has been seen from the service.

**The offline shape verification therefore keeps its place, and its claim is now narrower and
truer.** It still refuses, before upload: refs ↔ parts in both directions; the per-file, body and
page caps; a strict `DisallowUnknownFields` round trip with **no `perf` key**; the closed viewport
and finding-type sets; an a11y detail without a top-level string `id` (the legacy `"<id> — <help>"`
string); a `layout`/`perf` finding authored here; an **empty** a11y digest; and `RunReport`'s struct
tags against a fixture of pairwise-distinct values, because a mis-spelled tag prints a reassuring
empty diff.

⚠ **Those are still a claim about this copy of the server's rules — for the REFUSALS.** The
acceptance path is now measured against the real service; the rejection path is not, and if the hub
tightens a rule these tests stay green while the push starts failing. That asymmetry is the honest
statement, and it is why the offline tests were not deleted once the wire leg worked.

### 3b. ✅ The CI push was blocked by an edge; the fix is load-bearing and the header proved it

**`verify-push` caught this, and it is the first thing it has ever caught.** The walk step exited
**0** while nothing landed. Without that control the job would have been green over a non-event —
which is the entire argument for it, now paid for.

**Diagnosis, measured in order:**

| step | finding |
|---|---|
| step conclusions (API, not `gh run view`) | everything `success` except `verify-push` |
| the walk's log, from the **uploaded artifact** | `push failed (non-fatal): push rejected: 403 Forbidden: <!DOCTYPE html…` |
| that body | Cloudflare **managed challenge** — `Just a moment...`, `challenges.cloudflare.com`. **Not** `1010` |

⚠ **The artifact is why this was answerable.** `gh run view --log` and `--log-failed` return
byte-identical output with every line labelled `UNKNOWN STEP` for that run, so a missing line there
is not evidence of one. The stderr line **did** print — the control's diagnosis surface was never the
weak part; the log reader was.

#### ✅ The header is LOAD-BEARING — and the retraction that got here is the part worth keeping

**Do not delete `UserAgent` as dead weight. Removing it re-breaks the CI push.** That is measured by
running the discriminator, not inferred.

An A-B-A pattern over four CI runs at four separated times, one variable, and **nothing changed at
the edge by anyone** — the operator's setup was entirely through the service's API (a plugin target,
two credentials, four repo secrets; no WAF, firewall or proxy rule touched):

| run | header | `verify-push` |
|---|---|---|
| `430b6cc` | absent | **failure** — `403`, `text/html`, managed challenge |
| `87c4064` | **present** | success, `status: done` |
| `1de95bf` | **present** | success |
| the discriminator (closed, branch deleted) | absent | **failure** — `403`, `text/html`, managed challenge |

Two observations per arm, interleaved. ⚠ Still not a proof — the edge is a third party and could in
principle vary on its own in a way that coincided with the variable twice — but an A-B-A with
separated observations is the strongest shape available from outside, and the actionable conclusion
is established.

**And the retraction it replaced is kept, because the error is more instructive than the result.** An
earlier version of this section, and of `push.go`'s `UserAgent` doc, claimed the user agent was
*measured not to be* the cause. Two real measurements backed it: three workstation pushes on Go's
default UA answered `200`, and an unauthenticated read-API probe from that workstation was answered
`401` **by the app** under the default, an honest and a browser-shaped UA alike.

**Both measurements are true; neither supports the conclusion.** That workstation is never
challenged, so at that origin the user agent has nothing to overcome — I measured the dimension at a
point where it is **inert** and concluded the dimension does not matter. Two points, both on the flat
part of the curve, and having two of them made it *feel* safer rather than *be* safer. The rule says
to measure at a boundary **and** a middle; both of mine were in the middle.

⚠ **What must not be read into it:** the header is an honest identification, **not** a browser
impersonation, and not a technique for passing a challenge. It works because an identifiable
non-browser client is treated differently from an anonymous one — not because it looks like a
browser. If a challenge stands in front of this client again, the answer is an operator allow rule for
the endpoint or the address range — never a better disguise, and never disabled TLS verification.

#### ✅ And the loud-refusal fix validated itself in the wild

The discriminator's failure is the first time the new message ran on the real path, triggered by
something other than a test:

```
the push leg was refused BEFORE REACHING THE SERVICE: 403 Forbidden with Content-Type
"text/html; charset=UTF-8", and the body is not JSON. It looks like a Cloudflare MANAGED
CHALLENGE (an interstitial that expects a browser to run JavaScript), which no HTTP client can pass.
🔴 THIS IS NOT A TOKEN OR PAYLOAD PROBLEM …
```

and that run's whole walk log was **5,674 bytes** where the old error string alone had been
**816,059**. Both halves of the item confirmed by an independent trigger rather than by their own
tests.

#### 🔴 A new datum that strengthens keeping the pixel diff advisory

That first CI push reported **`4 changed`** pages against a workstation baseline, with `0
size-changed` — i.e. four of six pages differ visually between **chromium 153.0.8010.36 (the runner's
snap)** and **153.0.8010.52 (the workstation)**. Two workstation runs against each other had reported
`0 changed`.

So the pixel diff is stable across runs on one machine and **not** across chromium builds. Since the
runner's chromium is an unpinned input installed fresh every run, a pixel gate would flip on somebody
else's release schedule. That is now a measured reason rather than an inherited one.

#### What was fixed here, and it is about DIAGNOSIS

- **An HTML or non-JSON body now reads as `refused BEFORE REACHING THE SERVICE`**, with `THIS IS NOT
  A TOKEN OR PAYLOAD PROBLEM`. The old text's first reading is "bad push token" — the wrong thing to
  check, and the first reading actually reached. The test is **structural** (content type, and whether
  the body parses as JSON), never a vendor keyword, because the next intermediary will not say
  "cloudflare"; the vendor is named only as a hint when it identifies itself.
- **The body cap dropped 1 MiB → 4 KiB.** The refusal produced an **816,059-byte** error string: the
  diagnosis sat in the first 200 bytes and the rest buried it in the log of the run that needed
  reading. The CI walk log is now **5,343 bytes** total.

  🔴 **AND THAT 816,059-BYTE STRING IS AN INSTANCE OF A CLASS, NOT A ONE-OFF: AN UNBOUNDED READ OF A
  HOSTILE-SHAPED RESPONSE.** The read *was* bounded — `io.LimitReader(resp.Body, 1<<20)` — which is
  exactly what makes it worth writing down: a limit chosen as "surely nothing is bigger than this" is
  not a limit on the thing that matters, which is how much of it a human has to read. The shape that
  bites is a response whose SIZE is chosen by whoever is answering, and an intermediary answering with
  a challenge page is precisely that. The fix has two halves and needs both: a cap sized for a
  DIAGNOSIS (4 KiB) rather than for a payload, and **a test that drives the real HTTP path**, because
  the cap lives in the caller's `io.LimitReader` and a unit test of the formatter cannot see it. The
  mutant that survived the first battery — raising the cap while every classifier case capped its own
  body — is the reason that test exists rather than a cheaper one.

Red at base, green at HEAD, mutation-tested — **6 mutants, 6 killed**, harness validated first:

| mutant | killed by |
|---|---|
| **the base**: the old one-line message, no edge/service distinction | the challenge and 1010 cases |
| the truncation removed | the challenge case **and** the real-HTTP cap test |
| the cap back to 1 MiB | the real-HTTP cap test **only** |
| the HTML test becomes a vendor keyword hunt | the 1010 **and** unbranded-HTML cases |
| the user agent dropped from the push leg | the both-legs test |
| the user agent dropped from the read-back leg | the both-legs test |

🔴 **The third row is why `TestTheBODYCAPIsExercisedOnTheREALHTTPPath` exists.** The classifier test
caps the body *itself*, so raising `maxDiagnosticBody` back to 1 MiB left all its cases green — the
cap lives in the caller's `io.LimitReader`, and a unit test of the formatter is structurally blind to
it. That mutant **survived** the first battery.

### 4. ✅ The nested-module escape now has a LEDGER — the deferral it replaced had no checker

The escape itself stands and is not reversed: chromedp in a separate module is a stronger claim
than a test, because a different module's packages are unreachable without a `require` in the root
`go.mod` that `DeclaredModules` would see. What was wrong was the **deferral**.

`internal/depspolicy`'s doc used to close with *"if a second nested module is ever added, the right
response is to make `go.mod` files COUNTED here"* — while the same doc established that **nothing
counted `go.mod` files**. A closing condition whose trigger nothing can observe is not a work item.
And the trigger was the wrong one: the likely event is not a second module appearing, it is **this
module's dependency set changing** — a routine `go get -u` re-breaks the toolchain pin, and the CI
assertion reads only the `go` directive.

Closed the way every comparable blind spot here is: `DeclaredNestedModules` + `NestedModuleAllowlist`
versus a **tree walk that counts `go.mod` files**, failing on grow *or* shrink.

**Two lists per module, because the two lock files legitimately differ** — measured: `go.sum` carries
`ledongthuc/pdf` and `orisano/pixelmatch` (optional deps of chromedp, in the module graph, hence
hashed) which `go.mod` does not require. One list compared against both would be permanently red, so
the difference is declared. `IsThirdParty` drops the parent module, reached through the `replace`.

**A build failure through nix, and the rows that make it one are load-bearing — measured both ways:**

| | result |
|---|---|
| with `uiaudit`, `uiaudit/go.mod`, `uiaudit/go.sum` in `onlyGo` | `nix build .#cairn-ui` → `ok internal/depspolicy` **inside the derivation** |
| with those three rows removed | `nix build` **rc=1**, and the refusal **names `onlyGo`** |

The directory row is required for the two file rows to mean anything — `cleanSourceWith` never
visits a path whose parent the filter rejected. Verified by materialising the filtered source: it
holds `uiaudit/go.mod` and `uiaudit/go.sum` and **zero** `.go` files from that directory, which is
deliberate (nothing in nix builds that module, so shipping its sources would add chromedp to every
derivation's source closure for nothing).

**Mutation battery — 6 mutants, 6 KILLED**, harness validated first:

| mutant | killed by |
|---|---|
| the directory ledger SHRINKS | the directory comparison |
| a declared dependency REMOVED (ledger says less than the file) | the `go.mod` comparison |
| a dependency ADDED to the file only (the `go get -u` shape) | both file comparisons |
| the two lists collapsed (go.sum-only entries copied into `GoMod`) | the `go.mod` comparison |
| `NestedModuleDirs` stubbed to return the ledger (a tautology) | `TestNestedModuleDirsActuallyWALKS` |
| `uiaudit/go.mod` absent from the tree | the absent-file refusal, which names `onlyGo` |

🔴 **The fifth SURVIVED the first battery and is why the walk control exists.** Stubbing the walk to
return the ledger left the comparison green, made the absent-file branch unreachable, and would have
hidden a second nested module — "reads as coverage while providing none", inside the package whose
job is refusing exactly that. The control drives the walk over a synthetic tree whose answer cannot
come from the ledger, and asserts that it does not equal the ledger.

⚠ **It still does not put those dependencies under the import ban or in the `ok` floor.** It makes
the escape's BOUNDARY observable; it does not make the escape smaller.

### 5. Deleted in round 0, and why each earned deletion rather than a defence

- **`spike/main.go` — DELETED.** It was a second copy of the sign-in that **rotted inside its own
  PR**: it still carried both defects fixed in `browser.go` — the bare first-match
  `form button[type=submit]` selector and the success check that read the *absence* of `id="token"`.
  On the merged tree it would click the GitHub button, acquire `__Host-cairn-oauth` (satisfying its
  own prefix check), get 303'd back, find the token field, and print **`SPIKE 1 FAIL: the cookie was
  not re-sent`** — a confident wrong diagnosis of a working surface. Nothing in CI ran it. My
  "a spike nobody can re-run is a claim again" defence is answered better by the walk, which
  re-measures both spike claims on every push. The **README's spike evidence stays** — it quotes the
  measurement, which is what makes it evidence.
  The second origin it carried is not lost: the cookie test below now runs at **both** `127.0.0.1`
  and `localhost` as subtests, which is a caller rather than a flag nobody passes.
- **`StylesheetCheck` — DELETED.** A strict subset of `internal/ui`'s own
  `TestTheStylesheetIsServedAsItsOwnRoute`, which asserts the exact `Content-Type` including charset,
  `X-Content-Type-Options: nosniff`, byte-equality with the stylesheet constant, a size floor, that
  no page carries an inline `<style>`, that every page links the route, and unauthenticated
  reachability — **as a hard failure in root `go test ./...`**, so in the `go` job and all three nix
  derivations. Mine was an advisory tick in a non-blocking job. `doc.go` states the rule it broke.
  ⚠ And its justifying comment was **false within this package**: it claimed no browser-side
  collector looks at a failed stylesheet fetch, while `Browser.onEvent` records any subresource
  `Status >= 400` as a first-party network event — the very thing a network zero is asserted to mean.
  `StylesheetPath` survives, because the ledger-derived structural-zero test reads it.

### 6. ✅ The session cookie's attributes are now PINNED as a browser honours them

Justification 1 used to be an assertion. `TestTheSessionCookiesFourFlagsAreHONOUREDByTheBrowser`
reads six facts back out of a real jar after a real navigation, **at two origins**:

```
HONOURED: name=__Host-cairn-session secure=true httpOnly=true sameSite=Lax path="/" domain="127.0.0.1" (host-only)
HONOURED: name=__Host-cairn-session secure=true httpOnly=true sameSite=Lax path="/" domain="localhost"  (host-only)
and RE-SENT: a second navigation to / answered 200
```

Two origins because the dimension is the **origin host**: `session.go`'s comment names `localhost`,
the pod binds `127.0.0.1`, and Chromium's trustworthy-origin rule is stated per host. The `Domain`
assertion is the subtle one — a `__Host-` cookie carrying `Domain` must be refused outright, so what
is pinned is that the jar's domain is the origin's host with **no leading dot**.

🔴 **A latent bug fixed in the same pass:** `SignIn` took the **first** `__Host-`-prefixed cookie by
jar iteration order. The auth change adds `__Host-cairn-oauth`, so which cookie was read would have
been decided by map order — and every attribute printed and asserted would have been about the wrong
one. It now names `identity.SessionCookieName`.

⚠ **What this does NOT cover, and no test here can:** that the cookie is **attached to a
cross-site-initiated callback** while `SameSite=Strict` would withhold it. That needs a real provider
redirecting from a different origin, which a hermetic walk cannot stage. It is in the blind set.

### 7. The blind set

Concurrency, real network conditions, a second browser engine, a narrowed credential, the
share flow's WRITE paths (`POST /share`, `POST /unshare` — skipped as non-GET and never
exercised), the **cross-site cookie attachment** on a provider callback (needs a real provider; see
residual 6), the session-volume-vanishes deployment as an actual boot condition rather than as
the thing sign-in would catch, and any width other than 390 and 1440.

## Gating — advisory only, deliberately

**Nothing in this job blocks, and a captured regression exits 0.** The first diff against a
nonexistent baseline flags every axe rule "new" exactly once, so a gate promoted on day one is
red for a reason that has nothing to do with the tree — the permanently-red gate this
repository already refuses, reached from a new direction.

- **The one promotion candidate, after two baseline runs:** `new_a11y_rules` non-empty. It is
  a closed set of rule ids, it is deterministic, and this harness pushes the structured detail
  it is derived from. Nothing else qualifies.
- **The pixel diff stays advisory indefinitely, and there is now a MEASURED LOCAL reason rather
  than only an inherited one.** The inherited figure is 684 visual regressions over 565 pushes
  across existing producers. The local measurement is stronger, because it is about *this* harness:

  | comparison | result |
  |---|---|
  | two runs on ONE machine (chromium 153.0.8010.52) | **`0 changed`** of 6 pages |
  | a CI run (chromium **153.0.8010.36**, the runner's snap) against that baseline | **`4 changed`** of 6, `0 size-changed` |

  So the pixel diff is stable across runs on one machine and **not across chromium builds** — and
  four of six pages moved on a *patch* difference, with no page-height change to explain it.

  🔴 **THE RUNNER'S CHROMIUM IS AN UNPINNED INPUT, INSTALLED FRESH OVER THE NETWORK EVERY RUN.** So
  a pixel gate would flip on somebody else's release schedule, on a signal a future reader will
  eventually propose promoting. **Pinning chromium is the thing that would have to happen first** —
  a fixed build (a nix-pinned one, or a version-pinned container) is the precondition, not a nicety,
  and until it exists this signal cannot gate whatever its numbers look like. A full-page height
  shift also reads as a near-100% pixel change, which is why the log annotates a `size_changed`
  page as a layout change rather than a regression.
- **Nothing LLM-derived gates anything, ever.** Blocker-key stability there is measured 0.22
  and synthesis 0.00. The read-back deliberately decodes only `summary` and `diff`; the
  persona evaluator's output is not read at all, so it cannot be printed beside the
  deterministic rows and invite exactly that.

## The `verify-push` control

The push path is **non-fatal by design** — a walk that captured is worth its artifacts even if
the push did not land. Without a control, a misconfigured token is therefore a **silent green**.
So the CI job greps the walk log for the confirmation line, and **fails when it is absent**
while credentials were present. The string is a Go constant (`pushConfirmation`) read by both
sides rather than a literal in the YAML, because a second spelling is how a control starts
matching nothing.

Three states, three outcomes, and they are distinguished rather than collapsed:

| credentials | outcome |
|---|---|
| all four absent (fork PR) | exit 3 → mapped to success by `run.sh`; `verify-push` skipped |
| some present, some absent | **hard failure** naming the absent variables — a half-configured push is the silent-green shape |
| all four present | push attempted; `verify-push` requires the confirmation line |

**It is two guards, and both have been watched to go red.** A grep that reports success over a
needle nothing can produce is the shape this repository refuses, so the step first asserts that
`uiaudit/main.go` still DEFINES the needle, then searches the log for it. Measured:

| case | outcome |
|---|---|
| the real `main.go` + a log carrying the line | **ACCEPTED** (the positive control — without it a red result cannot be told from an unmatchable needle) |
| the real `main.go` + a log where the push FAILED | REFUSED: *no push was confirmed* |
| the CONSTANT renamed, log still carrying the old line | REFUSED: *the constant no longer says this; the control would match nothing* |
| both wrong | REFUSED, by the constant guard — it runs first |
| a real walk log from a credential-less run | REFUSED: *no push was confirmed* |

⚠ The last row is why the step is gated on `credentialed == 'yes'`: without that gate the
fork-PR case, which is *supposed* to skip, would fail the control instead.

**The test-count floors carry the same treatment.** `^--- PASS` counts top-level functions
only, because `go test -v` indents a subtest's line — so both counts are floors (the numbers are
whatever the current tree measures; CI carries them and a stale copy here was already wrong once), and `FAIL`/`SKIP` are matched with `^[[:space:]]*` so an *indented*
failure is seen. Seven controls, each refusing for its own reason: the real log ACCEPTED; empty
log → *no result lines*; a top-level test removed → *15 < 16*; a **subtest row** removed →
one under the total floor (which the top-level count structurally cannot see); an appended `FAIL` → *1 failing*;
an appended `SKIP` → *1 skipped*; an appended **indented** `FAIL` → *1 failing*.

## Public-repo constraints

### 🔴 The toolchain pin was a NO-OP, and CI's own log is what proved it

**Measured, from the first run of this job:**

```
Setup go version spec 1.25
go version go1.25.14 linux/amd64
go: downloading go1.26.0 (linux/amd64)
```

It went **green on 1.26 while advertising 1.25**. Go 1.21+ defaults to `GOTOOLCHAIN=auto`, which
downloads a newer toolchain when any `go` directive in the module graph asks for one — silently,
with the pinned compiler already installed. **The same shape as the `buildGoModule` no-op
`AGENTS.md` already records, reached by a different mechanism.**

Two independent causes, so fixing one would not have been enough:

1. `uiaudit/go.mod` had been rewritten to `go 1.26` by a `go mod tidy` run on a 1.26 host. **Tidy
   raises the directive to the running toolchain and does not warn** — it had silently undone an
   earlier `go mod edit -go=1.25.0`.
2. **`chromedp v0.16.0` declares `go 1.26` itself**, as did the `cdproto` pseudo-version it pulled.
   So even a corrected directive would have been overridden by the graph.

The fix is all three together: the directive says `1.25.0`; **`chromedp` is held at v0.14.2** — the
newest release declaring `go 1.24` — with `cdproto` at the revision that release requires; and the
job sets **`GOTOOLCHAIN: local`**, which turns a recurrence into a hard failure instead of a
download. A step then **reads the version out of the compiler** and asserts the directive, because
setting a pin is not observing one.

**Proven under the real toolchain, not inferred:** `GOTOOLCHAIN=go1.25.14` builds `cairn-ui`,
vets and runs the whole suite — **21 top-level / 23 subtests / 44 total, 0 failures** — and
`GOTOOLCHAIN=local` (which forbids any switch) reports no switch needed.

⚠ **The dependency hold is the fragile half.** A routine `go get -u` in this module re-breaks the
pin, and the only thing that will say so is the assertion step. The version chart, if it needs
revisiting: `v0.16.0`/`v0.15.1`/`v0.15.0` → `go 1.26`; `v0.14.2`/`v0.14.1`/`v0.14.0` → `go 1.24`;
`v0.13.7` → `go 1.23`.

### 🔴 The hub's project name is a DENIED IDENTIFIER, env-var spellings included

**This is the one constraint that changed the design, and it was measured rather than
anticipated.** `push.go`'s package-level comment is the canonical statement; the operational
consequence is here because it is what an operator needs.

The upstream hub's project name is in `tests/leakscan.py`'s `denied-identifier` closed digest
set. AGENTS.md is explicit that the remedy is to replace the name and **not** to add an
exception — *"because there are none"*. A first draft of this module spelled the name in prose,
in comments, in the four environment variables and in the CI job:

```
leakscan: 83 finding(s) across 379 file(s) — REFUSING      (exit 1)
```

83 findings across 13 files — and four of them were the environment variables themselves. The
scanner matches the identifier inside an `UPPER_SNAKE_CASE` env-var name as readily as in a
sentence, so the mandated spelling of the four secrets could not be used. (The old spelling is
not quoted here: quoting it is itself a finding, which this README also learned the hard way.)

**So the four environment variables in this repository are:**

| this repository | every sibling producer of the same hub |
|---|---|
| `CAIRN_AUDIT_PUSH_URL` | the hub's own prefix |
| `CAIRN_AUDIT_PUSH_TOKEN` | " |
| `CAIRN_AUDIT_API_URL` | " |
| `CAIRN_AUDIT_API_TOKEN` | " |

⚠ **An operator wiring the four GitHub secrets must use the left column.** The secrets do not
exist in this repository yet, so the rename costs nothing here — but it differs from every
other producer pushing to the same hub, and a secret created under the familiar name would
leave the job permanently in its "no credentials" branch, which exits **0**. The `verify-push`
control does not catch that, because with all four absent it does not run: that state is
indistinguishable from a fork PR by design.

- **No screenshot, pixel baseline, or binary capture is ever committed.** `tests/leakscan.py`
  classifies a file with a NUL byte in its first 8000 as binary and **skips it by name**, so a
  committed PNG carrying a real name would pass the gate untouched. Captures live in the work
  directory (a CI artifact) and in the hub, which is where a baseline belongs anyway.
- **Capture only against the synthetic store.** `tests/reader_fixtures.py` builds it; no
  fixture content is invented here. axe JSON and digests ARE scanned as text, but a NEW private
  name in one would pass silently because `denied-identifier` is a closed digest set.
- **Both hub URLs are env/secret only, never literals.** `leakscan.py`'s
  `reachable-hostname` rule is a general unbounded regex over several domains, line-oriented
  across every file type, so a hostname in any fixture or doc fires it. Docs use
  `audit-hub.example.com`; the harness targets `127.0.0.1`.

## Layout

| file | purpose |
|---|---|
| `doc.go` | what the harness is for, what it cannot see, why it does not gate |
| `main.go` | flags, the two-pass walk, the signal summary, the diff block, exit codes |
| `boot.go` | the hermetic world: the fixture store, the token file, `cairn-ui`, readiness |
| `targets.go` | the ledger-derived walk and `ExpandLinks` |
| `browser.go` | Chromium, sign-in by clicking, the document-status gate, per-page capture |
| `payload.go` | the hub's push schema mirrored exactly, plus the pre-upload refusal |
| `push.go` | the multipart POST and the synchronous diff read-back |
| `embed.go` | the three vendored scripts |
| `vendor-js/` | axe, the a11y digest and the layout script, verbatim — `VENDOR.md` has the provenance |
| `spike/main.go` | the two spikes, re-runnable |
| `run.sh` | the CI entrypoint; builds both modules separately, which is the point of the layout |
| `targets_test.go` | the derivation's guards (one regression, the rest invariant, labelled) |
| `payload_test.go` | the push shape, offline — the only evidence the push leg has |
| `control_test.go` | the positive control, the structural-zero pair, the document-status gate |
