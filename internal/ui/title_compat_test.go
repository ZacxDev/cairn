package ui

import (
	"html"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

// crawlPages renders every HTML page reachable from the route ledger's GET rows by following
// same-origin links, plus the two public frames no bare GET row reaches (a refused sign-in, and
// the join page WITH a token) — so every frame this package has is in the set, and the scope,
// entry, arc and invite pages a bare row cannot name are reached through the links that name them.
//
// ⚠ IT USES ONLY `htmlRows`, `fetch` AND `splitRoute`, which exist on the commit before the
// instance label, so the file it lives in compiles there and its invariant guard can be RUN there.
func crawlPages(t *testing.T, srv *Server) map[string]string {
	t.Helper()
	out := map[string]string{}
	var queue []string
	for row := range htmlRows(t, srv) {
		_, path, _ := splitRoute(row)
		queue = append(queue, path)
	}
	href := regexp.MustCompile(`href="(/[^"]*)"`)
	seen := map[string]bool{}
	for len(queue) > 0 && len(seen) < 400 {
		p := queue[0]
		queue = queue[1:]
		if seen[p] || strings.HasPrefix(p, "/static") || strings.HasPrefix(p, "/manifest") {
			continue
		}
		seen[p] = true
		rec := fetch(srv, p, nil)
		if !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/html") {
			continue
		}
		out["GET "+p] = rec.Body.String()
		for _, m := range href.FindAllStringSubmatch(rec.Body.String(), -1) {
			queue = append(queue, html.UnescapeString(m[1]))
		}
	}
	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, SignInPath, strings.NewReader("token=wrong"))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Origin", "http://"+r.Host)
	srv.ServeHTTP(rec, r)
	out["POST /sign-in (refused)"] = rec.Body.String()
	out["GET /join?token=…"] = fetch(srv, JoinPath+"?token="+fixtureInviteToken, nil).Body.String()
	return out
}

var (
	titleElement = regexp.MustCompile(`<title>([^<]*)</title>`)
	headingOne   = regexp.MustCompile(`<h1>.*?</h1>`)
)

// TestAnUnlabelledDeploymentRendersTodaysTitlesAndHeader: with no instance label, every page's
// `<title>` is `cairn` or `cairn — <page>` and its heading is the bare wordmark — the shapes every
// page had before the label existed.
//
// ⚠ AN INVARIANT GUARD, LABELLED AS ONE: GREEN on the commit before `-instance-name` existed, by
// design. What it guards is that the label is INERT when unset. The page-for-page byte comparison
// against that commit (19 pages, stylesheet URL normalised) was a one-off measurement recorded in
// the PR, not a test, because a golden copy of every page would go red on every unrelated edit.
func TestAnUnlabelledDeploymentRendersTodaysTitlesAndHeader(t *testing.T) {
	srv := newTestServer(t, staticAuth{testIdentity()})
	pages := crawlPages(t, srv)
	if len(pages) < 15 {
		t.Fatalf("the crawl reached only %d page(s), so the assertions below cover almost nothing", len(pages))
	}
	t.Logf("%d unlabelled page(s) crawled", len(pages))
	for row, body := range pages {
		titles := titleElement.FindAllStringSubmatch(body, -1)
		if len(titles) != 1 {
			t.Errorf("%s: %d <title> elements, want 1", row, len(titles))
			continue
		}
		if title := titles[0][1]; title != "cairn" && !strings.HasPrefix(title, "cairn — ") {
			t.Errorf("%s: title %q is neither \"cairn\" nor \"cairn — <page>\"", row, title)
		}
		h1 := headingOne.FindString(body)
		if h1 != `<h1><a href="/">cairn</a></h1>` && h1 != `<h1>cairn</h1>` {
			t.Errorf("%s: heading %q is not the bare wordmark", row, h1)
		}
		if strings.Contains(body, "instance-name") {
			t.Errorf("%s: an unlabelled deployment rendered an instance element", row)
		}
	}
}
