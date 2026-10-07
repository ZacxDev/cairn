package presence

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/ZacxDev/cairn/internal/control"
	"github.com/ZacxDev/cairn/internal/identity"
)

// The synthetic world every test here runs over. Every name, id and credential is invented
// (this repository is public); dates are year 2000. Values are PAIRWISE DISTINCT so a mutant
// that confuses two of them cannot pass by coincidence.
var (
	userA    = control.DerivedID(control.PrefixUser, "presence-owner-a")
	userB    = control.DerivedID(control.PrefixUser, "presence-owner-b")
	projectA = control.DerivedID(control.PrefixProject, "presence-project")
	scopeOne = control.DerivedID(control.PrefixScope, "presence-scope-one")
	scopeTwo = control.DerivedID(control.PrefixScope, "presence-scope-two")
	clock0   = time.Date(2000, 1, 2, 3, 4, 5, 0, time.UTC)

	ownerA = Owner{Kind: control.KindUser, ID: userA}
	ownerB = Owner{Kind: control.KindUser, ID: userB}
)

const (
	plainTokenA       = "fixture-presence-plain-credential-of-owner-a-not-real"
	narrowedTokenA    = "fixture-presence-narrowed-credential-of-owner-a-not-real"
	narrowedAllTokenA = "fixture-presence-narrowed-to-all-of-owner-a-not-real"
	emptyNarrowTokenA = "fixture-presence-narrowed-to-none-of-owner-a-not-real"
	plainTokenB       = "fixture-presence-plain-credential-of-owner-b-not-real"
)

type fixtureSource struct{ m control.Model }

func (s fixtureSource) Model(context.Context) (control.Model, error) { return s.m, nil }

// world is the control plane: owners A and B, one project of A's with two scopes, and four
// credentials for A — one plain and three narrowed (to one scope, to every scope, to nothing).
func world(t *testing.T) *control.Cache {
	t.Helper()
	m, err := control.Replay([]control.Event{
		{Kind: control.EventUserCreated, At: clock0, UserID: userA, Provider: "fixture-provider",
			Subject: "00000000-0000-4000-8000-0000000000a1", Email: "owner-a@notes.example.invalid"},
		{Kind: control.EventUserCreated, At: clock0, UserID: userB, Provider: "fixture-provider",
			Subject: "00000000-0000-4000-8000-0000000000b2", Email: "owner-b@notes.example.invalid"},
		{Kind: control.EventProjectCreated, At: clock0, ProjectID: projectA, Name: "presence-project", UserID: userA},
		{Kind: control.EventMemberSet, At: clock0, ProjectID: projectA, UserID: userA, Role: control.RoleOwner},
		{Kind: control.EventScopeCreated, At: clock0, ScopeID: scopeOne, DisplayName: "presence-scope-one", ProjectID: projectA},
		{Kind: control.EventScopeCreated, At: clock0, ScopeID: scopeTwo, DisplayName: "presence-scope-two", ProjectID: projectA},
		{Kind: control.EventCredentialIssued, At: clock0, CredentialID: "crd_presence_plain_a",
			SubjectKind: control.KindUser, SubjectID: userA, TokenHash: control.HashToken(plainTokenA), Label: "plain"},
		{Kind: control.EventCredentialIssued, At: clock0, CredentialID: "crd_presence_narrow_a",
			SubjectKind: control.KindUser, SubjectID: userA, TokenHash: control.HashToken(narrowedTokenA),
			Label: "narrowed to one", NarrowedScopes: []control.ID{scopeOne}},
		{Kind: control.EventCredentialIssued, At: clock0, CredentialID: "crd_presence_narrowall_a",
			SubjectKind: control.KindUser, SubjectID: userA, TokenHash: control.HashToken(narrowedAllTokenA),
			Label: "narrowed to all", NarrowedScopes: []control.ID{scopeOne, scopeTwo}},
		{Kind: control.EventCredentialIssued, At: clock0, CredentialID: "crd_presence_narrownone_a",
			SubjectKind: control.KindUser, SubjectID: userA, TokenHash: control.HashToken(emptyNarrowTokenA),
			Label: "narrowed to none", NarrowedScopes: []control.ID{}},
		{Kind: control.EventCredentialIssued, At: clock0, CredentialID: "crd_presence_plain_b",
			SubjectKind: control.KindUser, SubjectID: userB, TokenHash: control.HashToken(plainTokenB), Label: "plain b"},
	})
	if err != nil {
		t.Fatalf("building the fixture world: %v", err)
	}
	c := control.NewCache(fixtureSource{m}, control.CacheOptions{Now: func() time.Time { return clock0 }})
	if err := c.Refresh(context.Background()); err != nil {
		t.Fatalf("materializing: %v", err)
	}
	return c
}

// bearerIdentity authenticates `token` through the REAL machine-token backend, which is how a
// bearer credential reaches the browser surface — so the narrowed bit tested here is the one a
// request actually carries, not one a test constructed.
func bearerIdentity(t *testing.T, authority *control.Cache, token string) identity.Identity {
	t.Helper()
	machine, err := identity.NewMachineToken(authority)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Authorization", "Bearer "+token)
	id, err := machine.Authenticate(r)
	if err != nil {
		t.Fatalf("the fixture credential did not authenticate: %v", err)
	}
	return id
}

// cookieIdentity authenticates a session for `user` through the REAL cookie backend.
func cookieIdentity(t *testing.T, authority *control.Cache, user control.ID) identity.Identity {
	t.Helper()
	sessions, err := identity.OpenFileSessionStore(filepath.Join(t.TempDir(), "sessions"))
	if err != nil {
		t.Fatal(err)
	}
	// The session store's own clock, set to the fixture instant so the record is live.
	sessions.Now = func() time.Time { return clock0 }
	id, err := identity.NewSessionID()
	if err != nil {
		t.Fatal(err)
	}
	if err := sessions.Create(identity.Session{Digest: identity.SessionDigest(id), Kind: control.KindUser,
		Principal: user, IssuedAt: clock0, ExpiresAt: clock0.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	cookie, err := identity.NewCookieSession(sessions, authority)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.AddCookie(&http.Cookie{Name: identity.SessionCookieName, Value: id})
	got, err := cookie.Authenticate(r)
	if err != nil {
		t.Fatalf("the fixture session did not authenticate: %v", err)
	}
	return got
}

// fakeClock is a settable instant shared by a store and a queue.
type fakeClock struct{ now time.Time }

func (c *fakeClock) Now() time.Time { return c.now }

func row(session, lastActivity string) Row {
	r := Row{Session: session, Runtime: "claude", Target: "notes:3", Label: "notes", Hotkey: "Alt+n",
		LastActivity: lastActivity}
	if lastActivity != "" {
		t, err := time.Parse(time.RFC3339, lastActivity)
		if err != nil {
			panic(err)
		}
		r.activity = t
	}
	return r
}
