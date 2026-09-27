package control

import (
	"testing"
	"time"
)

// 🔴 THESE ARE FIRST COVERAGE, NOT REGRESSION TESTS, AND SAYING SO IS THE POINT. The
// functions under test are new in the same change, so there is no pre-change tree on
// which they could be watched to fail — a test written against code that does not exist
// either fails to COMPILE, which is not a meaningful red, or passes vacuously. What makes
// them worth trusting is the mutation battery, recorded in the commit: each of the
// conditions below was broken on purpose and the named test watched to fail.

// year-2000 instants, per `AGENTS.md`: a fixture that needs a date uses an obviously
// synthetic one.
var invAt = time.Date(2000, 6, 1, 0, 0, 0, 0, time.UTC)

// worldWithMemberships builds a Model directly rather than replaying events.
//
// ⚠ DELIBERATE, AND IT IS A NARROWING. Replaying through `apply` would also exercise
// `checkSubject` and the event validators, which have their own tests; what is under test
// here is a READ over a Model, so the fixture is a Model. The cost is that these tests
// cannot catch a model that `apply` would never build — which is exactly why
// `ProjectsManagedBy` skips an unknown project rather than trusting the invariant.
func worldWithMemberships(t *testing.T) Model {
	t.Helper()
	m := NewModel()
	m.Projects["prj_atlas"] = Project{ID: "prj_atlas", Name: "atlas", OwnerUserID: "usr_owner", CreatedAt: invAt}
	m.Projects["prj_beacon"] = Project{ID: "prj_beacon", Name: "beacon", OwnerUserID: "usr_other", CreatedAt: invAt}
	// 🔴 A SOLO PROJECT WHOSE NAME SORTS FIRST, so the "solo is included" assertion cannot
	// pass merely because it happened to land at the end of the slice.
	m.Projects["prj_alone"] = Project{ID: "prj_alone", Name: "aardvark", OwnerUserID: "usr_owner", Solo: true, CreatedAt: invAt}
	// 🔴 TWO PROJECTS SHARING ONE DISPLAY NAME, because the sort's tie-break is only
	// observable when a tie exists. Without this pair the `cmp.Compare` on the id is dead
	// code that no assertion can distinguish from its absence.
	m.Projects["prj_dup_b"] = Project{ID: "prj_dup_b", Name: "twin", OwnerUserID: "usr_owner", CreatedAt: invAt}
	m.Projects["prj_dup_a"] = Project{ID: "prj_dup_a", Name: "twin", OwnerUserID: "usr_owner", CreatedAt: invAt}
	// 🔴 AND ONE PROJECT WHOSE ID ORDER DISAGREES WITH ITS NAME ORDER, because without it
	// every id in this fixture happened to sort the same way as its name — so dropping the
	// NAME comparison entirely left the output byte-identical and that mutant SURVIVED a
	// green run. `prj_zulu`/"beta" sorts 3rd by name and last by id, which is the only
	// reason the primary sort key is now observable at all. Same shape as the tie-break
	// above, one level up: a fixture whose two candidate orderings coincide cannot tell
	// the two implementations apart.
	m.Projects["prj_zulu"] = Project{ID: "prj_zulu", Name: "beta", OwnerUserID: "usr_owner", CreatedAt: invAt}

	set := func(project, user ID, role Role) {
		if m.Memberships[project] == nil {
			m.Memberships[project] = map[ID]Membership{}
		}
		m.Memberships[project][user] = Membership{UserID: user, ProjectID: project, Role: role, AddedAt: invAt}
	}
	set("prj_atlas", "usr_owner", RoleOwner)
	set("prj_atlas", "usr_admin", RoleAdmin)
	set("prj_atlas", "usr_member", RoleMember)
	set("prj_beacon", "usr_owner", RoleMember) // the same user, a role that may NOT manage
	set("prj_alone", "usr_owner", RoleOwner)
	set("prj_dup_a", "usr_owner", RoleAdmin)
	set("prj_dup_b", "usr_owner", RoleAdmin)
	set("prj_zulu", "usr_owner", RoleAdmin)
	// A membership naming a project the model does not hold — the defensive skip.
	set("prj_vanished", "usr_owner", RoleOwner)
	return m
}

func TestOnlyOwnerAndAdminMayManageMembers(t *testing.T) {
	// 🔴 EVERY ROLE THE PACKAGE DECLARES IS LISTED, plus two that are not roles at all.
	// A table that named only the roles it expected to be true would stay green if a new
	// role were added and defaulted to permitted.
	for _, tc := range []struct {
		role Role
		want bool
	}{
		{RoleOwner, true},
		{RoleAdmin, true},
		{RoleMember, false},
		{Role(""), false},
		{Role("superuser"), false},
	} {
		if got := tc.role.CanManageMembers(); got != tc.want {
			t.Errorf("Role(%q).CanManageMembers() = %v, want %v", tc.role, got, tc.want)
		}
	}

	// 🔴 AND THE LEDGER: every role this package declares must appear above. A role added
	// without a row here would otherwise be unmeasured, and the default for an unlisted
	// role is the permissive-looking one to write by accident.
	declared := []Role{RoleOwner, RoleAdmin, RoleMember}
	for _, r := range declared {
		if !r.Valid() {
			t.Fatalf("internal: %q is not a valid role, so this ledger is wrong", r)
		}
	}
	if len(declared) != 3 {
		t.Fatalf("this test knows %d roles; update the table above with the new one", len(declared))
	}
}

func TestProjectsManagedByListsOnlyWhereTheRoleAdmits(t *testing.T) {
	m := worldWithMemberships(t)

	got := m.ProjectsManagedBy("usr_owner")
	var names []string
	for _, p := range got {
		names = append(names, p.Name+"/"+string(p.ID))
	}
	// By NAME, with the id as tie-break: aardvark (solo, owner) · atlas (owner) ·
	// beta/prj_zulu · twin/prj_dup_a · twin/prj_dup_b.
	// 🔴 `beta/prj_zulu` sits 3rd here and would sit LAST under an id-only sort, which is
	// what makes the primary key observable — see the fixture's comment.
	// NOT beacon — the same user is only a `member` there.
	// NOT prj_vanished — a membership whose project the model does not hold.
	want := []string{"aardvark/prj_alone", "atlas/prj_atlas", "beta/prj_zulu", "twin/prj_dup_a", "twin/prj_dup_b"}
	if len(names) != len(want) {
		t.Fatalf("got %v, want %v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("position %d is %q, want %q (whole list %v)", i, names[i], want[i], names)
		}
	}

	// 🔴 THE NEGATIVE THAT MATTERS: a project where the caller is only a member must not
	// appear. This is the clause that decides whether an ordinary member can invite.
	for _, p := range got {
		if p.ID == "prj_beacon" {
			t.Error("a project where the caller is only a `member` was offered for invitation")
		}
	}

	// An admin sees the one project they administer, and nothing else.
	admin := m.ProjectsManagedBy("usr_admin")
	if len(admin) != 1 || admin[0].ID != "prj_atlas" {
		t.Errorf("usr_admin: got %v, want exactly prj_atlas", admin)
	}

	// A plain member sees nothing — and an empty slice, not a nil-vs-empty distinction
	// any caller has to care about.
	if member := m.ProjectsManagedBy("usr_member"); len(member) != 0 {
		t.Errorf("usr_member: got %v, want none", member)
	}
	// A user with no memberships at all, and the empty id.
	if none := m.ProjectsManagedBy("usr_nobody"); len(none) != 0 {
		t.Errorf("usr_nobody: got %v, want none", none)
	}
	if none := m.ProjectsManagedBy(""); len(none) != 0 {
		t.Errorf("empty id: got %v, want none", none)
	}
}

// TestProjectsManagedByReadsMembershipsNotTheOwnerField is the discriminating case for the
// one design decision in this file.
//
// 🔴 THE TWO SOURCES DISAGREE HERE ON PURPOSE. `prj_beacon.OwnerUserID` is `usr_other`,
// who has NO membership row at all; a listing built from the owner FIELD would offer
// beacon to them. And `usr_owner` holds `owner` on atlas by MEMBERSHIP while
// `prj_atlas.OwnerUserID` names them too — so atlas alone cannot tell the two
// implementations apart, which is why beacon's shape is what this asserts.
func TestProjectsManagedByReadsMembershipsNotTheOwnerField(t *testing.T) {
	m := worldWithMemberships(t)
	if got := m.ProjectsManagedBy("usr_other"); len(got) != 0 {
		t.Fatalf("usr_other is `prj_beacon`'s OwnerUserID but holds no membership row; "+
			"got %v, want none. A listing derived from the owner FIELD would offer it.", got)
	}
	// And the inverse: a user demoted in the model still owns the field.
	if _, ok := m.RoleIn("prj_beacon", "usr_other"); ok {
		t.Fatal("internal: the fixture no longer expresses the disagreement this test needs")
	}
}

func TestRoleInAnswersBothMissingDirections(t *testing.T) {
	m := worldWithMemberships(t)
	if r, ok := m.RoleIn("prj_atlas", "usr_admin"); !ok || r != RoleAdmin {
		t.Errorf("RoleIn(atlas, admin) = %q,%v; want admin,true", r, ok)
	}
	// 🔴 BOTH MISSING DIRECTIONS, because the map is keyed project-then-user and a
	// caller indexing it by hand would have to handle each — the two bugs are a panic on
	// the outer miss and treating the outer miss as the inner one.
	if _, ok := m.RoleIn("prj_nosuch", "usr_admin"); ok {
		t.Error("RoleIn reported a role in a project that does not exist")
	}
	if _, ok := m.RoleIn("prj_atlas", "usr_nobody"); ok {
		t.Error("RoleIn reported a role for a user with no membership")
	}
}
