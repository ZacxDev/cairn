package ui

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/ZacxDev/cairn/internal/control"
	"github.com/ZacxDev/cairn/internal/identity"
	"github.com/ZacxDev/cairn/internal/invite"
)

// 🔴 THE TEAM-LINK TOKEN TRAVELS IN THE URL FRAGMENT (operator decision): a REUSABLE team-link
// token must never travel in a URL a server receives, so no access log at any hop can hold one.
//
// Every test here reads only symbols that existed BEFORE the change, plus `join.js` from DISK,
// so each compiles on the pre-change tree and its RED there is an assertion failing rather than a
// build error. The served script is compared against the same disk bytes, so the guard below
// reads exactly what a browser receives.

// joinScriptFromDisk is `join.js` as committed — what the guards below read.
func joinScriptFromDisk(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("join.js")
	if err != nil {
		t.Fatalf("join.js is not on disk (%v): the join page has no script to move the fragment into the form", err)
	}
	if len(b) == 0 {
		t.Fatal("join.js is EMPTY, so every guard below reads nothing")
	}
	return string(b)
}

// joinDigestFromBytes is the stylesheet discipline for the join script: the 12-hex digest
// recomputed here from the committed file, never read off `hashAsset`.
func joinDigestFromBytes(t *testing.T) string {
	t.Helper()
	sum := sha256.Sum256([]byte(joinScriptFromDisk(t)))
	return hex.EncodeToString(sum[:])[:12]
}

// joinServer is a server whose provider is ARMED (the join page renders its accept form only
// then) over the given invitation half, with the exchange left for the caller to steer.
func joinServer(t *testing.T, inviting Inviting) (*Server, *stubOAuth) {
	t.Helper()
	cfg := testConfig(t, refusingAuth{})
	stub := &stubOAuth{}
	cfg.OAuth = stub
	cfg.Inviting = inviting
	srv, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return srv, stub
}

var (
	startFormRe   = regexp.MustCompile(`(?s)<form[^>]*action="` + regexp.QuoteMeta(OAuthStartPath) + `"[^>]*>(.*?)</form>`)
	hiddenFieldRe = regexp.MustCompile(`<input type="hidden" name="([^"]+)"[^>]*?value="([^"]*)"`)
	scriptSrcRe   = regexp.MustCompile(`<script src="([^"]+)"`)
)

// acceptFormFields is the accept form's hidden fields, read off a RENDERED join page — which is
// what a browser with script off would post. Nil when the page carries no accept form.
func acceptFormFields(page string) url.Values {
	m := startFormRe.FindStringSubmatch(page)
	if m == nil {
		return nil
	}
	out := url.Values{}
	for _, f := range hiddenFieldRe.FindAllStringSubmatch(m[1], -1) {
		out.Add(f[1], htmlUnescape(f[2]))
	}
	return out
}

func htmlUnescape(s string) string {
	return strings.NewReplacer("&amp;", "&", "&#34;", `"`, "&lt;", "<", "&gt;", ">", "&#39;", "'").Replace(s)
}

// startWith posts `form` to the start row as a same-origin browser would and returns the flight
// cookie.
func startWith(t *testing.T, srv *Server, form url.Values) *http.Cookie {
	t.Helper()
	req := httptest.NewRequest("POST", OAuthStartPath, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", "http://"+req.Host)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("the start row answered %d, want 303: %s", rec.Code, rec.Body.String())
	}
	for _, c := range rec.Result().Cookies() {
		if c.Name == oauthFlightCookieName {
			return c
		}
	}
	t.Fatal("the start row set no flight cookie")
	return nil
}

func joinCallback(srv *Server, cookie *http.Cookie) *httptest.ResponseRecorder {
	cb := httptest.NewRequest("GET", OAuthCallbackPath+"?code=fixture-code", nil)
	cb.AddCookie(cookie)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, cb)
	return rec
}

// followTheQueryLink is the WHOLE legacy path a browser takes for `/join?invite=<token>`: open
// the page, post the accept form exactly as rendered, and come back through the callback.
func followTheQueryLink(t *testing.T, srv *Server, token string) *httptest.ResponseRecorder {
	t.Helper()
	page := fetch(srv, JoinPath+"?"+url.Values{inviteTokenField: {token}}.Encode(), nil)
	if page.Code != http.StatusOK {
		t.Fatalf("GET /join?invite=… answered %d", page.Code)
	}
	form := acceptFormFields(page.Body.String())
	if form == nil {
		t.Fatalf("the query-link join page renders no accept form: %s", page.Body.String())
	}
	carried := false
	for _, vs := range form {
		carried = carried || slices.Contains(vs, token)
	}
	if !carried {
		t.Fatalf("the accept form does not carry the query token at all (%v), so the case below measures nothing", form)
	}
	return joinCallback(srv, startWith(t, srv, form))
}

// TestNoMintedLinkCarriesItsTokenInAQueryString — every mint, of every kind (an invitation, a
// single-use team link, a reusable one), renders `/join#invite=<token>` and never `?invite=`.
// RED before the change: both mint handlers built `JoinPath + "?" + …`.
func TestNoMintedLinkCarriesItsTokenInAQueryString(t *testing.T) {
	wantInvite := JoinPath + "#" + url.Values{inviteTokenField: {fixtureInviteToken}}.Encode()
	wantLink := JoinPath + "#" + url.Values{inviteTokenField: {fixtureLinkToken}}.Encode()

	rig := newInviteRig(t, nil)
	inv := rig.post(InvitePath, url.Values{FieldProject: {string(fixtureNamedProject.ID)}, FieldRole: {string(control.RoleMember)}})
	pages := map[string]string{"an invitation": inv.Body.String()}
	if inv.Code != http.StatusOK {
		t.Fatalf("the invitation mint answered %d: %s", inv.Code, inv.Body.String())
	}
	for name, reuse := range map[string]bool{"a single-use team link": false, "a reusable team link": true} {
		trig, _ := newTeamHTTPRig(t, nil)
		form := linkForm(fixtureScopeTarget())
		if reuse {
			form.Set(FieldReuse, "1")
		}
		rec := trig.post(TeamLinkPath, form)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: the mint answered %d: %s", name, rec.Code, rec.Body.String())
		}
		pages[name] = rec.Body.String()
	}
	for name, body := range pages {
		if strings.Contains(body, JoinPath+"?") || strings.Contains(body, "?"+inviteTokenField+"=") {
			t.Errorf("%s: the minted page carries a QUERY-STRING join link; every server that sees it logs the token", name)
		}
		want := wantLink
		if name == "an invitation" {
			want = wantInvite
		}
		// POSITIVE CONTROL: the page DOES carry the link, in the fragment — so the absence above is
		// about the shape, not about a page that rendered no link at all.
		if strings.Count(body, want) != 1 {
			t.Errorf("%s: the fragment link %q appears %d time(s), want exactly once", name, want, strings.Count(body, want))
		}
	}
}

// TestAReusableTeamLinkInTheQUERYStringIsRefusedAndNotRedeemed — the transition's refusal, over
// the REAL `ControlInviting`/`ControlTeamLinks`, the real dispatcher and the real flight, driven
// from the RENDERED legacy page. For a stranger it is the uniform 401 a dead link gets; for a
// known user the sign-in completes (the house rule: a failed redemption never locks an existing
// user out) and nothing is joined. Either way the link's redemption log stays EMPTY.
//
// RED before the change: `/join?invite=` rendered the token into the ordinary field and the
// callback redeemed it. The POSITIVE CONTROL is the fragment path — the SAME token posted in the
// body field `join.js` fills — which must redeem, so the zero above is not a harness wired to
// nothing.
func TestAReusableTeamLinkInTheQUERYStringIsRefusedAndNotRedeemed(t *testing.T) {
	for _, who := range []string{"a stranger", "a known user"} {
		t.Run(who, func(t *testing.T) {
			r := newTeamRig(t)
			token, link := r.mint(invOwner, invite.LinkReader, true, scopeT(tlA))
			srv, stub := joinServer(t, r.inviting)
			var logBuf bytes.Buffer
			srv.log = &logBuf
			before := ""
			if who == "a stranger" {
				stub.err = &identity.UnprovisionedSubject{Provider: "fixture-provider", Subject: strangerSubject(1)}
			} else {
				stub.principal = r.principal(tlAdminOnly)
				before = r.authorityOf(tlAdminOnly)
			}

			rec := followTheQueryLink(t, srv, token)

			if log, _ := r.links.LinkRedemptions(link.Digest); len(log) != 0 {
				t.Errorf("a REUSABLE link arriving in a query string was REDEEMED (%d row(s)). Accepting it is what "+
					"keeps honest browsers following the logged `?invite=` shape", len(log))
			}
			// The refusal's log line must name the remedy and who holds it, and must NOT claim the
			// token is retired — the positive control below redeems that very token. Pinned as the
			// WHOLE fixed text plus the digest, so a reword back to "stops working" fails here.
			wantLine := "cairn-ui: " + queryBorneReusableRefusedLog + ": link=" + shortDigest(link.Digest) + " ("
			if !strings.Contains(logBuf.String(), wantLine) {
				t.Errorf("the refusal log line is missing or reworded; want a line starting %q, got:\n%s", wantLine, logBuf.String())
			}
			if who == "a stranger" {
				if rec.Code != http.StatusUnauthorized || !strings.Contains(pageText(rec.Body.String()), signInRefused) {
					t.Errorf("the stranger got %d %q, want the uniform 401 %q", rec.Code, pageText(rec.Body.String()), signInRefused)
				}
				if sessionWasOpenedFor(rec, "") {
					t.Error("a session was opened for a stranger whose only way in was a refused link")
				}
			} else {
				if rec.Code != http.StatusSeeOther {
					t.Errorf("the known user's sign-in answered %d, want 303 — a refused link must not lock them out", rec.Code)
				}
				if after := r.authorityOf(tlAdminOnly); after != before {
					t.Errorf("the known user's authority moved %q -> %q through a refused query-string link", before, after)
				}
			}

			// POSITIVE CONTROL: the fragment path — the body field the script fills — redeems the SAME link.
			// 🔴 It is ALSO the measured residual: this token is the one that "travelled in a query
			// string", and posting it unmarked redeems it. The query refusal retires the logged SHAPE,
			// not the logged TOKEN; only the minter's revoke (or expiry) does that.
			ok := joinCallback(srv, startAFlightCarryingAnInvite(t, srv, token))
			if ok.Code != http.StatusSeeOther {
				t.Fatalf("the fragment path answered %d: %s", ok.Code, ok.Body.String())
			}
			if log, _ := r.links.LinkRedemptions(link.Digest); len(log) != 1 {
				t.Fatalf("the fragment path left %d redemption row(s), want 1 — the zero above is about the harness", len(log))
			}
		})
	}
}

// TestASingleUseTokenInTheQUERYStringStillRedeems — the transition's acceptance: links already
// sent as `?invite=` keep working for a single-use INVITATION and a single-use TEAM LINK.
// ⚠ AN INVARIANT GUARD, LABELLED AS ONE: green before the change too. It is the half that stops
// the refusal above being implemented as "refuse every query token".
func TestASingleUseTokenInTheQUERYStringStillRedeems(t *testing.T) {
	t.Run("an invitation", func(t *testing.T) {
		r := newTeamRig(t)
		token, _, err := r.inviting.Mint(context.Background(), r.principal(invOwner), tlThird, control.RoleMember, 0)
		if err != nil {
			t.Fatal(err)
		}
		srv, stub := joinServer(t, r.inviting)
		stub.err = &identity.UnprovisionedSubject{Provider: "fixture-provider", Subject: strangerSubject(3)}
		if rec := followTheQueryLink(t, srv, token); rec.Code != http.StatusSeeOther {
			t.Fatalf("a single-use invitation sent as `?invite=` answered %d, want 303: %s", rec.Code, rec.Body.String())
		}
		if user, ok := r.authority.Model().UserByProviderSubject("fixture-provider", strangerSubject(3)); !ok {
			t.Error("no user was provisioned")
		} else if _, member := r.authority.Model().RoleIn(tlThird, user.ID); !member {
			t.Error("the invitation was not redeemed into its project")
		}
	})
	t.Run("a single-use team link", func(t *testing.T) {
		r := newTeamRig(t)
		token, link := r.mint(invOwner, invite.LinkReader, false, scopeT(tlA))
		srv, stub := joinServer(t, r.inviting)
		stub.err = &identity.UnprovisionedSubject{Provider: "fixture-provider", Subject: strangerSubject(4)}
		if rec := followTheQueryLink(t, srv, token); rec.Code != http.StatusSeeOther {
			t.Fatalf("a single-use link sent as `?invite=` answered %d, want 303: %s", rec.Code, rec.Body.String())
		}
		if log, _ := r.links.LinkRedemptions(link.Digest); len(log) != 1 {
			t.Errorf("a single-use link sent as `?invite=` left %d redemption row(s), want 1", len(log))
		}
	})
}

// TestTheFragmentJoinPageMovesTheTokenThroughTheScript pins the page a fragment link opens,
// served through the real dispatcher: `no-store`; a `<noscript>` saying the link needs
// JavaScript (never a silent failure); the accept form present but `hidden`, posting to the start
// row with an EMPTY `invite` field for the script to fill; the no-invitation sentence `hidden`;
// and exactly one script, allowlisted, at the content-hashed path of the committed `join.js`,
// serving those bytes. RED before the change: the bare page had no form, no script, no noscript.
func TestTheFragmentJoinPageMovesTheTokenThroughTheScript(t *testing.T) {
	srv, _ := joinServer(t, benignInviting())
	rec := fetch(srv, JoinPath, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /join answered %d", rec.Code)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("GET /join Cache-Control %q, want no-store", got)
	}
	body := rec.Body.String()
	for _, want := range []string{
		`<noscript><p class="refused">This invitation link needs JavaScript to open: the invitation is in the part of ` +
			`the link after the #, which your browser keeps to itself. Turn JavaScript on for this site and open the ` +
			`link again.</p></noscript>`,
		`<p class="refused" id="join-missing" hidden>`,
		`<div id="join-accept" hidden>`,
		`<form class="signin-provider" id="join-form" method="post" action="` + OAuthStartPath + `">`,
		`<input type="hidden" name="` + inviteTokenField + `" id="join-invite" value="">`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the fragment join page lacks %q", want)
		}
	}
	// No token rides the server-rendered page: the accept form's ONLY hidden field is the empty one.
	if f := acceptFormFields(body); len(f) != 1 || f.Get(inviteTokenField) != "" || !f.Has(inviteTokenField) {
		t.Errorf("the fragment page's accept form fields are %v, want exactly one empty %q", f, inviteTokenField)
	}
	srcs := scriptSrcRe.FindAllStringSubmatch(body, -1)
	want := "/static/join." + joinDigestFromBytes(t) + ".js"
	if len(srcs) != 1 || srcs[0][1] != want {
		t.Fatalf("the fragment join page carries scripts %v, want exactly [%s]", srcs, want)
	}
	if v := scriptViolations(body); len(v) != 0 {
		t.Errorf("the join page carries a script outside the allowlist: %v", v)
	}
	if !slices.Contains(AllowedScriptSources(), want) {
		t.Errorf("AllowedScriptSources() = %v does not name the join script %s", AllowedScriptSources(), want)
	}
	served := fetch(srv, want, nil)
	if served.Code != http.StatusOK || served.Body.String() != joinScriptFromDisk(t) {
		t.Errorf("GET %s answered %d with %d byte(s), want 200 and the committed join.js", want, served.Code, served.Body.Len())
	}
	for h, v := range map[string]string{
		"Content-Type": "text/javascript; charset=utf-8", "X-Content-Type-Options": "nosniff",
		"Cache-Control": "public, max-age=31536000, immutable",
	} {
		if got := served.Header().Get(h); got != v {
			t.Errorf("%s: %s = %q, want %q", want, h, got, v)
		}
	}

	// A QUERY link's page carries NO script (the server already has the token) and no noscript.
	legacy := fetch(srv, JoinPath+"?"+inviteTokenField+"="+fixtureInviteToken, nil).Body.String()
	if strings.Contains(strings.ToLower(legacy), "<script") || strings.Contains(legacy, "<noscript") {
		t.Error("the query-link join page carries the fragment script or its noscript notice")
	}
	// And an UNARMED provider renders the plain refusal and no script: there is nothing to accept with.
	cfg := testConfig(t, refusingAuth{})
	cfg.OAuth = nil
	unarmedSrv, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	unarmed := fetch(unarmedSrv, JoinPath, nil).Body.String()
	if strings.Contains(strings.ToLower(unarmed), "<script") || !strings.Contains(pageText(unarmed), "cannot accept an invitation right now") {
		t.Errorf("the unarmed join page should refuse plainly with no script: %q", pageText(unarmed))
	}
}

// joinBannedSinks: what moving one value from the fragment into one form field has no use for.
// Every way to navigate (`location` beyond `.hash`, `href`, `.assign`, `open(`), to submit or
// redirect a form (`submit`, `action`), to send a request, to store, or to turn text into markup.
var joinBannedSinks = []string{
	"innerHTML", "outerHTML", "insertAdjacentHTML", "document.write", "eval(", "Function(",
	"setTimeout(", "setInterval(", "fetch(", "XMLHttpRequest", "sendBeacon", "WebSocket",
	"localStorage", "sessionStorage", "indexedDB", "document.cookie", "import(", "createElement",
	"setAttribute", "src=", "href", "assign(", "open(", "pushState", "submit", "action", "formAction",
	".search", "postMessage", "window.name", "document.URL", "baseURI", "referrer",
}

// joinScriptViolations is the spelling guard's structural half, over the code with whitespace
// collapsed: `location` only as the ONE read `location.hash`, which is the first statement of
// `take`; `history` only as the ONE `history.replaceState(null, "", "<JoinPath>")` — a LITERAL
// target, so nothing URL-controlled reaches it — IMMEDIATELY after that read, so the fragment is
// cleared before the page is touched; the only event subscriptions `hashchange` -> `take` and
// `pageshow` -> `restored`; the token written to exactly one place, `field.value`, whose only other
// write is `restored` EMPTYING it on a back-forward-cache restore; and the form revealed only past
// the empty-token return.
func joinScriptViolations(code string) []string {
	code = strings.Join(strings.Fields(code), " ")
	var out []string
	count := func(s string, want int) {
		if n := strings.Count(code, s); n != want {
			out = append(out, strconv.Quote(s)+" appears "+strconv.Itoa(n)+" time(s), want "+strconv.Itoa(want))
		}
	}
	read := `function take() { var hash = location.hash; history.replaceState(null, "", "` + JoinPath + `"); var accept`
	count("location", 1)
	count("history", 1)
	count(read, 1)
	count("addEventListener(", 2)
	count(`window.addEventListener("hashchange", take);`, 1)
	count(`window.addEventListener("pageshow", restored);`, 1)
	count(".value = ", 2)
	count("field.value = token;", 1)
	// The back-forward-cache restore: only on `persisted`, and it EMPTIES the field and hides the
	// form, so Back from the accept POST never shows a filled form (the only other write to `.value`).
	count(`function restored(event) { if (!event.persisted) { return; }`, 1)
	count(`field.value = ""; accept.hidden = true; missing.hidden = false; }`, 1)
	if r, g := strings.Index(code, read), strings.Index(code, "getElementById"); r < 0 || g < 0 || r > g {
		out = append(out, "the fragment is not read and cleared before the page is touched")
	}
	// The form is revealed only past the empty-token return: an EMPTY form completes as an
	// ordinary sign-in for somebody who was invited.
	guard := `if (token === "") { if (field.value === "") { missing.hidden = false; } return; }`
	count(guard, 1)
	count("accept.hidden = false", 1)
	if gi, ri := strings.Index(code, guard), strings.Index(code, "accept.hidden = false"); gi < 0 || ri < 0 || gi > ri {
		out = append(out, "the accept form can be revealed before the empty-token return")
	}
	for _, sink := range joinBannedSinks {
		if strings.Contains(code, sink) {
			out = append(out, "uses "+strconv.Quote(sink))
		}
	}
	return out
}

// TestTheJoinScriptTouchesOnlyWhatItSays — `join.js` may touch only `location.hash`,
// `history.replaceState` and the one accept form's field, and nothing URL-controlled may become a
// navigation target. ⚠ A SPELLING GUARD, LABELLED AS ONE (`pwa.js`'s twin): `window["loc"+"ation"]`
// walks it. The STATE guard is the browser test `uiaudit`'s `TestTheJoinFragmentIsClearedAndPosted`.
// RED before the change: there was no `join.js`.
func TestTheJoinScriptTouchesOnlyWhatItSays(t *testing.T) {
	code := scriptCode(joinScriptFromDisk(t))
	// INSTRUMENT CONTROL: the strip left the code.
	for _, must := range []string{`getElementById("join-invite")`, `getElementById("join-accept")`, ".hidden = false", "decodeURIComponent("} {
		if !strings.Contains(code, must) {
			t.Fatalf("the comment-stripped join.js lacks %q, so the scan below reads something other than the code", must)
		}
	}
	for _, v := range joinScriptViolations(code) {
		t.Errorf("join.js: %s", v)
	}
	// NEGATIVE CONTROLS, in code position, each a realistic edit.
	for name, mutated := range map[string]string{
		"a navigation to a URL-controlled value": code + "\nlocation.href = token;\n",
		"an auto-submit":                         code + "\ndocument.getElementById(\"join-form\").submit();\n",
		"a form retarget":                        code + "\nform.action = token;\n",
		"a second history entry":                 code + "\nhistory.pushState(null, \"\", \"/join\");\n",
		"the clear moved after the page is touched": strings.Replace(code,
			`history.replaceState(null, "", "`+JoinPath+`");`, "", 1) + "\nhistory.replaceState(null, \"\", \"" + JoinPath + "\");\n",
		"the clear's target taken from the URL": strings.Replace(code,
			`history.replaceState(null, "", "`+JoinPath+`");`, `history.replaceState(null, "", hash.slice(1));`, 1),
		"the token copied to a second sink":       code + "\nmissing.textContent = token; other.value = token;\n",
		"the empty-token return dropped":          strings.Replace(code, `if (token === "") {`, `if (false) {`, 1),
		"a second event subscription":             code + "\nwindow.addEventListener(\"message\", take);\n",
		"the restore no longer empties the field": strings.Replace(code, `field.value = "";`, `field.value = field.value;`, 1),
		"the restore subscription dropped":        strings.Replace(code, `window.addEventListener("pageshow", restored);`, "", 1),
	} {
		if mutated == code || len(joinScriptViolations(mutated)) == 0 {
			t.Errorf("NEGATIVE CONTROL FAILED: %s produced no violation, so the guard above cannot go red", name)
		}
	}
}

// TestAQueryBorneTokenThatCannotBeClassifiedIsNotRedeemed — `admitQueryBorne` FAILS CLOSED: when the
// link store cannot say whether a query-borne token is reusable, the token is dropped rather than
// redeemed. Driven through the start row with the LEGACY field and the callback, over the stubbed
// invitation half (a known user, so the sign-in itself completes either way).
func TestAQueryBorneTokenThatCannotBeClassifiedIsNotRedeemed(t *testing.T) {
	run := func(t *testing.T, classifyErr error) *staticInviting {
		t.Helper()
		cfg, stub, inviting := providerConfigWithInviting(t)
		stub.principal = testIdentity().Principal
		inviting.team.reusableErr = classifyErr
		srv, err := New(cfg)
		if err != nil {
			t.Fatal(err)
		}
		rec := joinCallback(srv, startWith(t, srv, url.Values{inviteQueryTokenField: {fixtureInviteToken}}))
		if rec.Code != http.StatusSeeOther {
			t.Fatalf("the known user's sign-in answered %d, want 303", rec.Code)
		}
		return inviting
	}
	if inv := run(t, errFixtureClassify); inv.redeemForCalls != 0 || inv.redeemCalls != 0 {
		t.Errorf("a query-borne token the link store could not classify was redeemed (RedeemFor %d, Redeem %d) — "+
			"the refusal fails OPEN", inv.redeemForCalls, inv.redeemCalls)
	}
	// POSITIVE CONTROL: the same flow with a working store (the token is not a reusable link) redeems.
	if inv := run(t, nil); inv.redeemForCalls != 1 || inv.redeemedForToken != fixtureInviteToken {
		t.Fatalf("the same query-borne single-use token was not redeemed (RedeemFor %d) — the zero above is about "+
			"the harness", inv.redeemForCalls)
	}
}

var errFixtureClassify = errors.New("fixture: the link store is unreachable")
