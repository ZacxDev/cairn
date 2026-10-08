package ui

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ZacxDev/cairn/internal/control"
	"github.com/ZacxDev/cairn/internal/identity"
	"github.com/ZacxDev/cairn/internal/presence"
)

// 🔴 THE BELL (S5), THROUGH THE REAL DISPATCHER. Every request below is AUTHENTICATED (`staticAuth`)
// and carries a session cookie the test chose, so the CSRF gate is REACHED rather than shadowed by the
// chain — `TestTheCSRFGuardIsReachedByAnAuthenticatedRequest`'s rule. The queue is observed by claiming
// from it directly, which is exactly what the agent route's handler does.
//
// Fixture values are pairwise distinct: the cookie, the hosts, the targets and the session ids never
// coincide, and none equals a constant the handler spells.

const (
	bellCookie = "a-session-cookie-the-bell-test-chose"
	// bellHost is NOT `host-a`, so a ring aimed at the fixture default could not pass for one aimed here.
	bellHost = "host-q"
)

// bellService is a presence service whose store holds `rows` for `owner` on `host`, pushed `age` before
// the UI clock; the queue runs on `queueNow`.
func bellService(t *testing.T, owner presence.Owner, host string, age time.Duration, queueNow func() time.Time, rows ...string) *presence.Service {
	t.Helper()
	clock := &badgeClock{at: sessNow.Add(-age)}
	st := &presence.Store{Now: clock.now}
	st.Replace(owner, host, pushRows(t, host, rows...))
	clock.at = sessNow
	return &presence.Service{Store: st, Queue: &presence.Queue{Now: queueNow}}
}

func fixedAt(at time.Time) func() time.Time { return func() time.Time { return at } }

// ringRequest is a `POST /ring` for `session` with the gates' inputs chosen per call: `origin` ("" =
// no header), and `csrf` ("" = no field).
func ringRequest(session, origin, csrf string) *http.Request {
	form := url.Values{FieldSession: {session}}
	if csrf != "" {
		form.Set(FieldCSRF, csrf)
	}
	r := httptest.NewRequest(http.MethodPost, RingPath, strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	r.AddCookie(&http.Cookie{Name: identity.SessionCookieName, Value: bellCookie})
	return r
}

// sameOriginOf is the Origin a browser on this test server's page would send.
func sameOriginOf(r *http.Request) string { return "http://" + r.Host }

// ring sends a same-origin, CSRF-carrying `POST /ring` — the request the bell form makes.
func ring(t *testing.T, srv *Server, session string) *httptest.ResponseRecorder {
	t.Helper()
	r := ringRequest(session, "", identity.CSRFTokenFor(bellCookie))
	r.Header.Set("Origin", sameOriginOf(r))
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, r)
	return rec
}

// answer is everything a ring response says: status, every header, the body.
type answer struct {
	code   int
	header http.Header
	body   string
}

func answerOf(rec *httptest.ResponseRecorder) answer {
	return answer{code: rec.Code, header: rec.Header().Clone(), body: rec.Body.String()}
}

// TestTheRingRowIsBehindBothCrossSiteGates — the S5 test plan's first item: `POST /ring` without an
// Origin, with a foreign one, and without (or with a wrong) CSRF token gets the EXISTING refusals,
// each asserted by its OWN message (both gates answer 403, so a status alone would score one gate's
// kill as the other's), and the queue is unchanged after every one. The positive control is the same
// request with both gates satisfied: 303, and exactly one ring queued. RED with the row declared
// `classPublic` (`ui-ring-row-declared-public`), which is the shape a future class exemption takes: a
// public row dispatches ahead of the chain and therefore ahead of the CSRF gate. The gates' own
// predicates are `TestACrossSiteStateChangeIsRefusedBeforeAuthentication`'s and the CSRF test's to pin.
func TestTheRingRowIsBehindBothCrossSiteGates(t *testing.T) {
	roamer, _ := sessionWorld(t)
	src := StoreSource{Root: sessionStore(t), ArcJournal: sessionJournal(t)}
	row := wireRow(badgeSession, "claude", "notes:3", badgeHotkey, "")

	for _, arm := range []struct {
		name, origin, csrf, want string
	}{
		{"no Origin header", "", identity.CSRFTokenFor(bellCookie), crossSiteRefusal},
		{"a foreign Origin", "https://evil.invalid", identity.CSRFTokenFor(bellCookie), crossSiteRefusal},
		{"no CSRF token", "same", "", csrfRefusal},
		{"another session's CSRF token", "same", identity.CSRFTokenFor("some-other-session-cookie"), csrfRefusal},
	} {
		t.Run(arm.name, func(t *testing.T) {
			svc := bellService(t, ownerOf(roamer), bellHost, 0, fixedAt(sessNow), row)
			srv := badgeServer(t, src, roamer, svc)
			r := ringRequest(badgeSession, arm.origin, arm.csrf)
			if arm.origin == "same" {
				r.Header.Set("Origin", sameOriginOf(r))
			}
			rec := httptest.NewRecorder()
			srv.ServeHTTP(rec, r)
			if rec.Code != http.StatusForbidden || rec.Body.String() != arm.want {
				t.Errorf("answered %d %q, want 403 %q — the gate that must refuse this did not", rec.Code, rec.Body.String(), arm.want)
			}
			if got := svc.Queue.Claim(ownerOf(roamer), bellHost); len(got) != 0 {
				t.Errorf("a REFUSED ring was queued anyway: %+v", got)
			}
		})
	}

	// POSITIVE CONTROL: both gates satisfied, the same world — the refusals above are about the gates.
	svc := bellService(t, ownerOf(roamer), bellHost, 0, fixedAt(sessNow), row)
	if rec := ring(t, badgeServer(t, src, roamer, svc), badgeSession); rec.Code != http.StatusSeeOther {
		t.Fatalf("POSITIVE CONTROL FAILED: a same-origin ring carrying its token answered %d %q", rec.Code, rec.Body.String())
	}
	if got := svc.Queue.Claim(ownerOf(roamer), bellHost); len(got) != 1 || got[0].Session != badgeSession {
		t.Fatalf("POSITIVE CONTROL FAILED: the owner's ring was not queued for %s: %+v", bellHost, got)
	}
}

// TestEveryRingAnswerIsTheSameRedirect — decision 5 for the ring: the OWNER's ring is queued and every
// other case queues nothing, and ALL of them — queued, refused, presence off — get the SAME status,
// headers and body: a 303 to the session page. The presence-OFF answer is the reference. RED with
// `Service.Ring` reading the store without the predicate (`presence-ring-reads-the-store-without-the-predicate`)
// on the queue arms, with the handler never asking presence (`ui-ring-never-asks-presence`) on the
// owner's arm, and with the handler answering the queued case differently
// (`ui-ring-answers-a-queued-ring-differently`).
func TestEveryRingAnswerIsTheSameRedirect(t *testing.T) {
	roamer, easterner := sessionWorld(t)
	src := StoreSource{Root: sessionStore(t), ArcJournal: sessionJournal(t)}
	narrowed := roamer
	narrowed.Auth = control.Narrow(roamer.Auth, []control.ID{sessNorth, sessSouth})
	if !narrowed.Auth.Narrowed() || ownerOf(narrowed) != ownerOf(roamer) {
		t.Fatal("INSTRUMENT: the narrowed viewer is not the owner, narrowed")
	}
	row := wireRow(badgeSession, "claude", "notes:3", badgeHotkey, "")

	off := answerOf(ring(t, badgeServer(t, src, roamer, nil), badgeSession))
	if off.code != http.StatusSeeOther || off.header.Get("Location") != sessionHref(badgeSession) || off.body != "" {
		t.Fatalf("the presence-OFF ring answered %d Location=%q body=%q; want 303 to %q with no body",
			off.code, off.header.Get("Location"), off.body, sessionHref(badgeSession))
	}

	for _, arm := range []struct {
		name   string
		viewer identity.Identity
		svc    *presence.Service
		queued int
	}{
		{"the owner", roamer, bellService(t, ownerOf(roamer), bellHost, 0, fixedAt(sessNow), row), 1},
		{"another owner's presence", roamer, bellService(t, ownerOf(easterner), bellHost, 0, fixedAt(sessNow), row), 0},
		{"the owner, narrowed", narrowed, bellService(t, ownerOf(roamer), bellHost, 0, fixedAt(sessNow), row), 0},
		{"expired presence (181 s after a 180 s TTL push)", roamer,
			bellService(t, ownerOf(roamer), bellHost, presence.DefaultTTL+time.Second, fixedAt(sessNow), row), 0},
		{"presence for another session only", roamer,
			bellService(t, ownerOf(roamer), bellHost, 0, fixedAt(sessNow), wireRow("s-other-09", "claude", "notes:4", "", "")), 0},
		{"an empty store", roamer, &presence.Service{Store: &presence.Store{}, Queue: &presence.Queue{}}, 0},
	} {
		t.Run(arm.name, func(t *testing.T) {
			got := answerOf(ring(t, badgeServer(t, src, arm.viewer, arm.svc), badgeSession))
			if !reflect.DeepEqual(got, off) {
				t.Errorf("answered %+v, want the presence-OFF answer %+v — the response says whether a pane exists", got, off)
			}
			// Every owner key a ring could have been filed under, on the host the row named.
			queued := len(arm.svc.Queue.Claim(ownerOf(roamer), bellHost)) + len(arm.svc.Queue.Claim(ownerOf(easterner), bellHost))
			if queued != arm.queued {
				t.Errorf("%d ring(s) queued, want %d", queued, arm.queued)
			}
		})
	}

	// An id the trailer grammar cannot produce is the same redirect to ITS page, and queues nothing even
	// for an owner whose store holds a live row (the row's session is the fixture's, so nothing could
	// match it anyway — this pins that the bound answers the redirect rather than a 400).
	svc := bellService(t, ownerOf(roamer), bellHost, 0, fixedAt(sessNow), row)
	bad := "not a session/id"
	got := answerOf(ring(t, badgeServer(t, src, roamer, svc), bad))
	if got.code != http.StatusSeeOther || got.header.Get("Location") != sessionHref(bad) || got.body != "" {
		t.Errorf("a malformed session id answered %+v, want the 303 to its own page", got)
	}
	if q := svc.Queue.Claim(ownerOf(roamer), bellHost); len(q) != 0 {
		t.Errorf("a malformed session id queued %+v", q)
	}
}

// TestARepeatWhilePendingQueuesNoSecondRing — O4/D3 through the route: ringing twice while the first is
// pending queues ONE ring and answers identically; once claimed, the next ring is queued afresh (the
// positive control that the dedupe is "while pending", not "forever").
//
// 🔴 AND THE REPEAT DOES NOT RESTART THE PENDING RING'S CLOCK. The queue is keyed `(owner, session)`, so
// a queue that dropped its dedupe would still hold ONE ring — the repeat would REPLACE it, and the count
// alone could not tell. What differs is the ring's age: a repeat at 30 s must leave the ORIGINAL ring,
// which is gone at 61 s; a replacing repeat would keep a ring alive past 60 s for as long as somebody
// kept clicking. RED with the dedupe dropped (`presence-queue-a-repeat-replaces-the-pending-ring`).
func TestARepeatWhilePendingQueuesNoSecondRing(t *testing.T) {
	roamer, _ := sessionWorld(t)
	src := StoreSource{Root: sessionStore(t), ArcJournal: sessionJournal(t)}
	q := &badgeClock{at: sessNow}
	svc := bellService(t, ownerOf(roamer), bellHost, 0, q.now, wireRow(badgeSession, "claude", "notes:3", "", ""))
	srv := badgeServer(t, src, roamer, svc)

	first := answerOf(ring(t, srv, badgeSession))
	second := answerOf(ring(t, srv, badgeSession))
	if !reflect.DeepEqual(first, second) {
		t.Errorf("the repeat answered %+v, the first %+v — a repeat while pending must be the same answer", second, first)
	}
	if got := svc.Queue.Claim(ownerOf(roamer), bellHost); len(got) != 1 {
		t.Fatalf("two rings while pending queued %d, want exactly 1: %+v", len(got), got)
	}
	ring(t, srv, badgeSession)
	if got := svc.Queue.Claim(ownerOf(roamer), bellHost); len(got) != 1 {
		t.Fatalf("POSITIVE CONTROL FAILED: after a claim, a new ring queued %d, want 1", len(got))
	}

	start := q.at
	ring(t, srv, badgeSession)
	q.at = start.Add(30 * time.Second)
	ring(t, srv, badgeSession)
	q.at = start.Add(61 * time.Second)
	if got := svc.Queue.Claim(ownerOf(roamer), bellHost); len(got) != 0 {
		t.Fatalf("a repeat at 30 s kept a ring alive at 61 s (%+v): the repeat REPLACED the pending ring "+
			"instead of being a no-op, so clicking keeps a ring pending forever", got)
	}
}

// TestARungRingLivesSixtySeconds — the TTL through the route, on the queue's injected clock (no sleep):
// a ring claimed at 59 s is returned exactly once; an unclaimed ring is gone at 61 s, and a new one can
// then be queued. The two instants straddle the 60 s bound by one second each side, so a TTL moved
// either way past them is red (`presence-ring-ttl-too-long`, `presence-ring-ttl-too-short`).
func TestARungRingLivesSixtySeconds(t *testing.T) {
	roamer, _ := sessionWorld(t)
	src := StoreSource{Root: sessionStore(t), ArcJournal: sessionJournal(t)}
	q := &badgeClock{at: sessNow}
	svc := bellService(t, ownerOf(roamer), bellHost, 0, q.now, wireRow(badgeSession, "claude", "notes:3", "", ""))
	srv := badgeServer(t, src, roamer, svc)

	ring(t, srv, badgeSession)
	q.at = sessNow.Add(59 * time.Second)
	if got := svc.Queue.Claim(ownerOf(roamer), bellHost); len(got) != 1 {
		t.Fatalf("a ring claimed at 59 s returned %d ring(s), want 1", len(got))
	}
	if got := svc.Queue.Claim(ownerOf(roamer), bellHost); len(got) != 0 {
		t.Fatalf("a ring was claimed TWICE: %+v", got)
	}

	start := q.at
	ring(t, srv, badgeSession)
	q.at = start.Add(61 * time.Second)
	if got := svc.Queue.Claim(ownerOf(roamer), bellHost); len(got) != 0 {
		t.Fatalf("an unclaimed ring survived to 61 s: %+v", got)
	}
	ring(t, srv, badgeSession)
	if got := svc.Queue.Claim(ownerOf(roamer), bellHost); len(got) != 1 {
		t.Fatalf("after the unclaimed ring expired, a new ring queued %d, want 1", len(got))
	}
}

// TestTheRingGoesToTheBadgesHost — decision 7 at enqueue time, through the route: with the session live
// on two of the owner's hosts, the ring is claimable ONLY on the host the badge names — the newer
// `last_activity` — and, on a tie of two empty values, the byte-wise smaller label. RED with the target
// pick inverted (`presence-target-picks-oldest-activity`) and the tie-break flipped
// (`presence-target-tie-goes-to-larger-host`).
func TestTheRingGoesToTheBadgesHost(t *testing.T) {
	roamer, _ := sessionWorld(t)
	src := StoreSource{Root: sessionStore(t), ArcJournal: sessionJournal(t)}
	for _, tc := range []struct {
		name            string
		olderAct, newer string
		want, other     string
	}{
		// host-r is NEWER though it sorts after host-p, so a smaller-label pick is red here.
		{"newest last_activity wins", "2000-01-10T09:00:00Z", "2000-01-10T10:00:00Z", "host-r", "host-p"},
		// Both empty: the smaller label, host-p.
		{"a tie goes to the smaller label", "", "", "host-p", "host-r"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st := &presence.Store{Now: fixedAt(sessNow)}
			st.Replace(ownerOf(roamer), "host-p", pushRows(t, "host-p", wireRow(badgeSession, "claude", "notes:5", "", tc.olderAct)))
			st.Replace(ownerOf(roamer), "host-r", pushRows(t, "host-r", wireRow(badgeSession, "opencode", "notes:6", "", tc.newer)))
			svc := &presence.Service{Store: st, Queue: &presence.Queue{Now: fixedAt(sessNow)}}
			ring(t, badgeServer(t, src, roamer, svc), badgeSession)
			if got := svc.Queue.Claim(ownerOf(roamer), tc.other); len(got) != 0 {
				t.Errorf("the ring went to %s, which is not the target: %+v", tc.other, got)
			}
			if got := svc.Queue.Claim(ownerOf(roamer), tc.want); len(got) != 1 {
				t.Errorf("the target host %s has %d ring(s), want 1", tc.want, len(got))
			}
		})
	}
}

var bellFormRE = regexp.MustCompile(`<form class="bell" method="post" action="([^"]*)" data-presence="bell"><input type="hidden" name="` +
	FieldCSRF + `" value="([^"]*)"><input type="hidden" name="` + FieldSession + `" value="([^"]*)">`)

// cookieGet is a GET carrying the bell test's session cookie, so the page derives a CSRF token and
// renders its forms — the state in which a bell COULD render.
func cookieGet(t *testing.T, srv *Server, path string) string {
	t.Helper()
	r := httptest.NewRequest(http.MethodGet, path, nil)
	r.AddCookie(&http.Cookie{Name: identity.SessionCookieName, Value: bellCookie})
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, r)
	if rec.Code != http.StatusOK {
		t.Fatalf("%s answered %d: %s", path, rec.Code, rec.Body.String())
	}
	return rec.Body.String()
}

// TestTheBellRendersOnlyBesideTheOwnersBadge — where the bell renders, and the byte-identity of
// decision 5 in the state S4's test never builds: a request WITH a session cookie, so a CSRF token
// exists and a bell could render. The owner's session page carries exactly one bell form posting to
// `RingPath` with this cookie's token and the page's session id; a non-owner, a narrowed owner, expired
// presence and presence for another session render bytes EQUAL to presence off; a request with no
// cookie gets the badge but no bell (it has no token to carry); and no other surface renders one.
// RED with the bell rendered outside the badge's condition (`ui-bell-rendered-without-the-badge`) and
// with it dropped (`ui-bell-never-rendered`).
func TestTheBellRendersOnlyBesideTheOwnersBadge(t *testing.T) {
	roamer, easterner := sessionWorld(t)
	src := StoreSource{Root: sessionStore(t), ArcJournal: sessionJournal(t)}
	narrowed := roamer
	narrowed.Auth = control.Narrow(roamer.Auth, []control.ID{sessNorth, sessSouth})
	row := wireRow(badgeSession, "claude", "notes:3", badgeHotkey, "")
	page := sessionURL(badgeSession)

	off := cookieGet(t, badgeServer(t, src, roamer, nil), page)
	if strings.Contains(off, `data-presence=`) || strings.Contains(off, `action="`+RingPath+`"`) {
		t.Fatal("the presence-OFF page carries a presence node or a ring form")
	}
	if csrfOf(t, off) == "" {
		t.Fatal("INSTRUMENT: the off page rendered no CSRF token, so the cookie never reached it and no bell could render anywhere")
	}

	owned := cookieGet(t, badgeServer(t, src, roamer, bellService(t, ownerOf(roamer), bellHost, 0, nil, row)), page)
	forms := bellFormRE.FindAllStringSubmatch(owned, -1)
	if len(forms) != 1 || forms[0][1] != RingPath || forms[0][2] != identity.CSRFTokenFor(bellCookie) || forms[0][3] != badgeSession {
		t.Fatalf("the owner's page carries bell forms %q; want exactly one posting to %q with this cookie's token and %q",
			forms, RingPath, badgeSession)
	}
	// WHERE the form sits, as the elements OPEN at its start tag — never as string offsets, which an
	// HTML parser does not respect: a `<form>` start tag closes an open `<p>`, so a form written inside
	// a paragraph's bytes lands AFTER it in the DOM, followed by a stray empty `<p>` (the audit finding
	// this assertion was rewritten for).
	open := openElementsAt(t, owned, `<form class="bell"`)
	if slices.Contains(open, "p") {
		t.Errorf("the bell form starts inside an open <p> (open elements %v): a parser closes the paragraph there, "+
			"so the form is NOT inside the badge's container in the DOM", open)
	}
	if !slices.ContainsFunc(openTags(t, owned, `<form class="bell"`), func(tag string) bool {
		return strings.Contains(tag, `data-presence="session"`)
	}) {
		t.Error("the bell form is not inside the session badge's container")
	}
	if pane, bell := strings.Index(owned, `data-presence="pane"`), strings.Index(owned, `<form class="bell"`); pane < 0 || bell < pane {
		t.Error("the bell does not come after the badge")
	}

	for arm, got := range map[string]string{
		"another owner's presence": cookieGet(t, badgeServer(t, src, roamer, bellService(t, ownerOf(easterner), bellHost, 0, nil, row)), page),
		"expired presence": cookieGet(t, badgeServer(t, src, roamer,
			bellService(t, ownerOf(roamer), bellHost, presence.DefaultTTL+time.Second, nil, row)), page),
		"presence for another session": cookieGet(t, badgeServer(t, src, roamer,
			bellService(t, ownerOf(roamer), bellHost, 0, nil, wireRow("s-other-09", "claude", "notes:4", "", ""))), page),
	} {
		if got != off {
			t.Errorf("%s: the page's bytes differ from the presence-OFF page", arm)
		}
	}
	narrowedOff := cookieGet(t, badgeServer(t, src, narrowed, nil), page)
	if got := cookieGet(t, badgeServer(t, src, narrowed, bellService(t, ownerOf(roamer), bellHost, 0, nil, row)), page); got != narrowedOff {
		t.Error("a NARROWED owner's page differs from its presence-OFF page")
	}

	// No cookie: the badge, and no bell — a form with no token would be a control that always 403s.
	bare := bodyOf(t, badgeServer(t, src, roamer, bellService(t, ownerOf(roamer), bellHost, 0, nil, row)), page)
	if !strings.Contains(bare, `data-presence="pane"`) || strings.Contains(bare, `data-presence="bell"`) {
		t.Error("with no session cookie the owner's page must carry the badge and no bell")
	}

	// The bell is on the session page only — the row surfaces carry the badge, not a ring control.
	svc := bellService(t, ownerOf(roamer), bellHost, 0, nil, row)
	for name, path := range badgeSurfaces() {
		if name == "session page" {
			continue
		}
		if body := cookieGet(t, badgeServer(t, src, roamer, svc), path); strings.Contains(body, `data-presence="bell"`) {
			t.Errorf("%s renders a bell; S5 places it on the session page only", name)
		}
	}
}

// The tag walk reuses `browse_test.go`'s `htmlTag` (quote-aware, so a `>` inside an attribute value
// cannot end a tag) and `voidElements` (never pushed: they take no end tag).

// openTags is the stack of start tags (whole, attributes and all) still OPEN at the first occurrence of
// `needle` in `doc`, outermost first, tracking EXPLICIT tags only.
//
// ⚠ WHAT IT CANNOT SEE, stated because it is not a parser (no HTML5 parser is in `depspolicy`'s
// allowlist, and growing it for one test is not this test's decision): it does NOT apply implied end
// tags or parser-inserted elements. So it answers "which elements does the MARKUP leave open here" —
// and that is exactly the question for a `<p>`: a flow element starting while a `<p>` is open in the
// bytes is the shape a parser rewrites. It cannot see the other implied-close rules (a nested `<a>`,
// `<li>` in `<li>`, table fostering), none of which this markup uses.
func openTags(t *testing.T, doc, needle string) []string {
	t.Helper()
	at := strings.Index(doc, needle)
	if at < 0 {
		t.Fatalf("INSTRUMENT: %q is not in the document", needle)
	}
	var stack []string
	for _, m := range htmlTag.FindAllStringSubmatchIndex(doc[:at], -1) {
		whole, name := doc[m[0]:m[1]], strings.ToLower(doc[m[4]:m[5]])
		switch {
		case doc[m[2]:m[3]] == "/":
			for i := len(stack) - 1; i >= 0; i-- {
				if tagName(stack[i]) == name {
					stack = stack[:i]
					break
				}
			}
		case !voidElements[name] && !strings.HasSuffix(whole, "/>"):
			stack = append(stack, whole)
		}
	}
	return stack
}

// openElementsAt is [openTags] reduced to element names.
func openElementsAt(t *testing.T, doc, needle string) []string {
	t.Helper()
	var names []string
	for _, tag := range openTags(t, doc, needle) {
		names = append(names, tagName(tag))
	}
	return names
}

func tagName(tag string) string {
	return strings.ToLower(htmlTag.FindStringSubmatch(tag)[2])
}

// TestTheOpenElementTrackerSeesAnOpenParagraph is the tracker's own pair of controls, so its "no <p>
// open" reading above is a measurement: a needle inside an unclosed <p> MUST report it, and one after
// the paragraph closes must not.
func TestTheOpenElementTrackerSeesAnOpenParagraph(t *testing.T) {
	doc := `<html><body><div class="x"><p data-presence="session"><span>a</span><input type="hidden"><form id="in"></form></p><form id="out"></form></div></body></html>`
	if got := openElementsAt(t, doc, `<form id="in"`); !slices.Equal(got, []string{"html", "body", "div", "p"}) {
		t.Errorf("POSITIVE CONTROL: inside the paragraph the open elements are %v, want [html body div p]", got)
	}
	if got := openElementsAt(t, doc, `<form id="out"`); !slices.Equal(got, []string{"html", "body", "div"}) {
		t.Errorf("NEGATIVE CONTROL: after the paragraph closed the open elements are %v, want [html body div]", got)
	}
}
