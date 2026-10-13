package ui

import (
	"bytes"
	"context"
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

// # THE FOLD: single-project invitations are no longer minted; they are team links.
//
// Operator decision (option A): the Team page keeps ONE mint form (the team-link form, the
// project preselected from `?project=`), `POST /invite` creates nothing and 303s to it, and
// every invitation that already exists stays listed, revocable and redeemable until it
// expires. No route was removed and no row was migrated.
//
// The tests below drive the REAL `ControlInviting` over a real journal and the in-memory
// stores, through the real dispatcher with both cross-site gates satisfied — the store's own
// row count is the measure, never a counter on a fixture, so "nothing was minted" is a fact
// about the table a redeemer's token is looked up in.

// seedInvitation writes one single-project invitation row and returns its token — the row an
// invitation minted BEFORE the fold left behind.
//
// ⚠ IT WRITES THE STORE DIRECTLY, WITH NO AUTHORITY CHECK, BECAUSE NOTHING IN THE SHIPPED
// TREE CREATES THESE ROWS ANY MORE. The checks the old mint made (may this actor manage the
// project, may their role confer this one) guarded a write that no longer exists; what the
// surviving code is answerable for is what it does with a row that is already there.
func (r *invRig) seedInvitation(inviter control.Principal, project control.ID, role control.Role,
	ttl time.Duration) (string, invite.Invite, error) {

	return seedInvitationInto(r.inviting, inviter, project, role, ttl)
}

// seedInvitationInto is [invRig.seedInvitation] for a test that built its own `ControlInviting`.
func seedInvitationInto(c ControlInviting, inviter control.Principal, project control.ID, role control.Role,
	ttl time.Duration) (string, invite.Invite, error) {

	if ttl <= 0 {
		ttl = invite.DefaultTTL
	}
	token, digest, err := invite.NewToken()
	if err != nil {
		return "", invite.Invite{}, err
	}
	now := c.now()
	inv := invite.Invite{
		Digest: digest, ProjectID: project, Role: role, Inviter: inviter.ID,
		CreatedAt: now, ExpiresAt: now.Add(ttl),
	}
	if err := c.Invites.Create(inv); err != nil {
		return "", invite.Invite{}, err
	}
	return token, inv, nil
}

// foldRig is the real invitation half behind the real dispatcher, signed in as `as`.
func foldRig(t *testing.T, r *teamRig, as control.ID, log *bytes.Buffer) *inviteRig {
	t.Helper()
	cfg := testConfig(t, staticAuth{identity.Identity{Principal: r.principal(as)}})
	cfg.Inviting = r.inviting
	cfg.Now = func() time.Time { return r.clock }
	if log != nil {
		cfg.Log = log
	}
	srv, err := New(cfg)
	if err != nil {
		t.Fatalf("the server did not build: %v", err)
	}
	return &inviteRig{t: t, srv: srv, cookie: inviteCookieValue}
}

func (r *teamRig) invitationCount(project control.ID) int {
	r.t.Helper()
	rows, err := r.invites.ForProject(project)
	if err != nil {
		r.t.Fatalf("reading the invitations: %v", err)
	}
	return len(rows)
}

func (r *teamRig) linkCount(minter control.ID) int {
	r.t.Helper()
	links, err := r.team.Links(r.principal(minter))
	if err != nil {
		r.t.Fatalf("reading the team links: %v", err)
	}
	return len(links)
}

// TestPostInviteNoLongerMintsAnInvitation — REGRESSION. Before the fold this request answered
// 200 with a join link in its body and one more row in the invitation store.
func TestPostInviteNoLongerMintsAnInvitation(t *testing.T) {
	r := newTeamRig(t)

	// POSITIVE CONTROL on the instrument: the count this test reads MOVES when a row is
	// written, so the unchanged count below is a measurement and not a reader wired to nothing.
	if _, _, err := r.seedInvitation(r.principal(invOwner), invProject, control.RoleMember, 0); err != nil {
		t.Fatalf("precondition: %v", err)
	}
	if got := r.invitationCount(invProject); got != 1 {
		t.Fatalf("POSITIVE CONTROL FAILED: one seeded invitation reads as %d row(s)", got)
	}

	// Two roles, because the old handler had two outcomes worth distinguishing: `member` (any
	// manager could mint it) and `owner` (the self-service path this fold deliberately closes).
	for _, role := range []control.Role{control.RoleMember, control.RoleOwner} {
		var log bytes.Buffer
		rig := foldRig(t, r, invOwner, &log)
		rec := rig.post(InvitePath, url.Values{FieldProject: {string(invProject)}, FieldRole: {string(role)}})

		if rec.Code != http.StatusSeeOther {
			t.Fatalf("role=%s: POST /invite answered %d, want 303 to the team-link form. Body: %q",
				role, rec.Code, rec.Body.String())
		}
		want := "/team?outcome=invite-folded&project=" + string(invProject) + "#links"
		if got := rec.Header().Get("Location"); got != want {
			t.Errorf("role=%s: the redirect is %q, want %q", role, got, want)
		}
		if strings.Contains(rec.Body.String(), "/join#invite=") {
			t.Errorf("role=%s: the response carries a join link, so a capability was created", role)
		}
		if got := r.invitationCount(invProject); got != 1 {
			t.Errorf("role=%s: the invitation store holds %d row(s) after POST /invite, want the 1 seeded "+
				"before it — the route still mints", role, got)
		}
		// And it did not quietly mint the OTHER kind either: a redirect to the form is not a
		// submission of it.
		if got := r.linkCount(invOwner); got != 0 {
			t.Errorf("role=%s: POST /invite created %d team link(s); it must create nothing", role, got)
		}
		if strings.Contains(log.String(), "an invitation was minted") {
			t.Errorf("role=%s: the log records a mint: %q", role, log.String())
		}
	}

	// The landing page says what happened, from a CLOSED code, and ticks the project.
	rig := foldRig(t, r, invOwner, nil)
	landed := rig.get("/team?outcome=invite-folded&project=" + string(invProject))
	if landed.Code != http.StatusOK {
		t.Fatalf("the redirect target answered %d", landed.Code)
	}
	const banner = "Nothing was created. Invitations are now team links: use the Create a team link form."
	if !strings.Contains(pageText(landed.Body.String()), banner) {
		t.Errorf("the landing page does not say that nothing was created. Text: %q", pageText(landed.Body.String()))
	}
	// POSITIVE CONTROL: without the code the banner is absent, so it is the code that renders it.
	if strings.Contains(pageText(rig.get("/team?project="+string(invProject)).Body.String()), banner) {
		t.Error("the banner renders without its outcome code")
	}
}

// TestPostInviteCarriesOnlyAProjectItWasGivenIntoItsRedirect — the redirect's one caller-chosen
// part. With no project the Location names none (so it cannot land on the uniform 404), and
// the value is query-escaped rather than concatenated.
func TestPostInviteCarriesOnlyAProjectItWasGivenIntoItsRedirect(t *testing.T) {
	r := newTeamRig(t)
	rig := foldRig(t, r, invOwner, nil)
	for _, tc := range []struct{ project, want string }{
		{"", "/team?outcome=invite-folded#links"},
		{"prj_x&outcome=revoked#share", "/team?outcome=invite-folded&project=prj_x%26outcome%3Drevoked%23share#links"},
	} {
		form := url.Values{FieldRole: {"member"}}
		if tc.project != "" {
			form.Set(FieldProject, tc.project)
		}
		rec := rig.post(InvitePath, form)
		if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != tc.want {
			t.Errorf("project=%q: answered %d → %q, want 303 → %q", tc.project, rec.Code, rec.Header().Get("Location"), tc.want)
		}
	}
}

// TestAnInvitationIssuedBeforeTheFoldStillRedeemsAndIsStillRevocable — an INVARIANT GUARD, and
// labelled as one: this was true before the fold and the fold must not have changed it, so it
// is green on both sides of the change. What makes it evidence is its mutation rows (the
// outstanding list dropped, the revoke row refusing, a legacy token refused at redemption).
//
// It walks the whole path an already-issued invitation needs: listed on the project's section
// with a revoke button, redeemable through the OAuth callback at the role it was issued at
// (INCLUDING `owner`, which nothing can mint any more), and revocable by a manager who did not
// create it — the property a team link does not have.
func TestAnInvitationIssuedBeforeTheFoldStillRedeemsAndIsStillRevocable(t *testing.T) {
	r := newTeamRig(t)
	ctx := context.Background()
	ownerToken, ownerInv, err := r.seedInvitation(r.principal(invOwner), invProject, control.RoleOwner, 0)
	if err != nil {
		t.Fatal(err)
	}
	memberToken, memberInv, err := r.seedInvitation(r.principal(invOwner), invProject, control.RoleMember, 0)
	if err != nil {
		t.Fatal(err)
	}

	// LISTED, each with its revoke form, to a manager who did NOT create them.
	admin := foldRig(t, r, invAdmin, nil)
	page := admin.get(invitePath(invProject))
	if page.Code != http.StatusOK {
		t.Fatalf("the project's section answered %d: %s", page.Code, page.Body.String())
	}
	body := page.Body.String()
	for _, inv := range []invite.Invite{ownerInv, memberInv} {
		if !strings.Contains(body, `name="`+FieldDigest+`" value="`+inv.Digest+`"`) {
			t.Errorf("the %s invitation issued before the fold is not listed with a revoke form", inv.Role)
		}
	}
	if got := strings.Count(body, `action="/invite/revoke"`); got != 2 {
		t.Errorf("the section offers %d invitation revoke form(s), want 2", got)
	}

	// REDEEMABLE, through the real callback, at the role it was issued at.
	cfg := testConfig(t, refusingAuth{})
	stub := &stubOAuth{err: &identity.UnprovisionedSubject{Provider: "fixture-provider", Subject: strangerSubject(41)}}
	cfg.OAuth = stub
	cfg.Inviting = r.inviting
	public, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	cb := httptest.NewRequest("GET", OAuthCallbackPath+"?code=fixture-code", nil)
	cb.AddCookie(startAFlightCarryingAnInvite(t, public, ownerToken))
	rec := httptest.NewRecorder()
	public.ServeHTTP(rec, cb)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("redeeming an invitation issued before the fold answered %d: %s", rec.Code, rec.Body.String())
	}
	user, held := r.authority.Model().UserByProviderSubject("fixture-provider", strangerSubject(41))
	if !held {
		t.Fatal("the redeemer was not provisioned, so the already-issued token no longer redeems")
	}
	if role, in := r.authority.Model().RoleIn(invProject, user.ID); !in || role != control.RoleOwner {
		t.Errorf("the redeemer holds (%q, %v) in the project, want owner — the role the invitation was issued at", role, in)
	}

	// REVOCABLE by a co-manager, and the revoked token then refuses.
	rev := admin.post(InviteRevokePath, url.Values{FieldDigest: {memberInv.Digest}, FieldProject: {string(invProject)}})
	wantLoc := "/team?outcome=invite-revoked&project=" + string(invProject) + "#invite"
	if rev.Code != http.StatusSeeOther || rev.Header().Get("Location") != wantLoc {
		t.Fatalf("a co-manager's revoke answered %d → %q, want 303 → %q. Body: %q",
			rev.Code, rev.Header().Get("Location"), wantLoc, rev.Body.String())
	}
	rows, err := r.invites.ForProject(invProject)
	if err != nil {
		t.Fatal(err)
	}
	states := map[string]invite.State{}
	for _, row := range rows {
		states[row.Digest] = row.StateAt(r.clock)
	}
	if states[memberInv.Digest] != invite.StateRevoked || states[ownerInv.Digest] != invite.StateRedeemed {
		t.Errorf("the rows read %v, want the member invitation revoked and the owner one redeemed", states)
	}
	if _, err := r.inviting.Redeem(ctx, memberToken, "fixture-provider", strangerSubject(42)); !errors.Is(err, invite.ErrNotRedeemable) {
		t.Errorf("a REVOKED invitation answered %v at redemption, want ErrNotRedeemable", err)
	}
}

// TestTheTeamPageHasOneMintFormAndPreselectsTheProject — REGRESSION. Before the fold a
// project's section carried its own mint form (`action="/invite"`) and the team-link form
// ticked nothing.
func TestTheTeamPageHasOneMintFormAndPreselectsTheProject(t *testing.T) {
	r := newTeamRig(t)
	rig := foldRig(t, r, invOwner, nil)
	box := func(project control.ID, checked bool) string {
		out := `<input type="checkbox" name="target" value="project:` + string(project) + `"`
		if checked {
			return out + ` checked>`
		}
		return out + `>`
	}

	// Two points on the dimension: no project chosen, and one chosen.
	bare := rig.get(TeamPath).Body.String()
	chosen := rig.get(invitePath(invProject)).Body.String()

	for name, body := range map[string]string{"no project": bare, "a project": chosen} {
		if strings.Contains(body, `action="/invite"`) {
			t.Errorf("%s: the page still carries the single-project mint form", name)
		}
		if got := strings.Count(body, `action="/team/link"`); got != 1 {
			t.Errorf("%s: the page carries %d team-link form(s), want exactly 1", name, got)
		}
	}
	// POSITIVE CONTROL on the matcher: the unticked spelling IS on the bare page, so the
	// literal below can match this renderer's output at all.
	if !strings.Contains(bare, box(invProject, false)) {
		t.Fatalf("POSITIVE CONTROL FAILED: the bare Team page does not carry the project's unticked box. Body: %q", bare)
	}
	if strings.Contains(bare, " checked") {
		t.Error("with no project chosen a box is pre-ticked: the unread default would put a target on a link")
	}
	if !strings.Contains(chosen, box(invProject, true)) {
		t.Errorf("?project= does not pre-tick that project on the team-link form")
	}
	// EXACTLY that one: the owner's other project and every scope stay unticked.
	if got := strings.Count(chosen, " checked"); got != 1 {
		t.Errorf("?project= pre-ticks %d control(s), want exactly 1", got)
	}
	if !strings.Contains(chosen, box(tlThird, false)) {
		t.Error("the caller's OTHER project is missing or ticked")
	}
}

// TestTheFoldNoticeIsPinnedWhole — `InviteHonesty`'s ruling: the notice states the two costs
// the operator accepted, and those are the clauses a reword would drop first. A LITERAL, so a
// change to what the page promises is a two-place edit.
func TestTheFoldNoticeIsPinnedWhole(t *testing.T) {
	const want = "New invitations are team links. Choosing a project here ticks it on the Create a team " +
		"link form below; no invitation of this older kind can be created any more. Those that " +
		"already exist stay listed here, and stay redeemable and revocable until they expire. Two " +
		"things a team link does not do, and both were accepted: it cannot confer the owner role, so " +
		"making somebody an owner is now only the operator's cairn-server -set-member; and it is " +
		"listed to, and revocable by, only whoever created it, so two admins of one project cannot " +
		"see or revoke each other's links."
	if got := normalizeSpace(InviteFoldNotice); got != normalizeSpace(want) {
		t.Errorf("`InviteFoldNotice` no longer reads as the pinned sentence.\n got: %q\nwant: %q", got, want)
	}
	r := newTeamRig(t)
	rig := foldRig(t, r, invOwner, nil)
	for name, path := range map[string]string{"index": TeamPath, "project": invitePath(invProject)} {
		if text := pageText(rig.get(path).Body.String()); !strings.Contains(text, normalizeSpace(want)) {
			t.Errorf("%s: the page does not carry the whole fold notice. Text: %q", name, text)
		}
	}
	// POSITIVE CONTROL: the containment can fail.
	if strings.Contains(pageText(rig.get(TeamPath).Body.String()), normalizeSpace(want)+" extra") {
		t.Fatal("POSITIVE CONTROL FAILED: the containment check matched a string the page cannot carry")
	}
}

// TestTheInvitingInterfaceCannotMint — STRUCTURAL: the server holds its invitation half as an
// `Inviting`, so a method set with no `Mint` is a server that cannot create an invitation from
// any handler, present or future. The behavioural test above measures the one route; this
// refuses the capability coming back by another.
func TestTheInvitingInterfaceCannotMint(t *testing.T) {
	var inv Inviting = ControlInviting{}
	if _, ok := inv.(interface {
		Mint(context.Context, control.Principal, control.ID, control.Role, time.Duration) (string, invite.Invite, error)
	}); ok {
		t.Error("the production invitation half can mint a single-project invitation again")
	}
	// POSITIVE CONTROL: the same assertion shape finds a method that IS there.
	if _, ok := inv.(interface {
		Revoke(context.Context, control.Principal, string) error
	}); !ok {
		t.Fatal("POSITIVE CONTROL FAILED: the type assertion cannot see a method the type has")
	}
}

// TestTheMintedTeamLinkIsRenderedOnceAsTextAndNeverLogged carries over, to the ONE mint this
// surface has left, the three claims the invitation mint's own guard made before the fold
// removed it. They fail in three different places, which is why they are asserted separately:
// the token must be IN the body exactly once (or the mint produced nothing usable), it must
// NOT be an anchor (a path with no origin resolves against this page, so one stray click — or
// one link-prefetcher — spends the link on its creator), and it must NOT reach the log (the
// journal-leak shape this repository has already shipped once).
func TestTheMintedTeamLinkIsRenderedOnceAsTextAndNeverLogged(t *testing.T) {
	var log bytes.Buffer
	rig, _ := newTeamHTTPRig(t, func(cfg *Config) { cfg.Log = &log })
	rec := rig.post(TeamLinkPath, linkForm(fixtureProjectTarget()))
	if rec.Code != http.StatusOK {
		t.Fatalf("the mint answered %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	wantLink := JoinPath + "#" + url.Values{inviteTokenField: {fixtureLinkToken}}.Encode()
	if n := strings.Count(body, fixtureLinkToken); n != 1 || !strings.Contains(body, wantLink) {
		t.Errorf("the token appears %d time(s) on the page and the link %q is present=%v; want exactly once, "+
			"as that link", n, wantLink, strings.Contains(body, wantLink))
	}
	if strings.Contains(body, `href="`+wantLink) || strings.Contains(body, `href="`+JoinPath+"#") {
		t.Error("the minted link is rendered as an anchor: it resolves against this origin, so one stray " +
			"click redeems the link on the person who created it")
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("the mint response's Cache-Control is %q, want \"no-store\": its body is a bearer capability", got)
	}
	if strings.Contains(log.String(), fixtureLinkToken) {
		t.Errorf("the minted TOKEN reached the log: %q", log.String())
	}
	// POSITIVE CONTROL on the sink: it captured the mint line, so the absence above is about
	// the token and not about a writer wired to nothing.
	if !strings.Contains(log.String(), "a team link was minted") {
		t.Fatalf("the log captured no mint line, so the absence above is about the sink. Log: %q", log.String())
	}
}
