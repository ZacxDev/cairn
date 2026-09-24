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
3. **Layout at 390px and 1440px.** This surface had never been rendered at any width.
4. **A signal `/healthz` structurally cannot give.** `internal/ui/README.md` names a
   deployment whose session volume vanishes after start: readiness passes, every login fails.
   Signing in through the form is the only thing that separates those two worlds.

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
- **A `GET` row in neither `plainGET` nor `linkExpanded`** → the walk **REFUSES**, naming the
  row and both remedies. That is what stops a new route being absorbed silently.

`LedgerAccounting` requires `captured rows + skipped rows == len(ledger)` and is printed every
run: `ledger has 7 row(s); 3 target(s) derived, 4 row(s) skipped`.

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

### 3. The wire leg is UNEXERCISED

No push has left this harness to any server. Creating the hub's plugin target and minting
the push and read keys are Supabase-gated operator steps. What **is** verified is the payload's
SHAPE, offline, against the rules the server enforces — every one of which rejects the WHOLE
multi-page push rather than the page:

- refs ↔ parts integrity in both directions (a referenced part missing; an orphan part)
- per-file 16 MiB cap, body 64 MiB cap, ≤200 pages
- a strict round-trip through a `DisallowUnknownFields` decoder, and **no `perf` key**
- the closed viewport set and the closed finding-type set
- a11y details carry a top-level string `id` — the LEGACY `"<id> — <help>"` string is REFUSED
- `layout`/`perf` findings authored here are REFUSED (the server derives them from the raw
  block via `internal/signals`)
- an **empty** a11y digest is REFUSED with the reason, since sending one 400s the whole push
- `RunReport`'s struct tags are pinned against a fixture written from the hub's own field
  names, each field a distinct value, because a mis-spelled tag prints a reassuring empty diff

⚠ And that is a claim about **this copy of the server's rules**, not about the server. If
the hub tightens one, these tests stay green and the push starts failing.

### 4. Nothing else in this repository governs `uiaudit/`'s dependencies

Declared in full in `internal/depspolicy`'s package doc, section *"WHAT A NESTED MODULE
ESCAPES"*. Summary: the allowlist reads the verified-root `go.mod` only; the import ban walks
`cmd/`+`internal/` only; the `ok` floor never sees a nested module because `go test ./...` does
not descend; `flake.nix`'s `onlyGo` filter is an allowlist that excludes any new top-level
directory. **No test anywhere counts `go.mod` files.** Whoever adds a module here is the whole
review. Mitigation: nothing here ships — not packaged, not a flake output, not on a deploy
path.

### 5. The blind set

Concurrency, real network conditions, a second browser engine, a narrowed credential, the
share flow's WRITE paths (`POST /share`, `POST /unshare` — skipped as non-GET and never
exercised), the session-volume-vanishes deployment as an actual boot condition rather than as
the thing sign-in would catch, and any width other than 390 and 1440.

## Gating — advisory only, deliberately

**Nothing in this job blocks, and a captured regression exits 0.** The first diff against a
nonexistent baseline flags every axe rule "new" exactly once, so a gate promoted on day one is
red for a reason that has nothing to do with the tree — the permanently-red gate this
repository already refuses, reached from a new direction.

- **The one promotion candidate, after two baseline runs:** `new_a11y_rules` non-empty. It is
  a closed set of rule ids, it is deterministic, and this harness pushes the structured detail
  it is derived from. Nothing else qualifies.
- **The pixel diff stays advisory indefinitely.** 684 visual regressions over 565 pushes
  across existing producers. A full-page height shift also reads as a near-100% pixel change,
  which is why the log annotates a `size_changed` page as a layout change rather than a
  regression.
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

## Public-repo constraints

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
