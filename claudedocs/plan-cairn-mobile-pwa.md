# Plan: a mobile-first browser surface, installable with no service worker

This is a DESIGN, not a description of anything built. None of it exists yet.

**Where the citations point.** Every claim about today's behaviour carries a `file:line` read off
`origin/main` at **`0d3a1fa`** (PR #202 merged). Files #202 did not touch have the same lines as at
`a20ebab`, where the measurements below were taken; the walk's numbers do not depend on #202,
which changed only the unauthenticated answer. Re-read before editing.

Every number about the current mobile state names the instrument that produced it and what that
instrument cannot see. Every claim about browsers cites a source in the research section, says how
it was obtained, and says which engine it covers.

Examples are synthetic: the two deployments are "the personal instance" and "the client instance",
and fixture data is the uiaudit world (year-2000 dates, `alpha-notes`, `sess-000…`). Operator
decisions are PARAPHRASED, never quoted: this repository does not carry anyone's messages
(`AGENTS.md`).

**Revision history.**
- *Revision 2* recorded the operator's answers to the first open questions (O5–O11).
- *Revision 3* applies the operator's decision to **drop the service worker for v1 (O13)** and the
  PR #203 audit rulings. Removed: the worker, the offline page, the shell cache, the update banner,
  `Clear-Site-Data` at sign-out, the theme-colour flag, and the per-page < 44 px ceiling ledger.
  Changed: uiaudit gains REAL touch emulation, measured to make `(pointer: coarse)` match; the
  Search shortcut became query-based; the checklist was made coherent; S3 shrank to `no-store`
  alone; and every operator decision is paraphrased. Removed decisions and threats keep their
  numbers, marked REMOVED, so references stay stable.

## Goal and premise

The operator asked for cairn's browser surface to become mobile-first and installable as a
progressive web app, with a research pass on current best practice as the first step.

There are two outcomes, carried in one plan because the second is only worth having if the first
holds.

1. **Mobile-first.** Every browse page is usable on a phone with a thumb. That covers reading
   entries and scopes; arcs, sessions and the bell; search with the tag filter; and sharing and
   admin. The responsive layout already works: there is no horizontal overflow at 390 px, and that
   is gated. The gap is touch ergonomics: tap targets, input zoom and header density.
2. **Installable.** Both instances can be added to a home screen or dock under their own name and
   icon, and run in a standalone window. **Nothing is stored on the device by construction**: there
   is no service worker, so there is no cache to get wrong. The one exception is a single
   dismissal flag for the iOS install hint (O8). Offline, the browser shows its own error page.

### What would make this unnecessary

Drop the work, or the named half of it, if any of these holds:

- **Nobody opens cairn on a phone.** Nothing here measures who uses the browser surface or on what
  device. The operator's choice of every mobile priority is the only evidence of demand. If phone
  use is "read one entry occasionally", the measured state below (no overflow, readable text) may
  already be enough, and only the input-zoom fix is worth doing.
- **A browser tab is enough.** iOS 26 opens *every* site added to the Home Screen as a web app,
  with no manifest needed (R1). On iOS, "installable" already works today with a generic name and
  icon. The manifest's value there is the per-instance name and icon; on Chromium it is the
  install prompt and app shortcuts. If both instances' tabs are told apart well enough by URL, the
  install half buys only an icon.
- **The CLI covers the phone case.** It does not: the CLI needs a shell. Named so nobody argues it
  later.

### closing-condition

- **closing-condition:** `check`. Three mechanical parts, all required:
  1. Slices S0–S6b are MERGED on cairn `main`, verified by content, not ancestry.
  2. **`uiaudit/pwa_check.sh` exits 0 on `main`.**
  3. The `uiaudit-touch` CI job (S6b) is NOT `continue-on-error`, and its log reports the pinned
     chromium version. Both are read off the workflow file and the job log.

  `pwa_check.sh` exits **2** ("could not vouch", never a skip and never 0) when chromium or a built
  `cairn-ui` is missing, or when any of its own controls misbehaves.

  🔴 **ONE INSTRUMENT, NO DUPLICATED CLAUSE (audit D4).** `pwa_check.sh` is an ORCHESTRATOR. It
  boots the worlds and invokes each clause where that clause already lives; it never re-implements
  one. The table says where each clause lives and who else calls it.

  | clause | what it asserts | where it is implemented (the ONE place) | also called by |
  |---|---|---|---|
  | **(a) installability** | `Page.getInstallabilityErrors` returns `[]` on both armed boots, and exactly `[no-manifest]` on a third boot with `-app-name` unset (the negative control: installability is opt-in per deployment) | `uiaudit/pwa_test.go` | — |
  | **(b) per-instance identity** | `Page.getAppManifest` parses with 0 errors; each `name` equals its flag; `id`, `start_url` and `scope` are `/`; the two boots' icon URLs DIFFER and each fetched icon's bytes equal the committed file for its variant (O6); ≥ 1 `narrow` and ≥ 1 `wide` screenshot, each byte-equal to its committed, derivation-pinned file, IHDR matching `sizes` (O7) | `uiaudit/pwa_test.go` | — |
  | **(c) mobile ergonomics at the touch rungs** | the touch REACHABILITY control first: `matchMedia('(pointer: coarse)')` is true at every touch capture (`mobile`, `tablet`) and false at every non-touch capture (`laptop`, `desktop`, `ultrawide`), or the walk refuses (decision 15). Then 0 axe `target-size` violations, 0 visible inputs with computed `font-size` < 16 px, 0 horizontal overflow | `refuseWalkRegressions` (`uiaudit/main.go`) | the `uiaudit` walk; the `uiaudit-touch` job (S6b) |
  | **(d) no-store** | every HTML response from a non-public GET row carries `Cache-Control: no-store`, walked over the route ledger, so a new row is covered without editing the test | `internal/ui` Go test `TestEveryNonPublicHTMLRowIsNoStore` | the `go` CI job |
  | **(e) client-side storage** | after the signed-in walk, `localStorage` is EMPTY (Chromium has no `navigator.standalone`, so the iOS hint never renders). On a page where the test defines `navigator.standalone = false`, dismissing the hint leaves EXACTLY `{cairn.installHintDismissed: "1"}` | `uiaudit/pwa_test.go` | — |

  The two armed boots use the uiaudit synthetic world:
  - `-app-name 'cairn (alpha)' -app-icon-variant <variant A>`;
  - `-app-name 'cairn (beta)' -app-icon-variant <variant B>`.

  `--self-test` applies one sabotage per check, each on a scratch copy of the tree with its `.git`
  removed (the `tests/control_mutants.py` pattern). Each must be caught by its OWN clause's
  message. The run prints `sabotaged=9 caught=9` once every slice has landed.

  **Which slice wires which clause into `pwa_check.sh`, and the self-test count each slice pins:**

  | slice | clauses it wires | sabotages it adds | `sabotaged=N` it pins |
  |---|---|---|---|
  | **S2** (creates the script) | (a); (b: name, icon); **(c)**; and (d) IF S3 has already landed | 1 + 2 + 3 (+1) | **6**, or **7** if S3 landed first |
  | **S3** | (d), IF S2 has already landed (otherwise S2 wires it; see above) | +1 | **7** if S2 landed first; otherwise nothing (no script yet; (d) runs in the `go` job only) |
  | **S4** (lands after S2 AND S3) | (b: screenshots); (e) | +2 | **9** |

  **(c) goes to S2, not S0/S1 or S4.** Its three checks (the reachability refusal, axe
  `target-size` and input font size) are refusals in `refuseWalkRegressions` from S1 on. They need
  no PWA code, so `pwa_check.sh` can call them the moment it exists, and S2 is where it starts to
  exist. S0/S1 cannot wire them because there is no script yet. Waiting until S4 would leave the
  closing instrument blind to the mobile half for two slices.

  **(d) is wired by whichever of S2 and S3 lands SECOND**, because it needs both the script (S2)
  and the test (S3). S3 has no dependency and may land first.

  | clause | sabotage |
  |---|---|
  | (a) | drop the manifest link from `pwaHead()` |
  | (b) name | hardcode the manifest `name` |
  | (b) icon | ignore `-app-icon-variant`, so both boots serve variant A |
  | (b) screenshots | serve one screenshot row with one byte appended (bytes ≠ the committed, derivation-checked file) |
  | (c) reachability | drop `setTouchEmulationEnabled` from the touch rungs; the coarse-pointer control must refuse |
  | (c) target size | shrink the scope page's `.view-tab`s to 12×12 px with no gap (the adjacent-12-px shape the S0 control measured RED; a LONE small target passes 2.5.8's spacing exception, so it is not a sabotage) |
  | (c) input font | revert the 16 px input rule |
  | (d) | restore the empty `Cache-Control` default |
  | (e) | `pwa.js` writes a second key (a timestamp) beside the dismissal flag |

  ⚠ **This is a CHECK THAT EXITS, NOT "the uiaudit CI row is green".** The `uiaudit` job is
  `continue-on-error: true` (`.github/workflows/ci.yml:1660`), so a red row blocks nothing. The
  touch checks become blocking in S6b, as their own job, after chromium is pinned (O11).
  `pwa_check.sh` stays in the non-blocking job.

  ⚠ **What the closing condition does NOT require: the iPhone checklist.** It is a human judgement
  on a device, so it cannot be a mechanical check. It gates the CLIENT instance's install
  announcement (O5); S5's on-device verification (O9) is part of it. The closing condition covers
  S5 structurally (S5's test plan); its device behaviour is covered only by the checklist. That is
  stated, not hidden.

### Rollout, and what THIS round builds

**This round (O10):** merge this plan (PR #203), then build **S0** (uiaudit touch measurements and
real touch emulation, report-only) and **S1** (the mobile-first CSS, which turns S0's checks into
refusals). Nothing installable ships this round. **S2–S5 come in later rounds**, in slice order. S3
(`no-store`) has no dependency and may land any time **before S4** — S4's pinned `sabotaged=9`
counts S3's clause (d), so S3 must have landed by then (it is the one ordering rule among S2–S4
beyond S4 needing S2's manifest). S6a and S6b may land any time after S1.

Per instance, once S2–S5 are built (NOT part of the closing condition):

- **The personal instance may ship as soon as it is built (O5).** Deploy it with its `-app-name`
  and `-app-icon-variant`.
- **The client instance may be deployed armed, but users are told it can be installed ONLY after
  the operator has run the iPhone checklist against it and recorded a PASS (O5).** The closing
  evidence for that gate is the filled-in record, judged by the operator.
- **Q7 (the cost of `no-store` on Back) is measured on the personal instance as soon as S3 deploys,
  in a Safari tab, with no install needed.** It does not wait for the checklist. If the cost is
  unacceptable, decision 8 is reverted; reverting it is a one-line header change with no data to
  migrate.

### The iPhone install checklist (Q1 → O5) — the artifact

Run it on a real iPhone on the current iOS (26 at the time of writing), against the CLIENT instance
deployed with S2–S5 armed. Start signed out, with no cairn icon on the Home Screen. Each step says
what PASS looks like. Any step that is not a PASS stops the announcement. Keep the filled record
beside the deploy notes, not in this public repository if it names the deployment.

| # | step | PASS looks like | covers |
|---|---|---|---|
| 1 | In Safari, open the instance's root URL. | A 303 to the sign-in page. Tapping the credential field does NOT zoom the page. | #202, S1 (16 px inputs) |
| 2 | Sign in, in Safari, with GitHub. | Lands on the root page, signed in. | baseline before installing |
| 3 | On the root page, find the install hint and tap dismiss. Reload. | The hint shows once and is gone after the reload (O8's `localStorage` key). | decision 11, O8 |
| 4 | Share → Add to Home Screen. | The proposed name is the instance's `short_name`/`name`. The icon is this instance's VARIANT. If the personal instance is also installed, the two icons are visibly different. | O2, O6 |
| 5 | Launch from the Home Screen. | Standalone: no Safari address bar or toolbar. The header sits fully below the status bar, and nothing is hidden under a notch or the home indicator. The app shows the SIGN-IN page: an installed app does not share Safari's cookies (R6). | display; no `viewport-fit=cover` |
| 6 | Tap "Sign in with GitHub". | GitHub opens in an in-app sheet, not the Safari app. After signing in, the sheet CLOSES and the app window shows a signed-in page ("signed in as …"). | **R6, the OAuth hand-back** |
| 7 | Force-quit the app, then relaunch it. | Still signed in. | the app's own cookie jar |
| 8 | Navigate root → scope → entry. Use the in-app Back control, then Reload. | Back returns to the scope page and Reload re-fetches. Both controls appear ONLY in the installed app, never in Safari. | S5 (O9) |
| 9 | From Messages, open an invitation link (`/join?token=…`, a TEST invitation minted for this run). | Record WHERE it opens. Expected: Safari, not the installed app, because iOS does not route links into Home Screen apps (R8). Accept it there: the GitHub flow completes and lands on the scope. | `/join` over the same flight |
| 10 | In the installed app, sign out, then swipe back from the edge. | The sign-in page. Swiping back does NOT show the previous private page's content. | decision 8, T5 |
| 11 | Turn on Airplane mode and navigate. | The browser's own offline error, with no cairn content (O13). Turning the network back on and navigating recovers. | O13 |

Record template (copy it OUTSIDE the repository): device model · iOS version · instance · cairn
revision from the startup line · result per step (PASS / FAIL plus one line) · operator initials.

**If step 6 or 9 fails:** the credential form still works (R6), so the instance stays usable. The
announcement waits, and a `window.open` fallback becomes a separate, evidenced change.

**Personal-instance spot checks (not a gate; run when it ships):**
- The bell: on a session page you own, `Ring` is easy to hit (≥ 44 px) and the page returns to
  itself. Presence runs only on the personal instance.
- The same steps 4–8 under the personal variant.

## STEP 1 — What's current: the research pass (October 2026)

The research covered primary sources first: web.dev, MDN, the W3C Manifest and Service Workers
material, Chrome/Edge docs, WebKit and Apple, Firefox release notes, the Chrome DevTools Protocol
and the WCAG 2.2 Understanding docs.

Tags:
- **[V]** — the page was fetched and read.
- **[S]** — a search snippet or secondary source only; treat it as a hypothesis.
- **[M]** — measured here with chromium 154 (see "Measured current mobile state").

Each finding names the engine it covers.

### R1. Install criteria per engine

- **Chromium (Chrome, Edge; desktop and Android).**
  - Requires HTTPS and a manifest with `name` or `short_name`, 192 px and 512 px icons,
    `start_url`, and a `display` of `fullscreen`, `standalone`, `minimal-ui` or
    `window-controls-overlay`. `prefer_related_applications` must not be true.
  - The automatic promotion also needs user engagement: one click and 30 s on the page
    [web.dev/articles/install-criteria, updated 2024-09-19] [V]. That page does not mention a
    service worker at all.
  - Edge: [learn.microsoft.com/…/progressive-web-apps/how-to/] [V].
  - **A service worker with a fetch handler is NO LONGER required for install.** It was dropped in
    Chrome 108 on mobile and 112 on desktop [developer.chrome.com/blog/update-install-criteria]
    [V].
  - That same post (2023) adds that Chrome's OWN ambient prompt algorithm still looked for a
    `fetch()` handler at the time, while "developers can still use `beforeInstallPrompt()` to
    control the prompt" [V]. Whether the ambient prompt still needs a handler today is UNVERIFIED.
    The design does not depend on it (decision 11).
  - **[M]** `Page.getInstallabilityErrors` returned `[]` for a valid manifest with NO service
    worker.
- **Safari, iOS/iPadOS 26.** "By default, every website added to the Home Screen opens as a web
  app", and the user can untick "Open as Web App"
  [webkit.org/blog/16993/news-from-wwdc25-…-safari-26-beta/] [V]. Before 26, a site needed
  `display: standalone` or the `apple-mobile-web-app-capable` meta. Since 16.4, Add to Home Screen
  also works from third-party iOS browsers [MDN, Making PWAs installable] [V].
- **Safari, macOS (Sonoma / Safari 17+).** File → Add to Dock works with or without a manifest.
  Cookies are copied from Safari once, when the app is added, and the two stores are separate
  after that [developer.apple.com/videos/play/wwdc2023/10120/] [V].
- **Firefox desktop.** Version 143 added web apps pinned to the taskbar: **Windows only**,
  disabled on Linux and macOS [Firefox 143 release notes; Mozilla taskbar-tabs docs] [V].
  Whether it reads the manifest is **unconfirmed** [MDN, possibly stale].
- **Firefox Android.** Home-screen shortcuts, not WebAPKs [MDN] [V]. Its display-mode behaviour
  today is **uncertain** [S, a 2017 source].

### R2. Manifest members

**Required for Chromium install:**
- `name` or `short_name`;
- `icons`, at 192 and 512;
- `start_url`;
- `display` or `display_override`.

Sources: [web.dev/articles/add-manifest] [V]; MDN's manifest reference [V].

**Strongly recommended:**
- **`id`.** Without it, Chrome derives the id from `start_url`, and changing `start_url` later
  makes the app a new one. The id is origin-bound
  [developer.chrome.com/docs/capabilities/pwa-manifest-id] [V]. The two instances live on
  different origins, so they are **distinct apps by construction**. Telling them apart takes only
  a different name and icon.
- **`scope`.** Decides whether a navigation is in-scope or out-of-scope, which R6 turns on.
- **`icons`.** Purposes are `any` (the default), `maskable` and `monochrome` [MDN icons] [V]. Ship
  the maskable icon as a SEPARATE file [S, common guidance]: its safe-zone padding looks wrong
  when shown as `any`.
- **`description`.** At most 300 characters [V].
- **`screenshots`.**
  - Chromium's richer install dialog needs "at least one screenshot for the corresponding form
    factor" [developer.chrome.com/blog/richer-pwa-installation] [V].
  - `form_factor` is `narrow` or `wide`; leave it out and the screenshot applies to all;
    "distribution platforms may choose how many screenshots to display"
    [MDN, Manifest/Reference/screenshots] [V].
  - *Revision 3 removed the specific size and count limits revision 1 gave: neither cited page
    carries them.*
- **`shortcuts`.**
  - `name` and `url` are required, and each `url` must be in `scope`.
  - Chrome Android shows **3**; Windows shows 10. Icons are optional, with 192 px recommended.
  - **iOS ignores shortcuts.**
  - Browsers re-read a manifest about once a day at most.

  Sources: [web.dev/articles/app-shortcuts; MDN shortcuts; firt.dev/notes/pwa-ios] [V].
- **`theme_color` / `background_color`.**
  - `background_color` paints the Android splash screen.
  - The manifest has no shipped dark-mode colour member [S, proposal status unverified].
  - Per-scheme colour is set in-page with `<meta name="theme-color" media=…>` [MDN theme-color]
    [V].

**What iOS reads** [firt.dev/notes/pwa-ios] [V; written before iOS 17, so re-check]:
- Honoured: `name`, `short_name`, `display`, `start_url`, `scope`, `icons` (15.4),
  `theme_color` (15.0), `id` (16.4).
- **Ignored:** `background_color`, `shortcuts`, `screenshots`, `orientation`.
- **`<link rel="apple-touch-icon">` OVERRIDES the manifest icons.**

### R3. Service workers — background for FUTURE work only (O13 dropped the worker for v1)

Kept so a later round that wants an offline page starts from evidence rather than re-deriving it:

- **Updates.** An update is detected when the worker script's bytes change; never rename the
  script [web.dev/articles/service-worker-lifecycle] [V].
- **Activation.** Use a prompt-to-reload pattern, never a silent `skipWaiting()`
  [developer.chrome.com/docs/workbox/handling-service-worker-updates] [V].
- **Caches.** Version them, and clean up in `activate` [V].
- **Navigation preload** [web.dev/blog/navigation-preload] [V].
- **Scope.** A script at `/sw.js` covers `/`; a wider scope needs `Service-Worker-Allowed`
  [MDN register] [V].
- 🔴 **One measured fact matters to any future worker.** The edge in front of the deployment
  LENGTHENED a `max-age=300` to 14400 (`internal/ui/stylesheet.go:40-60`). The worker script needs
  an unversioned URL, so a future worker would need an edge bypass rule and a deployed-digest
  probe. That is half of why the worker was not worth it for v1.

### R4. Not storing authenticated pages on the device

- **With no service worker there is no Cache API storage at all.** The only device-side store
  left is the browser's own HTTP cache and back/forward cache. That is why decision 8 sends
  `no-store`.
- **`Cache-Control: no-store` and bfcache (Chromium)**
  [developer.chrome.com/docs/web-platform/bfcache-ccns] [V]:
  - Chrome admits `no-store` pages to the back/forward cache. Experiments ran from Chrome 116;
    "the final rollout to 100%" was scheduled for March–April 2025, and full completion was not
    re-verified here.
  - Such a page is evicted "on changes to cookies, or other authorization methods". It is also
    evicted when it uses WebSocket, WebTransport or WebRTC, or when a fetch it makes returns
    `no-store`.
  - **The bfcache timeout for `no-store` pages is 3 minutes**, against 10 minutes for other pages.
  - So a sign-out (which changes the cookie) evicts the page, and any `no-store` page leaves
    bfcache after 3 minutes regardless.
- **`Clear-Site-Data`** [MDN] [V] is NOT used (audit D3, accepted).
  - `"cookies"` reaches sibling hosts under the registrable domain. If the instances share one,
    signing out of one would sign the user out of the other.
  - `"storage"` would erase O8's dismissal flag.
  - `"cache"` adds nothing over `no-store` plus Chromium's cookie-change eviction.

### R5. Install prompts

- **`beforeinstallprompt`** is not Baseline; it lives in WICG Manifest Incubations and is
  Chromium-only [MDN beforeinstallprompt] [V; Chromium-only is [S], consistent with MDN].
- **Does it fire without a service worker?**
  - **YES [M]: fired=1** on chromium 154 headless, for a page with a valid manifest and no worker
    registered.
  - **The control: fired=0** for the same page with no manifest.
  - **The scope of that claim:** the run used `--bypass-app-banner-engagement-checks`, because
    headless chromium supplies no real engagement. So it proves the event needs no worker; it does
    NOT exercise the engagement heuristic.
  - **Corroborated by:** web.dev's criteria list, which names no worker [V], and Chrome's own
    statement that developers can still use `beforeinstallprompt` once the fetch-handler
    requirement was dropped [V, 2023].
- **iOS** has no programmatic prompt. Install is Share → Add to Home Screen, with an "Open as Web
  App" toggle since iOS 26 [V].
- **Successors, not shipped:**
  - `navigator.install()`: an Edge origin trial in 143–148 [V];
  - an `<install>` element: a Chrome/Edge origin trial in 148–153, which requires a manifest `id`
    [V].

  Neither is in v1. Setting `id` keeps the door open.

### R6. 🔴 OAuth sign-in from an installed standalone app

**Verdict for cairn's flow:**

| Platform | Expected result | Confidence |
|---|---|---|
| Desktop Chrome/Edge installed app | works | high [V] |
| Android Chrome WebAPK | works | high; cookies are shared with the Chrome profile [web.dev/articles/webapks] [V] |
| iOS Home Screen web app | **expected to work** | **medium** |

On iOS this must be tested on a real device: checklist step 6.

The flow:
1. A top-level form POST to `/sign-in/github`.
2. A 303 to the provider (`internal/ui/oauth.go:583`).
3. GitHub, then the provider.
4. A 303 to `/sign-in/github/callback`. The callback needs the `__Host-cairn-oauth` flight cookie
   (`oauth.go:425-455`) and consumes the flight at `:653`.

**What the sources say about iOS:**
- **Out-of-scope pages open in an in-app sheet.** Out-of-scope links "will open in Safari View
  Controller"; OAuth on a third-party domain "will still open in your web app … through
  heuristics" [WWDC23 10120] [V].
- **The hand-back carries the app's storage.** When the external flow redirects to an in-scope URL,
  "the PWA closes the browser and loads the content in the standalone window", and the in-app
  browser shares storage with the opener [firt.dev/ios-12.2] [V, an old iOS version]. web.dev
  says the same [web.dev/learn/pwa/windows] [V].
- **The installed app does NOT share cookies with Safari.** Only macOS copies them, once
  [WWDC23] [V].

**What is NOT known:**
- No source covers iOS 17/18/26 for a **server-side** redirect flow.
- Two third-party reports of OAuth escaping to Safari came from client-side PKCE flows that were
  untested on a device [V, anecdotal; links omitted].
- Whether `window.open` works as Apple's fallback is disputed between web.dev (2022) and WWDC23
  [V both].

**Why cairn is in the favourable case:**
- The whole flow is top-level, same-window redirects.
- The PKCE verifier stays server-side (`oauth.go:114-125`).
- The flight cookie is `SameSite=Lax` (`oauth.go:425-455`).
- The callback is inside `scope` `/`.

**The residual failure mode is safe.** A callback that runs in a cookie jar without the flight
cookie finds no flight and renders the sign-in page with `oauthIncomplete` at **400**
(`oauth.go:653-660`; it was `:639-640` at `a20ebab`). It fails closed: no session is minted in the
wrong jar. The credential form always renders (`render.go:1694`), so a working way in remains.
`/join?token=…` rides the same flight (`render.go:2408`).

### R7. Mobile layout

- **Safe areas.** With the default `viewport-fit`, "content is automatically inset within the
  display's safe area"; `viewport-fit=cover` extends the page edge to edge, and then
  `env(safe-area-inset-*)` is how content avoids the housing [webkit.org/blog/7929, "Designing
  Websites for iPhone X"] [V; 2017, Safari in a browser tab]. Whether an iOS 26 *standalone*
  window insets the same way is UNSOURCED. Checklist step 5 checks it. *Revision 3 corrects
  revision 1, which cited MDN `env()` for this; that page does not say it.*
- **Target size.**
  - WCAG **2.5.8 (AA)**: 24×24 CSS px, except Spacing, Equivalent, Inline, User agent control
    and Essential [w3.org/WAI/WCAG22/Understanding/target-size-minimum] [V].
    *Audit round 1 said the list has no "Essential". Re-read against the W3C page, the normative
    text lists five exceptions, the fifth being Essential, so the list is kept as written.*
  - **2.5.5 (AAA)**: 44×44 [V]. Apple HIG says 44 pt; Material says 48 dp [S].
- **Viewport units.** `svh`/`lvh`/`dvh` are supported in Chrome 108, Firefox 101 and Safari 15.4
  [web.dev/blog/viewport-units] [V].
- **Overscroll.** `overscroll-behavior` is not Baseline per MDN [V].
- **iOS input zoom.** iOS zooms into a focused input whose font is below 16 px. The fix is
  `font-size: max(16px, 1em)` [S, css-tricks]. WebKit-specific.
- **Pointer media queries under emulation.** See R10.

### R8. Standalone-mode quirks

- **No back button in iOS standalone.** An edge swipe exists; Android has a system back [S]. The
  app needs its own navigation.
- **No pull-to-refresh in iOS standalone** [S].
- **Out-of-scope links leave the window.** iOS opens them in Safari View Controller; Mac Safari
  web apps use the default browser; Chromium uses an in-app browser [V].
- **Links from other apps open in Safari, not the installed app.** Apple suggests offering
  one-time codes instead of emailed sign-in links [WWDC23] [V].
- `@media (display-mode: standalone)` is widely available [MDN] [V].

### R9. Future work (one line each, OUT of scope)

- **Web Push:** iOS 16.4+, Home Screen apps only, permission from a user gesture
  [webkit.org/blog/13878] [V].
- **Web Share Target:** experimental, effectively Chromium on Android only [MDN] [V/S].
- **An offline page via a service worker:** see R3, and O13 for why not in v1.

### R10. 🔴 Making `(pointer: coarse)` match in headless chromium (audit round 1, item 1)

**The finding:** uiaudit's touch rungs call only `Emulation.setDeviceMetricsOverride(…, mobile)`
(`uiaudit/browser.go:619`). Per the protocol definition, that `mobile` flag covers the "viewport
meta tag, overlay scrollbars, text autosizing and more", not touch input. The protocol definition is
carried in the generated client `uiaudit` pins: `github.com/chromedp/cdproto`, `emulation.go`, the
`Mobile` field.

`Emulation.setTouchEmulationEnabled` "enables touch on platforms which do not support them", with a
`maxTouchPoints` parameter [same source, [V]; CDP reference
chromedevtools.github.io/devtools-protocol/tot/Emulation]. Puppeteer's `hasTouch` viewport option is
implemented with this same call [S, Puppeteer `EmulationManager`].

**[M], chromium 154 headless, one tab across steps:**

| emulation | `pointer:coarse` | `any-pointer:coarse` | `hover:none` | `maxTouchPoints` |
|---|---|---|---|---|
| laptop, no emulation | false | false | **true** | 0 |
| `setDeviceMetricsOverride(mobile=true)` only — **uiaudit today** | **false** | false | true | 0 |
| plus `setEmitTouchEventsForMouse(true)` | **false** | false | true | 0 |
| plus `setTouchEmulationEnabled(true, maxTouchPoints=5)` | **TRUE** | true | true | 5 |
| back to laptop, touch emulation explicitly DISABLED | false | false | true | 0 |
| back to laptop, touch emulation NOT disabled | **true** (stale) | true | true | 5 |

**Three consequences:**
1. **`setTouchEmulationEnabled` is the call that makes `(pointer: coarse)` match.**
   `setEmitTouchEventsForMouse` does not. The audit's measurement (all 265 captures read
   coarse=false) is reproduced: the walk today is blind to any `pointer: coarse` rule.
2. **Touch state PERSISTS across navigations in one tab.** S0 must explicitly DISABLE it at every
   non-touch rung. Otherwise the laptop capture is measured coarse, and the reachability control's
   "false at laptop" half is what catches that.
3. **Headless reports `hover: none` at EVERY width**, and `pointer: fine` false. Every existing
   `hover:` rule in `tailwind.css` is therefore invisible to the walk at all widths. That is
   pre-existing and is recorded as a blind spot. The plan's rules key on `pointer: coarse`, which
   CAN be driven, never on `hover`.

**Fallback, if emulation ever stops matching** (a chromium change; the reachability control would
go red): decision 14's rules move from `@media (pointer: coarse)` to a width rule, `@media
(width < 64rem)` (Tailwind's `lg`). Every phone and portrait tablet would then get the touch sizing,
and a narrow desktop window would too. The 16 px input rule moves with them: since S1 it lives in
the same pointer block (decision 14, amended).
**The reachability control changes with it:** the check stops being `pointer: coarse` matches at
touch captures, and becomes "the width rule's query matches at `mobile` and `tablet` (390 and 834
px are both under 64rem = 1024 px) and does not match at `laptop`, `desktop` and `ultrawide`". The
(c) reachability sabotage becomes "lower the width rule's breakpoint to 40rem (640 px)", which must
make the `tablet` captures refuse.

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
- https://developer.mozilla.org/en-US/docs/Web/Progressive_web_apps/Manifest/Reference/screenshots
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
- https://developer.mozilla.org/en-US/docs/Web/API/Window/beforeinstallprompt_event
- https://blogs.windows.com/msedgedev/2025/11/24/the-web-install-api-is-ready-for-testing/
- https://developer.chrome.com/blog/install-element-ot
- https://www.w3.org/WAI/WCAG22/Understanding/target-size-minimum.html
- https://www.w3.org/WAI/WCAG22/Understanding/target-size-enhanced.html
- https://webkit.org/blog/7929/designing-websites-for-iphone-x/
- https://web.dev/blog/viewport-units
- https://developer.mozilla.org/en-US/docs/Web/CSS/Reference/Properties/overscroll-behavior
- https://developer.mozilla.org/en-US/docs/Web/CSS/Reference/At-rules/@media/display-mode
- https://chromedevtools.github.io/devtools-protocol/tot/Emulation (setTouchEmulationEnabled,
  setEmitTouchEventsForMouse, setDeviceMetricsOverride)
- https://webkit.org/blog/13878/web-push-for-web-apps-on-ios-and-ipados/
- https://developer.mozilla.org/en-US/docs/Web/Progressive_web_apps/Manifest/Reference/share_target

## STEP 2 — What exists today, read off the code (`0d3a1fa`)

### Rendering and the page frames

- **There are THREE frames.** `shell()` is the authenticated frame
  (`internal/ui/render.go:584-664`). `SignInPage` (`:1694`) and `JoinPage` (`:2408`) build their own
  `c.HTML5` frames on purpose, because a public page must not offer authenticated navigation
  (`TestNoPublicPageOffersAuthenticatedNavigation`, `navaffordance_test.go:207`). Any head element
  this plan adds therefore has three call sites, so **one `pwaHead()` helper serves all three**
  (decision 4).
- **gomponents emits the viewport meta unconditionally**: `width=device-width, initial-scale=1`.
  The comment at `render.go:588-598` says a second tag would duplicate it. So
  `viewport-fit=cover` cannot be added without replacing `c.HTML5`'s head, and this plan does not
  need it (R7).
- **The header** is the wordmark, three unconditional nav links (`Arcs`, `Sharing`,
  `Invitations`), the viewer line and a sign-out form (`render.go:601-659`). Breadcrumbs are part
  of the frame (`:660`; `breadcrumbs` at `:666-682`), so every authenticated page already has a way
  "up" to the root.
- **Search** is a server-side `GET` form on `/`. The tag filter rides in a hidden input
  (`render.go:684-720`).

### Stylesheet, colour scheme and breakpoints

- Tailwind v4 builds `internal/ui/tailwind.css` into the committed `internal/ui/app.css`.
  `checks.ui-stylesheet-is-current` regenerates and diffs it (`flake.nix:1170-1195`), and the file
  is served at a content-hashed, `immutable` path.
- **The palette is DARK, ALWAYS.** There is no `prefers-color-scheme`, by operator decision;
  `TestTheGeneratedStylesheetHasNoColourSchemePreference` (`internal/ui/browse_test.go:540`) asserts
  the absence (`tailwind.css:41-55`), and `color-scheme: dark` is set at `:182`. So there is ONE
  `theme_color` and ONE `theme-color` meta, with no `media` variants.
- The surface token `oklch(0.21 0.008 75)` (`tailwind.css:60`) converts by hand to about
  **`#1a1814`**.
- **Breakpoints:** `sm` 40rem, `lg` 64rem, `xl` 80rem, plus `--breakpoint-ultra: 125rem`
  (`tailwind.css:109`).
- **The only pointer-aware rules are `hover:` variants.** There is no `(pointer: coarse)` rule
  anywhere.
- **Inputs are `text-sm` (14 px)** (`tailwind.css:438-443`), which zooms the page on iOS.

### Script policy

- **`AllowedScriptSources()` is exactly ONE entry**: `FilterScriptPath`, the content-hashed
  `filter.js` (`internal/ui/script.go:52, 66-68`).
- Three guards hold the list:
  - the renderer guard;
  - uiaudit's `refuseWalkRegressions`, which reads `document.scripts`;
  - `TestTheFilterScriptTouchesOnlyWhatItSays`, a spelling guard.
- This plan adds ONE more `<script>`, `pwa.js`, in S4 (decision 5). There is no service worker
  (O13), so no non-`<script>` script exists to need its own ledger.

### No CSP

`writeHTML` sends no `Content-Security-Policy`, by operator decision. `server.go:1659-1746` records
the accepted blast radius, and `TestTheHTMLResponseSendsNoContentSecurityPolicy` pins the absence
(`routes_test.go:1047-1110`).

Installability needs no CSP: no `manifest-src` is required. Nothing here reopens the decision.

### Caching of HTML today

- **Every HTML page goes out with NO `Cache-Control`** (`writeHTMLCached`, `server.go:1777-1792`).
- The ONE exception is the invitation mint response, sent `no-store` (`writeHTMLNoStore`,
  `:1751-1775`).
- That comment calls a surface-wide value "a caching decision about the whole surface", and this
  plan takes that decision (decision 8).

### Routes, and the gates a new row passes through

- **The route table** is an exact-match map (`routes.go:117-275`).
  - Public rows are dispatched BEFORE authentication (`server.go:1249-1253`).
  - Today's public rows: the sign-in pair, the GitHub pair, `/join`, both stylesheet rows and the
    filter script.
- **The hand ledger** is `TestTheRouteLedgerMatchesTheDispatchTable` (`routes_test.go:55`).
  `bareGETAnswer` is at `:415` and `contentAuthority` at `:734`.
- **The unauthenticated answer (#202, merged as `0d3a1fa`).** A **`GET`** that asks for HTML and
  presents no `Authorization` header gets a 303 to `signInLocation(r)` (`server.go:1255-1281`;
  `redirectsToSignIn`, `returnto.go:153-166`).
  - **GET only, NOT HEAD.** Admitting `HEAD` was measured to loop (`returnto.go:153-161`).
    Revision 1 said "GET/HEAD"; that was wrong.
  - `signInLocation` returns a BARE `/sign-in` when the return path is `/` or is refused
    (`returnto.go:130-136`). Otherwise it returns `/sign-in?next=<escaped request-URI>`.
  - **A fragment never reaches the server**, so `/#q` cannot carry a return path. That is why the
    Search shortcut became query-based (decision 10).
  - `Accept: */*` keeps the 401.
- **The manifest, icons and screenshots MUST be public rows.** Chromium fetches the manifest
  without credentials unless the link says `crossorigin="use-credentials"` [MDN manifest] [S].
  None of these rows may consult an authority.
- **`/favicon.ico` is not a row.** It answers 401, and uiaudit carries a carve-out for it
  (`uiaudit/browser.go:223-242`).

### Embedding and the nix build

`onlyGo` admits embedded non-Go files BY NAME: `app.css` and `filter.js` (`flake.nix:351-443`,
`:429-433`). Every new embedded file must be added there or the nix build stops compiling: the icon
PNGs and `variants.json`, the screenshot PNGs, and `pwa.js`.

### Instance identity

- **`cairn-ui` has no notion of which instance it is.** The flags at `cmd/cairn-ui/main.go:213-290`
  include no name, title or colour.
- **The convention for a per-deployment opt-in** is a flag with NO default, off when unset, and
  refused when blank. Examples:
  - `-arc-journal` (`main.go:263-266`);
  - the presence flags (`main.go:270-283`);
  - the blank-value remedy, `controlJournalDefault` (`main.go:912`).

### The uiaudit world, and what it cannot reach

- `uiaudit/boot.go:153-158` deliberately leaves `-control-journal` unset. Its recorded reason: a
  walk that invented a journal "would render a page no deployment serves". That is the TOKEN-FILE
  deployment.
- **But both deployed instances run with a control journal.** The arcs/presence plan measured
  both journals (`claudedocs/plan-cairn-arcs-presence.md`, decision 11). So the journal world is the
  DEPLOYED shape, and the token-file world is a supported one.
- **The invite MINT form additionally needs `-db-dsn` (PostgreSQL)**: without it, `Inviting` is nil
  and the page renders `NoInviteStore` (`main.go:248-252, 438`).
- CI has one PostgreSQL precedent: the `pgtest` job's `services: postgres` (`ci.yml:1952-1966`),
  whose image is pinned in agreement with `tests/test_pgtest_tier_is_declared.py`.

### Governed ledgers that a new row or file moves

| ledger | where | moves when |
|---|---|---|
| route table + hand ledger + `bareGETAnswer` | `routes.go`, `routes_test.go:55, 415` | any new row (S2, S4) |
| `TestEveryServedPathComesFromTheLedger` near-miss probes | `routes_test.go:489` | a computed (hashed) key |
| `AllowedScriptSources` | `script.go:66` | `pwa.js` (S4) |
| `onlyGo` | `flake.nix:351-443` | any new `//go:embed` |
| nix regenerate-and-diff checks | `flake.nix`, beside `checks.ui-stylesheet-is-current` (`:1170`) | icons per variant (S2); screenshots (S4) |
| the uiaudit job's `continue-on-error` and its chromium install | `ci.yml:1660`, `:1745-1751` (apt/snap, unpinned) | S6a pins chromium; S6b adds a SEPARATE blocking job, the promotion path `ci.yml:1655-1659` prescribes |
| `tests/control_mutants.py` rows, pinned by `tests/test_control_mutant_count_is_pinned.py` into `ci.yml:848, 865, 930` and `internal/control/README.md` | `PKGS` includes `./internal/ui/` (`control_mutants.py:115`) | any Go-side guard. **287** rows at `0d3a1fa` (`ci.yml:865`'s step name) |
| uiaudit test floors | `ci.yml:1817` (29 top-level), `:1821` (64 total) | a new uiaudit test |
| `internal/ui/README.md` phases; `uiaudit/README.md` | — | every slice |
| `depspolicy` allowlist | `internal/depspolicy` | **nothing**: no new Go module (asserted) |

### uiaudit — what it measures, and whether it can gate "mobile-first"

- **It walks** every GET row and every published link, at five widths
  (`uiaudit/targets.go:71-87`). `mobile` (390×844) and `tablet` (834×1112) are flagged
  `Touch: true`.
- **It refuses on** (`refuseWalkRegressions`, `uiaudit/main.go:495-575`):
  - horizontal overflow;
  - a script outside the allowlist;
  - a capture with no axe engine;
  - the ultrawide content floor;
  - a collapsed width matrix.
- **It only reports** `layout-smells.js`'s counts and axe violations.
- 🔴 **axe's `target-size` rule (2.5.8) never runs.** It is disabled in the vendored axe 4.12.1,
  and `axeRunJS` does not enable it (`uiaudit/browser.go:884`).
- 🔴 **`Touch: true` does NOT emulate touch.** It only sets the `mobile` metrics flag
  (`browser.go:619`), so `(pointer: coarse)` never matches (R10, measured).
- ⚠ **New measurements belong in `browser.go`, not `layout-smells.js`.** That file is a canonical
  copy other harnesses share, and its keys are a push contract (`vendor-js/layout-smells.js:1-3`,
  `uiaudit/embed.go:14`).
- 🔴 **The job is `continue-on-error: true`** (`ci.yml:1660`).
- 🔴 **It cannot emulate standalone display mode** (measured below).

## Measured current mobile state

### Instruments

- `uiaudit/run.sh` on `a20ebab`, using chromium **154.0.8037.92** from `nix-shell -p chromium`.
  That is NOT CI's chromium.
- The token-file synthetic world, with no push.
- 265 captures (53 targets × 5 widths), `rc=0`.
- A second walk from a SCRATCH COPY of the tree, with axe `target-size` enabled plus a script
  listing every visible `a, button, input, select, summary, [role=button]` with its box, and every
  input's computed `font-size`.
- Standalone probes for each instrument (below).

### What these instruments cannot see

- A real iOS or Android browser, and WebKit at all.
- The share per-scope page and the invite mint form.
- The GitHub button.
- Standalone display mode.
- `pointer: coarse` (R10). **The per-element numbers below are therefore the CURRENT rules' boxes.**
  They are correct as such, because no `pointer: coarse` rule exists yet.

### Controls, run first and reported as pairs

- **axe `target-size`:**
  - two adjacent 12×12 buttons gave `target-size:2` when the rule was enabled, and **nothing under
    uiaudit's current call**;
  - two 48×48 buttons gave 0;
  - two adjacent 14 px-high links gave `target-size:2`.
- **`Page.getInstallabilityErrors`:**
  - no manifest → `[no-manifest]`;
  - a manifest without icons → `[manifest-missing-suitable-icon, no-acceptable-icon]`;
  - a valid manifest, with or without a worker → `[]`.
- **`beforeinstallprompt`** (engagement check bypassed): fired=1 with a manifest and no worker;
  fired=0 without a manifest (R5).
- **`setEmulatedMedia(display-mode: standalone)`** had NO effect. Standalone-only CSS is
  unmeasurable here.
- **Touch emulation:** see R10. `setTouchEmulationEnabled` makes `(pointer: coarse)` match;
  uiaudit's current call does not.

### Surface-wide results (mobile, 390 px)

| signal | result | gated today? |
|---|---|---|
| horizontal overflow | **0** of 265 captures | yes |
| `<meta viewport>` missing | 0 | reported |
| text under 12 px | 0 | reported |
| axe violations (default rules) | 0 | reported |
| axe `target-size` (2.5.8 AA), enabled | **0** over 53 mobile captures | **never runs today** |
| interactive elements with a side < 24 px | **322** over 53 mobile captures | — |
| interactive elements with a side < 44 px | 2785 summed over all 265 captures; **identical at mobile, tablet and desktop for every page** | reported |
| inputs with `font-size` < 16 px | **3 of 3**: `#q`, `#entry-filter`, `#token`, all **14 px** | — |

axe passes because 2.5.8's spacing and inline exceptions apply, so the AA floor is met. The 44 px
comfort size (Apple HIG, 2.5.5 AAA) is missed nearly everywhere, and nothing adapts to touch.

### Per-page defects at 390 px

- **Header (every authenticated page):**
  - two rows, 80 px tall;
  - nav links `Arcs` 30×16, `Sharing` 54×16, `Invitations` 73×16;
  - `Sign out` 72×26.
- **Root `/`:**
  - card titles about 100–118 × 21;
  - the search input is 358×38 at a 14 px font;
  - tag chips are about 20 px high.
- **Scope page:**
  - each entry row's ONLY link is its mono ref, about 14 px high (up to 24 sub-24 px targets per
    page);
  - tabs are about 26 px high;
  - the filter input is 316×38 at 14 px.
- **Scope arcs/sessions tabs:** up to 38 sub-44 px targets; arc and session links about 14 px
  high; pills about 20 px.
- **`/arcs`:** arc links 14 px high; the toggle is 86×22.
- **`/arc`:** breadcrumbs 24 px; tabs 26 px; pills about 19–20 px.
- **`/session`:** `Ring` is **49×22**; badges and pills about 20 px.
- **`/entry`:** breadcrumbs 24 px, view tabs 26 px. The raw `<pre>` wraps, so there is no inner
  scroll.
- **Share and invite indexes:** only header targets.
- **Sign-in:** the credential field is 200×38 at 14 px. This page and `/join` are the only ones
  with 0 sub-24 px targets.
- **Tables:** there are none on any captured page.

## Decisions — who chose what

### Chosen by the OPERATOR (paraphrased; not re-litigated)

| # | what the operator chose | cost accepted / where it lands |
|---|---|---|
| O1 | Installable but network-only: nothing private ever stored on the device. *(Originally with a shell-caching service worker and an offline page; O13 removed both.)* | No offline reading. |
| O2 | Both instances installable, distinguishable through per-instance configuration. | One deploy-time config line per instance (decision 1). |
| O3 | Extras: app shortcuts, an in-page Install affordance, and an update banner. Web Push and the share target are out. *(O13 dropped the update banner: with no worker there is no staleness for it to fix.)* | One script on every page (decision 5). Shortcuts are invisible on iOS. |
| O4 | Every mobile priority: reading entries and scopes, arcs, sessions and the bell, search with the tag filter, and sharing and admin. | The admin forms need a wider harness world (S0). |
| O5 | The client instance is announced as installable only after the operator has run the iPhone checklist. The personal instance may ship as soon as it is built. | The client announcement waits on a person with a device. |
| O6 | A distinct icon per instance, through a no-default flag that selects a committed, pre-generated variant. No deployment supplies image bytes. | A closed set of committed icon variants (S2). |
| O7 | Install screenshots, generated only from the synthetic uiaudit world by a pinned nix derivation, with regenerate-and-diff, and synthetic data only. | A chromium-in-the-sandbox derivation (S4). |
| O8 | Remember a dismissed iOS install hint in `localStorage`. | The plan's ONE client-side write (decision 11, T9). |
| O9 | The standalone Back/Reload controls (S5) are in v1, verified on a device. | S5 is in the slice list. Its device check is checklist step 8. |
| O10 | This round: merge the plan, then build S0 and S1. Later rounds build the rest. | Nothing installable ships this round. |
| O11 | Pin CI's chromium first, then make the touch checks blocking, in a separate small PR. | S6a and S6b. |
| O13 | **No service worker in v1.** Install relies on the manifest, icons, shortcuts and the Install button. Nothing is stored on the device apart from O8's key, and offline shows the browser's own error. A worker stays possible future work. | No offline page. No update UX (a consequence: nothing goes stale). |

**A FACT, not a choice (moved out of this table by audit D7):** PR #202 is merged on `main` as
`0d3a1fa` and deployed to the personal instance. Every gate this plan had on it is satisfied.

### Chosen by the AGENT writing this plan (open to review)

1. **Instance identity comes from three new `cairn-ui` flags. The first one ARMS the feature.**
   - **`-app-name`** (env `CAIRN_UI_APP_NAME`): **NO default.** Unset means no manifest link and no
     `pwa.js`, and `/manifest.webmanifest` answers 404.
   - **`-app-short-name`** (env `CAIRN_UI_APP_SHORT_NAME`): optional, ≤ 12 characters (longer is
     refused). Omitted from the manifest when unset.
   - **`-app-icon-variant`** (env `CAIRN_UI_APP_ICON_VARIANT`, O6): **NO default**, and
     **REQUIRED whenever `-app-name` is set.** Its value must be a member of the closed set
     `ui.IconVariants()` (decision 3); anything else is refused, naming the set.
   - All three refuse a blank value the `controlJournalDefault` way (`main.go:912`). The startup
     line names the armed app.
   - **The theme-colour flag is GONE (audit D5).** The name plus the icon variant already tell
     the instances apart. `theme_color`/`background_color` are the stylesheet's surface colour,
     derived (recommendation B6), not configured.
   - *Why a flag and not the hostname:* the process cannot know its external origin, and a `Host`
     header is chosen by the proxy.
2. **The manifest is a PUBLIC, server-rendered row at a FIXED path**, `GET /manifest.webmanifest`.
   - Headers: `application/manifest+json`, `Cache-Control: no-cache`, `nosniff`.
   - Built with `encoding/json` over a Go struct, never by string assembly.
   - Members:
     - `id`/`start_url`/`scope` `/`, `display: "standalone"`;
     - `name`, optional `short_name`, a constant `description`;
     - the surface colour;
     - `icons` (decision 3);
     - `shortcuts` (decision 10);
     - `screenshots` (decision 16, from S4 on).
   - No `display_override`.
3. **Icons: one committed SVG template, a CLOSED set of variants, committed PNGs, and a nix
   regenerate-and-diff check** — the `app.css` discipline.
   - The variant list lives in ONE committed file, `internal/ui/icons/variants.json`.
     `ui.IconVariants()` embeds it, and the nix derivation reads it with `builtins.fromJSON`: one
     list, two readers.
   - Variant names are neutral (`amber`, `teal`, …), never an instance's name, so this public repo
     never records which deployment is which (Q9).
   - The files: every variant × {192 any, 512 any, 512 maskable, 180 apple-touch}.
   - Every file is a content-hashed `immutable` public row, so the ledger never depends on
     configuration. Only the SELECTED variant is linked.
   - **Guards:**
     - the embedded set EQUALS `IconVariants() × sizes`, failing if it grows or shrinks;
     - `checks.ui-icons-are-current` regenerates every variant, with a negative control;
     - two variants' 512 px files must differ.
4. **`pwaHead()` is the ONE place PWA head elements are spelled; all three frames call it.**
   - **S2** emits: `<link rel="manifest">`, one `<meta name="theme-color">`, `<link rel="icon">`
     and `<link rel="apple-touch-icon">`.
   - 🔴 **S2 emits NO `<script>`.** `pwa.js` does not exist until S4, and S4 adds its tag to
     `pwaHead()`. Until then the existing allowlist guard (one entry, `filter.js`) is what proves
     it (audit round 1, item 2).
   - Nothing is emitted when unarmed.
   - A test walks the frame LEDGER, so a fourth frame cannot skip it.
5. **One new script, `pwa.js` (S4), the SECOND entry in `AllowedScriptSources`.** It is
   content-hashed and `classPublic`, linked by `pwaHead()`. It:
   - reveals a hidden Install button on `beforeinstallprompt` (decision 11);
   - reveals the iOS hint and remembers its dismissal (decision 11, O8);
   - from S5, reveals Back/Reload in standalone only.

   It registers no service worker. Its spelling guard, `TestThePWAScriptTouchesOnlyWhatItSays`,
   refuses `innerHTML`, `eval`, `fetch`, `document.cookie`, `sessionStorage`, `indexedDB`,
   `caches`, and `navigator.serviceWorker`, which pins O13 in the script itself. It allows
   `localStorage` ONLY as `getItem`/`setItem` of the one `HINT_KEY`. It is a SPELLING guard,
   labelled as one; clause (e) is the STATE guard.
6. **REMOVED (O13)** — the `Service-Worker: script` refusal. With no worker there is nothing to
   protect.
7. **REMOVED (O13)** — the network-only worker and its shell cache.
8. **Every HTML response from a non-public row is `Cache-Control: no-store`** (S3).
   - `writeHTML`'s default becomes `no-store`, and `writeHTMLNoStore` folds into it. Public pages
     (sign-in, join) get `no-cache`.
   - **SUPERSEDED AT BUILD (S3, #209): public pages are `no-store` too.** This was an agent
     decision, and S3 departs from it deliberately: ONE HTML writer and ONE value means no
     authenticated page can be opted down to a weaker header; a deploy is still seen at once under
     `no-store`; the cost is bfcache on the sign-in page; it also settles `/join`'s hidden invitation
     token. The reasoning and the revert (one constant, if Q7 says so) are in `internal/ui/README.md`
     Phase Q. Do not restore a `no-cache` writer to match the sentence above.
   - With no worker, this is the WHOLE device-side storage control for authenticated pages.
   - **What it costs, as RESEARCH, not measurement (audit D3):** Chromium admits `no-store` pages
     to bfcache, keeps them for at most **3 minutes** (against 10), and evicts them on any cookie
     change, so a sign-out always evicts (R4). Safari and Firefox re-fetch on Back. Nobody has
     measured the cost here; Q7 measures it on a phone.
9. **REMOVED (O13)** — worker currency, the deployed-digest probe and the kill switch.
10. **Shortcuts: Arcs → `/arcs`, Search → `/?q=`, Sharing → `/share`.**
    - **SUPERSEDED AT BUILD (UI hub change): Search targets `/scopes?q=`.** The root became a hub and
      the scope list with its search box moved to `/scopes`; `/?q=` still answers, with a 303 to
      `/scopes?q=` (query preserved), so S4 should point the shortcut at `/scopes?q=` directly.
    - Three items, Chrome Android's limit. No shortcut icons.
    - **Search is query-based now (audit round 1, item 3).** `/#q` could not carry a return path:
      a fragment never reaches the server, and `signInLocation` answers a bare `/sign-in` for `/`
      (`returnto.go:130-136`). `/?q=` is a real request-URI that `safeNext` accepts
      (`returnto.go:70-93`: `/`-prefixed printable ASCII, no `//`, no `#`). An empty `q` renders
      the root page with the search box at the top.
    - #202 is on `main`, so an expired-session shortcut lands at the sign-in page and returns to
      the shortcut afterwards. S4 pins the literal `Location` per shortcut.
11. **The Install affordance.**
    - **Chromium:** a hidden `<button class="install">` in the header, revealed only by
      `beforeinstallprompt`. The event was **measured to fire with no service worker** (R5, [M]),
      so O13 needs no change to this design. Chrome's own menu install works regardless.
    - **iOS:** a one-line hint on the ROOT page ("Install: Share → Add to Home Screen") with a
      dismiss button.
      - It shows only when `"standalone" in navigator && navigator.standalone === false`. That is
        feature detection, never user-agent sniffing.
      - Everything stays hidden in `display-mode: standalone` and when script is off.
    - **The dismissal is REMEMBERED (O8), and this is the plan's ONE client-side write:**
      - key `cairn.installHintDismissed`, value `"1"`;
      - written only on a dismiss tap, and read only to decide whether to show the hint;
      - no user data, no identity, no timestamp — a fact about the device's browser;
      - every access in `try/catch`: blocked storage throws, and the hint then simply shows.
    - **It lives** in Safari's storage for this origin (the hint never renders in the installed
      app, whose storage is separate anyway, R6).
    - **It is cleared** when the user clears website data, or when the browser evicts
      script-writable storage. Safari can do that after days without interaction [S]; the hint
      then reappears, which is harmless.
    - **Sign-out does NOT clear it, deliberately.** It says nothing about who was signed in.
      Clearing it would re-show a dismissed hint after every sign-out, and it would need
      `Clear-Site-Data: "storage"` or a second write path. Unarming the feature leaves the key
      orphaned and unread.
12. **REMOVED (O13)** — the update banner.
13. **REMOVED (audit D3)** — `Clear-Site-Data` at sign-out. Sign-out keeps its server-side revoke
    and explicit cookie clear (`session.go:370-382`). `no-store` plus Chromium's cookie-change
    eviction covers the cached-page case.
14. **The mobile-first CSS is POINTER-driven**, under `@media (pointer: coarse)`, and S0 makes the
    gate able to see it (R10).
    - `min-height: 44px` and a matching hit area on: the nav links, `button[type=submit]`,
      `.view-tab`, `.crumb`, the entry-row link (the whole row is the target, B1) and the bell.
    - Inputs at `font-size: max(16px, 1em)` **under the same `(pointer: coarse)` query** —
      *amended at S1 (operator-accepted, PR #205 round 0, R-a); it said "at EVERY width".* The
      zoom depends on the font, not the width, and iOS Safari reports a coarse pointer; keeping
      the rule inside the pointer block is what leaves the fine-pointer (desktop) rendering
      unchanged. **Residual risk, unmeasured:** a WebKit that reports a FINE primary pointer and
      still zooms on focus keeps 14 px, and the iPhone checklist covers iPhone only.
    - The header becomes compact: the wordmark and the nav on one row, the viewer and sign-out
      beneath it — in DOM order, so the reading/focus order matches the visual order (S1 round 1).
    - *As built (S1), the 44 px size is REPORTED, not refused* — an operator decision (R-d): the
      walk refuses axe `target-size` (2.5.8) and input font only.
    - *Fallback if emulation ever stops matching:* the width rule in R10.
15. **uiaudit's touch measurements live in `browser.go`, at the touch rungs:**
    - **Real touch emulation:** `setTouchEmulationEnabled(true, maxTouchPoints=5)` at `Touch`
      rungs, and an EXPLICIT `setTouchEmulationEnabled(false)` at every other rung, because the
      state persists across navigations (R10).
    - **The reachability control:** `matchMedia('(pointer: coarse)').matches` must be true at
      every touch capture and false at every non-touch capture. Any mismatch makes the walk
      REFUSE, naming the capture.
    - axe with `rules: {"target-size": {enabled: true}}`.
    - Every visible input's computed `font-size`.
    - **Gates** (refusals from S1 on): `target-size` violations > 0; any input < 16 px.
    - **The per-page < 44 px ceiling ledger is DROPPED (audit D6).** The < 44 px count stays a
      REPORTED number.
16. **Install screenshots (O7): build output of the synthetic world, landing in S4.**
    - **The generator** is a `uiScreenshots` nix derivation. It builds `cairn-ui` and the uiaudit
      store with `tests/reader_fixtures.py`'s own builder, boots both in the sandbox with no
      network, signs in with the fixture credential, and captures with nixpkgs' chromium:
      `headless=new`, device scale 1, animations off, pinned fontconfig, a FIXED synthetic
      `-app-name`.
    - **Captures:** `narrow` 390×844 of `/` and `/arcs`; `wide` 1440×900 of `/`.
    - **Pinning:** the PNGs are committed, embedded and served at content-hashed public rows.
      `checks.ui-screenshots-are-current` regenerates them and diffs byte for byte, with a
      negative control.
    - **Why S4:** the screenshots depict the UI, so they come after S1's CSS and travel with S4's
      other manifest change.
    - 🔴 **How `leakscan`'s binary blind spot is covered: PROVENANCE, enforced.** `leakscan`
      skips PNGs by name. Instead, the committed bytes must EQUAL what the derivation renders,
      and the derivation's only data input is the synthetic fixture world (text that `leakscan`
      scans), behind a filtered `src`. A hand-made or real-data screenshot cannot equal the
      derivation's output. This is a guarantee about where the content came from, not a scan.
    - ⚠ **Unmeasured:** whether the bytes are identical on two hosts. If S4 measures a difference,
      the check compares decoded pixels at zero tolerance instead, and says so.
17. **S6: pin, then block, as two PRs (O11).**
    - **S6a** takes the uiaudit job's chromium from the flake's pinned nixpkgs instead of apt or
      snap (`ci.yml:1745-1751`).
    - **S6b** adds a NEW job, `uiaudit-touch`, with no `continue-on-error`. It runs the walk's
      touch refusals, from `refuseWalkRegressions`, the one place they live. The advisory job
      keeps its `continue-on-error`, as `ci.yml:1655-1659` prescribes.

## Threat model

| threat | control |
|---|---|
| **T1. Private data stored on the device** | **By construction: no service worker, so no Cache API storage (O13).** The browser's own HTTP cache and bfcache are handled by decision 8 (`no-store`): Chromium keeps such a page ≤ 3 min and evicts it on a cookie change (R4). Guard: clause (d). |
| **T2. REMOVED (O13)** — worker scope hijack. No worker. ⚠ A future worker reopens it, and R3 says what a design must answer. | — |
| **T3. REMOVED (O13)** — an update stranding users on an old worker. With no worker, every navigation fetches fresh HTML. | — |
| **T4. The OAuth redirect leaves standalone** | R6: expected to stay in the app on iOS. A callback without the flight cookie fails CLOSED (`oauthIncomplete` 400, `oauth.go:653-660`), and the credential form remains. The gate is checklist steps 6 and 9 (O5), not code. |
| **T5. Sign-out with an installed app** | `POST /sign-out` revokes server-side and clears the cookie (`session.go:370-382`), so a surviving page cannot act: its CSRF token is derived from a dead session. `no-store` plus Chromium's cookie-change eviction keeps the page out of Back (checklist step 10). The installed icon survives by design; it holds nothing. |
| **T6. The manifest as an information leak** | Public, but it carries only the configured name, the icon variant's paths and constant paths. The flag's help says: put nothing in `-app-name` you would not put on the sign-in page. |
| **T7. Shortcut / start_url dead ends** | Closed by #202 for the expired-session case; S4 pins each shortcut's `Location`. Plain-text 404/500 answers still lack navigation in standalone; B4 covers them. *Corrected at S5: S5's Back is NOT on such a page — it has no header and no script.* |
| **T8. Clickjacking** | Unchanged: the no-CSP decision is not reopened, and installation adds no framing path. |
| **T9. Client-side storage** | Exactly one key, `cairn.installHintDismissed = "1"` (O8): no user data, written only on a tap, `try/catch`-wrapped. Nothing else is written by script. The spelling guard, plus clause (e) as the STATE guard, enforce it. It is deliberately NOT cleared at sign-out (decision 11). An XSS that reads it learns only "this browser dismissed a hint". |
| **T10. A committed binary carrying real data** (`leakscan` skips binaries) | Provenance, enforced by regenerate-and-diff. Icons come from the committed SVG template plus `variants.json`; screenshots come only from the synthetic world with a fixed synthetic name. A real-data image cannot equal the derivation's output. The residual: a non-synthetic string added to the FIXTURE would be rendered, but the fixture is text and `leakscan` scans it. |

## Slices

Each slice is mergeable alone, leaves `main` releasable, and has tests that run on that slice
alone (audit round 1, item 2).

| slice | what | ledgers it moves | mergeable alone because |
|---|---|---|---|
| **S0** *(THIS round)* | **Measure.** REAL touch emulation at the touch rungs, explicitly disabled elsewhere, with the reachability control (decision 15, R10); axe `target-size` enabled; input `font-size`. All REPORTED, because input font is red on `main`. **A journal-backed world BESIDE the token-file world** (Q10). | `uiaudit/browser.go` (emulation, `axeRunJS`, font capture), `main.go` (report lines; the reachability refusal is ON from S0, because it is about the harness, not the page), `uiaudit/boot.go` (a second, journal-backed boot), new `uiaudit/touch_test.go`, the `ci.yml:1817/1821` floors, `uiaudit/README.md` (including a new blind spot: headless is `hover: none` at every width, R10). NOT `layout-smells.js`. | No product change; the job is non-blocking. |
| **S1** *(THIS round)* | **Mobile-first CSS** (decision 14) plus B1–B3, with S0's `target-size` and input-font checks flipped to REFUSALS at the touch rungs. | `tailwind.css` → `app.css`, so the hashed stylesheet path moves; `render.go` only for B1's row link; `internal/ui/README.md`; uiaudit refusal switches. No route, no script. | CSS-only on the product side. |
| **S2** | **Manifest, icon variants, the three flags, `pwaHead()` WITHOUT a script tag, and `uiaudit/pwa_check.sh`** with clauses (a), (b: name + icon) and (c), plus (d) if S3 landed first, and their sabotages (6, or 7). Installable on Chromium from here. | Routes: `GET /manifest.webmanifest` and one hashed public row per icon file, in the hand ledger, `bareGETAnswer` and the near-miss probes. `onlyGo`: PNGs, `variants.json`. `flake.nix`: `uiIcons` + `checks.ui-icons-are-current`. `cmd/cairn-ui` flags and tests. Mutant rows. READMEs. | Inert unless `-app-name` is set. |
| **S3** | **`no-store` alone** (decision 8), plus `TestEveryNonPublicHTMLRowIsNoStore` (clause d), wired into `pwa_check.sh` by whichever of S2/S3 lands second (until then it runs in the `go` job only). | `server.go` (`writeHTML`'s default; `writeHTMLNoStore` folded in), `internal/ui/README.md`, mutant row. | A header change. No dependency, rollback-safe. |
| **S4** | **`pwa.js`** (Install button, the iOS hint and its remembered dismissal), **shortcuts**, **screenshots**, and `pwa_check.sh` clauses (b, screenshots) and (e). | `AllowedScriptSources` (2nd entry); the hashed `pwa.js` row; `pwaHead()` gains the tag; `onlyGo`: `pwa.js` and the screenshot PNGs; the allowlist guard's controls; the spelling guard; manifest `shortcuts` + `screenshots`; screenshot rows; `flake.nix`: `uiScreenshots` + `checks.ui-screenshots-are-current`; mutant rows; `ci.yml` (`pwa_check.sh` step); README. | Additive. Needs S2's manifest, and S3 landed first (its `sabotaged=9` pin counts S3's clause (d)). |
| **S5** *(IN v1, O9)* | **Standalone Back/Reload and polish**: ~~a sticky compact header in `display-mode: standalone`~~ *(dropped by operator decision in the S5 follow-up: S1's touch header measured 101 px tall at 390 px and 834 px — two 44 px rows, ~12% of a phone screen when pinned — and it occluded in-page anchors; the header is compact, two rows, and scrolls away)*; Back/Reload buttons revealed by `pwa.js` in standalone only; `overscroll-behavior-y: contain`. | `tailwind.css` → `app.css`; `pwa.js`, whose spelling guard admits only `history.back` and `location.reload`; README. | Hidden outside standalone. Device behaviour is checklist step 8. |
| **S6a** | **Pin CI's chromium** to the flake's nixpkgs (decision 17). | `ci.yml:1745-1751`; `uiaudit/README.md` gating section. | CI-only. |
| **S6b** | **The blocking `uiaudit-touch` job**, in its own PR (O11). | `ci.yml` (a new job; the advisory job unchanged); `uiaudit/README.md`; branch protection, an operator setting named in the PR. Needs S1 and S6a. | CI-only. |

**Why S3 stays its own slice rather than merging into S2:**
- It is a caching decision about the WHOLE surface, which the code's own comment calls out.
- It applies to every browser whether or not anyone installs anything.
- Its rollback story is "revert one header". Folding it into the manifest slice would make
  reverting either revert both.
- It has no dependency, so it can land first, before the checklist, and Q7 can be measured early.

**Mutant rows** (indicative names; the pinned count starts at **287** on `0d3a1fa`). S0, S1, S5 and
S6 add no Go-side guard.

- **S2:**
  - `ui-manifest-row-requires-auth`
  - `ui-manifest-name-is-a-constant`
  - `ui-manifest-served-when-unarmed`
  - `ui-pwa-head-missing-from-sign-in`
  - `ui-pwa-head-emits-a-script-before-s4`
  - `ui-app-name-blank-accepted`
  - `ui-icon-variant-optional-when-armed`
  - `ui-icon-variant-outside-the-set-accepted`
  - `ui-manifest-links-every-variant`
- **S3:** `ui-html-no-store-dropped`.
- **S4:**
  - `ui-pwa-script-not-allowlisted`
  - `ui-pwa-script-uses-innerhtml`
  - `ui-pwa-script-writes-a-second-storage-key`
  - `ui-pwa-script-registers-a-service-worker`
  - `ui-manifest-screenshot-not-the-committed-file`

Browser-behaviour mutants are not battery rows (`tests/control_mutants.py` runs Go tests only).
They are `pwa_check.sh --self-test`'s sabotages.

### Test plan per slice (negative controls named)

**S0.**
- **Touch reachability.**
  - With emulation on, `pointer: coarse` is true at mobile and tablet, and false at laptop.
  - Two controls:
    - drop the `setTouchEmulationEnabled` call → the walk refuses at the first touch capture;
    - drop the explicit DISABLE → the walk refuses at the first laptop capture, because touch
      state persists.
  - Both were measured on chromium 154 in R10's probe.
- **axe `target-size`.** Two adjacent 12 px buttons give ≥ 1 (positive control); two 48 px
  buttons give 0. With the rule left disabled, the positive control reads 0, which proves the
  enable is what reaches the rule.
- **Font.** A 14 px input is reported; a 16 px input is not.
- **The journal world.** The walk captures `/share?scope=…` with its grant form present, asserted
  by a capture count, so a world that silently fell back to token-file reads as red. The invite
  page is captured in its `NoInviteStore` state (Q10).

**S1.**
- **The report flips.** On `main` the report reads 3 inputs under 16 px; after S1 it reads 0.
  The refusals are then ON.
- **RED at base, green at head**, at the touch rungs and with touch emulation on.
- **The coarse rules apply ONLY where coarse:** at laptop the measured boxes are unchanged from
  base, which checks that the pointer rules did not leak into desktop layout.
- **Width checks:** overflow stays 0 at all five widths, and the ultrawide content floor is
  unchanged.

**S2.**
- **Manifest:** JSON-decoded, every member pinned to a literal. Two names in one process give two
  manifests.
- **Escaping:** a name with `"`, `<` and a newline stays a JSON value.
- **Unarmed:** the manifest is 404 and no frame emits `pwaHead()` output.
- **Flag refusals:**
  - a blank value → `exitConfig`;
  - a 13-character short name is refused, 12 is accepted;
  - `-app-name` without `-app-icon-variant` is refused, naming the flag;
  - an unknown variant is refused, naming the set.
- **Icons:**
  - the set EQUALS variants × sizes;
  - IHDR matches `sizes`;
  - two variants differ;
  - `checks.ui-icons-are-current` has its negative control.
- **No script yet:** `AllowedScriptSources()` still has ONE entry, and every frame's armed output
  has no `<script>` beyond `filter.js` on the scope page.
- **`pwa_check.sh`:** clauses (a), (b: name, icon) and (c), plus (d) if S3 landed first.
  `--self-test` prints `sabotaged=6 caught=6` (or `7` if S3 landed first). The three (c)
  sabotages need S1's refusals to be ON, which slice order guarantees.

**S3.**
- `TestEveryNonPublicHTMLRowIsNoStore` walks the ledger. Every non-public GET row must send
  `no-store`, and every public HTML row `no-cache`. **Superseded at build (decision 8's note):
  every HTML row, public or not, must send `no-store`.**
- **RED at base** (no header today), green at head.
- The mint response is still `no-store`, because the folded `writeHTMLNoStore` caller is covered
  by the same walk.
- If S2 landed first, S3 wires (d) into `pwa_check.sh` and its `--self-test` then prints
  `sabotaged=7 caught=7`.

**S4.**
- **Allowlist:** exactly `pwa.js` and `filter.js`; inline and foreign scripts are still refused.
- **Install button:** hidden by default, and revealed by a synthetic `beforeinstallprompt`
  dispatched by the test — the REACHABILITY control, since headless supplies no engagement.
- **Shortcuts:** each `url` is a declared GET row. The unauthenticated HTML `GET` of each answers
  a 303 to these literal `Location`s:
  - Arcs → `/sign-in?next=%2Farcs`
  - Search → `/sign-in?next=%2F%3Fq%3D`
  - Sharing → `/sign-in?next=%2Fshare`

  The literals are pinned, never derived from `signInLocation`.
- **The remembered hint:**
  - with `navigator.standalone = false`, dismiss → exactly `{cairn.installHintDismissed: "1"}`;
  - after a reload, the hint is hidden;
  - with `setItem` stubbed to throw, the hint still shows and the page logs no error;
  - after `POST /sign-out`, the key is still present, pinning decision 11.
- **Screenshots:**
  - `checks.ui-screenshots-are-current` has its negative control (one byte appended must
    compare unequal);
  - each manifest entry resolves to a declared row whose bytes EQUAL the embedded file and whose
    IHDR matches;
  - there is ≥ 1 `narrow` and ≥ 1 `wide`;
  - **provenance control:** rebuild with one fixture scope renamed, and the check goes RED.
- **`pwa_check.sh --self-test`:** `sabotaged=9 caught=9`. S4 adds two sabotages, (b:
  screenshots) and (e), to S2's and S3's seven.

**S5.**
- `pwa.js` reveals Back/Reload only under `display-mode: standalone`. CDP cannot emulate that
  ([M]), so the browser test stubs `matchMedia` for that one query and asserts both controls
  appear; without the stub they stay hidden (the control).
- The spelling guard admits only `history.back` and `location.reload` as additions.
- **Device behaviour is checklist step 8**, a device check rather than a test, and not part of the
  closing condition.
- **AS BUILT (S5):** the standalone CSS (the widened first row, overscroll) is keyed
  on the REVEALED controls (`.page-header:has(> .standalone-nav:not([hidden]))`), not on `@media
  (display-mode: standalone)` — same condition in practice (only `pwa.js`'s standalone check reveals
  them), but measurable under the test's stub. The browser test (`uiaudit/standalone_test.go`) also
  reads the layout: no header row added, reading order, NOT sticky, overscroll. Reasoning:
  `internal/ui/README.md` Phase V.
- **DROPPED BY OPERATOR DECISION (S5 follow-up): the sticky header.** S5's first commit (#223) pinned
  the standalone header; the follow-up removed it. Pinned, the two-row header cost about 12% of a phone
  screen on every page and occluded in-page anchor targets, and the recorded ask was working Back and
  Reload, which do not need it. The header now scrolls away like a tab's, so the audit's 🟡 "sticky
  header covers in-page anchors" is closed by removal and **no `scroll-padding` is needed**. Back/Reload
  stay in the header; `overscroll-behavior-y: contain` stays. `TestStandaloneBackAndReload` refuses
  `sticky`/`fixed` and requires the header to have scrolled away after 400px — watched RED with the
  sticky rule re-added.

**S6a.**
- The job log's `chromium --version` equals the flake-resolved version on two consecutive runs.

**S6b.**
- **Negative controls:**
  - a scratch branch with the scope page's `.view-tab`s at 12×12 px and no gap fails
    `uiaudit-touch` with the target-size refusal's own message;
  - a 14 px input fails it with the font refusal's message;
  - a branch that removes touch emulation fails it with the reachability refusal's message.
- **Positive control:** `main` passes.
- **No `continue-on-error`**, read off the workflow file.

## Open questions

### Answered by the operator (recorded as O5–O13)

| Q | question | recommendation | operator's answer |
|---|---|---|---|
| Q1 | Device-prove iOS sign-in before the client instance is announced? | yes | yes → O5 |
| Q2 | A distinct icon per instance? | no for v1 | yes, via the variant flag → O6 |
| Q3 | Install screenshots? | no for v1 | yes, synthetic and derivation-pinned → O7 |
| Q4 | Remember the dismissed iOS hint? | no | yes, in `localStorage` → O8 |
| Q5 | How to verify standalone-only behaviour? | on device | as recommended, S5 in scope → O9. Rejected: a `?display=standalone` hook, a public surface, not a test seam. |
| Q6 | Make the touch refusals blocking? | after pinning chromium | pin, then block in a separate PR → O11 |
| Q8 | Client installable before #202? | — | moot: #202 is merged |

### Still open

- **Q7. Does `no-store` cost anything the operator cares about?** **Recommend accepting it.**
  Measure it on the personal instance as soon as S3 deploys, in a Safari tab, by navigating and
  going Back. It does not need the checklist.
- **Q9. Which variant does each instance get?** It is a deployment choice. **Recommend** the
  operator picks it at S2 deploy time and keeps the mapping with the deployment manifests, never in
  this repository.
- **Q10 (new, audit round 1, item 5). How does S0's journal world exist, and what about the
  invite MINT form?**
  - **Recommend:** the journal-backed boot runs **BESIDE** the token-file boot, not instead of it.
    The token-file world remains a supported deployment and the walk's existing captures depend
    on it.
  - This answers `uiaudit/boot.go:153-158`'s objection on its own terms. That comment rejects an
    INVENTED journal because it would render "a page no deployment serves"; a journal world is
    exactly what both deployed instances serve.
  - The journal is seeded by `uiaudit/boot.go` through `internal/control`'s own writer, granting
    the fixture principal `admin` on fixture scopes, so the walk reaches the per-scope share page
    and its grant form.
  - **The invite MINT form stays UNCAPTURED in S0.** It needs `-db-dsn` (PostgreSQL), and the
    uiaudit job has none. Its controls (one `select`, one submit) are covered by S1's rules by
    CLASS, and that residual is named.
  - Capturing it means adding a `services: postgres` to the uiaudit job on the `pgtest` precedent.
    That image pin is asserted by `tests/test_pgtest_tier_is_declared.py`, so it would be a
    fourth site joining that agreement. **Recommend deferring that** until an S1 defect is found
    on the mint form.

## Recommended improvements beyond the ask (clearly recommendations)

- **B1. Whole-row targets for list rows.** On the scope page the only link is a 14 px mono ref.
  Make the row the link. This is the single biggest touch win measured: up to 24 small targets per
  page. It is in S1.
- **B2. A compact mobile header.** Under `pointer: coarse`, make the nav a full-width row of three
  44 px links. In S1.
- **B3. The bell as a real button on touch.** `Ring` is 49×22; make it ≥ 44 px under
  `pointer: coarse` (`tailwind.css:472-475`). In S1.
- **B4. HTML refusal pages for browsers.** Plain-text 404/500 bodies are dead ends in a standalone
  window. For `Accept: text/html` only, render a minimal public frame linking `/`, keeping the 401
  uniform.
- **B5. `<link rel="icon">` ends the favicon carve-out** (`browser.go:223-242`). Delete the
  carve-out in S2, rather than leave a dead branch.
- **B6. Derive the theme colour.** Emit the surface colour from the Tailwind build rather than
  hand-converting `oklch`, so a palette change reaches the browser chrome.
- **B7. A tables policy before tables arrive.** Require `overflow-x-auto` on any table wrapper.
- **B8 (new, R10). The `hover:` blind spot.** Headless chromium reports `hover: none` at every
  width, so every existing `hover:` rule is unmeasured by the walk. Record it in
  `uiaudit/README.md`'s blind set in S0. Do not try to fix it by emulation: the protocol has no
  hover emulation in the calls measured here.

## What could not be measured

- **Any WebKit behaviour.** That covers iOS input zoom, the OAuth hand-back (R6), safe-area
  insetting in an iOS 26 standalone window (R7, unsourced), the iOS hint's feature detection, and
  Back-navigation cost under `no-store`. All of it is deferred to the iPhone checklist or Q7.
- **`beforeinstallprompt` under REAL engagement.** It was measured only with the engagement check
  bypassed (R5). So was whether Chrome's own ambient prompt still wants a fetch handler.
- **Standalone display mode in any harness here.** CDP media emulation had no effect [M].
- **The invite mint form at 390 px** (Q10), and the GitHub button. The uiaudit world has neither
  a DSN nor a provider.
- **Whether the derivation-built screenshots are byte-identical on two hosts** (decision 16).
- **How long iOS Safari keeps the O8 key** [S].
- **The pinned chromium's numbers against the ones above.** All of them are chromium 154 from
  nixpkgs on one host; S6a moves CI to the flake's chromium, and S0 re-measures there.
- **The `hover:` rules at any width** (B8).
- **Sizes and effort.** Not estimated; nobody has measured these slices.
