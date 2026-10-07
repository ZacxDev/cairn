package ui

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ZacxDev/cairn/internal/control"
	"github.com/ZacxDev/cairn/internal/identity"
)

// A narrowed credential must not become more than its narrowing on this surface.
//
// The world is `fixtureCache` plus a SECOND scope in the same project — so the fixture user
// reaches two scopes and a narrowing to one of them is a real subset — and three narrowed
// credentials, one per shape a narrowing can have. Every value is synthetic.
var (
	fixtureScopeTwo = control.DerivedID(control.PrefixScope, "quarry-ledger")
	// A second project the fixture user belongs to, with a collaborator nobody else shares
	// and NO scopes — so it adds a share candidate without changing the user's scope set
	// (the "equal to today's full set" credential below stays equal).
	fixtureProjectKiln  = control.DerivedID(control.PrefixProject, "kiln")
	fixtureCollaborator = control.DerivedID(control.PrefixUser, "sable")
)

const (
	narrowedToOneToken  = "fixture-narrowed-to-one-scope-not-a-real-token"
	narrowedToNoneToken = "fixture-narrowed-to-nothing-not-a-real-token"
	narrowedToAllToken  = "fixture-narrowed-to-every-scope-not-a-real-token"
)

func narrowedWorld(t *testing.T) *control.Cache {
	t.Helper()
	return fixtureCacheWith(t,
		control.Event{Kind: control.EventScopeCreated, At: fixtureClock, ScopeID: fixtureScopeTwo,
			DisplayName: "quarry-ledger", ProjectID: fixtureProject},
		control.Event{Kind: control.EventUserCreated, At: fixtureClock, UserID: fixtureCollaborator,
			Provider: "fixture-provider", Subject: "00000000-0000-4000-8000-000000000002",
			Email: "sable@notes.example.invalid"},
		control.Event{Kind: control.EventProjectCreated, At: fixtureClock, ProjectID: fixtureProjectKiln,
			Name: "kiln", UserID: fixtureUser},
		control.Event{Kind: control.EventMemberSet, At: fixtureClock, ProjectID: fixtureProjectKiln,
			UserID: fixtureUser, Role: control.RoleOwner},
		control.Event{Kind: control.EventMemberSet, At: fixtureClock, ProjectID: fixtureProjectKiln,
			UserID: fixtureCollaborator, Role: control.RoleMember},
		control.Event{Kind: control.EventCredentialIssued, At: fixtureClock, CredentialID: "crd_narrow_one",
			SubjectKind: control.KindUser, SubjectID: fixtureUser,
			TokenHash: control.HashToken(narrowedToOneToken), Label: "narrowed to one",
			NarrowedScopes: []control.ID{fixtureScope}},
		// 🔴 NON-NIL EMPTY: this credential sees NOTHING. `nil` is its opposite.
		control.Event{Kind: control.EventCredentialIssued, At: fixtureClock, CredentialID: "crd_narrow_none",
			SubjectKind: control.KindUser, SubjectID: fixtureUser,
			TokenHash: control.HashToken(narrowedToNoneToken), Label: "narrowed to nothing",
			NarrowedScopes: []control.ID{}},
		control.Event{Kind: control.EventCredentialIssued, At: fixtureClock, CredentialID: "crd_narrow_all",
			SubjectKind: control.KindUser, SubjectID: fixtureUser,
			TokenHash: control.HashToken(narrowedToAllToken), Label: "narrowed to everything",
			NarrowedScopes: []control.ID{fixtureScope, fixtureScopeTwo}},
	)
}

// sessionRows counts the records the session store holds, read from its file, so "no session
// was minted" is a fact about the store and not only about the absence of a header.
func (l *live) sessionRows() int {
	l.t.Helper()
	raw, err := os.ReadFile(l.sessions.Path())
	if err != nil {
		l.t.Fatalf("reading the session store: %v", err)
	}
	n := 0
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.TrimSpace(line) != "" {
			n++
		}
	}
	return n
}

// TestANarrowedCredentialCannotSignIn is the regression.
//
// 🔴 ON THE PRE-FIX CODE THE SIGN-IN SUCCEEDS, AND THE TEST THEN PROVES THE LEAK RATHER THAN
// A MISSING STRING: it resolves the minted cookie through the REAL cookie backend over the
// same store and authority and asserts what that session can read. A session opened from a
// credential narrowed to `quarry-notes` reads `quarry-ledger` too, because a session is keyed
// on the principal and re-resolves the principal's full authority on every request.
func TestANarrowedCredentialCannotSignIn(t *testing.T) {
	authority := narrowedWorld(t)

	// The precondition the whole test rests on: the credential really is narrowed, and the
	// principal behind it really reaches the scope the narrowing excludes. Without both, a
	// refusal below would be about something else.
	_, narrowedAuth, err := authority.Authenticate(narrowedToOneToken)
	if err != nil {
		t.Fatalf("precondition: the narrowed credential does not authenticate at all: %v", err)
	}
	if narrowedAuth.Allows(fixtureScopeTwo, control.VerbRead) {
		t.Fatal("precondition: the narrowing does not exclude quarry-ledger, so nothing below measures a widening")
	}
	if p, _, _ := authority.Authenticate(testCredential); !control.Resolve(authority.Model(), p).Allows(fixtureScopeTwo, control.VerbRead) {
		t.Fatal("precondition: the fixture user cannot read quarry-ledger at all, so a widening would be invisible")
	}

	for _, tc := range []struct{ name, token string }{
		{"narrowed to a strict subset", narrowedToOneToken},
		{"narrowed to NOTHING (non-nil empty)", narrowedToNoneToken},
		// Decided: refused. It is still a narrowed credential, and a session would silently
		// pick up every scope the principal is granted AFTER sign-in, which the narrowing
		// was issued to exclude.
		{"narrowed to a list equal to today's full set", narrowedToAllToken},
	} {
		t.Run(tc.name, func(t *testing.T) {
			l := newLiveOver(t, authority, nil, nil, nil)
			rec := l.do(http.MethodPost, SignInPath, url.Values{FieldToken: {tc.token}})

			if c := sessionCookieOf(t, rec); c != nil {
				// Measure what the minted session can actually read — the real impact.
				cookie, err := identity.NewCookieSession(l.sessions, authority)
				if err != nil {
					t.Fatalf("the cookie backend did not build: %v", err)
				}
				r := httptest.NewRequest(http.MethodGet, RootPath, nil)
				r.AddCookie(c)
				id, err := cookie.Authenticate(r)
				if err == nil && id.Auth.Allows(fixtureScopeTwo, control.VerbRead) {
					t.Fatalf("A NARROWED CREDENTIAL OPENED A SESSION THAT READS A SCOPE OUTSIDE ITS NARROWING "+
						"(quarry-ledger); sign-in answered %d. The session re-derives the principal's full "+
						"authority, so the narrowing was discarded at the door.", rec.Code)
				}
				t.Fatalf("a narrowed credential was given a session cookie (status %d)", rec.Code)
			}
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("status %d, want 401", rec.Code)
			}
			if n := l.sessionRows(); n != 0 {
				t.Fatalf("the session store holds %d row(s) after a refused sign-in, want 0", n)
			}
			if !strings.Contains(l.log.String(), "sign-in refused: the credential is narrowed") {
				t.Errorf("the operator log does not say why:\n%s", l.log.String())
			}
			if strings.Contains(l.log.String(), tc.token) {
				t.Error("THE PRESENTED TOKEN REACHED THE LOG")
			}
		})
	}

	// POSITIVE CONTROL, over the SAME world: the un-narrowed credential still signs in, and
	// its session reads both scopes. Without this, the refusals above could be a guard that
	// refuses everybody.
	t.Run("an un-narrowed credential still signs in", func(t *testing.T) {
		l := newLiveOver(t, authority, nil, nil, nil)
		c := l.signIn(testCredential)
		cookie, err := identity.NewCookieSession(l.sessions, authority)
		if err != nil {
			t.Fatalf("the cookie backend did not build: %v", err)
		}
		r := httptest.NewRequest(http.MethodGet, RootPath, nil)
		r.AddCookie(c)
		id, err := cookie.Authenticate(r)
		if err != nil {
			t.Fatalf("POSITIVE CONTROL FAILED: the un-narrowed session does not authenticate: %v", err)
		}
		if !id.Auth.Allows(fixtureScopeTwo, control.VerbRead) || !id.Auth.Allows(fixtureScope, control.VerbRead) {
			t.Fatal("POSITIVE CONTROL FAILED: the un-narrowed session does not read both scopes")
		}
		if n := l.sessionRows(); n != 1 {
			t.Fatalf("POSITIVE CONTROL: the store holds %d rows after one sign-in, want 1", n)
		}
	})
}

// TestANarrowedSignInIsIndistinguishableFromAWrongToken pins the refusal byte for byte.
// A different status or sentence would tell a caller "this token is real, only narrowed".
func TestANarrowedSignInIsIndistinguishableFromAWrongToken(t *testing.T) {
	authority := narrowedWorld(t)
	wrong := newLiveOver(t, authority, nil, nil, nil).do(http.MethodPost, SignInPath,
		url.Values{FieldToken: {"a-wrong-credential-value"}})
	for _, token := range []string{narrowedToOneToken, narrowedToNoneToken, narrowedToAllToken} {
		got := newLiveOver(t, authority, nil, nil, nil).do(http.MethodPost, SignInPath,
			url.Values{FieldToken: {token}})
		if got.Code != wrong.Code {
			t.Errorf("status %d for a narrowed credential, %d for a wrong one", got.Code, wrong.Code)
		}
		if got.Body.String() != wrong.Body.String() {
			t.Errorf("the narrowed refusal's body differs from a wrong token's:\n--- narrowed\n%s\n--- wrong\n%s",
				got.Body.String(), wrong.Body.String())
		}
	}
	if wrong.Code != http.StatusUnauthorized || !strings.Contains(wrong.Body.String(), signInRefused) {
		t.Fatalf("CONTROL: the wrong-token refusal is not the uniform 401 (%d)", wrong.Code)
	}
}

// TestANarrowedSignInCountsTowardTheLockout pins the decision that a narrowed credential is a
// FAILED sign-in for the limiter, exactly as a wrong token is. With a one-failure limiter, one
// narrowed attempt must lock the client out, so the VALID credential that follows is refused.
func TestANarrowedSignInCountsTowardTheLockout(t *testing.T) {
	l := newLiveOver(t, narrowedWorld(t), nil, nil, oneFailureLimiter())
	const peer = "203.0.113.9:1111"
	l.doFrom(peer, nil, url.Values{FieldToken: {narrowedToOneToken}})
	before := l.log.String()
	rec := l.doFrom(peer, nil, rightToken(t))
	after := strings.TrimPrefix(l.log.String(), before)
	if rec.Code != http.StatusUnauthorized || strings.Contains(after, "a session was opened") {
		t.Fatalf("a narrowed attempt did not count toward the lockout: the next valid sign-in answered %d\n%s",
			rec.Code, after)
	}
	if !strings.Contains(after, "is locked out") {
		t.Fatalf("the follow-up was refused for a reason other than the lockout:\n%s", after)
	}
	// CONTROL: a different client, same valid credential, signs in — the limiter is per client.
	if rec := l.doFrom("203.0.113.10:1111", nil, rightToken(t)); rec.Code != http.StatusSeeOther {
		t.Fatalf("CONTROL FAILED: a fresh client with the valid credential answered %d", rec.Code)
	}
}

// TestANarrowedBearerCannotMintAnInvitation is the SAME defect through the other door.
//
// 🔴 THE INVITE FLOW AUTHORISES FROM THE PRINCIPAL'S PROJECT ROLE, WHICH NO SCOPE NARROWING
// BOUNDS. A narrowed bearer token reaches `POST /invite` through the machine-token backend and
// passes the CSRF gate with a self-chosen cookie (see `csrfTokenValid`). On the pre-fix code it
// mints an invitation into the owner's project — which the holder can redeem as an identity
// they control, for membership across every scope in it. The un-narrowed bearer is the
// positive control: the same request with it mints, so the refusal is about the narrowing.
func TestANarrowedBearerCannotMintAnInvitation(t *testing.T) {
	authority := narrowedWorld(t)
	inviting := ControlInviting{Authority: authority, Invites: newMemInvites(),
		Now: func() time.Time { return fixtureClock }}

	mint := func(t *testing.T, bearer string) *httptest.ResponseRecorder {
		t.Helper()
		l := newLiveOver(t, authority, inviting, nil, nil)
		return l.bearerDo(http.MethodPost, InvitePath, bearer,
			url.Values{FieldProject: {string(fixtureProject)}, FieldRole: {string(control.RoleMember)}})
	}

	if rec := mint(t, testCredential); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), JoinPath+"?") {
		t.Fatalf("POSITIVE CONTROL FAILED: the un-narrowed bearer could not mint (status %d), so a refusal "+
			"below would not be about the narrowing:\n%s", rec.Code, rec.Body.String())
	}
	for _, token := range []string{narrowedToOneToken, narrowedToNoneToken, narrowedToAllToken} {
		rec := mint(t, token)
		if strings.Contains(rec.Body.String(), JoinPath+"?") {
			t.Fatalf("A NARROWED BEARER MINTED AN INVITATION into the owner's project (status %d): "+
				"membership authority is not bounded by a scope narrowing", rec.Code)
		}
		if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), inviteWriteRefusal) {
			t.Fatalf("a narrowed bearer's mint answered %d, want the uniform 403 %q:\n%s",
				rec.Code, inviteWriteRefusal, rec.Body.String())
		}
	}
}
