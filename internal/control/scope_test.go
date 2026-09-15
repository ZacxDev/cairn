package control

import "testing"

// TestMovingAScopeMovesItsOwnershipAuthority.
//
// 🔴 A SCOPE'S PROJECT BINDING IS MUTABLE, AND OWNERSHIP AUTHORITY IS DERIVED FROM IT,
// SO A MOVE IS AN AUTHORIZATION CHANGE WEARING THE COSTUME OF A FILING CHANGE. Nobody
// is granted anything and nobody is revoked; the same `Resolve` reads a different
// `ProjectID` and a different set of people can see the scope. That is the correct
// behaviour and it is the one most likely to surprise, which is why it is pinned rather
// than left to follow from the model.
func TestMovingAScopeMovesItsOwnershipAuthority(t *testing.T) {
	before := world(t)
	if got := verbsOrEmpty(Resolve(before, user(uDave)).VerbsOn(sAtlasRunbook)); got != "" {
		t.Fatalf("dave already has %q on atlas-runbook — this test cannot see the move", got)
	}

	m, err := Replay(append(worldEvents(),
		Event{Kind: EventScopeMoved, At: at(30), ScopeID: sAtlasRunbook, ProjectID: pBeacon, Actor: uAlice},
	))
	if err != nil {
		t.Fatalf("replay: %v", err)
	}

	// Beacon's owner gains it...
	if got := verbsOrEmpty(Resolve(m, user(uDave)).VerbsOn(sAtlasRunbook)); got != "read,write,admin" {
		t.Errorf("after the move dave has %q on atlas-runbook, want read,write,admin", got)
	}
	// ...and atlas's members lose it.
	if got := verbsOrEmpty(Resolve(m, user(uAlice)).VerbsOn(sAtlasRunbook)); got != "" {
		t.Errorf("after the move alice still has %q on atlas-runbook, want nothing", got)
	}
	// And erin, whose grant is over the PROJECT rather than over any scope, gains
	// it too — a project-as-object grant covers scopes added to the project after
	// the grant, which is the whole reason to grant a project.
	if got := verbsOrEmpty(Resolve(m, user(uErin)).VerbsOn(sAtlasRunbook)); got != "read" {
		t.Errorf("after the move erin has %q on atlas-runbook, want read — a project grant covers what the project holds NOW", got)
	}
}

// TestAMoveCannotCollideANameInsideTheDestination.
//
// Display names are unique within a project — that is what lets a caller holding a
// project resolve one without ambiguity — so a move that would break it is refused at
// the journal boundary rather than stored and disambiguated later.
func TestAMoveCannotCollideANameInsideTheDestination(t *testing.T) {
	_, err := Replay(append(worldEvents(),
		Event{Kind: EventScopeCreated, At: at(30), ScopeID: "scp_dup", DisplayName: "atlas-notes", ProjectID: pBeacon},
		Event{Kind: EventScopeMoved, At: at(31), ScopeID: sAtlasNotes, ProjectID: pBeacon, Actor: uAlice},
	))
	if err == nil {
		t.Fatal("a move that collides a name inside the destination project replayed cleanly")
	}
}

// TestRenamingAScopeChangesTheProjectedNameAndNotTheAuthority.
//
// 🔴 THE ID IS THE IDENTITY; THE PATH IS A PROJECTION. That split is what lets a
// syncing client see a known id at a new name and RENAME its local directory instead of
// re-downloading the scope, and it is what keeps a rename from being an authorization
// event. Both halves are asserted here because only together do they say what the split
// buys.
func TestRenamingAScopeChangesTheProjectedNameAndNotTheAuthority(t *testing.T) {
	m, err := Replay(append(worldEvents(),
		Event{Kind: EventScopeRenamed, At: at(30), ScopeID: sAtlasNotes, DisplayName: "atlas-journal", Actor: uAlice},
	))
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	a := Resolve(m, user(uCarol))

	// The authority is unchanged — same id, same verbs.
	if got := verbsOrEmpty(a.VerbsOn(sAtlasNotes)); got != "read,write,admin" {
		t.Errorf("after the rename carol has %q on the scope, want read,write,admin — a rename is not an authorization event", got)
	}
	// The PROJECTION follows the new name, and stops answering to the old one.
	vs := a.VisibleScopes(VerbRead)
	if !vs.Allows("atlas-journal") {
		t.Error("the projected ScopeSet does not allow the NEW name")
	}
	if vs.Allows("atlas-notes") {
		t.Error("the projected ScopeSet still allows the OLD name — the projection has to follow the rename or a stale directory keeps being served")
	}
}

// TestProjectAdministrationIsNotAScopeVerb.
//
// 🔴 OWNER AND ADMIN GET IDENTICAL SCOPE VERBS ON PURPOSE, AND THEIR DIFFERENCE LIVES
// HERE. Giving them different scope verbs to make the roles "look different" would put
// the owner/admin distinction in two places with no call site able to justify the
// second. This test is what makes that claim checkable rather than asserted in a
// comment.
func TestProjectAdministrationIsNotAScopeVerb(t *testing.T) {
	m := world(t)

	for _, tc := range []struct {
		who               ID
		project           ID
		mayAdmin, mayDrop bool
	}{
		{uAlice, pAtlas, true, true},   // owner
		{uBob, pAtlas, true, false},    // admin: manages, cannot delete
		{uCarol, pAtlas, false, false}, // member
		{uDave, pAtlas, false, false},  // not a member at all
		{uErin, pBeacon, false, false}, // granted every beacon SCOPE, and no standing in the project
		{uDave, pBeacon, true, true},
	} {
		if got := m.MayAdministerProject(tc.who, tc.project); got != tc.mayAdmin {
			t.Errorf("MayAdministerProject(%s, %s) = %v, want %v", tc.who, tc.project, got, tc.mayAdmin)
		}
		if got := m.MayDeleteProject(tc.who, tc.project); got != tc.mayDrop {
			t.Errorf("MayDeleteProject(%s, %s) = %v, want %v", tc.who, tc.project, got, tc.mayDrop)
		}
	}

	// And the other half of the claim: the two roles are indistinguishable on every
	// scope verb, so the test above is the ONLY thing separating them.
	alice, bob := Resolve(m, user(uAlice)), Resolve(m, user(uBob))
	for _, scopeID := range sortedLedgerScopes(uAlice) {
		if alice.VerbsOn(scopeID) != bob.VerbsOn(scopeID) {
			t.Errorf("owner and admin differ on %s (%q vs %q) — if that is intended, `roleVerbs` and this test both have to say so",
				scopeID, verbsOrEmpty(alice.VerbsOn(scopeID)), verbsOrEmpty(bob.VerbsOn(scopeID)))
		}
	}
}

// TestAmbiguousScopeNamesAreReportedRatherThanPicked.
//
// 🔴 A DISPLAY NAME IS NOT UNIQUE ACROSS PROJECTS, AND THAT IS LEGITIMATE — two teams
// may each keep a scope called `notes`. A resolver that silently picked the first would
// hand one project's caller the other project's scope id, which is a cross-tenant read
// dressed as a lookup.
func TestAmbiguousScopeNamesAreReportedRatherThanPicked(t *testing.T) {
	m, err := Replay(append(worldEvents(),
		Event{Kind: EventScopeCreated, At: at(30), ScopeID: "scp_beacon_dup", DisplayName: "atlas-notes", ProjectID: pBeacon},
	))
	if err != nil {
		t.Fatalf("replay: %v", err)
	}

	if _, err := m.ScopeByName("atlas-notes"); err == nil {
		t.Fatal("an ambiguous display name resolved to one scope")
	}
	// Within a project it is unambiguous, because `apply` refuses a collision there.
	got, found := m.ScopeByNameIn(pAtlas, "atlas-notes")
	if !found || got.ID != sAtlasNotes {
		t.Fatalf("ScopeByNameIn(atlas, atlas-notes) = %v/%v, want the atlas scope", got.ID, found)
	}
	if other, found := m.ScopeByNameIn(pBeacon, "atlas-notes"); !found || other.ID != "scp_beacon_dup" {
		t.Fatalf("ScopeByNameIn(beacon, atlas-notes) = %v/%v, want the beacon scope", other.ID, found)
	}
}
