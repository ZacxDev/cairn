package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"github.com/ZacxDev/cairn/internal/ui"
)

// joinFixtureToken is SYNTHETIC: the base64url alphabet a real token uses, and no store holds it.
const joinFixtureToken = "fixture-fragment-token_not-a-capability-0123"

// joinWorld serves the REAL fragment join page (`ui.JoinPage` with an armed provider and no query
// token — exactly what `GET /join` renders for a `#invite=` link) and the committed `join.js` at
// the path that page links, and records every request line a browser sends it plus the one POST
// body the accept form submits. Everything else answers 404.
type joinWorld struct {
	mu       sync.Mutex
	requests []string // every request line's URI, as the server received it
	posted   chan url.Values
}

func newJoinWorld(t *testing.T) (*joinWorld, *httptest.Server) {
	t.Helper()
	script, err := os.ReadFile("../internal/ui/join.js")
	if err != nil {
		t.Fatal(err)
	}
	// The served bytes ARE the embedded bytes: the page links the digest of what `ui` embeds, so a
	// disk file that differed would be a different path, and this refuses rather than serving it.
	sum := sha256.Sum256(script)
	if want := "/static/join." + hex.EncodeToString(sum[:])[:12] + ".js"; want != ui.JoinScriptPath {
		t.Fatalf("../internal/ui/join.js digests to %s but the page links %s", want, ui.JoinScriptPath)
	}
	var page strings.Builder
	if err := ui.JoinPage("", true, ui.App{}).Render(&page); err != nil {
		t.Fatal(err)
	}
	w := &joinWorld{posted: make(chan url.Values, 1)}
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		w.mu.Lock()
		w.requests = append(w.requests, r.RequestURI)
		w.mu.Unlock()
		switch {
		case r.Method == http.MethodGet && r.URL.Path == ui.JoinPath:
			rw.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = io.WriteString(rw, page.String())
		case r.Method == http.MethodGet && r.URL.Path == ui.JoinScriptPath:
			rw.Header().Set("Content-Type", "text/javascript; charset=utf-8")
			_, _ = rw.Write(script)
		case r.Method == http.MethodPost && r.URL.Path == ui.OAuthStartPath:
			_ = r.ParseForm()
			select {
			case w.posted <- r.PostForm:
			default:
			}
			_, _ = io.WriteString(rw, "started")
		default:
			http.NotFound(rw, r)
		}
	}))
	t.Cleanup(srv.Close)
	return w, srv
}

func (w *joinWorld) seen() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]string(nil), w.requests...)
}

// joinState is what the script has done to the page, read off the live DOM.
type joinState struct {
	Href          string `json:"href"`
	Field         string `json:"field"`
	AcceptHidden  bool   `json:"acceptHidden"`
	MissingHidden bool   `json:"missingHidden"`
}

const joinStateJS = `(() => ({
  href: location.href,
  field: document.getElementById("join-invite").value,
  acceptHidden: document.getElementById("join-accept").hidden,
  missingHidden: document.getElementById("join-missing").hidden,
}))()`

// TestTheJoinFragmentIsClearedAndPosted is the STATE guard over `join.js`, in a real Chromium:
// opening `/join#invite=<token>` leaves the address bar at `/join` with no fragment, puts the token
// in the accept form's field and reveals the form; clicking accept POSTs the token in the BODY to
// the start row; and NO request line the server received ever carried the token. A bare `/join`
// reveals the no-invitation sentence and leaves the form hidden — the control that the reveal is
// about the fragment.
func TestTheJoinFragmentIsClearedAndPosted(t *testing.T) {
	chromiumOrRefuse(t)
	world, srv := newJoinWorld(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	b, err := NewBrowser(ctx, srv.URL, 90*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()

	read := func() joinState {
		t.Helper()
		var s joinState
		if err := chromedp.Run(b.ctx, chromedp.Evaluate(joinStateJS, &s)); err != nil {
			t.Fatal(err)
		}
		return s
	}

	// The CONTROL: no fragment, so the script reveals the no-invitation sentence and nothing else.
	if err := chromedp.Run(b.ctx, chromedp.Navigate(srv.URL+ui.JoinPath), chromedp.WaitVisible("#join-missing", chromedp.ByID)); err != nil {
		t.Fatalf("a bare /join never revealed the no-invitation sentence: %v", err)
	}
	if s := read(); !s.AcceptHidden || s.Field != "" {
		t.Errorf("a bare /join revealed the accept form (hidden=%v) or filled its field (%q)", s.AcceptHidden, s.Field)
	}

	link := srv.URL + ui.JoinPath + "#" + url.Values{"invite": {joinFixtureToken}}.Encode()
	check := func(how string) {
		t.Helper()
		s := read()
		if s.Href != srv.URL+ui.JoinPath || strings.Contains(s.Href, joinFixtureToken) {
			t.Errorf("%s: after the script ran the address bar reads %q, want %q — the fragment was not cleared",
				how, s.Href, srv.URL+ui.JoinPath)
		}
		if s.Field != joinFixtureToken || !s.MissingHidden || s.AcceptHidden {
			t.Errorf("%s: field %q (want the fragment's token), no-invitation hidden=%v (want true), accept hidden=%v (want false)",
				how, s.Field, s.MissingHidden, s.AcceptHidden)
		}
	}
	// SAME-DOCUMENT: the whole link pasted into the tab already showing `/join` — the remedy the
	// no-invitation sentence asks for. Only the fragment differs, so the browser does NOT reload;
	// the script's `hashchange` subscription is what picks it up.
	if err := chromedp.Run(b.ctx, chromedp.Navigate(link), chromedp.WaitVisible("#join-accept", chromedp.ByID)); err != nil {
		t.Fatalf("pasting the whole link into the /join tab never revealed the accept form: %v", err)
	}
	check("same-document paste")
	// A FRESH LOAD — the ordinary click on a link.
	if err := chromedp.Run(b.ctx, chromedp.Navigate("about:blank"), chromedp.Navigate(link),
		chromedp.WaitVisible("#join-accept", chromedp.ByID)); err != nil {
		t.Fatalf("the fragment link never revealed the accept form: %v", err)
	}
	check("fresh load")

	if err := chromedp.Run(b.ctx, chromedp.Click("#join-accept button", chromedp.ByQuery)); err != nil {
		t.Fatal(err)
	}
	select {
	case form := <-world.posted:
		if form.Get("invite") != joinFixtureToken {
			t.Errorf("the accept POST's body carried invite=%q, want the fragment's token", form.Get("invite"))
		}
	case <-time.After(30 * time.Second):
		t.Fatal("clicking accept never POSTed to the start row")
	}

	seen := world.seen()
	// POSITIVE CONTROL on the request log: it recorded the page, the script and the POST, so the
	// absence below is about the token and not about a log wired to nothing.
	for _, want := range []string{ui.JoinPath, ui.JoinScriptPath, ui.OAuthStartPath} {
		found := false
		for _, r := range seen {
			found = found || r == want
		}
		if !found {
			t.Fatalf("the server never saw a request for %s (saw %v), so the check below reads nothing", want, seen)
		}
	}
	for _, r := range seen {
		if strings.Contains(r, joinFixtureToken) || strings.Contains(r, "invite") {
			t.Errorf("a request line carried the token: %q — an access log at any hop would hold it", r)
		}
	}
}
