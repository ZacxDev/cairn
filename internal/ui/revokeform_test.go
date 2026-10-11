package ui

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/ZacxDev/cairn/internal/control"
)

// Round 2 🟡A of #214's audit: the scope page's take-back list includes project-wide grants
// for EVERY viewer (O-b), and the Revoke button was rendered on them for any viewer with a
// session — including an outsider holding `admin` on one scope, for whom `POST /unshare`
// always answers 403. The row also named the owning project to that outsider.

const outsiderToken = "fixture-outsider-scope-admin-not-a-real-token"

var (
	outsiderUser         = control.DerivedID(control.PrefixUser, "outsider-scope-admin")
	projectWideGrantID   = control.DerivedID(control.PrefixGrant, "project-wide-reader")
	outsiderScopeGrantID = control.DerivedID(control.PrefixGrant, "outsider-scope-admin")
)

// outsiderWorld is `narrowedWorld` plus an OUTSIDER (a user in no project) granted `admin` on
// `fixtureScope`, and a project-wide `read` grant on `fixtureProject` (the scope's owner) to
// the collaborator — the shape a team link's project-`reader` writes.
func outsiderWorld(t *testing.T) *control.Cache {
	t.Helper()
	return narrowedWorldWith(t,
		control.Event{Kind: control.EventUserCreated, At: fixtureClock, UserID: outsiderUser,
			Provider: "fixture-provider", Subject: "00000000-0000-4000-8000-000000000009",
			Email: "outsider@notes.example.invalid"},
		control.Event{Kind: control.EventCredentialIssued, At: fixtureClock, CredentialID: "crd_outsider",
			SubjectKind: control.KindUser, SubjectID: outsiderUser,
			TokenHash: control.HashToken(outsiderToken), Label: "outsider"},
		control.Event{Kind: control.EventGranted, At: fixtureClock, Actor: fixtureUser, GrantID: outsiderScopeGrantID,
			SubjectKind: control.KindUser, SubjectID: outsiderUser,
			ObjectKind: control.ObjectScope, ObjectID: fixtureScope,
			Verbs: control.NewVerbSet(control.VerbRead, control.VerbAdmin)},
		control.Event{Kind: control.EventGranted, At: fixtureClock, Actor: fixtureUser, GrantID: projectWideGrantID,
			SubjectKind: control.KindUser, SubjectID: fixtureCollaborator,
			ObjectKind: control.ObjectProject, ObjectID: fixtureProject,
			Verbs: control.NewVerbSet(control.VerbRead)},
	)
}

// TestARevokeFormIsRenderedOnlyWhereTheRevokeWouldBeAuthorised — the outsider scope admin sees
// the project-wide row WITHOUT a revoke form and WITHOUT the project's name; the project's owner
// sees both. The form is decided by `mayRevokeGrant`, the predicate `POST /unshare` runs. RED
// under `ui-revoke-form-rendered-without-mayrevokegrant`.
func TestARevokeFormIsRenderedOnlyWhereTheRevokeWouldBeAuthorised(t *testing.T) {
	authority := outsiderWorld(t)
	inviting := ControlInviting{Authority: authority, Invites: newMemInvites(), Now: func() time.Time { return fixtureClock },
		Links: &ControlTeamLinks{Authority: authority, Store: newMemLinks()}}
	page := TeamPath + "?" + url.Values{QueryScope: {string(fixtureScope)}}.Encode()
	revokeForm := `name="` + FieldGrant + `" value="` + string(projectWideGrantID) + `"`

	outsider := newLiveOver(t, authority, inviting, nil, nil).bearerDo(http.MethodGet, page, outsiderToken, nil)
	if outsider.Code != http.StatusOK {
		t.Fatalf("INSTRUMENT: the outsider scope admin's scope page answered %d — the outsider must ADMINISTER "+
			"the scope for this case to mean anything:\n%s", outsider.Code, outsider.Body.String())
	}
	body := outsider.Body.String()
	// POSITIVE CONTROL: the outsider DOES see the row (O-b lists it) and DOES get a revoke form on
	// the scope grant it may take back — so an absence below is about the project-wide row alone.
	if !strings.Contains(pageText(body), "a project-wide grant") {
		t.Fatalf("POSITIVE CONTROL: the outsider's page does not list the project-wide grant at all:\n%s", pageText(body))
	}
	if !strings.Contains(body, `name="`+FieldGrant+`" value="`+string(outsiderScopeGrantID)+`"`) {
		t.Fatal("POSITIVE CONTROL: the outsider has no revoke form even on the scope grant it may revoke")
	}
	if strings.Contains(body, revokeForm) {
		t.Error("an outsider scope admin is offered a Revoke button on a PROJECT-WIDE grant, which POST /unshare " +
			"always refuses for them (403) — a button that cannot work")
	}
	if !strings.Contains(pageText(body), "only a project owner or admin can revoke this") {
		t.Error("the project-wide row does not say who CAN revoke it")
	}
	if strings.Contains(pageText(body), "every scope in quarry") {
		t.Error("the project-wide row names the owning project to a viewer who is not in it")
	}

	owner := newLiveOver(t, authority, inviting, nil, nil).bearerDo(http.MethodGet, page, testCredential, nil)
	if owner.Code != http.StatusOK || !strings.Contains(owner.Body.String(), revokeForm) {
		t.Errorf("the project's OWNER is not offered the revoke form on the project-wide grant (status %d)", owner.Code)
	}
	if !strings.Contains(pageText(owner.Body.String()), "every scope in quarry") {
		t.Error("the project's owner is not told which project the grant covers")
	}
}
