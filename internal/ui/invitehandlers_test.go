package ui

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/ZacxDev/cairn/internal/control"
	"github.com/ZacxDev/cairn/internal/identity"
	"github.com/ZacxDev/cairn/internal/invite"
)

// inviteRig drives the invite flow through the REAL dispatcher, with both cross-site gates
// satisfied, so every test below measures a handler rather than gate (2) or gate (6).
//
// ⚠ IT IS NOT `shareRig`, AND THE DIFFERENCE IS DELIBERATE RATHER THAN DUPLICATION. That rig
// builds a real `control.FileStore`, a real store on disk and a real sign-in exchange,
// because what it measures is `ControlSharing` against a journal. These tests measure the
// HTTP surface — statuses, headers, bytes, which authority was asked — over a stubbed
// `Inviting`, and a rig that needed a Postgres to answer would be untestable in the sandbox.
// `inviting_test.go` is where the real implementation is driven.
//
// 🔴 THE COOKIE VALUE IS SYNTHETIC AND DOES NOT RESOLVE TO A SESSION, WHICH IS SOUND AND IS
// WORTH SAYING BECAUSE IT LOOKS LIKE A SHORTCUT. Authentication here is `staticAuth`, so the
// chain never consults the session store; what the cookie is FOR is gate (6), which derives
// its token from the cookie's VALUE (`csrfTokenFor`) and never resolves it. So this exercises
// the real CSRF gate with a real token — see `TestTheInviteWritesAreBehindBothCrossSiteGates`,
// which is what proves the gates are reached rather than bypassed by this construction.
type inviteRig struct {
	t        *testing.T
	srv      *Server
	inviting *staticInviting
	log      *bytes.Buffer
	cookie   string
}

// inviteCookieValue is a synthetic session-cookie value. It is not a credential: nothing
// resolves it, and the chain in this rig authenticates without looking at it.
const inviteCookieValue = "fixture-session-cookie-value-which-resolves-to-nothing"

// inviteClock is the instant every state assertion is taken at: one hour after the fixture
// invitation was created, so an invitation with the default TTL is OPEN and a test does not
// depend on the wall clock. Year 2000, per the fixture rule.
var inviteClock = fixtureInviteCreated.Add(time.Hour)

func newInviteRig(t *testing.T, mutate func(*Config)) *inviteRig {
	t.Helper()
	rig := &inviteRig{t: t, inviting: benignInviting(), log: &bytes.Buffer{}, cookie: inviteCookieValue}
	cfg := testConfig(t, staticAuth{testIdentity()})
	cfg.Inviting = rig.inviting
	cfg.Log = rig.log
	cfg.Now = func() time.Time { return inviteClock }
	if mutate != nil {
		mutate(&cfg)
	}
	srv, err := New(cfg)
	if err != nil {
		t.Fatalf("the server did not build: %v", err)
	}
	rig.srv = srv
	return rig
}

func (r *inviteRig) get(path string) *httptest.ResponseRecorder {
	r.t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.AddCookie(&http.Cookie{Name: identity.SessionCookieName, Value: r.cookie})
	rec := httptest.NewRecorder()
	r.srv.ServeHTTP(rec, req)
	return rec
}

// anonymousGet carries NO cookie, which is what a public row is reached with.
func (r *inviteRig) anonymousGet(path string) *httptest.ResponseRecorder {
	r.t.Helper()
	rec := httptest.NewRecorder()
	r.srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func (r *inviteRig) post(path string, form url.Values) *httptest.ResponseRecorder {
	r.t.Helper()
	form.Set(FieldCSRF, identity.CSRFTokenFor(r.cookie))
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", "https://"+req.Host)
	req.AddCookie(&http.Cookie{Name: identity.SessionCookieName, Value: r.cookie})
	rec := httptest.NewRecorder()
	r.srv.ServeHTTP(rec, req)
	return rec
}

// invitePath is one project's invite page.
func invitePath(project control.ID) string {
	return InvitePath + "?" + QueryProject + "=" + string(project)
}

// TestTheInviteWritesAreBehindBothCrossSiteGates is the instrument control for every POST
// below, and it has to come first.
//
// 🔴 WITHOUT IT, `inviteRig.post` IS AN UNVALIDATED HARNESS. Every write test below asserts
// what a handler did with a request that satisfied both gates — and if the gates were not
// actually reached, those tests would pass identically against a server with no cross-site
// defence at all. So this drives each write row THREE ways: with no `Origin` (gate 2), with a
// wrong CSRF token (gate 6), and correctly (the positive control proving the rig can get
// past both).
func TestTheInviteWritesAreBehindBothCrossSiteGates(t *testing.T) {
	writes := []struct{ name, path string }{
		{"mint", InvitePath},
		{"revoke", InviteRevokePath},
	}
	for _, w := range writes {
		rig := newInviteRig(t, nil)

		// Gate (2): no Origin at all.
		req := httptest.NewRequest(http.MethodPost, w.path, strings.NewReader(""))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.AddCookie(&http.Cookie{Name: identity.SessionCookieName, Value: rig.cookie})
		rec := httptest.NewRecorder()
		rig.srv.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s: a POST with no Origin answered %d, want 403 — gate (2) is not covering this row",
				w.name, rec.Code)
		}

		// Gate (6): a real Origin, a WRONG token.
		form := url.Values{}
		form.Set(FieldCSRF, "not-this-session's-token")
		req = httptest.NewRequest(http.MethodPost, w.path, strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Origin", "https://"+req.Host)
		req.AddCookie(&http.Cookie{Name: identity.SessionCookieName, Value: rig.cookie})
		rec = httptest.NewRecorder()
		rig.srv.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s: a POST with a wrong CSRF token answered %d, want 403 — gate (6) is not covering "+
				"this row", w.name, rec.Code)
		}

		// POSITIVE CONTROL: the rig CAN get past both, or every write test below is
		// measuring a refusal it cannot see.
		if rig.inviting.minted != 0 || rig.inviting.revoked != 0 {
			t.Fatalf("%s: a refused request reached the authority (minted %d, revoked %d)",
				w.name, rig.inviting.minted, rig.inviting.revoked)
		}
	}

	// The control, spelled as a request that must SUCCEED.
	rig := newInviteRig(t, nil)
	form := url.Values{}
	form.Set(FieldProject, string(fixtureNamedProject.ID))
	form.Set(FieldRole, string(control.RoleMember))
	if rec := rig.post(InvitePath, form); rec.Code != http.StatusOK {
		t.Fatalf("the positive control failed: a correctly-gated mint answered %d, want 200. Every write "+
			"assertion in this file would then be about a cross-site refusal. Body: %q",
			rec.Code, rec.Body.String())
	}
	if rig.inviting.minted != 1 {
		t.Fatalf("the positive control reached no authority (minted %d)", rig.inviting.minted)
	}
}

// TestTheInviteRowsAnswerHonestlyWithNoInviteStore is the no-store configuration, and the
// asymmetry between the read and the writes IS the thing under test.
//
// 🔴 THE READ ANSWERS 200 AND THE WRITES ANSWER 501, WHICH IS A DECISION AND NOT AN
// INCONSISTENCY. `shell` links `GET /invite` from the header of EVERY page, unconditionally,
// so a 501 on the read would be a dead link in the frame of the whole surface — the exact
// "feature reads as absent" defect the header link exists to close, arriving through the
// other door. The writes are linked by nothing but a form on a page that is not rendered in
// this configuration, so they refuse with the cause.
func TestTheInviteRowsAnswerHonestlyWithNoInviteStore(t *testing.T) {
	rig := newInviteRig(t, func(cfg *Config) {
		// Both halves of the database, as `cmd/cairn-ui` leaves them with no DSN: `New` refuses
		// team links with no invitation half (`ErrTeamLinksWithoutInviting`).
		cfg.Inviting, cfg.TeamLinks = nil, nil
	})

	rec := rig.get(InvitePath)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s with no invite store answered %d, want 200. A refusal here is a dead link in "+
			"every page's header. Body: %q", InvitePath, rec.Code, rec.Body.String())
	}
	if !strings.Contains(pageText(rec.Body.String()), normalizeSpace(NoInviteStore)) {
		t.Errorf("the page does not carry NoInviteStore, so a reader who followed the header link is shown "+
			"an empty flow with no explanation. Body: %q", rec.Body.String())
	}
	// It must NOT claim an authority answer it did not get: "No project is yours to invite
	// into" is a sentence about MEMBERSHIP, and in this configuration nothing was asked.
	if strings.Contains(pageText(rec.Body.String()), "That is an authority answer") {
		t.Error("the no-store page renders the authority sentence. Nothing was asked — there is no " +
			"`Inviting` to ask — so that sentence is the measured-lie shape `handlePage` already shipped once")
	}

	for _, path := range []string{InvitePath, InviteRevokePath} {
		form := url.Values{}
		form.Set(FieldProject, string(fixtureNamedProject.ID))
		form.Set(FieldRole, string(control.RoleMember))
		form.Set(FieldDigest, fixtureInviteDigest)
		rec := rig.post(path, form)
		if rec.Code != http.StatusNotImplemented {
			t.Errorf("POST %s with no invite store answered %d, want 501", path, rec.Code)
		}
		if !strings.Contains(rec.Body.String(), NoInviteStore) {
			t.Errorf("POST %s refused without naming the configuration, so an operator is sent hunting a "+
				"permission problem that does not exist", path)
		}
	}

	// POSITIVE CONTROL: with a store wired, the SAME requests do not answer 501 — so the
	// 501s above are about the configuration and not about the requests.
	wired := newInviteRig(t, nil)
	form := url.Values{}
	form.Set(FieldProject, string(fixtureNamedProject.ID))
	form.Set(FieldRole, string(control.RoleMember))
	if rec := wired.post(InvitePath, form); rec.Code == http.StatusNotImplemented {
		t.Error("a mint against a WIRED invite store also answered 501, so the assertions above are a " +
			"fact about the request and not about the missing store")
	}
}

// TestAProjectThatIsNotInvitableIsRefusedBEFORETheUnnarrowedReAD is the narrowing seam, and
// it asserts a RELATIONSHIP rather than a status.
//
// 🔴 `Inviting.Outstanding` PERFORMS NO AUTHORITY CHECK AND SAYS SO IN ITS OWN DOC. It
// returns digests, roles and timestamps for any project it is handed — a listing that would
// tell an outsider who is being invited where. The only thing standing in front of it is
// that `handleInvitePage` reaches it exclusively for a project `Invitable` returned. A guard
// on the 404 alone cannot see that: a handler that called `Outstanding` first and refused
// afterwards would answer the same 404 while having already read the rows.
func TestAProjectThatIsNotInvitableIsRefusedBEFORETheUnnarrowedRead(t *testing.T) {
	rig := newInviteRig(t, nil)
	foreign := control.DerivedID(control.PrefixProject, "somebody-elses")

	// INSTRUMENT CONTROL: `Outstanding` is counted through the same field as `Invitable`,
	// so the two are distinguished by COUNT. A narrowed read is one call; a narrowed read
	// plus an unnarrowed one is two.
	rec := rig.get(invitePath(foreign))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("a foreign project answered %d, want 404", rec.Code)
	}
	if got := strings.TrimSpace(rec.Body.String()); got != inviteRefusal {
		t.Errorf("the refusal body is %q, want %q — a refusal that names a reason is an existence oracle "+
			"over every project in the deployment", got, inviteRefusal)
	}
	if rig.inviting.reads != 1 {
		t.Errorf("the authority was asked %d time(s) for a refused project, want exactly 1 (`Invitable`). "+
			"More than one means `Outstanding` was reached — which performs NO authority check and returns "+
			"the project's invitations", rig.inviting.reads)
	}

	// POSITIVE CONTROL: an INVITABLE project really does reach `Outstanding`, so the `1`
	// above is a narrowing rather than a handler that never reads anything.
	ok := newInviteRig(t, nil)
	if rec := ok.get(invitePath(fixtureNamedProject.ID)); rec.Code != http.StatusOK {
		t.Fatalf("an invitable project answered %d, want 200", rec.Code)
	}
	if ok.inviting.reads != 2 {
		t.Errorf("an invitable project drew %d authority read(s), want 2 (`Invitable` then `Outstanding`); "+
			"the count above cannot distinguish a narrowing from a handler that reads nothing", ok.inviting.reads)
	}
}

// TestTheMintedTokenIsRenderedOnceUnderNoStoreAndNeverLogged is the capability's own guard.
//
// 🔴 THREE CLAIMS, AND THEY FAIL IN THREE DIFFERENT PLACES, WHICH IS WHY THEY ARE ASSERTED
// SEPARATELY RATHER THAN AS "the page looks right". The token must be IN the body (or the
// mint produced nothing usable), the response must carry `no-store` (or a shared cache may
// keep a page that IS a bearer capability), and the token must NOT be in the log (the
// journal-leak shape this repository has already shipped once, at rank 5).
func TestTheMintedTokenIsRenderedOnceUnderNoStoreAndNeverLogged(t *testing.T) {
	rig := newInviteRig(t, nil)
	form := url.Values{}
	form.Set(FieldProject, string(fixtureNamedProject.ID))
	form.Set(FieldRole, string(control.RoleMember))
	rec := rig.post(InvitePath, form)

	if rec.Code != http.StatusOK {
		t.Fatalf("a mint answered %d, want 200. Body: %q", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()

	// The link, as the page renders it: a PATH plus the token, and no origin.
	wantLink := JoinPath + "?" + url.Values{inviteTokenField: []string{fixtureInviteToken}}.Encode()
	if !strings.Contains(body, wantLink) {
		t.Errorf("the minted page does not carry the join link %q. The token is returned once and cannot "+
			"be re-derived, so a page that does not render it has produced an invitation nobody can use",
			wantLink)
	}
	if n := strings.Count(body, fixtureInviteToken); n != 1 {
		t.Errorf("the token appears %d time(s) in the page, want exactly 1. More than one is a second "+
			"place it can be copied from by accident — most of the ways that happens put it in an href", n)
	}

	// 🔴 NOT AN ANCHOR. `MintedInvite.Link` is a PATH with no origin, so an `<a href>` would
	// resolve it against THIS page and offer the minter a one-click way to redeem the
	// invitation they just created — spending it on themselves. It is also the shape a
	// link-prefetcher follows.
	if strings.Contains(body, `href="`+wantLink) {
		t.Error("the minted link is rendered as an anchor. It resolves against this origin, so one stray " +
			"click — or one link-prefetcher — redeems the invitation on the person who created it")
	}

	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("the mint response's Cache-Control is %q, want \"no-store\". Its BODY is a bearer "+
			"capability that can create a principal", got)
	}
	// ⚠ THE POSITIVE CONTROL THAT STOOD HERE IS RETIRED, NOT FORGOTTEN. It required an ordinary
	// invite page to carry NO `Cache-Control`, proving the assertion above was about the mint
	// response in particular. Since S3 of the mobile plan every HTML page is
	// `no-store` (the one value `writeHTML` sends, walked by `TestEveryNonPublicHTMLRowIsNoStore`), so the
	// mint is no longer special and that control would now assert the opposite of the contract.
	// What keeps the assertion above non-vacuous is that it is a literal value a different
	// default ("" or `no-cache`) fails.

	if logged := rig.log.String(); strings.Contains(logged, fixtureInviteToken) {
		t.Errorf("the minted TOKEN reached the log. Hand-appending a secret to a durable stream is what "+
			"put a 64-character secret in the control journal once already. Log: %q", logged)
	}
	// POSITIVE CONTROL on the log sink: it must have captured SOMETHING, or "the token is
	// not in the log" is a fact about a writer wired to nothing.
	if !strings.Contains(rig.log.String(), "an invitation was minted") {
		t.Fatalf("the log captured no mint line, so the absence above is about the sink rather than about "+
			"the token. Log: %q", rig.log.String())
	}
	// And the line that IS there must carry the digest-free facts an operator needs.
	if !strings.Contains(rig.log.String(), string(fixtureNamedProject.ID)) {
		t.Error("the mint log line does not name the project, so an operator reading the stream cannot " +
			"tell which project gained an invitation")
	}
}

// TestRevokingNamesTheInvitationByDigestAndRedirects pins the write's handle and its hop.
//
// ⚠ THE DIGEST AND NOT A TOKEN, WHICH IS FORCED RATHER THAN CHOSEN: the mint page never
// holds a token (`invite.Store`'s own rule is that every read takes the presented token, and
// `RevokeByDigest` is its one stated exception). A test that passed a token here would be
// asserting a shape the interface cannot serve.
func TestRevokingNamesTheInvitationByDigestAndRedirects(t *testing.T) {
	rig := newInviteRig(t, nil)
	form := url.Values{}
	form.Set(FieldDigest, fixtureInviteDigest)
	form.Set(FieldProject, string(fixtureNamedProject.ID))
	rec := rig.post(InviteRevokePath, form)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("a revoke answered %d, want 303. A write that renders its own answer is a write a "+
			"refresh repeats. Body: %q", rec.Code, rec.Body.String())
	}
	if rig.inviting.lastDigest != fixtureInviteDigest {
		t.Errorf("the authority was asked to revoke %q, want the digest the form carried (%q)",
			rig.inviting.lastDigest, fixtureInviteDigest)
	}
	loc := rec.Header().Get("Location")
	if !strings.Contains(loc, QueryProject+"="+string(fixtureNamedProject.ID)) ||
		!strings.Contains(loc, QueryOutcome+"="+inviteOutcomeRevoked) {
		t.Errorf("the redirect is %q; it must land on the project's own page and carry the outcome CODE", loc)
	}
	// 🔴 AND THE REDIRECT MUST NOT CARRY THE DIGEST. It is not a credential, so this is not
	// a disclosure — it is that a URL in browser history and in every access log en route
	// should carry the minimum, and `redirectToScope` already sets that bar one flow over.
	if strings.Contains(loc, fixtureInviteDigest) {
		t.Errorf("the redirect carries the invitation's digest: %q", loc)
	}

	// A revoke naming no digest is refused BEFORE the authority is reached.
	empty := newInviteRig(t, nil)
	if rec := empty.post(InviteRevokePath, url.Values{}); rec.Code != http.StatusBadRequest {
		t.Errorf("a revoke with no digest answered %d, want 400", rec.Code)
	}
	if empty.inviting.lastDigest != "" {
		t.Errorf("a revoke with no digest still reached the authority (asked for %q)", empty.inviting.lastDigest)
	}
}

// TestTheTwoInviteWriteRefusalsAnswerDIFFERENTLYForDIFFERENTREASONS is `refuseInviteWrite`'s
// mapping, driven through the real dispatcher.
//
// 🔴 ONE OF THE TWO DISCRIMINATES AND ONE DOES NOT, AND A TEST THAT CHECKED ONLY THE STATUS
// WOULD MISS IT. `ErrNotInvitable` is a fact about somebody else's projects and must be
// uniform; `ErrRoleNotConferrable` is a fact about the caller's own standing and must SAY so,
// or a person who picked `owner` from a list is told "that invitation cannot be recorded" and
// can never learn why. Both are 403, so the bodies are the whole claim.
func TestTheTwoInviteWriteRefusalsAnswerDifferentlyForDifferentReasons(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want string
	}{
		{"not invitable", ErrNotInvitable, inviteWriteRefusal},
		{"role not conferrable", ErrRoleNotConferrable, roleRefusal},
	} {
		rig := newInviteRig(t, nil)
		rig.inviting.mintErr = tc.err
		form := url.Values{}
		form.Set(FieldProject, string(fixtureNamedProject.ID))
		form.Set(FieldRole, string(control.RoleOwner))
		rec := rig.post(InvitePath, form)
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s: answered %d, want 403", tc.name, rec.Code)
		}
		if got := strings.TrimSpace(rec.Body.String()); got != tc.want {
			t.Errorf("%s: body is %q, want %q", tc.name, got, tc.want)
		}
	}

	// 🔴 AND AN ERROR THE MAPPING DOES NOT KNOW IS A 500, NOT A 403 — which is what makes an
	// UNWRAPPED sentinel a real defect rather than a wording nit. This is the arm a
	// `fmt.Errorf` with no `%w` falls into.
	rig := newInviteRig(t, nil)
	rig.inviting.mintErr = invite.ErrNotRedeemable
	form := url.Values{}
	form.Set(FieldProject, string(fixtureNamedProject.ID))
	form.Set(FieldRole, string(control.RoleMember))
	if rec := rig.post(InvitePath, form); rec.Code != http.StatusForbidden {
		t.Errorf("ErrNotRedeemable answered %d, want 403 — it is one of the two uniform arms", rec.Code)
	}
}

// TestTheJoinPageNeverConsultsTheInviteAuthority is the public row's safety property, and it
// is a STRUCTURAL claim because nothing observable can distinguish two uniform pages.
//
// 🔴 THE HAZARD: `GET /join` is dispatched BEFORE the authentication chain. A handler that
// resolved the token would answer differently for one that exists and one that does not, on
// an unauthenticated route, at whatever rate a caller cares to drive it — an oracle over
// other people's invitations. A guard comparing two rendered pages cannot see a resolution
// that changes nothing on screen but takes measurably longer, so what is asserted is that the
// authority is not asked AT ALL.
func TestTheJoinPageNeverConsultsTheInviteAuthority(t *testing.T) {
	rig := newInviteRig(t, nil)

	rec := rig.anonymousGet(JoinPath + "?" + inviteTokenField + "=" + fixtureInviteToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("an ANONYMOUS GET %s answered %d, want 200 — an invited person has no session, so a row "+
			"behind the chain is a door that opens only for people already inside", JoinPath, rec.Code)
	}
	if rig.inviting.reads != 0 {
		t.Errorf("the join page consulted the invite authority %d time(s). It must resolve nothing: the "+
			"row is public, so a resolution here is an oracle over which invitations exist",
			rig.inviting.reads)
	}

	// POSITIVE CONTROL: this rig's counter DOES move for a route that asks, so the zero
	// above is a measurement rather than a counter wired to nothing.
	if rig.get(InvitePath); rig.inviting.reads == 0 {
		t.Fatal("the invite page did not move the counter either, so the zero above is about the fixture")
	}

	// And two DIFFERENT tokens render the same page but for the token itself. A byte
	// comparison after substituting each token out is what pins uniformity.
	other := newInviteRig(t, nil)
	a := other.anonymousGet(JoinPath + "?" + inviteTokenField + "=" + fixtureInviteToken).Body.String()
	b := other.anonymousGet(JoinPath + "?" + inviteTokenField + "=some-other-value").Body.String()
	if strings.ReplaceAll(a, fixtureInviteToken, "<T>") != strings.ReplaceAll(b, "some-other-value", "<T>") {
		t.Error("two different tokens render different pages. Whatever the difference is, it is an answer " +
			"about one of the two tokens, on a public route")
	}
	// The substitution must have DONE something, or the comparison above is between two
	// strings neither of which contained a token.
	if !strings.Contains(a, fixtureInviteToken) {
		t.Fatal("the join page does not carry the token at all, so the comparison above compared two " +
			"unmodified strings and proves nothing about uniformity")
	}
}

// TestTheJoinPageWithoutATokenSaysSoAndOffersNoForm is the one distinction that page draws.
//
// ⚠ IT IS NOT AN ORACLE, WHICH IS WHY IT IS ALLOWED WHERE EVERY OTHER DISTINCTION IS NOT:
// "the URL carried no token" is a fact about the caller's own address bar — a truncated
// paste, a retyped link — and no token was presented for the answer to be about.
func TestTheJoinPageWithoutATokenSaysSoAndOffersNoForm(t *testing.T) {
	rig := newInviteRig(t, nil)
	rec := rig.anonymousGet(JoinPath)
	if rec.Code != http.StatusOK {
		t.Fatalf("a bare GET %s answered %d, want 200", JoinPath, rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(pageText(body), "carries no invitation") {
		t.Errorf("a bare join page does not say the link carried no invitation, so a person with a "+
			"truncated link is given no way to tell that IS the problem. Body: %q", body)
	}
	// 🔴 NO ACCEPT FORM, BECAUSE AN EMPTY ONE COMPLETES AS AN ORDINARY SIGN-IN. The flight
	// would carry no invitation, the callback would take the non-provisioning path, and
	// somebody who was invited would end up either signed in as nobody or refused — with
	// nothing anywhere saying the link was at fault.
	if strings.Contains(body, `action="`+OAuthStartPath+`"`) {
		t.Error("a join page with no token still renders the accept form. Submitting it opens a flight " +
			"carrying no invitation, which completes as an ordinary sign-in")
	}

	// POSITIVE CONTROL: with a token, the form IS there — so the absence above is about the
	// missing token and not about a page that never renders a form.
	with := rig.anonymousGet(JoinPath + "?" + inviteTokenField + "=" + fixtureInviteToken).Body.String()
	if !strings.Contains(with, `action="`+OAuthStartPath+`"`) {
		t.Fatal("the join page renders no accept form even WITH a token, so the assertion above is a " +
			"fact about the page rather than about the missing token")
	}
	// The form must post to the provider START row, which is where the invitation is bound
	// onto the server-side flight. A second route here would be a second place a flight is
	// opened — see `joinForm`.
	if !strings.Contains(with, `name="`+inviteTokenField+`"`) {
		t.Error("the accept form does not carry the invitation field, so the token would never reach the " +
			"flight and the redemption branch in `handleOAuthCallback` would be unreachable")
	}
}

// TestTheRoleChooserOffersOnlyWhatTheCallerMayConfer is the escalation rule as the FORM
// expresses it, and it is a different claim from the mint's refusal.
//
// 🔴 THE MINT'S GUARD IS WHAT MAKES IT SAFE; THIS IS WHAT MAKES IT HONEST. `ControlInviting.
// Mint` refuses an `owner` invitation from an admin whatever the form offered — that is the
// security property and `inviting_test.go` measures it. What this measures is that the
// chooser does not OFFER a value the mint will refuse, which is the difference between a form
// and a trap. Both read `control.Role.CanConfer`, which is why the predicate is a method.
func TestTheRoleChooserOffersOnlyWhatTheCallerMayConfer(t *testing.T) {
	for _, tc := range []struct {
		held        control.Role
		wantOffered []control.Role
		wantAbsent  []control.Role
	}{
		{control.RoleOwner, []control.Role{control.RoleOwner, control.RoleAdmin, control.RoleMember}, nil},
		{control.RoleAdmin, []control.Role{control.RoleAdmin, control.RoleMember}, []control.Role{control.RoleOwner}},
	} {
		project := fixtureNamedProject
		project.HeldRole = tc.held
		view := InviteView{
			Viewer:   "operator@example.invalid",
			CSRF:     renderCSRF,
			Projects: []control.NamedProject{project},
			Project:  project,
		}
		html := renderNode(t, InvitePage(view))

		for _, r := range tc.wantOffered {
			if !strings.Contains(html, `value="`+string(r)+`"`) {
				t.Errorf("held=%s: the chooser does not offer %q, so an authority the model permits "+
					"cannot be expressed by the form", tc.held, r)
			}
		}
		for _, r := range tc.wantAbsent {
			if strings.Contains(html, `value="`+string(r)+`"`) {
				t.Errorf("held=%s: the chooser OFFERS %q, which `Mint` will refuse. A form whose visible "+
					"options include one that cannot work is a trap", tc.held, r)
			}
		}
		// 🔴 THE PRE-SELECTED OPTION IS THE LEAST PRIVILEGED ONE, ON BOTH ROWS. A `select`
		// with no explicit selection submits its FIRST option and `control.AllRoles` is in
		// descending authority, so the unread default would be `owner` — a form that hands
		// out ownership of a project to everybody who does not read it.
		if !strings.Contains(html, `<option value="`+string(leastPrivilegedRole)+`" selected>`) {
			t.Errorf("held=%s: %q is not the pre-selected option. The list is in DESCENDING authority, so "+
				"an unselected chooser defaults to the most authority it can confer", tc.held, leastPrivilegedRole)
		}
	}

	// POSITIVE CONTROL on the matcher: a role string this chooser could never offer must be
	// absent, or `strings.Contains` on `value="…"` is matching something else on the page.
	view := InviteView{Viewer: "v", CSRF: renderCSRF, Project: fixtureNamedProject,
		Projects: []control.NamedProject{fixtureNamedProject}}
	if html := renderNode(t, InvitePage(view)); strings.Contains(html, `value="emperor"`) {
		t.Error("the matcher reports a role nothing defines as offered, so its verdicts above are about " +
			"the matcher")
	}
}

// TestTheRevokeButtonIsOfferedOnlyForAnOpenInvitation walks all four states.
//
// ⚠ THE BUTTON'S ABSENCE IS NOT THE GUARD AND THIS TEST DOES NOT CLAIM IT IS.
// `invite.Store.Revoke` refuses a non-open invitation itself and `refuseInviteWrite` answers
// that uniformly; what is measured here is that a person is not offered a control that cannot
// work. Read it as a UX assertion with a security-shaped subject.
func TestTheRevokeButtonIsOfferedOnlyForAnOpenInvitation(t *testing.T) {
	states := map[string]invite.Invite{
		"open":     {Digest: fixtureInviteDigest, ExpiresAt: inviteClock.Add(time.Hour)},
		"expired":  {Digest: fixtureInviteDigest, ExpiresAt: inviteClock.Add(-time.Hour)},
		"revoked":  {Digest: fixtureInviteDigest, ExpiresAt: inviteClock.Add(time.Hour), RevokedAt: inviteClock},
		"redeemed": {Digest: fixtureInviteDigest, ExpiresAt: inviteClock.Add(time.Hour), RedeemedAt: inviteClock},
	}
	// The four names are the closed set `invite.State` defines, asserted rather than assumed
	// so a fifth state cannot be added without this walk growing.
	for _, want := range []invite.State{invite.StateOpen, invite.StateExpired, invite.StateRevoked, invite.StateRedeemed} {
		if _, named := states[string(want)]; !named {
			t.Fatalf("invite.State %q has no fixture here, so this walk does not cover it", want)
		}
	}

	for name, inv := range states {
		inv.ProjectID = fixtureNamedProject.ID
		inv.Role = control.RoleMember
		rows := inviteRows([]invite.Invite{inv}, inviteClock)
		if len(rows) != 1 {
			t.Fatalf("%s: inviteRows produced %d row(s), want 1", name, len(rows))
		}
		if rows[0].State != name {
			t.Fatalf("%s: the row's state is %q — the fixture does not produce the state it is named for, "+
				"so the assertion below is about a different case", name, rows[0].State)
		}
		view := InviteView{Viewer: "v", CSRF: renderCSRF, Project: fixtureNamedProject,
			Projects: []control.NamedProject{fixtureNamedProject}, Outstanding: rows}
		html := renderNode(t, InvitePage(view))

		hasButton := strings.Contains(html, `action="`+InviteRevokePath+`"`)
		if name == "open" && !hasButton {
			t.Errorf("an OPEN invitation offers no revoke button, so an invitation could be created and "+
				"never withdrawn (%s)", name)
		}
		if name != "open" && hasButton {
			t.Errorf("a %s invitation offers a revoke button that cannot work", name)
		}
		// Whatever the state, the row must SAY which one it is — that is the whole reason
		// spent rows are listed rather than filtered out.
		if !strings.Contains(pageText(html), name) {
			t.Errorf("%s: the row does not render its state, so a listing that includes spent rows is "+
				"indistinguishable from one that does not", name)
		}
		// And the full digest must appear only where a form needs it.
		if name != "open" && strings.Contains(html, fixtureInviteDigest) {
			t.Errorf("%s: the full digest is rendered on a row with no form to carry it", name)
		}
	}
}

// TestTheInviteHonestyNoticeIsPinnedWhole is `ReplicaHonesty`'s guard applied to the second
// notice this surface makes, and it is pinned the same way and for the same reason.
//
// 🔴 A GUARD ON WORDS IS WALKABLE BY REWORDING, AND THE CLAUSE MOST WORTH DROPPING IS ALWAYS
// THE ONE THAT MAKES THE PRODUCT SOUND WEAKEST. Here that is "takes nothing back from
// somebody who has already joined" — the sentence that says revoking an invitation is not
// revoking access. So the WHOLE normalised string is compared, entities resolved and
// whitespace collapsed, and a cosmetic reword FAILS this test on purpose.
func TestTheInviteHonestyNoticeIsPinnedWhole(t *testing.T) {
	// 🔴 A LITERAL, NOT `normalizeSpace(InviteHonesty)`, AND THIS IS THE DEFECT THE SWEEP
	// FOUND IN MY OWN GUARD. The first version compared the page against the CONSTANT, so both
	// sides moved together and a reword was invisible — the battery row
	// `ui-invite-honesty-notice-loses-its-weakest-clause` SURVIVED, and the battery's verdict
	// for that is "a guard the suite does not actually have". The constant's own comment
	// claimed "a cosmetic reword FAILS this test on purpose", which was false as implemented.
	//
	// ⚠ THE SAME HOLE WAS THEN FOUND IN `TestTheReplicaHonestyNoticeIsPinnedWhole`, which this
	// guard was modelled on and which had shipped. Both are fixed the same way, and the
	// duplication between the literal and the constant is the MECHANISM: two places, one
	// reviewer, for a change to what this surface promises.
	const wantLiteral = "An invitation is a link, and the link is the authority: it is shown " +
		"once and cannot be recovered, anyone who holds it can use it to join this project, and " +
		"revoking it stops it being redeemed but takes nothing back from somebody who has " +
		"already joined."
	want := normalizeSpace(wantLiteral)
	if want == "" {
		t.Fatal("the pinned literal is empty, so the comparison below is vacuous")
	}
	if got := normalizeSpace(InviteHonesty); got != want {
		t.Errorf("`InviteHonesty` no longer reads as the pinned sentence.\n got: %q\nwant: %q\n"+
			"If the wording changed ON PURPOSE, change both — that two-place edit IS the gate.",
			got, want)
	}
	for name, view := range map[string]InviteView{
		"index":   {Viewer: "v", CSRF: renderCSRF, Projects: []control.NamedProject{fixtureNamedProject}},
		"project": {Viewer: "v", CSRF: renderCSRF, Project: fixtureNamedProject, Projects: []control.NamedProject{fixtureNamedProject}},
		"minted": {Viewer: "v", CSRF: renderCSRF, Project: fixtureNamedProject,
			Minted: &MintedInvite{Link: JoinPath + "?invite=x", Role: control.RoleMember, Expires: "2000-01-09T03:04:05Z"}},
		"no-store": {Viewer: "v", CSRF: renderCSRF, NoStore: true},
	} {
		got := pageText(renderNode(t, InvitePage(view)))
		if !strings.Contains(got, want) {
			t.Errorf("%s: the page does not carry the whole notice.\nwant: %q\n got: %q", name, want, got)
		}
	}

	// POSITIVE CONTROL: the comparison can FAIL. A `strings.Contains` against a normalised
	// page is exactly the shape that silently always passes if the normalisation collapses
	// too much.
	if strings.Contains(pageText(renderNode(t, InvitePage(InviteView{Viewer: "v"}))),
		normalizeSpace(InviteHonesty+" and one clause nobody wrote")) {
		t.Error("the comparison is satisfied by a string the page does not contain, so its verdicts above " +
			"are about the matcher")
	}
}

// TestTheInviteIndexSaysAnEmptyListIsAnAuthorityAnswer pins the sentence the authority walk
// licenses.
//
// 🔴 IT IS THE SAME CLAIM `Page`'s EMPTY BRANCH MAKES, AND THAT ONE SHIPPED AS A LIE. A
// handler that rendered this without asking told an operator with authority over everything
// that they had none. Here the sentence is pinned AND `TestEveryContentRouteConsultsTheAuthority`
// requires the route to have asked — two guards, because either alone is satisfiable by a
// handler that does the wrong half.
func TestTheInviteIndexSaysAnEmptyListIsAnAuthorityAnswer(t *testing.T) {
	rig := newInviteRig(t, func(cfg *Config) {
		empty := benignInviting()
		empty.invitable = nil
		cfg.Inviting = empty
	})
	rec := rig.get(InvitePath)
	if rec.Code != http.StatusOK {
		t.Fatalf("an empty invite index answered %d, want 200", rec.Code)
	}
	text := pageText(rec.Body.String())
	if !strings.Contains(text, "That is an authority answer, not an empty control plane") {
		t.Errorf("an empty index does not say the emptiness is an AUTHORITY answer. \"nothing here\" and "+
			"\"nothing you may see\" are different facts with different next actions. Body: %q", text)
	}
	// POSITIVE CONTROL: a NON-empty index does not carry that sentence, so the assertion
	// above is about the empty branch.
	full := newInviteRig(t, nil)
	if strings.Contains(pageText(full.get(InvitePath).Body.String()), "That is an authority answer") {
		t.Error("a populated index also renders the empty-branch sentence")
	}
}
