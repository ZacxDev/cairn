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

// CanConfer answers whether a principal holding `r` may hand `other` to somebody else.
//
// # 🔴 IT EXISTS SO THE ESCALATION RULE HAS ONE SPELLING, AND IT ALREADY HAD TWO READERS
//
// The rule is "an admin may not make an owner". Without it that condition sits open-coded
// at the MINT — where an invitation's role is chosen — and again at whatever renders the
// role CHOOSER, because a chooser that offers `owner` to an admin offers a value the mint
// will refuse. Two spellings of one rule, and the one that drifts is whichever is edited
// second. [Role.CanManageMembers]'s own comment makes this argument at length about a
// predicate that was open-coded at several sites and measured wrong at all but one of them
// in the same direction; this is the same argument, arriving before the second site exists.
//
// # 🔴 WHY AN ADMIN MAY NOT CONFER `owner`
//
// Because they could then confer it on THEMSELVES and the trail would not say so. An admin
// mints an `owner` invitation, redeems it with their own provider identity, and the journal
// records a `member-set` at `owner` whose actor is the inviter — which is them — so the
// escalation reads as an ordinary join performed by somebody entitled to perform it.
// `refuseOrphaning` draws the same line from the other side when it refuses to remove a
// project's last owner: the owner role is the one that cannot be reached sideways.
//
// ⚠ A ROLE THAT CANNOT MANAGE MEMBERS CONFERS NOTHING, which is a restatement rather than
// a second rule — it is [Role.CanManageMembers] consulted here so that a caller cannot
// satisfy this predicate without also satisfying that one. A caller checking only this one
// is then not a hole.
//
// ⚠ AND AN INVALID `other` IS REFUSED, so a role string that reached a form or a database
// without a mapping in `roleVerbs` cannot be conferred. That is [Role.Valid]'s fail-closed
// direction, asked here rather than assumed of the caller.
func (r Role) CanConfer(other Role) bool {
	if !r.CanManageMembers() || !other.Valid() {
		return false
	}
	return r == RoleOwner || other != RoleOwner
}

// NamedProject is a project as a chooser renders it: the id a write must name, the
// display name a human recognises, and the role the listed principal holds in it.
//
// 🔴 THE FIRST TWO, FOR THE REASON `NamedScope` CARRIES BOTH. The id is what any write must
// carry, because a display name is mutable and not unique; the name is the only half a
// person can act on. A chooser offering one without the other is either unusable or
// unsafe, and the share flow already records what happens when a human is shown a name
// and the handler keys on an id — a 404 whose sentence is false.
//
// 🔴 AND `HeldRole` IS HERE RATHER THAN FETCHED AGAIN, WHICH IS THE POINT OF PUTTING IT ON
// THIS TYPE AT ALL. A role chooser must offer only what the caller may confer
// ([Role.CanConfer]), so it needs the caller's own role in each project. Every value in
// this slice was produced by reading exactly that membership, so carrying it costs nothing
// — where a renderer that asked again would be a SECOND read of the model, taken at a
// different instant from the one that produced the list, and `ControlInviting.Invitable`'s
// own comment is about exactly that window. It is the role of the USER
// [Model.ProjectsManagedBy] was asked about, never the project's owner.
type NamedProject struct {
	ID       ID
	Name     string
	HeldRole Role
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
		// `ship.Role` is the membership this loop ALREADY read to decide the project
		// belongs in the list, so the caller's role costs no second lookup — see
		// [NamedProject.HeldRole] for why a renderer must not go and ask for it again.
		out = append(out, NamedProject{ID: p.ID, Name: p.Name, HeldRole: ship.Role})
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
