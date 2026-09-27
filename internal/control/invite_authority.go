package control

import (
	"cmp"
	"slices"
)

// CanManageMembers answers whether this role may change who belongs to a project.
//
// # 🔴 WHY THIS IS A METHOD ON `Role` AND NOT A CONDITION AT THE CALL SITE
//
// "May this principal add somebody to this project" is about to have at least three
// readers — the invite mint page, the redemption path, and whatever eventually replaces
// `cairn-server -set-member` — and this repository has already had to consolidate a
// predicate that was open-coded at several sites, where it was measured wrong at all but
// one of them IN THE SAME DIRECTION. One spelling, here, is the cheap version of that
// lesson.
//
// # 🔴 IT IS NOT DERIVED FROM AN `Authorization`, AND THAT IS THE LOAD-BEARING PART
//
// `Resolve` computes authority over SCOPES: read, write, admin, per scope. Membership is
// a different axis — it is about the PROJECT, and a scope grant confers nothing over who
// belongs to the project that owns it. A reader looking for `auth.Allows(...)` here will
// not find it because there is nothing for it to ask: `Authorization` has no project
// dimension, so answering this from one would mean inventing a mapping from scope verbs
// to membership rights. The mapping that suggests itself — "admin on any of the project's
// scopes ⇒ may invite" — is WRONG in the direction that matters: a scope can be granted
// to an outsider with `admin`, and that grant must not become the power to add members to
// the project that owns it.
//
// ⚠ OWNER AND ADMIN, NOT MEMBER. A member can read and write what the project reaches and
// cannot change who else can — which is the same line `refuseOrphaning` draws from the
// other side when it refuses to remove a project's last owner.
func (r Role) CanManageMembers() bool {
	return r == RoleOwner || r == RoleAdmin
}

// NamedProject is a project as a chooser renders it: the id a write must name, and the
// display name a human recognises.
//
// 🔴 BOTH FIELDS, FOR THE REASON `NamedScope` CARRIES BOTH. The id is what any write must
// carry, because a display name is mutable and not unique; the name is the only half a
// person can act on. A chooser offering one without the other is either unusable or
// unsafe, and the share flow already records what happens when a human is shown a name
// and the handler keys on an id — a 404 whose sentence is false.
type NamedProject struct {
	ID   ID
	Name string
}

// ProjectsManagedBy lists every project in which `user` holds a role that
// [Role.CanManageMembers] admits, sorted by display name then id.
//
// 🔴 IT READS MEMBERSHIPS, NEVER `Project.OwnerUserID`. Those two can disagree: the owner
// field records who the project was created FOR, and `EventMemberSet` can move the owner
// role afterwards, so a listing built from the field would offer a project to somebody
// whose membership has since been demoted and omit the person who actually holds it.
// `Memberships` is what `Resolve` reads, so it is what this reads.
//
// ⚠ A SOLO PROJECT IS INCLUDED, DELIBERATELY. `Project.Solo` marks one auto-created at
// signup and its own comment says it is otherwise a normal project — it carries no
// one-member invariant — so excluding it here would be a rule this package does not have,
// and it would block the first thing a new user would want to do, which is invite
// somebody into the project they already have.
//
// ⚠ THE SORT IS BY NAME WITH THE ID AS TIE-BREAK, so the order is TOTAL. Names are not
// unique (see `checkScopeNamesAreFree` for the same problem one layer down), and a sort on
// a non-unique key leaves the order of equal elements unspecified — which would make a
// rendered chooser's order wobble between requests and any test over it flaky.
// ⚠ THERE IS NO `user == ""` FAST PATH, AND ITS ABSENCE IS MEASURED RATHER THAN AN
// OVERSIGHT. One was written here and a mutation sweep SURVIVED its removal: no membership
// can be keyed on an empty id (`checkSubject` refuses one), so the loop below already
// answers nothing for it and the guard could not change any output. A guard whose deletion
// no test can detect is not protection — it is a line that reads as protection, which is
// worse than nothing because it stops the next reader asking.
func (m Model) ProjectsManagedBy(user ID) []NamedProject {
	var out []NamedProject
	for project, ms := range m.Memberships {
		ship, ok := ms[user]
		if !ok || !ship.Role.CanManageMembers() {
			continue
		}
		// A membership naming a project the model does not hold is not reachable through
		// `apply` — `checkSubject` refuses it — so this is a defensive skip rather than a
		// case with a story. It is a skip and not a panic because a Model is also built
		// by tests and by a replay of somebody else's journal.
		p, known := m.Projects[project]
		if !known {
			continue
		}
		out = append(out, NamedProject{ID: p.ID, Name: p.Name})
	}
	slices.SortFunc(out, func(a, b NamedProject) int {
		if c := cmp.Compare(a.Name, b.Name); c != 0 {
			return c
		}
		return cmp.Compare(string(a.ID), string(b.ID))
	})
	return out
}

// RoleIn is the role `user` holds in `project`, and false when they hold none.
//
// 🔴 A SINGLE LOOKUP EXISTS SO A CALLER NEVER INDEXES `Memberships` TWICE. The map is
// keyed project-then-user and a caller that reached in itself would have to handle a
// missing outer map and a missing inner one; two of those, at two call sites, is how one
// of them ends up treating "no such project" as "no membership" and the other panicking.
func (m Model) RoleIn(project, user ID) (Role, bool) {
	inner, ok := m.Memberships[project]
	if !ok {
		return "", false
	}
	ship, ok := inner[user]
	if !ok {
		return "", false
	}
	return ship.Role, true
}
