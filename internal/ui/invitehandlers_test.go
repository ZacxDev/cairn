package ui

import (
	"bytes"
	"errors"
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
	return TeamPath + "?" + QueryProject + "=" + string(project)
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
		// `POST /invite` creates nothing since the fold and is STILL behind both gates: they
		// are derived from the method, not from what the handler does.
		{"folded mint", InvitePath},
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
		if rig.inviting.revoked != 0 {
			t.Fatalf("%s: a refused request reached the authority (revoked %d)", w.name, rig.inviting.revoked)
		}
	}

	// The controls, spelled as requests that must get PAST both gates: the revoke reaches the
	// authority, and the folded mint reaches its handler's own answer (a 303, never a 403).
	rig := newInviteRig(t, nil)
	form := url.Values{}
	form.Set(FieldProject, string(fixtureNamedProject.ID))
	form.Set(FieldDigest, fixtureInviteDigest)
	if rec := rig.post(InviteRevokePath, form); rec.Code != http.StatusSeeOther || rig.inviting.revoked != 1 {
		t.Fatalf("the positive control failed: a correctly-gated revoke answered %d and reached the authority "+
			"%d time(s), want 303 and 1. Every write assertion in this file would then be about a cross-site "+
			"refusal. Body: %q", rec.Code, rig.inviting.revoked, rec.Body.String())
	}
	if rec := rig.post(InvitePath, form); rec.Code != http.StatusSeeOther {
		t.Fatalf("the positive control failed: a correctly-gated POST %s answered %d, want its handler's 303. "+
			"Body: %q", InvitePath, rec.Code, rec.Body.String())
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
		// No database: no invitation half, and so no link half (`Inviting.TeamLinks`).
		cfg.Inviting = nil
	})

	rec := rig.get(TeamPath)
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
	form.Set(FieldDigest, fixtureInviteDigest)
	for _, path := range []string{InvitePath, InviteRevokePath} {
		if rec := wired.post(path, form); rec.Code == http.StatusNotImplemented {
			t.Errorf("POST %s against a WIRED invite store also answered 501, so the assertions above are a "+
				"fact about the request and not about the missing store", path)
		}
	}
}

// TestAProjectThatIsNotInvitableIsRefusedBEFORETheUnnarrowedReAD is the narrowing seam, and
// it asserts a RELATIONSHIP rather than a status.
//
// 🔴 `Inviting.Outstanding` PERFORMS NO AUTHORITY CHECK AND SAYS SO IN ITS OWN DOC. It
// returns digests, roles and timestamps for any project it is handed — a listing that would
// tell an outsider who is being invited where. The only thing standing in front of it is
// that `inviteSection` reaches it exclusively for a project `Invitable` returned. A guard
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

// TestTheInviteRevokeRefusalsAreUniform is `refuseInviteWrite`'s mapping, driven through the
// real dispatcher.
//
// 🔴 BOTH ARMS ANSWER ONE STATUS AND ONE SENTENCE, AND THE BODY IS THE CLAIM. `ErrNotInvitable`
// is "no such project, or not yours" and `ErrNotRedeemable` is "no such invitation, or no
// longer open"; answering them differently would let a revoke ask whether a digest exists.
// (The third arm this mapping had — a role the caller could not confer — went with the mint.)
func TestTheInviteRevokeRefusalsAreUniform(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
	}{
		{"not invitable", ErrNotInvitable},
		{"not redeemable", invite.ErrNotRedeemable},
	} {
		rig := newInviteRig(t, nil)
		rig.inviting.revokeErr = tc.err
		rec := rig.post(InviteRevokePath, url.Values{FieldDigest: {fixtureInviteDigest}})
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s: answered %d, want 403", tc.name, rec.Code)
		}
		if got := strings.TrimSpace(rec.Body.String()); got != inviteWriteRefusal {
			t.Errorf("%s: body is %q, want %q", tc.name, got, inviteWriteRefusal)
		}
	}

	// 🔴 AND AN ERROR THE MAPPING DOES NOT KNOW IS A 500, NOT A 403 — the arm a store failure
	// falls into, and the one that must not carry the store's own text to the wire.
	rig := newInviteRig(t, nil)
	rig.inviting.revokeErr = errors.New("fixture store failure naming a table")
	rec := rig.post(InviteRevokePath, url.Values{FieldDigest: {fixtureInviteDigest}})
	if rec.Code != http.StatusInternalServerError || strings.Contains(rec.Body.String(), "fixture store failure") {
		t.Errorf("an unknown error answered %d %q, want a 500 that does not carry its text", rec.Code, rec.Body.String())
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
	if rig.get(TeamPath); rig.inviting.reads == 0 {
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
//
// 🔴 SINCE THE FRAGMENT, A BARE `GET /join` IS ALSO WHAT EVERY NEW LINK OPENS — the token is after
// the `#`, which the server never sees. So the server renders the no-invitation sentence and the
// accept form BOTH `hidden`, and `join.js` reveals exactly one of them; what this pins is that the
// server never renders a VISIBLE form with an empty token (the script's half is
// `TestTheJoinScriptTouchesOnlyWhatItSays`).
func TestTheJoinPageWithoutATokenSaysSoAndOffersNoForm(t *testing.T) {
	rig := newInviteRig(t, nil)
	rec := rig.anonymousGet(JoinPath)
	if rec.Code != http.StatusOK {
		t.Fatalf("a bare GET %s answered %d, want 200", JoinPath, rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(pageText(body), "carries no invitation") {
		t.Errorf("a bare join page does not carry the no-invitation sentence for the script to reveal, so a "+
			"person with a truncated link is given no way to tell that IS the problem. Body: %q", body)
	}
	// 🔴 NO VISIBLE ACCEPT FORM, BECAUSE AN EMPTY ONE COMPLETES AS AN ORDINARY SIGN-IN. The flight
	// would carry no invitation, the callback would take the non-provisioning path, and
	// somebody who was invited would end up either signed in as nobody or refused — with
	// nothing anywhere saying the link was at fault. The one form on the page must sit inside
	// the `hidden` accept block.
	formAt := strings.Index(body, `action="`+OAuthStartPath+`"`)
	blockAt := strings.Index(body, `<div id="join-accept" hidden>`)
	if strings.Count(body, `action="`+OAuthStartPath+`"`) != 1 || blockAt < 0 || formAt < blockAt ||
		strings.Contains(body[blockAt:formAt], "</div>") {
		t.Error("a join page with no token renders an accept form OUTSIDE the hidden accept block. Submitting " +
			"it opens a flight carrying no invitation, which completes as an ordinary sign-in")
	}

	// POSITIVE CONTROL: with a QUERY token the form is there and VISIBLE — so the hiding above is
	// about the missing token and not about a page that never renders a visible form.
	with := rig.anonymousGet(JoinPath + "?" + inviteTokenField + "=" + fixtureInviteToken).Body.String()
	if !strings.Contains(with, `action="`+OAuthStartPath+`"`) || strings.Contains(with, `<div id="join-accept" hidden>`) {
		t.Fatal("the join page renders no visible accept form even WITH a token, so the assertion above is a " +
			"fact about the page rather than about the missing token")
	}
	// The form must post to the provider START row, which is where the invitation is bound
	// onto the server-side flight — in the LEGACY field, which marks the flight query-borne
	// (`inviteQueryTokenField`). A second route here would be a second place a flight is
	// opened — see `joinForm`.
	if !strings.Contains(with, `name="`+inviteQueryTokenField+`" value="`+fixtureInviteToken+`"`) {
		t.Error("the query link's accept form does not carry the token in the legacy field, so the callback " +
			"cannot tell a query-borne token from a fragment one")
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
		html := renderNode(t, inviteOnTeam(view))

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
		"index":    {Viewer: "v", CSRF: renderCSRF, Projects: []control.NamedProject{fixtureNamedProject}},
		"project":  {Viewer: "v", CSRF: renderCSRF, Project: fixtureNamedProject, Projects: []control.NamedProject{fixtureNamedProject}},
		"no-store": {Viewer: "v", CSRF: renderCSRF, NoStore: true},
	} {
		got := pageText(renderNode(t, inviteOnTeam(view)))
		if !strings.Contains(got, want) {
			t.Errorf("%s: the page does not carry the whole notice.\nwant: %q\n got: %q", name, want, got)
		}
	}

	// POSITIVE CONTROL: the comparison can FAIL. A `strings.Contains` against a normalised
	// page is exactly the shape that silently always passes if the normalisation collapses
	// too much.
	if strings.Contains(pageText(renderNode(t, inviteOnTeam(InviteView{Viewer: "v"}))),
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
	rec := rig.get(TeamPath)
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
	if strings.Contains(pageText(full.get(TeamPath).Body.String()), "That is an authority answer") {
		t.Error("a populated index also renders the empty-branch sentence")
	}
}
