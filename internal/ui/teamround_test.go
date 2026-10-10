package ui

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ZacxDev/cairn/internal/control"
	"github.com/ZacxDev/cairn/internal/identity"
	"github.com/ZacxDev/cairn/internal/invite"
)

// The guards round 1 of #214's audit and the operator's decisions added. Each names the item it
// closes; the RED evidence is in `internal/ui/README.md`, Phase S.

// barrierLinks holds every `RedeemLink` until `n` callers have arrived, which is what makes the
// double-callback race DETERMINISTIC rather than a 39-in-40 measurement: both callers have
// already passed `Redeem`'s "is this subject held" check (it precedes the spend) when either
// is let through.
type barrierLinks struct {
	*memLinks
	mu      sync.Mutex
	waiting int
	n       int
	release chan struct{}
}

func (b *barrierLinks) RedeemLink(token string, by control.ID, provisioned bool, at time.Time) (invite.TeamLink, error) {
	b.mu.Lock()
	b.waiting++
	if b.waiting == b.n {
		close(b.release)
	}
	b.mu.Unlock()
	select {
	case <-b.release:
	case <-time.After(10 * time.Second):
		return invite.TeamLink{}, errors.New("barrierLinks: the second caller never arrived")
	}
	return b.memLinks.RedeemLink(token, by, provisioned, at)
}

// TestTheRedemptionLogNamesOnlyRealJoins — round 1 🟡1, reproduced. Two tabs, ONE GitHub
// identity the control plane has never seen, ONE reusable link: both callbacks pass the
// "unknown subject" check, both spend the link, and only one journal write can land (a second
// user row for the same provider/subject is refused). The log must render exactly one JOIN —
// naming the principal that exists — and the other spend as an unconfirmed attempt.
// RED with the row confirmed at the spend (`ui-teamlink-log-confirms-before-the-authority-write`)
// and with the page rendering every row as a join (`ui-team-log-renders-an-unconfirmed-row-as-a-join`).
func TestTheRedemptionLogNamesOnlyRealJoins(t *testing.T) {
	r := newTeamRig(t)
	token, link := r.mint(invOwner, invite.LinkReader, true, scopeT(tlA))
	barrier := &barrierLinks{memLinks: r.links, n: 2, release: make(chan struct{})}
	r.team.Store = barrier
	r.inviting.Links = r.team

	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := range errs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, errs[i] = r.inviting.Redeem(context.Background(), token, "fixture-provider", "tl-two-tabs")
		}(i)
	}
	wg.Wait()
	failed := 0
	for _, err := range errs {
		if err != nil {
			failed++
		}
	}
	if failed != 1 {
		t.Fatalf("INSTRUMENT: %d of the 2 concurrent redemptions failed (%v); the race this test exists for is "+
			"exactly one journal write landing, so the reproduction did not happen", failed, errs)
	}

	m := r.authority.Model()
	user, held := m.UserByProviderSubject("fixture-provider", "tl-two-tabs")
	if !held {
		t.Fatal("neither tab created the user")
	}
	log, _ := r.links.LinkRedemptions(link.Digest)
	if len(log) != 2 {
		t.Fatalf("the store holds %d redemption row(s), want 2 — BOTH tabs spent the link: %+v", len(log), log)
	}
	confirmed := 0
	for _, row := range log {
		_, exists := m.PrincipalFor(control.KindUser, row.By)
		if row.Confirmed {
			confirmed++
			if row.By != user.ID {
				t.Errorf("a CONFIRMED row names %s, not the user that exists (%s)", row.By, user.ID)
			}
		}
		if row.Confirmed && !exists {
			t.Errorf("a CONFIRMED row names %s, a principal the control plane does not hold", row.By)
		}
	}
	if confirmed != 1 {
		t.Fatalf("%d row(s) are confirmed, want exactly 1 — the one join that landed", confirmed)
	}

	// And the page: exactly one "joined", and the other row an attempt.
	links, err := r.team.Links(r.principal(invOwner))
	if err != nil || len(links) != 1 {
		t.Fatalf("Links answered %d link(s), %v", len(links), err)
	}
	page := pageText(renderNode(t, TeamPage(TeamView{Viewer: "v", Links: teamLinkRows(links, r.clock)})))
	if got := strings.Count(page, "joined (account created by this link)"); got != 1 {
		t.Errorf("the page renders %d join(s) for one real account:\n%s", got, page)
	}
	if !strings.Contains(page, "NOT confirmed: the join may not have been recorded") {
		t.Error("the failed tab's spend is not rendered as an unconfirmed attempt")
	}
}

// TestTheOldFlowPathsRedirectToTheTeamPage — operator decision O-a: `GET /share` and
// `GET /invite` are 303s to the matching Team section, the query carried, and a pre-move invite
// revoke code translated so it cannot read as the share flow's banner.
func TestTheOldFlowPathsRedirectToTheTeamPage(t *testing.T) {
	rig := newInviteRig(t, nil)
	for _, tc := range []struct{ from, want string }{
		{SharePath, TeamPath + "#share"},
		{SharePath + "?scope=scp_x&outcome=shared&by=now", TeamPath + "?scope=scp_x&outcome=shared&by=now#share"},
		{InvitePath, TeamPath + "#invite"},
		{InvitePath + "?project=prj_x", TeamPath + "?project=prj_x#invite"},
		{InvitePath + "?outcome=revoked&project=prj_x", TeamPath + "?outcome=invite-revoked&project=prj_x#invite"},
	} {
		rec := rig.get(tc.from)
		if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != tc.want {
			t.Errorf("GET %s answered %d → %q, want 303 → %q", tc.from, rec.Code, rec.Header().Get("Location"), tc.want)
		}
	}
}

// TestTheTeamPageRefusesAForeignScopeOrProjectUniformly — the two old pages' uniform 404s,
// reached on the page that now answers them, byte for byte.
func TestTheTeamPageRefusesAForeignScopeOrProjectUniformly(t *testing.T) {
	rig := newInviteRig(t, nil)
	for _, tc := range []struct{ path, want string }{
		{TeamPath + "?" + QueryScope + "=scp_not_mine", scopeRefusal},
		{TeamPath + "?" + QueryProject + "=prj_not_mine", inviteRefusal},
	} {
		rec := rig.get(tc.path)
		if rec.Code != http.StatusNotFound || rec.Body.String() != tc.want {
			t.Errorf("GET %s answered %d %q, want 404 %q", tc.path, rec.Code, rec.Body.String(), tc.want)
		}
	}
	// POSITIVE CONTROL: the fixture's own project renders its section. (A scope page needs an
	// authority that ALLOWS admin, which this static rig's identity does not carry; the share
	// rig in `sharing_test.go` drives `/team?scope=` against a real journal.)
	project := rig.get(TeamPath + "?" + QueryProject + "=" + string(fixtureNamedProject.ID))
	if project.Code != http.StatusOK || !strings.Contains(project.Body.String(), `action="`+InvitePath+`"`) {
		t.Errorf("POSITIVE CONTROL: the caller's own project answered %d without its mint form", project.Code)
	}
}

// TestTheMovedWritesLandOnTheTeamPage — the POST rows are unchanged as routes, and each lands
// on its Team section: a share and a revoke at `#share` with the scope and outcome, an invite
// revoke at `#invite` with this flow's own code, and a mint renders its link ON the Team page.
func TestTheMovedWritesLandOnTheTeamPage(t *testing.T) {
	rig := newInviteRig(t, nil)
	rec := rig.post(InviteRevokePath, url.Values{FieldDigest: {fixtureInviteDigest}, FieldProject: {string(fixtureNamedProject.ID)}})
	want := TeamPath + "?" + QueryOutcome + "=" + inviteOutcomeRevoked + "&" + QueryProject + "=" + string(fixtureNamedProject.ID) + "#invite"
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != want {
		t.Errorf("an invite revoke answered %d → %q, want 303 → %q", rec.Code, rec.Header().Get("Location"), want)
	}
	mint := rig.post(InvitePath, url.Values{FieldProject: {string(fixtureNamedProject.ID)}, FieldRole: {string(control.RoleMember)}})
	body := mint.Body.String()
	if mint.Code != http.StatusOK || !strings.Contains(body, "<title>cairn — team") ||
		strings.Count(body, url.Values{inviteTokenField: {fixtureInviteToken}}.Encode()) != 1 {
		t.Errorf("an invitation mint answered %d, and must render the Team page carrying the link exactly once", mint.Code)
	}
}

// TestAProjectWideGrantIsListedLabelledAndRevocable — operator decision O-b and round 1 🟡3.
// A project-wide `reader` grant (what a team link's project-reader writes) must appear where a
// reader decides who has access: in the scope's audience labelled HOW it reaches the scope, in
// what the scope page can take back, and in the project's own list on the Team page — and a
// project manager must be able to revoke it there. RED with `Revocable` back to scope grants
// only (`ui-share-revocable-drops-project-wide-grants`), with the audience label dropped
// (`ui-share-audience-loses-the-project-wide-label`), and with `Unshare` refusing every project
// grant (`ui-unshare-refuses-every-project-wide-grant`).
func TestAProjectWideGrantIsListedLabelledAndRevocable(t *testing.T) {
	r := newTeamRig(t)
	ctx := context.Background()
	token, _ := r.mint(invOwner, invite.LinkReader, false, projectT(invProject))
	red, err := r.inviting.Redeem(ctx, token, "fixture-provider", strangerSubject(1))
	if err != nil {
		t.Fatalf("precondition: the project-reader link was refused: %v", err)
	}
	sharing := ControlSharing{Authority: r.authority, Now: func() time.Time { return invClock }}

	audience, _ := sharing.Audience(tlA)
	var found *Viewer
	for i := range audience {
		if audience[i].Display == red.Principal.Display {
			found = &audience[i]
		}
	}
	if found == nil || !found.ByProjectGrant || found.ByMembership {
		t.Fatalf("the project-wide reader is in the audience as %+v; want listed, ByProjectGrant, not by membership", found)
	}
	revocable, _ := sharing.Revocable(tlA)
	var grant *GrantRow
	for i := range revocable {
		if revocable[i].Subject.ID == red.Principal.ID {
			grant = &revocable[i]
		}
	}
	if grant == nil || !grant.ProjectWide || grant.Project != "quarry" {
		t.Fatalf("the project-wide grant is not in what the scope page can take back, labelled: %+v (all %+v)", grant, revocable)
	}
	listed, _ := sharing.ProjectGrants(invProject)
	if len(listed) != 1 || listed[0].ID != grant.ID {
		t.Fatalf("the project's own list is %+v, want exactly that grant", listed)
	}

	// The page says it, in words, before the button.
	html := renderNode(t, TeamPage(TeamView{Viewer: "v", CSRF: renderCSRF,
		Invite: InviteView{Project: control.NamedProject{ID: invProject, Name: "quarry"}}, ProjectGrants: listed}))
	if !strings.Contains(pageText(html), "project-wide: every scope in quarry — revoking it withdraws all of them") {
		t.Error("the Team page's project section does not label the grant as project-wide")
	}

	// A scope-only admin (an OUTSIDER holding admin on one scope) may NOT revoke it…
	scopeAdmin := r.principal(tlReadAdmin)
	if _, err := sharing.Unshare(ctx, scopeAdmin, control.Resolve(r.authority.Model(), scopeAdmin), grant.ID); !errors.Is(err, ErrNotPermitted) {
		t.Errorf("an outsider with admin on one scope revoked a PROJECT-WIDE grant (err=%v)", err)
	}
	// …a plain member may not…
	member := r.principal(invPlain)
	if _, err := sharing.Unshare(ctx, member, control.Resolve(r.authority.Model(), member), grant.ID); !errors.Is(err, ErrNotPermitted) {
		t.Errorf("a plain member revoked a project-wide grant (err=%v)", err)
	}
	// …a NARROWED manager may not (membership authority is not in a narrowing)…
	admin := r.principal(invAdmin)
	narrowed := control.Narrow(control.Resolve(r.authority.Model(), admin), []control.ID{tlA})
	if _, err := sharing.Unshare(ctx, admin, narrowed, grant.ID); !errors.Is(err, ErrNotPermitted) {
		t.Errorf("a NARROWED credential revoked a project-wide grant (err=%v)", err)
	}
	// …and the project's admin may — POSITIVE CONTROL, and the access is then gone.
	if _, err := sharing.Unshare(ctx, admin, control.Resolve(r.authority.Model(), admin), grant.ID); err != nil {
		t.Fatalf("the project's admin could not revoke the project-wide grant: %v", err)
	}
	if got := r.authorityOf(red.Principal.ID); got != "" {
		t.Errorf("after the revoke the reader still holds %q", got)
	}
}

// TestANarrowedBearerCannotRevokeAProjectWideGrant drives the same refusal through the REAL
// dispatcher and chain, as the membership ledger's exemption for `handleUnshare` cites.
func TestANarrowedBearerCannotRevokeAProjectWideGrant(t *testing.T) {
	grantID := control.DerivedID(control.PrefixGrant, "narrowed-project-wide")
	authority := narrowedWorldWith(t, control.Event{
		Kind: control.EventGranted, At: fixtureClock, Actor: fixtureUser, GrantID: grantID,
		SubjectKind: control.KindUser, SubjectID: fixtureCollaborator,
		ObjectKind: control.ObjectProject, ObjectID: fixtureProject, Verbs: control.NewVerbSet(control.VerbRead),
	})
	inviting := ControlInviting{Authority: authority, Invites: newMemInvites(), Now: func() time.Time { return fixtureClock },
		Links: &ControlTeamLinks{Authority: authority, Store: newMemLinks()}}
	form := url.Values{FieldGrant: {string(grantID)}}
	for _, bearer := range narrowedBearers {
		rec := newLiveOver(t, authority, inviting, nil, nil).bearerDo(http.MethodPost, UnsharePath, bearer, form)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("a NARROWED bearer revoked its owner's project-wide grant: %d %q", rec.Code, rec.Body.String())
		}
	}
	// POSITIVE CONTROL: the un-narrowed bearer (the project's owner) gets PAST the authority
	// check. This fixture world is read-only, so the write itself then answers the read-only
	// 501 — a different answer from the 403 above, which is what places the refusal at the
	// authority check rather than at the write.
	rec := newLiveOver(t, authority, inviting, nil, nil).bearerDo(http.MethodPost, UnsharePath, testCredential, form)
	if rec.Code != http.StatusNotImplemented || rec.Body.String() != ReadOnlyAuthority {
		t.Fatalf("POSITIVE CONTROL FAILED: the owner's revoke answered %d %q, want 501 ReadOnlyAuthority", rec.Code, rec.Body.String())
	}
}

// TestALinkRedemptionLogLineNamesTheLink — round 1 🟢5: the operator's log line for a team
// link names the link (digest PREFIX), its role and its target count, never a blank
// project/role, and never the token.
func TestALinkRedemptionLogLineNamesTheLink(t *testing.T) {
	r := newTeamRig(t)
	token, link := r.mint(invOwner, invite.LinkReader, true, scopeT(tlA), projectT(tlThird))
	cfg := testConfig(t, refusingAuth{})
	stub := &stubOAuth{err: &identity.UnprovisionedSubject{Provider: "fixture-provider", Subject: strangerSubject(7)}}
	cfg.OAuth = stub
	cfg.Inviting = r.inviting
	var logBuf bytes.Buffer
	cfg.Log = &logBuf
	srv, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	cookie := startAFlightCarryingAnInvite(t, srv, token)
	cb := httptest.NewRequest("GET", OAuthCallbackPath+"?code=fixture-code", nil)
	cb.AddCookie(cookie)
	srv.ServeHTTP(httptest.NewRecorder(), cb)

	want := "provisioned=true link=" + shortDigest(link.Digest) + " role=reader targets=2"
	if !strings.Contains(logBuf.String(), want) {
		t.Errorf("the log does not carry %q:\n%s", want, logBuf.String())
	}
	if strings.Contains(logBuf.String(), token) {
		t.Error("the log carries the TOKEN")
	}
	if strings.Contains(logBuf.String(), "project= role=") {
		t.Error("the log still prints a blank project/role for a team link")
	}
}

// TestAProjectWideRevokeFromTheProjectSectionLandsBackThere — round 2 nit: a project-wide grant
// revoked from the Team page's PROJECT section lands on `/team?project=…#invite`, not on the share
// section; from a scope page (no project field) it lands on the share section as before.
func TestAProjectWideRevokeFromTheProjectSectionLandsBackThere(t *testing.T) {
	for _, tc := range []struct {
		name        string
		withProject bool
	}{{"from the project section", true}, {"from a scope page", false}} {
		t.Run(tc.name, func(t *testing.T) {
			r := newTeamRig(t)
			token, _ := r.mint(invOwner, invite.LinkReader, false, projectT(invProject))
			if _, err := r.inviting.Redeem(context.Background(), token, "fixture-provider", strangerSubject(1)); err != nil {
				t.Fatal(err)
			}
			sharing := ControlSharing{Authority: r.authority, Now: func() time.Time { return invClock }}
			grants, _ := sharing.ProjectGrants(invProject)
			if len(grants) != 1 {
				t.Fatalf("precondition: %d project-wide grant(s), want 1", len(grants))
			}
			admin := r.principal(invAdmin)
			cfg := testConfig(t, staticAuth{identity.Identity{Principal: admin, Auth: control.Resolve(r.authority.Model(), admin)}})
			cfg.Sharing = sharing
			srv, err := New(cfg)
			if err != nil {
				t.Fatal(err)
			}
			form := url.Values{FieldGrant: {string(grants[0].ID)}, FieldCSRF: {identity.CSRFTokenFor(inviteCookieValue)}}
			if tc.withProject {
				form.Set(FieldProject, string(invProject))
			}
			req := httptest.NewRequest(http.MethodPost, UnsharePath, strings.NewReader(form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			req.Header.Set("Origin", "https://"+req.Host)
			req.AddCookie(&http.Cookie{Name: identity.SessionCookieName, Value: inviteCookieValue})
			rec := httptest.NewRecorder()
			srv.ServeHTTP(rec, req)
			loc := rec.Header().Get("Location")
			want := "#share"
			if tc.withProject {
				want = QueryProject + "=" + string(invProject) + "#invite"
			}
			if rec.Code != http.StatusSeeOther || !strings.HasSuffix(loc, want) || !strings.HasPrefix(loc, TeamPath+"?") {
				t.Errorf("the revoke answered %d → %q, want 303 to /team…%s", rec.Code, loc, want)
			}
		})
	}
}
