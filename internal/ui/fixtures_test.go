package ui

import (
	"context"
	"testing"
	"time"

	"github.com/ZacxDev/cairn/internal/control"
)

// The synthetic world the session tests authenticate against.
//
// 🔴 EVERY NAME AND THE ONE CREDENTIAL ARE INVENTED. This repository is public and was
// extracted from a private one; a credential that LOOKS real is worse than one that is,
// because nobody can tell. `example.invalid` is IANA-reserved and resolves nowhere, and
// the fixture date is year 2000, which is what `tests/leakscan.py` allows.
var (
	fixtureUser    = control.DerivedID(control.PrefixUser, "rowan")
	fixtureProject = control.DerivedID(control.PrefixProject, "quarry")
	fixtureScope   = control.DerivedID(control.PrefixScope, "quarry-notes")
	fixtureClock   = time.Date(2000, 6, 1, 12, 0, 0, 0, time.UTC)
)

// fixtureCache is the ONE materialized world, built once per call and shared by the
// machine-token backend, the cookie backend and the sign-in exchange.
//
// 🔴 ONE AUTHORITY FOR ALL THREE, WHICH IS WHAT THE WIRING DOES. `cmd/cairn-ui` passes
// the same `*control.Cache` as `Config.Credentials` and to both backends; a fixture that
// gave them separate worlds would let a defect in which world a path reads pass unseen.
func fixtureCache(t *testing.T) *control.Cache {
	t.Helper()
	m, err := control.Replay([]control.Event{
		{Kind: control.EventUserCreated, At: fixtureClock, UserID: fixtureUser,
			Provider: "fixture-provider", Subject: "00000000-0000-4000-8000-000000000001",
			Email: "rowan@notes.example.invalid"},
		{Kind: control.EventProjectCreated, At: fixtureClock, ProjectID: fixtureProject, Name: "quarry", UserID: fixtureUser},
		{Kind: control.EventMemberSet, At: fixtureClock, ProjectID: fixtureProject, UserID: fixtureUser, Role: control.RoleOwner},
		{Kind: control.EventScopeCreated, At: fixtureClock, ScopeID: fixtureScope, DisplayName: "quarry-notes", ProjectID: fixtureProject},
		{Kind: control.EventCredentialIssued, At: fixtureClock, CredentialID: "crd_fixture",
			SubjectKind: control.KindUser, SubjectID: fixtureUser,
			TokenHash: control.HashToken(testCredential), Label: "fixture"},
	})
	if err != nil {
		t.Fatalf("building the fixture world: %v", err)
	}
	c := control.NewCache(fixtureSource{m}, control.CacheOptions{Now: func() time.Time { return fixtureClock }})
	if err := c.Refresh(context.Background()); err != nil {
		t.Fatalf("materializing the fixture world: %v", err)
	}
	return c
}

type fixtureSource struct{ m control.Model }

func (s fixtureSource) Model(context.Context) (control.Model, error) { return s.m, nil }
