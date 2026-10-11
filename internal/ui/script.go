package ui

import (
	_ "embed"
	"net/http"

	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"

	"github.com/ZacxDev/cairn/internal/identity"
)

// filterScript is the scope page's entry filter: the FIRST of the three scripts this surface serves
// (the second, `pwa.js`, is linked only on an armed deployment — `pwa.go`; the third, `join.js`, only
// by the fragment join page — `join.go`).
//
// 🔴 THIS FILE REVERSES THE PACKAGE'S ZERO-SCRIPT PROPERTY, ON AN OPERATOR DECISION, AND WHAT
// REPLACES IT IS AN ALLOWLIST RATHER THAN A HOPE. The surface used to render no script at all and
// `uiaudit` refused any capture with `document.scripts.length != 0`. The operator chose a
// client-side filter over a server-side form, so the claim is now NARROWER and still exact: every
// script element THIS SERVER RENDERS is a same-origin `src` named by [AllowedScriptSources], at
// most once, with NO inline body — and nothing else. ⚠ That is a claim about the ORIGIN's bytes:
// a script inserted downstream (the edge injection `README.md` measures) reaches the SERVED page
// and none of the guards below can see it, because each boots its own pod. Three guards hold the
// origin claim, at three depths:
//
//   - `TestEveryBrowsePageCarriesOnlyAllowlistedScripts` (render_test) over the rendered bytes, with
//     negative controls that an inline script and a foreign `src` both go red;
//   - `uiaudit`'s `refuseWalkRegressions` over the BROWSER's own `document.scripts`, which sees a
//     script an injection or a parser recovery created and a byte scan cannot;
//   - `TestTheFilterScriptTouchesOnlyWhatItSays`, which refuses the sinks a filter has no use for
//     (`innerHTML`, `eval`, `fetch`, …) in `filter.js`'s text — and its twins for the second and
//     third scripts, `TestThePWAScriptTouchesOnlyWhatItSays` over `pwa.js`'s and
//     `TestTheJoinScriptTouchesOnlyWhatItSays` over `join.js`'s.
//
// ⚠ WHAT IT DOES NOT CHANGE: the escaping story. Every user string still reaches the page through
// `g.Text` or a quoted attribute value; the script READS what the server already escaped into a
// `data-filter` attribute and writes only `hidden` and a count's `textContent`. And
// `rawban_test.go` still bans `g.Raw`/`g.Rawf`: the tag below is built from `h.Script`/`h.Src`, so
// the one script element is as structurally ordinary as a `<link>`.
//
// ⚠ AND THE SAME FLAKE RULE AS `app.css`: `//go:embed` on a file `flake.nix`'s `onlyGo` filter does
// not carry stops compilation dead in the sandbox, so the filter names this file explicitly.
//
//go:embed filter.js
var filterScript string

// FilterScriptPath is the path the scope page links, and it carries a digest of the bytes it
// serves — the stylesheet's mechanism exactly (see `stylesheet.go`), so a changed script is a NEW
// URL and the year-long `immutable` on its response is licensed by the URL, not by the bytes
// being stable.
//
// ⚠ THERE IS NO UNVERSIONED TWIN, WHERE THE STYLESHEET HAS ONE. That row exists for URLs already
// loose in the world from before the hashed path existed; no page has ever linked an unversioned
// script, so there is nothing for a twin to keep answering.
var FilterScriptPath = hashedScriptPathFor(filterScript)

// hashedScriptPathFor spells the served path for a given script body, through the SAME digest
// the stylesheet uses.
func hashedScriptPathFor(js string) string {
	return "/static/filter." + hashAsset(js) + ".js"
}

// AllowedScriptSources is the WHOLE script allowlist: the `src` values a rendered page may carry
// on a `<script>` element. A copy, so a caller cannot widen it.
//
// 🔴 A SCRIPT NOT NAMED HERE IS A REGRESSION WHEREVER IT APPEARS, AND SO IS AN INLINE ONE — an
// empty `src` is never on this list. Adding a script is an edit HERE, to a list both the
// renderer's guard and `uiaudit`'s walk read, rather than a tag that quietly appears on a page.
//
// ✅ AND THE SECOND ENTRY IS `pwa.js` (S4 of the mobile plan, `pwa.go`): linked by `pwaHead` on an
// ARMED deployment only, with its own spelling guard, `TestThePWAScriptTouchesOnlyWhatItSays`.
//
// ✅ AND THE THIRD IS `join.js` (`join.go`), by operator decision: a team-link token travels in the
// URL FRAGMENT so no server's access log can hold it, and only script can move a fragment into a
// form. Linked by the fragment join page alone, with its own spelling guard,
// `TestTheJoinScriptTouchesOnlyWhatItSays`.
func AllowedScriptSources() []string {
	return []string{FilterScriptPath, PWAScriptPath, JoinScriptPath}
}

// filterScriptTag is the ONE way a page reaches the script. `defer` so it runs after the
// document is parsed, wherever the element sits.
func filterScriptTag() g.Node {
	return h.Script(h.Src(FilterScriptPath), h.Defer())
}

// handleFilterScript serves the script's hashed row. PUBLIC, like both stylesheet rows, for the
// same reason they are: it consults no authority and answers the same bytes to everybody, which
// is what makes it safe to serve before authentication. It serves a Go variable and touches no
// filesystem, so there is no traversal to get wrong (see `writeStylesheet`).
func (s *Server) handleFilterScript(w http.ResponseWriter, _ *http.Request, _ identity.Identity) {
	w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	// `nosniff` matters MORE here than on the stylesheet: a browser refuses to execute a script
	// whose type it was told is not JavaScript only when this header is set.
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", stylesheetCacheImmutable)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(filterScript))
}
