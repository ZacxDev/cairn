package ui

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

// linkedStylesheetHref is the `<link rel=stylesheet>` href a rendered page carries.
//
// 🔴 IT IS READ OUT OF THE RENDERED HTML RATHER THAN FROM A PACKAGE VARIABLE, because the
// claim every test in this file makes is about what a BROWSER is told to fetch. Reading
// `StylesheetHashedPath` and asserting things about it would be the implementation restating
// itself: a renderer that emitted a different href entirely would satisfy every such
// assertion.
var linkedStylesheetHref = regexp.MustCompile(`<link [^>]*rel="stylesheet"[^>]*href="([^"]+)"`)

// hrefFromPage renders a page through the whole server and returns the stylesheet href it
// carries.
func hrefFromPage(t *testing.T, srv *Server, page string) string {
	t.Helper()
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest("GET", page, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("%s answered %d, want 200 — a page that did not render says nothing about its href",
			page, rec.Code)
	}
	m := linkedStylesheetHref.FindStringSubmatch(rec.Body.String())
	if m == nil {
		t.Fatalf("%s carries no <link rel=\"stylesheet\" href=…>, so this test has nothing to measure. "+
			"Either the page lost its stylesheet or the extractor above stopped matching the markup "+
			"gomponents emits — and the second failure would make every assertion here vacuous.", page)
	}
	return m[1]
}

// stylesheetDigestFromBytes is the digest the ledger expectation substitutes into the hashed
// row.
//
// 🔴 IT RECOMPUTES FROM THE EMBEDDED BYTES AND NEVER READS `hashStylesheet`, so the ledger row
// is a claim about the stylesheet rather than a restatement of the code that hashes it. The
// width is spelled here a SECOND time on purpose: changing `stylesheetHashWidth` then fails
// this file too, which is a decision somebody takes twice rather than a number that follows
// the implementation wherever it goes.
func stylesheetDigestFromBytes(t *testing.T) string {
	t.Helper()
	if len(stylesheet) == 0 {
		t.Fatal("the embedded stylesheet is EMPTY, so the digest below is the digest of nothing and every " +
			"comparison built on it would hold for a surface that serves no styles at all")
	}
	sum := sha256.Sum256([]byte(stylesheet))
	return hex.EncodeToString(sum[:])[:12]
}

// TestThePageLinksTheStylesheetByItsOwnDigest is THE REGRESSION TEST, and the defect it pins
// shipped to a real browser.
//
// 🔴 THE SYMPTOM: after a deploy the origin served the current stylesheet while a returning
// browser went on applying the previous one, because the URL was the same string before and
// after. Nothing was 4xx, nothing was slow, no entry was missing — the page simply rendered
// against bytes that no longer existed anywhere but in a cache. The measured window was hours
// rather than the five minutes this process asked for, because a cache in front of the origin
// is free to lengthen a `max-age` and one did.
//
// 🔴 SO THE INVARIANT IS "THE URL IS DERIVED FROM THE BYTES", AND IT IS MEASURED END TO END:
// the page is rendered, its `<link href>` is read out of the HTML, THAT href is fetched, and
// the bytes that come back are hashed. The digest of the response must appear in the URL that
// asked for it. A URL with that property cannot come to mean different bytes, which is the
// only thing that makes a long-lived cache entry safe.
//
// ⚠ IT DOES NOT PIN THE DIGEST WIDTH, AND THAT IS DELIBERATE. It requires the first EIGHT hex
// characters of the response's digest to appear in the URL, so an implementation that carried
// 12, 16 or all 64 satisfies it. Pinning the width here would make this test fail for a change
// that did not break the property it exists for. Eight hex characters occurring by chance in a
// twenty-character path is not a case worth defending against.
func TestThePageLinksTheStylesheetByItsOwnDigest(t *testing.T) {
	srv := newTestServer(t, staticAuth{testIdentity()})

	checked := 0
	for _, page := range []string{RootPath, SignInPath, SharePath} {
		href := hrefFromPage(t, srv, page)

		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, httptest.NewRequest("GET", href, nil))
		if rec.Code != http.StatusOK {
			t.Errorf("%s links %q and that path answers %d. The link and the route are one relationship; "+
				"a page linking a path nobody serves renders unstyled.", page, href, rec.Code)
			continue
		}
		served := rec.Body.String()
		if len(served) < 200 {
			t.Errorf("%s served %d bytes, which is not a stylesheet — the digest comparison below would be "+
				"a fact about an empty response", href, len(served))
			continue
		}
		sum := sha256.Sum256([]byte(served))
		digest := hex.EncodeToString(sum[:])
		if !strings.Contains(href, digest[:8]) {
			t.Errorf("the page %s links %q, and the bytes that URL serves digest to %s… — the URL carries "+
				"NO part of the digest of its own body.\n"+
				"That is the whole defect: a cache is keyed on the URL, so a URL that does not move when "+
				"the bytes move is an entry nothing can invalidate, and a returning browser goes on "+
				"applying a stylesheet the origin no longer serves. Derive the path from the bytes "+
				"(`hashedStylesheetPathFor`) rather than writing it down.",
				page, href, digest[:16])
			continue
		}
		checked++
	}
	if checked == 0 {
		t.Fatal("no page was measured, so this test reported nothing either way")
	}
	t.Logf("%d page(s) link a stylesheet URL carrying the digest of the bytes it serves", checked)
}

// withStylesheetBytes swaps the embedded stylesheet AND everything derived from it — the
// hashed path and the dispatch row keyed on it — and restores all three.
//
// 🔴 IT EXISTS SO THE DERIVATION CAN BE MEASURED AT MORE THAN ONE POINT, which is the only way
// to tell it from a frozen literal. `TestThePageLinksTheStylesheetByItsOwnDigest` above
// compares a URL against the digest of one stylesheet; an implementation that hardcoded the
// current digest as a string would satisfy it exactly, and would be the original defect wearing
// a longer path. Two different bodies is what separates them.
//
// ⚠ IT MUTATES PACKAGE STATE, WHICH IS SAFE HERE FOR ONE REASON AND NOT BY LUCK: no test in
// this package calls `t.Parallel`. A parallel test added later would see a stylesheet swapped
// out from under it. Restores are registered with `t.Cleanup`, so nested calls unwind LIFO and
// each one restores whatever the previous call had installed.
func withStylesheetBytes(t *testing.T, css string) {
	t.Helper()
	prevCSS, prevPath := stylesheet, StylesheetHashedPath
	prevRoute, ok := routes[routeKey{"GET", prevPath}]
	if !ok {
		t.Fatalf("there is no dispatch row for %q, so this helper would install a second one and the "+
			"restore below would leave the table wrong", prevPath)
	}

	stylesheet = css
	StylesheetHashedPath = hashedStylesheetPathFor(stylesheet)
	delete(routes, routeKey{"GET", prevPath})
	routes[routeKey{"GET", StylesheetHashedPath}] = prevRoute

	t.Cleanup(func() {
		delete(routes, routeKey{"GET", StylesheetHashedPath})
		stylesheet, StylesheetHashedPath = prevCSS, prevPath
		routes[routeKey{"GET", prevPath}] = prevRoute
	})
}

// The two synthetic stylesheets the test below drives. Each is a real stylesheet — the pages'
// own selectors, one declaration apart — because the assertion is about the URL and a body
// that could not be served at all would measure the wrong thing.
const (
	themeOne = "body { color: #101010; }\n.viewer { font-weight: 600; }\n" +
		"/* a synthetic stylesheet: this file's fixtures never reach a build */\n"
	themeTwo = "body { color: #fefefe; }\n.viewer { font-weight: 600; }\n" +
		"/* a synthetic stylesheet: this file's fixtures never reach a build */\n"
)

// TestTheStylesheetURLChangesWhenTheBytesChange measures the derivation at TWO POINTS, which is
// the claim a single stylesheet structurally cannot make.
//
// 🔴 THE MUTANT IT IS FOR: `StylesheetHashedPath` written down as a literal equal to the
// current digest. Every other test in this package stays green under that mutant —
// `TestThePageLinksTheStylesheetByItsOwnDigest` included, because the literal WOULD be the
// digest of the one stylesheet this tree carries — and the surface would be back to a URL that
// cannot move, which is the defect. Only varying the bytes separates the two.
//
// ⚠ AND IT ASSERTS THE OLD URL IS GONE, NOT MERELY THAT A NEW ONE APPEARED. A route table that
// accumulated a row per theme would satisfy "the linked URL changed" while leaving the previous
// URL serving the previous bytes for ever — harmless for a cache, and a growing set of
// internet-reachable paths that the ledger would then be wrong about.
func TestTheStylesheetURLChangesWhenTheBytesChange(t *testing.T) {
	withStylesheetBytes(t, themeOne)
	srvOne := newTestServer(t, staticAuth{testIdentity()})
	hrefOne := hrefFromPage(t, srvOne, RootPath)

	recOne := httptest.NewRecorder()
	srvOne.ServeHTTP(recOne, httptest.NewRequest("GET", hrefOne, nil))
	if body := recOne.Body.String(); recOne.Code != http.StatusOK || body != themeOne {
		t.Fatalf("the first theme's URL %q answered %d with %d byte(s); it must serve the first theme's "+
			"own %d bytes, or the comparison below is about something other than these two stylesheets",
			hrefOne, recOne.Code, len(body), len(themeOne))
	}

	withStylesheetBytes(t, themeTwo)
	srvTwo := newTestServer(t, staticAuth{testIdentity()})
	hrefTwo := hrefFromPage(t, srvTwo, RootPath)

	if hrefOne == hrefTwo {
		t.Fatalf("two DIFFERENT stylesheets are linked at the SAME URL %q. A cache is keyed on the URL, so "+
			"a browser holding the first will never be told the second exists — which is the exact failure "+
			"the hashed path exists to close. The path is not derived from the bytes: check that "+
			"`StylesheetHashedPath` is computed by `hashedStylesheetPathFor` and is not a literal.", hrefOne)
	}

	recTwo := httptest.NewRecorder()
	srvTwo.ServeHTTP(recTwo, httptest.NewRequest("GET", hrefTwo, nil))
	if body := recTwo.Body.String(); recTwo.Code != http.StatusOK || body != themeTwo {
		t.Errorf("the second theme's URL %q answered %d with %d byte(s), want 200 and the second theme's "+
			"own %d bytes", hrefTwo, recTwo.Code, len(body), len(themeTwo))
	}

	// The previous URL is no longer a row. A dispatcher that kept it would serve the previous
	// bytes at a path nothing links and the ledger does not declare.
	recStale := httptest.NewRecorder()
	srvTwo.ServeHTTP(recStale, httptest.NewRequest("GET", hrefOne, nil))
	if recStale.Code != http.StatusNotFound {
		t.Errorf("the PREVIOUS theme's URL %q still answers %d after the bytes changed. Each stylesheet's "+
			"URL must belong to that stylesheet alone; a table that accumulates one row per theme grows "+
			"internet-reachable paths the ledger does not declare.", hrefOne, recStale.Code)
	}

	t.Logf("two stylesheet bodies, two URLs: %q then %q; the first 404s once the bytes change", hrefOne, hrefTwo)
}

// TestTheStylesheetRowsCarryTheCacheHeadersTheirURLsLicense pins BOTH rows' `Cache-Control` as
// a whole string, in both directions.
//
// 🔴 THE HEADER IS PINNED AS THE WHOLE VALUE AND THE EXPECTATIONS ARE LITERALS, never
// `stylesheetCacheImmutable`. A test reading the constant it is checking asserts `a == a`: the
// two rows could be swapped, or both set to the short value, and it would hold.
//
// 🔴 AND `immutable` IS ONLY EVER CORRECT ON THE HASHED ROW, WHICH IS WHY THE UNVERSIONED ONE
// IS ASSERTED TOO RATHER THAN LEFT ALONE. `immutable` tells a cache not to revalidate for a
// year. On a URL that can come to mean different bytes that is a promise the server cannot
// keep, and keeping the unversioned row is only defensible while its header stays short.
func TestTheStylesheetRowsCarryTheCacheHeadersTheirURLsLicense(t *testing.T) {
	srv := newTestServer(t, staticAuth{testIdentity()})

	for _, tc := range []struct {
		name string
		path string
		want string
	}{
		{"the hashed row, whose URL changes with the bytes", StylesheetHashedPath,
			"public, max-age=31536000, immutable"},
		{"the unversioned row, whose URL never changes", StylesheetPath,
			"public, max-age=300"},
	} {
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, httptest.NewRequest("GET", tc.path, nil))
		if rec.Code != http.StatusOK {
			t.Errorf("%s (%s) answered %d, want 200", tc.name, tc.path, rec.Code)
			continue
		}
		if got := rec.Header().Get("Cache-Control"); got != tc.want {
			t.Errorf("%s (%s) sent Cache-Control %q, want %q", tc.name, tc.path, got, tc.want)
		}
		// Both rows serve the SAME bytes. Only the header differs, so a reader who finds the
		// unversioned URL in a log is not looking at an archive of an older theme.
		if got := rec.Body.String(); got != stylesheet {
			t.Errorf("%s (%s) served %d bytes and the embedded stylesheet is %d; both rows serve the "+
				"current bytes and differ only in what they ask a cache to do",
				tc.name, tc.path, len(got), len(stylesheet))
		}
	}

	// 🔴 THE OTHER DIRECTION, SPELLED OUT: the unversioned row must not merely differ from the
	// hashed one, it must not contain `immutable` at all. A value like
	// `public, max-age=300, immutable` would pass an inequality against the hashed row's string
	// while telling every cache the very thing this row cannot promise.
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest("GET", StylesheetPath, nil))
	if strings.Contains(rec.Header().Get("Cache-Control"), "immutable") {
		t.Errorf("%s is served `Cache-Control: %q`. Its URL carries no version, so `immutable` is a promise "+
			"about bytes it cannot keep — that is the defect the hashed row was added to close, reinstated "+
			"on the row that was kept for compatibility.", StylesheetPath, rec.Header().Get("Cache-Control"))
	}
}

// linksUnversionedStylesheet answers whether a rendered body points a browser at the
// unversioned path. It is a function rather than an inline `strings.Contains` so the test below
// can feed it a body that MUST match — a detector nobody has watched match is indistinguishable
// from one wired to nothing.
func linksUnversionedStylesheet(body string) bool {
	return strings.Contains(body, `href="`+StylesheetPath+`"`)
}

// TestNoPageLinksTheUnversionedStylesheetPath is the condition under which keeping the
// unversioned row is defensible at all.
//
// 🔴 THE ROW IS KEPT SO THAT URLS ALREADY LOOSE IN THE WORLD DO NOT 404 — a page a browser
// rendered before the deploy, a bookmark, a link out of a log. That argument holds only while
// the set of such URLs is CLOSED. The moment a page links it, every visitor is issued an
// unversioned URL again and the stale-cache failure is back, with the hashed row sitting beside
// it doing nothing. The failure would be silent: every page renders, every style applies on a
// cold cache, and only a returning visitor after a theme change sees anything wrong.
func TestNoPageLinksTheUnversionedStylesheetPath(t *testing.T) {
	// INSTRUMENT CONTROL: the detector must match a body that really does link the path, or the
	// zeroes below are a fact about the detector.
	if !linksUnversionedStylesheet(`<head><link rel="stylesheet" href="` + StylesheetPath + `"></head>`) {
		t.Fatal("the detector does not match a body that plainly links the unversioned path, so every " +
			"clean verdict below is a fact about this function and not about the pages")
	}
	if linksUnversionedStylesheet(`<head><link rel="stylesheet" href="` + StylesheetHashedPath + `"></head>`) {
		t.Fatalf("the detector matches the HASHED path %q as well, so it cannot tell the two rows apart "+
			"and would fail on a correct page", StylesheetHashedPath)
	}

	srv := newTestServer(t, staticAuth{testIdentity()})
	for _, page := range []string{RootPath, SignInPath, SharePath} {
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, httptest.NewRequest("GET", page, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("%s answered %d, want 200 — a page that did not render cannot be checked for its href",
				page, rec.Code)
		}
		if linksUnversionedStylesheet(rec.Body.String()) {
			t.Errorf("%s links the UNVERSIONED stylesheet path %s. That row is served only so URLs already "+
				"loose in the world keep answering; a page that links it hands every visitor a URL whose "+
				"cache entry nothing can invalidate, which is the defect the hashed row closes. Link "+
				"`StylesheetHashedPath` — `stylesheetLink` is the one place that decides.", page, StylesheetPath)
		}
	}
}

// TestTheHashedStylesheetPathHasTheShapeItClaims.
//
// ⚠ AN INVARIANT GUARD, LABELLED, AND IT IS NOT COUNTED AS REGRESSION COVERAGE. No
// implementation has ever produced a malformed path here. It is pinned because everything else
// in this file compares the path against something else derived from the same bytes, so a
// derivation that emitted, say, a path with no digest in it at all would have to be caught by
// the shape rather than by a comparison.
func TestTheHashedStylesheetPathHasTheShapeItClaims(t *testing.T) {
	shape := regexp.MustCompile(`^/static/app\.[0-9a-f]{12}\.css$`)
	if !shape.MatchString(StylesheetHashedPath) {
		t.Errorf("the hashed stylesheet path is %q, which is not `/static/app.<12 lowercase hex>.css`. "+
			"The width is `stylesheetHashWidth` and the spelling is `hashedStylesheetPathFor`; both are "+
			"written a second time here so a change to either is a decision taken twice.",
			StylesheetHashedPath)
	}
	// 🔴 THE STORED VALUE MUST BE THE LIVE DERIVATION OVER THE EMBEDDED BYTES, AND THIS IS THE
	// ONE ASSERTION THAT KILLS THE FROZEN-LITERAL MUTANT. `StylesheetHashedPath` written down as
	// the current digest satisfies every other test in this package — the pages link it, the
	// route serves it, and it IS the digest of the one stylesheet this tree carries — while the
	// URL has quietly stopped tracking the bytes and the next theme change ships the original
	// defect. Only comparing the stored value against a fresh derivation separates them.
	if got, want := StylesheetHashedPath, hashedStylesheetPathFor(stylesheet); got != want {
		t.Errorf("StylesheetHashedPath is %q but deriving it from the embedded stylesheet yields %q. The "+
			"path is WRITTEN DOWN rather than derived, so it will not move when `app.css` does — which is "+
			"the whole property this row exists for.", got, want)
	}
	if StylesheetHashedPath == StylesheetPath {
		t.Error("the hashed path and the unversioned path are the same string, so the two rows are one row " +
			"and the ledger declares a duplicate")
	}
	// The digest is of the STYLESHEET and not of the empty string, which is what an
	// initialisation-order mistake between the `//go:embed` variable and this derivation would
	// produce — and it would produce it silently, as a perfectly well-formed path.
	if strings.Contains(StylesheetHashedPath, hashStylesheet("")) {
		t.Error("the hashed path carries the digest of the EMPTY string, so it was derived before the " +
			"embedded stylesheet was assigned. The path would be well-formed, stable and wrong: it would " +
			"never change when the theme does.")
	}
}
