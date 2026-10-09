package ui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestEveryNonPublicHTMLRowIsNoStore is clause (d) of the mobile plan's closing condition
// (`claudedocs/plan-cairn-mobile-pwa.md`, decision 8): every HTML page a non-public GET row
// answers carries `Cache-Control: no-store`, and every HTML page a PUBLIC row answers carries
// `no-cache`. `uiaudit/pwa_check.sh` runs THIS test as clause (d) and greps its own result
// line and the `pwa clause (d) no-store` tag below; it never re-implements the check.
//
// 🔴 IT WALKS THE LEDGER, SO A ROW ADDED LATER IS COVERED ON THE DAY IT IS ADDED, WITHOUT
// EDITING THIS TEST. Every GET row in [DeclaredRouteLedger] is driven bare: a non-public row
// as an AUTHENTICATED caller (or the chain answers instead of the handler, and the walk would
// measure a redirect), a public row as an ANONYMOUS one (its real audience; public rows are
// dispatched before the chain, so this reaches the handler).
//
// 🔴 A NON-PUBLIC GET ROW THAT ANSWERS NO HTML IS A FAILURE, NOT A SKIP. Every such row today
// renders a page to a bare request (`bareGETAnswer` records them all as 200), so a non-HTML
// answer means the walk could not see the header it exists to check — and a skip nobody counts
// is a pass. If a genuinely non-HTML private row is ever added, that is a decision to write
// down HERE, in the open.
//
// ⚠ THE EXPECTED VALUES ARE LITERALS, NEVER `htmlCachePrivate`/`htmlCachePublic`. Reading
// them off the implementation would make this assert `a == a`, and a constant edited to the
// wrong value would move both sides together.
//
// ⚠ WHAT IT DOES NOT WALK: the POST rows. The one POST that answers a page — the invitation
// MINT — is covered by `TestTheMintedTokenIsRenderedOnceUnderNoStoreAndNeverLogged`, and it
// reaches the header through the same [writeHTML] the GET rows here do.
func TestEveryNonPublicHTMLRowIsNoStore(t *testing.T) {
	authed := newTestServer(t, staticAuth{testIdentity()})
	anon := newTestServer(t, refusingAuth{})

	privateHTML, publicHTML, privateRows := 0, 0, 0
	for _, line := range DeclaredRouteLedger() {
		method, path, ok := splitRoute(line)
		if !ok {
			t.Fatalf("the ledger line %q does not parse, so this walk cannot drive it", line)
		}
		if method != http.MethodGet {
			continue
		}
		public := strings.Contains(ledgerClasses(line), "public")
		srv, want := authed, "no-store"
		if public {
			srv, want = anon, "no-cache"
		} else {
			privateRows++
		}

		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
		if !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/html") {
			if !public {
				t.Errorf("pwa clause (d) no-store: %s answered %d with Content-Type %q to an authenticated bare "+
					"GET, so this walk cannot see the header it exists to check. Every non-public GET row renders a "+
					"page today; a row that genuinely does not is a decision to write down in this test.",
					line, rec.Code, rec.Header().Get("Content-Type"))
			}
			continue
		}
		if public {
			publicHTML++
		} else {
			privateHTML++
		}

		// The map lookup, not `Header.Get`: `Get` cannot tell an ABSENT header from an empty one,
		// and an absent `Cache-Control` is exactly the base this clause exists to refuse.
		values, present := rec.Header()["Cache-Control"]
		if !present || len(values) != 1 || values[0] != want {
			t.Errorf("pwa clause (d) no-store: %s answered an HTML page with Cache-Control %q (present=%v), want "+
				"exactly %q. A non-public page must be `no-store` so neither the browser's HTTP cache nor a shared "+
				"cache keeps authority-narrowed content on the device; a public page is `no-cache` (decision 8).",
				line, values, present, want)
		}
	}

	// POSITIVE CONTROLS: the walk reached HTML on both sides. Zero on either would make every
	// assertion above vacuous for that side, and a ledger read that returned nothing would
	// pass with no row checked at all.
	if privateRows == 0 || privateHTML == 0 {
		t.Fatalf("pwa clause (d) no-store: the walk drove %d non-public GET row(s) and read %d HTML page(s) — "+
			"with none, the no-store assertion checked nothing", privateRows, privateHTML)
	}
	if publicHTML == 0 {
		t.Fatalf("pwa clause (d) no-store: no PUBLIC GET row answered HTML, so the no-cache half checked nothing " +
			"(the sign-in page alone should)")
	}
	t.Logf("clause (d): %d non-public GET row(s), %d of them answered HTML; %d public GET row(s) answered HTML",
		privateRows, privateHTML, publicHTML)
}
