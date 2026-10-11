package ui

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/ZacxDev/cairn/internal/control"
)

// Decision 17 of `claudedocs/plan-cairn-scope-mail.md`, driven through the REAL dispatcher and
// chain: an agent credential — narrowed to `quarry-notes` AND to the verbs read,write — must not
// reach anything gated on `admin`. The owner holds admin on that scope, so before verb narrowing
// existed this credential carried it.

const (
	agentToken          = "fixture-agent-credential-narrowed-by-scope-and-verbs"
	agentVerbsOnlyToken = "fixture-agent-credential-narrowed-by-verbs-only-xx"
)

var scopeGrantToCollaborator = control.DerivedID(control.PrefixGrant, "verb-narrowing-scope-grant")

// verbNarrowedWorld is `narrowedWorld` plus a SCOPE grant on `quarry-notes` to the collaborator
// (what `POST /unshare` revokes) and two agent credentials: one scope- AND verb-narrowed, one
// narrowed by VERBS ONLY (a nil scope list).
func verbNarrowedWorld(t *testing.T) *control.Cache {
	t.Helper()
	readWrite := control.NewVerbSet(control.VerbRead, control.VerbWrite)
	return narrowedWorldWith(t,
		control.Event{Kind: control.EventGranted, At: fixtureClock, Actor: fixtureUser, GrantID: scopeGrantToCollaborator,
			SubjectKind: control.KindUser, SubjectID: fixtureCollaborator,
			ObjectKind: control.ObjectScope, ObjectID: fixtureScope, Verbs: control.NewVerbSet(control.VerbRead)},
		control.Event{Kind: control.EventCredentialIssued, At: fixtureClock, CredentialID: "crd_agent",
			SubjectKind: control.KindUser, SubjectID: fixtureUser, TokenHash: control.HashToken(agentToken),
			Label: "agents@host-a", NarrowedScopes: []control.ID{fixtureScope}},
		control.Event{Kind: control.EventCredentialVerbsNarrowed, At: fixtureClock, CredentialID: "crd_agent", Verbs: readWrite},
		control.Event{Kind: control.EventCredentialIssued, At: fixtureClock, CredentialID: "crd_agent_verbs_only",
			SubjectKind: control.KindUser, SubjectID: fixtureUser, TokenHash: control.HashToken(agentVerbsOnlyToken),
			Label: "agents@host-b"},
		control.Event{Kind: control.EventCredentialVerbsNarrowed, At: fixtureClock, CredentialID: "crd_agent_verbs_only", Verbs: readWrite},
	)
}

// TestAVerbNarrowedAgentCredentialCannotRevokeAGrant is the REGRESSION. `mayRevokeGrant` asks
// only `Allows(scope, admin)`, so the scope-narrowed agent credential of an owner revoked other
// principals' grants on its scope; with decision 17 it holds no admin and is refused. RED under
// the mutant where `Narrow` ignores its verb argument — which is the base behaviour — and, for
// the VERBS-ONLY credential, under the base `Narrow`'s early return on a nil scope list.
//
// POSITIVE CONTROL: the owner's un-narrowed bearer gets PAST the authority check (the fixture
// authority is read-only, so it then answers the read-only 501 — a different answer from 403,
// which places the refusal at the authority check rather than at the write).
func TestAVerbNarrowedAgentCredentialCannotRevokeAGrant(t *testing.T) {
	authority := verbNarrowedWorld(t)
	form := url.Values{FieldGrant: {string(scopeGrantToCollaborator)}}
	for _, bearer := range []string{agentToken, agentVerbsOnlyToken} {
		_, auth, err := authority.Authenticate(bearer)
		if err != nil {
			t.Fatalf("precondition: the agent credential does not authenticate: %v", err)
		}
		if !auth.Allows(fixtureScope, control.VerbWrite) {
			t.Fatal("precondition: the agent credential cannot write quarry-notes, so a refusal below proves nothing")
		}
		rec := newLiveOver(t, authority, nil, nil, nil).bearerDo(http.MethodPost, UnsharePath, bearer, form)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("A READ,WRITE AGENT CREDENTIAL REVOKED A GRANT ON ITS SCOPE (status %d %q) — it still carries admin",
				rec.Code, rec.Body.String())
		}
	}
	rec := newLiveOver(t, authority, nil, nil, nil).bearerDo(http.MethodPost, UnsharePath, testCredential, form)
	if rec.Code != http.StatusNotImplemented || rec.Body.String() != ReadOnlyAuthority {
		t.Fatalf("POSITIVE CONTROL FAILED: the owner's un-narrowed revoke answered %d %q, want 501 ReadOnlyAuthority",
			rec.Code, rec.Body.String())
	}
}

// TestAVerbNarrowedAgentCredentialReachesNoMembershipAuthority — `POST /share`, invitations and
// team links refuse the agent credential. ⚠ INVARIANT GUARDS, NOT REGRESSION COVERAGE: each of
// these refuses every SCOPE-narrowed caller already at base (`membershipActor` is the zero
// principal for a narrowed caller, STEP 1 of the plan), so decision 17 changes none of them.
// They pin that the verb narrowing did not REOPEN one — a `Narrowed()` that went false for a
// verb-narrowed authority would.
func TestAVerbNarrowedAgentCredentialReachesNoMembershipAuthority(t *testing.T) {
	authority := verbNarrowedWorld(t)
	links := &ControlTeamLinks{Authority: authority, Store: newMemLinks(), Now: func() time.Time { return fixtureClock }}
	for _, bearer := range []string{agentToken, agentVerbsOnlyToken} {
		share := url.Values{FieldScope: {string(fixtureScope)}, FieldSubject: {string(fixtureCollaborator)},
			FieldVerb: {string(control.VerbRead)}}
		if rec := newLiveOver(t, authority, nil, nil, nil).bearerDo(http.MethodPost, SharePath, bearer, share); rec.Code != http.StatusForbidden {
			t.Errorf("an agent credential's share answered %d, want 403", rec.Code)
		}
		inviting, store, digest := seededInvite(t, authority)
		rec := newLiveOver(t, authority, inviting, nil, nil).bearerDo(http.MethodPost, InviteRevokePath, bearer,
			url.Values{FieldDigest: {digest}, FieldProject: {string(fixtureProject)}})
		if rec.Code != http.StatusForbidden || !store.rows[digest].RevokedAt.IsZero() {
			t.Errorf("an agent credential's invite revoke answered %d (revoked=%v), want 403",
				rec.Code, !store.rows[digest].RevokedAt.IsZero())
		}
		inviting.Links = links
		form := url.Values{FieldTarget: {"project:" + string(fixtureProject)}, FieldRole: {"reader"}, FieldTTLDays: {"1"}}
		if rec := newLiveOver(t, authority, inviting, nil, nil).bearerDo(http.MethodPost, TeamLinkPath, bearer, form); rec.Code != http.StatusForbidden {
			t.Errorf("an agent credential minted a team link: %d %q", rec.Code, strings.TrimSpace(rec.Body.String()))
		}
	}
}
