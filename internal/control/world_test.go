package control

import (
	"testing"
	"time"
)

// The synthetic world every test in this package reasons about.
//
// 🔴 EVERY NAME HERE IS INVENTED. This repository is public and was extracted from
// a private one; `AGENTS.md` requires fixtures to be synthetic and `tests/leakscan.py`
// gates part of that mechanically. `atlas` and `beacon` are not anybody's projects,
// and the dates are the deliberately-synthetic year-2000 ones the scanner allows.
//
// 🔴 THE WORLD IS BUILT TO MAKE EVERY MECHANISM SEPARATELY OBSERVABLE. Each cell of
// the matrix below is reached by a KNOWN route, and the routes are chosen so that no
// two of them produce the same row:
//
//	ownership at three roles   — alice/bob/carol in `atlas`, whose rows differ only
//	                             in the admin column, which is the one thing
//	                             `roleVerbs` distinguishes
//	a personal cross-project   — carol on `beacon-notes`, read only
//	a project-as-SUBJECT share — `atlas` on `beacon-secrets`, so every atlas member
//	                             gains it without being named
//	a project-as-OBJECT share  — erin on all of `beacon`, while being a member of
//	                             nothing, so her row can only come from the grant
//	a UNION of two routes      — carol on `atlas-notes`: member (read,write) plus a
//	                             personal admin grant, which no single route gives
//	a REVOKED grant            — bob on `beacon-notes`, whose tombstone is the only
//	                             reason his row differs from what the grant says
//	a principal with NOTHING   — erin outside `beacon`, and everyone outside their
//	                             own project
//
// A resolver that ignored grants, ignored membership, ignored roles, ignored
// revocation, or expanded project subjects at the wrong time would each break a
// DIFFERENT set of cells. That is what makes this a relationship test rather than
// five component tests sharing a fixture.
const (
	uAlice = ID("usr_alice")
	uBob   = ID("usr_bob")
	uCarol = ID("usr_carol")
	uDave  = ID("usr_dave")
	uErin  = ID("usr_erin")

	pAtlas  = ID("prj_atlas")
	pBeacon = ID("prj_beacon")

	sAtlasNotes    = ID("scp_atlas_notes")
	sAtlasRunbook  = ID("scp_atlas_runbook")
	sBeaconNotes   = ID("scp_beacon_notes")
	sBeaconSecrets = ID("scp_beacon_secrets")
)

// t0 is a synthetic year-2000 instant — what `AGENTS.md` prescribes for a fixture
// that genuinely needs a date, and what `leakscan.py` allows.
var t0 = time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)

func at(n int) time.Time { return t0.Add(time.Duration(n) * time.Minute) }

// worldEvents is the journal that builds the fixture. Returned as EVENTS rather
// than as a hand-built Model on purpose: the tests then exercise `apply` on the way
// in, so a world that cannot be journalled cannot be tested against.
func worldEvents() []Event {
	return []Event{
		{Kind: EventUserCreated, At: at(1), UserID: uAlice, Provider: "example", Subject: "alice", Email: "alice@example.invalid"},
		{Kind: EventUserCreated, At: at(2), UserID: uBob, Provider: "example", Subject: "bob", Email: "bob@example.invalid"},
		{Kind: EventUserCreated, At: at(3), UserID: uCarol, Provider: "example", Subject: "carol", Email: "carol@example.invalid"},
		{Kind: EventUserCreated, At: at(4), UserID: uDave, Provider: "example", Subject: "dave", Email: "dave@example.invalid"},
		{Kind: EventUserCreated, At: at(5), UserID: uErin, Provider: "example", Subject: "erin", Email: "erin@example.invalid"},

		{Kind: EventProjectCreated, At: at(6), ProjectID: pAtlas, Name: "atlas", UserID: uAlice},
		{Kind: EventProjectCreated, At: at(7), ProjectID: pBeacon, Name: "beacon", UserID: uDave},

		{Kind: EventMemberSet, At: at(8), ProjectID: pAtlas, UserID: uAlice, Role: RoleOwner, Actor: uAlice},
		{Kind: EventMemberSet, At: at(9), ProjectID: pAtlas, UserID: uBob, Role: RoleAdmin, Actor: uAlice},
		{Kind: EventMemberSet, At: at(10), ProjectID: pAtlas, UserID: uCarol, Role: RoleMember, Actor: uAlice},
		{Kind: EventMemberSet, At: at(11), ProjectID: pBeacon, UserID: uDave, Role: RoleOwner, Actor: uDave},

		{Kind: EventScopeCreated, At: at(12), ScopeID: sAtlasNotes, DisplayName: "atlas-notes", ProjectID: pAtlas},
		{Kind: EventScopeCreated, At: at(13), ScopeID: sAtlasRunbook, DisplayName: "atlas-runbook", ProjectID: pAtlas},
		{Kind: EventScopeCreated, At: at(14), ScopeID: sBeaconNotes, DisplayName: "beacon-notes", ProjectID: pBeacon},
		{Kind: EventScopeCreated, At: at(15), ScopeID: sBeaconSecrets, DisplayName: "beacon-secrets", ProjectID: pBeacon},

		// G1 — a personal cross-project share, read only.
		{Kind: EventGranted, At: at(16), GrantID: "grt_1", Actor: uDave,
			SubjectKind: KindUser, SubjectID: uCarol,
			ObjectKind: ObjectScope, ObjectID: sBeaconNotes,
			Verbs: NewVerbSet(VerbRead)},

		// G2 — a PROJECT as subject. Every atlas member gains it, and none of them
		// is named by the grant.
		{Kind: EventGranted, At: at(17), GrantID: "grt_2", Actor: uDave,
			SubjectKind: KindProject, SubjectID: pAtlas,
			ObjectKind: ObjectScope, ObjectID: sBeaconSecrets,
			Verbs: NewVerbSet(VerbRead, VerbWrite)},

		// G3 — a PROJECT as object, to a user who is a member of nothing.
		{Kind: EventGranted, At: at(18), GrantID: "grt_3", Actor: uDave,
			SubjectKind: KindUser, SubjectID: uErin,
			ObjectKind: ObjectProject, ObjectID: pBeacon,
			Verbs: NewVerbSet(VerbRead)},

		// G4 — granted, then REVOKED. Bob's row is the tombstone's only witness.
		{Kind: EventGranted, At: at(19), GrantID: "grt_4", Actor: uDave,
			SubjectKind: KindUser, SubjectID: uBob,
			ObjectKind: ObjectScope, ObjectID: sBeaconNotes,
			Verbs: NewVerbSet(VerbRead, VerbWrite, VerbAdmin)},
		{Kind: EventGrantRevoked, At: at(20), GrantID: "grt_4", Actor: uDave},

		// G5 — a personal grant that UNIONS with carol's membership: member gives
		// read+write, this gives admin, and only the union gives all three.
		{Kind: EventGranted, At: at(21), GrantID: "grt_5", Actor: uAlice,
			SubjectKind: KindUser, SubjectID: uCarol,
			ObjectKind: ObjectScope, ObjectID: sAtlasNotes,
			Verbs: NewVerbSet(VerbAdmin)},
	}
}

func world(t *testing.T) Model {
	t.Helper()
	m, err := Replay(worldEvents())
	if err != nil {
		t.Fatalf("building the fixture world: %v", err)
	}
	return m
}

func user(id ID) Principal { return Principal{Kind: KindUser, ID: id} }
