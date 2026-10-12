//go:build pgtest

// The browser's MEMOS TAB (S5 of `claudedocs/plan-cairn-scope-mail.md`) on the REAL binary over a
// real PostgreSQL: `internal/ui`'s handlers through `wireBrowserMemos`' adapter into the real
// `pgstore.MemoStore`, behind a real browser sign-in. `internal/ui`'s own tests drive the same
// handlers over a fake store that asks the same predicates; this is where the REAL store's refusal is
// measured, and where the wiring that connects the two is.
package main

import (
	"context"
	"database/sql"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ZacxDev/cairn/internal/control"
	"github.com/ZacxDev/cairn/internal/identity"
	"github.com/ZacxDev/cairn/internal/pgstore"
	"github.com/ZacxDev/cairn/internal/ui"
)

// memoTabStore is a store root holding the journal world's two scopes, one synthetic entry each, so
// the scope page finds them.
func memoTabStore(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, scope := range []string{"alpha-notes", "beta-notes"} {
		dir := filepath.Join(root, scope)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		body := "---\nservice: kiln\nscope: " + scope + "\n---\n\n## What it is\n\nsynthetic.\n"
		if err := os.WriteFile(filepath.Join(dir, "kiln.md"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// browserSession signs in on the child's browser listener with `token` and returns the Cookie header
// and the CSRF token a page of that session carries.
func browserSession(t *testing.T, browser, token string) (cookie, csrf string) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, browser+ui.SignInPath, strings.NewReader(url.Values{ui.FieldToken: {token}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", browser)
	resp, err := noFollow().Do(req)
	if err != nil {
		t.Fatalf("signing in: %v", err)
	}
	resp.Body.Close()
	var parts []string
	for _, ck := range resp.Cookies() {
		parts = append(parts, ck.Name+"="+ck.Value)
		if ck.Name == identity.SessionCookieName {
			csrf = identity.CSRFTokenFor(ck.Value)
		}
	}
	if resp.StatusCode != http.StatusSeeOther || csrf == "" {
		t.Fatalf("sign-in answered %d with no session cookie", resp.StatusCode)
	}
	return strings.Join(parts, "; "), csrf
}

func noFollow() *http.Client {
	return &http.Client{Timeout: 20 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

func browserGet(t *testing.T, browser, cookie, path string) string {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, browser+path, nil)
	req.Header.Set("Cookie", cookie)
	resp, err := noFollow().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var b strings.Builder
	buf := make([]byte, 32<<10)
	for {
		n, err := resp.Body.Read(buf)
		b.Write(buf[:n])
		if err != nil {
			break
		}
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s answered %d: %s", path, resp.StatusCode, b.String())
	}
	return b.String()
}

// browserPost is a same-origin form POST carrying the session's CSRF token — the tab's own forms.
func browserPost(t *testing.T, browser, cookie, csrf, path string, form url.Values) *http.Response {
	t.Helper()
	form.Set("csrf", csrf)
	req, _ := http.NewRequest(http.MethodPost, browser+path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", browser)
	req.Header.Set("Cookie", cookie)
	resp, err := noFollow().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	return resp
}

func memoCount(t *testing.T, dsn string) int {
	t.Helper()
	db, err := sql.Open(pgstore.DriverName, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM memos`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// TestTheMemoTabsForgedSendIsRefusedByTheRealStore — the S5 test plan's read-only viewer, end to end
// on the real binary: reader-b (a READ grant on alpha-notes) is shown the memos tab and NO send form;
// a send forged by hand with BOTH cross-site gates satisfied is refused by the real store's
// `memo.MaySend` (`no-write`) and the `memos` table does not move. POSITIVE CONTROL: writer-a's
// identical request is stored, and reader-b then sees it on the tab. Also the startup line's
// `memos in postgres`, read off the wired value.
func TestTheMemoTabsForgedSendIsRefusedByTheRealStore(t *testing.T) {
	journal, _ := memoJournal(t)
	dsn := schemaDSN(t)
	c := startMemoChild(t, journal, dsn, "-store", memoTabStore(t))
	browser := c.serving(t)
	if !strings.Contains(c.out.String(), "memos in postgres") {
		t.Fatalf("the startup line does not announce the memo store:\n%s", c.out.String())
	}
	tab := ui.ScopePath + "?" + url.Values{ui.QueryID: {string(capiAlpha)}, ui.QueryTab: {ui.TabMemos}}.Encode()
	form := url.Values{ui.FieldScope: {string(capiAlpha)}, ui.FieldMemoSubject: {"kiln schedule"},
		ui.FieldMemoBody: {"the second firing moves"}, ui.FieldTTLDays: {"3"}}

	readerCookie, readerCSRF := browserSession(t, browser, capiTokReader)
	page := browserGet(t, browser, readerCookie, tab)
	if !strings.Contains(page, `id="scope-memos"`) || strings.Contains(page, `action="`+ui.MemoPath+`"`) {
		t.Fatalf("reader-b is not shown the memos tab WITHOUT a send form:\n%s", page)
	}

	before := memoCount(t, dsn)
	resp := browserPost(t, browser, readerCookie, readerCSRF, ui.MemoPath, cloneValues(form))
	loc, _ := url.Parse(resp.Header.Get("Location"))
	if resp.StatusCode != http.StatusSeeOther || loc == nil || loc.Query().Get("outcome") != "memo-refused" ||
		loc.Query().Get(ui.QueryMemoReason) != "no-write" {
		t.Fatalf("reader-b's forged send answered %d Location=%q, want a 303 refusing it as no-write",
			resp.StatusCode, resp.Header.Get("Location"))
	}
	if after := memoCount(t, dsn); after != before {
		t.Fatalf("the refused send stored a memo: %d → %d rows", before, after)
	}

	// POSITIVE CONTROL: the same request from writer-a, the owner, is stored.
	writerCookie, writerCSRF := browserSession(t, browser, capiTokWriter)
	if !strings.Contains(browserGet(t, browser, writerCookie, tab), `action="`+ui.MemoPath+`"`) {
		t.Fatal("POSITIVE CONTROL FAILED: writer-a is not shown the send form")
	}
	resp = browserPost(t, browser, writerCookie, writerCSRF, ui.MemoPath, cloneValues(form))
	loc, _ = url.Parse(resp.Header.Get("Location"))
	if resp.StatusCode != http.StatusSeeOther || loc == nil || loc.Query().Get("outcome") != "memo-sent" {
		t.Fatalf("POSITIVE CONTROL FAILED: writer-a's send answered %d Location=%q", resp.StatusCode, resp.Header.Get("Location"))
	}
	if after := memoCount(t, dsn); after != before+1 {
		t.Fatalf("POSITIVE CONTROL FAILED: writer-a's send left %d → %d rows", before, after)
	}
	page = browserGet(t, browser, readerCookie, tab)
	if !strings.Contains(page, "kiln schedule") || !strings.Contains(page, "writer-a (user)") {
		t.Fatalf("reader-b does not see writer-a's memo, with its sender line, on the tab:\n%s", page)
	}
	// A browser session sends with NO credential, so the sender line carries no `via` label.
	if strings.Contains(page, "writer-a via") {
		t.Errorf("a browser send was labelled with a credential:\n%s", page)
	}
}

// memoRowState is one `memos` row as the database holds it: whether it is retracted, and whether its
// subject and body are still there.
func memoRowState(t *testing.T, dsn string, id int64) (retracted, hasText bool) {
	t.Helper()
	db, err := sql.Open(pgstore.DriverName, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.QueryRow(`SELECT retracted_at IS NOT NULL, subject IS NOT NULL OR body IS NOT NULL FROM memos WHERE id = $1`, id).
		Scan(&retracted, &hasText); err != nil {
		t.Fatalf("reading memo %d: %v", id, err)
	}
	return retracted, hasText
}

// TestTheMemosTabRetractsThroughTheRealStoreAndShowsTheTombstone — the adapter's other two methods
// (#242 round 1): `browserMemos.All` and `browserMemos.Retract` had never been driven against the
// real store, so an adapter that listed only LIVE memos and one whose `Retract` did nothing both left
// every Go test and this tier green. On the REAL binary over a real PostgreSQL, as writer-a:
//
//   - send through the tab's own form; the page shows the memo with a Retract form naming its id
//     (the id read from the database, so the form is checked against the row, not against itself);
//   - CONTROL, before retracting: the row's `retracted_at` is NULL and its text is there;
//   - `POST /memo/retract` answers the `memo-retracted` redirect;
//   - the ROW: `retracted_at` set, subject and body NULLed — the retraction happened in the store,
//     which a redirect alone cannot show;
//   - the TAB, re-read: the memo is still listed, as its TOMBSTONE line naming the retractor, with
//     no subject, no body and no Retract form — for writer-a AND for reader-b.
//
// RED with `browserMemos.Retract` returning nil without calling the store (the ROW assertion), with
// `browserMemos.All` returning nothing, and with `pgstore.MemoStore.All` selecting only live rows
// (the TOMBSTONE assertion, each).
func TestTheMemosTabRetractsThroughTheRealStoreAndShowsTheTombstone(t *testing.T) {
	journal, _ := memoJournal(t)
	dsn := schemaDSN(t)
	c := startMemoChild(t, journal, dsn, "-store", memoTabStore(t))
	browser := c.serving(t)
	tab := ui.ScopePath + "?" + url.Values{ui.QueryID: {string(capiAlpha)}, ui.QueryTab: {ui.TabMemos}}.Encode()
	const subject, body = "glaze batch seven", "cured overnight; safe to load"

	cookie, csrf := browserSession(t, browser, capiTokWriter)
	resp := browserPost(t, browser, cookie, csrf, ui.MemoPath, url.Values{ui.FieldScope: {string(capiAlpha)},
		ui.FieldMemoSubject: {subject}, ui.FieldMemoBody: {body}, ui.FieldTTLDays: {"5"}})
	if loc, _ := url.Parse(resp.Header.Get("Location")); resp.StatusCode != http.StatusSeeOther || loc == nil || loc.Query().Get("outcome") != "memo-sent" {
		t.Fatalf("could not vouch: writer-a's send answered %d Location=%q", resp.StatusCode, resp.Header.Get("Location"))
	}
	var id int64
	func() {
		db, err := sql.Open(pgstore.DriverName, dsn)
		if err != nil {
			t.Fatal(err)
		}
		defer db.Close()
		if err := db.QueryRow(`SELECT id FROM memos WHERE subject = $1`, subject).Scan(&id); err != nil {
			t.Fatalf("could not vouch: the sent memo's row: %v", err)
		}
	}()
	retractForm := `name="` + ui.FieldMemoID + `" value="` + strconv.FormatInt(id, 10) + `"`
	page := browserGet(t, browser, cookie, tab)
	if !strings.Contains(page, subject) || !strings.Contains(page, body) || !strings.Contains(page, retractForm) {
		t.Fatalf("writer-a's tab does not show the sent memo with a Retract form for id %d:\n%s", id, page)
	}
	// CONTROL: live before the retraction, so the assertions after it measure a change.
	if retracted, hasText := memoRowState(t, dsn, id); retracted || !hasText {
		t.Fatalf("CONTROL FAILED: before retracting, the row is retracted=%v hasText=%v", retracted, hasText)
	}

	resp = browserPost(t, browser, cookie, csrf, ui.MemoRetractPath, url.Values{ui.FieldScope: {string(capiAlpha)},
		ui.FieldMemoID: {strconv.FormatInt(id, 10)}})
	if loc, _ := url.Parse(resp.Header.Get("Location")); resp.StatusCode != http.StatusSeeOther || loc == nil || loc.Query().Get("outcome") != "memo-retracted" {
		t.Fatalf("the retraction answered %d Location=%q, want the memo-retracted redirect", resp.StatusCode, resp.Header.Get("Location"))
	}
	if retracted, hasText := memoRowState(t, dsn, id); !retracted || hasText {
		t.Fatalf("after the tab's retraction the ROW is retracted=%v hasText=%v, want retracted with its text gone — "+
			"the tab reported a retraction the store did not make", retracted, hasText)
	}

	readerCookie, _ := browserSession(t, browser, capiTokReader)
	for who, ck := range map[string]string{"writer-a": cookie, "reader-b": readerCookie} {
		page := browserGet(t, browser, ck, tab)
		if !strings.Contains(page, `data-memo-id="`+strconv.FormatInt(id, 10)+`"`) ||
			!strings.Contains(page, `data-memo="tombstone"`) || !strings.Contains(page, "retracted by writer-a (user) at ") {
			t.Errorf("%s: the retracted memo is not listed as its tombstone naming the retractor:\n%s", who, page)
		}
		if strings.Contains(page, subject) || strings.Contains(page, body) || strings.Contains(page, retractForm) {
			t.Errorf("%s: the tombstone still shows the memo's text or a Retract form:\n%s", who, page)
		}
	}
}

func cloneValues(v url.Values) url.Values {
	out := url.Values{}
	for k, vs := range v {
		out[k] = append([]string(nil), vs...)
	}
	return out
}

// TestWireBrowserMemosNeedsADatabaseAndAJournal — the wiring's two conditions, over a real database:
// no journal → the INTERFACE nil (never a typed nil `main` would announce as a store), and both →
// a store whose model is the authority's.
func TestWireBrowserMemosNeedsADatabaseAndAJournal(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db, err := pgstore.Open(ctx, schemaDSN(t))
	if err != nil {
		t.Fatalf("could not vouch: opening the tier's database: %v", err)
	}
	defer db.Close()
	_, fs := memoJournal(t)
	cache := control.NewCache(fs, control.CacheOptions{})
	if err := cache.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	if m, err := wireBrowserMemos(db, "", cache); err != nil || m != nil {
		t.Errorf("no journal wired %v (%v), want the interface nil", m, err)
	}
	m, err := wireBrowserMemos(db, "a-journal-path", cache)
	if err != nil || m == nil {
		t.Fatalf("a database and a journal wired %v (%v), want a store", m, err)
	}
	if _, ok := m.Model().PrincipalFor(control.KindUser, capiWriter); !ok {
		t.Error("the wired store's model is not the authority's: writer-a does not resolve")
	}
}
