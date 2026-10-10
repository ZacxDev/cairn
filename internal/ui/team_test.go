package ui

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/ZacxDev/cairn/internal/identity"
	"github.com/ZacxDev/cairn/internal/invite"
)

// newTeamHTTPRig is `newInviteRig` with the Team page's link fixture exposed, so a case can
// steer it and read what a handler passed through.
func newTeamHTTPRig(t *testing.T, mutate func(*Config)) (*inviteRig, *staticTeamLinks) {
	t.Helper()
	rig := newInviteRig(t, mutate)
	return rig, rig.inviting.team
}

func linkForm(targets ...invite.Target) url.Values {
	form := url.Values{FieldRole: {string(invite.LinkReader)}, FieldTTLDays: {"7"}}
	for _, t := range targets {
		form.Add(FieldTarget, targetValue(t))
	}
	return form
}

// TestTheTeamLinkRowsAreBehindBothCrossSiteGates is the instrument control for every POST
// below — `TestTheInviteWritesAreBehindBothCrossSiteGates`' reason: without it the rig's
// `post` is an unvalidated harness. Each write row is driven with no Origin, a foreign
// Origin, no CSRF token and another session's token, and each must be refused by THAT
// gate's own message with the authority never reached; then correctly, as the positive
// control.
func TestTheTeamLinkRowsAreBehindBothCrossSiteGates(t *testing.T) {
	for _, path := range []string{TeamLinkPath, TeamLinkRevokePath} {
		for _, arm := range []struct {
			name, origin, csrf, want string
		}{
			{"no Origin", "", identity.CSRFTokenFor(inviteCookieValue), crossSiteRefusal},
			{"a foreign Origin", "https://evil.invalid", identity.CSRFTokenFor(inviteCookieValue), crossSiteRefusal},
			{"no CSRF token", "same", "", csrfRefusal},
			{"another session's token", "same", identity.CSRFTokenFor("some-other-session-cookie"), csrfRefusal},
		} {
			t.Run(path+"/"+arm.name, func(t *testing.T) {
				rig, links := newTeamHTTPRig(t, nil)
				form := linkForm(fixtureScopeTarget())
				form.Set(FieldDigest, fixtureLinkDigest)
				if arm.csrf != "" {
					form.Set(FieldCSRF, arm.csrf)
				}
				req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
				req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				req.AddCookie(&http.Cookie{Name: identity.SessionCookieName, Value: inviteCookieValue})
				switch arm.origin {
				case "":
				case "same":
					req.Header.Set("Origin", "https://"+req.Host)
				default:
					req.Header.Set("Origin", arm.origin)
				}
				rec := httptest.NewRecorder()
				rig.srv.ServeHTTP(rec, req)
				if rec.Code != http.StatusForbidden || rec.Body.String() != arm.want {
					t.Errorf("answered %d %q, want 403 %q", rec.Code, rec.Body.String(), arm.want)
				}
				if links.minted != 0 || links.revoked != 0 || links.lastActor.ID != "" {
					t.Errorf("a REFUSED request reached the authority (minted %d, revoked %d)", links.minted, links.revoked)
				}
			})
		}
	}
	// POSITIVE CONTROL: the rig gets past both gates and reaches each handler.
	rig, links := newTeamHTTPRig(t, nil)
	if rec := rig.post(TeamLinkPath, linkForm(fixtureScopeTarget())); rec.Code != http.StatusOK || links.minted != 1 {
		t.Fatalf("POSITIVE CONTROL FAILED: a gated mint answered %d, minted %d", rec.Code, links.minted)
	}
	if rec := rig.post(TeamLinkRevokePath, url.Values{FieldDigest: {fixtureLinkDigest}}); rec.Code != http.StatusSeeOther || links.revoked != 1 {
		t.Fatalf("POSITIVE CONTROL FAILED: a gated revoke answered %d, revoked %d", rec.Code, links.revoked)
	}
}

// TestTheTeamLinkFormPassesEveryTickedTargetThrough — every ticked box, the role, the
// lifetime and the reuse tick reach `Mint` unchanged, and the token is rendered once.
func TestTheTeamLinkFormPassesEveryTickedTargetThrough(t *testing.T) {
	rig, links := newTeamHTTPRig(t, nil)
	form := linkForm(fixtureProjectTarget(), fixtureScopeTarget())
	form.Set(FieldRole, string(invite.LinkMember))
	form.Set(FieldTTLDays, "3")
	form.Set(FieldReuse, "1")
	rec := rig.post(TeamLinkPath, form)
	if rec.Code != http.StatusOK {
		t.Fatalf("the mint answered %d: %s", rec.Code, rec.Body.String())
	}
	if len(links.lastTargets) != 2 || links.lastTargets[0] != fixtureProjectTarget() || links.lastTargets[1] != fixtureScopeTarget() {
		t.Errorf("Mint was given targets %v, want exactly the two ticked", links.lastTargets)
	}
	if links.lastRole != invite.LinkMember || links.lastTTL != 3*24*time.Hour || !links.lastReusable {
		t.Errorf("Mint was given role=%s ttl=%v reusable=%v, want member 72h true", links.lastRole, links.lastTTL, links.lastReusable)
	}
	if links.lastActor.ID != testIdentity().Principal.ID {
		t.Errorf("Mint acted as %q, want the caller", links.lastActor.ID)
	}
	body := rec.Body.String()
	wantLink := JoinPath + "?" + url.Values{inviteTokenField: {fixtureLinkToken}}.Encode()
	if strings.Count(body, wantLink) != 1 {
		t.Errorf("the link %q appears %d time(s) on the page, want exactly once", wantLink, strings.Count(body, wantLink))
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("the response carrying a capability has Cache-Control %q, want no-store", got)
	}
	// An unticked reuse box is single use.
	rig.post(TeamLinkPath, linkForm(fixtureScopeTarget()))
	if links.lastReusable {
		t.Error("a form with the reuse box UNTICKED minted a reusable link")
	}
}

// TestAMalformedTeamLinkFormIsRefusedBeforeTheAuthority: a bad target, a lifetime outside
// 1..30 days, or no target at all is a 400 and never reaches `Mint`.
func TestAMalformedTeamLinkFormIsRefusedBeforeTheAuthority(t *testing.T) {
	for name, mutate := range map[string]func(url.Values){
		"no target":        func(f url.Values) { f.Del(FieldTarget) },
		"a bare id":        func(f url.Values) { f.Set(FieldTarget, string(fixtureScope)) },
		"an unknown kind":  func(f url.Values) { f.Set(FieldTarget, "group:"+string(fixtureScope)) },
		"zero days":        func(f url.Values) { f.Set(FieldTTLDays, "0") },
		"past the ceiling": func(f url.Values) { f.Set(FieldTTLDays, "31") },
		"not a number":     func(f url.Values) { f.Set(FieldTTLDays, "7d") },
		"a duplicate box":  func(f url.Values) { f.Add(FieldTarget, targetValue(fixtureScopeTarget())) },
	} {
		t.Run(name, func(t *testing.T) {
			rig, links := newTeamHTTPRig(t, nil)
			form := linkForm(fixtureScopeTarget())
			mutate(form)
			rec := rig.post(TeamLinkPath, form)
			if rec.Code != http.StatusBadRequest || rec.Body.String() != teamWriteRefusal {
				t.Errorf("answered %d %q, want 400 %q", rec.Code, rec.Body.String(), teamWriteRefusal)
			}
			if links.lastActor.ID != "" {
				t.Error("a malformed form reached Mint")
			}
		})
	}
	// The ceiling itself is accepted.
	rig, links := newTeamHTTPRig(t, nil)
	form := linkForm(fixtureScopeTarget())
	form.Set(FieldTTLDays, "30")
	if rec := rig.post(TeamLinkPath, form); rec.Code != http.StatusOK || links.lastTTL != invite.MaxLinkTTL {
		t.Errorf("30 days answered %d with ttl %v, want 200 and the ceiling", rec.Code, links.lastTTL)
	}
}

// TestTheTeamWriteRefusalsAreUniform: "not yours", "no such target", "not your link" and "no
// such link" are ONE status and ONE body, so the writes are not an oracle.
func TestTheTeamWriteRefusalsAreUniform(t *testing.T) {
	var bodies []string
	for _, err := range []error{ErrNotLinkable, invite.ErrNotRedeemable, errors.Join(ErrNotLinkable, errors.New("detail"))} {
		rig, links := newTeamHTTPRig(t, nil)
		links.mintErr, links.revokeErr = err, err
		for _, rec := range []*httptest.ResponseRecorder{
			rig.post(TeamLinkPath, linkForm(fixtureScopeTarget())),
			rig.post(TeamLinkRevokePath, url.Values{FieldDigest: {fixtureLinkDigest}}),
		} {
			if rec.Code != http.StatusForbidden {
				t.Errorf("%v answered %d, want 403", err, rec.Code)
			}
			bodies = append(bodies, rec.Body.String())
		}
	}
	for _, b := range bodies {
		if b != teamWriteRefusal {
			t.Errorf("a refusal said %q, want the uniform %q", b, teamWriteRefusal)
		}
	}
	rig, links := newTeamHTTPRig(t, nil)
	links.mintErr = errors.New("a store failure carrying a deployment detail")
	if rec := rig.post(TeamLinkPath, linkForm(fixtureScopeTarget())); rec.Code != http.StatusInternalServerError ||
		strings.Contains(rec.Body.String(), "deployment detail") {
		t.Errorf("a store failure answered %d %q, want 500 without the store's text", rec.Code, rec.Body.String())
	}
}

// TestTheTeamPageAnswersHonestlyWithNoStore — the journal/token-file deployment: no
// database, so no invitation half and no link half. The page answers 200 and says
// `NoInviteStore` where the invite list and the link form would be; the writes answer 501.
func TestTheTeamPageAnswersHonestlyWithNoStore(t *testing.T) {
	rig, _ := newTeamHTTPRig(t, func(cfg *Config) { cfg.Inviting = nil })
	rec := rig.get(TeamPath)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /team with no store answered %d, want 200", rec.Code)
	}
	body := pageText(rec.Body.String())
	if got := strings.Count(body, normalizeSpace(NoInviteStore)); got != 2 {
		t.Errorf("the page says NoInviteStore %d time(s), want 2 (the invite list and the link half)", got)
	}
	if strings.Contains(rec.Body.String(), `action="`+TeamLinkPath+`"`) {
		t.Error("a page with no store rendered the link form, which posts to a row that answers 501")
	}
	// The SHARE half still answers: it needs no database.
	if !strings.Contains(rec.Body.String(), `href="`+teamHref(QueryScope+"="+string(fixtureNamedScope.ID), teamShareAnchor)+`"`) {
		t.Error("the share list vanished with the database, but sharing does not need one")
	}
	for _, path := range []string{TeamLinkPath, TeamLinkRevokePath} {
		form := linkForm(fixtureScopeTarget())
		form.Set(FieldDigest, fixtureLinkDigest)
		if rec := rig.post(path, form); rec.Code != http.StatusNotImplemented || rec.Body.String() != NoInviteStore {
			t.Errorf("POST %s with no store answered %d %q, want 501 NoInviteStore", path, rec.Code, rec.Body.String())
		}
	}
}

// TestTheTeamPageRendersEveryLinkWithItsLog — the minter's list: state, reuse, count, each
// target, each redemption, and a revoke form only for an OPEN link.
func TestTheTeamPageRendersEveryLinkWithItsLog(t *testing.T) {
	rig, links := newTeamHTTPRig(t, nil)
	rec := rig.get(TeamPath)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /team answered %d", rec.Code)
	}
	text := pageText(rec.Body.String())
	for _, want := range []string{
		shortDigest(fixtureLinkDigest), "reusable", "redeemed 2 time(s)", "project " + fixtureNamedProject.Name,
		"#1 wren@notes.example.invalid joined (account created by this link)",
		// 🔴 THE UNCONFIRMED SPEND IS RENDERED AS AN ATTEMPT, NEVER AS A JOIN (round 1 🟡1).
		"#2 an attempt by usr_fixture_never_created at",
		"NOT confirmed: no join was recorded for it",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("the page does not carry %q", want)
		}
	}
	if !strings.Contains(rec.Body.String(), `action="`+TeamLinkRevokePath+`"`) {
		t.Error("an OPEN link rendered no revoke form")
	}
	// A spent single-use link renders no revoke button.
	links.links[0].Link.Reusable = false
	rec = rig.get(TeamPath)
	if strings.Contains(rec.Body.String(), `action="`+TeamLinkRevokePath+`"`) {
		t.Error("a SPENT single-use link rendered a revoke form for a revoke that cannot work")
	}
	// The chooser offers each mintable target, and the role default is the least privileged.
	for _, m := range benignTeamLinks().mintable {
		if !strings.Contains(rec.Body.String(), `value="`+targetValue(m.Target)+`"`) {
			t.Errorf("the chooser does not offer %v", m.Target)
		}
	}
	if !strings.Contains(rec.Body.String(), `<option value="reader" selected>`) {
		t.Error("the role chooser's default is not `reader`")
	}
	if strings.Contains(rec.Body.String(), `name="`+FieldReuse+`" value="1" checked`) {
		t.Error("the reuse box is ticked by default")
	}
}

// TestTheTeamHonestyNoticeIsPinnedWhole — `TestTheInviteHonestyNoticeIsPinnedWhole`'s
// ruling: a LITERAL here, so a reword of the constant fails, and the page carries it whole
// on every shape. RED under `ui-team-honesty-notice-loses-its-reuse-clause`.
func TestTheTeamHonestyNoticeIsPinnedWhole(t *testing.T) {
	const wantLiteral = "A team link is a link, and the link is the authority: it is shown once " +
		"and cannot be recovered, and anyone who holds it can use it to join every project and " +
		"scope it names. A link that allows reuse can be redeemed by any number of people, any " +
		"number of times, until it expires or is revoked. A link stops working if whoever created " +
		"it loses the authority it confers, and revoking it stops further redemptions but takes " +
		"nothing back from anybody who already joined."
	want := normalizeSpace(wantLiteral)
	if got := normalizeSpace(TeamHonesty); got != want {
		t.Errorf("`TeamHonesty` no longer reads as the pinned sentence.\n got: %q\nwant: %q", got, want)
	}
	for name, view := range map[string]TeamView{
		"index":    {Viewer: "v", CSRF: renderCSRF},
		"no-store": {Viewer: "v", CSRF: renderCSRF, NoInviteStore: true},
		"minted":   {Viewer: "v", CSRF: renderCSRF, Minted: &MintedTeamLink{Link: JoinPath + "?invite=x"}},
	} {
		if got := pageText(renderNode(t, TeamPage(view))); !strings.Contains(got, want) {
			t.Errorf("%s: the page does not carry the whole notice", name)
		}
	}
	// POSITIVE CONTROL: the comparison can fail.
	if strings.Contains(pageText(renderNode(t, TeamPage(TeamView{Viewer: "v"}))), want+" extra") {
		t.Fatal("POSITIVE CONTROL FAILED: the containment check matched a string the page cannot carry")
	}
}

// TestAnInvitationHalfWithoutTeamLinksIsRefused — `ErrInvitingWithoutTeamLinks`. The shape it
// refuses is the one round 1 measured shipping green: a `ControlInviting` with no `Links`
// serves every invite page and hands no link token anywhere, so every team link refuses at the
// callback. Both the REAL type and the fixture are driven, because the fixture is what every
// dispatch test builds from.
func TestAnInvitationHalfWithoutTeamLinksIsRefused(t *testing.T) {
	r := newTeamRig(t)
	for name, inviting := range map[string]Inviting{
		"ControlInviting with no Links": ControlInviting{Authority: r.authority, Invites: r.invites},
		"the fixture with no team":      &staticInviting{},
	} {
		cfg := testConfig(t, staticAuth{testIdentity()})
		cfg.Inviting = inviting
		if _, err := New(cfg); !errors.Is(err, ErrInvitingWithoutTeamLinks) {
			t.Errorf("%s: a half-wired server built (err=%v) — every team link it minted would be unredeemable", name, err)
		}
	}
	// POSITIVE CONTROL: the same real type WITH its link half builds, and the server's link
	// half is that very object.
	cfg := testConfig(t, staticAuth{testIdentity()})
	cfg.Inviting = r.inviting
	srv, err := New(cfg)
	if err != nil {
		t.Fatalf("POSITIVE CONTROL FAILED: a fully wired server was refused: %v", err)
	}
	if got, ok := srv.teamLinks.(ControlTeamLinks); !ok || got.Store != r.team.Store {
		t.Fatalf("the server's link half is %T, not the ControlInviting's own Links", srv.teamLinks)
	}
}

// TestANarrowedBearerHasNoTeamLinkAuthority drives the REAL `ControlTeamLinks` through the
// REAL chain: a credential narrowed to one scope must not reach membership authority
// through the Team page — no target offered, and a hand-made mint refused.
func TestANarrowedBearerHasNoTeamLinkAuthority(t *testing.T) {
	authority := narrowedWorld(t)
	links := &ControlTeamLinks{Authority: authority, Store: newMemLinks(), Now: func() time.Time { return fixtureClock }}
	inviting := ControlInviting{Authority: authority, Invites: newMemInvites(), Now: func() time.Time { return fixtureClock }, Links: links}
	box := `value="` + targetValue(invite.Target{Kind: invite.TargetProject, ID: fixtureProject}) + `"`

	// POSITIVE CONTROL: the un-narrowed bearer is offered the project and may mint over it.
	l := newLiveOver(t, authority, inviting, nil, nil)
	if rec := l.bearerDo(http.MethodGet, TeamPath, testCredential, nil); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), box) {
		t.Fatalf("POSITIVE CONTROL FAILED: the un-narrowed bearer's Team page answered %d without the project box", rec.Code)
	}
	form := url.Values{FieldTarget: {"project:" + string(fixtureProject)}, FieldRole: {"reader"}, FieldTTLDays: {"1"}}
	if rec := l.bearerDo(http.MethodPost, TeamLinkPath, testCredential, form); rec.Code != http.StatusOK {
		t.Fatalf("POSITIVE CONTROL FAILED: the un-narrowed bearer's mint answered %d %q", rec.Code, rec.Body.String())
	}
	for _, bearer := range narrowedBearers {
		l := newLiveOver(t, authority, inviting, nil, nil)
		if rec := l.bearerDo(http.MethodGet, TeamPath, bearer, nil); strings.Contains(rec.Body.String(), box) {
			t.Fatalf("a NARROWED bearer was offered its owner's project on a team link (status %d)", rec.Code)
		}
		if rec := l.bearerDo(http.MethodPost, TeamLinkPath, bearer, form); rec.Code != http.StatusForbidden {
			t.Fatalf("a NARROWED bearer minted a team link over its owner's project: %d %q", rec.Code, rec.Body.String())
		}
	}
}

// TestAReusableTeamLinkProvisionsEveryStrangerThroughTheCallback is the SEAM: the real
// dispatcher, the real flight carrying the token, the real `ControlInviting` handing an
// unknown token to the real `ControlTeamLinks`. Two strangers redeem one reusable link and
// both come out as principals with the link's authority, and the log names both.
func TestAReusableTeamLinkProvisionsEveryStrangerThroughTheCallback(t *testing.T) {
	r := newTeamRig(t)
	token, link := r.mint(invOwner, invite.LinkReader, true, scopeT(tlA))

	cfg := testConfig(t, refusingAuth{})
	stub := &stubOAuth{}
	cfg.OAuth = stub
	cfg.Inviting = r.inviting

	srv, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 2; i++ {
		stub.err = &identity.UnprovisionedSubject{Provider: "fixture-provider", Subject: strangerSubject(i)}
		cookie := startAFlightCarryingAnInvite(t, srv, token)
		cb := httptest.NewRequest("GET", OAuthCallbackPath+"?code=fixture-code", nil)
		cb.AddCookie(cookie)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, cb)
		if rec.Code != http.StatusSeeOther {
			t.Fatalf("stranger %d: the callback answered %d: %s", i, rec.Code, rec.Body.String())
		}
		user, held := r.authority.Model().UserByProviderSubject("fixture-provider", strangerSubject(i))
		if !held {
			t.Fatalf("stranger %d was not provisioned by the reusable link", i)
		}
		if got := r.authorityOf(user.ID); got != string(tlA)+"=read" {
			t.Errorf("stranger %d holds %q, want exactly %s=read", i, got, tlA)
		}
	}
	log, _ := r.links.LinkRedemptions(link.Digest)
	if len(log) != 2 || !log[0].Provisioned || !log[1].Provisioned {
		t.Errorf("the redemption log is %+v, want two provisioning rows", log)
	}
}
