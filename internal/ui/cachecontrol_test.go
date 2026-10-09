package ui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ZacxDev/cairn/internal/control"
	"github.com/ZacxDev/cairn/internal/report"
)

// realPage is, for every CONTENT GET row, the query that reaches its REAL page against
// `walkSource`'s world, and a string only that page renders.
//
// 🔴 IT EXISTS BECAUSE A BARE GET MEASURED THE WRONG PAGE FOR FOUR ROWS. `/scope`, `/entry`,
// `/arc` and `/session` answer a parameterless request with the NAVIGATE page (`bareGETAnswer`
// says why), so a walk that drove them bare checked `NavigatePage`'s header four times and
// `ScopePage`, `EntryPage`, `ArcPage` and `SessionPage` never: an audit switched each of those
// four pages to a different header writer and this test stayed green.
//
// 🔴 HAND-WRITTEN, AND A CONTENT ROW MISSING FROM IT FAILS — `bareGETAnswer`'s mechanism, for its
// reason: what "the real page" is per row cannot be derived from the response without asserting
// `a == a`. Each marker is checked ABSENT from the navigate page this world renders, so a fixture
// that silently falls back to navigate goes red instead of measuring the fallback.
var realPage = map[string]struct{ query, marker string }{
	"GET / content":        {"", `class="scope-grid"`},
	"GET /scope content":   {"?" + QueryID + "=" + string(fixtureScope), `<h2>platform</h2>`},
	"GET /entry content":   {"?" + QueryScope + "=" + string(fixtureScope) + "&" + QueryRef + "=runbook", `<h2>runbook</h2>`},
	"GET /arc content":     {"?" + QueryHome + "=" + string(fixtureScope) + "&" + QuerySlug + "=walk-arc", `<h2>Arc</h2>`},
	"GET /arcs content":    {"", `id="arcs-index"`},
	"GET /session content": {"?" + QuerySession + "=" + walkSession, `id="session-summary"`},
	"GET /share content":   {"", `<title>cairn — sharing`},
	"GET /invite content":  {"", `class="invite-honesty"`},
}

// walkSession is the one session id `walkSource` answers as FOUND.
const walkSession = "s-walk-01"

// walkSource is the dispatch fixture with ONE difference: a session that is found, so `GET
// /session` renders `SessionPage` rather than the plain-text unseen refusal `staticSource` gives.
type walkSource struct{ staticSource }

func (s walkSource) Session(_ control.Authorization, session string) (SessionAnswer, error) {
	rep := report.SessionAcrossReport{ID: session}
	if session == walkSession {
		rep.Arcs = []report.SessionArc{{Home: "platform", Slug: "walk-arc", Status: "open", Role: "member"}}
	}
	return SessionAnswer{Report: rep}, nil
}

// TestEveryNonPublicHTMLRowIsNoStore is clause (d) of the mobile plan's closing condition
// (`claudedocs/plan-cairn-mobile-pwa.md`): every HTML page a GET row answers carries
// `Cache-Control: no-store`. The name is the plan's; since audit round 0 the PUBLIC rows are held
// to the same value (one writer, one value — `writeHTML`), so it walks both. `uiaudit/pwa_check.sh`
// runs THIS test as clause (d) and greps its result line and the `pwa clause (d) no-store` tag
// below; it never re-implements the check.
//
// WHAT IT DRIVES, exactly: every GET row in [DeclaredRouteLedger]. A CONTENT row is driven at its
// REAL page through [realPage] (its query and its marker), as an authenticated caller; any other
// non-public GET row bare, authenticated; a public row bare and ANONYMOUS (its audience; public
// rows are dispatched before the chain). A new non-content or public row is covered without
// editing this test; a new CONTENT row fails until [realPage] says how to reach its real page.
//
// 🔴 A NON-PUBLIC GET ROW THAT ANSWERS NO HTML IS A FAILURE, NOT A SKIP: the walk could not see the
// header it exists to check, and a skip nobody counts is a pass. A public row that answers no HTML
// (a stylesheet, an icon, the manifest) is not a page and is not this clause's.
//
// ⚠ WHAT IT DOES NOT DRIVE: the POST rows, and every page a row renders under a query OTHER than
// the one above — a refusal, another tab, the raw view, a search. Those reach the header through
// the same [writeHTML], which is the property that makes one query per row sufficient; it is a
// structural argument, not a measurement of each of them. The one POST that answers a page — the
// invitation MINT — is pinned by `TestTheMintedTokenIsRenderedOnceUnderNoStoreAndNeverLogged`.
//
// ⚠ THE EXPECTED VALUE IS A LITERAL, NEVER `htmlCacheControl`: reading it off the implementation
// would make this assert `a == a`.
func TestEveryNonPublicHTMLRowIsNoStore(t *testing.T) {
	cfg := testConfig(t, staticAuth{testIdentity()})
	cfg.Source = walkSource{staticSource{scopes: benignWorld()}}
	authed, err := New(cfg)
	if err != nil {
		t.Fatalf("the server did not build: %v", err)
	}
	anon := newTestServer(t, refusingAuth{})

	// The navigate page this world renders: every marker must be ABSENT from it, or a row that fell
	// back to navigate would still show its marker and the reached-the-real-page check would be blind.
	var nav strings.Builder
	if err := NavigatePage(PageView{Viewer: testIdentity().Principal.Display, Scopes: benignWorld()}).Render(&nav); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(nav.String(), "Pick a scope") {
		t.Fatalf("the navigate page no longer says %q, so the marker control below reads the wrong page", "Pick a scope")
	}
	for route, rp := range realPage {
		if strings.Contains(nav.String(), rp.marker) {
			t.Errorf("pwa clause (d) no-store: %s's marker %q also appears on the NAVIGATE page, so it cannot tell "+
				"the real page from the fallback", route, rp.marker)
		}
	}

	privateHTML, publicHTML, privateRows, realPages := 0, 0, 0, 0
	for _, line := range DeclaredRouteLedger() {
		method, path, ok := splitRoute(line)
		if !ok {
			t.Fatalf("the ledger line %q does not parse, so this walk cannot drive it", line)
		}
		if method != http.MethodGet {
			continue
		}
		classes := ledgerClasses(line)
		public := strings.Contains(classes, "public")
		srv, target, marker := authed, path, ""
		switch {
		case public:
			srv = anon
		case strings.Contains(classes, "content"):
			rp, declared := realPage[line]
			if !declared {
				t.Errorf("pwa clause (d) no-store: %s is a content row and `realPage` does not say which query reaches "+
					"its REAL page. A bare GET may render the navigate fallback, which is a different page.", line)
				continue
			}
			target, marker = path+rp.query, rp.marker
		}
		if !public {
			privateRows++
		}

		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, httptest.NewRequest(method, target, nil))
		if !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/html") {
			if !public {
				t.Errorf("pwa clause (d) no-store: GET %s answered %d with Content-Type %q, so this walk cannot see the "+
					"header it exists to check. A non-public GET row that genuinely renders no page is a decision to "+
					"write down in this test.", target, rec.Code, rec.Header().Get("Content-Type"))
			}
			continue
		}
		if marker != "" {
			if !strings.Contains(rec.Body.String(), marker) {
				t.Errorf("pwa clause (d) no-store: GET %s answered %d without its real page's marker %q — the walk "+
					"measured some OTHER page (a navigate fallback or a refusal), not the one this row exists for",
					target, rec.Code, marker)
				continue
			}
			realPages++
		}
		if public {
			publicHTML++
		} else {
			privateHTML++
		}

		// The map lookup, not `Header.Get`: `Get` cannot tell an ABSENT header from an empty one.
		values, present := rec.Header()["Cache-Control"]
		if !present || len(values) != 1 || values[0] != "no-store" {
			t.Errorf("pwa clause (d) no-store: GET %s (%s) answered an HTML page with Cache-Control %q (present=%v), "+
				"want exactly \"no-store\" — so neither the browser's HTTP cache nor a shared one keeps it on the device",
				target, line, values, present)
		}
	}

	// POSITIVE CONTROLS: the walk reached HTML on both sides and every content row's real page.
	if privateRows == 0 || privateHTML == 0 {
		t.Fatalf("pwa clause (d) no-store: the walk drove %d non-public GET row(s) and read %d HTML page(s) — with "+
			"none, the assertion checked nothing", privateRows, privateHTML)
	}
	if publicHTML == 0 {
		t.Fatalf("pwa clause (d) no-store: no PUBLIC GET row answered HTML, so the public half checked nothing " +
			"(the sign-in page alone should)")
	}
	if realPages != len(realPage) {
		t.Errorf("pwa clause (d) no-store: %d of %d content rows reached their real page", realPages, len(realPage))
	}
	t.Logf("clause (d): %d non-public GET row(s) (%d real content pages), %d answered HTML; %d public GET row(s) answered HTML",
		privateRows, realPages, privateHTML, publicHTML)
}
