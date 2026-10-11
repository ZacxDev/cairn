package ui

import (
	"bytes"
	"image/png"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// 🔴 S4 OF THE MOBILE PLAN: `pwa.js`, the shortcuts and the install screenshots (`pwa.go`). Every
// expectation is a LITERAL — paths digested here from the committed bytes, names, sizes, the exact
// markup — never read back off the implementation.

// TestThePWAScriptIsServedAtItsContentHashedRoute: the row `pwaHead` links answers the embedded bytes,
// as JavaScript, `nosniff`, `immutable`, and to an ANONYMOUS caller — the sign-in page links it.
func TestThePWAScriptIsServedAtItsContentHashedRoute(t *testing.T) {
	want := "/static/pwa." + pwaDigestFromBytes(t) + ".js"
	if PWAScriptPath != want {
		t.Errorf("PWAScriptPath is %q, want %q — the digest of the bytes this binary serves", PWAScriptPath, want)
	}
	disk, err := os.ReadFile("pwa.js")
	if err != nil {
		t.Fatal(err)
	}
	for _, auth := range []string{"authenticated", "anonymous"} {
		srv := newTestServer(t, staticAuth{testIdentity()})
		if auth == "anonymous" {
			srv = newTestServer(t, refusingAuth{})
		}
		rec := getAs(t, srv, want)
		if rec.Code != http.StatusOK || !bytes.Equal(rec.Body.Bytes(), disk) {
			t.Errorf("%s GET %s answered %d with %d byte(s), want 200 and the %d committed bytes of pwa.js",
				auth, want, rec.Code, rec.Body.Len(), len(disk))
			continue
		}
		for header, v := range map[string]string{
			"Content-Type":           "text/javascript; charset=utf-8",
			"X-Content-Type-Options": "nosniff",
			"Cache-Control":          "public, max-age=31536000, immutable",
		} {
			if got := rec.Header().Get(header); got != v {
				t.Errorf("%s %s: %s = %q, want %q", auth, want, header, got, v)
			}
		}
	}
}

// pwaHintKey is O8's ONE storage key, typed here as a literal.
const pwaHintKey = "cairn.installHintDismissed"

// bannedPWASinks: what an install button and a dismissable hint have no use for. Each turns text into
// markup or code, sends something off the page, or stores something — and `serviceWorker`/`caches` pin
// O13 (no service worker in v1) in the script itself. `location` and `history` are banned until S5,
// whose Back/Reload controls admit exactly `history.back` and `location.reload`.
var bannedPWASinks = []string{
	"innerHTML", "outerHTML", "insertAdjacentHTML", "document.write", "eval(", "Function(",
	"setTimeout(", "setInterval(", "fetch(", "XMLHttpRequest", "sendBeacon", "WebSocket",
	"document.cookie", "sessionStorage", "indexedDB", "caches", "serviceWorker", "import(",
	"createElement", "setAttribute", "src=", "href", "location", "history", "removeItem", "clear(",
}

// pwaStorageViolations is the localStorage half of the spelling guard: `localStorage` may appear ONLY
// as `window.localStorage.getItem(HINT_KEY)` and `window.localStorage.setItem(HINT_KEY, "1")`, each
// exactly once; `HINT_KEY` is declared once, as the literal key; and no other `"cairn.` string exists.
func pwaStorageViolations(code string) []string {
	var out []string
	get := `window.localStorage.getItem(HINT_KEY)`
	set := `window.localStorage.setItem(HINT_KEY, "1")`
	decl := `var HINT_KEY = "` + pwaHintKey + `";`
	if n := strings.Count(code, get); n != 1 {
		out = append(out, "the one read "+get+" appears "+strconv.Itoa(n)+" time(s), want 1")
	}
	if n := strings.Count(code, set); n != 1 {
		out = append(out, "the one write "+set+" appears "+strconv.Itoa(n)+" time(s), want 1")
	}
	if n := strings.Count(code, "localStorage"); n != 2 {
		out = append(out, "`localStorage` appears "+strconv.Itoa(n)+" time(s); only the one read and the one write are allowed")
	}
	if n := strings.Count(code, decl); n != 1 {
		out = append(out, "the key declaration "+decl+" appears "+strconv.Itoa(n)+" time(s), want 1")
	}
	if n := strings.Count(code, "HINT_KEY"); n != 3 {
		out = append(out, "HINT_KEY appears "+strconv.Itoa(n)+" time(s); only its declaration, the read and the write are allowed")
	}
	if n := strings.Count(code, `"cairn.`); n != 1 {
		out = append(out, "a `\"cairn.` string appears "+strconv.Itoa(n)+" time(s); the one storage key is the only one allowed")
	}
	return out
}

// TestThePWAScriptTouchesOnlyWhatItSays pins the header of `pwa.js`: it reveals two controls, reads
// and writes ONE storage key, and reaches nothing else — no worker, no cache, no request, no markup.
//
// ⚠ A SPELLING GUARD, AND LABELLED AS ONE (decision 5): `window["local"+"Storage"]` walks it. What it
// catches is the ordinary edit — a timestamp beside the flag, an `innerHTML` label, a worker
// registration "for offline". The STATE guard is the browser test `TestPWAClauses/e_storage`, which
// reads what the script actually stored.
func TestThePWAScriptTouchesOnlyWhatItSays(t *testing.T) {
	code := scriptCode(pwaScript)
	// INSTRUMENT CONTROL: the strip left the code — the calls the script is built from are there.
	for _, must := range []string{`addEventListener("beforeinstallprompt"`, ".hidden = false", "navigator.standalone",
		`getElementById("pwa-install-hint")`} {
		if !strings.Contains(code, must) {
			t.Fatalf("the comment-stripped pwa.js lacks %q, so the scan below is reading something other than the code", must)
		}
	}
	for _, sink := range bannedPWASinks {
		if strings.Contains(code, sink) {
			t.Errorf("pwa.js uses %q, which the install button and the iOS hint have no use for — see the header "+
				"of pwa.js and plan decisions 5 and 11", sink)
		}
	}
	for _, v := range pwaStorageViolations(code) {
		t.Errorf("pwa.js storage: %s — O8 allows ONE key, %q = \"1\", written only on a dismiss tap", v, pwaHintKey)
	}
	// NEGATIVE CONTROLS, in code position, each realistic: a second key, a worker, an innerHTML label.
	for name, extra := range map[string]string{
		"a second storage key": "\nwindow.localStorage.setItem(HINT_KEY + \"At\", String(Date.now()));\n",
		"a service worker":     "\nnavigator.serviceWorker.register(\"/sw.js\");\n",
		"an innerHTML label":   "\nbutton.innerHTML = \"<b>Install</b>\";\n",
	} {
		mutated := scriptCode(pwaScript + extra)
		red := len(pwaStorageViolations(mutated)) > 0
		for _, sink := range bannedPWASinks {
			red = red || strings.Contains(mutated, sink)
		}
		if !red {
			t.Errorf("NEGATIVE CONTROL FAILED: %s produced no violation, so the guard above cannot go red", name)
		}
	}
}

// TestTheInstallControlsAreHiddenAndArmedOnly pins the two controls' markup, exactly: the Install
// button on EVERY authenticated page (it is in the shell's header) and the iOS hint on the ROOT page
// ONLY — both rendered `hidden`, so a page with script off shows neither.
func TestTheInstallControlsAreHiddenAndArmedOnly(t *testing.T) {
	const button = `<button type="button" class="install" id="pwa-install" hidden>Install</button>`
	const hint = `<div class="install-hint" id="pwa-install-hint" hidden><p>Install: Share → Add to Home Screen</p>` +
		`<button type="button" id="pwa-install-hint-dismiss">Dismiss</button></div>`
	srv := armedServerWith(t, testConfig(t, staticAuth{testIdentity()}), appAlpha)
	pages := htmlRows(t, srv)
	withButton, withHint := 0, 0
	for row, html := range pages {
		public := strings.HasSuffix(row, " public")
		if n := strings.Count(html, button); (public && n != 0) || (!public && n != 1) {
			t.Errorf("%s: the Install button appears %d time(s); want 1 on an authenticated page, 0 on a public one", row, n)
		}
		withButton += strings.Count(html, button)
		n := strings.Count(html, hint)
		if (row == "GET / content") != (n == 1) || n > 1 {
			t.Errorf("%s: the iOS hint appears %d time(s); want exactly once on the root page and nowhere else", row, n)
		}
		withHint += n
	}
	// POSITIVE CONTROL: the walk saw both controls at all, so the per-page zeros mean something.
	if withButton < 8 || withHint != 1 {
		t.Fatalf("the walk saw the Install button on %d page(s) and the hint on %d — the assertions above "+
			"read almost nothing", withButton, withHint)
	}
	// Unarmed, neither is a node at all.
	if pwaInstallButton(App{}) != nil || pwaInstallHint(App{}) != nil {
		t.Error("an UNARMED app rendered an install control")
	}
}

// TestEveryShortcutIsADeclaredRowThatReturnsThroughSignIn: each manifest shortcut's path is a declared
// GET row; signed in it answers 200; and a STRANGER's browser navigation (an expired session on a
// home-screen shortcut) answers a 303 to these LITERAL locations — pinned, never derived from
// `signInLocation`, so the return path the plan promises (T7) is checked against what was promised.
func TestEveryShortcutIsADeclaredRowThatReturnsThroughSignIn(t *testing.T) {
	want := map[string]string{
		"/arcs":      "/sign-in?next=%2Farcs",
		"/scopes?q=": "/sign-in?next=%2Fscopes%3Fq%3D",
		"/team":      "/sign-in?next=%2Fteam",
	}
	shortcuts := appShortcuts()
	if len(shortcuts) != len(want) {
		t.Fatalf("%d shortcut(s), want %d", len(shortcuts), len(want))
	}
	ledger := DeclaredRouteLedger()
	signedIn := newTestServer(t, staticAuth{testIdentity()})
	stranger := newTestServer(t, refusingAuth{})
	for _, s := range shortcuts {
		loc, ok := want[s.URL]
		if !ok {
			t.Errorf("shortcut %q targets %q, which this test does not expect", s.Name, s.URL)
			continue
		}
		path, _, _ := strings.Cut(s.URL, "?")
		if !slices.Contains(ledger, "GET "+path+" content") {
			t.Errorf("shortcut %q targets %q, which is not a declared `GET %s content` row", s.Name, s.URL, path)
		}
		if rec := getAs(t, signedIn, s.URL); rec.Code != http.StatusOK {
			t.Errorf("signed in, shortcut %q (%s) answered %d, want 200", s.Name, s.URL, rec.Code)
		}
		rec := fetch(stranger, s.URL, map[string]string{"Accept": "text/html,application/xhtml+xml"})
		if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != loc {
			t.Errorf("a stranger's browser on shortcut %q (%s) answered %d Location %q, want 303 %q", s.Name, s.URL,
				rec.Code, rec.Header().Get("Location"), loc)
		}
	}
}

// TestTheScreenshotSetIsExactlyTheCommittedFiles: the embedded PNGs are EXACTLY the literal names
// (grow or shrink is red); each is a PNG whose IHDR is its declared size; there is at least one
// `narrow` and one `wide`; and each row serves the file ON DISK, `immutable`, to an anonymous caller.
func TestTheScreenshotSetIsExactlyTheCommittedFiles(t *testing.T) {
	want := map[string][3]string{
		"narrow-hub":  {"/", "narrow", "390x844"},
		"narrow-arcs": {"/arcs", "narrow", "390x844"},
		"wide-hub":    {"/", "wide", "1440x900"},
	}
	entries, err := screenshotFS.ReadDir("screenshots")
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".png") {
			got = append(got, strings.TrimSuffix(e.Name(), ".png"))
		}
	}
	slices.Sort(got)
	if !slices.Equal(got, []string{"narrow-arcs", "narrow-hub", "wide-hub"}) {
		t.Errorf("the embedded screenshot set is %v, want exactly [narrow-arcs narrow-hub wide-hub]. "+
			"Regenerate with `nix run .#build-ui-screenshots`", got)
	}
	forms := map[string]int{}
	for _, s := range ScreenshotSpecs() {
		w, ok := want[s.Name]
		if !ok {
			t.Errorf("screenshots.json declares %q, which this test does not expect", s.Name)
			continue
		}
		size := strconv.Itoa(s.Width) + "x" + strconv.Itoa(s.Height)
		if s.Page != w[0] || s.FormFactor != w[1] || size != w[2] {
			t.Errorf("%s is page %q %s %s, want page %q %s %s", s.Name, s.Page, s.FormFactor, size, w[0], w[1], w[2])
		}
		forms[s.FormFactor]++
	}
	if forms["narrow"] < 1 || forms["wide"] < 1 {
		t.Errorf("form factors %v: the richer install dialog needs at least one narrow and one wide", forms)
	}
	anon := newTestServer(t, refusingAuth{})
	served := 0
	for _, f := range screenshotFiles {
		disk, err := os.ReadFile(filepath.Join("screenshots", f.Name+".png"))
		if err != nil {
			t.Fatal(err)
		}
		cfg, err := png.DecodeConfig(bytes.NewReader(disk))
		if err != nil {
			t.Errorf("%s.png is not a PNG: %v", f.Name, err)
			continue
		}
		if size := strconv.Itoa(cfg.Width) + "x" + strconv.Itoa(cfg.Height); size != want[f.Name][2] {
			t.Errorf("%s.png is %s, its manifest entry says %s", f.Name, size, want[f.Name][2])
		}
		if f.Path != screenshotRowFromBytes(t, f.Name) {
			t.Errorf("%s is served at %s, want the digest of the committed file %s", f.Name, f.Path, screenshotRowFromBytes(t, f.Name))
		}
		rec := fetch(anon, f.Path, nil)
		if rec.Code != http.StatusOK || !bytes.Equal(rec.Body.Bytes(), disk) {
			t.Errorf("%s answered %d with %d byte(s), want 200 and the %d committed bytes", f.Path, rec.Code,
				rec.Body.Len(), len(disk))
			continue
		}
		for k, v := range map[string]string{
			"Content-Type":           "image/png",
			"Cache-Control":          "public, max-age=31536000, immutable",
			"X-Content-Type-Options": "nosniff",
		} {
			if got := rec.Header().Get(k); got != v {
				t.Errorf("%s %s: %q, want %q", f.Path, k, got, v)
			}
		}
		served++
	}
	if served != 3 {
		t.Errorf("served %d screenshot(s), want 3", served)
	}
}
