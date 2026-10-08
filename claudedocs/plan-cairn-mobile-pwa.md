# Plan: a mobile-first browser surface, installable as a network-only PWA

This is a DESIGN, not a description of anything built. None of it exists yet. Every claim about
today's behaviour was read off `origin/main` at `a20ebab` and carries a `file:line` so it can be
re-checked. **PR #202** (an unauthenticated browser GET → 303 `/sign-in?next=`) is now MERGED on
`main` as `0d3a1fa` and deployed to the personal instance; claims about it are marked `(#202)`.
⚠ #202 moved `internal/ui/server.go` by a few lines (`ServeHTTP` is `:1231` at `0d3a1fa`, `:1223`
at `a20ebab`); every other `file:line` here is still at `a20ebab`, so re-read before editing.

**Revision 2** records the operator's answers to this plan's open questions as OPERATOR decisions
(O5–O12): the iPhone checklist is a hard gate for the CLIENT instance only (and is now a concrete
artifact below); a distinct icon per instance; pinned, synthetic install screenshots; the dismissed
iOS hint remembered in `localStorage`; standalone Back/Reload (S5) IN scope; chromium pinned and the
touch checks made blocking in their own slice (S6); #202 merged, so every "if #202 merges" gate is
gone; and THIS round builds S0 + S1 only.

Every number about the current mobile state names the instrument that produced it and what that
instrument cannot see. Every claim about browsers cites a source in the research section, says
how it was obtained, and says which engine it covers.

Examples are synthetic: the two deployments are "the personal instance" and "the client
instance", and fixture data is the uiaudit world (year-2000 dates, `alpha-notes`, `sess-000…`).

## Goal and premise

The operator's ask: *"scope and propose making cairn mobile-first responsive and installable via
PWA … include a PWA latest best practice and docs research pass in first step"*.

There are two outcomes, and they are carried in one plan because the second is only worth having
if the first holds.

1. **Mobile-first.** Every browse page is usable on a phone with a thumb. That covers reading
   entries and scopes; arcs, sessions and the bell; search with the tag filter; and sharing and
   admin. The responsive work is already partly done: there is no horizontal overflow at 390 px,
   and that is gated. The gap is touch ergonomics: tap targets, input zoom and header density.
2. **Installable.** Both instances can be added to a home screen or dock under their own name and
   run in a standalone window. A minimal service worker caches only the static shell and an
   offline page, and **never** stores a private page on the device.

### What would make this unnecessary

Drop the work, or the named half of it, if any of these holds:

- **Nobody opens cairn on a phone.** Nothing in this repository measures who uses the browser
  surface or on what device. The operator's choice of "all of" the mobile priorities is the only
  evidence of demand. If the phone use is "read one entry occasionally", the measured state below
  (no overflow, readable text) may already be enough. In that case only the input-zoom fix is
  worth doing.
- **A browser tab is enough.** The research finds that iOS 26 opens *every* site added to the
  Home Screen as a web app, with no manifest needed (R1). On iOS, then, "installable" already
  works today with a generic name and icon. The manifest's value there is the per-instance name
  and icon, and on Chromium it is the install prompt and app shortcuts. If both instances' tabs
  are told apart well enough by URL, the PWA half buys only an icon.
- **The CLI covers the phone case.** It does not: the CLI needs a shell. The escape hatch is
  named here so nobody argues it later.

### closing-condition

- **closing-condition:** `check`. Slices S0–S6 are MERGED on cairn `main` (verified by content,
  not ancestry), AND **`uiaudit/pwa_check.sh`** exits 0 on `main`, AND the `uiaudit-touch` CI job
  (S6) is NOT `continue-on-error` and reports the pinned chromium version (`gh api` on the
  workflow file plus the job log — both mechanical). `pwa_check.sh` exits **2** ("could not
  vouch", never a skip and never 0) when chromium or a built `cairn-ui` is missing, or when
  either of its own controls misbehaves.

  It boots `cairn-ui` twice over the uiaudit synthetic world: once with
  `-app-name 'cairn (alpha)' -app-icon-variant <variant A>`, once with
  `-app-name 'cairn (beta)' -app-icon-variant <variant B>` and a different `-app-theme-color`. It
  then asserts these relationships, each through chromium:

  - **(a) installability:** `Page.getInstallabilityErrors` returns `[]` on both boots. A THIRD
    boot with `-app-name` unset returns exactly `[no-manifest]`. That boot is the negative
    control, and it pins that installability is opt-in per deployment.
  - **(b) per-instance identity:** `Page.getAppManifest` parses with 0 errors on both boots. The
    two `name`s equal their flags, the two `theme_color`s differ, and `id`, `start_url` and
    `scope` are all `/`. The two manifests' icon URLs DIFFER and each fetched icon's bytes equal
    the committed file for its variant (O6). The manifest lists ≥ 1 `narrow` and ≥ 1 `wide`
    screenshot, each fetchable, each a PNG whose IHDR size matches its declared `sizes`, and each
    byte-equal to the committed, derivation-checked file (O7).
  - **(c) never stores private data:** the instrument is CDP `CacheStorage.requestCacheNames` +
    `requestEntries`. After a signed-in walk of every GET row at the mobile viewport, CacheStorage
    holds EXACTLY the shell set the binary declares. The assertion fails if the set GROWS or
    SHRINKS. Every cached body is also free of every fixture scope name, entry ref and session id.
    Positive control for that check: the same walk over a scratch tree whose SW `cache.put`s
    navigations must report the leak.
  - **(d) offline fallback:** with the network emulated offline, a navigation to `/scope?id=…`
    renders the offline page. Its body carries none of the fixture's private strings.
  - **(e) mobile ergonomics at the `mobile` and `tablet` (touch) rungs:** 0 axe `target-size`
    violations, 0 visible text inputs with computed `font-size` < 16 px, 0 horizontal overflow.
    The first two are NEW measurements (S0).
  - **(f) no-store:** every HTML response from a non-public row carries `Cache-Control: no-store`.
  - **(g) client-side storage is exactly what O8 allows:** after the signed-in walk,
    `localStorage` for the origin is EMPTY (Chromium has no `navigator.standalone`, so the iOS
    hint never renders); and on a page where the test defines `navigator.standalone = false`,
    dismissing the hint leaves EXACTLY one key, `cairn.installHintDismissed`, with value `"1"`.
    Set equality: a second key, or a different value, is red.

  `--self-test` sabotages each clause on a scratch copy of the tree. The shape follows the
  `tests/control_mutants.py` pattern, and every copy has its `.git` removed. The sabotages:

  | clause | sabotage |
  |---|---|
  | (a) | drop the manifest link |
  | (b) | hardcode the name |
  | (c) | `cache.put` navigations |
  | (d) | an offline page rendered through `shell` with a viewer |
  | (e) | two sabotages, one per new measurement: shrink the scope page's `.view-tab`s to 12×12 px with no gap (the adjacent-12-px shape the S0 control measured RED — a LONE small target passes 2.5.8's spacing exception, so it would not be a sabotage), and revert the 16 px input rule |
  | (f) | restore the empty `Cache-Control` |
  | (g) | `pwa.js` writes a second key (e.g. a timestamp) beside the dismissal flag |

  Each sabotage must be reported caught by its OWN clause's message, and the run prints
  `sabotaged=N caught=N`.

  ⚠ **This is a CHECK THAT EXITS, NOT "the uiaudit CI row is green".** The `uiaudit` job is
  `continue-on-error: true` (`.github/workflows/ci.yml:1660`), so a red row does not block a merge.
  The closing condition is the script's exit status on `main`, run by CI as a step in that job,
  or by hand. The TOUCH checks become blocking in S6 (O11), as their own job, after chromium is
  pinned; `pwa_check.sh` itself stays in the non-blocking job until somebody decides otherwise.

### Rollout, and what THIS round builds

**This round (O10):** merge this plan (PR #203), then build **S0** (uiaudit touch measurements,
report-only) and **S1** (the mobile-first CSS, which flips S0's refusals on). Nothing installable
ships this round. **S2–S6 come in later rounds**, in slice order; S6's two small PRs may land any
time after S1.

Per instance, once S2–S5 are built (NOT part of the closing condition):

- **The personal instance may ship as soon as it is built (O5).** Deploy with its `-app-name` and
  `-app-icon-variant`, and confirm the deployed `/sw.js` digest matches the binary's startup line
  (decision 9).
- **The client instance is told it can install ONLY after the operator has run the iPhone
  checklist below and recorded a PASS (O5).** Deploying the client instance armed is fine before
  that; *announcing* installation is what waits. The gate's closing evidence is the filled-in
  record, judged by the operator.

### The iPhone install checklist (Q1 → O5) — the artifact

Run on a real iPhone on the current iOS (26 at the time of writing), against the CLIENT instance
deployed with S2–S5 armed, signed OUT in Safari first. Each step names what PASS looks like; any
step that is not PASS stops the announcement. Keep the filled record beside the deploy notes (not
in this public repo if it names the deployment).

| # | step | PASS looks like | covers |
|---|---|---|---|
| 1 | Open the instance's root URL in Safari. | 303 to the sign-in page; inputs do not zoom the page when tapped. | #202, S1 (16 px inputs) |
| 2 | Root page shows the install hint; tap "dismiss"; reload. | Hint shown once, gone after reload (O8's `localStorage` key). | decision 11, O8 |
| 3 | Share → Add to Home Screen. | The proposed name is the instance's `short_name`/`name`; the icon is this instance's VARIANT, visibly different from the personal instance's if both are installed. | O2, O6 |
| 4 | Launch from the Home Screen. | Standalone: no Safari address bar or toolbar; the header is fully below the status bar; no content under a notch/home indicator. | display, decision 7 (no `viewport-fit=cover`) |
| 5 | Tap "Sign in with GitHub". | GitHub opens in an in-app sheet (not the Safari app); after signing in, the sheet CLOSES and the app window shows a signed-in page ("signed in as …"). | **R6, the OAuth hand-back** |
| 6 | Force-quit the app; relaunch. | Still signed in. | the app's own cookie jar |
| 7 | Navigate root → scope → entry, then use the in-app Back and Reload controls. | Back returns to the scope page; Reload re-fetches; both visible ONLY in the installed app, not in Safari. | S5 (O9) |
| 8 | From the entry page, use the edge-swipe back gesture. | Returns to the scope page; note whether it re-fetched (a visible reload). | Q7 (`no-store` Back cost) |
| 9 | Ring the bell on a session page you own (personal instance only, where presence runs). | The button is easy to hit (≥ 44 px); the page returns to itself. | S1, B3 |
| 10 | Open an invitation link (`/join?token=…`, a TEST invitation minted for this run) from Messages. | Record WHERE it opens. Expected: Safari, not the installed app (iOS does not route links into Home Screen apps — R8). Accept it there; the GitHub flow completes and lands on the scope. Then open the installed app: it needs its own sign-in (no cookie sharing with Safari, R6). | `/join` over the same flight |
| 11 | In the installed app, Sign out; then edge-swipe back. | The sign-in page; going back does NOT show the previous private page's content. | decision 8, decision 13, T5 |
| 12 | Turn on Airplane mode; navigate. | The offline page (no private text on it); turning the network back on and navigating recovers. | O1, decision 7 |

Record template (copy outside the repo): device model · iOS version · instance (personal/client) ·
`sw=` digest from the startup line · result per step (PASS / FAIL + one line) · operator initials.

**If step 5 or 10 fails:** the credential form still works (R6), so the instance stays usable; the
announcement waits and a `window.open` fallback becomes a separate, evidenced change.

## STEP 1 — What's current: the research pass (October 2026)

The research covered primary sources first: web.dev, MDN, the W3C Manifest and Service Workers
material, Chrome/Edge docs, WebKit and Apple, Firefox release notes and the WCAG 2.2 Understanding
docs.

Tags: **[V]** means the page was fetched and read. **[S]** means search-snippet or secondary
only. **[M]** means measured here with chromium 154 (see "Measured current mobile state").
Engine scope is named on each finding. Treat [S] as a hypothesis.

### R1. Install criteria per engine

- **Chromium (Chrome, Edge; desktop and Android).**
  - What is required: HTTPS, a manifest with `name` or `short_name`, 192 px and 512 px icons,
    `start_url`, and a `display` of `fullscreen`/`standalone`/`minimal-ui`/`window-controls-overlay`.
    `prefer_related_applications` must not be true.
  - The automatic promotion also needs user engagement: one click and 30 s on the page.
  - Sources: [web.dev/articles/install-criteria] [V];
    [learn.microsoft.com/…/progressive-web-apps/how-to/] [V] for Edge.
  - **A service worker with a fetch handler is NO LONGER required.** It was dropped in Chrome 108
    on mobile and 112 on desktop [developer.chrome.com/blog/update-install-criteria] [V].
  - **[M]** Confirmed here on chromium 154 with an empty-handler worker and with no worker at all.
    `Page.getInstallabilityErrors` returned `[]` for a page with a valid manifest and NO service
    worker.
- **Safari, iOS/iPadOS 26.** "By default, every website added to the Home Screen opens as a web
  app"; the user can untick "Open as Web App"
  [webkit.org/blog/16993/news-from-wwdc25-…-safari-26-beta/] [V]. Before 26 it needed
  `display: standalone` or the `apple-mobile-web-app-capable` meta. Since 16.4, Add to Home
  Screen also works from third-party iOS browsers through the share menu [MDN, Making PWAs
  installable] [V].
- **Safari, macOS (Sonoma / Safari 17+).** File → Add to Dock works with or without a manifest.
  Cookies are copied from Safari once, at add time, and are separate afterwards
  [developer.apple.com/videos/play/wwdc2023/10120/] [V].
- **Firefox desktop.** 143 added "web apps" pinned to the taskbar. It is **Windows only** and
  disabled on Linux and macOS [Firefox 143 release notes; Mozilla taskbar-tabs docs] [V].
  Whether it reads the manifest is **unconfirmed**; MDN still says Firefox does not install from
  a manifest [V, possibly stale].
- **Firefox Android.** Home-screen shortcuts, not WebAPKs [MDN] [V]. Display-mode behaviour today
  is **uncertain** [S, a 2017 source].

### R2. Manifest members

**Required for Chromium install:**
- `name` or `short_name`
- `icons` (192 and 512)
- `start_url`
- `display` or `display_override`

Sources: [web.dev/articles/add-manifest] [V]; MDN manifest reference [V].

**Strongly recommended:**
- **`id`.** Without it Chrome derives the id from `start_url`, and changing `start_url` later
  makes the browser treat the app as a new one. It is also origin-bound
  [developer.chrome.com/docs/capabilities/pwa-manifest-id] [V]. The same-origin rule has a
  consequence here: the personal and client instances live on different origins, so they are
  **distinct apps by construction**. Per-instance identity needs no clever id; it needs only a
  different name and colour.
- **`scope`.** Decides in-scope versus out-of-scope navigation, which is what R6 turns on.
- **`icons`.** Purposes are `any` (the default), `maskable` and `monochrome`. Ship the maskable
  icon as a SEPARATE file: its safe-zone padding looks wrong when it is shown as an `any` icon
  [MDN icons] [V]; the separate-file advice itself is [S, common guidance].
- **`description`.** At most 300 characters [V].
- **`screenshots`.** Chromium's richer install dialog needs at least one screenshot whose
  `form_factor` matches: `narrow` for mobile, `wide` for desktop. Each side must be 320–3840 px.
  Up to 5 are shown on mobile and 8 on desktop
  [developer.chrome.com/blog/richer-pwa-installation] [V].
- **`shortcuts`.** `name` and `url` are required, and each `url` must be in `scope`. Chrome
  Android shows **3**; Windows shows 10. Icons are optional, with 192 px recommended. **iOS
  ignores shortcuts** [web.dev/articles/app-shortcuts; MDN shortcuts; firt.dev/notes/pwa-ios] [V].
  Browsers re-read a manifest at most about once a day [V].
- **`theme_color` / `background_color`.** `background_color` paints the Android splash.
  **The manifest has NO shipped dark-mode colour member.** A `color_scheme_dark` proposal exists,
  but its ship status is UNVERIFIED [S]. Per-scheme colour is done in-page, with `<meta
  name="theme-color" media="(prefers-color-scheme: …)">`, which has "limited availability" [MDN
  theme-color] [V].

**What iOS reads** [firt.dev/notes/pwa-ios] [V, written before iOS 17, so re-check]:
- Honoured: `name`, `short_name`, `display`, `start_url`, `scope`, `icons` (since 15.4),
  `theme_color` (15.0) and `id` (16.4).
- **Ignored:** `background_color`, `shortcuts`, `screenshots`, `orientation`.
- **`<link rel="apple-touch-icon">` OVERRIDES the manifest icons.**

### R3. Service-worker lifecycle and updates

- **How updates are detected.** An update is detected when the worker script is byte-different.
  The check runs on navigation and on functional events [web.dev/articles/service-worker-lifecycle]
  [V].
- **Do not rename the worker script** (`sw-v2.js`). Cached HTML would keep pointing at the old
  name, and updates would stall [V].
- **Pass `updateViaCache: 'none'` explicitly.** MDN's register page lists `all` as the default
  where the spec and other sources say `imports` [MDN `ServiceWorkerContainer.register`] [V, the
  conflict is real]. Setting it explicitly removes the question.
- **No silent `skipWaiting()` while clients are open.** It makes old pages run under new worker
  logic.
  - Recommended pattern: detect `registration.waiting` (`updatefound` → `statechange`) and show
    a "new version — reload" banner.
  - On click, `postMessage` the waiting worker. It calls `self.skipWaiting()`, and the page
    reloads once on `controllerchange` [developer.chrome.com/docs/workbox/handling-service-worker-updates] [V].
- **Version caches by name and delete stale ones in `activate`.** Delete only your own prefix
  [V].
- **Navigation preload.** Enable it in `activate` and read it with `event.preloadResponse`. It
  removes worker start-up latency from a network-first navigation. Support: Chrome 59, Firefox 99,
  Safari 15.4 [web.dev/blog/navigation-preload] [V].
- **Scope and origin.**
  - The worker script must be same-origin.
  - Its default maximum scope is its own directory. A wider scope needs the
    `Service-Worker-Allowed` response header [MDN register] [V].
  - So a worker served at `/sw.js` covers `/` with no header.

### R4. Why authenticated HTML must not be cached, and how to guarantee it

- **The service-worker side is a design rule, not a single source:**
  - never `cache.put` a navigation response;
  - precache an explicit allowlist (stylesheet, icons, offline page);
  - for navigations, use the network or preload response, and fall back to the cached offline
    page only when the network fails.

  The Cache API ignores `Cache-Control`, so no response header protects anything a worker chooses
  to store. The worker's code is the only guard on that path. That is why the closing condition
  measures CacheStorage STATE rather than headers.
- **`Cache-Control: no-store` and bfcache (Chromium).** Chrome now admits `no-store` pages to the
  back/forward cache; rollout reached 100% in spring 2025. Such a page is EVICTED when cookies or
  other authorisation change [developer.chrome.com/docs/web-platform/bfcache-ccns] [V]. So
  `no-store` costs less than it used to, and a sign-out (which clears the cookie) still evicts the
  page.
- **`Clear-Site-Data`** [MDN] [V]. Directive by directive:
  - `"cache"` clears the HTTP cache, bfcache and script caches.
  - `"cookies"` clears the origin's cookies **and those of its subdomains under the registrable
    domain**.
  - `"storage"` clears local, session and IndexedDB storage **and unregisters service workers**.
    The worker comes back on the next load that registers it.
  - `"executionContexts"` reloads every open context.
  - `"*"` clears everything.

  Support [caniuse] [V]:
  - `cookies`/`storage`: Chrome 61, Firefox 63, Safari 17.
  - `cache`: Safari 17; Chrome is listed as partial; Firefox dropped it in 94 and restored it in
    138.

  **Advisable at sign-out: `"cache"` only.** `"cookies"` reaches a sibling host under the same
  registrable domain. If the two instances share one, signing out of one would sign the user out
  of the other. `"storage"` buys nothing when CacheStorage holds only public bytes.

### R5. Install prompts

- **`beforeinstallprompt`** is not Baseline and lives only in WICG Manifest Incubations. It fires
  on **Chromium only** (Chrome, Edge, Samsung) [MDN beforeinstallprompt] [V; Chromium-only is [S]
  but consistent with MDN]. The in-page Install button therefore stays hidden until the event
  fires.
- **iOS** has no programmatic prompt. The user goes Share → Add to Home Screen, which since iOS 26
  has an "Open as Web App" toggle [V].
- **The two successors have not shipped:**
  - `navigator.install()`: an Edge origin trial in 143–148 [blogs.windows.com, Web Install API]
    [V]; a Chrome origin trial [S].
  - an `<install>` element: a Chrome/Edge origin trial in 148–153, which requires a manifest `id`
    [developer.chrome.com/blog/install-element-ot] [V].

  Neither belongs in v1. Setting `id` now keeps the door open.

### R6. 🔴 OAuth sign-in from an installed standalone app

**Verdict for cairn's flow**

| Platform | Expected result | Confidence |
|---|---|---|
| Desktop Chrome/Edge installed app | works | high [V] |
| Android Chrome WebAPK | works | high; cookies are shared with the Chrome profile [web.dev/articles/webapks] [V] |
| iOS Home Screen web app | **expected to work** | **medium** |

On iOS this must be tested on a real device before iOS install is announced.

The flow is: a top-level form POST to `/sign-in/github` → 303 to the provider → GitHub → provider
→ 303 to `/sign-in/github/callback`, which needs the `__Host-cairn-oauth` flight cookie
(`internal/ui/oauth.go:412-445`, `:564`, `:631`).

**What the sources say about iOS:**
- **Out-of-scope pages open in an in-app browser sheet.** "In Home Screen web apps on iOS, links
  outside the scope will open in Safari View Controller" [WWDC23 10120] [V].
- **OAuth stays in the app.** "Authentication through OAuth on a third-party domain will still
  open in your web app. This is done through heuristics" [WWDC23] [V].
- **The hand-back carries the app's storage.** When the external flow redirects to an in-scope
  URL — "including POST requests, JavaScript redirects or links" — "the PWA closes the browser
  and loads the content in the standalone window". The in-app browser "shares storage context
  with the opener PWA" [firt.dev/ios-12.2] [V, an old iOS version]. web.dev says the same in
  2022: the main window navigates to the in-scope URL [web.dev/learn/pwa/windows] [V].
- **The installed app does NOT share cookies with Safari.** A user signed in to GitHub in Safari
  signs in to GitHub again inside the sheet. Passkeys help. Only macOS copies cookies, once
  [WWDC23] [V].

**What is NOT known:**
- No WebKit bug or Apple document was found covering iOS 17/18/26 behaviour for a
  **server-side** redirect flow.
- Two third-party PRs from September 2026 report OAuth escaping to Safari. Both were client-side
  PKCE flows, and both say they were **not tested on a device** [V, anecdotal; links omitted].
- Whether `window.open` from a click works as Apple's fallback is disputed: web.dev (2022) says it
  "returns null", WWDC23 says it opens in the app [V both].

**Why cairn is in the favourable case:**
1. **The whole flow is top-level, same-window redirects.** There is no popup and no
   `window.open`.
2. **The PKCE verifier is SERVER-side** (`oauth.go:114-125`), so nothing in client storage has to
   survive the sheet.
3. **The flight cookie is `SameSite=Lax`** (`oauth.go:407-420`), which is what a cross-site
   top-level GET callback needs.
4. **The callback path is in `scope` `/`.**

**The residual failure mode is safe.** If the callback ran in a jar without the flight cookie, the
callback answers its existing `oauthNotStarted` refusal (`oauth.go:631-640`). That fails closed:
no session is minted in the wrong jar. The credential form, which always renders
(`render.go:1686`), stays a working door. **What breaks is convenience, not safety.**

That is why the real-device check is a rollout gate (operator decision O5; the checklist is in
"Rollout" above) and not a design change.
`/join?token=…` (invitation acceptance) rides the same flight (`render.go:2388`) and needs the
same check.

### R7. Mobile layout

- **Safe-area insets.** `env(safe-area-inset-*)` is non-zero only under `viewport-fit=cover` [MDN
  env()] [V]. Without `cover`, the UA keeps content inside the safe area.
- **Target size.**
  - WCAG **2.5.8 (AA)**: 24×24 CSS px, with spacing, inline-text, equivalent and user-agent
    exceptions [w3.org/WAI/WCAG22/Understanding/target-size-minimum] [V].
  - **2.5.5 (AAA)**: 44×44 [V].
  - Apple HIG: 44 pt. Material: 48 dp [S].
- **Viewport units.** `svh`/`lvh`/`dvh`: Chrome 108, Firefox 101, Safari 15.4
  [web.dev/blog/viewport-units] [V].
- **Overscroll.** `overscroll-behavior` is not Baseline per MDN [V]; Safari support starts at 16
  [S].
- **iOS input zoom.** iOS zooms into a focused input whose font is below 16 px. The fix is
  `font-size: max(16px, 1em)` on inputs; iOS 10+ ignores `maximum-scale` [S, css-tricks]. This is
  WebKit-specific.

### R8. Standalone-mode quirks

- **No back button in iOS standalone.** An edge swipe exists; Android has system back [S]. In-app
  navigation is required.
- **No pull-to-refresh in iOS standalone** [S].
- **Out-of-scope links open outside the app window.** iOS opens them in Safari View Controller
  [V], Mac Safari web apps in the default browser [V], and Chromium in an in-app browser [V].
- **`@media (display-mode: standalone)`** is widely available [MDN] [V].
- **Window titles.** The desktop app window shows `<title>` [general knowledge; not checked].

### R9. Future work (one line each, OUT of scope)

- **Web Push:** iOS 16.4+, Home Screen apps only, with permission from a user gesture
  [webkit.org/blog/13878] [V].
- **Web Share Target (`share_target`):** experimental and effectively Chromium-on-Android only
  [MDN] [V/S].

### Sources

- https://web.dev/articles/install-criteria
- https://developer.chrome.com/blog/update-install-criteria
- https://learn.microsoft.com/en-us/microsoft-edge/progressive-web-apps/how-to/
- https://webkit.org/blog/16993/news-from-wwdc25-web-technology-coming-this-fall-in-safari-26-beta/
- https://www.firefox.com/en-US/firefox/143.0/releasenotes/
- https://developer.mozilla.org/en-US/docs/Web/Progressive_web_apps/Guides/Making_PWAs_installable
- https://web.dev/articles/add-manifest
- https://developer.mozilla.org/en-US/docs/Web/Progressive_web_apps/Manifest/Reference/shortcuts
- https://developer.mozilla.org/en-US/docs/Web/Progressive_web_apps/Manifest/Reference/icons
- https://web.dev/articles/app-shortcuts
- https://developer.chrome.com/blog/richer-pwa-installation
- https://developer.chrome.com/docs/capabilities/pwa-manifest-id
- https://github.com/w3c/manifest/issues/975 (dark-mode colour proposal) [S]
- https://developer.mozilla.org/en-US/docs/Web/HTML/Reference/Elements/meta/name/theme-color
- https://firt.dev/notes/pwa-ios/
- https://firt.dev/ios-12.2/
- https://web.dev/learn/pwa/windows
- https://developer.apple.com/videos/play/wwdc2023/10120/
- https://web.dev/articles/webapks
- https://web.dev/articles/service-worker-lifecycle
- https://developer.chrome.com/docs/workbox/handling-service-worker-updates
- https://web.dev/blog/navigation-preload
- https://developer.mozilla.org/en-US/docs/Web/API/ServiceWorkerContainer/register
- https://developer.chrome.com/docs/web-platform/bfcache-ccns
- https://developer.mozilla.org/en-US/docs/Web/HTTP/Reference/Headers/Clear-Site-Data
- https://caniuse.com/mdn-http_headers_clear-site-data_cache
- https://developer.mozilla.org/en-US/docs/Web/API/Window/beforeinstallprompt_event
- https://blogs.windows.com/msedgedev/2025/11/24/the-web-install-api-is-ready-for-testing/
- https://developer.chrome.com/blog/install-element-ot
- https://www.w3.org/WAI/WCAG22/Understanding/target-size-minimum.html
- https://www.w3.org/WAI/WCAG22/Understanding/target-size-enhanced.html
- https://developer.mozilla.org/en-US/docs/Web/CSS/Reference/Values/env
- https://web.dev/blog/viewport-units
- https://developer.mozilla.org/en-US/docs/Web/CSS/Reference/Properties/overscroll-behavior
- https://developer.mozilla.org/en-US/docs/Web/CSS/Reference/At-rules/@media/display-mode
- https://webkit.org/blog/13878/web-push-for-web-apps-on-ios-and-ipados/
- https://developer.mozilla.org/en-US/docs/Web/Progressive_web_apps/Manifest/Reference/share_target

## STEP 2 — What exists today, read off the code

### Rendering and the page frames

- **There are THREE frames, not one.**
  - `shell()` is the authenticated frame (`internal/ui/render.go:584-664`).
  - `SignInPage` (`:1686`) and `JoinPage` (`:2388`) build their own `c.HTML5` frames on purpose,
    because a public page must not offer authenticated navigation
    (`TestNoPublicPageOffersAuthenticatedNavigation`, `navaffordance_test.go:207`).

  So any head element a PWA needs (manifest link, theme-color meta, apple-touch-icon,
  registration script) has THREE call sites. **Recommendation: one `pwaHead()` helper that all
  three frames call.** That follows the "one rule, one place" rule, with a test that every frame
  carries it.
- **The viewport meta is emitted by gomponents, unconditionally.** It is
  `width=device-width, initial-scale=1`, and the comment at `render.go:588-598` says a second tag
  would duplicate it. **Consequence:** `viewport-fit=cover` cannot be added without replacing
  `c.HTML5`'s head. Decision 7 avoids needing it.
- **The header** is the wordmark plus three nav links (`Arcs`, `Sharing`, `Invitations`), the
  viewer line and a sign-out form. Every link is unconditional (`render.go:601-659`). The
  breadcrumb sits in the frame (`:660`, `breadcrumbs` `:666-682`), so every authenticated page
  already has a way "up" to the root. That matters in standalone, where there is no back button.
- **Search** is a plain server-side `GET` form on `/`. The tag filter rides along as a hidden input
  (`render.go:684-720`). There is no script.

### Stylesheet, colour scheme and breakpoints

- Tailwind v4 compiles `internal/ui/tailwind.css` into the committed `internal/ui/app.css`.
  `checks.ui-stylesheet-is-current` regenerates and diffs it (`flake.nix:1170-1195`), and
  `stylesheet.go` serves it at a content-hashed, `immutable` path.
- **The palette is DARK, ALWAYS.** There is no `prefers-color-scheme` anywhere, by operator
  decision, and `TestTheStylesheetHasNoColourSchemePreference` asserts the absence
  (`tailwind.css:41-55`). `color-scheme: dark` is set at `:182`.

  **Consequence for the PWA:** ONE `theme_color` and ONE `theme-color` meta, with no `media`
  variants. A light/dark theme-color pair would contradict a pinned decision.

  The surface token `oklch(0.21 0.008 75)` (`tailwind.css:60`) converts to approximately
  **`#1a1814`**. That is a hand conversion: the manifest wants an sRGB hex, and `oklch` support in
  manifest parsers was not measured.
- **Breakpoints** are Tailwind's `sm` (40rem), `lg` (64rem) and `xl` (80rem), plus
  `--breakpoint-ultra: 125rem` (`tailwind.css:109`). The body ladder is at `:221-222`.
- **The only pointer-aware rules** are `hover:` variants. The stylesheet contains NO
  `(pointer: coarse)` rule.
- **Inputs are `text-sm`, which is 14 px** (`tailwind.css:438-443`). On iOS that zooms the page
  when the field is focused (R7) — measured below.

### Script policy

- **`AllowedScriptSources()` is exactly ONE entry**, `FilterScriptPath`, the content-hashed
  `filter.js` (`internal/ui/script.go:52, 66-68`). Three guards hold it:
  - the renderer guard;
  - uiaudit's `refuseWalkRegressions`, reading `document.scripts`;
  - `TestTheFilterScriptTouchesOnlyWhatItSays`, a SPELLING guard over the file.
- A service worker is NOT a `<script>` element, so it is not governed by that list. The
  REGISTRATION script is. **Both need a ledger** (decision 5).
- 🔴 **The worker CANNOT use the content-hash mechanism.** Update detection is a byte-diff
  against a STABLE URL, and renaming the worker stalls updates (R3). So `/sw.js` is the first
  script row on this surface with an unversioned path. It needs `Cache-Control: no-cache` and
  must never be `immutable`.

  That conflicts with a measured fact in `stylesheet.go:40-60`: **the edge in front of the
  deployment LENGTHENED a `max-age=300` to 14400.** A browser bypasses ITS OWN HTTP cache for the
  worker script, but an edge cache is not the browser's. That is why threat T3 and decision 9
  exist.

### No CSP

`writeHTML` sends no `Content-Security-Policy`, by operator decision. `server.go:1646-1733` records
the accepted blast radius, and `routes_test.go:1041-1075` pins the absence.

**A PWA needs none:**
- no `worker-src` is required to register a worker;
- the manifest needs no `manifest-src`;
- nothing here re-opens the decision.

What the decision costs a PWA is that an injected script can call `navigator.serviceWorker.register`.
It can only register a same-origin JavaScript URL, though, and the exact-match route ledger serves
JavaScript at exactly the paths it names. Decision 6 adds a request-derived refusal on top of that.

### Caching of HTML today

- **Every HTML page is sent with NO `Cache-Control`** (`writeHTMLCached`, `server.go:1764-1779`).
- The ONE exception is the invitation mint response, sent as `no-store` (`writeHTMLNoStore`,
  `:1738-1760`).
- That comment calls a surface-wide value "a caching decision about the whole surface, and it is
  not this one". **This plan is that decision** (decision 8): the operator's "never store
  private/authenticated pages on the device" is a statement about the HTTP layer too, not only
  about the worker.

### Routes, and the gates a PWA row passes through

- The route table is an exact-match map (`routes.go:117-275`).
  - Public rows (`classPublic`) are dispatched BEFORE authentication (`server.go:1238-1244`).
  - Today's public rows: the sign-in pair, the GitHub pair, `/join`, both stylesheet rows and
    the filter script.
- The hand ledger is `TestTheRouteLedgerMatchesTheDispatchTable` (`routes_test.go:55-100`). The
  routes test also carries the `bareGETAnswer` map (`:415`) and `contentAuthority` (`:702`).
- **The unauthenticated answer at `a20ebab`:** `GET /` with `Accept: text/html` gets a 303 to
  `/sign-in`. Every other unauthenticated request gets a uniform 401 `unauthorized`
  (`server.go:1246-1270`).
  - (#202, MERGED as `0d3a1fa`) widens that to every HTML GET/HEAD: a 303 to
    `/sign-in?next=<uri>`.
  - It keeps a 401 for `Accept: */*`, so curl and a worker's own fetches are unchanged.
  - **The manifest, worker, icons, screenshots and offline page MUST be public rows.** Chromium
    fetches the manifest WITHOUT credentials unless the link says
    `crossorigin="use-credentials"` [MDN manifest] [S], and none of them may consult an
    authority.
- **`/favicon.ico` is not a row.** It answers 401, and uiaudit carries a carve-out for it
  (`uiaudit/browser.go:222-242`). A `<link rel="icon">` in `pwaHead()` stops the request at its
  source.

### Embedding and the nix build

- `onlyGo` admits embedded non-Go files by NAME: `internal/ui/app.css` and
  `internal/ui/filter.js` (`flake.nix:351-443`, `:429-433`).
- **Every new embedded file must be added there or the nix build stops compiling.** That covers
  `sw.js`, `pwa.js`, each icon PNG, and the offline page if it is a file. The two existing lines
  carry the comment that says so.

### Instance identity

- **`cairn-ui` has NO notion of which instance it is.** Instances exist only in the CLIENT
  (`internal/client/instances.go`). `cmd/cairn-ui/main.go:213-290` has no name, title or colour
  flag.
- **The repository's convention for a per-deployment opt-in** is a flag with NO default, off when
  unset and refused when blank. Examples:
  - `-arc-journal` (`main.go:263-266`): env `$CAIRN_ARC_JOURNAL` via `envOr`, blank refused.
  - the presence flags (`main.go:268-283`): no env spelling at all.
  - The blank-value hazard of `envOr` is recorded at `main.go:236-245`, and `controlJournalDefault`
    is its remedy.

### Governed ledgers that a new row or file moves

| ledger | where | moves when |
|---|---|---|
| route table + hand ledger + `bareGETAnswer` (+ `contentAuthority` for content rows) | `routes.go`, `routes_test.go:55, 415, 702` | any new row |
| `TestEveryServedPathComesFromTheLedger` near-miss probes | `routes_test.go:488` | a computed (hashed) key |
| public-page navigation ledger | `navaffordance_test.go:207` | a new public PAGE (the offline page) |
| `AllowedScriptSources` | `script.go:66` | a new `<script>` (`pwa.js`) |
| `onlyGo` | `flake.nix:351-443` | any new `//go:embed` |
| nix regenerate-and-diff checks | `flake.nix` beside `checks.ui-stylesheet-is-current` (`:1170`) | a committed GENERATED binary: icons per variant (S2), install screenshots (S4) |
| the uiaudit job's `continue-on-error` and its chromium install step | `ci.yml:1660`, `:1745-1751` (apt/snap, unpinned) | S6 only: pin chromium, then a SEPARATE blocking job (the promotion path `ci.yml:1655-1659` prescribes) |
| `tests/control_mutants.py` rows; pinned by `tests/test_control_mutant_count_is_pinned.py` into `ci.yml:848, 865, 930` and `internal/control/README.md:261, 265, 275, 417` | `PKGS` includes `./internal/ui/` (`control_mutants.py:115`) | any Go-side guard. Count **279** at `a20ebab`; **287** on `main` at `0d3a1fa` (read off `ci.yml:865`'s step name there) |
| uiaudit test floors | `ci.yml:1817` (29 top-level), `:1821` (64 total) | a new uiaudit test |
| `internal/ui/README.md` phases; `uiaudit/README.md` | — | every slice |
| `depspolicy` allowlist | `internal/depspolicy` | **nothing in this plan**: no new Go module, asserted |

### uiaudit — what it measures, and whether it can gate "mobile-first"

- **The walk is derived from the route ledger.** It walks every GET row, follows published links,
  and captures at five widths (`uiaudit/targets.go:71-87`). Mobile is 390×844 with `Touch: true`;
  tablet is 834×1112, also touch.
- **What REFUSES** (`refuseWalkRegressions`, `uiaudit/main.go:495-575`):
  - horizontal overflow, at every width;
  - a script outside the allowlist;
  - a capture with no axe engine;
  - a content-width floor, at ultrawide only;
  - a collapsed width matrix.
- **What is only REPORTED:**
  - `layout-smells.js`'s `small_tap_targets` (< 44 px), `small_text` and `missing_viewport_meta`;
  - axe violations.
- 🔴 **axe's `target-size` rule (WCAG 2.5.8) never runs.** It is `enabled: false` in the
  vendored axe 4.12.1, and uiaudit calls `axe.run(document, {resultTypes:["violations"]})`
  (`uiaudit/browser.go:882`), which does not enable it. Measured below.
- ⚠ **`layout-smells.js` is a CANONICAL file other harnesses copy verbatim**, and its keys are a
  push contract (`vendor-js/layout-smells.js:1-3`, `uiaudit/embed.go:14`). New measurements
  therefore go in `browser.go`, the way `ContentBox` does (`browser.go:111-160`). Adding keys to
  `layout-smells.js` would break the contract.
- 🔴 **The `uiaudit` CI job is `continue-on-error: true`** (`ci.yml:1660`): a red row, no block.
- 🔴 **It cannot emulate standalone display mode** — measured below.

## Measured current mobile state

**Instruments:**
- `uiaudit/run.sh` on `origin/main` `a20ebab`, chromium **154.0.8037.92** from `nix-shell -p
  chromium`. That is NOT CI's chromium, which is unpinned (`uiaudit/README.md` gating section).
- The token-file synthetic world: no control journal, no invite store, no OAuth provider.
- Push skipped, because no credentials were in the environment.
- 265 captures over 53 targets × 5 widths, `run.sh rc=0`.
- A second walk ran from a SCRATCH COPY of the tree (never the worktree) with two instrument
  changes:
  - axe `target-size` enabled;
  - a measurement script listing every visible `a, button, input, select, summary, [role=button]`
    with its box, and every input's computed `font-size`.

**What these instruments cannot see:**
- a real iOS or Android browser;
- WebKit at all;
- the share flow's per-scope page and the invite mint form (see "Could not measure");
- the GitHub button;
- standalone display mode.

**Controls, run first and reported as pairs:**
- **axe `target-size`:**
  - on a synthetic page with two adjacent 12×12 buttons it reported `target-size:2` when enabled,
    and **NOTHING under uiaudit's current call** — so the current walk is structurally blind to
    2.5.8;
  - two 48×48 buttons: 0 (the negative control);
  - two 14 px-high adjacent inline-block links: `target-size:2`.
- **`Page.getInstallabilityErrors` (CDP, chromium 154).**

  | case | result |
  |---|---|
  | no manifest | `[no-manifest]` |
  | manifest without icons | `[manifest-missing-suitable-icon, no-acceptable-icon]` |
  | valid manifest, empty-handler worker | `[]` |
  | valid manifest, no worker | `[]` |

  So the instrument goes red and green, and Chromium no longer requires a worker (R1).
- **CDP `Emulation.setEmulatedMedia` with `display-mode: standalone`:**
  `matchMedia('(display-mode: standalone)')` stayed **false** and `display-mode: browser` stayed
  true. **Standalone-only CSS is UNMEASURABLE by this harness as built.**

**Surface-wide results:**

| signal (mobile 390, touch) | result | gated today? |
|---|---|---|
| horizontal overflow | **0** of 265 captures; 0 elements with a right edge past the viewport (scratch walk) | yes |
| `<meta viewport>` missing | 0 | reported |
| text under 12 px | 0 | reported |
| axe violations (default rules) | 0 | reported |
| axe `target-size` (2.5.8 AA), enabled | **0** over 53 mobile captures | **never runs today** |
| interactive elements with a side < 24 px | **322** over 53 mobile captures | — |
| interactive elements with a side < 44 px (`small_tap_targets`) | 2785 summed over all 265 captures; **identical counts at mobile, tablet and desktop for every page** | reported |
| inputs with `font-size` < 16 px | **3 of 3** distinct inputs: `#q` (search), `#entry-filter`, `#token` (credential), all **14 px** | — |

**How to read the target rows.** axe passes everything because WCAG 2.5.8's spacing and
inline-link exceptions apply: small links that are far enough apart PASS AA. So the AA floor is
met. The 44 px comfort target (Apple HIG, WCAG 2.5.5 AAA) is missed nearly everywhere.

The identical counts across widths mean **nothing on this surface adapts to a touch pointer.**

### Per-page defects at 390 px (from the scratch walk's element boxes)

**Header — every authenticated page:**
- Two rows, 80 px tall; `<main>` starts at y=128.
- Nav links `Arcs` 30×16, `Sharing` 54×16, `Invitations` 73×16.
- `Sign out` 72×26; wordmark 65×28.
- Four nav targets and the viewer line compete for one 358 px row.

**Root `/`:**
- Card titles (`a.card-name`) 100–118 × 21.
- Search input 358×38 at 14 px font (zooms on iOS); Search button 358×38.
- The tag filter is reached by tapping a tag chip, which links `/?tag=`. Those chips are pill
  links about 20 px high (`tailwind.css:383`).

**Scope `/scope?id=…`:**
- Each entry row's ONLY link is its mono ref, about 14 px high (e.g. `gadget-two` 72×14). Up to
  24 sub-24 px targets per page.
- Tabs (`a.view-tab`) about 26 px high.
- The entry filter is 316×38 at 14 px font.
- Task-ref chips are about 20 px high.

**Scope arcs / sessions tabs:**
- Up to 38 sub-44 px targets on the arcs tab.
- Arc and session links are about 14 px high; scope and session pills about 20 px.

**Arcs `/arcs`:**
- Arc links 14 px high.
- The `show all` / `live only` toggle is 86×22.

**Arc `/arc`:**
- Breadcrumbs 24 px high; tabs 26 px.
- Scope pills about 19–20 px.

**Session `/session` (the bell):**
- `Ring` is **49×22**: the one button on the page and the one a thumb most needs to hit.
- The presence badge, scope pills and arc pills are about 20 px.

**Entry `/entry`, rendered and raw:**
- Breadcrumbs 24 px; view tabs 26 px (`raw` 44×26).
- The raw `<pre>` wraps (`whitespace-pre-wrap`), so there is no inner horizontal scroll.

**Share index / invite index:**
- Only the header's targets. The forms behind them are not reached (see below).

**Sign-in:**
- Credential field 200×38 at 14 px (zooms on iOS); `Sign in` 78×38.
- It is the only page with 0 sub-24 px targets, along with `/join`.

**Screenshots** (local artifacts, never committed) confirm the layout reads correctly as one
column; the defects above are ergonomic, not structural.

**Tables:** there are no `<table>` elements on any page captured. The "tables" defect class is
absent today.

## Decisions — who chose what

### Chosen by the OPERATOR (not re-litigated)

| # | the operator's choice | cost accepted |
|---|---|---|
| O1 | **Offline: installable, NETWORK-ONLY.** "Manifest, icons, standalone window, a minimal service worker that caches only the static shell (stylesheet/icons) plus an offline fallback page. It must NEVER store private/authenticated pages on the device." | No offline reading. Chromium shows its own offline page when there is none, so the fallback page is polish plus a guaranteed state on WebKit (R1). |
| O2 | **Instances: BOTH installable, per-instance name.** "The manifest's name/short_name/theme come from instance config, so the personal and client installs are distinguishable." | A deploy-time config line per instance (decision 1). |
| O3 | **Extras in scope:** app shortcuts (Arcs, Search, …), an in-page "Install" affordance and a "new version, reload" banner. Web Push and the share target are OUT (R9). | One more script on every page (decision 5). Shortcuts are invisible on iOS (R2). |
| O4 | **Mobile priorities: all of** reading entries/scopes, arcs + sessions + the bell, search with the tag filter, and sharing/admin. | The admin forms are unmeasured today (see "Could not measure"), so S0 widens the harness world before S1 claims them. |

Revision 2 — the operator's answers to this plan's open questions, quoted as relayed:

| # | the operator's choice | cost accepted / where it lands |
|---|---|---|
| O5 | **Q1: YES.** "The client instance is told it can install only after the operator runs the iPhone checklist (install → GitHub sign-in → land signed in → `/join`). The personal instance may ship as soon as it's built." | The client announcement waits on a human with a device. The checklist is the artifact under "Rollout". |
| O6 | **Q2: YES, a distinct icon per instance**, via the plan's own mechanism: "a flag (no default) that selects a committed, pre-generated variant, with no per-deployment binary input." | A closed set of committed icon files per variant; folded into S2 (decision 3, decision 1's `-app-icon-variant`). |
| O7 | **Q3: YES, install screenshots**, "generated ONLY from the uiaudit synthetic world in a nix derivation, never hand-taken", pinned regenerate-and-diff like `app.css`, synthetic data only. | A chromium-in-the-sandbox derivation and committed PNGs; folded into S4 (decision 16). |
| O8 | **Q4: YES, remember the dismissed iOS hint in `localStorage`.** | The plan's ONE client-side write; decision 11 and T9 say exactly what and when it clears. |
| O9 | **S5 (standalone Back/Reload): IN v1 scope**, "verified on device per Q5's recommendation." | S5 is required by the closing condition's slice list; its behaviour is verified by checklist step 7, because no harness here can emulate standalone ([M]). |
| O10 | **Scope this round:** "merge the plan, then build S0 + S1"; S2–S5 in later rounds. | Nothing installable ships this round. |
| O11 | **Q6:** "pin CI's chromium first, then flip the uiaudit job to blocking for the touch checks in a SEPARATE small PR." | Slice S6 (two PRs), with its own closing check. |
| O12 | **Q8 is moot:** #202 is MERGED on `main` (`0d3a1fa`) and deployed to the personal instance. | Every #202 gate in this plan is satisfied. |

### Chosen by the AGENT writing this plan (open to review)

1. **Instance identity comes from four new `cairn-ui` flags, and the first one ARMS the feature.**
   - `-app-name` (env `CAIRN_UI_APP_NAME`): **NO default.** Unset, there is no manifest link, no
     registration script and no worker. `/manifest.webmanifest` answers 404, and `/sw.js` answers
     the unregistering worker (decision 9).
   - `-app-short-name` (env `CAIRN_UI_APP_SHORT_NAME`): optional, ≤ 12 characters, refused if
     longer. If unset, it is omitted from the manifest.
   - `-app-theme-color` (env `CAIRN_UI_APP_THEME_COLOR`): optional, `#rrggbb` only. If unset, it
     falls back to the stylesheet's surface colour, which is a property of the theme, not of the
     instance.
   - `-app-icon-variant` (env `CAIRN_UI_APP_ICON_VARIANT`) — **O6**: **NO default**, and
     **REQUIRED whenever `-app-name` is set** (startup refuses one without the other, so no
     deployment silently gets "the" icon). Its value must be a member of the CLOSED set
     `ui.IconVariants()` (decision 3); anything else is refused at startup, naming the set. It
     selects among committed files; no deployment supplies image bytes.

   All four refuse a blank or whitespace value at startup, the `controlJournalDefault` way
   (`main.go:236-245`). The env spellings are new names, not renames, so `internal/envalias` does
   not move. The startup line names the armed app name, the way `signInMode` is named
   (`main.go:765-770`).

   *Evidence:* the `-arc-journal` precedent (no default, env via the same reader); the blank-value
   hazard is recorded at `main.go:236-245`. *Why a flag and not a hostname:* the process cannot
   know its external origin (`routes.go` `JoinPath` comment), and a `Host` header is
   proxy-chosen.
2. **The manifest is a PUBLIC, server-rendered row at a FIXED path**, `GET /manifest.webmanifest`.
   - Headers: `application/manifest+json`, `Cache-Control: no-cache`, `nosniff`.
   - It is rendered from `encoding/json` over a Go struct, never by string concatenation, so a name
     containing `"` stays a value.
   - Members:
     - `id: "/"`, `start_url: "/"`, `scope: "/"`, `display: "standalone"`;
     - `name`, optional `short_name`, `description` (a constant), `theme_color` and
       `background_color` (decision 1);
     - `icons` (decision 3);
     - `shortcuts` (decision 10).
   - **`screenshots` (O7, added in S4):** the committed, derivation-pinned set of decision 16 —
     `form_factor: "narrow"` and `"wide"` entries pointing at content-hashed public rows. Absent
     from the manifest until S4 lands.
   - **No `display_override`.** Nothing beyond `standalone` is wanted.
   - Not hashed: `id` is explicit, so the URL could move, but a stable URL is simpler and the
     manifest is per-instance bytes.
3. **Icons: a committed SVG source, committed PNGs, and a nix regenerate-and-diff check.** This
   is the `app.css` discipline exactly.
   - **Source:** `internal/ui/icons/cairn.svg`, which is text, so leakscan reads it.
   - **Committed outputs:** 192 and 512 `any`, 512 `maskable` (an 80 % safe zone on a surface
     background) and a 180 px `apple-touch-icon` (opaque).
   - **Generator:** a `uiIcons` nix derivation with nixpkgs' `resvg`, metadata stripped.
   - **Check:** `checks.ui-icons-are-current`, which runs a negative control first, as
     `ui-stylesheet-is-current` does.
   - **Serving:** content-hashed `immutable` public rows, e.g. `/static/icon-192.<12 hex>.png`,
     through `hashAsset`.
   - **A distinct icon per instance (O6), as a CLOSED set of committed VARIANTS.** The SVG is one
     template with a small set of parameters (the background tint and a corner mark); each variant
     is a named parameter tuple in ONE committed file, `internal/ui/icons/variants.json`, which
     `ui.IconVariants()` embeds and the nix derivation reads with `builtins.fromJSON` — one list,
     two readers, no copy (it joins `onlyGo` with the PNGs). Variant names are
     neutral (`amber`, `teal`, …), never an instance's name, because this repo is public and must
     not learn which deployment is which. Files: every variant × {192 any, 512 any, 512 maskable,
     180 apple-touch}.
   - **Every variant's files are routes** (static, finite, content-hashed), so the ledger does not
     depend on configuration; `pwaHead()` and the manifest link only the SELECTED variant's.
   - **Guards:** the set of embedded icon files EQUALS `IconVariants() × sizes` (fails on GROW or
     SHRINK); `checks.ui-icons-are-current` regenerates EVERY variant and diffs; two variants'
     512 px files must differ (a relationship, so a template that ignores its parameters is red).

   *Alternative considered:* rasterise in Go at init with stdlib `image/png` and manual
   supersampling. That means no binary in the repo, but a hand rasteriser is more code than the
   problem deserves; `golang.org/x/image/vector` would be a new module.
4. **`pwaHead()` is the ONE place the PWA head elements are spelled**, and all three frames call
   it. It emits:
   - `<link rel="manifest">`;
   - one `<meta name="theme-color">` (no `media`, because dark always);
   - `<link rel="icon">` and `<link rel="apple-touch-icon">`;
   - the registration script tag.

   It emits NOTHING when `-app-name` is unset. A test asserts every frame's output carries all of
   them when armed, and none when unarmed. The test runs over the frames' LEDGER, so a fourth
   frame cannot skip it.
5. **Two scripts, two ledgers.**
   - **`pwa.js`** is the registration, install and update UI. It is a SECOND entry in
     `AllowedScriptSources`, content-hashed and `classPublic`, linked by `pwaHead()`. It:
     - registers `/sw.js` with `{scope: "/", updateViaCache: "none"}`;
     - reveals a hidden Install button on `beforeinstallprompt`;
     - reveals a hidden update banner when `registration.waiting` exists while a controller is
       active;
     - on Reload, `postMessage`s `SKIP_WAITING` and reloads once on `controllerchange`.

     It writes only `hidden` and one `textContent`, like `filter.js`, plus the ONE storage key of
     decision 11 (O8). Its spelling guard (`TestThePWAScriptTouchesOnlyWhatItSays`) refuses
     `innerHTML`, `eval`, `fetch`, `document.cookie`, `sessionStorage`, `indexedDB` and `caches.`
     (the last is the worker's business, never the page's); it allows `localStorage` ONLY as
     `localStorage.getItem(HINT_KEY)` / `localStorage.setItem(HINT_KEY, "1")` with `HINT_KEY`
     spelled once as `"cairn.installHintDismissed"`. It is a SPELLING guard, labelled as one;
     closing-condition clause (g) is the STATE guard.
   - **`sw.js`** is NOT in `AllowedScriptSources`, because it is never a `<script>` element. It
     gets its own exact row at `/sw.js` (`classPublic`, `text/javascript`, `Cache-Control:
     no-cache`, `nosniff`) and its own ledger: `ServiceWorkerShell()` is the declared shell set.
     The server templates that set into the worker body from Go (decision 7).
6. **Only `/sw.js` may be fetched as a worker script.**
   - A browser sends `Service-Worker: script` on every worker-script fetch [MDN register] [S].
   - Rule: a request carrying that header on any path other than `/sw.js` answers 403.
   - The rule is derived from the REQUEST, the way `stateChanging` is (`server.go:1299`), and
     never from a route class, because a class may only make a route LESS protected
     (`routes.go:28-39`).
   - It closes "register some other same-origin JavaScript as a worker". Today that is only
     `filter.js`, whose maximum scope would be `/static/`, so this is defence in depth, and is
     labelled as such.
7. **The worker is network-only, with an allowlist, and it never touches a non-GET.**
   - **`install`:** `caches.open("cairn-shell-<digest of the worker body>")`, then
     `addAll(SHELL)`. SHELL is exactly: the hashed stylesheet, the hashed `pwa.js`, the icons and
     `/offline`.
   - **`activate`:** delete every `cairn-shell-*` cache except the current one; enable navigation
     preload.
   - **`fetch`:**
     - a non-GET → `return` (no `respondWith`, so the browser's own path, `Origin` and all, is
       untouched; gate 2 depends on it);
     - `request.mode === "navigate"` → `respondWith(preloadResponse ?? fetch(request))`, which
       on rejection ONLY answers `caches.match("/offline")`. A navigation response is never
       stored. A navigation's `redirect: manual` turns #202's 303 into an opaque redirect the
       browser follows normally;
     - a GET whose URL is in SHELL → the cache first, then the network;
     - anything else → `return`.
   - **No `skipWaiting()` in `install`.** Only on the page's message.
   - **No `viewport-fit=cover`.** Without it the UA keeps content inside the safe area (R7), so no
     inset CSS is needed, and `c.HTML5`'s head stays as it is. The cost is letterboxing on a
     landscape notch, which is accepted.
   - The SHELL digest moves when any shell asset's bytes move, so the worker's bytes move with it
     and an update is detected (R3). A Go-only deploy does not change the worker. That is fine,
     because HTML is never cached: the next navigation is the new HTML anyway.
8. **Every HTML response from a non-public row is `Cache-Control: no-store`.**
   - `writeHTML`'s default changes from no header to `no-store`, and `writeHTMLNoStore` collapses
     into it. Public pages (sign-in, join, offline) get `no-cache`.
   - This is the HTTP half of O1: a worker that caches nothing does not stop the browser's own
     disk cache from holding an authenticated page.
   - *Measured cost: none on Chromium's bfcache* (R4: `no-store` pages are admitted, and evicted on
     a cookie change). On Safari and Firefox, back navigation re-fetches. That is a
     server-rendered surface paying one request.
   - The writeHTML comment called this "a caching decision about the whole surface". It is, and
     it is flagged for operator review.
9. **Worker currency is a DEPLOY property, and the worker carries its own kill switch.**
   - The startup line prints `sw=<digest>`.
   - The rollout step (not a CI gate, because CI cannot see the edge) fetches the DEPLOYED
     `/sw.js` through the edge and compares digests. This is the response to the measured
     edge-lengthening in `stylesheet.go:40-60`: the deployment must bypass the edge cache for
     `/sw.js`, and the probe is what proves it did.
   - **With `-app-name` unset, `/sw.js` answers a worker that unregisters itself.** Turning the
     feature off on a deployment therefore REMOVES it from every browser that next visits.
   - ⚠ **A ROLLBACK to a pre-PWA binary is different.** `/sw.js` then answers 404 or 401, the
     update fails, and the old worker STAYS. It is harmless, because it only passes requests
     through to the network and serves public bytes, and it is stated as residual R-1.
10. **Shortcuts:** Arcs → `/arcs`, Search → `/#q`, Sharing → `/share`.
    - That is three, Chrome Android's limit.
    - No shortcut icons in v1; the app icon stands in.
    - `#q` scrolls to the search field. Focusing it would need script, and plain HTML does not
      autofocus on a fragment.
    - 🔴 **This decision depended on #202, and that dependency is now SATISFIED (O12: merged as
      `0d3a1fa`).** Without it, a shortcut opened with an expired session would land on `/arcs` →
      a plaintext `401 unauthorized` inside a standalone window with no address bar and no back
      button. With it, the answer is a 303 to `/sign-in?next=/arcs`, and sign-in lands back on
      `/arcs`. S4's test plan pins that per shortcut, so a regression of #202 turns S4 red.
11. **The Install affordance.**
    - **Chromium:** a hidden `<button class="install">` in the header, revealed only by
      `beforeinstallprompt`.
    - **iOS:** there is no event. `pwa.js` instead reveals a one-line hint on the ROOT page:
      "Install: Share → Add to Home Screen". It is shown only when
      `"standalone" in navigator && navigator.standalone === false`. That is feature detection of
      an iOS-only property, never user-agent sniffing.
    - **Everything is hidden** when `matchMedia("(display-mode: standalone)")` matches, or when
      script is off. The hidden-until-revealed rule is `filter.js`'s
      (`internal/ui/README.md` Phase H).
    - **The hint has a dismiss button, and the dismissal is REMEMBERED in `localStorage` (O8).**
      This is the plan's ONE client-side write, and it is exactly:
      - key `cairn.installHintDismissed`, value `"1"`, written only on a tap of dismiss, read only
        to decide whether to reveal the hint. No user data, no identity, no timestamp, no URL; it
        is a property of the DEVICE'S BROWSER, not of a session or a principal.
      - Every access is wrapped in `try/catch`: a private window or blocked storage THROWS, and
        then the hint simply shows (fail-visible, never fail-broken).
      - **Where it lives:** in the browser tab's storage for this origin (the hint only renders
        when `navigator.standalone === false`, i.e. in Safari, never in the installed app, whose
        storage is separate anyway — R6).
      - **When it is cleared:** when the user clears website data for the origin; when the browser
        evicts script-writable storage (Safari's tracking prevention can cap it after days of no
        interaction [S] — the hint then reappears, which is harmless); and NEVER by sign-out
        (decision 13).
      - **Sign-out does NOT clear it, deliberately.** It carries nothing about the signed-in
        user, so there is nothing for sign-out to protect, and clearing it would re-show a
        dismissed hint to the same person on the same device after every sign-out. Clearing it
        would also need `Clear-Site-Data: "storage"` (which unregisters the worker, decision 13) or
        a sign-out script hook (a second write path) — both cost more than the key is worth.
        Turning the feature off (`-app-name` unset) leaves the key orphaned and unread; harmless,
        and stated.
12. **The update banner** is a hidden `role="status"` element in `shell()` with one Reload
    button. Its meaning, honestly stated: **"the static shell changed since this window loaded"**,
    not "any deploy happened". The HTML is network-only, so a stale page can only be one that has
    been open since before the deploy.
13. **Sign-out sends `Clear-Site-Data: "cache"`, and nothing else.** That clears the HTTP cache and
    bfcache, belt-and-braces over decision 8. `"cookies"` would sign the user out of a sibling
    instance under a shared registrable domain (R4); the session cookie is already cleared
    explicitly (`session.go:338`). `"storage"` would unregister the worker for no benefit, because
    CacheStorage holds only public bytes — and it would also wipe decision 11's dismissal flag,
    which sign-out deliberately keeps (O8). ⚠ So `localStorage` is NOT cleared by sign-out; that is
    the decision, not an oversight.
14. **The mobile-first CSS is pointer-driven, not width-driven.** Every rule below sits under
    `@media (pointer: coarse)`:
    - `min-height: 44px` and a matching hit area (padding or a `::after` box) on the nav links,
      every `button[type=submit]`, `.view-tab`, `.crumb`, the entry-row ref link (the whole row
      becomes the target) and the bell;
    - inputs at `font-size: max(16px, 1em)`, at EVERY width, because the zoom is triggered by
      the font, not the width;
    - the header becomes a single compact row: wordmark plus a nav that wraps beneath it.

    *Evidence:* the identical tap counts at 390, 834 and 1440 show that width breakpoints are not
    where touch lives, and a laptop with a touchscreen is coarse too. Neither `prefers-color-scheme`
    nor a light palette is introduced.
15. **uiaudit gains the measurements, in `browser.go`, not in `layout-smells.js`.**
    - axe runs with `rules: {"target-size": {enabled: true}}` at touch rungs.
    - A `Touch` capture records every visible input's computed `font-size` and every interactive
      element with a side < 44 px.
    - **Gate (refuse) at the touch rungs:** axe `target-size` violations > 0; any input < 16 px.
    - **Report, and refuse on GROWTH:** the < 44 px count, against a per-page ceiling ledger
      committed beside `targets.go`. It is a ratchet, so the count can only fall.
    - **New capture after S3:** a CacheStorage enumeration after the signed-in walk (clause c),
      and `Page.getInstallabilityErrors` on `/` (clause a).
16. **Install screenshots (O7) are BUILD OUTPUT of the synthetic world, pinned like `app.css`, and
    land in S4.**
    - **Generator:** a `uiScreenshots` nix derivation that builds `cairn-ui`, builds the uiaudit
      synthetic store with `tests/reader_fixtures.py`'s own builder (the store `uiaudit/boot.go`
      uses), boots both in the sandbox with no network, signs in with the fixture credential, and
      captures with nixpkgs' chromium (`headless=new`, device scale 1, animations off, fonts from a
      pinned fontconfig). Captures: `narrow` 390×844 of `/` and `/arcs`; `wide` 1440×900 of `/`.
      Rendered with a FIXED synthetic `-app-name` (e.g. `cairn`), so no instance name is ever in a
      committed pixel.
    - **Pinned:** the PNGs are committed under `internal/ui/screenshots/`, embedded, served at
      content-hashed public rows, and `checks.ui-screenshots-are-current` regenerates and diffs
      them byte-for-byte, negative control first.
    - **Why S4 and not S2:** a screenshot depicts the UI, so it belongs after S1's CSS and with the
      manifest change that already moves in S4 (shortcuts); putting it in S2 would make every S3/S4
      visual tweak a screenshot regeneration in a slice about something else.
    - 🔴 **How `leakscan`'s binary blind spot is covered.** `leakscan` skips a PNG by name, so it
      cannot read these files. What covers them is PROVENANCE, enforced: the committed bytes must
      equal what the derivation renders, and the derivation's only data input is the synthetic
      fixture world (`tests/reader_fixtures.py`, which `leakscan` DOES scan, plus the committed
      tree). A hand-taken or hand-edited screenshot fails `ui-screenshots-are-current`; a
      screenshot of real data cannot be produced by the derivation at all, because no real data
      is an input. The derivation's `src` is a filtered set (the `onlyGo` pattern) so a stray
      local file cannot become an input. ⚠ This is a guarantee about CONTENT ORIGIN, not a scan;
      it holds only while the fixture world itself stays synthetic, which `leakscan` checks.
    - ⚠ **Risk, unmeasured:** byte-identical PNGs across two hosts from the same nix chromium. One
      machine was measured stable run to run (`uiaudit/README.md`: `0 changed` of 6); two hosts were
      not. If S4 measures a cross-host difference, the check compares DECODED pixels at zero
      tolerance instead of bytes — still exact, just not encoder-sensitive — and says so.
17. **S6 pins CI's chromium and makes the TOUCH checks blocking, in that order, as two PRs (O11).**
    - **S6a — pin.** The uiaudit job stops installing chromium from apt/snap (`ci.yml:1745-1751`)
      and takes it from the flake's pinned nixpkgs (the same chromium the screenshot derivation
      uses), printing its version. Its closing check: the job log's `chromium --version` equals the
      version the flake lock resolves to, on two consecutive runs.
    - **S6b — block.** A NEW job, `uiaudit-touch`, with NO `continue-on-error`, running only the
      touch refusals (axe `target-size`, input font, overflow at the touch rungs) on the pinned
      chromium. This is the promotion path the workflow itself prescribes — split the assertion
      into its own blocking job, do not delete `continue-on-error` from the advisory one
      (`ci.yml:1655-1659`). Its closing check: on a scratch branch whose scope-page `.view-tab`s are
      shrunk to 12×12 px with no gap, `uiaudit-touch` FAILS with the target-size refusal's own message
      (negative control), and on `main` it passes (positive control).

## Threat model

| threat | control |
|---|---|
| **T1. Cached private data** — an authenticated page stored on the device by the worker | Decision 7: the fetch handler never stores a navigation and caches only the declared SHELL. Guards: (i) the STATE guard, closing-condition clause (c), which enumerates CacheStorage after a signed-in walk and checks it EQUALS the shell set and carries no fixture private string; (ii) a SPELLING guard over `sw.js` (no `cache.put`, `addAll` only over `SHELL`), labelled as one; (iii) clause (c)'s positive control. |
| **T1b. …or by the browser's HTTP cache / bfcache** | Decision 8 (`no-store` on every non-public HTML response) plus decision 13 (`Clear-Site-Data: "cache"` at sign-out); Chromium evicts bfcache on cookie change (R4). Guard: clause (f), a header assertion over every non-public GET row in the ledger walk. |
| **T1c. …in the offline page itself** | It is a PUBLIC page built like `SignInPage`: no viewer, no CSRF token, no store read. It joins the `TestNoPublicPageOffersAuthenticatedNavigation` ledger, and clause (d) asserts no fixture string appears in it. |
| **T2. Worker scope hijack** — another same-origin script registered as a worker, or `/sw.js` widened | The worker's scope is `/` by path, with no `Service-Worker-Allowed` anywhere (asserted: no response carries it). Decision 6 refuses `Service-Worker: script` on every other row. The route map is exact-match, so no user-controlled path serves JavaScript (an entry's raw view is `text/html`, `nosniff`). Same-SITE sibling hosts cannot register on this ORIGIN, because workers are origin-scoped. ⚠ A CDN that injects script (`uiaudit/README.md` blind set) can register `/sw.js`, but only OUR worker, and it can already do worse. |
| **T3. An update that strands users on an old worker** | No silent `skipWaiting` (decision 7). Even a stale worker is harmless by construction: it passes everything through except the immutable hashed assets it precached. A new page's NEW hashed stylesheet misses its cache and goes to the network. 🔴 The real stranding vector is the EDGE caching `/sw.js` (measured to lengthen max-age, `stylesheet.go:40-60`), which defeats byte-diff detection. Controls: `no-cache` on the row, an edge bypass rule as a deploy requirement, and the deployed-digest probe (decision 9). Rollback leaves the old worker in place (R-1); turning the feature off removes it (decision 9). |
| **T4. The OAuth redirect leaves standalone** | R6: expected to stay in the app on iOS via the in-app sheet and hand back to the window. The flow is top-level only, the verifier is server-side and the flight cookie is `Lax`. If it does not hand back, the callback fails CLOSED with `oauthNotStarted` (`oauth.go:631-640`) and no session is minted in a foreign jar. The credential form remains. The gate is the real-device check (O5: checklist steps 5 and 10, required before the CLIENT instance is told it can install), not code. |
| **T5. Sign-out with an installed app** | `POST /sign-out` revokes server-side first (`session.go:329-341`), so even a surviving page cannot act: its CSRF token is derived from a dead session. Add `Clear-Site-Data: "cache"` (decision 13). The worker survives sign-out by design (it holds nothing private), and so does the installed icon. ⚠ Not `"cookies"`: it reaches a sibling instance under the registrable domain (R4). |
| **T6. Manifest as an information leak** | The manifest is public and carries only the instance's configured name, colour and constant paths. A configured name is visible to anyone who can reach the sign-in page. That is accepted, and documented in the flag's help: do not put anything in `-app-name` you would not put on the sign-in page. |
| **T7. Shortcut / start_url dead ends** | Closed for the expired-session case by #202 (MERGED, `0d3a1fa`): an unauthenticated HTML GET is a 303 to `/sign-in?next=…`; S4's test pins it per shortcut. Plain-text 404/500 answers still have no navigation in a standalone window — S5's in-app Back control (O9) and recommendation B4 cover that. |
| **T9. Client-side storage** | The plan's ONE client-side write is decision 11's `cairn.installHintDismissed = "1"` (O8): no user data, no identity, written only on a tap, `try/catch`-wrapped. Nothing else is written to `localStorage`, `sessionStorage`, IndexedDB or cookies by script (spelling guard over `pwa.js`; closing-condition clause (g) as the STATE guard: exactly that key or none). It is NOT cleared at sign-out, deliberately (decision 11): it says nothing about who was signed in. An XSS that can read it learns only "this browser dismissed a hint". |
| **T10. A committed binary carrying real data** (icons, screenshots — `leakscan` skips binaries by name) | Provenance, enforced by regenerate-and-diff: icons are rendered from the committed SVG template and `variants.json` (decision 3); screenshots only from the synthetic fixture world with a fixed synthetic app name (decision 16). A hand-made or real-data image cannot equal the derivation's output, so `ui-icons-are-current` / `ui-screenshots-are-current` go red. Residual: a non-synthetic string added to the FIXTURE world would be rendered into a screenshot — but the fixture is text, and `leakscan` scans it. |
| **T8. Clickjacking of the installed surface** | Unchanged. The no-CSP decision (`server.go:1646-1733`) is not reopened, and installation adds no framing path. |

## Slices

Each slice is mergeable alone, and each leaves `main` releasable.

| slice | what | ledgers it moves | mergeable alone because |
|---|---|---|---|
| **S0** *(THIS round, O10)* | **Measure first.** uiaudit gains decision 15's measurements: axe `target-size` enabled at the touch rungs, input `font-size`, and the < 44 px element list with a per-page ceiling ledger. They are REPORTED, not refused, because input-font is RED on `main` (3 of 3 inputs at 14 px). It also widens the uiaudit world to a CONTROL-JOURNAL boot, so the share per-scope page and the invite mint form are captured (O4). | `uiaudit/browser.go`, `main.go` (report lines), new `uiaudit/touch_test.go` (+ its controls), the `ci.yml:1817/1821` floors, `uiaudit/README.md`, `uiaudit/boot.go` (journal world). NOT `layout-smells.js` (a push contract). No product change. | Pure measurement. The uiaudit job is non-blocking. |
| **S1** *(THIS round, O10)* | **Mobile-first CSS** (decision 14), with the S0 refusals flipped ON: input < 16 px and `target-size` refuse at touch rungs, and the < 44 px ceiling ledger set to the post-fix counts. | `internal/ui/tailwind.css` → regenerated `app.css`, so the hashed stylesheet path moves (`routes_test.go` recomputes it); `render.go` only if a row needs a wrapping element; `internal/ui/README.md` (new phase); uiaudit ceilings. No route, no script. | CSS-only on the product side. |
| **S2** | **Manifest, per-instance icon VARIANTS, instance flags, `pwaHead()`** (decisions 1–4; O2, O6). Installable on Chromium from here (no worker needed, [M]). | Routes: `GET /manifest.webmanifest` (public) and one hashed public row per icon FILE (every variant × 4 sizes), in the hand ledger and `bareGETAnswer`, with near-miss probes in `TestEveryServedPathComesFromTheLedger`. `onlyGo` gets every PNG, `icons/variants.json` and the SVG if embedded. `flake.nix`: `uiIcons` (all variants) + `checks.ui-icons-are-current`. `cmd/cairn-ui` flags (`-app-name`, `-app-short-name`, `-app-theme-color`, `-app-icon-variant`) + tests. `tests/control_mutants.py` rows, listed below. `internal/ui/README.md`; root `README.md` (the new flags). | Inert unless `-app-name` is set (a default-off flag). |
| **S3** | **Worker, offline page, `no-store`, sign-out header, and the `Service-Worker` refusal** (decisions 5-sw, 6, 7, 8, 9, 13). | Routes: `GET /sw.js` (public), `GET /offline` (public page; joins the navaffordance ledger). `onlyGo` gets `internal/ui/sw.js`. `writeHTML`'s default changes and `writeHTMLNoStore` is removed. `session.go` gets the sign-out header. The dispatcher gets the decision-6 rule. Mutant rows. uiaudit: the CacheStorage clause (c), the offline clause (d) and the header clause (f), each with controls. `cmd/cairn-ui` gets the `sw=` digest in the startup line. | With `-app-name` unset the worker unregisters itself, so S3 is safe to deploy unarmed. `no-store` is a header change, rollback-safe. |
| **S4** | **`pwa.js`: install + update UX, the iOS hint with its remembered dismissal (O8), shortcuts, and install SCREENSHOTS (O7)** (decisions 5-pwa, 10, 11, 12, 16) plus `uiaudit/pwa_check.sh` (the closing check). | `AllowedScriptSources` (2nd entry); the hashed `pwa.js` row; `onlyGo` gets `internal/ui/pwa.js` and every screenshot PNG; `TestEveryBrowsePageCarriesOnlyAllowlistedScripts` controls; the spelling guard (with its ONE allowed storage key); manifest `shortcuts` + `screenshots`; one hashed public row per screenshot; `flake.nix`: `uiScreenshots` + `checks.ui-screenshots-are-current`; mutant rows; `ci.yml` (a `pwa_check.sh` step in the uiaudit job); `internal/ui/README.md`. #202 is merged (O12), so nothing gates it. | Additive. Without it, the app is installable and updates silently on the next navigation, which is safe because HTML is network-only. |
| **S5** *(IN v1 scope, O9)* | **Standalone Back/Reload and polish:** a `display-mode: standalone` header (sticky, compact); in-app Back and Reload buttons revealed by `pwa.js` only in standalone (iOS has no back button and no pull-to-refresh, R8); `overscroll-behavior-y: contain` on `body` in standalone. | `tailwind.css` → `app.css`; `pwa.js` (still inside its spelling guard: `history.back()` and `location.reload()` added to the allowed calls, nothing else); README. ⚠ uiaudit **cannot** emulate standalone ([M]), so behaviour is verified on a device: checklist step 7. | Additive and hidden outside standalone. |
| **S6a** | **Pin CI's chromium** (decision 17, O11): the uiaudit job takes chromium from the flake's pinned nixpkgs instead of apt/snap. | `ci.yml:1745-1751`; `uiaudit/README.md`'s gating section (the "pin first" precondition becomes met). | CI-only. |
| **S6b** | **Make the TOUCH checks blocking** in a SEPARATE small PR (O11): a new `uiaudit-touch` job, no `continue-on-error`, touch refusals only. | `ci.yml` (new job; the advisory job's `continue-on-error` is NOT removed, `:1655-1659`); `uiaudit/README.md`; branch protection (an operator setting, named in the PR). Needs S1 (the refusals) and S6a. | CI-only. |

**Mutant rows** (names indicative). The pinned count is re-measured at each merge, starting from
**287** on `main` at `0d3a1fa` (#202 included). S0, S1, S5 and S6 add no Go-side guard and so no row.

- **S2:**
  - `ui-manifest-row-requires-auth`: class `public` → 0; killed by an unauthenticated manifest
    fetch expecting 200.
  - `ui-manifest-name-is-a-constant`: killed by the two-name test.
  - `ui-manifest-served-when-unarmed`.
  - `ui-pwa-head-missing-from-sign-in`: killed by the frame-ledger test.
  - `ui-app-name-blank-accepted`.
  - `ui-icon-variant-optional-when-armed`: `-app-name` without `-app-icon-variant` starts.
  - `ui-icon-variant-outside-the-set-accepted`.
  - `ui-manifest-links-every-variant`: the manifest links all variants instead of the selected one.
- **S3:**
  - `ui-html-no-store-dropped`;
  - `ui-sign-out-clears-cookies-site-wide`: `"cache"` → `"cache", "cookies"`;
  - `ui-service-worker-header-not-refused`;
  - `ui-sw-served-immutable`;
  - `ui-unarmed-sw-does-not-unregister`;
  - `ui-offline-page-renders-viewer`.
- **S4:**
  - `ui-pwa-script-not-allowlisted`;
  - `ui-pwa-script-uses-innerhtml`.
  - `ui-pwa-script-writes-a-second-storage-key`: killed by the spelling guard's key allowlist.
  - `ui-manifest-screenshot-not-the-committed-file`: the row serves bytes other than the embedded
    screenshot (killed by the byte-equality test).

**`sw.js` behaviour mutants** need a browser, so they are NOT battery rows: `tests/control_mutants.py`
runs Go tests only. They live in `pwa_check.sh --self-test` instead (clauses c and d).

### Test plan per slice (negative controls named)

**S0.**
- **The axe target-size instrument.** A browser test boots a page with two adjacent 12 px
  buttons and requires `target-size` ≥ 1 (positive control). Two 48 px buttons must give 0
  (negative control).
- **The font instrument.** A 14 px input must be reported; a 16 px input must not.
- Both controls must FAIL with the rule disabled and the measurement removed respectively. That
  proves REACHABILITY, not just breakability.
- **The ceiling ledger** refuses GROWTH: add one 12 px link to the fixture and it goes red. It
  refuses SHRINK-without-update too, so a stale ceiling cannot hide a regression.
- **The journal-backed world:** the walk must capture `/share?scope=…` and the invite page, with
  the mint form present. The capture count is asserted, so a world that silently fell back to
  token-file reads as red.

**S1.**
- On `main`, the S0 report reads 3 inputs < 16 px. After S1 it reads 0, and the refusal is ON.
- **RED at base, green at head**, for the input clause and for every touched target class.
- A literal-expectation render test that `(pointer: coarse)` rules exist for the named classes.
  It is a SPELLING guard, labelled as one; the browser measurement is the real one.
- Overflow stays 0 at all five widths. The content floor at ultrawide is unchanged, which checks
  that the header change did not leak into wide layouts.

**S2.**
- **Manifest:** JSON-decoded, with every member pinned to a LITERAL expectation. Two
  `-app-name`s in one process give two manifests (a relationship).
- **Name escaping:** a name containing `"`, `<` and a newline stays a JSON value.
- **Unarmed:** 404 for the manifest, and no `pwaHead()` output in any frame.
- **Flags:**
  - blank → exit `exitConfig`;
  - a short name of 13 characters is refused, 12 accepted;
  - `#12345` refused, `#1a1814` accepted;
  - the env value is read, and blank env is refused even when the flag is given (the
    `journalErr` rule).
- **Icons (O6):**
  - every icon row answers `image/png`, `immutable`; the set of rows EQUALS
    `IconVariants() × {192, 512, 512-maskable, 180}` (fails on GROW or SHRINK);
  - the IHDR width and height match the declared `sizes` (a decoded-PNG check, not a byte check);
  - two variants' 512 px files DIFFER (a template that ignores its parameters is red);
  - two boots with different `-app-icon-variant` link different icon URLs from manifest AND
    `pwaHead()` (relationship); `-app-name` set with the variant unset → startup refusal naming
    the flag; a variant outside the set → refusal naming the set;
  - `checks.ui-icons-are-current` regenerates EVERY variant and has its negative control.
- **Chromium installability** (uiaudit): `getInstallabilityErrors == []` armed and
  `[no-manifest]` unarmed. That pair is already measured as an instrument.

**S3.**
- **Clause (c), the CacheStorage state:** the cache keys after a signed-in walk EQUAL
  `ServiceWorkerShell()`, and no fixture private string appears in any cached body. Its control is
  a scratch tree whose worker `cache.put`s navigations, which must go red.
- **Clause (d):** an offline navigation renders `/offline`. Its control is a worker without the
  fallback, which must show Chromium's own error page and fail the clause.
- **No-store:** every non-public GET row carries `no-store`, and every public HTML row
  `no-cache`. A ledger walk is used, so a new row is covered without editing the test.
- **`Service-Worker: script`:**
  - → 403 on `/static/filter.<hash>.js` and on `/`;
  - → 200 on `/sw.js` (positive control).
- **Sign-out:** `Clear-Site-Data` equals exactly `"cache"`, pinned as the whole header value.
- **Unarmed `/sw.js`** returns the unregistering body. A browser test confirms that a page which
  had the armed worker loses its registration after one visit to an unarmed boot.
- **POST passthrough:** with the worker active, `POST /sign-in` without `Origin` is still 403
  (gate 2). That proves the worker did not rewrite the request.

**S4.**
- **The allowlist:** `pwa.js` and `filter.js` are the only two sources, and an inline or foreign
  script is still refused. These are the existing guard's controls, extended.
- **Update banner, in the browser:**
  - boot A, load a page, boot B with a changed stylesheet;
  - navigate and confirm `registration.waiting` is set and the banner is visible;
  - click Reload and confirm exactly ONE reload and the new hashed stylesheet in use.
  - Control: with the shell unchanged, there is no waiting worker and no banner.
- **Install button:** hidden by default. It is only revealed on a synthetic `beforeinstallprompt`
  dispatched in the test, which is a REACHABILITY control. Headless Chromium will not fire the
  real event without engagement.
- **Shortcuts:** each `url` is a declared GET row, and (with #202, now on `main`) each
  unauthenticated HTML GET of a shortcut url is a 303 to `/sign-in?next=<it>` — so a #202
  regression turns S4 red.
- **The remembered hint (O8), in the browser:** with `navigator.standalone` defined `false` by the
  test, the hint is visible; tap dismiss → `localStorage` holds exactly `{cairn.installHintDismissed:
  "1"}`; reload → hint hidden. Controls: with storage throwing (a stubbed `setItem` that throws),
  the hint still shows and the page logs no error; after `POST /sign-out` the key is STILL present
  (pins decision 11's "sign-out does not clear it", so nobody "fixes" it silently).
- **Screenshots (O7):** `checks.ui-screenshots-are-current` with its negative control (one byte
  appended must compare unequal); the manifest's `screenshots` entries each resolve to a declared
  row whose bytes EQUAL the embedded file and whose IHDR matches `sizes`; ≥ 1 `narrow` and ≥ 1
  `wide`. A grep-free provenance control: rebuild the derivation with ONE fixture scope renamed
  and confirm the check goes RED — proof the pixels really come from the fixture world.
- **`pwa_check.sh --self-test`:** `sabotaged=8 caught=8` (clauses a–g, two for e).

**S5 (O9).**
- `pwa.js`'s Back/Reload reveal is gated on `matchMedia("(display-mode: standalone)")`; since CDP
  cannot emulate that ([M]), the browser test stubs `matchMedia` for that one query and asserts the
  two controls appear, and that WITHOUT the stub they stay hidden (the control).
- The spelling guard still refuses everything decision 5 lists; `history.back` and
  `location.reload` are the only additions.
- **Behaviour on a device is checklist step 7** (O9 / Q5) — named as a device check, not a test.

**S6a.** The job log's `chromium --version` equals the flake-resolved version on two consecutive
runs; an edit that reintroduces apt/snap is visible in review and in the version line.

**S6b.** Negative control: a scratch branch with the scope page's `.view-tab`s at 12×12 px and no
gap fails `uiaudit-touch` with the target-size refusal's message; a scratch branch with a 14 px
input fails with the font refusal's message. Positive control: `main` passes. The job carries no
`continue-on-error` (read off the workflow file).

## Open questions

### Answered by the operator (revision 2) — recorded as O5–O12, kept here so the trail is readable

| Q | the question | the recommendation | the operator's answer |
|---|---|---|---|
| Q1 | Prove iOS sign-in on a real device before the client instance is told it can install? | YES, as a rollout gate | **YES** → O5. Client instance gated on the checklist under "Rollout"; personal instance ships when built. |
| Q2 | A distinct icon per instance? | NO for v1 (flag mechanism offered if wanted) | **YES, with that mechanism** → O6, decision 1 (`-app-icon-variant`) and decision 3, in S2. |
| Q3 | Install `screenshots`? | NO for v1 (only ever from the synthetic world if wanted) | **YES, synthetic-only, derivation-pinned** → O7, decision 16, in S4. |
| Q4 | Persist the dismissed iOS hint? | NO | **YES, in `localStorage`** → O8, decision 11, T9; sign-out deliberately does not clear it. |
| Q5 | Verify standalone-only CSS how? | minimal rules, verified on device | **As recommended**, with S5 IN scope → O9; checklist step 7. Rejected: a `?display=standalone` test hook, because a query parameter that changes rendering for everybody is a public surface, not a test seam. |
| Q6 | Make the uiaudit touch refusals blocking? | not until chromium is pinned | **Pin first, then block in a SEPARATE small PR** → O11, decision 17, S6a/S6b. |
| Q8 | Client installable before #202 merges? | no shortcuts until it merges | **Moot** → O12: #202 merged (`0d3a1fa`). |

### Still open

- **Q7. Does `no-store` (decision 8) cost anything the operator cares about?** **Recommend
  accepting it.** The visible effect is that Safari and Firefox re-fetch on Back. Checklist step 8
  measures it once on a phone; record the result before S3 merges.
- **Q9 (new). Which variant does each instance get?** A deployment choice, not a repo one: the
  manifests name it, and this public repo must not record which neutral variant name maps to which
  deployment. **Recommend** the operator picks at S2 deploy time and keeps the mapping with the
  deployment manifests.

## Recommended improvements beyond the ask (clearly recommendations)

- **B1. Whole-row targets for list rows.** On the scope page the only link is a 14 px-high mono
  ref. Make the row's title the link too, with the ref inside it, so the target is the row. This
  is the single biggest touch win measured (up to 24 small targets per scope page).
- **B2. A compact mobile header.** Four header targets at 16–28 px share one row with the
  viewer line. Under `pointer: coarse`, the nav becomes a full-width row of three 44 px links
  beneath the wordmark, and "signed in as" moves under the nav.
- **B3. The bell as a real button on touch.** `Ring` is 49×22, the smallest primary action
  measured. Give it ≥ 44 px under `pointer: coarse` and keep it visually quiet
  (`tailwind.css:472-475`).
- **B4. HTML refusal pages for browsers.** `writePlain`'s `unauthorized`, `no such route` and 500
  bodies are dead ends in a standalone window (no URL bar, no back on iOS). For `Accept:
  text/html` only, render a minimal public frame with a link to `/`, keeping the bytes uniform
  for the 401. That keeps the credential-table oracle property (`routes.go` comments).
- **B5. `<link rel="icon">` ends the favicon carve-out.** uiaudit's `/favicon.ico` special case
  (`browser.go:222-242`) becomes unnecessary once every frame links an icon. Delete the carve-out
  in the same slice rather than leave a dead branch.
- **B6. One theme-color, derived.** Generate the `#1a1814` fallback from the same token in the
  build (the Tailwind step can emit it) rather than hand-converting `oklch`, so a palette change
  cannot leave the browser chrome on the old colour.
- **B7. A "tables" policy before tables arrive.** None exist today. If one is added, require
  `overflow-x-auto` on its wrapper, the rule `pre` blocks already follow (`tailwind.css:935-987`),
  and the overflow refusal will hold it.

## What could not be measured

- **Any WebKit behaviour:** iOS input zoom, the OAuth hand-back (R6), safe-area letterboxing,
  the iOS install hint's feature detection, and back-navigation cost under `no-store`. All of
  these are deferred to the iPhone checklist (O5).
- **Real Android or desktop installation and the install prompt.** `beforeinstallprompt` needs
  engagement that headless Chromium does not supply.
- **Standalone display mode in any harness here.** CDP media emulation of `display-mode` had NO
  effect on chromium 154 [M].
- **The share flow's per-scope page and the invite mint form at 390 px.** The uiaudit world is
  token-file, which confers `admin` on nobody (`uiaudit/targets.go:129-135`), and has no invite
  store. S0 widens it.
- **The GitHub sign-in button's size.** The uiaudit world has no provider.
- **Whether the deployed edge honours `no-cache` on `/sw.js`.** That is a deployment property;
  decision 9's probe measures it at rollout.
- **CI's chromium.** All numbers are chromium 154.0.8037.92 from nixpkgs on one host. The CI
  runner's build is unpinned.
- **Whether Firefox desktop reads the manifest; Firefox Android's current display behaviour; the
  ship status of a manifest dark-colour member.** These are [S] or unverified in R1/R2. None
  changes the design, because this surface is dark always.
- **Whether the derivation-built screenshots are byte-identical across two hosts** (decision 16).
  One host is measured stable run to run; two are not. S4 measures it and falls back to a
  decoded-pixel comparison if needed.
- **How long iOS Safari keeps the O8 dismissal key.** Script-writable storage eviction is [S];
  the only consequence is the hint reappearing.
- **What the pinned (nixpkgs) chromium measures against the numbers above.** They are chromium
  154 from nixpkgs on one host; S6a makes CI use the flake's chromium, which may be a different
  build until the lock moves. S0 re-measures on that build.
- **Sizes and effort.** Nobody has measured these slices; they are not estimated.
