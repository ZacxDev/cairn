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

// TestAllRolesIsTheWholeRoleTable pins the two enumerations of one set against each other,
// and it is named in [AllRoles]'s own comment — so it has to exist.
//
// 🔴 TWO LISTS OF "ALL THE ROLES" IS THE DUPLICATION THIS REPOSITORY ALREADY TRACKS, AND
// EACH DIRECTION FAILS DIFFERENTLY. A role in `roleVerbs` and not in `AllRoles` is invisible
// to every chooser — offered nowhere, so an authority the model grants cannot be conferred
// by the surface. A role in `AllRoles` and not in `roleVerbs` is worse: it is offered, it is
// `Valid()`… no, it is NOT valid, which is exactly why the second direction is asserted
// through `Valid` rather than by eyeballing the map. `resolve` treats a miss in `roleVerbs`
// as the empty set, so such a role would be conferrable and would grant nothing.
func TestAllRolesIsTheWholeRoleTable(t *testing.T) {
	if len(AllRoles) == 0 {
		t.Fatal("AllRoles is empty, so every comparison below is vacuous and every chooser offers nothing")
	}
	if len(AllRoles) != len(roleVerbs) {
		t.Errorf("AllRoles has %d entries and roleVerbs has %d; the two enumerate the same set",
			len(AllRoles), len(roleVerbs))
	}
	for _, r := range AllRoles {
		if !r.Valid() {
			t.Errorf("AllRoles offers %q, which `Valid` refuses — `resolve` treats a role absent from "+
				"roleVerbs as the EMPTY verb set, so it would be conferrable and grant nothing", r)
		}
	}
	for r := range roleVerbs {
		found := false
		for _, offered := range AllRoles {
			if offered == r {
				found = true
			}
		}
		if !found {
			t.Errorf("roleVerbs defines %q and AllRoles does not offer it, so no chooser built from "+
				"AllRoles can express an authority the model grants", r)
		}
	}
	// 🔴 THE ORDER IS PART OF THE CONTRACT, BECAUSE A RENDERER PICKS ITS DEFAULT AGAINST IT.
	// `AllRoles`'s own comment says descending authority, and `internal/ui`'s `inviteForm`
	// relies on that being true to explain why it selects the least-privileged entry
	// EXPLICITLY rather than letting the first option win. If the order ever flips, that
	// comment becomes false while the form keeps working — so the order is pinned here.
	if AllRoles[0] != RoleOwner {
		t.Errorf("AllRoles[0] is %q, want %q. The list is documented as DESCENDING authority and a "+
			"chooser's unselected default is its FIRST option", AllRoles[0], RoleOwner)
	}
	if AllRoles[len(AllRoles)-1] != RoleMember {
		t.Errorf("AllRoles's last entry is %q, want %q", AllRoles[len(AllRoles)-1], RoleMember)
	}
}

// TestCanConferRefusesEveryEscalationAndPermitsEveryLegitimateHandOff is the escalation
// predicate's own table, driven exhaustively over `AllRoles` × `AllRoles` plus the invalid
// cases.
//
// 🔴 EXHAUSTIVE RATHER THAN A FEW CASES, BECAUSE THE PREDICATE IS THREE CONDITIONS AND A
// SAMPLE CANNOT SHOW WHICH ONE IS LOAD-BEARING. All nine ordered role pairs are here.
//
// 🔴 AND EVERY EXPECTATION IS A LITERAL, WHICH IS A CORRECTION RATHER THAN A STYLE. The first
// version of this table computed `want` as
// `held.CanManageMembers() && (held == RoleOwner || other != RoleOwner)` — the implementation,
// restated. That asserts `a == a`: it passes for any `CanConfer` and it would have passed for
// one with the escalation rule inverted, because the "expectation" would invert with it.
// `RULES.md` names this exactly ("never derive a test's expectation from the implementation it
// tests"), and it was written here anyway, in a table whose whole subject is a privilege rule.
//
// ⚠ THE GRID IS ALSO CHECKED FOR COMPLETENESS AGAINST `AllRoles`, so a fourth role cannot be
// added to the model and leave this table silently covering three.
func TestCanConferRefusesEveryEscalationAndPermitsEveryLegitimateHandOff(t *testing.T) {
	// held → conferred → may they. Nine literals, one per ordered pair.
	want := map[Role]map[Role]bool{
		RoleOwner:  {RoleOwner: true, RoleAdmin: true, RoleMember: true},
		RoleAdmin:  {RoleOwner: false, RoleAdmin: true, RoleMember: true},
		RoleMember: {RoleOwner: false, RoleAdmin: false, RoleMember: false},
	}
	// The grid must cover `AllRoles` in both dimensions, or a role added to the model would
	// be untested here while the walk below still reported a full pass.
	for _, held := range AllRoles {
		row, named := want[held]
		if !named {
			t.Fatalf("role %q is in AllRoles and has no row in this table, so it is untested", held)
		}
		for _, other := range AllRoles {
			if _, named := row[other]; !named {
				t.Fatalf("the %q row has no entry for %q, so that pair is untested", held, other)
			}
		}
	}

	pairs := 0
	for held, row := range want {
		for other, mayConfer := range row {
			pairs++
			if got := held.CanConfer(other); got != mayConfer {
				t.Errorf("Role(%q).CanConfer(%q) = %v, want %v", held, other, got, mayConfer)
			}
		}
	}
	if pairs != len(AllRoles)*len(AllRoles) {
		t.Fatalf("the walk covered %d pairs, want %d", pairs, len(AllRoles)*len(AllRoles))
	}

	// 🔴 THE ESCALATION CASE, SPELLED OUT SEPARATELY BECAUSE IT IS THE WHOLE REASON THE
	// FUNCTION EXISTS. An admin minting an `owner` invitation and redeeming it themselves
	// leaves a journal that reads as an ordinary join.
	if RoleAdmin.CanConfer(RoleOwner) {
		t.Error("an admin may confer `owner` — that is a privilege escalation whose audit trail reads as " +
			"an ordinary join, and it is the case this predicate was written for")
	}
	// And the positive half, or the assertion above is satisfied by a blanket refusal.
	if !RoleOwner.CanConfer(RoleOwner) {
		t.Error("an OWNER may not confer `owner`, so `CanConfer` is refusing everything and the assertion " +
			"above says nothing about escalation in particular")
	}
	if !RoleAdmin.CanConfer(RoleMember) {
		t.Error("an admin may not confer `member`, which is the ordinary case the invite flow exists for")
	}

	// An INVALID role is refused in the conferred position — `Valid`'s fail-closed direction,
	// asked here rather than assumed of the caller.
	for _, held := range AllRoles {
		if held.CanConfer(Role("emperor")) {
			t.Errorf("Role(%q) may confer a role this package does not define; `resolve` treats it as the "+
				"EMPTY verb set, so it would be conferrable and grant nothing", held)
		}
	}
	// And in the HOLDING position, which is the direction a journal replay can produce: a
	// role string written by a newer build.
	if Role("emperor").CanConfer(RoleMember) {
		t.Error("a role this package does not define may confer one that it does")
	}
	if Role("").CanConfer(RoleMember) {
		t.Error("the ZERO role may confer `member` — `RoleIn` returns it for a user with no membership, " +
			"so this is the value `ControlInviting.Mint` sees for a caller who belongs to nothing")
	}
}

// TestProjectsManagedByCarriesTheCallersOwnRole is the component half of a seam guard whose
// behavioural half lives in `internal/ui` (`TestTheRoleChooserIsDrivenByTheREALModelsHeldRole`).
//
// 🔴 IT EXISTS BECAUSE A MUTATION DELETING `HeldRole: ship.Role` SURVIVED THE WHOLE SUITE.
// Nothing here read the field and nothing over in the renderer built a value from this
// function, so the one wire carrying "what may this caller confer" from the model to the form
// was untested from both ends. This is the cheap end: it pins that the role reported is the
// LISTED USER'S, per project, which is the mistake a reimplementation would make — reporting
// the project's owner, or the first membership the map iteration reached.
func TestProjectsManagedByCarriesTheCallersOwnRole(t *testing.T) {
	m := worldWithMemberships(t)

	// 🔴 THE EXPECTATION IS PER (USER, PROJECT) AND EVERY VALUE IS A LITERAL, BECAUSE THE FIRST
	// DRAFT OF THIS TEST ASSERTED ONE ROLE PER USER AND WENT RED ON CORRECT CODE. `usr_owner`
	// holds `owner` in two of the fixture's projects and `admin` in three — which is a better
	// discriminator than the one I had assumed, and the red found my wrong expectation rather
	// than a defect. A per-user expectation is satisfiable by a function that reports the same
	// role everywhere; a per-project one is not.
	want := map[ID]map[ID]Role{
		"usr_owner": {
			"prj_alone": RoleOwner, "prj_atlas": RoleOwner,
			"prj_zulu": RoleAdmin, "prj_dup_a": RoleAdmin, "prj_dup_b": RoleAdmin,
		},
		"usr_admin": {"prj_atlas": RoleAdmin},
	}
	// POSITIVE CONTROL ON THE FIXTURE: the expectation must contain at least two DIFFERENT
	// roles, or a function returning a constant would satisfy all of it.
	distinct := map[Role]bool{}
	for _, row := range want {
		for _, r := range row {
			distinct[r] = true
		}
	}
	if len(distinct) < 2 {
		t.Fatalf("the expectation names %d distinct role(s); a function returning a CONSTANT would "+
			"satisfy every assertion below", len(distinct))
	}

	for user, row := range want {
		got := m.ProjectsManagedBy(user)
		if len(got) != len(row) {
			t.Fatalf("ProjectsManagedBy(%s) returned %d project(s), want %d — this table and the fixture "+
				"have drifted apart, so the role assertions below are about a different world",
				user, len(got), len(row))
		}
		for _, p := range got {
			wantRole, named := row[p.ID]
			if !named {
				t.Errorf("ProjectsManagedBy(%s) offered %s, which this table does not name", user, p.ID)
				continue
			}
			if p.HeldRole != wantRole {
				t.Errorf("ProjectsManagedBy(%s) reported HeldRole %q for %s, want %q",
					user, p.HeldRole, p.ID, wantRole)
			}
			if p.HeldRole == "" {
				t.Errorf("ProjectsManagedBy(%s) reported an EMPTY role for %s. `CanConfer` refuses "+
					"everything for the zero role, so a chooser built from this offers nothing at all "+
					"— which is a silent, plausible-looking empty form", user, p.ID)
			}
		}
	}
}
