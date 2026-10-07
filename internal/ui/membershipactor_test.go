package ui

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/ZacxDev/cairn/internal/control"
	"github.com/ZacxDev/cairn/internal/identity"
)

// bearerDo drives one request authenticated by a bearer token — the machine-token backend,
// which is how a narrowed credential reaches this surface. It also carries a cookie value the
// CALLER chose (no session behind it) and, on a state change, the CSRF token derived from
// it — exactly what `csrfTokenValid`'s comment says such a caller can do.
func (l *live) bearerDo(method, path, bearer string, form url.Values) *httptest.ResponseRecorder {
	l.t.Helper()
	var r *http.Request
	if form != nil {
		r = httptest.NewRequest(method, path, strings.NewReader(form.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	} else {
		r = httptest.NewRequest(method, path, nil)
	}
	r.Host = testHost
	r.Header.Set("Authorization", "Bearer "+bearer)
	// The cookie rides GETs too: a page renders its forms (and the share page its candidate
	// `select`) only when a CSRF token can be derived from a cookie on the request.
	const chosen = "a-cookie-value-the-caller-chose"
	r.AddCookie(&http.Cookie{Name: identity.SessionCookieName, Value: chosen})
	if method != http.MethodGet {
		r.Header.Set("Origin", "https://"+testHost)
		r.Header.Set(HeaderCSRF, identity.CSRFTokenFor(chosen))
	}
	rec := httptest.NewRecorder()
	l.srv.ServeHTTP(rec, r)
	return rec
}

var narrowedBearers = []string{narrowedToOneToken, narrowedToNoneToken, narrowedToAllToken}

// seededInvite is a real invitation into the fixture user's project, minted directly by the
// service as the UN-narrowed principal, so the page and revoke rows have something to show
// and something to withdraw.
func seededInvite(t *testing.T, authority *control.Cache) (ControlInviting, *memInvites, string) {
	t.Helper()
	store := newMemInvites()
	inviting := ControlInviting{Authority: authority, Invites: store, Now: func() time.Time { return fixtureClock }}
	owner, ok := authority.Model().PrincipalFor(control.KindUser, fixtureUser)
	if !ok {
		t.Fatal("precondition: the fixture user is not in the model")
	}
	_, inv, err := inviting.Mint(context.Background(), owner, fixtureProject, control.RoleMember, 0)
	if err != nil {
		t.Fatalf("precondition: the owner could not mint: %v", err)
	}
	return inviting, store, inv.Digest
}

// TestANarrowedBearerSeesNoInvitations covers `GET /invite?project=` — the page that lists a
// project's invitations through `Outstanding`, which performs NO authority check of its own
// and is reached only through `Invitable`.
func TestANarrowedBearerSeesNoInvitations(t *testing.T) {
	authority := narrowedWorld(t)
	inviting, _, _ := seededInvite(t, authority)
	path := InvitePath + "?" + url.Values{QueryProject: {string(fixtureProject)}}.Encode()

	// POSITIVE CONTROL: the un-narrowed bearer sees the project page and the invitation on it.
	rec := newLiveOver(t, authority, inviting, nil, nil).bearerDo(http.MethodGet, path, testCredential, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("POSITIVE CONTROL FAILED: the un-narrowed bearer's project page answered %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "quarry") {
		t.Fatal("POSITIVE CONTROL FAILED: the un-narrowed project page does not name the project")
	}
	for _, bearer := range narrowedBearers {
		rec := newLiveOver(t, authority, inviting, nil, nil).bearerDo(http.MethodGet, path, bearer, nil)
		if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), inviteRefusal) {
			t.Fatalf("A NARROWED BEARER WAS SHOWN ITS OWNER'S PROJECT INVITATIONS (status %d); want the "+
				"uniform 404 %q:\n%s", rec.Code, inviteRefusal, rec.Body.String())
		}
	}
}

// TestANarrowedBearerCannotRevokeAnInvitation covers `POST /invite/revoke`.
func TestANarrowedBearerCannotRevokeAnInvitation(t *testing.T) {
	authority := narrowedWorld(t)
	for _, bearer := range narrowedBearers {
		inviting, store, digest := seededInvite(t, authority)
		rec := newLiveOver(t, authority, inviting, nil, nil).bearerDo(http.MethodPost, InviteRevokePath, bearer,
			url.Values{FieldDigest: {digest}, FieldProject: {string(fixtureProject)}})
		if rec.Code == http.StatusSeeOther || !store.rows[digest].RevokedAt.IsZero() {
			t.Fatalf("A NARROWED BEARER REVOKED ITS OWNER'S INVITATION (status %d)", rec.Code)
		}
		if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), inviteWriteRefusal) {
			t.Fatalf("a narrowed bearer's revoke answered %d, want the uniform 403 %q", rec.Code, inviteWriteRefusal)
		}
	}
	// POSITIVE CONTROL: the un-narrowed bearer CAN revoke, so the refusals are about the narrowing.
	inviting, store, digest := seededInvite(t, authority)
	rec := newLiveOver(t, authority, inviting, nil, nil).bearerDo(http.MethodPost, InviteRevokePath, testCredential,
		url.Values{FieldDigest: {digest}, FieldProject: {string(fixtureProject)}})
	if rec.Code != http.StatusSeeOther || store.rows[digest].RevokedAt.IsZero() {
		t.Fatalf("POSITIVE CONTROL FAILED: the un-narrowed bearer could not revoke (status %d)", rec.Code)
	}
}

// TestANarrowedAdminBearerIsOfferedNoShareCandidates covers the share flow's candidate list,
// which is membership-derived: a caller narrowed to `quarry-notes` (where it holds admin) must
// not be shown — or be able to share with — a collaborator from `kiln`, a project its
// narrowing has nothing to do with.
func TestANarrowedAdminBearerIsOfferedNoShareCandidates(t *testing.T) {
	authority := narrowedWorld(t)
	page := SharePath + "?" + url.Values{QueryScope: {string(fixtureScope)}}.Encode()
	share := url.Values{FieldScope: {string(fixtureScope)}, FieldSubject: {string(fixtureCollaborator)},
		FieldVerb: {string(control.VerbRead)}}

	// POSITIVE CONTROL: un-narrowed, the collaborator IS offered, and the share passes the
	// subject check (whatever the read-only fixture authority then answers, it is not the
	// subject refusal).
	rec := newLiveOver(t, authority, nil, nil, nil).bearerDo(http.MethodGet, page, testCredential, nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), string(fixtureCollaborator)) {
		t.Fatalf("POSITIVE CONTROL FAILED: the un-narrowed share page (status %d) does not offer the collaborator",
			rec.Code)
	}

	// The narrowed admin bearer: subset and equal-to-full both hold admin on quarry-notes.
	// (Narrowed-to-nothing cannot reach the scope page at all, which the admin check decides.)
	for _, bearer := range []string{narrowedToOneToken, narrowedToAllToken} {
		rec := newLiveOver(t, authority, nil, nil, nil).bearerDo(http.MethodGet, page, bearer, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("precondition: a narrowed bearer with admin on quarry-notes got %d on its share page", rec.Code)
		}
		if strings.Contains(rec.Body.String(), string(fixtureCollaborator)) ||
			strings.Contains(rec.Body.String(), string(fixtureProjectKiln)) {
			t.Fatal("A NARROWED BEARER WAS OFFERED SHARE CANDIDATES FROM ITS OWNER'S MEMBERSHIPS (a collaborator " +
				"from `kiln`, which its narrowing excludes)")
		}
		rec = newLiveOver(t, authority, nil, nil, nil).bearerDo(http.MethodPost, SharePath, bearer, share)
		if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), shareWriteRefusal) {
			t.Fatalf("a narrowed bearer's share with a membership-derived collaborator answered %d, want the "+
				"uniform 403 %q", rec.Code, shareWriteRefusal)
		}
	}
}
